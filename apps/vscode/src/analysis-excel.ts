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
  stickyRows?: boolean;
}

interface AnalysisExcelOptions {
  generatedAt?: Date;
  targetUri?: string;
  settings?: AnalysisExcelSettingsSummary;
}

interface AnalysisExcelSettingsSummary {
  excelLocale?: AspGraphLocale | "auto";
  includeRelatedIncludeTreesForUnresolved?: boolean;
  forceRelatedIncludeTreeAnalysis?: boolean;
  skipTypeInference?: boolean;
  includeAnalysisTypeDetails?: boolean;
  maxDocuments?: number;
  maxTextLength?: number;
  includeTreeMaxDocuments?: number;
  includeTreeMaxTextLength?: number;
}

interface UsageCounts {
  references: number;
  assignments: number;
  calls: number;
}

interface ImplicitGlobalAssignmentCandidate {
  implicitGlobal: AspGraphNode;
  assignmentTarget?: AspGraphNode;
  uri: string;
  range?: AspGraphRange;
  includeDepth: number;
  count: number;
}

type LegacyImplicitGlobalNodeFields = {
  implicitLocal?: boolean;
  unresolvedGlobal?: boolean;
};

type SummaryTone = "good" | "warning" | "danger" | "info" | "neutral";

interface AnalysisContext {
  targetUri?: string;
  targetFileName: string;
  targetDeclarations: AspGraphNode[];
  targetDeclarationIds: Set<string>;
  includedFileUris: Set<string>;
  includedDeclarationIds: Set<string>;
  targetUsageLinks: AspGraphLink[];
  internalUsageLinks: AspGraphLink[];
  externalUsageLinks: AspGraphLink[];
  includedUsageLinks: AspGraphLink[];
  memberUsageLinks: AspGraphLink[];
  implicitGlobalDeclarations: AspGraphNode[];
  implicitGlobalUsageCounts: Map<string, UsageCounts>;
  implicitGlobalAssignmentCandidates: ImplicitGlobalAssignmentCandidate[];
  unresolvedLinks: AspGraphLink[];
  targetUsageCounts: Map<string, UsageCounts>;
  externalUsageCounts: Map<string, UsageCounts>;
  unusedDeclarations: AspGraphNode[];
}

type AnalysisTextKey =
  | "summary"
  | "analysisSummary"
  | "chartData"
  | "includeTree"
  | "files"
  | "includes"
  | "declarations"
  | "usages"
  | "internalUsages"
  | "externalFileUsages"
  | "includedSymbolUsages"
  | "memberUsages"
  | "implicitGlobals"
  | "implicitGlobalAssignments"
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
  | "implicitGlobalCount"
  | "implicitGlobalAssignmentCount"
  | "unusedCount"
  | "truncated"
  | "notTruncated"
  | "relatedIncludeTreeAnalysis"
  | "analysisSettings"
  | "setting"
  | "excelLanguage"
  | "automatic"
  | "enabled"
  | "disabled"
  | "forced"
  | "notForced"
  | "forceRelatedIncludeTreeAnalysis"
  | "skipTypeInference"
  | "analysisTypeDetails"
  | "maxDocuments"
  | "maxTextLength"
  | "includeTreeMaxDocuments"
  | "includeTreeMaxTextLength"
  | "includeTreeDescendant"
  | "includeTreeAncestor"
  | "includeTreeRelative"
  | "direction"
  | "depth"
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
  | "inferredType"
  | "returnType"
  | "parameters"
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
  | "usageFile"
  | "usageOwner"
  | "declarationFile"
  | "declarationName"
  | "declarationKind"
  | "includeFile"
  | "includedSymbol"
  | "includedKind"
  | "usedFromFile"
  | "receiver"
  | "memberName"
  | "expression"
  | "implicitGlobalFile"
  | "implicitGlobalName"
  | "assignmentFile"
  | "assignmentTarget"
  | "assignmentTargetFile"
  | "includeDepth"
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
  | "reviewImplicitGlobalsAction"
  | "reviewImplicitGlobalAssignmentsAction"
  | "reviewExternalUsagesAction"
  | "reviewMissingExternalUsagesAction"
  | "reviewIncludedUsagesAction"
  | "reviewMissingIncludedUsagesAction"
  | "tableDescription";

const text: Record<AspGraphLocale, Record<AnalysisTextKey, string>> = {
  en: {
    summary: "Summary",
    analysisSummary: "Analysis Summary",
    chartData: "Chart Data",
    includeTree: "Include Tree",
    files: "Files",
    includes: "Includes",
    declarations: "Declarations",
    usages: "Usages",
    internalUsages: "File-local Usage",
    externalFileUsages: "External File Usage",
    includedSymbolUsages: "Included Symbol Usage",
    memberUsages: "Member Usage",
    implicitGlobals: "Implicit Global Variables",
    implicitGlobalAssignments: "Implicit Global Assignment Candidates",
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
    implicitGlobalCount: "Implicit global variables",
    implicitGlobalAssignmentCount: "Implicit global assignment candidates",
    unusedCount: "Unused",
    truncated: "Truncated",
    notTruncated: "Not truncated",
    relatedIncludeTreeAnalysis: "Related include tree analysis",
    analysisSettings: "Analysis settings",
    setting: "Setting",
    excelLanguage: "Excel language",
    automatic: "Automatic",
    enabled: "Enabled",
    disabled: "Disabled",
    forced: "Forced",
    notForced: "Not forced",
    forceRelatedIncludeTreeAnalysis: "Force related include tree analysis",
    skipTypeInference: "Skip type inference",
    analysisTypeDetails: "Editor-inferred type details",
    maxDocuments: "Excel output document limit",
    maxTextLength: "Excel output text limit",
    includeTreeMaxDocuments: "Excel include tree document limit",
    includeTreeMaxTextLength: "Excel include tree text limit",
    includeTreeDescendant: "Descendant",
    includeTreeAncestor: "Ancestor",
    includeTreeRelative: "Relative",
    direction: "Direction",
    depth: "Depth",
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
    inferredType: "Inferred type",
    returnType: "Return type",
    parameters: "Parameters",
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
    usageFile: "Usage file",
    usageOwner: "Usage owner",
    declarationFile: "Declaration file",
    declarationName: "Declaration",
    declarationKind: "Declaration kind",
    includeFile: "Include file",
    includedSymbol: "Included symbol",
    includedKind: "Included kind",
    usedFromFile: "Used from file",
    receiver: "Receiver",
    memberName: "Member",
    expression: "Expression",
    implicitGlobalFile: "Implicit global variable file",
    implicitGlobalName: "Implicit global variable",
    assignmentFile: "Assignment file",
    assignmentTarget: "Assignment target",
    assignmentTargetFile: "Assignment target file",
    includeDepth: "Include depth",
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
    externalReferenceSummary: "Usage summary",
    includeUsageSummary: "Included file usage",
    topReferencedDeclarations: "Top referenced declarations",
    analysisCharts: "Charts",
    unreferencedDeclarations: "Unused declarations",
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
    reviewImplicitGlobalsAction:
      "Review the Implicit Global Variables sheet and decide whether declarations are missing.",
    reviewImplicitGlobalAssignmentsAction:
      "Review the Implicit Global Assignment Candidates sheet for possible cross-file writes.",
    reviewExternalUsagesAction: "Review the External File Usage sheet for other-file callers.",
    reviewMissingExternalUsagesAction: "No other-file usages were found for the target file.",
    reviewIncludedUsagesAction: "Review the Included Symbol Usage sheet for include dependencies.",
    reviewMissingIncludedUsagesAction: "No included-file symbol usages were found.",
    tableDescription: "Table description",
  },
  ja: {
    summary: "概要",
    analysisSummary: "分析サマリ",
    chartData: "チャート元データ",
    includeTree: "インクルードツリー",
    files: "ファイル",
    includes: "参照ファイル",
    declarations: "宣言",
    usages: "使用箇所",
    internalUsages: "ファイル内使用",
    externalFileUsages: "外部ファイルからの使用",
    includedSymbolUsages: "include 先シンボル使用",
    memberUsages: "メンバー使用",
    implicitGlobals: "暗黙global変数",
    implicitGlobalAssignments: "暗黙global変数代入候補",
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
    implicitGlobalCount: "暗黙global変数数",
    implicitGlobalAssignmentCount: "暗黙global変数代入候補数",
    unusedCount: "未使用数",
    truncated: "切り詰め",
    notTruncated: "切り詰めなし",
    relatedIncludeTreeAnalysis: "親戚 include tree 解析",
    analysisSettings: "解析設定",
    setting: "設定",
    excelLanguage: "Excel 言語",
    automatic: "自動",
    enabled: "有効",
    disabled: "無効",
    forced: "強制",
    notForced: "強制なし",
    forceRelatedIncludeTreeAnalysis: "親戚 include tree 解析の強制",
    skipTypeInference: "型推論を skip",
    analysisTypeDetails: "エディター推論型の詳細",
    maxDocuments: "Excel 出力 document 上限",
    maxTextLength: "Excel 出力 text 上限",
    includeTreeMaxDocuments: "Excel include tree document 上限",
    includeTreeMaxTextLength: "Excel include tree text 上限",
    includeTreeDescendant: "子孫",
    includeTreeAncestor: "祖先",
    includeTreeRelative: "親戚",
    direction: "方向",
    depth: "深さ",
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
    inferredType: "推論型",
    returnType: "戻り値の型",
    parameters: "引数",
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
    usageFile: "使用ファイル",
    usageOwner: "使用元ノード",
    declarationFile: "宣言ファイル",
    declarationName: "宣言名",
    declarationKind: "宣言種別",
    includeFile: "include ファイル",
    includedSymbol: "include 先シンボル",
    includedKind: "include 先種別",
    usedFromFile: "使用しているファイル",
    receiver: "receiver",
    memberName: "メンバー名",
    expression: "式",
    implicitGlobalFile: "暗黙global変数ファイル",
    implicitGlobalName: "暗黙global変数",
    assignmentFile: "代入候補ファイル",
    assignmentTarget: "代入対象",
    assignmentTargetFile: "代入対象ファイル",
    includeDepth: "include 元距離",
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
    externalReferenceSummary: "使用サマリ",
    includeUsageSummary: "include 先の使用",
    topReferencedDeclarations: "よく使われている宣言",
    analysisCharts: "グラフ",
    unreferencedDeclarations: "未使用の宣言",
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
    reviewImplicitGlobalsAction: "暗黙global変数 sheet で宣言漏れか確認",
    reviewImplicitGlobalAssignmentsAction:
      "暗黙global変数代入候補 sheet で include 元からの代入を確認",
    reviewExternalUsagesAction: "外部ファイルからの使用 sheet で利用元を確認",
    reviewMissingExternalUsagesAction: "対象ファイルは他ファイルから使われていない可能性あり",
    reviewIncludedUsagesAction: "include 先シンボル使用 sheet で include 依存を確認",
    reviewMissingIncludedUsagesAction: "include 先シンボルの使用なし",
    tableDescription: "表の説明",
  },
};

