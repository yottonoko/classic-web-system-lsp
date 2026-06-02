import { execFileSync } from "node:child_process";
import { createRequire } from "node:module";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { pathToFileURL } from "node:url";
import { performance } from "node:perf_hooks";
import { Worker } from "node:worker_threads";
import { embeddedOperationNames } from "./embedded-language-benchmark.mjs";
import { benchmarkSourcesForRun, readBenchmarkCacheMode } from "./benchmark-cache-mode.mjs";

const require = createRequire(import.meta.url);
const root = path.resolve(import.meta.dirname, "..");
const sampleRoot = path.join(root, "samples", "classic-asp-large-benchmark");
const generator = path.join(sampleRoot, "generate.mjs");
const coreDist = path.join(root, "packages", "core", "dist", "index.js");
const benchmarkIterations = readPositiveInteger("ASP_LSP_BENCH_ITERATIONS", 5);
const warmupIterations = readNonNegativeInteger("ASP_LSP_BENCH_WARMUPS", 1);
const benchmarkCacheMode = readBenchmarkCacheMode();
const collectDebugSteps = readBoolean("ASP_LSP_BENCH_DEBUG_STEPS");
const workerCount = readPositiveInteger(
  "ASP_LSP_BENCH_WORKERS",
  Math.max(1, Math.min(4, os.availableParallelism() - 1)),
);
const analyzeStepTotals = new Map();
const workerLatencySamples = new Map();
const results = [];

async function main() {
  if (!fs.existsSync(coreDist)) {
    throw new Error(
      "packages/core/dist/index.js is missing. Run `pnpm --filter @asp-lsp/core run build`.",
    );
  }

  execFileSync(process.execPath, [generator], { stdio: "inherit" });

  const sources = collectBenchmarkSources();
  const sourceStats = summarizeSources(sources);
  const sourcesForRun = (operation, run) =>
    benchmarkSourcesForRun(sources, benchmarkCacheMode, operation, run);
  const workerPool = new BenchmarkWorkerPool(workerCount);
  try {
    await runBenchmark("parseAspDocument", (run) =>
      runParallelOperation(workerPool, "parseAspDocument", sourcesForRun("parseAspDocument", run)),
    );
    await runBenchmark("buildVirtualDocuments", (run) =>
      runParallelOperation(
        workerPool,
        "buildVirtualDocuments",
        sourcesForRun("buildVirtualDocuments", run),
      ),
    );
    await runBenchmark("collectVbscriptSymbols", (run) =>
      runParallelOperation(
        workerPool,
        "collectVbscriptSymbols",
        sourcesForRun("collectVbscriptSymbols", run),
      ),
    );
    await runBenchmark("analyzeVbscript", (run) =>
      runParallelOperation(workerPool, "analyzeVbscript", sourcesForRun("analyzeVbscript", run)),
    );
    for (const operation of embeddedOperationNames) {
      await runBenchmark(operation, (run) =>
        runParallelOperation(workerPool, operation, sourcesForRun(operation, run)),
      );
    }
  } finally {
    await workerPool.close();
  }

  console.log("");
  console.log(`Large Classic ASP benchmark`);
  console.log(`Files: ${sourceStats.files}`);
  console.log(`Lines: ${sourceStats.lines.toLocaleString("en-US")}`);
  console.log(`Bytes: ${sourceStats.bytes.toLocaleString("en-US")}`);
  console.log(`Cache mode: ${benchmarkCacheMode}`);
  console.log(`Warmups: ${warmupIterations}`);
  console.log(`Iterations: ${benchmarkIterations}`);
  console.log(`Workers: ${workerCount}`);
  console.log("");
  printTable(results);
  if (collectDebugSteps) {
    console.log("");
    console.log("worker latency summary");
    console.log(
      `Measured calls include ${warmupIterations} warmup and ${benchmarkIterations} benchmark iterations.`,
    );
    console.log("");
    printWorkerLatencySummary(workerLatencySamples);
    console.log("");
    console.log("analyzeVbscript debug step totals");
    console.log(
      `Measured calls include ${warmupIterations} warmup and ${benchmarkIterations} benchmark iterations.`,
    );
    console.log("");
    printDebugStepTotals(analyzeStepTotals);
  }
}

