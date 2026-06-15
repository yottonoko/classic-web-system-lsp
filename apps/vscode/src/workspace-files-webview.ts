import * as vscode from "vscode";
import { displayPathForUriText } from "./path-display";

export type WorkspaceFilesLocale = "en" | "ja";
export type WorkspaceFilesTheme = "light" | "dark";
export type WorkspaceFilesThemeSetting = WorkspaceFilesTheme | "auto";

export interface WorkspaceFilesPayload {
  locale?: WorkspaceFilesLocale;
  includeGlobs: string[];
  excludeGlobs: string[];
  globStats?: WorkspaceFilesGlobStats;
  respectGitIgnore: boolean;
  roots: WorkspaceFilesRoot[];
  showUnmatched: boolean;
  stats: {
    files: number;
    totalBytes: number;
  };
  truncated?: {
    reason: string;
  };
  settings?: {
    theme?: WorkspaceFilesThemeSetting;
  };
}

export interface WorkspaceFilesGlobStats {
  include: WorkspaceFilesGlobStat[];
  exclude: WorkspaceFilesGlobStat[];
}

export interface WorkspaceFilesGlobStat {
  glob: string;
  files: number;
}

export interface WorkspaceFilesRoot {
  uri: string;
  fileName: string;
  name: string;
  displayPath?: string;
  files: WorkspaceFilesFile[];
}

export interface WorkspaceFilesFile {
  uri: string;
  fileName: string;
  matchesFilter: boolean;
  relativePath: string;
  displayPath?: string;
  size: number;
  mtimeMs: number;
}

export interface WorkspaceFilesPreviewRequest {
  includeGlobs: string[];
  excludeGlobs: string[];
  respectGitIgnore: boolean;
  showUnmatched: boolean;
}

export interface WorkspaceFilesSelectedExportRequest extends WorkspaceFilesPreviewRequest {
  selectedUri: string;
}

interface PreviewMessage extends WorkspaceFilesPreviewRequest {
  type: "preview";
  requestId: string;
}

interface ExportSelectedExcelMessage extends WorkspaceFilesSelectedExportRequest {
  type: "exportSelectedExcel";
}

interface OpenFileMessage {
  type: "openFile";
  uri: string;
}

interface PreviewResultHostMessage {
  type: "previewResult";
  requestId: string;
  payload?: WorkspaceFilesPayload;
  error?: string;
}

interface ExportResultHostMessage {
  type: "exportResult";
  ok: boolean;
  error?: string;
}

type WebviewMessage = PreviewMessage | ExportSelectedExcelMessage | OpenFileMessage;

export function showWorkspaceFilesWebview(
  context: vscode.ExtensionContext,
  payload: WorkspaceFilesPayload,
  title: string,
  viewColumn: vscode.ViewColumn,
  locale: WorkspaceFilesLocale,
  theme: WorkspaceFilesThemeSetting,
  handlers: {
    preview(request: WorkspaceFilesPreviewRequest): Promise<WorkspaceFilesPayload>;
    exportSelectedExcel?(request: WorkspaceFilesSelectedExportRequest): Promise<void>;
  },
): vscode.WebviewPanel {
  const webviewRoot = vscode.Uri.joinPath(context.extensionUri, "dist", "webview");
  const panel = vscode.window.createWebviewPanel("aspLsp.workspaceFiles", title, viewColumn, {
    enableScripts: true,
    retainContextWhenHidden: true,
    localResourceRoots: [webviewRoot],
  });
  panel.webview.onDidReceiveMessage((message: WebviewMessage) => {
    if (message.type === "preview") {
      void previewWorkspaceFiles(panel.webview, message, locale, theme, handlers);
    } else if (message.type === "exportSelectedExcel") {
      void exportSelectedWorkspaceFile(panel.webview, message, handlers);
    } else if (message.type === "openFile") {
      void openWorkspaceFile(message.uri);
    }
  });
  panel.webview.html = workspaceFilesWebviewHtml(
    panel.webview,
    webviewRoot,
    payloadForWebview(payload, locale, theme),
    title,
    locale,
  );
  return panel;
}

async function previewWorkspaceFiles(
  webview: vscode.Webview,
  message: PreviewMessage,
  locale: WorkspaceFilesLocale,
  theme: WorkspaceFilesThemeSetting,
  handlers: {
    preview(request: WorkspaceFilesPreviewRequest): Promise<WorkspaceFilesPayload>;
  },
): Promise<void> {
  const response: PreviewResultHostMessage = {
    type: "previewResult",
    requestId: message.requestId,
  };
  try {
    const payload = await handlers.preview(message);
    response.payload = payloadForWebview(payload, locale, theme);
  } catch (error) {
    response.error = error instanceof Error ? error.message : String(error);
  }
  await webview.postMessage(response);
}

async function exportSelectedWorkspaceFile(
  webview: vscode.Webview,
  message: ExportSelectedExcelMessage,
  handlers: {
    exportSelectedExcel?(request: WorkspaceFilesSelectedExportRequest): Promise<void>;
  },
): Promise<void> {
  const response: ExportResultHostMessage = { type: "exportResult", ok: true };
  try {
    await handlers.exportSelectedExcel?.(message);
  } catch (error) {
    response.ok = false;
    response.error = error instanceof Error ? error.message : String(error);
  }
  await webview.postMessage(response);
}

async function openWorkspaceFile(uriText: string): Promise<void> {
  const uri = vscode.Uri.parse(uriText);
  await vscode.window.showTextDocument(uri, { preview: true });
}

function workspaceFilesWebviewHtml(
  webview: vscode.Webview,
  webviewRoot: vscode.Uri,
  payload: WorkspaceFilesPayload,
  title: string,
  locale: WorkspaceFilesLocale,
): string {
  const nonce = nonceString();
  const scriptUri = webview.asWebviewUri(vscode.Uri.joinPath(webviewRoot, "workspace-files.js"));
  const payloadJson = JSON.stringify(payload).replaceAll("</", "<\\/");
  return `<!doctype html>
<html lang="${locale}">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src ${webview.cspSource} data:; style-src ${webview.cspSource} 'unsafe-inline'; script-src 'nonce-${nonce}';">
  <title>${escapeHtml(title)}</title>
</head>
<body>
  <div id="root"></div>
  <script nonce="${nonce}">window.__ASP_LSP_WORKSPACE_FILES__ = ${payloadJson};</script>
  <script nonce="${nonce}" src="${scriptUri}"></script>
</body>
</html>`;
}

function payloadForWebview(
  payload: WorkspaceFilesPayload,
  locale: WorkspaceFilesLocale,
  theme: WorkspaceFilesThemeSetting,
): WorkspaceFilesPayload {
  return {
    ...payload,
    locale,
    settings: { ...payload.settings, theme },
    roots: payload.roots.map((root) => ({
      ...root,
      displayPath: displayPathForUriText(root.uri) ?? root.name,
      files: root.files.map((file) => ({
        ...file,
        displayPath: displayPathForUriText(file.uri) ?? file.relativePath,
      })),
    })),
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
