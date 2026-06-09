import * as vscode from "vscode";
import type { AspFlowchartPayload, AspFlowchartNode, AspFlowchartInclude } from "@asp-lsp/core";

export type AspFlowchartLocale = "en" | "ja";

interface FlowchartPayload extends AspFlowchartPayload {
  locale?: AspFlowchartLocale;
}

interface OpenRangeMessage {
  type: "openRange";
  uri: string;
  range?: AspFlowchartNode["range"];
}

interface OpenIncludeFlowchartMessage {
  type: "openIncludeFlowchart";
  uri: string;
}

type WebviewMessage = OpenRangeMessage | OpenIncludeFlowchartMessage;

export function showAspFlowchartWebview(
  context: vscode.ExtensionContext,
  payload: AspFlowchartPayload,
  title: string,
  viewColumn: vscode.ViewColumn,
  locale: AspFlowchartLocale,
  loadPayload: (uri: string) => Promise<{ payload: AspFlowchartPayload; title: string }>,
): void {
  const webviewRoot = vscode.Uri.joinPath(context.extensionUri, "dist", "webview");
  const panel = vscode.window.createWebviewPanel("aspLsp.flowchart", title, viewColumn, {
    enableScripts: true,
    retainContextWhenHidden: true,
    localResourceRoots: [webviewRoot],
  });
  panel.webview.onDidReceiveMessage((message: WebviewMessage) => {
    if (message.type === "openRange") {
      void openFlowchartRange(message.uri, message.range);
    } else if (message.type === "openIncludeFlowchart") {
      void loadIncludeFlowchart(panel, message.uri, locale, loadPayload);
    }
  });
  panel.webview.html = flowchartWebviewHtml(panel.webview, webviewRoot, payload, title, locale);
}

async function loadIncludeFlowchart(
  panel: vscode.WebviewPanel,
  uri: string,
  locale: AspFlowchartLocale,
  loadPayload: (uri: string) => Promise<{ payload: AspFlowchartPayload; title: string }>,
): Promise<void> {
  const result = await loadPayload(uri);
  panel.title = result.title;
  await panel.webview.postMessage({
    type: "flowchartPayload",
    payload: flowchartPayloadForWebview(result.payload, locale),
  });
}

async function openFlowchartRange(
  uriText: string,
  range: AspFlowchartNode["range"] | undefined,
): Promise<void> {
  const uri = vscode.Uri.parse(uriText);
  const selection = range ? toVscodeRange(range) : undefined;
  await vscode.window.showTextDocument(uri, {
    preview: true,
    selection,
  });
}

function toVscodeRange(range: NonNullable<AspFlowchartNode["range"]>): vscode.Range {
  return new vscode.Range(
    range.start.line,
    range.start.character,
    range.end.line,
    range.end.character,
  );
}

function flowchartWebviewHtml(
  webview: vscode.Webview,
  webviewRoot: vscode.Uri,
  payload: AspFlowchartPayload,
  title: string,
  locale: AspFlowchartLocale,
): string {
  const nonce = nonceString();
  const scriptUri = webview.asWebviewUri(vscode.Uri.joinPath(webviewRoot, "flowchart.js"));
  const flowchartJson = JSON.stringify(flowchartPayloadForWebview(payload, locale)).replaceAll(
    "</",
    "<\\/",
  );
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
  <script nonce="${nonce}">window.__ASP_LSP_FLOWCHART__ = ${flowchartJson};</script>
  <script nonce="${nonce}" src="${scriptUri}"></script>
</body>
</html>`;
}

function flowchartPayloadForWebview(
  payload: AspFlowchartPayload,
  locale: AspFlowchartLocale,
): FlowchartPayload {
  return { ...payload, locale };
}

function nonceString(): string {
  const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789";
  let value = "";
  for (let index = 0; index < 32; index++) {
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

export type { AspFlowchartPayload, AspFlowchartInclude };
