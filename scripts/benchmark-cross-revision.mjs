import crypto from "node:crypto";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { spawn, execFileSync } from "node:child_process";

const repositoryRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const fixtureRelativePath = "internal/lspserver/performance_regression_fixture_test.go";
const defaultBaselineRef = "ea5cec7aa286e399e280f6e2200e70dc3f0b94e8";
const activeChildren = new Set();
const registeredTemporaryDirectories = new Set();

const benchmarkPolicies = {
  BenchmarkCrossRevisionTypedMemberCompletionCold: {
    maxTimeRatio: 1.2,
    maxBytesRatio: 1.25,
    maxAllocsRatio: 1.25,
    timeNoiseFloorNs: 1_000_000,
    bytesNoiseFloor: 256 * 1024,
    allocsNoiseFloor: 500,
  },
  BenchmarkCrossRevisionLargeAnalyze: {
    maxTimeRatio: 1.2,
    maxBytesRatio: 1.2,
    maxAllocsRatio: 1.2,
    timeNoiseFloorNs: 1_000_000,
    bytesNoiseFloor: 256 * 1024,
    allocsNoiseFloor: 500,
  },
};

const timeUnitsToNanoseconds = {
  ns: 1,
  us: 1_000,
  µs: 1_000,
  ms: 1_000_000,
  s: 1_000_000_000,
};

function fail(message) {
  throw new Error(message);
}

function positiveInteger(value, label) {
  if (!/^\d+$/.test(value ?? "")) {
    fail(`${label} must be a positive integer`);
  }
  const parsed = Number(value);
  if (!Number.isSafeInteger(parsed) || parsed < 1) {
    fail(`${label} must be a positive integer`);
  }
  return parsed;
}

function benchmarkDuration(value) {
  if (!/^(?:\d+(?:\.\d+)?(?:ns|us|µs|ms|s|m)|\d+x)$/.test(value ?? "")) {
    fail(
      `benchmark duration must use a Go duration or iteration count such as 250ms or 5x: ${value}`,
    );
  }
  return value;
}

function parseOptions(argv, environment = process.env) {
  const options = {
    baselineRef: environment.ASP_LSP_PERF_BASELINE_REF ?? defaultBaselineRef,
    currentRef: "HEAD",
    includeCurrentDiff: true,
    sampleCount: positiveInteger(environment.ASP_LSP_PERF_SAMPLES ?? "5", "sample count"),
    benchtime: benchmarkDuration(environment.ASP_LSP_PERF_BENCHTIME ?? "250ms"),
    timeoutMs: positiveInteger(environment.ASP_LSP_PERF_TIMEOUT_MS ?? "600000", "timeout"),
    benchmarkTimeoutMs: positiveInteger(
      environment.ASP_LSP_PERF_BENCHMARK_TIMEOUT_MS ?? "60000",
      "benchmark timeout",
    ),
  };
  for (let index = 0; index < argv.length; index += 1) {
    const argument = argv[index];
    const next = argv[index + 1];
    if (argument === "--") {
      continue;
    } else if (argument === "--baseline") {
      if (!next) fail("--baseline requires a revision");
      options.baselineRef = next;
      index += 1;
    } else if (argument === "--current") {
      if (!next) fail("--current requires a revision");
      options.currentRef = next;
      options.includeCurrentDiff = false;
      index += 1;
    } else if (argument === "--samples") {
      if (!next) fail("--samples requires a value");
      options.sampleCount = positiveInteger(next, "sample count");
      index += 1;
    } else if (argument === "--benchtime") {
      if (!next) fail("--benchtime requires a duration");
      options.benchtime = benchmarkDuration(next);
      index += 1;
    } else if (argument === "--timeout-ms") {
      if (!next) fail("--timeout-ms requires a value");
      options.timeoutMs = positiveInteger(next, "timeout");
      index += 1;
    } else if (argument === "--benchmark-timeout-ms") {
      if (!next) fail("--benchmark-timeout-ms requires a value");
      options.benchmarkTimeoutMs = positiveInteger(next, "benchmark timeout");
      index += 1;
    } else {
      fail(`unknown option: ${argument}`);
    }
  }
  if (options.sampleCount < 5) {
    fail("sample count must be at least 5 so the median resists outliers");
  }
  if (options.benchmarkTimeoutMs > 60_000) {
    fail("benchmark timeout must be at most 60000ms");
  }
  return options;
}

