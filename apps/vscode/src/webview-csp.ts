import * as vscode from "vscode";

export function graphWebviewContentSecurityPolicy(webview: vscode.Webview, nonce: string): string {
  return [
    "default-src 'none'",
    `img-src ${webview.cspSource} data: blob:`,
    `font-src ${webview.cspSource} data:`,
    `connect-src ${webview.cspSource} data: blob:`,
    `style-src ${webview.cspSource} 'unsafe-inline'`,
    `script-src 'nonce-${nonce}' 'wasm-unsafe-eval' 'unsafe-eval' blob:`,
    `worker-src ${webview.cspSource} blob:`,
    `child-src ${webview.cspSource} blob:`,
  ].join("; ");
}
