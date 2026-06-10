import React, {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  startTransition,
  useState,
} from "react";
import { createRoot } from "react-dom/client";
import ForceGraph2D from "react-force-graph-2d";
import ForceGraph3D from "react-force-graph-3d";
import SpriteText from "three-spritetext";
import { INITIAL, Registry, parseRawGrammar } from "vscode-textmate";
import { createOnigScanner, createOnigString, loadWASM } from "vscode-oniguruma";
import onigWasmUrl from "vscode-oniguruma/release/onig.wasm?url";
import classicAspGrammarJson from "../../syntaxes/classic-asp.tmLanguage.json";
import classicAspTagInjectionGrammarJson from "../../syntaxes/classic-asp-tag-injection.tmLanguage.json";
import vbscriptGrammarJson from "../../syntaxes/vbscript.tmLanguage.json";
import tailwindStyles from "./include-graph.css?inline";
import type {
  AspGraphLink,
  AspGraphLinkFilterCategory,
  AspGraphNode,
  AspGraphNodeCategory,
  AspGraphPayload,
  AspGraphRange,
  AspGraphSourceRangeRequestItem,
  AspGraphSourceRangeResponseItem,
} from "../include-graph-webview";
import type { ForceGraphMethods as ForceGraph2DMethods } from "react-force-graph-2d";
import type { ForceGraphMethods as ForceGraph3DMethods } from "react-force-graph-3d";
import type { IGrammar, IOnigLib, IRawGrammar, StateStack } from "vscode-textmate";

declare const acquireVsCodeApi: () => {
  postMessage(message: unknown): void;
};

declare global {
  interface Window {
    __ASP_LSP_GRAPH__?: AspGraphPayload;
  }
}

type ViewMode = "3d" | "2d";

type NodeColorCategory = AspGraphNodeCategory;

type GraphNode = AspGraphNode & {
  color: string;
  category: NodeColorCategory;
  referenceCount: number;
  value: number;
  x?: number;
  y?: number;
  z?: number;
  vx?: number;
  vy?: number;
  vz?: number;
  fx?: number;
  fy?: number;
  fz?: number;
};

type GraphLink = Omit<AspGraphLink, "source" | "target"> & {
  source: string | GraphNode;
  target: string | GraphNode;
  color: string;
};

type GraphData = {
  nodes: GraphNode[];
  links: GraphLink[];
};

interface OpenFlowchartMessage {
  type: "openFlowchart";
  uri: string;
  range?: AspGraphRange;
}

type WebviewTheme = "light" | "dark";
type WebviewThemeSetting = WebviewTheme | "auto";
type InfoPanelPosition = "left" | "right";

interface GraphThemePalette {
  canvasBackground: string;
  mutedLink: string;
  mutedNode: string;
  nodeColors: Record<NodeColorCategory, string>;
  linkFilterColors: Record<LinkFilterCategory, string>;
}

function isFileLikeGraphNode(node: Pick<AspGraphNode, "kind">): boolean {
  return node.kind === "file" || node.kind === "missingInclude";
}

type Selection = { type: "node"; item: GraphNode } | { type: "link"; item: GraphLink } | undefined;

type GraphStatsMetric = "files" | "declarations" | "links" | "missingIncludes";

type GraphStatsTarget = { type: "node"; id: string } | { type: "link"; id: string };

interface GraphStatsListItem {
  id: string;
  title: string;
  target: GraphStatsTarget;
  detail?: string;
  status?: string;
  color?: string;
  lineWidth?: number;
}

type HighlightState = {
  activeNodeIds: Set<string>;
  activeLinkIds: Set<string>;
};

type LinkFilterCategory = AspGraphLinkFilterCategory;

type CenteredSpriteText = SpriteText & {
  center: {
    y: number;
  };
};

interface PositionSyncEntry {
  id: string;
  sourceMode: ViewMode;
  x?: number;
  y?: number;
  z?: number;
  screenX?: number;
  screenY?: number;
  cameraDistance?: number;
}

interface PendingPositionSync {
  from: ViewMode;
  to: ViewMode;
  generation: number;
  entries: Map<string, PositionSyncEntry>;
}

interface PositionSyncTransform {
  centerX: number;
  centerY: number;
  scale: number;
}

interface SourceRangesMessage {
  type: "sourceRanges";
  requestId: string;
  items: AspGraphSourceRangeResponseItem[];
}

type SnippetLanguage = "classic-asp" | "vbscript";

type SnippetHighlightState =
  | { status: "loading" }
  | { status: "ready"; highlighter: SnippetHighlighter }
  | { status: "failed" };

interface SnippetHighlighter {
  classicAsp: IGrammar;
  vbscript: IGrammar;
}

interface HighlightOffsets {
  start: number;
  end: number;
}

const vscode = acquireVsCodeApi();
const graph = window.__ASP_LSP_GRAPH__;

type GraphLocale = "en" | "ja";

const graphMessageEn = {
  "action.fit": "Fit",
  "action.fitGraph": "Fit graph to canvas",
  "action.open": "Open",
  "action.openDirective": "Open directive",
  "action.openFile": "Open file",
  "action.openFirst": "Open first",
  "action.openFlowchart": "Open flowchart",
  "action.selectSource": "Select source",
  "action.selectTarget": "Select target",
  "direction.from": "from",
  "direction.to": "to",
  "detail.actualPath": "Actual path",
  "detail.actualPathHint":
    "Filesystem path VS Code resolved for the include target, when it differs from the directive text.",
  "detail.count": "count",
  "detail.countHint": "Number of source occurrences represented by this graph link.",
  "detail.declaration": "Declaration",
  "detail.directive": "Directive {source}",
  "detail.exists": "Exists",
  "detail.existsHint": "Whether the include target resolved to an existing file.",
  "detail.file": "File",
  "detail.fileHint": "Source file that owns or contains the selected graph node.",
  "detail.include": "Include",
  "detail.includeHint": "Include path written in the Classic ASP directive.",
  "detail.label": "Label",
  "detail.labelHint": "Raw graph label when it differs from the displayed link type.",
  "detail.line": "Line {line}",
  "detail.memberOf": "Member of",
  "detail.memberOfHint": "Class, object, or container that owns the selected declaration.",
  "detail.mode": "Mode",
  "detail.modeHint": "Include resolution mode used for this link.",
  "detail.nodeReferencesHint":
    "Number of visible include, assignment, reference, call, or unresolved-reference links targeting this node.",
  "detail.nodeStatusHint":
    "Additional node state such as root file, built-in symbol, or configured symbol.",
  "detail.nodeTypeHint": "Graph category assigned to the selected node.",
  "detail.references": "References",
  "detail.role": "Role",
  "detail.roleHint":
    "Extra graph role used to distinguish member links or other specialized relationships.",
  "detail.scope": "Scope",
  "detail.scopeHint": "VBScript binding scope for the selected declaration.",
  "detail.source": "Source",
  "detail.sourceHint": "Node where the selected graph link starts.",
  "detail.status": "Status",
  "detail.target": "Target",
  "detail.targetHint": "Node where the selected graph link ends.",
  "detail.type": "Type",
  "detail.linkTypeHint": "Graph category assigned to the selected link.",
  "empty.declarationSource": "Declaration source is unavailable.",
  "empty.graphData": "Graph data is unavailable.",
  "empty.includeSources": "No include sources found.",
  "empty.incomingLinks": "No incoming links found in the current graph.",
  "empty.includedFiles": "No included files found.",
  "empty.matchingItems": "No matching items in the current graph view.",
  "empty.outgoingLinks": "No outgoing links found in the current graph.",
  "empty.referencesOrCalls": "No assignments, references, or calls found in this graph.",
  "empty.sourceRanges": "No source ranges found for this link.",
  "empty.sourceUnavailable": "Source is unavailable.",
  "inspector.close": "Close inspector",
  "inspector.selectPrompt": "Select a node or link.",
  "label.class": "Class",
  "label.builtin": "Built-in",
  "label.classConstant": "Class constant",
  "label.classMethod": "Class method",
  "label.configured": "Configured",
  "label.event": "Event",
  "label.field": "Field",
  "label.file": "File",
  "label.function": "Function",
  "label.functionMethod": "Function method",
  "label.global": "Global",
  "label.globalConstant": "Global constant",
  "label.globalVariable": "Global variable",
  "label.implicitLocalVariable": "Implicit local variable",
  "label.unresolvedGlobalVariable": "Unresolved global variable",
  "label.local": "Local",
  "label.localConstant": "Local constant",
  "label.localVariable": "Local variable",
  "label.member": "Member",
  "label.method": "Method",
  "label.missing": "Missing",
  "label.missingFile": "Missing file",
  "label.missingInclude": "Missing include",
  "label.no": "No",
  "label.object": "Object",
  "label.parameter": "Parameter",
  "label.property": "Property",
  "label.root": "Root",
  "label.sub": "Sub",
  "label.subMethod": "Sub method",
  "label.unknown": "Unknown",
  "label.unresolved": "Unresolved",
  "label.unresolvedFunction": "Unresolved Function/Sub",
  "label.unresolvedMember": "Unresolved member",
  "label.virtual": "Virtual",
  "label.yes": "Yes",
  "legend.heading": "Legend",
  "legend.hideSingleNodes": "Hide single nodes",
  "legend.hideSingleNodesDescription": "Hide non-root nodes that have no visible links.",
  "legend.hideUnreferencedGlobalSymbols": "Hide unreferenced globals",
  "legend.hideUnreferencedGlobalSymbolsDescription":
    "Hide source functions, subs, classes, global variables, and global constants that are not tied to the root and have no references from other files.",
  "legend.linkFilters": "Link filters",
  "legend.nodeFilters": "Node filters",
  "legend.outgoingLinks": "Outgoing links",
  "legend.outgoingLinksDescription": "Also highlight links that start from the selected node.",
  "legend.selection": "Selection",
  "legend.unresolvedNodeFilters": "Unresolved nodes",
  "legend.visibilityFilters": "Visibility",
  "link.assignments.description":
    "VBScript assigns to a resolved variable, parameter, field, or property.",
  "link.assignments.label": "Assignment",
  "link.calls.description": "VBScript calls a procedure, function, method, constructor, or member.",
  "link.calls.label": "Call",
  "link.declares.description": "A file or containing scope declares a VBScript symbol.",
  "link.declares.label": "Declares",
  "link.include.description": "A Classic ASP file includes another file.",
  "link.include.label": "Include",
  "link.member.description": "VBScript references or calls an object member.",
  "link.member.label": "Member",
  "link.memberType": "Member {label}",
  "link.references.description": "VBScript reads or otherwise uses a resolved symbol.",
  "link.references.label": "Reference",
  "link.unresolvedReference.description":
    "VBScript references a symbol that could not be resolved.",
  "link.unresolvedReference.label": "Unresolved",
  "node.class.description": "VBScript class declarations.",
  "node.file.description": "Classic ASP source files and include files.",
  "node.missingInclude.description":
    "Include directives whose target file does not exist or could not be resolved.",
  "node.function.description": "Top-level VBScript Function declarations.",
  "node.globalConstant.description": "Top-level VBScript Const declarations.",
  "node.globalVariable.description":
    "Top-level VBScript variables and inferred global object variables.",
  "node.implicitLocalVariable.description":
    "Variables inferred from procedure or class bodies without a Dim declaration. They may write through to globals.",
  "node.unresolvedGlobalVariable.description":
    "Names inferred as global variables because no visible declaration was found.",
  "node.localConstant.description": "Procedure-local Const declarations and class constants.",
  "node.localVariable.description": "Procedure-local variables and class fields.",
  "node.member.description": "Built-in, configured, or unresolved object members.",
  "node.method.description": "Class methods whose procedure kind is unknown.",
  "node.methodFunction.description": "Class methods declared with Function.",
  "node.methodSub.description": "Class methods declared with Sub.",
  "node.parameter.description": "Function, Sub, method, and property parameters.",
  "node.property.description": "Class Property Get, Let, and Set declarations.",
  "node.root.description": "The graph root file or selected root context.",
  "node.sub.description": "Top-level VBScript Sub declarations.",
  "node.unresolvedFunction.description":
    "Calls to Function or Sub symbols that could not be resolved.",
  "node.unresolved.description": "Names that could not be resolved to a visible declaration.",
  "occurrence.includeDirective": "Include directive",
  "occurrence.one": "1 occurrence",
  "occurrence.other": "{count} occurrences",
  "section.declaration": "Declaration",
  "section.declarationHint": "Source snippet for the selected declaration.",
  "section.includedBy": "Included By",
  "section.includedByHint": "Files that include the selected file.",
  "section.includes": "Includes",
  "section.includesHint": "Files included by the selected file.",
  "section.incomingLinks": "Incoming links",
  "section.incomingLinksHint": "Visible graph links that target the selected node.",
  "section.occurrences": "Occurrences",
  "section.occurrencesHint": "Source occurrences represented by the selected graph link.",
  "section.outgoingLinks": "Outgoing links",
  "section.outgoingLinksHint": "Visible graph links that start from the selected node.",
  "section.referencesCalls": "Assignments / References / Calls",
  "section.referencesCallsHint": "Source snippets for assignments, references, and calls.",
  "section.sourceFileHint": "Source ranges grouped by file.",
  "snippet.loadingSource": "Loading source...",
  "stats.heading": "Graph list",
  "stats.metric.files": "Files",
  "stats.metric.links": "Links",
  "stats.metric.missing": "Missing",
  "stats.metric.vb": "VB",
  "stats.show": "Show graph list",
  "stats.title": "Graph list",
  "toolbar.graphMode": "Graph mode",
  "toolbar.graphModeDescription": "Switch between the 3D force graph and the 2D graph view.",
  "toolbar.matchCase": "Match case",
  "toolbar.matchCaseDescription":
    "Only match node names with the same uppercase and lowercase letters.",
  "toolbar.searchNodes": "Search nodes",
  "toolbar.stats": "List",
  "toolbar.truncated": "truncated: {reason}",
  "view.currentFileGraph": "Current File Graph",
  "view.folderGraph": "Folder Graph",
  "view.inspector": "Inspector",
  "view.resizeInspector": "Resize inspector pane",
  "view.workspaceGraph": "Workspace Graph",
} as const;

type GraphTextKey = keyof typeof graphMessageEn;
type GraphTextParams = Record<string, string | number>;

const graphMessages: Record<GraphLocale, Record<GraphTextKey, string>> = {
  en: graphMessageEn,
  ja: {
    "action.fit": "フィット",
    "action.fitGraph": "グラフをキャンバスに合わせる",
    "action.open": "開く",
    "action.openDirective": "directive を開く",
    "action.openFile": "ファイルを開く",
    "action.openFirst": "最初を開く",
    "action.openFlowchart": "フローチャートを開く",
    "action.selectSource": "元を選択",
    "action.selectTarget": "先を選択",
    "direction.from": "元",
    "direction.to": "先",
    "detail.actualPath": "実際の path",
    "detail.actualPathHint":
      "include 先として VS Code が解決した file system path です。directive の文字列と異なる時に出ます。",
    "detail.count": "件数",
    "detail.countHint": "この graph link が表す source 上の出現数です。",
    "detail.declaration": "宣言",
    "detail.directive": "Directive {source}",
    "detail.exists": "存在",
    "detail.existsHint": "include 先が実在する file に解決できたかどうかです。",
    "detail.file": "ファイル",
    "detail.fileHint": "選択した graph node を所有、または含んでいる source file です。",
    "detail.include": "include",
    "detail.includeHint": "Classic ASP directive に書かれている include path です。",
    "detail.label": "ラベル",
    "detail.labelHint": "表示中の link type と異なる場合の、生の graph label です。",
    "detail.line": "{line} 行目",
    "detail.memberOf": "所属",
    "detail.memberOfHint": "選択した declaration を所有する class、object、container です。",
    "detail.mode": "モード",
    "detail.modeHint": "この link で使われた include 解決 mode です。",
    "detail.nodeReferencesHint":
      "この node を target にする visible な include、代入、参照、呼び出し、未解決参照 link の数です。",
    "detail.nodeStatusHint": "root file、built-in symbol、configured symbol などの追加状態です。",
    "detail.nodeTypeHint": "選択した node に割り当てられた graph category です。",
    "detail.references": "参照",
    "detail.role": "役割",
    "detail.roleHint": "member link など、特別な関係を区別するための追加 role です。",
    "detail.scope": "スコープ",
    "detail.scopeHint": "選択した declaration の VBScript binding scope です。",
    "detail.source": "元",
    "detail.sourceHint": "選択した graph link が始まる node です。",
    "detail.status": "状態",
    "detail.target": "先",
    "detail.targetHint": "選択した graph link が終わる node です。",
    "detail.type": "種別",
    "detail.linkTypeHint": "選択した link に割り当てられた graph category です。",
    "empty.declarationSource": "宣言 source は利用できません。",
    "empty.graphData": "graph data は利用できません。",
    "empty.includeSources": "include 元は見つかりません。",
    "empty.incomingLinks": "現在の graph に incoming link はありません。",
    "empty.includedFiles": "include file は見つかりません。",
    "empty.matchingItems": "現在の graph view に一致する項目はありません。",
    "empty.outgoingLinks": "現在の graph に outgoing link はありません。",
    "empty.referencesOrCalls": "この graph に代入、参照、呼び出しはありません。",
    "empty.sourceRanges": "この link の source range は見つかりません。",
    "empty.sourceUnavailable": "source は利用できません。",
    "inspector.close": "inspector を閉じる",
    "inspector.selectPrompt": "node または link を選択してください。",
    "label.class": "クラス",
    "label.builtin": "組み込み",
    "label.classConstant": "クラス定数",
    "label.classMethod": "クラスメソッド",
    "label.configured": "設定済み",
    "label.event": "イベント",
    "label.field": "フィールド",
    "label.file": "ファイル",
    "label.function": "関数",
    "label.functionMethod": "関数メソッド",
    "label.global": "グローバル",
    "label.globalConstant": "グローバル定数",
    "label.globalVariable": "グローバル変数",
    "label.implicitLocalVariable": "dimなしローカル変数",
    "label.unresolvedGlobalVariable": "未解決グローバル変数",
    "label.local": "ローカル",
    "label.localConstant": "ローカル定数",
    "label.localVariable": "ローカル変数",
    "label.member": "メンバー",
    "label.method": "メソッド",
    "label.missing": "存在なし",
    "label.missingFile": "存在しないファイル",
    "label.missingInclude": "存在しない include",
    "label.no": "なし",
    "label.object": "オブジェクト",
    "label.parameter": "パラメーター",
    "label.property": "プロパティ",
    "label.root": "ルート",
    "label.sub": "Sub",
    "label.subMethod": "Sub メソッド",
    "label.unknown": "不明",
    "label.unresolved": "未解決",
    "label.unresolvedFunction": "未解決Function/Sub",
    "label.unresolvedMember": "未解決メンバー",
    "label.virtual": "仮想",
    "label.yes": "あり",
    "legend.heading": "凡例",
    "legend.hideSingleNodes": "単独 node を隠す",
    "legend.hideSingleNodesDescription": "visible link を持たない root 以外の node を隠します。",
    "legend.hideUnreferencedGlobalSymbols": "未外部参照を隠す",
    "legend.hideUnreferencedGlobalSymbolsDescription":
      "root に紐付かず、他のファイルから参照されていない source の function、sub、class、global variable、global constant node を隠します。",
    "legend.linkFilters": "リンクフィルター",
    "legend.nodeFilters": "ノードフィルター",
    "legend.outgoingLinks": "出力リンク",
    "legend.outgoingLinksDescription": "選択 node から出る link も highlight します。",
    "legend.selection": "選択",
    "legend.unresolvedNodeFilters": "未解決系",
    "legend.visibilityFilters": "非表示系",
    "link.assignments.description":
      "VBScript が解決済みの variable、parameter、field、property に代入します。",
    "link.assignments.label": "代入",
    "link.calls.description":
      "VBScript の procedure、function、method、constructor、member 呼び出しです。",
    "link.calls.label": "呼び出し",
    "link.declares.description": "file または包含 scope が VBScript symbol を宣言します。",
    "link.declares.label": "宣言",
    "link.include.description": "Classic ASP file が別の file を include します。",
    "link.include.label": "include",
    "link.member.description": "VBScript が object member を参照または呼び出します。",
    "link.member.label": "メンバー",
    "link.memberType": "メンバー {label}",
    "link.references.description": "VBScript が解決済み symbol を読み取り、または利用します。",
    "link.references.label": "参照",
    "link.unresolvedReference.description": "VBScript が解決できなかった symbol を参照します。",
    "link.unresolvedReference.label": "未解決",
    "node.class.description": "VBScript class declaration です。",
    "node.file.description": "Classic ASP source file と include file です。",
    "node.missingInclude.description":
      "include directive の target file が存在しない、または解決できなかった node です。",
    "node.function.description": "top-level の VBScript Function declaration です。",
    "node.globalConstant.description": "top-level の VBScript Const declaration です。",
    "node.globalVariable.description":
      "top-level の VBScript variable と推論された global object variable です。",
    "node.implicitLocalVariable.description":
      "procedure や class 内で Dim なしのまま使われた variable です。global を操作している可能性があります。",
    "node.unresolvedGlobalVariable.description":
      "visible declaration が見つからないため global 変数として推論した名前です。",
    "node.localConstant.description": "procedure-local Const declaration と class constant です。",
    "node.localVariable.description": "procedure-local variable と class field です。",
    "node.member.description": "built-in、configured、未解決の object member です。",
    "node.method.description": "procedure kind が不明な class method です。",
    "node.methodFunction.description": "Function で宣言された class method です。",
    "node.methodSub.description": "Sub で宣言された class method です。",
    "node.parameter.description": "Function、Sub、method、property の parameter です。",
    "node.property.description": "class Property Get、Let、Set declaration です。",
    "node.root.description": "graph の root file または選択された root context です。",
    "node.sub.description": "top-level の VBScript Sub declaration です。",
    "node.unresolvedFunction.description": "解決できなかった Function または Sub の呼び出しです。",
    "node.unresolved.description": "visible declaration に解決できなかった name です。",
    "occurrence.includeDirective": "include directive",
    "occurrence.one": "1 件",
    "occurrence.other": "{count} 件",
    "section.declaration": "宣言",
    "section.declarationHint": "選択した宣言の source snippet です。",
    "section.includedBy": "include 元",
    "section.includedByHint": "選択したファイルを include しているファイルです。",
    "section.includes": "include 先",
    "section.includesHint": "選択したファイルが include しているファイルです。",
    "section.incomingLinks": "入力リンク",
    "section.incomingLinksHint": "選択した node を target にする visible graph link です。",
    "section.occurrences": "出現箇所",
    "section.occurrencesHint": "選択した graph link が表す source 上の出現箇所です。",
    "section.outgoingLinks": "出力リンク",
    "section.outgoingLinksHint": "選択した node から始まる visible graph link です。",
    "section.referencesCalls": "代入 / 参照 / 呼び出し",
    "section.referencesCallsHint": "代入、参照、呼び出しの source snippet です。",
    "section.sourceFileHint": "source range を file ごとにまとめたものです。",
    "snippet.loadingSource": "source を読み込み中...",
    "stats.heading": "一覧",
    "stats.metric.files": "ファイル",
    "stats.metric.links": "リンク",
    "stats.metric.missing": "欠落",
    "stats.metric.vb": "VB",
    "stats.show": "graph 一覧を表示",
    "stats.title": "graph 一覧",
    "toolbar.graphMode": "グラフ表示",
    "toolbar.graphModeDescription": "3D force graph と 2D graph view を切り替えます。",
    "toolbar.matchCase": "大文字小文字を区別",
    "toolbar.matchCaseDescription": "node name の大文字小文字が一致するものだけを検索します。",
    "toolbar.searchNodes": "node を検索",
    "toolbar.stats": "一覧",
    "toolbar.truncated": "切り詰め: {reason}",
    "view.currentFileGraph": "現在のファイルのグラフ",
    "view.folderGraph": "フォルダーグラフ",
    "view.inspector": "情報",
    "view.resizeInspector": "inspector pane の幅を変更",
    "view.workspaceGraph": "ワークスペースグラフ",
  },
};