const tableDescriptions: Record<AspGraphLocale, Record<string, string>> = {
  en: {
    summary: "High-level counts and generation metadata for the exported target file analysis.",
    includeTree:
      "Include relationships that belong to the target file's descendants, ancestors, and related ancestor descendant trees.",
    analysisSettings: "Analysis settings used while generating this workbook.",
    reviewPriority: "Review-oriented summary of risks that usually need manual confirmation.",
    externalReferenceSummary:
      "Declaration counts grouped by kind, with how many are used or unused.",
    includeUsageSummary:
      "Usage counts for symbols declared in included files and used by the target file.",
    topReferencedDeclarations: "Most-used declarations in the target file, ordered by usage count.",
    unusedByKind: "Unused declaration totals grouped by declaration kind.",
    chartData: "Source data used by the workbook charts.",
    declarations: "Declarations that belong to the exported target file only.",
    internalUsages:
      "Usages inside the exported target file that point to target-file declarations.",
    externalFileUsages:
      "Usages from other files that point to declarations in the exported target file.",
    includedSymbolUsages:
      "Usages in the target file that point to declarations from included files.",
    memberUsages: "Member expression usages found in the target file.",
    implicitGlobals: "Implicit global variables inferred for the target file.",
    implicitGlobalAssignments:
      "Possible assignments from include-related context into inferred implicit globals.",
    unused: "Target-file declarations that have no detected usages.",
    unresolved: "Unresolved references, calls, and assignments found in the target file.",
  },
  ja: {
    summary: "出力対象ファイルの解析件数と生成情報の概要です。",
    includeTree:
      "対象ファイルの子孫、祖先、祖先から伸びる親戚 include tree に属する include 関係です。",
    analysisSettings: "この workbook を生成したときに使った解析設定です。",
    reviewPriority: "手作業で確認した方がよいリスク項目をまとめた表です。",
    externalReferenceSummary: "宣言種別ごとの総数、使用あり、未使用の内訳です。",
    includeUsageSummary: "対象ファイルが include 先の宣言をどの種類で使っているかの集計です。",
    topReferencedDeclarations: "対象ファイル内でよく使われている宣言を使用数順に並べた表です。",
    unusedByKind: "未使用宣言を宣言種別ごとに集計した表です。",
    chartData: "ワークブック内のグラフに使う元データです。",
    declarations: "出力対象ファイル自身にある宣言だけを並べた表です。",
    internalUsages: "出力対象ファイル内から同じファイル内の宣言へ向く使用箇所です。",
    externalFileUsages: "他ファイルから出力対象ファイル内の宣言へ向く使用箇所です。",
    includedSymbolUsages: "対象ファイルから include 先の宣言へ向く使用箇所です。",
    memberUsages: "対象ファイル内で見つかったメンバー式の使用箇所です。",
    implicitGlobals: "対象ファイルで推定された暗黙 global 変数です。",
    implicitGlobalAssignments: "include 関係から暗黙 global へ代入している可能性がある箇所です。",
    unused: "使用が検出されなかった対象ファイル内の宣言です。",
    unresolved: "対象ファイル内で名前解決できなかった参照、呼び出し、代入です。",
  },
};

