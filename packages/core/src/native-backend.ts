import { spawn, spawnSync, type ChildProcessWithoutNullStreams } from "node:child_process";
import { createHash } from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import type { AspParsedDocument, AspSettings, VbCstNode } from "./types";
import type { FileAnalysisSummary, VbProjectContext, VbSymbol } from "./vbscript-types";
import type { Diagnostic } from "vscode-languageserver-types";

export type AspAnalysisBackendKind = "native" | "typescript-fallback";

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

interface NativeJsonlResponse<T> {
  id: number;
  ok: boolean;
  result?: T;
  error?: string;
}

let cachedNativePath: string | undefined | null;
let nextNativeRequestId = 1;
let nativeWorkerPool: NativeCoreWorkerPool | undefined;
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

export async function tryNativeParseAspDocumentAsync(
  uri: string,
  text: string,
  settings: AspSettings,
): Promise<AspParsedDocument | undefined> {
  // 浅いドキュメント（CST スケルトン）を取得する。重い VB CST は必要時に
  // tryNativeParseAspDocumentVbscriptAsync で取得して attach する。
  const shallow = await nativeOperationAsync<AspParsedDocument>({
    operation: "parseAspDocumentShallow",
    uri,
    text,
    settings,
    cacheKey: nativeDocumentCacheKey(uri, text, settings),
  });
  if (!shallow) {
    return undefined;
  }
  // ドキュメント全文は転送省略しているため、入力テキストを再注入する。
  shallow.text = text;
  return shallow;
}

/// CST ノードに付く VB CST サブツリーを node.start で索引付けして取得する。
export interface NativeVbscriptSegment {
  start: number;
  vbscript: VbCstNode;
}

export function tryNativeParseAspDocumentVbscriptAsync(
  uri: string,
  text: string,
  settings: AspSettings,
): Promise<NativeVbscriptSegment[] | undefined> {
  return nativeOperationAsync<NativeVbscriptSegment[]>({
    operation: "parseAspDocumentVbscript",
    uri,
    text,
    settings,
    cacheKey: nativeDocumentCacheKey(uri, text, settings),
  });
}

export function tryNativeParseAspCst(
  text: string,
  settings: AspSettings,
): AspParsedDocument["cst"] | undefined {
  return nativeOperation<AspParsedDocument["cst"]>({ operation: "parseAspCst", text, settings });
}

