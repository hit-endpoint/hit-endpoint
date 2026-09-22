package telemetry

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/hit-endpoint/hit-endpoint/internal/types"
	"github.com/hit-endpoint/hit-endpoint/internal/zone"
)

const (
	DefaultCloudURL     = "https://api.hitendpoint.com"
	DefaultDashboardURL = "https://app.hitendpoint.com"
)

// PublishConfig controls the cloud telemetry publishing parameters.
type PublishConfig struct {
	APIKey      string `json:"api_key,omitempty"`
	ProjectID   string `json:"project_id,omitempty"`
	CloudURL    string `json:"cloud_url,omitempty"`
	FailOnError bool   `json:"fail_on_error,omitempty"`
}

// GitContext captures source control provenance for regression and drift analysis.
type GitContext struct {
	Branch string `json:"branch,omitempty"`
	Commit string `json:"commit,omitempty"`
	Author string `json:"author,omitempty"`
	Dirty  bool   `json:"dirty,omitempty"`
}

// CIContext captures CI/CD pipeline metadata.
type CIContext struct {
	Provider string `json:"provider,omitempty"` // "github-actions", "gitlab-ci", "circleci", etc.
	Workflow string `json:"workflow,omitempty"`
	RunID    string `json:"run_id,omitempty"`
	PRNumber string `json:"pr_number,omitempty"`
}

// EndpointTelemetry records metrics and assertion outcomes for an individual endpoint.
type EndpointTelemetry struct {
	Name      string           `json:"name,omitempty"`
	Ref       string           `json:"ref,omitempty"`
	Method    string           `json:"method"`
	URL       string           `json:"url"`
	Status    int              `json:"status,omitempty"`
	ElapsedMs float64          `json:"elapsed_ms"`
	OK        bool             `json:"ok"`
	Error     string           `json:"error,omitempty"`
	Tests     []TestTelemetry  `json:"tests,omitempty"`
}

type TestTelemetry struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail,omitempty"`
}

// NodeContext captures machine identity, common name, and ownership attribution.
type NodeContext struct {
	ID         string `json:"id"`
	CommonName string `json:"common_name"`
	Owner      string `json:"owner"`
	Type       string `json:"type"` // "developer" | "ci-runner" | "infra"
	Hostname   string `json:"hostname,omitempty"`
	OS         string `json:"os,omitempty"`
}

// RunTelemetryPayload represents the complete payload sent to Hit Cloud.
type RunTelemetryPayload struct {
	Version     string              `json:"version"`
	RunID       string              `json:"run_id"`
	ProjectID   string              `json:"project_id,omitempty"`
	Timestamp   string              `json:"timestamp"`
	Zone        string              `json:"zone,omitempty"`
	Server      string              `json:"server,omitempty"`
	Git         GitContext          `json:"git"`
	CI          CIContext           `json:"ci"`
	Node        NodeContext         `json:"node"`
	Summary     RunSummary          `json:"summary"`
	Percentiles map[string]float64 `json:"percentiles"`
	Endpoints   []EndpointTelemetry `json:"endpoints"`
}

type RunSummary struct {
	Total      int     `json:"total"`
	Passed     int     `json:"passed"`
	Failed     int     `json:"failed"`
	DurationMs float64 `json:"duration_ms"`
}

// PublishResult contains the confirmation and dashboard URL from Hit Cloud.
type PublishResult struct {
	RunID        string `json:"run_id"`
	DashboardURL string `json:"dashboard_url"`
	Status       string `json:"status"`
}

// GenerateRunID returns a cryptographically random, timestamped unique run ID.
func GenerateRunID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("run_%x", b)
}

