import type { Cell, Sheet } from "write-excel-file/node";
import type {
  AspGraphLink,
  AspGraphLocale,
  AspGraphNode,
  AspGraphPayload,
  AspGraphRange,
} from "./include-graph-webview";

export type AnalysisExcelSheet = Sheet<Buffer>;

interface AnalysisExcelOptions {
  generatedAt?: Date;
}

interface UsageCounts {
  references: number;
  assignments: number;
  calls: number;
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
    unusedCount: "Unused candidates",
    truncated: "Truncated",
    yes: "Yes",
    no: "No",
    used: "Used",
    unusedStatus: "Unused candidate",
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
    unused: "未使用候補",
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
    unusedCount: "未使用候補数",
    truncated: "切り詰め",
    yes: "あり",
    no: "なし",
    used: "使用あり",
    unusedStatus: "未使用候補",
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
  const usageCounts = usageCountsByTarget(payload.links);
  const fileNamesByUri = fileNamesByUriMap(payload.nodes);
  const unusedDeclarations = sourceDeclarationNodes(payload.nodes)
    .filter((node) => usageTotal(usageCounts.get(node.id)) === 0)
    .sort(compareNodesByLocation(fileNamesByUri));
  return [
    sheet(text[locale].summary, summaryRows(payload, locale, generatedAt, unusedDeclarations)),
    sheet(
      text[locale].analysisSummary,
      analysisSummaryRows(payload, locale, usageCounts, fileNamesByUri, unusedDeclarations),
    ),
    sheet(
      text[locale].chartData,
      chartDataRows(payload, locale, usageCounts, fileNamesByUri, unusedDeclarations),
    ),
    sheet(text[locale].files, fileRows(payload, locale)),
    sheet(text[locale].includes, includeRows(payload, locale, nodesById, fileNamesByUri)),
    sheet(text[locale].declarations, declarationRows(payload, locale, usageCounts, fileNamesByUri)),
    sheet(text[locale].referenced, referencedRows(payload, locale, usageCounts, fileNamesByUri)),
    sheet(text[locale].usages, usageRows(payload, locale, nodesById, fileNamesByUri)),
    sheet(
      text[locale].unused,
      unusedDeclarationRows(unusedDeclarations, locale, usageCounts, fileNamesByUri),
    ),
    sheet(text[locale].unresolved, unresolvedRows(payload, locale, nodesById, fileNamesByUri)),
  ];
}

function summaryRows(
  payload: AspGraphPayload,
  locale: AspGraphLocale,
  generatedAt: Date,
  unusedDeclarations: AspGraphNode[],
): Cell[][] {
  const t = text[locale];
  const rows: Array<[string, string | number]> = [
    [t.scope, valueLabel(payload.scope, locale)],
    [t.root, payload.rootUri ? fileDisplayName(payload.rootUri, payload.nodes) : ""],
    [t.generatedAt, generatedAt.toISOString()],
    [t.filesCount, payload.stats.files],
    [t.declarationsCount, payload.stats.declarations],
    [t.referencesCount, payload.stats.references],
    [t.assignmentsCount, payload.stats.assignments],
    [t.callsCount, payload.stats.calls],
    [t.includesCount, payload.stats.includes],
    [t.missingIncludesCount, payload.stats.missingIncludes],
    [t.unresolvedCount, payload.stats.unresolvedReferences],
    [t.unusedCount, unusedDeclarations.length],
    [t.truncated, payload.truncated?.reason ?? ""],
  ];
  return [header([t.name, t.value ?? "Value"]), ...rows.map(([name, value]) => [name, value])];
}

