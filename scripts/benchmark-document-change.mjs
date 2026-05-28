import { spawn } from "node:child_process";
import { execFileSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { pathToFileURL } from "node:url";
import { performance } from "node:perf_hooks";

const root = path.resolve(import.meta.dirname, "..");
const sampleRoot = path.join(root, "samples", "classic-asp-large-benchmark");
const generator = path.join(sampleRoot, "generate.mjs");
const serverPath = path.join(root, "packages", "language-server", "dist", "server.js");
const benchmarkIterations = readPositiveInteger("ASP_LSP_BENCH_ITERATIONS", 5);
const warmupIterations = readPositiveInteger("ASP_LSP_BENCH_WARMUPS", 1);
const rapidBurstSize = readPositiveInteger("ASP_LSP_BENCH_BURST_SIZE", 5);
const rapidDebounceMs = readNonNegativeInteger("ASP_LSP_BENCH_DEBOUNCE_MS", 80);
const defaultDebounceMs = readNonNegativeInteger("ASP_LSP_BENCH_DEFAULT_DEBOUNCE_MS", 250);
const collectDebugSteps = readBoolean("ASP_LSP_BENCH_DEBUG_STEPS");
const timeoutMs = readPositiveInteger("ASP_LSP_BENCH_TIMEOUT_MS", 120_000);
const changeKinds = readChangeKinds();
const changeModes = readChangeModes();
const editTargets = readEditTargets();
const backgroundModes = readBackgroundModes();
const debugEventNames = [
  "diskCache.hit",
  "diskCache.miss",
  "diskCache.write",
  "backgroundAnalysis.started",
  "backgroundAnalysis.completed",
  "check.vbscript.diagnostics.reuse",
  "js.snapshot.changeRange.hit",
  "js.snapshot.changeRange.miss",
  "js.snapshot.changeRange.reuse",
  "asp.builder.affected.count",
  "asp.builder.cache.hit",
  "asp.builder.cache.miss",
  "asp.signature.changed",
  "asp.signature.unchanged",
  "completion.cache.hit",
  "completion.cache.miss",
  "completion.cache.invalidate",
  "projectUpdate.scheduled",
  "projectUpdate.flushed",
  "openFileProjectMaintenance.completed",
  "disk.builder.restore.hit",
  "disk.builder.restore.miss",
  "disk.builder.persist",
  "worker.queue.wait",
  "worker.run.duration",
  "worker.payload.bytes",
];
const selectedStepNames = [
  "projectUpdate.flush",
  "documentChange.bumpAnalysisGeneration",
  "documentChange.dropCachedDocument",
  "documentChange.keepCachedDocument",
  "documentChange.scheduleDiagnostics",
  "analysis.parse.incremental",
  "analysis.parse.full",
  "check.parserDiagnostics",
  "check.includeDiagnostics",
  "check.htmlDiagnostics",
  "check.cssDiagnostics",
  "check.javascriptSyntax",
  "check.javascriptSemantic",
  "check.javascriptUnused",
  "check.vbscript.projectContext",
  "check.vbscript.diagnostics",
  "check.vbscript.diagnostics.reuse",
  "check.vbscript.diagnostics.symbols",
  "check.vbscript.diagnostics.unusedSymbols",
  "check.vbscript.diagnostics.identifierCase",
  "check.vbscript.diagnostics.callSyntax",
  "check.vbscript.diagnostics.declarationSyntax",
  "check.vbscript.diagnostics.serverScriptText",
  "check.vbscript.diagnostics.dedupe",
  "check.project.dedupe",
  "check.dedupe",
  "diagnostics.fast.dedupe",
  "diagnostics.fast.publish",
  "diagnostics.include.dedupe",
  "diagnostics.include.publish",
  "diagnostics.syntax.dedupe",
  "diagnostics.syntax.publish",
  "diagnostics.project.dedupe",
  "diagnostics.project.publish",
  "diagnostics.final.dedupe",
  "diagnostics.final.publish",
  "vbProjectContext.includeSummaryReuse",
  "vbProjectContext.includeParseFallback",
];

async function main() {
  if (!fs.existsSync(serverPath)) {
    throw new Error(
      "packages/language-server/dist/server.js is missing. Run `pnpm --filter @asp-lsp/language-server run build`.",
    );
  }

  execFileSync(process.execPath, [generator], { stdio: "inherit" });

  const sourceStats = summarizeSources(collectBenchmarkSources());
  const scenarioResults = [];
  for (const backgroundAnalysis of backgroundModes) {
    for (const changeMode of changeModes) {
      for (const editTarget of editTargets) {
        for (const changeKind of changeKinds) {
          scenarioResults.push(
            await runScenario(changeKind, changeMode, backgroundAnalysis, editTarget),
          );
        }
      }
    }
  }

  console.log("");
  console.log("Classic ASP document change benchmark");
  console.log(`Files: ${sourceStats.files}`);
  console.log(`Lines: ${sourceStats.lines.toLocaleString("en-US")}`);
  console.log(`Bytes: ${sourceStats.bytes.toLocaleString("en-US")}`);
  console.log(`Warmups: ${warmupIterations}`);
  console.log(`Iterations: ${benchmarkIterations}`);
  console.log(`Change kinds: ${changeKinds.join(", ")}`);
  console.log(`Change modes: ${changeModes.join(", ")}`);
  console.log(`Edit targets: ${editTargets.join(", ")}`);
  console.log(`Default debounce: ${defaultDebounceMs} ms`);
  console.log(`Rapid burst size: ${rapidBurstSize}`);
  console.log(`Rapid debounce: ${rapidDebounceMs} ms`);
  console.log(`Background analysis: ${backgroundModes.map(backgroundLabel).join(", ")}`);
  console.log("");
  printScenarioTable(scenarioResults);

  if (collectDebugSteps) {
    console.log("");
    console.log("Debug step totals");
    console.log(
      `Measured calls include ${benchmarkIterations} benchmark iterations per scenario. Warmups are excluded.`,
    );
    console.log("");
    printDebugStepTotals(scenarioResults);
    console.log("");
    console.log("Debug event counts");
    console.log("");
    printDebugEventTotals(scenarioResults);
  }

  console.log("");
  console.log("Workspace cache benchmark");
  console.log("");
  printWorkspaceCacheTable(await runWorkspaceCacheBenchmarks());
}

async function runScenario(changeKind, changeMode, backgroundAnalysis, editTarget) {
  const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-change-bench-"));
  const cacheDir = path.join(tempDir, "cache");
  const sourcePath = path.join(sampleRoot, "default.asp");
  const uri = pathToFileURL(sourcePath).href;
  const { burstSize, debounceMs } = changeModeSettings(changeMode);
  const totalChanges = (benchmarkIterations + warmupIterations) * burstSize + 8;
  const state = {
    text: appendMutableBenchmarkRegion(
      fs.readFileSync(sourcePath, "utf8"),
      totalChanges,
      editTarget,
    ),
    version: 1,
  };
  const editOffset = mutableEditOffset(state.text);
  const server = new RpcServer();

  try {
    await server.start();
    await server.request("initialize", {
      processId: process.pid,
      rootUri: pathToFileURL(sampleRoot).href,
      capabilities: {},
    });
    server.notify("workspace/didChangeConfiguration", {
      settings: {
        aspLsp: {
          cache: { enabled: true, directory: cacheDir },
          debug: { output: "verbose" },
          diagnostics: { debounceMs },
          workspace: { backgroundAnalysis },
        },
      },
    });
    const openStartedAt = performance.now();
    server.notify("textDocument/didOpen", {
      textDocument: {
        uri,
        languageId: "classic-asp",
        version: state.version,
        text: state.text,
      },
    });
    await server.waitForNotification("textDocument/publishDiagnostics");
    const openFirstDiagnosticsMs = performance.now() - openStartedAt;
    const openLogs = [];
    await waitForFinalCheckLog(server, uri, openLogs);
    const openFinalDiagnosticsMs = performance.now() - openStartedAt;
    const openFeatureMetrics = await measureOpenLanguageFeatures(
      server,
      uri,
      state.text,
      editTarget,
    );
    drainBenchmarkNotifications(server);

    for (let index = 0; index < warmupIterations; index += 1) {
      await measureDocumentChange(
        server,
        uri,
        state,
        editOffset,
        changeKind,
        burstSize,
        editTarget,
      );
      drainBenchmarkNotifications(server);
    }

    const samples = [];
    const debugStepTotals = new Map();
    const debugEventTotals = new Map();
    for (let index = 0; index < benchmarkIterations; index += 1) {
      const sample = await measureDocumentChange(
        server,
        uri,
        state,
        editOffset,
        changeKind,
        burstSize,
        editTarget,
      );
      samples.push(sample);
      for (const [step, elapsedMs] of sample.stepTimings) {
        debugStepTotals.set(step, (debugStepTotals.get(step) ?? 0) + elapsedMs);
      }
      addCounters(debugEventTotals, sample.eventCounts);
    }

    await server.request("shutdown", null);
    server.notify("exit", undefined);
    return {
      changeKind,
      changeMode,
      editTarget,
      backgroundAnalysis,
      burstSize,
      debounceMs,
      openMetrics: {
        firstDiagnosticsMs: openFirstDiagnosticsMs,
        finalDiagnosticsMs: openFinalDiagnosticsMs,
        ...openFeatureMetrics,
      },
      samples,
      debugStepTotals,
      debugEventTotals,
    };
  } finally {
    server.stop();
    fs.rmSync(tempDir, { recursive: true, force: true });
  }
}

async function measureDocumentChange(
  server,
  uri,
  state,
  editOffset,
  changeKind,
  burstSize,
  editTarget,
) {
  drainBenchmarkNotifications(server);
  const startedAt = performance.now();
  for (let index = 0; index < burstSize; index += 1) {
    const change = buildTextChange(state.text, editOffset, changeKind);
    state.version += 1;
    server.notify("textDocument/didChange", {
      textDocument: { uri, version: state.version },
      contentChanges: [{ range: change.range, text: change.text }],
    });
    state.text = change.nextText;
  }

  const interactiveHoverMs = await measureInteractiveHover(server, uri, state.text, editTarget);

  await server.waitForNotification("textDocument/publishDiagnostics");
  const firstDiagnosticsMs = performance.now() - startedAt;

  const logs = [];
  await waitForFinalCheckLog(server, uri, logs);
  const finalDiagnosticsMs = performance.now() - startedAt;
  logs.push(...server.takePendingNotifications("window/logMessage"));
  server.takePendingNotifications("textDocument/publishDiagnostics");

  return {
    firstDiagnosticsMs,
    finalDiagnosticsMs,
    interactiveHoverMs,
    analysisStarts: countLogsContaining(logs, `LSP analysis started: ${uri}`),
    stepTimings: collectLogTimings(logs),
    eventCounts: collectDebugEventCounts(logs),
  };
}

async function measureInteractiveHover(server, uri, text, editTarget) {
  const position = interactiveHoverPosition(text, editTarget);
  if (!position) {
    return undefined;
  }
  const startedAt = performance.now();
  await server.request("textDocument/hover", {
    textDocument: { uri },
    position,
  });
  return performance.now() - startedAt;
}

function interactiveHoverPosition(text, editTarget) {
  const marker =
    editTarget === "vbscript"
      ? "aspLspBenchmarkValue"
      : editTarget === "client-js"
        ? "aspLspBenchmark"
        : editTarget === "html"
          ? "data-asp-lsp-benchmark"
          : editTarget === "css"
            ? "asp-lsp-benchmark"
            : undefined;
  if (!marker) {
    return undefined;
  }
  const offset = text.lastIndexOf(marker);
  return offset === -1 ? undefined : positionAt(text, offset + Math.min(marker.length, 6));
}

async function measureOpenLanguageFeatures(server, uri, text, editTarget) {
  const position = interactiveHoverPosition(text, editTarget);
  const range = semanticTokenRange(text, position);
  const metrics = {};
  if (position) {
    metrics.hoverMs = await timedRequest(server, "textDocument/hover", {
      textDocument: { uri },
      position,
    });
    metrics.completionMs = await timedRequest(server, "textDocument/completion", {
      textDocument: { uri },
      position,
      context: { triggerKind: 1 },
    });
  }
  metrics.semanticTokensFullMs = await timedRequest(server, "textDocument/semanticTokens/full", {
    textDocument: { uri },
  });
  if (range) {
    metrics.semanticTokensRangeMs = await timedRequest(
      server,
      "textDocument/semanticTokens/range",
      {
        textDocument: { uri },
        range,
      },
    );
  }
  const codeLensStartedAt = performance.now();
  const codeLenses = await server.request("textDocument/codeLens", { textDocument: { uri } });
  metrics.codeLensMs = performance.now() - codeLensStartedAt;
  const resolvableLens = Array.isArray(codeLenses)
    ? codeLenses.find((item) => item && typeof item === "object" && item.data)
    : undefined;
  if (resolvableLens) {
    metrics.codeLensResolveMs = await timedRequest(server, "codeLens/resolve", resolvableLens);
  }
  return metrics;
}

async function timedRequest(server, method, params) {
  const startedAt = performance.now();
  await server.request(method, params);
  return performance.now() - startedAt;
}

function semanticTokenRange(text, position) {
  if (!position) {
    return undefined;
  }
  const lastLine = positionAt(text, text.length).line;
  return {
    start: { line: Math.max(0, position.line - 5), character: 0 },
    end: { line: Math.min(lastLine, position.line + 6), character: 0 },
  };
}

async function runWorkspaceCacheBenchmarks() {
  const results = [];
  if (backgroundModes.includes(false)) {
    results.push(await runColdWarmWorkspaceCacheScenario());
  }
  if (backgroundModes.includes(true)) {
    results.push(await runBackgroundWorkspaceCacheScenario());
  }
  return results;
}

async function runColdWarmWorkspaceCacheScenario() {
  const { server, tempDir, cacheDir } = await startWorkspaceCacheServer(false);
  try {
    const cold = await measureWorkspaceDiagnostics(server, "diskCache.write");
    const warm = await measureWorkspaceDiagnostics(server, "diskCache.hit");
    return {
      scenario: "background=off",
      cacheDir,
      rows: [
        { metric: "cold workspace diagnostics", ...cold },
        { metric: "warm workspace diagnostics", ...warm },
      ],
    };
  } finally {
    await stopWorkspaceCacheServer(server);
    fs.rmSync(tempDir, { recursive: true, force: true });
  }
}

async function runBackgroundWorkspaceCacheScenario() {
  const { server, tempDir, cacheDir } = await startWorkspaceCacheServer(true);
  try {
    const warmupStartedAt = performance.now();
    const warmupLogs = [];
    await waitForLogContaining(server, "backgroundAnalysis.completed", warmupLogs);
    warmupLogs.push(...server.takePendingNotifications("window/logMessage"));
    const warmup = {
      elapsedMs: performance.now() - warmupStartedAt,
      diagnosticCount: 0,
      eventCounts: collectDebugEventCounts(warmupLogs),
    };
    const warm = await measureWorkspaceDiagnostics(server, "diskCache.hit");
    return {
      scenario: "background=on",
      cacheDir,
      rows: [
        { metric: "background warmup", ...warmup },
        { metric: "post-background workspace diagnostics", ...warm },
      ],
    };
  } finally {
    await stopWorkspaceCacheServer(server);
    fs.rmSync(tempDir, { recursive: true, force: true });
  }
}

async function startWorkspaceCacheServer(backgroundAnalysis) {
  const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-workspace-cache-bench-"));
  const cacheDir = path.join(tempDir, "cache");
  const server = new RpcServer();
  await server.start();
  await server.request("initialize", {
    processId: process.pid,
    rootUri: pathToFileURL(sampleRoot).href,
    capabilities: {},
  });
  server.notify("workspace/didChangeConfiguration", {
    settings: {
      aspLsp: {
        cache: { enabled: true, directory: cacheDir },
        debug: { output: "verbose" },
        workspace: { backgroundAnalysis },
      },
    },
  });
  drainBenchmarkNotifications(server);
  return { server, tempDir, cacheDir };
}

async function stopWorkspaceCacheServer(server) {
  try {
    await server.request("shutdown", null);
    server.notify("exit", undefined);
  } finally {
    server.stop();
  }
}

async function measureWorkspaceDiagnostics(server, expectedLog) {
  drainBenchmarkNotifications(server);
  const startedAt = performance.now();
  const report = await server.request("workspace/diagnostic", { previousResultIds: [] });
  const logs = [];
  await waitForLogContaining(server, expectedLog, logs);
  await sleep(50);
  logs.push(...server.takePendingNotifications("window/logMessage"));
  return {
    elapsedMs: performance.now() - startedAt,
    diagnosticCount: countWorkspaceDiagnostics(report),
    eventCounts: collectDebugEventCounts(logs),
  };
}

async function sleep(ms) {
  await new Promise((resolve) => setTimeout(resolve, ms));
}

function buildTextChange(currentText, editOffset, changeKind) {
  if (changeKind === "insert") {
    const text = "x";
    return {
      range: {
        start: positionAt(currentText, editOffset),
        end: positionAt(currentText, editOffset),
      },
      text,
      nextText: currentText.slice(0, editOffset) + text + currentText.slice(editOffset),
    };
  }
  if (changeKind === "delete") {
    return {
      range: {
        start: positionAt(currentText, editOffset),
        end: positionAt(currentText, editOffset + 1),
      },
      text: "",
      nextText: currentText.slice(0, editOffset) + currentText.slice(editOffset + 1),
    };
  }
  const text = currentText[editOffset] === "x" ? "y" : "x";
  return {
    range: {
      start: positionAt(currentText, editOffset),
      end: positionAt(currentText, editOffset + 1),
    },
    text,
    nextText: currentText.slice(0, editOffset) + text + currentText.slice(editOffset + 1),
  };
}

function appendMutableBenchmarkRegion(text, totalChanges, editTarget) {
  const markerText = `${benchmarkMarker()}${"x".repeat(totalChanges)}`;
  if (editTarget === "html") {
    return `${text}
<div data-asp-lsp-benchmark="${markerText}"></div>
`;
  }
  if (editTarget === "css") {
    return `${text}
<style>
.asp-lsp-benchmark::after { content: "${markerText}"; }
</style>
`;
  }
  if (editTarget === "client-js") {
    return `${text}
<script>
const aspLspBenchmark = "${markerText}";
</script>
`;
  }
  return `${text}
<%
Dim aspLspBenchmarkValue
aspLspBenchmarkValue = 1
${markerText}
%>
`;
}

function mutableEditOffset(text) {
  const marker = benchmarkMarker();
  const markerOffset = text.indexOf(marker);
  if (markerOffset === -1) {
    throw new Error("Mutable benchmark marker was not inserted.");
  }
  return markerOffset + marker.length;
}

function benchmarkMarker() {
  return "' asp-lsp change benchmark ";
}

function collectBenchmarkSources() {
  const sources = [];
  const stack = [sampleRoot];
  while (stack.length > 0) {
    const directory = stack.pop();
    for (const entry of fs.readdirSync(directory, { withFileTypes: true })) {
      const absolutePath = path.join(directory, entry.name);
      if (entry.isDirectory()) {
        stack.push(absolutePath);
      } else if (entry.isFile() && /\.(?:asa|asp|inc)$/i.test(entry.name)) {
        sources.push(fs.readFileSync(absolutePath, "utf8"));
      }
    }
  }
  return sources;
}

function summarizeSources(sources) {
  return sources.reduce(
    (stats, text) => {
      stats.files += 1;
      stats.lines += text.split("\n").length - 1;
      stats.bytes += Buffer.byteLength(text);
      return stats;
    },
    { files: 0, lines: 0, bytes: 0 },
  );
}

function collectLogTimings(logs) {
  const timings = new Map();
  for (const log of logs) {
    const message = logMessage(log);
    const match = /^\[asp-lsp\] ([^:]+): .* in ([0-9.]+) ms/.exec(message);
    if (!match) {
      continue;
    }
    const step = match[1];
    if (step.startsWith("LSP ") || step.startsWith("background analysis ")) {
      continue;
    }
    const elapsedMs = Number(match[2]);
    timings.set(step, (timings.get(step) ?? 0) + elapsedMs);
  }
  return timings;
}

function collectDebugEventCounts(logs) {
  const counts = new Map();
  for (const log of logs) {
    const message = logMessage(log);
    for (const eventName of debugEventNames) {
      if (message.includes(`[asp-lsp] ${eventName}`)) {
        counts.set(eventName, (counts.get(eventName) ?? 0) + 1);
      }
    }
  }
  return counts;
}

function addCounters(target, source) {
  for (const [key, value] of source) {
    target.set(key, (target.get(key) ?? 0) + value);
  }
}

function countLogsContaining(logs, expected) {
  return logs.filter((log) => logMessage(log).includes(expected)).length;
}

function countWorkspaceDiagnostics(report) {
  const items =
    report && typeof report === "object" && Array.isArray(report.items) ? report.items : [];
  return items.reduce(
    (sum, item) =>
      sum + (item && typeof item === "object" && Array.isArray(item.items) ? item.items.length : 0),
    0,
  );
}

async function waitForLogContaining(server, expected, collectedLogs) {
  return waitForLogContainingAny(server, [expected], collectedLogs);
}

async function waitForFinalCheckLog(server, uri, collectedLogs) {
  return waitForLogContainingAny(server, finalCheckMessages(uri), collectedLogs);
}

function finalCheckMessages(uri) {
  return [
    `LSP check completed: ${uri}`,
    `LSP check slow completed: ${uri}`,
    `LSP check slow reused: ${uri}`,
  ];
}

async function waitForLogContainingAny(server, expectedMessages, collectedLogs) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    for (const log of server.takePendingNotifications("window/logMessage")) {
      collectedLogs.push(log);
      if (expectedMessages.some((expected) => logMessage(log).includes(expected))) {
        return log;
      }
    }
    const remaining = Math.max(1, deadline - Date.now());
    const log = await server.waitForNotification("window/logMessage", remaining);
    collectedLogs.push(log);
    if (expectedMessages.some((expected) => logMessage(log).includes(expected))) {
      return log;
    }
  }
  throw new Error(`Timed out waiting for log containing one of ${expectedMessages.join(", ")}.`);
}

