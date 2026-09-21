package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"hit/internal/telemetry"
)

// Config configures the Hit Fleet Hub HTTP server.
type Config struct {
	Addr           string
	DataDir        string
	APIKey         string
	ProjectID      string
	PublicURL      string
	DashboardTitle string
}

// Server implements the cloud-hosted / self-hosted fleet telemetry aggregator.
type Server struct {
	cfg          Config
	store        *Store
	httpServer   *http.Server
	listener     net.Listener
	stopLiveness chan struct{}
}

// NewServer initializes a new Hub Server instance.
func NewServer(cfg Config) (*Server, error) {
	if cfg.Addr == "" {
		cfg.Addr = ":8080"
	}
	if cfg.PublicURL == "" {
		cfg.PublicURL = fmt.Sprintf("http://127.0.0.1%s", cfg.Addr)
		if strings.HasPrefix(cfg.Addr, ":") {
			cfg.PublicURL = fmt.Sprintf("http://127.0.0.1%s", cfg.Addr)
		}
	}
	cfg.PublicURL = strings.TrimRight(cfg.PublicURL, "/")

	store, err := NewStore(cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize hub store: %w", err)
	}

	s := &Server{
		cfg:          cfg,
		store:        store,
		stopLiveness: make(chan struct{}),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/runs", s.handleRunsRoute)
	mux.HandleFunc("/v1/runs", s.handleRunsRoute) // compatibility route
	mux.HandleFunc("/api/v1/runs/", s.handleGetRun)
	mux.HandleFunc("/v1/runs/", s.handleGetRun) // compatibility route
	mux.HandleFunc("/api/v1/nodes", s.handleListNodes)
	mux.HandleFunc("/api/v1/nodes/register", s.handleNodeRegister)
	mux.HandleFunc("/api/v1/nodes/", s.handleNodeRoute)
	mux.HandleFunc("/api/v1/fleet", s.handleGetFleet)
	mux.HandleFunc("/api/v1/fleet/workers", s.handleGetFleetWorkers)
	mux.HandleFunc("/api/v1/dispatch", s.handleDispatchRoute)
	mux.HandleFunc("/api/v1/dispatch/", s.handleDispatchItemRoute)
	mux.HandleFunc("/api/v1/summary", s.handleGetSummary)
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/", s.handleUI)

	s.httpServer = &http.Server{
		Addr:         cfg.Addr,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	return s, nil
}

// Start begins listening on the configured address and starts the liveness loop.
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.cfg.Addr)
	if err != nil {
		return err
	}
	s.listener = ln

	// Background fleet liveness heartbeat monitoring
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.store.CheckLiveness(30*time.Second, 60*time.Second)
			case <-s.stopLiveness:
				return
			}
		}
	}()

	return s.httpServer.Serve(ln)
}

// URL returns the bound listener URL (useful if port 0 was provided).
func (s *Server) URL() string {
	if s.listener != nil {
		return fmt.Sprintf("http://%s", s.listener.Addr().String())
	}
	return s.cfg.PublicURL
}

// Shutdown gracefully stops the hub server.
func (s *Server) Shutdown(ctx context.Context) error {
	select {
	case <-s.stopLiveness:
	default:
		close(s.stopLiveness)
	}
	if s.httpServer != nil {
		return s.httpServer.Shutdown(ctx)
	}
	return nil
}

// Close immediately closes the listener and server.
func (s *Server) Close() error {
	select {
	case <-s.stopLiveness:
	default:
		close(s.stopLiveness)
	}
	if s.httpServer != nil {
		return s.httpServer.Close()
	}
	return nil
}

func (s *Server) authenticate(r *http.Request) bool {
	if s.cfg.APIKey == "" {
		return true // Auth disabled
	}
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		token := strings.TrimPrefix(auth, "Bearer ")
		return token == s.cfg.APIKey
	}
	return false
}

func (s *Server) handleRunsRoute(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.handleIngest(w, r)
	case http.MethodGet:
		s.handleListRuns(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleIngest(w http.ResponseWriter, r *http.Request) {
	if !s.authenticate(r) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	var payload telemetry.RunTelemetryPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"invalid json: %s"}`, err.Error()), http.StatusBadRequest)
		return
	}

	if payload.RunID == "" {
		payload.RunID = telemetry.GenerateRunID()
	}

	// Update or assign node identity defaults if missing
	if payload.Node.ID == "" && payload.Node.CommonName == "" {
		payload.Node = telemetry.DetectNodeContext(payload.Git, payload.CI)
	}

	if err := s.store.SaveRun(&payload); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"failed to store run: %s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	dashboardURL := fmt.Sprintf("%s/#/run/%s", s.cfg.PublicURL, payload.RunID)

	resp := map[string]any{
		"run_id":        payload.RunID,
		"dashboard_url": dashboardURL,
		"status":        "ingested",
		"node":          payload.Node,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))

	filter := RunFilter{
		Owner:   q.Get("owner"),
		NodeID:  q.Get("node"),
		Type:    q.Get("type"),
		Branch:  q.Get("branch"),
		Status:  q.Get("status"),
		Project: q.Get("project"),
		Search:  q.Get("search"),
		Limit:   limit,
		Offset:  offset,
	}

	runs, total := s.store.ListRuns(filter)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"runs":   runs,
		"total":  total,
		"limit":  filter.Limit,
		"offset": filter.Offset,
	})
}

func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	runID := ""
	if strings.HasPrefix(path, "/api/v1/runs/") {
		runID = strings.TrimPrefix(path, "/api/v1/runs/")
	} else if strings.HasPrefix(path, "/v1/runs/") {
		runID = strings.TrimPrefix(path, "/v1/runs/")
	}

	if runID == "" {
		http.NotFound(w, r)
		return
	}

	run, found := s.store.GetRun(runID)
	if !found {
		http.Error(w, `{"error":"run not found"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(run)
}

func (s *Server) handleListNodes(w http.ResponseWriter, r *http.Request) {
	nodes := s.store.ListNodes()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"nodes": nodes,
		"total": len(nodes),
	})
}

