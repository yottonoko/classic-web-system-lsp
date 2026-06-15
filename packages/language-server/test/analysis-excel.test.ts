import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import ExcelJS from "exceljs";
import { describe, expect, it } from "vitest";
import {
  createAnalysisExcelSheets,
  createAnalysisExcelSheetsAsync,
} from "../src/analysis-excel/sheets";
import { writeAnalysisExcelWorkbookFile } from "../src/analysis-excel/stream-writer";
import type { AspGraphPayload } from "../src/asp-graph/types";

describe("analysis Excel sheets", () => {
  it("summarizes includes, usages, unresolved items and unused declarations", () => {
    const payload = analysisPayload();
    const sheets = createAnalysisExcelSheets(payload, "ja", {
      generatedAt: new Date("2026-06-10T00:00:00.000Z"),
      targetUri: "file:///workspace/main.asp",
      settings: {
        skipTypeInference: false,
        includeAnalysisTypeDetails: true,
        maxDocuments: 8192,
        maxTextLength: 536870912,
        includeTreeMaxDocuments: 1024,
        includeTreeMaxTextLength: 67108864,
      },
    });

    expect(sheets.map((sheet) => sheet.sheet)).toEqual([
      "概要",
      "インクルードツリー",
      "分析サマリ",
      "チャート元データ",
      "宣言",
      "ファイル内使用",
      "外部ファイルからの使用",
      "include 先シンボル使用",
      "メンバー使用",
      "暗黙global変数",
      "暗黙global変数代入候補",
      "未使用",
      "未解決",
    ]);
    expect(table(sheets, "概要")).toEqual(
      expect.arrayContaining([
        ["解析範囲", "ファイル"],
        ["ルート", "main.asp"],
        ["生成時刻", expect.stringMatching(/GMT[+-]\d{2}:\d{2}$/)],
        ["宣言数", 7],
        ["参照数", 7],
        ["代入数", 4],
        ["呼び出し数", 2],
        ["include 数", 1],
        ["未解決数", 1],
        ["暗黙global変数数", 2],
        ["暗黙global変数代入候補数", 1],
        ["未使用数", 2],
        ["切り詰め", "workspace index が 10 件を超えたため切り詰められました"],
        ["親戚 include tree 解析", "無効"],
        ["Excel 言語", "自動"],
        ["親戚 include tree 解析", "無効"],
        ["親戚 include tree 解析の強制", "強制なし"],
        ["型推論を skip", "無効"],
        ["エディター推論型の詳細", "有効"],
        ["Excel 出力 document 上限", 8192],
        ["Excel 出力 text 上限", 536870912],
        ["Excel include tree document 上限", 1024],
        ["Excel include tree text 上限", 67108864],
      ]),
    );
    const includeTreeTable = table(sheets, "インクルードツリー");
    expect(includeTreeTable).toEqual(
      expect.arrayContaining([
        [
          "方向",
          "深さ",
          "参照元ファイル",
          "include ファイル",
          "include path",
          "mode",
          "存在",
          "解決先",
          "行",
          "列",
        ],
        ["祖先", 1, "parent.asp", "main.asp", "main.asp", "ファイル", "あり", "main.asp", 1, 6],
        [
          "子孫",
          1,
          "main.asp",
          "includes/util.inc",
          "includes/util.inc",
          "ファイル",
          "あり",
          "includes/util.inc",
          1,
          6,
        ],
        [
          "親戚",
          2,
          "parent.asp",
          "includes/sibling.inc",
          "sibling.inc",
          "ファイル",
          "あり",
          "includes/sibling.inc",
          2,
          6,
        ],
      ]),
    );
    expect(JSON.stringify(includeTreeTable)).not.toContain("unrelated.asp");
    expect(table(sheets, "分析サマリ")).toEqual(
      expect.arrayContaining([
        ["未使用の宣言", 2, "要確認", "未使用 sheet で削除可否を確認"],
        ["未解決", 1, "要確認", "未解決 sheet で名前解決を確認"],
        ["暗黙global変数数", 2, "要確認", "暗黙global変数 sheet で宣言漏れか確認"],
        [
          "暗黙global変数代入候補数",
          1,
          "あり",
          "暗黙global変数代入候補 sheet で include 元からの代入を確認",
        ],
        ["他ファイルからの使用数", 4, "あり", "外部ファイルからの使用 sheet で利用元を確認"],
        [
          "include 先シンボル使用数",
          5,
          "あり",
          "include 先シンボル使用 sheet で include 依存を確認",
        ],
        ["変数", 4, 2, 2, 4, expect.stringContaining("4")],
        ["暗黙global変数", 2, 2, 0, 3, expect.stringContaining("2")],
        ["関数", 1, 1, 0, 1, expect.stringContaining("1")],
        ["参照", 3, expect.stringContaining("3")],
        ["代入", 1, expect.stringContaining("1")],
        ["呼び出し", 1, expect.stringContaining("1")],
        ["TargetValue", "変数", "main.asp", 3, 2, 1, 1, 0],
        ["MissingValue", "暗黙global変数", "main.asp", 10, 2, 1, 1, 0],
        ["MissingRead", "暗黙global変数", "main.asp", 11, 1, 1, 0, 0],
        ["TargetProc", "関数", "main.asp", 4, 1, 0, 0, 1],
        ["LocalOnlyValue", "変数", "main.asp", 6, 2, 1, 1, 0],
        ["変数", 2, 4, 0.5, expect.stringContaining("2")],
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
    expect(sheets.find((sheet) => sheet.sheet === "分析サマリ")?.stickyRowsCount).toBeUndefined();
    expect(table(sheets, "概要")).toEqual(
      expect.arrayContaining([
        ["名前", "値"],
        ["解析範囲", "ファイル"],
      ]),
    );
    expect(rawTable(sheets, "概要")).toEqual(
      expect.arrayContaining([
        expect.arrayContaining(["表の説明", "出力対象ファイルの解析件数と生成情報の概要です。"]),
        expect.arrayContaining(["名前", "宣言や項目の名前です。"]),
        expect.arrayContaining(["値", "1列目の項目や設定に対応する値です。"]),
      ]),
    );
    const declarationTable = table(sheets, "宣言");
    expect(declarationTable[0]).toEqual([
      "ファイル",
      "名前",
      "種別",
      "所属",
      "scope",
      "procedure kind",
      "推論型",
      "戻り値の型",
      "引数",
      "暗黙宣言",
      "配列",
      "行",
      "列",
      "参照数",
      "代入数",
      "呼び出し数",
      "状態",
    ]);
    expect(rawTable(sheets, "宣言")).toEqual(
      expect.arrayContaining([
        expect.arrayContaining(["表の説明", "出力対象ファイル自身にある宣言だけを並べた表です。"]),
        expect.arrayContaining([
          "推論型",
          "宣言または推論された型です。不明な型付き項目は Variant です。",
        ]),
        expect.arrayContaining([
          "戻り値の型",
          "宣言または推論された戻り値の型です。不明な Function/Property は Variant です。",
        ]),
        expect.arrayContaining([
          "引数",
          "procedure の引数と推論型です。不明な引数型は Variant です。",
        ]),
      ]),
    );
    expect(table(sheets, "チャート元データ")).toEqual(
      expect.arrayContaining([
        ["未使用の宣言", 2, "要確認", "未使用 sheet で削除可否を確認"],
        ["他ファイルからの使用数", 4, "あり", "外部ファイルからの使用 sheet で利用元を確認"],
        ["変数", 4, 2, 2, 4],
        ["暗黙global変数", 2, 2, 0, 3],
        ["関数", 1, 1, 0, 1],
      ]),
    );
    expect(sheets.some((sheet) => sheet.sheet === "被参照")).toBe(false);
    expect(sheets.find((sheet) => sheet.sheet === "チャート元データ")?.hidden).toBe(true);
    expect(
      sheets.find((sheet) => sheet.sheet === "チャート元データ")?.autoFilterRef,
    ).toBeUndefined();
    expect(sheets.find((sheet) => sheet.sheet === "概要")?.autoFilterRef).toBe("A1:B31");
    expect(sheets.find((sheet) => sheet.sheet === "宣言")?.autoFilterRef).toBe("A1:Q8");
    expect(sheets.find((sheet) => sheet.sheet === "ファイル内使用")?.autoFilterRef).toBe("A1:K5");
    expect(sheets.find((sheet) => sheet.sheet === "外部ファイルからの使用")?.autoFilterRef).toBe(
      "A1:K5",
    );
    expect(sheets.find((sheet) => sheet.sheet === "include 先シンボル使用")?.autoFilterRef).toBe(
      "A1:J6",
    );
    expect(sheets.find((sheet) => sheet.sheet === "メンバー使用")?.autoFilterRef).toBe("A1:I2");
    expect(sheets.find((sheet) => sheet.sheet === "暗黙global変数")?.autoFilterRef).toBe("A1:K3");
    expect(sheets.find((sheet) => sheet.sheet === "暗黙global変数代入候補")?.autoFilterRef).toBe(
      "A1:I2",
    );
    expect(sheets.find((sheet) => sheet.sheet === "未使用")?.autoFilterRef).toBe("A1:M3");
    expect(sheets.find((sheet) => sheet.sheet === "未解決")?.autoFilterRef).toBe("A1:I2");
    expect(declarationTable).toEqual(
      expect.arrayContaining([
        [
          "main.asp",
          "TargetValue",
          "変数",
          "",
          "グローバル",
          "",
          "String",
          "",
          "",
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
          "Long",
          "customerId As String, includeInactive As Variant",
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
          "Variant",
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
          "Variant",
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
          "MissingValue",
          "暗黙global変数",
          "",
          "グローバル",
          "",
          "Variant",
          "",
          "",
          "あり",
          "",
          10,
          5,
          1,
          1,
          0,
          "使用あり",
        ],
        [
          "main.asp",
          "MissingRead",
          "暗黙global変数",
          "",
          "グローバル",
          "",
          "Variant",
          "",
          "",
          "あり",
          "",
          11,
          5,
          1,
          0,
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
          "Variant",
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
    expect(table(sheets, "宣言").flat()).not.toContain("SharedValue");
    expect(table(sheets, "ファイル内使用")).toEqual(
      expect.arrayContaining([
        [
          "参照",
          "読み取り",
          "main.asp",
          "main.asp",
          "main.asp",
          "LocalOnlyValue",
          "変数",
          "Variant",
          15,
          5,
          1,
        ],
        [
          "代入",
          "書き込み",
          "main.asp",
          "main.asp",
          "main.asp",
          "LocalOnlyValue",
          "変数",
          "Variant",
          16,
          5,
          1,
        ],
        [
          "参照",
          "読み取り",
          "main.asp",
          "main.asp",
          "main.asp",
          "MissingValue",
          "暗黙global変数",
          "Variant",
          10,
          5,
          1,
        ],
        [
          "参照",
          "読み取り",
          "main.asp",
          "main.asp",
          "main.asp",
          "MissingRead",
          "暗黙global変数",
          "Variant",
          11,
          5,
          1,
        ],
      ]),
    );
    expect(table(sheets, "外部ファイルからの使用")).toEqual(
      expect.arrayContaining([
        [
          "参照",
          "読み取り",
          "consumer.asp",
          "consumer.asp",
          "main.asp",
          "TargetValue",
          "変数",
          "String",
          12,
          5,
          1,
        ],
        [
          "代入",
          "書き込み",
          "consumer.asp",
          "consumer.asp",
          "main.asp",
          "TargetValue",
          "変数",
          "String",
          13,
          5,
          1,
        ],
        [
          "呼び出し",
          "関数",
          "consumer.asp",
          "consumer.asp",
          "main.asp",
          "TargetProc",
          "関数",
          "Long",
          14,
          5,
          1,
        ],
        [
          "代入",
          "書き込み",
          "parent.asp",
          "parent.asp",
          "main.asp",
          "MissingValue",
          "暗黙global変数",
          "Variant",
          4,
          5,
          1,
        ],
      ]),
    );
    expect(table(sheets, "外部ファイルからの使用").flat()).not.toContain("other.asp");
    expect(table(sheets, "include 先シンボル使用")).toEqual(
      expect.arrayContaining([
        [
          "参照",
          "読み取り",
          "includes/util.inc",
          "SharedValue",
          "変数",
          "String",
          "main.asp",
          6,
          5,
          1,
        ],
        [
          "代入",
          "書き込み",
          "includes/util.inc",
          "SharedValue",
          "変数",
          "String",
          "main.asp",
          7,
          5,
          1,
        ],
        ["呼び出し", "関数", "includes/util.inc", "DoWork", "関数", "", "main.asp", 8, 5, 1],
        [
          "参照",
          "読み取り",
          "includes/util.inc",
          "SharedConst",
          "定数",
          "Long",
          "main.asp",
          16,
          5,
          1,
        ],
        [
          "参照",
          "読み取り",
          "includes/util.inc",
          "SharedClass",
          "クラス",
          "",
          "main.asp",
          17,
          5,
          1,
        ],
      ]),
    );
    expect(table(sheets, "メンバー使用")).toEqual(
      expect.arrayContaining([
        [
          "呼び出し",
          "メンバー",
          "Customer",
          "UnknownMember",
          "Customer.UnknownMember",
          "main.asp",
          18,
          5,
          1,
        ],
      ]),
    );
    expect(table(sheets, "暗黙global変数")).toEqual(
      expect.arrayContaining([
        ["main.asp", "MissingValue", "暗黙global変数", "Variant", "グローバル", 10, 5, 2, 1, 1, 0],
        ["main.asp", "MissingRead", "暗黙global変数", "Variant", "グローバル", 11, 5, 1, 1, 0, 0],
      ]),
    );
    expect(table(sheets, "暗黙global変数代入候補")).toEqual(
      expect.arrayContaining([
        ["main.asp", "MissingValue", "parent.asp", "MissingValue", "main.asp", 1, 4, 5, 1],
      ]),
    );
    expect(table(sheets, "未使用")).toEqual(
      expect.arrayContaining([
        [
          "main.asp",
          "UnusedValue",
          "変数",
          "",
          "グローバル",
          "Variant",
          "なし",
          5,
          5,
          0,
          0,
          0,
          "未使用",
        ],
        [
          "main.asp",
          "LocalValue",
          "変数",
          "",
          "ローカル",
          "Variant",
          "なし",
          7,
          5,
          0,
          0,
          0,
          "未使用",
        ],
      ]),
    );
    expect(table(sheets, "未解決")).toEqual(
      expect.arrayContaining([
        [
          "呼び出し",
          "プロシージャ",
          "未解決Function/Sub",
          "main.asp",
          "MissingProc",
          "main.asp",
          9,
          5,
          1,
        ],
      ]),
    );
    expect(table(sheets, "未解決").flat()).not.toContain("UnknownMember");
    expect(table(sheets, "未解決").flat()).not.toContain("MissingValue");
  });

  it("reports cooperative progress while building analysis sheets", async () => {
    const progressEvents: Array<{
      label: string;
      current: number;
      total: number;
      detail?: string;
      activeItems?: string[];
    }> = [];
    let yieldCount = 0;
    const sheets = await createAnalysisExcelSheetsAsync(analysisPayload(), "ja", {
      generatedAt: new Date("2026-06-10T00:00:00.000Z"),
      targetUri: "file:///workspace/main.asp",
      progress: (event) => progressEvents.push(event),
      yieldControl: async () => {
        yieldCount += 1;
        await Promise.resolve();
      },
    });

    expect(sheets.map((sheet) => sheet.sheet)).toContain("概要");
    expect(yieldCount).toBeGreaterThan(5);
    expect(progressEvents.map((event) => event.label)).toEqual(
      expect.arrayContaining([
        "excel.normalizeGraph",
        "excel.analysisContext",
        "excel.analysisSummary",
        "excel.sheet",
        "excel.sheets",
      ]),
    );
    expect(progressEvents).toEqual(
      expect.arrayContaining([expect.objectContaining({ label: "excel.sheet", detail: "概要" })]),
    );
    expect(progressEvents.at(-1)).toEqual(
      expect.objectContaining({
        label: "excel.sheets",
        current: progressEvents.at(-1)?.total,
        activeItems: [],
      }),
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
          "Variant",
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
          "Variant",
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
          "Variant",
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
          "Variant",
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
    expect(sheets.some((sheet) => sheet.sheet === "被参照")).toBe(false);
  });

  it("uses localized file-local usage labels and no-truncation text", () => {
    const jaSheets = createAnalysisExcelSheets(
      { ...analysisPayload(), truncated: undefined },
      "ja",
      {
        generatedAt: new Date("2026-06-10T00:00:00.000Z"),
        targetUri: "file:///workspace/main.asp",
      },
    );
    const enSheets = createAnalysisExcelSheets(
      { ...analysisPayload(), truncated: undefined },
      "en",
      {
        generatedAt: new Date("2026-06-10T00:00:00.000Z"),
        targetUri: "file:///workspace/main.asp",
      },
    );

    expect(jaSheets.map((sheet) => sheet.sheet)).toContain("ファイル内使用");
    expect(jaSheets.map((sheet) => sheet.sheet)).not.toContain("内部使用");
    expect(enSheets.map((sheet) => sheet.sheet)).toContain("File-local Usage");
    expect(enSheets.map((sheet) => sheet.sheet)).not.toContain("Internal Usage");
    expect(enSheets.every((sheet) => sheet.sheet.length <= 31)).toBe(true);
    expect(enSheets.map((sheet) => sheet.sheet)).not.toContain(
      "Implicit Global Assignment Candidates",
    );
    expect(enSheets.map((sheet) => sheet.sheet)).toEqual(
      expect.arrayContaining([expect.stringMatching(/^Implicit Global Assignment/)]),
    );
    expect(table(jaSheets, "概要")).toEqual(expect.arrayContaining([["切り詰め", "切り詰めなし"]]));
    expect(table(enSheets, "Summary")).toEqual(
      expect.arrayContaining([["Truncated", "Not truncated"]]),
    );
  });

  it("marks filter and hidden-sheet metadata for workbook output", () => {
    const sheets = createAnalysisExcelSheets(analysisPayload(), "ja", {
      generatedAt: new Date("2026-06-10T00:00:00.000Z"),
      targetUri: "file:///workspace/main.asp",
    });

    expect(sheets.find((sheet) => sheet.sheet === "チャート元データ")?.hidden).toBe(true);
    expect(sheets.find((sheet) => sheet.sheet === "宣言")?.autoFilterRef).toBe("A1:Q8");
  });

  it("can stream an xlsx workbook file with sheet metadata", async () => {
    const sheets = createAnalysisExcelSheets(analysisPayload(), "ja", {
      generatedAt: new Date("2026-06-10T00:00:00.000Z"),
      targetUri: "file:///workspace/main.asp",
    });
    const filename = path.join(os.tmpdir(), `asp-lsp-analysis-${process.pid}-${Date.now()}.xlsx`);
    const progressEvents: Array<{
      label: string;
      current: number;
      total: number;
      detail?: string;
      activeItems?: string[];
    }> = [];
    try {
      await writeAnalysisExcelWorkbookFile(sheets, {
        filename,
        progress: (event) => progressEvents.push(event),
      });

      const bytes = fs.readFileSync(filename);
      expect(bytes.subarray(0, 2).toString("utf8")).toBe("PK");
      expect(bytes.length).toBeGreaterThan(1000);
      expect(progressEvents.map((event) => event.label)).toEqual(
        expect.arrayContaining([
          "excel.file",
          "excel.fileSheet",
          "excel.fileRows",
          "excel.fileCommit",
        ]),
      );
      expect(progressEvents.at(0)).toEqual(
        expect.objectContaining({ label: "excel.file", current: 0 }),
      );
      expect(progressEvents.at(-1)).toEqual(
        expect.objectContaining({
          label: "excel.fileCommit",
          current: progressEvents.at(-1)?.total,
          activeItems: [],
        }),
      );

      const workbook = new ExcelJS.Workbook();
      await workbook.xlsx.readFile(filename);
      expect(workbook.getWorksheet("チャート元データ")?.state).toBe("hidden");
      expect(workbook.getWorksheet("宣言")?.autoFilter).toBe("A1:Q8");
    } finally {
      fs.rmSync(filename, { force: true });
    }
  });

  it("does not render flowchart exception handling nodes in analysis sheets", () => {
    const payload = analysisPayload();
    payload.nodes.push({
      id: "flow:on-error",
      kind: "exceptionHandling",
      label: "On Error Resume Next",
      uri: "file:///workspace/main.asp",
      range: range(12, 4),
      origin: "source",
    } as unknown as AspGraphPayload["nodes"][number]);

    const sheets = createAnalysisExcelSheets(payload, "ja", {
      generatedAt: new Date("2026-06-10T00:00:00.000Z"),
      targetUri: "file:///workspace/main.asp",
    });
    const values = sheets.flatMap((sheet) => table(sheets, sheet.sheet)).flat();

    expect(values).not.toContain("On Error Resume Next");
    expect(values).not.toContain("exceptionHandling");
  });

  it("summarizes workspace file-list exports across all files", () => {
    const payload: AspGraphPayload = {
      ...analysisPayload(),
      scope: "workspace",
      rootUri: undefined,
    };
    const sheets = createAnalysisExcelSheets(payload, "ja", {
      generatedAt: new Date("2026-06-10T00:00:00.000Z"),
      settings: {
        analysisFileCount: 7,
        includeGlobs: ["**/*.asp", "**/*.inc"],
        excludeGlobs: ["legacy/**"],
      },
    });

    expect(table(sheets, "概要")).toEqual(
      expect.arrayContaining([
        ["解析範囲", "ワークスペース"],
        ["ルート", "ワークスペース"],
        ["解析 file 数", 7],
        ["一時 include glob", "**/*.asp\n**/*.inc"],
        ["一時 exclude glob", "legacy/**"],
      ]),
    );
    expect(table(sheets, "宣言")).toEqual(
      expect.arrayContaining([
        expect.arrayContaining(["includes/util.inc", "SharedValue"]),
        expect.arrayContaining(["main.asp", "TargetValue"]),
      ]),
    );
    expect(table(sheets, "インクルードツリー")).toEqual(
      expect.arrayContaining([
        expect.arrayContaining(["子孫", 1, "main.asp", "includes/util.inc"]),
        expect.arrayContaining(["子孫", 1, "parent.asp", "main.asp"]),
      ]),
    );
  });
});

function table(sheets: ReturnType<typeof createAnalysisExcelSheets>, name: string): unknown[][] {
  return rawTable(sheets, name)
    .map(stripSideDescription)
    .filter((row) => row.length > 0);
}

function rawTable(sheets: ReturnType<typeof createAnalysisExcelSheets>, name: string): unknown[][] {
  const sheet = sheets.find((candidate) => candidate.sheet === name);
  expect(sheet).toBeDefined();
  return sheet?.data.map((row) => row.map(cellValue)) ?? [];
}

function stripSideDescription(row: unknown[]): unknown[] {
  const separator = row.findIndex((value) => value === null);
  return separator >= 0 ? row.slice(0, separator) : row;
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
  const siblingUri = "file:///workspace/includes/sibling.inc";
  const consumerUri = "file:///workspace/consumer.asp";
  const otherUri = "file:///workspace/other.asp";
  const parentUri = "file:///workspace/parent.asp";
  const unrelatedUri = "file:///workspace/unrelated.asp";
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
        id: "file:/workspace/includes/sibling.inc",
        kind: "file",
        label: "sibling.inc",
        uri: siblingUri,
        fileName: "includes/sibling.inc",
        exists: true,
      },
      {
        id: "file:/workspace/other.asp",
        kind: "file",
        label: "other.asp",
        uri: otherUri,
        fileName: "other.asp",
        exists: true,
      },
      {
        id: "file:/workspace/unrelated.asp",
        kind: "file",
        label: "unrelated.asp",
        uri: unrelatedUri,
        fileName: "unrelated.asp",
        exists: true,
      },
      {
        id: "file:/workspace/parent.asp",
        kind: "file",
        label: "parent.asp",
        uri: parentUri,
        fileName: "parent.asp",
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
        typeName: "Long",
        parameters: [{ name: "customerId", typeName: "String" }, { name: "includeInactive" }],
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
        group: "unresolvedFunction",
      },
      {
        id: "vb:missing-value",
        kind: "vbDeclaration",
        label: "MissingValue",
        uri: mainUri,
        range: range(9, 4),
        declarationKind: "variable",
        bindingScope: "global",
        implicit: true,
        implicitLocal: true,
        origin: "source",
      },
      {
        id: "vb:missing-read",
        kind: "vbDeclaration",
        label: "MissingRead",
        uri: mainUri,
        range: range(10, 4),
        declarationKind: "variable",
        bindingScope: "global",
        implicit: true,
        unresolvedGlobal: true,
        origin: "source",
      },
      {
        id: "member:customer.unknownmember",
        kind: "vbMemberReference",
        label: "UnknownMember",
        uri: mainUri,
        range: range(17, 4),
        role: "member",
        receiverName: "Customer",
        memberName: "UnknownMember",
        fullPath: "Customer.UnknownMember",
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
        id: "link:include-main-from-parent",
        source: "file:/workspace/parent.asp",
        target: "file:/workspace/main.asp",
        kind: "include",
        label: "main.asp",
        count: 1,
        ranges: [{ uri: parentUri, range: range(0, 5) }],
        include: {
          path: "main.asp",
          mode: "file",
          exists: true,
          resolvedUri: mainUri,
          pathCaseMatches: true,
        },
      },
      {
        id: "link:include-sibling-from-parent",
        source: "file:/workspace/parent.asp",
        target: "file:/workspace/includes/sibling.inc",
        kind: "include",
        label: "sibling.inc",
        count: 1,
        ranges: [{ uri: parentUri, range: range(1, 5) }],
        include: {
          path: "sibling.inc",
          mode: "file",
          exists: true,
          resolvedUri: siblingUri,
          pathCaseMatches: true,
        },
      },
      {
        id: "link:include-other-from-unrelated",
        source: "file:/workspace/unrelated.asp",
        target: "file:/workspace/other.asp",
        kind: "include",
        label: "other.asp",
        count: 1,
        ranges: [{ uri: unrelatedUri, range: range(0, 5) }],
        include: {
          path: "other.asp",
          mode: "file",
          exists: true,
          resolvedUri: otherUri,
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
        id: "link:other-ref-shared",
        source: "file:/workspace/other.asp",
        target: "vb:shared",
        kind: "references",
        label: "read",
        role: "read",
        count: 1,
        ranges: [{ uri: otherUri, range: range(20, 4) }],
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
        target: "vb:missing-value",
        kind: "references",
        label: "read",
        role: "read",
        count: 1,
        ranges: [{ uri: mainUri, range: range(9, 4) }],
      },
      {
        id: "link:ref-missing-read",
        source: "file:/workspace/main.asp",
        target: "vb:missing-read",
        kind: "references",
        label: "read",
        role: "read",
        count: 1,
        ranges: [{ uri: mainUri, range: range(10, 4) }],
      },
      {
        id: "link:assign-missing-parent",
        source: "file:/workspace/parent.asp",
        target: "vb:missing-value",
        kind: "assignments",
        label: "write",
        role: "write",
        count: 1,
        ranges: [{ uri: parentUri, range: range(3, 4) }],
      },
      {
        id: "link:member-unknown",
        source: "file:/workspace/main.asp",
        target: "member:customer.unknownmember",
        kind: "calls",
        label: "member",
        role: "member",
        count: 1,
        ranges: [{ uri: mainUri, range: range(17, 4) }],
      },
    ],
    stats: {
      files: 7,
      declarations: 11,
      references: 8,
      assignments: 4,
      calls: 4,
      unresolvedReferences: 1,
      includes: 4,
      missingIncludes: 0,
      nodes: 20,
      links: 25,
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
