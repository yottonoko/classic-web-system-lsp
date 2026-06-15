import path from "node:path";
import { LSPErrorCodes, ResponseError } from "vscode-languageserver/node";
import type { TextDocument } from "vscode-languageserver-textdocument";
import type { AspInclude, AspSettings, VbSymbolIndex } from "@asp-lsp/core";
import type { CachedDocument } from "../document-store";
import {
  buildImplicitGlobalIncludeGraphAsync,
  canonicalizeImplicitGlobalIndexedDocument,
  computeImplicitGlobalCanonicalIds,
  implicitGlobalDocumentMetadataFromIndexed,
} from "./implicit-globals";
import type {
  AnalysisCancellation,
  AspGraphDocument,
  AspGraphDocumentCollectionTruncation,
  AspGraphIndexedDocument,
  AspGraphPayload,
  AspGraphScope,
  GraphCancellationToken,
  GraphFileIndexOperationCache,
  VbProjectContextLimits,
} from "./types";

export interface AspGraphProgressTaskHandle {
  id: string;
  isCancellationRequested(): boolean;
  update(update: {
    label?: string;
    detail?: string;
    current?: number;
    total?: number;
    activeItems?: string[];
  }): void;
  step(detail?: string): void;
  end(): void;
}

export interface AspGraphWorkspaceIndexedDocument {
  uri: string;
  fileName: string;
  mtimeMs: number;
  size: number;
}

export interface AspGraphDocumentSource {
  uri: string;
  fileName: string;
  textLength: number;
  load(): Promise<AspGraphDocument>;
}

export interface AspGraphBuildHost {
  documentsAll(): TextDocument[];
  globalSettings(): AspSettings;
  workspaceIndexValues(): AspGraphWorkspaceIndexedDocument[];
  workspaceIndexTruncated(): boolean;
  defaultGraphMaxDocuments: number;
  defaultGraphMaxTextLength: number;
  defaultMaxIndexFiles: number;
  defaultVbProjectMaxDocuments: number;
  defaultVbProjectMaxTextLength: number;
  beginProgressTask(
    kind: "analyzing",
    label: string,
    options: { cancellable?: boolean; detail?: string },
  ): AspGraphProgressTaskHandle;
  progressCancellation(
    progress: AspGraphProgressTaskHandle,
    cancellation: AnalysisCancellation,
  ): AnalysisCancellation;
  tokenFromAnalysisCancellation(cancellation: AnalysisCancellation): GraphCancellationToken;
  progressFileLabelFromUri(uri: string): string;
  progressFileLabel(fileName: string): string;
  logDebugSummary(settings: AspSettings, message: string): void;
  finishDebugStep(settings: AspSettings, uri: string, step: string, startedAt: bigint): void;
  yieldToEventLoop(): Promise<void>;
  progressMapHooks<T>(
    progress: AspGraphProgressTaskHandle,
    label: (item: T) => string,
    offset?: number,
  ): unknown;
  mapWithConcurrency<T, R>(
    items: readonly T[],
    concurrency: number,
    mapper: (item: T) => Promise<R>,
    hooks?: unknown,
  ): Promise<R[]>;
  analysisConcurrency(settings: AspSettings): number;
  includeReadConcurrency(settings: AspSettings): number;
  cachedSettings(uri: string): AspSettings;
  cachedDocumentForGraphAsync(uri: string): Promise<CachedDocument | undefined>;
  ensureFreshCachedDocumentAsync(document: TextDocument): Promise<CachedDocument>;
  cachedFromIndexedAsync(
    entry: AspGraphWorkspaceIndexedDocument,
    settings: AspSettings,
  ): Promise<CachedDocument>;
  graphDocumentFromCachedAsync(
    cached: CachedDocument,
    settings: AspSettings,
  ): Promise<AspGraphDocument>;
  graphDocumentFromIncludeFileAsync(
    fileName: string,
    settings: AspSettings,
  ): Promise<AspGraphDocument | undefined>;
  graphIncludeRefsForDocumentAsync(
    document: AspGraphDocument,
    settings: AspSettings,
  ): Promise<AspInclude[]>;
  graphDocumentsNeedRelatedIncludeTreeAnalysisAsync(
    documents: AspGraphDocument[],
    settings: AspSettings,
    cancellation: AnalysisCancellation,
    operationCache?: GraphFileIndexOperationCache,
    progress?: AspGraphProgressTaskHandle,
  ): Promise<boolean>;
  collectIncomingIncludeGraphDocumentsAsync(
    targetFileNames: Set<string>,
    settings: AspSettings,
    cancellation: AnalysisCancellation,
    options?: {
      excludedFileKeys?: Set<string>;
      fileFilter?: AspGraphDocumentFileFilter;
      token?: GraphCancellationToken;
      progress?: AspGraphProgressTaskHandle;
    },
  ): Promise<AspGraphDocument[]>;
  graphPayloadFromDocumentsAsync(
    scope: AspGraphScope,
    documents: AspGraphDocument[],
    settings: AspSettings,
    options?: {
      rootUri?: string;
      truncated?: AspGraphPayload["truncated"];
      cancellation?: AnalysisCancellation;
      includeAnalysisTypeDetails?: boolean;
      fileFilter?: AspGraphDocumentFileFilter;
      outputLimits?: VbProjectContextLimits;
      progress?: AspGraphProgressTaskHandle;
      operationCache?: GraphFileIndexOperationCache;
    },
  ): Promise<AspGraphPayload>;
  graphPayloadFromDocumentSourcesAsync(
    scope: AspGraphScope,
    sources: AspGraphDocumentSource[],
    settings: AspSettings,
    options?: {
      rootUri?: string;
      truncated?: AspGraphPayload["truncated"];
      cancellation?: AnalysisCancellation;
      includeAnalysisTypeDetails?: boolean;
      fileFilter?: AspGraphDocumentFileFilter;
      outputLimits?: VbProjectContextLimits;
      progress?: AspGraphProgressTaskHandle;
      operationCache?: GraphFileIndexOperationCache;
    },
  ): Promise<AspGraphPayload>;
  ensureWorkspaceIndexAsync(settings: AspSettings, token?: GraphCancellationToken): Promise<void>;
  workspaceAnalyzableOpenDocumentsAsync(settings: AspSettings): Promise<TextDocument[]>;
  graphIncludeTreeLimits(settings: AspSettings): VbProjectContextLimits;
  graphOutputLimits(settings: AspSettings): VbProjectContextLimits;
  vbProjectContextLimits(settings: AspSettings): VbProjectContextLimits;
  resolveIncludePathDetailsAsync(
    ownerUri: string,
    includePath: string,
    mode: AspInclude["mode"],
    settings: AspSettings,
  ): Promise<{ fileName: string; exists: boolean }>;
  fileSizeAsync(fileName: string, settings: AspSettings): Promise<number | undefined>;
  statAsync(fileName: string): Promise<{ isDirectory(): boolean } | undefined>;
  graphFileNameFromUri(uri: string): string;
  graphFileKey(fileName: string): string;
  graphFileKeyFromUri(uri: string): string;
  pathToFileUri(fileName: string): string;
  normalizeFileName(fileName: string): string;
  graphFileIndexFingerprint(index: VbSymbolIndex): string;
}

