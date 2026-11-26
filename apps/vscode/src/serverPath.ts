import * as fs from "node:fs";
import * as path from "node:path";

export interface ResolveServerPathOptions {
  configuredPath?: string;
  extensionPath: string;
  env?: NodeJS.ProcessEnv;
  platform?: NodeJS.Platform;
  fileExists?: (candidate: string) => boolean;
}

/**
 * Resolves the `mixin` executable used to launch `mixin lsp --asp-only`.
 *
 * Resolution order:
 * 1. Explicit `aspLsp.server.path` setting
 * 2. VSIX-bundled `server/mixin` (or `server/mixin.exe` on Windows)
 * 3. `MIXIN_BIN` environment variable
 * 4. `mixin` on `PATH`
 */
export function resolveMixinServerPath(options: ResolveServerPathOptions): string {
  const configured = options.configuredPath?.trim() ?? "";
  if (configured.length > 0) {
    return configured;
  }

  const platform = options.platform ?? process.platform;
  const binaryName = platform === "win32" ? "mixin.exe" : "mixin";
  const fileExists = options.fileExists ?? fs.existsSync;

  const bundled = path.join(options.extensionPath, "server", binaryName);
  if (fileExists(bundled)) {
    return bundled;
  }

  const envBin = (options.env ?? process.env).MIXIN_BIN?.trim() ?? "";
  if (envBin.length > 0) {
    return envBin;
  }

  return "mixin";
}
