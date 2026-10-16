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
	requestMultipartFiles(t, client, baseURL+"/api/v1/projects/"+project.ID+"/chat", "做一套年轻人红茶礼盒包装", [][]byte{makeDocx(t), makeDocx(t)}, []namedFile{{name: "brand-one.ai", data: []byte("AI-ONE")}, {name: "brand-two.svg", data: []byte("<svg></svg>")}}, &task)
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
	requestJSON(t, client, http.MethodPost, baseURL+"/api/v1/projects/"+project.ID+"/generate", map[string]any{"proposal_count": 3}, &generationTask)
	pollSucceeded(t, client, baseURL, generationTask.TaskID)
	var proposals []struct {
		ID     string `json:"id"`
		Round  int    `json:"round"`
		Assets []struct {
			ID         string `json:"id"`
			StorageURL string `json:"storage_url"`
		} `json:"assets"`
	}
	requestJSON(t, client, http.MethodGet, baseURL+"/api/v1/projects/"+project.ID+"/proposals", nil, &proposals)
	if len(proposals) != 3 || proposals[0].Round != 1 || len(proposals[0].Assets) == 0 {
		t.Fatalf("unexpected first round proposals: %#v", proposals)
	}

	proposalID := proposals[0].ID
	requestJSON(t, client, http.MethodPost, baseURL+"/api/v1/proposals/"+proposalID+"/feedback", map[string]any{"rating": 4, "comment": "保留主色，减少插画复杂度并突出低糖卖点"}, nil)
	var revisionTask struct {
		TaskID string `json:"task_id"`
	}
	requestJSON(t, client, http.MethodPost, baseURL+"/api/v1/projects/"+project.ID+"/revision", map[string]any{"selected_proposal_id": proposalID, "feedback": map[string]any{"rating": 4, "comment": "减少插画复杂度并突出低糖卖点"}, "preserve": []string{"主色"}}, &revisionTask)
	pollSucceeded(t, client, baseURL, revisionTask.TaskID)
	requestJSON(t, client, http.MethodGet, baseURL+"/api/v1/projects/"+project.ID+"/proposals?round=2", nil, &proposals)
	if len(proposals) != 1 || proposals[0].Round != 2 || len(proposals[0].Assets) == 0 {
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
