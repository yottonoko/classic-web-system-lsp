import { randomBytes } from "node:crypto";
import path from "node:path";
import * as vscode from "vscode";
import type {
  AspFlowchartPayload,
  AspFlowchartNode,
  AspFlowchartInclude,
  AspFlowchartLabelMode,
  AspFlowchartTarget,
} from "./protocol-types";
import { displayPathForPathOrUri, displayPathForUriText } from "./path-display";
import { uriTextForVSCode } from "./uri-encoding";
import { graphWebviewContentSecurityPolicy } from "./webview-csp";
import { extensionLocalizerForLocale } from "./extension-localization";

export type AspFlowchartLocale = "en" | "ja";
export type { AspFlowchartLabelMode };
export type AspFlowchartWebviewTheme = "light" | "dark";
export type AspFlowchartWebviewThemeSetting = AspFlowchartWebviewTheme | "auto";
export type AspFlowchartInfoPanelPosition = "left" | "right";

export interface AspFlowchartWebviewSettings {
  labelLineLength: number;
  labelMode: AspFlowchartLabelMode;
  minZoom: number;
  maxZoom: number;
  theme: AspFlowchartWebviewThemeSetting;
  infoPanelPosition: AspFlowchartInfoPanelPosition;
  showSourcePanel: boolean;
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
  labelMode?: AspFlowchartLabelMode;
}

interface OpenFlowchartLocationMessage {
  type: "openFlowchartLocation";
  uri: string;
  range?: AspFlowchartTarget["range"];
  labelMode?: AspFlowchartLabelMode;
}

