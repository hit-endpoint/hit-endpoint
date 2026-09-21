package report

import (
	"fmt"
	"strings"
	"testing"

	"hit/internal/history"
)

func TestAnalyzeLatency(t *testing.T) {
	// Generate 10 entries for pets/list: 5 fast (10ms), then 5 slow (50ms) -> +400% regression
	var entries []*history.Entry

	for i := 0; i < 5; i++ {
		entries = append(entries, &history.Entry{
			Timestamp: fmt.Sprintf("2026-09-09T10:0%d:00Z", i),
			Ref:       "pets/list",
			Method:    "GET",
			ElapsedMs: 10.0,
		})
	}
	for i := 5; i < 10; i++ {
		entries = append(entries, &history.Entry{
			Timestamp: fmt.Sprintf("2026-09-09T10:0%d:00Z", i),
			Ref:       "pets/list",
			Method:    "GET",
			ElapsedMs: 50.0,
		})
	}

	// 4 fast entries for auth/login: all 20ms -> stable
	for i := 0; i < 4; i++ {
		entries = append(entries, &history.Entry{
			Timestamp: fmt.Sprintf("2026-09-09T10:1%d:00Z", i),
			Ref:       "auth/login",
			Method:    "POST",
			ElapsedMs: 20.0,
		})
	}

	rep := AnalyzeLatency(entries, 25.0) // 25% threshold

	if rep.TotalHits != 14 {
		t.Fatalf("expected 14 total hits, got %d", rep.TotalHits)
	}
	if rep.EndpointsCount != 2 {
		t.Fatalf("expected 2 endpoints, got %d", rep.EndpointsCount)
	}
	if rep.RegressionAlerts != 1 {
		t.Fatalf("expected 1 regression alert, got %d", rep.RegressionAlerts)
	}

	var petEp *EndpointLatency
	for i := range rep.Endpoints {
		if rep.Endpoints[i].Ref == "pets/list" {
			petEp = &rep.Endpoints[i]
		}
	}
	if petEp == nil {
		t.Fatalf("missing pets/list endpoint")
	}
	if !petEp.Regressed {
		t.Errorf("expected pets/list to be flagged as regressed")
	}
	if petEp.DeltaPercent < 100.0 {
		t.Errorf("expected large positive delta percent, got %.1f%%", petEp.DeltaPercent)
	}

	tableStr := FormatLatencyTable(rep, false)
	if !strings.Contains(tableStr, "1 performance regression(s) detected") {
		t.Errorf("missing regression alert in table: %s", tableStr)
	}
	if !strings.Contains(tableStr, "pets/list") {
		t.Errorf("missing pets/list in table: %s", tableStr)
	}
}