const graphLocale: GraphLocale = graph?.locale === "ja" ? "ja" : "en";

function graphText(key: GraphTextKey, params?: GraphTextParams): string {
  let message = graphMessages[graphLocale][key] ?? graphMessages.en[key];
  for (const [name, value] of Object.entries(params ?? {})) {
    message = message.replaceAll(`{${name}}`, String(value));
  }
  return message;
}

const darkNodeColors: Record<NodeColorCategory, string> = {
  root: "#ffffff",
  file: "#67d8ef",
  missingInclude: "#ff4db8",
  function: "#c792ea",
  sub: "#b39ddb",
  class: "#c3e88d",
  method: "#f78c6c",
  methodFunction: "#f78c6c",
  methodSub: "#ffb86c",
  property: "#ff9cac",
  member: "#ffb86c",
  globalVariable: "#ffcb6b",
  implicitLocalVariable: "#c3e88d",
  unresolvedGlobalVariable: "#f78c6c",
  globalConstant: "#82aaff",
  localVariable: "#dcdcaa",
  localConstant: "#80cbc4",
  parameter: "#b2ccd6",
  unresolvedFunction: "#ff9cac",
  unresolved: "#ff5370",
};

const lightNodeColors: Record<NodeColorCategory, string> = {
  root: "#111827",
  file: "#0369a1",
  missingInclude: "#be185d",
  function: "#7e22ce",
  sub: "#5b21b6",
  class: "#15803d",
  method: "#c2410c",
  methodFunction: "#c2410c",
  methodSub: "#b45309",
  property: "#be123c",
  member: "#b45309",
  globalVariable: "#b45309",
  implicitLocalVariable: "#15803d",
  unresolvedGlobalVariable: "#c2410c",
  globalConstant: "#1d4ed8",
  localVariable: "#854d0e",
  localConstant: "#0f766e",
  parameter: "#475569",
  unresolvedFunction: "#be123c",
  unresolved: "#dc2626",
};

const darkLinkColors: Record<AspGraphLink["kind"], string> = {
  include: "#82aaff",
  declares: "#89ddff",
  references: "#c3e88d",
  assignments: "#ffcb6b",
  calls: "#f78c6c",
  unresolvedReference: "#ff5370",
};

const lightLinkColors: Record<AspGraphLink["kind"], string> = {
  include: "#2563eb",
  declares: "#0284c7",
  references: "#16a34a",
  assignments: "#ca8a04",
  calls: "#ea580c",
  unresolvedReference: "#dc2626",
};

const graphThemePalettes: Record<WebviewTheme, GraphThemePalette> = {
  dark: {
    canvasBackground: "#11151c",
    mutedLink: "#29313d",
    mutedNode: "#2d3542",
    nodeColors: darkNodeColors,
    linkFilterColors: {
      ...darkLinkColors,
      member: darkNodeColors.member,
    },
  },
  light: {
    canvasBackground: "#f8fafc",
    mutedLink: "#cbd5e1",
    mutedNode: "#cbd5e1",
    nodeColors: lightNodeColors,
    linkFilterColors: {
      ...lightLinkColors,
      member: lightNodeColors.member,
    },
  },
};

const linkMeanings: Record<AspGraphLink["kind"], { label: string; description: string }> = {
  include: {
    label: graphText("link.include.label"),
    description: graphText("link.include.description"),
  },
  declares: {
    label: graphText("link.declares.label"),
    description: graphText("link.declares.description"),
  },
  references: {
    label: graphText("link.references.label"),
    description: graphText("link.references.description"),
  },
  assignments: {
    label: graphText("link.assignments.label"),
    description: graphText("link.assignments.description"),
  },
  calls: {
    label: graphText("link.calls.label"),
    description: graphText("link.calls.description"),
  },
  unresolvedReference: {
    label: graphText("link.unresolvedReference.label"),
    description: graphText("link.unresolvedReference.description"),
  },
};

const linkFilterLabels: Record<LinkFilterCategory, string> = {
  include: graphText("link.include.label"),
  declares: graphText("link.declares.label"),
  references: graphText("link.references.label"),
  assignments: graphText("link.assignments.label"),
  calls: graphText("link.calls.label"),
  unresolvedReference: graphText("link.unresolvedReference.label"),
  member: graphText("link.member.label"),
};

const linkFilterDescriptions: Record<LinkFilterCategory, string> = {
  include: graphText("link.include.description"),
  declares: graphText("link.declares.description"),
  references: graphText("link.references.description"),
  assignments: graphText("link.assignments.description"),
  calls: graphText("link.calls.description"),
  unresolvedReference: graphText("link.unresolvedReference.description"),
  member: graphText("link.member.description"),
};

const nodeCategoryLabels: Record<NodeColorCategory, string> = {
  root: graphText("label.root"),
  file: graphText("label.file"),
  missingInclude: graphText("label.missingInclude"),
  function: graphText("label.function"),
  sub: graphText("label.sub"),
  class: graphText("label.class"),
  method: graphText("label.method"),
  methodFunction: graphText("label.functionMethod"),
  methodSub: graphText("label.subMethod"),
  property: graphText("label.property"),
  member: graphText("label.member"),
  globalVariable: graphText("label.globalVariable"),
  implicitLocalVariable: graphText("label.implicitLocalVariable"),
  unresolvedGlobalVariable: graphText("label.unresolvedGlobalVariable"),
  globalConstant: graphText("label.globalConstant"),
  localVariable: graphText("label.localVariable"),
  localConstant: graphText("label.localConstant"),
  parameter: graphText("label.parameter"),
  unresolvedFunction: graphText("label.unresolvedFunction"),
  unresolved: graphText("label.unresolved"),
};

const nodeCategoryDescriptions: Record<NodeColorCategory, string> = {
  root: graphText("node.root.description"),
  file: graphText("node.file.description"),
  missingInclude: graphText("node.missingInclude.description"),
  class: graphText("node.class.description"),
  function: graphText("node.function.description"),
  sub: graphText("node.sub.description"),
  method: graphText("node.method.description"),
  methodFunction: graphText("node.methodFunction.description"),
  methodSub: graphText("node.methodSub.description"),
  property: graphText("node.property.description"),
  member: graphText("node.member.description"),
  globalVariable: graphText("node.globalVariable.description"),
  implicitLocalVariable: graphText("node.implicitLocalVariable.description"),
  unresolvedGlobalVariable: graphText("node.unresolvedGlobalVariable.description"),
  globalConstant: graphText("node.globalConstant.description"),
  localVariable: graphText("node.localVariable.description"),
  localConstant: graphText("node.localConstant.description"),
  parameter: graphText("node.parameter.description"),
  unresolvedFunction: graphText("node.unresolvedFunction.description"),
  unresolved: graphText("node.unresolved.description"),
};

const nodeCategoryOrder: NodeColorCategory[] = [
  "root",
  "file",
  "missingInclude",
  "class",
  "function",
  "sub",
  "methodFunction",
  "methodSub",
  "property",
  "member",
  "globalVariable",
  "implicitLocalVariable",
  "unresolvedGlobalVariable",
  "globalConstant",
  "localVariable",
  "localConstant",
  "parameter",
  "unresolvedFunction",
  "unresolved",
];

const unresolvedNodeCategorySet = new Set<NodeColorCategory>([
  "missingInclude",
  "unresolvedGlobalVariable",
  "unresolvedFunction",
  "unresolved",
]);

const linkFilterOrder: LinkFilterCategory[] = [
  "include",
  "declares",
  "references",
  "assignments",
  "calls",
  "unresolvedReference",
  "member",
];

const minimumNodeValue = 0.6;
const maximumNodeValue = 16;
const maximumNodeScaleReferenceCount = 144;
const graphFitDurationMs = 400;
const graphFitPadding2d = 100;
const graphFitPadding3d = 5;
const graphFocusDurationMs = 900;
const graph2dMinimumFocusZoom = 2.2;
const graph2dLinkFocusPadding = 120;
const graph3dMinimumFocusDistance = 70;
const graph3dLinkDistanceScale = 1.35;
const positionSyncPinMs = 600;
const graph3dSyncSpan = 160;
const graphInitialNodeSpacing = 28;
const graphInitialNodeZSpacing = 12;
const graphGoldenAngle = Math.PI * (3 - Math.sqrt(5));
const graphForceVelocityDecay = 0.42;
const graphChargeBaseStrength = 45;
const graphChargeValueStrength = 8;
const graphLinkDistanceBase = 36;
const graphLinkDistanceValueScale = 2.5;
const sourceHighlightMarkClassName =
  "rounded-sm bg-[#ffcb6b]/45 px-0.5 text-inherit shadow-[0_0_0_1px_rgb(255_203_107_/_75%)]";
const inspectorDefaultWidth = 320;
const inspectorMinimumWidth = 260;
const inspectorMaximumWidth = 560;
const graphMinimumWidth = 360;
const paneResizeHandleWidth = 6;
const paneResizeKeyboardStep = 16;

function positiveModulo(value: number, divisor: number): number {
  return ((value % divisor) + divisor) % divisor;
}

function useResolvedWebviewTheme(setting: WebviewThemeSetting | undefined): WebviewTheme {
  const [vscodeTheme, setVsCodeTheme] = useState<WebviewTheme>(() => detectedVsCodeTheme());
  useEffect(() => {
    if (setting === "light" || setting === "dark") {
      return undefined;
    }
    const observer = new MutationObserver(() => setVsCodeTheme(detectedVsCodeTheme()));
    observer.observe(document.body, { attributes: true, attributeFilter: ["class"] });
    return () => observer.disconnect();
  }, [setting]);
  return setting === "light" || setting === "dark" ? setting : vscodeTheme;
}

function detectedVsCodeTheme(): WebviewTheme {
  const classList = document.body.classList;
  return classList.contains("vscode-light") || classList.contains("vscode-high-contrast-light")
    ? "light"
    : "dark";
}

