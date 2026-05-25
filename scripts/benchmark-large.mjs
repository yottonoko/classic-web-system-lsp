import { execFileSync } from "node:child_process";
import { createRequire } from "node:module";
import fs from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";
import { performance } from "node:perf_hooks";

const require = createRequire(import.meta.url);
const root = path.resolve(import.meta.dirname, "..");
const sampleRoot = path.join(root, "samples", "classic-asp-large-benchmark");
const generator = path.join(sampleRoot, "generate.mjs");
const coreDist = path.join(root, "packages", "core", "dist", "index.js");
const benchmarkIterations = readPositiveInteger("ASP_LSP_BENCH_ITERATIONS", 5);
const warmupIterations = readPositiveInteger("ASP_LSP_BENCH_WARMUPS", 1);
const results = [];

if (!fs.existsSync(coreDist)) {
  throw new Error(
    "packages/core/dist/index.js is missing. Run `pnpm --filter @asp-lsp/core run build`.",
  );
}

execFileSync(process.execPath, [generator], { stdio: "inherit" });

const {
  analyzeVbscript,
  buildVirtualDocuments,
  collectVbscriptSymbols,
  parseAspDocument,
} = require(coreDist);

const sources = collectBenchmarkSources();
const sourceStats = summarizeSources(sources);

runBenchmark("parseAspDocument", () => {
  for (const source of sources) {
    parseAspDocument(source.uri, source.text);
  }
});

const parsedDocuments = sources.map((source) => parseAspDocument(source.uri, source.text));
runBenchmark("buildVirtualDocuments", () => {
  for (const parsed of parsedDocuments) {
    buildVirtualDocuments(parsed);
  }
});

runBenchmark("collectVbscriptSymbols", () => {
  for (const parsed of parsedDocuments) {
    collectVbscriptSymbols(parsed);
  }
});

runBenchmark("analyzeVbscript", () => {
  for (const parsed of parsedDocuments) {
    analyzeVbscript(parsed);
  }
});

console.log("");
console.log(`Large Classic ASP benchmark`);
console.log(`Files: ${sourceStats.files}`);
console.log(`Lines: ${sourceStats.lines.toLocaleString("en-US")}`);
console.log(`Bytes: ${sourceStats.bytes.toLocaleString("en-US")}`);
console.log(`Warmups: ${warmupIterations}`);
console.log(`Iterations: ${benchmarkIterations}`);
console.log("");
printTable(results);

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

function runBenchmark(name, fn) {
  for (let index = 0; index < warmupIterations; index += 1) {
    fn();
  }

  const samples = [];
  for (let index = 0; index < benchmarkIterations; index += 1) {
    const start = performance.now();
    fn();
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
