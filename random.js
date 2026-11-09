// Load test with random ids so the cache hit ratio is realistic.
//
// Usage:
//   node bench/random.js [skewed|uniform] [connections] [requests]
//   N=10000 node bench/random.js skewed 100 1000000
//
// skewed  : hot head, like real traffic (~46% of requests hit the first 10% of ids)
// uniform : every id equally likely (worst case for caches)
//
// Single-threaded (about 100k req/s max). To push harder, run 2-4 copies in
// parallel terminals with a smaller request count each.
const autocannon = require("autocannon");

const mode = process.argv[2] || "skewed";
const connections = Number(process.argv[3]) || 100;
const amount = Number(process.argv[4]) || 1000000;
const N = Number(process.env.N) || 10000;
const base = process.env.BASE || "http://localhost:8080";

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
    console.log(`\nmode=${mode} N=${N} connections=${connections}`);
    console.log(`req/s avg ${res.requests.average}  median ${res.requests.p50}`);
    console.log(`latency p50 ${res.latency.p50} ms  p99 ${res.latency.p99} ms  max ${res.latency.max} ms`);
    console.log(`2xx ${res["2xx"]}  non-2xx ${res.non2xx}  errors ${res.errors}  timeouts ${res.timeouts}`);
  }
);
autocannon.track(inst, { renderProgressBar: true });