function App(): React.ReactElement {
  const theme = useResolvedWebviewTheme(graph?.settings?.theme);
  const themePalette = graphThemePalettes[theme];
  const [mode, setMode] = useState<ViewMode>(graph?.settings?.initialViewMode ?? "2d");
  const [selection, setSelection] = useState<Selection>();
  const [searchInput, setSearchInput] = useState("");
  const [searchQuery, setSearchQuery] = useState("");
  const [searchCursor, setSearchCursor] = useState(-1);
  const [searchMatchCase, setSearchMatchCase] = useState(false);
  const [inspectorWidth, setInspectorWidth] = useState(inspectorDefaultWidth);
  const [hideSingleNodes, setHideSingleNodes] = useState(graph?.settings?.hideSingleNodes ?? true);
  const [hideUnreferencedGlobalSymbols, setHideUnreferencedGlobalSymbols] = useState(
    graph?.settings?.hideUnreferencedGlobalSymbols ?? true,
  );
  const [showOutgoingSelectionLinks, setShowOutgoingSelectionLinks] = useState(
    graph?.settings?.showOutgoingSelectionLinks ?? true,
  );
  const [hiddenNodeCategories, setHiddenNodeCategories] = useState<Set<NodeColorCategory>>(
    () => new Set(graph?.settings?.hiddenNodeCategories ?? []),
  );
  const [hiddenLinkCategories, setHiddenLinkCategories] = useState<Set<LinkFilterCategory>>(
    () => new Set(graph?.settings?.hiddenLinkCategories ?? []),
  );
  const inspectorPosition = graph?.settings?.infoPanelPosition ?? "right";
  const graph2dRef = useRef<ForceGraph2DMethods<GraphNode, GraphLink> | undefined>(undefined);
  const graph3dRef = useRef<ForceGraph3DMethods<GraphNode, GraphLink> | undefined>(undefined);
  const hasAutoFit2dRef = useRef(false);
  const hasAutoFit3dRef = useRef(false);
  const positionSyncRef = useRef(new Map<string, PositionSyncEntry>());
  const pendingSyncRef = useRef<PendingPositionSync | undefined>(undefined);
  const positionSyncGenerationRef = useRef(0);
  const positionSyncReleaseTimerRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const searchInputRef = useRef<HTMLInputElement>(null);
  const skipAutoFitForModeRef = useRef(new Set<ViewMode>());
  const forceFitForModeRef = useRef(new Set<ViewMode>());
  const graphData = useMemo(() => graphDataFor(graph, themePalette), [themePalette]);
  const filteredGraphData = useMemo(
    () =>
      filterGraphData(
        graphData,
        hiddenNodeCategories,
        hiddenLinkCategories,
        hideSingleNodes,
        hideUnreferencedGlobalSymbols,
      ),
    [
      graphData,
      hiddenNodeCategories,
      hiddenLinkCategories,
      hideSingleNodes,
      hideUnreferencedGlobalSymbols,
    ],
  );
  const renderGraphData2d = useMemo(
    () => graphDataForRender(filteredGraphData, positionSyncRef.current, "2d"),
    [filteredGraphData],
  );
  const renderGraphData3d = useMemo(
    () => graphDataForRender(filteredGraphData, positionSyncRef.current, "3d"),
    [filteredGraphData],
  );
  const renderGraphData = mode === "3d" ? renderGraphData3d : renderGraphData2d;
  const filteredStats = useMemo(() => graphStatsFor(filteredGraphData), [filteredGraphData]);
  const searchTargets = useMemo(
    () => searchTargetsForSearch(searchQuery, searchMatchCase, filteredGraphData.nodes),
    [searchQuery, searchMatchCase, filteredGraphData.nodes],
  );
  const searchHighlight = useMemo(
    () => highlightForSearchTargets(searchTargets, filteredGraphData.links, searchQuery.trim()),
    [searchTargets, filteredGraphData.links, searchQuery],
  );
  const selectionHighlight = useMemo(
    () => highlightForSelection(selection, filteredGraphData.links, showOutgoingSelectionLinks),
    [selection, filteredGraphData.links, showOutgoingSelectionLinks],
  );
  const highlight = searchHighlight ?? selectionHighlight;
  const titleFileName = graphRootName(graph);
  const [layoutRef, layoutSize] = useElementSize<HTMLElement>();
  const [surfaceRef, surfaceSize] = useElementSize<HTMLElement>();
  const maximumInspectorWidth = maxInspectorWidthForLayout(layoutSize.width);
  const clampedInspectorWidth = clamp(inspectorWidth, inspectorMinimumWidth, maximumInspectorWidth);
  const layoutStyle = {
    "--inspector-width": `${clampedInspectorWidth}px`,
  } as React.CSSProperties;
  const layoutClassName =
    inspectorPosition === "left"
      ? "relative grid min-h-0 grid-cols-[var(--inspector-width)_6px_minmax(0,1fr)] overflow-hidden max-[780px]:grid-cols-1 max-[780px]:grid-rows-[minmax(0,1fr)]"
      : "relative grid min-h-0 grid-cols-[minmax(0,1fr)_6px_var(--inspector-width)] overflow-hidden max-[780px]:grid-cols-1 max-[780px]:grid-rows-[minmax(0,1fr)]";
  const surfaceClassName =
    inspectorPosition === "left"
      ? "relative order-3 min-h-0 min-w-0 overflow-hidden max-[780px]:order-1 [&_canvas]:block"
      : "relative order-1 min-h-0 min-w-0 overflow-hidden [&_canvas]:block";
  const canFitGraph =
    filteredGraphData.nodes.length > 0 && surfaceSize.width > 0 && surfaceSize.height > 0;
  const toggleNodeCategory = useCallback((category: NodeColorCategory) => {
    setHiddenNodeCategories((current) => toggledSet(current, category));
  }, []);
  const toggleLinkCategory = useCallback((category: LinkFilterCategory) => {
    setHiddenLinkCategories((current) => toggledSet(current, category));
  }, []);
  const updateSearchInput = useCallback((nextValue: string) => {
    setSearchInput(nextValue);
    startTransition(() => {
      setSearchQuery(nextValue);
    });
  }, []);
  const clearSearchInput = useCallback(() => {
    updateSearchInput("");
    setSearchCursor(-1);
  }, [updateSearchInput]);
  const fitGraphToCanvas = useCallback(
    (nextMode: ViewMode) => {
      if (!canFitGraph) {
        return false;
      }
      const graphRef = nextMode === "3d" ? graph3dRef : graph2dRef;
      if (!graphRef.current) {
        return false;
      }
      graphRef.current.zoomToFit(
        graphFitDurationMs,
        nextMode === "2d" ? graphFitPadding2d : graphFitPadding3d,
      );
      return true;
    },
    [canFitGraph],
  );
  const captureCurrentRenderPositions = useCallback(() => {
    capturePositionSyncEntries(
      mode,
      renderGraphData.nodes,
      graph2dRef.current,
      graph3dRef.current,
      positionSyncRef.current,
    );
  }, [mode, renderGraphData.nodes]);
  const selectGraphNode = useCallback(
    (node: GraphNode) => {
      captureCurrentRenderPositions();
      setSelection({ type: "node", item: node });
    },
    [captureCurrentRenderPositions],
  );
  const selectGraphLink = useCallback(
    (link: GraphLink) => {
      captureCurrentRenderPositions();
      setSelection({ type: "link", item: link });
    },
    [captureCurrentRenderPositions],
  );
  const selectAndFocusGraphTarget = useCallback(
    (target: GraphStatsTarget) => {
      const nextSelection = selectionForStatsTarget(target, filteredGraphData);
      if (!nextSelection) {
        return;
      }
      captureCurrentRenderPositions();
      setSelection(nextSelection);
      focusGraphTarget(target, mode, renderGraphData, graph2dRef.current, graph3dRef.current);
    },
    [captureCurrentRenderPositions, filteredGraphData, mode, renderGraphData],
  );
  const selectAndFocusGraphNode = useCallback(
    (node: GraphNode) => selectAndFocusGraphTarget({ type: "node", id: node.id }),
    [selectAndFocusGraphTarget],
  );
  const selectAndFocusGraphLink = useCallback(
    (link: GraphLink) => selectAndFocusGraphTarget({ type: "link", id: link.id }),
    [selectAndFocusGraphTarget],
  );
  const focusSearchResult = useCallback(
    (direction: 1 | -1) => {
      if (searchTargets.length === 0) {
        return false;
      }
      const baseCursor =
        searchCursor >= 0 && searchCursor < searchTargets.length
          ? searchCursor
          : direction > 0
            ? -1
            : 0;
      const nextCursor = positiveModulo(baseCursor + direction, searchTargets.length);
      const target = searchTargets[nextCursor];
      if (!target) {
        return false;
      }
      const nextSelection = selectionForStatsTarget(target, filteredGraphData);
      if (!nextSelection) {
        return false;
      }
      captureCurrentRenderPositions();
      setSearchCursor(nextCursor);
      setSelection(nextSelection);
      focusGraphTarget(target, mode, renderGraphData, graph2dRef.current, graph3dRef.current);
      return true;
    },
    [
      captureCurrentRenderPositions,
      filteredGraphData,
      mode,
      renderGraphData,
      searchCursor,
      searchTargets,
    ],
  );
  const switchGraphMode = useCallback(
    (nextMode: ViewMode) => {
      if (nextMode === mode) {
        return;
      }
      if (mode === "3d") {
        graph3dRef.current?.pauseAnimation();
      } else {
        graph2dRef.current?.pauseAnimation();
      }
      const generation = positionSyncGenerationRef.current + 1;
      positionSyncGenerationRef.current = generation;
      const entries = capturePositionSyncEntries(
        mode,
        renderGraphData.nodes,
        graph2dRef.current,
        graph3dRef.current,
        positionSyncRef.current,
      );
      pendingSyncRef.current =
        entries.size > 0
          ? {
              from: mode,
              to: nextMode,
              generation,
              entries,
            }
          : undefined;
      if (entries.size > 0) {
        skipAutoFitForModeRef.current.add(nextMode);
        if (mode === "2d" && nextMode === "3d") {
          forceFitForModeRef.current.add(nextMode);
        }
      }
      setMode(nextMode);
    },
    [mode, renderGraphData.nodes],
  );
  const handleEngineStop = useCallback(
    (nextMode: ViewMode) => {
      const autoFitRef = nextMode === "3d" ? hasAutoFit3dRef : hasAutoFit2dRef;
      if (forceFitForModeRef.current.delete(nextMode)) {
        autoFitRef.current = fitGraphToCanvas(nextMode);
        skipAutoFitForModeRef.current.delete(nextMode);
        return;
      }
      if (skipAutoFitForModeRef.current.delete(nextMode)) {
        autoFitRef.current = true;
        return;
      }
      if (autoFitRef.current) {
        return;
      }
      autoFitRef.current = fitGraphToCanvas(nextMode);
    },
    [fitGraphToCanvas],
  );
  useEffect(() => {
    setSearchCursor(-1);
  }, [searchTargets]);

  useEffect(() => {
    const handleSearchKeyDown = (event: KeyboardEvent) => {
      if (isSearchFocusShortcut(event)) {
        event.preventDefault();
        searchInputRef.current?.focus();
        searchInputRef.current?.select();
        return;
      }
      if (isSearchClearShortcut(event, searchInputRef.current)) {
        event.preventDefault();
        if (searchInput) {
          clearSearchInput();
        } else {
          searchInputRef.current?.blur();
        }
        return;
      }
      const direction = searchNavigationDirection(event, searchInputRef.current);
      if (direction && searchInput.trim()) {
        event.preventDefault();
        void focusSearchResult(direction);
      }
    };
    window.addEventListener("keydown", handleSearchKeyDown);
    return () => window.removeEventListener("keydown", handleSearchKeyDown);
  }, [clearSearchInput, focusSearchResult, searchInput]);

  useEffect(() => {
    const clearSelectionOnEscape = (event: KeyboardEvent) => {
      if (
        event.defaultPrevented ||
        document.activeElement === searchInputRef.current ||
        event.key !== "Escape"
      ) {
        return;
      }
      setSelection(undefined);
    };
    window.addEventListener("keydown", clearSelectionOnEscape);
    return () => window.removeEventListener("keydown", clearSelectionOnEscape);
  }, []);

  useEffect(
    () => () => {
      if (positionSyncReleaseTimerRef.current) {
        clearTimeout(positionSyncReleaseTimerRef.current);
      }
    },
    [],
  );

  useEffect(() => {
    configureGraphForces(graph2dRef.current);
    configureGraphForces(graph3dRef.current);
  }, [renderGraphData2d, renderGraphData3d]);

  useEffect(() => {
    if (selection && !isSelectionVisible(selection, filteredGraphData)) {
      setSelection(undefined);
    }
  }, [filteredGraphData, selection]);

  useEffect(() => {
    if (layoutSize.width <= 780) {
      return;
    }
    setInspectorWidth((currentWidth) =>
      clamp(currentWidth, inspectorMinimumWidth, maxInspectorWidthForLayout(layoutSize.width)),
    );
  }, [layoutSize.width]);

  useLayoutEffect(() => {
    const pending = pendingSyncRef.current;
    if (!pending || pending.to !== mode || !canFitGraph) {
      return;
    }
    const graphRef = mode === "3d" ? graph3dRef.current : graph2dRef.current;
    if (!graphRef) {
      return;
    }
    const pinnedNodeIds =
      mode === "3d"
        ? applyPositionSyncTo3d(pending, renderGraphData.nodes, graph3dRef.current)
        : applyPositionSyncTo2d(pending, renderGraphData.nodes, graph2dRef.current);
    pendingSyncRef.current = undefined;
    if (pinnedNodeIds.size === 0) {
      return;
    }
    graphRef.d3ReheatSimulation();
    if (positionSyncReleaseTimerRef.current) {
      clearTimeout(positionSyncReleaseTimerRef.current);
    }
    positionSyncReleaseTimerRef.current = setTimeout(() => {
      if (positionSyncGenerationRef.current !== pending.generation) {
        return;
      }
      releasePositionSyncPins(renderGraphData.nodes, pinnedNodeIds);
      const currentGraphRef = mode === "3d" ? graph3dRef.current : graph2dRef.current;
      currentGraphRef?.d3ReheatSimulation();
    }, positionSyncPinMs);
  }, [canFitGraph, mode, renderGraphData.nodes]);

  useEffect(() => {
    capturePositionSyncEntries(
      mode,
      renderGraphData.nodes,
      graph2dRef.current,
      graph3dRef.current,
      positionSyncRef.current,
    );
  }, [mode, renderGraphData.nodes]);

  useEffect(() => {
    if (mode === "3d") {
      graph3dRef.current?.resumeAnimation();
      graph2dRef.current?.pauseAnimation();
    } else {
      graph2dRef.current?.resumeAnimation();
      graph3dRef.current?.pauseAnimation();
    }
  }, [mode]);

  if (!graph) {
    return (
      <Shell theme={theme}>
        <main className="grid place-items-center text-[#9aa7b8]">
          {graphText("empty.graphData")}
        </main>
      </Shell>
    );
  }

  return (
    <Shell theme={theme}>
      <header className="grid grid-cols-[minmax(180px,1fr)_minmax(220px,320px)_auto_auto_auto] items-center gap-3 border-b border-[#2b3442] bg-[#171c25] px-3 py-2.5 max-[980px]:grid-cols-1 max-[980px]:items-stretch">
        <div className="flex min-w-0 items-center gap-2.5">
          <span className="overflow-hidden text-ellipsis whitespace-nowrap text-[13px] font-semibold text-[#d7dde8]">
            {graphScopeTitle(graph.scope)}
          </span>
          {titleFileName ? (
            <span
              className="min-w-0 overflow-hidden text-ellipsis whitespace-nowrap text-[12px] text-[#9aa7b8]"
              title={titleFileName}
            >
              {titleFileName}
            </span>
          ) : null}
          {graph.truncated ? (
            <span className="text-[11px] text-[#ffcb6b]">
              {graphText("toolbar.truncated", { reason: graph.truncated.reason })}
            </span>
          ) : null}
        </div>
        <div className="grid min-w-0 grid-cols-[minmax(0,1fr)_auto] items-center gap-2">
          <input
            ref={searchInputRef}
            type="search"
            className="h-7 min-w-0 rounded-md border border-[#394456] bg-[#11151c] px-2.5 text-xs text-[#d7dde8] outline-none placeholder:text-[#717b8c] focus:border-[#89ddff]"
            aria-label={graphText("toolbar.searchNodes")}
            placeholder={graphText("toolbar.searchNodes")}
            value={searchInput}
            onChange={(event) => updateSearchInput(event.currentTarget.value)}
          />
          <label
            className="inline-flex h-7 cursor-pointer select-none items-center gap-1.5 whitespace-nowrap rounded-md border border-[#394456] bg-[#151a22] px-2 text-[11px] text-[#b5c0d0]"
            title={graphText("toolbar.matchCaseDescription")}
          >
            <input
              type="checkbox"
              className="m-0 h-3.5 w-3.5 accent-[#89ddff]"
              checked={searchMatchCase}
              onChange={(event) => setSearchMatchCase(event.currentTarget.checked)}
            />
            {graphText("toolbar.matchCase")}
          </label>
        </div>
        <GraphStatsPopover
          graphData={filteredGraphData}
          stats={filteredStats}
          onSelectTarget={selectAndFocusGraphTarget}
        />
        <div
          className="inline-grid grid-cols-2 overflow-hidden rounded-md border border-[#394456]"
          aria-label={graphText("toolbar.graphMode")}
          title={graphText("toolbar.graphModeDescription")}
        >
          <button
            type="button"
            className={
              mode === "3d"
                ? "h-7 min-w-[42px] cursor-pointer border-0 bg-[#89ddff] text-[#11151c]"
                : "h-7 min-w-[42px] cursor-pointer border-0 bg-[#151a22] text-[#b5c0d0]"
            }
            title={graphText("toolbar.graphModeDescription")}
            onClick={() => switchGraphMode("3d")}
          >
            3D
          </button>
          <button
            type="button"
            className={
              mode === "2d"
                ? "h-7 min-w-[42px] cursor-pointer border-0 bg-[#89ddff] text-[#11151c]"
                : "h-7 min-w-[42px] cursor-pointer border-0 bg-[#151a22] text-[#b5c0d0]"
            }
            title={graphText("toolbar.graphModeDescription")}
            onClick={() => switchGraphMode("2d")}
          >
            2D
          </button>
        </div>
        <button
          type="button"
          className="h-7 min-w-[42px] cursor-pointer rounded-md border border-[#394456] bg-[#151a22] px-2.5 text-xs text-[#b5c0d0] disabled:cursor-not-allowed disabled:text-[#717b8c]"
          aria-label={graphText("action.fitGraph")}
          title={graphText("action.fitGraph")}
          disabled={!canFitGraph}
          onClick={() => fitGraphToCanvas(mode)}
        >
          {graphText("action.fit")}
        </button>
      </header>
      <main ref={layoutRef} className={layoutClassName} style={layoutStyle}>
        <section ref={surfaceRef} className={surfaceClassName}>
          <GraphLegend
            hiddenLinkCategories={hiddenLinkCategories}
            hiddenNodeCategories={hiddenNodeCategories}
            hideSingleNodes={hideSingleNodes}
            hideUnreferencedGlobalSymbols={hideUnreferencedGlobalSymbols}
            linkCategories={linkFilterOrder}
            nodeCategories={nodeCategoryOrder}
            themePalette={themePalette}
            showOutgoingSelectionLinks={showOutgoingSelectionLinks}
            onToggleHideSingleNodes={() => setHideSingleNodes((current) => !current)}
            onToggleHideUnreferencedGlobalSymbols={() =>
              setHideUnreferencedGlobalSymbols((current) => !current)
            }
            onToggleLinkCategory={toggleLinkCategory}
            onToggleNodeCategory={toggleNodeCategory}
            onToggleShowOutgoingSelectionLinks={() =>
              setShowOutgoingSelectionLinks((current) => !current)
            }
          />
          <div
            className={
              mode === "3d" ? "absolute inset-0" : "pointer-events-none absolute inset-0 opacity-0"
            }
            aria-hidden={mode !== "3d"}
          >
            <ForceGraph3D
              ref={graph3dRef}
              graphData={renderGraphData3d}
              width={surfaceSize.width}
              height={surfaceSize.height}
              backgroundColor={themePalette.canvasBackground}
              nodeColor={(node) => nodeColor(node as GraphNode, highlight, themePalette)}
              nodeVal={(node) => (node as GraphNode).value}
              nodeLabel={(node) => nodeLabel(node as GraphNode)}
              nodeThreeObjectExtend={true}
              nodeThreeObject={(node: GraphNode) => nodeTextObject(node, highlight, themePalette)}
              linkColor={(link) => linkColor(link as GraphLink, highlight, themePalette)}
              linkWidth={(link) => linkWidth3d(link as GraphLink, highlight)}
              linkLabel={(link) => linkLabel(link as GraphLink)}
              linkCurvature={0.25}
              linkDirectionalArrowLength={(link) => linkArrowLength(link as GraphLink)}
              linkDirectionalArrowRelPos={1}
              linkDirectionalArrowColor={(link) =>
                linkColor(link as GraphLink, highlight, themePalette)
              }
              linkDirectionalParticles={(link) => linkParticleCount(link as GraphLink, highlight)}
              linkDirectionalParticleWidth={(link) => linkParticleWidth3d(link as GraphLink)}
              onNodeClick={(node) => selectGraphNode(node as GraphNode)}
              onLinkClick={(link) => selectGraphLink(link as GraphLink)}
              onBackgroundClick={() => setSelection(undefined)}
              d3VelocityDecay={graphForceVelocityDecay}
              cooldownTicks={100}
              onEngineStop={() => handleEngineStop("3d")}
            />
          </div>
          <div
            className={
              mode === "2d" ? "absolute inset-0" : "pointer-events-none absolute inset-0 opacity-0"
            }
            aria-hidden={mode !== "2d"}
          >
            <ForceGraph2D
              ref={graph2dRef}
              graphData={renderGraphData2d}
              width={surfaceSize.width}
              height={surfaceSize.height}
              backgroundColor={themePalette.canvasBackground}
              nodeVal={(node) => (node as GraphNode).value}
              nodeLabel={(node) => nodeLabel(node as GraphNode)}
              linkColor={(link) => linkColor(link as GraphLink, highlight, themePalette)}
              linkWidth={(link) => linkWidth2d(link as GraphLink, highlight)}
              linkLabel={(link) => linkLabel(link as GraphLink)}
              linkCurvature={0.25}
              linkDirectionalArrowLength={(link) => linkArrowLength(link as GraphLink)}
              linkDirectionalArrowRelPos={1}
              linkDirectionalArrowColor={(link) =>
                linkColor(link as GraphLink, highlight, themePalette)
              }
              linkDirectionalParticles={(link) => linkParticleCount(link as GraphLink, highlight)}
              linkDirectionalParticleWidth={4.5}
              nodeCanvasObject={(node, canvas) =>
                paintNode(node as GraphNode, canvas, highlight, themePalette)
              }
              nodePointerAreaPaint={(node, color, canvas) =>
                paintNodePointerArea(node as GraphNode, color, canvas)
              }
              onNodeClick={(node) => selectGraphNode(node as GraphNode)}
              onLinkClick={(link) => selectGraphLink(link as GraphLink)}
              onBackgroundClick={() => setSelection(undefined)}
              d3VelocityDecay={graphForceVelocityDecay}
              cooldownTicks={100}
              onEngineStop={() => handleEngineStop("2d")}
            />
          </div>
        </section>
        <PaneResizeHandle
          maxWidth={maximumInspectorWidth}
          minWidth={inspectorMinimumWidth}
          position={inspectorPosition}
          width={clampedInspectorWidth}
          onWidthChange={setInspectorWidth}
        />
        <Inspector
          graphData={graphData}
          visibleGraphData={filteredGraphData}
          selection={selection}
          position={inspectorPosition}
          onClose={() => setSelection(undefined)}
          onSelectLink={selectAndFocusGraphLink}
          onSelectNode={selectAndFocusGraphNode}
        />
      </main>
    </Shell>
  );
}

function GraphLegend({
  hiddenLinkCategories,
  hiddenNodeCategories,
  hideSingleNodes,
  hideUnreferencedGlobalSymbols,
  linkCategories,
  nodeCategories,
  themePalette,
  showOutgoingSelectionLinks,
  onToggleHideSingleNodes,
  onToggleHideUnreferencedGlobalSymbols,
  onToggleLinkCategory,
  onToggleNodeCategory,
  onToggleShowOutgoingSelectionLinks,
}: {
  hiddenLinkCategories: ReadonlySet<LinkFilterCategory>;
  hiddenNodeCategories: ReadonlySet<NodeColorCategory>;
  hideSingleNodes: boolean;
  hideUnreferencedGlobalSymbols: boolean;
  linkCategories: LinkFilterCategory[];
  nodeCategories: NodeColorCategory[];
  themePalette: GraphThemePalette;
  showOutgoingSelectionLinks: boolean;
  onToggleHideSingleNodes(): void;
  onToggleHideUnreferencedGlobalSymbols(): void;
  onToggleLinkCategory(category: LinkFilterCategory): void;
  onToggleNodeCategory(category: NodeColorCategory): void;
  onToggleShowOutgoingSelectionLinks(): void;
}): React.ReactElement {
  const [isOpen, setOpen] = useState(false);
  const regularNodeCategories = nodeCategories.filter(
    (category) => !unresolvedNodeCategorySet.has(category),
  );
  const unresolvedNodeCategories = nodeCategories.filter((category) =>
    unresolvedNodeCategorySet.has(category),
  );
  return (
    <div className="absolute top-3 left-3 z-10 grid max-w-[min(560px,calc(100%_-_24px))] gap-0 rounded-md border border-[#303a49] bg-[#171c25]/90 px-2 py-0.5 shadow-[0_10px_26px_rgb(0_0_0_/_28%)] backdrop-blur">
      <button
        type="button"
        className="flex h-7 min-w-0 cursor-pointer items-center justify-between gap-3 border-0 bg-transparent p-0 text-left"
        aria-expanded={isOpen}
        title={graphText("legend.heading")}
        onClick={() => setOpen((current) => !current)}
      >
        <span className="min-w-0 overflow-hidden text-ellipsis whitespace-nowrap text-[11px] font-semibold tracking-[0.08em] text-[#9aa7b8] uppercase">
          {graphText("legend.heading")}
        </span>
        <span className="text-[11px] text-[#8d98a8]" aria-hidden="true">
          {isOpen ? "-" : "+"}
        </span>
      </button>
      {isOpen ? (
        <div className="grid gap-2 pt-1.5 pb-2">
          <LegendFilterGroup title={graphText("legend.linkFilters")}>
            {linkCategories.map((category) => (
              <LegendFilterItem
                key={category}
                checked={!hiddenLinkCategories.has(category)}
                color={themePalette.linkFilterColors[category]}
                label={linkFilterLabels[category]}
                title={linkFilterDescriptions[category]}
                variant="link"
                onToggle={() => onToggleLinkCategory(category)}
              />
            ))}
          </LegendFilterGroup>
          <LegendFilterGroup title={graphText("legend.nodeFilters")}>
            {regularNodeCategories.map((category) => (
              <LegendFilterItem
                key={category}
                checked={!hiddenNodeCategories.has(category)}
                color={themePalette.nodeColors[category]}
                label={nodeCategoryLabels[category]}
                title={nodeCategoryDescriptions[category]}
                variant="node"
                onToggle={() => onToggleNodeCategory(category)}
              />
            ))}
          </LegendFilterGroup>
          <LegendFilterGroup title={graphText("legend.unresolvedNodeFilters")}>
            {unresolvedNodeCategories.map((category) => (
              <LegendFilterItem
                key={category}
                checked={!hiddenNodeCategories.has(category)}
                color={themePalette.nodeColors[category]}
                label={nodeCategoryLabels[category]}
                title={nodeCategoryDescriptions[category]}
                variant="node"
                onToggle={() => onToggleNodeCategory(category)}
              />
            ))}
          </LegendFilterGroup>
          <LegendFilterGroup title={graphText("legend.visibilityFilters")}>
            <LegendFilterItem
              checked={hideSingleNodes}
              color={themePalette.mutedNode}
              label={graphText("legend.hideSingleNodes")}
              title={graphText("legend.hideSingleNodesDescription")}
              variant="node"
              onToggle={onToggleHideSingleNodes}
            />
            <LegendFilterItem
              checked={hideUnreferencedGlobalSymbols}
              color={themePalette.mutedNode}
              label={graphText("legend.hideUnreferencedGlobalSymbols")}
              title={graphText("legend.hideUnreferencedGlobalSymbolsDescription")}
              variant="node"
              onToggle={onToggleHideUnreferencedGlobalSymbols}
            />
          </LegendFilterGroup>
          <LegendFilterGroup title={graphText("legend.selection")}>
            <LegendFilterItem
              checked={showOutgoingSelectionLinks}
              color={themePalette.nodeColors.function}
              label={graphText("legend.outgoingLinks")}
              title={graphText("legend.outgoingLinksDescription")}
              variant="link"
              onToggle={onToggleShowOutgoingSelectionLinks}
            />
          </LegendFilterGroup>
        </div>
      ) : null}
    </div>
  );
}

function LegendFilterGroup({
  children,
  title,
}: {
  children: React.ReactNode;
  title: string;
}): React.ReactElement {
  return (
    <div>
      <LegendHeading>{title}</LegendHeading>
      <div className="grid grid-cols-[repeat(2,minmax(0,1fr))] gap-x-3 gap-y-1.5 max-[560px]:grid-cols-1">
        {children}
      </div>
    </div>
  );
}

function LegendHeading({ children }: { children: React.ReactNode }): React.ReactElement {
  return (
    <div className="mb-1.5 text-[11px] font-semibold tracking-[0.08em] text-[#9aa7b8] uppercase">
      {children}
    </div>
  );
}

function LegendFilterItem({
  checked,
  color,
  label,
  onToggle,
  title,
  variant,
}: {
  checked: boolean;
  color: string;
  label: string;
  onToggle(): void;
  title?: string;
  variant: "link" | "node";
}): React.ReactElement {
  return (
    <label
      className="group relative flex min-w-0 cursor-pointer select-none items-center gap-2 text-[11px] text-[#d7dde8]"
      title={title}
    >
      <input
        type="checkbox"
        className="m-0 h-3.5 w-3.5 shrink-0 accent-[#89ddff]"
        checked={checked}
        onChange={onToggle}
      />
      {variant === "link" ? (
        <span
          className="h-0 w-7 shrink-0 rounded-full border-t-2"
          style={{
            borderColor: color,
            borderTopWidth: label === linkFilterLabels.include ? 4 : 2,
          }}
          aria-hidden="true"
        />
      ) : (
        <span
          className="h-2.5 w-2.5 shrink-0 rounded-full"
          style={{ backgroundColor: color }}
          aria-hidden="true"
        />
      )}
      <span className="min-w-0 overflow-hidden text-ellipsis whitespace-nowrap">{label}</span>
      {title ? (
        <span
          role="tooltip"
          className="pointer-events-none absolute top-[calc(100%_+_6px)] left-0 z-30 hidden w-[min(260px,calc(100vw_-_40px))] rounded-md border border-[#405068] bg-[#0d1117] px-2 py-1.5 text-[11px] leading-[1.35] whitespace-normal text-[#d7dde8] shadow-[0_10px_24px_rgb(0_0_0_/_35%)] group-focus-within:block group-hover:block"
        >
          {title}
        </span>
      ) : null}
    </label>
  );
}

function useElementSize<TElement extends HTMLElement>(): [
  React.RefObject<TElement | null>,
  { width: number; height: number },
] {
  const ref = useRef<TElement>(null);
  const [size, setSize] = useState({ width: 0, height: 0 });

  useLayoutEffect(() => {
    const element = ref.current;
    if (!element) {
      return undefined;
    }

    const updateSize = () => {
      const { width, height } = element.getBoundingClientRect();
      const nextSize = {
        width: Math.max(1, Math.floor(width)),
        height: Math.max(1, Math.floor(height)),
      };
      setSize((currentSize) =>
        currentSize.width === nextSize.width && currentSize.height === nextSize.height
          ? currentSize
          : nextSize,
      );
    };

    updateSize();
    const observer = new ResizeObserver(updateSize);
    observer.observe(element);
    return () => observer.disconnect();
  }, []);

  return [ref, size];
}

function Shell({
  children,
  theme,
}: {
  children: React.ReactNode;
  theme: WebviewTheme;
}): React.ReactElement {
  return (
    <>
      <style>{tailwindStyles}</style>
      <div
        className="asp-lsp-graph-shell grid h-full min-w-0 grid-rows-[auto_1fr] bg-[#11151c] text-[#d7dde8]"
        data-asp-lsp-theme={theme}
      >
        {children}
      </div>
    </>
  );
}