function collectBenchmarkSources() {
  const relativePaths = [
    "default.asp",
    "includes/layer1.inc",
    "includes/layer2.inc",
    "includes/layer3.inc",
    "includes/layer4.inc",
    ...fs
      .readdirSync(path.join(sampleRoot, "includes", "generated"))
      .filter((entry) => entry.endsWith(".inc"))
      .sort()
      .map((entry) => path.join("includes", "generated", entry)),
  ];
  return relativePaths.map((relativePath) => {
    const absolutePath = path.join(sampleRoot, relativePath);
    return {
      relativePath,
      uri: pathToFileURL(absolutePath).href,
      text: fs.readFileSync(absolutePath, "utf8"),
    };
  });
}

function summarizeSources(items) {
  return items.reduce(
    (stats, item) => {
      stats.files += 1;
      stats.lines += item.text.split("\n").length - 1;
      stats.bytes += Buffer.byteLength(item.text);
      return stats;
    },
    { files: 0, lines: 0, bytes: 0 },
  );
}

async function runParallelOperation(pool, operation, inputs) {
  const messages =
    operation === "parseAspDocument"
      ? inputs.map((source) => ({ operation, source }))
      : inputs.map((source) => ({ operation, source, debugSteps: collectDebugSteps }));
  const outputs = await pool.runAll(messages);
  recordWorkerLatencySamples(operation, outputs);
  if (operation === "analyzeVbscript" && collectDebugSteps) {
    for (const output of outputs) {
      for (const [name, elapsed] of output.timings ?? []) {
        analyzeStepTotals.set(name, (analyzeStepTotals.get(name) ?? 0) + elapsed);
      }
    }
  }
}

function recordWorkerLatencySamples(operation, outputs) {
  const samples = workerLatencySamples.get(operation) ?? [];
  for (const output of outputs) {
    if (
      typeof output.elapsedMs === "number" &&
      typeof output.roundTripMs === "number" &&
      typeof output.payloadBytes === "number"
    ) {
      samples.push({
        elapsedMs: output.elapsedMs,
        roundTripMs: output.roundTripMs,
        payloadBytes: output.payloadBytes,
        overheadMs: Math.max(0, output.roundTripMs - output.elapsedMs),
      });
    }
  }
  workerLatencySamples.set(operation, samples);
}

async function runBenchmark(name, fn) {
  for (let index = 0; index < warmupIterations; index += 1) {
    await fn({ phase: "warmup", index });
  }

  const samples = [];
  for (let index = 0; index < benchmarkIterations; index += 1) {
    const start = performance.now();
    await fn({ phase: "measure", index });
    samples.push(performance.now() - start);
  }

  samples.sort((left, right) => left - right);
  const total = samples.reduce((sum, value) => sum + value, 0);
  results.push({
    name,
    min: samples[0],
    median: samples[Math.floor(samples.length / 2)],
    mean: total / samples.length,
    max: samples[samples.length - 1],
  });
}

function printTable(items) {
  const rows = [
    ["Operation", "min ms", "median ms", "mean ms", "max ms"],
    ...items.map((item) => [
      item.name,
      formatMillis(item.min),
      formatMillis(item.median),
      formatMillis(item.mean),
      formatMillis(item.max),
    ]),
  ];
  const widths = rows[0].map((_, column) => Math.max(...rows.map((row) => row[column].length)));
  for (const [index, row] of rows.entries()) {
    console.log(row.map((value, column) => value.padEnd(widths[column])).join("  "));
    if (index === 0) {
      console.log(widths.map((width) => "-".repeat(width)).join("  "));
    }
  }
}

function printDebugStepTotals(totals) {
  const rows = [
    ["Step", "total ms"],
    ...[...totals.entries()]
      .sort((left, right) => right[1] - left[1])
      .map(([name, total]) => [name, formatMillis(total)]),
  ];
  const widths = rows[0].map((_, column) => Math.max(...rows.map((row) => row[column].length)));
  for (const [index, row] of rows.entries()) {
    console.log(row.map((value, column) => value.padEnd(widths[column])).join("  "));
    if (index === 0) {
      console.log(widths.map((width) => "-".repeat(width)).join("  "));
    }
  }
}

