# Evals

Every stage is measured the same way, so results stay comparable.

## Test setup

- Tool: autocannon
- Endpoint: `GET /items/1`
- Server, Postgres and autocannon on the same Windows machine (noisy, use for relative comparison)
- Final numbers: AWS, load generator on a separate EC2 in the same region

> CPU / RAM / Go version / Postgres setup: _fill in_

## Queries

```powershell
# Standard run (used for every stage)
autocannon -c 100 -a 1000000 http://localhost:8080/items/1

# Connection sweep (find the saturation point)
autocannon -c 50  -a 1000000 http://localhost:8080/items/1
autocannon -c 200 -a 1000000 http://localhost:8080/items/1

# Quick smoke test
autocannon -c 100 -a 100000 http://localhost:8080/items/1

# Profile while the load test runs
go tool pprof -http=:9999 "http://localhost:6060/debug/pprof/profile?seconds=20"
```

Flags: `-c` connections, `-a` total requests, `-w` worker threads (use only if autocannon itself maxes out the CPU), `-d` duration. Don't combine `-a` and `-d`.

## Metrics recorded

| Metric | Source |
|---|---|
| req/s (avg, median) | Req/Sec table |
| Latency p50, p99, max | Latency table |
| Errors / non-2xx | Summary lines |
| Total time | Summary lines |

A run with non-2xx responses or timeouts is invalid. Fix it first.

## Stage 1: Baseline (Postgres only)

**Query:** `autocannon -c 100 -a 1000000 http://localhost:8080/items/1`

| Metric | Value |
|---|---|
| req/s avg | 23,809 |
| req/s median | 24,511 |
| req/s min / max | 7,522 / 24,927 (97.5th pct) |
| Latency p50 | 4 ms |
| Latency p99 | 6 ms |
| Latency avg / max | 3.61 ms / 81 ms |
| Bandwidth avg | 3.12 MB/s |
| Errors | _fill in_ |
| Total time | _fill in_ (about 42 s expected) |

```
┌─────────┬──────┬──────┬───────┬──────┬─────────┬─────────┬───────┐
│ Stat    │ 2.5% │ 50%  │ 97.5% │ 99%  │ Avg     │ Stdev   │ Max   │
├─────────┼──────┼──────┼───────┼──────┼─────────┼─────────┼───────┤
│ Latency │ 3 ms │ 4 ms │ 5 ms  │ 6 ms │ 3.61 ms │ 0.81 ms │ 81 ms │
└─────────┴──────┴──────┴───────┴──────┴─────────┴─────────┴───────┘
┌───────────┬────────┬─────────┬─────────┬─────────┬─────────┬──────────┬────────┐
│ Stat      │ 1%     │ 2.5%    │ 50%     │ 97.5%   │ Avg     │ Stdev    │ Min    │
├───────────┼────────┼─────────┼─────────┼─────────┼─────────┼──────────┼────────┤
│ Req/Sec   │ 7,523  │ 21,247  │ 24,511  │ 24,927  │ 23,809  │ 2,673.33 │ 7,522  │
├───────────┼────────┼─────────┼─────────┼─────────┼─────────┼──────────┼────────┤
│ Bytes/Sec │ 986 kB │ 2.78 MB │ 3.21 MB │ 3.27 MB │ 3.12 MB │ 350 kB   │ 985 kB │
└───────────┴────────┴─────────┴─────────┴─────────┴─────────┴──────────┴────────┘
```

**Notes:** stable throughput, tight latency, no heavy tail. The 7.5k minimum is likely warm-up. Suspected bottleneck: DB round trip per request with `MaxConns = 20`.

## Connection sweep (Stage 1)

| -c | req/s | p99 | Errors |
|---|---|---|---|
| 50 | _todo_ | _todo_ | _todo_ |
| 100 | 23,809 | 6 ms | _fill in_ |
| 200 | _todo_ | _todo_ | _todo_ |

## Results log

| Stage | Change | req/s | p99 | vs baseline |
|---|---|---|---|---|
| 1 | Baseline: Postgres only | 23,809 | 6 ms | 1.0x |
| 2 | pprof: identify hotspot | - | - | - |
| 3 | + Redis cache-aside | _todo_ | _todo_ | _todo_ |
| 4 | + L1 in-process cache + singleflight | _todo_ | _todo_ | _todo_ |
| 5 | + worker pool, timeouts, rate limit | _todo_ | _todo_ | _todo_ |
| 6 | Docker on AWS, single instance | _todo_ | _todo_ | _todo_ |
| 7 | 3x instances behind ALB | _todo_ | _todo_ | _todo_ |