// DetectGitContext attempts to detect git repository state from environment or local git CLI.
func DetectGitContext(dir string) GitContext {
	ctx := GitContext{}

	// 1. Check CI environment variables first
	if sha := os.Getenv("GITHUB_SHA"); sha != "" {
		ctx.Commit = sha
		if len(ctx.Commit) > 8 {
			ctx.Commit = ctx.Commit[:8]
		}
		ctx.Branch = os.Getenv("GITHUB_REF_NAME")
		ctx.Author = os.Getenv("GITHUB_ACTOR")
		return ctx
	}
	if sha := os.Getenv("CI_COMMIT_SHA"); sha != "" {
		ctx.Commit = sha
		if len(ctx.Commit) > 8 {
			ctx.Commit = ctx.Commit[:8]
		}
		ctx.Branch = os.Getenv("CI_COMMIT_REF_NAME")
		ctx.Author = os.Getenv("GITLAB_USER_LOGIN")
		return ctx
	}

	// 2. Try git CLI if in git worktree
	if dir == "" {
		dir, _ = os.Getwd()
	}

	commitCmd := exec.Command("git", "-C", dir, "rev-parse", "--short", "HEAD")
	if out, err := commitCmd.Output(); err == nil {
		ctx.Commit = strings.TrimSpace(string(out))
	}

	branchCmd := exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD")
	if out, err := branchCmd.Output(); err == nil {
		ctx.Branch = strings.TrimSpace(string(out))
	}

	authorCmd := exec.Command("git", "-C", dir, "config", "user.email")
	if out, err := authorCmd.Output(); err == nil {
		ctx.Author = strings.TrimSpace(string(out))
	}

	diffCmd := exec.Command("git", "-C", dir, "status", "--porcelain")
	if out, err := diffCmd.Output(); err == nil {
		ctx.Dirty = len(strings.TrimSpace(string(out))) > 0
	}

	return ctx
}

// DetectCIContext detects active CI/CD runner environments.
func DetectCIContext() CIContext {
	if os.Getenv("GITHUB_ACTIONS") == "true" {
		return CIContext{
			Provider: "github-actions",
			Workflow: os.Getenv("GITHUB_WORKFLOW"),
			RunID:    os.Getenv("GITHUB_RUN_ID"),
			PRNumber: os.Getenv("GITHUB_REF"),
		}
	}
	if os.Getenv("GITLAB_CI") == "true" {
		return CIContext{
			Provider: "gitlab-ci",
			Workflow: os.Getenv("CI_PIPELINE_NAME"),
			RunID:    os.Getenv("CI_PIPELINE_ID"),
		}
	}
	if os.Getenv("CIRCLECI") == "true" {
		return CIContext{
			Provider: "circleci",
			Workflow: os.Getenv("CIRCLE_WORKFLOW_ID"),
			RunID:    os.Getenv("CIRCLE_BUILD_NUM"),
		}
	}
	if os.Getenv("JENKINS_URL") != "" {
		return CIContext{
			Provider: "jenkins",
			Workflow: os.Getenv("JOB_NAME"),
			RunID:    os.Getenv("BUILD_NUMBER"),
		}
	}
	return CIContext{}
}

// DetectNodeContext resolves machine identity, common name, and ownership attribution.
func DetectNodeContext(git GitContext, ci CIContext) NodeContext {
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "unknown-host"
	}
	osName := runtime.GOOS

	nodeName := os.Getenv("HIT_NODE_NAME")
	nodeOwner := os.Getenv("HIT_NODE_OWNER")
	nodeType := os.Getenv("HIT_NODE_TYPE")

	if ci.Provider != "" {
		if nodeType == "" {
			nodeType = "ci-runner"
		}
		if nodeOwner == "" {
			if git.Author != "" {
				nodeOwner = git.Author
			} else {
				nodeOwner = ci.Provider
			}
		}
		if nodeName == "" {
			if ci.RunID != "" {
				nodeName = fmt.Sprintf("%s-job-%s", ci.Provider, ci.RunID)
			} else {
				nodeName = fmt.Sprintf("%s-runner", ci.Provider)
			}
		}
	} else {
		if nodeType == "" {
			nodeType = "developer"
		}
		if nodeOwner == "" {
			if git.Author != "" {
				nodeOwner = git.Author
			} else if u := os.Getenv("USER"); u != "" {
				nodeOwner = u
			} else {
				nodeOwner = "local-developer"
			}
		}
		if nodeName == "" {
			nodeName = hostname
		}
	}

	idRaw := fmt.Sprintf("%s:%s:%s:%s", nodeType, nodeOwner, nodeName, hostname)
	h := sha256.Sum256([]byte(idRaw))
	nodeID := fmt.Sprintf("node_%x", h[:6])

	return NodeContext{
		ID:         nodeID,
		CommonName: nodeName,
		Owner:      nodeOwner,
		Type:       nodeType,
		Hostname:   hostname,
		OS:         osName,
	}
}

