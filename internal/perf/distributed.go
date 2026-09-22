package perf

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/hit-endpoint/hit-endpoint/internal/runner"
	"github.com/hit-endpoint/hit-endpoint/internal/spec"
	"github.com/hit-endpoint/hit-endpoint/internal/telemetry"
	"github.com/hit-endpoint/hit-endpoint/internal/types"

	"gopkg.in/yaml.v3"
)

// WorkerJob defines the load generation job assigned to an individual worker.
type WorkerJob struct {
	ID          string            `json:"id"`
	Spec        *spec.RequestSpec `json:"spec"`
	Concurrency int               `json:"concurrency"`
	Total       int               `json:"total"`
	Duration    float64           `json:"duration"` // seconds
	RPS         float64           `json:"rps"`
	Warmup      int               `json:"warmup"`
	RampUp      float64           `json:"ramp_up"`
	Check       bool              `json:"check"`
	Region      string            `json:"region,omitempty"`
}

// WorkerStatus provides node health and execution status.
type WorkerStatus struct {
	ID      string `json:"id"`
	Region  string `json:"region"`
	Status  string `json:"status"` // "idle", "running", "completed", "error"
	Version string `json:"version"`
	Error   string `json:"error,omitempty"`
}

// WorkerMetrics represents real-time performance telemetry emitted by a worker.
type WorkerMetrics struct {
	WorkerID   string  `json:"worker_id"`
	Region     string  `json:"region"`
	Status     string  `json:"status"`
	Completed  int     `json:"completed"`
	Active     int     `json:"active"`
	CurrentRPS float64 `json:"current_rps"`
	OK         int     `json:"ok"`
	Failed     int     `json:"failed"`
	DurationS  float64 `json:"duration_s"`
}

// WorkerReportSummary encapsulates the result from an individual worker.
type WorkerReportSummary struct {
	WorkerID string            `json:"worker_id"`
	Region   string            `json:"region"`
	Endpoint string            `json:"endpoint"`
	Report   *types.PerfReport `json:"report"`
}

// DistributedPerfReport consolidates distributed load test results across all workers.
type DistributedPerfReport struct {
	Global            *types.PerfReport     `json:"global"`
	Workers           []WorkerReportSummary `json:"workers"`
	TotalWorkers      int                   `json:"total_workers"`
	ThresholdsPassed  bool                  `json:"thresholds_passed"`
	ThresholdResults  []ThresholdResult     `json:"threshold_results,omitempty"`
	CoordinatorTimeS  float64               `json:"coordinator_time_s"`
}

// WorkerServerConfig contains enrollment and network configuration for WorkerServer.
type WorkerServerConfig struct {
	ID           string
	Region       string
	Port         int
	HubURL       string
	HubToken     string
	AdvertiseURL string
	Capacity     int
	Owner        string
	HTTPClient   *http.Client
}

// WorkerServer implements the HTTP API for a hit load worker daemon.
type WorkerServer struct {
	ID           string
	Region       string
	Port         int
	HubURL       string
	HubToken     string
	AdvertiseURL string
	Capacity     int
	Owner        string
	server       *http.Server
	listener     net.Listener
	mu           sync.RWMutex
	status       string
	activeJob    *WorkerJob
	metrics      WorkerMetrics
	report       *types.PerfReport
	err          error
	cancelJob    context.CancelFunc
	stopHB       chan struct{}
	client       *http.Client
}

// NewWorkerServer creates an initialized WorkerServer instance.
func NewWorkerServer(id, region string, port int) *WorkerServer {
	return NewWorkerServerWithConfig(WorkerServerConfig{
		ID:     id,
		Region: region,
		Port:   port,
	})
}

// NewWorkerServerWithConfig creates a WorkerServer from full configuration including Hub enrollment.
func NewWorkerServerWithConfig(cfg WorkerServerConfig) *WorkerServer {
	if cfg.ID == "" {
		cfg.ID = fmt.Sprintf("worker-%d", time.Now().UnixNano()%10000)
	}
	if cfg.Region == "" {
		cfg.Region = "local"
	}
	if cfg.Capacity <= 0 {
		cfg.Capacity = runtime.NumCPU() * 2
		if cfg.Capacity < 4 {
			cfg.Capacity = 4
		}
	}
	if cfg.Owner == "" {
		cfg.Owner = os.Getenv("USER")
		if cfg.Owner == "" {
			cfg.Owner = "perf-worker"
		}
	}
	client := cfg.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	ws := &WorkerServer{
		ID:           cfg.ID,
		Region:       cfg.Region,
		Port:         cfg.Port,
		HubURL:       cfg.HubURL,
		HubToken:     cfg.HubToken,
		AdvertiseURL: cfg.AdvertiseURL,
		Capacity:     cfg.Capacity,
		Owner:        cfg.Owner,
		status:       "idle",
		client:       client,
	}
	ws.metrics = WorkerMetrics{
		WorkerID: ws.ID,
		Region:   ws.Region,
		Status:   ws.status,
	}
	return ws
}

