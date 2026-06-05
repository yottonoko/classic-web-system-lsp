import { execFileSync } from "node:child_process";
import { createRequire } from "node:module";
import fs from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";
import { performance } from "node:perf_hooks";
import {
  embeddedOperationNames,
  runEmbeddedOperationForParsed,
} from "./embedded-language-benchmark.mjs";
import { benchmarkSourcesForRun, readBenchmarkCacheMode } from "./benchmark-cache-mode.mjs";

const require = createRequire(import.meta.url);
const root = path.resolve(import.meta.dirname, "..");
const sampleRoot = path.join(root, "samples", "classic-asp-include-tree-benchmark");
const generator = path.join(sampleRoot, "generate.mjs");
const coreDist = path.join(root, "packages", "core", "dist", "index.js");
const benchmarkIterations = readPositiveInteger("ASP_LSP_BENCH_ITERATIONS", 5);
const warmupIterations = readNonNegativeInteger("ASP_LSP_BENCH_WARMUPS", 1);
const benchmarkCacheMode = readBenchmarkCacheMode();
const benchmarkConcurrency = readPositiveInteger("ASP_LSP_BENCH_CONCURRENCY", 4);
const collectDebugSteps = readBoolean("ASP_LSP_BENCH_DEBUG_STEPS");
const benchmarkOperationNames = new Set([
  "parseAspDocument",
  "extractAspIncludeRefs",
  "buildVirtualDocuments",
  "collectVbscriptSymbols",
  "extractVbscriptSymbolIndex",
  "analyzeVbscript",
  ...embeddedOperationNames,
]);
const benchmarkOperations = readOperationFilter(
  "ASP_LSP_BENCH_OPERATIONS",
  benchmarkOperationNames,
);
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
  extractAspIncludeRefs,
  extractVbscriptSymbolIndex,
  parseAspDocumentAsync,
} = core;

const sourceRefs = collectBenchmarkSourceRefs();
const sourceStats = summarizeSources(sourceRefs);

await runBenchmark("parseAspDocument", (run) =>
  measureAcrossSources(sourcesForRun("parseAspDocument", run), async (source) => {
    await parseAspDocumentAsync(source.uri, source.text);
  }),
);

await runBenchmark("extractAspIncludeRefs", (run) =>
  measureAcrossSources(sourcesForRun("extractAspIncludeRefs", run), async (source) => {
    extractAspIncludeRefs(source.text);
  }),
);

await runBenchmark("buildVirtualDocuments", (run) =>
  measureAcrossParsedSources(sourcesForRun("buildVirtualDocuments", run), (parsed) => {
    buildVirtualDocuments(parsed);
  }),
);

await runBenchmark("collectVbscriptSymbols", (run) =>
  measureAcrossSources(sourcesForRun("collectVbscriptSymbols", run), async (source) => {
    await collectVbscriptSymbolsFromTextAsync(source.uri, source.text);
  }),
);

await runBenchmark("extractVbscriptSymbolIndex", (run) =>
  measureAcrossSources(sourcesForRun("extractVbscriptSymbolIndex", run), async (source) => {
    extractVbscriptSymbolIndex(source.uri, source.text);
  }),
);

await runBenchmark("analyzeVbscript", (run) =>
  measureAcrossSources(sourcesForRun("analyzeVbscript", run), async (source) => {
    await analyzeVbscriptFromTextAsync(source.uri, source.text, {}, analyzeContext());
  }),
);

for (const operation of embeddedOperationNames) {
  await runBenchmark(operation, (run) =>
    measureAcrossParsedSources(sourcesForRun(operation, run), (parsed) => {
      runEmbeddedOperationForParsed(operation, parsed, core);
    }),
  );
}

console.log("");
console.log(`Include Tree Classic ASP benchmark`);
console.log(`Files: ${sourceStats.files}`);
console.log(`Lines: ${sourceStats.lines.toLocaleString("en-US")}`);
console.log(`Bytes: ${sourceStats.bytes.toLocaleString("en-US")}`);
console.log(`Cache mode: ${benchmarkCacheMode}`);
console.log(`Warmups: ${warmupIterations}`);
console.log(`Iterations: ${benchmarkIterations}`);
console.log(`Concurrency: ${benchmarkConcurrency}`);
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
  return benchmarkSourcesForRun(sourceRefs, benchmarkCacheMode, operation, run);
}

function collectBenchmarkSourceRefs() {
  return collectRelativePaths().map((relativePath) => {
    const absolutePath = path.join(sampleRoot, relativePath);
    const text = fs.readFileSync(absolutePath, "utf8");
    return {
      relativePath,
      uri: pathToFileURL(absolutePath).href,
      text,
      lines: text.split("\n").length - 1,
      bytes: Buffer.byteLength(text),
    };
  });
}

function collectRelativePaths() {
  const relativePaths = ["default.asp"];
  const stack = [path.join(sampleRoot, "includes")];
  while (stack.length > 0) {
    const directory = stack.pop();
    if (!directory || !fs.existsSync(directory)) {
      continue;
    }
    for (const entry of fs.readdirSync(directory, { withFileTypes: true })) {
      const absolutePath = path.join(directory, entry.name);
      if (entry.isDirectory()) {
        stack.push(absolutePath);
      } else if (entry.isFile() && entry.name.endsWith(".inc")) {
        relativePaths.push(path.relative(sampleRoot, absolutePath));
      }
    }
  }
  return relativePaths.sort();
}

function summarizeSources(items) {
  return items.reduce(
    (stats, item) => {
      stats.files += 1;
      stats.lines += item.lines;
      stats.bytes += item.bytes;
      return stats;
    },
    { files: 0, lines: 0, bytes: 0 },
  );
}

async function measureAcrossSources(sources, callback) {
  const start = performance.now();
  await runBounded(sources, async (source) => {
    await callback(source);
  });
  return performance.now() - start;
}

async function measureAcrossParsedSources(parsedDocuments, callback) {
  const start = performance.now();
  await runBounded(parsedDocuments, async (source) => {
    const parsed = await parseAspDocumentAsync(source.uri, source.text);
    await callback(parsed);
  });
  return performance.now() - start;
}

async function runBounded(items, callback) {
  let next = 0;
  const workers = Array.from({ length: Math.min(benchmarkConcurrency, items.length) }, async () => {
    for (;;) {
      const index = next;
      next += 1;
      if (index >= items.length) {
        return;
      }
      await callback(items[index]);
    }
  });
  await Promise.all(workers);
}

async function runBenchmark(name, fn) {
  if (benchmarkOperations && !benchmarkOperations.has(name)) {
    return;
  }
  for (let index = 0; index < warmupIterations; index += 1) {
    await fn({ phase: "warmup", index });
  }

  const samples = [];
  for (let index = 0; index < benchmarkIterations; index += 1) {
    samples.push(await fn({ phase: "measure", index }));
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

function readOperationFilter(name, knownOperations) {
  const raw = process.env[name];
  if (raw === undefined || raw.trim() === "") {
    return undefined;
  }
  const operations = raw
    .split(",")
    .map((operation) => operation.trim())
    .filter((operation) => operation.length > 0);
  const unknown = operations.filter((operation) => !knownOperations.has(operation));
  if (unknown.length > 0) {
    throw new Error(`${name} contains unknown operations: ${unknown.join(", ")}.`);
  }
  return operations.length > 0 ? new Set(operations) : undefined;
}
