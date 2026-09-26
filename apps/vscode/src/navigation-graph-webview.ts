import { randomBytes } from "node:crypto";
import * as vscode from "vscode";
import type { AspNavigationGraphPayload, AspNavigationEvidence } from "./protocol-types";
import { displayPathForUriText } from "./path-display";
import { uriTextForVSCode } from "./uri-encoding";
import { graphWebviewContentSecurityPolicy } from "./webview-csp";
import { extensionLocalizerForLocale } from "./extension-localization";

export type AspNavigationGraphLocale = "en" | "ja";
export type AspNavigationGraphWebviewThemeSetting = "auto" | "light" | "dark";

export interface AspNavigationGraphWebviewSettings {
  theme: AspNavigationGraphWebviewThemeSetting;
}

interface NavigationGraphPayload extends AspNavigationGraphPayload {
  locale?: AspNavigationGraphLocale;
  webviewSettings?: AspNavigationGraphWebviewSettings;
}

interface OpenRangeMessage {
  type: "openRange";
  uri: string;
  range?: AspNavigationEvidence["range"];
}

interface CopyTextMessage {
  type: "copyText";
  content: string;
}

type WebviewMessage = OpenRangeMessage | CopyTextMessage;

export function showAspNavigationGraphWebview(
  context: vscode.ExtensionContext,
  payload: AspNavigationGraphPayload,
  title: string,
  viewColumn: vscode.ViewColumn,
  locale: AspNavigationGraphLocale,
  settings: AspNavigationGraphWebviewSettings,
): vscode.WebviewPanel {
  const webviewRoot = vscode.Uri.joinPath(context.extensionUri, "dist", "webview");
  const panel = vscode.window.createWebviewPanel("aspLsp.navigationGraph", title, viewColumn, {
    enableScripts: true,
    retainContextWhenHidden: true,
    localResourceRoots: [webviewRoot],
  });
  panel.webview.onDidReceiveMessage((message: WebviewMessage) => {
    if (message.type === "openRange") {
      void openNavigationRange(message.uri, message.range).catch((error) => {
        void vscode.window.showErrorMessage(
          extensionLocalizerForLocale(locale)("navigationGraph.openFailed", {
            error: errorMessage(error),
          }),
        );
      });
    } else if (message.type === "copyText") {
      void Promise.resolve(vscode.env.clipboard.writeText(message.content)).catch(
        (error: unknown) => {
          void vscode.window.showErrorMessage(
            extensionLocalizerForLocale(locale)("navigationGraph.copyFailed", {
              error: errorMessage(error),
            }),
          );
        },
      );
    }
  });
  panel.webview.html = navigationGraphWebviewHtml(
    panel.webview,
    webviewRoot,
    navigationPayloadForWebview(payload, locale, settings),
    title,
    locale,
  );
  return panel;
}

export async function postAspNavigationGraphWebviewUpdate(
  panel: vscode.WebviewPanel,
  payload: AspNavigationGraphPayload,
  locale: AspNavigationGraphLocale,
  settings: AspNavigationGraphWebviewSettings,
): Promise<boolean> {
  return panel.webview.postMessage({
    type: "navigationGraphPayload",
    payload: navigationPayloadForWebview(payload, locale, settings),
  });
}

async function openNavigationRange(
  uriText: string,
  range: AspNavigationEvidence["range"] | undefined,
): Promise<void> {
  const uri = vscode.Uri.parse(uriTextForVSCode(uriText));
  const selection = range
    ? new vscode.Range(range.start.line, range.start.character, range.end.line, range.end.character)
    : undefined;
  await vscode.window.showTextDocument(uri, {
    preview: true,
    selection,
  });
}

function navigationPayloadForWebview(
  payload: AspNavigationGraphPayload,
  locale: AspNavigationGraphLocale,
  settings: AspNavigationGraphWebviewSettings,
): NavigationGraphPayload {
  const nodes = Array.isArray(payload.nodes) ? payload.nodes : [];
  const edges = Array.isArray(payload.edges) ? payload.edges : [];
  return {
    ...payload,
    nodes: nodes.map((node) => ({
      ...node,
      label: node.uri ? (displayPathForUriText(node.uri) ?? node.label) : node.label,
    })),
    edges: edges.map((edge) => ({
      ...edge,
      confidence: edge.confidence ?? "unknown",
      ranges: Array.isArray(edge.ranges) ? edge.ranges : [],
      evidence: Array.isArray(edge.evidence) ? edge.evidence : [],
    })),
    locale,
    webviewSettings: settings,
  };
}

function navigationGraphWebviewHtml(
  webview: vscode.Webview,
  webviewRoot: vscode.Uri,
  payload: NavigationGraphPayload,
  title: string,
  locale: AspNavigationGraphLocale,
): string {
  const nonce = nonceString();
  const scriptUri = webview.asWebviewUri(vscode.Uri.joinPath(webviewRoot, "navigation-graph.js"));
  const payloadJson = JSON.stringify(payload).replaceAll("</", "<\\/");
  return `<!doctype html>
<html lang="${locale}">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <meta http-equiv="Content-Security-Policy" content="${graphWebviewContentSecurityPolicy(webview, nonce)}">
  <title>${escapeHtml(title)}</title>
</head>
<body>
  <div id="root"></div>
  <script nonce="${nonce}">window.__ASP_LSP_NAVIGATION_GRAPH__ = ${payloadJson};</script>
  <script nonce="${nonce}" src="${scriptUri}"></script>
</body>
</html>`;
}

function nonceString(): string {
  return randomBytes(24).toString("base64");
}

function escapeHtml(value: string): string {
  return value
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;");
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

export type { AspNavigationGraphPayload };
