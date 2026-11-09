# Scaling a Go API from 20k to 142k req/s: what each layer actually bought

I'm new to Go. I wanted to learn how a real API scales, so I built the smallest one I could (one endpoint, one Postgres table) and made it faster one layer at a time, measuring every step with [autocannon](https://github.com/mcollina/autocannon). No guessing, no "should be faster". Every number below comes from a command in this repo, and the raw outputs are in [EVALS.md](EVALS.md).

## TL;DR

Same server, same machine, 10,000 items, random ids, 400 concurrent connections, 1,000,000 requests per run, median of 3 runs:

| Stage | What serves the request | req/s | p99 | vs Postgres |
|---|---|---|---|---|
| 1 | Postgres only | 20,392 | 28 ms | 1.0x |
| 2 | + Redis cache | 28,539 | 26 ms | 1.4x |
| 3 | + in-process L1 cache + singleflight | **142,045** | **6 ms** | **7.0x** |

```mermaid
%%{init: {"themeVariables": {"xyChart": {"plotColorPalette": "#2f9e44"}}}}%%
xychart-beta
    title "Realistic workload (skewed random ids): median req/s"
    x-axis ["Postgres", "+ Redis", "+ L1 + singleflight"]
    y-axis "req/s" 0 --> 160000
    bar [20392, 28539, 142045]
```

The surprise: Redis, the thing everyone reaches for, bought only 1.4x. The big win was skipping the network entirely.

## The setup

One endpoint: `GET /items/{id}` returns `{"id":1,"name":"test"}`. The server is plain `net/http` (Go 1.22 routing) with a `pgxpool` of 20 connections in front of Postgres.

![Baseline architecture](docs/architecture-baseline.png)

Go gives you "multithreading" for free here: `net/http` runs one goroutine per request, and the runtime spreads them across cores. The real limits turned out to be the database pool and the network hops, not Go.

## Stage 1: the baseline

Every request runs `SELECT id, name FROM items WHERE id=$1`. I started with a single hot item, `/items/1`, and 100 connections:

```
autocannon -c 100 -a 1000000 http://localhost:8080/items/1
```

**23,809 req/s, p50 4 ms, p99 6 ms.**

Why that number? 100 clients share a pool of 20 database connections, so about 80 requests are always waiting. Little's Law agrees: 100 connections / 23,809 req/s is about 4.2 ms, close to the measured 3.6 ms average.

## Stage 2: add Redis

Cache-aside: check Redis, on a miss read Postgres and write the result back with a 60 s TTL. I expected a big jump. I got **30,303 req/s (+27%)**.

Two odd things in the data. Throughput was the same at 100 and at 200 connections, and the same with 1 or 4 load-tester threads. Only latency changed. A flat ceiling like that means one fixed cost per request.

## The detour that mattered: measuring the floor

Before blaming Redis, I needed to know what this machine could do at all. I added a `/ping` route that touches nothing:

**76,919 req/s.**

So the machine, Go and autocannon were fine, and the Redis path reached only about 40% of that. The fixed cost was the network round trip to Redis (it runs behind Docker/WSL on my machine), paid on every single request. Measuring a floor first turned "Redis is disappointing" into "the hop is the problem".

## Stage 3: keep hot items in the server's own memory

Add a small in-process cache (L1, 5 s TTL) in front of Redis, and wrap the miss path in `singleflight` so a thousand simultaneous misses for one key become one Redis/DB call:

```go
// L1: memory, no network
if b, ok := s.l1.get(key); ok {
    w.Write(b)
    return
}

// L2 + DB, collapsed: many simultaneous misses -> 1 call
v, err, _ := s.sf.Do(key, func() (any, error) {
    ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 2*time.Second)
    defer cancel()
    // try Redis, fall back to Postgres, write back to Redis
    ...
})
s.l1.set(key, res.b, l1TTL)
```

![Final architecture](docs/architecture-final.png)

Single hot item: **90,921 req/s**, and **200,002 req/s** once the load tester had 4 worker threads. A cache hit is now a map lookup. There is no network at all.

## "But that's one key"

