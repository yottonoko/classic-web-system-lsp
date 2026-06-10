import type { Cell, Sheet } from "write-excel-file/node";
import type {
  AspGraphLink,
  AspGraphLocale,
  AspGraphNode,
  AspGraphPayload,
  AspGraphRange,
} from "./include-graph-webview";

export type AnalysisExcelSheet = Sheet<Buffer>;
type AnalysisExcelImage = NonNullable<AnalysisExcelSheet["images"]>[number];

interface ChartDatum {
  label: string;
  value: number;
  detail?: string;
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
  | "risk";

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
  return [
    sheet(text[locale].summary, summaryRows(payload, locale, generatedAt, context)),
    sheet(text[locale].analysisSummary, analysisRows, analysisSummaryImages(locale, context)),
    sheet(text[locale].chartData, chartDataRows(locale, context)),
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
    sectionTitle(t.declarationsByKind, 6),
    header([t.kind, t.total, t.usedCount, t.unusedCount, t.share, t.bar]),
    ...declarationKindChartRows(context.targetDeclarations, locale, context.externalUsageCounts),
    [],
    sectionTitle(t.scopeComparison, 6),
    header([t.bindingScope, t.total, t.usedCount, t.unusedCount, t.unusedRate, t.bar]),
    ...scopeComparisonRows(context.targetDeclarations, locale, context.externalUsageCounts),
    [],
    sectionTitle(t.usageMix, 5),
    header([t.usageKind, t.count, t.share, t.bar, t.risk]),
    ...usageMixRows(
      [...context.externalUsageLinks, ...context.includedUsageLinks],
      context.unresolvedLinks,
      locale,
    ),
    [],
    sectionTitle(t.unusedByKind, 4),
    header([t.kind, t.unusedCount, t.share, t.bar]),
    ...unusedByKindRows(context.unusedDeclarations, locale),
  ];
}

function chartDataRows(locale: AspGraphLocale, context: AnalysisContext): Cell[][] {
  const t = text[locale];
  return [
    sectionTitle(t.declarationsByKind, 5),
    header([t.kind, t.total, t.usedCount, t.unusedCount, t.share]),
    ...declarationKindChartRows(
      context.targetDeclarations,
      locale,
      context.externalUsageCounts,
    ).map((row) => row.slice(0, 5)),
    [],
    sectionTitle(t.scopeComparison, 5),
    header([t.bindingScope, t.total, t.usedCount, t.unusedCount, t.unusedRate]),
    ...scopeComparisonRows(context.targetDeclarations, locale, context.externalUsageCounts).map(
      (row) => row.slice(0, 5),
    ),
    [],
    sectionTitle(t.usageMix, 4),
    header([t.usageKind, t.count, t.share, t.risk]),
    ...usageMixRows(
      [...context.externalUsageLinks, ...context.includedUsageLinks],
      context.unresolvedLinks,
      locale,
    ).map((row) => [row[0], row[1], row[2], row[4]]),
    [],
    sectionTitle(t.unusedByKind, 3),
    header([t.kind, t.unusedCount, t.share]),
    ...unusedByKindRows(context.unusedDeclarations, locale).map((row) => row.slice(0, 3)),
  ];
}

function analysisSummaryImages(
  locale: AspGraphLocale,
  context: AnalysisContext,
): AnalysisExcelImage[] {
  const t = text[locale];
  const declarationRows = declarationKindChartRows(
    context.targetDeclarations,
    locale,
    context.externalUsageCounts,
  );
  const scopeRows = scopeComparisonRows(
    context.targetDeclarations,
    locale,
    context.externalUsageCounts,
  );
  const usageRows = usageMixRows(
    [...context.externalUsageLinks, ...context.includedUsageLinks],
    context.unresolvedLinks,
    locale,
  );
  const unusedRows = unusedByKindRows(context.unusedDeclarations, locale);
  return [
    chartImage(
      barChartSvg(t.declarationsByKind, chartDataFromRows(declarationRows, 0, 1, 3), "#4F81BD"),
      t.declarationsByKind,
      2,
    ),
    chartImage(pieChartSvg(t.usageMix, chartDataFromRows(usageRows, 0, 1, 4)), t.usageMix, 18),
    chartImage(
      barChartSvg(t.scopeComparison, chartDataFromRows(scopeRows, 0, 1, 3), "#70AD47"),
      t.scopeComparison,
      34,
    ),
    chartImage(
      barChartSvg(t.unusedByKind, chartDataFromRows(unusedRows, 0, 1, 2), "#C00000"),
      t.unusedByKind,
      50,
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
    width: 560,
    height: 260,
    dpi: 96,
    anchor: { row, column: 12 },
    title,
    description: title,
  };
}

function barChartSvg(title: string, data: ChartDatum[], color: string): string {
  const width = 560;
  const height = 260;
  const chartX = 150;
  const chartY = 46;
  const chartWidth = 340;
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
        return [
          `<text x="18" y="${y + 15}" class="label">${escapeXml(item.label)}</text>`,
          `<rect x="${chartX}" y="${y}" width="${barWidth.toFixed(1)}" height="${Math.max(10, rowHeight - 7).toFixed(1)}" rx="3" fill="${color}"/>`,
          `<text x="${chartX + barWidth + 8}" y="${y + 15}" class="value">${item.value}${item.detail ? ` / ${escapeXml(item.detail)}` : ""}</text>`,
        ].join(" ");
      })
      .join("\n"),
  );
}

