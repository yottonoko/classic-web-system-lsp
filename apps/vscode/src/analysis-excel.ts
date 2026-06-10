import type Stream from "node:stream";
import type { Blob } from "node:buffer";
import type { Cell, Feature, Sheet } from "write-excel-file/node";
import type {
  AspGraphLink,
  AspGraphLocale,
  AspGraphNode,
  AspGraphPayload,
  AspGraphRange,
} from "./include-graph-webview";
import { displayPathForUriText } from "./path-display";

type AnalysisExcelFileContent = Stream | Buffer | Blob;

export type AnalysisExcelSheet = Sheet<AnalysisExcelFileContent> & {
  autoFilterRef?: string;
  hidden?: boolean;
};
type AnalysisExcelImage = NonNullable<AnalysisExcelSheet["images"]>[number];

interface ChartDatum {
  label: string;
  value: number;
  detail?: string;
}

interface ReviewPriorityItem {
  label: string;
  count: number;
  status: string;
  action: string;
  tone: SummaryTone;
}

interface AnalysisSheetOptions {
  autoFilter?: boolean;
  hidden?: boolean;
  images?: AnalysisExcelImage[];
}

interface AnalysisExcelOptions {
  generatedAt?: Date;
  targetUri?: string;
}

interface UsageCounts {
  references: number;
  assignments: number;
  calls: number;
}

type SummaryTone = "good" | "warning" | "danger" | "info" | "neutral";

interface AnalysisContext {
  targetUri?: string;
  targetFileName: string;
  targetDeclarations: AspGraphNode[];
  targetDeclarationIds: Set<string>;
  includedFileUris: Set<string>;
  includedDeclarationIds: Set<string>;
  externalUsageLinks: AspGraphLink[];
  includedUsageLinks: AspGraphLink[];
  unresolvedLinks: AspGraphLink[];
  externalUsageCounts: Map<string, UsageCounts>;
  unusedDeclarations: AspGraphNode[];
}

type AnalysisTextKey =
  | "summary"
  | "analysisSummary"
  | "chartData"
  | "files"
  | "includes"
  | "declarations"
  | "referenced"
  | "usages"
  | "unused"
  | "unresolved"
  | "scope"
  | "value"
  | "root"
  | "generatedAt"
  | "filesCount"
  | "declarationsCount"
  | "referencesCount"
  | "assignmentsCount"
  | "callsCount"
  | "includesCount"
  | "missingIncludesCount"
  | "unresolvedCount"
  | "unusedCount"
  | "truncated"
  | "yes"
  | "no"
  | "used"
  | "unusedStatus"
  | "file"
  | "exists"
  | "rootFile"
  | "includesOut"
  | "includedBy"
  | "declarationCount"
  | "sourceFile"
  | "includePath"
  | "includeMode"
  | "resolvedTarget"
  | "actualPath"
  | "pathCaseMatches"
  | "line"
  | "column"
  | "name"
  | "kind"
  | "memberOf"
  | "bindingScope"
  | "procedureKind"
  | "type"
  | "implicit"
  | "array"
  | "referenceCount"
  | "assignmentCount"
  | "callCount"
  | "status"
  | "origin"
  | "usageCount"
  | "usageKind"
  | "role"
  | "source"
  | "target"
  | "count"
  | "metric"
  | "total"
  | "usedCount"
  | "share"
  | "unusedRate"
  | "bar"
  | "declarationsByKind"
  | "scopeComparison"
  | "usageMix"
  | "unusedByKind"
  | "fileHealth"
  | "unresolvedUsageCount"
  | "risk"
  | "reviewPriority"
  | "externalReferenceSummary"
  | "includeUsageSummary"
  | "topReferencedDeclarations"
  | "analysisCharts"
  | "unreferencedDeclarations"
  | "externalUsageCount"
  | "includedUsageCount"
  | "issueSummary"
  | "action"
  | "needsReview"
  | "ok"
  | "present"
  | "none"
  | "reviewUnusedAction"
  | "reviewUnresolvedAction"
  | "reviewExternalUsagesAction"
  | "reviewMissingExternalUsagesAction"
  | "reviewIncludedUsagesAction"
  | "reviewMissingIncludedUsagesAction";

