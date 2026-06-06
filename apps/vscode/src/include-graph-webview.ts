import * as vscode from "vscode";

export interface AspGraphPayload {
  scope: "document" | "workspace";
  rootUri?: string;
  nodes: AspGraphNode[];
  links: AspGraphLink[];
  settings?: {
    hideSingleNodes?: boolean;
    hiddenNodeCategories?: AspGraphNodeCategory[];
    hiddenLinkCategories?: AspGraphLinkFilterCategory[];
  };
  stats: {
    files: number;
    declarations: number;
    references: number;
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
  | "function"
  | "sub"
  | "class"
  | "method"
  | "methodFunction"
  | "methodSub"
  | "property"
  | "member"
  | "globalVariable"
  | "globalConstant"
  | "localVariable"
  | "localConstant"
  | "parameter"
  | "unresolved";

export type AspGraphLinkFilterCategory = AspGraphLink["kind"] | "member";

export interface AspGraphNode {
  id: string;
  kind: "file" | "vbDeclaration" | "vbUnresolved";
  label: string;
  uri?: string;
  fileName?: string;
  range?: AspGraphRange;
  exists?: boolean;
  declarationKind?: string;
  role?: string;
  memberOf?: string;
  bindingScope?: string;
  procedureKind?: string;
  group?: string;
  origin?: "source" | "builtin" | "configured";
  externalKind?: "function" | "constant" | "object" | "member" | "event";
  isRoot?: boolean;
}

export interface AspGraphLink {
  id: string;
  source: string;
  target: string;
  kind: "include" | "declares" | "references" | "calls" | "unresolvedReference";
  label: string;
  role?: string;
  count: number;
  ranges: Array<{ uri: string; range: AspGraphRange }>;
  include?: {
    path: string;
    mode: "file" | "virtual";
    exists: boolean;
    resolvedUri: string;
    actualPath?: string;
    pathCaseMatches?: boolean;
  };
}

export interface AspGraphRange {
  start: { line: number; character: number };
  end: { line: number; character: number };
}

interface OpenRangeMessage {
  type: "openRange";
  uri: string;
  range?: AspGraphRange;
}

type WebviewMessage = OpenRangeMessage;

export function showAspGraphWebview(
  context: vscode.ExtensionContext,
  payload: AspGraphPayload,
  title: string,
  viewColumn: vscode.ViewColumn,
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
    }
  });
  panel.webview.html = graphWebviewHtml(panel.webview, webviewRoot, payload, title);
}

async function openGraphRange(uriText: string, range: AspGraphRange | undefined): Promise<void> {
  const uri = vscode.Uri.parse(uriText);
  const selection = range ? toVscodeRange(range) : undefined;
  await vscode.window.showTextDocument(uri, {
    preview: true,
    selection,
  });
}

function toVscodeRange(range: AspGraphRange): vscode.Range {
  return new vscode.Range(
    range.start.line,
    range.start.character,
    range.end.line,
    range.end.character,
  );
}

function graphWebviewHtml(
  webview: vscode.Webview,
  webviewRoot: vscode.Uri,
  payload: AspGraphPayload,
  title: string,
): string {
  const nonce = nonceString();
  const scriptUri = webview.asWebviewUri(vscode.Uri.joinPath(webviewRoot, "include-graph.js"));
  const graphJson = JSON.stringify(payload).replaceAll("</", "<\\/");
  return `<!doctype html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src ${webview.cspSource} data:; style-src ${webview.cspSource} 'unsafe-inline'; script-src 'nonce-${nonce}';">
  <title>${escapeHtml(title)}</title>
</head>
<body>
  <div id="root"></div>
  <script nonce="${nonce}">window.__ASP_LSP_GRAPH__ = ${graphJson};</script>
  <script nonce="${nonce}" src="${scriptUri}"></script>
</body>
</html>`;
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