const headerDescriptions: Record<AspGraphLocale, Record<string, string>> = {
  en: {
    [text.en.scope]: "Analysis scope used for this workbook.",
    [text.en.value]: "Value for the metric or setting named in the first column.",
    [text.en.setting]: "Configuration setting or analysis option name.",
    [text.en.direction]: "Relationship direction from the exported target file.",
    [text.en.depth]: "Include graph distance for this relationship.",
    [text.en.metric]: "Metric, issue, or grouping name.",
    [text.en.total]: "Total number of matching items.",
    [text.en.usedCount]: "Number of declarations that have detected usages.",
    [text.en.unusedRate]: "Share of declarations that have no detected usages.",
    [text.en.bar]: "Compact visual bar for comparing counts.",
    [text.en.file]: "File that owns the row's item.",
    [text.en.exists]: "Whether the include target exists on disk.",
    [text.en.rootFile]: "Whether the file is the exported target file.",
    [text.en.includesOut]: "Number of outgoing include links from the file.",
    [text.en.includedBy]: "Number of files that include this file.",
    [text.en.declarationCount]: "Number of declarations found for the file.",
    [text.en.sourceFile]: "File that contains the include directive.",
    [text.en.includePath]: "Path text written in the include directive.",
    [text.en.includeMode]: "Include mode, such as file or virtual.",
    [text.en.resolvedTarget]: "Resolved include target file.",
    [text.en.actualPath]: "Filesystem path found during include resolution.",
    [text.en.pathCaseMatches]: "Whether the include path casing matches the actual path.",
    [text.en.line]: "One-based line number for the range.",
    [text.en.column]: "One-based column number for the range.",
    [text.en.name]: "Declaration or item name.",
    [text.en.kind]: "Declaration or graph item kind.",
    [text.en.memberOf]: "Owning class or object, when the item is a member.",
    [text.en.bindingScope]: "Scope where the declaration is bound.",
    [text.en.procedureKind]: "Procedure form, such as Function, Sub, or Property.",
    [text.en.inferredType]: "Inferred or declared type. Unknown type-capable items use Variant.",
    [text.en.returnType]:
      "Inferred or declared return type. Unknown Function/Property returns use Variant.",
    [text.en.parameters]:
      "Procedure parameters with inferred types; unknown parameter types use Variant.",
    [text.en.implicit]: "Whether the declaration was inferred from implicit VBScript usage.",
    [text.en.array]: "Array kind and dimensions, when detected.",
    [text.en.referenceCount]: "Detected read/reference usage count.",
    [text.en.assignmentCount]: "Detected assignment/write usage count.",
    [text.en.callCount]: "Detected call usage count.",
    [text.en.status]: "Review status for the row.",
    [text.en.usageKind]: "Usage link category.",
    [text.en.role]: "Usage role, such as read, write, or call.",
    [text.en.usageFile]: "File where the usage occurs.",
    [text.en.usageOwner]: "Graph node that owns the usage.",
    [text.en.declarationFile]: "File where the referenced declaration is defined.",
    [text.en.declarationName]: "Referenced declaration name.",
    [text.en.declarationKind]: "Referenced declaration kind.",
    [text.en.includeFile]: "Included file that owns the referenced symbol.",
    [text.en.includedSymbol]: "Symbol declared in an included file.",
    [text.en.includedKind]: "Kind of the included symbol.",
    [text.en.usedFromFile]: "File that uses the included symbol.",
    [text.en.receiver]: "Receiver expression before the member access.",
    [text.en.memberName]: "Member name after the receiver.",
    [text.en.expression]: "Full member expression text.",
    [text.en.implicitGlobalFile]: "File that owns the implicit global variable.",
    [text.en.implicitGlobalName]: "Implicit global variable name.",
    [text.en.assignmentFile]: "File containing a possible assignment.",
    [text.en.assignmentTarget]: "Assignment target name.",
    [text.en.assignmentTargetFile]: "File that owns the assignment target.",
    [text.en.includeDepth]: "Distance through include parents from the target file.",
    [text.en.count]: "Count represented by this row.",
    [text.en.risk]: "Review risk or issue category.",
    [text.en.action]: "Suggested review action.",
  },
  ja: {
    [text.ja.scope]: "この workbook で使った解析範囲です。",
    [text.ja.value]: "1列目の項目や設定に対応する値です。",
    [text.ja.setting]: "設定または解析 option の名前です。",
    [text.ja.direction]: "出力対象ファイルから見た include 関係の方向です。",
    [text.ja.depth]: "この関係の include graph 上の距離です。",
    [text.ja.metric]: "指標、確認項目、分類の名前です。",
    [text.ja.total]: "条件に一致した項目の合計数です。",
    [text.ja.usedCount]: "使用が検出された宣言の数です。",
    [text.ja.unusedRate]: "使用が検出されなかった宣言の比率です。",
    [text.ja.bar]: "件数を比較するための簡易棒グラフです。",
    [text.ja.file]: "この行の項目を持つファイルです。",
    [text.ja.exists]: "include 先が disk 上に存在するかです。",
    [text.ja.rootFile]: "出力対象ファイルかどうかです。",
    [text.ja.includesOut]: "このファイルから出ている include 数です。",
    [text.ja.includedBy]: "このファイルを include しているファイル数です。",
    [text.ja.declarationCount]: "このファイルで見つかった宣言数です。",
    [text.ja.sourceFile]: "include directive が書かれているファイルです。",
    [text.ja.includePath]: "include directive に書かれた path 文字列です。",
    [text.ja.includeMode]: "file や virtual などの include mode です。",
    [text.ja.resolvedTarget]: "解決された include 先ファイルです。",
    [text.ja.actualPath]: "include 解決で見つかった実 filesystem path です。",
    [text.ja.pathCaseMatches]: "include path の大文字小文字が実 path と一致するかです。",
    [text.ja.line]: "範囲の1始まりの行番号です。",
    [text.ja.column]: "範囲の1始まりの列番号です。",
    [text.ja.name]: "宣言や項目の名前です。",
    [text.ja.kind]: "宣言や graph 項目の種別です。",
    [text.ja.memberOf]: "member の場合の所有 class や object です。",
    [text.ja.bindingScope]: "宣言が束縛される scope です。",
    [text.ja.procedureKind]: "Function、Sub、Property などの procedure 形式です。",
    [text.ja.inferredType]: "宣言または推論された型です。不明な型付き項目は Variant です。",
    [text.ja.returnType]:
      "宣言または推論された戻り値の型です。不明な Function/Property は Variant です。",
    [text.ja.parameters]: "procedure の引数と推論型です。不明な引数型は Variant です。",
    [text.ja.implicit]: "VBScript の暗黙使用から推定された宣言かどうかです。",
    [text.ja.array]: "検出できた配列種別と次元です。",
    [text.ja.referenceCount]: "読み取り/参照として検出された使用数です。",
    [text.ja.assignmentCount]: "代入/書き込みとして検出された使用数です。",
    [text.ja.callCount]: "呼び出しとして検出された使用数です。",
    [text.ja.status]: "この行の確認状態です。",
    [text.ja.usageKind]: "使用 link の分類です。",
    [text.ja.role]: "read、write、call などの使用 role です。",
    [text.ja.usageFile]: "使用箇所があるファイルです。",
    [text.ja.usageOwner]: "使用箇所を所有する graph node です。",
    [text.ja.declarationFile]: "参照先宣言が定義されているファイルです。",
    [text.ja.declarationName]: "参照先宣言の名前です。",
    [text.ja.declarationKind]: "参照先宣言の種別です。",
    [text.ja.includeFile]: "参照先 symbol を持つ include ファイルです。",
    [text.ja.includedSymbol]: "include 先ファイルで宣言された symbol です。",
    [text.ja.includedKind]: "include 先 symbol の種別です。",
    [text.ja.usedFromFile]: "include 先 symbol を使っているファイルです。",
    [text.ja.receiver]: "member access の receiver 式です。",
    [text.ja.memberName]: "receiver の後ろの member 名です。",
    [text.ja.expression]: "member 式全体です。",
    [text.ja.implicitGlobalFile]: "暗黙 global 変数を持つファイルです。",
    [text.ja.implicitGlobalName]: "暗黙 global 変数の名前です。",
    [text.ja.assignmentFile]: "代入候補があるファイルです。",
    [text.ja.assignmentTarget]: "代入対象の名前です。",
    [text.ja.assignmentTargetFile]: "代入対象を持つファイルです。",
    [text.ja.includeDepth]: "対象ファイルから include 元へたどった距離です。",
    [text.ja.count]: "この行が表す件数です。",
    [text.ja.risk]: "確認リスクや問題分類です。",
    [text.ja.action]: "推奨される確認作業です。",
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
    unresolvedFunction: "Unresolved Function/Sub",
    implicitGlobalVariable: "Implicit global variable",
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
    unresolvedFunction: "未解決Function/Sub",
    implicitGlobalVariable: "暗黙global変数",
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
  const normalizedPayload = normalizeAnalysisGraphPayload(payload);
  const generatedAt = options.generatedAt ?? new Date();
  const nodesById = new Map(normalizedPayload.nodes.map((node) => [node.id, node]));
  const fileNamesByUri = fileNamesByUriMap(normalizedPayload.nodes);
  const context = analysisContext(normalizedPayload, options, nodesById, fileNamesByUri);
  const analysisRows = analysisSummaryRows(locale, context, fileNamesByUri);
  const analysisChartStartRow = analysisRows.length + 3;
  const analysisRowsWithChartSpace = [
    ...analysisRows,
    [],
    sectionTitle(text[locale].analysisCharts, 1),
    ...blankRows(34),
  ];
  return excelSafeSheetNames([
    sheet(
      text[locale].summary,
      summaryRows(normalizedPayload, locale, generatedAt, context, options),
    ),
    sheet(
      text[locale].includeTree,
      includeTreeRows(normalizedPayload, locale, context, nodesById, fileNamesByUri),
    ),
    sheet(text[locale].analysisSummary, analysisRowsWithChartSpace, {
      autoFilter: false,
      images: analysisSummaryImages(locale, context, analysisChartStartRow),
      stickyRows: false,
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
        context.targetUsageCounts,
        fileNamesByUri,
      ),
    ),
    sheet(
      text[locale].internalUsages,
      usageRows(context.internalUsageLinks, locale, nodesById, fileNamesByUri, "internalUsages"),
    ),
    sheet(
      text[locale].externalFileUsages,
      usageRows(
        context.externalUsageLinks,
        locale,
        nodesById,
        fileNamesByUri,
        "externalFileUsages",
      ),
    ),
    sheet(
      text[locale].includedSymbolUsages,
      includedUsageRows(context.includedUsageLinks, locale, nodesById, fileNamesByUri),
    ),
    sheet(
      text[locale].memberUsages,
      memberUsageRows(context.memberUsageLinks, locale, nodesById, fileNamesByUri),
    ),
    sheet(
      text[locale].implicitGlobals,
      implicitGlobalRows(
        context.implicitGlobalDeclarations,
        locale,
        context.implicitGlobalUsageCounts,
        fileNamesByUri,
      ),
    ),
    sheet(
      text[locale].implicitGlobalAssignments,
      implicitGlobalAssignmentRows(
        context.implicitGlobalAssignmentCandidates,
        locale,
        fileNamesByUri,
      ),
    ),
    sheet(
      text[locale].unused,
      unusedDeclarationRows(
        context.unusedDeclarations,
        locale,
        context.targetUsageCounts,
        fileNamesByUri,
      ),
    ),
    sheet(
      text[locale].unresolved,
      unresolvedRows(context.unresolvedLinks, locale, nodesById, fileNamesByUri),
    ),
  ]);
}

function normalizeAnalysisGraphPayload(payload: AspGraphPayload): AspGraphPayload {
  return {
    ...payload,
    nodes: payload.nodes.map(normalizeAnalysisGraphNode),
  };
}

function normalizeAnalysisGraphNode(node: AspGraphNode): AspGraphNode {
  const legacy = node as AspGraphNode & LegacyImplicitGlobalNodeFields;
  if (node.declarationKind !== "variable") {
    return node;
  }
  if (
    node.implicitGlobal !== true &&
    legacy.implicitLocal !== true &&
    legacy.unresolvedGlobal !== true
  ) {
    return node;
  }
  const { implicitLocal: _implicitLocal, unresolvedGlobal: _unresolvedGlobal, ...rest } = legacy;
  return {
    ...rest,
    implicitGlobal: true,
    implicitGlobalCandidate:
      node.implicitGlobalCandidate === true ||
      legacy.implicitLocal === true ||
      legacy.unresolvedGlobal === true
        ? true
        : undefined,
  };
}

function summaryRows(
  payload: AspGraphPayload,
  locale: AspGraphLocale,
  generatedAt: Date,
  context: AnalysisContext,
  options: AnalysisExcelOptions,
): Cell[][] {
  const t = text[locale];
  const rows: Array<[string, string | number]> = [
    [t.scope, valueLabel("file", locale)],
    [t.root, context.targetFileName],
    [t.generatedAt, formatGeneratedAt(generatedAt)],
    [t.declarationsCount, context.targetDeclarations.length],
    [t.referencesCount, usageLinkCount(context.targetUsageLinks, "references")],
    [t.assignmentsCount, usageLinkCount(context.targetUsageLinks, "assignments")],
    [t.callsCount, usageLinkCount(context.targetUsageLinks, "calls")],
    [t.includesCount, context.includedFileUris.size],
    [t.unresolvedCount, usageLinkCount(context.unresolvedLinks)],
    [t.implicitGlobalCount, context.implicitGlobalDeclarations.length],
    [
      t.implicitGlobalAssignmentCount,
      context.implicitGlobalAssignmentCandidates.reduce((sum, item) => sum + item.count, 0),
    ],
    [t.unusedCount, context.unusedDeclarations.length],
    [t.truncated, truncationDisplay(payload.truncated?.reason, locale)],
    [
      t.relatedIncludeTreeAnalysis,
      enabledDisplay(options.settings?.includeRelatedIncludeTreesForUnresolved === true, locale),
    ],
  ];
  return [
    ...describedTable(
      locale,
      "summary",
      [t.name, t.value],
      rows.map(([name, value]) => [name, value]),
    ),
    [],
    ...describedSectionRows(
      t.analysisSettings,
      2,
      locale,
      "analysisSettings",
      [t.setting, t.value],
      analysisSettingRows(locale, options.settings),
    ),
  ];
}

function analysisSettingRows(
  locale: AspGraphLocale,
  settings: AnalysisExcelSettingsSummary | undefined,
): Cell[][] {
  const t = text[locale];
  return [
    [t.excelLanguage, localeSettingDisplay(settings?.excelLocale, locale)],
    [
      t.relatedIncludeTreeAnalysis,
      enabledDisplay(settings?.includeRelatedIncludeTreesForUnresolved === true, locale),
    ],
    [
      t.forceRelatedIncludeTreeAnalysis,
      forcedDisplay(settings?.forceRelatedIncludeTreeAnalysis === true, locale),
    ],
    [t.skipTypeInference, enabledDisplay(settings?.skipTypeInference === true, locale)],
    [t.analysisTypeDetails, enabledDisplay(settings?.includeAnalysisTypeDetails === true, locale)],
    [t.maxDocuments, settings?.maxDocuments ?? ""],
    [t.maxTextLength, settings?.maxTextLength ?? ""],
    [t.includeTreeMaxDocuments, settings?.includeTreeMaxDocuments ?? ""],
    [t.includeTreeMaxTextLength, settings?.includeTreeMaxTextLength ?? ""],
  ];
}

function localeSettingDisplay(
  setting: AspGraphLocale | "auto" | undefined,
  locale: AspGraphLocale,
): string {
  if (!setting || setting === "auto") {
    return text[locale].automatic;
  }
  return setting === "ja" ? "日本語" : "English";
}

function enabledDisplay(value: boolean, locale: AspGraphLocale): string {
  return value ? text[locale].enabled : text[locale].disabled;
}

function forcedDisplay(value: boolean, locale: AspGraphLocale): string {
  return value ? text[locale].forced : text[locale].notForced;
}

function includeTreeRows(
  payload: AspGraphPayload,
  locale: AspGraphLocale,
  context: AnalysisContext,
  nodesById: Map<string, AspGraphNode>,
  fileNamesByUri: Map<string, string>,
): Cell[][] {
  const t = text[locale];
  const headers = [
    t.direction,
    t.depth,
    t.sourceFile,
    t.includeFile,
    t.includePath,
    t.includeMode,
    t.exists,
    t.resolvedTarget,
    t.line,
    t.column,
  ];
  const rows = includeTreeRelations(payload, context.targetUri, nodesById)
    .sort(compareIncludeTreeRelations(fileNamesByUri))
    .map(({ link, source, target, direction, depth }) => {
      const firstRange = rangesForLink(link)[0]?.range;
      const resolvedUri = link.include?.resolvedUri ?? target.uri;
      return [
        includeTreeDirectionDisplay(direction, locale),
        depth,
        displayNameForUri(source.uri, fileNamesByUri),
        displayNameForUri(target.uri, fileNamesByUri),
        link.include?.path ?? link.label,
        valueLabel(link.include?.mode, locale),
        yn(link.include?.exists ?? target.exists === true, locale),
        displayNameForUri(resolvedUri, fileNamesByUri),
        oneBasedLine(firstRange),
        oneBasedColumn(firstRange),
      ];
    });
  return describedTable(locale, "includeTree", headers, rows);
}

type IncludeTreeDirection = "ancestor" | "descendant" | "relative";

interface IncludeTreeRelation {
  link: AspGraphLink;
  source: AspGraphNode;
  target: AspGraphNode;
  direction: IncludeTreeDirection;
  depth: number;
}

function includeTreeRelations(
  payload: AspGraphPayload,
  targetUri: string | undefined,
  nodesById: Map<string, AspGraphNode>,
): IncludeTreeRelation[] {
  if (!targetUri) {
    return [];
  }
  const targetIds = new Set(
    payload.nodes
      .filter((node) => isFileLikeGraphNode(node) && sameGraphUri(node.uri, targetUri))
      .map((node) => node.id),
  );
  if (targetIds.size === 0) {
    return [];
  }
  const includeLinks = payload.links.filter((link) => link.kind === "include");
  const outgoing = includeLinksByEndpoint(includeLinks, "source");
  const incoming = includeLinksByEndpoint(includeLinks, "target");
  const descendantDepths = includeTreeDepths(targetIds, outgoing);
  const ancestorDepths = includeTreeDepths(targetIds, incoming);
  const relativeDepths = relatedIncludeTreeDepths(ancestorDepths, outgoing, descendantDepths);
  const result: IncludeTreeRelation[] = [];
  for (const link of includeLinks) {
    const source = nodesById.get(link.source);
    const target = nodesById.get(link.target);
    if (!source || !target) {
      continue;
    }
    const relation = includeTreeRelationForLink(
      link,
      targetIds,
      descendantDepths,
      ancestorDepths,
      relativeDepths,
    );
    if (!relation) {
      continue;
    }
    result.push({ link, source, target, ...relation });
  }
  return result;
}

function includeLinksByEndpoint(
  links: AspGraphLink[],
  endpoint: "source" | "target",
): Map<string, AspGraphLink[]> {
  const result = new Map<string, AspGraphLink[]>();
  for (const link of links) {
    const key = link[endpoint];
    const existing = result.get(key);
    if (existing) {
      existing.push(link);
    } else {
      result.set(key, [link]);
    }
  }
  return result;
}

function includeTreeDepths(
  roots: Set<string>,
  linksBySource: Map<string, AspGraphLink[]>,
): Map<string, number> {
  const depths = new Map([...roots].map((id) => [id, 0]));
  const queue = [...roots];
  while (queue.length > 0) {
    const current = queue.shift() ?? "";
    const currentDepth = depths.get(current) ?? 0;
    for (const link of linksBySource.get(current) ?? []) {
      const next = link.source === current ? link.target : link.source;
      if (depths.has(next)) {
        continue;
      }
      depths.set(next, currentDepth + 1);
      queue.push(next);
    }
  }
  return depths;
}

function relatedIncludeTreeDepths(
  ancestorDepths: Map<string, number>,
  outgoing: Map<string, AspGraphLink[]>,
  descendantDepths: Map<string, number>,
): Map<string, number> {
  const result = new Map<string, number>();
  const ancestors = [...ancestorDepths.entries()].filter(([, depth]) => depth > 0);
  for (const [ancestorId, ancestorDepth] of ancestors) {
    const queue = [{ id: ancestorId, depth: ancestorDepth }];
    const visited = new Set<string>([ancestorId]);
    while (queue.length > 0) {
      const current = queue.shift();
      if (!current) {
        break;
      }
      for (const link of outgoing.get(current.id) ?? []) {
        const next = link.target;
        if (visited.has(next)) {
          continue;
        }
        visited.add(next);
        const nextDepth = current.depth + 1;
        if (!ancestorDepths.has(next) && !descendantDepths.has(next)) {
          const existing = result.get(next);
          if (existing === undefined || nextDepth < existing) {
            result.set(next, nextDepth);
          }
        }
        queue.push({ id: next, depth: nextDepth });
      }
    }
  }
  return result;
}

function includeTreeRelationForLink(
  link: AspGraphLink,
  targetIds: Set<string>,
  descendantDepths: Map<string, number>,
  ancestorDepths: Map<string, number>,
  relativeDepths: Map<string, number>,
): { direction: IncludeTreeDirection; depth: number } | undefined {
  const sourceDescendantDepth = descendantDepths.get(link.source);
  const targetDescendantDepth = descendantDepths.get(link.target);
  if (
    sourceDescendantDepth !== undefined &&
    targetDescendantDepth !== undefined &&
    targetDescendantDepth > sourceDescendantDepth
  ) {
    return { direction: "descendant", depth: targetDescendantDepth };
  }
  const sourceAncestorDepth = ancestorDepths.get(link.source);
  const targetAncestorDepth = ancestorDepths.get(link.target);
  if (
    sourceAncestorDepth !== undefined &&
    (targetAncestorDepth !== undefined || targetIds.has(link.target))
  ) {
    return { direction: "ancestor", depth: sourceAncestorDepth };
  }
  const sourceRelativeDepth = relativeDepths.get(link.source);
  const targetRelativeDepth = relativeDepths.get(link.target);
  if (sourceAncestorDepth !== undefined && targetRelativeDepth !== undefined) {
    return { direction: "relative", depth: targetRelativeDepth };
  }
  if (
    sourceAncestorDepth !== undefined &&
    targetDescendantDepth !== undefined &&
    !targetIds.has(link.target)
  ) {
    return { direction: "relative", depth: sourceAncestorDepth + targetDescendantDepth };
  }
  if (sourceRelativeDepth !== undefined && targetRelativeDepth !== undefined) {
    return { direction: "relative", depth: targetRelativeDepth };
  }
  if (sourceRelativeDepth !== undefined && targetDescendantDepth !== undefined) {
    return { direction: "relative", depth: sourceRelativeDepth + targetDescendantDepth };
  }
  return undefined;
}

function includeTreeDirectionDisplay(
  direction: IncludeTreeDirection,
  locale: AspGraphLocale,
): string {
  switch (direction) {
    case "ancestor":
      return text[locale].includeTreeAncestor;
    case "descendant":
      return text[locale].includeTreeDescendant;
    case "relative":
      return text[locale].includeTreeRelative;
  }
}

function compareIncludeTreeRelations(
  fileNamesByUri: Map<string, string>,
): (left: IncludeTreeRelation, right: IncludeTreeRelation) => number {
  const order: Record<IncludeTreeDirection, number> = {
    ancestor: 0,
    descendant: 1,
    relative: 2,
  };
  return (left, right) =>
    order[left.direction] - order[right.direction] ||
    left.depth - right.depth ||
    displayNameForUri(left.source.uri, fileNamesByUri).localeCompare(
      displayNameForUri(right.source.uri, fileNamesByUri),
    ) ||
    displayNameForUri(left.target.uri, fileNamesByUri).localeCompare(
      displayNameForUri(right.target.uri, fileNamesByUri),
    );
}

function formatGeneratedAt(date: Date): string {
  const offsetMinutes = -date.getTimezoneOffset();
  const sign = offsetMinutes >= 0 ? "+" : "-";
  const absoluteOffset = Math.abs(offsetMinutes);
  const offsetHours = Math.floor(absoluteOffset / 60);
  const offsetRemainderMinutes = absoluteOffset % 60;
  return `${date.getFullYear()}-${pad2(date.getMonth() + 1)}-${pad2(date.getDate())} ${pad2(
    date.getHours(),
  )}:${pad2(date.getMinutes())}:${pad2(date.getSeconds())} GMT${sign}${pad2(
    offsetHours,
  )}:${pad2(offsetRemainderMinutes)}`;
}

function pad2(value: number): string {
  return String(value).padStart(2, "0");
}

function truncationDisplay(reason: string | undefined, locale: AspGraphLocale): string {
  if (!reason) {
    return text[locale].notTruncated;
  }
  const match = /^([A-Za-z]+)>(\d+)$/.exec(reason);
  if (!match) {
    return reason;
  }
  const [, kind, limit] = match;
  if (locale === "ja") {
    switch (kind) {
      case "documents":
        return `ドキュメント数が ${limit} 件を超えたため切り詰められました`;
      case "text":
        return `解析対象テキストが ${limit} 文字を超えたため切り詰められました`;
      case "depth":
        return `include の深さが ${limit} を超えたため切り詰められました`;
      case "workspaceIndex":
        return `workspace index が ${limit} 件を超えたため切り詰められました`;
      default:
        return reason;
    }
  }
  switch (kind) {
    case "documents":
      return `Truncated because the document count exceeded ${limit}.`;
    case "text":
      return `Truncated because the analyzed text exceeded ${limit} characters.`;
    case "depth":
      return `Truncated because the include depth exceeded ${limit}.`;
    case "workspaceIndex":
      return `Truncated because the workspace index exceeded ${limit} entries.`;
    default:
      return reason;
  }
}

function analysisSummaryRows(
  locale: AspGraphLocale,
  context: AnalysisContext,
  fileNamesByUri: Map<string, string>,
): Cell[][] {
  const t = text[locale];
  const reviewHeaders = [t.metric, t.count, t.status, t.action];
  const externalSummaryHeaders = [t.kind, t.total, t.usedCount, t.unusedCount, t.usageCount, t.bar];
  const includeUsageHeaders = [t.usageKind, t.count, t.bar];
  const topReferencedHeaders = [
    t.name,
    t.kind,
    t.file,
    t.line,
    t.usageCount,
    t.referenceCount,
    t.assignmentCount,
    t.callCount,
  ];
  const unusedByKindHeaders = [t.kind, t.unusedCount, t.total, t.unusedRate, t.bar];
  return [
    ...describedSectionRows(
      t.reviewPriority,
      4,
      locale,
      "reviewPriority",
      reviewHeaders,
      reviewPriorityRows(locale, context),
    ),
    [],
    ...describedSectionRows(
      t.externalReferenceSummary,
      6,
      locale,
      "externalReferenceSummary",
      externalSummaryHeaders,
      declarationKindSummaryRows(context.targetDeclarations, locale, context.targetUsageCounts),
    ),
    [],
    ...describedSectionRows(
      t.includeUsageSummary,
      3,
      locale,
      "includeUsageSummary",
      includeUsageHeaders,
      usageCountRows(context.includedUsageLinks, locale),
    ),
    [],
    ...describedSectionRows(
      t.topReferencedDeclarations,
      8,
      locale,
      "topReferencedDeclarations",
      topReferencedHeaders,
      topReferencedDeclarationRows(
        context.targetDeclarations,
        locale,
        context.targetUsageCounts,
        fileNamesByUri,
      ),
    ),
    [],
    ...describedSectionRows(
      t.unusedByKind,
      5,
      locale,
      "unusedByKind",
      unusedByKindHeaders,
      unusedByKindRows(context.unusedDeclarations, context.targetDeclarations, locale),
    ),
  ];
}

function chartDataRows(locale: AspGraphLocale, context: AnalysisContext): Cell[][] {
  const t = text[locale];
  const reviewHeaders = [t.metric, t.count, t.status, t.action];
  const externalSummaryHeaders = [t.kind, t.total, t.usedCount, t.unusedCount, t.usageCount];
  const includeUsageHeaders = [t.usageKind, t.count];
  const unusedByKindHeaders = [t.kind, t.unusedCount, t.total, t.unusedRate];
  return [
    ...describedSectionRows(
      t.reviewPriority,
      4,
      locale,
      "chartData",
      reviewHeaders,
      reviewPriorityRows(locale, context),
    ),
    [],
    ...describedSectionRows(
      t.externalReferenceSummary,
      5,
      locale,
      "chartData",
      externalSummaryHeaders,
      declarationKindSummaryRows(context.targetDeclarations, locale, context.targetUsageCounts).map(
        (row) => row.slice(0, 5),
      ),
    ),
    [],
    ...describedSectionRows(
      t.includeUsageSummary,
      2,
      locale,
      "chartData",
      includeUsageHeaders,
      usageCountRows(context.includedUsageLinks, locale).map((row) => row.slice(0, 2)),
    ),
    [],
    ...describedSectionRows(
      t.unusedByKind,
      4,
      locale,
      "chartData",
      unusedByKindHeaders,
      unusedByKindRows(context.unusedDeclarations, context.targetDeclarations, locale).map((row) =>
        row.slice(0, 4),
      ),
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
    context.targetUsageCounts,
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
  const implicitGlobalCount = context.implicitGlobalDeclarations.length;
  const implicitGlobalAssignmentCount = context.implicitGlobalAssignmentCandidates.reduce(
    (sum, item) => sum + item.count,
    0,
  );
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
      label: t.implicitGlobalCount,
      count: implicitGlobalCount,
      status: implicitGlobalCount > 0 ? t.needsReview : t.ok,
      action: implicitGlobalCount > 0 ? t.reviewImplicitGlobalsAction : t.ok,
      tone: implicitGlobalCount > 0 ? "warning" : "good",
    },
    {
      label: t.implicitGlobalAssignmentCount,
      count: implicitGlobalAssignmentCount,
      status: implicitGlobalAssignmentCount > 0 ? t.present : t.none,
      action: implicitGlobalAssignmentCount > 0 ? t.reviewImplicitGlobalAssignmentsAction : t.ok,
      tone: implicitGlobalAssignmentCount > 0 ? "info" : "neutral",
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
    const key = declarationKindKey(node);
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
      valueLabel(declarationKindKey(node), locale),
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
    const key = declarationKindKey(node);
    counts.set(key, (counts.get(key) ?? 0) + 1);
  }
  const totals = new Map<string, number>();
  for (const node of declarations) {
    const key = declarationKindKey(node);
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
        target?.fullPath ?? target?.label ?? link.target,
        valueLabel(target ? declarationKindKey(target) : undefined, locale),
        declarationUsageTypeDisplay(target),
        displayNameForUri(uri, fileNamesByUri),
        oneBasedLine(range),
        oneBasedColumn(range),
        link.ranges.length > 1 ? 1 : link.count,
      ]);
    })
    .sort(compareRows(2, 3, 6, 7, 0));
  const headers = [
    t.usageKind,
    t.role,
    t.includeFile,
    t.includedSymbol,
    t.includedKind,
    t.inferredType,
    t.usedFromFile,
    t.line,
    t.column,
    t.count,
  ];
  return describedTable(locale, "includedSymbolUsages", headers, rows);
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
  const headers = [
    t.file,
    t.name,
    t.kind,
    t.memberOf,
    t.bindingScope,
    t.procedureKind,
    t.inferredType,
    t.returnType,
    t.parameters,
    t.implicit,
    t.array,
    t.line,
    t.column,
    t.referenceCount,
    t.assignmentCount,
    t.callCount,
    t.status,
  ];
  return describedTable(locale, "declarations", headers, rows);
}