const text: Record<AspGraphLocale, Record<AnalysisTextKey, string>> = {
  en: {
    summary: "Summary",
    analysisSummary: "Analysis Summary",
    chartData: "Chart Data",
    files: "Files",
    includes: "Includes",
    declarations: "Declarations",
    referenced: "Referenced",
    usages: "Usages",
    unused: "Unused",
    unresolved: "Unresolved",
    scope: "Scope",
    value: "Value",
    root: "Root",
    generatedAt: "Generated at",
    filesCount: "Files",
    declarationsCount: "Declarations",
    referencesCount: "References",
    assignmentsCount: "Assignments",
    callsCount: "Calls",
    includesCount: "Includes",
    missingIncludesCount: "Missing includes",
    unresolvedCount: "Unresolved",
    unusedCount: "Unused",
    truncated: "Truncated",
    yes: "Yes",
    no: "No",
    used: "Used",
    unusedStatus: "Unused",
    file: "File",
    exists: "Exists",
    rootFile: "Root",
    includesOut: "Includes",
    includedBy: "Included by",
    declarationCount: "Declarations",
    sourceFile: "Source file",
    includePath: "Include path",
    includeMode: "Mode",
    resolvedTarget: "Resolved target",
    actualPath: "Actual path",
    pathCaseMatches: "Path case matches",
    line: "Line",
    column: "Column",
    name: "Name",
    kind: "Kind",
    memberOf: "Member of",
    bindingScope: "Binding scope",
    procedureKind: "Procedure kind",
    type: "Type",
    implicit: "Implicit",
    array: "Array",
    referenceCount: "References",
    assignmentCount: "Assignments",
    callCount: "Calls",
    status: "Status",
    origin: "Origin",
    usageCount: "Usage count",
    usageKind: "Usage kind",
    role: "Role",
    source: "Source",
    target: "Target",
    count: "Count",
    metric: "Metric",
    total: "Total",
    usedCount: "Used",
    share: "Share",
    unusedRate: "Unused rate",
    bar: "Bar",
    declarationsByKind: "Declarations by kind",
    scopeComparison: "Global vs local",
    usageMix: "Usage mix",
    unusedByKind: "Unused by kind",
    fileHealth: "File health",
    unresolvedUsageCount: "Unresolved usages",
    risk: "Risk",
    reviewPriority: "Review priority",
    externalReferenceSummary: "External reference summary",
    includeUsageSummary: "Included file usage",
    topReferencedDeclarations: "Top referenced declarations",
    analysisCharts: "Charts",
    unreferencedDeclarations: "Declarations unreferenced by other files",
    externalUsageCount: "External usages",
    includedUsageCount: "Included symbol usages",
    issueSummary: "Issue summary",
    action: "Action",
    needsReview: "Review needed",
    ok: "OK",
    present: "Present",
    none: "None",
    reviewUnusedAction: "Review the Unused sheet before removing declarations.",
    reviewUnresolvedAction: "Review the Unresolved sheet and fix name resolution.",
    reviewExternalUsagesAction: "Review Referenced and Usages sheets for callers.",
    reviewMissingExternalUsagesAction: "No other-file usages were found for the target file.",
    reviewIncludedUsagesAction: "Review Included file usage rows for include dependencies.",
    reviewMissingIncludedUsagesAction: "No included-file symbol usages were found.",
  },
  ja: {
    summary: "概要",
    analysisSummary: "分析サマリ",
    chartData: "チャート元データ",
    files: "ファイル",
    includes: "参照ファイル",
    declarations: "宣言",
    referenced: "被参照",
    usages: "使用箇所",
    unused: "未使用",
    unresolved: "未解決",
    scope: "解析範囲",
    value: "値",
    root: "ルート",
    generatedAt: "生成時刻",
    filesCount: "ファイル数",
    declarationsCount: "宣言数",
    referencesCount: "参照数",
    assignmentsCount: "代入数",
    callsCount: "呼び出し数",
    includesCount: "include 数",
    missingIncludesCount: "missing include 数",
    unresolvedCount: "未解決数",
    unusedCount: "未使用数",
    truncated: "切り詰め",
    yes: "あり",
    no: "なし",
    used: "使用あり",
    unusedStatus: "未使用",
    file: "ファイル",
    exists: "存在",
    rootFile: "ルート",
    includesOut: "参照しているファイル数",
    includedBy: "参照されている数",
    declarationCount: "宣言数",
    sourceFile: "参照元ファイル",
    includePath: "include path",
    includeMode: "mode",
    resolvedTarget: "解決先",
    actualPath: "実際の path",
    pathCaseMatches: "大文字小文字一致",
    line: "行",
    column: "列",
    name: "名前",
    kind: "種別",
    memberOf: "所属",
    bindingScope: "scope",
    procedureKind: "procedure kind",
    type: "型",
    implicit: "暗黙宣言",
    array: "配列",
    referenceCount: "参照数",
    assignmentCount: "代入数",
    callCount: "呼び出し数",
    status: "状態",
    origin: "由来",
    usageCount: "使用数",
    usageKind: "使用種別",
    role: "role",
    source: "使用元",
    target: "対象",
    count: "数",
    metric: "指標",
    total: "合計",
    usedCount: "使用あり",
    share: "比率",
    unusedRate: "未使用率",
    bar: "棒グラフ",
    declarationsByKind: "種別ごとの宣言",
    scopeComparison: "グローバルとローカル",
    usageMix: "使用種別",
    unusedByKind: "種別ごとの未使用",
    fileHealth: "ファイル別の状態",
    unresolvedUsageCount: "未解決の使用数",
    risk: "リスク",
    reviewPriority: "確認優先",
    externalReferenceSummary: "他ファイル参照サマリ",
    includeUsageSummary: "include 先の使用",
    topReferencedDeclarations: "よく使われている宣言",
    analysisCharts: "グラフ",
    unreferencedDeclarations: "他ファイルから未参照の宣言",
    externalUsageCount: "他ファイルからの使用数",
    includedUsageCount: "include 先シンボル使用数",
    issueSummary: "確認項目",
    action: "対応",
    needsReview: "要確認",
    ok: "問題なし",
    present: "あり",
    none: "なし",
    reviewUnusedAction: "未使用 sheet で削除可否を確認",
    reviewUnresolvedAction: "未解決 sheet で名前解決を確認",
    reviewExternalUsagesAction: "被参照と使用箇所 sheet で利用元を確認",
    reviewMissingExternalUsagesAction: "対象ファイルは他ファイルから使われていない可能性あり",
    reviewIncludedUsagesAction: "参照ファイル sheet で include 依存を確認",
    reviewMissingIncludedUsagesAction: "include 先シンボルの使用なし",
  },
};

const valueText: Record<AspGraphLocale, Record<string, string>> = {
  en: {
    document: "Document",
    folder: "Folder",
    workspace: "Workspace",
    file: "File",
    virtual: "Virtual",
    function: "Function",
    sub: "Sub",
    class: "Class",
    method: "Method",
    property: "Property",
    field: "Field",
    parameter: "Parameter",
    variable: "Variable",
    constant: "Constant",
    object: "Object",
    event: "Event",
    global: "Global",
    local: "Local",
    unknown: "Unknown",
    source: "Source",
    builtin: "Built-in",
    configured: "Configured",
    references: "References",
    assignments: "Assignments",
    calls: "Calls",
    unresolvedReference: "Unresolved reference",
    include: "Includes",
    missingIncludes: "Missing includes",
    read: "Read",
    write: "Write",
    procedure: "Procedure",
    member: "Member",
    fixed: "Fixed array",
    dynamic: "Dynamic array",
    array: "Array",
  },
  ja: {
    document: "ドキュメント",
    folder: "フォルダー",
    workspace: "ワークスペース",
    file: "ファイル",
    virtual: "仮想",
    function: "関数",
    sub: "Sub",
    class: "クラス",
    method: "メソッド",
    property: "プロパティ",
    field: "フィールド",
    parameter: "パラメーター",
    variable: "変数",
    constant: "定数",
    object: "オブジェクト",
    event: "イベント",
    global: "グローバル",
    local: "ローカル",
    unknown: "不明",
    source: "ソース",
    builtin: "組み込み",
    configured: "設定",
    references: "参照",
    assignments: "代入",
    calls: "呼び出し",
    unresolvedReference: "未解決参照",
    include: "include",
    missingIncludes: "missing include",
    read: "読み取り",
    write: "書き込み",
    procedure: "プロシージャ",
    member: "メンバー",
    fixed: "固定配列",
    dynamic: "動的配列",
    array: "配列",
  },
};

