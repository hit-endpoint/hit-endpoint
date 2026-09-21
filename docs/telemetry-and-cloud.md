# Cloud Telemetry Hub & Metrics Publisher (`hit run --publish`)

Local terminal reports and `.hit/history.jsonl` files are great for individual developers debugging locally. But as engineering teams scale, API tests run across hundreds of pull requests and distributed CI runners where no single developer has a unified view of regression history, latency shifts, or flaky assertion trends.

`hit` includes a built-in telemetry publisher that aggregates test outcomes, Git provenance, CI pipeline context, and latency percentiles into a centralized telemetry collector or SaaS dashboard.

---

## Quickstart

```bash
# 1. Publish run results using default endpoint or HIT_TELEMETRY_ENDPOINT
hit run collections/ --publish

# 2. Publish to a custom internal metrics aggregator or dashboard
hit run collections/ --publish=https://telemetry.internal.company.com/v1/runs

# 3. Enable publishing automatically via environment variable in CI
export HIT_PUBLISH=true
export HIT_API_KEY="sec_live_xyz123"
export HIT_PROJECT_ID="payments-api"
hit run collections/
```

When publishing completes, `hit` outputs the direct dashboard URL:
```text
✓ Telemetry published to Hit Cloud: https://app.hitendpoint.com/runs/run_1726701234_a1b2c3d4
```

---

## Key Capabilities

### 1. Client-Side Secret Masking
Sensitive information is redacted **client-side** before any telemetry payload leaves your machine or CI runner:
* All secrets defined in `.secrets.yaml` and runtime variables marked as sensitive are replaced with `[MASKED]`.
* Sensitive HTTP headers (`Authorization`, `X-Api-Key`, `Cookie`, `Set-Cookie`, `Proxy-Authorization`) and bearer tokens are automatically scrubbed.
* URLs containing credential query parameters or inline basic auth credentials are sanitized.

### 2. Automatic Git & CI Metadata Detection
`hit` inspects standard environment variables and local git worktrees to attach rich context without requiring manual flags:

* **Git Context**: Commit SHA (shortened), branch name, committer email, and uncommitted dirty state (`dirty: true`).
* **CI Context**: Automatically detects providers including **GitHub Actions**, **GitLab CI**, **CircleCI**, and **Jenkins**, extracting workflow names, run IDs, and pull request references.

### 3. Percentile Latencies
Every test run calculates precise statistical quantiles across all evaluated requests:
* **p50**: Median response time
* **p90**: 90th percentile
* **p95**: 95th percentile
* **p99**: Tail latency ceiling

### 4. Node Identity & Attribution
`hit` automatically attributes every run to a specific machine and owner:
* **Common Name**: Human-readable name (e.g. `alices-macbook.local` or `github-actions-job-98412`).
* **Owner**: The responsible developer or team (e.g. `alice@company.com` or `payments-team`).
* **Node Type**: Categorized automatically as `developer` (local workstation) or `ci-runner` (automated pipeline).
* **Overrides**: Can be customized via environment variables (`HIT_NODE_NAME`, `HIT_NODE_OWNER`, `HIT_NODE_TYPE`).

---

## Configuration

Telemetry can be configured through CLI flags, environment variables, or directly inside `zone.yaml`:

### Environment Variables

| Variable | Description |
|---|---|
| `HIT_PUBLISH` | Set to `true` to enable telemetry publishing for all `hit run` executions. |
| `HIT_TELEMETRY_ENDPOINT` | Custom ingestion endpoint URL. Defaults to `https://api.hitendpoint.com`. |
| `HIT_API_KEY` | Bearer token / Personal Access Token / Project Token passed in `Authorization: Bearer <token>`. |
| `HIT_PROJECT_ID` | Project or repository identifier to group test runs within the dashboard. |
| `HIT_NODE_NAME` | Explicit common name override for the reporting node. |
| `HIT_NODE_OWNER` | Explicit developer or team owner attribution for the reporting node. |
| `HIT_NODE_TYPE` | Explicit node category override (`developer` or `ci-runner`). |

### Zone Configuration (`zone.yaml`)

```yaml
# zone.yaml
name: payments-service

servers:
  staging: https://staging.api.example.com
  production: https://api.example.com

cloud:
  endpoint: https://telemetry.internal.company.com/v1/runs
  project_id: payments-microservice
```

---

## Telemetry Ingestion Payload Schema

For organizations hosting their own internal observability or metric collectors, `hit` dispatches an `application/json` `POST` request adhering to the following schema:

