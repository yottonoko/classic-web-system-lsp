import fs from "node:fs";
import path from "node:path";

export interface ExtensionPathResolver {
  asAbsolutePath(relativePath: string): string;
}

export type ServerLaunchPath = { kind: "binary"; path: string };

export function getServerLaunchPath(context: ExtensionPathResolver): ServerLaunchPath {
  const bundledBinary = context.asAbsolutePath(
    path.join("server", "bin", currentPlatformTarget(), serverExecutableName()),
  );
  if (fs.existsSync(bundledBinary)) {
    return { kind: "binary", path: bundledBinary };
  }

  const devBinary = context.asAbsolutePath(
    path.join("..", "..", "target", "release", serverExecutableName()),
  );
  if (fs.existsSync(devBinary)) {
    return { kind: "binary", path: devBinary };
  }

  throw new Error(
    `Rust language server binary not found. Run \`pnpm run build:server\` or install a platform VSIX for ${currentPlatformTarget()}.`,
  );
}

function currentPlatformTarget(): string {
  return `${process.platform}-${process.arch}`;
}

function serverExecutableName(): string {
  return process.platform === "win32" ? "asp-lsp-server.exe" : "asp-lsp-server";
}
