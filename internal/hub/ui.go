package hub

// RenderDashboardHTML returns the complete, self-contained single-page dashboard HTML.
func RenderDashboardHTML(title string) string {
	if title == "" {
		title = "Hit Cloud Fleet Hub"
	}
	return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>` + title + `</title>
<style>
  :root {
    --bg-primary: #0b0f19;
    --bg-secondary: #111827;
    --bg-card: #1f2937;
    --bg-card-hover: #374151;
    --border-color: #374151;
    --text-primary: #f9fafb;
    --text-secondary: #9ca3af;
    --text-muted: #6b7280;
    --accent: #38bdf8;
    --accent-hover: #0ea5e9;
    --success: #22c55e;
    --success-bg: rgba(34, 197, 94, 0.15);
    --danger: #ef4444;
    --danger-bg: rgba(239, 68, 68, 0.15);
    --warning: #f59e0b;
    --warning-bg: rgba(245, 158, 11, 0.15);
    --info-bg: rgba(56, 189, 248, 0.15);
    --font-mono: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace;
  }
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body {
    background-color: var(--bg-primary);
    color: var(--text-primary);
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
    line-height: 1.5;
    padding: 0;
    margin: 0;
  }
  header {
    background: var(--bg-secondary);
    border-bottom: 1px solid var(--border-color);
    padding: 1rem 2rem;
    display: flex;
    justify-content: space-between;
    align-items: center;
  }
  .brand {
    display: flex;
    align-items: center;
    gap: 0.75rem;
  }
  .brand-logo {
    background: linear-gradient(135deg, #38bdf8, #6366f1);
    color: #fff;
    font-weight: 800;
    font-size: 1.1rem;
    width: 36px;
    height: 36px;
    border-radius: 8px;
    display: flex;
    align-items: center;
    justify-content: center;
    letter-spacing: -0.5px;
  }
  .brand-title {
    font-size: 1.25rem;
    font-weight: 700;
    color: var(--text-primary);
  }
  .badge-live {
    font-size: 0.7rem;
    font-weight: 600;
    padding: 0.2rem 0.6rem;
    border-radius: 9999px;
    background: var(--success-bg);
    color: var(--success);
    border: 1px solid var(--success);
    display: flex;
    align-items: center;
    gap: 0.35rem;
  }
  .pulse-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--success);
    animation: pulse 1.5s infinite;
  }
  @keyframes pulse {
    0% { opacity: 1; transform: scale(1); }
    50% { opacity: 0.4; transform: scale(1.3); }
    100% { opacity: 1; transform: scale(1); }
  }
  .header-actions {
    display: flex;
    align-items: center;
    gap: 1rem;
  }
  .btn {
    background: var(--bg-card);
    color: var(--text-primary);
    border: 1px solid var(--border-color);
    padding: 0.5rem 1rem;
    border-radius: 6px;
    font-size: 0.875rem;
    cursor: pointer;
    transition: all 0.15s ease;
    text-decoration: none;
    display: inline-flex;
    align-items: center;
    gap: 0.4rem;
  }
  .btn:hover { background: var(--bg-card-hover); }
  
  .container {
    max-width: 1400px;
    margin: 0 auto;
    padding: 1.5rem 2rem;
  }

  /* Metric cards */
  .stats-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
    gap: 1rem;
    margin-bottom: 2rem;
  }
  .stat-card {
    background: var(--bg-secondary);
    border: 1px solid var(--border-color);
    border-radius: 8px;
    padding: 1.25rem;
  }
  .stat-label {
    color: var(--text-muted);
    font-size: 0.8rem;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    margin-bottom: 0.5rem;
  }
  .stat-value {
    font-size: 1.75rem;
    font-weight: 700;
    color: var(--text-primary);
  }
  .stat-subtext {
    font-size: 0.8rem;
    color: var(--text-secondary);
    margin-top: 0.25rem;
  }

  /* Navigation Tabs */
  .tabs-nav {
    display: flex;
    gap: 0.5rem;
    border-bottom: 1px solid var(--border-color);
    margin-bottom: 1.5rem;
  }
  .tab-btn {
    background: none;
    border: none;
    color: var(--text-secondary);
    font-size: 0.95rem;
    font-weight: 600;
    padding: 0.75rem 1.25rem;
    cursor: pointer;
    position: relative;
    transition: color 0.15s ease;
  }
  .tab-btn:hover { color: var(--text-primary); }
  .tab-btn.active {
    color: var(--accent);
  }
  .tab-btn.active::after {
    content: "";
    position: absolute;
    bottom: -1px;
    left: 0;
    right: 0;
    height: 2px;
    background: var(--accent);
  }

  /* Filter toolbar */
  .filter-bar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 1rem;
    background: var(--bg-secondary);
    border: 1px solid var(--border-color);
    border-radius: 8px;
    padding: 0.75rem 1rem;
    margin-bottom: 1.5rem;
  }
  .filter-group {
    display: flex;
    align-items: center;
    gap: 0.75rem;
  }
  input[type="text"], select {
    background: var(--bg-card);
    color: var(--text-primary);
    border: 1px solid var(--border-color);
    padding: 0.5rem 0.75rem;
    border-radius: 6px;
    font-size: 0.875rem;
    outline: none;
  }
  input[type="text"]:focus, select:focus {
    border-color: var(--accent);
  }

  /* Table layout */
  .table-card {
    background: var(--bg-secondary);
    border: 1px solid var(--border-color);
    border-radius: 8px;
    overflow: hidden;
  }
  table {
    width: 100%;
    border-collapse: collapse;
    text-align: left;
    font-size: 0.875rem;
  }
  th {
    background: var(--bg-card);
    padding: 0.85rem 1rem;
    font-weight: 600;
    color: var(--text-secondary);
    border-bottom: 1px solid var(--border-color);
  }
  td {
    padding: 0.85rem 1rem;
    border-bottom: 1px solid var(--border-color);
  }
  tr:hover td {
    background: rgba(255, 255, 255, 0.02);
  }
  .badge {
    display: inline-block;
    padding: 0.2rem 0.55rem;
    border-radius: 4px;
    font-size: 0.75rem;
    font-weight: 600;
    text-transform: uppercase;
  }
  .badge-pass { background: var(--success-bg); color: var(--success); }
  .badge-fail { background: var(--danger-bg); color: var(--danger); }
  .badge-dev { background: var(--info-bg); color: var(--accent); }
  .badge-ci { background: rgba(168, 85, 247, 0.15); color: #c084fc; }
  .font-mono { font-family: var(--font-mono); font-size: 0.82rem; }
  .run-link {
    color: var(--accent);
    text-decoration: none;
    font-weight: 600;
    cursor: pointer;
  }
  .run-link:hover { text-decoration: underline; }

  /* Node Cards Grid */
  .nodes-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
    gap: 1.25rem;
  }
  .node-card {
    background: var(--bg-secondary);
    border: 1px solid var(--border-color);
    border-radius: 8px;
    padding: 1.25rem;
    display: flex;
    flex-direction: column;
    justify-content: space-between;
  }
  .node-header {
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    margin-bottom: 0.75rem;
  }
  .node-name {
    font-size: 1.05rem;
    font-weight: 700;
    color: var(--text-primary);
  }
  .node-owner {
    font-size: 0.8rem;
    color: var(--text-secondary);
    margin-bottom: 0.5rem;
  }
  .node-stats {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 0.5rem;
    background: var(--bg-card);
    padding: 0.75rem;
    border-radius: 6px;
    margin: 0.75rem 0;
  }
  .node-stat-item .val {
    font-size: 1.1rem;
    font-weight: 700;
  }
  .node-stat-item .lbl {
    font-size: 0.7rem;
    color: var(--text-muted);
    text-transform: uppercase;
  }

  /* Modal Details */
  .modal-backdrop {
    display: none;
    position: fixed;
    top: 0; left: 0; right: 0; bottom: 0;
    background: rgba(0, 0, 0, 0.75);
    z-index: 1000;
    align-items: center;
    justify-content: center;
    padding: 2rem;
  }
  .modal-backdrop.show { display: flex; }
  .modal-content {
    background: var(--bg-secondary);
    border: 1px solid var(--border-color);
    border-radius: 12px;
    max-width: 900px;
    width: 100%;
    max-height: 90vh;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }
  .modal-header {
    padding: 1.25rem 1.5rem;
    border-bottom: 1px solid var(--border-color);
    display: flex;
    justify-content: space-between;
    align-items: center;
  }
  .modal-body {
    padding: 1.5rem;
    overflow-y: auto;
  }
  .modal-close {
    background: none;
    border: none;
    color: var(--text-muted);
    font-size: 1.5rem;
    cursor: pointer;
  }
  .modal-close:hover { color: var(--text-primary); }
  /* Fleet Topology */
  .fleet-summary-bar {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
    gap: 1rem;
    margin-bottom: 1.5rem;
  }
  .fleet-region-block {
    background: var(--bg-secondary);
    border: 1px solid var(--border-color);
    border-radius: 8px;
    padding: 1.25rem;
    margin-bottom: 1.5rem;
  }
  .fleet-region-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    border-bottom: 1px solid var(--border-color);
    padding-bottom: 0.75rem;
    margin-bottom: 1rem;
  }
  .fleet-region-title {
    font-size: 1.05rem;
    font-weight: 700;
    color: var(--text-primary);
    display: flex;
    align-items: center;
    gap: 0.5rem;
  }
  .fleet-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
    gap: 1.25rem;
  }
  .fleet-card {
    background: var(--bg-card);
    border: 1px solid var(--border-color);
    border-radius: 8px;
    padding: 1.25rem;
    display: flex;
    flex-direction: column;
    justify-content: space-between;
  }
  .state-pill {
    font-size: 0.72rem;
    font-weight: 700;
    padding: 0.25rem 0.6rem;
    border-radius: 9999px;
    text-transform: uppercase;
    display: inline-flex;
    align-items: center;
    gap: 0.35rem;
  }
  .state-idle { background: var(--success-bg); color: var(--success); border: 1px solid var(--success); }
  .state-busy { background: rgba(56, 189, 248, 0.2); color: var(--accent); border: 1px solid var(--accent); }
  .state-degraded { background: var(--warning-bg); color: var(--warning); border: 1px solid var(--warning); }
  .state-offline { background: var(--danger-bg); color: var(--danger); border: 1px solid var(--danger); }
  .cap-bar-bg {
    width: 100%;
    height: 7px;
    background: rgba(255, 255, 255, 0.1);
    border-radius: 4px;
    overflow: hidden;
    margin-top: 0.35rem;
  }
  .cap-bar-fill {
    height: 100%;
    background: var(--accent);
    border-radius: 4px;
    transition: width 0.3s ease;
  }
