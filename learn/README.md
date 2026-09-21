# 🎓 API Testing Learning Sandbox

Welcome to the **API Testing Learning Sandbox**! Whether you are a software engineer building backend services, a QA engineer writing automated test suites, or a developer exploring API architecture, this sandbox provides a hands-on, progressive curriculum for mastering API testing from fundamentals to advanced automation.

Every lesson is paired with **executable examples** that run against the built-in offline mock API server (`hit mock`) or live public endpoints. Zero cloud setup, zero external dependencies.

---

## 🚀 Sandbox Quickstart

Open a terminal and start the built-in mock API server in the background:

```bash
# Start the offline learning mock server (runs locally on port 8765)
./hit mock &

# Verify connectivity:
./hit code http://127.0.0.1:8765/health
# 200
```

You now have a fully operational REST API sandbox running locally with authentication endpoints, paginated collections, delay simulators, and CRUD resources.

---

## 📚 The Curriculum

### 📖 [API Testing Glossary](glossary.md)
A comprehensive A–Z reference guide covering essential terminology across HTTP specifications, RESTful architecture, authentication schemes, assertion patterns, load testing metrics, and CI/CD pipelines. Bookmark this and reference it anytime!

---

### [Lesson 1: HTTP Protocols & Request Anatomy](01-http-basics.md)
* **Topics**: The Client-Server model, HTTP verbs (`GET`, `POST`, `PUT`, `DELETE`), URL structures, status code categories (2xx, 4xx, 5xx), and status lines.
* **Hands-on Exercises**: Sending ad-hoc requests, querying status codes with `hit code`, measuring latency with `hit time`, and viewing raw headers with `hit headers`.

### [Lesson 2: Parameters, Payloads & Content Negotiation](02-headers-and-payloads.md)
* **Topics**: Path parameters vs query parameters, request headers, Content-Type negotiation (`application/json`, `application/x-www-form-urlencoded`), and payload formatting.
* **Hands-on Exercises**: Filtering resources via query strings, sending JSON payloads, urlencoded forms, and passing custom trace headers.

### [Lesson 3: Authentication, Security & State Management](03-authentication-and-state.md)
* **Topics**: Basic Authentication, Bearer tokens, API Keys, token expiration, secret management hygiene, and avoiding credentials in git.
* **Hands-on Exercises**: Authenticating against `/auth/login`, capturing dynamic tokens into session state, masking secrets, and reusing captured tokens across requests.

### [Lesson 4: Writing Declarative Assertions & Validations](04-assertions-and-validations.md)
* **Topics**: Anatomy of an assertion, testing status codes, validating response headers, JMESPath expressions for deep JSON schema checks, array lengths, type checking, and negative testing.
* **Hands-on Exercises**: Writing test blocks in YAML, asserting 201 Created and 404 Not Found, testing response times (`max_ms`), and verifying JSON properties.

### [Lesson 5: Stateful Scenario Chains & User Journeys](05-scenario-flows.md)
* **Topics**: Single-endpoint testing vs end-to-end integration testing, chaining dependent steps (Login $\to$ Create $\to$ Fetch $\to$ Delete $\to$ Verify), passing dynamic IDs, and step overrides.
* **Hands-on Exercises**: Building and executing multi-step chains in `chains/*.yaml`, managing chain-scoped variables, and testing teardown states.

### [Lesson 6: Load Testing, Concurrency & Performance SLAs](06-performance-and-slas.md)
* **Topics**: Latency vs throughput, Concurrency vs Requests-Per-Second (RPS), understanding percentiles (p50, p90, p95, p99), cold-start thundering herds, and SLA threshold gates.
* **Hands-on Exercises**: Running load tests with `hit perf`, linear ramp-up staging, setting CI pass/fail gates (`--threshold "p95<100ms,errors<1%"`), and benchmarking multi-step chains.

### [Lesson 7: OpenAPI Contracts, Coverage & Drift Audits](07-contracts-and-drift.md)
* **Topics**: API contracts (OpenAPI 3.x / Swagger 2.0), contract testing, identifying untested backlog operations, detecting undocumented endpoints (API drift), and validating status code schemas.
* **Hands-on Exercises**: Importing specifications, generating automated contract audits with `hit report coverage`, and identifying breaking API drift.

---

## 🛠️ Sandbox Tools Reference

| Command | Purpose |
|---|---|
| `./hit mock [port]` | Runs local offline mock server with auth, CRUD, delays |
| `hit <url>` | Sends request and prints full status line, headers, and body |
| `hit body <url>` | Prints only the formatted JSON/text response body |
| `hit code <url>` | Prints only the HTTP status code (e.g. `200`, `404`) |
| `hit time <url>` | Prints only the response duration (e.g. `45ms`) |
| `hit headers <url>` | Prints the status line and response headers |
| `hit -z <zone> run <ref>` | Runs a saved YAML request file with assertions (`--publish`, `--junit`, `--enforce-policy`) |
| `hit -z <zone> perf <ref>` | Runs a multi-worker benchmark load test (`--workers`, `--distribute`) |
| `hit perf worker [--port P]` | Ephemeral worker daemon for distributed load testing |
| `hit policy [dir] [--strict]` | Governance linter for assertions, SLA ceilings, and secret hygiene |
| `hit probe [run\|daemon]` | Git-native synthetic API monitoring with multi-region consensus and alerts |
| `hit -z <zone> report coverage` | Audits test coverage against OpenAPI specifications |
| `hit schedule <url\|ref>` | Runs scheduled periodic probes and validates expectations |
| `hit shorthand [ls\|set]` | Manages and invokes named endpoint presets |
| `hit learn verify [1-7\|all]` | Interactive grading engine verifying completed lesson exercises |
