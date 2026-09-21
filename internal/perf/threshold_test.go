package perf

import (
	"testing"

	"hit/internal/types"
)

func TestParseThresholds(t *testing.T) {
	rules, err := ParseThresholds("p95<200ms, errors<1%, rps>=50, failed==0")
	if err != nil {
		t.Fatalf("ParseThresholds failed: %v", err)
	}
	if len(rules) != 4 {
		t.Fatalf("expected 4 rules, got %d", len(rules))
	}

	if rules[0].Metric != "p95" || rules[0].Op != "<" || rules[0].Target != 200 {
		t.Errorf("unexpected rule 0: %+v", rules[0])
	}
	if rules[1].Metric != "errors" || rules[1].Op != "<" || rules[1].Target != 0.01 || !rules[1].IsPercent {
		t.Errorf("unexpected rule 1: %+v", rules[1])
	}
	if rules[2].Metric != "rps" || rules[2].Op != ">=" || rules[2].Target != 50 {
		t.Errorf("unexpected rule 2: %+v", rules[2])
	}
	if rules[3].Metric != "failed" || rules[3].Op != "==" || rules[3].Target != 0 {
		t.Errorf("unexpected rule 3: %+v", rules[3])
	}

	// Test invalid
	_, err = ParseThresholds("not-a-valid-rule")
	if err == nil {
		t.Errorf("expected error for invalid rule")
	}
}

func TestEvaluateThresholds(t *testing.T) {
	rep := &types.PerfReport{
		Completed:   100,
		OK:          98,
		Failed:      2,
		DurationS:   2.0, // 50 rps
		LatenciesMs: []float64{10, 20, 30, 40, 50, 60, 70, 80, 90, 100},
	}

	// Passing rules
	rules, err := ParseThresholds("p95<150ms, mean<70ms, errors<=5, failed<5%, rps>=40")
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	results := EvaluateThresholds(rep, rules)
	for _, res := range results {
		if !res.Passed {
			t.Errorf("expected rule %s to pass, but failed: %s", res.Rule.Raw, res.Message)
		}
	}

	// Failing rules
	failingRules, _ := ParseThresholds("p95<50ms, failed==0, rps>100, errors<1%")
	failingResults := EvaluateThresholds(rep, failingRules)
	for _, res := range failingResults {
		if res.Passed {
			t.Errorf("expected rule %s to fail, but passed: %s", res.Rule.Raw, res.Message)
		}
	}
}
