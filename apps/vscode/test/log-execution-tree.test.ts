import { expect, test } from "vitest";
import { parseDebugLog } from "../src/webview/log-analysis-model";
import { buildLogExecutionTree } from "../src/webview/log-execution-tree";

const parse = (lines: object[]) =>
  parseDebugLog(lines.map((line) => JSON.stringify(line)).join("\n"), "en").entries;
test("pairs correlated requests without requiring completion URI", () => {
  const result = buildLogExecutionTree(
    parse([
      {
        event: "lsp.request.received",
        timestamp: "2026-09-08T00:00:00Z",
        requestId: 1,
        uri: "file:///a.asp",
      },
      {
        event: "lsp.request.completed",
        timestamp: "2026-09-08T00:00:01Z",
        requestId: 1,
        durationMs: 1000,
      },
    ]),
  );
  expect(result.spans).toHaveLength(1);
  expect(result.spans[0].end?.line).toBe(2);
  expect(result.spans[0].bottom).toBeGreaterThan(result.spans[0].top);
});
test("keeps parallel intervals in separate lanes and marks inferred parents", () => {
  const result = buildLogExecutionTree(
    parse([
      {
        event: "check.parser.completed",
        timestamp: "2026-09-08T00:00:00.800Z",
        durationMs: 500,
        uri: "file:///a.asp",
      },
      {
        event: "check.calls.completed",
        timestamp: "2026-09-08T00:00:00.900Z",
        durationMs: 500,
        uri: "file:///a.asp",
      },
      {
        event: "check.total.completed",
        timestamp: "2026-09-08T00:00:01Z",
        durationMs: 1000,
        uri: "file:///a.asp",
      },
    ]),
  );
  expect(new Set(result.spans.map((span) => span.lane)).size).toBe(3);
  expect(
    result.spans
      .filter((span) => span.event !== "check.total")
      .every((span) => span.parent === 3 && span.inferredParent),
  ).toBe(true);
});
test("does not guess duplicate lifecycle identities or invent timestamps", () => {
  const entries = parse([
    { event: "lsp.request.received", requestId: 1 },
    { event: "lsp.request.received", requestId: 1 },
    { event: "lsp.request.completed", requestId: 1, durationMs: 10 },
  ]);
  const result = buildLogExecutionTree(entries);
  expect(result.spans).toHaveLength(3);
  expect(result.ticks).toHaveLength(0);
  expect(result.spans.every((span) => span.ambiguous && span.begin === undefined)).toBe(true);
});
test("bounds diagram work while preserving omitted count", () => {
  const entries = parse(
    Array.from({ length: 1000 }, (_, i) => ({
      event: "check.parser.completed",
      timestamp: new Date(1788825600000 + i).toISOString(),
      durationMs: 1,
    })),
  );
  const result = buildLogExecutionTree(entries);
  expect(result.spans).toHaveLength(100);
  expect(result.omitted).toBe(900);
});

test("builds Output-only lifecycles and infers containment from recorded starts", () => {
  const entries = parseDebugLog(
    `[asp-lsp] LSP check started: file:///a.asp
[asp-lsp] check.parser.started uri=file:///a.asp
[asp-lsp] check.css.started uri=file:///a.asp
[asp-lsp] check.parser in 5 ms count=0 uri=file:///a.asp
[asp-lsp] check.css in 8 ms count=0 uri=file:///a.asp
[asp-lsp] LSP check completed: file:///a.asp in 12 ms`,
    "en",
  ).entries;
  const result = buildLogExecutionTree(entries);
  expect(result.spans).toHaveLength(3);
  expect(result.ticks).toHaveLength(0);
  expect(result.spans.every((span) => span.end && span.bottom > span.top)).toBe(true);
  expect(result.spans.slice(1).every((span) => span.parent === 1 && span.inferredParent)).toBe(
    true,
  );
  expect(new Set(result.spans.map((span) => span.lane)).size).toBe(3);
  expect(entries.every((entry) => entry.timestamp === undefined)).toBe(true);
});
test("places untimed runs before, between and after neighboring timestamps", () => {
  const entries = parseDebugLog(
    `before
2026-09-08T00:00:00Z [asp-lsp] custom.first
between A
between B
2026-09-08T00:00:01Z [asp-lsp] custom.last
after`,
    "en",
  ).entries;
  const result = buildLogExecutionTree(entries);
  expect(result.spans.map((span) => span.id)).toEqual([1, 2, 3, 4, 5, 6]);
  expect(
    result.spans.every((span, index) => index === 0 || span.top > result.spans[index - 1].top),
  ).toBe(true);
  expect(result.ticks).toHaveLength(2);
  expect(entries[2].timestamp).toBeUndefined();
});
test("matches diagnostic cache hit to its untimed start", () => {
  const result = buildLogExecutionTree(
    parseDebugLog(
      `[asp-lsp] analysisDatabase.diagnostics.started uri=file:///a.asp
[asp-lsp] analysisDatabase.diagnostics.hit in 2 ms count=3 uri=file:///a.asp`,
      "en",
    ).entries,
  );
  expect(result.spans).toHaveLength(1);
  expect(result.spans[0].end?.line).toBe(2);
});

