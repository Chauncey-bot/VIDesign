package workflow

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	"image/png"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"packaging-ai/internal/agent"
	"packaging-ai/internal/domain"
	"packaging-ai/internal/repository"
	"packaging-ai/internal/taskqueue"
)

const (
	GenerateTask = "generate_design"
	RevisionTask = "revise_design"
	FusionTask   = "fuse_design"
)

// protectedBrandMarkSurfacePrompt describes the visual treatment for a source
// Logo supplied to the image generator as an authoritative reference image.
const protectedBrandMarkSurfacePrompt = ` With a protected brand mark, the customer's original logo file is supplied to the image generator as an authoritative visual reference and the only source of the Logo artwork and Logo wording. Generate that exact logo as part of the package artwork itself: preserve its geometry, typography, every Chinese character and subtitle, proportions, spacing, color relationships and registered mark. Never substitute brand_name, product_name, product_series_name or any other Brief text for words inside the reference Logo; if exact Logo wording cannot be rendered, leave the Logo area blank rather than inventing or substituting text. Apply it directly to one clear, flat or appropriately curved, unobstructed primary package surface with a safe margin from edges, folds, seams, hardware, illustrations and required copy. Make the logo look like the specified physical finish (print, foil, emboss, engraving, paint or label) on that same surface, with matching perspective, curvature, grain, roughness, gloss, local highlights, contact shadow and occlusion. Do not invent, simplify, trace, replace, duplicate or redraw a different brand mark, and do not place it on the background, canvas, table, air or a different object.`

const protectedBrandMarkSurfaceNegative = `watermark, pasted PNG, floating brand mark, mark on canvas, mark in background, mark in air, white sticker edge, white halo, transparent overlay, flat unlit mark, duplicated mark, invented seal, fake brand symbol, pseudo-text, distorted mark, stretched mark, compressed mark, cropped mark, mark crossing an edge, mark crossing a fold or seam, mark covering required content, mark detached from the target surface`

type DesignWorkflow struct {
	repo     *repository.MySQLRepository
	concept  agent.Agent
	revision agent.Agent
	fusion   agent.Agent
	image    agent.ImageProvider
}

func NewDesignWorkflow(repo *repository.MySQLRepository, concept, revision agent.Agent, image agent.ImageProvider, fusion ...agent.Agent) *DesignWorkflow {
	var fusionAgent agent.Agent
	if len(fusion) > 0 {
		fusionAgent = fusion[0]
	}
	return &DesignWorkflow{repo: repo, concept: concept, revision: revision, fusion: fusionAgent, image: image}
}

func (w *DesignWorkflow) Handle(ctx context.Context, task taskqueue.Task) error {
	switch task.Kind {
	case GenerateTask:
		return w.generate(ctx, task)
	case RevisionTask:
		return w.revise(ctx, task)
	case FusionTask:
		return w.fuse(ctx, task)
	default:
		return fmt.Errorf("unsupported design task: %s", task.Kind)
	}
}

