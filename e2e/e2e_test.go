package e2e

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestProjectChatPersistsBrief(t *testing.T) {
	baseURL := os.Getenv("E2E_BASE_URL")
	if baseURL == "" {
		t.Skip("set E2E_BASE_URL to run Docker end-to-end tests")
	}
	client := &http.Client{Timeout: 10 * time.Second}
	var authResponse struct {
		Token string `json:"token"`
	}
	requestJSON(t, client, http.MethodPost, baseURL+"/api/v1/auth/register", map[string]any{"email": fmt.Sprintf("e2e-%d@example.com", time.Now().UnixNano()), "password": "password123"}, &authResponse)
	if authResponse.Token == "" {
		t.Fatal("auth token is empty")
	}
	os.Setenv("E2E_TOKEN", authResponse.Token)
	defer os.Unsetenv("E2E_TOKEN")

	var project struct {
		ID string `json:"id"`
	}
	requestJSON(t, client, http.MethodPost, baseURL+"/api/v1/projects", map[string]any{"name": "E2E 红茶包装"}, &project)
	if project.ID == "" {
		t.Fatal("project id is empty")
	}
	var secondProject struct {
		ID string `json:"id"`
	}
	requestJSON(t, client, http.MethodPost, baseURL+"/api/v1/projects", map[string]any{"name": "E2E 第二个项目"}, &secondProject)
	var projectList []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	requestJSON(t, client, http.MethodGet, baseURL+"/api/v1/projects", nil, &projectList)
	if len(projectList) < 2 {
		t.Fatalf("project list has %d projects, want at least 2", len(projectList))
	}

	var task struct {
		TaskID string `json:"task_id"`
	}
	requestMultipartFiles(t, client, baseURL+"/api/v1/projects/"+project.ID+"/chat", "做一套年轻人红茶礼盒包装", [][]byte{makeDocx(t), makeDocx(t)}, []namedFile{{name: "brand-one.svg", data: validLogoSVG("#b5302e")}, {name: "brand-two.svg", data: validLogoSVG("#c58d42")}}, &task)
	if task.TaskID == "" {
		t.Fatal("task id is empty")
	}

	var taskStatus struct {
		Status string `json:"status"`
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		requestJSON(t, client, http.MethodGet, baseURL+"/api/v1/tasks/"+task.TaskID, nil, &taskStatus)
		if taskStatus.Status == "succeeded" {
			break
		}
		if taskStatus.Status == "failed" {
			t.Fatalf("task failed")
		}
		time.Sleep(500 * time.Millisecond)
	}
	if taskStatus.Status != "succeeded" {
		t.Fatalf("task did not complete: %s", taskStatus.Status)
	}

	var brief struct {
		CompletionScore int            `json:"completion_score"`
		ProductInfo     map[string]any `json:"product_info"`
	}
	requestJSON(t, client, http.MethodGet, baseURL+"/api/v1/projects/"+project.ID+"/brief", nil, &brief)
	if brief.CompletionScore != 100 {
		t.Fatalf("completion score = %d, want 100", brief.CompletionScore)
	}
	if brief.ProductInfo["product_name"] != "E2E测试红茶" {
		t.Fatalf("product_name not persisted: %#v", brief.ProductInfo)
	}

	var generationTask struct {
		TaskID string `json:"task_id"`
	}
	requestJSON(t, client, http.MethodPost, baseURL+"/api/v1/projects/"+project.ID+"/generate", map[string]any{"proposal_count": 3, "product_name": "松萝茶·山岚玉青"}, &generationTask)
	pollSucceeded(t, client, baseURL, generationTask.TaskID)
	var proposals []struct {
		ID          string `json:"id"`
		Round       int    `json:"round"`
		ProductName string `json:"product_name"`
		FusionID    string `json:"fusion_id"`
		Assets      []struct {
			ID         string `json:"id"`
			StorageURL string `json:"storage_url"`
			Prompt     string `json:"prompt"`
			Provider   string `json:"provider"`
		} `json:"assets"`
	}
	requestJSON(t, client, http.MethodGet, baseURL+"/api/v1/projects/"+project.ID+"/proposals", nil, &proposals)
	if len(proposals) != 3 || proposals[0].Round != 1 || proposals[0].ProductName != "松萝茶·山岚玉青" || proposals[1].ProductName != "松萝茶·山岚玉青" || proposals[2].ProductName != "松萝茶·山岚玉青" || len(proposals[0].Assets) == 0 {
		t.Fatalf("unexpected first round proposals: %#v", proposals)
	}
	if !bytes.Contains([]byte(proposals[0].Assets[0].Provider), []byte("logo-in-generation")) || !bytes.Contains([]byte(proposals[0].Assets[0].Prompt), []byte("authoritative visual reference")) || !bytes.Contains([]byte(proposals[0].Assets[0].Prompt), []byte("specified physical finish")) {
		t.Fatalf("protected Logo path was not applied: provider=%q prompt=%q", proposals[0].Assets[0].Provider, proposals[0].Assets[0].Prompt)
	}
	var fusion struct {
		ID                string   `json:"id"`
		Status            string   `json:"status"`
		SourceProposalIDs []string `json:"source_proposal_ids"`
	}
	requestJSON(t, client, http.MethodPost, baseURL+"/api/v1/projects/"+project.ID+"/fusions", map[string]any{
		"name":                "自然东方 × 高端礼赠融合版",
		"source_proposal_ids": []string{proposals[0].ID, proposals[1].ID},
		"source_features":     map[string]any{"proposals": []string{proposals[0].ID, proposals[1].ID}},
		"feature_selections":  []map[string]any{{"dimension": "strategy", "value": "保留自然东方叙事", "source_proposal_id": proposals[0].ID, "decision": "keep", "weight": 90}},
		"weights":             map[string]float64{"strategy": 0.4, "visual": 0.35, "production": 0.25},
		"constraints":         []string{"控制装饰密度"},
	}, &fusion)
	if fusion.ID == "" || len(fusion.SourceProposalIDs) != 2 || fusion.Status != "draft" {
		t.Fatalf("unexpected fusion: %#v", fusion)
	}
	var fusionTask struct {
		TaskID string `json:"task_id"`
	}
	requestJSON(t, client, http.MethodPost, baseURL+"/api/v1/projects/"+project.ID+"/fusions/"+fusion.ID+"/generate", nil, &fusionTask)
	pollSucceeded(t, client, baseURL, fusionTask.TaskID)
	var fusionAfter struct {
		Status            string   `json:"status"`
		OutputProposalIDs []string `json:"output_proposal_ids"`
	}
	requestJSON(t, client, http.MethodGet, baseURL+"/api/v1/projects/"+project.ID+"/fusions/"+fusion.ID, nil, &fusionAfter)
	if fusionAfter.Status != "completed" || len(fusionAfter.OutputProposalIDs) != 1 {
		t.Fatalf("fusion did not complete: %#v", fusionAfter)
	}
	requestJSON(t, client, http.MethodGet, baseURL+"/api/v1/projects/"+project.ID+"/proposals?round=2", nil, &proposals)
	if len(proposals) != 1 || proposals[0].FusionID != fusion.ID || len(proposals[0].Assets) == 0 {
		t.Fatalf("unexpected fusion proposal: %#v", proposals)
	}

	proposalID := proposals[0].ID
	requestJSON(t, client, http.MethodPost, baseURL+"/api/v1/proposals/"+proposalID+"/feedback", map[string]any{"rating": 4, "comment": "保留主色，减少插画复杂度并突出低糖卖点"}, nil)
	var revisionTask struct {
		TaskID string `json:"task_id"`
	}
	requestMultipartRevision(t, client, baseURL+"/api/v1/projects/"+project.ID+"/revision", proposalID, map[string]any{"rating": 4, "comment": "减少插画复杂度并突出低糖卖点"}, []namedFile{{name: "feedback-reference.png", data: []byte{1, 2, 3}}}, &revisionTask)
	pollSucceeded(t, client, baseURL, revisionTask.TaskID)
	requestJSON(t, client, http.MethodGet, baseURL+"/api/v1/projects/"+project.ID+"/proposals?round=3", nil, &proposals)
	if len(proposals) != 1 || proposals[0].Round != 3 || len(proposals[0].Assets) == 0 {
		t.Fatalf("unexpected revision proposals: %#v", proposals)
	}
	assetResponse := requestBytes(t, client, http.MethodGet, baseURL+"/api/v1/assets/"+proposals[0].Assets[0].ID)
	if len(assetResponse) == 0 {
		t.Fatal("asset download is empty")
	}
	requestJSON(t, client, http.MethodPost, baseURL+"/api/v1/projects/"+project.ID+"/export", map[string]any{"formats": []string{"pdf", "markdown"}}, nil)
	pdf := requestBytes(t, client, http.MethodGet, baseURL+"/api/v1/projects/"+project.ID+"/export?format=pdf")
	if len(pdf) < 5 || string(pdf[:5]) != "%PDF-" {
		t.Fatalf("invalid PDF export header: %q", pdf[:min(10, len(pdf))])
	}
	if !bytes.Contains(pdf, []byte("/FontFile2")) || bytes.Count(pdf, []byte("/Subtype /Image")) < 1 {
		t.Fatalf("PDF does not contain embedded Chinese font and image")
	}
	markdown := requestBytes(t, client, http.MethodGet, baseURL+"/api/v1/projects/"+project.ID+"/export?format=markdown")
	if !bytes.Contains(markdown, []byte("设计 Brief")) {
		t.Fatal("markdown report is missing Brief section")
	}
	bundle := requestBytes(t, client, http.MethodGet, baseURL+"/api/v1/projects/"+project.ID+"/export?format=zip")
	if len(bundle) < 4 || string(bundle[:4]) != "PK\x03\x04" {
		t.Fatalf("invalid bundle export header: %q", bundle[:min(4, len(bundle))])
	}
}

