import type { Stylesheet } from "vscode-css-languageservice";
import type { Diagnostic } from "vscode-languageserver/node";
import type { TextDocument } from "vscode-languageserver-textdocument";
import type {
  AspEditImpact,
  AspEmbeddedLanguage,
  AspIncrementalChange,
  AspSettings,
  AspParsedDocument,
  FileAnalysisSummary,
  VbProjectContext,
  VbSymbol,
  VbSymbolIndex,
  VirtualDocument,
} from "@asp-lsp/core";
import { fileIdentityKeyFromUri, sameFileIdentityUri } from "./file-identity";
import type { VbProjectAnalysis, VbProjectSummaryGraph } from "./analysis-caches";

export const maxCachedDocumentEditHistory = 64;

export interface SemanticTokenData {
  line: number;
  character: number;
  length: number;
  tokenType: string;
  tokenModifiers?: readonly string[];
}

export interface CachedDocument {
  source: TextDocument;
  parsed: AspParsedDocument;
  parseDepth: "skeleton" | "full";
  virtuals: Map<AspEmbeddedLanguage, VirtualDocument>;
  virtualsMaterialized: boolean;
  cssContext?: CachedCssContext;
  identity: DocumentIdentity;
  generation: number;
  lastAccess: number;
  demotedAt?: number;
  parseSettingsIdentity: string;
  includeResolutionIdentity: string;
  diagnosticsIdentity: string;
  jsProjectIdentity: string;
  workspaceGeneration: number;
  includeResolutionGeneration: number;
  jsProjectGeneration: number;
  editHistory: AspEditImpact[];
  lastEditImpact?: AspEditImpact;
  lastIncrementalChange?: AspIncrementalChange;
  lastEditIsOrdinaryVbscriptComment?: boolean;
  analysis?: CachedAnalysis;
}

export interface CachedCssContext {
  key: string;
  virtual: VirtualDocument;
  document: TextDocument;
  stylesheet: Stylesheet;
}

export interface CachedAnalysis {
  diagnostics?: DiagnosticCacheEntry;
  includeDiagnostics?: DiagnosticCacheEntry;
  syntaxDiagnostics?: DiagnosticCacheEntry;
  projectDiagnostics?: DiagnosticCacheEntry;
  htmlDiagnostics?: DiagnosticCacheEntry;
  cssDiagnostics?: DiagnosticCacheEntry;
  vbDiagnostics?: DiagnosticCacheEntry;
  vbFastDiagnostics?: DiagnosticCacheEntry;
  jsSyntaxDiagnostics?: CachedJsDiagnosticsEntry;
  jsSlowDiagnostics?: CachedJsDiagnosticsEntry;
  semanticTokensFull?: CachedSemanticTokensEntry;
  semanticJavascriptTokens?: CachedSemanticJavascriptTokensEntry;
  vbProjectContext?: { key: string; rootKey: string; context: VbProjectContext };
  localVbProjectContext?: { key: string; context: VbProjectContext };
  immediateLocalVbProjectContext?: { key: string; context: VbProjectContext };
  vbProjectDocuments?: {
    collectionKey: string;
    documents: AspParsedDocument[];
  };
  vbProjectSummaryGraph?: {
    collectionKey: string;
    graph: VbProjectSummaryGraph;
  };
  vbProjectSummaryGraphSeed?: {
    collectionKey: string;
    graph: VbProjectSummaryGraph;
    rootTextLength: number;
  };
  vbFileSummary?: {
    key: string;
    summary: FileAnalysisSummary;
  };
  vbProjectAnalysis?: {
    key: string;
    analysis: VbProjectAnalysis;
  };
  referenceCodeLensSymbols?: {
    key: string;
    symbols: VbSymbol[];
  };
  unresolvedVbscriptCompletionIndex?: {
    key: string;
    index: VbSymbolIndex;
  };
}

export interface CachedSemanticTokensEntry {
  key: string;
  data: number[];
}

export interface CachedSemanticJavascriptTokensEntry {
  key: string;
  tokens: SemanticTokenData[];
}

export interface DocumentIdentity {
  uri: string;
  version: number;
}

