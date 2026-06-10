import * as vscode from "vscode";
import { displayPathForPathOrUri, displayPathForUri, displayPathForUriText } from "./path-display";

export type AspGraphLocale = "en" | "ja";
export type AspGraphWebviewTheme = "light" | "dark";
export type AspGraphWebviewThemeSetting = AspGraphWebviewTheme | "auto";
export type AspGraphInfoPanelPosition = "left" | "right";

export interface AspGraphPayload {
  scope: "document" | "folder" | "workspace";
  rootUri?: string;
  locale?: AspGraphLocale;
  nodes: AspGraphNode[];
  links: AspGraphLink[];
  settings?: {
    initialViewMode?: "2d" | "3d";
    theme?: AspGraphWebviewThemeSetting;
    infoPanelPosition?: AspGraphInfoPanelPosition;
    hideSingleNodes?: boolean;
    hideUnreferencedGlobalSymbols?: boolean;
    showOutgoingSelectionLinks?: boolean;
    showIncomingDocumentIncludes?: boolean;
    showIncomingFolderIncludes?: boolean;
    hiddenNodeCategories?: AspGraphNodeCategory[];
    hiddenLinkCategories?: AspGraphLinkFilterCategory[];
  };
  stats: {
    files: number;
    declarations: number;
    references: number;
    assignments: number;
    calls: number;
    unresolvedReferences: number;
    includes: number;
    missingIncludes: number;
    nodes: number;
    links: number;
  };
  truncated?: {
    reason: string;
  };
}

export type AspGraphNodeCategory =
  | "root"
  | "file"
  | "missingInclude"
  | "function"
  | "sub"
  | "class"
  | "method"
  | "methodFunction"
  | "methodSub"
  | "property"
  | "member"
  | "globalVariable"
  | "unresolvedGlobalVariable"
  | "globalConstant"
  | "localVariable"
  | "localConstant"
  | "parameter"
  | "unresolved";

export type AspGraphLinkFilterCategory = AspGraphLink["kind"] | "member";

export interface AspGraphNode {
  id: string;
  kind: "file" | "missingInclude" | "vbDeclaration" | "vbUnresolved" | "vbMemberReference";
  label: string;
  uri?: string;
  fileName?: string;
  displayPath?: string;
  range?: AspGraphRange;
  sourceRange?: AspGraphRange;
  exists?: boolean;
  declarationKind?: string;
  role?: string;
  receiverName?: string;
  memberName?: string;
  memberOf?: string;
  bindingScope?: string;
  procedureKind?: string;
  implicit?: boolean;
  unresolvedGlobal?: boolean;
  typeName?: string;
  arrayKind?: string;
  arrayDimensions?: string[];
  group?: string;
  origin?: "source" | "builtin" | "configured";
  externalKind?: "function" | "constant" | "object" | "member" | "event";
  isRoot?: boolean;
}

export interface AspGraphLink {
  id: string;
  source: string;
  target: string;
  kind: "include" | "declares" | "references" | "assignments" | "calls" | "unresolvedReference";
  label: string;
  role?: string;
  count: number;
  ranges: Array<{ uri: string; displayPath?: string; range: AspGraphRange }>;
  include?: {
    path: string;
    mode: "file" | "virtual";
    exists: boolean;
    resolvedUri: string;
    actualPath?: string;
    displayPath?: string;
    pathCaseMatches?: boolean;
  };
}

export interface AspGraphRange {
  start: { line: number; character: number };
  end: { line: number; character: number };
}

export type AspGraphSourceRangeKind = "declaration" | "reference" | "call" | "include";

export interface AspGraphSourceRangeRequestItem {
  id: string;
  uri: string;
  range?: AspGraphRange;
  highlightRange?: AspGraphRange;
  kind?: AspGraphSourceRangeKind;
}