function analysisSummaryRows(
  payload: AspGraphPayload,
  locale: AspGraphLocale,
  usageCounts: Map<string, UsageCounts>,
  fileNamesByUri: Map<string, string>,
  unusedDeclarations: AspGraphNode[],
): Cell[][] {
  const t = text[locale];
  return [
    sectionTitle(t.declarationsByKind, 6),
    header([t.kind, t.total, t.usedCount, t.unusedCount, t.share, t.bar]),
    ...declarationKindChartRows(payload.nodes, locale, usageCounts),
    [],
    sectionTitle(t.scopeComparison, 6),
    header([t.bindingScope, t.total, t.usedCount, t.unusedCount, t.unusedRate, t.bar]),
    ...scopeComparisonRows(payload.nodes, locale, usageCounts),
    [],
    sectionTitle(t.usageMix, 5),
    header([t.usageKind, t.count, t.share, t.bar, t.risk]),
    ...usageMixRows(payload, locale),
    [],
    sectionTitle(t.unusedByKind, 4),
    header([t.kind, t.unusedCount, t.share, t.bar]),
    ...unusedByKindRows(unusedDeclarations, locale),
    [],
    sectionTitle(t.fileHealth, 10),
    header([
      t.file,
      t.declarationCount,
      t.unusedCount,
      t.unusedRate,
      t.usageCount,
      t.unresolvedUsageCount,
      t.includesOut,
      t.includedBy,
      t.risk,
      t.bar,
    ]),
    ...fileHealthRows(payload, locale, usageCounts, fileNamesByUri, unusedDeclarations),
  ];
}

function chartDataRows(
  payload: AspGraphPayload,
  locale: AspGraphLocale,
  usageCounts: Map<string, UsageCounts>,
  fileNamesByUri: Map<string, string>,
  unusedDeclarations: AspGraphNode[],
): Cell[][] {
  const t = text[locale];
  return [
    sectionTitle(t.declarationsByKind, 5),
    header([t.kind, t.total, t.usedCount, t.unusedCount, t.share]),
    ...declarationKindChartRows(payload.nodes, locale, usageCounts).map((row) => row.slice(0, 5)),
    [],
    sectionTitle(t.scopeComparison, 5),
    header([t.bindingScope, t.total, t.usedCount, t.unusedCount, t.unusedRate]),
    ...scopeComparisonRows(payload.nodes, locale, usageCounts).map((row) => row.slice(0, 5)),
    [],
    sectionTitle(t.usageMix, 4),
    header([t.usageKind, t.count, t.share, t.risk]),
    ...usageMixRows(payload, locale).map((row) => [row[0], row[1], row[2], row[4]]),
    [],
    sectionTitle(t.unusedByKind, 3),
    header([t.kind, t.unusedCount, t.share]),
    ...unusedByKindRows(unusedDeclarations, locale).map((row) => row.slice(0, 3)),
    [],
    sectionTitle(t.fileHealth, 9),
    header([
      t.file,
      t.declarationCount,
      t.unusedCount,
      t.unusedRate,
      t.usageCount,
      t.unresolvedUsageCount,
      t.includesOut,
      t.includedBy,
      t.risk,
    ]),
    ...fileHealthRows(payload, locale, usageCounts, fileNamesByUri, unusedDeclarations).map((row) =>
      row.slice(0, 9),
    ),
  ];
}