const SHEET_DATA_END_TAG = "</sheetData>";

export const analysisExcelWorkbookFeatures: Feature<AnalysisExcelFileContent>[] = [
  {
    files: {
      transform: {
        "xl/workbook.xml": {
          transformElementAttributes(tagName, attributes, index, sheetsOptions) {
            if (tagName !== "sheet" || index === undefined) {
              return attributes;
            }
            const sheetOptions = sheetsOptions[index] as AnalysisExcelSheet | undefined;
            return sheetOptions?.hidden === true ? { ...attributes, state: "hidden" } : attributes;
          },
        },
        "xl/worksheets/sheet{id}.xml": {
          transform(content, sheetOptions) {
            const ref = (sheetOptions as AnalysisExcelSheet | undefined)?.autoFilterRef;
            if (!ref || content.includes("<autoFilter ")) {
              return content;
            }
            return content.replace(
              SHEET_DATA_END_TAG,
              `${SHEET_DATA_END_TAG}<autoFilter ref="${escapeXml(ref)}"/>`,
            );
          },
        },
      },
    },
  },
];

export function createAnalysisExcelSheets(
  payload: AspGraphPayload,
  locale: AspGraphLocale,
  options: AnalysisExcelOptions = {},
): AnalysisExcelSheet[] {
  const generatedAt = options.generatedAt ?? new Date();
  const nodesById = new Map(payload.nodes.map((node) => [node.id, node]));
  const fileNamesByUri = fileNamesByUriMap(payload.nodes);
  const context = analysisContext(payload, options, nodesById, fileNamesByUri);
  const analysisRows = analysisSummaryRows(locale, context);
  const analysisChartStartRow = analysisRows.length + 3;
  const analysisRowsWithChartSpace = [
    ...analysisRows,
    [],
    sectionTitle(text[locale].analysisCharts, 1),
    ...blankRows(34),
  ];
  return [
    sheet(text[locale].summary, summaryRows(payload, locale, generatedAt, context)),
    sheet(text[locale].analysisSummary, analysisRowsWithChartSpace, {
      autoFilter: false,
      images: analysisSummaryImages(locale, context, analysisChartStartRow),
    }),
    sheet(text[locale].chartData, chartDataRows(locale, context), {
      autoFilter: false,
      hidden: true,
    }),
    sheet(
      text[locale].declarations,
      declarationRows(
        context.targetDeclarations,
        locale,
        context.externalUsageCounts,
        fileNamesByUri,
      ),
    ),
    sheet(
      text[locale].referenced,
      referencedRows(
        context.targetDeclarations,
        locale,
        context.externalUsageCounts,
        fileNamesByUri,
      ),
    ),
    sheet(
      text[locale].usages,
      usageRows(context.externalUsageLinks, locale, nodesById, fileNamesByUri),
    ),
    sheet(
      text[locale].includes,
      includedUsageRows(context.includedUsageLinks, locale, nodesById, fileNamesByUri),
    ),
    sheet(
      text[locale].unused,
      unusedDeclarationRows(
        context.unusedDeclarations,
        locale,
        context.externalUsageCounts,
        fileNamesByUri,
      ),
    ),
    sheet(
      text[locale].unresolved,
      unresolvedRows(context.unresolvedLinks, locale, nodesById, fileNamesByUri),
    ),
  ];
}

function summaryRows(
  payload: AspGraphPayload,
  locale: AspGraphLocale,
  generatedAt: Date,
  context: AnalysisContext,
): Cell[][] {
  const t = text[locale];
  const rows: Array<[string, string | number]> = [
    [t.scope, valueLabel("file", locale)],
    [t.root, context.targetFileName],
    [t.generatedAt, generatedAt.toISOString()],
    [t.declarationsCount, context.targetDeclarations.length],
    [t.referencesCount, usageLinkCount(context.externalUsageLinks, "references")],
    [t.assignmentsCount, usageLinkCount(context.externalUsageLinks, "assignments")],
    [t.callsCount, usageLinkCount(context.externalUsageLinks, "calls")],
    [t.includesCount, context.includedFileUris.size],
    [t.unresolvedCount, usageLinkCount(context.unresolvedLinks)],
    [t.unusedCount, context.unusedDeclarations.length],
    [t.truncated, payload.truncated?.reason ?? ""],
  ];
  return [header([t.name, t.value ?? "Value"]), ...rows.map(([name, value]) => [name, value])];
}

function analysisSummaryRows(locale: AspGraphLocale, context: AnalysisContext): Cell[][] {
  const t = text[locale];
  return [
    sectionTitle(t.reviewPriority, 4),
    header([t.metric, t.count, t.status, t.action]),
    ...reviewPriorityRows(locale, context),
    [],
    sectionTitle(t.externalReferenceSummary, 6),
    header([t.kind, t.total, t.usedCount, t.unusedCount, t.usageCount, t.bar]),
    ...declarationKindSummaryRows(context.targetDeclarations, locale, context.externalUsageCounts),
    [],
    sectionTitle(t.includeUsageSummary, 3),
    header([t.usageKind, t.count, t.bar]),
    ...usageCountRows(context.includedUsageLinks, locale),
    [],
    sectionTitle(t.topReferencedDeclarations, 8),
    header([
      t.name,
      t.kind,
      t.file,
      t.line,
      t.usageCount,
      t.referenceCount,
      t.assignmentCount,
      t.callCount,
    ]),
    ...topReferencedDeclarationRows(
      context.targetDeclarations,
      locale,
      context.externalUsageCounts,
      new Map(context.targetDeclarations.map((node) => [node.uri ?? "", context.targetFileName])),
    ),
    [],
    sectionTitle(t.unusedByKind, 5),
    header([t.kind, t.unusedCount, t.total, t.unusedRate, t.bar]),
    ...unusedByKindRows(context.unusedDeclarations, context.targetDeclarations, locale),
  ];
}