</style>
</head>
<body>

<header>
  <div class="brand">
    <div class="brand-logo">HIT</div>
    <div>
      <div class="brand-title">Hit Cloud Fleet Hub</div>
    </div>
    <div class="badge-live"><div class="pulse-dot"></div> CONNECTED</div>
  </div>
  <div class="header-actions">
    <button class="btn" style="background:#0284c7; color:#fff; border-color:#38bdf8; font-weight:600;" onclick="openDispatchModal()">⚡ Dispatch Run</button>
    <button class="btn" onclick="loadAllData()">↻ Refresh</button>
  </div>
</header>

<div class="container">
  <!-- Metric Cards -->
  <div class="stats-grid">
    <div class="stat-card">
      <div class="stat-label">Total Test Runs</div>
      <div class="stat-value" id="stat-total-runs">-</div>
      <div class="stat-subtext" id="stat-run-breakdown">Across all developers & CI</div>
    </div>
    <div class="stat-card">
      <div class="stat-label">Fleet Pass Rate</div>
      <div class="stat-value" id="stat-pass-rate">-</div>
      <div class="stat-subtext" id="stat-pass-sub">Assertion reliability</div>
    </div>
    <div class="stat-card">
      <div class="stat-label">Active Fleet Nodes</div>
      <div class="stat-value" id="stat-active-nodes">-</div>
      <div class="stat-subtext" id="stat-nodes-sub">Developer laptops & CI runners</div>
    </div>
    <div class="stat-card">
      <div class="stat-label">Fleet P95 Tail Latency</div>
      <div class="stat-value" id="stat-p95-latency">-</div>
      <div class="stat-subtext">95th percentile response time</div>
    </div>
  </div>

  <!-- Navigation Tabs -->
  <div class="tabs-nav">
    <button class="tab-btn active" id="tab-team" onclick="switchTab('team')">Team Runs (Aggregated)</button>
    <button class="tab-btn" id="tab-my" onclick="switchTab('my')">My Runs (Developer Node)</button>
    <button class="tab-btn" id="tab-nodes" onclick="switchTab('nodes')">Nodes & Runners</button>
    <button class="tab-btn" id="tab-fleet" onclick="switchTab('fleet')">Fleet Topology (Live)</button>
  </div>

  <!-- Filter Bar -->
  <div class="filter-bar" id="filter-bar">
    <div class="filter-group">
      <input type="text" id="search-input" placeholder="Search branch, commit, run ID..." oninput="applyFilters()">
      <select id="status-filter" onchange="applyFilters()">
        <option value="">All Statuses</option>
        <option value="passed">Passed Only</option>
        <option value="failed">Failed Only</option>
      </select>
      <select id="type-filter" onchange="applyFilters()">
        <option value="">All Node Types</option>
        <option value="developer">Developer Workstations</option>
        <option value="ci-runner">CI / CD Runners</option>
      </select>
    </div>
    <div class="filter-group" id="owner-select-group" style="display:none;">
      <label style="font-size: 0.85rem; color: var(--text-secondary);">Developer:</label>
      <select id="owner-select" onchange="applyFilters()">
        <option value="">Select Developer Owner...</option>
      </select>
    </div>
  </div>

  <!-- Runs Table View (Team or My Runs) -->
  <div class="table-card" id="runs-view">
    <table>
      <thead>
        <tr>
          <th>Status</th>
          <th>Run ID</th>
          <th>Node & Type</th>
          <th>Owner / Author</th>
          <th>Git Branch & Commit</th>
          <th>Endpoints</th>
          <th>Duration</th>
          <th>P95 Latency</th>
          <th>Time</th>
        </tr>
      </thead>
      <tbody id="runs-table-body">
        <tr><td colspan="9" class="empty-state">Loading runs...</td></tr>
      </tbody>
    </table>
  </div>

  <!-- Nodes Directory View -->
  <div id="nodes-view" style="display:none;">
    <div class="nodes-grid" id="nodes-grid">
      <div class="empty-state">Loading discovered nodes...</div>
    </div>
  </div>

  <!-- Fleet Topology View -->
  <div id="fleet-view" style="display:none;">
    <div class="fleet-summary-bar">
      <div class="stat-card">
        <div class="stat-label">Fleet Workers</div>
        <div class="stat-value" id="stat-fleet-workers">-</div>
        <div class="stat-subtext" id="stat-fleet-workers-sub">Ready for distributed load</div>
      </div>
      <div class="stat-card">
        <div class="stat-label">Total Fleet Capacity</div>
        <div class="stat-value" id="stat-fleet-capacity">-</div>
        <div class="stat-subtext">Concurrency worker slots</div>
      </div>
      <div class="stat-card">
        <div class="stat-label">Active Fleet Jobs</div>
        <div class="stat-value" id="stat-fleet-jobs">-</div>
        <div class="stat-subtext">Currently executing tests</div>
      </div>
      <div class="stat-card">
        <div class="stat-label">Fleet Health</div>
        <div class="stat-value" id="stat-fleet-health">-</div>
        <div class="stat-subtext">Heartbeat status & liveness</div>
      </div>
    </div>
    <div id="fleet-regions-container">
      <div class="empty-state">Loading fleet topology...</div>
    </div>
  </div>
