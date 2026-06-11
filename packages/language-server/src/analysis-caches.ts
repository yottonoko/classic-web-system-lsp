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
  GraphFileIndex,
  WorkspaceVbReferenceExecutionOptions,
  WorkspaceVbReferenceSummaryIncludeGraph,
} from "./asp-graph/types";
import type { CachedTsDiagnostic } from "./document-store";

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

export const jsLanguageServiceCache = new Map<string, JsLanguageServiceCacheEntry>();
export const jsOpenProjectFilesCache = new Map<string, JsOpenProjectFilesCacheEntry>();
export const jsProjectConfigCache = new Map<string, JsProjectConfigCacheEntry>();
export const jsDocumentRegistry = ts.createDocumentRegistry(ts.sys.useCaseSensitiveFileNames);
export const jsScriptSnapshots = new Map<string, AspJsScriptSnapshot>();
export const jsFileExistsCache = new Map<string, boolean>();
export const jsReadFileCache = new Map<string, string | undefined>();
export const jsDirectoryExistsCache = new Map<string, boolean>();
export const jsDirectoriesCache = new Map<string, string[]>();
export const jsReadDirectoryCache = new Map<string, string[]>();
export const jsRealpathCache = new Map<string, string>();
export const jsFileStatCache = new Map<string, JsFileStat | undefined>();

export const graphFileIndexCache = new Map<string, GraphFileIndex>();
export const graphFileIndexInFlight = new Map<string, Promise<GraphFileIndex>>();
export const graphFileIndexCacheMaxEntries = 64;

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
export const lightweightJsUnusedDiagnosticsCache = new Map<string, LightweightJsUnusedCacheEntry>();

export type { WorkspaceVbReferenceExecutionOptions, WorkspaceVbReferenceSummaryIncludeGraph };