// BuildTelemetryPayload constructs the sanitized cloud telemetry payload from execution results.
func BuildTelemetryPayload(results []*types.Result, flowErrors []string, z *zone.Zone, serverName string, maskFn func(string) string) *RunTelemetryPayload {
	runID := GenerateRunID()
	zoneName := ""
	rootPath := ""
	if z != nil {
		zoneName = z.Name()
		rootPath = z.Root
	}

	if maskFn == nil {
		maskFn = func(s string) string { return s }
	}

	gitCtx := DetectGitContext(rootPath)
	ciCtx := DetectCIContext()
	nodeCtx := DetectNodeContext(gitCtx, ciCtx)

	payload := &RunTelemetryPayload{
		Version:     "1.0",
		RunID:       runID,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
		Zone:        zoneName,
		Server:      serverName,
		Git:         gitCtx,
		CI:          ciCtx,
		Node:        nodeCtx,
		Endpoints:   make([]EndpointTelemetry, 0, len(results)),
		Percentiles: make(map[string]float64),
	}

	var latencies []float64
	var totalDuration float64
	passedCount := 0
	failedCount := len(flowErrors)

	for _, r := range results {
		maskedURL := maskFn(r.Url)

		item := EndpointTelemetry{
			Name:      r.Name,
			Ref:       r.Ref,
			Method:    r.Method,
			URL:       maskedURL,
			Status:    r.Status,
			ElapsedMs: math.Round(r.ElapsedMs*10) / 10,
			OK:        r.OK(),
			Error:     maskFn(r.Error),
		}

		if r.OK() {
			passedCount++
		} else {
			failedCount++
		}

		if r.ElapsedMs > 0 {
			latencies = append(latencies, r.ElapsedMs)
			totalDuration += r.ElapsedMs
		}

		for _, t := range r.Tests {
			item.Tests = append(item.Tests, TestTelemetry{
				Name:   t.Name,
				Passed: t.Passed,
				Detail: maskFn(t.Detail),
			})
		}

		payload.Endpoints = append(payload.Endpoints, item)
	}

	payload.Summary = RunSummary{
		Total:      len(results),
		Passed:     passedCount,
		Failed:     failedCount,
		DurationMs: math.Round(totalDuration*10) / 10,
	}

	// Calculate latency percentiles
	if len(latencies) > 0 {
		sort.Float64s(latencies)
		pAt := func(pct float64) float64 {
			idx := int(math.Round(pct * float64(len(latencies)-1)))
			if idx < 0 {
				idx = 0
			}
			if idx >= len(latencies) {
				idx = len(latencies) - 1
			}
			return math.Round(latencies[idx]*10) / 10
		}
		payload.Percentiles["p50"] = pAt(0.50)
		payload.Percentiles["p90"] = pAt(0.90)
		payload.Percentiles["p95"] = pAt(0.95)
		payload.Percentiles["p99"] = pAt(0.99)
	}

	return payload
}

// PublishRun uploads the execution telemetry payload to the Hit Cloud platform.
func PublishRun(ctx context.Context, client *http.Client, cfg PublishConfig, payload *RunTelemetryPayload) (*PublishResult, error) {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}

	cloudURL := cfg.CloudURL
	if cloudURL == "" {
		cloudURL = DefaultCloudURL
	}
	cloudURL = strings.TrimRight(cloudURL, "/")

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to encode telemetry JSON: %w", err)
	}

	endpoint := cloudURL
	if !strings.HasSuffix(endpoint, "/runs") {
		if strings.HasSuffix(endpoint, "/v1") || strings.HasSuffix(endpoint, "/api/v1") {
			endpoint = endpoint + "/runs"
		} else {
			endpoint = endpoint + "/api/v1/runs"
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to create publish request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "hit/0.1.0 (cloud-telemetry)")
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", cfg.APIKey))
	}
	if cfg.ProjectID != "" {
		req.Header.Set("X-Hit-Project", cfg.ProjectID)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cloud upload failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodySnippet, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return nil, fmt.Errorf("cloud platform returned HTTP %d: %s", resp.StatusCode, string(bodySnippet))
	}

	var res PublishResult
	bodyBytes, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(bodyBytes, &res); err != nil || res.DashboardURL == "" {
		res.RunID = payload.RunID
		res.DashboardURL = fmt.Sprintf("%s/runs/%s", DefaultDashboardURL, payload.RunID)
		res.Status = "received"
	}

	return &res, nil
}
