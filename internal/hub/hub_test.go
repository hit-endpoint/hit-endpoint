package hub

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"hit/internal/telemetry"
)

func TestHubStore_SaveAndQuery(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "hit_hub_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	store, err := NewStore(tempDir)
	if err != nil {
		t.Fatalf("failed to initialize store: %v", err)
	}

	// 1. Developer Workstation Run
	run1 := &telemetry.RunTelemetryPayload{
		RunID:     "run_dev_1",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		ProjectID: "payments-api",
		Git: telemetry.GitContext{
			Branch: "feat/checkout",
			Commit: "c1a2b3",
			Author: "alice@acme.com",
		},
		Node: telemetry.NodeContext{
			ID:         "node_alice_laptop",
			CommonName: "alices-macbook.local",
			Owner:      "alice@acme.com",
			Type:       "developer",
		},
		Summary: telemetry.RunSummary{
			Total:      5,
			Passed:     5,
			Failed:     0,
			DurationMs: 340.5,
		},
		Percentiles: map[string]float64{
			"p50": 20.0,
			"p95": 85.0,
		},
	}

	// 2. CI Runner Run
	run2 := &telemetry.RunTelemetryPayload{
		RunID:     "run_ci_1",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		ProjectID: "payments-api",
		Git: telemetry.GitContext{
			Branch: "main",
			Commit: "d4e5f6",
			Author: "bob@acme.com",
		},
		CI: telemetry.CIContext{
			Provider: "github-actions",
			Workflow: "CI Pipeline",
			RunID:    "94812",
		},
		Node: telemetry.NodeContext{
			ID:         "node_ci_runner_1",
			CommonName: "github-actions-job-94812",
			Owner:      "payments-team",
			Type:       "ci-runner",
		},
		Summary: telemetry.RunSummary{
			Total:      10,
			Passed:     8,
			Failed:     2,
			DurationMs: 1200.0,
		},
		Percentiles: map[string]float64{
			"p50": 45.0,
			"p95": 140.0,
		},
	}

	if err := store.SaveRun(run1); err != nil {
		t.Fatalf("failed to save run1: %v", err)
	}
	if err := store.SaveRun(run2); err != nil {
		t.Fatalf("failed to save run2: %v", err)
	}

	// Verify Nodes Discovered
	nodes := store.ListNodes()
	if len(nodes) != 2 {
		t.Fatalf("expected 2 discovered nodes, got %d", len(nodes))
	}

	// Verify Filter by Owner
	aliceRuns, totalAlice := store.ListRuns(RunFilter{Owner: "alice@acme.com"})
	if totalAlice != 1 || len(aliceRuns) != 1 {
		t.Errorf("expected 1 run for Alice, got %d", totalAlice)
	}
	if aliceRuns[0].RunID != "run_dev_1" {
		t.Errorf("expected run_dev_1, got %s", aliceRuns[0].RunID)
	}

	// Verify Filter by Type
	ciRuns, totalCI := store.ListRuns(RunFilter{Type: "ci-runner"})
	if totalCI != 1 || len(ciRuns) != 1 {
		t.Errorf("expected 1 CI run, got %d", totalCI)
	}
	if ciRuns[0].RunID != "run_ci_1" {
		t.Errorf("expected run_ci_1, got %s", ciRuns[0].RunID)
	}

	// Verify Summary
	summary := store.GetSummary()
	if summary.TotalRuns != 2 {
		t.Errorf("expected 2 total runs, got %d", summary.TotalRuns)
	}
	if summary.PassedRuns != 1 || summary.FailedRuns != 1 {
		t.Errorf("expected 1 passed and 1 failed run, got %d/%d", summary.PassedRuns, summary.FailedRuns)
	}
	if summary.TotalNodes != 2 {
		t.Errorf("expected 2 total nodes, got %d", summary.TotalNodes)
	}

	// Verify Persistence across fresh store reload
	store2, err := NewStore(tempDir)
	if err != nil {
		t.Fatalf("failed to reload store from disk: %v", err)
	}
	reloadedRuns, count := store2.ListRuns(RunFilter{})
	if count != 2 || len(reloadedRuns) != 2 {
		t.Errorf("expected 2 reloaded runs from disk, got %d", count)
	}
}

