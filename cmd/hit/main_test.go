package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// captureOutput captures stdout and stderr while running f()
func captureOutput(f func() int) (int, string, string) {
	origStdout := os.Stdout
	origStderr := os.Stderr

	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()

	os.Stdout = wOut
	os.Stderr = wErr

	outC := make(chan string)
	errC := make(chan string)

	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, rOut)
		outC <- buf.String()
	}()

	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, rErr)
		errC <- buf.String()
	}()

	code := f()

	_ = wOut.Close()
	_ = wErr.Close()

	os.Stdout = origStdout
	os.Stderr = origStderr

	stdout := <-outC
	stderr := <-errC

	_ = rOut.Close()
	_ = rErr.Close()

	return code, stdout, stderr
}

func TestAdhocDefaultResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Custom-Header", "test-val")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"hello":"world"}`))
	}))
	defer srv.Close()

	// hit <url> defaults to full response (status line, headers, body)
	code, stdout, _ := captureOutput(func() int {
		return run([]string{"--no-color", "--no-history", srv.URL})
	})
	if code != 0 {
		t.Fatalf("expected code 0, got %d", code)
	}
	if !strings.Contains(stdout, "200 OK") {
		t.Errorf("expected 200 OK in response, got:\n%s", stdout)
	}
	if !strings.Contains(strings.ToLower(stdout), "x-custom-header: test-val") {
		t.Errorf("expected header in response, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, `"hello": "world"`) {
		t.Errorf("expected body in response, got:\n%s", stdout)
	}

	// hit GET <url>
	code, stdout, _ = captureOutput(func() int {
		return run([]string{"--no-color", "--no-history", "GET", srv.URL})
	})
	if code != 0 {
		t.Fatalf("expected code 0, got %d", code)
	}
	if !strings.Contains(stdout, "200 OK") {
		t.Errorf("expected 200 OK, got:\n%s", stdout)
	}
}

func TestAdhocBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	code, stdout, _ := captureOutput(func() int {
		return run([]string{"--no-color", "--no-history", "body", srv.URL})
	})
	if code != 0 {
		t.Fatalf("expected code 0, got %d", code)
	}
	trimmed := strings.TrimSpace(stdout)
	if !strings.Contains(trimmed, `"status": "ok"`) {
		t.Errorf("expected body, got: %s", stdout)
	}
	if strings.Contains(stdout, "HTTP/1.1") {
		t.Errorf("hit body should not print status line: %s", stdout)
	}
}

func TestAdhocCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/notfound" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// 200 OK
	code, stdout, _ := captureOutput(func() int {
		return run([]string{"--no-color", "--no-history", "code", srv.URL})
	})
	if code != 0 {
		t.Fatalf("expected code 0, got %d", code)
	}
	if strings.TrimSpace(stdout) != "200" {
		t.Errorf("expected '200', got: %q", stdout)
	}

	// 404 without explicit assertions should output 404 and exit 0 (informative query)
	code, stdout, _ = captureOutput(func() int {
		return run([]string{"--no-color", "--no-history", "code", srv.URL + "/notfound"})
	})
	if code != 0 {
		t.Fatalf("expected code 0 for query, got %d", code)
	}
	if strings.TrimSpace(stdout) != "404" {
		t.Errorf("expected '404', got: %q", stdout)
	}

	// Test via flag: hit <url> --code
	code, stdout, _ = captureOutput(func() int {
		return run([]string{"--no-color", "--no-history", srv.URL, "--code"})
	})
	if code != 0 {
		t.Fatalf("expected code 0, got %d", code)
	}
	if strings.TrimSpace(stdout) != "200" {
		t.Errorf("expected '200', got: %q", stdout)
	}
}

func TestAdhocTime(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	code, stdout, _ := captureOutput(func() int {
		return run([]string{"--no-color", "--no-history", "time", srv.URL})
	})
	if code != 0 {
		t.Fatalf("expected code 0, got %d", code)
	}
	trimmed := strings.TrimSpace(stdout)
	if !strings.HasSuffix(trimmed, "ms") && !strings.HasSuffix(trimmed, "s") {
		t.Errorf("expected time formatted with ms or s, got: %q", trimmed)
	}

	// Raw / ms flag
	code, stdout, _ = captureOutput(func() int {
		return run([]string{"--no-color", "--no-history", "time", srv.URL, "--raw"})
	})
	if code != 0 {
		t.Fatalf("expected code 0, got %d", code)
	}
	rawVal, err := strconv.ParseFloat(strings.TrimSpace(stdout), 64)
	if err != nil || rawVal <= 0 {
		t.Errorf("expected positive float for --raw, got: %q", stdout)
	}
}

func TestAdhocHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Test-Id", "xyz-123")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	code, stdout, _ := captureOutput(func() int {
		return run([]string{"--no-color", "--no-history", "headers", srv.URL})
	})
	if code != 0 {
		t.Fatalf("expected code 0, got %d", code)
	}
	if !strings.Contains(strings.ToLower(stdout), "x-test-id: xyz-123") {
		t.Errorf("expected header, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "HTTP/1.1 200 OK") {
		t.Errorf("expected status line, got:\n%s", stdout)
	}
}

