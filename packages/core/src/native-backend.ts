import { spawn, spawnSync, type ChildProcessWithoutNullStreams } from "node:child_process";
import { createHash } from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import type { AspParsedDocument, AspSettings, VbCstNode, VbToken } from "./types";
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

// --frames プロトコルの応答フレーム種別タグ（body 先頭の 1 バイト）。
const FRAME_KIND_JSON = 0;
const FRAME_KIND_VBSCRIPT_COLUMNAR = 1;

// VB トークン種別の整数コード表。crates/asp-core 側のエンコーダと一致させること。
const VB_TOKEN_KINDS: readonly VbToken["kind"][] = [
  "identifier",
  "keyword",
  "string",
  "number",
  "symbol",
  "comment",
  "whitespace",
  "newline",
  "unknown",
];

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

export async function tryNativeParseAspDocumentVbscriptAsync(
  uri: string,
  text: string,
  settings: AspSettings,
): Promise<NativeVbscriptSegment[] | undefined> {
  // 応答は列指向バイナリ（FRAME_KIND_VBSCRIPT_COLUMNAR）。token text はソースから復元する。
  const payload = await nativeOperationAsync<Uint8Array>({
    operation: "parseAspDocumentVbscript",
    uri,
    text,
    settings,
    cacheKey: nativeDocumentCacheKey(uri, text, settings),
  });
  if (!payload) {
    return undefined;
  }
  const buffer = Buffer.isBuffer(payload) ? payload : Buffer.from(payload);
  return decodeVbscriptColumnar(buffer, text);
}

interface ColumnarVbNode {
  kind: VbCstNode["kind"];
  start: number;
  end: number;
  contentStart?: number;
  contentEnd?: number;
  nameToken?: number;
  tokens: [number, number];
  identifiers?: number[];
  parameters?: number[];
  children: ColumnarVbNode[];
  procedureKind?: VbCstNode["procedureKind"];
  propertyAccessor?: VbCstNode["propertyAccessor"];
  declarationKind?: VbCstNode["declarationKind"];
  visibility?: VbCstNode["visibility"];
  typeName?: string;
  memberOf?: string;
  scopeName?: string;
  scopeStart?: number;
  scopeEnd?: number;
  arrayDeclarations?: VbCstNode["arrayDeclarations"];
  parameterMetadata?: VbCstNode["parameterMetadata"];
}

/// 列指向バイナリ payload（segmentCount から始まる）を VbCstNode セグメント列へ復元する。
function decodeVbscriptColumnar(buf: Buffer, source: string): NativeVbscriptSegment[] {
  let offset = 0;
  const readU32 = (): number => {
    const value = buf.readUInt32LE(offset);
    offset += 4;
    return value;
  };
  const segmentCount = readU32();
  const segments: NativeVbscriptSegment[] = [];
  for (let s = 0; s < segmentCount; s += 1) {
    const nodeStart = readU32();
    const poolCount = readU32();
    const kindCodes = buf.subarray(offset, offset + poolCount);
    offset += poolCount;
    const starts: number[] = [];
    for (let i = 0; i < poolCount; i += 1) {
      starts.push(buf.readUInt32LE(offset));
      offset += 4;
    }
    const ends: number[] = [];
    for (let i = 0; i < poolCount; i += 1) {
      ends.push(buf.readUInt32LE(offset));
      offset += 4;
    }
    const valueCount = readU32();
    const valueByIndex = new Map<number, string>();
    for (let i = 0; i < valueCount; i += 1) {
      const tokenIndex = readU32();
      const byteLen = readU32();
      valueByIndex.set(tokenIndex, buf.toString("utf8", offset, offset + byteLen));
      offset += byteLen;
    }
    const treeJsonLen = readU32();
    const tree = JSON.parse(buf.toString("utf8", offset, offset + treeJsonLen)) as ColumnarVbNode;
    offset += treeJsonLen;

    const pool: VbToken[] = [];
    for (let i = 0; i < poolCount; i += 1) {
      const start = starts[i];
      const end = ends[i];
      const token: VbToken = {
        kind: VB_TOKEN_KINDS[kindCodes[i]] ?? "unknown",
        start,
        end,
        text: source.slice(start, end),
      };
      const value = valueByIndex.get(i);
      if (value !== undefined) {
        token.value = value;
      }
      pool.push(token);
    }
    segments.push({ start: nodeStart, vbscript: rebuildVbNode(tree, pool) });
  }
  return segments;
}