func (w *DesignWorkflow) fuse(ctx context.Context, task taskqueue.Task) error {
	if w.fusion == nil {
		return fmt.Errorf("fusion agent is not configured")
	}
	project, err := w.repo.Get(ctx, task.ProjectID)
	if err != nil {
		return err
	}
	brief, err := w.repo.GetLatest(ctx, task.ProjectID)
	if err != nil {
		return err
	}
	fusionID, _ := task.Payload["fusion_id"].(string)
	fusion, err := w.repo.GetFusion(ctx, fusionID)
	if err != nil {
		return err
	}
	if fusion.ProjectID != task.ProjectID {
		return fmt.Errorf("fusion does not belong to project")
	}
	protectedLogoIDs, protectedLogoAssets, err := w.protectedLogoAssets(ctx, brief)
	if err != nil {
		return err
	}
	sources := make([]map[string]any, 0, len(fusion.SourceProposalIDs))
	for _, sourceID := range fusion.SourceProposalIDs {
		proposal, err := w.repo.GetProposal(ctx, sourceID)
		if err != nil {
			return err
		}
		if proposal.ProjectID != task.ProjectID {
			return fmt.Errorf("source proposal does not belong to project")
		}
		proposal.Assets = nil
		sources = append(sources, map[string]any{"id": proposal.ID, "product_name": proposal.ProductName, "title": proposal.Title, "concept": proposal.Concept, "design_strategy": proposal.DesignStrategy, "color_palette": proposal.ColorPalette, "typography": proposal.Typography, "difference_summary": proposal.Difference})
	}
	briefPayload, _ := json.Marshal(brief)
	var briefMap map[string]any
	_ = json.Unmarshal(briefPayload, &briefMap)
	input := map[string]any{"brief": briefMap, "fusion": fusion, "sources": sources, "protected_logo_assets": protectedLogoAssets}
	run, err := w.fusion.Run(ctx, agent.RunInput{ProjectID: task.ProjectID, Round: project.CurrentRound + 1, Payload: input})
	if err != nil {
		return err
	}
	if err := w.repo.CreateAgentRun(ctx, task.ProjectID, run); err != nil {
		return err
	}
	proposal := proposalFromMap(task.ProjectID, project.CurrentRound+1, run.Payload)
	proposal.ProductName = productNameForProposal(proposal, "", map[string]struct{}{}, 0)
	proposal.FusionID = fusion.ID
	if err := w.persistProposalWithImage(ctx, proposal, run.Payload, protectedLogoIDs); err != nil {
		return err
	}
	if err := w.repo.CompleteAgentRun(ctx, task.ProjectID, run.AgentName, run.Payload); err != nil {
		return err
	}
	fusion.Status = "completed"
	fusion.OutputProposalIDs = append(fusion.OutputProposalIDs, proposal.ID)
	fusion.Version++
	if err := w.repo.UpdateFusion(ctx, fusion); err != nil {
		return err
	}
	if err := w.repo.UpdateRoundAndStatus(ctx, project.ID, project.CurrentRound+1, domain.ProjectRevising); err != nil {
		return err
	}
	return w.repo.UpdateStatus(ctx, project.ID, domain.ProjectAwaitFeedback)
}

func (w *DesignWorkflow) generate(ctx context.Context, task taskqueue.Task) error {
	project, err := w.repo.Get(ctx, task.ProjectID)
	if err != nil {
		return err
	}
	brief, err := w.repo.GetLatest(ctx, task.ProjectID)
	if err != nil {
		return err
	}
	if project.Status != domain.ProjectBriefReady && project.Status != domain.ProjectAwaitFeedback {
		return fmt.Errorf("project is not ready for design generation: %s", project.Status)
	}
	count := taskProposalCount(task.Payload["proposal_count"])
	round := project.CurrentRound + 1
	requestedProductName := strings.TrimSpace(stringValue(task.Payload, "product_name", ""))
	protectedLogoIDs, protectedLogoAssets, err := w.protectedLogoAssets(ctx, brief)
	if err != nil {
		return err
	}
	usedProductNames := make(map[string]struct{})
	generated := 0
	for generated < count {
		batch := count - generated
		if batch > 3 {
			batch = 3
		}
		conceptPayload := map[string]any{"brief": brief, "proposal_count": batch, "product_name": requestedProductName, "protected_logo_assets": protectedLogoAssets}
		if requestedProductName == "" {
			conceptPayload["product_name_mode"] = "auto_unique"
		} else {
			conceptPayload["product_name_mode"] = "specified"
		}
		run, err := w.concept.Run(ctx, agent.RunInput{ProjectID: task.ProjectID, Round: round, Payload: conceptPayload})
		if err != nil {
			return err
		}
		if err := w.repo.CreateAgentRun(ctx, task.ProjectID, run); err != nil {
			return err
		}
		items, ok := run.Payload["proposals"].([]any)
		if !ok || len(items) == 0 {
			return fmt.Errorf("concept Agent returned no proposals")
		}
		for _, item := range items {
			proposalMap, ok := item.(map[string]any)
			if !ok {
				continue
			}
			proposal := proposalFromMap(task.ProjectID, round, proposalMap)
			proposal.ProductName = productNameForProposal(proposal, requestedProductName, usedProductNames, generated+1)
			if err := w.persistProposalWithImage(ctx, proposal, proposalMap, protectedLogoIDs); err != nil {
				return err
			}
			generated++
			if generated >= count {
				break
			}
		}
		if err := w.repo.CompleteAgentRun(ctx, task.ProjectID, run.AgentName, run.Payload); err != nil {
			return err
		}
	}
	if err := w.repo.UpdateRoundAndStatus(ctx, project.ID, round, domain.ProjectGenerating); err != nil {
		return err
	}
	return w.repo.UpdateStatus(ctx, project.ID, domain.ProjectAwaitFeedback)
}