function usageRows(
  links: AspGraphLink[],
  locale: AspGraphLocale,
  nodesById: Map<string, AspGraphNode>,
  fileNamesByUri: Map<string, string>,
  descriptionKey: string,
): Cell[][] {
  const t = text[locale];
  const rows = links
    .flatMap((link) => usageLinkRows(link, locale, nodesById, fileNamesByUri))
    .sort(compareRows(2, 8, 9, 5));
  const headers = [
    t.usageKind,
    t.role,
    t.usageFile,
    t.usageOwner,
    t.declarationFile,
    t.declarationName,
    t.declarationKind,
    t.inferredType,
    t.line,
    t.column,
    t.count,
  ];
  return describedTable(locale, descriptionKey, headers, rows);
}

function memberUsageRows(
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
        target?.receiverName ?? "",
        target?.memberName ?? target?.label ?? link.target,
        target?.fullPath ?? target?.label ?? link.target,
        displayNameForUri(uri, fileNamesByUri),
        oneBasedLine(range),
        oneBasedColumn(range),
        link.ranges.length > 1 ? 1 : link.count,
      ]);
    })
    .sort(compareRows(5, 6, 2, 3));
  const headers = [
    t.usageKind,
    t.role,
    t.receiver,
    t.memberName,
    t.expression,
    t.usageFile,
    t.line,
    t.column,
    t.count,
  ];
  return describedTable(locale, "memberUsages", headers, rows);
}

