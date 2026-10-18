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

type conceptTestProvider struct{ request ChatRequest }

func (p *conceptTestProvider) Chat(_ context.Context, request ChatRequest) (ChatResponse, error) {
	p.request = request
	return ChatResponse{Content: `{"proposals":[{"product_name":"松萝茶·山岚玉青","title":"自然东方","concept":"保留山岚玉青主色","design_strategy":{"direction":"自然东方"},"color_palette":["#2e6654"],"typography":"现代宋体","difference_summary":"清晰克制","image_prompt":"jade green tea packaging"}]}`, Provider: "test", Model: "test-model"}, nil
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

func TestLLMConceptIncludesSpecifiedProductName(t *testing.T) {
	provider := &conceptTestProvider{}
	concept := NewLLMConcept(provider)
	result, err := concept.Run(context.Background(), RunInput{Payload: map[string]any{
		"brief":          map[string]any{"product_name": "Brief 中的名称"},
		"proposal_count": 1,
		"product_name":   "松萝茶·山岚玉青",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Payload["proposals"] == nil || !strings.Contains(provider.request.Messages[0].Content, "松萝茶·山岚玉青") || !strings.Contains(provider.request.Messages[0].Content, "不得改写") {
		t.Fatalf("product name was not included in concept request: %#v", provider.request)
	}
	if !strings.Contains(provider.request.SystemPrompt, "product_name") {
		t.Fatalf("product_name field missing from system prompt: %s", provider.request.SystemPrompt)
	}
}

func TestLLMConceptLocksUploadedBrandLogo(t *testing.T) {
	provider := &conceptTestProvider{}
	concept := NewLLMConcept(provider)
	_, err := concept.Run(context.Background(), RunInput{Payload: map[string]any{
		"brief":                 map[string]any{"product_name": "品牌茶"},
		"proposal_count":        1,
		"protected_logo_assets": []map[string]any{{"asset_id": "logo-1", "name": "brand.svg", "type": ".svg"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(provider.request.Messages[0].Content, "禁止重绘") || !strings.Contains(provider.request.Messages[0].Content, "原样使用") {
		t.Fatalf("logo protection rule missing from concept request: %s", provider.request.Messages[0].Content)
	}
	if !strings.Contains(provider.request.SystemPrompt, "禁止对 Logo 做任何调整") {
		t.Fatalf("logo protection rule missing from system prompt: %s", provider.request.SystemPrompt)
	}
}