function chartDataRows(locale: AspGraphLocale, context: AnalysisContext): Cell[][] {
  const t = text[locale];
  return [
    sectionTitle(t.reviewPriority, 4),
    header([t.metric, t.count, t.status, t.action]),
    ...reviewPriorityRows(locale, context),
    [],
    sectionTitle(t.externalReferenceSummary, 5),
    header([t.kind, t.total, t.usedCount, t.unusedCount, t.usageCount]),
    ...declarationKindSummaryRows(
      context.targetDeclarations,
      locale,
      context.externalUsageCounts,
    ).map((row) => row.slice(0, 5)),
    [],
    sectionTitle(t.includeUsageSummary, 2),
    header([t.usageKind, t.count]),
    ...usageCountRows(context.includedUsageLinks, locale).map((row) => row.slice(0, 2)),
    [],
    sectionTitle(t.unusedByKind, 4),
    header([t.kind, t.unusedCount, t.total, t.unusedRate]),
    ...unusedByKindRows(context.unusedDeclarations, context.targetDeclarations, locale).map((row) =>
      row.slice(0, 4),
    ),
  ];
}

function analysisSummaryImages(
  locale: AspGraphLocale,
  context: AnalysisContext,
  startRow: number,
): AnalysisExcelImage[] {
  const t = text[locale];
  const declarationRows = declarationKindSummaryRows(
    context.targetDeclarations,
    locale,
    context.externalUsageCounts,
  );
  return [
    chartImage(
      barChartSvg(t.externalReferenceSummary, chartDataFromRows(declarationRows, 0, 1, 3), [
        "#2563EB",
        "#16A34A",
        "#DC2626",
        "#7C3AED",
        "#0891B2",
      ]),
      t.externalReferenceSummary,
      startRow,
    ),
    chartImage(
      barChartSvg(t.issueSummary, reviewPriorityChartData(locale, context), [
        "#DC2626",
        "#EA580C",
        "#2563EB",
        "#16A34A",
      ]),
      t.issueSummary,
      startRow + 17,
    ),
  ];
}

function chartDataFromRows(
  rows: Cell[][],
  labelIndex: number,
  valueIndex: number,
  detailIndex?: number,
): ChartDatum[] {
  return rows
    .map((row) => ({
      label: String(cellValue(row[labelIndex]) ?? ""),
      value: Number(cellValue(row[valueIndex]) ?? 0),
      detail:
        detailIndex === undefined || cellValue(row[detailIndex]) === undefined
          ? undefined
          : String(cellValue(row[detailIndex])),
    }))
    .filter((item) => item.label && Number.isFinite(item.value) && item.value > 0);
}

function chartImage(svg: string, title: string, row: number): AnalysisExcelImage {
  return {
    content: Buffer.from(svg, "utf8"),
    contentType: "image/svg",
    width: 640,
    height: 260,
    dpi: 96,
    anchor: { row, column: 1 },
    title,
    description: title,
  };
}

function barChartSvg(title: string, data: ChartDatum[], colors: string | string[]): string {
  const width = 640;
  const height = 260;
  const chartX = 190;
  const chartY = 46;
  const chartWidth = 360;
  const rowHeight = data.length > 0 ? Math.min(26, 166 / data.length) : 24;
  const max = Math.max(1, ...data.map((item) => item.value));
  const rows = data.slice(0, 8);
  return svgFrame(
    width,
    height,
    title,
    rows
      .map((item, index) => {
        const y = chartY + index * rowHeight;
        const barWidth = Math.max(3, (item.value / max) * chartWidth);
        const color = Array.isArray(colors) ? colors[index % colors.length] : colors;
        return [
          `<text x="18" y="${y + 15}" class="label">${escapeXml(truncateLabel(item.label, 24))}</text>`,
          `<rect x="${chartX}" y="${y}" width="${barWidth.toFixed(1)}" height="${Math.max(10, rowHeight - 7).toFixed(1)}" rx="3" fill="${color}"/>`,
          `<text x="${chartX + barWidth + 8}" y="${y + 15}" class="value">${item.value}${item.detail ? ` / ${escapeXml(item.detail)}` : ""}</text>`,
        ].join(" ");
      })
      .join("\n"),
  );
}

function svgFrame(width: number, height: number, title: string, body: string): string {
  return `<svg xmlns="http://www.w3.org/2000/svg" width="${width}" height="${height}" viewBox="0 0 ${width} ${height}">
  <style>
    .title { font: 700 17px system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; fill: #1f2937; }
    .label { font: 12px system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; fill: #374151; }
    .value { font: 700 12px system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; fill: #111827; }
  </style>
  <rect x="0" y="0" width="${width}" height="${height}" rx="10" fill="#ffffff"/>
  <rect x="0.5" y="0.5" width="${width - 1}" height="${height - 1}" rx="9.5" fill="none" stroke="#d1d5db"/>
  <text x="18" y="28" class="title">${escapeXml(title)}</text>
  ${body || `<text x="18" y="72" class="label">No data</text>`}
</svg>`;
}

function escapeXml(value: string): string {
  return value
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;");
}

function reviewPriorityRows(locale: AspGraphLocale, context: AnalysisContext): Cell[][] {
  return reviewPriorityItems(locale, context).map((item) => [
    item.label,
    item.count,
    toneCell(item.status, item.tone),
    item.action,
  ]);
}

function reviewPriorityItems(
  locale: AspGraphLocale,
  context: AnalysisContext,
): ReviewPriorityItem[] {
  const t = text[locale];
  const externalUsageCount = usageLinkCount(context.externalUsageLinks);
  const includedUsageCount = usageLinkCount(context.includedUsageLinks);
  const unresolvedCount = usageLinkCount(context.unresolvedLinks);
  const unusedCount = context.unusedDeclarations.length;
  return [
    {
      label: t.unreferencedDeclarations,
      count: unusedCount,
      status: unusedCount > 0 ? t.needsReview : t.ok,
      action: unusedCount > 0 ? t.reviewUnusedAction : t.ok,
      tone: unusedCount > 0 ? "warning" : "good",
    },
    {
      label: t.unresolved,
      count: unresolvedCount,
      status: unresolvedCount > 0 ? t.needsReview : t.ok,
      action: unresolvedCount > 0 ? t.reviewUnresolvedAction : t.ok,
      tone: unresolvedCount > 0 ? "danger" : "good",
    },
    {
      label: t.externalUsageCount,
      count: externalUsageCount,
      status: externalUsageCount > 0 ? t.present : t.none,
      action:
        externalUsageCount > 0 ? t.reviewExternalUsagesAction : t.reviewMissingExternalUsagesAction,
      tone: externalUsageCount > 0 ? "info" : "warning",
    },
    {
      label: t.includedUsageCount,
      count: includedUsageCount,
      status: includedUsageCount > 0 ? t.present : t.none,
      action:
        includedUsageCount > 0 ? t.reviewIncludedUsagesAction : t.reviewMissingIncludedUsagesAction,
      tone: includedUsageCount > 0 ? "info" : "neutral",
    },
  ];
}

