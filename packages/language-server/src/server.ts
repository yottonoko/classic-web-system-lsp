#!/usr/bin/env node
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { JsDiagnosticsWorkerPool } from "./js-worker-pool";
import { VbDiagnosticsWorkerPool } from "./vb-worker-pool";
import {
  DiskAnalysisCache,
  type DiskAnalysisBuilderState,
  type DiskIncludeRefsCacheEntry,
  type DiskAnalysisSourceMetadata,
  type DiskSummaryCacheEntry,
  type DiskVbSymbolIndexCacheEntry,
} from "./disk-analysis-cache";
import type {
  JsDiagnosticsWorkerResponse,
  JsDiagnosticsWorkerVirtualDocument,
} from "./js-diagnostics-protocol";
import type {
  VbDiagnosticsWorkerContext,
  VbDiagnosticsWorkerDocument,
  VbDiagnosticsWorkerResponse,
} from "./vb-diagnostics-protocol";
import { analyse, detect } from "chardet";
import {
  CodeActionKind,
  CompletionItemKind,
  createConnection,
  DiagnosticSeverity,
  DiagnosticTag,
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
  analyzeVbscriptFromTextAsync,
  buildVbTypeEnvironment,
  buildVirtualDocuments,
  collectVbscriptSymbols,
  collectVbscriptSymbolsAsync,
  createLocalizer,
  extractAspIncludeRefs,
  extractVbscriptSymbolIndex,
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
  getVbscriptReferencesForSymbol,
  getVbscriptGraphExternalSymbols,
  getVbscriptSelectionRanges,
  getVbscriptSemanticTokens,
  getVbscriptSignatureHelp,
  getVbscriptTypeDefinition,
  hydrateVbscriptCst,
  parseAspDocument,
  parseAspDocumentAsync,
  parseAspDocumentSkeletonAsync,
  parseVbscriptTypeRef,
  prepareVbscriptCallHierarchy,
  resolveVbscriptCompletionItem,
  shiftAspRangeAfterChange,
  summarizeAspFileAnalysisAsync,
  summarizeAspFileAnalysisFromTextAsync,
  updateAspParsedDocument,
  updateAspParsedDocumentSkeletonAsync,
  type AspFormattingOptions,
  type AspEmbeddedLanguage,
  type AspEditImpact,
  type AspIncrementalChange,
  type AspInclude,
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
  type VbReference,
  type VbReferenceOptions,
  type VbSymbol,
  type VbSymbolIndex,
  type VbSymbolKind,
  type VbToken,
  type VbType,
  type VbTypeEnvironment,
  type VbGraphExternalSymbol,
} from "@asp-lsp/core";
import { getCSSLanguageService, type Stylesheet } from "vscode-css-languageservice";
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
const jsFileExistsCache = new Map<string, boolean>();
const jsReadFileCache = new Map<string, string | undefined>();
const jsDirectoryExistsCache = new Map<string, boolean>();
const jsDirectoriesCache = new Map<string, string[]>();
const jsReadDirectoryCache = new Map<string, string[]>();
const jsRealpathCache = new Map<string, string>();
const jsFileStatCache = new Map<string, JsFileStat | undefined>();
const graphFileIndexCache = new Map<string, GraphFileIndex>();
const graphFileIndexInFlight = new Map<string, Promise<GraphFileIndex>>();
const semanticTokenResults = new Map<string, { uri: string; data: number[] }>();
const latestSemanticTokenResultByUri = new Map<string, string>();
const pendingSemanticJavascriptTokenBuilds = new Map<string, Promise<void>>();
const regionIndexes = new WeakMap<AspParsedDocument, RegionIndex>();
const defaultMaxIndexFiles = 5000;
const defaultScanChunkSize = 200;
const defaultDiagnosticsDebounceMs = 250;
const graphFileIndexCacheMaxEntries = 64;
const reindexWorkspaceCommand = "aspLsp.reindexWorkspace";
const clearCacheCommand = "aspLsp.clearCache";
const clearDiskCacheCommand = "aspLsp.clearDiskCache";
const clearProcessCacheCommand = "aspLsp.clearProcessCache";
const reindexWorkspaceServerCommand = "aspLsp.server.reindexWorkspace";
const clearCacheServerCommand = "aspLsp.server.clearCache";
const clearDiskCacheServerCommand = "aspLsp.server.clearDiskCache";
const clearProcessCacheServerCommand = "aspLsp.server.clearProcessCache";
const buildGraphServerCommand = "aspLsp.server.buildGraph";
const languageServerVersion = "0.3.10";
const projectUpdateDelayMs = 250;
const openFileProjectMaintenanceDelayMs = 2_500;
const semanticTokensLargeSourceThreshold = internalTestThreshold(
  "ASP_LSP_TEST_SEMANTIC_TOKENS_LARGE_SOURCE_THRESHOLD",
  1024 * 1024,
);
const semanticTokensLargeJavascriptThreshold = internalTestThreshold(
  "ASP_LSP_TEST_SEMANTIC_TOKENS_LARGE_JAVASCRIPT_THRESHOLD",
  128 * 1024,
);
let globalSettings: AspSettings = { defaultLanguage: "VBScript", checkJs: false };
let workspaceRoots: string[] = [];
let clientLocale = "en";
let workspaceIndexDirty = true;
let workspaceIndexTruncated = false;
let workspaceVbReferenceIndex: WorkspaceVbReferenceIndex | undefined;
let jsLanguageServiceCacheTick = 0;
let lightweightJsUnusedCacheTick = 0;
let jsScriptSnapshotSequence = 0;
let semanticTokenResultCounter = 0;
let semanticTokensRefreshSupported = false;
let inlayHintRefreshSupported = false;
let documentCacheGeneration = 0;
let workspaceGeneration = 0;
let includeResolutionGeneration = 0;
let jsProjectGeneration = 0;
let diskAnalysisCache = createDiskAnalysisCache(globalSettings);
let lastForegroundActivityAt = 0;
let projectUpdateTimer: ReturnType<typeof setTimeout> | undefined;
let openFileProjectMaintenanceTimer: ReturnType<typeof setTimeout> | undefined;
let pendingProjectUpdateReason: string | undefined;
let pendingOpenFileMaintenanceReason: string | undefined;
let jsDiagnosticsWorkerPool: JsDiagnosticsWorkerPool | undefined;
let jsDiagnosticsWorkerRequestId = 0;
const tsUnusedDiagnosticCodes = new Set([6133, 6138, 6192, 6196, 6198]);
const maxLightweightJsUnusedCacheEntries = 32;
const lightweightJsUnusedDiagnosticsCache = new Map<string, LightweightJsUnusedCacheEntry>();
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
  "constant",
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

interface CachedCssContext {
  key: string;
  virtual: VirtualDocument;
  document: TextDocument;
  stylesheet: Stylesheet;
}

interface CachedAnalysis {
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
}

interface CachedSemanticTokensEntry {
  key: string;
  data: number[];
}

interface CachedSemanticJavascriptTokensEntry {
  key: string;
  tokens: SemanticTokenData[];
}

interface EmbeddedSemanticTokenOptions {
  settings: AspSettings;
  jsVirtuals: VirtualDocument[];
  deferLargeJavascript: boolean;
  javascriptCacheKey: string;
}

interface DocumentIdentity {
  uri: string;
  version: number;
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

interface InFlightDocumentRefresh {
  identity: DocumentIdentity;
  parseSettingsIdentity: string;
  promise: Promise<CachedDocument>;
}

interface WatchedAspFileChange {
  fileName: string;
  type: FileChangeType;
}

interface IncludeStateRefreshResult {
  includeRefsChangedFiles: Set<string>;
  publicChangedFiles: Set<string>;
}

interface DiagnosticCacheEntry {
  key: string;
  items: Diagnostic[];
  text: string;
}

interface CachedTsDiagnostic {
  code: number;
  category: ts.DiagnosticCategory;
  messageText: string;
  start?: number;
  length?: number;
  reportsUnnecessary?: boolean;
}

interface CachedJsDiagnostic {
  diagnostic: CachedTsDiagnostic;
  severity?: DiagnosticSeverity;
  source?: string;
}

interface CachedJsVirtualDiagnostics {
  virtualKey: string;
  diagnostics: CachedJsDiagnostic[];
}

interface CachedJsDiagnosticsEntry {
  key: string;
  virtuals: CachedJsVirtualDiagnostics[];
}

interface LightweightJsUnusedCacheEntry {
  diagnostics: CachedTsDiagnostic[];
  lastUsed: number;
}

interface TsDiagnosticLike {
  code: number;
  category: ts.DiagnosticCategory;
  messageText: string | ts.DiagnosticMessageChain;
  start?: number;
  length?: number;
  reportsUnnecessary?: unknown;
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

type AnalysisExecutionMode = "foreground" | "workspace";

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

interface JsFileStat {
  mtimeMs: number;
  size: number;
  isFile: boolean;
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

interface IncludeSummaryCacheEntry {
  key: string;
  fileName: string;
  uri: string;
  source: DiskAnalysisSourceMetadata;
  summary: FileAnalysisSummary;
  publicFingerprint: string;
  publicSignature: FilePublicSignature;
  parsed?: AspParsedDocument;
}

interface IncludeRefsCacheEntry {
  key: string;
  fileName: string;
  uri: string;
  source: DiskAnalysisSourceMetadata;
  includeRefs: AspInclude[];
  fingerprint: string;
}

interface IncludeDocumentCacheEntry extends IncludeSummaryCacheEntry {
  parsed: AspParsedDocument;
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
  summaryUris: string[];
  summaries: FileAnalysisSummary[];
  summaryGraphKey: string;
  complete: boolean;
  symbols: VbSymbol[];
  typeEnvironment: VbTypeEnvironment;
  externalRefUsages: VbExternalRefUsage[];
}

interface VbProjectContextBuildOptions {
  allowReadMissing: boolean;
}

interface VbProjectSummaryGraph {
  rootSummary: FileAnalysisSummary;
  summaries: FileAnalysisSummary[];
  documents: AspParsedDocument[];
  key: string;
  complete: boolean;
  missingFiles: string[];
  truncatedReason?: string;
  textLength: number;
}

interface WorkspaceVbReferenceSummary {
  uri: string;
  fileName: string;
  summary: FileAnalysisSummary;
}

interface WorkspaceVbReferenceIndex {
  key: string;
  summaries: WorkspaceVbReferenceSummary[];
  byUsageKey: Map<string, WorkspaceVbReferenceSummary[]>;
  byMemberName: Map<string, WorkspaceVbReferenceSummary[]>;
  lastUsed: number;
}

interface VbReferenceCodeLensData {
  kind: "vbscript-reference";
  uri: string;
  name: string;
  symbolKind: VbSymbolKind;
  memberOf?: string;
  line: number;
  character: number;
}

type AspGraphScope = "document" | "workspace";

type AspGraphNodeKind = "file" | "vbDeclaration" | "vbUnresolved";

type AspGraphNodeOrigin = "source" | "builtin" | "configured";

type AspGraphExternalKind = "function" | "constant" | "object" | "member" | "event";

type AspGraphLinkKind = "include" | "declares" | "references" | "calls" | "unresolvedReference";

interface AspGraphNode {
  id: string;
  kind: AspGraphNodeKind;
  label: string;
  uri?: string;
  fileName?: string;
  range?: Range;
  exists?: boolean;
  declarationKind?: string;
  role?: string;
  memberOf?: string;
  bindingScope?: string;
  group?: string;
  origin?: AspGraphNodeOrigin;
  externalKind?: AspGraphExternalKind;
}

interface AspGraphLink {
  id: string;
  source: string;
  target: string;
  kind: AspGraphLinkKind;
  label: string;
  role?: string;
  count: number;
  ranges: Array<{ uri: string; range: Range }>;
  include?: {
    path: string;
    mode: AspInclude["mode"];
    exists: boolean;
    resolvedUri: string;
    actualPath?: string;
    pathCaseMatches?: boolean;
  };
}

interface AspGraphPayload {
  scope: AspGraphScope;
  rootUri?: string;
  nodes: AspGraphNode[];
  links: AspGraphLink[];
  stats: {
    files: number;
    declarations: number;
    references: number;
    calls: number;
    unresolvedReferences: number;
    includes: number;
    missingIncludes: number;
    nodes: number;
    links: number;
  };
  truncated?: {
    reason: string;
  };
}

interface AspGraphDocument {
  uri: string;
  fileName: string;
  text: string;
  source: DiskAnalysisSourceMetadata;
  diskBacked: boolean;
}

interface GraphFileIndex {
  key: string;
  uri: string;
  fileName: string;
  source: DiskAnalysisSourceMetadata;
  includeRefs: AspInclude[];
  vbSymbolIndex: VbSymbolIndex;
  fingerprint: string;
  lastUsed: number;
}

interface AspGraphBuildState {
  nodes: Map<string, AspGraphNode>;
  links: Map<string, AspGraphLink>;
  declarations: Set<string>;
  externalSymbols: AspGraphExternalIndex;
  stats: AspGraphPayload["stats"];
  truncated?: AspGraphPayload["truncated"];
}

interface AspGraphExternalIndex {
  byName: Map<string, VbGraphExternalSymbol[]>;
  memberByOwnerAndName: Map<string, VbGraphExternalSymbol>;
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
  contextIdentity?: string;
  prefix: string;
  offset: number;
  documentVersion: number;
  items: CompletionItem[];
}

interface CachedVbProjectContextLookup {
  key: string;
  context: VbProjectContext;
}

interface VbProjectContextLimits {
  maxDocuments: number;
  maxTextLength: number;
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