function drainBenchmarkNotifications(server) {
  server.takePendingNotifications("textDocument/publishDiagnostics");
  server.takePendingNotifications("window/logMessage");
}

function logMessage(message) {
  const params = message.params;
  if (params && typeof params === "object" && typeof params.message === "string") {
    return params.message;
  }
  return JSON.stringify(message.params);
}

function positionAt(text, offset) {
  let line = 0;
  let character = 0;
  for (let index = 0; index < offset; index += 1) {
    if (text[index] === "\n") {
      line += 1;
      character = 0;
    } else {
      character += 1;
    }
  }
  return { line, character };
}

function printScenarioTable(scenarioResults) {
  const rows = [["Scenario", "Metric", "min ms", "median ms", "mean ms", "max ms"]];
  for (const scenario of scenarioResults) {
    const scenarioName = scenarioLabel(scenario);
    rows.push([
      scenarioName,
      "didOpen->firstDiagnostics",
      ...statsCells([scenario.openMetrics.firstDiagnosticsMs]),
    ]);
    rows.push([
      scenarioName,
      "didOpen->finalDiagnostics",
      ...statsCells([scenario.openMetrics.finalDiagnosticsMs]),
    ]);
    for (const [metric, label] of [
      ["hoverMs", "post-open hover"],
      ["completionMs", "post-open completion"],
      ["semanticTokensFullMs", "post-open semanticTokens/full"],
      ["semanticTokensRangeMs", "post-open semanticTokens/range"],
      ["codeLensMs", "post-open codeLens"],
      ["codeLensResolveMs", "post-open codeLens/resolve"],
    ]) {
      const value = scenario.openMetrics[metric];
      if (value !== undefined) {
        rows.push([scenarioName, label, ...statsCells([value])]);
      }
    }
    rows.push([
      scenarioName,
      "didChange->firstDiagnostics",
      ...statsCells(scenario.samples.map((sample) => sample.firstDiagnosticsMs)),
    ]);
    rows.push([
      scenarioName,
      "didChange->finalDiagnostics",
      ...statsCells(scenario.samples.map((sample) => sample.finalDiagnosticsMs)),
    ]);
    if (scenario.samples.some((sample) => sample.interactiveHoverMs !== undefined)) {
      rows.push([
        scenarioName,
        "interactive hover",
        ...statsCells(
          scenario.samples
            .map((sample) => sample.interactiveHoverMs)
            .filter((value) => value !== undefined),
        ),
      ]);
    }
    rows.push([
      scenarioName,
      "LSP analysis starts",
      ...statsCells(scenario.samples.map((sample) => sample.analysisStarts)),
    ]);
    for (const step of selectedStepNames) {
      const samples = scenario.samples.map((sample) => sample.stepTimings.get(step) ?? 0);
      if (samples.some((value) => value > 0)) {
        rows.push([scenarioName, step, ...statsCells(samples)]);
      }
    }
  }
  printRows(rows);
}