function implicitGlobalRows(
  declarations: AspGraphNode[],
  locale: AspGraphLocale,
  usageCounts: Map<string, UsageCounts>,
  fileNamesByUri: Map<string, string>,
): Cell[][] {
  const t = text[locale];
  const rows = declarations.sort(compareNodesByLocation(fileNamesByUri)).map((node) => {
    const usage = usageCounts.get(node.id);
    return [
      displayNameForUri(node.uri, fileNamesByUri),
      node.label,
      valueLabel(declarationKindKey(node), locale),
      declarationTypeDisplay(node),
      valueLabel(node.bindingScope, locale),
      oneBasedLine(node.range),
      oneBasedColumn(node.range),
      usageTotal(usage),
      usage?.references ?? 0,
      usage?.assignments ?? 0,
      usage?.calls ?? 0,
    ];
  });
  const headers = [
    t.file,
    t.name,
    t.kind,
    t.inferredType,
    t.bindingScope,
    t.line,
    t.column,
    t.usageCount,
    t.referenceCount,
    t.assignmentCount,
    t.callCount,
  ];
  return describedTable(locale, "implicitGlobals", headers, rows);
}

function implicitGlobalAssignmentRows(
  candidates: ImplicitGlobalAssignmentCandidate[],
  locale: AspGraphLocale,
  fileNamesByUri: Map<string, string>,
): Cell[][] {
  const t = text[locale];
  const rows = candidates
    .map((candidate) => [
      displayNameForUri(candidate.implicitGlobal.uri, fileNamesByUri),
      candidate.implicitGlobal.label,
      displayNameForUri(candidate.uri, fileNamesByUri),
      candidate.assignmentTarget?.label ?? "",
      displayNameForUri(candidate.assignmentTarget?.uri, fileNamesByUri),
      candidate.includeDepth,
      oneBasedLine(candidate.range),
      oneBasedColumn(candidate.range),
      candidate.count,
    ])
    .sort(compareRows(0, 1, 5, 2, 6, 7));
  const headers = [
    t.implicitGlobalFile,
    t.implicitGlobalName,
    t.assignmentFile,
    t.assignmentTarget,
    t.assignmentTargetFile,
    t.includeDepth,
    t.line,
    t.column,
    t.count,
  ];
  return describedTable(locale, "implicitGlobalAssignments", headers, rows);
}

