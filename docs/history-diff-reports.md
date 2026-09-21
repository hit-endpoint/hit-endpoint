# History, Diffing & Comprehensive Reports

`hit` records every request you send—both ad hoc CLI commands and zone suite runs—into a local, append-only, ring-buffered `.jsonl` history log. Sensitive credentials and secrets are automatically masked (`[MASKED]`) prior to disk writes.

* **Zone Hits**: Saved to `<zone>/.hit/history.jsonl` (scoped per project).
* **Ad Hoc Hits**: Saved to `~/.hit/history.jsonl` (global fallback).
* **Bypass Recording**: Pass `--no-history` to suppress logging for any invocation.

---

## Viewing History (`hit history`)

```bash
# List recent execution hits (index, method, URL, status code, latency)
hit history

# Limit number of entries shown
hit history --limit 50

# Filter history entries
hit history --folder auth             # Filter by collection folder
hit history --status 500              # Filter by HTTP status code
hit history --failed                  # Show only requests with failed assertions or non-2xx codes
hit history --json                    # Emit history records as JSON lines

# Inspect exact details of a specific historic hit (headers, body, tests, timing)
hit history show 1                    # Inspect most recent hit (1-indexed)
hit history show hit_1725998123_a1b2  # Inspect by unique ID

# Clear history log
hit history clear
```

---

## Replaying Requests (`hit replay`)

Re-execute any recorded request with its exact recorded headers, query parameters, and payload:

```bash
# Replay the most recent request
hit replay 1

# Replay an older request with verbose output
hit replay 3 -v --headers

# Replay with variable overrides
hit replay 1 --var env=production
```

---

## Response Diffing & Regression Analysis (`hit diff`)

Detect API drift, unexpected breaking payload changes, or performance degradation by comparing two historic hits, or diffing a live replayed response against its recorded baseline:

```bash
# Compare two historic hits (Hit 2 against Hit 1)
hit diff 2 1

# Compare an older hit against the latest recorded hit (defaults 2 against 1)
hit diff 2

# Live Replay Diff: Re-execute hit 1 live and diff fresh response against baseline
hit replay 1 --diff

# Include response header differences in the diff
hit diff 2 1 --headers

# Suppress status line and latency metrics, focusing strictly on body payload
hit diff 2 1 --body-only

# Machine-readable JSON diff output for automated CI regression gates
hit diff 2 1 --json
```

### Key Diff Features
* **Status & Latency Metrics**: Colorized status transitions (`200 OK` vs `500 Internal Error`), payload size delta (`+140 B`), and execution duration shift (`45ms → 88ms (+95.5%)`).
* **Canonical JSON Normalization**: Payloads are parsed and formatted with sorted keys prior to comparison, preventing false positives caused solely by unordered JSON keys.
* **Unified Git-Style Hunks**: Clean, syntax-highlighted hunks (`@@ -1,4 +1,4 @@`) with red deletions (`-`) and green additions (`+`).

---

## Promoting History to Request Files

Convert debugging CLI hits into permanent, version-controlled zone request YAML files:

```bash
hit history save 1 collections/pets/create.yaml

# Auto-synthesize rich assertions from the historic response
hit history save 1 collections/pets/create.yaml --infer
```

---

## Reports and API Audits (`hit report`)

### 1. Interactive HTML Report (`--html` or `hit report html`)
Generate a self-contained, interactive HTML dashboard with dark theme, responsive metric cards, client-side JavaScript filtering, collapsible drawer inspectors, and assertion breakdowns. Zero CDN dependencies—opens in any browser offline:

```bash
# Generate HTML report directly during test suite execution:
hit run smoke --html test-report.html

# Or generate from recent zone history:
hit report html -o report.html -n 50
```

### 2. HTTP Archive Export (`--har` or `hit report har`)
Export requests, responses, headers, query parameters, cookies, and microsecond timings to standard **HAR 1.2** files compatible with Chrome DevTools, Safari, Charles Proxy, Postman, Insomnia, or Datadog:

```bash
# Export test run directly to HAR:
hit run smoke --har test-run.har

# Export filtered historical hits:
hit report har -o history.har
hit report har -o errors.har --status 500 --limit 100
```

### 3. OpenAPI Contract Coverage & Drift Audit (`hit report coverage`)
Audit your test suite against an OpenAPI 3.0, 3.1, or Swagger 2.0 specification:

```bash
hit report coverage --openapi openapi.yaml
hit report coverage --openapi https://api.example.com/openapi.json --strict
hit report coverage --openapi openapi.yaml --json
```

* **Coverage Score**: Percentage of OpenAPI operations with matching tests.
* **Untested Backlog**: Lists operations defined in OpenAPI that have no tests.
* **Undocumented Endpoints (API Drift)**: Flags zone requests calling endpoints not declared in the specification.
* **Status Code Conformance**: Validates whether test assertions check documented response status codes.
* **CI Drift Gate**: Under `--strict`, exits with code `1` if coverage is below 100% or drift is detected.

### 4. Latency Trends & Regression Detection (`hit report latency`)
Analyze response time trends across endpoints over time using historical logs:

```bash
hit report latency                    # Analyze 7-day percentiles (p50, p90, p95, p99)
hit report latency --days 14          # Custom analysis window
hit report latency --threshold 25%    # Flag endpoints with >25% latency regression (exits 1)
hit report latency --json
```

### 5. Automated JUnit XML Export (`--junit`)
Export test and SLA outcomes directly to standard JUnit XML format for native visualization in CI test summary tabs (GitHub Actions, GitLab CI, Jenkins, CircleCI):

```bash
# Export test suite results to JUnit XML
hit run collections/ --junit test-results.xml

# Export load testing benchmark and SLA gate results
hit perf collections/users/get.yaml --threshold "p99 < 200ms" --junit perf-results.xml

# Export governance policy compliance checks
hit policy --strict --junit policy-results.xml
```

### 6. SARIF Security & Vulnerability Export (`--sarif`)
Export mutation fuzzing crashes and vulnerability findings directly to OASIS **SARIF 2.1.0** (Static Analysis Results Interchange Format) to surface security warnings in GitHub Code Scanning:

```bash
# Export boundary fuzzing findings to SARIF
hit fuzz collections/payments/charge.yaml -n 200 --sarif security-findings.sarif
```

### 7. Real-Time Failure Webhooks (`--webhook-on-failure`)
Dispatch an automated HTTP POST webhook with a structured incident payload whenever an assertion fails or a test suite errors out:

```bash
hit run collections/ --webhook-on-failure https://alerts.internal.corp/v1/ci-failure
```