func TestHubServer_HTTPRoutes(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "hit_hub_server_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	server, err := NewServer(Config{
		Addr:      "127.0.0.1:0",
		DataDir:   tempDir,
		APIKey:    "test_pat_secret",
		PublicURL: "http://hub.example.com",
	})
	if err != nil {
		t.Fatalf("failed to create hub server: %v", err)
	}

	handler := server.httpServer.Handler

	// 1. Health check
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 health check, got %d", rr.Code)
	}

	// 2. Unauthorized Ingest
	payload := &telemetry.RunTelemetryPayload{
		RunID: "run_test_http",
		Node: telemetry.NodeContext{
			CommonName: "dev-pc",
			Owner:      "dev@acme.com",
			Type:       "developer",
		},
		Summary: telemetry.RunSummary{Total: 2, Passed: 2},
	}
	body, _ := json.Marshal(payload)

	req = httptest.NewRequest(http.MethodPost, "/api/v1/runs", bytes.NewReader(body))
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 unauthorized without token, got %d", rr.Code)
	}

	// 3. Authorized Ingest
	req = httptest.NewRequest(http.MethodPost, "/api/v1/runs", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer test_pat_secret")
	req.Header.Set("Content-Type", "application/json")
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201 created, got %d: %s", rr.Code, rr.Body.String())
	}

	var ingestResp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&ingestResp); err != nil {
		t.Fatalf("failed to decode ingest response: %v", err)
	}
	if ingestResp["run_id"] != "run_test_http" {
		t.Errorf("expected run_id run_test_http, got %v", ingestResp["run_id"])
	}
	if !strings.Contains(ingestResp["dashboard_url"].(string), "run_test_http") {
		t.Errorf("expected dashboard_url to contain run_id, got %v", ingestResp["dashboard_url"])
	}

	// 4. Query Runs via GET /api/v1/runs
	req = httptest.NewRequest(http.MethodGet, "/api/v1/runs", nil)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 from list runs, got %d", rr.Code)
	}

	// 5. Query Specific Run via GET /api/v1/runs/run_test_http
	req = httptest.NewRequest(http.MethodGet, "/api/v1/runs/run_test_http", nil)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 from get run, got %d", rr.Code)
	}

	// 6. Query Nodes via GET /api/v1/nodes
	req = httptest.NewRequest(http.MethodGet, "/api/v1/nodes", nil)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 from list nodes, got %d", rr.Code)
	}

	// 7. Render UI via GET /
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 from web dashboard, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "Hit Cloud Fleet Hub") {
		t.Errorf("expected HTML dashboard title in response")
	}
}

