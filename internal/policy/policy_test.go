package policy

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hit-endpoint/hit-endpoint/internal/spec"
)

func TestCheckRequestSpec_MissingStatus(t *testing.T) {
	cfg := &PolicyConfig{
		RequireAssertions: []string{"status"},
	}

	sp := spec.NewRequestSpec()
	sp.Tests = []map[string]any{
		{"max_ms": 500},
	}

	violations := CheckRequestSpec("collections/users/get.yaml", sp, "", cfg)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation for missing status, got %d", len(violations))
	}
	if violations[0].Rule != "require_assertions.status" {
		t.Errorf("expected rule require_assertions.status, got %s", violations[0].Rule)
	}
	if violations[0].Severity != SeverityError {
		t.Errorf("expected severity ERROR, got %s", violations[0].Severity)
	}
}

func TestCheckRequestSpec_MaxLatencyBreach(t *testing.T) {
	cfg := &PolicyConfig{
		MaxLatency: 1000 * time.Millisecond,
	}

	sp := spec.NewRequestSpec()
	sp.Tests = []map[string]any{
		{"status": 200},
		{"max_ms": 2500},
	}

	violations := CheckRequestSpec("collections/checkout.yaml", sp, "", cfg)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation for max_latency breach, got %d", len(violations))
	}
	if violations[0].Rule != "max_latency" {
		t.Errorf("expected rule max_latency, got %s", violations[0].Rule)
	}
}

func TestCheckRequestSpec_DisallowInsecureTLS(t *testing.T) {
	cfg := &PolicyConfig{
		DisallowInsecureTLS: true,
	}

	sp := spec.NewRequestSpec()
	sp.Verify = false

	violations := CheckRequestSpec("collections/prod-login.yaml", sp, "", cfg)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation for insecure TLS, got %d", len(violations))
	}
	if violations[0].Rule != "disallow_insecure_tls" {
		t.Errorf("expected rule disallow_insecure_tls, got %s", violations[0].Rule)
	}
}

func TestCheckRequestSpec_HardcodedSecrets(t *testing.T) {
	cfg := &PolicyConfig{
		DisallowHardcodedSecrets: true,
	}

	sp := spec.NewRequestSpec()
	sp.Headers["Authorization"] = "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.doNotCommit"

	violations := CheckRequestSpec("collections/users/list.yaml", sp, "", cfg)
	if len(violations) == 0 {
		t.Fatal("expected violation for hardcoded JWT bearer token")
	}
	found := false
	for _, v := range violations {
		if v.Rule == "disallow_hardcoded_secrets" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected disallow_hardcoded_secrets violation")
	}
}

func TestCheckRequestSpec_Compliant(t *testing.T) {
	cfg := &PolicyConfig{
		RequireAssertions:        []string{"status", "latency"},
		MaxLatency:               1500 * time.Millisecond,
		DisallowInsecureTLS:      true,
		DisallowHardcodedSecrets: true,
		RequireDescription:       true,
	}

	sp := spec.NewRequestSpec()
	sp.Description = "Fetches user profile by ID"
	sp.Verify = true
	sp.Headers["Authorization"] = "Bearer {{auth_token}}"
	sp.Tests = []map[string]any{
		{"status": 200},
		{"max_ms": 500},
	}

	violations := CheckRequestSpec("collections/users/get.yaml", sp, "", cfg)
	if len(violations) != 0 {
		t.Fatalf("expected 0 violations for compliant spec, got %d: %+v", len(violations), violations)
	}
}

func TestPolicyReport_WriteJUnitReport(t *testing.T) {
	rep := &PolicyReport{
		ZoneRoot:   "/workspace",
		Scope:      "all",
		TotalFiles: 5,
		Passed:     false,
		Violations: []Violation{
			{
				File:        "collections/get-users.yaml",
				Rule:        "require_assertions.status",
				Severity:    SeverityError,
				Message:     "missing status assertion",
				Remediation: "add status: 200",
			},
		},
	}

	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "policy-junit.xml")
	if err := rep.WriteJUnitReport(outPath); err != nil {
		t.Fatalf("failed to write policy junit report: %v", err)
	}

	b, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}

	var parsed policyJUnitSuites
	if err := xml.Unmarshal(b, &parsed); err != nil {
		t.Fatalf("failed to parse generated xml: %v", err)
	}
	if parsed.Failures != 1 {
		t.Errorf("expected 1 failure in junit, got %d", parsed.Failures)
	}
}

func TestParsePolicyYAML_RulesWrapperAndUnknownKeys(t *testing.T) {
	raw := []byte(`
rules:
  require_assertions:
    - status
    - latency
  max_latency: 1500ms
  disallow_insecure_tls: true
  disallow_hardcoded_secrets: true
  require_description: true
  require_auth: true
`)
	cfg, err := ParsePolicyYAML(raw)
	if err != nil {
		t.Fatalf("ParsePolicyYAML failed on rules: wrapper: %v", err)
	}
	if len(cfg.RequireAssertions) != 2 || cfg.MaxLatency != 1500*time.Millisecond || !cfg.RequireDescription || !cfg.RequireAuth {
		t.Errorf("unexpected parsed config: %+v", cfg)
	}

	_, err = ParsePolicyYAML([]byte("unknown_wrapper:\n  require_auth: true\n"))
	if err == nil {
		t.Errorf("expected error for unknown top-level policy key")
	}
}

func TestCheckRequestSpec_RequireDescriptionAndAuth(t *testing.T) {
	cfg := &PolicyConfig{
		RequireDescription: true,
		RequireAuth:        true,
	}
	sp := spec.NewRequestSpec()
	sp.Name = "01-list" // fallback stem name
	raw := "method: GET\nurl: /pets\n"

	violations := CheckRequestSpec("collections/pets/01-list.yaml", sp, raw, cfg)
	if len(violations) != 2 {
		t.Fatalf("expected 2 violations (description + auth), got %d: %+v", len(violations), violations)
	}

	rep := &PolicyReport{
		TotalFiles: 1,
		Passed:     true,
		Violations: violations,
	}
	rep.ApplyStrict(true)
	if rep.Passed {
		t.Errorf("expected ApplyStrict(true) to mark report with warnings as failed")
	}
}

func TestPolicyReport_WriteJUnitReport_MultipleViolationsPerFile(t *testing.T) {
	rep := &PolicyReport{
		ZoneRoot:   "/workspace",
		Scope:      "all",
		TotalFiles: 5,
		Passed:     false,
		Violations: []Violation{
			{File: "a.yaml", Rule: "r1", Severity: SeverityError, Message: "m1"},
			{File: "a.yaml", Rule: "r2", Severity: SeverityError, Message: "m2"},
			{File: "b.yaml", Rule: "r1", Severity: SeverityError, Message: "m3"},
		},
	}
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "policy-junit.xml")
	if err := rep.WriteJUnitReport(outPath); err != nil {
		t.Fatalf("WriteJUnitReport failed: %v", err)
	}
	b, _ := os.ReadFile(outPath)
	var parsed policyJUnitSuites
	if err := xml.Unmarshal(b, &parsed); err != nil {
		t.Fatalf("xml unmarshal failed: %v", err)
	}
	if parsed.Failures != 3 {
		t.Errorf("expected 3 failures, got %d", parsed.Failures)
	}
	if parsed.Tests < parsed.Failures || parsed.Tests != len(parsed.Suites[0].Cases) {
		t.Errorf("inconsistent JUnit counts: tests=%d, failures=%d, cases=%d", parsed.Tests, parsed.Failures, len(parsed.Suites[0].Cases))
	}
}