// taskProposalCount handles numbers after Task has been decoded from JSON.
// Keep the default conservative while allowing the UI's requested nine
// proposals to be generated as three-image concept batches.
func taskProposalCount(value any) int {
	count := 3
	switch number := value.(type) {
	case int:
		count = number
	case int64:
		count = int(number)
	case float64:
		count = int(number)
	case json.Number:
		if parsed, err := number.Int64(); err == nil {
			count = int(parsed)
		}
	}
	if count < 1 || count > 9 {
		return 3
	}
	return count
}

func (w *DesignWorkflow) revise(ctx context.Context, task taskqueue.Task) error {
	project, err := w.repo.Get(ctx, task.ProjectID)
	if err != nil {
		return err
	}
	brief, err := w.repo.GetLatest(ctx, task.ProjectID)
	if err != nil {
		return err
	}
	selectedID, _ := task.Payload["selected_proposal_id"].(string)
	selected, err := w.repo.GetProposal(ctx, selectedID)
	if err != nil {
		return err
	}
	// Proposal assets can contain multi-megabyte base64 data URLs. They are
	// already persisted and are not useful to the text revision Agent; sending
	// them back would exceed the model context window.
	selectedForAgent := selected
	selectedForAgent.Assets = nil
	selectedForAgent.ImageAssetIDs = nil
	feedback, _ := task.Payload["feedback"].(map[string]any)
	preserve, _ := task.Payload["preserve"].([]any)
	protectedLogoIDs, protectedLogoAssets, err := w.protectedLogoAssets(ctx, brief)
	if err != nil {
		return err
	}
	revisionPayload := map[string]any{"brief": brief, "proposal": selectedForAgent, "feedback": feedback, "preserve": preserve, "protected_logo_assets": protectedLogoAssets}
	if ids, ok := task.Payload["reference_asset_ids"].([]any); ok {
		assetIDs := make([]string, 0, len(ids))
		for _, id := range ids {
			if value, ok := id.(string); ok {
				assetIDs = append(assetIDs, value)
			}
		}
		assets, err := w.repo.GetAssets(ctx, assetIDs)
		if err != nil {
			return err
		}
		references := make([]map[string]any, 0, len(assets))
		for _, asset := range assets {
			reference := map[string]any{"name": asset.Prompt, "type": asset.Model}
			if strings.HasPrefix(asset.StorageURL, "data:image/") {
				reference["storage_url"] = asset.StorageURL
			}
			references = append(references, reference)
		}
		revisionPayload["reference_assets"] = references
	}
	run, err := w.revision.Run(ctx, agent.RunInput{ProjectID: task.ProjectID, Round: project.CurrentRound + 1, Payload: revisionPayload})
	if err != nil {
		return err
	}
	if err := w.repo.CreateAgentRun(ctx, task.ProjectID, run); err != nil {
		return err
	}
	proposal := proposalFromMap(task.ProjectID, project.CurrentRound+1, run.Payload)
	proposal.ProductName = productNameForProposal(proposal, selected.ProductName, map[string]struct{}{}, 0)
	if err := w.persistProposalWithImage(ctx, proposal, run.Payload, protectedLogoIDs); err != nil {
		return err
	}
	if err := w.repo.CompleteAgentRun(ctx, task.ProjectID, run.AgentName, run.Payload); err != nil {
		return err
	}
	if err := w.repo.UpdateRoundAndStatus(ctx, project.ID, project.CurrentRound+1, domain.ProjectRevising); err != nil {
		return err
	}
	return w.repo.UpdateStatus(ctx, project.ID, domain.ProjectAwaitFeedback)
}