test("preserves record order when timestamps repeat or move backward", () => {
  const entries = parseDebugLog(
    `2026-09-08T00:00:02Z [asp-lsp] custom.first
untimed
2026-09-08T00:00:02Z [asp-lsp] custom.second
untimed again
2026-09-08T00:00:01Z [asp-lsp] custom.third`,
    "en",
  ).entries;
  const result = buildLogExecutionTree(entries);
  expect(result.spans.map((span) => span.id)).toEqual([1, 2, 3, 4, 5]);
  expect(
    result.spans.every((span, index) => index === 0 || span.top > result.spans[index - 1].top),
  ).toBe(true);
});

test("uses explicit spans for parallel untimed checks and cross-file children", () => {
  const entries = parse([
    { event: "check.diagnostics.started", spanId: "root", traceId: "t" },
    {
      event: "check.parser.started",
      spanId: "a",
      parentSpanId: "root",
      traceId: "t",
      uri: "file:///a.asp",
    },
    {
      event: "check.parser.started",
      spanId: "b",
      parentSpanId: "root",
      traceId: "t",
      uri: "file:///b.asp",
    },
    { event: "check.parser.completed", spanId: "b", traceId: "t" },
    { event: "check.parser.completed", spanId: "a", traceId: "t" },
    { event: "check.diagnostics.completed", spanId: "root", traceId: "t" },
  ]);
  const result = buildLogExecutionTree(entries);
  expect(result.spans).toHaveLength(3);
  expect(result.spans[1].end?.line).toBe(5);
  expect(result.spans[2].end?.line).toBe(4);
  expect(
    result.spans
      .slice(1)
      .every((span) => span.parent === 1 && !span.inferredParent && !span.ambiguous),
  ).toBe(true);
});

test("does not infer a different parent when explicit parent is missing or in another trace", () => {
  const result = buildLogExecutionTree(
    parse([
      { event: "check.total.started", spanId: "root", traceId: "other", uri: "file:///a.asp" },
      {
        event: "check.parser.started",
        spanId: "child",
        parentSpanId: "root",
        traceId: "t",
        uri: "file:///a.asp",
      },
      { event: "check.parser.completed", spanId: "child", traceId: "t" },
      { event: "check.total.completed", spanId: "root", traceId: "other", uri: "file:///a.asp" },
    ]),
  );
  expect(result.spans[1].parent).toBeUndefined();
});

test("keeps explicit links acyclic even with malformed parent IDs", () => {
  const result = buildLogExecutionTree(
    parse([
      { event: "check.parser.started", spanId: "a", parentSpanId: "b", traceId: "t" },
      { event: "check.css.started", spanId: "b", parentSpanId: "a", traceId: "t" },
      { event: "check.parser.completed", spanId: "a", traceId: "t" },
      { event: "check.css.completed", spanId: "b", traceId: "t" },
    ]),
  );
  expect(result.spans.some((span) => span.ambiguous && span.parent === undefined)).toBe(true);
  expect(
    result.spans.every(
      (span) =>
        span.parent === undefined ||
        result.spans.find((parent) => parent.id === span.parent)?.parent !== span.id,
    ),
  ).toBe(true);
});

test("reads explicit correlation from Output text without timestamps", () => {
  const result = buildLogExecutionTree(
    parseDebugLog(
      `[asp-lsp] check.diagnostics.started spanId=root traceId=t uri=file:///a.asp
[asp-lsp] check.parser.started parentSpanId=root spanId=child traceId=t uri=file:///a.asp
[asp-lsp] check.parser in 1 ms parentSpanId=root spanId=child traceId=t uri=file:///a.asp
[asp-lsp] check.diagnostics in 2 ms spanId=root traceId=t uri=file:///a.asp`,
      "en",
    ).entries,
  );
  expect(result.spans).toHaveLength(2);
  expect(result.spans[1].parent).toBe(1);
  expect(result.spans[1].inferredParent).toBe(false);
});
