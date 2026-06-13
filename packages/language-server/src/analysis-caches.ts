import ts from "typescript";
import type { VbReferencesWorkerResponse } from "./vb-references-protocol";
import type {
  AspParsedDocument,
  FileAnalysisSummary,
  VbExternalRefUsage,
  VbProjectContext,
  VbReference,
  VbSymbol,
  VbTypeEnvironment,
  VirtualDocument,
} from "@asp-lsp/core";
import type {
  AspGraphPayload,
  GraphFileIndex,
  WorkspaceVbReferenceExecutionOptions,
  WorkspaceVbReferenceSummaryIncludeGraph,
} from "./asp-graph/types";
import type { CachedTsDiagnostic } from "./document-store";
import {
  estimateJsonBytes,
  estimateStringArrayBytes,
  estimateStringBytes,
  SizedLruCache,
} from "./memory-budget";

export interface JsFileStat {
  mtimeMs: number;
  size: number;
  isFile: boolean;
}

export interface JsProjectFile {
  fileName: string;
  text: string;
  version: string;
  uri: string;
  virtual?: VirtualDocument;
  snapshot?: ts.IScriptSnapshot;
}

export interface JsProjectContext {
  virtual: VirtualDocument;
  service: ts.LanguageService;
  fileName: string;
  offset: number;
  files: Map<string, JsProjectFile>;
}

export interface JsLanguageServiceProject {
  service: ts.LanguageService;
  host: ts.LanguageServiceHost;
  files: Map<string, JsProjectFile>;
  options: ts.CompilerOptions;
  currentDirectory: string;
  moduleResolutionCache: ts.ModuleResolutionCache;
  optionsKey: string;
  projectVersion: number;
}

export interface JsLanguageServiceCacheEntry {
  project: JsLanguageServiceProject;
  lastUsed: number;
}

export interface JsOpenProjectFilesCacheEntry {
  files: Map<string, JsProjectFile>;
  lastUsed: number;
}

export interface JsProjectConfigCacheEntry {
  config: JsProjectConfig;
  lastUsed: number;
}

export interface JsProjectConfig {
  fileNames: string[];
  options: ts.CompilerOptions;
  currentDirectory: string;
}

export interface VbProjectContextCacheEntry {
  context: VbProjectContext;
  lastUsed: number;
}

export interface VbProjectAnalysis {
  documents: AspParsedDocument[];
  summaryUris: string[];
  summaries: FileAnalysisSummary[];
  summaryGraphKey: string;
  complete: boolean;
  symbols: VbSymbol[];
  typeEnvironment: VbTypeEnvironment;
  externalRefUsages: VbExternalRefUsage[];
}

export interface VbProjectSummaryGraph {
  rootSummary: FileAnalysisSummary;
  summaries: FileAnalysisSummary[];
  documents: AspParsedDocument[];
  key: string;
  complete: boolean;
  missingFiles: string[];
  truncatedReason?: string;
  textLength: number;
}

export interface WorkspaceVbReferenceWorkerTask {
  key: string;
  promise: Promise<VbReferencesWorkerResponse>;
}

export interface WorkspaceVbReferenceWorkerBatchTask {
  key: string;
  promise: Promise<VbReferencesWorkerResponse>;
}

export interface WorkspaceVbReferenceRequestTask {
  key: string;
  promise: Promise<WorkspaceVbReferenceRequestResult>;
}

export interface WorkspaceVbReferenceRequestResult {
  key: string;
  referencesByTarget: Map<string, VbReference[]>;
  lastUsed: number;
}

export interface AspGraphPayloadCacheEntry {
  payload: AspGraphPayload;
  signature: string;
  lastUsed: number;
}

export interface WorkspaceVbReferenceBatchTask {
  key: string;
  promise: Promise<WorkspaceVbReferenceBatchResult>;
}

export interface WorkspaceVbReferenceBatchResult {
  key: string;
  referencesByTarget: Map<string, VbReference[]>;
  lastUsed: number;
}

export interface WorkspaceVbReferenceReachabilityEntry {
  key: string;
  skippedUris: Set<string>;
  complete: boolean;
  lastUsed: number;
}

export interface WorkspaceVbReferenceReachabilityState {
  reachesTarget: boolean;
  complete: boolean;
  documents: number;
}

export interface WorkspaceVbReferenceReachabilityGraphNode {
  uri: string;
  fileName: string;
  includes: string[];
  complete: boolean;
}

export interface LightweightJsUnusedCacheEntry {
  diagnostics: CachedTsDiagnostic[];
  lastUsed: number;
}