A single hot key is the best case for any cache. So I seeded 10,000 rows (`seed.sql`) and wrote a load script that requests random ids, in two shapes:

- **skewed:** about 46% of requests hit the first 10% of ids, like real traffic with a few popular items
- **uniform:** every id equally likely

To compare stages fairly I added `CACHE_MODE=db|redis|l1`, so one binary runs all three, and `bench-all.js`, which runs every mode on both workloads, 3 runs each, and writes the table. That's the TL;DR table at the top. All 18 runs finished with 0 errors.

| Mode | skewed req/s | uniform req/s | p99 (skewed) |
|---|---|---|---|
| db | 20,392 | 20,396 | 28 ms |
| redis | 28,539 | 27,755 | 26 ms |
| l1 | 142,045 | 142,045 | 6 ms |

## What I got wrong along the way

- **I assumed the database was the bottleneck.** Remove it with Redis and throughput barely moved. The cost was the network hop.
- **I blamed the cache for a slowdown that was the tester.** One copy of my random-id load script maxed out at 40-50k req/s. Four copies in parallel hit 142k. The load generator is part of the system; check it before blaming the server.
- **I predicted uniform would be slower than skewed under L1.** It wasn't. At 142k req/s over only 10,000 ids, each id is requested about 14 times a second, so even a 5 s TTL leaves few misses. My keyspace was too small to stress the cache.
- **One benchmark run reported 997k failed requests out of 1M.** I never found the cause, because my script hid the server's output. It now saves a server log per mode, refuses to start if the port is already in use, and aborts any run with more than 1% failures.
- **Autocannon times runs in whole seconds.** A 7-second run therefore has about 12% granularity, which is why results repeat (124,378 / 142,045). Use more requests when the server is this fast.

## What I'd tell past me

1. Measure a floor (`/ping`) before optimizing anything.
2. Check the load tester before blaming the server.
3. Report req/s, p99 and error count together, from at least 3 runs, and say which were cold.
4. Skipping a network hop beats making it faster.
5. One hot key flatters caches. Use random ids.

## Honest caveats

- Everything runs on one machine (Ryzen 7 7800X3D, Windows): server, Postgres, Redis and four load-tester processes share the CPU. Treat the numbers as comparisons between stages, not production capacity.
- 10,000 ids is a small keyspace. A million-id dataset would show real L1/Redis miss behavior.
- The L1 map has no size limit or eviction. Fine here, not in production (use `ristretto` or `otter`).
- I did not measure actual cache hit ratios; the "mostly hits" claims are reasoning, not data.

## Run it yourself

```powershell
# 1. Postgres + Redis running, then seed 10,000 rows
psql -U postgres -d items_db -f seed.sql

# 2. .env
#    DATABASE_URL=postgres://postgres:pass@localhost:5432/items_db
#    REDIS_URL=redis://localhost:6379/0

# 3. Run the server in a mode: db | redis | l1 (default)
$env:CACHE_MODE = "l1"
go run .

# 4. Load test
npm install
node multi.js skewed 100 1000000 4      # 4 parallel copies, combined result
node bench-all.js                       # every mode, both workloads, 3 runs each
```

`bench-all.js` starts and stops the server itself, so stop any running server first. If Redis isn't in a Docker container named `redis`, set `FLUSH_CMD` (for example `wsl redis-cli flushall`).

## Repo map

| File | What it is |
|---|---|
| `main.go` | Server, L1 cache, singleflight, mode switch |
| `modes.go` | The `db` and `redis` handlers, kept for comparison |
| `seed.sql` | 10,000 rows |
| `random.js` | Random-id load generator (skewed or uniform) |
| `multi.js` | Runs several copies in parallel and combines the results |
| `bench-all.js` | Runs every mode and workload, writes `bench-results.md` |
| `EVALS.md` | Every result, with raw autocannon output |

## Next

1. Seed 1M rows and re-run uniform to see real misses and why the L1 needs eviction.
2. Docker, then AWS: ALB, several instances, ElastiCache Redis, RDS Postgres, with the load generator on a separate instance.
3. The actual goal: 1M requests per second, which on this evidence means many instances behind a load balancer.