function printWorkerLatencySummary(samplesByOperation) {
  const rows = [
    [
      "Operation",
      "calls",
      "payload mean bytes",
      "run mean ms",
      "round-trip mean ms",
      "overhead mean ms",
    ],
  ];
  for (const [operation, samples] of samplesByOperation) {
    rows.push([
      operation,
      String(samples.length),
      formatNumber(mean(samples.map((sample) => sample.payloadBytes))),
      formatMillis(mean(samples.map((sample) => sample.elapsedMs))),
      formatMillis(mean(samples.map((sample) => sample.roundTripMs))),
      formatMillis(mean(samples.map((sample) => sample.overheadMs))),
    ]);
  }
  const widths = rows[0].map((_, column) => Math.max(...rows.map((row) => row[column].length)));
  for (const [index, row] of rows.entries()) {
    console.log(row.map((value, column) => value.padEnd(widths[column])).join("  "));
    if (index === 0) {
      console.log(widths.map((width) => "-".repeat(width)).join("  "));
    }
  }
}

function mean(samples) {
  return samples.reduce((sum, value) => sum + value, 0) / samples.length;
}

function formatMillis(value) {
  return value.toFixed(2);
}

function formatNumber(value) {
  return value.toFixed(0);
}

function readPositiveInteger(name, fallback) {
  const raw = process.env[name];
  if (raw === undefined || raw === "") {
    return fallback;
  }
  const value = Number(raw);
  if (!Number.isInteger(value) || value <= 0) {
    throw new Error(`${name} must be a positive integer.`);
  }
  return value;
}

function readNonNegativeInteger(name, fallback) {
  const raw = process.env[name];
  if (raw === undefined || raw === "") {
    return fallback;
  }
  const value = Number(raw);
  if (!Number.isInteger(value) || value < 0) {
    throw new Error(`${name} must be a non-negative integer.`);
  }
  return value;
}

function readBoolean(name) {
  const raw = process.env[name];
  if (raw === undefined || raw === "" || raw === "0" || raw.toLowerCase() === "false") {
    return false;
  }
  if (raw === "1" || raw.toLowerCase() === "true") {
    return true;
  }
  throw new Error(`${name} must be 1, 0, true, or false.`);
}

class BenchmarkWorkerPool {
  constructor(count) {
    this.workers = Array.from(
      { length: count },
      () => new Worker(path.join(root, "scripts", "benchmark-large-worker.mjs")),
    );
    this.nextId = 0;
    this.cursor = 0;
    this.pending = new Map();
    for (const worker of this.workers) {
      worker.on("message", (message) => this.handleMessage(message));
      worker.on("error", (error) => this.rejectAll(error));
      worker.on("exit", (code) => {
        if (code !== 0) {
          this.rejectAll(new Error(`Benchmark worker exited with code ${code}`));
        }
      });
    }
  }

  runAll(messages) {
    return Promise.all(messages.map((message) => this.run(message)));
  }

  run(message) {
    const id = ++this.nextId;
    const worker = this.workers[this.cursor % this.workers.length];
    this.cursor += 1;
    const payloadBytes = Buffer.byteLength(JSON.stringify(message), "utf8");
    const postedAt = performance.now();
    return new Promise((resolve, reject) => {
      this.pending.set(id, { resolve, reject, payloadBytes, postedAt });
      worker.postMessage({ id, ...message });
    });
  }

  handleMessage(message) {
    const pending = this.pending.get(message.id);
    if (!pending) {
      return;
    }
    this.pending.delete(message.id);
    if (message.error) {
      pending.reject(new Error(message.error));
      return;
    }
    message.roundTripMs = performance.now() - pending.postedAt;
    message.payloadBytes = pending.payloadBytes;
    pending.resolve(message);
  }

  rejectAll(error) {
    for (const pending of this.pending.values()) {
      pending.reject(error);
    }
    this.pending.clear();
  }

  async close() {
    await Promise.all(this.workers.map((worker) => worker.terminate()));
  }
}

await main();