export interface AspGraphSourceRangeResponseItem {
  id: string;
  uri: string;
  fileName?: string;
  text?: string;
  range?: AspGraphRange;
  highlightRange?: AspGraphRange;
  error?: string;
}

interface OpenRangeMessage {
  type: "openRange";
  uri: string;
  range?: AspGraphRange;
}

interface OpenFlowchartMessage {
  type: "openFlowchart";
  uri: string;
  range?: AspGraphRange;
}

interface ReadSourceRangesMessage {
  type: "readSourceRanges";
  requestId: string;
  items: AspGraphSourceRangeRequestItem[];
}

type WebviewMessage = OpenRangeMessage | OpenFlowchartMessage | ReadSourceRangesMessage;

export function showAspGraphWebview(
  context: vscode.ExtensionContext,
  payload: AspGraphPayload,
  title: string,
  viewColumn: vscode.ViewColumn,
  locale: AspGraphLocale,
  theme: AspGraphWebviewThemeSetting,
  infoPanelPosition: AspGraphInfoPanelPosition,
  openFlowchart: (uri: string, range?: AspGraphRange) => Promise<void>,
): void {
  const webviewRoot = vscode.Uri.joinPath(context.extensionUri, "dist", "webview");
  const panel = vscode.window.createWebviewPanel("aspLsp.graph", title, viewColumn, {
    enableScripts: true,
    retainContextWhenHidden: true,
    localResourceRoots: [webviewRoot],
  });
  panel.webview.onDidReceiveMessage((message: WebviewMessage) => {
    if (message.type === "openRange") {
      void openGraphRange(message.uri, message.range);
    } else if (message.type === "openFlowchart") {
      void openFlowchart(message.uri, message.range);
    } else if (message.type === "readSourceRanges") {
      void readGraphSourceRanges(panel.webview, message, locale);
    }
  });
  panel.webview.html = graphWebviewHtml(
    panel.webview,
    webviewRoot,
    payload,
    title,
    locale,
    theme,
    infoPanelPosition,
  );
}

async function openGraphRange(uriText: string, range: AspGraphRange | undefined): Promise<void> {
  const uri = vscode.Uri.parse(uriText);
  const selection = range ? toVscodeRange(range) : undefined;
  await vscode.window.showTextDocument(uri, {
    preview: true,
    selection,
  });
}

async function readGraphSourceRanges(
  webview: vscode.Webview,
  message: ReadSourceRangesMessage,
  locale: AspGraphLocale,
): Promise<void> {
  const items = await Promise.all(message.items.map((item) => readGraphSourceRange(item, locale)));
  await webview.postMessage({ type: "sourceRanges", requestId: message.requestId, items });
}

async function readGraphSourceRange(
  item: AspGraphSourceRangeRequestItem,
  locale: AspGraphLocale,
): Promise<AspGraphSourceRangeResponseItem> {
  try {
    const uri = vscode.Uri.parse(item.uri);
    const document = await textDocumentForGraphSource(uri);
    if (!item.range) {
      return {
        id: item.id,
        uri: item.uri,
        fileName: graphSourceFileName(document),
        error: graphHostText(locale, "sourceRangeUnavailable"),
      };
    }
    const displayRange = displayRangeForGraphSource(document, toVscodeRange(item.range));
    return {
      id: item.id,
      uri: item.uri,
      fileName: graphSourceFileName(document),
      text: document.getText(displayRange),
      range: fromVscodeRange(displayRange),
      highlightRange: item.highlightRange,
    };
  } catch (error) {
    return {
      id: item.id,
      uri: item.uri,
      range: item.range,
      highlightRange: item.highlightRange,
      error: error instanceof Error ? error.message : String(error),
    };
  }
}

async function textDocumentForGraphSource(uri: vscode.Uri): Promise<vscode.TextDocument> {
  return (
    vscode.workspace.textDocuments.find((document) => document.uri.toString() === uri.toString()) ??
    (await vscode.workspace.openTextDocument(uri))
  );
}