function printDebugStepTotals(scenarioResults) {
  const rows = [["Scenario", "Step", "total ms"]];
  for (const scenario of scenarioResults) {
    const scenarioName = scenarioLabel(scenario);
    for (const [step, total] of [...scenario.debugStepTotals.entries()].sort(
      (left, right) => right[1] - left[1],
    )) {
      rows.push([scenarioName, step, formatMillis(total)]);
    }
  }
  printRows(rows);
}

function printDebugEventTotals(scenarioResults) {
  const rows = [["Scenario", "Event", "count"]];
  for (const scenario of scenarioResults) {
    const scenarioName = scenarioLabel(scenario);
    for (const eventName of debugEventNames) {
      const count = scenario.debugEventTotals.get(eventName) ?? 0;
      if (count > 0) {
        rows.push([scenarioName, eventName, String(count)]);
      }
    }
  }
  printRows(rows);
}

function printWorkspaceCacheTable(results) {
  const rows = [
    [
      "Scenario",
      "Metric",
      "elapsed ms",
      "diagnostics",
      "disk hits",
      "disk misses",
      "disk writes",
      "background starts",
      "background completes",
    ],
  ];
  for (const result of results) {
    for (const row of result.rows) {
      rows.push([
        result.scenario,
        row.metric,
        formatMillis(row.elapsedMs),
        String(row.diagnosticCount),
        String(row.eventCounts.get("diskCache.hit") ?? 0),
        String(row.eventCounts.get("diskCache.miss") ?? 0),
        String(row.eventCounts.get("diskCache.write") ?? 0),
        String(row.eventCounts.get("backgroundAnalysis.started") ?? 0),
        String(row.eventCounts.get("backgroundAnalysis.completed") ?? 0),
      ]);
    }
  }
  printRows(rows);
}