function reviewPriorityChartData(locale: AspGraphLocale, context: AnalysisContext): ChartDatum[] {
  return reviewPriorityItems(locale, context)
    .map((item) => ({
      label: item.label,
      value: item.count,
      detail: item.status,
    }))
    .filter((item) => item.value > 0);
}

function declarationKindSummaryRows(
  declarations: AspGraphNode[],
  locale: AspGraphLocale,
  usageCounts: Map<string, UsageCounts>,
): Cell[][] {
  const counts = new Map<
    string,
    { total: number; used: number; unused: number; usageCount: number }
  >();
  for (const node of declarations) {
    const key = node.declarationKind ?? "unknown";
    const entry = counts.get(key) ?? { total: 0, used: 0, unused: 0, usageCount: 0 };
    const usage = usageTotal(usageCounts.get(node.id));
    entry.total += 1;
    entry.usageCount += usage;
    if (usage > 0) {
      entry.used += 1;
    } else {
      entry.unused += 1;
    }
    counts.set(key, entry);
  }
  const max = maxCount([...counts.values()].map((entry) => entry.total));
  return [...counts.entries()]
    .sort((left, right) => right[1].total - left[1].total || compareValues(left[0], right[0]))
    .map(([kind, entry]) => [
      valueLabel(kind, locale),
      entry.total,
      entry.used,
      entry.unused,
      entry.usageCount,
      barString(entry.total, max),
    ]);
}

function usageCountRows(usageLinks: AspGraphLink[], locale: AspGraphLocale): Cell[][] {
  const rows: Array<[string, number]> = [
    ["references", usageLinkCount(usageLinks, "references")],
    ["assignments", usageLinkCount(usageLinks, "assignments")],
    ["calls", usageLinkCount(usageLinks, "calls")],
  ];
  const max = maxCount(rows.map(([, count]) => count));
  return rows
    .filter(([, count]) => count > 0)
    .map(([kind, count]) => [valueLabel(kind, locale), count, barString(count, max)]);
}

function topReferencedDeclarationRows(
  declarations: AspGraphNode[],
  locale: AspGraphLocale,
  usageCounts: Map<string, UsageCounts>,
  fileNamesByUri: Map<string, string>,
): Cell[][] {
  return declarations
    .map((node) => ({ node, usage: usageCounts.get(node.id) }))
    .filter(({ usage }) => usageTotal(usage) > 0)
    .sort((left, right) => {
      const usageDifference = usageTotal(right.usage) - usageTotal(left.usage);
      return usageDifference || compareValues(left.node.label, right.node.label);
    })
    .slice(0, 10)
    .map(({ node, usage }) => [
      node.label,
      valueLabel(node.declarationKind, locale),
      displayNameForUri(node.uri, fileNamesByUri),
      oneBasedLine(node.range),
      usageTotal(usage),
      usage?.references ?? 0,
      usage?.assignments ?? 0,
      usage?.calls ?? 0,
    ]);
}

function unusedByKindRows(
  unusedDeclarations: AspGraphNode[],
  declarations: AspGraphNode[],
  locale: AspGraphLocale,
): Cell[][] {
  const counts = new Map<string, number>();
  for (const node of unusedDeclarations) {
    const key = node.declarationKind ?? "unknown";
    counts.set(key, (counts.get(key) ?? 0) + 1);
  }
  const totals = new Map<string, number>();
  for (const node of declarations) {
    const key = node.declarationKind ?? "unknown";
    totals.set(key, (totals.get(key) ?? 0) + 1);
  }
  const max = maxCount([...counts.values()]);
  return [...counts.entries()]
    .sort((left, right) => right[1] - left[1] || compareValues(left[0], right[0]))
    .map(([kind, count]) => {
      const total = totals.get(kind) ?? count;
      return [
        valueLabel(kind, locale),
        count,
        total,
        percentCell(ratio(count, total)),
        barString(count, max),
      ];
    });
}

function includedUsageRows(
  links: AspGraphLink[],
  locale: AspGraphLocale,
  nodesById: Map<string, AspGraphNode>,
  fileNamesByUri: Map<string, string>,
): Cell[][] {
  const t = text[locale];
  const rows = links
    .flatMap((link) => {
      const target = nodesById.get(link.target);
      return rangesForLink(link).map(({ uri, range }) => [
        valueLabel(link.kind, locale),
        valueLabel(link.role ?? link.label, locale),
        displayNameForUri(target?.uri, fileNamesByUri),
        target?.label ?? link.target,
        displayNameForUri(uri, fileNamesByUri),
        oneBasedLine(range),
        oneBasedColumn(range),
        link.ranges.length > 1 ? 1 : link.count,
      ]);
    })
    .sort(compareRows(2, 3, 4, 5, 0));
  return [
    header([t.usageKind, t.role, t.file, t.name, t.sourceFile, t.line, t.column, t.count]),
    ...rows,
  ];
}

function declarationRows(
  declarations: AspGraphNode[],
  locale: AspGraphLocale,
  usageCounts: Map<string, UsageCounts>,
  fileNamesByUri: Map<string, string>,
): Cell[][] {
  const t = text[locale];
  const rows = declarations
    .sort(compareNodesByLocation(fileNamesByUri))
    .map((node) => declarationRow(node, locale, usageCounts, fileNamesByUri));
  return [
    header([
      t.file,
      t.name,
      t.kind,
      t.memberOf,
      t.bindingScope,
      t.procedureKind,
      t.type,
      t.implicit,
      t.array,
      t.line,
      t.column,
      t.referenceCount,
      t.assignmentCount,
      t.callCount,
      t.status,
    ]),
    ...rows,
  ];
}