function sha256(source) {
  return crypto.createHash("sha256").update(source).digest("hex");
}

function spawnTracked(command, args, options) {
  const child = spawn(command, args, {
    ...options,
    detached: process.platform !== "win32",
  });
  activeChildren.add(child);
  const forget = () => activeChildren.delete(child);
  child.once("close", forget);
  child.once("error", forget);
  return child;
}

function terminateChild(child, signal) {
  if (!child || !child.pid) return;
  if (process.platform === "win32") {
    try {
      execFileSync("taskkill", ["/PID", String(child.pid), "/T", "/F"], {
        stdio: "ignore",
        timeout: 5_000,
      });
    } catch {
      // taskkill reports failure when the whole tree has already exited.
    }
    return;
  }
  if (child.pid) {
    try {
      process.kill(-child.pid, signal);
      return;
    } catch (error) {
      if (error?.code !== "ESRCH" && child.exitCode === null && child.signalCode === null) {
        child.kill(signal);
      }
      return;
    }
  }
}

function cleanupTemporaryDirectory(directory) {
  registeredTemporaryDirectories.delete(directory);
  fs.rmSync(directory, { recursive: true, force: true });
}

function registerTemporaryDirectory(directory) {
  registeredTemporaryDirectories.add(directory);
}

function cleanupRegisteredTemporaryDirectories() {
  for (const directory of [...registeredTemporaryDirectories]) {
    cleanupTemporaryDirectory(directory);
  }
}

function terminateForSignal(exitCode, exit) {
  for (const child of activeChildren) {
    terminateChild(child, "SIGKILL");
  }
  cleanupRegisteredTemporaryDirectories();
  exit(exitCode);
}

function installSignalHandlers(exit = (code) => process.exit(code)) {
  let handlingSignal = false;
  const handlers = new Map();
  for (const [signal, exitCode] of [
    ["SIGINT", 130],
    ["SIGTERM", 143],
  ]) {
    const handler = () => {
      if (handlingSignal) return;
      handlingSignal = true;
      terminateForSignal(exitCode, exit);
    };
    handlers.set(signal, handler);
    process.on(signal, handler);
  }
  return () => {
    for (const [signal, handler] of handlers) {
      process.off(signal, handler);
    }
  };
}

function runProcess(command, args, options = {}) {
  const timeoutMs = options.timeoutMs ?? 600_000;
  const cwd = options.cwd ?? repositoryRoot;
  const environment = options.env ?? process.env;
  const binaryOutput = options.encoding === "buffer";
  const maxBuffer = options.maxBuffer ?? 128 * 1024 * 1024;
  return new Promise((resolve, reject) => {
    const child = spawnTracked(command, args, {
      cwd,
      env: environment,
      stdio: [options.input === undefined ? "ignore" : "pipe", "pipe", "pipe"],
    });
    let stdout = binaryOutput ? [] : "";
    let stderr = "";
    let outputBytes = 0;
    let outputError;
    let timedOut = false;
    const timeout = setTimeout(() => {
      timedOut = true;
      terminateChild(child, "SIGKILL");
    }, timeoutMs);
    if (!binaryOutput) child.stdout.setEncoding("utf8");
    child.stderr.setEncoding("utf8");
    child.stdout.on("data", (chunk) => {
      outputBytes += Buffer.byteLength(chunk);
      if (outputBytes > maxBuffer && !outputError) {
        outputError = new Error(`${command} output exceeded ${maxBuffer} bytes`);
        terminateChild(child, "SIGKILL");
        return;
      }
      if (binaryOutput) {
        stdout.push(chunk);
      } else {
        stdout += chunk;
      }
    });
    child.stderr.on("data", (chunk) => {
      outputBytes += Buffer.byteLength(chunk);
      if (outputBytes > maxBuffer && !outputError) {
        outputError = new Error(`${command} output exceeded ${maxBuffer} bytes`);
        terminateChild(child, "SIGKILL");
        return;
      }
      stderr += chunk;
    });
    child.on("error", (error) => {
      clearTimeout(timeout);
      reject(error);
    });
    child.on("close", (exitCode, signal) => {
      clearTimeout(timeout);
      if (outputError) {
        reject(outputError);
        return;
      }
      resolve({
        command,
        args,
        stdout: binaryOutput ? Buffer.concat(stdout) : stdout,
        stderr,
        exitCode,
        signal,
        timedOut,
      });
    });
    if (options.input !== undefined) {
      child.stdin.end(options.input);
    }
  });
}