func (w *DesignWorkflow) persistProposalWithImage(ctx context.Context, proposal domain.DesignProposal, source map[string]any, protectedLogoIDs []string) error {
	prompt, _ := source["image_prompt"].(string)
	if prompt == "" {
		prompt = proposal.Concept + ", a production-ready packaging product mockup based on the package form, material, dimensions and use case in the brief, premium product photography"
	}
	prompt += " Render one coherent, manufacturable package system containing every package specification and component required by the brief, such as the main box, inner box, bottle, jar, tin, pouch, tube, label, carton or different capacities. Each required package unit must be visible as a real physical object with its own readable display surface, consistent branding, material and production process. Arrange the complete set as a coherent product presentation without unrelated products or overlapping the required package faces. Do not create a moodboard, presentation board, design sheet, nine-grid collage, split-screen layout or floating graphic elements."
	if proposal.ProductName != "" {
		prompt = fmt.Sprintf("%s. The product name must be rendered exactly as provided, character for character, with no translation, abbreviation, added suffix, omitted character or synonym: %s.", prompt, proposal.ProductName)
	}
	negative := "moodboard, presentation board, design sheet, nine-grid collage, split-screen collage, unrelated product, missing package specification, hidden package unit, blocked package face, floating graphic, logo on canvas, logo in background, watermark, invented logo, fake seal, duplicate logo"
	if proposal.ProductName != "" {
		negative += "; do not alter, translate, abbreviate, add to, remove from or replace the specified product name"
	}
	if len(protectedLogoIDs) > 0 {
		negative += "; " + protectedBrandMarkSurfaceNegative + "; do not invent, duplicate, replace, distort, crop, restyle or add effects to the supplied reference Logo"
		prompt += protectedBrandMarkSurfacePrompt
	}
	referenceImages, err := w.logoReferenceImages(ctx, protectedLogoIDs)
	if err != nil {
		return err
	}
	image, err := w.image.Generate(ctx, agent.ImageRequest{Prompt: prompt, Negative: negative, ReferenceIDs: protectedLogoIDs, ReferenceImages: referenceImages, Width: 1024, Height: 1024})
	if err != nil {
		return err
	}
	if len(protectedLogoIDs) > 0 {
		image.Provider += "+logo-in-generation"
	}
	// Persist only after the image request succeeds. A provider timeout can
	// cause Asynq to retry a task; creating the proposal first would leave empty
	// duplicate proposals on every retry.
	if err := w.repo.CreateProposal(ctx, proposal); err != nil {
		return err
	}
	asset := domain.Asset{ID: repository.NewID(), ProjectID: proposal.ProjectID, AssetType: "proposal_mockup", StorageURL: image.AssetURL, Width: image.Width, Height: image.Height, Prompt: prompt, Provider: image.Provider, Model: image.Model, Version: 1}
	if err := w.repo.CreateAsset(ctx, asset); err != nil {
		return err
	}
	return w.repo.LinkProposalAsset(ctx, proposal.ID, asset.ID)
}

func readImageSource(ctx context.Context, source string) ([]byte, error) {
	if strings.HasPrefix(source, "data:") {
		comma := strings.IndexByte(source, ',')
		if comma < 0 {
			return nil, fmt.Errorf("invalid data URL")
		}
		meta, payload := source[:comma], source[comma+1:]
		if strings.HasSuffix(meta, ";base64") {
			return base64.StdEncoding.DecodeString(payload)
		}
		return []byte(payload), nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("image download failed (%d)", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 32<<20))
}

func renderLogoAsset(ctx context.Context, asset domain.Asset) ([]byte, error) {
	input, err := readImageSource(ctx, asset.StorageURL)
	if err != nil {
		return nil, err
	}
	ext := strings.ToLower(filepath.Ext(asset.Prompt))
	if ext == "" {
		ext = strings.ToLower(strings.TrimSpace(asset.Model))
	}
	if ext == ".png" || ext == ".jpg" || ext == ".jpeg" || ext == ".webp" {
		return input, nil
	}
	tmpDir, err := os.MkdirTemp("", "protected-logo-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)
	inputPath := filepath.Join(tmpDir, "source"+ext)
	if err := os.WriteFile(inputPath, input, 0600); err != nil {
		return nil, err
	}
	switch ext {
	case ".ai", ".eps", ".pdf":
		outputBase := filepath.Join(tmpDir, "logo")
		cmd := exec.CommandContext(ctx, "pdftoppm", "-png", "-singlefile", "-r", "144", inputPath, outputBase)
		if output, err := cmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("pdftoppm: %w (%s)", err, strings.TrimSpace(string(output)))
		}
		return os.ReadFile(outputBase + ".png")
	case ".svg":
		outputPath := filepath.Join(tmpDir, "logo.png")
		cmd := exec.CommandContext(ctx, "rsvg-convert", "-o", outputPath, inputPath)
		if output, err := cmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("rsvg-convert: %w (%s)", err, strings.TrimSpace(string(output)))
		}
		return os.ReadFile(outputPath)
	case ".png":
		return input, nil
	default:
		return nil, fmt.Errorf("unsupported protected logo format %q", ext)
	}
}

