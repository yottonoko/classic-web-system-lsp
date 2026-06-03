import { execFileSync } from "node:child_process";
import { createRequire } from "node:module";
import fs from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";
import { performance } from "node:perf_hooks";
import {
  clearEmbeddedBenchmarkCaches,
  embeddedOperationNames,
  runEmbeddedOperation,
} from "./embedded-language-benchmark.mjs";
import {
  benchmarkSourcesForRun,
  readBenchmarkCacheMode,
  readBenchmarkDisableCaches,
} from "./benchmark-cache-mode.mjs";

const require = createRequire(import.meta.url);
const root = path.resolve(import.meta.dirname, "..");
const sampleRoot = path.join(root, "samples", "classic-asp-huge-benchmark");
const generator = path.join(sampleRoot, "generate.mjs");
const coreDist = path.join(root, "packages", "core", "dist", "index.js");
const benchmarkIterations = readPositiveInteger("ASP_LSP_BENCH_ITERATIONS", 5);
const warmupIterations = readNonNegativeInteger("ASP_LSP_BENCH_WARMUPS", 1);
const benchmarkCacheMode = readBenchmarkCacheMode();
const disableCaches = readBenchmarkDisableCaches();
const operationFilter = process.env.ASP_LSP_BENCH_OPERATION;
const collectDebugSteps = readBoolean("ASP_LSP_BENCH_DEBUG_STEPS");
const analyzeStepTotals = new Map();
const results = [];

if (!fs.existsSync(coreDist)) {
  throw new Error(
    "packages/core/dist/index.js is missing. Run `pnpm --filter @asp-lsp/core run build`.",
  );
}

execFileSync(process.execPath, [generator], { stdio: "inherit" });

const core = require(coreDist);
const {
  analyzeVbscriptFromTextAsync,
  buildVirtualDocuments,
  collectVbscriptSymbolsFromTextAsync,
  parseAspDocumentAsync,
} = core;

const sources = collectBenchmarkSources();
const sourceStats = summarizeSources(sources);

if (shouldRunOperation("parseAspDocument")) {
  await runBenchmark("parseAspDocument", async (run) => {
    for (const source of sourcesForRun("parseAspDocument", run)) {
      await runSourceBenchmark(() => parseAspDocumentAsync(source.uri, source.text));
    }
  });
}

if (shouldRunOperation("buildVirtualDocuments")) {
  await runBenchmark("buildVirtualDocuments", async (run) => {
    for (const source of sourcesForRun("buildVirtualDocuments", run)) {
      await runSourceBenchmark(async () => {
        const parsed = await parseAspDocumentAsync(source.uri, source.text);
        buildVirtualDocuments(parsed);
      });
    }
  });
}

if (shouldRunOperation("collectVbscriptSymbols")) {
  await runBenchmark("collectVbscriptSymbols", async (run) => {
    for (const source of sourcesForRun("collectVbscriptSymbols", run)) {
      await runSourceBenchmark(() => collectVbscriptSymbolsFromTextAsync(source.uri, source.text));
    }
  });
}

if (shouldRunOperation("analyzeVbscript")) {
  await runBenchmark("analyzeVbscript", async (run) => {
    for (const source of sourcesForRun("analyzeVbscript", run)) {
      await runSourceBenchmark(() =>
        analyzeVbscriptFromTextAsync(source.uri, source.text, {}, analyzeContext()),
      );
    }
  });
}

for (const operation of embeddedOperationNames) {
  if (!shouldRunOperation(operation)) {
    continue;
  }
  await runBenchmark(operation, async (run) => {
    for (const source of sourcesForRun(operation, run)) {
      await runSourceBenchmark(() => runEmbeddedOperation(operation, source, core));
    }
  });
}

console.log("");
console.log(`Huge Classic ASP benchmark`);
console.log(`Files: ${sourceStats.files}`);
console.log(`Lines: ${sourceStats.lines.toLocaleString("en-US")}`);
console.log(`Bytes: ${sourceStats.bytes.toLocaleString("en-US")}`);
console.log(`Cache mode: ${benchmarkCacheMode}`);
console.log(`Benchmark caches: ${disableCaches ? "disabled" : "enabled"}`);
console.log(`Warmups: ${warmupIterations}`);
console.log(`Iterations: ${benchmarkIterations}`);
if (operationFilter) {
  console.log(`Operation filter: ${operationFilter}`);
}
console.log("");
printTable(results);
if (collectDebugSteps) {
  console.log("");
  console.log("analyzeVbscript debug step totals");
  console.log(
    `Measured calls include ${warmupIterations} warmup and ${benchmarkIterations} benchmark iterations.`,
  );
  console.log("");
  printDebugStepTotals(analyzeStepTotals);
}

function sourcesForRun(operation, run) {
  return benchmarkSourcesForRun(sources, benchmarkCacheMode, operation, run);
}

function shouldRunOperation(operation) {
  return !operationFilter || operationFilter === operation;
}

async function runSourceBenchmark(action) {
  if (!disableCaches) {
    return action();
  }
  clearBenchmarkCaches();
  try {
    return await action();
  } finally {
    clearBenchmarkCaches();
  }
}

function clearBenchmarkCaches() {
  core.clearAspCoreCaches?.();
  clearEmbeddedBenchmarkCaches();
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

function analyzeContext() {
  if (!collectDebugSteps) {
    return {};
  }
  return {
    debugStep(name, action) {
      const start = performance.now();
      const value = action();
      analyzeStepTotals.set(name, (analyzeStepTotals.get(name) ?? 0) + performance.now() - start);
      return value;
    },
  };
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

function formatMillis(value) {
  return value.toFixed(2);
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
