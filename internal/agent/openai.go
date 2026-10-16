package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type OpenAICompatible struct {
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
}

func NewOpenAICompatible(baseURL, apiKey, model string) (*OpenAICompatible, error) {
	if strings.TrimSpace(baseURL) == "" || strings.TrimSpace(apiKey) == "" || strings.TrimSpace(model) == "" {
		return nil, fmt.Errorf("LLM base URL, API key and model are required")
	}
	return &OpenAICompatible{baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, model: model, client: &http.Client{Timeout: 90 * time.Second}}, nil
}

func (p *OpenAICompatible) Chat(ctx context.Context, request ChatRequest) (ChatResponse, error) {
	messages := make([]map[string]any, 0, len(request.Messages)+1)
	if request.SystemPrompt != "" {
		messages = append(messages, map[string]any{"role": "system", "content": request.SystemPrompt})
	}
	for _, message := range request.Messages {
		if len(message.ImageURLs) == 0 {
			messages = append(messages, map[string]any{"role": message.Role, "content": message.Content})
			continue
		}
		content := []map[string]any{{"type": "text", "text": message.Content}}
		for _, imageURL := range message.ImageURLs {
			content = append(content, map[string]any{"type": "image_url", "image_url": map[string]string{"url": imageURL}})
		}
		messages = append(messages, map[string]any{"role": message.Role, "content": content})
	}
	body := map[string]any{"model": p.model, "messages": messages, "temperature": request.Temperature, "response_format": map[string]string{"type": "json_object"}}
	data, err := json.Marshal(body)
	if err != nil {
		return ChatResponse{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return ChatResponse{}, err
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return ChatResponse{}, err
	}
	defer resp.Body.Close()
	responseData, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return ChatResponse{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ChatResponse{}, fmt.Errorf("LLM request failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(responseData)))
	}
	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
		Model string `json:"model"`
	}
	if err := json.Unmarshal(responseData, &result); err != nil {
		return ChatResponse{}, fmt.Errorf("decode LLM response: %w", err)
	}
	if len(result.Choices) == 0 {
		return ChatResponse{}, fmt.Errorf("LLM response contains no choices")
	}
	model := result.Model
	if model == "" {
		model = p.model
	}
	return ChatResponse{Content: result.Choices[0].Message.Content, Provider: "openai-compatible", Model: model, InputTokens: result.Usage.PromptTokens, OutputTokens: result.Usage.CompletionTokens}, nil
}

type LLMIntake struct{ provider LLMProvider }

func NewLLMIntake(provider LLMProvider) *LLMIntake { return &LLMIntake{provider: provider} }

func (a *LLMIntake) Name() string { return "intake" }

func (a *LLMIntake) Run(ctx context.Context, input RunInput) (RunOutput, error) {
	message, _ := input.Payload["message"].(string)
	briefJSON, err := json.Marshal(input.Payload["brief"])
	if err != nil {
		return RunOutput{}, err
	}
	documentText, _ := input.Payload["document_text"].(string)
	imageURLs, _ := input.Payload["document_images"].([]string)
	userContent := fmt.Sprintf("已有 Brief：%s\n客户最新消息：%s", briefJSON, message)
	if documentText != "" {
		userContent += "\nWord 文档正文：\n" + documentText
	}
	result, err := a.provider.Chat(ctx, ChatRequest{
		SystemPrompt: `你是产品包装设计项目的需求采集 Agent。根据客户最新消息和已有 Brief，提取明确事实，不要臆测。只输出 JSON，不要 Markdown，字段必须是：brief_patch（对象）、assistant_message（字符串）、next_questions（字符串数组）、completion_score（0到100整数）。brief_patch 只放本轮新增或修正的字段。completion_score 根据产品品类、产品名称、目标客户、包装类型、包装必填文字、材质、包装规格的完整程度计算。`,
		Messages:     []Message{{Role: "user", Content: userContent, ImageURLs: imageURLs}},
		Temperature:  0.2,
	})
	if err != nil {
		return RunOutput{}, err
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(result.Content), &payload); err != nil {
		return RunOutput{}, fmt.Errorf("decode intake JSON: %w", err)
	}
	if _, ok := payload["brief_patch"].(map[string]any); !ok {
		return RunOutput{}, fmt.Errorf("intake output missing brief_patch")
	}
	return RunOutput{AgentName: a.Name(), Payload: payload, Usage: Usage{Provider: result.Provider, Model: result.Model, InputTokens: result.InputTokens, OutputTokens: result.OutputTokens}}, nil
}