function unusedDeclarationRows(
  unusedDeclarations: AspGraphNode[],
  locale: AspGraphLocale,
  usageCounts: Map<string, UsageCounts>,
  fileNamesByUri: Map<string, string>,
): Cell[][] {
  const t = text[locale];
  const headers = [
    t.file,
    t.name,
    t.kind,
    t.memberOf,
    t.bindingScope,
    t.inferredType,
    t.implicit,
    t.line,
    t.column,
    t.referenceCount,
    t.assignmentCount,
    t.callCount,
    t.status,
  ];
  const rows = unusedDeclarations.map((node) => {
    const usage = usageCounts.get(node.id);
    return [
      displayNameForUri(node.uri, fileNamesByUri),
      node.label,
      valueLabel(declarationKindKey(node), locale),
      node.memberOf ?? "",
      valueLabel(node.bindingScope, locale),
      declarationTypeDisplay(node),
      yn(node.implicit === true, locale),
      oneBasedLine(node.range),
      oneBasedColumn(node.range),
      usage?.references ?? 0,
      usage?.assignments ?? 0,
      usage?.calls ?? 0,
      text[locale].unusedStatus,
    ];
  });
  return describedTable(locale, "unused", headers, rows);
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
    .sort(compareRows(5, 6, 4));
  const headers = [
    t.usageKind,
    t.role,
    t.kind,
    t.source,
    t.name,
    t.file,
    t.line,
    t.column,
    t.count,
  ];
  return describedTable(locale, "unresolved", headers, rows);
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
    valueLabel(declarationKindKey(node), locale),
    node.memberOf ?? "",
    valueLabel(node.bindingScope, locale),
    valueLabel(node.procedureKind, locale),
    declarationTypeDisplay(node),
    declarationReturnTypeDisplay(node),
    declarationParametersDisplay(node),
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

function declarationTypeDisplay(node: AspGraphNode): string {
  if (!isTypeCapableDeclaration(node)) {
    return "";
  }
  return node.typeName ?? "Variant";
}

function declarationUsageTypeDisplay(node: AspGraphNode | undefined): string {
  if (!node) {
    return "";
  }
  if (isCallableReturnDeclaration(node)) {
    return node.typeName ?? "";
  }
  return declarationTypeDisplay(node);
}

function declarationReturnTypeDisplay(node: AspGraphNode): string {
  if (!isCallableReturnDeclaration(node)) {
    return "";
  }
  return node.typeName ?? "Variant";
}

function declarationParametersDisplay(node: AspGraphNode): string {
  if (!isCallableDeclaration(node) || !node.parameters?.length) {
    return "";
  }
  return node.parameters.map(parameterDisplay).join(", ");
}

function parameterDisplay(parameter: NonNullable<AspGraphNode["parameters"]>[number]): string {
  const prefix = parameter.optional === true ? "Optional " : "";
  const mode = parameter.mode ? `${parameterModeDisplay(parameter.mode)} ` : "";
  return `${prefix}${mode}${parameter.name} As ${parameter.typeName ?? "Variant"}`;
}

function parameterModeDisplay(mode: string): string {
  switch (mode.toLowerCase()) {
    case "byref":
      return "ByRef";
    case "byval":
      return "ByVal";
    default:
      return mode;
  }
}

function isTypeCapableDeclaration(node: AspGraphNode): boolean {
  const kind = node.declarationKind;
  return kind === "variable" || kind === "constant" || kind === "field" || kind === "parameter";
}

function isCallableDeclaration(node: AspGraphNode): boolean {
  return (
    node.declarationKind === "function" ||
    node.declarationKind === "sub" ||
    node.declarationKind === "method" ||
    node.declarationKind === "property"
  );
}

function isCallableReturnDeclaration(node: AspGraphNode): boolean {
  return (
    node.declarationKind === "function" ||
    node.declarationKind === "property" ||
    (node.declarationKind === "method" && node.procedureKind !== "sub")
  );
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
    valueLabel(unresolvedNodeKindKey(target), locale),
    source?.label ?? source?.fileName ?? link.source,
    target?.label ?? target?.fileName ?? link.target,
    displayNameForUri(uri, fileNamesByUri),
    oneBasedLine(range),
    oneBasedColumn(range),
    rangeCount,
  ]);
}

