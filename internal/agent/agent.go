package agent

import "context"

// Agent 是所有领域 Agent 的统一抽象。输入输出必须是可审计的结构化数据。
type Agent interface {
	Name() string
	Run(ctx context.Context, input RunInput) (RunOutput, error)
}

type RunInput struct {
	ProjectID string         `json:"project_id"`
	Round     int            `json:"round"`
	Payload   map[string]any `json:"payload"`
}

type RunOutput struct {
	AgentName string         `json:"agent_name"`
	Payload   map[string]any `json:"payload"`
	Warnings  []string       `json:"warnings,omitempty"`
	Usage     Usage          `json:"usage"`
}

type Usage struct {
	Provider       string `json:"provider,omitempty"`
	Model          string `json:"model,omitempty"`
	InputTokens    int    `json:"input_tokens,omitempty"`
	OutputTokens   int    `json:"output_tokens,omitempty"`
	DurationMillis int64  `json:"duration_ms,omitempty"`
}

type LLMProvider interface {
	Chat(ctx context.Context, request ChatRequest) (ChatResponse, error)
}

type ChatRequest struct {
	SystemPrompt string         `json:"system_prompt"`
	Messages     []Message      `json:"messages"`
	JSONSchema   map[string]any `json:"json_schema,omitempty"`
	Temperature  float64        `json:"temperature,omitempty"`
}

type Message struct {
	Role      string   `json:"role"`
	Content   string   `json:"content"`
	ImageURLs []string `json:"image_urls,omitempty"`
}

type ChatResponse struct {
	Content      string `json:"content"`
	Provider     string `json:"provider"`
	Model        string `json:"model"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
}

type ImageProvider interface {
	Generate(ctx context.Context, request ImageRequest) (ImageResponse, error)
}

type ImageRequest struct {
	Prompt          string   `json:"prompt"`
	Negative        string   `json:"negative,omitempty"`
	ReferenceIDs    []string `json:"reference_asset_ids,omitempty"`
	ReferenceImages []string `json:"reference_images,omitempty"`
	Width           int      `json:"width,omitempty"`
	Height          int      `json:"height,omitempty"`
}

type ImageResponse struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	AssetURL string `json:"asset_url"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
}

// Domain Agent 类型。实际实现可分别放入同名文件。
type IntakeAgent interface{ Agent }
type BrandStrategyAgent interface{ Agent }
type ConceptAgent interface{ Agent }
type VisualGenerationAgent interface{ Agent }
type ComplianceAgent interface{ Agent }
type FeedbackAnalysisAgent interface{ Agent }
type RevisionAgent interface{ Agent }
