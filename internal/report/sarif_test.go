package report

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"hit/internal/fuzz"
)

func TestBuildFuzzSARIF(t *testing.T) {
	result := &fuzz.FuzzResult{
		TargetMethod:    "POST",
		TargetURL:       "https://api.example.com/users",
		TotalMutations:  20,
		Handled4xx:      15,
		Success2xx:      2,
		ServerErrors5xx: 2,
		TransportErrors: 1,
		Anomalies: []fuzz.Anomaly{
			{
				MutationName: "SQLi Probe",
				Category:     "injection",
				StatusCode:   500,
				DurationMs:   120.5,
				ResponseBody: "Internal Server Error: database syntax near ''",
				Issue:        "Server returned HTTP 500 (Unhandled internal error)",
				Severity:     "CRITICAL",
			},
			{
				MutationName: "Large Buffer",
				Category:     "buffer_overflow",
				StatusCode:   0,
				DurationMs:   5002.0,
				Issue:        "Server hang / Request timed out (> 5s)",
				Severity:     "HIGH",
			},
			{
				MutationName: "Connection Drop",
				Category:     "headers",
				StatusCode:   0,
				DurationMs:   12.0,
				Issue:        "Transport error: connection reset by peer",
				Severity:     "HIGH",
			},
		},
		ElapsedMs: 6200.0,
	}

	doc := BuildFuzzSARIF(result, "collections/users/create.yaml")
	if doc.Version != "2.1.0" {
		t.Errorf("expected SARIF version 2.1.0, got %s", doc.Version)
	}
	if len(doc.Runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(doc.Runs))
	}
	run := doc.Runs[0]
	if run.Tool.Driver.Name != "hit-fuzz" {
		t.Errorf("expected driver name hit-fuzz, got %s", run.Tool.Driver.Name)
	}
	if len(run.Results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(run.Results))
	}

	// Result 1: 5xx Crash
	r1 := run.Results[0]
	if r1.RuleID != "HIT-FUZZ-5XX" {
		t.Errorf("expected rule HIT-FUZZ-5XX, got %s", r1.RuleID)
	}
	if r1.Level != "error" {
		t.Errorf("expected level error, got %s", r1.Level)
	}
	if len(r1.Locations) == 0 || r1.Locations[0].PhysicalLocation.ArtifactLocation.URI != "collections/users/create.yaml" {
		t.Errorf("unexpected location: %+v", r1.Locations)
	}

	// Result 2: Hang / Timeout
	r2 := run.Results[1]
	if r2.RuleID != "HIT-FUZZ-HANG" {
		t.Errorf("expected rule HIT-FUZZ-HANG, got %s", r2.RuleID)
	}
	if r2.Level != "warning" {
		t.Errorf("expected level warning, got %s", r2.Level)
	}

	// Result 3: Transport error
	r3 := run.Results[2]
	if r3.RuleID != "HIT-FUZZ-TRANSPORT" {
		t.Errorf("expected rule HIT-FUZZ-TRANSPORT, got %s", r3.RuleID)
	}
	if r3.Level != "warning" {
		t.Errorf("expected level warning, got %s", r3.Level)
	}

	// Test writing to file
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "fuzz-report.sarif")
	if err := WriteFuzzSARIF(result, "collections/users/create.yaml", outPath); err != nil {
		t.Fatalf("failed to write SARIF file: %v", err)
	}

	b, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("failed to read SARIF file: %v", err)
	}

	var parsed SarifDocument
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatalf("failed to parse generated SARIF JSON: %v", err)
	}
	if len(parsed.Runs[0].Results) != 3 {
		t.Errorf("expected 3 results in parsed SARIF, got %d", len(parsed.Runs[0].Results))
	}
}
