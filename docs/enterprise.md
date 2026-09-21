# 🛡️ Enterprise CI/CD, Governance & Monitoring

`hit` is designed from the ground up for automated enterprise pipelines, security compliance gates, and production-grade observability across distributed engineering teams.

This guide provides an architectural overview of all five core enterprise pillars in `hit`, with quick links to deep-dive documentation.

---

## 1. CI/CD Standards & Incident Webhooks

Export standardized, machine-readable test and security results for native integration with GitHub Actions, GitLab CI, CircleCI, Jenkins, and alerting systems:

```bash
# Export test suite results to standard JUnit XML and dispatch incident webhook on failure
hit run collections/ --junit test-results.xml --webhook-on-failure https://alerts.corp.internal/v1/fail

# Export fuzzing vulnerabilities directly to SARIF 2.1.0 for GitHub Code Scanning alerts
hit fuzz collections/auth/login.yaml -n 100 --sarif security-findings.sarif
```

* **JUnit XML 2.0**: Native test summary tabs in GitHub Actions, GitLab CI, and Jenkins with step-level assertion failures.
* **SARIF 2.1.0 (Static Analysis Results Interchange Format)**: Integrates directly with GitHub Code Scanning to surface security warnings in pull requests without third-party plugins.
* **Incident Webhooks**: Real-time payloads delivered to internal alert aggregators when test assertions or server availability fail.

👉 *Deep dive*: [History, Diffing & Comprehensive Reports](history-diff-reports.md)

---

## 2. Organization-Wide Governance & Policy Gates

Enforce contract mandates, SLA ceilings, and secret hygiene in pre-commit hooks and PR workflows:

```bash
# Lint API definitions against .hit/policy.yaml standards
hit policy --strict --junit policy-report.xml

# Enforce latency SLAs and contract policies at test runtime
hit run collections/ --enforce-policy
```

* **Pre-Commit / PR Linting**: Validates that all endpoints define HTTP status assertions, latency SLAs, and schema contracts.
* **Zero-Secret Leakage**: Prohibits hardcoded API keys, passwords, and tokens in YAML files, enforcing `$env:VAR` and `.secrets.yaml`.
* **Runtime Policy Gates**: With `--enforce-policy`, test executions instantly fail if actual response latency exceeds the declared SLA ceiling.

👉 *Deep dive*: [Enterprise Governance & Policy Gates](governance-and-policies.md)

---

## 3. Cloud Fleet Hub, Regional Topology & Remote Run Dispatch

Centralize test telemetry, monitor live regional worker fleets, and dispatch remote tests with real-time streaming:

* **Telemetry Hub & Node Identity Attribution**:
  - Automatically attributes runs to **Developer Workstations** (`owner: developer@acme.com`) or **CI/CD Runners** (GitHub Actions, GitLab CI, CircleCI, Jenkins).
  - Client-side secret redaction masks all tokens (`[MASKED]`) before network transmission.
  - Computes exact p50, p90, p95, and p99 percentiles across all assertions and endpoints.
  - Air-gapped, dark-mode single-page web dashboard with zero external CDN dependencies.
* **Active Fleet Control Plane & Regional Topology**:
  - Load test workers (`hit perf worker`) and synthetic monitoring daemons (`hit probe daemon`) auto-enroll into the fleet via `--hub <URL>`.
  - 15-second heartbeats and an automated 4-state liveness machine (🟢 IDLE $\to$ 🔵 BUSY $\to$ 🟡 DEGRADED $\to$ 🔴 OFFLINE).
  - Dynamic worker auto-discovery (`hit perf --hub`): coordinators automatically query and partition load across active workers without manual IP lists.
  - Interactive visual **Fleet Topology** tab organized into regional swimlanes (`us-east`, `eu-central`, `local`).
* **Live Interactive Dispatch & Fleet Orchestration**:
  - **Interactive Web UI Dispatch**: Click **"⚡ Run on Node"** on any worker card or **"⚡ Dispatch Run"** in the header to trigger ad-hoc test specs or load benchmarks directly from your browser.
  - **Embedded Real-Time Console**: Watch live execution logs, step progress, and assertion checks (`✓ PASS`, `✗ FAIL`) stream to the browser or CLI in real time.
  - **CLI Remote Trigger (`hit dispatch`)**: Run `hit dispatch <spec|ref> --hub <URL> [--node <ID>] [--region <REGION>]` with live terminal streaming.
  - **Firewall/NAT Resilience**: Workers pull from `GET /api/v1/nodes/{id}/queue`, enabling dispatch into private VPCs and developer laptops without opening inbound ports.

### 🖥️ How to View and Interact with the Web Dashboard

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

### Quick CLI Commands for Hub & Fleet
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

👉 *Deep dive*: [Cloud Fleet Hub, Topology & Remote Dispatch](telemetry-and-cloud.md)

---

## 4. Git-Native Synthetic API Monitoring & Incident Alerting

Version-control your uptime probes and alert destinations directly in Git alongside your codebase:

```bash
# Run multi-region consensus check across edge nodes
hit probe run probes/checkout-sla.yaml

# Test PagerDuty, Slack Block Kit, and Opsgenie alert notifications
hit probe test-alert probes/checkout-sla.yaml

# Run 24/7 continuous synthetic monitoring daemon enrolled in Fleet Hub
hit probe daemon probes/checkout-sla.yaml --hub http://hub.internal:8080 --region us-east
```

* **Multi-Region Consensus**: Eliminates transient false positives by verifying failures across multiple geographic edge nodes before alerting on-call.
* **Native Integrations**: First-class support for PagerDuty Events API v2, Slack Block Kit rich notifications, and Opsgenie alerts.
* **Fleet Integration**: Probe daemons report live heartbeats and execution status to the Fleet Hub control plane.

👉 *Deep dive*: [Synthetic API Monitoring & Alerting](synthetic-monitoring.md)

---

## 5. Distributed Cloud Load Testing & Dynamic Fleet Auto-Discovery

Scale load tests beyond single-machine socket limits with distributed worker clusters and dynamic Hub auto-discovery:

```bash
# Start a worker enrolled in the Hub fleet with 15s heartbeats
hit perf worker --port 9090 --region us-east --hub http://hub.internal:8080 --capacity 16

# Coordinator auto-discovers healthy workers from Hub (no manual IP lists)
hit perf collections/orders/create.yaml -c 500 -d 30s \
  --hub http://hub.internal:8080 --region us-east \
  --threshold "p99 < 150ms, errors == 0" \
  --junit perf-results.xml

# Or run zero-socket distributed testing locally
hit perf collections/orders/create.yaml --distribute 4
```

* **Hub Auto-Discovery**: The load testing coordinator automatically queries the Hub for idle, healthy worker nodes and partitions target concurrency evenly.
* **Cluster Percentiles**: Calculates exact cluster-wide p50, p90, p95, and p99 latencies aggregated across all remote worker streams.
* **CI Performance Gates**: Automated `--threshold` assertions fail build pipelines if response times exceed SLA budgets.

👉 *Deep dive*: [High-Performance Load Testing](performance-testing.md)
