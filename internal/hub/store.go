package hub

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"hit/internal/telemetry"
)

// NodeRecord tracks an individual machine, worker, or runner in the fleet.
type NodeRecord struct {
	ID            string         `json:"id"`
	CommonName    string         `json:"common_name"`
	Owner         string         `json:"owner"`
	Type          string         `json:"type"` // "developer", "ci-runner", "perf-worker", "probe-daemon", "infra"
	Region        string         `json:"region"`
	EndpointURL   string         `json:"endpoint_url,omitempty"`
	State         string         `json:"state"` // "idle", "busy", "degraded", "offline"
	Capacity      int            `json:"capacity"`
	ActiveJobs    int            `json:"active_jobs"`
	Capabilities  []string       `json:"capabilities,omitempty"`
	Metrics       map[string]any `json:"metrics,omitempty"`
	Hostname      string         `json:"hostname,omitempty"`
	OS            string         `json:"os,omitempty"`
	FirstSeen     time.Time      `json:"first_seen"`
	LastSeen      time.Time      `json:"last_seen"`
	LastHeartbeat time.Time      `json:"last_heartbeat"`
	TotalRuns     int            `json:"total_runs"`
	PassedRuns    int            `json:"passed_runs"`
	FailedRuns    int            `json:"failed_runs"`
	LastRunID     string         `json:"last_run_id,omitempty"`
	LastStatus    string         `json:"last_status,omitempty"` // "passed" | "failed"
}

// NodeRegisterRequest registers a persistent node with the hub.
type NodeRegisterRequest struct {
	ID           string         `json:"id"`
	CommonName   string         `json:"common_name"`
	Owner        string         `json:"owner"`
	Type         string         `json:"type"` // "perf-worker", "probe-daemon", "ci-runner", "developer"
	Region       string         `json:"region"`
	EndpointURL  string         `json:"endpoint_url"`
	Capacity     int            `json:"capacity"`
	Capabilities []string       `json:"capabilities,omitempty"`
	Hostname     string         `json:"hostname"`
	OS           string         `json:"os"`
	Metrics      map[string]any `json:"metrics,omitempty"`
}

// NodeHeartbeatRequest conveys dynamic operational state and health.
type NodeHeartbeatRequest struct {
	State      string         `json:"state"` // "idle", "busy", "degraded"
	ActiveJobs int            `json:"active_jobs"`
	Metrics    map[string]any `json:"metrics,omitempty"`
}

// FleetTopology groups active nodes by geographic region, role, and health state.
type FleetTopology struct {
	TotalNodes    int                      `json:"total_nodes"`
	OnlineNodes   int                      `json:"online_nodes"`
	IdleWorkers   int                      `json:"idle_workers"`
	BusyWorkers   int                      `json:"busy_workers"`
	ActiveProbes  int                      `json:"active_probes"`
	TotalCapacity int                      `json:"total_capacity"`
	ActiveJobs    int                      `json:"active_jobs"`
	ByRegion      map[string][]*NodeRecord `json:"by_region"`
	NodesByRegion map[string][]*NodeRecord `json:"nodes_by_region,omitempty"`
	ByRole        map[string][]*NodeRecord `json:"by_role"`
	ByState       map[string][]*NodeRecord `json:"by_state"`
	Nodes         []*NodeRecord            `json:"nodes"`
}

// FleetSummary aggregates high-level metrics across all nodes and runs.
type FleetSummary struct {
	TotalRuns   int            `json:"total_runs"`
	PassedRuns  int            `json:"passed_runs"`
	FailedRuns  int            `json:"failed_runs"`
	PassRate    float64        `json:"pass_rate"`
	TotalNodes  int            `json:"total_nodes"`
	ActiveNodes int            `json:"active_nodes"` // Seen in last 24h
	AvgDuration float64        `json:"avg_duration_ms"`
	P95Latency  float64        `json:"p95_latency_ms"`
	ByOwner     map[string]int `json:"by_owner"`
	ByType      map[string]int `json:"by_type"`
}

