import { describe, expect, it } from "vitest";
import { analysisExcelWorkbookFeatures, createAnalysisExcelSheets } from "../src/analysis-excel";
import type { AspGraphPayload } from "../src/include-graph-webview";

describe("analysis Excel sheets", () => {
  it("summarizes includes, usages, unresolved items and unused declarations", () => {
    const payload = analysisPayload();
    const sheets = createAnalysisExcelSheets(payload, "ja", {
      generatedAt: new Date("2026-06-10T00:00:00.000Z"),
      targetUri: "file:///workspace/main.asp",
    });

    expect(sheets.map((sheet) => sheet.sheet)).toEqual([
      "概要",
      "分析サマリ",
      "チャート元データ",
      "宣言",
      "被参照",
      "使用箇所",
      "参照ファイル",
      "未使用",
      "未解決",
    ]);
    expect(table(sheets, "概要")).toEqual(
      expect.arrayContaining([
        ["解析範囲", "ファイル"],
        ["ルート", "main.asp"],
        ["宣言数", 9],
        ["参照数", 5],
        ["代入数", 3],
        ["呼び出し数", 2],
        ["include 数", 1],
        ["未解決数", 2],
        ["未使用数", 2],
        ["切り詰め", "workspaceIndex>10"],
      ]),
    );
    expect(table(sheets, "分析サマリ")).toEqual(
      expect.arrayContaining([
        ["未使用の宣言", 2, "要確認", "未使用 sheet で削除可否を確認"],
        ["未解決", 2, "要確認", "未解決 sheet で名前解決を確認"],
        ["他ファイルからの使用数", 3, "あり", "使用箇所 sheet で他ファイルからの利用元を確認"],
        ["include 先シンボル使用数", 5, "あり", "参照ファイル sheet で include 依存を確認"],
        ["変数", 5, 3, 2, 6, expect.stringContaining("5")],
        ["関数", 2, 2, 0, 2, expect.stringContaining("2")],
        ["定数", 1, 1, 0, 1, expect.stringContaining("1")],
        ["クラス", 1, 1, 0, 1, expect.stringContaining("1")],
        ["参照", 3, expect.stringContaining("3")],
        ["代入", 1, expect.stringContaining("1")],
        ["呼び出し", 1, expect.stringContaining("1")],
        ["SharedValue", "変数", "includes/util.inc", 1, 2, 1, 1, 0],
        ["DoWork", "関数", "includes/util.inc", 2, 1, 0, 0, 1],
        ["SharedConst", "定数", "includes/util.inc", 3, 1, 1, 0, 0],
        ["SharedClass", "クラス", "includes/util.inc", 4, 1, 1, 0, 0],
        ["TargetValue", "変数", "main.asp", 3, 2, 1, 1, 0],
        ["TargetProc", "関数", "main.asp", 4, 1, 0, 0, 1],
        ["LocalOnlyValue", "変数", "main.asp", 6, 2, 1, 1, 0],
        ["変数", 2, 5, 0.4, expect.stringContaining("2")],
      ]),
    );
    expect(sheets.find((sheet) => sheet.sheet === "分析サマリ")?.images).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          anchor: expect.objectContaining({ column: 1 }),
          contentType: "image/svg",
          title: "使用サマリ",
        }),
        expect.objectContaining({
          anchor: expect.objectContaining({ column: 1 }),
          contentType: "image/svg",
          title: "確認項目",
        }),
      ]),
    );
    expect(sheets.find((sheet) => sheet.sheet === "分析サマリ")?.autoFilterRef).toBeUndefined();
    expect(table(sheets, "チャート元データ")).toEqual(
      expect.arrayContaining([
        ["未使用の宣言", 2, "要確認", "未使用 sheet で削除可否を確認"],
        ["変数", 5, 3, 2, 6],
        ["関数", 2, 2, 0, 2],
        ["定数", 1, 1, 0, 1],
        ["クラス", 1, 1, 0, 1],
      ]),
    );
    expect(sheets.find((sheet) => sheet.sheet === "チャート元データ")?.hidden).toBe(true);
    expect(
      sheets.find((sheet) => sheet.sheet === "チャート元データ")?.autoFilterRef,
    ).toBeUndefined();
    expect(sheets.find((sheet) => sheet.sheet === "概要")?.autoFilterRef).toBe("A1:B12");
    expect(sheets.find((sheet) => sheet.sheet === "宣言")?.autoFilterRef).toBe("A1:O10");
    expect(sheets.find((sheet) => sheet.sheet === "被参照")?.autoFilterRef).toBe("A1:K8");
    expect(sheets.find((sheet) => sheet.sheet === "使用箇所")?.autoFilterRef).toBe("A1:H4");
    expect(sheets.find((sheet) => sheet.sheet === "参照ファイル")?.autoFilterRef).toBe("A1:H6");
    expect(sheets.find((sheet) => sheet.sheet === "未使用")?.autoFilterRef).toBe("A1:M3");
    expect(sheets.find((sheet) => sheet.sheet === "未解決")?.autoFilterRef).toBe("A1:H3");
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
          "includes/util.inc",
          "DoWork",
          "関数",
          "",
          "グローバル",
          "",
          "",
          "なし",
          "",
          2,
          10,
          0,
          0,
          1,
          "使用あり",
        ],
        [
          "includes/util.inc",
          "SharedConst",
          "定数",
          "",
          "グローバル",
          "",
          "Long",
          "なし",
          "",
          3,
          5,
          1,
          0,
          0,
          "使用あり",
        ],
        [
          "includes/util.inc",
          "SharedClass",
          "クラス",
          "",
          "グローバル",
          "",
          "",
          "なし",
          "",
          4,
          5,
          1,
          0,
          0,
          "使用あり",
        ],
        [
          "main.asp",
          "TargetValue",
          "変数",
          "",
          "グローバル",
          "",
          "String",
          "なし",
          "",
          3,
          5,
          1,
          1,
          0,
          "使用あり",
        ],
        [
          "main.asp",
          "TargetProc",
          "関数",
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
          1,
          "使用あり",
        ],
        [
          "main.asp",
          "LocalOnlyValue",
          "変数",
          "",
          "グローバル",
          "",
          "",
          "なし",
          "",
          6,
          5,
          1,
          1,
          0,
          "使用あり",
        ],
        [
          "main.asp",
          "LocalValue",
          "変数",
          "",
          "ローカル",
          "",
          "",
          "なし",
          "",
          7,
          5,
          0,
          0,
          0,
          "未使用",
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
          5,
          5,
          0,
          0,
          0,
          "未使用",
        ],
      ]),
    );
    expect(table(sheets, "被参照")).toEqual(
      expect.arrayContaining([
        ["includes/util.inc", "SharedValue", "変数", "", "ソース", 1, 5, 2, 1, 1, 0],
        ["includes/util.inc", "DoWork", "関数", "", "ソース", 2, 10, 1, 0, 0, 1],
        ["includes/util.inc", "SharedConst", "定数", "", "ソース", 3, 5, 1, 1, 0, 0],
        ["includes/util.inc", "SharedClass", "クラス", "", "ソース", 4, 5, 1, 1, 0, 0],
        ["main.asp", "TargetValue", "変数", "", "ソース", 3, 5, 2, 1, 1, 0],
        ["main.asp", "TargetProc", "関数", "", "ソース", 4, 5, 1, 0, 0, 1],
        ["main.asp", "LocalOnlyValue", "変数", "", "ソース", 6, 5, 2, 1, 1, 0],
      ]),
    );
    expect(table(sheets, "使用箇所")).toEqual(
      expect.arrayContaining([
        ["参照", "読み取り", "consumer.asp", "TargetValue", "consumer.asp", 12, 5, 1],
        ["代入", "書き込み", "consumer.asp", "TargetValue", "consumer.asp", 13, 5, 1],
        ["呼び出し", "関数", "consumer.asp", "TargetProc", "consumer.asp", 14, 5, 1],
      ]),
    );
    expect(table(sheets, "参照ファイル")).toEqual(
      expect.arrayContaining([
        ["参照", "読み取り", "includes/util.inc", "SharedValue", "main.asp", 6, 5, 1],
        ["代入", "書き込み", "includes/util.inc", "SharedValue", "main.asp", 7, 5, 1],
        ["呼び出し", "関数", "includes/util.inc", "DoWork", "main.asp", 8, 5, 1],
        ["参照", "読み取り", "includes/util.inc", "SharedConst", "main.asp", 16, 5, 1],
        ["参照", "読み取り", "includes/util.inc", "SharedClass", "main.asp", 17, 5, 1],
      ]),
    );
    expect(table(sheets, "未使用")).toEqual(
      expect.arrayContaining([
        ["main.asp", "UnusedValue", "変数", "", "グローバル", "", "なし", 5, 5, 0, 0, 0, "未使用"],
        ["main.asp", "LocalValue", "変数", "", "ローカル", "", "なし", 7, 5, 0, 0, 0, "未使用"],
      ]),
    );
    expect(table(sheets, "未解決")).toEqual(
      expect.arrayContaining([
        ["呼び出し", "プロシージャ", "main.asp", "MissingProc", "main.asp", 9, 5, 1],
        ["未解決参照", "読み取り", "main.asp", "MissingValue", "main.asp", 10, 5, 1],
      ]),
    );
  });

  it("checks unused functions, local variables, constants and parameters", () => {
    const sheets = createAnalysisExcelSheets(unusedDeclarationKindsPayload(), "ja", {
      generatedAt: new Date("2026-06-10T00:00:00.000Z"),
      targetUri: "file:///workspace/coverage.asp",
    });
    const unusedRows = table(sheets, "未使用");

    expect(unusedRows).toEqual(
      expect.arrayContaining([
        [
          "coverage.asp",
          "UnusedFunction",
          "関数",
          "",
          "グローバル",
          "",
          "なし",
          2,
          5,
          0,
          0,
          0,
          "未使用",
        ],
        [
          "coverage.asp",
          "UnusedLocalValue",
          "変数",
          "",
          "ローカル",
          "",
          "なし",
          4,
          5,
          0,
          0,
          0,
          "未使用",
        ],
        [
          "coverage.asp",
          "UnusedParameter",
          "パラメーター",
          "",
          "ローカル",
          "",
          "なし",
          10,
          5,
          0,
          0,
          0,
          "未使用",
        ],
        [
          "coverage.asp",
          "UnusedConst",
          "定数",
          "",
          "グローバル",
          "",
          "なし",
          6,
          5,
          0,
          0,
          0,
          "未使用",
        ],
        [
          "coverage.asp",
          "UnusedLocalConst",
          "定数",
          "",
          "ローカル",
          "",
          "なし",
          8,
          5,
          0,
          0,
          0,
          "未使用",
        ],
      ]),
    );
    expect(unusedRows.flat()).not.toContain("UsedFunction");
    expect(unusedRows.flat()).not.toContain("UsedLocalValue");
    expect(unusedRows.flat()).not.toContain("UsedConst");
    expect(unusedRows.flat()).not.toContain("UsedLocalConst");
    expect(unusedRows.flat()).not.toContain("UsedParameter");
    expect(table(sheets, "被参照")).toEqual(
      expect.arrayContaining([
        ["coverage.asp", "UsedFunction", "関数", "", "ソース", 1, 5, 1, 0, 0, 1],
        ["coverage.asp", "UsedLocalValue", "変数", "", "ソース", 3, 5, 1, 0, 1, 0],
        ["coverage.asp", "UsedConst", "定数", "", "ソース", 5, 5, 1, 1, 0, 0],
        ["coverage.asp", "UsedLocalConst", "定数", "", "ソース", 7, 5, 1, 1, 0, 0],
        ["coverage.asp", "UsedParameter", "パラメーター", "", "ソース", 9, 5, 1, 1, 0, 0],
      ]),
    );
  });

  it("writes filter and hidden-sheet metadata through workbook features", () => {
    const sheets = createAnalysisExcelSheets(analysisPayload(), "ja", {
      generatedAt: new Date("2026-06-10T00:00:00.000Z"),
      targetUri: "file:///workspace/main.asp",
    });
    const chartDataIndex = sheets.findIndex((sheet) => sheet.sheet === "チャート元データ");
    const workbookTransform =
      analysisExcelWorkbookFeatures[0]?.files?.transform?.["xl/workbook.xml"]
        ?.transformElementAttributes;
    const worksheetTransform =
      analysisExcelWorkbookFeatures[0]?.files?.transform?.["xl/worksheets/sheet{id}.xml"]
        ?.transform;

    expect(workbookTransform).toBeDefined();
    expect(worksheetTransform).toBeDefined();
    expect(
      workbookTransform?.(
        "sheet",
        { "r:id": "rId3", name: "チャート元データ", sheetId: 3 },
        chartDataIndex,
        sheets,
        {},
      ),
    ).toMatchObject({ state: "hidden" });
    expect(
      worksheetTransform?.(
        '<worksheet><sheetData><row r="1"/></sheetData><pageMargins/></worksheet>',
        sheets.find((sheet) => sheet.sheet === "宣言")!,
        { sheetId: "4", sheetIndex: 3 },
      ),
    ).toContain('<autoFilter ref="A1:O10"/>');
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
  const consumerUri = "file:///workspace/consumer.asp";
  return {
    scope: "document",
    rootUri: mainUri,
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
        id: "file:/workspace/consumer.asp",
        kind: "file",
        label: "consumer.asp",
        uri: consumerUri,
        fileName: "consumer.asp",
        exists: true,
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
        id: "vb:shared-const",
        kind: "vbDeclaration",
        label: "SharedConst",
        uri: utilUri,
        range: range(2, 4),
        declarationKind: "constant",
        bindingScope: "global",
        typeName: "Long",
        origin: "source",
      },
      {
        id: "vb:shared-class",
        kind: "vbDeclaration",
        label: "SharedClass",
        uri: utilUri,
        range: range(3, 4),
        declarationKind: "class",
        bindingScope: "global",
        origin: "source",
      },
      {
        id: "vb:target-value",
        kind: "vbDeclaration",
        label: "TargetValue",
        uri: mainUri,
        range: range(2, 4),
        declarationKind: "variable",
        bindingScope: "global",
        typeName: "String",
        origin: "source",
      },
      {
        id: "vb:target-proc",
        kind: "vbDeclaration",
        label: "TargetProc",
        uri: mainUri,
        range: range(3, 4),
        declarationKind: "function",
        bindingScope: "global",
        origin: "source",
      },
      {
        id: "vb:unused",
        kind: "vbDeclaration",
        label: "UnusedValue",
        uri: mainUri,
        range: range(4, 4),
        declarationKind: "variable",
        bindingScope: "global",
        origin: "source",
      },
      {
        id: "vb:local-only",
        kind: "vbDeclaration",
        label: "LocalOnlyValue",
        uri: mainUri,
        range: range(5, 4),
        declarationKind: "variable",
        bindingScope: "global",
        origin: "source",
      },
      {
        id: "vb:local",
        kind: "vbDeclaration",
        label: "LocalValue",
        uri: mainUri,
        range: range(6, 4),
        declarationKind: "variable",
        bindingScope: "local",
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
        id: "link:declare-shared",
        source: "vb:shared",
        target: "file:/workspace/includes/util.inc",
        kind: "declares",
        label: "declares",
        count: 1,
        ranges: [{ uri: utilUri, range: range(0, 4) }],
      },
      {
        id: "link:declare-target-value",
        source: "vb:target-value",
        target: "file:/workspace/main.asp",
        kind: "declares",
        label: "declares",
        count: 1,
        ranges: [{ uri: mainUri, range: range(2, 4) }],
      },
      {
        id: "link:declare-target-proc",
        source: "vb:target-proc",
        target: "file:/workspace/main.asp",
        kind: "declares",
        label: "declares",
        count: 1,
        ranges: [{ uri: mainUri, range: range(3, 4) }],
      },
      {
        id: "link:declare-unused",
        source: "vb:unused",
        target: "file:/workspace/main.asp",
        kind: "declares",
        label: "declares",
        count: 1,
        ranges: [{ uri: mainUri, range: range(4, 4) }],
      },
      {
        id: "link:declare-local-only",
        source: "vb:local-only",
        target: "file:/workspace/main.asp",
        kind: "declares",
        label: "declares",
        count: 1,
        ranges: [{ uri: mainUri, range: range(5, 4) }],
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
        id: "link:ref-shared-const",
        source: "file:/workspace/main.asp",
        target: "vb:shared-const",
        kind: "references",
        label: "read",
        role: "read",
        count: 1,
        ranges: [{ uri: mainUri, range: range(15, 4) }],
      },
      {
        id: "link:ref-shared-class",
        source: "file:/workspace/main.asp",
        target: "vb:shared-class",
        kind: "references",
        label: "read",
        role: "read",
        count: 1,
        ranges: [{ uri: mainUri, range: range(16, 4) }],
      },
      {
        id: "link:ref-target-value",
        source: "file:/workspace/consumer.asp",
        target: "vb:target-value",
        kind: "references",
        label: "read",
        role: "read",
        count: 1,
        ranges: [{ uri: consumerUri, range: range(11, 4) }],
      },
      {
        id: "link:assign-target-value",
        source: "file:/workspace/consumer.asp",
        target: "vb:target-value",
        kind: "assignments",
        label: "write",
        role: "write",
        count: 1,
        ranges: [{ uri: consumerUri, range: range(12, 4) }],
      },
      {
        id: "link:call-target-proc",
        source: "file:/workspace/consumer.asp",
        target: "vb:target-proc",
        kind: "calls",
        label: "function",
        role: "function",
        count: 1,
        ranges: [{ uri: consumerUri, range: range(13, 4) }],
      },
      {
        id: "link:ref-local-only",
        source: "file:/workspace/main.asp",
        target: "vb:local-only",
        kind: "references",
        label: "read",
        role: "read",
        count: 1,
        ranges: [{ uri: mainUri, range: range(14, 4) }],
      },
      {
        id: "link:assign-local-only",
        source: "file:/workspace/main.asp",
        target: "vb:local-only",
        kind: "assignments",
        label: "write",
        role: "write",
        count: 1,
        ranges: [{ uri: mainUri, range: range(15, 4) }],
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
      declarations: 9,
      references: 5,
      assignments: 3,
      calls: 3,
      unresolvedReferences: 1,
      includes: 1,
      missingIncludes: 0,
      nodes: 14,
      links: 18,
    },
    truncated: {
      reason: "workspaceIndex>10",
    },
  };
}

