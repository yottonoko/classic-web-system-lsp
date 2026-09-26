import { describe, it, expect } from "vitest";
import fs from "node:fs";
import path from "node:path";
import { parseDebugLog, logHeatmap, logTimingSummary } from "../src/webview/log-analysis-model";

describe("debug log analysis", () => {
  it("parses real server text, appended metadata, JSON lines and fractional elapsed units", () => {
    const result = parseDebugLog(
      [
        '2026-09-08T01:00:00.100Z DEBUG diagnostics.step [asp-lsp] check.parser in 1.5 ms count=2 uri=file:///a.asp {"durationMs":1.543,"step":"check.parser","count":2}',
        "[asp-lsp] check.css in 25 µs count=0",
        JSON.stringify({
          timestamp: "2026-09-08T01:00:00.200Z",
          event: "lsp.request.completed",
          metadata: { method: "textDocument/hover", durationMs: 20, status: "ok" },
        }),
        "[asp-lsp] LSP analysis completed: file:///a.asp in 200.0 ms, diagnostics=2",
      ].join("\n"),
      "ja",
    );
    expect(result.entries.map((e) => e.duration)).toEqual([1.543, 0.025, 20, 200]);
    expect(result.entries.every((e) => e.known)).toBe(true);
    expect(result.entries[0].fields.count).toBe(2);
    expect(result.entries[3].event).toBe("analysis.total.completed");
  });
  it("handles object-valued fields and quoted braces in metadata safely", () => {
    const entries = parseDebugLog(
      '[asp-lsp] check.parser in 1 ms {"durationMs":1.234,"reason":"literal { brace","nested":{"ok":true}}\n' +
        JSON.stringify({
          event: { toString: 0 },
          method: { toString: 0 },
          status: { toString: 0 },
        }),
      "en",
    ).entries;
    expect(entries[0].duration).toBe(1.234);
    expect(entries[0].fields.nested).toEqual({ ok: true });
    expect(entries[1].known).toBe(false);
  });
  it("preserves malformed and unknown records, refuses invalid durations, and caps input", () => {
    const result = parseDebugLog(
      'unrecognized text\n{"bad":\n[asp-lsp] check.parser durationMs=-2\n[asp-lsp] custom.event durationMs=NaN',
      "en",
    );
    expect(result.entries).toHaveLength(4);
    expect(result.entries[0].known).toBe(false);
    expect(result.entries.every((e) => e.duration === undefined)).toBe(true);
    expect(parseDebugLog("x\n".repeat(10001), "en").truncated).toBe(true);
    expect(parseDebugLog("x".repeat(20001), "en").truncated).toBe(true);
  });
  it("switches explanations and aggregates heat by completion-time bucket without treating counts as duration", () => {
    const text =
      "2026-09-08T01:00:00Z [asp-lsp] check.parser durationMs=10\n2026-09-08T01:00:01Z [asp-lsp] check.parser durationMs=30\n[asp-lsp] worker.payload.bytes payload=100000";
    const en = parseDebugLog(text, "en").entries;
    expect(en[0].explanation).not.toBe(parseDebugLog(text, "ja").entries[0].explanation);
    expect(logTimingSummary(en)).toMatchObject({ timed: 2, maximum: 30, p95: 30, span: 1000 });
    expect(logHeatmap(en).rows[0].total).toBe(40);
    expect(logHeatmap(en).rows[0].values[0]).toBe(10);
    expect(logHeatmap(en).rows[0].values.at(-1)).toBe(30);
  });
  it("explains dynamic LSP lifecycle events in both languages", () => {
    for (const kind of ["request", "notification", "response"])
      for (const phase of ["received", "completed"])
        for (const locale of ["ja", "en"] as const) {
          const entry = parseDebugLog(
            `[asp-lsp] lsp.${kind}.${phase} method=textDocument/hover`,
            locale,
          ).entries[0];
          expect(entry.known).toBe(true);
          expect(entry.explanation.length).toBeGreaterThan(20);
        }
  });
  it("covers every static server event and database operation emitted by the source", () => {
    const root = path.resolve(import.meta.dirname, "../../../internal/lspserver");
    const missing: string[] = [];
    let checked = 0;
    for (const file of fs
      .readdirSync(root)
      .filter((name) => name.endsWith(".go") && !name.endsWith("_test.go"))) {
      const source = fs.readFileSync(path.join(root, file), "utf8");
      for (const match of source.matchAll(/\[asp-lsp\] ([a-zA-Z][\w.]*)/g)) {
        if (!match[1].includes(".")) continue;
        const event = match[1].replace(/\.$/, ".completed");
        if (["lsp.completed", "check.completed", "database.completed"].includes(event)) continue;
        checked++;
        for (const locale of ["ja", "en"] as const)
          if (!parseDebugLog(`[asp-lsp] ${event}`, locale).entries[0].known)
            missing.push(`${file}: ${event} (${locale})`);
      }
      for (const match of source.matchAll(/logAnalysisDatabaseEvent\(\s*"([^"]+)",\s*"([^"]+)"/g)) {
        checked++;
        const event = `database.${match[1]}.${match[2]}`;
        for (const locale of ["ja", "en"] as const)
          if (!parseDebugLog(`[asp-lsp] ${event}`, locale).entries[0].known)
            missing.push(`${file}: ${event} (${locale})`);
      }
    }
    expect(checked).toBeGreaterThan(140);
    expect(missing).toEqual([]);
  });
});

it("covers every measured diagnostic stage including stale termination", () => {
  const source = fs.readFileSync(
    path.resolve(import.meta.dirname, "../../../internal/lspserver/diagnostics_parallel.go"),
    "utf8",
  );
  const stages = source
    .match(/taskNames := \[\.\.\.\]string\{([\s\S]*?)\}/)![1]
    .match(/"([^"]+)"/g)!
    .map((value) => JSON.parse(value));
  for (const stage of [
    ...stages.map((stage) => `check.${stage}`),
    "analysis.parse",
    "analysisDatabase.diagnostics",
  ])
    for (const suffix of ["started", "completed", "cancelled", "stale"])
      for (const locale of ["ja", "en"] as const)
        expect(parseDebugLog(`[asp-lsp] ${stage}.${suffix}`, locale).entries[0].known).toBe(true);
});
it("explains failed persistence and notifications without success or request ID assumptions", () => {
  const failed = parseDebugLog(
    "[asp-lsp] database.workspaceLegacyUndefinedGlobals.write.failed: permission denied",
    "en",
  ).entries[0];
  expect(failed.explanation).toContain("failed");
  expect(failed.explanation).not.toContain("Persists results");
  const notification = parseDebugLog(
    "[asp-lsp] lsp.notification.received method=textDocument/didOpen",
    "en",
  ).entries[0];
  expect(notification.explanation).toContain("Notifications do not have");
  expect(notification.explanation).not.toContain("requestId identifies");
});
it("preserves legacy Output fields and formatting scope", () => {
  const entry = parseDebugLog(
    "[asp-lsp] Formatting conversion completed (document): file:///a.asp in 3.0 ms, edits=2, count=1",
    "en",
  ).entries[0];
  expect(entry.fields).toMatchObject({
    scope: "document",
    uri: "file:///a.asp",
    edits: 2,
    count: 1,
  });
  expect(entry.event).toBe("format.conversion.completed");
});
