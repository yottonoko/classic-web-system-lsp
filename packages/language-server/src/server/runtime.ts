import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { JsDiagnosticsWorkerPool } from "../js-worker-pool";
import { VbDiagnosticsWorkerPool } from "../vb-worker-pool";
import { VbReferencesWorkerPool } from "../vb-references-worker-pool";
import { BulkWorkerPool } from "../asp-graph/bulk-worker-pool";
import {
  DiskAnalysisCache,
  diskContentHash,
  type DiskAnalysisBuilderState,
  type DiskIncludeRefsCacheEntry,
  type DiskAnalysisSourceMetadata,
  type DiskParsedDocumentCacheEntry,
  type DiskSummaryCacheEntry,
  type DiskWorkspaceIndexedDocument,
  type DiskVbSymbolIndexCacheEntry,
} from "../disk-analysis-cache";
import {
  fileIdentityKeyFromFileName,
  fileIdentityKeyFromUri,
  sameFileIdentityUri,
} from "../file-identity";
import { fsGateway } from "../fs-gateway";
import { WorkspaceIncludeGraph } from "../include-graph-index";
import { DebugLogFileWriter, type DebugLogFileLevel } from "../debug-log-file";
import {
  cache,
  cachedDocumentForUri,
  cachedDocumentsForUri,
  deleteCachedDocumentsForUri,
  inFlightDocumentRefreshes,
  maxCachedDocumentEditHistory,
  documentStore,
  touchCachedDocument,
  type CachedAnalysis,
  type CachedCssContext,
  type CachedDocument,
  type CachedJsDiagnosticsEntry,
  type CachedTsDiagnostic,
  type DiagnosticCacheEntry,
  type DocumentIdentity,
  type InFlightDocumentRefresh,
  type SemanticTokenData,
} from "../document-store";
import {
  AspJsScriptSnapshot,
  aspGraphPayloadCache,
  graphFileIndexCache,
  graphFileIndexCacheMaxEntries,
  graphFileIndexInFlight,
  jsDirectoryExistsCache,
  jsDirectoriesCache,
  jsDocumentRegistry,
  jsFileExistsCache,
  jsFileStatCache,
  jsLanguageServiceCache,
  jsOpenProjectFilesCache,
  jsProjectConfigCache,
  jsReadDirectoryCache,
  jsReadFileCache,
  jsRealpathCache,
  jsScriptSnapshots,
  lightweightJsUnusedDiagnosticsCache,
  memoryManagedAnalysisCaches,
  maxJsOpenProjectFilesCacheEntries,
  maxLightweightJsUnusedCacheEntries,
  maxVbProjectContextCacheEntries,
  maxWorkspaceVbReferenceBatchCacheEntries,
  maxWorkspaceVbReferenceReachabilityCacheEntries,
  maxWorkspaceVbReferenceReachabilityDocuments,
  maxWorkspaceVbReferenceRequestCacheEntries,
  maxWorkspaceVbReferenceWorkerCacheEntries,
  vbProjectContextCache,
  workspaceVbReferenceBatchCompleted,
  workspaceVbReferenceBatchInFlight,
  workspaceVbReferenceReachabilityCache,
  workspaceVbReferenceReachabilityConcurrency,
  workspaceVbReferenceReachabilityInFlight,
  workspaceVbReferenceRequestCompleted,
  workspaceVbReferenceRequestInFlight,
  workspaceVbReferenceWorkerBatchInFlight,
  workspaceVbReferenceWorkerCompleted,
  workspaceVbReferenceWorkerInFlight,
  type JsFileStat,
  type JsLanguageServiceProject,
  type JsProjectConfig,
  type JsProjectContext,
  type JsProjectFile,
  type VbProjectAnalysis,
  type VbProjectSummaryGraph,
  type WorkspaceVbReferenceReachabilityEntry,
  type WorkspaceVbReferenceReachabilityGraphNode,
  type WorkspaceVbReferenceReachabilityState,
} from "../analysis-caches";
import type {
  AnalysisCancellation,
  AspGraphBuildState,
  AspGraphDeclarationTypeHint,
  AspGraphDocument,
  AspGraphExternalIndex,
  AspGraphIndexedDocument,
  AspGraphLink,
  AspGraphLinkFilterCategory,
  AspGraphNode,
  AspGraphNodeCategory,
  AspGraphNodeKind,
  AspGraphNodeParameter,
  AspGraphPayload,
  AspGraphScope,
  AspGraphUpdatedNotification,
  FilePublicSignature,
  GraphCancellationToken,
  GraphFileIndex,
  GraphFileIndexOperationCache,
  PrecomputedIncludeReachability,
  VbProjectContextLimits,
  WorkspaceVbReferenceExecutionOptions,
  WorkspaceVbReferenceSummaryIncludeGraph,
} from "../asp-graph/types";
import {
  analysisCancellationFromToken,
  createAspGraphBuildService,
  graphCommandScope,
  graphCommandUri,
  throwIfGraphCancelled,
  type CollectIncludeTreeGraphDocumentsOptions,
  type CollectRelatedIncludeTreeOwnerGraphDocumentsOptions,
  type AspGraphBuildService,
  type AspGraphDocumentSource,
} from "../asp-graph/build";
import {
  runSpilledGraphIndexPipeline,
  type BulkGraphIndexPipelineProgressEvent,
} from "../asp-graph/bulk-pipeline";
import { createAnalysisExcelSheetsAsync, type AspGraphLocale } from "../analysis-excel/sheets";
import { writeAnalysisExcelWorkbookFile } from "../analysis-excel/stream-writer";
import type {
  JsDiagnosticsWorkerResponse,
  JsDiagnosticsWorkerVirtualDocument,
} from "../js-diagnostics-protocol";
import type {
  VbDiagnosticsWorkerContext,
  VbDiagnosticsWorkerDocument,
  VbDiagnosticsWorkerResponse,
} from "../vb-diagnostics-protocol";
import type {
  VbReferencesWorkerCandidate,
  VbReferencesWorkerCacheOptions,
  VbReferencesWorkerOpenDocument,
  VbReferencesWorkerResponse,
  VbReferencesWorkerTargetSymbol,
} from "../vb-references-protocol";
import { analyse, detect } from "chardet";
import {
  CodeActionKind,
  CompletionItemKind,
  createConnection,
  DiagnosticSeverity,
  DiagnosticTag,
  DocumentHighlightKind,
  DocumentSymbol,
  ErrorCodes,
  FileChangeType,
  FoldingRange,
  Hover,
  InlineValueVariableLookup,
  InitializeParams,
  InitializeResult,
  InsertTextFormat,
  Location,
  MonikerKind,
  ProposedFeatures,
  ReferenceParams,
  ResponseError,
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
  analyzeVbscriptAsync as analyzeParsedVbscriptAsync,
  buildAspFlowchart,
  buildVbTypeEnvironment,
  buildVirtualDocuments,
  collectVbscriptSymbols,
  collectVbscriptSymbolsAsync,
  collectVbscriptSymbolsFromTextAsync,
  createLocalizer,
  extractAspIncludeRefs,
  extractAspNavigationCandidates,
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
  getVbscriptReferencesForSymbols,
  getVbscriptGraphExternalSymbols,
  getVbscriptSelectionRanges,
  getVbscriptSemanticTokens,
  getVbscriptSignatureHelp,
  getVbscriptTypeDefinition,
  hydrateVbscriptCst,
  needsVbscriptCstHydration,
  parseAspDocument,
  parseAspDocumentAsync,
  parseAspDocumentSkeleton,
  parseAspDocumentSkeletonAsync,
  parseVbscriptDocument,
  parseVbscriptDocumentAsync,
  rangeFromOffsets,
  registerParserMemoryCaches,
  parseVbscriptTypeRef,
  prepareVbscriptCallHierarchy,
  resolveVbscriptCompletionItem,
  shiftAspRangeAfterChange,
  summarizeAspFileAnalysisAsync,
  updateAspParsedDocument,
  updateAspParsedDocumentSkeletonAsync,
  vbscriptReferenceSymbolKey,
  type AspFormattingOptions,
  type AspEmbeddedLanguage,
  type AspEditImpact,
  type AspFlowchartInclude,
  type AspFlowchartLabelMode,
  type AspFlowchartPayload,
  type AspFlowchartSymbolDocument,
  type AspIncrementalChange,
  type AspInclude,
  type AspLegacyEncoding,
  type AspLocale,
  type AspLocaleSetting,
  type AspNavigationCandidate,
  type AspNavigationConfidence,
  type AspNavigationEdge,
  type AspNavigationEdgeKind,
  type AspNavigationGraphPayload,
  type AspNavigationGraphScope,
  type AspNavigationNode,
  type AspNavigationParameterFlow,
  type AspNavigationUrlPart,
  type AspNavigationUrlValue,
  type AspCstNode,
  type AspParsedDocument,
  type AspSettings,
  type AspRegion,
  type FileAnalysisSummary,
  type VirtualDocument,
  type VbCstNode,
  type VbProjectContext,
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
import { getCSSLanguageService } from "vscode-css-languageservice";
import {
  getLanguageService as getHtmlLanguageService,
  TokenType,
} from "vscode-html-languageservice";
import ts from "typescript";
import {
  defaultMemoryMaxCacheBytes,
  MemoryBudgetManager,
  estimateJsonBytes,
  estimateStringBytes,
  registeredMapCache,
  type MemoryPressureResult,
  type RegisteredCache,
} from "../memory-budget";
import {
  buildFlowchartServerCommand,
  buildGraphServerCommand,
  buildNavigationGraphServerCommand,
  cancelProgressTaskServerCommand,
  clearCacheCommand,
  clearCacheServerCommand,
  clearDiskCacheCommand,
  clearDiskCacheServerCommand,
  clearProcessCacheCommand,
  clearProcessCacheServerCommand,
  exportAnalysisExcelServerCommand,
  graphUpdatedNotificationMethod,
  languageServerVersion,
  navigationGraphUpdatedNotificationMethod,
  previewWorkspaceFilesServerCommand,
  reindexWorkspaceCommand,
  reindexWorkspaceServerCommand,
  statusNotificationMethod,
} from "./constants";
import {
  semanticTokenModifierBitsets,
  semanticTokenModifiers,
  semanticTokenTypeIndexes,
  semanticTokenTypes,
} from "./semantic-tokens";

const connection = createConnection(ProposedFeatures.all);
const documentOpenContentVersions = new Map<string, number>();
const documents = new TextDocuments({
  create: TextDocument.create,
  update: (document, changes, version) => {
    const pending = pendingChangeFromContentChanges(version, changes);
    const existing = pendingDocumentChanges.get(document.uri);
    pendingDocumentChanges.set(
      document.uri,
      existing ? mergePendingDocumentChanges(existing, pending) : pending,
    );
    return TextDocument.update(document, changes, version);
  },
});
const htmlService = getHtmlLanguageService();
const cssService = getCSSLanguageService();
const settingsByUri = new Map<string, AspSettings>();
const includePathResolutionCache = new Map<string, IncludePathResolution>();
const pathResolutionCache = new Map<string, PathResolution>();
const includeCycleCache = new Map<string, string[] | null>();
const diskAnalysisSettingsKeyCache = new Map<string, string>();
const includePublicSummaries = new Map<string, IncludePublicSummaryState>();
const workspaceIndex = new Map<string, WorkspaceIndexedDocument>();
const workspaceIncludeGraph = new WorkspaceIncludeGraph();
let workspaceIncludeGraphDirty = true;
let workspaceIncludeGraphRestoreAllowed = true;
interface SemanticTokenResultEntry {
  uri: string;
  data: number[];
  reuseKey?: string;
  version?: number;
  generation?: number;
  vbscriptFingerprint?: string;
  includeRefsKey?: string;
}

const semanticTokenResults = new Map<string, SemanticTokenResultEntry>();
const latestSemanticTokenResultByUri = new Map<string, string>();
const retainedSemanticTokenResults = new Map<string, SemanticTokenResultEntry>();
const latestRetainedSemanticTokenResultByUri = new Map<string, string>();
const maxRetainedSemanticTokenResults = 128;
interface PendingVbProjectSummaryGraph {
  allowReadMissing: boolean;
  promise: Promise<VbProjectSummaryGraph>;
}

const pendingVbProjectSummaryGraphs = new Map<string, PendingVbProjectSummaryGraph>();
const pendingSemanticJavascriptTokenBuilds = new Map<string, Promise<void>>();
const interactiveVbProjectContextSnapshots = new Map<string, InteractiveVbProjectContextSnapshot>();
const pendingInteractiveVbProjectContextRefreshes = new Map<string, Promise<void>>();
const workspaceIndexedDiagnosticsCache = new Map<string, WorkspaceIndexedDiagnosticsCacheEntry>();
let workspaceDiagnosticsReportCache: WorkspaceDiagnosticsReportCacheEntry | undefined;
const vbCanonicalContextSymbolsCache = new Map<string, VbCanonicalContextSymbolsCacheEntry>();
const pendingJsDiagnosticsPrewarms = new Set<string>();
const regionIndexes = new WeakMap<AspParsedDocument, RegionIndex>();
const defaultMaxIndexFiles = 5000;
const defaultScanChunkSize = 200;
const defaultVbProjectMaxDocuments = 256;
const defaultVbProjectMaxTextLength = 16 * 1024 * 1024;
const defaultGraphMaxDocuments = defaultMaxIndexFiles;
const defaultGraphMaxTextLength = 256 * 1024 * 1024;
const defaultGraphMaxNodes = 5_000;
const graphCanonicalizedReplayMaxBytes = 256 * 1024 * 1024;
const defaultExcelMaxDocuments = 8192;
const defaultExcelMaxTextLength = 512 * 1024 * 1024;
const defaultExcelIncludeTreeMaxDocuments = 1024;
const defaultExcelIncludeTreeMaxTextLength = 64 * 1024 * 1024;
const defaultWorkspaceIncludes = ["**/*.{asp,asa,inc,vbs}"];
const defaultDiagnosticsDebounceMs = 250;
const defaultNetworkStatCacheTtlMs = 30_000;
const defaultNetworkNegativeStatCacheTtlMs = 5_000;
const defaultNetworkReaddirCacheTtlMs = 30_000;
const defaultNetworkIncludeReadConcurrency = 16;
const completionTriggerKindTriggerCharacter = 2;
const projectUpdateDelayMs = 250;
const openFileProjectMaintenanceDelayMs = 2_500;
const semanticTokensLargeSourceThreshold = internalTestThreshold(
  "ASP_LSP_TEST_SEMANTIC_TOKENS_LARGE_SOURCE_THRESHOLD",
  1024 * 1024,
);
const semanticTokensLargeJavascriptThreshold = internalTestThreshold(
  "ASP_LSP_TEST_SEMANTIC_TOKENS_LARGE_JAVASCRIPT_THRESHOLD",
  50 * 1024,
);
const semanticTokensDeferredWorkDelayMs = 25;
const graphBackgroundBuildMinDocuments = internalTestThreshold(
  "ASP_LSP_TEST_GRAPH_BACKGROUND_MIN_DOCUMENTS",
  1000,
);
const graphBackgroundBuildDebounceMs = internalTestThreshold(
  "ASP_LSP_TEST_GRAPH_BACKGROUND_DEBOUNCE_MS",
  150,
);
const graphPartialMaxDocuments = internalTestThreshold(
  "ASP_LSP_TEST_GRAPH_PARTIAL_MAX_DOCUMENTS",
  300,
);
const defaultDebugLogFileName = "asp-lsp-debug.log";
const debugLogFileMaxBytes = internalTestThreshold(
  "ASP_LSP_TEST_DEBUG_LOG_MAX_BYTES",
  10 * 1024 * 1024,
);
const debugLogFileMaxBackups = internalTestThreshold("ASP_LSP_TEST_DEBUG_LOG_MAX_BACKUPS", 5);
const debugLogFileWriter = new DebugLogFileWriter((message) => connection.console.warn(message), {
  maxBytes: debugLogFileMaxBytes,
  maxBackups: debugLogFileMaxBackups,
});
let globalSettings: AspSettings = { defaultLanguage: "VBScript", checkJs: false };
let workspaceRoots: string[] = [];
let workspaceRootsIdentityCache: { source: string[]; roots: string[]; key: string } | undefined;
let clientLocale = "en";
let workspaceIndexDirty = true;
let workspaceIndexTruncated = false;
let workspaceIndexRestoreAllowed = true;
let workspaceIndexRevalidationSerial = 0;
let vbReferencesWorkerPool: VbReferencesWorkerPool | undefined;
let bulkWorkerPool: BulkWorkerPool | undefined;
let vbReferencesWorkerRequestId = 0;
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
const serverTextFingerprintCacheMaxEntries = 256;
const serverTextFingerprintCache = new Map<string, string>();
const diskAnalysisSettingsKeyCacheMaxEntries = 4096;
const workspaceIndexedDiagnosticsCacheMaxEntries = 4096;
const vbCanonicalContextSymbolsCacheMaxEntries = 128;
const maxJsSnapshotChangeRangeScanLength = 32 * 1024;
const memoryBudgetManager = new MemoryBudgetManager();
let lastMemoryBudgetCheckAt = 0;
let diskAnalysisCache = createDiskAnalysisCache(globalSettings);
const sourceManifest = new Map<string, DiskAnalysisSourceMetadata>();
const diagnosticKeyCache = new WeakMap<Diagnostic, string>();
const settingsParseIdentityCache = new WeakMap<AspSettings, string>();
const settingsIncludeResolutionIdentityCache = new WeakMap<AspSettings, string>();
const settingsDiagnosticsIdentityCache = new WeakMap<AspSettings, string>();
const settingsCacheIdentityCache = new WeakMap<AspSettings, string>();
const settingsJsProjectIdentityCache = new WeakMap<
  AspSettings,
  { rootsKey: string; value: string }
>();
const settingsWorkspaceIndexIdentityCache = new WeakMap<
  AspSettings,
  { rootsKey: string; value: string }
>();
const includeResolutionIdentityCache = new WeakMap<
  AspSettings,
  { generation: number; value: string }
>();
const jsProjectIdentityCache = new WeakMap<AspSettings, { generation: number; value: string }>();
const symbolsByLowerNameCache = new WeakMap<VbSymbol[], Map<string, VbSymbol[]>>();
const typeEnvironmentTypesByLowerNameCache = new WeakMap<VbType[], Map<string, VbType>>();
let lastForegroundActivityAt = 0;
let projectUpdateTimer: ReturnType<typeof setTimeout> | undefined;
let openFileProjectMaintenanceTimer: ReturnType<typeof setTimeout> | undefined;
let pendingProjectUpdateReason: string | undefined;
let pendingOpenFileMaintenanceReason: string | undefined;
let loadingStatusDepth = 0;
let analyzingStatusDepth = 0;
let currentStatusKind: AspLspStatusKind = "idle";
let progressTaskSequence = 0;
let lastPublishedStatusPayload = "";
const progressTasks = new Map<string, AspLspProgressTask>();
const activeGraphBackgroundBuilds = new Map<string, AspGraphBackgroundBuild>();
let jsDiagnosticsWorkerPool: JsDiagnosticsWorkerPool | undefined;
let jsDiagnosticsWorkerRequestId = 0;
const tsUnusedDiagnosticCodes = new Set([6133, 6138, 6192, 6196, 6198]);
const hiddenJavaScriptGlobalCompletions = new Set(["__dirname", "__filename"]);
const browserJavaScriptLibs = ["lib.esnext.d.ts", "lib.dom.d.ts", "lib.dom.iterable.d.ts"];

type AspLspStatusKind = "idle" | "loading" | "analyzing";
type ActiveAspLspStatusKind = Exclude<AspLspStatusKind, "idle">;

type AspLspProgressTaskState = "running" | "cancelling";

interface AspLspProgressTaskSnapshot {
  id: string;
  kind: ActiveAspLspStatusKind;
  label: string;
  detail?: string;
  current?: number;
  total?: number;
  activeItems?: string[];
  cancellable?: boolean;
  state: AspLspProgressTaskState;
  startedAt: number;
  updatedAt: number;
}

interface AspLspProgressTask extends AspLspProgressTaskSnapshot {
  cancelRequested: boolean;
}

interface AspLspProgressTaskHandle {
  id: string;
  isCancellationRequested(): boolean;
  update(update: AspLspProgressTaskUpdate): void;
  step(detail?: string): void;
  end(): void;
}

type AspLspProgressTaskUpdate = Partial<
  Pick<AspLspProgressTaskSnapshot, "label" | "detail" | "current" | "total" | "activeItems">
>;

interface AspLspProgressTaskOptions {
  detail?: string;
  current?: number;
  total?: number;
  cancellable?: boolean;
  activeItems?: string[];
}

interface AspGraphCommandRequestIdentity {
  scope: AspGraphScope;
  uri?: string;
  settings: AspSettings;
  key: string;
  signature: string;
  correlationId: string;
}

interface AspGraphBackgroundBuild {
  key: string;
  signature: string;
  scope: AspGraphScope;
  uri?: string;
  argument: unknown;
  task: AspLspProgressTaskHandle;
  correlations: Set<string>;
  timer?: ReturnType<typeof setTimeout>;
  started: boolean;
}

interface PathResolution {
  fileName: string;
  exists: boolean;
  pathCaseMatches: boolean;
  actualPath?: string;
}

interface IncludePathResolution extends PathResolution {
  actualIncludePath?: string;
}

interface RegionIndex {
  byStart: AspRegion[];
}

interface EmbeddedSemanticTokenOptions {
  settings: AspSettings;
  jsVirtuals: VirtualDocument[];
  deferLargeJavascript: boolean;
  javascriptCacheKey: string;
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

interface IncludeStateRefreshResult {
  includeRefsChangedFiles: Set<string>;
  publicChangedFiles: Set<string>;
}

interface TsDiagnosticLike {
  code: number;
  category: ts.DiagnosticCategory;
  messageText: string | ts.DiagnosticMessageChain;
  start?: number;
  length?: number;
  reportsUnnecessary?: unknown;
}

type DiagnosticLayerKey = "fast" | "include" | "syntax" | "projectFast" | "project" | "final";

interface StagedDiagnosticsState {
  generation: number;
  uri: string;
  version: number;
  documentGeneration: number;
  diagnosticsIdentity: string;
  includeResolutionGeneration: number;
  jsProjectGeneration: number;
  workspaceGeneration: number;
  startedAt: bigint;
  preservePreviousDiagnosticsUntilFinal: boolean;
  asyncLayersStarted: boolean;
  layers: Partial<Record<DiagnosticLayerKey, Diagnostic[]>>;
}

interface PublishedDiagnosticsState {
  version?: number;
  diagnostics: Diagnostic[];
}

type AnalysisExecutionMode = "foreground" | "workspace";

interface OffsetEdit {
  start: number;
  end: number;
  newText: string;
}

interface HtmlProtectedSpan {
  block: boolean;
  end: number;
  placeholder: string;
  start: number;
  text: string;
}

interface WorkspaceIndexedDocument {
  uri: string;
  fileName: string;
  mtimeMs: number;
  size: number;
}

type JavaScriptMode = "definition" | "declaration" | "typeDefinition" | "implementation";

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

interface VbProjectContextBuildOptions {
  allowReadMissing: boolean;
}

interface VbReferenceCodeLensData {
  kind: "vbscript-reference";
  uri: string;
  name: string;
  symbolKind: VbSymbolKind;
  memberOf?: string;
  scopeName?: string;
  propertyAccessor?: VbSymbol["propertyAccessor"];
  line: number;
  character: number;
  endLine?: number;
  endCharacter?: number;
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

interface InteractiveVbProjectContextSnapshot {
  familyKey: string;
  key: string;
  rootUri: string;
  context: VbProjectContext;
  lastUsed: number;
}

interface WorkspaceIndexedDiagnosticsCacheEntry {
  key: string;
  items: Diagnostic[];
  lastUsed: number;
}

interface WorkspaceDiagnosticsReportItem {
  kind: "full";
  uri: string;
  version: number | null;
  items: Diagnostic[];
}

interface WorkspaceDiagnosticsReport {
  items: WorkspaceDiagnosticsReportItem[];
}

interface WorkspaceDiagnosticsReportCacheEntry {
  key: string;
  report: WorkspaceDiagnosticsReport;
  lastUsed: number;
}

interface VbCanonicalContextSymbolsCacheEntry {
  symbols: VbSymbol[];
  lastUsed: number;
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
      externalRefUsageKeys: summaryVbReferenceUsages(summary).map((usage) => usage.key),
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
      externalRefUsageKeys: summaryVbReferenceUsages(entry.summary).map((usage) => usage.key),
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
      !sameFileIdentityUri(entry.uri, cached.source.uri) ||
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
    // CompletionItems carry text edit ranges, so replay them only for the exact
    // document snapshot and position that produced them.
    if (
      cached.source.version !== entry.documentVersion ||
      offset !== entry.offset ||
      prefix !== entry.prefix
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
    const fileKeys = new Set(
      [...uris]
        .filter((uri) => uri.startsWith("file://"))
        .map((uri) => fileIdentityKeyFromUri(uri)),
    );
    let removed = 0;
    for (const [key, entry] of this.entries) {
      const entryKey = entry.uri.startsWith("file://") ? fileIdentityKeyFromUri(entry.uri) : "";
      if (!uris.has(entry.uri) && !fileKeys.has(entryKey)) {
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
      uri: fileIdentityKeyFromUri(cached.source.uri),
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

interface WorkspaceGlobPattern {
  pattern: string;
  regex: RegExp;
  matchBasename: boolean;
}

interface WorkspaceGitIgnoreRule extends WorkspaceGlobPattern {
  directoryOnly: boolean;
  negated: boolean;
}

interface WorkspaceScanFilter {
  root: string;
  includes: WorkspaceGlobPattern[];
  excludes: WorkspaceGlobPattern[];
  gitIgnoreRules: WorkspaceGitIgnoreRule[];
}

interface WorkspaceFilePreviewFile {
  uri: string;
  fileName: string;
  matchesFilter: boolean;
  relativePath: string;
  size: number;
  mtimeMs: number;
}

interface WorkspaceFilePreviewRoot {
  uri: string;
  fileName: string;
  name: string;
  files: WorkspaceFilePreviewFile[];
}

interface WorkspaceFilePreviewPayload {
  includeGlobs: string[];
  excludeGlobs: string[];
  globStats: WorkspaceFilePreviewGlobStats;
  respectGitIgnore: boolean;
  roots: WorkspaceFilePreviewRoot[];
  showUnmatched: boolean;
  stats: {
    files: number;
    totalBytes: number;
  };
  truncated?: {
    reason: string;
  };
}

interface WorkspaceFilePreviewGlobStats {
  include: WorkspaceFilePreviewGlobStat[];
  exclude: WorkspaceFilePreviewGlobStat[];
}

interface WorkspaceFilePreviewGlobStat {
  glob: string;
  files: number;
}

function aspFileOperationFilter() {
  return {
    scheme: "file",
    pattern: {
      glob: "**/*.{asp,asa,inc,vbs}",
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
    const fileKey = fileIdentityKeyFromFileName(normalized);
    const identity = await includeDocumentSourceIdentityAsync(normalized, settings);
    if (!identity) {
      return undefined;
    }
    const { key, source, text, uri, diskBacked } = identity;
    const existing = this.cache.get(fileKey);
    if (existing?.key === key) {
      return existing;
    }
    const inFlightKey = `${fileKey}:${key}`;
    const pending = this.inFlight.get(inFlightKey);
    if (pending) {
      return pending;
    }
    const generation = this.generation(fileKey);
    let promise: Promise<IncludeDocumentCacheEntry | undefined> | undefined;
    promise = (async () => {
      try {
        if (diskBacked) {
          const cachedParsed = await diskAnalysisCache
            .readParsedDocument({ source, settingsKey: includeSummarySettingsKey(settings) })
            .catch((error) => {
              logDiskAnalysisCacheError("diskParsed.read", error);
              return undefined;
            });
          if (cachedParsed) {
            const entry = includeDocumentCacheEntryFromDisk(normalized, key, cachedParsed);
            if (this.generation(fileKey) === generation) {
              this.cache.set(fileKey, entry);
              this.summaryCache.set(fileKey, entry);
              this.includeRefsCache.set(fileKey, includeRefsCacheEntryFromSummary(entry, settings));
              rememberIncludePublicSummary(entry, settings);
              rememberSourceMetadata(entry.source);
              logDebugSummary(settings, `[asp-lsp] diskParsed.hit: ${entry.uri}`);
            }
            return entry;
          }
          logDebugSummary(settings, `[asp-lsp] diskParsed.miss: ${pathToFileUri(normalized)}`);
        }
        const nextText = text ?? (await readTextFileAsync(normalized, settings.legacyEncoding));
        const contentSource = sourceWithContentHash(source, nextText);
        if (diskBacked && source.contentHash === undefined) {
          const cachedParsed = await diskAnalysisCache
            .readParsedDocument({
              source: contentSource,
              settingsKey: includeSummarySettingsKey(settings),
            })
            .catch((error) => {
              logDiskAnalysisCacheError("diskParsed.readHash", error);
              return undefined;
            });
          if (cachedParsed) {
            const entry = includeDocumentCacheEntryFromDisk(normalized, key, cachedParsed);
            if (this.generation(fileKey) === generation) {
              this.cache.set(fileKey, entry);
              this.summaryCache.set(fileKey, entry);
              this.includeRefsCache.set(fileKey, includeRefsCacheEntryFromSummary(entry, settings));
              rememberIncludePublicSummary(entry, settings);
              rememberSourceMetadata(entry.source);
              logDebugSummary(settings, `[asp-lsp] diskParsed.hashHit: ${entry.uri}`);
            }
            return entry;
          }
        }
        const entry = await createIncludeDocumentCacheEntryAsync(
          normalized,
          uri,
          nextText,
          settings,
          key,
          contentSource,
        );
        if (this.generation(fileKey) === generation) {
          this.cache.set(fileKey, entry);
          this.summaryCache.set(fileKey, entry);
          this.includeRefsCache.set(fileKey, includeRefsCacheEntryFromSummary(entry, settings));
          rememberIncludePublicSummary(entry, settings);
          if (diskBacked) {
            void writeIncludeDocumentDiskEntries(entry, settings);
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
    const fileKey = fileIdentityKeyFromFileName(normalized);
    const identity = await includeDocumentSourceIdentityAsync(normalized, settings);
    if (!identity) {
      return undefined;
    }
    const { key, source, text, uri, diskBacked } = identity;
    const existing = this.summaryCache.get(fileKey);
    if (existing?.key === key) {
      return existing;
    }
    const existingDocument = this.cache.get(fileKey);
    if (existingDocument?.key === key) {
      this.summaryCache.set(fileKey, existingDocument);
      return existingDocument;
    }
    const baseInFlightKey = `${fileKey}:${key}`;
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
    const generation = this.generation(fileKey);
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
            if (this.generation(fileKey) === generation) {
              this.summaryCache.set(fileKey, entry);
              this.includeRefsCache.set(fileKey, includeRefsCacheEntryFromSummary(entry, settings));
              rememberIncludePublicSummary(entry, settings);
              rememberSourceMetadata(entry.source);
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
        const contentSource = sourceWithContentHash(source, nextText);
        if (diskBacked && source.contentHash === undefined) {
          const cachedSummary = await diskAnalysisCache
            .readSummary({
              source: contentSource,
              settingsKey: includeSummarySettingsKey(settings),
            })
            .catch((error) => {
              logDiskAnalysisCacheError("diskSummary.readHash", error);
              return undefined;
            });
          if (cachedSummary) {
            const entry = includeSummaryCacheEntryFromDisk(normalized, key, cachedSummary);
            if (this.generation(fileKey) === generation) {
              this.summaryCache.set(fileKey, entry);
              this.includeRefsCache.set(fileKey, includeRefsCacheEntryFromSummary(entry, settings));
              rememberIncludePublicSummary(entry, settings);
              rememberSourceMetadata(entry.source);
              logDebugSummary(settings, `[asp-lsp] diskSummary.hashHit: ${entry.uri}`);
            }
            return entry;
          }
        }
        const entry = await createIncludeDocumentCacheEntryAsync(
          normalized,
          uri,
          nextText,
          settings,
          key,
          contentSource,
        );
        if (this.generation(fileKey) === generation) {
          this.cache.set(fileKey, entry);
          this.summaryCache.set(fileKey, entry);
          this.includeRefsCache.set(fileKey, includeRefsCacheEntryFromSummary(entry, settings));
          rememberIncludePublicSummary(entry, settings);
          if (diskBacked) {
            await writeIncludeDocumentDiskEntries(entry, settings);
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
    const fileKey = fileIdentityKeyFromFileName(normalized);
    const identity = await includeDocumentSourceIdentityAsync(normalized, settings);
    if (!identity) {
      return undefined;
    }
    const { source, text, uri, diskBacked } = identity;
    const key = includeRefsCacheKey(normalized, source, settings);
    const existing = this.includeRefsCache.get(fileKey);
    if (existing?.key === key) {
      return existing;
    }
    const existingSummary = this.summaryCache.get(fileKey);
    if (existingSummary && sameDiskAnalysisSource(existingSummary.source, source)) {
      const entry = includeRefsCacheEntryFromSummary(existingSummary, settings);
      this.includeRefsCache.set(fileKey, entry);
      return entry;
    }
    const readAllowed = options.allowRead !== false;
    const readInFlightKey = `${fileKey}:${key}:read`;
    const noReadInFlightKey = `${fileKey}:${key}:no-read`;
    const inFlightKey = readAllowed ? readInFlightKey : noReadInFlightKey;
    const pending = readAllowed
      ? this.includeRefsInFlight.get(readInFlightKey)
      : (this.includeRefsInFlight.get(readInFlightKey) ??
        this.includeRefsInFlight.get(noReadInFlightKey));
    if (pending) {
      return pending;
    }
    const generation = this.generation(fileKey);
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
            if (this.generation(fileKey) === generation) {
              this.includeRefsCache.set(fileKey, entry);
              rememberSourceMetadata(entry.source);
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
        const contentSource = sourceWithContentHash(source, nextText);
        if (diskBacked && source.contentHash === undefined) {
          const cachedRefs = await diskAnalysisCache
            .readIncludeRefs({
              source: contentSource,
              settingsKey: includeRefsSettingsKey(settings),
            })
            .catch((error) => {
              logDiskAnalysisCacheError("diskIncludeRefs.readHash", error);
              return undefined;
            });
          if (cachedRefs) {
            const entry = includeRefsCacheEntryFromDisk(normalized, key, cachedRefs);
            if (this.generation(fileKey) === generation) {
              this.includeRefsCache.set(fileKey, entry);
              rememberSourceMetadata(entry.source);
              logDebugSummary(settings, `[asp-lsp] diskIncludeRefs.hashHit: ${entry.uri}`);
            }
            return entry;
          }
        }
        const entry = createIncludeRefsCacheEntry(normalized, uri, nextText, key, contentSource);
        if (this.generation(fileKey) === generation) {
          this.includeRefsCache.set(fileKey, entry);
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
    return this.includeRefsCache.get(fileIdentityKeyFromFileName(fileName));
  }

  cachedPublicSummary(fileName: string): IncludePublicSummaryState | undefined {
    return includePublicSummaries.get(fileIdentityKeyFromFileName(fileName));
  }

  registerMemoryCaches(register: (cache: RegisteredCache) => void): void {
    register(
      registeredMapCache("include.documents", this.cache, {
        priority: 30,
        estimateEntryBytes: (_key, entry) => estimateIncludeDocumentCacheEntryBytes(entry),
      }),
    );
    register(
      registeredMapCache("include.summaries", this.summaryCache, {
        priority: 35,
        estimateEntryBytes: (_key, entry) => estimateIncludeSummaryCacheEntryBytes(entry),
      }),
    );
    register(
      registeredMapCache("include.refs", this.includeRefsCache, {
        priority: 20,
        estimateEntryBytes: (_key, entry) => estimateIncludeRefsCacheEntryBytes(entry),
      }),
    );
  }

  invalidateFiles(fileNames: Iterable<string>): void {
    for (const fileName of fileNames) {
      const normalized = normalizeFileName(fileName);
      const fileKey = fileIdentityKeyFromFileName(normalized);
      this.generations.set(fileKey, this.generation(fileKey) + 1);
      this.cache.delete(fileKey);
      this.summaryCache.delete(fileKey);
      this.includeRefsCache.delete(fileKey);
      for (const key of this.inFlight.keys()) {
        if (key.startsWith(`${fileKey}:`)) {
          this.inFlight.delete(key);
        }
      }
      for (const key of this.summaryInFlight.keys()) {
        if (key.startsWith(`${fileKey}:`)) {
          this.summaryInFlight.delete(key);
        }
      }
      for (const key of this.includeRefsInFlight.keys()) {
        if (key.startsWith(`${fileKey}:`)) {
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

const diagnosticsTimers = new Map<string, ReturnType<typeof setTimeout>>();
const stagedDiagnosticsByUri = new Map<string, StagedDiagnosticsState>();
const publishedDiagnosticsByUri = new Map<string, PublishedDiagnosticsState>();
const pendingDocumentChanges = new Map<string, PendingDocumentChange>();
const includeDocumentLoader = new IncludeDocumentLoader();
const pendingIncludeSummaryRefreshes = new Map<string, Promise<void>>();
const aspProjectBuilderState = new AspProjectBuilderState();
const completionSessionCache = new CompletionSessionCache();
let vbDiagnosticsWorkerPool: VbDiagnosticsWorkerPool | undefined;
let vbDiagnosticsWorkerRequestId = 0;
let stagedDiagnosticsGeneration = 0;

registerMemoryBudgetCaches();

function registerMemoryBudgetCaches(): void {
  for (const managedCache of memoryManagedAnalysisCaches) {
    memoryBudgetManager.register(managedCache);
  }
  registerParserMemoryCaches((managedCache) => memoryBudgetManager.register(managedCache));
  includeDocumentLoader.registerMemoryCaches((managedCache) =>
    memoryBudgetManager.register(managedCache),
  );
  memoryBudgetManager.register(
    registeredMapCache("server.textFingerprint", serverTextFingerprintCache, {
      priority: 5,
      estimateEntryBytes: (key, value) =>
        estimateStringBytes(key) + estimateStringBytes(value) + 64,
    }),
  );
  memoryBudgetManager.register(
    registeredMapCache("server.includePathResolution", includePathResolutionCache, {
      priority: 20,
      estimateEntryBytes: (key, value) => estimateStringBytes(key) + estimateJsonBytes(value, 512),
    }),
  );
  memoryBudgetManager.register(
    registeredMapCache("server.pathResolution", pathResolutionCache, {
      priority: 20,
      estimateEntryBytes: (key, value) => estimateStringBytes(key) + estimateJsonBytes(value, 512),
    }),
  );
  memoryBudgetManager.register(
    registeredMapCache("server.includeCycles", includeCycleCache, {
      priority: 20,
      estimateEntryBytes: (key, value) => estimateStringBytes(key) + estimateJsonBytes(value, 256),
    }),
  );
  memoryBudgetManager.register(
    registeredMapCache("server.diskAnalysisSettingsKey", diskAnalysisSettingsKeyCache, {
      priority: 20,
      estimateEntryBytes: (key, value) =>
        estimateStringBytes(key) + estimateStringBytes(value) + 64,
    }),
  );
  memoryBudgetManager.register(
    registeredMapCache("server.workspaceIndexedDiagnostics", workspaceIndexedDiagnosticsCache, {
      priority: 20,
      estimateEntryBytes: (key, value) =>
        estimateStringBytes(key) +
        estimateStringBytes(value.key) +
        estimateJsonBytes(value.items, 2048) +
        64,
    }),
  );
  memoryBudgetManager.register(
    registeredMapCache("server.semanticTokens", semanticTokenResults, {
      priority: 25,
      estimateEntryBytes: (key, value) =>
        estimateStringBytes(key) + estimateStringBytes(value.uri) + value.data.length * 8 + 64,
    }),
  );
  memoryBudgetManager.register(
    registeredMapCache("server.latestSemanticTokens", latestSemanticTokenResultByUri, {
      priority: 25,
      estimateEntryBytes: (key, value) => estimateStringBytes(key) + estimateStringBytes(value),
    }),
  );
  memoryBudgetManager.register(
    registeredMapCache("graph.fileIndex", graphFileIndexCache, {
      priority: 30,
      estimateEntryBytes: (_key, value) => estimateGraphFileIndexBytes(value),
    }),
  );
  memoryBudgetManager.register(
    registeredMapCache("vb.projectContext", vbProjectContextCache, {
      priority: 35,
      estimateEntryBytes: (key, value) => estimateStringBytes(key) + estimateJsonBytes(value, 4096),
    }),
  );
  memoryBudgetManager.register(
    registeredMapCache("vb.canonicalContextSymbols", vbCanonicalContextSymbolsCache, {
      priority: 25,
      estimateEntryBytes: (key, value) =>
        estimateStringBytes(key) + estimateJsonBytes(value.symbols, 4096) + 64,
    }),
  );
  memoryBudgetManager.register(
    registeredMapCache("vb.references.workerCompleted", workspaceVbReferenceWorkerCompleted, {
      priority: 20,
      estimateEntryBytes: (key, value) => estimateStringBytes(key) + estimateJsonBytes(value, 4096),
    }),
  );
  memoryBudgetManager.register(
    registeredMapCache("vb.references.requestCompleted", workspaceVbReferenceRequestCompleted, {
      priority: 20,
      estimateEntryBytes: (key, value) =>
        estimateStringBytes(key) + estimateWorkspaceReferenceMapBytes(value.referencesByTarget),
    }),
  );
  memoryBudgetManager.register(
    registeredMapCache("vb.references.batchCompleted", workspaceVbReferenceBatchCompleted, {
      priority: 20,
      estimateEntryBytes: (key, value) =>
        estimateStringBytes(key) + estimateWorkspaceReferenceMapBytes(value.referencesByTarget),
    }),
  );
  memoryBudgetManager.register(
    registeredMapCache("vb.references.reachability", workspaceVbReferenceReachabilityCache, {
      priority: 20,
      estimateEntryBytes: (key, value) => estimateStringBytes(key) + estimateJsonBytes(value, 1024),
    }),
  );
  memoryBudgetManager.register(documentAnalysisMemoryCache());
}

function checkMemoryPressure(
  settings: AspSettings,
  reason: string,
  options: { force?: boolean } = {},
): void {
  const now = Date.now();
  if (!options.force && now - lastMemoryBudgetCheckAt < 1_000) {
    return;
  }
  lastMemoryBudgetCheckAt = now;
  const result = memoryBudgetManager.checkPressure({
    reason,
    maxCacheBytes: settings.memory?.maxCacheBytes,
  });
  if (
    settings.memory?.debugTelemetry === true ||
    result.evictedBytes > 0 ||
    result.pressure !== "none"
  ) {
    logMemoryBudgetTelemetry(settings, result);
  }
}

function logMemoryBudgetTelemetry(settings: AspSettings, result: MemoryPressureResult): void {
  const message = `[asp-lsp] memory.snapshot: reason=${result.reason}, pressure=${result.pressure}, total=${result.after.totalEstimatedBytes}/${result.after.maxCacheBytes}, heap=${result.after.heapUsed}/${result.after.heapSizeLimit}, evicted=${result.evictedBytes}`;
  logDebugFile(settings, "trace", "memory.snapshot", message, {
    reason: result.reason,
    pressure: result.pressure,
    targetBytes: result.targetBytes,
    requestedBytes: result.requestedBytes,
    evictedBytes: result.evictedBytes,
    before: result.before,
    after: result.after,
    evictions: result.evictions,
  });
}

function documentAnalysisMemoryCache(): RegisteredCache {
  return {
    name: "documents.analysis",
    priority: 40,
    estimateBytes: () => {
      let total = 0;
      for (const cached of cache.values()) {
        if (openDocumentForUri(cached.source.uri)) {
          continue;
        }
        total += estimateCachedDocumentEvictableBytes(cached);
      }
      return total;
    },
    evict: (targetBytes) => {
      let freed = 0;
      const candidates = [...cache.values()]
        .filter((cached) => !openDocumentForUri(cached.source.uri))
        .sort((left, right) => left.lastAccess - right.lastAccess);
      for (const cached of candidates) {
        if (freed >= targetBytes) {
          break;
        }
        const bytes = estimateCachedDocumentEvictableBytes(cached);
        if (bytes <= 0) {
          continue;
        }
        const demoted = documentStore.demote(cached, {
          settings: cachedSettings(cached.source.uri),
          parseSkeleton: (uri, text, settings) =>
            isStandaloneVbscriptSource(uri, cached.source.languageId)
              ? parseVbscriptDocument(uri, text)
              : parseAspDocumentSkeleton(uri, text, settings),
        });
        if (!demoted) {
          continue;
        }
        freed += bytes;
      }
      return freed;
    },
    entryCount: () => cache.size,
  };
}

function estimateCachedDocumentEvictableBytes(cached: CachedDocument): number {
  return (
    estimateVirtualDocumentsBytes(cached.virtuals) +
    estimateJsonBytes(cached.analysis, 2048) +
    estimateJsonBytes(cached.cssContext, 1024) +
    (cached.parseDepth === "full"
      ? Math.floor(estimateParsedDocumentBytes(cached.parsed) * 0.6)
      : 0)
  );
}

function estimateVirtualDocumentsBytes(
  virtuals: ReadonlyMap<AspEmbeddedLanguage, VirtualDocument>,
): number {
  let total = 0;
  for (const [language, virtual] of virtuals) {
    total +=
      estimateStringBytes(language) +
      estimateStringBytes(virtual.uri) +
      estimateStringBytes(virtual.languageId) +
      estimateStringBytes(virtual.text) +
      virtual.sourceMap.segments.length * 48 +
      128;
  }
  return total;
}

function estimateIncludeDocumentCacheEntryBytes(entry: IncludeDocumentCacheEntry): number {
  return estimateIncludeSummaryCacheEntryBytes(entry) + estimateParsedDocumentBytes(entry.parsed);
}

function estimateIncludeSummaryCacheEntryBytes(entry: IncludeSummaryCacheEntry): number {
  return (
    estimateStringBytes(entry.key) +
    estimateStringBytes(entry.fileName) +
    estimateStringBytes(entry.uri) +
    estimateJsonBytes(entry.source, 512) +
    estimateJsonBytes(entry.summary, 2048) +
    estimateStringBytes(entry.publicFingerprint) +
    estimateJsonBytes(entry.publicSignature, 1024) +
    (entry.parsed ? estimateParsedDocumentBytes(entry.parsed) : 0)
  );
}

function estimateIncludeRefsCacheEntryBytes(entry: IncludeRefsCacheEntry): number {
  return (
    estimateStringBytes(entry.key) +
    estimateStringBytes(entry.fileName) +
    estimateStringBytes(entry.uri) +
    estimateJsonBytes(entry.source, 512) +
    estimateJsonBytes(entry.includeRefs, 1024) +
    estimateStringBytes(entry.fingerprint)
  );
}

function estimateParsedDocumentBytes(parsed: AspParsedDocument): number {
  return (
    estimateStringBytes(parsed.uri) +
    estimateStringBytes(parsed.text) +
    parsed.regions.length * 256 +
    parsed.includes.length * 192 +
    parsed.directives.length * 192 +
    parsed.serverObjects.length * 192 +
    estimateJsonBytes(parsed.cst, 4096)
  );
}

function estimateGraphFileIndexBytes(index: GraphFileIndex): number {
  let typeHintBytes = 128;
  for (const [key, value] of index.typeHints) {
    typeHintBytes += estimateStringBytes(key) + estimateJsonBytes(value, 256);
  }
  return (
    estimateStringBytes(index.key) +
    estimateStringBytes(index.uri) +
    estimateStringBytes(index.fileName) +
    estimateJsonBytes(index.source, 512) +
    estimateJsonBytes(index.includeRefs, 1024) +
    estimateJsonBytes(index.vbSymbolIndex, 4096) +
    estimateStringBytes(index.fingerprint) +
    typeHintBytes
  );
}

function estimateWorkspaceReferenceMapBytes(
  referencesByTarget: ReadonlyMap<string, VbReference[]>,
): number {
  let total = 128;
  for (const [key, references] of referencesByTarget) {
    total += estimateStringBytes(key) + estimateJsonBytes(references, 2048);
  }
  return total;
}

connection.onInitialize((params: InitializeParams): InitializeResult => {
  clientLocale = typeof params.locale === "string" ? params.locale : "en";
  semanticTokensRefreshSupported =
    params.capabilities.workspace?.semanticTokens?.refreshSupport === true;
  inlayHintRefreshSupported = params.capabilities.workspace?.inlayHint?.refreshSupport === true;
  globalSettings = normalizeSettings(globalSettings);
  workspaceRoots = [
    ...(params.workspaceFolders?.map((folder) => uriToFileName(folder.uri)) ?? []),
    ...(params.rootUri ? [uriToFileName(params.rootUri)] : []),
    ...(!params.workspaceFolders?.length && !params.rootUri && params.rootPath
      ? [params.rootPath]
      : []),
  ].filter((root, index, roots) => root.length > 0 && roots.indexOf(root) === index);
  configureFsGateway(globalSettings);
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
        // Space is used to convert IME candidates, and LSP completion requests cannot
        // tell whether the editor is composing text.
        triggerCharacters: ["<", ".", '"', "'", ":", "#", "(", ";"],
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
          buildFlowchartServerCommand,
          buildNavigationGraphServerCommand,
          exportAnalysisExcelServerCommand,
          previewWorkspaceFilesServerCommand,
          cancelProgressTaskServerCommand,
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
  deleteCachedDocumentsForUri(event.document.uri);
  const settings = cachedSettings(event.document.uri);
  logDebugTrace(settings, "document.open", "[asp-lsp] document.open", {
    uri: event.document.uri,
    version: event.document.version,
    languageId: event.document.languageId,
    lineCount: event.document.lineCount,
    textLength: event.document.getText().length,
  });
  invalidateRootlessWorkspaceIndex("document.open");
  scheduleOpenFileProjectMaintenance("document.open");
  validateOpenedDocument(event.document);
});
documents.onDidChangeContent((event) => {
  noteForegroundActivity();
  cancelAspGraphBackgroundBuilds("document.change");
  const openedVersion = documentOpenContentVersions.get(event.document.uri);
  if (openedVersion === event.document.version) {
    documentOpenContentVersions.delete(event.document.uri);
    return;
  }
  documentOpenContentVersions.delete(event.document.uri);
  const settings = cachedSettings(event.document.uri);
  const pendingChange = pendingDocumentChanges.get(event.document.uri);
  logDebugTrace(settings, "document.change", "[asp-lsp] document.change", {
    uri: event.document.uri,
    version: event.document.version,
    pendingReason: pendingChange?.reason,
    ranged: pendingChange?.ranged,
    changeCount: pendingChange?.changes.length ?? 0,
    textLength: event.document.getText().length,
  });
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
      measureDebugStep(
        settings,
        event.document.uri,
        "documentChange.scheduleDiagnostics.postScheduleProjectUpdate",
        () => {
          if (
            openDocumentForUri(event.document.uri)?.version === cached.identity.version &&
            shouldScheduleProjectUpdateForDocumentChange(cached)
          ) {
            scheduleProjectUpdate("document.change");
          }
        },
      );
    })
    .catch((error: unknown) =>
      logServerWarning(
        `[asp-lsp] documentChange.scheduleDiagnostics.failed: ${errorMessage(error)}`,
      ),
    );
});
documents.onDidSave(async (event) => {
  noteForegroundActivity();
  const fileName = uriToFileName(event.document.uri);
  fsGateway.invalidatePath(fileName);
  logDebugTrace(cachedSettings(event.document.uri), "document.save", "[asp-lsp] document.save", {
    uri: event.document.uri,
    version: event.document.version,
  });
  await indexWorkspaceFileAsync(fileName, globalSettings);
  await refreshWorkspaceIncludeGraphFileAsync(fileName, globalSettings);
  if (!workspaceIndexDirty && cacheFreshness(globalSettings) === "watch") {
    await writeWorkspaceIndexToDiskAsync(globalSettings);
  }
  invalidateCachedAnalysisForUris(new Set([event.document.uri]), "document.save");
  scheduleProjectUpdate("document.save");
  validate(event.document);
});
documents.onDidClose((event) => {
  const settings =
    settingsByUri.get(event.document.uri) ?? settingsForUri(event.document.uri, globalSettings);
  logDebugTrace(settings, "document.close", "[asp-lsp] document.close", {
    uri: event.document.uri,
  });
  cancelScheduledDiagnostics(event.document.uri);
  documentOpenContentVersions.delete(event.document.uri);
  pendingDocumentChanges.delete(event.document.uri);
  inFlightDocumentRefreshes.delete(event.document.uri);
  deleteCachedDocumentsForUri(event.document.uri);
  clearInteractiveVbProjectContextSnapshotsForUris([event.document.uri]);
  clearSemanticTokensForUri(event.document.uri);
  stagedDiagnosticsByUri.delete(event.document.uri);
  publishedDiagnosticsByUri.delete(event.document.uri);
  resetIncludeDependencies(event.document.uri);
  invalidateRootlessWorkspaceIndex("document.close");
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
    configureFsGateway(globalSettings);
    logDebugTrace(
      globalSettings,
      "workspaceFolders.changed",
      "[asp-lsp] workspaceFolders.changed",
      {
        added: added.length,
        removed: removedFolders.length,
        roots: workspaceRoots.length,
      },
    );
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
  const previousGlobalSettings = globalSettings;
  const previousSettingsByUri = currentOpenDocumentSettingsByUri();
  const incoming = readSettingsFromChange(change.settings);
  if (incoming) {
    globalSettings = normalizeSettings(incoming);
  }
  logDebugTrace(globalSettings, "configuration.changed", "[asp-lsp] configuration.changed", {
    hasIncoming: incoming !== undefined,
    openDocuments: documents.all().length,
    logFileEnabled: globalSettings.debug?.logFile?.enabled === true,
  });
  configureFsGateway(globalSettings);
  void configureDiskAnalysisCacheAsync().catch((error) =>
    logDiskAnalysisCacheError("diskCache.configure", error),
  );
  clearCacheSettingProcessStateIfChanged(previousGlobalSettings, globalSettings);
  settingsByUri.clear();
  const impact = settingsInvalidationImpact(previousSettingsByUri);
  applySettingsInvalidation(impact);
  checkMemoryPressure(globalSettings, "configuration.changed", { force: true });
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
  const includeResolutionStructureChanges: WatchedAspFileChange[] = [];
  let nonAspIncludeResolutionStructureChanged = false;
  for (const file of change.changes) {
    const fileName = normalizeFileName(uriToFileName(file.uri));
    fsGateway.invalidatePath(fileName);
    if (file.type !== FileChangeType.Changed) {
      includeResolutionStructureChanges.push({ fileName, type: file.type });
      nonAspIncludeResolutionStructureChanged =
        nonAspIncludeResolutionStructureChanged || !isAspWorkspaceFile(fileName);
    }
    if (isAspWorkspaceFile(fileName)) {
      aspChanged = true;
      aspChanges.push({ fileName, type: file.type });
      if (file.type === FileChangeType.Deleted) {
        workspaceIndex.delete(fileIdentityKeyFromFileName(fileName));
        forgetSourceMetadata(fileName);
      } else {
        await indexWorkspaceFileAsync(fileName, globalSettings);
      }
      await refreshWorkspaceIncludeGraphFileAsync(fileName, globalSettings);
    }
    if (path.basename(fileName) === ".gitignore" && globalSettings.workspace?.respectGitIgnore) {
      invalidateWorkspaceIndex("gitignore.changed");
    }
    if (isScriptWorkspaceFile(fileName) || isJavaScriptProjectEnvironmentFile(fileName)) {
      scriptChanged = true;
    }
  }
  if (!aspChanged && !scriptChanged && includeResolutionStructureChanges.length === 0) {
    return;
  }
  logDebugTrace(globalSettings, "watchedFiles.changed", "[asp-lsp] watchedFiles.changed", {
    changes: change.changes.length,
    aspChanged,
    scriptChanged,
    aspChanges: aspChanges.length,
  });
  let includeRefsChangedFiles = new Set<string>();
  let publicChangedFiles = new Set<string>();
  let graphChangedFiles = new Set<string>();
  if (aspChanged) {
    clearDiskAnalysisSettingsKeyCache();
    const refresh = await refreshIncludeStateForAspChangesAsync(aspChanges);
    includeRefsChangedFiles = refresh.includeRefsChangedFiles;
    publicChangedFiles = refresh.publicChangedFiles;
    graphChangedFiles = new Set([...includeRefsChangedFiles, ...publicChangedFiles]);
    if (graphChangedFiles.size > 0) {
      await ensureIncludeGraphForOpenDocumentsAsync(graphChangedFiles);
    }
    includeCycleCache.clear();
    if (!workspaceIndexDirty && cacheFreshness(globalSettings) === "watch") {
      await writeWorkspaceIndexToDiskAsync(globalSettings);
    }
  }
  if (includeResolutionStructureChanges.length > 0) {
    invalidateIncludeResolutionForAspChanges(
      includeResolutionStructureChanges,
      "watchedFile.structureChanged",
    );
  }
  if (aspChanged || scriptChanged) {
    invalidateJsProject(scriptChanged ? "watchedScript.changed" : "watchedAsp.changed");
  }
  scheduleProjectUpdate(scriptChanged ? "watchedScript.changed" : "watchedAsp.changed");
  const affectedUris = scriptChanged
    ? new Set(documents.all().map((document) => document.uri))
    : nonAspIncludeResolutionStructureChanged
      ? openDocumentUris()
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

connection.workspace.onDidRenameFiles((params) => {
  for (const file of params.files) {
    fsGateway.invalidatePath(uriToFileName(file.oldUri));
    fsGateway.invalidatePath(uriToFileName(file.newUri));
  }
  invalidateWorkspaceIndex("fileOperation.rename");
  invalidateIncludeResolution("fileOperation.rename");
  invalidateJsProject("fileOperation.rename");
  invalidateCachedAnalysisForUris(openDocumentUris(), "fileOperation.rename");
});

connection.workspace.onDidCreateFiles((params) => {
  const changes: WatchedAspFileChange[] = [];
  for (const file of params.files) {
    const fileName = normalizeFileName(uriToFileName(file.uri));
    fsGateway.invalidatePath(fileName);
    changes.push({ fileName, type: FileChangeType.Created });
  }
  invalidateWorkspaceIndex("fileOperation.create");
  invalidateIncludeResolutionForAspChanges(changes, "fileOperation.create");
  invalidateJsProject("fileOperation.create");
  invalidateCachedAnalysisForUris(openDocumentUris(), "fileOperation.create");
});

connection.workspace.onDidDeleteFiles((params) => {
  const changes: WatchedAspFileChange[] = [];
  for (const file of params.files) {
    const fileName = normalizeFileName(uriToFileName(file.uri));
    fsGateway.invalidatePath(fileName);
    changes.push({ fileName, type: FileChangeType.Deleted });
  }
  invalidateWorkspaceIndex("fileOperation.delete");
  invalidateIncludeResolutionForAspChanges(changes, "fileOperation.delete");
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
    const directiveOpenCompletions = aspDirectiveOpenCompletions(cached, params.position, region);
    if (!region) {
      return withCompletionData(directiveOpenCompletions, {
        kind: "aspDirective",
        uri: cached.source.uri,
      });
    }
    const triggerCharacter = completionTriggerCharacter(params);
    if (isCssOnlyCompletionTrigger(triggerCharacter) && region.language !== "css") {
      return [];
    }
    const remember = (items: CompletionItem[], contextIdentity?: string): CompletionItem[] => {
      completionSessionCache.set(cached, settings, region, params.position, items, contextIdentity);
      return items;
    };
    if (region.language === "asp-directive") {
      return remember(
        withCompletionData(aspDirectiveCompletions(cached, params.position, region), {
          kind: "aspDirective",
          uri: cached.source.uri,
        }),
      );
    }
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
      const context = withCachedVbBuiltinRuntime(
        cached,
        warmedContext?.context ??
          (await buildImmediateLocalVbProjectContextAsync(cached, settings)),
      );
      const completions = getVbscriptCompletions(cached.parsed, params.position, context);
      const completionItems =
        completions.length > 0
          ? withUnresolvedVbscriptCompletionItems(
              cached,
              settings,
              context,
              params.position,
              completions,
            )
          : fallbackVbMemberCompletions(cached, params.position, context);
      const completionItemsWithDirective = directiveOpenCompletions.length
        ? [...directiveOpenCompletions, ...completionItems]
        : completionItems;
      const itemsWithDirective = withCompletionData(completionItemsWithDirective, {
        kind: "vbscript",
        uri: cached.source.uri,
      });
      return shouldCache ? remember(itemsWithDirective, contextIdentity) : itemsWithDirective;
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
      const htmlItems = htmlService.doComplete(
        virtualDocument,
        params.position,
        htmlService.parseHTMLDocument(virtualDocument),
      ).items;
      const baseItems = [
        ...aspIncludeCompletions(cached, params.position, settings),
        ...directiveOpenCompletions,
        ...htmlItems,
      ];
      return remember(
        withCompletionData(
          withAdditionalCompletionItems(
            baseItems,
            htmlClassIdAttributeCompletions(cached, params.position),
          ),
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
            withCachedVbBuiltinRuntime(
              cached,
              withSourceUriFormatter(
                await interactiveVbProjectContextAsync(cached, cachedSettings(cached.source.uri)),
              ),
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
    const settings = cachedSettings(params.textDocument.uri);
    const cached = await measureDebugStepAsync(
      settings,
      params.textDocument.uri,
      "hover.ensureFresh",
      () => getFreshCachedAsync(params.textDocument.uri),
    );
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
      return measureDebugStepAsync(settings, cached.source.uri, "hover.javascript", () =>
        jsHoverAsync(cached, params.position),
      );
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
  const settings = cachedSettings(cached.source.uri);
  const symbolRenameScope = renameSymbolScope(settings);
  if (isJavaScriptPosition(cached, params.position)) {
    const rename = await jsRenameAsync(cached, params.position, params.newName, symbolRenameScope);
    const crossLanguage = await crossLanguageRename(
      cached,
      params.position,
      params.newName,
      symbolRenameScope,
    );
    return mergeWorkspaceEdits([rename, crossLanguage]) ?? null;
  }
  if (isHtmlPosition(cached, params.position)) {
    const rename = htmlRename(cached, params.position, params.newName);
    const crossLanguage = await crossLanguageRename(
      cached,
      params.position,
      params.newName,
      "document",
    );
    return mergeWorkspaceEdits([rename, crossLanguage]) ?? null;
  }
  if (isCssPosition(cached, params.position)) {
    const rename = cssRename(cached, params.position, params.newName);
    const crossLanguage = await crossLanguageRename(
      cached,
      params.position,
      params.newName,
      "document",
    );
    return mergeWorkspaceEdits([rename, crossLanguage]) ?? null;
  }
  if (!isVbscriptPosition(cached, params.position)) {
    return null;
  }
  const context =
    symbolRenameScope === "workspace"
      ? await buildFullVbProjectContextForWorkspaceOperationAsync(cached, settings)
      : await localVbReferenceContextAsync(cached, settings);
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
  const openDocuments = await workspaceAnalyzableOpenDocumentsAsync(globalSettings);
  const openedUris = new Set(openDocuments.map((document) => document.uri));
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
      openDocuments.map(async (document) => {
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
      openDocuments.map(async (document) => {
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
  const document = openDocumentForUri(params.textDocument.uri);
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
  const openDocuments = await workspaceAnalyzableOpenDocumentsAsync(globalSettings);
  const openedUris = new Set(openDocuments.map((document) => document.uri));
  const concurrency = analysisConcurrency(globalSettings);
  const indexedEntries = workspaceEntriesAffectedFirst(
    [...workspaceIndex.values()].filter((entry) => !openedUris.has(entry.uri)),
  );
  const reportCacheKey = workspaceDiagnosticsReportCacheKey(openDocuments, indexedEntries);
  const cachedReport = cachedWorkspaceDiagnosticsReport(reportCacheKey, globalSettings);
  if (cachedReport) {
    return cachedReport;
  }
  const task = beginProgressTask("analyzing", "workspace.diagnostics", {
    current: 0,
    total: indexedEntries.length + openDocuments.length,
    cancellable: true,
  });
  const cancellation: AnalysisCancellation = {
    isCancellationRequested: () => token.isCancellationRequested || task.isCancellationRequested(),
  };
  const progressToken = {
    get isCancellationRequested() {
      return cancellation.isCancellationRequested();
    },
  };
  try {
    task.update({ label: "workspace.diagnostics.openDocuments" });
    const openItems = await mapWithConcurrency(
      openDocuments,
      concurrency,
      async (document) => {
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
      },
      progressMapHooks(task, (document) => progressFileLabelFromUri(document.uri)),
    );
    task.update({ label: "workspace.diagnostics.indexed", current: openDocuments.length });
    const indexedItems = await mapWithConcurrency(
      indexedEntries,
      concurrency,
      async (entry) => ({
        kind: "full" as const,
        uri: entry.uri,
        version: null,
        items: await diagnosticsForIndexed(entry, cachedSettings(entry.uri), progressToken),
      }),
      progressMapHooks(task, (entry) => progressFileLabel(entry.fileName), openDocuments.length),
    );
    const report = {
      items: [...openItems.filter((item) => item !== undefined), ...indexedItems],
    };
    if (!token.isCancellationRequested) {
      rememberWorkspaceDiagnosticsReport(reportCacheKey, report, globalSettings);
    }
    return cloneWorkspaceDiagnosticsReport(report);
  } finally {
    task.end();
  }
});

connection.onExecuteCommand(async (params, token) => {
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
    return buildGraphCommandAsync(params.arguments?.[0], token);
  }
  if (params.command === buildFlowchartServerCommand) {
    return buildAspFlowchartForCommand(params.arguments?.[0], token);
  }
  if (params.command === buildNavigationGraphServerCommand) {
    return buildAspNavigationGraphForCommand(params.arguments?.[0], token);
  }
  if (params.command === exportAnalysisExcelServerCommand) {
    return exportAnalysisExcelForCommand(params.arguments?.[0], token);
  }
  if (params.command === previewWorkspaceFilesServerCommand) {
    return previewWorkspaceFilesForCommand(params.arguments?.[0], token);
  }
  if (params.command === cancelProgressTaskServerCommand) {
    const taskId = progressTaskIdArgument(params.arguments?.[0]);
    return { ok: taskId ? cancelProgressTask(taskId) : false };
  }
  return {
    ok: false,
    message: createLocalizer(globalSettings.resolvedLocale).t("server.unknownCommand", {
      command: params.command,
    }),
  };
});

function progressTaskIdArgument(argument: unknown): string | undefined {
  if (!argument || typeof argument !== "object" || !("id" in argument)) {
    return undefined;
  }
  const id = (argument as { id?: unknown }).id;
  return typeof id === "string" ? id : undefined;
}

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
    ...htmlInlineStyleCodeActions(cached, params.range, params.context),
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
  const previous = latestSemanticTokenEntryForUri(cached.source.uri);
  const data = await buildFullSemanticTokenDataAsync(cached, previous);
  return cacheSemanticTokens(
    cached.source.uri,
    data,
    semanticTokenResultMetadata(cached, cachedSettings(cached.source.uri)),
  );
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
    const previous = semanticTokenEntryById(params.previousResultId);
    const next = await buildFullSemanticTokenDataAsync(cached, previous);
    if (!previous || !sameFileIdentityUri(previous.uri, cached.source.uri)) {
      return cacheSemanticTokens(
        cached.source.uri,
        next,
        semanticTokenResultMetadata(cached, cachedSettings(cached.source.uri)),
      );
    }
    const resultId = nextSemanticTokenResultId();
    semanticTokenResults.set(resultId, {
      uri: cached.source.uri,
      data: next,
      ...semanticTokenResultMetadata(cached, cachedSettings(cached.source.uri)),
    });
    latestSemanticTokenResultByUri.set(semanticTokenUriKey(cached.source.uri), resultId);
    semanticTokenResults.delete(params.previousResultId);
    retainedSemanticTokenResults.delete(params.previousResultId);
    if (
      latestRetainedSemanticTokenResultByUri.get(semanticTokenUriKey(cached.source.uri)) ===
      params.previousResultId
    ) {
      latestRetainedSemanticTokenResultByUri.delete(semanticTokenUriKey(cached.source.uri));
    }
    return {
      resultId,
      edits: [semanticTokenDeltaEdit(previous.data, next)],
    };
  },
);

function validate(document: TextDocument): void {
  cancelScheduledDiagnostics(document.uri);
  void validateAsync(document).catch((error: unknown) =>
    logServerWarning(`[asp-lsp] validate.failed: ${errorMessage(error)}`),
  );
}

function validateOpenedDocument(document: TextDocument): void {
  cancelScheduledDiagnostics(document.uri);
  void validateOpenedDocumentAsync(document).catch((error: unknown) =>
    logServerWarning(`[asp-lsp] validate.open.failed: ${errorMessage(error)}`),
  );
}

async function validateOpenedDocumentAsync(document: TextDocument): Promise<void> {
  const cached = await validateAsync(document);
  if (!cached || openDocumentForUri(document.uri)?.version !== cached.identity.version) {
    return;
  }
  const settings = cachedSettings(document.uri);
  scheduleDocumentOpenVbProjectContextPrewarm(cached, settings);
  scheduleDocumentOpenJsDiagnosticsPrewarm(cached, settings);
}

async function validateAsync(document: TextDocument): Promise<CachedDocument | undefined> {
  const cached = await ensureFreshDiagnosticsCachedDocumentAsync(document);
  if (openDocumentForUri(document.uri)?.version !== cached.identity.version) {
    return undefined;
  }
  startStagedDiagnostics(cached, cachedSettings(document.uri), true, {
    preservePreviousDiagnosticsUntilFinal: hasPublishedDiagnostics(document.uri),
  });
  return cached;
}

function refreshCachedDocument(document: TextDocument, impactReason?: string): CachedDocument {
  const startedAt = process.hrtime.bigint();
  const settingsStartedAt = startedAt;
  const settings = cachedSettings(document.uri);
  startAnalysisLog(settings, document.uri);
  finishDebugStep(settings, document.uri, "analysis.settings", settingsStartedAt);
  const parseStartedAt = process.hrtime.bigint();
  const parsed = parseSourceDocument(
    document.uri,
    document.getText(),
    settings,
    document.languageId,
  );
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
  cacheDocumentIfCurrent(document, cached);
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
  const parsed = await parseSourceDocumentSkeletonAsync(
    document.uri,
    document.getText(),
    settings,
    document.languageId,
  );
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
  cacheDocumentIfCurrent(document, cached);
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
  if (isStandaloneVbscriptSource(document.uri, document.languageId)) {
    return refreshCachedDocument(document, "standalone vbscript edit");
  }
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
      ? [...previous.editHistory, updated.impact].slice(-maxCachedDocumentEditHistory)
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
  cacheDocumentIfCurrent(document, cached);
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
  if (isStandaloneVbscriptSource(document.uri, document.languageId)) {
    return refreshCachedDocumentSkeletonAsync(document, "standalone vbscript edit");
  }
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
      ? [...previous.editHistory, updated.impact].slice(-maxCachedDocumentEditHistory)
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
  cacheDocumentIfCurrent(document, cached);
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
    touchCachedDocument(existing);
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
      if (canApplyPendingIncremental(existing, pending)) {
        return refreshCachedDocumentIncremental(existing, document, settings, pending.changes[0]);
      }
      return refreshCachedDocument(document, pending?.reason ?? "non-incremental document change");
    }
  }
  pendingDocumentChanges.delete(document.uri);
  return refreshCachedDocument(document);
}

function canApplyPendingIncremental(
  existing: CachedDocument,
  pending: PendingDocumentChange,
): pending is PendingDocumentChange & { changes: [AspIncrementalChange] } {
  return (
    pending.ranged &&
    pending.changes.length === 1 &&
    existing.identity.version + 1 === pending.version
  );
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
    touchCachedDocument(existing);
    return existing;
  }
  if (existing && existing.parseSettingsIdentity === parseIdentity) {
    const pending = pendingDocumentChanges.get(document.uri);
    if (pending?.version === document.version) {
      pendingDocumentChanges.delete(document.uri);
      if (canApplyPendingIncremental(existing, pending)) {
        return rememberInFlightDocumentRefresh(
          document,
          parseIdentity,
          withProgressTaskAsync(
            "analyzing",
            "document.analysis",
            { current: 0, total: 2, detail: progressFileLabelFromUri(document.uri) },
            async (task) => {
              task.update({ label: "document.analysis.incremental", current: 0 });
              const cached = await refreshCachedDocumentIncrementalAsync(
                existing,
                document,
                settings,
                pending.changes[0],
              );
              task.update({ label: "document.analysis.cache", current: 1 });
              task.update({ label: "document.analysis.ready", current: 2 });
              return cached;
            },
          ),
        );
      }
      return rememberInFlightDocumentRefresh(
        document,
        parseIdentity,
        withProgressTaskAsync(
          "analyzing",
          "document.analysis",
          { current: 0, total: 2, detail: progressFileLabelFromUri(document.uri) },
          async (task) => {
            task.update({ label: "document.analysis.parse", current: 0 });
            const cached = await refreshCachedDocumentSkeletonAsync(
              document,
              pending?.reason ?? "non-incremental document change",
            );
            task.update({ label: "document.analysis.cache", current: 1 });
            task.update({ label: "document.analysis.ready", current: 2 });
            return cached;
          },
        ),
      );
    }
  }
  pendingDocumentChanges.delete(document.uri);
  return rememberInFlightDocumentRefresh(
    document,
    parseIdentity,
    withProgressTaskAsync(
      "analyzing",
      "document.analysis",
      { current: 0, total: 2, detail: progressFileLabelFromUri(document.uri) },
      async (task) => {
        task.update({ label: "document.analysis.parse", current: 0 });
        const cached = await refreshCachedDocumentSkeletonAsync(document);
        task.update({ label: "document.analysis.cache", current: 1 });
        task.update({ label: "document.analysis.ready", current: 2 });
        return cached;
      },
    ),
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
  const document = openDocumentForUri(uri);
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
    lastAccess: Date.now(),
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

function cacheDocumentIfCurrent(document: TextDocument, cached: CachedDocument): void {
  const current = openDocumentForUri(document.uri);
  if (current && current.version !== document.version) {
    return;
  }
  touchCachedDocument(cached);
  cache.set(document.uri, cached);
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
  return sameFileIdentityUri(left.uri, right.uri) && left.version === right.version;
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

function mergePendingDocumentChanges(
  _previous: PendingDocumentChange,
  next: PendingDocumentChange,
): PendingDocumentChange {
  return {
    version: next.version,
    changes: [],
    reason: "multiple pending document changes",
    ranged: false,
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
  const cached = await measureDebugStepAsync(
    settings,
    document.uri,
    "documentChange.scheduleDiagnostics.ensureFresh",
    () => ensureFreshDiagnosticsCachedDocumentAsync(document),
  );
  if (openDocumentForUri(document.uri)?.version !== cached.identity.version) {
    return cached;
  }
  const preservePreviousDiagnosticsUntilFinal = hasPublishedDiagnostics(document.uri);
  const state = measureDebugStep(
    settings,
    document.uri,
    "documentChange.scheduleDiagnostics.startStaged",
    () =>
      startStagedDiagnostics(cached, settings, false, {
        preservePreviousDiagnosticsUntilFinal,
      }),
  );
  const delay = settings.diagnostics?.debounceMs ?? defaultDiagnosticsDebounceMs;
  logDebugTrace(settings, "diagnostics.schedule", "[asp-lsp] diagnostics.schedule", {
    uri: document.uri,
    version: document.version,
    delay,
    preservePreviousDiagnosticsUntilFinal,
  });
  if (delay <= 0) {
    measureDebugStep(
      settings,
      document.uri,
      "documentChange.scheduleDiagnostics.kickoffAsyncLayers",
      () => ensureStagedDiagnosticsAsyncLayers(cached, settings, state),
    );
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
      measureDebugStep(
        settings,
        document.uri,
        "documentChange.scheduleDiagnostics.kickoffAsyncLayers",
        () => ensureStagedDiagnosticsAsyncLayers(cached, settings, state),
      );
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
  if (isClientScriptOnlyIncrementalChange(cached)) {
    return false;
  }
  return cached.lastEditImpact.language !== "html" && cached.lastEditImpact.language !== "css";
}

function isClientScriptOnlyIncrementalChange(cached: CachedDocument): boolean {
  const impact = cached.lastEditImpact;
  const change = cached.lastIncrementalChange;
  if (impact?.kind !== "incremental" || !change) {
    return false;
  }
  const text = cached.source.getText();
  const candidates = new Set<number>();
  const addCandidate = (offset: number): void => {
    if (text.length === 0) {
      return;
    }
    candidates.add(Math.max(0, Math.min(text.length - 1, offset)));
  };
  const changeStart = cached.source.offsetAt(change.range.start);
  addCandidate(changeStart);
  if (change.text.length > 0) {
    addCandidate(changeStart + change.text.length - 1);
  }
  const regions = [...candidates]
    .map((offset) => findRegionAt(cached.parsed, offset))
    .filter((region): region is AspRegion => region !== undefined);
  return regions.length > 0 && regions.every(isClientScriptRegion);
}

function isClientScriptRegion(region: AspRegion): boolean {
  return (
    region.kind === "client-script" &&
    (region.language === "javascript" || region.language === "jscript")
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
  const reusable = reusableStagedDiagnosticsState(cached);
  if (reusable) {
    reusable.preservePreviousDiagnosticsUntilFinal =
      reusable.preservePreviousDiagnosticsUntilFinal ||
      options.preservePreviousDiagnosticsUntilFinal === true;
    logDebugTrace(settings, "diagnostics.reuse", "[asp-lsp] diagnostics.reuse", {
      uri: cached.source.uri,
      version: cached.source.version,
      generation: reusable.generation,
      runAsyncLayers,
      preservePreviousDiagnosticsUntilFinal: reusable.preservePreviousDiagnosticsUntilFinal,
    });
    logDebugSummary(
      settings,
      `[asp-lsp] diagnostics.reuse: ${cached.source.uri}, generation=${reusable.generation}`,
    );
    if (runAsyncLayers) {
      ensureStagedDiagnosticsAsyncLayers(cached, settings, reusable);
    }
    return reusable;
  }
  const state: StagedDiagnosticsState = {
    generation: ++stagedDiagnosticsGeneration,
    uri: cached.source.uri,
    version: cached.source.version,
    documentGeneration: cached.generation,
    diagnosticsIdentity: cached.diagnosticsIdentity,
    includeResolutionGeneration: cached.includeResolutionGeneration,
    jsProjectGeneration: cached.jsProjectGeneration,
    workspaceGeneration: cached.workspaceGeneration,
    startedAt: startCheckLog(cached, settings),
    preservePreviousDiagnosticsUntilFinal: options.preservePreviousDiagnosticsUntilFinal === true,
    asyncLayersStarted: false,
    layers: {},
  };
  stagedDiagnosticsByUri.set(cached.source.uri, state);
  logDebugTrace(settings, "diagnostics.start", "[asp-lsp] diagnostics.start", {
    uri: cached.source.uri,
    version: cached.source.version,
    generation: state.generation,
    runAsyncLayers,
    preservePreviousDiagnosticsUntilFinal: state.preservePreviousDiagnosticsUntilFinal,
  });
  state.layers.fast = measureDebugStep(
    settings,
    cached.source.uri,
    "check.parserDiagnostics",
    () => cached.parsed.diagnostics,
  );
  publishStagedDiagnosticsLayer(cached, settings, state, "fast");
  if (runAsyncLayers) {
    ensureStagedDiagnosticsAsyncLayers(cached, settings, state);
  }
  return state;
}

function reusableStagedDiagnosticsState(
  cached: CachedDocument,
): StagedDiagnosticsState | undefined {
  const active = stagedDiagnosticsByUri.get(cached.source.uri);
  if (
    !active ||
    active.version !== cached.source.version ||
    active.documentGeneration !== cached.generation ||
    active.diagnosticsIdentity !== cached.diagnosticsIdentity ||
    active.includeResolutionGeneration !== cached.includeResolutionGeneration ||
    active.jsProjectGeneration !== cached.jsProjectGeneration ||
    active.workspaceGeneration !== cached.workspaceGeneration
  ) {
    return undefined;
  }
  return active;
}

function ensureStagedDiagnosticsAsyncLayers(
  cached: CachedDocument,
  settings: AspSettings,
  state: StagedDiagnosticsState,
): void {
  if (state.asyncLayersStarted) {
    return;
  }
  state.asyncLayersStarted = true;
  void runStagedDiagnosticsWithProgress(cached, settings, state).catch((error: unknown) =>
    logServerWarning(`[asp-lsp] diagnostics.failed: ${errorMessage(error)}`),
  );
}

async function runStagedDiagnosticsWithProgress(
  cached: CachedDocument,
  settings: AspSettings,
  state: StagedDiagnosticsState,
): Promise<void> {
  const task = beginProgressTask("analyzing", "diagnostics", {
    current: 0,
    total: 4,
    detail: progressFileLabelFromUri(cached.source.uri),
  });
  try {
    await runStagedDiagnostics(cached, settings, state, task);
  } finally {
    task.end();
  }
}

async function runStagedDiagnostics(
  cached: CachedDocument,
  settings: AspSettings,
  state: StagedDiagnosticsState,
  progress?: AspLspProgressTaskHandle,
): Promise<void> {
  const cancellation: AnalysisCancellation = {
    isCancellationRequested: () => !isCurrentStagedDiagnostics(cached, state),
  };
  const includeItemsPromise = measureDebugStep(
    settings,
    cached.source.uri,
    "diagnostics.async.start.include",
    () => includeDiagnosticsForCachedAsync(cached, settings, "check", cancellation),
  );
  const projectFastItemsPromise = measureDebugStep(
    settings,
    cached.source.uri,
    "diagnostics.async.start.projectFast",
    () =>
      projectFastDiagnosticsForCachedAsync(
        cached,
        settings,
        "check.projectFast",
        cancellation,
        "foreground",
      ),
  );
  void includeItemsPromise.catch(() => undefined);
  void projectFastItemsPromise.catch(() => undefined);
  const syntaxItems = measureDebugStep(
    settings,
    cached.source.uri,
    "diagnostics.async.start.syntax",
    () => syntaxDiagnosticsForCached(cached, settings, "check"),
  );
  const includeItems = await includeItemsPromise;
  if (!isCurrentStagedDiagnostics(cached, state)) {
    logStaleStagedDiagnostics(settings, state, "include");
    return;
  }
  state.layers.include = includeItems;
  publishStagedDiagnosticsLayer(cached, settings, state, "include");
  progress?.update({
    label: "diagnostics.include",
    current: 1,
    detail: progressFileLabelFromUri(cached.source.uri),
  });
  await yieldToEventLoop();

  if (!isCurrentStagedDiagnostics(cached, state)) {
    logStaleStagedDiagnostics(settings, state, "syntax");
    return;
  }
  state.layers.syntax = syntaxItems;
  publishStagedDiagnosticsLayer(cached, settings, state, "syntax");
  progress?.update({
    label: "diagnostics.syntax",
    current: 2,
    detail: progressFileLabelFromUri(cached.source.uri),
  });
  await yieldToEventLoop();

  if (!isCurrentStagedDiagnostics(cached, state)) {
    logStaleStagedDiagnostics(settings, state, "projectFast");
    return;
  }
  state.layers.projectFast = await projectFastItemsPromise;
  publishStagedDiagnosticsLayer(cached, settings, state, "projectFast");
  progress?.update({
    label: "diagnostics.projectFast",
    current: 3,
    detail: progressFileLabelFromUri(cached.source.uri),
  });
  await yieldToEventLoop();

  const projectItemsPromise = projectDiagnosticsForCachedAsync(
    cached,
    settings,
    "check",
    cancellation,
    "foreground",
  );
  void projectItemsPromise.catch(() => undefined);

  if (!isCurrentStagedDiagnostics(cached, state)) {
    logStaleStagedDiagnostics(settings, state, "project");
    return;
  }
  state.layers.project = await projectItemsPromise;
  shareStagedAnalysisWithCurrentCache(cached, state);
  publishStagedDiagnosticsLayer(cached, settings, state, "project");
  progress?.update({
    label: "diagnostics.project",
    current: 4,
    detail: progressFileLabelFromUri(cached.source.uri),
  });
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
    ...(state.layers.project ?? state.layers.projectFast ?? []),
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
  const document = openDocumentForUri(state.uri);
  const active = stagedDiagnosticsByUri.get(state.uri);
  const current = cache.get(state.uri);
  return (
    active?.generation === state.generation &&
    document?.version === state.version &&
    current?.identity.version === state.version &&
    cached.identity.version === state.version &&
    current?.diagnosticsIdentity === state.diagnosticsIdentity &&
    current.includeResolutionGeneration === state.includeResolutionGeneration &&
    current.jsProjectGeneration === state.jsProjectGeneration &&
    current.workspaceGeneration === state.workspaceGeneration
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

async function projectFastDiagnosticsForCachedAsync(
  cached: CachedDocument,
  settings: AspSettings,
  stepPrefix: string,
  cancellation: AnalysisCancellation = neverCancelled,
  mode: AnalysisExecutionMode = "foreground",
): Promise<Diagnostic[]> {
  const vbItemsPromise = vbFastDiagnosticsAsync(cached, settings, stepPrefix, cancellation, mode);
  const jsItems = cachedJsSlowDiagnostics(cached, settings, stepPrefix);
  const vbItems = await vbItemsPromise;
  if (cancellation.isCancellationRequested()) {
    return [];
  }
  return measureDebugStep(settings, cached.source.uri, `${stepPrefix}.project.dedupe`, () =>
    dedupeDiagnostics([...vbItems, ...jsItems]),
  );
}

function cachedJsSlowDiagnostics(
  cached: CachedDocument,
  settings: AspSettings,
  stepPrefix: string,
): Diagnostic[] {
  const cachedItems = cached.analysis?.jsSlowDiagnostics;
  if (!cachedItems || isClientScriptOnlyIncrementalChange(cached)) {
    return [];
  }
  if (cachedItems.key !== jsDiagnosticsCacheKey(cached, settings)) {
    return [];
  }
  return measureDebugStep(
    settings,
    cached.source.uri,
    `${stepPrefix}.javascriptDiagnostics.reuse`,
    () => cachedJsDiagnosticsToLsp(cached, cachedItems),
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

function workspaceEntriesOpenFirst(
  entries: WorkspaceIndexedDocument[],
): WorkspaceIndexedDocument[] {
  const opened = openDocumentUris();
  if (entries.length === 0 || opened.size === 0) {
    return entries;
  }
  const openEntries: WorkspaceIndexedDocument[] = [];
  const restEntries: WorkspaceIndexedDocument[] = [];
  for (const entry of entries) {
    (opened.has(entry.uri) ? openEntries : restEntries).push(entry);
  }
  return [...openEntries, ...restEntries];
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
  logDebugFile(settings, "debug", "debug.summary", message);
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
  const message = `[asp-lsp] ${step}: ${uri} ${formatElapsedMs(elapsedMs)}`;
  if (isDebugVerboseEnabled(settings)) {
    connection.console.info(message);
  }
  logDebugFile(settings, "debug", "debug.elapsed", message, { uri, step, elapsedMs });
}

function logDebugTrace(
  settings: AspSettings,
  category: string,
  message: string,
  metadata?: Record<string, unknown>,
): void {
  logDebugFile(settings, "trace", category, message, metadata);
}

function logServerWarning(message: string, settings: AspSettings = globalSettings): void {
  connection.console.warn(message);
  logDebugFile(settings, "warn", "server.warning", message);
}

function logDebugFile(
  settings: AspSettings,
  level: DebugLogFileLevel,
  category: string,
  message: string,
  metadata?: Record<string, unknown>,
): void {
  if (settings.debug?.logFile?.enabled !== true) {
    return;
  }
  debugLogFileWriter.enqueue({
    filePath: resolveDebugLogFilePath(settings),
    level,
    category,
    message,
    metadata,
  });
}

function resolveDebugLogFilePath(settings: AspSettings): string {
  const configuredPath = settings.debug?.logFile?.path?.trim();
  const selectedPath =
    configuredPath ||
    process.env.ASP_LSP_DEFAULT_DEBUG_LOG_FILE ||
    path.join(os.tmpdir(), defaultDebugLogFileName);
  return path.isAbsolute(selectedPath)
    ? selectedPath
    : path.resolve(workspaceRoots[0] ?? process.cwd(), selectedPath);
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
  return availableAnalysisConcurrency();
}

const neverCancelled: AnalysisCancellation = {
  isCancellationRequested: () => false,
};

async function fileExistsAsync(fileName: string): Promise<boolean> {
  const stat = await fsGateway.statAsync(fileName);
  return Boolean(stat?.isFile());
}

async function fileSizeAsync(
  fileName: string,
  settings: AspSettings = globalSettings,
): Promise<number | undefined> {
  if (cacheFreshness(settings) === "watch") {
    const source = sourceMetadataFromManifest(fileName);
    if (source) {
      return source.size;
    }
  }
  const stat = await fsGateway.statAsync(fileName);
  return stat?.isFile() ? stat.size : undefined;
}

async function pathExistsAsync(fileName: string): Promise<boolean> {
  const stat = await fsGateway.statAsync(fileName);
  return Boolean(stat);
}

async function mapWithConcurrency<T, U>(
  items: readonly T[],
  concurrency: number,
  callback: (item: T, index: number) => Promise<U>,
  progress?: {
    onItemStart?: (item: T, index: number, workerIndex: number) => void;
    onItemDone?: (item: T, index: number, workerIndex: number) => void;
  },
): Promise<U[]> {
  const results = Array.from<U>({ length: items.length });
  let next = 0;
  const workers = Array.from(
    { length: Math.min(concurrency, items.length) },
    async (_, workerIndex) => {
      while (next < items.length) {
        const index = next;
        next += 1;
        progress?.onItemStart?.(items[index], index, workerIndex);
        try {
          results[index] = await callback(items[index], index);
        } finally {
          progress?.onItemDone?.(items[index], index, workerIndex);
        }
        if ((index + 1) % 64 === 0) {
          await yieldToEventLoop();
        }
      }
    },
  );
  await Promise.all(workers);
  return results;
}

function progressMapHooks<T>(
  handle: AspLspProgressTaskHandle,
  labelForItem: (item: T, index: number) => string,
  initialCompleted = 0,
): {
  onItemStart(item: T, index: number, workerIndex: number): void;
  onItemDone(item: T, index: number, workerIndex: number): void;
} {
  const active = new Map<number, string>();
  let completed = initialCompleted;
  const publish = (detail?: string): void => {
    handle.update({
      current: completed,
      detail,
      activeItems: [...active.entries()]
        .sort((left, right) => left[0] - right[0])
        .map(([, label]) => label),
    });
  };
  return {
    onItemStart(item, index, workerIndex) {
      active.set(workerIndex, labelForItem(item, index));
      publish(active.get(workerIndex));
    },
    onItemDone(_item, _index, workerIndex) {
      active.delete(workerIndex);
      completed += 1;
      publish();
    },
  };
}

function progressFileLabelFromUri(uri: string): string {
  return uri.startsWith("file://") ? progressFileLabel(graphFileNameFromUri(uri)) : uri;
}

function progressFileLabel(fileName: string): string {
  const normalized = normalizeFileName(fileName);
  const root = workspaceRoots
    .map(normalizeFileName)
    .sort((left, right) => right.length - left.length)
    .find((candidate) => isFileInDirectoryOrEqual(normalized, candidate));
  return root
    ? path.relative(root, normalized) || path.basename(normalized)
    : path.basename(normalized);
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

interface ResolvedNetworkProfile {
  kind: "local" | "network";
  statCacheTtlMs: number;
  negativeStatCacheTtlMs: number;
  readdirCacheTtlMs: number;
  includeReadConcurrency: number;
  caseResolution: "full" | "fast";
}

function resolveNetworkProfile(settings: AspSettings): ResolvedNetworkProfile {
  const requested = settings.network?.profile ?? "auto";
  const network =
    requested === "network" || (requested === "auto" && networkPathHeuristic(settings));
  const base: ResolvedNetworkProfile = network
    ? {
        kind: "network",
        statCacheTtlMs: defaultNetworkStatCacheTtlMs,
        negativeStatCacheTtlMs: defaultNetworkNegativeStatCacheTtlMs,
        readdirCacheTtlMs: defaultNetworkReaddirCacheTtlMs,
        includeReadConcurrency: defaultNetworkIncludeReadConcurrency,
        caseResolution: "fast",
      }
    : {
        kind: "local",
        statCacheTtlMs: 0,
        negativeStatCacheTtlMs: 0,
        readdirCacheTtlMs: 0,
        includeReadConcurrency: analysisConcurrency(settings),
        caseResolution: "full",
      };
  return {
    ...base,
    statCacheTtlMs: nonNegativeIntegerSetting(
      settings.network?.statCacheTtlMs,
      base.statCacheTtlMs,
    ),
    negativeStatCacheTtlMs:
      settings.network?.statCacheTtlMs !== undefined
        ? Math.min(
            defaultNetworkNegativeStatCacheTtlMs,
            nonNegativeIntegerSetting(settings.network.statCacheTtlMs, base.negativeStatCacheTtlMs),
          )
        : base.negativeStatCacheTtlMs,
    readdirCacheTtlMs: nonNegativeIntegerSetting(
      settings.network?.readdirCacheTtlMs,
      base.readdirCacheTtlMs,
    ),
    includeReadConcurrency: networkConcurrencySetting(
      settings.network?.includeReadConcurrency,
      base.includeReadConcurrency,
    ),
    caseResolution:
      settings.network?.caseResolution && settings.network.caseResolution !== "auto"
        ? settings.network.caseResolution
        : base.caseResolution,
  };
}

function configureFsGateway(settings: AspSettings): void {
  const profile = resolveNetworkProfile(settings);
  fsGateway.configure({
    statTtlMs: profile.statCacheTtlMs,
    negativeStatTtlMs: profile.negativeStatCacheTtlMs,
    readdirTtlMs: profile.readdirCacheTtlMs,
  });
}

function includeReadConcurrency(settings: AspSettings): number {
  return resolveNetworkProfile(settings).includeReadConcurrency;
}

function networkPathHeuristic(settings: AspSettings): boolean {
  return [
    ...workspaceRoots,
    ...(settings.virtualRoots ?? []),
    ...(settings.includePaths ?? []),
    settings.virtualRoot,
  ].some((candidate) => typeof candidate === "string" && looksLikeNetworkPath(candidate));
}

function looksLikeNetworkPath(fileName: string): boolean {
  const normalized = fileName.replace(/\\/g, "/");
  return (
    fileName.startsWith("\\\\") ||
    normalized.startsWith("//") ||
    normalized.startsWith("/Volumes/") ||
    normalized.startsWith("/mnt/") ||
    normalized.startsWith("/net/") ||
    normalized.includes("/.gvfs/")
  );
}

function networkConcurrencySetting(value: number | undefined, fallback: number): number {
  if (typeof value !== "number" || !Number.isFinite(value) || value <= 0) {
    return Math.max(1, fallback);
  }
  return Math.max(1, Math.min(64, Math.floor(value)));
}

function nonNegativeIntegerSetting(value: number | undefined, fallback: number): number {
  if (typeof value !== "number" || !Number.isFinite(value) || value < 0) {
    return fallback;
  }
  return Math.floor(value);
}

function noteForegroundActivity(): void {
  lastForegroundActivityAt = Date.now();
}

function beginServerStatus(kind: ActiveAspLspStatusKind, reason: string): () => void {
  incrementServerStatus(kind);
  publishServerStatus(reason);
  let ended = false;
  return () => {
    if (ended) {
      return;
    }
    ended = true;
    decrementServerStatus(kind);
    publishServerStatus(reason);
  };
}

async function withServerStatusAsync<T>(
  kind: ActiveAspLspStatusKind,
  reason: string,
  callback: () => Promise<T>,
): Promise<T> {
  const end = beginServerStatus(kind, reason);
  try {
    return await callback();
  } finally {
    end();
  }
}

async function withProgressTaskAsync<T>(
  kind: ActiveAspLspStatusKind,
  label: string,
  options: AspLspProgressTaskOptions,
  callback: (task: AspLspProgressTaskHandle) => Promise<T>,
): Promise<T> {
  const task = beginProgressTask(kind, label, options);
  try {
    return await callback(task);
  } finally {
    task.end();
  }
}

function beginProgressTask(
  kind: ActiveAspLspStatusKind,
  label: string,
  options: AspLspProgressTaskOptions = {},
): AspLspProgressTaskHandle {
  const id = `task-${++progressTaskSequence}`;
  const startedAt = Date.now();
  const task: AspLspProgressTask = {
    id,
    kind,
    label,
    detail: options.detail,
    current: options.current,
    total: options.total,
    activeItems: options.activeItems,
    cancellable: options.cancellable === true,
    state: "running",
    startedAt,
    updatedAt: startedAt,
    cancelRequested: false,
  };
  progressTasks.set(id, task);
  incrementServerStatus(kind);
  publishServerStatus(label, true);
  let ended = false;
  const handle: AspLspProgressTaskHandle = {
    id,
    isCancellationRequested: () => task.cancelRequested,
    update(update) {
      if (ended) {
        return;
      }
      if (update.detail !== undefined) {
        task.detail = update.detail;
      }
      if (update.label !== undefined) {
        task.label = update.label;
      }
      if (update.current !== undefined) {
        task.current = update.current;
      }
      if (update.total !== undefined) {
        task.total = update.total;
      }
      if (update.activeItems !== undefined) {
        task.activeItems = update.activeItems;
      }
      task.updatedAt = Date.now();
      publishServerStatus(task.label, true);
    },
    step(detail) {
      if (ended) {
        return;
      }
      task.current = Math.max(0, (task.current ?? 0) + 1);
      if (detail !== undefined) {
        task.detail = detail;
      }
      task.updatedAt = Date.now();
      publishServerStatus(task.label, true);
    },
    end() {
      if (ended) {
        return;
      }
      ended = true;
      progressTasks.delete(id);
      decrementServerStatus(kind);
      publishServerStatus(task.label, true);
    },
  };
  return handle;
}

function incrementServerStatus(kind: ActiveAspLspStatusKind): void {
  if (kind === "loading") {
    loadingStatusDepth += 1;
  } else {
    analyzingStatusDepth += 1;
  }
}

function decrementServerStatus(kind: ActiveAspLspStatusKind): void {
  if (kind === "loading") {
    loadingStatusDepth = Math.max(0, loadingStatusDepth - 1);
  } else {
    analyzingStatusDepth = Math.max(0, analyzingStatusDepth - 1);
  }
}

function cancelProgressTask(id: string): boolean {
  const task = progressTasks.get(id);
  if (!task || !task.cancellable) {
    return false;
  }
  task.cancelRequested = true;
  task.state = "cancelling";
  task.updatedAt = Date.now();
  publishServerStatus("task.cancel", true);
  return true;
}

function progressCancellation(
  handle: AspLspProgressTaskHandle | undefined,
  parent: AnalysisCancellation = neverCancelled,
): AnalysisCancellation {
  return {
    isCancellationRequested: () =>
      parent.isCancellationRequested() || handle?.isCancellationRequested() === true,
  };
}

function tokenFromAnalysisCancellation(cancellation: AnalysisCancellation): {
  readonly isCancellationRequested: boolean;
} {
  return {
    get isCancellationRequested() {
      return cancellation.isCancellationRequested();
    },
  };
}

const aspGraphBuildService: AspGraphBuildService = createAspGraphBuildService({
  documentsAll: () => documents.all(),
  globalSettings: () => globalSettings,
  workspaceIndexValues: () => [...workspaceIndex.values()],
  workspaceIndexTruncated: () => workspaceIndexTruncated,
  defaultGraphMaxDocuments,
  defaultGraphMaxTextLength,
  defaultMaxIndexFiles,
  defaultVbProjectMaxDocuments,
  defaultVbProjectMaxTextLength,
  beginProgressTask,
  progressCancellation,
  tokenFromAnalysisCancellation,
  progressFileLabelFromUri,
  progressFileLabel,
  logDebugSummary,
  finishDebugStep,
  yieldToEventLoop,
  progressMapHooks,
  mapWithConcurrency,
  analysisConcurrency,
  includeReadConcurrency,
  cachedSettings,
  cachedDocumentForGraphAsync,
  ensureFreshCachedDocumentAsync,
  cachedFromIndexedAsync,
  graphDocumentFromCachedAsync,
  graphDocumentFromIncludeFileAsync,
  graphIncludeRefsForDocumentAsync,
  graphDocumentsNeedRelatedIncludeTreeAnalysisAsync,
  collectIncomingIncludeGraphDocumentsAsync,
  graphPayloadFromDocumentsAsync,
  graphPayloadFromDocumentSourcesAsync,
  ensureWorkspaceIndexAsync,
  workspaceAnalyzableOpenDocumentsAsync,
  graphIncludeTreeLimits,
  graphOutputLimits,
  vbProjectContextLimits,
  resolveIncludePathDetailsAsync,
  fileSizeAsync,
  statAsync: fsGateway.statAsync.bind(fsGateway),
  graphFileNameFromUri,
  graphFileKey,
  graphFileKeyFromUri,
  pathToFileUri,
  normalizeFileName,
  graphFileIndexFingerprint,
});

function publishServerStatus(reason: string, force = false): void {
  const nextKind =
    analyzingStatusDepth > 0 ? "analyzing" : loadingStatusDepth > 0 ? "loading" : "idle";
  const tasks = [...progressTasks.values()].map(progressTaskSnapshot);
  const progress = aggregateProgress(tasks);
  const payload = { status: nextKind, reason, progress, tasks };
  const payloadKey = JSON.stringify(payload);
  if (!force && nextKind === currentStatusKind && payloadKey === lastPublishedStatusPayload) {
    return;
  }
  currentStatusKind = nextKind;
  lastPublishedStatusPayload = payloadKey;
  connection.sendNotification(statusNotificationMethod, payload);
}

function progressTaskSnapshot(task: AspLspProgressTask): AspLspProgressTaskSnapshot {
  return {
    id: task.id,
    kind: task.kind,
    label: task.label,
    detail: task.detail,
    current: task.current,
    total: task.total,
    activeItems: task.activeItems,
    cancellable: task.cancellable,
    state: task.state,
    startedAt: task.startedAt,
    updatedAt: task.updatedAt,
  };
}

function aggregateProgress(
  tasks: AspLspProgressTaskSnapshot[],
): { current: number; total: number } | undefined {
  const measurable = tasks.filter(
    (task) => typeof task.current === "number" && typeof task.total === "number",
  );
  if (measurable.length === 0) {
    return undefined;
  }
  return measurable.reduce(
    (total, task) => ({
      current: total.current + (task.current ?? 0),
      total: total.total + (task.total ?? 0),
    }),
    { current: 0, total: 0 },
  );
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
  const endStatus = beginServerStatus("loading", "project.update");
  const startedAt = process.hrtime.bigint();
  try {
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
    checkMemoryPressure(globalSettings, "projectUpdate.flushed");
    return true;
  } finally {
    endStatus();
  }
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

function cacheFreshness(settings: AspSettings): "metadata" | "watch" {
  if (settings.cache?.freshness === "watch" || settings.cache?.freshness === "metadata") {
    return settings.cache.freshness;
  }
  return resolveNetworkProfile(settings).kind === "network" ? "watch" : "metadata";
}

function rememberSourceMetadata(source: DiskAnalysisSourceMetadata): void {
  sourceManifest.set(fileIdentityKeyFromFileName(source.fileName), {
    ...source,
    fileName: normalizeFileName(source.fileName),
  });
}

function sourceMetadataFromManifest(fileName: string): DiskAnalysisSourceMetadata | undefined {
  return sourceManifest.get(fileIdentityKeyFromFileName(fileName));
}

function forgetSourceMetadata(fileName: string): void {
  sourceManifest.delete(fileIdentityKeyFromFileName(fileName));
}

async function configureDiskAnalysisCacheAsync(): Promise<void> {
  await withServerStatusAsync("loading", "diskCache.configure", async () => {
    diskAnalysisCache = createDiskAnalysisCache(globalSettings);
    await diskAnalysisCache.sweep();
  });
}

function logDiskAnalysisCacheError(operation: string, error: unknown): void {
  logServerWarning(`[asp-lsp] ${operation}.failed: ${errorMessage(error)}`);
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
    const key = diagnosticKeyFast(diagnostic);
    if (seen.has(key)) {
      return false;
    }
    seen.add(key);
    return true;
  });
}

function diagnosticKeyFast(diagnostic: Diagnostic): string {
  const cached = diagnosticKeyCache.get(diagnostic);
  if (cached !== undefined) {
    return cached;
  }
  const { start, end } = diagnostic.range;
  const key = [
    diagnostic.source ?? "",
    String(diagnostic.code ?? ""),
    String(diagnostic.severity ?? ""),
    `${start.line},${start.character},${end.line},${end.character}`,
    diagnostic.message,
  ].join("\0");
  diagnosticKeyCache.set(diagnostic, key);
  return key;
}

function analysisFor(cached: CachedDocument): CachedAnalysis {
  const settings = cachedSettings(cached.source.uri);
  const identities = measureDebugStep(settings, cached.source.uri, "analysis.identity", () => ({
    diagnostics: diagnosticsIdentity(settings),
    includeResolution: includeResolutionIdentity(settings),
    jsProject: jsProjectIdentity(settings),
  }));
  const nextDiagnosticsIdentity = identities.diagnostics;
  const nextIncludeResolutionIdentity = identities.includeResolution;
  const nextJsProjectIdentity = identities.jsProject;
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
  const key = measureDebugStep(
    settings,
    cached.source.uri,
    `${stepPrefix}.javascriptSyntax.key`,
    () => jsDiagnosticsCacheKey(cached, settings),
  );
  const cachedItems = analysis.jsSyntaxDiagnostics;
  if (cachedItems?.key === key) {
    return measureDebugStep(
      settings,
      cached.source.uri,
      `${stepPrefix}.javascriptSyntax.reuse`,
      () => cachedJsDiagnosticsToLsp(cached, cachedItems),
    );
  }
  const virtuals = measureDebugStep(
    settings,
    cached.source.uri,
    `${stepPrefix}.javascriptSyntax.virtuals`,
    () => jsVirtualDocuments(cached),
  );
  const entry: CachedJsDiagnosticsEntry = {
    key,
    virtuals: virtuals.map((virtual) => {
      const sourceFile = measureDebugStep(
        settings,
        cached.source.uri,
        `${stepPrefix}.javascriptSyntax.parse`,
        () =>
          ts.createSourceFile(
            jsVirtualFileName(virtual.uri),
            virtual.text,
            ts.ScriptTarget.ESNext,
            false,
            ts.ScriptKind.JS,
          ),
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
      logServerWarning(`[asp-lsp] javascript.diagnostics.worker.failed: ${errorMessage(error)}`);
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

function scheduleDocumentOpenJsDiagnosticsPrewarm(
  cached: CachedDocument,
  settings: AspSettings,
): void {
  if (settings.checkJs !== true || !shouldUseJsDiagnosticsWorker("foreground")) {
    return;
  }
  const virtuals = jsVirtualDocuments(cached);
  if (virtuals.length === 0) {
    return;
  }
  const key = JSON.stringify({
    uri: cached.source.uri,
    version: cached.identity.version,
    diagnostics: diagnosticsIdentity(settings),
    jsProject: cached.jsProjectGeneration,
    virtuals: virtuals.map(jsDiagnosticsVirtualKey),
  });
  if (pendingJsDiagnosticsPrewarms.has(key)) {
    return;
  }
  const uri = cached.source.uri;
  const version = cached.identity.version;
  pendingJsDiagnosticsPrewarms.add(key);
  setTimeout(() => {
    void runDocumentOpenJsDiagnosticsPrewarmAsync(uri, version, key, settings).catch(
      (error: unknown) =>
        logServerWarning(
          `[asp-lsp] javascript.diagnostics.prewarm.failed: ${uri}, error=${errorMessage(error)}`,
        ),
    );
  }, semanticTokensDeferredWorkDelayMs);
}

async function runDocumentOpenJsDiagnosticsPrewarmAsync(
  uri: string,
  version: number,
  key: string,
  settings: AspSettings,
): Promise<void> {
  try {
    const document = openDocumentForUri(uri);
    if (!document || document.version !== version) {
      return;
    }
    const cached = await ensureFreshCachedDocumentAsync(document);
    if (openDocumentForUri(uri)?.version !== version) {
      return;
    }
    const virtual = jsVirtualDocuments(cached)[0];
    if (!virtual) {
      return;
    }
    const pool = getJsDiagnosticsWorkerPool(settings, "foreground");
    const id = ++jsDiagnosticsWorkerRequestId;
    const activeVirtual = jsDiagnosticsWorkerVirtualDocument(virtual);
    const activeVirtualFileName = normalizeFileName(jsVirtualFileName(activeVirtual.uri));
    const openVirtuals = (await openJsDiagnosticsWorkerVirtualDocumentsAsync()).filter(
      (openVirtual) =>
        normalizeFileName(jsVirtualFileName(openVirtual.uri)) !== activeVirtualFileName,
    );
    const response = await pool.run(
      {
        id,
        kind: "prewarm",
        activeVirtual,
        openVirtuals,
        settings,
        workspaceRoots,
        projectGeneration: jsProjectGeneration,
      },
      neverCancelled,
    );
    if (response.error) {
      throw jsWorkerResponseError(response);
    }
    for (const timing of response.timings ?? []) {
      logDebugElapsed(
        settings,
        uri,
        `document.open.jsPrewarm.${timing.name}.worker`,
        timing.elapsedMs,
      );
    }
    logDebugSummary(settings, `[asp-lsp] javascript.diagnostics.prewarm.completed: ${uri}`);
  } finally {
    pendingJsDiagnosticsPrewarms.delete(key);
  }
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
  const message = `[asp-lsp] javascript.diagnostics.worker: ${uri} ${metrics}`;
  if (isDebugVerboseEnabled(settings)) {
    connection.console.info(message);
  }
  logDebugFile(settings, "debug", "javascript.diagnostics.worker", message, {
    uri,
    queueWaitMs: response.queueWaitMs,
    runMs: response.runMs,
    payloadBytes: response.payloadBytes,
    resultBytes: response.resultBytes,
    queueLength: response.queueLengthAtDispatch,
  });
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
    parse: cached.parseSettingsIdentity,
    source: {
      uri: fileIdentityKeyFromUri(cached.identity.uri),
      version: cached.identity.version,
      generation: cached.identity.version === 0 ? cached.generation : undefined,
    },
    virtuals: jsVirtualDocuments(cached).map(jsDiagnosticsVirtualKey),
  });
}

function jsDiagnosticsVirtualKey(virtual: VirtualDocument): string {
  return JSON.stringify({
    uri: virtual.uri,
    language: virtual.languageId,
    sourceUri: virtualSourceUri(virtual),
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

async function vbFastDiagnosticsAsync(
  cached: CachedDocument,
  settings: AspSettings,
  stepPrefix: string,
  cancellation: AnalysisCancellation = neverCancelled,
  mode: AnalysisExecutionMode = "foreground",
): Promise<Diagnostic[]> {
  // Reuse the previous fast result when the VBScript regions are unchanged
  // (for example while only embedded HTML/CSS/JS is being edited), which skips
  // the per-keystroke worker dispatch. The fast path keeps its own cache slot so
  // its locally-scoped diagnostics never satisfy the full project-context path.
  const diagnosticsKey = vbDiagnosticsCacheKey(cached, settings);
  const cachedItems = cached.analysis?.vbFastDiagnostics;
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
    () => fastInteractiveVbProjectContextAsync(cached, settings),
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
  analysisFor(cached).vbFastDiagnostics = {
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
    await analyzeParsedVbscriptAsync(cached.parsed, {
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
  if (cached.parseDepth !== "skeleton" || !needsVbscriptCstHydration(cached.parsed)) {
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
      text: cached.parsed.text,
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
    deadCodeDiagnostics: context.deadCodeDiagnostics,
    syntaxSnippets: context.syntaxSnippets,
    syntaxKeywords: context.syntaxKeywords,
    builtinRuntime: context.builtinRuntime,
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
      ...previousDocuments.documents.filter(
        (document) => !sameFileIdentityUri(document.uri, previous.source.uri),
      ),
    ],
  };
  const previousSummaryGraph = previous.analysis?.vbProjectSummaryGraph;
  if (
    previousSummaryGraph &&
    previousSummaryGraph.collectionKey === vbProjectDocumentCollectionKey(previous, settings) &&
    previous.includeResolutionGeneration === cached.includeResolutionGeneration &&
    previous.workspaceGeneration === cached.workspaceGeneration
  ) {
    analysisFor(cached).vbProjectSummaryGraphSeed = {
      collectionKey: vbProjectDocumentCollectionKey(cached, settings),
      graph: previousSummaryGraph.graph,
      rootTextLength: previous.parsed.text.length,
    };
  }
}

function reuseVbDiagnosticsForIncrementalChange(
  previousEntry: DiagnosticCacheEntry | undefined,
  previous: CachedDocument,
  cached: CachedDocument,
  settings: AspSettings,
  change: AspIncrementalChange,
  impact: AspEditImpact,
): DiagnosticCacheEntry | undefined {
  if (!previousEntry) {
    return undefined;
  }
  return {
    key: vbDiagnosticsCacheKey(cached, settings),
    text: cached.parsed.text,
    items:
      impact.delta === 0
        ? previousEntry.items
        : previousEntry.items.map((diagnostic) =>
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
  const reusedDiagnostics = reuseVbDiagnosticsForIncrementalChange(
    previousDiagnostics,
    previous,
    cached,
    settings,
    change,
    impact,
  );
  if (reusedDiagnostics) {
    analysis.vbDiagnostics = reusedDiagnostics;
  }
  // Seed the interactive (projectFast) slot as well so unchanged-VBScript edits
  // skip the per-keystroke worker dispatch on the fast diagnostics layer.
  const reusedFastDiagnostics = reuseVbDiagnosticsForIncrementalChange(
    previous.analysis?.vbFastDiagnostics,
    previous,
    cached,
    settings,
    change,
    impact,
  );
  if (reusedFastDiagnostics) {
    analysis.vbFastDiagnostics = reusedFastDiagnostics;
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
  const canReuse = jsDiagnosticsCanReuseAcrossIncrementalChange(cached, impact);
  if (previousSyntax) {
    analysis.jsSyntaxDiagnostics = canReuse
      ? { ...previousSyntax, key: jsDiagnosticsCacheKey(cached, cachedSettings(cached.source.uri)) }
      : previousSyntax;
  }
  if (previousSlow) {
    analysis.jsSlowDiagnostics = canReuse
      ? { ...previousSlow, key: jsDiagnosticsCacheKey(cached, cachedSettings(cached.source.uri)) }
      : previousSlow;
  }
}

function jsDiagnosticsCanReuseAcrossIncrementalChange(
  cached: CachedDocument,
  impact: AspEditImpact,
): boolean {
  return (
    impact.kind === "incremental" &&
    (impact.language === "html" || impact.language === "css") &&
    !isClientScriptOnlyIncrementalChange(cached)
  );
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
        text: computeTextFingerprint(parsed.text.slice(region.contentStart, region.contentEnd)),
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
      location: sameFileIdentityUri(info.location.uri, rootUri)
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
      ...(context.documents?.filter((document) => !sameFileIdentityUri(document.uri, rootUri)) ??
        []),
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
      ...(context.documents?.filter((document) => !sameFileIdentityUri(document.uri, rootUri)) ??
        []),
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
  if (!sameFileIdentityUri(symbol.sourceUri, rootUri)) {
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
  const service = ts.createLanguageService(host, jsDocumentRegistry);
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
  const sourceUri = virtualSourceUri(virtual);
  const open = openDocumentForUri(sourceUri);
  return JSON.stringify({
    uri: virtual.uri,
    language: virtual.languageId,
    sourceUri,
    sourceVersion: open
      ? {
          uri: fileIdentityKeyFromUri(sourceUri),
          version: open.version,
        }
      : undefined,
    text: open ? undefined : textFingerprint(virtual.text),
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
    unresolvedVbscriptCompletions: settings.vbscript?.showUnresolvedSymbolsInCompletion === true,
    javascript: settings.javascript,
    checkJs: settings.checkJs,
    locale: settings.resolvedLocale,
  });
}

function completionPrefixAt(text: string, offset: number): string {
  const prefix = /[A-Za-z0-9_$]*$/.exec(text.slice(0, offset));
  return prefix?.[0] ?? "";
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

function completionTriggerCharacter(params: TextDocumentPositionParams): string | undefined {
  const context = (
    params as TextDocumentPositionParams & {
      context?: { triggerKind?: number; triggerCharacter?: string };
    }
  ).context;
  return context?.triggerKind === completionTriggerKindTriggerCharacter
    ? context.triggerCharacter
    : undefined;
}

function isCssOnlyCompletionTrigger(triggerCharacter: string | undefined): boolean {
  return triggerCharacter === " " || triggerCharacter === ";";
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
  const items = cssService
    .doComplete(document, position, stylesheet)
    .items.map((item) => remapCompletionItem(virtual, item))
    .filter((item): item is CompletionItem => Boolean(item));
  const baseItems =
    items.length > 0
      ? items
      : cssStyleAttributeSemicolonCompletions(cached, params, virtual, position);
  return withAdditionalCompletionItems(
    baseItems,
    cssSelectorCompletionsFromHtml(cached, params.position),
  );
}

function cssStyleAttributeSemicolonCompletions(
  cached: CachedDocument,
  params: TextDocumentPositionParams,
  virtual: VirtualDocument,
  position: Position,
): CompletionItem[] {
  const sourceOffset = cached.source.offsetAt(params.position);
  const region = findRegionAt(cached.parsed, sourceOffset);
  if (
    region?.kind !== "style-attribute" ||
    sourceOffset !== region.contentEnd ||
    cached.source.getText()[sourceOffset - 1] !== ";"
  ) {
    return [];
  }
  const virtualOffset = offsetAtText(virtual.text, position);
  const syntheticText = `${virtual.text.slice(0, virtualOffset)} ${virtual.text.slice(virtualOffset)}`;
  const syntheticDocument = TextDocument.create(virtual.uri, virtual.languageId, 0, syntheticText);
  const syntheticPosition = syntheticDocument.positionAt(virtualOffset + 1);
  const sourceRange = { start: params.position, end: params.position };
  return cssService
    .doComplete(syntheticDocument, syntheticPosition, cssService.parseStylesheet(syntheticDocument))
    .items.map((item) => completionItemAtSourceRange(item, sourceRange, " "));
}

function withAdditionalCompletionItems(
  items: CompletionItem[],
  additions: CompletionItem[],
): CompletionItem[] {
  if (additions.length === 0) {
    return items;
  }
  const labels = new Set(items.map((item) => completionLabelKey(item.label)));
  const filtered = additions.filter((item) => {
    const key = completionLabelKey(item.label);
    if (labels.has(key)) {
      return false;
    }
    labels.add(key);
    return true;
  });
  return filtered.length > 0 ? [...items, ...filtered] : items;
}

function completionLabelKey(label: CompletionItem["label"]): string {
  return (typeof label === "string" ? label : String(label)).toLowerCase();
}

interface CssSelectorCompletionContext {
  kind: "class" | "id";
  range: Range;
}

function cssSelectorCompletionsFromHtml(
  cached: CachedDocument,
  position: Position,
): CompletionItem[] {
  const context = cssSelectorCompletionContextAt(cached, position);
  if (!context) {
    return [];
  }
  const names = htmlAttributeNameValues(
    cached.source.getText(),
    context.kind === "class" ? "class" : "id",
  );
  const localizer = localizerForUri(cached.source.uri);
  return names.map((name, index) => ({
    label: name,
    kind: CompletionItemKind.Value,
    detail: localizer.t("server.completion.cssSelectorFromHtml.detail"),
    documentation: localizer.t("server.completion.cssSelectorFromHtml.documentation"),
    filterText: `${context.kind === "class" ? "." : "#"}${name} ${name}`,
    textEdit: { range: context.range, newText: name },
    sortText: `9_html-selector-${String(index).padStart(4, "0")}-${name.toLowerCase()}`,
  }));
}

function cssSelectorCompletionContextAt(
  cached: CachedDocument,
  position: Position,
): CssSelectorCompletionContext | undefined {
  const offset = cached.source.offsetAt(position);
  const region = findRegionAt(cached.parsed, offset);
  if (!region || region.language !== "css" || region.kind === "style-attribute") {
    return undefined;
  }
  const text = cached.source.getText();
  const before = text.slice(region.contentStart, offset);
  if (before.lastIndexOf("{") > before.lastIndexOf("}")) {
    return undefined;
  }
  let nameStart = offset;
  while (nameStart > region.contentStart && isHtmlCssNamePartCode(text.charCodeAt(nameStart - 1))) {
    nameStart -= 1;
  }
  const marker = text[nameStart - 1];
  if ((marker !== "." && marker !== "#") || nameStart <= region.contentStart) {
    return undefined;
  }
  return {
    kind: marker === "." ? "class" : "id",
    range: {
      start: cached.source.positionAt(nameStart),
      end: position,
    },
  };
}

interface HtmlClassIdAttributeCompletionContext {
  kind: "class" | "id";
  range: Range;
  existingClassNames: Set<string>;
}

function htmlClassIdAttributeCompletions(
  cached: CachedDocument,
  position: Position,
): CompletionItem[] {
  const context = htmlClassIdAttributeCompletionContextAt(cached, position);
  if (!context) {
    return [];
  }
  const cssNames = cssSelectorNameIndex(cached);
  const names =
    context.kind === "class"
      ? uniqueCompletionNames([
          ...cssNames.classes,
          ...htmlAttributeNameValues(cached.source.getText(), "class"),
        ]).filter((name) => !context.existingClassNames.has(name.toLowerCase()))
      : cssNames.ids;
  const localizer = localizerForUri(cached.source.uri);
  const detailKey =
    context.kind === "class"
      ? "server.completion.htmlClassValue.detail"
      : "server.completion.htmlIdValue.detail";
  const documentationKey =
    context.kind === "class"
      ? "server.completion.htmlClassValue.documentation"
      : "server.completion.htmlIdValue.documentation";
  return names.map((name, index) => ({
    label: name,
    kind: CompletionItemKind.Value,
    detail: localizer.t(detailKey),
    documentation: localizer.t(documentationKey),
    textEdit: { range: context.range, newText: name },
    sortText: `9_html-attribute-${context.kind}-${String(index).padStart(4, "0")}-${name.toLowerCase()}`,
  }));
}

function htmlClassIdAttributeCompletionContextAt(
  cached: CachedDocument,
  position: Position,
): HtmlClassIdAttributeCompletionContext | undefined {
  const offset = cached.source.offsetAt(position);
  const region = findRegionAt(cached.parsed, offset);
  if (!region || region.language !== "html") {
    return undefined;
  }
  const text = cached.source.getText();
  for (const tag of htmlStartTags(text)) {
    if (offset < tag.start || offset > tag.end) {
      continue;
    }
    for (const attribute of tag.attributes) {
      const name = attribute.name.toLowerCase();
      if (
        (name !== "class" && name !== "id") ||
        attribute.value === true ||
        offset < attribute.valueStart ||
        offset > attribute.valueEnd
      ) {
        continue;
      }
      if (name === "id") {
        return {
          kind: "id",
          range: {
            start: cached.source.positionAt(attribute.valueStart),
            end: cached.source.positionAt(attribute.valueEnd),
          },
          existingClassNames: new Set(),
        };
      }
      const tokenRange = htmlClassAttributeTokenRange(text, attribute, offset);
      return {
        kind: "class",
        range: {
          start: cached.source.positionAt(tokenRange.start),
          end: cached.source.positionAt(tokenRange.end),
        },
        existingClassNames: new Set(
          htmlClassAttributeNames(attribute.value).map((value) => value.toLowerCase()),
        ),
      };
    }
  }
  return undefined;
}

function htmlClassAttributeTokenRange(
  text: string,
  attribute: HtmlAttributeSpan,
  offset: number,
): { start: number; end: number } {
  let start = offset;
  while (start > attribute.valueStart && !isHtmlWhitespaceCode(text.charCodeAt(start - 1))) {
    start -= 1;
  }
  let end = offset;
  while (end < attribute.valueEnd && !isHtmlWhitespaceCode(text.charCodeAt(end))) {
    end += 1;
  }
  return { start, end };
}

interface CssSelectorNameIndex {
  classes: string[];
  ids: string[];
}

function cssSelectorNameIndex(cached: CachedDocument): CssSelectorNameIndex {
  const context = cssContext(cached);
  if (!context) {
    return { classes: [], ids: [] };
  }
  const classes: string[] = [];
  const ids: string[] = [];
  const seenClasses = new Set<string>();
  const seenIds = new Set<string>();
  for (const symbol of cssService.findDocumentSymbols(context.document, context.stylesheet)) {
    for (const match of symbol.name.matchAll(/([#.])([A-Za-z_][A-Za-z0-9_-]*)/g)) {
      const name = match[2] ?? "";
      if (!isHtmlCssCompletionName(name)) {
        continue;
      }
      const seen = match[1] === "." ? seenClasses : seenIds;
      const values = match[1] === "." ? classes : ids;
      addUniqueCompletionName(values, seen, name);
    }
  }
  return { classes, ids };
}

function htmlAttributeNameValues(text: string, attributeName: "class" | "id"): string[] {
  const values: string[] = [];
  const seen = new Set<string>();
  for (const tag of htmlStartTags(text)) {
    const attribute = htmlAttributeByName(tag, attributeName);
    if (!attribute || attribute.value === true) {
      continue;
    }
    const names =
      attributeName === "class" ? htmlClassAttributeNames(attribute.value) : [attribute.value];
    for (const name of names) {
      addUniqueCompletionName(values, seen, name);
    }
  }
  return values;
}

function htmlClassAttributeNames(value: string): string[] {
  return value.split(/\s+/).filter(isHtmlCssCompletionName);
}

function uniqueCompletionNames(names: string[]): string[] {
  const values: string[] = [];
  const seen = new Set<string>();
  for (const name of names) {
    addUniqueCompletionName(values, seen, name);
  }
  return values;
}

function addUniqueCompletionName(values: string[], seen: Set<string>, name: string): void {
  if (!isHtmlCssCompletionName(name)) {
    return;
  }
  const key = name.toLowerCase();
  if (seen.has(key)) {
    return;
  }
  seen.add(key);
  values.push(name);
}

function isHtmlCssCompletionName(value: string): boolean {
  return /^[A-Za-z_][A-Za-z0-9_-]*$/.test(value);
}

function isHtmlCssNamePartCode(code: number): boolean {
  return isAsciiAlphaCode(code) || isAsciiDigitCode(code) || code === 45 || code === 95;
}

function withUnresolvedVbscriptCompletionItems(
  cached: CachedDocument,
  settings: AspSettings,
  context: VbProjectContext,
  position: Position,
  items: CompletionItem[],
): CompletionItem[] {
  if (settings.vbscript?.showUnresolvedSymbolsInCompletion !== true) {
    return items;
  }
  const additions = unresolvedVbscriptCompletionItems(cached, settings, context, position, items);
  return additions.length > 0 ? [...items, ...additions] : items;
}

function unresolvedVbscriptCompletionItems(
  cached: CachedDocument,
  settings: AspSettings,
  context: VbProjectContext,
  position: Position,
  existingItems: CompletionItem[],
): CompletionItem[] {
  const text = cached.source.getText();
  const key = textFingerprint(text);
  const analysis = analysisFor(cached);
  let index =
    analysis.unresolvedVbscriptCompletionIndex?.key === key
      ? analysis.unresolvedVbscriptCompletionIndex.index
      : undefined;
  if (!index) {
    index = measureDebugStep(
      settings,
      cached.source.uri,
      "completion.unresolvedSymbols.extract",
      () =>
        extractVbscriptSymbolIndex(cached.source.uri, text, settings, {
          includeImplicitVariables: true,
        }),
    );
    analysis.unresolvedVbscriptCompletionIndex = { key, index };
  }
  const existingNames = new Set(existingItems.map((item) => item.label.toLowerCase()));
  const visibleNames = visibleVbscriptCompletionSymbolNames(cached, context);
  const externalNames = new Set(
    getVbscriptGraphExternalSymbols(
      settings,
      context.builtinRuntime ?? vbBuiltinRuntimeForCached(cached),
    ).map((symbol) => symbol.name.toLowerCase()),
  );
  const items: CompletionItem[] = [];
  const localizer = createLocalizer(settings.resolvedLocale);
  const add = (item: CompletionItem): void => {
    const key = item.label.toLowerCase();
    if (existingNames.has(key) || visibleNames.has(key) || externalNames.has(key)) {
      return;
    }
    existingNames.add(key);
    items.push(item);
  };
  for (const declaration of index.declarations) {
    if (
      declaration.kind !== "variable" ||
      declaration.implicitGlobalCandidate !== true ||
      rangeContainsPosition(declaration.nameRange, position)
    ) {
      continue;
    }
    add({
      label: declaration.name,
      kind: CompletionItemKind.Variable,
      detail: localizer.t("vb.completion.implicitGlobalVariable"),
      sortText: `90-implicit-global-${declaration.normalizedName}`,
    });
  }
  for (const callSite of index.callSites) {
    if (
      callSite.resolvedId ||
      callSite.memberName ||
      !isCallableUnresolvedRole(callSite.callKind) ||
      rangeContainsPosition(callSite.range, position)
    ) {
      continue;
    }
    add({
      label: callSite.name,
      kind: CompletionItemKind.Function,
      detail: localizer.t("vb.completion.unresolvedFunction"),
      sortText: `91-unresolved-call-${callSite.normalizedName}`,
    });
  }
  return items;
}

function rangeContainsPosition(range: Range, position: Position): boolean {
  return comparePositions(range.start, position) <= 0 && comparePositions(position, range.end) <= 0;
}

function visibleVbscriptCompletionSymbolNames(
  cached: CachedDocument,
  context: VbProjectContext,
): Set<string> {
  const names = new Set<string>();
  for (const symbol of context.symbols ?? []) {
    if (
      sameFileIdentityUri(symbol.sourceUri, cached.parsed.uri) ||
      (!symbol.scopeName && !symbol.memberOf)
    ) {
      names.add(symbol.name.toLowerCase());
    }
  }
  return names;
}

function completionItemAtSourceRange(
  item: CompletionItem,
  range: Range,
  newTextPrefix = "",
): CompletionItem {
  if (!item.textEdit) {
    return {
      ...item,
      insertText:
        newTextPrefix && typeof item.insertText === "string"
          ? `${newTextPrefix}${item.insertText}`
          : item.insertText,
      additionalTextEdits: undefined,
    };
  }
  const textEdit =
    "range" in item.textEdit
      ? { ...item.textEdit, range }
      : { ...item.textEdit, insert: range, replace: range };
  return {
    ...item,
    textEdit: newTextPrefix
      ? { ...textEdit, newText: `${newTextPrefix}${textEdit.newText}` }
      : textEdit,
    additionalTextEdits: undefined,
  };
}

type AspIncludeCompletionContextKind = "html" | "comment" | "includeMode";

interface AspIncludeCompletionContext {
  kind: AspIncludeCompletionContextKind;
  prefix: string;
  replaceStart: number;
}

function aspIncludeCompletions(
  cached: CachedDocument,
  position: Position,
  settings: AspSettings,
): CompletionItem[] {
  const offset = cached.source.offsetAt(position);
  const context = aspIncludeCompletionContextAt(cached.source.getText(), offset);
  if (!context) {
    return [];
  }
  const range = {
    start: cached.source.positionAt(context.replaceStart),
    end: position,
  };
  const localizer = createLocalizer(settings.resolvedLocale);
  const detail = localizer.t("server.completion.include.detail");
  const documentation = localizer.t("server.completion.include.documentation");
  if (context.kind === "includeMode") {
    return ["file", "virtual"].map((mode) => ({
      label: mode,
      kind: CompletionItemKind.Property,
      detail,
      documentation,
      insertText: `${mode}="\${1:path}"`,
      textEdit: { range, newText: `${mode}="\${1:path}"` },
      insertTextFormat: InsertTextFormat.Snippet,
      sortText: `0_${mode}`,
    }));
  }
  const prefix = context.kind === "comment" ? "" : "<!-- ";
  const snippets: CompletionItem[] = [
    {
      label: "#include file",
      kind: CompletionItemKind.Snippet,
      detail,
      documentation,
      insertText: `${prefix}#include file="\${1:path}" -->`,
      textEdit: { range, newText: `${prefix}#include file="\${1:path}" -->` },
      insertTextFormat: InsertTextFormat.Snippet,
      filterText: includeSnippetFilterText("file", context.prefix),
      sortText: "0_include_file",
    },
    {
      label: "#include virtual",
      kind: CompletionItemKind.Snippet,
      detail,
      documentation,
      insertText: `${prefix}#include virtual="\${1:path}" -->`,
      textEdit: { range, newText: `${prefix}#include virtual="\${1:path}" -->` },
      insertTextFormat: InsertTextFormat.Snippet,
      filterText: includeSnippetFilterText("virtual", context.prefix),
      sortText: "0_include_virtual",
    },
  ];
  return context.kind === "comment"
    ? [
        {
          label: "#include",
          kind: CompletionItemKind.Keyword,
          detail,
          documentation,
          insertText: "#include ",
          textEdit: { range, newText: "#include " },
          filterText: includeKeywordFilterText(context.prefix),
          sortText: "0_include",
        },
        ...snippets,
      ]
    : snippets;
}

function aspDirectiveOpenCompletions(
  cached: CachedDocument,
  position: Position,
  region?: AspRegion,
): CompletionItem[] {
  const offset = cached.source.offsetAt(position);
  const text = cached.source.getText();
  const searchStart = Math.max(region?.start ?? 0, offset - 64);
  const lastOpen = text.lastIndexOf("<", Math.max(0, offset - 1));
  const start = lastOpen >= searchStart ? lastOpen : Math.max(0, offset - 3);
  const prefix = text.slice(start, offset);
  const match = /^<%@?[A-Za-z]*$/i.exec(prefix);
  if (!match) {
    return [];
  }
  const range = {
    start: cached.source.positionAt(start),
    end: position,
  };
  return [
    {
      label: '<%@ Language="VBScript" CodePage=65001 %>',
      kind: CompletionItemKind.Snippet,
      detail: "Classic ASP page directive",
      documentation: "Inserts a Classic ASP page directive with language and code page.",
      insertText: '<%@ Language="${1:VBScript}" CodePage=${2:65001} %>',
      textEdit: {
        range,
        newText: '<%@ Language="${1:VBScript}" CodePage=${2:65001} %>',
      },
      insertTextFormat: InsertTextFormat.Snippet,
      filterText: "asp directive language codepage page <%@",
      sortText: "0_asp_directive_page",
    },
  ];
}

function aspDirectiveCompletions(
  cached: CachedDocument,
  position: Position,
  region: AspRegion,
): CompletionItem[] {
  const offset = cached.source.offsetAt(position);
  if (offset < region.contentStart || offset > region.contentEnd) {
    return [];
  }
  const text = cached.source.getText();
  const valueContext = aspDirectiveValueContextAt(text, region, offset);
  if (valueContext) {
    return aspDirectiveValueCompletions(cached, position, valueContext);
  }
  const range = aspDirectiveWordRange(cached, offset);
  return aspDirectiveAttributeNames().map((name, index) => ({
    label: name,
    kind: CompletionItemKind.Property,
    detail: "Classic ASP directive attribute",
    textEdit: { range, newText: name },
    sortText: `1_${String(index).padStart(2, "0")}_${name}`,
  }));
}

function aspDirectiveValueCompletions(
  cached: CachedDocument,
  position: Position,
  context: AspDirectiveValueCompletionContext,
): CompletionItem[] {
  const values = aspDirectiveValues(context.attribute);
  const range = {
    start: cached.source.positionAt(context.replaceStart),
    end: position,
  };
  return values.map((value, index) => ({
    label: value,
    kind: /^\d+$/.test(value) ? CompletionItemKind.Value : CompletionItemKind.Constant,
    detail: `${context.attribute} value`,
    textEdit: { range, newText: value },
    sortText: `0_${String(index).padStart(2, "0")}_${value}`,
  }));
}

interface AspDirectiveValueCompletionContext {
  attribute: string;
  replaceStart: number;
}

function aspDirectiveValueContextAt(
  text: string,
  region: AspRegion,
  offset: number,
): AspDirectiveValueCompletionContext | undefined {
  const before = text.slice(region.contentStart, offset);
  const match = /([A-Za-z][A-Za-z0-9]*)\s*=\s*(?:"[^"]*|'[^']*|[^\s%>]*)$/i.exec(before);
  if (!match) {
    return undefined;
  }
  const attribute = aspDirectiveAttributeNames().find(
    (name) => name.toLowerCase() === match[1].toLowerCase(),
  );
  if (!attribute) {
    return undefined;
  }
  const equals = before.lastIndexOf("=");
  const valuePrefixStart = region.contentStart + equals + 1;
  let replaceStart = valuePrefixStart;
  while (replaceStart < offset && /\s/.test(text[replaceStart])) {
    replaceStart += 1;
  }
  if (text[replaceStart] === '"' || text[replaceStart] === "'") {
    replaceStart += 1;
  }
  return { attribute, replaceStart };
}

function aspDirectiveWordRange(cached: CachedDocument, offset: number): Range {
  const text = cached.source.getText();
  let start = offset;
  while (start > 0 && /[A-Za-z0-9_]/.test(text[start - 1])) {
    start -= 1;
  }
  return {
    start: cached.source.positionAt(start),
    end: cached.source.positionAt(offset),
  };
}

function aspDirectiveAttributeNames(): string[] {
  return ["Language", "CodePage", "LCID", "Transaction", "EnableSessionState"];
}

function aspDirectiveValues(attribute: string): string[] {
  switch (attribute.toLowerCase()) {
    case "language":
      return ["VBScript", "JScript", "JavaScript"];
    case "codepage":
      return ["65001", "932", "1252"];
    case "lcid":
      return ["1041", "1033"];
    case "transaction":
      return ["Required", "Requires_New", "Supported", "Not_Supported"];
    case "enablesessionstate":
      return ["True", "False"];
    default:
      return [];
  }
}

function includeKeywordFilterText(prefix: string): string {
  return prefix.startsWith("#") ? "#include include inc" : "include inc #include";
}

function includeSnippetFilterText(mode: "file" | "virtual", prefix: string): string {
  return prefix.startsWith("#")
    ? `#include ${mode} include ${mode} inc`
    : `include ${mode} inc #include ${mode}`;
}

function aspIncludeCompletionContextAt(
  text: string,
  offset: number,
): AspIncludeCompletionContext | undefined {
  const before = text.slice(0, offset);
  const commentStart = Math.max(before.lastIndexOf("<!--"), before.lastIndexOf("<!—"));
  if (commentStart >= 0 && before.lastIndexOf("-->") < commentStart) {
    const bodyStart = commentStart + (before.startsWith("<!—", commentStart) ? 3 : 4);
    const body = before.slice(bodyStart);
    const modeMatch = /^(\s*#include\s+)([A-Za-z]*)$/i.exec(body);
    if (modeMatch) {
      return {
        kind: "includeMode",
        prefix: modeMatch[2],
        replaceStart: bodyStart + modeMatch[1].length,
      };
    }
    const includeMatch = /^(\s*)(#?[A-Za-z]*)$/i.exec(body);
    if (includeMatch) {
      return {
        kind: "comment",
        prefix: includeMatch[2],
        replaceStart: bodyStart + includeMatch[1].length,
      };
    }
    return undefined;
  }
  if (isHtmlTextCompletionContext(text, offset)) {
    const replaceStart = htmlTextCompletionReplaceStart(text, offset);
    return { kind: "html", prefix: text.slice(replaceStart, offset), replaceStart };
  }
  return undefined;
}

function htmlTextCompletionReplaceStart(text: string, offset: number): number {
  let start = offset;
  while (start > 0 && /[#A-Za-z]/.test(text[start - 1])) {
    start -= 1;
  }
  return start;
}

function isHtmlTextCompletionContext(text: string, offset: number): boolean {
  const lineStart =
    Math.max(text.lastIndexOf("\n", offset - 1), text.lastIndexOf("\r", offset - 1)) + 1;
  const prefix = text.slice(lineStart, offset);
  const lastOpen = prefix.lastIndexOf("<");
  const lastClose = prefix.lastIndexOf(">");
  return lastOpen <= lastClose;
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
  const settings = cachedSettings(cached.source.uri);
  const context = await measureDebugStepAsync(
    settings,
    cached.source.uri,
    "hover.javascript.context",
    () => jsContextAtAsync(cached, position),
  );
  if (!context) {
    return null;
  }
  const quickInfo = measureDebugStep(
    settings,
    cached.source.uri,
    "hover.javascript.quickInfo",
    () => context.service.getQuickInfoAtPosition(context.fileName, context.offset),
  );
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
  scope: "document" | "workspace",
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
    if (scope === "document" && !sameFileIdentityUri(mapped.uri, cached.source.uri)) {
      continue;
    }
    changes[mapped.uri] = [
      ...(changes[mapped.uri] ?? []),
      { range: mapped.range, newText: newName },
    ];
  }
  return Object.keys(changes).length > 0 ? { changes } : null;
}

function renameSymbolScope(settings: AspSettings): "document" | "workspace" {
  return settings.rename?.workspaceSymbolRename === true ? "workspace" : "document";
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
  scope: "document" | "workspace" = "workspace",
): Promise<WorkspaceEdit | undefined> {
  const target = crossLanguageRenameTarget(cached, position);
  if (!target || !/^[A-Za-z_][A-Za-z0-9_-]*$/.test(newName)) {
    return undefined;
  }
  const changes: NonNullable<WorkspaceEdit["changes"]> = {};
  const seen = new Set<string>();
  for (const candidate of await crossLanguageRenameCandidates(cached, scope)) {
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

async function crossLanguageRenameCandidates(
  active: CachedDocument,
  scope: "document" | "workspace" = "workspace",
): Promise<CachedDocument[]> {
  if (scope === "document") {
    return [active];
  }
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
        sameFileIdentityUri(symbol.sourceUri, cached.source.uri) &&
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
  const context = withSourceUriFormatter(
    await fastInteractiveVbProjectContextAsync(cached, settings),
  );
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
  return (symbolsByLowerName(context.symbols ?? []).get(lower) ?? []).find(
    (symbol) =>
      sameFileIdentityUri(symbol.sourceUri, cached.parsed.uri) ||
      (!symbol.scopeName && !symbol.memberOf),
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
  const symbol = (symbolsByLowerName(context.symbols ?? []).get(owner.toLowerCase()) ?? []).find(
    (candidate) =>
      sameFileIdentityUri(candidate.sourceUri, cached.parsed.uri) ||
      (!candidate.scopeName && !candidate.memberOf),
  );
  const typeName = symbol?.type?.name ?? symbol?.typeName;
  const typeEnvironment = context.typeEnvironment ?? buildVbTypeEnvironment(cached.parsed, context);
  const type = typeName
    ? typesByLowerName(typeEnvironment.types).get(typeName.toLowerCase())
    : undefined;
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

function symbolsByLowerName(symbols: VbSymbol[]): Map<string, VbSymbol[]> {
  const cached = symbolsByLowerNameCache.get(symbols);
  if (cached) {
    return cached;
  }
  const index = new Map<string, VbSymbol[]>();
  for (const symbol of symbols) {
    pushAspGraphMapItem(index, symbol.name.toLowerCase(), symbol);
  }
  symbolsByLowerNameCache.set(symbols, index);
  return index;
}

function typesByLowerName(types: VbType[]): Map<string, VbType> {
  const cached = typeEnvironmentTypesByLowerNameCache.get(types);
  if (cached) {
    return cached;
  }
  const index = new Map<string, VbType>();
  for (const type of types) {
    if (!index.has(type.name.toLowerCase())) {
      index.set(type.name.toLowerCase(), type);
    }
  }
  typeEnvironmentTypesByLowerNameCache.set(types, index);
  return index;
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
  const settings = cachedSettings(cached.source.uri);
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
  const project = await measureDebugStepAsync(
    settings,
    cached.source.uri,
    "javascript.context.languageService",
    () => createJsLanguageServiceAsync(virtual, settings),
  );
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
    .filter(
      (symbol) => sameFileIdentityUri(symbol.sourceUri, cached.source.uri) && symbol.scopeRange,
    )
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
  const contextSettings = withCachedVbBuiltinRuntime(cached, vbProjectContextSettings(settings));
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
  const contextSettings = withCachedVbBuiltinRuntime(cached, vbProjectContextSettings(settings));
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
      sameFileIdentityUri(document.uri, cached.source.uri)
        ? cachedFileAnalysisSummaryAsync(cached, contextSettings, settings)
        : summarizeAspFileAnalysisAsync(
            document,
            withUriVbBuiltinRuntime(document.uri, vbProjectContextSettings(settings)),
          ),
    ),
  );
  let symbols = summaries.flatMap((summary) => summary.vbscript?.localSymbols ?? []);
  symbols = await canonicalizeImplicitGlobalContextSymbolsAsync(summaries, symbols, settings);
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
    externalRefUsages: summaries.flatMap((summary) => summaryVbReferenceUsages(summary)),
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

async function fastInteractiveVbProjectContextAsync(
  cached: CachedDocument,
  settings: AspSettings,
): Promise<VbProjectContext> {
  return (
    (await fastInteractiveVbProjectContextLookupAsync(cached, settings))?.context ??
    (await buildImmediateLocalVbProjectContextAsync(cached, settings))
  );
}

interface SemanticTokensVbProjectContextState {
  context: VbProjectContext;
  cacheFull: boolean;
}

async function semanticTokensVbProjectContextAsync(
  cached: CachedDocument,
  settings: AspSettings,
): Promise<SemanticTokensVbProjectContextState> {
  const exact = cachedVbProjectContextLookup(cached, settings);
  if (exact) {
    rememberInteractiveVbProjectContextSnapshot(cached, settings, exact.key, exact.context);
    return { context: exact.context, cacheFull: true };
  }
  if (cached.parsed.includes.length > 0) {
    const stale = await staleInteractiveVbProjectContextLookupAsync(cached, settings);
    if (stale) {
      scheduleInteractiveVbProjectContextRefresh(cached, settings, "semanticTokens.context.stale");
      return { context: stale.context, cacheFull: false };
    }
    scheduleInteractiveVbProjectContextRefresh(cached, settings, "semanticTokens.context.local");
    return {
      context: await buildImmediateLocalVbProjectContextAsync(cached, settings),
      cacheFull: false,
    };
  }
  return {
    context: await buildImmediateLocalVbProjectContextAsync(cached, settings),
    cacheFull: true,
  };
}

async function interactiveVbProjectContextLookupAsync(
  cached: CachedDocument,
  settings: AspSettings,
): Promise<CachedVbProjectContextLookup | undefined> {
  const exact = cachedVbProjectContextLookup(cached, settings);
  if (exact) {
    rememberInteractiveVbProjectContextSnapshot(cached, settings, exact.key, exact.context);
    return exact;
  }
  if (cached.parsed.includes.length === 0) {
    return undefined;
  }
  const stale = await staleInteractiveVbProjectContextLookupAsync(cached, settings);
  if (stale) {
    scheduleInteractiveVbProjectContextRefresh(cached, settings, "vbProject.context.stale");
    return stale;
  }
  const built = await summaryBackedVbProjectContextLookupAsync(cached, settings, {
    allowReadMissing: true,
  });
  if (built) {
    rememberInteractiveVbProjectContextSnapshot(cached, settings, built.key, built.context);
  }
  return built;
}

async function fastInteractiveVbProjectContextLookupAsync(
  cached: CachedDocument,
  settings: AspSettings,
): Promise<CachedVbProjectContextLookup | undefined> {
  const exact = cachedVbProjectContextLookup(cached, settings);
  if (exact) {
    rememberInteractiveVbProjectContextSnapshot(cached, settings, exact.key, exact.context);
    return exact;
  }
  if (cached.parsed.includes.length === 0) {
    return undefined;
  }
  const stale = await staleInteractiveVbProjectContextLookupAsync(cached, settings);
  if (stale) {
    scheduleInteractiveVbProjectContextRefresh(cached, settings, "vbProject.context.stale");
    return stale;
  }
  scheduleInteractiveVbProjectContextRefresh(cached, settings, "vbProject.context.local");
  return undefined;
}

async function staleInteractiveVbProjectContextLookupAsync(
  cached: CachedDocument,
  settings: AspSettings,
): Promise<CachedVbProjectContextLookup | undefined> {
  const familyKey = interactiveVbProjectContextFamilyKey(cached, settings);
  const snapshot = interactiveVbProjectContextSnapshots.get(familyKey);
  if (!snapshot) {
    return undefined;
  }
  snapshot.lastUsed = Date.now();
  const context = await refreshInteractiveVbProjectContextSnapshotRootAsync(
    snapshot.context,
    cached,
    settings,
  );
  logDebugSummary(
    settings,
    `[asp-lsp] vbProject.context.stale: ${cached.source.uri}, key=${snapshot.key}`,
  );
  return {
    key: `stale:${snapshot.key}:${cached.identity.version}`,
    context: { ...context, locale: settings.resolvedLocale },
  };
}

async function refreshInteractiveVbProjectContextSnapshotRootAsync(
  context: VbProjectContext,
  cached: CachedDocument,
  settings: AspSettings,
): Promise<VbProjectContext> {
  await hydrateCachedVbscriptCstAsync(cached, settings, "analysis");
  const contextSettings = withCachedVbBuiltinRuntime(cached, vbProjectContextSettings(settings));
  const rootSymbols = await collectVbscriptSymbolsAsync(cached.parsed, contextSettings);
  const includeSymbols = (context.symbols ?? []).filter(
    (symbol) => !sameFileIdentityUri(symbol.sourceUri, cached.source.uri),
  );
  const symbols = [
    ...rootSymbols,
    ...includeSymbols,
    ...configuredVbscriptGlobals(cached, settings),
  ];
  const rootTypeEnvironment = buildVbTypeEnvironment(cached.parsed, {
    ...contextSettings,
    symbols,
  });
  return {
    ...context,
    documents: [
      cached.parsed,
      ...(context.documents?.filter(
        (document) => !sameFileIdentityUri(document.uri, cached.source.uri),
      ) ?? []),
    ],
    symbols,
    typeEnvironment: mergeVbTypeEnvironment(
      rootTypeEnvironment,
      context.typeEnvironment?.types ?? [],
      symbols,
    ),
    ...contextSettings,
  };
}

function scheduleInteractiveVbProjectContextRefresh(
  cached: CachedDocument,
  settings: AspSettings,
  reason: string,
): void {
  const familyKey = interactiveVbProjectContextFamilyKey(cached, settings);
  if (pendingInteractiveVbProjectContextRefreshes.has(familyKey)) {
    return;
  }
  const uri = cached.source.uri;
  const version = cached.identity.version;
  const promise = new Promise<void>((resolve) => {
    setTimeout(() => {
      void buildVbProjectContextAsync(cached, settings, { allowReadMissing: true })
        .then((context) => {
          const current = openDocumentForUri(uri);
          if (current?.version !== version) {
            return;
          }
          const key =
            cached.analysis?.vbProjectContext?.key ??
            vbProjectContextCacheKey(context.documents ?? [cached.parsed], settings);
          rememberInteractiveVbProjectContextSnapshot(cached, settings, key, context);
          requestVisualRefresh(reason);
          if (reason !== "document.open.prewarm") {
            validate(current);
          }
          logDebugSummary(
            settings,
            `[asp-lsp] vbProject.context.refresh.completed: ${uri}, reason=${reason}`,
          );
        })
        .catch((error: unknown) =>
          logServerWarning(
            `[asp-lsp] vbProject.context.refresh.failed: ${uri}, error=${errorMessage(error)}`,
          ),
        )
        .finally(resolve);
    }, semanticTokensDeferredWorkDelayMs);
  }).finally(() => {
    pendingInteractiveVbProjectContextRefreshes.delete(familyKey);
  });
  pendingInteractiveVbProjectContextRefreshes.set(familyKey, promise);
}

function scheduleDocumentOpenVbProjectContextPrewarm(
  cached: CachedDocument,
  settings: AspSettings,
): void {
  if (cached.parsed.includes.length === 0) {
    return;
  }
  scheduleInteractiveVbProjectContextRefresh(cached, settings, "document.open.prewarm");
}

function rememberInteractiveVbProjectContextSnapshot(
  cached: CachedDocument,
  settings: AspSettings,
  key: string,
  context: VbProjectContext,
): void {
  if (!isIncludeAwareVbProjectContext(cached, context)) {
    return;
  }
  const familyKey = interactiveVbProjectContextFamilyKey(cached, settings);
  interactiveVbProjectContextSnapshots.set(familyKey, {
    familyKey,
    key,
    rootUri: cached.source.uri,
    context,
    lastUsed: Date.now(),
  });
  if (interactiveVbProjectContextSnapshots.size > maxVbProjectContextCacheEntries) {
    const oldest = [...interactiveVbProjectContextSnapshots.entries()].sort(
      (left, right) => left[1].lastUsed - right[1].lastUsed,
    )[0]?.[0];
    if (oldest) {
      interactiveVbProjectContextSnapshots.delete(oldest);
    }
  }
}

function isIncludeAwareVbProjectContext(
  cached: CachedDocument,
  context: VbProjectContext,
): boolean {
  return (
    cached.parsed.includes.length > 0 &&
    (context.documents ?? []).some(
      (document) => !sameFileIdentityUri(document.uri, cached.source.uri),
    )
  );
}

function interactiveVbProjectContextFamilyKey(
  cached: CachedDocument,
  settings: AspSettings,
): string {
  return JSON.stringify({
    root: vbProjectDocumentCollectionKey(cached, settings),
    settings: vbProjectContextSettings(settings),
    globals: settings.vbscript?.globals,
  });
}

function clearInteractiveVbProjectContextSnapshotsForUris(uris: Iterable<string>): void {
  const uriList = [...uris];
  for (const [key, snapshot] of interactiveVbProjectContextSnapshots) {
    if (uriList.some((uri) => sameFileIdentityUri(snapshot.rootUri, uri))) {
      interactiveVbProjectContextSnapshots.delete(key);
    }
  }
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
    const contextSettings = withCachedVbBuiltinRuntime(cached, vbProjectContextSettings(settings));
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
  const contextSettings = withCachedVbBuiltinRuntime(cached, vbProjectContextSettings(settings));
  const key = JSON.stringify({
    document: vbProjectDocumentFingerprint(cached.parsed),
    settings: {
      typeChecking: contextSettings.typeChecking,
      ifSyntaxDiagnostics: contextSettings.ifSyntaxDiagnostics,
      identifierCase: contextSettings.identifierCase,
      identifierCaseByKind: contextSettings.identifierCaseByKind,
      comTypes: contextSettings.comTypes,
      unusedDiagnostics: contextSettings.unusedDiagnostics,
      deadCodeDiagnostics: contextSettings.deadCodeDiagnostics,
      syntaxSnippets: contextSettings.syntaxSnippets,
      syntaxKeywords: contextSettings.syntaxKeywords,
      builtinRuntime: contextSettings.builtinRuntime,
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
  const contextSettings = withCachedVbBuiltinRuntime(cached, vbProjectContextSettings(settings));
  const key = JSON.stringify({
    document: vbProjectDocumentFingerprint(cached.parsed),
    settings: {
      typeChecking: contextSettings.typeChecking,
      ifSyntaxDiagnostics: contextSettings.ifSyntaxDiagnostics,
      identifierCase: contextSettings.identifierCase,
      identifierCaseByKind: contextSettings.identifierCaseByKind,
      comTypes: contextSettings.comTypes,
      unusedDiagnostics: contextSettings.unusedDiagnostics,
      deadCodeDiagnostics: contextSettings.deadCodeDiagnostics,
      syntaxSnippets: contextSettings.syntaxSnippets,
      syntaxKeywords: contextSettings.syntaxKeywords,
      builtinRuntime: contextSettings.builtinRuntime,
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
  const context = await interactiveVbProjectContextAsync(cached, settings);
  const symbol = getVbscriptDefinition(cached.parsed, position, context);
  return symbol ? workspaceVbscriptReferencesForSymbol(cached, symbol, settings, options) : [];
}

async function workspaceVbscriptReferencesForSymbol(
  cached: CachedDocument,
  symbol: VbSymbol,
  settings: AspSettings,
  options: VbReferenceOptions = {},
  executionOptions: WorkspaceVbReferenceExecutionOptions = {},
): Promise<VbReference[]> {
  return (
    (
      await workspaceVbscriptReferencesForSymbols(
        cached,
        [symbol],
        settings,
        options,
        { logSymbol: symbol.name },
        executionOptions,
      )
    ).get(vbscriptReferenceSymbolKey(symbol)) ?? []
  );
}

async function workspaceVbscriptReferencesForSymbols(
  cached: CachedDocument,
  symbols: VbSymbol[],
  settings: AspSettings,
  options: VbReferenceOptions = {},
  logOptions: { logSymbol?: string } = {},
  executionOptions: WorkspaceVbReferenceExecutionOptions = {},
): Promise<Map<string, VbReference[]>> {
  const openDocuments = vbReferencesWorkerOpenDocuments();
  const openDocumentsKey = vbReferencesWorkerOpenDocumentsKey(openDocuments);
  const key = workspaceVbReferenceRequestCacheKey(
    cached,
    symbols,
    settings,
    options,
    openDocumentsKey,
    executionOptions,
  );
  const cachedResult = workspaceVbReferenceRequestCompleted.get(key);
  if (cachedResult) {
    cachedResult.lastUsed = Date.now();
    logDebugSummary(settings, `[asp-lsp] vb.references.workspace.cache.hit: ${cached.source.uri}`);
    return cachedResult.referencesByTarget;
  }
  const inFlight = workspaceVbReferenceRequestInFlight.get(key);
  if (inFlight) {
    logDebugSummary(settings, `[asp-lsp] vb.references.workspace.reuse: ${cached.source.uri}`);
    return (await inFlight.promise).referencesByTarget;
  }
  const promise = workspaceVbscriptReferencesForSymbolsUncached(
    cached,
    symbols,
    settings,
    options,
    openDocuments,
    openDocumentsKey,
    logOptions,
    executionOptions,
  ).then((referencesByTarget) => {
    const result = { key, referencesByTarget, lastUsed: Date.now() };
    workspaceVbReferenceRequestCompleted.set(key, result);
    pruneWorkspaceVbReferenceRequestCompleted();
    return result;
  });
  workspaceVbReferenceRequestInFlight.set(key, { key, promise });
  try {
    return (await promise).referencesByTarget;
  } finally {
    if (workspaceVbReferenceRequestInFlight.get(key)?.promise === promise) {
      workspaceVbReferenceRequestInFlight.delete(key);
    }
  }
}

async function workspaceVbscriptReferencesForSymbolsUncached(
  cached: CachedDocument,
  symbols: VbSymbol[],
  settings: AspSettings,
  options: VbReferenceOptions,
  openDocuments: VbReferencesWorkerOpenDocument[],
  openDocumentsKey: string,
  logOptions: { logSymbol?: string },
  executionOptions: WorkspaceVbReferenceExecutionOptions,
): Promise<Map<string, VbReference[]>> {
  const context = await localVbReferenceContextAsync(cached, settings);
  const targets = symbols.map(
    (symbol) => equivalentVbSymbol(context.symbols ?? [], symbol) ?? symbol,
  );
  const targetKeyByContextKey = new Map<string, string>();
  const referencesByTarget = new Map<string, VbReference[]>();
  for (let index = 0; index < targets.length; index += 1) {
    const contextKey = vbscriptReferenceSymbolKey(targets[index]);
    const requestedKey = vbscriptReferenceSymbolKey(symbols[index]);
    targetKeyByContextKey.set(contextKey, requestedKey);
    referencesByTarget.set(requestedKey, []);
  }
  const localReferences = getVbscriptReferencesForSymbols(targets, context, options);
  for (const [contextKey, references] of localReferences) {
    addVbReferencesToArray(
      referencesByTarget,
      targetKeyByContextKey.get(contextKey) ?? contextKey,
      references,
    );
  }

  const targetForWorkers = targets.map(vbReferencesWorkerTargetSymbol);
  const candidates = await workspaceVbReferenceWorkerCandidates(
    new Set(),
    settings,
    targetForWorkers,
  );
  logDebugSummary(
    settings,
    `[asp-lsp] vb.references.workspace.candidates: ${cached.source.uri}, symbol=${logOptions.logSymbol ?? targets[0]?.name ?? "(batch)"}, symbols=${targets.length}, candidates=${candidates.length}`,
  );
  const summaryFastPath = await workspaceVbReferenceSummaryFastPath(
    candidates,
    targetForWorkers,
    settings,
    executionOptions,
  );
  for (const [contextKey, references] of summaryFastPath.referencesByTarget) {
    const requestedKey = targetKeyByContextKey.get(contextKey) ?? contextKey;
    addVbReferencesToArray(referencesByTarget, requestedKey, references);
  }
  if (summaryFastPath.fastPathCandidates > 0) {
    logDebugSummary(
      settings,
      `[asp-lsp] vb.references.summary.fastPath: ${cached.source.uri}, candidates=${summaryFastPath.fastPathCandidates}, fallback=${summaryFastPath.workerCandidates.length}`,
    );
  }
  const workerResponses = await Promise.all(
    summaryFastPath.workerCandidates.map((candidate) =>
      workspaceVbReferenceWorkerBatchResponse(
        candidate,
        targetForWorkers,
        settings,
        options,
        openDocuments,
        openDocumentsKey,
        executionOptions,
      ),
    ),
  );
  for (const response of workerResponses) {
    for (const target of targetForWorkers) {
      const contextKey = vbReferencesWorkerTargetKey(target);
      const requestedKey = targetKeyByContextKey.get(contextKey) ?? contextKey;
      addVbReferencesToArray(
        referencesByTarget,
        requestedKey,
        response.referencesByTarget?.[contextKey] ?? [],
      );
    }
  }

  for (const [key, references] of referencesByTarget) {
    referencesByTarget.set(key, dedupeVbReferences(references).sort(vbReferenceOrder));
  }
  return referencesByTarget;
}

async function workspaceVbReferenceSummaryFastPath(
  candidates: VbReferencesWorkerCandidate[],
  targets: VbReferencesWorkerTargetSymbol[],
  settings: AspSettings,
  executionOptions: WorkspaceVbReferenceExecutionOptions = {},
): Promise<{
  referencesByTarget: Map<string, VbReference[]>;
  workerCandidates: VbReferencesWorkerCandidate[];
  fastPathCandidates: number;
}> {
  const referencesByTarget = new Map<string, VbReference[]>(
    targets.map((target) => [vbReferencesWorkerTargetKey(target), []]),
  );
  if (
    candidates.length === 0 ||
    targets.length === 0 ||
    executionOptions.workerMaxDepth === 0 ||
    targets.some((target) => !isGlobalWorkspaceReferenceFallbackTarget(target))
  ) {
    return { referencesByTarget, workerCandidates: candidates, fastPathCandidates: 0 };
  }
  const results = await mapWithConcurrency(
    candidates,
    workspaceVbReferenceReachabilityConcurrency,
    async (candidate) => ({
      candidate,
      referencesByTarget: await workspaceVbReferenceSummaryReferencesForCandidate(
        candidate,
        targets,
        settings,
      ),
    }),
  );
  const workerCandidates: VbReferencesWorkerCandidate[] = [];
  let fastPathCandidates = 0;
  for (const result of results) {
    if (!result.referencesByTarget) {
      workerCandidates.push(result.candidate);
      continue;
    }
    fastPathCandidates += 1;
    for (const [key, references] of result.referencesByTarget) {
      addVbReferencesToArray(referencesByTarget, key, references);
    }
  }
  return { referencesByTarget, workerCandidates, fastPathCandidates };
}

async function workspaceVbReferenceSummaryReferencesForCandidate(
  candidate: VbReferencesWorkerCandidate,
  targets: VbReferencesWorkerTargetSymbol[],
  settings: AspSettings,
): Promise<Map<string, VbReference[]> | undefined> {
  if (targets.some((target) => sameFileIdentityUri(target.sourceUri, candidate.uri))) {
    return undefined;
  }
  const summaries = await workspaceVbReferenceCandidateSummaryClosure(candidate, settings);
  if (!summaries) {
    return undefined;
  }
  const includeGraph = await workspaceVbReferenceSummaryIncludeGraph(summaries, settings);
  const localSymbols = summaries.flatMap((summary) => summary.vbscript?.localSymbols ?? []);
  const referencesByTarget = new Map<string, VbReference[]>();
  for (const target of targets) {
    const sameNameSymbols = localSymbols.filter(
      (symbol) =>
        !symbol.memberOf &&
        !symbol.scopeName &&
        symbol.name.toLowerCase() === target.name.toLowerCase() &&
        ["function", "sub", "class", "variable", "constant"].includes(symbol.kind),
    );
    if (!sameNameSymbols.some((symbol) => sameVbReferenceTargetIdentity(symbol, target))) {
      return undefined;
    }
    if (sameNameSymbols.some((symbol) => !sameVbReferenceTargetIdentity(symbol, target))) {
      return undefined;
    }
    const visibilityMemo = createIncludeVisibilityMemo();
    referencesByTarget.set(
      vbReferencesWorkerTargetKey(target),
      summaries.flatMap((summary) =>
        summaryVbReferenceUsages(summary)
          .filter((usage) => usage.key === target.name.toLowerCase())
          .flatMap((usage) =>
            usage.ranges
              .filter((range) =>
                isSummaryFallbackTargetVisibleAt(
                  includeGraph,
                  summary.uri,
                  target,
                  range,
                  visibilityMemo,
                ),
              )
              .map((range) => ({ uri: summary.uri, range })),
          ),
      ),
    );
  }
  return referencesByTarget;
}

interface IncludeReachabilityGraph {
  directIncludesByOwnerKey: Map<string, Array<{ targetKey: string }>>;
  parentIncludesByTargetKey: Map<string, Array<{ ownerKey: string }>>;
}

interface IncludeVisibilityMemo {
  cache: Map<string, boolean>;
  visiting: Set<string>;
}

const sourceGraphVisibilityMemoByState = new WeakMap<AspGraphBuildState, IncludeVisibilityMemo>();

function createIncludeVisibilityMemo(): IncludeVisibilityMemo {
  return { cache: new Map(), visiting: new Set() };
}

function sourceGraphVisibilityMemo(state: AspGraphBuildState): IncludeVisibilityMemo {
  let memo = sourceGraphVisibilityMemoByState.get(state);
  if (!memo) {
    memo = createIncludeVisibilityMemo();
    sourceGraphVisibilityMemoByState.set(state, memo);
  }
  return memo;
}

function includeVisibilityMemoKey(ownerKey: string, targetKey: string, range: Range): string {
  return `${ownerKey}\0${targetKey}\0${range.start.line}:${range.start.character}`;
}

function memoizedIncludeVisibility(
  memo: IncludeVisibilityMemo,
  key: string,
  compute: () => boolean,
): boolean {
  const cached = memo.cache.get(key);
  if (cached !== undefined) {
    return cached;
  }
  if (memo.visiting.has(key)) {
    return false;
  }
  memo.visiting.add(key);
  try {
    const value = compute();
    memo.cache.set(key, value);
    return value;
  } finally {
    memo.visiting.delete(key);
  }
}

function precomputeIncludeReachability(
  graph: IncludeReachabilityGraph,
  targetKeys: Iterable<string>,
): PrecomputedIncludeReachability {
  const hasCycle = includeGraphHasCycle(graph);
  const reachingFileKeysByTarget = new Map<string, Set<string>>();
  if (hasCycle) {
    return { hasCycle, reachingFileKeysByTarget };
  }
  for (const targetKey of targetKeys) {
    const reaching = new Set<string>();
    const queue = [targetKey];
    for (let index = 0; index < queue.length; index += 1) {
      const currentKey = queue[index];
      for (const parentInclude of graph.parentIncludesByTargetKey.get(currentKey) ?? []) {
        if (reaching.has(parentInclude.ownerKey)) {
          continue;
        }
        reaching.add(parentInclude.ownerKey);
        queue.push(parentInclude.ownerKey);
      }
    }
    reachingFileKeysByTarget.set(targetKey, reaching);
  }
  return { hasCycle, reachingFileKeysByTarget };
}

function includeGraphHasCycle(graph: IncludeReachabilityGraph): boolean {
  const visiting = new Set<string>();
  const visited = new Set<string>();
  const visit = (ownerKey: string): boolean => {
    if (visiting.has(ownerKey)) {
      return true;
    }
    if (visited.has(ownerKey)) {
      return false;
    }
    visiting.add(ownerKey);
    for (const include of graph.directIncludesByOwnerKey.get(ownerKey) ?? []) {
      if (visit(include.targetKey)) {
        return true;
      }
    }
    visiting.delete(ownerKey);
    visited.add(ownerKey);
    return false;
  };
  for (const ownerKey of graph.directIncludesByOwnerKey.keys()) {
    if (visit(ownerKey)) {
      return true;
    }
  }
  return false;
}

function precomputedIncludeCanReachTarget(
  reachability: PrecomputedIncludeReachability | undefined,
  startKey: string,
  targetKey: string,
): boolean | undefined {
  if (!reachability || reachability.hasCycle) {
    return undefined;
  }
  return reachability.reachingFileKeysByTarget.get(targetKey)?.has(startKey) === true;
}

async function workspaceVbReferenceSummaryIncludeGraph(
  summaries: FileAnalysisSummary[],
  settings: AspSettings,
): Promise<WorkspaceVbReferenceSummaryIncludeGraph> {
  const graph: WorkspaceVbReferenceSummaryIncludeGraph = {
    directIncludesByOwnerKey: new Map(),
    parentIncludesByTargetKey: new Map(),
  };
  await mapWithConcurrency(
    summaries,
    workspaceVbReferenceReachabilityConcurrency,
    async (summary) => {
      for (const include of summary.includeRefs) {
        const resolved = await resolveIncludePathDetailsAsync(
          summary.uri,
          include.path,
          include.mode,
          settings,
        );
        if (!resolved.exists) {
          continue;
        }
        const ownerKey = fileIdentityKeyFromUri(summary.uri);
        const targetKey = fileIdentityKeyFromFileName(resolved.fileName);
        pushAspGraphMapItem(graph.directIncludesByOwnerKey, ownerKey, {
          range: include.range,
          targetKey,
        });
        pushAspGraphMapItem(graph.parentIncludesByTargetKey, targetKey, {
          ownerKey,
          range: include.range,
        });
      }
    },
  );
  return graph;
}

function isSummaryFallbackTargetVisibleAt(
  graph: WorkspaceVbReferenceSummaryIncludeGraph,
  ownerUri: string,
  target: VbReferencesWorkerTargetSymbol,
  referenceRange: Range,
  memo: IncludeVisibilityMemo = createIncludeVisibilityMemo(),
): boolean {
  if (!target.sourceUri.startsWith("file://")) {
    return true;
  }
  const ownerKey = fileIdentityKeyFromUri(ownerUri);
  const targetKey = fileIdentityKeyFromUri(target.sourceUri);
  if (targetKey === ownerKey) {
    return true;
  }
  return isSummaryFallbackTargetVisibleFromFileAt(
    graph,
    ownerKey,
    target,
    targetKey,
    referenceRange,
    memo,
    new Set([ownerKey]),
  );
}

function isSummaryFallbackTargetVisibleFromFileAt(
  graph: WorkspaceVbReferenceSummaryIncludeGraph,
  ownerKey: string,
  target: VbReferencesWorkerTargetSymbol,
  targetKey: string,
  referenceRange: Range,
  memo: IncludeVisibilityMemo,
  visited: Set<string>,
): boolean {
  const key = includeVisibilityMemoKey(
    ownerKey,
    vbReferencesWorkerTargetKey(target),
    referenceRange,
  );
  return memoizedIncludeVisibility(memo, key, () => {
    if (targetKey === ownerKey) {
      return positionBeforeOrEqual(target.range.start, referenceRange.start);
    }
    if (hasEarlierReachableSummaryInclude(graph, ownerKey, targetKey, referenceRange)) {
      return true;
    }
    for (const parentInclude of graph.parentIncludesByTargetKey.get(ownerKey) ?? []) {
      if (visited.has(parentInclude.ownerKey)) {
        continue;
      }
      visited.add(parentInclude.ownerKey);
      const visible = isSummaryFallbackTargetVisibleFromFileAt(
        graph,
        parentInclude.ownerKey,
        target,
        targetKey,
        parentInclude.range,
        memo,
        visited,
      );
      visited.delete(parentInclude.ownerKey);
      if (visible) {
        return true;
      }
    }
    return false;
  });
}

function hasEarlierReachableSummaryInclude(
  graph: WorkspaceVbReferenceSummaryIncludeGraph,
  ownerKey: string,
  targetKey: string,
  referenceRange: Range,
  reachability?: PrecomputedIncludeReachability,
): boolean {
  const includes = graph.directIncludesByOwnerKey.get(ownerKey) ?? [];
  return includes.some((include) => {
    if (!positionBeforeOrEqual(include.range.start, referenceRange.start)) {
      return false;
    }
    if (include.targetKey === targetKey) {
      return true;
    }
    const precomputed = precomputedIncludeCanReachTarget(
      reachability,
      include.targetKey,
      targetKey,
    );
    return (
      precomputed === true ||
      (precomputed === undefined &&
        isSummaryIncludeReachable(graph, include.targetKey, targetKey, new Set([ownerKey])))
    );
  });
}

function isSummaryIncludeReachable(
  graph: WorkspaceVbReferenceSummaryIncludeGraph,
  startKey: string,
  targetKey: string,
  visited: Set<string>,
): boolean {
  if (startKey === targetKey) {
    return true;
  }
  if (visited.has(startKey)) {
    return false;
  }
  visited.add(startKey);
  return (graph.directIncludesByOwnerKey.get(startKey) ?? []).some(
    (include) =>
      include.targetKey === targetKey ||
      isSummaryIncludeReachable(graph, include.targetKey, targetKey, visited),
  );
}

async function workspaceVbReferenceCandidateSummaryClosure(
  candidate: VbReferencesWorkerCandidate,
  settings: AspSettings,
): Promise<FileAnalysisSummary[] | undefined> {
  const limits = vbProjectContextLimits(settings);
  const summaries: FileAnalysisSummary[] = [];
  const visited = new Set<string>();
  let textLength = 0;

  const visit = async (uri: string, fileName: string, depth: number): Promise<boolean> => {
    if (depth > 20 || summaries.length >= limits.maxDocuments) {
      return false;
    }
    const entry = await includeDocumentLoader.readSummaryAsync(fileName, settings, {
      allowRead: true,
    });
    const entryKey = entry ? fileIdentityKeyFromFileName(entry.fileName) : undefined;
    if (!entry || !entryKey || visited.has(entryKey)) {
      return Boolean(entry);
    }
    if (textLength + entry.source.size > limits.maxTextLength) {
      return false;
    }
    visited.add(entryKey);
    textLength += entry.source.size;
    summaries.push(entry.summary);
    for (const include of entry.summary.includeRefs) {
      const resolved = await resolveIncludePathDetailsAsync(
        uri,
        include.path,
        include.mode,
        settings,
      );
      if (!resolved.exists) {
        return false;
      }
      if (!(await visit(pathToFileUri(resolved.fileName), resolved.fileName, depth + 1))) {
        return false;
      }
    }
    return true;
  };

  return (await visit(candidate.uri, candidate.fileName, 0)) ? summaries : undefined;
}

async function workspaceVbReferenceWorkerCandidates(
  excludedUris: Set<string>,
  settings: AspSettings,
  targets: VbReferencesWorkerTargetSymbol[] = [],
): Promise<VbReferencesWorkerCandidate[]> {
  await ensureWorkspaceIndexAsync(settings);
  const excludedFileKeys = new Set(
    [...excludedUris]
      .filter((uri) => uri.startsWith("file://"))
      .map((uri) => fileIdentityKeyFromUri(uri)),
  );
  const candidates = new Map<string, VbReferencesWorkerCandidate>();
  for (const document of await workspaceAnalyzableOpenDocumentsAsync(settings)) {
    const fileKey = fileIdentityKeyFromUri(document.uri);
    if (excludedUris.has(document.uri) || excludedFileKeys.has(fileKey)) {
      continue;
    }
    const fileName = normalizeFileName(uriToFileName(document.uri));
    const identity = await includeDocumentSourceIdentityAsync(fileName, settings);
    if (!identity) {
      continue;
    }
    const candidate = {
      uri: document.uri,
      fileName,
      source: {
        ...identity.source,
        openVersion: document.version,
      },
    };
    candidates.set(fileKey, candidate);
  }
  for (const entry of workspaceIndex.values()) {
    const fileKey = fileIdentityKeyFromFileName(entry.fileName);
    if (excludedUris.has(entry.uri) || excludedFileKeys.has(fileKey) || candidates.has(fileKey)) {
      continue;
    }
    const candidate = {
      uri: entry.uri,
      fileName: entry.fileName,
      source: {
        fileName: entry.fileName,
        mtimeMs: entry.mtimeMs,
        size: entry.size,
      },
    };
    candidates.set(fileKey, candidate);
  }
  return filterWorkspaceVbReferenceWorkerCandidates([...candidates.values()], targets, settings);
}

async function filterWorkspaceVbReferenceWorkerCandidates(
  candidates: VbReferencesWorkerCandidate[],
  targets: VbReferencesWorkerTargetSymbol[],
  settings: AspSettings,
): Promise<VbReferencesWorkerCandidate[]> {
  if (targets.length === 0 || candidates.length === 0) {
    return candidates;
  }
  const targetFiles = new Set(
    targets
      .map((target) =>
        target.sourceUri.startsWith("file://")
          ? fileIdentityKeyFromUri(target.sourceUri)
          : undefined,
      )
      .filter((fileName): fileName is string => Boolean(fileName)),
  );
  if (targetFiles.size === 0) {
    return candidates;
  }
  const reachability = await workspaceVbReferenceReachability(candidates, targetFiles, settings);
  return candidates.filter((candidate) => !reachability.skippedUris.has(candidate.uri));
}

async function workspaceVbReferenceReachability(
  candidates: VbReferencesWorkerCandidate[],
  targetFiles: Set<string>,
  settings: AspSettings,
): Promise<WorkspaceVbReferenceReachabilityEntry> {
  const key = JSON.stringify({
    candidates: textFingerprint(
      JSON.stringify(
        candidates
          .map((candidate) => ({
            uri: fileIdentityKeyFromUri(candidate.uri),
            fileName: fileIdentityKeyFromFileName(candidate.fileName),
            source: diskAnalysisSourceIdentity(candidate.source),
          }))
          .sort((left, right) => left.uri.localeCompare(right.uri)),
      ),
    ),
    targets: [...targetFiles].sort(),
    settings: {
      include: includeResolutionSettingsIdentity(settings),
      legacyEncoding: settings.legacyEncoding,
    },
    limits: {
      maxDocuments: maxWorkspaceVbReferenceReachabilityDocuments,
    },
    workspaceGeneration,
  });
  const cached = workspaceVbReferenceReachabilityCache.get(key);
  if (cached) {
    cached.lastUsed = Date.now();
    return cached;
  }
  const inFlight = workspaceVbReferenceReachabilityInFlight.get(key);
  if (inFlight) {
    logDebugSummary(settings, "[asp-lsp] vb.references.reachability.reuse");
    return inFlight;
  }

  const promise = computeWorkspaceVbReferenceReachability(key, candidates, targetFiles, settings);
  workspaceVbReferenceReachabilityInFlight.set(key, promise);
  try {
    return await promise;
  } finally {
    if (workspaceVbReferenceReachabilityInFlight.get(key) === promise) {
      workspaceVbReferenceReachabilityInFlight.delete(key);
    }
  }
}

async function computeWorkspaceVbReferenceReachability(
  key: string,
  candidates: VbReferencesWorkerCandidate[],
  targetFiles: Set<string>,
  settings: AspSettings,
): Promise<WorkspaceVbReferenceReachabilityEntry> {
  const basenameReachability = await workspaceVbReferenceBasenameReachability(
    candidates,
    targetFiles,
  );
  if (basenameReachability) {
    basenameReachability.key = key;
    workspaceVbReferenceReachabilityCache.set(key, basenameReachability);
    pruneWorkspaceVbReferenceReachabilityCache();
    for (const candidate of candidates) {
      if (basenameReachability.skippedUris.has(candidate.uri)) {
        logDebugSummary(settings, `[asp-lsp] vb.references.reachability.skip: ${candidate.uri}`);
      }
    }
    return basenameReachability;
  }

  const graph = await workspaceVbReferenceReachabilityGraph(candidates, settings);
  const stateCache = new Map<string, WorkspaceVbReferenceReachabilityState>();
  let complete = true;

  const visit = (
    uri: string,
    fileName: string,
    depth: number,
    pathStack: Set<string>,
  ): WorkspaceVbReferenceReachabilityState => {
    const fileKey = fileIdentityKeyFromFileName(fileName);
    if (targetFiles.has(fileKey)) {
      return { reachesTarget: true, complete: true, documents: 1 };
    }
    const cachedState = stateCache.get(fileKey);
    if (cachedState) {
      return cachedState;
    }
    if (depth > 20 || pathStack.size > maxWorkspaceVbReferenceReachabilityDocuments) {
      return { reachesTarget: false, complete: false, documents: 1 };
    }
    if (pathStack.has(fileKey)) {
      return { reachesTarget: false, complete: true, documents: 0 };
    }

    pathStack.add(fileKey);
    const node = graph.get(fileKey);
    if (!node) {
      pathStack.delete(fileKey);
      return { reachesTarget: false, complete: false, documents: 1 };
    }

    let reachesTarget = false;
    let stateComplete = node.complete;
    let documents = 1;
    for (const includeFileName of node.includes) {
      const childState = visit(
        pathToFileUri(includeFileName),
        includeFileName,
        depth + 1,
        pathStack,
      );
      reachesTarget ||= childState.reachesTarget;
      stateComplete &&= childState.complete;
      documents += childState.documents;
      if (documents > maxWorkspaceVbReferenceReachabilityDocuments) {
        stateComplete = false;
        break;
      }
    }
    pathStack.delete(fileKey);

    const state = { reachesTarget, complete: stateComplete, documents };
    stateCache.set(fileKey, state);
    return state;
  };

  const skippedUris = new Set<string>();
  for (const candidate of candidates) {
    const state = visit(candidate.uri, candidate.fileName, 0, new Set());
    complete &&= state.complete;
    if (state.complete && !state.reachesTarget) {
      skippedUris.add(candidate.uri);
    }
  }

  const entry = {
    key,
    skippedUris,
    complete,
    lastUsed: Date.now(),
  };
  workspaceVbReferenceReachabilityCache.set(key, entry);
  pruneWorkspaceVbReferenceReachabilityCache();
  for (const candidate of candidates) {
    if (skippedUris.has(candidate.uri)) {
      logDebugSummary(settings, `[asp-lsp] vb.references.reachability.skip: ${candidate.uri}`);
    }
  }
  return entry;
}

async function workspaceVbReferenceBasenameReachability(
  candidates: VbReferencesWorkerCandidate[],
  targetFiles: Set<string>,
): Promise<WorkspaceVbReferenceReachabilityEntry | undefined> {
  const targetBasenames = [...targetFiles]
    .map((fileName) => path.basename(fileName).toLowerCase())
    .filter((name) => name.length > 0);
  if (targetBasenames.length === 0) {
    return undefined;
  }
  const targetNeedles = targetBasenames
    .map(asciiNeedle)
    .filter((item): item is Uint8Array => Boolean(item));
  if (targetNeedles.length !== targetBasenames.length) {
    return undefined;
  }

  const matches = await mapWithConcurrency(
    candidates,
    workspaceVbReferenceReachabilityConcurrency,
    async (candidate) => {
      const fallbackOpenDocument = openDocumentForFileName(candidate.fileName);
      const bytes =
        fallbackOpenDocument === undefined
          ? await fs.promises.readFile(candidate.fileName).catch(() => undefined)
          : Buffer.from(fallbackOpenDocument.getText(), "utf8");
      if (bytes === undefined) {
        return { candidate, unknown: true, containsTargetBasename: false };
      }
      const containsTargetBasename = includeDirectiveMentionsAnyTarget(bytes, targetNeedles);
      if (containsTargetBasename === undefined) {
        return { candidate, unknown: true, containsTargetBasename: false };
      }
      return {
        candidate,
        unknown: false,
        containsTargetBasename,
      };
    },
  );

  if (matches.some((match) => match.unknown || match.containsTargetBasename)) {
    return undefined;
  }
  return {
    key: "",
    skippedUris: new Set(candidates.map((candidate) => candidate.uri)),
    complete: true,
    lastUsed: Date.now(),
  };
}

function asciiNeedle(text: string): Uint8Array | undefined {
  const bytes: number[] = [];
  for (let index = 0; index < text.length; index += 1) {
    const code = text.charCodeAt(index);
    if (code > 0x7f) {
      return undefined;
    }
    bytes.push(asciiLowerByte(code));
  }
  return Uint8Array.from(bytes);
}

function includeDirectiveMentionsAnyTarget(
  bytes: Uint8Array,
  targetNeedles: Uint8Array[],
): boolean | undefined {
  const includeNeedle = Uint8Array.from([35, 105, 110, 99, 108, 117, 100, 101]);
  const directiveCloseNeedle = Uint8Array.from([45, 45, 62]);
  let offset = 0;
  for (;;) {
    const includeAt = indexOfAsciiInsensitive(bytes, includeNeedle, offset);
    if (includeAt < 0) {
      return false;
    }
    const closeAt = indexOfAsciiInsensitive(bytes, directiveCloseNeedle, includeAt);
    if (closeAt < 0) {
      return undefined;
    }
    const closeEnd = closeAt + directiveCloseNeedle.length;
    if (
      targetNeedles.some(
        (needle) => indexOfAsciiInsensitive(bytes, needle, includeAt, closeEnd) >= 0,
      )
    ) {
      return true;
    }
    offset = closeEnd;
  }
}

function indexOfAsciiInsensitive(
  bytes: Uint8Array,
  needle: Uint8Array,
  offset: number,
  end = bytes.length,
): number {
  if (needle.length === 0) {
    return offset;
  }
  const limit = end - needle.length;
  for (let index = offset; index <= limit; index += 1) {
    let matched = true;
    for (let needleIndex = 0; needleIndex < needle.length; needleIndex += 1) {
      if (asciiLowerByte(bytes[index + needleIndex]) !== needle[needleIndex]) {
        matched = false;
        break;
      }
    }
    if (matched) {
      return index;
    }
  }
  return -1;
}

function asciiLowerByte(value: number): number {
  return value >= 65 && value <= 90 ? value + 32 : value;
}

async function workspaceVbReferenceReachabilityGraph(
  candidates: VbReferencesWorkerCandidate[],
  settings: AspSettings,
): Promise<Map<string, WorkspaceVbReferenceReachabilityGraphNode>> {
  const fileNames = [
    ...new Map(
      candidates.map((candidate) => [
        fileIdentityKeyFromFileName(candidate.fileName),
        normalizeFileName(candidate.fileName),
      ]),
    ).values(),
  ];
  const nodes = new Map<string, WorkspaceVbReferenceReachabilityGraphNode>();
  const includeRefsEntries = await mapWithConcurrency(
    fileNames,
    workspaceVbReferenceReachabilityConcurrency,
    async (fileName) => ({
      fileName,
      entry: await includeDocumentLoader.readIncludeRefsAsync(fileName, settings, {
        allowRead: true,
      }),
    }),
  );
  const includeEdges: Array<{
    ownerUri: string;
    ownerFileName: string;
    include: AspInclude;
  }> = [];
  for (const { fileName, entry } of includeRefsEntries) {
    const normalized = normalizeFileName(fileName);
    const fileKey = fileIdentityKeyFromFileName(normalized);
    if (!entry) {
      nodes.set(fileKey, {
        uri: pathToFileUri(normalized),
        fileName: normalized,
        includes: [],
        complete: false,
      });
      continue;
    }
    nodes.set(fileKey, {
      uri: entry.uri,
      fileName: normalized,
      includes: [],
      complete: true,
    });
    for (const include of entry.includeRefs) {
      includeEdges.push({ ownerUri: entry.uri, ownerFileName: normalized, include });
    }
  }

  await mapWithConcurrency(
    includeEdges,
    workspaceVbReferenceReachabilityConcurrency,
    async ({ ownerUri, ownerFileName, include }) => {
      const node = nodes.get(fileIdentityKeyFromFileName(ownerFileName));
      if (!node) {
        return;
      }
      const resolved = await resolveIncludePathDetailsAsync(
        ownerUri,
        include.path,
        include.mode,
        settings,
      );
      if (!resolved.exists) {
        node.complete = false;
        return;
      }
      node.includes.push(normalizeFileName(resolved.fileName));
    },
  );
  return nodes;
}

function vbReferencesWorkerTargetSymbol(symbol: VbSymbol): VbReferencesWorkerTargetSymbol {
  return {
    name: symbol.name,
    kind: symbol.kind,
    sourceUri: symbol.sourceUri,
    range: symbol.range,
    memberOf: symbol.memberOf,
    scopeName: symbol.scopeName,
    visibility: symbol.visibility,
    procedureKind: symbol.procedureKind,
  };
}

function vbReferencesWorkerOpenDocuments(): VbReferencesWorkerOpenDocument[] {
  return documents
    .all()
    .filter((document) => isClassicAspGraphUri(document.uri))
    .map((document) => ({
      uri: document.uri,
      fileName: normalizeFileName(uriToFileName(document.uri)),
      text: document.getText(),
      version: document.version,
    }));
}

function vbReferencesWorkerOpenDocumentsKey(
  openDocuments: VbReferencesWorkerOpenDocument[],
): string {
  return JSON.stringify(
    openDocuments
      .map((document) => ({
        uri: fileIdentityKeyFromUri(document.uri),
        version: document.version,
        text: textFingerprint(document.text),
      }))
      .sort((left, right) => left.uri.localeCompare(right.uri)),
  );
}

function workspaceVbReferenceRequestCacheKey(
  cached: CachedDocument,
  symbols: VbSymbol[],
  settings: AspSettings,
  options: VbReferenceOptions,
  openDocumentsKey: string,
  executionOptions: WorkspaceVbReferenceExecutionOptions,
): string {
  return JSON.stringify({
    scope: "workspaceReferences",
    source: {
      uri: fileIdentityKeyFromUri(cached.source.uri),
      version: cached.source.version,
      text: textFingerprint(cached.source.getText()),
      parsed: vbProjectDocumentFingerprint(cached.parsed),
    },
    symbols: symbols.map(vbscriptReferenceSymbolKey).sort(),
    settings: {
      parse: parseSettingsIdentity(settings),
      include: includeResolutionSettingsIdentity(settings),
      vbscript: vbProjectContextSettings(settings),
      legacyEncoding: settings.legacyEncoding,
    },
    options,
    executionOptions,
    workspaceGeneration,
    openDocuments: openDocumentsKey,
  });
}

function vbReferencesWorkerCacheOptions(settings: AspSettings): VbReferencesWorkerCacheOptions {
  return {
    disk: {
      enabled: settings.cache?.enabled !== false,
      directory: settings.cache?.directory,
      ttlHours: settings.cache?.ttlHours,
      maxSizeMb: settings.cache?.maxSizeMb,
      namespace: diskAnalysisNamespace(),
      toolVersion: languageServerVersion,
    },
    freshness: cacheFreshness(settings),
    sourceManifest: [...workspaceIndex.values()].map((entry) => {
      const source = sourceManifest.get(fileIdentityKeyFromFileName(entry.fileName));
      return {
        uri: entry.uri,
        fileName: normalizeFileName(entry.fileName),
        mtimeMs: entry.mtimeMs,
        size: entry.size,
        contentHash: source?.contentHash,
      };
    }),
  };
}

async function workspaceVbReferenceWorkerResponse(
  candidate: VbReferencesWorkerCandidate,
  target: VbReferencesWorkerTargetSymbol,
  settings: AspSettings,
  options: VbReferenceOptions,
  openDocuments: VbReferencesWorkerOpenDocument[],
  openDocumentsKey: string,
  executionOptions: WorkspaceVbReferenceExecutionOptions,
): Promise<VbReferencesWorkerResponse> {
  const key = workspaceVbReferenceWorkerTaskKey(
    candidate,
    target,
    settings,
    workspaceVbReferenceWorkerOptions(options),
    openDocumentsKey,
    executionOptions,
  );
  const cached = workspaceVbReferenceWorkerCompleted.get(key);
  if (cached) {
    workspaceVbReferenceWorkerCompleted.delete(key);
    workspaceVbReferenceWorkerCompleted.set(key, cached);
    logDebugSummary(settings, `[asp-lsp] vb.references.worker.cache.hit: ${candidate.uri}`);
    return cached;
  }
  const inFlight = workspaceVbReferenceWorkerInFlight.get(key);
  if (inFlight) {
    logDebugSummary(settings, `[asp-lsp] vb.references.worker.reuse: ${candidate.uri}`);
    return inFlight.promise;
  }

  const request = {
    id: ++vbReferencesWorkerRequestId,
    candidate,
    target,
    settings,
    workspaceRoots,
    openDocuments,
    cache: vbReferencesWorkerCacheOptions(settings),
    options: workspaceVbReferenceWorkerOptions(options),
    limits: {
      ...vbProjectContextLimits(settings),
      maxDepth: executionOptions.workerMaxDepth ?? 20,
      includeReadConcurrency: includeReadConcurrency(settings),
    },
  };
  const pool = getVbReferencesWorkerPool(settings);
  const promise = pool
    .run(request)
    .then(async (response) => {
      if (
        !sameVbReferencesWorkerCandidate(candidate, response.candidate) ||
        vbReferencesWorkerOpenDocumentsKey(vbReferencesWorkerOpenDocuments()) !==
          openDocumentsKey ||
        !(await isCurrentVbReferencesWorkerCandidate(candidate, settings))
      ) {
        logDebugSummary(settings, `[asp-lsp] vb.references.worker.stale: ${candidate.uri}`);
        return { ...response, references: [] };
      }
      workspaceVbReferenceWorkerCompleted.set(key, response);
      pruneWorkspaceVbReferenceWorkerCompleted();
      if (response.error) {
        logDebugSummary(
          settings,
          `[asp-lsp] vb.references.worker.error: ${candidate.uri}, ${response.error.message}`,
        );
      } else {
        logDebugSummary(
          settings,
          `[asp-lsp] vb.references.worker.complete: ${candidate.uri}, references=${response.references?.length ?? 0}, fallback=${response.fallbackReasons?.join("|") ?? ""}, cacheHits=${response.cacheHits ?? 0}`,
        );
      }
      return response;
    })
    .catch((error: unknown) => {
      logServerWarning(
        `[asp-lsp] vb.references.worker.failed: ${candidate.uri}, error=${errorMessage(error)}`,
      );
      return {
        id: request.id,
        candidate,
        references: [],
        error: { message: errorMessage(error) },
      } satisfies VbReferencesWorkerResponse;
    })
    .finally(() => {
      if (workspaceVbReferenceWorkerInFlight.get(key)?.promise === promise) {
        workspaceVbReferenceWorkerInFlight.delete(key);
      }
    });
  workspaceVbReferenceWorkerInFlight.set(key, { key, promise });
  return promise;
}

async function workspaceVbReferenceWorkerBatchResponse(
  candidate: VbReferencesWorkerCandidate,
  targets: VbReferencesWorkerTargetSymbol[],
  settings: AspSettings,
  options: VbReferenceOptions,
  openDocuments: VbReferencesWorkerOpenDocument[],
  openDocumentsKey: string,
  executionOptions: WorkspaceVbReferenceExecutionOptions,
): Promise<VbReferencesWorkerResponse> {
  if (targets.length === 0) {
    return { id: 0, candidate, references: [], referencesByTarget: {} };
  }
  if (targets.length === 1) {
    const response = await workspaceVbReferenceWorkerResponse(
      candidate,
      targets[0],
      settings,
      options,
      openDocuments,
      openDocumentsKey,
      executionOptions,
    );
    return {
      ...response,
      referencesByTarget: {
        [vbReferencesWorkerTargetKey(targets[0])]: response.references ?? [],
      },
    };
  }
  const key = workspaceVbReferenceWorkerBatchTaskKey(
    candidate,
    targets,
    settings,
    workspaceVbReferenceWorkerOptions(options),
    openDocumentsKey,
    executionOptions,
  );
  const inFlight = workspaceVbReferenceWorkerBatchInFlight.get(key);
  if (inFlight) {
    logDebugSummary(settings, `[asp-lsp] vb.references.worker.batch.reuse: ${candidate.uri}`);
    return inFlight.promise;
  }
  const request = {
    id: ++vbReferencesWorkerRequestId,
    candidate,
    target: targets[0],
    targets,
    settings,
    workspaceRoots,
    openDocuments,
    cache: vbReferencesWorkerCacheOptions(settings),
    options: workspaceVbReferenceWorkerOptions(options),
    limits: {
      ...vbProjectContextLimits(settings),
      maxDepth: executionOptions.workerMaxDepth ?? 20,
      includeReadConcurrency: Math.max(1, Math.min(4, analysisConcurrency(settings))),
    },
  };
  const pool = getVbReferencesWorkerPool(settings);
  const promise = pool
    .run(request)
    .then(async (response) => {
      if (
        !sameVbReferencesWorkerCandidate(candidate, response.candidate) ||
        vbReferencesWorkerOpenDocumentsKey(vbReferencesWorkerOpenDocuments()) !==
          openDocumentsKey ||
        !(await isCurrentVbReferencesWorkerCandidate(candidate, settings))
      ) {
        logDebugSummary(settings, `[asp-lsp] vb.references.worker.stale: ${candidate.uri}`);
        return { ...response, references: [], referencesByTarget: {} };
      }
      for (const target of targets) {
        const targetKey = vbReferencesWorkerTargetKey(target);
        seedWorkspaceVbReferenceWorkerCompleted(
          candidate,
          target,
          settings,
          options,
          openDocumentsKey,
          executionOptions,
          {
            ...response,
            references: response.referencesByTarget?.[targetKey] ?? [],
            referencesByTarget: undefined,
          },
        );
      }
      if (response.error) {
        logDebugSummary(
          settings,
          `[asp-lsp] vb.references.worker.error: ${candidate.uri}, ${response.error.message}`,
        );
      } else {
        logDebugSummary(
          settings,
          `[asp-lsp] vb.references.worker.batch.complete: ${candidate.uri}, symbols=${targets.length}, fallback=${response.fallbackReasons?.join("|") ?? ""}, cacheHits=${response.cacheHits ?? 0}`,
        );
      }
      return response;
    })
    .catch((error: unknown) => {
      logServerWarning(
        `[asp-lsp] vb.references.worker.failed: ${candidate.uri}, error=${errorMessage(error)}`,
      );
      return {
        id: request.id,
        candidate,
        references: [],
        referencesByTarget: {},
        error: { message: errorMessage(error) },
      } satisfies VbReferencesWorkerResponse;
    })
    .finally(() => {
      if (workspaceVbReferenceWorkerBatchInFlight.get(key)?.promise === promise) {
        workspaceVbReferenceWorkerBatchInFlight.delete(key);
      }
    });
  workspaceVbReferenceWorkerBatchInFlight.set(key, { key, promise });
  return promise;
}

function seedWorkspaceVbReferenceWorkerCompleted(
  candidate: VbReferencesWorkerCandidate,
  target: VbReferencesWorkerTargetSymbol,
  settings: AspSettings,
  options: VbReferenceOptions,
  openDocumentsKey: string,
  executionOptions: WorkspaceVbReferenceExecutionOptions,
  response: VbReferencesWorkerResponse,
): void {
  const key = workspaceVbReferenceWorkerTaskKey(
    candidate,
    target,
    settings,
    workspaceVbReferenceWorkerOptions(options),
    openDocumentsKey,
    executionOptions,
  );
  workspaceVbReferenceWorkerCompleted.set(key, response);
  pruneWorkspaceVbReferenceWorkerCompleted();
}

function workspaceVbReferenceWorkerOptions(_options: VbReferenceOptions): VbReferenceOptions {
  return {
    includeDeclaration: false,
    includeFunctionReturnAssignments: false,
  };
}

function vbReferencesWorkerTargetKey(symbol: VbReferencesWorkerTargetSymbol): string {
  const range = vbReferencesWorkerTargetIdentityRange(symbol);
  return [
    fileIdentityKeyFromUri(symbol.sourceUri),
    symbol.kind,
    symbol.memberOf ?? "",
    symbol.name.toLowerCase(),
    range?.start.line ?? "",
    range?.start.character ?? "",
  ].join("|");
}

function vbReferencesWorkerTargetIdentityRange(
  symbol: VbReferencesWorkerTargetSymbol,
): VbReferencesWorkerTargetSymbol["range"] | undefined {
  return symbol.kind === "property" ? undefined : symbol.range;
}

function workspaceVbReferenceWorkerTaskKey(
  candidate: VbReferencesWorkerCandidate,
  target: VbReferencesWorkerTargetSymbol,
  settings: AspSettings,
  options: VbReferenceOptions,
  openDocumentsKey: string,
  executionOptions: WorkspaceVbReferenceExecutionOptions,
): string {
  return JSON.stringify({
    target: {
      ...target,
      sourceUri: fileIdentityKeyFromUri(target.sourceUri),
    },
    candidate: {
      ...candidate,
      uri: fileIdentityKeyFromUri(candidate.uri),
      fileName: fileIdentityKeyFromFileName(candidate.fileName),
      source: {
        ...candidate.source,
        fileName: fileIdentityKeyFromFileName(candidate.source.fileName),
      },
    },
    settings: {
      parse: parseSettingsIdentity(settings),
      include: includeResolutionSettingsIdentity(settings),
      vbscript: vbProjectContextSettings(settings),
      legacyEncoding: settings.legacyEncoding,
    },
    options,
    executionOptions,
    workspaceGeneration,
    openDocuments: openDocumentsKey,
  });
}

function workspaceVbReferenceWorkerBatchTaskKey(
  candidate: VbReferencesWorkerCandidate,
  targets: VbReferencesWorkerTargetSymbol[],
  settings: AspSettings,
  options: VbReferenceOptions,
  openDocumentsKey: string,
  executionOptions: WorkspaceVbReferenceExecutionOptions,
): string {
  return JSON.stringify({
    targets: targets
      .map((target) => ({
        ...target,
        sourceUri: fileIdentityKeyFromUri(target.sourceUri),
      }))
      .sort((left, right) => JSON.stringify(left).localeCompare(JSON.stringify(right))),
    candidate: {
      ...candidate,
      uri: fileIdentityKeyFromUri(candidate.uri),
      fileName: fileIdentityKeyFromFileName(candidate.fileName),
      source: {
        ...candidate.source,
        fileName: fileIdentityKeyFromFileName(candidate.source.fileName),
      },
    },
    settings: {
      parse: parseSettingsIdentity(settings),
      include: includeResolutionSettingsIdentity(settings),
      vbscript: vbProjectContextSettings(settings),
      legacyEncoding: settings.legacyEncoding,
    },
    options,
    executionOptions,
    workspaceGeneration,
    openDocuments: openDocumentsKey,
  });
}

function sameVbReferencesWorkerCandidate(
  left: VbReferencesWorkerCandidate,
  right: VbReferencesWorkerCandidate,
): boolean {
  return (
    sameFileIdentityUri(left.uri, right.uri) &&
    fileIdentityKeyFromFileName(left.fileName) === fileIdentityKeyFromFileName(right.fileName) &&
    fileIdentityKeyFromFileName(left.source.fileName) ===
      fileIdentityKeyFromFileName(right.source.fileName) &&
    left.source.mtimeMs === right.source.mtimeMs &&
    left.source.size === right.source.size &&
    left.source.openVersion === right.source.openVersion
  );
}

async function isCurrentVbReferencesWorkerCandidate(
  candidate: VbReferencesWorkerCandidate,
  settings: AspSettings,
): Promise<boolean> {
  const document = openDocumentForFileName(candidate.fileName);
  if (document) {
    return (
      candidate.source.openVersion === document.version &&
      candidate.source.size === document.getText().length
    );
  }
  if (cacheFreshness(settings) === "watch") {
    const source = sourceMetadataFromManifest(candidate.fileName);
    if (source) {
      return (
        candidate.source.openVersion === undefined &&
        candidate.source.mtimeMs === source.mtimeMs &&
        candidate.source.size === source.size
      );
    }
  }
  const stat = await fsGateway.statAsync(candidate.fileName);
  return Boolean(
    stat?.isFile() &&
    candidate.source.openVersion === undefined &&
    candidate.source.mtimeMs === stat.mtimeMs &&
    candidate.source.size === stat.size,
  );
}

function pruneWorkspaceVbReferenceWorkerCompleted(): void {
  while (workspaceVbReferenceWorkerCompleted.size > maxWorkspaceVbReferenceWorkerCacheEntries) {
    const oldest = workspaceVbReferenceWorkerCompleted.keys().next().value;
    if (!oldest) {
      return;
    }
    workspaceVbReferenceWorkerCompleted.delete(oldest);
  }
  checkMemoryPressure(globalSettings, "vb.references.worker.prune");
}

function pruneWorkspaceVbReferenceRequestCompleted(): void {
  while (workspaceVbReferenceRequestCompleted.size > maxWorkspaceVbReferenceRequestCacheEntries) {
    const oldest = [...workspaceVbReferenceRequestCompleted.entries()].sort(
      (left, right) => left[1].lastUsed - right[1].lastUsed,
    )[0];
    if (!oldest) {
      return;
    }
    workspaceVbReferenceRequestCompleted.delete(oldest[0]);
  }
  checkMemoryPressure(globalSettings, "vb.references.request.prune");
}

function pruneWorkspaceVbReferenceBatchCompleted(): void {
  while (workspaceVbReferenceBatchCompleted.size > maxWorkspaceVbReferenceBatchCacheEntries) {
    const oldest = workspaceVbReferenceBatchCompleted.keys().next().value;
    if (!oldest) {
      return;
    }
    workspaceVbReferenceBatchCompleted.delete(oldest);
  }
  checkMemoryPressure(globalSettings, "vb.references.batch.prune");
}

function pruneWorkspaceVbReferenceReachabilityCache(): void {
  while (
    workspaceVbReferenceReachabilityCache.size > maxWorkspaceVbReferenceReachabilityCacheEntries
  ) {
    const oldest = [...workspaceVbReferenceReachabilityCache.entries()].sort(
      (left, right) => left[1].lastUsed - right[1].lastUsed,
    )[0];
    if (!oldest) {
      return;
    }
    workspaceVbReferenceReachabilityCache.delete(oldest[0]);
  }
  checkMemoryPressure(globalSettings, "vb.references.reachability.prune");
}

function clearWorkspaceVbReferenceCaches(): void {
  workspaceVbReferenceWorkerInFlight.clear();
  workspaceVbReferenceWorkerBatchInFlight.clear();
  workspaceVbReferenceWorkerCompleted.clear();
  workspaceVbReferenceRequestInFlight.clear();
  workspaceVbReferenceRequestCompleted.clear();
  workspaceVbReferenceBatchInFlight.clear();
  workspaceVbReferenceBatchCompleted.clear();
  workspaceVbReferenceReachabilityInFlight.clear();
  workspaceVbReferenceReachabilityCache.clear();
}

function getVbReferencesWorkerPool(settings: AspSettings): VbReferencesWorkerPool {
  vbReferencesWorkerPool ??= new VbReferencesWorkerPool();
  vbReferencesWorkerPool.resize(analysisConcurrency(settings));
  return vbReferencesWorkerPool;
}

function getBulkWorkerPool(): BulkWorkerPool {
  bulkWorkerPool ??= new BulkWorkerPool();
  bulkWorkerPool.resize(1);
  return bulkWorkerPool;
}

function equivalentVbSymbol(symbols: VbSymbol[], target: VbSymbol): VbSymbol | undefined {
  return symbols.find((symbol) => sameVbSymbolIdentity(symbol, target));
}

function sameVbSymbolIdentity(left: VbSymbol, right: VbSymbol): boolean {
  return (
    sameFileIdentityUri(left.sourceUri, right.sourceUri) &&
    left.name.toLowerCase() === right.name.toLowerCase() &&
    left.kind === right.kind &&
    (left.memberOf ?? "").toLowerCase() === (right.memberOf ?? "").toLowerCase() &&
    left.range.start.line === right.range.start.line &&
    left.range.start.character === right.range.start.character &&
    left.range.end.line === right.range.end.line &&
    left.range.end.character === right.range.end.character
  );
}

function sameVbReferenceTargetIdentity(
  left: VbSymbol,
  right: VbReferencesWorkerTargetSymbol,
): boolean {
  return (
    sameFileIdentityUri(left.sourceUri, right.sourceUri) &&
    left.name.toLowerCase() === right.name.toLowerCase() &&
    left.kind === right.kind &&
    (left.memberOf ?? "").toLowerCase() === (right.memberOf ?? "").toLowerCase() &&
    left.range.start.line === right.range.start.line &&
    left.range.start.character === right.range.start.character &&
    left.range.end.line === right.range.end.line &&
    left.range.end.character === right.range.end.character
  );
}

function isGlobalWorkspaceReferenceFallbackTarget(symbol: VbReferencesWorkerTargetSymbol): boolean {
  return (
    !symbol.scopeName &&
    !symbol.memberOf &&
    symbol.visibility !== "private" &&
    ["function", "sub", "class", "variable", "constant"].includes(symbol.kind)
  );
}

function addVbReferences(target: Map<string, VbReference>, references: VbReference[]): void {
  for (const reference of references) {
    target.set(vbReferenceKey(reference), reference);
  }
}

function addVbReferencesToArray(
  target: Map<string, VbReference[]>,
  key: string,
  references: VbReference[],
): void {
  const existing = target.get(key) ?? [];
  for (const reference of references) {
    existing.push(reference);
  }
  target.set(key, existing);
}

function dedupeVbReferences(references: VbReference[]): VbReference[] {
  const deduped = new Map<string, VbReference>();
  addVbReferences(deduped, references);
  return [...deduped.values()];
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

function fallbackWorkspaceExternalReferences(
  summary: FileAnalysisSummary,
  symbol: VbSymbol,
): VbReference[] {
  if (!isGlobalWorkspaceReferenceFallbackSymbol(symbol)) {
    return [];
  }
  return summaryVbReferenceUsages(summary)
    .filter((usage) => usage.key === symbol.name.toLowerCase())
    .flatMap((usage) => usage.ranges.map((range) => ({ uri: summary.uri, range })));
}

function summaryVbReferenceUsages(
  summary: FileAnalysisSummary,
): NonNullable<NonNullable<FileAnalysisSummary["vbscript"]>["externalRefUsages"]> {
  return summary.vbscript?.externalRefUsages ?? [];
}

function isGlobalWorkspaceReferenceFallbackSymbol(symbol: VbSymbol): boolean {
  return (
    !symbol.scopeName &&
    !symbol.memberOf &&
    symbol.visibility !== "private" &&
    ["function", "sub", "class"].includes(symbol.kind)
  );
}

async function refreshIncludeStateForAspChangesAsync(
  changes: WatchedAspFileChange[],
): Promise<IncludeStateRefreshResult> {
  const includeRefsChangedFiles = new Set<string>();
  const publicChangedFiles = new Set<string>();
  const previousIncludeRefs = new Map<string, IncludeRefsCacheEntry | undefined>();
  for (const change of changes) {
    const fileName = normalizeFileName(change.fileName);
    previousIncludeRefs.set(
      fileIdentityKeyFromFileName(fileName),
      includeDocumentLoader.cachedIncludeRefs(fileName),
    );
  }
  includeDocumentLoader.invalidateFiles(changes.map((change) => change.fileName));
  invalidateGraphFileIndexFiles(changes.map((change) => change.fileName));
  for (const change of changes) {
    const fileName = normalizeFileName(change.fileName);
    const fileKey = fileIdentityKeyFromFileName(fileName);
    const previous = includeDocumentLoader.cachedPublicSummary(fileName);
    const uri = pathToFileUri(fileName);
    const settings = cachedSettings(uri);
    const nextIncludeRefs =
      change.type === FileChangeType.Deleted
        ? undefined
        : await includeDocumentLoader.readIncludeRefsAsync(fileName, settings, {
            allowRead: true,
          });
    const previousIncludeFingerprint = previousIncludeRefs.get(fileKey)?.fingerprint ?? "missing";
    const nextIncludeFingerprint = nextIncludeRefs?.fingerprint ?? "missing";
    if (previousIncludeFingerprint === nextIncludeFingerprint) {
      logDebugSummary(
        settings,
        `[asp-lsp] include.refs.reuse: ${uri}, fingerprint=${nextIncludeFingerprint}`,
      );
    } else {
      includeRefsChangedFiles.add(fileKey);
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
    publicChangedFiles.add(fileKey);
    aspProjectBuilderState.markFileAffected(fileName, "watchedAsp.publicBoundary");
    logInvalidation(
      "includePublicBoundary",
      `watchedAsp.changed, uri=${uri}, previous=${previous?.publicFingerprint ?? "missing"}, next=${nextFingerprint}`,
    );
  }
  return { includeRefsChangedFiles, publicChangedFiles };
}

async function ensureIncludeGraphForOpenDocumentsAsync(changedFiles: Set<string>): Promise<void> {
  const changedFileKeys = new Set([...changedFiles].map(fileIdentityKeyFromFileName));
  for (const document of documents.all()) {
    const cached = await ensureFreshDiagnosticsCachedDocumentAsync(document);
    const ownerKey = fileIdentityKeyFromUri(cached.source.uri);
    const affected =
      changedFileKeys.has(ownerKey) ||
      workspaceIncludeGraph.dependsOnAnyTarget(uriToFileName(cached.source.uri), changedFiles, {
        transitive: true,
      });
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
  const visited = new Set<string>([fileIdentityKeyFromUri(cached.source.uri)]);
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
    resetIncludeDependencies(ownerUri);
    await prefetchIncludeRefsForOwnerAsync(ownerUri, includeRefs, settings);
    for (const include of includeRefs) {
      const resolved = await resolveIncludePathDetailsAsync(
        ownerUri,
        include.path,
        include.mode,
        settings,
      );
      const includeUri = pathToFileUri(resolved.fileName);
      recordIncludeDependency(ownerUri, includeUri);
      const includeKey = fileIdentityKeyFromFileName(resolved.fileName);
      if (!resolved.exists || visited.has(includeKey)) {
        continue;
      }
      if (visited.size >= limits.maxDocuments) {
        noteTruncated(`documents>${limits.maxDocuments}`);
        continue;
      }
      const size = await fileSizeAsync(resolved.fileName, settings);
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
      visited.add(fileIdentityKeyFromFileName(entry.fileName));
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

function affectedOpenUrisForAspChanges(
  changes: WatchedAspFileChange[],
  publicChangedFiles: Set<string>,
): Set<string> {
  const openUrisByFileKey = openDocumentUrisByFileKey();
  const affected = new Set<string>();
  for (const change of changes) {
    const changedKey = fileIdentityKeyFromFileName(change.fileName);
    const changedOpenUri = openUrisByFileKey.get(changedKey);
    if (changedOpenUri) {
      affected.add(changedOpenUri);
    }
    if (!publicChangedFiles.has(fileIdentityKeyFromFileName(change.fileName))) {
      continue;
    }
    for (const dependentFileName of workspaceIncludeGraph.dependentFileNamesForTargets(
      [change.fileName],
      { transitive: true },
    )) {
      const dependentKey = fileIdentityKeyFromFileName(dependentFileName);
      const dependentUri = openUrisByFileKey.get(dependentKey);
      if (dependentUri) {
        affected.add(dependentUri);
      }
    }
  }
  return affected;
}

function openDocumentUris(): Set<string> {
  return new Set(documents.all().map((document) => document.uri));
}

function openDocumentUrisByFileKey(): Map<string, string> {
  return new Map(
    documents.all().map((document) => [fileIdentityKeyFromUri(document.uri), document.uri]),
  );
}

function clearIncludeGraph(): void {
  clearIncludeDependencies();
  includePublicSummaries.clear();
  aspProjectBuilderState.clear();
}

function resetIncludeDependencies(ownerUriOrFileName: string): void {
  workspaceIncludeGraph.delete(includeDependencyFileName(ownerUriOrFileName));
}

function recordIncludeDependency(ownerUriOrFileName: string, includeUriOrFileName: string): void {
  workspaceIncludeGraph.recordEphemeralDependency(
    includeDependencyFileName(ownerUriOrFileName),
    includeDependencyFileName(includeUriOrFileName),
  );
}

function clearIncludeDependencies(): void {
  workspaceIncludeGraph.clearEphemeral();
}

function includeDependencyFileName(uriOrFileName: string): string {
  return normalizeFileName(
    uriOrFileName.startsWith("file://") ? uriToFileName(uriOrFileName) : uriOrFileName,
  );
}

function invalidateCachedAnalysisForUris(uris: Set<string>, reason = "analysis.invalidate"): void {
  if (uris.size > 0) {
    invalidateAspGraphPayloadCache(reason);
    vbProjectContextCache.clear();
    vbCanonicalContextSymbolsCache.clear();
    clearWorkspaceDiagnosticsCaches();
    clearInteractiveVbProjectContextSnapshotsForUris(uris);
    clearWorkspaceVbReferenceCaches();
    completionSessionCache.clearUris(uris, reason);
    invalidateGraphFileIndexFiles(
      [...uris].filter((uri) => uri.startsWith("file://")).map(uriToFileName),
    );
    logInvalidation("analysis", `${reason}, files=${uris.size}`);
  }
  for (const uri of uris) {
    for (const cached of cachedDocumentsForUri(uri)) {
      cached.analysis = undefined;
    }
    clearSemanticTokensForUri(uri);
  }
}

function requestSemanticTokensRefresh(reason: string): void {
  if (semanticTokensRefreshSupported) {
    try {
      void Promise.resolve(connection.languages.semanticTokens.refresh()).catch((error: unknown) =>
        logServerWarning(
          `[asp-lsp] semanticTokens.refresh.failed: reason=${reason}, error=${errorMessage(error)}`,
        ),
      );
    } catch (error) {
      logServerWarning(
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
        logServerWarning(
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
    deadCodeDiagnostics: settings.vbscript?.deadCodeDiagnostics !== false,
    syntaxSnippets: settings.vbscript?.syntaxSnippets !== false,
    syntaxKeywords: settings.vbscript?.syntaxKeywords !== false,
    incrementalAnalysis: settings.incremental?.analysis !== false,
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
      deadCodeDiagnostics: settings.vbscript?.deadCodeDiagnostics !== false,
      syntaxSnippets: settings.vbscript?.syntaxSnippets !== false,
      syntaxKeywords: settings.vbscript?.syntaxKeywords !== false,
      incrementalAnalysis: settings.incremental?.analysis !== false,
      builtinRuntime: vbBuiltinRuntimeForCached(cached),
    },
    globals: settings.vbscript?.globals,
  });
}

function rememberVbProjectContext(key: string, context: VbProjectContext): void {
  vbProjectContextCache.set(key, { context, lastUsed: Date.now() });
  if (vbProjectContextCache.size > maxVbProjectContextCacheEntries) {
    const oldest = [...vbProjectContextCache.entries()].sort(
      (left, right) => left[1].lastUsed - right[1].lastUsed,
    )[0]?.[0];
    if (oldest) {
      vbProjectContextCache.delete(oldest);
    }
  }
  checkMemoryPressure(globalSettings, "vb.projectContext.remember");
}

function vbProjectContextCacheKey(documents: AspParsedDocument[], settings: AspSettings): string {
  return JSON.stringify({
    documents: documents.map((document) => ({
      uri: document.uri,
      standaloneVbscript: isStandaloneVbscriptSource(document.uri),
      vbscript: vbProjectDocumentFingerprint(document),
    })),
    settings: {
      typeChecking: settings.vbscript?.typeChecking,
      ifSyntaxDiagnostics: settings.vbscript?.ifSyntaxDiagnostics ?? "basic",
      identifierCase: settings.vbscript?.identifierCase,
      identifierCaseByKind: settings.vbscript?.identifierCaseByKind,
      comTypes: settings.vbscript?.comTypes,
      unusedDiagnostics: settings.vbscript?.unusedDiagnostics !== false,
      deadCodeDiagnostics: settings.vbscript?.deadCodeDiagnostics !== false,
      syntaxSnippets: settings.vbscript?.syntaxSnippets !== false,
      syntaxKeywords: settings.vbscript?.syntaxKeywords !== false,
      incrementalAnalysis: settings.incremental?.analysis !== false,
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
        text: computeTextFingerprint(document.text.slice(region.contentStart, region.contentEnd)),
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
  const contextSettings = withCachedVbBuiltinRuntime(cached, vbProjectContextSettings(settings));
  const summaries = graph.summaries;
  let symbols = summaries.flatMap((summary) => summary.vbscript?.localSymbols ?? []);
  symbols = await canonicalizeImplicitGlobalContextSymbolsCachedAsync(
    graph,
    contextSettings,
    summaries,
    symbols,
    settings,
  );
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
    externalRefUsages: summaries.flatMap((summary) => summaryVbReferenceUsages(summary)),
  };
  analysisFor(cached).vbProjectAnalysis = { key, analysis };
  return analysis;
}

async function canonicalizeImplicitGlobalContextSymbolsCachedAsync(
  graph: VbProjectSummaryGraph,
  contextSettings: VbProjectContext,
  summaries: FileAnalysisSummary[],
  symbols: VbSymbol[],
  settings: AspSettings,
): Promise<VbSymbol[]> {
  const key = JSON.stringify({
    graph: graph.key,
    context: {
      typeChecking: contextSettings.typeChecking,
      ifSyntaxDiagnostics: contextSettings.ifSyntaxDiagnostics,
      identifierCase: contextSettings.identifierCase,
      identifierCaseByKind: contextSettings.identifierCaseByKind,
      comTypes: contextSettings.comTypes,
      unusedDiagnostics: contextSettings.unusedDiagnostics,
      deadCodeDiagnostics: contextSettings.deadCodeDiagnostics,
      syntaxSnippets: contextSettings.syntaxSnippets,
      syntaxKeywords: contextSettings.syntaxKeywords,
      incrementalAnalysis: contextSettings.incrementalAnalysis,
    },
    globals: settings.vbscript?.globals,
  });
  const cached = vbCanonicalContextSymbolsCache.get(key);
  if (cached) {
    cached.lastUsed = Date.now();
    logDebugSummary(settings, "[asp-lsp] asp.graph.implicitGlobals.canonicalize.cacheHit");
    return [...cached.symbols];
  }
  const canonical = await canonicalizeImplicitGlobalContextSymbolsAsync(
    summaries,
    symbols,
    settings,
  );
  vbCanonicalContextSymbolsCache.set(key, { symbols: canonical, lastUsed: Date.now() });
  pruneVbCanonicalContextSymbolsCache();
  return [...canonical];
}

function pruneVbCanonicalContextSymbolsCache(): void {
  while (vbCanonicalContextSymbolsCache.size > vbCanonicalContextSymbolsCacheMaxEntries) {
    const oldest = [...vbCanonicalContextSymbolsCache.entries()].sort(
      (left, right) => left[1].lastUsed - right[1].lastUsed,
    )[0]?.[0];
    if (!oldest) {
      return;
    }
    vbCanonicalContextSymbolsCache.delete(oldest);
  }
}

function vbProjectAnalysisCacheKey(graph: VbProjectSummaryGraph, settings: AspSettings): string {
  return JSON.stringify({
    graph: graph.key,
    context: vbProjectContextSettings(settings),
    globals: settings.vbscript?.globals,
  });
}

async function canonicalizeImplicitGlobalContextSymbolsAsync(
  summaries: FileAnalysisSummary[],
  symbols: VbSymbol[],
  settings: AspSettings,
): Promise<VbSymbol[]> {
  if (summaries.length < 2 || symbols.length < 2) {
    return symbols;
  }
  const startedAt = process.hrtime.bigint();
  try {
    const entriesByName = new Map<string, ContextImplicitGlobalSymbolEntry[]>();
    for (let order = 0; order < symbols.length; order += 1) {
      const symbol = symbols[order];
      if (!isContextImplicitGlobalMergeSymbol(symbol) || !symbol.sourceUri.startsWith("file://")) {
        continue;
      }
      pushAspGraphMapItem(entriesByName, symbol.name.toLowerCase(), {
        symbol,
        order,
        fileKey: fileIdentityKeyFromUri(symbol.sourceUri),
      });
    }
    if (entriesByName.size === 0) {
      return symbols;
    }
    const summary = implicitGlobalGroupSummary(entriesByName.values());
    logDebugSummary(
      settings,
      `[asp-lsp] asp.graph.implicitGlobals.context.groups: groups=${summary.groups}, maxGroupSize=${summary.maxGroupSize}`,
    );
    const includeGraph = await workspaceVbReferenceSummaryIncludeGraph(summaries, settings);
    const targetKeys = new Set<string>();
    for (const entries of entriesByName.values()) {
      for (const entry of entries) {
        targetKeys.add(entry.fileKey);
      }
    }
    const reachability = precomputeIncludeReachability(includeGraph, targetKeys);
    const union = new ImplicitGlobalUnionFind();
    for (const entries of entriesByName.values()) {
      if (entries.length < 2 || !entries.some((entry) => entry.symbol.implicit === true)) {
        continue;
      }
      for (let leftIndex = 0; leftIndex < entries.length; leftIndex += 1) {
        const left = entries[leftIndex];
        for (let rightIndex = leftIndex + 1; rightIndex < entries.length; rightIndex += 1) {
          const right = entries[rightIndex];
          const leftId = String(left.order);
          const rightId = String(right.order);
          if (union.find(leftId) === union.find(rightId)) {
            continue;
          }
          if (
            isContextImplicitGlobalSymbolVisibleFromFile(
              includeGraph,
              left.fileKey,
              right,
              left.symbol.range,
              reachability,
            ) ||
            isContextImplicitGlobalSymbolVisibleFromFile(
              includeGraph,
              right.fileKey,
              left,
              right.symbol.range,
              reachability,
            )
          ) {
            union.union(leftId, rightId);
          }
        }
      }
    }
    const entriesByRoot = new Map<string, ContextImplicitGlobalSymbolEntry[]>();
    for (const entries of entriesByName.values()) {
      for (const entry of entries) {
        const id = String(entry.order);
        const root = union.find(id);
        if (root !== id || union.size(root) > 1) {
          pushAspGraphMapItem(entriesByRoot, root, entry);
        }
      }
    }
    const canonicalOrderByOrder = new Map<number, number>();
    for (const entries of entriesByRoot.values()) {
      const canonical = contextImplicitGlobalCanonicalSymbol(entries, includeGraph, reachability);
      for (const entry of entries) {
        canonicalOrderByOrder.set(entry.order, canonical.order);
      }
    }
    if (![...canonicalOrderByOrder].some(([order, canonicalOrder]) => order !== canonicalOrder)) {
      return symbols;
    }
    return symbols.filter((_, index) => {
      const canonicalOrder = canonicalOrderByOrder.get(index);
      return canonicalOrder === undefined || canonicalOrder === index;
    });
  } finally {
    finishDebugStep(settings, "workspace", "asp.graph.implicitGlobals.canonicalize", startedAt);
  }
}

interface ContextImplicitGlobalSymbolEntry {
  symbol: VbSymbol;
  order: number;
  fileKey: string;
}

class ImplicitGlobalUnionFind {
  private readonly parents = new Map<string, string>();
  private readonly sizes = new Map<string, number>();

  find(id: string): string {
    const parent = this.parents.get(id);
    if (!parent) {
      this.parents.set(id, id);
      this.sizes.set(id, 1);
      return id;
    }
    if (parent === id) {
      return id;
    }
    const root = this.find(parent);
    this.parents.set(id, root);
    return root;
  }

  union(left: string, right: string): void {
    const leftRoot = this.find(left);
    const rightRoot = this.find(right);
    if (leftRoot === rightRoot) {
      return;
    }
    const leftSize = this.sizes.get(leftRoot) ?? 1;
    const rightSize = this.sizes.get(rightRoot) ?? 1;
    const [parent, child, size] =
      leftSize >= rightSize
        ? [leftRoot, rightRoot, leftSize + rightSize]
        : [rightRoot, leftRoot, leftSize + rightSize];
    this.parents.set(child, parent);
    this.sizes.set(parent, size);
    this.sizes.delete(child);
  }

  size(id: string): number {
    return this.sizes.get(this.find(id)) ?? 1;
  }
}

function implicitGlobalGroupSummary<T>(groups: Iterable<readonly T[]>): {
  groups: number;
  maxGroupSize: number;
} {
  let groupCount = 0;
  let maxGroupSize = 0;
  for (const group of groups) {
    groupCount += 1;
    maxGroupSize = Math.max(maxGroupSize, group.length);
  }
  return { groups: groupCount, maxGroupSize };
}

function isContextImplicitGlobalMergeSymbol(symbol: VbSymbol): boolean {
  return symbol.kind === "variable" && !symbol.memberOf && !symbol.scopeName;
}

function contextImplicitGlobalCanonicalSymbol(
  entries: ContextImplicitGlobalSymbolEntry[],
  includeGraph: WorkspaceVbReferenceSummaryIncludeGraph,
  reachability: PrecomputedIncludeReachability,
): ContextImplicitGlobalSymbolEntry {
  const visibilityScoreByOrder = new Map(
    entries.map((entry) => [
      entry.order,
      contextImplicitGlobalCanonicalVisibilityScore(entry, entries, includeGraph, reachability),
    ]),
  );
  return [...entries].sort(
    (left, right) =>
      (visibilityScoreByOrder.get(left.order) ?? 1) -
        (visibilityScoreByOrder.get(right.order) ?? 1) ||
      contextImplicitGlobalCanonicalScore(left.symbol) -
        contextImplicitGlobalCanonicalScore(right.symbol) ||
      left.order - right.order,
  )[0];
}

function contextImplicitGlobalCanonicalVisibilityScore(
  entry: ContextImplicitGlobalSymbolEntry,
  entries: ContextImplicitGlobalSymbolEntry[],
  includeGraph: WorkspaceVbReferenceSummaryIncludeGraph,
  reachability: PrecomputedIncludeReachability,
): number {
  return entries.every(
    (candidate) =>
      candidate.order === entry.order ||
      isContextImplicitGlobalSymbolVisibleFromFile(
        includeGraph,
        candidate.fileKey,
        entry,
        candidate.symbol.range,
        reachability,
      ),
  )
    ? 0
    : 1;
}

function contextImplicitGlobalCanonicalScore(symbol: VbSymbol): number {
  return symbol.implicit === true ? 1 : 0;
}

function isContextImplicitGlobalSymbolVisibleFromFile(
  includeGraph: WorkspaceVbReferenceSummaryIncludeGraph,
  ownerKey: string,
  declaration: ContextImplicitGlobalSymbolEntry,
  referenceRange: Range,
  reachability: PrecomputedIncludeReachability,
): boolean {
  if (declaration.fileKey === ownerKey) {
    return true;
  }
  if (
    hasEarlierReachableSummaryInclude(
      includeGraph,
      ownerKey,
      declaration.fileKey,
      referenceRange,
      reachability,
    )
  ) {
    return true;
  }
  return isContextImplicitGlobalSymbolVisibleFromParentContext(
    includeGraph,
    ownerKey,
    declaration,
    reachability,
    new Set([ownerKey]),
  );
}

function isContextImplicitGlobalSymbolVisibleFromParentContext(
  includeGraph: WorkspaceVbReferenceSummaryIncludeGraph,
  ownerKey: string,
  declaration: ContextImplicitGlobalSymbolEntry,
  reachability: PrecomputedIncludeReachability,
  visited: Set<string>,
): boolean {
  for (const parentInclude of includeGraph.parentIncludesByTargetKey.get(ownerKey) ?? []) {
    if (visited.has(parentInclude.ownerKey)) {
      continue;
    }
    visited.add(parentInclude.ownerKey);
    if (
      isContextImplicitGlobalSymbolVisibleBeforeParentInclude(
        includeGraph,
        parentInclude.ownerKey,
        declaration,
        parentInclude.range,
        reachability,
        visited,
      )
    ) {
      visited.delete(parentInclude.ownerKey);
      return true;
    }
    visited.delete(parentInclude.ownerKey);
  }
  return false;
}

function isContextImplicitGlobalSymbolVisibleBeforeParentInclude(
  includeGraph: WorkspaceVbReferenceSummaryIncludeGraph,
  parentKey: string,
  declaration: ContextImplicitGlobalSymbolEntry,
  includeRange: Range,
  reachability: PrecomputedIncludeReachability,
  visited: Set<string>,
): boolean {
  if (declaration.fileKey === parentKey) {
    return positionBeforeOrEqual(declaration.symbol.range.start, includeRange.start);
  }
  if (
    hasEarlierReachableSummaryInclude(
      includeGraph,
      parentKey,
      declaration.fileKey,
      includeRange,
      reachability,
    )
  ) {
    return true;
  }
  return isContextImplicitGlobalSymbolVisibleFromParentContext(
    includeGraph,
    parentKey,
    declaration,
    reachability,
    visited,
  );
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
      deadCodeDiagnostics: context.deadCodeDiagnostics,
      syntaxSnippets: context.syntaxSnippets,
      syntaxKeywords: context.syntaxKeywords,
      incrementalAnalysis: context.incrementalAnalysis,
      builtinRuntime: context.builtinRuntime,
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
  const visited = new Set<string>([fileIdentityKeyFromUri(cached.source.uri)]);
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
    resetIncludeDependencies(document.uri);
    for (const include of document.includes) {
      const resolved = await resolveIncludePathDetailsAsync(
        document.uri,
        include.path,
        include.mode,
        settings,
      );
      const includeUri = pathToFileUri(resolved.fileName);
      recordIncludeDependency(document.uri, includeUri);
      const includeKey = fileIdentityKeyFromFileName(resolved.fileName);
      if (!resolved.exists || visited.has(includeKey)) {
        continue;
      }
      if (documents.length >= limits.maxDocuments) {
        noteTruncated(`documents>${limits.maxDocuments}`);
        continue;
      }
      const size = await fileSizeAsync(resolved.fileName, settings);
      if (size !== undefined && textLength + size > limits.maxTextLength) {
        noteTruncated(`text>${limits.maxTextLength}`);
        continue;
      }
      const entry = await includeDocumentLoader.readAsync(resolved.fileName, settings);
      if (!entry) {
        continue;
      }
      visited.add(fileIdentityKeyFromFileName(entry.fileName));
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
  const pending = pendingVbProjectSummaryGraphs.get(collectionKey);
  if (pending && (pending.allowReadMissing || !options.allowReadMissing)) {
    const graph = await pending.promise;
    analysisFor(cached).vbProjectSummaryGraph = { collectionKey, graph };
    analysisFor(cached).vbProjectDocuments = {
      collectionKey,
      documents: graph.documents,
    };
    logDebugSummary(
      settings,
      `[asp-lsp] vbProject.summaryGraph.pendingReuse: complete=${graph.complete}, summaries=${graph.summaries.length}`,
    );
    return graph;
  }
  const promise = measureDebugStepAsync(
    settings,
    cached.source.uri,
    "vbProject.summaryGraph.collect",
    () => collectVbProjectSummaryGraphAsync(cached, settings, options),
  );
  pendingVbProjectSummaryGraphs.set(collectionKey, {
    allowReadMissing: options.allowReadMissing,
    promise,
  });
  let graph: VbProjectSummaryGraph;
  try {
    graph = await promise;
  } finally {
    if (pendingVbProjectSummaryGraphs.get(collectionKey)?.promise === promise) {
      pendingVbProjectSummaryGraphs.delete(collectionKey);
    }
  }
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
  const contextSettings = withCachedVbBuiltinRuntime(cached, vbProjectContextSettings(settings));
  const rootSummary = await cachedFileAnalysisSummaryAsync(cached, contextSettings, settings);
  const seededGraph = seededVbProjectSummaryGraph(cached, settings, rootSummary);
  if (seededGraph && (seededGraph.complete || !options.allowReadMissing)) {
    logDebugSummary(
      settings,
      `[asp-lsp] vbProject.summaryGraph.seedReuse: ${cached.source.uri}, summaries=${seededGraph.summaries.length}, complete=${seededGraph.complete}`,
    );
    return seededGraph;
  }
  const summaries: FileAnalysisSummary[] = [rootSummary];
  const projectDocuments: AspParsedDocument[] = [cached.parsed];
  const visited = new Set<string>([fileIdentityKeyFromUri(cached.source.uri)]);
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
    resetIncludeDependencies(owner.uri);
    for (const include of owner.includeRefs) {
      const resolved = await resolveIncludePathDetailsAsync(
        owner.uri,
        include.path,
        include.mode,
        settings,
      );
      const includeUri = pathToFileUri(resolved.fileName);
      recordIncludeDependency(owner.uri, includeUri);
      const includeKey = fileIdentityKeyFromFileName(resolved.fileName);
      if (!resolved.exists || visited.has(includeKey)) {
        continue;
      }
      if (summaries.length >= limits.maxDocuments) {
        noteTruncated(`documents>${limits.maxDocuments}`);
        continue;
      }
      const size = await fileSizeAsync(resolved.fileName, settings);
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
      visited.add(fileIdentityKeyFromFileName(entry.fileName));
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

function seededVbProjectSummaryGraph(
  cached: CachedDocument,
  settings: AspSettings,
  rootSummary: FileAnalysisSummary,
): VbProjectSummaryGraph | undefined {
  const seed = cached.analysis?.vbProjectSummaryGraphSeed;
  const collectionKey = vbProjectDocumentCollectionKey(cached, settings);
  if (!seed || seed.collectionKey !== collectionKey) {
    return undefined;
  }
  if (!sameFileIdentityUri(seed.graph.rootSummary.uri, cached.source.uri)) {
    return undefined;
  }
  if (!sameSummaryIncludeRefs(seed.graph.rootSummary.includeRefs, rootSummary.includeRefs)) {
    return undefined;
  }
  const summaries = [
    rootSummary,
    ...seed.graph.summaries.filter(
      (summary) => !sameFileIdentityUri(summary.uri, cached.source.uri),
    ),
  ];
  const documents = [
    cached.parsed,
    ...seed.graph.documents.filter(
      (document) => !sameFileIdentityUri(document.uri, cached.source.uri),
    ),
  ];
  const textLength = Math.max(
    cached.parsed.text.length,
    seed.graph.textLength - seed.rootTextLength + cached.parsed.text.length,
  );
  return {
    rootSummary,
    summaries,
    documents,
    key: vbProjectSummaryGraphKey(rootSummary, summaries, {
      complete: seed.graph.complete,
      missingFiles: seed.graph.missingFiles,
      truncatedReason: seed.graph.truncatedReason,
      textLength,
      settings,
    }),
    complete: seed.graph.complete,
    missingFiles: seed.graph.missingFiles,
    truncatedReason: seed.graph.truncatedReason,
    textLength,
  };
}

function sameSummaryIncludeRefs(
  left: FileAnalysisSummary["includeRefs"],
  right: FileAnalysisSummary["includeRefs"],
): boolean {
  if (left.length !== right.length) {
    return false;
  }
  return left.every((include, index) => {
    const other = right[index];
    return include.path === other.path && include.mode === other.mode;
  });
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
      const ownerDocument = openDocumentForUri(ownerUri);
      if (ownerDocument) {
        affected.add(ownerDocument.uri);
      }
      for (const dependentFileName of workspaceIncludeGraph.dependentFileNamesForTargets(
        [uriToFileName(entry.uri)],
        { transitive: true },
      )) {
        const dependentKey = fileIdentityKeyFromFileName(dependentFileName);
        const dependent = documents
          .all()
          .find((document) => fileIdentityKeyFromUri(document.uri) === dependentKey);
        if (dependent) {
          affected.add(dependent.uri);
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
      logServerWarning(
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
    languageId: cached.source.languageId,
    builtinRuntime: vbBuiltinRuntimeForCached(cached),
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
      positiveIntegerFromEnv("ASP_LSP_VB_PROJECT_MAX_DOCUMENTS", defaultVbProjectMaxDocuments),
    maxTextLength:
      settings.workspace?.vbProjectMaxTextLength ??
      positiveIntegerFromEnv("ASP_LSP_VB_PROJECT_MAX_TEXT_LENGTH", defaultVbProjectMaxTextLength),
  };
}

function graphIncludeTreeLimits(settings: AspSettings): VbProjectContextLimits {
  return {
    maxDocuments: settings.graph?.includeTreeMaxDocuments ?? defaultVbProjectMaxDocuments,
    maxTextLength: settings.graph?.includeTreeMaxTextLength ?? defaultVbProjectMaxTextLength,
  };
}

function graphOutputLimits(settings: AspSettings): VbProjectContextLimits {
  return {
    maxDocuments: settings.graph?.maxDocuments ?? defaultGraphMaxDocuments,
    maxTextLength: settings.graph?.maxTextLength ?? defaultGraphMaxTextLength,
  };
}

function positiveIntegerFromEnv(name: string, fallback: number): number {
  const value = Number.parseInt(process.env[name] ?? "", 10);
  return Number.isFinite(value) && value > 0 ? Math.floor(value) : fallback;
}

interface IncludeDocumentSourceIdentity {
  key: string;
  uri: string;
  source: DiskAnalysisSourceMetadata;
  text?: string;
  diskBacked: boolean;
}

async function includeDocumentSourceIdentityAsync(
  fileName: string,
  settings: AspSettings,
): Promise<IncludeDocumentSourceIdentity | undefined> {
  const normalized = normalizeFileName(fileName);
  const uri = pathToFileUri(normalized);
  const openDocument = openDocumentForFileName(normalized);
  if (openDocument) {
    const text = openDocument.getText();
    const source = {
      fileName: normalized,
      mtimeMs: openDocument.version,
      size: text.length,
      contentHash: diskContentHash(text),
    };
    return {
      key: JSON.stringify({
        fileName: fileIdentityKeyFromFileName(normalized),
        openVersion: openDocument.version,
        text: textFingerprint(text),
        settings: includeDocumentSettingsIdentity(settings),
      }),
      uri: openDocument.uri,
      source,
      text,
      diskBacked: false,
    };
  }
  if (cacheFreshness(settings) === "watch") {
    const source = sourceMetadataFromManifest(normalized);
    if (source) {
      logDebugSummary(settings, `[asp-lsp] sourceIdentity.watch.hit: ${uri}`);
      return {
        key: JSON.stringify({
          ...source,
          settings: includeDocumentSettingsIdentity(settings),
        }),
        source,
        uri,
        diskBacked: true,
      };
    }
  }
  const stat = await fsGateway.statAsync(normalized);
  if (!stat?.isFile()) {
    forgetSourceMetadata(normalized);
    return undefined;
  }
  const source = {
    fileName: normalized,
    mtimeMs: stat.mtimeMs,
    size: stat.size,
  };
  rememberSourceMetadata(source);
  return {
    key: JSON.stringify({
      ...source,
      fileName: fileIdentityKeyFromFileName(source.fileName),
      settings: includeDocumentSettingsIdentity(settings),
    }),
    uri,
    source,
    diskBacked: true,
  };
}

function includeDocumentSettingsIdentity(settings: AspSettings): string {
  return JSON.stringify({
    summary: "file-analysis-summary-v2",
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
    scanner: "asp-graph-file-index-v7",
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
    fileName: fileIdentityKeyFromFileName(fileName),
    source: diskAnalysisSourceIdentity(source),
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

function includeDocumentCacheEntryFromDisk(
  fileName: string,
  key: string,
  entry: DiskParsedDocumentCacheEntry,
): IncludeDocumentCacheEntry {
  const publicSignature =
    (entry.publicSignature as FilePublicSignature | undefined) ??
    filePublicSignature(entry.summary);
  return {
    key,
    fileName,
    uri: entry.parsed.uri,
    source: entry.source,
    parsed: entry.parsed,
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
  typeHints: Map<string, AspGraphDeclarationTypeHint> = new Map(),
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
    typeHints,
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

function diskParsedDocumentCacheEntry(
  entry: IncludeDocumentCacheEntry,
  settings: AspSettings,
): DiskParsedDocumentCacheEntry {
  return {
    source: entry.source,
    settingsKey: includeSummarySettingsKey(settings),
    parsed: entry.parsed,
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

async function writeIncludeDocumentDiskEntries(
  entry: IncludeDocumentCacheEntry,
  settings: AspSettings,
): Promise<void> {
  if (!diskAnalysisCache.enabled) {
    return;
  }
  const includeRefsEntry = includeRefsCacheEntryFromSummary(entry, settings);
  await Promise.all([
    diskAnalysisCache
      .writeParsedDocument(diskParsedDocumentCacheEntry(entry, settings))
      .then(() => logDebugSummary(settings, `[asp-lsp] diskParsed.write: ${entry.uri}`)),
    diskAnalysisCache
      .writeSummary(diskSummaryCacheEntry(entry, settings))
      .then(() => logDebugSummary(settings, `[asp-lsp] diskSummary.write: ${entry.uri}`)),
    diskAnalysisCache
      .writeIncludeRefs(diskIncludeRefsCacheEntry(includeRefsEntry, settings))
      .then(() => logDebugSummary(settings, `[asp-lsp] diskIncludeRefs.write: ${entry.uri}`)),
  ]).catch((error) => logDiskAnalysisCacheError("diskParsed.write", error));
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
  uri: string,
  text: string,
  key: string,
  source: DiskAnalysisSourceMetadata,
): IncludeRefsCacheEntry {
  const includeRefs = extractAspIncludeRefs(text);
  return {
    key,
    fileName,
    uri,
    source: sourceWithContentHash(source, text),
    includeRefs,
    fingerprint: includeRefsFingerprint(includeRefs),
  };
}

async function createIncludeDocumentCacheEntryAsync(
  fileName: string,
  uri: string,
  text: string,
  settings: AspSettings,
  key: string,
  source: DiskAnalysisSourceMetadata,
): Promise<IncludeDocumentCacheEntry> {
  const parsed = await parseSourceDocumentAsync(uri, text, settings);
  await hydrateVbscriptCst(parsed, settings);
  const summary = await summarizeAspFileAnalysisAsync(
    parsed,
    withUriVbBuiltinRuntime(uri, vbProjectContextSettings(settings)),
  );
  const publicSignature = filePublicSignature(summary);
  return {
    key,
    fileName,
    uri: parsed.uri,
    source: sourceWithContentHash(source, text),
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
  if (fileIdentityKeyFromFileName(left.fileName) !== fileIdentityKeyFromFileName(right.fileName)) {
    return false;
  }
  if (left.contentHash !== undefined && right.contentHash !== undefined) {
    return left.contentHash === right.contentHash;
  }
  return left.mtimeMs === right.mtimeMs && left.size === right.size;
}

function diskAnalysisSourceIdentity(
  source: DiskAnalysisSourceMetadata,
): DiskAnalysisSourceMetadata {
  return {
    ...source,
    fileName: fileIdentityKeyFromFileName(source.fileName),
  };
}

function sourceWithContentHash(
  source: DiskAnalysisSourceMetadata,
  text: string,
): DiskAnalysisSourceMetadata {
  const contentHash = source.contentHash ?? diskContentHash(text);
  return {
    ...source,
    contentHash,
  };
}

function rememberIncludePublicSummary(
  entry: IncludeSummaryCacheEntry,
  settings?: AspSettings,
): void {
  includePublicSummaries.set(fileIdentityKeyFromFileName(entry.fileName), {
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
  const implicitGlobalCandidateNames = [
    ...(summary.vbscript?.implicitGlobalCandidateNames ?? []),
  ].sort();
  const externalRefUsages = summaryVbReferenceUsages(summary).map((usage) => ({
    key: usage.key,
    name: usage.name,
    memberName: usage.memberName,
    kindHint: usage.kindHint,
    count: usage.count,
  }));
  const affectsGlobalScope =
    summary.defaultLanguage === "VBScript" ||
    exports.length > 0 ||
    implicitGlobalCandidateNames.length > 0 ||
    summary.languageRegions.some((region) => region.kind === "server-script");
  const payload = {
    defaultLanguage: summary.defaultLanguage,
    languages,
    regionKinds: [...new Set(summary.languageRegions.map((region) => region.kind))].sort(),
    vbscript: {
      exports,
      implicitGlobalCandidateNames,
    },
    affectsGlobalScope,
  };
  return {
    fingerprint: summary.publicSignatureHash ?? textFingerprint(JSON.stringify(payload)),
    defaultLanguage: summary.defaultLanguage,
    languages,
    exports,
    implicitGlobalCandidateNames,
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
  const resolvedIncludes = await measureDebugStepAsync(
    settings,
    cached.source.uri,
    "includeDiagnostics.directIncludes",
    async () => {
      const items: Array<{
        include: (typeof cached.parsed.includes)[number];
        resolved: Awaited<ReturnType<typeof resolveIncludePathDetailsAsync>>;
      }> = [];
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
        items.push({ include, resolved });
      }
      return items;
    },
  );
  let cycleGraph: IncludeCycleSummaryGraph | undefined;
  for (const { include, resolved } of resolvedIncludes) {
    const cycle = await measureDebugStepAsync(
      settings,
      cached.source.uri,
      "includeDiagnostics.cycleGraph",
      async () => {
        cycleGraph ??= await includeCycleSummaryGraphAsync(cached, settings);
        return cycleGraph
          ? findIncludeCycleInSummaryGraph(cycleGraph, owner, resolved.fileName)
          : await findIncludeCycleAsync(owner, resolved.fileName, settings, cancellation);
      },
    );
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

interface IncludeCycleSummaryGraph {
  adjacency: Map<string, string[]>;
  fileNamesByKey: Map<string, string>;
}

async function includeCycleSummaryGraphAsync(
  cached: CachedDocument,
  settings: AspSettings,
): Promise<IncludeCycleSummaryGraph | undefined> {
  const graph = await collectCachedVbProjectSummaryGraphAsync(cached, settings, {
    allowReadMissing: true,
  });
  if (!graph.complete) {
    return undefined;
  }
  const fileNamesByKey = new Map<string, string>();
  for (const summary of graph.summaries) {
    fileNamesByKey.set(fileIdentityKeyFromUri(summary.uri), uriToFileName(summary.uri));
  }
  const adjacency = new Map<string, string[]>();
  for (const summary of graph.summaries) {
    const ownerKey = fileIdentityKeyFromUri(summary.uri);
    const targets: string[] = [];
    for (const include of summary.includeRefs) {
      const resolved = await resolveIncludePathDetailsAsync(
        summary.uri,
        include.path,
        include.mode,
        settings,
      );
      if (!resolved.exists) {
        continue;
      }
      const targetKey = fileIdentityKeyFromFileName(resolved.fileName);
      if (fileNamesByKey.has(targetKey)) {
        targets.push(targetKey);
      }
    }
    adjacency.set(ownerKey, targets);
  }
  return { adjacency, fileNamesByKey };
}

function findIncludeCycleInSummaryGraph(
  graph: IncludeCycleSummaryGraph,
  owner: string,
  start: string,
): string[] | undefined {
  const ownerKey = fileIdentityKeyFromFileName(owner);
  const startKey = fileIdentityKeyFromFileName(start);
  const visited = new Set<string>();
  const stack: string[] = [];
  const stackIndexes = new Map<string, number>();

  const search = (fileKey: string): string[] | undefined => {
    if (fileKey === ownerKey && stack.length > 0) {
      return [...stack, ownerKey].map((key) => graph.fileNamesByKey.get(key) ?? key);
    }
    const stackIndex = stackIndexes.get(fileKey);
    if (stackIndex !== undefined) {
      return stack.slice(stackIndex).map((key) => graph.fileNamesByKey.get(key) ?? key);
    }
    if (visited.has(fileKey)) {
      return undefined;
    }
    visited.add(fileKey);
    stackIndexes.set(fileKey, stack.length);
    stack.push(fileKey);
    for (const next of graph.adjacency.get(fileKey) ?? []) {
      const cycle = search(next);
      if (cycle) {
        return cycle;
      }
    }
    stack.pop();
    stackIndexes.delete(fileKey);
    return undefined;
  };

  return search(startKey);
}

async function includeRenameWorkspaceEditAsync(
  files: Array<{ oldUri: string; newUri: string }>,
): Promise<WorkspaceEdit | null> {
  if (globalSettings.rename?.updateIncludesOnFileRename !== true) {
    return null;
  }
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
    const fileKey = fileIdentityKeyFromFileName(normalized);
    resetIncludeDependencies(pathToFileUri(normalized));
    if (sameFile(normalized, owner) && stack.length > 0) {
      return [...stack, owner];
    }
    const existingStackIndex = stackIndexes.get(fileKey);
    if (existingStackIndex !== undefined) {
      return [...stack.slice(existingStackIndex), normalized];
    }
    if (visited.has(fileKey)) {
      return undefined;
    }
    if (visited.size >= limits.maxDocuments) {
      noteTruncated(`documents>${limits.maxDocuments}`);
      return undefined;
    }
    const size = await fileSizeAsync(normalized, settings);
    if (size !== undefined && totalTextLength + size > limits.maxTextLength) {
      noteTruncated(`text>${limits.maxTextLength}`);
      return undefined;
    }
    totalTextLength += size ?? 0;
    visited.add(fileKey);
    stackIndexes.set(fileKey, stack.length);
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
    await prefetchIncludeRefsForOwnerAsync(pathToFileUri(normalized), entry.includeRefs, settings);
    for (const include of entry.includeRefs) {
      const next = await resolveIncludePathDetailsAsync(
        pathToFileUri(normalized),
        include.path,
        include.mode,
        settings,
      );
      recordIncludeDependency(pathToFileUri(normalized), pathToFileUri(next.fileName));
      if (!next.exists) {
        continue;
      }
      const cycle = await search(next.fileName, depth + 1);
      if (cycle) {
        return cycle;
      }
    }
    stack.pop();
    stackIndexes.delete(fileKey);
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
    owner: fileIdentityKeyFromFileName(owner),
    start: fileIdentityKeyFromFileName(start),
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
  const result: WorkspaceEdit = {};
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
  if (Object.keys(changes).length > 0) {
    result.changes = changes;
  }
  const documentChanges = edit.documentChanges
    ?.map((change) => remapDocumentChange(virtual, change, sourceUri))
    .filter((change): change is NonNullable<WorkspaceEdit["documentChanges"]>[number] =>
      Boolean(change),
    );
  if (documentChanges && documentChanges.length > 0) {
    result.documentChanges = documentChanges;
  }
  if (edit.changeAnnotations) {
    result.changeAnnotations = edit.changeAnnotations;
  }
  return result;
}

function remapDocumentChange(
  virtual: VirtualDocument,
  change: NonNullable<WorkspaceEdit["documentChanges"]>[number],
  sourceUri: string,
): NonNullable<WorkspaceEdit["documentChanges"]>[number] | undefined {
  if (!isTextDocumentEdit(change)) {
    return change;
  }
  const uri = change.textDocument.uri;
  const targetUri = uri === virtual.uri ? sourceUri : uri;
  const edits = change.edits
    .map((edit) => {
      const range =
        uri === virtual.uri ? sourceRangeFromVirtualRange(virtual, edit.range) : edit.range;
      return range ? { ...edit, range } : undefined;
    })
    .filter((edit): edit is (typeof change.edits)[number] => Boolean(edit));
  return { ...change, textDocument: { ...change.textDocument, uri: targetUri }, edits };
}

function isTextDocumentEdit(
  change: NonNullable<WorkspaceEdit["documentChanges"]>[number],
): change is Extract<
  NonNullable<WorkspaceEdit["documentChanges"]>[number],
  { textDocument: { uri: string }; edits: TextEdit[] }
> {
  return (
    "textDocument" in change &&
    typeof change.textDocument === "object" &&
    change.textDocument !== null &&
    "uri" in change.textDocument &&
    Array.isArray((change as { edits?: unknown }).edits)
  );
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
  const existing = cachedDocumentForUri(uri);
  if (existing) {
    return existing;
  }
  const document = openDocumentForUri(uri);
  if (!document) {
    return undefined;
  }
  const settings = cachedSettings(document.uri);
  const parsed = parseSourceDocument(
    document.uri,
    document.getText(),
    settings,
    document.languageId,
  );
  const cached = createCachedDocument(document, parsed, settings);
  cache.set(document.uri, cached);
  return cached;
}

async function getIndexedCachedAsync(
  uri: string,
  settings: AspSettings,
): Promise<CachedDocument | undefined> {
  await ensureWorkspaceIndexAsync(settings);
  const entry = workspaceIndex.get(fileIdentityKeyFromUri(uri));
  return entry ? cachedFromIndexedAsync(entry, cachedSettings(entry.uri)) : undefined;
}

async function cachedFromIndexedAsync(
  entry: WorkspaceIndexedDocument,
  settings: AspSettings,
): Promise<CachedDocument> {
  const includeEntry = await includeDocumentLoader.readAsync(entry.fileName, settings);
  const parsed =
    includeEntry?.parsed ??
    (await parseSourceDocumentAsync(
      entry.uri,
      await readTextFileAsync(entry.fileName, settings.legacyEncoding),
      settings,
    ));
  return createCachedDocument(
    TextDocument.create(entry.uri, languageIdForUri(entry.uri), 0, parsed.text),
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
  const includeRefsEntry = await includeDocumentLoader
    .readIncludeRefsAsync(entry.fileName, settings, { allowRead: true })
    .catch((error) => {
      logDiskAnalysisCacheError("diskIncludeRefs.preflight", error);
      return undefined;
    });
  let settingsKey =
    includeRefsEntry && sameDiskAnalysisSource(includeRefsEntry.source, sourceMetadata)
      ? await diskAnalysisSettingsKeyFromIncludeRefs(
          settings,
          entry.uri,
          includeRefsEntry.includeRefs,
          cancellation,
        )
      : undefined;
  if (cancellation.isCancellationRequested()) {
    return [];
  }
  if (settingsKey) {
    const processCachedDiagnostics = cachedWorkspaceIndexedDiagnostics(
      entry,
      sourceMetadata,
      settingsKey,
      settings,
    );
    if (processCachedDiagnostics) {
      return processCachedDiagnostics;
    }
    const cachedDiagnostics = await readDiskAnalysisDiagnostics(
      entry,
      sourceMetadata,
      settingsKey,
      settings,
    );
    if (cachedDiagnostics) {
      return cachedDiagnostics;
    }
  }
  const cached = await cachedFromIndexedAsync(entry, settings);
  const contentSourceMetadata = sourceWithContentHash(sourceMetadata, cached.source.getText());
  if (cancellation.isCancellationRequested()) {
    return [];
  }
  settingsKey ??= await diskAnalysisSettingsKey(settings, cached.parsed, cancellation);
  if (cancellation.isCancellationRequested()) {
    return [];
  }
  const processCachedDiagnostics = cachedWorkspaceIndexedDiagnostics(
    entry,
    contentSourceMetadata,
    settingsKey,
    settings,
  );
  if (processCachedDiagnostics) {
    return processCachedDiagnostics;
  }
  const cachedDiagnostics = await readDiskAnalysisDiagnostics(
    entry,
    contentSourceMetadata,
    settingsKey,
    settings,
  );
  if (cachedDiagnostics) {
    return cachedDiagnostics;
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
    source: contentSourceMetadata,
    settingsKey,
    diagnostics: items,
    builderState: aspProjectBuilderState.diskStateForUri(cached.source.uri),
  });
  rememberWorkspaceIndexedDiagnostics(entry, contentSourceMetadata, settingsKey, items, settings);
  if (sameDiskAnalysisSource(sourceMetadata, contentSourceMetadata)) {
    rememberWorkspaceIndexedDiagnostics(entry, sourceMetadata, settingsKey, items, settings);
  }
  logDebugSummary(settings, `[asp-lsp] diskCache.write: ${entry.uri}`);
  logDebugSummary(settings, `[asp-lsp] disk.builder.persist: ${entry.uri}`);
  return items;
}

async function readDiskAnalysisDiagnostics(
  entry: WorkspaceIndexedDocument,
  sourceMetadata: DiskAnalysisSourceMetadata,
  settingsKey: string,
  settings: AspSettings,
): Promise<Diagnostic[] | undefined> {
  const cachedAnalysis = await diskAnalysisCache.readAnalysis({
    source: sourceMetadata,
    settingsKey,
  });
  if (!cachedAnalysis) {
    return undefined;
  }
  logDebugSummary(settings, `[asp-lsp] diskCache.hit: ${entry.uri}`);
  aspProjectBuilderState.restoreDiskState(
    entry.uri,
    normalizeFileName(entry.fileName),
    sourceMetadata,
    cachedAnalysis.builderState,
    cachedAnalysis.diagnostics,
    settings,
  );
  rememberWorkspaceIndexedDiagnostics(
    entry,
    sourceMetadata,
    settingsKey,
    cachedAnalysis.diagnostics,
    settings,
  );
  return cachedAnalysis.diagnostics;
}

function cachedWorkspaceIndexedDiagnostics(
  entry: WorkspaceIndexedDocument,
  source: DiskAnalysisSourceMetadata,
  settingsKey: string,
  settings: AspSettings,
): Diagnostic[] | undefined {
  const key = workspaceIndexedDiagnosticsCacheKey(entry, source, settingsKey);
  const cached = workspaceIndexedDiagnosticsCache.get(key);
  if (!cached) {
    return undefined;
  }
  cached.lastUsed = Date.now();
  logDebugSummary(settings, `[asp-lsp] workspaceDiagnostics.cache.hit: ${entry.uri}`);
  return [...cached.items];
}

function rememberWorkspaceIndexedDiagnostics(
  entry: WorkspaceIndexedDocument,
  source: DiskAnalysisSourceMetadata,
  settingsKey: string,
  items: Diagnostic[],
  settings: AspSettings,
): void {
  const key = workspaceIndexedDiagnosticsCacheKey(entry, source, settingsKey);
  workspaceIndexedDiagnosticsCache.set(key, { key, items: [...items], lastUsed: Date.now() });
  pruneWorkspaceIndexedDiagnosticsCache();
  logDebugSummary(settings, `[asp-lsp] workspaceDiagnostics.cache.write: ${entry.uri}`);
}

function workspaceDiagnosticsReportCacheKey(
  openDocuments: TextDocument[],
  indexedEntries: WorkspaceIndexedDocument[],
): string {
  return JSON.stringify({
    openDocuments: openDocuments
      .map((document) => {
        const settings = cachedSettings(document.uri);
        return {
          uri: document.uri,
          version: document.version,
          settings: workspaceDiagnosticsSettingsIdentity(settings),
        };
      })
      .sort((left, right) =>
        fileIdentityKeyFromUri(left.uri).localeCompare(fileIdentityKeyFromUri(right.uri)),
      ),
    indexedEntries: indexedEntries
      .map((entry) => {
        const settings = cachedSettings(entry.uri);
        return {
          uri: entry.uri,
          fileName: normalizeFileName(entry.fileName),
          mtimeMs: entry.mtimeMs,
          size: entry.size,
          settings: workspaceDiagnosticsSettingsIdentity(settings),
        };
      })
      .sort((left, right) =>
        fileIdentityKeyFromFileName(left.fileName).localeCompare(
          fileIdentityKeyFromFileName(right.fileName),
        ),
      ),
    includeResolutionGeneration,
    jsProjectGeneration,
    workspaceGeneration,
  });
}

function workspaceDiagnosticsSettingsIdentity(settings: AspSettings): string {
  return JSON.stringify({
    parse: parseSettingsIdentity(settings),
    diagnostics: diagnosticsIdentity(settings),
    include: includeResolutionIdentity(settings),
    js: jsProjectIdentity(settings),
    workspace: workspaceIndexSettingsIdentity(settings),
    legacyEncoding: settings.legacyEncoding,
  });
}

function cachedWorkspaceDiagnosticsReport(
  key: string,
  settings: AspSettings,
): WorkspaceDiagnosticsReport | undefined {
  const cached = workspaceDiagnosticsReportCache;
  if (!cached || cached.key !== key) {
    return undefined;
  }
  cached.lastUsed = Date.now();
  logDebugSummary(
    settings,
    `[asp-lsp] workspaceDiagnostics.report.cache.hit: items=${cached.report.items.length}`,
  );
  return cloneWorkspaceDiagnosticsReport(cached.report);
}

function rememberWorkspaceDiagnosticsReport(
  key: string,
  report: WorkspaceDiagnosticsReport,
  settings: AspSettings,
): void {
  workspaceDiagnosticsReportCache = {
    key,
    report: cloneWorkspaceDiagnosticsReport(report),
    lastUsed: Date.now(),
  };
  logDebugSummary(
    settings,
    `[asp-lsp] workspaceDiagnostics.report.cache.write: items=${report.items.length}`,
  );
}

function cloneWorkspaceDiagnosticsReport(
  report: WorkspaceDiagnosticsReport,
): WorkspaceDiagnosticsReport {
  return {
    items: report.items.map((item) => ({
      ...item,
      items: [...item.items],
    })),
  };
}

function clearWorkspaceDiagnosticsCaches(): void {
  workspaceIndexedDiagnosticsCache.clear();
  workspaceDiagnosticsReportCache = undefined;
}

function workspaceIndexedDiagnosticsCacheKey(
  entry: WorkspaceIndexedDocument,
  source: DiskAnalysisSourceMetadata,
  settingsKey: string,
): string {
  return JSON.stringify({
    uri: entry.uri,
    fileName: normalizeFileName(entry.fileName),
    source,
    settingsKey,
    includeResolutionGeneration,
    jsProjectGeneration,
    workspaceGeneration,
  });
}

function pruneWorkspaceIndexedDiagnosticsCache(): void {
  while (workspaceIndexedDiagnosticsCache.size > workspaceIndexedDiagnosticsCacheMaxEntries) {
    const oldest = [...workspaceIndexedDiagnosticsCache.entries()].sort(
      (left, right) => left[1].lastUsed - right[1].lastUsed,
    )[0]?.[0];
    if (!oldest) {
      return;
    }
    workspaceIndexedDiagnosticsCache.delete(oldest);
  }
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
  return diskAnalysisSettingsKeyFromIncludeRefs(
    settings,
    parsed.uri,
    parsed.includes,
    cancellation,
  );
}

async function diskAnalysisSettingsKeyFromIncludeRefs(
  settings: AspSettings,
  rootUri: string,
  includeRefs: FileAnalysisSummary["includeRefs"],
  cancellation: AnalysisCancellation,
): Promise<string> {
  const cacheKey = diskAnalysisSettingsKeyCacheKey(settings, rootUri, includeRefs);
  const cached = diskAnalysisSettingsKeyCache.get(cacheKey);
  if (cached !== undefined) {
    diskAnalysisSettingsKeyCache.delete(cacheKey);
    diskAnalysisSettingsKeyCache.set(cacheKey, cached);
    return cached;
  }
  const settingsKey = JSON.stringify({
    parse: parseSettingsIdentity(settings),
    diagnostics: diagnosticsIdentity(settings),
    include: includeResolutionIdentity(settings),
    includeDependencies: await diskAnalysisIncludeDependencyKey(
      rootUri,
      includeRefs,
      settings,
      cancellation,
    ),
    js: jsProjectSettingsIdentity(settings),
    workspace: workspaceIndexSettingsIdentity(settings),
  });
  if (!cancellation.isCancellationRequested()) {
    diskAnalysisSettingsKeyCache.set(cacheKey, settingsKey);
    pruneDiskAnalysisSettingsKeyCache();
  }
  return settingsKey;
}

function diskAnalysisSettingsKeyCacheKey(
  settings: AspSettings,
  rootUri: string,
  includeRefs: FileAnalysisSummary["includeRefs"],
): string {
  return JSON.stringify({
    root: fileIdentityKeyFromUri(rootUri),
    includeRefs: includeRefsFingerprint(includeRefs),
    parse: parseSettingsIdentity(settings),
    diagnostics: diagnosticsIdentity(settings),
    include: includeResolutionIdentity(settings),
    includeGeneration: includeResolutionGeneration,
    js: jsProjectSettingsIdentity(settings),
    workspace: workspaceIndexSettingsIdentity(settings),
    workspaceGeneration,
  });
}

function pruneDiskAnalysisSettingsKeyCache(): void {
  while (diskAnalysisSettingsKeyCache.size > diskAnalysisSettingsKeyCacheMaxEntries) {
    const oldest = diskAnalysisSettingsKeyCache.keys().next().value;
    if (oldest === undefined) {
      return;
    }
    diskAnalysisSettingsKeyCache.delete(oldest);
  }
}

function clearDiskAnalysisSettingsKeyCache(): void {
  diskAnalysisSettingsKeyCache.clear();
}

async function diskAnalysisIncludeDependencyKey(
  rootUri: string,
  rootIncludeRefs: FileAnalysisSummary["includeRefs"],
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
    await prefetchIncludeRefsForOwnerAsync(ownerUri, includeRefs, settings);
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
      const fileKey = fileIdentityKeyFromFileName(normalizedFileName);
      const stat = await fsGateway.statAsync(normalizedFileName);
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
      if (!exists || visited.has(fileKey)) {
        continue;
      }
      visited.add(fileKey);
      const entry = await includeDocumentLoader
        .readIncludeRefsAsync(normalizedFileName, settings, { allowRead: true })
        .catch(() => undefined);
      if (entry) {
        await visitRefs(entry.uri, entry.includeRefs, depth + 1);
      } else {
        scheduleIncludeSummaryRefresh(
          rootUri,
          normalizedFileName,
          settings,
          "diskKey.missingSummary",
        );
      }
      await yieldToEventLoop();
    }
  };
  await visitRefs(rootUri, rootIncludeRefs, 0);
  return textFingerprint(JSON.stringify(dependencies));
}

async function ensureWorkspaceIndexAsync(
  settings: AspSettings,
  token?: { isCancellationRequested?: boolean },
): Promise<void> {
  if (!workspaceIndexDirty) {
    return;
  }
  if (workspaceIndexRestoreAllowed && cacheFreshness(settings) === "watch") {
    const restored = await restoreWorkspaceIndexFromDiskAsync(settings);
    if (restored) {
      scheduleWorkspaceIndexRevalidation(settings, "workspaceIndex.restore");
      return;
    }
  }
  const maxFiles = settings.workspace?.maxIndexFiles ?? defaultMaxIndexFiles;
  const task = beginProgressTask("loading", "workspace.index", {
    current: 0,
    total: maxFiles,
    cancellable: true,
  });
  try {
    workspaceIndex.clear();
    workspaceIndexTruncated = false;
    let scannedFiles = 0;
    const chunkSize = settings.workspace?.scanChunkSize ?? defaultScanChunkSize;
    const scanToken = {
      get isCancellationRequested() {
        return token?.isCancellationRequested === true || task.isCancellationRequested();
      },
    };
    for (const root of workspaceIndexRoots()) {
      if (scanToken.isCancellationRequested || scannedFiles >= maxFiles) {
        break;
      }
      task.update({
        label: "workspace.index.scanRoot",
        current: Math.min(scannedFiles, maxFiles),
        detail: progressFileLabel(root),
      });
      const filter = await createWorkspaceScanFilter(root, settings);
      scannedFiles = await indexWorkspaceRootAsync(
        root,
        {
          scannedFiles,
          maxFiles,
          chunkSize,
          token: scanToken,
          progress: task,
        },
        filter,
        settings,
      );
      task.update({ current: Math.min(scannedFiles, maxFiles), detail: progressFileLabel(root) });
    }
    workspaceIndexTruncated = scannedFiles >= maxFiles;
    workspaceIndexDirty = scanToken.isCancellationRequested;
    workspaceIndexRestoreAllowed = workspaceIndexDirty;
    if (!workspaceIndexDirty) {
      task.update({
        label: "workspace.index.writeCache",
        current: Math.min(scannedFiles, maxFiles),
        detail: `${workspaceIndex.size}`,
      });
      await writeWorkspaceIndexToDiskAsync(settings);
    }
    if (workspaceIndexTruncated) {
      logServerWarning(
        createLocalizer(settings.resolvedLocale).t("server.workspaceIndex.truncated", { maxFiles }),
        settings,
      );
    }
  } finally {
    task.end();
  }
}

async function indexWorkspaceRootAsync(
  root: string,
  state: {
    scannedFiles: number;
    maxFiles: number;
    chunkSize: number;
    token?: { isCancellationRequested?: boolean };
    progress?: AspLspProgressTaskHandle;
  },
  filter: WorkspaceScanFilter,
  settings: AspSettings,
): Promise<number> {
  const stat = await fsGateway.statAsync(root);
  if (!stat?.isDirectory()) {
    return state.scannedFiles;
  }
  const concurrency = includeReadConcurrency(settings);
  const directories = [root];
  let scannedFiles = state.scannedFiles;
  let operations = 0;
  while (directories.length > 0 && scannedFiles < state.maxFiles) {
    if (state.token?.isCancellationRequested) {
      return scannedFiles;
    }
    const batch = directories.splice(0, concurrency);
    const batches = await mapWithConcurrency(batch, concurrency, async (directory) => {
      const listing = await fsGateway.readdirAsync(directory);
      const childDirectories: string[] = [];
      const files: string[] = [];
      for (const entry of listing?.entries ?? []) {
        if (state.token?.isCancellationRequested) {
          break;
        }
        const fullPath = path.join(directory, entry.name);
        if (entry.isDirectory()) {
          if (workspaceScanFilterShouldVisitDirectory(filter, fullPath)) {
            childDirectories.push(fullPath);
          }
        } else if (entry.isFile() && workspaceScanFilterIncludesFile(filter, fullPath)) {
          files.push(fullPath);
        }
      }
      return { childDirectories, files };
    });
    const files: string[] = [];
    for (const item of batches) {
      directories.push(...item.childDirectories);
      files.push(...item.files);
    }
    const filesToIndex: string[] = [];
    for (const fileName of files) {
      if (state.token?.isCancellationRequested || scannedFiles >= state.maxFiles) {
        break;
      }
      scannedFiles += 1;
      filesToIndex.push(fileName);
      if (scannedFiles % state.chunkSize === 0 || scannedFiles >= state.maxFiles) {
        state.progress?.update({
          label: "workspace.index.scanFiles",
          current: Math.min(scannedFiles, state.maxFiles),
          detail: progressFileLabel(fileName),
        });
      }
    }
    await mapWithConcurrency(filesToIndex, concurrency, (fileName) =>
      indexWorkspaceFileAsync(fileName, settings, { silentStatus: true }),
    );
    operations += batch.length + filesToIndex.length;
    if (operations >= state.chunkSize) {
      operations %= state.chunkSize;
      await yieldToEventLoop();
    }
  }
  return scannedFiles;
}

async function indexWorkspaceFileAsync(
  fileName: string,
  settings: AspSettings,
  options: { silentStatus?: boolean } = {},
): Promise<void> {
  const run = async (): Promise<void> => {
    const normalized = normalizeFileName(fileName);
    const fileKey = fileIdentityKeyFromFileName(normalized);
    if (!(await shouldIndexWorkspaceFileAsync(normalized, settings))) {
      workspaceIndex.delete(fileKey);
      forgetSourceMetadata(normalized);
      return;
    }
    const stat = await fsGateway.statAsync(normalized);
    if (!stat?.isFile()) {
      workspaceIndex.delete(fileKey);
      forgetSourceMetadata(normalized);
      return;
    }
    const existing = workspaceIndex.get(fileKey);
    if (existing && existing.mtimeMs === stat.mtimeMs && existing.size === stat.size) {
      return;
    }
    const uri = pathToFileUri(normalized);
    workspaceIndex.set(fileKey, {
      uri,
      fileName: normalized,
      mtimeMs: stat.mtimeMs,
      size: stat.size,
    });
    rememberSourceMetadata({
      fileName: normalized,
      mtimeMs: stat.mtimeMs,
      size: stat.size,
    });
  };
  if (options.silentStatus === true) {
    await run();
    return;
  }
  await withServerStatusAsync("loading", "workspace.indexFile", run);
}

async function restoreWorkspaceIndexFromDiskAsync(settings: AspSettings): Promise<boolean> {
  const settingsKey = workspaceIndexSettingsIdentity(settings);
  const entry = await diskAnalysisCache.readWorkspaceIndex(settingsKey).catch((error) => {
    logDiskAnalysisCacheError("workspaceIndex.restore", error);
    return undefined;
  });
  if (!entry) {
    return false;
  }
  workspaceIndex.clear();
  workspaceIndexTruncated = entry.truncated;
  sourceManifest.clear();
  for (const indexed of entry.entries) {
    const normalized = normalizeFileName(indexed.fileName);
    const fileKey = fileIdentityKeyFromFileName(normalized);
    const restored = {
      uri: indexed.uri,
      fileName: normalized,
      mtimeMs: indexed.mtimeMs,
      size: indexed.size,
    };
    workspaceIndex.set(fileKey, restored);
    rememberSourceMetadata(restored);
  }
  workspaceIndexDirty = false;
  workspaceIndexRestoreAllowed = false;
  logDebugSummary(settings, `[asp-lsp] workspaceIndex.restore: files=${workspaceIndex.size}`);
  return true;
}

async function writeWorkspaceIndexToDiskAsync(settings: AspSettings): Promise<void> {
  if (!diskAnalysisCache.enabled) {
    return;
  }
  const entries: DiskWorkspaceIndexedDocument[] = [...workspaceIndex.values()].map((entry) => ({
    uri: entry.uri,
    fileName: normalizeFileName(entry.fileName),
    mtimeMs: entry.mtimeMs,
    size: entry.size,
    contentHash: sourceManifest.get(fileIdentityKeyFromFileName(entry.fileName))?.contentHash,
  }));
  await diskAnalysisCache
    .writeWorkspaceIndex({
      settingsKey: workspaceIndexSettingsIdentity(settings),
      entries,
      truncated: workspaceIndexTruncated,
    })
    .then(() =>
      logDebugSummary(settings, `[asp-lsp] workspaceIndex.write: files=${entries.length}`),
    )
    .catch((error) => logDiskAnalysisCacheError("workspaceIndex.write", error));
}

function scheduleWorkspaceIndexRevalidation(settings: AspSettings, reason: string): void {
  if (resolveNetworkProfile(settings).kind !== "network" || cacheFreshness(settings) !== "watch") {
    return;
  }
  const serial = ++workspaceIndexRevalidationSerial;
  const workspaceSettingsKey = workspaceIndexSettingsIdentity(settings);
  const startedGeneration = workspaceGeneration;
  void revalidateWorkspaceIndexAsync(settings, {
    serial,
    reason,
    workspaceSettingsKey,
    startedGeneration,
  }).catch((error) =>
    connection.console.warn(`[asp-lsp] workspaceIndex.revalidate.failed: ${errorMessage(error)}`),
  );
}

async function revalidateWorkspaceIndexAsync(
  settings: AspSettings,
  context: {
    serial: number;
    reason: string;
    workspaceSettingsKey: string;
    startedGeneration: number;
  },
): Promise<void> {
  const entries = [...workspaceIndex.values()];
  if (entries.length === 0) {
    return;
  }
  logDebugSummary(
    settings,
    `[asp-lsp] workspaceIndex.revalidate.started: reason=${context.reason}, files=${entries.length}`,
  );
  const changedFiles = new Set<string>();
  const deletedFiles = new Set<string>();
  await mapWithConcurrency(entries, includeReadConcurrency(settings), async (entry) => {
    if (
      context.serial !== workspaceIndexRevalidationSerial ||
      workspaceGeneration !== context.startedGeneration ||
      workspaceIndexSettingsIdentity(settings) !== context.workspaceSettingsKey
    ) {
      return;
    }
    const normalized = normalizeFileName(entry.fileName);
    const stat = await fsGateway.statAsync(normalized);
    const fileKey = fileIdentityKeyFromFileName(normalized);
    if (!stat?.isFile()) {
      workspaceIndex.delete(fileKey);
      forgetSourceMetadata(normalized);
      deletedFiles.add(normalized);
      return;
    }
    if (entry.mtimeMs === stat.mtimeMs && entry.size === stat.size) {
      return;
    }
    const next = {
      uri: entry.uri,
      fileName: normalized,
      mtimeMs: stat.mtimeMs,
      size: stat.size,
    };
    workspaceIndex.set(fileKey, next);
    rememberSourceMetadata(next);
    changedFiles.add(normalized);
  });
  if (
    context.serial !== workspaceIndexRevalidationSerial ||
    workspaceGeneration !== context.startedGeneration ||
    workspaceIndexSettingsIdentity(settings) !== context.workspaceSettingsKey
  ) {
    return;
  }
  const driftedFiles = new Set([...changedFiles, ...deletedFiles]);
  if (driftedFiles.size === 0) {
    logDebugSummary(settings, `[asp-lsp] workspaceIndex.revalidate.complete: drift=0`);
    return;
  }
  includeDocumentLoader.invalidateFiles(driftedFiles);
  invalidateGraphFileIndexFiles(driftedFiles);
  clearWorkspaceVbReferenceCaches();
  for (const fileName of deletedFiles) {
    workspaceIncludeGraph.delete(fileName);
  }
  for (const fileName of changedFiles) {
    await refreshWorkspaceIncludeGraphFileAsync(fileName, settings);
  }
  await writeWorkspaceIndexToDiskAsync(settings);
  logDebugSummary(
    settings,
    `[asp-lsp] workspaceIndex.revalidate.complete: changed=${changedFiles.size}, deleted=${deletedFiles.size}`,
  );
}

function workspaceIncludeGraphSettingsIdentity(settings: AspSettings): string {
  return JSON.stringify({
    workspace: workspaceIndexSettingsIdentity(settings),
    includeRefs: includeRefsSettingsKey(settings),
    includeResolution: includeResolutionSettingsIdentity(settings),
  });
}

async function ensureWorkspaceIncludeGraphAsync(
  settings: AspSettings,
  cancellation: AnalysisCancellation,
): Promise<void> {
  const settingsKey = workspaceIncludeGraphSettingsIdentity(settings);
  if (!workspaceIncludeGraphDirty && workspaceIncludeGraph.settingsKey === settingsKey) {
    return;
  }
  if (workspaceIncludeGraphRestoreAllowed) {
    const restored = await restoreWorkspaceIncludeGraphFromDiskAsync(settings);
    if (restored) {
      return;
    }
  }
  workspaceIncludeGraph.reset(settingsKey);
  const entries = workspaceEntriesOpenFirst([...workspaceIndex.values()]);
  await mapWithConcurrency(entries, includeReadConcurrency(settings), async (entry) => {
    if (cancellation.isCancellationRequested()) {
      return;
    }
    await updateWorkspaceIncludeGraphEntryAsync(entry, settings, { allowRead: true });
  });
  if (cancellation.isCancellationRequested()) {
    workspaceIncludeGraphDirty = true;
    workspaceIncludeGraph.reset();
    return;
  }
  workspaceIncludeGraphDirty = false;
  workspaceIncludeGraphRestoreAllowed = false;
  await writeWorkspaceIncludeGraphToDiskAsync(settings);
  logDebugSummary(
    settings,
    `[asp-lsp] workspaceIncludeGraph.built: files=${workspaceIncludeGraph.size}`,
  );
}

async function restoreWorkspaceIncludeGraphFromDiskAsync(settings: AspSettings): Promise<boolean> {
  const settingsKey = workspaceIncludeGraphSettingsIdentity(settings);
  const entry = await diskAnalysisCache.readWorkspaceIncludeGraph(settingsKey).catch((error) => {
    logDiskAnalysisCacheError("workspaceIncludeGraph.restore", error);
    return undefined;
  });
  if (!entry) {
    return false;
  }
  workspaceIncludeGraph.restore({ settingsKey: entry.settingsKey, entries: entry.entries });
  workspaceIncludeGraphDirty = false;
  workspaceIncludeGraphRestoreAllowed = false;
  logDebugSummary(
    settings,
    `[asp-lsp] workspaceIncludeGraph.restore: files=${entry.entries.length}`,
  );
  return true;
}

async function writeWorkspaceIncludeGraphToDiskAsync(settings: AspSettings): Promise<void> {
  if (!diskAnalysisCache.enabled || workspaceIncludeGraphDirty) {
    return;
  }
  const snapshot = workspaceIncludeGraph.snapshot(workspaceIncludeGraphSettingsIdentity(settings));
  if (!snapshot) {
    return;
  }
  await diskAnalysisCache
    .writeWorkspaceIncludeGraph(snapshot)
    .then(() =>
      logDebugSummary(
        settings,
        `[asp-lsp] workspaceIncludeGraph.write: files=${snapshot.entries.length}`,
      ),
    )
    .catch((error) => logDiskAnalysisCacheError("workspaceIncludeGraph.write", error));
}

async function updateWorkspaceIncludeGraphEntryAsync(
  entry: WorkspaceIndexedDocument,
  settings: AspSettings,
  options: { allowRead?: boolean } = {},
): Promise<void> {
  const includeRefsEntry = await includeDocumentLoader.readIncludeRefsAsync(
    entry.fileName,
    settings,
    options,
  );
  if (
    !includeRefsEntry ||
    !sameDiskAnalysisSource(includeRefsEntry.source, diskAnalysisSourceMetadata(entry))
  ) {
    workspaceIncludeGraph.delete(entry.fileName);
    return;
  }
  const targetFileNames = await resolvedIncludeTargetFileNamesAsync(
    entry.uri,
    includeRefsEntry.includeRefs,
    settings,
  );
  workspaceIncludeGraph.upsert(
    entry.fileName,
    diskAnalysisSourceMetadata(entry),
    targetFileNames,
    includeRefsEntry.fingerprint,
  );
}

async function refreshWorkspaceIncludeGraphFileAsync(
  fileName: string,
  settings: AspSettings,
): Promise<void> {
  if (
    workspaceIncludeGraphDirty ||
    workspaceIncludeGraph.settingsKey !== workspaceIncludeGraphSettingsIdentity(settings)
  ) {
    return;
  }
  const entry = workspaceIndex.get(fileIdentityKeyFromFileName(fileName));
  if (entry) {
    await updateWorkspaceIncludeGraphEntryAsync(entry, settings, { allowRead: true });
  } else {
    workspaceIncludeGraph.delete(fileName);
  }
  await writeWorkspaceIncludeGraphToDiskAsync(settings);
}

async function resolvedIncludeTargetFileNamesAsync(
  ownerUri: string,
  includeRefs: AspInclude[],
  settings: AspSettings,
): Promise<string[]> {
  const resolved = await mapWithConcurrency(
    includeRefs,
    includeReadConcurrency(settings),
    async (include) =>
      resolveIncludePathDetailsAsync(ownerUri, include.path, include.mode, settings).catch(
        () => undefined,
      ),
  );
  return resolved
    .filter((item): item is IncludePathResolution => item !== undefined && item.exists === true)
    .map((item) => normalizeFileName(item.fileName));
}

function invalidateWorkspaceIncludeGraph(reason: string): void {
  workspaceIncludeGraphDirty = true;
  workspaceIncludeGraphRestoreAllowed = false;
  workspaceIncludeGraph.reset();
  logInvalidation("workspaceIncludeGraph", reason);
}

function allowWorkspaceIncludeGraphRestore(reason: string): void {
  workspaceIncludeGraphDirty = true;
  workspaceIncludeGraphRestoreAllowed = true;
  workspaceIncludeGraph.reset();
  logInvalidation("workspaceIncludeGraph", reason);
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
  invalidateAspGraphPayloadCache(reason);
  workspaceGeneration += 1;
  workspaceIndexDirty = true;
  workspaceIndexTruncated = false;
  workspaceIndexRestoreAllowed = false;
  workspaceIndex.clear();
  sourceManifest.clear();
  clearDiskAnalysisSettingsKeyCache();
  clearWorkspaceDiagnosticsCaches();
  vbCanonicalContextSymbolsCache.clear();
  invalidateWorkspaceIncludeGraph(reason);
  clearWorkspaceVbReferenceCaches();
  logInvalidation("workspaceIndex", reason, workspaceGeneration);
}

async function clearDiskAnalysisCacheByCommand(): Promise<void> {
  await diskAnalysisCache.clear();
  clearDiskAnalysisSettingsKeyCache();
  clearWorkspaceDiagnosticsCaches();
  fsGateway.invalidateAll();
  logDebugSummary(globalSettings, "[asp-lsp] diskCache.clear");
}

async function clearProcessCachesByCommand(reason: string): Promise<void> {
  invalidateAspGraphPayloadCache(reason);
  fsGateway.invalidateAll();
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
  clearDiskAnalysisSettingsKeyCache();
  clearWorkspaceDiagnosticsCaches();
  vbCanonicalContextSymbolsCache.clear();
  interactiveVbProjectContextSnapshots.clear();
  pendingInteractiveVbProjectContextRefreshes.clear();
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
  workspaceIndexRestoreAllowed = true;
  workspaceIndex.clear();
  sourceManifest.clear();
  clearWorkspaceDiagnosticsCaches();
  allowWorkspaceIncludeGraphRestore(`processCache.${reason}`);
  clearWorkspaceVbReferenceCaches();
  logDebugSummary(globalSettings, `[asp-lsp] processCache.workspaceIndex.clear: ${reason}`);
}

function invalidateRootlessWorkspaceIndex(reason: string): void {
  if (workspaceRoots.length === 0) {
    invalidateWorkspaceIndex(`rootlessWorkspace.${reason}`);
  }
}

async function closeDiagnosticsWorkerPools(reason: string): Promise<void> {
  const jsPool = jsDiagnosticsWorkerPool;
  const vbPool = vbDiagnosticsWorkerPool;
  const referencesPool = vbReferencesWorkerPool;
  const bulkPool = bulkWorkerPool;
  jsDiagnosticsWorkerPool = undefined;
  vbDiagnosticsWorkerPool = undefined;
  vbReferencesWorkerPool = undefined;
  bulkWorkerPool = undefined;
  clearWorkspaceVbReferenceCaches();
  if (jsPool) {
    try {
      await jsPool.close();
    } catch (error) {
      logServerWarning(
        `[asp-lsp] jsWorkerPool.close.failed: reason=${reason}, error=${errorMessage(error)}`,
      );
    }
  }
  if (vbPool) {
    try {
      await vbPool.close();
    } catch (error) {
      logServerWarning(
        `[asp-lsp] vbWorkerPool.close.failed: reason=${reason}, error=${errorMessage(error)}`,
      );
    }
  }
  if (referencesPool) {
    try {
      await referencesPool.close();
    } catch (error) {
      logServerWarning(
        `[asp-lsp] references.worker.close.failed: reason=${reason}, error=${errorMessage(error)}`,
      );
    }
  }
  if (bulkPool) {
    try {
      await bulkPool.close();
    } catch (error) {
      logServerWarning(
        `[asp-lsp] bulk.worker.close.failed: reason=${reason}, error=${errorMessage(error)}`,
      );
    }
  }
}

function isHardExcludedWorkspaceDirectory(name: string, fullPath: string): boolean {
  const normalized = fullPath.split(path.sep).join("/");
  return (
    [".git", "node_modules", "dist", "out"].includes(name) ||
    normalized.endsWith("/server/language-server/node_modules")
  );
}

async function createWorkspaceScanFilter(
  root: string,
  settings: AspSettings,
): Promise<WorkspaceScanFilter> {
  return createWorkspaceScanFilterWithPatterns(
    root,
    settings.workspace?.includes ?? defaultWorkspaceIncludes,
    settings.workspace?.excludes ?? [],
    settings.workspace?.respectGitIgnore === true,
  );
}

async function createWorkspaceScanFilterWithPatterns(
  root: string,
  includes: string[],
  excludes: string[],
  respectGitIgnore: boolean,
): Promise<WorkspaceScanFilter> {
  const normalizedRoot = normalizeFileName(root);
  return {
    root: normalizedRoot,
    includes: compileWorkspaceGlobPatterns(includes),
    excludes: compileWorkspaceGlobPatterns(excludes),
    gitIgnoreRules: respectGitIgnore ? await readWorkspaceGitIgnoreRulesAsync(normalizedRoot) : [],
  };
}

async function shouldIndexWorkspaceFileAsync(
  fileName: string,
  settings: AspSettings,
): Promise<boolean> {
  if (!isAspWorkspaceFile(fileName)) {
    return false;
  }
  const root = workspaceRootForFileName(fileName);
  if (!root) {
    return false;
  }
  const filter = await createWorkspaceScanFilter(root, settings);
  return workspaceScanFilterIncludesFile(filter, fileName);
}

async function workspaceAnalyzableOpenDocumentsAsync(
  settings: AspSettings,
): Promise<TextDocument[]> {
  const items: TextDocument[] = [];
  for (const document of documents.all()) {
    if (!isClassicAspGraphUri(document.uri)) {
      continue;
    }
    if (await shouldIndexWorkspaceFileAsync(uriToFileName(document.uri), settings)) {
      items.push(document);
    }
  }
  return items;
}

function workspaceScanFilterIncludesFile(filter: WorkspaceScanFilter, fileName: string): boolean {
  const relative = workspaceRelativePath(filter.root, fileName);
  if (!relative) {
    return false;
  }
  return (
    workspacePatternListMatches(filter.includes, relative, false) &&
    !workspacePatternListExcludesPath(filter.excludes, relative, false) &&
    !workspaceGitIgnoreRulesIgnorePath(filter.gitIgnoreRules, relative, false)
  );
}

async function createWorkspaceFileNameFilterAsync(
  settings: AspSettings,
  options: {
    includeGlobs?: string[];
    excludeGlobs?: string[];
    respectGitIgnore?: boolean;
    selectedUri?: string;
  } = {},
): Promise<(fileName: string) => boolean> {
  const workspaceSettings = (settings.workspace ??
    normalizeWorkspaceSettings(settings)) as NonNullable<AspSettings["workspace"]>;
  const includeGlobs =
    options.includeGlobs ?? workspaceSettings.includes ?? defaultWorkspaceIncludes;
  const excludeGlobs = options.excludeGlobs ?? workspaceSettings.excludes ?? [];
  const respectGitIgnore = options.respectGitIgnore ?? workspaceSettings.respectGitIgnore === true;
  const roots = workspaceIndexRoots()
    .map(normalizeFileName)
    .sort((left, right) => right.length - left.length);
  const filters = await Promise.all(
    roots.map(async (root) => ({
      key: graphFileKey(root),
      filter: await createWorkspaceScanFilterWithPatterns(
        root,
        includeGlobs,
        excludeGlobs,
        respectGitIgnore,
      ),
    })),
  );
  const selectedKey = workspaceFileKeyFromUriText(options.selectedUri);
  return (fileName) => {
    const normalized = normalizeFileName(fileName);
    const fileKey = graphFileKey(normalized);
    if (selectedKey && fileKey === selectedKey) {
      return true;
    }
    const root = filters.find((item) => fileIdentityKeyIsWithinOrEqual(fileKey, item.key));
    return root ? workspaceScanFilterIncludesFile(root.filter, normalized) : false;
  };
}

function workspaceFileKeyFromUriText(uri: string | undefined): string | undefined {
  if (!uri?.startsWith("file://")) {
    return undefined;
  }
  try {
    return graphFileKeyFromUri(uri);
  } catch {
    return undefined;
  }
}

function workspaceScanFilterShouldVisitDirectory(
  filter: WorkspaceScanFilter,
  directory: string,
): boolean {
  const normalized = normalizeFileName(directory);
  if (normalized === filter.root) {
    return true;
  }
  if (isHardExcludedWorkspaceDirectory(path.basename(normalized), normalized)) {
    return false;
  }
  const relative = workspaceRelativePath(filter.root, normalized);
  if (!relative) {
    return false;
  }
  return (
    !workspacePatternListExcludesPath(filter.excludes, relative, true) &&
    !workspaceGitIgnoreRulesIgnorePath(filter.gitIgnoreRules, relative, true)
  );
}

function workspacePreviewShouldVisitDirectory(
  filter: WorkspaceScanFilter,
  directory: string,
): boolean {
  const normalized = normalizeFileName(directory);
  if (normalized === filter.root) {
    return true;
  }
  if (isHardExcludedWorkspaceDirectory(path.basename(normalized), normalized)) {
    return false;
  }
  const relative = workspaceRelativePath(filter.root, normalized);
  if (!relative) {
    return false;
  }
  return !workspaceGitIgnoreRulesIgnorePath(filter.gitIgnoreRules, relative, true);
}

function workspacePatternListMatches(
  patterns: WorkspaceGlobPattern[],
  relativePath: string,
  isDirectory: boolean,
): boolean {
  return patterns.some((pattern) => workspacePatternMatches(pattern, relativePath, isDirectory));
}

function workspacePatternMatchIndexes(
  patterns: WorkspaceGlobPattern[],
  relativePath: string,
  isDirectory: boolean,
): number[] {
  return patterns.flatMap((pattern, index) =>
    workspacePatternMatches(pattern, relativePath, isDirectory) ? [index] : [],
  );
}

function workspacePatternListExcludesPath(
  patterns: WorkspaceGlobPattern[],
  relativePath: string,
  isDirectory: boolean,
): boolean {
  return patterns.some(
    (pattern) =>
      workspacePatternMatches(pattern, relativePath, isDirectory) ||
      workspacePatternMatchesAncestorDirectory(pattern, relativePath),
  );
}

function workspacePatternExcludesPathIndexes(
  patterns: WorkspaceGlobPattern[],
  relativePath: string,
  isDirectory: boolean,
): number[] {
  return patterns.flatMap((pattern, index) =>
    workspacePatternMatches(pattern, relativePath, isDirectory) ||
    workspacePatternMatchesAncestorDirectory(pattern, relativePath)
      ? [index]
      : [],
  );
}

function workspaceGitIgnoreRulesIgnorePath(
  rules: WorkspaceGitIgnoreRule[],
  relativePath: string,
  isDirectory: boolean,
): boolean {
  let ignored = false;
  for (const rule of rules) {
    if (
      workspacePatternMatches(rule, relativePath, isDirectory) ||
      workspacePatternMatchesAncestorDirectory(rule, relativePath)
    ) {
      ignored = !rule.negated;
    }
  }
  return ignored;
}

function workspacePatternMatchesAncestorDirectory(
  pattern: WorkspaceGlobPattern,
  relativePath: string,
): boolean {
  const parts = relativePath.split("/").filter((part) => part.length > 0);
  for (let index = 1; index < parts.length; index += 1) {
    if (workspacePatternMatches(pattern, parts.slice(0, index).join("/"), true)) {
      return true;
    }
  }
  return false;
}

function workspacePatternMatches(
  pattern: WorkspaceGlobPattern,
  relativePath: string,
  isDirectory: boolean,
): boolean {
  const normalized = normalizeWorkspaceRelativePath(relativePath);
  const candidates = [normalized];
  if (isDirectory) {
    candidates.push(`${normalized}/`);
  }
  if (pattern.matchBasename) {
    const basename = path.posix.basename(normalized);
    candidates.push(basename);
    if (isDirectory) {
      candidates.push(`${basename}/`);
    }
  }
  return candidates.some((candidate) => pattern.regex.test(candidate));
}

async function readWorkspaceGitIgnoreRulesAsync(root: string): Promise<WorkspaceGitIgnoreRule[]> {
  const text = await fs.promises
    .readFile(path.join(root, ".gitignore"), "utf8")
    .catch(() => undefined);
  return text === undefined ? [] : parseWorkspaceGitIgnoreRules(text);
}

function parseWorkspaceGitIgnoreRules(text: string): WorkspaceGitIgnoreRule[] {
  const rules: WorkspaceGitIgnoreRule[] = [];
  for (const rawLine of text.split(/\r?\n/)) {
    let line = rawLine.trim();
    if (!line || line.startsWith("#")) {
      continue;
    }
    let negated = false;
    if (line.startsWith("!")) {
      negated = true;
      line = line.slice(1);
    }
    line = line.replace(/\\#/g, "#").replace(/\\!/g, "!");
    const directoryOnly = line.endsWith("/");
    line = line.replace(/^\/+/, "").replace(/\/+$/, "");
    if (!line) {
      continue;
    }
    const compiled = compileWorkspaceGlobPattern(directoryOnly ? `${line}/**` : line);
    rules.push({ ...compiled, directoryOnly, negated });
  }
  return rules;
}

function compileWorkspaceGlobPatterns(patterns: string[]): WorkspaceGlobPattern[] {
  return patterns.map(compileWorkspaceGlobPattern);
}

function compileWorkspaceGlobPattern(pattern: string): WorkspaceGlobPattern {
  const normalized = normalizeWorkspaceGlobPattern(pattern);
  return {
    pattern: normalized,
    regex: new RegExp(`^${workspaceGlobToRegExpSource(normalized)}$`, "i"),
    matchBasename: !normalized.includes("/"),
  };
}

function normalizeWorkspaceGlobPattern(pattern: string): string {
  return normalizeWorkspaceRelativePath(pattern).replace(/^\/+/, "");
}

function normalizeWorkspaceRelativePath(value: string): string {
  return value.split(path.sep).join("/").replace(/\\/g, "/").replace(/^\.\//, "");
}

function workspaceRelativePath(root: string, fileName: string): string | undefined {
  const relative = path.relative(root, normalizeFileName(fileName));
  if (!relative || relative.startsWith("..") || path.isAbsolute(relative)) {
    return undefined;
  }
  return normalizeWorkspaceRelativePath(relative);
}

function workspaceRootForFileName(fileName: string): string | undefined {
  const normalized = normalizeFileName(fileName);
  const fileKey = fileIdentityKeyFromFileName(normalized);
  return workspaceIndexRoots()
    .map(normalizeFileName)
    .sort((left, right) => right.length - left.length)
    .find((root) => {
      const rootKey = fileIdentityKeyFromFileName(root);
      return fileIdentityKeyIsWithinOrEqual(fileKey, rootKey);
    });
}

function workspaceIndexRoots(): string[] {
  const roots =
    workspaceRoots.length > 0
      ? workspaceRoots
      : documents
          .all()
          .filter((document) => isClassicAspGraphUri(document.uri))
          .map((document) => path.dirname(uriToFileName(document.uri)));
  return roots
    .map(normalizeFileName)
    .filter(
      (root, index, items) =>
        root.length > 0 &&
        items.findIndex(
          (item) => fileIdentityKeyFromFileName(item) === fileIdentityKeyFromFileName(root),
        ) === index,
    );
}

function workspaceGlobToRegExpSource(pattern: string): string {
  let source = "";
  for (let index = 0; index < pattern.length; ) {
    const character = pattern[index];
    if (character === "*") {
      if (pattern[index + 1] === "*") {
        index += 2;
        if (pattern[index] === "/") {
          index += 1;
          source += "(?:.*/)?";
        } else {
          source += ".*";
        }
      } else {
        index += 1;
        source += "[^/]*";
      }
      continue;
    }
    if (character === "?") {
      source += "[^/]";
      index += 1;
      continue;
    }
    if (character === "{") {
      const close = pattern.indexOf("}", index + 1);
      if (close !== -1) {
        const alternatives = pattern
          .slice(index + 1, close)
          .split(",")
          .map((part) => workspaceGlobToRegExpSource(part));
        source += `(?:${alternatives.join("|")})`;
        index = close + 1;
        continue;
      }
    }
    if (character === "[") {
      const close = pattern.indexOf("]", index + 1);
      if (close > index + 1) {
        const content = pattern.slice(index + 1, close).replace(/\\/g, "\\\\");
        source += `[${content.startsWith("!") ? `^${content.slice(1)}` : content}]`;
        index = close + 1;
        continue;
      }
    }
    source += escapeRegExpCharacter(character);
    index += 1;
  }
  return source;
}

function escapeRegExpCharacter(character: string): string {
  return String.raw`\^$+?.()|{}[]`.includes(character) ? `\\${character}` : character;
}

function isAspWorkspaceFile(fileName: string): boolean {
  return /\.(?:asp|asa|inc|vbs)$/i.test(fileName);
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
    if (
      region.contentEnd < offset ||
      (region.contentEnd === offset && offset !== parsed.text.length)
    ) {
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

function memoizeBySettings<T>(
  cache: WeakMap<AspSettings, T>,
  settings: AspSettings,
  factory: () => T,
): T {
  const cached = cache.get(settings);
  if (cached !== undefined) {
    return cached;
  }
  const value = factory();
  cache.set(settings, value);
  return value;
}

function sortedWorkspaceRootsIdentity(): { roots: string[]; key: string } {
  if (workspaceRootsIdentityCache?.source === workspaceRoots) {
    return workspaceRootsIdentityCache;
  }
  const roots = workspaceRoots.map(normalizeFileName).sort();
  const key = JSON.stringify(roots);
  workspaceRootsIdentityCache = { source: workspaceRoots, roots, key };
  return workspaceRootsIdentityCache;
}

function workspaceIndexRootsIdentity(): { roots: string[]; key: string } {
  if (workspaceRoots.length > 0) {
    return sortedWorkspaceRootsIdentity();
  }
  const roots = workspaceIndexRoots().sort();
  return { roots, key: JSON.stringify(roots) };
}

function parseSettingsIdentity(settings: AspSettings): string {
  return memoizeBySettings(settingsParseIdentityCache, settings, () =>
    JSON.stringify({
      defaultLanguage: settings.defaultLanguage ?? "VBScript",
      resolvedLocale: settings.resolvedLocale ?? "en",
      incremental: settings.incremental?.mode ?? "legacy",
    }),
  );
}

function includeResolutionIdentity(settings: AspSettings): string {
  const cached = includeResolutionIdentityCache.get(settings);
  if (cached?.generation === includeResolutionGeneration) {
    return cached.value;
  }
  const value = JSON.stringify({
    generation: includeResolutionGeneration,
    settings: includeResolutionSettingsKey(settings),
  });
  includeResolutionIdentityCache.set(settings, { generation: includeResolutionGeneration, value });
  return value;
}

function includeResolutionSettingsIdentity(settings: AspSettings): string {
  return memoizeBySettings(settingsIncludeResolutionIdentityCache, settings, () =>
    JSON.stringify(includeResolutionSettingsKey(settings)),
  );
}

function diagnosticsIdentity(settings: AspSettings): string {
  return memoizeBySettings(settingsDiagnosticsIdentityCache, settings, () =>
    JSON.stringify({
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
        deadCodeDiagnostics: settings.vbscript?.deadCodeDiagnostics !== false,
      },
      incrementalAnalysis: settings.incremental?.analysis !== false,
      locale: settings.resolvedLocale ?? "en",
    }),
  );
}

function jsProjectIdentity(settings: AspSettings): string {
  const cached = jsProjectIdentityCache.get(settings);
  if (cached?.generation === jsProjectGeneration) {
    return cached.value;
  }
  const value = JSON.stringify({
    generation: jsProjectGeneration,
    settings: jsProjectSettingsIdentity(settings),
  });
  jsProjectIdentityCache.set(settings, { generation: jsProjectGeneration, value });
  return value;
}

function jsProjectSettingsIdentity(settings: AspSettings): string {
  const rootsIdentity = sortedWorkspaceRootsIdentity();
  const cached = settingsJsProjectIdentityCache.get(settings);
  if (cached?.rootsKey === rootsIdentity.key) {
    return cached.value;
  }
  const value = JSON.stringify({
    checkJs: settings.checkJs === true,
    javascript: {
      autoImports: settings.javascript?.autoImports !== false,
      unusedDiagnostics: settings.javascript?.unusedDiagnostics !== false,
      ignoreProjectConfig: settings.javascript?.ignoreProjectConfig === true,
      compilerOptions: settings.javascript?.compilerOptions ?? {},
    },
    roots: rootsIdentity.roots,
  });
  settingsJsProjectIdentityCache.set(settings, { rootsKey: rootsIdentity.key, value });
  return value;
}

function workspaceIndexSettingsIdentity(settings: AspSettings): string {
  const rootsIdentity = workspaceIndexRootsIdentity();
  const cached = settingsWorkspaceIndexIdentityCache.get(settings);
  if (cached?.rootsKey === rootsIdentity.key) {
    return cached.value;
  }
  const value = JSON.stringify({
    roots: rootsIdentity.roots,
    includes: settings.workspace?.includes ?? defaultWorkspaceIncludes,
    excludes: settings.workspace?.excludes ?? [],
    respectGitIgnore: settings.workspace?.respectGitIgnore === true,
    maxIndexFiles: settings.workspace?.maxIndexFiles ?? defaultMaxIndexFiles,
    scanChunkSize: settings.workspace?.scanChunkSize ?? defaultScanChunkSize,
  });
  settingsWorkspaceIndexIdentityCache.set(settings, { rootsKey: rootsIdentity.key, value });
  return value;
}

function cacheSettingsIdentity(settings: AspSettings): string {
  return memoizeBySettings(settingsCacheIdentityCache, settings, () =>
    JSON.stringify({
      ...normalizeCacheSettings(settings),
      freshness: cacheFreshness(settings),
    }),
  );
}

function clearCacheSettingProcessStateIfChanged(previous: AspSettings, next: AspSettings): void {
  if (cacheSettingsIdentity(previous) === cacheSettingsIdentity(next)) {
    return;
  }
  clearWorkspaceIndexProcessCaches("settings.cache");
  clearIncludeCaches();
  clearWorkspaceVbReferenceCaches();
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
  const cached = cachedDocumentForUri(uri);
  if (!impact) {
    return;
  }
  if (impact.parse) {
    deleteCachedDocumentsForUri(uri);
    vbProjectContextCache.clear();
    clearInteractiveVbProjectContextSnapshotsForUris([uri]);
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
    const previousGlobalSettings = globalSettings;
    const previousSettingsByUri = currentOpenDocumentSettingsByUri();
    globalSettings = normalizeSettings(
      (await connection.workspace.getConfiguration("aspLsp")) as Record<string, unknown>,
    );
    configureFsGateway(globalSettings);
    await configureDiskAnalysisCacheAsync();
    clearCacheSettingProcessStateIfChanged(previousGlobalSettings, globalSettings);
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
    configureFsGateway(globalSettings);
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
    rename: normalizeRenameSettings(settings),
    styleExtraction: normalizeStyleExtractionSettings(settings),
    flowchart: normalizeFlowchartSettings(settings),
    navigationGraph: normalizeNavigationGraphSettings(settings),
    graph: normalizeGraphSettings(settings),
    excel: normalizeExcelSettings(settings),
    cache: normalizeCacheSettings(settings),
    memory: normalizeMemorySettings(settings),
    network: normalizeNetworkSettings(settings),
    workspace: normalizeWorkspaceSettings(settings),
    incremental: normalizeIncrementalSettings(settings),
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
    includes: normalizeStringArraySetting(record.includes, defaultWorkspaceIncludes),
    excludes: normalizeStringArraySetting(record.excludes, []),
    respectGitIgnore: record.respectGitIgnore === true,
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
      positiveIntegerFromEnv("ASP_LSP_VB_PROJECT_MAX_DOCUMENTS", defaultVbProjectMaxDocuments),
    ),
    vbProjectMaxTextLength: positiveIntegerSetting(
      record.vbProjectMaxTextLength,
      positiveIntegerFromEnv("ASP_LSP_VB_PROJECT_MAX_TEXT_LENGTH", defaultVbProjectMaxTextLength),
    ),
  };
}

function normalizeIncrementalSettings(
  settings: Record<string, unknown> | AspSettings,
): NonNullable<AspSettings["incremental"]> {
  const raw = settings.incremental;
  const record = raw && typeof raw === "object" ? (raw as Record<string, unknown>) : {};
  return {
    mode:
      record.mode === "full" || record.mode === "off" || record.mode === "legacy"
        ? record.mode
        : "full",
    analysis: record.analysis !== false,
  };
}

function normalizeStringArraySetting(value: unknown, fallback: string[]): string[] {
  if (!Array.isArray(value)) {
    return [...fallback];
  }
  const items = value
    .filter((item): item is string => typeof item === "string" && item.trim().length > 0)
    .map((item) => item.trim());
  return items.length > 0 ? items : [...fallback];
}

function positiveIntegerSetting(value: unknown, fallback: number): number {
  return typeof value === "number" && value > 0 ? Math.floor(value) : fallback;
}

function optionalNonNegativeIntegerSetting(value: unknown): number | undefined {
  return typeof value === "number" && Number.isFinite(value) && value >= 0
    ? Math.floor(value)
    : undefined;
}

function optionalPositiveIntegerSetting(value: unknown): number | undefined {
  return typeof value === "number" && Number.isFinite(value) && value > 0
    ? Math.floor(value)
    : undefined;
}

function normalizeNetworkSettings(
  settings: Record<string, unknown> | AspSettings,
): AspSettings["network"] {
  const raw = settings.network;
  const record = raw && typeof raw === "object" ? (raw as Record<string, unknown>) : {};
  return {
    profile:
      record.profile === "local" || record.profile === "network" || record.profile === "auto"
        ? record.profile
        : "auto",
    statCacheTtlMs: optionalNonNegativeIntegerSetting(record.statCacheTtlMs),
    readdirCacheTtlMs: optionalNonNegativeIntegerSetting(record.readdirCacheTtlMs),
    includeReadConcurrency: optionalPositiveIntegerSetting(record.includeReadConcurrency),
    caseResolution:
      record.caseResolution === "auto" ||
      record.caseResolution === "full" ||
      record.caseResolution === "fast"
        ? record.caseResolution
        : "auto",
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
    freshness:
      record.freshness === "watch" || record.freshness === "metadata" || record.freshness === "auto"
        ? record.freshness
        : "auto",
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

function normalizeMemorySettings(
  settings: Record<string, unknown> | AspSettings,
): NonNullable<AspSettings["memory"]> {
  const raw = settings.memory;
  const record = raw && typeof raw === "object" ? (raw as Record<string, unknown>) : {};
  return {
    maxCacheBytes: positiveIntegerSetting(record.maxCacheBytes, defaultMemoryMaxCacheBytes),
    debugTelemetry: record.debugTelemetry === true,
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
    logFile: normalizeDebugLogFileSettings(record.logFile),
  };
}

function normalizeDebugOutputLevel(value: unknown): NonNullable<AspSettings["debug"]>["output"] {
  return value === "summary" || value === "verbose" ? value : "off";
}

function normalizeDebugLogFileSettings(
  value: unknown,
): NonNullable<AspSettings["debug"]>["logFile"] {
  const record = value && typeof value === "object" ? (value as Record<string, unknown>) : {};
  return {
    enabled: record.enabled === true,
    path: typeof record.path === "string" ? record.path.trim() : "",
  };
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
    scopeMarkers: normalizeInlayScopeMarkerSettings(record.scopeMarkers),
  };
}

function normalizeInlayScopeMarkerSettings(
  value: unknown,
): NonNullable<NonNullable<AspSettings["inlayHints"]>["scopeMarkers"]> {
  const record = value && typeof value === "object" ? (value as Record<string, unknown>) : {};
  return {
    global: record.global === true,
    local: record.local === true,
    uncertain: record.uncertain === true,
  };
}

function normalizeCodeLensSettings(
  settings: Record<string, unknown> | AspSettings,
): AspSettings["codeLens"] {
  const raw = settings.codeLens;
  const record = raw && typeof raw === "object" ? (raw as Record<string, unknown>) : {};
  const referenceScope = record.referenceScope === "workspace" ? "workspace" : "analyzed";
  return {
    references: record.references !== false,
    includes: record.includes === true,
    referenceScope,
    referenceProcedures: record.referenceProcedures !== false,
    referenceGlobals: record.referenceGlobals !== false,
    referenceClasses: record.referenceClasses !== false,
    referenceClassMembers: record.referenceClassMembers !== false,
    includeRelatedIncludeTreesForUnresolved:
      referenceScope === "workspace" && record.includeRelatedIncludeTreesForUnresolved !== false,
  };
}

function normalizeRenameSettings(
  settings: Record<string, unknown> | AspSettings,
): AspSettings["rename"] {
  const raw = settings.rename;
  const record = raw && typeof raw === "object" ? (raw as Record<string, unknown>) : {};
  return {
    updateIncludesOnFileRename: record.updateIncludesOnFileRename === true,
    workspaceSymbolRename: record.workspaceSymbolRename === true,
  };
}

function normalizeStyleExtractionSettings(
  settings: Record<string, unknown> | AspSettings,
): AspSettings["styleExtraction"] {
  const raw = settings.styleExtraction;
  const record = raw && typeof raw === "object" ? (raw as Record<string, unknown>) : {};
  return {
    insertionMode:
      record.insertionMode === "reuseExistingStyleTag" ? "reuseExistingStyleTag" : "nearby",
  };
}

function normalizeFlowchartSettings(
  settings: Record<string, unknown> | AspSettings,
): AspSettings["flowchart"] {
  const raw = settings.flowchart;
  const record = raw && typeof raw === "object" ? (raw as Record<string, unknown>) : {};
  return {
    labelLineLength: Math.max(8, positiveIntegerSetting(record.labelLineLength, 34)),
    labelMode: flowchartLabelMode(record.labelMode),
  };
}

function normalizeNavigationGraphSettings(
  settings: Record<string, unknown> | AspSettings,
): NonNullable<AspSettings["navigationGraph"]> {
  const raw = settings.navigationGraph;
  const record = raw && typeof raw === "object" ? (raw as Record<string, unknown>) : {};
  return {
    maxNodes: positiveIntegerSetting(record.maxNodes, 500),
    maxEdges: positiveIntegerSetting(record.maxEdges, 1200),
  };
}

function normalizeGraphSettings(
  settings: Record<string, unknown> | AspSettings,
): NonNullable<AspSettings["graph"]> {
  const raw = settings.graph;
  const record = raw && typeof raw === "object" ? (raw as Record<string, unknown>) : {};
  return {
    initialViewMode: record.initialViewMode === "3d" ? "3d" : "2d",
    showRootNodes: record.showRootNodes !== false,
    showFileNodes: record.showFileNodes !== false,
    showFunctionNodes: record.showFunctionNodes !== false,
    showSubNodes: record.showSubNodes !== false,
    showClassNodes: record.showClassNodes !== false,
    showMethodNodes: record.showMethodNodes === true,
    showMethodFunctionNodes: record.showMethodFunctionNodes === true,
    showMethodSubNodes: record.showMethodSubNodes === true,
    showPropertyNodes: record.showPropertyNodes === true,
    showMemberNodes: record.showMemberNodes === true,
    showGlobalVariableNodes: record.showGlobalVariableNodes !== false,
    showGlobalConstantNodes: record.showGlobalConstantNodes !== false,
    showLocalVariableNodes: record.showLocalVariableNodes === true,
    showLocalConstantNodes: record.showLocalConstantNodes === true,
    showParameterNodes: record.showParameterNodes === true,
    showUnresolvedNodes: record.showUnresolvedNodes !== false,
    hideSingleNodes: record.hideSingleNodes !== false,
    hideUnreferencedGlobalSymbols: record.hideUnreferencedGlobalSymbols !== false,
    showOutgoingSelectionLinks: record.showOutgoingSelectionLinks !== false,
    showIncludeLinks: record.showIncludeLinks !== false,
    showDeclareLinks: record.showDeclareLinks !== false,
    showReferenceLinks: record.showReferenceLinks !== false,
    showAssignmentLinks: record.showAssignmentLinks !== false,
    showCallLinks: record.showCallLinks !== false,
    showUnresolvedLinks: record.showUnresolvedLinks !== false,
    showMemberLinks: record.showMemberLinks !== false,
    showIncomingDocumentIncludes: record.showIncomingDocumentIncludes === true,
    showIncomingFolderIncludes: record.showIncomingFolderIncludes === true,
    includeRelatedIncludeTreesForUnresolved:
      record.includeRelatedIncludeTreesForUnresolved !== false,
    useReverseIncludeIndex: record.useReverseIncludeIndex !== false,
    maxDocuments: positiveIntegerSetting(record.maxDocuments, defaultGraphMaxDocuments),
    maxTextLength: positiveIntegerSetting(record.maxTextLength, defaultGraphMaxTextLength),
    maxNodes: positiveIntegerSetting(record.maxNodes, defaultGraphMaxNodes),
    includeTreeMaxDocuments: positiveIntegerSetting(
      record.includeTreeMaxDocuments,
      defaultVbProjectMaxDocuments,
    ),
    includeTreeMaxTextLength: positiveIntegerSetting(
      record.includeTreeMaxTextLength,
      defaultVbProjectMaxTextLength,
    ),
    workerSymbolExtraction: record.workerSymbolExtraction === true,
  };
}

function normalizeExcelSettings(
  settings: Record<string, unknown> | AspSettings,
): NonNullable<AspSettings["excel"]> {
  const raw = settings.excel;
  const record = raw && typeof raw === "object" ? (raw as Record<string, unknown>) : {};
  return {
    includeRelatedIncludeTreesForUnresolved:
      record.includeRelatedIncludeTreesForUnresolved !== false,
    skipTypeInference: record.skipTypeInference === true,
    locale: normalizeLocaleSetting(record.locale),
    maxDocuments: positiveIntegerSetting(record.maxDocuments, defaultExcelMaxDocuments),
    maxTextLength: positiveIntegerSetting(record.maxTextLength, defaultExcelMaxTextLength),
    includeTreeMaxDocuments: positiveIntegerSetting(
      record.includeTreeMaxDocuments,
      defaultExcelIncludeTreeMaxDocuments,
    ),
    includeTreeMaxTextLength: positiveIntegerSetting(
      record.includeTreeMaxTextLength,
      defaultExcelIncludeTreeMaxTextLength,
    ),
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
    deadCodeDiagnostics: record.deadCodeDiagnostics !== false,
    syntaxSnippets: record.syntaxSnippets !== false,
    syntaxKeywords: record.syntaxKeywords !== false,
    showUnresolvedSymbolsInCompletion: record.showUnresolvedSymbolsInCompletion === true,
    initializedDimQuickFixStyle: normalizeInitializedDimQuickFixStyle(
      record.initializedDimQuickFixStyle,
    ),
  };
}

function normalizeInitializedDimQuickFixStyle(
  value: unknown,
): NonNullable<NonNullable<AspSettings["vbscript"]>["initializedDimQuickFixStyle"]> {
  return value === "newline" ? "newline" : "sameLineColon";
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
    compilerOptions:
      record.compilerOptions &&
      typeof record.compilerOptions === "object" &&
      !Array.isArray(record.compilerOptions)
        ? { ...(record.compilerOptions as Record<string, unknown>) }
        : {},
  };
}

function normalizeFormatSettings(
  settings: Record<string, unknown> | AspSettings,
): AspSettings["format"] {
  const raw = settings.format;
  const record = raw && typeof raw === "object" ? (raw as Record<string, unknown>) : {};
  return {
    indentSize: optionalPositiveIntegerSetting(record.indentSize),
    indentStyle: formatIndentStyle(record.indentStyle),
    printWidth: optionalPositiveIntegerSetting(record.printWidth),
    endOfLine: stringUnion(record.endOfLine, ["lf", "crlf", "auto"]),
    insertFinalNewline: optionalBooleanSetting(record.insertFinalNewline),
    preserveNewLines: optionalBooleanSetting(record.preserveNewLines),
    maxPreserveNewLines: optionalNonNegativeIntegerSetting(record.maxPreserveNewLines),
    indentEmptyLines: optionalBooleanSetting(record.indentEmptyLines),
    enabledLanguages: formatLanguageArray(record.enabledLanguages),
    embeddedLanguageFormatting: stringUnion(record.embeddedLanguageFormatting, ["auto", "off"]),
    respectDisableRegions: optionalBooleanSetting(record.respectDisableRegions),
    htmlIndentSize: optionalPositiveIntegerSetting(record.htmlIndentSize),
    htmlIndentStyle: formatIndentStyle(record.htmlIndentStyle),
    htmlWrapLineLength: optionalNonNegativeIntegerSetting(record.htmlWrapLineLength),
    htmlWrapAttributes: stringUnion(record.htmlWrapAttributes, [
      "auto",
      "force",
      "force-aligned",
      "force-expand-multiline",
      "aligned-multiple",
      "preserve",
      "preserve-aligned",
    ]),
    htmlWrapAttributesIndentSize: optionalPositiveIntegerSetting(
      record.htmlWrapAttributesIndentSize,
    ),
    htmlIndentInnerHtml: optionalBooleanSetting(record.htmlIndentInnerHtml),
    htmlUnformatted: optionalNonEmptyString(record.htmlUnformatted),
    htmlContentUnformatted: optionalNonEmptyString(record.htmlContentUnformatted),
    htmlExtraLiners: optionalNonEmptyString(record.htmlExtraLiners),
    cssIndentSize: optionalPositiveIntegerSetting(record.cssIndentSize),
    cssIndentStyle: formatIndentStyle(record.cssIndentStyle),
    cssWrapLineLength: optionalNonNegativeIntegerSetting(record.cssWrapLineLength),
    cssNewlineBetweenRules: optionalBooleanSetting(record.cssNewlineBetweenRules),
    cssNewlineBetweenSelectors: optionalBooleanSetting(record.cssNewlineBetweenSelectors),
    cssSpaceAroundSelectorSeparator: optionalBooleanSetting(record.cssSpaceAroundSelectorSeparator),
    cssBraceStyle: stringUnion(record.cssBraceStyle, ["collapse", "expand"]),
    javascriptIndentSize: optionalPositiveIntegerSetting(record.javascriptIndentSize),
    javascriptIndentStyle: formatIndentStyle(record.javascriptIndentStyle),
    jscriptIndentSize: optionalPositiveIntegerSetting(record.jscriptIndentSize),
    jscriptIndentStyle: formatIndentStyle(record.jscriptIndentStyle),
    javascriptSemicolons: stringUnion(record.javascriptSemicolons, ["ignore", "insert", "remove"]),
    javascriptIndentSwitchCase: optionalBooleanSetting(record.javascriptIndentSwitchCase),
    javascriptPlaceOpenBraceOnNewLineForFunctions: optionalBooleanSetting(
      record.javascriptPlaceOpenBraceOnNewLineForFunctions,
    ),
    javascriptPlaceOpenBraceOnNewLineForControlBlocks: optionalBooleanSetting(
      record.javascriptPlaceOpenBraceOnNewLineForControlBlocks,
    ),
    javascriptInsertSpaceAfterCommaDelimiter: optionalBooleanSetting(
      record.javascriptInsertSpaceAfterCommaDelimiter,
    ),
    javascriptInsertSpaceAfterSemicolonInForStatements: optionalBooleanSetting(
      record.javascriptInsertSpaceAfterSemicolonInForStatements,
    ),
    javascriptInsertSpaceBeforeAndAfterBinaryOperators: optionalBooleanSetting(
      record.javascriptInsertSpaceBeforeAndAfterBinaryOperators,
    ),
    javascriptInsertSpaceAfterKeywordsInControlFlowStatements: optionalBooleanSetting(
      record.javascriptInsertSpaceAfterKeywordsInControlFlowStatements,
    ),
    javascriptInsertSpaceAfterFunctionKeywordForAnonymousFunctions: optionalBooleanSetting(
      record.javascriptInsertSpaceAfterFunctionKeywordForAnonymousFunctions,
    ),
    javascriptInsertSpaceAfterOpeningAndBeforeClosingNonemptyParenthesis: optionalBooleanSetting(
      record.javascriptInsertSpaceAfterOpeningAndBeforeClosingNonemptyParenthesis,
    ),
    javascriptInsertSpaceAfterOpeningAndBeforeClosingNonemptyBrackets: optionalBooleanSetting(
      record.javascriptInsertSpaceAfterOpeningAndBeforeClosingNonemptyBrackets,
    ),
    javascriptInsertSpaceAfterOpeningAndBeforeClosingNonemptyBraces: optionalBooleanSetting(
      record.javascriptInsertSpaceAfterOpeningAndBeforeClosingNonemptyBraces,
    ),
    javascriptInsertSpaceAfterOpeningAndBeforeClosingEmptyBraces: optionalBooleanSetting(
      record.javascriptInsertSpaceAfterOpeningAndBeforeClosingEmptyBraces,
    ),
    javascriptInsertSpaceBeforeFunctionParenthesis: optionalBooleanSetting(
      record.javascriptInsertSpaceBeforeFunctionParenthesis,
    ),
    vbscriptIndentSize: optionalPositiveIntegerSetting(record.vbscriptIndentSize),
    vbscriptIndentStyle: formatIndentStyle(record.vbscriptIndentStyle),
    vbscriptKeywordCase: stringUnion(record.vbscriptKeywordCase, [
      "preserve",
      "upper",
      "lower",
      "title",
    ]),
    vbscriptLineContinuationIndentSize: optionalPositiveIntegerSetting(
      record.vbscriptLineContinuationIndentSize,
    ),
    vbscriptSelectCaseIndent: stringUnion(record.vbscriptSelectCaseIndent, [
      "caseIndented",
      "caseAligned",
    ]),
    uppercaseKeywords: record.uppercaseKeywords === true,
    alignAssignments: record.alignAssignments === true,
    onSave: record.onSave === true,
    vbscriptBlockIndent: stringUnion(record.vbscriptBlockIndent, [
      "alignWithDelimiter",
      "indentInsideDelimiter",
    ]),
    vbscriptTagIndentMode: formatTagIndentMode(record.vbscriptTagIndentMode),
    cssTagIndentMode: formatTagIndentMode(record.cssTagIndentMode),
    javascriptTagIndentMode: formatTagIndentMode(record.javascriptTagIndentMode),
    aspDelimiterSpacing: stringUnion(record.aspDelimiterSpacing, ["padded", "compact"]),
    aspBlockNewline: stringUnion(record.aspBlockNewline, [
      "preserve",
      "alwaysMultiline",
      "singleLineWhenPossible",
    ]),
    nestedAspInCssJs: stringUnion(record.nestedAspInCssJs, [
      "skipRegion",
      "protectAspOnly",
      "formatAroundAsp",
    ]),
    fragmentMode: stringUnion(record.fragmentMode, ["auto", "fragment", "document"]),
    ignoreVbscriptTagIndent: record.ignoreVbscriptTagIndent === true,
    ignoreCssTagIndent: record.ignoreCssTagIndent === true,
    ignoreJavaScriptTagIndent: record.ignoreJavaScriptTagIndent === true,
  };
}

function optionalBooleanSetting(value: unknown): boolean | undefined {
  return typeof value === "boolean" ? value : undefined;
}

function optionalNonEmptyString(value: unknown): string | undefined {
  return typeof value === "string" && value.trim().length > 0 ? value : undefined;
}

function formatIndentStyle(value: unknown): "space" | "tab" | undefined {
  return stringUnion(value, ["space", "tab"]);
}

function formatTagIndentMode(
  value: unknown,
): "relativeToTag" | "ignoreTag" | "preserveExisting" | undefined {
  return stringUnion(value, ["relativeToTag", "ignoreTag", "preserveExisting"]);
}

function formatLanguageArray(
  value: unknown,
): Array<"html" | "vbscript" | "css" | "javascript" | "jscript"> | undefined {
  if (!Array.isArray(value)) {
    return undefined;
  }
  const languages = value.filter(
    (item): item is "html" | "vbscript" | "css" | "javascript" | "jscript" =>
      item === "html" ||
      item === "vbscript" ||
      item === "css" ||
      item === "javascript" ||
      item === "jscript",
  );
  return languages.length > 0 ? [...new Set(languages)] : undefined;
}

function stringUnion<const T extends string>(value: unknown, values: readonly T[]): T | undefined {
  return typeof value === "string" && values.includes(value as T) ? (value as T) : undefined;
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

function isFormatLanguageEnabled(
  options: AspFormattingOptions,
  language: "html" | "vbscript" | "css" | "javascript" | "jscript",
): boolean {
  return !options.enabledLanguages || options.enabledLanguages.includes(language);
}

function formatDisabledProtection(
  text: string,
  options: AspFormattingOptions,
): { restore: (formatted: string) => string; text: string } {
  if (options.respectDisableRegions === false) {
    return { restore: (formatted) => formatted, text };
  }
  const spans = formatDisabledSpans(text);
  if (spans.length === 0) {
    return { restore: (formatted) => formatted, text };
  }
  const replacements = spans.map((span, index) => ({
    ...span,
    placeholder: formatDisabledPlaceholder(text, index),
    text: text.slice(span.start, span.end),
  }));
  const protectedText = applyOffsetEdits(
    text,
    replacements.map((span) => ({
      start: span.start,
      end: span.end,
      newText: span.placeholder,
    })),
  );
  return {
    text: protectedText,
    restore: (formatted) => {
      let restored = formatted;
      for (const span of replacements) {
        if (placeholderCount(restored, span.placeholder) !== 1) {
          return text;
        }
        restored = restored.replace(span.placeholder, span.text);
      }
      return restored;
    },
  };
}

function formatRangeOverlapsDisabled(
  text: string,
  start: number,
  end: number,
  options: AspFormattingOptions,
): boolean {
  return (
    options.respectDisableRegions !== false &&
    formatDisabledSpans(text).some((span) => span.start < end && span.end > start)
  );
}

function formatDisabledSpans(text: string): Array<{ end: number; start: number }> {
  const spans: Array<{ end: number; start: number }> = [];
  const marker = /(?:asp-lsp-format|asp-format)\s+(off|on)\b/gi;
  let disabledStart: number | undefined;
  let match: RegExpExecArray | null;
  while ((match = marker.exec(text))) {
    const markerLineStart = lineStartOffset(text, match.index);
    const markerLineEnd = lineEndOffset(text, match.index);
    if (match[1].toLowerCase() === "off") {
      disabledStart ??= markerLineStart;
    } else if (disabledStart !== undefined) {
      spans.push({ start: disabledStart, end: markerLineEnd });
      disabledStart = undefined;
    }
  }
  if (disabledStart !== undefined) {
    spans.push({ start: disabledStart, end: text.length });
  }
  return spans;
}

function formatDisabledPlaceholder(text: string, index: number): string {
  let placeholder = `AspLspFormatDisabledMarker${index}`;
  while (text.includes(placeholder)) {
    placeholder = `X${placeholder}X`;
  }
  return placeholder;
}

function finalizeFormattedDocument(
  formatted: string,
  original: string,
  options: AspFormattingOptions,
): string {
  let result = applyFormattedEndOfLine(formatted, original, options);
  if (options.insertFinalNewline === true && result.length > 0 && !result.endsWith("\n")) {
    result += "\n";
  }
  return result;
}

function applyFormattedEndOfLine(
  formatted: string,
  original: string,
  options: AspFormattingOptions,
): string {
  const endOfLine = options.endOfLine ?? "auto";
  if (endOfLine === "lf") {
    return formatted.replace(/\r\n?/g, "\n");
  }
  if (endOfLine === "crlf") {
    return formatted.replace(/\r\n?|\n/g, "\n").replace(/\n/g, "\r\n");
  }
  const originalEndOfLine = original.includes("\r\n") ? "\r\n" : "\n";
  return originalEndOfLine === "\n"
    ? formatted.replace(/\r\n?/g, "\n")
    : formatted.replace(/\r\n?|\n/g, "\n").replace(/\n/g, "\r\n");
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
  const protectedDocument = formatDisabledProtection(original, formattingOptions);
  const formatInput = protectedDocument.text;
  const inputParsed =
    formatInput === original
      ? cached.parsed
      : parseSourceDocument(cached.source.uri, formatInput, settings, cached.source.languageId);
  let formatted = measureDebugStep(settings, cached.source.uri, "format.html", () =>
    isFormatLanguageEnabled(formattingOptions, "html")
      ? formatHtmlDocumentWithPlaceholders(inputParsed, formatInput, formattingOptions)
      : formatInput,
  );
  let parsed = measureDebugStep(settings, cached.source.uri, "format.html.reparse", () =>
    formatted === formatInput
      ? inputParsed
      : parseSourceDocument(cached.source.uri, formatted, settings, cached.source.languageId),
  );
  formatted = measureDebugStep(settings, cached.source.uri, "format.core", () =>
    isFormatLanguageEnabled(formattingOptions, "vbscript")
      ? applyTextEdits(formatted, formatAspDocument(parsed, formattingOptions))
      : formatted,
  );
  parsed = measureDebugStep(settings, cached.source.uri, "format.reparse", () =>
    parseSourceDocument(cached.source.uri, formatted, settings, cached.source.languageId),
  );
  formatted = applyOffsetEdits(
    formatted,
    embeddedFormattingEdits(parsed, formatted, formattingOptions, settings, cached.source.uri),
  );
  formatted = finalizeFormattedDocument(
    protectedDocument.restore(formatted),
    original,
    formattingOptions,
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
  const protectedDocument = formatDisabledProtection(original, formattingOptions);
  const formatInput = protectedDocument.text;
  const inputParsed =
    formatInput === original
      ? cached.parsed
      : await parseSourceDocumentAsync(
          cached.source.uri,
          formatInput,
          settings,
          cached.source.languageId,
        );
  await hydrateVbscriptCst(inputParsed, settings);
  let formatted = measureDebugStep(settings, cached.source.uri, "format.html", () =>
    isFormatLanguageEnabled(formattingOptions, "html")
      ? formatHtmlDocumentWithPlaceholders(inputParsed, formatInput, formattingOptions)
      : formatInput,
  );
  let parsed = await measureDebugStepAsync(settings, cached.source.uri, "format.html.reparse", () =>
    formatted === formatInput
      ? Promise.resolve(inputParsed)
      : parseSourceDocumentAsync(cached.source.uri, formatted, settings, cached.source.languageId),
  );
  await hydrateVbscriptCst(parsed, settings);
  formatted = measureDebugStep(settings, cached.source.uri, "format.core", () =>
    isFormatLanguageEnabled(formattingOptions, "vbscript")
      ? applyTextEdits(formatted, formatAspDocument(parsed, formattingOptions))
      : formatted,
  );
  parsed = await measureDebugStepAsync(settings, cached.source.uri, "format.reparse", () =>
    parseSourceDocumentAsync(cached.source.uri, formatted, settings, cached.source.languageId),
  );
  await hydrateVbscriptCst(parsed, settings);
  formatted = applyOffsetEdits(
    formatted,
    embeddedFormattingEdits(parsed, formatted, formattingOptions, settings, cached.source.uri),
  );
  formatted = finalizeFormattedDocument(
    protectedDocument.restore(formatted),
    original,
    formattingOptions,
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
  if (formatRangeOverlapsDisabled(original, rangeStart, rangeEnd, formattingOptions)) {
    finishFormattingLog(cached, settings, "range", startedAt, 0);
    return [];
  }
  const coreEdits = measureDebugStep(settings, cached.source.uri, "format.core", () =>
    isFormatLanguageEnabled(formattingOptions, "vbscript")
      ? formatAspRange(cached.parsed, range, formattingOptions)
      : [],
  );
  let formatted = measureDebugStep(settings, cached.source.uri, "format.core.apply", () =>
    applyTextEdits(original, coreEdits),
  );
  let formattedRangeEnd =
    coreEdits.length === 1
      ? rangeStart + coreEdits[0].newText.length
      : rangeEnd + offsetEditsDelta(original, coreEdits);
  const parsed = await measureDebugStepAsync(settings, cached.source.uri, "format.reparse", () =>
    parseSourceDocumentAsync(cached.source.uri, formatted, settings, cached.source.languageId),
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

function formatHtmlDocumentWithPlaceholders(
  parsed: AspParsedDocument,
  text: string,
  options: AspFormattingOptions,
): string {
  const protectedSpans = htmlProtectedSpans(parsed, text);
  const protectedText = applyOffsetEdits(
    text,
    protectedSpans.map((span) => ({
      start: span.start,
      end: span.end,
      newText: htmlProtectedReplacement(span, text),
    })),
  );
  const htmlInput = htmlFormatterInput(protectedText, options);
  const document = TextDocument.create("__asp_lsp_format.html", "html", 0, htmlInput.text);
  const edits = htmlService.format(document, undefined, htmlFormattingOptions(options));
  if (edits.length === 0) {
    return text;
  }
  let formatted = applyOffsetEdits(
    htmlInput.text,
    edits.map((edit) => ({
      start: offsetAtText(htmlInput.text, edit.range.start),
      end: offsetAtText(htmlInput.text, edit.range.end),
      newText: edit.newText,
    })),
  );
  const restoredHtmlInput = htmlInput.restore(formatted);
  if (restoredHtmlInput === undefined) {
    return text;
  }
  formatted = restoredHtmlInput;
  for (const span of protectedSpans) {
    if (placeholderCount(formatted, span.placeholder) !== 1) {
      return text;
    }
    formatted = formatted.replace(span.placeholder, span.text);
  }
  return formatted;
}

function htmlFormatterInput(
  text: string,
  options: AspFormattingOptions,
): { restore: (formatted: string) => string | undefined; text: string } {
  if (options.fragmentMode !== "fragment") {
    return { text, restore: (formatted) => formatted };
  }
  const tag = htmlFragmentWrapperTag(text);
  return {
    text: `<${tag}>\n${text}\n</${tag}>`,
    restore: (formatted) => unwrapHtmlFragmentText(formatted, tag, options),
  };
}

function htmlFragmentWrapperTag(text: string): string {
  let index = 0;
  let tag = "asp-lsp-fragment";
  while (new RegExp(`<\\s*/?\\s*${escapeRegExp(tag)}\\b`, "i").test(text)) {
    index += 1;
    tag = `asp-lsp-fragment-${index}`;
  }
  return tag;
}

function unwrapHtmlFragmentText(
  formatted: string,
  tag: string,
  options: AspFormattingOptions,
): string | undefined {
  const lines = formatted.replace(/\r\n?/g, "\n").split("\n");
  const escapedTag = escapeRegExp(tag);
  const opening = new RegExp(`^\\s*<${escapedTag}(?:\\s[^>]*)?>\\s*$`, "i");
  const closing = new RegExp(`^\\s*</${escapedTag}>\\s*$`, "i");
  const openingIndex = lines.findIndex((line) => opening.test(line));
  let closingIndex = -1;
  for (let index = lines.length - 1; index >= 0; index -= 1) {
    if (closing.test(lines[index])) {
      closingIndex = index;
      break;
    }
  }
  if (openingIndex === -1 || closingIndex === -1 || closingIndex < openingIndex) {
    return undefined;
  }
  const indent = indentUnitForLanguage(options, "html");
  return lines
    .slice(openingIndex + 1, closingIndex)
    .map((line) => (line.startsWith(indent) ? line.slice(indent.length) : line))
    .join("\n");
}

function htmlProtectedSpans(parsed: AspParsedDocument, text: string): HtmlProtectedSpan[] {
  const candidates = parsed.regions
    .flatMap((region) => htmlProtectedSpanCandidate(region))
    .filter((span) => span.start < span.end)
    .sort((left, right) => left.start - right.start || right.end - left.end);
  const accepted: Omit<HtmlProtectedSpan, "placeholder">[] = [];
  let coveredEnd = -1;
  for (const span of candidates) {
    if (span.start < coveredEnd) {
      continue;
    }
    accepted.push({ ...span, text: text.slice(span.start, span.end) });
    coveredEnd = span.end;
  }
  return accepted.map((span, index) => ({
    ...span,
    placeholder: htmlProtectedPlaceholder(text, index),
  }));
}

function htmlProtectedReplacement(span: HtmlProtectedSpan, text: string): string {
  if (!span.block) {
    return span.placeholder;
  }
  const beforeLinePrefix = text.slice(lineStartOffset(text, span.start), span.start);
  const afterLineSuffix = text.slice(span.end, lineEndOffset(text, span.end));
  const before = span.start > 0 && beforeLinePrefix.trim().length > 0 ? "\n" : "";
  const after = span.end < text.length && afterLineSuffix.trim().length > 0 ? "\n" : "";
  return `${before}${span.placeholder}${after}`;
}

function htmlProtectedSpanCandidate(
  region: AspRegion,
): Array<{ block: boolean; end: number; start: number }> {
  if (
    region.kind === "style" ||
    region.kind === "client-script" ||
    region.kind === "server-script" ||
    region.kind === "style-attribute"
  ) {
    return [{ block: false, start: region.contentStart, end: region.contentEnd }];
  }
  if (region.kind === "asp-block" || region.kind === "asp-directive") {
    return [{ block: true, start: region.start, end: region.end }];
  }
  if (region.kind === "asp-expression") {
    return [{ block: false, start: region.start, end: region.end }];
  }
  return [];
}

function htmlProtectedPlaceholder(text: string, index: number): string {
  let placeholder = `__ASP_LSP_FORMAT_PROTECTED_${index}__`;
  while (text.includes(placeholder)) {
    placeholder = `_${placeholder}_`;
  }
  return placeholder;
}

function placeholderCount(text: string, placeholder: string): number {
  return text.split(placeholder).length - 1;
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
  if (options.embeddedLanguageFormatting === "off") {
    finishDebugStep(settings, uri, "format.embedded", startedAt);
    return [];
  }
  const edits = [
    ...(isFormatLanguageEnabled(options, "css")
      ? measureDebugStep(settings, uri, "format.embedded.css", () =>
          cssFormattingEdits(parsed, text, options, spanStart, spanEnd),
        )
      : []),
    ...(isFormatLanguageEnabled(options, "javascript") ||
    isFormatLanguageEnabled(options, "jscript")
      ? measureDebugStep(settings, uri, "format.embedded.javascript", () =>
          javaScriptFormattingEdits(parsed, text, options, spanStart, spanEnd),
        )
      : []),
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
    .filter((region) => shouldFormatNestedAspRegion(parsed, region, options))
    .flatMap((region) => formatCssRegion(parsed, text, region, options, spanStart, spanEnd));
}

function formatCssRegion(
  parsed: AspParsedDocument,
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
  const nestedProtection =
    options.nestedAspInCssJs === "protectAspOnly" && regionHasNestedAsp(parsed, region)
      ? embeddedAspProtection(parsed, region, content)
      : undefined;
  if (nestedProtection && (localStart !== 0 || localEnd !== content.length)) {
    return [];
  }
  const formatContent = nestedProtection?.text ?? content;
  const doc = TextDocument.create("__asp_lsp_format.css", "css", 0, formatContent);
  const formatStart = nestedProtection ? 0 : localStart;
  const formatEnd = nestedProtection ? formatContent.length : localEnd;
  const edits = cssService.format(
    doc,
    { start: doc.positionAt(formatStart), end: doc.positionAt(formatEnd) },
    cssFormattingOptions(options),
  );
  const offsetEdits = edits.map((edit) => ({
    start: offsetAtText(formatContent, edit.range.start),
    end: offsetAtText(formatContent, edit.range.end),
    newText: edit.newText,
  }));
  const formattedContent = nestedProtection?.restore(applyOffsetEdits(formatContent, offsetEdits));
  if (formattedContent !== undefined) {
    if (region.kind === "style") {
      return [
        {
          start: region.contentStart,
          end: region.contentEnd,
          newText: wrapEmbeddedElementContent(
            text,
            region,
            options,
            formattedContent,
            "css",
            isTagIndentIgnored(options, "css"),
            false,
          ),
        },
      ];
    }
    return [{ start: region.contentStart, end: region.contentEnd, newText: formattedContent }];
  }
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
          "css",
          isTagIndentIgnored(options, "css"),
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
        isFormatLanguageEnabled(
          options,
          region.language === "jscript" ? "jscript" : "javascript",
        ) &&
        region.contentEnd > spanStart &&
        region.contentStart < spanEnd,
    )
    .filter((region) => shouldFormatNestedAspRegion(parsed, region, options))
    .flatMap((region) => formatJavaScriptRegion(parsed, text, region, options, spanStart, spanEnd));
}

function shouldFormatNestedAspRegion(
  parsed: AspParsedDocument,
  region: AspRegion,
  options: AspFormattingOptions,
): boolean {
  if (!regionHasNestedAsp(parsed, region)) {
    return true;
  }
  return (
    options.nestedAspInCssJs === "protectAspOnly" || options.nestedAspInCssJs === "formatAroundAsp"
  );
}

function embeddedAspProtection(
  parsed: AspParsedDocument,
  owner: AspRegion,
  content: string,
): { restore: (formatted: string) => string; text: string } | undefined {
  const spans = parsed.regions
    .filter(
      (region) =>
        region !== owner &&
        (region.kind === "asp-block" ||
          region.kind === "asp-expression" ||
          region.kind === "asp-directive") &&
        region.start >= owner.contentStart &&
        region.end <= owner.contentEnd,
    )
    .map((region, index) => ({
      start: region.start - owner.contentStart,
      end: region.end - owner.contentStart,
      placeholder: embeddedAspPlaceholder(content, index),
      text: content.slice(region.start - owner.contentStart, region.end - owner.contentStart),
    }));
  if (spans.length === 0) {
    return undefined;
  }
  const protectedText = applyOffsetEdits(
    content,
    spans.map((span) => ({
      start: span.start,
      end: span.end,
      newText: span.placeholder,
    })),
  );
  return {
    text: protectedText,
    restore: (formatted) => {
      let restored = formatted;
      for (const span of spans) {
        if (placeholderCount(restored, span.placeholder) !== 1) {
          return content;
        }
        restored = restored.replace(span.placeholder, span.text);
      }
      return restored;
    },
  };
}

function embeddedAspPlaceholder(content: string, index: number): string {
  let placeholder = `__ASP_LSP_EMBEDDED_ASP_${index}__`;
  while (content.includes(placeholder)) {
    placeholder = `_${placeholder}_`;
  }
  return placeholder;
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
  parsed: AspParsedDocument,
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
  const nestedProtection =
    options.nestedAspInCssJs === "protectAspOnly" && regionHasNestedAsp(parsed, region)
      ? embeddedAspProtection(parsed, region, content)
      : undefined;
  if (nestedProtection && (localStart !== 0 || localEnd !== content.length)) {
    return [];
  }
  const formatContent = nestedProtection?.text ?? content;
  const formatOptions = tsFormatOptions(
    options,
    region.language === "jscript" ? "jscript" : "javascript",
    embeddedBodyBaseIndentSize(
      text,
      region,
      options,
      isTagIndentIgnored(options, region.language === "jscript" ? "jscript" : "javascript"),
    ),
  );
  if (localStart === 0 && localEnd === content.length) {
    const trimmedContent = formatContent.trim();
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
    const restored = nestedProtection?.restore(formatted) ?? formatted;
    return [
      {
        start: region.contentStart,
        end: region.contentEnd,
        newText: wrapEmbeddedElementContent(
          text,
          region,
          options,
          restored,
          region.language === "jscript" ? "jscript" : "javascript",
          isTagIndentIgnored(options, region.language === "jscript" ? "jscript" : "javascript"),
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
  return ts.createLanguageService(
    {
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
    },
    jsDocumentRegistry,
  );
}

function htmlFormattingOptions(options: AspFormattingOptions) {
  return {
    tabSize: formatIndentSizeForLanguage(options, "html"),
    insertSpaces: formatInsertSpacesForLanguage(options, "html"),
    indentEmptyLines: options.indentEmptyLines,
    wrapLineLength: options.htmlWrapLineLength ?? options.printWidth,
    unformatted: options.htmlUnformatted,
    contentUnformatted: options.htmlContentUnformatted,
    indentInnerHtml: options.htmlIndentInnerHtml,
    wrapAttributes: options.htmlWrapAttributes,
    wrapAttributesIndentSize: options.htmlWrapAttributesIndentSize,
    preserveNewLines: options.preserveNewLines,
    maxPreserveNewLines: options.maxPreserveNewLines,
    endWithNewline: options.insertFinalNewline,
    extraLiners: options.htmlExtraLiners,
  };
}

function cssFormattingOptions(options: AspFormattingOptions) {
  return {
    tabSize: formatIndentSizeForLanguage(options, "css"),
    insertSpaces: formatInsertSpacesForLanguage(options, "css"),
    insertFinalNewline: options.insertFinalNewline,
    newlineBetweenRules: options.cssNewlineBetweenRules,
    newlineBetweenSelectors: options.cssNewlineBetweenSelectors,
    spaceAroundSelectorSeparator: options.cssSpaceAroundSelectorSeparator,
    braceStyle: options.cssBraceStyle,
    preserveNewLines: options.preserveNewLines,
    maxPreserveNewLines: options.maxPreserveNewLines,
    wrapLineLength: options.cssWrapLineLength ?? options.printWidth,
    indentEmptyLines: options.indentEmptyLines,
  };
}

function tsFormatOptions(
  options: AspFormattingOptions,
  language: "javascript" | "jscript",
  baseIndentSize?: number,
): ts.FormatCodeSettings {
  return {
    baseIndentSize,
    indentSize: formatIndentSizeForLanguage(options, language),
    tabSize: options.tabSize,
    convertTabsToSpaces: formatInsertSpacesForLanguage(options, language),
    newLineCharacter: "\n",
    semicolons: tsSemicolonPreference(options.javascriptSemicolons),
    indentSwitchCase: options.javascriptIndentSwitchCase,
    placeOpenBraceOnNewLineForFunctions: options.javascriptPlaceOpenBraceOnNewLineForFunctions,
    placeOpenBraceOnNewLineForControlBlocks:
      options.javascriptPlaceOpenBraceOnNewLineForControlBlocks,
    insertSpaceAfterCommaDelimiter: options.javascriptInsertSpaceAfterCommaDelimiter,
    insertSpaceAfterSemicolonInForStatements:
      options.javascriptInsertSpaceAfterSemicolonInForStatements,
    insertSpaceBeforeAndAfterBinaryOperators:
      options.javascriptInsertSpaceBeforeAndAfterBinaryOperators ?? true,
    insertSpaceAfterKeywordsInControlFlowStatements:
      options.javascriptInsertSpaceAfterKeywordsInControlFlowStatements,
    insertSpaceAfterFunctionKeywordForAnonymousFunctions:
      options.javascriptInsertSpaceAfterFunctionKeywordForAnonymousFunctions,
    insertSpaceAfterOpeningAndBeforeClosingNonemptyParenthesis:
      options.javascriptInsertSpaceAfterOpeningAndBeforeClosingNonemptyParenthesis,
    insertSpaceAfterOpeningAndBeforeClosingNonemptyBrackets:
      options.javascriptInsertSpaceAfterOpeningAndBeforeClosingNonemptyBrackets,
    insertSpaceAfterOpeningAndBeforeClosingNonemptyBraces:
      options.javascriptInsertSpaceAfterOpeningAndBeforeClosingNonemptyBraces,
    insertSpaceAfterOpeningAndBeforeClosingEmptyBraces:
      options.javascriptInsertSpaceAfterOpeningAndBeforeClosingEmptyBraces,
    insertSpaceBeforeFunctionParenthesis: options.javascriptInsertSpaceBeforeFunctionParenthesis,
  };
}

function tsSemicolonPreference(
  value: AspFormattingOptions["javascriptSemicolons"],
): ts.SemicolonPreference | undefined {
  switch (value) {
    case "ignore":
      return ts.SemicolonPreference.Ignore;
    case "insert":
      return ts.SemicolonPreference.Insert;
    case "remove":
      return ts.SemicolonPreference.Remove;
    default:
      return undefined;
  }
}

function formatIndentSizeForLanguage(
  options: AspFormattingOptions,
  language: "html" | "css" | "javascript" | "jscript",
): number {
  switch (language) {
    case "html":
      return options.htmlIndentSize ?? options.indentSize ?? options.tabSize;
    case "css":
      return options.cssIndentSize ?? options.indentSize ?? options.tabSize;
    case "javascript":
      return options.javascriptIndentSize ?? options.indentSize ?? options.tabSize;
    case "jscript":
      return (
        options.jscriptIndentSize ??
        options.javascriptIndentSize ??
        options.indentSize ??
        options.tabSize
      );
  }
}

function formatIndentStyleForLanguage(
  options: AspFormattingOptions,
  language: "html" | "css" | "javascript" | "jscript",
): "space" | "tab" {
  const fallback = options.indentStyle ?? (options.insertSpaces ? "space" : "tab");
  switch (language) {
    case "html":
      return options.htmlIndentStyle ?? fallback;
    case "css":
      return options.cssIndentStyle ?? fallback;
    case "javascript":
      return options.javascriptIndentStyle ?? fallback;
    case "jscript":
      return options.jscriptIndentStyle ?? options.javascriptIndentStyle ?? fallback;
  }
}

function formatInsertSpacesForLanguage(
  options: AspFormattingOptions,
  language: "html" | "css" | "javascript" | "jscript",
): boolean {
  return formatIndentStyleForLanguage(options, language) !== "tab";
}

function indentUnitForLanguage(
  options: AspFormattingOptions,
  language: "html" | "css" | "javascript" | "jscript",
): string {
  return formatInsertSpacesForLanguage(options, language)
    ? " ".repeat(formatIndentSizeForLanguage(options, language))
    : "\t";
}

function isJavaScriptLikeRegion(region: AspRegion): boolean {
  return region.language === "javascript" || region.language === "jscript";
}

function wrapEmbeddedElementContent(
  text: string,
  region: AspRegion,
  options: AspFormattingOptions,
  content: string,
  language: "css" | "javascript" | "jscript",
  ignoreTagIndent: boolean,
  contentAlreadyIndented: boolean,
): string {
  const trimmed = contentAlreadyIndented ? trimOuterBlankLines(content) : content.trim();
  if (trimmed.length === 0) {
    return "";
  }
  const tagIndent = embeddedTagIndent(text, region, options, language, ignoreTagIndent);
  const body =
    contentAlreadyIndented || ignoreTagIndent
      ? trimmed
      : indentLines(trimmed, `${tagIndent}${indentUnitForLanguage(options, language)}`);
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
  const language = region.language === "jscript" ? "jscript" : "javascript";
  return (
    leadingIndentWidth(text, region.start, options) + formatIndentSizeForLanguage(options, language)
  );
}

function isTagIndentIgnored(
  options: AspFormattingOptions,
  language: "css" | "javascript" | "jscript",
): boolean {
  const mode = tagIndentMode(options, language);
  return mode === "ignoreTag";
}

function tagIndentMode(
  options: AspFormattingOptions,
  language: "css" | "javascript" | "jscript",
): NonNullable<AspFormattingOptions["cssTagIndentMode"]> {
  if (language === "css") {
    return options.cssTagIndentMode ?? (options.ignoreCssTagIndent ? "ignoreTag" : "relativeToTag");
  }
  return (
    options.javascriptTagIndentMode ??
    (options.ignoreJavaScriptTagIndent ? "ignoreTag" : "relativeToTag")
  );
}

function embeddedTagIndent(
  text: string,
  region: AspRegion,
  options: AspFormattingOptions,
  language: "css" | "javascript" | "jscript",
  ignoreTagIndent: boolean,
): string {
  if (ignoreTagIndent) {
    return "";
  }
  if (tagIndentMode(options, language) === "preserveExisting") {
    const lineStart = lineStartOffset(text, region.start);
    return text.slice(lineStart, region.start).match(/^[\t ]*/)?.[0] ?? "";
  }
  const htmlIndentSize = formatIndentSizeForLanguage(options, "html");
  return indentUnitForLanguage(options, "html").repeat(
    Math.floor(leadingIndentWidth(text, region.start, options) / htmlIndentSize),
  );
}

function indentLines(text: string, indent: string): string {
  return text
    .split("\n")
    .map((line) => (line.length === 0 ? "" : `${indent}${line}`))
    .join("\n");
}

function leadingIndentWidth(text: string, offset: number, options: AspFormattingOptions): number {
  const lineStart = lineStartOffset(text, offset);
  const indent = text.slice(lineStart, offset).match(/^[\t ]*/)?.[0] ?? "";
  return [...indent].reduce((width, char) => width + (char === "\t" ? options.tabSize : 1), 0);
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
  const ownerFile = uriToFileName(virtualSourceUri(virtual));
  const configEntry = readJsProjectConfigEntry(ownerFile, settings, optionOverrides);
  const cacheKey = jsLanguageServiceCacheKey(configEntry.key);
  const cached = jsLanguageServiceCache.get(cacheKey);
  if (cached) {
    const openFiles = await measureDebugStepAsync(
      settings,
      virtualSourceUri(virtual),
      "javascript.languageService.collectOpenFiles",
      () => collectOpenJsProjectFilesAsync(virtual, settings),
    );
    measureDebugStep(
      settings,
      virtualSourceUri(virtual),
      "javascript.languageService.updateOpenFiles",
      () => updateJsLanguageServiceOpenFiles(cached.project, openFiles),
    );
    cached.lastUsed = ++jsLanguageServiceCacheTick;
    logDebugSummary(
      settings,
      `[asp-lsp] javascript.languageService.reuse: ${virtualSourceUri(virtual)}, files=${cached.project.files.size}`,
    );
    return cached.project;
  }
  const collected = await collectJsProjectFilesAsync(
    virtual,
    settings,
    optionOverrides,
    configEntry.config,
  );
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

function jsLanguageServiceCacheKey(projectConfigKey: string): string {
  return JSON.stringify({
    projectConfig: projectConfigKey,
    projectGeneration: jsProjectGeneration,
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
  const filesChanged = !jsProjectFilesEqual(project.files, collected.files);
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
  if (filesChanged || resolutionShapeChanged) {
    project.projectVersion += 1;
  }
}

function updateJsLanguageServiceOpenFiles(
  project: Pick<JsLanguageServiceProject, "files" | "projectVersion">,
  openFiles: Map<string, JsProjectFile>,
): void {
  let changed = false;
  const openFileNames = new Set(openFiles.keys());
  for (const [fileName, file] of project.files) {
    if (file.virtual && !openFileNames.has(fileName)) {
      project.files.delete(fileName);
      changed = true;
    }
  }
  for (const [fileName, file] of openFiles) {
    if (!jsProjectFileEqual(project.files.get(fileName), file)) {
      project.files.set(fileName, file);
      changed = true;
    }
  }
  if (changed) {
    project.projectVersion += 1;
  }
}

function jsProjectFilesEqual(
  left: Map<string, JsProjectFile>,
  right: Map<string, JsProjectFile>,
): boolean {
  if (left.size !== right.size) {
    return false;
  }
  for (const [fileName, file] of left) {
    if (!jsProjectFileEqual(file, right.get(fileName))) {
      return false;
    }
  }
  return true;
}

function jsProjectFileEqual(
  left: JsProjectFile | undefined,
  right: JsProjectFile | undefined,
): boolean {
  return (
    left !== undefined &&
    right !== undefined &&
    left.fileName === right.fileName &&
    left.version === right.version
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
    jsLanguageServiceCache.delete(oldest[0]);
  }
  checkMemoryPressure(globalSettings, "js.languageService.prune");
}

function clearJsLanguageServiceCache(): void {
  jsLanguageServiceCache.clear();
}

function clearJsProjectCaches(): void {
  clearJsLanguageServiceCache();
  jsOpenProjectFilesCache.clear();
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
  clearDiskAnalysisSettingsKeyCache();
  clearWorkspaceDiagnosticsCaches();
  vbCanonicalContextSymbolsCache.clear();
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

function invalidateIncludeResolutionForAspChanges(
  changes: WatchedAspFileChange[],
  reason: string,
): void {
  if (changes.length === 0) {
    return;
  }
  let removedIncludeResolutions = 0;
  let removedPathResolutions = 0;
  for (const [key, resolution] of includePathResolutionCache) {
    if (includeResolutionShouldDrop(resolution, changes)) {
      includePathResolutionCache.delete(key);
      removedIncludeResolutions += 1;
    }
  }
  for (const [key, resolution] of pathResolutionCache) {
    if (includeResolutionShouldDrop(resolution, changes)) {
      pathResolutionCache.delete(key);
      removedPathResolutions += 1;
    }
  }
  includeCycleCache.clear();
  clearDiskAnalysisSettingsKeyCache();
  completionSessionCache.clear("includeResolution.partial");
  logDebugSummary(
    globalSettings,
    `[asp-lsp] invalidation.includeResolution.partial: ${reason}, includeResolutions=${removedIncludeResolutions}, pathResolutions=${removedPathResolutions}`,
  );
}

function includeResolutionShouldDrop(
  resolution: PathResolution,
  changes: WatchedAspFileChange[],
): boolean {
  for (const change of changes) {
    const fileName = normalizeFileName(change.fileName);
    if (
      sameFile(resolution.fileName, fileName) ||
      (resolution.actualPath !== undefined && sameFile(resolution.actualPath, fileName))
    ) {
      return true;
    }
    if (
      change.type === FileChangeType.Created &&
      (!resolution.exists || sameFile(path.dirname(resolution.fileName), path.dirname(fileName)))
    ) {
      return true;
    }
  }
  return false;
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
  const cached = serverTextFingerprintCache.get(text);
  if (cached !== undefined) {
    serverTextFingerprintCache.delete(text);
    serverTextFingerprintCache.set(text, cached);
    return cached;
  }
  const fingerprint = computeTextFingerprint(text);
  serverTextFingerprintCache.set(text, fingerprint);
  while (serverTextFingerprintCache.size > serverTextFingerprintCacheMaxEntries) {
    const oldest = serverTextFingerprintCache.keys().next().value;
    if (oldest === undefined) {
      break;
    }
    serverTextFingerprintCache.delete(oldest);
  }
  checkMemoryPressure(globalSettings, "server.textFingerprint.prune");
  return fingerprint;
}

function computeTextFingerprint(text: string): string {
  let hash = 2166136261;
  for (let index = 0; index < text.length; index += 1) {
    hash ^= text.charCodeAt(index);
    hash = Math.imul(hash, 16777619);
  }
  return `${text.length}:${hash >>> 0}`;
}

function jsScriptSnapshotForVirtual(
  fileName: string,
  virtual: VirtualDocument,
  settings: AspSettings,
): AspJsScriptSnapshot {
  const sourceUri = virtualSourceUri(virtual);
  const version = jsVirtualDocumentVersion(virtual);
  const previous = jsScriptSnapshots.get(fileName);
  if (previous?.version === version) {
    logDebugSummary(
      settings,
      `[asp-lsp] js.snapshot.changeRange.reuse: ${sourceUri}, version=${version}`,
    );
    return previous;
  }
  const trackChangeRange = virtual.text.length <= maxJsSnapshotChangeRangeScanLength;
  const change =
    previous && trackChangeRange && previous.segments.length > 0
      ? jsVirtualTextChangeRange(previous, virtual)
      : undefined;
  const segments = trackChangeRange
    ? virtual.sourceMap.segments.map((segment) => ({
        virtualStart: segment.virtualStart,
        virtualEnd: segment.virtualEnd,
      }))
    : [];
  const snapshot = new AspJsScriptSnapshot(
    fileName,
    virtual.text,
    version,
    ++jsScriptSnapshotSequence,
    segments,
    previous,
    change,
  );
  trimJsScriptSnapshotHistory(snapshot);
  jsScriptSnapshots.set(fileName, snapshot);
  logDebugSummary(
    settings,
    change
      ? `[asp-lsp] js.snapshot.changeRange.hit: ${sourceUri}, oldLength=${change.span.length}, newLength=${change.newLength}`
      : `[asp-lsp] js.snapshot.changeRange.miss: ${sourceUri}, reason=${
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
  config: JsProjectConfig = readJsProjectConfig(
    uriToFileName(virtualSourceUri(activeVirtual)),
    settings,
    optionOverrides,
  ),
): Promise<JsProjectConfig & { files: Map<string, JsProjectFile> }> {
  const files = await collectOpenJsProjectFilesAsync(activeVirtual, settings);
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
  const cacheKey = jsOpenProjectFilesCacheKey(activeVirtual, settings);
  const cached = jsOpenProjectFilesCache.get(cacheKey);
  if (cached) {
    cached.lastUsed = ++jsLanguageServiceCacheTick;
    logDebugSummary(
      settings,
      `[asp-lsp] javascript.openProjectFiles.reuse: ${virtualSourceUri(activeVirtual)}, files=${cached.files.size}`,
    );
    return new Map(cached.files);
  }
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
  jsOpenProjectFilesCache.set(cacheKey, {
    files: new Map(files),
    lastUsed: ++jsLanguageServiceCacheTick,
  });
  logDebugSummary(
    settings,
    `[asp-lsp] javascript.openProjectFiles.collect: ${virtualSourceUri(activeVirtual)}, files=${files.size}`,
  );
  pruneJsOpenProjectFilesCache();
  return files;
}

function jsOpenProjectFilesCacheKey(activeVirtual: VirtualDocument, settings: AspSettings): string {
  return JSON.stringify({
    generation: jsProjectGeneration,
    settings: jsProjectSettingsIdentity(settings),
    activeVirtual: jsDiagnosticsVirtualKey(activeVirtual),
    openDocuments: documents
      .all()
      .map((document) => ({
        uri: document.uri,
        version: document.version,
        languageId: document.languageId,
        parse: parseSettingsIdentity(cachedSettings(document.uri)),
      }))
      .sort((left, right) => left.uri.localeCompare(right.uri)),
  });
}

function pruneJsOpenProjectFilesCache(): void {
  while (jsOpenProjectFilesCache.size > maxJsOpenProjectFilesCacheEntries) {
    const oldest = [...jsOpenProjectFilesCache.entries()].sort(
      (left, right) => left[1].lastUsed - right[1].lastUsed,
    )[0];
    if (!oldest) {
      return;
    }
    jsOpenProjectFilesCache.delete(oldest[0]);
  }
}

function addJsProjectVirtualFile(
  files: Map<string, JsProjectFile>,
  virtual: VirtualDocument,
  settings: AspSettings,
): void {
  const sourceUri = virtualSourceUri(virtual);
  const fileName = normalizeFileName(jsVirtualFileName(virtual.uri));
  const snapshot = jsScriptSnapshotForVirtual(fileName, virtual, settings);
  files.set(fileName, {
    fileName,
    text: virtual.text,
    version: snapshot.version,
    uri: sourceUri,
    virtual,
    snapshot,
  });
}

function jsVirtualDocumentVersion(virtual: VirtualDocument): string {
  const sourceUri = virtualSourceUri(virtual);
  const open = openDocumentForUri(sourceUri);
  if (open) {
    return JSON.stringify({
      language: virtual.languageId,
      sourceUri: fileIdentityKeyFromUri(sourceUri),
      sourceVersion: open.version,
    });
  }
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
  return readJsProjectConfigEntry(ownerFile, settings, optionOverrides).config;
}

function readJsProjectConfigEntry(
  ownerFile: string,
  settings: AspSettings,
  optionOverrides: Partial<ts.CompilerOptions> = {},
): { key: string; config: JsProjectConfig } {
  const ownerDirectory = path.dirname(ownerFile);
  const configPath =
    settings.javascript?.ignoreProjectConfig === true
      ? undefined
      : (ts.findConfigFile(ownerDirectory, cachedTsFileExists, "tsconfig.json") ??
        ts.findConfigFile(ownerDirectory, cachedTsFileExists, "jsconfig.json"));
  const currentDirectory = configPath
    ? path.dirname(configPath)
    : defaultJsProjectCurrentDirectory(ownerDirectory);
  const cacheKey = jsProjectConfigCacheKey(configPath, currentDirectory, settings, optionOverrides);
  const cached = jsProjectConfigCache.get(cacheKey);
  if (cached) {
    cached.lastUsed = ++jsLanguageServiceCacheTick;
    return { key: cacheKey, config: cached.config };
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
  return { key: cacheKey, config: result };
}

function defaultJsProjectCurrentDirectory(ownerDirectory: string): string {
  const roots = workspaceRoots.length > 0 ? workspaceRoots : [ownerDirectory];
  return roots[0] ?? ownerDirectory;
}

function jsProjectConfigCacheKey(
  configPath: string | undefined,
  currentDirectory: string,
  settings: AspSettings,
  optionOverrides: Partial<ts.CompilerOptions>,
): string {
  const environmentFiles = [configPath, nearestPackageJson(currentDirectory)]
    .filter((fileName): fileName is string => Boolean(fileName))
    .map((fileName) => {
      const stat = cachedJsFileStat(fileName);
      return stat ? `${normalizeFileName(fileName)}:${stat.mtimeMs}:${stat.size}` : fileName;
    });
  return JSON.stringify({
    currentDirectory: normalizeFileName(currentDirectory),
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
  checkMemoryPressure(globalSettings, "js.projectConfig.prune");
}

function browserJavaScriptCompilerOptions(
  options: ts.CompilerOptions,
  currentDirectory: string,
  settings: AspSettings,
  optionOverrides: Partial<ts.CompilerOptions>,
): ts.CompilerOptions {
  const settingsOptions = javascriptCompilerOptionsFromSettings(settings, currentDirectory);
  const next: ts.CompilerOptions = {
    ...options,
    ...settingsOptions,
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

function javascriptCompilerOptionsFromSettings(
  settings: AspSettings,
  currentDirectory: string,
): ts.CompilerOptions {
  const compilerOptions = settings.javascript?.compilerOptions;
  if (!compilerOptions || Array.isArray(compilerOptions)) {
    return {};
  }
  return ts.convertCompilerOptionsFromJson(compilerOptions, currentDirectory).options;
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
  const stat = await fsGateway.statAsync(fileName);
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
  if (resolveNetworkProfile(settings).caseResolution === "fast") {
    const stat = await fsGateway.statAsync(fileName);
    if (stat) {
      return {
        fileName,
        exists: true,
        pathCaseMatches: true,
        actualPath: fileName,
      };
    }
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
    const listing = await fsGateway.readdirAsync(current);
    if (!listing) {
      return { fileName, exists: false, pathCaseMatches };
    }
    const exact = listing.entries.find((entry) => entry.name === segment);
    if (exact) {
      current = path.join(current, exact.name);
      continue;
    }
    const lower = segment.toLowerCase();
    const insensitive = listing.byLowerName.get(lower) ?? [];
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
    baseDirectory: fileIdentityKeyFromFileName(baseDirectory),
    requestedPath,
    windowsPathResolution: settings.windowsPathResolution !== false,
    caseResolution: resolveNetworkProfile(settings).caseResolution,
  });
}

function includeResolutionSettingsKey(settings: AspSettings): unknown {
  return {
    virtualRoot: settings.virtualRoot,
    virtualRoots: settings.virtualRoots?.map(fileIdentityKeyFromFileName),
    includePaths: settings.includePaths?.map(fileIdentityKeyFromFileName),
    windowsPathResolution: settings.windowsPathResolution !== false,
    caseResolution: resolveNetworkProfile(settings).caseResolution,
    legacyEncoding: settings.legacyEncoding,
    roots: workspaceRoots.map(fileIdentityKeyFromFileName).sort(),
  };
}

function workspaceRootFromUri(uri: string): string {
  const fileName = uriToFileName(uri);
  const normalized = normalizeFileName(fileName);
  const fileKey = fileIdentityKeyFromFileName(normalized);
  const root = workspaceRoots.find((candidate) => {
    const normalizedRoot = normalizeFileName(candidate);
    const rootKey = fileIdentityKeyFromFileName(normalizedRoot);
    return fileIdentityKeyIsWithinOrEqual(fileKey, rootKey);
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
  const fileKey = fileIdentityKeyFromFileName(normalized);
  const root = workspaceRoots
    .map(normalizeFileName)
    .filter((candidate) => {
      const candidateKey = fileIdentityKeyFromFileName(candidate);
      return fileIdentityKeyIsWithinOrEqual(fileKey, candidateKey);
    })
    .sort((left, right) => right.length - left.length)[0];
  const label = root ? path.relative(root, fileName) || path.basename(fileName) : fileName;
  return `[${escapeMarkdownLinkText(label + fragment)}](${pathToFileUri(fileName)}${fragment})`;
}

function escapeMarkdownLinkText(text: string): string {
  return text.replace(/[\\[\]]/g, "\\$&");
}

function uriToFileName(uri: string): string {
  const fileName = uri.startsWith("file://") ? fileURLToPath(uri) : uri;
  return fileName.replace(/\.(html|css|javascript|vbscript|jscript)\.virtual$/, "");
}

function isStandaloneVbscriptSource(uri: string, languageId?: string): boolean {
  return languageId === "vbscript" || /\.vbs$/i.test(uriToFileName(uri));
}

function languageIdForUri(uri: string): "classic-asp" | "vbscript" {
  return isStandaloneVbscriptSource(uri) ? "vbscript" : "classic-asp";
}

function parseSourceDocument(
  uri: string,
  text: string,
  settings: AspSettings,
  languageId?: string,
): AspParsedDocument {
  return isStandaloneVbscriptSource(uri, languageId)
    ? parseVbscriptDocument(uri, text)
    : parseAspDocument(uri, text, settings);
}

async function parseSourceDocumentAsync(
  uri: string,
  text: string,
  settings: AspSettings,
  languageId?: string,
): Promise<AspParsedDocument> {
  return isStandaloneVbscriptSource(uri, languageId)
    ? parseVbscriptDocumentAsync(uri, text)
    : parseAspDocumentAsync(uri, text, settings);
}

async function parseSourceDocumentSkeletonAsync(
  uri: string,
  text: string,
  settings: AspSettings,
  languageId?: string,
): Promise<AspParsedDocument> {
  return isStandaloneVbscriptSource(uri, languageId)
    ? parseVbscriptDocumentAsync(uri, text)
    : parseAspDocumentSkeletonAsync(uri, text, settings);
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

function openDocumentForFileName(fileName: string): TextDocument | undefined {
  const uri = pathToFileUri(fileName);
  return (
    documents.get(uri) ??
    documents
      .all()
      .find(
        (document) =>
          fileIdentityKeyFromUri(document.uri) === fileIdentityKeyFromFileName(fileName),
      )
  );
}

function openDocumentForUri(uri: string): TextDocument | undefined {
  return (
    documents.get(uri) ??
    (uri.startsWith("file://")
      ? documents
          .all()
          .find((document) => fileIdentityKeyFromUri(document.uri) === fileIdentityKeyFromUri(uri))
      : undefined)
  );
}

function graphFileNameFromUri(uri: string): string {
  return normalizeFileName(uriToFileName(uri));
}

function graphFileKey(fileName: string): string {
  return fileIdentityKeyFromFileName(fileName);
}

function graphFileKeyFromUri(uri: string): string {
  return graphFileKey(graphFileNameFromUri(uri));
}

function fileIdentityKeyIsWithinOrEqual(fileKey: string, directoryKey: string): boolean {
  if (fileKey === directoryKey) {
    return true;
  }
  const prefix = directoryKey.endsWith("/") ? directoryKey : `${directoryKey}/`;
  return fileKey.startsWith(prefix);
}

function sameFile(left: string, right: string): boolean {
  return fileIdentityKeyFromFileName(left) === fileIdentityKeyFromFileName(right);
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

function vbBuiltinRuntimeForCached(
  cached: CachedDocument,
): NonNullable<VbProjectContext["builtinRuntime"]> {
  return isStandaloneVbscriptSource(cached.source.uri, cached.source.languageId)
    ? "windowsScriptHost"
    : "classicAsp";
}

function vbBuiltinRuntimeForUri(
  uri: string | undefined,
): NonNullable<VbProjectContext["builtinRuntime"]> {
  return uri && isStandaloneVbscriptSource(uri) ? "windowsScriptHost" : "classicAsp";
}

function withCachedVbBuiltinRuntime(
  cached: CachedDocument,
  context: VbProjectContext,
): VbProjectContext {
  return {
    ...context,
    builtinRuntime: context.builtinRuntime ?? vbBuiltinRuntimeForCached(cached),
  };
}

function withUriVbBuiltinRuntime(uri: string, context: VbProjectContext): VbProjectContext {
  return {
    ...context,
    builtinRuntime: vbBuiltinRuntimeForUri(uri),
  };
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
  const settings = cachedSettings(cached.source.uri);
  const context =
    renameSymbolScope(settings) === "workspace"
      ? await buildFullVbProjectContextForWorkspaceOperationAsync(cached, settings)
      : await localVbReferenceContextAsync(cached, settings);
  const symbol = context.symbols?.find(
    (candidate) =>
      sameFileIdentityUri(candidate.sourceUri, cached.source.uri) &&
      sameRange(candidate.range, diagnostic.range),
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
    sameFileIdentityUri(left.sourceUri, right.sourceUri) &&
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
      sameFileIdentityUri(left.sourceUri, right.sourceUri) &&
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
      sameFileIdentityUri(candidate.sourceUri, cached.source.uri) &&
      sameRange(candidate.range, diagnostic.range),
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

const htmlExtractInlineStyleKind = `${CodeActionKind.Refactor}.extract`;

interface HtmlAttributeSpan {
  name: string;
  value: string | true;
  start: number;
  end: number;
  nameStart: number;
  nameEnd: number;
  valueStart: number;
  valueEnd: number;
  quote?: '"' | "'";
}

interface HtmlStartTagSpan {
  name: string;
  start: number;
  end: number;
  nameEnd: number;
  attributesEnd: number;
  attributes: HtmlAttributeSpan[];
  closing: boolean;
}

interface InlineStyleExtractionTarget {
  tag: HtmlStartTagSpan;
  styleAttribute: HtmlAttributeSpan;
  styleText: string;
}

type InlineStyleExtractionMode = "class" | "id";

function htmlInlineStyleCodeActions(
  cached: CachedDocument,
  range: Range,
  context: CodeActionContext,
): CodeAction[] {
  if (!codeActionAllows(context, htmlExtractInlineStyleKind)) {
    return [];
  }
  const target = findInlineStyleExtractionTarget(cached, range);
  if (!target) {
    return [];
  }
  return [
    inlineStyleExtractionAction(cached, target, "class"),
    inlineStyleExtractionAction(cached, target, "id"),
  ].filter((action): action is CodeAction => Boolean(action));
}

function inlineStyleExtractionAction(
  cached: CachedDocument,
  target: InlineStyleExtractionTarget,
  mode: InlineStyleExtractionMode,
): CodeAction | undefined {
  const edit = inlineStyleExtractionEdit(cached, target, mode);
  if (!edit) {
    return undefined;
  }
  return {
    title: localizerForUri(cached.source.uri).t(
      mode === "class"
        ? "server.refactor.extractInlineStyleToClass"
        : "server.refactor.extractInlineStyleToId",
    ),
    kind: htmlExtractInlineStyleKind,
    edit,
  };
}

function findInlineStyleExtractionTarget(
  cached: CachedDocument,
  range: Range,
): InlineStyleExtractionTarget | undefined {
  const text = cached.source.getText();
  const rangeStart = cached.source.offsetAt(range.start);
  const rangeEnd = cached.source.offsetAt(range.end);
  for (const region of cached.parsed.regions) {
    if (region.kind !== "style-attribute") {
      continue;
    }
    const tagStart = htmlStartTagOffsetForAttribute(text, region.contentStart);
    const tag = tagStart === undefined ? undefined : readHtmlStartTagAt(text, tagStart);
    if (!tag || tag.closing || !rangeTouchesHtmlStartTag(rangeStart, rangeEnd, tag)) {
      continue;
    }
    const styleAttribute = tag.attributes.find(
      (attribute) =>
        attribute.name.toLowerCase() === "style" &&
        attribute.value !== true &&
        attribute.valueStart === region.contentStart &&
        attribute.valueEnd === region.contentEnd,
    );
    if (!styleAttribute || styleAttribute.value === true) {
      continue;
    }
    const styleText = text.slice(styleAttribute.valueStart, styleAttribute.valueEnd).trim();
    if (!styleText || styleText.includes("<%") || styleText.includes("%>")) {
      continue;
    }
    return { tag, styleAttribute, styleText };
  }
  return undefined;
}

function rangeTouchesHtmlStartTag(
  rangeStart: number,
  rangeEnd: number,
  tag: HtmlStartTagSpan,
): boolean {
  return rangeStart === rangeEnd
    ? rangeStart >= tag.start && rangeStart < tag.end
    : rangeStart < tag.end && rangeEnd > tag.start;
}

function inlineStyleExtractionEdit(
  cached: CachedDocument,
  target: InlineStyleExtractionTarget,
  mode: InlineStyleExtractionMode,
): WorkspaceEdit | undefined {
  const text = cached.source.getText();
  const tag = target.tag;
  const classAttribute = htmlAttributeByName(tag, "class");
  const idAttribute = htmlAttributeByName(tag, "id");
  const selectorName =
    mode === "class"
      ? nextAvailableHtmlName(text, "class")
      : idAttribute && idAttribute.value !== true && idAttribute.value.trim()
        ? idAttribute.value.trim()
        : nextAvailableHtmlName(text, "id");
  if (!selectorName) {
    return undefined;
  }
  if (
    (mode === "class" && classAttribute?.value === true) ||
    (mode === "id" && idAttribute?.value === true)
  ) {
    return undefined;
  }
  const selector = mode === "class" ? `.${selectorName}` : `#${cssEscapeIdentifier(selectorName)}`;
  const edits: TextEdit[] = [
    styleRuleInsertionEdit(cached, target, selector),
    removeStyleAttributeEdit(cached, target),
  ];
  const nameEdit =
    mode === "class"
      ? classAttribute
        ? appendAttributeTokenEdit(cached, classAttribute, selectorName)
        : insertAttributeEdit(cached, tag, "class", selectorName)
      : idAttribute
        ? undefined
        : insertAttributeEdit(cached, tag, "id", selectorName);
  if (nameEdit) {
    edits.push(nameEdit);
  }
  return { changes: { [cached.source.uri]: edits } };
}

function styleRuleInsertionEdit(
  cached: CachedDocument,
  target: InlineStyleExtractionTarget,
  selector: string,
): TextEdit {
  const text = cached.source.getText();
  const existingStyle =
    cachedSettings(cached.source.uri).styleExtraction?.insertionMode === "reuseExistingStyleTag"
      ? nearestStyleElement(cached.parsed.regions, target.tag.start)
      : undefined;
  if (existingStyle) {
    const newline = documentNewline(text);
    return {
      range: {
        start: cached.source.positionAt(existingStyle.contentEnd),
        end: cached.source.positionAt(existingStyle.contentEnd),
      },
      newText: styleRuleAppendText(text, existingStyle, selector, target.styleText, newline),
    };
  }
  const insertOffset = lineStartOffset(text, target.tag.start);
  return {
    range: {
      start: cached.source.positionAt(insertOffset),
      end: cached.source.positionAt(insertOffset),
    },
    newText: standaloneStyleElementText(text, target.tag.start, selector, target.styleText),
  };
}

function nearestStyleElement(
  regions: readonly AspRegion[],
  targetOffset: number,
): AspRegion | undefined {
  return regions
    .filter((region) => region.kind === "style")
    .sort((left, right) => {
      const leftDistance = styleElementDistance(left, targetOffset);
      const rightDistance = styleElementDistance(right, targetOffset);
      return (
        leftDistance - rightDistance ||
        Number(right.start < targetOffset) - Number(left.start < targetOffset) ||
        left.start - right.start
      );
    })[0];
}

function styleElementDistance(region: AspRegion, targetOffset: number): number {
  if (targetOffset < region.start) {
    return region.start - targetOffset;
  }
  if (targetOffset > region.end) {
    return targetOffset - region.end;
  }
  return 0;
}

function styleRuleAppendText(
  text: string,
  styleRegion: AspRegion,
  selector: string,
  styleText: string,
  newline: string,
): string {
  const rule = styleRuleText(selector, styleText, "  ", "    ", newline);
  const existingContent = text.slice(styleRegion.contentStart, styleRegion.contentEnd);
  const prefix =
    existingContent.trim().length > 0 && !existingContent.endsWith("\n") ? newline : "";
  return `${prefix}${rule}`;
}

function standaloneStyleElementText(
  text: string,
  tagStart: number,
  selector: string,
  styleText: string,
): string {
  const newline = documentNewline(text);
  const indent = text.slice(lineStartOffset(text, tagStart), tagStart).match(/^[\t ]*/)?.[0] ?? "";
  const rule = styleRuleText(selector, styleText, `${indent}  `, `${indent}    `, newline);
  return `${indent}<style>${newline}${rule}${indent}</style>${newline}`;
}

function styleRuleText(
  selector: string,
  styleText: string,
  outerIndent: string,
  innerIndent: string,
  newline: string,
): string {
  return [
    `${outerIndent}${selector} {`,
    ...inlineStyleDeclarationLines(styleText).map((declaration) => `${innerIndent}${declaration}`),
    `${outerIndent}}`,
    "",
  ].join(newline);
}

function inlineStyleDeclarationLines(styleText: string): string[] {
  return splitInlineStyleDeclarations(styleText).map((declaration) =>
    normalizeInlineStyleDeclaration(declaration),
  );
}

function splitInlineStyleDeclarations(styleText: string): string[] {
  const declarations: string[] = [];
  let start = 0;
  let quote: '"' | "'" | undefined;
  let parenDepth = 0;
  for (let index = 0; index < styleText.length; index++) {
    const char = styleText[index];
    if (quote) {
      if (char === "\\") {
        index++;
      } else if (char === quote) {
        quote = undefined;
      }
      continue;
    }
    if (char === '"' || char === "'") {
      quote = char;
      continue;
    }
    if (char === "(") {
      parenDepth++;
      continue;
    }
    if (char === ")") {
      parenDepth = Math.max(0, parenDepth - 1);
      continue;
    }
    if (char === ";" && parenDepth === 0) {
      const declaration = styleText.slice(start, index).trim();
      if (declaration) {
        declarations.push(declaration);
      }
      start = index + 1;
    }
  }
  const lastDeclaration = styleText.slice(start).trim();
  if (lastDeclaration) {
    declarations.push(lastDeclaration);
  }
  return declarations.length > 0 ? declarations : [styleText.trim()];
}

function normalizeInlineStyleDeclaration(declaration: string): string {
  const trimmed = declaration.trim();
  const colonIndex = trimmed.indexOf(":");
  const normalized =
    colonIndex < 0
      ? trimmed
      : `${trimmed.slice(0, colonIndex).trimEnd()}: ${trimmed.slice(colonIndex + 1).trimStart()}`;
  return /;\s*$/.test(normalized) ? normalized : `${normalized};`;
}

function removeStyleAttributeEdit(
  cached: CachedDocument,
  target: InlineStyleExtractionTarget,
): TextEdit {
  const text = cached.source.getText();
  const range = htmlAttributeRemovalOffsets(text, target.tag, target.styleAttribute);
  return {
    range: {
      start: cached.source.positionAt(range.start),
      end: cached.source.positionAt(range.end),
    },
    newText: "",
  };
}

function appendAttributeTokenEdit(
  cached: CachedDocument,
  attribute: HtmlAttributeSpan,
  token: string,
): TextEdit | undefined {
  if (attribute.value === true) {
    return undefined;
  }
  const value = attribute.value;
  const newValue = value.trim().length > 0 ? `${value} ${token}` : token;
  const newText = attribute.quote ? newValue : `"${newValue}"`;
  return {
    range: {
      start: cached.source.positionAt(attribute.valueStart),
      end: cached.source.positionAt(attribute.valueEnd),
    },
    newText,
  };
}

function insertAttributeEdit(
  cached: CachedDocument,
  tag: HtmlStartTagSpan,
  name: "class" | "id",
  value: string,
): TextEdit {
  const text = cached.source.getText();
  const offset = htmlAttributeInsertionOffset(text, tag);
  return {
    range: {
      start: cached.source.positionAt(offset),
      end: cached.source.positionAt(offset),
    },
    newText: ` ${name}="${value}"`,
  };
}

function htmlAttributeRemovalOffsets(
  text: string,
  tag: HtmlStartTagSpan,
  attribute: HtmlAttributeSpan,
): { start: number; end: number } {
  let start = attribute.start;
  while (start > tag.nameEnd && isHtmlWhitespaceCode(text.charCodeAt(start - 1))) {
    start -= 1;
  }
  let end = attribute.end;
  if (start === attribute.start) {
    while (end < tag.attributesEnd && isHtmlWhitespaceCode(text.charCodeAt(end))) {
      end += 1;
    }
  }
  return { start, end };
}

function htmlAttributeInsertionOffset(text: string, tag: HtmlStartTagSpan): number {
  let offset = tag.end - 1;
  while (offset > tag.nameEnd && isHtmlWhitespaceCode(text.charCodeAt(offset - 1))) {
    offset -= 1;
  }
  if (text[offset - 1] === "/") {
    offset -= 1;
  }
  return offset;
}

function nextAvailableHtmlName(text: string, attributeName: "class" | "id"): string {
  const used = usedHtmlAttributeNames(text, attributeName);
  for (let index = 1; index < 1000; index += 1) {
    const name = `style-${index}`;
    if (!used.has(name.toLowerCase())) {
      return name;
    }
  }
  return "style-1";
}

function usedHtmlAttributeNames(text: string, attributeName: "class" | "id"): Set<string> {
  const used = new Set<string>();
  for (const tag of htmlStartTags(text)) {
    const attribute = htmlAttributeByName(tag, attributeName);
    if (!attribute || attribute.value === true) {
      continue;
    }
    const values =
      attributeName === "class" ? attribute.value.split(/\s+/).filter(Boolean) : [attribute.value];
    for (const value of values) {
      used.add(value.toLowerCase());
    }
  }
  return used;
}

function htmlAttributeByName(
  tag: HtmlStartTagSpan,
  name: "class" | "id" | "style",
): HtmlAttributeSpan | undefined {
  return tag.attributes.find((attribute) => attribute.name.toLowerCase() === name);
}

function* htmlStartTags(text: string): Iterable<HtmlStartTagSpan> {
  let offset = 0;
  while (offset < text.length) {
    const start = text.indexOf("<", offset);
    if (start === -1) {
      return;
    }
    const tag = readHtmlStartTagAt(text, start);
    if (tag && !tag.closing) {
      yield tag;
      offset = tag.end;
    } else {
      offset = start + 1;
    }
  }
}

function htmlStartTagOffsetForAttribute(text: string, attributeOffset: number): number | undefined {
  const start = text.lastIndexOf("<", attributeOffset);
  const end = text.lastIndexOf(">", attributeOffset);
  return start > end ? start : undefined;
}

function readHtmlStartTagAt(text: string, start: number): HtmlStartTagSpan | undefined {
  if (text[start] !== "<" || text.startsWith("<!--", start) || text[start + 1] === "%") {
    return undefined;
  }
  let cursor = start + 1;
  const closing = text[cursor] === "/";
  if (closing) {
    cursor += 1;
  }
  while (cursor < text.length && isHtmlWhitespaceCode(text.charCodeAt(cursor))) {
    cursor += 1;
  }
  const nameStart = cursor;
  if (!isAsciiAlphaCode(text.charCodeAt(cursor))) {
    return undefined;
  }
  cursor += 1;
  while (cursor < text.length && isHtmlTagNamePartCode(text.charCodeAt(cursor))) {
    cursor += 1;
  }
  const name = text.slice(nameStart, cursor).toLowerCase();
  const tagEnd = findHtmlTagEnd(text, cursor);
  if (tagEnd === -1) {
    return undefined;
  }
  return {
    name,
    start,
    end: tagEnd + 1,
    nameEnd: cursor,
    attributesEnd: tagEnd,
    attributes: parseHtmlAttributeSpans(text, cursor, tagEnd),
    closing,
  };
}

function findHtmlTagEnd(text: string, offset: number): number {
  let quote: string | undefined;
  for (let index = offset; index < text.length; index += 1) {
    const char = text[index];
    if (quote) {
      if (char === quote) {
        quote = undefined;
      }
      continue;
    }
    if (char === '"' || char === "'") {
      quote = char;
      continue;
    }
    if (text.startsWith("<%", index)) {
      const close = text.indexOf("%>", index + 2);
      if (close === -1) {
        return -1;
      }
      index = close + 1;
      continue;
    }
    if (char === ">") {
      return index;
    }
  }
  return -1;
}

function parseHtmlAttributeSpans(text: string, start: number, end: number): HtmlAttributeSpan[] {
  const attributes: HtmlAttributeSpan[] = [];
  let cursor = start;
  while (cursor < end) {
    while (cursor < end) {
      const code = text.charCodeAt(cursor);
      if (code !== 47 && !isHtmlWhitespaceCode(code)) {
        break;
      }
      cursor += 1;
    }
    if (text.startsWith("<%", cursor)) {
      const close = text.indexOf("%>", cursor + 2);
      cursor = close === -1 ? end : close + 2;
      continue;
    }
    const nameStart = cursor;
    if (!isAttributeNameStartCode(text.charCodeAt(cursor))) {
      cursor += 1;
      continue;
    }
    cursor += 1;
    while (cursor < end && isAttributeNamePartCode(text.charCodeAt(cursor))) {
      cursor += 1;
    }
    const nameEnd = cursor;
    const name = text.slice(nameStart, nameEnd);
    while (cursor < end && isHtmlWhitespaceCode(text.charCodeAt(cursor))) {
      cursor += 1;
    }
    if (text[cursor] !== "=") {
      attributes.push({
        name,
        value: true,
        start: nameStart,
        end: cursor,
        nameStart,
        nameEnd,
        valueStart: cursor,
        valueEnd: cursor,
      });
      continue;
    }
    cursor += 1;
    while (cursor < end && isHtmlWhitespaceCode(text.charCodeAt(cursor))) {
      cursor += 1;
    }
    const quote: '"' | "'" | undefined =
      text[cursor] === '"' || text[cursor] === "'" ? (text[cursor] as '"' | "'") : undefined;
    const valueStart = quote ? cursor + 1 : cursor;
    if (quote) {
      cursor += 1;
      while (cursor < end && text[cursor] !== quote) {
        cursor += 1;
      }
      const valueEnd = cursor;
      if (cursor < end) {
        cursor += 1;
      }
      attributes.push({
        name,
        value: text.slice(valueStart, valueEnd),
        start: nameStart,
        end: cursor,
        nameStart,
        nameEnd,
        valueStart,
        valueEnd,
        quote,
      });
      continue;
    }
    while (cursor < end) {
      const code = text.charCodeAt(cursor);
      if (code === 62 || isHtmlWhitespaceCode(code)) {
        break;
      }
      cursor += 1;
    }
    attributes.push({
      name,
      value: text.slice(valueStart, cursor),
      start: nameStart,
      end: cursor,
      nameStart,
      nameEnd,
      valueStart,
      valueEnd: cursor,
    });
  }
  return attributes;
}

function isHtmlWhitespaceCode(code: number): boolean {
  return (
    code === 9 ||
    code === 10 ||
    code === 11 ||
    code === 12 ||
    code === 13 ||
    code === 32 ||
    code === 160 ||
    code === 5760 ||
    (code >= 8192 && code <= 8202) ||
    code === 8232 ||
    code === 8233 ||
    code === 8239 ||
    code === 8287 ||
    code === 12288 ||
    code === 65279
  );
}

function isAsciiAlphaCode(code: number): boolean {
  return (code >= 65 && code <= 90) || (code >= 97 && code <= 122);
}

function isAsciiDigitCode(code: number): boolean {
  return code >= 48 && code <= 57;
}

function isHtmlTagNamePartCode(code: number): boolean {
  return (
    isAsciiAlphaCode(code) || isAsciiDigitCode(code) || code === 58 || code === 95 || code === 45
  );
}

function isAttributeNameStartCode(code: number): boolean {
  return isAsciiAlphaCode(code) || code === 95 || code === 58;
}

function isAttributeNamePartCode(code: number): boolean {
  return (
    isAsciiAlphaCode(code) ||
    isAsciiDigitCode(code) ||
    code === 45 ||
    code === 95 ||
    code === 58 ||
    code === 46
  );
}

function cssEscapeIdentifier(value: string): string {
  let escaped = "";
  for (let index = 0; index < value.length; index += 1) {
    const char = value[index] ?? "";
    const code = char.charCodeAt(0);
    if (code === 0) {
      escaped += "\uFFFD";
    } else if (
      (index === 0 && isAsciiDigitCode(code)) ||
      (index === 1 && isAsciiDigitCode(code) && value[0] === "-")
    ) {
      escaped += `\\${code.toString(16)} `;
    } else if (
      isAsciiAlphaCode(code) ||
      isAsciiDigitCode(code) ||
      code === 45 ||
      code === 95 ||
      code >= 128
    ) {
      escaped += char;
    } else {
      escaped += `\\${char}`;
    }
  }
  return escaped;
}

function documentNewline(text: string): string {
  return text.includes("\r\n") ? "\r\n" : "\n";
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
  const virtualContext = cssCodeActionContextToVirtual(virtual, context);
  return cssService
    .doCodeActions2(css.document, { start, end }, virtualContext, css.stylesheet)
    .map((action) => remapCssCodeAction(virtual, action, cached.source.uri))
    .filter((action): action is CodeAction => Boolean(action));
}

function cssCodeActionContextToVirtual(
  virtual: VirtualDocument,
  context: CodeActionContext,
): CodeActionContext {
  return {
    ...context,
    diagnostics: context.diagnostics
      .map((diagnostic) => diagnosticToVirtualRange(virtual, diagnostic))
      .filter((diagnostic): diagnostic is Diagnostic => Boolean(diagnostic)),
  };
}

function diagnosticToVirtualRange(
  virtual: VirtualDocument,
  diagnostic: Diagnostic,
): Diagnostic | undefined {
  const start = virtual.sourceMap.toVirtualPosition(diagnostic.range.start);
  const end = virtual.sourceMap.toVirtualPosition(diagnostic.range.end);
  return start && end ? { ...diagnostic, range: { start, end } } : undefined;
}

function remapCssCodeAction(
  virtual: VirtualDocument,
  action: CodeAction,
  sourceUri: string,
): CodeAction | undefined {
  const diagnostics = action.diagnostics
    ?.map((diagnostic) => remapDiagnostic(virtual, diagnostic, diagnostic.source ?? "asp-lsp-css"))
    .filter((diagnostic): diagnostic is Diagnostic => Boolean(diagnostic));
  if (!action.edit) {
    return { ...action, diagnostics };
  }
  return {
    ...action,
    diagnostics,
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

async function buildAspFlowchartForCommand(
  argument: unknown,
  token?: GraphCancellationToken,
): Promise<AspFlowchartPayload> {
  const task = beginProgressTask("analyzing", "flowchart.build", {
    cancellable: true,
    current: 0,
    total: 4,
  });
  const cancellation = progressCancellation(task, analysisCancellationFromToken(token));
  const operationCache: GraphFileIndexOperationCache = new Map();
  try {
    return await buildAspFlowchartForCommandWithProgress(
      argument,
      cancellation,
      task,
      operationCache,
    );
  } finally {
    task.end();
  }
}

async function buildAspFlowchartForCommandWithProgress(
  argument: unknown,
  cancellation: AnalysisCancellation,
  progress: AspLspProgressTaskHandle,
  operationCache: GraphFileIndexOperationCache,
): Promise<AspFlowchartPayload> {
  const uri = graphCommandUri(argument) ?? documents.all()[0]?.uri;
  progress.update({
    label: "flowchart.loadDocument",
    current: 0,
    total: 4,
    detail: uri ? progressFileLabelFromUri(uri) : undefined,
  });
  const cached = uri ? await cachedDocumentForGraphAsync(uri) : undefined;
  throwIfGraphCancelled(cancellation);
  if (!cached) {
    return emptyAspFlowchartPayload(uri);
  }
  const settings = cachedSettings(cached.source.uri);
  progress.update({
    label: "flowchart.hydrateDocument",
    current: 1,
    total: 4,
    detail: progressFileLabelFromUri(cached.source.uri),
  });
  await hydrateCachedVbscriptCstAsync(cached, settings, "flowchart");
  progress.update({
    label: "flowchart.collectIncludes",
    current: 2,
    total: 4,
    detail: progressFileLabelFromUri(cached.source.uri),
  });
  const documentsForFlowchart = await collectDocumentGraphDocumentsAsync(
    cached,
    settings,
    cancellation,
  );
  const uniqueFlowchartDocuments = uniqueAspGraphDocuments(documentsForFlowchart);
  progress.update({
    label: "flowchart.indexDocuments",
    current: 0,
    total: uniqueFlowchartDocuments.length + 2,
  });
  const rawIndexedDocuments = await mapWithConcurrency(
    uniqueFlowchartDocuments,
    analysisConcurrency(settings),
    async (document): Promise<AspGraphIndexedDocument> => {
      throwIfGraphCancelled(cancellation);
      const graphIndex = await graphFileIndexForDocumentAsync(document, settings, {
        operationCache,
      });
      throwIfGraphCancelled(cancellation);
      return { document, graphIndex };
    },
    progressMapHooks(progress, (document) => progressFileLabel(document.fileName)),
  );
  progress.update({
    label: "flowchart.canonicalizeSymbols",
    current: uniqueFlowchartDocuments.length,
    total: uniqueFlowchartDocuments.length + 2,
  });
  const indexedDocuments = await canonicalizeImplicitGlobalIndexedDocumentsAsync(
    rawIndexedDocuments,
    settings,
    cancellation,
  );
  throwIfGraphCancelled(cancellation);
  progress.update({
    label: "flowchart.buildPayload",
    current: uniqueFlowchartDocuments.length + 1,
    total: uniqueFlowchartDocuments.length + 2,
    detail: progressFileLabelFromUri(cached.source.uri),
  });
  const payload = buildAspFlowchart(cached.parsed, {
    fileName: flowchartDisplayFileName(graphFileNameFromUri(cached.source.uri)),
    includes: await flowchartIncludesForDocumentAsync(cached.parsed, settings),
    labelLineLength:
      flowchartCommandLabelLineLength(argument) ?? settings.flowchart?.labelLineLength,
    labelMode: flowchartCommandLabelMode(argument) ?? settings.flowchart?.labelMode,
    locale: flowchartCommandLocale(argument) ?? settings.resolvedLocale,
    symbols: indexedDocuments.map((indexed) =>
      flowchartSymbolDocumentFromIndex(indexed.graphIndex.vbSymbolIndex),
    ),
  });
  progress.update({
    label: "flowchart.buildPayload",
    current: uniqueFlowchartDocuments.length + 2,
    total: uniqueFlowchartDocuments.length + 2,
    detail: progressFileLabelFromUri(cached.source.uri),
  });
  return payload;
}

function flowchartSymbolDocumentFromIndex(index: VbSymbolIndex): AspFlowchartSymbolDocument {
  return {
    uri: index.uri,
    declarations: index.declarations.map((declaration) => ({
      id: declaration.id,
      name: declaration.name,
      normalizedName: declaration.normalizedName,
      kind: declaration.kind,
      range: declaration.range,
      nameRange: declaration.nameRange,
      sourceRange: declaration.sourceRange,
      scopeId: declaration.scopeId,
      parentId: declaration.parentId,
      memberOf: declaration.memberOf,
      bindingScope: declaration.bindingScope,
      procedureKind: declaration.procedureKind,
      implicit: declaration.implicit,
      implicitGlobal: declaration.implicitGlobal,
      implicitGlobalCandidate: declaration.implicitGlobalCandidate,
      typeName: declaration.typeName,
    })),
    references: index.references.map((reference) => ({
      name: reference.name,
      normalizedName: reference.normalizedName,
      range: reference.range,
      scopeId: reference.scopeId,
      resolvedId: reference.resolvedId,
      role: reference.role,
      expectedKinds: reference.expectedKinds,
      baseName: reference.baseName,
      memberName: reference.memberName,
    })),
    callSites: index.callSites.map((callSite) => ({
      name: callSite.name,
      normalizedName: callSite.normalizedName,
      range: callSite.range,
      scopeId: callSite.scopeId,
      receiverName: callSite.receiverName,
      memberName: callSite.memberName,
      callKind: callSite.callKind,
      argumentCount: callSite.argumentCount,
      resolvedId: callSite.resolvedId,
    })),
  };
}

function flowchartCommandLocale(argument: unknown): AspLocale | undefined {
  if (!argument || typeof argument !== "object" || !("locale" in argument)) {
    return undefined;
  }
  const locale = (argument as { locale?: unknown }).locale;
  return locale === "ja" || locale === "en" ? locale : undefined;
}

function flowchartCommandLabelLineLength(argument: unknown): number | undefined {
  if (!argument || typeof argument !== "object" || !("labelLineLength" in argument)) {
    return undefined;
  }
  const value = (argument as { labelLineLength?: unknown }).labelLineLength;
  return typeof value === "number" && Number.isFinite(value) && value >= 8
    ? Math.floor(value)
    : undefined;
}

function flowchartCommandLabelMode(argument: unknown): AspFlowchartLabelMode | undefined {
  if (!argument || typeof argument !== "object" || !("labelMode" in argument)) {
    return undefined;
  }
  return flowchartLabelMode((argument as { labelMode?: unknown }).labelMode);
}

function flowchartLabelMode(value: unknown): AspFlowchartLabelMode {
  return value === "raw" || value === "description" ? value : "normal";
}

async function flowchartIncludesForDocumentAsync(
  parsed: AspParsedDocument,
  settings: AspSettings,
): Promise<AspFlowchartInclude[]> {
  const includes: AspFlowchartInclude[] = [];
  for (const include of parsed.includes) {
    const resolved = await resolveIncludePathDetailsAsync(
      parsed.uri,
      include.path,
      include.mode,
      settings,
    );
    includes.push({
      path: include.path,
      mode: include.mode,
      range: include.range,
      exists: resolved.exists,
      resolvedUri: pathToFileUri(normalizeFileName(resolved.fileName)),
      actualPath: resolved.actualPath
        ? flowchartDisplayFileName(normalizeFileName(resolved.actualPath))
        : undefined,
      pathCaseMatches: resolved.pathCaseMatches,
    });
  }
  return includes;
}

function flowchartDisplayFileName(fileName: string): string {
  const normalized = normalizeFileName(fileName);
  const workspaceRoot = workspaceRoots
    .map(normalizeFileName)
    .sort((left, right) => right.length - left.length)
    .find((root) => isFileInDirectoryOrEqual(normalized, root));
  if (!workspaceRoot) {
    return normalized;
  }
  const relative = path.relative(workspaceRoot, normalized);
  return relative || path.basename(normalized);
}

function emptyAspFlowchartPayload(uri: string | undefined): AspFlowchartPayload {
  const payload: Omit<AspFlowchartPayload, "mermaid"> = {
    uri: uri ?? "",
    sections: [],
    nodes: [],
    edges: [],
    includes: [],
    stats: {
      sections: 0,
      nodes: 0,
      edges: 0,
      includes: 0,
    },
  };
  return { ...payload, mermaid: "flowchart TB" };
}

interface AspNavigationSourceDocument {
  uri: string;
  fileName: string;
  text: string;
  parsed: AspParsedDocument;
}

interface AspNavigationBuildState {
  scope: AspNavigationGraphScope;
  rootUri?: string;
  settings: NonNullable<AspSettings["navigationGraph"]>;
  nodes: Map<string, AspNavigationNode>;
  edges: Map<string, AspNavigationEdge>;
  workspaceFileKeys: Set<string>;
  truncatedNodes: number;
  truncatedEdges: number;
}

async function buildAspNavigationGraphForCommand(
  argument: unknown,
  token?: GraphCancellationToken,
): Promise<AspNavigationGraphPayload> {
  const task = beginProgressTask("analyzing", "navigationGraph.build", {
    cancellable: true,
    current: 0,
    total: 4,
  });
  const cancellation = progressCancellation(task, analysisCancellationFromToken(token));
  try {
    const scope = navigationGraphCommandScope(argument);
    const uri =
      graphCommandUri(argument) ??
      documents.all().find((document) => isClassicAspGraphUri(document.uri))?.uri;
    const settings = uri ? cachedSettings(uri) : globalSettings;
    const navigationSettings = navigationGraphCommandSettings(argument, settings);
    task.update({
      label: "navigationGraph.collectDocuments",
      current: 0,
      total: 4,
      detail: uri ? progressFileLabelFromUri(uri) : scope,
    });
    const sourceDocuments = await collectNavigationSourceDocumentsAsync(
      scope,
      uri,
      settings,
      cancellation,
    );
    throwIfGraphCancelled(cancellation);
    task.update({
      label: "navigationGraph.resolveIncludes",
      current: 1,
      total: 4,
      detail: `${sourceDocuments.length}`,
    });
    const includeOwners = await navigationIncludeOwnersAsync(sourceDocuments, settings);
    const state: AspNavigationBuildState = {
      scope,
      rootUri: uri,
      settings: navigationSettings,
      nodes: new Map(),
      edges: new Map(),
      workspaceFileKeys: new Set(
        sourceDocuments.map((document) => graphFileKey(document.fileName)),
      ),
      truncatedNodes: 0,
      truncatedEdges: 0,
    };
    task.update({
      label: "navigationGraph.extract",
      current: 2,
      total: 4,
      detail: `${sourceDocuments.length}`,
    });
    for (const document of sourceDocuments) {
      throwIfGraphCancelled(cancellation);
      const owners = navigationSourceUrisForDocument(document, includeOwners);
      const candidates = [
        ...extractAspNavigationCandidates(document.parsed),
        ...extractJavascriptNavigationCandidates(document.parsed),
      ];
      for (const ownerUri of owners) {
        addNavigationCandidatesForOwner(state, document, ownerUri, candidates);
      }
    }
    task.update({
      label: "navigationGraph.buildPayload",
      current: 3,
      total: 4,
      detail: `${state.nodes.size}/${state.edges.size}`,
    });
    const payload = navigationPayloadFromState(state, sourceDocuments.length);
    connection.sendNotification(navigationGraphUpdatedNotificationMethod, payload);
    task.update({
      label: "navigationGraph.buildPayload",
      current: 4,
      total: 4,
      detail: `${payload.nodes.length}/${payload.edges.length}`,
    });
    return payload;
  } finally {
    task.end();
  }
}

function navigationGraphCommandScope(argument: unknown): AspNavigationGraphScope {
  if (argument && typeof argument === "object" && "scope" in argument) {
    const scope = (argument as { scope?: unknown }).scope;
    if (scope === "folder" || scope === "workspace") {
      return scope;
    }
  }
  return "document";
}

function navigationGraphCommandSettings(
  argument: unknown,
  settings: AspSettings,
): NonNullable<AspSettings["navigationGraph"]> {
  const base = settings.navigationGraph ?? normalizeNavigationGraphSettings(settings);
  if (!argument || typeof argument !== "object") {
    return base;
  }
  const record = argument as { maxNodes?: unknown; maxEdges?: unknown };
  return {
    maxNodes: positiveIntegerSetting(record.maxNodes, base.maxNodes ?? 500),
    maxEdges: positiveIntegerSetting(record.maxEdges, base.maxEdges ?? 1200),
  };
}

async function collectNavigationSourceDocumentsAsync(
  scope: AspNavigationGraphScope,
  uri: string | undefined,
  settings: AspSettings,
  cancellation: AnalysisCancellation,
): Promise<AspNavigationSourceDocument[]> {
  if (scope === "document") {
    if (!uri) {
      return [];
    }
    const cached = await cachedDocumentForGraphAsync(uri);
    if (!cached) {
      return [];
    }
    const graphDocuments = await collectDocumentGraphDocumentsAsync(cached, settings, cancellation);
    return navigationDocumentsFromGraphDocumentsAsync(graphDocuments, settings, cancellation);
  }
  const folder =
    scope === "folder" && uri ? await navigationFolderNameFromUriAsync(uri) : undefined;
  const maxDocuments = settings.graph?.maxDocuments ?? defaultGraphMaxDocuments;
  const documentsByKey = new Map<string, AspNavigationSourceDocument>();
  for (const openDocument of await workspaceAnalyzableOpenDocumentsAsync(settings)) {
    throwIfGraphCancelled(cancellation);
    const fileName = graphFileNameFromUri(openDocument.uri);
    if (folder && !isFileInDirectoryOrEqual(fileName, folder)) {
      continue;
    }
    const cached = await ensureFreshCachedDocumentAsync(openDocument);
    documentsByKey.set(graphFileKey(fileName), navigationDocumentFromCached(cached));
  }
  await ensureWorkspaceIndexAsync(settings, tokenFromAnalysisCancellation(cancellation));
  for (const entry of workspaceIndex.values()) {
    throwIfGraphCancelled(cancellation);
    if (documentsByKey.size >= maxDocuments) {
      break;
    }
    if (folder && !isFileInDirectoryOrEqual(entry.fileName, folder)) {
      continue;
    }
    const key = graphFileKey(entry.fileName);
    if (documentsByKey.has(key)) {
      continue;
    }
    const cached = await cachedFromIndexedAsync(entry, cachedSettings(entry.uri));
    documentsByKey.set(key, navigationDocumentFromCached(cached));
  }
  return [...documentsByKey.values()];
}

async function navigationDocumentsFromGraphDocumentsAsync(
  graphDocuments: AspGraphDocument[],
  settings: AspSettings,
  cancellation: AnalysisCancellation,
): Promise<AspNavigationSourceDocument[]> {
  const documentsByKey = new Map<string, AspNavigationSourceDocument>();
  for (const document of graphDocuments) {
    throwIfGraphCancelled(cancellation);
    const parsed = await parseAspDocumentAsync(document.uri, document.text, settings);
    documentsByKey.set(graphFileKey(document.fileName), {
      uri: document.uri,
      fileName: document.fileName,
      text: document.text,
      parsed,
    });
  }
  return [...documentsByKey.values()];
}

function navigationDocumentFromCached(cached: CachedDocument): AspNavigationSourceDocument {
  const fileName = graphFileNameFromUri(cached.source.uri);
  return {
    uri: pathToFileUri(fileName),
    fileName,
    text: cached.parsed.text,
    parsed: cached.parsed,
  };
}

async function navigationFolderNameFromUriAsync(uri: string): Promise<string> {
  const fileName = graphFileNameFromUri(uri);
  const stat = await fsGateway.statAsync(fileName);
  return stat?.isDirectory() ? normalizeFileName(fileName) : path.dirname(fileName);
}

async function navigationIncludeOwnersAsync(
  documents: AspNavigationSourceDocument[],
  settings: AspSettings,
): Promise<Map<string, Set<string>>> {
  const owners = new Map<string, Set<string>>();
  for (const document of documents) {
    for (const include of document.parsed.includes) {
      const resolved = await resolveIncludePathDetailsAsync(
        document.uri,
        include.path,
        include.mode,
        settings,
      );
      const key = graphFileKey(resolved.actualPath ?? resolved.fileName);
      let set = owners.get(key);
      if (!set) {
        set = new Set();
        owners.set(key, set);
      }
      set.add(document.uri);
    }
  }
  return owners;
}

function navigationSourceUrisForDocument(
  document: AspNavigationSourceDocument,
  includeOwners: Map<string, Set<string>>,
): string[] {
  const owners = includeOwners.get(graphFileKey(document.fileName));
  if (owners && owners.size > 0 && isClassicAspFragmentFile(document.fileName)) {
    return [...owners];
  }
  return [document.uri];
}

function addNavigationCandidatesForOwner(
  state: AspNavigationBuildState,
  document: AspNavigationSourceDocument,
  ownerUri: string,
  candidates: AspNavigationCandidate[],
): void {
  const sourceNode = ensureNavigationUriNode(state, ownerUri);
  for (const candidate of candidates) {
    if (shouldSkipNavigationCandidate(candidate)) {
      continue;
    }
    const targetNode = ensureNavigationTargetNode(state, ownerUri, candidate);
    const parameters = [
      ...navigationParametersFromLiteralTarget(candidate.target),
      ...(candidate.parameters ?? []),
    ];
    const declaredInUri = candidate.declaredInUri ?? document.uri;
    const edgeKey = [
      sourceNode.id,
      targetNode.id,
      candidate.kind,
      candidate.method ?? "",
      candidate.targetFrame ?? "",
    ].join("|");
    const existing = state.edges.get(edgeKey);
    const evidence = candidate.evidence.map((item) => ({
      ...item,
      uri: item.uri || declaredInUri,
    }));
    if (existing) {
      existing.count = (existing.count ?? 1) + 1;
      existing.ranges.push(candidate.range);
      existing.evidence.push(...evidence);
      existing.parameters = mergeNavigationParameters(existing.parameters ?? [], parameters);
      existing.confidence = lowerNavigationConfidence(
        existing.confidence,
        candidate.confidence ?? "unknown",
      );
      continue;
    }
    if (state.edges.size >= (state.settings.maxEdges ?? 1200)) {
      state.truncatedEdges += 1;
      continue;
    }
    state.edges.set(edgeKey, {
      id: `edge:${state.edges.size + 1}`,
      source: sourceNode.id,
      target: targetNode.id,
      kind: candidate.kind,
      label: navigationEdgeLabel(candidate),
      confidence: candidate.confidence ?? "unknown",
      method: candidate.method,
      targetFrame: candidate.targetFrame,
      ranges: [candidate.range],
      parameters,
      declaredInUri,
      evidence,
      count: 1,
    });
  }
}

function ensureNavigationUriNode(state: AspNavigationBuildState, uri: string): AspNavigationNode {
  const fileName = graphFileNameFromUri(uri);
  const id = `page:${graphFileKey(fileName)}`;
  const existing = state.nodes.get(id);
  if (existing) {
    return existing;
  }
  const node: AspNavigationNode = {
    id,
    kind: isClassicAspFragmentFile(fileName) ? "fragment" : "page",
    label: flowchartDisplayFileName(fileName),
    uri: pathToFileUri(fileName),
    fileName: flowchartDisplayFileName(fileName),
    exists: true,
    isRoot: state.rootUri ? sameFileIdentityUri(pathToFileUri(fileName), state.rootUri) : false,
  };
  return addNavigationNode(state, node);
}

function ensureNavigationTargetNode(
  state: AspNavigationBuildState,
  sourceUri: string,
  candidate: AspNavigationCandidate,
): AspNavigationNode {
  const target = candidate.target;
  if (target.kind !== "literal") {
    return ensureNavigationUnknownNode(state, target.text ?? "dynamic target");
  }
  const rawTarget = (target.text ?? "").trim();
  if (rawTarget.length === 0) {
    return candidate.kind === "htmlForm"
      ? ensureNavigationUriNode(state, sourceUri)
      : ensureNavigationUnknownNode(state, "empty target");
  }
  if (isExternalNavigationUrl(rawTarget)) {
    const id = `external:${stableNavigationId(rawTarget)}`;
    return addNavigationNode(state, {
      id,
      kind: "external",
      label: rawTarget,
      externalUrl: rawTarget,
      exists: true,
    });
  }
  const resolved = resolveLiteralNavigationTarget(sourceUri, rawTarget);
  if (!resolved) {
    return ensureNavigationUnknownNode(state, rawTarget);
  }
  const id = `page:${graphFileKey(resolved.fileName)}`;
  const existing = state.nodes.get(id);
  if (existing) {
    return existing;
  }
  return addNavigationNode(state, {
    id,
    kind: isClassicAspFragmentFile(resolved.fileName) ? "fragment" : "page",
    label: flowchartDisplayFileName(resolved.fileName),
    uri: pathToFileUri(resolved.fileName),
    fileName: flowchartDisplayFileName(resolved.fileName),
    exists: resolved.exists,
  });
}

function addNavigationNode(
  state: AspNavigationBuildState,
  node: AspNavigationNode,
): AspNavigationNode {
  const existing = state.nodes.get(node.id);
  if (existing) {
    return existing;
  }
  if (state.nodes.size >= (state.settings.maxNodes ?? 500)) {
    state.truncatedNodes += 1;
    return ensureNavigationUnknownNode(state, "node limit");
  }
  state.nodes.set(node.id, node);
  return node;
}

function ensureNavigationUnknownNode(
  state: AspNavigationBuildState,
  label: string,
): AspNavigationNode {
  const id = `unknown:${stableNavigationId(label || "unknown")}`;
  const existing = state.nodes.get(id);
  if (existing) {
    return existing;
  }
  const node: AspNavigationNode = {
    id,
    kind: "unknown",
    label: label || "unknown",
    exists: false,
  };
  state.nodes.set(id, node);
  return node;
}

function resolveLiteralNavigationTarget(
  sourceUri: string,
  target: string,
): { fileName: string; exists: boolean } | undefined {
  const pathPart = target.split("#", 1)[0].split("?", 1)[0];
  const normalizedTarget = pathPart.length > 0 ? pathPart : uriToFileName(sourceUri);
  if (/^[a-z][a-z0-9+.-]*:/i.test(normalizedTarget) || normalizedTarget.startsWith("//")) {
    return undefined;
  }
  const sourceFileName = graphFileNameFromUri(sourceUri);
  let fileName: string;
  if (normalizedTarget.startsWith("/")) {
    const root =
      workspaceRootForFileName(sourceFileName) ?? workspaceRoots[0] ?? path.dirname(sourceFileName);
    fileName = path.resolve(root, normalizedTarget.replace(/^\/+/, ""));
  } else if (path.isAbsolute(normalizedTarget)) {
    fileName = normalizeFileName(normalizedTarget);
  } else {
    fileName = path.resolve(path.dirname(sourceFileName), normalizedTarget);
  }
  return {
    fileName: normalizeFileName(fileName),
    exists: fs.existsSync(fileName),
  };
}

function shouldSkipNavigationCandidate(candidate: AspNavigationCandidate): boolean {
  if (candidate.target.kind !== "literal") {
    return false;
  }
  const value = (candidate.target.text ?? "").trim().toLowerCase();
  return candidate.kind === "htmlAnchor" && (value === "#" || value.startsWith("javascript:void"));
}

function navigationEdgeLabel(candidate: AspNavigationCandidate): string {
  if (candidate.method) {
    return `${candidate.kind} ${candidate.method}`;
  }
  return candidate.kind;
}

function navigationPayloadFromState(
  state: AspNavigationBuildState,
  documentCount: number,
): AspNavigationGraphPayload {
  const nodes = [...state.nodes.values()].sort((left, right) =>
    left.label.localeCompare(right.label),
  );
  const edges = [...state.edges.values()].sort((left, right) => left.id.localeCompare(right.id));
  const confidenceCount = (confidence: AspNavigationConfidence) =>
    edges.filter((edge) => edge.confidence === confidence).length;
  return {
    scope: state.scope,
    rootUri: state.rootUri,
    nodes,
    edges,
    settings: state.settings,
    stats: {
      documents: documentCount,
      nodes: nodes.length,
      edges: edges.length,
      certain: confidenceCount("certain"),
      probable: confidenceCount("probable"),
      possible: confidenceCount("possible"),
      unknown: confidenceCount("unknown"),
      external: nodes.filter((node) => node.kind === "external").length,
      truncatedNodes: state.truncatedNodes,
      truncatedEdges: state.truncatedEdges,
    },
    truncated: state.truncatedNodes > 0 || state.truncatedEdges > 0,
  };
}

function mergeNavigationParameters(
  left: AspNavigationParameterFlow[],
  right: AspNavigationParameterFlow[],
): AspNavigationParameterFlow[] {
  const parameters = new Map<string, AspNavigationParameterFlow>();
  for (const parameter of [...left, ...right]) {
    parameters.set(`${parameter.source}:${parameter.name}:${parameter.value ?? ""}`, parameter);
  }
  return [...parameters.values()];
}

function navigationParametersFromLiteralTarget(
  target: AspNavigationUrlValue,
): AspNavigationParameterFlow[] {
  if (target.kind !== "literal") {
    return parametersFromNavigationParts(target.parts);
  }
  const query = target.text?.split("#", 1)[0].split("?")[1];
  if (!query) {
    return [];
  }
  return query
    .split("&")
    .filter((part) => part.length > 0)
    .map((part) => {
      const [name, value] = part.split("=");
      return {
        name: decodeURIComponentSafe(name),
        source: "queryString" as const,
        value: value === undefined ? undefined : decodeURIComponentSafe(value),
        confidence: "certain" as const,
      };
    });
}

function parametersFromNavigationParts(
  parts: AspNavigationUrlPart[] | undefined,
): AspNavigationParameterFlow[] {
  return (parts ?? [])
    .filter((part) => part.kind === "request")
    .map((part) => ({
      name: part.name ?? "value",
      source: part.source ?? "request",
      targetUsage: part.text,
      confidence: "possible" as const,
    }));
}

function decodeURIComponentSafe(value: string): string {
  try {
    return decodeURIComponent(value.replace(/\+/g, " "));
  } catch {
    return value;
  }
}

function lowerNavigationConfidence(
  left: AspNavigationConfidence,
  right: AspNavigationConfidence,
): AspNavigationConfidence {
  const order: AspNavigationConfidence[] = ["certain", "probable", "possible", "unknown"];
  return order[Math.max(order.indexOf(left), order.indexOf(right))] ?? "unknown";
}

function isExternalNavigationUrl(value: string): boolean {
  return /^[a-z][a-z0-9+.-]*:/i.test(value) || value.startsWith("//");
}

function isClassicAspFragmentFile(fileName: string): boolean {
  return path.extname(fileName).toLowerCase() === ".inc";
}

function stableNavigationId(value: string): string {
  return (
    value
      .toLowerCase()
      .replace(/[^a-z0-9._-]+/g, "-")
      .replace(/^-+|-+$/g, "")
      .slice(0, 96) || "unknown"
  );
}

function extractJavascriptNavigationCandidates(
  parsed: AspParsedDocument,
): AspNavigationCandidate[] {
  const candidates: AspNavigationCandidate[] = [];
  for (const region of parsed.regions) {
    if (region.language !== "javascript") {
      continue;
    }
    const source = parsed.text.slice(region.contentStart, region.contentEnd);
    const sourceFile = ts.createSourceFile(
      `${parsed.uri}.js`,
      source,
      ts.ScriptTarget.Latest,
      true,
      ts.ScriptKind.JS,
    );
    const state: {
      variables: Map<string, AspNavigationUrlValue>;
      formActions: Map<string, AspNavigationUrlValue>;
      formMethods: Map<string, string>;
    } = {
      variables: new Map(),
      formActions: new Map(),
      formMethods: new Map(),
    };
    const visit = (node: ts.Node): void => {
      collectJavascriptNavigationFromNode(parsed, region, sourceFile, node, state, candidates);
      ts.forEachChild(node, visit);
    };
    ts.forEachChild(sourceFile, visit);
  }
  return candidates;
}

function collectJavascriptNavigationFromNode(
  parsed: AspParsedDocument,
  region: AspRegion,
  sourceFile: ts.SourceFile,
  node: ts.Node,
  state: {
    variables: Map<string, AspNavigationUrlValue>;
    formActions: Map<string, AspNavigationUrlValue>;
    formMethods: Map<string, string>;
  },
  candidates: AspNavigationCandidate[],
): void {
  if (ts.isVariableDeclaration(node) && ts.isIdentifier(node.name) && node.initializer) {
    state.variables.set(
      node.name.text,
      evaluateJavascriptUrlValue(node.initializer, sourceFile, state.variables),
    );
    return;
  }
  if (ts.isBinaryExpression(node) && node.operatorToken.kind === ts.SyntaxKind.EqualsToken) {
    const left = node.left;
    const rightValue = evaluateJavascriptUrlValue(node.right, sourceFile, state.variables);
    if (isJavascriptLocationTarget(left)) {
      candidates.push(
        javascriptNavigationCandidate(
          parsed,
          region,
          sourceFile,
          node,
          node.right,
          "javascriptLocation",
          rightValue,
          "location assignment",
        ),
      );
      return;
    }
    if (ts.isPropertyAccessExpression(left) && left.name.text === "action") {
      state.formActions.set(normalizeJavascriptObjectKey(left.expression, sourceFile), rightValue);
      return;
    }
    if (ts.isPropertyAccessExpression(left) && left.name.text === "method") {
      const method = rightValue.kind === "literal" ? rightValue.text?.toUpperCase() : undefined;
      if (method) {
        state.formMethods.set(normalizeJavascriptObjectKey(left.expression, sourceFile), method);
      }
    }
    return;
  }
  if (!ts.isCallExpression(node)) {
    return;
  }
  const expression = node.expression;
  if (ts.isPropertyAccessExpression(expression)) {
    const memberName = expression.name.text;
    if (
      (memberName === "assign" || memberName === "replace" || memberName === "open") &&
      isJavascriptLocationReceiver(expression.expression, memberName)
    ) {
      const targetExpression = node.arguments[0];
      candidates.push(
        javascriptNavigationCandidate(
          parsed,
          region,
          sourceFile,
          node,
          targetExpression,
          "javascriptLocation",
          targetExpression
            ? evaluateJavascriptUrlValue(targetExpression, sourceFile, state.variables)
            : { kind: "unknown", text: "{unknown}" },
          memberName === "open" ? "window.open" : `location.${memberName}`,
        ),
      );
      return;
    }
    if (
      (memberName === "pushState" || memberName === "replaceState") &&
      isHistoryReceiver(expression.expression)
    ) {
      const targetExpression = node.arguments[2];
      candidates.push(
        javascriptNavigationCandidate(
          parsed,
          region,
          sourceFile,
          node,
          targetExpression,
          "javascriptHistory",
          targetExpression
            ? evaluateJavascriptUrlValue(targetExpression, sourceFile, state.variables)
            : { kind: "unknown", text: "{unknown}" },
          `history.${memberName}`,
        ),
      );
      return;
    }
    if (memberName === "submit") {
      const formKey = normalizeJavascriptObjectKey(expression.expression, sourceFile);
      candidates.push(
        javascriptNavigationCandidate(
          parsed,
          region,
          sourceFile,
          node,
          expression.expression,
          "javascriptFormSubmit",
          state.formActions.get(formKey) ?? { kind: "unknown", text: formKey },
          "form.submit",
          state.formMethods.get(formKey),
        ),
      );
    }
  }
}

function javascriptNavigationCandidate(
  parsed: AspParsedDocument,
  region: AspRegion,
  sourceFile: ts.SourceFile,
  node: ts.Node,
  valueNode: ts.Node | undefined,
  kind: AspNavigationEdgeKind,
  target: AspNavigationUrlValue,
  label: string,
  method?: string,
): AspNavigationCandidate {
  const start = region.contentStart + node.getStart(sourceFile);
  const end = region.contentStart + node.getEnd();
  const valueStart = valueNode ? region.contentStart + valueNode.getStart(sourceFile) : start;
  const valueEnd = valueNode ? region.contentStart + valueNode.getEnd() : end;
  const range = rangeFromOffsets(parsed.text, start, end);
  const valueRange = rangeFromOffsets(parsed.text, valueStart, valueEnd);
  const confidence =
    target.kind === "literal" ? "probable" : target.kind === "template" ? "possible" : "unknown";
  return {
    kind,
    target,
    range,
    valueRange,
    method,
    parameters: parametersFromNavigationParts(target.parts),
    declaredInUri: parsed.uri,
    evidence: [
      {
        uri: parsed.uri,
        range,
        valueRange,
        label,
        snippet: parsed.text.slice(start, end).replace(/\s+/g, " ").trim().slice(0, 240),
        extractor: "javascript",
      },
    ],
    confidence,
    source: "javascript",
  };
}

function evaluateJavascriptUrlValue(
  node: ts.Expression,
  sourceFile: ts.SourceFile,
  variables: Map<string, AspNavigationUrlValue>,
): AspNavigationUrlValue {
  if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) {
    return navigationUrlValueFromJavaScriptLiteral(node.text);
  }
  if (ts.isNumericLiteral(node)) {
    return { kind: "literal", text: node.text };
  }
  if (node.kind === ts.SyntaxKind.TrueKeyword || node.kind === ts.SyntaxKind.FalseKeyword) {
    return { kind: "literal", text: node.kind === ts.SyntaxKind.TrueKeyword ? "true" : "false" };
  }
  if (ts.isIdentifier(node)) {
    return variables.get(node.text) ?? { kind: "unknown", text: node.text };
  }
  if (ts.isBinaryExpression(node) && node.operatorToken.kind === ts.SyntaxKind.PlusToken) {
    return combineJavascriptUrlValues([
      evaluateJavascriptUrlValue(node.left, sourceFile, variables),
      evaluateJavascriptUrlValue(node.right, sourceFile, variables),
    ]);
  }
  if (ts.isTemplateExpression(node)) {
    const parts: AspNavigationUrlPart[] = [{ kind: "text", text: node.head.text }];
    for (const span of node.templateSpans) {
      parts.push({ kind: "unknown", text: span.expression.getText(sourceFile) });
      parts.push({ kind: "text", text: span.literal.text });
    }
    return navigationTemplateValue(parts);
  }
  if (ts.isCallExpression(node) && ts.isIdentifier(node.expression)) {
    const name = node.expression.text;
    if (
      (name === "String" || name === "encodeURIComponent" || name === "encodeURI") &&
      node.arguments[0]
    ) {
      return evaluateJavascriptUrlValue(node.arguments[0], sourceFile, variables);
    }
  }
  return { kind: "unknown", text: node.getText(sourceFile) };
}

function navigationUrlValueFromJavaScriptLiteral(value: string): AspNavigationUrlValue {
  if (!value.includes("<%")) {
    return { kind: "literal", text: value };
  }
  const parts: AspNavigationUrlPart[] = [];
  for (const part of value.split(/(<%[\s\S]*?%>)/g).filter((item) => item.length > 0)) {
    if (!part.startsWith("<%")) {
      parts.push({ kind: "text", text: part });
      continue;
    }
    const request = part.match(/Request(?:\.(QueryString|Form))?\s*\(\s*["']([^"']+)["']\s*\)/i);
    if (request) {
      parts.push({
        kind: "request",
        source:
          request[1]?.toLowerCase() === "querystring"
            ? "queryString"
            : request[1]?.toLowerCase() === "form"
              ? "form"
              : "request",
        name: request[2],
      });
    } else {
      parts.push({ kind: "unknown", text: part });
    }
  }
  return navigationTemplateValue(parts);
}

function combineJavascriptUrlValues(values: AspNavigationUrlValue[]): AspNavigationUrlValue {
  const parts: AspNavigationUrlPart[] = [];
  for (const value of values) {
    if (value.kind === "literal") {
      parts.push({ kind: "text", text: value.text ?? "" });
    } else if (value.kind === "template") {
      parts.push(...(value.parts ?? []));
    } else {
      parts.push({ kind: "unknown", text: value.text });
    }
  }
  if (parts.every((part) => part.kind === "text")) {
    return { kind: "literal", text: parts.map((part) => part.text ?? "").join("") };
  }
  return navigationTemplateValue(parts);
}

function navigationTemplateValue(parts: AspNavigationUrlPart[]): AspNavigationUrlValue {
  return {
    kind: "template",
    text: parts
      .map((part) => {
        if (part.kind === "text") {
          return part.text ?? "";
        }
        if (part.kind === "request") {
          return `{${part.source ?? "request"}:${part.name ?? "value"}}`;
        }
        return "{unknown}";
      })
      .join(""),
    parts,
  };
}

function isJavascriptLocationTarget(node: ts.Expression): boolean {
  if (ts.isIdentifier(node) && node.text === "location") {
    return true;
  }
  if (!ts.isPropertyAccessExpression(node)) {
    return false;
  }
  const text = node.getText();
  return (
    text === "location.href" ||
    text === "window.location" ||
    text === "window.location.href" ||
    text === "document.location" ||
    text === "document.location.href"
  );
}

function isJavascriptLocationReceiver(node: ts.Expression, memberName: string): boolean {
  const text = node.getText();
  if (memberName === "open") {
    return text === "window";
  }
  return text === "location" || text === "window.location" || text === "document.location";
}

function isHistoryReceiver(node: ts.Expression): boolean {
  const text = node.getText();
  return text === "history" || text === "window.history";
}

function normalizeJavascriptObjectKey(node: ts.Expression, sourceFile: ts.SourceFile): string {
  return node.getText(sourceFile).replace(/\s+/g, "");
}

function buildAspGraphForCommand(
  argument: unknown,
  token?: GraphCancellationToken,
): Promise<AspGraphPayload> {
  return aspGraphBuildService.buildAspGraphForCommand(argument, token);
}

async function buildCappedAspGraphForCommand(
  argument: unknown,
  token?: GraphCancellationToken,
): Promise<AspGraphPayload> {
  const payload = await buildAspGraphForCommand(argument, token);
  const uri = graphCommandUri(argument);
  const settings = uri ? cachedSettings(uri) : globalSettings;
  return capAspGraphPayloadForWebview(payload, settings);
}

async function buildGraphCommandAsync(
  argument: unknown,
  token?: GraphCancellationToken,
): Promise<AspGraphPayload> {
  const identity = aspGraphCommandRequestIdentity(argument);
  const cached = aspGraphPayloadCache.get(identity.key);
  if (cached) {
    cached.lastUsed = Date.now();
  }
  if (cached?.signature === identity.signature) {
    return graphPayloadForCommandResponse(cached.payload, identity.correlationId, {
      pending: false,
    });
  }
  if (await shouldBuildGraphSynchronouslyAsync(identity, argument, token)) {
    const payload = cleanAspGraphPayloadForCache(
      await buildCappedAspGraphForCommand(argument, token),
    );
    aspGraphPayloadCache.set(identity.key, {
      payload,
      signature: identity.signature,
      lastUsed: Date.now(),
    });
    return graphPayloadForCommandResponse(payload, identity.correlationId, { pending: false });
  }
  const background = ensureAspGraphBackgroundBuild(identity, argument);
  if (cached) {
    return graphPayloadForCommandResponse(cached.payload, identity.correlationId, {
      pending: true,
      backgroundTaskId: background.task.id,
    });
  }
  const partial = await buildFastPartialAspGraphAsync(identity);
  return graphPayloadForCommandResponse(partial, identity.correlationId, {
    pending: true,
    backgroundTaskId: background.task.id,
  });
}

function aspGraphCommandRequestIdentity(argument: unknown): AspGraphCommandRequestIdentity {
  const scope = graphCommandScope(argument);
  const uri = graphCommandNormalizedUri(graphCommandUri(argument));
  const settings = uri ? cachedSettings(uri) : globalSettings;
  return {
    scope,
    uri,
    settings,
    key: aspGraphPayloadCacheKey(argument, settings, scope, uri),
    signature: aspGraphPayloadSignature(scope, uri),
    correlationId: nextAspGraphCorrelationId(),
  };
}

function aspGraphPayloadCacheKey(
  argument: unknown,
  settings: AspSettings,
  scope: AspGraphScope,
  uri: string | undefined,
): string {
  return JSON.stringify({
    scope,
    uri,
    request: {
      includeIncomingDocumentIncludes: graphCommandBooleanArgument(
        argument,
        "includeIncomingDocumentIncludes",
      ),
      includeRelatedIncludeTreesForUnresolved: graphCommandBooleanArgument(
        argument,
        "includeRelatedIncludeTreesForUnresolved",
      ),
      forceRelatedIncludeTreeAnalysis: graphCommandBooleanArgument(
        argument,
        "forceRelatedIncludeTreeAnalysis",
      ),
      includeAnalysisTypeDetails: graphCommandBooleanArgument(
        argument,
        "includeAnalysisTypeDetails",
      ),
      maxDocuments: graphCommandNumberArgument(argument, "maxDocuments"),
      maxTextLength: graphCommandNumberArgument(argument, "maxTextLength"),
      includeTreeMaxDocuments: graphCommandNumberArgument(argument, "includeTreeMaxDocuments"),
      includeTreeMaxTextLength: graphCommandNumberArgument(argument, "includeTreeMaxTextLength"),
    },
    settings: {
      parse: parseSettingsIdentity(settings),
      include: includeResolutionIdentity(settings),
      graph: normalizeGraphSettings(settings),
      vbscript: settings.vbscript,
      defaultLanguage: settings.defaultLanguage,
      workspace: workspaceIndexSettingsIdentity(settings),
    },
  });
}

function aspGraphPayloadSignature(scope: AspGraphScope, uri: string | undefined): string {
  const openDocuments = documents
    .all()
    .filter((document) => graphSignatureIncludesOpenDocument(scope, uri, document.uri))
    .map((document) => [
      graphCommandNormalizedUri(document.uri) ?? document.uri,
      document.version,
      document.getText().length,
    ]);
  return JSON.stringify({
    workspaceGeneration,
    includeResolutionGeneration,
    openDocuments,
  });
}

function graphSignatureIncludesOpenDocument(
  scope: AspGraphScope,
  rootUri: string | undefined,
  documentUri: string,
): boolean {
  if (!isClassicAspGraphUri(documentUri)) {
    return false;
  }
  if (scope === "workspace" || !rootUri?.startsWith("file://")) {
    return scope === "workspace";
  }
  const rootFileName = graphFileNameFromUri(rootUri);
  const documentFileName = graphFileNameFromUri(documentUri);
  return scope === "document"
    ? graphFileKey(rootFileName) === graphFileKey(documentFileName)
    : isFileInDirectoryOrEqual(documentFileName, rootFileName);
}

function graphCommandBooleanArgument(argument: unknown, name: string): boolean | undefined {
  if (!argument || typeof argument !== "object" || !(name in argument)) {
    return undefined;
  }
  const value = (argument as Record<string, unknown>)[name];
  return typeof value === "boolean" ? value : undefined;
}

function graphCommandNumberArgument(argument: unknown, name: string): number | undefined {
  if (!argument || typeof argument !== "object" || !(name in argument)) {
    return undefined;
  }
  const value = (argument as Record<string, unknown>)[name];
  return typeof value === "number" && Number.isFinite(value) ? Math.floor(value) : undefined;
}

function graphCommandNormalizedUri(uri: string | undefined): string | undefined {
  return uri?.startsWith("file://") ? pathToFileUri(graphFileNameFromUri(uri)) : uri;
}

let aspGraphCorrelationSequence = 0;

function nextAspGraphCorrelationId(): string {
  aspGraphCorrelationSequence += 1;
  return `graph-${Date.now().toString(36)}-${aspGraphCorrelationSequence.toString(36)}`;
}

async function shouldBuildGraphSynchronouslyAsync(
  identity: AspGraphCommandRequestIdentity,
  argument: unknown,
  token?: GraphCancellationToken,
): Promise<boolean> {
  if (graphBackgroundBuildMinDocuments <= 0) {
    return false;
  }
  const estimatedDocuments = await estimateGraphCommandDocumentCountAsync(
    identity,
    argument,
    token,
  );
  return estimatedDocuments <= graphBackgroundBuildMinDocuments;
}

async function estimateGraphCommandDocumentCountAsync(
  identity: AspGraphCommandRequestIdentity,
  argument: unknown,
  token?: GraphCancellationToken,
): Promise<number> {
  if (
    identity.scope === "document" &&
    !documentGraphCommandMayUseWorkspaceIndex(argument, identity.settings)
  ) {
    return 1;
  }
  await ensureWorkspaceIndexAsync(identity.settings, token);
  if (identity.scope === "workspace") {
    return (
      workspaceIndex.size +
      documents.all().filter((document) => isClassicAspGraphUri(document.uri)).length
    );
  }
  if (identity.scope === "folder" && identity.uri?.startsWith("file://")) {
    const folderName = graphFileNameFromUri(identity.uri);
    return (
      [...workspaceIndex.values()].filter((entry) =>
        isFileInDirectoryOrEqual(entry.fileName, folderName),
      ).length +
      documents
        .all()
        .filter(
          (document) =>
            isClassicAspGraphUri(document.uri) &&
            isFileInDirectoryOrEqual(graphFileNameFromUri(document.uri), folderName),
        ).length
    );
  }
  return workspaceIndex.size;
}

function documentGraphCommandMayUseWorkspaceIndex(
  argument: unknown,
  settings: AspSettings,
): boolean {
  return (
    settings.graph?.showIncomingDocumentIncludes === true ||
    graphCommandBooleanArgument(argument, "includeIncomingDocumentIncludes") === true ||
    graphCommandBooleanArgument(argument, "forceRelatedIncludeTreeAnalysis") === true ||
    graphCommandBooleanArgument(argument, "includeRelatedIncludeTreesForUnresolved") !== false
  );
}

function ensureAspGraphBackgroundBuild(
  identity: AspGraphCommandRequestIdentity,
  argument: unknown,
): AspGraphBackgroundBuild {
  const existing = activeGraphBackgroundBuilds.get(identity.key);
  if (existing?.signature === identity.signature) {
    existing.correlations.add(identity.correlationId);
    return existing;
  }
  const carryCorrelations = existing ? [...existing.correlations] : [];
  if (existing) {
    cancelAspGraphBackgroundBuild(existing, { notify: false });
  }
  const task = beginProgressTask("analyzing", `graph.${identity.scope}`, {
    current: 0,
    total: 1,
    detail: identity.uri ? progressFileLabelFromUri(identity.uri) : undefined,
    cancellable: true,
  });
  const build: AspGraphBackgroundBuild = {
    key: identity.key,
    signature: identity.signature,
    scope: identity.scope,
    uri: identity.uri,
    argument,
    task,
    correlations: new Set([...carryCorrelations, identity.correlationId]),
    started: false,
  };
  activeGraphBackgroundBuilds.set(identity.key, build);
  build.timer = setTimeout(() => {
    build.timer = undefined;
    build.started = true;
    void runAspGraphBackgroundBuildAsync(build);
  }, graphBackgroundBuildDebounceMs);
  return build;
}

function cancelAspGraphBackgroundBuild(
  build: AspGraphBackgroundBuild,
  options: { notify: boolean },
): void {
  if (build.timer) {
    clearTimeout(build.timer);
    build.timer = undefined;
  }
  cancelProgressTask(build.task.id);
  if (!build.started) {
    activeGraphBackgroundBuilds.delete(build.key);
    build.task.end();
    if (options.notify) {
      notifyAspGraphBackgroundError(build, graphBackgroundCancelledMessage(build));
    }
  }
}

function cancelAspGraphBackgroundBuilds(reason: string): void {
  const builds = Array.from(activeGraphBackgroundBuilds.values());
  for (const build of builds) {
    logDebugSummary(globalSettings, `[asp-lsp] graph.background.cancel: ${reason}`);
    cancelAspGraphBackgroundBuild(build, { notify: true });
  }
}

async function runAspGraphBackgroundBuildAsync(build: AspGraphBackgroundBuild): Promise<void> {
  try {
    const cancellation = progressCancellation(build.task);
    const payload = cleanAspGraphPayloadForCache(
      await buildCappedAspGraphForCommand(
        build.argument,
        tokenFromAnalysisCancellation(cancellation),
      ),
    );
    if (activeGraphBackgroundBuilds.get(build.key) !== build) {
      return;
    }
    if (aspGraphPayloadSignature(build.scope, build.uri) !== build.signature) {
      notifyAspGraphBackgroundError(build, graphBackgroundCancelledMessage(build));
      return;
    }
    aspGraphPayloadCache.set(build.key, {
      payload,
      signature: build.signature,
      lastUsed: Date.now(),
    });
    for (const correlationId of build.correlations) {
      sendAspGraphUpdatedNotification({
        correlationId,
        scope: build.scope,
        uri: build.uri,
        payload: graphPayloadForCommandResponse(payload, correlationId, { pending: false }),
        final: true,
      });
    }
  } catch (error) {
    if (activeGraphBackgroundBuilds.get(build.key) === build) {
      notifyAspGraphBackgroundError(build, graphBackgroundErrorMessage(build, error));
    }
  } finally {
    if (activeGraphBackgroundBuilds.get(build.key) === build) {
      activeGraphBackgroundBuilds.delete(build.key);
    }
    build.task.end();
  }
}

function notifyAspGraphBackgroundError(build: AspGraphBackgroundBuild, error: string): void {
  for (const correlationId of build.correlations) {
    sendAspGraphUpdatedNotification({
      correlationId,
      scope: build.scope,
      uri: build.uri,
      final: true,
      error,
    });
  }
}

function sendAspGraphUpdatedNotification(notification: AspGraphUpdatedNotification): void {
  connection.sendNotification(graphUpdatedNotificationMethod, notification);
}

function graphBackgroundCancelledMessage(build: AspGraphBackgroundBuild): string {
  return createLocalizer(
    build.uri ? cachedSettings(build.uri).resolvedLocale : globalSettings.resolvedLocale,
  ).t("server.graph.backgroundCancelled");
}

function graphBackgroundErrorMessage(build: AspGraphBackgroundBuild, error: unknown): string {
  logServerWarning(`[asp-lsp] graph.background.failed: ${errorMessage(error)}`);
  return createLocalizer(
    build.uri ? cachedSettings(build.uri).resolvedLocale : globalSettings.resolvedLocale,
  ).t("server.graph.backgroundFailed");
}

function graphPayloadForCommandResponse(
  payload: AspGraphPayload,
  correlationId: string,
  options: { pending: boolean; backgroundTaskId?: string },
): AspGraphPayload {
  return {
    ...payload,
    correlationId,
    pending: options.pending,
    backgroundTaskId: options.backgroundTaskId,
  };
}

function cleanAspGraphPayloadForCache(payload: AspGraphPayload): AspGraphPayload {
  const {
    correlationId: _correlationId,
    pending: _pending,
    backgroundTaskId: _backgroundTaskId,
    ...clean
  } = payload;
  return clean;
}

async function buildFastPartialAspGraphAsync(
  identity: AspGraphCommandRequestIdentity,
): Promise<AspGraphPayload> {
  const state = createAspGraphBuildState(identity.settings, identity.uri, {
    reason: "partial-pending",
  });
  const sources = await partialGraphSourcesAsync(identity);
  const limitedSources = sources.slice(0, graphPartialMaxDocuments);
  for (const source of limitedSources) {
    await addPartialGraphSourceAsync(state, source, identity.settings);
  }
  state.stats = recomputeAspGraphStats(state.nodes.values(), state.links.values());
  const truncated =
    sources.length > limitedSources.length
      ? {
          reason: `partial-pending; documents>${graphPartialMaxDocuments}`,
          nodes: sources.length,
          links: state.links.size,
        }
      : state.truncated;
  return capAspGraphPayloadForWebview(
    {
      scope: identity.scope,
      rootUri: state.rootUri,
      nodes: [...state.nodes.values()],
      links: [...state.links.values()],
      settings: graphPayloadSettings(identity.settings),
      stats: state.stats,
      truncated,
    },
    identity.settings,
  );
}

interface PartialGraphSource {
  uri: string;
  fileName: string;
  includeRefs: AspInclude[];
  exists: boolean;
}

async function partialGraphSourcesAsync(
  identity: AspGraphCommandRequestIdentity,
): Promise<PartialGraphSource[]> {
  const sources = new Map<string, PartialGraphSource>();
  const addSource = (source: PartialGraphSource): void => {
    sources.set(graphFileKey(source.fileName), source);
  };
  for (const document of documents.all()) {
    if (!partialGraphIncludesOpenDocument(identity, document.uri)) {
      continue;
    }
    const fileName = graphFileNameFromUri(document.uri);
    addSource({
      uri: pathToFileUri(fileName),
      fileName,
      includeRefs: extractAspIncludeRefs(document.getText()),
      exists: true,
    });
  }
  if (identity.scope === "document" && identity.uri?.startsWith("file://")) {
    const fileName = graphFileNameFromUri(identity.uri);
    if (!sources.has(graphFileKey(fileName))) {
      const entry = await readGraphIncludeRefsEntryAsync(fileName, identity.settings);
      addSource({
        uri: pathToFileUri(fileName),
        fileName,
        includeRefs: entry?.includeRefs ?? [],
        exists: true,
      });
    }
  }
  for (const entry of workspaceIndex.values()) {
    if (!partialGraphIncludesFileName(identity, entry.fileName)) {
      continue;
    }
    const includeRefsEntry = await readGraphIncludeRefsEntryAsync(
      entry.fileName,
      identity.settings,
    );
    addSource({
      uri: entry.uri,
      fileName: entry.fileName,
      includeRefs: includeRefsEntry?.includeRefs ?? [],
      exists: true,
    });
    if (sources.size >= graphPartialMaxDocuments) {
      break;
    }
  }
  return [...sources.values()];
}

function partialGraphIncludesOpenDocument(
  identity: AspGraphCommandRequestIdentity,
  uri: string,
): boolean {
  return (
    isClassicAspGraphUri(uri) && partialGraphIncludesFileName(identity, graphFileNameFromUri(uri))
  );
}

function partialGraphIncludesFileName(
  identity: AspGraphCommandRequestIdentity,
  fileName: string,
): boolean {
  if (identity.scope === "workspace") {
    return true;
  }
  if (!identity.uri?.startsWith("file://")) {
    return false;
  }
  const rootFileName = graphFileNameFromUri(identity.uri);
  return identity.scope === "document"
    ? graphFileKey(rootFileName) === graphFileKey(fileName)
    : isFileInDirectoryOrEqual(fileName, rootFileName);
}

async function addPartialGraphSourceAsync(
  state: AspGraphBuildState,
  source: PartialGraphSource,
  settings: AspSettings,
): Promise<void> {
  addFileGraphNode(state, source.fileName, source.exists);
  for (const include of source.includeRefs) {
    const resolved = await resolveIncludePathDetailsAsync(
      source.uri,
      include.path,
      include.mode,
      settings,
    );
    const targetFileName = normalizeFileName(resolved.fileName);
    addFileGraphNode(state, targetFileName, resolved.exists);
    addAspGraphLink(state, {
      source: fileGraphNodeId(source.fileName),
      target: fileGraphNodeId(targetFileName),
      kind: "include",
      label: include.mode === "virtual" ? `virtual ${include.path}` : include.path,
      ranges: [{ uri: source.uri, range: include.range }],
      include: {
        path: include.path,
        mode: include.mode,
        exists: resolved.exists,
        resolvedUri: pathToFileUri(targetFileName),
        actualPath: resolved.actualPath
          ? graphDisplayFileName(state, resolved.actualPath)
          : undefined,
        pathCaseMatches: resolved.pathCaseMatches,
      },
    });
  }
}

function capAspGraphPayloadForWebview(
  payload: AspGraphPayload,
  settings: AspSettings,
): AspGraphPayload {
  const maxNodes = normalizeGraphSettings(settings).maxNodes ?? defaultGraphMaxNodes;
  if (payload.nodes.length <= maxNodes) {
    return payload;
  }
  const degreeById = new Map<string, number>();
  for (const link of payload.links) {
    degreeById.set(link.source, (degreeById.get(link.source) ?? 0) + link.count);
    degreeById.set(link.target, (degreeById.get(link.target) ?? 0) + link.count);
  }
  const ranked = payload.nodes
    .map((node, index) => ({ node, index, score: graphPayloadNodeScore(node, degreeById) }))
    .sort((left, right) => right.score - left.score || left.index - right.index)
    .slice(0, maxNodes);
  const keptIds = new Set(ranked.map((item) => item.node.id));
  const links = payload.links.filter(
    (link) => keptIds.has(link.source) && keptIds.has(link.target),
  );
  const reason = payload.truncated?.reason
    ? `${payload.truncated.reason}; nodes>${maxNodes}`
    : `nodes>${maxNodes}`;
  return {
    ...payload,
    nodes: ranked.map((item) => item.node),
    links,
    settings: { ...(payload.settings ?? graphPayloadSettings(settings)), maxNodes },
    truncated: {
      reason,
      nodes: payload.truncated?.nodes ?? payload.nodes.length,
      links: payload.truncated?.links ?? payload.links.length,
    },
  };
}

function graphPayloadNodeScore(
  node: AspGraphNode,
  degreeById: ReadonlyMap<string, number>,
): number {
  let score = degreeById.get(node.id) ?? 0;
  if (node.isRoot === true) {
    score += 1_000_000;
  }
  if (node.kind === "missingInclude" || node.kind === "vbUnresolved") {
    score += 100_000;
  }
  if (node.implicitGlobalCandidate === true) {
    score += 10_000;
  }
  if (node.kind === "file") {
    score += 1_000;
  }
  return score;
}

async function exportAnalysisExcelForCommand(
  argument: unknown,
  token?: GraphCancellationToken,
): Promise<{ ok: true; targetPath: string }> {
  const targetPath = exportAnalysisExcelTargetPath(argument);
  if (!targetPath) {
    throw new ResponseError(ErrorCodes.InvalidParams, "targetPath is required.");
  }
  const scope = graphCommandScope(argument);
  const uri = graphCommandUri(argument);
  const settings = uri ? cachedSettings(uri) : globalSettings;
  const excelSettings = settings.excel ?? normalizeExcelSettings(settings);
  const fileUris = exportAnalysisExcelFileUris(argument);
  const includeGlobs = exportAnalysisExcelStringArrayArgument(argument, "includeGlobs");
  const excludeGlobs = exportAnalysisExcelStringArrayArgument(argument, "excludeGlobs");
  const respectGitIgnore = exportAnalysisExcelOptionalBooleanArgument(argument, "respectGitIgnore");
  const includeRelatedIncludeTreesForUnresolved = exportAnalysisExcelBooleanArgument(
    argument,
    "includeRelatedIncludeTreesForUnresolved",
    excelSettings.includeRelatedIncludeTreesForUnresolved !== false,
  );
  const skipTypeInference = exportAnalysisExcelBooleanArgument(
    argument,
    "skipTypeInference",
    excelSettings.skipTypeInference === true,
  );
  const task = beginProgressTask("analyzing", "excel.graph", {
    current: 0,
    total: 3,
    detail: uri ? progressFileLabelFromUri(uri) : targetPath,
    cancellable: true,
  });
  const cancellation = progressCancellation(task, analysisCancellationFromToken(token));
  try {
    const fileFilter =
      fileUris.length > 0
        ? undefined
        : await createWorkspaceFileNameFilterAsync(settings, {
            includeGlobs,
            excludeGlobs,
            respectGitIgnore,
            selectedUri: uri,
          });
    const outputLimits = {
      maxDocuments: excelSettings.maxDocuments ?? defaultExcelMaxDocuments,
      maxTextLength: excelSettings.maxTextLength ?? defaultExcelMaxTextLength,
    };
    const graph =
      fileUris.length > 0
        ? await buildAspGraphFromExcelFileUrisAsync(fileUris, settings, cancellation, {
            includeAnalysisTypeDetails: !skipTypeInference,
            outputLimits,
            progress: task,
          })
        : await buildAspGraphForCommand(
            {
              scope,
              uri,
              activeDocument: exportAnalysisExcelActiveDocument(argument),
              includeRelatedIncludeTreesForUnresolved,
              forceRelatedIncludeTreeAnalysis: includeRelatedIncludeTreesForUnresolved,
              includeAnalysisTypeDetails: !skipTypeInference,
              fileFilter,
              maxDocuments: outputLimits.maxDocuments,
              maxTextLength: outputLimits.maxTextLength,
              includeTreeMaxDocuments:
                excelSettings.includeTreeMaxDocuments ?? defaultExcelIncludeTreeMaxDocuments,
              includeTreeMaxTextLength:
                excelSettings.includeTreeMaxTextLength ?? defaultExcelIncludeTreeMaxTextLength,
            },
            tokenFromAnalysisCancellation(cancellation),
          );
    const excelLocale = analysisExcelLocale(settings, excelSettings.locale);
    task.update({ label: "excel.sheets", current: 1, total: 3, detail: targetPath });
    throwIfGraphCancelled(cancellation);
    const sheets = await createAnalysisExcelSheetsAsync(graph, excelLocale, {
      generatedAt: new Date(),
      targetUri: uri,
      settings: {
        excelLocale: excelSettings.locale ?? "auto",
        includeRelatedIncludeTreesForUnresolved,
        forceRelatedIncludeTreeAnalysis: includeRelatedIncludeTreesForUnresolved,
        skipTypeInference,
        includeAnalysisTypeDetails: !skipTypeInference,
        maxDocuments: excelSettings.maxDocuments ?? defaultExcelMaxDocuments,
        maxTextLength: excelSettings.maxTextLength ?? defaultExcelMaxTextLength,
        includeTreeMaxDocuments:
          excelSettings.includeTreeMaxDocuments ?? defaultExcelIncludeTreeMaxDocuments,
        includeTreeMaxTextLength:
          excelSettings.includeTreeMaxTextLength ?? defaultExcelIncludeTreeMaxTextLength,
        analysisFileCount: fileUris.length > 0 ? fileUris.length : undefined,
        includeGlobs,
        excludeGlobs,
        respectGitIgnore,
      },
      progress: (event) =>
        task.update({
          label: event.label,
          current: event.current,
          total: event.total,
          detail: event.detail,
          activeItems: event.activeItems,
        }),
      yieldControl: yieldToEventLoop,
    });
    task.update({ label: "excel.file", current: 2, total: 3, detail: targetPath });
    await writeAnalysisExcelWorkbookFile(sheets, {
      filename: targetPath,
      progress: (event) =>
        task.update({
          label: event.label,
          current: event.current,
          total: event.total,
          detail: event.detail,
          activeItems: event.activeItems,
        }),
      yieldControl: yieldToEventLoop,
    });
    task.update({
      label: "excel.file",
      current: 3,
      total: 3,
      detail: targetPath,
      activeItems: [],
    });
    return { ok: true, targetPath };
  } finally {
    task.end();
  }
}

function exportAnalysisExcelTargetPath(argument: unknown): string | undefined {
  if (!argument || typeof argument !== "object" || !("targetPath" in argument)) {
    return undefined;
  }
  const targetPath = (argument as { targetPath?: unknown }).targetPath;
  return typeof targetPath === "string" && targetPath.length > 0 ? targetPath : undefined;
}

function exportAnalysisExcelFileUris(argument: unknown): string[] {
  if (!argument || typeof argument !== "object" || !("fileUris" in argument)) {
    return [];
  }
  const fileUris = (argument as { fileUris?: unknown }).fileUris;
  if (!Array.isArray(fileUris)) {
    return [];
  }
  return fileUris.filter((uri): uri is string => typeof uri === "string" && uri.length > 0);
}

function exportAnalysisExcelStringArrayArgument(
  argument: unknown,
  key: string,
): string[] | undefined {
  if (!argument || typeof argument !== "object" || !(key in argument)) {
    return undefined;
  }
  const value = (argument as Record<string, unknown>)[key];
  if (!Array.isArray(value)) {
    return undefined;
  }
  const items = value.filter(
    (item): item is string => typeof item === "string" && item.trim().length > 0,
  );
  return items.length > 0 ? items.map((item) => item.trim()) : [];
}

function exportAnalysisExcelActiveDocument(argument: unknown): unknown {
  return argument && typeof argument === "object" && "activeDocument" in argument
    ? (argument as { activeDocument?: unknown }).activeDocument
    : undefined;
}

function exportAnalysisExcelBooleanArgument(
  argument: unknown,
  key: string,
  fallback: boolean,
): boolean {
  if (!argument || typeof argument !== "object" || !(key in argument)) {
    return fallback;
  }
  return (argument as Record<string, unknown>)[key] === true;
}

function exportAnalysisExcelOptionalBooleanArgument(
  argument: unknown,
  key: string,
): boolean | undefined {
  if (!argument || typeof argument !== "object" || !(key in argument)) {
    return undefined;
  }
  const value = (argument as Record<string, unknown>)[key];
  return typeof value === "boolean" ? value : undefined;
}

function analysisExcelLocale(
  settings: AspSettings,
  configured: AspSettings["locale"],
): AspGraphLocale {
  const locale =
    configured === "auto" || configured === undefined ? settings.resolvedLocale : configured;
  return locale === "ja" ? "ja" : "en";
}

async function previewWorkspaceFilesForCommand(
  argument: unknown,
  token?: GraphCancellationToken,
): Promise<WorkspaceFilePreviewPayload> {
  const workspaceSettings = (globalSettings.workspace ??
    normalizeWorkspaceSettings(globalSettings)) as NonNullable<AspSettings["workspace"]>;
  const includeGlobs = previewWorkspaceStringArrayArgument(
    argument,
    "includeGlobs",
    workspaceSettings.includes ?? defaultWorkspaceIncludes,
    true,
  );
  const excludeGlobs = previewWorkspaceStringArrayArgument(
    argument,
    "excludeGlobs",
    workspaceSettings.excludes ?? [],
    true,
  );
  const respectGitIgnore = previewWorkspaceBooleanArgument(
    argument,
    "respectGitIgnore",
    workspaceSettings.respectGitIgnore === true,
  );
  const showUnmatched = previewWorkspaceBooleanArgument(argument, "showUnmatched", true);
  const maxFiles = previewWorkspacePositiveIntegerArgument(
    argument,
    "maxFiles",
    workspaceSettings.maxIndexFiles ?? defaultMaxIndexFiles,
  );
  const task = beginProgressTask("loading", "workspace.previewFiles", {
    cancellable: true,
  });
  const cancellation = progressCancellation(task, analysisCancellationFromToken(token));
  try {
    return await previewWorkspaceFilesAsync(
      globalSettings,
      includeGlobs,
      excludeGlobs,
      respectGitIgnore,
      showUnmatched,
      maxFiles,
      cancellation,
      task,
    );
  } finally {
    task.end();
  }
}

function previewWorkspaceStringArrayArgument(
  argument: unknown,
  key: string,
  fallback: string[],
  allowEmpty = false,
): string[] {
  if (!argument || typeof argument !== "object" || !(key in argument)) {
    return [...fallback];
  }
  const value = (argument as Record<string, unknown>)[key];
  if (!Array.isArray(value)) {
    return [...fallback];
  }
  const items = value
    .filter((item): item is string => typeof item === "string" && item.trim().length > 0)
    .map((item) => item.trim());
  return items.length > 0 || allowEmpty ? items : [...fallback];
}

function previewWorkspaceBooleanArgument(
  argument: unknown,
  key: string,
  fallback: boolean,
): boolean {
  if (!argument || typeof argument !== "object" || !(key in argument)) {
    return fallback;
  }
  return (argument as Record<string, unknown>)[key] === true;
}

function previewWorkspacePositiveIntegerArgument(
  argument: unknown,
  key: string,
  fallback: number,
): number {
  if (!argument || typeof argument !== "object" || !(key in argument)) {
    return fallback;
  }
  const value = (argument as Record<string, unknown>)[key];
  return typeof value === "number" && Number.isFinite(value) && value > 0
    ? Math.floor(value)
    : fallback;
}

async function previewWorkspaceFilesAsync(
  settings: AspSettings,
  includeGlobs: string[],
  excludeGlobs: string[],
  respectGitIgnore: boolean,
  showUnmatched: boolean,
  maxFiles: number,
  cancellation: AnalysisCancellation,
  progress?: AspLspProgressTaskHandle,
): Promise<WorkspaceFilePreviewPayload> {
  const roots = workspaceIndexRoots().sort((left, right) =>
    normalizeFileName(left).localeCompare(normalizeFileName(right)),
  );
  const previewRoots: WorkspaceFilePreviewRoot[] = [];
  const globStats = createWorkspacePreviewGlobStats(includeGlobs, excludeGlobs);
  let files = 0;
  let visibleFiles = 0;
  let totalBytes = 0;
  let truncated = false;
  for (const root of roots) {
    throwIfGraphCancelled(cancellation);
    const normalizedRoot = normalizeFileName(root);
    progress?.update({
      label: "workspace.previewFiles",
      current: visibleFiles,
      total: maxFiles,
      detail: progressFileLabel(normalizedRoot),
    });
    const filter = await createWorkspaceScanFilterWithPatterns(
      normalizedRoot,
      includeGlobs,
      excludeGlobs,
      respectGitIgnore,
    );
    const rootFiles = await previewWorkspaceRootFilesAsync(filter, settings, {
      maxFiles,
      get files() {
        return visibleFiles;
      },
      addFile(file): void {
        visibleFiles += 1;
        if (file.matchesFilter) {
          files += 1;
          totalBytes += file.size;
        }
      },
      cancellation,
      globStats,
      progress,
      showUnmatched,
    });
    if (rootFiles.truncated) {
      truncated = true;
    }
    previewRoots.push({
      uri: pathToFileUri(normalizedRoot),
      fileName: normalizedRoot,
      name: path.basename(normalizedRoot) || normalizedRoot,
      files: rootFiles.files,
    });
  }
  return {
    includeGlobs,
    excludeGlobs,
    globStats,
    respectGitIgnore,
    roots: previewRoots,
    showUnmatched,
    stats: {
      files,
      totalBytes,
    },
    truncated: truncated ? { reason: `files>${maxFiles}` } : undefined,
  };
}

async function previewWorkspaceRootFilesAsync(
  filter: WorkspaceScanFilter,
  settings: AspSettings,
  state: {
    maxFiles: number;
    readonly files: number;
    addFile(file: WorkspaceFilePreviewFile): void;
    cancellation: AnalysisCancellation;
    globStats: WorkspaceFilePreviewGlobStats;
    progress?: AspLspProgressTaskHandle;
    showUnmatched: boolean;
  },
): Promise<{ files: WorkspaceFilePreviewFile[]; truncated: boolean }> {
  const stat = await fsGateway.statAsync(filter.root);
  if (!stat?.isDirectory()) {
    return { files: [], truncated: false };
  }
  const concurrency = includeReadConcurrency(settings);
  const directories = [filter.root];
  const files: WorkspaceFilePreviewFile[] = [];
  let truncated = false;
  while (directories.length > 0) {
    throwIfGraphCancelled(state.cancellation);
    if (state.files >= state.maxFiles) {
      truncated = true;
      break;
    }
    const batch = directories.splice(0, concurrency);
    const batches = await mapWithConcurrency(batch, concurrency, async (directory) => {
      const listing = await fsGateway.readdirAsync(directory);
      const childDirectories: string[] = [];
      const childFiles: Array<{ fileName: string; matchesFilter: boolean }> = [];
      for (const entry of [...(listing?.entries ?? [])].sort((left, right) =>
        left.name.localeCompare(right.name),
      )) {
        throwIfGraphCancelled(state.cancellation);
        const fullPath = path.join(directory, entry.name);
        if (entry.isDirectory()) {
          if (workspacePreviewShouldVisitDirectory(filter, fullPath)) {
            childDirectories.push(fullPath);
          }
        } else if (entry.isFile() && isAspWorkspaceFile(fullPath)) {
          const relative = workspaceRelativePath(filter.root, normalizeFileName(fullPath));
          if (
            !relative ||
            workspaceGitIgnoreRulesIgnorePath(filter.gitIgnoreRules, relative, false)
          ) {
            continue;
          }
          const includeIndexes = workspacePatternMatchIndexes(filter.includes, relative, false);
          const includeMatched = includeIndexes.length > 0;
          for (const index of includeIndexes) {
            state.globStats.include[index].files += 1;
          }
          const excludeIndexes = includeMatched
            ? workspacePatternExcludesPathIndexes(filter.excludes, relative, false)
            : [];
          for (const index of excludeIndexes) {
            state.globStats.exclude[index].files += 1;
          }
          const matchesFilter = includeMatched && excludeIndexes.length === 0;
          if (matchesFilter || state.showUnmatched) {
            childFiles.push({ fileName: fullPath, matchesFilter });
          }
        }
      }
      return { childDirectories, childFiles };
    });
    const childFiles: Array<{ fileName: string; matchesFilter: boolean }> = [];
    for (const item of batches) {
      directories.push(...item.childDirectories);
      childFiles.push(...item.childFiles);
    }
    for (const candidate of childFiles.sort((left, right) =>
      left.fileName.localeCompare(right.fileName),
    )) {
      throwIfGraphCancelled(state.cancellation);
      if (state.files >= state.maxFiles) {
        truncated = true;
        break;
      }
      const fileName = candidate.fileName;
      const fileStat = await fsGateway.statAsync(fileName);
      if (!fileStat?.isFile()) {
        continue;
      }
      const normalized = normalizeFileName(fileName);
      const relativePath = workspaceRelativePath(filter.root, normalized);
      if (!relativePath) {
        continue;
      }
      const file: WorkspaceFilePreviewFile = {
        uri: pathToFileUri(normalized),
        fileName: normalized,
        matchesFilter: candidate.matchesFilter,
        relativePath,
        size: fileStat.size,
        mtimeMs: fileStat.mtimeMs,
      };
      files.push(file);
      state.addFile(file);
      state.progress?.update({
        label: "workspace.previewFiles",
        current: Math.min(state.files, state.maxFiles),
        total: state.maxFiles,
        detail: progressFileLabel(normalized),
      });
    }
    await yieldToEventLoop();
  }
  return { files, truncated };
}

function createWorkspacePreviewGlobStats(
  includeGlobs: string[],
  excludeGlobs: string[],
): WorkspaceFilePreviewGlobStats {
  return {
    include: includeGlobs.map((glob) => ({ glob, files: 0 })),
    exclude: excludeGlobs.map((glob) => ({ glob, files: 0 })),
  };
}

async function buildAspGraphFromExcelFileUrisAsync(
  fileUris: string[],
  settings: AspSettings,
  cancellation: AnalysisCancellation,
  options: {
    includeAnalysisTypeDetails: boolean;
    outputLimits: VbProjectContextLimits;
    progress?: AspLspProgressTaskHandle;
  },
): Promise<AspGraphPayload> {
  const sources = await excelGraphDocumentSourcesFromUrisAsync(
    fileUris,
    settings,
    cancellation,
    options.progress,
  );
  if (sources.length === 0) {
    throw new ResponseError(ErrorCodes.InvalidParams, "fileUris contains no analyzable files.");
  }
  return graphPayloadFromDocumentSourcesAsync("workspace", sources, settings, {
    cancellation,
    includeAnalysisTypeDetails: options.includeAnalysisTypeDetails,
    outputLimits: options.outputLimits,
    progress: options.progress,
    operationCache: new Map(),
  });
}

async function excelGraphDocumentSourcesFromUrisAsync(
  fileUris: string[],
  settings: AspSettings,
  cancellation: AnalysisCancellation,
  progress?: AspLspProgressTaskHandle,
): Promise<AspGraphDocumentSource[]> {
  const seen = new Set<string>();
  const sources: AspGraphDocumentSource[] = [];
  progress?.update({
    label: "graph.prepareDocuments",
    current: 0,
    total: fileUris.length,
    activeItems: [],
  });
  for (const uri of fileUris) {
    throwIfGraphCancelled(cancellation);
    const source = await excelGraphDocumentSourceFromUriAsync(uri, settings, cancellation);
    if (!source) {
      continue;
    }
    const key = graphFileKey(source.fileName);
    if (seen.has(key)) {
      continue;
    }
    seen.add(key);
    sources.push(source);
    progress?.update({
      label: "graph.prepareDocuments",
      current: sources.length,
      total: fileUris.length,
      detail: progressFileLabel(source.fileName),
    });
  }
  return sources;
}

async function excelGraphDocumentSourceFromUriAsync(
  uri: string,
  settings: AspSettings,
  cancellation: AnalysisCancellation,
): Promise<AspGraphDocumentSource | undefined> {
  if (!uri.startsWith("file://")) {
    return undefined;
  }
  let fileName: string;
  try {
    fileName = graphFileNameFromUri(uri);
  } catch {
    return undefined;
  }
  if (!isAspWorkspaceFile(fileName) || !workspaceRootForFileName(fileName)) {
    return undefined;
  }
  const openDocument = openDocumentForFileName(fileName);
  if (openDocument) {
    const cached = await ensureFreshCachedDocumentAsync(openDocument);
    const graphDocument = await graphDocumentFromCachedAsync(
      cached,
      cachedSettings(cached.source.uri),
    );
    return graphPayloadDocumentSourceFromDocument(graphDocument);
  }
  const stat = await fsGateway.statAsync(fileName);
  if (!stat?.isFile()) {
    return undefined;
  }
  const canonicalUri = pathToFileUri(fileName);
  const entry: WorkspaceIndexedDocument = {
    uri: canonicalUri,
    fileName,
    mtimeMs: stat.mtimeMs,
    size: stat.size,
  };
  return {
    uri: canonicalUri,
    fileName,
    textLength: stat.size,
    load: async () => {
      throwIfGraphCancelled(cancellation);
      const cached = await cachedFromIndexedAsync(entry, cachedSettings(canonicalUri));
      throwIfGraphCancelled(cancellation);
      return graphDocumentFromCachedAsync(cached, cachedSettings(canonicalUri));
    },
  };
}

function collectDocumentGraphDocumentsAsync(
  root: CachedDocument,
  settings: AspSettings,
  cancellation: AnalysisCancellation = neverCancelled,
): Promise<AspGraphDocument[]> {
  return aspGraphBuildService.collectDocumentGraphDocumentsAsync(root, settings, cancellation);
}

function collectIncludeTreeGraphDocumentsAsync(
  root: AspGraphDocument,
  settings: AspSettings,
  cancellation: AnalysisCancellation = neverCancelled,
  options: CollectIncludeTreeGraphDocumentsOptions = {},
): Promise<AspGraphDocument[]> {
  return aspGraphBuildService.collectIncludeTreeGraphDocumentsAsync(
    root,
    settings,
    cancellation,
    options,
  );
}

function collectRelatedIncludeTreeOwnerGraphDocumentsAsync(
  rootDocuments: AspGraphDocument[],
  settings: AspSettings,
  cancellation: AnalysisCancellation,
  options: CollectRelatedIncludeTreeOwnerGraphDocumentsOptions = {},
): Promise<AspGraphDocument[]> {
  return aspGraphBuildService.collectRelatedIncludeTreeOwnerGraphDocumentsAsync(
    rootDocuments,
    settings,
    cancellation,
    options,
  );
}

async function cachedDocumentForGraphAsync(uri: string): Promise<CachedDocument | undefined> {
  const document = openDocumentForUri(uri);
  if (document) {
    return ensureFreshCachedDocumentAsync(document);
  }
  const fileName = graphFileNameFromUri(uri);
  const openDocument = documents
    .all()
    .find((candidate) => graphFileKeyFromUri(candidate.uri) === graphFileKey(fileName));
  if (openDocument) {
    return ensureFreshCachedDocumentAsync(openDocument);
  }
  if (!isClassicAspGraphUri(uri)) {
    return undefined;
  }
  const stat = await fsGateway.statAsync(fileName);
  if (!stat?.isFile()) {
    return undefined;
  }
  const canonicalUri = pathToFileUri(fileName);
  return cachedFromIndexedAsync(
    { uri: canonicalUri, fileName, mtimeMs: stat.mtimeMs, size: stat.size },
    cachedSettings(canonicalUri),
  );
}

async function graphDocumentsNeedRelatedIncludeTreeAnalysisAsync(
  documentsForGraph: AspGraphDocument[],
  settings: AspSettings,
  cancellation: AnalysisCancellation,
  operationCache?: GraphFileIndexOperationCache,
  progress?: AspLspProgressTaskHandle,
): Promise<boolean> {
  const externalSymbols = createAspGraphExternalIndex(
    getVbscriptGraphExternalSymbols(settings, vbBuiltinRuntimeForUri(documentsForGraph[0]?.uri)),
  );
  const documents = uniqueAspGraphDocuments(documentsForGraph);
  progress?.update({
    label: "graph.checkRelatedIncludes",
    current: 0,
    total: documents.length,
    activeItems: [],
  });
  await yieldToEventLoop();
  const indexedDocuments = await mapWithConcurrency(
    documents,
    analysisConcurrency(settings),
    async (document): Promise<VbSymbolIndex> => {
      throwIfGraphCancelled(cancellation);
      const graphIndex = await graphFileIndexForDocumentAsync(document, settings, {
        operationCache,
      });
      throwIfGraphCancelled(cancellation);
      return graphIndex.vbSymbolIndex;
    },
    progress
      ? progressMapHooks(progress, (document) => progressFileLabel(document.fileName))
      : undefined,
  );
  progress?.update({
    label: "graph.checkRelatedIncludes",
    current: documents.length,
    total: documents.length,
    activeItems: [],
  });
  await yieldToEventLoop();
  return indexedDocuments.some((index) =>
    vbSymbolIndexNeedsRelatedIncludeTreeAnalysis(index, externalSymbols),
  );
}

function vbSymbolIndexNeedsRelatedIncludeTreeAnalysis(
  index: VbSymbolIndex,
  externalSymbols: AspGraphExternalIndex,
): boolean {
  return (
    index.declarations.some(
      (declaration) =>
        declaration.implicitGlobal === true || declaration.implicitGlobalCandidate === true,
    ) ||
    index.references.some(
      (reference) =>
        !reference.resolvedId &&
        !reference.memberName &&
        !externalSymbols.byName.has(reference.name.toLowerCase()),
    ) ||
    index.callSites.some(
      (callSite) =>
        !callSite.resolvedId &&
        !callSite.memberName &&
        !externalSymbols.byName.has(callSite.name.toLowerCase()),
    )
  );
}

async function prefetchIncludeRefsForOwnerAsync(
  ownerUri: string,
  includeRefs: readonly AspInclude[],
  settings: AspSettings,
): Promise<void> {
  await mapWithConcurrency(includeRefs, includeReadConcurrency(settings), async (include) => {
    const resolved = await resolveIncludePathDetailsAsync(
      ownerUri,
      include.path,
      include.mode,
      settings,
    );
    if (!resolved.exists) {
      return;
    }
    await Promise.all([
      fileSizeAsync(resolved.fileName, settings),
      includeDocumentLoader.readIncludeRefsAsync(resolved.fileName, settings, { allowRead: true }),
    ]).catch(() => undefined);
  });
}

async function graphIncludeRefsForDocumentAsync(
  document: AspGraphDocument,
  settings: AspSettings,
): Promise<AspInclude[]> {
  const includeRefsEntry = await readGraphIncludeRefsEntryAsync(document.fileName, settings);
  if (includeRefsEntry && sameDiskAnalysisSource(includeRefsEntry.source, document.source)) {
    return includeRefsEntry.includeRefs;
  }
  return extractAspIncludeRefs(document.text);
}

async function collectIncomingIncludeGraphDocumentsAsync(
  targetFileNames: Set<string>,
  settings: AspSettings,
  cancellation: AnalysisCancellation,
  options: {
    excludedFileKeys?: Set<string>;
    fileFilter?: (fileName: string) => boolean;
    token?: GraphCancellationToken;
    progress?: AspLspProgressTaskHandle;
  } = {},
): Promise<AspGraphDocument[]> {
  const targets = new Set([...targetFileNames].map(fileIdentityKeyFromFileName));
  if (targets.size === 0) {
    return [];
  }
  await ensureWorkspaceIndexAsync(
    settings,
    options.token ?? tokenFromAnalysisCancellation(cancellation),
  );
  throwIfGraphCancelled(cancellation);
  const excludedFileKeys = options.excludedFileKeys ?? new Set<string>();
  const documentsForGraph: AspGraphDocument[] = [];
  const opened = new Set<string>();
  const concurrency = analysisConcurrency(settings);
  const openDocuments = await workspaceAnalyzableOpenDocumentsAsync(settings);
  options.progress?.update({
    label: "graph.findIncomingIncludes",
    current: 0,
    total: openDocuments.length,
    activeItems: [],
  });
  await yieldToEventLoop();
  const openGraphDocuments = await mapWithConcurrency(
    openDocuments,
    concurrency,
    async (document): Promise<AspGraphDocument | undefined> => {
      throwIfGraphCancelled(cancellation);
      const fileName = graphFileNameFromUri(document.uri);
      if (
        excludedFileKeys.has(graphFileKey(fileName)) ||
        (options.fileFilter && !options.fileFilter(fileName))
      ) {
        return undefined;
      }
      const cached = await ensureFreshCachedDocumentAsync(document);
      const graphDocument = await graphDocumentFromCachedAsync(
        cached,
        cachedSettings(document.uri),
      );
      return (await graphDocumentDirectlyIncludesAnyTargetAsync(
        graphDocument,
        targets,
        settings,
        cancellation,
      ))
        ? graphDocument
        : undefined;
    },
    options.progress
      ? progressMapHooks(options.progress, (document) => progressFileLabelFromUri(document.uri))
      : undefined,
  );
  for (const graphDocument of openGraphDocuments) {
    if (!graphDocument) {
      continue;
    }
    opened.add(graphFileKey(graphDocument.fileName));
    documentsForGraph.push(graphDocument);
  }
  const indexedEntries = await incomingIncludeIndexedEntriesAsync(
    targetFileNames,
    opened,
    excludedFileKeys,
    options.fileFilter,
    settings,
    cancellation,
    options.progress,
  );
  options.progress?.update({
    label: "graph.filterIncomingIncludes",
    current: 0,
    total: indexedEntries.length,
    activeItems: [],
  });
  await yieldToEventLoop();
  const indexedGraphDocuments = await mapWithConcurrency(
    indexedEntries,
    concurrency,
    async (entry): Promise<AspGraphDocument | undefined> => {
      throwIfGraphCancelled(cancellation);
      if (
        !(await indexedEntryDirectlyIncludesAnyTargetAsync(entry, targets, settings, cancellation))
      ) {
        return undefined;
      }
      const cached = await cachedFromIndexedAsync(entry, cachedSettings(entry.uri));
      return graphDocumentFromCachedAsync(cached, cachedSettings(entry.uri));
    },
    options.progress
      ? progressMapHooks(options.progress, (entry) => progressFileLabel(entry.fileName))
      : undefined,
  );
  documentsForGraph.push(
    ...indexedGraphDocuments.filter(
      (document): document is AspGraphDocument => document !== undefined,
    ),
  );
  return documentsForGraph;
}

async function incomingIncludeIndexedEntriesAsync(
  targetFileNames: Set<string>,
  opened: Set<string>,
  excludedFileKeys: Set<string>,
  fileFilter: ((fileName: string) => boolean) | undefined,
  settings: AspSettings,
  cancellation: AnalysisCancellation,
  progress?: AspLspProgressTaskHandle,
): Promise<WorkspaceIndexedDocument[]> {
  const indexedEntries = [...workspaceIndex.values()].filter(
    (entry) =>
      !opened.has(graphFileKey(entry.fileName)) &&
      !excludedFileKeys.has(graphFileKey(entry.fileName)) &&
      (fileFilter === undefined || fileFilter(entry.fileName)),
  );
  if (settings.graph?.useReverseIncludeIndex === false) {
    return indexedEntries;
  }
  progress?.update({
    label: "graph.reverseIncludeIndex",
    current: 0,
    total: indexedEntries.length,
    activeItems: [],
  });
  await ensureWorkspaceIncludeGraphAsync(settings, cancellation);
  throwIfGraphCancelled(cancellation);
  const candidateKeys = new Set(
    workspaceIncludeGraph.candidatesForTargets(targetFileNames).map(graphFileKey),
  );
  const candidates = await mapWithConcurrency(
    indexedEntries,
    includeReadConcurrency(settings),
    async (entry) => {
      throwIfGraphCancelled(cancellation);
      if (candidateKeys.has(graphFileKey(entry.fileName))) {
        return entry;
      }
      const graphEntry = workspaceIncludeGraph.get(entry.fileName);
      if (
        !graphEntry ||
        !sameDiskAnalysisSource(graphEntry.source, diskAnalysisSourceMetadata(entry))
      ) {
        return entry;
      }
      if (cacheFreshness(settings) === "metadata") {
        const stat = await fsGateway.statAsync(entry.fileName);
        if (
          !stat?.isFile() ||
          stat.mtimeMs !== graphEntry.source.mtimeMs ||
          stat.size !== graphEntry.source.size
        ) {
          return entry;
        }
      }
      return undefined;
    },
    progress ? progressMapHooks(progress, (entry) => progressFileLabel(entry.fileName)) : undefined,
  );
  return candidates.filter((entry): entry is WorkspaceIndexedDocument => entry !== undefined);
}

async function indexedEntryDirectlyIncludesAnyTargetAsync(
  entry: WorkspaceIndexedDocument,
  targetFileNames: Set<string>,
  settings: AspSettings,
  cancellation: AnalysisCancellation,
): Promise<boolean> {
  const includeRefsEntry = await readGraphIncludeRefsEntryAsync(entry.fileName, settings);
  const includeRefs =
    includeRefsEntry &&
    sameDiskAnalysisSource(includeRefsEntry.source, diskAnalysisSourceMetadata(entry))
      ? includeRefsEntry.includeRefs
      : extractAspIncludeRefs(await readTextFileAsync(entry.fileName, settings.legacyEncoding));
  throwIfGraphCancelled(cancellation);
  return includeRefsDirectlyIncludeAnyTarget(entry.uri, includeRefs, targetFileNames, settings);
}

async function graphDocumentDirectlyIncludesAnyTargetAsync(
  document: AspGraphDocument,
  targetFileNames: Set<string>,
  settings: AspSettings,
  cancellation: AnalysisCancellation,
): Promise<boolean> {
  const includeRefs = await graphIncludeRefsForDocumentAsync(document, settings);
  throwIfGraphCancelled(cancellation);
  return includeRefsDirectlyIncludeAnyTarget(document.uri, includeRefs, targetFileNames, settings);
}

async function includeRefsDirectlyIncludeAnyTarget(
  ownerUri: string,
  includeRefs: AspInclude[],
  targetFileNames: Set<string>,
  settings: AspSettings,
): Promise<boolean> {
  for (const include of includeRefs) {
    const resolved = await resolveIncludePathDetailsAsync(
      ownerUri,
      include.path,
      include.mode,
      settings,
    );
    if (resolved.exists && targetFileNames.has(fileIdentityKeyFromFileName(resolved.fileName))) {
      return true;
    }
  }
  return false;
}

async function readGraphIncludeRefsEntryAsync(
  fileName: string,
  settings: AspSettings,
): Promise<IncludeRefsCacheEntry | undefined> {
  return includeDocumentLoader
    .readIncludeRefsAsync(fileName, settings, { allowRead: true })
    .catch((error) => {
      logDiskAnalysisCacheError("graphIncludeRefs.read", error);
      return undefined;
    });
}

async function graphPayloadFromDocumentsAsync(
  scope: AspGraphScope,
  documentsForGraph: AspGraphDocument[],
  settings: AspSettings,
  options: {
    rootUri?: string;
    truncated?: AspGraphPayload["truncated"];
    cancellation?: AnalysisCancellation;
    includeAnalysisTypeDetails?: boolean;
    fileFilter?: (fileName: string) => boolean;
    outputLimits?: VbProjectContextLimits;
    progress?: AspLspProgressTaskHandle;
    operationCache?: GraphFileIndexOperationCache;
  } = {},
): Promise<AspGraphPayload> {
  return graphPayloadFromDocumentSourcesAsync(
    scope,
    documentsForGraph.map(graphPayloadDocumentSourceFromDocument),
    settings,
    options,
  );
}

async function graphPayloadFromDocumentSourcesAsync(
  scope: AspGraphScope,
  sourcesForGraph: AspGraphDocumentSource[],
  settings: AspSettings,
  options: {
    rootUri?: string;
    truncated?: AspGraphPayload["truncated"];
    cancellation?: AnalysisCancellation;
    includeAnalysisTypeDetails?: boolean;
    fileFilter?: (fileName: string) => boolean;
    outputLimits?: VbProjectContextLimits;
    progress?: AspLspProgressTaskHandle;
    operationCache?: GraphFileIndexOperationCache;
  } = {},
): Promise<AspGraphPayload> {
  const cancellation = options.cancellation ?? neverCancelled;
  throwIfGraphCancelled(cancellation);
  const { sources, truncated } = limitAspGraphPayloadDocumentSources(
    uniqueAspGraphDocumentSources(sourcesForGraph),
    options.outputLimits ?? graphOutputLimits(settings),
    options.truncated,
  );
  const state = createAspGraphBuildState(settings, options.rootUri, truncated, {
    includeAnalysisTypeDetails: options.includeAnalysisTypeDetails === true,
  });
  for (const source of sources) {
    throwIfGraphCancelled(cancellation);
    addFileGraphNode(state, source.fileName, true);
  }
  const progress = options.progress;
  const graphProgressTotal = Math.max(1, sources.length * 5 + 2);
  let graphProgressCurrent = 0;
  const graphProgressActiveItems = new Map<string, string>();
  const updateGraphProgress = (
    label: string,
    detail?: string,
    activeItems: string[] = [],
  ): void => {
    progress?.update({
      label,
      current: Math.min(graphProgressCurrent, graphProgressTotal),
      total: graphProgressTotal,
      detail,
      activeItems,
    });
  };
  const advanceGraphProgress = (label: string, detail?: string, activeItems?: string[]): void => {
    graphProgressCurrent = Math.min(graphProgressCurrent + 1, graphProgressTotal);
    updateGraphProgress(label, detail, activeItems);
  };
  const activeGraphProgressItems = (): string[] => [...graphProgressActiveItems.values()];
  const handlePipelineProgress = (event: BulkGraphIndexPipelineProgressEvent): void => {
    const activeKey = `${event.stage}:${event.index ?? "all"}`;
    const detail = event.source ? progressFileLabel(event.source.fileName) : undefined;
    if (event.phase === "start") {
      if (detail) {
        graphProgressActiveItems.set(activeKey, detail);
      }
      updateGraphProgress(
        graphPipelineProgressLabel(event.stage),
        detail,
        activeGraphProgressItems(),
      );
      return;
    }
    graphProgressActiveItems.delete(activeKey);
    advanceGraphProgress(
      graphPipelineProgressLabel(event.stage),
      detail,
      activeGraphProgressItems(),
    );
  };
  updateGraphProgress("graph.prepareDocuments");
  await yieldToEventLoop();
  const pipeline = await runSpilledGraphIndexPipeline({
    sources,
    settings,
    cancellation,
    concurrency: analysisConcurrency(settings),
    namespace: `${scope}-${Date.now().toString(36)}`,
    async indexDocument(document): Promise<AspGraphIndexedDocument> {
      throwIfGraphCancelled(cancellation);
      const graphIndex = await graphFileIndexForDocumentAsync(document, settings, {
        includeTypeHints: options.includeAnalysisTypeDetails === true,
        operationCache: options.operationCache,
      });
      throwIfGraphCancelled(cancellation);
      return { document, graphIndex };
    },
    graphFileKey,
    normalizeFileName,
    resolveIncludePathDetailsAsync,
    graphFileIndexFingerprint,
    isCancellationRequested: () => cancellation.isCancellationRequested(),
    throwIfCancelled: () => throwIfGraphCancelled(cancellation),
    logDebug: (message) => logDebugSummary(settings, message),
    onProgress: handlePipelineProgress,
    workerPool: getBulkWorkerPool(),
  });
  try {
    throwIfGraphCancelled(cancellation);
    const canonicalizedReplay =
      pipeline.bytesWritten <= graphCanonicalizedReplayMaxBytes
        ? ([] as AspGraphIndexedDocument[])
        : undefined;
    const structureStartedAt = process.hrtime.bigint();
    for await (const indexed of pipeline.scanCanonicalized()) {
      throwIfGraphCancelled(cancellation);
      canonicalizedReplay?.push(indexed);
      const detail = progressFileLabel(indexed.document.fileName);
      updateGraphProgress("graph.addStructure", detail, [detail]);
      await addDocumentStructureToAspGraphAsync(state, indexed, settings, {
        fileFilter: options.fileFilter,
      });
      advanceGraphProgress("graph.addStructure", detail);
      throwIfGraphCancelled(cancellation);
      await yieldToEventLoop();
    }
    finishDebugStep(settings, "workspace", "graph.addStructure", structureStartedAt);
    if (canonicalizedReplay) {
      logDebugSummary(
        settings,
        `[asp-lsp] asp.graph.bulk.replay.memory: files=${canonicalizedReplay.length}, bytes=${pipeline.bytesWritten}`,
      );
    } else {
      logDebugSummary(
        settings,
        `[asp-lsp] asp.graph.bulk.replay.disk: files=${pipeline.files}, bytes=${pipeline.bytesWritten}`,
      );
    }
    const usagesStartedAt = process.hrtime.bigint();
    for await (const indexed of canonicalizedReplay ?? pipeline.scanCanonicalized()) {
      throwIfGraphCancelled(cancellation);
      const detail = progressFileLabel(indexed.document.fileName);
      updateGraphProgress("graph.addUsages", detail, [detail]);
      addDocumentUsageToAspGraph(state, indexed);
      advanceGraphProgress("graph.addUsages", detail);
      await yieldToEventLoop();
    }
    finishDebugStep(settings, "workspace", "graph.addUsages", usagesStartedAt);
  } finally {
    await pipeline.dispose();
  }
  logDebugSummary(settings, `[asp-lsp] asp.graph.bulk.complete: files=${pipeline.files}`);
  updateGraphProgress("graph.finalize");
  await yieldToEventLoop();
  const finalizeStartedAt = process.hrtime.bigint();
  removeUnusedImplicitGlobalCandidateGraphDeclarations(state);
  state.stats = recomputeAspGraphStats(state.nodes.values(), state.links.values());
  finishDebugStep(settings, "workspace", "graph.finalize", finalizeStartedAt);
  advanceGraphProgress("graph.finalize");
  return {
    scope,
    rootUri: state.rootUri,
    nodes: [...state.nodes.values()],
    links: [...state.links.values()],
    settings: graphPayloadSettings(settings),
    stats: state.stats,
    truncated: state.truncated,
  };
}

function limitAspGraphPayloadDocumentSources(
  sources: AspGraphDocumentSource[],
  limits: VbProjectContextLimits,
  truncated: AspGraphPayload["truncated"] | undefined,
): { sources: AspGraphDocumentSource[]; truncated?: AspGraphPayload["truncated"] } {
  if (sources.length === 0) {
    return { sources, truncated };
  }
  const limited: AspGraphDocumentSource[] = [];
  let textLength = 0;
  let reason = truncated?.reason;
  for (const source of sources) {
    const nextTextLength = textLength + source.textLength;
    const exceedsDocumentLimit = limited.length >= limits.maxDocuments;
    const exceedsTextLimit = nextTextLength > limits.maxTextLength;
    if (exceedsDocumentLimit || (exceedsTextLimit && limited.length > 0)) {
      reason ??= exceedsDocumentLimit
        ? `documents>${limits.maxDocuments}`
        : `text>${limits.maxTextLength}`;
      break;
    }
    limited.push(source);
    textLength = nextTextLength;
    if (exceedsTextLimit) {
      reason ??= `text>${limits.maxTextLength}`;
      break;
    }
  }
  return {
    sources: limited,
    truncated: reason ? { reason } : truncated,
  };
}

function graphPipelineProgressLabel(event: BulkGraphIndexPipelineProgressEvent["stage"]): string {
  switch (event) {
    case "load":
      return "graph.loadDocuments";
    case "index":
      return "graph.indexDocuments";
    case "spill":
      return "graph.spillIndexes";
    case "canonicalize":
      return "graph.canonicalizeSymbols";
  }
}

function removeUnusedImplicitGlobalCandidateGraphDeclarations(state: AspGraphBuildState): void {
  const usedTargets = new Set(
    [...state.links.values()]
      .filter((link) => link.kind === "references" || link.kind === "assignments")
      .map((link) => link.target),
  );
  const removableIds = new Set<string>();
  for (const node of state.nodes.values()) {
    if (
      node.kind === "vbDeclaration" &&
      node.implicitGlobalCandidate === true &&
      !usedTargets.has(node.id)
    ) {
      removableIds.add(node.id);
    }
  }
  for (const id of removableIds) {
    state.nodes.delete(id);
  }
  if (removableIds.size === 0) {
    return;
  }
  for (const [id, link] of state.links) {
    if (removableIds.has(link.source) || removableIds.has(link.target)) {
      state.links.delete(id);
    }
  }
}

function createAspGraphBuildState(
  settings: AspSettings,
  rootUri?: string,
  truncated?: AspGraphPayload["truncated"],
  options: { includeAnalysisTypeDetails?: boolean } = {},
): AspGraphBuildState {
  const rootFileKey = rootUri?.startsWith("file://") ? graphFileKeyFromUri(rootUri) : undefined;
  const canonicalRootUri = rootFileKey ? pathToFileUri(rootFileKey) : rootUri;
  const workspaceRootFileNames = workspaceRoots
    .map(normalizeFileName)
    .sort((left, right) => right.length - left.length);
  return {
    nodes: new Map(),
    links: new Map(),
    declarations: new Set(),
    sourceDeclarationsByName: new Map(),
    sourceDeclarationsById: new Map(),
    sourceDeclarationFileKeysById: new Map(),
    directIncludesByOwnerKey: new Map(),
    parentIncludesByTargetKey: new Map(),
    includeReachability: undefined,
    externalSymbols: createAspGraphExternalIndex(
      getVbscriptGraphExternalSymbols(settings, vbBuiltinRuntimeForUri(rootUri)),
    ),
    includeAnalysisTypeDetails: options.includeAnalysisTypeDetails === true,
    rootUri: canonicalRootUri,
    rootFileKey,
    workspaceRootFileNames,
    truncated,
    stats: {
      files: 0,
      declarations: 0,
      references: 0,
      assignments: 0,
      calls: 0,
      unresolvedReferences: 0,
      includes: 0,
      missingIncludes: 0,
      nodes: 0,
      links: 0,
    },
  };
}

function uniqueAspGraphDocuments(documentsForGraph: AspGraphDocument[]): AspGraphDocument[] {
  const seen = new Set<string>();
  const unique: AspGraphDocument[] = [];
  for (const document of documentsForGraph) {
    const key = graphFileKey(document.fileName);
    if (seen.has(key)) {
      continue;
    }
    seen.add(key);
    unique.push(document);
  }
  return unique;
}

function uniqueAspGraphDocumentSources(
  sourcesForGraph: AspGraphDocumentSource[],
): AspGraphDocumentSource[] {
  const seen = new Set<string>();
  const unique: AspGraphDocumentSource[] = [];
  for (const source of sourcesForGraph) {
    const key = graphFileKey(source.fileName);
    if (seen.has(key)) {
      continue;
    }
    seen.add(key);
    unique.push(source);
  }
  return unique;
}

function graphPayloadDocumentSourceFromDocument(
  document: AspGraphDocument,
): AspGraphDocumentSource {
  return {
    uri: document.uri,
    fileName: document.fileName,
    textLength: document.text.length,
    load: async () => document,
  };
}

async function canonicalizeImplicitGlobalIndexedDocumentsAsync(
  indexedDocuments: AspGraphIndexedDocument[],
  settings: AspSettings,
  cancellation: AnalysisCancellation = neverCancelled,
): Promise<AspGraphIndexedDocument[]> {
  return aspGraphBuildService.canonicalizeImplicitGlobalIndexedDocumentsAsync(
    indexedDocuments,
    settings,
    cancellation,
  );
}

function graphDisplayFileName(state: AspGraphBuildState, fileName: string): string {
  const normalized = normalizeFileName(fileName);
  const workspaceRoot = state.workspaceRootFileNames.find((root) =>
    isFileInDirectoryOrEqual(normalized, root),
  );
  if (!workspaceRoot) {
    return normalized;
  }
  const relative = path.relative(workspaceRoot, normalized);
  return relative || path.basename(normalized);
}

function isFileInDirectoryOrEqual(fileName: string, directory: string): boolean {
  const relative = path.relative(directory, normalizeFileName(fileName));
  return (
    relative === "" ||
    (relative.length > 0 && !relative.startsWith("..") && !path.isAbsolute(relative))
  );
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
  const ownerName =
    receiver?.typeName ??
    receiver?.name ??
    sourceGraphReceiverTypeName(state, receiverName) ??
    receiverName;
  return state.externalSymbols.memberByOwnerAndName.get(externalMemberKey(ownerName, memberName));
}

function sourceGraphReceiverTypeName(
  state: AspGraphBuildState,
  receiverName: string,
): string | undefined {
  return resolveSourceGraphDeclaration(state, receiverName, ["variable"])?.typeName;
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
    : fileGraphNodeIdFromUri(uri);
}

async function addDocumentStructureToAspGraphAsync(
  state: AspGraphBuildState,
  indexed: AspGraphIndexedDocument,
  settings: AspSettings,
  options: {
    fileFilter?: (fileName: string) => boolean;
  } = {},
): Promise<void> {
  const { document, graphIndex } = indexed;
  const index = graphIndex.vbSymbolIndex;
  const documentKey = graphFileKey(document.fileName);
  const fileNode = fileGraphNodeId(document.fileName);
  for (const include of graphIndex.includeRefs) {
    const resolved = await resolveIncludePathDetailsAsync(
      document.uri,
      include.path,
      include.mode,
      settings,
    );
    const targetFileName = normalizeFileName(resolved.fileName);
    if (options.fileFilter && !options.fileFilter(targetFileName)) {
      continue;
    }
    const targetKey = graphFileKey(targetFileName);
    const targetUri = pathToFileUri(targetFileName);
    pushAspGraphMapItem(state.directIncludesByOwnerKey, documentKey, {
      range: include.range,
      targetKey,
    });
    pushAspGraphMapItem(state.parentIncludesByTargetKey, targetKey, {
      ownerKey: documentKey,
      range: include.range,
    });
    addFileGraphNode(state, targetFileName, resolved.exists);
    state.stats.includes += 1;
    if (!resolved.exists) {
      state.stats.missingIncludes += 1;
    }
    addAspGraphLink(state, {
      source: fileNode,
      target: fileGraphNodeId(targetFileName),
      kind: "include",
      label: include.mode === "virtual" ? `virtual ${include.path}` : include.path,
      ranges: [{ uri: document.uri, range: include.range }],
      include: {
        path: include.path,
        mode: include.mode,
        exists: resolved.exists,
        resolvedUri: targetUri,
        actualPath: resolved.actualPath
          ? graphDisplayFileName(state, resolved.actualPath)
          : undefined,
        pathCaseMatches: resolved.pathCaseMatches,
      },
    });
  }
  for (const declaration of index.declarations) {
    const declarationNode = declarationGraphNodeId(declaration.id);
    const typeHint = graphIndex.typeHints.get(
      graphDeclarationTypeHintKey(declaration.kind, declaration.name, declaration.nameRange),
    );
    state.declarations.add(declaration.id);
    state.sourceDeclarationsById.set(declaration.id, declaration);
    state.sourceDeclarationFileKeysById.set(declaration.id, documentKey);
    state.stats.declarations += 1;
    state.nodes.set(declarationNode, {
      id: declarationNode,
      kind: "vbDeclaration",
      label: declaration.memberOf
        ? `${declaration.memberOf}.${declaration.name}`
        : declaration.name,
      uri: document.uri,
      range: declaration.nameRange,
      sourceRange: declaration.sourceRange,
      declarationKind: declaration.kind,
      memberOf: declaration.memberOf,
      bindingScope: declaration.bindingScope,
      procedureKind: declaration.procedureKind,
      implicit: declaration.implicit,
      implicitGlobal: declaration.implicitGlobal,
      implicitGlobalCandidate: declaration.implicitGlobalCandidate,
      typeName: aspGraphDeclarationTypeName(state, typeHint?.typeName ?? declaration.typeName),
      parameters: aspGraphDeclarationParameters(state, declaration, typeHint),
      arrayKind: declaration.arrayKind,
      arrayDimensions: declaration.arrayDimensions,
      group: declaration.kind,
      origin: "source",
    });
    if (isCrossFileSourceGraphDeclaration(declaration)) {
      pushAspGraphMapItem(state.sourceDeclarationsByName, declaration.normalizedName, declaration);
    }
    addAspGraphLink(state, {
      source: declarationNode,
      target: declarationSourceGraphNodeId(state, document.uri, declaration.scopeId),
      kind: "declares",
      label: "declares",
      ranges: [{ uri: document.uri, range: declaration.nameRange }],
    });
  }
}

function addDocumentUsageToAspGraph(
  state: AspGraphBuildState,
  indexed: AspGraphIndexedDocument,
): void {
  const { document, graphIndex } = indexed;
  const index = graphIndex.vbSymbolIndex;
  const memberChainPaths = graphMemberChainPaths(state, indexed);
  for (const reference of index.references) {
    if (reference.role === "call" || reference.role === "new" || reference.role === "member") {
      continue;
    }
    const resolvedDeclaration = reference.resolvedId
      ? state.sourceDeclarationsById.get(reference.resolvedId)
      : undefined;
    const sourceDeclaration = reference.resolvedId
      ? resolveIncludedImplicitSourceGraphDeclaration(state, indexed, reference)
      : resolveVisibleSourceGraphDeclaration(
          state,
          document.uri,
          reference.name,
          reference.expectedKinds,
          reference.range,
        );
    const external =
      (reference.resolvedId && resolvedDeclaration?.implicitGlobalCandidate !== true) ||
      sourceDeclaration
        ? undefined
        : resolveExternalGraphSymbol(state, reference.name);
    if (isSuppressedBuiltinGraphExternalSymbol(external)) {
      continue;
    }
    if (!reference.resolvedId && !sourceDeclaration && !external) {
      continue;
    }
    const target = sourceDeclaration
      ? declarationGraphNodeId(sourceDeclaration.id)
      : external
        ? addExternalGraphNode(state, external)
        : reference.resolvedId
          ? declarationGraphNodeId(reference.resolvedId)
          : "";
    const linkKind = reference.role === "write" ? "assignments" : "references";
    if (linkKind === "assignments") {
      state.stats.assignments += 1;
    } else {
      state.stats.references += 1;
    }
    addAspGraphLink(state, {
      source: scopeGraphNodeId(state, document.uri, reference.scopeId),
      target,
      kind: linkKind,
      label: reference.role,
      role: reference.role,
      ranges: [{ uri: document.uri, range: reference.range }],
    });
  }
  for (const callSite of index.callSites) {
    state.stats.calls += 1;
    const sourceDeclaration = callSite.resolvedId
      ? undefined
      : resolveSourceGraphCallSite(state, indexed, callSite);
    const external = callSite.resolvedId
      ? undefined
      : sourceDeclaration
        ? undefined
        : resolveExternalGraphCallSite(state, callSite);
    if (isSuppressedBuiltinGraphExternalSymbol(external)) {
      continue;
    }
    if (!callSite.resolvedId && !sourceDeclaration && !external && callSite.memberName) {
      addAspGraphLink(state, {
        source: scopeGraphNodeId(state, document.uri, callSite.scopeId),
        target: addMemberReferenceGraphNodeForAccess(
          state,
          document.uri,
          callSite,
          memberChainPaths.get(graphMemberAccessKey(callSite)),
        ),
        kind: "calls",
        label: "member",
        role: "member",
        ranges: [{ uri: document.uri, range: callSite.range }],
      });
      continue;
    }
    const defaultMemberReceiver =
      !callSite.resolvedId && !sourceDeclaration && !external
        ? resolveVisibleSourceGraphDefaultMemberReceiver(state, indexed, callSite)
        : undefined;
    if (defaultMemberReceiver) {
      state.stats.references += 1;
      addAspGraphLink(state, {
        source: scopeGraphNodeId(state, document.uri, callSite.scopeId),
        target: declarationGraphNodeId(defaultMemberReceiver.id),
        kind: "references",
        label: "read",
        role: "read",
        ranges: [{ uri: document.uri, range: callSite.range }],
      });
      continue;
    }
    const target = callSite.resolvedId
      ? declarationGraphNodeId(callSite.resolvedId)
      : sourceDeclaration
        ? declarationGraphNodeId(sourceDeclaration.id)
        : external
          ? addExternalGraphNode(state, external)
          : unresolvedGraphNodeId(callSite.name, callSite.callKind);
    if (!callSite.resolvedId && !sourceDeclaration && !external) {
      addUnresolvedGraphNode(state, document.uri, callSite.name, callSite.range, callSite.callKind);
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
    const sourceDeclaration = resolveVisibleSourceGraphDeclaration(
      state,
      document.uri,
      deferred.name,
      deferred.expectedKinds,
      deferred.range,
    );
    const external = sourceDeclaration
      ? undefined
      : resolveExternalGraphDeferredRef(state, deferred);
    if (sourceDeclaration || external) {
      continue;
    }
    if (deferred.memberName) {
      state.stats.references += 1;
      addAspGraphLink(state, {
        source: scopeGraphNodeId(state, document.uri, deferred.scopeId),
        target: addMemberReferenceGraphNodeForAccess(
          state,
          document.uri,
          deferred,
          memberChainPaths.get(graphMemberAccessKey(deferred)),
        ),
        kind: "references",
        label: "member",
        role: "member",
        ranges: [{ uri: document.uri, range: deferred.range }],
      });
      continue;
    }
    if (resolveVisibleSourceGraphDefaultMemberReceiver(state, indexed, deferred)) {
      continue;
    }
    state.stats.unresolvedReferences += 1;
    const target = unresolvedGraphNodeId(deferred.name, deferred.role);
    addUnresolvedGraphNode(state, document.uri, deferred.name, deferred.range, deferred.role);
    addAspGraphLink(state, {
      source: scopeGraphNodeId(state, document.uri, deferred.scopeId),
      target,
      kind: "unresolvedReference",
      label: deferred.role,
      role: deferred.role,
      ranges: [{ uri: document.uri, range: deferred.range }],
    });
  }
  addMemberChainLinksToAspGraph(state, indexed, memberChainPaths);
}

interface AspGraphMemberChainPath {
  rootName: string;
  receiverPath: string;
  fullPath: string;
  memberName: string;
  range: Range;
  scopeId?: string;
  parentKey?: string;
}

type AspGraphMemberAccess = {
  name: string;
  range: Range;
  scopeId?: string;
  receiverName?: string;
  memberName?: string;
};

interface AspGraphMemberChainRootReferenceIndex {
  byScopedName: Map<string, Array<VbSymbolIndex["references"][number]>>;
  byName: Map<string, Array<VbSymbolIndex["references"][number]>>;
}

type AspGraphMemberCallSite = VbSymbolIndex["callSites"][number] & {
  receiverName: string;
  memberName: string;
};

function graphMemberChainPaths(
  state: AspGraphBuildState,
  indexed: AspGraphIndexedDocument,
): Map<string, AspGraphMemberChainPath> {
  const paths = new Map<string, AspGraphMemberChainPath>();
  const processed: AspGraphMemberCallSite[] = [];
  const callSites = indexed.graphIndex.vbSymbolIndex.callSites
    .filter((callSite): callSite is AspGraphMemberCallSite =>
      shouldGraphMemberChainCallSite(state, indexed, callSite),
    )
    .sort(compareGraphMemberAccessStart);
  for (const callSite of callSites) {
    const parent = findGraphMemberChainParent(indexed.document.text, processed, callSite);
    const parentKey = parent ? graphMemberAccessKey(parent) : undefined;
    const parentPath = parentKey ? paths.get(parentKey) : undefined;
    const receiverPath = parentPath?.fullPath ?? callSite.receiverName;
    const fullPath = `${receiverPath}.${callSite.memberName}`;
    paths.set(graphMemberAccessKey(callSite), {
      rootName: parentPath?.rootName ?? callSite.receiverName,
      receiverPath,
      fullPath,
      memberName: callSite.memberName,
      range: callSite.range,
      scopeId: callSite.scopeId,
      parentKey,
    });
    processed.push(callSite);
  }
  return paths;
}

function shouldGraphMemberChainCallSite(
  state: AspGraphBuildState,
  indexed: AspGraphIndexedDocument,
  callSite: VbSymbolIndex["callSites"][number],
): callSite is AspGraphMemberCallSite {
  if (!callSite.receiverName || !callSite.memberName || callSite.resolvedId) {
    return false;
  }
  const sourceDeclaration = resolveSourceGraphCallSite(state, indexed, callSite);
  if (sourceDeclaration) {
    return false;
  }
  const external = resolveExternalGraphCallSite(state, callSite);
  return !external && !isSuppressedBuiltinGraphExternalSymbol(external);
}

function compareGraphMemberAccessStart(left: AspGraphMemberAccess, right: AspGraphMemberAccess) {
  if (left.range.start.line !== right.range.start.line) {
    return left.range.start.line - right.range.start.line;
  }
  return left.range.start.character - right.range.start.character;
}

function findGraphMemberChainParent(
  text: string,
  processed: AspGraphMemberCallSite[],
  callSite: AspGraphMemberCallSite,
): AspGraphMemberCallSite | undefined {
  for (let index = processed.length - 1; index >= 0; index -= 1) {
    const candidate = processed[index];
    if (
      candidate.scopeId === callSite.scopeId &&
      candidate.memberName.toLowerCase() === callSite.receiverName.toLowerCase() &&
      isGraphMemberChainSeparator(text, candidate.range, callSite.range)
    ) {
      return candidate;
    }
  }
  return undefined;
}

function isGraphMemberChainSeparator(text: string, left: Range, right: Range): boolean {
  if (left.end.line !== right.start.line) {
    return false;
  }
  const between = text.slice(offsetAtText(text, left.end), offsetAtText(text, right.start));
  return /^\s*\.\s*$/.test(between);
}

function addMemberReferenceGraphNodeForAccess(
  state: AspGraphBuildState,
  uri: string,
  access: AspGraphMemberAccess,
  chainPath: AspGraphMemberChainPath | undefined,
): string {
  const memberName = chainPath?.memberName ?? access.memberName ?? access.name;
  return addMemberReferenceGraphNode(
    state,
    uri,
    chainPath?.receiverPath ?? access.receiverName,
    memberName,
    access.range,
    chainPath?.fullPath,
  );
}

function addMemberChainLinksToAspGraph(
  state: AspGraphBuildState,
  indexed: AspGraphIndexedDocument,
  paths: Map<string, AspGraphMemberChainPath>,
): void {
  const rootReferenceIndex = graphMemberChainRootReferenceIndex(
    indexed.graphIndex.vbSymbolIndex.references,
  );
  for (const path of paths.values()) {
    const source = addMemberReferenceGraphNode(
      state,
      indexed.document.uri,
      path.receiverPath,
      path.memberName,
      path.range,
      path.fullPath,
    );
    const parentPath = path.parentKey ? paths.get(path.parentKey) : undefined;
    const target = parentPath
      ? addMemberReferenceGraphNode(
          state,
          indexed.document.uri,
          parentPath.receiverPath,
          parentPath.memberName,
          parentPath.range,
          parentPath.fullPath,
        )
      : graphMemberChainBaseTarget(state, indexed, path, rootReferenceIndex);
    addAspGraphLink(state, {
      source,
      target,
      kind: "calls",
      label: "member",
      role: "member",
      ranges: [{ uri: indexed.document.uri, range: path.range }],
    });
  }
}

function graphMemberChainBaseTarget(
  state: AspGraphBuildState,
  indexed: AspGraphIndexedDocument,
  path: AspGraphMemberChainPath,
  rootReferenceIndex: AspGraphMemberChainRootReferenceIndex,
): string {
  const declaration = resolveGraphMemberChainBaseDeclaration(
    state,
    indexed,
    path,
    rootReferenceIndex,
  );
  return declaration
    ? declarationGraphNodeId(declaration.id)
    : addMemberReferenceGraphNode(
        state,
        indexed.document.uri,
        undefined,
        path.rootName,
        path.range,
        path.rootName,
      );
}

function resolveGraphMemberChainBaseDeclaration(
  state: AspGraphBuildState,
  indexed: AspGraphIndexedDocument,
  path: AspGraphMemberChainPath,
  rootReferenceIndex: AspGraphMemberChainRootReferenceIndex,
): VbSymbolIndex["declarations"][number] | undefined {
  const rootReference = findGraphMemberChainRootReference(indexed, path, rootReferenceIndex);
  if (rootReference?.resolvedId) {
    return (
      resolveIncludedImplicitSourceGraphDeclaration(state, indexed, rootReference) ??
      state.sourceDeclarationsById.get(rootReference.resolvedId)
    );
  }
  return resolveVisibleSourceGraphDeclaration(
    state,
    indexed.document.uri,
    path.rootName,
    rootReference?.expectedKinds ?? ["variable", "constant", "parameter", "field"],
    path.range,
  );
}

function findGraphMemberChainRootReference(
  indexed: AspGraphIndexedDocument,
  path: AspGraphMemberChainPath,
  rootReferenceIndex: AspGraphMemberChainRootReferenceIndex,
): VbSymbolIndex["references"][number] | undefined {
  for (const reference of rootReferenceIndex.byScopedName.get(
    graphMemberChainRootReferenceKey(path.rootName, path.scopeId),
  ) ?? []) {
    if (isGraphMemberChainSeparator(indexed.document.text, reference.range, path.range)) {
      return reference;
    }
  }
  return rootReferenceIndex.byName
    .get(path.rootName.toLowerCase())
    ?.find((reference) =>
      isGraphMemberChainSeparator(indexed.document.text, reference.range, path.range),
    );
}

function graphMemberChainRootReferenceIndex(
  references: VbSymbolIndex["references"],
): AspGraphMemberChainRootReferenceIndex {
  const index: AspGraphMemberChainRootReferenceIndex = {
    byScopedName: new Map(),
    byName: new Map(),
  };
  for (const reference of references) {
    if (reference.role === "member") {
      continue;
    }
    const name = reference.name.toLowerCase();
    pushAspGraphMapItem(index.byName, name, reference);
    pushAspGraphMapItem(
      index.byScopedName,
      graphMemberChainRootReferenceKey(reference.name, reference.scopeId),
      reference,
    );
  }
  return index;
}

function graphMemberChainRootReferenceKey(name: string, scopeId: string | undefined): string {
  return `${scopeId ?? ""}\u0000${name.toLowerCase()}`;
}

function graphMemberAccessKey(access: AspGraphMemberAccess): string {
  return [
    access.scopeId ?? "",
    access.name.toLowerCase(),
    access.memberName?.toLowerCase() ?? "",
    access.range.start.line,
    access.range.start.character,
    access.range.end.line,
    access.range.end.character,
  ].join("|");
}

function isSuppressedBuiltinGraphExternalSymbol(
  symbol: VbGraphExternalSymbol | undefined,
): boolean {
  return symbol?.origin === "builtin";
}

function aspGraphDeclarationTypeName(
  state: AspGraphBuildState,
  typeName: string | undefined,
): string | undefined {
  return state.includeAnalysisTypeDetails ? typeName : visibleAspGraphTypeName(typeName);
}

function aspGraphDeclarationParameters(
  state: AspGraphBuildState,
  declaration: VbSymbolIndex["declarations"][number],
  typeHint: AspGraphDeclarationTypeHint | undefined,
): AspGraphNodeParameter[] | undefined {
  if (!declaration.parameters?.length && !typeHint?.parameters?.length) {
    return undefined;
  }
  const hinted = new Map(
    (typeHint?.parameters ?? []).map((parameter) => [parameter.name.toLowerCase(), parameter]),
  );
  return (declaration.parameters ?? typeHint?.parameters ?? []).map((parameter) => {
    const hint = hinted.get(parameter.name.toLowerCase());
    return {
      name: parameter.name,
      mode: hint?.mode,
      optional: hint?.optional,
      typeName: aspGraphDeclarationTypeName(state, hint?.typeName),
    };
  });
}

function visibleAspGraphTypeName(typeName: string | undefined): string | undefined {
  return typeName && !isSuppressedBuiltinGraphTypeName(typeName) ? typeName : undefined;
}

function isSuppressedBuiltinGraphTypeName(typeName: string): boolean {
  return ["regexp", "match", "matches", "submatches"].includes(typeName.toLowerCase());
}

function resolveVisibleSourceGraphDefaultMemberReceiver(
  state: AspGraphBuildState,
  indexed: AspGraphIndexedDocument,
  item: Pick<VbSymbolIndex["callSites"][number], "name" | "memberName" | "range">,
): VbSymbolIndex["declarations"][number] | undefined {
  if (item.memberName) {
    return undefined;
  }
  return resolveVisibleSourceGraphDeclaration(
    state,
    indexed.document.uri,
    item.name,
    ["variable", "constant"],
    item.range,
  );
}

function isCrossFileSourceGraphDeclaration(
  declaration: VbSymbolIndex["declarations"][number],
): boolean {
  return declaration.bindingScope !== "local" && !declaration.memberOf;
}

function resolveSourceGraphCallSite(
  state: AspGraphBuildState,
  indexed: AspGraphIndexedDocument,
  callSite: VbSymbolIndex["callSites"][number],
): VbSymbolIndex["declarations"][number] | undefined {
  if (callSite.memberName) {
    return undefined;
  }
  return resolveVisibleSourceGraphDeclaration(
    state,
    indexed.document.uri,
    callSite.name,
    expectedSourceGraphKindsForCallSite(callSite),
    callSite.range,
  );
}

function resolveSourceGraphDeclaration(
  state: AspGraphBuildState,
  name: string | undefined,
  expectedKinds: VbSymbolIndex["references"][number]["expectedKinds"],
): VbSymbolIndex["declarations"][number] | undefined {
  if (!name) {
    return undefined;
  }
  const candidates = state.sourceDeclarationsByName.get(name.toLowerCase());
  return candidates?.find(
    (declaration) => !expectedKinds || expectedKinds.includes(declaration.kind),
  );
}

function resolveVisibleSourceGraphDeclaration(
  state: AspGraphBuildState,
  ownerUri: string,
  name: string | undefined,
  expectedKinds: VbSymbolIndex["references"][number]["expectedKinds"],
  referenceRange: Range,
): VbSymbolIndex["declarations"][number] | undefined {
  if (!name) {
    return undefined;
  }
  const ownerKey = graphFileKeyFromUri(ownerUri);
  const candidates = state.sourceDeclarationsByName.get(name.toLowerCase());
  return candidates?.find(
    (declaration) =>
      matchesGraphExpectedKinds(declaration, expectedKinds) &&
      isSourceGraphDeclarationVisibleFromDocument(state, ownerKey, declaration, referenceRange),
  );
}

function resolveIncludedImplicitSourceGraphDeclaration(
  state: AspGraphBuildState,
  indexed: AspGraphIndexedDocument,
  reference: VbSymbolIndex["references"][number],
): VbSymbolIndex["declarations"][number] | undefined {
  if (!reference.resolvedId) {
    return undefined;
  }
  const resolvedDeclaration = state.sourceDeclarationsById.get(reference.resolvedId);
  if (!resolvedDeclaration?.implicit) {
    return undefined;
  }
  const candidates = state.sourceDeclarationsByName.get(reference.normalizedName);
  const ownerKey = graphFileKey(indexed.document.fileName);
  return candidates?.find((candidate) => {
    const candidateKey = state.sourceDeclarationFileKeysById.get(candidate.id);
    return (
      candidateKey !== undefined &&
      candidateKey !== ownerKey &&
      isCrossFileSourceGraphDeclaration(candidate) &&
      matchesGraphExpectedKinds(candidate, reference.expectedKinds ?? [resolvedDeclaration.kind]) &&
      isSourceGraphDeclarationVisibleFromDocument(state, ownerKey, candidate, reference.range)
    );
  });
}

function matchesGraphExpectedKinds(
  declaration: VbSymbolIndex["declarations"][number],
  expectedKinds: VbSymbolIndex["references"][number]["expectedKinds"],
): boolean {
  return !expectedKinds || expectedKinds.includes(declaration.kind);
}

function hasEarlierReachableGraphInclude(
  state: AspGraphBuildState,
  ownerKey: string,
  targetKey: string,
  referenceRange: Range,
): boolean {
  const includes = state.directIncludesByOwnerKey.get(ownerKey) ?? [];
  return includes.some((include) => {
    if (!positionBeforeOrEqual(include.range.start, referenceRange.start)) {
      return false;
    }
    if (include.targetKey === targetKey) {
      return true;
    }
    const precomputed = graphIncludeCanReachTarget(state, include.targetKey, targetKey);
    return (
      precomputed === true ||
      (precomputed === undefined &&
        isGraphIncludeReachable(state, include.targetKey, targetKey, new Set([ownerKey])))
    );
  });
}

function isSourceGraphDeclarationVisibleFromDocument(
  state: AspGraphBuildState,
  ownerKey: string,
  declaration: VbSymbolIndex["declarations"][number],
  referenceRange: Range,
): boolean {
  const declarationKey = state.sourceDeclarationFileKeysById.get(declaration.id);
  if (!declarationKey) {
    return false;
  }
  if (declarationKey === ownerKey) {
    return true;
  }
  return isSourceGraphDeclarationVisibleFromFileAt(
    state,
    ownerKey,
    declaration,
    declarationKey,
    referenceRange,
    sourceGraphVisibilityMemo(state),
    new Set([ownerKey]),
  );
}

function isSourceGraphDeclarationVisibleFromFileAt(
  state: AspGraphBuildState,
  ownerKey: string,
  declaration: VbSymbolIndex["declarations"][number],
  declarationKey: string,
  referenceRange: Range,
  memo: IncludeVisibilityMemo,
  visited: Set<string>,
): boolean {
  const key = includeVisibilityMemoKey(
    ownerKey,
    `${declarationKey}\0${declaration.id}`,
    referenceRange,
  );
  return memoizedIncludeVisibility(memo, key, () => {
    if (declarationKey === ownerKey) {
      return positionBeforeOrEqual(declaration.nameRange.start, referenceRange.start);
    }
    if (hasEarlierReachableGraphInclude(state, ownerKey, declarationKey, referenceRange)) {
      return true;
    }
    for (const parentInclude of state.parentIncludesByTargetKey.get(ownerKey) ?? []) {
      if (visited.has(parentInclude.ownerKey)) {
        continue;
      }
      visited.add(parentInclude.ownerKey);
      const visible = isSourceGraphDeclarationVisibleFromFileAt(
        state,
        parentInclude.ownerKey,
        declaration,
        declarationKey,
        parentInclude.range,
        memo,
        visited,
      );
      visited.delete(parentInclude.ownerKey);
      if (visible) {
        return true;
      }
    }
    return false;
  });
}

function graphIncludeCanReachTarget(
  state: AspGraphBuildState,
  startKey: string,
  targetKey: string,
): boolean | undefined {
  const reachability = graphIncludeReachability(state);
  if (reachability.hasCycle) {
    return undefined;
  }
  let reaching = reachability.reachingFileKeysByTarget.get(targetKey);
  if (!reaching) {
    reaching = graphReachingFileKeysForTarget(state, targetKey);
    reachability.reachingFileKeysByTarget.set(targetKey, reaching);
  }
  return reaching.has(startKey);
}

function graphIncludeReachability(state: AspGraphBuildState): PrecomputedIncludeReachability {
  if (!state.includeReachability) {
    state.includeReachability = {
      hasCycle: includeGraphHasCycle(state),
      reachingFileKeysByTarget: new Map(),
    };
  }
  return state.includeReachability;
}

function graphReachingFileKeysForTarget(state: AspGraphBuildState, targetKey: string): Set<string> {
  const reaching = new Set<string>();
  const queue = [targetKey];
  for (let index = 0; index < queue.length; index += 1) {
    const currentKey = queue[index];
    for (const parentInclude of state.parentIncludesByTargetKey.get(currentKey) ?? []) {
      if (reaching.has(parentInclude.ownerKey)) {
        continue;
      }
      reaching.add(parentInclude.ownerKey);
      queue.push(parentInclude.ownerKey);
    }
  }
  return reaching;
}

function isGraphIncludeReachable(
  state: AspGraphBuildState,
  startKey: string,
  targetKey: string,
  visited: Set<string>,
): boolean {
  if (startKey === targetKey) {
    return true;
  }
  if (visited.has(startKey)) {
    return false;
  }
  visited.add(startKey);
  return (state.directIncludesByOwnerKey.get(startKey) ?? []).some(
    (include) =>
      include.targetKey === targetKey ||
      isGraphIncludeReachable(state, include.targetKey, targetKey, visited),
  );
}

function positionBeforeOrEqual(left: Position, right: Position): boolean {
  return left.line < right.line || (left.line === right.line && left.character <= right.character);
}

function expectedSourceGraphKindsForCallSite(
  callSite: VbSymbolIndex["callSites"][number],
): VbSymbolIndex["references"][number]["expectedKinds"] {
  switch (callSite.callKind) {
    case "constructor":
      return ["class"];
    case "function":
      return ["function"];
    case "procedure":
      return ["function", "sub", "method", "property"];
    case "unknown":
      return ["function", "sub", "class", "method", "property"];
    case "member":
      return undefined;
  }
}

async function graphDocumentFromCachedAsync(
  cached: CachedDocument,
  settings: AspSettings,
): Promise<AspGraphDocument> {
  const fileName = graphFileNameFromUri(cached.source.uri);
  const identity = await includeDocumentSourceIdentityAsync(fileName, settings);
  return {
    uri: pathToFileUri(fileName),
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

async function graphDocumentFromIncludeFileAsync(
  fileName: string,
  settings: AspSettings,
): Promise<AspGraphDocument | undefined> {
  const entry = await includeDocumentLoader.readAsync(fileName, settings);
  return entry ? graphDocumentFromIncludeEntry(entry) : undefined;
}

function graphDocumentFromIncludeEntry(entry: IncludeDocumentCacheEntry): AspGraphDocument {
  const fileName = normalizeFileName(entry.fileName);
  return {
    uri: pathToFileUri(fileName),
    fileName,
    text: entry.parsed.text,
    source: entry.source,
    diskBacked: !documents
      .all()
      .some((document) => graphFileKeyFromUri(document.uri) === graphFileKey(fileName)),
  };
}

async function graphFileIndexForDocumentAsync(
  document: AspGraphDocument,
  settings: AspSettings,
  options: { includeTypeHints?: boolean; operationCache?: GraphFileIndexOperationCache } = {},
): Promise<GraphFileIndex> {
  const settingsKey = graphFileIndexSettingsKey(settings);
  const baseKey = JSON.stringify({
    fileName: graphFileKey(document.fileName),
    source: diskAnalysisSourceIdentity(document.source),
    settings: settingsKey,
    text: document.diskBacked ? undefined : textFingerprint(document.text),
    typeHints: false,
  });
  const key = JSON.stringify({
    fileName: graphFileKey(document.fileName),
    source: diskAnalysisSourceIdentity(document.source),
    settings: settingsKey,
    text: document.diskBacked ? undefined : textFingerprint(document.text),
    typeHints: options.includeTypeHints === true,
  });
  const documentKey = graphFileKey(document.fileName);
  const existing = graphFileIndexCache.get(documentKey);
  if (existing?.key === key) {
    existing.lastUsed = Date.now();
    return existing;
  }
  if (options.includeTypeHints === true && existing?.key === baseKey) {
    const typeHints = await graphDeclarationTypeHintsForDocumentAsync(document, settings);
    const entry: GraphFileIndex = { ...existing, key, typeHints, lastUsed: Date.now() };
    graphFileIndexCache.set(documentKey, entry);
    logDebugSummary(settings, `[asp-lsp] graphVbIndex.extendTypeHints: ${document.uri}`);
    return entry;
  }
  const operationBasePending =
    options.includeTypeHints === true ? options.operationCache?.get(baseKey) : undefined;
  if (operationBasePending) {
    const baseEntry = await operationBasePending;
    const typeHints = await graphDeclarationTypeHintsForDocumentAsync(document, settings);
    const entry: GraphFileIndex = { ...baseEntry, key, typeHints, lastUsed: Date.now() };
    graphFileIndexCache.set(documentKey, entry);
    options.operationCache?.set(key, Promise.resolve(entry));
    logDebugSummary(settings, `[asp-lsp] graphVbIndex.operationReuse: ${document.uri}`);
    return entry;
  }
  const operationPending = options.operationCache?.get(key);
  if (operationPending) {
    return operationPending;
  }
  const pending = graphFileIndexInFlight.get(key);
  if (pending) {
    options.operationCache?.set(key, pending);
    return pending;
  }
  const promise = (async () => {
    const includeRefsEntry = await readGraphIncludeRefsEntryAsync(document.fileName, settings);
    if (document.diskBacked) {
      const cachedIndex = await diskAnalysisCache
        .readVbSymbolIndex({ source: document.source, settingsKey })
        .catch((error) => {
          logDiskAnalysisCacheError("graphVbIndex.read", error);
          return undefined;
        });
      if (cachedIndex) {
        const typeHints =
          options.includeTypeHints === true
            ? await graphDeclarationTypeHintsForDocumentAsync(document, settings)
            : new Map<string, AspGraphDeclarationTypeHint>();
        const entry = graphFileIndexFromDisk(
          document.fileName,
          key,
          cachedIndex,
          includeRefsEntry,
          typeHints,
        );
        graphFileIndexCache.set(documentKey, entry);
        pruneGraphFileIndexCache();
        logDebugSummary(settings, `[asp-lsp] graphVbIndex.hit: ${document.uri}`);
        return entry;
      }
      logDebugSummary(settings, `[asp-lsp] graphVbIndex.miss: ${document.uri}`);
    }
    const extracted = await extractGraphVbSymbolIndexAsync(document, settings);
    const includeRefs =
      includeRefsEntry && sameDiskAnalysisSource(includeRefsEntry.source, document.source)
        ? includeRefsEntry.includeRefs
        : extracted.includeRefs;
    const vbSymbolIndex: VbSymbolIndex = { ...extracted, includeRefs };
    const typeHints =
      options.includeTypeHints === true
        ? await graphDeclarationTypeHintsForDocumentAsync(document, settings)
        : new Map<string, AspGraphDeclarationTypeHint>();
    const entry: GraphFileIndex = {
      key,
      uri: document.uri,
      fileName: document.fileName,
      source: document.source,
      includeRefs,
      vbSymbolIndex,
      typeHints,
      fingerprint: graphFileIndexFingerprint(vbSymbolIndex),
      lastUsed: Date.now(),
    };
    graphFileIndexCache.set(documentKey, entry);
    pruneGraphFileIndexCache();
    if (document.diskBacked) {
      await diskAnalysisCache.writeVbSymbolIndex(diskVbSymbolIndexCacheEntry(entry, settings));
      logDebugSummary(settings, `[asp-lsp] graphVbIndex.write: ${document.uri}`);
    }
    return entry;
  })();
  options.operationCache?.set(key, promise);
  graphFileIndexInFlight.set(key, promise);
  try {
    return await promise;
  } finally {
    if (graphFileIndexInFlight.get(key) === promise) {
      graphFileIndexInFlight.delete(key);
    }
    void promise.catch(() => options.operationCache?.delete(key));
  }
}

async function extractGraphVbSymbolIndexAsync(
  document: AspGraphDocument,
  settings: AspSettings,
): Promise<VbSymbolIndex> {
  const fallback = () =>
    extractVbscriptSymbolIndex(document.uri, document.text, settings, {
      includeImplicitVariables: true,
    });
  if (settings.graph?.workerSymbolExtraction !== true) {
    return fallback();
  }
  const id = ++vbReferencesWorkerRequestId;
  const range = {
    start: { line: 0, character: 0 },
    end: { line: 0, character: 0 },
  };
  try {
    const response = await getVbReferencesWorkerPool(settings).run({
      id,
      kind: "extractSymbolIndex",
      candidate: {
        uri: document.uri,
        fileName: document.fileName,
        source: document.source,
        text: document.text,
      },
      target: {
        name: "",
        kind: "variable",
        sourceUri: document.uri,
        range,
      },
      settings: graphSymbolExtractionWorkerSettings(settings),
      workspaceRoots,
      openDocuments: [],
      options: {},
      limits: {
        maxDocuments: 1,
        maxTextLength: document.text.length,
        maxDepth: 0,
        includeReadConcurrency: 1,
      },
    });
    logDebugSummary(
      settings,
      `[asp-lsp] graphVbIndex.worker.complete: ${document.uri}, request=${id}, declarations=${response.symbolIndex?.declarations.length ?? 0}`,
    );
    logDebugSummary(
      settings,
      `[asp-lsp] worker.queue.wait: ${document.uri}, request=${id}, ${formatElapsedMs(response.queueWaitMs ?? 0)}, queueLength=${response.queueLengthAtDispatch ?? 0}, cancelled=${response.cancelled === true}`,
    );
    logDebugSummary(
      settings,
      `[asp-lsp] worker.run.duration: ${document.uri}, request=${id}, ${formatElapsedMs(response.runMs ?? 0)}`,
    );
    logDebugSummary(
      settings,
      `[asp-lsp] worker.payload.bytes: ${document.uri}, request=${id}, payload=${response.payloadBytes ?? 0}, result=${response.resultBytes ?? 0}`,
    );
    if (response.error) {
      throw new Error(response.error.message);
    }
    if (!response.symbolIndex) {
      throw new Error("VBScript symbol-index worker returned no index.");
    }
    return response.symbolIndex;
  } catch (error) {
    logDebugSummary(
      settings,
      `[asp-lsp] graphVbIndex.worker.fallback: ${document.uri}, reason=${errorMessage(error)}`,
    );
    return fallback();
  }
}

function graphSymbolExtractionWorkerSettings(settings: AspSettings): AspSettings {
  return {
    defaultLanguage: settings.defaultLanguage,
    legacyEncoding: settings.legacyEncoding,
  };
}

async function graphDeclarationTypeHintsForDocumentAsync(
  document: AspGraphDocument,
  settings: AspSettings,
): Promise<Map<string, AspGraphDeclarationTypeHint>> {
  const symbols = await collectVbscriptSymbolsFromTextAsync(
    document.uri,
    document.text,
    settings,
    vbProjectContextSettings(settings),
  );
  return graphDeclarationTypeHintsFromSymbols(symbols);
}

function graphDeclarationTypeHintsFromSymbols(
  symbols: VbSymbol[],
): Map<string, AspGraphDeclarationTypeHint> {
  const parameterSymbols = new Map<string, VbSymbol>();
  for (const symbol of symbols) {
    if (symbol.kind !== "parameter") {
      continue;
    }
    parameterSymbols.set(graphParameterTypeHintKey(symbol.scopeRange, symbol.name), symbol);
  }
  const hints = new Map<string, AspGraphDeclarationTypeHint>();
  for (const symbol of symbols) {
    hints.set(graphDeclarationTypeHintKey(symbol.kind, symbol.name, symbol.range), {
      typeName: symbol.typeName,
      parameters: graphParameterHintsForSymbol(symbol, parameterSymbols),
    });
  }
  return hints;
}

function graphParameterHintsForSymbol(
  symbol: VbSymbol,
  parameterSymbols: ReadonlyMap<string, VbSymbol>,
): AspGraphNodeParameter[] | undefined {
  const parameters =
    symbol.parameterDetails && symbol.parameterDetails.length > 0
      ? symbol.parameterDetails
      : (symbol.parameters ?? []).map((name) => ({
          name,
          mode: "byref" as const,
          optional: undefined,
        }));
  if (parameters.length === 0) {
    return undefined;
  }
  return parameters.map((parameter) => {
    const parameterSymbol = parameterSymbols.get(
      graphParameterTypeHintKey(symbol.scopeRange, parameter.name),
    );
    return {
      name: parameter.name,
      mode: parameter.mode,
      optional: parameter.optional,
      typeName: parameterSymbol?.typeName,
    };
  });
}

function graphDeclarationTypeHintKey(kind: string, name: string, range: Range): string {
  return `${kind.toLowerCase()}\0${name.toLowerCase()}\0${graphRangeKey(range)}`;
}

function graphParameterTypeHintKey(scopeRange: Range | undefined, name: string): string {
  return `${scopeRange ? graphRangeKey(scopeRange) : ""}\0${name.toLowerCase()}`;
}

function graphRangeKey(range: Range): string {
  return [range.start.line, range.start.character, range.end.line, range.end.character].join(":");
}

function graphFileIndexFingerprint(index: VbSymbolIndex): string {
  return textFingerprint(JSON.stringify(index));
}

function pruneGraphFileIndexCache(): void {
  while (graphFileIndexCache.size > graphFileIndexCacheMaxEntries) {
    let oldestKey: string | undefined;
    let oldestLastUsed = Number.POSITIVE_INFINITY;
    for (const [key, entry] of graphFileIndexCache) {
      if (entry.lastUsed < oldestLastUsed) {
        oldestKey = key;
        oldestLastUsed = entry.lastUsed;
      }
    }
    if (!oldestKey) {
      return;
    }
    graphFileIndexCache.delete(oldestKey);
  }
  checkMemoryPressure(globalSettings, "graph.fileIndex.prune");
}

function invalidateGraphFileIndexFiles(fileNames: Iterable<string>): void {
  for (const fileName of fileNames) {
    const normalized = normalizeFileName(fileName);
    graphFileIndexCache.delete(graphFileKey(normalized));
  }
  graphFileIndexInFlight.clear();
}

function clearGraphFileIndexCache(): void {
  graphFileIndexCache.clear();
  graphFileIndexInFlight.clear();
  invalidateAspGraphPayloadCache("graphFileIndex.clear");
}

function invalidateAspGraphPayloadCache(reason: string): void {
  aspGraphPayloadCache.clear();
  cancelAspGraphBackgroundBuilds(reason);
}

function addFileGraphNode(state: AspGraphBuildState, fileName: string, exists: boolean): void {
  const normalizedFileName = normalizeFileName(fileName);
  const key = graphFileKey(normalizedFileName);
  const id = fileGraphNodeId(normalizedFileName);
  const canonicalUri = pathToFileUri(normalizedFileName);
  const existing = state.nodes.get(id);
  if (!existing) {
    state.stats.files += 1;
  }
  const nextExists = existing?.exists === true || exists;
  const nextKind: AspGraphNodeKind = nextExists ? "file" : "missingInclude";
  state.nodes.set(id, {
    ...existing,
    id,
    kind: nextKind,
    label: path.basename(normalizedFileName),
    uri: canonicalUri,
    fileName: graphDisplayFileName(state, normalizedFileName),
    exists: nextExists,
    group: nextExists ? "file" : "missingInclude",
    isRoot: existing?.isRoot === true || key === state.rootFileKey ? true : undefined,
  });
}

function addUnresolvedGraphNode(
  state: AspGraphBuildState,
  uri: string,
  name: string,
  range: Range,
  role: string,
): void {
  const id = unresolvedGraphNodeId(name);
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
    group: isCallableUnresolvedRole(role) ? "unresolvedFunction" : "unresolved",
  });
}

function addMemberReferenceGraphNode(
  state: AspGraphBuildState,
  uri: string,
  receiverName: string | undefined,
  memberName: string,
  range: Range,
  fullPath?: string,
): string {
  const memberPath = fullPath ?? (receiverName ? `${receiverName}.${memberName}` : memberName);
  const id = memberReferenceGraphNodeId(memberPath);
  if (!state.nodes.has(id)) {
    state.nodes.set(id, {
      id,
      kind: "vbMemberReference",
      label: memberName,
      uri,
      range,
      role: "member",
      receiverName,
      memberName,
      fullPath: memberPath,
      group: "member",
    });
  }
  return id;
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
    appendAspGraphRanges(existing.ranges, input.ranges);
    return;
  }
  state.links.set(key, {
    ...input,
    id: `link:${state.links.size}`,
    count: 1,
  });
}

function appendAspGraphRanges(
  target: AspGraphLink["ranges"],
  rangesToAppend: readonly AspGraphLink["ranges"][number][],
): void {
  for (const range of rangesToAppend) {
    target.push(range);
  }
}

function graphPayloadSettings(settings: AspSettings): NonNullable<AspGraphPayload["settings"]> {
  const graphSettings = normalizeGraphSettings(settings);
  return {
    initialViewMode: graphSettings.initialViewMode === "3d" ? "3d" : "2d",
    hideSingleNodes: graphSettings.hideSingleNodes !== false,
    hideUnreferencedGlobalSymbols: graphSettings.hideUnreferencedGlobalSymbols !== false,
    showOutgoingSelectionLinks: graphSettings.showOutgoingSelectionLinks !== false,
    showIncomingDocumentIncludes: graphSettings.showIncomingDocumentIncludes === true,
    showIncomingFolderIncludes: graphSettings.showIncomingFolderIncludes === true,
    includeRelatedIncludeTreesForUnresolved:
      graphSettings.includeRelatedIncludeTreesForUnresolved === true,
    hiddenNodeCategories: graphNodeCategoryOrder.filter(
      (category) => !isVisibleAspGraphNodeCategory(category, graphSettings),
    ),
    hiddenLinkCategories: graphLinkFilterOrder.filter(
      (category) => !isVisibleAspGraphLinkCategory(category, graphSettings),
    ),
    maxNodes: graphSettings.maxNodes ?? defaultGraphMaxNodes,
  };
}

const graphNodeCategoryOrder: AspGraphNodeCategory[] = [
  "root",
  "file",
  "missingInclude",
  "function",
  "sub",
  "class",
  "method",
  "methodFunction",
  "methodSub",
  "property",
  "member",
  "globalVariable",
  "implicitGlobalVariable",
  "globalConstant",
  "localVariable",
  "localConstant",
  "parameter",
  "unresolvedFunction",
  "unresolved",
];

const graphLinkFilterOrder: AspGraphLinkFilterCategory[] = [
  "include",
  "declares",
  "references",
  "assignments",
  "calls",
  "unresolvedReference",
  "member",
];

function isVisibleAspGraphNodeCategory(
  category: AspGraphNodeCategory,
  settings: NonNullable<AspSettings["graph"]>,
): boolean {
  switch (category) {
    case "root":
      return settings.showRootNodes !== false;
    case "file":
      return settings.showFileNodes !== false;
    case "missingInclude":
      return settings.showFileNodes !== false;
    case "function":
      return settings.showFunctionNodes !== false;
    case "sub":
      return settings.showSubNodes !== false;
    case "class":
      return settings.showClassNodes !== false;
    case "method":
      return settings.showMethodNodes === true;
    case "methodFunction":
      return settings.showMethodFunctionNodes === true;
    case "methodSub":
      return settings.showMethodSubNodes === true;
    case "property":
      return settings.showPropertyNodes === true;
    case "member":
      return settings.showMemberNodes === true;
    case "globalVariable":
      return settings.showGlobalVariableNodes !== false;
    case "implicitGlobalVariable":
      return settings.showGlobalVariableNodes !== false;
    case "globalConstant":
      return settings.showGlobalConstantNodes !== false;
    case "localVariable":
      return settings.showLocalVariableNodes === true;
    case "localConstant":
      return settings.showLocalConstantNodes === true;
    case "parameter":
      return settings.showParameterNodes === true;
    case "unresolvedFunction":
      return settings.showUnresolvedNodes !== false;
    case "unresolved":
      return settings.showUnresolvedNodes !== false;
  }
}

function isVisibleAspGraphLinkCategory(
  category: AspGraphLinkFilterCategory,
  settings: NonNullable<AspSettings["graph"]>,
): boolean {
  if (category === "member") {
    return settings.showMemberLinks === true;
  }
  switch (category) {
    case "include":
      return settings.showIncludeLinks !== false;
    case "declares":
      return settings.showDeclareLinks !== false;
    case "references":
      return settings.showReferenceLinks !== false;
    case "assignments":
      return settings.showAssignmentLinks !== false;
    case "calls":
      return settings.showCallLinks !== false;
    case "unresolvedReference":
      return settings.showUnresolvedLinks !== false;
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
    assignments: 0,
    calls: 0,
    unresolvedReferences: 0,
    includes: 0,
    missingIncludes: 0,
    nodes: 0,
    links: 0,
  };
  for (const node of nodes) {
    stats.nodes += 1;
    if (node.kind === "file" || node.kind === "missingInclude") {
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
    } else if (link.kind === "assignments") {
      stats.assignments += 1;
    } else if (link.kind === "calls") {
      stats.calls += 1;
    } else if (link.kind === "unresolvedReference") {
      stats.unresolvedReferences += 1;
    }
  }
  return stats;
}

function fileGraphNodeId(fileName: string): string {
  return `file:${graphFileKey(fileName)}`;
}

function fileGraphNodeIdFromUri(uri: string): string {
  return fileGraphNodeId(graphFileNameFromUri(uri));
}

function declarationGraphNodeId(id: string): string {
  return `vb:${id}`;
}

function unresolvedGraphNodeId(name: string, role?: string): string {
  const prefix = isCallableUnresolvedRole(role) ? "unresolved-call" : "unresolved";
  return `${prefix}:${name.toLowerCase()}`;
}

function isCallableUnresolvedRole(role: string | undefined): boolean {
  return role === "function" || role === "procedure" || role === "unknown";
}

function memberReferenceGraphNodeId(memberPath: string): string {
  return `member:${memberPath.toLowerCase()}`;
}

function scopeGraphNodeId(
  state: AspGraphBuildState,
  uri: string,
  scopeId: string | undefined,
): string {
  return scopeId && state.declarations.has(scopeId)
    ? declarationGraphNodeId(scopeId)
    : fileGraphNodeIdFromUri(uri);
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
    for (const symbol of await referenceCodeLensSymbolsForCachedDocumentAsync(
      cached,
      documentSettings,
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

function shouldShowVbReferenceCodeLens(
  symbol: VbSymbol,
  sourceUri: string,
  settings: AspSettings["codeLens"],
): boolean {
  if (!sameFileIdentityUri(symbol.sourceUri, sourceUri)) {
    return false;
  }
  if (["function", "sub", "method", "property"].includes(symbol.kind)) {
    return settings?.referenceProcedures !== false;
  }
  if (symbol.kind === "class") {
    return settings?.referenceClasses !== false;
  }
  if (symbol.kind === "variable" || symbol.kind === "constant") {
    if (symbol.memberOf) {
      return settings?.referenceClassMembers !== false;
    }
    return !symbol.scopeName && settings?.referenceGlobals !== false;
  }
  if (symbol.kind === "field") {
    return settings?.referenceClassMembers !== false;
  }
  return false;
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
  const executionOptions = workspaceVbReferenceCodeLensExecutionOptions(settings);
  const references = await resolveCodeLensReferencesWithProgress(
    cached,
    symbol,
    settings,
    options,
    executionOptions,
  );
  const localizer = localizerForUri(cached.source.uri);
  const referenceTitle = localizer.t(
    references.length === 1 ? "server.codeLens.reference" : "server.codeLens.references",
    { count: references.length },
  );
  const title =
    settings.codeLens?.referenceScope === "workspace"
      ? referenceTitle
      : `${referenceTitle}${localizer.t("server.codeLens.analyzedOnlySuffix")}`;
  return {
    ...lens,
    command: {
      title,
      command: "aspLsp.showReferences",
      arguments: [
        cached.source.uri,
        symbol.range.start,
        references.map((reference) => Location.create(reference.uri, reference.range)),
      ],
    },
  };
}

async function resolveCodeLensReferencesWithProgress(
  cached: CachedDocument,
  symbol: VbSymbol,
  settings: AspSettings,
  options: VbReferenceOptions,
  executionOptions: WorkspaceVbReferenceExecutionOptions,
): Promise<VbReference[]> {
  if (settings.codeLens?.referenceScope !== "workspace") {
    return analyzedVbscriptReferencesForSymbolAsync(cached, symbol, settings, options);
  }
  return withProgressTaskAsync(
    "analyzing",
    "references.count",
    {
      current: 0,
      total: settings.codeLens?.includeRelatedIncludeTreesForUnresolved === true ? 3 : 2,
      detail: progressFileLabelFromUri(cached.source.uri),
      activeItems: [symbol.name],
    },
    async (task) => {
      task.update({
        label: "references.workspace",
        current: 0,
        activeItems: [symbol.name],
      });
      const workspaceReferences = await workspaceVbscriptCodeLensReferencesForSymbol(
        cached,
        symbol,
        settings,
        options,
        executionOptions,
      );
      if (settings.codeLens?.includeRelatedIncludeTreesForUnresolved !== true) {
        task.update({
          label: "references.finalize",
          current: 2,
          activeItems: [symbol.name],
        });
        return workspaceReferences;
      }
      task.update({
        label: "references.relatedIncludeTree",
        current: 1,
        activeItems: [symbol.name],
      });
      const relatedReferences = await relatedIncludeTreeVbscriptCodeLensReferencesForSymbol(
        cached,
        symbol,
        settings,
        options,
      );
      task.update({
        label: "references.finalize",
        current: 3,
        activeItems: [symbol.name],
      });
      return relatedReferences
        ? dedupeVbReferences([...workspaceReferences, ...relatedReferences]).sort(vbReferenceOrder)
        : workspaceReferences;
    },
  );
}

function workspaceVbReferenceCodeLensExecutionOptions(
  settings: AspSettings,
): WorkspaceVbReferenceExecutionOptions {
  return settings.codeLens?.includeRelatedIncludeTreesForUnresolved === true
    ? {}
    : { workerMaxDepth: 0 };
}

async function workspaceVbscriptCodeLensReferencesForSymbol(
  cached: CachedDocument,
  symbol: VbSymbol,
  settings: AspSettings,
  options: VbReferenceOptions,
  executionOptions: WorkspaceVbReferenceExecutionOptions,
): Promise<VbReference[]> {
  const targets = await referenceCodeLensSymbolsForCachedDocumentAsync(cached, settings);
  const batchTargets = targets.some((target) => sameVbSymbolIdentity(target, symbol))
    ? targets
    : [...targets, symbol];
  const cachedBatch = workspaceVbscriptCompletedCodeLensBatchReferences(
    cached,
    batchTargets,
    settings,
    options,
    executionOptions,
  );
  if (cachedBatch) {
    return cachedBatch.get(vbscriptReferenceSymbolKey(symbol)) ?? [];
  }
  const references = await workspaceVbscriptReferencesForSymbol(
    cached,
    symbol,
    settings,
    options,
    executionOptions,
  );
  if (batchTargets.length > 1) {
    scheduleWorkspaceVbscriptCodeLensBatchReferences(
      cached,
      batchTargets,
      settings,
      options,
      executionOptions,
    );
  }
  return references;
}

async function relatedIncludeTreeVbscriptCodeLensReferencesForSymbol(
  cached: CachedDocument,
  symbol: VbSymbol,
  settings: AspSettings,
  options: VbReferenceOptions,
): Promise<VbReference[] | undefined> {
  const targetGraphDocument = await graphDocumentFromCachedAsync(cached, settings);
  const documentsForGraph = await collectIncludeTreeGraphDocumentsAsync(
    targetGraphDocument,
    settings,
    neverCancelled,
  );
  if (
    !(await graphDocumentsNeedRelatedIncludeTreeAnalysisAsync(
      documentsForGraph,
      settings,
      neverCancelled,
    ))
  ) {
    return undefined;
  }
  const ownerDocuments = await collectRelatedIncludeTreeOwnerGraphDocumentsAsync(
    [targetGraphDocument],
    settings,
    neverCancelled,
    {
      excludedFileKeys: new Set(
        documentsForGraph.map((document) => graphFileKey(document.fileName)),
      ),
    },
  );
  if (ownerDocuments.length === 0) {
    return undefined;
  }
  const candidates = ownerDocuments.map(vbReferenceCandidateFromGraphDocument);
  return workspaceVbscriptReferencesForSymbolWithCandidates(
    cached,
    symbol,
    settings,
    options,
    candidates,
    { logSymbol: `${symbol.name} (include family)` },
  );
}

async function workspaceVbscriptReferencesForSymbolWithCandidates(
  cached: CachedDocument,
  symbol: VbSymbol,
  settings: AspSettings,
  options: VbReferenceOptions,
  candidates: VbReferencesWorkerCandidate[],
  logOptions: { logSymbol?: string },
): Promise<VbReference[]> {
  const context = await localVbReferenceContextAsync(cached, settings);
  const target = equivalentVbSymbol(context.symbols ?? [], symbol) ?? symbol;
  const referencesByTarget = new Map<string, VbReference[]>([
    [vbscriptReferenceSymbolKey(symbol), []],
  ]);
  const requestedKey = vbscriptReferenceSymbolKey(symbol);
  addVbReferencesToArray(
    referencesByTarget,
    requestedKey,
    getVbscriptReferencesForSymbol(target, context, options),
  );

  const targetForWorkers = [vbReferencesWorkerTargetSymbol(target)];
  const workerTargetKey = vbReferencesWorkerTargetKey(targetForWorkers[0]);
  logDebugSummary(
    settings,
    `[asp-lsp] vb.references.includeFamily.candidates: ${cached.source.uri}, symbol=${logOptions.logSymbol ?? target.name}, candidates=${candidates.length}`,
  );
  const summaryFastPath = await workspaceVbReferenceSummaryFastPath(
    candidates,
    targetForWorkers,
    settings,
  );
  for (const [key, references] of summaryFastPath.referencesByTarget) {
    addVbReferencesToArray(
      referencesByTarget,
      key === workerTargetKey ? requestedKey : key,
      references,
    );
  }
  const openDocuments = vbReferencesWorkerOpenDocuments();
  const openDocumentsKey = vbReferencesWorkerOpenDocumentsKey(openDocuments);
  const workerResponses = await Promise.all(
    summaryFastPath.workerCandidates.map((candidate) =>
      workspaceVbReferenceWorkerBatchResponse(
        candidate,
        targetForWorkers,
        settings,
        options,
        openDocuments,
        openDocumentsKey,
        {},
      ),
    ),
  );
  for (const response of workerResponses) {
    addVbReferencesToArray(
      referencesByTarget,
      requestedKey,
      response.referencesByTarget?.[workerTargetKey] ?? [],
    );
  }
  return dedupeVbReferences(referencesByTarget.get(requestedKey) ?? []).sort(vbReferenceOrder);
}

function vbReferenceCandidateFromGraphDocument(
  document: AspGraphDocument,
): VbReferencesWorkerCandidate {
  const openDocument = openDocumentForFileName(document.fileName);
  return {
    uri: document.uri,
    fileName: document.fileName,
    source: {
      ...document.source,
      openVersion: openDocument?.version,
    },
  };
}

async function referenceCodeLensSymbolsForCachedDocumentAsync(
  cached: CachedDocument,
  settings: AspSettings,
): Promise<VbSymbol[]> {
  // Key the CodeLens symbol cache on cheap document identity (version + parse
  // generation) instead of hashing every embedded VBScript region. The content
  // hash walked `cached.parsed.text` once per region, which is pathologically
  // slow when the open document text is a non-flat (rope) string, and is
  // redundant here: `cached.analysis` is recreated on every reparse and
  // `cached.source.version` already changes on every edit.
  const key = JSON.stringify({
    uri: cached.source.uri,
    version: cached.source.version,
    generation: cached.generation,
    settings: {
      codeLens: settings.codeLens,
      vbscript: vbProjectContextSettings(settings),
    },
  });
  const existing = cached.analysis?.referenceCodeLensSymbols;
  if (existing?.key === key) {
    return existing.symbols;
  }
  await hydrateCachedVbscriptCstAsync(cached, settings, "codeLens");
  // CodeLens only needs symbol identity (kind/name/range/scope), not inferred
  // types, so skip the type-inference passes to keep collection cheap.
  const symbols = (
    await collectVbscriptSymbolsAsync(cached.parsed, vbProjectContextSettings(settings), {
      inferTypes: false,
      variantFallback: false,
    })
  ).filter((item) => shouldShowVbReferenceCodeLens(item, cached.source.uri, settings.codeLens));
  analysisFor(cached).referenceCodeLensSymbols = { key, symbols };
  return symbols;
}

async function workspaceVbscriptCodeLensBatchReferences(
  cached: CachedDocument,
  targets: VbSymbol[],
  settings: AspSettings,
  options: VbReferenceOptions,
  executionOptions: WorkspaceVbReferenceExecutionOptions,
): Promise<Map<string, VbReference[]>> {
  const openDocuments = vbReferencesWorkerOpenDocuments();
  const openDocumentsKey = vbReferencesWorkerOpenDocumentsKey(openDocuments);
  const key = workspaceVbReferenceBatchCacheKey(
    cached,
    targets,
    settings,
    options,
    openDocumentsKey,
    executionOptions,
  );
  const cachedBatch = workspaceVbReferenceBatchCompleted.get(key);
  if (cachedBatch) {
    cachedBatch.lastUsed = Date.now();
    logDebugSummary(settings, `[asp-lsp] vb.references.batch.cache.hit: ${cached.source.uri}`);
    return cachedBatch.referencesByTarget;
  }
  const inFlight = workspaceVbReferenceBatchInFlight.get(key);
  if (inFlight) {
    logDebugSummary(settings, `[asp-lsp] vb.references.batch.reuse: ${cached.source.uri}`);
    return (await inFlight.promise).referencesByTarget;
  }
  const promise = workspaceVbscriptReferencesForSymbols(
    cached,
    targets,
    settings,
    options,
    {
      logSymbol: "(codelens-batch)",
    },
    executionOptions,
  ).then((referencesByTarget) => {
    const result = { key, referencesByTarget, lastUsed: Date.now() };
    workspaceVbReferenceBatchCompleted.set(key, result);
    pruneWorkspaceVbReferenceBatchCompleted();
    logDebugSummary(
      settings,
      `[asp-lsp] vb.references.batch.complete: ${cached.source.uri}, symbols=${targets.length}`,
    );
    return result;
  });
  workspaceVbReferenceBatchInFlight.set(key, { key, promise });
  try {
    return (await promise).referencesByTarget;
  } finally {
    if (workspaceVbReferenceBatchInFlight.get(key)?.promise === promise) {
      workspaceVbReferenceBatchInFlight.delete(key);
    }
  }
}

function workspaceVbscriptCompletedCodeLensBatchReferences(
  cached: CachedDocument,
  targets: VbSymbol[],
  settings: AspSettings,
  options: VbReferenceOptions,
  executionOptions: WorkspaceVbReferenceExecutionOptions,
): Map<string, VbReference[]> | undefined {
  const openDocuments = vbReferencesWorkerOpenDocuments();
  const openDocumentsKey = vbReferencesWorkerOpenDocumentsKey(openDocuments);
  const key = workspaceVbReferenceBatchCacheKey(
    cached,
    targets,
    settings,
    options,
    openDocumentsKey,
    executionOptions,
  );
  const cachedBatch = workspaceVbReferenceBatchCompleted.get(key);
  if (!cachedBatch) {
    return undefined;
  }
  cachedBatch.lastUsed = Date.now();
  logDebugSummary(settings, `[asp-lsp] vb.references.batch.cache.hit: ${cached.source.uri}`);
  return cachedBatch.referencesByTarget;
}

function scheduleWorkspaceVbscriptCodeLensBatchReferences(
  cached: CachedDocument,
  targets: VbSymbol[],
  settings: AspSettings,
  options: VbReferenceOptions,
  executionOptions: WorkspaceVbReferenceExecutionOptions,
): void {
  setTimeout(() => {
    void workspaceVbscriptCodeLensBatchReferences(
      cached,
      targets,
      settings,
      options,
      executionOptions,
    ).catch((error: unknown) => {
      logServerWarning(
        `[asp-lsp] vb.references.batch.failed: ${cached.source.uri}, error=${errorMessage(error)}`,
        settings,
      );
    });
  }, 0);
}

function workspaceVbReferenceBatchCacheKey(
  cached: CachedDocument,
  targets: VbSymbol[],
  settings: AspSettings,
  options: VbReferenceOptions,
  openDocumentsKey: string,
  executionOptions: WorkspaceVbReferenceExecutionOptions,
): string {
  return JSON.stringify({
    scope: "workspaceCodeLens",
    source: {
      uri: fileIdentityKeyFromUri(cached.source.uri),
      version: cached.source.version,
      text: textFingerprint(cached.source.getText()),
      parsed: vbProjectDocumentFingerprint(cached.parsed),
    },
    targets: targets.map(vbscriptReferenceSymbolKey).sort(),
    settings: {
      parse: parseSettingsIdentity(settings),
      include: includeResolutionSettingsIdentity(settings),
      vbscript: vbProjectContextSettings(settings),
      legacyEncoding: settings.legacyEncoding,
    },
    options: workspaceVbReferenceWorkerOptions(options),
    executionOptions,
    workspaceGeneration,
    openDocuments: openDocumentsKey,
  });
}

function vbReferenceCodeLensData(symbol: VbSymbol): VbReferenceCodeLensData {
  return {
    kind: "vbscript-reference",
    uri: symbol.sourceUri,
    name: symbol.name,
    symbolKind: symbol.kind,
    memberOf: symbol.memberOf,
    scopeName: symbol.scopeName,
    propertyAccessor: symbol.propertyAccessor,
    line: symbol.range.start.line,
    character: symbol.range.start.character,
    endLine: symbol.range.end.line,
    endCharacter: symbol.range.end.character,
  };
}

function vbReferenceCodeLensDataFromUnknown(value: unknown): VbReferenceCodeLensData | undefined {
  if (!value || typeof value !== "object") {
    return undefined;
  }
  const data = value as Partial<VbReferenceCodeLensData>;
  if (
    data.kind !== "vbscript-reference" ||
    typeof data.uri !== "string" ||
    typeof data.name !== "string" ||
    typeof data.symbolKind !== "string" ||
    typeof data.line !== "number" ||
    typeof data.character !== "number"
  ) {
    return undefined;
  }
  return {
    kind: "vbscript-reference",
    uri: data.uri,
    name: data.name,
    symbolKind: data.symbolKind,
    memberOf: typeof data.memberOf === "string" ? data.memberOf : undefined,
    scopeName: typeof data.scopeName === "string" ? data.scopeName : undefined,
    propertyAccessor:
      data.propertyAccessor === "get" ||
      data.propertyAccessor === "let" ||
      data.propertyAccessor === "set"
        ? data.propertyAccessor
        : undefined,
    line: data.line,
    character: data.character,
    endLine: typeof data.endLine === "number" ? data.endLine : undefined,
    endCharacter: typeof data.endCharacter === "number" ? data.endCharacter : undefined,
  };
}

async function vbSymbolForCodeLensDataAsync(
  cached: CachedDocument,
  data: VbReferenceCodeLensData,
): Promise<VbSymbol | undefined> {
  const settings = cachedSettings(data.uri);
  const symbols = await referenceCodeLensSymbolsForCachedDocumentAsync(cached, settings);
  const candidates = symbols.filter((symbol) => vbSymbolMatchesCodeLensIdentity(symbol, data));
  const exact = candidates.find(
    (symbol) =>
      symbol.range.start.line === data.line &&
      symbol.range.start.character === data.character &&
      (data.endLine === undefined || symbol.range.end.line === data.endLine) &&
      (data.endCharacter === undefined || symbol.range.end.character === data.endCharacter),
  );
  if (exact) {
    return exact;
  }
  return candidates.length === 1 ? candidates[0] : undefined;
}

function vbSymbolMatchesCodeLensIdentity(symbol: VbSymbol, data: VbReferenceCodeLensData): boolean {
  return (
    sameFileIdentityUri(symbol.sourceUri, data.uri) &&
    symbol.name.toLowerCase() === data.name.toLowerCase() &&
    symbol.kind === data.symbolKind &&
    (symbol.memberOf ?? "").toLowerCase() === (data.memberOf ?? "").toLowerCase() &&
    (data.scopeName === undefined ||
      (symbol.scopeName ?? "").toLowerCase() === data.scopeName.toLowerCase()) &&
    (data.propertyAccessor === undefined || symbol.propertyAccessor === data.propertyAccessor)
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
    if (sameFileIdentityUri(candidate.source.uri, cached.source.uri)) {
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
        await cachedFileAnalysisSummaryAsync(
          candidate,
          vbProjectContextSettings(settings),
          settings,
        ),
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
  const contextSettings = withCachedVbBuiltinRuntime(cached, vbProjectContextSettings(settings));
  await hydrateCachedVbscriptCstAsync(cached, settings, "references");
  const symbols = await collectVbscriptSymbolsAsync(cached.parsed, contextSettings);
  const summary = await cachedFileAnalysisSummaryAsync(cached, contextSettings, settings);
  return {
    documents: [cached.parsed],
    includeSummaryUris: [cached.source.uri],
    symbols,
    typeEnvironment: buildVbTypeEnvironment(cached.parsed, { ...contextSettings, symbols }),
    externalRefUsages: summaryVbReferenceUsages(summary),
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
      tsFormatOptions(
        formatOptions(formattingOptions, cachedSettings(cached.source.uri)),
        context.virtual.languageId === "jscript" ? "jscript" : "javascript",
      ),
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
  const contextState = await measureDebugStepAsync(
    settings,
    cached.source.uri,
    "semanticTokens.context",
    () => semanticTokensVbProjectContextAsync(cached, settings),
  );
  return buildSemanticTokensWithContextAsync(cached, contextState.context, range, settings, {
    cacheFull: contextState.cacheFull,
  });
}

async function buildFullSemanticTokenDataAsync(
  cached: CachedDocument,
  previous: SemanticTokenResultEntry | undefined,
): Promise<number[]> {
  const reused = await buildIncrementalFullSemanticTokenDataAsync(cached, previous);
  if (reused) {
    return reused;
  }
  return (await buildSemanticTokensAsync(cached)).data;
}

async function buildIncrementalFullSemanticTokenDataAsync(
  cached: CachedDocument,
  previous: SemanticTokenResultEntry | undefined,
): Promise<number[] | undefined> {
  const settings = cachedSettings(cached.source.uri);
  if (!canReuseSemanticTokenResult(cached, settings, previous)) {
    return undefined;
  }
  const change = cached.lastIncrementalChange;
  if (!change) {
    return undefined;
  }
  const startedAt = process.hrtime.bigint();
  const dirty = semanticTokenDirtyRanges(cached, change);
  const contextState = await measureDebugStepAsync(
    settings,
    cached.source.uri,
    "semanticTokens.context",
    () => semanticTokensVbProjectContextAsync(cached, settings),
  );
  if (!contextState.cacheFull) {
    return undefined;
  }
  const rangeTokens = await buildSemanticTokensWithContextAsync(
    cached,
    contextState.context,
    dirty.current,
    settings,
    { cacheFull: false },
  );
  const merged = mergeIncrementalSemanticTokenData(previous!.data, rangeTokens.data, dirty, change);
  const result = semanticTokensFromData(merged).data;
  const jsVirtuals = jsVirtualDocuments(cached);
  const fullCacheKey = semanticTokensFullCacheKey(cached, settings, jsVirtuals);
  analysisFor(cached).semanticTokensFull = {
    key: fullCacheKey,
    data: [...result],
  };
  finishDebugStep(settings, cached.source.uri, "semanticTokens.full.incrementalReuse", startedAt);
  return result;
}

function canReuseSemanticTokenResult(
  cached: CachedDocument,
  settings: AspSettings,
  previous: SemanticTokenResultEntry | undefined,
): previous is SemanticTokenResultEntry & { version: number; reuseKey: string } {
  return (
    previous !== undefined &&
    sameFileIdentityUri(previous.uri, cached.source.uri) &&
    previous.version !== undefined &&
    previous.version + 1 === cached.identity.version &&
    previous.reuseKey === semanticTokensReuseKey(cached, settings) &&
    previous.vbscriptFingerprint === vbscriptRegionContentFingerprint(cached.parsed) &&
    previous.includeRefsKey === semanticIncludeRefsKey(cached.parsed) &&
    cached.lastEditImpact?.kind === "incremental" &&
    cached.lastIncrementalChange !== undefined &&
    canReuseSemanticTokensAcrossDirtyScope(cached.lastEditImpact)
  );
}

function canReuseSemanticTokensAcrossDirtyScope(impact: AspEditImpact): boolean {
  const language = impact.dirtyScope?.language ?? impact.language;
  return (
    impact.dirtyScope?.structuralRisk !== true &&
    language !== undefined &&
    language !== "vbscript" &&
    language !== "mixed" &&
    language !== "asp-directive"
  );
}

async function buildSemanticTokensWithContextAsync(
  cached: CachedDocument,
  vbContext: VbProjectContext,
  range?: Range,
  settings = cachedSettings(cached.source.uri),
  options: { cacheFull?: boolean } = {},
): Promise<SemanticTokens> {
  const full = !range;
  const cacheFull = options.cacheFull !== false;
  const jsVirtuals = jsVirtualDocuments(cached);
  const fullCacheKey = full ? semanticTokensFullCacheKey(cached, settings, jsVirtuals) : undefined;
  const analysis = full && cacheFull ? analysisFor(cached) : undefined;
  if (fullCacheKey && analysis?.semanticTokensFull?.key === fullCacheKey) {
    logDebugSummary(settings, `[asp-lsp] semanticTokens.full.cacheHit: ${cached.source.uri}`);
    return { data: [...analysis.semanticTokensFull.data] };
  }
  const rangeStart = range ? cached.source.offsetAt(range.start) : 0;
  const rangeEnd = range ? cached.source.offsetAt(range.end) : cached.source.getText().length;
  const tokens: SemanticTokenData[] = [];
  measureDebugStep(settings, cached.source.uri, "semanticTokens.aspRegions", () => {
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
      }
    }
  });
  const vbSemanticTokens = measureDebugStep(
    settings,
    cached.source.uri,
    "semanticTokens.vbscript.raw",
    () => getVbscriptSemanticTokens(cached.parsed, vbContext, range),
  );
  measureDebugStep(settings, cached.source.uri, "semanticTokens.vbscript.map", () => {
    for (const semanticToken of vbSemanticTokens) {
      const offset = cached.source.offsetAt(semanticToken.range.start);
      if (offset < rangeStart || offset >= rangeEnd) {
        continue;
      }
      tokens.push({
        line: semanticToken.range.start.line,
        character: semanticToken.range.start.character,
        length: Math.max(
          1,
          semanticToken.range.end.character - semanticToken.range.start.character,
        ),
        tokenType: semanticToken.tokenType,
        tokenModifiers: semanticToken.tokenModifiers,
      });
    }
  });
  measureDebugStep(settings, cached.source.uri, "semanticTokens.fallback.group", () =>
    addFallbackVbSemanticTokens(tokens, cached, vbContext, rangeStart, rangeEnd),
  );
  measureDebugStep(settings, cached.source.uri, "semanticTokens.includes", () =>
    addIncludeSemanticTokens(tokens, cached, rangeStart, rangeEnd),
  );
  const javascriptCacheKey = semanticJavascriptTokensCacheKey(cached, settings, jsVirtuals);
  const javascriptDeferred = await measureDebugStepAsync(
    settings,
    cached.source.uri,
    "semanticTokens.embedded",
    () =>
      addEmbeddedSemanticTokensAsync(tokens, cached, rangeStart, rangeEnd, {
        settings,
        jsVirtuals,
        deferLargeJavascript: full,
        javascriptCacheKey,
      }),
  );
  const uniqueTokens = measureDebugStep(settings, cached.source.uri, "semanticTokens.dedupe", () =>
    sortAndDedupeSemanticTokens(tokens),
  );
  const result = measureDebugStep(settings, cached.source.uri, "semanticTokens.encode", () =>
    semanticTokensFromData(uniqueTokens),
  );
  if (fullCacheKey && !javascriptDeferred && cacheFull) {
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
      !sameFileIdentityUri(symbol.sourceUri, cached.parsed.uri) &&
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

interface NormalizedSemanticTokenData {
  line: number;
  character: number;
  length: number;
  tokenType: number;
  tokenModifiers: number;
}

function sortAndDedupeSemanticTokens(
  tokens: readonly SemanticTokenData[],
): NormalizedSemanticTokenData[] {
  const normalized = tokens
    .map((token): NormalizedSemanticTokenData | undefined => {
      const tokenType = semanticTokenTypeIndexes.get(token.tokenType);
      if (tokenType === undefined) {
        return undefined;
      }
      return {
        line: token.line,
        character: token.character,
        length: token.length,
        tokenType,
        tokenModifiers: semanticTokenModifierBitset(token.tokenModifiers),
      };
    })
    .filter((token): token is NormalizedSemanticTokenData => token !== undefined)
    .sort(compareNormalizedSemanticTokens);
  const unique: NormalizedSemanticTokenData[] = [];
  let previous: NormalizedSemanticTokenData | undefined;
  for (const token of normalized) {
    if (!previous || compareNormalizedSemanticTokens(previous, token) !== 0) {
      unique.push(token);
      previous = token;
    }
  }
  return unique;
}

function compareNormalizedSemanticTokens(
  left: NormalizedSemanticTokenData,
  right: NormalizedSemanticTokenData,
): number {
  return (
    left.line - right.line ||
    left.character - right.character ||
    left.length - right.length ||
    left.tokenType - right.tokenType ||
    left.tokenModifiers - right.tokenModifiers
  );
}

function semanticTokensFromData(tokens: readonly NormalizedSemanticTokenData[]): SemanticTokens {
  const builder = new SemanticTokensBuilder();
  for (const token of tokens) {
    builder.push(token.line, token.character, token.length, token.tokenType, token.tokenModifiers);
  }
  return builder.build();
}

function decodeSemanticTokenData(data: readonly number[]): NormalizedSemanticTokenData[] {
  const tokens: NormalizedSemanticTokenData[] = [];
  let line = 0;
  let character = 0;
  for (let index = 0; index + 4 < data.length; index += 5) {
    line += data[index];
    character = data[index] === 0 ? character + data[index + 1] : data[index + 1];
    tokens.push({
      line,
      character,
      length: data[index + 2],
      tokenType: data[index + 3],
      tokenModifiers: data[index + 4],
    });
  }
  return tokens;
}

function sortAndDedupeNormalizedSemanticTokens(
  tokens: readonly NormalizedSemanticTokenData[],
): NormalizedSemanticTokenData[] {
  const sorted = [...tokens].sort(compareNormalizedSemanticTokens);
  const unique: NormalizedSemanticTokenData[] = [];
  let previous: NormalizedSemanticTokenData | undefined;
  for (const token of sorted) {
    if (!previous || compareNormalizedSemanticTokens(previous, token) !== 0) {
      unique.push(token);
      previous = token;
    }
  }
  return unique;
}

interface SemanticTokenDirtyRanges {
  previousStartLine: number;
  previousEndLine: number;
  currentStartLine: number;
  currentEndLine: number;
  current: Range;
}

function semanticTokenDirtyRanges(
  cached: CachedDocument,
  change: AspIncrementalChange,
): SemanticTokenDirtyRanges {
  const insertedLineCount = countNewlines(change.text);
  const previousStartLine = Math.max(0, change.range.start.line - 1);
  const previousEndLine = change.range.end.line + 1;
  const currentStartLine = previousStartLine;
  const currentEndLine = Math.min(
    cached.source.lineCount - 1,
    change.range.start.line + insertedLineCount + 1,
  );
  return {
    previousStartLine,
    previousEndLine,
    currentStartLine,
    currentEndLine,
    current: {
      start: { line: currentStartLine, character: 0 },
      end: {
        line: currentEndLine,
        character: lineText(cached.source, currentEndLine).length,
      },
    },
  };
}

function mergeIncrementalSemanticTokenData(
  previousData: readonly number[],
  rangeData: readonly number[],
  dirty: SemanticTokenDirtyRanges,
  change: AspIncrementalChange,
): NormalizedSemanticTokenData[] {
  const lineDelta = countNewlines(change.text) - (change.range.end.line - change.range.start.line);
  const retained = decodeSemanticTokenData(previousData).flatMap((token) => {
    if (token.line >= dirty.previousStartLine && token.line <= dirty.previousEndLine) {
      return [];
    }
    if (token.line > dirty.previousEndLine) {
      return [{ ...token, line: token.line + lineDelta }];
    }
    return [token];
  });
  const currentRangeTokens = decodeSemanticTokenData(rangeData).filter(
    (token) => token.line >= dirty.currentStartLine && token.line <= dirty.currentEndLine,
  );
  return sortAndDedupeNormalizedSemanticTokens([...retained, ...currentRangeTokens]);
}

function countNewlines(text: string): number {
  let count = 0;
  for (const character of text) {
    if (character === "\n") {
      count += 1;
    }
  }
  return count;
}

function semanticTokenModifierBitset(modifiers: readonly string[] | undefined): number {
  let bitset = 0;
  for (const modifier of modifiers ?? []) {
    const modifierBitset = semanticTokenModifierBitsets.get(modifier);
    if (modifierBitset !== undefined) {
      bitset |= modifierBitset;
    }
  }
  return bitset;
}

function cacheSemanticTokens(
  uri: string,
  data: number[],
  metadata: Omit<SemanticTokenResultEntry, "uri" | "data"> = {},
): SemanticTokens {
  const uriKey = semanticTokenUriKey(uri);
  const previous = latestSemanticTokenResultByUri.get(uriKey);
  if (previous) {
    semanticTokenResults.delete(previous);
  }
  const retained = latestRetainedSemanticTokenResultByUri.get(uriKey);
  if (retained) {
    retainedSemanticTokenResults.delete(retained);
    latestRetainedSemanticTokenResultByUri.delete(uriKey);
  }
  const resultId = nextSemanticTokenResultId();
  semanticTokenResults.set(resultId, { uri, data, ...metadata });
  latestSemanticTokenResultByUri.set(uriKey, resultId);
  return { data, resultId };
}

function semanticTokenEntryById(resultId: string): SemanticTokenResultEntry | undefined {
  return semanticTokenResults.get(resultId) ?? retainedSemanticTokenResults.get(resultId);
}

function latestSemanticTokenEntryForUri(uri: string): SemanticTokenResultEntry | undefined {
  const uriKey = semanticTokenUriKey(uri);
  const resultId = latestSemanticTokenResultByUri.get(uriKey);
  if (resultId) {
    return semanticTokenResults.get(resultId);
  }
  const retainedResultId = latestRetainedSemanticTokenResultByUri.get(uriKey);
  return retainedResultId ? retainedSemanticTokenResults.get(retainedResultId) : undefined;
}

function semanticTokenResultMetadata(
  cached: CachedDocument,
  settings: AspSettings,
): Omit<SemanticTokenResultEntry, "uri" | "data"> {
  return {
    reuseKey: semanticTokensReuseKey(cached, settings),
    version: cached.identity.version,
    generation: cached.generation,
    vbscriptFingerprint: vbscriptRegionContentFingerprint(cached.parsed),
    includeRefsKey: semanticIncludeRefsKey(cached.parsed),
  };
}

function semanticIncludeRefsKey(parsed: AspParsedDocument): string {
  return JSON.stringify(
    parsed.includes.map((include) => ({
      offset: include.offset,
      path: include.path,
      mode: include.mode,
    })),
  );
}

function semanticTokensReuseKey(cached: CachedDocument, settings: AspSettings): string {
  return JSON.stringify({
    parseSettings: cached.parseSettingsIdentity,
    includeResolution: includeResolutionSettingsIdentity(settings),
    jsProject: jsProjectSettingsIdentity(settings),
    diagnostics: diagnosticsIdentity(settings),
  });
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
  const uriKey = semanticTokenUriKey(uri);
  const resultId = latestSemanticTokenResultByUri.get(uriKey);
  if (resultId) {
    const entry = semanticTokenResults.get(resultId);
    if (entry) {
      retainSemanticTokenResult(resultId, entry);
    }
    semanticTokenResults.delete(resultId);
    latestSemanticTokenResultByUri.delete(uriKey);
  }
}

function retainSemanticTokenResult(resultId: string, entry: SemanticTokenResultEntry): void {
  const uriKey = semanticTokenUriKey(entry.uri);
  const previous = latestRetainedSemanticTokenResultByUri.get(uriKey);
  if (previous && previous !== resultId) {
    retainedSemanticTokenResults.delete(previous);
  }
  retainedSemanticTokenResults.set(resultId, entry);
  latestRetainedSemanticTokenResultByUri.set(uriKey, resultId);
  while (retainedSemanticTokenResults.size > maxRetainedSemanticTokenResults) {
    const oldestResultId = retainedSemanticTokenResults.keys().next().value;
    if (!oldestResultId) {
      break;
    }
    const oldest = retainedSemanticTokenResults.get(oldestResultId);
    retainedSemanticTokenResults.delete(oldestResultId);
    if (
      oldest &&
      latestRetainedSemanticTokenResultByUri.get(semanticTokenUriKey(oldest.uri)) === oldestResultId
    ) {
      latestRetainedSemanticTokenResultByUri.delete(semanticTokenUriKey(oldest.uri));
    }
  }
}

function semanticTokenUriKey(uri: string): string {
  return uri.startsWith("file://") ? fileIdentityKeyFromUri(uri) : uri;
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
  return jsVirtuals.map((virtual) => {
    const sourceVersion = jsVirtualOpenSourceVersion(virtual);
    return {
      uri: virtual.uri,
      languageId: virtual.languageId,
      sourceVersion,
      text: sourceVersion ? undefined : textFingerprint(virtual.text),
    };
  });
}

function jsVirtualOpenSourceVersion(
  virtual: VirtualDocument,
): { uri: string; version: number } | undefined {
  const sourceUri = virtualSourceUri(virtual);
  const open = openDocumentForUri(sourceUri);
  return open ? { uri: fileIdentityKeyFromUri(sourceUri), version: open.version } : undefined;
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
  measureDebugStep(options.settings, cached.source.uri, "semanticTokens.embedded.css", () => {
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
  });
  const shouldDeferJavascript = measureDebugStep(
    options.settings,
    cached.source.uri,
    "semanticTokens.embedded.deferDecision",
    () =>
      options.jsVirtuals.length > 0 &&
      options.deferLargeJavascript &&
      shouldDeferFullJavascriptSemanticTokens(cached, options.jsVirtuals),
  );
  if (shouldDeferJavascript) {
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
    ...(await measureDebugStepAsync(
      options.settings,
      cached.source.uri,
      "semanticTokens.embedded.javascript",
      () =>
        computeJavascriptSemanticTokensAsync(
          cached,
          options.settings,
          jsVirtuals,
          rangeStart,
          rangeEnd,
        ),
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
    setTimeout(() => {
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
    }, semanticTokensDeferredWorkDelayMs);
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
    logServerWarning(
      `[asp-lsp] semanticTokens.javascript.cache.failed: ${cached.source.uri}, error=${errorMessage(error)}`,
      settings,
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
  await vbReferencesWorkerPool?.close();
  vbReferencesWorkerPool = undefined;
  await bulkWorkerPool?.close();
  bulkWorkerPool = undefined;
  clearWorkspaceVbReferenceCaches();
});

documents.listen(connection);
connection.listen();