interface ReloadFlowchartMessage {
  type: "reloadFlowchart";
  uri: string;
  labelMode?: AspFlowchartLabelMode;
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
  | ReloadFlowchartMessage
  | ExportFlowchartMessage
  | CopyTextMessage;

export function showAspFlowchartWebview(
  context: vscode.ExtensionContext,
  payload: AspFlowchartPayload,
  title: string,
  viewColumn: vscode.ViewColumn,
  locale: AspFlowchartLocale,
  settings: AspFlowchartWebviewSettings,
  loadPayload: (
    uri: string,
    labelMode?: AspFlowchartLabelMode,
  ) => Promise<{ payload: AspFlowchartPayload; title: string }>,
  initialTargetRange?: AspFlowchartTarget["range"],
): void {
  const webviewRoot = vscode.Uri.joinPath(context.extensionUri, "dist", "webview");
  const panel = vscode.window.createWebviewPanel("aspLsp.flowchart", title, viewColumn, {
    enableScripts: true,
    retainContextWhenHidden: true,
    localResourceRoots: [webviewRoot],
  });
  let loadGeneration = 0;
  const loadLocation = (
    uri: string,
    targetRange: AspFlowchartTarget["range"] | undefined,
    labelMode: AspFlowchartLabelMode | undefined,
  ): void => {
    const generation = ++loadGeneration;
    void loadFlowchartLocation(
      panel,
      uri,
      targetRange,
      locale,
      settings,
      loadPayload,
      () => generation === loadGeneration,
      labelMode,
    );
  };
  panel.onDidDispose(() => {
    loadGeneration += 1;
  });
  panel.webview.onDidReceiveMessage((message: WebviewMessage) => {
    if (message.type === "openRange") {
      void openFlowchartRange(message.uri, message.range).catch((error) => {
        void vscode.window.showErrorMessage(
          extensionLocalizerForLocale(locale)("flowchart.openFailed", {
            error: errorMessage(error),
          }),
        );
      });
    } else if (message.type === "openIncludeFlowchart") {
      loadLocation(message.uri, undefined, message.labelMode);
    } else if (message.type === "openFlowchartLocation") {
      loadLocation(message.uri, message.range, message.labelMode);
    } else if (message.type === "reloadFlowchart") {
      loadLocation(message.uri, undefined, message.labelMode);
    } else if (message.type === "exportFlowchart") {
      void exportFlowchartContent(message, locale);
    } else if (message.type === "copyText") {
      void copyFlowchartText(message.content, locale).catch((error) => {
        void vscode.window.showErrorMessage(
          extensionLocalizerForLocale(locale)("flowchart.copyFailed", {
            error: errorMessage(error),
          }),
        );
      });
    }
  });
  panel.webview.html = flowchartWebviewHtml(
    panel.webview,
    webviewRoot,
    payload,
    title,
    locale,
    settings,
    initialTargetRange,
  );
}

async function copyFlowchartText(content: string, locale: AspFlowchartLocale): Promise<void> {
  await vscode.env.clipboard.writeText(content);
  void vscode.window.showInformationMessage(
    extensionLocalizerForLocale(locale)("flowchart.copied"),
  );
}

async function loadFlowchartLocation(
  panel: vscode.WebviewPanel,
  uri: string,
  targetRange: AspFlowchartTarget["range"] | undefined,
  locale: AspFlowchartLocale,
  settings: AspFlowchartWebviewSettings,
  loadPayload: (
    uri: string,
    labelMode?: AspFlowchartLabelMode,
  ) => Promise<{ payload: AspFlowchartPayload; title: string }>,
  isCurrent: () => boolean,
  labelMode?: AspFlowchartLabelMode,
): Promise<void> {
  try {
    const result = await loadPayload(uri, labelMode);
    if (!isCurrent()) {
      return;
    }
    panel.title = result.title;
    await panel.webview.postMessage({
      type: "flowchartPayload",
      payload: flowchartPayloadForWebview(result.payload, locale, settings),
      targetRange,
    });
  } catch (error) {
    if (!isCurrent()) {
      return;
    }
    void vscode.window.showErrorMessage(
      extensionLocalizerForLocale(locale)("flowchart.openFailed", { error: errorMessage(error) }),
    );
  }
}

async function exportFlowchartContent(
  message: ExportFlowchartMessage,
  locale: AspFlowchartLocale,
): Promise<void> {
  try {
    const content = flowchartExportMessageContent(message);
    if (!content.trim()) {
      void vscode.window.showErrorMessage(
        extensionLocalizerForLocale(locale)("flowchart.exportEmpty"),
      );
      return;
    }
    const extension = message.format === "svg" ? "svg" : "mmd";
    const target = await vscode.window.showSaveDialog({
      defaultUri: flowchartExportDefaultUri(message.uri, message.sectionLabel, extension),
      filters:
        message.format === "svg"
          ? { "SVG Image": ["svg"] }
          : { "Mermaid Diagram": ["mmd"], "Plain Text": ["txt"] },
      saveLabel: extensionLocalizerForLocale(locale)("flowchart.saveLabel"),
    });
    if (!target) {
      return;
    }
    await vscode.workspace.fs.writeFile(target, new TextEncoder().encode(content));
    void vscode.window.showInformationMessage(
      extensionLocalizerForLocale(locale)("flowchart.exported", { file: target.fsPath }),
    );
  } catch (error) {
    void vscode.window.showErrorMessage(
      extensionLocalizerForLocale(locale)("flowchart.exportFailed", { error: errorMessage(error) }),
    );
  }
}

function flowchartExportMessageContent(message: ExportFlowchartMessage): string {
  const content = message.content.trim();
  if (message.format !== "svg" || !content) {
    return content ? `${content}\n` : "";
  }
  return content.startsWith("<?xml")
    ? `${content}\n`
    : `<?xml version="1.0" encoding="UTF-8"?>\n${content}\n`;
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
  const uri = vscode.Uri.parse(uriTextForVSCode(uriText));
  return vscode.Uri.file(path.join(path.dirname(uri.fsPath), base));
}

function flowchartExportBaseName(uriText: string): string {
  try {
    return sanitizeFileName(
      path.basename(vscode.Uri.parse(uriTextForVSCode(uriText)).fsPath).replace(/\.[^.]+$/, ""),
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
  const uri = vscode.Uri.parse(uriTextForVSCode(uriText));
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
  initialTargetRange?: AspFlowchartTarget["range"],
): string {
  const nonce = nonceString();
  const scriptUri = webview.asWebviewUri(vscode.Uri.joinPath(webviewRoot, "flowchart.js"));
  const flowchartJson = JSON.stringify(
    flowchartPayloadForWebview(payload, locale, settings),
  ).replaceAll("</", "<\\/");
  const targetRangeJson = JSON.stringify(initialTargetRange ?? null).replaceAll("</", "<\\/");
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
  <script nonce="${nonce}">window.__ASP_LSP_FLOWCHART__ = ${flowchartJson}; window.__ASP_LSP_FLOWCHART_TARGET_RANGE__ = ${targetRangeJson};</script>
  <script nonce="${nonce}" src="${scriptUri}"></script>
</body>
</html>`;
}

function flowchartPayloadForWebview(
  payload: AspFlowchartPayload,
  locale: AspFlowchartLocale,
  settings: AspFlowchartWebviewSettings,
): FlowchartPayload {
  const sections = Array.isArray(payload.sections)
    ? payload.sections.map((section) => ({
        ...section,
        nodeIds: Array.isArray(section.nodeIds) ? section.nodeIds : [],
      }))
    : [];
  const nodes = Array.isArray(payload.nodes) ? payload.nodes : [];
  const edges = Array.isArray(payload.edges) ? payload.edges : [];
  const includes = Array.isArray(payload.includes) ? payload.includes : [];
  return {
    ...payload,
    fileName: displayPathForUriText(payload.uri) ?? payload.fileName,
    sections,
    nodes,
    edges,
    includes: includes.map((include) => ({
      ...include,
      actualPath: displayPathForPathOrUri(include.actualPath),
    })),
    locale,
    settings,
  };
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
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

export type { AspFlowchartPayload, AspFlowchartInclude };
