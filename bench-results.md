| Mode | Workload | Runs (req/s) | Median req/s | Median p99 | Errors |
|---|---|---|---|---|---|
| db | skewed | 20,392 / 20,816 / 19,220 | **20,392** | 28 ms | 0 |
| db | uniform | 21,263 / 20,396 / 20,392 | **20,396** | 25 ms | 0 |
| redis | skewed | 27,755 / 28,539 / 28,539 | **28,539** | 26 ms | 0 |
| redis | uniform | 27,747 / 27,755 / 28,547 | **27,755** | 28 ms | 0 |
| l1 | skewed | 124,378 / 142,045 / 142,045 | **142,045** | 6 ms | 0 |
| l1 | uniform | 124,378 / 142,045 / 142,045 | **142,045** | 6 ms | 0 |

4 copies x 100 connections, 1,000,000 requests per run, run 1 of each cell starts with an empty Redis (cold).
