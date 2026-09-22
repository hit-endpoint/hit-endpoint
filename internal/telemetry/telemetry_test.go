package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/hit-endpoint/hit-endpoint/internal/assertions"
	"github.com/hit-endpoint/hit-endpoint/internal/types"
)

type roundTripFunc func(req *http.Request) *http.Response

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req), nil
}

func TestBuildTelemetryPayload(t *testing.T) {
	results := []*types.Result{
		{
			Ref:       "users/get",
			Method:    "GET",
			Url:       "https://api.example.com/users/123",
			Status:    200,
			HasStatus: true,
			ElapsedMs: 40.0,
			Tests: []assertions.TestResult{
				{Name: "status is 200", Passed: true},
			},
		},
		{
			Ref:       "users/post",
			Method:    "POST",
			Url:       "https://api.example.com/users",
			Status:    201,
			HasStatus: true,
			ElapsedMs: 80.0,
			Tests: []assertions.TestResult{
				{Name: "status is 201", Passed: true},
			},
		},
		{
			Ref:       "users/delete",
			Method:    "DELETE",
			Url:       "https://api.example.com/users/123",
			Status:    500,
			HasStatus: true,
			ElapsedMs: 120.0,
			Tests: []assertions.TestResult{
				{Name: "status is 204", Passed: false, Detail: "got 500"},
			},
		},
	}

	maskFn := func(s string) string {
		return strings.ReplaceAll(s, "123", "[MASKED]")
	}

	payload := BuildTelemetryPayload(results, nil, nil, "staging", maskFn)

	if payload.Summary.Total != 3 {
		t.Errorf("expected 3 total requests, got %d", payload.Summary.Total)
	}
	if payload.Summary.Passed != 2 {
		t.Errorf("expected 2 passed requests, got %d", payload.Summary.Passed)
	}
	if payload.Summary.Failed != 1 {
		t.Errorf("expected 1 failed request, got %d", payload.Summary.Failed)
	}

	// Verify secret masking
	if strings.Contains(payload.Endpoints[0].URL, "123") {
		t.Errorf("expected URL to be masked, got %s", payload.Endpoints[0].URL)
	}

	// Verify percentiles
	if p50, ok := payload.Percentiles["p50"]; !ok || p50 <= 0 {
		t.Errorf("expected p50 percentile, got %v", p50)
	}
	if p99, ok := payload.Percentiles["p99"]; !ok || p99 <= 0 {
		t.Errorf("expected p99 percentile, got %v", p99)
	}

	// Verify node identity
	if payload.Node.ID == "" {
		t.Error("expected non-empty Node ID")
	}
	if payload.Node.CommonName == "" {
		t.Error("expected non-empty Node CommonName")
	}
	if payload.Node.Type == "" {
		t.Error("expected non-empty Node Type")
	}
}

func TestDetectNodeContext(t *testing.T) {
	// 1. Developer Workstation
	gitDev := GitContext{Author: "alice@acme.com"}
	ciDev := CIContext{}
	nodeDev := DetectNodeContext(gitDev, ciDev)

	if nodeDev.Type != "developer" {
		t.Errorf("expected developer type, got %s", nodeDev.Type)
	}
	if nodeDev.Owner != "alice@acme.com" {
		t.Errorf("expected alice@acme.com owner, got %s", nodeDev.Owner)
	}
	if nodeDev.CommonName == "" {
		t.Error("expected common name to be populated")
	}

	// 2. CI Runner Context
	gitCI := GitContext{Author: "bob@acme.com"}
	ciEnv := CIContext{Provider: "github-actions", RunID: "8899"}
	nodeCI := DetectNodeContext(gitCI, ciEnv)

	if nodeCI.Type != "ci-runner" {
		t.Errorf("expected ci-runner type, got %s", nodeCI.Type)
	}
	if !strings.Contains(nodeCI.CommonName, "github-actions") {
		t.Errorf("expected common name to contain provider, got %s", nodeCI.CommonName)
	}
}

func TestPublishRun(t *testing.T) {
	var receivedPayload RunTelemetryPayload
	var authHeader string
	var projectHeader string
	called := false

	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) *http.Response {
			called = true
			authHeader = req.Header.Get("Authorization")
			projectHeader = req.Header.Get("X-Hit-Project")

			b, err := io.ReadAll(req.Body)
			if err != nil {
				return &http.Response{StatusCode: 500, Body: io.NopCloser(bytes.NewBufferString("read error"))}
			}

			if err := json.Unmarshal(b, &receivedPayload); err != nil {
				return &http.Response{StatusCode: 400, Body: io.NopCloser(bytes.NewBufferString("json error"))}
			}

			respData, _ := json.Marshal(PublishResult{
				RunID:        receivedPayload.RunID,
				DashboardURL: "https://app.hitendpoint.com/projects/test-proj/runs/" + receivedPayload.RunID,
				Status:       "ingested",
			})

			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(bytes.NewReader(respData)),
			}
		}),
	}

	cfg := PublishConfig{
		APIKey:    "hit_test_secret_key",
		ProjectID: "test-proj",
		CloudURL:  "https://mock.hitendpoint.com",
	}

	payload := &RunTelemetryPayload{
		Version:   "1.0",
		RunID:     "run_abc123",
		ProjectID: "test-proj",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Summary: RunSummary{
			Total:  10,
			Passed: 10,
		},
	}

	res, err := PublishRun(context.Background(), client, cfg, payload)
	if err != nil {
		t.Fatalf("unexpected publish error: %v", err)
	}

	if !called {
		t.Fatal("expected client to be called")
	}

	if authHeader != "Bearer hit_test_secret_key" {
		t.Errorf("expected auth header Bearer hit_test_secret_key, got %s", authHeader)
	}
	if projectHeader != "test-proj" {
		t.Errorf("expected project header test-proj, got %s", projectHeader)
	}
	if res.Status != "ingested" {
		t.Errorf("expected status ingested, got %s", res.Status)
	}
	if !strings.Contains(res.DashboardURL, "run_abc123") {
		t.Errorf("expected dashboard URL to contain run ID, got %s", res.DashboardURL)
	}
}

func TestPublishRun_ServerError(t *testing.T) {
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) *http.Response {
			return &http.Response{
				StatusCode: 502,
				Body:       io.NopCloser(bytes.NewBufferString("Bad Gateway")),
			}
		}),
	}

	cfg := PublishConfig{
		CloudURL: "https://mock.hitendpoint.com",
	}
	payload := &RunTelemetryPayload{
		RunID: "run_fail",
	}

	_, err := PublishRun(context.Background(), client, cfg, payload)
	if err == nil {
		t.Fatal("expected error on 502 Bad Gateway response")
	}
}