function GraphStatsPopover({
  graphData,
  onSelectTarget,
  stats,
}: {
  graphData: GraphData;
  onSelectTarget(target: GraphStatsTarget): void;
  stats: AspGraphPayload["stats"];
}): React.ReactElement {
  const [isOpen, setOpen] = useState(false);
  const [activeMetric, setActiveMetric] = useState<GraphStatsMetric>("files");
  const containerRef = useRef<HTMLDivElement>(null);
  const statsItems = useMemo(
    () => statsItemsForMetric(activeMetric, graphData),
    [activeMetric, graphData],
  );

  useEffect(() => {
    if (!isOpen) {
      return;
    }
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        setOpen(false);
      }
    };
    const closeOnOutsidePointerDown = (event: PointerEvent) => {
      const target = event.target;
      if (target instanceof Node && containerRef.current?.contains(target)) {
        return;
      }
      setOpen(false);
    };
    window.addEventListener("keydown", closeOnEscape);
    window.addEventListener("pointerdown", closeOnOutsidePointerDown);
    return () => {
      window.removeEventListener("keydown", closeOnEscape);
      window.removeEventListener("pointerdown", closeOnOutsidePointerDown);
    };
  }, [isOpen]);

  return (
    <div ref={containerRef} className="relative inline-flex justify-end max-[980px]:justify-start">
      <button
        type="button"
        className="inline-flex h-7 cursor-pointer items-center rounded-md border border-[#394456] bg-[#151a22] px-2.5 text-xs text-[#b5c0d0] hover:border-[#4b5a70] hover:text-[#d7dde8]"
        aria-haspopup="dialog"
        aria-expanded={isOpen}
        aria-label={graphText("stats.show")}
        title={graphText("stats.show")}
        onClick={() => setOpen((current) => !current)}
      >
        <span className="font-semibold text-[#d7dde8]">{graphText("toolbar.stats")}</span>
      </button>
      {isOpen ? (
        <div
          role="dialog"
          aria-label={graphText("stats.title")}
          className="absolute top-[calc(100%_+_6px)] right-0 z-20 grid w-[min(430px,calc(100vw_-_24px))] gap-2 rounded-md border border-[#303a49] bg-[#171c25] p-2 shadow-[0_14px_34px_rgb(0_0_0_/_34%)] max-[980px]:right-auto max-[980px]:left-0"
        >
          <div className="text-[11px] font-semibold tracking-[0.08em] text-[#9aa7b8] uppercase">
            {graphText("stats.heading")}
          </div>
          <div className="grid grid-cols-2 gap-2 max-[360px]:grid-cols-1">
            <MetricButton
              activeMetric={activeMetric}
              label={graphText("stats.metric.files")}
              metric="files"
              value={stats.files}
              onSelect={setActiveMetric}
            />
            <MetricButton
              activeMetric={activeMetric}
              label={graphText("stats.metric.vb")}
              metric="declarations"
              value={stats.declarations}
              onSelect={setActiveMetric}
            />
            <MetricButton
              activeMetric={activeMetric}
              label={graphText("stats.metric.links")}
              metric="links"
              value={stats.links}
              onSelect={setActiveMetric}
            />
            <MetricButton
              activeMetric={activeMetric}
              label={graphText("stats.metric.missing")}
              metric="missingIncludes"
              value={stats.missingIncludes}
              onSelect={setActiveMetric}
            />
          </div>
          <GraphStatsList
            items={statsItems}
            onSelectItem={(target) => {
              setOpen(false);
              onSelectTarget(target);
            }}
          />
        </div>
      ) : null}
    </div>
  );
}

function MetricButton({
  activeMetric,
  label,
  metric,
  onSelect,
  value,
}: {
  activeMetric: GraphStatsMetric;
  label: string;
  metric: GraphStatsMetric;
  onSelect(metric: GraphStatsMetric): void;
  value: number;
}): React.ReactElement {
  const active = activeMetric === metric;
  return (
    <button
      type="button"
      className={
        active
          ? "inline-flex cursor-pointer items-baseline gap-[5px] rounded-md border border-[#89ddff] bg-[#1c2d3a] px-[7px] py-[3px] text-left text-[11px] text-[#d7dde8]"
          : "inline-flex cursor-pointer items-baseline gap-[5px] rounded-md border border-[#303a49] bg-transparent px-[7px] py-[3px] text-left text-[11px] text-[#9aa7b8] hover:border-[#4b5a70] hover:text-[#d7dde8]"
      }
      aria-pressed={active}
      title={`${label}: ${value}`}
      onClick={() => onSelect(metric)}
    >
      <span>{label}</span>
      <strong className="text-xs text-[#f4f7fb]">{value}</strong>
    </button>
  );
}

function GraphStatsList({
  items,
  onSelectItem,
}: {
  items: GraphStatsListItem[];
  onSelectItem(target: GraphStatsTarget): void;
}): React.ReactElement {
  if (items.length === 0) {
    return (
      <p className="m-0 rounded-md border border-[#303a49] bg-[#11151c] p-2 text-xs text-[#8d98a8]">
        {graphText("empty.matchingItems")}
      </p>
    );
  }
  return (
    <div className="grid max-h-72 gap-1.5 overflow-auto pr-1">
      {items.map((item) => (
        <button
          key={item.id}
          type="button"
          className="grid cursor-pointer gap-1 rounded-md border border-[#303a49] bg-[#11151c] p-2 text-left hover:border-[#4b5a70]"
          title={detailParts(item.title, item.detail, item.status).join(" · ")}
          onClick={() => onSelectItem(item.target)}
        >
          <div className="flex min-w-0 items-center gap-2">
            {item.color ? (
              <GraphStatsColorIndicator
                color={item.color}
                lineWidth={item.lineWidth}
                variant={item.target.type}
              />
            ) : null}
            <span className="min-w-0 flex-1 overflow-hidden text-ellipsis whitespace-nowrap text-xs font-semibold text-[#d7dde8]">
              {item.title}
            </span>
            {item.status ? (
              <span className="shrink-0 rounded border border-[#405068] px-1.5 py-[1px] text-[10px] text-[#9aa7b8]">
                {item.status}
              </span>
            ) : null}
          </div>
          {item.detail ? (
            <div className="text-[11px] leading-[1.35] text-[#8d98a8] [overflow-wrap:anywhere]">
              {item.detail}
            </div>
          ) : null}
        </button>
      ))}
    </div>
  );
}

function GraphStatsColorIndicator({
  color,
  lineWidth,
  variant,
}: {
  color: string;
  lineWidth?: number;
  variant: GraphStatsTarget["type"];
}): React.ReactElement {
  if (variant === "link") {
    return (
      <span
        className="h-0 w-7 shrink-0 rounded-full border-t-2"
        style={{ borderColor: color, borderTopWidth: lineWidth ?? 2 }}
        aria-hidden="true"
      />
    );
  }
  return (
    <span
      className="h-2.5 w-2.5 shrink-0 rounded-full"
      style={{ backgroundColor: color }}
      aria-hidden="true"
    />
  );
}

function statsItemsForMetric(metric: GraphStatsMetric, graphData: GraphData): GraphStatsListItem[] {
  switch (metric) {
    case "files":
      return graphData.nodes.filter(isFileLikeGraphNode).map((node) => ({
        id: `file:${node.id}`,
        title: node.label,
        target: { type: "node", id: node.id },
        detail: detailParts(nodeFileLabel(node)).join(" · "),
        status: fileStatsStatus(node),
        color: node.color,
      }));
    case "declarations":
      return graphData.nodes
        .filter((node) => node.kind === "vbDeclaration")
        .map((node) => ({
          id: `declaration:${node.id}`,
          title: node.label,
          target: { type: "node", id: node.id },
          detail: detailParts(
            nodeTypeLabel(node),
            nodeFileLabel(node),
            rangeLineLabel(node.range),
          ).join(" · "),
          status: nodeStatusLabel(node),
          color: node.color,
        }));
    case "links":
      return linkStatsItems(graphData);
    case "missingIncludes":
      return missingIncludeStatsItems(graphData);
  }
}

function linkStatsItems(graphData: GraphData): GraphStatsListItem[] {
  const nodesById = graphNodeMap(graphData.nodes);
  return graphData.links.map((link) => ({
    id: `link:${link.id}`,
    title: linkStatsTitle(link),
    target: { type: "link", id: link.id },
    detail: detailParts(
      `${endpointLabel(link.source, nodesById)} -> ${endpointLabel(link.target, nodesById)}`,
      graphRoleLabel(link.role),
    ).join(" · "),
    status: `x${link.count}`,
    color: link.color,
    lineWidth: linkSwatchWidth(link),
  }));
}

function missingIncludeStatsItems(graphData: GraphData): GraphStatsListItem[] {
  const nodesById = graphNodeMap(graphData.nodes);
  return graphData.links
    .filter((link) => link.kind === "include" && link.include?.exists === false)
    .map((link) => {
      const directive = link.ranges[0];
      return {
        id: `missing:${link.id}`,
        title: link.include?.path ?? link.label,
        target: { type: "link", id: link.id },
        detail: detailParts(
          directive
            ? graphText("detail.directive", { source: directiveSourceLabel(directive) })
            : undefined,
          `${endpointLabel(link.source, nodesById)} -> ${endpointLabel(link.target, nodesById)}`,
          includeModeLabel(link.include?.mode),
        ).join(" · "),
        status: graphText("label.missing"),
        color: link.color,
        lineWidth: linkSwatchWidth(link),
      };
    });
}

function fileStatsStatus(node: GraphNode): string {
  const status = detailParts(
    node.isRoot ? graphText("label.root") : undefined,
    node.exists === false ? graphText("label.missing") : undefined,
  );
  return status.length > 0 ? status.join(" / ") : graphText("label.file");
}

function linkStatsTitle(link: GraphLink): string {
  if (graphLinkFilterCategory(link) === "member") {
    return linkFilterLabels.member;
  }
  return linkMeanings[link.kind].label;
}

function endpointLabel(
  endpoint: string | GraphNode,
  nodesById: ReadonlyMap<string, GraphNode>,
): string {
  const id = nodeIdForEndpoint(endpoint);
  return nodesById.get(id)?.label ?? id;
}

function directiveSourceLabel(location: {
  uri: string;
  displayPath?: string;
  range: AspGraphRange;
}): string {
  return detailParts(
    location.displayPath ?? pathForDisplay(location.uri) ?? location.uri,
    rangeLineLabel(location.range),
  ).join(" ");
}

function rangeLineLabel(range: AspGraphRange | undefined): string | undefined {
  return range ? graphText("detail.line", { line: range.start.line + 1 }) : undefined;
}

function detailParts(...values: Array<string | undefined>): string[] {
  return values.filter((value): value is string => Boolean(value));
}

function PaneResizeHandle({
  maxWidth,
  minWidth,
  onWidthChange,
  position,
  width,
}: {
  maxWidth: number;
  minWidth: number;
  onWidthChange(width: number): void;
  position: InfoPanelPosition;
  width: number;
}): React.ReactElement {
  const updateWidth = useCallback(
    (nextWidth: number) => onWidthChange(clamp(nextWidth, minWidth, maxWidth)),
    [maxWidth, minWidth, onWidthChange],
  );
  const handlePointerDown = useCallback(
    (event: React.PointerEvent<HTMLDivElement>) => {
      if (event.button !== 0) {
        return;
      }
      event.preventDefault();
      const startX = event.clientX;
      const startWidth = width;
      const previousCursor = document.body.style.cursor;
      const previousUserSelect = document.body.style.userSelect;
      document.body.style.cursor = "col-resize";
      document.body.style.userSelect = "none";

      const handlePointerMove = (moveEvent: PointerEvent) => {
        moveEvent.preventDefault();
        const deltaX = moveEvent.clientX - startX;
        updateWidth(position === "left" ? startWidth + deltaX : startWidth - deltaX);
      };
      const stopResize = () => {
        document.body.style.cursor = previousCursor;
        document.body.style.userSelect = previousUserSelect;
        window.removeEventListener("pointermove", handlePointerMove);
        window.removeEventListener("pointerup", stopResize);
        window.removeEventListener("pointercancel", stopResize);
      };

      window.addEventListener("pointermove", handlePointerMove);
      window.addEventListener("pointerup", stopResize);
      window.addEventListener("pointercancel", stopResize);
    },
    [position, updateWidth, width],
  );
  const handleKeyDown = useCallback(
    (event: React.KeyboardEvent<HTMLDivElement>) => {
      if (event.key === "ArrowLeft") {
        event.preventDefault();
        updateWidth(
          width + (position === "left" ? -paneResizeKeyboardStep : paneResizeKeyboardStep),
        );
      } else if (event.key === "ArrowRight") {
        event.preventDefault();
        updateWidth(
          width + (position === "left" ? paneResizeKeyboardStep : -paneResizeKeyboardStep),
        );
      } else if (event.key === "Home") {
        event.preventDefault();
        updateWidth(minWidth);
      } else if (event.key === "End") {
        event.preventDefault();
        updateWidth(maxWidth);
      }
    },
    [maxWidth, minWidth, position, updateWidth, width],
  );

  return (
    <div
      role="separator"
      tabIndex={0}
      aria-label={graphText("view.resizeInspector")}
      aria-orientation="vertical"
      aria-valuemin={minWidth}
      aria-valuemax={maxWidth}
      aria-valuenow={width}
      title={graphText("view.resizeInspector")}
      className="relative order-2 cursor-col-resize bg-[#11151c] outline-none before:absolute before:inset-y-0 before:left-1/2 before:w-px before:-translate-x-1/2 before:bg-[#2b3442] hover:bg-[#1c2430] focus:bg-[#1c2430] focus:before:bg-[#89ddff] max-[780px]:hidden"
      onKeyDown={handleKeyDown}
      onPointerDown={handlePointerDown}
    />
  );
}

function Inspector({
  graphData,
  visibleGraphData,
  onSelectLink,
  onSelectNode,
  position,
  selection,
  onClose,
}: {
  graphData: GraphData;
  visibleGraphData: GraphData;
  onSelectLink(link: GraphLink): void;
  onSelectNode(node: GraphNode): void;
  position: InfoPanelPosition;
  selection: Selection;
  onClose(): void;
}): React.ReactElement {
  const borderClass = position === "left" ? "border-r" : "border-l";
  const orderClass = position === "left" ? "order-1" : "order-3";
  const className = selection
    ? `${orderClass} min-w-0 overflow-auto ${borderClass} border-[#2b3442] bg-[#171c25] p-3.5 max-[780px]:absolute max-[780px]:inset-x-2.5 max-[780px]:top-2.5 max-[780px]:z-10 max-[780px]:block max-[780px]:max-h-[min(260px,calc(100%_-_20px))] max-[780px]:rounded-md max-[780px]:border max-[780px]:shadow-[0_14px_34px_rgb(0_0_0_/_34%)]`
    : `${orderClass} min-w-0 overflow-auto ${borderClass} border-[#2b3442] bg-[#171c25] p-3.5 max-[780px]:hidden`;
  if (!selection) {
    return (
      <aside className={className}>
        <h2 className="asp-lsp-graph-inspector-title mb-3 text-sm leading-[1.35] font-semibold [overflow-wrap:anywhere]">
          {graphText("view.inspector")}
        </h2>
        <p className="m-0 text-xs text-[#8d98a8]">{graphText("inspector.selectPrompt")}</p>
      </aside>
    );
  }
  return (
    <aside className={className}>
      <div className="mb-3 flex min-w-0 items-start gap-2">
        <h2 className="asp-lsp-graph-inspector-title m-0 min-w-0 flex-1 text-sm leading-[1.35] font-semibold [overflow-wrap:anywhere]">
          {inspectorTitleForSelection(selection, graphData)}
        </h2>
        <button
          type="button"
          className="hidden h-7 w-7 shrink-0 rounded-md border border-[#405068] bg-[#202735] text-sm leading-none text-[#d7dde8] max-[780px]:inline-grid max-[780px]:place-items-center"
          aria-label={graphText("inspector.close")}
          title={graphText("inspector.close")}
          onClick={onClose}
        >
          x
        </button>
      </div>
      {selection.type === "node" ? (
        <NodeInspector
          graphData={graphData}
          node={selection.item}
          visibleGraphData={visibleGraphData}
          onSelectLink={onSelectLink}
        />
      ) : (
        <LinkInspector graphData={graphData} link={selection.item} onSelectNode={onSelectNode} />
      )}
    </aside>
  );
}

function inspectorTitleForSelection(selection: Selection, graphData: GraphData): string {
  if (!selection) {
    return graphText("view.inspector");
  }
  if (selection.type === "node") {
    return selection.item.label;
  }
  const nodesById = graphNodeMap(graphData.nodes);
  return `${linkInspectorTypeLabel(selection.item)}: ${endpointLabel(
    selection.item.source,
    nodesById,
  )} -> ${endpointLabel(selection.item.target, nodesById)}`;
}

function NodeInspector({
  graphData,
  node,
  onSelectLink,
  visibleGraphData,
}: {
  graphData: GraphData;
  node: GraphNode;
  onSelectLink(link: GraphLink): void;
  visibleGraphData: GraphData;
}): React.ReactElement {
  const location = node.uri ? { uri: node.uri, range: node.range } : undefined;
  const canOpenFlowchart = Boolean(location?.uri && node.exists !== false);
  return (
    <>
      <NodeDetails node={node} />
      <OpenFlowchartButton
        className="mb-3 h-[30px] w-full"
        disabled={!canOpenFlowchart}
        label={graphText("action.openFlowchart")}
        range={location?.range}
        uri={location?.uri}
      />
      {isFileLikeGraphNode(node) ? (
        <FileNodeRelations graphData={graphData} node={node} />
      ) : (
        <NodeSourceSections graphData={graphData} node={node} />
      )}
      <NodeLinkSections graphData={visibleGraphData} node={node} onSelectLink={onSelectLink} />
      {isFileLikeGraphNode(node) ? (
        <OpenLocationButton
          className="mt-3 h-[30px] w-full"
          disabled={!location?.uri}
          label={graphText("action.openFile")}
          range={location?.range}
          uri={location?.uri}
        />
      ) : null}
    </>
  );
}

function LinkInspector({
  graphData,
  link,
  onSelectNode,
}: {
  graphData: GraphData;
  link: GraphLink;
  onSelectNode(node: GraphNode): void;
}): React.ReactElement {
  const nodesById = useMemo(() => graphNodeMap(graphData.nodes), [graphData.nodes]);
  const sourceItems = useMemo(() => sourceItemsForLink(link), [link]);
  const sourceState = useSourceRanges(sourceItems);
  const location = link.ranges[0];
  return (
    <>
      <LinkDetails link={link} nodesById={nodesById} onSelectNode={onSelectNode} />
      <div className="mb-3 grid gap-2">
        <Accordion
          count={link.count}
          defaultOpen={true}
          hint={graphText("section.occurrencesHint")}
          headerAction={
            location ? (
              <div className="flex flex-wrap gap-1.5">
                <OpenLocationButton
                  className="h-7 px-2"
                  label={graphText("action.openFirst")}
                  range={location.range}
                  uri={location.uri}
                />
                <OpenFlowchartButton
                  className="h-7 px-2"
                  label={graphText("action.openFlowchart")}
                  range={location.range}
                  uri={location.uri}
                />
              </div>
            ) : undefined
          }
          title={graphText("section.occurrences")}
        >
          {sourceItems.length > 0 ? (
            <SourceFileGroups items={sourceItems} sourceState={sourceState} />
          ) : (
            <EmptyInspectorText>{graphText("empty.sourceRanges")}</EmptyInspectorText>
          )}
        </Accordion>
      </div>
    </>
  );
}

interface GraphSourceItem {
  id: string;
  uri: string;
  displayPath?: string;
  range: AspGraphRange;
  highlightRange: AspGraphRange;
  kind: AspGraphSourceRangeRequestItem["kind"];
  title: string;
  detail?: string;
}

interface GraphSourceState {
  loading: boolean;
  byId: Map<string, AspGraphSourceRangeResponseItem>;
}

interface IncludeRelation {
  id: string;
  title: string;
  detail: string;
  fileUri?: string;
  fileRange?: AspGraphRange;
  fileLabel: string;
  directiveUri?: string;
  directiveRange?: AspGraphRange;
  directiveLabel: string;
  exists?: boolean;
}

function NodeLinkSections({
  graphData,
  node,
  onSelectLink,
}: {
  graphData: GraphData;
  node: GraphNode;
  onSelectLink(link: GraphLink): void;
}): React.ReactElement {
  const { incoming, outgoing } = useMemo(
    () => nodeLinksFor(node, graphData.links),
    [graphData.links, node],
  );
  const nodesById = useMemo(() => graphNodeMap(graphData.nodes), [graphData.nodes]);
  return (
    <div className="mb-3 grid gap-2">
      <Accordion
        count={outgoing.length}
        defaultOpen={outgoing.length > 0}
        hint={graphText("section.outgoingLinksHint")}
        title={graphText("section.outgoingLinks")}
      >
        {outgoing.length > 0 ? (
          <NodeLinkList
            direction="outgoing"
            links={outgoing}
            nodesById={nodesById}
            onSelectLink={onSelectLink}
          />
        ) : (
          <EmptyInspectorText>{graphText("empty.outgoingLinks")}</EmptyInspectorText>
        )}
      </Accordion>
      <Accordion
        count={incoming.length}
        defaultOpen={incoming.length > 0}
        hint={graphText("section.incomingLinksHint")}
        title={graphText("section.incomingLinks")}
      >
        {incoming.length > 0 ? (
          <NodeLinkList
            direction="incoming"
            links={incoming}
            nodesById={nodesById}
            onSelectLink={onSelectLink}
          />
        ) : (
          <EmptyInspectorText>{graphText("empty.incomingLinks")}</EmptyInspectorText>
        )}
      </Accordion>
    </div>
  );
}

function NodeLinkList({
  direction,
  links,
  nodesById,
  onSelectLink,
}: {
  direction: "incoming" | "outgoing";
  links: GraphLink[];
  nodesById: ReadonlyMap<string, GraphNode>;
  onSelectLink(link: GraphLink): void;
}): React.ReactElement {
  return (
    <div className="grid gap-2">
      {links.map((link) => (
        <button
          key={link.id}
          type="button"
          className="grid cursor-pointer gap-1 rounded-md border border-[#303a49] bg-[#11151c] p-2 text-left hover:border-[#4b5a70]"
          title={detailParts(
            linkInspectorTypeLabel(link),
            `${endpointLabel(link.source, nodesById)} -> ${endpointLabel(link.target, nodesById)}`,
            nodeLinkDetail(link),
          ).join(" · ")}
          onClick={() => onSelectLink(link)}
        >
          <div className="flex min-w-0 items-center gap-2">
            <span
              className="h-2.5 w-2.5 shrink-0 rounded-full"
              style={{ backgroundColor: link.color }}
              aria-hidden="true"
            />
            <span className="min-w-0 flex-1 overflow-hidden text-ellipsis whitespace-nowrap text-xs font-semibold text-[#d7dde8]">
              {linkInspectorTypeLabel(link)}
            </span>
            <span className="shrink-0 rounded border border-[#405068] bg-[#151a22] px-1.5 py-0.5 text-[10px] leading-none text-[#9aa7b8]">
              {link.count}
            </span>
          </div>
          <div
            className="overflow-hidden text-ellipsis whitespace-nowrap text-[11px] text-[#8d98a8]"
            title={`${endpointLabel(link.source, nodesById)} -> ${endpointLabel(
              link.target,
              nodesById,
            )}`}
          >
            {direction === "outgoing" ? graphText("direction.to") : graphText("direction.from")}{" "}
            {endpointLabel(direction === "outgoing" ? link.target : link.source, nodesById)}
          </div>
          {nodeLinkDetail(link) ? (
            <div className="text-[11px] leading-[1.35] text-[#8d98a8] [overflow-wrap:anywhere]">
              {nodeLinkDetail(link)}
            </div>
          ) : null}
        </button>
      ))}
    </div>
  );
}

