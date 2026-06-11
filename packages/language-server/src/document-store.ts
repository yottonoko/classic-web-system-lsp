import type { Stylesheet } from "vscode-css-languageservice";
import type { Diagnostic } from "vscode-languageserver/node";
import type { TextDocument } from "vscode-languageserver-textdocument";
import type {
  AspEditImpact,
  AspEmbeddedLanguage,
  AspIncrementalChange,
  AspParsedDocument,
  FileAnalysisSummary,
  VbProjectContext,
  VbSymbol,
  VbSymbolIndex,
  VirtualDocument,
} from "@asp-lsp/core";
import { fileIdentityKeyFromUri, sameFileIdentityUri } from "./file-identity";
import type { VbProjectAnalysis, VbProjectSummaryGraph } from "./analysis-caches";

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

  // TODO(P2): Replace these duplicate include dependency maps with WorkspaceIncludeGraph.
  readonly includeForwardDependencies = new Map<string, Set<string>>();
  readonly includeReverseDependencies = new Map<string, Set<string>>();

  cachedDocumentForUri(uri: string): CachedDocument | undefined {
    return (
      this.cache.get(uri) ??
      (uri.startsWith("file://")
        ? [...this.cache.values()].find((cached) => sameFileIdentityUri(cached.source.uri, uri))
        : undefined)
    );
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
    return direct && !matches.includes(direct) ? [direct, ...matches] : matches;
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

  reverseDependenciesInclude(includeKeys: Set<string>, ownerKey: string): boolean {
    for (const includeKey of includeKeys) {
      if (this.includeReverseDependencies.get(includeKey)?.has(ownerKey)) {
        return true;
      }
    }
    return false;
  }

  resetIncludeDependencies(ownerUri: string): void {
    const ownerKey = fileIdentityKeyFromUri(ownerUri);
    const previous = this.includeForwardDependencies.get(ownerKey);
    if (!previous) {
      return;
    }
    for (const includeKey of previous) {
      const owners = this.includeReverseDependencies.get(includeKey);
      owners?.delete(ownerKey);
      if (owners?.size === 0) {
        this.includeReverseDependencies.delete(includeKey);
      }
    }
    this.includeForwardDependencies.delete(ownerKey);
  }

  recordIncludeDependency(ownerUri: string, includeUri: string): void {
    const ownerKey = fileIdentityKeyFromUri(ownerUri);
    const includeKey = fileIdentityKeyFromUri(includeUri);
    let forward = this.includeForwardDependencies.get(ownerKey);
    if (!forward) {
      forward = new Set();
      this.includeForwardDependencies.set(ownerKey, forward);
    }
    forward.add(includeKey);
    let reverse = this.includeReverseDependencies.get(includeKey);
    if (!reverse) {
      reverse = new Set();
      this.includeReverseDependencies.set(includeKey, reverse);
    }
    reverse.add(ownerKey);
  }

  clearIncludeDependencies(): void {
    this.includeForwardDependencies.clear();
    this.includeReverseDependencies.clear();
  }
}

export const documentStore = new DocumentStore();
export const cache = documentStore.cache;
export const inFlightDocumentRefreshes = documentStore.inFlightDocumentRefreshes;
export const includeForwardDependencies = documentStore.includeForwardDependencies;
export const includeReverseDependencies = documentStore.includeReverseDependencies;

export const cachedDocumentForUri = (uri: string): CachedDocument | undefined =>
  documentStore.cachedDocumentForUri(uri);
export const cachedDocumentsForUri = (uri: string): CachedDocument[] =>
  documentStore.cachedDocumentsForUri(uri);
export const deleteCachedDocumentsForUri = (uri: string): void =>
  documentStore.deleteCachedDocumentsForUri(uri);
export const reverseDependenciesInclude = (includeKeys: Set<string>, ownerKey: string): boolean =>
  documentStore.reverseDependenciesInclude(includeKeys, ownerKey);
export const resetIncludeDependencies = (ownerUri: string): void =>
  documentStore.resetIncludeDependencies(ownerUri);
export const recordIncludeDependency = (ownerUri: string, includeUri: string): void =>
  documentStore.recordIncludeDependency(ownerUri, includeUri);
export const clearIncludeDependencies = (): void => documentStore.clearIncludeDependencies();