export interface InFlightDocumentRefresh {
  identity: DocumentIdentity;
  parseSettingsIdentity: string;
  promise: Promise<CachedDocument>;
}

export interface DiagnosticCacheEntry {
  key: string;
  items: Diagnostic[];
  text: string;
}

export interface CachedTsDiagnostic {
  code: number;
  category: import("typescript").DiagnosticCategory;
  messageText: string;
  start?: number;
  length?: number;
  reportsUnnecessary?: boolean;
}

export interface CachedJsDiagnostic {
  diagnostic: CachedTsDiagnostic;
  severity?: import("vscode-languageserver/node").DiagnosticSeverity;
  source?: string;
}

export interface CachedJsVirtualDiagnostics {
  virtualKey: string;
  diagnostics: CachedJsDiagnostic[];
}

export interface CachedJsDiagnosticsEntry {
  key: string;
  virtuals: CachedJsVirtualDiagnostics[];
}

export class DocumentStore {
  readonly cache = new Map<string, CachedDocument>();
  readonly inFlightDocumentRefreshes = new Map<string, InFlightDocumentRefresh>();

  cachedDocumentForUri(uri: string): CachedDocument | undefined {
    const cached =
      this.cache.get(uri) ??
      (uri.startsWith("file://")
        ? [...this.cache.values()].find((cached) => sameFileIdentityUri(cached.source.uri, uri))
        : undefined);
    if (cached) {
      this.touch(cached);
    }
    return cached;
  }

  cachedDocumentsForUri(uri: string): CachedDocument[] {
    const direct = this.cache.get(uri);
    if (!uri.startsWith("file://")) {
      return direct ? [direct] : [];
    }
    const fileKey = fileIdentityKeyFromUri(uri);
    const matches = [...this.cache.values()].filter(
      (cached) => fileIdentityKeyFromUri(cached.source.uri) === fileKey,
    );
    const result = direct && !matches.includes(direct) ? [direct, ...matches] : matches;
    for (const cached of result) {
      this.touch(cached);
    }
    return result;
  }

  deleteCachedDocumentsForUri(uri: string): void {
    for (const [key, cached] of this.cache) {
      if (
        key === uri ||
        (uri.startsWith("file://") && sameFileIdentityUri(cached.source.uri, uri))
      ) {
        this.cache.delete(key);
      }
    }
  }

  touch(cached: CachedDocument, now = Date.now()): void {
    cached.lastAccess = now;
  }

  demote(
    cached: CachedDocument,
    options: {
      settings: AspSettings;
      parseSkeleton: (uri: string, text: string, settings: AspSettings) => AspParsedDocument;
      now?: number;
    },
  ): boolean {
    const hadEvictableState =
      cached.analysis !== undefined ||
      cached.cssContext !== undefined ||
      cached.virtuals.size > 0 ||
      cached.virtualsMaterialized ||
      cached.parseDepth === "full";
    if (!hadEvictableState) {
      return false;
    }
    cached.analysis = undefined;
    cached.cssContext = undefined;
    cached.virtuals.clear();
    cached.virtualsMaterialized = false;
    if (cached.parseDepth === "full") {
      cached.parsed = options.parseSkeleton(
        cached.source.uri,
        cached.source.getText(),
        options.settings,
      );
      cached.parseDepth = "skeleton";
    }
    cached.demotedAt = options.now ?? Date.now();
    cached.generation += 1;
    return true;
  }
}

export const documentStore = new DocumentStore();
export const cache = documentStore.cache;
export const inFlightDocumentRefreshes = documentStore.inFlightDocumentRefreshes;

export const cachedDocumentForUri = (uri: string): CachedDocument | undefined =>
  documentStore.cachedDocumentForUri(uri);
export const cachedDocumentsForUri = (uri: string): CachedDocument[] =>
  documentStore.cachedDocumentsForUri(uri);
export const deleteCachedDocumentsForUri = (uri: string): void =>
  documentStore.deleteCachedDocumentsForUri(uri);
export const touchCachedDocument = (cached: CachedDocument, now?: number): void =>
  documentStore.touch(cached, now);
