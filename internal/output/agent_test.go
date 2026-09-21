package output

import (
	"bytes"
	"strings"
	"testing"

	"hit/internal/assertions"
	"hit/internal/types"
)

func TestPrintAgentResult(t *testing.T) {
	// 1. Pass result
	passRes := &types.Result{
		Ref:       "pets/list",
		Status:    200,
		Reason:    "OK",
		ElapsedMs: 15.2,
		HasStatus: true,
		Tests: []assertions.TestResult{
			{Name: "status == 200", Passed: true},
		},
	}
	var buf bytes.Buffer
	PrintAgentResult(passRes, &buf)
	out := buf.String()
	if !strings.Contains(out, "PASS pets/list (200 OK, 15ms) [1/1 passed]") {
		t.Errorf("unexpected pass output: %q", out)
	}

	// 2. Fail result
	failRes := &types.Result{
		Ref:       "pets/create",
		Method:    "POST",
		Url:       "http://localhost:8765/pets",
		Status:    500,
		Reason:    "Internal Server Error",
		ElapsedMs: 42.0,
		HasStatus: true,
		Tests: []assertions.TestResult{
			{Name: "status == 201", Passed: false, Detail: "got 500"},
		},
		CaptureErrors: map[string]string{
			"pet_id": "field not found",
		},
		Text: `{"error": "database connection refused"}`,
	}
	buf.Reset()
	PrintAgentResult(failRes, &buf)
	out = buf.String()
	if !strings.Contains(out, "FAIL pets/create (500 Internal Server Error, 42ms)") {
		t.Errorf("missing FAIL line: %q", out)
	}
	if !strings.Contains(out, "URL: POST http://localhost:8765/pets") {
		t.Errorf("missing URL line: %q", out)
	}
	if !strings.Contains(out, "Assertion Failed: status == 201 -> got 500") {
		t.Errorf("missing Assertion Failed line: %q", out)
	}
	if !strings.Contains(out, "Capture Error: pet_id -> field not found") {
		t.Errorf("missing Capture Error line: %q", out)
	}
	if !strings.Contains(out, `Response: {"error": "database connection refused"}`) {
		t.Errorf("missing Response line: %q", out)
	}
}

func TestPrintAgentSummary(t *testing.T) {
	r1 := &types.Result{Status: 200, HasStatus: true}
	r2 := &types.Result{Status: 500, HasStatus: true, Tests: []assertions.TestResult{{Passed: false}}}

	var buf bytes.Buffer
	PrintAgentSummary([]*types.Result{r1}, &buf)
	if !strings.Contains(buf.String(), "SUMMARY: ALL 1 REQUESTS PASSED") {
		t.Errorf("unexpected all-pass summary: %q", buf.String())
	}

	buf.Reset()
	PrintAgentSummary([]*types.Result{r1, r2}, &buf)
	if !strings.Contains(buf.String(), "SUMMARY: 1 PASSED, 1 FAILED (TOTAL 2)") {
		t.Errorf("unexpected failed summary: %q", buf.String())
	}
}