```json
{
  "version": "1.0",
  "run_id": "run_1726701234_a1b2c3d4",
  "project_id": "payments-api",
  "timestamp": "2026-09-18T22:30:00Z",
  "zone": "payments-service",
  "server": "staging",
  "git": {
    "commit": "8f3a9b1c",
    "branch": "feature/checkout-v2",
    "author": "engineer@company.com",
    "dirty": false
  },
  "ci": {
    "provider": "github-actions",
    "workflow": "Continuous Integration",
    "run_id": "9876543210",
    "pr_number": "refs/pull/42/merge"
  },
  "node": {
    "id": "node_7a8b9c",
    "common_name": "gha-runner-9876",
    "owner": "payments-team",
    "type": "ci-runner",
    "hostname": "runner-vm-44",
    "os": "linux"
  },
  "summary": {
    "total": 24,
    "passed": 24,
    "failed": 0,
    "duration_ms": 1420.5
  },
  "percentiles": {
    "p50": 42.1,
    "p90": 88.4,
    "p95": 115.0,
    "p99": 210.3
  },
  "endpoints": [
    {
      "name": "Process Payment",
      "ref": "collections/payments/charge.yaml",
      "method": "POST",
      "url": "https://staging.api.example.com/v1/charges",
      "status": 201,
      "elapsed_ms": 115.0,
      "ok": true,
      "tests": [
        {
          "name": "status: 201",
          "passed": true
        },
        {
          "name": "latency: < 500ms",
          "passed": true
        }
      ]
    }
  ]
}
```

---

## CI Pipeline Integration

### GitHub Actions

```yaml
name: API Regression Suite
on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.22'

      - name: Install hit
        run: go install github.com/your-org/hit-endpoint/cmd/hit@latest

      - name: Run Tests & Publish Telemetry
        env:
          HIT_PUBLISH: "true"
          HIT_API_KEY: ${{ secrets.HIT_CLOUD_API_KEY }}
          HIT_PROJECT_ID: "payments-core"
        run: |
          hit run collections/ -s staging --junit results.xml
```

---

## Self-Hosting the Fleet Hub & Dashboard (`hit hub`)

You can host your own telemetry aggregation and monitoring dashboard with pure Go, zero database dependencies, and an air-gapped web interface:

```bash
# 1. Start the Fleet Hub server (default port: 8080, storage: ~/.hit/hub)
hit hub --port 8080 --dir /var/lib/hit-hub

# 2. Require an API Key / Token for ingestion
hit hub --port 8080 --api-key "hit_sec_team123"

# 3. Publish runs from developer laptops or CI runners to your hub
hit run collections/ --publish=http://127.0.0.1:8080/api/v1/runs

# 4. Open the interactive dashboard in your browser
open http://127.0.0.1:8080/
```

### Dashboard Views & Interaction

#### 🖥️ How to View and Interact with the Web Dashboard

1. **Launch the Fleet Hub**:
   ```bash
   hit hub --port 8080 --dir ~/.hit/hub
   ```