// RunFilter contains query criteria for listing historical runs.
type RunFilter struct {
	Owner   string
	NodeID  string
	Type    string
	Branch  string
	Status  string // "passed" | "failed"
	Project string
	Search  string
	Limit   int
	Offset  int
}

// JobEvent captures a timestamped log or assertion outcome during job execution.
type JobEvent struct {
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"` // "info", "pass", "fail", "error"
	Message   string    `json:"message"`
}

// DispatchJob represents an orchestrated run dispatched across the fleet.
type DispatchJob struct {
	ID             string     `json:"id"`
	Type           string     `json:"type"` // "test" | "perf"
	Name           string     `json:"name"`
	TargetNodeID   string     `json:"target_node_id,omitempty"`
	TargetRegion   string     `json:"target_region,omitempty"`
	TargetRole     string     `json:"target_role,omitempty"`
	AssignedNode   string     `json:"assigned_node,omitempty"`
	SpecYAML       string     `json:"spec_yaml"`
	Concurrency    int        `json:"concurrency,omitempty"`
	Requests       int        `json:"requests,omitempty"`
	DurationS      float64    `json:"duration_s,omitempty"`
	State          string     `json:"state"` // "queued", "assigned", "running", "completed", "failed", "canceled"
	CreatedAt      time.Time  `json:"created_at"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
	Logs           []JobEvent `json:"logs,omitempty"`
	Result         any        `json:"result,omitempty"`
	Error          string     `json:"error,omitempty"`
	TelemetryRunID string     `json:"telemetry_run_id,omitempty"`
}

// DispatchRequest contains parameters to schedule a new execution across fleet nodes.
type DispatchRequest struct {
	Type         string  `json:"type"` // "test" | "perf"
	Name         string  `json:"name,omitempty"`
	TargetNodeID string  `json:"target_node_id,omitempty"`
	TargetRegion string  `json:"target_region,omitempty"`
	TargetRole   string  `json:"target_role,omitempty"`
	SpecYAML     string  `json:"spec_yaml"`
	Concurrency  int     `json:"concurrency,omitempty"`
	Requests     int     `json:"requests,omitempty"`
	DurationS    float64 `json:"duration_s,omitempty"`
}

// Store provides thread-safe in-memory caching and optional disk persistence.
type Store struct {
	mu       sync.RWMutex
	runs     map[string]*telemetry.RunTelemetryPayload
	runOrder []string // newest first
	nodes    map[string]*NodeRecord
	jobs     map[string]*DispatchJob
	jobOrder []string // newest first
	dataDir  string
}

// NewStore initializes a new Store instance.
func NewStore(dataDir string) (*Store, error) {
	s := &Store{
		runs:     make(map[string]*telemetry.RunTelemetryPayload),
		runOrder: make([]string, 0),
		nodes:    make(map[string]*NodeRecord),
		jobs:     make(map[string]*DispatchJob),
		jobOrder: make([]string, 0),
		dataDir:  dataDir,
	}

	if dataDir != "" {
		if err := os.MkdirAll(dataDir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create hub data directory: %w", err)
		}
		if err := s.load(); err != nil {
			// Non-fatal, start fresh if file corrupted
			fmt.Fprintf(os.Stderr, "hub store warning: load failed: %v\n", err)
		}
	}

	return s, nil
}

// SaveRun registers a run payload and updates node discovery.
func (s *Store) SaveRun(payload *telemetry.RunTelemetryPayload) error {
	if payload == nil || payload.RunID == "" {
		return fmt.Errorf("invalid nil or empty run payload")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	if payload.Timestamp == "" {
		payload.Timestamp = now.Format(time.RFC3339)
	}

	// Update runs map
	if _, exists := s.runs[payload.RunID]; !exists {
		s.runOrder = append([]string{payload.RunID}, s.runOrder...)
	}
	s.runs[payload.RunID] = payload

	// Resolve node identity
	nodeID := payload.Node.ID
	if nodeID == "" {
		nodeID = fmt.Sprintf("node_%s", payload.Node.CommonName)
		if nodeID == "node_" {
			nodeID = "node_default"
		}
	}

	commonName := payload.Node.CommonName
	if commonName == "" {
		commonName = payload.Node.Hostname
		if commonName == "" {
			commonName = "unnamed-node"
		}
	}

	owner := payload.Node.Owner
	if owner == "" {
		if payload.Git.Author != "" {
			owner = payload.Git.Author
		} else {
			owner = "unknown-owner"
		}
	}

	nodeType := payload.Node.Type
	if nodeType == "" {
		if payload.CI.Provider != "" {
			nodeType = "ci-runner"
		} else {
			nodeType = "developer"
		}
	}

	status := "passed"
	if payload.Summary.Failed > 0 {
		status = "failed"
	}

	nr, exists := s.nodes[nodeID]
	if !exists {
		nr = &NodeRecord{
			ID:            nodeID,
			CommonName:    commonName,
			Owner:         owner,
			Type:          nodeType,
			Region:        "local",
			State:         "idle",
			Hostname:      payload.Node.Hostname,
			OS:            payload.Node.OS,
			FirstSeen:     now,
			LastSeen:      now,
			LastHeartbeat: now,
		}
		s.nodes[nodeID] = nr
	}

	nr.CommonName = commonName
	nr.Owner = owner
	nr.Type = nodeType
	nr.LastSeen = now
	nr.LastHeartbeat = now
	nr.TotalRuns++
	if status == "passed" {
		nr.PassedRuns++
	} else {
		nr.FailedRuns++
	}
	nr.LastRunID = payload.RunID
	nr.LastStatus = status

	// Persist to disk if dataDir configured
	if s.dataDir != "" {
		s.persistRun(payload)
	}

	return nil
}

func (s *Store) persistRun(payload *telemetry.RunTelemetryPayload) {
	filePath := filepath.Join(s.dataDir, "runs.jsonl")
	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	data, err := json.Marshal(payload)
	if err == nil {
		f.Write(data)
		f.WriteString("\n")
	}
}

func (s *Store) load() error {
	filePath := filepath.Join(s.dataDir, "runs.jsonl")
	f, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// Allow large lines up to 10MB
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 10*1024*1024)

	var loaded []*telemetry.RunTelemetryPayload
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var p telemetry.RunTelemetryPayload
		if err := json.Unmarshal([]byte(line), &p); err == nil && p.RunID != "" {
			loaded = append(loaded, &p)
		}
	}

	// Replay loaded runs to rebuild state
	for _, p := range loaded {
		s.SaveRun(p)
	}

	return scanner.Err()
}

// GetRun returns an individual run by its ID.
func (s *Store) GetRun(runID string) (*telemetry.RunTelemetryPayload, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.runs[runID]
	return p, ok
}

// ListRuns returns filtered runs and total count matching the criteria.
func (s *Store) ListRuns(f RunFilter) ([]*telemetry.RunTelemetryPayload, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var matched []*telemetry.RunTelemetryPayload

	for _, id := range s.runOrder {
		p := s.runs[id]
		if p == nil {
			continue
		}

		if f.Owner != "" && !strings.EqualFold(p.Node.Owner, f.Owner) && !strings.EqualFold(p.Git.Author, f.Owner) {
			continue
		}
		if f.NodeID != "" && p.Node.ID != f.NodeID && p.Node.CommonName != f.NodeID {
			continue
		}
		if f.Type != "" && !strings.EqualFold(p.Node.Type, f.Type) {
			continue
		}
		if f.Branch != "" && !strings.EqualFold(p.Git.Branch, f.Branch) {
			continue
		}
		if f.Project != "" && !strings.EqualFold(p.ProjectID, f.Project) && !strings.EqualFold(p.Zone, f.Project) {
			continue
		}
		if f.Status != "" {
			passed := p.Summary.Failed == 0
			if f.Status == "passed" && !passed {
				continue
			}
			if f.Status == "failed" && passed {
				continue
			}
		}
		if f.Search != "" {
			term := strings.ToLower(f.Search)
			combined := strings.ToLower(fmt.Sprintf("%s %s %s %s %s %s",
				p.RunID, p.Node.CommonName, p.Node.Owner, p.Git.Branch, p.Git.Commit, p.ProjectID))
			if !strings.Contains(combined, term) {
				continue
			}
		}

		matched = append(matched, p)
	}

	total := len(matched)
	if f.Offset > 0 {
		if f.Offset >= len(matched) {
			return []*telemetry.RunTelemetryPayload{}, total
		}
		matched = matched[f.Offset:]
	}

	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	if len(matched) > limit {
		matched = matched[:limit]
	}

	return matched, total
}

// ListNodes returns all discovered machines and runners.
func (s *Store) ListNodes() []*NodeRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()

	list := make([]*NodeRecord, 0, len(s.nodes))
	for _, nr := range s.nodes {
		clone := *nr
		list = append(list, &clone)
	}

	// Sort by LastSeen descending
	sort.Slice(list, func(i, j int) bool {
		return list[i].LastSeen.After(list[j].LastSeen)
	})

	return list
}

// GetSummary returns aggregated fleet-wide metrics.
func (s *Store) GetSummary() FleetSummary {
	s.mu.RLock()
	defer s.mu.RUnlock()

	summary := FleetSummary{
		TotalRuns: len(s.runs),
		ByOwner:   make(map[string]int),
		ByType:    make(map[string]int),
	}

	now := time.Now().UTC()
	cutoff24h := now.Add(-24 * time.Hour)

	var durations []float64
	var p95s []float64

	for _, nr := range s.nodes {
		summary.TotalNodes++
		if nr.LastSeen.After(cutoff24h) {
			summary.ActiveNodes++
		}
	}

	for _, p := range s.runs {
		if p.Summary.Failed == 0 {
			summary.PassedRuns++
		} else {
			summary.FailedRuns++
		}

		durations = append(durations, p.Summary.DurationMs)
		if v, ok := p.Percentiles["p95"]; ok && v > 0 {
			p95s = append(p95s, v)
		}

		owner := p.Node.Owner
		if owner != "" {
			summary.ByOwner[owner]++
		}

		nodeType := p.Node.Type
		if nodeType != "" {
			summary.ByType[nodeType]++
		}
	}

	if summary.TotalRuns > 0 {
		summary.PassRate = float64(summary.PassedRuns) / float64(summary.TotalRuns) * 100

		var totalDur float64
		for _, d := range durations {
			totalDur += d
		}
		summary.AvgDuration = totalDur / float64(len(durations))

		if len(p95s) > 0 {
			sort.Float64s(p95s)
			summary.P95Latency = p95s[len(p95s)*95/100]
		}
	}

	return summary
}

// RegisterNode enrolls a node or persistent worker into the fleet registry.
func (s *Store) RegisterNode(req NodeRegisterRequest) (*NodeRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	nodeID := req.ID
	if nodeID == "" {
		nodeID = fmt.Sprintf("node_%s_%d", req.Type, now.UnixNano()%100000)
	}

	commonName := req.CommonName
	if commonName == "" {
		commonName = req.Hostname
		if commonName == "" {
			commonName = nodeID
		}
	}

	owner := req.Owner
	if owner == "" {
		owner = "platform"
	}

	region := req.Region
	if region == "" {
		region = "local"
	}

	nodeType := req.Type
	if nodeType == "" {
		nodeType = "perf-worker"
	}

	capacity := req.Capacity
	if capacity <= 0 {
		capacity = 50
	}

	nr, exists := s.nodes[nodeID]
	if !exists {
		nr = &NodeRecord{
			ID:         nodeID,
			FirstSeen:  now,
			PassedRuns: 0,
			FailedRuns: 0,
			TotalRuns:  0,
		}
		s.nodes[nodeID] = nr
	}

	nr.CommonName = commonName
	nr.Owner = owner
	nr.Type = nodeType
	nr.Region = region
	nr.EndpointURL = req.EndpointURL
	nr.Capacity = capacity
	nr.Capabilities = req.Capabilities
	nr.State = "idle"
	nr.Hostname = req.Hostname
	nr.OS = req.OS
	nr.Metrics = req.Metrics
	nr.LastSeen = now
	nr.LastHeartbeat = now

	return nr, nil
}

// HeartbeatNode updates a node's operational state, load, and timestamp.
func (s *Store) HeartbeatNode(nodeID string, req NodeHeartbeatRequest) (*NodeRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	nr, exists := s.nodes[nodeID]
	if !exists {
		return nil, fmt.Errorf("node %q not found", nodeID)
	}

	now := time.Now().UTC()
	nr.LastSeen = now
	nr.LastHeartbeat = now
	if req.State != "" {
		nr.State = req.State
	}
	nr.ActiveJobs = req.ActiveJobs
	if req.Metrics != nil {
		nr.Metrics = req.Metrics
	}

	return nr, nil
}

// DeregisterNode marks a node as offline.
func (s *Store) DeregisterNode(nodeID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	nr, exists := s.nodes[nodeID]
	if !exists {
		return fmt.Errorf("node %q not found", nodeID)
	}

	nr.State = "offline"
	nr.ActiveJobs = 0
	return nil
}

// GetFleetTopology groups discovered nodes by region, role, and health state.
func (s *Store) GetFleetTopology() FleetTopology {
	s.mu.RLock()
	defer s.mu.RUnlock()

	topo := FleetTopology{
		TotalNodes: len(s.nodes),
		ByRegion:   make(map[string][]*NodeRecord),
		ByRole:     make(map[string][]*NodeRecord),
		ByState:    make(map[string][]*NodeRecord),
		Nodes:      make([]*NodeRecord, 0, len(s.nodes)),
	}

	for _, nr := range s.nodes {
		clone := *nr
		reg := nr.Region
		if reg == "" {
			reg = "default"
		}
		topo.ByRegion[reg] = append(topo.ByRegion[reg], &clone)

		role := nr.Type
		if role == "" {
			role = "developer"
		}
		topo.ByRole[role] = append(topo.ByRole[role], &clone)

		state := nr.State
		if state == "" {
			state = "idle"
		}
		topo.ByState[state] = append(topo.ByState[state], &clone)

		topo.TotalCapacity += nr.Capacity
		topo.ActiveJobs += nr.ActiveJobs
		topo.Nodes = append(topo.Nodes, &clone)

		if state != "offline" {
			topo.OnlineNodes++
		}
		if nr.Type == "perf-worker" || hasCapability(nr, "perf-worker") {
			if state == "busy" {
				topo.BusyWorkers++
			} else if state == "idle" || state == "healthy" {
				topo.IdleWorkers++
			}
		} else if nr.Type == "probe-daemon" || nr.Type == "probe-runner" || hasCapability(nr, "probe-runner") {
			if state != "offline" {
				topo.ActiveProbes++
			}
		}
	}

	topo.NodesByRegion = topo.ByRegion
	return topo
}

// GetNode retrieves a node record by its identifier.
func (s *Store) GetNode(nodeID string) (*NodeRecord, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	nr, exists := s.nodes[nodeID]
	if !exists {
		return nil, false
	}
	clone := *nr
	return &clone, true
}

// GetAvailableWorkers returns idle or healthy load workers, optionally filtered by region or capability.
func (s *Store) GetAvailableWorkers(filter string) []*NodeRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var workers []*NodeRecord
	for _, nr := range s.nodes {
		isPerf := nr.Type == "perf-worker" || hasCapability(nr, "perf-worker")
		if !isPerf {
			continue
		}
		if nr.State == "offline" || nr.State == "degraded" {
			continue
		}
		if filter != "" {
			matchCap := hasCapability(nr, filter)
			matchRegion := strings.EqualFold(nr.Region, filter)
			matchType := strings.EqualFold(nr.Type, filter)
			if !matchCap && !matchRegion && !matchType {
				continue
			}
		}
		clone := *nr
		workers = append(workers, &clone)
	}

	return workers
}

// CheckLiveness transitions nodes to degraded/offline if heartbeats are missed.
func (s *Store) CheckLiveness(degradedTimeout, offlineTimeout time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	for _, nr := range s.nodes {
		// Only check persistent daemons
		isDaemon := nr.Type == "perf-worker" || nr.Type == "probe-daemon" || nr.Type == "probe-runner" ||
			hasCapability(nr, "perf-worker") || hasCapability(nr, "probe-runner")
		if !isDaemon {
			continue
		}
		if nr.State == "offline" {
			continue
		}

		elapsed := now.Sub(nr.LastHeartbeat)
		if elapsed > offlineTimeout {
			nr.State = "offline"
			nr.ActiveJobs = 0
		} else if elapsed > degradedTimeout && nr.State != "busy" {
			nr.State = "degraded"
		}
	}
}

func hasCapability(nr *NodeRecord, cap string) bool {
	for _, c := range nr.Capabilities {
		if strings.EqualFold(c, cap) {
			return true
		}
	}
	return false
}

// CreateJob queues a new execution request and attempts to match with an available node.
func (s *Store) CreateJob(req DispatchRequest) (*DispatchJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	jobID := fmt.Sprintf("job_%x_%d", now.Unix(), len(s.jobs)+1)

	name := req.Name
	if name == "" {
		if req.Type == "perf" {
			name = fmt.Sprintf("Perf Load Test (%d reqs)", req.Requests)
		} else {
			name = "Ad-hoc Test Run"
		}
	}

	job := &DispatchJob{
		ID:           jobID,
		Type:         req.Type,
		Name:         name,
		TargetNodeID: req.TargetNodeID,
		TargetRegion: req.TargetRegion,
		TargetRole:   req.TargetRole,
		SpecYAML:     req.SpecYAML,
		Concurrency:  req.Concurrency,
		Requests:     req.Requests,
		DurationS:    req.DurationS,
		State:        "queued",
		CreatedAt:    now,
		Logs:         make([]JobEvent, 0),
	}

	// Try immediate assignment
	if req.TargetNodeID != "" {
		if targetNode, exists := s.nodes[req.TargetNodeID]; exists && targetNode.State != "offline" {
			job.State = "assigned"
			job.AssignedNode = targetNode.ID
		}
	} else {
		// Dynamic match: find an idle worker matching region & role
		for _, node := range s.nodes {
			if node.State == "idle" || node.State == "healthy" {
				if req.TargetRegion != "" && !strings.EqualFold(node.Region, req.TargetRegion) {
					continue
				}
				if req.TargetRole != "" && !hasCapability(node, req.TargetRole) && !strings.EqualFold(node.Type, req.TargetRole) {
					continue
				}
				job.State = "assigned"
				job.AssignedNode = node.ID
				break
			}
		}
	}

	job.Logs = append(job.Logs, JobEvent{
		Timestamp: now,
		Level:     "info",
		Message:   fmt.Sprintf("Job %s queued with status %s", jobID, job.State),
	})

	s.jobs[jobID] = job
	s.jobOrder = append([]string{jobID}, s.jobOrder...)

	return job.clone(), nil
}

// GetJob retrieves a dispatch job by ID.
func (s *Store) GetJob(jobID string) (*DispatchJob, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	j, ok := s.jobs[jobID]
	if !ok {
		return nil, false
	}
	return j.clone(), true
}

// ListJobs returns recent dispatch jobs.
func (s *Store) ListJobs(limit int) []*DispatchJob {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 {
		limit = 50
	}

	var list []*DispatchJob
	for _, id := range s.jobOrder {
		if j, ok := s.jobs[id]; ok {
			list = append(list, j.clone())
			if len(list) >= limit {
				break
			}
		}
	}
	return list
}

// ClaimNextJob is called by a worker to fetch and claim the next pending job.
func (s *Store) ClaimNextJob(nodeID, region string, capabilities []string) (*DispatchJob, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()

	// Traverse jobs in FIFO order (oldest first)
	for i := len(s.jobOrder) - 1; i >= 0; i-- {
		id := s.jobOrder[i]
		job, ok := s.jobs[id]
		if !ok {
			continue
		}

		if job.State != "queued" && job.State != "assigned" {
			continue
		}

		// If already assigned to another node, skip
		if job.AssignedNode != "" && job.AssignedNode != nodeID {
			continue
		}

		// If targeted to another node, skip
		if job.TargetNodeID != "" && job.TargetNodeID != nodeID {
			continue
		}

		// Check region match if specified
		if job.TargetRegion != "" && region != "" && !strings.EqualFold(job.TargetRegion, region) {
			continue
		}

		// Check role/capability match
		if job.TargetRole != "" {
			matched := false
			for _, c := range capabilities {
				if strings.EqualFold(c, job.TargetRole) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}

		// Claim job
		job.State = "running"
		job.AssignedNode = nodeID
		job.StartedAt = &now
		job.Logs = append(job.Logs, JobEvent{
			Timestamp: now,
			Level:     "info",
			Message:   fmt.Sprintf("Job claimed by node %s (region: %s)", nodeID, region),
		})

		// Update node record
		if nr, exists := s.nodes[nodeID]; exists {
			nr.State = "busy"
			nr.ActiveJobs++
		}

		return job.clone(), true
	}

	return nil, false
}

// AppendJobLog appends an event to an active job.
func (s *Store) AppendJobLog(jobID string, event JobEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, ok := s.jobs[jobID]
	if !ok {
		return fmt.Errorf("job %s not found", jobID)
	}

	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	job.Logs = append(job.Logs, event)
	return nil
}

// CompleteJob finalizes job execution and releases node concurrency.
func (s *Store) CompleteJob(jobID string, state string, result any, errMsg string, telemetryRunID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, ok := s.jobs[jobID]
	if !ok {
		return fmt.Errorf("job %s not found", jobID)
	}

	now := time.Now().UTC()
	if state == "" {
		state = "completed"
		if errMsg != "" {
			state = "failed"
		}
	}
	job.State = state
	job.CompletedAt = &now
	job.Result = result
	job.Error = errMsg
	job.TelemetryRunID = telemetryRunID

	level := "pass"
	if state == "failed" {
		level = "fail"
	}
	job.Logs = append(job.Logs, JobEvent{
		Timestamp: now,
		Level:     level,
		Message:   fmt.Sprintf("Job finished with state: %s", state),
	})

	// Release node
	if job.AssignedNode != "" {
		if nr, exists := s.nodes[job.AssignedNode]; exists {
			if nr.ActiveJobs > 0 {
				nr.ActiveJobs--
			}
			if nr.ActiveJobs == 0 && nr.State != "offline" {
				nr.State = "idle"
			}
		}
	}

	return nil
}

// CancelJob aborts a queued or running job.
func (s *Store) CancelJob(jobID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, ok := s.jobs[jobID]
	if !ok {
		return fmt.Errorf("job %s not found", jobID)
	}

	if job.State == "completed" || job.State == "failed" || job.State == "canceled" {
		return nil
	}

	now := time.Now().UTC()
	job.State = "canceled"
	job.CompletedAt = &now
	job.Logs = append(job.Logs, JobEvent{
		Timestamp: now,
		Level:     "info",
		Message:   "Job canceled by user",
	})

	// Release node
	if job.AssignedNode != "" {
		if nr, exists := s.nodes[job.AssignedNode]; exists {
			if nr.ActiveJobs > 0 {
				nr.ActiveJobs--
			}
			if nr.ActiveJobs == 0 && nr.State != "offline" {
				nr.State = "idle"
			}
		}
	}

	return nil
}

func (j *DispatchJob) clone() *DispatchJob {
	if j == nil {
		return nil
	}
	cp := *j
	cp.Logs = make([]JobEvent, len(j.Logs))
	copy(cp.Logs, j.Logs)
	return &cp
}


