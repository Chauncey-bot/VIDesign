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

type OpenAICompatibleImage struct {
	baseURL, apiKey, model string
	client                 *http.Client
}

func NewOpenAICompatibleImage(baseURL, apiKey, model string) *OpenAICompatibleImage {
	return &OpenAICompatibleImage{baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, model: model, client: &http.Client{Timeout: 120 * time.Second}}
}

func (p *OpenAICompatibleImage) Generate(ctx context.Context, request ImageRequest) (ImageResponse, error) {
	body, err := json.Marshal(map[string]any{"model": p.model, "prompt": request.Prompt, "negative_prompt": request.Negative, "size": fmt.Sprintf("%dx%d", request.Width, request.Height), "n": 1})
	if err != nil {
		return ImageResponse{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/images/generations", bytes.NewReader(body))
	if err != nil {
		return ImageResponse{}, err
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return ImageResponse{}, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return ImageResponse{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ImageResponse{}, fmt.Errorf("image request failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var result struct {
		Data []struct {
			URL string `json:"url"`
			B64 string `json:"b64_json"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return ImageResponse{}, err
	}
	if len(result.Data) == 0 {
		return ImageResponse{}, fmt.Errorf("image response contains no data")
	}
	url := result.Data[0].URL
	if url == "" {
		url = "data:image/png;base64," + result.Data[0].B64
	}
	return ImageResponse{Provider: "openai-compatible", Model: p.model, AssetURL: url, Width: request.Width, Height: request.Height}, nil
}
