import crypto from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import { execFileSync } from "node:child_process";

const legacyRef = process.env.ASP_LSP_LEGACY_REF;
const legacySnapshot = JSON.parse(fs.readFileSync("docs/go-port-legacy-cases.json", "utf8"));
const ledgerPath = "docs/go-port-parity.md";
const ledger = fs.readFileSync(ledgerPath, "utf8");

function fail(message) {
  throw new Error(message);
}

function normalizeName(value) {
  return value.replace(/\s+/g, " ").trim();
}

function gitShow(refPath) {
  return execFileSync("git", ["show", `${legacyRef}:${refPath}`], { encoding: "utf8" });
}

function legacyCases(file) {
  const snapshot = legacySnapshot.files[file];
  if (!snapshot || !Array.isArray(snapshot.names)) fail(`missing legacy snapshot: ${file}`);
  if (!legacyRef) return snapshot.names.map((name) => ({ name }));
  const source = gitShow(file);
  const hash = crypto.createHash("sha256").update(source).digest("hex");
  if (hash !== snapshot.sha256) fail(`legacy source differs from snapshot: ${file}`);
  const cases = parseLegacyCases(source);
  if (JSON.stringify(cases.map((testCase) => testCase.name)) !== JSON.stringify(snapshot.names)) {
    fail(`legacy case names differ from snapshot: ${file}`);
  }
  return cases;
}

