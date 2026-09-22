package output

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"testing"

	"github.com/hit-endpoint/hit-endpoint/internal/assertions"
	"github.com/hit-endpoint/hit-endpoint/internal/types"
)

func TestBuildJUnitSuites(t *testing.T) {
	results := []*types.Result{
		{
			Ref:       "pets/list",
			Method:    "GET",
			Url:       "http://127.0.0.1:8765/pets",
			Status:    200,
			HasStatus: true,
			ElapsedMs: 25.5,
			Tests: []assertions.TestResult{
				{Name: "status is 200", Passed: true},
				{Name: "items is array", Passed: true},
			},
		},
		{
			Ref:       "pets/create",
			Method:    "POST",
			Url:       "http://127.0.0.1:8765/pets",
			Status:    400,
			HasStatus: true,
			ElapsedMs: 15.2,
			Tests: []assertions.TestResult{
				{Name: "status is 201", Passed: false, Detail: "expected 201 got 400"},
			},
		},
		{
			Ref:       "pets/unreachable",
			Method:    "GET",
			Url:       "http://127.0.0.1:9999/pets",
			ElapsedMs: 5.0,
			Error:     "connection refused",
		},
	}

	suites := BuildJUnitSuites(results)
	if suites.Tests != 4 {
		t.Errorf("expected 4 tests, got %d", suites.Tests)
	}
	if suites.Failures != 1 {
		t.Errorf("expected 1 failure, got %d", suites.Failures)
	}
	if suites.Errors != 1 {
		t.Errorf("expected 1 error, got %d", suites.Errors)
	}

	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "report.xml")
	if err := WriteJUnitXML(results, outPath); err != nil {
		t.Fatalf("failed to write junit xml: %v", err)
	}

	b, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}

	var parsed JUnitTestSuites
	if err := xml.Unmarshal(b, &parsed); err != nil {
		t.Fatalf("failed to parse generated xml: %v", err)
	}
	if len(parsed.Suites) != 3 {
		t.Errorf("expected 3 suites, got %d", len(parsed.Suites))
	}
}

func TestBuildJUnitSuites_Matrix(t *testing.T) {
	parent := &types.Result{
		Ref:    "users/create",
		Method: "POST",
		Url:    "http://127.0.0.1:8765/users",
		Matrix: []*types.Result{
			{
				Method:    "POST",
				Url:       "http://127.0.0.1:8765/users",
				Status:    201,
				HasStatus: true,
				ElapsedMs: 20.0,
				Tests: []assertions.TestResult{
					{Name: "status is 201", Passed: true},
				},
			},
			{
				Method:    "POST",
				Url:       "http://127.0.0.1:8765/users",
				Status:    400,
				HasStatus: true,
				ElapsedMs: 18.0,
				Tests: []assertions.TestResult{
					{Name: "status is 201", Passed: false, Detail: "got 400"},
				},
			},
		},
	}

	suites := BuildJUnitSuites([]*types.Result{parent})
	if len(suites.Suites) != 2 {
		t.Fatalf("expected 2 suites for matrix rows, got %d", len(suites.Suites))
	}
	if suites.Tests != 2 {
		t.Errorf("expected 2 tests, got %d", suites.Tests)
	}
	if suites.Failures != 1 {
		t.Errorf("expected 1 failure, got %d", suites.Failures)
	}
}

func TestBuildPerfJUnitSuites(t *testing.T) {
	rep := &types.PerfReport{
		Name:         "checkout-flow",
		Method:       "POST",
		Url:          "https://example.com/checkout",
		Concurrency:  10,
		Completed:    500,
		OK:           490,
		Failed:       10,
		DurationS:    5.2,
		StatusCounts: map[int]int{200: 490, 500: 10},
		Errors:       map[string]int{"status 500": 10},
	}

	suites := BuildPerfJUnitSuites(rep, false, "p99 latency 450ms > 300ms")
	if suites.Tests != 3 {
		t.Errorf("expected 3 tests in perf junit, got %d", suites.Tests)
	}
	if suites.Failures != 2 { // Errors > 0 and SLA threshold failed
		t.Errorf("expected 2 failures in perf junit, got %d", suites.Failures)
	}

	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "perf-junit.xml")
	if err := WritePerfJUnitXML(rep, false, "p99 latency 450ms > 300ms", outPath); err != nil {
		t.Fatalf("failed to write perf junit xml: %v", err)
	}

	b, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}
	var parsed JUnitTestSuites
	if err := xml.Unmarshal(b, &parsed); err != nil {
		t.Fatalf("failed to parse generated xml: %v", err)
	}
	if len(parsed.Suites) != 1 {
		t.Errorf("expected 1 suite, got %d", len(parsed.Suites))
	}
}
