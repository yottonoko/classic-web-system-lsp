import { describe, expect, it } from "vitest";
import { createAnalysisExcelSheets } from "../src/analysis-excel";
import type { AspGraphPayload } from "../src/include-graph-webview";

describe("analysis Excel sheets", () => {
  it("summarizes includes, usages, unresolved items and unused declarations", () => {
    const payload = analysisPayload();
    const sheets = createAnalysisExcelSheets(payload, "ja", {
      generatedAt: new Date("2026-06-10T00:00:00.000Z"),
    });

    expect(sheets.map((sheet) => sheet.sheet)).toEqual([
      "概要",
      "ファイル",
      "参照ファイル",
      "宣言",
      "被参照",
      "使用箇所",
      "未使用候補",
      "未解決",
    ]);
    expect(table(sheets, "概要")).toEqual(
      expect.arrayContaining([
        ["解析範囲", "ワークスペース"],
        ["未使用候補数", 1],
        ["切り詰め", "workspaceIndex>10"],
      ]),
    );
    expect(table(sheets, "参照ファイル")).toEqual(
      expect.arrayContaining([
        [
          "main.asp",
          "includes/util.inc",
          "ファイル",
          "includes/util.inc",
          "あり",
          "",
          "あり",
          1,
          6,
        ],
        ["main.asp", "/missing.inc", "仮想", "missing.inc", "なし", "", "", 2, 6],
      ]),
    );
    expect(table(sheets, "宣言")).toEqual(
      expect.arrayContaining([
        [
          "includes/util.inc",
          "SharedValue",
          "変数",
          "",
          "グローバル",
          "",
          "String",
          "なし",
          "",
          1,
          5,
          1,
          1,
          0,
          "使用あり",
        ],
        [
          "main.asp",
          "UnusedValue",
          "変数",
          "",
          "グローバル",
          "",
          "",
          "なし",
          "",
          4,
          5,
          0,
          0,
          0,
          "未使用候補",
        ],
      ]),
    );
    expect(table(sheets, "被参照")).toEqual(
      expect.arrayContaining([
        ["includes/util.inc", "SharedValue", "変数", "", "ソース", 1, 5, 2, 1, 1, 0],
        ["includes/util.inc", "DoWork", "関数", "", "ソース", 2, 10, 1, 0, 0, 1],
      ]),
    );
    expect(table(sheets, "使用箇所")).toEqual(
      expect.arrayContaining([
        ["参照", "読み取り", "main.asp", "SharedValue", "main.asp", 6, 5, 1],
        ["代入", "書き込み", "main.asp", "SharedValue", "main.asp", 7, 5, 1],
        ["呼び出し", "関数", "main.asp", "DoWork", "main.asp", 8, 5, 1],
      ]),
    );
    expect(table(sheets, "未使用候補")).toEqual(
      expect.arrayContaining([
        [
          "main.asp",
          "UnusedValue",
          "変数",
          "",
          "グローバル",
          "",
          "なし",
          4,
          5,
          0,
          0,
          0,
          "未使用候補",
        ],
      ]),
    );
    expect(table(sheets, "未解決")).toEqual(
      expect.arrayContaining([
        ["呼び出し", "プロシージャ", "main.asp", "MissingProc", "main.asp", 9, 5, 1],
        ["未解決参照", "読み取り", "main.asp", "MissingValue", "main.asp", 10, 5, 1],
      ]),
    );
  });
});

function table(sheets: ReturnType<typeof createAnalysisExcelSheets>, name: string): unknown[][] {
  const sheet = sheets.find((candidate) => candidate.sheet === name);
  expect(sheet).toBeDefined();
  return sheet?.data.slice(1).map((row) => row.map(cellValue)) ?? [];
}

function cellValue(cell: unknown): unknown {
  if (cell && typeof cell === "object" && "value" in cell) {
    return cell.value;
  }
  return cell;
}

