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
		sources = append(sources, map[string]any{"id": proposal.ID, "title": proposal.Title, "concept": proposal.Concept, "design_strategy": proposal.DesignStrategy, "color_palette": proposal.ColorPalette, "typography": proposal.Typography, "difference_summary": proposal.Difference})
	}
	briefPayload, _ := json.Marshal(brief)
	var briefMap map[string]any
	_ = json.Unmarshal(briefPayload, &briefMap)
	input := map[string]any{"brief": briefMap, "fusion": fusion, "sources": sources}
	run, err := w.fusion.Run(ctx, agent.RunInput{ProjectID: task.ProjectID, Round: project.CurrentRound + 1, Payload: input})
	if err != nil {
		return err
	}
	if err := w.repo.CreateAgentRun(ctx, task.ProjectID, run); err != nil {
		return err
	}
	proposal := proposalFromMap(task.ProjectID, project.CurrentRound+1, run.Payload)
	proposal.FusionID = fusion.ID
	if err := w.persistProposalWithImage(ctx, proposal, run.Payload); err != nil {
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
	generated := 0
	for generated < count {
		batch := count - generated
		if batch > 3 {
			batch = 3
		}
		run, err := w.concept.Run(ctx, agent.RunInput{ProjectID: task.ProjectID, Round: round, Payload: map[string]any{"brief": brief, "proposal_count": batch}})
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
			if err := w.persistProposalWithImage(ctx, proposal, proposalMap); err != nil {
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
	revisionPayload := map[string]any{"brief": brief, "proposal": selectedForAgent, "feedback": feedback, "preserve": preserve}
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
	if err := w.persistProposalWithImage(ctx, proposal, run.Payload); err != nil {
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

func (w *DesignWorkflow) persistProposalWithImage(ctx context.Context, proposal domain.DesignProposal, source map[string]any) error {
	if err := w.repo.CreateProposal(ctx, proposal); err != nil {
		return err
	}
	prompt, _ := source["image_prompt"].(string)
	if prompt == "" {
		prompt = proposal.Concept + ", packaging mockup, premium product photography"
	}
	image, err := w.image.Generate(ctx, agent.ImageRequest{Prompt: prompt, Width: 1024, Height: 1024})
	if err != nil {
		return err
	}
	asset := domain.Asset{ID: repository.NewID(), ProjectID: proposal.ProjectID, AssetType: "proposal_mockup", StorageURL: image.AssetURL, Width: image.Width, Height: image.Height, Prompt: prompt, Provider: image.Provider, Model: image.Model, Version: 1}
	if err := w.repo.CreateAsset(ctx, asset); err != nil {
		return err
	}
	return w.repo.LinkProposalAsset(ctx, proposal.ID, asset.ID)
}

func proposalFromMap(projectID string, round int, value map[string]any) domain.DesignProposal {
	strategy, _ := value["design_strategy"].(map[string]any)
	palette := toStringSlice(value["color_palette"])
	return domain.DesignProposal{ID: repository.NewID(), ProjectID: projectID, Round: round, Title: stringValue(value, "title", "未命名方案"), Concept: stringValue(value, "concept", "包装创意方案"), DesignStrategy: strategy, ColorPalette: palette, Typography: stringValue(value, "typography", ""), Difference: stringValue(value, "difference_summary", ""), Status: "draft"}
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
