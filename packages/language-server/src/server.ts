#!/usr/bin/env node
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { VbDiagnosticsWorkerPool } from "./vb-worker-pool";
import {
  DiskAnalysisCache,
  type DiskAnalysisBuilderState,
  type DiskAnalysisSourceMetadata,
} from "./disk-analysis-cache";
import type {
  VbDiagnosticsWorkerContext,
  VbDiagnosticsWorkerResponse,
} from "./vb-diagnostics-protocol";
import { analyse, detect } from "chardet";
import {
  CodeActionKind,
  CompletionItemKind,
  createConnection,
  DiagnosticSeverity,
  DocumentHighlightKind,
  DocumentSymbol,
  FileChangeType,
  FoldingRange,
  Hover,
  InlineValueVariableLookup,
  InitializeParams,
  InitializeResult,
  Location,
  MonikerKind,
  ProposedFeatures,
  ReferenceParams,
  SemanticTokensBuilder,
  SymbolInformation,
  SymbolKind,
  TextDocumentPositionParams,
  TextDocuments,
  TextDocumentSyncKind,
  UniquenessLevel,
} from "vscode-languageserver/node";
import type {
  CallHierarchyIncomingCall,
  CallHierarchyItem,
  CallHierarchyOutgoingCall,
  CodeAction,
  CodeActionContext,
  CodeLens,
  Color,
  ColorInformation,
  ColorPresentation,
  CompletionItem,
  Diagnostic,
  DocumentLink,
  DocumentHighlight,
  FormattingOptions,
  InlineValue,
  InlineValueParams,
  InlayHint,
  LinkedEditingRanges,
  Moniker,
  Position,
  Range,
  RenameParams,
  SemanticTokens,
  SemanticTokensDelta,
  SelectionRange,
  SignatureHelp,
  TextEdit,
  TypeHierarchyItem,
  WorkspaceEdit,
} from "vscode-languageserver/node";
import { TextDocument } from "vscode-languageserver-textdocument";
import {
  analyzeVbscript,
  buildVbTypeEnvironment,
  buildVirtualDocument,
  collectVbscriptSymbols,
  createLocalizer,
  formatAspDocument,
  formatAspRange,
  getVbscriptCompletions,
  getVbscriptDefinition,
  getVbscriptDocumentationQuickAction,
  getVbscriptDocumentHighlights,
  getVbscriptDocumentSymbols,
  getVbscriptHover,
  getVbscriptImplementation,
  getVbscriptIncomingCalls,
  getVbscriptInlayHints,
  getVbscriptOutgoingCalls,
  getVbscriptRenameRange,
  getVbscriptReferences,
  getVbscriptSelectionRanges,
  getVbscriptSemanticTokens,
  getVbscriptSignatureHelp,
  getVbscriptTypeDefinition,
  parseAspDocument,
  parseVbscriptTypeRef,
  prepareVbscriptCallHierarchy,
  resolveVbscriptCompletionItem,
  shiftAspRangeAfterChange,
  summarizeAspFileAnalysis,
  updateAspParsedDocument,
  type AspFormattingOptions,
  type AspEmbeddedLanguage,
  type AspEditImpact,
  type AspIncrementalChange,
  type AspLegacyEncoding,
  type AspLocale,
  type AspLocaleSetting,
  type AspCstNode,
  type AspParsedDocument,
  type AspSettings,
  type AspRegion,
  type FileAnalysisSummary,
  type VirtualDocument,
  type VbCstNode,
  type VbProjectContext,
  type VbExternalRefUsage,
  type VbSymbol,
  type VbSymbolKind,
  type VbType,
  type VbTypeEnvironment,
} from "@asp-lsp/core";
import { getCSSLanguageService } from "vscode-css-languageservice";
import {
  getLanguageService as getHtmlLanguageService,
  TokenType,
} from "vscode-html-languageservice";
import ts from "typescript";

const connection = createConnection(ProposedFeatures.all);
const documentOpenContentVersions = new Map<string, number>();
const documents = new TextDocuments({
  create: TextDocument.create,
  update: (document, changes, version) => {
    pendingDocumentChanges.set(document.uri, pendingChangeFromContentChanges(version, changes));
    return TextDocument.update(document, changes, version);
  },
});
const htmlService = getHtmlLanguageService();
const cssService = getCSSLanguageService();
const settingsByUri = new Map<string, AspSettings>();
const includePathResolutionCache = new Map<string, IncludePathResolution>();
const pathResolutionCache = new Map<string, PathResolution>();
const includeCycleCache = new Map<string, string[] | null>();
const includeForwardDependencies = new Map<string, Set<string>>();
const includeReverseDependencies = new Map<string, Set<string>>();
const includePublicSummaries = new Map<string, IncludePublicSummaryState>();
const workspaceIndex = new Map<string, WorkspaceIndexedDocument>();
const jsLanguageServiceCache = new Map<string, JsLanguageServiceCacheEntry>();
const jsProjectConfigCache = new Map<string, JsProjectConfigCacheEntry>();
const jsDocumentRegistry = ts.createDocumentRegistry(ts.sys.useCaseSensitiveFileNames);
const jsScriptSnapshots = new Map<string, AspJsScriptSnapshot>();
const semanticTokenResults = new Map<string, { uri: string; data: number[] }>();
const latestSemanticTokenResultByUri = new Map<string, string>();
const regionIndexes = new WeakMap<AspParsedDocument, RegionIndex>();
const defaultMaxIndexFiles = 5000;
const defaultScanChunkSize = 200;
const defaultDiagnosticsDebounceMs = 250;
const reindexWorkspaceCommand = "aspLsp.reindexWorkspace";
const clearCacheCommand = "aspLsp.clearCache";
const reindexWorkspaceServerCommand = "aspLsp.server.reindexWorkspace";
const clearCacheServerCommand = "aspLsp.server.clearCache";
const languageServerVersion = "0.1.6";
const projectUpdateDelayMs = 250;
const openFileProjectMaintenanceDelayMs = 2_500;
const backgroundAnalysisIdleDelayMs = 5_000;
let globalSettings: AspSettings = { defaultLanguage: "VBScript", checkJs: false };
let workspaceRoots: string[] = [];
let clientLocale = "en";
let workspaceIndexDirty = true;
let workspaceIndexTruncated = false;
let jsLanguageServiceCacheTick = 0;
let jsScriptSnapshotSequence = 0;
let semanticTokenResultCounter = 0;
let documentCacheGeneration = 0;
let workspaceGeneration = 0;
let includeResolutionGeneration = 0;
let jsProjectGeneration = 0;
let diskAnalysisCache = createDiskAnalysisCache(globalSettings);
let backgroundAnalysisTimer: ReturnType<typeof setTimeout> | undefined;
let backgroundAnalysisGeneration = 0;
let backgroundAnalysisRunning = false;
let backgroundAnalysisRunningGeneration: number | undefined;
let pendingBackgroundAnalysisReason: string | undefined;
let lastForegroundActivityAt = 0;
let projectUpdateTimer: ReturnType<typeof setTimeout> | undefined;
let openFileProjectMaintenanceTimer: ReturnType<typeof setTimeout> | undefined;
let pendingProjectUpdateReason: string | undefined;
let pendingOpenFileMaintenanceReason: string | undefined;
const tsUnusedDiagnosticCodes = new Set([6133, 6138, 6192, 6196, 6198]);
const hiddenJavaScriptGlobalCompletions = new Set(["__dirname", "__filename"]);
const browserJavaScriptLibs = ["lib.esnext.d.ts", "lib.dom.d.ts", "lib.dom.iterable.d.ts"];

interface PathResolution {
  fileName: string;
  exists: boolean;
  pathCaseMatches: boolean;
  actualPath?: string;
}

interface IncludePathResolution extends PathResolution {
  actualIncludePath?: string;
}

const semanticTokenTypes = [
  "keyword",
  "variable",
  "parameter",
  "function",
  "class",
  "method",
  "property",
  "comment",
  "string",
  "operator",
  "namespace",
  "interface",
  "enum",
  "enumMember",
  "typeAlias",
  "typeParameter",
] as const;
const semanticTokenModifiers = [
  "public",
  "private",
  "readonly",
  "library",
  "byref",
  "byval",
] as const;

interface SemanticTokenData {
  line: number;
  character: number;
  length: number;
  tokenType: string;
  tokenModifiers?: readonly string[];
}

interface RegionIndex {
  byStart: AspRegion[];
}

interface CachedDocument {
  source: TextDocument;
  parsed: AspParsedDocument;
  virtuals: Map<string, VirtualDocument>;
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
  analysis?: CachedAnalysis;
}

interface CachedAnalysis {
  diagnostics?: DiagnosticCacheEntry;
  includeDiagnostics?: DiagnosticCacheEntry;
  syntaxDiagnostics?: DiagnosticCacheEntry;
  projectDiagnostics?: DiagnosticCacheEntry;
  htmlDiagnostics?: DiagnosticCacheEntry;
  cssDiagnostics?: DiagnosticCacheEntry;
  vbDiagnostics?: DiagnosticCacheEntry;
  jsSyntaxDiagnostics?: DiagnosticCacheEntry;
  jsSlowDiagnostics?: DiagnosticCacheEntry;
  vbProjectContext?: { key: string; rootKey: string; context: VbProjectContext };
  localVbProjectContext?: { key: string; context: VbProjectContext };
  immediateLocalVbProjectContext?: { key: string; context: VbProjectContext };
  vbProjectDocuments?: {
    collectionKey: string;
    documents: AspParsedDocument[];
  };
  vbFileSummary?: {
    key: string;
    summary: FileAnalysisSummary;
  };
  vbProjectAnalysis?: {
    key: string;
    analysis: VbProjectAnalysis;
  };
}

interface DocumentIdentity {
  uri: string;
  version: number;
  text: string;
}

interface SettingsInvalidationImpact {
  parse: boolean;
  includeResolution: boolean;
  jsProject: boolean;
  diagnostics: boolean;
  workspaceIndex: boolean;
}

interface PendingDocumentChange {
  version: number;
  changes: AspIncrementalChange[];
  reason: string;
  ranged: boolean;
}

interface WatchedAspFileChange {
  fileName: string;
  type: FileChangeType;
}

interface DiagnosticCacheEntry {
  key: string;
  items: Diagnostic[];
  text: string;
}

type DiagnosticLayerKey = "fast" | "include" | "syntax" | "project" | "final";

interface StagedDiagnosticsState {
  generation: number;
  uri: string;
  version: number;
  documentGeneration: number;
  diagnosticsIdentity: string;
  startedAt: bigint;
  preservePreviousDiagnosticsUntilFinal: boolean;
  layers: Partial<Record<DiagnosticLayerKey, Diagnostic[]>>;
}

interface PublishedDiagnosticsState {
  version?: number;
  diagnostics: Diagnostic[];
}

interface AnalysisCancellation {
  isCancellationRequested(): boolean;
}

type AnalysisExecutionMode = "foreground" | "workspace" | "idle";

interface OffsetEdit {
  start: number;
  end: number;
  newText: string;
}

interface WorkspaceIndexedDocument {
  uri: string;
  fileName: string;
  mtimeMs: number;
  size: number;
}

type JavaScriptMode = "definition" | "declaration" | "typeDefinition" | "implementation";

interface JsProjectFile {
  fileName: string;
  text: string;
  version: string;
  uri: string;
  virtual?: VirtualDocument;
  snapshot?: ts.IScriptSnapshot;
}

interface JsProjectContext {
  virtual: VirtualDocument;
  service: ts.LanguageService;
  fileName: string;
  offset: number;
  files: Map<string, JsProjectFile>;
}

interface JsLanguageServiceProject {
  service: ts.LanguageService;
  host: ts.LanguageServiceHost;
  files: Map<string, JsProjectFile>;
  options: ts.CompilerOptions;
  currentDirectory: string;
  moduleResolutionCache: ts.ModuleResolutionCache;
  optionsKey: string;
  projectVersion: number;
}

interface JsLanguageServiceCacheEntry {
  project: JsLanguageServiceProject;
  lastUsed: number;
}

interface JsProjectConfigCacheEntry {
  config: JsProjectConfig;
  lastUsed: number;
}

interface JsProjectConfig {
  fileNames: string[];
  options: ts.CompilerOptions;
  currentDirectory: string;
}

interface VbProjectContextCacheEntry {
  context: VbProjectContext;
  lastUsed: number;
}

interface IncludeDocumentCacheEntry {
  key: string;
  fileName: string;
  uri: string;
  parsed: AspParsedDocument;
  summary: FileAnalysisSummary;
  publicFingerprint: string;
  publicSignature: FilePublicSignature;
}

interface IncludePublicSummaryState {
  fileName: string;
  uri: string;
  key: string;
  publicFingerprint: string;
  publicSignature: FilePublicSignature;
}

interface VbProjectAnalysis {
  documents: AspParsedDocument[];
  symbols: VbSymbol[];
  typeEnvironment: VbTypeEnvironment;
  externalRefUsages: VbExternalRefUsage[];
}

interface FilePublicSignature {
  fingerprint: string;
  defaultLanguage: AspParsedDocument["defaultLanguage"];
  languages: string[];
  exports: unknown[];
  externalRefUsages: unknown[];
  affectsGlobalScope: boolean;
}

interface AspProjectFileBuilderState {
  uri: string;
  fileName: string;
  version: number;
  textFingerprint: string;
  publicSignature: FilePublicSignature;
  includeDeps: string[];
  externalRefUsageKeys: string[];
  diagnosticsLayers: Partial<Record<DiagnosticLayerKey, DiagnosticCacheEntry>>;
  changedReasons: string[];
}

interface CompletionCacheEntry {
  baseKey: string;
  uri: string;
  language: AspEmbeddedLanguage;
  prefix: string;
  offset: number;
  documentVersion: number;
  items: CompletionItem[];
}

class AspProjectBuilderState {
  private readonly files = new Map<string, AspProjectFileBuilderState>();
  private readonly affectedQueue = new Map<string, Set<string>>();

  updateFromSummary(
    cached: CachedDocument,
    summary: FileAnalysisSummary,
    settings: AspSettings,
    reason: string,
  ): void {
    const uri = summary.uri;
    const fileName = normalizeFileName(uriToFileName(uri));
    const signature = filePublicSignature(summary);
    const previous = this.files.get(uri);
    const changedReasons = builderStateChangeReasons(previous, cached, summary, signature);
    this.files.set(uri, {
      uri,
      fileName,
      version: cached.identity.version,
      textFingerprint: textFingerprint(summary.fingerprint),
      publicSignature: signature,
      includeDeps: summary.includeRefs.map(
        (include) => `${include.mode}:${include.path.toLowerCase()}`,
      ),
      externalRefUsageKeys: summary.vbscript?.externalRefUsages.map((usage) => usage.key) ?? [],
      diagnosticsLayers: previous?.diagnosticsLayers ?? {},
      changedReasons,
    });
    if (changedReasons.length > 0) {
      this.markAffected(uri, `${reason}:${changedReasons.join(",")}`);
      logDebugSummary(
        settings,
        `[asp-lsp] asp.builder.affected.count: ${uri}, count=${this.affectedQueue.size}, reason=${changedReasons.join(",")}`,
      );
    }
    logDebugSummary(
      settings,
      previous?.publicSignature.fingerprint === signature.fingerprint
        ? `[asp-lsp] asp.signature.unchanged: ${uri}, fingerprint=${signature.fingerprint}`
        : `[asp-lsp] asp.signature.changed: ${uri}, previous=${previous?.publicSignature.fingerprint ?? "missing"}, next=${signature.fingerprint}, globalScope=${signature.affectsGlobalScope}`,
    );
  }

  updateIncludeSummary(entry: IncludeDocumentCacheEntry, settings: AspSettings): void {
    const signature = entry.publicSignature;
    const previous = this.files.get(entry.uri);
    this.files.set(entry.uri, {
      uri: entry.uri,
      fileName: entry.fileName,
      version: 0,
      textFingerprint: entry.summary.fingerprint,
      publicSignature: signature,
      includeDeps: entry.summary.includeRefs.map(
        (include) => `${include.mode}:${include.path.toLowerCase()}`,
      ),
      externalRefUsageKeys:
        entry.summary.vbscript?.externalRefUsages.map((usage) => usage.key) ?? [],
      diagnosticsLayers: previous?.diagnosticsLayers ?? {},
      changedReasons:
        previous?.publicSignature.fingerprint === signature.fingerprint ? [] : ["publicSignature"],
    });
    if (previous?.publicSignature.fingerprint !== signature.fingerprint) {
      this.markAffected(entry.uri, "includePublicSignature");
      logDebugSummary(
        settings,
        `[asp-lsp] asp.builder.affected.count: ${entry.uri}, count=${this.affectedQueue.size}, reason=includePublicSignature`,
      );
      logDebugSummary(
        settings,
        `[asp-lsp] asp.signature.changed: ${entry.uri}, previous=${previous?.publicSignature.fingerprint ?? "missing"}, next=${signature.fingerprint}, globalScope=${signature.affectsGlobalScope}`,
      );
    } else {
      logDebugSummary(
        settings,
        `[asp-lsp] asp.signature.unchanged: ${entry.uri}, fingerprint=${signature.fingerprint}`,
      );
    }
  }

  updateDiagnosticsLayer(
    uri: string,
    layer: DiagnosticLayerKey,
    key: string,
    items: Diagnostic[],
    text: string,
    settings: AspSettings,
  ): void {
    const state = this.files.get(uri);
    if (!state) {
      return;
    }
    const previous = state.diagnosticsLayers[layer];
    if (previous?.key === key) {
      logDebugSummary(settings, `[asp-lsp] asp.builder.cache.hit: ${uri}, layer=${layer}`);
      return;
    }
    state.diagnosticsLayers[layer] = { key, items, text };
    logDebugSummary(settings, `[asp-lsp] asp.builder.cache.miss: ${uri}, layer=${layer}`);
  }

  diskStateForUri(uri: string): DiskAnalysisBuilderState | undefined {
    const state = this.files.get(uri);
    if (!state) {
      return undefined;
    }
    return {
      publicSignature: state.publicSignature,
      includeDeps: state.includeDeps,
      externalRefUsageKeys: state.externalRefUsageKeys,
      diagnosticsLayerFingerprints: Object.fromEntries(
        Object.entries(state.diagnosticsLayers).map(([layer, entry]) => [layer, entry?.key ?? ""]),
      ),
    };
  }

  restoreDiskState(
    uri: string,
    fileName: string,
    source: DiskAnalysisSourceMetadata,
    builderState: DiskAnalysisBuilderState | undefined,
    diagnostics: Diagnostic[],
    settings: AspSettings,
  ): void {
    if (!builderState?.publicSignature) {
      return;
    }
    const publicSignature = builderState.publicSignature as FilePublicSignature;
    this.files.set(uri, {
      uri,
      fileName,
      version: 0,
      textFingerprint: `${source.size}:${source.mtimeMs}`,
      publicSignature,
      includeDeps: builderState.includeDeps?.map(String) ?? [],
      externalRefUsageKeys: builderState.externalRefUsageKeys ?? [],
      diagnosticsLayers: {
        final: {
          key: builderState.diagnosticsLayerFingerprints?.final ?? "disk",
          items: diagnostics,
          text: "",
        },
      },
      changedReasons: [],
    });
    logDebugSummary(settings, `[asp-lsp] disk.builder.restore.hit: ${uri}`);
  }

  markAffected(uri: string, reason: string): void {
    const reasons = this.affectedQueue.get(uri) ?? new Set<string>();
    reasons.add(reason);
    this.affectedQueue.set(uri, reasons);
  }

  markFileAffected(fileName: string, reason: string): void {
    const uri = pathToFileUri(normalizeFileName(fileName));
    this.markAffected(uri, reason);
  }

  takeNextAffectedUri(): string | undefined {
    const next = this.affectedQueue.keys().next();
    if (next.done) {
      return undefined;
    }
    this.affectedQueue.delete(next.value);
    return next.value;
  }

  affectedCount(): number {
    return this.affectedQueue.size;
  }

  clear(): void {
    this.files.clear();
    this.affectedQueue.clear();
  }
}

class CompletionSessionCache {
  private readonly entries = new Map<string, CompletionCacheEntry>();
  private tick = 0;

  get(
    cached: CachedDocument,
    settings: AspSettings,
    region: AspRegion,
    position: Position,
  ): CompletionItem[] | undefined {
    const entry = this.entries.get(this.baseKey(cached, settings, region));
    if (!entry || entry.uri !== cached.source.uri || entry.language !== region.language) {
      return undefined;
    }
    const offset = cached.source.offsetAt(position);
    const prefix = completionPrefixAt(cached.source.getText(), offset);
    if (prefix.length === 0) {
      return undefined;
    }
    if (
      !prefix.startsWith(entry.prefix) ||
      offset < entry.offset ||
      !completionCacheContinuation(cached, entry, offset, prefix)
    ) {
      return undefined;
    }
    logDebugSummary(
      settings,
      `[asp-lsp] completion.cache.hit: ${cached.source.uri}, language=${region.language}, prefix=${prefix}`,
    );
    return entry.items.filter((item) => completionItemMatchesPrefix(item, prefix));
  }

  set(
    cached: CachedDocument,
    settings: AspSettings,
    region: AspRegion,
    position: Position,
    items: CompletionItem[],
  ): void {
    const offset = cached.source.offsetAt(position);
    const prefix = completionPrefixAt(cached.source.getText(), offset);
    if (prefix.length === 0) {
      return;
    }
    const baseKey = this.baseKey(cached, settings, region);
    this.entries.set(baseKey, {
      baseKey,
      uri: cached.source.uri,
      language: region.language,
      prefix,
      offset,
      documentVersion: cached.source.version,
      items,
    });
    this.tick += 1;
    if (this.entries.size > 32) {
      const first = this.entries.keys().next();
      if (!first.done) {
        this.entries.delete(first.value);
      }
    }
    logDebugSummary(
      settings,
      `[asp-lsp] completion.cache.miss: ${cached.source.uri}, language=${region.language}, prefix=${prefix}, tick=${this.tick}`,
    );
  }

  clear(reason: string): void {
    if (this.entries.size > 0) {
      logDebugSummary(globalSettings, `[asp-lsp] completion.cache.invalidate: reason=${reason}`);
    }
    this.entries.clear();
  }

  private baseKey(cached: CachedDocument, settings: AspSettings, region: AspRegion): string {
    return JSON.stringify({
      uri: cached.source.uri,
      language: region.language,
      regionKind: region.kind,
      regionStart: region.start,
      parse: cached.parseSettingsIdentity,
      include: cached.includeResolutionGeneration,
      js: cached.jsProjectGeneration,
      settings: completionSettingsIdentity(settings),
    });
  }
}

interface JsCallHierarchyData {
  kind: "javascript";
  rootUri: string;
  language: string;
  fileName: string;
  position: number;
}

interface VbTypeHierarchyData {
  kind: "vbscript";
  rootUri: string;
  uri: string;
  typeName: string;
  line: number;
  character: number;
}

function aspFileOperationFilter() {
  return {
    scheme: "file",
    pattern: {
      glob: "**/*.{asp,asa,inc}",
      matches: "file" as const,
      options: { ignoreCase: true },
    },
  };
}

class IncludeDocumentLoader {
  private readonly cache = new Map<string, IncludeDocumentCacheEntry>();
  private readonly inFlight = new Map<string, Promise<IncludeDocumentCacheEntry | undefined>>();
  private readonly generations = new Map<string, number>();

  read(fileName: string, settings: AspSettings): IncludeDocumentCacheEntry | undefined {
    const normalized = normalizeFileName(fileName);
    const key = includeDocumentCacheKey(normalized, settings);
    if (!key) {
      return undefined;
    }
    const existing = this.cache.get(normalized);
    if (existing?.key === key) {
      return existing;
    }
    const text = readTextFile(normalized, settings.legacyEncoding);
    const entry = createIncludeDocumentCacheEntry(normalized, text, settings, key);
    this.cache.set(normalized, entry);
    rememberIncludePublicSummary(entry, settings);
    return entry;
  }

  async readAsync(
    fileName: string,
    settings: AspSettings,
  ): Promise<IncludeDocumentCacheEntry | undefined> {
    const normalized = normalizeFileName(fileName);
    const key = await includeDocumentCacheKeyAsync(normalized, settings);
    if (!key) {
      return undefined;
    }
    const existing = this.cache.get(normalized);
    if (existing?.key === key) {
      return existing;
    }
    const inFlightKey = `${normalized}:${key}`;
    const pending = this.inFlight.get(inFlightKey);
    if (pending) {
      return pending;
    }
    const generation = this.generation(normalized);
    let promise: Promise<IncludeDocumentCacheEntry | undefined> | undefined;
    promise = (async () => {
      try {
        const text = await readTextFileAsync(normalized, settings.legacyEncoding);
        const entry = createIncludeDocumentCacheEntry(normalized, text, settings, key);
        if (this.generation(normalized) === generation) {
          this.cache.set(normalized, entry);
          rememberIncludePublicSummary(entry, settings);
        }
        return entry;
      } finally {
        if (this.inFlight.get(inFlightKey) === promise) {
          this.inFlight.delete(inFlightKey);
        }
      }
    })();
    this.inFlight.set(inFlightKey, promise);
    return promise;
  }

  cachedPublicSummary(fileName: string): IncludePublicSummaryState | undefined {
    return includePublicSummaries.get(normalizeFileName(fileName));
  }

  invalidateFiles(fileNames: Iterable<string>): void {
    for (const fileName of fileNames) {
      const normalized = normalizeFileName(fileName);
      this.generations.set(normalized, this.generation(normalized) + 1);
      this.cache.delete(normalized);
      for (const key of this.inFlight.keys()) {
        if (key.startsWith(`${normalized}:`)) {
          this.inFlight.delete(key);
        }
      }
    }
  }

  clear(): void {
    this.cache.clear();
    this.inFlight.clear();
    this.generations.clear();
  }

  private generation(fileName: string): number {
    return this.generations.get(fileName) ?? 0;
  }
}

const cache = new Map<string, CachedDocument>();
const diagnosticsTimers = new Map<string, ReturnType<typeof setTimeout>>();
const stagedDiagnosticsByUri = new Map<string, StagedDiagnosticsState>();
const publishedDiagnosticsByUri = new Map<string, PublishedDiagnosticsState>();
const pendingDocumentChanges = new Map<string, PendingDocumentChange>();
const vbProjectContextCache = new Map<string, VbProjectContextCacheEntry>();
const includeDocumentLoader = new IncludeDocumentLoader();
const aspProjectBuilderState = new AspProjectBuilderState();
const completionSessionCache = new CompletionSessionCache();
const maxVbProjectContextCacheEntries = 32;
let vbDiagnosticsWorkerPool: VbDiagnosticsWorkerPool | undefined;
let vbDiagnosticsWorkerRequestId = 0;
let stagedDiagnosticsGeneration = 0;

connection.onInitialize((params: InitializeParams): InitializeResult => {
  clientLocale = typeof params.locale === "string" ? params.locale : "en";
  globalSettings = normalizeSettings(globalSettings);
  workspaceRoots = [
    ...(params.workspaceFolders?.map((folder) => uriToFileName(folder.uri)) ?? []),
    ...(params.rootUri ? [uriToFileName(params.rootUri)] : []),
  ].filter((root, index, roots) => root.length > 0 && roots.indexOf(root) === index);
  return {
    capabilities: {
      textDocumentSync: {
        openClose: true,
        change: TextDocumentSyncKind.Incremental,
        willSave: true,
        willSaveWaitUntil: true,
        save: { includeText: false },
      },
      completionProvider: {
        triggerCharacters: ["<", ".", '"', "'", ":", "#", "("],
        resolveProvider: true,
      },
      signatureHelpProvider: { triggerCharacters: ["(", ",", " "] },
      hoverProvider: true,
      declarationProvider: true,
      definitionProvider: true,
      typeDefinitionProvider: true,
      implementationProvider: true,
      referencesProvider: true,
      renameProvider: { prepareProvider: true },
      documentHighlightProvider: true,
      workspaceSymbolProvider: true,
      documentSymbolProvider: true,
      foldingRangeProvider: true,
      documentLinkProvider: { resolveProvider: true },
      codeActionProvider: {
        resolveProvider: true,
        codeActionKinds: [
          CodeActionKind.QuickFix,
          CodeActionKind.Refactor,
          CodeActionKind.Source,
          CodeActionKind.SourceOrganizeImports,
          "source.organizeImports.aspLsp.javascript",
        ],
      },
      executeCommandProvider: {
        commands: [reindexWorkspaceServerCommand, clearCacheServerCommand],
      },
      codeLensProvider: { resolveProvider: true },
      colorProvider: true,
      selectionRangeProvider: true,
      linkedEditingRangeProvider: true,
      inlayHintProvider: { resolveProvider: true },
      callHierarchyProvider: true,
      typeHierarchyProvider: true,
      monikerProvider: true,
      inlineValueProvider: true,
      semanticTokensProvider: {
        legend: {
          tokenTypes: [...semanticTokenTypes],
          tokenModifiers: [...semanticTokenModifiers],
        },
        full: { delta: true },
        range: true,
      },
      documentFormattingProvider: true,
      documentRangeFormattingProvider: true,
      documentOnTypeFormattingProvider: {
        firstTriggerCharacter: "\n",
        moreTriggerCharacter: [">"],
      },
      workspace: {
        workspaceFolders: {
          supported: true,
          changeNotifications: true,
        },
        fileOperations: {
          willRename: { filters: [aspFileOperationFilter()] },
          didRename: { filters: [aspFileOperationFilter()] },
          didCreate: { filters: [aspFileOperationFilter()] },
          didDelete: { filters: [aspFileOperationFilter()] },
        },
      },
    },
  };
});

documents.onDidOpen((event) => {
  noteForegroundActivity();
  documentOpenContentVersions.set(event.document.uri, event.document.version);
  pendingDocumentChanges.delete(event.document.uri);
  publishedDiagnosticsByUri.delete(event.document.uri);
  cache.delete(event.document.uri);
  scheduleOpenFileProjectMaintenance("document.open");
  validate(event.document);
});
documents.onDidChangeContent((event) => {
  noteForegroundActivity();
  const openedVersion = documentOpenContentVersions.get(event.document.uri);
  if (openedVersion === event.document.version) {
    documentOpenContentVersions.delete(event.document.uri);
    return;
  }
  documentOpenContentVersions.delete(event.document.uri);
  const settings = cachedSettings(event.document.uri);
  measureDebugStep(settings, event.document.uri, "documentChange.keepCachedDocument", () => {
    // Keep the previous parsed document available for updateAspParsedDocument.
  });
  measureDebugStep(settings, event.document.uri, "documentChange.scheduleDiagnostics", () =>
    scheduleDiagnostics(event.document),
  );
  scheduleProjectUpdate("document.change");
});
documents.onDidSave((event) => {
  noteForegroundActivity();
  indexWorkspaceFile(uriToFileName(event.document.uri));
  invalidateCachedAnalysisForUris(new Set([event.document.uri]), "document.save");
  scheduleProjectUpdate("document.save");
  validate(event.document);
});
documents.onDidClose((event) => {
  cancelScheduledDiagnostics(event.document.uri);
  documentOpenContentVersions.delete(event.document.uri);
  pendingDocumentChanges.delete(event.document.uri);
  cache.delete(event.document.uri);
  clearSemanticTokensForUri(event.document.uri);
  stagedDiagnosticsByUri.delete(event.document.uri);
  publishedDiagnosticsByUri.delete(event.document.uri);
  resetIncludeDependencies(event.document.uri);
  scheduleOpenFileProjectMaintenance("document.close");
  connection.sendDiagnostics({ uri: event.document.uri, diagnostics: [] });
});

documents.onWillSave((event) => {
  validate(event.document);
});

documents.onWillSaveWaitUntil((event) => {
  validate(event.document);
  const cached = ensureFreshCachedDocument(event.document);
  const settings = cached ? cachedSettings(cached.source.uri) : undefined;
  return cached && settings?.format?.onSave === true
    ? formatAspDocumentWithDelegates(cached, defaultFormattingOptions(settings))
    : [];
});

connection.onInitialized(() => {
  void refreshConfiguration();
});

connection.onNotification(
  "workspace/didChangeWorkspaceFolders",
  (event: {
    event?: {
      added?: Array<{ uri: string }>;
      removed?: Array<{ uri: string }>;
    };
  }) => {
    const added = event.event?.added ?? [];
    const removedFolders = event.event?.removed ?? [];
    const removed = new Set(
      removedFolders.map((folder) => normalizeFileName(uriToFileName(folder.uri))),
    );
    workspaceRoots = [
      ...workspaceRoots.filter((root) => !removed.has(normalizeFileName(root))),
      ...added.map((folder) => uriToFileName(folder.uri)).filter((root) => root.length > 0),
    ].filter((root, index, roots) => roots.indexOf(root) === index);
    invalidateWorkspaceIndex("workspaceFolders.changed");
    invalidateIncludeResolution("workspaceFolders.changed");
    invalidateJsProject("workspaceFolders.changed");
    invalidateCachedAnalysisForUris(openDocumentUris(), "workspaceFolders.changed");
    scheduleProjectUpdate("workspaceFolders.changed");
    scheduleOpenFileProjectMaintenance("workspaceFolders.changed");
    for (const document of documents.all()) {
      validate(document);
    }
    scheduleBackgroundAnalysis("workspaceFolders.changed");
  },
);

connection.onDidChangeConfiguration((change) => {
  const previousSettingsByUri = currentOpenDocumentSettingsByUri();
  const incoming = readSettingsFromChange(change.settings);
  if (incoming) {
    globalSettings = normalizeSettings(incoming);
  }
  configureDiskAnalysisCache();
  settingsByUri.clear();
  const impact = settingsInvalidationImpact(previousSettingsByUri);
  applySettingsInvalidation(impact);
  scheduleProjectUpdate("configuration.changed");
  scheduleOpenFileProjectMaintenance("configuration.changed");
  for (const document of documents.all()) {
    applyDocumentSettingsInvalidation(document.uri, impact.get(document.uri));
    if (shouldValidateAfterSettingsChange(impact.get(document.uri))) {
      validate(document);
    }
  }
  scheduleBackgroundAnalysis("configuration.changed");
});

connection.onDidChangeWatchedFiles((change) => {
  let aspChanged = false;
  let scriptChanged = false;
  const aspChanges: WatchedAspFileChange[] = [];
  for (const file of change.changes) {
    const fileName = normalizeFileName(uriToFileName(file.uri));
    if (isAspWorkspaceFile(fileName)) {
      aspChanged = true;
      aspChanges.push({ fileName, type: file.type });
      if (file.type === FileChangeType.Deleted) {
        workspaceIndex.delete(fileName);
      } else {
        indexWorkspaceFile(fileName);
      }
    }
    if (isScriptWorkspaceFile(fileName) || isJavaScriptProjectEnvironmentFile(fileName)) {
      scriptChanged = true;
    }
  }
  if (!aspChanged && !scriptChanged) {
    return;
  }
  let publicChangedFiles = new Set<string>();
  if (aspChanged) {
    publicChangedFiles = refreshIncludePublicBoundariesForAspChanges(aspChanges);
    if (publicChangedFiles.size > 0) {
      ensureIncludeGraphForOpenDocuments(publicChangedFiles);
    }
    includeCycleCache.clear();
    if (aspChanges.some((change) => change.type !== FileChangeType.Changed)) {
      invalidateIncludeResolution("watchedAsp.structureChanged");
    }
  }
  if (aspChanged || scriptChanged) {
    invalidateJsProject(scriptChanged ? "watchedScript.changed" : "watchedAsp.changed");
  }
  scheduleProjectUpdate(scriptChanged ? "watchedScript.changed" : "watchedAsp.changed");
  const affectedUris = scriptChanged
    ? new Set(documents.all().map((document) => document.uri))
    : affectedOpenUrisForAspChanges(aspChanges, publicChangedFiles);
  invalidateCachedAnalysisForUris(
    affectedUris,
    scriptChanged ? "watchedScript.changed" : "watchedAsp.changed",
  );
  for (const document of documents.all().filter((item) => affectedUris.has(item.uri))) {
    validate(document);
  }
  scheduleBackgroundAnalysis(scriptChanged ? "watchedScript.changed" : "watchedAsp.changed");
});

connection.workspace.onWillRenameFiles((params) => includeRenameWorkspaceEdit(params.files));

connection.workspace.onDidRenameFiles(() => {
  invalidateWorkspaceIndex("fileOperation.rename");
  invalidateIncludeResolution("fileOperation.rename");
  invalidateJsProject("fileOperation.rename");
  invalidateCachedAnalysisForUris(openDocumentUris(), "fileOperation.rename");
});

connection.workspace.onDidCreateFiles(() => {
  invalidateWorkspaceIndex("fileOperation.create");
  invalidateIncludeResolution("fileOperation.create");
  invalidateJsProject("fileOperation.create");
  invalidateCachedAnalysisForUris(openDocumentUris(), "fileOperation.create");
});

connection.workspace.onDidDeleteFiles(() => {
  invalidateWorkspaceIndex("fileOperation.delete");
  invalidateIncludeResolution("fileOperation.delete");
  invalidateJsProject("fileOperation.delete");
  invalidateCachedAnalysisForUris(openDocumentUris(), "fileOperation.delete");
});

connection.onCompletion((params) =>
  runInteractiveLanguageFeature(() => {
    const cached = getFreshCached(params.textDocument.uri);
    if (!cached) {
      return [];
    }
    const settings = cachedSettings(cached.source.uri);
    const region = findRegionAt(cached.parsed, cached.source.offsetAt(params.position));
    if (!region) {
      return [];
    }
    const cachedCompletion = completionSessionCache.get(cached, settings, region, params.position);
    if (cachedCompletion) {
      return cachedCompletion;
    }
    const remember = (items: CompletionItem[]): CompletionItem[] => {
      completionSessionCache.set(cached, settings, region, params.position, items);
      return items;
    };
    if (region.language === "vbscript") {
      const context = immediateVbProjectContext(cached, settings);
      const completions = getVbscriptCompletions(cached.parsed, params.position, context);
      return remember(
        withCompletionData(
          completions.length > 0
            ? completions
            : fallbackVbMemberCompletions(cached, params.position, context),
          { kind: "vbscript", uri: cached.source.uri },
        ),
      );
    }
    if (region.language === "html") {
      const virtual = getCachedVirtual(cached, "html");
      if (!virtual) {
        return [];
      }
      const virtualDocument = toTextDocument(virtual);
      return remember(
        withCompletionData(
          htmlService.doComplete(
            virtualDocument,
            params.position,
            htmlService.parseHTMLDocument(virtualDocument),
          ).items,
          {
            kind: "html",
            uri: cached.source.uri,
            locale: settings.resolvedLocale,
          },
        ),
      );
    }
    if (region.language === "css") {
      return remember(
        withCompletionData(cssCompletion(cached, params, "css"), {
          kind: "css",
          uri: cached.source.uri,
          locale: settings.resolvedLocale,
        }),
      );
    }
    if (region && isJavaScriptLikeRegion(region)) {
      return remember(jsCompletion(cached, params));
    }
    return [];
  }),
);

connection.onCompletionResolve((item) =>
  runInteractiveLanguageFeature(() => {
    const data = item.data as { kind?: string; uri?: string } | undefined;
    if (data?.kind === "vbscript" && data.uri) {
      const cached = getFreshCached(data.uri);
      return cached
        ? resolveVbscriptCompletionItem(
            item,
            cached.parsed,
            immediateVbProjectContext(cached, cachedSettings(cached.source.uri)),
          )
        : item;
    }
    if (data?.kind === "javascript" && data.uri) {
      const resolved = resolveJsCompletion(item, data.uri);
      return resolved ?? item;
    }
    if ((data?.kind === "html" || data?.kind === "css") && data.uri) {
      return resolveEmbeddedCompletion(item, data.kind);
    }
    return item;
  }),
);

connection.onHover((params) =>
  runInteractiveLanguageFeature(() => {
    const cached = getFreshCached(params.textDocument.uri);
    if (!cached) {
      return null;
    }
    const region = findRegionAt(cached.parsed, cached.source.offsetAt(params.position));
    if (!region) {
      return null;
    }
    if (region.language === "vbscript") {
      return aspHover(cached, params);
    }
    if (region && isJavaScriptLikeRegion(region)) {
      return jsHover(cached, params.position);
    }
    if (region.language === "html") {
      const virtual = getCachedVirtual(cached, "html");
      if (!virtual) {
        return null;
      }
      const doc = toTextDocument(virtual);
      return htmlService.doHover(doc, params.position, htmlService.parseHTMLDocument(doc));
    }
    if (region.language === "css") {
      const virtual = getCachedVirtual(cached, "css");
      if (!virtual) {
        return null;
      }
      const virtualPosition = virtual.sourceMap.toVirtualPosition(params.position);
      if (!virtualPosition) {
        return null;
      }
      const doc = toTextDocument(virtual);
      return remapHover(
        virtual,
        cssService.doHover(doc, virtualPosition, cssService.parseStylesheet(doc)),
      );
    }
    return null;
  }),
);

connection.onDefinition((params) => {
  return definitionLikeLocation(params.textDocument.uri, params.position, "definition");
});

connection.onDeclaration((params) => {
  return definitionLikeLocation(params.textDocument.uri, params.position, "declaration");
});

connection.onTypeDefinition((params) => {
  return definitionLikeLocation(params.textDocument.uri, params.position, "typeDefinition");
});

connection.onImplementation((params) => {
  return definitionLikeLocation(params.textDocument.uri, params.position, "implementation");
});

connection.onReferences((params: ReferenceParams) => {
  const cached = getFreshCached(params.textDocument.uri);
  if (!cached) {
    return [];
  }
  const region = findRegionAt(cached.parsed, cached.source.offsetAt(params.position));
  if (!region) {
    return [];
  }
  if (region && isJavaScriptLikeRegion(region)) {
    return jsReferences(cached, params.position);
  }
  if (region.language !== "vbscript") {
    return [];
  }
  return getVbscriptReferences(
    cached.parsed,
    params.position,
    buildVbProjectContext(cached, cachedSettings(cached.source.uri)),
    { includeDeclaration: params.context.includeDeclaration },
  ).map((reference) => Location.create(reference.uri, reference.range));
});

connection.onPrepareRename((params) => {
  const cached = getFreshCached(params.textDocument.uri);
  if (!cached) {
    return null;
  }
  if (isJavaScriptPosition(cached, params.position)) {
    return (
      jsPrepareRename(cached, params.position) ??
      crossLanguagePrepareRename(cached, params.position)
    );
  }
  if (isHtmlPosition(cached, params.position)) {
    return (
      crossLanguagePrepareRename(cached, params.position) ??
      htmlPrepareRename(cached, params.position)
    );
  }
  if (isCssPosition(cached, params.position)) {
    return (
      crossLanguagePrepareRename(cached, params.position) ??
      cssPrepareRename(cached, params.position)
    );
  }
  if (!isVbscriptPosition(cached, params.position)) {
    return null;
  }
  return (
    getVbscriptRenameRange(
      cached.parsed,
      params.position,
      buildVbProjectContext(cached, cachedSettings(cached.source.uri)),
    ) ?? null
  );
});

connection.onRenameRequest((params: RenameParams): WorkspaceEdit | null => {
  const cached = getFreshCached(params.textDocument.uri);
  if (!cached) {
    return null;
  }
  if (isJavaScriptPosition(cached, params.position)) {
    return (
      mergeWorkspaceEdits([
        jsRename(cached, params.position, params.newName),
        crossLanguageRename(cached, params.position, params.newName),
      ]) ?? null
    );
  }
  if (isHtmlPosition(cached, params.position)) {
    return (
      mergeWorkspaceEdits([
        htmlRename(cached, params.position, params.newName),
        crossLanguageRename(cached, params.position, params.newName),
      ]) ?? null
    );
  }
  if (isCssPosition(cached, params.position)) {
    return (
      mergeWorkspaceEdits([
        cssRename(cached, params.position, params.newName),
        crossLanguageRename(cached, params.position, params.newName),
      ]) ?? null
    );
  }
  if (!isVbscriptPosition(cached, params.position)) {
    return null;
  }
  const context = buildVbProjectContext(cached, cachedSettings(cached.source.uri));
  const range = getVbscriptRenameRange(cached.parsed, params.position, context);
  if (!range || !/^[A-Za-z][A-Za-z0-9_]*$/.test(params.newName)) {
    return null;
  }
  const changes: WorkspaceEdit["changes"] = {};
  for (const reference of getVbscriptReferences(cached.parsed, params.position, context)) {
    const edits = changes[reference.uri] ?? [];
    edits.push({ range: reference.range, newText: params.newName });
    changes[reference.uri] = edits;
  }
  return { changes };
});

connection.onDocumentHighlight((params): DocumentHighlight[] => {
  const cached = getFreshCached(params.textDocument.uri);
  if (!cached) {
    return [];
  }
  const region = findRegionAt(cached.parsed, cached.source.offsetAt(params.position));
  if (region?.language === "vbscript") {
    return getVbscriptDocumentHighlights(
      cached.parsed,
      params.position,
      bestEffortVbProjectContext(cached, cachedSettings(cached.source.uri)),
    );
  }
  if (region && isJavaScriptLikeRegion(region)) {
    return jsDocumentHighlights(cached, params.position);
  }
  if (region?.language === "html") {
    return htmlDocumentHighlights(cached, params.position);
  }
  if (region?.language === "css") {
    return cssDocumentHighlights(cached, params.position);
  }
  return [];
});

connection.onSignatureHelp((params): SignatureHelp | null => {
  const cached = getFreshCached(params.textDocument.uri);
  if (!cached) {
    return null;
  }
  if (isJavaScriptPosition(cached, params.position)) {
    return jsSignatureHelp(cached, params.position);
  }
  if (!isVbscriptPosition(cached, params.position)) {
    return null;
  }
  return (
    getVbscriptSignatureHelp(
      cached.parsed,
      params.position,
      immediateVbProjectContext(cached, cachedSettings(cached.source.uri)),
    ) ?? null
  );
});

connection.onWorkspaceSymbol(async (params, token) => {
  await ensureWorkspaceIndexAsync(globalSettings, token);
  const query = params.query.toLowerCase();
  const openedUris = new Set(documents.all().map((document) => document.uri));
  const matchesQuery = (name: string): boolean =>
    query.length === 0 || name.toLowerCase().includes(query);
  const indexedSymbols = (
    await mapWithConcurrency(
      [...workspaceIndex.values()].filter((entry) => !openedUris.has(entry.uri)),
      analysisConcurrency(globalSettings),
      async (entry) => {
        if (token.isCancellationRequested) {
          return [];
        }
        const cached = await cachedFromIndexedAsync(entry, cachedSettings(entry.uri));
        return [
          ...collectVbscriptSymbols(cached.parsed)
            .filter((symbol) => matchesQuery(symbol.name))
            .map(vbSymbolInformation),
          ...workspaceSymbolsForCached(cached).filter((symbol) => matchesQuery(symbol.name)),
        ];
      },
    )
  ).flat();
  const openSymbols = documents.all().flatMap((document) => {
    const cached = ensureFreshCachedDocument(document);
    return cached
      ? (buildVbProjectContext(cached, cachedSettings(document.uri)).symbols ?? []).filter(
          (symbol) => matchesQuery(symbol.name),
        )
      : [];
  });
  const vbSymbols = openSymbols.map(vbSymbolInformation);
  const openRichSymbols = documents.all().flatMap((document) => {
    const cached = ensureFreshCachedDocument(document);
    return cached
      ? workspaceSymbolsForCached(cached).filter((symbol) => matchesQuery(symbol.name))
      : [];
  });
  return [...vbSymbols, ...indexedSymbols, ...openRichSymbols];
});

connection.onDocumentSymbol((params) => {
  const cached = getFreshCached(params.textDocument.uri);
  if (!cached) {
    return [];
  }
  const htmlVirtual = getCachedVirtual(cached, "html");
  const htmlSymbols: DocumentSymbol[] = htmlVirtual
    ? htmlService.findDocumentSymbols2(
        toTextDocument(htmlVirtual),
        htmlService.parseHTMLDocument(toTextDocument(htmlVirtual)),
      )
    : [];
  return [
    ...htmlSymbols,
    ...cssDocumentSymbols(cached),
    ...jsDocumentSymbols(cached),
    ...getVbscriptDocumentSymbols(cached.parsed),
  ];
});

connection.onFoldingRanges((params) => {
  const cached = getFreshCached(params.textDocument.uri);
  if (!cached) {
    return [];
  }
  const htmlVirtual = getCachedVirtual(cached, "html");
  const htmlRanges: FoldingRange[] = htmlVirtual
    ? htmlService.getFoldingRanges(toTextDocument(htmlVirtual), {})
    : [];
  const cssRanges = cssFoldingRanges(cached);
  const jsRanges = jsFoldingRanges(cached);
  const vbRanges = vbscriptFoldingRanges(cached);
  const aspRanges: FoldingRange[] = cached.parsed.regions
    .filter(
      (region) =>
        region.kind !== "html" &&
        cached.source.positionAt(region.start).line !== cached.source.positionAt(region.end).line,
    )
    .map((region) => ({
      startLine: cached.source.positionAt(region.start).line,
      endLine: cached.source.positionAt(region.end).line,
    }));
  return [...htmlRanges, ...cssRanges, ...jsRanges, ...vbRanges, ...aspRanges];
});

connection.onDocumentLinks((params) => {
  const cached = getFreshCached(params.textDocument.uri);
  if (!cached) {
    return [];
  }
  return cached.parsed.includes.map((include): DocumentLink => {
    const targetPath = resolveIncludePath(
      cached.source.uri,
      include.path,
      include.mode,
      cachedSettings(cached.source.uri),
    );
    return { range: include.pathRange, target: pathToFileUri(targetPath) };
  });
});

connection.onSelectionRanges((params): SelectionRange[] => {
  const cached = getFreshCached(params.textDocument.uri);
  if (!cached) {
    return [];
  }
  return params.positions.map((position) => selectionRangeAt(cached, position));
});

connection.languages.inlayHint.on((params): InlayHint[] =>
  runInteractiveLanguageFeature(() => {
    const cached = getFreshCached(params.textDocument.uri);
    if (!cached) {
      return [];
    }
    return [
      ...getVbscriptInlayHints(
        cached.parsed,
        params.range,
        bestEffortVbProjectContext(cached, cachedSettings(cached.source.uri)),
        cachedSettings(cached.source.uri).inlayHints,
      ),
      ...jsInlayHints(cached, params.range),
    ];
  }),
);

connection.languages.diagnostics.on(async (params) => {
  noteForegroundActivity();
  const cached =
    getFreshCached(params.textDocument.uri) ?? getIndexedCached(params.textDocument.uri);
  return {
    kind: "full" as const,
    items: cached ? await diagnosticsForCachedAsync(cached, cachedSettings(cached.source.uri)) : [],
  };
});

connection.languages.diagnostics.onWorkspace(async (_params, token) => {
  noteForegroundActivity();
  await ensureWorkspaceIndexAsync(globalSettings, token);
  const openedUris = new Set(documents.all().map((document) => document.uri));
  const concurrency = analysisConcurrency(globalSettings);
  const indexedEntries = workspaceEntriesAffectedFirst(
    [...workspaceIndex.values()].filter((entry) => !openedUris.has(entry.uri)),
  );
  const indexedItems = await mapWithConcurrency(indexedEntries, concurrency, async (entry) => ({
    kind: "full" as const,
    uri: entry.uri,
    version: null,
    items: await diagnosticsForIndexed(entry, cachedSettings(entry.uri), token),
  }));
  const openItems = await mapWithConcurrency(documents.all(), concurrency, async (document) => {
    const cached = ensureFreshCachedDocument(document);
    return cached
      ? {
          kind: "full" as const,
          uri: document.uri,
          version: document.version,
          items: await diagnosticsForCachedAsync(
            cached,
            cachedSettings(document.uri),
            "check.workspace",
            neverCancelled,
            "workspace",
          ),
        }
      : undefined;
  });
  return {
    items: [...openItems.filter((item) => item !== undefined), ...indexedItems],
  };
});

connection.onExecuteCommand(async (params) => {
  if (
    params.command === reindexWorkspaceCommand ||
    params.command === reindexWorkspaceServerCommand
  ) {
    invalidateWorkspaceIndex("command.reindexWorkspace");
    invalidateIncludeResolution("command.reindexWorkspace");
    invalidateJsProject("command.reindexWorkspace");
    invalidateCachedAnalysisForUris(openDocumentUris(), "command.reindexWorkspace");
    for (const document of documents.all()) {
      validate(document);
    }
    scheduleBackgroundAnalysis("command.reindexWorkspace");
    return { ok: true };
  }
  if (params.command === clearCacheCommand || params.command === clearCacheServerCommand) {
    diskAnalysisCache.clear();
    vbProjectContextCache.clear();
    clearJsProjectCaches();
    logDebugSummary(globalSettings, "[asp-lsp] diskCache.clear");
    return { ok: true };
  }
  return {
    ok: false,
    message: createLocalizer(globalSettings.resolvedLocale).t("server.unknownCommand", {
      command: params.command,
    }),
  };
});

connection.languages.callHierarchy.onPrepare((params): CallHierarchyItem[] => {
  const cached = getFreshCached(params.textDocument.uri);
  if (!cached) {
    return [];
  }
  if (isJavaScriptPosition(cached, params.position)) {
    return jsPrepareCallHierarchy(cached, params.position);
  }
  if (!isVbscriptPosition(cached, params.position)) {
    return [];
  }
  return prepareVbscriptCallHierarchy(
    cached.parsed,
    params.position,
    buildVbProjectContext(cached, cachedSettings(cached.source.uri)),
    cached.source.uri,
  );
});

connection.languages.callHierarchy.onIncomingCalls((params): CallHierarchyIncomingCall[] => {
  if (isJsCallHierarchyItem(params.item)) {
    return jsIncomingCalls(params.item);
  }
  const root = callHierarchyRootUri(params.item);
  const cached = getFreshCached(root) ?? getFreshCached(params.item.uri);
  if (!cached) {
    return [];
  }
  return getVbscriptIncomingCalls(
    params.item,
    buildVbProjectContext(cached, cachedSettings(cached.source.uri)),
  );
});

connection.languages.callHierarchy.onOutgoingCalls((params): CallHierarchyOutgoingCall[] => {
  if (isJsCallHierarchyItem(params.item)) {
    return jsOutgoingCalls(params.item);
  }
  const root = callHierarchyRootUri(params.item);
  const cached = getFreshCached(root) ?? getFreshCached(params.item.uri);
  if (!cached) {
    return [];
  }
  return getVbscriptOutgoingCalls(
    params.item,
    buildVbProjectContext(cached, cachedSettings(cached.source.uri)),
  );
});

connection.languages.typeHierarchy.onPrepare((params): TypeHierarchyItem[] => {
  const cached = getFreshCached(params.textDocument.uri);
  if (!cached) {
    return [];
  }
  const item = vbTypeHierarchyItemAt(cached, params.position);
  return item ? [item] : [];
});

connection.languages.typeHierarchy.onSupertypes((): TypeHierarchyItem[] => []);

connection.languages.typeHierarchy.onSubtypes((params): TypeHierarchyItem[] =>
  vbTypeHierarchyRelatedItems(params.item),
);

connection.languages.moniker.on((params): Moniker[] => {
  const cached = getFreshCached(params.textDocument.uri);
  if (!cached) {
    return [];
  }
  return monikersAt(cached, params.position);
});

connection.languages.inlineValue.on((params: InlineValueParams): InlineValue[] => {
  const cached = getFreshCached(params.textDocument.uri);
  if (!cached) {
    return [];
  }
  return inlineValues(cached, params.range);
});

connection.languages.onLinkedEditingRange((params): LinkedEditingRanges | null => {
  const cached = getFreshCached(params.textDocument.uri);
  if (!cached) {
    return null;
  }
  const ranges = htmlLinkedRanges(cached, params.position);
  return ranges ? { ranges } : null;
});

function htmlLinkedRanges(cached: CachedDocument, position: Position): Range[] | null {
  const region = findRegionAt(cached.parsed, cached.source.offsetAt(position));
  if (!region || region.language !== "html") {
    return null;
  }
  const virtual = getCachedVirtual(cached, "html");
  if (!virtual) {
    return null;
  }
  const doc = toTextDocument(virtual);
  return htmlService.findLinkedEditingRanges(doc, position, htmlService.parseHTMLDocument(doc));
}

connection.onDocumentColor((params): ColorInformation[] => {
  const cached = getFreshCached(params.textDocument.uri);
  if (!cached) {
    return [];
  }
  return cssDocumentColors(cached);
});

connection.onColorPresentation((params): ColorPresentation[] => {
  const cached = getFreshCached(params.textDocument.uri);
  if (!cached) {
    return [];
  }
  return cssColorPresentations(cached, params.color, params.range);
});

connection.onCodeLens((params): CodeLens[] => {
  const cached = getFreshCached(params.textDocument.uri);
  if (!cached) {
    return [];
  }
  return codeLenses(cached);
});

connection.onDocumentFormatting((params) => {
  const cached = getFreshCached(params.textDocument.uri);
  if (!cached) {
    return [];
  }
  return formatAspDocumentWithDelegates(cached, params.options);
});

connection.onDocumentRangeFormatting((params) => {
  const cached = getFreshCached(params.textDocument.uri);
  if (!cached) {
    return [];
  }
  const region = findRegionAt(cached.parsed, cached.source.offsetAt(params.range.start));
  if (!region || region.language !== "html") {
    return formatAspRangeWithDelegates(cached, params.range, params.options);
  }
  const virtual = getCachedVirtual(cached, "html");
  if (!virtual) {
    return [];
  }
  if (rangeOverlapsNonHtml(cached, params.range)) {
    return formatAspRangeWithDelegates(cached, params.range, params.options);
  }
  return htmlService.format(toTextDocument(virtual), params.range, {
    tabSize: params.options.tabSize,
    insertSpaces: params.options.insertSpaces,
  }) as TextEdit[];
});

connection.onCodeAction((params): CodeAction[] => {
  const cached = getFreshCached(params.textDocument.uri);
  if (!cached) {
    return [];
  }
  return [
    ...params.context.diagnostics.flatMap((diagnostic) =>
      quickFixesForDiagnostic(cached, diagnostic),
    ),
    ...vbscriptCodeActions(cached, params.range, params.context),
    ...cssCodeActions(cached, params.range, params.context),
    ...jsCodeActions(cached, params.range, params.context),
  ];
});

connection.onCodeActionResolve((action) => action);

connection.onCodeLensResolve((lens) => lens);

connection.onDocumentLinkResolve((link) => link);

connection.languages.inlayHint.resolve((hint) => hint);

connection.onDocumentOnTypeFormatting((params) => {
  const cached = getFreshCached(params.textDocument.uri);
  if (!cached) {
    return [];
  }
  return onTypeFormatting(cached, params.position, params.ch, params.options);
});

connection.languages.semanticTokens.on((params) => {
  const cached = getFreshCached(params.textDocument.uri);
  if (!cached) {
    return { data: [] };
  }
  return cacheSemanticTokens(cached.source.uri, buildSemanticTokens(cached).data);
});

connection.languages.semanticTokens.onRange((params): SemanticTokens => {
  const cached = getFreshCached(params.textDocument.uri);
  if (!cached) {
    return { data: [] };
  }
  return buildSemanticTokens(cached, params.range);
});

connection.languages.semanticTokens.onDelta((params): SemanticTokens | SemanticTokensDelta => {
  const cached = getFreshCached(params.textDocument.uri);
  if (!cached) {
    return { data: [] };
  }
  const previous = semanticTokenResults.get(params.previousResultId);
  const next = buildSemanticTokens(cached).data;
  if (!previous || previous.uri !== cached.source.uri) {
    return cacheSemanticTokens(cached.source.uri, next);
  }
  const resultId = nextSemanticTokenResultId();
  semanticTokenResults.set(resultId, { uri: cached.source.uri, data: next });
  latestSemanticTokenResultByUri.set(cached.source.uri, resultId);
  semanticTokenResults.delete(params.previousResultId);
  return {
    resultId,
    edits: [semanticTokenDeltaEdit(previous.data, next)],
  };
});

function validate(document: TextDocument): void {
  cancelScheduledDiagnostics(document.uri);
  const cached = ensureFreshCachedDocument(document);
  startStagedDiagnostics(cached, cachedSettings(document.uri), true, {
    preservePreviousDiagnosticsUntilFinal: hasPublishedDiagnostics(document.uri),
  });
}

function refreshCachedDocument(document: TextDocument, impactReason?: string): CachedDocument {
  const startedAt = process.hrtime.bigint();
  const settingsStartedAt = startedAt;
  const settings = cachedSettings(document.uri);
  startAnalysisLog(settings, document.uri);
  finishDebugStep(settings, document.uri, "analysis.settings", settingsStartedAt);
  const parseStartedAt = process.hrtime.bigint();
  const parsed = parseAspDocument(document.uri, document.getText(), settings);
  finishDebugStep(settings, document.uri, "analysis.parse.full", parseStartedAt);
  finishDebugStep(
    settings,
    document.uri,
    "analysis.virtualDocuments.lazy",
    process.hrtime.bigint(),
  );
  if (impactReason) {
    logDebugSummary(
      settings,
      `[asp-lsp] analysis.parse.impact: ${document.uri}, mode=full, reason=${impactReason}`,
    );
  }
  const cacheStartedAt = process.hrtime.bigint();
  const cached = createCachedDocument(document, parsed, settings);
  cache.set(document.uri, cached);
  finishDebugStep(settings, document.uri, "analysis.cacheUpdate", cacheStartedAt);
  finishAnalysisLog(settings, document.uri, startedAt, "full");
  return cached;
}

function refreshCachedDocumentIncremental(
  previous: CachedDocument,
  document: TextDocument,
  settings: AspSettings,
  change: AspIncrementalChange,
): CachedDocument {
  const startedAt = process.hrtime.bigint();
  const settingsStartedAt = startedAt;
  startAnalysisLog(settings, document.uri);
  finishDebugStep(settings, document.uri, "analysis.settings", settingsStartedAt);
  const parseStartedAt = process.hrtime.bigint();
  const updated = updateAspParsedDocument(previous.parsed, [change], settings);
  finishDebugStep(
    settings,
    document.uri,
    updated.impact.kind === "incremental" ? "analysis.parse.incremental" : "analysis.parse.full",
    parseStartedAt,
  );
  logDebugSummary(
    settings,
    `[asp-lsp] analysis.parse.impact: ${document.uri}, mode=${updated.impact.kind}, reason=${updated.impact.reason}`,
  );
  finishDebugStep(
    settings,
    document.uri,
    "analysis.virtualDocuments.lazy",
    process.hrtime.bigint(),
  );
  const cacheStartedAt = process.hrtime.bigint();
  const editHistory =
    updated.impact.kind === "incremental"
      ? [...previous.editHistory, updated.impact].slice(-8)
      : [];
  const cached = createCachedDocument(document, updated.parsed, settings, editHistory);
  cached.lastEditImpact = updated.impact;
  cached.lastIncrementalChange = change;
  seedVbReuseAfterIncrementalChange(previous, cached, settings, change, updated.impact);
  cache.set(document.uri, cached);
  finishDebugStep(settings, document.uri, "analysis.cacheUpdate", cacheStartedAt);
  finishAnalysisLog(settings, document.uri, startedAt, updated.impact.kind);
  return cached;
}

function ensureFreshCachedDocument(document: TextDocument): CachedDocument {
  const existing = cache.get(document.uri);
  const settings = cachedSettings(document.uri);
  if (
    existing &&
    sameDocumentIdentity(existing.identity, documentIdentityFor(document)) &&
    existing.parseSettingsIdentity === parseSettingsIdentity(settings)
  ) {
    updateCachedDocumentRuntimeIdentity(existing, settings);
    return existing;
  }
  if (existing && existing.parseSettingsIdentity === parseSettingsIdentity(settings)) {
    const pending = pendingDocumentChanges.get(document.uri);
    if (pending?.version === document.version) {
      pendingDocumentChanges.delete(document.uri);
      if (pending.ranged && pending.changes.length === 1) {
        return refreshCachedDocumentIncremental(existing, document, settings, pending.changes[0]);
      }
      return refreshCachedDocument(document, pending?.reason ?? "non-incremental document change");
    }
  }
  pendingDocumentChanges.delete(document.uri);
  return refreshCachedDocument(document);
}

function getFreshCached(uri: string): CachedDocument | undefined {
  const document = documents.get(uri);
  return document ? ensureFreshCachedDocument(document) : getCached(uri);
}

function createCachedDocument(
  document: TextDocument,
  parsed: AspParsedDocument,
  settings: AspSettings,
  editHistory: AspEditImpact[] = [],
): CachedDocument {
  const cached: CachedDocument = {
    source: document,
    parsed,
    virtuals: new Map<string, VirtualDocument>(),
    identity: documentIdentityFor(document),
    generation: ++documentCacheGeneration,
    parseSettingsIdentity: parseSettingsIdentity(settings),
    includeResolutionIdentity: includeResolutionIdentity(settings),
    diagnosticsIdentity: diagnosticsIdentity(settings),
    jsProjectIdentity: jsProjectIdentity(settings),
    workspaceGeneration,
    includeResolutionGeneration,
    jsProjectGeneration,
    editHistory,
  };
  return cached;
}

function updateCachedDocumentRuntimeIdentity(cached: CachedDocument, settings: AspSettings): void {
  cached.includeResolutionIdentity = includeResolutionIdentity(settings);
  cached.diagnosticsIdentity = diagnosticsIdentity(settings);
  cached.jsProjectIdentity = jsProjectIdentity(settings);
  cached.workspaceGeneration = workspaceGeneration;
  cached.includeResolutionGeneration = includeResolutionGeneration;
  cached.jsProjectGeneration = jsProjectGeneration;
}

function documentIdentityFor(document: TextDocument): DocumentIdentity {
  return {
    uri: document.uri,
    version: document.version,
    text: textFingerprint(document.getText()),
  };
}

function sameDocumentIdentity(left: DocumentIdentity, right: DocumentIdentity): boolean {
  return left.uri === right.uri && left.version === right.version && left.text === right.text;
}

function pendingChangeFromContentChanges(
  version: number,
  changes: Array<{
    range?: Range;
    rangeLength?: number;
    text: string;
  }>,
): PendingDocumentChange {
  if (changes.length !== 1) {
    return {
      version,
      changes: [],
      reason: "multiple content changes",
      ranged: false,
    };
  }
  const change = changes[0];
  if (!change.range) {
    return {
      version,
      changes: [],
      reason: "full document replacement",
      ranged: false,
    };
  }
  return {
    version,
    changes: [
      {
        range: change.range,
        rangeLength: change.rangeLength,
        text: change.text,
      },
    ],
    reason: "single ranged edit",
    ranged: true,
  };
}

function startAnalysisLog(settings: AspSettings, uri: string): void {
  logDebugSummary(settings, `[asp-lsp] LSP analysis started: ${uri}`);
}

function finishAnalysisLog(
  settings: AspSettings,
  uri: string,
  startedAt: bigint,
  mode: AspEditImpact["kind"] = "full",
): void {
  const elapsedMs = Number(process.hrtime.bigint() - startedAt) / 1_000_000;
  logDebugSummary(
    settings,
    `[asp-lsp] LSP analysis completed: ${uri} ${formatElapsedMs(elapsedMs)}, mode=${mode}`,
  );
}

function scheduleDiagnostics(document: TextDocument): void {
  cancelScheduledDiagnostics(document.uri);
  const settings = cachedSettings(document.uri);
  const cached = ensureFreshCachedDocument(document);
  const state = startStagedDiagnostics(cached, settings, false, {
    preservePreviousDiagnosticsUntilFinal: hasPublishedDiagnostics(document.uri),
  });
  const delay = settings.diagnostics?.debounceMs ?? defaultDiagnosticsDebounceMs;
  if (delay <= 0) {
    void runStagedDiagnostics(cached, settings, state);
    return;
  }
  diagnosticsTimers.set(
    document.uri,
    setTimeout(() => {
      diagnosticsTimers.delete(document.uri);
      if (!isCurrentStagedDiagnostics(cached, state)) {
        logStaleStagedDiagnostics(settings, state, "include");
        return;
      }
      void runStagedDiagnostics(cached, settings, state);
    }, delay),
  );
}

function cancelScheduledDiagnostics(uri: string): void {
  const timer = diagnosticsTimers.get(uri);
  if (!timer) {
    return;
  }
  clearTimeout(timer);
  diagnosticsTimers.delete(uri);
}

function hasPublishedDiagnostics(uri: string): boolean {
  return (publishedDiagnosticsByUri.get(uri)?.diagnostics.length ?? 0) > 0;
}

function startStagedDiagnostics(
  cached: CachedDocument,
  settings: AspSettings,
  runAsyncLayers = true,
  options: { preservePreviousDiagnosticsUntilFinal?: boolean } = {},
): StagedDiagnosticsState {
  const state: StagedDiagnosticsState = {
    generation: ++stagedDiagnosticsGeneration,
    uri: cached.source.uri,
    version: cached.source.version,
    documentGeneration: cached.generation,
    diagnosticsIdentity: cached.diagnosticsIdentity,
    startedAt: startCheckLog(cached, settings),
    preservePreviousDiagnosticsUntilFinal: options.preservePreviousDiagnosticsUntilFinal === true,
    layers: {},
  };
  stagedDiagnosticsByUri.set(cached.source.uri, state);
  state.layers.fast = measureDebugStep(
    settings,
    cached.source.uri,
    "check.parserDiagnostics",
    () => cached.parsed.diagnostics,
  );
  publishStagedDiagnosticsLayer(cached, settings, state, "fast");
  if (runAsyncLayers) {
    void runStagedDiagnostics(cached, settings, state);
  }
  return state;
}

async function runStagedDiagnostics(
  cached: CachedDocument,
  settings: AspSettings,
  state: StagedDiagnosticsState,
): Promise<void> {
  const cancellation: AnalysisCancellation = {
    isCancellationRequested: () => !isCurrentStagedDiagnostics(cached, state),
  };
  const includeItems = await includeDiagnosticsForCachedAsync(
    cached,
    settings,
    "check",
    cancellation,
  );
  if (!isCurrentStagedDiagnostics(cached, state)) {
    logStaleStagedDiagnostics(settings, state, "include");
    return;
  }
  state.layers.include = includeItems;
  publishStagedDiagnosticsLayer(cached, settings, state, "include");
  await yieldToEventLoop();

  if (!isCurrentStagedDiagnostics(cached, state)) {
    logStaleStagedDiagnostics(settings, state, "syntax");
    return;
  }
  state.layers.syntax = syntaxDiagnosticsForCached(cached, settings, "check");
  publishStagedDiagnosticsLayer(cached, settings, state, "syntax");
  await yieldToEventLoop();

  if (!isCurrentStagedDiagnostics(cached, state)) {
    logStaleStagedDiagnostics(settings, state, "project");
    return;
  }
  state.layers.project = await projectDiagnosticsForCachedAsync(
    cached,
    settings,
    "check",
    cancellation,
    "foreground",
  );
  publishStagedDiagnosticsLayer(cached, settings, state, "project");
  if (!isCurrentStagedDiagnostics(cached, state)) {
    logStaleStagedDiagnostics(settings, state, "final");
    return;
  }
  const finalItems = publishStagedDiagnosticsLayer(cached, settings, state, "final");
  finishCheckLog(cached, settings, state.startedAt, finalItems.length);
}

function publishStagedDiagnosticsLayer(
  cached: CachedDocument,
  settings: AspSettings,
  state: StagedDiagnosticsState,
  layer: DiagnosticLayerKey,
): Diagnostic[] {
  if (!isCurrentStagedDiagnostics(cached, state)) {
    logStaleStagedDiagnostics(settings, state, layer);
    return [];
  }
  const startedAt = process.hrtime.bigint();
  const items = measureDebugStep(settings, cached.source.uri, `diagnostics.${layer}.dedupe`, () =>
    dedupeDiagnostics(stagedDiagnosticsItems(state)),
  );
  if (layer === "final") {
    state.layers.final = items;
  }
  if (shouldPreservePublishedDiagnostics(state, layer)) {
    finishDebugStep(settings, cached.source.uri, `diagnostics.${layer}.preserve`, startedAt);
    logDebugSummary(
      settings,
      `[asp-lsp] diagnostics.${layer}.preserved: ${cached.source.uri}, generation=${state.generation}, diagnostics=${items.length}, previous=${publishedDiagnosticsByUri.get(state.uri)?.diagnostics.length ?? 0}`,
    );
    return items;
  }
  connection.sendDiagnostics({
    uri: cached.source.uri,
    version: state.version,
    diagnostics: items,
  });
  publishedDiagnosticsByUri.set(cached.source.uri, {
    version: state.version,
    diagnostics: items,
  });
  finishDebugStep(settings, cached.source.uri, `diagnostics.${layer}.publish`, startedAt);
  logDebugSummary(
    settings,
    `[asp-lsp] diagnostics.${layer}.published: ${cached.source.uri}, generation=${state.generation}, diagnostics=${items.length}`,
  );
  return items;
}

function stagedDiagnosticsItems(state: StagedDiagnosticsState): Diagnostic[] {
  return [
    ...(state.layers.fast ?? []),
    ...(state.layers.include ?? []),
    ...(state.layers.syntax ?? []),
    ...(state.layers.project ?? []),
  ];
}

function shouldPreservePublishedDiagnostics(
  state: StagedDiagnosticsState,
  layer: DiagnosticLayerKey,
): boolean {
  return (
    state.preservePreviousDiagnosticsUntilFinal &&
    layer !== "final" &&
    hasPublishedDiagnostics(state.uri)
  );
}

function isCurrentStagedDiagnostics(
  cached: CachedDocument,
  state: StagedDiagnosticsState,
): boolean {
  const document = documents.get(state.uri);
  const active = stagedDiagnosticsByUri.get(state.uri);
  return (
    active?.generation === state.generation &&
    document?.version === state.version &&
    cache.get(state.uri)?.generation === state.documentGeneration &&
    cached.generation === state.documentGeneration &&
    cache.get(state.uri)?.diagnosticsIdentity === state.diagnosticsIdentity
  );
}

function logStaleStagedDiagnostics(
  settings: AspSettings,
  state: StagedDiagnosticsState,
  layer: DiagnosticLayerKey,
): void {
  logDebugSummary(
    settings,
    `[asp-lsp] diagnostics.${layer}.stale: ${state.uri}, generation=${state.generation}`,
  );
}

async function diagnosticsForCachedAsync(
  cached: CachedDocument,
  settings: AspSettings,
  stepPrefix = "check.async",
  cancellation: AnalysisCancellation = neverCancelled,
  mode: AnalysisExecutionMode = "foreground",
): Promise<Diagnostic[]> {
  const parserItems = cached.parsed.diagnostics;
  const includeItems = await includeDiagnosticsForCachedAsync(
    cached,
    settings,
    stepPrefix,
    cancellation,
  );
  if (cancellation.isCancellationRequested()) {
    return [];
  }
  const syntaxItems = syntaxDiagnosticsForCached(cached, settings, stepPrefix);
  const projectItems = await projectDiagnosticsForCachedAsync(
    cached,
    settings,
    stepPrefix,
    cancellation,
    mode,
  );
  const items = measureDebugStep(settings, cached.source.uri, `${stepPrefix}.dedupe`, () =>
    dedupeDiagnostics([...parserItems, ...includeItems, ...syntaxItems, ...projectItems]),
  );
  aspProjectBuilderState.updateDiagnosticsLayer(
    cached.source.uri,
    "final",
    diagnosticsCacheKey(cached, settings),
    items,
    cached.parsed.text,
    settings,
  );
  return items;
}

async function includeDiagnosticsForCachedAsync(
  cached: CachedDocument,
  settings: AspSettings,
  stepPrefix = "check.include",
  cancellation: AnalysisCancellation = neverCancelled,
): Promise<Diagnostic[]> {
  const startedAt = process.hrtime.bigint();
  const items = await includeDiagnosticsAsync(cached, settings, cancellation);
  finishDebugStep(settings, cached.source.uri, `${stepPrefix}.includeDiagnostics`, startedAt);
  return items;
}

function syntaxDiagnosticsForCached(
  cached: CachedDocument,
  settings: AspSettings,
  stepPrefix: string,
): Diagnostic[] {
  return embeddedSyntaxDiagnostics(cached, settings, stepPrefix);
}

function embeddedSyntaxDiagnostics(
  cached: CachedDocument,
  settings: AspSettings,
  stepPrefix: string,
): Diagnostic[] {
  const diagnostics: Diagnostic[] = [];
  if (!isIncDocument(cached.source.uri)) {
    diagnostics.push(
      ...measureDebugStep(settings, cached.source.uri, `${stepPrefix}.htmlDiagnostics`, () =>
        htmlDiagnostics(cached),
      ),
    );
  }
  diagnostics.push(
    ...measureDebugStep(settings, cached.source.uri, `${stepPrefix}.cssDiagnostics`, () =>
      cssDiagnostics(cached),
    ),
  );
  diagnostics.push(
    ...measureDebugStep(settings, cached.source.uri, `${stepPrefix}.javascriptSyntax`, () =>
      jsSyntaxDiagnostics(cached),
    ),
  );
  return diagnostics;
}

async function projectDiagnosticsForCachedAsync(
  cached: CachedDocument,
  settings: AspSettings,
  stepPrefix: string,
  cancellation: AnalysisCancellation = neverCancelled,
  mode: AnalysisExecutionMode = "foreground",
): Promise<Diagnostic[]> {
  const vbItemsPromise = vbDiagnosticsAsync(cached, settings, stepPrefix, cancellation, mode);
  const jsItems = jsSlowDiagnostics(cached, settings, stepPrefix);
  const vbItems = await vbItemsPromise;
  if (cancellation.isCancellationRequested()) {
    return [];
  }
  return measureDebugStep(settings, cached.source.uri, `${stepPrefix}.project.dedupe`, () =>
    dedupeDiagnostics([...vbItems, ...jsItems]),
  );
}

function diagnosticsCacheKey(cached: CachedDocument, settings: AspSettings): string {
  return JSON.stringify({
    diagnostics: diagnosticsIdentity(settings),
    text: textFingerprint(cached.parsed.text),
    includeResolution: cached.includeResolutionGeneration,
    jsProject: cached.jsProjectGeneration,
    workspace: cached.workspaceGeneration,
  });
}

function workspaceEntriesAffectedFirst(
  entries: WorkspaceIndexedDocument[],
): WorkspaceIndexedDocument[] {
  if (entries.length === 0 || aspProjectBuilderState.affectedCount() === 0) {
    return entries;
  }
  const byUri = new Map(entries.map((entry) => [entry.uri, entry]));
  const selected: WorkspaceIndexedDocument[] = [];
  let uri: string | undefined;
  while ((uri = aspProjectBuilderState.takeNextAffectedUri())) {
    const entry = byUri.get(uri);
    if (!entry) {
      continue;
    }
    selected.push(entry);
    byUri.delete(uri);
  }
  return [...selected, ...entries.filter((entry) => byUri.has(entry.uri))];
}

function startCheckLog(cached: CachedDocument, settings: AspSettings): bigint {
  logDebugSummary(settings, `[asp-lsp] LSP check started: ${cached.source.uri}`);
  return process.hrtime.bigint();
}

function finishCheckLog(
  cached: CachedDocument,
  settings: AspSettings,
  startedAt: bigint,
  diagnosticCount: number,
): void {
  const elapsedMs = Number(process.hrtime.bigint() - startedAt) / 1_000_000;
  logDebugSummary(
    settings,
    `[asp-lsp] LSP check completed: ${cached.source.uri} ${formatElapsedMs(elapsedMs)}, diagnostics=${diagnosticCount}`,
  );
}

function logDebugSummary(settings: AspSettings, message: string): void {
  if (isDebugSummaryEnabled(settings)) {
    connection.console.info(message);
  }
}

function finishDebugStep(
  settings: AspSettings,
  uri: string,
  step: string,
  startedAt: bigint,
): void {
  logDebugElapsed(settings, uri, step, Number(process.hrtime.bigint() - startedAt) / 1_000_000);
}

function logDebugElapsed(
  settings: AspSettings,
  uri: string,
  step: string,
  elapsedMs: number,
): void {
  if (!isDebugVerboseEnabled(settings)) {
    return;
  }
  connection.console.info(`[asp-lsp] ${step}: ${uri} ${formatElapsedMs(elapsedMs)}`);
}

function measureDebugStep<T>(
  settings: AspSettings,
  uri: string,
  step: string,
  callback: () => T,
): T {
  const startedAt = process.hrtime.bigint();
  try {
    return callback();
  } finally {
    finishDebugStep(settings, uri, step, startedAt);
  }
}

async function measureDebugStepAsync<T>(
  settings: AspSettings,
  uri: string,
  step: string,
  callback: () => Promise<T>,
): Promise<T> {
  const startedAt = process.hrtime.bigint();
  try {
    return await callback();
  } finally {
    finishDebugStep(settings, uri, step, startedAt);
  }
}

function availableAnalysisConcurrency(): number {
  return Math.max(1, os.availableParallelism());
}

function defaultBusyAnalysisConcurrency(): number {
  return Math.max(1, Math.floor(availableAnalysisConcurrency() / 2));
}

function defaultIdleAnalysisConcurrency(): number {
  return Math.max(1, availableAnalysisConcurrency() - 1);
}

const neverCancelled: AnalysisCancellation = {
  isCancellationRequested: () => false,
};

async function fileExistsAsync(fileName: string): Promise<boolean> {
  const stat = await fs.promises.stat(fileName).catch(() => undefined);
  return Boolean(stat?.isFile());
}

async function mapWithConcurrency<T, U>(
  items: readonly T[],
  concurrency: number,
  callback: (item: T, index: number) => Promise<U>,
): Promise<U[]> {
  const results = Array.from<U>({ length: items.length });
  let next = 0;
  const workers = Array.from({ length: Math.min(concurrency, items.length) }, async () => {
    while (next < items.length) {
      const index = next;
      next += 1;
      results[index] = await callback(items[index], index);
    }
  });
  await Promise.all(workers);
  return results;
}

function clampAnalysisConcurrency(value: number | undefined, fallback: number): number {
  if (typeof value !== "number" || value <= 0) {
    return fallback;
  }
  return Math.max(1, Math.min(availableAnalysisConcurrency(), Math.floor(value)));
}

function busyAnalysisConcurrency(settings: AspSettings): number {
  return clampAnalysisConcurrency(
    settings.workspace?.busyAnalysisConcurrency,
    defaultBusyAnalysisConcurrency(),
  );
}

function idleAnalysisConcurrency(settings: AspSettings): number {
  return clampAnalysisConcurrency(
    settings.workspace?.idleAnalysisConcurrency,
    defaultIdleAnalysisConcurrency(),
  );
}

function analysisConcurrency(settings: AspSettings): number {
  return busyAnalysisConcurrency(settings);
}

function workerAnalysisConcurrency(settings: AspSettings, mode: "busy" | "idle" = "busy"): number {
  return mode === "idle" ? idleAnalysisConcurrency(settings) : busyAnalysisConcurrency(settings);
}

function noteForegroundActivity(): void {
  lastForegroundActivityAt = Date.now();
  if (
    globalSettings.workspace?.backgroundAnalysis === true &&
    (backgroundAnalysisTimer || backgroundAnalysisRunning || pendingBackgroundAnalysisReason)
  ) {
    scheduleBackgroundAnalysis(pendingBackgroundAnalysisReason ?? "foreground.defer");
  }
}

function scheduleProjectUpdate(reason: string): void {
  pendingProjectUpdateReason = reason;
  if (projectUpdateTimer) {
    clearTimeout(projectUpdateTimer);
  }
  projectUpdateTimer = setTimeout(() => {
    projectUpdateTimer = undefined;
    flushPendingProjectUpdates(reason);
  }, projectUpdateDelayMs);
  logDebugSummary(globalSettings, `[asp-lsp] projectUpdate.scheduled: reason=${reason}`);
}

function scheduleOpenFileProjectMaintenance(reason: string): void {
  pendingOpenFileMaintenanceReason = reason;
  if (openFileProjectMaintenanceTimer) {
    clearTimeout(openFileProjectMaintenanceTimer);
  }
  openFileProjectMaintenanceTimer = setTimeout(() => {
    openFileProjectMaintenanceTimer = undefined;
    flushOpenFileProjectMaintenance(reason);
  }, openFileProjectMaintenanceDelayMs);
  logDebugSummary(
    globalSettings,
    `[asp-lsp] openFileProjectMaintenance.scheduled: reason=${reason}`,
  );
}

function flushPendingProjectUpdates(
  reason = pendingProjectUpdateReason ?? "foreground.flush",
): void {
  if (projectUpdateTimer) {
    clearTimeout(projectUpdateTimer);
    projectUpdateTimer = undefined;
  }
  if (!pendingProjectUpdateReason && reason === "foreground.flush") {
    return;
  }
  const startedAt = process.hrtime.bigint();
  let refreshed = 0;
  for (const document of documents.all()) {
    const cached = ensureFreshCachedDocument(document);
    collectCachedVbProjectDocuments(cached, cachedSettings(document.uri));
    for (const virtual of jsVirtualDocuments(cached)) {
      createJsLanguageService(virtual, cachedSettings(document.uri));
    }
    refreshed += 1;
  }
  pendingProjectUpdateReason = undefined;
  logDebugElapsed(
    globalSettings,
    "workspace",
    "projectUpdate.flush",
    Number(process.hrtime.bigint() - startedAt) / 1_000_000,
  );
  logDebugSummary(
    globalSettings,
    `[asp-lsp] projectUpdate.flushed: reason=${reason}, openFiles=${refreshed}`,
  );
}

function flushOpenFileProjectMaintenance(
  reason = pendingOpenFileMaintenanceReason ?? "openFiles.maintenance",
): void {
  if (openFileProjectMaintenanceTimer) {
    clearTimeout(openFileProjectMaintenanceTimer);
    openFileProjectMaintenanceTimer = undefined;
  }
  if (!pendingOpenFileMaintenanceReason && reason === "openFiles.maintenance") {
    return;
  }
  pendingOpenFileMaintenanceReason = undefined;
  flushPendingProjectUpdates(reason);
  logDebugSummary(
    globalSettings,
    `[asp-lsp] openFileProjectMaintenance.completed: reason=${reason}`,
  );
}

function createDiskAnalysisCache(settings: AspSettings): DiskAnalysisCache {
  return new DiskAnalysisCache({
    enabled: settings.cache?.enabled !== false,
    directory: settings.cache?.directory,
    ttlHours: settings.cache?.ttlHours,
    maxSizeMb: settings.cache?.maxSizeMb,
    namespace: diskAnalysisNamespace(),
    toolVersion: languageServerVersion,
  });
}

function configureDiskAnalysisCache(): void {
  diskAnalysisCache = createDiskAnalysisCache(globalSettings);
  diskAnalysisCache.sweep();
}

function diskAnalysisNamespace(): string {
  return textFingerprint(
    JSON.stringify({
      roots: workspaceRoots.map(normalizeFileName).sort(),
      cwd: process.cwd(),
    }),
  );
}

function scheduleBackgroundAnalysis(reason: string): void {
  if (globalSettings.workspace?.backgroundAnalysis !== true) {
    cancelBackgroundAnalysis();
    return;
  }
  pendingBackgroundAnalysisReason = reason;
  if (backgroundAnalysisTimer) {
    clearTimeout(backgroundAnalysisTimer);
  }
  const generation = ++backgroundAnalysisGeneration;
  backgroundAnalysisTimer = setTimeout(() => {
    backgroundAnalysisTimer = undefined;
    void runBackgroundAnalysis(generation, reason);
  }, backgroundAnalysisIdleDelayMs);
}

async function runBackgroundAnalysis(generation: number, reason: string): Promise<void> {
  const settings = globalSettings;
  if (settings.workspace?.backgroundAnalysis !== true) {
    return;
  }
  backgroundAnalysisRunning = true;
  backgroundAnalysisRunningGeneration = generation;
  try {
    await waitForForegroundIdle();
    if (generation !== backgroundAnalysisGeneration) {
      return;
    }
    logDebugSummary(settings, `[asp-lsp] backgroundAnalysis.started: reason=${reason}`);
    const token = {
      get isCancellationRequested() {
        return generation !== backgroundAnalysisGeneration;
      },
    };
    await ensureWorkspaceIndexAsync(settings, token);
    if (token.isCancellationRequested) {
      return;
    }
    const openedUris = openDocumentUris();
    const entries = [...workspaceIndex.values()].filter((entry) => !openedUris.has(entry.uri));
    await mapWithConcurrency(entries, idleAnalysisConcurrency(settings), async (entry) => {
      if (token.isCancellationRequested) {
        return;
      }
      await waitForForegroundIdle();
      if (token.isCancellationRequested) {
        return;
      }
      await diagnosticsForIndexed(entry, cachedSettings(entry.uri), token, "idle");
    });
    if (!token.isCancellationRequested) {
      diskAnalysisCache.sweep();
      pendingBackgroundAnalysisReason = undefined;
      logDebugSummary(
        settings,
        `[asp-lsp] backgroundAnalysis.completed: files=${entries.length}, cache=${diskAnalysisCache.directory}`,
      );
    }
  } finally {
    if (backgroundAnalysisRunningGeneration === generation) {
      backgroundAnalysisRunning = false;
      backgroundAnalysisRunningGeneration = undefined;
    }
  }
}

function cancelBackgroundAnalysis(): void {
  if (backgroundAnalysisTimer) {
    clearTimeout(backgroundAnalysisTimer);
    backgroundAnalysisTimer = undefined;
  }
  backgroundAnalysisGeneration += 1;
  pendingBackgroundAnalysisReason = undefined;
}

async function waitForForegroundIdle(): Promise<void> {
  while (Date.now() - lastForegroundActivityAt < backgroundAnalysisIdleDelayMs) {
    await delay(100);
  }
}

function runInteractiveLanguageFeature<T>(callback: () => T): T {
  noteForegroundActivity();
  flushPendingProjectUpdates();
  return callback();
}

function isDebugSummaryEnabled(settings: AspSettings): boolean {
  return settings.debug?.output === "summary" || settings.debug?.output === "verbose";
}

function isDebugVerboseEnabled(settings: AspSettings): boolean {
  return settings.debug?.output === "verbose";
}

function formatElapsedMs(elapsedMs: number): string {
  return `in ${elapsedMs.toFixed(1)} ms`;
}

function dedupeDiagnostics(diagnostics: Diagnostic[]): Diagnostic[] {
  const seen = new Set<string>();
  return diagnostics.filter((diagnostic) => {
    const key = diagnosticKey(diagnostic);
    if (seen.has(key)) {
      return false;
    }
    seen.add(key);
    return true;
  });
}

function diagnosticKey(diagnostic: Diagnostic): string {
  return JSON.stringify({
    source: diagnostic.source ?? "",
    code: diagnostic.code ?? "",
    severity: diagnostic.severity ?? "",
    range: diagnostic.range,
    message: diagnostic.message,
  });
}

function analysisFor(cached: CachedDocument): CachedAnalysis {
  const settings = cachedSettings(cached.source.uri);
  const nextDiagnosticsIdentity = diagnosticsIdentity(settings);
  const nextIncludeResolutionIdentity = includeResolutionIdentity(settings);
  const nextJsProjectIdentity = jsProjectIdentity(settings);
  if (
    cached.analysis &&
    (cached.diagnosticsIdentity !== nextDiagnosticsIdentity ||
      cached.includeResolutionIdentity !== nextIncludeResolutionIdentity ||
      cached.jsProjectIdentity !== nextJsProjectIdentity)
  ) {
    cached.analysis = undefined;
  }
  cached.diagnosticsIdentity = nextDiagnosticsIdentity;
  cached.includeResolutionIdentity = nextIncludeResolutionIdentity;
  cached.jsProjectIdentity = nextJsProjectIdentity;
  cached.workspaceGeneration = workspaceGeneration;
  cached.includeResolutionGeneration = includeResolutionGeneration;
  cached.jsProjectGeneration = jsProjectGeneration;
  cached.analysis ??= {};
  return cached.analysis;
}

function getCachedVirtual(
  cached: CachedDocument,
  language: AspEmbeddedLanguage,
): VirtualDocument | undefined {
  const existing = cached.virtuals.get(language);
  if (existing) {
    return existing;
  }
  const regions = cached.parsed.regions.filter((region) => region.language === language);
  if (regions.length === 0 && language !== "html") {
    return undefined;
  }
  const virtual = buildVirtualDocument(
    cached.parsed.uri,
    cached.parsed.text,
    language,
    regions,
    cached.parsed.regions,
  );
  cached.virtuals.set(language, virtual);
  return virtual;
}

function htmlDiagnostics(cached: CachedDocument): Diagnostic[] {
  const virtual = getCachedVirtual(cached, "html");
  if (!virtual) {
    return [];
  }
  const htmlDoc = toTextDocument(virtual);
  const scanner = htmlService.createScanner(virtual.text);
  const diagnostics: Diagnostic[] = [];
  let token = scanner.scan();
  while (token !== TokenType.EOS) {
    const error = scanner.getTokenError();
    if (error) {
      const startOffset = scanner.getTokenOffset();
      const endOffset = scanner.getTokenEnd();
      const range = virtualRangeStaysWithinSegment(virtual, startOffset, endOffset)
        ? sourceRangeFromVirtualRange(virtual, {
            start: htmlDoc.positionAt(startOffset),
            end: htmlDoc.positionAt(endOffset),
          })
        : undefined;
      if (!range) {
        token = scanner.scan();
        continue;
      }
      diagnostics.push({
        severity: DiagnosticSeverity.Warning,
        range,
        message: error,
        source: "asp-lsp-html",
      });
    }
    token = scanner.scan();
  }
  return diagnostics;
}

function cssDiagnostics(cached: CachedDocument): Diagnostic[] {
  const virtual = getCachedVirtual(cached, "css");
  if (!virtual) {
    return [];
  }
  const doc = toTextDocument(virtual);
  return cssService
    .doValidation(doc, cssService.parseStylesheet(doc))
    .map((diagnostic) => remapDiagnostic(virtual, diagnostic, "asp-lsp-css"))
    .filter(isDiagnostic);
}

function jsSyntaxDiagnostics(cached: CachedDocument): Diagnostic[] {
  return jsVirtualDocuments(cached).flatMap((virtual) => {
    const sourceFile = ts.createSourceFile(
      jsVirtualFileName(virtual.uri),
      virtual.text,
      ts.ScriptTarget.ESNext,
      true,
      ts.ScriptKind.JS,
    );
    const parseDiagnostics =
      (sourceFile as ts.SourceFile & { parseDiagnostics?: readonly ts.Diagnostic[] })
        .parseDiagnostics ?? [];
    return parseDiagnostics
      .map((diagnostic) => tsDiagnosticToLsp(virtual, diagnostic))
      .filter(isDiagnostic);
  });
}

function jsSlowDiagnostics(
  cached: CachedDocument,
  settings: AspSettings,
  stepPrefix: string,
): Diagnostic[] {
  return jsVirtualDocuments(cached).flatMap((virtual) => {
    const semantic = measureDebugStep(
      settings,
      cached.source.uri,
      `${stepPrefix}.javascriptSemantic`,
      () => {
        if (settings.checkJs !== true) {
          return [];
        }
        const project = createJsLanguageService(virtual, settings);
        return project.service.getSemanticDiagnostics(jsProjectFileName(virtual, project));
      },
    );
    const unused = measureDebugStep(
      settings,
      cached.source.uri,
      `${stepPrefix}.javascriptUnused`,
      () =>
        settings.javascript?.unusedDiagnostics === false
          ? []
          : lightweightJsUnusedDiagnostics(virtual),
    );
    const semanticKeys = new Set(semantic.map(tsDiagnosticKey));
    const unusedOnly = unused.filter(
      (diagnostic) => !semanticKeys.has(tsDiagnosticKey(diagnostic)),
    );
    return [
      ...semantic.map((diagnostic) => tsDiagnosticToLsp(virtual, diagnostic)),
      ...unusedOnly.map((diagnostic) =>
        tsDiagnosticToLsp(virtual, diagnostic, {
          severity: DiagnosticSeverity.Hint,
          source: "asp-lsp-typescript-unused",
        }),
      ),
    ].filter(isDiagnostic);
  });
}

async function vbDiagnosticsAsync(
  cached: CachedDocument,
  settings: AspSettings,
  stepPrefix: string,
  cancellation: AnalysisCancellation = neverCancelled,
  mode: AnalysisExecutionMode = "foreground",
): Promise<Diagnostic[]> {
  const diagnosticsKey = vbDiagnosticsCacheKey(cached, settings);
  const cachedItems = cached.analysis?.vbDiagnostics;
  if (cachedItems?.key === diagnosticsKey) {
    return measureDebugStep(
      settings,
      cached.source.uri,
      `${stepPrefix}.vbscript.diagnostics.reuse`,
      () => cachedItems.items,
    );
  }
  const context = measureDebugStep(
    settings,
    cached.source.uri,
    `${stepPrefix}.vbscript.projectContext`,
    () => buildVbProjectContext(cached, settings),
  );
  const items = await measureDebugStepAsync(
    settings,
    cached.source.uri,
    mode === "foreground"
      ? `${stepPrefix}.vbscript.diagnostics`
      : `${stepPrefix}.vbscript.diagnostics.worker`,
    () => analyzeVbscriptAsync(cached, context, settings, stepPrefix, cancellation, mode),
  );
  if (cancellation.isCancellationRequested()) {
    return [];
  }
  analysisFor(cached).vbDiagnostics = {
    key: diagnosticsKey,
    items,
    text: cached.parsed.text,
  };
  return items;
}

async function analyzeVbscriptAsync(
  cached: CachedDocument,
  context: VbProjectContext,
  settings: AspSettings,
  stepPrefix: string,
  cancellation: AnalysisCancellation,
  mode: AnalysisExecutionMode,
): Promise<Diagnostic[]> {
  if (shouldUseVbDiagnosticsWorker(mode)) {
    try {
      const response = await runVbDiagnosticsWorker(
        cached,
        cloneableVbProjectContext(context),
        settings,
        cancellation,
        mode,
      );
      if (response.error) {
        throw workerResponseError(response);
      }
      logVbWorkerTimings(settings, cached.source.uri, stepPrefix, response);
      return response.diagnostics ?? [];
    } catch (error) {
      logDebugSummary(
        settings,
        `[asp-lsp] vbscript.worker.fallback: ${cached.source.uri}, reason=${errorMessage(error)}`,
      );
    }
  }
  return analyzeVbscript(cached.parsed, {
    ...context,
    debugStep: (name, action) =>
      measureDebugStep(
        settings,
        cached.source.uri,
        `${stepPrefix}.vbscript.diagnostics.${name}`,
        action,
      ),
  }).diagnostics;
}

async function runVbDiagnosticsWorker(
  cached: CachedDocument,
  context: VbDiagnosticsWorkerContext,
  settings: AspSettings,
  cancellation: AnalysisCancellation,
  mode: AnalysisExecutionMode,
): Promise<VbDiagnosticsWorkerResponse> {
  if (cancellation.isCancellationRequested()) {
    return { id: 0, diagnostics: [] };
  }
  const pool = getVbDiagnosticsWorkerPool(settings, mode);
  const id = ++vbDiagnosticsWorkerRequestId;
  const concurrencyMode = mode === "idle" ? "idle" : "busy";
  logDebugSummary(
    settings,
    `[asp-lsp] vbscript.worker.dispatch: ${cached.source.uri}, request=${id}, mode=${mode}, concurrency=${workerAnalysisConcurrency(settings, concurrencyMode)}`,
  );
  const response = await pool.run(
    {
      id,
      parsed: cached.parsed,
      context,
      cancellationGeneration: backgroundAnalysisGeneration,
    },
    { isCancellationRequested: () => cancellation.isCancellationRequested() },
  );
  logDebugSummary(
    settings,
    `[asp-lsp] vbscript.worker.complete: ${cached.source.uri}, request=${id}, diagnostics=${response.diagnostics?.length ?? 0}`,
  );
  logDebugSummary(
    settings,
    `[asp-lsp] worker.queue.wait: ${cached.source.uri}, request=${id}, ${formatElapsedMs(response.queueWaitMs ?? 0)}, queueLength=${response.queueLengthAtDispatch ?? 0}, cancelled=${response.cancelled === true}`,
  );
  logDebugSummary(
    settings,
    `[asp-lsp] worker.run.duration: ${cached.source.uri}, request=${id}, ${formatElapsedMs(response.runMs ?? 0)}`,
  );
  logDebugSummary(
    settings,
    `[asp-lsp] worker.payload.bytes: ${cached.source.uri}, request=${id}, payload=${response.payloadBytes ?? 0}, result=${response.resultBytes ?? 0}`,
  );
  return response;
}

function getVbDiagnosticsWorkerPool(
  settings: AspSettings,
  mode: AnalysisExecutionMode,
): VbDiagnosticsWorkerPool {
  vbDiagnosticsWorkerPool ??= new VbDiagnosticsWorkerPool();
  vbDiagnosticsWorkerPool.resize(
    workerAnalysisConcurrency(settings, mode === "idle" ? "idle" : "busy"),
  );
  return vbDiagnosticsWorkerPool;
}

function shouldUseVbDiagnosticsWorker(mode: AnalysisExecutionMode): boolean {
  return (
    process.env.ASP_LSP_DISABLE_VB_WORKERS !== "1" &&
    (mode !== "foreground" || process.env.ASP_LSP_FORCE_VB_WORKERS === "1")
  );
}

function cloneableVbProjectContext(context: VbProjectContext): VbDiagnosticsWorkerContext {
  return {
    documents: context.documents,
    symbols: context.symbols,
    externalRefUsages: context.externalRefUsages,
    typeChecking: context.typeChecking,
    identifierCase: context.identifierCase,
    identifierCaseByKind: context.identifierCaseByKind,
    comTypes: context.comTypes,
    typeEnvironment: context.typeEnvironment,
    unusedDiagnostics: context.unusedDiagnostics,
    syntaxSnippets: context.syntaxSnippets,
    locale: context.locale,
  };
}

function logVbWorkerTimings(
  settings: AspSettings,
  uri: string,
  stepPrefix: string,
  response: VbDiagnosticsWorkerResponse,
): void {
  for (const timing of response.timings ?? []) {
    logDebugElapsed(
      settings,
      uri,
      `${stepPrefix}.vbscript.diagnostics.${timing.name}`,
      timing.elapsedMs,
    );
  }
}

function workerResponseError(response: VbDiagnosticsWorkerResponse): Error {
  const error = new Error(response.error?.message ?? "VBScript diagnostics worker failed.");
  error.name = response.error?.name ?? error.name;
  error.stack = response.error?.stack ?? error.stack;
  return error;
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function seedVbReuseAfterIncrementalChange(
  previous: CachedDocument,
  cached: CachedDocument,
  settings: AspSettings,
  change: AspIncrementalChange,
  impact: AspEditImpact,
): void {
  if (
    impact.kind !== "incremental" ||
    impact.language === "vbscript" ||
    impact.language === "jscript" ||
    vbscriptRegionContentFingerprint(previous.parsed) !==
      vbscriptRegionContentFingerprint(cached.parsed)
  ) {
    return;
  }
  const analysis = analysisFor(cached);
  const previousDiagnostics = previous.analysis?.vbDiagnostics;
  if (previousDiagnostics) {
    analysis.vbDiagnostics = {
      key: vbDiagnosticsCacheKey(cached, settings),
      text: cached.parsed.text,
      items: previousDiagnostics.items.map((diagnostic) =>
        shiftDiagnosticForIncrementalChange(
          diagnostic,
          previous.source.uri,
          previous.parsed.text,
          cached.parsed.text,
          change,
        ),
      ),
    };
  }
  const previousContext = previous.analysis?.vbProjectContext;
  if (previousContext) {
    const context = shiftVbProjectContextForIncrementalChange(
      previousContext.context,
      previous.source.uri,
      previous.parsed.text,
      cached.parsed.text,
      change,
      cached.parsed,
    );
    analysis.vbProjectContext = {
      key: vbProjectContextCacheKey(context.documents ?? [cached.parsed], settings),
      rootKey: vbProjectRootContextCacheKey(cached, settings),
      context,
    };
  }
  if (previousDiagnostics || previousContext) {
    logDebugSummary(
      settings,
      `[asp-lsp] analysis.vbscript.reuse: ${cached.source.uri}, diagnostics=${previousDiagnostics ? "hit" : "miss"}, projectContext=${previousContext ? "hit" : "miss"}`,
    );
  }
}

function vbDiagnosticsCacheKey(cached: CachedDocument, settings: AspSettings): string {
  return JSON.stringify({
    root: vbProjectRootContextCacheKey(cached, settings),
    vbscript: vbscriptRegionContentFingerprint(cached.parsed),
    locale: settings.resolvedLocale,
  });
}

function vbscriptRegionContentFingerprint(parsed: AspParsedDocument): string {
  return JSON.stringify({
    defaultLanguage: parsed.defaultLanguage,
    includes: parsed.includes.map((include) => ({
      path: include.path,
      mode: include.mode,
    })),
    regions: parsed.regions
      .filter((region) => region.language === "vbscript")
      .map((region) => ({
        kind: region.kind,
        text: textFingerprint(parsed.text.slice(region.contentStart, region.contentEnd)),
      })),
  });
}

function shiftDiagnosticForIncrementalChange(
  diagnostic: Diagnostic,
  rootUri: string,
  previousText: string,
  nextText: string,
  change: AspIncrementalChange,
): Diagnostic {
  return {
    ...diagnostic,
    range: shiftAspRangeAfterChange(diagnostic.range, previousText, nextText, change),
    relatedInformation: diagnostic.relatedInformation?.map((info) => ({
      ...info,
      location:
        info.location.uri === rootUri
          ? {
              ...info.location,
              range: shiftAspRangeAfterChange(info.location.range, previousText, nextText, change),
            }
          : info.location,
    })),
  };
}

function shiftVbProjectContextForIncrementalChange(
  context: VbProjectContext,
  rootUri: string,
  previousText: string,
  nextText: string,
  change: AspIncrementalChange,
  currentRoot: AspParsedDocument,
): VbProjectContext {
  const symbols = context.symbols?.map((symbol) =>
    shiftVbSymbolForIncrementalChange(symbol, rootUri, previousText, nextText, change),
  );
  return {
    ...context,
    documents: [
      currentRoot,
      ...(context.documents?.filter((document) => document.uri !== rootUri) ?? []),
    ],
    symbols,
    typeEnvironment: context.typeEnvironment
      ? {
          ...context.typeEnvironment,
          symbols:
            context.typeEnvironment.symbols?.map((symbol) =>
              shiftVbSymbolForIncrementalChange(symbol, rootUri, previousText, nextText, change),
            ) ?? [],
        }
      : undefined,
    externalRefUsages: context.externalRefUsages?.map((usage) => ({
      ...usage,
      ranges: usage.ranges.map((range) =>
        shiftAspRangeAfterChange(range, previousText, nextText, change),
      ),
    })),
  };
}

function shiftVbSymbolForIncrementalChange(
  symbol: VbSymbol,
  rootUri: string,
  previousText: string,
  nextText: string,
  change: AspIncrementalChange,
): VbSymbol {
  if (symbol.sourceUri !== rootUri) {
    return symbol;
  }
  return {
    ...symbol,
    range: shiftAspRangeAfterChange(symbol.range, previousText, nextText, change),
    scopeRange: symbol.scopeRange
      ? shiftAspRangeAfterChange(symbol.scopeRange, previousText, nextText, change)
      : undefined,
  };
}

function lightweightJsUnusedDiagnostics(virtual: VirtualDocument): ts.Diagnostic[] {
  const fileName = normalizeFileName(jsVirtualFileName(virtual.uri));
  const files = new Map([[fileName, virtual.text]]);
  const host: ts.LanguageServiceHost = {
    getScriptFileNames: () => [fileName],
    getScriptVersion: () => "0",
    getScriptSnapshot: (requested) => {
      const text = files.get(normalizeFileName(requested));
      return text === undefined ? undefined : ts.ScriptSnapshot.fromString(text);
    },
    getScriptKind: () => ts.ScriptKind.JS,
    getCurrentDirectory: () => path.dirname(uriToFileName(virtualSourceUri(virtual))),
    getCompilationSettings: () => ({
      allowJs: true,
      checkJs: true,
      noEmit: true,
      noLib: true,
      noUnusedLocals: true,
      noUnusedParameters: true,
      target: ts.ScriptTarget.ESNext,
      module: ts.ModuleKind.CommonJS,
      types: [],
    }),
    getDefaultLibFileName: () => "",
    fileExists: (requested) => files.has(normalizeFileName(requested)),
    readFile: (requested) => files.get(normalizeFileName(requested)),
    readDirectory: () => [],
    directoryExists: () => true,
    getDirectories: () => [],
  };
  const service = ts.createLanguageService(host);
  try {
    return service
      .getSemanticDiagnostics(fileName)
      .filter((diagnostic) => tsUnusedDiagnosticCodes.has(diagnostic.code));
  } finally {
    service.dispose();
  }
}

function completionSettingsIdentity(settings: AspSettings): string {
  return JSON.stringify({
    vbscript: vbProjectContextSettings(settings),
    javascript: settings.javascript,
    checkJs: settings.checkJs,
    locale: settings.resolvedLocale,
  });
}

function completionPrefixAt(text: string, offset: number): string {
  const prefix = /[A-Za-z0-9_$]*$/.exec(text.slice(0, offset));
  return prefix?.[0] ?? "";
}

function completionCacheContinuation(
  cached: CachedDocument,
  entry: CompletionCacheEntry,
  offset: number,
  prefix: string,
): boolean {
  if (cached.source.version === entry.documentVersion) {
    return offset === entry.offset && prefix === entry.prefix;
  }
  return (
    cached.source.version > entry.documentVersion &&
    offset >= entry.offset &&
    prefix.length >= entry.prefix.length
  );
}

function completionItemMatchesPrefix(item: CompletionItem, prefix: string): boolean {
  if (prefix.length === 0) {
    return true;
  }
  const label = typeof item.label === "string" ? item.label : String(item.label);
  const filterText = item.filterText ?? label;
  const lowerPrefix = prefix.toLowerCase();
  return (
    label.toLowerCase().startsWith(lowerPrefix) || filterText.toLowerCase().startsWith(lowerPrefix)
  );
}

function cssCompletion(
  cached: CachedDocument,
  params: TextDocumentPositionParams,
  language: "css",
): CompletionItem[] {
  const virtual = getCachedVirtual(cached, language);
  if (!virtual) {
    return [];
  }
  const position = virtual.sourceMap.toVirtualPosition(params.position);
  if (!position) {
    return [];
  }
  const doc = toTextDocument(virtual);
  return cssService
    .doComplete(doc, position, cssService.parseStylesheet(doc))
    .items.map((item) => remapCompletionItem(virtual, item))
    .filter((item): item is CompletionItem => Boolean(item));
}

function remapCompletionItem(
  virtual: VirtualDocument,
  item: CompletionItem,
): CompletionItem | undefined {
  const textEdit = item.textEdit ? remapCompletionTextEdit(virtual, item.textEdit) : undefined;
  if (item.textEdit && !textEdit) {
    return undefined;
  }
  const additionalTextEdits = item.additionalTextEdits
    ?.map((edit) => remapTextEdit(virtual, edit))
    .filter((edit): edit is TextEdit => Boolean(edit));
  return {
    ...item,
    textEdit,
    additionalTextEdits,
  };
}

function remapCompletionTextEdit(
  virtual: VirtualDocument,
  textEdit: NonNullable<CompletionItem["textEdit"]>,
): CompletionItem["textEdit"] | undefined {
  if ("range" in textEdit) {
    return remapTextEdit(virtual, textEdit);
  }
  const insert = sourceRangeFromVirtualRange(virtual, textEdit.insert);
  const replace = sourceRangeFromVirtualRange(virtual, textEdit.replace);
  return insert && replace ? { ...textEdit, insert, replace } : undefined;
}

function remapTextEdit(virtual: VirtualDocument, textEdit: TextEdit): TextEdit | undefined {
  const range = sourceRangeFromVirtualRange(virtual, textEdit.range);
  return range ? { ...textEdit, range } : undefined;
}

function jsCompletion(
  cached: CachedDocument,
  params: TextDocumentPositionParams,
): CompletionItem[] {
  const context = jsContextAt(cached, params.position);
  if (!context) {
    return [];
  }
  const { offset, service, fileName } = context;
  const preferences = jsCompletionPreferences(cachedSettings(cached.source.uri));
  return (
    service
      .getCompletionsAtPosition(fileName, offset, preferences)
      ?.entries.filter((entry) => !hiddenJavaScriptGlobalCompletions.has(entry.name))
      .map((entry) => {
        return {
          label: entry.name,
          kind: tsCompletionKind(entry.kind),
          detail: entry.kind,
          data: {
            kind: "javascript",
            uri: cached.source.uri,
            language: context.virtual.languageId,
            name: entry.name,
            virtualOffset: offset,
            source: entry.source,
            tsData: entry.data,
          },
        };
      }) ?? []
  );
}

function jsCompletionPreferences(settings: AspSettings): ts.GetCompletionsAtPositionOptions {
  if (settings.javascript?.autoImports === false) {
    return {};
  }
  return {
    includeCompletionsForModuleExports: true,
    includeCompletionsForImportStatements: true,
  };
}

function safeGetCompletionEntryDetails(
  service: ts.LanguageService,
  fileName: string,
  position: number,
  name: string,
  source: string | undefined,
  preferences: ts.UserPreferences,
  data: ts.CompletionEntryData | undefined,
): ts.CompletionEntryDetails | undefined {
  try {
    return service.getCompletionEntryDetails(
      fileName,
      position,
      name,
      {},
      source,
      preferences,
      data,
    );
  } catch {
    return undefined;
  }
}

function jsHover(cached: CachedDocument, position: Position): Hover | null {
  const context = jsContextAt(cached, position);
  if (!context) {
    return null;
  }
  const quickInfo = context.service.getQuickInfoAtPosition(context.fileName, context.offset);
  if (!quickInfo) {
    return null;
  }
  const signature = ts.displayPartsToString(quickInfo.displayParts);
  const docs = ts.displayPartsToString(quickInfo.documentation);
  return {
    contents: {
      kind: "markdown",
      value: docs
        ? `\`\`\`javascript\n${signature}\n\`\`\`\n\n${docs}`
        : `\`\`\`javascript\n${signature}\n\`\`\``,
    },
    range: textSpanToSourceRange(context.virtual, quickInfo.textSpan),
  };
}

function jsReferences(cached: CachedDocument, position: Position): Location[] {
  const context = jsContextAt(cached, position);
  if (!context) {
    return [];
  }
  return (
    context.service
      .getReferencesAtPosition(context.fileName, context.offset)
      ?.map((reference) => tsReferenceToLocation(context, reference))
      .filter((location): location is Location => Boolean(location)) ?? []
  );
}

function jsPrepareRename(cached: CachedDocument, position: Position): Range | null {
  const context = jsContextAt(cached, position);
  if (!context) {
    return null;
  }
  const info = context.service.getRenameInfo(context.fileName, context.offset, {});
  if (!info.canRename) {
    return null;
  }
  return textSpanToSourceRange(context.virtual, info.triggerSpan) ?? null;
}

function jsRename(
  cached: CachedDocument,
  position: Position,
  newName: string,
): WorkspaceEdit | null {
  const context = jsContextAt(cached, position);
  if (!context || !/^[\p{ID_Start}_$][\p{ID_Continue}_$]*$/u.test(newName)) {
    return null;
  }
  const locations = context.service.findRenameLocations(
    context.fileName,
    context.offset,
    false,
    false,
    {},
  );
  if (!locations) {
    return null;
  }
  const changes: NonNullable<WorkspaceEdit["changes"]> = {};
  for (const location of locations) {
    const mapped = textSpanToLocation(context, location.fileName, location.textSpan);
    if (!mapped) {
      continue;
    }
    changes[mapped.uri] = [
      ...(changes[mapped.uri] ?? []),
      { range: mapped.range, newText: newName },
    ];
  }
  return Object.keys(changes).length > 0 ? { changes } : null;
}

function htmlPrepareRename(cached: CachedDocument, position: Position): Range | null {
  const ranges = htmlLinkedRanges(cached, position);
  return ranges?.[0] ?? wordRangeAt(cached.source, position);
}

function htmlRename(
  cached: CachedDocument,
  position: Position,
  newName: string,
): WorkspaceEdit | null {
  const virtual = getCachedVirtual(cached, "html");
  if (!virtual) {
    return null;
  }
  const doc = toTextDocument(virtual);
  const edit = htmlService.doRename(doc, position, newName, htmlService.parseHTMLDocument(doc));
  return edit ? remapWorkspaceEdit(virtual, edit, cached.source.uri) : null;
}

function cssPrepareRename(cached: CachedDocument, position: Position): Range | null {
  const virtual = getCachedVirtual(cached, "css");
  const virtualPosition = virtual?.sourceMap.toVirtualPosition(position);
  if (!virtual || !virtualPosition) {
    return null;
  }
  const doc = toTextDocument(virtual);
  const range = cssService.prepareRename(doc, virtualPosition, cssService.parseStylesheet(doc));
  return range ? (sourceRangeFromVirtualRange(virtual, range) ?? null) : null;
}

function cssRename(
  cached: CachedDocument,
  position: Position,
  newName: string,
): WorkspaceEdit | null {
  const virtual = getCachedVirtual(cached, "css");
  const virtualPosition = virtual?.sourceMap.toVirtualPosition(position);
  if (!virtual || !virtualPosition) {
    return null;
  }
  const doc = toTextDocument(virtual);
  return remapWorkspaceEdit(
    virtual,
    cssService.doRename(doc, virtualPosition, newName, cssService.parseStylesheet(doc)),
    cached.source.uri,
  );
}

function crossLanguageRename(
  cached: CachedDocument,
  position: Position,
  newName: string,
): WorkspaceEdit | undefined {
  const target = crossLanguageRenameTarget(cached, position);
  if (!target || !/^[A-Za-z_][A-Za-z0-9_-]*$/.test(newName)) {
    return undefined;
  }
  const changes: NonNullable<WorkspaceEdit["changes"]> = {};
  const seen = new Set<string>();
  for (const candidate of crossLanguageRenameCandidates(cached)) {
    const text = candidate.source.getText();
    const edits = [
      ...htmlAttributeRenameEdits(candidate, target, newName),
      ...cssSelectorRenameEdits(candidate, target, newName),
      ...jsSelectorRenameEdits(candidate, target, newName),
    ];
    for (const edit of edits) {
      const key = `${candidate.source.uri}:${offsetAtText(text, edit.range.start)}:${offsetAtText(text, edit.range.end)}`;
      if (seen.has(key)) {
        continue;
      }
      seen.add(key);
      changes[candidate.source.uri] = [...(changes[candidate.source.uri] ?? []), edit];
    }
  }
  return Object.keys(changes).length > 0 ? { changes } : undefined;
}

function crossLanguageRenameCandidates(active: CachedDocument): CachedDocument[] {
  ensureWorkspaceIndex();
  const candidates: CachedDocument[] = [active];
  const seen = new Set([active.source.uri]);
  for (const document of documents.all()) {
    if (seen.has(document.uri)) {
      continue;
    }
    const cached = ensureFreshCachedDocument(document);
    if (cached) {
      seen.add(cached.source.uri);
      candidates.push(cached);
    }
  }
  for (const entry of workspaceIndex.values()) {
    if (seen.has(entry.uri)) {
      continue;
    }
    seen.add(entry.uri);
    candidates.push(cachedFromIndexed(entry));
  }
  return candidates;
}

function crossLanguagePrepareRename(cached: CachedDocument, position: Position): Range | null {
  return crossLanguageRenameTarget(cached, position) ? wordRangeAt(cached.source, position) : null;
}

function crossLanguageRenameTarget(
  cached: CachedDocument,
  position: Position,
): { kind: "id" | "class"; name: string } | undefined {
  const region = findRegionAt(cached.parsed, cached.source.offsetAt(position));
  if (region?.language === "html") {
    return htmlAttributeRenameTarget(cached, position);
  }
  if (region?.language === "css") {
    return cssSelectorRenameTarget(cached, position);
  }
  if (region && isJavaScriptLikeRegion(region)) {
    return jsSelectorRenameTarget(cached, position);
  }
  return undefined;
}

function htmlAttributeRenameTarget(
  cached: CachedDocument,
  position: Position,
): { kind: "id" | "class"; name: string } | undefined {
  const offset = cached.source.offsetAt(position);
  const text = cached.source.getText();
  for (const match of text.matchAll(/\b(id|class)\s*=\s*(["'])([^"']*)\2/gi)) {
    const value = match[3] ?? "";
    const valueStart = (match.index ?? 0) + match[0].indexOf(value);
    const valueEnd = valueStart + value.length;
    if (offset < valueStart || offset > valueEnd) {
      continue;
    }
    const kind = (match[1]?.toLowerCase() === "id" ? "id" : "class") as "id" | "class";
    if (kind === "id") {
      return { kind, name: value };
    }
    return classTokenAt(value, valueStart, offset);
  }
  return undefined;
}

function cssSelectorRenameTarget(
  cached: CachedDocument,
  position: Position,
): { kind: "id" | "class"; name: string } | undefined {
  const text = cached.source.getText();
  const offset = cached.source.offsetAt(position);
  let start = offset;
  while (start > 0 && /[A-Za-z0-9_-]/.test(text[start - 1] ?? "")) {
    start -= 1;
  }
  let end = offset;
  while (end < text.length && /[A-Za-z0-9_-]/.test(text[end] ?? "")) {
    end += 1;
  }
  const prefix = text[start - 1];
  if ((prefix !== "." && prefix !== "#") || start === end) {
    return undefined;
  }
  return { kind: prefix === "#" ? "id" : "class", name: text.slice(start, end) };
}

function jsSelectorRenameTarget(
  cached: CachedDocument,
  position: Position,
): { kind: "id" | "class"; name: string } | undefined {
  const offset = cached.source.offsetAt(position);
  for (const match of cached.source.getText().matchAll(/(["'])([^"']*)\1/g)) {
    const value = match[2] ?? "";
    const valueStart = (match.index ?? 0) + 1;
    const valueEnd = valueStart + value.length;
    if (offset < valueStart || offset > valueEnd) {
      continue;
    }
    if (isSelectorStringContext(cached.source.getText(), match.index ?? 0, "getElementById")) {
      return { kind: "id", name: value };
    }
    if (isSelectorStringContext(cached.source.getText(), match.index ?? 0, "classList")) {
      return { kind: "class", name: value };
    }
    const selector = selectorTokenAt(value, valueStart, offset);
    if (
      selector &&
      (isSelectorStringContext(cached.source.getText(), match.index ?? 0, "querySelector") ||
        isSelectorStringContext(cached.source.getText(), match.index ?? 0, "querySelectorAll"))
    ) {
      return selector;
    }
  }
  return undefined;
}

function htmlAttributeRenameEdits(
  cached: CachedDocument,
  target: { kind: "id" | "class"; name: string },
  newName: string,
): TextEdit[] {
  const edits: TextEdit[] = [];
  const text = cached.source.getText();
  for (const match of text.matchAll(/\b(id|class)\s*=\s*(["'])([^"']*)\2/gi)) {
    const kind = match[1]?.toLowerCase() === "id" ? "id" : "class";
    const value = match[3] ?? "";
    const valueStart = (match.index ?? 0) + match[0].indexOf(value);
    if (kind === "id" && target.kind === "id" && value === target.name) {
      edits.push(offsetTextEdit(cached.source, valueStart, valueStart + value.length, newName));
    }
    if (kind === "class" && target.kind === "class") {
      edits.push(...classTokenEdits(cached.source, value, valueStart, target.name, newName));
    }
  }
  return edits;
}

function cssSelectorRenameEdits(
  cached: CachedDocument,
  target: { kind: "id" | "class"; name: string },
  newName: string,
): TextEdit[] {
  const edits: TextEdit[] = [];
  const text = cached.source.getText();
  const prefix = target.kind === "id" ? "#" : ".";
  const pattern = new RegExp(`\\${prefix}${escapeRegExp(target.name)}\\b`, "g");
  for (const match of text.matchAll(pattern)) {
    const start = (match.index ?? 0) + 1;
    if (findRegionAt(cached.parsed, start)?.language === "css") {
      edits.push(offsetTextEdit(cached.source, start, start + target.name.length, newName));
    }
  }
  return edits;
}

function jsSelectorRenameEdits(
  cached: CachedDocument,
  target: { kind: "id" | "class"; name: string },
  newName: string,
): TextEdit[] {
  const edits: TextEdit[] = [];
  const text = cached.source.getText();
  for (const match of text.matchAll(/(["'])([^"']*)\1/g)) {
    const quoteStart = match.index ?? 0;
    const region = findRegionAt(cached.parsed, quoteStart);
    if (!region || !isJavaScriptLikeRegion(region)) {
      continue;
    }
    const value = match[2] ?? "";
    const valueStart = quoteStart + 1;
    if (
      target.kind === "id" &&
      value === target.name &&
      isSelectorStringContext(text, quoteStart, "getElementById")
    ) {
      edits.push(offsetTextEdit(cached.source, valueStart, valueStart + value.length, newName));
    }
    if (
      target.kind === "class" &&
      value === target.name &&
      isSelectorStringContext(text, quoteStart, "classList")
    ) {
      edits.push(offsetTextEdit(cached.source, valueStart, valueStart + value.length, newName));
    }
    if (
      isSelectorStringContext(text, quoteStart, "querySelector") ||
      isSelectorStringContext(text, quoteStart, "querySelectorAll")
    ) {
      edits.push(
        ...selectorTokenEdits(cached.source, value, valueStart, target.kind, target.name, newName),
      );
    }
  }
  return edits;
}

function classTokenAt(
  value: string,
  valueStart: number,
  offset: number,
): { kind: "class"; name: string } | undefined {
  for (const match of value.matchAll(/[A-Za-z_][A-Za-z0-9_-]*/g)) {
    const start = valueStart + (match.index ?? 0);
    const end = start + match[0].length;
    if (offset >= start && offset <= end) {
      return { kind: "class", name: match[0] };
    }
  }
  return undefined;
}

function selectorTokenAt(
  value: string,
  valueStart: number,
  offset: number,
): { kind: "id" | "class"; name: string } | undefined {
  for (const match of value.matchAll(/([#.])([A-Za-z_][A-Za-z0-9_-]*)/g)) {
    const nameStart = valueStart + (match.index ?? 0) + 1;
    const nameEnd = nameStart + (match[2]?.length ?? 0);
    if (offset >= nameStart && offset <= nameEnd) {
      return { kind: match[1] === "#" ? "id" : "class", name: match[2] ?? "" };
    }
  }
  return undefined;
}

function classTokenEdits(
  document: TextDocument,
  value: string,
  valueStart: number,
  oldName: string,
  newName: string,
): TextEdit[] {
  return [...value.matchAll(/[A-Za-z_][A-Za-z0-9_-]*/g)]
    .filter((match) => match[0] === oldName)
    .map((match) =>
      offsetTextEdit(
        document,
        valueStart + (match.index ?? 0),
        valueStart + (match.index ?? 0) + match[0].length,
        newName,
      ),
    );
}

function selectorTokenEdits(
  document: TextDocument,
  value: string,
  valueStart: number,
  kind: "id" | "class",
  oldName: string,
  newName: string,
): TextEdit[] {
  return [...value.matchAll(/([#.])([A-Za-z_][A-Za-z0-9_-]*)/g)]
    .filter((match) => (kind === "id" ? match[1] === "#" : match[1] === "."))
    .filter((match) => match[2] === oldName)
    .map((match) =>
      offsetTextEdit(
        document,
        valueStart + (match.index ?? 0) + 1,
        valueStart + (match.index ?? 0) + 1 + oldName.length,
        newName,
      ),
    );
}

function isSelectorStringContext(text: string, quoteStart: number, callName: string): boolean {
  const prefix = text.slice(Math.max(0, quoteStart - 80), quoteStart);
  return new RegExp(`${callName.replace(".", "\\.")}\\s*\\([^)]*$`).test(prefix);
}

function offsetTextEdit(
  document: TextDocument,
  start: number,
  end: number,
  newText: string,
): TextEdit {
  return {
    range: { start: document.positionAt(start), end: document.positionAt(end) },
    newText,
  };
}

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function jsSignatureHelp(cached: CachedDocument, position: Position): SignatureHelp | null {
  const context = jsContextAt(cached, position);
  if (!context) {
    return null;
  }
  const help = context.service.getSignatureHelpItems(context.fileName, context.offset, undefined);
  if (!help) {
    return null;
  }
  return {
    signatures: help.items.map((item) => ({
      label:
        ts.displayPartsToString(item.prefixDisplayParts) +
        item.parameters
          .map((parameter) => ts.displayPartsToString(parameter.displayParts))
          .join(ts.displayPartsToString(item.separatorDisplayParts)) +
        ts.displayPartsToString(item.suffixDisplayParts),
      documentation: ts.displayPartsToString(item.documentation),
      parameters: item.parameters.map((parameter) => ({
        label: ts.displayPartsToString(parameter.displayParts),
        documentation: ts.displayPartsToString(parameter.documentation),
      })),
    })),
    activeSignature: help.selectedItemIndex,
    activeParameter: help.argumentIndex,
  };
}

function jsDocumentHighlights(cached: CachedDocument, position: Position): DocumentHighlight[] {
  const context = jsContextAt(cached, position);
  if (!context) {
    return [];
  }
  return (
    context.service
      .getDocumentHighlights(context.fileName, context.offset, [context.fileName])
      ?.flatMap((fileHighlights) =>
        fileHighlights.highlightSpans
          .map((span): DocumentHighlight | undefined => {
            const range = textSpanToSourceRange(context.virtual, span.textSpan);
            return range
              ? {
                  range,
                  kind:
                    span.kind === "writtenReference"
                      ? DocumentHighlightKind.Write
                      : DocumentHighlightKind.Read,
                }
              : undefined;
          })
          .filter((highlight): highlight is DocumentHighlight => Boolean(highlight)),
      ) ?? []
  );
}

function htmlDocumentHighlights(cached: CachedDocument, position: Position): DocumentHighlight[] {
  const virtual = getCachedVirtual(cached, "html");
  if (!virtual) {
    return [];
  }
  const doc = toTextDocument(virtual);
  const service = htmlService as {
    findDocumentHighlights?: (
      document: TextDocument,
      position: Position,
      htmlDocument: unknown,
    ) => DocumentHighlight[];
  };
  return service.findDocumentHighlights?.(doc, position, htmlService.parseHTMLDocument(doc)) ?? [];
}

function cssDocumentHighlights(cached: CachedDocument, position: Position): DocumentHighlight[] {
  const virtual = getCachedVirtual(cached, "css");
  const virtualPosition = virtual?.sourceMap.toVirtualPosition(position);
  if (!virtual || !virtualPosition) {
    return [];
  }
  const doc = toTextDocument(virtual);
  const service = cssService as {
    findDocumentHighlights?: (
      document: TextDocument,
      position: Position,
      stylesheet: unknown,
    ) => DocumentHighlight[];
  };
  return (
    service
      .findDocumentHighlights?.(doc, virtualPosition, cssService.parseStylesheet(doc))
      .map((highlight) => {
        const range = sourceRangeFromVirtualRange(virtual, highlight.range);
        return range ? { ...highlight, range } : undefined;
      })
      .filter((highlight): highlight is DocumentHighlight => Boolean(highlight)) ?? []
  );
}

function jsInlayHints(cached: CachedDocument, range: Range): InlayHint[] {
  const settings = cachedSettings(cached.source.uri);
  const hints = settings.inlayHints;
  if (
    hints?.parameterNames === false &&
    hints.variableTypes === false &&
    hints.functionReturnTypes === false
  ) {
    return [];
  }
  return jsVirtualDocuments(cached).flatMap((virtual) => {
    const sourceStart = cached.source.offsetAt(range.start);
    const sourceEnd = cached.source.offsetAt(range.end);
    const segments = virtual.sourceMap.segments.filter(
      (candidate) => candidate.sourceStart < sourceEnd && candidate.sourceEnd > sourceStart,
    );
    if (segments.length === 0) {
      return [];
    }
    const project = createJsLanguageService(virtual, settings);
    const fileName = jsProjectFileName(virtual, project);
    const seen = new Set<string>();
    return segments
      .flatMap((segment) => {
        const start = segment.virtualStart + Math.max(0, sourceStart - segment.sourceStart);
        const end =
          segment.virtualStart + Math.min(segment.sourceEnd, sourceEnd) - segment.sourceStart;
        if (start >= end) {
          return [];
        }
        return project.service.provideInlayHints(
          fileName,
          { start, length: end - start },
          {
            includeInlayParameterNameHints: hints?.parameterNames === false ? "none" : "all",
            includeInlayVariableTypeHints: hints?.variableTypes !== false,
            includeInlayFunctionLikeReturnTypeHints: hints?.functionReturnTypes !== false,
            includeInlayPropertyDeclarationTypeHints: hints?.variableTypes !== false,
          },
        );
      })
      .map((hint): InlayHint | undefined => {
        const sourceOffset = sourceOffsetFromVirtualPoint(virtual, hint.position);
        const sourcePosition =
          sourceOffset === undefined ? undefined : cached.source.positionAt(sourceOffset);
        if (!sourcePosition || !isJavaScriptPosition(cached, sourcePosition)) {
          return undefined;
        }
        const label =
          hint.text ||
          hint.displayParts
            ?.map((part) => part.text)
            .join("")
            .trim();
        const key = `${sourcePosition.line}:${sourcePosition.character}:${label}`;
        if (!label || seen.has(key)) {
          return undefined;
        }
        seen.add(key);
        return { position: sourcePosition, label };
      })
      .filter((hint): hint is InlayHint => Boolean(hint));
  });
}

function jsPrepareCallHierarchy(cached: CachedDocument, position: Position): CallHierarchyItem[] {
  const context = jsContextAt(cached, position);
  if (!context) {
    return [];
  }
  const items = context.service.prepareCallHierarchy(context.fileName, context.offset);
  const list = Array.isArray(items) ? items : items ? [items] : [];
  return list
    .map((item) => tsCallHierarchyItemToLsp(context, item, cached.source.uri))
    .filter((item): item is CallHierarchyItem => Boolean(item));
}

function jsIncomingCalls(item: CallHierarchyItem): CallHierarchyIncomingCall[] {
  const context = jsCallHierarchyContext(item);
  if (!context) {
    return [];
  }
  return context.service
    .provideCallHierarchyIncomingCalls(context.fileName, context.offset)
    .map((call) => {
      const from = tsCallHierarchyItemToLsp(context, call.from, context.rootUri);
      return from
        ? {
            from,
            fromRanges: call.fromSpans
              .map((span) => textSpanToLocation(context, call.from.file, span)?.range)
              .filter((range): range is Range => Boolean(range)),
          }
        : undefined;
    })
    .filter((call): call is CallHierarchyIncomingCall => Boolean(call));
}

function jsOutgoingCalls(item: CallHierarchyItem): CallHierarchyOutgoingCall[] {
  const context = jsCallHierarchyContext(item);
  if (!context) {
    return [];
  }
  return context.service
    .provideCallHierarchyOutgoingCalls(context.fileName, context.offset)
    .map((call) => {
      const to = tsCallHierarchyItemToLsp(context, call.to, context.rootUri);
      return to
        ? {
            to,
            fromRanges: call.fromSpans
              .map((span) => textSpanToLocation(context, context.fileName, span)?.range)
              .filter((range): range is Range => Boolean(range)),
          }
        : undefined;
    })
    .filter((call): call is CallHierarchyOutgoingCall => Boolean(call));
}

function isJsCallHierarchyItem(item: CallHierarchyItem): boolean {
  return (item.data as Partial<JsCallHierarchyData> | undefined)?.kind === "javascript";
}

function jsCallHierarchyContext(
  item: CallHierarchyItem,
): (JsProjectContext & { rootUri: string }) | undefined {
  const data = item.data as Partial<JsCallHierarchyData> | undefined;
  if (data?.kind !== "javascript" || !data.rootUri || !data.language) {
    return undefined;
  }
  const cached = getFreshCached(data.rootUri);
  const virtual = cached
    ? getCachedVirtual(cached, data.language === "jscript" ? "jscript" : "javascript")
    : undefined;
  if (!cached || !virtual || typeof data.position !== "number") {
    return undefined;
  }
  const project = createJsLanguageService(virtual, cachedSettings(data.rootUri));
  const fileName = data.fileName ?? jsProjectFileName(virtual, project);
  return {
    virtual,
    service: project.service,
    fileName,
    offset: data.position,
    files: project.files,
    rootUri: data.rootUri,
  };
}

function tsCallHierarchyItemToLsp(
  context: JsProjectContext,
  item: ts.CallHierarchyItem,
  rootUri: string,
): CallHierarchyItem | undefined {
  const range = textSpanToLocation(context, item.file, item.span)?.range;
  const selectionRange = textSpanToLocation(context, item.file, item.selectionSpan)?.range;
  const location = textSpanToLocation(context, item.file, item.selectionSpan);
  if (!range || !selectionRange || !location) {
    return undefined;
  }
  return {
    name: item.name,
    kind: tsSymbolKind(item.kind),
    detail: item.containerName,
    uri: location.uri,
    range,
    selectionRange,
    data: {
      kind: "javascript",
      rootUri,
      language: context.virtual.languageId,
      fileName: item.file,
      position: item.selectionSpan.start,
    } satisfies JsCallHierarchyData,
  };
}

function vbTypeHierarchyItemAt(
  cached: CachedDocument,
  position: Position,
): TypeHierarchyItem | undefined {
  if (!isVbscriptPosition(cached, position)) {
    return undefined;
  }
  const context = bestEffortVbProjectContext(cached, cachedSettings(cached.source.uri));
  const symbol =
    getVbscriptDefinition(cached.parsed, position, context) ??
    getVbscriptTypeDefinition(cached.parsed, position, context);
  if (!symbol) {
    return undefined;
  }
  const typeName =
    symbol.kind === "class" ? symbol.name : (symbol.type?.name ?? symbol.typeName ?? symbol.name);
  const type = vbTypeByName(context, typeName);
  if (!type || type.kind === "intrinsic") {
    return undefined;
  }
  return vbTypeHierarchyItem(type, cached.source.uri, symbol.range, context);
}

function vbTypeHierarchyItem(
  type: VbType,
  rootUri: string,
  fallbackRange: Range,
  context: VbProjectContext,
): TypeHierarchyItem {
  const symbol = context.symbols?.find(
    (candidate) =>
      candidate.kind === "class" &&
      candidate.name.toLowerCase() === type.name.toLowerCase() &&
      candidate.sourceUri,
  );
  const range = symbol?.range ?? fallbackRange;
  const uri = symbol?.sourceUri ?? rootUri;
  return {
    name: type.name,
    kind: type.kind === "com" ? SymbolKind.Interface : SymbolKind.Class,
    detail: type.kind === "com" ? "COM type catalog" : uri,
    uri,
    range,
    selectionRange: range,
    data: {
      kind: "vbscript",
      rootUri,
      uri,
      typeName: type.name,
      line: range.start.line,
      character: range.start.character,
    } satisfies VbTypeHierarchyData,
  };
}

function vbTypeHierarchyRelatedItems(item: TypeHierarchyItem): TypeHierarchyItem[] {
  const data = item.data as Partial<VbTypeHierarchyData> | undefined;
  if (data?.kind !== "vbscript" || !data.typeName) {
    return [];
  }
  const cached = getFreshCached(data.rootUri ?? item.uri) ?? getFreshCached(item.uri);
  if (!cached) {
    return [];
  }
  const context = bestEffortVbProjectContext(cached, cachedSettings(cached.source.uri));
  const type = vbTypeByName(context, data.typeName);
  if (!type) {
    return [];
  }
  const seen = new Set<string>();
  return type.members.flatMap((member) => {
    const typeName = member.type?.name ?? member.signature?.returnType?.name;
    const related = typeName ? vbTypeByName(context, typeName) : undefined;
    if (!related || related.kind === "intrinsic" || related.name === type.name) {
      return [];
    }
    const key = related.name.toLowerCase();
    if (seen.has(key)) {
      return [];
    }
    seen.add(key);
    return [vbTypeHierarchyItem(related, cached.source.uri, item.selectionRange, context)];
  });
}

function vbTypeByName(context: VbProjectContext, name: string): VbType | undefined {
  return context.typeEnvironment?.types.find(
    (type) => type.name.toLowerCase() === name.toLowerCase(),
  );
}

function monikersAt(cached: CachedDocument, position: Position): Moniker[] {
  if (isVbscriptPosition(cached, position)) {
    return vbMonikersAt(cached, position);
  }
  if (isJavaScriptPosition(cached, position)) {
    return jsMonikersAt(cached, position);
  }
  return [];
}

function vbMonikersAt(cached: CachedDocument, position: Position): Moniker[] {
  const context = bestEffortVbProjectContext(cached, cachedSettings(cached.source.uri));
  const symbol =
    getVbscriptDefinition(cached.parsed, position, context) ??
    getVbscriptTypeDefinition(cached.parsed, position, context);
  if (!symbol) {
    return [];
  }
  return [
    {
      scheme: "asp-lsp",
      identifier: [
        symbol.sourceUri,
        symbol.containerName,
        symbol.memberOf,
        symbol.scopeName,
        symbol.name,
        symbol.range.start.line,
        symbol.range.start.character,
      ]
        .filter((part) => part !== undefined && part !== "")
        .join("#"),
      unique: UniquenessLevel.project,
      kind: symbol.scopeName ? MonikerKind.local : MonikerKind.$export,
    },
  ];
}

function jsMonikersAt(cached: CachedDocument, position: Position): Moniker[] {
  const context = jsContextAt(cached, position);
  const quickInfo = context?.service.getQuickInfoAtPosition(context.fileName, context.offset);
  if (!context || !quickInfo) {
    return [];
  }
  const sourceRange = textSpanToSourceRange(context.virtual, quickInfo.textSpan);
  const name = sourceRange ? textInRange(cached.source, sourceRange).trim() : "";
  if (!name) {
    return [];
  }
  return [
    {
      scheme: "asp-lsp-js",
      identifier: [
        cached.source.uri,
        context.virtual.languageId,
        name,
        quickInfo.textSpan.start,
        quickInfo.textSpan.length,
      ].join("#"),
      unique: UniquenessLevel.project,
      kind:
        quickInfo.kind === "class" || quickInfo.kind === "function"
          ? MonikerKind.$export
          : MonikerKind.local,
    },
  ];
}

function inlineValues(cached: CachedDocument, range: Range): InlineValue[] {
  return [...vbInlineValues(cached, range), ...jsInlineValues(cached, range)];
}

function vbInlineValues(cached: CachedDocument, range: Range): InlineValue[] {
  const context = bestEffortVbProjectContext(cached, cachedSettings(cached.source.uri));
  const seen = new Set<string>();
  return (context.symbols ?? [])
    .filter(
      (symbol) =>
        symbol.sourceUri === cached.source.uri &&
        isInlineValueSymbol(symbol.kind) &&
        rangesOverlap(symbol.range, range),
    )
    .flatMap((symbol) => {
      const key = `${symbol.name}:${symbol.range.start.line}:${symbol.range.start.character}`;
      if (seen.has(key)) {
        return [];
      }
      seen.add(key);
      return [InlineValueVariableLookup.create(symbol.range, symbol.name, false)];
    });
}

function jsInlineValues(cached: CachedDocument, range: Range): InlineValue[] {
  const sourceStart = cached.source.offsetAt(range.start);
  const sourceEnd = cached.source.offsetAt(range.end);
  const seen = new Set<string>();
  return jsVirtualDocuments(cached).flatMap((virtual) => {
    const project = createJsLanguageService(virtual, cachedSettings(cached.source.uri));
    const fileName = jsProjectFileName(virtual, project);
    const file = project.files.get(fileName);
    if (!file) {
      return [];
    }
    const sourceFile = ts.createSourceFile(fileName, file.text, ts.ScriptTarget.Latest, true);
    const values: InlineValue[] = [];
    const visit = (node: ts.Node): void => {
      if (ts.isIdentifier(node)) {
        const start = node.getStart(sourceFile);
        const end = node.getEnd();
        const segment = virtual.sourceMap.segments.find(
          (candidate) =>
            candidate.virtualStart <= start &&
            candidate.virtualStart + (candidate.sourceEnd - candidate.sourceStart) >= end &&
            candidate.sourceStart < sourceEnd &&
            candidate.sourceEnd > sourceStart,
        );
        const sourceRange = segment
          ? textSpanToSourceRange(virtual, { start, length: end - start })
          : undefined;
        const key = sourceRange
          ? `${node.text}:${sourceRange.start.line}:${sourceRange.start.character}`
          : undefined;
        if (sourceRange && rangesOverlap(sourceRange, range) && key && !seen.has(key)) {
          seen.add(key);
          values.push(InlineValueVariableLookup.create(sourceRange, node.text, true));
        }
      }
      ts.forEachChild(node, visit);
    };
    visit(sourceFile);
    return values;
  });
}

function isInlineValueSymbol(kind: VbSymbolKind): boolean {
  return ["variable", "parameter", "constant", "field", "property"].includes(kind);
}

function resolveJsCompletion(item: CompletionItem, uri: string): CompletionItem | undefined {
  const cached = getFreshCached(uri);
  const data = item.data as
    | {
        name?: string;
        virtualOffset?: number;
        language?: string;
        source?: string;
        tsData?: ts.CompletionEntryData;
      }
    | undefined;
  const virtual = cached
    ? getCachedVirtual(cached, data?.language === "jscript" ? "jscript" : "javascript")
    : undefined;
  if (!cached || !virtual || typeof data?.virtualOffset !== "number" || !data.name) {
    return undefined;
  }
  const project = createJsLanguageService(virtual, cachedSettings(uri));
  const service = project.service;
  const fileName = jsProjectFileName(virtual, project);
  const preferences = jsCompletionPreferences(cachedSettings(uri));
  const details = safeGetCompletionEntryDetails(
    service,
    fileName,
    data.virtualOffset,
    data.name,
    data.source,
    preferences,
    data.tsData,
  );
  const importEdit = fileTextChangesToWorkspaceEdit(
    virtual,
    details?.codeActions?.flatMap((action) => action.changes) ?? [],
  );
  return {
    ...item,
    detail: details ? ts.displayPartsToString(details.displayParts) : item.detail,
    documentation: details ? ts.displayPartsToString(details.documentation) : item.documentation,
    additionalTextEdits: importEdit?.changes?.[cached.source.uri],
  };
}

function aspHover(cached: CachedDocument, params: TextDocumentPositionParams): Hover | null {
  const settings = cachedSettings(cached.source.uri);
  const context = buildVbProjectContext(cached, settings);
  const value = getVbscriptHover(cached.parsed, params.position, context);
  const fallback = value ?? fallbackVbscriptHover(cached, params.position, context);
  return fallback ? { contents: { kind: "markdown", value: fallback } } : null;
}

function fallbackVbscriptHover(
  cached: CachedDocument,
  position: Position,
  context: VbProjectContext,
): string | undefined {
  const symbol = fallbackVbscriptSymbolAt(cached, position, context);
  if (!symbol) {
    return undefined;
  }
  return `\`\`\`vbscript\n${fallbackVbscriptSignature(symbol)}\n\`\`\``;
}

function fallbackVbscriptSignature(symbol: VbSymbol): string {
  if (symbol.kind === "class") {
    return `Class ${symbol.name}`;
  }
  if (symbol.kind === "function" || symbol.kind === "method" || symbol.kind === "property") {
    const keyword =
      symbol.kind === "property"
        ? "Property"
        : symbol.procedureKind === "sub"
          ? "Sub"
          : symbol.kind === "method"
            ? "Function"
            : "Function";
    const parameters = symbol.parameterDetails?.length
      ? symbol.parameterDetails
          .map((parameter) => `${parameter.mode === "byval" ? "ByVal" : "ByRef"} ${parameter.name}`)
          .join(", ")
      : (symbol.parameters ?? []).join(", ");
    const typeSuffix =
      symbol.kind !== "property" && symbol.procedureKind === "sub"
        ? ""
        : ` As ${symbol.typeName ?? "Variant"}`;
    return `${keyword} ${symbol.name}(${parameters})${typeSuffix}`;
  }
  if (symbol.kind === "sub") {
    const parameters = symbol.parameterDetails?.length
      ? symbol.parameterDetails
          .map((parameter) => `${parameter.mode === "byval" ? "ByVal" : "ByRef"} ${parameter.name}`)
          .join(", ")
      : (symbol.parameters ?? []).join(", ");
    return `Sub ${symbol.name}(${parameters})`;
  }
  const declaration =
    symbol.kind === "constant" ? "Const" : symbol.kind === "field" ? "Public" : "Dim";
  return `${declaration} ${symbol.name} As ${symbol.typeName ?? "Variant"}`;
}

function fallbackVbscriptSymbolAt(
  cached: CachedDocument,
  position: Position,
  context: VbProjectContext,
): VbSymbol | undefined {
  const word = vbIdentifierWordAt(cached.parsed.text, cached.source.offsetAt(position));
  if (!word) {
    return undefined;
  }
  const lower = word.toLowerCase();
  return (context.symbols ?? []).find(
    (symbol) =>
      symbol.name.toLowerCase() === lower &&
      (symbol.sourceUri === cached.parsed.uri || (!symbol.scopeName && !symbol.memberOf)),
  );
}

function fallbackVbMemberCompletions(
  cached: CachedDocument,
  position: Position,
  context: VbProjectContext,
): CompletionItem[] {
  const offset = cached.source.offsetAt(position);
  if (cached.parsed.text.charAt(offset - 1) !== ".") {
    return [];
  }
  const owner = vbIdentifierWordBefore(cached.parsed.text, offset - 1);
  if (!owner) {
    return [];
  }
  const symbol = (context.symbols ?? []).find(
    (candidate) =>
      candidate.name.toLowerCase() === owner.toLowerCase() &&
      (candidate.sourceUri === cached.parsed.uri || (!candidate.scopeName && !candidate.memberOf)),
  );
  const typeName = symbol?.type?.name ?? symbol?.typeName;
  const type = (
    context.typeEnvironment ?? buildVbTypeEnvironment(cached.parsed, context)
  ).types.find((candidate) => candidate.name.toLowerCase() === typeName?.toLowerCase());
  return (
    type?.members.map((member) => ({
      label: member.name,
      kind:
        member.kind === "method"
          ? CompletionItemKind.Method
          : member.kind === "field"
            ? CompletionItemKind.Field
            : CompletionItemKind.Property,
      detail: `${member.kind}${member.type ? ` As ${member.type.name}` : ""}`,
    })) ?? []
  );
}

function vbIdentifierWordAt(sourceText: string, offset: number): string | undefined {
  if (!isVbIdentifierCharacter(sourceText.charAt(offset))) {
    offset -= 1;
  }
  if (!isVbIdentifierCharacter(sourceText.charAt(offset))) {
    return undefined;
  }
  let start = offset;
  while (start > 0 && isVbIdentifierCharacter(sourceText.charAt(start - 1))) {
    start -= 1;
  }
  let end = offset + 1;
  while (end < sourceText.length && isVbIdentifierCharacter(sourceText.charAt(end))) {
    end += 1;
  }
  const word = sourceText.slice(start, end);
  return /^[A-Za-z_]/.test(word) ? word : undefined;
}

function vbIdentifierWordBefore(sourceText: string, offset: number): string | undefined {
  let cursor = offset - 1;
  while (cursor >= 0 && /\s/.test(sourceText.charAt(cursor))) {
    cursor -= 1;
  }
  return vbIdentifierWordAt(sourceText, cursor);
}

function isVbIdentifierCharacter(value: string): boolean {
  return /^[A-Za-z0-9_]$/.test(value);
}

function withCompletionData(
  items: CompletionItem[],
  data: Record<string, unknown>,
): CompletionItem[] {
  return items.map((item) => ({
    ...item,
    data: { ...data, ...(item.data as object | undefined) },
  }));
}

function callHierarchyRootUri(item: CallHierarchyItem): string {
  const data = item.data as { rootUri?: string } | undefined;
  return data?.rootUri ?? item.uri;
}

function resolveEmbeddedCompletion(item: CompletionItem, kind: "html" | "css"): CompletionItem {
  const service = (kind === "html" ? htmlService : cssService) as {
    doResolve?: (completion: CompletionItem) => CompletionItem | Promise<CompletionItem>;
  };
  const resolved = service.doResolve?.(item);
  if (resolved && typeof (resolved as Promise<CompletionItem>).then !== "function") {
    return resolved as CompletionItem;
  }
  const localizer = createLocalizer((item.data as { locale?: AspLocale } | undefined)?.locale);
  return {
    ...item,
    detail:
      item.detail ??
      localizer.t(
        kind === "html" ? "server.completion.html.detail" : "server.completion.css.detail",
      ),
    documentation:
      item.documentation ??
      localizer.t(
        kind === "html"
          ? "server.completion.html.documentation"
          : "server.completion.css.documentation",
      ),
  };
}

function definitionLikeLocation(
  uri: string,
  position: Position,
  mode: JavaScriptMode,
): Location | Location[] | null {
  const cached = getFreshCached(uri);
  if (!cached) {
    return null;
  }
  const region = findRegionAt(cached.parsed, cached.source.offsetAt(position));
  if (!region) {
    return null;
  }
  if (region.language === "vbscript") {
    const context = immediateVbProjectContext(cached, cachedSettings(cached.source.uri));
    const symbol =
      mode === "typeDefinition"
        ? getVbscriptTypeDefinition(cached.parsed, position, context)
        : mode === "implementation"
          ? getVbscriptImplementation(cached.parsed, position, context)
          : (getVbscriptDefinition(cached.parsed, position, context) ??
            fallbackVbscriptSymbolAt(cached, position, context));
    return symbol ? Location.create(symbol.sourceUri, symbol.range) : null;
  }
  if (isJavaScriptLikeRegion(region)) {
    return jsLocations(cached, position, mode);
  }
  if (region.language === "css" && mode !== "implementation") {
    const virtual = getCachedVirtual(cached, "css");
    const virtualPosition = virtual?.sourceMap.toVirtualPosition(position);
    if (!virtual || !virtualPosition) {
      return null;
    }
    const doc = toTextDocument(virtual);
    const location = cssService.findDefinition(
      doc,
      virtualPosition,
      cssService.parseStylesheet(doc),
    );
    return location ? (remapLocation(virtual, location) ?? null) : null;
  }
  return null;
}

function jsLocations(cached: CachedDocument, position: Position, mode: JavaScriptMode): Location[] {
  const context = jsContextAt(cached, position);
  if (!context) {
    return [];
  }
  const definitions =
    mode === "typeDefinition"
      ? context.service.getTypeDefinitionAtPosition(context.fileName, context.offset)
      : mode === "implementation"
        ? context.service.getImplementationAtPosition(context.fileName, context.offset)
        : context.service.getDefinitionAtPosition(context.fileName, context.offset);
  return (
    definitions
      ?.map((definition) => tsDefinitionToLocation(context, definition))
      .filter((location): location is Location => Boolean(location)) ?? []
  );
}

function tsDefinitionToLocation(
  context: JsProjectContext,
  definition: ts.DefinitionInfo | ts.ImplementationLocation,
): Location | undefined {
  return textSpanToLocation(context, definition.fileName, definition.textSpan);
}

function remapLocation(virtual: VirtualDocument, location: Location): Location | undefined {
  const start = virtual.sourceMap.toSourcePosition(location.range.start);
  const end = virtual.sourceMap.toSourcePosition(location.range.end);
  return start && end
    ? Location.create(virtual.uri.replace(`.${virtual.languageId}.virtual`, ""), { start, end })
    : undefined;
}

function virtualSourceUri(virtual: VirtualDocument): string {
  return virtual.uri.replace(`.${virtual.languageId}.virtual`, "");
}

function jsContextAt(cached: CachedDocument, position: Position): JsProjectContext | undefined {
  const region = findRegionAt(cached.parsed, cached.source.offsetAt(position));
  if (!region || !isJavaScriptLikeRegion(region)) {
    return undefined;
  }
  const virtual = getCachedVirtual(cached, region.language);
  const virtualPosition = virtual?.sourceMap.toVirtualPosition(position);
  if (!virtual || !virtualPosition) {
    return undefined;
  }
  const doc = toTextDocument(virtual);
  const project = createJsLanguageService(virtual, cachedSettings(cached.source.uri));
  const fileName = jsProjectFileName(virtual, project);
  return {
    virtual,
    service: project.service,
    fileName,
    offset: doc.offsetAt(virtualPosition),
    files: project.files,
  };
}

function jsVirtualDocuments(cached: CachedDocument): VirtualDocument[] {
  const languages: AspEmbeddedLanguage[] = ["javascript", "jscript"];
  return languages
    .map((language) => getCachedVirtual(cached, language))
    .filter((virtual): virtual is VirtualDocument => Boolean(virtual));
}

function workspaceSymbolsForCached(cached: CachedDocument): SymbolInformation[] {
  return [
    ...includeSymbols(cached),
    ...htmlWorkspaceSymbols(cached),
    ...cssWorkspaceSymbols(cached),
    ...jsWorkspaceSymbols(cached),
  ];
}

function includeSymbols(cached: CachedDocument): SymbolInformation[] {
  return cached.parsed.includes.map((include) =>
    SymbolInformation.create(
      include.path,
      SymbolKind.File,
      include.range,
      cached.source.uri,
      "include",
    ),
  );
}

function htmlWorkspaceSymbols(cached: CachedDocument): SymbolInformation[] {
  const symbols: SymbolInformation[] = [];
  const html = getCachedVirtual(cached, "html");
  if (!html) {
    return symbols;
  }
  const text = html.text;
  const pattern = /\b(?:id|name)=["']([^"']+)["']/gi;
  let match: RegExpExecArray | null;
  while ((match = pattern.exec(text))) {
    const start = match.index + match[0].indexOf(match[1]);
    const end = start + match[1].length;
    const range = textSpanToSourceRange(html, { start, length: end - start });
    if (range) {
      symbols.push(
        SymbolInformation.create(match[1], SymbolKind.Key, range, cached.source.uri, "html"),
      );
    }
  }
  return symbols;
}

function cssWorkspaceSymbols(cached: CachedDocument): SymbolInformation[] {
  return cssDocumentSymbols(cached).map((symbol) =>
    SymbolInformation.create(
      symbol.name,
      symbol.kind,
      symbol.selectionRange,
      cached.source.uri,
      "css",
    ),
  );
}

function jsWorkspaceSymbols(cached: CachedDocument): SymbolInformation[] {
  return jsDocumentSymbols(cached).map((symbol) =>
    SymbolInformation.create(
      symbol.name,
      symbol.kind,
      symbol.selectionRange,
      cached.source.uri,
      "javascript",
    ),
  );
}

function cssDocumentSymbols(cached: CachedDocument): DocumentSymbol[] {
  const virtual = getCachedVirtual(cached, "css");
  if (!virtual) {
    return [];
  }
  const doc = toTextDocument(virtual);
  return cssService
    .findDocumentSymbols2(doc, cssService.parseStylesheet(doc))
    .map((symbol) => remapDocumentSymbol(virtual, symbol))
    .filter((symbol): symbol is DocumentSymbol => Boolean(symbol));
}

function jsDocumentSymbols(cached: CachedDocument): DocumentSymbol[] {
  return jsVirtualDocuments(cached).flatMap((virtual) => {
    const project = createJsLanguageService(virtual, cachedSettings(virtualSourceUri(virtual)));
    const tree = project.service.getNavigationTree(jsProjectFileName(virtual, project));
    return (tree.childItems ?? [])
      .map((item) => navigationTreeToDocumentSymbol(virtual, item))
      .filter((symbol): symbol is DocumentSymbol => Boolean(symbol));
  });
}

function navigationTreeToDocumentSymbol(
  activeVirtual: VirtualDocument,
  item: ts.NavigationTree,
): DocumentSymbol | undefined {
  const primarySpan = item.spans[0];
  if (!primarySpan) {
    return undefined;
  }
  const range = textSpanToSourceRange(activeVirtual, primarySpan);
  const selectionRange = textSpanToSourceRange(activeVirtual, item.nameSpan ?? primarySpan);
  if (!range || !selectionRange) {
    return undefined;
  }
  return {
    name: item.text,
    detail: item.kindModifiers,
    kind: tsSymbolKind(item.kind),
    range,
    selectionRange,
    children: (item.childItems ?? [])
      .map((child) => navigationTreeToDocumentSymbol(activeVirtual, child))
      .filter((symbol): symbol is DocumentSymbol => Boolean(symbol)),
  };
}

function remapDocumentSymbol(
  virtual: VirtualDocument,
  symbol: DocumentSymbol,
): DocumentSymbol | undefined {
  const range = sourceRangeFromVirtualRange(virtual, symbol.range);
  const selectionRange = sourceRangeFromVirtualRange(virtual, symbol.selectionRange);
  if (!range || !selectionRange) {
    return undefined;
  }
  return {
    ...symbol,
    range,
    selectionRange,
    children: symbol.children
      ?.map((child) => remapDocumentSymbol(virtual, child))
      .filter((child): child is DocumentSymbol => Boolean(child)),
  };
}

function cssFoldingRanges(cached: CachedDocument): FoldingRange[] {
  const virtual = getCachedVirtual(cached, "css");
  if (!virtual) {
    return [];
  }
  return cssService
    .getFoldingRanges(toTextDocument(virtual), {})
    .map((range) => remapFoldingRange(virtual, range))
    .filter((range): range is FoldingRange => Boolean(range));
}

function jsFoldingRanges(cached: CachedDocument): FoldingRange[] {
  return jsVirtualDocuments(cached).flatMap((virtual) => {
    const project = createJsLanguageService(virtual, cachedSettings(virtualSourceUri(virtual)));
    return project.service
      .getOutliningSpans(jsProjectFileName(virtual, project))
      .map((span) => textSpanToSourceRange(virtual, span.textSpan))
      .filter((range): range is Range => Boolean(range))
      .map((range) => ({ startLine: range.start.line, endLine: range.end.line }));
  });
}

function vbscriptFoldingRanges(cached: CachedDocument): FoldingRange[] {
  const context = bestEffortVbProjectContext(cached, cachedSettings(cached.source.uri));
  return (context.symbols ?? [])
    .filter((symbol) => symbol.sourceUri === cached.source.uri && symbol.scopeRange)
    .map((symbol) => symbol.scopeRange)
    .filter((range): range is Range => Boolean(range))
    .filter((range) => range.start.line < range.end.line)
    .map((range) => ({ startLine: range.start.line, endLine: range.end.line }));
}

function remapFoldingRange(
  virtual: VirtualDocument,
  range: FoldingRange,
): FoldingRange | undefined {
  const doc = toTextDocument(virtual);
  const start = virtual.sourceMap.toSourcePosition({
    line: range.startLine,
    character: range.startCharacter ?? 0,
  });
  const endOffset = doc.offsetAt({
    line: range.endLine,
    character: range.endCharacter ?? Number.MAX_SAFE_INTEGER,
  });
  const end = virtual.sourceMap.toSourcePosition(doc.positionAt(endOffset));
  return start && end ? { ...range, startLine: start.line, endLine: end.line } : undefined;
}

function textSpanToSourceRange(virtual: VirtualDocument, span: ts.TextSpan): Range | undefined {
  const doc = toTextDocument(virtual);
  const start = virtual.sourceMap.toSourcePosition(doc.positionAt(span.start));
  const end = virtual.sourceMap.toSourcePosition(doc.positionAt(span.start + span.length));
  return start && end ? { start, end } : undefined;
}

function textSpanToLocation(
  context: JsProjectContext,
  fileName: string,
  span: ts.TextSpan,
): Location | undefined {
  const file = context.files.get(normalizeFileName(fileName));
  if (!file) {
    return undefined;
  }
  if (file.virtual) {
    const range = textSpanToSourceRange(file.virtual, span);
    return range ? Location.create(virtualSourceUri(file.virtual), range) : undefined;
  }
  const doc = TextDocument.create(file.uri, "javascript", 0, file.text);
  return Location.create(file.uri, {
    start: doc.positionAt(span.start),
    end: doc.positionAt(span.start + span.length),
  });
}

function tsReferenceToLocation(
  context: JsProjectContext,
  reference: ts.ReferenceEntry,
): Location | undefined {
  return textSpanToLocation(context, reference.fileName, reference.textSpan);
}

function buildVbProjectContext(cached: CachedDocument, settings: AspSettings): VbProjectContext {
  const rootKey = vbProjectRootContextCacheKey(cached, settings);
  const project = collectCachedVbProjectAnalysis(cached, settings);
  const documents = project.documents;
  const contextSettings = vbProjectContextSettings(settings);
  const key = vbProjectContextCacheKey(documents, settings);
  if (cached.analysis?.vbProjectContext?.key === key) {
    return { ...cached.analysis.vbProjectContext.context, locale: settings.resolvedLocale };
  }
  const globalCached = vbProjectContextCache.get(key);
  if (globalCached) {
    globalCached.lastUsed = Date.now();
    const context = { ...globalCached.context, locale: settings.resolvedLocale };
    analysisFor(cached).vbProjectContext = { key, rootKey, context: globalCached.context };
    return context;
  }
  const context = {
    documents,
    symbols: project.symbols,
    typeEnvironment: project.typeEnvironment,
    externalRefUsages: project.externalRefUsages,
    ...contextSettings,
  };
  rememberVbProjectContext(key, context);
  analysisFor(cached).vbProjectContext = { key, rootKey, context };
  return { ...context, locale: settings.resolvedLocale };
}

function immediateVbProjectContext(
  cached: CachedDocument,
  settings: AspSettings,
): VbProjectContext {
  return buildVbProjectContext(cached, settings);
}

function bestEffortVbProjectContext(
  cached: CachedDocument,
  settings: AspSettings,
): VbProjectContext {
  return buildVbProjectContext(cached, settings);
}

function refreshIncludePublicBoundariesForAspChanges(changes: WatchedAspFileChange[]): Set<string> {
  const changed = new Set<string>();
  includeDocumentLoader.invalidateFiles(changes.map((change) => change.fileName));
  for (const change of changes) {
    const fileName = normalizeFileName(change.fileName);
    const previous = includeDocumentLoader.cachedPublicSummary(fileName);
    const uri = pathToFileUri(fileName);
    const next =
      change.type === FileChangeType.Deleted
        ? undefined
        : includeDocumentLoader.read(fileName, cachedSettings(uri));
    const nextFingerprint = next?.publicFingerprint ?? "missing";
    if (previous?.publicFingerprint === nextFingerprint) {
      logDebugSummary(
        cachedSettings(uri),
        `[asp-lsp] include.publicBoundary.reuse: ${uri}, fingerprint=${nextFingerprint}`,
      );
      continue;
    }
    changed.add(fileName);
    aspProjectBuilderState.markFileAffected(fileName, "watchedAsp.publicBoundary");
    logInvalidation(
      "includePublicBoundary",
      `watchedAsp.changed, uri=${uri}, previous=${previous?.publicFingerprint ?? "missing"}, next=${nextFingerprint}`,
    );
  }
  return changed;
}

function ensureIncludeGraphForOpenDocuments(publicChangedFiles: Set<string>): void {
  const changedUris = new Set([...publicChangedFiles].map(pathToFileUri));
  for (const document of documents.all()) {
    const cached = ensureFreshCachedDocument(document);
    const existing = includeForwardDependencies.get(cached.source.uri);
    if (existing && setsIntersect(existing, changedUris)) {
      continue;
    }
    collectCachedVbProjectDocuments(cached, cachedSettings(cached.source.uri));
  }
}

function setsIntersect<T>(left: Set<T>, right: Set<T>): boolean {
  for (const item of left) {
    if (right.has(item)) {
      return true;
    }
  }
  return false;
}

function affectedOpenUrisForAspChanges(
  changes: WatchedAspFileChange[],
  publicChangedFiles: Set<string>,
): Set<string> {
  const allOpenUris = openDocumentUris();
  const affected = new Set<string>();
  for (const change of changes) {
    const changedUri = pathToFileUri(change.fileName);
    if (allOpenUris.has(changedUri)) {
      affected.add(changedUri);
    }
    if (!publicChangedFiles.has(normalizeFileName(change.fileName))) {
      continue;
    }
    for (const dependent of includeReverseDependencies.get(changedUri) ?? []) {
      if (allOpenUris.has(dependent)) {
        affected.add(dependent);
      }
    }
  }
  return affected;
}

function openDocumentUris(): Set<string> {
  return new Set(documents.all().map((document) => document.uri));
}

function resetIncludeDependencies(ownerUri: string): void {
  const previous = includeForwardDependencies.get(ownerUri);
  if (!previous) {
    return;
  }
  for (const includeUri of previous) {
    const owners = includeReverseDependencies.get(includeUri);
    owners?.delete(ownerUri);
    if (owners?.size === 0) {
      includeReverseDependencies.delete(includeUri);
    }
  }
  includeForwardDependencies.delete(ownerUri);
}

function recordIncludeDependency(ownerUri: string, includeUri: string): void {
  let forward = includeForwardDependencies.get(ownerUri);
  if (!forward) {
    forward = new Set();
    includeForwardDependencies.set(ownerUri, forward);
  }
  forward.add(includeUri);
  let reverse = includeReverseDependencies.get(includeUri);
  if (!reverse) {
    reverse = new Set();
    includeReverseDependencies.set(includeUri, reverse);
  }
  reverse.add(ownerUri);
}

function clearIncludeGraph(): void {
  includeForwardDependencies.clear();
  includeReverseDependencies.clear();
  includePublicSummaries.clear();
  aspProjectBuilderState.clear();
}

function invalidateCachedAnalysisForUris(uris: Set<string>, reason = "analysis.invalidate"): void {
  if (uris.size > 0) {
    vbProjectContextCache.clear();
    logInvalidation("analysis", `${reason}, files=${uris.size}`);
  }
  for (const uri of uris) {
    const cached = cache.get(uri);
    if (cached) {
      cached.analysis = undefined;
    }
    clearSemanticTokensForUri(uri);
  }
}

function vbProjectContextSettings(
  settings: AspSettings,
): Omit<VbProjectContext, "documents" | "symbols" | "typeEnvironment" | "locale"> {
  return {
    typeChecking: settings.vbscript?.typeChecking,
    identifierCase: settings.vbscript?.identifierCase,
    identifierCaseByKind: settings.vbscript?.identifierCaseByKind,
    comTypes: settings.vbscript?.comTypes,
    unusedDiagnostics: settings.vbscript?.unusedDiagnostics !== false,
    syntaxSnippets: settings.vbscript?.syntaxSnippets !== false,
  };
}

function vbProjectRootContextCacheKey(cached: CachedDocument, settings: AspSettings): string {
  return JSON.stringify({
    root: vbProjectDocumentCollectionKey(cached, settings),
    settings: {
      typeChecking: settings.vbscript?.typeChecking,
      identifierCase: settings.vbscript?.identifierCase,
      identifierCaseByKind: settings.vbscript?.identifierCaseByKind,
      comTypes: settings.vbscript?.comTypes,
      unusedDiagnostics: settings.vbscript?.unusedDiagnostics !== false,
      syntaxSnippets: settings.vbscript?.syntaxSnippets !== false,
    },
    globals: settings.vbscript?.globals,
  });
}

function rememberVbProjectContext(key: string, context: VbProjectContext): void {
  vbProjectContextCache.set(key, { context, lastUsed: Date.now() });
  if (vbProjectContextCache.size <= maxVbProjectContextCacheEntries) {
    return;
  }
  const oldest = [...vbProjectContextCache.entries()].sort(
    (left, right) => left[1].lastUsed - right[1].lastUsed,
  )[0]?.[0];
  if (oldest) {
    vbProjectContextCache.delete(oldest);
  }
}

function vbProjectContextCacheKey(documents: AspParsedDocument[], settings: AspSettings): string {
  return JSON.stringify({
    documents: documents.map((document) => ({
      uri: document.uri,
      vbscript: vbProjectDocumentFingerprint(document),
    })),
    settings: {
      typeChecking: settings.vbscript?.typeChecking,
      identifierCase: settings.vbscript?.identifierCase,
      identifierCaseByKind: settings.vbscript?.identifierCaseByKind,
      comTypes: settings.vbscript?.comTypes,
      unusedDiagnostics: settings.vbscript?.unusedDiagnostics !== false,
      syntaxSnippets: settings.vbscript?.syntaxSnippets !== false,
    },
    globals: settings.vbscript?.globals,
  });
}

function vbProjectDocumentFingerprint(document: AspParsedDocument): unknown {
  return {
    defaultLanguage: document.defaultLanguage,
    includes: document.includes.map((include) => ({
      offset: include.offset,
      path: include.path,
      mode: include.mode,
    })),
    regions: document.regions
      .filter((region) => region.language === "vbscript")
      .map((region) => ({
        kind: region.kind,
        start: region.start,
        end: region.end,
        contentStart: region.contentStart,
        contentEnd: region.contentEnd,
        text: textFingerprint(document.text.slice(region.contentStart, region.contentEnd)),
      })),
  };
}

function configuredVbscriptGlobals(cached: CachedDocument, settings: AspSettings): VbSymbol[] {
  const globals = settings.vbscript?.globals;
  if (!globals) {
    return [];
  }
  const range = {
    start: { line: 0, character: 0 },
    end: { line: 0, character: 0 },
  };
  return Object.entries(globals).flatMap(([name, value]) => {
    const typeName = typeof value === "string" ? value : value.type;
    if (!/^[A-Za-z][A-Za-z0-9_]*$/.test(name) || !typeName) {
      return [];
    }
    const type = parseVbscriptTypeRef(typeName);
    return [
      {
        name,
        kind: typeof value === "object" && value.kind === "constant" ? "constant" : "variable",
        range,
        sourceUri: `${cached.source.uri}#runtime-global`,
        typeName: type.name,
        type,
      } satisfies VbSymbol,
    ];
  });
}

function collectCachedVbProjectAnalysis(
  cached: CachedDocument,
  settings: AspSettings,
): VbProjectAnalysis {
  const documents = collectCachedVbProjectDocuments(cached, settings);
  const key = vbProjectContextCacheKey(documents, settings);
  const existing = cached.analysis?.vbProjectAnalysis;
  if (existing?.key === key) {
    return existing.analysis;
  }
  const contextSettings = vbProjectContextSettings(settings);
  const summaries = documents.map((document) =>
    fileAnalysisSummaryForProjectDocument(cached, document, settings, contextSettings),
  );
  const symbols = summaries.flatMap((summary) => summary.vbscript?.localSymbols ?? []);
  symbols.push(...configuredVbscriptGlobals(cached, settings));
  const typeEnvironment = mergeVbTypeEnvironment(
    buildVbTypeEnvironment(cached.parsed, { ...contextSettings, symbols }),
    summaries.flatMap((summary) => summary.vbscript?.typeFacts ?? []),
    symbols,
  );
  const analysis = {
    documents,
    symbols,
    typeEnvironment,
    externalRefUsages: summaries.flatMap((summary) => summary.vbscript?.externalRefUsages ?? []),
  };
  analysisFor(cached).vbProjectAnalysis = { key, analysis };
  return analysis;
}

function fileAnalysisSummaryForProjectDocument(
  cached: CachedDocument,
  document: AspParsedDocument,
  settings: AspSettings,
  context: VbProjectContext,
): FileAnalysisSummary {
  if (document.uri !== cached.parsed.uri) {
    const entry = includeDocumentLoader.read(uriToFileName(document.uri), settings);
    if (entry?.parsed === document || entry?.key) {
      return entry.summary;
    }
  }
  return cachedFileAnalysisSummary(cached, context);
}

function cachedFileAnalysisSummary(
  cached: CachedDocument,
  context: VbProjectContext,
): FileAnalysisSummary {
  const key = JSON.stringify({
    document: vbProjectDocumentFingerprint(cached.parsed),
    context: {
      typeChecking: context.typeChecking,
      identifierCase: context.identifierCase,
      identifierCaseByKind: context.identifierCaseByKind,
      comTypes: context.comTypes,
      unusedDiagnostics: context.unusedDiagnostics,
      syntaxSnippets: context.syntaxSnippets,
    },
  });
  const existing = cached.analysis?.vbFileSummary;
  if (existing?.key === key) {
    aspProjectBuilderState.updateFromSummary(
      cached,
      existing.summary,
      cachedSettings(cached.source.uri),
      "summary.reuse",
    );
    return existing.summary;
  }
  const summary = summarizeAspFileAnalysis(cached.parsed, context);
  analysisFor(cached).vbFileSummary = { key, summary };
  aspProjectBuilderState.updateFromSummary(
    cached,
    summary,
    cachedSettings(cached.source.uri),
    "summary.update",
  );
  return summary;
}

function mergeVbTypeEnvironment(
  base: VbTypeEnvironment,
  facts: VbType[],
  symbols: VbSymbol[],
): VbTypeEnvironment {
  const byName = new Map<string, VbType>();
  for (const type of base.types) {
    byName.set(type.name.toLowerCase(), type);
  }
  for (const type of facts) {
    const key = type.name.toLowerCase();
    const existing = byName.get(key);
    byName.set(
      key,
      existing
        ? {
            ...existing,
            members: mergeVbMembers(existing.members, type.members),
          }
        : type,
    );
  }
  return {
    types: [...byName.values()],
    symbols,
  };
}

function mergeVbMembers(left: VbType["members"], right: VbType["members"]): VbType["members"] {
  const byName = new Map(left.map((member) => [member.name.toLowerCase(), member]));
  for (const member of right) {
    byName.set(member.name.toLowerCase(), member);
  }
  return [...byName.values()];
}

function collectCachedVbProjectDocuments(
  cached: CachedDocument,
  settings: AspSettings,
): AspParsedDocument[] {
  const collectionKey = vbProjectDocumentCollectionKey(cached, settings);
  const existing = cached.analysis?.vbProjectDocuments;
  if (existing?.collectionKey === collectionKey) {
    return existing.documents;
  }
  const documents = collectVbProjectDocuments(cached.parsed, settings);
  analysisFor(cached).vbProjectDocuments = { collectionKey, documents };
  return documents;
}

function vbProjectDocumentCollectionKey(cached: CachedDocument, settings: AspSettings): string {
  return JSON.stringify({
    uri: cached.source.uri,
    text: textFingerprint(cached.parsed.text),
    resolution: includeResolutionSettingsKey(settings),
  });
}

function collectVbProjectDocuments(
  root: AspParsedDocument,
  settings: AspSettings,
): AspParsedDocument[] {
  const documents: AspParsedDocument[] = [];
  const visited = new Set<string>();
  resetIncludeDependencies(root.uri);
  const visit = (document: AspParsedDocument, depth: number): void => {
    if (depth > 20 || visited.has(document.uri)) {
      return;
    }
    visited.add(document.uri);
    documents.push(document);
    for (const include of document.includes) {
      const resolved = resolveIncludePath(document.uri, include.path, include.mode, settings);
      const uri = pathToFileUri(resolved);
      recordIncludeDependency(root.uri, uri);
      if (!fs.existsSync(resolved)) {
        continue;
      }
      if (visited.has(uri)) {
        continue;
      }
      visit(readParsedIncludeDocument(resolved, settings), depth + 1);
    }
  };
  visit(root, 0);
  return documents;
}

function readParsedIncludeDocument(fileName: string, settings: AspSettings): AspParsedDocument {
  const entry = includeDocumentLoader.read(fileName, settings);
  if (!entry) {
    throw new Error(`Include document does not exist: ${fileName}`);
  }
  return entry.parsed;
}

async function readParsedIncludeDocumentAsync(
  fileName: string,
  settings: AspSettings,
): Promise<AspParsedDocument> {
  const entry = await includeDocumentLoader.readAsync(fileName, settings);
  if (!entry) {
    throw new Error(`Include document does not exist: ${fileName}`);
  }
  return entry.parsed;
}

function readTextFile(fileName: string, encoding: AspLegacyEncoding | undefined): string {
  return decodeLegacyText(fs.readFileSync(fileName), encoding);
}

function includeDocumentCacheKey(fileName: string, settings: AspSettings): string | undefined {
  const stat = fs.statSync(fileName, { throwIfNoEntry: false });
  if (!stat?.isFile()) {
    return undefined;
  }
  return JSON.stringify({
    fileName,
    mtimeMs: stat.mtimeMs,
    size: stat.size,
    settings: includeDocumentSettingsIdentity(settings),
  });
}

async function includeDocumentCacheKeyAsync(
  fileName: string,
  settings: AspSettings,
): Promise<string | undefined> {
  const stat = await fs.promises.stat(fileName).catch(() => undefined);
  if (!stat?.isFile()) {
    return undefined;
  }
  return JSON.stringify({
    fileName,
    mtimeMs: stat.mtimeMs,
    size: stat.size,
    settings: includeDocumentSettingsIdentity(settings),
  });
}

function includeDocumentSettingsIdentity(settings: AspSettings): string {
  return JSON.stringify({
    parse: parseSettingsIdentity(settings),
    legacyEncoding: settings.legacyEncoding,
    vbscript: vbProjectContextSettings(settings),
  });
}

function createIncludeDocumentCacheEntry(
  fileName: string,
  text: string,
  settings: AspSettings,
  key: string,
): IncludeDocumentCacheEntry {
  const parsed = parseAspDocument(pathToFileUri(fileName), text, settings);
  const summary = summarizeAspFileAnalysis(parsed, vbProjectContextSettings(settings));
  const publicSignature = filePublicSignature(summary);
  return {
    key,
    fileName,
    uri: parsed.uri,
    parsed,
    summary,
    publicFingerprint: publicSignature.fingerprint,
    publicSignature,
  };
}

function rememberIncludePublicSummary(
  entry: IncludeDocumentCacheEntry,
  settings?: AspSettings,
): void {
  includePublicSummaries.set(entry.fileName, {
    fileName: entry.fileName,
    uri: entry.uri,
    key: entry.key,
    publicFingerprint: entry.publicFingerprint,
    publicSignature: entry.publicSignature,
  });
  aspProjectBuilderState.updateIncludeSummary(entry, settings ?? cachedSettings(entry.uri));
}

function filePublicSignature(summary: FileAnalysisSummary): FilePublicSignature {
  const languages = [...new Set(summary.languageRegions.map((region) => region.language))].sort();
  const exports = summary.vbscript?.exports.map(publicExportBoundary) ?? [];
  const externalRefUsages =
    summary.vbscript?.externalRefUsages.map((usage) => ({
      key: usage.key,
      name: usage.name,
      memberName: usage.memberName,
      kindHint: usage.kindHint,
      count: usage.count,
    })) ?? [];
  const affectsGlobalScope =
    summary.defaultLanguage === "VBScript" ||
    exports.length > 0 ||
    externalRefUsages.length > 0 ||
    summary.languageRegions.some((region) => region.kind === "server-script");
  const payload = {
    defaultLanguage: summary.defaultLanguage,
    languages,
    includes: summary.includeRefs.map((include) => ({
      path: include.path,
      mode: include.mode,
    })),
    vbscript: {
      exports,
      externalRefUsages,
    },
    affectsGlobalScope,
  };
  return {
    fingerprint: textFingerprint(JSON.stringify(payload)),
    defaultLanguage: summary.defaultLanguage,
    languages,
    exports,
    externalRefUsages,
    affectsGlobalScope,
  };
}

function publicExportBoundary(
  summary: NonNullable<FileAnalysisSummary["vbscript"]>["exports"][number],
): unknown {
  return {
    name: summary.name,
    kind: summary.kind,
    typeName: summary.typeName,
    memberOf: summary.memberOf,
    visibility: summary.visibility,
    members: summary.members?.map(publicExportBoundary),
  };
}

function builderStateChangeReasons(
  previous: AspProjectFileBuilderState | undefined,
  cached: CachedDocument,
  summary: FileAnalysisSummary,
  signature: FilePublicSignature,
): string[] {
  if (!previous) {
    return ["new"];
  }
  const reasons: string[] = [];
  if (previous.version !== cached.identity.version) {
    reasons.push("version");
  }
  if (previous.textFingerprint !== textFingerprint(summary.fingerprint)) {
    reasons.push("text");
  }
  if (previous.publicSignature.fingerprint !== signature.fingerprint) {
    reasons.push("publicSignature");
  }
  const includeDeps = summary.includeRefs.map(
    (include) => `${include.mode}:${include.path.toLowerCase()}`,
  );
  if (previous.includeDeps.join("|") !== includeDeps.join("|")) {
    reasons.push("includeDeps");
  }
  return reasons;
}

async function includeDiagnosticsAsync(
  cached: CachedDocument,
  settings: AspSettings,
  cancellation: AnalysisCancellation,
): Promise<Diagnostic[]> {
  const diagnostics: Diagnostic[] = [];
  const owner = uriToFileName(cached.source.uri);
  const localizer = localizerForSettings(settings);
  for (const include of cached.parsed.includes) {
    if (cancellation.isCancellationRequested()) {
      return [];
    }
    const resolved = resolveIncludePathDetails(
      cached.source.uri,
      include.path,
      include.mode,
      settings,
    );
    recordIncludeDependency(cached.source.uri, pathToFileUri(resolved.fileName));
    if (!resolved.exists) {
      diagnostics.push({
        severity: DiagnosticSeverity.Warning,
        range: include.range,
        message: localizer.t("server.include.unresolved", { path: include.path }),
        code: "include.missing",
        source: "asp-lsp-include",
      });
      continue;
    }
    if (settings.windowsPathResolution !== false && !resolved.pathCaseMatches) {
      diagnostics.push({
        severity: DiagnosticSeverity.Warning,
        range: include.pathRange,
        message: localizer.t("server.include.caseMismatch", {
          path: include.path,
          actualPath: resolved.actualIncludePath ?? resolved.actualPath ?? resolved.fileName,
        }),
        code: "include.pathCaseMismatch",
        source: "asp-lsp-include",
      });
    }
    if (sameFile(resolved.fileName, owner)) {
      diagnostics.push({
        severity: DiagnosticSeverity.Warning,
        range: include.range,
        message: localizer.t("server.include.currentDocument"),
        code: "include.currentDocument",
        source: "asp-lsp-include",
      });
      continue;
    }
    const cycle = await findIncludeCycleAsync(owner, resolved.fileName, settings, cancellation);
    if (cancellation.isCancellationRequested()) {
      return [];
    }
    if (cycle) {
      diagnostics.push({
        severity: DiagnosticSeverity.Warning,
        range: include.range,
        message: localizer.t("server.include.cycle", {
          cycle: cycle.map((fileName) => path.basename(fileName)).join(" -> "),
        }),
        code: "include.cycle",
        source: "asp-lsp-include",
      });
    }
    await yieldToEventLoop();
  }
  return diagnostics;
}

function includeRenameWorkspaceEdit(
  files: Array<{ oldUri: string; newUri: string }>,
): WorkspaceEdit | null {
  ensureWorkspaceIndex();
  const changes: NonNullable<WorkspaceEdit["changes"]> = {};
  const seenEdits = new Set<string>();
  const renamePairs = files.map((file) => ({
    oldFile: normalizeFileName(uriToFileName(file.oldUri)),
    newFile: normalizeFileName(uriToFileName(file.newUri)),
  }));
  const candidates = [
    ...documents.all().flatMap((document) => {
      const cached = ensureFreshCachedDocument(document);
      return cached ? [cached] : [];
    }),
    ...[...workspaceIndex.values()].map(cachedFromIndexed),
  ];
  for (const cached of candidates) {
    const settings = cachedSettings(cached.source.uri);
    for (const include of cached.parsed.includes) {
      const resolved = normalizeFileName(
        resolveIncludePath(cached.source.uri, include.path, include.mode, settings),
      );
      const pair = renamePairs.find((item) => sameFile(item.oldFile, resolved));
      if (!pair) {
        continue;
      }
      const key = `${cached.source.uri}:${include.range.start.line}:${include.range.start.character}`;
      if (seenEdits.has(key)) {
        continue;
      }
      seenEdits.add(key);
      const nextPath = includePathForRenamedTarget(
        cached.source.uri,
        include.mode,
        pair.newFile,
        settings,
      );
      changes[cached.source.uri] = [
        ...(changes[cached.source.uri] ?? []),
        {
          range: include.range,
          newText: `<!-- #include ${include.mode}="${nextPath}" -->`,
        },
      ];
    }
  }
  return Object.keys(changes).length > 0 ? { changes } : null;
}

function includePathForRenamedTarget(
  ownerUri: string,
  mode: "file" | "virtual",
  targetFile: string,
  settings: AspSettings,
): string {
  if (mode === "file") {
    return path
      .relative(path.dirname(uriToFileName(ownerUri)), targetFile)
      .split(path.sep)
      .join("/");
  }
  for (const root of [...(settings.virtualRoots ?? []), settings.virtualRoot, ...workspaceRoots]) {
    if (!root) {
      continue;
    }
    const relative = path.relative(root, targetFile);
    if (!relative.startsWith("..") && !path.isAbsolute(relative)) {
      return `/${relative.split(path.sep).join("/")}`;
    }
  }
  return `/${path.basename(targetFile)}`;
}

async function findIncludeCycleAsync(
  owner: string,
  start: string,
  settings: AspSettings,
  cancellation: AnalysisCancellation,
): Promise<string[] | undefined> {
  const cacheKey = includeCycleCacheKey(owner, start, settings);
  if (includeCycleCache.has(cacheKey)) {
    return includeCycleCache.get(cacheKey) ?? undefined;
  }
  if (!(await fileExistsAsync(start)) || cancellation.isCancellationRequested()) {
    includeCycleCache.set(cacheKey, null);
    return undefined;
  }
  const visited = new Set<string>();
  const stack: string[] = [];
  const stackIndexes = new Map<string, number>();
  const search = async (fileName: string, depth: number): Promise<string[] | undefined> => {
    if (depth > 20 || cancellation.isCancellationRequested()) {
      return undefined;
    }
    const normalized = normalizeFileName(fileName);
    if (sameFile(normalized, owner) && stack.length > 0) {
      return [...stack, owner];
    }
    const existingStackIndex = stackIndexes.get(normalized);
    if (existingStackIndex !== undefined) {
      return [...stack.slice(existingStackIndex), normalized];
    }
    if (visited.has(normalized)) {
      return undefined;
    }
    visited.add(normalized);
    stackIndexes.set(normalized, stack.length);
    stack.push(normalized);
    const parsed = await readParsedIncludeDocumentAsync(normalized, settings).catch(
      () => undefined,
    );
    await yieldToEventLoop();
    if (!parsed || cancellation.isCancellationRequested()) {
      stack.pop();
      stackIndexes.delete(normalized);
      return undefined;
    }
    for (const include of parsed.includes) {
      const next = resolveIncludePath(
        pathToFileUri(normalized),
        include.path,
        include.mode,
        settings,
      );
      if (!(await fileExistsAsync(next))) {
        continue;
      }
      const cycle = await search(next, depth + 1);
      if (cycle) {
        return cycle;
      }
    }
    stack.pop();
    stackIndexes.delete(normalized);
    return undefined;
  };
  const cycle = await search(start, 0);
  if (!cancellation.isCancellationRequested()) {
    includeCycleCache.set(cacheKey, cycle ?? null);
  }
  return cycle;
}

function includeCycleCacheKey(owner: string, start: string, settings: AspSettings): string {
  return JSON.stringify({
    owner: normalizeFileName(owner),
    start: normalizeFileName(start),
    resolution: includeResolutionSettingsKey(settings),
  });
}

function remapDiagnostic(
  virtual: VirtualDocument,
  diagnostic: Diagnostic,
  source: string,
): Diagnostic | undefined {
  const start = virtual.sourceMap.toSourcePosition(diagnostic.range.start);
  const end = virtual.sourceMap.toSourcePosition(diagnostic.range.end);
  if (!start || !end) {
    return undefined;
  }
  return { ...diagnostic, range: { start, end }, source };
}

function selectionRangeAt(cached: CachedDocument, position: Position): SelectionRange {
  const region = findRegionAt(cached.parsed, cached.source.offsetAt(position));
  if (region?.language === "html") {
    const virtual = getCachedVirtual(cached, "html");
    if (virtual) {
      return remapSelectionRange(
        virtual,
        htmlService.getSelectionRanges(toTextDocument(virtual), [position])[0],
      );
    }
  }
  if (region?.language === "css") {
    const virtual = getCachedVirtual(cached, "css");
    const virtualPosition = virtual?.sourceMap.toVirtualPosition(position);
    if (virtual && virtualPosition) {
      const doc = toTextDocument(virtual);
      return remapSelectionRange(
        virtual,
        cssService.getSelectionRanges(doc, [virtualPosition], cssService.parseStylesheet(doc))[0],
      );
    }
  }
  if (region?.language === "javascript") {
    const range = jsSelectionRange(cached, position);
    if (range) {
      return range;
    }
  }
  if (region?.language === "vbscript" || region?.language === "jscript") {
    return getVbscriptSelectionRanges(cached.parsed, [position])[0];
  }
  return { range: { start: position, end: position } };
}

function remapSelectionRange(virtual: VirtualDocument, range: SelectionRange): SelectionRange {
  const start = virtual.sourceMap.toSourcePosition(range.range.start) ?? range.range.start;
  const end = virtual.sourceMap.toSourcePosition(range.range.end) ?? range.range.end;
  return {
    range: { start, end },
    parent: range.parent ? remapSelectionRange(virtual, range.parent) : undefined,
  };
}

function jsSelectionRange(cached: CachedDocument, position: Position): SelectionRange | undefined {
  const context = jsContextAt(cached, position);
  if (!context) {
    return undefined;
  }
  const selection = context.service.getSmartSelectionRange(context.fileName, context.offset);
  return remapTsSelectionRange(context.virtual, selection);
}

function remapTsSelectionRange(virtual: VirtualDocument, range: ts.SelectionRange): SelectionRange {
  const doc = toTextDocument(virtual);
  const start = virtual.sourceMap.toSourcePosition(doc.positionAt(range.textSpan.start));
  const end = virtual.sourceMap.toSourcePosition(
    doc.positionAt(range.textSpan.start + range.textSpan.length),
  );
  return {
    range: {
      start: start ?? { line: 0, character: 0 },
      end: end ?? start ?? { line: 0, character: 0 },
    },
    parent: range.parent ? remapTsSelectionRange(virtual, range.parent) : undefined,
  };
}

function cssDocumentColors(cached: CachedDocument): ColorInformation[] {
  const virtual = getCachedVirtual(cached, "css");
  if (!virtual) {
    return [];
  }
  const doc = toTextDocument(virtual);
  return cssService
    .findDocumentColors(doc, cssService.parseStylesheet(doc))
    .map((color) => {
      const range = sourceRangeFromVirtualRange(virtual, color.range);
      return range ? { ...color, range } : undefined;
    })
    .filter((color): color is ColorInformation => Boolean(color));
}

function cssColorPresentations(
  cached: CachedDocument,
  color: Color,
  range: Range,
): ColorPresentation[] {
  const virtual = getCachedVirtual(cached, "css");
  if (!virtual) {
    return [];
  }
  const start = virtual.sourceMap.toVirtualPosition(range.start);
  const end = virtual.sourceMap.toVirtualPosition(range.end);
  if (!start || !end) {
    return [];
  }
  const doc = toTextDocument(virtual);
  return cssService
    .getColorPresentations(doc, cssService.parseStylesheet(doc), color, { start, end })
    .map((presentation) => ({
      ...presentation,
      textEdit: presentation.textEdit
        ? {
            ...presentation.textEdit,
            range: sourceRangeFromVirtualRange(virtual, presentation.textEdit.range) ?? range,
          }
        : undefined,
      additionalTextEdits: presentation.additionalTextEdits
        ?.map((edit) => {
          const editRange = sourceRangeFromVirtualRange(virtual, edit.range);
          return editRange ? { ...edit, range: editRange } : undefined;
        })
        .filter((edit): edit is TextEdit => Boolean(edit)),
    }));
}

function sourceRangeFromVirtualRange(virtual: VirtualDocument, range: Range): Range | undefined {
  const start = virtual.sourceMap.toSourcePosition(range.start);
  const end = virtual.sourceMap.toSourcePosition(range.end);
  return start && end ? { start, end } : undefined;
}

function remapHover(virtual: VirtualDocument, hover: Hover | null): Hover | null {
  if (!hover?.range) {
    return hover;
  }
  const range = sourceRangeFromVirtualRange(virtual, hover.range);
  return range ? { ...hover, range } : { ...hover, range: undefined };
}

function remapWorkspaceEdit(
  virtual: VirtualDocument,
  edit: WorkspaceEdit,
  sourceUri: string,
): WorkspaceEdit {
  const changes: NonNullable<WorkspaceEdit["changes"]> = {};
  for (const [uri, textEdits] of Object.entries(edit.changes ?? {})) {
    const targetUri = uri === virtual.uri ? sourceUri : uri;
    changes[targetUri] = [
      ...(changes[targetUri] ?? []),
      ...textEdits
        .map((textEdit) => {
          const range =
            uri === virtual.uri
              ? sourceRangeFromVirtualRange(virtual, textEdit.range)
              : textEdit.range;
          return range ? { ...textEdit, range } : undefined;
        })
        .filter((textEdit): textEdit is TextEdit => Boolean(textEdit)),
    ];
  }
  return { changes };
}

function tsDiagnosticToLsp(
  virtual: VirtualDocument,
  diagnostic: ts.Diagnostic,
  override: { severity?: DiagnosticSeverity; source?: string } = {},
): Diagnostic | undefined {
  if (diagnostic.start === undefined || diagnostic.length === undefined) {
    return undefined;
  }
  const virtualDoc = toTextDocument(virtual);
  const start = virtualDoc.positionAt(diagnostic.start);
  const end = virtualDoc.positionAt(diagnostic.start + diagnostic.length);
  const sourceStart = virtual.sourceMap.toSourcePosition(start);
  const sourceEnd = virtual.sourceMap.toSourcePosition(end);
  if (
    !sourceStart ||
    !sourceEnd ||
    !virtualRangeStaysWithinSegment(virtual, diagnostic.start, diagnostic.start + diagnostic.length)
  ) {
    return undefined;
  }
  return {
    severity:
      override.severity ??
      (diagnostic.category === ts.DiagnosticCategory.Error
        ? DiagnosticSeverity.Error
        : DiagnosticSeverity.Warning),
    range: { start: sourceStart, end: sourceEnd },
    message: ts.flattenDiagnosticMessageText(diagnostic.messageText, "\n"),
    code: diagnostic.code,
    source: override.source ?? "asp-lsp-typescript",
  };
}

function virtualRangeStaysWithinSegment(
  virtual: VirtualDocument,
  start: number,
  end: number,
): boolean {
  const lastOffset = Math.max(start, end - 1);
  const segment = sourceMapSegmentAtVirtualOffset(virtual, start);
  return Boolean(segment && lastOffset < segment.virtualEnd);
}

function tsDiagnosticKey(diagnostic: ts.Diagnostic): string {
  return [diagnostic.code, diagnostic.start ?? -1, diagnostic.length ?? -1].join(":");
}

function toTextDocument(virtual: VirtualDocument): TextDocument {
  return TextDocument.create(virtual.uri, virtual.languageId, 0, virtual.text);
}

function getCached(uri: string): CachedDocument | undefined {
  const existing = cache.get(uri);
  if (existing) {
    return existing;
  }
  const document = documents.get(uri);
  if (!document) {
    return undefined;
  }
  const settings = cachedSettings(uri);
  const parsed = parseAspDocument(uri, document.getText(), settings);
  const cached = createCachedDocument(document, parsed, settings);
  cache.set(uri, cached);
  return cached;
}

function getIndexedCached(uri: string): CachedDocument | undefined {
  ensureWorkspaceIndex();
  const entry = workspaceIndex.get(normalizeFileName(uriToFileName(uri)));
  return entry ? cachedFromIndexed(entry) : undefined;
}

function cachedFromIndexed(entry: WorkspaceIndexedDocument): CachedDocument {
  const settings = cachedSettings(entry.uri);
  const parsed = parseAspDocument(
    entry.uri,
    readTextFile(entry.fileName, settings.legacyEncoding),
    settings,
  );
  return createCachedDocument(
    TextDocument.create(entry.uri, "classic-asp", 0, parsed.text),
    parsed,
    settings,
  );
}

async function cachedFromIndexedAsync(
  entry: WorkspaceIndexedDocument,
  settings: AspSettings,
): Promise<CachedDocument> {
  const parsed = parseAspDocument(
    entry.uri,
    await readTextFileAsync(entry.fileName, settings.legacyEncoding),
    settings,
  );
  return createCachedDocument(
    TextDocument.create(entry.uri, "classic-asp", 0, parsed.text),
    parsed,
    settings,
  );
}

async function diagnosticsForIndexed(
  entry: WorkspaceIndexedDocument,
  settings: AspSettings,
  token?: { isCancellationRequested?: boolean },
  mode: AnalysisExecutionMode = "workspace",
): Promise<Diagnostic[]> {
  if (token?.isCancellationRequested) {
    return [];
  }
  const sourceMetadata = diskAnalysisSourceMetadata(entry);
  const cached = await cachedFromIndexedAsync(entry, settings);
  if (token?.isCancellationRequested) {
    return [];
  }
  const settingsKey = diskAnalysisSettingsKey(settings, cached.parsed);
  const lookup = {
    source: sourceMetadata,
    settingsKey,
  };
  const cachedAnalysis = diskAnalysisCache.readAnalysis(lookup);
  if (cachedAnalysis) {
    logDebugSummary(settings, `[asp-lsp] diskCache.hit: ${entry.uri}`);
    aspProjectBuilderState.restoreDiskState(
      entry.uri,
      normalizeFileName(entry.fileName),
      sourceMetadata,
      cachedAnalysis.builderState,
      cachedAnalysis.diagnostics,
      settings,
    );
    return cachedAnalysis.diagnostics;
  }
  logDebugSummary(settings, `[asp-lsp] diskCache.miss: ${entry.uri}`);
  logDebugSummary(settings, `[asp-lsp] disk.builder.restore.miss: ${entry.uri}`);
  const cancellation: AnalysisCancellation = {
    isCancellationRequested: () => token?.isCancellationRequested === true,
  };
  if (cancellation.isCancellationRequested()) {
    return [];
  }
  const items = await diagnosticsForCachedAsync(
    cached,
    settings,
    "check.workspace",
    cancellation,
    mode,
  );
  if (cancellation.isCancellationRequested()) {
    return [];
  }
  diskAnalysisCache.write({
    source: sourceMetadata,
    settingsKey,
    diagnostics: items,
    builderState: aspProjectBuilderState.diskStateForUri(cached.source.uri),
  });
  logDebugSummary(settings, `[asp-lsp] diskCache.write: ${entry.uri}`);
  logDebugSummary(settings, `[asp-lsp] disk.builder.persist: ${entry.uri}`);
  return items;
}

function diskAnalysisSourceMetadata(entry: WorkspaceIndexedDocument): DiskAnalysisSourceMetadata {
  return {
    fileName: normalizeFileName(entry.fileName),
    mtimeMs: entry.mtimeMs,
    size: entry.size,
  };
}

function diskAnalysisSettingsKey(settings: AspSettings, parsed: AspParsedDocument): string {
  return JSON.stringify({
    parse: parseSettingsIdentity(settings),
    diagnostics: diagnosticsIdentity(settings),
    include: includeResolutionIdentity(settings),
    includeDependencies: diskAnalysisIncludeDependencyKey(parsed, settings),
    js: jsProjectSettingsIdentity(settings),
    workspace: workspaceIndexSettingsIdentity(settings),
  });
}

function diskAnalysisIncludeDependencyKey(root: AspParsedDocument, settings: AspSettings): string {
  const dependencies: unknown[] = [];
  const visited = new Set<string>();
  const visit = (document: AspParsedDocument, depth: number): void => {
    if (depth > 20) {
      return;
    }
    for (const include of document.includes) {
      const resolved = resolveIncludePathDetails(
        document.uri,
        include.path,
        include.mode,
        settings,
      );
      const normalizedFileName = normalizeFileName(resolved.fileName);
      const stat = fs.statSync(normalizedFileName, { throwIfNoEntry: false });
      const exists = stat?.isFile() === true;
      dependencies.push({
        owner: normalizeFileName(uriToFileName(document.uri)),
        path: include.path,
        mode: include.mode,
        fileName: normalizedFileName,
        exists,
        mtimeMs: exists ? stat.mtimeMs : undefined,
        size: exists ? stat.size : undefined,
        pathCaseMatches: resolved.pathCaseMatches,
        actualPath: resolved.actualPath ? normalizeFileName(resolved.actualPath) : undefined,
      });
      if (!exists || visited.has(normalizedFileName)) {
        continue;
      }
      visited.add(normalizedFileName);
      const entry = includeDocumentLoader.read(normalizedFileName, settings);
      if (entry) {
        visit(entry.parsed, depth + 1);
      }
    }
  };
  visit(root, 0);
  return textFingerprint(JSON.stringify(dependencies));
}

function ensureWorkspaceIndex(): void {
  if (!workspaceIndexDirty) {
    return;
  }
  for (const root of workspaceRoots) {
    indexWorkspaceRoot(root);
  }
  workspaceIndexDirty = false;
}

async function ensureWorkspaceIndexAsync(
  settings: AspSettings,
  token?: { isCancellationRequested?: boolean },
): Promise<void> {
  if (!workspaceIndexDirty) {
    return;
  }
  workspaceIndex.clear();
  workspaceIndexTruncated = false;
  let scannedFiles = 0;
  const maxFiles = settings.workspace?.maxIndexFiles ?? defaultMaxIndexFiles;
  const chunkSize = settings.workspace?.scanChunkSize ?? defaultScanChunkSize;
  for (const root of workspaceRoots) {
    if (token?.isCancellationRequested || scannedFiles >= maxFiles) {
      break;
    }
    scannedFiles = await indexWorkspaceRootAsync(root, settings, {
      scannedFiles,
      maxFiles,
      chunkSize,
      token,
    });
  }
  workspaceIndexTruncated = scannedFiles >= maxFiles;
  workspaceIndexDirty = Boolean(token?.isCancellationRequested);
  if (workspaceIndexTruncated) {
    connection.console.warn(
      createLocalizer(settings.resolvedLocale).t("server.workspaceIndex.truncated", { maxFiles }),
    );
  }
}

async function indexWorkspaceRootAsync(
  root: string,
  settings: AspSettings,
  state: {
    scannedFiles: number;
    maxFiles: number;
    chunkSize: number;
    token?: { isCancellationRequested?: boolean };
  },
): Promise<number> {
  const stat = await fs.promises.stat(root).catch(() => undefined);
  if (!stat?.isDirectory()) {
    return state.scannedFiles;
  }
  const directories = [root];
  let scannedFiles = state.scannedFiles;
  let operations = 0;
  while (directories.length > 0 && scannedFiles < state.maxFiles) {
    if (state.token?.isCancellationRequested) {
      return scannedFiles;
    }
    const directory = directories.pop() ?? root;
    const entries = await fs.promises.readdir(directory, { withFileTypes: true }).catch(() => []);
    for (const entry of entries) {
      if (state.token?.isCancellationRequested || scannedFiles >= state.maxFiles) {
        return scannedFiles;
      }
      const fullPath = path.join(directory, entry.name);
      if (entry.isDirectory()) {
        if (!isExcludedWorkspaceDirectory(entry.name, fullPath)) {
          directories.push(fullPath);
        }
      } else if (entry.isFile() && isAspWorkspaceFile(entry.name)) {
        await indexWorkspaceFileAsync(fullPath);
        scannedFiles += 1;
      }
      operations += 1;
      if (operations % state.chunkSize === 0) {
        await yieldToEventLoop();
      }
    }
  }
  return scannedFiles;
}

async function indexWorkspaceFileAsync(fileName: string): Promise<void> {
  const normalized = normalizeFileName(fileName);
  const stat = await fs.promises.stat(normalized).catch(() => undefined);
  if (!stat?.isFile()) {
    workspaceIndex.delete(normalized);
    return;
  }
  const existing = workspaceIndex.get(normalized);
  if (existing && existing.mtimeMs === stat.mtimeMs && existing.size === stat.size) {
    return;
  }
  const uri = pathToFileUri(normalized);
  workspaceIndex.set(normalized, {
    uri,
    fileName: normalized,
    mtimeMs: stat.mtimeMs,
    size: stat.size,
  });
}

async function readTextFileAsync(
  fileName: string,
  encoding: AspLegacyEncoding | undefined,
): Promise<string> {
  return decodeLegacyText(await fs.promises.readFile(fileName), encoding);
}

function decodeLegacyText(buffer: Uint8Array, encoding: AspLegacyEncoding | undefined): string {
  return new TextDecoder(textDecoderLabel(buffer, encoding)).decode(buffer);
}

function textDecoderLabel(buffer: Uint8Array, encoding: AspLegacyEncoding | undefined): string {
  switch (encoding ?? "auto") {
    case "utf8":
      return "utf-8";
    case "shift_jis":
    case "cp932":
      return "shift_jis";
    case "auto":
      return autoTextDecoderLabel(buffer);
  }
}

function autoTextDecoderLabel(buffer: Uint8Array): string {
  if (isValidUtf8(buffer)) {
    return "utf-8";
  }
  const detected = supportedTextDecoderLabel(detect(buffer));
  if (detected && detected !== "utf-8") {
    return detected;
  }
  return (
    analyse(buffer)
      .map((match) => supportedTextDecoderLabel(match.name))
      .find((label) => label && label !== "utf-8") ?? "utf-8"
  );
}

function supportedTextDecoderLabel(name: string | null): string | undefined {
  const normalized = name?.toLowerCase().replace(/[-_\s]/g, "");
  if (!normalized || normalized === "ascii" || normalized === "utf8") {
    return "utf-8";
  }
  if (
    normalized === "shiftjis" ||
    normalized === "sjis" ||
    normalized === "cp932" ||
    normalized === "windows31j" ||
    normalized === "windows932" ||
    normalized === "mskanji"
  ) {
    return "shift_jis";
  }
  if (normalized === "eucjp") {
    return "euc-jp";
  }
  if (normalized === "iso2022jp") {
    return "iso-2022-jp";
  }
  return undefined;
}

function isValidUtf8(buffer: Uint8Array): boolean {
  try {
    new TextDecoder("utf-8", { fatal: true }).decode(buffer);
    return true;
  } catch {
    return false;
  }
}

async function yieldToEventLoop(): Promise<void> {
  await new Promise<void>((resolve) => setImmediate(resolve));
}

async function delay(ms: number): Promise<void> {
  await new Promise<void>((resolve) => setTimeout(resolve, ms));
}

function indexWorkspaceRoot(root: string): void {
  const stat = fs.statSync(root, { throwIfNoEntry: false });
  if (!stat?.isDirectory()) {
    return;
  }
  const visit = (dir: string): void => {
    for (const entry of readDirectoryEntries(dir)) {
      const fullPath = path.join(dir, entry.name);
      if (entry.isDirectory()) {
        if (!isExcludedWorkspaceDirectory(entry.name, fullPath)) {
          visit(fullPath);
        }
        continue;
      }
      if (entry.isFile() && isAspWorkspaceFile(entry.name)) {
        indexWorkspaceFile(fullPath);
      }
    }
  };
  visit(root);
}

function indexWorkspaceFile(fileName: string): void {
  const normalized = normalizeFileName(fileName);
  const stat = fs.statSync(normalized, { throwIfNoEntry: false });
  if (!stat?.isFile()) {
    workspaceIndex.delete(normalized);
    return;
  }
  const existing = workspaceIndex.get(normalized);
  if (existing && existing.mtimeMs === stat.mtimeMs && existing.size === stat.size) {
    return;
  }
  const uri = pathToFileUri(normalized);
  workspaceIndex.set(normalized, {
    uri,
    fileName: normalized,
    mtimeMs: stat.mtimeMs,
    size: stat.size,
  });
}

function invalidateWorkspaceIndex(reason = "workspaceIndex.invalidate"): void {
  workspaceGeneration += 1;
  workspaceIndexDirty = true;
  workspaceIndexTruncated = false;
  workspaceIndex.clear();
  logInvalidation("workspaceIndex", reason, workspaceGeneration);
}

function isExcludedWorkspaceDirectory(name: string, fullPath: string): boolean {
  const normalized = fullPath.split(path.sep).join("/");
  return (
    [".git", "node_modules", "dist", "out"].includes(name) ||
    normalized.endsWith("/server/language-server/node_modules")
  );
}

function isAspWorkspaceFile(fileName: string): boolean {
  return /\.(?:asp|asa|inc)$/i.test(fileName);
}

function isScriptWorkspaceFile(fileName: string): boolean {
  return /\.(?:[cm]?js|jsx|[cm]?ts|tsx|d\.ts)$/i.test(fileName);
}

function isJavaScriptProjectEnvironmentFile(fileName: string): boolean {
  const normalized = normalizeFileName(fileName).split(path.sep).join("/");
  return (
    /\/(?:tsconfig|jsconfig|package)\.json$/i.test(normalized) ||
    normalized.includes("/node_modules/@types/")
  );
}

function nearestPackageJson(directory: string): string | undefined {
  let current = normalizeFileName(directory);
  for (;;) {
    const candidate = path.join(current, "package.json");
    if (ts.sys.fileExists(candidate)) {
      return candidate;
    }
    const parent = path.dirname(current);
    if (parent === current) {
      return undefined;
    }
    current = parent;
  }
}

function findRegionAt(parsed: AspParsedDocument, offset: number): AspRegion | undefined {
  const regions = regionIndexFor(parsed).byStart;
  let low = 0;
  let high = regions.length - 1;
  let lastStartBeforeOffset = -1;
  while (low <= high) {
    const middle = Math.floor((low + high) / 2);
    if (regions[middle].contentStart <= offset) {
      lastStartBeforeOffset = middle;
      low = middle + 1;
    } else {
      high = middle - 1;
    }
  }
  let best: AspRegion | undefined;
  for (let index = lastStartBeforeOffset; index >= 0; index -= 1) {
    const region = regions[index];
    if (region.contentEnd <= offset) {
      continue;
    }
    if (!best || region.contentEnd - region.contentStart < best.contentEnd - best.contentStart) {
      best = region;
    }
  }
  const boundaryRegion = regions.find(
    (region) =>
      (region.kind === "style-attribute" ||
        region.kind === "style" ||
        region.kind === "client-script" ||
        region.kind === "server-script") &&
      region.contentEnd === offset,
  );
  if (boundaryRegion && (!best || best.kind === "html")) {
    return boundaryRegion;
  }
  return best ?? (offset > 0 ? findRegionAt(parsed, offset - 1) : undefined);
}

function regionIndexFor(parsed: AspParsedDocument): RegionIndex {
  const cached = regionIndexes.get(parsed);
  if (cached) {
    return cached;
  }
  const index = {
    byStart: [...parsed.regions].sort(
      (left, right) =>
        left.contentStart - right.contentStart ||
        left.contentEnd - left.contentStart - (right.contentEnd - right.contentStart),
    ),
  };
  regionIndexes.set(parsed, index);
  return index;
}

function cachedSettings(uri: string): AspSettings {
  const existing = settingsByUri.get(uri);
  if (existing) {
    return existing;
  }
  const settings = settingsForUri(uri, globalSettings);
  settingsByUri.set(uri, settings);
  return settings;
}

function settingsForUri(uri: string, baseSettings: AspSettings): AspSettings {
  const settings: AspSettings = {
    ...baseSettings,
    virtualRoot:
      baseSettings.virtualRoot || baseSettings.virtualRoots?.[0] || workspaceRootFromUri(uri),
    virtualRoots:
      baseSettings.virtualRoots && baseSettings.virtualRoots.length > 0
        ? baseSettings.virtualRoots
        : [workspaceRootFromUri(uri), ...workspaceRoots],
  };
  return settings;
}

function currentOpenDocumentSettingsByUri(): Map<string, AspSettings> {
  return new Map(
    documents.all().map((document) => [document.uri, settingsForUri(document.uri, globalSettings)]),
  );
}

function parseSettingsIdentity(settings: AspSettings): string {
  return JSON.stringify({
    defaultLanguage: settings.defaultLanguage ?? "VBScript",
    resolvedLocale: settings.resolvedLocale ?? "en",
  });
}

function includeResolutionIdentity(settings: AspSettings): string {
  return JSON.stringify({
    generation: includeResolutionGeneration,
    settings: includeResolutionSettingsKey(settings),
  });
}

function includeResolutionSettingsIdentity(settings: AspSettings): string {
  return JSON.stringify(includeResolutionSettingsKey(settings));
}

function diagnosticsIdentity(settings: AspSettings): string {
  return JSON.stringify({
    parse: parseSettingsIdentity(settings),
    includeResolution: includeResolutionSettingsIdentity(settings),
    checkJs: settings.checkJs === true,
    javascript: {
      unusedDiagnostics: settings.javascript?.unusedDiagnostics !== false,
    },
    vbscript: {
      typeChecking: settings.vbscript?.typeChecking,
      identifierCase: settings.vbscript?.identifierCase,
      identifierCaseByKind: settings.vbscript?.identifierCaseByKind,
      comTypes: settings.vbscript?.comTypes,
      globals: settings.vbscript?.globals,
      unusedDiagnostics: settings.vbscript?.unusedDiagnostics !== false,
    },
    locale: settings.resolvedLocale ?? "en",
  });
}

function jsProjectIdentity(settings: AspSettings): string {
  return JSON.stringify({
    generation: jsProjectGeneration,
    settings: jsProjectSettingsIdentity(settings),
  });
}

function jsProjectSettingsIdentity(settings: AspSettings): string {
  return JSON.stringify({
    checkJs: settings.checkJs === true,
    javascript: {
      autoImports: settings.javascript?.autoImports !== false,
      unusedDiagnostics: settings.javascript?.unusedDiagnostics !== false,
      ignoreProjectConfig: settings.javascript?.ignoreProjectConfig === true,
    },
    roots: workspaceRoots.map(normalizeFileName).sort(),
  });
}

function workspaceIndexSettingsIdentity(settings: AspSettings): string {
  return JSON.stringify({
    roots: workspaceRoots.map(normalizeFileName).sort(),
    maxIndexFiles: settings.workspace?.maxIndexFiles ?? defaultMaxIndexFiles,
    scanChunkSize: settings.workspace?.scanChunkSize ?? defaultScanChunkSize,
  });
}

function settingsInvalidationImpact(
  previousSettingsByUri: Map<string, AspSettings>,
): Map<string, SettingsInvalidationImpact> {
  const impact = new Map<string, SettingsInvalidationImpact>();
  for (const document of documents.all()) {
    const previous = previousSettingsByUri.get(document.uri) ?? settingsForUri(document.uri, {});
    const next = cachedSettings(document.uri);
    const parse = parseSettingsIdentity(previous) !== parseSettingsIdentity(next);
    const includeResolution =
      includeResolutionSettingsIdentity(previous) !== includeResolutionSettingsIdentity(next);
    const jsProject = jsProjectSettingsIdentity(previous) !== jsProjectSettingsIdentity(next);
    const diagnostics = diagnosticsIdentity(previous) !== diagnosticsIdentity(next);
    const workspaceIndex =
      workspaceIndexSettingsIdentity(previous) !== workspaceIndexSettingsIdentity(next);
    impact.set(document.uri, {
      parse,
      includeResolution,
      jsProject,
      diagnostics,
      workspaceIndex,
    });
  }
  return impact;
}

function applySettingsInvalidation(impact: Map<string, SettingsInvalidationImpact>): void {
  const impacts = [...impact.values()];
  if (impacts.some((item) => item.workspaceIndex)) {
    invalidateWorkspaceIndex("settings.workspaceIndex");
  }
  if (impacts.some((item) => item.includeResolution)) {
    invalidateIncludeResolution("settings.includeResolution");
  }
  if (impacts.some((item) => item.jsProject)) {
    invalidateJsProject("settings.jsProject");
  }
}

function applyDocumentSettingsInvalidation(
  uri: string,
  impact: SettingsInvalidationImpact | undefined,
): void {
  const cached = cache.get(uri);
  if (!impact) {
    return;
  }
  if (impact.parse) {
    cache.delete(uri);
    vbProjectContextCache.clear();
    clearSemanticTokensForUri(uri);
    logInvalidation("parseCache", `settings.parse, uri=${uri}`);
    return;
  }
  if (cached) {
    updateCachedDocumentRuntimeIdentity(cached, cachedSettings(uri));
  }
  if (impact.includeResolution || impact.jsProject || impact.diagnostics) {
    invalidateCachedAnalysisForUris(new Set([uri]), "settings.analysis");
  }
}

function shouldValidateAfterSettingsChange(
  impact: SettingsInvalidationImpact | undefined,
): boolean {
  return Boolean(
    impact && (impact.parse || impact.includeResolution || impact.jsProject || impact.diagnostics),
  );
}

function localizerForSettings(settings: AspSettings) {
  return createLocalizer(settings.resolvedLocale);
}

function localizerForUri(uri: string) {
  return localizerForSettings(cachedSettings(uri));
}

async function refreshConfiguration(): Promise<void> {
  try {
    const previousSettingsByUri = currentOpenDocumentSettingsByUri();
    globalSettings = normalizeSettings(
      (await connection.workspace.getConfiguration("aspLsp")) as Record<string, unknown>,
    );
    configureDiskAnalysisCache();
    settingsByUri.clear();
    const impact = settingsInvalidationImpact(previousSettingsByUri);
    applySettingsInvalidation(impact);
    for (const document of documents.all()) {
      applyDocumentSettingsInvalidation(document.uri, impact.get(document.uri));
      if (shouldValidateAfterSettingsChange(impact.get(document.uri))) {
        await validate(document);
      }
    }
  } catch {
    globalSettings = normalizeSettings(globalSettings);
    configureDiskAnalysisCache();
  }
  scheduleBackgroundAnalysis("configuration.refresh");
}

function readSettingsFromChange(settings: unknown): Record<string, unknown> | undefined {
  if (!settings || typeof settings !== "object") {
    return undefined;
  }
  const record = settings as Record<string, unknown>;
  const nested = record.aspLsp;
  return nested && typeof nested === "object" ? (nested as Record<string, unknown>) : record;
}

function normalizeSettings(settings: Record<string, unknown> | AspSettings): AspSettings {
  const rawVirtualRoots = Array.isArray(settings.virtualRoots)
    ? settings.virtualRoots
    : Array.isArray(settings.includePaths)
      ? settings.includePaths
      : undefined;
  const virtualRoots = rawVirtualRoots
    ?.filter((value): value is string => typeof value === "string" && value.length > 0)
    .map((value) => path.resolve(value));
  const includePaths = Array.isArray(settings.includePaths)
    ? settings.includePaths
        .filter((value): value is string => typeof value === "string" && value.length > 0)
        .map((value) => path.resolve(value))
    : undefined;
  return {
    locale: normalizeLocaleSetting(settings.locale),
    resolvedLocale: resolveLocale(normalizeLocaleSetting(settings.locale)),
    defaultLanguage: settings.defaultLanguage === "JScript" ? "JScript" : "VBScript",
    checkJs: settings.checkJs === true,
    windowsPathResolution: settings.windowsPathResolution !== false,
    virtualRoot:
      typeof settings.virtualRoot === "string" && settings.virtualRoot.length > 0
        ? path.resolve(settings.virtualRoot)
        : undefined,
    virtualRoots,
    includePaths,
    legacyEncoding: normalizeLegacyEncoding(settings.legacyEncoding),
    diagnostics: normalizeDiagnosticsSettings(settings),
    debug: normalizeDebugSettings(settings),
    format: normalizeFormatSettings(settings),
    javascript: normalizeJavascriptSettings(settings),
    vbscript: normalizeVbscriptSettings(settings),
    inlayHints: normalizeInlayHintSettings(settings),
    codeLens: normalizeCodeLensSettings(settings),
    cache: normalizeCacheSettings(settings),
    workspace: normalizeWorkspaceSettings(settings),
  };
}

function normalizeLocaleSetting(value: unknown): AspLocaleSetting {
  return value === "en" || value === "ja" || value === "auto" ? value : "auto";
}

function resolveLocale(setting: AspLocaleSetting): AspLocale {
  if (setting === "en" || setting === "ja") {
    return setting;
  }
  return clientLocale.toLowerCase().startsWith("ja") ? "ja" : "en";
}

function normalizeWorkspaceSettings(
  settings: Record<string, unknown> | AspSettings,
): AspSettings["workspace"] {
  const raw = settings.workspace;
  const record = raw && typeof raw === "object" ? (raw as Record<string, unknown>) : {};
  return {
    maxIndexFiles:
      typeof record.maxIndexFiles === "number" && record.maxIndexFiles > 0
        ? Math.floor(record.maxIndexFiles)
        : defaultMaxIndexFiles,
    scanChunkSize:
      typeof record.scanChunkSize === "number" && record.scanChunkSize > 0
        ? Math.floor(record.scanChunkSize)
        : defaultScanChunkSize,
    backgroundAnalysis: record.backgroundAnalysis === true,
    idleAnalysisConcurrency: clampAnalysisConcurrency(
      typeof record.idleAnalysisConcurrency === "number"
        ? record.idleAnalysisConcurrency
        : undefined,
      defaultIdleAnalysisConcurrency(),
    ),
    busyAnalysisConcurrency: clampAnalysisConcurrency(
      typeof record.busyAnalysisConcurrency === "number"
        ? record.busyAnalysisConcurrency
        : undefined,
      defaultBusyAnalysisConcurrency(),
    ),
  };
}

function normalizeCacheSettings(
  settings: Record<string, unknown> | AspSettings,
): AspSettings["cache"] {
  const raw = settings.cache;
  const record = raw && typeof raw === "object" ? (raw as Record<string, unknown>) : {};
  return {
    enabled: record.enabled !== false,
    directory: typeof record.directory === "string" ? record.directory : undefined,
    ttlHours:
      typeof record.ttlHours === "number" && record.ttlHours > 0
        ? Math.floor(record.ttlHours)
        : 24 * 14,
    maxSizeMb:
      typeof record.maxSizeMb === "number" && record.maxSizeMb > 0
        ? Math.floor(record.maxSizeMb)
        : 128,
  };
}

function normalizeLegacyEncoding(value: unknown): AspLegacyEncoding {
  return value === "auto" || value === "utf8" || value === "shift_jis" || value === "cp932"
    ? value
    : "auto";
}

function normalizeDiagnosticsSettings(
  settings: Record<string, unknown> | AspSettings,
): AspSettings["diagnostics"] {
  const raw = settings.diagnostics;
  const record = raw && typeof raw === "object" ? (raw as Record<string, unknown>) : {};
  return {
    debounceMs:
      typeof record.debounceMs === "number" && record.debounceMs >= 0
        ? Math.floor(record.debounceMs)
        : defaultDiagnosticsDebounceMs,
  };
}

function normalizeDebugSettings(
  settings: Record<string, unknown> | AspSettings,
): AspSettings["debug"] {
  const raw = settings.debug;
  const record = raw && typeof raw === "object" ? (raw as Record<string, unknown>) : {};
  return {
    output: normalizeDebugOutputLevel(record.output),
  };
}

function normalizeDebugOutputLevel(value: unknown): NonNullable<AspSettings["debug"]>["output"] {
  return value === "summary" || value === "verbose" ? value : "off";
}

function normalizeInlayHintSettings(
  settings: Record<string, unknown> | AspSettings,
): AspSettings["inlayHints"] {
  const raw = settings.inlayHints;
  const record = raw && typeof raw === "object" ? (raw as Record<string, unknown>) : {};
  return {
    variableTypes: record.variableTypes !== false,
    parameterNames: record.parameterNames !== false,
    functionReturnTypes: record.functionReturnTypes !== false,
    implicitByRef: record.implicitByRef !== false,
    globalVariableMarkers: record.globalVariableMarkers !== false,
  };
}

function normalizeCodeLensSettings(
  settings: Record<string, unknown> | AspSettings,
): AspSettings["codeLens"] {
  const raw = settings.codeLens;
  const record = raw && typeof raw === "object" ? (raw as Record<string, unknown>) : {};
  return {
    references: record.references !== false,
    includes: record.includes === true,
  };
}

function normalizeVbscriptSettings(
  settings: Record<string, unknown> | AspSettings,
): AspSettings["vbscript"] {
  const raw = settings.vbscript;
  const record = raw && typeof raw === "object" ? (raw as Record<string, unknown>) : {};
  return {
    typeChecking: record.typeChecking === "strict" ? "strict" : "basic",
    identifierCase: normalizeVbscriptIdentifierCase(record.identifierCase),
    identifierCaseByKind: normalizeVbscriptIdentifierCaseByKind(record.identifierCaseByKind),
    comTypes:
      record.comTypes && typeof record.comTypes === "object"
        ? (record.comTypes as NonNullable<AspSettings["vbscript"]>["comTypes"])
        : undefined,
    globals:
      record.globals && typeof record.globals === "object"
        ? (record.globals as NonNullable<AspSettings["vbscript"]>["globals"])
        : undefined,
    unusedDiagnostics: record.unusedDiagnostics !== false,
    includeSuggestions: record.includeSuggestions !== false,
    syntaxSnippets: record.syntaxSnippets !== false,
  };
}

function normalizeVbscriptIdentifierCase(
  value: unknown,
): NonNullable<NonNullable<AspSettings["vbscript"]>["identifierCase"]> | undefined {
  switch (value) {
    case "PascalCase":
    case "UPPERCASE":
    case "camelCase":
    case "lowercase":
    case "snake_case":
    case "UPPER_SNAKE":
    case "ignore":
      return value;
    case "pascal":
      return "PascalCase";
    case "upper":
      return "UPPERCASE";
    case "camel":
      return "camelCase";
    case "lower":
      return "lowercase";
    case "snake":
      return "snake_case";
    case "upperSnake":
      return "UPPER_SNAKE";
    default:
      return undefined;
  }
}

function normalizeVbscriptIdentifierCaseByKind(
  value: unknown,
): NonNullable<NonNullable<AspSettings["vbscript"]>["identifierCaseByKind"]> | undefined {
  if (!value || typeof value !== "object") {
    return undefined;
  }
  const record = value as Record<string, unknown>;
  const result: NonNullable<NonNullable<AspSettings["vbscript"]>["identifierCaseByKind"]> = {};
  for (const key of [
    "variable",
    "parameter",
    "class",
    "function",
    "sub",
    "constant",
    "field",
    "property",
    "method",
  ] as const) {
    const normalized = normalizeVbscriptIdentifierCase(record[key]);
    if (normalized) {
      result[key] = normalized;
    }
  }
  return Object.keys(result).length > 0 ? result : undefined;
}

function normalizeJavascriptSettings(
  settings: Record<string, unknown> | AspSettings,
): AspSettings["javascript"] {
  const raw = settings.javascript;
  const record = raw && typeof raw === "object" ? (raw as Record<string, unknown>) : {};
  return {
    unusedDiagnostics: record.unusedDiagnostics !== false,
    autoImports: record.autoImports !== false,
    ignoreProjectConfig: record.ignoreProjectConfig === true,
  };
}

function normalizeFormatSettings(
  settings: Record<string, unknown> | AspSettings,
): AspSettings["format"] {
  const raw = settings.format;
  const record = raw && typeof raw === "object" ? (raw as Record<string, unknown>) : {};
  const indentStyle =
    record.indentStyle === "tab" || record.indentStyle === "space" ? record.indentStyle : undefined;
  return {
    indentSize:
      typeof record.indentSize === "number" && record.indentSize > 0
        ? record.indentSize
        : undefined,
    indentStyle,
    uppercaseKeywords: record.uppercaseKeywords === true,
    alignAssignments: record.alignAssignments === true,
    onSave: record.onSave === true,
    ignoreVbscriptTagIndent: record.ignoreVbscriptTagIndent === true,
    ignoreCssTagIndent: record.ignoreCssTagIndent === true,
    ignoreJavaScriptTagIndent: record.ignoreJavaScriptTagIndent === true,
  };
}

function formatOptions(options: { tabSize: number; insertSpaces: boolean }, settings: AspSettings) {
  return {
    tabSize: options.tabSize,
    insertSpaces: options.insertSpaces,
    ...settings.format,
  };
}

function defaultFormattingOptions(settings: AspSettings): {
  tabSize: number;
  insertSpaces: boolean;
} {
  return {
    tabSize: settings.format?.indentSize ?? 2,
    insertSpaces: settings.format?.indentStyle !== "tab",
  };
}

function formatAspDocumentWithDelegates(
  cached: CachedDocument,
  options: { tabSize: number; insertSpaces: boolean },
): TextEdit[] {
  const settings = cachedSettings(cached.source.uri);
  const startedAt = startFormattingLog(cached, settings, "document");
  const formattingOptions = measureDebugStep(settings, cached.source.uri, "format.options", () =>
    formatOptions(options, settings),
  );
  const original = cached.source.getText();
  let formatted = measureDebugStep(settings, cached.source.uri, "format.core", () =>
    applyTextEdits(original, formatAspDocument(cached.parsed, formattingOptions)),
  );
  const parsed = measureDebugStep(settings, cached.source.uri, "format.reparse", () =>
    parseAspDocument(cached.source.uri, formatted, settings),
  );
  formatted = applyOffsetEdits(
    formatted,
    embeddedFormattingEdits(parsed, formatted, formattingOptions, settings, cached.source.uri),
  );
  const edits = measureDebugStep(settings, cached.source.uri, "format.editAssembly", () =>
    formatted === original
      ? []
      : [
          {
            range: {
              start: cached.source.positionAt(0),
              end: cached.source.positionAt(original.length),
            },
            newText: formatted,
          },
        ],
  );
  finishFormattingLog(cached, settings, "document", startedAt, edits.length);
  return edits;
}

function formatAspRangeWithDelegates(
  cached: CachedDocument,
  range: Range,
  options: { tabSize: number; insertSpaces: boolean },
): TextEdit[] {
  const settings = cachedSettings(cached.source.uri);
  const startedAt = startFormattingLog(cached, settings, "range");
  const formattingOptions = measureDebugStep(settings, cached.source.uri, "format.options", () =>
    formatOptions(options, settings),
  );
  const original = cached.source.getText();
  const rangeStart = lineStartOffset(original, cached.source.offsetAt(range.start));
  const rangeEnd = lineEndOffset(original, cached.source.offsetAt(range.end));
  const coreEdits = measureDebugStep(settings, cached.source.uri, "format.core", () =>
    formatAspRange(cached.parsed, range, formattingOptions),
  );
  let formatted = measureDebugStep(settings, cached.source.uri, "format.core.apply", () =>
    applyTextEdits(original, coreEdits),
  );
  let formattedRangeEnd =
    coreEdits.length === 1
      ? rangeStart + coreEdits[0].newText.length
      : rangeEnd + offsetEditsDelta(original, coreEdits);
  const parsed = measureDebugStep(settings, cached.source.uri, "format.reparse", () =>
    parseAspDocument(cached.source.uri, formatted, settings),
  );
  const embeddedEdits = embeddedFormattingEdits(
    parsed,
    formatted,
    formattingOptions,
    settings,
    cached.source.uri,
    rangeStart,
    formattedRangeEnd,
  );
  formattedRangeEnd += offsetEditsDelta(formatted, embeddedEdits);
  formatted = applyOffsetEdits(formatted, embeddedEdits);
  const newText = formatted.slice(rangeStart, formattedRangeEnd);
  const originalText = original.slice(rangeStart, rangeEnd);
  const edits = measureDebugStep(settings, cached.source.uri, "format.editAssembly", () =>
    newText === originalText
      ? []
      : [
          {
            range: {
              start: cached.source.positionAt(rangeStart),
              end: cached.source.positionAt(rangeEnd),
            },
            newText,
          },
        ],
  );
  finishFormattingLog(cached, settings, "range", startedAt, edits.length);
  return edits;
}

function startFormattingLog(
  cached: CachedDocument,
  settings: AspSettings,
  scope: "document" | "range",
): bigint {
  logDebugSummary(
    settings,
    `[asp-lsp] Formatting conversion started (${scope}): ${cached.source.uri}`,
  );
  return process.hrtime.bigint();
}

function finishFormattingLog(
  cached: CachedDocument,
  settings: AspSettings,
  scope: "document" | "range",
  startedAt: bigint,
  editCount: number,
): void {
  const elapsedMs = Number(process.hrtime.bigint() - startedAt) / 1_000_000;
  logDebugSummary(
    settings,
    `[asp-lsp] Formatting conversion completed (${scope}): ${cached.source.uri} ${formatElapsedMs(elapsedMs)}, edits=${editCount}`,
  );
}

function embeddedFormattingEdits(
  parsed: AspParsedDocument,
  text: string,
  options: AspFormattingOptions,
  settings: AspSettings,
  uri: string,
  spanStart = 0,
  spanEnd = text.length,
): OffsetEdit[] {
  const startedAt = process.hrtime.bigint();
  const edits = [
    ...measureDebugStep(settings, uri, "format.embedded.css", () =>
      cssFormattingEdits(parsed, text, options, spanStart, spanEnd),
    ),
    ...measureDebugStep(settings, uri, "format.embedded.javascript", () =>
      javaScriptFormattingEdits(parsed, text, options, spanStart, spanEnd),
    ),
  ];
  finishDebugStep(settings, uri, "format.embedded", startedAt);
  return edits;
}

function cssFormattingEdits(
  parsed: AspParsedDocument,
  text: string,
  options: AspFormattingOptions,
  spanStart: number,
  spanEnd: number,
): OffsetEdit[] {
  if (
    !parsed.regions.some(
      (region) =>
        region.language === "css" && region.contentEnd > spanStart && region.contentStart < spanEnd,
    )
  ) {
    return [];
  }
  return parsed.regions
    .filter(
      (region) =>
        region.language === "css" && region.contentEnd > spanStart && region.contentStart < spanEnd,
    )
    .filter((region) => !regionHasNestedAsp(parsed, region))
    .flatMap((region) => formatCssRegion(text, region, options, spanStart, spanEnd));
}

function formatCssRegion(
  text: string,
  region: AspRegion,
  options: AspFormattingOptions,
  spanStart: number,
  spanEnd: number,
): OffsetEdit[] {
  const content = text.slice(region.contentStart, region.contentEnd);
  const doc = TextDocument.create("__asp_lsp_format.css", "css", 0, content);
  const localStart = Math.max(0, spanStart - region.contentStart);
  const localEnd = Math.min(content.length, spanEnd - region.contentStart);
  if (localStart >= localEnd) {
    return [];
  }
  const edits = cssService.format(
    doc,
    { start: doc.positionAt(localStart), end: doc.positionAt(localEnd) },
    {
      tabSize: options.indentSize ?? options.tabSize,
      insertSpaces: (options.indentStyle ?? (options.insertSpaces ? "space" : "tab")) !== "tab",
    },
  );
  const offsetEdits = edits.map((edit) => ({
    start: offsetAtText(content, edit.range.start),
    end: offsetAtText(content, edit.range.end),
    newText: edit.newText,
  }));
  if (region.kind === "style" && localStart === 0 && localEnd === content.length) {
    return [
      {
        start: region.contentStart,
        end: region.contentEnd,
        newText: wrapEmbeddedElementContent(
          text,
          region,
          options,
          applyOffsetEdits(content, offsetEdits),
          options.ignoreCssTagIndent === true,
          false,
        ),
      },
    ];
  }
  return offsetEdits.map((edit) => ({
    start: region.contentStart + edit.start,
    end: region.contentStart + edit.end,
    newText: edit.newText,
  }));
}

function javaScriptFormattingEdits(
  parsed: AspParsedDocument,
  text: string,
  options: AspFormattingOptions,
  spanStart = 0,
  spanEnd = text.length,
): OffsetEdit[] {
  return parsed.regions
    .filter(
      (region) =>
        isJavaScriptLikeRegion(region) &&
        region.contentEnd > spanStart &&
        region.contentStart < spanEnd,
    )
    .filter((region) => !regionHasNestedAsp(parsed, region))
    .flatMap((region) => formatJavaScriptRegion(text, region, options, spanStart, spanEnd));
}

function regionHasNestedAsp(parsed: AspParsedDocument, owner: AspRegion): boolean {
  return parsed.regions.some(
    (region) =>
      region !== owner &&
      (region.kind === "asp-block" ||
        region.kind === "asp-expression" ||
        region.kind === "asp-directive") &&
      region.start >= owner.contentStart &&
      region.end <= owner.contentEnd,
  );
}

function formatJavaScriptRegion(
  text: string,
  region: AspRegion,
  options: AspFormattingOptions,
  spanStart: number,
  spanEnd: number,
): OffsetEdit[] {
  const content = text.slice(region.contentStart, region.contentEnd);
  const localStart = Math.max(0, spanStart - region.contentStart);
  const localEnd = Math.min(content.length, spanEnd - region.contentStart);
  if (localStart >= localEnd) {
    return [];
  }
  const formatOptions = tsFormatOptions(
    options,
    embeddedBodyBaseIndentSize(text, region, options, options.ignoreJavaScriptTagIndent === true),
  );
  if (localStart === 0 && localEnd === content.length) {
    const trimmedContent = content.trim();
    if (trimmedContent.length === 0) {
      return [];
    }
    const changes = getJavaScriptFormattingService(trimmedContent).getFormattingEditsForDocument(
      "__asp_lsp_format.js",
      formatOptions,
    );
    const formatted = applyOffsetEdits(
      trimmedContent,
      changes.map((change) => ({
        start: change.span.start,
        end: change.span.start + change.span.length,
        newText: change.newText,
      })),
    );
    return [
      {
        start: region.contentStart,
        end: region.contentEnd,
        newText: wrapEmbeddedElementContent(
          text,
          region,
          options,
          formatted,
          options.ignoreJavaScriptTagIndent === true,
          true,
        ),
      },
    ];
  }
  const changes = getJavaScriptFormattingService(content).getFormattingEditsForRange(
    "__asp_lsp_format.js",
    localStart,
    localEnd,
    formatOptions,
  );
  return changes.map((change) => ({
    start: region.contentStart + change.span.start,
    end: region.contentStart + change.span.start + change.span.length,
    newText: change.newText,
  }));
}

function getJavaScriptFormattingService(text: string): ts.LanguageService {
  const fileName = "__asp_lsp_format.js";
  return ts.createLanguageService({
    getScriptFileNames: () => [fileName],
    getScriptVersion: () => "0",
    getScriptSnapshot: (requested) =>
      requested === fileName ? ts.ScriptSnapshot.fromString(text) : undefined,
    getCurrentDirectory: () => process.cwd(),
    getCompilationSettings: () => ({
      allowJs: true,
      checkJs: false,
      target: ts.ScriptTarget.ESNext,
      module: ts.ModuleKind.CommonJS,
    }),
    getDefaultLibFileName: (compilerOptions) => ts.getDefaultLibFilePath(compilerOptions),
    fileExists: ts.sys.fileExists,
    readFile: ts.sys.readFile,
    readDirectory: ts.sys.readDirectory,
  });
}

function tsFormatOptions(
  options: AspFormattingOptions,
  baseIndentSize?: number,
): ts.FormatCodeSettings {
  const indentStyle = options.indentStyle ?? (options.insertSpaces ? "space" : "tab");
  return {
    baseIndentSize,
    indentSize: options.indentSize ?? options.tabSize,
    tabSize: options.tabSize,
    convertTabsToSpaces: indentStyle !== "tab",
    newLineCharacter: "\n",
  };
}

function isJavaScriptLikeRegion(region: AspRegion): boolean {
  return region.language === "javascript" || region.language === "jscript";
}

function wrapEmbeddedElementContent(
  text: string,
  region: AspRegion,
  options: AspFormattingOptions,
  content: string,
  ignoreTagIndent: boolean,
  contentAlreadyIndented: boolean,
): string {
  const trimmed = contentAlreadyIndented ? trimOuterBlankLines(content) : content.trim();
  if (trimmed.length === 0) {
    return "";
  }
  const tagLevel = ignoreTagIndent ? 0 : leadingIndentLevel(text, region.start, options);
  const tagIndent = indentUnit(options).repeat(tagLevel);
  const body =
    contentAlreadyIndented || ignoreTagIndent
      ? trimmed
      : indentLines(trimmed, indentUnit(options).repeat(tagLevel + 1));
  return `\n${body}\n${tagIndent}`;
}

function trimOuterBlankLines(text: string): string {
  return text
    .replace(/^\s*\r?\n/, "")
    .replace(/\r?\n\s*$/, "")
    .trimEnd();
}

function embeddedBodyBaseIndentSize(
  text: string,
  region: AspRegion,
  options: AspFormattingOptions,
  ignoreTagIndent: boolean,
): number {
  if (ignoreTagIndent) {
    return 0;
  }
  return leadingIndentWidth(text, region.start, options) + (options.indentSize ?? options.tabSize);
}

function indentLines(text: string, indent: string): string {
  return text
    .split("\n")
    .map((line) => (line.length === 0 ? "" : `${indent}${line}`))
    .join("\n");
}

function leadingIndentLevel(text: string, offset: number, options: AspFormattingOptions): number {
  return Math.floor(
    leadingIndentWidth(text, offset, options) / (options.indentSize ?? options.tabSize),
  );
}

function leadingIndentWidth(text: string, offset: number, options: AspFormattingOptions): number {
  const lineStart = lineStartOffset(text, offset);
  const indent = text.slice(lineStart, offset).match(/^[\t ]*/)?.[0] ?? "";
  return [...indent].reduce((width, char) => width + (char === "\t" ? options.tabSize : 1), 0);
}

function indentUnit(options: AspFormattingOptions): string {
  const style = options.indentStyle ?? (options.insertSpaces ? "space" : "tab");
  return style === "tab" ? "\t" : " ".repeat(options.indentSize ?? options.tabSize);
}

function applyTextEdits(text: string, edits: TextEdit[]): string {
  return applyOffsetEdits(
    text,
    edits.map((edit) => ({
      start: offsetAtText(text, edit.range.start),
      end: offsetAtText(text, edit.range.end),
      newText: edit.newText,
    })),
  );
}

function applyOffsetEdits(text: string, edits: OffsetEdit[]): string {
  return [...edits]
    .sort((left, right) => right.start - left.start || right.end - left.end)
    .reduce(
      (current, edit) => `${current.slice(0, edit.start)}${edit.newText}${current.slice(edit.end)}`,
      text,
    );
}

function offsetEditsDelta(text: string, edits: TextEdit[] | OffsetEdit[]): number {
  return edits.reduce((delta, edit) => {
    if ("range" in edit) {
      return (
        delta +
        edit.newText.length -
        (offsetAtText(text, edit.range.end) - offsetAtText(text, edit.range.start))
      );
    }
    return delta + edit.newText.length - (edit.end - edit.start);
  }, 0);
}

function offsetAtText(text: string, position: Range["start"]): number {
  let line = 0;
  let character = 0;
  for (let index = 0; index < text.length; index += 1) {
    if (line === position.line && character === position.character) {
      return index;
    }
    if (text[index] === "\n") {
      line += 1;
      character = 0;
    } else {
      character += 1;
    }
  }
  return text.length;
}

function lineStartOffset(text: string, offset: number): number {
  return text.lastIndexOf("\n", Math.max(0, offset - 1)) + 1;
}

function lineEndOffset(text: string, offset: number): number {
  const end = text.indexOf("\n", offset);
  return end === -1 ? text.length : end;
}

function rangeOverlapsNonHtml(cached: CachedDocument, range: Range): boolean {
  const start = cached.source.offsetAt(range.start);
  const end = cached.source.offsetAt(range.end);
  return cached.parsed.regions.some(
    (region) => region.kind !== "html" && region.start < end && region.end > start,
  );
}

function createJsLanguageService(
  virtual: VirtualDocument,
  settings: AspSettings,
  optionOverrides: Partial<ts.CompilerOptions> = {},
): JsLanguageServiceProject {
  const cacheKey = jsLanguageServiceCacheKey(virtual, settings, optionOverrides);
  const collected = collectJsProjectFiles(virtual, settings, optionOverrides);
  const cached = jsLanguageServiceCache.get(cacheKey);
  if (cached) {
    updateJsLanguageServiceProject(cached.project, collected);
    cached.lastUsed = ++jsLanguageServiceCacheTick;
    logDebugSummary(
      settings,
      `[asp-lsp] javascript.languageService.reuse: ${virtualSourceUri(virtual)}, files=${cached.project.files.size}`,
    );
    return cached.project;
  }
  const files = new Map<string, JsProjectFile>();
  const moduleResolutionHost: ts.ModuleResolutionHost = {
    fileExists: (requested) =>
      files.has(normalizeFileName(requested)) || ts.sys.fileExists(requested),
    readFile: (requested) =>
      files.get(normalizeFileName(requested))?.text ?? ts.sys.readFile(requested),
    directoryExists: ts.sys.directoryExists,
    getDirectories: getTypeScriptDirectories,
    realpath: ts.sys.realpath,
  };
  const project = {
    service: undefined as unknown as ts.LanguageService,
    host: undefined as unknown as ts.LanguageServiceHost,
    files,
    options: collected.options,
    currentDirectory: collected.currentDirectory,
    moduleResolutionCache: ts.createModuleResolutionCache(
      collected.currentDirectory,
      normalizeFileName,
      collected.options,
    ),
    optionsKey: jsCompilerOptionsKey(collected.options),
    projectVersion: 0,
  };
  const host: ts.LanguageServiceHost = {
    getScriptFileNames: () => [...files.keys()],
    getProjectVersion: () => String(project.projectVersion),
    getScriptVersion: (requested) => files.get(normalizeFileName(requested))?.version ?? "0",
    getScriptSnapshot: (requested) => {
      const file = files.get(normalizeFileName(requested));
      if (file) {
        return file.snapshot ?? ts.ScriptSnapshot.fromString(file.text);
      }
      const text = ts.sys.readFile(requested);
      return text === undefined ? undefined : ts.ScriptSnapshot.fromString(text);
    },
    getScriptKind: (requested) => scriptKindForFileName(requested),
    getCurrentDirectory: () => project.currentDirectory,
    getCompilationSettings: () => project.options,
    getDefaultLibFileName: (options) => ts.getDefaultLibFilePath(options),
    fileExists: (requested) =>
      files.has(normalizeFileName(requested)) || ts.sys.fileExists(requested),
    readFile: (requested) =>
      files.get(normalizeFileName(requested))?.text ?? ts.sys.readFile(requested),
    readDirectory: readTypeScriptDirectory,
    directoryExists: ts.sys.directoryExists,
    getDirectories: getTypeScriptDirectories,
    realpath: ts.sys.realpath,
    useCaseSensitiveFileNames: () => ts.sys.useCaseSensitiveFileNames,
    resolveModuleNames: (
      moduleNames,
      containingFile,
      _reusedNames,
      _redirectedReference,
      options,
    ) =>
      moduleNames.map(
        (moduleName) =>
          ts.resolveModuleName(
            moduleName,
            containingFile,
            options,
            moduleResolutionHost,
            project.moduleResolutionCache,
          ).resolvedModule,
      ),
  };
  project.host = host;
  updateJsLanguageServiceProject(project, collected);
  project.service = ts.createLanguageService(host, jsDocumentRegistry);
  jsLanguageServiceCache.set(cacheKey, {
    project,
    lastUsed: ++jsLanguageServiceCacheTick,
  });
  logDebugSummary(
    settings,
    `[asp-lsp] javascript.languageService.create: ${virtualSourceUri(virtual)}, files=${project.files.size}`,
  );
  pruneJsLanguageServiceCache();
  return project;
}

function jsLanguageServiceCacheKey(
  virtual: VirtualDocument,
  settings: AspSettings,
  optionOverrides: Partial<ts.CompilerOptions>,
): string {
  return JSON.stringify({
    projectEnvironment: jsProjectEnvironmentFingerprint(virtualSourceUri(virtual), settings),
    projectGeneration: jsProjectGeneration,
    settings: {
      checkJs: settings.checkJs ?? false,
      autoImports: settings.javascript?.autoImports !== false,
      unusedDiagnostics: settings.javascript?.unusedDiagnostics !== false,
      ignoreProjectConfig: settings.javascript?.ignoreProjectConfig === true,
    },
    optionOverrides,
    roots: workspaceRoots.map(normalizeFileName).sort(),
  });
}

function updateJsLanguageServiceProject(
  project: Pick<
    JsLanguageServiceProject,
    | "files"
    | "options"
    | "currentDirectory"
    | "moduleResolutionCache"
    | "optionsKey"
    | "projectVersion"
  >,
  collected: JsProjectConfig & { files: Map<string, JsProjectFile> },
): void {
  const previousFiles = jsProjectFilesFingerprint(project.files);
  const nextFiles = jsProjectFilesFingerprint(collected.files);
  const nextOptionsKey = jsCompilerOptionsKey(collected.options);
  const resolutionShapeChanged =
    project.currentDirectory !== collected.currentDirectory ||
    project.optionsKey !== nextOptionsKey;
  project.files.clear();
  for (const [fileName, file] of collected.files) {
    project.files.set(fileName, file);
  }
  project.options = collected.options;
  project.currentDirectory = collected.currentDirectory;
  if (resolutionShapeChanged) {
    project.moduleResolutionCache = ts.createModuleResolutionCache(
      project.currentDirectory,
      normalizeFileName,
      project.options,
    );
    project.optionsKey = nextOptionsKey;
  }
  if (previousFiles !== nextFiles || resolutionShapeChanged) {
    project.projectVersion += 1;
  }
}

function jsProjectFilesFingerprint(files: Map<string, JsProjectFile>): string {
  return JSON.stringify(
    [...files.values()]
      .map((file) => ({
        fileName: file.fileName,
        version: file.version,
      }))
      .sort((left, right) => left.fileName.localeCompare(right.fileName)),
  );
}

function jsCompilerOptionsKey(options: ts.CompilerOptions): string {
  return JSON.stringify(options, (_key, value) =>
    typeof value === "function" ? undefined : value,
  );
}

function jsProjectEnvironmentFingerprint(ownerUri: string, settings: AspSettings): string {
  const ownerFile = uriToFileName(ownerUri);
  const ownerDirectory = path.dirname(ownerFile);
  const configPath =
    settings.javascript?.ignoreProjectConfig === true
      ? undefined
      : (ts.findConfigFile(ownerDirectory, ts.sys.fileExists, "tsconfig.json") ??
        ts.findConfigFile(ownerDirectory, ts.sys.fileExists, "jsconfig.json"));
  return [configPath, nearestPackageJson(ownerDirectory)]
    .filter((fileName): fileName is string => Boolean(fileName))
    .map((fileName) => {
      const stat = fs.statSync(fileName, { throwIfNoEntry: false });
      return stat ? `${normalizeFileName(fileName)}:${stat.mtimeMs}:${stat.size}` : fileName;
    })
    .join("|");
}

function pruneJsLanguageServiceCache(): void {
  while (jsLanguageServiceCache.size > 16) {
    const oldest = [...jsLanguageServiceCache.entries()].sort(
      (left, right) => left[1].lastUsed - right[1].lastUsed,
    )[0];
    if (!oldest) {
      return;
    }
    oldest[1].project.service.dispose();
    jsLanguageServiceCache.delete(oldest[0]);
  }
}

function clearJsLanguageServiceCache(): void {
  for (const entry of jsLanguageServiceCache.values()) {
    entry.project.service.dispose();
  }
  jsLanguageServiceCache.clear();
}

function clearJsProjectCaches(): void {
  clearJsLanguageServiceCache();
  jsProjectConfigCache.clear();
  jsScriptSnapshots.clear();
  completionSessionCache.clear("jsProject");
}

function clearIncludeCaches(): void {
  includePathResolutionCache.clear();
  pathResolutionCache.clear();
  includeCycleCache.clear();
  includeDocumentLoader.clear();
  clearIncludeGraph();
  completionSessionCache.clear("includeResolution");
}

function invalidateIncludeResolution(reason: string): void {
  includeResolutionGeneration += 1;
  clearIncludeCaches();
  logInvalidation("includeResolution", reason, includeResolutionGeneration);
}

function invalidateJsProject(reason: string): void {
  jsProjectGeneration += 1;
  clearJsProjectCaches();
  logInvalidation("jsProject", reason, jsProjectGeneration);
}

function logInvalidation(layer: string, reason: string, generation?: number): void {
  logDebugSummary(
    globalSettings,
    `[asp-lsp] invalidation.${layer}: ${reason}${
      generation === undefined ? "" : `, generation=${generation}`
    }`,
  );
}

function textFingerprint(text: string): string {
  let hash = 2166136261;
  for (let index = 0; index < text.length; index += 1) {
    hash ^= text.charCodeAt(index);
    hash = Math.imul(hash, 16777619);
  }
  return `${text.length}:${hash >>> 0}`;
}

class AspJsScriptSnapshot implements ts.IScriptSnapshot {
  previous?: AspJsScriptSnapshot;

  constructor(
    readonly fileName: string,
    private readonly text: string,
    readonly version: string,
    readonly sequence: number,
    readonly segments: readonly { virtualStart: number; virtualEnd: number }[],
    previous: AspJsScriptSnapshot | undefined,
    readonly changeFromPrevious: ts.TextChangeRange | undefined,
  ) {
    this.previous = previous;
  }

  getText(start: number, end: number): string {
    return this.text.slice(start, end);
  }

  getLength(): number {
    return this.text.length;
  }

  getChangeRange(oldSnapshot: ts.IScriptSnapshot): ts.TextChangeRange | undefined {
    if (!(oldSnapshot instanceof AspJsScriptSnapshot)) {
      return undefined;
    }
    if (oldSnapshot.fileName !== this.fileName || oldSnapshot.sequence >= this.sequence) {
      return undefined;
    }
    const changes: ts.TextChangeRange[] = [];
    if (!this.changeFromPrevious) {
      return undefined;
    }
    changes.push(this.changeFromPrevious);
    let cursor = this.previous;
    while (cursor && cursor !== oldSnapshot) {
      if (!cursor.changeFromPrevious) {
        return undefined;
      }
      changes.push(cursor.changeFromPrevious);
      cursor = cursor.previous;
      if (changes.length > 8) {
        return undefined;
      }
    }
    return cursor === oldSnapshot
      ? ts.collapseTextChangeRangesAcrossMultipleVersions(changes.reverse())
      : undefined;
  }
}

function jsScriptSnapshotForVirtual(
  fileName: string,
  virtual: VirtualDocument,
  settings: AspSettings,
): AspJsScriptSnapshot {
  const version = jsVirtualDocumentVersion(virtual);
  const previous = jsScriptSnapshots.get(fileName);
  if (previous?.version === version) {
    logDebugSummary(
      settings,
      `[asp-lsp] js.snapshot.changeRange.reuse: ${virtualSourceUri(virtual)}, version=${version}`,
    );
    return previous;
  }
  const change = previous ? jsVirtualTextChangeRange(previous, virtual) : undefined;
  const snapshot = new AspJsScriptSnapshot(
    fileName,
    virtual.text,
    version,
    ++jsScriptSnapshotSequence,
    virtual.sourceMap.segments.map((segment) => ({
      virtualStart: segment.virtualStart,
      virtualEnd: segment.virtualEnd,
    })),
    previous,
    change,
  );
  trimJsScriptSnapshotHistory(snapshot);
  jsScriptSnapshots.set(fileName, snapshot);
  logDebugSummary(
    settings,
    change
      ? `[asp-lsp] js.snapshot.changeRange.hit: ${virtualSourceUri(virtual)}, oldLength=${change.span.length}, newLength=${change.newLength}`
      : `[asp-lsp] js.snapshot.changeRange.miss: ${virtualSourceUri(virtual)}, reason=${
          previous ? "unsafe-or-large-edit" : "initial"
        }`,
  );
  return snapshot;
}

function jsVirtualTextChangeRange(
  previous: AspJsScriptSnapshot,
  virtual: VirtualDocument,
): ts.TextChangeRange | undefined {
  const oldText = previous.getText(0, previous.getLength());
  const nextText = virtual.text;
  if (oldText === nextText) {
    return ts.createTextChangeRange(ts.createTextSpan(0, 0), 0);
  }
  let prefix = 0;
  const minLength = Math.min(oldText.length, nextText.length);
  while (prefix < minLength && oldText.charCodeAt(prefix) === nextText.charCodeAt(prefix)) {
    prefix += 1;
  }
  let oldSuffix = oldText.length;
  let nextSuffix = nextText.length;
  while (
    oldSuffix > prefix &&
    nextSuffix > prefix &&
    oldText.charCodeAt(oldSuffix - 1) === nextText.charCodeAt(nextSuffix - 1)
  ) {
    oldSuffix -= 1;
    nextSuffix -= 1;
  }
  const deletedLength = oldSuffix - prefix;
  const insertedLength = nextSuffix - prefix;
  if (deletedLength > 256 || insertedLength > 256) {
    return undefined;
  }
  if (
    !virtualTextRangeWithinSingleSegment(previous.segments, prefix, oldSuffix) ||
    !virtualTextRangeWithinSingleSegment(virtual.sourceMap.segments, prefix, nextSuffix)
  ) {
    return undefined;
  }
  return ts.createTextChangeRange(ts.createTextSpan(prefix, deletedLength), insertedLength);
}

function virtualTextRangeWithinSingleSegment(
  segments: readonly { virtualStart: number; virtualEnd: number }[],
  start: number,
  end: number,
): boolean {
  const lastOffset = Math.max(start, end - 1);
  return segments.some(
    (segment) => segment.virtualStart <= start && lastOffset < segment.virtualEnd,
  );
}

function trimJsScriptSnapshotHistory(snapshot: AspJsScriptSnapshot): void {
  let cursor: AspJsScriptSnapshot | undefined = snapshot;
  for (let depth = 0; cursor; depth += 1) {
    if (depth >= 8) {
      cursor.previous = undefined;
      return;
    }
    cursor = cursor.previous;
  }
}

function collectJsProjectFiles(
  activeVirtual: VirtualDocument,
  settings: AspSettings,
  optionOverrides: Partial<ts.CompilerOptions> = {},
): JsProjectConfig & { files: Map<string, JsProjectFile> } {
  const files = new Map<string, JsProjectFile>();
  const addVirtual = (virtual: VirtualDocument): void => {
    const fileName = normalizeFileName(jsVirtualFileName(virtual.uri));
    const snapshot = jsScriptSnapshotForVirtual(fileName, virtual, settings);
    files.set(fileName, {
      fileName,
      text: virtual.text,
      version: snapshot.version,
      uri: virtualSourceUri(virtual),
      virtual,
      snapshot,
    });
  };
  addVirtual(activeVirtual);
  for (const document of documents.all()) {
    const cached = ensureFreshCachedDocument(document);
    if (!cached) {
      continue;
    }
    for (const virtual of jsVirtualDocuments(cached)) {
      addVirtual(virtual);
    }
  }

  const ownerFile = uriToFileName(virtualSourceUri(activeVirtual));
  const config = readJsProjectConfig(ownerFile, settings, optionOverrides);
  for (const fileName of config.fileNames) {
    const normalized = normalizeFileName(fileName);
    if (files.has(normalized) || !ts.sys.fileExists(normalized)) {
      continue;
    }
    const text = ts.sys.readFile(normalized);
    if (text === undefined) {
      continue;
    }
    const stat = fs.statSync(normalized, { throwIfNoEntry: false });
    files.set(normalized, {
      fileName: normalized,
      text,
      version: stat ? `${stat.mtimeMs}:${stat.size}` : "0",
      uri: pathToFileUri(normalized),
    });
  }
  return {
    files,
    fileNames: config.fileNames,
    options: config.options,
    currentDirectory: config.currentDirectory,
  };
}

function jsVirtualDocumentVersion(virtual: VirtualDocument): string {
  return JSON.stringify({
    language: virtual.languageId,
    text: textFingerprint(virtual.text),
  });
}

function readJsProjectConfig(
  ownerFile: string,
  settings: AspSettings,
  optionOverrides: Partial<ts.CompilerOptions> = {},
): JsProjectConfig {
  const ownerDirectory = path.dirname(ownerFile);
  const configPath =
    settings.javascript?.ignoreProjectConfig === true
      ? undefined
      : (ts.findConfigFile(ownerDirectory, ts.sys.fileExists, "tsconfig.json") ??
        ts.findConfigFile(ownerDirectory, ts.sys.fileExists, "jsconfig.json"));
  const cacheKey = jsProjectConfigCacheKey(ownerFile, configPath, settings, optionOverrides);
  const cached = jsProjectConfigCache.get(cacheKey);
  if (cached) {
    cached.lastUsed = ++jsLanguageServiceCacheTick;
    return cached.config;
  }
  let result: JsProjectConfig;
  const defaultOptions: ts.CompilerOptions = {
    allowJs: true,
    checkJs: settings.checkJs ?? false,
    noEmit: true,
    target: ts.ScriptTarget.ESNext,
    module: ts.ModuleKind.CommonJS,
    moduleResolution: ts.ModuleResolutionKind.Node10,
    lib: browserJavaScriptLibs,
  };
  if (configPath) {
    const config = ts.readConfigFile(configPath, ts.sys.readFile);
    const parsed = ts.parseJsonConfigFileContent(
      config.config ?? {},
      ts.sys,
      path.dirname(configPath),
      defaultOptions,
      configPath,
    );
    const currentDirectory = path.dirname(configPath);
    result = {
      fileNames: parsed.fileNames,
      options: browserJavaScriptCompilerOptions(
        parsed.options,
        currentDirectory,
        settings,
        optionOverrides,
      ),
      currentDirectory,
    };
  } else {
    const roots = workspaceRoots.length > 0 ? workspaceRoots : [ownerDirectory];
    const currentDirectory = roots[0] ?? ownerDirectory;
    result = {
      fileNames: [],
      options: browserJavaScriptCompilerOptions(
        defaultOptions,
        currentDirectory,
        settings,
        optionOverrides,
      ),
      currentDirectory,
    };
  }
  jsProjectConfigCache.set(cacheKey, {
    config: result,
    lastUsed: ++jsLanguageServiceCacheTick,
  });
  pruneJsProjectConfigCache();
  return result;
}

function jsProjectConfigCacheKey(
  ownerFile: string,
  configPath: string | undefined,
  settings: AspSettings,
  optionOverrides: Partial<ts.CompilerOptions>,
): string {
  const environmentFiles = [configPath, nearestPackageJson(path.dirname(ownerFile))]
    .filter((fileName): fileName is string => Boolean(fileName))
    .map((fileName) => {
      const stat = fs.statSync(fileName, { throwIfNoEntry: false });
      return stat ? `${normalizeFileName(fileName)}:${stat.mtimeMs}:${stat.size}` : fileName;
    });
  return JSON.stringify({
    ownerDirectory: normalizeFileName(path.dirname(ownerFile)),
    configPath: configPath ? normalizeFileName(configPath) : undefined,
    environmentFiles,
    settings: jsProjectSettingsIdentity(settings),
    optionOverrides,
  });
}

function pruneJsProjectConfigCache(): void {
  while (jsProjectConfigCache.size > 16) {
    const oldest = [...jsProjectConfigCache.entries()].sort(
      (left, right) => left[1].lastUsed - right[1].lastUsed,
    )[0];
    if (!oldest) {
      return;
    }
    jsProjectConfigCache.delete(oldest[0]);
  }
}

function browserJavaScriptCompilerOptions(
  options: ts.CompilerOptions,
  currentDirectory: string,
  settings: AspSettings,
  optionOverrides: Partial<ts.CompilerOptions>,
): ts.CompilerOptions {
  const next: ts.CompilerOptions = {
    ...options,
    ...optionOverrides,
    allowJs: true,
    noEmit: true,
    noLib: false,
    checkJs: optionOverrides.checkJs ?? settings.checkJs ?? false,
  };
  next.lib = ensureBrowserJavaScriptLibs(next.lib);
  next.types = browserJavaScriptTypes(next, currentDirectory);
  return next;
}

function ensureBrowserJavaScriptLibs(libs: string[] | undefined): string[] {
  const existing = new Set((libs ?? browserJavaScriptLibs).map((lib) => lib.toLowerCase()));
  return [
    ...(libs ?? []),
    ...browserJavaScriptLibs.filter((lib) => !existing.has(lib.toLowerCase())),
  ];
}

function browserJavaScriptTypes(
  options: ts.CompilerOptions,
  currentDirectory: string,
): string[] | undefined {
  if (options.types) {
    return options.types.filter((type) => type.toLowerCase() !== "node");
  }
  const host: ts.ModuleResolutionHost = {
    fileExists: ts.sys.fileExists,
    readFile: ts.sys.readFile,
    directoryExists: ts.sys.directoryExists,
    getDirectories: getTypeScriptDirectories,
    realpath: ts.sys.realpath,
    getCurrentDirectory: () => currentDirectory,
  };
  const types = ts
    .getAutomaticTypeDirectiveNames(options, host)
    .filter((type) => type.toLowerCase() !== "node");
  return types;
}

function readDirectoryEntries(dir: string): fs.Dirent[] {
  try {
    return fs.readdirSync(dir, { withFileTypes: true });
  } catch {
    return [];
  }
}

function readTypeScriptDirectory(
  rootDir: string,
  extensions?: readonly string[],
  excludes?: readonly string[],
  includes?: readonly string[],
  depth?: number,
): string[] {
  try {
    return ts.sys.readDirectory(rootDir, extensions, excludes, includes, depth);
  } catch {
    return [];
  }
}

function getTypeScriptDirectories(dir: string): string[] {
  try {
    return ts.sys.getDirectories(dir);
  } catch {
    return [];
  }
}

function scriptKindForFileName(fileName: string): ts.ScriptKind {
  const lower = fileName.toLowerCase();
  if (lower.endsWith(".ts") || lower.endsWith(".mts") || lower.endsWith(".cts")) {
    return ts.ScriptKind.TS;
  }
  if (lower.endsWith(".tsx")) {
    return ts.ScriptKind.TSX;
  }
  if (lower.endsWith(".jsx")) {
    return ts.ScriptKind.JSX;
  }
  return ts.ScriptKind.JS;
}

function resolveIncludePath(
  ownerUri: string,
  includePath: string,
  mode: "file" | "virtual",
  settings: AspSettings,
): string {
  return resolveIncludePathDetails(ownerUri, includePath, mode, settings).fileName;
}

function resolveIncludePathDetails(
  ownerUri: string,
  includePath: string,
  mode: "file" | "virtual",
  settings: AspSettings,
): IncludePathResolution {
  const cacheKey = includePathResolutionCacheKey(ownerUri, includePath, mode, settings);
  const cached = includePathResolutionCache.get(cacheKey);
  if (cached) {
    return cached;
  }
  const resolved = resolveIncludePathDetailsUncached(ownerUri, includePath, mode, settings);
  includePathResolutionCache.set(cacheKey, resolved);
  return resolved;
}

function resolveIncludePathDetailsUncached(
  ownerUri: string,
  includePath: string,
  mode: "file" | "virtual",
  settings: AspSettings,
): IncludePathResolution {
  if (mode === "virtual") {
    const normalizedInclude = includePath.replace(/^\/+/, "");
    for (const root of [
      ...(settings.virtualRoots ?? []),
      settings.virtualRoot,
      ...workspaceRoots,
      workspaceRootFromUri(ownerUri),
    ]) {
      if (!root) {
        continue;
      }
      const candidate = resolveIncludeCandidate(
        root,
        normalizedInclude,
        settings,
        (actual) => `/${path.relative(root, actual).split(path.sep).join("/")}`,
      );
      if (candidate.exists) {
        return candidate;
      }
    }
    const root = settings.virtualRoot ?? workspaceRootFromUri(ownerUri);
    return resolveIncludeCandidate(
      root,
      normalizedInclude,
      settings,
      (actual) => `/${path.relative(root, actual).split(path.sep).join("/")}`,
    );
  }
  const ownerDirectory = path.dirname(uriToFileName(ownerUri));
  const local = resolveIncludeCandidate(ownerDirectory, includePath, settings, (actual) =>
    path.relative(ownerDirectory, actual).split(path.sep).join("/"),
  );
  if (local.exists) {
    return local;
  }
  for (const root of [...(settings.includePaths ?? []), ...(settings.virtualRoots ?? [])]) {
    const candidate = resolveIncludeCandidate(root, includePath, settings, (actual) =>
      path.relative(root, actual).split(path.sep).join("/"),
    );
    if (candidate.exists) {
      return candidate;
    }
  }
  return local;
}

function includePathResolutionCacheKey(
  ownerUri: string,
  includePath: string,
  mode: "file" | "virtual",
  settings: AspSettings,
): string {
  return JSON.stringify({
    owner: normalizeFileName(uriToFileName(ownerUri)),
    includePath,
    mode,
    resolution: includeResolutionSettingsKey(settings),
  });
}

function resolveIncludeCandidate(
  baseDirectory: string,
  includePath: string,
  settings: AspSettings,
  actualIncludePath: (actualPath: string) => string,
): IncludePathResolution {
  const resolved = resolvePathFromBase(baseDirectory, includePath, settings);
  return {
    ...resolved,
    actualIncludePath: resolved.actualPath ? actualIncludePath(resolved.actualPath) : undefined,
  };
}

function resolvePathFromBase(
  baseDirectory: string,
  requestedPath: string,
  settings: AspSettings,
): PathResolution {
  const cacheKey = pathResolutionCacheKey(baseDirectory, requestedPath, settings);
  const cached = pathResolutionCache.get(cacheKey);
  if (cached) {
    return cached;
  }
  const resolved = resolvePathFromBaseUncached(baseDirectory, requestedPath, settings);
  pathResolutionCache.set(cacheKey, resolved);
  return resolved;
}

function resolvePathFromBaseUncached(
  baseDirectory: string,
  requestedPath: string,
  settings: AspSettings,
): PathResolution {
  const fileName = path.resolve(baseDirectory, requestedPath);
  if (settings.windowsPathResolution === false) {
    const exists = fs.existsSync(fileName);
    return {
      fileName,
      exists,
      pathCaseMatches: true,
      actualPath: exists ? fileName : undefined,
    };
  }
  let start = path.isAbsolute(requestedPath)
    ? path.parse(fileName).root
    : normalizeFileName(baseDirectory);
  let relative = path.relative(start, fileName);
  if (relative.startsWith("..") || path.isAbsolute(relative)) {
    start = path.parse(fileName).root;
    relative = path.relative(start, fileName);
  }
  if (!relative) {
    const exists = fs.existsSync(fileName);
    return {
      fileName,
      exists,
      pathCaseMatches: true,
      actualPath: exists ? fileName : undefined,
    };
  }
  let current = start;
  let pathCaseMatches = true;
  for (const segment of relative.split(path.sep).filter((part) => part.length > 0)) {
    let entries: fs.Dirent[];
    try {
      entries = fs.readdirSync(current, { withFileTypes: true });
    } catch {
      return { fileName, exists: false, pathCaseMatches };
    }
    const exact = entries.find((entry) => entry.name === segment);
    if (exact) {
      current = path.join(current, exact.name);
      continue;
    }
    const lower = segment.toLowerCase();
    const insensitive = entries.filter((entry) => entry.name.toLowerCase() === lower);
    if (insensitive.length !== 1) {
      return { fileName, exists: false, pathCaseMatches };
    }
    pathCaseMatches = false;
    current = path.join(current, insensitive[0].name);
  }
  return {
    fileName: current,
    exists: true,
    pathCaseMatches,
    actualPath: current,
  };
}

function pathResolutionCacheKey(
  baseDirectory: string,
  requestedPath: string,
  settings: AspSettings,
): string {
  return JSON.stringify({
    baseDirectory: normalizeFileName(baseDirectory),
    requestedPath,
    windowsPathResolution: settings.windowsPathResolution !== false,
  });
}

function includeResolutionSettingsKey(settings: AspSettings): unknown {
  return {
    virtualRoot: settings.virtualRoot,
    virtualRoots: settings.virtualRoots?.map(normalizeFileName),
    includePaths: settings.includePaths?.map(normalizeFileName),
    windowsPathResolution: settings.windowsPathResolution !== false,
    legacyEncoding: settings.legacyEncoding,
    roots: workspaceRoots.map(normalizeFileName).sort(),
  };
}

function workspaceRootFromUri(uri: string): string {
  const fileName = uriToFileName(uri);
  return fs.statSync(fileName, { throwIfNoEntry: false })?.isDirectory()
    ? fileName
    : path.dirname(fileName);
}

function uriToFileName(uri: string): string {
  const fileName = uri.startsWith("file://") ? fileURLToPath(uri) : uri;
  return fileName.replace(/\.(html|css|javascript|vbscript|jscript)\.virtual$/, "");
}

function jsVirtualFileName(uri: string): string {
  const fileName = uri.startsWith("file://") ? fileURLToPath(uri) : uri;
  return fileName.replace(/\.(javascript|jscript)\.virtual$/, ".$1.js");
}

function jsProjectFileName(
  virtual: VirtualDocument,
  project: Pick<JsLanguageServiceProject, "files">,
): string {
  const normalized = normalizeFileName(jsVirtualFileName(virtual.uri));
  return (
    project.files.get(normalized)?.fileName ??
    [...project.files.values()].find((file) => file.virtual?.uri === virtual.uri)?.fileName ??
    normalized
  );
}

function pathToFileUri(fileName: string): string {
  return pathToFileURL(fileName).toString();
}

function normalizeFileName(fileName: string): string {
  return path.resolve(fileName);
}

function sameFile(left: string, right: string): boolean {
  return normalizeFileName(left) === normalizeFileName(right);
}

function isIncDocument(uri: string): boolean {
  return uriToFileName(uri).toLowerCase().endsWith(".inc");
}

function tsCompletionKind(kind: string): CompletionItemKind {
  switch (kind) {
    case "function":
    case "method":
      return CompletionItemKind.Function;
    case "var":
    case "let":
    case "const":
      return CompletionItemKind.Variable;
    case "class":
      return CompletionItemKind.Class;
    case "property":
      return CompletionItemKind.Property;
    default:
      return CompletionItemKind.Text;
  }
}

function isVbscriptPosition(cached: CachedDocument, position: Range["start"]): boolean {
  const region = findRegionAt(cached.parsed, cached.source.offsetAt(position));
  return region?.language === "vbscript";
}

function isJavaScriptPosition(cached: CachedDocument, position: Range["start"]): boolean {
  const region = findRegionAt(cached.parsed, cached.source.offsetAt(position));
  return Boolean(region && isJavaScriptLikeRegion(region));
}

function isHtmlPosition(cached: CachedDocument, position: Range["start"]): boolean {
  const region = findRegionAt(cached.parsed, cached.source.offsetAt(position));
  return region?.language === "html";
}

function isCssPosition(cached: CachedDocument, position: Range["start"]): boolean {
  const region = findRegionAt(cached.parsed, cached.source.offsetAt(position));
  return region?.language === "css";
}

function vbWorkspaceSymbolKind(kind: VbSymbolKind): SymbolKind {
  switch (kind) {
    case "class":
      return SymbolKind.Class;
    case "method":
    case "sub":
    case "function":
      return SymbolKind.Function;
    case "property":
      return SymbolKind.Property;
    case "constant":
      return SymbolKind.Constant;
    case "field":
      return SymbolKind.Field;
    case "parameter":
    case "variable":
      return SymbolKind.Variable;
  }
}

function vbSymbolInformation(symbol: VbSymbol): SymbolInformation {
  return SymbolInformation.create(
    symbol.name,
    vbWorkspaceSymbolKind(symbol.kind),
    symbol.range,
    symbol.sourceUri,
    symbol.memberOf,
  );
}

function tsSymbolKind(kind: ts.ScriptElementKind): SymbolKind {
  switch (kind) {
    case ts.ScriptElementKind.classElement:
      return SymbolKind.Class;
    case ts.ScriptElementKind.memberFunctionElement:
    case ts.ScriptElementKind.functionElement:
    case ts.ScriptElementKind.localFunctionElement:
      return SymbolKind.Function;
    case ts.ScriptElementKind.memberVariableElement:
    case ts.ScriptElementKind.memberGetAccessorElement:
    case ts.ScriptElementKind.memberSetAccessorElement:
      return SymbolKind.Property;
    case ts.ScriptElementKind.constElement:
      return SymbolKind.Constant;
    case ts.ScriptElementKind.letElement:
    case ts.ScriptElementKind.variableElement:
      return SymbolKind.Variable;
    case ts.ScriptElementKind.moduleElement:
      return SymbolKind.Module;
    default:
      return SymbolKind.Object;
  }
}

function quickFixesForDiagnostic(cached: CachedDocument, diagnostic: Diagnostic): CodeAction[] {
  if (diagnostic.source === "asp-lsp-vbscript") {
    const name = textInRange(cached.source, diagnostic.range).trim();
    if (!name) {
      return [];
    }
    const line = diagnostic.range.start.line;
    const localizer = localizerForUri(cached.source.uri);
    return [
      {
        title: localizer.t("server.quickfix.declareDim", { name }),
        kind: CodeActionKind.QuickFix,
        diagnostics: [diagnostic],
        edit: {
          changes: {
            [cached.source.uri]: [
              {
                range: {
                  start: { line, character: 0 },
                  end: { line, character: 0 },
                },
                newText: `Dim ${name}\n`,
              },
            ],
          },
        },
      },
      ...vbscriptIncludeSuggestionActions(cached, diagnostic, name),
    ];
  }
  if (diagnostic.source === "asp-lsp-vbscript-unused") {
    return removeUnusedVbscriptDeclarationActions(cached, diagnostic);
  }
  if (diagnostic.source === "asp-lsp-vbscript-type") {
    return vbscriptTypeDiagnosticActions(cached, diagnostic);
  }
  if (diagnostic.source === "asp-lsp-vbscript-naming") {
    return vbscriptNamingDiagnosticActions(cached, diagnostic);
  }
  if (
    diagnostic.source === "asp-lsp-vbscript-syntax" &&
    diagnostic.code === "initializedDeclaration"
  ) {
    const action = splitInitializedDimDeclarationAction(cached, diagnostic.range, diagnostic);
    return action ? [action] : [];
  }
  if (diagnostic.source === "asp-lsp-vbscript-syntax") {
    const action = fixVbscriptCallSyntaxAction(cached, diagnostic);
    return action ? [action] : [];
  }
  if (diagnostic.source === "asp-lsp-include") {
    const include = cached.parsed.includes.find(
      (candidate) =>
        candidate.range.start.line === diagnostic.range.start.line &&
        candidate.range.start.character === diagnostic.range.start.character,
    );
    const includePath = include?.path;
    if (!includePath) {
      return [];
    }
    const localizer = localizerForUri(cached.source.uri);
    const targetPath = include
      ? resolveIncludePath(
          cached.source.uri,
          include.path,
          include.mode,
          cachedSettings(cached.source.uri),
        )
      : undefined;
    return [
      {
        title: localizer.t("server.quickfix.createMissingInclude", { path: includePath }),
        kind: CodeActionKind.QuickFix,
        diagnostics: [diagnostic],
        edit: targetPath
          ? ({
              documentChanges: [
                {
                  kind: "create",
                  uri: pathToFileUri(targetPath),
                  options: { ignoreIfExists: true },
                },
              ],
            } satisfies WorkspaceEdit)
          : undefined,
      },
    ];
  }
  return [];
}

function fixVbscriptCallSyntaxAction(
  cached: CachedDocument,
  diagnostic: Diagnostic,
): CodeAction | undefined {
  const data = diagnostic.data as { fixKind?: string; newText?: string } | undefined;
  if (
    data?.fixKind !== "vbscriptCallSyntax" ||
    !data.newText ||
    diagnostic.range.start.line !== diagnostic.range.end.line
  ) {
    return undefined;
  }
  const text = lineText(cached.source, diagnostic.range.start.line);
  if (text.includes(":") || /_\s*(?:'.*)?$/.test(text)) {
    return undefined;
  }
  if (!isVbscriptRange(cached, diagnostic.range)) {
    return undefined;
  }
  return {
    title: localizerForUri(cached.source.uri).t("server.quickfix.fixVbscriptCallSyntax"),
    kind: CodeActionKind.QuickFix,
    diagnostics: [diagnostic],
    edit: {
      changes: {
        [cached.source.uri]: [{ range: diagnostic.range, newText: data.newText }],
      },
    },
  };
}

function vbscriptNamingDiagnosticActions(
  cached: CachedDocument,
  diagnostic: Diagnostic,
): CodeAction[] {
  const data = diagnostic.data as
    | {
        name?: string;
        expectedName?: string;
      }
    | undefined;
  const name = data?.name ?? textInRange(cached.source, diagnostic.range).trim();
  const expectedName = data?.expectedName;
  if (
    !name ||
    !expectedName ||
    name === expectedName ||
    !/^[A-Za-z][A-Za-z0-9_]*$/.test(expectedName)
  ) {
    return [];
  }
  const context = buildVbProjectContext(cached, cachedSettings(cached.source.uri));
  const symbol = context.symbols?.find(
    (candidate) =>
      candidate.sourceUri === cached.source.uri && sameRange(candidate.range, diagnostic.range),
  );
  if (!symbol || hasVbscriptIdentifierCollision(symbol, expectedName, context.symbols ?? [])) {
    return [];
  }
  const changes: WorkspaceEdit["changes"] = {};
  for (const reference of getVbscriptReferences(cached.parsed, diagnostic.range.start, context)) {
    const edits = changes[reference.uri] ?? [];
    edits.push({ range: reference.range, newText: expectedName });
    changes[reference.uri] = edits;
  }
  if (Object.keys(changes).length === 0) {
    return [];
  }
  return [
    {
      title: localizerForUri(cached.source.uri).t("server.quickfix.renameIdentifierCase", {
        name,
        expectedName,
      }),
      kind: CodeActionKind.QuickFix,
      diagnostics: [diagnostic],
      edit: { changes },
    },
  ];
}

function hasVbscriptIdentifierCollision(
  symbol: VbSymbol,
  expectedName: string,
  symbols: VbSymbol[],
): boolean {
  const lower = expectedName.toLowerCase();
  return symbols.some(
    (candidate) =>
      candidate.name.toLowerCase() === lower &&
      !sameVbscriptSymbol(candidate, symbol) &&
      sameVbscriptNamingScope(candidate, symbol),
  );
}

function sameVbscriptSymbol(left: VbSymbol, right: VbSymbol): boolean {
  return (
    left.sourceUri === right.sourceUri &&
    left.kind === right.kind &&
    (left.memberOf ?? "").toLowerCase() === (right.memberOf ?? "").toLowerCase() &&
    (left.scopeName ?? "").toLowerCase() === (right.scopeName ?? "").toLowerCase() &&
    sameRange(left.range, right.range)
  );
}

function sameVbscriptNamingScope(left: VbSymbol, right: VbSymbol): boolean {
  if (left.memberOf || right.memberOf) {
    return (left.memberOf ?? "").toLowerCase() === (right.memberOf ?? "").toLowerCase();
  }
  if (left.scopeName || right.scopeName) {
    return (
      left.sourceUri === right.sourceUri &&
      (left.scopeName ?? "").toLowerCase() === (right.scopeName ?? "").toLowerCase()
    );
  }
  return true;
}

function vbscriptTypeDiagnosticActions(
  cached: CachedDocument,
  diagnostic: Diagnostic,
): CodeAction[] {
  const code = String(diagnostic.code ?? "");
  const data = diagnostic.data as
    | {
        name?: string;
        type?: string;
        actual?: string;
      }
    | undefined;
  const name = data?.name ?? textInRange(cached.source, diagnostic.range).trim();
  if (!name) {
    return [];
  }
  const localizer = localizerForUri(cached.source.uri);
  if (code === "objectNeedsSet") {
    return [
      {
        title: localizer.t("server.quickfix.addSet", { name }),
        kind: CodeActionKind.QuickFix,
        diagnostics: [diagnostic],
        edit: {
          changes: {
            [cached.source.uri]: [
              {
                range: { start: diagnostic.range.start, end: diagnostic.range.start },
                newText: "Set ",
              },
            ],
          },
        },
      },
    ];
  }
  if (code === "setScalar") {
    const edit = removeLeadingSetEdit(cached.source, diagnostic.range.start.line);
    return edit
      ? [
          {
            title: localizer.t("server.quickfix.removeSet", { name }),
            kind: CodeActionKind.QuickFix,
            diagnostics: [diagnostic],
            edit: { changes: { [cached.source.uri]: [edit] } },
          },
        ]
      : [];
  }
  if (code === "typeMismatch" && data?.actual) {
    return [
      {
        title: localizer.t("server.quickfix.annotateType", { name, type: data.actual }),
        kind: CodeActionKind.QuickFix,
        diagnostics: [diagnostic],
        edit: {
          changes: {
            [cached.source.uri]: [
              {
                range: {
                  start: { line: diagnostic.range.start.line, character: 0 },
                  end: { line: diagnostic.range.start.line, character: 0 },
                },
                newText: `' @type ${name} As ${data.actual}\n`,
              },
            ],
          },
        },
      },
    ];
  }
  return [];
}

function removeUnusedVbscriptDeclarationActions(
  cached: CachedDocument,
  diagnostic: Diagnostic,
): CodeAction[] {
  const symbol = buildVbProjectContext(cached, cachedSettings(cached.source.uri)).symbols?.find(
    (candidate) =>
      candidate.sourceUri === cached.source.uri && sameRange(candidate.range, diagnostic.range),
  );
  if (!symbol || symbol.kind === "class" || symbol.kind === "function" || symbol.kind === "sub") {
    return [];
  }
  const edit =
    symbol.kind === "parameter"
      ? removeVbscriptParameterEdit(cached, symbol)
      : removeLineEdit(cached.source, symbol.range.start.line);
  if (!edit) {
    return [];
  }
  return [
    {
      title: localizerForUri(cached.source.uri).t("server.quickfix.removeUnusedDeclaration", {
        name: symbol.name,
      }),
      kind: CodeActionKind.QuickFix,
      diagnostics: [diagnostic],
      edit: {
        changes: {
          [cached.source.uri]: [edit],
        },
      },
    },
  ];
}

function removeLeadingSetEdit(document: TextDocument, line: number): TextEdit | undefined {
  const text = lineText(document, line);
  const match = /^(\s*)Set\s+/i.exec(text);
  if (!match) {
    return undefined;
  }
  const start = match[1]?.length ?? 0;
  return {
    range: {
      start: { line, character: start },
      end: { line, character: match[0].length },
    },
    newText: "",
  };
}

function removeVbscriptParameterEdit(
  cached: CachedDocument,
  symbol: VbSymbol,
): TextEdit | undefined {
  const line = lineText(cached.source, symbol.range.start.line);
  const startOffset = cached.source.offsetAt(symbol.range.start);
  const endOffset = cached.source.offsetAt(symbol.range.end);
  const lineStart = cached.source.offsetAt({ line: symbol.range.start.line, character: 0 });
  const lineEnd = lineStart + line.length;
  let removeStart = startOffset;
  let removeEnd = endOffset;
  const after = cached.source.getText({
    start: symbol.range.end,
    end: cached.source.positionAt(lineEnd),
  });
  const before = cached.source.getText({
    start: cached.source.positionAt(lineStart),
    end: symbol.range.start,
  });
  const afterComma = /^\s*,\s*/.exec(after);
  const beforeComma = /,\s*$/.exec(before);
  if (afterComma) {
    removeEnd += afterComma[0].length;
  } else if (beforeComma) {
    removeStart -= beforeComma[0].length;
  }
  if (removeStart < lineStart || removeEnd > lineEnd || removeStart >= removeEnd) {
    return undefined;
  }
  return {
    range: {
      start: cached.source.positionAt(removeStart),
      end: cached.source.positionAt(removeEnd),
    },
    newText: "",
  };
}

function vbscriptIncludeSuggestionActions(
  cached: CachedDocument,
  diagnostic: Diagnostic,
  symbolName: string,
): CodeAction[] {
  const settings = cachedSettings(cached.source.uri);
  if (settings.vbscript?.includeSuggestions === false) {
    return [];
  }
  ensureWorkspaceIndex();
  const ownerFile = normalizeFileName(uriToFileName(cached.source.uri));
  const includedFiles = new Set(
    cached.parsed.includes.map((include) =>
      normalizeFileName(
        resolveIncludePath(cached.source.uri, include.path, include.mode, settings),
      ),
    ),
  );
  const candidates = new Map<string, CachedDocument>();
  for (const entry of workspaceIndex.values()) {
    if (normalizeFileName(entry.fileName) !== ownerFile) {
      candidates.set(normalizeFileName(entry.fileName), cachedFromIndexed(entry));
    }
  }
  for (const document of documents.all()) {
    const fileName = normalizeFileName(uriToFileName(document.uri));
    if (fileName !== ownerFile) {
      const opened = ensureFreshCachedDocument(document);
      if (opened) {
        candidates.set(fileName, opened);
      }
    }
  }
  const matches = [...candidates.entries()]
    .filter(([fileName]) => !includedFiles.has(fileName))
    .map(([fileName, candidate]) => {
      const hasSymbol = collectVbscriptSymbols(candidate.parsed).some(
        (symbol) =>
          symbol.sourceUri === candidate.source.uri &&
          !symbol.memberOf &&
          symbol.name.toLowerCase() === symbolName.toLowerCase(),
      );
      return hasSymbol ? { fileName, candidate } : undefined;
    })
    .filter((item): item is { fileName: string; candidate: CachedDocument } => Boolean(item))
    .sort(
      (left, right) => includeCandidateRank(left.fileName) - includeCandidateRank(right.fileName),
    )
    .slice(0, 5);
  const insert = includeInsertionPoint(cached);
  const localizer = localizerForUri(cached.source.uri);
  return matches.map(({ fileName }) => {
    const include = includeSuggestionPath(cached.source.uri, fileName, settings);
    return {
      title: localizer.t("server.quickfix.includeSymbol", {
        path: include.path,
        symbol: symbolName,
      }),
      kind: CodeActionKind.QuickFix,
      diagnostics: [diagnostic],
      edit: {
        changes: {
          [cached.source.uri]: [
            {
              range: { start: insert, end: insert },
              newText: `<!-- #include ${include.mode}="${include.path}" -->\n`,
            },
          ],
        },
      },
    } satisfies CodeAction;
  });
}

function includeCandidateRank(fileName: string): number {
  const lower = fileName.toLowerCase();
  return (lower.endsWith(".inc") ? 0 : lower.endsWith(".asa") ? 1 : 2) * 100_000 + lower.length;
}

function includeInsertionPoint(cached: CachedDocument): Position {
  if (cached.parsed.includes.length === 0) {
    return { line: 0, character: 0 };
  }
  const last = cached.parsed.includes.reduce((current, include) =>
    include.range.end.line > current.range.end.line ? include : current,
  );
  return { line: last.range.end.line + 1, character: 0 };
}

function includeSuggestionPath(
  ownerUri: string,
  targetFile: string,
  settings: AspSettings,
): { mode: "file" | "virtual"; path: string } {
  for (const root of [...(settings.virtualRoots ?? []), settings.virtualRoot]) {
    if (!root) {
      continue;
    }
    const relative = path.relative(root, targetFile);
    if (!relative.startsWith("..") && !path.isAbsolute(relative)) {
      return { mode: "virtual", path: `/${relative.split(path.sep).join("/")}` };
    }
  }
  return {
    mode: "file",
    path: path
      .relative(path.dirname(uriToFileName(ownerUri)), targetFile)
      .split(path.sep)
      .join("/"),
  };
}

const vbscriptExtractVariableKind = `${CodeActionKind.Refactor}.extract`;

function vbscriptCodeActions(
  cached: CachedDocument,
  range: Range,
  context: CodeActionContext,
): CodeAction[] {
  const actions: CodeAction[] = [];
  if (codeActionAllows(context, CodeActionKind.QuickFix)) {
    if (context.diagnostics.length === 0) {
      const documentation = getVbscriptDocumentationQuickAction(
        cached.parsed,
        range.start,
        buildVbProjectContext(cached, cachedSettings(cached.source.uri)),
      );
      if (documentation) {
        actions.push({
          title: localizerForUri(cached.source.uri).t(
            "server.quickfix.generateVbscriptDocumentation",
          ),
          kind: CodeActionKind.QuickFix,
          edit: {
            changes: {
              [cached.source.uri]: documentation.edits,
            },
          },
        });
      }
    }
    const hasInitializedDimDiagnostic = context.diagnostics.some(
      (diagnostic) =>
        diagnostic.source === "asp-lsp-vbscript-syntax" &&
        diagnostic.code === "initializedDeclaration",
    );
    if (!hasInitializedDimDiagnostic) {
      const splitDim = splitInitializedDimDeclarationAction(cached, range);
      if (splitDim) {
        actions.push(splitDim);
      }
    }
    const splitMultiDim = splitMultiDimDeclarationAction(cached, range);
    if (splitMultiDim) {
      actions.push(splitMultiDim);
    }
  }
  if (!codeActionAllows(context, vbscriptExtractVariableKind)) {
    return actions;
  }
  const edit = extractVbscriptVariableEdit(cached, range);
  if (!edit) {
    return actions;
  }
  actions.push({
    title: localizerForUri(cached.source.uri).t("server.refactor.extractVbscriptVariable"),
    kind: vbscriptExtractVariableKind,
    edit,
  });
  return actions;
}

function splitInitializedDimDeclarationAction(
  cached: CachedDocument,
  range: Range,
  diagnostic?: Diagnostic,
): CodeAction | undefined {
  const line = range.start.line;
  if (range.end.line !== line) {
    return undefined;
  }
  const text = lineText(cached.source, line);
  const match = /^(\s*)Dim\s+([A-Za-z][A-Za-z0-9_]*)\s*=\s*(.+?)\s*$/i.exec(text);
  if (!match || /[_:]/.test(text) || !isVbscriptRange(cached, lineRange(cached.source, line))) {
    return undefined;
  }
  const [, indent, name, value] = match;
  if (!name || !value.trim()) {
    return undefined;
  }
  return {
    title: localizerForUri(cached.source.uri).t("server.quickfix.splitInitializedDim"),
    kind: CodeActionKind.QuickFix,
    diagnostics: diagnostic ? [diagnostic] : undefined,
    edit: {
      changes: {
        [cached.source.uri]: [
          {
            range: lineRange(cached.source, line),
            newText: `${indent}Dim ${name}\n${indent}${name} = ${value.trim()}`,
          },
        ],
      },
    },
  };
}

function splitMultiDimDeclarationAction(
  cached: CachedDocument,
  range: Range,
): CodeAction | undefined {
  const node = findMultiDimDeclarationNode(cached, range);
  if (!node) {
    return undefined;
  }
  const segments = splitDimDeclarationSegments(cached.source.getText(), node);
  if (!segments || segments.length < 2) {
    return undefined;
  }
  const start = cached.source.positionAt(node.start);
  const end = cached.source.positionAt(node.end);
  const line = lineText(cached.source, start.line);
  const lineStart = cached.source.offsetAt({ line: start.line, character: 0 });
  const prefix = cached.source.getText().slice(lineStart, node.start);
  const indent = /^\s*$/.test(prefix) ? prefix : "";
  const newText = segments
    .map((segment, index) => `${index === 0 ? "" : indent}Dim ${segment}`)
    .join("\n");
  if (!line.slice(start.character, end.character).trim()) {
    return undefined;
  }
  return {
    title: localizerForUri(cached.source.uri).t("server.quickfix.splitMultiDim"),
    kind: CodeActionKind.QuickFix,
    edit: {
      changes: {
        [cached.source.uri]: [
          {
            range: { start, end },
            newText,
          },
        ],
      },
    },
  };
}

function findMultiDimDeclarationNode(cached: CachedDocument, range: Range): VbCstNode | undefined {
  const rangeStart = cached.source.offsetAt(range.start);
  const rangeEnd = cached.source.offsetAt(range.end);
  return vbscriptNodes(cached.parsed).find((node) => {
    if (
      node.kind !== "VariableDeclaration" ||
      node.declarationKind !== "dim" ||
      (node.identifiers?.length ?? 0) < 2
    ) {
      return false;
    }
    const start = cached.source.positionAt(node.start);
    const end = cached.source.positionAt(node.end);
    if (
      start.line !== end.line ||
      !isVbscriptRange(cached, { start, end }) ||
      !rangeTouchesOffsets(rangeStart, rangeEnd, node.start, node.end)
    ) {
      return false;
    }
    const tokens = node.tokens.filter((token) => !isVbscriptTriviaToken(token));
    return (
      !topLevelVbscriptToken(tokens, "=") &&
      !topLevelVbscriptToken(tokens, ":") &&
      !topLevelVbscriptKeyword(tokens, "as")
    );
  });
}

function vbscriptNodes(parsed: AspParsedDocument): VbCstNode[] {
  const nodes: VbCstNode[] = [];
  const visit = (node: AspCstNode): void => {
    if (node.vbscript) {
      nodes.push(...flattenVbscriptNode(node.vbscript));
    }
    for (const child of node.children) {
      visit(child);
    }
  };
  visit(parsed.cst);
  return nodes;
}

function flattenVbscriptNode(node: VbCstNode): VbCstNode[] {
  return [node, ...node.children.flatMap((child) => flattenVbscriptNode(child))];
}

function rangeTouchesOffsets(
  rangeStart: number,
  rangeEnd: number,
  nodeStart: number,
  nodeEnd: number,
): boolean {
  return rangeStart === rangeEnd
    ? rangeStart >= nodeStart && rangeStart <= nodeEnd
    : rangeStart < nodeEnd && rangeEnd > nodeStart;
}

function splitDimDeclarationSegments(text: string, node: VbCstNode): string[] | undefined {
  const tokens = node.tokens.filter((token) => token.kind !== "comment");
  const keyword = tokens.find((token) => !isVbscriptTriviaToken(token));
  if (keyword?.text.toLowerCase() !== "dim") {
    return undefined;
  }
  const segments: string[] = [];
  let depth = 0;
  let segmentStart = keyword.end;
  for (const token of tokens) {
    if (token.end <= keyword.end || isVbscriptTriviaToken(token)) {
      continue;
    }
    if (token.text === "(") {
      depth += 1;
    } else if (token.text === ")") {
      depth = Math.max(0, depth - 1);
    } else if (token.text === "," && depth === 0) {
      const segment = text.slice(segmentStart, token.start).trim();
      if (!segment) {
        return undefined;
      }
      segments.push(segment);
      segmentStart = token.end;
    }
  }
  const lastSegment = text.slice(segmentStart, node.end).trim();
  if (!lastSegment) {
    return undefined;
  }
  segments.push(lastSegment);
  return segments.length === (node.identifiers?.length ?? 0) ? segments : undefined;
}

function topLevelVbscriptToken(tokens: VbCstNode["tokens"], text: string): boolean {
  let depth = 0;
  for (const token of tokens) {
    if (token.text === "(") {
      depth += 1;
    } else if (token.text === ")") {
      depth = Math.max(0, depth - 1);
    } else if (token.text === text && depth === 0) {
      return true;
    }
  }
  return false;
}

function topLevelVbscriptKeyword(tokens: VbCstNode["tokens"], keyword: string): boolean {
  let depth = 0;
  for (const token of tokens) {
    if (token.text === "(") {
      depth += 1;
    } else if (token.text === ")") {
      depth = Math.max(0, depth - 1);
    } else if (token.text.toLowerCase() === keyword && depth === 0) {
      return true;
    }
  }
  return false;
}

function isVbscriptTriviaToken(token: VbCstNode["tokens"][number]): boolean {
  return token.kind === "whitespace" || token.kind === "newline" || token.kind === "comment";
}

function extractVbscriptVariableEdit(
  cached: CachedDocument,
  range: Range,
): WorkspaceEdit | undefined {
  if (!isVbscriptRange(cached, range) || range.start.line !== range.end.line) {
    return undefined;
  }
  const selected = textInRange(cached.source, range);
  if (!selected.trim() || selected !== selected.trim() || /[\r\n]/.test(selected)) {
    return undefined;
  }
  const line = lineText(cached.source, range.start.line);
  const indent = /^\s*/.exec(line)?.[0] ?? "";
  const name = nextVbscriptExtractVariableName(cached);
  const insertPosition = { line: range.start.line, character: 0 };
  return {
    changes: {
      [cached.source.uri]: [
        {
          range: { start: insertPosition, end: insertPosition },
          newText: `${indent}Dim ${name}\n${indent}${name} = ${selected}\n`,
        },
        {
          range,
          newText: name,
        },
      ],
    },
  };
}

function nextVbscriptExtractVariableName(cached: CachedDocument): string {
  const used = new Set(
    collectVbscriptSymbols(cached.parsed).map((symbol) => symbol.name.toLowerCase()),
  );
  if (!used.has("extractedvalue")) {
    return "extractedValue";
  }
  for (let index = 1; index < 1000; index += 1) {
    const name = `extractedValue${index}`;
    if (!used.has(name.toLowerCase())) {
      return name;
    }
  }
  return "extractedValue";
}

function isVbscriptRange(cached: CachedDocument, range: Range): boolean {
  const start = cached.source.offsetAt(range.start);
  const end = cached.source.offsetAt(range.end);
  if (start >= end) {
    return false;
  }
  return (
    findRegionAt(cached.parsed, start)?.language === "vbscript" &&
    findRegionAt(cached.parsed, Math.max(start, end - 1))?.language === "vbscript"
  );
}

function cssCodeActions(
  cached: CachedDocument,
  range: Range,
  context: CodeActionContext,
): CodeAction[] {
  const virtual = getCachedVirtual(cached, "css");
  const start = virtual?.sourceMap.toVirtualPosition(range.start);
  const end = virtual?.sourceMap.toVirtualPosition(range.end);
  if (!virtual || !start || !end) {
    return [];
  }
  const doc = toTextDocument(virtual);
  return cssService
    .doCodeActions2(doc, { start, end }, context, cssService.parseStylesheet(doc))
    .map((action) => remapCssCodeAction(virtual, action, cached.source.uri))
    .filter((action): action is CodeAction => Boolean(action));
}

function remapCssCodeAction(
  virtual: VirtualDocument,
  action: CodeAction,
  sourceUri: string,
): CodeAction | undefined {
  if (!action.edit) {
    return action;
  }
  return {
    ...action,
    edit: remapWorkspaceEdit(virtual, action.edit, sourceUri),
  };
}

function jsCodeActions(
  cached: CachedDocument,
  range: Range,
  context: CodeActionContext,
): CodeAction[] {
  const actions: CodeAction[] = [];
  if (codeActionAllows(context, CodeActionKind.SourceOrganizeImports)) {
    const edit = organizeJavaScriptImportsEdit(cached);
    if (edit || jsVirtualDocuments(cached).length > 0) {
      actions.push({
        title: localizerForUri(cached.source.uri).t("server.codeAction.organizeJavascriptImports"),
        kind: "source.organizeImports.aspLsp.javascript",
        edit: edit ?? { changes: {} },
      });
    }
  }
  const sourceStart = cached.source.offsetAt(range.start);
  const sourceEnd = cached.source.offsetAt(range.end);
  for (const virtual of jsVirtualDocuments(cached)) {
    const virtualStart = virtual.sourceMap.toVirtualOffset(sourceStart);
    const virtualEnd = virtual.sourceMap.toVirtualOffset(sourceEnd);
    if (virtualStart === undefined || virtualEnd === undefined) {
      continue;
    }
    const project = createJsLanguageService(virtual, cachedSettings(cached.source.uri));
    const service = project.service;
    const fileName = jsProjectFileName(virtual, project);
    if (codeActionAllows(context, CodeActionKind.QuickFix)) {
      const errorCodes = context.diagnostics
        .filter(
          (diagnostic) =>
            diagnostic.source === "asp-lsp-typescript" ||
            diagnostic.source === "asp-lsp-typescript-unused",
        )
        .map((diagnostic) => Number(diagnostic.code))
        .filter((code) => Number.isInteger(code));
      for (const fix of service.getCodeFixesAtPosition(
        fileName,
        virtualStart,
        virtualEnd,
        errorCodes,
        {},
        jsCompletionPreferences(cachedSettings(cached.source.uri)),
      )) {
        const edit = fileTextChangesToWorkspaceEdit(virtual, fix.changes);
        if (edit) {
          actions.push({ title: fix.description, kind: CodeActionKind.QuickFix, edit });
        }
      }
    }
    if (codeActionAllows(context, CodeActionKind.Refactor)) {
      for (const refactor of service.getApplicableRefactors(
        fileName,
        { pos: virtualStart, end: virtualEnd },
        {},
      )) {
        for (const action of refactor.actions.filter((item) => !item.notApplicableReason)) {
          const edits = service.getEditsForRefactor(
            fileName,
            {},
            { pos: virtualStart, end: virtualEnd },
            refactor.name,
            action.name,
            {},
          );
          const edit = edits ? fileTextChangesToWorkspaceEdit(virtual, edits.edits) : undefined;
          if (edit) {
            actions.push({
              title: action.description,
              kind: action.kind ?? CodeActionKind.Refactor,
              edit,
            });
          }
        }
      }
    }
  }
  return actions;
}

function codeActionAllows(context: CodeActionContext, kind: string): boolean {
  if (!context.only || context.only.length === 0) {
    return true;
  }
  return context.only.some(
    (candidate) =>
      kind === candidate || kind.startsWith(`${candidate}.`) || candidate.startsWith(`${kind}.`),
  );
}

function organizeJavaScriptImportsEdit(cached: CachedDocument): WorkspaceEdit | undefined {
  const edits = jsVirtualDocuments(cached)
    .map((virtual) => {
      const project = createJsLanguageService(virtual, cachedSettings(cached.source.uri));
      return fileTextChangesToWorkspaceEdit(
        virtual,
        project.service.organizeImports(
          { type: "file", fileName: jsProjectFileName(virtual, project) },
          {},
          {},
        ),
      );
    })
    .filter((edit): edit is WorkspaceEdit => Boolean(edit));
  return mergeWorkspaceEdits(edits);
}

function fileTextChangesToWorkspaceEdit(
  virtual: VirtualDocument,
  changes: readonly ts.FileTextChanges[],
): WorkspaceEdit | undefined {
  const sourceUri = virtualSourceUri(virtual);
  const currentFileName = normalizeFileName(jsVirtualFileName(virtual.uri));
  const edits: TextEdit[] = [];
  for (const change of changes) {
    if (normalizeFileName(change.fileName) !== currentFileName) {
      return undefined;
    }
    for (const textChange of change.textChanges) {
      const edit = textChangeToSourceTextEdit(virtual, textChange);
      if (!edit) {
        return undefined;
      }
      edits.push(edit);
    }
  }
  return edits.length > 0 ? { changes: { [sourceUri]: edits } } : undefined;
}

function textChangeToSourceTextEdit(
  virtual: VirtualDocument,
  textChange: ts.TextChange,
): TextEdit | undefined {
  const range = textSpanToSourceRange(virtual, textChange.span);
  return range ? { range, newText: textChange.newText } : undefined;
}

function mergeWorkspaceEdits(
  edits: Array<WorkspaceEdit | null | undefined>,
): WorkspaceEdit | undefined {
  const changes: NonNullable<WorkspaceEdit["changes"]> = {};
  for (const edit of edits) {
    if (!edit) {
      continue;
    }
    for (const [uri, textEdits] of Object.entries(edit.changes ?? {})) {
      changes[uri] = [...(changes[uri] ?? []), ...textEdits];
    }
  }
  return Object.keys(changes).length > 0 ? { changes } : undefined;
}

function codeLenses(cached: CachedDocument): CodeLens[] {
  const settings = cachedSettings(cached.source.uri).codeLens;
  const localizer = localizerForUri(cached.source.uri);
  const lenses: CodeLens[] = [];
  if (settings?.references !== false) {
    const context = buildVbProjectContext(cached, cachedSettings(cached.source.uri));
    for (const symbol of (context.symbols ?? []).filter(
      (item) =>
        item.sourceUri === cached.source.uri &&
        ["function", "sub", "class", "method", "property"].includes(item.kind),
    )) {
      const references = getVbscriptReferences(cached.parsed, symbol.range.start, context, {
        includeDeclaration: false,
        includeFunctionReturnAssignments: false,
      });
      lenses.push({
        range: symbol.range,
        command: {
          title: localizer.t(
            references.length === 1 ? "server.codeLens.reference" : "server.codeLens.references",
            { count: references.length },
          ),
          command: "aspLsp.showReferences",
          arguments: [
            cached.source.uri,
            symbol.range.start,
            references.map((reference) => Location.create(reference.uri, reference.range)),
          ],
        },
      });
    }
  }
  if (settings?.includes !== false) {
    for (const include of cached.parsed.includes) {
      const target = resolveIncludePath(
        cached.source.uri,
        include.path,
        include.mode,
        cachedSettings(cached.source.uri),
      );
      lenses.push({
        range: include.range,
        command: {
          title: localizer.t("server.codeLens.include", { name: path.basename(target) }),
          command: "vscode.open",
          arguments: [pathToFileUri(target)],
        },
      });
    }
  }
  return lenses;
}

function onTypeFormatting(
  cached: CachedDocument,
  position: Position,
  character: string,
  formattingOptions: FormattingOptions,
): TextEdit[] {
  if (character === ">") {
    return (
      jsOnTypeFormatting(cached, position, character, formattingOptions) ??
      htmlOnTypeFormatting(cached, position, formattingOptions) ??
      aspCloseOnTypeFormatting(cached, position, formattingOptions) ??
      []
    );
  }
  const jsEdits = jsOnTypeFormatting(cached, position, character, formattingOptions);
  if (jsEdits) {
    return jsEdits;
  }
  if (!isVbscriptPosition(cached, position)) {
    return [];
  }
  const line = position.line;
  if (line <= 0) {
    return [];
  }
  const current = lineText(cached.source, line);
  const previous = previousNonEmptyLine(cached.source, line - 1);
  if (!previous) {
    return [];
  }
  const options = formatOptions(formattingOptions, cachedSettings(cached.source.uri));
  const unit =
    options.indentStyle === "tab" || options.insertSpaces === false
      ? "\t"
      : " ".repeat(options.indentSize ?? options.tabSize);
  const baseIndent = /^\s*/.exec(previous.text)?.[0] ?? "";
  const trimmedPrevious = previous.text.trim();
  const trimmedCurrent = current.trim();
  const shouldIndent =
    /^(If|For|For\s+Each|Do|While|With|Sub|Function|Class|Property)\b/i.test(trimmedPrevious) &&
    !/^End\b/i.test(trimmedPrevious);
  const shouldOutdent = /^(End|Else|ElseIf|Next|Loop|Wend)\b/i.test(trimmedCurrent);
  const desired = shouldOutdent
    ? baseIndent.slice(0, Math.max(0, baseIndent.length - unit.length))
    : shouldIndent
      ? `${baseIndent}${unit}`
      : baseIndent;
  const existing = /^\s*/.exec(current)?.[0] ?? "";
  return existing === desired
    ? []
    : [
        {
          range: {
            start: { line, character: 0 },
            end: { line, character: existing.length },
          },
          newText: desired,
        },
      ];
}

function jsOnTypeFormatting(
  cached: CachedDocument,
  position: Position,
  character: string,
  formattingOptions: FormattingOptions,
): TextEdit[] | undefined {
  const context = jsContextAt(cached, position);
  if (!context) {
    return undefined;
  }
  return context.service
    .getFormattingEditsAfterKeystroke(
      context.fileName,
      context.offset,
      character,
      tsFormatOptions(formatOptions(formattingOptions, cachedSettings(cached.source.uri))),
    )
    .map((change) => textChangeToSourceTextEdit(context.virtual, change))
    .filter((edit): edit is TextEdit => Boolean(edit));
}

function htmlOnTypeFormatting(
  cached: CachedDocument,
  position: Position,
  formattingOptions: FormattingOptions,
): TextEdit[] | undefined {
  const offset = Math.max(0, cached.source.offsetAt(position) - 1);
  const region = findRegionAt(cached.parsed, offset);
  if (!region || region.language !== "html") {
    return undefined;
  }
  const lineRange = {
    start: { line: position.line, character: 0 },
    end: { line: position.line + 1, character: 0 },
  };
  if (rangeOverlapsNonHtml(cached, lineRange)) {
    return undefined;
  }
  const virtual = getCachedVirtual(cached, "html");
  if (!virtual) {
    return undefined;
  }
  return htmlService
    .format(toTextDocument(virtual), lineRange, {
      tabSize: formattingOptions.tabSize,
      insertSpaces: formattingOptions.insertSpaces,
    })
    .map((edit) => {
      const range = sourceRangeFromVirtualRange(virtual, edit.range);
      return range ? { ...edit, range } : undefined;
    })
    .filter((edit): edit is TextEdit => Boolean(edit));
}

function aspCloseOnTypeFormatting(
  cached: CachedDocument,
  position: Position,
  formattingOptions: FormattingOptions,
): TextEdit[] | undefined {
  const current = lineText(cached.source, position.line);
  if (!current.slice(0, position.character).trimEnd().endsWith("%>")) {
    return undefined;
  }
  const previous = previousNonEmptyLine(cached.source, position.line - 1);
  if (!previous) {
    return undefined;
  }
  const options = formatOptions(formattingOptions, cachedSettings(cached.source.uri));
  const unit =
    options.indentStyle === "tab" || options.insertSpaces === false
      ? "\t"
      : " ".repeat(options.indentSize ?? options.tabSize);
  const previousIndent = /^\s*/.exec(previous.text)?.[0] ?? "";
  const desired = /^End\b/i.test(previous.text.trim())
    ? previousIndent.slice(0, Math.max(0, previousIndent.length - unit.length))
    : previousIndent;
  const existing = /^\s*/.exec(current)?.[0] ?? "";
  return existing === desired
    ? []
    : [
        {
          range: {
            start: { line: position.line, character: 0 },
            end: { line: position.line, character: existing.length },
          },
          newText: desired,
        },
      ];
}

function lineText(document: TextDocument, line: number): string {
  return document
    .getText({
      start: { line, character: 0 },
      end: { line: line + 1, character: 0 },
    })
    .replace(/\r?\n$/, "");
}

function lineRange(document: TextDocument, line: number): Range {
  return {
    start: { line, character: 0 },
    end: { line, character: lineText(document, line).length },
  };
}

function textInRange(document: TextDocument, range: Range): string {
  return document.getText(range);
}

function removeLineEdit(document: TextDocument, line: number): TextEdit {
  const end =
    line + 1 < document.lineCount
      ? { line: line + 1, character: 0 }
      : { line, character: lineText(document, line).length };
  return {
    range: {
      start: { line, character: 0 },
      end,
    },
    newText: "",
  };
}

function sameRange(left: Range, right: Range): boolean {
  return (
    left.start.line === right.start.line &&
    left.start.character === right.start.character &&
    left.end.line === right.end.line &&
    left.end.character === right.end.character
  );
}

function rangesOverlap(left: Range, right: Range): boolean {
  return comparePositions(left.start, right.end) < 0 && comparePositions(left.end, right.start) > 0;
}

function comparePositions(left: Position, right: Position): number {
  if (left.line !== right.line) {
    return left.line - right.line;
  }
  return left.character - right.character;
}

function wordRangeAt(document: TextDocument, position: Position): Range | null {
  const line = lineText(document, position.line);
  const pattern = /[A-Za-z_][A-Za-z0-9_-]*/g;
  let match: RegExpExecArray | null;
  while ((match = pattern.exec(line))) {
    const start = match.index;
    const end = start + match[0].length;
    if (position.character >= start && position.character <= end) {
      return {
        start: { line: position.line, character: start },
        end: { line: position.line, character: end },
      };
    }
  }
  return null;
}

function previousNonEmptyLine(
  document: TextDocument,
  line: number,
): { line: number; text: string } | undefined {
  for (let current = line; current >= 0; current -= 1) {
    const text = lineText(document, current);
    if (text.trim().length > 0) {
      return { line: current, text };
    }
  }
  return undefined;
}

function buildSemanticTokens(cached: CachedDocument, range?: Range): SemanticTokens {
  const rangeStart = range ? cached.source.offsetAt(range.start) : 0;
  const rangeEnd = range ? cached.source.offsetAt(range.end) : cached.source.getText().length;
  const tokens: SemanticTokenData[] = [];
  for (const region of regionsInSourceRange(cached.parsed, rangeStart, rangeEnd)) {
    if (region.kind === "asp-block" || region.kind === "asp-expression") {
      addSemanticTokenInSourceRange(
        tokens,
        cached.source,
        region.start,
        2,
        "keyword",
        rangeStart,
        rangeEnd,
      );
      if (region.kind === "asp-expression") {
        addSemanticTokenInSourceRange(
          tokens,
          cached.source,
          region.start + 2,
          1,
          "keyword",
          rangeStart,
          rangeEnd,
        );
      }
      if (region.end - region.contentEnd >= 2) {
        addSemanticTokenInSourceRange(
          tokens,
          cached.source,
          region.contentEnd,
          2,
          "keyword",
          rangeStart,
          rangeEnd,
        );
      }
    } else if (region.kind === "asp-directive") {
      addSemanticTokenInSourceRange(
        tokens,
        cached.source,
        region.start,
        Math.min(region.end - region.start, 3),
        "keyword",
        rangeStart,
        rangeEnd,
      );
    }
  }
  const vbContext = bestEffortVbProjectContext(cached, cachedSettings(cached.source.uri));
  for (const semanticToken of getVbscriptSemanticTokens(cached.parsed, vbContext, range)) {
    const offset = cached.source.offsetAt(semanticToken.range.start);
    if (offset < rangeStart || offset > rangeEnd) {
      continue;
    }
    tokens.push({
      line: semanticToken.range.start.line,
      character: semanticToken.range.start.character,
      length: Math.max(1, semanticToken.range.end.character - semanticToken.range.start.character),
      tokenType: semanticToken.tokenType,
      tokenModifiers: semanticToken.tokenModifiers,
    });
  }
  addFallbackVbSemanticTokens(tokens, cached, vbContext, rangeStart, rangeEnd);
  addIncludeSemanticTokens(tokens, cached, rangeStart, rangeEnd);
  addEmbeddedSemanticTokens(tokens, cached, rangeStart, rangeEnd);
  const uniqueTokens = dedupeSemanticTokens(tokens).sort(
    (left, right) => left.line - right.line || left.character - right.character,
  );
  const builder = new SemanticTokensBuilder();
  for (const token of uniqueTokens) {
    builder.push(
      token.line,
      token.character,
      token.length,
      semanticTokenTypes.indexOf(token.tokenType as (typeof semanticTokenTypes)[number]),
      semanticTokenModifierBitset(token.tokenModifiers),
    );
  }
  return builder.build();
}

function addSemanticTokenInSourceRange(
  tokens: SemanticTokenData[],
  document: TextDocument,
  offset: number,
  length: number,
  tokenType: string,
  rangeStart: number,
  rangeEnd: number,
  tokenModifiers?: readonly string[],
): void {
  if (offset < rangeStart || offset > rangeEnd) {
    return;
  }
  addSemanticToken(tokens, document, offset, length, tokenType, tokenModifiers);
}

function regionsInSourceRange(
  parsed: AspParsedDocument,
  rangeStart: number,
  rangeEnd: number,
): AspRegion[] {
  const regions = parsed.regions;
  let low = 0;
  let high = regions.length;
  while (low < high) {
    const middle = Math.floor((low + high) / 2);
    if (regions[middle].end < rangeStart) {
      low = middle + 1;
    } else {
      high = middle;
    }
  }
  const result: AspRegion[] = [];
  for (let index = low; index < regions.length; index += 1) {
    const region = regions[index];
    if (region.start > rangeEnd) {
      break;
    }
    result.push(region);
  }
  return result;
}

function addIncludeSemanticTokens(
  tokens: SemanticTokenData[],
  cached: CachedDocument,
  rangeStart: number,
  rangeEnd: number,
): void {
  for (const include of cached.parsed.includes) {
    addRangeSemanticToken(
      tokens,
      cached.source,
      include.directiveRange,
      "keyword",
      rangeStart,
      rangeEnd,
    );
    addRangeSemanticToken(
      tokens,
      cached.source,
      include.modeRange,
      "property",
      rangeStart,
      rangeEnd,
    );
    addRangeSemanticToken(tokens, cached.source, include.pathRange, "string", rangeStart, rangeEnd);
  }
}

function addFallbackVbSemanticTokens(
  tokens: SemanticTokenData[],
  cached: CachedDocument,
  context: VbProjectContext,
  rangeStart: number,
  rangeEnd: number,
): void {
  const candidates = (context.symbols ?? []).filter(
    (symbol) => symbol.sourceUri !== cached.parsed.uri && !symbol.scopeName && !symbol.memberOf,
  );
  for (const symbol of candidates) {
    const tokenType = fallbackVbSemanticTokenType(symbol.kind);
    if (!tokenType) {
      continue;
    }
    const pattern = new RegExp(`\\b${escapeRegExp(symbol.name)}\\b`, "gi");
    const searchStart = Math.max(0, rangeStart - symbol.name.length);
    const searchEnd = Math.min(cached.parsed.text.length, rangeEnd + symbol.name.length);
    const searchText = cached.parsed.text.slice(searchStart, searchEnd);
    let match: RegExpExecArray | null;
    while ((match = pattern.exec(searchText))) {
      const start = searchStart + match.index;
      const end = start + match[0].length;
      if (
        end < rangeStart ||
        start > rangeEnd ||
        !isWholeWordAt(cached.parsed.text, start, end) ||
        !isVbscriptOffset(cached.parsed, start)
      ) {
        continue;
      }
      const position = cached.source.positionAt(start);
      tokens.push({
        line: position.line,
        character: position.character,
        length: match[0].length,
        tokenType,
        tokenModifiers: fallbackVbSemanticTokenModifiers(symbol),
      });
    }
  }
}

function isWholeWordAt(text: string, start: number, end: number): boolean {
  return !/[A-Za-z0-9_]/.test(text.charAt(start - 1)) && !/[A-Za-z0-9_]/.test(text.charAt(end));
}

function fallbackVbSemanticTokenType(kind: VbSymbolKind): string | undefined {
  if (kind === "function" || kind === "sub") {
    return "function";
  }
  if (kind === "class") {
    return "class";
  }
  if (kind === "variable" || kind === "constant") {
    return "variable";
  }
  return undefined;
}

function fallbackVbSemanticTokenModifiers(symbol: VbSymbol): readonly string[] | undefined {
  if (symbol.visibility === "public") {
    return ["public"];
  }
  if (symbol.visibility === "private") {
    return ["private"];
  }
  return undefined;
}

function isVbscriptOffset(parsed: AspParsedDocument, offset: number): boolean {
  return findRegionAt(parsed, offset)?.language === "vbscript";
}

function addRangeSemanticToken(
  tokens: SemanticTokenData[],
  document: TextDocument,
  range: Range,
  tokenType: string,
  rangeStart: number,
  rangeEnd: number,
): void {
  const offset = document.offsetAt(range.start);
  if (offset < rangeStart || offset > rangeEnd || range.start.line !== range.end.line) {
    return;
  }
  addSemanticToken(tokens, document, offset, document.offsetAt(range.end) - offset, tokenType);
}

function dedupeSemanticTokens(tokens: SemanticTokenData[]): SemanticTokenData[] {
  const seen = new Set<string>();
  return tokens.filter((token) => {
    const key = `${token.line}:${token.character}:${token.length}:${token.tokenType}:${semanticTokenModifierBitset(token.tokenModifiers)}`;
    if (seen.has(key)) {
      return false;
    }
    seen.add(key);
    return true;
  });
}

function semanticTokenModifierBitset(modifiers: readonly string[] | undefined): number {
  let bitset = 0;
  for (const modifier of modifiers ?? []) {
    const index = semanticTokenModifiers.indexOf(
      modifier as (typeof semanticTokenModifiers)[number],
    );
    if (index !== -1) {
      bitset |= 1 << index;
    }
  }
  return bitset;
}

function cacheSemanticTokens(uri: string, data: number[]): SemanticTokens {
  const previous = latestSemanticTokenResultByUri.get(uri);
  if (previous) {
    semanticTokenResults.delete(previous);
  }
  const resultId = nextSemanticTokenResultId();
  semanticTokenResults.set(resultId, { uri, data });
  latestSemanticTokenResultByUri.set(uri, resultId);
  return { data, resultId };
}

function nextSemanticTokenResultId(): string {
  semanticTokenResultCounter += 1;
  return String(semanticTokenResultCounter);
}

function semanticTokenDeltaEdit(previous: number[], next: number[]) {
  let prefix = 0;
  while (prefix < previous.length && prefix < next.length && previous[prefix] === next[prefix]) {
    prefix += 1;
  }
  let previousSuffix = previous.length;
  let nextSuffix = next.length;
  while (
    previousSuffix > prefix &&
    nextSuffix > prefix &&
    previous[previousSuffix - 1] === next[nextSuffix - 1]
  ) {
    previousSuffix -= 1;
    nextSuffix -= 1;
  }
  return {
    start: prefix,
    deleteCount: previousSuffix - prefix,
    data: next.slice(prefix, nextSuffix),
  };
}

function clearSemanticTokensForUri(uri: string): void {
  const resultId = latestSemanticTokenResultByUri.get(uri);
  if (resultId) {
    semanticTokenResults.delete(resultId);
    latestSemanticTokenResultByUri.delete(uri);
  }
}

function addEmbeddedSemanticTokens(
  tokens: SemanticTokenData[],
  cached: CachedDocument,
  rangeStart: number,
  rangeEnd: number,
): void {
  const html = getCachedVirtual(cached, "html");
  if (html && virtualOverlapsSourceRange(html, rangeStart, rangeEnd)) {
    const pattern = /<\/?\s*([A-Za-z][A-Za-z0-9:-]*)/g;
    for (const slice of virtualSearchSlicesForSourceRange(html, rangeStart, rangeEnd)) {
      pattern.lastIndex = 0;
      let match: RegExpExecArray | null;
      while ((match = pattern.exec(slice.text))) {
        addVirtualWordToken(
          tokens,
          cached.source,
          cached,
          html,
          slice.start + match.index + match[0].lastIndexOf(match[1]),
          match[1].length,
          "keyword",
          rangeStart,
          rangeEnd,
        );
      }
    }
  }
  const css = getCachedVirtual(cached, "css");
  if (css && virtualOverlapsSourceRange(css, rangeStart, rangeEnd)) {
    const pattern = /\b([A-Za-z-]+)\s*:/g;
    for (const slice of virtualSearchSlicesForSourceRange(css, rangeStart, rangeEnd)) {
      pattern.lastIndex = 0;
      let match: RegExpExecArray | null;
      while ((match = pattern.exec(slice.text))) {
        if (match.index !== undefined) {
          addVirtualWordToken(
            tokens,
            cached.source,
            cached,
            css,
            slice.start + match.index,
            match[1].length,
            "property",
            rangeStart,
            rangeEnd,
          );
        }
      }
    }
  }
  for (const virtual of jsVirtualDocuments(cached)) {
    if (!virtualOverlapsSourceRange(virtual, rangeStart, rangeEnd)) {
      continue;
    }
    const project = createJsLanguageService(virtual, cachedSettings(cached.source.uri));
    addJavaScriptSemanticTokens(
      tokens,
      cached,
      virtual,
      project.service,
      jsProjectFileName(virtual, project),
      rangeStart,
      rangeEnd,
    );
  }
}

function virtualSearchSlicesForSourceRange(
  virtual: VirtualDocument,
  rangeStart: number,
  rangeEnd: number,
): Array<{ start: number; text: string }> {
  return virtualSpansForSourceRange(virtual, rangeStart, rangeEnd).map((span) => {
    const start = Math.max(0, span.start - 128);
    const end = Math.min(virtual.text.length, span.start + span.length + 128);
    return {
      start,
      text: virtual.text.slice(start, end),
    };
  });
}

function addJavaScriptSemanticTokens(
  tokens: SemanticTokenData[],
  cached: CachedDocument,
  virtual: VirtualDocument,
  service: ts.LanguageService,
  fileName: string,
  rangeStart: number,
  rangeEnd: number,
): void {
  for (const span of virtualSpansForSourceRange(virtual, rangeStart, rangeEnd)) {
    const spans = service.getEncodedSemanticClassifications(
      fileName,
      span,
      ts.SemanticClassificationFormat.TwentyTwenty,
    ).spans;
    for (let index = 0; index + 2 < spans.length; index += 3) {
      const token = jsSemanticTokenFromClassification(spans[index + 2]);
      if (!token) {
        continue;
      }
      addVirtualWordToken(
        tokens,
        cached.source,
        cached,
        virtual,
        spans[index],
        spans[index + 1],
        token.tokenType,
        rangeStart,
        rangeEnd,
        token.tokenModifiers,
      );
    }
  }
}

function virtualOverlapsSourceRange(
  virtual: VirtualDocument,
  rangeStart: number,
  rangeEnd: number,
): boolean {
  return virtual.sourceMap.segments.some(
    (segment) => segment.sourceEnd >= rangeStart && segment.sourceStart <= rangeEnd,
  );
}

function virtualSpansForSourceRange(
  virtual: VirtualDocument,
  rangeStart: number,
  rangeEnd: number,
): Array<{ start: number; length: number }> {
  const lastSourceEnd = virtual.sourceMap.segments.at(-1)?.sourceEnd ?? -1;
  if (rangeStart <= 0 && rangeEnd >= lastSourceEnd) {
    return [{ start: 0, length: virtual.text.length }];
  }
  const spans: Array<{ start: number; length: number }> = [];
  for (const segment of virtual.sourceMap.segments) {
    const sourceStart = Math.max(segment.sourceStart, rangeStart);
    const sourceEnd = Math.min(segment.sourceEnd, rangeEnd);
    if (sourceStart > sourceEnd) {
      continue;
    }
    const virtualStart = segment.virtualStart + (sourceStart - segment.sourceStart);
    const virtualEnd = segment.virtualStart + (sourceEnd - segment.sourceStart);
    spans.push({ start: virtualStart, length: Math.max(1, virtualEnd - virtualStart + 1) });
  }
  return spans.length > 0 ? spans : [{ start: 0, length: virtual.text.length }];
}

function jsSemanticTokenFromClassification(
  classification: number,
): { tokenType: string; tokenModifiers?: readonly string[] } | undefined {
  const typeIndex = (classification >> 8) - 1;
  const modifierSet = classification & 255;
  const tokenType = jsSemanticTokenType(typeIndex);
  if (!tokenType) {
    return undefined;
  }
  const tokenModifiers = jsSemanticTokenModifiers(modifierSet);
  return { tokenType, tokenModifiers };
}

function jsSemanticTokenType(typeIndex: number): string | undefined {
  switch (typeIndex) {
    case 0:
      return "class";
    case 1:
      return "enum";
    case 2:
      return "interface";
    case 3:
      return "namespace";
    case 4:
      return "typeParameter";
    case 5:
      return "typeAlias";
    case 6:
      return "parameter";
    case 7:
      return "variable";
    case 8:
      return "enumMember";
    case 9:
      return "property";
    case 10:
      return "function";
    case 11:
      return "method";
    default:
      return undefined;
  }
}

function jsSemanticTokenModifiers(modifierSet: number): string[] | undefined {
  const modifiers: string[] = [];
  if (modifierSet & (1 << 3)) {
    modifiers.push("readonly");
  }
  if (modifierSet & (1 << 4)) {
    modifiers.push("library");
  }
  return modifiers.length > 0 ? modifiers : undefined;
}

function addVirtualWordToken(
  tokens: SemanticTokenData[],
  document: TextDocument,
  cached: CachedDocument,
  virtual: VirtualDocument,
  virtualOffset: number,
  length: number,
  tokenType: string,
  rangeStart: number,
  rangeEnd: number,
  tokenModifiers?: readonly string[],
): void {
  const sourceOffset = virtual.sourceMap.toSourceOffset(virtualOffset);
  const sourceEndOffset = virtual.sourceMap.toSourceOffset(virtualOffset + length - 1);
  if (
    !virtualRangeStaysWithinSegment(virtual, virtualOffset, virtualOffset + length) ||
    sourceOffset === undefined ||
    sourceEndOffset === undefined ||
    sourceOffset < rangeStart ||
    sourceOffset > rangeEnd ||
    !isVirtualTokenSourceRegion(cached, sourceOffset, sourceEndOffset, virtual.languageId)
  ) {
    return;
  }
  addSemanticToken(
    tokens,
    document,
    sourceOffset,
    sourceEndOffset - sourceOffset + 1,
    tokenType,
    tokenModifiers,
  );
}

function isVirtualTokenSourceRegion(
  cached: CachedDocument,
  sourceStart: number,
  sourceEnd: number,
  languageId: VirtualDocument["languageId"],
): boolean {
  const startRegion = findRegionAt(cached.parsed, sourceStart);
  const endRegion = findRegionAt(cached.parsed, sourceEnd);
  return Boolean(
    startRegion && endRegion && startRegion === endRegion && startRegion.language === languageId,
  );
}

function sourceOffsetFromVirtualPoint(
  virtual: VirtualDocument,
  virtualOffset: number,
): number | undefined {
  const segment = sourceMapSegmentAtVirtualOffset(virtual, virtualOffset);
  return segment ? segment.sourceStart + (virtualOffset - segment.virtualStart) : undefined;
}

function sourceMapSegmentAtVirtualOffset(virtual: VirtualDocument, virtualOffset: number) {
  const segments = virtual.sourceMap.segments;
  let low = 0;
  let high = segments.length - 1;
  while (low <= high) {
    const middle = Math.floor((low + high) / 2);
    const segment = segments[middle];
    if (virtualOffset < segment.virtualStart) {
      high = middle - 1;
    } else if (virtualOffset >= segment.virtualEnd) {
      low = middle + 1;
    } else {
      return segment;
    }
  }
  return undefined;
}

function addSemanticToken(
  tokens: SemanticTokenData[],
  document: TextDocument,
  offset: number,
  length: number,
  tokenType: string,
  tokenModifiers?: readonly string[],
): void {
  const position = document.positionAt(offset);
  tokens.push({
    line: position.line,
    character: position.character,
    length,
    tokenType,
    tokenModifiers,
  });
}

function isDiagnostic(value: Diagnostic | undefined): value is Diagnostic {
  return value !== undefined;
}

connection.onShutdown(async () => {
  cancelBackgroundAnalysis();
  await vbDiagnosticsWorkerPool?.close();
  vbDiagnosticsWorkerPool = undefined;
});

documents.listen(connection);
connection.listen();