</div>

<!-- Run Inspection Modal -->
<div class="modal-backdrop" id="run-modal" onclick="closeModal(event)">
  <div class="modal-content" onclick="event.stopPropagation()">
    <div class="modal-header">
      <div>
        <h3 id="modal-run-title" style="font-size: 1.15rem; font-weight:700;">Run Details</h3>
        <p id="modal-run-subtitle" style="font-size: 0.8rem; color: var(--text-muted);"></p>
      </div>
      <button class="modal-close" onclick="closeModal()">&times;</button>
    </div>
    <div class="modal-body" id="modal-body">
      Loading run details...
    </div>
  </div>
</div>

<!-- Dispatch Run Modal -->
<div class="modal-backdrop" id="dispatch-modal" onclick="closeDispatchModal(event)">
  <div class="modal-content" style="max-width:760px;" onclick="event.stopPropagation()">
    <div class="modal-header">
      <div>
        <h3 style="font-size: 1.15rem; font-weight:700;">⚡ Fleet Interactive Run Dispatch</h3>
        <p style="font-size: 0.8rem; color: var(--text-muted);">Trigger ad-hoc test specs or distributed load benchmarks across fleet workers</p>
      </div>
      <button class="modal-close" onclick="closeDispatchModal()">&times;</button>
    </div>
    <div class="modal-body" id="dispatch-modal-body">
      <div id="dispatch-form">
        <div style="display:grid; grid-template-columns: 1fr 1fr; gap:1rem; margin-bottom:1rem;">
          <div>
            <label style="font-size:0.8rem; font-weight:600; color:var(--text-secondary); display:block; margin-bottom:0.35rem;">Target Node</label>
            <select id="dispatch-target-node" style="width:100%;">
              <option value="">Auto-Match (Next Available Idle Worker)</option>
            </select>
          </div>
          <div>
            <label style="font-size:0.8rem; font-weight:600; color:var(--text-secondary); display:block; margin-bottom:0.35rem;">Target Region (Optional)</label>
            <input type="text" id="dispatch-target-region" placeholder="e.g. us-east, eu-central" style="width:100%;">
          </div>
        </div>

        <div style="display:grid; grid-template-columns: 1fr 1fr; gap:1rem; margin-bottom:1rem;">
          <div>
            <label style="font-size:0.8rem; font-weight:600; color:var(--text-secondary); display:block; margin-bottom:0.35rem;">Workload Type</label>
            <select id="dispatch-type" onchange="onDispatchTypeChange()" style="width:100%;">
              <option value="test">Functional API Test Spec</option>
              <option value="perf">Performance Load Benchmark</option>
            </select>
          </div>
          <div>
            <label style="font-size:0.8rem; font-weight:600; color:var(--text-secondary); display:block; margin-bottom:0.35rem;">Run Name / Reference</label>
            <input type="text" id="dispatch-name" placeholder="e.g. Health Endpoint Verification" style="width:100%;">
          </div>
        </div>

        <div id="dispatch-perf-opts" style="display:none; grid-template-columns: 1fr 1fr; gap:1rem; margin-bottom:1rem;">
          <div>
            <label style="font-size:0.8rem; font-weight:600; color:var(--text-secondary); display:block; margin-bottom:0.35rem;">Concurrency</label>
            <input type="number" id="dispatch-concurrency" value="10" min="1" max="1000" style="width:100%; background:var(--bg-card); color:var(--text-primary); border:1px solid var(--border-color); padding:0.5rem 0.75rem; border-radius:6px;">
          </div>
          <div>
            <label style="font-size:0.8rem; font-weight:600; color:var(--text-secondary); display:block; margin-bottom:0.35rem;">Total Requests</label>
            <input type="number" id="dispatch-requests" value="100" min="1" max="100000" style="width:100%; background:var(--bg-card); color:var(--text-primary); border:1px solid var(--border-color); padding:0.5rem 0.75rem; border-radius:6px;">
          </div>
        </div>

        <div style="margin-bottom:1rem;">
          <label style="font-size:0.8rem; font-weight:600; color:var(--text-secondary); display:block; margin-bottom:0.35rem;">Request Specification (YAML)</label>
          <textarea id="dispatch-spec" rows="7" style="width:100%; background:var(--bg-card); border:1px solid var(--border-color); color:var(--text-primary); padding:0.75rem; border-radius:6px; font-family:var(--font-mono); font-size:0.82rem; resize:vertical;"></textarea>
        </div>

        <button class="btn" style="width:100%; justify-content:center; background:#0284c7; color:#fff; border-color:#38bdf8; font-weight:700; padding:0.75rem;" onclick="submitDispatch()">🚀 Launch Run on Fleet</button>
      </div>

      <!-- Execution Console Stream -->
      <div id="dispatch-console" style="display:none;">
        <div style="display:flex; justify-content:space-between; align-items:center; margin-bottom:0.75rem; padding-bottom:0.5rem; border-bottom:1px solid var(--border-color);">
          <div>
            <span style="font-weight:700; font-size:0.95rem;" id="dispatch-job-title">Dispatching Job...</span>
            <span class="font-mono" style="font-size:0.75rem; color:var(--text-muted); margin-left:0.5rem;" id="dispatch-job-id"></span>
          </div>
          <div id="dispatch-job-state-pill"></div>
        </div>
        <div style="background:#050811; border:1px solid var(--border-color); border-radius:6px; padding:1rem; max-height:280px; overflow-y:auto; font-family:var(--font-mono); font-size:0.8rem;" id="dispatch-terminal">
          <div style="color:var(--text-muted);">Waiting for fleet worker assignment...</div>
        </div>
        <div style="margin-top:0.75rem; display:flex; justify-content:space-between; align-items:center;">
          <button class="btn" onclick="resetDispatchForm()">← Dispatch Another Run</button>
          <button class="btn" id="dispatch-inspect-btn" style="display:none; background:var(--bg-secondary); color:var(--accent);" onclick="inspectFromDispatch()">View In Runs Inspector &rarr;</button>
        </div>
      </div>
    </div>
  </div>