function statsCells(samples) {
  const sorted = [...samples].sort((left, right) => left - right);
  const total = sorted.reduce((sum, value) => sum + value, 0);
  return [
    formatMillis(sorted[0]),
    formatMillis(sorted[Math.floor(sorted.length / 2)]),
    formatMillis(total / sorted.length),
    formatMillis(sorted[sorted.length - 1]),
  ];
}

function scenarioLabel(scenario) {
  return `target=${scenario.editTarget}, ${scenario.changeKind}, mode=${scenario.changeMode}, burst=${scenario.burstSize}, debounce=${scenario.debounceMs}ms, background=${backgroundLabel(
    scenario.backgroundAnalysis,
  )}`;
}

function printRows(rows) {
  const widths = rows[0].map((_, column) => Math.max(...rows.map((row) => row[column].length)));
  for (const [index, row] of rows.entries()) {
    console.log(row.map((value, column) => value.padEnd(widths[column])).join("  "));
    if (index === 0) {
      console.log(widths.map((width) => "-".repeat(width)).join("  "));
    }
  }
}

function backgroundLabel(value) {
  return value ? "on" : "off";
}

function formatMillis(value) {
  return value.toFixed(2);
}

function readChangeKinds() {
  const raw = process.env.ASP_LSP_BENCH_CHANGE_KIND ?? "all";
  if (raw === "all") {
    return ["insert", "delete", "replace"];
  }
  const values = raw
    .split(",")
    .map((value) => value.trim())
    .filter(Boolean);
  for (const value of values) {
    if (value !== "insert" && value !== "delete" && value !== "replace") {
      throw new Error("ASP_LSP_BENCH_CHANGE_KIND must be insert, delete, replace, or all.");
    }
  }
  return values.length > 0 ? values : ["insert", "delete", "replace"];
}