func validLogoSVG(color string) []byte {
	return []byte(fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="240" height="120" viewBox="0 0 240 120"><rect width="240" height="120" fill="none"/><rect x="16" y="12" width="72" height="96" fill="%s"/><path d="M28 28h48v12H40v16h28v12H40v20h36v12H28z" fill="#fff"/><path d="M112 45h112v10H112zM112 75h82v10H112z" fill="#222"/></svg>`, color))
}

func pollSucceeded(t *testing.T, client *http.Client, baseURL, taskID string) {
	t.Helper()
	var taskStatus struct {
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		requestJSON(t, client, http.MethodGet, baseURL+"/api/v1/tasks/"+taskID, nil, &taskStatus)
		if taskStatus.Status == "succeeded" {
			return
		}
		if taskStatus.Status == "failed" {
			t.Fatalf("task failed: %s", taskStatus.Error)
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("task did not complete: %s", taskStatus.Status)
}

func requestJSON(t *testing.T, client *http.Client, method, url string, body any, out any) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token := os.Getenv("E2E_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		t.Fatalf("%s %s: status=%d body=%s", method, url, resp.StatusCode, data)
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			t.Fatal(fmt.Errorf("decode %s: %w", url, err))
		}
	}
}

func requestBytes(t *testing.T, client *http.Client, method, url string) []byte {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if token := os.Getenv("E2E_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		t.Fatalf("%s %s: status=%d body=%s", method, url, resp.StatusCode, data)
	}
	return data
}

func requestMultipart(t *testing.T, client *http.Client, url, message string, document []byte, out any) {
	requestMultipartFiles(t, client, url, message, [][]byte{document}, nil, out)
}

type namedFile struct {
	name string
	data []byte
}

func requestMultipartFiles(t *testing.T, client *http.Client, url, message string, documents [][]byte, brandFiles []namedFile, out any) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("message", message)
	for index, document := range documents {
		part, err := writer.CreateFormFile("document", fmt.Sprintf("brief-%d.docx", index+1))
		if err != nil {
			t.Fatal(err)
		}
		_, _ = part.Write(document)
	}
	for _, brandFile := range brandFiles {
		part, err := writer.CreateFormFile("brand_file", brandFile.name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = part.Write(brandFile.data)
	}
	_ = writer.Close()
	req, err := http.NewRequest(http.MethodPost, url, &body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if token := os.Getenv("E2E_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		t.Fatalf("multipart status=%d body=%s", resp.StatusCode, data)
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			t.Fatal(err)
		}
	}
}

func requestMultipartRevision(t *testing.T, client *http.Client, url, proposalID string, feedback map[string]any, references []namedFile, out any) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("selected_proposal_id", proposalID)
	feedbackData, _ := json.Marshal(feedback)
	_ = writer.WriteField("feedback", string(feedbackData))
	_ = writer.WriteField("preserve", `[]`)
	for _, reference := range references {
		part, err := writer.CreateFormFile("reference_file", reference.name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = part.Write(reference.data)
	}
	_ = writer.Close()
	req, err := http.NewRequest(http.MethodPost, url, &body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if token := os.Getenv("E2E_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		t.Fatalf("revision multipart status=%d body=%s", resp.StatusCode, data)
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			t.Fatal(err)
		}
	}
}

func makeDocx(t *testing.T) []byte {
	t.Helper()
	var body bytes.Buffer
	writer := zip.NewWriter(&body)
	file, err := writer.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.Write([]byte(`<w:document xmlns:w="x"><w:body><w:p><w:r><w:t>详细包装需求文档：突出低糖卖点</w:t></w:r></w:p></w:body></w:document>`))
	image, _ := writer.Create("word/media/image1.png")
	_, _ = image.Write([]byte{1, 2, 3})
	_ = writer.Close()
	return body.Bytes()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