export class AspJsScriptSnapshot implements ts.IScriptSnapshot {
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

export const jsLanguageServiceCache = new SizedLruCache<string, JsLanguageServiceCacheEntry>(
  "js.languageService",
  {
    priority: 30,
    maxEntries: 16,
    estimateEntryBytes: estimateJsLanguageServiceCacheEntryBytes,
    disposeEntry: (_key, entry) => entry.project.service.dispose(),
  },
);
export const jsOpenProjectFilesCache = new SizedLruCache<string, JsOpenProjectFilesCacheEntry>(
  "js.openProjectFiles",
  {
    priority: 25,
    maxEntries: 32,
    estimateEntryBytes: (key, entry) =>
      estimateStringBytes(key) + estimateJsProjectFilesBytes(entry.files) + 64,
  },
);
export const jsProjectConfigCache = new SizedLruCache<string, JsProjectConfigCacheEntry>(
  "js.projectConfig",
  {
    priority: 35,
    maxEntries: 16,
    estimateEntryBytes: (key, entry) =>
      estimateStringBytes(key) +
      estimateStringArrayBytes(entry.config.fileNames) +
      estimateJsonBytes(entry.config.options, 4096) +
      estimateStringBytes(entry.config.currentDirectory) +
      256,
  },
);
export const jsDocumentRegistry = ts.createDocumentRegistry(ts.sys.useCaseSensitiveFileNames);
export const jsScriptSnapshots = new SizedLruCache<string, AspJsScriptSnapshot>(
  "js.scriptSnapshots",
  {
    priority: 20,
    maxEntries: 512,
    estimateEntryBytes: estimateJsScriptSnapshotEntryBytes,
  },
);
export const jsFileExistsCache = new SizedLruCache<string, boolean>("js.fs.fileExists", {
  priority: 15,
  maxEntries: 8192,
  estimateEntryBytes: (key) => estimateStringBytes(key) + 16,
});
export const jsReadFileCache = new SizedLruCache<string, string | undefined>("js.fs.readFile", {
  priority: 10,
  maxEntries: 512,
  estimateEntryBytes: (key, text) => estimateStringBytes(key) + estimateStringBytes(text) + 32,
});
export const jsDirectoryExistsCache = new SizedLruCache<string, boolean>("js.fs.directoryExists", {
  priority: 15,
  maxEntries: 8192,
  estimateEntryBytes: (key) => estimateStringBytes(key) + 16,
});
export const jsDirectoriesCache = new SizedLruCache<string, string[]>("js.fs.directories", {
  priority: 15,
  maxEntries: 2048,
  estimateEntryBytes: (key, entries) =>
    estimateStringBytes(key) + estimateStringArrayBytes(entries) + 64,
});
export const jsReadDirectoryCache = new SizedLruCache<string, string[]>("js.fs.readDirectory", {
  priority: 15,
  maxEntries: 1024,
  estimateEntryBytes: (key, entries) =>
    estimateStringBytes(key) + estimateStringArrayBytes(entries) + 64,
});
export const jsRealpathCache = new SizedLruCache<string, string>("js.fs.realpath", {
  priority: 15,
  maxEntries: 4096,
  estimateEntryBytes: (key, realpath) => estimateStringBytes(key) + estimateStringBytes(realpath),
});
export const jsFileStatCache = new SizedLruCache<string, JsFileStat | undefined>("js.fs.stat", {
  priority: 15,
  maxEntries: 4096,
  estimateEntryBytes: (key, stat) => estimateStringBytes(key) + (stat ? 64 : 16),
});

export const graphFileIndexCache = new Map<string, GraphFileIndex>();
export const graphFileIndexInFlight = new Map<string, Promise<GraphFileIndex>>();
export const graphFileIndexCacheMaxEntries = 64;
export const aspGraphPayloadCache = new SizedLruCache<string, AspGraphPayloadCacheEntry>(
  "graph.payload",
  {
    priority: 30,
    maxEntries: 8,
    estimateEntryBytes: (key, entry) =>
      estimateStringBytes(key) +
      estimateStringBytes(entry.signature) +
      estimateAspGraphPayloadBytes(entry.payload) +
      128,
  },
);

export const vbProjectContextCache = new Map<string, VbProjectContextCacheEntry>();
export const maxVbProjectContextCacheEntries = 32;

export const maxWorkspaceVbReferenceWorkerCacheEntries = 128;
export const maxWorkspaceVbReferenceRequestCacheEntries = 64;
export const maxWorkspaceVbReferenceBatchCacheEntries = 64;
export const maxWorkspaceVbReferenceReachabilityCacheEntries = 2048;
export const maxWorkspaceVbReferenceReachabilityDocuments = 50_000;
export const workspaceVbReferenceReachabilityConcurrency = 256;

export const workspaceVbReferenceWorkerInFlight = new Map<string, WorkspaceVbReferenceWorkerTask>();
export const workspaceVbReferenceWorkerBatchInFlight = new Map<
  string,
  WorkspaceVbReferenceWorkerBatchTask
>();
export const workspaceVbReferenceWorkerCompleted = new Map<string, VbReferencesWorkerResponse>();
export const workspaceVbReferenceRequestInFlight = new Map<
  string,
  WorkspaceVbReferenceRequestTask
>();
export const workspaceVbReferenceRequestCompleted = new Map<
  string,
  WorkspaceVbReferenceRequestResult
>();
export const workspaceVbReferenceBatchInFlight = new Map<string, WorkspaceVbReferenceBatchTask>();
export const workspaceVbReferenceBatchCompleted = new Map<
  string,
  WorkspaceVbReferenceBatchResult
>();
export const workspaceVbReferenceReachabilityInFlight = new Map<
  string,
  Promise<WorkspaceVbReferenceReachabilityEntry>
>();
export const workspaceVbReferenceReachabilityCache = new Map<
  string,
  WorkspaceVbReferenceReachabilityEntry
>();

export const maxJsOpenProjectFilesCacheEntries = 32;
export const maxLightweightJsUnusedCacheEntries = 32;
export const lightweightJsUnusedDiagnosticsCache = new SizedLruCache<
  string,
  LightweightJsUnusedCacheEntry
>("js.lightweightUnusedDiagnostics", {
  priority: 25,
  maxEntries: maxLightweightJsUnusedCacheEntries,
  estimateEntryBytes: (key, entry) =>
    estimateStringBytes(key) + estimateJsonBytes(entry.diagnostics, 1024) + 64,
});

export type { WorkspaceVbReferenceExecutionOptions, WorkspaceVbReferenceSummaryIncludeGraph };

export const memoryManagedAnalysisCaches = [
  jsLanguageServiceCache,
  jsOpenProjectFilesCache,
  jsProjectConfigCache,
  jsScriptSnapshots,
  jsFileExistsCache,
  jsReadFileCache,
  jsDirectoryExistsCache,
  jsDirectoriesCache,
  jsReadDirectoryCache,
  jsRealpathCache,
  jsFileStatCache,
  lightweightJsUnusedDiagnosticsCache,
  aspGraphPayloadCache,
];

function estimateJsLanguageServiceCacheEntryBytes(
  key: string,
  entry: JsLanguageServiceCacheEntry,
): number {
  return (
    estimateStringBytes(key) +
    estimateJsProjectFilesBytes(entry.project.files) +
    estimateJsonBytes(entry.project.options, 4096) +
    estimateStringBytes(entry.project.currentDirectory) +
    estimateStringBytes(entry.project.optionsKey) +
    320 * 1024
  );
}

function estimateJsProjectFilesBytes(files: ReadonlyMap<string, JsProjectFile>): number {
  let total = 128;
  for (const [fileName, file] of files) {
    total += estimateStringBytes(fileName) + estimateJsProjectFileBytes(file);
  }
  return total;
}

function estimateAspGraphPayloadBytes(payload: AspGraphPayload): number {
  return estimateJsonBytes(payload, 64 * 1024 * 1024);
}

function estimateJsProjectFileBytes(file: JsProjectFile): number {
  return (
    estimateStringBytes(file.fileName) +
    estimateStringBytes(file.text) +
    estimateStringBytes(file.version) +
    estimateStringBytes(file.uri) +
    (file.virtual
      ? estimateStringBytes(file.virtual.text) + file.virtual.sourceMap.segments.length * 48
      : 0) +
    128
  );
}

function estimateJsScriptSnapshotEntryBytes(key: string, snapshot: AspJsScriptSnapshot): number {
  let total = estimateStringBytes(key);
  let cursor: AspJsScriptSnapshot | undefined = snapshot;
  let depth = 0;
  while (cursor && depth < 9) {
    total +=
      estimateStringBytes(cursor.fileName) +
      estimateStringBytes(cursor.version) +
      cursor.getLength() * 2 +
      cursor.segments.length * 32 +
      128;
    cursor = cursor.previous;
    depth += 1;
  }
  return total;
}
