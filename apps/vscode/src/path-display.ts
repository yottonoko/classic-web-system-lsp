import path from "node:path";
import * as vscode from "vscode";

export function displayPathForUri(uri: vscode.Uri): string {
  if (uri.scheme !== "file") {
    return uri.toString();
  }
  const workspaceFolder = vscode.workspace.getWorkspaceFolder(uri);
  if (!workspaceFolder) {
    return uri.fsPath;
  }
  const relative = path.relative(workspaceFolder.uri.fsPath, uri.fsPath);
  return relative || path.basename(uri.fsPath);
}

export function displayPathForUriText(uriText: string | undefined): string | undefined {
  if (!uriText) {
    return undefined;
  }
  try {
    return displayPathForUri(vscode.Uri.parse(uriText));
  } catch {
    return uriText;
  }
}

export function displayPathForPathOrUri(value: string | undefined): string | undefined {
  if (!value) {
    return undefined;
  }
  if (value.startsWith("file://")) {
    return displayPathForUriText(value);
  }
  if (path.isAbsolute(value)) {
    return displayPathForUri(vscode.Uri.file(value));
  }
  return value;
}