function unusedDeclarationKindsPayload(): AspGraphPayload {
  const uri = "file:///workspace/coverage.asp";
  return {
    scope: "document",
    rootUri: uri,
    nodes: [
      {
        id: "file:/workspace/coverage.asp",
        kind: "file",
        label: "coverage.asp",
        uri,
        fileName: "coverage.asp",
        exists: true,
        isRoot: true,
      },
      {
        id: "vb:used-function",
        kind: "vbDeclaration",
        label: "UsedFunction",
        uri,
        range: range(0, 4),
        declarationKind: "function",
        bindingScope: "global",
        origin: "source",
      },
      {
        id: "vb:unused-function",
        kind: "vbDeclaration",
        label: "UnusedFunction",
        uri,
        range: range(1, 4),
        declarationKind: "function",
        bindingScope: "global",
        origin: "source",
      },
      {
        id: "vb:used-local",
        kind: "vbDeclaration",
        label: "UsedLocalValue",
        uri,
        range: range(2, 4),
        declarationKind: "variable",
        bindingScope: "local",
        origin: "source",
      },
      {
        id: "vb:unused-local",
        kind: "vbDeclaration",
        label: "UnusedLocalValue",
        uri,
        range: range(3, 4),
        declarationKind: "variable",
        bindingScope: "local",
        origin: "source",
      },
      {
        id: "vb:used-const",
        kind: "vbDeclaration",
        label: "UsedConst",
        uri,
        range: range(4, 4),
        declarationKind: "constant",
        bindingScope: "global",
        origin: "source",
      },
      {
        id: "vb:unused-const",
        kind: "vbDeclaration",
        label: "UnusedConst",
        uri,
        range: range(5, 4),
        declarationKind: "constant",
        bindingScope: "global",
        origin: "source",
      },
      {
        id: "vb:used-local-const",
        kind: "vbDeclaration",
        label: "UsedLocalConst",
        uri,
        range: range(6, 4),
        declarationKind: "constant",
        bindingScope: "local",
        origin: "source",
      },
      {
        id: "vb:unused-local-const",
        kind: "vbDeclaration",
        label: "UnusedLocalConst",
        uri,
        range: range(7, 4),
        declarationKind: "constant",
        bindingScope: "local",
        origin: "source",
      },
      {
        id: "vb:used-parameter",
        kind: "vbDeclaration",
        label: "UsedParameter",
        uri,
        range: range(8, 4),
        declarationKind: "parameter",
        bindingScope: "local",
        origin: "source",
      },
      {
        id: "vb:unused-parameter",
        kind: "vbDeclaration",
        label: "UnusedParameter",
        uri,
        range: range(9, 4),
        declarationKind: "parameter",
        bindingScope: "local",
        origin: "source",
      },
    ],
    links: [
      {
        id: "link:call-used-function",
        source: "file:/workspace/coverage.asp",
        target: "vb:used-function",
        kind: "calls",
        label: "function",
        role: "function",
        count: 1,
        ranges: [{ uri, range: range(10, 4) }],
      },
      {
        id: "link:assign-used-local",
        source: "file:/workspace/coverage.asp",
        target: "vb:used-local",
        kind: "assignments",
        label: "write",
        role: "write",
        count: 1,
        ranges: [{ uri, range: range(11, 4) }],
      },
      {
        id: "link:ref-used-const",
        source: "file:/workspace/coverage.asp",
        target: "vb:used-const",
        kind: "references",
        label: "read",
        role: "read",
        count: 1,
        ranges: [{ uri, range: range(12, 4) }],
      },
      {
        id: "link:ref-used-local-const",
        source: "file:/workspace/coverage.asp",
        target: "vb:used-local-const",
        kind: "references",
        label: "read",
        role: "read",
        count: 1,
        ranges: [{ uri, range: range(13, 4) }],
      },
      {
        id: "link:ref-used-parameter",
        source: "file:/workspace/coverage.asp",
        target: "vb:used-parameter",
        kind: "references",
        label: "read",
        role: "read",
        count: 1,
        ranges: [{ uri, range: range(14, 4) }],
      },
    ],
    stats: {
      files: 1,
      declarations: 9,
      references: 3,
      assignments: 1,
      calls: 1,
      unresolvedReferences: 0,
      includes: 0,
      missingIncludes: 0,
      nodes: 10,
      links: 5,
    },
  };
}

function range(line: number, character: number) {
  return {
    start: { line, character },
    end: { line, character: character + 1 },
  };
}
