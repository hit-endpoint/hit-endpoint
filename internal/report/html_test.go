package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hit/internal/assertions"
	"hit/internal/types"
)

func TestGenerateHTMLReport(t *testing.T) {
	results := []*types.Result{
		{
			Ref:       "pets/list",
			Method:    "GET",
			Url:       "http://localhost:8765/pets",
			Status:    200,
			Reason:    "OK",
			HasStatus: true,
			ElapsedMs: 12.5,
			Size:      120,
			Text:      `[{"id": 1, "name": "Rex"}]`,
			Tests: []assertions.TestResult{
				{Name: "status == 200", Passed: true},
			},
		},
		{
			Ref:       "pets/create",
			Method:    "POST",
			Url:       "http://localhost:8765/pets",
			Status:    500,
			Reason:    "Internal Server Error",
			HasStatus: true,
			ElapsedMs: 45.0,
			Size:      40,
			Text:      `{"error": "db timeout"}`,
			Tests: []assertions.TestResult{
				{Name: "status == 201", Passed: false, Detail: "expected 201, got 500"},
			},
		},
	}

	htmlContent := GenerateHTMLReport(results, "Test Suite Run")
	if !strings.Contains(htmlContent, "Test Suite Run") {
		t.Errorf("missing title in HTML")
	}
	if !strings.Contains(htmlContent, "pets/list") {
		t.Errorf("missing pets/list in HTML")
	}
	if !strings.Contains(htmlContent, "pets/create") {
		t.Errorf("missing pets/create in HTML")
	}
	if !strings.Contains(htmlContent, "expected 201, got 500") {
		t.Errorf("missing test failure detail in HTML")
	}

	tmpDir, err := os.MkdirTemp("", "hit-html-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	reportFile := filepath.Join(tmpDir, "report.html")
	if err := WriteHTMLReport(results, "Export Test", reportFile); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(reportFile); err != nil || fi.Size() == 0 {
		t.Fatalf("expected written report.html, err: %v", err)
	}
}
