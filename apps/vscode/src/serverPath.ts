import * as fs from "node:fs";
import * as path from "node:path";

export type ResolvedServerLaunch =
  | { kind: "native"; command: string; args: string[] }
  | { kind: "wasm"; wasmPath: string; args: string[] };

export interface ResolveServerOptions {
  configuredPath?: string;
  extraArgs?: string[];
  extensionPath: string;
  env?: NodeJS.ProcessEnv;
  platform?: NodeJS.Platform;
  fileExists?: (candidate: string) => boolean;
}

function isWasmFile(p: string): boolean {
  return p.toLowerCase().endsWith(".wasm");
}

/**
 * Resolves how to launch the standalone `classic-asp-lsp` server (native binary or WASM).
 *
 * Resolution order:
 * 1. Explicit `aspLsp.server.path` setting (native binary or `.wasm` module)
 * 2. VSIX-bundled native binary: `server/classic-asp-lsp.exe` (Windows) / `server/classic-asp-lsp`
 * 3. VSIX-bundled WASM (Any OS): `server/classic-asp-lsp.wasm`
 * 4. `CLASSIC_ASP_LSP_BIN` environment variable (native binary or `.wasm`)
 * 5. `classic-asp-lsp` on `PATH`
 */
export function resolveAspLspServer(options: ResolveServerOptions): ResolvedServerLaunch {
  const extraArgs = options.extraArgs ?? [];
  const configured = options.configuredPath?.trim() ?? "";
  if (configured.length > 0) {
    return isWasmFile(configured)
      ? { kind: "wasm", wasmPath: configured, args: extraArgs }
      : { kind: "native", command: configured, args: extraArgs };
  }

  const platform = options.platform ?? process.platform;
  const binaryNames =
    platform === "win32"
      ? ["classic-asp-lsp.exe", "mixin-asp-lsp.exe"]
      : ["classic-asp-lsp", "mixin-asp-lsp"];
  const fileExists = options.fileExists ?? fs.existsSync;

  for (const binaryName of binaryNames) {
    const bundledNative = path.join(options.extensionPath, "server", binaryName);
    if (fileExists(bundledNative)) {
      return { kind: "native", command: bundledNative, args: extraArgs };
    }
  }

  for (const wasmName of ["classic-asp-lsp.wasm", "mixin-asp-lsp.wasm"]) {
    const bundledWasm = path.join(options.extensionPath, "server", wasmName);
    if (fileExists(bundledWasm)) {
      return { kind: "wasm", wasmPath: bundledWasm, args: extraArgs };
    }
  }

  const envBin = (options.env ?? process.env).CLASSIC_ASP_LSP_BIN?.trim() ?? "";
  if (envBin.length > 0) {
    return isWasmFile(envBin)
      ? { kind: "wasm", wasmPath: envBin, args: extraArgs }
      : { kind: "native", command: envBin, args: extraArgs };
  }

  return { kind: "native", command: "classic-asp-lsp", args: extraArgs };
}