func TestZoneCodeAndTimeFlags(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	code, stdout, _ := captureOutput(func() int {
		return run([]string{"-z", "../../examples/petstore-zone", "--var", "base_url=" + srv.URL, "--no-history", "run", "health", "--code"})
	})
	if code != 0 {
		t.Fatalf("expected code 0, got %d", code)
	}
	if strings.TrimSpace(stdout) != "200" {
		t.Errorf("expected '200', got: %q", stdout)
	}

	code, stdout, _ = captureOutput(func() int {
		return run([]string{"-z", "../../examples/petstore-zone", "--var", "base_url=" + srv.URL, "--no-history", "run", "health", "--time"})
	})
	if code != 0 {
		t.Fatalf("expected code 0, got %d", code)
	}
	trimmed := strings.TrimSpace(stdout)
	if !strings.HasSuffix(trimmed, "ms") && !strings.HasSuffix(trimmed, "s") {
		t.Errorf("expected time formatted with ms or s, got: %q", trimmed)
	}
}

func TestCmdLearn(t *testing.T) {
	code, stdout, _ := captureOutput(func() int {
		return run([]string{"--no-color", "learn"})
	})
	if code != 0 {
		t.Fatalf("expected code 0, got %d", code)
	}
	if !strings.Contains(stdout, "Learning Sandbox") {
		t.Errorf("expected Learning Sandbox in output, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "learn/glossary.md") {
		t.Errorf("expected glossary.md reference, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "learn/01-http-basics.md") {
		t.Errorf("expected lesson 1 reference, got:\n%s", stdout)
	}
}

func TestCmdDiff(t *testing.T) {
	tempDir := t.TempDir()
	_ = os.WriteFile(tempDir+"/zone.yaml", []byte("name: test\n"), 0644)
	_ = os.MkdirAll(tempDir+"/.hit", 0755)

	respBody := `{"version": "1.0", "status": "active"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(respBody))
	}))
	defer srv.Close()

	// First request: v1.0
	code, _, _ := captureOutput(func() int {
		return run([]string{"-w", tempDir, "--no-color", srv.URL})
	})
	if code != 0 {
		t.Fatalf("first request failed: code %d", code)
	}

	// Change server response to v2.0
	respBody = `{"version": "2.0", "status": "active"}`

	// Second request: v2.0
	code, _, _ = captureOutput(func() int {
		return run([]string{"-w", tempDir, "--no-color", srv.URL})
	})
	if code != 0 {
		t.Fatalf("second request failed: code %d", code)
	}

	// hit diff 2 1 (compare older hit #2 with newer hit #1)
	code, stdout, stderr := captureOutput(func() int {
		return run([]string{"-w", tempDir, "--no-color", "diff", "2", "1"})
	})
	if code != 0 {
		t.Fatalf("hit diff failed with code %d: stderr=%s", code, stderr)
	}

	if !strings.Contains(stdout, "Diff:") {
		t.Errorf("expected 'Diff:' header in diff output, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "-   \"version\": \"1.0\"") {
		t.Errorf("expected deletion of version 1.0, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "+   \"version\": \"2.0\"") {
		t.Errorf("expected insertion of version 2.0, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "  \"status\": \"active\"") {
		t.Errorf("expected context line with status active, got:\n%s", stdout)
	}

	// hit diff 2 --json
	code, stdout, stderr = captureOutput(func() int {
		return run([]string{"-w", tempDir, "diff", "2", "--json"})
	})
	if code != 0 {
		t.Fatalf("hit diff --json failed with code %d: stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, `"has_differences": true`) {
		t.Errorf("expected has_differences in JSON diff output, got:\n%s", stdout)
	}
}

func TestCmdReplayDiff(t *testing.T) {
	tempDir := t.TempDir()
	_ = os.WriteFile(tempDir+"/zone.yaml", []byte("name: test-replay-diff\n"), 0644)
	_ = os.MkdirAll(tempDir+"/.hit", 0755)

	respBody := `{"count": 10, "state": "initial"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(respBody))
	}))
	defer srv.Close()

	// Initial request recorded in history
	code, _, _ := captureOutput(func() int {
		return run([]string{"-w", tempDir, "--no-color", srv.URL})
	})
	if code != 0 {
		t.Fatalf("request failed: code %d", code)
	}

	// Server state changes before replay
	respBody = `{"count": 25, "state": "updated"}`

	// hit replay 1 --diff
	code, stdout, stderr := captureOutput(func() int {
		return run([]string{"-w", tempDir, "--no-color", "replay", "1", "--diff"})
	})
	if code != 0 {
		t.Fatalf("hit replay --diff failed with code %d: stderr=%s", code, stderr)
	}

	if !strings.Contains(stdout, "Live Replay") {
		t.Errorf("expected 'Live Replay' in diff output, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "-   \"count\": 10") {
		t.Errorf("expected - count 10, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "+   \"count\": 25") {
		t.Errorf("expected + count 25, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "+   \"state\": \"updated\"") {
		t.Errorf("expected + state updated, got:\n%s", stdout)
	}
}

func TestCmdRunHAR(t *testing.T) {
	tempDir := t.TempDir()
	_ = os.WriteFile(tempDir+"/zone.yaml", []byte("name: test-har\n"), 0644)
	_ = os.MkdirAll(tempDir+"/collections", 0755)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"items": ["cat", "dog"]}`))
	}))
	defer srv.Close()

	reqYAML := "name: test-req\nmethod: GET\nurl: " + srv.URL + "?filter=all\n"
	_ = os.WriteFile(tempDir+"/collections/get-items.yaml", []byte(reqYAML), 0644)

	harPath := filepath.Join(tempDir, "run.har")
	code, stdout, stderr := captureOutput(func() int {
		return run([]string{"-w", tempDir, "--no-color", "run", "collections/get-items.yaml", "--har", harPath})
	})
	if code != 0 {
		t.Fatalf("hit run --har failed: code %d, stderr=%s", code, stderr)
	}

	if !strings.Contains(stdout, "HAR archive written") {
		t.Errorf("expected confirmation of HAR written, got:\n%s", stdout)
	}

	data, err := os.ReadFile(harPath)
	if err != nil {
		t.Fatalf("failed to read generated HAR file: %v", err)
	}

	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("generated HAR file is not valid JSON: %v", err)
	}

	logObj, ok := root["log"].(map[string]any)
	if !ok {
		t.Fatalf("missing 'log' object in HAR")
	}
	if logObj["version"] != "1.2" {
		t.Errorf("expected HAR version 1.2, got %v", logObj["version"])
	}
	entries, ok := logObj["entries"].([]any)
	if !ok || len(entries) != 1 {
		t.Fatalf("expected 1 entry in HAR, got %d", len(entries))
	}
}

func TestCmdReportHAR(t *testing.T) {
	tempDir := t.TempDir()
	_ = os.WriteFile(tempDir+"/zone.yaml", []byte("name: test-report-har\n"), 0644)
	_ = os.MkdirAll(tempDir+"/.hit", 0755)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status": "ok"}`))
	}))
	defer srv.Close()

	// Perform an adhoc hit recorded to history
	code, _, _ := captureOutput(func() int {
		return run([]string{"-w", tempDir, "--no-color", srv.URL})
	})
	if code != 0 {
		t.Fatalf("request failed: code %d", code)
	}

	harPath := filepath.Join(tempDir, "history.har")
	code, stdout, stderr := captureOutput(func() int {
		return run([]string{"-w", tempDir, "report", "har", "-o", harPath})
	})
	if code != 0 {
		t.Fatalf("hit report har failed: code %d, stderr=%s", code, stderr)
	}

	if !strings.Contains(stdout, "HAR archive written") {
		t.Errorf("expected confirmation of HAR written, got:\n%s", stdout)
	}

	data, err := os.ReadFile(harPath)
	if err != nil {
		t.Fatalf("failed to read generated HAR file: %v", err)
	}

	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("generated HAR is not valid JSON: %v", err)
	}

	logObj := root["log"].(map[string]any)
	entries := logObj["entries"].([]any)
	if len(entries) < 1 {
		t.Errorf("expected at least 1 entry in exported history HAR, got %d", len(entries))
	}
}

func TestCmdAssertURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"id": "c138f7ef-d0ba-47a3-8321-705a6104c86e",
			"name": "Alice",
			"email": "alice@example.com",
			"active": true,
			"count": 42
		}`))
	}))
	defer srv.Close()

	// Default YAML output
	code, stdout, stderr := captureOutput(func() int {
		return run([]string{"assert", srv.URL})
	})
	if code != 0 {
		t.Fatalf("hit assert URL failed: code %d, stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, "status: 200") {
		t.Errorf("expected status: 200 in assertion output, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "max_ms:") {
		t.Errorf("expected max_ms in assertion output, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "contains: application/json") {
		t.Errorf("expected header content-type check in assertion output, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "id:") || !strings.Contains(stdout, "matches: ^[0-9a-fA-F-]{36}$") {
		t.Errorf("expected UUID regex matcher for id, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "email:") || !strings.Contains(stdout, `matches: ^[^@]+@[^@]+\.[^@]+$`) {
		t.Errorf("expected email regex matcher for email, got:\n%s", stdout)
	}

	// Flag --no-latency
	code, stdout, _ = captureOutput(func() int {
		return run([]string{"assert", srv.URL, "--no-latency"})
	})
	if code != 0 {
		t.Fatalf("hit assert with --no-latency failed")
	}
	if strings.Contains(stdout, "max_ms:") {
		t.Errorf("expected max_ms to be omitted with --no-latency, got:\n%s", stdout)
	}

	// Flag --json
	code, stdout, _ = captureOutput(func() int {
		return run([]string{"assert", srv.URL, "--json"})
	})
	if code != 0 {
		t.Fatalf("hit assert with --json failed")
	}
	var jsonDoc map[string]any
	if err := json.Unmarshal([]byte(stdout), &jsonDoc); err != nil {
		t.Fatalf("expected valid JSON from hit assert --json, got error: %v, stdout:\n%s", err, stdout)
	}
	if _, ok := jsonDoc["tests"]; !ok {
		t.Errorf("missing 'tests' array in JSON output")
	}
}

func TestCmdAssertSaveAndAppend(t *testing.T) {
	tempDir := t.TempDir()
	_ = os.WriteFile(tempDir+"/zone.yaml", []byte("name: test-assert-zone\n"), 0644)
	_ = os.MkdirAll(tempDir+"/collections", 0755)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"total": 10, "items": [{"id": 1, "name": "Item 1"}]}`))
	}))
	defer srv.Close()

	saveFile := filepath.Join(tempDir, "collections", "test_item.yaml")

	// Save assertions to a new request file
	code, stdout, stderr := captureOutput(func() int {
		return run([]string{"-w", tempDir, "assert", srv.URL, "--save", saveFile})
	})
	if code != 0 {
		t.Fatalf("assert --save failed: code %d, stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, "saved") {
		t.Errorf("expected confirmation of saved assertions, got:\n%s", stdout)
	}

	data, err := os.ReadFile(saveFile)
	if err != nil {
		t.Fatalf("failed to read saved file: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "status: 200") || !strings.Contains(content, "total:") {
		t.Errorf("saved file missing expected tests:\n%s", content)
	}

	// Append more assertions (e.g. running against target file directly with --append)
	code, stdout, stderr = captureOutput(func() int {
		return run([]string{"-w", tempDir, "assert", saveFile, "--append"})
	})
	if code != 0 {
		t.Fatalf("assert --append failed: code %d, stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, "appended") {
		t.Errorf("expected confirmation of appended assertions, got:\n%s", stdout)
	}
}

func TestCmdAssertHistory(t *testing.T) {
	tempDir := t.TempDir()
	_ = os.WriteFile(tempDir+"/zone.yaml", []byte("name: test-assert-hist\n"), 0644)
	_ = os.MkdirAll(tempDir+"/.hit", 0755)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"user": "tester", "role": "admin"}`))
	}))
	defer srv.Close()

	// 1. Run ad hoc request to write into history
	code, _, stderr := captureOutput(func() int {
		return run([]string{"-w", tempDir, srv.URL})
	})
	if code != 0 {
		t.Fatalf("initial request failed: %s", stderr)
	}

	// 2. Assert against history index 1
	code, stdout, stderr := captureOutput(func() int {
		return run([]string{"-w", tempDir, "assert", "1"})
	})
	if code != 0 {
		t.Fatalf("hit assert 1 failed: code %d, stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, "user:") || !strings.Contains(stdout, "role: admin") {
		t.Errorf("expected inferred fields from history in output, got:\n%s", stdout)
	}
}

func TestCmdNewInfer(t *testing.T) {
	tempDir := t.TempDir()
	_ = os.WriteFile(tempDir+"/zone.yaml", []byte("name: test-new-infer\n"), 0644)
	_ = os.MkdirAll(tempDir+"/collections", 0755)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status": "ok", "version": "1.0.0"}`))
	}))
	defer srv.Close()

	code, stdout, stderr := captureOutput(func() int {
		return run([]string{"-w", tempDir, "new", "health", "-u", srv.URL, "--infer"})
	})
	if code != 0 {
		t.Fatalf("hit new --infer failed: code %d, stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, "created") || !strings.Contains(stdout, "inferred assertions") {
		t.Errorf("expected confirmation of created spec with inferred assertions, got:\n%s", stdout)
	}

	filePath := filepath.Join(tempDir, "collections", "health.yaml")
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read created spec file: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "status: ok") || !strings.Contains(content, "version:") {
		t.Errorf("created spec missing inferred tests:\n%s", content)
	}
}

