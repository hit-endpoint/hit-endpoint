package snippet

import (
	"strings"
	"testing"
)

func TestGenerateSnippets(t *testing.T) {
	req := RequestInfo{
		Method: "POST",
		URL:    "http://127.0.0.1:8765/pets",
		Headers: map[string]string{
			"Authorization": "Bearer demo-token-123",
			"Content-Type":  "application/json",
		},
		Query: map[string]string{
			"verbose": "true",
		},
		Body:        []byte(`{"name":"Rex","kind":"dog","age":3}`),
		ExtractPath: "items[0].id",
	}

	// 1. Python
	pyCode, err := GenerateSnippet("python", req)
	if err != nil {
		t.Fatalf("unexpected error generating python snippet: %v", err)
	}
	if !strings.Contains(pyCode, "import requests") {
		t.Errorf("expected python snippet to import requests")
	}
	if !strings.Contains(pyCode, `requests.post(url, headers=headers, json=payload)`) {
		t.Errorf("expected python snippet to invoke requests.post")
	}
	if !strings.Contains(pyCode, `data["items"][0]["id"]`) {
		t.Errorf("expected python snippet to extract data[\"items\"][0][\"id\"], got: %s", pyCode)
	}

	// 2. JavaScript
	jsCode, err := GenerateSnippet("javascript", req)
	if err != nil {
		t.Fatalf("unexpected error generating js snippet: %v", err)
	}
	if !strings.Contains(jsCode, "await fetch(url, options)") {
		t.Errorf("expected js snippet to use fetch")
	}
	if !strings.Contains(jsCode, `data.items[0].id`) {
		t.Errorf("expected js snippet to extract data.items[0].id, got: %s", jsCode)
	}

	// 3. PHP
	phpCode, err := GenerateSnippet("php", req)
	if err != nil {
		t.Fatalf("unexpected error generating php snippet: %v", err)
	}
	if !strings.Contains(phpCode, "curl_init") {
		t.Errorf("expected php snippet to use curl_init")
	}
	if !strings.Contains(phpCode, `$data["items"][0]["id"] ?? null`) {
		t.Errorf("expected php snippet to extract $data[\"items\"][0][\"id\"] ?? null, got: %s", phpCode)
	}

	// 4. Go
	goCode, err := GenerateSnippet("go", req)
	if err != nil {
		t.Fatalf("unexpected error generating go snippet: %v", err)
	}
	if !strings.Contains(goCode, "http.NewRequest") {
		t.Errorf("expected go snippet to use http.NewRequest")
	}
	if !strings.Contains(goCode, `["items"]`) || !strings.Contains(goCode, `["id"]`) {
		t.Errorf("expected go snippet to extract element from data, got: %s", goCode)
	}

	// 5. GenerateAll
	allCode := GenerateAll(req)
	if !strings.Contains(allCode, "Python") || !strings.Contains(allCode, "JavaScript") ||
		!strings.Contains(allCode, "PHP") || !strings.Contains(allCode, "Go") {
		t.Errorf("expected GenerateAll to contain all 4 languages")
	}

	// 6. Unsupported language error
	_, err = GenerateSnippet("rust", req)
	if err == nil {
		t.Errorf("expected error for unsupported language, got nil")
	}
}