function referencedRows(
  declarations: AspGraphNode[],
  locale: AspGraphLocale,
  usageCounts: Map<string, UsageCounts>,
  fileNamesByUri: Map<string, string>,
): Cell[][] {
  const t = text[locale];
  const rows = declarations
    .filter((node) => usageTotal(usageCounts.get(node.id)) > 0)
    .sort((left, right) => {
      const usage = usageTotal(usageCounts.get(right.id)) - usageTotal(usageCounts.get(left.id));
      return usage || compareNodesByLocation(fileNamesByUri)(left, right);
    })
    .map((node) => {
      const usage = usageCounts.get(node.id);
      return [
        displayNameForUri(node.uri, fileNamesByUri),
        node.label,
        valueLabel(node.declarationKind, locale),
        node.memberOf ?? "",
        valueLabel(node.origin, locale),
        oneBasedLine(node.range),
        oneBasedColumn(node.range),
        usageTotal(usage),
        usage?.references ?? 0,
        usage?.assignments ?? 0,
        usage?.calls ?? 0,
      ];
    });
  return [
    header([
      t.file,
      t.name,
      t.kind,
      t.memberOf,
      t.origin,
      t.line,
      t.column,
      t.usageCount,
      t.referenceCount,
      t.assignmentCount,
      t.callCount,
    ]),
    ...rows,
  ];
}

function usageRows(
  links: AspGraphLink[],
  locale: AspGraphLocale,
  nodesById: Map<string, AspGraphNode>,
  fileNamesByUri: Map<string, string>,
): Cell[][] {
  const t = text[locale];
  const rows = links
    .flatMap((link) => usageLikeLinkRows(link, locale, nodesById, fileNamesByUri))
    .sort(compareRows(4, 5, 0, 3));
  return [
    header([t.usageKind, t.role, t.source, t.target, t.file, t.line, t.column, t.count]),
    ...rows,
  ];
}

function unusedDeclarationRows(
  unusedDeclarations: AspGraphNode[],
  locale: AspGraphLocale,
  usageCounts: Map<string, UsageCounts>,
  fileNamesByUri: Map<string, string>,
): Cell[][] {
  const t = text[locale];
  return [
    header([
      t.file,
      t.name,
      t.kind,
      t.memberOf,
      t.bindingScope,
      t.type,
      t.implicit,
      t.line,
      t.column,
      t.referenceCount,
      t.assignmentCount,
      t.callCount,
      t.status,
    ]),
    ...unusedDeclarations.map((node) => {
      const usage = usageCounts.get(node.id);
      return [
        displayNameForUri(node.uri, fileNamesByUri),
        node.label,
        valueLabel(node.declarationKind, locale),
        node.memberOf ?? "",
        valueLabel(node.bindingScope, locale),
        node.typeName ?? "",
        yn(node.implicit === true, locale),
        oneBasedLine(node.range),
        oneBasedColumn(node.range),
        usage?.references ?? 0,
        usage?.assignments ?? 0,
        usage?.calls ?? 0,
        text[locale].unusedStatus,
      ];
    }),
  ];
}

function unresolvedRows(
  links: AspGraphLink[],
  locale: AspGraphLocale,
  nodesById: Map<string, AspGraphNode>,
  fileNamesByUri: Map<string, string>,
): Cell[][] {
  const t = text[locale];
  const rows = links
    .flatMap((link) => usageLikeLinkRows(link, locale, nodesById, fileNamesByUri))
    .sort(compareRows(4, 5, 3));
  return [
    header([t.usageKind, t.role, t.source, t.name, t.file, t.line, t.column, t.count]),
    ...rows,
  ];
}

function declarationRow(
  node: AspGraphNode,
  locale: AspGraphLocale,
  usageCounts: Map<string, UsageCounts>,
  fileNamesByUri: Map<string, string>,
): Cell[] {
  const usage = usageCounts.get(node.id);
  const total = usageTotal(usage);
  return [
    displayNameForUri(node.uri, fileNamesByUri),
    node.label,
    valueLabel(node.declarationKind, locale),
    node.memberOf ?? "",
    valueLabel(node.bindingScope, locale),
    valueLabel(node.procedureKind, locale),
    node.typeName ?? "",
    yn(node.implicit === true, locale),
    arrayDisplay(node, locale),
    oneBasedLine(node.range),
    oneBasedColumn(node.range),
    usage?.references ?? 0,
    usage?.assignments ?? 0,
    usage?.calls ?? 0,
    total > 0 ? text[locale].used : text[locale].unusedStatus,
  ];
}

function usageLikeLinkRows(
  link: AspGraphLink,
  locale: AspGraphLocale,
  nodesById: Map<string, AspGraphNode>,
  fileNamesByUri: Map<string, string>,
): Cell[][] {
  const source = nodesById.get(link.source);
  const target = nodesById.get(link.target);
  const rangeCount = link.ranges.length > 1 ? 1 : link.count;
  return rangesForLink(link).map(({ uri, range }) => [
    valueLabel(link.kind, locale),
    valueLabel(link.role ?? link.label, locale),
    source?.label ?? source?.fileName ?? link.source,
    target?.label ?? target?.fileName ?? link.target,
    displayNameForUri(uri, fileNamesByUri),
    oneBasedLine(range),
    oneBasedColumn(range),
    rangeCount,
  ]);
}

