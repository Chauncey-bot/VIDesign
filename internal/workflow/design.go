package workflow

import (
	"context"
	"encoding/json"
	"fmt"
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
	count := 3
	if value, ok := task.Payload["proposal_count"].(float64); ok && int(value) > 0 {
		count = int(value)
	}
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
	if err := w.repo.CreateProposal(ctx, proposal); err != nil {
		return err
	}
	prompt, _ := source["image_prompt"].(string)
	if prompt == "" {
		prompt = proposal.Concept + ", packaging mockup, premium product photography"
	}
	if proposal.ProductName != "" {
		prompt = fmt.Sprintf("%s. The product name must be considered in the design and clearly presented as: %s.", prompt, proposal.ProductName)
	}
	negative := ""
	if len(protectedLogoIDs) > 0 {
		negative = "do not redraw, alter, recolor, distort, crop, restyle, replace or add effects to any protected brand logo"
		prompt += " Use the customer's original uploaded brand logo file exactly as provided. Do not redraw, alter, recolor, distort, crop, restyle, rearrange, replace or add effects to the logo; only scale proportionally and position it on the package."
	}
	image, err := w.image.Generate(ctx, agent.ImageRequest{Prompt: prompt, Negative: negative, ReferenceIDs: protectedLogoIDs, Width: 1024, Height: 1024})
	if err != nil {
		return err
	}
	asset := domain.Asset{ID: repository.NewID(), ProjectID: proposal.ProjectID, AssetType: "proposal_mockup", StorageURL: image.AssetURL, Width: image.Width, Height: image.Height, Prompt: prompt, Provider: image.Provider, Model: image.Model, Version: 1}
	if err := w.repo.CreateAsset(ctx, asset); err != nil {
		return err
	}
	return w.repo.LinkProposalAsset(ctx, proposal.ID, asset.ID)
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
		metadata = append(metadata, map[string]any{"asset_id": asset.ID, "name": asset.Prompt, "type": asset.Model, "usage": "原始 Logo，禁止任何调整"})
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
