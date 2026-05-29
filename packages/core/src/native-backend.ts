import { spawnSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import type { AspParsedDocument, AspSettings, VbCstNode } from "./types";
import type { FileAnalysisSummary, VbProjectContext, VbSymbol } from "./vbscript-types";
import type { Diagnostic } from "vscode-languageserver-types";

export type AspAnalysisBackendKind = "native" | "wasm" | "typescript-fallback";

export interface AspAnalysisBackendInfo {
  backend: AspAnalysisBackendKind;
  engine: string;
  version?: string;
  reason?: string;
}

interface NativeRequest {
  operation: string;
  [key: string]: unknown;
}

interface WasmExports {
  memory: WebAssembly.Memory;
  asp_lsp_alloc(len: number): number;
  asp_lsp_dealloc(pointer: number, len: number): void;
  asp_lsp_handle(pointer: number, len: number): number;
  asp_lsp_last_output_len(): number;
}

let cachedNativePath: string | undefined | null;
let cachedWasmPath: string | undefined | null;
let cachedWasmExports: WasmExports | undefined;
let lastBackendInfo: AspAnalysisBackendInfo = {
  backend: "typescript-fallback",
  engine: "typescript",
  reason: "not loaded",
};

export function aspAnalysisBackendInfo(): AspAnalysisBackendInfo {
  return lastBackendInfo;
}

export function tryNativeParseAspDocument(
  uri: string,
  text: string,
  settings: AspSettings,
): AspParsedDocument | undefined {
  return nativeOperation<AspParsedDocument>({ operation: "parseAspDocument", uri, text, settings });
}

export function tryNativeParseAspCst(
  text: string,
  settings: AspSettings,
): AspParsedDocument["cst"] | undefined {
  return nativeOperation<AspParsedDocument["cst"]>({ operation: "parseAspCst", text, settings });
}

export function tryNativeParseVbscriptCst(
  text: string,
  sourceText: string,
  baseOffset: number,
): VbCstNode | undefined {
  return nativeOperation<VbCstNode>({
    operation: "parseVbscriptCst",
    text,
    sourceText,
    baseOffset,
  });
}

export function tryNativeCollectVbscriptSymbols(
  parsed: AspParsedDocument,
  _context: VbProjectContext,
): VbSymbol[] | undefined {
  return nativeOperation<VbSymbol[]>({ operation: "collectVbscriptSymbols", parsed });
}

export function tryNativeAnalyzeVbscript(
  parsed: AspParsedDocument,
  context: VbProjectContext,
): { diagnostics: Diagnostic[]; symbols: VbSymbol[] } | undefined {
  return nativeOperation<{ diagnostics: Diagnostic[]; symbols: VbSymbol[] }>({
    operation: "analyzeVbscript",
    parsed,
    context: cloneableContext(context),
  });
}

export function tryNativeSummarizeAspFileAnalysis(
  parsed: AspParsedDocument,
  context: VbProjectContext,
): FileAnalysisSummary | undefined {
  return nativeOperation<FileAnalysisSummary>({
    operation: "summarizeAspFileAnalysis",
    parsed,
    context: cloneableContext(context),
  });
}

function nativeOperation<T>(request: NativeRequest): T | undefined {
  const mode = backendMode();
  if (mode === "typescript" || mode === "off") {
    lastBackendInfo = {
      backend: "typescript-fallback",
      engine: "typescript",
      reason: `disabled by ASP_LSP_ANALYSIS_BACKEND=${mode}`,
    };
    return undefined;
  }
  if (mode !== "wasm") {
    const nativePath = resolveNativePath();
    if (nativePath) {
      const result = runNative<T>(nativePath, request);
      if (result !== undefined || mode === "native") {
        return result;
      }
    }
  }
  if (mode !== "native") {
    const wasmPath = resolveWasmPath();
    if (wasmPath) {
      const result = runWasm<T>(wasmPath, request);
      if (result !== undefined || mode === "wasm") {
        return result;
      }
    }
  }
  return undefined;
}

function backendMode(): string {
  return (process.env.ASP_LSP_ANALYSIS_BACKEND ?? "auto").toLowerCase();
}

function runNative<T>(binary: string, request: NativeRequest): T | undefined {
  const result = spawnSync(binary, {
    input: JSON.stringify(request),
    encoding: "utf8",
    maxBuffer: 64 * 1024 * 1024,
  });
  if (result.status !== 0 || result.error) {
    lastBackendInfo = {
      backend: "typescript-fallback",
      engine: "typescript",
      reason: result.error?.message ?? result.stderr.trim() ?? `native exited ${result.status}`,
    };
    if (backendMode() === "native") {
      throw new Error(lastBackendInfo.reason);
    }
    return undefined;
  }
  lastBackendInfo = { backend: "native", engine: "asp-lsp-core" };
  return parseJsonResult<T>(result.stdout);
}

function runWasm<T>(wasmPath: string, request: NativeRequest): T | undefined {
  try {
    const exports = wasmExports(wasmPath);
    const input = Buffer.from(JSON.stringify(request), "utf8");
    const inputPointer = exports.asp_lsp_alloc(input.byteLength);
    new Uint8Array(exports.memory.buffer, inputPointer, input.byteLength).set(input);
    const outputPointer = exports.asp_lsp_handle(inputPointer, input.byteLength);
    exports.asp_lsp_dealloc(inputPointer, input.byteLength);
    const outputLength = exports.asp_lsp_last_output_len();
    const output = Buffer.from(
      new Uint8Array(exports.memory.buffer, outputPointer, outputLength),
    ).toString("utf8");
    exports.asp_lsp_dealloc(outputPointer, outputLength);
    lastBackendInfo = { backend: "wasm", engine: "asp-lsp-core" };
    return parseJsonResult<T>(output);
  } catch (error) {
    lastBackendInfo = {
      backend: "typescript-fallback",
      engine: "typescript",
      reason: error instanceof Error ? error.message : String(error),
    };
    if (backendMode() === "wasm") {
      throw error;
    }
    return undefined;
  }
}

function wasmExports(wasmPath: string): WasmExports {
  if (cachedWasmExports) {
    return cachedWasmExports;
  }
  const module = new WebAssembly.Module(fs.readFileSync(wasmPath));
  const instance = new WebAssembly.Instance(module, {});
  cachedWasmExports = instance.exports as unknown as WasmExports;
  return cachedWasmExports;
}

function parseJsonResult<T>(raw: string): T | undefined {
  const parsed = JSON.parse(raw) as T | { error?: string };
  if (typeof parsed === "object" && parsed !== null && "error" in parsed) {
    throw new Error(String((parsed as { error?: string }).error));
  }
  return parsed as T;
}

function resolveNativePath(): string | undefined {
  const explicit = process.env.ASP_LSP_NATIVE_CORE_PATH;
  if (explicit) {
    return fs.existsSync(explicit) ? explicit : undefined;
  }
  if (cachedNativePath !== undefined) {
    return cachedNativePath ?? undefined;
  }
  const executable = process.platform === "win32" ? "asp-lsp-core.exe" : "asp-lsp-core";
  const candidates = sourceCheckoutCoreDist()
    ? []
    : [
        path.join(__dirname, "..", "native", runtimeTarget(), executable),
        path.join(__dirname, "native", runtimeTarget(), executable),
      ];
  cachedNativePath = candidates.find((candidate) => fs.existsSync(candidate)) ?? null;
  return cachedNativePath ?? undefined;
}

function resolveWasmPath(): string | undefined {
  const explicit = process.env.ASP_LSP_WASM_CORE_PATH;
  if (explicit) {
    return fs.existsSync(explicit) ? explicit : undefined;
  }
  if (cachedWasmPath !== undefined) {
    return cachedWasmPath ?? undefined;
  }
  const candidates = sourceCheckoutCoreDist()
    ? []
    : [
        path.join(__dirname, "..", "wasm", "asp_lsp_core.wasm"),
        path.join(__dirname, "wasm", "asp_lsp_core.wasm"),
      ];
  cachedWasmPath = candidates.find((candidate) => fs.existsSync(candidate)) ?? null;
  return cachedWasmPath ?? undefined;
}

function runtimeTarget(): string {
  const platform =
    process.platform === "win32"
      ? "win32"
      : process.platform === "darwin"
        ? "darwin"
        : process.platform === "linux"
          ? "linux"
          : process.platform;
  const arch = process.arch === "x64" ? "x64" : process.arch === "arm64" ? "arm64" : process.arch;
  return `${platform}-${arch}`;
}

function sourceCheckoutCoreDist(): boolean {
  if (process.env.ASP_LSP_ENABLE_SOURCE_NATIVE === "1") {
    return false;
  }
  return (
    (path.basename(__dirname) === "dist" || path.basename(__dirname) === "src") &&
    path.basename(path.dirname(__dirname)) === "core"
  );
}

function cloneableContext(context: VbProjectContext): Omit<VbProjectContext, "debugStep"> {
  const { debugStep: _debugStep, ...cloneable } = context;
  return cloneable;
}
