// Load test with random ids so the cache hit ratio is realistic.
//
// Usage:
//   node random.js [skewed|uniform] [connections] [requests]
//   N=10000 node random.js skewed 100 1000000
//
// skewed  : hot head, like real traffic (~46% of requests hit the first 10% of ids)
// uniform : every id equally likely (worst case for caches)
//
// One copy is single-threaded and tops out around 40-50k req/s because every
// request gets a fresh path. For the server's real ceiling use multi.js.
// Set JSON=1 to print one JSON line (used by multi.js).
const autocannon = require("autocannon");

const mode = process.argv[2] || "skewed";
const connections = Number(process.argv[3]) || 100;
const amount = Number(process.argv[4]) || 1000000;
const N = Number(process.env.N) || 10000;
const base = process.env.BASE || "http://localhost:8080";
const asJson = process.env.JSON === "1";

function pickId() {
  if (mode === "uniform") return 1 + Math.floor(Math.random() * N);
  return 1 + Math.floor(N * Math.pow(Math.random(), 3));
}

const inst = autocannon(
  {
    url: base,
    connections,
    amount,
    requests: [
      {
        method: "GET",
        setupRequest: (req) => {
          req.path = "/items/" + pickId();
          return req;
        },
      },
    ],
  },
  (err, res) => {
    if (err) throw err;
    if (asJson) {
      console.log(JSON.stringify({
        total: res.requests.total,
        duration: res.duration,
        avg: res.requests.average,
        median: res.requests.p50,
        p50: res.latency.p50,
        p99: res.latency.p99,
        max: res.latency.max,
        ok2xx: res["2xx"],
        non2xx: res.non2xx,
        errors: res.errors,
        timeouts: res.timeouts,
      }));
      return;
    }
    console.log(`\nmode=${mode} N=${N} connections=${connections}`);
    console.log(`req/s avg ${res.requests.average}  median ${res.requests.p50}`);
    console.log(`latency p50 ${res.latency.p50} ms  p99 ${res.latency.p99} ms  max ${res.latency.max} ms`);
    console.log(`2xx ${res["2xx"]}  non-2xx ${res.non2xx}  errors ${res.errors}  timeouts ${res.timeouts}`);
  }
);
if (!asJson) autocannon.track(inst, { renderProgressBar: true });