</div>

<script>
var currentTab = 'team';
var allRuns = [];
var allNodes = [];
var currentFleet = null;
var currentSummary = null;

function loadAllData() {
  Promise.all([
    fetch('/api/v1/summary').then(function(r){ return r.json(); }),
    fetch('/api/v1/runs?limit=100').then(function(r){ return r.json(); }),
    fetch('/api/v1/nodes').then(function(r){ return r.json(); }),
    fetch('/api/v1/fleet').then(function(r){ return r.json(); })
  ]).then(function(results) {
    currentSummary = results[0];
    allRuns = results[1].runs || [];
    allNodes = results[2].nodes || [];
    currentFleet = results[3] || null;

    renderSummary(currentSummary);
    populateOwnerDropdown(allNodes);
    renderCurrentTab();
  }).catch(function(err) {
    console.error("Failed to load hub data:", err);
  });
}

function renderSummary(s) {
  if (!s) return;
  document.getElementById('stat-total-runs').innerText = s.total_runs || 0;
  document.getElementById('stat-run-breakdown').innerText = (s.passed_runs || 0) + " passed · " + (s.failed_runs || 0) + " failed";
  
  var passRate = s.pass_rate ? (s.pass_rate.toFixed(1) + "%") : "100%";
  document.getElementById('stat-pass-rate').innerText = passRate;
  document.getElementById('stat-pass-rate').style.color = s.failed_runs > 0 ? "var(--warning)" : "var(--success)";

  document.getElementById('stat-active-nodes').innerText = s.active_nodes || (s.total_nodes || 0);
  document.getElementById('stat-nodes-sub').innerText = (s.total_nodes || 0) + " total recognized";

  document.getElementById('stat-p95-latency').innerText = s.p95_latency_ms ? (s.p95_latency_ms.toFixed(1) + " ms") : "-";
}

function populateOwnerDropdown(nodes) {
  var select = document.getElementById('owner-select');
  var existing = select.value;
  select.innerHTML = '<option value="">All Developers</option>';
  
  var owners = {};
  nodes.forEach(function(n) {
    if (n.type === 'developer' && n.owner) owners[n.owner] = true;
  });
  
  Object.keys(owners).forEach(function(owner) {
    var opt = document.createElement('option');
    opt.value = owner;
    opt.innerText = owner;
    select.appendChild(opt);
  });
  if (existing) select.value = existing;
}

function switchTab(tab) {
  currentTab = tab;
  document.getElementById('tab-team').className = (tab === 'team') ? 'tab-btn active' : 'tab-btn';
  document.getElementById('tab-my').className = (tab === 'my') ? 'tab-btn active' : 'tab-btn';
  document.getElementById('tab-nodes').className = (tab === 'nodes') ? 'tab-btn active' : 'tab-btn';
  document.getElementById('tab-fleet').className = (tab === 'fleet') ? 'tab-btn active' : 'tab-btn';

  document.getElementById('owner-select-group').style.display = (tab === 'my') ? 'flex' : 'none';
  document.getElementById('runs-view').style.display = (tab === 'team' || tab === 'my') ? 'block' : 'none';
  document.getElementById('nodes-view').style.display = (tab === 'nodes') ? 'block' : 'none';
  document.getElementById('fleet-view').style.display = (tab === 'fleet') ? 'block' : 'none';
  document.getElementById('filter-bar').style.display = (tab === 'team' || tab === 'my') ? 'flex' : 'none';

  renderCurrentTab();
}

function renderCurrentTab() {
  if (currentTab === 'nodes') {
    renderNodes(allNodes);
  } else if (currentTab === 'fleet') {
    renderFleetTopology(currentFleet);
  } else {
    applyFilters();
  }
}

function applyFilters() {
  var search = document.getElementById('search-input').value.toLowerCase();
  var status = document.getElementById('status-filter').value;
  var typeFilter = document.getElementById('type-filter').value;
  var ownerSelect = document.getElementById('owner-select').value;

  var filtered = allRuns.filter(function(r) {
    if (currentTab === 'my') {
      if (ownerSelect && (!r.node || r.node.owner !== ownerSelect)) return false;
      if (!ownerSelect && (!r.node || r.node.type !== 'developer')) return false;
    }

    if (status === 'passed' && r.summary && r.summary.failed > 0) return false;
    if (status === 'failed' && r.summary && r.summary.failed === 0) return false;

    if (typeFilter && (!r.node || r.node.type !== typeFilter)) return false;

    if (search) {
      var match = (r.run_id + " " + (r.node ? r.node.common_name : "") + " " + (r.node ? r.node.owner : "") + " " + (r.git ? r.git.branch : "") + " " + (r.git ? r.git.commit : "") + " " + (r.project_id || "")).toLowerCase();
      if (match.indexOf(search) === -1) return false;
    }

    return true;
  });

  renderRunsTable(filtered);
}

