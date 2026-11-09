// Run several random.js copies in parallel and combine the results.
//
// Usage:
//   node multi.js [skewed|uniform] [connections-per-copy] [total-requests] [copies]
//   node multi.js skewed 100 1000000 4
//
// Combined req/s = total requests / duration of the slowest copy (the copies overlap).
const { spawn } = require("child_process");
const path = require("path");

const mode = process.argv[2] || "skewed";
const conns = Number(process.argv[3]) || 100;
const total = Number(process.argv[4]) || 1000000;
const copies = Number(process.argv[5]) || 4;
const each = Math.floor(total / copies);

const results = [];
let finished = 0;

console.log(`mode=${mode} copies=${copies} connections=${conns * copies} (${conns} each) requests=${each * copies}`);

for (let i = 0; i < copies; i++) {
  const p = spawn(process.execPath, [path.join(__dirname, "random.js"), mode, String(conns), String(each)], {
    env: { ...process.env, JSON: "1" },
  });
  let out = "";
  p.stdout.on("data", (d) => (out += d));
  p.stderr.on("data", (d) => process.stderr.write(d));
  p.on("exit", () => {
    try {
      results.push(JSON.parse(out.trim().split("\n").pop()));
    } catch (e) {
      console.error("copy", i, "gave no result:", out);
    }
    if (++finished === copies) report();
  });
}

function report() {
  if (results.length === 0) process.exit(1);
  const sum = (k) => results.reduce((a, r) => a + r[k], 0);
  const maxDur = Math.max(...results.map((r) => r.duration));
  const totalReq = sum("total");
  console.log("\n--- combined ---");
  console.log(`requests ${totalReq} in ${maxDur.toFixed(2)} s  =>  ${Math.round(totalReq / maxDur)} req/s`);
  console.log(`latency p50 ${Math.max(...results.map((r) => r.p50))} ms (worst copy)  p99 ${Math.max(...results.map((r) => r.p99))} ms (worst copy)  max ${Math.max(...results.map((r) => r.max))} ms`);
  console.log(`2xx ${sum("ok2xx")}  non-2xx ${sum("non2xx")}  errors ${sum("errors")}  timeouts ${sum("timeouts")}`);
}