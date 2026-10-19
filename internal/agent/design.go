package agent

import (
	"context"
	"encoding/json"
	"fmt"
)

const genericProductionSceneRule = `包装设计必须局限于真实、可生产的包装实体，但不得预设行业、产品、材料、尺寸或包装类型。必须根据 Brief 中的行业、产品类型、用途、包装形态、数量、尺寸、容量、材料、生产工艺、销售场景、法规文字和品牌调性动态设计。包装形态可以是盒、瓶、罐、袋、管、标签、套盒、卡盒、运输箱或组合包装；应根据输入选择主展示面、结构、比例和工艺。每张方案图必须展示 Brief 中定义的完整包装组合：不同包装规格、容量、组件和包装层级都要作为真实实体出现，并保持同一产品系统的统一设计、材质和品牌层级。各实体应有清晰可见的展示面，合理排列且互不遮挡，不得遗漏 Brief 中要求的规格；不得加入 Brief 未要求的无关产品。image_prompt 必须描述真实包装产品组合摄影，不要生成设计说明板、九宫格、拼贴画布或漂浮图形。`

// genericBrandMarkRule is intentionally independent of any particular
// industry, product, package shape or material. It describes the observable
// treatment that makes a protected brand mark look physically applied to its
// target surface while keeping the source artwork authoritative.
const genericBrandMarkRule = `如果输入包含客户上传的品牌标识原文件（包括 Logo、字标、图形标志或组合标志），该标识是不可修改的品牌资产：必须保持图形结构、字体、笔画、文字内容、比例、字距和排列关系完全一致，禁止重绘、仿制、简化、变形、拉伸、压缩、裁切、拆分、重排、替换字体或生成相似标志。品牌标识应附着在最终展示物的真实可见表面，并根据该表面的平面或曲面、透视、尺度、边缘、接缝、安全距离、遮挡关系和观看角度自然放置。标识可以呈现印刷、专色、烫印、丝印、压凹凸、浮雕、蚀刻、雕刻、刺绣、喷涂、贴标或其他由 Brief 决定的实体工艺，但工艺只能改变表面表现，不能改变标识设计本身。图像生成时会收到客户原始品牌标识作为权威参考图；必须在同一次生成中直接使用该原始标识，保持其结构、字形、颜色关系和注册标记，并让标识与目标表面的透视、材质纹理、粗糙度、反射、高光、阴影、遮挡和局部光线一致，不能像水印、贴纸、透明 PNG 或悬浮图形。不得在背景、画布、说明板、展示台、空气或其他非目标表面生成额外品牌标识、印章或 Logo-like 图形。

如果输入没有品牌标识原文件，不得虚构品牌标识或相似标志，只预留品牌识别区域。Brief 指定的品牌名称、产品名称、规格、容量、型号、功能文字和法规文字必须逐字保留，不得翻译、改写、缩写、增删字符或替换同义词。`

const genericDesignRule = genericProductionSceneRule + "\n" + genericBrandMarkRule

type LLMConcept struct{ provider LLMProvider }

func NewLLMConcept(provider LLMProvider) *LLMConcept { return &LLMConcept{provider: provider} }
func (a *LLMConcept) Name() string                   { return "concept" }

func (a *LLMConcept) Run(ctx context.Context, input RunInput) (RunOutput, error) {
	brief, err := json.Marshal(input.Payload["brief"])
	if err != nil {
		return RunOutput{}, err
	}
	count := proposalCount(input.Payload["proposal_count"])
	productName, _ := input.Payload["product_name"].(string)
	productNameRule := "未指定产品名称：请为每套方案生成一个简洁、可用且彼此不同的 product_name。"
	if productName != "" {
		productNameRule = fmt.Sprintf("已指定产品名称“%s”：每套方案的 product_name 以及 image_prompt、包装文字和所有需要显示名称的内容都必须逐字使用这个名称，不得改写、翻译、缩写、增删字符、添加后缀或替换同义词。", productName)
	}
	logoRule := "未上传品牌标识原文件。"
	if assets, ok := input.Payload["protected_logo_assets"].([]map[string]any); ok && len(assets) > 0 {
		logoRule = "客户上传了受保护的品牌标识原文件，图像生成时会作为权威参考图一并输入，而且它是 Logo 图形和 Logo 内部文字的唯一来源。请在真实包装实体表面直接生成该原始 Logo，保持每个字形、字符、固定副标题、结构、比例、颜色关系和注册标记；不得用 Brief 中的品牌名、产品名或系列名替换 Logo 内部文字，无法准确生成时宁可保留空白区域，也不得重绘、替换、复制或虚构其他 Logo；应匹配目标表面的透视、材质、工艺、光影和遮挡。"
	}
	result, err := a.provider.Chat(ctx, ChatRequest{
		SystemPrompt: `你是专业的产品包装创意总监。根据 Brief 生成多套差异明显、可执行的包装设计方向。只输出 JSON：proposals 是数组，每项包含 product_name、title、concept、design_strategy（对象）、color_palette（字符串数组）、typography、difference_summary、image_prompt。指定产品名称时，所有相关输出和 image_prompt 必须逐字使用指定名称，不得改写、翻译、缩写、增删字符、添加后缀或替换同义词。每套方案必须体现不同的设计策略，不要生成具体品牌侵权元素。` + genericDesignRule,
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

// proposalCount accepts both native Go integers and numbers decoded from the
// JSON task payload. Asynq unmarshals map[string]any numbers as float64, so a
// type assertion to int silently fell back to the default batch size.
func proposalCount(value any) int {
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
	if count < 1 || count > 6 {
		return 3
	}
	return count
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
		SystemPrompt: `你是包装设计融合总监。请依据多个已有方案、客户确认的保留/调整/排除项、权重和生产约束，整合成一套统一的新方案。每个视觉决策必须服务于同一个品牌叙事，避免元素堆叠。只输出 JSON，字段必须包含 product_name、title、concept、design_strategy（对象）、color_palette（字符串数组）、typography、difference_summary、image_prompt。product_name 要继承或平衡来源方案中的产品名称，并纳入包装文字层级和 image_prompt 设计考虑。如果 Brief 或来源方案中存在指定产品名称，必须逐字保留，不得改写、翻译、缩写、增删字符、添加后缀或替换同义词。difference_summary 要说明采用了哪些来源方案优点以及做了哪些平衡。` + genericDesignRule,
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
		SystemPrompt: `你是包装设计迭代 Agent。基于原方案和客户反馈生成一套修改方案。保留客户明确认可的产品名称和视觉元素，只修改反馈要求的部分。只输出 JSON，字段与单套 proposal 相同，并包含 product_name、title、concept、design_strategy、color_palette、typography、difference_summary、image_prompt。产品名称必须继续纳入包装文字层级和 image_prompt 设计考虑；如果原方案存在指定产品名称，必须逐字保留，不得改写、翻译、缩写、增删字符、添加后缀或替换同义词。` + genericDesignRule,
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
