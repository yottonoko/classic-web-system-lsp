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
const collectDebugSteps = readBoolean("ASP_LSP_BENCH_DEBUG_STEPS");
const timeoutMs = readPositiveInteger("ASP_LSP_BENCH_TIMEOUT_MS", 120_000);
const changeKinds = readChangeKinds();
const changeModes = readChangeModes();
const backgroundModes = readBackgroundModes();
const selectedStepNames = [
  "documentChange.bumpAnalysisGeneration",
  "documentChange.dropCachedDocument",
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
  "check.vbscript.diagnostics.symbols",
  "check.vbscript.diagnostics.unusedSymbols",
  "check.vbscript.diagnostics.identifierCase",
  "check.vbscript.diagnostics.callSyntax",
  "check.vbscript.diagnostics.declarationSyntax",
  "check.vbscript.diagnostics.serverScriptText",
  "check.vbscript.diagnostics.dedupe",
  "check.project.dedupe",
  "check.dedupe",
  "diagnostics.fast.total",
  "diagnostics.slow.include",
  "diagnostics.slow.syntax",
  "diagnostics.slow.project",
  "diagnostics.slow.project.reuse",
  "diagnostics.slow.project.currentFileOnly",
  "diagnostics.slow.project.fullRecompute",
  "diagnostics.slow.send",
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
      for (const changeKind of changeKinds) {
        scenarioResults.push(await runScenario(changeKind, changeMode, backgroundAnalysis));
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
  }
}

async function runScenario(changeKind, changeMode, backgroundAnalysis) {
  const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-change-bench-"));
  const cacheDir = path.join(tempDir, "cache");
  const sourcePath = path.join(sampleRoot, "default.asp");
  const uri = pathToFileURL(sourcePath).href;
  const { burstSize, debounceMs } = changeModeSettings(changeMode);
  const totalChanges = (benchmarkIterations + warmupIterations) * burstSize + 8;
  const state = {
    text: appendMutableBenchmarkRegion(fs.readFileSync(sourcePath, "utf8"), totalChanges),
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
    server.notify("textDocument/didOpen", {
      textDocument: {
        uri,
        languageId: "classic-asp",
        version: state.version,
        text: state.text,
      },
    });
    const openLogs = [];
    await waitForFinalCheckLog(server, uri, openLogs);
    drainBenchmarkNotifications(server);

    for (let index = 0; index < warmupIterations; index += 1) {
      await measureDocumentChange(server, uri, state, editOffset, changeKind, burstSize);
      drainBenchmarkNotifications(server);
    }

    const samples = [];
    const debugStepTotals = new Map();
    for (let index = 0; index < benchmarkIterations; index += 1) {
      const sample = await measureDocumentChange(
        server,
        uri,
        state,
        editOffset,
        changeKind,
        burstSize,
      );
      samples.push(sample);
      for (const [step, elapsedMs] of sample.stepTimings) {
        debugStepTotals.set(step, (debugStepTotals.get(step) ?? 0) + elapsedMs);
      }
    }

    await server.request("shutdown", null);
    server.notify("exit", undefined);
    return {
      changeKind,
      changeMode,
      backgroundAnalysis,
      burstSize,
      debounceMs,
      samples,
      debugStepTotals,
    };
  } finally {
    server.stop();
    fs.rmSync(tempDir, { recursive: true, force: true });
  }
}

async function measureDocumentChange(server, uri, state, editOffset, changeKind, burstSize) {
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
    analysisStarts: countLogsContaining(logs, `LSP analysis started: ${uri}`),
    stepTimings: collectLogTimings(logs),
  };
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

function appendMutableBenchmarkRegion(text, totalChanges) {
  return `${text}
<%
' asp-lsp change benchmark ${"x".repeat(totalChanges)}
%>
`;
}

function mutableEditOffset(text) {
  const marker = "' asp-lsp change benchmark ";
  const markerOffset = text.indexOf(marker);
  if (markerOffset === -1) {
    throw new Error("Mutable benchmark marker was not inserted.");
  }
  return markerOffset + marker.length;
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

function countLogsContaining(logs, expected) {
  return logs.filter((log) => logMessage(log).includes(expected)).length;
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
      "didChange->firstDiagnostics",
      ...statsCells(scenario.samples.map((sample) => sample.firstDiagnosticsMs)),
    ]);
    rows.push([
      scenarioName,
      "didChange->finalDiagnostics",
      ...statsCells(scenario.samples.map((sample) => sample.finalDiagnosticsMs)),
    ]);
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
  return `${scenario.changeKind}, mode=${scenario.changeMode}, burst=${scenario.burstSize}, debounce=${scenario.debounceMs}ms, background=${backgroundLabel(
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
    return ["single", "rapid"];
  }
  const values = raw
    .split(",")
    .map((value) => value.trim())
    .filter(Boolean);
  for (const value of values) {
    if (value !== "single" && value !== "rapid") {
      throw new Error("ASP_LSP_BENCH_CHANGE_MODE must be single, rapid, or all.");
    }
  }
  return values.length > 0 ? values : ["single", "rapid"];
}

function changeModeSettings(changeMode) {
  return changeMode === "rapid"
    ? { burstSize: rapidBurstSize, debounceMs: rapidDebounceMs }
    : { burstSize: 1, debounceMs: 0 };
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