function renderRunsTable(runs) {
  var tbody = document.getElementById('runs-table-body');
  if (!runs || runs.length === 0) {
    tbody.innerHTML = '<tr><td colspan="9" class="empty-state">No matching test runs found.</td></tr>';
    return;
  }

  var html = '';
  for (var i = 0; i < runs.length; i++) {
    var r = runs[i];
    var passed = r.summary && r.summary.failed === 0;
    var badgeClass = passed ? 'badge-pass' : 'badge-fail';
    var badgeText = passed ? 'PASS' : 'FAIL';
    
    var nodeType = (r.node && r.node.type) ? r.node.type : 'developer';
    var typeBadge = (nodeType === 'ci-runner') 
      ? '<span class="badge badge-ci">⚙️ CI RUNNER</span>' 
      : '<span class="badge badge-dev">💻 DEV WORKSTATION</span>';

    var branch = (r.git && r.git.branch) ? ('🌿 ' + escapeHtml(r.git.branch)) : '-';
    var commit = (r.git && r.git.commit) ? (' <span class="font-mono" style="color:var(--text-muted)">(' + r.git.commit.substring(0, 7) + ')</span>') : '';
    var pr = (r.ci && r.ci.pr_number) ? (' <span class="font-mono" style="color:var(--accent)">' + escapeHtml(r.ci.pr_number) + '</span>') : '';

    var p95 = (r.percentiles && r.percentiles.p95) ? (r.percentiles.p95.toFixed(1) + ' ms') : '-';
    var duration = (r.summary && r.summary.duration_ms) ? ((r.summary.duration_ms / 1000).toFixed(2) + ' s') : '-';
    var endpointsRatio = (r.summary ? r.summary.passed : 0) + '/' + (r.summary ? r.summary.total : 0);

    var timeAgo = formatTimeAgo(r.timestamp);
    var nodeName = (r.node && r.node.common_name) ? r.node.common_name : 'unnamed';
    var owner = (r.node && r.node.owner) ? r.node.owner : ((r.git && r.git.author) ? r.git.author : '-');

    html += '<tr>' +
      '<td><span class="badge ' + badgeClass + '">' + badgeText + '</span></td>' +
      '<td><a class="run-link font-mono" onclick="inspectRun(\'' + r.run_id + '\')">' + escapeHtml(r.run_id) + '</a></td>' +
      '<td><div style="font-weight:600;">' + escapeHtml(nodeName) + '</div><div style="margin-top:0.2rem;">' + typeBadge + '</div></td>' +
      '<td><div style="font-weight:500;">' + escapeHtml(owner) + '</div></td>' +
      '<td>' + branch + commit + pr + '</td>' +
      '<td>' + endpointsRatio + ' ok</td>' +
      '<td class="font-mono">' + duration + '</td>' +
      '<td class="font-mono">' + p95 + '</td>' +
      '<td style="color:var(--text-muted); font-size:0.8rem;">' + timeAgo + '</td>' +
      '</tr>';
  }
  tbody.innerHTML = html;
}

function renderNodes(nodes) {
  var container = document.getElementById('nodes-grid');
  if (!nodes || nodes.length === 0) {
    container.innerHTML = '<div class="empty-state">No nodes registered yet. Run <code>hit run --publish</code> from a developer workstation or CI runner!</div>';
    return;
  }

  var html = '';
  for (var i = 0; i < nodes.length; i++) {
    var n = nodes[i];
    var isCI = n.type === 'ci-runner';
    var typeBadge = isCI ? '<span class="badge badge-ci">⚙️ CI RUNNER</span>' : '<span class="badge badge-dev">💻 DEVELOPER</span>';
    var successRate = n.total_runs > 0 ? ((n.passed_runs / n.total_runs) * 100).toFixed(0) + '%' : '100%';
    var timeAgo = formatTimeAgo(n.last_seen);

    html += '<div class="node-card">' +
      '<div>' +
        '<div class="node-header">' +
          '<div>' +
            '<div class="node-name">' + escapeHtml(n.common_name) + '</div>' +
            '<div class="node-owner">Owner: <strong>' + escapeHtml(n.owner) + '</strong></div>' +
          '</div>' +
          typeBadge +
        '</div>' +
        '<div style="font-size:0.75rem; color:var(--text-muted); margin-bottom:0.5rem;" class="font-mono">' +
          'Node ID: ' + escapeHtml(n.id) + ' · OS: ' + escapeHtml(n.os || 'unknown') +
        '</div>' +
        '<div class="node-stats">' +
          '<div class="node-stat-item"><div class="val">' + n.total_runs + '</div><div class="lbl">Total Runs</div></div>' +
          '<div class="node-stat-item"><div class="val" style="color:var(--success);">' + successRate + '</div><div class="lbl">Pass Rate</div></div>' +
        '</div>' +
      '</div>' +
      '<div style="display:flex; justify-content:space-between; align-items:center; margin-top:0.5rem; font-size:0.8rem; color:var(--text-muted);">' +
        '<span>Last active: ' + timeAgo + '</span>' +
        '<button class="btn" style="padding:0.3rem 0.6rem; font-size:0.75rem;" onclick="filterByNode(\'' + n.id + '\')">View Runs</button>' +
      '</div>' +
    '</div>';
  }
  container.innerHTML = html;
}

