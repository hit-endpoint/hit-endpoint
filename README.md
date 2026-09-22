# Hit Endpoint (`hit`)

> **23 Integrated Features** — Declarative, Git-native API testing, simulation, fuzzing, load testing, and fleet operations from the command line.

[![Features](https://img.shields.io/badge/features-23%20integrated-blue.svg)](#-feature-matrix)
[![Go Version](https://img.shields.io/badge/go-1.22%2B-00ADD8.svg)](https://go.dev)
[![License](https://img.shields.io/badge/license-Apache%202.0-green.svg)](LICENSE)
[![Zero Dependencies](https://img.shields.io/badge/dependencies-zero-brightgreen.svg)](#)

A fast, compiled Go CLI delivering **23 production-ready testing features** in a single standalone binary. Requests are small, version-controlled YAML files stored directly in your repository—zero runtime dependencies, no UI overhead, no cloud account required, and no licensing hurdles.

---

## 🚀 Quick Start

### 1. Install

```bash
# Requires Go 1.22+
go install github.com/hit-endpoint/hit-endpoint/cmd/hit@latest

# Or build from source:
git clone https://github.com/hit-endpoint/hit-endpoint.git
cd hit-endpoint
go build -o hit ./cmd/hit
```

HTTPS uses your OS root certificate store. Pass `-k` or `--insecure` to skip verification for self-signed certificates.

### 2. Make an Ad Hoc Request

```bash
# Full response: status line, headers, and formatted JSON
hit https://httpbin.org/get

# Dedicated scripting outputs (clean output, no extra formatting):
hit body https://httpbin.org/get       # Response body only
hit code https://httpbin.org/get       # HTTP status code (e.g. 200)
hit time https://httpbin.org/get --ms  # Execution time in raw ms (e.g. 42.5)
hit headers https://httpbin.org/get    # Status line & headers only
```

### 3. Send Payloads & Parameters

```bash
# Query parameters and headers
hit body https://httpbin.org/get -q page=2 -q limit=50 -H 'Accept: application/json'

# JSON payload
hit POST https://httpbin.org/post -j '{"name": "Rex", "type": "dog"}'

# Form data with Basic Authentication and status assertion
hit POST https://httpbin.org/post -f user=admin -f pass=secret --auth basic:admin:secret --status 200
```

---

## 📄 Declarative YAML Requests

Organize your APIs into version-controlled files under a **Zone** (a self-contained directory representing an API workspace—housing base URLs, environment secrets, and collections).

```yaml
# collections/users/get-user.yaml
method: GET
path: /users/{{user_id}}
headers:
  Accept: application/json
assert:
  status: 200
  latency: < 250ms
  body.name: "Jane Doe"
```

Run it across environments instantly:

```bash
hit run collections/users/get-user.yaml -s staging
```

---

## 📚 Feature Matrix

| Feature | Command / Flag | Highlights |
|---|---|---|
| [**Ergonomic CLI**](docs/adhoc-requests.md) | `hit body`, `hit code`, `hit time`, `hit headers` | Ad-hoc request execution designed for UNIX piping, scripts, and terminal debugging. |
| [**Universal Importers**](docs/importers.md) | `hit import <file>` | One-click migration from OpenAPI 3.x, Swagger 2.0, Postman Collections, or cURL. |
| [**Zone Scaffolding**](docs/zones-and-servers.md) | `hit wizard`, `hit sanity` | Zero-friction workspace scaffolding + 8-point environment readiness check. |
| [**Endpoint Shorthands**](docs/shorthands.md) | `hit shorthand`, `hit <alias>` | Fast aliases for frequently called endpoints with pre-baked servers and headers. |
| [**Scenario Chains**](docs/requests-and-flows.md) | `chains/*.yaml`, `hit run` | Multi-step user journeys (login $\to$ create $\to$ delete) with state passing and script hooks. |
| [**Data-Driven Matrix Testing**](docs/matrix-testing.md) | `matrix:`, `hit run` | Parameterized row variations from inline data or external CSV/JSON files. |
| [**Auto Assertions**](docs/assertions-and-validation.md) | `hit assert`, `hit new --infer` | Automatically infers 100%-passing assertions and JSON contracts from live responses. |
| [**Semantic Diffing & Replay**](docs/history-diff-reports.md) | `hit diff <ID1> <ID2>`, `hit replay` | Unified Git-style hunks across status, payloads, and latency shifts, plus instant replay. |
| [**CI/CD & Security Reporting**](docs/history-diff-reports.md) | `--junit`, `--sarif`, `--webhook-on-failure` | Native JUnit XML test tabs, GitHub Code Scanning SARIF, incident webhooks, and HAR 1.2 / HTML reports. |
| [**Multi-Language Snippets**](docs/code-blocks.md) | `hit snippet <REF\|URL> -l L` | Exports runnable code blocks in Python (`requests`), JavaScript (`fetch`), PHP (`curl`), and Go (`net/http`). |
| [**Offline Mocking & Chaos**](docs/mock-server.md) | `hit mock --chaos`, `--openapi` | Zero-dependency mock server with latency jitter, token expiry, payload corruption, and chaos modes. |
| [**First-Class GraphQL**](docs/graphql.md) | `hit graphql`, `hit schema graphql` | Query execution with `--fail-on-errors`, full schema introspection, and SDL export. |
| [**Streaming (SSE/WS)**](docs/streaming-sse-ws.md) | `hit sse`, `hit ws` | Real-time TTFT benchmarking for LLMs and full-duplex RFC 6455 WebSockets with pattern matching. |
| [**Endpoint Scheduler**](docs/scheduler.md) | `hit schedule [--every D] [--at T]` | Automated periodic probes with response expectation validation (`--status`, `--expect`). |
| [**Mutation Fuzzing**](docs/fuzz-and-mutation.md) | `hit fuzz <URL> [--sarif]` | 6 mutation strategies detecting 5xx crashes, boundary leaks, and hangs. |
| [**Distributed Load Benchmarks**](docs/performance-testing.md) | `hit perf [--hub U] [--workers L]` | Coordinator with dynamic Hub auto-discovery, global percentiles (p50/p95/p99), and SLA gates. |
| [**API Cost Estimator**](docs/cost-estimator.md) | `hit cost` | Multi-scenario cost range modeling (Min, Expected, Worst), tiered volume curves, retries, and FinOps budget gates. |
| [**Governance & Policy**](docs/governance-and-policies.md) | `hit policy`, `--enforce-policy` | Pre-commit and CI gate enforcing contract checks, SLA ceilings, and secret hygiene. |
| [**Remote Dispatch & Live Console**](docs/telemetry-and-cloud.md#live-interactive-dispatch--fleet-job-orchestration) | `hit dispatch` | Interactive UI run triggers, real-time log/assertion streaming, and remote node targeting. |
| [**Telemetry & Fleet Hub**](docs/telemetry-and-cloud.md) | `hit run --publish`, `hit hub` | Centralized dashboard, developer vs. CI node attribution, and regional fleet topology. |
| [**Synthetic Monitoring**](docs/synthetic-monitoring.md) | `hit probe run`, `hit probe daemon` | Multi-region consensus monitoring with Hub enrollment, PagerDuty, and Slack alerts. |
| [**AI / MCP Server**](docs/ai-agent-and-mcp.md) | `hit mcp` | Native Model Context Protocol support for Cursor, Claude, and Windsurf. |
| [**Interactive Learning Sandbox**](learn/README.md) | `hit learn`, `hit learn verify` | Hands-on offline interactive training curriculum and automated grading engine. |

For in-depth guides on every feature, explore the [**Documentation Library**](docs/README.md).

---

## 📋 Command Quick Reference

| Command | Purpose |
|---|---|
| `hit [METHOD] URL` | Ad hoc request; prints status line, headers, and body |
| `hit body [METHOD] URL` | Prints response body only |
| `hit code [METHOD] URL` | Prints HTTP status code only |
| `hit time [METHOD] URL [--ms]` | Prints response duration (`45ms` or raw `45.2`) |
| `hit headers [METHOD] URL` | Prints HTTP status line and response headers |
| `hit import [collection\|openapi\|curl] FILE...` | One-click migration from OpenAPI 3.x, Swagger, Postman, or cURL |
| `hit wizard [DIR] [-y]` | Scaffolds a new API zone (configs, secrets, dirs) |
| `hit sanity [COLLECTION]` | Pre-flight check for endpoints, secrets, and credentials |
| `hit shorthand [NAME] [--url U] [--server S]` | Inspects or creates friendly aliases and server presets (`hit <alias>`) |
| `hit run REF... [-s SERVER]` | Executes saved requests or scenario chains (`--publish`, `--junit`, `--webhook-on-failure`, `--enforce-policy`) |
| `matrix:` in request specs | Parameterized data-driven test variants executed across inline or CSV/JSON rows |
| `hit assert <URL\|REF> [--save]` | Infers assertions automatically from live response |
| `hit diff <ID1> [<ID2>]` | Diffs two requests semantically with normalized JSON |
| `hit replay <ID>` | Replays a request from append-only local history log |
| `hit report coverage [--html] [--har]` | Generates OpenAPI coverage audit, interactive HTML, or HAR 1.2 exports |
| `hit snippet <REF\|URL> -l L` | Exports runnable code (Python, JS, Go, PHP) |
| `hit mock [PORT] [--chaos] [--openapi S]` | Starts offline mock server with optional chaos controls & OpenAPI generation |
| `hit graphql <URL> -q "..."` | Executes GraphQL query with error checks, or `--introspect` for SDL schema |
| `hit sse <URL>` | Connects to SSE streams and benchmarks TTFT (Time To First Token) |
| `hit ws <URL> [--expect P]` | Pure Go WebSocket client with pattern matching assertions |
| `hit schedule <REF> [--every D] [--at T]` | Automated recurring endpoint probes with expectation validations |
| `hit fuzz <REF\|URL> [-n N] [--sarif F]` | Boundary & mutation fuzzing against 5xx crashes with SARIF export |
| `hit perf REF [-c N] [--hub U]` | Distributed load test with worker clusters & dynamic Hub auto-discovery |
| `hit perf worker [--port P] [--hub U]` | Starts load worker daemon with automated Hub registration & 15s heartbeats |
| `hit cost [<FILE\|REF>] [-n N] [--budget B]` | Estimates commercial API cost range across tiers, retries, and failure rates |
| `hit policy [DIR] [--strict] [--junit F]` | Governance linter for assertions, SLA ceilings, and secret hygiene |
| `hit dispatch <FILE\|REF> [--hub U]` | Dispatches ad-hoc test or perf run to target node/region with live streaming |
| `hit hub [--port P] [--dir D] [--api-key K]` | Starts fleet control plane hub and web UI with visual regional topology |
| `hit probe run <FILE\|REF>` | Multi-region consensus probe check across simulated or edge regions |
| `hit probe daemon <FILE> [--hub U]` | Continuous synthetic API monitoring daemon with Hub enrollment & incident alerting |
| `hit probe test-alert <FILE>` | Validates PagerDuty, Slack Block Kit, and Opsgenie notifications |
| `hit mcp` | Starts stdio Model Context Protocol server for AI tools |
| `hit learn [verify]` | Hands-on offline interactive training curriculum & automated verification grader |

For full syntax specifications, see the [**Single Source of Truth Reference**](reference.md) or run `hit reference`.

---

## 🛡️ Enterprise CI/CD, Governance & Monitoring

`hit` provides production-grade infrastructure for automated engineering pipelines, compliance gates, and distributed observability:

* **CI/CD Standards & Incident Webhooks**: Export results to standard **JUnit XML** and **SARIF 2.1.0** (GitHub Code Scanning) with automated failure webhooks (`--junit`, `--sarif`, `--webhook-on-failure`).
* **Organization-Wide Governance Gates**: Lint API definitions against `.hit/policy.yaml` rules, block hardcoded secrets, and enforce runtime latency SLA ceilings (`hit policy`, `--enforce-policy`).
* **Cloud Fleet Hub & Regional Topology**: Centralized telemetry dashboard attributing runs to developers vs. CI runners, 15s heartbeats, and real-time regional worker mesh (`hit hub`, `hit run --publish`).
* **Live Interactive Dispatch**: Trigger functional tests or load benchmarks from the Web UI or CLI with real-time log streaming (`hit dispatch`).
* **Git-Native Synthetic Probes**: Version-control uptime monitors in Git with multi-region consensus checks and PagerDuty / Slack alerting (`hit probe run`, `hit probe daemon`).
* **Distributed Cloud Load Testing**: Multi-worker performance testing with automated Hub worker auto-discovery and statistical percentiles (`hit perf --hub`).

👉 For complete architectural guides, REST API specs, and dashboard walkthroughs, see the [**Enterprise CI/CD, Governance & Monitoring Guide**](docs/enterprise.md) and [**Cloud Fleet Hub Guide**](docs/telemetry-and-cloud.md).

---

## 🎓 Interactive Learning Sandbox

`hit` ships with an offline interactive curriculum and an automated grading engine.

```bash
# 1. Start the local sandbox mock server (port 8765)
hit mock &

# 2. Verify connectivity
hit code http://127.0.0.1:8765/health

# 3. Work through lessons under learn/ and verify your work
hit learn verify 1
hit learn verify all
```

Explore the curriculum lessons and interactive glossary in [**`learn/README.md`**](learn/README.md).

---

## 📁 Repository Layout

```text
hit-endpoint/
├── cmd/hit/          # CLI binary entrypoint
├── internal/         # Modular engines (assertions, diff, runner, fuzz, perf, mock)
├── docs/             # Technical guides and deep-dive documentation
├── examples/         # Reference zones (petstore, bandsintown)
├── learn/            # 7-lesson interactive curriculum + glossary
└── prompts/          # Optimized LLM prompts for test/request generation
```
