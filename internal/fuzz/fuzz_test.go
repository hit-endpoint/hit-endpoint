package fuzz

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGenerateMutations(t *testing.T) {
	method := "POST"
	rawURL := "http://127.0.0.1:8765/pets"
	headers := map[string]string{
		"Authorization": "Bearer demo-token",
		"Content-Type":  "application/json",
	}
	query := map[string]string{
		"kind": "dog",
	}
	body := []byte(`{"name":"Rex","age":3,"active":true}`)

	mutations := GenerateMutations(method, rawURL, headers, query, body, nil)
	if len(mutations) < 15 {
		t.Fatalf("expected at least 15 mutations across categories, got %d", len(mutations))
	}

	// Verify categories present
	cats := make(map[string]int)
	for _, m := range mutations {
		cats[m.Category]++
	}

	for _, reqCat := range []string{"boundaries", "types", "strings", "injection", "nulls", "headers"} {
		if cats[reqCat] == 0 {
			t.Errorf("expected mutations in category %q, got 0", reqCat)
		}
	}
}

func TestRunFuzzExecution(t *testing.T) {
	// Mock server that returns 400 for bad input, but crashes (500) if age > 1000000
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer malformed.jwt.token.truncated" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if ageVal, ok := payload["age"].(float64); ok && ageVal > 1000000 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"fatal integer overflow crash"}`))
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer ts.Close()

	headers := map[string]string{
		"Authorization": "Bearer valid-token",
		"Content-Type":  "application/json",
	}
	body := []byte(`{"name":"Rex","age":3}`)

	res, err := Run("POST", ts.URL, headers, nil, body, FuzzOptions{
		MaxMutations: 20,
	})
	if err != nil {
		t.Fatalf("unexpected error running fuzzer: %v", err)
	}

	if res.TotalMutations == 0 {
		t.Fatalf("expected mutations to run, got 0")
	}

	// Should have detected the 500 server crash on boundary integer overflow
	if res.ServerErrors5xx == 0 {
		t.Errorf("expected at least one 5xx server error detected, got %d", res.ServerErrors5xx)
	}
	if len(res.Anomalies) == 0 {
		t.Errorf("expected anomalies flagged for 500 error, got 0")
	}
	anomalyFound := false
	for _, a := range res.Anomalies {
		if a.StatusCode == 500 && strings.Contains(a.Issue, "500") {
			anomalyFound = true
			break
		}
	}
	if !anomalyFound {
		t.Errorf("expected 500 anomaly in report, got: %+v", res.Anomalies)
	}
}