async function runGit(args, options = {}) {
  const result = await runProcess("git", args, {
    cwd: options.cwd ?? repositoryRoot,
    encoding: options.encoding ?? "utf8",
    maxBuffer: options.maxBuffer ?? 128 * 1024 * 1024,
    input: options.input,
    timeoutMs: options.timeoutMs ?? 600_000,
  });
  if (result.timedOut) fail(`timed out running git ${args[0] ?? "command"}`);
  if (result.exitCode !== 0) {
    fail(
      [`git ${args.join(" ")} failed`, result.stderr.trim(), String(result.stdout).trim()]
        .filter(Boolean)
        .join("\n"),
    );
  }
  return result.stdout;
}

async function archiveRevision(ref, destination, timeoutMs) {
  await new Promise((resolve, reject) => {
    const archive = spawnTracked("git", ["archive", "--format=tar", ref], {
      cwd: repositoryRoot,
      stdio: ["ignore", "pipe", "pipe"],
    });
    const extractor = spawnTracked("tar", ["-xf", "-", "-C", destination], {
      cwd: repositoryRoot,
      stdio: ["pipe", "ignore", "pipe"],
    });
    let archiveError = "";
    let extractorError = "";
    let archiveExit;
    let extractorExit;
    let settled = false;
    const timeout = setTimeout(() => {
      if (settled) return;
      settled = true;
      terminateChild(archive, "SIGKILL");
      terminateChild(extractor, "SIGKILL");
      reject(new Error(`timed out archiving ${ref}`));
    }, timeoutMs);
    archive.stderr.setEncoding("utf8");
    extractor.stderr.setEncoding("utf8");
    archive.stderr.on("data", (chunk) => {
      archiveError += chunk;
    });
    extractor.stderr.on("data", (chunk) => {
      extractorError += chunk;
    });
    const finish = () => {
      if (settled || archiveExit === undefined || extractorExit === undefined) return;
      settled = true;
      clearTimeout(timeout);
      if (archiveExit !== 0 || extractorExit !== 0) {
        reject(
          new Error(
            [`could not archive ${ref}`, archiveError.trim(), extractorError.trim()]
              .filter(Boolean)
              .join("\n"),
          ),
        );
      } else {
        resolve();
      }
    };
    const failArchive = (error) => {
      if (settled) return;
      settled = true;
      clearTimeout(timeout);
      terminateChild(archive, "SIGKILL");
      terminateChild(extractor, "SIGKILL");
      reject(error);
    };
    archive.on("error", failArchive);
    extractor.on("error", failArchive);
    archive.on("close", (code) => {
      archiveExit = code;
      finish();
    });
    extractor.on("close", (code) => {
      extractorExit = code;
      finish();
    });
    const handlePipeError = (error) => {
      if (settled) return;
      failArchive(error);
    };
    archive.stdout.on("error", handlePipeError);
    extractor.stdin.on("error", handlePipeError);
    archive.stdout.pipe(extractor.stdin);
  });
}

async function currentWorkingTreePatch() {
  return runGit(["diff", "--binary", "HEAD", "--", ".", `:(exclude)${fixtureRelativePath}`], {
    encoding: "buffer",
    maxBuffer: 256 * 1024 * 1024,
  });
}

async function applyPatch(destination, patch) {
  if (patch.length === 0) return;
  await runGit(["apply", "--binary", "--whitespace=nowarn", "-"], {
    cwd: destination,
    input: patch,
    encoding: "utf8",
    maxBuffer: 16 * 1024 * 1024,
  });
}