// logoReferenceImages converts the customer's source artwork to transparent
// PNG reference images for the image generator. The image model receives the
// original mark during generation; this service does not paste it afterward.
func (w *DesignWorkflow) logoReferenceImages(ctx context.Context, ids []string) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	assets, err := w.repo.GetAssets(ctx, ids)
	if err != nil {
		return nil, err
	}
	references := make([]string, 0, len(assets))
	for _, asset := range assets {
		data, err := renderLogoAsset(ctx, asset)
		if err != nil {
			return nil, fmt.Errorf("render logo reference %s: %w", asset.ID, err)
		}
		decoded, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("decode logo reference %s: %w", asset.ID, err)
		}
		cropped := cropLogo(decoded)
		var encoded bytes.Buffer
		if err := png.Encode(&encoded, cropped); err != nil {
			return nil, fmt.Errorf("encode logo reference %s: %w", asset.ID, err)
		}
		references = append(references, "data:image/png;base64,"+base64.StdEncoding.EncodeToString(encoded.Bytes()))
	}
	return references, nil
}

func cropLogo(source image.Image) image.Image {
	bounds := source.Bounds()
	minX, minY := bounds.Max.X, bounds.Max.Y
	maxX, maxY := bounds.Min.X, bounds.Min.Y
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, a := source.At(x, y).RGBA()
			if a < 1024 || (r > 62500 && g > 62500 && b > 62500) {
				continue
			}
			if x < minX {
				minX = x
			}
			if y < minY {
				minY = y
			}
			if x > maxX {
				maxX = x
			}
			if y > maxY {
				maxY = y
			}
		}
	}
	if minX > maxX || minY > maxY {
		return source
	}
	result := image.NewRGBA(image.Rect(0, 0, maxX-minX+1, maxY-minY+1))
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			r, g, b, a := source.At(x, y).RGBA()
			if r > 62500 && g > 62500 && b > 62500 {
				a = 0
			}
			result.SetRGBA(x-minX, y-minY, color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a >> 8)})
		}
	}
	return result
}

func (w *DesignWorkflow) protectedLogoAssets(ctx context.Context, brief domain.Brief) ([]string, []map[string]any, error) {
	ids := protectedAssetIDs(brief.BrandInfo["protected_logo_asset_ids"])
	if len(ids) == 0 {
		return nil, []map[string]any{}, nil
	}
	assets, err := w.repo.GetAssets(ctx, ids)
	if err != nil {
		return nil, nil, err
	}
	metadata := make([]map[string]any, 0, len(assets))
	for _, asset := range assets {
		metadata = append(metadata, map[string]any{"asset_id": asset.ID, "name": asset.Prompt, "type": asset.Model, "usage": "原始品牌标识，禁止任何调整"})
	}
	return ids, metadata, nil
}

func proposalFromMap(projectID string, round int, value map[string]any) domain.DesignProposal {
	strategy, _ := value["design_strategy"].(map[string]any)
	palette := toStringSlice(value["color_palette"])
	return domain.DesignProposal{ID: repository.NewID(), ProjectID: projectID, Round: round, ProductName: stringValue(value, "product_name", ""), Title: stringValue(value, "title", "未命名方案"), Concept: stringValue(value, "concept", "包装创意方案"), DesignStrategy: strategy, ColorPalette: palette, Typography: stringValue(value, "typography", ""), Difference: stringValue(value, "difference_summary", ""), Status: "draft"}
}

func productNameForProposal(proposal domain.DesignProposal, requested string, used map[string]struct{}, ordinal int) string {
	if requested = strings.TrimSpace(requested); requested != "" {
		used[requested] = struct{}{}
		return requested
	}
	candidate := strings.TrimSpace(proposal.ProductName)
	if candidate == "" {
		candidate = strings.TrimSpace(proposal.Title)
		if candidate == "" {
			candidate = "创意茶品"
		}
		candidate += "系列"
	}
	base := candidate
	for suffix := 2; ; suffix++ {
		if _, exists := used[candidate]; !exists {
			used[candidate] = struct{}{}
			return candidate
		}
		candidate = fmt.Sprintf("%s %d", base, suffix)
		if suffix == 2 && ordinal > 0 {
			candidate = fmt.Sprintf("%s %d", base, ordinal)
		}
	}
}

func stringValue(value map[string]any, key, fallback string) string {
	if result, ok := value[key].(string); ok && strings.TrimSpace(result) != "" {
		return result
	}
	return fallback
}

func toStringSlice(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok {
			result = append(result, text)
		}
	}
	return result
}
