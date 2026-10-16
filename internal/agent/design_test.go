package agent

import (
	"context"
	"strings"
	"testing"
)

type fusionTestProvider struct{}

func (fusionTestProvider) Chat(_ context.Context, request ChatRequest) (ChatResponse, error) {
	if !strings.Contains(request.SystemPrompt, "融合总监") {
		return ChatResponse{Content: `{"title":"bad"}`}, nil
	}
	return ChatResponse{Content: `{"title":"融合方向","concept":"保留来源优点","design_strategy":{"direction":"平衡"},"color_palette":["#123456"],"typography":"现代宋体","difference_summary":"已记录来源取舍","image_prompt":"balanced packaging"}`, Provider: "test", Model: "test-model"}, nil
}

func TestLLMFusionProducesAuditableProposal(t *testing.T) {
	agent := NewLLMFusion(fusionTestProvider{})
	result, err := agent.Run(context.Background(), RunInput{Payload: map[string]any{
		"sources": []map[string]any{{"id": "a", "title": "方案 A"}, {"id": "b", "title": "方案 B"}},
		"fusion":  map[string]any{"feature_selections": []any{"保留 A 的叙事", "采用 B 的色彩"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if result.AgentName != "fusion" || result.Payload["title"] != "融合方向" {
		t.Fatalf("unexpected fusion result: %#v", result)
	}
}