function parseBenchmarkOutput(output, expectedNames = Object.keys(benchmarkPolicies)) {
  const expected = new Set(expectedNames);
  const reports = new Map();
  const linePattern =
    /^(\S+)\s+(\d+)\s+([0-9]+(?:\.[0-9]+)?)\s+(ns|us|µs|ms|s)\/op\s+([0-9]+(?:\.[0-9]+)?)\s+B\/op\s+([0-9]+(?:\.[0-9]+)?)\s+allocs\/op$/;
  for (const rawLine of output.split(/\r?\n/)) {
    const line = rawLine.trim();
    if (!line.startsWith("Benchmark")) continue;
    const match = line.match(linePattern);
    if (!match) {
      fail(`unparseable benchmark line: ${line}`);
    }
    const name = match[1].replace(/-\d+$/, "");
    if (!expected.has(name)) {
      fail(`unexpected benchmark result: ${name}`);
    }
    if (reports.has(name)) {
      fail(`duplicate benchmark result: ${name}`);
    }
    const timeValue = Number(match[3]);
    const bytesPerOperation = Number(match[5]);
    const allocationsPerOperation = Number(match[6]);
    if (![timeValue, bytesPerOperation, allocationsPerOperation].every(Number.isFinite)) {
      fail(`non-finite benchmark result: ${name}`);
    }
    reports.set(name, {
      name,
      iterations: Number(match[2]),
      nsPerOperation: timeValue * timeUnitsToNanoseconds[match[4]],
      bytesPerOperation,
      allocationsPerOperation,
    });
  }
  const missing = expectedNames.filter((name) => !reports.has(name));
  if (missing.length > 0) {
    fail(`missing benchmark results: ${missing.join(", ")}`);
  }
  return Object.fromEntries(reports);
}

function median(values) {
  if (
    !Array.isArray(values) ||
    values.length === 0 ||
    values.some((value) => !Number.isFinite(value))
  ) {
    fail("median requires a non-empty finite sample list");
  }
  const sorted = [...values].sort((left, right) => left - right);
  const middle = Math.floor(sorted.length / 2);
  return sorted.length % 2 === 1 ? sorted[middle] : (sorted[middle - 1] + sorted[middle]) / 2;
}

function medianReport(samples) {
  const names = Object.keys(benchmarkPolicies);
  const report = {};
  for (const name of names) {
    const measurements = samples.map((sample) => sample[name]);
    if (measurements.some((measurement) => measurement === undefined)) {
      fail(`cannot compute median for missing benchmark: ${name}`);
    }
    report[name] = {
      name,
      iterations: median(measurements.map((measurement) => measurement.iterations)),
      nsPerOperation: median(measurements.map((measurement) => measurement.nsPerOperation)),
      bytesPerOperation: median(measurements.map((measurement) => measurement.bytesPerOperation)),
      allocationsPerOperation: median(
        measurements.map((measurement) => measurement.allocationsPerOperation),
      ),
    };
  }
  return report;
}

function exceedsThreshold(current, baseline, ratio, noiseFloor) {
  return current > baseline * ratio + noiseFloor;
}

function compareReports(baseline, current) {
  const comparisons = {};
  let passed = true;
  for (const [name, policy] of Object.entries(benchmarkPolicies)) {
    const baselineMeasurement = baseline[name];
    const currentMeasurement = current[name];
    if (!baselineMeasurement || !currentMeasurement) {
      fail(`cannot compare missing benchmark: ${name}`);
    }
    const timeRatio = currentMeasurement.nsPerOperation / baselineMeasurement.nsPerOperation;
    const bytesRatio = currentMeasurement.bytesPerOperation / baselineMeasurement.bytesPerOperation;
    const allocsRatio =
      currentMeasurement.allocationsPerOperation / baselineMeasurement.allocationsPerOperation;
    const failures = [];
    if (
      exceedsThreshold(
        currentMeasurement.nsPerOperation,
        baselineMeasurement.nsPerOperation,
        policy.maxTimeRatio,
        policy.timeNoiseFloorNs,
      )
    ) {
      failures.push(`time ${timeRatio.toFixed(2)}x > ${policy.maxTimeRatio.toFixed(2)}x`);
    }
    if (
      exceedsThreshold(
        currentMeasurement.bytesPerOperation,
        baselineMeasurement.bytesPerOperation,
        policy.maxBytesRatio,
        policy.bytesNoiseFloor,
      )
    ) {
      failures.push(`B/op ${bytesRatio.toFixed(2)}x > ${policy.maxBytesRatio.toFixed(2)}x`);
    }
    if (
      exceedsThreshold(
        currentMeasurement.allocationsPerOperation,
        baselineMeasurement.allocationsPerOperation,
        policy.maxAllocsRatio,
        policy.allocsNoiseFloor,
      )
    ) {
      failures.push(`allocs/op ${allocsRatio.toFixed(2)}x > ${policy.maxAllocsRatio.toFixed(2)}x`);
    }
    const comparison = {
      name,
      baseline: baselineMeasurement,
      current: currentMeasurement,
      timeRatio,
      bytesRatio,
      allocsRatio,
      failures,
      passed: failures.length === 0,
    };
    comparisons[name] = comparison;
    passed &&= comparison.passed;
  }
  return { passed, comparisons };
}

