package learn

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hit-endpoint/hit-endpoint/internal/history"
)

func TestLessonVerifier(t *testing.T) {
	// Mock HTTP server responding 200 on /health
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	v := NewVerifier(nil, ts.URL)
	v.history = []*history.Entry{
		{
			Method: "GET",
			Url:    ts.URL + "/health",
			Status: 200,
		},
		{
			Method: "GET",
			Url:    ts.URL + "/pets?kind=dog",
			Status: 200,
		},
		{
			Method: "POST",
			Url:    ts.URL + "/pets",
			Status: 201,
			Body:   `{"name":"Rex","kind":"dog"}`,
		},
		{
			Method: "POST",
			Url:    ts.URL + "/auth/login",
			Status: 200,
			Body:   `{"username":"mark"}`,
		},
		{
			Method:  "GET",
			Url:     ts.URL + "/pets",
			Status:  200,
			Headers: map[string]string{"Authorization": "Bearer demo-token-123"},
		},
		{
			Method: "GET",
			Url:    ts.URL + "/pets/1",
			Status: 200,
			Tests: []history.TestSummary{
				{Name: "status is 200", Passed: true},
			},
		},
		{
			Source: "flow",
			Ref:    "flows/smoke.yaml",
			Method: "GET",
			Url:    ts.URL + "/health",
			Status: 200,
		},
	}

	// Verify Lesson 1
	l1 := v.VerifyLesson(1)
	if !l1.Passed {
		t.Errorf("expected Lesson 1 to pass, got failed checks: %+v", l1.Checks)
	}

	// Verify Lesson 2
	l2 := v.VerifyLesson(2)
	if !l2.Passed {
		t.Errorf("expected Lesson 2 to pass, got failed checks: %+v", l2.Checks)
	}

	// Verify Lesson 3
	l3 := v.VerifyLesson(3)
	if !l3.Passed {
		t.Errorf("expected Lesson 3 to pass, got failed checks: %+v", l3.Checks)
	}

	// Verify Lesson 4
	l4 := v.VerifyLesson(4)
	if !l4.Passed {
		t.Errorf("expected Lesson 4 to pass, got failed checks: %+v", l4.Checks)
	}

	// Verify Lesson 5
	l5 := v.VerifyLesson(5)
	if !l5.Passed {
		t.Errorf("expected Lesson 5 to pass, got failed checks: %+v", l5.Checks)
	}

	// Verify All
	report := v.VerifyAll()
	if report.TotalLessons != 7 {
		t.Errorf("expected 7 total lessons, got %d", report.TotalLessons)
	}
	if report.CompletedLessons < 5 {
		t.Errorf("expected at least 5 completed lessons, got %d", report.CompletedLessons)
	}
}
