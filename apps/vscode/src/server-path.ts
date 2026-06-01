import fs from "node:fs";
import path from "node:path";

export interface ExtensionPathResolver {
  asAbsolutePath(relativePath: string): string;
}

export type ServerLaunchPath =
  | { kind: "binary"; path: string }
  | { kind: "nodeModule"; path: string };

export interface ServerLaunchOptions {
  useLegacyServer?: boolean;
}

export function getServerLaunchPath(
  context: ExtensionPathResolver,
  options: ServerLaunchOptions = {},
): ServerLaunchPath {
  if (options.useLegacyServer) {
    return { kind: "nodeModule", path: getServerModulePath(context) };
  }

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

  return { kind: "nodeModule", path: getServerModulePath(context) };
}

export function getServerModulePath(context: ExtensionPathResolver): string {
  const bundled = context.asAbsolutePath(
    path.join("server", "language-server", "dist", "server.js"),
  );
  if (fs.existsSync(bundled)) {
    return bundled;
  }
  return context.asAbsolutePath(
    path.join("node_modules", "@asp-lsp", "language-server", "dist", "server.js"),
  );
}

function currentPlatformTarget(): string {
  return `${process.platform}-${process.arch}`;
}

function serverExecutableName(): string {
  return process.platform === "win32" ? "asp-lsp-server.exe" : "asp-lsp-server";
}