function NodeSourceSections({
  graphData,
  node,
}: {
  graphData: GraphData;
  node: GraphNode;
}): React.ReactElement {
  const { declarationItems, usageItems } = useMemo(
    () => sourceItemsForNode(node, graphData),
    [graphData, node],
  );
  const sourceItems = useMemo(
    () => [...declarationItems, ...usageItems],
    [declarationItems, usageItems],
  );
  const sourceState = useSourceRanges(sourceItems);
  const declarationItem = declarationItems[0];
  return (
    <div className="mb-3 grid gap-2">
      <Accordion
        defaultOpen={true}
        hint={graphText("section.declarationHint")}
        headerAction={
          declarationItem ? (
            <OpenLocationButton
              className="h-7 px-2"
              label={graphText("action.open")}
              range={declarationItem.highlightRange}
              uri={declarationItem.uri}
            />
          ) : undefined
        }
        title={graphText("section.declaration")}
      >
        {declarationItem ? (
          <DeclarationSource item={declarationItem} sourceState={sourceState} />
        ) : (
          <EmptyInspectorText>{graphText("empty.declarationSource")}</EmptyInspectorText>
        )}
      </Accordion>
      <Accordion
        count={usageItems.length}
        defaultOpen={true}
        hint={graphText("section.referencesCallsHint")}
        title={graphText("section.referencesCalls")}
      >
        {usageItems.length > 0 ? (
          <SourceFileGroups items={usageItems} sourceState={sourceState} />
        ) : (
          <EmptyInspectorText>{graphText("empty.referencesOrCalls")}</EmptyInspectorText>
        )}
      </Accordion>
    </div>
  );
}

function FileNodeRelations({
  graphData,
  node,
}: {
  graphData: GraphData;
  node: GraphNode;
}): React.ReactElement {
  const { incoming, outgoing } = useMemo(
    () => includeRelationsForFileNode(node, graphData),
    [graphData, node],
  );
  return (
    <div className="mb-3 grid gap-2">
      <Accordion
        count={outgoing.length}
        defaultOpen={true}
        hint={graphText("section.includesHint")}
        title={graphText("section.includes")}
      >
        {outgoing.length > 0 ? (
          <IncludeRelationList relations={outgoing} />
        ) : (
          <EmptyInspectorText>{graphText("empty.includedFiles")}</EmptyInspectorText>
        )}
      </Accordion>
      <Accordion
        count={incoming.length}
        defaultOpen={incoming.length > 0}
        hint={graphText("section.includedByHint")}
        title={graphText("section.includedBy")}
      >
        {incoming.length > 0 ? (
          <IncludeRelationList relations={incoming} />
        ) : (
          <EmptyInspectorText>{graphText("empty.includeSources")}</EmptyInspectorText>
        )}
      </Accordion>
    </div>
  );
}

function Accordion({
  children,
  count,
  defaultOpen,
  hint,
  headerAction,
  title,
}: {
  children: React.ReactNode;
  count?: number;
  defaultOpen: boolean;
  hint?: string;
  headerAction?: React.ReactNode;
  title: string;
}): React.ReactElement {
  const [isOpen, setOpen] = useState(defaultOpen);
  const headerTitle = detailParts(title, hint).join(" · ");
  return (
    <section className="overflow-hidden rounded-md border border-[#303a49] bg-[#151a22]">
      <div className="flex min-h-9 items-center gap-2">
        <div
          role="button"
          tabIndex={0}
          className="flex min-h-9 min-w-0 flex-1 cursor-pointer items-center justify-between gap-2 px-2.5 py-1.5 text-left outline-none focus-visible:ring-1 focus-visible:ring-[#89ddff]"
          aria-expanded={isOpen}
          title={headerTitle}
          onClick={() => setOpen((current) => !current)}
          onKeyDown={(event) => {
            if (event.key === "Enter" || event.key === " ") {
              event.preventDefault();
              setOpen((current) => !current);
            }
          }}
        >
          <span className="flex min-w-0 items-center gap-1.5">
            <span className="min-w-0 overflow-hidden text-ellipsis whitespace-nowrap text-xs font-semibold text-[#d7dde8]">
              {title}
            </span>
            {hint ? (
              <span
                className="shrink-0"
                onClick={(event) => event.stopPropagation()}
                onKeyDown={(event) => event.stopPropagation()}
              >
                <DetailHint hint={hint} label={title} />
              </span>
            ) : null}
          </span>
          <span className="inline-flex shrink-0 items-center gap-2 text-[11px] text-[#8d98a8]">
            {count !== undefined ? <span>{count}</span> : null}
            <span aria-hidden="true">{isOpen ? "-" : "+"}</span>
          </span>
        </div>
        {headerAction ? <div className="shrink-0 pr-2">{headerAction}</div> : null}
      </div>
      {isOpen ? <div className="grid gap-2 border-t border-[#303a49] p-2.5">{children}</div> : null}
    </section>
  );
}

function DeclarationSource({
  item,
  sourceState,
}: {
  item: GraphSourceItem;
  sourceState: GraphSourceState;
}): React.ReactElement {
  const source = sourceState.byId.get(item.id);
  const displayRange = source?.range ?? item.range;
  const loading = sourceState.loading && !source;
  return (
    <div className="grid gap-2">
      <div className="overflow-hidden text-ellipsis whitespace-nowrap text-[11px] text-[#8d98a8]">
        {item.detail ?? graphText("detail.line", { line: displayRange.start.line + 1 })}
      </div>
      {source?.error ? (
        <p className="m-0 text-[11px] text-[#ff9cac]">{source.error}</p>
      ) : (
        <SourceCodeSnippet className="max-h-80" item={item} loading={loading} source={source} />
      )}
    </div>
  );
}

function SourceFileGroups({
  items,
  sourceState,
}: {
  items: GraphSourceItem[];
  sourceState: GraphSourceState;
}): React.ReactElement {
  const groups = groupedSourceItems(items);
  return (
    <div className="grid gap-2">
      {groups.map((group) => (
        <Accordion
          key={group.uri}
          count={group.items.length}
          defaultOpen={groups.length === 1}
          hint={graphText("section.sourceFileHint")}
          title={sourceGroupTitle(group.uri, group.items, sourceState.byId)}
        >
          <SourceRangeList items={group.items} sourceState={sourceState} />
        </Accordion>
      ))}
    </div>
  );
}

function SourceRangeList({
  items,
  sourceState,
}: {
  items: GraphSourceItem[];
  sourceState: GraphSourceState;
}): React.ReactElement {
  return (
    <div className="grid gap-2">
      {items.map((item) => (
        <SourceRangeCard
          key={item.id}
          item={item}
          loading={sourceState.loading && !sourceState.byId.has(item.id)}
          source={sourceState.byId.get(item.id)}
        />
      ))}
    </div>
  );
}

function SourceRangeCard({
  item,
  loading,
  source,
}: {
  item: GraphSourceItem;
  loading: boolean;
  source: AspGraphSourceRangeResponseItem | undefined;
}): React.ReactElement {
  const displayRange = source?.range ?? item.range;
  return (
    <article className="grid gap-2 rounded-md border border-[#303a49] bg-[#11151c] p-2">
      <div className="flex min-w-0 items-center gap-2">
        <div className="min-w-0 flex-1">
          <div className="overflow-hidden text-ellipsis whitespace-nowrap text-xs font-semibold text-[#d7dde8]">
            {item.title}
          </div>
          <div className="overflow-hidden text-ellipsis whitespace-nowrap text-[11px] text-[#8d98a8]">
            {item.detail ?? graphText("detail.line", { line: displayRange.start.line + 1 })}
          </div>
        </div>
        <div className="flex shrink-0 flex-wrap gap-1.5">
          <OpenLocationButton
            className="h-7 px-2"
            label={graphText("action.open")}
            range={item.highlightRange}
            uri={item.uri}
          />
          <OpenFlowchartButton
            className="h-7 px-2"
            label={graphText("action.openFlowchart")}
            range={item.highlightRange}
            uri={item.uri}
          />
        </div>
      </div>
      {source?.error ? (
        <p className="m-0 text-[11px] text-[#ff9cac]">{source.error}</p>
      ) : (
        <SourceCodeSnippet className="max-h-44" item={item} loading={loading} source={source} />
      )}
    </article>
  );
}

function SourceCodeSnippet({
  className,
  item,
  loading,
  source,
}: {
  className: string;
  item: GraphSourceItem;
  loading: boolean;
  source: AspGraphSourceRangeResponseItem | undefined;
}): React.ReactElement {
  const text =
    source?.text ??
    (loading ? graphText("snippet.loadingSource") : graphText("empty.sourceUnavailable"));
  const highlightState = useSnippetHighlightState(Boolean(source?.text));
  const highlightOffsets = sourceHighlightOffsets(source);
  const language = snippetLanguageForSourceItem(item);
  return (
    <pre
      className={`m-0 overflow-auto rounded border border-[#253041] bg-[#0d1117] p-2 font-mono text-[11px] leading-[1.45] whitespace-pre-wrap text-[#d7dde8] [tab-size:2] ${className}`}
    >
      {source?.text && highlightState.status === "ready" ? (
        <HighlightedSourceText
          highlightOffsets={highlightOffsets}
          highlighter={highlightState.highlighter}
          language={language}
          text={source.text}
        />
      ) : (
        <PlainSourceText highlightOffsets={highlightOffsets} text={text} />
      )}
    </pre>
  );
}

function sourceHighlightOffsets(
  source: AspGraphSourceRangeResponseItem | undefined,
): HighlightOffsets | undefined {
  if (!source?.text || !source.range || !source.highlightRange) {
    return undefined;
  }
  const start = positionOffsetInRangeText(source.text, source.range, source.highlightRange.start);
  const end = positionOffsetInRangeText(source.text, source.range, source.highlightRange.end);
  if (start === undefined || end === undefined || end <= start) {
    return undefined;
  }
  return { start, end };
}

function PlainSourceText({
  highlightOffsets,
  text,
}: {
  highlightOffsets: HighlightOffsets | undefined;
  text: string;
}): React.ReactElement {
  return <>{renderCodeSegment(text, "plain", 0, highlightOffsets)}</>;
}

function HighlightedSourceText({
  highlightOffsets,
  highlighter,
  language,
  text,
}: {
  highlightOffsets: HighlightOffsets | undefined;
  highlighter: SnippetHighlighter;
  language: SnippetLanguage;
  text: string;
}): React.ReactElement {
  const grammar = language === "classic-asp" ? highlighter.classicAsp : highlighter.vbscript;
  const lines = text.split("\n");
  const children: React.ReactNode[] = [];
  let state: StateStack | null = INITIAL;
  let lineOffset = 0;
  for (let index = 0; index < lines.length; index += 1) {
    const line = lines[index] ?? "";
    const result = grammar.tokenizeLine(line, state);
    state = result.ruleStack;
    children.push(
      <React.Fragment key={`line:${index}`}>
        {renderTokenizedLine(line, result.tokens, lineOffset, highlightOffsets)}
        {index < lines.length - 1 ? "\n" : null}
      </React.Fragment>,
    );
    lineOffset += line.length + (index < lines.length - 1 ? 1 : 0);
  }
  return <>{children}</>;
}

function renderTokenizedLine(
  line: string,
  tokens: Array<{ startIndex: number; endIndex: number; scopes: string[] }>,
  lineOffset: number,
  highlightOffsets: HighlightOffsets | undefined,
): React.ReactNode[] {
  const parts: React.ReactNode[] = [];
  let cursor = 0;
  for (let index = 0; index < tokens.length; index += 1) {
    const token = tokens[index];
    const start = clamp(token.startIndex, 0, line.length);
    const end = clamp(token.endIndex, start, line.length);
    if (start > cursor) {
      parts.push(
        ...renderCodeSegment(
          line.slice(cursor, start),
          `gap:${index}:${cursor}`,
          lineOffset + cursor,
          highlightOffsets,
        ),
      );
    }
    if (end > start) {
      parts.push(
        ...renderCodeSegment(
          line.slice(start, end),
          `token:${index}:${start}`,
          lineOffset + start,
          highlightOffsets,
          tokenStyleForScopes(token.scopes),
        ),
      );
    }
    cursor = end;
  }
  if (cursor < line.length) {
    parts.push(
      ...renderCodeSegment(
        line.slice(cursor),
        `tail:${cursor}`,
        lineOffset + cursor,
        highlightOffsets,
      ),
    );
  }
  return parts.length > 0 ? parts : [""];
}

function renderCodeSegment(
  text: string,
  key: string,
  offset: number,
  highlightOffsets: HighlightOffsets | undefined,
  style?: React.CSSProperties,
): React.ReactNode[] {
  if (!text) {
    return [];
  }
  if (!highlightOffsets) {
    return [styledSourceSpan(key, text, style)];
  }
  const highlightStart = clamp(highlightOffsets.start - offset, 0, text.length);
  const highlightEnd = clamp(highlightOffsets.end - offset, 0, text.length);
  if (highlightEnd <= 0 || highlightStart >= text.length || highlightEnd <= highlightStart) {
    return [styledSourceSpan(key, text, style)];
  }
  const parts: React.ReactNode[] = [];
  if (highlightStart > 0) {
    parts.push(styledSourceSpan(`${key}:before`, text.slice(0, highlightStart), style));
  }
  parts.push(
    <mark key={`${key}:highlight`} className={sourceHighlightMarkClassName} style={style}>
      {text.slice(highlightStart, highlightEnd)}
    </mark>,
  );
  if (highlightEnd < text.length) {
    parts.push(styledSourceSpan(`${key}:after`, text.slice(highlightEnd), style));
  }
  return parts;
}

function styledSourceSpan(
  key: string,
  text: string,
  style: React.CSSProperties | undefined,
): React.ReactElement {
  return (
    <span key={key} style={style}>
      {text}
    </span>
  );
}

function tokenStyleForScopes(scopes: string[]): React.CSSProperties | undefined {
  if (scopes.some((scope) => scope.includes("comment"))) {
    return { color: "#6a9955", fontStyle: "italic" };
  }
  if (scopes.some((scope) => scope.includes("string"))) {
    return { color: "#c3e88d" };
  }
  if (scopes.some((scope) => scope.includes("constant.numeric"))) {
    return { color: "#f78c6c" };
  }
  if (scopes.some((scope) => scope.includes("keyword") || scope.includes("storage"))) {
    return { color: "#c792ea", fontWeight: 600 };
  }
  if (scopes.some((scope) => scope.includes("entity.name.tag"))) {
    return { color: "#ff5370", fontWeight: 600 };
  }
  if (scopes.some((scope) => scope.includes("meta.tag") || scope.includes("punctuation"))) {
    return { color: "#89ddff" };
  }
  if (
    scopes.some(
      (scope) =>
        scope.includes("entity.name") ||
        scope.includes("support.function") ||
        scope.includes("support.class"),
    )
  ) {
    return { color: "#82aaff" };
  }
  if (scopes.some((scope) => scope.includes("variable.parameter"))) {
    return { color: "#b2ccd6" };
  }
  if (scopes.some((scope) => scope.includes("variable") || scope.includes("support"))) {
    return { color: "#dcdcaa" };
  }
  return undefined;
}

function useSnippetHighlightState(enabled: boolean): SnippetHighlightState {
  const [state, setState] = useState<SnippetHighlightState>({ status: "loading" });
  useEffect(() => {
    if (!enabled) {
      return undefined;
    }
    let cancelled = false;
    setState({ status: "loading" });
    void loadSnippetHighlighter()
      .then((highlighter) => {
        if (!cancelled) {
          setState({ status: "ready", highlighter });
        }
      })
      .catch(() => {
        if (!cancelled) {
          setState({ status: "failed" });
        }
      });
    return () => {
      cancelled = true;
    };
  }, [enabled]);
  return enabled ? state : { status: "failed" };
}

function snippetLanguageForSourceItem(item: GraphSourceItem): SnippetLanguage {
  return item.kind === "include" ? "classic-asp" : "vbscript";
}

let snippetHighlighterPromise: Promise<SnippetHighlighter> | undefined;

function loadSnippetHighlighter(): Promise<SnippetHighlighter> {
  snippetHighlighterPromise ??= createSnippetHighlighter();
  return snippetHighlighterPromise;
}

async function createSnippetHighlighter(): Promise<SnippetHighlighter> {
  const onigLib = (async (): Promise<IOnigLib> => {
    await loadWASM(await fetch(onigWasmUrl));
    return {
      createOnigScanner,
      createOnigString,
    };
  })();
  const rawGrammars = new Map<string, IRawGrammar>([
    [
      "text.html.classic-asp",
      rawTextMateGrammar(classicAspGrammarJson, "classic-asp.tmLanguage.json"),
    ],
    [
      "classic-asp.tag-injection",
      rawTextMateGrammar(
        classicAspTagInjectionGrammarJson,
        "classic-asp-tag-injection.tmLanguage.json",
      ),
    ],
    ["source.vbscript", rawTextMateGrammar(vbscriptGrammarJson, "vbscript.tmLanguage.json")],
    ["text.html.basic", rawTextMateGrammar(minimalHtmlGrammar(), "html.json")],
    ["source.css", rawTextMateGrammar(minimalCssGrammar(), "css.json")],
    ["source.js", rawTextMateGrammar(minimalJavaScriptGrammar(), "javascript.json")],
  ]);
  const registry = new Registry({
    onigLib,
    loadGrammar: async (scopeName) => rawGrammars.get(scopeName) ?? null,
    getInjections: (scopeName) =>
      scopeName === "text.html.classic-asp" ? ["classic-asp.tag-injection"] : undefined,
  });
  const [classicAsp, vbscript] = await Promise.all([
    registry.loadGrammar("text.html.classic-asp"),
    registry.loadGrammar("source.vbscript"),
  ]);
  if (!classicAsp || !vbscript) {
    throw new Error("Failed to load graph snippet TextMate grammars.");
  }
  return { classicAsp, vbscript };
}

function rawTextMateGrammar(grammar: unknown, fileName: string): IRawGrammar {
  return parseRawGrammar(JSON.stringify(grammar), fileName);
}

function minimalHtmlGrammar(): unknown {
  return {
    scopeName: "text.html.basic",
    patterns: [
      { include: "#style" },
      { include: "#script" },
      { include: "#tag" },
      { match: "[^<]+" },
    ],
    repository: {
      tag: {
        begin: "<[A-Za-z][A-Za-z0-9:-]*\\b",
        end: ">",
        name: "meta.tag.html",
        patterns: [
          {
            begin: '"',
            end: '"',
            name: "string.quoted.double.html",
          },
          {
            begin: "'",
            end: "'",
            name: "string.quoted.single.html",
          },
        ],
      },
      style: {
        begin: "<style\\b[^>]*>",
        end: "</style>",
        contentName: "source.css",
        name: "meta.embedded.block.css.html",
        patterns: [{ include: "source.css" }],
      },
      script: {
        begin: "<script\\b[^>]*>",
        end: "</script>",
        contentName: "source.js",
        name: "meta.embedded.block.javascript.html",
        patterns: [{ include: "source.js" }],
      },
    },
  };
}

function minimalCssGrammar(): unknown {
  return {
    scopeName: "source.css",
    name: "source.css",
    patterns: [
      {
        begin: "/\\*",
        end: "\\*/",
        name: "comment.block.css",
        patterns: aspIslandPatterns(),
      },
      { match: "\\.[A-Za-z_][A-Za-z0-9_-]*", name: "entity.other.attribute-name.class.css" },
      { match: "-?[A-Za-z_][A-Za-z0-9_-]*(?=\\s*:)", name: "support.type.property-name.css" },
    ],
  };
}

function minimalJavaScriptGrammar(): unknown {
  return {
    scopeName: "source.js",
    name: "source.js",
    patterns: [
      {
        begin: "//",
        end: "$",
        name: "comment.line.double-slash.js",
        patterns: aspIslandPatterns(),
      },
      {
        begin: "/\\*",
        end: "\\*/",
        name: "comment.block.js",
        patterns: aspIslandPatterns(),
      },
      { match: "\\bconst\\b", name: "storage.modifier.js" },
      { match: "[A-Za-z_$][A-Za-z0-9_$]*", name: "variable.other.js" },
    ],
  };
}

function aspIslandPatterns(): unknown[] {
  return [
    { include: "text.html.classic-asp#asp-expression" },
    { include: "text.html.classic-asp#asp-directive" },
    { include: "text.html.classic-asp#asp-block" },
  ];
}

function positionOffsetInRangeText(
  text: string,
  range: AspGraphRange,
  position: AspGraphRange["start"],
): number | undefined {
  if (
    position.line < range.start.line ||
    position.line > range.end.line ||
    (position.line === range.start.line && position.character < range.start.character)
  ) {
    return undefined;
  }
  let offset = 0;
  for (let line = range.start.line; line < position.line; line += 1) {
    const newlineOffset = text.indexOf("\n", offset);
    if (newlineOffset === -1) {
      return undefined;
    }
    offset = newlineOffset + 1;
  }
  const lineStartCharacter = position.line === range.start.line ? range.start.character : 0;
  return clamp(offset + position.character - lineStartCharacter, 0, text.length);
}

function IncludeRelationList({ relations }: { relations: IncludeRelation[] }): React.ReactElement {
  return (
    <div className="grid gap-2">
      {relations.map((relation) => (
        <article
          key={relation.id}
          className="grid gap-2 rounded-md border border-[#303a49] bg-[#11151c] p-2"
        >
          <div className="min-w-0">
            <div className="overflow-hidden text-ellipsis whitespace-nowrap text-xs font-semibold text-[#d7dde8]">
              {relation.title}
            </div>
            <div className="overflow-hidden text-ellipsis whitespace-nowrap text-[11px] text-[#8d98a8]">
              {relation.detail}
            </div>
          </div>
          <div className="flex flex-wrap gap-2">
            <OpenLocationButton
              className="h-7 px-2"
              disabled={relation.exists === false || !relation.fileUri}
              label={relation.fileLabel}
              range={relation.fileRange}
              uri={relation.fileUri}
            />
            <OpenFlowchartButton
              className="h-7 px-2"
              disabled={relation.exists === false || !relation.fileUri}
              label={graphText("action.openFlowchart")}
              range={relation.fileRange}
              uri={relation.fileUri}
            />
            <OpenLocationButton
              className="h-7 px-2"
              disabled={!relation.directiveUri}
              label={relation.directiveLabel}
              range={relation.directiveRange}
              uri={relation.directiveUri}
            />
          </div>
        </article>
      ))}
    </div>
  );
}

function EmptyInspectorText({ children }: { children: React.ReactNode }): React.ReactElement {
  return <p className="m-0 text-xs text-[#8d98a8]">{children}</p>;
}

function OpenLocationButton({
  className,
  disabled,
  label,
  range,
  uri,
}: {
  className?: string;
  disabled?: boolean;
  label: string;
  range?: AspGraphRange;
  uri?: string;
}): React.ReactElement {
  return (
    <button
      type="button"
      className={`cursor-pointer rounded-md border border-[#405068] bg-[#c3e88d] text-xs text-[#11151c] disabled:cursor-not-allowed disabled:bg-[#202735] disabled:text-[#717b8c] ${className ?? ""}`}
      disabled={disabled || !uri}
      title={label}
      onClick={(event) => {
        event.stopPropagation();
        if (uri) {
          vscode.postMessage({ type: "openRange", uri, range });
        }
      }}
    >
      {label}
    </button>
  );
}