function analysisPayload(): AspGraphPayload {
  const mainUri = "file:///workspace/main.asp";
  const utilUri = "file:///workspace/includes/util.inc";
  const missingUri = "file:///workspace/missing.inc";
  return {
    scope: "workspace",
    rootUri: "file:///workspace",
    nodes: [
      {
        id: "file:/workspace/main.asp",
        kind: "file",
        label: "main.asp",
        uri: mainUri,
        fileName: "main.asp",
        exists: true,
        isRoot: true,
      },
      {
        id: "file:/workspace/includes/util.inc",
        kind: "file",
        label: "util.inc",
        uri: utilUri,
        fileName: "includes/util.inc",
        exists: true,
      },
      {
        id: "file:/workspace/missing.inc",
        kind: "file",
        label: "missing.inc",
        uri: missingUri,
        fileName: "missing.inc",
        exists: false,
      },
      {
        id: "vb:shared",
        kind: "vbDeclaration",
        label: "SharedValue",
        uri: utilUri,
        range: range(0, 4),
        declarationKind: "variable",
        bindingScope: "global",
        typeName: "String",
        origin: "source",
      },
      {
        id: "vb:doWork",
        kind: "vbDeclaration",
        label: "DoWork",
        uri: utilUri,
        range: range(1, 9),
        declarationKind: "function",
        bindingScope: "global",
        origin: "source",
      },
      {
        id: "vb:unused",
        kind: "vbDeclaration",
        label: "UnusedValue",
        uri: mainUri,
        range: range(3, 4),
        declarationKind: "variable",
        bindingScope: "global",
        origin: "source",
      },
      {
        id: "unresolved:missingproc",
        kind: "vbUnresolved",
        label: "MissingProc",
        uri: mainUri,
        range: range(8, 4),
      },
      {
        id: "unresolved:missingvalue",
        kind: "vbUnresolved",
        label: "MissingValue",
        uri: mainUri,
        range: range(9, 4),
      },
    ],
    links: [
      {
        id: "link:include-util",
        source: "file:/workspace/main.asp",
        target: "file:/workspace/includes/util.inc",
        kind: "include",
        label: "includes/util.inc",
        count: 1,
        ranges: [{ uri: mainUri, range: range(0, 5) }],
        include: {
          path: "includes/util.inc",
          mode: "file",
          exists: true,
          resolvedUri: utilUri,
          pathCaseMatches: true,
        },
      },
      {
        id: "link:include-missing",
        source: "file:/workspace/main.asp",
        target: "file:/workspace/missing.inc",
        kind: "include",
        label: "/missing.inc",
        count: 1,
        ranges: [{ uri: mainUri, range: range(1, 5) }],
        include: {
          path: "/missing.inc",
          mode: "virtual",
          exists: false,
          resolvedUri: missingUri,
        },
      },
      {
        id: "link:declare-shared",
        source: "vb:shared",
        target: "file:/workspace/includes/util.inc",
        kind: "declares",
        label: "declares",
        count: 1,
        ranges: [{ uri: utilUri, range: range(0, 4) }],
      },
      {
        id: "link:ref-shared",
        source: "file:/workspace/main.asp",
        target: "vb:shared",
        kind: "references",
        label: "read",
        role: "read",
        count: 1,
        ranges: [{ uri: mainUri, range: range(5, 4) }],
      },
      {
        id: "link:assign-shared",
        source: "file:/workspace/main.asp",
        target: "vb:shared",
        kind: "assignments",
        label: "write",
        role: "write",
        count: 1,
        ranges: [{ uri: mainUri, range: range(6, 4) }],
      },
      {
        id: "link:call-do-work",
        source: "file:/workspace/main.asp",
        target: "vb:doWork",
        kind: "calls",
        label: "function",
        role: "function",
        count: 1,
        ranges: [{ uri: mainUri, range: range(7, 4) }],
      },
      {
        id: "link:call-missing",
        source: "file:/workspace/main.asp",
        target: "unresolved:missingproc",
        kind: "calls",
        label: "procedure",
        role: "procedure",
        count: 1,
        ranges: [{ uri: mainUri, range: range(8, 4) }],
      },
      {
        id: "link:ref-missing",
        source: "file:/workspace/main.asp",
        target: "unresolved:missingvalue",
        kind: "unresolvedReference",
        label: "read",
        role: "read",
        count: 1,
        ranges: [{ uri: mainUri, range: range(9, 4) }],
      },
    ],
    stats: {
      files: 3,
      declarations: 3,
      references: 1,
      assignments: 1,
      calls: 2,
      unresolvedReferences: 1,
      includes: 2,
      missingIncludes: 1,
      nodes: 8,
      links: 8,
    },
    truncated: {
      reason: "workspaceIndex>10",
    },
  };
}

function range(line: number, character: number) {
  return {
    start: { line, character },
    end: { line, character: character + 1 },
  };
}