function rebuildVbNode(node: ColumnarVbNode, pool: VbToken[]): VbCstNode {
  const result: VbCstNode = {
    kind: node.kind,
    start: node.start,
    end: node.end,
    tokens: pool.slice(node.tokens[0], node.tokens[1]),
    children: node.children.map((child) => rebuildVbNode(child, pool)),
  };
  if (node.contentStart !== undefined) {
    result.contentStart = node.contentStart;
  }
  if (node.contentEnd !== undefined) {
    result.contentEnd = node.contentEnd;
  }
  if (node.nameToken !== undefined) {
    result.nameToken = pool[node.nameToken];
  }
  if (node.identifiers) {
    result.identifiers = node.identifiers.map((index) => pool[index]);
  }
  if (node.parameters) {
    result.parameters = node.parameters.map((index) => pool[index]);
  }
  if (node.procedureKind) {
    result.procedureKind = node.procedureKind;
  }
  if (node.propertyAccessor) {
    result.propertyAccessor = node.propertyAccessor;
  }
  if (node.declarationKind) {
    result.declarationKind = node.declarationKind;
  }
  if (node.visibility) {
    result.visibility = node.visibility;
  }
  if (node.typeName !== undefined) {
    result.typeName = node.typeName;
  }
  if (node.memberOf !== undefined) {
    result.memberOf = node.memberOf;
  }
  if (node.scopeName !== undefined) {
    result.scopeName = node.scopeName;
  }
  if (node.scopeStart !== undefined) {
    result.scopeStart = node.scopeStart;
  }
  if (node.scopeEnd !== undefined) {
    result.scopeEnd = node.scopeEnd;
  }
  if (node.arrayDeclarations) {
    result.arrayDeclarations = node.arrayDeclarations;
  }
  if (node.parameterMetadata) {
    result.parameterMetadata = node.parameterMetadata;
  }
  return result;
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
  private stdoutBuffer: Buffer = Buffer.alloc(0);
  private stderr = "";

  get pendingCount(): number {
    return this.pending.size;
  }

  constructor(binary: string) {
    this.binary = binary;
    // --frames: stdin は改行区切り JSON のまま、stdout は length-prefixed バイナリフレーム。
    this.child = spawn(binary, ["--frames"], { stdio: "pipe" });
    this.unref();
    this.child.stderr.setEncoding("utf8");
    this.child.stdout.on("data", (chunk: Buffer) => this.handleStdout(chunk));
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

  private handleStdout(chunk: Buffer): void {
    this.stdoutBuffer =
      this.stdoutBuffer.length === 0 ? chunk : Buffer.concat([this.stdoutBuffer, chunk]);
    // フレーム = [u32 LE bodyLen][body]。body[0] が種別タグ。
    while (this.stdoutBuffer.length >= 4) {
      const bodyLen = this.stdoutBuffer.readUInt32LE(0);
      if (this.stdoutBuffer.length < 4 + bodyLen) {
        break;
      }
      const body = this.stdoutBuffer.subarray(4, 4 + bodyLen);
      this.stdoutBuffer = this.stdoutBuffer.subarray(4 + bodyLen);
      this.processFrame(body);
    }
  }

  private processFrame(body: Buffer): void {
    const kind = body[0];
    if (kind === FRAME_KIND_JSON) {
      this.handleResponse(body.toString("utf8", 1));
      return;
    }
    if (kind === FRAME_KIND_VBSCRIPT_COLUMNAR) {
      // body = [u8 kind][u32 id][columnar payload]
      const id = body.readUInt32LE(1);
      const payload = body.subarray(5);
      const pending = this.pending.get(id);
      if (!pending) {
        return;
      }
      this.pending.delete(id);
      pending.resolve(payload);
      if (this.pending.size === 0) {
        this.unref();
      }
      return;
    }
    this.rejectAll(new Error(`unknown native frame kind ${kind}`));
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
