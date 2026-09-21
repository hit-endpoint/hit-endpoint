# Endpoint Scheduler & Recurring Probes (`hit schedule`)

Hit Endpoint includes a built-in scheduler (`hit schedule`) for automating timed endpoint health checks, periodic polling, SLA monitoring, and expectation comparisons.

No cron daemons, background agents, or complex monitoring pipelines are required. Run recurring checks directly from your terminal or CI environment.

---

## 🚀 Quick Start

### 1. Simple Periodic Probe
Execute an endpoint every 5 seconds for 5 iterations and assert status code 200:

```bash
hit schedule https://httpbin.org/get --every 5s -n 5 --status 200
```

Terminal output displays live execution ticks:

```text
⏰ Scheduling executions for https://httpbin.org/get
   Interval: 5s | Max Runs: 5
   Expected: status=200

[1/5] 14:30:00  https://httpbin.org/get  200 (42.1ms)  ✔ PASS
[2/5] 14:30:05  https://httpbin.org/get  200 (38.4ms)  ✔ PASS
[3/5] 14:30:10  https://httpbin.org/get  200 (39.0ms)  ✔ PASS
[4/5] 14:30:15  https://httpbin.org/get  200 (41.2ms)  ✔ PASS
[5/5] 14:30:20  https://httpbin.org/get  200 (37.8ms)  ✔ PASS

────────────────────────────────────────────────────────────
Schedule Summary: 5 runs | 5 passed | 0 failed (100% success rate)
```

### 2. Scheduled Start Time
Delay execution to start at a specific time in the future:

```bash
# Start in 30 seconds
hit schedule https://api.example.com/health --at +30s

# Start at 3:30 PM (local time)
hit schedule https://api.example.com/batch --at 15:30

# Start at an absolute timestamp
hit schedule https://api.example.com/batch --at "2026-09-12 02:00:00"
```

---

## 🎯 Supported Targets

`hit schedule` works interchangeably across:

1. **Ad Hoc URLs**:
   ```bash
   hit schedule https://api.example.com/v1/status -m GET -H "Authorization: Bearer token"
   ```

2. **Saved Zone Requests**:
   ```bash
   hit schedule petstore/pets/01-list-pets -s staging --every 10s -n 10
   ```
   When targeting a zone request reference, all assertions defined in the YAML file are automatically validated on each tick!

3. **Configured Shorthands**:
   ```bash
   hit schedule petstore-prod --every 1m -d 1h --status 200
   ```

---

## ⚙️ Options & Flag Reference

| Flag | Shorthand | Description | Default |
|---|---|---|---|
| `--at <TIME>` | `--start` | Start time: `now`, `+10s`, `+5m`, `15:04`, or RFC3339 | `now` |
| `--every <DURATION>` | `-i`, `--interval` | Interval between runs: `100ms`, `5s`, `1m` | None (single run) |
| `-n <COUNT>` | `--count` | Stop after N runs | Infinite if interval set |
| `-d <DURATION>` | `--duration` | Total time limit: `30s`, `10m`, `2h` | None |
| `--status <CODE>` | | Asserts expected HTTP status code | None |
| `--expect <PATTERN>` | | Asserts regex or string pattern in response body | None |
| `-m <METHOD>` | `--method` | HTTP method (`GET`, `POST`, `PUT`, `DELETE`, etc.) | `GET` |
| `-H <HEADER>` | `--header` | Custom headers (`-H "Key: Value"`, repeatable) | None |
| `-b <BODY>` | `--body` | Raw body text or `@file` path | None |
| `-j <JSON>` | `--json-body` | JSON body string or `@file` path | None |
| `-s <SERVER>` | `-e`, `--env` | Target server/environment profile | Zone default |
| `-z <ZONE>` | `-w` | Target zone directory | Current directory |
| `--json` | | Output aggregate summary & ticks as JSON | `false` |
| `-q`, `--quiet` | | Suppress tick logs; output final summary only | `false` |

---

## 🧪 Response Comparison & Assertions

You can assert multiple conditions on each scheduled run:

```bash
hit schedule https://api.example.com/v1/orders \
  --every 10s \
  -n 6 \
  --status 200 \
  --expect '"status":"active"'
```

If any run fails the expectation:
- The tick logs a distinct `✘ FAIL` badge with the failure reason.
- The command exits with non-zero exit code (`1`), enabling automated alerting and CI pipeline integration.

---

## 📜 Seamless History & Reporting Integration

Every scheduled tick is automatically logged to your local append-only history log (`.hit/history/history.jsonl`):

```bash
# View all recent runs including scheduled ticks
hit history

# Compare two scheduled ticks to inspect body or latency differences
hit diff 1 2

# Generate an interactive HTML report of the scheduled session
hit report html -o schedule-report.html

# Analyze latency percentiles across all ticks
hit report latency
```
