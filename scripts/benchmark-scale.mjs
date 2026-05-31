import { createRequire } from "node:module";
import { execFileSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { pathToFileURL } from "node:url";
import { performance } from "node:perf_hooks";
import { benchmarkSourcesForRun, readBenchmarkCacheMode } from "./benchmark-cache-mode.mjs";
import {
  clearEmbeddedBenchmarkCaches,
  embeddedOperationNames,
  prewarmEmbeddedBenchmarkServices,
  runEmbeddedOperation,
  summarizeSources,
} from "./embedded-language-benchmark.mjs";

const root = path.resolve(import.meta.dirname, "..");
const coreDist = path.join(root, "packages", "core", "dist", "index.js");
const reportsRoot = path.join(root, "reports");
const nativeBinary = path.join(
  root,
  "target",
  "release",
  process.platform === "win32" ? "asp-lsp-core.exe" : "asp-lsp-core",
);
const allMultipliers = [1, 2, 4, 8];
const baseTargetLines = 10_000;
const extraIncludeCount = 3;
const benchmarkIterations = readPositiveInteger("ASP_LSP_BENCH_ITERATIONS", 1);
const benchmarkConcurrency = readPositiveInteger("ASP_LSP_BENCH_CONCURRENCY", 1);
const allOperationNames = [
  "collectVbscriptSymbols",
  "analyzeVbscript",
  "parseAspDocument",
  "buildVirtualDocuments",
  ...embeddedOperationNames,
];
const multipliers = selectedMultipliers();
const operationNames = selectedOperationNames();
const mainFiles = [
  {
    file: "default.asp",
    layer: 0,
    role: "entry",
    chainInclude: "includes/layer1.inc",
    extraPrefix: "default",
  },
  {
    file: "includes/layer1.inc",
    layer: 1,
    role: "include",
    chainInclude: "layer2.inc",
    extraPrefix: "layer1",
  },
  {
    file: "includes/layer2.inc",
    layer: 2,
    role: "include",
    chainInclude: "layer3.inc",
    extraPrefix: "layer2",
  },
  {
    file: "includes/layer3.inc",
    layer: 3,
    role: "include",
    chainInclude: "layer4.inc",
    extraPrefix: "layer3",
  },
  {
    file: "includes/layer4.inc",
    layer: 4,
    role: "leaf",
    chainInclude: null,
    extraPrefix: "layer4",
  },
];

async function main() {
  if (!fs.existsSync(coreDist)) {
    throw new Error(
      "packages/core/dist/index.js is missing. Run `pnpm --filter @asp-lsp/core run build`.",
    );
  }

  const tempRoot = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-scale-benchmark-"));
  const core = createRequire(import.meta.url)(coreDist);
  const backends = selectedBackends();
  const cacheModes = selectedCacheModes();
  const records = [];
  const summaries = [];
  prewarmEmbeddedBenchmarkServices();

  try {
    for (const cacheMode of cacheModes) {
      const warmupIterations = warmupsForCacheMode(cacheMode);
      for (const multiplier of multipliers) {
        for (const backend of backends) {
          const sampleRoot = path.join(tempRoot, `${backend.id}-${cacheMode}-${multiplier}x`);
          generateScaleSample(sampleRoot, multiplier);
          const sources = collectScaleSources(sampleRoot);
          const sourceStats = summarizeSources(sources);
          const results = [];
          console.log(
            `[run] ${backend.label} ${cacheMode} ${multiplier}x (${sourceStats.lines.toLocaleString("en-US")} lines)`,
          );

          await withBackendEnvironment(backend, async () => {
            for (const operation of operationNames) {
              clearEmbeddedBenchmarkCaches();
              try {
                await runBenchmark(results, operation, warmupIterations, async (run) =>
                  measureAcrossSources(
                    benchmarkSourcesForRun(sources, cacheMode, operation, run),
                    benchmarkConcurrency,
                    async (source) => {
                      await runOperation(core, operation, source);
                    },
                  ),
                );
              } finally {
                clearEmbeddedBenchmarkCaches();
              }
            }
          });

          const common = {
            backend: backend.label,
            backendId: backend.id,
            cacheMode,
            scale: `${multiplier}x`,
            multiplier,
            files: sourceStats.files,
            lines: sourceStats.lines,
            bytes: sourceStats.bytes,
            warmups: warmupIterations,
            iterations: benchmarkIterations,
            concurrency: benchmarkConcurrency,
          };
          summaries.push(common);
          for (const result of results) {
            records.push({ ...common, operation: result.name, ...result });
          }
          const parse = results.find((result) => result.name === "parseAspDocument");
          console.log(`      parseAspDocument median ${parse?.median.toFixed(2)} ms`);
        }
      }
    }
  } finally {
    fs.rmSync(tempRoot, { recursive: true, force: true });
  }

  printSummary(records, summaries, cacheModes, backends);
  const reportPath = writeScaleReport({ records, summaries, cacheModes, backends });
  console.log("");
  console.log(`Report: ${reportPath}`);
}

function selectedBackends() {
  const raw = (process.env.ASP_LSP_ANALYSIS_BACKEND ?? "").toLowerCase();
  if (raw === "typescript" || raw === "off") {
    return [{ id: "ts", label: "TypeScript", mode: "typescript" }];
  }
  if (raw === "native") {
    return [{ id: "native", label: "Native", mode: "native" }];
  }
  return [
    { id: "ts", label: "TypeScript", mode: "typescript" },
    { id: "native", label: "Native", mode: "native" },
  ];
}

function selectedCacheModes() {
  if (
    process.env.ASP_LSP_BENCH_CACHE_MODE === undefined ||
    process.env.ASP_LSP_BENCH_CACHE_MODE === ""
  ) {
    return ["cold", "hot"];
  }
  return [readBenchmarkCacheMode()];
}

function selectedMultipliers() {
  const raw = process.env.ASP_LSP_BENCH_SCALES;
  if (raw === undefined || raw === "") {
    return allMultipliers;
  }
  const selected = raw
    .split(",")
    .map((part) => part.trim().replace(/x$/i, ""))
    .filter(Boolean)
    .map((part) => Number(part));
  if (selected.length === 0) {
    throw new Error("ASP_LSP_BENCH_SCALES must include at least one scale.");
  }
  for (const multiplier of selected) {
    if (!allMultipliers.includes(multiplier)) {
      throw new Error(
        `ASP_LSP_BENCH_SCALES contains unsupported scale ${multiplier}. Expected one of ${allMultipliers.join(", ")}.`,
      );
    }
  }
  return [...new Set(selected)];
}

function selectedOperationNames() {
  const raw = process.env.ASP_LSP_BENCH_OPERATIONS;
  if (raw === undefined || raw === "") {
    return allOperationNames;
  }
  const selected = raw
    .split(",")
    .map((part) => part.trim())
    .filter(Boolean);
  if (selected.length === 0) {
    throw new Error("ASP_LSP_BENCH_OPERATIONS must include at least one operation.");
  }
  const supported = new Set(allOperationNames);
  for (const operation of selected) {
    if (!supported.has(operation)) {
      throw new Error(
        `ASP_LSP_BENCH_OPERATIONS contains unsupported operation ${operation}. Expected one of ${allOperationNames.join(", ")}.`,
      );
    }
  }
  return [...new Set(selected)];
}

function warmupsForCacheMode(cacheMode) {
  const raw = process.env.ASP_LSP_BENCH_WARMUPS;
  if (raw !== undefined && raw !== "") {
    return readNonNegativeInteger("ASP_LSP_BENCH_WARMUPS", 0);
  }
  return cacheMode === "cold" ? 0 : 1;
}

async function withBackendEnvironment(backend, callback) {
  const previousBackend = process.env.ASP_LSP_ANALYSIS_BACKEND;
  const previousEnableSourceNative = process.env.ASP_LSP_ENABLE_SOURCE_NATIVE;
  const previousNativeCorePath = process.env.ASP_LSP_NATIVE_CORE_PATH;
  process.env.ASP_LSP_ANALYSIS_BACKEND = backend.mode;
  if (backend.mode === "native") {
    process.env.ASP_LSP_ENABLE_SOURCE_NATIVE = "1";
    if (!process.env.ASP_LSP_NATIVE_CORE_PATH && fs.existsSync(nativeBinary)) {
      process.env.ASP_LSP_NATIVE_CORE_PATH = nativeBinary;
    }
  }
  try {
    return await callback();
  } finally {
    restoreEnv("ASP_LSP_ANALYSIS_BACKEND", previousBackend);
    restoreEnv("ASP_LSP_ENABLE_SOURCE_NATIVE", previousEnableSourceNative);
    restoreEnv("ASP_LSP_NATIVE_CORE_PATH", previousNativeCorePath);
  }
}

function restoreEnv(name, value) {
  if (value === undefined) {
    delete process.env[name];
  } else {
    process.env[name] = value;
  }
}

async function runOperation(core, operation, source) {
  if (operation === "parseAspDocument") {
    await core.parseAspDocumentAsync(source.uri, source.text);
  } else if (operation === "buildVirtualDocuments") {
    const parsed = await core.parseAspDocumentAsync(source.uri, source.text);
    core.buildVirtualDocuments(parsed);
  } else if (operation === "collectVbscriptSymbols") {
    await core.collectVbscriptSymbolsFromTextAsync(source.uri, source.text);
  } else if (operation === "analyzeVbscript") {
    await core.analyzeVbscriptFromTextAsync(source.uri, source.text, {});
  } else if (embeddedOperationNames.includes(operation)) {
    await runEmbeddedOperation(operation, source, core);
  } else {
    throw new Error(`Unknown benchmark operation: ${operation}`);
  }
}

async function runBenchmark(results, name, warmupIterations, fn) {
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

async function measureAcrossSources(sources, concurrency, callback) {
  const start = performance.now();
  await runBounded(sources, concurrency, callback);
  return performance.now() - start;
}

async function runBounded(items, concurrency, callback) {
  let next = 0;
  const workers = Array.from({ length: Math.min(concurrency, items.length) }, async () => {
    while (next < items.length) {
      const item = items[next];
      next += 1;
      await callback(item);
    }
  });
  await Promise.all(workers);
}

function generateScaleSample(sampleRoot, multiplier) {
  const includesRoot = path.join(sampleRoot, "includes");
  const generatedIncludesRoot = path.join(includesRoot, "generated");
  fs.rmSync(sampleRoot, { recursive: true, force: true });
  fs.mkdirSync(generatedIncludesRoot, { recursive: true });
  for (const spec of mainFiles) {
    writeFile(sampleRoot, spec.file, buildMainFile(spec, multiplier));
    for (let index = 1; index <= extraIncludeCount; index += 1) {
      const suffix = String(index).padStart(2, "0");
      writeFile(
        sampleRoot,
        `includes/generated/${spec.extraPrefix}-extra-${suffix}.inc`,
        buildExtraInclude(spec, index),
      );
    }
  }
}

function buildMainFile(spec, multiplier) {
  const targetLines = baseTargetLines * multiplier;
  const lines = generatedHeader(spec.file, multiplier);
  pushIncludeDirectives(lines, spec);
  lines.push(`<%`);
  lines.push(`Dim layer${spec.layer}BenchmarkTitle`);
  lines.push(
    `layer${spec.layer}BenchmarkTitle = "Layer ${spec.layer} scale benchmark ${multiplier}x"`,
  );
  lines.push(`%>`);
  lines.push(
    `<div class="layer${spec.layer}-benchmark" data-role="${spec.role}" data-scale="${multiplier}x">`,
  );
  lines.push(`  <h1><%= Server.HTMLEncode(layer${spec.layer}BenchmarkTitle) %></h1>`);
  let block = 1;
  while (lines.length + 38 < targetLines) {
    pushBenchmarkBlock(lines, spec.layer, block, spec.role);
    block += 1;
  }
  lines.push(`</div>`);
  while (lines.length < targetLines) {
    lines.push(
      `<!-- layer${spec.layer} scale ${multiplier}x filler line ${String(lines.length + 1).padStart(5, "0")} -->`,
    );
  }
  if (lines.length !== targetLines) {
    throw new Error(`${spec.file} generated ${lines.length} lines, expected ${targetLines}`);
  }
  return `${lines.join("\n")}\n`;
}

function pushBenchmarkBlock(lines, layer, block, role) {
  const padded = String(block).padStart(4, "0");
  const prefix = `layer${layer}`;
  lines.push(`<!-- ${prefix} benchmark block ${padded} -->`);
  lines.push(`<section class="${prefix}-block" data-layer="${layer}" data-block="${padded}">`);
  lines.push(`  <h2>${role} block ${padded}</h2>`);
  lines.push(`  <p>Static benchmark markup for ${prefix} block ${padded}.</p>`);
  lines.push(`  <style>`);
  lines.push(`    .${prefix}-block[data-block="${padded}"] .benchmark-meter {`);
  lines.push(`      --benchmark-layer: ${layer};`);
  lines.push(`      color: hsl(${(layer * 47 + block) % 360} 64% 32%);`);
  lines.push(
    `      border-left: ${1 + (block % 4)}px solid hsl(${(layer * 31 + block) % 360} 56% 52%);`,
  );
  lines.push(`    }`);
  lines.push(
    `    .${prefix}-block[data-block="${padded}"] .benchmark-meter::before { content: "${prefix}-${padded}"; }`,
  );
  lines.push(`  </style>`);
  lines.push(`  <script>`);
  lines.push(`    (function () {`);
  lines.push(`      const key = "${prefix}-${padded}";`);
  lines.push(`      const store = (window.aspLspBenchmark = window.aspLspBenchmark || {});`);
  lines.push(
    `      store[key] = { layer: ${layer}, block: ${block}, role: "${role}", even: ${block % 2 === 0} };`,
  );
  lines.push(`      document.documentElement.dataset.aspLspLastBenchmark = key;`);
  lines.push(`    })();`);
  lines.push(`  </script>`);
  lines.push(`<%`);
  lines.push(`Dim ${prefix}Index${padded}`);
  lines.push(`${prefix}Index${padded} = ${block}`);
  lines.push(`If (${prefix}Index${padded} Mod 2) = 0 Then`);
  lines.push(
    `    Response.Write "<span class=""even"">" & Server.HTMLEncode("${prefix}-even-${padded}") & "</span>"`,
  );
  lines.push(`Else`);
  lines.push(
    `    Response.Write "<span class=""odd"">" & Server.HTMLEncode("${prefix}-odd-${padded}") & "</span>"`,
  );
  lines.push(`End If`);
  lines.push(`%>`);
  lines.push(`  <ul>`);
  lines.push(`    <li><%= Server.HTMLEncode("${prefix}-item-${padded}-a") %></li>`);
  lines.push(`    <li><%= Server.HTMLEncode("${prefix}-item-${padded}-b") %></li>`);
  lines.push(`  </ul>`);
  lines.push(`  <div class="benchmark-meter" data-score="<%= ${prefix}Index${padded} %>"></div>`);
  lines.push(`</section>`);
  lines.push(``);
}

function pushIncludeDirectives(lines, spec) {
  if (spec.chainInclude) {
    lines.push(`<!-- #include file="${spec.chainInclude}" -->`);
  }
  for (let index = 1; index <= extraIncludeCount; index += 1) {
    lines.push(`<!-- #include file="${extraIncludePath(spec, index)}" -->`);
  }
}

function extraIncludePath(spec, index) {
  const suffix = String(index).padStart(2, "0");
  const fileName = `${spec.extraPrefix}-extra-${suffix}.inc`;
  return spec.file === "default.asp" ? `includes/generated/${fileName}` : `generated/${fileName}`;
}

function buildExtraInclude(spec, index) {
  const suffix = String(index).padStart(2, "0");
  const name = `${spec.extraPrefix}-extra-${suffix}`;
  const variable = `${spec.extraPrefix}Extra${suffix}`;
  return `${generatedHeader(`includes/generated/${name}.inc`, 1).join("\n")}
<!-- Leaf helper include for ${spec.file}; intentionally has no nested includes. -->
<aside class="${name}" data-owner="${spec.file}" data-helper="${suffix}">
<%
Dim ${variable}Label
${variable}Label = "${name}"
Response.Write "<span class=""helper"">" & Server.HTMLEncode(${variable}Label) & "</span>"
%>
</aside>
`;
}

function generatedHeader(file, multiplier) {
  return [
    `<%@ Language="VBScript" %>`,
    `<!-- Generated by scripts/benchmark-scale.mjs: ${file} (${multiplier}x) -->`,
  ];
}

function writeFile(sampleRoot, relativePath, content) {
  const absolutePath = path.join(sampleRoot, relativePath);
  fs.mkdirSync(path.dirname(absolutePath), { recursive: true });
  fs.writeFileSync(absolutePath, content);
}

function collectScaleSources(sampleRoot) {
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

function printSummary(records, summaries, cacheModes, backends) {
  console.log("");
  console.log("Scale Classic ASP benchmark");
  console.log(`Backends: ${backends.map((backend) => backend.label).join(", ")}`);
  console.log(`Cache modes: ${cacheModes.join(", ")}`);
  console.log(`Multipliers: ${multipliers.map((multiplier) => `${multiplier}x`).join(", ")}`);
  console.log(`Iterations: ${benchmarkIterations}`);
  console.log(`Concurrency: ${benchmarkConcurrency}`);
  console.log("");
  printTable([
    ["Backend", "Cache", "Scale", "Lines", "Bytes", "parse median ms", "js diagnostics median ms"],
    ...summaries.map((summary) => {
      const parse = records.find(
        (record) =>
          record.backend === summary.backend &&
          record.cacheMode === summary.cacheMode &&
          record.scale === summary.scale &&
          record.operation === "parseAspDocument",
      );
      const javascript = records.find(
        (record) =>
          record.backend === summary.backend &&
          record.cacheMode === summary.cacheMode &&
          record.scale === summary.scale &&
          record.operation === "javascriptDiagnostics",
      );
      return [
        summary.backend,
        summary.cacheMode,
        summary.scale,
        summary.lines.toLocaleString("en-US"),
        summary.bytes.toLocaleString("en-US"),
        formatMillis(parse?.median ?? 0),
        formatMillis(javascript?.median ?? 0),
      ];
    }),
  ]);
}

function printTable(rows) {
  const widths = rows[0].map((_, column) => Math.max(...rows.map((row) => row[column].length)));
  for (const [index, row] of rows.entries()) {
    console.log(row.map((value, column) => value.padEnd(widths[column])).join("  "));
    if (index === 0) {
      console.log(widths.map((width) => "-".repeat(width)).join("  "));
    }
  }
}

function writeScaleReport(payload) {
  fs.mkdirSync(reportsRoot, { recursive: true });
  const date = new Intl.DateTimeFormat("en-CA", { timeZone: "Asia/Tokyo" }).format(new Date());
  const reportPath = path.join(reportsRoot, `benchmark-scale-${date}.html`);
  const fullPayload = {
    generatedAt: new Date().toISOString(),
    commit: gitShortSha(),
    platform: `${process.platform} ${process.arch}`,
    node: process.version,
    cpu: os.cpus()[0]?.model ?? "unknown CPU",
    runOptions: {
      iterations: benchmarkIterations,
      concurrency: benchmarkConcurrency,
      multipliers,
      operations: operationNames,
      nodeOptions: process.env.NODE_OPTIONS ?? "",
    },
    ...payload,
  };
  fs.writeFileSync(reportPath, scaleReportHtml(fullPayload));
  return reportPath;
}

function scaleReportHtml(payload) {
  const json = JSON.stringify(payload).replace(/</g, "\\u003c");
  return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Classic ASP LSP Scale Benchmark</title>
<style>
:root{color-scheme:dark;--bg:#070913;--ink:#f8fbff;--muted:#9fb0c8;--line:rgba(255,255,255,.14);--ts:#38bdf8;--native:#fb7185;--hot:#fbbf24;--cold:#67e8f9;--shadow:0 30px 90px rgba(0,0,0,.42)}
*{box-sizing:border-box}body{margin:0;min-height:100vh;background:radial-gradient(circle at 12% 8%,rgba(34,211,238,.24),transparent 28%),radial-gradient(circle at 88% 6%,rgba(251,113,133,.22),transparent 26%),radial-gradient(circle at 70% 82%,rgba(251,191,36,.14),transparent 34%),linear-gradient(135deg,var(--bg),#0b1020 48%,#111827);color:var(--ink);font-family:Inter,ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;line-height:1.5}
body:before{content:"";position:fixed;inset:0;pointer-events:none;background-image:linear-gradient(rgba(255,255,255,.035) 1px,transparent 1px),linear-gradient(90deg,rgba(255,255,255,.035) 1px,transparent 1px);background-size:44px 44px;mask-image:linear-gradient(to bottom,rgba(0,0,0,.9),transparent 76%)}
main{width:min(1380px,calc(100% - 32px));margin:0 auto;padding:34px 0 56px;position:relative}h1,h2,p{margin:0}h1{max-width:980px;font-size:clamp(34px,5vw,72px);line-height:.96;letter-spacing:0}.shine{display:block;background:linear-gradient(95deg,#fff 0%,#67e8f9 34%,#fda4af 67%,#fbbf24 100%);-webkit-background-clip:text;background-clip:text;color:transparent}.hero{min-height:44vh;display:grid;align-content:center;padding:36px 0 26px}.lede{max-width:940px;margin-top:18px;color:var(--muted);font-size:17px}
.meta,.toolbar,.legend{display:flex;flex-wrap:wrap;gap:10px}.meta{margin-top:24px}.pill,button,select{border:1px solid var(--line);border-radius:999px;background:rgba(255,255,255,.07);color:#eaf2ff;padding:8px 12px;font:inherit;font-size:13px;backdrop-filter:blur(18px)}button{cursor:pointer;transition:transform .16s ease,border-color .16s ease,background .16s ease}button:hover,button.active{transform:translateY(-1px);border-color:rgba(103,232,249,.72);background:rgba(103,232,249,.13)}
.panel,.metric,.table-wrap{border:1px solid var(--line);border-radius:8px;background:linear-gradient(180deg,rgba(255,255,255,.1),rgba(255,255,255,.045));box-shadow:var(--shadow);backdrop-filter:blur(22px)}.metrics{display:grid;grid-template-columns:repeat(auto-fit,minmax(230px,1fr));gap:14px;margin-top:28px}.metric{padding:16px;min-height:124px}.label{color:var(--muted);font-size:12px;text-transform:uppercase;letter-spacing:.08em}.value{margin-top:10px;font-size:30px;font-weight:780}.note{margin-top:8px;color:var(--muted);font-size:13px}.panel{padding:18px;margin-top:18px}.toolbar{justify-content:space-between;align-items:center;gap:16px;margin-bottom:14px}.toolbar-group{display:flex;flex-wrap:wrap;gap:8px;align-items:center}.legend{color:var(--muted);font-size:13px;align-items:center}.legend span{display:inline-flex;align-items:center;gap:7px}.swatch{width:24px;height:3px;border-radius:99px;display:inline-block}.ts-cold{background:#38bdf8}.ts-hot{background:#67e8f9}.native-cold{background:#fb7185}.native-hot{background:#fbbf24}
.chart-shell{overflow-x:auto;padding-bottom:4px}svg{display:block;min-width:980px;width:100%;height:auto}.axis,.row-label{fill:#9fb0c8;font-size:12px}.grid{stroke:rgba(255,255,255,.11);stroke-dasharray:4 6}.line{fill:none;stroke-width:3;stroke-linecap:round;stroke-linejoin:round}.dot{cursor:crosshair;filter:drop-shadow(0 8px 16px rgba(0,0,0,.28))}
.heatmap-wrap{margin-top:14px;overflow:auto}.heatmap{width:100%;min-width:980px;border-collapse:separate;border-spacing:8px}.heatmap th,.heatmap td{border:0;padding:0;background:transparent}.heatmap th{position:static;color:#dbeafe;font-size:12px;text-transform:uppercase;letter-spacing:.08em}.heatmap .operation{width:280px;padding:10px 8px;color:#dbeafe;font-weight:700;text-transform:none;letter-spacing:0}.heatmap-cell{min-height:66px;border:1px solid rgba(255,255,255,.16);border-radius:8px;padding:10px 12px;display:grid;align-content:center;gap:2px;box-shadow:inset 0 1px 0 rgba(255,255,255,.13);cursor:crosshair}.heatmap-cell .time{font-size:19px;font-weight:780;color:#fff}.heatmap-cell .detail{font-size:12px;color:rgba(248,251,255,.72)}.heatmap-cell.missing{color:var(--muted);place-items:center;background:rgba(255,255,255,.045);cursor:default}
.table-wrap{margin-top:14px;overflow:auto;max-height:620px}table{width:100%;border-collapse:collapse;min-width:980px}th,td{padding:10px 12px;border-bottom:1px solid rgba(255,255,255,.09);text-align:left;font-size:13px}th{position:sticky;top:0;z-index:1;background:rgba(15,23,42,.96);color:#dbeafe}td.num{text-align:right;font-variant-numeric:tabular-nums}.footer{color:var(--muted);margin-top:18px;font-size:13px}
.tooltip{position:fixed;z-index:20;pointer-events:none;opacity:0;transform:translate(-50%,calc(-100% - 18px));min-width:220px;max-width:340px;border:1px solid rgba(255,255,255,.18);border-radius:8px;background:rgba(5,10,24,.92);color:#f8fbff;padding:11px 12px;box-shadow:0 24px 70px rgba(0,0,0,.52);backdrop-filter:blur(16px);transition:opacity .1s ease}.tooltip strong{display:block;margin-bottom:5px}.tooltip .muted{color:var(--muted);font-size:12px}
@media (max-width:760px){main{width:min(100% - 20px,1380px);padding-top:18px}.hero{min-height:38vh}.panel{padding:14px}}
</style>
</head>
<body>
<main>
<section class="hero"><h1><span>Classic ASP LSP</span><span class="shine">Scale Benchmark</span></h1><p class="lede">Measured 1x / 2x / 4x / 8x samples generated with the same file shape and benchmark harness. Use the controls to compare operation growth across backend and cache modes.</p><div class="meta" id="meta"></div><div class="metrics" id="metrics"></div></section>
<section class="panel"><div class="toolbar"><div><h2 style="margin:0">Scale Timing Lines</h2><p class="footer" style="margin-top:4px">X axis is generated sample scale. Y axis is log-scaled milliseconds. Hover each point for exact values.</p></div><div class="toolbar-group"><span class="pill">Operation</span><select id="operationSelect"></select></div></div><div class="legend"><span><i class="swatch ts-cold"></i>TS cold</span><span><i class="swatch ts-hot"></i>TS hot</span><span><i class="swatch native-cold"></i>Native cold</span><span><i class="swatch native-hot"></i>Native hot</span></div><div class="chart-shell"><svg id="lineChart"></svg></div></section>
<section class="panel"><div class="toolbar"><div><h2 style="margin:0">1x Native Ratio Heatmap</h2><p class="footer" style="margin-top:4px">Native / TypeScript median ratio for the 1x sample. Red is slower, green is faster, and yellow is roughly equal. Hover cells for exact values.</p></div></div><div class="heatmap-wrap"><table class="heatmap" id="oneXHeatmap"></table></div></section>
<section><h2>All Measurements</h2><div class="table-wrap"><table id="dataTable"></table></div></section><p class="footer">Generated locally from scripts/benchmark-scale.mjs. Reports are ignored by git.</p>
</main><div class="tooltip" id="tooltip"></div><script id="benchmark-data" type="application/json">${json}</script>
<script>
const payload=JSON.parse(document.getElementById("benchmark-data").textContent),records=payload.records,fmt=new Intl.NumberFormat("en-US",{maximumFractionDigits:2,minimumFractionDigits:2}),intFmt=new Intl.NumberFormat("en-US"),operations=Array.from(new Set(records.map(r=>r.operation))),tip=document.getElementById("tooltip"),equalDeltaMs=.1,state={operation:"parseAspDocument"};
document.getElementById("meta").innerHTML=["Generated "+new Date(payload.generatedAt).toLocaleString(),"Commit "+payload.commit,"Iterations "+payload.runOptions.iterations,"Concurrency "+payload.runOptions.concurrency,payload.platform,payload.node,payload.cpu].filter(Boolean).map(v=>"<span class='pill'>"+esc(v)+"</span>").join("");
document.getElementById("operationSelect").innerHTML=operations.map(op=>"<option value='"+attr(op)+"'"+(op===state.operation?" selected":"")+">"+esc(op)+"</option>").join("");
document.getElementById("operationSelect").addEventListener("change",e=>{state.operation=e.target.value;renderChart()});
renderMetrics();renderChart();renderHeatmap();renderTable();
function renderMetrics(){const parseSeries=seriesFor("parseAspDocument"),coldNative=parseSeries.find(s=>s.backend==="Native"&&s.cacheMode==="cold"),hotNative=parseSeries.find(s=>s.backend==="Native"&&s.cacheMode==="hot"),largest=coldNative?.points.at(-1),hot=hotNative?.points.at(-1),cards=[["records",String(records.length),"measurement rows in this report"],["largest sample",largest?intFmt.format(largest.lines)+" lines":"-","8x generated sample"],["native cold parse 8x",largest?fmt.format(largest.median)+" ms":"-","parseAspDocument median"],["native hot parse 8x",hot?fmt.format(hot.median)+" ms":"-","parseAspDocument median"]];document.getElementById("metrics").innerHTML=cards.map(c=>"<article class='metric'><div class='label'>"+esc(c[0])+"</div><div class='value'>"+esc(c[1])+"</div><div class='note'>"+esc(c[2])+"</div></article>").join("")}
function renderChart(){const svg=document.getElementById("lineChart"),w=1180,h=470,left=86,right=42,top=40,bottom=72,cw=w-left-right,ch=h-top-bottom,series=seriesFor(state.operation),points=series.flatMap(s=>s.points),maxY=Math.max(...points.map(p=>p.median),1),minX=1,maxX=8,xScale=x=>left+(Math.log2(x)-Math.log2(minX))/(Math.log2(maxX)-Math.log2(minX))*cw,yScale=y=>top+(1-Math.log10(y+1)/Math.log10(maxY+1))*ch,colors={"TypeScript:cold":"#38bdf8","TypeScript:hot":"#67e8f9","Native:cold":"#fb7185","Native:hot":"#fbbf24"};let html="<rect x='0' y='0' width='"+w+"' height='"+h+"' rx='8' fill='rgba(255,255,255,.025)'/>";[0,.25,.5,.75,1].forEach(t=>{const y=top+t*ch,value=Math.pow(maxY+1,1-t)-1;html+="<line class='grid' x1='"+left+"' y1='"+y+"' x2='"+(w-right)+"' y2='"+y+"'/><text class='axis' x='"+(left-12)+"' y='"+(y+4)+"' text-anchor='end'>"+fmt.format(value)+" ms</text>"});payload.runOptions.multipliers.forEach(multiplier=>{const x=xScale(multiplier),sample=points.find(p=>p.multiplier===multiplier);html+="<line class='grid' x1='"+x+"' y1='"+top+"' x2='"+x+"' y2='"+(h-bottom)+"'/><text class='axis' x='"+x+"' y='"+(h-42)+"' text-anchor='middle'>"+multiplier+"x</text><text class='axis' x='"+x+"' y='"+(h-25)+"' text-anchor='middle'>"+(sample?intFmt.format(sample.lines):"-")+" lines</text><text class='axis' x='"+x+"' y='"+(h-10)+"' text-anchor='middle'>"+(sample?fmt.format(sample.bytes/1024/1024):"-")+" MB</text>"});html+="<text class='axis' x='"+left+"' y='22'>"+esc(state.operation)+" median timing</text>";for(const s of series){const color=colors[s.backend+":"+s.cacheMode],d=s.points.map((p,i)=>(i?"L":"M")+xScale(p.multiplier).toFixed(2)+","+yScale(p.median).toFixed(2)).join(" ");html+="<path class='line' d='"+d+"' stroke='"+color+"'/>";for(const p of s.points){const x=xScale(p.multiplier),y=yScale(p.median),tipText=s.backend+" "+s.cacheMode+" / "+state.operation+"\\n"+p.scale+"\\nmedian "+fmt.format(p.median)+" ms\\nmin "+fmt.format(p.min)+" ms / max "+fmt.format(p.max)+" ms\\nlines "+intFmt.format(p.lines)+" / bytes "+intFmt.format(p.bytes);html+="<circle class='dot' data-tip='"+attr(tipText)+"' cx='"+x+"' cy='"+y+"' r='5' fill='"+color+"' stroke='rgba(255,255,255,.72)' stroke-width='1.2'/>"}}svg.setAttribute("viewBox","0 0 "+w+" "+h);svg.innerHTML=html;bind(svg.querySelectorAll("[data-tip]"))}
function renderHeatmap(){const cacheModes=["cold","hot"],oneX=records.filter(r=>r.multiplier===1);let html="<thead><tr><th class='operation'>Operation</th>"+cacheModes.map(mode=>"<th>Native / TS "+esc(mode)+"</th>").join("")+"</tr></thead><tbody>";for(const operation of operations){html+="<tr><th class='operation'>"+esc(operation)+"</th>";for(const cacheMode of cacheModes){const native=oneX.find(r=>r.backend==="Native"&&r.cacheMode===cacheMode&&r.operation===operation),ts=oneX.find(r=>r.backend==="TypeScript"&&r.cacheMode===cacheMode&&r.operation===operation);if(!native||!ts||ts.median<=0){html+="<td><div class='heatmap-cell missing'>-</div></td>";continue}const ratio=native.median/ts.median,delta=native.median-ts.median,status=ratioStatus(ratio,delta),tipText="Native / TS "+cacheMode+" / "+operation+"\\nratio "+fmt.format(ratio)+"x ("+status+")\\ndelta "+fmt.format(delta)+" ms\\nNative median "+fmt.format(native.median)+" ms\\nTypeScript median "+fmt.format(ts.median)+" ms\\nNative min/max "+fmt.format(native.min)+" / "+fmt.format(native.max)+" ms\\nTS min/max "+fmt.format(ts.min)+" / "+fmt.format(ts.max)+" ms";html+="<td><div class='heatmap-cell' style='background:"+ratioColor(ratio,delta)+"' data-tip='"+attr(tipText)+"'><span class='time'>"+fmt.format(ratio)+"x</span><span class='detail'>"+esc(status)+"</span></div></td>"}html+="</tr>"}document.getElementById("oneXHeatmap").innerHTML=html+"</tbody>";bind(document.querySelectorAll("#oneXHeatmap [data-tip]"))}
function ratioStatus(ratio,delta){if(Math.abs(delta)<equalDeltaMs)return"roughly equal";if(ratio>1.05)return"native slower";if(ratio<0.95)return"native faster";return"roughly equal"}
function ratioColor(ratio,delta){if(Math.abs(delta)<equalDeltaMs)return"linear-gradient(135deg,rgba(250,204,21,.54),rgba(251,191,36,.78))";const strength=(0.38+Math.min(1,Math.abs(Math.log2(Math.max(ratio,0.000001)))/4)*0.44).toFixed(3);if(ratio>1.05)return"linear-gradient(135deg,rgba(251,113,133,"+strength+"),rgba(239,68,68,.82))";if(ratio<0.95)return"linear-gradient(135deg,rgba(74,222,128,"+strength+"),rgba(16,185,129,.82))";return"linear-gradient(135deg,rgba(250,204,21,.54),rgba(251,191,36,.78))"}
function seriesFor(operation){return["TypeScript","Native"].flatMap(backend=>["cold","hot"].map(cacheMode=>({backend,cacheMode,points:records.filter(r=>r.backend===backend&&r.cacheMode===cacheMode&&r.operation===operation).sort((a,b)=>a.multiplier-b.multiplier)}))).filter(s=>s.points.length>0)}
function renderTable(){let html="<thead><tr><th>Backend</th><th>Cache</th><th>Scale</th><th>Operation</th><th>median ms</th><th>min ms</th><th>mean ms</th><th>max ms</th><th>Files</th><th>Lines</th><th>Bytes</th></tr></thead><tbody>";records.slice().sort((a,b)=>a.backend.localeCompare(b.backend)||a.cacheMode.localeCompare(b.cacheMode)||a.multiplier-b.multiplier||a.operation.localeCompare(b.operation)).forEach(r=>{html+="<tr><td>"+r.backend+"</td><td>"+r.cacheMode+"</td><td>"+r.scale+"</td><td>"+r.operation+"</td><td class='num'>"+fmt.format(r.median)+"</td><td class='num'>"+fmt.format(r.min)+"</td><td class='num'>"+fmt.format(r.mean)+"</td><td class='num'>"+fmt.format(r.max)+"</td><td class='num'>"+intFmt.format(r.files)+"</td><td class='num'>"+intFmt.format(r.lines)+"</td><td class='num'>"+intFmt.format(r.bytes)+"</td></tr>"});document.getElementById("dataTable").innerHTML=html+"</tbody>"}
function bind(nodes){nodes.forEach(n=>{n.addEventListener("mousemove",e=>{tip.innerHTML=formatTip(n.dataset.tip||"");tip.style.left=e.clientX+"px";tip.style.top=e.clientY+"px";tip.style.opacity="1"});n.addEventListener("mouseleave",()=>tip.style.opacity="0")})}
function formatTip(text){const lines=text.split("\\n");return"<strong>"+esc(lines[0]||"")+"</strong>"+lines.slice(1).map(l=>"<div class='muted'>"+esc(l)+"</div>").join("")}
function esc(v){return String(v).replace(/[&<>"]/g,ch=>({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;"}[ch]))}
function attr(v){return esc(v).replace(/'/g,"&#39;")}
</script></body></html>`;
}

function gitShortSha() {
  try {
    return execFileSync("git", ["rev-parse", "--short", "HEAD"], {
      cwd: root,
      encoding: "utf8",
      stdio: ["ignore", "pipe", "ignore"],
    }).trim();
  } catch {
    return "unknown";
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

await main();