function pieChartSvg(title: string, data: ChartDatum[]): string {
  const width = 560;
  const height = 260;
  const total = data.reduce((sum, item) => sum + item.value, 0);
  const radius = 72;
  const circumference = 2 * Math.PI * radius;
  const colors = ["#4F81BD", "#C0504D", "#9BBB59", "#8064A2", "#4BACC6", "#F79646"];
  let offset = 0;
  const slices = data
    .slice(0, colors.length)
    .map((item, index) => {
      const length = total > 0 ? (item.value / total) * circumference : 0;
      const dashOffset = -offset;
      offset += length;
      return `<circle cx="132" cy="138" r="${radius}" fill="none" stroke="${colors[index]}" stroke-width="36" stroke-dasharray="${length.toFixed(2)} ${(circumference - length).toFixed(2)}" stroke-dashoffset="${dashOffset.toFixed(2)}" transform="rotate(-90 132 138)"/>`;
    })
    .join("\n");
  const legend = data
    .slice(0, colors.length)
    .map((item, index) => {
      const y = 72 + index * 24;
      const share = total > 0 ? `${((item.value / total) * 100).toFixed(1)}%` : "0.0%";
      return [
        `<rect x="260" y="${y - 11}" width="12" height="12" rx="2" fill="${colors[index]}"/>`,
        `<text x="280" y="${y}" class="label">${escapeXml(item.label)}</text>`,
        `<text x="450" y="${y}" class="value">${item.value} (${share})</text>`,
      ].join(" ");
    })
    .join("\n");
  return svgFrame(width, height, title, `${slices}\n${legend}`);
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

function declarationKindChartRows(
  declarations: AspGraphNode[],
  locale: AspGraphLocale,
  usageCounts: Map<string, UsageCounts>,
): Cell[][] {
  const counts = new Map<string, { total: number; used: number; unused: number }>();
  for (const node of declarations) {
    const key = node.declarationKind ?? "unknown";
    const entry = counts.get(key) ?? { total: 0, used: 0, unused: 0 };
    entry.total += 1;
    if (usageTotal(usageCounts.get(node.id)) > 0) {
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
      percentCell(ratio(entry.total, declarations.length)),
      barString(entry.total, max),
    ]);
}

function scopeComparisonRows(
  declarations: AspGraphNode[],
  locale: AspGraphLocale,
  usageCounts: Map<string, UsageCounts>,
): Cell[][] {
  const counts = new Map<string, { total: number; used: number; unused: number }>();
  for (const node of declarations) {
    const key = node.bindingScope ?? "unknown";
    const entry = counts.get(key) ?? { total: 0, used: 0, unused: 0 };
    entry.total += 1;
    if (usageTotal(usageCounts.get(node.id)) > 0) {
      entry.used += 1;
    } else {
      entry.unused += 1;
    }
    counts.set(key, entry);
  }
  const max = maxCount([...counts.values()].map((entry) => entry.total));
  return [...counts.entries()]
    .sort((left, right) => compareValues(valueLabel(left[0], locale), valueLabel(right[0], locale)))
    .map(([scope, entry]) => [
      valueLabel(scope, locale),
      entry.total,
      entry.used,
      entry.unused,
      percentCell(ratio(entry.unused, entry.total)),
      barString(entry.total, max),
    ]);
}

function usageMixRows(
  usageLinks: AspGraphLink[],
  unresolvedLinks: AspGraphLink[],
  locale: AspGraphLocale,
): Cell[][] {
  const rows: Array<[string, number, string]> = [
    ["references", usageLinkCount(usageLinks, "references"), ""],
    ["assignments", usageLinkCount(usageLinks, "assignments"), ""],
    ["calls", usageLinkCount(usageLinks, "calls"), ""],
    ["unresolvedReference", usageLinkCount(unresolvedLinks), "unresolved"],
  ];
  const total = rows.reduce((sum, [, count]) => sum + count, 0);
  const max = maxCount(rows.map(([, count]) => count));
  return rows
    .filter(([, count]) => count > 0)
    .map(([kind, count, risk]) => [
      valueLabel(kind, locale),
      count,
      percentCell(ratio(count, total)),
      barString(count, max),
      risk,
    ]);
}

function unusedByKindRows(unusedDeclarations: AspGraphNode[], locale: AspGraphLocale): Cell[][] {
  const counts = new Map<string, number>();
  for (const node of unusedDeclarations) {
    const key = node.declarationKind ?? "unknown";
    counts.set(key, (counts.get(key) ?? 0) + 1);
  }
  const max = maxCount([...counts.values()]);
  return [...counts.entries()]
    .sort((left, right) => right[1] - left[1] || compareValues(left[0], right[0]))
    .map(([kind, count]) => [
      valueLabel(kind, locale),
      count,
      percentCell(ratio(count, unusedDeclarations.length)),
      barString(count, max),
    ]);
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
      names.set(node.uri, node.fileName ?? node.label);
    }
  }
  return names;
}

function isFileLikeGraphNode(node: AspGraphNode): boolean {
  return node.kind === "file" || node.kind === "missingInclude";
}

function displayNameForUri(uri: string | undefined, fileNamesByUri: Map<string, string>): string {
  return uri ? (fileNamesByUri.get(uri) ?? uri) : "";
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
    backgroundColor: "#DDEBF7",
  }));
}

function sectionTitle(value: string, columnSpan: number): Cell[] {
  return [
    {
      value,
      type: String,
      fontWeight: "bold",
      backgroundColor: "#E2F0D9",
      columnSpan,
    },
    ...Array.from({ length: Math.max(0, columnSpan - 1) }, () => null),
  ];
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

function sheet(name: string, rows: Cell[][], images?: AnalysisExcelImage[]): AnalysisExcelSheet {
  return {
    sheet: name,
    data: rows,
    columns: columnsForRows(rows),
    stickyRowsCount: 1,
    images,
  };
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