const neverCancelled: AnalysisCancellation = {
  isCancellationRequested: () => false,
};

export interface AspGraphBuildService {
  buildAspGraphForCommand(
    argument: unknown,
    token?: GraphCancellationToken,
  ): Promise<AspGraphPayload>;
  buildDocumentAspGraphAsync(
    uri: string | undefined,
    cancellation?: AnalysisCancellation,
    options?: BuildDocumentAspGraphOptions,
  ): Promise<AspGraphPayload>;
  buildFolderAspGraphAsync(
    uri: string | undefined,
    token?: GraphCancellationToken,
    cancellation?: AnalysisCancellation,
    progress?: AspGraphProgressTaskHandle,
    operationCache?: GraphFileIndexOperationCache,
    outputLimits?: VbProjectContextLimits,
  ): Promise<AspGraphPayload>;
  buildWorkspaceAspGraphAsync(
    token?: GraphCancellationToken,
    cancellation?: AnalysisCancellation,
    progress?: AspGraphProgressTaskHandle,
    operationCache?: GraphFileIndexOperationCache,
    outputLimits?: VbProjectContextLimits,
  ): Promise<AspGraphPayload>;
  collectDocumentGraphDocumentsAsync(
    root: CachedDocument,
    settings: AspSettings,
    cancellation?: AnalysisCancellation,
  ): Promise<AspGraphDocument[]>;
  collectIncludeTreeGraphDocumentsAsync(
    root: AspGraphDocument,
    settings: AspSettings,
    cancellation?: AnalysisCancellation,
    options?: CollectIncludeTreeGraphDocumentsOptions,
  ): Promise<AspGraphDocument[]>;
  collectRelatedIncludeTreeGraphDocumentsAsync(
    rootDocuments: AspGraphDocument[],
    settings: AspSettings,
    cancellation: AnalysisCancellation,
    options?: CollectRelatedIncludeTreeGraphDocumentsOptions,
  ): Promise<AspGraphDocument[]>;
  collectRelatedIncludeTreeOwnerGraphDocumentsAsync(
    rootDocuments: AspGraphDocument[],
    settings: AspSettings,
    cancellation: AnalysisCancellation,
    options?: CollectRelatedIncludeTreeOwnerGraphDocumentsOptions,
  ): Promise<AspGraphDocument[]>;
  canonicalizeImplicitGlobalIndexedDocumentsAsync(
    indexedDocuments: AspGraphIndexedDocument[],
    settings: AspSettings,
    cancellation?: AnalysisCancellation,
  ): Promise<AspGraphIndexedDocument[]>;
}

export interface BuildDocumentAspGraphOptions {
  includeIncomingDocumentIncludes?: boolean;
  includeRelatedIncludeTreesForUnresolved?: boolean;
  forceRelatedIncludeTreeAnalysis?: boolean;
  includeAnalysisTypeDetails?: boolean;
  fileFilter?: AspGraphDocumentFileFilter;
  outputLimits?: VbProjectContextLimits;
  includeTreeLimits?: VbProjectContextLimits;
  progress?: AspGraphProgressTaskHandle;
  operationCache?: GraphFileIndexOperationCache;
}

export type AspGraphDocumentFileFilter = (fileName: string) => boolean;

export interface CollectIncludeTreeGraphDocumentsOptions {
  excludedFileKeys?: Set<string>;
  fileFilter?: AspGraphDocumentFileFilter;
  initialTextLength?: number;
  limits?: VbProjectContextLimits;
  truncation?: AspGraphDocumentCollectionTruncation;
  progress?: AspGraphProgressTaskHandle;
  progressLabel?: string;
}

export interface CollectRelatedIncludeTreeGraphDocumentsOptions extends CollectIncludeTreeGraphDocumentsOptions {
  token?: GraphCancellationToken;
}

export interface CollectRelatedIncludeTreeOwnerGraphDocumentsOptions {
  excludedFileKeys?: Set<string>;
  fileFilter?: AspGraphDocumentFileFilter;
  token?: GraphCancellationToken;
}