function readChangeModes() {
  const raw = process.env.ASP_LSP_BENCH_CHANGE_MODE ?? "all";
  if (raw === "all") {
    return ["single", "default", "rapid"];
  }
  const values = raw
    .split(",")
    .map((value) => value.trim())
    .filter(Boolean);
  for (const value of values) {
    if (value !== "single" && value !== "default" && value !== "rapid") {
      throw new Error("ASP_LSP_BENCH_CHANGE_MODE must be single, default, rapid, or all.");
    }
  }
  return values.length > 0 ? values : ["single", "default", "rapid"];
}

function readEditTargets() {
  const raw = process.env.ASP_LSP_BENCH_EDIT_TARGET ?? "vbscript";
  if (raw === "all") {
    return ["vbscript", "html", "css", "client-js"];
  }
  const values = raw
    .split(",")
    .map((value) => value.trim())
    .filter(Boolean);
  for (const value of values) {
    if (value !== "vbscript" && value !== "html" && value !== "css" && value !== "client-js") {
      throw new Error("ASP_LSP_BENCH_EDIT_TARGET must be vbscript, html, css, client-js, or all.");
    }
  }
  return values.length > 0 ? values : ["vbscript"];
}

function changeModeSettings(changeMode) {
  if (changeMode === "rapid") {
    return { burstSize: rapidBurstSize, debounceMs: rapidDebounceMs };
  }
  if (changeMode === "default") {
    return { burstSize: 1, debounceMs: defaultDebounceMs };
  }
  return { burstSize: 1, debounceMs: 0 };
}