2. **Open in Your Browser**:
   Navigate to: **[http://localhost:8080/](http://localhost:8080/)**
3. **Explore the Dashboard Views**:
   * **Team Runs (Aggregated)**: View all runs across the engineering team and CI pipelines with branch filters, pass rate, and p50–p99 latency distributions.
   * **My Runs (Developer Workstation)**: Filter down to your personal workstation's test activity without team noise.
   * **Nodes & Runners**: Live inventory of developer laptops and CI runners, total runs executed, and health status.
   * **Fleet Topology (Live)**: Regional visual swimlanes displaying workers, health pills, concurrency capacity progress bars, and heartbeat timestamps.
   * **Interactive Dispatch**: Click **"⚡ Run on Node"** on any card or **"⚡ Dispatch Run"** in the header to configure a spec, launch execution, and watch live results stream in the embedded console!
   * **Run Inspector**: Click any run ID to drill down into JMESPath assertions, request/response headers, and tail latency quantiles.

#### Quick CLI Commands for Hub & Fleet
```bash
# 1. Start the Hub
hit hub --port 8080

# 2. Publish a local run to the Hub
hit run collections/ -s staging --publish=http://127.0.0.1:8080/api/v1/runs

# 3. Start a load worker auto-enrolled in the Hub
hit perf worker --port 9090 --region us-east --hub http://127.0.0.1:8080 --capacity 16

# 4. Dispatch a test to a remote node with real-time streaming
hit dispatch collections/petstore/00-health.yaml --hub http://127.0.0.1:8080 --region us-east
```

---

## Interactive Runner & Node Fleet Topology Management

Beyond post-run telemetry reporting, `hit hub` serves as an active control plane for distributed load testing worker nodes (`hit perf worker`) and synthetic monitoring daemons (`hit probe daemon`).

### 1. Node Registration Handshake & Heartbeats
When workers or probe daemons launch with `--hub <URL>`, they automatically enroll into the hub's active fleet:
1. **Registration Handshake**: Node POSTs to `/api/v1/nodes/register` broadcasting its node ID, owner, region, advertise URL, capacity, and capabilities (`perf-worker`, `probe-runner`).
2. **Periodic Heartbeats**: Every 15 seconds, the node issues a lightweight POST to `/api/v1/nodes/{id}/heartbeat` reporting current execution state (`idle` vs `busy`), active job count, and runtime metrics (RPS, CPU, memory).
3. **Graceful Deregistration**: On shutdown (`Ctrl+C` or `SIGTERM`), the node POSTs to `/api/v1/nodes/{id}/deregister`, immediately marking itself offline so coordinators stop routing traffic to it.

```bash
# Enroll a load test worker in the US-East fleet
hit perf worker --port 9090 --region us-east --hub http://hub.internal:8080 --capacity 16

# Enroll a synthetic monitoring probe daemon
hit probe daemon probes/api-health.yaml --region eu-west --hub http://hub.internal:8080
```

### 2. Automatic Liveness State Machine
The hub runs an automated background liveness supervisor:
* **🟢 IDLE / HEALTHY**: Node heartbeating regularly within 15s; available to accept distributed load.
* **🔵 BUSY**: Node is actively generating load or evaluating test assertions.
* **🟡 DEGRADED**: Heartbeat has not been received within 30 seconds.
* **🔴 OFFLINE**: Heartbeat has not been received within 60 seconds. Offline workers are automatically excluded from coordinator scheduling.

### 3. Coordinator Auto-Discovery (`hit perf --hub`)
Instead of manually maintaining and passing lists of worker IPs (`--workers http://10.0.1.1:8989,http://10.0.1.2:8989`), coordinators dynamically query the Fleet Hub:

```bash
# Auto-discover all available idle workers from the Hub
hit perf petstore/03-list-pets -c 100 -n 10000 --hub http://hub.internal:8080

# Auto-discover workers constrained to a specific geographic region
hit perf petstore/03-list-pets -c 100 -n 10000 --hub http://hub.internal:8080 --region eu-west
```

Coordinators query `GET /api/v1/fleet/workers?capability=perf-worker`, filter out degraded or offline nodes, and partition concurrency evenly across all healthy workers.

---

## Live Interactive Dispatch & Fleet Job Orchestration

`hit` enables bidirectional remote run execution: trigger functional API tests or distributed performance benchmarks on specific nodes or regional clusters directly from the **Web Dashboard** or the **CLI (`hit dispatch`)**, with live execution streaming and automatic telemetry publication.

```
 ┌────────────────────────────────────────────────────────┐
 │              HIT CLOUD FLEET HUB (:8080)               │
 │  • POST /api/v1/dispatch       • GET /api/v1/dispatch  │
 │  • GET  /api/v1/dispatch/{id}  • POST .../cancel       │
 └───────────────────────────▲────────────────────────────┘
                             │
            ┌────────────────┴────────────────┐
            │ (Poll queue / stream logs)      │ (CLI dispatch & stream)
┌───────────┴────────────────┐   ┌────────────┴────────────────┐
│    ENROLLED FLEET WORKER   │   │       ENGINEER / CI         │
│  hit perf worker --hub ... │   │  hit dispatch <spec|ref>    │
│  • Polls queue (every 2s)  │   │  --hub http://hub:8080      │
│  • Executes test / perf    │   │  --node worker-east-1       │
│  • Streams events to Hub   │   │  --stream                   │
│  • Auto-ingests run report │   │  • Real-time console stream │
└────────────────────────────┘   └─────────────────────────────┘
```

### 1. Interactive Web UI Dispatch ("⚡ Run on Node")
* **Regional Node Trigger**: In the **Fleet Topology** tab, every active node card features a **"⚡ Run on Node"** button, instantly pre-selecting that worker for targeted execution.
* **Top-Level Header Action**: Click **"⚡ Dispatch Run"** from any view to launch runs with automatic idle worker matchmaking.
* **Workload Configurations**:
  * **Functional API Test Spec**: Executes request methods, URL targets, custom headers, and multi-clause JMESPath assertions (`status == 200`, `response_time < 500ms`, `body.status == 'active'`).
  * **Performance Load Benchmark**: Configures concurrency, total requests, or duration directly from the modal.
* **Real-Time Streaming Terminal**: Live dark-mode console inside the modal displays log lines, step progress, and assertion outcomes (`✓ PASS`, `✗ FAIL`) as the remote node executes.
* **One-Click Telemetry Inspector**: Completed runs instantly provide a link to drill into full assertion and percentile details in the Run Inspector.

### 2. CLI Dispatch Command (`hit dispatch`)

Engineers and CI/CD pipelines can trigger remote runs across the fleet without leaving the terminal:

```bash
# 1. Dispatch a test collection to a specific node with real-time streaming
hit dispatch collections/petstore/00-health.yaml --hub http://hub.internal:8080 --node worker-east-1

# 2. Dispatch to any idle worker in a specific geographic region
hit dispatch collections/petstore/00-health.yaml --hub http://hub.internal:8080 --region eu-central

# 3. Dispatch an ad-hoc URL target
hit dispatch https://api.acme.com/v1/health --hub http://hub.internal:8080

# 4. Trigger a remote distributed load benchmark
hit dispatch collections/checkout.yaml --hub http://hub.internal:8080 --perf -c 25 -n 1000

# 5. Non-blocking fire-and-forget submission (returns JSON payload with Job ID)
hit dispatch collections/smoke.yaml --hub http://hub.internal:8080 --no-stream --json
```

**Exit Codes**:
* `0`: Remote job completed and all assertions passed.
* `1`: Remote job completed with assertion or transport failures.
* `2`: Usage error, invalid spec, or connection timeout.

### 3. Queue Polling & Firewall/NAT Resilience
Workers connect outbound to the Hub via `GET /api/v1/nodes/{id}/queue`, ensuring jobs can be dispatched into private VPCs, Kubernetes clusters, and local developer workstations without opening inbound firewall ports. When a job is claimed, the worker streams events to `/api/v1/nodes/{id}/jobs/{job_id}/events` and finalizes results to `/api/v1/nodes/{id}/jobs/{job_id}/complete`.

---

### Hub REST API Reference

| Endpoint | Method | Description |
|---|---|---|
| `/api/v1/runs` | `POST` | Ingests `RunTelemetryPayload` and registers/updates reporting node. |
| `/api/v1/runs` | `GET` | List runs with filters (`?owner=...`, `?node=...`, `?type=...`, `?status=...`). |
| `/api/v1/runs/{id}` | `GET` | Returns full execution detail for a single run ID. |
| `/api/v1/nodes` | `GET` | Returns all discovered developer and CI nodes with health and owner stats. |
| `/api/v1/nodes/register` | `POST` | Enrolls a worker or daemon into the active fleet. |
| `/api/v1/nodes/{id}/heartbeat` | `POST` | Updates worker health state, active jobs, and performance metrics. |
| `/api/v1/nodes/{id}/deregister` | `POST` | Gracefully marks a node as offline upon shutdown. |
| `/api/v1/nodes/{id}/queue` | `GET` | Worker queue polling endpoint to claim next pending assigned job. |
| `/api/v1/nodes/{id}/jobs/{jid}/events` | `POST` | Worker posts real-time execution log events and assertion checks. |
| `/api/v1/nodes/{id}/jobs/{jid}/complete` | `POST` | Worker posts job completion status, report, and telemetry run payload. |
| `/api/v1/dispatch` | `POST` | Submits a new run dispatch job to a target node, region, or next idle worker. |
| `/api/v1/dispatch` | `GET` | Lists active and recent dispatch jobs (`?limit=50`). |
| `/api/v1/dispatch/{id}` | `GET` | Returns dispatch job state, duration, logs, and execution report. |
| `/api/v1/dispatch/{id}/cancel` | `POST` | Cancels a queued or running dispatch job. |
| `/api/v1/fleet` | `GET` | Returns complete fleet topology grouped by region, role, and state. |
| `/api/v1/fleet/workers` | `GET` | Returns ready-to-use worker URLs for coordinator auto-discovery (`?capability=...&region=...`). |
| `/api/v1/summary` | `GET` | Roll-up metrics (total runs, pass rate, active nodes, fleet P95). |
| `/health` | `GET` | Health check endpoint (`{"status":"healthy"}`). |
| `/` | `GET` | Embedded dark-mode single-page web dashboard. |

### Docker Deployment

```bash
docker run -d \
  --name hit-hub \
  --restart always \
  -p 8080:8080 \
  -v hit-hub-data:/root/.hit/hub \
  ghcr.io/markjordan/hit-endpoint:latest \
  hub --port 8080 --api-key "${HIT_HUB_API_KEY}"
```