func TestHubStore_FleetTopologyAndLiveness(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "hit_hub_fleet_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	store, err := NewStore(tempDir)
	if err != nil {
		t.Fatalf("failed to initialize store: %v", err)
	}

	// Register 3 nodes
	node1, err := store.RegisterNode(NodeRegisterRequest{
		ID:           "worker-us-1",
		CommonName:   "worker-us-1.internal",
		Owner:        "infra-team",
		Type:         "perf-worker",
		Region:       "us-east",
		EndpointURL:  "http://10.0.1.10:9090",
		Capacity:     8,
		Capabilities: []string{"perf-worker", "http-load"},
	})
	if err != nil {
		t.Fatalf("failed to register node1: %v", err)
	}
	if node1.State != "idle" || node1.Capacity != 8 {
		t.Errorf("unexpected node1 state/capacity: %+v", node1)
	}

	_, err = store.RegisterNode(NodeRegisterRequest{
		ID:           "worker-eu-1",
		CommonName:   "worker-eu-1.internal",
		Owner:        "infra-team",
		Type:         "perf-worker",
		Region:       "eu-west",
		EndpointURL:  "http://10.0.2.10:9090",
		Capacity:     16,
		Capabilities: []string{"perf-worker"},
	})
	if err != nil {
		t.Fatalf("failed to register node2: %v", err)
	}

	_, err = store.RegisterNode(NodeRegisterRequest{
		ID:           "probe-us-1",
		CommonName:   "probe-us-1.internal",
		Owner:        "sre-team",
		Type:         "probe-runner",
		Region:       "us-east",
		EndpointURL:  "http://10.0.1.20:8081",
		Capacity:     4,
		Capabilities: []string{"probe-runner"},
	})
	if err != nil {
		t.Fatalf("failed to register node3: %v", err)
	}

	// Heartbeat node 1 with active load
	hbResp, err := store.HeartbeatNode("worker-us-1", NodeHeartbeatRequest{
		State:      "busy",
		ActiveJobs: 3,
		Metrics: map[string]any{
			"cpu_percent": 35.5,
			"memory_mb":   256.0,
		},
	})
	if err != nil {
		t.Fatalf("failed to heartbeat node1: %v", err)
	}
	if hbResp.State != "busy" || hbResp.ActiveJobs != 3 {
		t.Errorf("unexpected heartbeat response: %+v", hbResp)
	}

	// Verify Available Workers for perf-worker capability
	workers := store.GetAvailableWorkers("perf-worker")
	if len(workers) != 2 {
		t.Fatalf("expected 2 available perf workers, got %d", len(workers))
	}

	// Verify Fleet Topology
	topology := store.GetFleetTopology()
	if topology.TotalNodes != 3 {
		t.Errorf("expected 3 total nodes in topology, got %d", topology.TotalNodes)
	}
	if topology.TotalCapacity != 28 { // 8 + 16 + 4
		t.Errorf("expected 28 total capacity, got %d", topology.TotalCapacity)
	}
	if topology.ActiveJobs != 3 {
		t.Errorf("expected 3 active jobs, got %d", topology.ActiveJobs)
	}
	if len(topology.NodesByRegion["us-east"]) != 2 {
		t.Errorf("expected 2 nodes in us-east, got %d", len(topology.NodesByRegion["us-east"]))
	}
	if len(topology.NodesByRegion["eu-west"]) != 1 {
		t.Errorf("expected 1 node in eu-west, got %d", len(topology.NodesByRegion["eu-west"]))
	}

	// Test Liveness State Transitions:
	// Artificially age node2's heartbeat
	store.mu.Lock()
	store.nodes["worker-eu-1"].LastHeartbeat = time.Now().UTC().Add(-40 * time.Second)
	store.mu.Unlock()

	// Check liveness (degraded after 30s, offline after 60s)
	store.CheckLiveness(30*time.Second, 60*time.Second)
	n2, _ := store.GetNode("worker-eu-1")
	if n2.State != "degraded" {
		t.Errorf("expected worker-eu-1 to be degraded, got %s", n2.State)
	}

	// Further age node2's heartbeat to 70s
	store.mu.Lock()
	store.nodes["worker-eu-1"].LastHeartbeat = time.Now().UTC().Add(-70 * time.Second)
	store.mu.Unlock()

	store.CheckLiveness(30*time.Second, 60*time.Second)
	n2, _ = store.GetNode("worker-eu-1")
	if n2.State != "offline" {
		t.Errorf("expected worker-eu-1 to be offline, got %s", n2.State)
	}

	// Now available workers should only return worker-us-1 (since worker-eu-1 is offline)
	workersAfterOffline := store.GetAvailableWorkers("perf-worker")
	if len(workersAfterOffline) != 1 || workersAfterOffline[0].ID != "worker-us-1" {
		t.Errorf("expected 1 active perf worker after offline transition, got %d", len(workersAfterOffline))
	}

	// Deregister probe-us-1
	err = store.DeregisterNode("probe-us-1")
	if err != nil {
		t.Fatalf("failed to deregister probe-us-1: %v", err)
	}
	probeNode, _ := store.GetNode("probe-us-1")
	if probeNode.State != "offline" || probeNode.ActiveJobs != 0 {
		t.Errorf("expected probeNode offline with 0 jobs, got %s, %d", probeNode.State, probeNode.ActiveJobs)
	}
}