function OpenFlowchartButton({
  className,
  disabled,
  label,
  range,
  uri,
}: {
  className?: string;
  disabled?: boolean;
  label: string;
  range?: AspGraphRange;
  uri?: string;
}): React.ReactElement {
  return (
    <button
      type="button"
      className={`cursor-pointer rounded-md border border-[#405068] bg-[#89ddff] text-xs text-[#11151c] disabled:cursor-not-allowed disabled:bg-[#202735] disabled:text-[#717b8c] ${className ?? ""}`}
      disabled={disabled || !uri}
      title={label}
      onClick={(event) => {
        event.stopPropagation();
        if (uri) {
          const message: OpenFlowchartMessage = { type: "openFlowchart", uri, range };
          vscode.postMessage(message);
        }
      }}
    >
      {label}
    </button>
  );
}

function NodeDetails({ node }: { node: GraphNode }): React.ReactElement {
  return (
    <dl className="mb-3.5 grid grid-cols-[82px_minmax(0,1fr)] gap-x-2.5 gap-y-2">
      <Detail
        hint={graphText("detail.nodeTypeHint")}
        label={graphText("detail.type")}
        value={nodeTypeLabel(node)}
      />
      <Detail
        hint={graphText("detail.nodeReferencesHint")}
        label={graphText("detail.references")}
        value={String(node.referenceCount)}
      />
      <Detail
        hint={graphText("detail.fileHint")}
        label={graphText("detail.file")}
        value={nodeFileLabel(node)}
      />
      <Detail
        hint={graphText("detail.memberOfHint")}
        label={graphText("detail.memberOf")}
        value={node.memberOf}
      />
      <Detail
        hint={graphText("detail.scopeHint")}
        label={graphText("detail.scope")}
        value={nodeScopeLabel(node)}
      />
      <Detail
        hint={graphText("detail.nodeStatusHint")}
        label={graphText("detail.status")}
        value={nodeStatusLabel(node)}
      />
    </dl>
  );
}

function nodeTypeLabel(node: GraphNode): string {
  if (node.kind === "missingInclude") {
    return graphText("label.missingInclude");
  }
  if (node.kind === "file") {
    return node.exists === false ? graphText("label.missingFile") : graphText("label.file");
  }
  if (node.kind === "vbUnresolved") {
    if (node.role === "member") {
      return graphText("label.unresolvedMember");
    }
    if (isCallableUnresolvedRole(node.role)) {
      return graphText("label.unresolvedFunction");
    }
    return graphText("label.unresolved");
  }
  if (node.kind === "vbMemberReference") {
    return graphText("label.member");
  }
  switch (node.declarationKind) {
    case "function":
      return graphText("label.function");
    case "sub":
      return graphText("label.sub");
    case "class":
      return graphText("label.class");
    case "method":
      if (node.procedureKind === "function") {
        return graphText("label.functionMethod");
      }
      if (node.procedureKind === "sub") {
        return graphText("label.subMethod");
      }
      return graphText("label.classMethod");
    case "property":
      return graphText("label.property");
    case "field":
      return graphText("label.field");
    case "parameter":
      return graphText("label.parameter");
    case "variable":
      if (node.unresolvedGlobal === true) {
        return graphText("label.unresolvedGlobalVariable");
      }
      if (node.implicitLocal === true) {
        return graphText("label.implicitLocalVariable");
      }
      return node.bindingScope === "local"
        ? graphText("label.localVariable")
        : graphText("label.globalVariable");
    case "constant":
      if (node.memberOf) {
        return graphText("label.classConstant");
      }
      return node.bindingScope === "local"
        ? graphText("label.localConstant")
        : graphText("label.globalConstant");
    case "object":
      return graphText("label.object");
    case "event":
      return graphText("label.event");
    default:
      return node.externalKind
        ? externalKindLabel(node.externalKind)
        : nodeCategoryLabels[node.category];
  }
}

function nodeFileLabel(node: GraphNode): string | undefined {
  return node.displayPath ?? node.fileName ?? pathForDisplay(node.uri);
}

function nodeScopeLabel(node: GraphNode): string | undefined {
  switch (node.bindingScope) {
    case "global":
      return graphText("label.global");
    case "local":
      return graphText("label.local");
    case "unknown":
      return graphText("label.unknown");
    default:
      return undefined;
  }
}

function nodeStatusLabel(node: GraphNode): string | undefined {
  if (node.kind === "missingInclude") {
    return graphText("label.missing");
  }
  if (node.kind === "file") {
    return node.isRoot ? graphText("label.root") : undefined;
  }
  if (node.origin === "builtin") {
    return graphText("label.builtin");
  }
  if (node.origin === "configured") {
    return graphText("label.configured");
  }
  return undefined;
}

