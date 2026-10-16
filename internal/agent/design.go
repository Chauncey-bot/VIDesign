package agent

import (
	"context"
	"encoding/json"
	"fmt"
)

type LLMConcept struct{ provider LLMProvider }

func NewLLMConcept(provider LLMProvider) *LLMConcept { return &LLMConcept{provider: provider} }
func (a *LLMConcept) Name() string                   { return "concept" }

func (a *LLMConcept) Run(ctx context.Context, input RunInput) (RunOutput, error) {
	brief, err := json.Marshal(input.Payload["brief"])
	if err != nil {
		return RunOutput{}, err
	}
	count := 3
	if value, ok := input.Payload["proposal_count"].(int); ok && value > 0 && value <= 6 {
		count = value
	}
	result, err := a.provider.Chat(ctx, ChatRequest{
		SystemPrompt: `你是专业的产品包装创意总监。根据 Brief 生成多套差异明显、可执行的包装设计方向。只输出 JSON：proposals 是数组，每项包含 title、concept、design_strategy（对象）、color_palette（字符串数组）、typography、difference_summary、image_prompt。每套方案必须体现不同的设计策略，不要生成具体品牌侵权元素。`,
		Messages:     []Message{{Role: "user", Content: fmt.Sprintf("生成 %d 套方案。Brief：%s", count, brief)}},
		Temperature:  0.8,
	})
	if err != nil {
		return RunOutput{}, err
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(result.Content), &payload); err != nil {
		return RunOutput{}, fmt.Errorf("decode concept JSON: %w", err)
	}
	if _, ok := payload["proposals"].([]any); !ok {
		return RunOutput{}, fmt.Errorf("concept output missing proposals")
	}
	return RunOutput{AgentName: a.Name(), Payload: payload, Usage: Usage{Provider: result.Provider, Model: result.Model, InputTokens: result.InputTokens, OutputTokens: result.OutputTokens}}, nil
}

type RevisionInput struct {
	Brief    map[string]any `json:"brief"`
	Proposal map[string]any `json:"proposal"`
	Feedback map[string]any `json:"feedback"`
	Preserve []string       `json:"preserve"`
}

type LLMRevision struct{ provider LLMProvider }

func NewLLMRevision(provider LLMProvider) *LLMRevision { return &LLMRevision{provider: provider} }
func (a *LLMRevision) Name() string                    { return "revision" }

func (a *LLMRevision) Run(ctx context.Context, input RunInput) (RunOutput, error) {
	data, err := json.Marshal(input.Payload)
	if err != nil {
		return RunOutput{}, err
	}
	result, err := a.provider.Chat(ctx, ChatRequest{
		SystemPrompt: `你是包装设计迭代 Agent。基于原方案和客户反馈生成一套二轮修改方案。保留客户明确认可的元素，只修改反馈要求的部分。只输出 JSON，字段与单套 proposal 相同：title、concept、design_strategy、color_palette、typography、difference_summary、image_prompt。`,
		Messages:     []Message{{Role: "user", Content: string(data)}},
		Temperature:  0.6,
	})
	if err != nil {
		return RunOutput{}, err
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(result.Content), &payload); err != nil {
		return RunOutput{}, fmt.Errorf("decode revision JSON: %w", err)
	}
	if _, ok := payload["title"].(string); !ok {
		return RunOutput{}, fmt.Errorf("revision output missing title")
	}
	return RunOutput{AgentName: a.Name(), Payload: payload, Usage: Usage{Provider: result.Provider, Model: result.Model, InputTokens: result.InputTokens, OutputTokens: result.OutputTokens}}, nil
}