function readBackgroundModes() {
  const raw = process.env.ASP_LSP_BENCH_BACKGROUND ?? "both";
  if (raw === "both") {
    return [false, true];
  }
  if (raw === "off") {
    return [false];
  }
  if (raw === "on") {
    return [true];
  }
  throw new Error("ASP_LSP_BENCH_BACKGROUND must be on, off, or both.");
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

class RpcServer {
  constructor() {
    this.nextId = 1;
    this.buffer = Buffer.alloc(0);
    this.stderr = "";
    this.responses = new Map();
    this.notificationWaiters = new Map();
    this.pendingNotifications = new Map();
  }

  async start() {
    this.child = spawn(process.execPath, [serverPath, "--stdio"], {
      cwd: root,
      env: { ...process.env },
      stdio: ["pipe", "pipe", "pipe"],
    });
    this.child.stdout.on("data", (chunk) => this.read(chunk));
    this.child.stderr.on("data", (chunk) => {
      this.stderr += chunk.toString("utf8");
    });
  }

  request(method, params) {
    const id = this.nextId;
    this.nextId += 1;
    this.write({ jsonrpc: "2.0", id, method, params });
    return new Promise((resolve, reject) => {
      const timer = setTimeout(
        () => reject(new Error(`Timed out waiting for ${method}: ${this.stderr}`)),
        timeoutMs,
      );
      this.responses.set(id, { method, resolve, reject, timer });
    });
  }

  notify(method, params) {
    this.write({ jsonrpc: "2.0", method, params });
  }

  waitForNotification(method, waitMs = timeoutMs) {
    const pending = this.pendingNotifications.get(method);
    const message = pending?.shift();
    if (message) {
      return Promise.resolve(message);
    }
    return new Promise((resolve, reject) => {
      const waiter = {
        resolve,
        reject,
        timer: setTimeout(() => {
          const waiters = this.notificationWaiters.get(method) ?? [];
          this.notificationWaiters.set(
            method,
            waiters.filter((item) => item !== waiter),
          );
          reject(new Error(`Timed out waiting for ${method}: ${this.stderr}`));
        }, waitMs),
      };
      const waiters = this.notificationWaiters.get(method) ?? [];
      waiters.push(waiter);
      this.notificationWaiters.set(method, waiters);
    });
  }

  takePendingNotifications(method) {
    const pending = this.pendingNotifications.get(method) ?? [];
    this.pendingNotifications.set(method, []);
    return pending;
  }

  stop() {
    this.child?.kill();
  }

  write(message) {
    const body = JSON.stringify(message);
    this.child?.stdin.write(`Content-Length: ${Buffer.byteLength(body, "utf8")}\r\n\r\n${body}`);
  }

  read(chunk) {
    this.buffer = Buffer.concat([this.buffer, chunk]);
    while (true) {
      const headerEnd = this.buffer.indexOf("\r\n\r\n");
      if (headerEnd === -1) {
        return;
      }
      const header = this.buffer.slice(0, headerEnd).toString("utf8");
      const length = /Content-Length:\s*(\d+)/i.exec(header)?.[1];
      if (!length) {
        throw new Error(`Missing Content-Length header: ${header}`);
      }
      const bodyStart = headerEnd + 4;
      const bodyEnd = bodyStart + Number(length);
      if (this.buffer.length < bodyEnd) {
        return;
      }
      const message = JSON.parse(this.buffer.slice(bodyStart, bodyEnd).toString("utf8"));
      this.buffer = this.buffer.slice(bodyEnd);
      if (message.id !== undefined) {
        this.resolveResponse(message);
      } else if (message.method) {
        this.resolveNotification(message);
      }
    }
  }

  resolveResponse(message) {
    const pending = this.responses.get(message.id);
    if (!pending) {
      return;
    }
    clearTimeout(pending.timer);
    this.responses.delete(message.id);
    if (message.error) {
      pending.reject(
        new Error(
          `Request ${pending.method} failed: ${JSON.stringify(message.error)} ${this.stderr}`,
        ),
      );
      return;
    }
    pending.resolve(message.result);
  }

  resolveNotification(message) {
    const waiters = this.notificationWaiters.get(message.method) ?? [];
    const waiter = waiters.shift();
    if (waiter) {
      clearTimeout(waiter.timer);
      waiter.resolve(message);
      this.notificationWaiters.set(message.method, waiters);
      return;
    }
    const pending = this.pendingNotifications.get(message.method) ?? [];
    pending.push(message);
    this.pendingNotifications.set(message.method, pending);
  }
}

await main();