function graphSourceFileName(document: vscode.TextDocument): string {
  return displayPathForUri(document.uri);
}

function displayRangeForGraphSource(
  document: vscode.TextDocument,
  range: vscode.Range,
): vscode.Range {
  const startLine = clampedLine(document, range.start.line);
  const endLine = clampedLine(document, Math.max(range.start.line, range.end.line));
  return new vscode.Range(
    document.lineAt(startLine).range.start,
    document.lineAt(endLine).range.end,
  );
}

function clampedLine(document: vscode.TextDocument, line: number): number {
  return Math.max(0, Math.min(document.lineCount - 1, line));
}

function toVscodeRange(range: AspGraphRange): vscode.Range {
  return new vscode.Range(
    range.start.line,
    range.start.character,
    range.end.line,
    range.end.character,
  );
}

function fromVscodeRange(range: vscode.Range): AspGraphRange {
  return {
    start: { line: range.start.line, character: range.start.character },
    end: { line: range.end.line, character: range.end.character },
  };
}

function graphWebviewHtml(
  webview: vscode.Webview,
  webviewRoot: vscode.Uri,
  payload: AspGraphPayload,
  title: string,
  locale: AspGraphLocale,
  theme: AspGraphWebviewThemeSetting,
  infoPanelPosition: AspGraphInfoPanelPosition,
): string {
  const nonce = nonceString();
  const scriptUri = webview.asWebviewUri(vscode.Uri.joinPath(webviewRoot, "include-graph.js"));
  const graphJson = JSON.stringify(
    graphPayloadForWebview(payload, locale, theme, infoPanelPosition),
  ).replaceAll("</", "<\\/");
  return `<!doctype html>
<html lang="${locale}">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src ${webview.cspSource} data:; connect-src ${webview.cspSource} data:; style-src ${webview.cspSource} 'unsafe-inline'; script-src 'nonce-${nonce}' 'wasm-unsafe-eval';">
  <title>${escapeHtml(title)}</title>
</head>
<body>
  <div id="root"></div>
  <script nonce="${nonce}">window.__ASP_LSP_GRAPH__ = ${graphJson};</script>
  <script nonce="${nonce}" src="${scriptUri}"></script>
</body>
</html>`;
}

function graphPayloadForWebview(
  payload: AspGraphPayload,
  locale: AspGraphLocale,
  theme: AspGraphWebviewThemeSetting,
  infoPanelPosition: AspGraphInfoPanelPosition,
): AspGraphPayload {
  return {
    ...payload,
    locale,
    nodes: payload.nodes.map((node) => ({
      ...node,
      displayPath: displayPathForUriText(node.uri) ?? node.fileName,
    })),
    links: payload.links.map((link) => ({
      ...link,
      ranges: link.ranges.map((range) => ({
        ...range,
        displayPath: displayPathForUriText(range.uri),
      })),
      include: link.include
        ? {
            ...link.include,
            actualPath: displayPathForPathOrUri(link.include.actualPath),
            displayPath: displayPathForUriText(link.include.resolvedUri),
          }
        : undefined,
    })),
    settings: { ...payload.settings, theme, infoPanelPosition },
  };
}

function nonceString(): string {
  const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789";
  let value = "";
  for (let index = 0; index < 32; index += 1) {
    value += alphabet[Math.floor(Math.random() * alphabet.length)];
  }
  return value;
}

function escapeHtml(value: string): string {
  return value
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;");
}

function graphHostText(locale: AspGraphLocale, key: "sourceRangeUnavailable"): string {
  const messages: Record<AspGraphLocale, Record<"sourceRangeUnavailable", string>> = {
    en: {
      sourceRangeUnavailable: "Source range is unavailable.",
    },
    ja: {
      sourceRangeUnavailable: "source range は利用できません。",
    },
  };
  return messages[locale][key] ?? messages.en[key];
}
