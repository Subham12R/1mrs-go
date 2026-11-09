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

## Stage 3: + Redis cache-aside

Request path: Redis -> (miss) -> Postgres -> write back, TTL 60 s. 404s are not cached.

| Run | req/s avg | p50 | p99 | Latency avg | Max | Errors |
|---|---|---|---|---|---|---|
| `-c 100` | 30,303 | 3 ms | 5 ms | 2.75 ms | 37 ms | none printed |
| `-c 200` | 30,305 | 6 ms | 9 ms | 5.9 ms | 213 ms | _fill in_ |
| `-c 100 -w 4` | 29,413 | 3 ms | 5 ms | 2.81 ms | 42 ms | none printed |

Fair comparison with Stage 1 (both `-c 100`): **23,809 -> 30,303 req/s (+27%)**, p99 6 ms -> 5 ms, latency avg 3.61 -> 2.75 ms, max 81 -> 37 ms.

Cache hit ratio (`redis-cli info stats`): _todo_ (expect about 1M hits, about 1 miss).

**Findings**

- Throughput is flat at about 30k across 100 and 200 connections and across 1 and 4 autocannon workers. Latency only scales with connections (Little's Law: 100 / 30,303 = 3.3 ms). So the ceiling is not the DB, not concurrency, and not autocannon.
- Not the machine: `/ping` reaches 76,919 req/s on the same box (see Floor). The Redis network hop is the limiter.
- The Stage 1 baseline may have been near the same ceiling, which would explain the small +27%.
- The first one-second sample is warm-up (min req/s of 422 to 14,895 across runs).

**Next:** Stage 4 (L1 cache + singleflight), expected to approach the /ping floor.

## Floor: GET /ping (no Redis, no DB)

Measures the ceiling of Go + this machine + autocannon, so every stage can be compared to what is achievable.

**Query:** `autocannon -c 100 -a 1000000 http://localhost:8080/ping`

| Metric | Value |
|---|---|
| req/s avg | 76,919 |
| req/s median / peak | 71,615 / 102,911 |
| Latency p50 / p99 | 1 ms / 1 ms |
| Latency avg / max | 0.8 ms / 26 ms |
| Total time | 13.03 s |

The Redis hit path (30,303 req/s) reaches about 40% of this floor, so the Redis network hop (WSL/Docker) is the limiter, not the CPU (Ryzen 7 7800X3D), Go or autocannon. The Postgres baseline (23,809) is 31% of the floor.

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
| - | Floor: /ping (no Redis, no DB) | 76,919 | 1 ms | 3.23x |
| 3 | + Redis cache-aside | 30,303 | 5 ms | 1.27x |
| 4 | + L1 in-process cache + singleflight | _todo_ | _todo_ | _todo_ |
| 5 | + worker pool, timeouts, rate limit | _todo_ | _todo_ | _todo_ |
| 6 | Docker on AWS, single instance | _todo_ | _todo_ | _todo_ |
| 7 | 3x instances behind ALB | _todo_ | _todo_ | _todo_ |