function analysisContext(
  payload: AspGraphPayload,
  options: AnalysisExcelOptions,
  nodesById: Map<string, AspGraphNode>,
  fileNamesByUri: Map<string, string>,
): AnalysisContext {
  const targetUri = options.targetUri ?? defaultAnalysisTargetUri(payload);
  const targetFileName = displayNameForUri(targetUri, fileNamesByUri);
  const targetDeclarations = sourceDeclarationNodes(payload.nodes)
    .filter((node) => sameGraphUri(node.uri, targetUri) && isExternallyVisibleDeclaration(node))
    .sort(compareNodesByLocation(fileNamesByUri));
  const targetDeclarationIds = new Set(targetDeclarations.map((node) => node.id));
  const includedFileUris = includedFileUrisForTarget(payload, targetUri, nodesById);
  const includedDeclarationIds = new Set(
    sourceDeclarationNodes(payload.nodes)
      .filter(
        (node) =>
          node.uri !== undefined &&
          includedFileUris.has(graphUriIdentity(node.uri)) &&
          isExternallyVisibleDeclaration(node),
      )
      .map((node) => node.id),
  );
  const externalUsageLinks = filteredGraphLinks(
    payload.links,
    (link) => isUsageGraphLink(link) && targetDeclarationIds.has(link.target),
    ({ uri }) => !sameGraphUri(uri, targetUri),
  );
  const includedUsageLinks = filteredGraphLinks(
    payload.links,
    (link) => isUsageGraphLink(link) && includedDeclarationIds.has(link.target),
    ({ uri }) => sameGraphUri(uri, targetUri),
  );
  const unresolvedLinks = filteredGraphLinks(
    payload.links,
    (link) =>
      link.kind === "unresolvedReference" || nodesById.get(link.target)?.kind === "vbUnresolved",
    ({ uri }) => sameGraphUri(uri, targetUri),
  );
  const externalUsageCounts = usageCountsByTarget(externalUsageLinks);
  const unusedDeclarations = targetDeclarations
    .filter((node) => usageTotal(externalUsageCounts.get(node.id)) === 0)
    .sort(compareNodesByLocation(fileNamesByUri));
  return {
    targetUri,
    targetFileName,
    targetDeclarations,
    targetDeclarationIds,
    includedFileUris,
    includedDeclarationIds,
    externalUsageLinks,
    includedUsageLinks,
    unresolvedLinks,
    externalUsageCounts,
    unusedDeclarations,
  };
}

function defaultAnalysisTargetUri(payload: AspGraphPayload): string | undefined {
  return (
    payload.rootUri ??
    payload.nodes.find((node) => isFileLikeGraphNode(node) && node.isRoot === true)?.uri ??
    payload.nodes.find(isFileLikeGraphNode)?.uri
  );
}

function isExternallyVisibleDeclaration(node: AspGraphNode): boolean {
  return node.bindingScope !== "local" && node.declarationKind !== "parameter";
}

function includedFileUrisForTarget(
  payload: AspGraphPayload,
  targetUri: string | undefined,
  nodesById: Map<string, AspGraphNode>,
): Set<string> {
  const targetFileIds = new Set(
    payload.nodes
      .filter((node) => isFileLikeGraphNode(node) && sameGraphUri(node.uri, targetUri))
      .map((node) => node.id),
  );
  const includesBySource = new Map<string, string[]>();
  for (const link of payload.links) {
    if (link.kind !== "include") {
      continue;
    }
    const existing = includesBySource.get(link.source);
    if (existing) {
      existing.push(link.target);
    } else {
      includesBySource.set(link.source, [link.target]);
    }
  }
  const result = new Set<string>();
  const visited = new Set<string>(targetFileIds);
  const queue = [...targetFileIds];
  while (queue.length > 0) {
    const source = queue.shift() ?? "";
    for (const target of includesBySource.get(source) ?? []) {
      if (visited.has(target)) {
        continue;
      }
      visited.add(target);
      const node = nodesById.get(target);
      if (node?.uri) {
        result.add(graphUriIdentity(node.uri));
      }
      queue.push(target);
    }
  }
  return result;
}

function filteredGraphLinks(
  links: AspGraphLink[],
  linkPredicate: (link: AspGraphLink) => boolean,
  rangePredicate: (range: { uri: string; range: AspGraphRange }) => boolean,
): AspGraphLink[] {
  return links.flatMap((link) => {
    if (!linkPredicate(link)) {
      return [];
    }
    const ranges = link.ranges.filter(rangePredicate);
    return ranges.length > 0 ? [{ ...link, count: ranges.length, ranges }] : [];
  });
}

function isUsageGraphLink(link: AspGraphLink): boolean {
  return link.kind === "references" || link.kind === "assignments" || link.kind === "calls";
}

function usageCountsByTarget(links: AspGraphLink[]): Map<string, UsageCounts> {
  const counts = new Map<string, UsageCounts>();
  for (const link of links) {
    if (link.kind !== "references" && link.kind !== "assignments" && link.kind !== "calls") {
      continue;
    }
    const entry = counts.get(link.target) ?? { references: 0, assignments: 0, calls: 0 };
    if (link.kind === "references") {
      entry.references += link.count;
    } else if (link.kind === "assignments") {
      entry.assignments += link.count;
    } else {
      entry.calls += link.count;
    }
    counts.set(link.target, entry);
  }
  return counts;
}

function usageLinkCount(links: AspGraphLink[], kind?: AspGraphLink["kind"]): number {
  return links.reduce(
    (sum, link) => (kind === undefined || link.kind === kind ? sum + link.count : sum),
    0,
  );
}

function sourceDeclarationNodes(nodes: AspGraphNode[]): AspGraphNode[] {
  return nodes.filter((node) => node.kind === "vbDeclaration" && node.origin === "source");
}

function usageTotal(usage: UsageCounts | undefined): number {
  return (usage?.references ?? 0) + (usage?.assignments ?? 0) + (usage?.calls ?? 0);
}

function fileNamesByUriMap(nodes: AspGraphNode[]): Map<string, string> {
  const names = new Map<string, string>();
  for (const node of nodes) {
    if (isFileLikeGraphNode(node) && node.uri) {
      names.set(node.uri, node.displayPath ?? node.fileName ?? node.label);
    }
  }
  return names;
}

function isFileLikeGraphNode(node: AspGraphNode): boolean {
  return node.kind === "file" || node.kind === "missingInclude";
}

function displayNameForUri(uri: string | undefined, fileNamesByUri: Map<string, string>): string {
  return uri ? (fileNamesByUri.get(uri) ?? displayPathForUriText(uri) ?? uri) : "";
}

function sameGraphUri(left: string | undefined, right: string | undefined): boolean {
  return (
    left !== undefined && right !== undefined && graphUriIdentity(left) === graphUriIdentity(right)
  );
}

function graphUriIdentity(uri: string): string {
  try {
    const parsed = new URL(uri);
    if (parsed.protocol === "file:") {
      return `file://${decodeURIComponent(parsed.pathname).replace(/\\/g, "/").toLowerCase()}`;
    }
  } catch {
    // Fall through to a conservative string identity.
  }
  return uri;
}