function renderFleetTopology(fleet) {
  var totalWorkersEl = document.getElementById('stat-fleet-workers');
  var totalCapEl = document.getElementById('stat-fleet-capacity');
  var activeJobsEl = document.getElementById('stat-fleet-jobs');
  var fleetHealthEl = document.getElementById('stat-fleet-health');
  var container = document.getElementById('fleet-regions-container');

  if (!fleet || !fleet.nodes || fleet.nodes.length === 0) {
    if (totalWorkersEl) totalWorkersEl.innerText = '0';
    if (totalCapEl) totalCapEl.innerText = '0 slots';
    if (activeJobsEl) activeJobsEl.innerText = '0';
    if (fleetHealthEl) fleetHealthEl.innerText = 'No Workers';
    if (container) {
      container.innerHTML = '<div class="empty-state">No workers enrolled in fleet yet.<br><br>Launch a performance worker with:<br><code style="background:var(--bg-card); padding:0.25rem 0.5rem; border-radius:4px; font-family:var(--font-mono); font-size:0.85rem;">hit perf worker --port 9090 --hub http://localhost:8080</code><br><br>Or launch a probe daemon with:<br><code style="background:var(--bg-card); padding:0.25rem 0.5rem; border-radius:4px; font-family:var(--font-mono); font-size:0.85rem;">hit probe daemon --hub http://localhost:8080</code></div>';
    }
    return;
  }

  var totalNodes = fleet.total_nodes || fleet.nodes.length;
  var activeWorkers = fleet.active_workers || 0;
  var totalCap = fleet.total_capacity || 0;
  var activeJobs = fleet.active_jobs || 0;

  var offlineCount = 0;
  var degradedCount = 0;
  fleet.nodes.forEach(function(n) {
    if (n.state === 'offline') offlineCount++;
    else if (n.state === 'degraded') degradedCount++;
  });

  if (totalWorkersEl) totalWorkersEl.innerText = activeWorkers + ' / ' + totalNodes;
  if (totalCapEl) totalCapEl.innerText = totalCap + ' slots';
  if (activeJobsEl) activeJobsEl.innerText = activeJobs + ' active';
  if (fleetHealthEl) {
    if (offlineCount > 0) {
      fleetHealthEl.innerHTML = '<span style="color:var(--danger); font-weight:700;">' + offlineCount + ' Offline</span>';
    } else if (degradedCount > 0) {
      fleetHealthEl.innerHTML = '<span style="color:var(--warning); font-weight:700;">' + degradedCount + ' Degraded</span>';
    } else {
      fleetHealthEl.innerHTML = '<span style="color:var(--success); font-weight:700;">100% Healthy</span>';
    }
  }

  var regionMap = {};
  if (fleet.nodes_by_region && Object.keys(fleet.nodes_by_region).length > 0) {
    regionMap = fleet.nodes_by_region;
  } else {
    fleet.nodes.forEach(function(n) {
      var r = n.region || 'default';
      if (!regionMap[r]) regionMap[r] = [];
      regionMap[r].push(n);
    });
  }

  var html = '';
  var regions = Object.keys(regionMap);
  for (var rIdx = 0; rIdx < regions.length; rIdx++) {
    var region = regions[rIdx];
    var nodes = regionMap[region] || [];

    html += '<div class="fleet-region-block">' +
      '<div class="fleet-region-header">' +
        '<div class="fleet-region-title">' +
          '<span>🌐 Region: <strong>' + escapeHtml(region) + '</strong></span>' +
        '</div>' +
        '<span style="font-size:0.85rem; color:var(--text-secondary);">' + nodes.length + ' node' + (nodes.length === 1 ? '' : 's') + '</span>' +
      '</div>' +
      '<div class="fleet-grid">';

    for (var nIdx = 0; nIdx < nodes.length; nIdx++) {
      var node = nodes[nIdx];
      var state = node.state || 'idle';
      var stateBadge = '<span class="state-pill state-idle">🟢 IDLE</span>';
      if (state === 'busy') {
        stateBadge = '<span class="state-pill state-busy">🔵 BUSY</span>';
      } else if (state === 'degraded') {
        stateBadge = '<span class="state-pill state-degraded">🟡 DEGRADED</span>';
      } else if (state === 'offline') {
        stateBadge = '<span class="state-pill state-offline">🔴 OFFLINE</span>';
      }

      var cap = node.capacity || 0;
      var jobs = node.active_jobs || 0;
      var capPct = (cap > 0) ? Math.min(100, Math.round((jobs / cap) * 100)) : 0;

      var capsHtml = '';
      if (node.capabilities && node.capabilities.length > 0) {
        capsHtml = '<div style="display:flex; flex-wrap:wrap; gap:0.35rem; margin-top:0.4rem;">';
        for (var cIdx = 0; cIdx < node.capabilities.length; cIdx++) {
          capsHtml += '<span class="badge badge-ci" style="font-size:0.68rem; padding:0.15rem 0.4rem;">' + escapeHtml(node.capabilities[cIdx]) + '</span>';
        }
        capsHtml += '</div>';
      }

      var metricsHtml = '';
      if (node.metrics) {
        var cpu = (node.metrics.cpu_percent !== undefined) ? (node.metrics.cpu_percent + '%') : null;
        var mem = (node.metrics.memory_mb !== undefined) ? (node.metrics.memory_mb + ' MB') : null;
        if (cpu || mem) {
          metricsHtml = '<div style="font-size:0.75rem; color:var(--text-muted); margin-top:0.35rem;">';
          if (cpu) metricsHtml += 'CPU: ' + cpu + ' ';
          if (mem) metricsHtml += 'RAM: ' + mem;
          metricsHtml += '</div>';
        }
      }

      var epHtml = '';
      if (node.endpoint_url) {
        epHtml = '<div class="font-mono" style="font-size:0.75rem; color:var(--accent); margin:0.3rem 0; word-break:break-all;">' +
          '⚡ ' + escapeHtml(node.endpoint_url) +
        '</div>';
      }

      var hbTime = formatTimeAgo(node.last_heartbeat || node.last_seen);

      html += '<div class="fleet-card">' +
        '<div>' +
          '<div style="display:flex; justify-content:space-between; align-items:flex-start; margin-bottom:0.4rem;">' +
            '<div>' +
              '<div style="font-weight:700; font-size:0.95rem; color:var(--text-primary);">' + escapeHtml(node.common_name || node.id) + '</div>' +
              '<div style="font-size:0.78rem; color:var(--text-secondary);">' + escapeHtml(node.owner || 'unassigned') + ' · ' + escapeHtml(node.type || 'worker') + '</div>' +
            '</div>' +
            stateBadge +
          '</div>' +
          epHtml +
          '<div style="margin:0.5rem 0;">' +
            '<div style="display:flex; justify-content:space-between; font-size:0.78rem; color:var(--text-secondary);">' +
              '<span>Worker Load</span>' +
              '<span class="font-mono">' + jobs + ' / ' + cap + ' slots (' + capPct + '%)</span>' +
            '</div>' +
            '<div class="cap-bar-bg">' +
              '<div class="cap-bar-fill" style="width:' + capPct + '%;"></div>' +
            '</div>' +
          '</div>' +
          capsHtml +
          metricsHtml +
        '</div>' +
        '<div style="border-top:1px solid var(--border-color); margin-top:0.75rem; padding-top:0.5rem; display:flex; justify-content:space-between; align-items:center; font-size:0.75rem; color:var(--text-muted);">' +
          '<span class="font-mono">ID: ' + escapeHtml(node.id.substring(0, 16)) + '</span>' +
          '<span>HB: ' + hbTime + '</span>' +
        '</div>' +
        '<div style="margin-top:0.5rem;">' +
          '<button class="btn" style="width:100%; justify-content:center; padding:0.35rem; font-size:0.75rem; background:#0284c7; color:#fff; border-color:#38bdf8; font-weight:600;" onclick="openDispatchModal(\'' + escapeHtml(node.id) + '\', \'' + escapeHtml(node.region||'') + '\')">⚡ Run on Node</button>' +
        '</div>' +
      '</div>';
    }

    html += '</div></div>';
  }

  container.innerHTML = html;
}

function filterByNode(nodeId) {
  switchTab('team');
  document.getElementById('search-input').value = nodeId;
  applyFilters();
}

