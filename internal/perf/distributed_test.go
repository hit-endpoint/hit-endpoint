package perf

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"hit/internal/spec"
	"hit/internal/types"
)

func TestMergePerfReports(t *testing.T) {
	r1 := &types.PerfReport{
		Name:         "User Checkout",
		Method:       "POST",
		Url:          "https://api.example.com/checkout",
		Concurrency:  10,
		Completed:    100,
		OK:           95,
		Failed:       5,
		DurationS:    5.0,
		LatenciesMs:  []float64{10.0, 20.0, 30.0, 40.0, 50.0},
		StatusCounts: map[int]int{200: 95, 500: 5},
		Errors:       map[string]int{"timeout": 5},
	}

	r2 := &types.PerfReport{
		Name:         "User Checkout",
		Method:       "POST",
		Url:          "https://api.example.com/checkout",
		Concurrency:  15,
		Completed:    150,
		OK:           150,
		Failed:       0,
		DurationS:    6.0,
		LatenciesMs:  []float64{15.0, 25.0, 35.0, 45.0, 55.0},
		StatusCounts: map[int]int{200: 150},
		Errors:       map[string]int{},
	}

	merged := MergePerfReports([]*types.PerfReport{r1, r2})

	if merged.Concurrency != 25 {
		t.Errorf("expected merged concurrency 25, got %d", merged.Concurrency)
	}
	if merged.Completed != 250 {
		t.Errorf("expected merged completed 250, got %d", merged.Completed)
	}
	if merged.OK != 245 {
		t.Errorf("expected merged OK 245, got %d", merged.OK)
	}
	if merged.Failed != 5 {
		t.Errorf("expected merged Failed 5, got %d", merged.Failed)
	}
	if len(merged.LatenciesMs) != 10 {
		t.Errorf("expected 10 latencies, got %d", len(merged.LatenciesMs))
	}
	if merged.StatusCounts[200] != 245 {
		t.Errorf("expected 245 status 200, got %d", merged.StatusCounts[200])
	}
	if merged.StatusCounts[500] != 5 {
		t.Errorf("expected 5 status 500, got %d", merged.StatusCounts[500])
	}
	if merged.Errors["timeout"] != 5 {
		t.Errorf("expected 5 timeout errors, got %d", merged.Errors["timeout"])
	}

	// Verify percentiles
	p50 := merged.Percentile(0.50)
	if p50 <= 0 {
		t.Errorf("expected valid p50, got %.2f", p50)
	}
}

// mockWorkerTransport intercepts HTTP requests to simulated worker URLs in-memory.
type mockWorkerTransport struct {
	mu      sync.Mutex
	workers map[string]*mockWorkerState
}

type mockWorkerState struct {
	id          string
	region      string
	status      string
	concurrency int
	total       int
	completed   int
}

func newMockWorkerTransport() *mockWorkerTransport {
	return &mockWorkerTransport{
		workers: make(map[string]*mockWorkerState),
	}
}

func (m *mockWorkerTransport) registerWorker(url, id, region string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.workers[strings.TrimRight(url, "/")] = &mockWorkerState{
		id:     id,
		region: region,
		status: "idle",
	}
}