function rangesForLink(link: AspGraphLink): Array<{ uri: string; range?: AspGraphRange }> {
  return link.ranges.length > 0 ? link.ranges : [{ uri: "", range: undefined }];
}

function oneBasedLine(range: AspGraphRange | undefined): number | "" {
  return range ? range.start.line + 1 : "";
}

function oneBasedColumn(range: AspGraphRange | undefined): number | "" {
  return range ? range.start.character + 1 : "";
}

function yn(value: boolean, locale: AspGraphLocale): string {
  return value ? text[locale].yes : text[locale].no;
}

function arrayDisplay(node: AspGraphNode, locale: AspGraphLocale): string {
  if (!node.arrayKind && !node.arrayDimensions?.length) {
    return "";
  }
  const dimensions = node.arrayDimensions?.length ? `(${node.arrayDimensions.join(", ")})` : "";
  return `${valueLabel(node.arrayKind ?? "array", locale)}${dimensions}`;
}

function valueLabel(value: string | undefined, locale: AspGraphLocale): string {
  return value ? (valueText[locale][value] ?? value) : "";
}

function header(values: string[]): Cell[] {
  return values.map((value) => ({
    value,
    type: String,
    fontWeight: "bold",
    textColor: "#FFFFFF",
    backgroundColor: "#1F4E79",
  }));
}

function sectionTitle(value: string, columnSpan: number): Cell[] {
  return [
    {
      value,
      type: String,
      fontWeight: "bold",
      textColor: "#17365D",
      backgroundColor: "#EAF2F8",
      columnSpan,
    },
    ...Array.from({ length: Math.max(0, columnSpan - 1) }, () => null),
  ];
}

function toneCell(value: string, tone: SummaryTone): Cell {
  const style: Record<SummaryTone, { backgroundColor: string; textColor: string }> = {
    danger: { backgroundColor: "#F4CCCC", textColor: "#990000" },
    good: { backgroundColor: "#D9EAD3", textColor: "#274E13" },
    info: { backgroundColor: "#CFE2F3", textColor: "#0B5394" },
    neutral: { backgroundColor: "#E7E6E6", textColor: "#404040" },
    warning: { backgroundColor: "#FCE5CD", textColor: "#B45F06" },
  };
  return {
    value,
    type: String,
    fontWeight: "bold",
    ...style[tone],
  };
}

function percentCell(value: number): Cell {
  return {
    value,
    type: Number,
    format: "0.0%",
  };
}

function ratio(numerator: number, denominator: number): number {
  return denominator > 0 ? numerator / denominator : 0;
}

function barString(value: number, max: number): string {
  if (value <= 0 || max <= 0) {
    return "";
  }
  const filled = Math.max(1, Math.round((value / max) * 20));
  return `${"█".repeat(filled)} ${value}`;
}

function maxCount(values: number[]): number {
  return Math.max(0, ...values);
}

function sheet(
  name: string,
  rows: Cell[][],
  options: AnalysisSheetOptions = {},
): AnalysisExcelSheet {
  const autoFilterRef = options.autoFilter === false ? undefined : autoFilterRefForRows(rows);
  return {
    sheet: name,
    data: rows,
    columns: columnsForRows(rows),
    stickyRowsCount: 1,
    autoFilterRef,
    hidden: options.hidden === true ? true : undefined,
    images: options.images,
  };
}

function autoFilterRefForRows(rows: Cell[][]): string | undefined {
  const columnCount = Math.max(0, ...rows.map((row) => row.length));
  if (rows.length === 0 || columnCount === 0) {
    return undefined;
  }
  return `A1:${spreadsheetColumnName(columnCount - 1)}${rows.length}`;
}

function spreadsheetColumnName(columnIndex: number): string {
  let name = "";
  let current = columnIndex + 1;
  while (current > 0) {
    const remainder = (current - 1) % 26;
    name = String.fromCharCode(65 + remainder) + name;
    current = Math.floor((current - 1) / 26);
  }
  return name;
}

function blankRows(count: number): Cell[][] {
  return Array.from({ length: count }, () => []);
}

function truncateLabel(value: string, maxLength: number): string {
  return value.length > maxLength ? `${value.slice(0, Math.max(0, maxLength - 1))}…` : value;
}

function columnsForRows(rows: Cell[][]): Array<{ width: number }> {
  const columnCount = Math.max(0, ...rows.map((row) => row.length));
  return Array.from({ length: columnCount }, (_, column) => ({
    width: Math.max(
      10,
      Math.min(48, Math.max(...rows.map((row) => cellDisplayLength(row[column])), 0) + 2),
    ),
  }));
}

function cellDisplayLength(cell: Cell): number {
  if (cell === null || cell === undefined) {
    return 0;
  }
  if (typeof cell === "object" && "value" in cell) {
    return String(cell.value ?? "").length;
  }
  return String(cell).length;
}

function compareNodesByLocation(
  fileNamesByUri: Map<string, string>,
): (left: AspGraphNode, right: AspGraphNode) => number {
  return (left, right) =>
    compareValues(
      displayNameForUri(left.uri, fileNamesByUri),
      displayNameForUri(right.uri, fileNamesByUri),
    ) ||
    compareValues(oneBasedLine(left.range), oneBasedLine(right.range)) ||
    compareValues(left.label, right.label);
}

function compareRows(...columns: number[]): (left: Cell[], right: Cell[]) => number {
  return (left, right) => {
    for (const column of columns) {
      const comparison = compareValues(cellValue(left[column]), cellValue(right[column]));
      if (comparison) {
        return comparison;
      }
    }
    return 0;
  };
}

function cellValue(cell: Cell): string | number | boolean {
  if (cell === null || cell === undefined) {
    return "";
  }
  if (typeof cell === "object" && "value" in cell) {
    return primitiveCellValue(cell.value?.valueOf());
  }
  return primitiveCellValue(cell.valueOf());
}

function primitiveCellValue(value: unknown): string | number | boolean {
  return typeof value === "string" || typeof value === "number" || typeof value === "boolean"
    ? value
    : String(value ?? "");
}

function compareValues(left: string | number | boolean, right: string | number | boolean): number {
  if (typeof left === "number" && typeof right === "number") {
    return left - right;
  }
  return String(left).localeCompare(String(right), "en");
}