// Handler returns the http.Handler serving the worker REST endpoints.
func (ws *WorkerServer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", ws.handleHealth)
	mux.HandleFunc("/status", ws.handleHealth)
	mux.HandleFunc("/run", ws.handleRun)
	mux.HandleFunc("/metrics", ws.handleMetrics)
	mux.HandleFunc("/cancel", ws.handleCancel)
	mux.HandleFunc("/report", ws.handleReport)
	return mux
}

// Start runs the worker server on the configured port or custom listener.
func (ws *WorkerServer) Start(customListener net.Listener) error {
	ws.mu.Lock()
	ws.listener = customListener
	ws.server = &http.Server{
		Handler: ws.Handler(),
	}
	ws.mu.Unlock()

	var l net.Listener
	if ws.listener != nil {
		l = ws.listener
	} else {
		addr := fmt.Sprintf(":%d", ws.Port)
		var err error
		l, err = net.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("failed to listen on %s: %w", addr, err)
		}
		ws.mu.Lock()
		ws.listener = l
		ws.mu.Unlock()
	}

	if ws.HubURL != "" {
		ws.mu.Lock()
		if ws.stopHB == nil {
			ws.stopHB = make(chan struct{})
			go ws.runHubEnrollmentAndHeartbeat(l)
		}
		ws.mu.Unlock()
	}

	return ws.server.Serve(l)
}

// Stop gracefully shuts down the worker HTTP server and deregisters from the hub.
func (ws *WorkerServer) Stop(ctx context.Context) error {
	ws.mu.Lock()
	if ws.cancelJob != nil {
		ws.cancelJob()
	}
	stopCh := ws.stopHB
	ws.stopHB = nil
	srv := ws.server
	ws.mu.Unlock()

	if stopCh != nil {
		close(stopCh)
	}

	if ws.HubURL != "" {
		ws.deregisterFromHub()
	}

	if srv != nil {
		return srv.Shutdown(ctx)
	}
	return nil
}