function unresolvedNodeKindKey(node: AspGraphNode | undefined): string {
  if (
    node?.kind === "vbUnresolved" &&
    (isCallableUnresolvedRole(node.role) || node.group === "unresolvedFunction")
  ) {
    return "unresolvedFunction";
  }
  return node?.kind === "vbUnresolved" ? "unresolvedReference" : "unknown";
}

function usageLinkRows(
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
    displayNameForUri(uri, fileNamesByUri),
    source?.label ?? source?.fileName ?? link.source,
    displayNameForUri(target?.uri, fileNamesByUri),
    target?.label ?? target?.fileName ?? link.target,
    valueLabel(target ? declarationKindKey(target) : undefined, locale),
    declarationUsageTypeDisplay(target),
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
  const rootDeclarations = sourceDeclarationNodes(payload.nodes)
    .filter((node) => sameGraphUri(node.uri, targetUri) && isAnalysisDeclaration(node))
    .sort(compareNodesByLocation(fileNamesByUri));
  const rootDeclarationIds = new Set(rootDeclarations.map((node) => node.id));
  const includedFileUris = includedFileUrisForTarget(payload, targetUri, nodesById);
  const includedDeclarations = sourceDeclarationNodes(payload.nodes).filter(
    (node) =>
      node.uri !== undefined &&
      includedFileUris.has(graphUriIdentity(node.uri)) &&
      isExternallyVisibleDeclaration(node),
  );
  const includedDeclarationIds = new Set(includedDeclarations.map((node) => node.id));
  const includedUsageLinks = filteredGraphLinks(
    payload.links,
    (link) => isUsageGraphLink(link) && includedDeclarationIds.has(link.target),
    ({ uri }) => sameGraphUri(uri, targetUri),
  );
  const targetDeclarations = rootDeclarations;
  const targetDeclarationIds = new Set(targetDeclarations.map((node) => node.id));
  const implicitGlobalDeclarations = targetDeclarations.filter(isImplicitGlobalDeclaration);
  const implicitGlobalCandidateDeclarations = implicitGlobalDeclarations.filter(
    isImplicitGlobalCandidateDeclaration,
  );
  const implicitGlobalDeclarationIds = new Set(implicitGlobalDeclarations.map((node) => node.id));
  const implicitGlobalUsageLinks = filteredGraphLinks(
    payload.links,
    (link) => isUsageGraphLink(link) && implicitGlobalDeclarationIds.has(link.target),
    () => true,
  );
  const implicitGlobalUsageCounts = usageCountsByTarget(implicitGlobalUsageLinks);
  const implicitGlobalAssignmentCandidates = implicitGlobalAssignmentCandidatesForPayload(
    payload,
    implicitGlobalCandidateDeclarations,
    nodesById,
  );
  const internalUsageLinks = filteredGraphLinks(
    payload.links,
    (link) => isUsageGraphLink(link) && rootDeclarationIds.has(link.target),
    ({ uri }) => sameGraphUri(uri, targetUri),
  );
  const externalUsageLinks = filteredGraphLinks(
    payload.links,
    (link) => isUsageGraphLink(link) && rootDeclarationIds.has(link.target),
    ({ uri }) => !sameGraphUri(uri, targetUri),
  );
  const targetUsageLinks = [...internalUsageLinks, ...externalUsageLinks, ...includedUsageLinks];
  const memberUsageLinks = filteredGraphLinks(
    payload.links,
    (link) => isMemberReferenceGraphLink(link, nodesById),
    ({ uri }) => sameGraphUri(uri, targetUri),
  );
  const unresolvedLinks = filteredGraphLinks(
    payload.links,
    (link) => isUnresolvedGraphLink(link, nodesById),
    ({ uri }) => sameGraphUri(uri, targetUri),
  );
  const targetUsageCounts = usageCountsByTarget(targetUsageLinks);
  const externalUsageCounts = usageCountsByTarget(externalUsageLinks);
  const unusedDeclarations = targetDeclarations
    .filter((node) => usageTotal(targetUsageCounts.get(node.id)) === 0)
    .sort(compareNodesByLocation(fileNamesByUri));
  return {
    targetUri,
    targetFileName,
    targetDeclarations,
    targetDeclarationIds,
    includedFileUris,
    includedDeclarationIds,
    targetUsageLinks,
    internalUsageLinks,
    externalUsageLinks,
    includedUsageLinks,
    memberUsageLinks,
    implicitGlobalDeclarations,
    implicitGlobalUsageCounts,
    implicitGlobalAssignmentCandidates,
    unresolvedLinks,
    targetUsageCounts,
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
  return node.bindingScope !== "local" && isAnalysisDeclaration(node);
}

function isAnalysisDeclaration(node: AspGraphNode): boolean {
  return node.kind === "vbDeclaration";
}

function isImplicitGlobalDeclaration(node: AspGraphNode): boolean {
  return (
    node.kind === "vbDeclaration" &&
    node.declarationKind === "variable" &&
    node.implicitGlobal === true
  );
}

function isImplicitGlobalCandidateDeclaration(node: AspGraphNode): boolean {
  return isImplicitGlobalDeclaration(node) && node.implicitGlobalCandidate === true;
}

function declarationKindKey(node: AspGraphNode): string {
  if (isImplicitGlobalDeclaration(node)) {
    return "implicitGlobalVariable";
  }
  return node.declarationKind ?? "unknown";
}

function implicitGlobalAssignmentCandidatesForPayload(
  payload: AspGraphPayload,
  implicitGlobals: AspGraphNode[],
  nodesById: Map<string, AspGraphNode>,
): ImplicitGlobalAssignmentCandidate[] {
  if (implicitGlobals.length === 0) {
    return [];
  }
  const candidates: ImplicitGlobalAssignmentCandidate[] = [];
  for (const implicitGlobal of implicitGlobals) {
    const ancestorDepths = includeAncestorDepthsByUri(payload, implicitGlobal.uri, nodesById);
    if (ancestorDepths.size === 0) {
      continue;
    }
    const implicitGlobalName = graphNameKey(implicitGlobal.label);
    for (const link of payload.links) {
      if (link.kind !== "assignments") {
        continue;
      }
      const target = nodesById.get(link.target);
      if (graphNameKey(target?.label ?? "") !== implicitGlobalName) {
        continue;
      }
      for (const { uri, range } of rangesForLink(link)) {
        const depth = uri ? ancestorDepths.get(graphUriIdentity(uri)) : undefined;
        if (depth === undefined) {
          continue;
        }
        candidates.push({
          implicitGlobal,
          assignmentTarget: target,
          uri,
          range,
          includeDepth: depth,
          count: link.ranges.length > 1 ? 1 : link.count,
        });
      }
    }
  }
  return candidates;
}

function includeAncestorDepthsByUri(
  payload: AspGraphPayload,
  targetUri: string | undefined,
  nodesById: Map<string, AspGraphNode>,
): Map<string, number> {
  if (!targetUri) {
    return new Map();
  }
  const targetFileNodeIds = new Set(
    payload.nodes
      .filter((node) => isFileLikeGraphNode(node) && sameGraphUri(node.uri, targetUri))
      .map((node) => node.id),
  );
  if (targetFileNodeIds.size === 0) {
    return new Map();
  }
  const parentIdsByTargetId = new Map<string, string[]>();
  for (const link of payload.links) {
    if (link.kind !== "include") {
      continue;
    }
    const existing = parentIdsByTargetId.get(link.target);
    if (existing) {
      existing.push(link.source);
    } else {
      parentIdsByTargetId.set(link.target, [link.source]);
    }
  }
  const result = new Map<string, number>();
  const visited = new Set(targetFileNodeIds);
  const queue = [...targetFileNodeIds].map((id) => ({ id, depth: 0 }));
  while (queue.length > 0) {
    const current = queue.shift();
    if (!current) {
      break;
    }
    for (const parentId of parentIdsByTargetId.get(current.id) ?? []) {
      if (visited.has(parentId)) {
        continue;
      }
      visited.add(parentId);
      const parent = nodesById.get(parentId);
      const depth = current.depth + 1;
      if (parent?.uri) {
        const parentUriKey = graphUriIdentity(parent.uri);
        const previousDepth = result.get(parentUriKey);
        if (previousDepth === undefined || depth < previousDepth) {
          result.set(parentUriKey, depth);
        }
      }
      queue.push({ id: parentId, depth });
    }
  }
  return result;
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

function graphNameKey(value: string): string {
  return value.toLowerCase();
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

function isMemberReferenceGraphLink(
  link: AspGraphLink,
  nodesById: Map<string, AspGraphNode>,
): boolean {
  const target = nodesById.get(link.target);
  return (
    target?.kind === "vbMemberReference" ||
    (target?.kind === "vbUnresolved" && target.role === "member")
  );
}

function isCallableUnresolvedRole(role: string | undefined): boolean {
  return role === "function" || role === "procedure" || role === "unknown";
}

function isUnresolvedGraphLink(link: AspGraphLink, nodesById: Map<string, AspGraphNode>): boolean {
  if (isMemberReferenceGraphLink(link, nodesById)) {
    return false;
  }
  return link.kind === "unresolvedReference" || nodesById.get(link.target)?.kind === "vbUnresolved";
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
  return uri ? (fileNamesByUri.get(uri) ?? displayPathForUriTextFallback(uri)) : "";
}

function displayPathForUriTextFallback(uriText: string): string {
  try {
    const parsed = new URL(uriText);
    if (parsed.protocol !== "file:") {
      return uriText;
    }
    const decodedPath = safeDecodeUriPath(parsed.pathname);
    const localPath = decodedPath.replace(/^\/([A-Za-z]:)/, "$1");
    return parsed.host ? `//${parsed.host}${localPath}` : localPath;
  } catch {
    return uriText;
  }
}

function safeDecodeUriPath(value: string): string {
  try {
    return decodeURIComponent(value);
  } catch {
    return value;
  }
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

function describedTable(
  locale: AspGraphLocale,
  descriptionKey: string,
  headers: string[],
  rows: Cell[][],
): Cell[][] {
  return tableRowsWithSideDescription(locale, descriptionKey, headers, rows);
}

function describedSectionRows(
  title: string,
  columnSpan: number,
  locale: AspGraphLocale,
  descriptionKey: string,
  headers: string[],
  rows: Cell[][],
): Cell[][] {
  return [
    sectionTitle(title, columnSpan),
    ...tableRowsWithSideDescription(locale, descriptionKey, headers, rows),
  ];
}

function tableRowsWithSideDescription(
  locale: AspGraphLocale,
  descriptionKey: string,
  headers: string[],
  rows: Cell[][],
): Cell[][] {
  const tableRows = [header(headers), ...rows];
  const sideRows = tableSideDescriptionRows(locale, descriptionKey, headers);
  const rowCount = Math.max(tableRows.length, sideRows.length + 1);
  return Array.from({ length: rowCount }, (_, index) => {
    const tableRow = tableRows[index] ?? blankCells(headers.length);
    const sideRow = index === 0 ? undefined : sideRows[index - 1];
    return sideRow ? [...padRow(tableRow, headers.length), null, ...sideRow] : tableRow;
  });
}

function tableSideDescriptionRows(
  locale: AspGraphLocale,
  descriptionKey: string,
  headers: string[],
): Cell[][] {
  const description = tableDescriptions[locale][descriptionKey] ?? descriptionKey;
  return [
    sideDescriptionRow(text[locale].tableDescription, description),
    ...headers.map((item) => headerDescriptionRow(item, locale)),
  ];
}

function blankCells(length: number): Cell[] {
  return Array.from({ length }, () => null);
}

function padRow(row: Cell[], length: number): Cell[] {
  return row.length >= length ? row : [...row, ...blankCells(length - row.length)];
}

function sideDescriptionRow(label: string, description: string): Cell[] {
  return [
    {
      value: label,
      type: String,
      fontWeight: "bold",
      textColor: "#374151",
      backgroundColor: "#F9FAFB",
      wrap: true,
    },
    {
      value: description,
      type: String,
      textColor: "#374151",
      backgroundColor: "#F9FAFB",
      wrap: true,
    },
  ];
}

function headerDescriptionRow(headerName: string, locale: AspGraphLocale): Cell[] {
  const description =
    headerDescriptions[locale][headerName] ??
    (locale === "ja" ? `${headerName} の値です。` : `Value for ${headerName}.`);
  return sideDescriptionRow(headerName, description);
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
  let max = 0;
  for (const value of values) {
    max = Math.max(max, value);
  }
  return max;
}

function sheet(
  name: string,
  rows: Cell[][],
  options: AnalysisSheetOptions = {},
): AnalysisExcelSheet {
  const autoFilterRef = options.autoFilter === false ? undefined : autoFilterRefForRows(rows);
  const headerRowIndex = headerRowIndexForRows(rows);
  const stickyRowsCount =
    options.stickyRows === false
      ? undefined
      : headerRowIndex === undefined
        ? 1
        : headerRowIndex + 1;
  return {
    sheet: name,
    data: rows,
    columns: columnsForRows(rows),
    ...(stickyRowsCount === undefined ? {} : { stickyRowsCount }),
    autoFilterRef,
    hidden: options.hidden === true ? true : undefined,
    images: options.images,
  };
}

const excelSheetNameMaxLength = 31;

function excelSafeSheetNames(sheets: AnalysisExcelSheet[]): AnalysisExcelSheet[] {
  const used = new Set<string>();
  return sheets.map((item) => ({
    ...item,
    sheet: uniqueExcelSheetName(item.sheet ?? "Sheet", used),
  }));
}

function uniqueExcelSheetName(name: string, used: Set<string>): string {
  const base = excelSafeSheetName(name);
  for (let index = 1; ; index += 1) {
    const suffix = index === 1 ? "" : ` ${index}`;
    const prefixLength = excelSheetNameMaxLength - suffix.length;
    const candidate = `${base.slice(0, prefixLength)}${suffix}`;
    const key = candidate.toLowerCase();
    if (!used.has(key)) {
      used.add(key);
      return candidate;
    }
  }
}

function excelSafeSheetName(name: string): string {
  const sanitized = name.replace(/[\\/?*:[\]]/g, "-").trim();
  return (sanitized || "Sheet").slice(0, excelSheetNameMaxLength);
}

function autoFilterRefForRows(rows: Cell[][]): string | undefined {
  const headerRowIndex = headerRowIndexForRows(rows);
  if (headerRowIndex === undefined) {
    return undefined;
  }
  const columnCount = leadingHeaderCellCount(rows[headerRowIndex] ?? []);
  if (columnCount === 0) {
    return undefined;
  }
  const firstRow = headerRowIndex + 1;
  const lastRow = lastTableRowIndex(rows, headerRowIndex, columnCount) + 1;
  return `A${firstRow}:${spreadsheetColumnName(columnCount - 1)}${lastRow}`;
}

function headerRowIndexForRows(rows: Cell[][]): number | undefined {
  const index = rows.findIndex((row) => row.some(isHeaderCell));
  return index >= 0 ? index : undefined;
}

function isHeaderCell(cell: Cell): boolean {
  return (
    typeof cell === "object" &&
    cell !== null &&
    "backgroundColor" in cell &&
    (cell as { backgroundColor?: unknown }).backgroundColor === "#1F4E79"
  );
}

function leadingHeaderCellCount(row: Cell[]): number {
  let count = 0;
  for (const cell of row) {
    if (!isHeaderCell(cell)) {
      break;
    }
    count += 1;
  }
  return count;
}

function lastTableRowIndex(rows: Cell[][], headerRowIndex: number, columnCount: number): number {
  for (let index = rows.length - 1; index >= headerRowIndex; index -= 1) {
    if (rows[index].slice(0, columnCount).some((cell) => cell !== null && cell !== undefined)) {
      return index;
    }
  }
  return headerRowIndex;
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
  let columnCount = 0;
  for (const row of rows) {
    columnCount = Math.max(columnCount, row.length);
  }
  return Array.from({ length: columnCount }, (_, column) => {
    let maxLength = 0;
    for (const row of rows) {
      maxLength = Math.max(maxLength, cellDisplayLength(row[column]));
    }
    return { width: Math.max(10, Math.min(48, maxLength + 2)) };
  });
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
