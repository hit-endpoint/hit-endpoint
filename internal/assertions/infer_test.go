package assertions

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestInferFromResponse(t *testing.T) {
	status := 200
	elapsedMs := 42.0
	headers := map[string]string{
		"Content-Type": "application/json; charset=utf-8",
		"X-Server":     "test",
	}
	body := `{
		"id": 101,
		"uuid": "550e8400-e29b-41d4-a716-446655440000",
		"created_at": "2026-09-10T12:00:00Z",
		"email": "user@example.com",
		"status": "active",
		"verified": true,
		"total": 5,
		"items": [
			{"item_id": 1, "name": "Widget"}
		]
	}`

	opts := DefaultInferOptions()
	tests, yamlStr, err := InferFromResponse(status, elapsedMs, headers, body, opts)
	if err != nil {
		t.Fatalf("InferFromResponse error: %v", err)
	}

	if len(tests) == 0 {
		t.Fatalf("expected generated tests, got none")
	}

	if !strings.Contains(yamlStr, "status: 200") {
		t.Errorf("expected status: 200 in YAML, got:\n%s", yamlStr)
	}
	if !strings.Contains(yamlStr, "max_ms:") {
		t.Errorf("expected max_ms in YAML, got:\n%s", yamlStr)
	}
	if !strings.Contains(yamlStr, "content-type") {
		t.Errorf("expected content-type in YAML, got:\n%s", yamlStr)
	}
	if !strings.Contains(yamlStr, "matches: ^[0-9a-fA-F-]{36}$") {
		t.Errorf("expected UUID regex matcher in YAML, got:\n%s", yamlStr)
	}
	if !strings.Contains(yamlStr, "json.total >= length(json.items)") {
		t.Errorf("expected relational expr in YAML, got:\n%s", yamlStr)
	}

	// Self-verification:
	// Every single generated test must PASS when evaluated against the actual response!
	var parsedBody any
	_ = json.Unmarshal([]byte(body), &parsedBody)
	respMap := map[string]any{
		"status":  status,
		"ms":      elapsedMs,
		"headers": headers,
		"text":    body,
		"json":    parsedBody,
	}

	results := Evaluate(tests, respMap)
	if len(results) == 0 {
		t.Fatalf("expected test results, got none")
	}

	for _, r := range results {
		if !r.Passed {
			t.Errorf("synthesized test %q FAILED: detail=%s", r.Name, r.Detail)
		}
	}
}

func TestInferArrayRoot(t *testing.T) {
	status := 200
	elapsedMs := 15.0
	headers := map[string]string{"Content-Type": "application/json"}
	body := `[
		{"id": 1, "name": "Alpha"},
		{"id": 2, "name": "Beta"}
	]`

	opts := DefaultInferOptions()
	tests, _, err := InferFromResponse(status, elapsedMs, headers, body, opts)
	if err != nil {
		t.Fatalf("InferFromResponse error: %v", err)
	}

	var parsedBody any
	_ = json.Unmarshal([]byte(body), &parsedBody)
	respMap := map[string]any{
		"status":  status,
		"ms":      elapsedMs,
		"headers": headers,
		"text":    body,
		"json":    parsedBody,
	}

	results := Evaluate(tests, respMap)
	for _, r := range results {
		if !r.Passed {
			t.Errorf("array root synthesized test %q FAILED: %s", r.Name, r.Detail)
		}
	}
}

func TestInferHTMLAndPlainText(t *testing.T) {
	// HTML
	htmlBody := `<!DOCTYPE html><html><head><title>Dashboard</title></head><body>Welcome</body></html>`
	htmlHeaders := map[string]string{"Content-Type": "text/html"}
	htmlTests, _, err := InferFromResponse(200, 30.0, htmlHeaders, htmlBody, DefaultInferOptions())
	if err != nil {
		t.Fatalf("InferFromResponse html error: %v", err)
	}

	htmlResp := map[string]any{
		"status":  200,
		"ms":      30.0,
		"headers": htmlHeaders,
		"text":    htmlBody,
	}
	for _, r := range Evaluate(htmlTests, htmlResp) {
		if !r.Passed {
			t.Errorf("html test %q FAILED: %s", r.Name, r.Detail)
		}
	}

	// Plain text
	textBody := "OK"
	textHeaders := map[string]string{"Content-Type": "text/plain"}
	textTests, _, err := InferFromResponse(200, 10.0, textHeaders, textBody, DefaultInferOptions())
	if err != nil {
		t.Fatalf("InferFromResponse text error: %v", err)
	}

	textResp := map[string]any{
		"status":  200,
		"ms":      10.0,
		"headers": textHeaders,
		"text":    textBody,
	}
	for _, r := range Evaluate(textTests, textResp) {
		if !r.Passed {
			t.Errorf("plain text test %q FAILED: %s", r.Name, r.Detail)
		}
	}
}

func TestInferStrictMode(t *testing.T) {
	body := `{"count": 42, "role": "admin", "enabled": true}`
	opts := InferOptions{
		Strict:         true,
		IncludeLatency: false,
		IncludeHeaders: false,
	}

	tests, yamlStr, err := InferFromResponse(200, 0, nil, body, opts)
	if err != nil {
		t.Fatalf("Infer error: %v", err)
	}

	if !strings.Contains(yamlStr, "count: 42") {
		t.Errorf("expected count: 42 in strict mode, got:\n%s", yamlStr)
	}
	if !strings.Contains(yamlStr, "enabled: true") {
		t.Errorf("expected enabled: true in strict mode, got:\n%s", yamlStr)
	}

	var parsed any
	_ = json.Unmarshal([]byte(body), &parsed)
	respMap := map[string]any{
		"status": 200,
		"json":   parsed,
		"text":   body,
	}
	for _, r := range Evaluate(tests, respMap) {
		if !r.Passed {
			t.Errorf("strict test %q FAILED: %s", r.Name, r.Detail)
		}
	}
}