func TestCmdHistorySaveInfer(t *testing.T) {
	tempDir := t.TempDir()
	_ = os.WriteFile(tempDir+"/zone.yaml", []byte("name: test-hist-save-infer\n"), 0644)
	_ = os.MkdirAll(tempDir+"/.hit", 0755)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"product_id": 101, "in_stock": true}`))
	}))
	defer srv.Close()

	// Hit endpoint to populate history
	code, _, stderr := captureOutput(func() int {
		return run([]string{"-w", tempDir, srv.URL})
	})
	if code != 0 {
		t.Fatalf("request failed: %s", stderr)
	}

	// Promote with --infer
	targetFile := filepath.Join(tempDir, "collections", "product.yaml")
	code, stdout, stderr := captureOutput(func() int {
		return run([]string{"-w", tempDir, "history", "save", "1", targetFile, "--infer"})
	})
	if code != 0 {
		t.Fatalf("history save --infer failed: code %d, stderr=%s", code, stderr)
	}
	if !strings.Contains(stdout, "with inferred assertions") {
		t.Errorf("expected confirmation with inferred assertions, got:\n%s", stdout)
	}

	data, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatalf("failed to read saved request file: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "product_id:") || !strings.Contains(content, "in_stock:") {
		t.Errorf("promoted request missing inferred assertions:\n%s", content)
	}
}

func TestCmdMockChaosFlags(t *testing.T) {
	// Test invalid flaky rate triggers exit code 2
	code, _, stderr := captureOutput(func() int {
		return run([]string{"mock", "--flaky=invalid"})
	})
	if code != 2 {
		t.Fatalf("expected exit code 2 for invalid --flaky, got %d", code)
	}
	if !strings.Contains(stderr, "invalid --flaky rate") {
		t.Errorf("expected error message for invalid --flaky, got: %s", stderr)
	}

	// Test invalid rate limit triggers exit code 2
	code, _, stderr = captureOutput(func() int {
		return run([]string{"mock", "--rate-limit=xyz"})
	})
	if code != 2 {
		t.Fatalf("expected exit code 2 for invalid --rate-limit, got %d", code)
	}
	if !strings.Contains(stderr, "invalid --rate-limit") {
		t.Errorf("expected error message for invalid --rate-limit, got: %s", stderr)
	}

	// Test invalid latency triggers exit code 2
	code, _, stderr = captureOutput(func() int {
		return run([]string{"mock", "--latency=notaduration"})
	})
	if code != 2 {
		t.Fatalf("expected exit code 2 for invalid --latency, got %d", code)
	}
	if !strings.Contains(stderr, "invalid --latency duration") {
		t.Errorf("expected error message for invalid --latency, got: %s", stderr)
	}
}

func TestCmdMockOpenAPIFlags(t *testing.T) {
	// Test nonexistent OpenAPI spec triggers exit code 2
	code, _, stderr := captureOutput(func() int {
		return run([]string{"mock", "--openapi=nonexistent-file.yaml"})
	})
	if code != 2 {
		t.Fatalf("expected exit code 2 for nonexistent openapi file, got %d", code)
	}
	if !strings.Contains(stderr, "failed to load OpenAPI spec") {
		t.Errorf("expected error message for nonexistent spec, got: %s", stderr)
	}

	// Test invalid non-spec YAML triggers exit code 2
	tmpDir := t.TempDir()
	invalidFile := filepath.Join(tmpDir, "invalid.yaml")
	_ = os.WriteFile(invalidFile, []byte("key: value\n"), 0644)

	code, _, stderr = captureOutput(func() int {
		return run([]string{"mock", "--openapi=" + invalidFile})
	})
	if code != 2 {
		t.Fatalf("expected exit code 2 for invalid openapi spec, got %d", code)
	}
	if !strings.Contains(stderr, "not a valid OpenAPI") {
		t.Errorf("expected error message for invalid spec, got: %s", stderr)
	}
}

func TestCLISnippet(t *testing.T) {
	// 1. Python snippet with extract
	code, stdout, stderr := captureOutput(func() int {
		return run([]string{"snippet", "http://127.0.0.1:8765/pets", "--lang", "python", "--extract", "items[0].id"})
	})
	if code != 0 {
		t.Fatalf("expected code 0, got %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "import requests") || !strings.Contains(stdout, `data["items"][0]["id"]`) {
		t.Errorf("expected python requests and accessor in snippet, got:\n%s", stdout)
	}

	// 2. JS snippet
	code, stdout, stderr = captureOutput(func() int {
		return run([]string{"snippet", "http://127.0.0.1:8765/pets", "-m", "POST", "-j", `{"name":"Rex"}`, "--lang", "js", "--extract", "data.id"})
	})
	if code != 0 {
		t.Fatalf("expected code 0, got %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "fetch(url, options)") || !strings.Contains(stdout, "data.id") {
		t.Errorf("expected js fetch snippet, got:\n%s", stdout)
	}

	// 3. PHP snippet
	code, stdout, stderr = captureOutput(func() int {
		return run([]string{"snippet", "http://127.0.0.1:8765/pets", "--lang", "php", "--extract", "items[0].id"})
	})
	if code != 0 {
		t.Fatalf("expected code 0, got %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "curl_init") || !strings.Contains(stdout, `$data["items"][0]["id"]`) {
		t.Errorf("expected php curl snippet, got:\n%s", stdout)
	}

	// 4. Go snippet
	code, stdout, stderr = captureOutput(func() int {
		return run([]string{"snippet", "http://127.0.0.1:8765/pets", "--lang", "go"})
	})
	if code != 0 {
		t.Fatalf("expected code 0, got %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "http.NewRequest") || !strings.Contains(stdout, "package main") {
		t.Errorf("expected go snippet, got:\n%s", stdout)
	}

	// 5. All snippets
	code, stdout, stderr = captureOutput(func() int {
		return run([]string{"snippet", "http://127.0.0.1:8765/pets", "--all"})
	})
	if code != 0 {
		t.Fatalf("expected code 0, got %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "Python") || !strings.Contains(stdout, "JavaScript") || !strings.Contains(stdout, "PHP") || !strings.Contains(stdout, "Go") {
		t.Errorf("expected all snippets, got:\n%s", stdout)
	}
}

func TestCLIShowLang(t *testing.T) {
	wsDir := "../../examples/petstore-zone"
	if _, err := os.Stat(wsDir); os.IsNotExist(err) {
		wsDir = "examples/petstore-zone"
	}
	code, stdout, stderr := captureOutput(func() int {
		return run([]string{"-w", wsDir, "show", "petstore/pets/list", "--lang", "python", "--extract", "items[0].id"})
	})
	if code != 0 {
		t.Fatalf("expected code 0, got %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "import requests") || !strings.Contains(stdout, `data["items"][0]["id"]`) {
		t.Errorf("expected python snippet from show --lang, got:\n%s", stdout)
	}
}

func TestCLILearnVerify(t *testing.T) {
	wsDir := "../../examples/petstore-zone"
	if _, err := os.Stat(wsDir); os.IsNotExist(err) {
		wsDir = "examples/petstore-zone"
	}

	// Run learn verify 1 (offline check against zone)
	code, stdout, _ := captureOutput(func() int {
		return run([]string{"-w", wsDir, "learn", "verify", "1"})
	})
	if !strings.Contains(stdout, "Lesson 1:") {
		t.Errorf("expected Lesson 1 in verify output, got:\n%s", stdout)
	}
	if code != 0 && code != 2 {
		t.Errorf("expected code 0 or 2, got %d", code)
	}
}

func TestCLIFuzz(t *testing.T) {
	// Mock server that returns 200 for clean requests, 500 when injection / crash string is detected
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodyStr := string(b)
		if strings.Contains(bodyStr, "SLEEP(") || strings.Contains(bodyStr, "1e308") {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"database crash"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	// 1. Run fuzzer with clean categories (types, nulls)
	code, stdout, stderr := captureOutput(func() int {
		return run([]string{"fuzz", srv.URL, "-m", "POST", "-j", `{"name":"test"}`, "-n", "5", "--categories", "types,nulls", "--fail-on-5xx"})
	})
	if code != 0 {
		t.Fatalf("expected code 0 on clean fuzz, got %d, stderr: %s\nstdout: %s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "Total Mutations Executed:") {
		t.Errorf("expected summary in output, got:\n%s", stdout)
	}

	// 2. Run fuzzer with injection & boundaries -> should detect 500 and fail when --fail-on-5xx
	code, stdout, stderr = captureOutput(func() int {
		return run([]string{"fuzz", srv.URL, "-m", "POST", "-j", `{"name":"test","val":1}`, "-n", "30", "--categories", "injection,boundaries", "--fail-on-5xx"})
	})
	if code == 0 {
		t.Fatalf("expected failure code with --fail-on-5xx, got %d", code)
	}
	if !strings.Contains(stdout, "CRITICAL") && !strings.Contains(stderr, "crash") {
		t.Errorf("expected anomaly detection in output, got:\nstdout: %s\nstderr: %s", stdout, stderr)
	}
}

func TestCLIGraphQL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &req)

		q, _ := req["query"].(string)
		w.Header().Set("Content-Type", "application/json")

		if strings.Contains(q, "errorQuery") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"errors":[{"message":"Cannot query field errorQuery","locations":[{"line":1,"column":3}],"path":["errorQuery"]}]}`))
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"user":{"id":"42","name":"Arthur Dent"}}}`))
	}))
	defer srv.Close()

	// 1. Successful query
	code, stdout, stderr := captureOutput(func() int {
		return run([]string{"graphql", srv.URL, "-q", "{ user { id name } }"})
	})
	if code != 0 {
		t.Fatalf("expected code 0, got %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "Arthur Dent") {
		t.Errorf("expected Arthur Dent in output, got:\n%s", stdout)
	}

	// 2. Query with --fail-on-errors
	code, stdout, stderr = captureOutput(func() int {
		return run([]string{"graphql", srv.URL, "-q", "{ errorQuery }", "--fail-on-errors"})
	})
	if code == 0 {
		t.Fatalf("expected failure code with --fail-on-errors, got 0; stdout: %s", stdout)
	}
	if !strings.Contains(stdout, "Cannot query field errorQuery") && !strings.Contains(stderr, "error") {
		t.Errorf("expected error message in output, got stdout=%q, stderr=%q", stdout, stderr)
	}
}

func TestCLISchemaGraphQL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := `{
			"data": {
				"__schema": {
					"queryType": { "name": "Query" },
					"mutationType": null,
					"subscriptionType": null,
					"types": [
						{
							"kind": "OBJECT",
							"name": "Query",
							"fields": [
								{
									"name": "greeting",
									"type": { "kind": "SCALAR", "name": "String" }
								}
							]
						}
					]
				}
			}
		}`
		_, _ = w.Write([]byte(resp))
	}))
	defer srv.Close()

	code, stdout, stderr := captureOutput(func() int {
		return run([]string{"schema", "graphql", srv.URL})
	})
	if code != 0 {
		t.Fatalf("expected code 0, got %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "type Query") || !strings.Contains(stdout, "greeting: String") {
		t.Errorf("expected SDL in output, got:\n%s", stdout)
	}
}

func TestCLISSE(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		fmt.Fprintf(w, "event: message\ndata: chunk 1\n\n")
		flusher.Flush()
		time.Sleep(5 * time.Millisecond)
		fmt.Fprintf(w, "event: message\ndata: chunk 2\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	// 1. Normal run with limit 2
	code, stdout, stderr := captureOutput(func() int {
		return run([]string{"sse", srv.URL, "-n", "2"})
	})
	if code != 0 {
		t.Fatalf("expected code 0, got %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "chunk 1") || !strings.Contains(stdout, "Stream Metrics") {
		t.Errorf("expected stream output, got:\n%s", stdout)
	}

	// 2. JSON mode
	code, stdout, stderr = captureOutput(func() int {
		return run([]string{"sse", srv.URL, "-n", "2", "--json"})
	})
	if code != 0 {
		t.Fatalf("expected code 0, got %d, stderr: %s", code, stderr)
	}
	var res map[string]any
	if err := json.Unmarshal([]byte(stdout), &res); err != nil {
		t.Fatalf("expected valid JSON, got err: %v, stdout: %s", err, stdout)
	}
	if count, ok := res["event_count"].(float64); !ok || count != 2 {
		t.Errorf("expected event_count 2, got %v", res["event_count"])
	}
}

func TestCLIWS(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.ToLower(r.Header.Get("Upgrade")) != "websocket" {
			return
		}
		key := r.Header.Get("Sec-WebSocket-Key")
		h := sha1.New()
		h.Write([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
		accept := base64.StdEncoding.EncodeToString(h.Sum(nil))

		hj := w.(http.Hijacker)
		conn, bufrw, err := hj.Hijack()
		if err != nil {
			return
		}
		defer conn.Close()

		bufrw.WriteString("HTTP/1.1 101 Switching Protocols\r\n")
		bufrw.WriteString("Upgrade: websocket\r\n")
		bufrw.WriteString("Connection: Upgrade\r\n")
		bufrw.WriteString(fmt.Sprintf("Sec-WebSocket-Accept: %s\r\n\r\n", accept))
		bufrw.Flush()

		// Read frame, echo back
		_, _ = bufrw.ReadByte() // b0
		b1, _ := bufrw.ReadByte()
		lenByte := int(b1 & 0x7F)
		maskKey := make([]byte, 4)
		_, _ = io.ReadFull(bufrw, maskKey)
		payload := make([]byte, lenByte)
		_, _ = io.ReadFull(bufrw, payload)
		for i := 0; i < lenByte; i++ {
			payload[i] ^= maskKey[i%4]
		}

		var resp []byte
		resp = append(resp, 0x81, byte(lenByte))
		resp = append(resp, payload...)
		_, _ = bufrw.Write(resp)
		bufrw.Flush()
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")

	// 1. Success matching expectation
	code, stdout, stderr := captureOutput(func() int {
		return run([]string{"ws", wsURL, "-m", "hello hit ws", "--expect", "hello hit ws", "-n", "1"})
	})
	if code != 0 {
		t.Fatalf("expected code 0, got %d, stderr: %s\nstdout: %s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "✓ PASS") {
		t.Errorf("expected pass in output, got:\n%s", stdout)
	}

	// 2. Failure when expectation does not match
	code, stdout, stderr = captureOutput(func() int {
		return run([]string{"ws", wsURL, "-m", "hello hit ws", "--expect", "does-not-exist", "-n", "1", "-d", "500ms"})
	})
	if code == 0 {
		t.Fatalf("expected code != 0 on unmet expectation, got 0; stdout: %s", stdout)
	}
}

func TestCLIServersAndEnvs(t *testing.T) {
	tempDir := t.TempDir()
	// Init zone
	code, stdout, stderr := captureOutput(func() int {
		return run([]string{"init", tempDir, "--name", "test-zone"})
	})
	if code != 0 {
		t.Fatalf("init failed: %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "Created zone") {
		t.Errorf("expected 'Created zone' in init output, got: %s", stdout)
	}

	// Test 'hit servers' with -z flag
	code, stdout, stderr = captureOutput(func() int {
		return run([]string{"-z", tempDir, "servers"})
	})
	if code != 0 {
		t.Fatalf("servers command failed: %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "dev") {
		t.Errorf("expected 'dev' in servers output, got:\n%s", stdout)
	}

	// Test 'hit envs' with -w flag
	code, stdout, stderr = captureOutput(func() int {
		return run([]string{"-w", tempDir, "envs"})
	})
	if code != 0 {
		t.Fatalf("envs command failed: %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "dev") {
		t.Errorf("expected 'dev' in envs output, got:\n%s", stdout)
	}
}

func TestCLIShorthandFlow(t *testing.T) {
	tempDir := t.TempDir()
	// Init zone
	_ = run([]string{"init", tempDir})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Custom") != "hit-val" {
			http.Error(w, "missing header", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"shorthand":"worked"}`))
	}))
	defer srv.Close()

	// 1. hit shorthand set
	code, stdout, stderr := captureOutput(func() int {
		return run([]string{"-z", tempDir, "shorthand", "set", "test-api", srv.URL, "-m", "GET", "-H", "X-Custom: hit-val", "-d", "Test shorthand"})
	})
	if code != 0 {
		t.Fatalf("shorthand set failed: %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "saved successfully") {
		t.Errorf("expected success message, got: %s", stdout)
	}

	// 1b. hit shorthand [NAME] --url URL (direct shorthand syntax from README)
	code, stdout, stderr = captureOutput(func() int {
		return run([]string{"-z", tempDir, "shorthand", "direct-api", "--url", srv.URL, "-m", "GET", "-H", "X-Custom: hit-val"})
	})
	if code != 0 {
		t.Fatalf("shorthand direct --url failed: %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "saved successfully") {
		t.Errorf("expected success message, got: %s", stdout)
	}

	// 2. hit shorthand ls
	code, stdout, stderr = captureOutput(func() int {
		return run([]string{"-z", tempDir, "shorthand", "ls"})
	})
	if code != 0 {
		t.Fatalf("shorthand ls failed: %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "test-api") {
		t.Errorf("expected 'test-api' in list, got:\n%s", stdout)
	}

	// 3. hit shorthand get
	code, stdout, stderr = captureOutput(func() int {
		return run([]string{"-z", tempDir, "shorthand", "get", "test-api"})
	})
	if code != 0 {
		t.Fatalf("shorthand get failed: %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "test-api") || !strings.Contains(stdout, "X-Custom") {
		t.Errorf("expected details in shorthand get, got:\n%s", stdout)
	}

	// 4. hit <name> (direct execution)
	code, stdout, stderr = captureOutput(func() int {
		return run([]string{"-z", tempDir, "--no-color", "--no-history", "test-api"})
	})
	if code != 0 {
		t.Fatalf("direct shorthand invocation failed: %d, stderr: %s\nstdout: %s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "200 OK") || !strings.Contains(stdout, "worked") {
		t.Errorf("expected 200 OK and worked in output, got:\n%s", stdout)
	}

	// 5. hit body <name>
	code, stdout, stderr = captureOutput(func() int {
		return run([]string{"-z", tempDir, "--no-color", "--no-history", "body", "test-api"})
	})
	if code != 0 {
		t.Fatalf("hit body shorthand failed: %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, `"shorthand": "worked"`) {
		t.Errorf("expected body output, got:\n%s", stdout)
	}

	// 6. hit shorthand rm
	code, stdout, stderr = captureOutput(func() int {
		return run([]string{"-z", tempDir, "shorthand", "rm", "test-api"})
	})
	if code != 0 {
		t.Fatalf("shorthand rm failed: %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "removed") {
		t.Errorf("expected removed message, got: %s", stdout)
	}
}

func TestCLISchedule(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"status":"ok","run":%d}`, callCount)))
	}))
	defer srv.Close()

	// 1. Success run: 3 ticks every 20ms
	code, stdout, stderr := captureOutput(func() int {
		return run([]string{
			"schedule", srv.URL,
			"--at", "now",
			"--every", "20ms",
			"-n", "3",
			"--status", "200",
			"--expect", "ok",
			"--no-color", "--no-history",
		})
	})
	if code != 0 {
		t.Fatalf("schedule command failed: %d, stderr: %s\nstdout: %s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "Schedule Summary:") || !strings.Contains(stdout, "3 runs") || !strings.Contains(stdout, "3 passed") {
		t.Errorf("expected 3 passed runs in summary, got:\n%s", stdout)
	}
	if callCount != 3 {
		t.Errorf("expected server to be called 3 times, got %d", callCount)
	}

	// 2. Expectation failure run
	code, stdout, stderr = captureOutput(func() int {
		return run([]string{
			"schedule", srv.URL,
			"--at", "now",
			"-n", "1",
			"--status", "404",
			"--no-color", "--no-history",
		})
	})
	if code == 0 {
		t.Fatalf("expected schedule to fail with non-matching status, got 0. stdout: %s", stdout)
	}
	if !strings.Contains(stdout, "1 failed") && !strings.Contains(stderr, "failed expectation") {
		t.Errorf("expected failure message, got stdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
}

func TestWizardCommand(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "hit-wizard-cli-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	targetDir := filepath.Join(tmpDir, "my-service")

	// 1. Run hit wizard --name my-service --url http://127.0.0.1:9090 -y
	code, stdout, stderr := captureOutput(func() int {
		return run([]string{
			"wizard", targetDir,
			"--name", "my-service",
			"--url", "http://127.0.0.1:9090",
			"--yes",
			"--no-color",
		})
	})
	if code != 0 {
		t.Fatalf("expected wizard to succeed, got %d. stderr: %s, stdout: %s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "Created new zone 'my-service'") {
		t.Errorf("expected wizard creation message, got: %s", stdout)
	}
	if !strings.Contains(stdout, "zone.yaml") || !strings.Contains(stdout, "chains/") {
		t.Errorf("expected scaffold tree in stdout, got: %s", stdout)
	}

	// 2. Run hit sanity on the scaffolded zone
	code, stdout, _ = captureOutput(func() int {
		return run([]string{
			"-z", targetDir,
			"sanity",
			"--offline",
			"--no-color",
		})
	})
	// Sanity should run and report the missing secrets file guidance
	if !strings.Contains(stdout, "secrets file missing: copy local.secrets.example.yaml to local.secrets.yaml") {
		t.Errorf("expected sanity check to guide user about missing secrets file, got: %s", stdout)
	}

	// 3. Test hit zone new alias
	targetDir2 := filepath.Join(tmpDir, "my-service-2")
	code, stdout, stderr = captureOutput(func() int {
		return run([]string{
			"zone", "new", targetDir2,
			"-y",
			"--no-color",
		})
	})
	if code != 0 {
		t.Fatalf("expected 'hit zone new' to succeed, got %d. stderr: %s, stdout: %s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "Created new zone") {
		t.Errorf("expected creation message from zone new, got: %s", stdout)
	}
}






