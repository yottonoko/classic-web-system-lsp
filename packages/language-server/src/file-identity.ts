import path from "node:path";
import { fileURLToPath } from "node:url";
import { sourceUriIdentityKey } from "@asp-lsp/core";

export function fileIdentityKeyFromFileName(fileName: string): string {
  return fileSystemIdentityKey(path.resolve(fileName));
}

export function fileIdentityKeyFromUri(uri: string): string {
  if (!uri.startsWith("file://")) {
    return sourceUriIdentityKey(uri);
  }
  try {
    return fileIdentityKeyFromFileName(fileURLToPath(uri));
  } catch {
    return sourceUriIdentityKey(uri);
  }
}

export function sameFileIdentityUri(left: string, right: string): boolean {
  return fileIdentityKeyFromUri(left) === fileIdentityKeyFromUri(right);
}

function fileSystemIdentityKey(fileName: string): string {
  const slashPath = fileName.replace(/\\/g, "/");
  const withoutLeadingDriveSlash = slashPath.replace(/^\/([A-Za-z]:)(?=\/|$)/, "$1");
  const windowsLike =
    process.platform === "win32" ||
    /^[A-Za-z]:(?:\/|$)/.test(withoutLeadingDriveSlash) ||
    /^\/[A-Za-z]:(?:\/|$)/.test(slashPath) ||
    slashPath.startsWith("//");
  return windowsLike ? withoutLeadingDriveSlash.toLowerCase() : fileName;
}
