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
	productName, _ := input.Payload["product_name"].(string)
	productNameRule := "未指定产品名称：请为每套方案生成一个简洁、可用且彼此不同的 product_name。"
	if productName != "" {
		productNameRule = fmt.Sprintf("已指定产品名称“%s”：每套方案的 product_name 必须使用这个名称，并将它纳入包装文字层级、识别性和画面设计考虑。", productName)
	}
	logoRule := "未上传品牌 Logo。"
	if assets, ok := input.Payload["protected_logo_assets"].([]map[string]any); ok && len(assets) > 0 {
		logoRule = "客户上传了受保护的品牌 Logo 源文件。必须原样使用这些 Logo：禁止重绘、变形、改色、描边、重新排版、裁切、加滤镜或替换；只允许按比例缩放和调整在包装上的位置。image_prompt 必须明确要求使用原始 Logo 文件。"
	}
	result, err := a.provider.Chat(ctx, ChatRequest{
		SystemPrompt: `你是专业的产品包装创意总监。根据 Brief 生成多套差异明显、可执行的包装设计方向。只输出 JSON：proposals 是数组，每项包含 product_name、title、concept、design_strategy（对象）、color_palette（字符串数组）、typography、difference_summary、image_prompt。产品名称必须进入包装信息层级和 image_prompt 的设计考虑。每套方案必须体现不同的设计策略，不要生成具体品牌侵权元素。如果输入包含受保护的品牌 Logo，必须在每套 image_prompt 中要求使用客户上传的原始 Logo，禁止对 Logo 做任何调整。`,
		Messages:     []Message{{Role: "user", Content: fmt.Sprintf("生成 %d 套方案。%s %s Brief：%s", count, productNameRule, logoRule, brief)}},
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

// LLMFusion turns customer-confirmed source decisions into one coherent,
// production-aware proposal. It deliberately receives structured source data
// so the model cannot silently lose the provenance of a selected element.
type LLMFusion struct{ provider LLMProvider }

func NewLLMFusion(provider LLMProvider) *LLMFusion { return &LLMFusion{provider: provider} }
func (a *LLMFusion) Name() string                  { return "fusion" }

func (a *LLMFusion) Run(ctx context.Context, input RunInput) (RunOutput, error) {
	data, err := json.Marshal(input.Payload)
	if err != nil {
		return RunOutput{}, err
	}
	result, err := a.provider.Chat(ctx, ChatRequest{
		SystemPrompt: `你是包装设计融合总监。请依据多个已有方案、客户确认的保留/调整/排除项、权重和生产约束，整合成一套统一的新方案。每个视觉决策必须服务于同一个品牌叙事，避免元素堆叠。只输出 JSON，字段必须包含 product_name、title、concept、design_strategy（对象）、color_palette（字符串数组）、typography、difference_summary、image_prompt。product_name 要继承或平衡来源方案中的产品名称，并纳入包装文字层级和 image_prompt 设计考虑。difference_summary 要说明采用了哪些来源方案优点以及做了哪些平衡。如果 Brief 中包含受保护的品牌 Logo，必须原样使用客户上传的 Logo，禁止重绘、变形、改色、描边、重新排版、裁切、加滤镜或替换。`,
		Messages:     []Message{{Role: "user", Content: string(data)}},
		Temperature:  0.65,
	})
	if err != nil {
		return RunOutput{}, err
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(result.Content), &payload); err != nil {
		return RunOutput{}, fmt.Errorf("decode fusion JSON: %w", err)
	}
	if _, ok := payload["title"].(string); !ok {
		return RunOutput{}, fmt.Errorf("fusion output missing title")
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
		SystemPrompt: `你是包装设计迭代 Agent。基于原方案和客户反馈生成一套二轮修改方案。保留客户明确认可的产品名称和视觉元素，只修改反馈要求的部分。只输出 JSON，字段与单套 proposal 相同，并包含 product_name：product_name、title、concept、design_strategy、color_palette、typography、difference_summary、image_prompt。产品名称必须继续纳入包装文字层级和 image_prompt 设计考虑。如果 Brief 中包含受保护的品牌 Logo，必须原样使用客户上传的 Logo，禁止重绘、变形、改色、描边、重新排版、裁切、加滤镜或替换。`,
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
