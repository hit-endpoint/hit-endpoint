# Prompt: run a load test and summarise it

```
Load test the hit request `<ref>` in `zones/<zone>` against server `<server>`
(this must not be production unless I say so).

Run `hit -z zones/<zone> perf <ref> -s <server> -c <concurrency> -d <seconds>
--warmup 20 --check --json --csv perf-<ref>.csv`. Then give me:
- throughput and the p50 / p95 / p99 latencies in a short table
- error and non-2xx counts and what they were
- whether the request's own tests kept passing under load (`--check`)
- one or two sentences on whether the numbers look healthy for this kind of endpoint, and what
  a follow-up run should change (concurrency, duration, `--rps` cap)
```