func (s *Server) handleGetSummary(w http.ResponseWriter, r *http.Request) {
	summary := s.store.GetSummary()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(summary)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"healthy"}`))
}

func (s *Server) handleUI(w http.ResponseWriter, r *http.Request) {
	html := RenderDashboardHTML(s.cfg.DashboardTitle)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(html))
}

func (s *Server) handleNodeRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.authenticate(r) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	var req NodeRegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"invalid json: %s"}`, err.Error()), http.StatusBadRequest)
		return
	}

	nr, err := s.store.RegisterNode(req)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"failed to register node: %s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(nr)
}

func (s *Server) handleNodeRoute(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/nodes/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}

	nodeID := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	switch action {
	case "heartbeat":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req NodeHeartbeatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			req = NodeHeartbeatRequest{State: "idle"}
		}
		nr, err := s.store.HeartbeatNode(nodeID, req)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(nr)

	case "deregister":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		_ = s.store.DeregisterNode(nodeID)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"deregistered"}`))

	case "queue":
		nr, found := s.store.GetNode(nodeID)
		region := ""
		var caps []string
		if found {
			region = nr.Region
			caps = nr.Capabilities
		}
		if qReg := r.URL.Query().Get("region"); qReg != "" {
			region = qReg
		}
		job, claimed := s.store.ClaimNextJob(nodeID, region, caps)
		w.Header().Set("Content-Type", "application/json")
		if !claimed {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"job":null}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"job": job,
		})

	case "jobs":
		if len(parts) < 4 {
			http.NotFound(w, r)
			return
		}
		jobID := parts[2]
		subAction := parts[3]
		switch subAction {
		case "events":
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			var ev JobEvent
			if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
			_ = s.store.AppendJobLog(jobID, ev)
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"status":"logged"}`))

		case "complete":
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			var compReq struct {
				State     string                         `json:"state"`
				Result    any                            `json:"result"`
				Error     string                         `json:"error"`
				Telemetry *telemetry.RunTelemetryPayload `json:"telemetry,omitempty"`
			}
			if err := json.NewDecoder(r.Body).Decode(&compReq); err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
			telemetryRunID := ""
			if compReq.Telemetry != nil {
				telemetryRunID = compReq.Telemetry.RunID
				if telemetryRunID == "" {
					telemetryRunID = telemetry.GenerateRunID()
					compReq.Telemetry.RunID = telemetryRunID
				}
				_ = s.store.SaveRun(compReq.Telemetry)
			}
			_ = s.store.CompleteJob(jobID, compReq.State, compReq.Result, compReq.Error, telemetryRunID)
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"status":"completed"}`))

		default:
			http.NotFound(w, r)
		}

	default:
		nodes := s.store.ListNodes()
		for _, n := range nodes {
			if n.ID == nodeID || n.CommonName == nodeID {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(n)
				return
			}
		}
		http.Error(w, `{"error":"node not found"}`, http.StatusNotFound)
	}
}

func (s *Server) handleGetFleet(w http.ResponseWriter, r *http.Request) {
	topo := s.store.GetFleetTopology()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(topo)
}

func (s *Server) handleGetFleetWorkers(w http.ResponseWriter, r *http.Request) {
	filter := r.URL.Query().Get("capability")
	if filter == "" {
		filter = r.URL.Query().Get("region")
	}
	workers := s.store.GetAvailableWorkers(filter)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"workers": workers,
		"total":   len(workers),
		"filter":  filter,
	})
}

func (s *Server) handleDispatchRoute(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		if !s.authenticate(r) {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		var req DispatchRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"invalid json: %s"}`, err.Error()), http.StatusBadRequest)
			return
		}
		if req.Type == "" {
			req.Type = "test"
		}
		job, err := s.store.CreateJob(req)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"failed to create job: %s"}`, err.Error()), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(job)

	case http.MethodGet:
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit <= 0 {
			limit = 50
		}
		jobs := s.store.ListJobs(limit)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"jobs":  jobs,
			"total": len(jobs),
		})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleDispatchItemRoute(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/dispatch/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}

	jobID := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	if action == "cancel" {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.authenticate(r) {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		if err := s.store.CancelJob(jobID); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"canceled"}`))
		return
	}

	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	job, ok := s.store.GetJob(jobID)
	if !ok {
		http.Error(w, `{"error":"job not found"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(job)
}


