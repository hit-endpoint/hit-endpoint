# Hit Endpoint Documentation Index

Welcome to the comprehensive documentation library for **Hit Endpoint** (`hit`). Browse the focused topic guides below:

---

## 📚 Guides by Topic

| Guide | Description |
|---|---|
| [**Ad Hoc Requests & CLI Ergonomics**](adhoc-requests.md) | The 5 output modes (`hit`, `hit body`, `hit code`, `hit time`, `hit headers`), shell scripting friendliness, `--raw`/`--ms` timing, and command options. |
| [**Zones, Servers & Secrets**](zones-and-servers.md) | Zone layout, multi-server servers, secret masking hygiene (`.secrets.yaml`, `$env:`), variable precedence, and automated pre-flight checks (`hit sanity`). |
| [**Endpoint Scheduler & Recurring Probes**](scheduler.md) | Automate scheduled execution (`hit schedule`), start delays (`--at`), periodic intervals (`--every`), and response expectation comparisons (`--status`, `--expect`). |
| [**Endpoint Shorthands & Named Presets**](shorthands.md) | Configure friendly aliases (`hit <name>`, `hit body <name>`) with pre-baked servers, headers, and bodies in `zone.yaml`, `shorthands.yaml`, or dynamically via CLI. |
| [**Saved Requests, Collections & Scenarios**](requests-and-flows.md) | Declarative YAML request specifications, folder inheritance (`_defaults.yaml`), `hit run`, multi-step request chains (`chains/`), and Python script hooks. |
| [**Declarative Assertions & Auto-Generation**](assertions-and-validation.md) | Status codes, JMESPath JSON validations, regex operators, and automatic assertion inference from live endpoints or history (`hit assert`, `hit new --infer`). |
| [**High-Performance Load Testing**](performance-testing.md) | Multi-worker concurrency benchmarks (`hit perf`), virtual user ramp-up staging, throughput caps (`--rps`), and automated CI SLA/SLO threshold gates (`--threshold`). |
| [**History, Diffing & Comprehensive Reports**](history-diff-reports.md) | Append-only history logs (`hit history`), request replay (`hit replay`), semantic JSON response diffing (`hit diff`), offline HTML reports (`--html`), HAR 1.2 export (`--har`), and OpenAPI contract drift audits (`hit report coverage`). |
| [**Mock Server, Chaos & Dynamic OpenAPI**](mock-server.md) | Zero-dependency built-in offline mock API (`hit mock`), chaos engineering simulation (flaky, rate limits, jitter, auth expiry, corruption), and dynamic OpenAPI 3.x in-memory stateful mocking. |
| [**Mutation & Fuzz Testing Engine**](fuzz-and-mutation.md) | Automated boundary, type confusion, extreme strings, injection, and header fuzzing (`hit fuzz`) to uncover 5xx backend crashes and hangs. |
| [**Multi-Language Code Snippets**](code-blocks.md) | Generate complete, runnable code blocks in Python (`requests`), JavaScript (`fetch`), PHP (`curl`), and Go (`net/http`) with safe property extractors (`hit snippet`, `hit show --lang`). |
| [**First-Class GraphQL & Schema Introspection**](graphql.md) | Query execution (`hit graphql`), error inspection (`--fail-on-errors`), schema introspection (`--introspect`), and SDL export (`hit schema graphql`). |
| [**Data-Driven Matrix Testing**](matrix-testing.md) | Parameterized row variants (`matrix:`), inline data, external CSV/JSON files, variable templates (`{{row.field}}`), and consolidated scorecard output. |
| [**Real-Time & Streaming Testing (SSE & WS)**](streaming-sse-ws.md) | Server-Sent Events monitoring with Time-To-First-Token (`hit sse`), and pure Go RFC 6455 WebSocket duplex client with pattern assertions (`hit ws`). |
| [**Universal Importers**](importers.md) | One-command migrations from OpenAPI 3.0/3.1, Swagger 2.0, Postman Collections, and cURL commands (`hit import`). |
| [**Enterprise CI/CD, Governance & Monitoring**](enterprise.md) | Comprehensive overview of CI/CD standards (JUnit, SARIF), policy gates, fleet control plane, synthetic uptime probes, and distributed load testing. |
| [**Enterprise Governance & Policy Gates**](governance-and-policies.md) | Policy linter (`hit policy`), contract mandates, SLA ceilings, secret hygiene checks, and runtime verification (`--enforce-policy`). |
| [**Synthetic API Monitoring & Alerting**](synthetic-monitoring.md) | Git-native synthetic probes (`hit probe run`, `hit probe daemon`), multi-region consensus, and PagerDuty, Slack, Opsgenie alerts. |
| [**Cloud Fleet Hub, Topology & Remote Dispatch**](telemetry-and-cloud.md) | Centralized telemetry & node attribution, active regional fleet topology with worker auto-discovery, and interactive remote dispatch with real-time streaming. |
| [**API Cost & Retry Estimator**](cost-estimator.md) | Multi-scenario cost range modeling (Min, Expected, Worst-case, Outage Storm), tiered volume curves, LLM token pricing, and FinOps budget guardrails (`hit cost`). |
| [**AI Coding Agent Tooling & MCP Server**](ai-agent-and-mcp.md) | Model Context Protocol stdio server (`hit mcp`), token-compact agent output (`--format=agent`), JSON Schemas (`hit schema`), and prompt templates (`prompts/`). |

---

## 🎓 Hands-On Learning Sandbox

* [**API Testing Learning Curriculum & Sandbox**](../learn/README.md): An offline interactive curriculum with hands-on exercises, an encyclopedic glossary, and an automated verification grading engine (`hit learn verify`).

---

## 📖 CLI Syntax Reference

* [**Single Source of Truth Reference**](../reference.md): Complete, unabridged specification of all YAML schemas, flags, placeholders, and CLI commands. Can also be printed directly from the terminal via `hit reference`.