function inspectRun(runId) {
  var modal = document.getElementById('run-modal');
  var title = document.getElementById('modal-run-title');
  var subtitle = document.getElementById('modal-run-subtitle');
  var body = document.getElementById('modal-body');

  modal.className = 'modal-backdrop show';
  title.innerText = 'Run: ' + runId;
  subtitle.innerText = 'Loading telemetry...';
  body.innerHTML = '<div class="empty-state">Loading run details...</div>';

  fetch('/api/v1/runs/' + encodeURIComponent(runId)).then(function(res) {
    if (!res.ok) throw new Error("Run not found");
    return res.json();
  }).then(function(r) {
    subtitle.innerText = (r.timestamp || '') + ' · Node: ' + ((r.node && r.node.common_name) ? r.node.common_name : 'unnamed');

    var epHtml = '';
    var eps = r.endpoints || [];
    for (var i = 0; i < eps.length; i++) {
      var ep = eps[i];
      var badge = ep.ok ? '<span class="badge badge-pass">PASS</span>' : '<span class="badge badge-fail">FAIL</span>';
      
      var testsHtml = '';
      var tests = ep.tests || [];
      for (var j = 0; j < tests.length; j++) {
        var t = tests[j];
        testsHtml += '<div style="font-size:0.8rem; padding:0.25rem 0.5rem; background:rgba(0,0,0,0.2); border-radius:4px; margin-top:0.25rem; display:flex; justify-content:space-between;">' +
          '<span>' + escapeHtml(t.name) + '</span>' +
          '<span style="color:' + (t.passed ? 'var(--success)' : 'var(--danger)') + '; font-weight:600;">' + (t.passed ? '✓' : '✗ ' + escapeHtml(t.detail||'')) + '</span>' +
        '</div>';
      }

      epHtml += '<div style="background:var(--bg-card); border:1px solid var(--border-color); border-radius:6px; padding:0.75rem 1rem; margin-bottom:0.75rem;">' +
        '<div style="display:flex; justify-content:space-between; align-items:center;">' +
          '<div>' +
            '<span class="badge badge-dev" style="margin-right:0.5rem;">' + escapeHtml(ep.method) + '</span>' +
            '<span class="font-mono" style="font-weight:600;">' + escapeHtml(ep.url) + '</span>' +
          '</div>' +
          '<div style="display:flex; align-items:center; gap:0.75rem;">' +
            '<span class="font-mono" style="color:var(--text-muted);">' + ep.elapsed_ms.toFixed(1) + ' ms</span>' +
            '<span class="badge ' + (ep.status >= 400 ? 'badge-fail' : 'badge-pass') + '">' + ep.status + '</span>' +
            badge +
          '</div>' +
        '</div>' +
        (ep.error ? ('<div style="color:var(--danger); font-size:0.8rem; margin-top:0.5rem;">Error: ' + escapeHtml(ep.error) + '</div>') : '') +
        (testsHtml ? ('<div style="margin-top:0.5rem;">' + testsHtml + '</div>') : '') +
      '</div>';
    }

    var p50 = (r.percentiles && r.percentiles.p50) ? (r.percentiles.p50.toFixed(1) + ' ms') : '-';
    var p90 = (r.percentiles && r.percentiles.p90) ? (r.percentiles.p90.toFixed(1) + ' ms') : '-';
    var p95 = (r.percentiles && r.percentiles.p95) ? (r.percentiles.p95.toFixed(1) + ' ms') : '-';
    var p99 = (r.percentiles && r.percentiles.p99) ? (r.percentiles.p99.toFixed(1) + ' ms') : '-';

    body.innerHTML = '<div style="display:grid; grid-template-columns: repeat(4, 1fr); gap:0.75rem; margin-bottom:1.25rem;">' +
      '<div class="stat-card" style="padding:0.75rem;"><div class="stat-label">P50 Latency</div><div class="stat-value" style="font-size:1.25rem;">' + p50 + '</div></div>' +
      '<div class="stat-card" style="padding:0.75rem;"><div class="stat-label">P90 Latency</div><div class="stat-value" style="font-size:1.25rem;">' + p90 + '</div></div>' +
      '<div class="stat-card" style="padding:0.75rem;"><div class="stat-label">P95 Latency</div><div class="stat-value" style="font-size:1.25rem;">' + p95 + '</div></div>' +
      '<div class="stat-card" style="padding:0.75rem;"><div class="stat-label">P99 Latency</div><div class="stat-value" style="font-size:1.25rem;">' + p99 + '</div></div>' +
    '</div>' +
    '<h4 style="margin-bottom:0.75rem; font-size:0.9rem; text-transform:uppercase; color:var(--text-muted); letter-spacing:0.5px;">Endpoint Breakdowns</h4>' +
    (epHtml || '<div class="empty-state">No individual endpoint logs captured.</div>');
  }).catch(function(e) {
    body.innerHTML = '<div class="empty-state">Error fetching run details: ' + escapeHtml(e.message) + '</div>';
  });
}

function closeModal(e) {
  if (e && e.target !== e.currentTarget && !e.target.classList.contains('modal-close')) return;
  document.getElementById('run-modal').className = 'modal-backdrop';
}

document.addEventListener('keydown', function(e) {
  if (e.key === 'Escape') {
    closeModal();
    closeDispatchModal();
  }
});

var currentDispatchJobId = null;
var currentDispatchTelemetryId = null;
var dispatchPollTimer = null;

function openDispatchModal(targetNodeId, targetRegion) {
  var modal = document.getElementById('dispatch-modal');
  var nodeSelect = document.getElementById('dispatch-target-node');
  var regionInput = document.getElementById('dispatch-target-region');
  var specArea = document.getElementById('dispatch-spec');

  var opts = '<option value="">Auto-Match (Next Available Idle Worker)</option>';
  if (allNodes && allNodes.length > 0) {
    for (var i = 0; i < allNodes.length; i++) {
      var n = allNodes[i];
      var label = (n.common_name || n.id) + ' [' + (n.region || 'default') + ' · ' + (n.state || 'idle') + ']';
      opts += '<option value="' + escapeHtml(n.id) + '">' + escapeHtml(label) + '</option>';
    }
  }
  nodeSelect.innerHTML = opts;

  if (targetNodeId) {
    nodeSelect.value = targetNodeId;
  }
  if (targetRegion) {
    regionInput.value = targetRegion;
  } else {
    regionInput.value = '';
  }

  if (!specArea.value || specArea.value.trim() === '') {
    specArea.value = 'name: "Health Endpoint Check"\nmethod: "GET"\nurl: "http://127.0.0.1:8765/health"\ntests:\n  - status == 200\n  - response_time < 500\n';
  }

  resetDispatchForm();
  modal.className = 'modal-backdrop show';
}

function closeDispatchModal(e) {
  if (e && e.target !== e.currentTarget && !e.target.classList.contains('modal-close')) return;
  if (dispatchPollTimer) {
    clearInterval(dispatchPollTimer);
    dispatchPollTimer = null;
  }
  document.getElementById('dispatch-modal').className = 'modal-backdrop';
}

