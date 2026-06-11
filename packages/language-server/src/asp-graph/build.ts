import path from "node:path";
import {
  LSPErrorCodes,
  ResponseError,
  type Position,
  type Range,
} from "vscode-languageserver/node";
import type { TextDocument } from "vscode-languageserver-textdocument";
import type { AspInclude, AspSettings, VbSymbolIndex } from "@asp-lsp/core";
import type { CachedDocument } from "../document-store";
import type {
  AnalysisCancellation,
  AspGraphDocument,
  AspGraphDocumentCollectionTruncation,
  AspGraphIndexedDocument,
  AspGraphPayload,
  AspGraphScope,
  GraphCancellationToken,
  GraphFileIndexOperationCache,
  PrecomputedIncludeReachability,
  VbProjectContextLimits,
} from "./types";

export interface AspGraphProgressTaskHandle {
  id: string;
  isCancellationRequested(): boolean;
  update(update: {
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
  ): Promise<boolean>;
  collectIncomingIncludeGraphDocumentsAsync(
    targetFileNames: Set<string>,
    settings: AspSettings,
    cancellation: AnalysisCancellation,
    options?: {
      excludedFileKeys?: Set<string>;
      token?: GraphCancellationToken;
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
  outputLimits?: VbProjectContextLimits;
  includeTreeLimits?: VbProjectContextLimits;
  progress?: AspGraphProgressTaskHandle;
  operationCache?: GraphFileIndexOperationCache;
}

export interface CollectIncludeTreeGraphDocumentsOptions {
  excludedFileKeys?: Set<string>;
  initialTextLength?: number;
  limits?: VbProjectContextLimits;
  truncation?: AspGraphDocumentCollectionTruncation;
}

export interface CollectRelatedIncludeTreeGraphDocumentsOptions extends CollectIncludeTreeGraphDocumentsOptions {
  token?: GraphCancellationToken;
}

export interface CollectRelatedIncludeTreeOwnerGraphDocumentsOptions {
  excludedFileKeys?: Set<string>;
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
    const cached = uri ? await host.cachedDocumentForGraphAsync(uri) : undefined;
    throwIfGraphCancelled(cancellation);
    if (!cached) {
      return emptyAspGraphPayload("document", uri, host);
    }
    const settings = host.cachedSettings(cached.source.uri);
    const targetGraphDocument = await host.graphDocumentFromCachedAsync(cached, settings);
    const graphDocumentTruncation: AspGraphDocumentCollectionTruncation = {};
    const includeTreeLimits = options.includeTreeLimits ?? host.graphIncludeTreeLimits(settings);
    const documentsForGraph = await collectIncludeTreeGraphDocumentsAsync(
      targetGraphDocument,
      settings,
      cancellation,
      { limits: includeTreeLimits, truncation: graphDocumentTruncation },
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
        )))
    ) {
      usedWorkspaceIndexForDocumentGraph = true;
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
            initialTextLength: graphDocumentsTextLength(documentsForGraph),
            limits: includeTreeLimits,
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
    await host.ensureWorkspaceIndexAsync(settings, token);
    throwIfGraphCancelled(cancellation);
    const opened = new Set<string>();
    const documentsForGraph: AspGraphDocument[] = [];
    const openDocuments = await host.workspaceAnalyzableOpenDocumentsAsync(settings);
    throwIfGraphCancelled(cancellation);
    const concurrency = host.analysisConcurrency(settings);
    progress?.update({
      current: 0,
      total: openDocuments.length,
      detail: host.progressFileLabel(folderName),
    });
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
    const indexedEntries = host
      .workspaceIndexValues()
      .filter(
        (entry) =>
          !opened.has(host.graphFileKey(entry.fileName)) &&
          isFileInDirectory(entry.fileName, folderName),
      );
    progress?.update({
      current: openDocuments.length,
      total: openDocuments.length + indexedEntries.length,
    });
    const indexedGraphDocuments = await host.mapWithConcurrency(
      indexedEntries,
      concurrency,
      async (entry) => {
        throwIfGraphCancelled(cancellation);
        const cached = await host.cachedFromIndexedAsync(entry, host.cachedSettings(entry.uri));
        throwIfGraphCancelled(cancellation);
        return host.graphDocumentFromCachedAsync(cached, host.cachedSettings(entry.uri));
      },
      progress
        ? host.progressMapHooks(
            progress,
            (entry: AspGraphWorkspaceIndexedDocument) => host.progressFileLabel(entry.fileName),
            openDocuments.length,
          )
        : undefined,
    );
    appendAspGraphDocuments(documentsForGraph, indexedGraphDocuments);
    if (settings.graph?.showIncomingFolderIncludes === true) {
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
            token,
          },
        ),
      );
    }
    return host.graphPayloadFromDocumentsAsync("folder", documentsForGraph, settings, {
      rootUri: host.pathToFileUri(folderName),
      truncated: host.workspaceIndexTruncated()
        ? {
            reason: `workspaceIndex>${settings.workspace?.maxIndexFiles ?? host.defaultMaxIndexFiles}`,
          }
        : undefined,
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
    await host.ensureWorkspaceIndexAsync(settings, token);
    throwIfGraphCancelled(cancellation);
    const opened = new Set<string>();
    const documentsForGraph: AspGraphDocument[] = [];
    const openDocuments = await host.workspaceAnalyzableOpenDocumentsAsync(settings);
    throwIfGraphCancelled(cancellation);
    const concurrency = host.analysisConcurrency(settings);
    progress?.update({ current: 0, total: openDocuments.length });
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
    const indexedEntries = host
      .workspaceIndexValues()
      .filter((entry) => !opened.has(host.graphFileKey(entry.fileName)));
    progress?.update({
      current: openDocuments.length,
      total: openDocuments.length + indexedEntries.length,
    });
    const indexedGraphDocuments = await host.mapWithConcurrency(
      indexedEntries,
      concurrency,
      async (entry) => {
        throwIfGraphCancelled(cancellation);
        const cached = await host.cachedFromIndexedAsync(entry, host.cachedSettings(entry.uri));
        throwIfGraphCancelled(cancellation);
        return host.graphDocumentFromCachedAsync(cached, host.cachedSettings(entry.uri));
      },
      progress
        ? host.progressMapHooks(
            progress,
            (entry: AspGraphWorkspaceIndexedDocument) => host.progressFileLabel(entry.fileName),
            openDocuments.length,
          )
        : undefined,
    );
    appendAspGraphDocuments(documentsForGraph, indexedGraphDocuments);
    return host.graphPayloadFromDocumentsAsync("workspace", documentsForGraph, settings, {
      truncated: host.workspaceIndexTruncated()
        ? {
            reason: `workspaceIndex>${settings.workspace?.maxIndexFiles ?? host.defaultMaxIndexFiles}`,
          }
        : undefined,
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
      visited.add(documentKey);
      documentsForGraph.push(document);
      textLength += document.text.length;
      const includeRefs = await host.graphIncludeRefsForDocumentAsync(document, settings);
      throwIfGraphCancelled(cancellation);
      await prefetchGraphIncludeTargetsAsync(document.uri, includeRefs, settings, cancellation);
      for (const include of includeRefs) {
        throwIfGraphCancelled(cancellation);
        const resolved = await host.resolveIncludePathDetailsAsync(
          document.uri,
          include.path,
          include.mode,
          settings,
        );
        throwIfGraphCancelled(cancellation);
        const includeKey = host.graphFileKey(resolved.fileName);
        if (!resolved.exists || visited.has(includeKey) || excludedFileKeys.has(includeKey)) {
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
        await Promise.all([
          host.fileSizeAsync(resolved.fileName, settings),
          host.graphDocumentFromIncludeFileAsync(resolved.fileName, settings),
        ]).catch(() => undefined);
      },
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
            initialTextLength: textLength,
            limits,
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
      const indexedByFileKey = new Map<string, AspGraphIndexedDocument>();
      const declarationFileKeyById = new Map<string, string>();
      const declarationOrderById = new Map<string, number>();
      const declarationsByName = new Map<string, Array<VbSymbolIndex["declarations"][number]>>();
      let declarationOrder = 0;
      for (const indexed of indexedDocuments) {
        const fileKey = host.graphFileKey(indexed.document.fileName);
        indexedByFileKey.set(fileKey, indexed);
        for (const declaration of indexed.graphIndex.vbSymbolIndex.declarations) {
          declarationFileKeyById.set(declaration.id, fileKey);
          declarationOrderById.set(declaration.id, declarationOrder);
          declarationOrder += 1;
          if (isImplicitGlobalMergeDeclaration(declaration)) {
            pushAspGraphMapItem(declarationsByName, declaration.normalizedName, declaration);
          }
        }
      }
      if (declarationsByName.size === 0) {
        return indexedDocuments;
      }
      const summary = implicitGlobalGroupSummary(declarationsByName.values());
      host.logDebugSummary(
        settings,
        `[asp-lsp] asp.graph.implicitGlobals.groups: groups=${summary.groups}, maxGroupSize=${summary.maxGroupSize}`,
      );
      const includeGraph = await implicitGlobalIncludeGraphAsync(
        indexedDocuments,
        indexedByFileKey,
        settings,
        cancellation,
      );
      const targetKeys = new Set<string>();
      for (const declarations of declarationsByName.values()) {
        for (const declaration of declarations) {
          const fileKey = declarationFileKeyById.get(declaration.id);
          if (fileKey) {
            targetKeys.add(fileKey);
          }
        }
      }
      const reachability = precomputeIncludeReachability(includeGraph, targetKeys);
      const union = new ImplicitGlobalUnionFind();
      for (const declarations of declarationsByName.values()) {
        throwIfGraphCancelled(cancellation);
        if (
          declarations.length < 2 ||
          !declarations.some((declaration) => declaration.implicitGlobal === true)
        ) {
          continue;
        }
        for (let leftIndex = 0; leftIndex < declarations.length; leftIndex += 1) {
          const left = declarations[leftIndex];
          const leftFileKey = declarationFileKeyById.get(left.id);
          if (!leftFileKey) {
            continue;
          }
          for (let rightIndex = leftIndex + 1; rightIndex < declarations.length; rightIndex += 1) {
            const right = declarations[rightIndex];
            const rightFileKey = declarationFileKeyById.get(right.id);
            if (!rightFileKey) {
              continue;
            }
            if (union.find(left.id) === union.find(right.id)) {
              continue;
            }
            if (
              isImplicitGlobalDeclarationVisibleFromFile(
                includeGraph,
                leftFileKey,
                right,
                rightFileKey,
                left.nameRange,
                reachability,
              ) ||
              isImplicitGlobalDeclarationVisibleFromFile(
                includeGraph,
                rightFileKey,
                left,
                leftFileKey,
                right.nameRange,
                reachability,
              )
            ) {
              union.union(left.id, right.id);
            }
          }
        }
      }
      const declarationsByRoot = new Map<string, Array<VbSymbolIndex["declarations"][number]>>();
      for (const declarations of declarationsByName.values()) {
        for (const declaration of declarations) {
          const root = union.find(declaration.id);
          if (root !== declaration.id || union.size(root) > 1) {
            pushAspGraphMapItem(declarationsByRoot, root, declaration);
          }
        }
      }
      const canonicalIdById = new Map<string, string>();
      for (const declarations of declarationsByRoot.values()) {
        const canonical = implicitGlobalCanonicalDeclaration(
          declarations,
          declarationOrderById,
          declarationFileKeyById,
          includeGraph,
          reachability,
        );
        for (const declaration of declarations) {
          canonicalIdById.set(declaration.id, canonical.id);
        }
      }
      if (![...canonicalIdById].some(([id, canonicalId]) => id !== canonicalId)) {
        return indexedDocuments;
      }
      return indexedDocuments.map((indexed) =>
        canonicalizeImplicitGlobalIndexedDocument(
          indexed,
          canonicalIdById,
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

  async function implicitGlobalIncludeGraphAsync(
    indexedDocuments: AspGraphIndexedDocument[],
    indexedByFileKey: Map<string, AspGraphIndexedDocument>,
    settings: AspSettings,
    cancellation: AnalysisCancellation,
  ): Promise<ImplicitGlobalIncludeGraph> {
    const includeGraph: ImplicitGlobalIncludeGraph = {
      directIncludesByOwnerKey: new Map(),
      parentIncludesByTargetKey: new Map(),
    };
    await host.mapWithConcurrency(
      indexedDocuments,
      host.analysisConcurrency(settings),
      async (indexed): Promise<void> => {
        throwIfGraphCancelled(cancellation);
        const ownerKey = host.graphFileKey(indexed.document.fileName);
        for (const include of indexed.graphIndex.includeRefs) {
          const resolved = await host.resolveIncludePathDetailsAsync(
            indexed.document.uri,
            include.path,
            include.mode,
            settings,
          );
          const targetKey = host.graphFileKey(host.normalizeFileName(resolved.fileName));
          if (!indexedByFileKey.has(targetKey)) {
            continue;
          }
          pushAspGraphMapItem(includeGraph.directIncludesByOwnerKey, ownerKey, {
            range: include.range,
            targetKey,
          });
          pushAspGraphMapItem(includeGraph.parentIncludesByTargetKey, targetKey, {
            ownerKey,
            range: include.range,
          });
        }
      },
    );
    return includeGraph;
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

interface IncludeReachabilityGraph {
  directIncludesByOwnerKey: Map<string, Array<{ targetKey: string }>>;
  parentIncludesByTargetKey: Map<string, Array<{ ownerKey: string }>>;
}

interface ImplicitGlobalIncludeGraph {
  directIncludesByOwnerKey: Map<string, Array<{ range: Range; targetKey: string }>>;
  parentIncludesByTargetKey: Map<string, Array<{ ownerKey: string; range: Range }>>;
}

function isImplicitGlobalMergeDeclaration(
  declaration: VbSymbolIndex["declarations"][number],
): boolean {
  return (
    declaration.kind === "variable" &&
    declaration.bindingScope === "global" &&
    !declaration.memberOf &&
    (declaration.implicitGlobal === true || declaration.implicit !== true)
  );
}

function implicitGlobalCanonicalDeclaration(
  declarations: Array<VbSymbolIndex["declarations"][number]>,
  declarationOrderById: Map<string, number>,
  declarationFileKeyById: Map<string, string>,
  includeGraph: ImplicitGlobalIncludeGraph,
  reachability: PrecomputedIncludeReachability,
): VbSymbolIndex["declarations"][number] {
  const visibilityScoreById = new Map(
    declarations.map((declaration) => [
      declaration.id,
      implicitGlobalCanonicalVisibilityScore(
        declaration,
        declarations,
        declarationFileKeyById,
        includeGraph,
        reachability,
      ),
    ]),
  );
  return [...declarations].sort(
    (left, right) =>
      (visibilityScoreById.get(left.id) ?? 1) - (visibilityScoreById.get(right.id) ?? 1) ||
      implicitGlobalCanonicalScore(left) - implicitGlobalCanonicalScore(right) ||
      (declarationOrderById.get(left.id) ?? 0) - (declarationOrderById.get(right.id) ?? 0),
  )[0];
}

function implicitGlobalCanonicalVisibilityScore(
  declaration: VbSymbolIndex["declarations"][number],
  declarations: Array<VbSymbolIndex["declarations"][number]>,
  declarationFileKeyById: Map<string, string>,
  includeGraph: ImplicitGlobalIncludeGraph,
  reachability: PrecomputedIncludeReachability,
): number {
  const targetKey = declarationFileKeyById.get(declaration.id);
  if (!targetKey) {
    return 1;
  }
  return declarations.every((candidate) => {
    if (candidate.id === declaration.id) {
      return true;
    }
    const ownerKey = declarationFileKeyById.get(candidate.id);
    return (
      ownerKey !== undefined &&
      isImplicitGlobalDeclarationVisibleFromFile(
        includeGraph,
        ownerKey,
        declaration,
        targetKey,
        candidate.nameRange,
        reachability,
      )
    );
  })
    ? 0
    : 1;
}

function implicitGlobalCanonicalScore(declaration: VbSymbolIndex["declarations"][number]): number {
  if (declaration.implicit !== true) {
    return 0;
  }
  return declaration.implicitGlobalCandidate === true ? 2 : 1;
}

function isImplicitGlobalDeclarationVisibleFromFile(
  includeGraph: ImplicitGlobalIncludeGraph,
  ownerKey: string,
  declaration: VbSymbolIndex["declarations"][number],
  declarationKey: string,
  referenceRange: Range,
  reachability: PrecomputedIncludeReachability,
): boolean {
  if (declarationKey === ownerKey) {
    return true;
  }
  if (
    hasEarlierReachableImplicitGlobalInclude(
      includeGraph,
      ownerKey,
      declarationKey,
      referenceRange,
      reachability,
    )
  ) {
    return true;
  }
  return isImplicitGlobalDeclarationVisibleFromParentContext(
    includeGraph,
    ownerKey,
    declaration,
    declarationKey,
    reachability,
    new Set([ownerKey]),
  );
}

function isImplicitGlobalDeclarationVisibleFromParentContext(
  includeGraph: ImplicitGlobalIncludeGraph,
  ownerKey: string,
  declaration: VbSymbolIndex["declarations"][number],
  declarationKey: string,
  reachability: PrecomputedIncludeReachability,
  visited: Set<string>,
): boolean {
  for (const parentInclude of includeGraph.parentIncludesByTargetKey.get(ownerKey) ?? []) {
    if (visited.has(parentInclude.ownerKey)) {
      continue;
    }
    visited.add(parentInclude.ownerKey);
    if (
      isImplicitGlobalDeclarationVisibleBeforeParentInclude(
        includeGraph,
        parentInclude.ownerKey,
        declaration,
        declarationKey,
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

function isImplicitGlobalDeclarationVisibleBeforeParentInclude(
  includeGraph: ImplicitGlobalIncludeGraph,
  parentKey: string,
  declaration: VbSymbolIndex["declarations"][number],
  declarationKey: string,
  includeRange: Range,
  reachability: PrecomputedIncludeReachability,
  visited: Set<string>,
): boolean {
  if (declarationKey === parentKey) {
    return positionBeforeOrEqual(declaration.nameRange.start, includeRange.start);
  }
  if (
    hasEarlierReachableImplicitGlobalInclude(
      includeGraph,
      parentKey,
      declarationKey,
      includeRange,
      reachability,
    )
  ) {
    return true;
  }
  return isImplicitGlobalDeclarationVisibleFromParentContext(
    includeGraph,
    parentKey,
    declaration,
    declarationKey,
    reachability,
    visited,
  );
}

function hasEarlierReachableImplicitGlobalInclude(
  includeGraph: ImplicitGlobalIncludeGraph,
  ownerKey: string,
  targetKey: string,
  referenceRange: Range,
  reachability?: PrecomputedIncludeReachability,
): boolean {
  return (includeGraph.directIncludesByOwnerKey.get(ownerKey) ?? []).some((include) => {
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
        isImplicitGlobalIncludeReachable(
          includeGraph,
          include.targetKey,
          targetKey,
          new Set([ownerKey]),
        ))
    );
  });
}

function isImplicitGlobalIncludeReachable(
  includeGraph: ImplicitGlobalIncludeGraph,
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
  return (includeGraph.directIncludesByOwnerKey.get(startKey) ?? []).some(
    (include) =>
      include.targetKey === targetKey ||
      isImplicitGlobalIncludeReachable(includeGraph, include.targetKey, targetKey, visited),
  );
}

function canonicalizeImplicitGlobalIndexedDocument(
  indexed: AspGraphIndexedDocument,
  canonicalIdById: Map<string, string>,
  graphFileIndexFingerprint: (index: VbSymbolIndex) => string,
): AspGraphIndexedDocument {
  const index = indexed.graphIndex.vbSymbolIndex;
  const canonicalDeclarationIds = new Set(canonicalIdById.values());
  const declarations = index.declarations.filter((declaration) => {
    const canonicalId = canonicalIdById.get(declaration.id);
    return (
      !canonicalId || canonicalId === declaration.id || canonicalDeclarationIds.has(declaration.id)
    );
  });
  const canonicalResolvedId = (resolvedId: string | undefined): string | undefined =>
    resolvedId ? (canonicalIdById.get(resolvedId) ?? resolvedId) : undefined;
  const references = index.references.map((reference) => ({
    ...reference,
    resolvedId: canonicalResolvedId(reference.resolvedId),
  }));
  const callSites = index.callSites.map((callSite) => ({
    ...callSite,
    resolvedId: canonicalResolvedId(callSite.resolvedId),
  }));
  const deferredExternalRefs = index.deferredExternalRefs.map((ref) => ({
    ...ref,
    localResolutionId: canonicalResolvedId(ref.localResolutionId),
  }));
  const vbSymbolIndex: VbSymbolIndex = {
    ...index,
    declarations,
    references,
    callSites,
    deferredExternalRefs,
    stats: {
      ...index.stats,
      declarations: declarations.length,
      references: references.length,
      callSites: callSites.length,
      deferredExternalRefs: deferredExternalRefs.length,
    },
  };
  return {
    ...indexed,
    graphIndex: {
      ...indexed.graphIndex,
      vbSymbolIndex,
      fingerprint: graphFileIndexFingerprint(vbSymbolIndex),
    },
  };
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

function pushAspGraphMapItem<K, V>(map: Map<K, V[]>, key: K, value: V): void {
  const existing = map.get(key);
  if (existing) {
    existing.push(value);
  } else {
    map.set(key, [value]);
  }
}

function positionBeforeOrEqual(left: Position, right: Position): boolean {
  return left.line < right.line || (left.line === right.line && left.character <= right.character);
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

function graphDocumentsTextLength(documentsForGraph: AspGraphDocument[]): number {
  return documentsForGraph.reduce((total, document) => total + document.text.length, 0);
}

function appendAspGraphDocuments(
  target: AspGraphDocument[],
  documentsToAppend: readonly AspGraphDocument[],
): void {
  for (const document of documentsToAppend) {
    target.push(document);
  }
}
