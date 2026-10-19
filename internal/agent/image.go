package agent

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
	"time"
)

type OpenAICompatibleImage struct {
	baseURL, apiKey, model string
	client                 *http.Client
}

func NewOpenAICompatibleImage(baseURL, apiKey, model string) *OpenAICompatibleImage {
	return &OpenAICompatibleImage{baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, model: model, client: &http.Client{Timeout: 300 * time.Second}}
}

func (p *OpenAICompatibleImage) Generate(ctx context.Context, request ImageRequest) (ImageResponse, error) {
	if len(request.ReferenceImages) > 0 {
		return p.editWithReferences(ctx, request)
	}
	bodyPayload := map[string]any{"model": p.model, "prompt": request.Prompt, "negative_prompt": request.Negative, "size": fmt.Sprintf("%dx%d", request.Width, request.Height), "n": 1}
	if len(request.ReferenceIDs) > 0 {
		// Keep the protected source asset IDs in the provider request so an
		// image adapter can resolve them as reference images without changing
		// the generic ImageProvider interface.
		bodyPayload["reference_asset_ids"] = request.ReferenceIDs
	}
	if len(request.ReferenceImages) > 0 {
		bodyPayload["reference_images"] = request.ReferenceImages
	}
	body, err := json.Marshal(bodyPayload)
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

// editWithReferences uses the OpenAI-compatible image edits contract. The
// custom reference_images JSON field is not part of that contract and may be
// silently ignored by a gateway, which would let the model replace a protected
// Logo with ordinary product text.
func (p *OpenAICompatibleImage) editWithReferences(ctx context.Context, request ImageRequest) (ImageResponse, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("model", p.model)
	_ = writer.WriteField("prompt", request.Prompt)
	if request.Negative != "" {
		_ = writer.WriteField("negative_prompt", request.Negative)
	}
	_ = writer.WriteField("size", fmt.Sprintf("%dx%d", request.Width, request.Height))
	_ = writer.WriteField("n", "1")
	// GPT Image 1/1.5 support high-fidelity input preservation for edits.
	_ = writer.WriteField("input_fidelity", "high")
	for index, reference := range request.ReferenceImages {
		data, contentType, err := decodeImageReference(reference)
		if err != nil {
			return ImageResponse{}, fmt.Errorf("decode logo reference %d: %w", index, err)
		}
		extension := extensionForMIME(contentType)
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="image[]"; filename="reference-%d%s"`, index+1, extension))
		header.Set("Content-Type", contentType)
		part, err := writer.CreatePart(header)
		if err != nil {
			return ImageResponse{}, err
		}
		if _, err := part.Write(data); err != nil {
			return ImageResponse{}, err
		}
	}
	if err := writer.Close(); err != nil {
		return ImageResponse{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/images/edits", &body)
	if err != nil {
		return ImageResponse{}, err
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return p.decodeImageResponse(req)
}

func decodeImageReference(reference string) ([]byte, string, error) {
	if strings.HasPrefix(reference, "data:") {
		comma := strings.IndexByte(reference, ',')
		if comma < 0 {
			return nil, "", fmt.Errorf("invalid data URL")
		}
		meta := strings.TrimPrefix(reference[:comma], "data:")
		metaParts := strings.Split(meta, ";")
		contentType := strings.TrimSpace(metaParts[0])
		encoded := false
		for _, part := range metaParts[1:] {
			if strings.EqualFold(strings.TrimSpace(part), "base64") {
				encoded = true
			}
		}
		if encoded {
			data, err := base64.StdEncoding.DecodeString(reference[comma+1:])
			return data, contentType, err
		}
		data, err := url.PathUnescape(reference[comma+1:])
		return []byte(data), contentType, err
	}
	return nil, "", fmt.Errorf("reference must be a data URL")
}

func extensionForMIME(contentType string) string {
	switch strings.ToLower(contentType) {
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	default:
		return ".png"
	}
}

func (p *OpenAICompatibleImage) decodeImageResponse(req *http.Request) (ImageResponse, error) {
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
	assetURL := result.Data[0].URL
	if assetURL == "" {
		assetURL = "data:image/png;base64," + result.Data[0].B64
	}
	return ImageResponse{Provider: "openai-compatible", Model: p.model, AssetURL: assetURL, Width: 1024, Height: 1024}, nil
}
