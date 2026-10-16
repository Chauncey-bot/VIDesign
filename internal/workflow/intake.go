package workflow

import (
	"context"
	"encoding/json"
	"fmt"

	"packaging-ai/internal/agent"
	"packaging-ai/internal/domain"
	"packaging-ai/internal/repository"
	"packaging-ai/internal/taskqueue"
)

const ChatTask = "intake_chat"

type IntakeWorkflow struct {
	repo   *repository.MySQLRepository
	intake agent.Agent
}

func NewIntakeWorkflow(repo *repository.MySQLRepository, intake agent.Agent) *IntakeWorkflow {
	return &IntakeWorkflow{repo: repo, intake: intake}
}

func (w *IntakeWorkflow) Handle(ctx context.Context, task taskqueue.Task) error {
	if task.Kind != ChatTask {
		return fmt.Errorf("unsupported task kind: %s", task.Kind)
	}
	project, err := w.repo.Get(ctx, task.ProjectID)
	if err != nil {
		return err
	}
	brief, err := w.repo.GetLatest(ctx, task.ProjectID)
	if err != nil && err != repository.ErrNotFound {
		return err
	}
	if brief.ProjectID == "" {
		brief = domain.Brief{ID: repository.NewID(), ProjectID: task.ProjectID, ProductInfo: map[string]any{}, BrandInfo: map[string]any{}, PackageSpec: map[string]any{}, RequiredText: []string{}, Constraints: []string{}, Version: 1}
	}

	briefPayload := map[string]any{}
	if brief.ProductInfo != nil {
		for key, value := range brief.ProductInfo {
			briefPayload[key] = value
		}
	}
	if brief.Audience != "" {
		briefPayload["audience"] = brief.Audience
	}
	if brief.Material != "" {
		briefPayload["material"] = brief.Material
	}
	if brief.PrintingProcess != "" {
		briefPayload["printing_process"] = brief.PrintingProcess
	}
	if brief.PackageSpec != nil {
		briefPayload["package_spec"] = brief.PackageSpec
	}
	if len(brief.RequiredText) > 0 {
		briefPayload["required_text"] = brief.RequiredText
	}
	if len(brief.Constraints) > 0 {
		briefPayload["constraints"] = brief.Constraints
	}

	message, _ := task.Payload["message"].(string)
	agentPayload := map[string]any{"message": message, "brief": briefPayload}
	if documentText, ok := task.Payload["document_text"].(string); ok {
		agentPayload["document_text"] = documentText
	}
	if ids, ok := task.Payload["document_asset_ids"].([]any); ok {
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
		imageURLs := make([]string, 0, len(assets))
		for _, asset := range assets {
			imageURLs = append(imageURLs, asset.StorageURL)
		}
		agentPayload["document_images"] = imageURLs
	}
	if ids, ok := task.Payload["brand_asset_ids"].([]any); ok {
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
		brandAssets := make([]map[string]any, 0, len(assets))
		for _, asset := range assets {
			brandAssets = append(brandAssets, map[string]any{"name": asset.Prompt, "type": asset.Model})
		}
		agentPayload["brand_assets"] = brandAssets
	}
	run, err := w.intake.Run(ctx, agent.RunInput{ProjectID: task.ProjectID, Round: project.CurrentRound, Payload: agentPayload})
	if err != nil {
		return err
	}
	if err := w.repo.CreateAgentRun(ctx, task.ProjectID, run); err != nil {
		return err
	}

	if patch, ok := run.Payload["brief_patch"].(map[string]any); ok {
		applyPatch(&brief, patch)
	}
	if score, ok := run.Payload["completion_score"].(int); ok {
		brief.CompletionScore = score
	}
	if score, ok := run.Payload["completion_score"].(float64); ok {
		brief.CompletionScore = int(score)
	}
	// Each intake pass is a new immutable Brief snapshot. Reusing the
	// previous ID would violate the briefs primary key on the next chat turn.
	brief.ID = repository.NewID()
	brief.Version++
	if err := w.repo.Upsert(ctx, brief); err != nil {
		return err
	}

	// Draft always passes through collecting_requirements before it can become
	// brief_ready, even when the first message contains every required field.
	if project.Status == domain.ProjectDraft {
		if err := w.repo.UpdateStatus(ctx, project.ID, domain.ProjectCollecting); err != nil {
			return err
		}
		project.Status = domain.ProjectCollecting
	}
	to := domain.ProjectCollecting
	if brief.CompletionScore >= 80 {
		to = domain.ProjectBriefReady
	}
	if err := domain.Transition(project.Status, to); err != nil {
		// A retried task can observe an already advanced state. Keep it idempotent.
		if project.Status != to {
			return err
		}
	} else if project.Status != to {
		if err := w.repo.UpdateStatus(ctx, project.ID, to); err != nil {
			return err
		}
	}
	return w.repo.CompleteAgentRun(ctx, task.ProjectID, run.AgentName, run.Payload)
}

func applyPatch(brief *domain.Brief, patch map[string]any) {
	if brief.ProductInfo == nil {
		brief.ProductInfo = map[string]any{}
	}
	for key, value := range patch {
		switch key {
		case "audience":
			brief.Audience = fmt.Sprint(value)
		case "market":
			brief.Market = fmt.Sprint(value)
		case "material":
			brief.Material = fmt.Sprint(value)
		case "printing_process":
			brief.PrintingProcess = fmt.Sprint(value)
		case "required_text":
			if values, ok := toStrings(value); ok {
				brief.RequiredText = values
			}
		case "constraints":
			if values, ok := toStrings(value); ok {
				brief.Constraints = values
			}
		case "package_spec":
			if values, ok := value.(map[string]any); ok {
				brief.PackageSpec = values
			}
		default:
			brief.ProductInfo[key] = value
		}
	}
}

func toStrings(value any) ([]string, bool) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, false
	}
	var values []string
	if err := json.Unmarshal(data, &values); err != nil {
		return nil, false
	}
	return values, true
}
