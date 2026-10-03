import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";

import {
  compareReports,
  median,
  medianReport,
  parseBenchmarkOutput,
  parseOptions,
  registerTemporaryDirectory,
  runProcess,
  terminateForSignal,
} from "./benchmark-cross-revision.mjs";

const benchmarkNames = [
  "BenchmarkCrossRevisionTypedMemberCompletionCold",
  "BenchmarkCrossRevisionLargeAnalyze",
];

function report(overrides = {}) {
  return Object.fromEntries(
    benchmarkNames.map((name) => [
      name,
      {
        name,
        iterations: 10,
        nsPerOperation: 10_000_000,
        bytesPerOperation: 10_000,
        allocationsPerOperation: 100,
        ...overrides[name],
      },
    ]),
  );
}

function isProcessRunning(pid) {
  try {
    process.kill(pid, 0);
  } catch (error) {
    if (error?.code === "ESRCH") return false;
    throw error;
  }
  // A killed orphan stays a zombie until init reaps it, and some container init processes reap late.
  if (process.platform !== "linux") return true;
  try {
    const stat = fs.readFileSync(`/proc/${pid}/stat`, "utf8");
    return stat[stat.lastIndexOf(")") + 2] !== "Z";
  } catch {
    return false;
  }
}

test("parses benchmark memory output and normalizes the parallelism suffix", () => {
  const parsed = parseBenchmarkOutput(
    [
      "BenchmarkCrossRevisionTypedMemberCompletionCold-1  12  1.5 ms/op  2048 B/op  31 allocs/op",
      "BenchmarkCrossRevisionLargeAnalyze  8  2500 ns/op  4096 B/op  42 allocs/op",
    ].join("\n"),
  );
  assert.equal(parsed.BenchmarkCrossRevisionTypedMemberCompletionCold.nsPerOperation, 1_500_000);
  assert.equal(parsed.BenchmarkCrossRevisionTypedMemberCompletionCold.bytesPerOperation, 2048);
  assert.equal(parsed.BenchmarkCrossRevisionTypedMemberCompletionCold.allocationsPerOperation, 31);
  assert.equal(parsed.BenchmarkCrossRevisionLargeAnalyze.nsPerOperation, 2500);
});

test("rejects incomplete benchmark reports", () => {
  assert.throws(
    () =>
      parseBenchmarkOutput(
        "BenchmarkCrossRevisionTypedMemberCompletionCold  1  10 ns/op  1 B/op  1 allocs/op",
      ),
    /missing benchmark results: BenchmarkCrossRevisionLargeAnalyze/,
  );
});

test("medianReport ignores one outlier without pairing fields incorrectly", () => {
  const samples = [
    report({
      BenchmarkCrossRevisionTypedMemberCompletionCold: {
        nsPerOperation: 11,
        bytesPerOperation: 101,
        allocationsPerOperation: 10,
      },
    }),
    report({
      BenchmarkCrossRevisionTypedMemberCompletionCold: {
        nsPerOperation: 12,
        bytesPerOperation: 102,
        allocationsPerOperation: 11,
      },
    }),
    report({
      BenchmarkCrossRevisionTypedMemberCompletionCold: {
        nsPerOperation: 900,
        bytesPerOperation: 900,
        allocationsPerOperation: 900,
      },
    }),
  ];
  const medians = medianReport(samples);
  assert.equal(medians.BenchmarkCrossRevisionTypedMemberCompletionCold.nsPerOperation, 12);
  assert.equal(medians.BenchmarkCrossRevisionTypedMemberCompletionCold.bytesPerOperation, 102);
  assert.equal(medians.BenchmarkCrossRevisionTypedMemberCompletionCold.allocationsPerOperation, 11);
});

test("compareReports permits bounded noise but fails time and allocation regressions", () => {
  const baseline = report();
  const withinNoise = report({
    BenchmarkCrossRevisionTypedMemberCompletionCold: {
      nsPerOperation: 12_500_000,
      bytesPerOperation: 12_400,
      allocationsPerOperation: 124,
    },
  });
  assert.equal(compareReports(baseline, withinNoise).passed, true);

  const regression = report({
    BenchmarkCrossRevisionTypedMemberCompletionCold: {
      nsPerOperation: 20_000_000,
      bytesPerOperation: 300_000,
      allocationsPerOperation: 1_000,
    },
  });
  const comparison = compareReports(baseline, regression);
  assert.equal(comparison.passed, false);
  assert.deepEqual(
    comparison.comparisons.BenchmarkCrossRevisionTypedMemberCompletionCold.failures,
    ["time 2.00x > 1.20x", "B/op 30.00x > 1.25x", "allocs/op 10.00x > 1.25x"],
  );
});

test("parseOptions keeps the fixed baseline defaults and validates gate settings", () => {
  const options = parseOptions([], {});
  assert.equal(options.baselineRef, "ea5cec7aa286e399e280f6e2200e70dc3f0b94e8");
  assert.equal(options.currentRef, "HEAD");
  assert.equal(options.includeCurrentDiff, true);
  assert.equal(options.sampleCount, 5);
  assert.equal(options.benchtime, "250ms");
  assert.equal(options.benchmarkTimeoutMs, 60000);
  const revisionOnly = parseOptions(["--current", "5a1d11ee7", "--samples", "5"], {});
  assert.equal(revisionOnly.currentRef, "5a1d11ee7");
  assert.equal(revisionOnly.includeCurrentDiff, false);
  assert.equal(revisionOnly.sampleCount, 5);
  assert.equal(parseOptions(["--", "--current", "5a1d11ee7"], {}).currentRef, "5a1d11ee7");
  assert.equal(parseOptions(["--benchtime", "5x"], {}).benchtime, "5x");
  assert.throws(() => parseOptions(["--samples", "4"], {}), /at least 5/);
  assert.throws(() => parseOptions(["--benchtime", "fast"], {}), /Go duration/);
  assert.throws(() => parseOptions(["--benchmark-timeout-ms", "60001"], {}), /at most 60000ms/);
});

test("median rejects empty or non-finite measurements", () => {
  assert.throws(() => median([]), /non-empty/);
  assert.throws(() => median([1, Number.NaN]), /finite/);
  assert.equal(median([1, 3]), 2);
});

test("signal termination removes registered snapshots before exiting", () => {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-cross-revision-test-"));
  fs.writeFileSync(path.join(directory, "lspserver.test"), "temporary benchmark binary");
  registerTemporaryDirectory(directory);
  let exitCode;
  terminateForSignal(130, (code) => {
    exitCode = code;
  });
  assert.equal(exitCode, 130);
  assert.equal(fs.existsSync(directory), false);
});

test("process timeout kills a SIGTERM-ignoring descendant process group", async () => {
  if (process.platform === "win32") return;
  const source = `
    const { spawn } = require("node:child_process");
    const child = spawn(process.execPath, ["-e", "process.on('SIGTERM', () => {}); setInterval(() => {}, 1000)"], { stdio: "ignore" });
    console.log(child.pid);
    setInterval(() => {}, 1000);
  `;
  const result = await runProcess(process.execPath, ["-e", source], { timeoutMs: 100 });
  assert.equal(result.timedOut, true);
  const descendantPID = Number(result.stdout.trim());
  assert.equal(Number.isSafeInteger(descendantPID), true);
  await new Promise((resolve) => setTimeout(resolve, 50));
  assert.equal(isProcessRunning(descendantPID), false);
});