function declarationKindChartRows(
  nodes: AspGraphNode[],
  locale: AspGraphLocale,
  usageCounts: Map<string, UsageCounts>,
): Cell[][] {
  const declarations = sourceDeclarationNodes(nodes);
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
  nodes: AspGraphNode[],
  locale: AspGraphLocale,
  usageCounts: Map<string, UsageCounts>,
): Cell[][] {
  const declarations = sourceDeclarationNodes(nodes);
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

function usageMixRows(payload: AspGraphPayload, locale: AspGraphLocale): Cell[][] {
  const rows: Array<[string, number, string]> = [
    ["references", payload.stats.references, ""],
    ["assignments", payload.stats.assignments, ""],
    ["calls", payload.stats.calls, ""],
    ["unresolvedReference", payload.stats.unresolvedReferences, "unresolved"],
    ["include", payload.stats.includes, ""],
    ["missingIncludes", payload.stats.missingIncludes, "missing"],
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

function fileHealthRows(
  payload: AspGraphPayload,
  locale: AspGraphLocale,
  usageCounts: Map<string, UsageCounts>,
  fileNamesByUri: Map<string, string>,
  unusedDeclarations: AspGraphNode[],
): Cell[][] {
  const declarationsByUri = new Map<string, number>();
  const unusedByUri = new Map<string, number>();
  const usagesByUri = new Map<string, number>();
  const unresolvedByUri = new Map<string, number>();
  for (const node of sourceDeclarationNodes(payload.nodes)) {
    if (node.uri) {
      declarationsByUri.set(node.uri, (declarationsByUri.get(node.uri) ?? 0) + 1);
    }
  }
  for (const node of unusedDeclarations) {
    if (node.uri) {
      unusedByUri.set(node.uri, (unusedByUri.get(node.uri) ?? 0) + 1);
    }
  }
  for (const link of payload.links) {
    if (
      link.kind !== "references" &&
      link.kind !== "assignments" &&
      link.kind !== "calls" &&
      link.kind !== "unresolvedReference"
    ) {
      continue;
    }
    for (const range of link.ranges) {
      usagesByUri.set(range.uri, (usagesByUri.get(range.uri) ?? 0) + link.count);
      if (link.kind === "unresolvedReference") {
        unresolvedByUri.set(range.uri, (unresolvedByUri.get(range.uri) ?? 0) + link.count);
      }
    }
  }
  const includeOut = countLinksByNode(payload.links, "source", "include");
  const includeIn = countLinksByNode(payload.links, "target", "include");
  const riskScores = payload.nodes
    .filter(isFileLikeGraphNode)
    .map((node) => fileRiskScore(node, declarationsByUri, unusedByUri, unresolvedByUri));
  const maxRisk = maxCount(riskScores);
  return payload.nodes
    .filter(isFileLikeGraphNode)
    .sort(compareFileNodes)
    .map((node) => {
      const uri = node.uri ?? "";
      const declarations = declarationsByUri.get(uri) ?? 0;
      const unused = unusedByUri.get(uri) ?? 0;
      const unresolved = unresolvedByUri.get(uri) ?? 0;
      const risk = fileRiskScore(node, declarationsByUri, unusedByUri, unresolvedByUri);
      return [
        displayNameForUri(uri, fileNamesByUri),
        declarations,
        unused,
        percentCell(ratio(unused, declarations)),
        usagesByUri.get(uri) ?? usageTotal(usageCounts.get(node.id)),
        unresolved,
        includeOut.get(node.id) ?? 0,
        includeIn.get(node.id) ?? 0,
        risk,
        barString(risk, maxRisk),
      ];
    });
}

function fileRiskScore(
  node: AspGraphNode,
  declarationsByUri: Map<string, number>,
  unusedByUri: Map<string, number>,
  unresolvedByUri: Map<string, number>,
): number {
  const uri = node.uri ?? "";
  const declarations = declarationsByUri.get(uri) ?? 0;
  const unused = unusedByUri.get(uri) ?? 0;
  const unresolved = unresolvedByUri.get(uri) ?? 0;
  return unused + unresolved * 2 + (node.exists === false ? Math.max(1, declarations) : 0);
}

function fileRows(payload: AspGraphPayload, locale: AspGraphLocale): Cell[][] {
  const t = text[locale];
  const includeOut = countLinksByNode(payload.links, "source", "include");
  const includeIn = countLinksByNode(payload.links, "target", "include");
  const declarations = new Map<string, number>();
  for (const node of sourceDeclarationNodes(payload.nodes)) {
    if (node.uri) {
      declarations.set(node.uri, (declarations.get(node.uri) ?? 0) + 1);
    }
  }
  const rows = payload.nodes
    .filter(isFileLikeGraphNode)
    .sort(compareFileNodes)
    .map((node) => [
      node.fileName ?? node.label,
      yn(node.exists !== false, locale),
      yn(node.isRoot === true, locale),
      includeOut.get(node.id) ?? 0,
      includeIn.get(node.id) ?? 0,
      declarations.get(node.uri ?? "") ?? 0,
    ]);
  return [
    header([t.file, t.exists, t.rootFile, t.includesOut, t.includedBy, t.declarationCount]),
    ...rows,
  ];
}

function includeRows(
  payload: AspGraphPayload,
  locale: AspGraphLocale,
  nodesById: Map<string, AspGraphNode>,
  fileNamesByUri: Map<string, string>,
): Cell[][] {
  const t = text[locale];
  const rows = payload.links
    .filter((link) => link.kind === "include")
    .flatMap((link) => {
      const source = nodesById.get(link.source);
      const target = nodesById.get(link.target);
      return rangesForLink(link).map(({ uri, range }) => [
        source?.fileName ?? source?.label ?? displayNameForUri(uri, fileNamesByUri),
        link.include?.path ?? link.label,
        valueLabel(link.include?.mode, locale),
        target?.fileName ??
          target?.label ??
          displayNameForUri(link.include?.resolvedUri, fileNamesByUri),
        yn(link.include?.exists !== false, locale),
        link.include?.actualPath ?? "",
        link.include?.pathCaseMatches === undefined ? "" : yn(link.include.pathCaseMatches, locale),
        oneBasedLine(range),
        oneBasedColumn(range),
      ]);
    })
    .sort(compareRows(0, 7, 1));
  return [
    header([
      t.sourceFile,
      t.includePath,
      t.includeMode,
      t.resolvedTarget,
      t.exists,
      t.actualPath,
      t.pathCaseMatches,
      t.line,
      t.column,
    ]),
    ...rows,
  ];
}

function declarationRows(
  payload: AspGraphPayload,
  locale: AspGraphLocale,
  usageCounts: Map<string, UsageCounts>,
  fileNamesByUri: Map<string, string>,
): Cell[][] {
  const t = text[locale];
  const rows = sourceDeclarationNodes(payload.nodes)
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
  payload: AspGraphPayload,
  locale: AspGraphLocale,
  usageCounts: Map<string, UsageCounts>,
  fileNamesByUri: Map<string, string>,
): Cell[][] {
  const t = text[locale];
  const rows = payload.nodes
    .filter((node) => node.kind === "vbDeclaration" && usageTotal(usageCounts.get(node.id)) > 0)
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
  payload: AspGraphPayload,
  locale: AspGraphLocale,
  nodesById: Map<string, AspGraphNode>,
  fileNamesByUri: Map<string, string>,
): Cell[][] {
  const t = text[locale];
  const rows = payload.links
    .filter(
      (link) => link.kind === "references" || link.kind === "assignments" || link.kind === "calls",
    )
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
  payload: AspGraphPayload,
  locale: AspGraphLocale,
  nodesById: Map<string, AspGraphNode>,
  fileNamesByUri: Map<string, string>,
): Cell[][] {
  const t = text[locale];
  const rows = payload.links
    .filter(
      (link) =>
        link.kind === "unresolvedReference" || nodesById.get(link.target)?.kind === "vbUnresolved",
    )
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
  return rangesForLink(link).map(({ uri, range }) => [
    valueLabel(link.kind, locale),
    valueLabel(link.role ?? link.label, locale),
    source?.label ?? source?.fileName ?? link.source,
    target?.label ?? target?.fileName ?? link.target,
    displayNameForUri(uri, fileNamesByUri),
    oneBasedLine(range),
    oneBasedColumn(range),
    link.count,
  ]);
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

function sourceDeclarationNodes(nodes: AspGraphNode[]): AspGraphNode[] {
  return nodes.filter((node) => node.kind === "vbDeclaration" && node.origin === "source");
}

function usageTotal(usage: UsageCounts | undefined): number {
  return (usage?.references ?? 0) + (usage?.assignments ?? 0) + (usage?.calls ?? 0);
}

function countLinksByNode(
  links: AspGraphLink[],
  property: "source" | "target",
  kind: AspGraphLink["kind"],
): Map<string, number> {
  const counts = new Map<string, number>();
  for (const link of links) {
    if (link.kind === kind) {
      counts.set(link[property], (counts.get(link[property]) ?? 0) + link.count);
    }
  }
  return counts;
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

function fileDisplayName(uri: string, nodes: AspGraphNode[]): string {
  return fileNamesByUriMap(nodes).get(uri) ?? uri;
}

function displayNameForUri(uri: string | undefined, fileNamesByUri: Map<string, string>): string {
  return uri ? (fileNamesByUri.get(uri) ?? uri) : "";
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

function sheet(name: string, rows: Cell[][]): AnalysisExcelSheet {
  return {
    sheet: name,
    data: rows,
    columns: columnsForRows(rows),
    stickyRowsCount: 1,
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

function compareFileNodes(left: AspGraphNode, right: AspGraphNode): number {
  return compareValues(left.fileName ?? left.label, right.fileName ?? right.label);
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