function pathForDisplay(value: string | undefined): string | undefined {
  if (!value) {
    return undefined;
  }
  let normalized = value;
  if (normalized.startsWith("file://")) {
    try {
      const url = new URL(normalized);
      normalized =
        url.protocol === "file:" ? `${url.host ? `//${url.host}` : ""}${url.pathname}` : normalized;
    } catch {
      normalized = normalized.replace(/^file:\/\//, "");
    }
  }
  return safeDecodeURIComponent(normalized);
}

function safeDecodeURIComponent(value: string): string {
  try {
    return decodeURIComponent(value);
  } catch {
    return value;
  }
}

function externalKindLabel(value: NonNullable<GraphNode["externalKind"]>): string {
  switch (value) {
    case "function":
      return graphText("label.function");
    case "constant":
      return graphText("label.globalConstant");
    case "member":
      return graphText("label.member");
    case "object":
      return graphText("label.object");
    case "event":
      return graphText("label.event");
  }
}

function graphRoleLabel(role: string | undefined): string | undefined {
  switch (role) {
    case "member":
      return graphText("label.member");
    default:
      return role;
  }
}

function includeModeLabel(
  mode: NonNullable<AspGraphLink["include"]>["mode"] | undefined,
): string | undefined {
  switch (mode) {
    case "file":
      return graphText("label.file");
    case "virtual":
      return graphText("label.virtual");
    default:
      return undefined;
  }
}

function booleanLabel(value: boolean | undefined): string | undefined {
  return value === undefined ? undefined : graphText(value ? "label.yes" : "label.no");
}

function LinkDetails({
  link,
  nodesById,
  onSelectNode,
}: {
  link: GraphLink;
  nodesById: ReadonlyMap<string, GraphNode>;
  onSelectNode(node: GraphNode): void;
}): React.ReactElement {
  const typeLabel = linkInspectorTypeLabel(link);
  const sourceNode = nodesById.get(nodeIdForEndpoint(link.source));
  const targetNode = nodesById.get(nodeIdForEndpoint(link.target));
  const sourceLabel = endpointLabel(link.source, nodesById);
  const targetLabel = endpointLabel(link.target, nodesById);
  return (
    <div className="mb-3.5 grid gap-3">
      <section className="rounded-md border border-[#303a49] bg-[#11151c] p-3">
        <div className="flex min-w-0 items-center gap-3">
          <span
            className="h-3 w-3 shrink-0 rounded-full"
            style={{ backgroundColor: link.color }}
            aria-hidden="true"
          />
          <div className="min-w-0 flex-1">
            <div className="overflow-hidden text-ellipsis whitespace-nowrap text-xs font-semibold text-[#d7dde8]">
              {typeLabel}
            </div>
            <div
              className="text-[11px] leading-[1.35] text-[#8d98a8] [overflow-wrap:anywhere]"
              title={`${sourceLabel} -> ${targetLabel}`}
            >
              {sourceLabel} -&gt; {targetLabel}
            </div>
            <div className="mt-2 flex flex-wrap gap-2">
              <SelectEndpointButton
                label={graphText("action.selectSource")}
                node={sourceNode}
                onSelectNode={onSelectNode}
              />
              <SelectEndpointButton
                label={graphText("action.selectTarget")}
                node={targetNode}
                onSelectNode={onSelectNode}
              />
            </div>
          </div>
          <div
            className="grid min-w-[58px] shrink-0 justify-items-end rounded border border-[#405068] bg-[#151a22] px-2 py-1"
            title={graphText("detail.countHint")}
          >
            <span className="text-sm leading-none font-semibold text-[#f4f7fb]">{link.count}</span>
            <span className="inline-flex items-center gap-1 text-[10px] leading-[1.2] text-[#9aa7b8]">
              {graphText("detail.count")}
              <DetailHint hint={graphText("detail.countHint")} label={graphText("detail.count")} />
            </span>
          </div>
        </div>
      </section>
      <dl className="grid grid-cols-[86px_minmax(0,1fr)] gap-x-2.5 gap-y-2">
        <Detail
          hint={graphText("detail.linkTypeHint")}
          label={graphText("detail.type")}
          value={typeLabel}
        />
        <Detail
          hint={graphText("detail.sourceHint")}
          label={graphText("detail.source")}
          value={sourceLabel}
        />
        <Detail
          hint={graphText("detail.targetHint")}
          label={graphText("detail.target")}
          value={targetLabel}
        />
        <Detail
          hint={graphText("detail.roleHint")}
          label={graphText("detail.role")}
          value={graphRoleLabel(link.role)}
        />
        <Detail
          hint={graphText("detail.labelHint")}
          label={graphText("detail.label")}
          value={link.label !== typeLabel ? link.label : undefined}
        />
        <Detail
          hint={graphText("detail.includeHint")}
          label={graphText("detail.include")}
          value={link.include?.path}
        />
        <Detail
          hint={graphText("detail.modeHint")}
          label={graphText("detail.mode")}
          value={includeModeLabel(link.include?.mode)}
        />
        <Detail
          hint={graphText("detail.existsHint")}
          label={graphText("detail.exists")}
          value={link.include ? booleanLabel(link.include.exists) : undefined}
        />
        <Detail
          hint={graphText("detail.actualPathHint")}
          label={graphText("detail.actualPath")}
          value={link.include?.actualPath}
        />
      </dl>
    </div>
  );
}

function SelectEndpointButton({
  label,
  node,
  onSelectNode,
}: {
  label: string;
  node: GraphNode | undefined;
  onSelectNode(node: GraphNode): void;
}): React.ReactElement {
  return (
    <button
      type="button"
      className="h-7 cursor-pointer rounded-md border border-[#405068] bg-[#202735] px-2 text-[11px] text-[#d7dde8] hover:border-[#4b5a70] disabled:cursor-not-allowed disabled:border-[#303a49] disabled:text-[#717b8c]"
      disabled={!node}
      title={label}
      onClick={() => {
        if (node) {
          onSelectNode(node);
        }
      }}
    >
      {label}
    </button>
  );
}

function Detail({
  hint,
  label,
  value,
}: {
  hint?: string;
  label: string;
  value: string | undefined;
}): React.ReactElement | null {
  if (!value) {
    return null;
  }
  return (
    <>
      <dt className="flex min-w-0 items-center gap-1 text-[11px] text-[#8d98a8]">
        <span className="min-w-0 overflow-hidden text-ellipsis whitespace-nowrap">{label}</span>
        {hint ? <DetailHint hint={hint} label={label} /> : null}
      </dt>
      <dd
        className="m-0 overflow-hidden text-ellipsis whitespace-nowrap text-xs text-[#d7dde8]"
        title={value}
      >
        {value}
      </dd>
    </>
  );
}

interface TooltipPosition {
  left: number;
  top: number;
  maxWidth: number;
}

function DetailHint({ hint, label }: { hint: string; label: string }): React.ReactElement {
  const triggerRef = useRef<HTMLSpanElement>(null);
  const tooltipRef = useRef<HTMLSpanElement>(null);
  const [visible, setVisible] = useState(false);
  const [position, setPosition] = useState<TooltipPosition>();
  const showTooltip = useCallback(() => {
    setVisible(true);
  }, []);
  const hideTooltip = useCallback(() => {
    setVisible(false);
    setPosition(undefined);
  }, []);
  useLayoutEffect(() => {
    if (!visible) {
      return undefined;
    }
    const updatePosition = (): void => {
      setPosition(tooltipPositionFor(triggerRef.current, tooltipRef.current));
    };
    updatePosition();
    window.addEventListener("resize", updatePosition);
    window.addEventListener("scroll", updatePosition, true);
    return () => {
      window.removeEventListener("resize", updatePosition);
      window.removeEventListener("scroll", updatePosition, true);
    };
  }, [visible]);
  return (
    <span className="group relative inline-flex shrink-0 items-center">
      <span
        ref={triggerRef}
        tabIndex={0}
        aria-label={`${label}: ${hint}`}
        className="inline-grid h-3.5 w-3.5 cursor-help place-items-center rounded-full border border-[#405068] text-[10px] leading-none text-[#8d98a8] outline-none hover:border-[#89ddff] hover:text-[#d7dde8] focus:border-[#89ddff] focus:text-[#d7dde8]"
        onBlur={hideTooltip}
        onFocus={showTooltip}
        onPointerEnter={showTooltip}
        onPointerLeave={hideTooltip}
      >
        ?
      </span>
      {visible ? (
        <span
          ref={tooltipRef}
          role="tooltip"
          className="pointer-events-none fixed z-[1000] rounded-md border border-[#405068] bg-[#0d1117] px-2 py-1.5 text-[11px] leading-[1.35] whitespace-normal text-[#d7dde8] shadow-[0_10px_24px_rgb(0_0_0_/_35%)]"
          style={{
            left: position?.left ?? -9999,
            top: position?.top ?? -9999,
            maxWidth: position?.maxWidth ?? tooltipMaximumWidth(),
            visibility: position ? "visible" : "hidden",
          }}
        >
          {hint}
        </span>
      ) : null}
    </span>
  );
}

function tooltipPositionFor(
  element: HTMLElement | null,
  tooltip: HTMLElement | null,
): TooltipPosition | undefined {
  if (!element) {
    return undefined;
  }
  const margin = 12;
  const gap = 6;
  const rect = element.getBoundingClientRect();
  const maxWidth = tooltipMaximumWidth();
  const tooltipRect = tooltip?.getBoundingClientRect();
  const tooltipWidth = Math.min(tooltipRect?.width ?? maxWidth, maxWidth);
  const tooltipHeight = tooltipRect?.height ?? 80;
  const left = clamp(
    rect.left + rect.width / 2 - tooltipWidth / 2,
    margin,
    Math.max(margin, window.innerWidth - tooltipWidth - margin),
  );
  const belowTop = rect.bottom + gap;
  const aboveTop = rect.top - gap - tooltipHeight;
  const top =
    belowTop + tooltipHeight + margin <= window.innerHeight || aboveTop < margin
      ? clamp(belowTop, margin, Math.max(margin, window.innerHeight - tooltipHeight - margin))
      : aboveTop;
  return { left, top, maxWidth };
}

function tooltipMaximumWidth(): number {
  const margin = 12;
  return Math.max(160, Math.min(280, window.innerWidth - margin * 2));
}

function useSourceRanges(items: GraphSourceItem[]): GraphSourceState {
  const [state, setState] = useState<GraphSourceState>(() => ({
    loading: false,
    byId: new Map(),
  }));

  useEffect(() => {
    if (items.length === 0) {
      setState({ loading: false, byId: new Map() });
      return undefined;
    }
    const requestId = `source:${Date.now()}:${Math.random().toString(36).slice(2)}`;
    const handleMessage = (event: MessageEvent<unknown>) => {
      if (!isSourceRangesMessage(event.data) || event.data.requestId !== requestId) {
        return;
      }
      setState({
        loading: false,
        byId: new Map(event.data.items.map((item) => [item.id, item])),
      });
    };
    window.addEventListener("message", handleMessage);
    setState({ loading: true, byId: new Map() });
    vscode.postMessage({
      type: "readSourceRanges",
      requestId,
      items: items.map(sourceRangeRequestItem),
    });
    return () => window.removeEventListener("message", handleMessage);
  }, [items]);

  return state;
}

function isSourceRangesMessage(value: unknown): value is SourceRangesMessage {
  if (!value || typeof value !== "object") {
    return false;
  }
  const message = value as Partial<SourceRangesMessage>;
  return (
    message.type === "sourceRanges" &&
    typeof message.requestId === "string" &&
    Array.isArray(message.items)
  );
}

function sourceRangeRequestItem(item: GraphSourceItem): AspGraphSourceRangeRequestItem {
  return {
    id: item.id,
    uri: item.uri,
    range: item.range,
    highlightRange: item.highlightRange,
    kind: item.kind,
  };
}

function sourceItemsForNode(
  node: GraphNode,
  graphData: GraphData,
): { declarationItems: GraphSourceItem[]; usageItems: GraphSourceItem[] } {
  const declarationItems =
    node.uri && node.range
      ? [
          {
            id: `declaration:${node.id}`,
            uri: node.uri,
            displayPath: nodeFileLabel(node),
            range: node.sourceRange ?? node.range,
            highlightRange: node.range,
            kind: "declaration" as const,
            title: graphText("detail.declaration"),
            detail: nodeTypeLabel(node),
          },
        ]
      : [];
  const usageItems: GraphSourceItem[] = [];
  for (const link of graphData.links) {
    if (
      nodeIdForEndpoint(link.target) !== node.id ||
      (link.kind !== "references" &&
        link.kind !== "assignments" &&
        link.kind !== "calls" &&
        link.kind !== "unresolvedReference")
    ) {
      continue;
    }
    link.ranges.forEach((location, index) => {
      usageItems.push({
        id: `usage:${link.id}:${index}:${location.uri}:${location.range.start.line}:${location.range.start.character}`,
        uri: location.uri,
        displayPath: location.displayPath ?? pathForDisplay(location.uri),
        range: location.range,
        highlightRange: location.range,
        kind: link.kind === "calls" ? "call" : "reference",
        title: sourceUsageTitle(link),
      });
    });
  }
  usageItems.sort(compareSourceItems);
  return { declarationItems, usageItems };
}

function sourceItemsForLink(link: GraphLink): GraphSourceItem[] {
  return link.ranges
    .map((location, index) => ({
      id: `link:${link.id}:${index}:${location.uri}:${location.range.start.line}:${location.range.start.character}`,
      uri: location.uri,
      displayPath: location.displayPath ?? pathForDisplay(location.uri),
      range: location.range,
      highlightRange: location.range,
      kind: sourceKindForLink(link),
      title: linkOccurrenceTitle(link),
      detail: linkOccurrenceDetail(link, location),
    }))
    .sort(compareSourceItems);
}

function nodeLinksFor(
  node: GraphNode,
  links: GraphLink[],
): { incoming: GraphLink[]; outgoing: GraphLink[] } {
  const incoming: GraphLink[] = [];
  const outgoing: GraphLink[] = [];
  for (const link of links) {
    if (nodeIdForEndpoint(link.source) === node.id) {
      outgoing.push(link);
    }
    if (nodeIdForEndpoint(link.target) === node.id) {
      incoming.push(link);
    }
  }
  return { incoming, outgoing };
}

function nodeLinkDetail(link: GraphLink): string | undefined {
  if (link.kind === "include") {
    return includeRelationDetail(link);
  }
  const typeLabel = linkInspectorTypeLabel(link);
  const parts = detailParts(
    graphRoleLabel(link.role),
    link.label !== typeLabel ? link.label : undefined,
    link.ranges.length > 0 ? occurrenceCountLabel(link.ranges.length) : undefined,
  );
  return parts.length > 0 ? parts.join(" · ") : undefined;
}

function sourceKindForLink(link: GraphLink): AspGraphSourceRangeRequestItem["kind"] {
  switch (link.kind) {
    case "include":
      return "include";
    case "declares":
      return "declaration";
    case "calls":
      return "call";
    case "assignments":
    case "references":
    case "unresolvedReference":
      return "reference";
  }
}

function linkOccurrenceTitle(link: GraphLink): string {
  if (link.kind === "include") {
    return graphText("occurrence.includeDirective");
  }
  if (link.kind === "declares") {
    return graphText("detail.declaration");
  }
  return sourceUsageTitle(link);
}

function linkOccurrenceDetail(
  link: GraphLink,
  location: { uri: string; range: AspGraphRange },
): string | undefined {
  const detail = detailParts(
    rangeLineLabel(location.range),
    link.kind === "include" ? link.include?.path : undefined,
    graphRoleLabel(link.role),
  );
  return detail.length > 0 ? detail.join(" · ") : undefined;
}

function sourceUsageTitle(link: GraphLink): string {
  const label =
    link.kind === "unresolvedReference"
      ? graphText("label.unresolved")
      : linkMeanings[link.kind].label;
  const roleLabel = graphRoleLabel(link.role);
  return roleLabel ? `${label}: ${roleLabel}` : label;
}

function occurrenceCountLabel(count: number): string {
  return graphText(count === 1 ? "occurrence.one" : "occurrence.other", { count });
}

function linkInspectorTypeLabel(link: GraphLink): string {
  const label = linkMeanings[link.kind].label;
  if (graphLinkFilterCategory(link) !== "member") {
    return label;
  }
  return graphText("link.memberType", {
    label: graphLocale === "en" ? label.toLowerCase() : label,
  });
}

function compareSourceItems(left: GraphSourceItem, right: GraphSourceItem): number {
  return (
    left.uri.localeCompare(right.uri) ||
    left.range.start.line - right.range.start.line ||
    left.range.start.character - right.range.start.character ||
    left.title.localeCompare(right.title)
  );
}

function groupedSourceItems(
  items: GraphSourceItem[],
): Array<{ uri: string; items: GraphSourceItem[] }> {
  const groups = new Map<string, GraphSourceItem[]>();
  for (const item of items) {
    const group = groups.get(item.uri);
    if (group) {
      group.push(item);
    } else {
      groups.set(item.uri, [item]);
    }
  }
  return [...groups.entries()].map(([uri, groupItems]) => ({ uri, items: groupItems }));
}

function sourceGroupTitle(
  uri: string,
  items: GraphSourceItem[],
  sourcesById: ReadonlyMap<string, AspGraphSourceRangeResponseItem>,
): string {
  const source = items.map((item) => sourcesById.get(item.id)).find(Boolean);
  return source?.fileName ?? items[0]?.displayPath ?? pathForDisplay(uri) ?? uri;
}

function includeRelationsForFileNode(
  node: GraphNode,
  graphData: GraphData,
): { incoming: IncludeRelation[]; outgoing: IncludeRelation[] } {
  const nodesById = graphNodeMap(graphData.nodes);
  const incoming: IncludeRelation[] = [];
  const outgoing: IncludeRelation[] = [];
  for (const link of graphData.links) {
    if (link.kind !== "include") {
      continue;
    }
    const sourceId = nodeIdForEndpoint(link.source);
    const targetId = nodeIdForEndpoint(link.target);
    if (sourceId === node.id) {
      outgoing.push(includeTargetRelation(link, nodesById));
    }
    if (targetId === node.id) {
      incoming.push(includeSourceRelation(link, nodesById));
    }
  }
  return { incoming, outgoing };
}

function includeTargetRelation(
  link: GraphLink,
  nodesById: ReadonlyMap<string, GraphNode>,
): IncludeRelation {
  const target = nodesById.get(nodeIdForEndpoint(link.target));
  const directive = link.ranges[0];
  const targetUri = target?.uri ?? link.include?.resolvedUri;
  return {
    id: `include-target:${link.id}`,
    title: target?.label ?? link.include?.path ?? link.label,
    detail: includeRelationDetail(link),
    fileUri: targetUri,
    fileLabel: graphText("action.openFile"),
    directiveUri: directive?.uri,
    directiveRange: directive?.range,
    directiveLabel: graphText("action.openDirective"),
    exists: link.include?.exists,
  };
}

function includeSourceRelation(
  link: GraphLink,
  nodesById: ReadonlyMap<string, GraphNode>,
): IncludeRelation {
  const source = nodesById.get(nodeIdForEndpoint(link.source));
  const directive = link.ranges[0];
  return {
    id: `include-source:${link.id}`,
    title: source?.label ?? directive?.displayPath ?? pathForDisplay(directive?.uri) ?? link.label,
    detail: includeRelationDetail(link),
    fileUri: source?.uri ?? directive?.uri,
    fileLabel: graphText("action.openFile"),
    directiveUri: directive?.uri,
    directiveRange: directive?.range,
    directiveLabel: graphText("action.openDirective"),
    exists: true,
  };
}

function includeRelationDetail(link: GraphLink): string {
  const parts = [
    includeModeLabel(link.include?.mode),
    link.include?.path,
    link.include?.exists === false ? graphText("label.missing") : undefined,
  ].filter(Boolean);
  return parts.length > 0 ? parts.join(" · ") : link.label;
}

function graphNodeMap(nodes: GraphNode[]): Map<string, GraphNode> {
  return new Map(nodes.map((node) => [node.id, node]));
}

function initialGraphNodePosition(
  index: number,
  totalCount: number,
  value: number,
): { x: number; y: number; z: number } {
  if (totalCount <= 1) {
    return { x: 0, y: 0, z: 0 };
  }
  const angle = index * graphGoldenAngle;
  const radius = Math.sqrt(index + 1) * graphInitialNodeSpacing + value;
  return {
    x: Math.cos(angle) * radius,
    y: Math.sin(angle) * radius,
    z: (positiveModulo(index, 7) - 3) * graphInitialNodeZSpacing,
  };
}

function graphDataFor(
  payload: AspGraphPayload | undefined,
  themePalette: GraphThemePalette,
): GraphData {
  if (!payload) {
    return { nodes: [], links: [] };
  }
  const referenceCounts = graphReferenceCounts(payload.links);
  return {
    nodes: payload.nodes.map((node, index) => {
      const category = nodeCategoryForColor(node);
      const referenceCount = referenceCounts.get(node.id) ?? 0;
      const value = nodeValue(referenceCount);
      const position = initialGraphNodePosition(index, payload.nodes.length, value);
      return {
        ...node,
        category,
        referenceCount,
        value,
        color: themePalette.nodeColors[category],
        x: position.x,
        y: position.y,
        z: position.z,
      };
    }),
    links: payload.links.map((link) => ({
      ...link,
      color: graphLinkColor(link, themePalette),
    })),
  };
}

function graphDataForRender(
  graphData: GraphData,
  positions: ReadonlyMap<string, PositionSyncEntry>,
  mode: ViewMode,
): GraphData {
  const transform =
    mode === "3d" ? positionSyncTransformFor3d(graphData.nodes, positions) : undefined;
  return {
    nodes: graphData.nodes.map((node) => {
      const position = positions.get(node.id);
      const renderPosition =
        mode === "3d" ? positionSyncPositionFor3d(position, transform) : position;
      return {
        ...node,
        x: renderPosition?.x ?? node.x,
        y: renderPosition?.y ?? node.y,
        z: renderPosition?.z ?? node.z,
      };
    }),
    links: graphData.links.map((link) => ({
      ...link,
      source: nodeIdForEndpoint(link.source),
      target: nodeIdForEndpoint(link.target),
    })),
  };
}

function positionSyncTransformFor3d(
  nodes: GraphNode[],
  positions: ReadonlyMap<string, PositionSyncEntry>,
): PositionSyncTransform | undefined {
  let minimumX = Infinity;
  let maximumX = -Infinity;
  let minimumY = Infinity;
  let maximumY = -Infinity;
  for (const node of nodes) {
    const position = positions.get(node.id);
    const x = finiteNumber(position?.x);
    const y = finiteNumber(position?.y);
    if (x === undefined || y === undefined) {
      continue;
    }
    minimumX = Math.min(minimumX, x);
    maximumX = Math.max(maximumX, x);
    minimumY = Math.min(minimumY, y);
    maximumY = Math.max(maximumY, y);
  }
  if (
    !Number.isFinite(minimumX) ||
    !Number.isFinite(maximumX) ||
    !Number.isFinite(minimumY) ||
    !Number.isFinite(maximumY)
  ) {
    return undefined;
  }
  const span = Math.max(maximumX - minimumX, maximumY - minimumY);
  return {
    centerX: (minimumX + maximumX) / 2,
    centerY: (minimumY + maximumY) / 2,
    scale: span > 0 ? graph3dSyncSpan / span : 1,
  };
}

function positionSyncPositionFor3d(
  position: PositionSyncEntry | undefined,
  transform: PositionSyncTransform | undefined,
): PositionSyncEntry | undefined {
  if (!position) {
    return undefined;
  }
  const x = finiteNumber(position?.x);
  const y = finiteNumber(position?.y);
  if (x === undefined || y === undefined) {
    return position;
  }
  if (!transform) {
    return position;
  }
  return {
    ...position,
    x: (x - transform.centerX) * transform.scale,
    y: (position.sourceMode === "2d" ? -1 : 1) * (y - transform.centerY) * transform.scale,
  };
}

function graphScopeTitle(scope: AspGraphPayload["scope"]): string {
  if (scope === "document") {
    return graphText("view.currentFileGraph");
  }
  if (scope === "folder") {
    return graphText("view.folderGraph");
  }
  return graphText("view.workspaceGraph");
}

function graphRootName(payload: AspGraphPayload | undefined): string | undefined {
  if (!payload || payload.scope === "workspace") {
    return undefined;
  }
  if (payload.scope === "folder") {
    return baseNameFromUri(payload.rootUri);
  }
  const rootNode =
    payload.nodes.find((node) => node.isRoot) ??
    payload.nodes.find((node) => node.uri === payload.rootUri);
  return (
    rootNode?.label || baseNameFromPath(rootNode?.fileName) || baseNameFromUri(payload.rootUri)
  );
}

function baseNameFromPath(value: string | undefined): string | undefined {
  const fileName = value?.replaceAll("\\", "/").split("/").filter(Boolean).at(-1);
  return fileName || undefined;
}

function baseNameFromUri(value: string | undefined): string | undefined {
  if (!value) {
    return undefined;
  }
  try {
    return baseNameFromPath(decodeURIComponent(new URL(value).pathname));
  } catch {
    return baseNameFromPath(value);
  }
}

function graphLinkColor(link: AspGraphLink, themePalette: GraphThemePalette): string {
  return themePalette.linkFilterColors[graphLinkFilterCategory(link)];
}

function linkSwatchWidth(link: Pick<AspGraphLink, "kind" | "role">): number {
  return graphLinkFilterCategory(link) === "include" ? 4 : 2;
}

function graphLinkFilterCategory(link: Pick<AspGraphLink, "kind" | "role">): LinkFilterCategory {
  return link.role === "member" ? "member" : link.kind;
}

function filterGraphData(
  graphData: GraphData,
  hiddenNodeCategories: ReadonlySet<NodeColorCategory>,
  hiddenLinkCategories: ReadonlySet<LinkFilterCategory>,
  hideSingleNodes: boolean,
  hideUnreferencedGlobalSymbols: boolean,
): GraphData {
  const hasPayloadRoot = graphData.nodes.some((node) => node.isRoot);
  const canHideUnreferencedGlobalSymbols = hideUnreferencedGlobalSymbols && hasPayloadRoot;
  const retainedGlobalNodeIds = canHideUnreferencedGlobalSymbols
    ? retainedGlobalSymbolNodeIds(graphData.nodes, graphData.links)
    : undefined;
  let visibleNodes = graphData.nodes.filter(
    (node) =>
      !hiddenNodeCategories.has(node.category) &&
      (!canHideUnreferencedGlobalSymbols ||
        !isHideableGlobalSymbolNode(node) ||
        retainedGlobalNodeIds?.has(node.id) === true),
  );
  let visibleNodeIds = new Set(visibleNodes.map((node) => node.id));
  let visibleLinks = graphData.links.filter((link) => {
    const sourceId = nodeIdForEndpoint(link.source);
    const targetId = nodeIdForEndpoint(link.target);
    return (
      visibleNodeIds.has(sourceId) &&
      visibleNodeIds.has(targetId) &&
      !hiddenLinkCategories.has(graphLinkFilterCategory(link))
    );
  });
  if (hideSingleNodes) {
    const connectedNodeIds = connectedNodeIdsFor(visibleLinks);
    visibleNodes = visibleNodes.filter(
      (node) =>
        node.isRoot ||
        connectedNodeIds.has(node.id) ||
        retainedGlobalNodeIds?.has(node.id) === true ||
        (!hasPayloadRoot && isFileLikeGraphNode(node)),
    );
    visibleNodeIds = new Set(visibleNodes.map((node) => node.id));
    visibleLinks = visibleLinks.filter(
      (link) =>
        visibleNodeIds.has(nodeIdForEndpoint(link.source)) &&
        visibleNodeIds.has(nodeIdForEndpoint(link.target)),
    );
  }
  const referenceCounts = graphReferenceCountsForGraphLinks(visibleLinks);
  for (const node of visibleNodes) {
    const referenceCount = referenceCounts.get(node.id) ?? 0;
    node.value = nodeValue(referenceCount);
  }
  return {
    nodes: visibleNodes,
    links: visibleLinks,
  };
}

function retainedGlobalSymbolNodeIds(nodes: GraphNode[], links: GraphLink[]): Set<string> {
  const nodesById = graphNodeMap(nodes);
  const retainedNodeIds = new Set<string>();
  const rootNodes = nodes.filter((node) => node.isRoot);
  const rootNodeIds = new Set(rootNodes.map((node) => node.id));
  const rootUris = new Set(
    rootNodes.map((node) => node.uri).filter((uri): uri is string => Boolean(uri)),
  );
  for (const node of nodes) {
    if (node.uri && rootUris.has(node.uri) && isHideableGlobalSymbolNode(node)) {
      retainedNodeIds.add(node.id);
    }
  }
  for (const link of links) {
    const sourceId = nodeIdForEndpoint(link.source);
    const targetId = nodeIdForEndpoint(link.target);
    const sourceNode = nodesById.get(sourceId);
    const targetNode = nodesById.get(targetId);
    if (rootNodeIds.has(sourceId) && targetNode && isHideableGlobalSymbolNode(targetNode)) {
      retainedNodeIds.add(targetNode.id);
    }
    if (rootNodeIds.has(targetId) && sourceNode && isHideableGlobalSymbolNode(sourceNode)) {
      retainedNodeIds.add(sourceNode.id);
    }
    if (
      !isSymbolReferenceGraphLink(link) ||
      !sourceNode?.uri ||
      !targetNode?.uri ||
      sourceNode.uri === targetNode.uri
    ) {
      continue;
    }
    if (isHideableGlobalSymbolNode(targetNode)) {
      retainedNodeIds.add(targetNode.id);
    }
    if (isHideableGlobalSymbolNode(sourceNode)) {
      retainedNodeIds.add(sourceNode.id);
    }
  }
  return retainedNodeIds;
}

function isSymbolReferenceGraphLink(link: GraphLink): boolean {
  return link.kind === "references" || link.kind === "assignments" || link.kind === "calls";
}

function isHideableGlobalSymbolNode(node: GraphNode): boolean {
  if (node.kind !== "vbDeclaration" || node.origin !== "source") {
    return false;
  }
  switch (node.category) {
    case "function":
    case "sub":
    case "class":
    case "globalVariable":
    case "globalConstant":
      return true;
    default:
      return false;
  }
}

function connectedNodeIdsFor(links: GraphLink[]): Set<string> {
  const ids = new Set<string>();
  for (const link of links) {
    ids.add(nodeIdForEndpoint(link.source));
    ids.add(nodeIdForEndpoint(link.target));
  }
  return ids;
}

function graphStatsFor(graphData: GraphData): AspGraphPayload["stats"] {
  const stats: AspGraphPayload["stats"] = {
    files: 0,
    declarations: 0,
    references: 0,
    assignments: 0,
    calls: 0,
    unresolvedReferences: 0,
    includes: 0,
    missingIncludes: 0,
    nodes: graphData.nodes.length,
    links: graphData.links.length,
  };
  for (const node of graphData.nodes) {
    if (isFileLikeGraphNode(node)) {
      stats.files += 1;
    } else if (node.kind === "vbDeclaration") {
      stats.declarations += 1;
    }
  }
  for (const link of graphData.links) {
    if (link.kind === "include") {
      stats.includes += 1;
      if (link.include?.exists === false) {
        stats.missingIncludes += 1;
      }
    } else if (link.kind === "references") {
      stats.references += 1;
    } else if (link.kind === "assignments") {
      stats.assignments += 1;
    } else if (link.kind === "calls") {
      stats.calls += 1;
    } else if (link.kind === "unresolvedReference") {
      stats.unresolvedReferences += 1;
    }
  }
  return stats;
}

function graphReferenceCounts(links: AspGraphLink[]): Map<string, number> {
  const counts = new Map<string, number>();
  for (const link of links) {
    if (
      link.kind !== "include" &&
      link.kind !== "references" &&
      link.kind !== "assignments" &&
      link.kind !== "calls" &&
      link.kind !== "unresolvedReference"
    ) {
      continue;
    }
    counts.set(link.target, (counts.get(link.target) ?? 0) + link.count);
  }
  return counts;
}

function graphReferenceCountsForGraphLinks(links: GraphLink[]): Map<string, number> {
  const counts = new Map<string, number>();
  for (const link of links) {
    if (
      link.kind !== "include" &&
      link.kind !== "references" &&
      link.kind !== "assignments" &&
      link.kind !== "calls" &&
      link.kind !== "unresolvedReference"
    ) {
      continue;
    }
    const targetId = nodeIdForEndpoint(link.target);
    counts.set(targetId, (counts.get(targetId) ?? 0) + link.count);
  }
  return counts;
}

function toggledSet<T>(set: ReadonlySet<T>, value: T): Set<T> {
  const nextSet = new Set(set);
  if (nextSet.has(value)) {
    nextSet.delete(value);
  } else {
    nextSet.add(value);
  }
  return nextSet;
}

function capturePositionSyncEntries(
  mode: ViewMode,
  nodes: GraphNode[],
  graph2d: ForceGraph2DMethods<GraphNode, GraphLink> | undefined,
  graph3d: ForceGraph3DMethods<GraphNode, GraphLink> | undefined,
  store: Map<string, PositionSyncEntry>,
): Map<string, PositionSyncEntry> {
  const entries = new Map<string, PositionSyncEntry>();
  for (const node of nodes) {
    const previous = store.get(node.id);
    const captured =
      mode === "3d"
        ? capturePositionSyncEntry3d(node, graph3d, previous)
        : capturePositionSyncEntry2d(node, graph2d, previous);
    if (captured) {
      store.set(node.id, captured);
      entries.set(node.id, { ...captured });
    } else if (previous) {
      entries.set(node.id, { ...previous });
    }
  }
  return entries;
}

function capturePositionSyncEntry3d(
  node: GraphNode,
  graph3d: ForceGraph3DMethods<GraphNode, GraphLink> | undefined,
  previous: PositionSyncEntry | undefined,
): PositionSyncEntry | undefined {
  const x = finiteNumber(node.x) ?? previous?.x;
  const y = finiteNumber(node.y) ?? previous?.y;
  const z = finiteNumber(node.z) ?? previous?.z ?? 0;
  if (x === undefined || y === undefined) {
    return previous;
  }
  const screen = graph3dScreenCoords(graph3d, x, y, z);
  const cameraDistance = graph3dCameraDistanceToPoint(graph3d, x, y, z);
  return {
    id: node.id,
    sourceMode: "3d",
    x,
    y,
    z,
    screenX: finiteNumber(screen?.x) ?? previous?.screenX,
    screenY: finiteNumber(screen?.y) ?? previous?.screenY,
    cameraDistance: cameraDistance ?? previous?.cameraDistance,
  };
}

function capturePositionSyncEntry2d(
  node: GraphNode,
  graph2d: ForceGraph2DMethods<GraphNode, GraphLink> | undefined,
  previous: PositionSyncEntry | undefined,
): PositionSyncEntry | undefined {
  const x = finiteNumber(node.x) ?? previous?.x;
  const y = finiteNumber(node.y) ?? previous?.y;
  if (x === undefined || y === undefined) {
    return previous;
  }
  const screen = graph2dScreenCoords(graph2d, x, y);
  return {
    id: node.id,
    sourceMode: "2d",
    x,
    y,
    z: finiteNumber(node.z) ?? previous?.z ?? 0,
    screenX: finiteNumber(screen?.x) ?? previous?.screenX,
    screenY: finiteNumber(screen?.y) ?? previous?.screenY,
    cameraDistance: previous?.cameraDistance,
  };
}

function applyPositionSyncTo2d(
  pending: PendingPositionSync,
  nodes: GraphNode[],
  graph2d: ForceGraph2DMethods<GraphNode, GraphLink> | undefined,
): Set<string> {
  const pinnedNodeIds = new Set<string>();
  for (const node of nodes) {
    const entry = pending.entries.get(node.id);
    if (!entry) {
      continue;
    }
    const projected =
      pending.from === "3d"
        ? graph2dCoordsFromScreen(graph2d, entry.screenX, entry.screenY)
        : undefined;
    const x = finiteNumber(projected?.x) ?? entry.x;
    const y = finiteNumber(projected?.y) ?? entry.y;
    if (x === undefined || y === undefined) {
      continue;
    }
    node.x = x;
    node.y = y;
    node.z = entry.z;
    node.vx = 0;
    node.vy = 0;
    node.vz = 0;
    node.fx = x;
    node.fy = y;
    delete node.fz;
    pinnedNodeIds.add(node.id);
  }
  return pinnedNodeIds;
}

function applyPositionSyncTo3d(
  pending: PendingPositionSync,
  nodes: GraphNode[],
  graph3d: ForceGraph3DMethods<GraphNode, GraphLink> | undefined,
): Set<string> {
  const pinnedNodeIds = new Set<string>();
  const transform = positionSyncTransformFor3dFromEntries(nodes, pending.entries);
  const fallbackDistance = graph3dFallbackCameraDistance(graph3d, nodes);
  for (const node of nodes) {
    const capturedEntry = pending.entries.get(node.id);
    const entry = positionSyncPositionFor3d(capturedEntry, transform);
    if (!entry) {
      continue;
    }
    const projected =
      pending.from === "2d"
        ? graph3dCoordsFromScreen(
            graph3d,
            capturedEntry?.screenX,
            capturedEntry?.screenY,
            finiteNumber(capturedEntry?.cameraDistance) ?? fallbackDistance,
          )
        : undefined;
    const x = finiteNumber(projected?.x) ?? finiteNumber(entry.x);
    const y = finiteNumber(projected?.y) ?? finiteNumber(entry.y);
    if (x === undefined || y === undefined) {
      continue;
    }
    const z = finiteNumber(projected?.z) ?? finiteNumber(entry.z) ?? 0;
    node.x = x;
    node.y = y;
    node.z = z;
    node.vx = 0;
    node.vy = 0;
    node.vz = 0;
    node.fx = x;
    node.fy = y;
    node.fz = z;
    pinnedNodeIds.add(node.id);
  }
  return pinnedNodeIds;
}

function positionSyncTransformFor3dFromEntries(
  nodes: GraphNode[],
  entries: ReadonlyMap<string, PositionSyncEntry>,
): PositionSyncTransform | undefined {
  return positionSyncTransformFor3d(nodes, entries);
}

function releasePositionSyncPins(nodes: GraphNode[], pinnedNodeIds: ReadonlySet<string>): void {
  for (const node of nodes) {
    if (!pinnedNodeIds.has(node.id)) {
      continue;
    }
    delete node.fx;
    delete node.fy;
    delete node.fz;
  }
}

function selectionForStatsTarget(target: GraphStatsTarget, graphData: GraphData): Selection {
  if (target.type === "node") {
    const node = graphData.nodes.find((candidate) => candidate.id === target.id);
    return node ? { type: "node", item: node } : undefined;
  }
  const link = graphData.links.find((candidate) => candidate.id === target.id);
  return link ? { type: "link", item: link } : undefined;
}

function focusGraphTarget(
  target: GraphStatsTarget,
  mode: ViewMode,
  graphData: GraphData,
  graph2d: ForceGraph2DMethods<GraphNode, GraphLink> | undefined,
  graph3d: ForceGraph3DMethods<GraphNode, GraphLink> | undefined,
): void {
  if (mode === "3d") {
    focusGraphTarget3d(target, graphData, graph3d);
  } else {
    focusGraphTarget2d(target, graphData, graph2d);
  }
}

function focusGraphTarget2d(
  target: GraphStatsTarget,
  graphData: GraphData,
  graph2d: ForceGraph2DMethods<GraphNode, GraphLink> | undefined,
): void {
  if (!graph2d) {
    return;
  }
  if (target.type === "node") {
    const node = graphData.nodes.find((candidate) => candidate.id === target.id);
    const point = node ? graphNodePoint2d(node) : undefined;
    if (!point) {
      return;
    }
    try {
      graph2d.centerAt(point.x, point.y, graphFocusDurationMs);
      graph2d.zoom(
        Math.max(finiteNumber(graph2d.zoom()) ?? 1, graph2dMinimumFocusZoom),
        graphFocusDurationMs,
      );
    } catch {
      return;
    }
    return;
  }
  const link = graphData.links.find((candidate) => candidate.id === target.id);
  const endpoints = linkEndpointNodes(link, graphData.nodes);
  const sourcePoint = endpoints ? graphNodePoint2d(endpoints.source) : undefined;
  const targetPoint = endpoints ? graphNodePoint2d(endpoints.target) : undefined;
  if (!endpoints || !sourcePoint || !targetPoint) {
    return;
  }
  const focusPoint = midpoint2d(sourcePoint, targetPoint);
  try {
    graph2d.centerAt(focusPoint.x, focusPoint.y, graphFocusDurationMs);
    graph2d.zoomToFit(
      graphFocusDurationMs,
      graph2dLinkFocusPadding,
      (node) => node.id === endpoints.source.id || node.id === endpoints.target.id,
    );
  } catch {
    return;
  }
}

function focusGraphTarget3d(
  target: GraphStatsTarget,
  graphData: GraphData,
  graph3d: ForceGraph3DMethods<GraphNode, GraphLink> | undefined,
): void {
  if (!graph3d) {
    return;
  }
  if (target.type === "node") {
    const node = graphData.nodes.find((candidate) => candidate.id === target.id);
    const point = node ? graphNodePoint3d(node) : undefined;
    if (!point) {
      return;
    }
    focusGraph3dPoint(graph3d, graphData.nodes, point, graph3dMinimumFocusDistance);
    return;
  }
  const link = graphData.links.find((candidate) => candidate.id === target.id);
  const endpoints = linkEndpointNodes(link, graphData.nodes);
  const sourcePoint = endpoints ? graphNodePoint3d(endpoints.source) : undefined;
  const targetPoint = endpoints ? graphNodePoint3d(endpoints.target) : undefined;
  if (!sourcePoint || !targetPoint) {
    return;
  }
  const focusPoint = midpoint3d(sourcePoint, targetPoint);
  focusGraph3dPoint(
    graph3d,
    graphData.nodes,
    focusPoint,
    Math.max(
      graph3dMinimumFocusDistance,
      distance3d(sourcePoint, targetPoint) * graph3dLinkDistanceScale,
    ),
  );
}

function focusGraph3dPoint(
  graph3d: ForceGraph3DMethods<GraphNode, GraphLink>,
  nodes: GraphNode[],
  focusPoint: { x: number; y: number; z: number },
  distance: number,
): void {
  const direction = graph3dViewDirection(graph3d, nodes, focusPoint);
  if (!direction) {
    return;
  }
  const nextPosition = {
    x: focusPoint.x + direction.x * distance,
    y: focusPoint.y + direction.y * distance,
    z: focusPoint.z + direction.z * distance,
  };
  try {
    graph3d.cameraPosition(nextPosition, focusPoint, graphFocusDurationMs);
  } catch {
    return;
  }
}

function graph3dViewDirection(
  graph3d: ForceGraph3DMethods<GraphNode, GraphLink>,
  nodes: GraphNode[],
  focusPoint: { x: number; y: number; z: number },
): { x: number; y: number; z: number } | undefined {
  const cameraPosition = graph3dCameraPosition(graph3d);
  if (!cameraPosition) {
    return undefined;
  }
  const currentTarget = graph3dControlsTarget(graph3d) ?? graphNodeCenter3d(nodes) ?? focusPoint;
  return (
    normalize3d({
      x: cameraPosition.x - currentTarget.x,
      y: cameraPosition.y - currentTarget.y,
      z: cameraPosition.z - currentTarget.z,
    }) ??
    normalize3d({
      x: cameraPosition.x - focusPoint.x,
      y: cameraPosition.y - focusPoint.y,
      z: cameraPosition.z - focusPoint.z,
    }) ?? { x: 0, y: 0, z: 1 }
  );
}

function linkEndpointNodes(
  link: GraphLink | undefined,
  nodes: GraphNode[],
): { source: GraphNode; target: GraphNode } | undefined {
  if (!link) {
    return undefined;
  }
  const nodesById = graphNodeMap(nodes);
  const source = nodesById.get(nodeIdForEndpoint(link.source));
  const target = nodesById.get(nodeIdForEndpoint(link.target));
  return source && target ? { source, target } : undefined;
}

function graphNodePoint2d(node: GraphNode): { x: number; y: number } | undefined {
  const x = finiteNumber(node.x);
  const y = finiteNumber(node.y);
  return x !== undefined && y !== undefined ? { x, y } : undefined;
}

function graphNodePoint3d(node: GraphNode): { x: number; y: number; z: number } | undefined {
  const x = finiteNumber(node.x);
  const y = finiteNumber(node.y);
  const z = finiteNumber(node.z) ?? 0;
  return x !== undefined && y !== undefined ? { x, y, z } : undefined;
}

function midpoint2d(
  source: { x: number; y: number },
  target: { x: number; y: number },
): { x: number; y: number } {
  return {
    x: (source.x + target.x) / 2,
    y: (source.y + target.y) / 2,
  };
}

function midpoint3d(
  source: { x: number; y: number; z: number },
  target: { x: number; y: number; z: number },
): { x: number; y: number; z: number } {
  return {
    x: (source.x + target.x) / 2,
    y: (source.y + target.y) / 2,
    z: (source.z + target.z) / 2,
  };
}

function graph3dScreenCoords(
  graph3d: ForceGraph3DMethods<GraphNode, GraphLink> | undefined,
  x: number,
  y: number,
  z: number,
): { x: number; y: number } | undefined {
  try {
    return graph3d?.graph2ScreenCoords(x, y, z);
  } catch {
    return undefined;
  }
}

function graph3dCoordsFromScreen(
  graph3d: ForceGraph3DMethods<GraphNode, GraphLink> | undefined,
  screenX: number | undefined,
  screenY: number | undefined,
  distance: number | undefined,
): { x: number; y: number; z?: number } | undefined {
  if (screenX === undefined || screenY === undefined || distance === undefined) {
    return undefined;
  }
  try {
    return graph3d?.screen2GraphCoords(screenX, screenY, distance);
  } catch {
    return undefined;
  }
}

function graph2dScreenCoords(
  graph2d: ForceGraph2DMethods<GraphNode, GraphLink> | undefined,
  x: number,
  y: number,
): { x: number; y: number } | undefined {
  try {
    return graph2d?.graph2ScreenCoords(x, y);
  } catch {
    return undefined;
  }
}

function graph2dCoordsFromScreen(
  graph2d: ForceGraph2DMethods<GraphNode, GraphLink> | undefined,
  screenX: number | undefined,
  screenY: number | undefined,
): { x: number; y: number } | undefined {
  if (screenX === undefined || screenY === undefined) {
    return undefined;
  }
  try {
    return graph2d?.screen2GraphCoords(screenX, screenY);
  } catch {
    return undefined;
  }
}

function graph3dCameraDistanceToPoint(
  graph3d: ForceGraph3DMethods<GraphNode, GraphLink> | undefined,
  x: number,
  y: number,
  z: number,
): number | undefined {
  const cameraPosition = graph3dCameraPosition(graph3d);
  if (!cameraPosition) {
    return undefined;
  }
  return distance3d(cameraPosition, { x, y, z });
}

function graph3dFallbackCameraDistance(
  graph3d: ForceGraph3DMethods<GraphNode, GraphLink> | undefined,
  nodes: GraphNode[],
): number | undefined {
  const cameraPosition = graph3dCameraPosition(graph3d);
  if (!cameraPosition) {
    return undefined;
  }
  const controlsTarget = graph3dControlsTarget(graph3d);
  const center = controlsTarget ?? graphNodeCenter3d(nodes);
  return center ? distance3d(cameraPosition, center) : undefined;
}

function graph3dCameraPosition(
  graph3d: ForceGraph3DMethods<GraphNode, GraphLink> | undefined,
): { x: number; y: number; z: number } | undefined {
  try {
    const position = graph3d?.camera().position;
    const x = finiteNumber(position?.x);
    const y = finiteNumber(position?.y);
    const z = finiteNumber(position?.z);
    return x !== undefined && y !== undefined && z !== undefined ? { x, y, z } : undefined;
  } catch {
    return undefined;
  }
}

function graph3dControlsTarget(
  graph3d: ForceGraph3DMethods<GraphNode, GraphLink> | undefined,
): { x: number; y: number; z: number } | undefined {
  try {
    const controls = graph3d?.controls() as { target?: { x?: number; y?: number; z?: number } };
    const x = finiteNumber(controls?.target?.x);
    const y = finiteNumber(controls?.target?.y);
    const z = finiteNumber(controls?.target?.z);
    return x !== undefined && y !== undefined && z !== undefined ? { x, y, z } : undefined;
  } catch {
    return undefined;
  }
}

function graphNodeCenter3d(nodes: GraphNode[]): { x: number; y: number; z: number } | undefined {
  let count = 0;
  let totalX = 0;
  let totalY = 0;
  let totalZ = 0;
  for (const node of nodes) {
    const x = finiteNumber(node.x);
    const y = finiteNumber(node.y);
    const z = finiteNumber(node.z) ?? 0;
    if (x === undefined || y === undefined) {
      continue;
    }
    count += 1;
    totalX += x;
    totalY += y;
    totalZ += z;
  }
  return count > 0 ? { x: totalX / count, y: totalY / count, z: totalZ / count } : undefined;
}

function distance3d(
  from: { x: number; y: number; z: number },
  to: { x: number; y: number; z: number },
): number {
  return Math.hypot(from.x - to.x, from.y - to.y, from.z - to.z);
}

function normalize3d(vector: {
  x: number;
  y: number;
  z: number;
}): { x: number; y: number; z: number } | undefined {
  const length = Math.hypot(vector.x, vector.y, vector.z);
  return length > 0
    ? {
        x: vector.x / length,
        y: vector.y / length,
        z: vector.z / length,
      }
    : undefined;
}

function finiteNumber(value: number | undefined): number | undefined {
  return typeof value === "number" && Number.isFinite(value) ? value : undefined;
}

function isSelectionVisible(selection: Selection, graphData: GraphData): boolean {
  if (selection?.type === "node") {
    return graphData.nodes.some((node) => node.id === selection.item.id);
  }
  if (selection?.type === "link") {
    return graphData.links.some((link) => link.id === selection.item.id);
  }
  return true;
}

function searchTargetsForSearch(
  query: string,
  matchCase: boolean,
  nodes: GraphNode[],
): GraphStatsTarget[] {
  const normalizedQuery = normalizeSearchText(query.trim(), matchCase);
  if (!normalizedQuery) {
    return [];
  }
  return nodes
    .filter((node) => searchableNodeText(node, matchCase).includes(normalizedQuery))
    .map((node) => ({ type: "node" as const, id: node.id }));
}

function highlightForSearchTargets(
  targets: GraphStatsTarget[],
  links: GraphLink[],
  query: string,
): HighlightState | undefined {
  if (!query) {
    return undefined;
  }
  const activeNodeIds = new Set(targets.map((target) => target.id));
  const activeLinkIds = new Set(
    links
      .filter(
        (link) =>
          activeNodeIds.has(nodeIdForEndpoint(link.source)) &&
          activeNodeIds.has(nodeIdForEndpoint(link.target)),
      )
      .map((link) => link.id),
  );
  return { activeNodeIds, activeLinkIds };
}

function isSearchFocusShortcut(event: KeyboardEvent): boolean {
  return isPrimaryModifierShortcut(event, "f") && !event.shiftKey;
}

function isSearchClearShortcut(
  event: KeyboardEvent,
  searchInput: HTMLInputElement | null,
): boolean {
  return event.key === "Escape" && document.activeElement === searchInput;
}

function searchNavigationDirection(
  event: KeyboardEvent,
  searchInput: HTMLInputElement | null,
): 1 | -1 | undefined {
  if (event.key === "Enter" && document.activeElement === searchInput) {
    return event.shiftKey ? -1 : 1;
  }
  if (event.key === "F3" && !event.metaKey && !event.ctrlKey && !event.altKey) {
    return event.shiftKey ? -1 : 1;
  }
  if (isPrimaryModifierShortcut(event, "g")) {
    return event.shiftKey ? -1 : 1;
  }
  return undefined;
}

function isPrimaryModifierShortcut(event: KeyboardEvent, key: string): boolean {
  return event.key.toLowerCase() === key && (event.metaKey || event.ctrlKey) && !event.altKey;
}

function searchableNodeText(node: GraphNode, matchCase: boolean): string {
  return normalizeSearchText(node.label, matchCase);
}

function normalizeSearchText(value: string, matchCase: boolean): string {
  return matchCase ? value : value.toLowerCase();
}

function configureGraphForces(
  graph:
    | ForceGraph2DMethods<GraphNode, GraphLink>
    | ForceGraph3DMethods<GraphNode, GraphLink>
    | undefined,
): void {
  if (!graph) {
    return;
  }
  try {
    const chargeForce = graph.d3Force("charge") as
      | { strength?: (strength: (node: GraphNode) => number) => unknown }
      | undefined;
    chargeForce?.strength?.(graphNodeChargeStrength);
    const linkForce = graph.d3Force("link") as
      | { distance?: (distance: (link: GraphLink) => number) => unknown }
      | undefined;
    linkForce?.distance?.(graphLinkDistance);
    graph.d3ReheatSimulation();
  } catch {
    return;
  }
}

function graphNodeChargeStrength(node: GraphNode): number {
  return -(graphChargeBaseStrength + graphNodeForceValue(node) * graphChargeValueStrength);
}

function graphLinkDistance(link: GraphLink): number {
  return (
    graphLinkDistanceBase +
    (graphEndpointForceValue(link.source) + graphEndpointForceValue(link.target)) *
      graphLinkDistanceValueScale
  );
}

function graphEndpointForceValue(endpoint: string | GraphNode): number {
  return typeof endpoint === "string" ? minimumNodeValue : graphNodeForceValue(endpoint);
}

function graphNodeForceValue(node: GraphNode): number {
  return finiteNumber(node.value) ?? minimumNodeValue;
}

function highlightForSelection(
  selection: Selection,
  links: GraphLink[],
  showOutgoingLinks: boolean,
): HighlightState | undefined {
  if (!selection) {
    return undefined;
  }
  if (selection.type === "link") {
    const selectedLink = links.find((link) => link.id === selection.item.id) ?? selection.item;
    return {
      activeNodeIds: new Set([
        nodeIdForEndpoint(selectedLink.source),
        nodeIdForEndpoint(selectedLink.target),
      ]),
      activeLinkIds: new Set([selectedLink.id]),
    };
  }
  const selectedNodeId = selection.item.id;
  const activeNodeIds = new Set<string>([selectedNodeId]);
  const activeLinkIds = new Set<string>();
  for (const link of links) {
    if (nodeIdForEndpoint(link.target) !== selectedNodeId) {
      continue;
    }
    const sourceNodeId = nodeIdForEndpoint(link.source);
    activeNodeIds.add(sourceNodeId);
    activeLinkIds.add(link.id);
  }
  if (showOutgoingLinks) {
    for (const link of links) {
      if (nodeIdForEndpoint(link.source) !== selectedNodeId) {
        continue;
      }
      const targetNodeId = nodeIdForEndpoint(link.target);
      activeNodeIds.add(targetNodeId);
      activeLinkIds.add(link.id);
    }
  }
  return { activeNodeIds, activeLinkIds };
}

function nodeIdForEndpoint(endpoint: string | GraphNode): string {
  return typeof endpoint === "string" ? endpoint : endpoint.id;
}

function isActiveNode(node: GraphNode, highlight: HighlightState | undefined): boolean {
  return !highlight || highlight.activeNodeIds.has(node.id);
}

function isActiveLink(link: GraphLink, highlight: HighlightState | undefined): boolean {
  return !highlight || highlight.activeLinkIds.has(link.id);
}

function nodeColor(
  node: GraphNode,
  highlight: HighlightState | undefined,
  themePalette: GraphThemePalette,
): string {
  return isActiveNode(node, highlight) ? node.color : themePalette.mutedNode;
}

function linkColor(
  link: GraphLink,
  highlight: HighlightState | undefined,
  themePalette: GraphThemePalette,
): string {
  return isActiveLink(link, highlight) ? link.color : themePalette.mutedLink;
}

function nodeTextObject(
  node: GraphNode,
  highlight: HighlightState | undefined,
  themePalette: GraphThemePalette,
): SpriteText {
  const offset = nodeTextOffset3d(node);
  const textHeight = nodeTextHeight(node);
  const sprite = new SpriteText(node.label, textHeight, nodeColor(node, highlight, themePalette));
  sprite.fontFace = "system-ui, sans-serif";
  sprite.fontWeight = nodeTextFontWeight(node);
  sprite.backgroundColor = false;
  sprite.padding = 0.5;
  (sprite as CenteredSpriteText).center.y = nodeTextAnchor(offset, textHeight);
  return sprite;
}

function nodeTextHeight(node: GraphNode): number {
  return isFileLikeGraphNode(node) ? 5 : 4;
}

function nodeTextOffset(node: GraphNode): number {
  return nodeRadius(node) + (isFileLikeGraphNode(node) ? 2.5 : 1.5);
}

function nodeTextOffset3d(node: GraphNode): number {
  return nodeRadius(node) + (isFileLikeGraphNode(node) ? 1.5 : 0.75);
}

function nodeTextAnchor(offset: number, textHeight: number): number {
  return -offset / textHeight;
}

function nodeTextFontWeight(node: GraphNode): "500" | "600" {
  return isFileLikeGraphNode(node) ? "600" : "500";
}

function nodeValue(referenceCount: number): number {
  const scale = Math.sqrt(
    clamp(referenceCount, 0, maximumNodeScaleReferenceCount) / maximumNodeScaleReferenceCount,
  );
  return minimumNodeValue + scale * (maximumNodeValue - minimumNodeValue);
}

function linkArrowLength(link: GraphLink): number {
  return link.kind === "include" ? 7 : 3.5;
}

function linkWidth2d(link: GraphLink, highlight: HighlightState | undefined): number {
  const width = clamp(0.8 + Math.log2(link.count + 1) * 0.4, 0.8, 3);
  const visibleWidth = link.kind === "include" ? Math.max(3.5, width + 1.5) : width;
  return isActiveLink(link, highlight) ? visibleWidth : Math.max(0.35, visibleWidth * 0.35);
}

function linkWidth3d(link: GraphLink, highlight: HighlightState | undefined): number {
  if (!isActiveLink(link, highlight)) {
    return 0.05;
  }
  return link.kind === "include" ? 1.5 : 0;
}

function linkParticleCount(link: GraphLink, highlight: HighlightState | undefined): number {
  if (!isActiveLink(link, highlight)) {
    return 0;
  }
  return clamp(Math.ceil(Math.sqrt(link.count)), 1, 8);
}

function linkParticleWidth3d(link: GraphLink): number {
  return link.kind === "include" ? 2.75 : 1.25;
}

function clamp(value: number, minimum: number, maximum: number): number {
  return Math.min(maximum, Math.max(minimum, value));
}

function maxInspectorWidthForLayout(containerWidth: number): number {
  if (containerWidth <= 0) {
    return inspectorMaximumWidth;
  }
  return Math.max(
    inspectorMinimumWidth,
    Math.min(inspectorMaximumWidth, containerWidth - graphMinimumWidth - paneResizeHandleWidth),
  );
}

function nodeCategoryForColor(node: AspGraphNode): NodeColorCategory {
  if (node.isRoot) {
    return "root";
  }
  if (node.kind === "missingInclude") {
    return "missingInclude";
  }
  if (node.kind === "file") {
    return "file";
  }
  if (node.kind === "vbUnresolved") {
    if (node.role === "member") {
      return "member";
    }
    return isCallableUnresolvedRole(node.role) ? "unresolvedFunction" : "unresolved";
  }
  if (node.kind === "vbMemberReference") {
    return "member";
  }
  if (node.externalKind === "member") {
    return "member";
  }
  switch (node.declarationKind) {
    case "function":
      return "function";
    case "sub":
      return "sub";
    case "class":
      return "class";
    case "method":
      if (node.procedureKind === "function") {
        return "methodFunction";
      }
      if (node.procedureKind === "sub") {
        return "methodSub";
      }
      return "method";
    case "property":
      return "property";
    case "field":
      return "localVariable";
    case "parameter":
      return "parameter";
    case "variable":
      if (node.unresolvedGlobal === true) {
        return "unresolvedGlobalVariable";
      }
      if (node.implicitLocal === true) {
        return "implicitLocalVariable";
      }
      return node.bindingScope === "local" ? "localVariable" : "globalVariable";
    case "constant":
      if (node.bindingScope === "local") {
        return "localConstant";
      }
      return node.memberOf ? "localConstant" : "globalConstant";
    case "object":
      return "globalVariable";
    default:
      return externalNodeCategory(node);
  }
}

function externalNodeCategory(node: AspGraphNode): NodeColorCategory {
  switch (node.externalKind) {
    case "function":
      return "function";
    case "constant":
      return "globalConstant";
    case "member":
      return "member";
    case "object":
      return "globalVariable";
    default:
      return "globalVariable";
  }
}

function nodeLabel(node: GraphNode): string {
  if (node.kind === "vbDeclaration" && node.declarationKind) {
    return `${nodeTypeLabel(node)}: ${node.label}`;
  }
  if (node.kind === "vbUnresolved" && node.role) {
    return `${nodeTypeLabel(node)}: ${node.label}`;
  }
  if (node.kind === "vbMemberReference") {
    return `${graphText("label.member")}: ${node.label}`;
  }
  return node.label;
}

function isCallableUnresolvedRole(role: string | undefined): boolean {
  return role === "function" || role === "procedure" || role === "unknown";
}

function linkLabel(link: GraphLink): string {
  const meaning = linkMeanings[link.kind];
  const count = occurrenceCountLabel(link.count);
  return `${meaning.label}: ${link.label} (${count})\n${meaning.description}`;
}

function paintNode(
  node: GraphNode,
  canvas: CanvasRenderingContext2D,
  highlight: HighlightState | undefined,
  themePalette: GraphThemePalette,
): void {
  const radius = nodeRadius(node);
  const active = isActiveNode(node, highlight);
  canvas.beginPath();
  canvas.arc(node.x ?? 0, node.y ?? 0, radius, 0, 2 * Math.PI, false);
  canvas.fillStyle = active ? node.color : themePalette.mutedNode;
  canvas.fill();
  const offset = nodeTextOffset(node);
  canvas.save();
  canvas.font = `${nodeTextFontWeight(node)} ${nodeTextHeight(node)}px system-ui, sans-serif`;
  canvas.fillStyle = nodeColor(node, highlight, themePalette);
  canvas.textAlign = "center";
  canvas.textBaseline = "bottom";
  canvas.fillText(node.label, node.x ?? 0, (node.y ?? 0) - offset);
  canvas.restore();
}

function paintNodePointerArea(
  node: GraphNode,
  color: string,
  canvas: CanvasRenderingContext2D,
): void {
  canvas.fillStyle = color;
  canvas.beginPath();
  canvas.arc(node.x ?? 0, node.y ?? 0, nodeRadius(node) + 2, 0, 2 * Math.PI, false);
  canvas.fill();
}

function nodeRadius(node: GraphNode): number {
  return (isFileLikeGraphNode(node) ? 3.4 : 3) + node.value;
}

createRoot(document.getElementById("root") ?? document.body).render(<App />);
