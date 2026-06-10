import path from "node:path";
import * as vscode from "vscode";
import type {
  AspFlowchartPayload,
  AspFlowchartNode,
  AspFlowchartInclude,
  AspFlowchartTarget,
} from "@asp-lsp/core";

export type AspFlowchartLocale = "en" | "ja";
export type AspFlowchartWebviewTheme = "light" | "dark";
export type AspFlowchartWebviewThemeSetting = AspFlowchartWebviewTheme | "auto";

export interface AspFlowchartWebviewSettings {
  maxTextSize: number;
  theme: AspFlowchartWebviewThemeSetting;
}

interface FlowchartPayload extends AspFlowchartPayload {
  locale?: AspFlowchartLocale;
  settings?: AspFlowchartWebviewSettings;
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

interface OpenFlowchartLocationMessage {
  type: "openFlowchartLocation";
  uri: string;
  range?: AspFlowchartTarget["range"];
}

interface ExportFlowchartMessage {
  type: "exportFlowchart";
  format: "mermaid" | "svg";
  uri: string;
  sectionLabel?: string;
  content: string;
}

interface CopyTextMessage {
  type: "copyText";
  content: string;
}

type WebviewMessage =
  | OpenRangeMessage
  | OpenIncludeFlowchartMessage
  | OpenFlowchartLocationMessage
  | ExportFlowchartMessage
  | CopyTextMessage;

export function showAspFlowchartWebview(
  context: vscode.ExtensionContext,
  payload: AspFlowchartPayload,
  title: string,
  viewColumn: vscode.ViewColumn,
  locale: AspFlowchartLocale,
  settings: AspFlowchartWebviewSettings,
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
      void loadFlowchartLocation(panel, message.uri, undefined, locale, settings, loadPayload);
    } else if (message.type === "openFlowchartLocation") {
      void loadFlowchartLocation(panel, message.uri, message.range, locale, settings, loadPayload);
    } else if (message.type === "exportFlowchart") {
      void exportFlowchartContent(message, locale);
    } else if (message.type === "copyText") {
      void copyFlowchartText(message.content, locale);
    }
  });
  panel.webview.html = flowchartWebviewHtml(
    panel.webview,
    webviewRoot,
    payload,
    title,
    locale,
    settings,
  );
}

async function copyFlowchartText(content: string, locale: AspFlowchartLocale): Promise<void> {
  await vscode.env.clipboard.writeText(content);
  void vscode.window.showInformationMessage(flowchartHostText(locale, "copied"));
}

async function loadFlowchartLocation(
  panel: vscode.WebviewPanel,
  uri: string,
  targetRange: AspFlowchartTarget["range"] | undefined,
  locale: AspFlowchartLocale,
  settings: AspFlowchartWebviewSettings,
  loadPayload: (uri: string) => Promise<{ payload: AspFlowchartPayload; title: string }>,
): Promise<void> {
  const result = await loadPayload(uri);
  panel.title = result.title;
  await panel.webview.postMessage({
    type: "flowchartPayload",
    payload: flowchartPayloadForWebview(result.payload, locale, settings),
    targetRange,
  });
}

async function exportFlowchartContent(
  message: ExportFlowchartMessage,
  locale: AspFlowchartLocale,
): Promise<void> {
  const extension = message.format === "svg" ? "svg" : "mmd";
  const target = await vscode.window.showSaveDialog({
    defaultUri: flowchartExportDefaultUri(message.uri, message.sectionLabel, extension),
    filters:
      message.format === "svg"
        ? { "SVG Image": ["svg"] }
        : { "Mermaid Diagram": ["mmd"], "Plain Text": ["txt"] },
    saveLabel: flowchartHostText(locale, "saveLabel"),
  });
  if (!target) {
    return;
  }
  await vscode.workspace.fs.writeFile(target, Buffer.from(message.content, "utf8"));
  void vscode.window.showInformationMessage(
    flowchartHostText(locale, "exported").replace("{file}", target.fsPath),
  );
}

function flowchartExportDefaultUri(
  uriText: string,
  sectionLabel: string | undefined,
  extension: string,
): vscode.Uri | undefined {
  const base = `${flowchartExportBaseName(uriText)}-${sanitizeFileName(sectionLabel ?? "flowchart")}.${extension}`;
  if (!uriText.startsWith("file://")) {
    return undefined;
  }
  const uri = vscode.Uri.parse(uriText);
  return vscode.Uri.file(path.join(path.dirname(uri.fsPath), base));
}

function flowchartExportBaseName(uriText: string): string {
  try {
    return sanitizeFileName(
      path.basename(vscode.Uri.parse(uriText).fsPath).replace(/\.[^.]+$/, ""),
    );
  } catch {
    return "flowchart";
  }
}

function sanitizeFileName(value: string): string {
  const sanitized = value.replace(/[\\/:*?"<>|]+/g, "-").replace(/\s+/g, "-");
  return sanitized || "flowchart";
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
  settings: AspFlowchartWebviewSettings,
): string {
  const nonce = nonceString();
  const scriptUri = webview.asWebviewUri(vscode.Uri.joinPath(webviewRoot, "flowchart.js"));
  const flowchartJson = JSON.stringify(
    flowchartPayloadForWebview(payload, locale, settings),
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
  <script nonce="${nonce}">window.__ASP_LSP_FLOWCHART__ = ${flowchartJson};</script>
  <script nonce="${nonce}" src="${scriptUri}"></script>
</body>
</html>`;
}

function flowchartPayloadForWebview(
  payload: AspFlowchartPayload,
  locale: AspFlowchartLocale,
  settings: AspFlowchartWebviewSettings,
): FlowchartPayload {
  return { ...payload, locale, settings };
}

function flowchartHostText(
  locale: AspFlowchartLocale,
  key: "saveLabel" | "exported" | "copied",
): string {
  const messages = {
    en: {
      saveLabel: "Export",
      exported: "Exported flowchart to {file}.",
      copied: "Copied Mermaid flowchart.",
    },
    ja: {
      saveLabel: "出力",
      exported: "フローチャートを {file} に出力しました。",
      copied: "Mermaid フローチャートをコピーしました。",
    },
  };
  return messages[locale][key] ?? messages.en[key];
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