function formatDuration(ns) {
  if (ns >= 1_000_000_000) return `${(ns / 1_000_000_000).toFixed(2)}s`;
  if (ns >= 1_000_000) return `${(ns / 1_000_000).toFixed(2)}ms`;
  if (ns >= 1_000) return `${(ns / 1_000).toFixed(2)}us`;
  return `${ns.toFixed(0)}ns`;
}

function formatBytes(bytes) {
  if (bytes >= 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(2)}MiB`;
  if (bytes >= 1024) return `${(bytes / 1024).toFixed(2)}KiB`;
  return `${bytes.toFixed(0)}B`;
}

function reportComparison(result) {
  for (const comparison of Object.values(result.comparisons)) {
    const { baseline, current } = comparison;
    const verdict = comparison.passed ? "PASS" : "FAIL";
    console.log(
      `${verdict} ${comparison.name}: ` +
        `time ${formatDuration(baseline.nsPerOperation)} -> ${formatDuration(current.nsPerOperation)} ` +
        `(${comparison.timeRatio.toFixed(2)}x), ` +
        `B/op ${formatBytes(baseline.bytesPerOperation)} -> ${formatBytes(current.bytesPerOperation)} ` +
        `(${comparison.bytesRatio.toFixed(2)}x), ` +
        `allocs/op ${baseline.allocationsPerOperation.toFixed(0)} -> ${current.allocationsPerOperation.toFixed(0)} ` +
        `(${comparison.allocsRatio.toFixed(2)}x)` +
        (comparison.failures.length > 0 ? ` [${comparison.failures.join("; ")}]` : ""),
    );
  }
  console.log(`cross-revision performance gate: ${result.passed ? "PASS" : "FAIL"}`);
}

async function createSnapshot({ ref, label, fixtureSource, includeCurrentDiff, timeoutMs }) {
  const destination = fs.mkdtempSync(path.join(os.tmpdir(), `asp-lsp-cross-revision-${label}-`));
  registerTemporaryDirectory(destination);
  try {
    await archiveRevision(ref, destination, timeoutMs);
    if (includeCurrentDiff) {
      await applyPatch(destination, await currentWorkingTreePatch());
    }
    const fixturePath = path.join(destination, fixtureRelativePath);
    fs.mkdirSync(path.dirname(fixturePath), { recursive: true });
    fs.writeFileSync(fixturePath, fixtureSource, "utf8");
    return { destination, ref, label, binaryPath: path.join(destination, "lspserver.test") };
  } catch (error) {
    cleanupTemporaryDirectory(destination);
    throw error;
  }
}

async function buildSnapshot(snapshot, timeoutMs) {
  console.log(`compile ${snapshot.label}: starting`);
  const result = await runProcess(
    "go",
    ["test", "./internal/lspserver", "-run", "^$", "-c", "-o", snapshot.binaryPath],
    {
      cwd: snapshot.destination,
      timeoutMs,
      env: process.env,
    },
  );
  if (result.timedOut) fail(`timed out compiling ${snapshot.label}`);
  if (result.exitCode !== 0) {
    fail(
      [`could not compile ${snapshot.label}`, result.stdout.trim(), result.stderr.trim()]
        .filter(Boolean)
        .join("\n"),
    );
  }
  if (!fs.existsSync(snapshot.binaryPath)) {
    fail(`compiler did not produce the explicit output ${snapshot.binaryPath}`);
  }
  console.log(`compile ${snapshot.label}: complete`);
}

async function runSnapshotBenchmark(snapshot, benchmarkName, benchtime, timeoutMs) {
  const result = await runProcess(
    snapshot.binaryPath,
    [
      "-test.run=^$",
      `-test.bench=^${benchmarkName}$`,
      `-test.benchtime=${benchtime}`,
      "-test.count=1",
      "-test.cpu=1",
      "-test.benchmem",
      `-test.timeout=${timeoutMs}ms`,
    ],
    {
      cwd: snapshot.destination,
      timeoutMs,
      env: { ...process.env, GOMAXPROCS: "1" },
    },
  );
  if (result.timedOut) fail(`timed out running ${snapshot.label}`);
  if (result.exitCode !== 0) {
    fail(
      [`${snapshot.label} benchmark failed`, result.stdout.trim(), result.stderr.trim()]
        .filter(Boolean)
        .join("\n"),
    );
  }
  return parseBenchmarkOutput(`${result.stdout}\n${result.stderr}`, [benchmarkName]);
}

async function resolvedRevision(ref) {
  return (await runGit(["rev-parse", "--verify", `${ref}^{commit}`])).trim();
}

export {
  benchmarkPolicies,
  compareReports,
  median,
  medianReport,
  parseBenchmarkOutput,
  parseOptions,
  runProcess,
  cleanupRegisteredTemporaryDirectories,
  installSignalHandlers,
  registerTemporaryDirectory,
  terminateForSignal,
};

async function main() {
  const options = parseOptions(process.argv.slice(2));
  const fixtureSource = fs.readFileSync(path.join(repositoryRoot, fixtureRelativePath), "utf8");
  const fixtureDigest = sha256(fixtureSource);
  const baselineCommit = await resolvedRevision(options.baselineRef);
  const currentCommit = await resolvedRevision(options.currentRef);
  const snapshots = [];
  try {
    snapshots.push(
      await createSnapshot({
        ref: baselineCommit,
        label: "baseline",
        fixtureSource,
        includeCurrentDiff: false,
        timeoutMs: options.timeoutMs,
      }),
    );
    snapshots.push(
      await createSnapshot({
        ref: currentCommit,
        label: "current",
        fixtureSource,
        includeCurrentDiff: options.includeCurrentDiff,
        timeoutMs: options.timeoutMs,
      }),
    );
    console.log(`baseline: ${baselineCommit}`);
    console.log(
      `current:  ${currentCommit}${options.includeCurrentDiff ? " (+tracked working-tree diff)" : ""}`,
    );
    console.log(`fixture:  ${fixtureRelativePath} (sha256 ${fixtureDigest})`);
    console.log(
      `samples:  ${options.sampleCount} alternating isolated executions (${options.benchtime} each)`,
    );
    for (const snapshot of snapshots) {
      await buildSnapshot(snapshot, options.timeoutMs);
    }
    const samples = { baseline: [], current: [] };
    for (let index = 0; index < options.sampleCount; index += 1) {
      const order = index % 2 === 0 ? ["baseline", "current"] : ["current", "baseline"];
      for (const label of order) {
        const snapshot = snapshots.find((candidate) => candidate.label === label);
        console.log(`sample ${index + 1}/${options.sampleCount} ${label}: starting`);
        const sample = {};
        for (const benchmarkName of Object.keys(benchmarkPolicies)) {
          Object.assign(
            sample,
            await runSnapshotBenchmark(
              snapshot,
              benchmarkName,
              options.benchtime,
              options.benchmarkTimeoutMs,
            ),
          );
        }
        samples[label].push(sample);
        console.log(`sample ${index + 1}/${options.sampleCount} ${label}: complete`);
      }
    }
    const baselineReport = medianReport(samples.baseline);
    const currentReport = medianReport(samples.current);
    const result = compareReports(baselineReport, currentReport);
    reportComparison(result);
    if (!result.passed) process.exitCode = 1;
  } finally {
    for (const snapshot of snapshots) {
      cleanupTemporaryDirectory(snapshot.destination);
    }
  }
}

const invokedPath = process.argv[1] ? pathToFileURL(path.resolve(process.argv[1])).href : "";
if (import.meta.url === invokedPath) {
  const uninstallSignalHandlers = installSignalHandlers();
  main()
    .catch((error) => {
      console.error(error instanceof Error ? error.message : String(error));
      process.exitCode = 1;
    })
    .finally(uninstallSignalHandlers);
}