  updateIncludeSummary(entry: IncludeSummaryCacheEntry, settings: AspSettings): void {
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
    contextIdentity?: string,
  ): CompletionItem[] | undefined {
    const entry = this.entries.get(this.baseKey(cached, settings, region));
    if (
      !entry ||
      entry.uri !== cached.source.uri ||
      entry.language !== region.language ||
      entry.contextIdentity !== contextIdentity
    ) {
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
    contextIdentity?: string,
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
      contextIdentity,
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

  clearUris(uris: Set<string>, reason: string): void {
    let removed = 0;
    for (const [key, entry] of this.entries) {
      if (!uris.has(entry.uri)) {
        continue;
      }
      this.entries.delete(key);
      removed += 1;
    }
    if (removed > 0) {
      logDebugSummary(
        globalSettings,
        `[asp-lsp] completion.cache.invalidate: reason=${reason}, files=${uris.size}, entries=${removed}`,
      );
    }
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
  private readonly summaryCache = new Map<string, IncludeSummaryCacheEntry>();
  private readonly includeRefsCache = new Map<string, IncludeRefsCacheEntry>();
  private readonly inFlight = new Map<string, Promise<IncludeDocumentCacheEntry | undefined>>();
  private readonly summaryInFlight = new Map<
    string,
    Promise<IncludeSummaryCacheEntry | undefined>
  >();
  private readonly includeRefsInFlight = new Map<
    string,
    Promise<IncludeRefsCacheEntry | undefined>
  >();
  private readonly generations = new Map<string, number>();

  async readAsync(
    fileName: string,
    settings: AspSettings,
  ): Promise<IncludeDocumentCacheEntry | undefined> {
    const normalized = normalizeFileName(fileName);
    const identity = await includeDocumentSourceIdentityAsync(normalized, settings);
    if (!identity) {
      return undefined;
    }
    const { key, source, text, diskBacked } = identity;
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
        const nextText = text ?? (await readTextFileAsync(normalized, settings.legacyEncoding));
        const entry = await createIncludeDocumentCacheEntryAsync(
          normalized,
          nextText,
          settings,
          key,
          source,
        );
        if (this.generation(normalized) === generation) {
          this.cache.set(normalized, entry);
          this.summaryCache.set(normalized, entry);
          this.includeRefsCache.set(normalized, includeRefsCacheEntryFromSummary(entry, settings));
          rememberIncludePublicSummary(entry, settings);
          if (diskBacked) {
            void diskAnalysisCache
              .writeSummary(diskSummaryCacheEntry(entry, settings))
              .catch((error) => logDiskAnalysisCacheError("diskSummary.write", error));
          }
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

  async readSummaryAsync(
    fileName: string,
    settings: AspSettings,
    options: { allowRead?: boolean } = {},
  ): Promise<IncludeSummaryCacheEntry | undefined> {
    const normalized = normalizeFileName(fileName);
    const identity = await includeDocumentSourceIdentityAsync(normalized, settings);
    if (!identity) {
      return undefined;
    }
    const { key, source, text, diskBacked } = identity;
    const existing = this.summaryCache.get(normalized);
    if (existing?.key === key) {
      return existing;
    }
    const existingDocument = this.cache.get(normalized);
    if (existingDocument?.key === key) {
      this.summaryCache.set(normalized, existingDocument);
      return existingDocument;
    }
    const baseInFlightKey = `${normalized}:${key}`;
    const readAllowed = options.allowRead !== false;
    const readInFlightKey = `${baseInFlightKey}:read`;
    const noReadInFlightKey = `${baseInFlightKey}:no-read`;
    const inFlightKey = readAllowed ? readInFlightKey : noReadInFlightKey;
    const pending = readAllowed
      ? this.summaryInFlight.get(readInFlightKey)
      : (this.summaryInFlight.get(readInFlightKey) ?? this.summaryInFlight.get(noReadInFlightKey));
    if (pending) {
      return pending;
    }
    const generation = this.generation(normalized);
    let promise: Promise<IncludeSummaryCacheEntry | undefined> | undefined;
    promise = (async () => {
      try {
        if (diskBacked) {
          const cachedSummary = await diskAnalysisCache
            .readSummary({ source, settingsKey: includeSummarySettingsKey(settings) })
            .catch((error) => {
              logDiskAnalysisCacheError("diskSummary.read", error);
              return undefined;
            });
          if (cachedSummary) {
            const entry = includeSummaryCacheEntryFromDisk(normalized, key, cachedSummary);
            if (this.generation(normalized) === generation) {
              this.summaryCache.set(normalized, entry);
              this.includeRefsCache.set(
                normalized,
                includeRefsCacheEntryFromSummary(entry, settings),
              );
              rememberIncludePublicSummary(entry, settings);
              logDebugSummary(settings, `[asp-lsp] diskSummary.hit: ${entry.uri}`);
            }
            return entry;
          }
          logDebugSummary(settings, `[asp-lsp] diskSummary.miss: ${pathToFileUri(normalized)}`);
        }
        if (!readAllowed) {
          return undefined;
        }
        const nextText = text ?? (await readTextFileAsync(normalized, settings.legacyEncoding));
        const entry = await createIncludeDocumentCacheEntryAsync(
          normalized,
          nextText,
          settings,
          key,
          source,
        );
        if (this.generation(normalized) === generation) {
          this.cache.set(normalized, entry);
          this.summaryCache.set(normalized, entry);
          this.includeRefsCache.set(normalized, includeRefsCacheEntryFromSummary(entry, settings));
          rememberIncludePublicSummary(entry, settings);
          if (diskBacked) {
            await diskAnalysisCache.writeSummary(diskSummaryCacheEntry(entry, settings));
            logDebugSummary(settings, `[asp-lsp] diskSummary.write: ${entry.uri}`);
          }
        }
        return entry;
      } finally {
        if (this.summaryInFlight.get(inFlightKey) === promise) {
          this.summaryInFlight.delete(inFlightKey);
        }
      }
    })();
    this.summaryInFlight.set(inFlightKey, promise);
    return promise;
  }

  async readIncludeRefsAsync(
    fileName: string,
    settings: AspSettings,
    options: { allowRead?: boolean } = {},
  ): Promise<IncludeRefsCacheEntry | undefined> {
    const normalized = normalizeFileName(fileName);
    const identity = await includeDocumentSourceIdentityAsync(normalized, settings);
    if (!identity) {
      return undefined;
    }
    const { source, text, diskBacked } = identity;
    const key = includeRefsCacheKey(normalized, source, settings);
    const existing = this.includeRefsCache.get(normalized);
    if (existing?.key === key) {
      return existing;
    }
    const existingSummary = this.summaryCache.get(normalized);
    if (existingSummary && sameDiskAnalysisSource(existingSummary.source, source)) {
      const entry = includeRefsCacheEntryFromSummary(existingSummary, settings);
      this.includeRefsCache.set(normalized, entry);
      return entry;
    }
    const readAllowed = options.allowRead !== false;
    const readInFlightKey = `${normalized}:${key}:read`;
    const noReadInFlightKey = `${normalized}:${key}:no-read`;
    const inFlightKey = readAllowed ? readInFlightKey : noReadInFlightKey;
    const pending = readAllowed
      ? this.includeRefsInFlight.get(readInFlightKey)
      : (this.includeRefsInFlight.get(readInFlightKey) ??
        this.includeRefsInFlight.get(noReadInFlightKey));
    if (pending) {
      return pending;
    }
    const generation = this.generation(normalized);
    let promise: Promise<IncludeRefsCacheEntry | undefined> | undefined;
    promise = (async () => {
      try {
        if (diskBacked) {
          const cachedRefs = await diskAnalysisCache
            .readIncludeRefs({ source, settingsKey: includeRefsSettingsKey(settings) })
            .catch((error) => {
              logDiskAnalysisCacheError("diskIncludeRefs.read", error);
              return undefined;
            });
          if (cachedRefs) {
            const entry = includeRefsCacheEntryFromDisk(normalized, key, cachedRefs);
            if (this.generation(normalized) === generation) {
              this.includeRefsCache.set(normalized, entry);
              logDebugSummary(settings, `[asp-lsp] diskIncludeRefs.hit: ${entry.uri}`);
            }
            return entry;
          }
          logDebugSummary(settings, `[asp-lsp] diskIncludeRefs.miss: ${pathToFileUri(normalized)}`);
        }
        if (!readAllowed) {
          return undefined;
        }
        const nextText = text ?? (await readTextFileAsync(normalized, settings.legacyEncoding));
        const entry = createIncludeRefsCacheEntry(normalized, nextText, key, source);
        if (this.generation(normalized) === generation) {
          this.includeRefsCache.set(normalized, entry);
          if (diskBacked) {
            await diskAnalysisCache.writeIncludeRefs(diskIncludeRefsCacheEntry(entry, settings));
            logDebugSummary(settings, `[asp-lsp] diskIncludeRefs.write: ${entry.uri}`);
          }
        }
        return entry;
      } finally {
        if (this.includeRefsInFlight.get(inFlightKey) === promise) {
          this.includeRefsInFlight.delete(inFlightKey);
        }
      }
    })();
    this.includeRefsInFlight.set(inFlightKey, promise);
    return promise;
  }

  cachedIncludeRefs(fileName: string): IncludeRefsCacheEntry | undefined {
    return this.includeRefsCache.get(normalizeFileName(fileName));
  }

  cachedPublicSummary(fileName: string): IncludePublicSummaryState | undefined {
    return includePublicSummaries.get(normalizeFileName(fileName));
  }

  invalidateFiles(fileNames: Iterable<string>): void {
    for (const fileName of fileNames) {
      const normalized = normalizeFileName(fileName);
      this.generations.set(normalized, this.generation(normalized) + 1);
      this.cache.delete(normalized);
      this.summaryCache.delete(normalized);
      this.includeRefsCache.delete(normalized);
      for (const key of this.inFlight.keys()) {
        if (key.startsWith(`${normalized}:`)) {
          this.inFlight.delete(key);
        }
      }
      for (const key of this.summaryInFlight.keys()) {
        if (key.startsWith(`${normalized}:`)) {
          this.summaryInFlight.delete(key);
        }
      }
      for (const key of this.includeRefsInFlight.keys()) {
        if (key.startsWith(`${normalized}:`)) {
          this.includeRefsInFlight.delete(key);
        }
      }
    }
  }

  clear(): void {
    this.cache.clear();
    this.summaryCache.clear();
    this.includeRefsCache.clear();
    this.inFlight.clear();
    this.summaryInFlight.clear();
    this.includeRefsInFlight.clear();
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
const inFlightDocumentRefreshes = new Map<string, InFlightDocumentRefresh>();
const vbProjectContextCache = new Map<string, VbProjectContextCacheEntry>();
const includeDocumentLoader = new IncludeDocumentLoader();
const pendingIncludeSummaryRefreshes = new Map<string, Promise<void>>();
const aspProjectBuilderState = new AspProjectBuilderState();
const completionSessionCache = new CompletionSessionCache();
const maxVbProjectContextCacheEntries = 32;
let vbDiagnosticsWorkerPool: VbDiagnosticsWorkerPool | undefined;
let vbDiagnosticsWorkerRequestId = 0;
let stagedDiagnosticsGeneration = 0;

connection.onInitialize((params: InitializeParams): InitializeResult => {
  clientLocale = typeof params.locale === "string" ? params.locale : "en";
  semanticTokensRefreshSupported =
    params.capabilities.workspace?.semanticTokens?.refreshSupport === true;
  inlayHintRefreshSupported = params.capabilities.workspace?.inlayHint?.refreshSupport === true;
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
        commands: [
          reindexWorkspaceServerCommand,
          clearCacheServerCommand,
          clearDiskCacheServerCommand,
          clearProcessCacheServerCommand,
          buildGraphServerCommand,
        ],
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
  void measureDebugStepAsync(
    settings,
    event.document.uri,
    "documentChange.scheduleDiagnostics",
    () => scheduleDiagnosticsAsync(event.document),
  )
    .then((cached) => {
      if (
        documents.get(event.document.uri)?.version === cached.identity.version &&
        shouldScheduleProjectUpdateForDocumentChange(cached)
      ) {
        scheduleProjectUpdate("document.change");
      }
    })
    .catch((error: unknown) =>
      connection.console.warn(
        `[asp-lsp] documentChange.scheduleDiagnostics.failed: ${errorMessage(error)}`,
      ),
    );
});
documents.onDidSave(async (event) => {
  noteForegroundActivity();
  await indexWorkspaceFileAsync(uriToFileName(event.document.uri));
  invalidateCachedAnalysisForUris(new Set([event.document.uri]), "document.save");
  scheduleProjectUpdate("document.save");
  validate(event.document);
});
documents.onDidClose((event) => {
  cancelScheduledDiagnostics(event.document.uri);
  documentOpenContentVersions.delete(event.document.uri);
  pendingDocumentChanges.delete(event.document.uri);
  inFlightDocumentRefreshes.delete(event.document.uri);
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
  },
);

connection.onDidChangeConfiguration((change) => {
  const previousSettingsByUri = currentOpenDocumentSettingsByUri();
  const incoming = readSettingsFromChange(change.settings);
  if (incoming) {
    globalSettings = normalizeSettings(incoming);
  }
  void configureDiskAnalysisCacheAsync().catch((error) =>
    logDiskAnalysisCacheError("diskCache.configure", error),
  );
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
});

connection.onDidChangeWatchedFiles(async (change) => {
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
        await indexWorkspaceFileAsync(fileName);
      }
    }
    if (isScriptWorkspaceFile(fileName) || isJavaScriptProjectEnvironmentFile(fileName)) {
      scriptChanged = true;
    }
  }
  if (!aspChanged && !scriptChanged) {
    return;
  }
  let includeRefsChangedFiles = new Set<string>();
  let publicChangedFiles = new Set<string>();
  let graphChangedFiles = new Set<string>();
  if (aspChanged) {
    const refresh = await refreshIncludeStateForAspChangesAsync(aspChanges);
    includeRefsChangedFiles = refresh.includeRefsChangedFiles;
    publicChangedFiles = refresh.publicChangedFiles;
    graphChangedFiles = new Set([...includeRefsChangedFiles, ...publicChangedFiles]);
    if (graphChangedFiles.size > 0) {
      await ensureIncludeGraphForOpenDocumentsAsync(graphChangedFiles);
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
    : affectedOpenUrisForAspChanges(aspChanges, graphChangedFiles);
  invalidateCachedAnalysisForUris(
    affectedUris,
    scriptChanged ? "watchedScript.changed" : "watchedAsp.changed",
  );
  for (const document of documents.all().filter((item) => affectedUris.has(item.uri))) {
    validate(document);
  }
});

connection.workspace.onWillRenameFiles((params) => includeRenameWorkspaceEditAsync(params.files));

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
  runInteractiveLanguageFeature(async () => {
    const cached = await getFreshCachedAsync(params.textDocument.uri);
    if (!cached) {
      return [];
    }
    const settings = cachedSettings(cached.source.uri);
    const region = findRegionAt(cached.parsed, cached.source.offsetAt(params.position));
    if (!region) {
      return [];
    }
    const remember = (items: CompletionItem[], contextIdentity?: string): CompletionItem[] => {
      completionSessionCache.set(cached, settings, region, params.position, items, contextIdentity);
      return items;
    };
    if (region.language === "vbscript") {
      const warmedContext =
        cached.parsed.includes.length > 0
          ? await interactiveVbProjectContextLookupAsync(cached, settings)
          : cachedVbProjectContextLookup(cached, settings);
      const contextIdentity = cached.parsed.includes.length > 0 ? warmedContext?.key : undefined;
      const cachedCompletion = completionSessionCache.get(
        cached,
        settings,
        region,
        params.position,
        contextIdentity,
      );
      if (cachedCompletion) {
        return cachedCompletion;
      }
      const shouldCache = Boolean(warmedContext) || cached.parsed.includes.length === 0;
      const context =
        warmedContext?.context ??
        (await buildImmediateLocalVbProjectContextAsync(cached, settings));
      const completions = getVbscriptCompletions(cached.parsed, params.position, context);
      const items = withCompletionData(
        completions.length > 0
          ? completions
          : fallbackVbMemberCompletions(cached, params.position, context),
        { kind: "vbscript", uri: cached.source.uri },
      );
      return shouldCache ? remember(items, contextIdentity) : items;
    }
    const cachedCompletion = completionSessionCache.get(cached, settings, region, params.position);
    if (cachedCompletion) {
      return cachedCompletion;
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
      return remember(await jsCompletionAsync(cached, params));
    }
    return [];
  }),
);

connection.onCompletionResolve((item) =>
  runInteractiveLanguageFeature(async () => {
    const data = item.data as { kind?: string; uri?: string } | undefined;
    if (data?.kind === "vbscript" && data.uri) {
      const cached = await getFreshCachedAsync(data.uri);
      return cached
        ? resolveVbscriptCompletionItem(
            item,
            cached.parsed,
            withSourceUriFormatter(
              await interactiveVbProjectContextAsync(cached, cachedSettings(cached.source.uri)),
            ),
          )
        : item;
    }
    if (data?.kind === "javascript" && data.uri) {
      const resolved = await resolveJsCompletion(item, data.uri);
      return resolved ?? item;
    }
    if ((data?.kind === "html" || data?.kind === "css") && data.uri) {
      return resolveEmbeddedCompletion(item, data.kind);
    }
    return item;
  }),
);

connection.onHover((params) =>
  runInteractiveLanguageFeature(async () => {
    const cached = await getFreshCachedAsync(params.textDocument.uri);
    if (!cached) {
      return null;
    }
    const region = findRegionAt(cached.parsed, cached.source.offsetAt(params.position));
    if (!region) {
      return null;
    }
    if (region.language === "vbscript") {
      return aspHoverAsync(cached, params);
    }
    if (region && isJavaScriptLikeRegion(region)) {
      return jsHoverAsync(cached, params.position);
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
      const context = cssContext(cached);
      if (!context) {
        return null;
      }
      const { document, stylesheet, virtual } = context;
      const virtualPosition = virtual.sourceMap.toVirtualPosition(params.position);
      if (!virtualPosition) {
        return null;
      }
      return remapHover(virtual, cssService.doHover(document, virtualPosition, stylesheet));
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

connection.onReferences(async (params: ReferenceParams) => {
  const cached = await getFreshCachedAsync(params.textDocument.uri);
  if (!cached) {
    return [];
  }
  const region = findRegionAt(cached.parsed, cached.source.offsetAt(params.position));
  if (!region) {
    return [];
  }
  if (region && isJavaScriptLikeRegion(region)) {
    return jsReferencesAsync(cached, params.position);
  }
  if (region.language !== "vbscript") {
    return [];
  }
  return (
    await workspaceVbscriptReferencesForPosition(cached, params.position, {
      includeDeclaration: params.context.includeDeclaration,
    })
  ).map((reference) => Location.create(reference.uri, reference.range));
});

connection.onPrepareRename(async (params) => {
  const cached = await getFreshCachedAsync(params.textDocument.uri);
  if (!cached) {
    return null;
  }
  if (isJavaScriptPosition(cached, params.position)) {
    return (
      (await jsPrepareRenameAsync(cached, params.position)) ??
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
      await buildFullVbProjectContextForWorkspaceOperationAsync(
        cached,
        cachedSettings(cached.source.uri),
      ),
    ) ?? null
  );
});

connection.onRenameRequest(async (params: RenameParams): Promise<WorkspaceEdit | null> => {
  const cached = await getFreshCachedAsync(params.textDocument.uri);
  if (!cached) {
    return null;
  }
  if (isJavaScriptPosition(cached, params.position)) {
    const rename = await jsRenameAsync(cached, params.position, params.newName);
    const crossLanguage = await crossLanguageRename(cached, params.position, params.newName);
    return mergeWorkspaceEdits([rename, crossLanguage]) ?? null;
  }
  if (isHtmlPosition(cached, params.position)) {
    const rename = htmlRename(cached, params.position, params.newName);
    const crossLanguage = await crossLanguageRename(cached, params.position, params.newName);
    return mergeWorkspaceEdits([rename, crossLanguage]) ?? null;
  }
  if (isCssPosition(cached, params.position)) {
    const rename = cssRename(cached, params.position, params.newName);
    const crossLanguage = await crossLanguageRename(cached, params.position, params.newName);
    return mergeWorkspaceEdits([rename, crossLanguage]) ?? null;
  }
  if (!isVbscriptPosition(cached, params.position)) {
    return null;
  }
  const context = await buildFullVbProjectContextForWorkspaceOperationAsync(
    cached,
    cachedSettings(cached.source.uri),
  );
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

connection.onDocumentHighlight(async (params): Promise<DocumentHighlight[]> => {
  const cached = await getFreshCachedAsync(params.textDocument.uri);
  if (!cached) {
    return [];
  }
  const region = findRegionAt(cached.parsed, cached.source.offsetAt(params.position));
  if (region?.language === "vbscript") {
    return getVbscriptDocumentHighlights(
      cached.parsed,
      params.position,
      await interactiveVbProjectContextAsync(cached, cachedSettings(cached.source.uri)),
    );
  }
  if (region && isJavaScriptLikeRegion(region)) {
    return jsDocumentHighlightsAsync(cached, params.position);
  }
  if (region?.language === "html") {
    return htmlDocumentHighlights(cached, params.position);
  }
  if (region?.language === "css") {
    return cssDocumentHighlights(cached, params.position);
  }
  return [];
});

connection.onSignatureHelp(async (params): Promise<SignatureHelp | null> => {
  const cached = await getFreshCachedAsync(params.textDocument.uri);
  if (!cached) {
    return null;
  }
  if (isJavaScriptPosition(cached, params.position)) {
    return jsSignatureHelpAsync(cached, params.position);
  }
  if (!isVbscriptPosition(cached, params.position)) {
    return null;
  }
  return (
    getVbscriptSignatureHelp(
      cached.parsed,
      params.position,
      await interactiveVbProjectContextAsync(cached, cachedSettings(cached.source.uri)),
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
        await hydrateCachedVbscriptCstAsync(cached, cachedSettings(entry.uri), "workspaceSymbol");
        return [
          ...(await collectVbscriptSymbolsAsync(cached.parsed))
            .filter((symbol) => matchesQuery(symbol.name))
            .map(vbSymbolInformation),
          ...(await workspaceSymbolsForCachedAsync(cached)).filter((symbol) =>
            matchesQuery(symbol.name),
          ),
        ];
      },
    )
  ).flat();
  const openSymbols = (
    await Promise.all(
      documents.all().map(async (document) => {
        const cached = await ensureFreshCachedDocumentAsync(document);
        return cached
          ? ((
              await buildVbProjectContextAsync(cached, cachedSettings(document.uri))
            ).symbols?.filter((symbol) => matchesQuery(symbol.name)) ?? [])
          : [];
      }),
    )
  ).flat();
  const vbSymbols = openSymbols.map(vbSymbolInformation);
  const openRichSymbols = (
    await Promise.all(
      documents.all().map(async (document) => {
        const cached = await ensureFreshCachedDocumentAsync(document);
        return cached
          ? (await workspaceSymbolsForCachedAsync(cached)).filter((symbol) =>
              matchesQuery(symbol.name),
            )
          : [];
      }),
    )
  ).flat();
  return [...vbSymbols, ...indexedSymbols, ...openRichSymbols];
});

connection.onDocumentSymbol(async (params): Promise<DocumentSymbol[]> => {
  const cached = await getFreshCachedAsync(params.textDocument.uri);
  if (!cached) {
    return [];
  }
  await hydrateCachedVbscriptCstAsync(cached, cachedSettings(cached.source.uri), "documentSymbol");
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
    ...(await jsDocumentSymbolsAsync(cached)),
    ...getVbscriptDocumentSymbols(cached.parsed),
  ].map(documentSymbolWithContainedSelectionRange);
});

connection.onFoldingRanges(async (params): Promise<FoldingRange[]> => {
  const cached = await getFreshCachedAsync(params.textDocument.uri);
  if (!cached) {
    return [];
  }
  await hydrateCachedVbscriptCstAsync(cached, cachedSettings(cached.source.uri), "folding");
  const htmlVirtual = getCachedVirtual(cached, "html");
  const htmlRanges: FoldingRange[] = htmlVirtual
    ? htmlService.getFoldingRanges(toTextDocument(htmlVirtual), {})
    : [];
  const cssRanges = cssFoldingRanges(cached);
  const jsRanges = await jsFoldingRangesAsync(cached);
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

connection.onDocumentLinks(async (params): Promise<DocumentLink[]> => {
  const cached = await getFreshCachedAsync(params.textDocument.uri);
  if (!cached) {
    return [];
  }
  return Promise.all(
    cached.parsed.includes.map(async (include): Promise<DocumentLink> => {
      const targetPath = await resolveIncludePathAsync(
        cached.source.uri,
        include.path,
        include.mode,
        cachedSettings(cached.source.uri),
      );
      return { range: include.pathRange, target: pathToFileUri(targetPath) };
    }),
  );
});

connection.onSelectionRanges(async (params): Promise<SelectionRange[]> => {
  const cached = await getFreshCachedAsync(params.textDocument.uri);
  if (!cached) {
    return [];
  }
  await hydrateCachedVbscriptCstAsync(cached, cachedSettings(cached.source.uri), "selectionRange");
  return Promise.all(params.positions.map((position) => selectionRangeAtAsync(cached, position)));
});

connection.languages.inlayHint.on(
  (params): Promise<InlayHint[]> =>
    runInteractiveLanguageFeature(async () => {
      const cached = await getFreshCachedAsync(params.textDocument.uri);
      if (!cached) {
        return [];
      }
      return [
        ...getVbscriptInlayHints(
          cached.parsed,
          params.range,
          await interactiveVbProjectContextAsync(cached, cachedSettings(cached.source.uri)),
          cachedSettings(cached.source.uri).inlayHints,
        ),
        ...(await jsInlayHintsAsync(cached, params.range)),
      ];
    }),
);

connection.languages.diagnostics.on(async (params) => {
  noteForegroundActivity();
  const document = documents.get(params.textDocument.uri);
  const cached = document
    ? await ensureFreshDiagnosticsCachedDocumentAsync(document)
    : await getIndexedCachedAsync(params.textDocument.uri, globalSettings);
  return {
    kind: "full" as const,
    items: cached ? await diagnosticsForCachedAsync(cached, cachedSettings(cached.source.uri)) : [],
  };
});

connection.languages.diagnostics.onWorkspace(async (_params, token) => {
  noteForegroundActivity();
  await ensureWorkspaceIndexAsync(globalSettings, token);
  const cancellation: AnalysisCancellation = {
    isCancellationRequested: () => token.isCancellationRequested,
  };
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
    const cached = await ensureFreshDiagnosticsCachedDocumentAsync(document);
    return cached
      ? {
          kind: "full" as const,
          uri: document.uri,
          version: document.version,
          items: await diagnosticsForCachedAsync(
            cached,
            cachedSettings(document.uri),
            "check.workspace",
            cancellation,
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
    return { ok: true };
  }
  if (params.command === clearCacheCommand || params.command === clearCacheServerCommand) {
    await clearDiskAnalysisCacheByCommand();
    await clearProcessCachesByCommand("command.clearCache");
    logDebugSummary(globalSettings, "[asp-lsp] cache.clear");
    return { ok: true, cleared: "all" };
  }
  if (params.command === clearDiskCacheCommand || params.command === clearDiskCacheServerCommand) {
    await clearDiskAnalysisCacheByCommand();
    return { ok: true, cleared: "disk" };
  }
  if (
    params.command === clearProcessCacheCommand ||
    params.command === clearProcessCacheServerCommand
  ) {
    await clearProcessCachesByCommand("command.clearProcessCache");
    return { ok: true, cleared: "process" };
  }
  if (params.command === buildGraphServerCommand) {
    return buildAspGraphForCommand(params.arguments?.[0]);
  }
  return {
    ok: false,
    message: createLocalizer(globalSettings.resolvedLocale).t("server.unknownCommand", {
      command: params.command,
    }),
  };
});

connection.languages.callHierarchy.onPrepare(async (params): Promise<CallHierarchyItem[]> => {
  const cached = await getFreshCachedAsync(params.textDocument.uri);
  if (!cached) {
    return [];
  }
  if (isJavaScriptPosition(cached, params.position)) {
    return (await jsPrepareCallHierarchyAsync(cached, params.position)).map(
      itemWithContainedSelectionRange,
    );
  }
  if (!isVbscriptPosition(cached, params.position)) {
    return [];
  }
  return prepareVbscriptCallHierarchy(
    cached.parsed,
    params.position,
    await buildFullVbProjectContextForWorkspaceOperationAsync(
      cached,
      cachedSettings(cached.source.uri),
    ),
    cached.source.uri,
  ).map(itemWithContainedSelectionRange);
});

connection.languages.callHierarchy.onIncomingCalls(
  async (params): Promise<CallHierarchyIncomingCall[]> => {
    if (isJsCallHierarchyItem(params.item)) {
      return (await jsIncomingCallsAsync(params.item)).map(incomingCallWithContainedSelectionRange);
    }
    const root = callHierarchyRootUri(params.item);
    const cached =
      (await getFreshCachedAsync(root)) ?? (await getFreshCachedAsync(params.item.uri));
    if (!cached) {
      return [];
    }
    return getVbscriptIncomingCalls(
      params.item,
      await buildFullVbProjectContextForWorkspaceOperationAsync(
        cached,
        cachedSettings(cached.source.uri),
      ),
    ).map(incomingCallWithContainedSelectionRange);
  },
);

connection.languages.callHierarchy.onOutgoingCalls(
  async (params): Promise<CallHierarchyOutgoingCall[]> => {
    if (isJsCallHierarchyItem(params.item)) {
      return (await jsOutgoingCallsAsync(params.item)).map(outgoingCallWithContainedSelectionRange);
    }
    const root = callHierarchyRootUri(params.item);
    const cached =
      (await getFreshCachedAsync(root)) ?? (await getFreshCachedAsync(params.item.uri));
    if (!cached) {
      return [];
    }
    return getVbscriptOutgoingCalls(
      params.item,
      await buildFullVbProjectContextForWorkspaceOperationAsync(
        cached,
        cachedSettings(cached.source.uri),
      ),
    ).map(outgoingCallWithContainedSelectionRange);
  },
);

connection.languages.typeHierarchy.onPrepare(async (params): Promise<TypeHierarchyItem[]> => {
  const cached = await getFreshCachedAsync(params.textDocument.uri);
  if (!cached) {
    return [];
  }
  await hydrateCachedVbscriptCstAsync(cached, cachedSettings(cached.source.uri), "typeHierarchy");
  const item = vbTypeHierarchyItemAt(cached, params.position);
  return item ? [itemWithContainedSelectionRange(item)] : [];
});

connection.languages.typeHierarchy.onSupertypes((): TypeHierarchyItem[] => []);

connection.languages.typeHierarchy.onSubtypes(
  async (params): Promise<TypeHierarchyItem[]> =>
    (await vbTypeHierarchyRelatedItemsAsync(params.item)).map(itemWithContainedSelectionRange),
);

connection.languages.moniker.on(async (params): Promise<Moniker[]> => {
  const cached = await getFreshCachedAsync(params.textDocument.uri);
  if (!cached) {
    return [];
  }
  await hydrateCachedVbscriptCstAsync(cached, cachedSettings(cached.source.uri), "moniker");
  return monikersAtAsync(cached, params.position);
});

connection.languages.inlineValue.on(async (params: InlineValueParams): Promise<InlineValue[]> => {
  const cached = await getFreshCachedAsync(params.textDocument.uri);
  if (!cached) {
    return [];
  }
  await hydrateCachedVbscriptCstAsync(cached, cachedSettings(cached.source.uri), "inlineValue");
  return inlineValuesAsync(cached, params.range);
});

connection.languages.onLinkedEditingRange(async (params): Promise<LinkedEditingRanges | null> => {
  const cached = await getFreshCachedAsync(params.textDocument.uri);
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

connection.onDocumentColor(async (params): Promise<ColorInformation[]> => {
  const cached = await getFreshCachedAsync(params.textDocument.uri);
  if (!cached) {
    return [];
  }
  return cssDocumentColors(cached);
});

connection.onColorPresentation(async (params): Promise<ColorPresentation[]> => {
  const cached = await getFreshCachedAsync(params.textDocument.uri);
  if (!cached) {
    return [];
  }
  return cssColorPresentations(cached, params.color, params.range);
});

connection.onCodeLens(async (params): Promise<CodeLens[]> => {
  const cached = await getFreshCachedAsync(params.textDocument.uri);
  if (!cached) {
    return [];
  }
  return codeLensesAsync(cached);
});

connection.onDocumentFormatting(async (params): Promise<TextEdit[]> => {
  const cached = await getFreshCachedAsync(params.textDocument.uri);
  if (!cached) {
    return [];
  }
  return formatAspDocumentWithDelegatesAsync(cached, params.options);
});

connection.onDocumentRangeFormatting(async (params): Promise<TextEdit[]> => {
  const cached = await getFreshCachedAsync(params.textDocument.uri);
  if (!cached) {
    return [];
  }
  const region = findRegionAt(cached.parsed, cached.source.offsetAt(params.range.start));
  if (!region || region.language !== "html") {
    return formatAspRangeWithDelegatesAsync(cached, params.range, params.options);
  }
  const virtual = getCachedVirtual(cached, "html");
  if (!virtual) {
    return [];
  }
  if (rangeOverlapsNonHtml(cached, params.range)) {
    return formatAspRangeWithDelegatesAsync(cached, params.range, params.options);
  }
  return htmlService.format(toTextDocument(virtual), params.range, {
    tabSize: params.options.tabSize,
    insertSpaces: params.options.insertSpaces,
  }) as TextEdit[];
});

connection.onCodeAction(async (params): Promise<CodeAction[]> => {
  const cached = await getFreshCachedAsync(params.textDocument.uri);
  if (!cached) {
    return [];
  }
  const quickFixes = (
    await Promise.all(
      params.context.diagnostics.map((diagnostic) =>
        quickFixesForDiagnosticAsync(cached, diagnostic),
      ),
    )
  ).flat();
  return [
    ...quickFixes,
    ...(await vbscriptCodeActionsAsync(cached, params.range, params.context)),
    ...cssCodeActions(cached, params.range, params.context),
    ...(await jsCodeActionsAsync(cached, params.range, params.context)),
  ];
});

connection.onCodeActionResolve((action) => action);

connection.onCodeLensResolve((lens) => resolveCodeLens(lens));

connection.onDocumentLinkResolve((link) => link);

connection.languages.inlayHint.resolve((hint) => hint);

connection.onDocumentOnTypeFormatting(async (params): Promise<TextEdit[]> => {
  const cached = await getFreshCachedAsync(params.textDocument.uri);
  if (!cached) {
    return [];
  }
  await hydrateCachedVbscriptCstAsync(cached, cachedSettings(cached.source.uri), "format");
  return onTypeFormattingAsync(cached, params.position, params.ch, params.options);
});

connection.languages.semanticTokens.on(async (params): Promise<SemanticTokens> => {
  const cached = await getFreshCachedAsync(params.textDocument.uri);
  if (!cached) {
    return { data: [] };
  }
  return cacheSemanticTokens(cached.source.uri, (await buildSemanticTokensAsync(cached)).data);
});

connection.languages.semanticTokens.onRange(async (params): Promise<SemanticTokens> => {
  const cached = await getFreshCachedAsync(params.textDocument.uri);
  if (!cached) {
    return { data: [] };
  }
  return buildSemanticTokensAsync(cached, params.range);
});

connection.languages.semanticTokens.onDelta(
  async (params): Promise<SemanticTokens | SemanticTokensDelta> => {
    const cached = await getFreshCachedAsync(params.textDocument.uri);
    if (!cached) {
      return { data: [] };
    }
    const previous = semanticTokenResults.get(params.previousResultId);
    const next = (await buildSemanticTokensAsync(cached)).data;
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
  },
);

function validate(document: TextDocument): void {
  cancelScheduledDiagnostics(document.uri);
  void validateAsync(document).catch((error: unknown) =>
    connection.console.warn(`[asp-lsp] validate.failed: ${errorMessage(error)}`),
  );
}

async function validateAsync(document: TextDocument): Promise<void> {
  const cached = await ensureFreshDiagnosticsCachedDocumentAsync(document);
  if (documents.get(document.uri)?.version !== cached.identity.version) {
    return;
  }
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
      `[asp-lsp] analysis.parse.impact: ${document.uri}, mode=skeleton, reason=${impactReason}`,
    );
  }
  const cacheStartedAt = process.hrtime.bigint();
  const cached = createCachedDocument(document, parsed, settings);
  cache.set(document.uri, cached);
  finishDebugStep(settings, document.uri, "analysis.cacheUpdate", cacheStartedAt);
  finishAnalysisLog(settings, document.uri, startedAt, "skeleton");
  return cached;
}

async function refreshCachedDocumentSkeletonAsync(
  document: TextDocument,
  impactReason?: string,
): Promise<CachedDocument> {
  const startedAt = process.hrtime.bigint();
  const settingsStartedAt = startedAt;
  const settings = cachedSettings(document.uri);
  startAnalysisLog(settings, document.uri);
  finishDebugStep(settings, document.uri, "analysis.settings", settingsStartedAt);
  const parseStartedAt = process.hrtime.bigint();
  const parsed = await parseAspDocumentSkeletonAsync(document.uri, document.getText(), settings);
  finishDebugStep(settings, document.uri, "analysis.parse.skeleton", parseStartedAt);
  finishDebugStep(
    settings,
    document.uri,
    "analysis.virtualDocuments.lazy",
    process.hrtime.bigint(),
  );
  if (impactReason) {
    logDebugSummary(
      settings,
      `[asp-lsp] analysis.parse.impact: ${document.uri}, mode=skeleton, reason=${impactReason}`,
    );
  }
  const cacheStartedAt = process.hrtime.bigint();
  const cached = createCachedDocument(document, parsed, settings, [], "skeleton");
  cache.set(document.uri, cached);
  finishDebugStep(settings, document.uri, "analysis.cacheUpdate", cacheStartedAt);
  finishAnalysisLog(settings, document.uri, startedAt, "skeleton");
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
  const cached = createCachedDocument(
    document,
    updated.parsed,
    settings,
    editHistory,
    updated.impact.kind === "incremental" ? previous.parseDepth : "full",
  );
  cached.lastEditImpact = updated.impact;
  cached.lastIncrementalChange = change;
  cached.lastEditIsOrdinaryVbscriptComment =
    updated.impact.kind === "incremental" &&
    updated.impact.language === "vbscript" &&
    isOrdinaryVbscriptCommentEdit(previous, change);
  seedIncludeDiagnosticsAfterIncrementalChange(previous, cached, settings, change, updated.impact);
  seedVbProjectDocumentsAfterStableIncludeGraph(previous, cached, settings);
  seedVbReuseAfterIncrementalChange(previous, cached, settings, change, updated.impact);
  seedSyntaxDiagnosticsAfterIncrementalChange(previous, cached, change, updated.impact);
  seedJsDiagnosticsAfterIncrementalChange(previous, cached, updated.impact);
  cache.set(document.uri, cached);
  finishDebugStep(settings, document.uri, "analysis.cacheUpdate", cacheStartedAt);
  finishAnalysisLog(settings, document.uri, startedAt, updated.impact.kind);
  return cached;
}

async function refreshCachedDiagnosticsDocumentIncrementalAsync(
  previous: CachedDocument,
  document: TextDocument,
  settings: AspSettings,
  change: AspIncrementalChange,
): Promise<CachedDocument> {
  const startedAt = process.hrtime.bigint();
  const settingsStartedAt = startedAt;
  startAnalysisLog(settings, document.uri);
  finishDebugStep(settings, document.uri, "analysis.settings", settingsStartedAt);
  const parseStartedAt = process.hrtime.bigint();
  const updated = await updateAspParsedDocumentSkeletonAsync(previous.parsed, [change], settings);
  finishDebugStep(
    settings,
    document.uri,
    updated.impact.kind === "incremental"
      ? "analysis.parse.incremental"
      : "analysis.parse.skeleton",
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
  const cached = createCachedDocument(document, updated.parsed, settings, editHistory, "skeleton");
  cached.lastEditImpact = updated.impact;
  cached.lastIncrementalChange = change;
  cached.lastEditIsOrdinaryVbscriptComment =
    updated.impact.kind === "incremental" &&
    updated.impact.language === "vbscript" &&
    isOrdinaryVbscriptCommentEdit(previous, change);
  seedIncludeDiagnosticsAfterIncrementalChange(previous, cached, settings, change, updated.impact);
  seedVbProjectDocumentsAfterStableIncludeGraph(previous, cached, settings);
  seedVbReuseAfterIncrementalChange(previous, cached, settings, change, updated.impact);
  seedSyntaxDiagnosticsAfterIncrementalChange(previous, cached, change, updated.impact);
  seedJsDiagnosticsAfterIncrementalChange(previous, cached, updated.impact);
  cache.set(document.uri, cached);
  finishDebugStep(settings, document.uri, "analysis.cacheUpdate", cacheStartedAt);
  finishAnalysisLog(settings, document.uri, startedAt, updated.impact.kind);
  return cached;
}

async function refreshCachedDocumentIncrementalAsync(
  previous: CachedDocument,
  document: TextDocument,
  settings: AspSettings,
  change: AspIncrementalChange,
): Promise<CachedDocument> {
  return refreshCachedDiagnosticsDocumentIncrementalAsync(previous, document, settings, change);
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
    if (existing.parseDepth === "full") {
      return existing;
    }
    const upgraded = refreshCachedDocument(document);
    upgraded.analysis = existing.analysis;
    upgraded.cssContext = existing.cssContext;
    return upgraded;
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

async function ensureFreshCachedDocumentAsync(document: TextDocument): Promise<CachedDocument> {
  const existing = cache.get(document.uri);
  const settings = cachedSettings(document.uri);
  const identity = documentIdentityFor(document);
  const parseIdentity = parseSettingsIdentity(settings);
  const inFlight = inFlightDocumentRefreshes.get(document.uri);
  if (
    inFlight &&
    sameDocumentIdentity(inFlight.identity, identity) &&
    inFlight.parseSettingsIdentity === parseIdentity
  ) {
    return inFlight.promise;
  }
  if (
    existing &&
    sameDocumentIdentity(existing.identity, identity) &&
    existing.parseSettingsIdentity === parseIdentity
  ) {
    updateCachedDocumentRuntimeIdentity(existing, settings);
    return existing;
  }
  if (existing && existing.parseSettingsIdentity === parseIdentity) {
    const pending = pendingDocumentChanges.get(document.uri);
    if (pending?.version === document.version) {
      pendingDocumentChanges.delete(document.uri);
      if (pending.ranged && pending.changes.length === 1) {
        return rememberInFlightDocumentRefresh(
          document,
          parseIdentity,
          refreshCachedDocumentIncrementalAsync(existing, document, settings, pending.changes[0]),
        );
      }
      return rememberInFlightDocumentRefresh(
        document,
        parseIdentity,
        refreshCachedDocumentSkeletonAsync(
          document,
          pending?.reason ?? "non-incremental document change",
        ),
      );
    }
  }
  pendingDocumentChanges.delete(document.uri);
  return rememberInFlightDocumentRefresh(
    document,
    parseIdentity,
    refreshCachedDocumentSkeletonAsync(document),
  );
}

async function ensureFreshDiagnosticsCachedDocumentAsync(
  document: TextDocument,
): Promise<CachedDocument> {
  return ensureFreshCachedDocumentAsync(document);
}

function rememberInFlightDocumentRefresh(
  document: TextDocument,
  parseSettingsIdentityValue: string,
  promise: Promise<CachedDocument>,
): Promise<CachedDocument> {
  const entry: InFlightDocumentRefresh = {
    identity: documentIdentityFor(document),
    parseSettingsIdentity: parseSettingsIdentityValue,
    promise,
  };
  inFlightDocumentRefreshes.set(document.uri, entry);
  void promise.finally(() => {
    if (inFlightDocumentRefreshes.get(document.uri) === entry) {
      inFlightDocumentRefreshes.delete(document.uri);
    }
  });
  return promise;
}

async function getFreshCachedAsync(uri: string): Promise<CachedDocument | undefined> {
  const document = documents.get(uri);
  return document ? ensureFreshCachedDocumentAsync(document) : getCached(uri);
}

function createCachedDocument(
  document: TextDocument,
  parsed: AspParsedDocument,
  settings: AspSettings,
  editHistory: AspEditImpact[] = [],
  parseDepth: CachedDocument["parseDepth"] = "full",
): CachedDocument {
  const cached: CachedDocument = {
    source: document,
    parsed,
    parseDepth,
    virtuals: new Map<AspEmbeddedLanguage, VirtualDocument>(),
    virtualsMaterialized: false,
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
  };
}

function sameDocumentIdentity(left: DocumentIdentity, right: DocumentIdentity): boolean {
  return left.uri === right.uri && left.version === right.version;
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
  mode: AspEditImpact["kind"] | "skeleton" = "full",
): void {
  const elapsedMs = Number(process.hrtime.bigint() - startedAt) / 1_000_000;
  logDebugSummary(
    settings,
    `[asp-lsp] LSP analysis completed: ${uri} ${formatElapsedMs(elapsedMs)}, mode=${mode}`,
  );
}

async function scheduleDiagnosticsAsync(document: TextDocument): Promise<CachedDocument> {
  cancelScheduledDiagnostics(document.uri);
  const settings = cachedSettings(document.uri);
  const cached = await ensureFreshDiagnosticsCachedDocumentAsync(document);
  if (documents.get(document.uri)?.version !== cached.identity.version) {
    return cached;
  }
  const state = startStagedDiagnostics(cached, settings, false, {
    preservePreviousDiagnosticsUntilFinal: hasPublishedDiagnostics(document.uri),
  });
  const delay = settings.diagnostics?.debounceMs ?? defaultDiagnosticsDebounceMs;
  if (delay <= 0) {
    void runStagedDiagnostics(cached, settings, state);
    return cached;
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
  return cached;
}

function shouldScheduleProjectUpdateForDocumentChange(cached: CachedDocument): boolean {
  if (cached.lastEditImpact?.kind !== "incremental") {
    return true;
  }
  if (cached.lastEditIsOrdinaryVbscriptComment) {
    return false;
  }
  return cached.lastEditImpact.language !== "html" && cached.lastEditImpact.language !== "css";
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
  const includeItemsPromise = includeDiagnosticsForCachedAsync(
    cached,
    settings,
    "check",
    cancellation,
  );
  const projectItemsPromise = projectDiagnosticsForCachedAsync(
    cached,
    settings,
    "check",
    cancellation,
    "foreground",
  );
  void includeItemsPromise.catch(() => undefined);
  void projectItemsPromise.catch(() => undefined);
  const syntaxItems = syntaxDiagnosticsForCached(cached, settings, "check");
  const includeItems = await includeItemsPromise;
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
  state.layers.syntax = syntaxItems;
  publishStagedDiagnosticsLayer(cached, settings, state, "syntax");
  await yieldToEventLoop();

  if (!isCurrentStagedDiagnostics(cached, state)) {
    logStaleStagedDiagnostics(settings, state, "project");
    return;
  }
  state.layers.project = await projectItemsPromise;
  shareStagedAnalysisWithCurrentCache(cached, state);
  publishStagedDiagnosticsLayer(cached, settings, state, "project");
  if (!isCurrentStagedDiagnostics(cached, state)) {
    logStaleStagedDiagnostics(settings, state, "final");
    return;
  }
  const finalItems = publishStagedDiagnosticsLayer(cached, settings, state, "final");
  finishCheckLog(cached, settings, state.startedAt, finalItems.length);
}

function shareStagedAnalysisWithCurrentCache(
  cached: CachedDocument,
  state: StagedDiagnosticsState,
): void {
  const current = cache.get(state.uri);
  if (
    !current ||
    !cached.analysis ||
    current === cached ||
    current.identity.version !== cached.identity.version ||
    current.diagnosticsIdentity !== state.diagnosticsIdentity
  ) {
    return;
  }
  current.analysis = cached.analysis;
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
  const current = cache.get(state.uri);
  return (
    active?.generation === state.generation &&
    document?.version === state.version &&
    current?.identity.version === state.version &&
    cached.identity.version === state.version &&
    current?.diagnosticsIdentity === state.diagnosticsIdentity
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
  const includeItemsPromise = includeDiagnosticsForCachedAsync(
    cached,
    settings,
    stepPrefix,
    cancellation,
  );
  const projectItemsPromise = projectDiagnosticsForCachedAsync(
    cached,
    settings,
    stepPrefix,
    cancellation,
    mode,
  );
  void includeItemsPromise.catch(() => undefined);
  void projectItemsPromise.catch(() => undefined);
  const syntaxItems = syntaxDiagnosticsForCached(cached, settings, stepPrefix);
  const [includeItems, projectItems] = await Promise.all([
    includeItemsPromise,
    projectItemsPromise,
  ]);
  if (cancellation.isCancellationRequested()) {
    return [];
  }
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
  const key = includeDiagnosticsCacheKey(cached, settings);
  const cachedItems = cached.analysis?.includeDiagnostics;
  const items =
    cachedItems?.key === key
      ? measureDebugStep(
          settings,
          cached.source.uri,
          `${stepPrefix}.includeDiagnostics.reuse`,
          () => {
            logDebugSummary(settings, `[asp-lsp] includeDiagnostics.reuse: ${cached.source.uri}`);
            return cachedItems.items;
          },
        )
      : await includeDiagnosticsAsync(cached, settings, cancellation);
  if (cachedItems?.key !== key && !cancellation.isCancellationRequested()) {
    analysisFor(cached).includeDiagnostics = {
      key,
      items,
      text: cached.parsed.text,
    };
  }
  finishDebugStep(settings, cached.source.uri, `${stepPrefix}.includeDiagnostics`, startedAt);
  return items;
}

function includeDiagnosticsCacheKey(cached: CachedDocument, settings: AspSettings): string {
  return JSON.stringify({
    includeResolution: cached.includeResolutionIdentity,
    locale: settings.resolvedLocale,
    windowsPathResolution: settings.windowsPathResolution !== false,
    includes: cached.parsed.includes.map((include) => ({
      path: include.path,
      mode: include.mode,
    })),
  });
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
      jsSyntaxDiagnostics(cached, settings, stepPrefix),
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
  const jsItems = await jsSlowDiagnosticsAsync(cached, settings, stepPrefix, cancellation, mode);
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

function internalTestThreshold(name: string, fallback: number): number {
  const raw = process.env[name];
  if (process.env.NODE_ENV !== "test" || !raw) {
    return fallback;
  }
  const value = Number(raw);
  return Number.isFinite(value) && value > 0 ? Math.floor(value) : fallback;
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

const neverCancelled: AnalysisCancellation = {
  isCancellationRequested: () => false,
};

async function fileExistsAsync(fileName: string): Promise<boolean> {
  const stat = await fs.promises.stat(fileName).catch(() => undefined);
  return Boolean(stat?.isFile());
}

async function fileSizeAsync(fileName: string): Promise<number | undefined> {
  const stat = await fs.promises.stat(fileName).catch(() => undefined);
  return stat?.isFile() ? stat.size : undefined;
}

async function pathExistsAsync(fileName: string): Promise<boolean> {
  const stat = await fs.promises.stat(fileName).catch(() => undefined);
  return Boolean(stat);
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

function analysisConcurrency(settings: AspSettings): number {
  return busyAnalysisConcurrency(settings);
}

function workerAnalysisConcurrency(settings: AspSettings): number {
  return busyAnalysisConcurrency(settings);
}

function noteForegroundActivity(): void {
  lastForegroundActivityAt = Date.now();
}

function scheduleProjectUpdate(reason: string): void {
  pendingProjectUpdateReason = reason;
  if (projectUpdateTimer) {
    clearTimeout(projectUpdateTimer);
  }
  projectUpdateTimer = setTimeout(() => {
    projectUpdateTimer = undefined;
    void flushPendingProjectUpdatesWhenIdle(reason);
  }, projectUpdateDelayMs);
  logDebugSummary(globalSettings, `[asp-lsp] projectUpdate.scheduled: reason=${reason}`);
}

async function flushPendingProjectUpdatesWhenIdle(reason: string): Promise<boolean> {
  const elapsedSinceForeground = Date.now() - lastForegroundActivityAt;
  if (elapsedSinceForeground < projectUpdateDelayMs) {
    projectUpdateTimer = setTimeout(() => {
      projectUpdateTimer = undefined;
      void flushPendingProjectUpdatesWhenIdle(reason);
    }, projectUpdateDelayMs - elapsedSinceForeground);
    logDebugSummary(globalSettings, `[asp-lsp] projectUpdate.deferred: reason=${reason}`);
    return false;
  }
  return flushPendingProjectUpdatesAsync(reason);
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

async function flushPendingProjectUpdatesAsync(
  reason = pendingProjectUpdateReason ?? "foreground.flush",
): Promise<boolean> {
  if (projectUpdateTimer) {
    clearTimeout(projectUpdateTimer);
    projectUpdateTimer = undefined;
  }
  if (!pendingProjectUpdateReason && reason === "foreground.flush") {
    return false;
  }
  const startedAt = process.hrtime.bigint();
  let refreshed = 0;
  for (const document of documents.all()) {
    const cached = await ensureFreshDiagnosticsCachedDocumentAsync(document);
    await collectCachedVbProjectDocumentsAsync(cached, cachedSettings(document.uri));
    for (const virtual of jsVirtualDocuments(cached)) {
      await prefetchJsProjectFilesAsync(virtual, cachedSettings(document.uri));
      await createJsLanguageServiceAsync(virtual, cachedSettings(document.uri));
    }
    refreshed += 1;
    await yieldToEventLoop();
    if (Date.now() - lastForegroundActivityAt < projectUpdateDelayMs) {
      scheduleProjectUpdate(reason);
      logDebugSummary(globalSettings, `[asp-lsp] projectUpdate.interrupted: reason=${reason}`);
      return false;
    }
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
  return true;
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
  void flushPendingProjectUpdatesWhenIdle(reason).then((completed) => {
    if (completed) {
      logDebugSummary(
        globalSettings,
        `[asp-lsp] openFileProjectMaintenance.completed: reason=${reason}`,
      );
    }
  });
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

async function configureDiskAnalysisCacheAsync(): Promise<void> {
  diskAnalysisCache = createDiskAnalysisCache(globalSettings);
  await diskAnalysisCache.sweep();
}

function logDiskAnalysisCacheError(operation: string, error: unknown): void {
  connection.console.warn(`[asp-lsp] ${operation}.failed: ${errorMessage(error)}`);
}

function diskAnalysisNamespace(): string {
  return textFingerprint(
    JSON.stringify({
      roots: workspaceRoots.map(normalizeFileName).sort(),
      cwd: process.cwd(),
    }),
  );
}

function runInteractiveLanguageFeature<T>(callback: () => T): T {
  noteForegroundActivity();
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
  if (!cached.virtualsMaterialized) {
    cached.virtuals = buildVirtualDocuments(cached.parsed);
    cached.virtualsMaterialized = true;
  }
  return cached.virtuals.get(language);
}

function cssContext(cached: CachedDocument): CachedCssContext | undefined {
  const virtual = getCachedVirtual(cached, "css");
  if (!virtual) {
    return undefined;
  }
  const key = cssContextKey(virtual);
  return cssContextForVirtual(cached, virtual, key);
}

function cssContextForVirtual(
  cached: CachedDocument,
  virtual: VirtualDocument,
  key: string,
): CachedCssContext {
  const reusable = cached.cssContext;
  if (reusable?.key === key) {
    const context = reusable.virtual === virtual ? reusable : { ...reusable, virtual };
    cached.cssContext = context;
    logDebugSummary(cachedSettings(cached.source.uri), `[asp-lsp] css.context.reuse: ${key}`);
    return context;
  }
  const document = toTextDocument(virtual);
  const context: CachedCssContext = {
    key,
    virtual,
    document,
    stylesheet: cssService.parseStylesheet(document),
  };
  cached.cssContext = context;
  logDebugSummary(cachedSettings(cached.source.uri), `[asp-lsp] css.context.create: ${key}`);
  return context;
}

function cssContextKey(virtual: VirtualDocument): string {
  return `${virtual.uri}|${virtual.languageId}|${textFingerprint(virtual.text)}`;
}

function htmlDiagnostics(cached: CachedDocument): Diagnostic[] {
  const analysis = analysisFor(cached);
  if (analysis.htmlDiagnostics) {
    logDebugSummary(
      cachedSettings(cached.source.uri),
      `[asp-lsp] htmlDiagnostics.reuse: ${cached.source.uri}`,
    );
    return analysis.htmlDiagnostics.items;
  }
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
  analysis.htmlDiagnostics = { key: "html", items: diagnostics, text: cached.parsed.text };
  return diagnostics;
}

function cssDiagnostics(cached: CachedDocument): Diagnostic[] {
  const analysis = analysisFor(cached);
  const virtual = getCachedVirtual(cached, "css");
  if (!virtual) {
    return [];
  }
  const key = cssContextKey(virtual);
  if (analysis.cssDiagnostics?.key === key) {
    logDebugSummary(
      cachedSettings(cached.source.uri),
      `[asp-lsp] cssDiagnostics.reuse: ${cached.source.uri}`,
    );
    return analysis.cssDiagnostics.items;
  }
  const context = cssContextForVirtual(cached, virtual, key);
  const { document, stylesheet, virtual: contextVirtual } = context;
  const diagnostics = cssService
    .doValidation(document, stylesheet)
    .map((diagnostic) => remapDiagnostic(contextVirtual, diagnostic, "asp-lsp-css"))
    .filter(isDiagnostic);
  analysis.cssDiagnostics = { key: context.key, items: diagnostics, text: cached.parsed.text };
  return diagnostics;
}

function jsSyntaxDiagnostics(
  cached: CachedDocument,
  settings: AspSettings,
  stepPrefix: string,
): Diagnostic[] {
  const analysis = analysisFor(cached);
  const key = jsDiagnosticsCacheKey(cached, settings);
  const cachedItems = analysis.jsSyntaxDiagnostics;
  if (cachedItems?.key === key) {
    return measureDebugStep(
      settings,
      cached.source.uri,
      `${stepPrefix}.javascriptSyntax.reuse`,
      () => cachedJsDiagnosticsToLsp(cached, cachedItems),
    );
  }
  const virtuals = jsVirtualDocuments(cached);
  const entry: CachedJsDiagnosticsEntry = {
    key,
    virtuals: virtuals.map((virtual) => {
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
      return {
        virtualKey: jsDiagnosticsVirtualKey(virtual),
        diagnostics: parseDiagnostics.map((diagnostic) => ({
          diagnostic: cacheTsDiagnostic(diagnostic),
        })),
      };
    }),
  };
  analysis.jsSyntaxDiagnostics = entry;
  return cachedJsDiagnosticsToLsp(cached, entry);
}

async function jsSlowDiagnosticsAsync(
  cached: CachedDocument,
  settings: AspSettings,
  stepPrefix: string,
  cancellation: AnalysisCancellation = neverCancelled,
  mode: AnalysisExecutionMode = "foreground",
): Promise<Diagnostic[]> {
  const analysis = analysisFor(cached);
  const key = jsDiagnosticsCacheKey(cached, settings);
  const cachedItems = analysis.jsSlowDiagnostics;
  if (cachedItems?.key === key) {
    return measureDebugStep(
      settings,
      cached.source.uri,
      `${stepPrefix}.javascriptDiagnostics.reuse`,
      () => cachedJsDiagnosticsToLsp(cached, cachedItems),
    );
  }
  const virtuals = jsVirtualDocuments(cached);
  const workerOpenVirtuals =
    settings.checkJs === true && shouldUseJsDiagnosticsWorker(mode)
      ? await openJsDiagnosticsWorkerVirtualDocumentsAsync()
      : undefined;
  const entry: CachedJsDiagnosticsEntry = {
    key,
    virtuals: await Promise.all(
      virtuals.map(async (virtual) => {
        const semantic = await measureDebugStepAsync(
          settings,
          cached.source.uri,
          `${stepPrefix}.javascriptSemantic`,
          async () => {
            if (settings.checkJs !== true) {
              return [];
            }
            return jsSemanticDiagnosticsAsync(
              cached,
              virtual,
              settings,
              cancellation,
              mode,
              workerOpenVirtuals,
            );
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
        return {
          virtualKey: jsDiagnosticsVirtualKey(virtual),
          diagnostics: [
            ...semantic.map((diagnostic) => ({ diagnostic: cacheTsDiagnostic(diagnostic) })),
            ...unusedOnly.map((diagnostic) => ({
              diagnostic: cacheTsDiagnostic(diagnostic),
              severity: DiagnosticSeverity.Hint,
              source: "asp-lsp-typescript-unused",
            })),
          ],
        };
      }),
    ),
  };
  analysis.jsSlowDiagnostics = entry;
  return cachedJsDiagnosticsToLsp(cached, entry);
}

async function jsSemanticDiagnosticsAsync(
  cached: CachedDocument,
  virtual: VirtualDocument,
  settings: AspSettings,
  cancellation: AnalysisCancellation,
  mode: AnalysisExecutionMode,
  workerOpenVirtuals?: readonly JsDiagnosticsWorkerVirtualDocument[],
): Promise<CachedTsDiagnostic[]> {
  if (shouldUseJsDiagnosticsWorker(mode)) {
    try {
      const response = await runJsDiagnosticsWorker(
        virtual,
        settings,
        cancellation,
        mode,
        workerOpenVirtuals,
      );
      if (cancellation.isCancellationRequested() || response.cancelled) {
        return [];
      }
      if (response.error) {
        throw jsWorkerResponseError(response);
      }
      logJsWorkerTimings(settings, cached.source.uri, "check.javascript", response);
      return response.diagnostics ?? [];
    } catch (error) {
      connection.console.warn(
        `[asp-lsp] javascript.diagnostics.worker.failed: ${errorMessage(error)}`,
      );
    }
  }
  await prefetchJsProjectFilesAsync(virtual, settings);
  if (cancellation.isCancellationRequested()) {
    return [];
  }
  const project = await createJsLanguageServiceAsync(virtual, settings);
  return project.service
    .getSemanticDiagnostics(jsProjectFileName(virtual, project))
    .map(cacheTsDiagnostic);
}

async function runJsDiagnosticsWorker(
  virtual: VirtualDocument,
  settings: AspSettings,
  cancellation: AnalysisCancellation,
  mode: AnalysisExecutionMode,
  workerOpenVirtuals?: readonly JsDiagnosticsWorkerVirtualDocument[],
): Promise<JsDiagnosticsWorkerResponse> {
  const pool = getJsDiagnosticsWorkerPool(settings, mode);
  const id = ++jsDiagnosticsWorkerRequestId;
  const activeVirtual = jsDiagnosticsWorkerVirtualDocument(virtual);
  const activeVirtualFileName = normalizeFileName(jsVirtualFileName(activeVirtual.uri));
  const openVirtuals = (
    workerOpenVirtuals ?? (await openJsDiagnosticsWorkerVirtualDocumentsAsync())
  ).filter(
    (openVirtual) =>
      normalizeFileName(jsVirtualFileName(openVirtual.uri)) !== activeVirtualFileName,
  );
  return pool.run(
    {
      id,
      activeVirtual,
      openVirtuals,
      settings,
      workspaceRoots,
      projectGeneration: jsProjectGeneration,
    },
    { isCancellationRequested: () => cancellation.isCancellationRequested() },
  );
}

function getJsDiagnosticsWorkerPool(
  settings: AspSettings,
  _mode: AnalysisExecutionMode,
): JsDiagnosticsWorkerPool {
  jsDiagnosticsWorkerPool ??= new JsDiagnosticsWorkerPool();
  jsDiagnosticsWorkerPool.resize(workerAnalysisConcurrency(settings));
  return jsDiagnosticsWorkerPool;
}

function shouldUseJsDiagnosticsWorker(_mode: AnalysisExecutionMode): boolean {
  return (
    process.env.ASP_LSP_DISABLE_JS_WORKERS !== "1" || process.env.ASP_LSP_FORCE_JS_WORKERS === "1"
  );
}

function jsDiagnosticsWorkerVirtualDocument(
  virtual: VirtualDocument,
): JsDiagnosticsWorkerVirtualDocument {
  return {
    uri: virtual.uri,
    languageId: virtual.languageId,
    text: virtual.text,
  };
}

async function openJsDiagnosticsWorkerVirtualDocumentsAsync(): Promise<
  JsDiagnosticsWorkerVirtualDocument[]
> {
  const virtuals = await Promise.all(
    documents.all().map(async (document) => {
      const cached = await ensureFreshCachedDocumentAsync(document);
      return jsVirtualDocuments(cached);
    }),
  );
  return virtuals.flat().map(jsDiagnosticsWorkerVirtualDocument);
}

function logJsWorkerTimings(
  settings: AspSettings,
  uri: string,
  stepPrefix: string,
  response: JsDiagnosticsWorkerResponse,
): void {
  logDebugSummary(
    settings,
    `[asp-lsp] worker.queue.wait: ${uri}, request=${response.id}, ${formatElapsedMs(response.queueWaitMs ?? 0)}, queueLength=${response.queueLengthAtDispatch ?? 0}, cancelled=${response.cancelled === true}`,
  );
  logDebugSummary(
    settings,
    `[asp-lsp] worker.run.duration: ${uri}, request=${response.id}, ${formatElapsedMs(response.runMs ?? 0)}`,
  );
  logDebugSummary(
    settings,
    `[asp-lsp] worker.payload.bytes: ${uri}, request=${response.id}, payload=${response.payloadBytes ?? 0}, result=${response.resultBytes ?? 0}`,
  );
  for (const timing of response.timings ?? []) {
    logDebugElapsed(settings, uri, `${stepPrefix}.${timing.name}.worker`, timing.elapsedMs);
  }
  if (!isDebugVerboseEnabled(settings)) {
    return;
  }
  const metrics = [
    response.queueWaitMs === undefined
      ? undefined
      : `queueWaitMs=${response.queueWaitMs.toFixed(1)}`,
    response.runMs === undefined ? undefined : `runMs=${response.runMs.toFixed(1)}`,
    response.payloadBytes === undefined ? undefined : `payloadBytes=${response.payloadBytes}`,
    response.resultBytes === undefined ? undefined : `resultBytes=${response.resultBytes}`,
    response.queueLengthAtDispatch === undefined
      ? undefined
      : `queueLength=${response.queueLengthAtDispatch}`,
  ]
    .filter((item): item is string => Boolean(item))
    .join(", ");
  connection.console.info(`[asp-lsp] javascript.diagnostics.worker: ${uri} ${metrics}`);
}

function jsWorkerResponseError(response: JsDiagnosticsWorkerResponse): Error {
  const error = new Error(response.error?.message ?? "JavaScript diagnostics worker failed.");
  error.name = response.error?.name ?? error.name;
  if (response.error?.stack) {
    error.stack = response.error.stack;
  }
  return error;
}

function jsDiagnosticsCacheKey(cached: CachedDocument, settings: AspSettings): string {
  return JSON.stringify({
    diagnostics: diagnosticsIdentity(settings),
    jsProject: cached.jsProjectGeneration,
    workspace: cached.workspaceGeneration,
    virtuals: jsVirtualDocuments(cached).map(jsDiagnosticsVirtualKey),
  });
}

function jsDiagnosticsVirtualKey(virtual: VirtualDocument): string {
  return JSON.stringify({
    uri: virtual.uri,
    language: virtual.languageId,
    sourceUri: virtualSourceUri(virtual),
    text: textFingerprint(virtual.text),
  });
}

function cachedJsDiagnosticsToLsp(
  cached: CachedDocument,
  entry: CachedJsDiagnosticsEntry,
): Diagnostic[] {
  const virtuals = new Map(
    jsVirtualDocuments(cached).map((virtual) => [jsDiagnosticsVirtualKey(virtual), virtual]),
  );
  return entry.virtuals.flatMap((cachedVirtual) => {
    const virtual = virtuals.get(cachedVirtual.virtualKey);
    if (!virtual) {
      return [];
    }
    return cachedVirtual.diagnostics
      .map((item) =>
        tsDiagnosticToLsp(cached.source, virtual, item.diagnostic, {
          severity: item.severity,
          source: item.source,
        }),
      )
      .filter(isDiagnostic);
  });
}

function cacheTsDiagnostic(diagnostic: TsDiagnosticLike): CachedTsDiagnostic {
  return {
    code: diagnostic.code,
    category: diagnostic.category,
    messageText: ts.flattenDiagnosticMessageText(diagnostic.messageText, "\n"),
    start: diagnostic.start,
    length: diagnostic.length,
    reportsUnnecessary: diagnostic.reportsUnnecessary === true,
  };
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
  await hydrateCachedVbscriptCstAsync(cached, settings, stepPrefix);
  const context = await measureDebugStepAsync(
    settings,
    cached.source.uri,
    `${stepPrefix}.vbscript.projectContext`,
    () => buildVbProjectContextAsync(cached, settings),
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
  return (
    await analyzeVbscriptFromTextAsync(cached.source.uri, cached.source.getText(), settings, {
      ...context,
      debugStep: (name, action) =>
        measureDebugStep(
          settings,
          cached.source.uri,
          `${stepPrefix}.vbscript.diagnostics.${name}`,
          action,
        ),
    })
  ).diagnostics;
}

async function hydrateCachedVbscriptCstAsync(
  cached: CachedDocument,
  settings: AspSettings,
  stepPrefix: string,
): Promise<void> {
  if (cached.parseDepth !== "skeleton" || cstHasVbscript(cached.parsed.cst)) {
    return;
  }
  await measureDebugStepAsync(settings, cached.source.uri, `${stepPrefix}.vbscript.hydrate`, () =>
    hydrateVbscriptCst(cached.parsed, settings),
  );
}

function cstHasVbscript(node: AspCstNode): boolean {
  if (node.vbscript) {
    return true;
  }
  return node.children.some((child) => cstHasVbscript(child));
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
  logDebugSummary(
    settings,
    `[asp-lsp] vbscript.worker.dispatch: ${cached.source.uri}, request=${id}, mode=${mode}, concurrency=${workerAnalysisConcurrency(settings)}`,
  );
  const response = await pool.run(
    {
      id,
      uri: cached.source.uri,
      text: cached.source.getText(),
      settings,
      context,
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
  _mode: AnalysisExecutionMode,
): VbDiagnosticsWorkerPool {
  vbDiagnosticsWorkerPool ??= new VbDiagnosticsWorkerPool();
  vbDiagnosticsWorkerPool.resize(workerAnalysisConcurrency(settings));
  return vbDiagnosticsWorkerPool;
}

function shouldUseVbDiagnosticsWorker(_mode: AnalysisExecutionMode): boolean {
  return (
    process.env.ASP_LSP_DISABLE_VB_WORKERS !== "1" || process.env.ASP_LSP_FORCE_VB_WORKERS === "1"
  );
}

function cloneableVbProjectContext(context: VbProjectContext): VbDiagnosticsWorkerContext {
  return {
    documents: context.documents?.map(vbDiagnosticsWorkerDocument),
    includeSummaryUris: context.includeSummaryUris,
    symbols: context.symbols,
    externalRefUsages: context.externalRefUsages,
    typeChecking: context.typeChecking,
    ifSyntaxDiagnostics: context.ifSyntaxDiagnostics,
    identifierCase: context.identifierCase,
    identifierCaseByKind: context.identifierCaseByKind,
    comTypes: context.comTypes,
    typeEnvironment: context.typeEnvironment,
    unusedDiagnostics: context.unusedDiagnostics,
    syntaxSnippets: context.syntaxSnippets,
    syntaxKeywords: context.syntaxKeywords,
    locale: context.locale,
  };
}

function vbDiagnosticsWorkerDocument(document: AspParsedDocument): VbDiagnosticsWorkerDocument {
  return {
    uri: document.uri,
    text: document.text,
    regions: document.regions,
    directives: document.directives,
    includes: document.includes,
    serverObjects: document.serverObjects,
    defaultLanguage: document.defaultLanguage,
    diagnostics: document.diagnostics,
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

function seedIncludeDiagnosticsAfterIncrementalChange(
  previous: CachedDocument,
  cached: CachedDocument,
  settings: AspSettings,
  change: AspIncrementalChange,
  impact: AspEditImpact,
): void {
  const previousDiagnostics = previous.analysis?.includeDiagnostics;
  if (
    impact.kind !== "incremental" ||
    !previousDiagnostics ||
    !sameIncludeRefs(previous.parsed, cached.parsed)
  ) {
    return;
  }
  analysisFor(cached).includeDiagnostics = {
    key: includeDiagnosticsCacheKey(cached, settings),
    text: cached.parsed.text,
    items:
      impact.delta === 0
        ? previousDiagnostics.items
        : previousDiagnostics.items.map((diagnostic) =>
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

function sameIncludeRefs(left: AspParsedDocument, right: AspParsedDocument): boolean {
  if (left.includes.length !== right.includes.length) {
    return false;
  }
  return left.includes.every((include, index) => {
    const other = right.includes[index];
    return include.path === other.path && include.mode === other.mode;
  });
}

function seedVbProjectDocumentsAfterStableIncludeGraph(
  previous: CachedDocument,
  cached: CachedDocument,
  settings: AspSettings,
): void {
  if (
    previous.parsed.defaultLanguage !== cached.parsed.defaultLanguage ||
    !sameIncludeRefs(previous.parsed, cached.parsed)
  ) {
    return;
  }
  const previousDocuments = previous.analysis?.vbProjectDocuments;
  if (
    !previousDocuments ||
    previousDocuments.collectionKey !== vbProjectDocumentCollectionKey(previous, settings)
  ) {
    return;
  }
  analysisFor(cached).vbProjectDocuments = {
    collectionKey: vbProjectDocumentCollectionKey(cached, settings),
    documents: [
      cached.parsed,
      ...previousDocuments.documents.filter((document) => document.uri !== previous.source.uri),
    ],
  };
}

function seedVbReuseAfterIncrementalChange(
  previous: CachedDocument,
  cached: CachedDocument,
  settings: AspSettings,
  change: AspIncrementalChange,
  impact: AspEditImpact,
): void {
  const canReuseVbscriptChange =
    impact.kind === "incremental" &&
    impact.language === "vbscript" &&
    cached.lastEditIsOrdinaryVbscriptComment === true;
  const canReuseUnchangedVbscript =
    impact.language !== "vbscript" &&
    impact.language !== "jscript" &&
    sameIncludeRefs(previous.parsed, cached.parsed) &&
    vbscriptRegionContentFingerprint(previous.parsed) ===
      vbscriptRegionContentFingerprint(cached.parsed);
  if (!canReuseVbscriptChange && !canReuseUnchangedVbscript) {
    return;
  }
  const analysis = analysisFor(cached);
  const previousDiagnostics = previous.analysis?.vbDiagnostics;
  if (previousDiagnostics) {
    analysis.vbDiagnostics = {
      key: vbDiagnosticsCacheKey(cached, settings),
      text: cached.parsed.text,
      items:
        impact.delta === 0
          ? previousDiagnostics.items
          : previousDiagnostics.items.map((diagnostic) =>
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
    const context =
      impact.delta === 0
        ? replaceVbProjectContextRootDocument(
            previousContext.context,
            previous.source.uri,
            cached.parsed,
          )
        : shiftVbProjectContextForIncrementalChange(
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

function isOrdinaryVbscriptCommentEdit(
  previous: CachedDocument,
  change: AspIncrementalChange,
): boolean {
  if (change.text.includes("\n") || change.text.includes("\r")) {
    return false;
  }
  const startOffset = previous.source.offsetAt(change.range.start);
  const endOffset =
    change.rangeLength === undefined
      ? previous.source.offsetAt(change.range.end)
      : startOffset + change.rangeLength;
  const token = vbTokenCoveringRange(previous.parsed, startOffset, endOffset);
  if (!isOrdinaryVbscriptCommentToken(token)) {
    return false;
  }
  const relativeStart = startOffset - token.start;
  const relativeEnd = endOffset - token.start;
  const nextText = `${token.text.slice(0, relativeStart)}${change.text}${token.text.slice(relativeEnd)}`;
  return isOrdinaryVbscriptCommentText(nextText);
}

function vbTokenCoveringRange(
  parsed: AspParsedDocument,
  startOffset: number,
  endOffset: number,
): VbToken | undefined {
  const children = parsed.cst.children;
  for (let index = lastCstChildIndexAtOffset(children, startOffset); index >= 0; index -= 1) {
    const child = children[index];
    const tokens = child.vbscript?.tokens;
    if (!tokens || child.contentEnd < startOffset || child.contentStart > endOffset) {
      continue;
    }
    const token = tokenCoveringRange(tokens, startOffset, endOffset);
    if (token) {
      return token;
    }
  }
  return undefined;
}

function lastCstChildIndexAtOffset(children: AspCstNode[], offset: number): number {
  let low = 0;
  let high = children.length - 1;
  let index = -1;
  while (low <= high) {
    const middle = Math.floor((low + high) / 2);
    if (children[middle].contentStart <= offset) {
      index = middle;
      low = middle + 1;
    } else {
      high = middle - 1;
    }
  }
  return index;
}

function tokenCoveringRange(
  tokens: VbToken[],
  startOffset: number,
  endOffset: number,
): VbToken | undefined {
  let low = 0;
  let high = tokens.length - 1;
  let candidate: VbToken | undefined;
  while (low <= high) {
    const middle = Math.floor((low + high) / 2);
    const token = tokens[middle];
    if (token.start <= startOffset) {
      candidate = token;
      low = middle + 1;
    } else {
      high = middle - 1;
    }
  }
  return candidate && candidate.end >= endOffset ? candidate : undefined;
}

function isOrdinaryVbscriptCommentToken(token: VbToken | undefined): token is VbToken {
  return token?.kind === "comment" && isOrdinaryVbscriptCommentText(token.text);
}

function isOrdinaryVbscriptCommentText(text: string): boolean {
  return text.startsWith("'") && !text.startsWith("'''");
}

function seedSyntaxDiagnosticsAfterIncrementalChange(
  previous: CachedDocument,
  cached: CachedDocument,
  change: AspIncrementalChange,
  impact: AspEditImpact,
): void {
  if (impact.kind !== "incremental") {
    return;
  }
  const analysis = analysisFor(cached);
  if (impact.language !== "html" && previous.analysis?.htmlDiagnostics) {
    analysis.htmlDiagnostics = {
      ...previous.analysis.htmlDiagnostics,
      text: cached.parsed.text,
      items: shiftDiagnosticsForIncrementalChange(
        previous.analysis.htmlDiagnostics.items,
        previous.source.uri,
        previous.parsed.text,
        cached.parsed.text,
        change,
        impact,
      ),
    };
  }
  if (impact.language !== "css" && previous.analysis?.cssDiagnostics) {
    analysis.cssDiagnostics = {
      ...previous.analysis.cssDiagnostics,
      text: cached.parsed.text,
      items: shiftDiagnosticsForIncrementalChange(
        previous.analysis.cssDiagnostics.items,
        previous.source.uri,
        previous.parsed.text,
        cached.parsed.text,
        change,
        impact,
      ),
    };
  }
}

function shiftDiagnosticsForIncrementalChange(
  diagnostics: Diagnostic[],
  rootUri: string,
  previousText: string,
  nextText: string,
  change: AspIncrementalChange,
  impact: AspEditImpact,
): Diagnostic[] {
  return impact.delta === 0
    ? diagnostics
    : diagnostics.map((diagnostic) =>
        shiftDiagnosticForIncrementalChange(diagnostic, rootUri, previousText, nextText, change),
      );
}

function seedJsDiagnosticsAfterIncrementalChange(
  previous: CachedDocument,
  cached: CachedDocument,
  impact: AspEditImpact,
): void {
  if (impact.kind !== "incremental") {
    return;
  }
  const previousSyntax = previous.analysis?.jsSyntaxDiagnostics;
  const previousSlow = previous.analysis?.jsSlowDiagnostics;
  if (!previousSyntax && !previousSlow) {
    return;
  }
  const analysis = analysisFor(cached);
  if (previousSyntax) {
    analysis.jsSyntaxDiagnostics = previousSyntax;
  }
  if (previousSlow) {
    analysis.jsSlowDiagnostics = previousSlow;
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
    serverObjects: serverObjectDeclarationsFingerprint(parsed),
    regions: parsed.regions
      .filter((region) => region.language === "vbscript")
      .map((region) => ({
        kind: region.kind,
        text: textFingerprint(parsed.text.slice(region.contentStart, region.contentEnd)),
      })),
  });
}

function serverObjectDeclarationsFingerprint(parsed: AspParsedDocument): unknown {
  return parsed.serverObjects.map((serverObject) => ({
    id: serverObject.id,
    progId: serverObject.progId,
    classId: serverObject.classId,
  }));
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

function replaceVbProjectContextRootDocument(
  context: VbProjectContext,
  rootUri: string,
  currentRoot: AspParsedDocument,
): VbProjectContext {
  return {
    ...context,
    documents: [
      currentRoot,
      ...(context.documents?.filter((document) => document.uri !== rootUri) ?? []),
    ],
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

function lightweightJsUnusedDiagnostics(virtual: VirtualDocument): CachedTsDiagnostic[] {
  const cacheKey = lightweightJsUnusedDiagnosticsCacheKey(virtual);
  const cached = lightweightJsUnusedDiagnosticsCache.get(cacheKey);
  if (cached) {
    cached.lastUsed = ++lightweightJsUnusedCacheTick;
    return cached.diagnostics;
  }
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
    const diagnostics = service
      .getSemanticDiagnostics(fileName)
      .filter((diagnostic) => tsUnusedDiagnosticCodes.has(diagnostic.code))
      .map(cacheTsDiagnostic);
    lightweightJsUnusedDiagnosticsCache.set(cacheKey, {
      diagnostics,
      lastUsed: ++lightweightJsUnusedCacheTick,
    });
    pruneLightweightJsUnusedDiagnosticsCache();
    return diagnostics;
  } finally {
    service.dispose();
  }
}

function lightweightJsUnusedDiagnosticsCacheKey(virtual: VirtualDocument): string {
  return JSON.stringify({
    uri: virtual.uri,
    language: virtual.languageId,
    sourceUri: virtualSourceUri(virtual),
    text: textFingerprint(virtual.text),
  });
}

function pruneLightweightJsUnusedDiagnosticsCache(): void {
  while (lightweightJsUnusedDiagnosticsCache.size > maxLightweightJsUnusedCacheEntries) {
    const oldest = [...lightweightJsUnusedDiagnosticsCache.entries()].sort(
      (left, right) => left[1].lastUsed - right[1].lastUsed,
    )[0];
    if (!oldest) {
      return;
    }
    lightweightJsUnusedDiagnosticsCache.delete(oldest[0]);
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
  const context = cssContext(cached);
  if (!context || context.virtual.languageId !== language) {
    return [];
  }
  const { document, stylesheet, virtual } = context;
  const position = virtual.sourceMap.toVirtualPosition(params.position);
  if (!position) {
    return [];
  }
  return cssService
    .doComplete(document, position, stylesheet)
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

async function jsCompletionAsync(
  cached: CachedDocument,
  params: TextDocumentPositionParams,
): Promise<CompletionItem[]> {
  const context = await jsContextAtAsync(cached, params.position);
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

async function jsHoverAsync(cached: CachedDocument, position: Position): Promise<Hover | null> {
  const context = await jsContextAtAsync(cached, position);
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

async function jsReferencesAsync(cached: CachedDocument, position: Position): Promise<Location[]> {
  const context = await jsContextAtAsync(cached, position);
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

async function jsPrepareRenameAsync(
  cached: CachedDocument,
  position: Position,
): Promise<Range | null> {
  const context = await jsContextAtAsync(cached, position);
  if (!context) {
    return null;
  }
  const info = context.service.getRenameInfo(context.fileName, context.offset, {});
  if (!info.canRename) {
    return null;
  }
  return textSpanToSourceRange(context.virtual, info.triggerSpan) ?? null;
}

async function jsRenameAsync(
  cached: CachedDocument,
  position: Position,
  newName: string,
): Promise<WorkspaceEdit | null> {
  const context = await jsContextAtAsync(cached, position);
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
  const context = cssContext(cached);
  const virtual = context?.virtual;
  const virtualPosition = virtual?.sourceMap.toVirtualPosition(position);
  if (!context || !virtual || !virtualPosition) {
    return null;
  }
  const range = cssService.prepareRename(context.document, virtualPosition, context.stylesheet);
  return range ? (sourceRangeFromVirtualRange(virtual, range) ?? null) : null;
}

function cssRename(
  cached: CachedDocument,
  position: Position,
  newName: string,
): WorkspaceEdit | null {
  const context = cssContext(cached);
  const virtual = context?.virtual;
  const virtualPosition = virtual?.sourceMap.toVirtualPosition(position);
  if (!context || !virtual || !virtualPosition) {
    return null;
  }
  return remapWorkspaceEdit(
    virtual,
    cssService.doRename(context.document, virtualPosition, newName, context.stylesheet),
    cached.source.uri,
  );
}

async function crossLanguageRename(
  cached: CachedDocument,
  position: Position,
  newName: string,
): Promise<WorkspaceEdit | undefined> {
  const target = crossLanguageRenameTarget(cached, position);
  if (!target || !/^[A-Za-z_][A-Za-z0-9_-]*$/.test(newName)) {
    return undefined;
  }
  const changes: NonNullable<WorkspaceEdit["changes"]> = {};
  const seen = new Set<string>();
  for (const candidate of await crossLanguageRenameCandidates(cached)) {
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

async function crossLanguageRenameCandidates(active: CachedDocument): Promise<CachedDocument[]> {
  const settings = cachedSettings(active.source.uri);
  await ensureWorkspaceIndexAsync(settings);
  const candidates: CachedDocument[] = [active];
  const seen = new Set([active.source.uri]);
  for (const document of documents.all()) {
    if (seen.has(document.uri)) {
      continue;
    }
    const cached = await ensureFreshCachedDocumentAsync(document);
    if (cached) {
      seen.add(cached.source.uri);
      candidates.push(cached);
    }
  }
  const indexed = await mapWithConcurrency(
    [...workspaceIndex.values()].filter((entry) => !seen.has(entry.uri)),
    analysisConcurrency(settings),
    async (entry) => {
      seen.add(entry.uri);
      await yieldToEventLoop();
      return cachedFromIndexedAsync(entry, cachedSettings(entry.uri)).catch(() => undefined);
    },
  );
  candidates.push(...indexed.filter((item): item is CachedDocument => Boolean(item)));
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

async function jsSignatureHelpAsync(
  cached: CachedDocument,
  position: Position,
): Promise<SignatureHelp | null> {
  const context = await jsContextAtAsync(cached, position);
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

async function jsDocumentHighlightsAsync(
  cached: CachedDocument,
  position: Position,
): Promise<DocumentHighlight[]> {
  const context = await jsContextAtAsync(cached, position);
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
  const context = cssContext(cached);
  const virtual = context?.virtual;
  const virtualPosition = virtual?.sourceMap.toVirtualPosition(position);
  if (!context || !virtual || !virtualPosition) {
    return [];
  }
  const service = cssService as {
    findDocumentHighlights?: (
      document: TextDocument,
      position: Position,
      stylesheet: unknown,
    ) => DocumentHighlight[];
  };
  return (
    service
      .findDocumentHighlights?.(context.document, virtualPosition, context.stylesheet)
      .map((highlight) => {
        const range = sourceRangeFromVirtualRange(virtual, highlight.range);
        return range ? { ...highlight, range } : undefined;
      })
      .filter((highlight): highlight is DocumentHighlight => Boolean(highlight)) ?? []
  );
}

async function jsInlayHintsAsync(cached: CachedDocument, range: Range): Promise<InlayHint[]> {
  const settings = cachedSettings(cached.source.uri);
  const hints = settings.inlayHints;
  const parameterNamesEnabled = hints?.parameterNames !== false;
  const variableTypesEnabled = hints?.variableTypes === true;
  const functionReturnTypesEnabled = hints?.functionReturnTypes === true;
  if (!parameterNamesEnabled && !variableTypesEnabled && !functionReturnTypesEnabled) {
    return [];
  }
  const hintsByVirtual = await Promise.all(
    jsVirtualDocuments(cached).map(async (virtual) => {
      const sourceStart = cached.source.offsetAt(range.start);
      const sourceEnd = cached.source.offsetAt(range.end);
      const segments = virtual.sourceMap.segments.filter(
        (candidate) => candidate.sourceStart < sourceEnd && candidate.sourceEnd > sourceStart,
      );
      if (segments.length === 0) {
        return [];
      }
      const project = await createJsLanguageServiceAsync(virtual, settings);
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
              includeInlayParameterNameHints: parameterNamesEnabled ? "all" : "none",
              includeInlayVariableTypeHints: variableTypesEnabled,
              includeInlayFunctionLikeReturnTypeHints: functionReturnTypesEnabled,
              includeInlayPropertyDeclarationTypeHints: variableTypesEnabled,
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
    }),
  );
  return hintsByVirtual.flat();
}

async function jsPrepareCallHierarchyAsync(
  cached: CachedDocument,
  position: Position,
): Promise<CallHierarchyItem[]> {
  const context = await jsContextAtAsync(cached, position);
  if (!context) {
    return [];
  }
  const items = context.service.prepareCallHierarchy(context.fileName, context.offset);
  const list = Array.isArray(items) ? items : items ? [items] : [];
  return list
    .map((item) => tsCallHierarchyItemToLsp(context, item, cached.source.uri))
    .filter((item): item is CallHierarchyItem => Boolean(item));
}

async function jsIncomingCallsAsync(item: CallHierarchyItem): Promise<CallHierarchyIncomingCall[]> {
  const context = await jsCallHierarchyContextAsync(item);
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

async function jsOutgoingCallsAsync(item: CallHierarchyItem): Promise<CallHierarchyOutgoingCall[]> {
  const context = await jsCallHierarchyContextAsync(item);
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

async function jsCallHierarchyContextAsync(
  item: CallHierarchyItem,
): Promise<(JsProjectContext & { rootUri: string }) | undefined> {
  const data = item.data as Partial<JsCallHierarchyData> | undefined;
  if (data?.kind !== "javascript" || !data.rootUri || !data.language) {
    return undefined;
  }
  const cached = await getFreshCachedAsync(data.rootUri);
  const virtual = cached
    ? getCachedVirtual(cached, data.language === "jscript" ? "jscript" : "javascript")
    : undefined;
  if (!cached || !virtual || typeof data.position !== "number") {
    return undefined;
  }
  const project = await createJsLanguageServiceAsync(virtual, cachedSettings(data.rootUri));
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

async function vbTypeHierarchyRelatedItemsAsync(
  item: TypeHierarchyItem,
): Promise<TypeHierarchyItem[]> {
  const data = item.data as Partial<VbTypeHierarchyData> | undefined;
  if (data?.kind !== "vbscript" || !data.typeName) {
    return [];
  }
  const cached =
    (await getFreshCachedAsync(data.rootUri ?? item.uri)) ?? (await getFreshCachedAsync(item.uri));
  if (!cached) {
    return [];
  }
  const context = await bestEffortVbProjectContextAsync(cached, cachedSettings(cached.source.uri));
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

async function monikersAtAsync(cached: CachedDocument, position: Position): Promise<Moniker[]> {
  if (isVbscriptPosition(cached, position)) {
    return vbMonikersAt(cached, position);
  }
  if (isJavaScriptPosition(cached, position)) {
    return jsMonikersAtAsync(cached, position);
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

async function jsMonikersAtAsync(cached: CachedDocument, position: Position): Promise<Moniker[]> {
  const context = await jsContextAtAsync(cached, position);
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

async function inlineValuesAsync(cached: CachedDocument, range: Range): Promise<InlineValue[]> {
  return [...vbInlineValues(cached, range), ...(await jsInlineValuesAsync(cached, range))];
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

async function jsInlineValuesAsync(cached: CachedDocument, range: Range): Promise<InlineValue[]> {
  const sourceStart = cached.source.offsetAt(range.start);
  const sourceEnd = cached.source.offsetAt(range.end);
  const seen = new Set<string>();
  const values = await Promise.all(
    jsVirtualDocuments(cached).map(async (virtual) => {
      const project = await createJsLanguageServiceAsync(
        virtual,
        cachedSettings(cached.source.uri),
      );
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
    }),
  );
  return values.flat();
}

function isInlineValueSymbol(kind: VbSymbolKind): boolean {
  return ["variable", "parameter", "constant", "field", "property"].includes(kind);
}

async function resolveJsCompletion(
  item: CompletionItem,
  uri: string,
): Promise<CompletionItem | undefined> {
  const cached = await getFreshCachedAsync(uri);
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
  const project = await createJsLanguageServiceAsync(virtual, cachedSettings(uri));
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

async function aspHoverAsync(
  cached: CachedDocument,
  params: TextDocumentPositionParams,
): Promise<Hover | null> {
  const settings = cachedSettings(cached.source.uri);
  const context = withSourceUriFormatter(await interactiveVbProjectContextAsync(cached, settings));
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

async function definitionLikeLocation(
  uri: string,
  position: Position,
  mode: JavaScriptMode,
): Promise<Location | Location[] | null> {
  const cached = await getFreshCachedAsync(uri);
  if (!cached) {
    return null;
  }
  const region = findRegionAt(cached.parsed, cached.source.offsetAt(position));
  if (!region) {
    return null;
  }
  if (region.language === "vbscript") {
    const context = await interactiveVbProjectContextAsync(
      cached,
      cachedSettings(cached.source.uri),
    );
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
    return jsLocationsAsync(cached, position, mode);
  }
  if (region.language === "css" && mode !== "implementation") {
    const context = cssContext(cached);
    const virtual = context?.virtual;
    const virtualPosition = virtual?.sourceMap.toVirtualPosition(position);
    if (!context || !virtual || !virtualPosition) {
      return null;
    }
    const location = cssService.findDefinition(
      context.document,
      virtualPosition,
      context.stylesheet,
    );
    return location ? (remapLocation(virtual, location) ?? null) : null;
  }
  return null;
}

async function jsLocationsAsync(
  cached: CachedDocument,
  position: Position,
  mode: JavaScriptMode,
): Promise<Location[]> {
  const context = await jsContextAtAsync(cached, position);
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

async function jsContextAtAsync(
  cached: CachedDocument,
  position: Position,
): Promise<JsProjectContext | undefined> {
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
  const project = await createJsLanguageServiceAsync(virtual, cachedSettings(cached.source.uri));
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

async function workspaceSymbolsForCachedAsync(
  cached: CachedDocument,
): Promise<SymbolInformation[]> {
  return [
    ...includeSymbols(cached),
    ...htmlWorkspaceSymbols(cached),
    ...cssWorkspaceSymbols(cached),
    ...(await jsWorkspaceSymbolsAsync(cached)),
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

async function jsWorkspaceSymbolsAsync(cached: CachedDocument): Promise<SymbolInformation[]> {
  return (await jsDocumentSymbolsAsync(cached)).map((symbol) =>
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
  const context = cssContext(cached);
  if (!context) {
    return [];
  }
  const { document, stylesheet, virtual } = context;
  return cssService
    .findDocumentSymbols2(document, stylesheet)
    .map((symbol) => remapDocumentSymbol(virtual, symbol))
    .filter((symbol): symbol is DocumentSymbol => Boolean(symbol));
}

async function jsDocumentSymbolsAsync(cached: CachedDocument): Promise<DocumentSymbol[]> {
  const symbols = await Promise.all(
    jsVirtualDocuments(cached).map(async (virtual) => {
      const project = await createJsLanguageServiceAsync(
        virtual,
        cachedSettings(virtualSourceUri(virtual)),
      );
      const tree = project.service.getNavigationTree(jsProjectFileName(virtual, project));
      return (tree.childItems ?? [])
        .map((item) => navigationTreeToDocumentSymbol(virtual, item))
        .filter((symbol): symbol is DocumentSymbol => Boolean(symbol));
    }),
  );
  return symbols.flat();
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

function documentSymbolWithContainedSelectionRange(symbol: DocumentSymbol): DocumentSymbol {
  return {
    ...symbol,
    range: rangeWithContainedSelectionRange(symbol.range, symbol.selectionRange),
    children: symbol.children?.map(documentSymbolWithContainedSelectionRange),
  };
}

function itemWithContainedSelectionRange<T extends { range: Range; selectionRange: Range }>(
  item: T,
): T {
  const range = rangeWithContainedSelectionRange(item.range, item.selectionRange);
  return range === item.range ? item : { ...item, range };
}

function incomingCallWithContainedSelectionRange(
  call: CallHierarchyIncomingCall,
): CallHierarchyIncomingCall {
  return { ...call, from: itemWithContainedSelectionRange(call.from) };
}

function outgoingCallWithContainedSelectionRange(
  call: CallHierarchyOutgoingCall,
): CallHierarchyOutgoingCall {
  return { ...call, to: itemWithContainedSelectionRange(call.to) };
}

function rangeWithContainedSelectionRange(range: Range, selectionRange: Range): Range {
  return rangeContainsRange(range, selectionRange) ? range : rangeContaining(range, selectionRange);
}

function rangeContainsRange(outer: Range, inner: Range): boolean {
  return (
    comparePositions(outer.start, inner.start) <= 0 && comparePositions(outer.end, inner.end) >= 0
  );
}

function rangeContaining(left: Range, right: Range): Range {
  return {
    start: comparePositions(left.start, right.start) <= 0 ? left.start : right.start,
    end: comparePositions(left.end, right.end) >= 0 ? left.end : right.end,
  };
}

function cssFoldingRanges(cached: CachedDocument): FoldingRange[] {
  const context = cssContext(cached);
  if (!context) {
    return [];
  }
  const { document, virtual } = context;
  return cssService
    .getFoldingRanges(document, {})
    .map((range) => remapFoldingRange(virtual, range))
    .filter((range): range is FoldingRange => Boolean(range));
}

async function jsFoldingRangesAsync(cached: CachedDocument): Promise<FoldingRange[]> {
  const ranges = await Promise.all(
    jsVirtualDocuments(cached).map(async (virtual) => {
      const project = await createJsLanguageServiceAsync(
        virtual,
        cachedSettings(virtualSourceUri(virtual)),
      );
      return project.service
        .getOutliningSpans(jsProjectFileName(virtual, project))
        .map((span) => textSpanToSourceRange(virtual, span.textSpan))
        .filter((range): range is Range => Boolean(range))
        .map((range) => ({ startLine: range.start.line, endLine: range.end.line }));
    }),
  );
  return ranges.flat();
}

function vbscriptFoldingRanges(cached: CachedDocument): FoldingRange[] {
  const context = bestEffortVbProjectContext(cached, cachedSettings(cached.source.uri));
  const symbolRanges = (context.symbols ?? [])
    .filter((symbol) => symbol.sourceUri === cached.source.uri && symbol.scopeRange)
    .map((symbol) => symbol.scopeRange)
    .filter((range): range is Range => Boolean(range))
    .filter((range) => range.start.line < range.end.line)
    .map((range) => ({ startLine: range.start.line, endLine: range.end.line }));
  return dedupeFoldingRanges([...symbolRanges, ...vbscriptBlockFoldingRanges(cached)]);
}

type VbFoldingBlockKind = "If" | "DoLoop" | "While" | "For" | "ForEach";

interface VbFoldingBlock {
  kind: VbFoldingBlockKind;
  start: VbToken;
  branchStart?: VbToken;
}

function vbscriptBlockFoldingRanges(cached: CachedDocument): FoldingRange[] {
  const ranges: FoldingRange[] = [];
  for (const document of vbscriptDocuments(cached.parsed)) {
    const tokens = document.tokens.filter(
      (token) => token.kind !== "whitespace" && token.kind !== "comment",
    );
    const stack: VbFoldingBlock[] = [];
    for (let index = 0; index < tokens.length; index += 1) {
      if (!isVbFoldingStatementStart(tokens, index)) {
        continue;
      }
      const token = tokens[index];
      const first = lowerVbToken(token);
      const second = lowerVbToken(tokens[index + 1]);
      if (!first) {
        continue;
      }
      if (first === "end" && second === "if") {
        closeVbFoldingIfBlock(cached, ranges, stack, token);
        continue;
      }
      if (first === "elseif" || first === "else") {
        closeVbFoldingIfBranch(cached, ranges, stack, token);
        continue;
      }
      if (first === "loop") {
        closeVbFoldingBlock(cached, ranges, stack, ["DoLoop"], token);
        continue;
      }
      if (first === "wend") {
        closeVbFoldingBlock(cached, ranges, stack, ["While"], token);
        continue;
      }
      if (first === "next") {
        closeVbFoldingBlock(cached, ranges, stack, ["For", "ForEach"], token);
        continue;
      }
      if (first === "if" && isVbFoldingMultilineIf(tokens, index)) {
        stack.push({ kind: "If", start: token, branchStart: token });
        continue;
      }
      if (first === "do") {
        stack.push({ kind: "DoLoop", start: token });
        continue;
      }
      if (first === "while") {
        stack.push({ kind: "While", start: token });
        continue;
      }
      if (first === "for") {
        stack.push({ kind: second === "each" ? "ForEach" : "For", start: token });
      }
    }
  }
  return dedupeFoldingRanges(ranges);
}

function vbscriptDocuments(parsed: AspParsedDocument): VbCstNode[] {
  const documents: VbCstNode[] = [];
  const visit = (node: AspCstNode): void => {
    if (node.vbscript) {
      documents.push(node.vbscript);
    }
    for (const child of node.children) {
      visit(child);
    }
  };
  visit(parsed.cst);
  return documents;
}

function closeVbFoldingIfBlock(
  cached: CachedDocument,
  ranges: FoldingRange[],
  stack: VbFoldingBlock[],
  closeToken: VbToken,
): void {
  const index = findLastFoldingBlockIndex(stack, (block) => block.kind === "If");
  if (index === -1) {
    return;
  }
  const [block] = stack.splice(index, 1);
  pushVbFoldingRange(cached, ranges, block.branchStart ?? block.start, closeToken);
}

function closeVbFoldingIfBranch(
  cached: CachedDocument,
  ranges: FoldingRange[],
  stack: VbFoldingBlock[],
  branchToken: VbToken,
): void {
  const block = stack.at(-1);
  if (block?.kind !== "If") {
    return;
  }
  pushVbFoldingRange(cached, ranges, block.branchStart ?? block.start, branchToken, true);
  block.branchStart = branchToken;
}

function closeVbFoldingBlock(
  cached: CachedDocument,
  ranges: FoldingRange[],
  stack: VbFoldingBlock[],
  kinds: VbFoldingBlockKind[],
  closeToken: VbToken,
): void {
  const index = findLastFoldingBlockIndex(stack, (block) => kinds.includes(block.kind));
  if (index === -1) {
    return;
  }
  const [block] = stack.splice(index, 1);
  pushVbFoldingRange(cached, ranges, block.start, closeToken);
}

function pushVbFoldingRange(
  cached: CachedDocument,
  ranges: FoldingRange[],
  startToken: VbToken,
  endToken: VbToken,
  endBeforeToken = false,
): void {
  const startLine = cached.source.positionAt(startToken.start).line;
  const endLine =
    cached.source.positionAt(endBeforeToken ? endToken.start : endToken.end).line -
    (endBeforeToken ? 1 : 0);
  if (startLine < endLine) {
    ranges.push({ startLine, endLine });
  }
}

function isVbFoldingStatementStart(tokens: VbToken[], index: number): boolean {
  const previous = tokens[index - 1];
  return !previous || previous.kind === "newline" || previous.text === ":";
}

function isVbFoldingMultilineIf(tokens: VbToken[], startIndex: number): boolean {
  const endIndex = vbFoldingStatementEndIndex(tokens, startIndex);
  let thenIndex = -1;
  for (let index = startIndex; index <= endIndex; index += 1) {
    if (lowerVbToken(tokens[index]) === "then") {
      thenIndex = index;
    }
  }
  return thenIndex !== -1 && thenIndex === endIndex;
}

function vbFoldingStatementEndIndex(tokens: VbToken[], startIndex: number): number {
  let index = startIndex;
  while (index + 1 < tokens.length) {
    const next = tokens[index + 1];
    if ((next.kind === "newline" && tokens[index]?.text !== "_") || next.text === ":") {
      break;
    }
    index += 1;
  }
  return index;
}

function lowerVbToken(token: VbToken | undefined): string | undefined {
  return token?.text.toLowerCase();
}

function dedupeFoldingRanges(ranges: FoldingRange[]): FoldingRange[] {
  const seen = new Set<string>();
  return ranges.filter((range) => {
    const key = `${range.startLine}:${range.startCharacter ?? ""}:${range.endLine}:${range.endCharacter ?? ""}`;
    if (seen.has(key)) {
      return false;
    }
    seen.add(key);
    return true;
  });
}

function findLastFoldingBlockIndex(
  stack: VbFoldingBlock[],
  predicate: (block: VbFoldingBlock) => boolean,
): number {
  for (let index = stack.length - 1; index >= 0; index -= 1) {
    if (predicate(stack[index])) {
      return index;
    }
  }
  return -1;
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

async function buildVbProjectContextAsync(
  cached: CachedDocument,
  settings: AspSettings,
  options: VbProjectContextBuildOptions = { allowReadMissing: false },
): Promise<VbProjectContext> {
  await hydrateCachedVbscriptCstAsync(cached, settings, "analysis");
  const rootKey = vbProjectRootContextCacheKey(cached, settings);
  const project = await collectCachedVbProjectAnalysisAsync(cached, settings, options);
  const documents = project.documents;
  const contextSettings = vbProjectContextSettings(settings);
  const key = JSON.stringify({
    graph: project.summaryGraphKey,
    settings: contextSettings,
    globals: settings.vbscript?.globals,
  });
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
    includeSummaryUris: project.summaryUris,
    symbols: project.symbols,
    typeEnvironment: project.typeEnvironment,
    externalRefUsages: project.externalRefUsages,
    ...contextSettings,
  };
  rememberVbProjectContext(key, context);
  analysisFor(cached).vbProjectContext = { key, rootKey, context };
  return { ...context, locale: settings.resolvedLocale };
}

async function buildFullVbProjectContextForWorkspaceOperationAsync(
  cached: CachedDocument,
  settings: AspSettings,
): Promise<VbProjectContext> {
  await hydrateCachedVbscriptCstAsync(cached, settings, "workspaceOperation");
  const documents = await collectFullVbProjectDocumentsForWorkspaceOperationAsync(cached, settings);
  const contextSettings = vbProjectContextSettings(settings);
  const key = JSON.stringify({
    mode: "workspaceOperation",
    documents: documents.map((document) => ({
      uri: document.uri,
      vbscript: vbProjectDocumentFingerprint(document),
    })),
    settings: contextSettings,
    globals: settings.vbscript?.globals,
  });
  const globalCached = vbProjectContextCache.get(key);
  if (globalCached) {
    globalCached.lastUsed = Date.now();
    return { ...globalCached.context, locale: settings.resolvedLocale };
  }
  const summaries = await Promise.all(
    documents.map((document) =>
      document.uri === cached.source.uri
        ? cachedFileAnalysisSummaryAsync(cached, contextSettings, settings)
        : summarizeAspFileAnalysisAsync(document, contextSettings),
    ),
  );
  const symbols = summaries.flatMap((summary) => summary.vbscript?.localSymbols ?? []);
  symbols.push(...configuredVbscriptGlobals(cached, settings));
  const context: VbProjectContext = {
    documents,
    includeSummaryUris: summaries.map((summary) => summary.uri),
    symbols,
    typeEnvironment: mergeVbTypeEnvironment(
      buildVbTypeEnvironment(cached.parsed, { ...contextSettings, symbols }),
      summaries.flatMap((summary) => summary.vbscript?.typeFacts ?? []),
      symbols,
    ),
    externalRefUsages: summaries.flatMap((summary) => summary.vbscript?.externalRefUsages ?? []),
    ...contextSettings,
  };
  rememberVbProjectContext(key, context);
  return { ...context, locale: settings.resolvedLocale };
}

async function interactiveVbProjectContextAsync(
  cached: CachedDocument,
  settings: AspSettings,
): Promise<VbProjectContext> {
  return (
    (await interactiveVbProjectContextLookupAsync(cached, settings))?.context ??
    (await buildImmediateLocalVbProjectContextAsync(cached, settings))
  );
}

async function interactiveVbProjectContextLookupAsync(
  cached: CachedDocument,
  settings: AspSettings,
): Promise<CachedVbProjectContextLookup | undefined> {
  if (cached.parsed.includes.length === 0) {
    return cachedVbProjectContextLookup(cached, settings);
  }
  return summaryBackedVbProjectContextLookupAsync(cached, settings, { allowReadMissing: true });
}

function bestEffortVbProjectContext(
  cached: CachedDocument,
  settings: AspSettings,
): VbProjectContext {
  return (
    cachedVbProjectContext(cached, settings) ??
    buildImmediateLocalVbProjectContext(cached, settings)
  );
}

async function bestEffortVbProjectContextAsync(
  cached: CachedDocument,
  settings: AspSettings,
): Promise<VbProjectContext> {
  return (
    cachedVbProjectContext(cached, settings) ??
    (await summaryBackedVbProjectContextLookupAsync(cached, settings))?.context ??
    (await buildImmediateLocalVbProjectContextAsync(cached, settings))
  );
}

function cachedVbProjectContext(
  cached: CachedDocument,
  settings: AspSettings,
): VbProjectContext | undefined {
  return cachedVbProjectContextLookup(cached, settings)?.context;
}

function withSourceUriFormatter(context: VbProjectContext): VbProjectContext {
  return { ...context, sourceUriFormatter: sourceUriDocumentationLink };
}

async function summaryBackedVbProjectContextLookupAsync(
  cached: CachedDocument,
  settings: AspSettings,
  options: VbProjectContextBuildOptions = { allowReadMissing: false },
): Promise<CachedVbProjectContextLookup | undefined> {
  await buildVbProjectContextAsync(cached, settings, options);
  return cachedVbProjectContextLookup(cached, settings);
}

function cachedVbProjectContextLookup(
  cached: CachedDocument,
  settings: AspSettings,
): CachedVbProjectContextLookup | undefined {
  const rootKey = vbProjectRootContextCacheKey(cached, settings);
  const existing = cached.analysis?.vbProjectContext;
  if (existing?.rootKey === rootKey) {
    return {
      key: existing.key,
      context: { ...existing.context, locale: settings.resolvedLocale },
    };
  }
  const summaryGraph = cached.analysis?.vbProjectSummaryGraph;
  if (summaryGraph?.collectionKey === vbProjectDocumentCollectionKey(cached, settings)) {
    const contextSettings = vbProjectContextSettings(settings);
    const key = JSON.stringify({
      graph: summaryGraph.graph.key,
      settings: contextSettings,
      globals: settings.vbscript?.globals,
    });
    const globalCached = vbProjectContextCache.get(key);
    if (globalCached) {
      globalCached.lastUsed = Date.now();
      analysisFor(cached).vbProjectContext = { key, rootKey, context: globalCached.context };
      return {
        key,
        context: { ...globalCached.context, locale: settings.resolvedLocale },
      };
    }
  }
  const documents = cached.analysis?.vbProjectDocuments;
  if (documents?.collectionKey !== vbProjectDocumentCollectionKey(cached, settings)) {
    return undefined;
  }
  const key = vbProjectContextCacheKey(documents.documents, settings);
  const globalCached = vbProjectContextCache.get(key);
  if (!globalCached) {
    return undefined;
  }
  globalCached.lastUsed = Date.now();
  analysisFor(cached).vbProjectContext = { key, rootKey, context: globalCached.context };
  return {
    key,
    context: { ...globalCached.context, locale: settings.resolvedLocale },
  };
}

function buildImmediateLocalVbProjectContext(
  cached: CachedDocument,
  settings: AspSettings,
): VbProjectContext {
  const contextSettings = vbProjectContextSettings(settings);
  const key = JSON.stringify({
    document: vbProjectDocumentFingerprint(cached.parsed),
    settings: {
      typeChecking: contextSettings.typeChecking,
      ifSyntaxDiagnostics: contextSettings.ifSyntaxDiagnostics,
      identifierCase: contextSettings.identifierCase,
      identifierCaseByKind: contextSettings.identifierCaseByKind,
      comTypes: contextSettings.comTypes,
      unusedDiagnostics: contextSettings.unusedDiagnostics,
      syntaxSnippets: contextSettings.syntaxSnippets,
      syntaxKeywords: contextSettings.syntaxKeywords,
    },
    globals: settings.vbscript?.globals,
  });
  const existing = cached.analysis?.immediateLocalVbProjectContext;
  if (existing?.key === key) {
    return { ...existing.context, locale: settings.resolvedLocale };
  }
  const symbols = collectVbscriptSymbols(cached.parsed, contextSettings);
  symbols.push(...configuredVbscriptGlobals(cached, settings));
  const context: VbProjectContext = {
    documents: [cached.parsed],
    includeSummaryUris: [cached.source.uri],
    symbols,
    typeEnvironment: buildVbTypeEnvironment(cached.parsed, { ...contextSettings, symbols }),
    externalRefUsages: [],
    ...contextSettings,
  };
  analysisFor(cached).immediateLocalVbProjectContext = { key, context };
  return { ...context, locale: settings.resolvedLocale };
}

async function buildImmediateLocalVbProjectContextAsync(
  cached: CachedDocument,
  settings: AspSettings,
): Promise<VbProjectContext> {
  const contextSettings = vbProjectContextSettings(settings);
  const key = JSON.stringify({
    document: vbProjectDocumentFingerprint(cached.parsed),
    settings: {
      typeChecking: contextSettings.typeChecking,
      ifSyntaxDiagnostics: contextSettings.ifSyntaxDiagnostics,
      identifierCase: contextSettings.identifierCase,
      identifierCaseByKind: contextSettings.identifierCaseByKind,
      comTypes: contextSettings.comTypes,
      unusedDiagnostics: contextSettings.unusedDiagnostics,
      syntaxSnippets: contextSettings.syntaxSnippets,
      syntaxKeywords: contextSettings.syntaxKeywords,
    },
    globals: settings.vbscript?.globals,
  });
  const existing = cached.analysis?.immediateLocalVbProjectContext;
  if (existing?.key === key) {
    return { ...existing.context, locale: settings.resolvedLocale };
  }
  await hydrateCachedVbscriptCstAsync(cached, settings, "analysis");
  const symbols = await collectVbscriptSymbolsAsync(cached.parsed, contextSettings);
  symbols.push(...configuredVbscriptGlobals(cached, settings));
  const context: VbProjectContext = {
    documents: [cached.parsed],
    includeSummaryUris: [cached.source.uri],
    symbols,
    typeEnvironment: buildVbTypeEnvironment(cached.parsed, { ...contextSettings, symbols }),
    externalRefUsages: [],
    ...contextSettings,
  };
  analysisFor(cached).immediateLocalVbProjectContext = { key, context };
  return { ...context, locale: settings.resolvedLocale };
}

async function workspaceVbscriptReferencesForPosition(
  cached: CachedDocument,
  position: Position,
  options: VbReferenceOptions = {},
): Promise<VbReference[]> {
  const settings = cachedSettings(cached.source.uri);
  const context = await buildFullVbProjectContextForWorkspaceOperationAsync(cached, settings);
  const symbol = getVbscriptDefinition(cached.parsed, position, context);
  return symbol ? workspaceVbscriptReferencesForSymbol(cached, symbol, settings, options) : [];
}

async function workspaceVbscriptReferencesForSymbol(
  cached: CachedDocument,
  symbol: VbSymbol,
  settings: AspSettings,
  options: VbReferenceOptions = {},
): Promise<VbReference[]> {
  const context = await buildFullVbProjectContextForWorkspaceOperationAsync(cached, settings);
  const target = equivalentVbSymbol(context.symbols ?? [], symbol) ?? symbol;
  const references = new Map<string, VbReference>();
  addVbReferences(references, getVbscriptReferencesForSymbol(target, context, options));

  const indexed = await workspaceVbReferenceIndexForSettings(settings);
  const contextUris = new Set((context.documents ?? []).map((document) => document.uri));
  const candidates = workspaceVbReferenceCandidatesForSymbol(indexed, target).filter(
    (candidate) => !contextUris.has(candidate.uri),
  );
  logDebugSummary(
    settings,
    `[asp-lsp] vb.references.workspace.candidates: ${target.sourceUri}, symbol=${target.name}, candidates=${candidates.length}`,
  );

  for (const candidate of candidates) {
    const candidateCached = await cachedForWorkspaceVbReferenceSummary(candidate, settings);
    if (!candidateCached) {
      continue;
    }
    const candidateContext = await buildFullVbProjectContextForWorkspaceOperationAsync(
      candidateCached,
      settings,
    );
    const candidateTarget = equivalentVbSymbol(candidateContext.symbols ?? [], target);
    if (candidateTarget) {
      addVbReferences(
        references,
        getVbscriptReferencesForSymbol(candidateTarget, candidateContext, options),
      );
      continue;
    }
    addVbReferences(references, fallbackWorkspaceExternalReferences(candidate.summary, target));
  }

  return [...references.values()].sort(vbReferenceOrder);
}

async function workspaceVbReferenceIndexForSettings(
  settings: AspSettings,
): Promise<WorkspaceVbReferenceIndex> {
  await ensureWorkspaceIndexAsync(settings);
  const key = workspaceVbReferenceIndexKey(settings);
  if (workspaceVbReferenceIndex?.key === key) {
    workspaceVbReferenceIndex.lastUsed = Date.now();
    logDebugSummary(settings, "[asp-lsp] vb.references.workspaceIndex.hit");
    return workspaceVbReferenceIndex;
  }
  logDebugSummary(settings, "[asp-lsp] vb.references.workspaceIndex.miss");
  const opened = new Set(documents.all().map((document) => document.uri));
  const summaries: WorkspaceVbReferenceSummary[] = [];
  for (const document of documents.all()) {
    const cached = await ensureFreshCachedDocumentAsync(document);
    if (cached) {
      summaries.push(await workspaceVbReferenceSummaryForCachedAsync(cached, settings));
    }
  }
  const indexedSummaries = await mapWithConcurrency(
    [...workspaceIndex.values()].filter((entry) => !opened.has(entry.uri)),
    analysisConcurrency(settings),
    async (entry) => workspaceVbReferenceSummaryForIndexed(entry, settings),
  );
  summaries.push(
    ...indexedSummaries.filter((summary): summary is WorkspaceVbReferenceSummary =>
      Boolean(summary),
    ),
  );

  const byUsageKey = new Map<string, WorkspaceVbReferenceSummary[]>();
  const byMemberName = new Map<string, WorkspaceVbReferenceSummary[]>();
  for (const summary of summaries) {
    for (const usage of summary.summary.vbscript?.externalRefUsages ?? []) {
      pushWorkspaceVbReferenceSummary(byUsageKey, usage.key, summary);
      if (usage.memberName) {
        pushWorkspaceVbReferenceSummary(byMemberName, usage.memberName.toLowerCase(), summary);
      }
    }
  }
  workspaceVbReferenceIndex = {
    key,
    summaries,
    byUsageKey,
    byMemberName,
    lastUsed: Date.now(),
  };
  logDebugSummary(
    settings,
    `[asp-lsp] vb.references.workspaceIndex.built: files=${summaries.length}, usageKeys=${byUsageKey.size}, memberKeys=${byMemberName.size}`,
  );
  return workspaceVbReferenceIndex;
}

function workspaceVbReferenceIndexKey(settings: AspSettings): string {
  return JSON.stringify({
    workspaceGeneration,
    workspace: workspaceIndexSettingsIdentity(settings),
    parse: parseSettingsIdentity(settings),
    include: includeResolutionSettingsIdentity(settings),
    vbscript: vbProjectContextSettings(settings),
    opened: documents.all().map((document) => documentIdentityFor(document)),
    indexed: [...workspaceIndex.values()]
      .map((entry) => ({
        uri: entry.uri,
        mtimeMs: entry.mtimeMs,
        size: entry.size,
      }))
      .sort((left, right) => left.uri.localeCompare(right.uri)),
  });
}

async function workspaceVbReferenceSummaryForCachedAsync(
  cached: CachedDocument,
  settings: AspSettings,
): Promise<WorkspaceVbReferenceSummary> {
  const fileName = normalizeFileName(uriToFileName(cached.source.uri));
  return {
    uri: cached.source.uri,
    fileName,
    summary: await cachedFileAnalysisSummaryAsync(
      cached,
      vbProjectContextSettings(settings),
      settings,
    ),
  };
}

async function workspaceVbReferenceSummaryForIndexed(
  entry: WorkspaceIndexedDocument,
  settings: AspSettings,
): Promise<WorkspaceVbReferenceSummary | undefined> {
  try {
    const text = await readTextFileAsync(entry.fileName, settings.legacyEncoding);
    return {
      uri: entry.uri,
      fileName: entry.fileName,
      summary: await summarizeAspFileAnalysisFromTextAsync(
        entry.uri,
        text,
        settings,
        vbProjectContextSettings(settings),
      ),
    };
  } catch {
    return undefined;
  }
}

async function cachedForWorkspaceVbReferenceSummary(
  summary: WorkspaceVbReferenceSummary,
  settings: AspSettings,
): Promise<CachedDocument | undefined> {
  const document = documents.get(summary.uri);
  if (document) {
    return ensureFreshCachedDocumentAsync(document);
  }
  try {
    const text = await readTextFileAsync(summary.fileName, settings.legacyEncoding);
    const parsed = await parseAspDocumentAsync(summary.uri, text, settings);
    // 参照解決などは parsed.cst を直接 walk するため、浅い CST を VB CST で full 化する。
    await hydrateVbscriptCst(parsed, settings);
    return createCachedDocument(
      TextDocument.create(summary.uri, "classic-asp", 0, text),
      parsed,
      settings,
    );
  } catch {
    return undefined;
  }
}

function workspaceVbReferenceCandidatesForSymbol(
  index: WorkspaceVbReferenceIndex,
  symbol: VbSymbol,
): WorkspaceVbReferenceSummary[] {
  const candidates = new Map<string, WorkspaceVbReferenceSummary>();
  const usageKey = vbSymbolExternalUsageKey(symbol);
  if (usageKey) {
    for (const candidate of index.byUsageKey.get(usageKey) ?? []) {
      candidates.set(candidate.uri, candidate);
    }
  }
  if (symbol.memberOf) {
    for (const candidate of index.byMemberName.get(symbol.name.toLowerCase()) ?? []) {
      candidates.set(candidate.uri, candidate);
    }
  }
  return [...candidates.values()];
}

function fallbackWorkspaceExternalReferences(
  summary: FileAnalysisSummary,
  symbol: VbSymbol,
): VbReference[] {
  if (!isGlobalWorkspaceReferenceFallbackSymbol(symbol)) {
    return [];
  }
  const usageKey = vbSymbolExternalUsageKey(symbol);
  if (!usageKey) {
    return [];
  }
  return (summary.vbscript?.externalRefUsages ?? [])
    .filter((usage) => usage.key === usageKey)
    .flatMap((usage) => usage.ranges.map((range) => ({ uri: summary.uri, range })));
}

function isGlobalWorkspaceReferenceFallbackSymbol(symbol: VbSymbol): boolean {
  return (
    !symbol.scopeName &&
    !symbol.memberOf &&
    symbol.visibility !== "private" &&
    ["function", "sub", "class"].includes(symbol.kind)
  );
}

function vbSymbolExternalUsageKey(symbol: VbSymbol): string | undefined {
  if (symbol.memberOf) {
    return undefined;
  }
  return symbol.name.toLowerCase();
}

function equivalentVbSymbol(symbols: VbSymbol[], target: VbSymbol): VbSymbol | undefined {
  return symbols.find((symbol) => sameVbSymbolIdentity(symbol, target));
}

function sameVbSymbolIdentity(left: VbSymbol, right: VbSymbol): boolean {
  return (
    left.sourceUri === right.sourceUri &&
    left.name.toLowerCase() === right.name.toLowerCase() &&
    left.kind === right.kind &&
    (left.memberOf ?? "").toLowerCase() === (right.memberOf ?? "").toLowerCase() &&
    left.range.start.line === right.range.start.line &&
    left.range.start.character === right.range.start.character &&
    left.range.end.line === right.range.end.line &&
    left.range.end.character === right.range.end.character
  );
}

function addVbReferences(target: Map<string, VbReference>, references: VbReference[]): void {
  for (const reference of references) {
    target.set(vbReferenceKey(reference), reference);
  }
}

function vbReferenceKey(reference: VbReference): string {
  return JSON.stringify({
    uri: reference.uri,
    range: reference.range,
  });
}

function vbReferenceOrder(left: VbReference, right: VbReference): number {
  return (
    left.uri.localeCompare(right.uri) ||
    left.range.start.line - right.range.start.line ||
    left.range.start.character - right.range.start.character ||
    left.range.end.line - right.range.end.line ||
    left.range.end.character - right.range.end.character
  );
}

function pushWorkspaceVbReferenceSummary(
  map: Map<string, WorkspaceVbReferenceSummary[]>,
  key: string,
  summary: WorkspaceVbReferenceSummary,
): void {
  const summaries = map.get(key) ?? [];
  summaries.push(summary);
  map.set(key, summaries);
}

async function refreshIncludeStateForAspChangesAsync(
  changes: WatchedAspFileChange[],
): Promise<IncludeStateRefreshResult> {
  const includeRefsChangedFiles = new Set<string>();
  const publicChangedFiles = new Set<string>();
  const previousIncludeRefs = new Map<string, IncludeRefsCacheEntry | undefined>();
  for (const change of changes) {
    const fileName = normalizeFileName(change.fileName);
    previousIncludeRefs.set(fileName, includeDocumentLoader.cachedIncludeRefs(fileName));
  }
  includeDocumentLoader.invalidateFiles(changes.map((change) => change.fileName));
  invalidateGraphFileIndexFiles(changes.map((change) => change.fileName));
  for (const change of changes) {
    const fileName = normalizeFileName(change.fileName);
    const previous = includeDocumentLoader.cachedPublicSummary(fileName);
    const uri = pathToFileUri(fileName);
    const settings = cachedSettings(uri);
    const nextIncludeRefs =
      change.type === FileChangeType.Deleted
        ? undefined
        : await includeDocumentLoader.readIncludeRefsAsync(fileName, settings, {
            allowRead: true,
          });
    const previousIncludeFingerprint = previousIncludeRefs.get(fileName)?.fingerprint ?? "missing";
    const nextIncludeFingerprint = nextIncludeRefs?.fingerprint ?? "missing";
    if (previousIncludeFingerprint === nextIncludeFingerprint) {
      logDebugSummary(
        settings,
        `[asp-lsp] include.refs.reuse: ${uri}, fingerprint=${nextIncludeFingerprint}`,
      );
    } else {
      includeRefsChangedFiles.add(fileName);
      aspProjectBuilderState.markFileAffected(fileName, "watchedAsp.includeRefs");
      logInvalidation(
        "includeRefs",
        `watchedAsp.changed, uri=${uri}, previous=${previousIncludeFingerprint}, next=${nextIncludeFingerprint}`,
      );
    }
    const next =
      change.type === FileChangeType.Deleted
        ? undefined
        : await includeDocumentLoader.readSummaryAsync(fileName, settings, {
            allowRead: true,
          });
    const nextFingerprint = next?.publicFingerprint ?? "missing";
    if (previous?.publicFingerprint === nextFingerprint) {
      logDebugSummary(
        settings,
        `[asp-lsp] include.publicBoundary.reuse: ${uri}, fingerprint=${nextFingerprint}`,
      );
      continue;
    }
    publicChangedFiles.add(fileName);
    aspProjectBuilderState.markFileAffected(fileName, "watchedAsp.publicBoundary");
    logInvalidation(
      "includePublicBoundary",
      `watchedAsp.changed, uri=${uri}, previous=${previous?.publicFingerprint ?? "missing"}, next=${nextFingerprint}`,
    );
  }
  return { includeRefsChangedFiles, publicChangedFiles };
}

async function ensureIncludeGraphForOpenDocumentsAsync(changedFiles: Set<string>): Promise<void> {
  const changedUris = new Set([...changedFiles].map(pathToFileUri));
  for (const document of documents.all()) {
    const cached = await ensureFreshDiagnosticsCachedDocumentAsync(document);
    const existing = includeForwardDependencies.get(cached.source.uri);
    const affected =
      changedUris.has(cached.source.uri) ||
      (existing ? setsIntersect(existing, changedUris) : false) ||
      reverseDependenciesInclude(changedUris, cached.source.uri);
    if (!affected) {
      continue;
    }
    await collectIncludeDependencyGraphForCachedAsync(cached, cachedSettings(cached.source.uri));
  }
}

async function collectIncludeDependencyGraphForCachedAsync(
  cached: CachedDocument,
  settings: AspSettings,
): Promise<void> {
  const limits = vbProjectContextLimits(settings);
  const visited = new Set<string>([cached.source.uri]);
  let textLength = cached.parsed.text.length;
  let truncatedReason: string | undefined;
  resetIncludeDependencies(cached.source.uri);

  const noteTruncated = (reason: string): void => {
    truncatedReason ??= reason;
  };

  const visitRefs = async (
    ownerUri: string,
    includeRefs: AspInclude[],
    depth: number,
  ): Promise<void> => {
    if (depth > 20) {
      noteTruncated("depth>20");
      return;
    }
    for (const include of includeRefs) {
      const resolved = await resolveIncludePathDetailsAsync(
        ownerUri,
        include.path,
        include.mode,
        settings,
      );
      const includeUri = pathToFileUri(resolved.fileName);
      recordIncludeDependency(cached.source.uri, includeUri);
      if (!resolved.exists || visited.has(includeUri)) {
        continue;
      }
      if (visited.size >= limits.maxDocuments) {
        noteTruncated(`documents>${limits.maxDocuments}`);
        continue;
      }
      const size = await fileSizeAsync(resolved.fileName);
      if (size !== undefined && textLength + size > limits.maxTextLength) {
        noteTruncated(`text>${limits.maxTextLength}`);
        continue;
      }
      const entry = await includeDocumentLoader.readIncludeRefsAsync(resolved.fileName, settings, {
        allowRead: true,
      });
      if (!entry) {
        scheduleIncludeSummaryRefresh(
          cached.source.uri,
          resolved.fileName,
          settings,
          "includeGraph.missingRefs",
        );
        continue;
      }
      visited.add(entry.uri);
      textLength += entry.source.size;
      await visitRefs(entry.uri, entry.includeRefs, depth + 1);
    }
  };

  await visitRefs(cached.source.uri, cached.parsed.includes, 0);
  if (truncatedReason) {
    logDebugSummary(
      settings,
      `[asp-lsp] includeGraph.truncated: ${cached.source.uri}, files=${visited.size}, text=${textLength}, reason=${truncatedReason}`,
    );
  } else {
    logDebugSummary(
      settings,
      `[asp-lsp] includeGraph.built: ${cached.source.uri}, files=${visited.size}, text=${textLength}`,
    );
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

function reverseDependenciesInclude(includeUris: Set<string>, ownerUri: string): boolean {
  for (const includeUri of includeUris) {
    if (includeReverseDependencies.get(includeUri)?.has(ownerUri)) {
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
    completionSessionCache.clearUris(uris, reason);
    invalidateGraphFileIndexFiles(
      [...uris].filter((uri) => uri.startsWith("file://")).map(uriToFileName),
    );
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

function requestSemanticTokensRefresh(reason: string): void {
  if (semanticTokensRefreshSupported) {
    try {
      void Promise.resolve(connection.languages.semanticTokens.refresh()).catch((error: unknown) =>
        connection.console.warn(
          `[asp-lsp] semanticTokens.refresh.failed: reason=${reason}, error=${errorMessage(error)}`,
        ),
      );
    } catch (error) {
      connection.console.warn(
        `[asp-lsp] semanticTokens.refresh.failed: reason=${reason}, error=${errorMessage(error)}`,
      );
    }
  }
}

function requestVisualRefresh(reason: string): void {
  requestSemanticTokensRefresh(reason);
  if (inlayHintRefreshSupported) {
    void connection.languages.inlayHint
      .refresh()
      .catch((error: unknown) =>
        connection.console.warn(
          `[asp-lsp] inlayHint.refresh.failed: reason=${reason}, error=${errorMessage(error)}`,
        ),
      );
  }
}

function vbProjectContextSettings(
  settings: AspSettings,
): Omit<VbProjectContext, "documents" | "symbols" | "typeEnvironment" | "locale"> {
  return {
    typeChecking: settings.vbscript?.typeChecking,
    ifSyntaxDiagnostics: settings.vbscript?.ifSyntaxDiagnostics ?? "basic",
    identifierCase: settings.vbscript?.identifierCase,
    identifierCaseByKind: settings.vbscript?.identifierCaseByKind,
    comTypes: settings.vbscript?.comTypes,
    unusedDiagnostics: settings.vbscript?.unusedDiagnostics !== false,
    syntaxSnippets: settings.vbscript?.syntaxSnippets !== false,
    syntaxKeywords: settings.vbscript?.syntaxKeywords !== false,
  };
}

function vbProjectRootContextCacheKey(cached: CachedDocument, settings: AspSettings): string {
  return JSON.stringify({
    root: vbProjectDocumentCollectionKey(cached, settings),
    settings: {
      typeChecking: settings.vbscript?.typeChecking,
      ifSyntaxDiagnostics: settings.vbscript?.ifSyntaxDiagnostics ?? "basic",
      identifierCase: settings.vbscript?.identifierCase,
      identifierCaseByKind: settings.vbscript?.identifierCaseByKind,
      comTypes: settings.vbscript?.comTypes,
      unusedDiagnostics: settings.vbscript?.unusedDiagnostics !== false,
      syntaxSnippets: settings.vbscript?.syntaxSnippets !== false,
      syntaxKeywords: settings.vbscript?.syntaxKeywords !== false,
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
      ifSyntaxDiagnostics: settings.vbscript?.ifSyntaxDiagnostics ?? "basic",
      identifierCase: settings.vbscript?.identifierCase,
      identifierCaseByKind: settings.vbscript?.identifierCaseByKind,
      comTypes: settings.vbscript?.comTypes,
      unusedDiagnostics: settings.vbscript?.unusedDiagnostics !== false,
      syntaxSnippets: settings.vbscript?.syntaxSnippets !== false,
      syntaxKeywords: settings.vbscript?.syntaxKeywords !== false,
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
    serverObjects: document.serverObjects.map((serverObject) => ({
      offset: serverObject.offset,
      id: serverObject.id,
      progId: serverObject.progId,
      classId: serverObject.classId,
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

async function collectCachedVbProjectAnalysisAsync(
  cached: CachedDocument,
  settings: AspSettings,
  options: VbProjectContextBuildOptions = { allowReadMissing: false },
): Promise<VbProjectAnalysis> {
  const graph = await collectCachedVbProjectSummaryGraphAsync(cached, settings, options);
  const key = vbProjectAnalysisCacheKey(graph, settings);
  const existing = cached.analysis?.vbProjectAnalysis;
  if (existing?.key === key) {
    return existing.analysis;
  }
  const contextSettings = vbProjectContextSettings(settings);
  const summaries = graph.summaries;
  const symbols = summaries.flatMap((summary) => summary.vbscript?.localSymbols ?? []);
  symbols.push(...configuredVbscriptGlobals(cached, settings));
  const typeEnvironment = mergeVbTypeEnvironment(
    buildVbTypeEnvironment(cached.parsed, { ...contextSettings, symbols }),
    summaries.flatMap((summary) => summary.vbscript?.typeFacts ?? []),
    symbols,
  );
  const analysis = {
    documents: graph.documents,
    summaryUris: graph.summaries.map((summary) => summary.uri),
    summaries,
    summaryGraphKey: graph.key,
    complete: graph.complete,
    symbols,
    typeEnvironment,
    externalRefUsages: summaries.flatMap((summary) => summary.vbscript?.externalRefUsages ?? []),
  };
  analysisFor(cached).vbProjectAnalysis = { key, analysis };
  return analysis;
}

function vbProjectAnalysisCacheKey(graph: VbProjectSummaryGraph, settings: AspSettings): string {
  return JSON.stringify({
    graph: graph.key,
    context: vbProjectContextSettings(settings),
    globals: settings.vbscript?.globals,
  });
}

async function cachedFileAnalysisSummaryAsync(
  cached: CachedDocument,
  context: VbProjectContext,
  settings: AspSettings = cachedSettings(cached.source.uri),
): Promise<FileAnalysisSummary> {
  const key = JSON.stringify({
    document: vbProjectDocumentFingerprint(cached.parsed),
    context: {
      typeChecking: context.typeChecking,
      ifSyntaxDiagnostics: context.ifSyntaxDiagnostics,
      identifierCase: context.identifierCase,
      identifierCaseByKind: context.identifierCaseByKind,
      comTypes: context.comTypes,
      unusedDiagnostics: context.unusedDiagnostics,
      syntaxSnippets: context.syntaxSnippets,
      syntaxKeywords: context.syntaxKeywords,
    },
  });
  const existing = cached.analysis?.vbFileSummary;
  if (existing?.key === key) {
    aspProjectBuilderState.updateFromSummary(cached, existing.summary, settings, "summary.reuse");
    return existing.summary;
  }
  await hydrateCachedVbscriptCstAsync(cached, settings, "summary");
  const summary = await summarizeAspFileAnalysisAsync(cached.parsed, context);
  analysisFor(cached).vbFileSummary = { key, summary };
  aspProjectBuilderState.updateFromSummary(cached, summary, settings, "summary.update");
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

async function collectCachedVbProjectDocumentsAsync(
  cached: CachedDocument,
  settings: AspSettings,
): Promise<AspParsedDocument[]> {
  const graph = await collectCachedVbProjectSummaryGraphAsync(cached, settings, {
    allowReadMissing: true,
  });
  return graph.documents;
}

async function collectFullVbProjectDocumentsForWorkspaceOperationAsync(
  cached: CachedDocument,
  settings: AspSettings,
): Promise<AspParsedDocument[]> {
  const limits = vbProjectContextLimits(settings);
  const documents: AspParsedDocument[] = [cached.parsed];
  const visited = new Set<string>([cached.source.uri]);
  let textLength = cached.parsed.text.length;
  let truncatedReason: string | undefined;

  const noteTruncated = (reason: string): void => {
    truncatedReason ??= reason;
  };

  const visit = async (document: AspParsedDocument, depth: number): Promise<void> => {
    if (depth > 20) {
      noteTruncated("depth>20");
      return;
    }
    for (const include of document.includes) {
      const resolved = await resolveIncludePathDetailsAsync(
        document.uri,
        include.path,
        include.mode,
        settings,
      );
      const includeUri = pathToFileUri(resolved.fileName);
      recordIncludeDependency(cached.source.uri, includeUri);
      if (!resolved.exists || visited.has(includeUri)) {
        continue;
      }
      if (documents.length >= limits.maxDocuments) {
        noteTruncated(`documents>${limits.maxDocuments}`);
        continue;
      }
      const size = await fileSizeAsync(resolved.fileName);
      if (size !== undefined && textLength + size > limits.maxTextLength) {
        noteTruncated(`text>${limits.maxTextLength}`);
        continue;
      }
      const entry = await includeDocumentLoader.readAsync(resolved.fileName, settings);
      if (!entry) {
        continue;
      }
      visited.add(entry.uri);
      documents.push(entry.parsed);
      textLength += entry.parsed.text.length;
      await visit(entry.parsed, depth + 1);
      await yieldToEventLoop();
    }
  };

  await visit(cached.parsed, 0);
  if (truncatedReason) {
    logDebugSummary(
      settings,
      `[asp-lsp] vbProject.workspaceOperation.documents.truncated: ${cached.source.uri}, documents=${documents.length}, text=${textLength}, reason=${truncatedReason}`,
    );
  }
  return documents;
}

async function collectCachedVbProjectSummaryGraphAsync(
  cached: CachedDocument,
  settings: AspSettings,
  options: { allowReadMissing: boolean },
): Promise<VbProjectSummaryGraph> {
  const collectionKey = vbProjectDocumentCollectionKey(cached, settings);
  const existing = cached.analysis?.vbProjectSummaryGraph;
  if (
    existing?.collectionKey === collectionKey &&
    (existing.graph.complete || !options.allowReadMissing)
  ) {
    logDebugSummary(
      settings,
      `[asp-lsp] vbProject.summaryGraph.reuse: complete=${existing.graph.complete}, summaries=${existing.graph.summaries.length}`,
    );
    return existing.graph;
  }
  const graph = await collectVbProjectSummaryGraphAsync(cached, settings, options);
  analysisFor(cached).vbProjectSummaryGraph = { collectionKey, graph };
  analysisFor(cached).vbProjectDocuments = {
    collectionKey,
    documents: graph.documents,
  };
  return graph;
}

async function collectVbProjectSummaryGraphAsync(
  cached: CachedDocument,
  settings: AspSettings,
  options: { allowReadMissing: boolean },
): Promise<VbProjectSummaryGraph> {
  const limits = vbProjectContextLimits(settings);
  const contextSettings = vbProjectContextSettings(settings);
  const rootSummary = await cachedFileAnalysisSummaryAsync(cached, contextSettings, settings);
  const summaries: FileAnalysisSummary[] = [rootSummary];
  const projectDocuments: AspParsedDocument[] = [cached.parsed];
  const visited = new Set<string>([cached.source.uri]);
  const missingFiles: string[] = [];
  let textLength = cached.parsed.text.length;
  let truncatedReason: string | undefined;
  resetIncludeDependencies(cached.source.uri);

  const noteTruncated = (reason: string): void => {
    truncatedReason ??= reason;
  };

  const visitSummary = async (owner: FileAnalysisSummary, depth: number): Promise<void> => {
    if (depth > 20) {
      noteTruncated("depth>20");
      return;
    }
    for (const include of owner.includeRefs) {
      const resolved = await resolveIncludePathDetailsAsync(
        owner.uri,
        include.path,
        include.mode,
        settings,
      );
      const includeUri = pathToFileUri(resolved.fileName);
      recordIncludeDependency(cached.source.uri, includeUri);
      if (!resolved.exists || visited.has(includeUri)) {
        continue;
      }
      if (summaries.length >= limits.maxDocuments) {
        noteTruncated(`documents>${limits.maxDocuments}`);
        continue;
      }
      const size = await fileSizeAsync(resolved.fileName);
      if (size !== undefined && textLength + size > limits.maxTextLength) {
        noteTruncated(`text>${limits.maxTextLength}`);
        continue;
      }
      const entry = await includeDocumentLoader.readSummaryAsync(resolved.fileName, settings, {
        allowRead: options.allowReadMissing,
      });
      if (!entry) {
        missingFiles.push(normalizeFileName(resolved.fileName));
        if (!options.allowReadMissing) {
          scheduleIncludeSummaryRefresh(
            cached.source.uri,
            resolved.fileName,
            settings,
            "summaryGraph.missing",
          );
        }
        continue;
      }
      visited.add(entry.uri);
      textLength += entry.source.size;
      summaries.push(entry.summary);
      if (entry.parsed) {
        projectDocuments.push(entry.parsed);
      }
      await visitSummary(entry.summary, depth + 1);
    }
  };

  await visitSummary(rootSummary, 0);
  const graph = {
    rootSummary,
    summaries,
    documents: projectDocuments,
    key: vbProjectSummaryGraphKey(rootSummary, summaries, {
      complete: missingFiles.length === 0 && !truncatedReason,
      missingFiles,
      truncatedReason,
      textLength,
      settings,
    }),
    complete: missingFiles.length === 0 && !truncatedReason,
    missingFiles,
    truncatedReason,
    textLength,
  };
  if (missingFiles.length > 0) {
    logDebugSummary(
      settings,
      `[asp-lsp] vbProject.summaryGraph.missing: ${cached.source.uri}, files=${missingFiles.length}`,
    );
  }
  if (truncatedReason) {
    logDebugSummary(
      settings,
      `[asp-lsp] vbProject.summaryGraph.truncated: ${cached.source.uri}, summaries=${summaries.length}, text=${textLength}, reason=${truncatedReason}`,
    );
    logDebugSummary(
      settings,
      `[asp-lsp] vbProject.documents.truncated: ${cached.source.uri}, documents=${summaries.length}, text=${textLength}, reason=${truncatedReason}`,
    );
  } else {
    logDebugSummary(
      settings,
      `[asp-lsp] vbProject.summaryGraph.built: ${cached.source.uri}, summaries=${summaries.length}, text=${textLength}, complete=${missingFiles.length === 0}`,
    );
  }
  return graph;
}

function vbProjectSummaryGraphKey(
  rootSummary: FileAnalysisSummary,
  summaries: FileAnalysisSummary[],
  state: {
    complete: boolean;
    missingFiles: string[];
    truncatedReason?: string;
    textLength: number;
    settings: AspSettings;
  },
): string {
  return JSON.stringify({
    root: rootSummary.fingerprint,
    summaries: summaries.map((summary) => ({
      uri: summary.uri,
      fingerprint: summary.fingerprint,
      publicSignature: filePublicSignature(summary).fingerprint,
    })),
    complete: state.complete,
    missingFiles: state.missingFiles,
    truncatedReason: state.truncatedReason,
    textLength: state.textLength,
    limits: vbProjectContextLimits(state.settings),
    resolution: includeResolutionSettingsKey(state.settings),
  });
}

function scheduleIncludeSummaryRefresh(
  ownerUri: string,
  fileName: string,
  settings: AspSettings,
  reason: string,
): void {
  const normalized = normalizeFileName(fileName);
  const key = JSON.stringify({
    fileName: normalized,
    settings: includeSummarySettingsKey(settings),
  });
  if (pendingIncludeSummaryRefreshes.has(key)) {
    return;
  }
  const promise = includeDocumentLoader
    .readSummaryAsync(normalized, settings, { allowRead: true })
    .then((entry) => {
      if (!entry) {
        return;
      }
      const affected = new Set<string>();
      if (documents.get(ownerUri)) {
        affected.add(ownerUri);
      }
      for (const dependent of includeReverseDependencies.get(entry.uri) ?? []) {
        if (documents.get(dependent)) {
          affected.add(dependent);
        }
      }
      if (affected.size === 0) {
        return;
      }
      invalidateCachedAnalysisForUris(affected, reason);
      requestVisualRefresh(reason);
      for (const document of documents.all().filter((item) => affected.has(item.uri))) {
        validate(document);
      }
    })
    .catch((error) =>
      connection.console.warn(
        `[asp-lsp] includeSummary.refresh.failed: ${pathToFileUri(normalized)}, reason=${errorMessage(error)}`,
      ),
    )
    .finally(() => {
      pendingIncludeSummaryRefreshes.delete(key);
    });
  pendingIncludeSummaryRefreshes.set(key, promise);
  logDebugSummary(
    settings,
    `[asp-lsp] includeSummary.refresh.scheduled: ${pathToFileUri(normalized)}`,
  );
}

function vbProjectDocumentCollectionKey(cached: CachedDocument, settings: AspSettings): string {
  return JSON.stringify({
    uri: cached.source.uri,
    defaultLanguage: cached.parsed.defaultLanguage,
    includes: cached.parsed.includes.map((include) => ({
      path: include.path,
      mode: include.mode,
    })),
    limits: vbProjectContextLimits(settings),
    resolution: includeResolutionSettingsKey(settings),
  });
}

function vbProjectContextLimits(settings: AspSettings): VbProjectContextLimits {
  return {
    maxDocuments:
      settings.workspace?.vbProjectMaxDocuments ??
      positiveIntegerFromEnv("ASP_LSP_VB_PROJECT_MAX_DOCUMENTS", 32),
    maxTextLength:
      settings.workspace?.vbProjectMaxTextLength ??
      positiveIntegerFromEnv("ASP_LSP_VB_PROJECT_MAX_TEXT_LENGTH", 1024 * 1024),
  };
}

function positiveIntegerFromEnv(name: string, fallback: number): number {
  const value = Number.parseInt(process.env[name] ?? "", 10);
  return Number.isFinite(value) && value > 0 ? Math.floor(value) : fallback;
}

interface IncludeDocumentSourceIdentity {
  key: string;
  source: DiskAnalysisSourceMetadata;
  text?: string;
  diskBacked: boolean;
}

async function includeDocumentSourceIdentityAsync(
  fileName: string,
  settings: AspSettings,
): Promise<IncludeDocumentSourceIdentity | undefined> {
  const uri = pathToFileUri(fileName);
  const openDocument = documents.get(uri);
  if (openDocument) {
    const text = openDocument.getText();
    const source = {
      fileName,
      mtimeMs: openDocument.version,
      size: text.length,
    };
    return {
      key: JSON.stringify({
        fileName,
        openVersion: openDocument.version,
        text: textFingerprint(text),
        settings: includeDocumentSettingsIdentity(settings),
      }),
      source,
      text,
      diskBacked: false,
    };
  }
  const stat = await fs.promises.stat(fileName).catch(() => undefined);
  if (!stat?.isFile()) {
    return undefined;
  }
  const source = {
    fileName,
    mtimeMs: stat.mtimeMs,
    size: stat.size,
  };
  return {
    key: JSON.stringify({
      ...source,
      settings: includeDocumentSettingsIdentity(settings),
    }),
    source,
    diskBacked: true,
  };
}

function includeDocumentSettingsIdentity(settings: AspSettings): string {
  return JSON.stringify({
    parse: parseSettingsIdentity(settings),
    legacyEncoding: settings.legacyEncoding,
    vbscript: vbProjectContextSettings(settings),
  });
}

function includeSummarySettingsKey(settings: AspSettings): string {
  return includeDocumentSettingsIdentity(settings);
}

function includeRefsSettingsKey(settings: AspSettings): string {
  return JSON.stringify({
    scanner: "asp-include-refs-v1",
    legacyEncoding: settings.legacyEncoding,
  });
}

function graphFileIndexSettingsKey(settings: AspSettings): string {
  return JSON.stringify({
    scanner: "asp-graph-file-index-v1",
    parse: parseSettingsIdentity(settings),
    legacyEncoding: settings.legacyEncoding,
    vbscript: vbProjectContextSettings(settings),
  });
}

function includeRefsCacheKey(
  fileName: string,
  source: DiskAnalysisSourceMetadata,
  settings: AspSettings,
): string {
  return JSON.stringify({
    fileName: normalizeFileName(fileName),
    source,
    settings: includeRefsSettingsKey(settings),
  });
}

function includeSummaryCacheEntryFromDisk(
  fileName: string,
  key: string,
  entry: DiskSummaryCacheEntry,
): IncludeSummaryCacheEntry {
  const publicSignature =
    (entry.publicSignature as FilePublicSignature | undefined) ??
    filePublicSignature(entry.summary);
  return {
    key,
    fileName,
    uri: entry.summary.uri,
    source: entry.source,
    summary: entry.summary,
    publicFingerprint: publicSignature.fingerprint,
    publicSignature,
  };
}

function includeRefsCacheEntryFromDisk(
  fileName: string,
  key: string,
  entry: DiskIncludeRefsCacheEntry,
): IncludeRefsCacheEntry {
  return {
    key,
    fileName,
    uri: pathToFileUri(fileName),
    source: entry.source,
    includeRefs: entry.includeRefs,
    fingerprint: entry.fingerprint,
  };
}

function graphFileIndexFromDisk(
  fileName: string,
  key: string,
  entry: DiskVbSymbolIndexCacheEntry,
  includeRefsEntry?: IncludeRefsCacheEntry,
): GraphFileIndex {
  const includeRefs =
    includeRefsEntry && sameDiskAnalysisSource(includeRefsEntry.source, entry.source)
      ? includeRefsEntry.includeRefs
      : entry.index.includeRefs;
  return {
    key,
    fileName,
    uri: pathToFileUri(fileName),
    source: entry.source,
    includeRefs,
    vbSymbolIndex: { ...entry.index, includeRefs },
    fingerprint: entry.fingerprint,
    lastUsed: Date.now(),
  };
}

function includeRefsCacheEntryFromSummary(
  entry: IncludeSummaryCacheEntry,
  settings: AspSettings,
): IncludeRefsCacheEntry {
  return {
    key: includeRefsCacheKey(entry.fileName, entry.source, settings),
    fileName: entry.fileName,
    uri: entry.uri,
    source: entry.source,
    includeRefs: entry.summary.includeRefs,
    fingerprint: includeRefsFingerprint(entry.summary.includeRefs),
  };
}

function diskSummaryCacheEntry(
  entry: IncludeSummaryCacheEntry,
  settings: AspSettings,
): DiskSummaryCacheEntry {
  return {
    source: entry.source,
    settingsKey: includeSummarySettingsKey(settings),
    summary: entry.summary,
    publicSignature: entry.publicSignature,
  };
}

function diskIncludeRefsCacheEntry(
  entry: IncludeRefsCacheEntry,
  settings: AspSettings,
): DiskIncludeRefsCacheEntry {
  return {
    source: entry.source,
    settingsKey: includeRefsSettingsKey(settings),
    includeRefs: entry.includeRefs,
    fingerprint: entry.fingerprint,
  };
}

function diskVbSymbolIndexCacheEntry(
  entry: GraphFileIndex,
  settings: AspSettings,
): DiskVbSymbolIndexCacheEntry {
  return {
    source: entry.source,
    settingsKey: graphFileIndexSettingsKey(settings),
    index: entry.vbSymbolIndex,
    fingerprint: entry.fingerprint,
  };
}

function createIncludeRefsCacheEntry(
  fileName: string,
  text: string,
  key: string,
  source: DiskAnalysisSourceMetadata,
): IncludeRefsCacheEntry {
  const includeRefs = extractAspIncludeRefs(text);
  return {
    key,
    fileName,
    uri: pathToFileUri(fileName),
    source,
    includeRefs,
    fingerprint: includeRefsFingerprint(includeRefs),
  };
}

async function createIncludeDocumentCacheEntryAsync(
  fileName: string,
  text: string,
  settings: AspSettings,
  key: string,
  source: DiskAnalysisSourceMetadata,
): Promise<IncludeDocumentCacheEntry> {
  const parsed = await parseAspDocumentAsync(pathToFileUri(fileName), text, settings);
  await hydrateVbscriptCst(parsed, settings);
  const summary = await summarizeAspFileAnalysisAsync(parsed, vbProjectContextSettings(settings));
  const publicSignature = filePublicSignature(summary);
  return {
    key,
    fileName,
    uri: parsed.uri,
    source,
    parsed,
    summary,
    publicFingerprint: publicSignature.fingerprint,
    publicSignature,
  };
}

function includeRefsFingerprint(includeRefs: AspInclude[]): string {
  return textFingerprint(
    JSON.stringify(includeRefs.map((include) => ({ mode: include.mode, path: include.path }))),
  );
}

function sameDiskAnalysisSource(
  left: DiskAnalysisSourceMetadata,
  right: DiskAnalysisSourceMetadata,
): boolean {
  return (
    left.fileName === right.fileName && left.mtimeMs === right.mtimeMs && left.size === right.size
  );
}

function rememberIncludePublicSummary(
  entry: IncludeSummaryCacheEntry,
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
  resetIncludeDependencies(cached.source.uri);
  for (const include of cached.parsed.includes) {
    if (cancellation.isCancellationRequested()) {
      return [];
    }
    const resolved = await resolveIncludePathDetailsAsync(
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

async function includeRenameWorkspaceEditAsync(
  files: Array<{ oldUri: string; newUri: string }>,
): Promise<WorkspaceEdit | null> {
  await ensureWorkspaceIndexAsync(globalSettings);
  const changes: NonNullable<WorkspaceEdit["changes"]> = {};
  const seenEdits = new Set<string>();
  const renamePairs = files.map((file) => ({
    oldFile: normalizeFileName(uriToFileName(file.oldUri)),
    newFile: normalizeFileName(uriToFileName(file.newUri)),
  }));
  const indexedCandidates = await Promise.all(
    [...workspaceIndex.values()].map((entry) =>
      cachedFromIndexedAsync(entry, cachedSettings(entry.uri)),
    ),
  );
  const openedCandidates = await Promise.all(
    documents.all().map((document) => ensureFreshCachedDocumentAsync(document)),
  );
  const candidates = [...openedCandidates, ...indexedCandidates];
  for (const cached of candidates) {
    const settings = cachedSettings(cached.source.uri);
    for (const include of cached.parsed.includes) {
      const resolved = normalizeFileName(
        await resolveIncludePathAsync(cached.source.uri, include.path, include.mode, settings),
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
  const limits = vbProjectContextLimits(settings);
  const visited = new Set<string>();
  const stack: string[] = [];
  const stackIndexes = new Map<string, number>();
  const ownerUri = pathToFileUri(owner);
  let totalTextLength = 0;
  let truncatedReason: string | undefined;
  let missingSummary = false;
  const noteTruncated = (reason: string): void => {
    truncatedReason ??= reason;
  };
  const search = async (fileName: string, depth: number): Promise<string[] | undefined> => {
    if (depth > 20 || cancellation.isCancellationRequested()) {
      if (depth > 20) {
        noteTruncated("depth>20");
      }
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
    if (visited.size >= limits.maxDocuments) {
      noteTruncated(`documents>${limits.maxDocuments}`);
      return undefined;
    }
    const size = await fileSizeAsync(normalized);
    if (size !== undefined && totalTextLength + size > limits.maxTextLength) {
      noteTruncated(`text>${limits.maxTextLength}`);
      return undefined;
    }
    totalTextLength += size ?? 0;
    visited.add(normalized);
    stackIndexes.set(normalized, stack.length);
    stack.push(normalized);
    const entry = await includeDocumentLoader
      .readIncludeRefsAsync(normalized, settings, { allowRead: true })
      .catch(() => undefined);
    await yieldToEventLoop();
    if (!entry || cancellation.isCancellationRequested()) {
      missingSummary = true;
      scheduleIncludeSummaryRefresh(
        pathToFileUri(owner),
        normalized,
        settings,
        "includeCycle.missingSummary",
      );
      stack.pop();
      stackIndexes.delete(normalized);
      return undefined;
    }
    for (const include of entry.includeRefs) {
      const next = await resolveIncludePathAsync(
        pathToFileUri(normalized),
        include.path,
        include.mode,
        settings,
      );
      recordIncludeDependency(ownerUri, pathToFileUri(next));
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
  if (!cancellation.isCancellationRequested() && !missingSummary) {
    includeCycleCache.set(cacheKey, cycle ?? null);
    if (truncatedReason) {
      logDebugSummary(
        settings,
        `[asp-lsp] includeCycle.truncated: owner=${pathToFileUri(owner)}, start=${pathToFileUri(start)}, files=${visited.size}, text=${totalTextLength}, reason=${truncatedReason}`,
      );
    }
  }
  return cycle;
}

function includeCycleCacheKey(owner: string, start: string, settings: AspSettings): string {
  return JSON.stringify({
    owner: normalizeFileName(owner),
    start: normalizeFileName(start),
    limits: vbProjectContextLimits(settings),
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

async function selectionRangeAtAsync(
  cached: CachedDocument,
  position: Position,
): Promise<SelectionRange> {
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
    const context = cssContext(cached);
    const virtual = context?.virtual;
    const virtualPosition = virtual?.sourceMap.toVirtualPosition(position);
    if (context && virtual && virtualPosition) {
      return remapSelectionRange(
        virtual,
        cssService.getSelectionRanges(context.document, [virtualPosition], context.stylesheet)[0],
      );
    }
  }
  if (region?.language === "javascript") {
    const range = await jsSelectionRangeAsync(cached, position);
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
  return selectionRangeWithContainedParents({
    range: { start, end },
    parent: range.parent ? remapSelectionRange(virtual, range.parent) : undefined,
  });
}

async function jsSelectionRangeAsync(
  cached: CachedDocument,
  position: Position,
): Promise<SelectionRange | undefined> {
  const context = await jsContextAtAsync(cached, position);
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
  return selectionRangeWithContainedParents({
    range: {
      start: start ?? { line: 0, character: 0 },
      end: end ?? start ?? { line: 0, character: 0 },
    },
    parent: range.parent ? remapTsSelectionRange(virtual, range.parent) : undefined,
  });
}

function selectionRangeWithContainedParents(range: SelectionRange): SelectionRange {
  if (!range.parent) {
    return range;
  }
  return {
    range: range.range,
    parent: expandSelectionRangeToContain(range.parent, range.range),
  };
}

function expandSelectionRangeToContain(range: SelectionRange, childRange: Range): SelectionRange {
  const containedRange = rangeContainsRange(range.range, childRange)
    ? range.range
    : rangeContaining(range.range, childRange);
  return {
    range: containedRange,
    parent: range.parent ? expandSelectionRangeToContain(range.parent, containedRange) : undefined,
  };
}

function cssDocumentColors(cached: CachedDocument): ColorInformation[] {
  const context = cssContext(cached);
  if (!context) {
    return [];
  }
  const { document, stylesheet, virtual } = context;
  return cssService
    .findDocumentColors(document, stylesheet)
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
  const context = cssContext(cached);
  if (!context) {
    return [];
  }
  const { document, stylesheet, virtual } = context;
  const start = virtual.sourceMap.toVirtualPosition(range.start);
  const end = virtual.sourceMap.toVirtualPosition(range.end);
  if (!start || !end) {
    return [];
  }
  return cssService
    .getColorPresentations(document, stylesheet, color, { start, end })
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
  sourceDocument: TextDocument,
  virtual: VirtualDocument,
  diagnostic: TsDiagnosticLike,
  override: { severity?: DiagnosticSeverity; source?: string } = {},
): Diagnostic | undefined {
  if (diagnostic.start === undefined || diagnostic.length === undefined) {
    return undefined;
  }
  const start = diagnostic.start;
  const end = diagnostic.start + diagnostic.length;
  const range = sourceRangeFromVirtualOffsets(sourceDocument, virtual, start, end);
  if (!range) {
    return undefined;
  }
  return {
    severity:
      override.severity ??
      (diagnostic.category === ts.DiagnosticCategory.Error
        ? DiagnosticSeverity.Error
        : DiagnosticSeverity.Warning),
    range,
    message: ts.flattenDiagnosticMessageText(diagnostic.messageText, "\n"),
    code: diagnostic.code,
    source: override.source ?? "asp-lsp-typescript",
    tags: diagnostic.reportsUnnecessary === true ? [DiagnosticTag.Unnecessary] : undefined,
  };
}

function sourceRangeFromVirtualOffsets(
  sourceDocument: TextDocument,
  virtual: VirtualDocument,
  start: number,
  end: number,
): Range | undefined {
  const lastOffset = Math.max(start, end - 1);
  const segment = sourceMapSegmentAtVirtualOffset(virtual, start);
  if (!segment || lastOffset >= segment.virtualEnd) {
    return undefined;
  }
  const sourceStart = segment.sourceStart + (start - segment.virtualStart);
  const sourceEnd = segment.sourceStart + (end - segment.virtualStart);
  return {
    start: sourceDocument.positionAt(sourceStart),
    end: sourceDocument.positionAt(sourceEnd),
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

function tsDiagnosticKey(diagnostic: TsDiagnosticLike): string {
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

async function getIndexedCachedAsync(
  uri: string,
  settings: AspSettings,
): Promise<CachedDocument | undefined> {
  await ensureWorkspaceIndexAsync(settings);
  const entry = workspaceIndex.get(normalizeFileName(uriToFileName(uri)));
  return entry ? cachedFromIndexedAsync(entry, cachedSettings(entry.uri)) : undefined;
}

async function cachedFromIndexedAsync(
  entry: WorkspaceIndexedDocument,
  settings: AspSettings,
): Promise<CachedDocument> {
  const parsed = await parseAspDocumentAsync(
    entry.uri,
    await readTextFileAsync(entry.fileName, settings.legacyEncoding),
    settings,
  );
  return createCachedDocument(
    TextDocument.create(entry.uri, "classic-asp", 0, parsed.text),
    parsed,
    settings,
    [],
    cstHasVbscript(parsed.cst) ? "full" : "skeleton",
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
  const cancellation: AnalysisCancellation = {
    isCancellationRequested: () => token?.isCancellationRequested === true,
  };
  const cached = await cachedFromIndexedAsync(entry, settings);
  if (cancellation.isCancellationRequested()) {
    return [];
  }
  const settingsKey = await diskAnalysisSettingsKey(settings, cached.parsed, cancellation);
  if (cancellation.isCancellationRequested()) {
    return [];
  }
  const lookup = {
    source: sourceMetadata,
    settingsKey,
  };
  const cachedAnalysis = await diskAnalysisCache.readAnalysis(lookup);
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
  await diskAnalysisCache.write({
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

async function diskAnalysisSettingsKey(
  settings: AspSettings,
  parsed: AspParsedDocument,
  cancellation: AnalysisCancellation,
): Promise<string> {
  return JSON.stringify({
    parse: parseSettingsIdentity(settings),
    diagnostics: diagnosticsIdentity(settings),
    include: includeResolutionIdentity(settings),
    includeDependencies: await diskAnalysisIncludeDependencyKey(parsed, settings, cancellation),
    js: jsProjectSettingsIdentity(settings),
    workspace: workspaceIndexSettingsIdentity(settings),
  });
}

async function diskAnalysisIncludeDependencyKey(
  root: AspParsedDocument,
  settings: AspSettings,
  cancellation: AnalysisCancellation,
): Promise<string> {
  const dependencies: unknown[] = [];
  const visited = new Set<string>();
  const visitRefs = async (
    ownerUri: string,
    includeRefs: FileAnalysisSummary["includeRefs"],
    depth: number,
  ): Promise<void> => {
    if (depth > 20 || cancellation.isCancellationRequested()) {
      return;
    }
    for (const include of includeRefs) {
      if (cancellation.isCancellationRequested()) {
        return;
      }
      const resolved = await resolveIncludePathDetailsAsync(
        ownerUri,
        include.path,
        include.mode,
        settings,
      );
      const normalizedFileName = normalizeFileName(resolved.fileName);
      const stat = await fs.promises.stat(normalizedFileName).catch(() => undefined);
      const exists = stat?.isFile() === true;
      dependencies.push({
        owner: normalizeFileName(uriToFileName(ownerUri)),
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
      const entry = await includeDocumentLoader
        .readIncludeRefsAsync(normalizedFileName, settings, { allowRead: true })
        .catch(() => undefined);
      if (entry) {
        await visitRefs(entry.uri, entry.includeRefs, depth + 1);
      } else {
        scheduleIncludeSummaryRefresh(
          root.uri,
          normalizedFileName,
          settings,
          "diskKey.missingSummary",
        );
      }
      await yieldToEventLoop();
    }
  };
  await visitRefs(root.uri, root.includes, 0);
  return textFingerprint(JSON.stringify(dependencies));
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
    scannedFiles = await indexWorkspaceRootAsync(root, {
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

function invalidateWorkspaceIndex(reason = "workspaceIndex.invalidate"): void {
  workspaceGeneration += 1;
  workspaceIndexDirty = true;
  workspaceIndexTruncated = false;
  workspaceIndex.clear();
  workspaceVbReferenceIndex = undefined;
  logInvalidation("workspaceIndex", reason, workspaceGeneration);
}

async function clearDiskAnalysisCacheByCommand(): Promise<void> {
  await diskAnalysisCache.clear();
  logDebugSummary(globalSettings, "[asp-lsp] diskCache.clear");
}

async function clearProcessCachesByCommand(reason: string): Promise<void> {
  const openedUris = openDocumentUris();
  if (projectUpdateTimer) {
    clearTimeout(projectUpdateTimer);
    projectUpdateTimer = undefined;
  }
  if (openFileProjectMaintenanceTimer) {
    clearTimeout(openFileProjectMaintenanceTimer);
    openFileProjectMaintenanceTimer = undefined;
  }
  pendingProjectUpdateReason = undefined;
  pendingOpenFileMaintenanceReason = undefined;
  for (const uri of openedUris) {
    cancelScheduledDiagnostics(uri);
    clearSemanticTokensForUri(uri);
  }
  cache.clear();
  inFlightDocumentRefreshes.clear();
  pendingIncludeSummaryRefreshes.clear();
  vbProjectContextCache.clear();
  completionSessionCache.clear(reason);
  clearWorkspaceIndexProcessCaches(reason);
  clearIncludeCaches();
  clearJsProjectCaches();
  await closeDiagnosticsWorkerPools(reason);
  requestVisualRefresh(reason);
  for (const document of documents.all()) {
    validate(document);
  }
  logDebugSummary(globalSettings, "[asp-lsp] processCache.clear");
}

function clearWorkspaceIndexProcessCaches(reason: string): void {
  workspaceIndexDirty = true;
  workspaceIndexTruncated = false;
  workspaceIndex.clear();
  workspaceVbReferenceIndex = undefined;
  logDebugSummary(globalSettings, `[asp-lsp] processCache.workspaceIndex.clear: ${reason}`);
}

async function closeDiagnosticsWorkerPools(reason: string): Promise<void> {
  const jsPool = jsDiagnosticsWorkerPool;
  const vbPool = vbDiagnosticsWorkerPool;
  jsDiagnosticsWorkerPool = undefined;
  vbDiagnosticsWorkerPool = undefined;
  if (jsPool) {
    try {
      await jsPool.close();
    } catch (error) {
      connection.console.warn(
        `[asp-lsp] jsWorkerPool.close.failed: reason=${reason}, error=${errorMessage(error)}`,
      );
    }
  }
  if (vbPool) {
    try {
      await vbPool.close();
    } catch (error) {
      connection.console.warn(
        `[asp-lsp] vbWorkerPool.close.failed: reason=${reason}, error=${errorMessage(error)}`,
      );
    }
  }
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
      ifSyntaxDiagnostics: settings.vbscript?.ifSyntaxDiagnostics ?? "basic",
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
    await configureDiskAnalysisCacheAsync();
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
    await configureDiskAnalysisCacheAsync();
  }
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
    graph: normalizeGraphSettings(settings),
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
    busyAnalysisConcurrency: clampAnalysisConcurrency(
      typeof record.busyAnalysisConcurrency === "number"
        ? record.busyAnalysisConcurrency
        : undefined,
      defaultBusyAnalysisConcurrency(),
    ),
    vbProjectMaxDocuments: positiveIntegerSetting(
      record.vbProjectMaxDocuments,
      positiveIntegerFromEnv("ASP_LSP_VB_PROJECT_MAX_DOCUMENTS", 32),
    ),
    vbProjectMaxTextLength: positiveIntegerSetting(
      record.vbProjectMaxTextLength,
      positiveIntegerFromEnv("ASP_LSP_VB_PROJECT_MAX_TEXT_LENGTH", 1024 * 1024),
    ),
  };
}

function positiveIntegerSetting(value: unknown, fallback: number): number {
  return typeof value === "number" && value > 0 ? Math.floor(value) : fallback;
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
    variableTypes: record.variableTypes === true,
    parameterNames: record.parameterNames !== false,
    functionReturnTypes: record.functionReturnTypes === true,
    implicitByRef: record.implicitByRef === true,
    globalVariableMarkers: normalizeInlayMarkerMode(record.globalVariableMarkers),
  };
}

function normalizeInlayMarkerMode(
  value: unknown,
): NonNullable<NonNullable<AspSettings["inlayHints"]>["globalVariableMarkers"]> {
  if (value === "all" || value === "local" || value === "global" || value === "off") {
    return value;
  }
  if (value === false) {
    return "off";
  }
  return "off";
}

function normalizeCodeLensSettings(
  settings: Record<string, unknown> | AspSettings,
): AspSettings["codeLens"] {
  const raw = settings.codeLens;
  const record = raw && typeof raw === "object" ? (raw as Record<string, unknown>) : {};
  return {
    references: record.references !== false,
    includes: record.includes === true,
    referenceScope: record.referenceScope === "workspace" ? "workspace" : "analyzed",
  };
}

function normalizeGraphSettings(
  settings: Record<string, unknown> | AspSettings,
): NonNullable<AspSettings["graph"]> {
  const raw = settings.graph;
  const record = raw && typeof raw === "object" ? (raw as Record<string, unknown>) : {};
  return {
    showBuiltinSymbols: record.showBuiltinSymbols === true,
    showConfiguredGlobals: record.showConfiguredGlobals === true,
    showConfiguredComTypes: record.showConfiguredComTypes === true,
    showObjectMembers: record.showObjectMembers === true,
    showFunctionParameters: record.showFunctionParameters === true,
    showLocalVariables: record.showLocalVariables === true,
    showLocalConstants: record.showLocalConstants === true,
    showClassFields: record.showClassFields === true,
    showClassMethods: record.showClassMethods === true,
    showClassProperties: record.showClassProperties === true,
    showClassConstants: record.showClassConstants === true,
    showClasses: record.showClasses !== false,
    showFunctions: record.showFunctions !== false,
    showSubs: record.showSubs !== false,
    showGlobalVariables: record.showGlobalVariables !== false,
    showGlobalConstants: record.showGlobalConstants !== false,
    showFiles: record.showFiles !== false,
    showMissingFiles: record.showMissingFiles !== false,
    showIncludeLinks: record.showIncludeLinks !== false,
    showDeclarationLinks: record.showDeclarationLinks !== false,
    showReferenceLinks: record.showReferenceLinks !== false,
    showCallLinks: record.showCallLinks !== false,
    showUnresolvedReferences: record.showUnresolvedReferences !== false,
  };
}

function normalizeVbscriptSettings(
  settings: Record<string, unknown> | AspSettings,
): AspSettings["vbscript"] {
  const raw = settings.vbscript;
  const record = raw && typeof raw === "object" ? (raw as Record<string, unknown>) : {};
  return {
    typeChecking: record.typeChecking === "strict" ? "strict" : "basic",
    ifSyntaxDiagnostics: normalizeIfSyntaxDiagnostics(record.ifSyntaxDiagnostics),
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
    syntaxSnippets: record.syntaxSnippets !== false,
    syntaxKeywords: record.syntaxKeywords !== false,
    initializedDimQuickFixStyle: normalizeInitializedDimQuickFixStyle(
      record.initializedDimQuickFixStyle,
    ),
  };
}

function normalizeInitializedDimQuickFixStyle(
  value: unknown,
): NonNullable<NonNullable<AspSettings["vbscript"]>["initializedDimQuickFixStyle"]> {
  return value === "sameLineColon" ? "sameLineColon" : "newline";
}

function normalizeIfSyntaxDiagnostics(
  value: unknown,
): NonNullable<NonNullable<AspSettings["vbscript"]>["ifSyntaxDiagnostics"]> {
  return value === "off" || value === "strict" ? value : "basic";
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

async function formatAspDocumentWithDelegatesAsync(
  cached: CachedDocument,
  options: { tabSize: number; insertSpaces: boolean },
): Promise<TextEdit[]> {
  const settings = cachedSettings(cached.source.uri);
  await hydrateCachedVbscriptCstAsync(cached, settings, "format");
  const startedAt = startFormattingLog(cached, settings, "document");
  const formattingOptions = measureDebugStep(settings, cached.source.uri, "format.options", () =>
    formatOptions(options, settings),
  );
  const original = cached.source.getText();
  let formatted = measureDebugStep(settings, cached.source.uri, "format.core", () =>
    applyTextEdits(original, formatAspDocument(cached.parsed, formattingOptions)),
  );
  const parsed = await measureDebugStepAsync(settings, cached.source.uri, "format.reparse", () =>
    parseAspDocumentAsync(cached.source.uri, formatted, settings),
  );
  await hydrateVbscriptCst(parsed, settings);
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

async function formatAspRangeWithDelegatesAsync(
  cached: CachedDocument,
  range: Range,
  options: { tabSize: number; insertSpaces: boolean },
): Promise<TextEdit[]> {
  const settings = cachedSettings(cached.source.uri);
  await hydrateCachedVbscriptCstAsync(cached, settings, "format");
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
  const parsed = await measureDebugStepAsync(settings, cached.source.uri, "format.reparse", () =>
    parseAspDocumentAsync(cached.source.uri, formatted, settings),
  );
  await hydrateVbscriptCst(parsed, settings);
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
    insertSpaceBeforeAndAfterBinaryOperators: true,
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

async function createJsLanguageServiceAsync(
  virtual: VirtualDocument,
  settings: AspSettings,
  optionOverrides: Partial<ts.CompilerOptions> = {},
): Promise<JsLanguageServiceProject> {
  const cacheKey = jsLanguageServiceCacheKey(virtual, settings, optionOverrides);
  const cached = jsLanguageServiceCache.get(cacheKey);
  if (cached) {
    updateJsLanguageServiceOpenFiles(
      cached.project,
      await collectOpenJsProjectFilesAsync(virtual, settings),
    );
    cached.lastUsed = ++jsLanguageServiceCacheTick;
    logDebugSummary(
      settings,
      `[asp-lsp] javascript.languageService.reuse: ${virtualSourceUri(virtual)}, files=${cached.project.files.size}`,
    );
    return cached.project;
  }
  const collected = await collectJsProjectFilesAsync(virtual, settings, optionOverrides);
  return createJsLanguageServiceFromCollected(virtual, settings, cacheKey, collected);
}

function createJsLanguageServiceFromCollected(
  virtual: VirtualDocument,
  settings: AspSettings,
  cacheKey: string,
  collected: JsProjectConfig & { files: Map<string, JsProjectFile> },
): JsLanguageServiceProject {
  const files = new Map<string, JsProjectFile>();
  const moduleResolutionHost: ts.ModuleResolutionHost = {
    fileExists: (requested) =>
      files.has(normalizeFileName(requested)) || cachedTsFileExists(requested),
    readFile: (requested) =>
      files.get(normalizeFileName(requested))?.text ?? cachedTsReadFile(requested),
    directoryExists: cachedTsDirectoryExists,
    getDirectories: cachedTsGetDirectories,
    realpath: cachedTsRealpath,
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
      const text = cachedTsReadFile(requested);
      return text === undefined ? undefined : ts.ScriptSnapshot.fromString(text);
    },
    getScriptKind: (requested) => scriptKindForFileName(requested),
    getCurrentDirectory: () => project.currentDirectory,
    getCompilationSettings: () => project.options,
    getDefaultLibFileName: (options) => ts.getDefaultLibFilePath(options),
    fileExists: (requested) =>
      files.has(normalizeFileName(requested)) || cachedTsFileExists(requested),
    readFile: (requested) =>
      files.get(normalizeFileName(requested))?.text ?? cachedTsReadFile(requested),
    readDirectory: cachedReadTypeScriptDirectory,
    directoryExists: cachedTsDirectoryExists,
    getDirectories: cachedTsGetDirectories,
    realpath: cachedTsRealpath,
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
    projectOwner: jsProjectOwnerIdentity(virtualSourceUri(virtual)),
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

function jsProjectOwnerIdentity(ownerUri: string): string {
  const ownerFile = uriToFileName(ownerUri);
  return JSON.stringify({
    ownerDirectory: normalizeFileName(path.dirname(ownerFile)),
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

function updateJsLanguageServiceOpenFiles(
  project: Pick<JsLanguageServiceProject, "files" | "projectVersion">,
  openFiles: Map<string, JsProjectFile>,
): void {
  const previousFiles = jsProjectFilesFingerprint(project.files);
  const openFileNames = new Set(openFiles.keys());
  for (const [fileName, file] of project.files) {
    if (file.virtual && !openFileNames.has(fileName)) {
      project.files.delete(fileName);
    }
  }
  for (const [fileName, file] of openFiles) {
    project.files.set(fileName, file);
  }
  if (previousFiles !== jsProjectFilesFingerprint(project.files)) {
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
  lightweightJsUnusedDiagnosticsCache.clear();
  clearJsFileSystemCache();
  completionSessionCache.clear("jsProject");
}

function clearJsFileSystemCache(): void {
  jsFileExistsCache.clear();
  jsReadFileCache.clear();
  jsDirectoryExistsCache.clear();
  jsDirectoriesCache.clear();
  jsReadDirectoryCache.clear();
  jsRealpathCache.clear();
  jsFileStatCache.clear();
}

function clearIncludeCaches(): void {
  includePathResolutionCache.clear();
  pathResolutionCache.clear();
  includeCycleCache.clear();
  includeDocumentLoader.clear();
  clearGraphFileIndexCache();
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

async function collectJsProjectFilesAsync(
  activeVirtual: VirtualDocument,
  settings: AspSettings,
  optionOverrides: Partial<ts.CompilerOptions> = {},
): Promise<JsProjectConfig & { files: Map<string, JsProjectFile> }> {
  const files = await collectOpenJsProjectFilesAsync(activeVirtual, settings);
  const ownerFile = uriToFileName(virtualSourceUri(activeVirtual));
  const config = readJsProjectConfig(ownerFile, settings, optionOverrides);
  await mapWithConcurrency(
    config.fileNames.map(normalizeFileName),
    analysisConcurrency(settings),
    async (fileName) => {
      if (files.has(fileName)) {
        return;
      }
      const exists = await cachedTsFileExistsAsync(fileName);
      if (!exists) {
        return;
      }
      const [text, stat] = await Promise.all([
        cachedTsReadFileAsync(fileName),
        cachedJsFileStatAsync(fileName),
      ]);
      if (text === undefined) {
        return;
      }
      files.set(fileName, {
        fileName,
        text,
        version: stat ? `${stat.mtimeMs}:${stat.size}` : "0",
        uri: pathToFileUri(fileName),
      });
    },
  );
  return {
    files,
    fileNames: config.fileNames,
    options: config.options,
    currentDirectory: config.currentDirectory,
  };
}

async function prefetchJsProjectFilesAsync(
  activeVirtual: VirtualDocument,
  settings: AspSettings,
  optionOverrides: Partial<ts.CompilerOptions> = {},
): Promise<void> {
  const ownerFile = uriToFileName(virtualSourceUri(activeVirtual));
  await prefetchJsProjectEnvironmentAsync(ownerFile, settings);
  const config = readJsProjectConfig(ownerFile, settings, optionOverrides);
  const openFileNames = new Set(
    (await collectOpenJsProjectFilesAsync(activeVirtual, settings)).keys(),
  );
  await mapWithConcurrency(
    config.fileNames.map(normalizeFileName),
    analysisConcurrency(settings),
    async (fileName) => {
      if (openFileNames.has(fileName)) {
        return;
      }
      const exists = await cachedTsFileExistsAsync(fileName);
      if (!exists) {
        return;
      }
      await Promise.all([cachedTsReadFileAsync(fileName), cachedJsFileStatAsync(fileName)]);
    },
  );
}

async function prefetchJsProjectEnvironmentAsync(
  ownerFile: string,
  settings: AspSettings,
): Promise<void> {
  const ownerDirectory = path.dirname(ownerFile);
  if (settings.javascript?.ignoreProjectConfig !== true) {
    await prefetchNearestJsProjectConfigAsync(ownerDirectory);
  }
  const configPath =
    settings.javascript?.ignoreProjectConfig === true
      ? undefined
      : (ts.findConfigFile(ownerDirectory, cachedTsFileExists, "tsconfig.json") ??
        ts.findConfigFile(ownerDirectory, cachedTsFileExists, "jsconfig.json"));
  if (configPath) {
    await Promise.all([cachedTsReadFileAsync(configPath), cachedJsFileStatAsync(configPath)]);
  }
  const packageJson = nearestPackageJson(ownerDirectory);
  if (packageJson) {
    await cachedJsFileStatAsync(packageJson);
  }
}

async function prefetchNearestJsProjectConfigAsync(directory: string): Promise<void> {
  let current = normalizeFileName(directory);
  while (true) {
    const tsconfig = path.join(current, "tsconfig.json");
    const jsconfig = path.join(current, "jsconfig.json");
    const [hasTsConfig, hasJsConfig] = await Promise.all([
      cachedTsFileExistsAsync(tsconfig),
      cachedTsFileExistsAsync(jsconfig),
    ]);
    if (hasTsConfig || hasJsConfig) {
      return;
    }
    const parent = path.dirname(current);
    if (parent === current) {
      return;
    }
    current = parent;
  }
}

async function collectOpenJsProjectFilesAsync(
  activeVirtual: VirtualDocument,
  settings: AspSettings,
): Promise<Map<string, JsProjectFile>> {
  const files = new Map<string, JsProjectFile>();
  addJsProjectVirtualFile(files, activeVirtual, settings);
  const cachedDocuments = await Promise.all(
    documents.all().map((document) => ensureFreshCachedDocumentAsync(document)),
  );
  for (const cached of cachedDocuments) {
    for (const virtual of jsVirtualDocuments(cached)) {
      addJsProjectVirtualFile(files, virtual, settings);
    }
  }
  return files;
}

function addJsProjectVirtualFile(
  files: Map<string, JsProjectFile>,
  virtual: VirtualDocument,
  settings: AspSettings,
): void {
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
      : (ts.findConfigFile(ownerDirectory, cachedTsFileExists, "tsconfig.json") ??
        ts.findConfigFile(ownerDirectory, cachedTsFileExists, "jsconfig.json"));
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
    const config = ts.readConfigFile(configPath, cachedTsReadFile);
    const parseHost: ts.ParseConfigHost = {
      useCaseSensitiveFileNames: ts.sys.useCaseSensitiveFileNames,
      fileExists: cachedTsFileExists,
      readFile: cachedTsReadFile,
      readDirectory: cachedReadTypeScriptDirectory,
    };
    const parsed = ts.parseJsonConfigFileContent(
      config.config ?? {},
      parseHost,
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
      const stat = cachedJsFileStat(fileName);
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
    fileExists: cachedTsFileExists,
    readFile: cachedTsReadFile,
    directoryExists: cachedTsDirectoryExists,
    getDirectories: cachedTsGetDirectories,
    realpath: cachedTsRealpath,
    getCurrentDirectory: () => currentDirectory,
  };
  const types = ts
    .getAutomaticTypeDirectiveNames(options, host)
    .filter((type) => type.toLowerCase() !== "node");
  return types;
}

function cachedTsFileExists(fileName: string): boolean {
  const key = safeNormalizeFileName(fileName);
  const cached = jsFileExistsCache.get(key);
  if (cached !== undefined) {
    return cached;
  }
  const exists = ts.sys.fileExists(fileName);
  jsFileExistsCache.set(key, exists);
  return exists;
}

async function cachedTsFileExistsAsync(fileName: string): Promise<boolean> {
  const key = safeNormalizeFileName(fileName);
  const cached = jsFileExistsCache.get(key);
  if (cached !== undefined) {
    return cached;
  }
  const exists = await fileExistsAsync(fileName);
  jsFileExistsCache.set(key, exists);
  return exists;
}

function cachedTsReadFile(fileName: string): string | undefined {
  const key = safeNormalizeFileName(fileName);
  if (jsReadFileCache.has(key)) {
    return jsReadFileCache.get(key);
  }
  const text = ts.sys.readFile(fileName);
  jsReadFileCache.set(key, text);
  return text;
}

async function cachedTsReadFileAsync(fileName: string): Promise<string | undefined> {
  const key = safeNormalizeFileName(fileName);
  if (jsReadFileCache.has(key)) {
    return jsReadFileCache.get(key);
  }
  const text = await fs.promises.readFile(fileName, "utf8").catch(() => undefined);
  jsReadFileCache.set(key, text);
  return text;
}

function cachedJsFileStat(fileName: string): JsFileStat | undefined {
  const key = safeNormalizeFileName(fileName);
  if (jsFileStatCache.has(key)) {
    return jsFileStatCache.get(key);
  }
  const stat = fs.statSync(fileName, { throwIfNoEntry: false });
  const metadata = stat
    ? {
        mtimeMs: stat.mtimeMs,
        size: stat.size,
        isFile: stat.isFile(),
      }
    : undefined;
  jsFileStatCache.set(key, metadata);
  return metadata;
}

async function cachedJsFileStatAsync(fileName: string): Promise<JsFileStat | undefined> {
  const key = safeNormalizeFileName(fileName);
  if (jsFileStatCache.has(key)) {
    return jsFileStatCache.get(key);
  }
  const stat = await fs.promises.stat(fileName).catch(() => undefined);
  const metadata = stat
    ? {
        mtimeMs: stat.mtimeMs,
        size: stat.size,
        isFile: stat.isFile(),
      }
    : undefined;
  jsFileStatCache.set(key, metadata);
  return metadata;
}

function cachedTsDirectoryExists(directory: string): boolean {
  const key = safeNormalizeFileName(directory);
  const cached = jsDirectoryExistsCache.get(key);
  if (cached !== undefined) {
    return cached;
  }
  const exists = ts.sys.directoryExists(directory);
  jsDirectoryExistsCache.set(key, exists);
  return exists;
}

function cachedTsGetDirectories(directory: string): string[] {
  const key = safeNormalizeFileName(directory);
  const cached = jsDirectoriesCache.get(key);
  if (cached) {
    return cached;
  }
  const directories = getTypeScriptDirectories(directory);
  jsDirectoriesCache.set(key, directories);
  return directories;
}

function cachedReadTypeScriptDirectory(
  rootDir: string,
  extensions?: readonly string[],
  excludes?: readonly string[],
  includes?: readonly string[],
  depth?: number,
): string[] {
  const key = JSON.stringify({
    rootDir: safeNormalizeFileName(rootDir),
    extensions,
    excludes,
    includes,
    depth,
  });
  const cached = jsReadDirectoryCache.get(key);
  if (cached) {
    return cached;
  }
  const entries = readTypeScriptDirectory(rootDir, extensions, excludes, includes, depth);
  jsReadDirectoryCache.set(key, entries);
  return entries;
}

function cachedTsRealpath(fileName: string): string {
  const key = safeNormalizeFileName(fileName);
  const cached = jsRealpathCache.get(key);
  if (cached) {
    return cached;
  }
  const realpath = ts.sys.realpath?.(fileName) ?? fileName;
  jsRealpathCache.set(key, realpath);
  return realpath;
}

function safeNormalizeFileName(fileName: string): string {
  try {
    return normalizeFileName(fileName);
  } catch {
    return fileName;
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

async function resolveIncludePathAsync(
  ownerUri: string,
  includePath: string,
  mode: "file" | "virtual",
  settings: AspSettings,
): Promise<string> {
  return (await resolveIncludePathDetailsAsync(ownerUri, includePath, mode, settings)).fileName;
}

async function resolveIncludePathDetailsAsync(
  ownerUri: string,
  includePath: string,
  mode: "file" | "virtual",
  settings: AspSettings,
): Promise<IncludePathResolution> {
  const cacheKey = includePathResolutionCacheKey(ownerUri, includePath, mode, settings);
  const cached = includePathResolutionCache.get(cacheKey);
  if (cached) {
    return cached;
  }
  const resolved = await resolveIncludePathDetailsUncachedAsync(
    ownerUri,
    includePath,
    mode,
    settings,
  );
  includePathResolutionCache.set(cacheKey, resolved);
  return resolved;
}

async function resolveIncludePathDetailsUncachedAsync(
  ownerUri: string,
  includePath: string,
  mode: "file" | "virtual",
  settings: AspSettings,
): Promise<IncludePathResolution> {
  if (mode === "virtual") {
    const normalizedInclude = includePath.replace(/^\/+/, "");
    const ownerRoot = await workspaceRootFromUriAsync(ownerUri);
    for (const root of [
      ...(settings.virtualRoots ?? []),
      settings.virtualRoot,
      ...workspaceRoots,
      ownerRoot,
    ]) {
      if (!root) {
        continue;
      }
      const candidate = await resolveIncludeCandidateAsync(
        root,
        normalizedInclude,
        settings,
        (actual) => `/${path.relative(root, actual).split(path.sep).join("/")}`,
      );
      if (candidate.exists) {
        return candidate;
      }
    }
    const root = settings.virtualRoot ?? ownerRoot;
    return resolveIncludeCandidateAsync(
      root,
      normalizedInclude,
      settings,
      (actual) => `/${path.relative(root, actual).split(path.sep).join("/")}`,
    );
  }
  const ownerDirectory = path.dirname(uriToFileName(ownerUri));
  const local = await resolveIncludeCandidateAsync(
    ownerDirectory,
    includePath,
    settings,
    (actual) => path.relative(ownerDirectory, actual).split(path.sep).join("/"),
  );
  if (local.exists) {
    return local;
  }
  for (const root of [...(settings.includePaths ?? []), ...(settings.virtualRoots ?? [])]) {
    const candidate = await resolveIncludeCandidateAsync(root, includePath, settings, (actual) =>
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

async function resolveIncludeCandidateAsync(
  baseDirectory: string,
  includePath: string,
  settings: AspSettings,
  actualIncludePath: (actualPath: string) => string,
): Promise<IncludePathResolution> {
  const resolved = await resolvePathFromBaseAsync(baseDirectory, includePath, settings);
  return {
    ...resolved,
    actualIncludePath: resolved.actualPath ? actualIncludePath(resolved.actualPath) : undefined,
  };
}

async function resolvePathFromBaseAsync(
  baseDirectory: string,
  requestedPath: string,
  settings: AspSettings,
): Promise<PathResolution> {
  const cacheKey = pathResolutionCacheKey(baseDirectory, requestedPath, settings);
  const cached = pathResolutionCache.get(cacheKey);
  if (cached) {
    return cached;
  }
  const resolved = await resolvePathFromBaseUncachedAsync(baseDirectory, requestedPath, settings);
  pathResolutionCache.set(cacheKey, resolved);
  return resolved;
}

async function resolvePathFromBaseUncachedAsync(
  baseDirectory: string,
  requestedPath: string,
  settings: AspSettings,
): Promise<PathResolution> {
  const fileName = path.resolve(baseDirectory, requestedPath);
  if (settings.windowsPathResolution === false) {
    const exists = await pathExistsAsync(fileName);
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
    const exists = await pathExistsAsync(fileName);
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
    const entries = await fs.promises
      .readdir(current, { withFileTypes: true })
      .catch(() => undefined);
    if (!entries) {
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
  const normalized = normalizeFileName(fileName);
  const root = workspaceRoots.find((candidate) => {
    const normalizedRoot = normalizeFileName(candidate);
    return normalized === normalizedRoot || normalized.startsWith(`${normalizedRoot}${path.sep}`);
  });
  return root ?? path.dirname(fileName);
}

function workspaceRootFromUriAsync(uri: string): Promise<string> {
  return Promise.resolve(workspaceRootFromUri(uri));
}

function sourceUriDocumentationLink(uri: string): string {
  const fragmentStart = uri.indexOf("#");
  const baseUri = fragmentStart === -1 ? uri : uri.slice(0, fragmentStart);
  const fragment = fragmentStart === -1 ? "" : uri.slice(fragmentStart);
  if (!baseUri.startsWith("file://")) {
    return escapeMarkdownLinkText(uri);
  }
  const fileName = uriToFileName(baseUri);
  const normalized = normalizeFileName(fileName);
  const root = workspaceRoots
    .map(normalizeFileName)
    .filter(
      (candidate) => normalized === candidate || normalized.startsWith(`${candidate}${path.sep}`),
    )
    .sort((left, right) => right.length - left.length)[0];
  const label = (root ? path.relative(root, fileName) || path.basename(fileName) : fileName)
    .split(path.sep)
    .join("/");
  return `[${escapeMarkdownLinkText(label + fragment)}](${pathToFileUri(fileName)}${fragment})`;
}

function escapeMarkdownLinkText(text: string): string {
  return text.replace(/[\\[\]]/g, "\\$&");
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

async function quickFixesForDiagnosticAsync(
  cached: CachedDocument,
  diagnostic: Diagnostic,
): Promise<CodeAction[]> {
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
    ];
  }
  if (diagnostic.source === "asp-lsp-vbscript-unused") {
    return removeUnusedVbscriptDeclarationActionsAsync(cached, diagnostic);
  }
  if (diagnostic.source === "asp-lsp-vbscript-type") {
    return vbscriptTypeDiagnosticActions(cached, diagnostic);
  }
  if (diagnostic.source === "asp-lsp-vbscript-naming") {
    return vbscriptNamingDiagnosticActionsAsync(cached, diagnostic);
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
      ? await resolveIncludePathAsync(
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

async function vbscriptNamingDiagnosticActionsAsync(
  cached: CachedDocument,
  diagnostic: Diagnostic,
): Promise<CodeAction[]> {
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
  const context = await buildFullVbProjectContextForWorkspaceOperationAsync(
    cached,
    cachedSettings(cached.source.uri),
  );
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

async function removeUnusedVbscriptDeclarationActionsAsync(
  cached: CachedDocument,
  diagnostic: Diagnostic,
): Promise<CodeAction[]> {
  const context = await buildVbProjectContextAsync(cached, cachedSettings(cached.source.uri));
  const symbol = context.symbols?.find(
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
  const endOffset = cached.source.offsetAt(symbol.range.end);
  const lineStart = cached.source.offsetAt({ line: symbol.range.start.line, character: 0 });
  const lineEnd = lineStart + line.length;
  let removeStart = parameterRemovalStartOffset(cached.source, symbol, lineStart);
  let removeEnd = endOffset;
  const after = cached.source.getText({
    start: cached.source.positionAt(removeEnd),
    end: cached.source.positionAt(lineEnd),
  });
  const before = cached.source.getText({
    start: cached.source.positionAt(lineStart),
    end: cached.source.positionAt(removeStart),
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

function parameterRemovalStartOffset(
  document: TextDocument,
  symbol: VbSymbol,
  lineStart: number,
): number {
  const startOffset = document.offsetAt(symbol.range.start);
  const before = document.getText({
    start: document.positionAt(lineStart),
    end: symbol.range.start,
  });
  const segmentStart = Math.max(before.lastIndexOf("("), before.lastIndexOf(",")) + 1;
  const segmentPrefix = before.slice(segmentStart);
  const keywordPrefix = /(?:^|\s)((?:(?:Optional|ByRef|ByVal)\s+)+)$/i.exec(segmentPrefix);
  if (!keywordPrefix?.[1]) {
    return startOffset;
  }
  return (
    lineStart +
    segmentStart +
    keywordPrefix.index +
    keywordPrefix[0].length -
    keywordPrefix[1].length
  );
}

const vbscriptExtractVariableKind = `${CodeActionKind.Refactor}.extract`;

async function vbscriptCodeActionsAsync(
  cached: CachedDocument,
  range: Range,
  context: CodeActionContext,
): Promise<CodeAction[]> {
  await hydrateCachedVbscriptCstAsync(cached, cachedSettings(cached.source.uri), "codeAction");
  const actions: CodeAction[] = [];
  if (codeActionAllows(context, CodeActionKind.QuickFix)) {
    if (context.diagnostics.length === 0) {
      const documentation = getVbscriptDocumentationQuickAction(
        cached.parsed,
        range.start,
        await buildVbProjectContextAsync(cached, cachedSettings(cached.source.uri)),
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
  const trimmedValue = value.trim();
  const newText =
    cachedSettings(cached.source.uri).vbscript?.initializedDimQuickFixStyle === "sameLineColon"
      ? `${indent}Dim ${name} : ${name} = ${trimmedValue}`
      : `${indent}Dim ${name}\n${indent}${name} = ${trimmedValue}`;
  return {
    title: localizerForUri(cached.source.uri).t("server.quickfix.splitInitializedDim"),
    kind: CodeActionKind.QuickFix,
    diagnostics: diagnostic ? [diagnostic] : undefined,
    edit: {
      changes: {
        [cached.source.uri]: [
          {
            range: lineRange(cached.source, line),
            newText,
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
  const css = cssContext(cached);
  const virtual = css?.virtual;
  const start = virtual?.sourceMap.toVirtualPosition(range.start);
  const end = virtual?.sourceMap.toVirtualPosition(range.end);
  if (!css || !virtual || !start || !end) {
    return [];
  }
  return cssService
    .doCodeActions2(css.document, { start, end }, context, css.stylesheet)
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

async function jsCodeActionsAsync(
  cached: CachedDocument,
  range: Range,
  context: CodeActionContext,
): Promise<CodeAction[]> {
  const actions: CodeAction[] = [];
  if (codeActionExplicitlyAllows(context, CodeActionKind.SourceOrganizeImports)) {
    const edit = await organizeJavaScriptImportsEditAsync(cached);
    if (edit || jsVirtualDocuments(cached).length > 0) {
      actions.push({
        title: localizerForUri(cached.source.uri).t("server.codeAction.organizeJavascriptImports"),
        kind: "source.organizeImports.aspLsp.javascript",
        edit: edit ?? { changes: {} },
      });
    }
  }
  const wantsQuickFix = codeActionAllows(context, CodeActionKind.QuickFix);
  const errorCodes = wantsQuickFix
    ? context.diagnostics
        .filter(
          (diagnostic) =>
            diagnostic.source === "asp-lsp-typescript" ||
            diagnostic.source === "asp-lsp-typescript-unused",
        )
        .map((diagnostic) => Number(diagnostic.code))
        .filter((code) => Number.isInteger(code))
    : [];
  const wantsRefactor = codeActionExplicitlyAllows(context, CodeActionKind.Refactor);
  if (errorCodes.length === 0 && !wantsRefactor) {
    return actions;
  }
  const sourceStart = cached.source.offsetAt(range.start);
  const sourceEnd = cached.source.offsetAt(range.end);
  for (const virtual of jsVirtualDocuments(cached)) {
    const virtualStart = virtual.sourceMap.toVirtualOffset(sourceStart);
    const virtualEnd = virtual.sourceMap.toVirtualOffset(sourceEnd);
    if (virtualStart === undefined || virtualEnd === undefined) {
      continue;
    }
    const project = await createJsLanguageServiceAsync(virtual, cachedSettings(cached.source.uri));
    const service = project.service;
    const fileName = jsProjectFileName(virtual, project);
    if (errorCodes.length > 0) {
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
    if (wantsRefactor) {
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

function codeActionExplicitlyAllows(context: CodeActionContext, kind: string): boolean {
  return Boolean(context.only?.length) && codeActionAllows(context, kind);
}

async function organizeJavaScriptImportsEditAsync(
  cached: CachedDocument,
): Promise<WorkspaceEdit | undefined> {
  const edits = (
    await Promise.all(
      jsVirtualDocuments(cached).map(async (virtual) => {
        const project = await createJsLanguageServiceAsync(
          virtual,
          cachedSettings(cached.source.uri),
        );
        return fileTextChangesToWorkspaceEdit(
          virtual,
          project.service.organizeImports(
            { type: "file", fileName: jsProjectFileName(virtual, project) },
            {},
            {},
          ),
        );
      }),
    )
  ).filter((edit): edit is WorkspaceEdit => Boolean(edit));
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

async function buildAspGraphForCommand(argument: unknown): Promise<AspGraphPayload> {
  const scope = graphCommandScope(argument);
  const uri = graphCommandUri(argument);
  return scope === "workspace"
    ? buildWorkspaceAspGraphAsync()
    : buildDocumentAspGraphAsync(uri ?? documents.all()[0]?.uri);
}

function graphCommandScope(argument: unknown): AspGraphScope {
  if (argument && typeof argument === "object" && "scope" in argument) {
    const scope = (argument as { scope?: unknown }).scope;
    return scope === "workspace" ? "workspace" : "document";
  }
  return "document";
}

function graphCommandUri(argument: unknown): string | undefined {
  if (!argument || typeof argument !== "object" || !("uri" in argument)) {
    return undefined;
  }
  const uri = (argument as { uri?: unknown }).uri;
  return typeof uri === "string" ? uri : undefined;
}

async function buildDocumentAspGraphAsync(uri: string | undefined): Promise<AspGraphPayload> {
  const cached = uri ? await cachedDocumentForGraphAsync(uri) : undefined;
  if (!cached) {
    return emptyAspGraphPayload("document", uri);
  }
  const settings = cachedSettings(cached.source.uri);
  const documentsForGraph = await collectDocumentGraphDocumentsAsync(cached, settings);
  const payload = await graphPayloadFromDocumentsAsync("document", documentsForGraph, settings, {
    rootUri: cached.source.uri,
  });
  return payload;
}

async function buildWorkspaceAspGraphAsync(): Promise<AspGraphPayload> {
  const settings = globalSettings;
  await ensureWorkspaceIndexAsync(settings);
  const opened = new Set<string>();
  const documentsForGraph: AspGraphDocument[] = [];
  for (const document of documents.all()) {
    if (!isClassicAspGraphUri(document.uri)) {
      continue;
    }
    const cached = await ensureFreshCachedDocumentAsync(document);
    opened.add(cached.source.uri);
    documentsForGraph.push(
      await graphDocumentFromCachedAsync(cached, cachedSettings(cached.source.uri)),
    );
  }
  for (const entry of workspaceIndex.values()) {
    if (opened.has(entry.uri)) {
      continue;
    }
    const cached = await cachedFromIndexedAsync(entry, cachedSettings(entry.uri));
    documentsForGraph.push(await graphDocumentFromCachedAsync(cached, cachedSettings(entry.uri)));
    await yieldToEventLoop();
  }
  const payload = await graphPayloadFromDocumentsAsync("workspace", documentsForGraph, settings, {
    truncated: workspaceIndexTruncated
      ? {
          reason: `workspaceIndex>${settings.workspace?.maxIndexFiles ?? defaultMaxIndexFiles}`,
        }
      : undefined,
  });
  return payload;
}

async function cachedDocumentForGraphAsync(uri: string): Promise<CachedDocument | undefined> {
  const document = documents.get(uri);
  if (document) {
    return ensureFreshCachedDocumentAsync(document);
  }
  if (!isClassicAspGraphUri(uri)) {
    return undefined;
  }
  const fileName = normalizeFileName(uriToFileName(uri));
  const stat = await fs.promises.stat(fileName).catch(() => undefined);
  if (!stat?.isFile()) {
    return undefined;
  }
  return cachedFromIndexedAsync(
    { uri, fileName, mtimeMs: stat.mtimeMs, size: stat.size },
    cachedSettings(uri),
  );
}

async function collectDocumentGraphDocumentsAsync(
  root: CachedDocument,
  settings: AspSettings,
): Promise<AspGraphDocument[]> {
  const limits = vbProjectContextLimits(settings);
  const documentsForGraph: AspGraphDocument[] = [];
  const visited = new Set<string>();
  let textLength = root.parsed.text.length;

  const visit = async (document: AspGraphDocument, depth: number): Promise<void> => {
    if (depth > 20 || visited.has(document.uri)) {
      return;
    }
    visited.add(document.uri);
    documentsForGraph.push(document);
    const includeRefs = (await graphFileIndexForDocumentAsync(document, settings)).includeRefs;
    for (const include of includeRefs) {
      const resolved = await resolveIncludePathDetailsAsync(
        document.uri,
        include.path,
        include.mode,
        settings,
      );
      const includeUri = pathToFileUri(resolved.fileName);
      if (!resolved.exists || visited.has(includeUri) || visited.size >= limits.maxDocuments) {
        continue;
      }
      const size = await fileSizeAsync(resolved.fileName);
      if (size !== undefined && textLength + size > limits.maxTextLength) {
        continue;
      }
      const entry = await includeDocumentLoader.readAsync(resolved.fileName, settings);
      if (!entry) {
        continue;
      }
      textLength += entry.source.size;
      await visit(graphDocumentFromIncludeEntry(entry), depth + 1);
    }
  };

  await visit(await graphDocumentFromCachedAsync(root, settings), 0);
  return documentsForGraph;
}

async function graphPayloadFromDocumentsAsync(
  scope: AspGraphScope,
  documentsForGraph: AspGraphDocument[],
  settings: AspSettings,
  options: { rootUri?: string; truncated?: AspGraphPayload["truncated"] } = {},
): Promise<AspGraphPayload> {
  const state = createAspGraphBuildState(settings, options.truncated);
  for (const document of documentsForGraph) {
    addFileGraphNode(state, document.uri, document.fileName, true);
  }
  for (const document of documentsForGraph) {
    await addDocumentToAspGraphAsync(state, document, settings);
    await yieldToEventLoop();
  }
  state.stats = recomputeAspGraphStats(state.nodes.values(), state.links.values());
  const payload = filterAspGraphPayload(
    {
      scope,
      rootUri: options.rootUri,
      nodes: [...state.nodes.values()],
      links: [...state.links.values()],
      stats: state.stats,
      truncated: state.truncated,
    },
    settings,
  );
  return payload;
}

function createAspGraphBuildState(
  settings: AspSettings,
  truncated?: AspGraphPayload["truncated"],
): AspGraphBuildState {
  return {
    nodes: new Map(),
    links: new Map(),
    declarations: new Set(),
    externalSymbols: createAspGraphExternalIndex(getVbscriptGraphExternalSymbols(settings)),
    truncated,
    stats: {
      files: 0,
      declarations: 0,
      references: 0,
      calls: 0,
      unresolvedReferences: 0,
      includes: 0,
      missingIncludes: 0,
      nodes: 0,
      links: 0,
    },
  };
}

function createAspGraphExternalIndex(symbols: VbGraphExternalSymbol[]): AspGraphExternalIndex {
  const byName = new Map<string, VbGraphExternalSymbol[]>();
  const memberByOwnerAndName = new Map<string, VbGraphExternalSymbol>();
  for (const symbol of symbols) {
    if (symbol.memberOf) {
      memberByOwnerAndName.set(externalMemberKey(symbol.memberOf, symbol.name), symbol);
      continue;
    }
    pushAspGraphMapItem(byName, symbol.name.toLowerCase(), symbol);
  }
  return { byName, memberByOwnerAndName };
}

function pushAspGraphMapItem<K, V>(map: Map<K, V[]>, key: K, value: V): void {
  const existing = map.get(key);
  if (existing) {
    existing.push(value);
  } else {
    map.set(key, [value]);
  }
}

function resolveExternalGraphSymbol(
  state: AspGraphBuildState,
  name: string | undefined,
): VbGraphExternalSymbol | undefined {
  if (!name) {
    return undefined;
  }
  return state.externalSymbols.byName.get(name.toLowerCase())?.[0];
}

function resolveExternalGraphCallSite(
  state: AspGraphBuildState,
  callSite: VbSymbolIndex["callSites"][number],
): VbGraphExternalSymbol | undefined {
  return callSite.memberName
    ? resolveExternalGraphMember(state, callSite.receiverName, callSite.memberName)
    : resolveExternalGraphSymbol(state, callSite.name);
}

function resolveExternalGraphDeferredRef(
  state: AspGraphBuildState,
  deferred: VbSymbolIndex["deferredExternalRefs"][number],
): VbGraphExternalSymbol | undefined {
  return deferred.memberName
    ? resolveExternalGraphMember(state, deferred.receiverName, deferred.memberName)
    : resolveExternalGraphSymbol(state, deferred.name);
}

function resolveExternalGraphMember(
  state: AspGraphBuildState,
  receiverName: string | undefined,
  memberName: string,
): VbGraphExternalSymbol | undefined {
  if (!receiverName) {
    return undefined;
  }
  const receiver = resolveExternalGraphSymbol(state, receiverName);
  const ownerName = receiver?.typeName ?? receiver?.name ?? receiverName;
  return state.externalSymbols.memberByOwnerAndName.get(externalMemberKey(ownerName, memberName));
}

function addExternalGraphNode(
  state: AspGraphBuildState,
  symbol: VbGraphExternalSymbol | undefined,
): string {
  if (!symbol) {
    return "";
  }
  const id = externalGraphNodeId(symbol);
  if (!state.nodes.has(id)) {
    state.nodes.set(id, {
      id,
      kind: "vbDeclaration",
      label: symbol.memberOf ? `${symbol.memberOf}.${symbol.name}` : symbol.name,
      declarationKind: symbol.declarationKind,
      memberOf: symbol.memberOf,
      group: symbol.category,
      origin: symbol.origin,
      externalKind: symbol.externalKind,
    });
  }
  return id;
}

function externalMemberKey(ownerName: string, memberName: string): string {
  return `${ownerName.toLowerCase()}\0${memberName.toLowerCase()}`;
}

function externalGraphNodeId(symbol: VbGraphExternalSymbol): string {
  return [
    "external",
    symbol.origin,
    symbol.externalKind,
    symbol.category,
    symbol.memberOf?.toLowerCase() ?? "",
    symbol.name.toLowerCase(),
  ].join(":");
}

function declarationSourceGraphNodeId(
  state: AspGraphBuildState,
  uri: string,
  scopeId: string | undefined,
): string {
  return scopeId && state.declarations.has(scopeId)
    ? declarationGraphNodeId(scopeId)
    : fileGraphNodeId(uri);
}

async function addDocumentToAspGraphAsync(
  state: AspGraphBuildState,
  document: AspGraphDocument,
  settings: AspSettings,
): Promise<void> {
  const graphIndex = await graphFileIndexForDocumentAsync(document, settings);
  const index = graphIndex.vbSymbolIndex;
  const fileNode = fileGraphNodeId(document.uri);
  for (const include of graphIndex.includeRefs) {
    const resolved = await resolveIncludePathDetailsAsync(
      document.uri,
      include.path,
      include.mode,
      settings,
    );
    const targetUri = pathToFileUri(resolved.fileName);
    addFileGraphNode(state, targetUri, resolved.fileName, resolved.exists);
    state.stats.includes += 1;
    if (!resolved.exists) {
      state.stats.missingIncludes += 1;
    }
    addAspGraphLink(state, {
      source: fileNode,
      target: fileGraphNodeId(targetUri),
      kind: "include",
      label: include.mode === "virtual" ? `virtual ${include.path}` : include.path,
      ranges: [{ uri: document.uri, range: include.range }],
      include: {
        path: include.path,
        mode: include.mode,
        exists: resolved.exists,
        resolvedUri: targetUri,
        actualPath: resolved.actualPath,
        pathCaseMatches: resolved.pathCaseMatches,
      },
    });
  }
  for (const declaration of index.declarations) {
    const declarationNode = declarationGraphNodeId(declaration.id);
    state.declarations.add(declaration.id);
    state.stats.declarations += 1;
    state.nodes.set(declarationNode, {
      id: declarationNode,
      kind: "vbDeclaration",
      label: declaration.memberOf
        ? `${declaration.memberOf}.${declaration.name}`
        : declaration.name,
      uri: document.uri,
      range: declaration.nameRange,
      declarationKind: declaration.kind,
      memberOf: declaration.memberOf,
      bindingScope: declaration.bindingScope,
      group: declaration.kind,
      origin: "source",
    });
    addAspGraphLink(state, {
      source: declarationSourceGraphNodeId(state, document.uri, declaration.scopeId),
      target: declarationNode,
      kind: "declares",
      label: "declares",
      ranges: [{ uri: document.uri, range: declaration.nameRange }],
    });
  }
  for (const reference of index.references) {
    if (reference.role === "call" || reference.role === "new" || reference.role === "member") {
      continue;
    }
    const external = reference.resolvedId
      ? undefined
      : resolveExternalGraphSymbol(state, reference.name);
    if (!reference.resolvedId && !external) {
      continue;
    }
    const target = reference.resolvedId
      ? declarationGraphNodeId(reference.resolvedId)
      : addExternalGraphNode(state, external);
    state.stats.references += 1;
    addAspGraphLink(state, {
      source: scopeGraphNodeId(state, document.uri, reference.scopeId),
      target,
      kind: "references",
      label: reference.role,
      role: reference.role,
      ranges: [{ uri: document.uri, range: reference.range }],
    });
  }
  for (const callSite of index.callSites) {
    state.stats.calls += 1;
    const external = callSite.resolvedId
      ? undefined
      : resolveExternalGraphCallSite(state, callSite);
    const target = callSite.resolvedId
      ? declarationGraphNodeId(callSite.resolvedId)
      : external
        ? addExternalGraphNode(state, external)
        : unresolvedGraphNodeId(document.uri, callSite.deferredKey ?? callSite.name, callSite.name);
    if (!callSite.resolvedId && !external) {
      addUnresolvedGraphNode(
        state,
        document.uri,
        callSite.deferredKey ?? callSite.name,
        callSite.name,
        callSite.range,
        callSite.callKind,
      );
    }
    addAspGraphLink(state, {
      source: scopeGraphNodeId(state, document.uri, callSite.scopeId),
      target,
      kind: "calls",
      label: callSite.callKind,
      role: callSite.callKind,
      ranges: [{ uri: document.uri, range: callSite.range }],
    });
  }
  for (const deferred of index.deferredExternalRefs) {
    const external = resolveExternalGraphDeferredRef(state, deferred);
    if (external) {
      continue;
    }
    state.stats.unresolvedReferences += 1;
    const target = unresolvedGraphNodeId(document.uri, deferred.key, deferred.name);
    addUnresolvedGraphNode(
      state,
      document.uri,
      deferred.key,
      deferred.name,
      deferred.range,
      deferred.role,
    );
    addAspGraphLink(state, {
      source: scopeGraphNodeId(state, document.uri, deferred.scopeId),
      target,
      kind: "unresolvedReference",
      label: deferred.role,
      role: deferred.role,
      ranges: [{ uri: document.uri, range: deferred.range }],
    });
  }
}

async function graphDocumentFromCachedAsync(
  cached: CachedDocument,
  settings: AspSettings,
): Promise<AspGraphDocument> {
  const fileName = normalizeFileName(uriToFileName(cached.source.uri));
  const identity = await includeDocumentSourceIdentityAsync(fileName, settings);
  return {
    uri: cached.source.uri,
    fileName,
    text: identity?.text ?? cached.parsed.text,
    source: identity?.source ?? {
      fileName,
      mtimeMs: cached.source.version,
      size: cached.parsed.text.length,
    },
    diskBacked: identity?.diskBacked ?? false,
  };
}

function graphDocumentFromIncludeEntry(entry: IncludeDocumentCacheEntry): AspGraphDocument {
  return {
    uri: entry.uri,
    fileName: entry.fileName,
    text: entry.parsed.text,
    source: entry.source,
    diskBacked: !documents.get(entry.uri),
  };
}

async function graphFileIndexForDocumentAsync(
  document: AspGraphDocument,
  settings: AspSettings,
): Promise<GraphFileIndex> {
  const settingsKey = graphFileIndexSettingsKey(settings);
  const key = JSON.stringify({
    fileName: document.fileName,
    source: document.source,
    settings: settingsKey,
    text: document.diskBacked ? undefined : textFingerprint(document.text),
  });
  const existing = graphFileIndexCache.get(document.fileName);
  if (existing?.key === key) {
    existing.lastUsed = Date.now();
    return existing;
  }
  const pending = graphFileIndexInFlight.get(key);
  if (pending) {
    return pending;
  }
  const promise = (async () => {
    const includeRefsEntry = await includeDocumentLoader
      .readIncludeRefsAsync(document.fileName, settings, { allowRead: true })
      .catch((error) => {
        logDiskAnalysisCacheError("graphIncludeRefs.read", error);
        return undefined;
      });
    if (document.diskBacked) {
      const cachedIndex = await diskAnalysisCache
        .readVbSymbolIndex({ source: document.source, settingsKey })
        .catch((error) => {
          logDiskAnalysisCacheError("graphVbIndex.read", error);
          return undefined;
        });
      if (cachedIndex) {
        const entry = graphFileIndexFromDisk(document.fileName, key, cachedIndex, includeRefsEntry);
        graphFileIndexCache.set(document.fileName, entry);
        pruneGraphFileIndexCache();
        logDebugSummary(settings, `[asp-lsp] graphVbIndex.hit: ${document.uri}`);
        return entry;
      }
      logDebugSummary(settings, `[asp-lsp] graphVbIndex.miss: ${document.uri}`);
    }
    const extracted = extractVbscriptSymbolIndex(document.uri, document.text, settings);
    const includeRefs =
      includeRefsEntry && sameDiskAnalysisSource(includeRefsEntry.source, document.source)
        ? includeRefsEntry.includeRefs
        : extracted.includeRefs;
    const vbSymbolIndex: VbSymbolIndex = { ...extracted, includeRefs };
    const entry: GraphFileIndex = {
      key,
      uri: document.uri,
      fileName: document.fileName,
      source: document.source,
      includeRefs,
      vbSymbolIndex,
      fingerprint: graphFileIndexFingerprint(vbSymbolIndex),
      lastUsed: Date.now(),
    };
    graphFileIndexCache.set(document.fileName, entry);
    pruneGraphFileIndexCache();
    if (document.diskBacked) {
      await diskAnalysisCache.writeVbSymbolIndex(diskVbSymbolIndexCacheEntry(entry, settings));
      logDebugSummary(settings, `[asp-lsp] graphVbIndex.write: ${document.uri}`);
    }
    return entry;
  })();
  graphFileIndexInFlight.set(key, promise);
  try {
    return await promise;
  } finally {
    if (graphFileIndexInFlight.get(key) === promise) {
      graphFileIndexInFlight.delete(key);
    }
  }
}

function graphFileIndexFingerprint(index: VbSymbolIndex): string {
  return textFingerprint(JSON.stringify(index));
}

function pruneGraphFileIndexCache(): void {
  while (graphFileIndexCache.size > graphFileIndexCacheMaxEntries) {
    const oldest = [...graphFileIndexCache.entries()].sort(
      (left, right) => left[1].lastUsed - right[1].lastUsed,
    )[0];
    if (!oldest) {
      return;
    }
    graphFileIndexCache.delete(oldest[0]);
  }
}

function invalidateGraphFileIndexFiles(fileNames: Iterable<string>): void {
  for (const fileName of fileNames) {
    const normalized = normalizeFileName(fileName);
    graphFileIndexCache.delete(normalized);
  }
  graphFileIndexInFlight.clear();
}

function clearGraphFileIndexCache(): void {
  graphFileIndexCache.clear();
  graphFileIndexInFlight.clear();
}

function addFileGraphNode(
  state: AspGraphBuildState,
  uri: string,
  fileName: string,
  exists: boolean,
): void {
  const id = fileGraphNodeId(uri);
  if (!state.nodes.has(id)) {
    state.stats.files += 1;
  }
  state.nodes.set(id, {
    id,
    kind: "file",
    label: path.basename(fileName),
    uri,
    fileName,
    exists,
    group: exists ? "file" : "missing",
  });
}

function addUnresolvedGraphNode(
  state: AspGraphBuildState,
  uri: string,
  key: string,
  name: string,
  range: Range,
  role: string,
): void {
  const id = unresolvedGraphNodeId(uri, key, name);
  if (state.nodes.has(id)) {
    return;
  }
  state.nodes.set(id, {
    id,
    kind: "vbUnresolved",
    label: name,
    uri,
    range,
    role,
    group: "unresolved",
  });
}

function addAspGraphLink(
  state: AspGraphBuildState,
  input: Omit<AspGraphLink, "id" | "count">,
): void {
  const key = JSON.stringify({
    source: input.source,
    target: input.target,
    kind: input.kind,
    role: input.role,
    includePath: input.include?.path,
  });
  const existing = state.links.get(key);
  if (existing) {
    existing.count += 1;
    existing.ranges.push(...input.ranges);
    return;
  }
  state.links.set(key, {
    ...input,
    id: `link:${state.links.size}`,
    count: 1,
  });
}

function filterAspGraphPayload(payload: AspGraphPayload, settings: AspSettings): AspGraphPayload {
  const graphSettings = normalizeGraphSettings(settings);
  const nodes = payload.nodes.filter((node) => isVisibleAspGraphNode(node, graphSettings));
  const visibleNodeIds = new Set(nodes.map((node) => node.id));
  const links = payload.links.filter(
    (link) =>
      isVisibleAspGraphLinkKind(link.kind, graphSettings) &&
      visibleNodeIds.has(link.source) &&
      visibleNodeIds.has(link.target),
  );
  return {
    ...payload,
    nodes,
    links,
    stats: recomputeAspGraphStats(nodes, links),
  };
}

function isVisibleAspGraphNode(
  node: AspGraphNode,
  settings: NonNullable<AspSettings["graph"]>,
): boolean {
  if (node.kind === "file") {
    return node.exists === false
      ? settings.showMissingFiles !== false
      : settings.showFiles !== false;
  }
  if (node.kind === "vbUnresolved") {
    return settings.showUnresolvedReferences !== false;
  }
  if (node.origin === "builtin") {
    return (
      settings.showBuiltinSymbols === true &&
      (node.externalKind !== "member" || settings.showObjectMembers === true)
    );
  }
  if (node.origin === "configured") {
    if (node.group === "configuredGlobal") {
      return settings.showConfiguredGlobals === true;
    }
    if (node.group === "configuredComType") {
      return (
        settings.showConfiguredComTypes === true &&
        (node.externalKind !== "member" || settings.showObjectMembers === true)
      );
    }
    return false;
  }
  return isVisibleSourceGraphDeclaration(node, settings);
}

function isVisibleSourceGraphDeclaration(
  node: AspGraphNode,
  settings: NonNullable<AspSettings["graph"]>,
): boolean {
  switch (node.declarationKind) {
    case "parameter":
      return settings.showFunctionParameters === true;
    case "variable":
      return node.bindingScope === "local"
        ? settings.showLocalVariables === true
        : settings.showGlobalVariables !== false;
    case "constant":
      if (node.bindingScope === "local") {
        return settings.showLocalConstants === true;
      }
      return node.memberOf
        ? settings.showClassConstants === true
        : settings.showGlobalConstants !== false;
    case "field":
      return settings.showClassFields === true;
    case "method":
      return settings.showClassMethods === true;
    case "property":
      return settings.showClassProperties === true;
    case "class":
      return settings.showClasses !== false;
    case "function":
      return settings.showFunctions !== false;
    case "sub":
      return settings.showSubs !== false;
    default:
      return true;
  }
}

function isVisibleAspGraphLinkKind(
  kind: AspGraphLinkKind,
  settings: NonNullable<AspSettings["graph"]>,
): boolean {
  switch (kind) {
    case "include":
      return settings.showIncludeLinks !== false;
    case "declares":
      return settings.showDeclarationLinks !== false;
    case "references":
      return settings.showReferenceLinks !== false;
    case "calls":
      return settings.showCallLinks !== false;
    case "unresolvedReference":
      return settings.showUnresolvedReferences !== false;
  }
}

function recomputeAspGraphStats(
  nodes: Iterable<AspGraphNode>,
  links: Iterable<AspGraphLink>,
): AspGraphPayload["stats"] {
  const stats: AspGraphPayload["stats"] = {
    files: 0,
    declarations: 0,
    references: 0,
    calls: 0,
    unresolvedReferences: 0,
    includes: 0,
    missingIncludes: 0,
    nodes: 0,
    links: 0,
  };
  for (const node of nodes) {
    stats.nodes += 1;
    if (node.kind === "file") {
      stats.files += 1;
    } else if (node.kind === "vbDeclaration") {
      stats.declarations += 1;
    }
  }
  for (const link of links) {
    stats.links += 1;
    if (link.kind === "include") {
      stats.includes += 1;
      if (link.include?.exists === false) {
        stats.missingIncludes += 1;
      }
    } else if (link.kind === "references") {
      stats.references += 1;
    } else if (link.kind === "calls") {
      stats.calls += 1;
    } else if (link.kind === "unresolvedReference") {
      stats.unresolvedReferences += 1;
    }
  }
  return stats;
}

function fileGraphNodeId(uri: string): string {
  return `file:${uri}`;
}

function declarationGraphNodeId(id: string): string {
  return `vb:${id}`;
}

function unresolvedGraphNodeId(uri: string, key: string, name: string): string {
  return `unresolved:${uri}:${key}:${name.toLowerCase()}`;
}

function scopeGraphNodeId(
  state: AspGraphBuildState,
  uri: string,
  scopeId: string | undefined,
): string {
  return scopeId && state.declarations.has(scopeId)
    ? declarationGraphNodeId(scopeId)
    : fileGraphNodeId(uri);
}

function emptyAspGraphPayload(scope: AspGraphScope, rootUri?: string): AspGraphPayload {
  return {
    scope,
    rootUri,
    nodes: [],
    links: [],
    stats: {
      files: 0,
      declarations: 0,
      references: 0,
      calls: 0,
      unresolvedReferences: 0,
      includes: 0,
      missingIncludes: 0,
      nodes: 0,
      links: 0,
    },
  };
}

function isClassicAspGraphUri(uri: string): boolean {
  if (!uri.startsWith("file://")) {
    return false;
  }
  return isAspWorkspaceFile(path.basename(uriToFileName(uri)));
}

async function codeLensesAsync(cached: CachedDocument): Promise<CodeLens[]> {
  const documentSettings = cachedSettings(cached.source.uri);
  const settings = documentSettings.codeLens;
  const lenses: CodeLens[] = [];
  if (settings?.references !== false) {
    await hydrateCachedVbscriptCstAsync(cached, documentSettings, "codeLens");
    const symbols = await collectVbscriptSymbolsAsync(
      cached.parsed,
      vbProjectContextSettings(documentSettings),
    );
    for (const symbol of symbols.filter(
      (item) =>
        item.sourceUri === cached.source.uri &&
        ["function", "sub", "class", "method", "property"].includes(item.kind),
    )) {
      lenses.push({
        range: symbol.range,
        data: vbReferenceCodeLensData(symbol),
      });
    }
  }
  if (settings?.includes !== false) {
    const localizer = localizerForUri(cached.source.uri);
    for (const include of cached.parsed.includes) {
      const target = await resolveIncludePathAsync(
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

async function resolveCodeLens(lens: CodeLens): Promise<CodeLens> {
  const data = vbReferenceCodeLensDataFromUnknown(lens.data);
  if (!data) {
    return lens;
  }
  const cached = await getFreshCachedAsync(data.uri);
  if (!cached) {
    return lens;
  }
  const symbol = await vbSymbolForCodeLensDataAsync(cached, data);
  if (!symbol) {
    return lens;
  }
  const settings = cachedSettings(cached.source.uri);
  const options = {
    includeDeclaration: false,
    includeFunctionReturnAssignments: false,
  };
  const references =
    settings.codeLens?.referenceScope === "workspace"
      ? await workspaceVbscriptReferencesForSymbol(cached, symbol, settings, options)
      : await analyzedVbscriptReferencesForSymbolAsync(cached, symbol, settings, options);
  const localizer = localizerForUri(cached.source.uri);
  return {
    ...lens,
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
  };
}

function vbReferenceCodeLensData(symbol: VbSymbol): VbReferenceCodeLensData {
  return {
    kind: "vbscript-reference",
    uri: symbol.sourceUri,
    name: symbol.name,
    symbolKind: symbol.kind,
    memberOf: symbol.memberOf,
    line: symbol.range.start.line,
    character: symbol.range.start.character,
  };
}

function vbReferenceCodeLensDataFromUnknown(value: unknown): VbReferenceCodeLensData | undefined {
  if (!value || typeof value !== "object") {
    return undefined;
  }
  const data = value as Partial<VbReferenceCodeLensData>;
  return data.kind === "vbscript-reference" &&
    typeof data.uri === "string" &&
    typeof data.name === "string" &&
    typeof data.symbolKind === "string" &&
    typeof data.line === "number" &&
    typeof data.character === "number"
    ? (data as VbReferenceCodeLensData)
    : undefined;
}

async function vbSymbolForCodeLensDataAsync(
  cached: CachedDocument,
  data: VbReferenceCodeLensData,
): Promise<VbSymbol | undefined> {
  const settings = cachedSettings(data.uri);
  await hydrateCachedVbscriptCstAsync(cached, settings, "codeLens");
  return (
    await collectVbscriptSymbolsAsync(cached.parsed, vbProjectContextSettings(settings))
  ).find(
    (symbol) =>
      symbol.sourceUri === data.uri &&
      symbol.name.toLowerCase() === data.name.toLowerCase() &&
      symbol.kind === data.symbolKind &&
      (symbol.memberOf ?? "").toLowerCase() === (data.memberOf ?? "").toLowerCase() &&
      symbol.range.start.line === data.line &&
      symbol.range.start.character === data.character,
  );
}

async function analyzedVbscriptReferencesForSymbolAsync(
  cached: CachedDocument,
  symbol: VbSymbol,
  settings: AspSettings,
  options: VbReferenceOptions,
): Promise<VbReference[]> {
  const references = new Map<string, VbReference>();
  const analyzed = await analyzedCachedDocumentsAsync();
  const currentContext = await localVbReferenceContextAsync(cached, settings);
  const currentTarget = equivalentVbSymbol(currentContext.symbols ?? [], symbol) ?? symbol;
  addVbReferences(
    references,
    getVbscriptReferencesForSymbol(currentTarget, currentContext, options),
  );

  for (const candidate of analyzed) {
    if (candidate.source.uri === cached.source.uri) {
      continue;
    }
    const warmed = candidate.analysis?.vbProjectContext?.context;
    const warmedTarget = warmed ? equivalentVbSymbol(warmed.symbols ?? [], symbol) : undefined;
    if (warmedTarget) {
      addVbReferences(references, getVbscriptReferencesForSymbol(warmedTarget, warmed, options));
      continue;
    }
    addVbReferences(
      references,
      fallbackWorkspaceExternalReferences(
        (await workspaceVbReferenceSummaryForCachedAsync(candidate, settings)).summary,
        symbol,
      ),
    );
  }

  return [...references.values()].sort(vbReferenceOrder);
}

async function analyzedCachedDocumentsAsync(): Promise<CachedDocument[]> {
  const byUri = new Map<string, CachedDocument>();
  for (const document of documents.all()) {
    const cached = await ensureFreshCachedDocumentAsync(document);
    byUri.set(cached.source.uri, cached);
  }
  for (const cached of cache.values()) {
    byUri.set(cached.source.uri, cached);
  }
  return [...byUri.values()];
}

async function localVbReferenceContextAsync(
  cached: CachedDocument,
  settings: AspSettings,
): Promise<VbProjectContext> {
  const contextSettings = vbProjectContextSettings(settings);
  await hydrateCachedVbscriptCstAsync(cached, settings, "references");
  const symbols = await collectVbscriptSymbolsAsync(cached.parsed, contextSettings);
  return {
    documents: [cached.parsed],
    includeSummaryUris: [cached.source.uri],
    symbols,
    typeEnvironment: buildVbTypeEnvironment(cached.parsed, { ...contextSettings, symbols }),
    externalRefUsages:
      (await cachedFileAnalysisSummaryAsync(cached, contextSettings, settings)).vbscript
        ?.externalRefUsages ?? [],
    ...contextSettings,
  };
}

async function onTypeFormattingAsync(
  cached: CachedDocument,
  position: Position,
  character: string,
  formattingOptions: FormattingOptions,
): Promise<TextEdit[]> {
  if (character === ">") {
    const jsEdits = await jsOnTypeFormattingAsync(cached, position, character, formattingOptions);
    if (jsEdits) {
      return jsEdits;
    }
    const htmlCloseTagEdits = htmlCloseTagOnTypeFormatting(cached, position);
    if (htmlCloseTagEdits) {
      return htmlCloseTagEdits;
    }
    if (isHtmlOnTypePosition(cached, position)) {
      return [];
    }
    return aspCloseOnTypeFormatting(cached, position, formattingOptions) ?? [];
  }
  const jsEdits = await jsOnTypeFormattingAsync(cached, position, character, formattingOptions);
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

async function jsOnTypeFormattingAsync(
  cached: CachedDocument,
  position: Position,
  character: string,
  formattingOptions: FormattingOptions,
): Promise<TextEdit[] | undefined> {
  const context = await jsContextAtAsync(cached, position);
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

function htmlCloseTagOnTypeFormatting(
  cached: CachedDocument,
  position: Position,
): TextEdit[] | undefined {
  const offset = Math.max(0, cached.source.offsetAt(position) - 1);
  const region = findRegionAt(cached.parsed, offset);
  if (!region || region.language !== "html") {
    return undefined;
  }
  const virtual = getCachedVirtual(cached, "html");
  const virtualPosition = virtual?.sourceMap.toVirtualPosition(position);
  if (!virtual || !virtualPosition) {
    return undefined;
  }
  const document = toTextDocument(virtual);
  const completion = htmlService.doTagComplete(
    document,
    virtualPosition,
    htmlService.parseHTMLDocument(document),
  );
  const newText = completion?.replace(/\$0/g, "");
  return newText
    ? [
        {
          range: { start: position, end: position },
          newText,
        },
      ]
    : undefined;
}

function isHtmlOnTypePosition(cached: CachedDocument, position: Position): boolean {
  const offset = Math.max(0, cached.source.offsetAt(position) - 1);
  return findRegionAt(cached.parsed, offset)?.language === "html";
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

async function buildSemanticTokensAsync(
  cached: CachedDocument,
  range?: Range,
): Promise<SemanticTokens> {
  const settings = cachedSettings(cached.source.uri);
  return buildSemanticTokensWithContextAsync(
    cached,
    await interactiveVbProjectContextAsync(cached, settings),
    range,
    settings,
  );
}

async function buildSemanticTokensWithContextAsync(
  cached: CachedDocument,
  vbContext: VbProjectContext,
  range?: Range,
  settings = cachedSettings(cached.source.uri),
): Promise<SemanticTokens> {
  const full = !range;
  const jsVirtuals = jsVirtualDocuments(cached);
  const fullCacheKey = full ? semanticTokensFullCacheKey(cached, settings, jsVirtuals) : undefined;
  const analysis = full ? analysisFor(cached) : undefined;
  if (fullCacheKey && analysis?.semanticTokensFull?.key === fullCacheKey) {
    return { data: [...analysis.semanticTokensFull.data] };
  }
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
  for (const semanticToken of getVbscriptSemanticTokens(cached.parsed, vbContext, range)) {
    const offset = cached.source.offsetAt(semanticToken.range.start);
    if (offset < rangeStart || offset >= rangeEnd) {
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
  const javascriptCacheKey = semanticJavascriptTokensCacheKey(cached, settings, jsVirtuals);
  const javascriptDeferred = await addEmbeddedSemanticTokensAsync(
    tokens,
    cached,
    rangeStart,
    rangeEnd,
    {
      settings,
      jsVirtuals,
      deferLargeJavascript: full,
      javascriptCacheKey,
    },
  );
  const uniqueTokens = dedupeSemanticTokens(tokens).sort(
    (left, right) => left.line - right.line || left.character - right.character,
  );
  const result = semanticTokensFromData(uniqueTokens);
  if (fullCacheKey && !javascriptDeferred) {
    analysisFor(cached).semanticTokensFull = {
      key: fullCacheKey,
      data: [...result.data],
    };
  }
  return result;
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
  if (offset < rangeStart || offset >= rangeEnd) {
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
    if (regions[middle].end <= rangeStart) {
      low = middle + 1;
    } else {
      high = middle;
    }
  }
  const result: AspRegion[] = [];
  for (let index = low; index < regions.length; index += 1) {
    const region = regions[index];
    if (region.start >= rangeEnd) {
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
  const identifierTokens = vbIdentifierTokensInSourceRange(cached, rangeStart, rangeEnd);
  const tokensByLowerName = new Map<string, VbToken[]>();
  for (const token of identifierTokens) {
    const lowerName = token.text.toLowerCase();
    const tokensForName = tokensByLowerName.get(lowerName);
    if (tokensForName) {
      tokensForName.push(token);
    } else {
      tokensByLowerName.set(lowerName, [token]);
    }
  }
  const candidates = (context.symbols ?? []).filter(
    (symbol) =>
      symbol.sourceUri !== cached.parsed.uri &&
      !symbol.scopeName &&
      !symbol.memberOf &&
      tokensByLowerName.has(symbol.name.toLowerCase()),
  );
  for (const symbol of candidates) {
    const tokenType = fallbackVbSemanticTokenType(symbol.kind);
    if (!tokenType) {
      continue;
    }
    for (const token of tokensByLowerName.get(symbol.name.toLowerCase()) ?? []) {
      const position = cached.source.positionAt(token.start);
      tokens.push({
        line: position.line,
        character: position.character,
        length: token.text.length,
        tokenType,
        tokenModifiers: fallbackVbSemanticTokenModifiers(symbol),
      });
    }
  }
}

function vbIdentifierTokensInSourceRange(
  cached: CachedDocument,
  rangeStart: number,
  rangeEnd: number,
): VbToken[] {
  const tokens: VbToken[] = [];
  for (const child of cached.parsed.cst.children) {
    if (
      child.language !== "vbscript" ||
      !child.vbscript ||
      child.contentEnd < rangeStart ||
      child.contentStart > rangeEnd
    ) {
      continue;
    }
    for (const token of child.vbscript.tokens) {
      if (token.kind === "identifier" && token.end >= rangeStart && token.start <= rangeEnd) {
        tokens.push(token);
      }
    }
  }
  return tokens;
}

function fallbackVbSemanticTokenType(kind: VbSymbolKind): string | undefined {
  if (kind === "function" || kind === "sub") {
    return "function";
  }
  if (kind === "class") {
    return "class";
  }
  if (kind === "constant") {
    return "constant";
  }
  if (kind === "variable") {
    return "variable";
  }
  return undefined;
}

function fallbackVbSemanticTokenModifiers(symbol: VbSymbol): readonly string[] | undefined {
  const modifiers: string[] = [];
  if (symbol.visibility === "public") {
    modifiers.push("public");
  }
  if (symbol.visibility === "private") {
    modifiers.push("private");
  }
  if (symbol.kind === "constant") {
    modifiers.push("readonly");
  }
  return modifiers.length > 0 ? modifiers : undefined;
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
  if (offset < rangeStart || offset >= rangeEnd || range.start.line !== range.end.line) {
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

function semanticTokensFromData(tokens: readonly SemanticTokenData[]): SemanticTokens {
  const builder = new SemanticTokensBuilder();
  for (const token of tokens) {
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

function semanticTokensFullCacheKey(
  cached: CachedDocument,
  settings: AspSettings,
  jsVirtuals: readonly VirtualDocument[],
): string {
  return JSON.stringify({
    uri: cached.source.uri,
    version: cached.source.version,
    generation: cached.generation,
    parseSettings: parseSettingsIdentity(settings),
    diagnostics: diagnosticsIdentity(settings),
    workspaceGeneration,
    includeResolutionGeneration,
    jsProjectGeneration,
    javascript: jsVirtualFingerprints(jsVirtuals),
  });
}

function semanticJavascriptTokensCacheKey(
  cached: CachedDocument,
  settings: AspSettings,
  jsVirtuals: readonly VirtualDocument[],
): string {
  return JSON.stringify({
    uri: cached.source.uri,
    version: cached.source.version,
    generation: cached.generation,
    settings: jsProjectSettingsIdentity(settings),
    jsProjectGeneration,
    javascript: jsVirtualFingerprints(jsVirtuals),
  });
}

function jsVirtualFingerprints(jsVirtuals: readonly VirtualDocument[]) {
  return jsVirtuals.map((virtual) => ({
    uri: virtual.uri,
    languageId: virtual.languageId,
    text: textFingerprint(virtual.text),
  }));
}

function shouldDeferFullJavascriptSemanticTokens(
  cached: CachedDocument,
  jsVirtuals: readonly VirtualDocument[],
): boolean {
  return (
    cached.source.getText().length >= semanticTokensLargeSourceThreshold ||
    jsVirtuals.some((virtual) => virtual.text.length >= semanticTokensLargeJavascriptThreshold)
  );
}

async function addEmbeddedSemanticTokensAsync(
  tokens: SemanticTokenData[],
  cached: CachedDocument,
  rangeStart: number,
  rangeEnd: number,
  options: EmbeddedSemanticTokenOptions,
): Promise<boolean> {
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
  if (
    options.jsVirtuals.length > 0 &&
    options.deferLargeJavascript &&
    shouldDeferFullJavascriptSemanticTokens(cached, options.jsVirtuals)
  ) {
    const cachedJavascript = analysisFor(cached).semanticJavascriptTokens;
    if (cachedJavascript) {
      tokens.push(...cachedJavascript.tokens);
      return false;
    }
    if (pendingSemanticJavascriptTokenBuilds.has(options.javascriptCacheKey)) {
      const javascriptTokens = await computeJavascriptSemanticTokensAsync(
        cached,
        options.settings,
        options.jsVirtuals,
        rangeStart,
        rangeEnd,
      );
      analysisFor(cached).semanticJavascriptTokens = {
        key: options.javascriptCacheKey,
        tokens: javascriptTokens,
      };
      tokens.push(...javascriptTokens);
      return false;
    }
    logDebugSummary(
      options.settings,
      `[asp-lsp] semanticTokens.javascript.deferred: ${cached.source.uri}, virtuals=${options.jsVirtuals.length}`,
    );
    scheduleSemanticJavascriptTokenCache(
      cached,
      options.settings,
      options.jsVirtuals,
      options.javascriptCacheKey,
    );
    return true;
  }
  const jsVirtuals = options.jsVirtuals.filter((virtual) =>
    virtualOverlapsSourceRange(virtual, rangeStart, rangeEnd),
  );
  if (jsVirtuals.length === 0) {
    return false;
  }
  tokens.push(
    ...(await computeJavascriptSemanticTokensAsync(
      cached,
      options.settings,
      jsVirtuals,
      rangeStart,
      rangeEnd,
    )),
  );
  return false;
}

async function computeJavascriptSemanticTokensAsync(
  cached: CachedDocument,
  settings: AspSettings,
  jsVirtuals: readonly VirtualDocument[],
  rangeStart: number,
  rangeEnd: number,
): Promise<SemanticTokenData[]> {
  const tokens: SemanticTokenData[] = [];
  for (const virtual of jsVirtuals) {
    if (!virtualOverlapsSourceRange(virtual, rangeStart, rangeEnd)) {
      continue;
    }
    const project = await createJsLanguageServiceAsync(virtual, settings);
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
  return tokens;
}

function scheduleSemanticJavascriptTokenCache(
  cached: CachedDocument,
  settings: AspSettings,
  jsVirtuals: readonly VirtualDocument[],
  javascriptCacheKey: string,
): void {
  if (pendingSemanticJavascriptTokenBuilds.has(javascriptCacheKey)) {
    return;
  }
  const sourceLength = cached.source.getText().length;
  const promise = new Promise<void>((resolve) => {
    setImmediate(() => {
      const current = cache.get(cached.source.uri);
      if (
        current !== cached ||
        current.source.version !== cached.source.version ||
        current.generation !== cached.generation
      ) {
        pendingSemanticJavascriptTokenBuilds.delete(javascriptCacheKey);
        resolve();
        return;
      }
      void computeAndCacheSemanticJavascriptTokensAsync(
        cached,
        settings,
        jsVirtuals,
        javascriptCacheKey,
        sourceLength,
      ).finally(resolve);
    });
  });
  pendingSemanticJavascriptTokenBuilds.set(javascriptCacheKey, promise);
}

async function computeAndCacheSemanticJavascriptTokensAsync(
  cached: CachedDocument,
  settings: AspSettings,
  jsVirtuals: readonly VirtualDocument[],
  javascriptCacheKey: string,
  sourceLength: number,
): Promise<void> {
  try {
    const tokens = await computeJavascriptSemanticTokensAsync(
      cached,
      settings,
      jsVirtuals,
      0,
      sourceLength,
    );
    const current = cache.get(cached.source.uri);
    if (
      current !== cached ||
      current.source.version !== cached.source.version ||
      current.generation !== cached.generation
    ) {
      return;
    }
    const currentJsVirtuals = jsVirtualDocuments(current);
    const currentSettings = cachedSettings(current.source.uri);
    const currentJavascriptCacheKey = semanticJavascriptTokensCacheKey(
      current,
      currentSettings,
      currentJsVirtuals,
    );
    const analysis = analysisFor(current);
    analysis.semanticJavascriptTokens = {
      key: currentJavascriptCacheKey,
      tokens,
    };
    analysis.semanticTokensFull = undefined;
    clearSemanticTokensForUri(current.source.uri);
    logDebugSummary(
      currentSettings,
      `[asp-lsp] semanticTokens.javascript.cached: ${current.source.uri}, tokens=${tokens.length}`,
    );
    requestSemanticTokensRefresh("semanticTokens.javascript.cached");
  } catch (error) {
    connection.console.warn(
      `[asp-lsp] semanticTokens.javascript.cache.failed: ${cached.source.uri}, error=${errorMessage(error)}`,
    );
  } finally {
    pendingSemanticJavascriptTokenBuilds.delete(javascriptCacheKey);
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
  for (const span of semanticVirtualSpansForSourceRange(virtual, rangeStart, rangeEnd)) {
    addJavaScriptSemanticTokensForSpan(
      tokens,
      cached,
      virtual,
      service,
      fileName,
      span,
      rangeStart,
      rangeEnd,
    );
  }
}

function addJavaScriptSemanticTokensForSpan(
  tokens: SemanticTokenData[],
  cached: CachedDocument,
  virtual: VirtualDocument,
  service: ts.LanguageService,
  fileName: string,
  span: { start: number; length: number },
  rangeStart: number,
  rangeEnd: number,
): void {
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

function semanticVirtualSpansForSourceRange(
  virtual: VirtualDocument,
  rangeStart: number,
  rangeEnd: number,
): Array<{ start: number; length: number }> {
  const spans: Array<{ start: number; length: number }> = [];
  for (const segment of virtual.sourceMap.segments) {
    const sourceStart = Math.max(segment.sourceStart, rangeStart);
    const sourceEnd = Math.min(segment.sourceEnd, rangeEnd);
    if (sourceStart >= sourceEnd) {
      continue;
    }
    const virtualStart = segment.virtualStart + (sourceStart - segment.sourceStart);
    const virtualEnd = segment.virtualStart + (sourceEnd - segment.sourceStart);
    spans.push({ start: virtualStart, length: virtualEnd - virtualStart });
  }
  return spans.length > 0 ? spans : [{ start: 0, length: virtual.text.length }];
}

function virtualOverlapsSourceRange(
  virtual: VirtualDocument,
  rangeStart: number,
  rangeEnd: number,
): boolean {
  return virtual.sourceMap.segments.some(
    (segment) => segment.sourceEnd > rangeStart && segment.sourceStart < rangeEnd,
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
    if (sourceStart >= sourceEnd) {
      continue;
    }
    const virtualStart = segment.virtualStart + (sourceStart - segment.sourceStart);
    const virtualEnd = segment.virtualStart + (sourceEnd - segment.sourceStart);
    spans.push({ start: virtualStart, length: Math.max(1, virtualEnd - virtualStart) });
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
  await jsDiagnosticsWorkerPool?.close();
  jsDiagnosticsWorkerPool = undefined;
  await vbDiagnosticsWorkerPool?.close();
  vbDiagnosticsWorkerPool = undefined;
});

documents.listen(connection);
connection.listen();