func TestHubServer_FleetHTTPRoutes(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "hit_hub_fleet_http_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	server, err := NewServer(Config{
		Addr:      "127.0.0.1:0",
		DataDir:   tempDir,
		APIKey:    "fleet_secret_key",
		PublicURL: "http://localhost:8080",
	})
	if err != nil {
		t.Fatalf("failed to create hub server: %v", err)
	}

	handler := server.httpServer.Handler

	// 1. Register Node via POST /api/v1/nodes/register (Unauthorized)
	regReq := NodeRegisterRequest{
		ID:           "test-worker-http",
		CommonName:   "worker-01",
		Owner:        "qa-lead",
		Type:         "perf-worker",
		Region:       "us-west",
		EndpointURL:  "http://10.0.3.5:9090",
		Capacity:     12,
		Capabilities: []string{"perf-worker"},
	}
	regBody, _ := json.Marshal(regReq)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/nodes/register", bytes.NewReader(regBody))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 unauthorized on register, got %d", rr.Code)
	}

	// 2. Register Node (Authorized)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/nodes/register", bytes.NewReader(regBody))
	req.Header.Set("Authorization", "Bearer fleet_secret_key")
	req.Header.Set("Content-Type", "application/json")
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated && rr.Code != http.StatusOK {
		t.Fatalf("expected 201 Created or 200 OK on register, got %d: %s", rr.Code, rr.Body.String())
	}

	// 3. Heartbeat Node via POST /api/v1/nodes/{id}/heartbeat
	hbReq := NodeHeartbeatRequest{
		State:      "busy",
		ActiveJobs: 4,
	}
	hbBody, _ := json.Marshal(hbReq)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/nodes/test-worker-http/heartbeat", bytes.NewReader(hbBody))
	req.Header.Set("Authorization", "Bearer fleet_secret_key")
	req.Header.Set("Content-Type", "application/json")
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on heartbeat, got %d: %s", rr.Code, rr.Body.String())
	}

	// 4. Query Fleet Topology via GET /api/v1/fleet
	req = httptest.NewRequest(http.MethodGet, "/api/v1/fleet", nil)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on GET /api/v1/fleet, got %d", rr.Code)
	}
	var topology FleetTopology
	if err := json.NewDecoder(rr.Body).Decode(&topology); err != nil {
		t.Fatalf("failed to decode topology: %v", err)
	}
	if topology.TotalNodes != 1 || topology.ActiveJobs != 4 || topology.TotalCapacity != 12 {
		t.Errorf("unexpected topology values: %+v", topology)
	}

	// 5. Query Fleet Workers via GET /api/v1/fleet/workers
	req = httptest.NewRequest(http.MethodGet, "/api/v1/fleet/workers?capability=perf-worker", nil)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on GET /api/v1/fleet/workers, got %d", rr.Code)
	}
	var workersResp struct {
		Workers []*NodeRecord `json:"workers"`
		Total   int           `json:"total"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&workersResp); err != nil {
		t.Fatalf("failed to decode workers resp: %v", err)
	}
	workers := workersResp.Workers
	if len(workers) != 1 || workers[0].EndpointURL != "http://10.0.3.5:9090" {
		t.Errorf("unexpected workers result: %+v", workers)
	}

	// 6. Deregister Node via POST /api/v1/nodes/{id}/deregister
	req = httptest.NewRequest(http.MethodPost, "/api/v1/nodes/test-worker-http/deregister", nil)
	req.Header.Set("Authorization", "Bearer fleet_secret_key")
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on deregister, got %d", rr.Code)
	}

	// After deregistration, worker should not be returned in available workers
	req = httptest.NewRequest(http.MethodGet, "/api/v1/fleet/workers", nil)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	workersResp.Workers = nil
	json.NewDecoder(rr.Body).Decode(&workersResp)
	if len(workersResp.Workers) != 0 {
		t.Errorf("expected 0 active workers after deregistration, got %d", len(workersResp.Workers))
	}
}

func TestHubStore_DispatchJobLifecycle(t *testing.T) {
	store, err := NewStore("")
	if err != nil {
		t.Fatalf("failed to initialize store: %v", err)
	}

	// Register a worker in us-east
	_, err = store.RegisterNode(NodeRegisterRequest{
		ID:           "node-east-1",
		CommonName:   "Worker East 1",
		Type:         "perf-worker",
		Region:       "us-east",
		Capacity:     10,
		Capabilities: []string{"perf-worker", "test-runner"},
	})
	if err != nil {
		t.Fatalf("failed to register node: %v", err)
	}

	// 1. Submit a dispatch request targeting us-east
	job, err := store.CreateJob(DispatchRequest{
		Type:         "test",
		Name:         "Health Check Test",
		TargetRegion: "us-east",
		SpecYAML:     "url: http://api/health\nmethod: GET",
	})
	if err != nil {
		t.Fatalf("failed to create dispatch job: %v", err)
	}

	if job.State != "assigned" || job.AssignedNode != "node-east-1" {
		t.Fatalf("expected job assigned to node-east-1, got state=%s assigned=%s", job.State, job.AssignedNode)
	}

	// 2. Node claims job from queue
	claimed, ok := store.ClaimNextJob("node-east-1", "us-east", []string{"perf-worker", "test-runner"})
	if !ok || claimed == nil {
		t.Fatalf("expected node-east-1 to claim job")
	}
	if claimed.ID != job.ID || claimed.State != "running" {
		t.Errorf("expected claimed job in running state, got: %+v", claimed)
	}

	node, _ := store.GetNode("node-east-1")
	if node.State != "busy" || node.ActiveJobs != 1 {
		t.Errorf("expected node to be busy with 1 active job, got state=%s jobs=%d", node.State, node.ActiveJobs)
	}

	// 3. Append execution logs
	err = store.AppendJobLog(job.ID, JobEvent{
		Level:   "pass",
		Message: "GET /health returned 200 OK in 12ms",
	})
	if err != nil {
		t.Fatalf("failed to append log: %v", err)
	}

	// 4. Complete Job
	err = store.CompleteJob(job.ID, "completed", map[string]any{"ok": true}, "", "run_123")
	if err != nil {
		t.Fatalf("failed to complete job: %v", err)
	}

	completedJob, found := store.GetJob(job.ID)
	if !found || completedJob.State != "completed" {
		t.Errorf("expected completed job, got: %+v", completedJob)
	}
	if len(completedJob.Logs) < 3 { // initial queued log + claimed log + pass log + finished log
		t.Errorf("expected at least 3 logs, got %d", len(completedJob.Logs))
	}

	nodeAfter, _ := store.GetNode("node-east-1")
	if nodeAfter.State != "idle" || nodeAfter.ActiveJobs != 0 {
		t.Errorf("expected node to return to idle with 0 active jobs, got state=%s jobs=%d", nodeAfter.State, nodeAfter.ActiveJobs)
	}

	// 5. Test CancelJob
	job2, _ := store.CreateJob(DispatchRequest{
		Type:         "perf",
		TargetNodeID: "node-east-1",
		Requests:     500,
	})
	_ = store.CancelJob(job2.ID)
	canceledJob, _ := store.GetJob(job2.ID)
	if canceledJob.State != "canceled" {
		t.Errorf("expected canceled state, got %s", canceledJob.State)
	}
}

func TestHubServer_DispatchHTTPRoutes(t *testing.T) {
	server, err := NewServer(Config{
		Addr:   ":0",
		APIKey: "dispatch_secret",
	})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	handler := server.httpServer.Handler

	// 1. Register node
	regPayload := `{"id":"worker-dispatch-test","common_name":"Worker Dispatch","type":"perf-worker","region":"eu-central","capacity":8,"capabilities":["perf-worker"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/nodes/register", strings.NewReader(regPayload))
	req.Header.Set("Authorization", "Bearer dispatch_secret")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201 on register, got %d", rr.Code)
	}

	// 2. Submit dispatch via POST /api/v1/dispatch
	dispatchPayload := `{"type":"test","name":"API Sanity","target_node_id":"worker-dispatch-test","spec_yaml":"url: https://api.test/ping"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/dispatch", strings.NewReader(dispatchPayload))
	req.Header.Set("Authorization", "Bearer dispatch_secret")
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201 on dispatch submit, got %d (body: %s)", rr.Code, rr.Body.String())
	}
	var createdJob DispatchJob
	if err := json.NewDecoder(rr.Body).Decode(&createdJob); err != nil {
		t.Fatalf("failed to decode created job: %v", err)
	}
	if createdJob.ID == "" || createdJob.State != "assigned" {
		t.Fatalf("unexpected job response: %+v", createdJob)
	}

	// 3. Worker queries /api/v1/nodes/{id}/queue to claim job
	req = httptest.NewRequest(http.MethodGet, "/api/v1/nodes/worker-dispatch-test/queue?region=eu-central", nil)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 on queue poll, got %d", rr.Code)
	}
	var queueResp struct {
		Job *DispatchJob `json:"job"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&queueResp); err != nil {
		t.Fatalf("failed to decode queue response: %v", err)
	}
	if queueResp.Job == nil || queueResp.Job.ID != createdJob.ID || queueResp.Job.State != "running" {
		t.Fatalf("unexpected claimed job from queue: %+v", queueResp.Job)
	}

	// 4. Worker posts event via POST /api/v1/nodes/{id}/jobs/{job_id}/events
	eventPayload := `{"level":"info","message":"Step 1: Sent GET /ping"}`
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/nodes/worker-dispatch-test/jobs/%s/events", createdJob.ID), strings.NewReader(eventPayload))
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 on event post, got %d", rr.Code)
	}

	// 5. Worker completes job via POST /api/v1/nodes/{id}/jobs/{job_id}/complete
	completePayload := fmt.Sprintf(`{
		"state": "completed",
		"result": {"passed": true},
		"telemetry": {
			"run_id": "auto_run_%s",
			"project_id": "test-project",
			"node": {"id": "worker-dispatch-test", "common_name": "Worker Dispatch", "type": "perf-worker"},
			"summary": {"total": 1, "passed": 1, "failed": 0, "duration_ms": 42.0}
		}
	}`, createdJob.ID)
	req = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/nodes/worker-dispatch-test/jobs/%s/complete", createdJob.ID), strings.NewReader(completePayload))
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 on job complete, got %d", rr.Code)
	}

	// 6. Verify job details via GET /api/v1/dispatch/{id}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/dispatch/"+createdJob.ID, nil)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 on get job, got %d", rr.Code)
	}
	var finalJob DispatchJob
	if err := json.NewDecoder(rr.Body).Decode(&finalJob); err != nil {
		t.Fatalf("failed to decode final job: %v", err)
	}
	if finalJob.State != "completed" || finalJob.TelemetryRunID != fmt.Sprintf("auto_run_%s", createdJob.ID) {
		t.Fatalf("unexpected final job state: %+v", finalJob)
	}

	// 7. Verify telemetry payload was automatically ingested into Hub store
	run, found := server.store.GetRun(fmt.Sprintf("auto_run_%s", createdJob.ID))
	if !found || run.Summary.Passed != 1 {
		t.Errorf("expected auto-ingested telemetry run, found=%v", found)
	}
}