export function tryNativeParseAspCstAsync(
  text: string,
  settings: AspSettings,
): Promise<AspParsedDocument["cst"] | undefined> {
  return nativeOperationAsync<AspParsedDocument["cst"]>({
    operation: "parseAspCst",
    text,
    settings,
  });
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

export function tryNativeParseVbscriptCstAsync(
  text: string,
  sourceText: string,
  baseOffset: number,
): Promise<VbCstNode | undefined> {
  return nativeOperationAsync<VbCstNode>({
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

export function tryNativeCollectVbscriptSymbolsAsync(
  parsed: AspParsedDocument,
  _context: VbProjectContext,
): Promise<VbSymbol[] | undefined> {
  return nativeOperationAsync<VbSymbol[]>({ operation: "collectVbscriptSymbols", parsed });
}

export function tryNativeCollectVbscriptSymbolsFromTextAsync(
  uri: string,
  text: string,
  settings: AspSettings,
  context: VbProjectContext,
): Promise<VbSymbol[] | undefined> {
  return nativeOperationAsync<VbSymbol[]>({
    operation: "collectVbscriptSymbolsFromText",
    uri,
    text,
    settings,
    context: cloneableContext(context),
    cacheKey: nativeDocumentCacheKey(uri, text, settings),
  });
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

export function tryNativeAnalyzeVbscriptAsync(
  parsed: AspParsedDocument,
  context: VbProjectContext,
): Promise<{ diagnostics: Diagnostic[]; symbols: VbSymbol[] } | undefined> {
  return nativeOperationAsync<{ diagnostics: Diagnostic[]; symbols: VbSymbol[] }>({
    operation: "analyzeVbscript",
    parsed,
    context: cloneableContext(context),
  });
}

export function tryNativeAnalyzeVbscriptFromTextAsync(
  uri: string,
  text: string,
  settings: AspSettings,
  context: VbProjectContext,
): Promise<{ diagnostics: Diagnostic[]; symbols: VbSymbol[] } | undefined> {
  return nativeOperationAsync<{ diagnostics: Diagnostic[]; symbols: VbSymbol[] }>({
    operation: "analyzeVbscriptFromText",
    uri,
    text,
    settings,
    context: cloneableContext(context),
    cacheKey: nativeDocumentCacheKey(uri, text, settings),
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

export function tryNativeSummarizeAspFileAnalysisAsync(
  parsed: AspParsedDocument,
  context: VbProjectContext,
): Promise<FileAnalysisSummary | undefined> {
  return nativeOperationAsync<FileAnalysisSummary>({
    operation: "summarizeAspFileAnalysis",
    parsed,
    context: cloneableContext(context),
  });
}

export function tryNativeSummarizeAspFileAnalysisFromTextAsync(
  uri: string,
  text: string,
  settings: AspSettings,
  context: VbProjectContext,
): Promise<FileAnalysisSummary | undefined> {
  return nativeOperationAsync<FileAnalysisSummary>({
    operation: "summarizeAspFileAnalysisFromText",
    uri,
    text,
    settings,
    context: cloneableContext(context),
    cacheKey: nativeDocumentCacheKey(uri, text, settings),
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
  if (!isSupportedNativeMode(mode)) {
    lastBackendInfo = {
      backend: "typescript-fallback",
      engine: "typescript",
      reason: `unsupported ASP_LSP_ANALYSIS_BACKEND=${mode}`,
    };
    return undefined;
  }
  const nativePath = resolveNativePath();
  if (nativePath) {
    const result = runNative<T>(nativePath, request);
    if (result !== undefined || mode === "native") {
      return result;
    }
  }
  return undefined;
}

async function nativeOperationAsync<T>(request: NativeRequest): Promise<T | undefined> {
  const mode = backendMode();
  if (mode === "typescript" || mode === "off") {
    lastBackendInfo = {
      backend: "typescript-fallback",
      engine: "typescript",
      reason: `disabled by ASP_LSP_ANALYSIS_BACKEND=${mode}`,
    };
    return undefined;
  }
  if (!isSupportedNativeMode(mode)) {
    lastBackendInfo = {
      backend: "typescript-fallback",
      engine: "typescript",
      reason: `unsupported ASP_LSP_ANALYSIS_BACKEND=${mode}`,
    };
    return undefined;
  }
  const nativePath = resolveNativePath();
  if (nativePath) {
    const result = await runNativeAsync<T>(nativePath, request);
    if (result !== undefined || mode === "native") {
      return result;
    }
  }
  return undefined;
}

function backendMode(): string {
  return (process.env.ASP_LSP_ANALYSIS_BACKEND ?? "auto").toLowerCase();
}

function isSupportedNativeMode(mode: string): boolean {
  return mode === "auto" || mode === "native";
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

async function runNativeAsync<T>(binary: string, request: NativeRequest): Promise<T | undefined> {
  try {
    const result = await workerPoolFor(binary).request<T>(request);
    lastBackendInfo = { backend: "native", engine: "asp-lsp-core" };
    return result;
  } catch (error) {
    lastBackendInfo = {
      backend: "typescript-fallback",
      engine: "typescript",
      reason: error instanceof Error ? error.message : String(error),
    };
    if (backendMode() === "native") {
      throw error;
    }
    return undefined;
  }
}

function workerPoolFor(binary: string): NativeCoreWorkerPool {
  const size = nativeWorkerPoolSize();
  if (
    !nativeWorkerPool ||
    nativeWorkerPool.binary !== binary ||
    nativeWorkerPool.size !== size ||
    nativeWorkerPool.closed
  ) {
    nativeWorkerPool?.dispose();
    nativeWorkerPool = new NativeCoreWorkerPool(binary, size);
  }
  return nativeWorkerPool;
}

function nativeWorkerPoolSize(): number {
  const raw = Number.parseInt(process.env.ASP_LSP_NATIVE_WORKERS ?? "4", 10);
  return Number.isFinite(raw) && raw > 0 ? Math.min(raw, 16) : 4;
}

class NativeCoreWorkerPool {
  readonly workers: Array<NativeCoreWorker | undefined> = [];

  constructor(
    readonly binary: string,
    readonly size: number,
  ) {}

  get closed(): boolean {
    const createdWorkers = this.workers.filter(
      (worker): worker is NativeCoreWorker => worker !== undefined,
    );
    return createdWorkers.length > 0 && createdWorkers.every((worker) => worker.closed);
  }

  request<T>(request: NativeRequest): Promise<T> {
    const worker = this.nextWorker(request);
    return worker.request<T>(request);
  }

  dispose(): void {
    for (const worker of this.workers) {
      worker?.dispose();
    }
    this.workers.length = 0;
  }

  private nextWorker(request: NativeRequest): NativeCoreWorker {
    const cacheKey = typeof request.cacheKey === "string" ? request.cacheKey : undefined;
    if (cacheKey) {
      return this.workerAt(hashString(cacheKey) % this.size);
    }
    const openWorkers = this.openWorkers();
    if (openWorkers.length < this.size) {
      const slot = this.workers.findIndex((worker) => worker === undefined || worker.closed);
      return this.workerAt(slot >= 0 ? slot : this.workers.length);
    }
    return openWorkers.reduce((best, candidate) =>
      candidate.pendingCount < best.pendingCount ? candidate : best,
    );
  }

  private workerAt(slot: number): NativeCoreWorker {
    const normalizedSlot = slot % this.size;
    const worker = this.workers[normalizedSlot];
    if (worker && !worker.closed) {
      return worker;
    }
    const nextWorker = new NativeCoreWorker(this.binary);
    this.workers[normalizedSlot] = nextWorker;
    return nextWorker;
  }

  private openWorkers(): NativeCoreWorker[] {
    return this.workers.filter(
      (worker): worker is NativeCoreWorker => worker !== undefined && !worker.closed,
    );
  }
}

class NativeCoreWorker {
  readonly binary: string;
  closed = false;
  private readonly child: ChildProcessWithoutNullStreams;
  private readonly pending = new Map<
    number,
    {
      resolve: (value: unknown) => void;
      reject: (error: Error) => void;
    }
  >();
  private stdoutChunks: string[] = [];
  private stderr = "";

  get pendingCount(): number {
    return this.pending.size;
  }

  constructor(binary: string) {
    this.binary = binary;
    this.child = spawn(binary, ["--jsonl"], { stdio: "pipe" });
    this.unref();
    this.child.stdout.setEncoding("utf8");
    this.child.stderr.setEncoding("utf8");
    this.child.stdout.on("data", (chunk: string) => this.handleStdout(chunk));
    this.child.stderr.on("data", (chunk: string) => {
      this.stderr = `${this.stderr}${chunk}`.slice(-4096);
    });
    this.child.on("error", (error) => this.rejectAll(error));
    this.child.on("exit", (code, signal) => {
      this.closed = true;
      this.rejectAll(
        new Error(
          this.stderr.trim() ||
            `native worker exited ${code ?? "unknown"}${signal ? ` (${signal})` : ""}`,
        ),
      );
    });
  }

  request<T>(request: NativeRequest): Promise<T> {
    if (this.closed) {
      return Promise.reject(new Error("native worker is closed"));
    }
    const id = nextNativeRequestId++;
    const payload = `${JSON.stringify({ id, request })}\n`;
    this.ref();
    return new Promise<T>((resolve, reject) => {
      this.pending.set(id, {
        resolve: (value) => resolve(value as T),
        reject,
      });
      this.child.stdin.write(payload, "utf8", (error) => {
        if (!error) {
          return;
        }
        this.pending.delete(id);
        if (this.pending.size === 0) {
          this.unref();
        }
        reject(error);
      });
    });
  }

  dispose(): void {
    this.closed = true;
    this.child.kill();
    this.rejectAll(new Error("native worker disposed"));
  }

  private handleStdout(chunk: string): void {
    if (!chunk.includes("\n")) {
      this.stdoutChunks.push(chunk);
      return;
    }
    const stdout = `${this.stdoutChunks.join("")}${chunk}`;
    this.stdoutChunks = [];
    const lines = stdout.split("\n");
    const incomplete = lines.pop();
    if (incomplete) {
      this.stdoutChunks.push(incomplete);
    }
    for (const rawLine of lines) {
      const line = rawLine.trim();
      if (line) {
        this.handleResponse(line);
      }
    }
  }

  private handleResponse(line: string): void {
    let response: NativeJsonlResponse<unknown>;
    try {
      response = JSON.parse(line) as NativeJsonlResponse<unknown>;
    } catch (error) {
      this.rejectAll(error instanceof Error ? error : new Error(String(error)));
      return;
    }
    const pending = this.pending.get(response.id);
    if (!pending) {
      return;
    }
    this.pending.delete(response.id);
    if (response.ok) {
      pending.resolve(response.result);
    } else {
      pending.reject(new Error(response.error ?? "native worker request failed"));
    }
    if (this.pending.size === 0) {
      this.unref();
    }
  }

  private rejectAll(error: Error): void {
    for (const pending of this.pending.values()) {
      pending.reject(error);
    }
    this.pending.clear();
    this.unref();
  }

  private ref(): void {
    this.child.ref();
    refStream(this.child.stdin);
    refStream(this.child.stdout);
    refStream(this.child.stderr);
  }

  private unref(): void {
    this.child.unref();
    unrefStream(this.child.stdin);
    unrefStream(this.child.stdout);
    unrefStream(this.child.stderr);
  }
}

function refStream(stream: unknown): void {
  (stream as { ref?: () => void }).ref?.();
}

function unrefStream(stream: unknown): void {
  (stream as { unref?: () => void }).unref?.();
}

function nativeDocumentCacheKey(uri: string, text: string, settings: AspSettings): string {
  const hash = createHash("sha256");
  hash.update(uri);
  hash.update("\0");
  hash.update(text);
  hash.update("\0");
  hash.update(JSON.stringify(settings));
  return hash.digest("base64url");
}

function hashString(value: string): number {
  let hash = 2166136261;
  for (let index = 0; index < value.length; index += 1) {
    hash ^= value.charCodeAt(index);
    hash = Math.imul(hash, 16777619);
  }
  return hash >>> 0;
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