export function createAspGraphBuildService(host: AspGraphBuildHost): AspGraphBuildService {
  async function buildAspGraphForCommand(
    argument: unknown,
    token?: GraphCancellationToken,
  ): Promise<AspGraphPayload> {
    const scope = graphCommandScope(argument);
    const uri = graphCommandUri(argument);
    const task = host.beginProgressTask("analyzing", `graph.${scope}`, {
      cancellable: true,
      detail: uri ? host.progressFileLabelFromUri(uri) : undefined,
    });
    const cancellation = host.progressCancellation(task, analysisCancellationFromToken(token));
    const operationCache: GraphFileIndexOperationCache = new Map();
    try {
      throwIfGraphCancelled(cancellation);
      if (scope === "workspace") {
        return buildWorkspaceAspGraphAsync(
          host.tokenFromAnalysisCancellation(cancellation),
          cancellation,
          task,
          operationCache,
          graphCommandOutputLimits(argument, host),
        );
      }
      if (scope === "folder") {
        return buildFolderAspGraphAsync(
          uri,
          host.tokenFromAnalysisCancellation(cancellation),
          cancellation,
          task,
          operationCache,
          graphCommandOutputLimits(argument, host),
        );
      }
      return buildDocumentAspGraphAsync(uri ?? host.documentsAll()[0]?.uri, cancellation, {
        includeIncomingDocumentIncludes: graphCommandIncludeIncomingDocumentIncludes(argument),
        includeRelatedIncludeTreesForUnresolved:
          graphCommandIncludeRelatedIncludeTreesForUnresolved(argument),
        forceRelatedIncludeTreeAnalysis: graphCommandForceRelatedIncludeTreeAnalysis(argument),
        includeAnalysisTypeDetails: graphCommandIncludeAnalysisTypeDetails(argument),
        fileFilter: graphCommandFileFilter(argument),
        outputLimits: graphCommandOutputLimits(argument, host),
        includeTreeLimits: graphCommandIncludeTreeLimits(argument, host),
        progress: task,
        operationCache,
      });
    } finally {
      task.end();
    }
  }

  async function buildDocumentAspGraphAsync(
    uri: string | undefined,
    cancellation: AnalysisCancellation = neverCancelled,
    options: BuildDocumentAspGraphOptions = {},
  ): Promise<AspGraphPayload> {
    throwIfGraphCancelled(cancellation);
    options.progress?.update({
      label: "graph.loadDocuments",
      current: 0,
      total: 4,
      detail: uri ? host.progressFileLabelFromUri(uri) : undefined,
    });
    await host.yieldToEventLoop();
    const cached = uri ? await host.cachedDocumentForGraphAsync(uri) : undefined;
    throwIfGraphCancelled(cancellation);
    if (!cached) {
      return emptyAspGraphPayload("document", uri, host);
    }
    const settings = host.cachedSettings(cached.source.uri);
    options.progress?.update({
      label: "graph.loadDocuments",
      current: 1,
      total: 4,
      detail: host.progressFileLabelFromUri(cached.source.uri),
    });
    await host.yieldToEventLoop();
    const targetGraphDocument = await host.graphDocumentFromCachedAsync(cached, settings);
    const graphDocumentTruncation: AspGraphDocumentCollectionTruncation = {};
    const includeTreeLimits = options.includeTreeLimits ?? host.graphIncludeTreeLimits(settings);
    options.progress?.update({
      label: "graph.collectIncludes",
      current: 2,
      total: 4,
      detail: host.progressFileLabel(targetGraphDocument.fileName),
    });
    await host.yieldToEventLoop();
    const documentsForGraph = await collectIncludeTreeGraphDocumentsAsync(
      targetGraphDocument,
      settings,
      cancellation,
      {
        limits: includeTreeLimits,
        fileFilter: options.fileFilter,
        progress: options.progress,
        progressLabel: "graph.collectIncludes",
        truncation: graphDocumentTruncation,
      },
    );
    const includeRelatedIncludeTreesForUnresolved =
      options.includeRelatedIncludeTreesForUnresolved ??
      settings.graph?.includeRelatedIncludeTreesForUnresolved === true;
    let usedWorkspaceIndexForDocumentGraph = false;
    if (
      includeRelatedIncludeTreesForUnresolved &&
      (options.forceRelatedIncludeTreeAnalysis === true ||
        (await host.graphDocumentsNeedRelatedIncludeTreeAnalysisAsync(
          documentsForGraph,
          settings,
          cancellation,
          options.operationCache,
          options.progress,
        )))
    ) {
      usedWorkspaceIndexForDocumentGraph = true;
      options.progress?.update({
        label: "graph.collectRelatedIncludes",
        current: 3,
        total: 4,
        detail: host.progressFileLabel(targetGraphDocument.fileName),
      });
      await host.yieldToEventLoop();
      appendAspGraphDocuments(
        documentsForGraph,
        await collectRelatedIncludeTreeGraphDocumentsAsync(
          [targetGraphDocument],
          settings,
          cancellation,
          {
            excludedFileKeys: new Set(
              documentsForGraph.map((document) => host.graphFileKey(document.fileName)),
            ),
            fileFilter: options.fileFilter,
            initialTextLength: graphDocumentsTextLength(documentsForGraph),
            limits: includeTreeLimits,
            progress: options.progress,
            progressLabel: "graph.collectRelatedIncludes",
            truncation: graphDocumentTruncation,
          },
        ),
      );
    }
    const includeIncomingDocumentIncludes =
      settings.graph?.showIncomingDocumentIncludes === true ||
      options.includeIncomingDocumentIncludes === true;
    if (includeIncomingDocumentIncludes) {
      usedWorkspaceIndexForDocumentGraph = true;
      options.progress?.update({
        label: "graph.collectIncomingIncludes",
        current: 3,
        total: 4,
        detail: host.progressFileLabel(targetGraphDocument.fileName),
      });
      await host.yieldToEventLoop();
      appendAspGraphDocuments(
        documentsForGraph,
        await host.collectIncomingIncludeGraphDocumentsAsync(
          new Set([host.graphFileNameFromUri(cached.source.uri)]),
          settings,
          cancellation,
          {
            excludedFileKeys: new Set(
              documentsForGraph.map((document) => host.graphFileKey(document.fileName)),
            ),
            fileFilter: options.fileFilter,
            progress: options.progress,
          },
        ),
      );
    }
    const truncated =
      graphDocumentTruncation.reason !== undefined
        ? { reason: graphDocumentTruncation.reason }
        : usedWorkspaceIndexForDocumentGraph && host.workspaceIndexTruncated()
          ? {
              reason: `workspaceIndex>${settings.workspace?.maxIndexFiles ?? host.defaultMaxIndexFiles}`,
            }
          : undefined;
    return host.graphPayloadFromDocumentsAsync("document", documentsForGraph, settings, {
      rootUri: cached.source.uri,
      truncated,
      cancellation,
      includeAnalysisTypeDetails: options.includeAnalysisTypeDetails,
      fileFilter: options.fileFilter,
      outputLimits: options.outputLimits,
      progress: options.progress,
      operationCache: options.operationCache,
    });
  }

  async function buildFolderAspGraphAsync(
    uri: string | undefined,
    token?: GraphCancellationToken,
    cancellation: AnalysisCancellation = neverCancelled,
    progress?: AspGraphProgressTaskHandle,
    operationCache?: GraphFileIndexOperationCache,
    outputLimits?: VbProjectContextLimits,
  ): Promise<AspGraphPayload> {
    throwIfGraphCancelled(cancellation);
    const folderName = await graphCommandFolderNameAsync(uri);
    throwIfGraphCancelled(cancellation);
    if (!folderName) {
      return emptyAspGraphPayload("folder", uri, host);
    }
    const settings = host.globalSettings();
    progress?.update({
      label: "graph.workspaceIndex",
      detail: folderName ? host.progressFileLabel(folderName) : undefined,
    });
    await host.yieldToEventLoop();
    await host.ensureWorkspaceIndexAsync(settings, token);
    throwIfGraphCancelled(cancellation);
    const opened = new Set<string>();
    const documentsForGraph: AspGraphDocument[] = [];
    const openDocuments = await host.workspaceAnalyzableOpenDocumentsAsync(settings);
    throwIfGraphCancelled(cancellation);
    const concurrency = host.analysisConcurrency(settings);
    progress?.update({
      label: "graph.openDocuments",
      current: 0,
      total: openDocuments.length,
      detail: host.progressFileLabel(folderName),
    });
    await host.yieldToEventLoop();
    const openGraphDocuments = await host.mapWithConcurrency(
      openDocuments,
      concurrency,
      async (document): Promise<AspGraphDocument | undefined> => {
        throwIfGraphCancelled(cancellation);
        const fileName = host.graphFileNameFromUri(document.uri);
        if (!isFileInDirectory(fileName, folderName)) {
          return undefined;
        }
        const cached = await host.ensureFreshCachedDocumentAsync(document);
        throwIfGraphCancelled(cancellation);
        return host.graphDocumentFromCachedAsync(cached, host.cachedSettings(cached.source.uri));
      },
      progress
        ? host.progressMapHooks(progress, (document: TextDocument) =>
            host.progressFileLabelFromUri(document.uri),
          )
        : undefined,
    );
    for (const graphDocument of openGraphDocuments) {
      if (!graphDocument) {
        continue;
      }
      opened.add(host.graphFileKey(graphDocument.fileName));
      documentsForGraph.push(graphDocument);
    }
    const graphLimits = outputLimits ?? host.graphOutputLimits(settings);
    const indexedLimit = limitWorkspaceGraphIndexEntries(
      host
        .workspaceIndexValues()
        .filter(
          (entry) =>
            !opened.has(host.graphFileKey(entry.fileName)) &&
            isFileInDirectory(entry.fileName, folderName),
        ),
      graphLimits,
      documentsForGraph.length,
      graphDocumentsTextLength(documentsForGraph),
    );
    const indexedEntries = indexedLimit.entries;
    progress?.update({
      label: "graph.loadDocuments",
      current: openDocuments.length,
      total: openDocuments.length + indexedEntries.length,
    });
    await host.yieldToEventLoop();
    const graphDocumentSources = [
      ...documentsForGraph.map(graphDocumentSourceFromDocument),
      ...indexedEntries.map((entry) =>
        workspaceGraphDocumentSourceFromIndexedEntry(host, entry, settings, cancellation),
      ),
    ];
    if (settings.graph?.showIncomingFolderIncludes === true) {
      appendAspGraphDocuments(
        documentsForGraph,
        await loadAspGraphDocumentSourcesAsync(
          graphDocumentSources.slice(documentsForGraph.length),
          host,
          progress,
          openDocuments.length,
        ),
      );
      const folderTargetFileNames = new Set(
        documentsForGraph
          .map((document) => document.fileName)
          .filter((fileName) => isFileInDirectory(fileName, folderName)),
      );
      appendAspGraphDocuments(
        documentsForGraph,
        await host.collectIncomingIncludeGraphDocumentsAsync(
          folderTargetFileNames,
          settings,
          cancellation,
          {
            excludedFileKeys: new Set(
              documentsForGraph.map((document) => host.graphFileKey(document.fileName)),
            ),
            progress,
            token,
          },
        ),
      );
      return host.graphPayloadFromDocumentsAsync("folder", documentsForGraph, settings, {
        rootUri: host.pathToFileUri(folderName),
        truncated: folderGraphTruncationReason(indexedLimit.reason, settings, host),
        cancellation,
        progress,
        operationCache,
        outputLimits,
      });
    }
    return host.graphPayloadFromDocumentSourcesAsync("folder", graphDocumentSources, settings, {
      rootUri: host.pathToFileUri(folderName),
      truncated: folderGraphTruncationReason(indexedLimit.reason, settings, host),
      cancellation,
      progress,
      operationCache,
      outputLimits,
    });
  }

  async function buildWorkspaceAspGraphAsync(
    token?: GraphCancellationToken,
    cancellation: AnalysisCancellation = neverCancelled,
    progress?: AspGraphProgressTaskHandle,
    operationCache?: GraphFileIndexOperationCache,
    outputLimits?: VbProjectContextLimits,
  ): Promise<AspGraphPayload> {
    throwIfGraphCancelled(cancellation);
    const settings = host.globalSettings();
    progress?.update({ label: "graph.workspaceIndex" });
    await host.yieldToEventLoop();
    await host.ensureWorkspaceIndexAsync(settings, token);
    throwIfGraphCancelled(cancellation);
    const opened = new Set<string>();
    const documentsForGraph: AspGraphDocument[] = [];
    const openDocuments = await host.workspaceAnalyzableOpenDocumentsAsync(settings);
    throwIfGraphCancelled(cancellation);
    const concurrency = host.analysisConcurrency(settings);
    progress?.update({ label: "graph.openDocuments", current: 0, total: openDocuments.length });
    await host.yieldToEventLoop();
    const openGraphDocuments = await host.mapWithConcurrency(
      openDocuments,
      concurrency,
      async (document) => {
        throwIfGraphCancelled(cancellation);
        const cached = await host.ensureFreshCachedDocumentAsync(document);
        throwIfGraphCancelled(cancellation);
        return host.graphDocumentFromCachedAsync(cached, host.cachedSettings(cached.source.uri));
      },
      progress
        ? host.progressMapHooks(progress, (document: TextDocument) =>
            host.progressFileLabelFromUri(document.uri),
          )
        : undefined,
    );
    for (const graphDocument of openGraphDocuments) {
      opened.add(host.graphFileKey(graphDocument.fileName));
      documentsForGraph.push(graphDocument);
    }
    const graphLimits = outputLimits ?? host.graphOutputLimits(settings);
    const indexedLimit = limitWorkspaceGraphIndexEntries(
      host.workspaceIndexValues().filter((entry) => !opened.has(host.graphFileKey(entry.fileName))),
      graphLimits,
      documentsForGraph.length,
      graphDocumentsTextLength(documentsForGraph),
    );
    const indexedEntries = indexedLimit.entries;
    progress?.update({
      label: "graph.loadDocuments",
      current: openDocuments.length,
      total: openDocuments.length + indexedEntries.length,
    });
    await host.yieldToEventLoop();
    const graphDocumentSources = [
      ...documentsForGraph.map(graphDocumentSourceFromDocument),
      ...indexedEntries.map((entry) =>
        workspaceGraphDocumentSourceFromIndexedEntry(host, entry, settings, cancellation),
      ),
    ];
    return host.graphPayloadFromDocumentSourcesAsync("workspace", graphDocumentSources, settings, {
      truncated: workspaceGraphTruncationReason(indexedLimit.reason, settings, host),
      cancellation,
      progress,
      operationCache,
      outputLimits,
    });
  }

  async function graphCommandFolderNameAsync(uri: string | undefined): Promise<string | undefined> {
    if (!uri?.startsWith("file://")) {
      return undefined;
    }
    const folderName = host.graphFileNameFromUri(uri);
    const stat = await host.statAsync(folderName);
    return stat?.isDirectory() ? folderName : undefined;
  }

  function isFileInDirectory(fileName: string, directory: string): boolean {
    const relative = path.relative(directory, host.normalizeFileName(fileName));
    return relative.length > 0 && !relative.startsWith("..") && !path.isAbsolute(relative);
  }

  async function collectDocumentGraphDocumentsAsync(
    root: CachedDocument,
    settings: AspSettings,
    cancellation: AnalysisCancellation = neverCancelled,
  ): Promise<AspGraphDocument[]> {
    return collectIncludeTreeGraphDocumentsAsync(
      await host.graphDocumentFromCachedAsync(root, settings),
      settings,
      cancellation,
    );
  }

  async function collectIncludeTreeGraphDocumentsAsync(
    root: AspGraphDocument,
    settings: AspSettings,
    cancellation: AnalysisCancellation = neverCancelled,
    options: CollectIncludeTreeGraphDocumentsOptions = {},
  ): Promise<AspGraphDocument[]> {
    const limits = options.limits ?? host.vbProjectContextLimits(settings);
    const documentsForGraph: AspGraphDocument[] = [];
    const visited = new Set<string>();
    const excludedFileKeys = options.excludedFileKeys ?? new Set<string>();
    let textLength = options.initialTextLength ?? 0;

    const visit = async (document: AspGraphDocument, depth: number): Promise<void> => {
      throwIfGraphCancelled(cancellation);
      const documentKey = host.graphFileKey(document.fileName);
      if (depth > 20) {
        noteAspGraphDocumentCollectionTruncated(options.truncation, "depth>20");
        return;
      }
      if (visited.has(documentKey) || excludedFileKeys.has(documentKey)) {
        return;
      }
      if (depth > 0 && options.fileFilter && !options.fileFilter(document.fileName)) {
        return;
      }
      visited.add(documentKey);
      documentsForGraph.push(document);
      textLength += document.text.length;
      options.progress?.update({
        label: options.progressLabel ?? "graph.collectIncludes",
        current: Math.min(visited.size, limits.maxDocuments),
        total: limits.maxDocuments,
        detail: host.progressFileLabel(document.fileName),
        activeItems: [host.progressFileLabel(document.fileName)],
      });
      await host.yieldToEventLoop();
      const includeRefs = await host.graphIncludeRefsForDocumentAsync(document, settings);
      throwIfGraphCancelled(cancellation);
      options.progress?.update({
        label: "graph.prefetchIncludes",
        current: 0,
        total: Math.max(1, includeRefs.length),
        detail: host.progressFileLabel(document.fileName),
        activeItems: [host.progressFileLabel(document.fileName)],
      });
      await host.yieldToEventLoop();
      await prefetchGraphIncludeTargetsAsync(
        document.uri,
        includeRefs,
        settings,
        cancellation,
        options.fileFilter,
        options.progress,
      );
      for (const include of includeRefs) {
        throwIfGraphCancelled(cancellation);
        options.progress?.update({
          label: "graph.resolveIncludes",
          current: Math.min(visited.size, limits.maxDocuments),
          total: limits.maxDocuments,
          detail: `${host.progressFileLabel(document.fileName)} -> ${include.path}`,
          activeItems: [host.progressFileLabel(document.fileName)],
        });
        await host.yieldToEventLoop();
        const resolved = await host.resolveIncludePathDetailsAsync(
          document.uri,
          include.path,
          include.mode,
          settings,
        );
        throwIfGraphCancelled(cancellation);
        const includeKey = host.graphFileKey(resolved.fileName);
        if (
          !resolved.exists ||
          visited.has(includeKey) ||
          excludedFileKeys.has(includeKey) ||
          (options.fileFilter && !options.fileFilter(resolved.fileName))
        ) {
          continue;
        }
        if (visited.size + excludedFileKeys.size >= limits.maxDocuments) {
          noteAspGraphDocumentCollectionTruncated(
            options.truncation,
            `documents>${limits.maxDocuments}`,
          );
          continue;
        }
        const size = await host.fileSizeAsync(resolved.fileName, settings);
        throwIfGraphCancelled(cancellation);
        if (size !== undefined && textLength + size > limits.maxTextLength) {
          noteAspGraphDocumentCollectionTruncated(
            options.truncation,
            `text>${limits.maxTextLength}`,
          );
          continue;
        }
        const entry = await host.graphDocumentFromIncludeFileAsync(resolved.fileName, settings);
        throwIfGraphCancelled(cancellation);
        if (!entry) {
          continue;
        }
        await visit(entry, depth + 1);
      }
    };

    await visit(root, 0);
    return documentsForGraph;
  }

  async function prefetchGraphIncludeTargetsAsync(
    ownerUri: string,
    includeRefs: AspInclude[],
    settings: AspSettings,
    cancellation: AnalysisCancellation,
    fileFilter?: AspGraphDocumentFileFilter,
    progress?: AspGraphProgressTaskHandle,
  ): Promise<void> {
    await host.mapWithConcurrency(
      includeRefs,
      host.includeReadConcurrency(settings),
      async (include) => {
        if (cancellation.isCancellationRequested()) {
          return;
        }
        const resolved = await host.resolveIncludePathDetailsAsync(
          ownerUri,
          include.path,
          include.mode,
          settings,
        );
        if (!resolved.exists || cancellation.isCancellationRequested()) {
          return;
        }
        if (fileFilter && !fileFilter(resolved.fileName)) {
          return;
        }
        await Promise.all([
          host.fileSizeAsync(resolved.fileName, settings),
          host.graphDocumentFromIncludeFileAsync(resolved.fileName, settings),
        ]).catch(() => undefined);
      },
      progress ? host.progressMapHooks(progress, (include: AspInclude) => include.path) : undefined,
    );
  }

  async function collectRelatedIncludeTreeGraphDocumentsAsync(
    rootDocuments: AspGraphDocument[],
    settings: AspSettings,
    cancellation: AnalysisCancellation,
    options: CollectRelatedIncludeTreeGraphDocumentsOptions = {},
  ): Promise<AspGraphDocument[]> {
    const limits = options.limits ?? host.vbProjectContextLimits(settings);
    const excludedFileKeys = new Set(options.excludedFileKeys ?? []);
    const documentsForGraph: AspGraphDocument[] = [];
    let textLength = options.initialTextLength ?? graphDocumentsTextLength(rootDocuments);
    let frontier = new Set(rootDocuments.map((document) => document.fileName));

    for (let depth = 0; depth < 20 && frontier.size > 0; depth += 1) {
      options.progress?.update({
        label: "graph.collectRelatedIncludes",
        current: Math.min(excludedFileKeys.size, limits.maxDocuments),
        total: limits.maxDocuments,
        detail: `depth ${depth + 1}, frontier ${frontier.size}`,
        activeItems: [...frontier].slice(0, 5).map(host.progressFileLabel),
      });
      await host.yieldToEventLoop();
      if (excludedFileKeys.size >= limits.maxDocuments) {
        noteAspGraphDocumentCollectionTruncated(
          options.truncation,
          `documents>${limits.maxDocuments}`,
        );
        break;
      }
      throwIfGraphCancelled(cancellation);
      const incoming = await host.collectIncomingIncludeGraphDocumentsAsync(
        frontier,
        settings,
        cancellation,
        {
          excludedFileKeys,
          fileFilter: options.fileFilter,
          progress: options.progress,
          token: options.token,
        },
      );
      if (incoming.length === 0) {
        frontier = new Set();
        break;
      }
      const nextFrontier = new Set<string>();
      for (const owner of incoming) {
        throwIfGraphCancelled(cancellation);
        if (excludedFileKeys.size >= limits.maxDocuments) {
          noteAspGraphDocumentCollectionTruncated(
            options.truncation,
            `documents>${limits.maxDocuments}`,
          );
          break;
        }
        const ownerKey = host.graphFileKey(owner.fileName);
        if (excludedFileKeys.has(ownerKey)) {
          continue;
        }
        const ownerTree = await collectIncludeTreeGraphDocumentsAsync(
          owner,
          settings,
          cancellation,
          {
            excludedFileKeys,
            fileFilter: options.fileFilter,
            initialTextLength: textLength,
            limits,
            progress: options.progress,
            progressLabel: "graph.collectRelatedIncludes",
            truncation: options.truncation,
          },
        );
        const treeHasOwner = ownerTree.some(
          (document) => host.graphFileKey(document.fileName) === ownerKey,
        );
        for (const document of ownerTree) {
          const documentKey = host.graphFileKey(document.fileName);
          if (excludedFileKeys.has(documentKey)) {
            continue;
          }
          excludedFileKeys.add(documentKey);
          documentsForGraph.push(document);
          textLength += document.text.length;
          if (excludedFileKeys.size >= limits.maxDocuments) {
            noteAspGraphDocumentCollectionTruncated(
              options.truncation,
              `documents>${limits.maxDocuments}`,
            );
            break;
          }
        }
        if (treeHasOwner) {
          nextFrontier.add(owner.fileName);
        }
      }
      frontier = nextFrontier;
    }
    if (frontier.size > 0) {
      noteAspGraphDocumentCollectionTruncated(options.truncation, "depth>20");
    }
    return documentsForGraph;
  }

  async function collectRelatedIncludeTreeOwnerGraphDocumentsAsync(
    rootDocuments: AspGraphDocument[],
    settings: AspSettings,
    cancellation: AnalysisCancellation,
    options: CollectRelatedIncludeTreeOwnerGraphDocumentsOptions = {},
  ): Promise<AspGraphDocument[]> {
    const limits = host.vbProjectContextLimits(settings);
    const excludedFileKeys = new Set(options.excludedFileKeys ?? []);
    const ownerDocuments: AspGraphDocument[] = [];
    let frontier = new Set(rootDocuments.map((document) => document.fileName));

    for (let depth = 0; depth < 20 && frontier.size > 0; depth += 1) {
      if (excludedFileKeys.size >= limits.maxDocuments) {
        break;
      }
      throwIfGraphCancelled(cancellation);
      const incoming = await host.collectIncomingIncludeGraphDocumentsAsync(
        frontier,
        settings,
        cancellation,
        {
          excludedFileKeys,
          fileFilter: options.fileFilter,
          token: options.token,
        },
      );
      if (incoming.length === 0) {
        break;
      }
      const nextFrontier = new Set<string>();
      for (const owner of incoming) {
        throwIfGraphCancelled(cancellation);
        const ownerKey = host.graphFileKey(owner.fileName);
        if (excludedFileKeys.has(ownerKey)) {
          continue;
        }
        excludedFileKeys.add(ownerKey);
        ownerDocuments.push(owner);
        nextFrontier.add(owner.fileName);
        if (excludedFileKeys.size >= limits.maxDocuments) {
          break;
        }
      }
      frontier = nextFrontier;
    }
    return ownerDocuments;
  }

  async function canonicalizeImplicitGlobalIndexedDocumentsAsync(
    indexedDocuments: AspGraphIndexedDocument[],
    settings: AspSettings,
    cancellation: AnalysisCancellation = neverCancelled,
  ): Promise<AspGraphIndexedDocument[]> {
    if (indexedDocuments.length < 2) {
      return indexedDocuments;
    }
    const startedAt = process.hrtime.bigint();
    try {
      const metadata = indexedDocuments.map((indexed) =>
        implicitGlobalDocumentMetadataFromIndexed(
          indexed,
          host.graphFileKey(indexed.document.fileName),
        ),
      );
      const includeGraph = await buildImplicitGlobalIncludeGraphAsync(
        metadata,
        settings,
        {
          graphFileKey: host.graphFileKey,
          normalizeFileName: host.normalizeFileName,
          resolveIncludePathDetailsAsync: host.resolveIncludePathDetailsAsync,
        },
        cancellation,
      );
      const result = computeImplicitGlobalCanonicalIds(metadata, includeGraph, cancellation);
      if (result.groups > 0) {
        host.logDebugSummary(
          settings,
          `[asp-lsp] asp.graph.implicitGlobals.groups: groups=${result.groups}, maxGroupSize=${result.maxGroupSize}`,
        );
      }
      if (![...result.canonicalIdById].some(([id, canonicalId]) => id !== canonicalId)) {
        return indexedDocuments;
      }
      return indexedDocuments.map((indexed) =>
        canonicalizeImplicitGlobalIndexedDocument(
          indexed,
          result.canonicalIdById,
          host.graphFileIndexFingerprint,
        ),
      );
    } finally {
      host.finishDebugStep(
        settings,
        "workspace",
        "asp.graph.implicitGlobals.canonicalize",
        startedAt,
      );
    }
  }

  return {
    buildAspGraphForCommand,
    buildDocumentAspGraphAsync,
    buildFolderAspGraphAsync,
    buildWorkspaceAspGraphAsync,
    collectDocumentGraphDocumentsAsync,
    collectIncludeTreeGraphDocumentsAsync,
    collectRelatedIncludeTreeGraphDocumentsAsync,
    collectRelatedIncludeTreeOwnerGraphDocumentsAsync,
    canonicalizeImplicitGlobalIndexedDocumentsAsync,
  };
}