func (ws *WorkerServer) runHubEnrollmentAndHeartbeat(l net.Listener) {
	advURL := ws.AdvertiseURL
	if advURL == "" {
		if l != nil {
			advURL = "http://" + l.Addr().String()
		} else {
			advURL = fmt.Sprintf("http://127.0.0.1:%d", ws.Port)
		}
	}

	host, _ := os.Hostname()
	regReq := map[string]any{
		"id":           ws.ID,
		"common_name":  ws.ID,
		"owner":        ws.Owner,
		"type":         "perf-worker",
		"region":       ws.Region,
		"endpoint_url": advURL,
		"capacity":     ws.Capacity,
		"capabilities": []string{"perf-worker", "http-load", "test-runner"},
		"hostname":     host,
		"os":           runtime.GOOS,
	}

	regBytes, _ := json.Marshal(regReq)
	regURL := strings.TrimRight(ws.HubURL, "/") + "/api/v1/nodes/register"

	// Register with Hub
	req, err := http.NewRequest(http.MethodPost, regURL, bytes.NewReader(regBytes))
	if err == nil {
		req.Header.Set("Content-Type", "application/json")
		if ws.HubToken != "" {
			req.Header.Set("Authorization", "Bearer "+ws.HubToken)
		}
		resp, err := ws.client.Do(req)
		if err == nil {
			resp.Body.Close()
		}
	}

	hbTicker := time.NewTicker(15 * time.Second)
	defer hbTicker.Stop()

	pollTicker := time.NewTicker(2 * time.Second)
	defer pollTicker.Stop()

	hbURL := fmt.Sprintf("%s/api/v1/nodes/%s/heartbeat", strings.TrimRight(ws.HubURL, "/"), ws.ID)
	queueURL := fmt.Sprintf("%s/api/v1/nodes/%s/queue?region=%s", strings.TrimRight(ws.HubURL, "/"), ws.ID, url.QueryEscape(ws.Region))

	for {
		select {
		case <-hbTicker.C:
			ws.mu.RLock()
			st := ws.status
			m := ws.metrics
			ws.mu.RUnlock()

			stateStr := "idle"
			activeJobs := 0
			if st == "running" {
				stateStr = "busy"
				activeJobs = 1
			}

			hbData := map[string]any{
				"state":       stateStr,
				"active_jobs": activeJobs,
				"metrics": map[string]any{
					"current_rps": m.CurrentRPS,
					"completed":   m.Completed,
				},
			}
			hbBytes, _ := json.Marshal(hbData)

			hReq, err := http.NewRequest(http.MethodPost, hbURL, bytes.NewReader(hbBytes))
			if err == nil {
				hReq.Header.Set("Content-Type", "application/json")
				if ws.HubToken != "" {
					hReq.Header.Set("Authorization", "Bearer "+ws.HubToken)
				}
				resp, err := ws.client.Do(hReq)
				if err == nil {
					resp.Body.Close()
				}
			}

		case <-pollTicker.C:
			ws.mu.RLock()
			st := ws.status
			ws.mu.RUnlock()
			if st != "idle" {
				continue
			}

			qReq, err := http.NewRequest(http.MethodGet, queueURL, nil)
			if err != nil {
				continue
			}
			if ws.HubToken != "" {
				qReq.Header.Set("Authorization", "Bearer "+ws.HubToken)
			}
			resp, err := ws.client.Do(qReq)
			if err != nil {
				continue
			}
			var qResp struct {
				Job *struct {
					ID           string  `json:"id"`
					Type         string  `json:"type"`
					Name         string  `json:"name"`
					TargetNodeID string  `json:"target_node_id"`
					TargetRegion string  `json:"target_region"`
					SpecYAML     string  `json:"spec_yaml"`
					Concurrency  int     `json:"concurrency"`
					Requests     int     `json:"requests"`
					DurationS    float64 `json:"duration_s"`
				} `json:"job"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&qResp)
			resp.Body.Close()

			if qResp.Job != nil && qResp.Job.ID != "" {
				j := qResp.Job
				go ws.executeDispatchedJob(j.ID, j.Type, j.Name, j.SpecYAML, j.Concurrency, j.Requests, j.DurationS)
			}

		case <-ws.stopHB:
			return
		}
	}
}

func (ws *WorkerServer) executeDispatchedJob(jobID, jobType, jobName, specYAML string, concurrency, requests int, durationS float64) {
	ws.mu.Lock()
	if ws.status == "running" {
		ws.mu.Unlock()
		return
	}
	ws.status = "running"
	_, cancel := context.WithCancel(context.Background())
	ws.cancelJob = cancel
	ws.mu.Unlock()

	defer func() {
		ws.mu.Lock()
		ws.status = "idle"
		ws.cancelJob = nil
		ws.mu.Unlock()
	}()

	emitLog := func(level, msg string) {
		evURL := fmt.Sprintf("%s/api/v1/nodes/%s/jobs/%s/events", strings.TrimRight(ws.HubURL, "/"), ws.ID, jobID)
		evData, _ := json.Marshal(map[string]any{
			"timestamp": time.Now().UTC(),
			"level":     level,
			"message":   msg,
		})
		req, err := http.NewRequest(http.MethodPost, evURL, bytes.NewReader(evData))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
			if ws.HubToken != "" {
				req.Header.Set("Authorization", "Bearer "+ws.HubToken)
			}
			if r, err := ws.client.Do(req); err == nil {
				r.Body.Close()
			}
		}
	}

	completeJob := func(state string, result any, errMsg string, telem *telemetry.RunTelemetryPayload) {
		compURL := fmt.Sprintf("%s/api/v1/nodes/%s/jobs/%s/complete", strings.TrimRight(ws.HubURL, "/"), ws.ID, jobID)
		compData, _ := json.Marshal(map[string]any{
			"state":     state,
			"result":    result,
			"error":     errMsg,
			"telemetry": telem,
		})
		req, err := http.NewRequest(http.MethodPost, compURL, bytes.NewReader(compData))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
			if ws.HubToken != "" {
				req.Header.Set("Authorization", "Bearer "+ws.HubToken)
			}
			if r, err := ws.client.Do(req); err == nil {
				r.Body.Close()
			}
		}
	}

	emitLog("info", fmt.Sprintf("Starting execution of %s (type: %s)", jobName, jobType))

	var specDict map[string]any
	if err := yaml.Unmarshal([]byte(specYAML), &specDict); err != nil {
		emitLog("error", fmt.Sprintf("YAML parse error: %v", err))
		completeJob("failed", nil, err.Error(), nil)
		return
	}

	sp, err := spec.SpecFromDict(specDict, nil, "")
	if err != nil {
		emitLog("error", fmt.Sprintf("Spec build error: %v", err))
		completeJob("failed", nil, err.Error(), nil)
		return
	}

	if jobType == "perf" {
		if concurrency <= 0 {
			concurrency = 10
		}
		if requests <= 0 && durationS <= 0 {
			requests = 100
		}

		sess, err := runner.NewSession(runner.SessionOptions{
			ZoneOptional: true,
			Persist:      false,
			NoHistory:    true,
		})
		if err != nil {
			emitLog("error", fmt.Sprintf("Session init error: %v", err))
			completeJob("failed", nil, err.Error(), nil)
			return
		}
		defer sess.Close()

		opts := PerfOptions{
			Concurrency: concurrency,
			Total:       requests,
			Duration:    durationS,
			Progress: func(completed int, elapsedSec float64) {
				rps := 0.0
				if elapsedSec > 0 {
					rps = float64(completed) / elapsedSec
				}
				emitLog("info", fmt.Sprintf("Progress: %d requests completed (%.1f req/s)", completed, rps))
			},
		}

		rep, runErr := RunPerf(sess, sp, opts)
		if runErr != nil && runErr != context.Canceled {
			emitLog("error", fmt.Sprintf("Perf run failed: %v", runErr))
			completeJob("failed", rep, runErr.Error(), nil)
			return
		}

		emitLog("pass", fmt.Sprintf("Completed %d requests in %.2fs (%.1f req/s, %d ok, %d failed)",
			rep.Completed, rep.DurationS, rep.RPS(), rep.OK, rep.Failed))
		completeJob("completed", rep, "", nil)
		return
	}

	sess, err := runner.NewSession(runner.SessionOptions{
		ZoneOptional: true,
		Persist:      false,
		NoHistory:    true,
	})
	if err != nil {
		emitLog("error", fmt.Sprintf("Session init error: %v", err))
		completeJob("failed", nil, err.Error(), nil)
		return
	}
	defer sess.Close()

	emitLog("info", fmt.Sprintf("Executing %s %s", sp.Method, sp.Url))
	res := sess.RunSpec(sp, nil)

	passedCount := 0
	failedCount := 0
	for _, t := range res.Tests {
		if t.Passed {
			passedCount++
			emitLog("pass", fmt.Sprintf("✓ %s", t.Name))
		} else {
			failedCount++
			emitLog("fail", fmt.Sprintf("✗ %s: %s", t.Name, t.Detail))
		}
	}

	if res.Error != "" {
		emitLog("error", fmt.Sprintf("Transport error: %s", res.Error))
	} else {
		emitLog("info", fmt.Sprintf("HTTP %d received (elapsed: %.1fms, size: %d bytes)", res.Status, res.ElapsedMs, res.Size))
	}

	finalStatus := "completed"
	if !res.OK() {
		finalStatus = "failed"
	}

	var testTelem []telemetry.TestTelemetry
	for _, t := range res.Tests {
		testTelem = append(testTelem, telemetry.TestTelemetry{
			Name:   t.Name,
			Passed: t.Passed,
			Detail: t.Detail,
		})
	}

	host, _ := os.Hostname()
	telemPayload := &telemetry.RunTelemetryPayload{
		RunID:     fmt.Sprintf("run_%s", jobID),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		ProjectID: "fleet-dispatch",
		Node: telemetry.NodeContext{
			ID:         ws.ID,
			CommonName: ws.ID,
			Owner:      ws.Owner,
			Type:       "perf-worker",
			Hostname:   host,
			OS:         runtime.GOOS,
		},
		Summary: telemetry.RunSummary{
			Total:      len(res.Tests),
			Passed:     passedCount,
			Failed:     failedCount,
			DurationMs: res.ElapsedMs,
		},
		Endpoints: []telemetry.EndpointTelemetry{
			{
				Method:    res.Method,
				URL:       res.Url,
				Status:    res.Status,
				ElapsedMs: res.ElapsedMs,
				OK:        res.OK(),
				Error:     res.Error,
				Tests:     testTelem,
			},
		},
		Percentiles: map[string]float64{
			"p50": res.ElapsedMs,
			"p90": res.ElapsedMs,
			"p95": res.ElapsedMs,
			"p99": res.ElapsedMs,
		},
	}

	completeJob(finalStatus, res, res.Error, telemPayload)
}

func (ws *WorkerServer) deregisterFromHub() {
	deregURL := fmt.Sprintf("%s/api/v1/nodes/%s/deregister", strings.TrimRight(ws.HubURL, "/"), ws.ID)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, deregURL, nil)
	if err == nil {
		if ws.HubToken != "" {
			req.Header.Set("Authorization", "Bearer "+ws.HubToken)
		}
		resp, err := ws.client.Do(req)
		if err == nil {
			resp.Body.Close()
		}
	}
}

func (ws *WorkerServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	ws.mu.RLock()
	st := WorkerStatus{
		ID:      ws.ID,
		Region:  ws.Region,
		Status:  ws.status,
		Version: "0.1.0",
	}
	if ws.err != nil {
		st.Error = ws.err.Error()
	}
	ws.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(st)
}

func (ws *WorkerServer) handleRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var job WorkerJob
	if err := json.NewDecoder(r.Body).Decode(&job); err != nil {
		http.Error(w, fmt.Sprintf("invalid job spec: %v", err), http.StatusBadRequest)
		return
	}

	ws.mu.Lock()
	if ws.status == "running" {
		ws.mu.Unlock()
		http.Error(w, "worker is busy running another job", http.StatusConflict)
		return
	}

	ws.status = "running"
	ws.activeJob = &job
	ws.report = nil
	ws.err = nil
	ws.metrics = WorkerMetrics{
		WorkerID: ws.ID,
		Region:   ws.Region,
		Status:   "running",
		Active:   job.Concurrency,
	}

	jobCtx, cancel := context.WithCancel(context.Background())
	ws.cancelJob = cancel
	ws.mu.Unlock()

	// Launch load generation in background
	go ws.executeJob(jobCtx, &job)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "started",
		"job_id": job.ID,
		"worker": ws.ID,
	})
}

func (ws *WorkerServer) executeJob(ctx context.Context, job *WorkerJob) {
	sess, err := runner.NewSession(runner.SessionOptions{
		ZoneOptional: true,
		Persist:      false,
		NoHistory:    true,
	})
	if err != nil {
		ws.mu.Lock()
		ws.status = "error"
		ws.err = err
		ws.mu.Unlock()
		return
	}
	defer sess.Close()

	opts := PerfOptions{
		Concurrency: job.Concurrency,
		Total:       job.Total,
		Duration:    job.Duration,
		RPS:         job.RPS,
		Warmup:      job.Warmup,
		RampUp:      job.RampUp,
		Check:       job.Check,
		Progress: func(completed int, elapsedSec float64) {
			ws.mu.Lock()
			ws.metrics.Completed = completed
			ws.metrics.DurationS = elapsedSec
			if elapsedSec > 0 {
				ws.metrics.CurrentRPS = float64(completed) / elapsedSec
			}
			ws.mu.Unlock()
		},
	}

	rep, runErr := RunPerf(sess, job.Spec, opts)

	ws.mu.Lock()
	defer ws.mu.Unlock()
	if runErr != nil && runErr != context.Canceled {
		ws.status = "error"
		ws.err = runErr
	} else {
		ws.status = "completed"
		ws.report = rep
		if rep != nil {
			ws.metrics.Completed = rep.Completed
			ws.metrics.OK = rep.OK
			ws.metrics.Failed = rep.Failed
			ws.metrics.DurationS = rep.DurationS
			ws.metrics.CurrentRPS = rep.RPS()
		}
	}
	ws.metrics.Status = ws.status
	ws.metrics.Active = 0
}

func (ws *WorkerServer) handleMetrics(w http.ResponseWriter, r *http.Request) {
	ws.mu.RLock()
	m := ws.metrics
	ws.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(m)
}

func (ws *WorkerServer) handleCancel(w http.ResponseWriter, r *http.Request) {
	ws.mu.Lock()
	if ws.cancelJob != nil {
		ws.cancelJob()
	}
	ws.status = "idle"
	ws.metrics.Status = "idle"
	ws.metrics.Active = 0
	ws.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "canceled"})
}

func (ws *WorkerServer) handleReport(w http.ResponseWriter, r *http.Request) {
	ws.mu.RLock()
	st := ws.status
	rep := ws.report
	err := ws.err
	ws.mu.RUnlock()

	if st == "running" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if st == "error" && err != nil {
		http.Error(w, fmt.Sprintf("job failed: %v", err), http.StatusInternalServerError)
		return
	}
	if rep == nil {
		http.Error(w, "no report available", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rep)
}

// CoordinatorOptions configures the coordinator for a distributed load run.
type CoordinatorOptions struct {
	Client     *http.Client
	OnProgress func(completed int, aggregateRPS float64, workerMetrics []WorkerMetrics)
	PollRate   time.Duration
	HubURL     string
	HubToken   string
	Region     string
}

// DiscoverWorkers queries the Fleet Hub for active performance workers.
func DiscoverWorkers(ctx context.Context, client *http.Client, hubURL, hubToken, region string) ([]string, error) {
	if hubURL == "" {
		return nil, fmt.Errorf("hub URL is required for worker discovery")
	}
	if client == nil {
		client = http.DefaultClient
	}

	queryURL := strings.TrimRight(hubURL, "/") + "/api/v1/fleet/workers?capability=perf-worker"
	if region != "" {
		queryURL += "&region=" + url.QueryEscape(region)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, queryURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build worker discovery request: %w", err)
	}
	if hubToken != "" {
		req.Header.Set("Authorization", "Bearer "+hubToken)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to hub for worker discovery: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("hub returned HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var data struct {
		Workers []struct {
			ID          string `json:"id"`
			EndpointURL string `json:"endpoint_url"`
			Region      string `json:"region"`
			State       string `json:"state"`
		} `json:"workers"`
		Total int `json:"total"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("failed to decode worker discovery response: %w", err)
	}

	var endpoints []string
	for _, w := range data.Workers {
		if w.EndpointURL != "" {
			endpoints = append(endpoints, w.EndpointURL)
		}
	}

	if len(endpoints) == 0 {
		return nil, fmt.Errorf("no active performance workers found registered in hub (%s)", hubURL)
	}

	return endpoints, nil
}

// MergePerfReports merges reports from multiple distributed worker nodes into a single consolidated report.
func MergePerfReports(reports []*types.PerfReport) *types.PerfReport {
	if len(reports) == 0 {
		return &types.PerfReport{
			StatusCounts: make(map[int]int),
			Errors:       make(map[string]int),
			TestFailures: make(map[string]int),
		}
	}

	merged := &types.PerfReport{
		Name:         reports[0].Name,
		Method:       reports[0].Method,
		Url:          reports[0].Url,
		StatusCounts: make(map[int]int),
		Errors:       make(map[string]int),
		TestFailures: make(map[string]int),
	}

	maxDuration := 0.0
	for _, r := range reports {
		if r == nil {
			continue
		}
		merged.Concurrency += r.Concurrency
		merged.Completed += r.Completed
		merged.OK += r.OK
		merged.Failed += r.Failed
		merged.BytesReceived += r.BytesReceived
		if r.DurationS > maxDuration {
			maxDuration = r.DurationS
		}

		merged.LatenciesMs = append(merged.LatenciesMs, r.LatenciesMs...)

		for code, cnt := range r.StatusCounts {
			merged.StatusCounts[code] += cnt
		}
		for errMsg, cnt := range r.Errors {
			merged.Errors[errMsg] += cnt
		}
		for tfName, cnt := range r.TestFailures {
			merged.TestFailures[tfName] += cnt
		}
	}

	merged.DurationS = maxDuration
	return merged
}

// DistributeJob coordinates a distributed performance test across multiple worker nodes.
func DistributeJob(ctx context.Context, workers []string, sp *spec.RequestSpec, opts PerfOptions, coordOpts *CoordinatorOptions) (*DistributedPerfReport, error) {
	client := http.DefaultClient
	pollRate := 500 * time.Millisecond
	var onProgress func(int, float64, []WorkerMetrics)
	if coordOpts != nil {
		if coordOpts.Client != nil {
			client = coordOpts.Client
		}
		if coordOpts.PollRate > 0 {
			pollRate = coordOpts.PollRate
		}
		onProgress = coordOpts.OnProgress
	}

	if len(workers) == 0 && coordOpts != nil && coordOpts.HubURL != "" {
		discovered, err := DiscoverWorkers(ctx, client, coordOpts.HubURL, coordOpts.HubToken, coordOpts.Region)
		if err != nil {
			return nil, fmt.Errorf("worker auto-discovery failed: %w", err)
		}
		workers = discovered
	}

	if len(workers) == 0 {
		return nil, fmt.Errorf("no workers specified for distributed performance run (pass --workers or --hub)")
	}

	cleanWorkers := make([]string, 0, len(workers))
	for _, w := range workers {
		trimmed := strings.TrimSpace(w)
		if trimmed != "" {
			if !strings.HasPrefix(trimmed, "http://") && !strings.HasPrefix(trimmed, "https://") {
				trimmed = "http://" + trimmed
			}
			cleanWorkers = append(cleanWorkers, strings.TrimRight(trimmed, "/"))
		}
	}
	if len(cleanWorkers) == 0 {
		return nil, fmt.Errorf("no valid worker URLs provided")
	}

	numWorkers := len(cleanWorkers)

	// 1. Pre-flight health check across all workers
	statuses := make([]WorkerStatus, numWorkers)
	for i, wURL := range cleanWorkers {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, wURL+"/status", nil)
		if err != nil {
			return nil, fmt.Errorf("failed to build status request for %s: %w", wURL, err)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("worker %s is unreachable: %w", wURL, err)
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("worker %s returned unhealthy status HTTP %d", wURL, resp.StatusCode)
		}
		_ = json.NewDecoder(resp.Body).Decode(&statuses[i])
		resp.Body.Close()

		if statuses[i].Status == "running" {
			return nil, fmt.Errorf("worker %s is already running another workload", wURL)
		}
	}

	// 2. Partition workload
	baseConc := opts.Concurrency / numWorkers
	remConc := opts.Concurrency % numWorkers
	if baseConc == 0 {
		baseConc = 1
		remConc = 0
	}

	baseTotal := 0
	remTotal := 0
	if opts.Total > 0 {
		baseTotal = opts.Total / numWorkers
		remTotal = opts.Total % numWorkers
	}

	workerRPS := 0.0
	if opts.RPS > 0 {
		workerRPS = opts.RPS / float64(numWorkers)
	}

	jobID := fmt.Sprintf("dist-job-%x", time.Now().UnixNano())
	startCoord := time.Now()

	// 3. Dispatch jobs to all workers
	var dispatchedWorkers []string
	cancelAll := func() {
		for _, wURL := range dispatchedWorkers {
			cReq, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, wURL+"/cancel", nil)
			if resp, err := client.Do(cReq); err == nil {
				resp.Body.Close()
			}
		}
	}

	for i, wURL := range cleanWorkers {
		wConc := baseConc
		if i < remConc {
			wConc++
		}

		wTotal := baseTotal
		if i < remTotal {
			wTotal++
		}

		job := WorkerJob{
			ID:          jobID,
			Spec:        sp,
			Concurrency: wConc,
			Total:       wTotal,
			Duration:    opts.Duration,
			RPS:         workerRPS,
			Warmup:      opts.Warmup,
			RampUp:      opts.RampUp,
			Check:       opts.Check,
			Region:      statuses[i].Region,
		}

		payload, err := json.Marshal(job)
		if err != nil {
			cancelAll()
			return nil, fmt.Errorf("failed to marshal worker job: %w", err)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, wURL+"/run", bytes.NewReader(payload))
		if err != nil {
			cancelAll()
			return nil, fmt.Errorf("failed to create run request for %s: %w", wURL, err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			cancelAll()
			return nil, fmt.Errorf("failed to dispatch to worker %s: %w", wURL, err)
		}
		if resp.StatusCode != http.StatusAccepted {
			snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
			resp.Body.Close()
			cancelAll()
			return nil, fmt.Errorf("worker %s rejected job (HTTP %d): %s", wURL, resp.StatusCode, string(snippet))
		}
		resp.Body.Close()
		dispatchedWorkers = append(dispatchedWorkers, wURL)
	}

	// 4. Polling loop: track progress and detect completion
	ticker := time.NewTicker(pollRate)
	defer ticker.Stop()

	completedWorkers := make(map[string]bool)

	for {
		select {
		case <-ctx.Done():
			cancelAll()
			return nil, ctx.Err()
		case <-ticker.C:
			allDone := true
			totalCompleted := 0
			var currentMetrics []WorkerMetrics

			for _, wURL := range cleanWorkers {
				if completedWorkers[wURL] {
					continue
				}

				mReq, err := http.NewRequestWithContext(ctx, http.MethodGet, wURL+"/metrics", nil)
				if err != nil {
					continue
				}
				resp, err := client.Do(mReq)
				if err != nil {
					continue
				}
				var wm WorkerMetrics
				_ = json.NewDecoder(resp.Body).Decode(&wm)
				resp.Body.Close()

				totalCompleted += wm.Completed
				currentMetrics = append(currentMetrics, wm)

				if wm.Status == "completed" || wm.Status == "error" {
					completedWorkers[wURL] = true
				} else {
					allDone = false
				}
			}

			if onProgress != nil {
				elapsed := time.Since(startCoord).Seconds()
				aggRPS := 0.0
				if elapsed > 0 {
					aggRPS = float64(totalCompleted) / elapsed
				}
				onProgress(totalCompleted, aggRPS, currentMetrics)
			}

			if len(completedWorkers) == numWorkers {
				allDone = true
			}

			if allDone {
				goto FetchReports
			}
		}
	}

FetchReports:
	// 5. Retrieve final reports from all workers
	summaries := make([]WorkerReportSummary, 0, numWorkers)
	reportsToMerge := make([]*types.PerfReport, 0, numWorkers)

	for i, wURL := range cleanWorkers {
		repReq, err := http.NewRequestWithContext(ctx, http.MethodGet, wURL+"/report", nil)
		if err != nil {
			return nil, fmt.Errorf("failed to build report request for %s: %w", wURL, err)
		}
		resp, err := client.Do(repReq)
		if err != nil {
			return nil, fmt.Errorf("failed to retrieve report from %s: %w", wURL, err)
		}
		if resp.StatusCode != http.StatusOK {
			snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
			resp.Body.Close()
			return nil, fmt.Errorf("worker %s returned HTTP %d on report request: %s", wURL, resp.StatusCode, string(snippet))
		}

		var rep types.PerfReport
		if err := json.NewDecoder(resp.Body).Decode(&rep); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("failed to decode report from %s: %w", wURL, err)
		}
		resp.Body.Close()

		summary := WorkerReportSummary{
			WorkerID: statuses[i].ID,
			Region:   statuses[i].Region,
			Endpoint: wURL,
			Report:   &rep,
		}
		summaries = append(summaries, summary)
		reportsToMerge = append(reportsToMerge, &rep)
	}

	// 6. Merge reports into unified global result
	globalReport := MergePerfReports(reportsToMerge)
	globalReport.DurationS = math.Round(time.Since(startCoord).Seconds()*100) / 100

	distReport := &DistributedPerfReport{
		Global:           globalReport,
		Workers:          summaries,
		TotalWorkers:     numWorkers,
		ThresholdsPassed: true,
		CoordinatorTimeS: globalReport.DurationS,
	}

	return distReport, nil
}

