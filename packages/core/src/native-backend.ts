import type { Diagnostic } from "vscode-languageserver-types";
import type { AspParsedDocument, AspSettings, VbCstNode } from "./types";
import type { FileAnalysisSummary, VbProjectContext, VbSymbol } from "./vbscript-types";

export type AspAnalysisBackendKind = "native" | "typescript-fallback";

export interface AspAnalysisBackendInfo {
  backend: AspAnalysisBackendKind;
  engine: string;
  version?: string;
  reason?: string;
}

export interface NativeVbscriptSegment {
  start: number;
  vbscript: VbCstNode;
}

let lastBackendInfo: AspAnalysisBackendInfo = fallbackInfo("not loaded");

export function aspAnalysisBackendInfo(): AspAnalysisBackendInfo {
  return lastBackendInfo;
}

export function shouldUseNativeAsyncSkeletonParse(): boolean {
  recordFallbackForMode();
  return false;
}

export function tryNativeParseAspDocument(
  _uri: string,
  _text: string,
  _settings: AspSettings,
): AspParsedDocument | undefined {
  return noNativeResult();
}

export function tryNativeParseAspDocumentAsync(
  _uri: string,
  _text: string,
  _settings: AspSettings,
): Promise<AspParsedDocument | undefined> {
  return Promise.resolve(noNativeResult());
}

export function tryNativeParseAspDocumentSkeletonAsync(
  _uri: string,
  _text: string,
  _settings: AspSettings,
): Promise<AspParsedDocument | undefined> {
  return Promise.resolve(noNativeResult());
}

export function tryNativeParseAspDocumentVbscriptAsync(
  _uri: string,
  _text: string,
  _settings: AspSettings,
): Promise<NativeVbscriptSegment[] | undefined> {
  return Promise.resolve(noNativeResult());
}

export function tryNativeParseAspCst(
  _text: string,
  _settings: AspSettings,
): AspParsedDocument["cst"] | undefined {
  return noNativeResult();
}

export function tryNativeParseAspCstAsync(
  _text: string,
  _settings: AspSettings,
): Promise<AspParsedDocument["cst"] | undefined> {
  return Promise.resolve(noNativeResult());
}

export function tryNativeParseVbscriptCst(
  _text: string,
  _sourceText: string,
  _baseOffset: number,
): VbCstNode | undefined {
  return noNativeResult();
}

export function tryNativeParseVbscriptCstAsync(
  _text: string,
  _sourceText: string,
  _baseOffset: number,
): Promise<VbCstNode | undefined> {
  return Promise.resolve(noNativeResult());
}

export function tryNativeCollectVbscriptSymbols(
  _parsed: AspParsedDocument,
  _context: VbProjectContext,
): VbSymbol[] | undefined {
  return noNativeResult();
}

export function tryNativeCollectVbscriptSymbolsAsync(
  _parsed: AspParsedDocument,
  _context: VbProjectContext,
): Promise<VbSymbol[] | undefined> {
  return Promise.resolve(noNativeResult());
}

export function tryNativeCollectVbscriptSymbolsFromTextAsync(
  _uri: string,
  _text: string,
  _settings: AspSettings,
  _context: VbProjectContext,
): Promise<VbSymbol[] | undefined> {
  return Promise.resolve(noNativeResult());
}

export function tryNativeAnalyzeVbscript(
  _parsed: AspParsedDocument,
  _context: VbProjectContext,
): { diagnostics: Diagnostic[]; symbols: VbSymbol[] } | undefined {
  return noNativeResult();
}

export function tryNativeAnalyzeVbscriptAsync(
  _parsed: AspParsedDocument,
  _context: VbProjectContext,
): Promise<{ diagnostics: Diagnostic[]; symbols: VbSymbol[] } | undefined> {
  return Promise.resolve(noNativeResult());
}

export function tryNativeAnalyzeVbscriptFromTextAsync(
  _uri: string,
  _text: string,
  _settings: AspSettings,
  _context: VbProjectContext,
): Promise<{ diagnostics: Diagnostic[]; symbols: VbSymbol[] } | undefined> {
  return Promise.resolve(noNativeResult());
}

export function tryNativeSummarizeAspFileAnalysis(
  _parsed: AspParsedDocument,
  _context: VbProjectContext,
): FileAnalysisSummary | undefined {
  return noNativeResult();
}

export function tryNativeSummarizeAspFileAnalysisAsync(
  _parsed: AspParsedDocument,
  _context: VbProjectContext,
): Promise<FileAnalysisSummary | undefined> {
  return Promise.resolve(noNativeResult());
}

export function tryNativeSummarizeAspFileAnalysisFromTextAsync(
  _uri: string,
  _text: string,
  _settings: AspSettings,
  _context: VbProjectContext,
): Promise<FileAnalysisSummary | undefined> {
  return Promise.resolve(noNativeResult());
}

function noNativeResult<T>(): T | undefined {
  recordFallbackForMode();
  return undefined;
}

function recordFallbackForMode(): void {
  const mode = backendMode();
  if (mode === "typescript" || mode === "off") {
    lastBackendInfo = fallbackInfo(`disabled by ASP_LSP_ANALYSIS_BACKEND=${mode}`);
    return;
  }
  if (mode !== "auto" && mode !== "native") {
    lastBackendInfo = fallbackInfo(`unsupported ASP_LSP_ANALYSIS_BACKEND=${mode}`);
    return;
  }
  lastBackendInfo = fallbackInfo("native backend removed after Rust LSP cutover");
}

function fallbackInfo(reason: string): AspAnalysisBackendInfo {
  return {
    backend: "typescript-fallback",
    engine: "typescript",
    reason,
  };
}

function backendMode(): string {
  return (process.env.ASP_LSP_ANALYSIS_BACKEND ?? "auto").toLowerCase();
}
