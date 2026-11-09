// Run every cache mode against the same workloads and print a results table.
//
// Usage:
//   node bench-all.js [runs-per-workload=3] [total-requests=1000000] [copies=4]
//
// What it does, for each mode in db, redis, l1:
//   1. starts the server with CACHE_MODE=<mode> (fresh L1)
//   2. for each workload (skewed, uniform): flushes Redis, then runs multi.js N times
//      (run 1 starts with an empty Redis, so it is labelled "cold")
//   3. stops the server
// Then prints a table (also saved to bench-results.md) with every run and the median.
//
// Env overrides:
//   FLUSH_CMD   command that empties Redis. Default: docker exec redis redis-cli flushall
//               WSL example: wsl redis-cli flushall
//   SERVER_CMD  command that starts the server (default: go build, then run the binary)
//   MODES       comma list, default db,redis,l1
//   BASE        default http://localhost:8080
const { spawn, spawnSync } = require("child_process");
const http = require("http");
const fs = require("fs");
const path = require("path");

const runs = Number(process.argv[2]) || 3;
const total = Number(process.argv[3]) || 1000000;
const copies = Number(process.argv[4]) || 4;
const base = process.env.BASE || "http://localhost:8080";
const flushCmd = process.env.FLUSH_CMD || "docker exec redis redis-cli flushall";
const modes = (process.env.MODES || "db,redis,l1").split(",");
const workloads = ["skewed", "uniform"];

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

function ping() {
  return new Promise((resolve) => {
    http.get(base + "/ping", (res) => { res.resume(); resolve(res.statusCode === 200); }).on("error", () => resolve(false));
  });
}
async function waitReady(ms = 30000) {
  const t0 = Date.now();
  while (Date.now() - t0 < ms) { if (await ping()) return true; await sleep(300); }
  return false;
}

function buildServer() {
  if (process.env.SERVER_CMD) return null;
  const exe = process.platform === "win32" ? "bench-server.exe" : "bench-server";
  console.log("building server...");
  const b = spawnSync("go", ["build", "-o", exe, "."], { stdio: "inherit" });
  if (b.status !== 0) { console.error("go build failed"); process.exit(1); }
  return path.resolve(exe);
}

function startServer(exe, mode) {
  const env = { ...process.env, CACHE_MODE: mode };
  const p = process.env.SERVER_CMD
    ? spawn(process.env.SERVER_CMD, { env, shell: true, stdio: "ignore" })
    : spawn(exe, [], { env, stdio: "ignore" });
  return p;
}

function flush() {
  const r = spawnSync(flushCmd, { shell: true, encoding: "utf8" });
  if (r.status !== 0) console.warn(`  warning: flush failed (${flushCmd}): ${(r.stderr || "").trim()}`);
}

function runMulti(workload) {
  const r = spawnSync(process.execPath, [path.join(__dirname, "multi.js"), workload, "100", String(total), String(copies)], { encoding: "utf8" });
  const out = r.stdout || "";
  const m1 = out.match(/requests (\d+) in ([\d.]+) s\s+=>\s+(\d+) req\/s/);
  const m2 = out.match(/p50 (\d+) ms.*?p99 (\d+) ms.*?max (\d+) ms/);
  const m3 = out.match(/2xx (\d+)\s+non-2xx (\d+)\s+errors (\d+)\s+timeouts (\d+)/);
  if (!m1 || !m2 || !m3) { console.warn("  could not parse multi.js output:\n" + out + (r.stderr || "")); return null; }
  return { rps: +m1[3], secs: +m1[2], p50: +m2[1], p99: +m2[2], max: +m2[3], ok: +m3[1], bad: +m3[2] + +m3[3] + +m3[4] };
}

const median = (a) => { const s = [...a].sort((x, y) => x - y); const n = s.length; return n % 2 ? s[(n - 1) / 2] : Math.round((s[n / 2 - 1] + s[n / 2]) / 2); };
const fmt = (n) => n.toLocaleString("en-US");

(async () => {
  const exe = buildServer();
  const rows = [];
  for (const mode of modes) {
    console.log(`\n=== mode: ${mode} ===`);
    const server = startServer(exe, mode);
    if (!(await waitReady())) { console.error("server did not become ready"); server.kill(); process.exit(1); }
    for (const wl of workloads) {
      flush();
      await sleep(1000);
      const res = [];
      for (let i = 1; i <= runs; i++) {
        const r = runMulti(wl);
        if (!r) continue;
        res.push(r);
        console.log(`  ${wl} run ${i}${i === 1 ? " (cold)" : ""}: ${fmt(r.rps)} req/s  p50 ${r.p50} ms  p99 ${r.p99} ms  bad ${r.bad}`);
        await sleep(2000);
      }
      rows.push({ mode, wl, res });
    }
    server.kill();
    await sleep(2000);
  }

  let md = `| Mode | Workload | Runs (req/s) | Median req/s | Median p99 | Errors |\n|---|---|---|---|---|---|\n`;
  for (const { mode, wl, res } of rows) {
    if (!res.length) continue;
    const list = res.map((r) => fmt(r.rps)).join(" / ");
    md += `| ${mode} | ${wl} | ${list} | **${fmt(median(res.map((r) => r.rps)))}** | ${median(res.map((r) => r.p99))} ms | ${res.reduce((a, r) => a + r.bad, 0)} |\n`;
  }
  md += `\n${copies} copies x 100 connections, ${fmt(total)} requests per run, run 1 of each cell starts with an empty Redis (cold).\n`;
  fs.writeFileSync("bench-results.md", md);
  console.log("\n" + md + "\n(saved to bench-results.md)");
})();