// InProcessTransport routes HTTP requests to in-process WorkerServer instances without network sockets.
type InProcessTransport struct {
	mu      sync.RWMutex
	workers map[string]*WorkerServer
}

// NewInProcessTransport creates a new in-process roundtripper.
func NewInProcessTransport() *InProcessTransport {
	return &InProcessTransport{
		workers: make(map[string]*WorkerServer),
	}
}

// Register maps a simulated worker base URL to an in-memory WorkerServer.
func (t *InProcessTransport) Register(url string, ws *WorkerServer) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.workers[strings.TrimRight(url, "/")] = ws
}

// RoundTrip dispatches the HTTP request directly to the WorkerServer's ServeHTTP method.
func (t *InProcessTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.mu.RLock()
	baseURL := fmt.Sprintf("%s://%s", req.URL.Scheme, req.URL.Host)
	ws, ok := t.workers[baseURL]
	t.mu.RUnlock()

	if !ok {
		return &http.Response{
			StatusCode: http.StatusBadGateway,
			Body:       io.NopCloser(strings.NewReader("in-process worker not found")),
		}, nil
	}

	rec := httptest.NewRecorder()
	ws.Handler().ServeHTTP(rec, req)
	return rec.Result(), nil
}

// RunDistributedLocal runs a distributed load test across N in-process worker engines.
func RunDistributedLocal(ctx context.Context, numWorkers int, sp *spec.RequestSpec, opts PerfOptions, coordOpts *CoordinatorOptions) (*DistributedPerfReport, error) {
	if numWorkers <= 0 {
		numWorkers = 2
	}

	transport := NewInProcessTransport()
	var workers []string
	var serverList []*WorkerServer

	for i := 0; i < numWorkers; i++ {
		wID := fmt.Sprintf("local-worker-%d", i+1)
		wURL := fmt.Sprintf("http://local-worker-%d", i+1)
		ws := NewWorkerServer(wID, fmt.Sprintf("core-%d", i+1), 0)
		transport.Register(wURL, ws)
		workers = append(workers, wURL)
		serverList = append(serverList, ws)
	}

	if coordOpts == nil {
		coordOpts = &CoordinatorOptions{}
	}
	coordOpts.Client = &http.Client{Transport: transport}

	defer func() {
		for _, ws := range serverList {
			_ = ws.Stop(context.Background())
		}
	}()

	return DistributeJob(ctx, workers, sp, opts, coordOpts)
}