function parseLegacyCases(source) {
  const cases = [];
  const patterns = [
    /\b(?:it|test)\s*\(\s*(["'`])([\s\S]*?)\1/g,
    /\b(?:it|test)\.each\s*\([\s\S]*?\)\s*\(\s*(["'`])([\s\S]*?)\1/g,
  ];
  for (const pattern of patterns) {
    for (const match of source.matchAll(pattern)) {
      cases.push({
        line: source.slice(0, match.index).split("\n").length,
        name: normalizeName(match[2] ?? ""),
      });
    }
  }
  const byName = new Map();
  for (const testCase of cases) {
    if (!byName.has(testCase.name)) {
      byName.set(testCase.name, testCase);
    }
  }
  return [...byName.values()].sort((a, b) => a.line - b.line || a.name.localeCompare(b.name));
}

function parseSummary() {
  const summary = new Map();
  for (const match of ledger.matchAll(
    /^- (Legacy TS\/Vitest cases tracked|Go tests discovered|`[^`]+`): (\d+)$/gm,
  )) {
    summary.set(match[1].replaceAll("`", ""), Number(match[2]));
  }
  return summary;
}

function parseMatrixRows() {
  const rows = [];
  const rowPattern = /^\| `([^`]+\.test\.ts)` \| (\d+) \| (\d+) \| (\d+) \| (\d+) \|$/gm;
  for (const match of ledger.matchAll(rowPattern)) {
    rows.push({
      file: match[1],
      covered: Number(match[2]),
      coveredByPackage: Number(match[3]),
      merged: Number(match[4]),
      missing: Number(match[5]),
    });
  }
  return rows;
}

function parseLedgerRows() {
  const rows = [];
  const rowPattern = /^\| `([^`]+)` \| `([^`]+\.test\.ts):(\d+)` ([^|]+?) \| ([^|]+) \|$/gm;
  for (const match of ledger.matchAll(rowPattern)) {
    rows.push({
      status: match[1],
      file: match[2],
      line: Number(match[3]),
      name: normalizeName(match[4]),
      coverage: match[5].trim(),
    });
  }
  return rows;
}

function collectGoTestNames(directory) {
  const names = new Set();
  const stack = [directory];
  while (stack.length > 0) {
    const current = stack.pop();
    for (const entry of fs.readdirSync(current, { withFileTypes: true })) {
      const entryPath = path.join(current, entry.name);
      if (entry.isDirectory()) {
        stack.push(entryPath);
      } else if (entry.isFile() && entry.name.endsWith("_test.go")) {
        const source = fs.readFileSync(entryPath, "utf8");
        for (const name of goTestNamesInSource(source)) names.add(name);
      }
    }
  }
  return names;
}

function goTestNamesInSource(source) {
  return [...source.matchAll(/\bfunc (Test[A-Za-z0-9_]+)\(/g)].map((match) => match[1]);
}

function walkFiles(directory) {
  const files = [];
  const stack = [directory];
  while (stack.length > 0) {
    const current = stack.pop();
    if (!fs.existsSync(current)) continue;
    for (const entry of fs.readdirSync(current, { withFileTypes: true })) {
      const entryPath = path.join(current, entry.name);
      if (entry.isDirectory()) {
        stack.push(entryPath);
      } else if (entry.isFile()) {
        files.push(entryPath.split(path.sep).join("/"));
      }
    }
  }
  return files;
}

function globToRegExp(pattern) {
  const escaped = pattern.replace(/[.+^${}()|[\]\\]/g, "\\$&");
  return new RegExp(`^${escaped.replaceAll("*", "[^/]*").replaceAll("?", "[^/]")}$`);
}

function expandCoveragePath(targetPath) {
  if (!targetPath.includes("*") && !targetPath.includes("?")) return [targetPath];
  const parts = targetPath.split("/");
  const wildcardIndex = parts.findIndex((part) => part.includes("*") || part.includes("?"));
  const base = parts.slice(0, wildcardIndex).join("/") || ".";
  const pattern = globToRegExp(targetPath);
  return walkFiles(base).filter((file) => pattern.test(file));
}

function assertNoLegacyRuntime() {
  for (const legacyPath of ["packages/core", "packages/language-server"]) {
    if (fs.existsSync(legacyPath)) {
      fail(`legacy runtime directory is present: ${legacyPath}`);
    }
  }
}

function verifySourceInvariants() {
  const invariants = [
    {
      name: "real TS-Go language service",
      file: "third_party/typescript-go/aspadapter/service.go",
      includes: [
        "ls.NewLanguageService",
        "ProvideCompletion",
        "ResolveCompletionItem",
        "ProvideHover",
        "ProvideDiagnostics",
        "GetSemanticDiagnostics",
        "ProvideDefinition",
        "ProvideCodeActions",
        "ProvideInlayHint",
        "ProvideSemanticTokens",
      ],
    },
    {
      name: "JavaScript LSP routing",
      file: "internal/lspserver/server_requests.go",
      includes: [
        '"textDocument/definition"',
        '"textDocument/references"',
        '"textDocument/rename"',
        '"textDocument/prepareCallHierarchy"',
      ],
      excludes: [
        "javascript.Hover(",
        "javascript.Definition(",
        "javascript.References(",
        "javascript.RenameEdit(",
        "javascript.SignatureHelp(",
        "javascript.SelectionRange(",
        "javascript.DocumentSymbols(",
        "javascript.FoldingRanges(",
      ],
    },
    {
      name: "JavaScript completion routing",
      file: "internal/lspserver/completion.go",
      includes: ["javaScriptLanguageServiceRequest", "dedupeCaseSensitiveCompletionItems"],
      excludes: ["javascript.Completions("],
    },
    {
      name: "JavaScript compiler diagnostics",
      file: "internal/lspserver/diagnostics_parallel.go",
      includes: [
        '"textDocument/syntacticDiagnostic"',
        '"textDocument/diagnostic"',
        "javaScriptLanguageServiceRequest",
      ],
      excludes: ["javascript.Diagnostics("],
    },
    {
      name: "CSS definition and code actions",
      file: "internal/embedded/css.go",
      includes: ["FindDefinition", "DoCodeActions2"],
    },
    {
      name: "protocol request lifecycle parity",
      file: "internal/lspserver/server_requests.go",
      includes: [
        'requestClient(ctx, "workspace/configuration"',
        'case "textDocument/onTypeFormatting":',
      ],
    },
    {
      name: "protocol notification lifecycle parity",
      file: "internal/lspserver/server_notifications.go",
      includes: ['case "textDocument/willSave":'],
    },
  ];
  for (const invariant of invariants) {
    const source = fs.readFileSync(invariant.file, "utf8");
    for (const required of invariant.includes ?? []) {
      if (!source.includes(required)) {
        fail(
          `${invariant.name} missing required source invariant ${required} in ${invariant.file}`,
        );
      }
    }
    for (const forbidden of invariant.excludes ?? []) {
      if (source.includes(forbidden)) {
        fail(
          `${invariant.name} contains forbidden approximation ${forbidden} in ${invariant.file}`,
        );
      }
    }
  }
}

function verifyRowsCoverLegacyCases(ledgerRows, matrixRows) {
  const ledgerByFile = new Map();
  for (const row of ledgerRows) {
    if (!ledgerByFile.has(row.file)) {
      ledgerByFile.set(row.file, new Map());
    }
    ledgerByFile.get(row.file).set(row.name, row);
  }
  for (const matrix of matrixRows) {
    const legacyNames = new Set(legacyCases(matrix.file).map((testCase) => testCase.name));
    const ledgerNames = new Set(ledgerByFile.get(matrix.file)?.keys() ?? []);
    const missing = [...legacyNames].filter((name) => !ledgerNames.has(name));
    const extra = [...ledgerNames].filter((name) => !legacyNames.has(name));
    if (missing.length > 0 || extra.length > 0) {
      fail(
        [
          `${matrix.file} ledger mismatch`,
          ...missing.map((name) => `  missing from ledger: ${name}`),
          ...extra.map((name) => `  not found in legacy file: ${name}`),
        ].join("\n"),
      );
    }
  }
}

function verifyCounts(summary, ledgerRows, matrixRows) {
  const statusCounts = new Map();
  for (const row of ledgerRows) {
    statusCounts.set(row.status, (statusCounts.get(row.status) ?? 0) + 1);
  }
  const expectedSummary = {
    "Legacy TS/Vitest cases tracked": ledgerRows.length,
    covered: statusCounts.get("covered") ?? 0,
    "covered-by-package": statusCounts.get("covered-by-package") ?? 0,
    merged: statusCounts.get("merged") ?? 0,
    missing: statusCounts.get("missing") ?? 0,
  };
  for (const [key, expected] of Object.entries(expectedSummary)) {
    if (summary.get(key) !== expected) {
      fail(`summary ${key}=${summary.get(key)}, want ${expected}`);
    }
  }
  if ((statusCounts.get("missing") ?? 0) !== 0) {
    fail("ledger still contains missing rows");
  }
  const matrixTotals = {
    covered: 0,
    "covered-by-package": 0,
    merged: 0,
    missing: 0,
  };
  for (const row of matrixRows) {
    matrixTotals.covered += row.covered;
    matrixTotals["covered-by-package"] += row.coveredByPackage;
    matrixTotals.merged += row.merged;
    matrixTotals.missing += row.missing;
  }
  for (const [key, expected] of Object.entries(matrixTotals)) {
    if (summary.get(key) !== expected) {
      fail(`matrix ${key} total=${expected}, summary=${summary.get(key)}`);
    }
  }
}

function verifyGoTestCount(summary, goTests) {
  if (summary.get("Go tests discovered") !== goTests.size) {
    fail(`summary Go tests discovered=${summary.get("Go tests discovered")}, want ${goTests.size}`);
  }
}

function verifyGoCoverageNames(ledgerRows, goTests) {
  const referenced = new Set();
  for (const row of ledgerRows) {
    for (const match of row.coverage.matchAll(/\b(Test[A-Za-z0-9_]+)\b/g)) {
      referenced.add(match[1]);
    }
  }
  const missing = [...referenced].filter((name) => !goTests.has(name)).sort();
  if (missing.length > 0) {
    fail(`ledger references missing Go tests:\n${missing.map((name) => `  ${name}`).join("\n")}`);
  }
}

function verifyGoCoverageFiles(ledgerRows) {
  const missing = [];
  const withoutTests = [];
  const missingPackageTargets = [];
  for (const row of ledgerRows) {
    const targets = [
      ...row.coverage.matchAll(/\b((?:internal|third_party|apps|cmd)\/[^\s|;]+?_test\.go)\b/g),
    ].map((match) => match[1]);
    if (row.status === "covered-by-package" && targets.length === 0) {
      missingPackageTargets.push(`${row.file}:${row.line} ${row.name}`);
      continue;
    }
    for (const target of targets) {
      const files = expandCoveragePath(target);
      if (files.length === 0) {
        missing.push(target);
        continue;
      }
      const hasTest = files.some((file) => {
        const source = fs.readFileSync(file, "utf8");
        return goTestNamesInSource(source).length > 0;
      });
      if (!hasTest) withoutTests.push(target);
    }
  }
  if (missingPackageTargets.length > 0) {
    fail(
      `covered-by-package rows lack Go test file targets:\n${missingPackageTargets.map((name) => `  ${name}`).join("\n")}`,
    );
  }
  if (missing.length > 0) {
    fail(
      `ledger references missing Go test files:\n${missing.map((file) => `  ${file}`).join("\n")}`,
    );
  }
  if (withoutTests.length > 0) {
    fail(
      `ledger references Go test files without Test functions:\n${withoutTests.map((file) => `  ${file}`).join("\n")}`,
    );
  }
}

const summary = parseSummary();
const matrixRows = parseMatrixRows();
const ledgerRows = parseLedgerRows();
const goTests = collectGoTestNames("internal");
verifyRowsCoverLegacyCases(ledgerRows, matrixRows);
verifyCounts(summary, ledgerRows, matrixRows);
verifyGoTestCount(summary, goTests);
verifyGoCoverageNames(ledgerRows, goTests);
verifyGoCoverageFiles(ledgerRows);
verifySourceInvariants();
assertNoLegacyRuntime();
console.log(
  `Verified ${ledgerRows.length} legacy cases across ${matrixRows.length} files with missing=0.`,
);
