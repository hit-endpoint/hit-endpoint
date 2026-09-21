package output

import (
	"fmt"
	"io"
	"strings"

	"hit/internal/types"
)

func PrintAgentResult(r *types.Result, w io.Writer) {
	ref := r.Ref
	if ref == "" {
		ref = r.Name
	}

	passedTests := 0
	for _, t := range r.Tests {
		if t.Passed {
			passedTests++
		}
	}

	if r.OK() {
		testInfo := ""
		if len(r.Tests) > 0 {
			testInfo = fmt.Sprintf(" [%d/%d passed]", passedTests, len(r.Tests))
		}
		fmt.Fprintf(w, "PASS %s (%d %s, %.0fms)%s\n", ref, r.Status, r.Reason, r.ElapsedMs, testInfo)
		return
	}

	// Failure formatting
	statusStr := fmt.Sprintf("%d %s", r.Status, r.Reason)
	if !r.HasStatus {
		statusStr = "NO_RESPONSE"
	}
	if r.Error != "" {
		statusStr = r.Error
	}
	fmt.Fprintf(w, "FAIL %s (%s, %.0fms)\n", ref, statusStr, r.ElapsedMs)
	fmt.Fprintf(w, "  URL: %s %s\n", r.Method, r.Url)

	for _, t := range r.FailedTests() {
		detail := t.Detail
		if detail != "" {
			fmt.Fprintf(w, "  Assertion Failed: %s -> %s\n", t.Name, detail)
		} else {
			fmt.Fprintf(w, "  Assertion Failed: %s\n", t.Name)
		}
	}
	for k, errStr := range r.CaptureErrors {
		fmt.Fprintf(w, "  Capture Error: %s -> %s\n", k, errStr)
	}
	if r.Text != "" && len(r.Tests) > 0 {
		trimmed := strings.TrimSpace(r.Text)
		if len(trimmed) > 200 {
			trimmed = trimmed[:200] + "..."
		}
		fmt.Fprintf(w, "  Response: %s\n", trimmed)
	}
}

func PrintAgentSummary(results []*types.Result, w io.Writer) {
	total := len(results)
	passed := 0
	failed := 0
	for _, r := range results {
		if r.OK() {
			passed++
		} else {
			failed++
		}
	}
	if failed == 0 {
		fmt.Fprintf(w, "SUMMARY: ALL %d REQUESTS PASSED\n", total)
	} else {
		fmt.Fprintf(w, "SUMMARY: %d PASSED, %d FAILED (TOTAL %d)\n", passed, failed, total)
	}
}