func (m *mockWorkerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	baseURL := fmt.Sprintf("%s://%s", req.URL.Scheme, req.URL.Host)
	w, exists := m.workers[baseURL]
	if !exists {
		return &http.Response{
			StatusCode: http.StatusBadGateway,
			Body:       io.NopCloser(strings.NewReader("worker not found")),
		}, nil
	}

	path := req.URL.Path
	switch path {
	case "/status", "/health":
		st := WorkerStatus{
			ID:      w.id,
			Region:  w.region,
			Status:  w.status,
			Version: "0.1.0",
		}
		data, _ := json.Marshal(st)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewReader(data)),
		}, nil

	case "/run":
		var job WorkerJob
		_ = json.NewDecoder(req.Body).Decode(&job)
		w.status = "running"
		w.concurrency = job.Concurrency
		w.total = job.Total
		w.completed = job.Total // simulate instantaneous completion for test

		respBody := map[string]string{"status": "started", "job_id": job.ID}
		data, _ := json.Marshal(respBody)
		return &http.Response{
			StatusCode: http.StatusAccepted,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewReader(data)),
		}, nil

	case "/metrics":
		w.status = "completed"
		metrics := WorkerMetrics{
			WorkerID:   w.id,
			Region:     w.region,
			Status:     "completed",
			Completed:  w.total,
			CurrentRPS: 50.0,
			OK:         w.total,
			Failed:     0,
			DurationS:  1.2,
		}
		data, _ := json.Marshal(metrics)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewReader(data)),
		}, nil

	case "/cancel":
		w.status = "idle"
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"status":"canceled"}`)),
		}, nil

	case "/report":
		rep := &types.PerfReport{
			Name:         "Mock Test",
			Method:       "GET",
			Url:          "https://example.com/api",
			Concurrency:  w.concurrency,
			Completed:    w.total,
			OK:           w.total,
			Failed:       0,
			DurationS:    1.2,
			LatenciesMs:  []float64{12.5, 14.2, 18.0, 22.1},
			StatusCounts: map[int]int{200: w.total},
			Errors:       map[string]int{},
		}
		data, _ := json.Marshal(rep)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewReader(data)),
		}, nil
	}

	return &http.Response{
		StatusCode: http.StatusNotFound,
		Body:       io.NopCloser(strings.NewReader("not found")),
	}, nil
}

func TestDistributeJob_CoordinatorFanOutAndMerge(t *testing.T) {
	mockTrans := newMockWorkerTransport()
	mockTrans.registerWorker("http://worker-1.local:8989", "worker-1", "us-east")
	mockTrans.registerWorker("http://worker-2.local:8989", "worker-2", "eu-central")
	mockTrans.registerWorker("http://worker-3.local:8989", "worker-3", "ap-southeast")

	client := &http.Client{Transport: mockTrans}

	workers := []string{
		"http://worker-1.local:8989",
		"http://worker-2.local:8989",
		"http://worker-3.local:8989",
	}

	sp := &spec.RequestSpec{
		Name:   "Mock Test",
		Method: "GET",
		Url:    "https://example.com/api",
	}

	opts := PerfOptions{
		Concurrency: 15,
		Total:       90,
	}

	coordOpts := &CoordinatorOptions{
		Client:   client,
		PollRate: 10 * time.Millisecond,
		OnProgress: func(completed int, rps float64, wm []WorkerMetrics) {
			// verify callback invocations
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	distRep, err := DistributeJob(ctx, workers, sp, opts, coordOpts)
	if err != nil {
		t.Fatalf("DistributeJob failed: %v", err)
	}

	if distRep.TotalWorkers != 3 {
		t.Errorf("expected 3 total workers, got %d", distRep.TotalWorkers)
	}
	if len(distRep.Workers) != 3 {
		t.Errorf("expected 3 worker summaries, got %d", len(distRep.Workers))
	}

	// Verify workload partition: 15 / 3 = 5 concurrency each, 90 / 3 = 30 total each
	if mockTrans.workers["http://worker-1.local:8989"].concurrency != 5 {
		t.Errorf("expected worker-1 concurrency 5, got %d", mockTrans.workers["http://worker-1.local:8989"].concurrency)
	}
	if mockTrans.workers["http://worker-1.local:8989"].total != 30 {
		t.Errorf("expected worker-1 total 30, got %d", mockTrans.workers["http://worker-1.local:8989"].total)
	}

	// Verify global merged report
	if distRep.Global.Completed != 90 {
		t.Errorf("expected global completed 90, got %d", distRep.Global.Completed)
	}
	if distRep.Global.OK != 90 {
		t.Errorf("expected global OK 90, got %d", distRep.Global.OK)
	}
	if distRep.Global.Concurrency != 15 {
		t.Errorf("expected global concurrency 15, got %d", distRep.Global.Concurrency)
	}
	if distRep.Global.StatusCounts[200] != 90 {
		t.Errorf("expected 90 status 200, got %d", distRep.Global.StatusCounts[200])
	}
}

func TestWorkerServer_HandlerEndpoints(t *testing.T) {
	ws := NewWorkerServer("test-worker-1", "us-west", 0)
	handler := ws.Handler()

	// 1. Check /status
	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 from /status, got %d", rec.Code)
	}
	var st WorkerStatus
	_ = json.NewDecoder(rec.Body).Decode(&st)
	if st.ID != "test-worker-1" || st.Status != "idle" {
		t.Errorf("unexpected status body: %+v", st)
	}

	// 2. Check /metrics
	req = httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 from /metrics, got %d", rec.Code)
	}

	// 3. Check /cancel when idle
	req = httptest.NewRequest(http.MethodPost, "/cancel", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 from /cancel, got %d", rec.Code)
	}
}

func TestInProcessTransport(t *testing.T) {
	trans := NewInProcessTransport()
	ws := NewWorkerServer("local-1", "core-1", 0)
	trans.Register("http://local-1", ws)

	client := &http.Client{Transport: trans}
	resp, err := client.Get("http://local-1/status")
	if err != nil {
		t.Fatalf("in-process request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected HTTP 200 from in-process worker, got %d", resp.StatusCode)
	}

	var st WorkerStatus
	_ = json.NewDecoder(resp.Body).Decode(&st)
	if st.ID != "local-1" || st.Region != "core-1" {
		t.Errorf("unexpected in-process worker status: %+v", st)
	}
}

// mockHubRoundTripper routes hub API calls in-memory for testing without socket dials.
type mockHubRoundTripper struct {
	mu           sync.Mutex
	registered   map[string]map[string]any
	heartbeats   map[string]int
	deregistered map[string]bool
	workers      []map[string]any
	queuedJobs   []map[string]any
	events       []map[string]any
	completed    []map[string]any
}

func newMockHubRoundTripper() *mockHubRoundTripper {
	return &mockHubRoundTripper{
		registered:   make(map[string]map[string]any),
		heartbeats:   make(map[string]int),
		deregistered: make(map[string]bool),
		queuedJobs:   make([]map[string]any, 0),
		events:       make([]map[string]any, 0),
		completed:    make([]map[string]any, 0),
	}
}

func (m *mockHubRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	path := req.URL.Path
	res := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Request:    req,
	}
	res.Header.Set("Content-Type", "application/json")

	if strings.HasSuffix(path, "/register") {
		var reg map[string]any
		_ = json.NewDecoder(req.Body).Decode(&reg)
		id := reg["id"].(string)
		m.registered[id] = reg
		res.StatusCode = http.StatusCreated
		res.Body = io.NopCloser(bytes.NewBufferString(`{"status":"registered"}`))
		return res, nil
	}

	if strings.HasSuffix(path, "/heartbeat") {
		parts := strings.Split(path, "/")
		nodeID := parts[len(parts)-2]
		m.heartbeats[nodeID]++
		res.Body = io.NopCloser(bytes.NewBufferString(`{"status":"heartbeat_ok"}`))
		return res, nil
	}

	if strings.HasSuffix(path, "/deregister") {
		parts := strings.Split(path, "/")
		nodeID := parts[len(parts)-2]
		m.deregistered[nodeID] = true
		res.Body = io.NopCloser(bytes.NewBufferString(`{"status":"deregistered"}`))
		return res, nil
	}

	if strings.HasSuffix(path, "/workers") {
		respBody, _ := json.Marshal(map[string]any{
			"workers": m.workers,
			"total":   len(m.workers),
		})
		res.Body = io.NopCloser(bytes.NewReader(respBody))
		return res, nil
	}

	if strings.HasSuffix(path, "/queue") {
		var job map[string]any
		if len(m.queuedJobs) > 0 {
			job = m.queuedJobs[0]
			m.queuedJobs = m.queuedJobs[1:]
		}
		respBody, _ := json.Marshal(map[string]any{"job": job})
		res.Body = io.NopCloser(bytes.NewReader(respBody))
		return res, nil
	}

	if strings.HasSuffix(path, "/events") {
		var ev map[string]any
		_ = json.NewDecoder(req.Body).Decode(&ev)
		m.events = append(m.events, ev)
		res.Body = io.NopCloser(bytes.NewBufferString(`{"status":"logged"}`))
		return res, nil
	}

	if strings.HasSuffix(path, "/complete") {
		var comp map[string]any
		_ = json.NewDecoder(req.Body).Decode(&comp)
		m.completed = append(m.completed, comp)
		res.Body = io.NopCloser(bytes.NewBufferString(`{"status":"completed"}`))
		return res, nil
	}

	res.StatusCode = http.StatusNotFound
	res.Body = io.NopCloser(bytes.NewBufferString(`{"error":"not found"}`))
	return res, nil
}

func TestWorkerServer_HubEnrollment(t *testing.T) {
	hubRT := newMockHubRoundTripper()
	mockClient := &http.Client{Transport: hubRT}

	ws := NewWorkerServerWithConfig(WorkerServerConfig{
		ID:           "worker-auto-1",
		Region:       "us-central",
		Port:         9090,
		HubURL:       "http://hub.internal",
		HubToken:     "secret-token",
		AdvertiseURL: "http://10.0.1.50:9090",
		Capacity:     8,
		Owner:        "qa-runner",
		HTTPClient:   mockClient,
	})

	// Test registration via runHubEnrollmentAndHeartbeat trigger
	ws.stopHB = make(chan struct{})
	go ws.runHubEnrollmentAndHeartbeat(nil)

	// Wait briefly for registration call
	time.Sleep(50 * time.Millisecond)

	hubRT.mu.Lock()
	reg, registered := hubRT.registered["worker-auto-1"]
	hubRT.mu.Unlock()

	if !registered {
		t.Fatalf("expected worker-auto-1 to be registered with hub")
	}
	if reg["region"] != "us-central" || reg["endpoint_url"] != "http://10.0.1.50:9090" {
		t.Errorf("unexpected registration payload: %+v", reg)
	}

	// Test deregistration on Stop
	_ = ws.Stop(context.Background())

	hubRT.mu.Lock()
	deregistered := hubRT.deregistered["worker-auto-1"]
	hubRT.mu.Unlock()

	if !deregistered {
		t.Errorf("expected worker-auto-1 to be deregistered after Stop")
	}
}

func TestDiscoverWorkers_And_DistributeJob_HubAutoDiscovery(t *testing.T) {
	hubRT := newMockHubRoundTripper()
	hubRT.workers = []map[string]any{
		{
			"id":           "worker-1",
			"endpoint_url": "http://worker-1.local:8989",
			"region":       "us-east",
			"state":        "idle",
		},
		{
			"id":           "worker-2",
			"endpoint_url": "http://worker-2.local:8989",
			"region":       "us-east",
			"state":        "idle",
		},
	}

	mockWorkerRT := newMockWorkerTransport()
	mockWorkerRT.workers["http://worker-1.local:8989"] = &mockWorkerState{id: "worker-1", region: "us-east", status: "idle"}
	mockWorkerRT.workers["http://worker-2.local:8989"] = &mockWorkerState{id: "worker-2", region: "us-east", status: "idle"}

	// Composite transport
	comboClient := &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.Host, "hub.internal") {
				return hubRT.RoundTrip(req)
			}
			return mockWorkerRT.RoundTrip(req)
		}),
	}

	// 1. Test DiscoverWorkers directly
	workers, err := DiscoverWorkers(context.Background(), comboClient, "http://hub.internal", "secret", "us-east")
	if err != nil {
		t.Fatalf("DiscoverWorkers failed: %v", err)
	}
	if len(workers) != 2 {
		t.Fatalf("expected 2 discovered workers, got %d", len(workers))
	}

	// 2. Test DistributeJob with HubURL and empty workers list
	sp := &spec.RequestSpec{
		Method: "GET",
		Url:    "https://api.example.com/items",
	}
	opts := PerfOptions{
		Concurrency: 10,
		Total:       40,
	}
	coordOpts := &CoordinatorOptions{
		Client:   comboClient,
		HubURL:   "http://hub.internal",
		HubToken: "secret",
		Region:   "us-east",
		PollRate: 10 * time.Millisecond,
	}

	report, err := DistributeJob(context.Background(), nil, sp, opts, coordOpts)
	if err != nil {
		t.Fatalf("DistributeJob auto-discovery run failed: %v", err)
	}

	if report.TotalWorkers != 2 {
		t.Errorf("expected 2 total workers from auto-discovery, got %d", report.TotalWorkers)
	}
	if report.Global.Completed != 40 {
		t.Errorf("expected 40 completed requests, got %d", report.Global.Completed)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestWorkerServer_DispatchQueueExecution(t *testing.T) {
	hubRT := newMockHubRoundTripper()
	mockClient := &http.Client{Transport: hubRT}

	ws := NewWorkerServerWithConfig(WorkerServerConfig{
		ID:         "worker-test-exec",
		Region:     "us-west",
		Port:       9095,
		HubURL:     "http://hub.internal",
		HubToken:   "secret",
		Capacity:   4,
		Owner:      "tester",
		HTTPClient: mockClient,
	})

	// Execute a dispatched test job
	ws.executeDispatchedJob(
		"test-dispatch-1",
		"test",
		"Ad-hoc Sanity Run",
		"name: mock-ping\nmethod: GET\nurl: http://127.0.0.1:9/unreachable\ntests:\n  - status == 200",
		1, 1, 0,
	)

	hubRT.mu.Lock()
	eventsCount := len(hubRT.events)
	completedCount := len(hubRT.completed)
	var finalComp map[string]any
	if completedCount > 0 {
		finalComp = hubRT.completed[0]
	}
	hubRT.mu.Unlock()

	if eventsCount == 0 {
		t.Errorf("expected worker to emit execution events to hub")
	}
	if completedCount != 1 {
		t.Fatalf("expected 1 completion report, got %d", completedCount)
	}
	if finalComp["state"] != "failed" { // unreachable address causes test failure
		t.Errorf("expected failed state for unreachable endpoint, got %v", finalComp["state"])
	}
	if finalComp["telemetry"] == nil {
		t.Errorf("expected telemetry payload in completion report")
	}
}



