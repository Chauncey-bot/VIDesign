package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestImageGenerationSendsLogoReferenceImages(t *testing.T) {
	var received map[string]string
	provider := NewOpenAICompatibleImage("http://image.test", "test-key", "test-image")
	provider.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/images/edits" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		received = map[string]string{"model": r.FormValue("model"), "prompt": r.FormValue("prompt"), "input_fidelity": r.FormValue("input_fidelity")}
		files := r.MultipartForm.File["image[]"]
		if len(files) != 1 || files[0].Filename != "reference-1.png" || files[0].Header.Get("Content-Type") != "image/png" {
			t.Fatalf("reference files = %#v", files)
		}
		file, err := files[0].Open()
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		data, err := io.ReadAll(file)
		if err != nil || string(data) != "abc" {
			t.Fatalf("reference bytes = %q, err=%v", data, err)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"data":[{"b64_json":"iVBORw0KGgo="}]}`)),
			Header:     http.Header{"Content-Type": []string{"application/json"}},
		}, nil
	})}
	if _, err := provider.Generate(context.Background(), ImageRequest{Prompt: "package", ReferenceIDs: []string{"logo-1"}, ReferenceImages: []string{"data:image/png;base64,YWJj"}, Width: 1024, Height: 1024}); err != nil {
		t.Fatal(err)
	}
	if received["model"] != "test-image" || received["prompt"] != "package" || received["input_fidelity"] != "high" {
		t.Fatalf("edit fields = %#v", received)
	}
}

func TestImageGenerationWithoutReferencesUsesJSONGenerationEndpoint(t *testing.T) {
	var received map[string]any
	provider := NewOpenAICompatibleImage("http://image.test", "test-key", "test-image")
	provider.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/images/generations" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatal(err)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"data":[{"b64_json":"iVBORw0KGgo="}]}`)), Header: http.Header{"Content-Type": []string{"application/json"}}}, nil
	})}
	if _, err := provider.Generate(context.Background(), ImageRequest{Prompt: "package", Width: 1024, Height: 1024}); err != nil {
		t.Fatal(err)
	}
	if received["prompt"] != "package" {
		t.Fatalf("generation fields = %#v", received)
	}
}