export function graphCommandScope(argument: unknown): AspGraphScope {
  if (argument && typeof argument === "object" && "scope" in argument) {
    const scope = (argument as { scope?: unknown }).scope;
    if (scope === "folder" || scope === "workspace") {
      return scope;
    }
  }
  return "document";
}

export function graphCommandUri(argument: unknown): string | undefined {
  if (!argument || typeof argument !== "object" || !("uri" in argument)) {
    return undefined;
  }
  const uri = (argument as { uri?: unknown }).uri;
  return typeof uri === "string" ? uri : undefined;
}

export function analysisCancellationFromToken(
  token: GraphCancellationToken | undefined,
): AnalysisCancellation {
  return {
    isCancellationRequested: () => token?.isCancellationRequested === true,
  };
}

export function throwIfGraphCancelled(cancellation: AnalysisCancellation): void {
  if (cancellation.isCancellationRequested()) {
    throw new ResponseError(LSPErrorCodes.RequestCancelled, "Graph generation was cancelled.");
  }
}

function emptyAspGraphPayload(
  scope: AspGraphScope,
  rootUri: string | undefined,
  host: AspGraphBuildHost,
): AspGraphPayload {
  const normalizedRootUri = rootUri?.startsWith("file://")
    ? host.pathToFileUri(host.graphFileNameFromUri(rootUri))
    : rootUri;
  return {
    scope,
    rootUri: normalizedRootUri,
    nodes: [],
    links: [],
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

function graphCommandIncludeIncomingDocumentIncludes(argument: unknown): boolean {
  if (
    !argument ||
    typeof argument !== "object" ||
    !("includeIncomingDocumentIncludes" in argument)
  ) {
    return false;
  }
  return (
    (argument as { includeIncomingDocumentIncludes?: unknown }).includeIncomingDocumentIncludes ===
    true
  );
}

function graphCommandIncludeRelatedIncludeTreesForUnresolved(
  argument: unknown,
): boolean | undefined {
  if (
    !argument ||
    typeof argument !== "object" ||
    !("includeRelatedIncludeTreesForUnresolved" in argument)
  ) {
    return undefined;
  }
  return (
    (argument as { includeRelatedIncludeTreesForUnresolved?: unknown })
      .includeRelatedIncludeTreesForUnresolved === true
  );
}

function graphCommandForceRelatedIncludeTreeAnalysis(argument: unknown): boolean {
  if (
    !argument ||
    typeof argument !== "object" ||
    !("forceRelatedIncludeTreeAnalysis" in argument)
  ) {
    return false;
  }
  return (
    (argument as { forceRelatedIncludeTreeAnalysis?: unknown }).forceRelatedIncludeTreeAnalysis ===
    true
  );
}

function graphCommandIncludeAnalysisTypeDetails(argument: unknown): boolean {
  if (!argument || typeof argument !== "object" || !("includeAnalysisTypeDetails" in argument)) {
    return false;
  }
  return (argument as { includeAnalysisTypeDetails?: unknown }).includeAnalysisTypeDetails === true;
}

function graphCommandFileFilter(argument: unknown): AspGraphDocumentFileFilter | undefined {
  if (!argument || typeof argument !== "object" || !("fileFilter" in argument)) {
    return undefined;
  }
  const fileFilter = (argument as { fileFilter?: unknown }).fileFilter;
  return typeof fileFilter === "function" ? (fileFilter as AspGraphDocumentFileFilter) : undefined;
}

function graphCommandIncludeTreeLimits(
  argument: unknown,
  host: AspGraphBuildHost,
): VbProjectContextLimits | undefined {
  if (!argument || typeof argument !== "object") {
    return undefined;
  }
  const record = argument as {
    includeTreeMaxDocuments?: unknown;
    includeTreeMaxTextLength?: unknown;
  };
  const maxDocuments = positiveIntegerCommandArgument(record.includeTreeMaxDocuments);
  const maxTextLength = positiveIntegerCommandArgument(record.includeTreeMaxTextLength);
  if (maxDocuments === undefined && maxTextLength === undefined) {
    return undefined;
  }
  return {
    maxDocuments: maxDocuments ?? host.defaultVbProjectMaxDocuments,
    maxTextLength: maxTextLength ?? host.defaultVbProjectMaxTextLength,
  };
}

function graphCommandOutputLimits(
  argument: unknown,
  host: AspGraphBuildHost,
): VbProjectContextLimits | undefined {
  if (!argument || typeof argument !== "object") {
    return undefined;
  }
  const record = argument as {
    maxDocuments?: unknown;
    maxTextLength?: unknown;
  };
  const maxDocuments = positiveIntegerCommandArgument(record.maxDocuments);
  const maxTextLength = positiveIntegerCommandArgument(record.maxTextLength);
  if (maxDocuments === undefined && maxTextLength === undefined) {
    return undefined;
  }
  return {
    maxDocuments: maxDocuments ?? host.defaultGraphMaxDocuments,
    maxTextLength: maxTextLength ?? host.defaultGraphMaxTextLength,
  };
}

function positiveIntegerCommandArgument(value: unknown): number | undefined {
  return typeof value === "number" && value > 0 ? Math.floor(value) : undefined;
}

function noteAspGraphDocumentCollectionTruncated(
  truncation: AspGraphDocumentCollectionTruncation | undefined,
  reason: string,
): void {
  if (truncation && !truncation.reason) {
    truncation.reason = reason;
  }
}

function limitWorkspaceGraphIndexEntries(
  entries: AspGraphWorkspaceIndexedDocument[],
  limits: VbProjectContextLimits,
  initialDocuments: number,
  initialTextLength: number,
): { entries: AspGraphWorkspaceIndexedDocument[]; reason?: string } {
  const limited: AspGraphWorkspaceIndexedDocument[] = [];
  let textLength = initialTextLength;
  let reason: string | undefined;
  for (const entry of entries) {
    const nextDocumentCount = initialDocuments + limited.length + 1;
    const nextTextLength = textLength + entry.size;
    const exceedsDocumentLimit = nextDocumentCount > limits.maxDocuments;
    const exceedsTextLimit = nextTextLength > limits.maxTextLength;
    if (exceedsDocumentLimit || (exceedsTextLimit && initialDocuments + limited.length > 0)) {
      reason = exceedsDocumentLimit
        ? `documents>${limits.maxDocuments}`
        : `text>${limits.maxTextLength}`;
      break;
    }
    limited.push(entry);
    textLength = nextTextLength;
    if (exceedsTextLimit) {
      reason = `text>${limits.maxTextLength}`;
      break;
    }
  }
  return { entries: limited, reason };
}

function workspaceGraphTruncationReason(
  reason: string | undefined,
  settings: AspSettings,
  host: AspGraphBuildHost,
): AspGraphPayload["truncated"] | undefined {
  const finalReason =
    reason ??
    (host.workspaceIndexTruncated()
      ? `workspaceIndex>${settings.workspace?.maxIndexFiles ?? host.defaultMaxIndexFiles}`
      : undefined);
  return finalReason ? { reason: finalReason } : undefined;
}

function folderGraphTruncationReason(
  reason: string | undefined,
  settings: AspSettings,
  host: AspGraphBuildHost,
): AspGraphPayload["truncated"] | undefined {
  return workspaceGraphTruncationReason(reason, settings, host);
}

function graphDocumentsTextLength(documentsForGraph: AspGraphDocument[]): number {
  return documentsForGraph.reduce((total, document) => total + document.text.length, 0);
}

function graphDocumentSourceFromDocument(document: AspGraphDocument): AspGraphDocumentSource {
  return {
    uri: document.uri,
    fileName: document.fileName,
    textLength: document.text.length,
    load: async () => document,
  };
}

function workspaceGraphDocumentSourceFromIndexedEntry(
  host: AspGraphBuildHost,
  entry: AspGraphWorkspaceIndexedDocument,
  settings: AspSettings,
  cancellation: AnalysisCancellation,
): AspGraphDocumentSource {
  return {
    uri: entry.uri,
    fileName: entry.fileName,
    textLength: entry.size,
    load: async () => {
      throwIfGraphCancelled(cancellation);
      const cached = await host.cachedFromIndexedAsync(entry, host.cachedSettings(entry.uri));
      throwIfGraphCancelled(cancellation);
      return host.graphDocumentFromCachedAsync(cached, host.cachedSettings(entry.uri));
    },
  };
}

async function loadAspGraphDocumentSourcesAsync(
  sources: readonly AspGraphDocumentSource[],
  host: AspGraphBuildHost,
  progress: AspGraphProgressTaskHandle | undefined,
  offset: number,
): Promise<AspGraphDocument[]> {
  return host.mapWithConcurrency(
    sources,
    Math.max(1, sources.length),
    (source) => source.load(),
    progress
      ? host.progressMapHooks(
          progress,
          (source: AspGraphDocumentSource) => host.progressFileLabel(source.fileName),
          offset,
        )
      : undefined,
  );
}

function appendAspGraphDocuments(
  target: AspGraphDocument[],
  documentsToAppend: readonly AspGraphDocument[],
): void {
  for (const document of documentsToAppend) {
    target.push(document);
  }
}
