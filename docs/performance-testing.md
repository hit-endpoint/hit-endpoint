# High-Performance Load Testing (`hit perf`)

`hit` includes a high-throughput, multi-worker performance benchmarking engine capable of load testing single requests or complex stateful scenario flows without requiring external tools like JMeter, k6, or Locust.

---

## Quickstart

```bash
# Send 1,000 requests using 20 concurrent virtual workers
hit perf pets/list -c 20 -n 1000

# Run load test for 60 seconds with 50 concurrent workers, capped at 200 RPS
hit perf pets/list -c 50 -d 60 --rps 200

# Staged linear ramp-up (scale from 1 to 30 workers over 10 seconds)
hit perf pets/list -c 30 --ramp-up 10 -d 60

# Load test stateful multi-step user journey flows
hit perf flows/login-and-crud.yaml -c 10 -n 100
```

---

## Command Flags & Tuning

| Flag | Description |
|---|---|
| `-c, --concurrency N` | Number of concurrent virtual worker goroutines (default: 10) |
| `-n, --requests N` | Total number of requests to execute |
| `-d, --duration S` | Test duration in seconds (alternative to `-n`) |
| `--rps N` | Throughput rate limiter capping total requests per second |
| `--warmup N` | Number of initial warmup requests executed before recording metrics |
| `--ramp-up S` | Duration in seconds over which virtual workers linearly scale from 1 to `N` |
| `--threshold SPEC` | Automated pass/fail SLA gates (e.g. `"p95<200ms,errors<1%"`) |
| `--check` | Returns exit code 1 if threshold gates fail |
| `--workers URLS` | Distribute load across cluster worker nodes (e.g. `"http://w1:8989,http://w2:8989"`) |
| `--distribute N` | Partition load across `N` local in-process worker engines |
| `--junit FILE` | Export load test and SLA gate results to standard JUnit XML |
| `--csv FILE` | Exports raw per-request execution records to CSV |
| `--json` | Emits complete benchmark metrics and histogram as JSON |

---

## Automated CI Threshold Gates (`--threshold`)

Integrate performance assertions into CI/CD build pipelines. If your service violates response time SLAs or error rate budgets, `hit perf` fails the pipeline with exit code `1`:

```bash
hit perf pets/list -c 20 -d 30 --threshold "p95<150ms,errors<1%,rps>100"
```

### Supported Threshold Metrics
* `p50`, `p90`, `p95`, `p99`: Latency percentiles (e.g. `p95 < 200ms`)
* `min`, `max`, `mean`: Absolute latency boundaries
* `rps`: Minimum throughput rate (e.g. `rps >= 50`)
* `errors`: Percentage or count of non-2xx status codes (e.g. `errors < 1%`)
* `failed`: Count of connection or transport errors (e.g. `failed == 0`)

---

## Stateful Scenario Flow Load Testing

Unlike standard HTTP benchmarking tools that only hit a single static URL, `hit perf` can load test multi-step scenario flows (`flows/*.yaml`):
* Each virtual worker maintains an **isolated session context**.
* Captured authentication tokens and dynamic IDs (e.g. created entities) remain scoped per virtual user.
* Benchmarks end-to-end user journeys (e.g. Login $\to$ Create Cart $\to$ Checkout $\to$ Verify Receipt).

---

## Benchmark Metrics & Terminal Output

A typical `hit perf` summary provides comprehensive operational insights:

```
Concurrency:        20 workers
Completed Requests: 1,000 (100% OK)
Execution Time:     2.41s
Throughput:         414.9 req/s

Latency Distribution:
  Min:     1.2ms
  Mean:    4.8ms
  50%:     3.9ms  (p50)
  90%:     8.1ms  (p90)
  95%:     11.4ms (p95)
  99%:     18.7ms (p99)
  Max:     24.2ms

Status Codes:
  200 OK:  1000

Latency Histogram:
  1.2ms - 5.0ms:   [====================================] 720
  5.0ms - 10.0ms:  [==========] 210
  10.0ms - 15.0ms: [===] 55
  15.0ms - 25.0ms: [=] 15

SLA Thresholds:
  ✓ p95 < 150ms (actual: 11.4ms)
  ✓ errors < 1% (actual: 0.0%)
```

---

## Distributed Cloud Load Testing

Scale load generation across multiple machines or container nodes to simulate enterprise traffic:

### 1. Start Worker Daemons
Run `hit` as an ephemeral load generator worker daemon, optionally enrolling directly into your Fleet Hub:

```bash
# Standalone worker
hit perf worker --port 8989 --region us-east

# Enrolled worker with Hub registration & 15s heartbeats
hit perf worker --port 8989 --region us-east --hub http://hub.internal:8080 --capacity 16
```

### 2. Coordinator Execution (Auto-Discovery or Manual IP List)
Run the coordinator to partition total concurrency and throughput rate caps across all workers:

```bash
# Dynamic Auto-Discovery from Fleet Hub (no manual IP lists)
hit perf collections/checkout.yaml \
  --hub http://hub.internal:8080 --region us-east \
  -c 300 -d 60s --threshold "p99 < 250ms, failed == 0" \
  --junit perf-results.xml

# Manual worker list
hit perf collections/checkout.yaml \
  --workers "http://worker1:8989,http://worker2:8989,http://worker3:8989" \
  -c 300 -d 60s --threshold "p99 < 250ms, failed == 0" \
  --junit perf-results.xml
```

The coordinator automatically:
1. Validates node health (or auto-discovers active workers from Hub) before initiating load.
2. Evenly divides concurrency and requests across workers.
3. Polls metrics concurrently and streams real-time cluster throughput in terminal.
4. Collects and merges raw latencies, calculating exact global percentiles (p50/p90/p95/p99).
5. Emits a consolidated terminal scorecard with per-worker regional breakdowns.

### 3. Local Multi-Worker Partitioning (`--distribute N`)
Utilize multi-core developer machines or CI agents without network setup:

```bash
hit perf collections/checkout.yaml --distribute 4 -c 100 -n 1000
```