function onDispatchTypeChange() {
  var type = document.getElementById('dispatch-type').value;
  var perfOpts = document.getElementById('dispatch-perf-opts');
  if (type === 'perf') {
    perfOpts.style.display = 'grid';
  } else {
    perfOpts.style.display = 'none';
  }
}

function resetDispatchForm() {
  if (dispatchPollTimer) {
    clearInterval(dispatchPollTimer);
    dispatchPollTimer = null;
  }
  document.getElementById('dispatch-form').style.display = 'block';
  document.getElementById('dispatch-console').style.display = 'none';
  document.getElementById('dispatch-inspect-btn').style.display = 'none';
  document.getElementById('dispatch-terminal').innerHTML = '<div style="color:var(--text-muted);">Waiting for fleet worker assignment...</div>';
  document.getElementById('dispatch-job-state-pill').innerHTML = '';
  document.getElementById('dispatch-job-title').innerText = 'Dispatching Job...';
  document.getElementById('dispatch-job-id').innerText = '';
}

function submitDispatch() {
  var node = document.getElementById('dispatch-target-node').value;
  var region = document.getElementById('dispatch-target-region').value;
  var type = document.getElementById('dispatch-type').value;
  var name = document.getElementById('dispatch-name').value;
  var spec = document.getElementById('dispatch-spec').value;
  var conc = parseInt(document.getElementById('dispatch-concurrency').value, 10) || 10;
  var reqs = parseInt(document.getElementById('dispatch-requests').value, 10) || 100;

  var payload = {
    type: type,
    name: name,
    target_node_id: node,
    target_region: region,
    spec_yaml: spec,
    concurrency: conc,
    requests: reqs
  };

  document.getElementById('dispatch-form').style.display = 'none';
  document.getElementById('dispatch-console').style.display = 'block';
  document.getElementById('dispatch-job-title').innerText = name || 'Fleet Run';
  document.getElementById('dispatch-job-state-pill').innerHTML = '<span class="state-pill state-busy">QUEUED</span>';

  fetch('/api/v1/dispatch', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload)
  }).then(function(res) {
    if (!res.ok) throw new Error('Failed to submit job: HTTP ' + res.status);
    return res.json();
  }).then(function(job) {
    currentDispatchJobId = job.id;
    document.getElementById('dispatch-job-id').innerText = 'ID: ' + job.id;
    startDispatchPolling(job.id);
  }).catch(function(err) {
    document.getElementById('dispatch-terminal').innerHTML = '<div style="color:var(--danger);">Error dispatching job: ' + escapeHtml(err.message) + '</div>';
  });
}

function startDispatchPolling(jobId) {
  if (dispatchPollTimer) clearInterval(dispatchPollTimer);
  pollDispatchJob(jobId);
  dispatchPollTimer = setInterval(function() {
    pollDispatchJob(jobId);
  }, 600);
}

function pollDispatchJob(jobId) {
  fetch('/api/v1/dispatch/' + encodeURIComponent(jobId))
    .then(function(res) {
      if (!res.ok) throw new Error('Job not found');
      return res.json();
    })
    .then(function(job) {
      updateDispatchConsole(job);
      if (job.state === 'completed' || job.state === 'failed' || job.state === 'canceled') {
        if (dispatchPollTimer) {
          clearInterval(dispatchPollTimer);
          dispatchPollTimer = null;
        }
        loadAllData();
      }
    })
    .catch(function(err) {
      console.error('Dispatch poll error:', err);
    });
}

function updateDispatchConsole(job) {
  var term = document.getElementById('dispatch-terminal');
  var pill = document.getElementById('dispatch-job-state-pill');
  var inspectBtn = document.getElementById('dispatch-inspect-btn');

  var state = job.state || 'queued';
  if (state === 'running') {
    pill.innerHTML = '<span class="state-pill state-busy">⚡ RUNNING</span>';
  } else if (state === 'completed') {
    pill.innerHTML = '<span class="state-pill state-idle">✓ COMPLETED</span>';
  } else if (state === 'failed') {
    pill.innerHTML = '<span class="state-pill state-offline">✗ FAILED</span>';
  } else if (state === 'canceled') {
    pill.innerHTML = '<span class="state-pill state-degraded">CANCELED</span>';
  } else {
    pill.innerHTML = '<span class="state-pill state-degraded">QUEUED</span>';
  }

  var linesHtml = '';
  if (job.logs && job.logs.length > 0) {
    for (var i = 0; i < job.logs.length; i++) {
      var ev = job.logs[i];
      var timeStr = ev.timestamp ? new Date(ev.timestamp).toLocaleTimeString() : '';
      var tag = '<span style="color:var(--text-muted); font-size:0.75rem;">[' + timeStr + ']</span> ';
      if (ev.level === 'pass') {
        tag += '<span style="color:var(--success); font-weight:700;">✓ PASS</span> ';
      } else if (ev.level === 'fail') {
        tag += '<span style="color:var(--danger); font-weight:700;">✗ FAIL</span> ';
      } else if (ev.level === 'error') {
        tag += '<span style="color:var(--danger); font-weight:700;">! ERR </span> ';
      } else {
        tag += '<span style="color:var(--accent); font-weight:600;">ℹ INFO</span> ';
      }
      linesHtml += '<div style="margin-bottom:0.25rem;">' + tag + escapeHtml(ev.message) + '</div>';
    }
  } else {
    linesHtml = '<div style="color:var(--text-muted);">Waiting for output from node ' + escapeHtml(job.assigned_node || 'matching node') + '...</div>';
  }

  term.innerHTML = linesHtml;
  term.scrollTop = term.scrollHeight;

  if (job.telemetry_run_id) {
    currentDispatchTelemetryId = job.telemetry_run_id;
    inspectBtn.style.display = 'inline-flex';
  }
}

function inspectFromDispatch() {
  if (currentDispatchTelemetryId) {
    closeDispatchModal();
    inspectRun(currentDispatchTelemetryId);
  }
}

function formatTimeAgo(dateStr) {
  if (!dateStr) return '-';
  var d = new Date(dateStr);
  var now = new Date();
  var diffSec = Math.floor((now - d) / 1000);
  if (diffSec < 60) return diffSec + 's ago';
  if (diffSec < 3600) return Math.floor(diffSec / 60) + 'm ago';
  if (diffSec < 86400) return Math.floor(diffSec / 3600) + 'h ago';
  return Math.floor(diffSec / 86400) + 'd ago';
}

function escapeHtml(str) {
  if (!str) return '';
  return String(str)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#039;');
}

loadAllData();
setInterval(loadAllData, 10000);
</script>
</body>
</html>`
}
