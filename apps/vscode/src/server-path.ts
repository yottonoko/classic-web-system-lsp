import fs from "node:fs";
import path from "node:path";

export interface ExtensionPathResolver {
  asAbsolutePath(relativePath: string): string;
}

export type ServerRuntimePath = { kind: "go"; command: string };
export type ServerExecutableStatus = "ready" | "missing" | "invalid";

export function getServerExecutablePath(context: ExtensionPathResolver): ServerRuntimePath {
  const executableName = process.platform === "win32" ? "asp-lsp-go.exe" : "asp-lsp-go";
  const developmentGoServer = context.asAbsolutePath(path.join("..", "..", "bin", executableName));
  const bundledGoServer = context.asAbsolutePath(path.join("server", executableName));
  const candidates: ServerRuntimePath[] = [
    { kind: "go", command: developmentGoServer },
    { kind: "go", command: bundledGoServer },
  ];
  const ready = candidates.find((candidate) => serverExecutableStatus(candidate) === "ready");
  if (ready) {
    return ready;
  }
  return (
    candidates.find((candidate) => serverExecutableStatus(candidate) === "invalid") ?? candidates[0]
  );
}

export function serverExecutableIsRunnable(runtime: ServerRuntimePath): boolean {
  return serverExecutableStatus(runtime) === "ready";
}

export function serverExecutableStatus(runtime: ServerRuntimePath): ServerExecutableStatus {
  try {
    const stat = fs.statSync(runtime.command);
    if (!stat.isFile()) {
      return "invalid";
    }
    return process.platform === "win32" || (stat.mode & 0o111) !== 0 ? "ready" : "invalid";
  } catch (error) {
    const code = (error as NodeJS.ErrnoException).code;
    return code === "ENOENT" || code === "ENOTDIR" ? "missing" : "invalid";
  }
}
