import { randomBytes } from "node:crypto";
import * as vscode from "vscode";
import { extensionLocalizerForLocale } from "./extension-localization";

/** Open the command-only, local log analysis tool without starting an LSP request. */
export function showLogAnalysisWebview(
  context: vscode.ExtensionContext,
  locale: "ja" | "en",
): void {
  const root = vscode.Uri.joinPath(context.extensionUri, "dist", "webview");
  const title = extensionLocalizerForLocale(locale)("logAnalysis.title");
  const panel = vscode.window.createWebviewPanel(
    "aspLsp.logAnalysis",
    title,
    vscode.ViewColumn.Active,
    { enableScripts: true, localResourceRoots: [root], retainContextWhenHidden: true },
  );
  const nonce = randomBytes(24).toString("hex");
  const script = panel.webview.asWebviewUri(vscode.Uri.joinPath(root, "log-analysis.js"));
  panel.webview.html = `<!doctype html><html lang="${locale}"><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; script-src 'nonce-${nonce}';"><title>${title}</title></head><body><div id="root"></div><script nonce="${nonce}" src="${script}"></script></body></html>`;
  context.subscriptions.push(panel);
}
