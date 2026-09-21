# Lesson 6: Load Testing, Concurrency & Performance SLAs

Functional testing ensures an API works for a single user; performance testing ensures it survives real-world traffic spikes. In this lesson, you will learn how to benchmark endpoints, interpret statistical latency percentiles, and establish automated CI/CD performance gates.

---

## 🎯 Learning Objectives
1. Understand the difference between **Throughput (RPS)** and **Latency (ms)**.
2. Learn why **Percentiles (p50, p95, p99)** are essential and why averages lie.
3. Prevent cold-start spikes using **Linear Ramp-Up Staging**.
4. Define and enforce automated **SLO/SLA Threshold Gates** in CI pipelines.
5. Load test end-to-end scenario flows with isolated session states.

---

## 🧠 Core Concept: The Flaw of Averages

Imagine an API handles 100 requests:
- 99 requests take **10ms**.
- 1 request hangs for **10,000ms** (10 seconds).
- **Average (Mean)**: ~110ms *(misleadingly looks acceptable!)*
- **p99 (99th percentile)**: 10,000ms *(identifies that 1 out of 100 users had a catastrophic experience!)*

Percentiles tell the true story:
* **p50 (Median)**: Half of all requests were faster than this.
* **p95**: 95% of requests completed faster than this; measures typical tail latency.
* **p99**: The worst 1% edge-case experience (slow database queries, GC pauses, lock contention).

---

## 💻 Hands-On Sandbox Exercises

Ensure `./hit mock 8765 &` is running.

### Exercise 6.1: Running Your First Benchmark

Let's load test the Petstore `/health` endpoint with 10 concurrent workers sending 200 requests:

```bash
./hit -z examples/petstore-zone perf health -c 10 -n 200
```

**Output**:
```
Benchmark: petstore/00-health  (http://127.0.0.1:8765/health)
concurrency: 10    requests: 200    warmup: 0

Throughput:
  200 requests in 32ms  (6,250.0 req/s)
  0 transport errors

Latency:
  min:    0.2 ms
  mean:   0.5 ms
  p50:    0.4 ms
  p90:    0.8 ms
  p95:    1.0 ms
  p99:    1.5 ms
  max:    2.1 ms

Distribution:
  [ 0.2ms -  0.6ms]  ██████████████████████████████  156 (78%)
  [ 0.6ms -  1.0ms]  ███████                          36 (18%)
  [ 1.0ms -  1.4ms]  █                                 6 (3%)
  [ 1.4ms -  2.1ms]  █                                 2 (1%)
```

Notice how `hit` provides:
- Throughput in requests per second (`req/s`).
- Full percentile breakdown (min, mean, p50, p90, p95, p99, max).
- ASCII distribution histogram visualizing where user latencies cluster.

---

### Exercise 6.2: Setting Automated CI Threshold Gates (`--threshold`)

In automated pipelines, you want tests to automatically **fail** if performance breaches your Service Level Objectives (SLOs).

Run a benchmark with an SLO that `p95` must be under 50ms and error rate below 1%:

```bash
./hit -z examples/petstore-zone perf health \
  -c 10 -n 100 \
  --threshold "p95<50ms,errors<1%"
```

**Output**:
```
Thresholds:
  ✓ p95 < 50ms (got 1.1ms)
  ✓ errors < 1% (got 0.0%)

All 2 threshold assertions passed.
```
Exit code is `0` (build passes!).

Now test what happens when an SLA is breached:
```bash
./hit -z examples/petstore-zone perf health \
  -c 10 -n 100 \
  --threshold "p95<0.01ms"
```
**Output**:
```
Thresholds:
  ✗ p95 < 0.01ms (got 1.0ms)

1 of 1 threshold assertions failed.
```
`hit` exits with code `1`, immediately catching the performance regression!

---

### Exercise 6.3: Avoiding Cold-Start Spikes (`--ramp-up`)

If you spin up 100 concurrent workers instantaneously against a server with unprimed caches or database connection pools, you can trigger a **Thundering Herd** problem.

Pass `--ramp-up <seconds>` to smoothly scale concurrency from 1 to $N$:

```bash
./hit -z examples/petstore-zone perf health \
  -c 20 -d 5 --ramp-up 2
```

Workers smoothly join over 2 seconds, providing realistic traffic ramping.

---

### Exercise 6.4: Benchmarking Multi-Step Scenario Flows

Testing isolated endpoints doesn't reveal performance bottlenecks in complete user journeys. `hit` can load test multi-step scenario flows:

```bash
./hit -z examples/petstore-zone perf smoke -c 5 -n 25
```

Each concurrent virtual worker executes its own isolated flow with cloned variable state, measuring iteration time across all steps!

---

## 📝 Practice Quiz & Challenge

1. **Question**: If an API's p99 latency is 1500ms but its average is 40ms, is the API performant?
   *(Answer: For 99% of requests it is, but 1 out of every 100 customers experiences a 1.5-second lag. In e-commerce or gaming, tail latency often causes high abandonment.)*
2. **Challenge**: Run a 3-second load test with rate-limiting set to 50 requests per second using `--rps 50`:
   ```bash
   ./hit -z examples/petstore-zone perf health -c 5 -d 3 --rps 50
   ```

---

**Next Up**: In [**Lesson 7: OpenAPI Contracts, Coverage & Drift Audits**](07-contracts-and-drift.md), we will learn how to audit our test coverage against API specifications and detect breaking changes!
