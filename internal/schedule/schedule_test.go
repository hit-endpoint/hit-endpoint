package schedule

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestParseStartTime(t *testing.T) {
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)

	// 1. "now"
	t1, err := ParseStartTime("now", now)
	if err != nil {
		t.Fatalf("ParseStartTime now failed: %v", err)
	}
	if !t1.Equal(now) {
		t.Errorf("expected %v, got %v", now, t1)
	}

	// 2. Relative offset "+10s"
	t2, err := ParseStartTime("+10s", now)
	if err != nil {
		t.Fatalf("ParseStartTime +10s failed: %v", err)
	}
	if !t2.Equal(now.Add(10 * time.Second)) {
		t.Errorf("expected %v, got %v", now.Add(10*time.Second), t2)
	}

	// 3. Time today "14:30"
	t3, err := ParseStartTime("14:30", now)
	if err != nil {
		t.Fatalf("ParseStartTime 14:30 failed: %v", err)
	}
	if t3.Hour() != 14 || t3.Minute() != 30 {
		t.Errorf("expected 14:30, got %v", t3)
	}

	// 4. Time that passed today "10:00" -> should be scheduled for tomorrow
	t4, err := ParseStartTime("10:00", now)
	if err != nil {
		t.Fatalf("ParseStartTime 10:00 failed: %v", err)
	}
	if t4.Day() != now.Day()+1 || t4.Hour() != 10 {
		t.Errorf("expected tomorrow 10:00, got %v", t4)
	}
}

func TestScheduleRun(t *testing.T) {
	requestCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"status":"healthy","count":%d}`, requestCount)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var tickIndices []int
	summary, err := Run(ctx, ScheduleOptions{
		Target:        srv.URL,
		StartTime:     time.Now(),
		Interval:      10 * time.Millisecond,
		Count:         3,
		ExpectStatus:  200,
		ExpectPattern: "healthy",
		OnTick: func(tick ScheduleTick) {
			tickIndices = append(tickIndices, tick.Index)
		},
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if summary.TotalRuns != 3 {
		t.Errorf("expected 3 total runs, got %d", summary.TotalRuns)
	}
	if summary.PassedRuns != 3 {
		t.Errorf("expected 3 passed runs, got %d", summary.PassedRuns)
	}
	if summary.FailedRuns != 0 {
		t.Errorf("expected 0 failed runs, got %d", summary.FailedRuns)
	}
	if len(tickIndices) != 3 {
		t.Errorf("expected 3 tick callbacks, got %d", len(tickIndices))
	}
}

func TestScheduleFailureExpectation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `{"error":"internal crash"}`)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	summary, err := Run(ctx, ScheduleOptions{
		Target:        srv.URL,
		StartTime:     time.Now(),
		Count:         1,
		ExpectStatus:  200,
		ExpectPattern: "healthy",
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if summary.TotalRuns != 1 {
		t.Errorf("expected 1 run, got %d", summary.TotalRuns)
	}
	if summary.PassedRuns != 0 {
		t.Errorf("expected 0 passed, got %d", summary.PassedRuns)
	}
	if summary.FailedRuns != 1 {
		t.Errorf("expected 1 failed, got %d", summary.FailedRuns)
	}
	if summary.Ticks[0].StatusMatch {
		t.Errorf("expected StatusMatch to be false")
	}
	if summary.Ticks[0].PatternMatch {
		t.Errorf("expected PatternMatch to be false")
	}
}
