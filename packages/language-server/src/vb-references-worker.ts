import fs from "node:fs";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { parentPort } from "node:worker_threads";
import { analyse, detect } from "chardet";
import {
  buildVbTypeEnvironment,
  extractAspIncludeRefs,
  extractVbscriptSymbolIndex,
  getVbscriptReferencesForSymbols,
  hydrateVbscriptCst,
  parseAspDocumentAsync,
  parseVbscriptTypeRef,
  summarizeAspFileAnalysisAsync,
  vbscriptReferenceSymbolKey,
  type AspInclude,
  type AspLegacyEncoding,
  type AspParsedDocument,
  type AspSettings,
  type FileAnalysisSummary,
  type VbProjectContext,
  type VbReference,
  type VbSymbol,
  type VbType,
  type VbTypeEnvironment,
} from "@asp-lsp/core";
import {
  fileIdentityKeyFromFileName,
  fileIdentityKeyFromUri,
  sameFileIdentityUri,
} from "./file-identity";
import type {
  VbReferencesWorkerOpenDocument,
  VbReferencesWorkerRequest,
  VbReferencesWorkerResponse,
  VbReferencesWorkerTargetSymbol,
} from "./vb-references-protocol";
import {
  DiskAnalysisCache,
  diskContentHash,
  type DiskIncludeRefsCacheEntry,
  type DiskAnalysisSourceMetadata,
  type DiskParsedDocumentCacheEntry,
} from "./disk-analysis-cache";

if (!parentPort) {
  throw new Error("VBScript references worker requires a parent port.");
}

parentPort.on("message", (request: VbReferencesWorkerRequest) => {
  void handleRequest(request);
});

async function handleRequest(request: VbReferencesWorkerRequest): Promise<void> {
  try {
    const result =
      request.kind === "extractSymbolIndex"
        ? await runSymbolIndexExtraction(request)
        : await runWorkspaceReferenceAnalysis(request);
    parentPort?.postMessage(result);
  } catch (error) {
    parentPort?.postMessage({
      id: request.id,
      candidate: request.candidate,
      references: [],
      error: {
        name: error instanceof Error ? error.name : undefined,
        message: error instanceof Error ? error.message : String(error),
        stack: error instanceof Error ? error.stack : undefined,
      },
    } satisfies VbReferencesWorkerResponse);
  }
}

async function runSymbolIndexExtraction(
  request: VbReferencesWorkerRequest,
): Promise<VbReferencesWorkerResponse> {
  await testDelayFromEnv();
  const openDocuments = openDocumentMap(request.openDocuments);
  const text =
    request.candidate.text ??
    (await readWorkspaceText(request.candidate.fileName, request.settings, openDocuments))?.text;
  if (text === undefined) {
    throw new Error(`Unable to read ${request.candidate.fileName} for symbol extraction.`);
  }
  return {
    id: request.id,
    candidate: request.candidate,
    symbolIndex: extractVbscriptSymbolIndex(request.candidate.uri, text, request.settings, {
      includeImplicitVariables: true,
    }),
  };
}

async function runWorkspaceReferenceAnalysis(
  request: VbReferencesWorkerRequest,
): Promise<VbReferencesWorkerResponse> {
  await testDelayFromEnv();
  const openDocuments = openDocumentMap(request.openDocuments);
  const cacheStats: WorkerDiskCacheStats = { includeRefsHits: 0, parsedDocumentHits: 0 };
  const closure = await collectLightweightIncludeClosure(request, openDocuments, cacheStats);
  const targets = request.targets?.length ? request.targets : [request.target];
  const targetFileNames = targets
    .map(targetFileNameFromSymbol)
    .filter((fileName): fileName is string => Boolean(fileName));
  if (
    closure.complete &&
    targetFileNames.length > 0 &&
    !targetFileNames.some((fileName) => closure.files.has(fileIdentityKeyFromFileName(fileName)))
  ) {
    return {
      id: request.id,
      candidate: request.candidate,
      references: [],
      referencesByTarget: emptyReferenceMap(targets),
      fallbackReasons: [],
      scannedFiles: closure.files.size,
      cacheHits: closure.openHits + workerDiskCacheHits(cacheStats),
    };
  }

  const referencesByTarget = await fullFallbackReferencesForTargets(
    request,
    openDocuments,
    targets,
    cacheStats,
  );
  const references = referencesByTarget[targetSymbolKey(request.target)] ?? [];
  return {
    id: request.id,
    candidate: request.candidate,
    references,
    referencesByTarget,
    fallbackReasons: [
      closure.complete ? "target-reachable" : "include-closure-incomplete",
      ...closure.reasons,
    ],
    scannedFiles: closure.files.size,
    cacheHits: closure.openHits + workerDiskCacheHits(cacheStats),
  };
}

async function testDelayFromEnv(): Promise<void> {
  if (process.env.NODE_ENV !== "test") {
    return;
  }
  const delayMs = Number(process.env.ASP_LSP_TEST_VB_REFERENCES_WORKER_DELAY_MS ?? 0);
  if (!Number.isFinite(delayMs) || delayMs <= 0) {
    return;
  }
  await new Promise<void>((resolve) => setTimeout(resolve, delayMs));
}

interface LightweightClosure {
  files: Set<string>;
  complete: boolean;
  reasons: string[];
  openHits: number;
}

interface WorkspaceIncludeRefsRead {
  includeRefs: AspInclude[];
  size: number;
  openDocument: boolean;
  cacheHit: boolean;
}

interface WorkerDiskCacheStats {
  includeRefsHits: number;
  parsedDocumentHits: number;
}

function workerDiskCacheHits(stats: WorkerDiskCacheStats): number {
  return stats.includeRefsHits + stats.parsedDocumentHits;
}

async function collectLightweightIncludeClosure(
  request: VbReferencesWorkerRequest,
  openDocuments: Map<string, VbReferencesWorkerOpenDocument>,
  cacheStats: WorkerDiskCacheStats,
): Promise<LightweightClosure> {
  const files = new Set<string>();
  const reasons: string[] = [];
  let complete = true;
  let textLength = 0;
  let openHits = 0;
  let frontier = [
    {
      uri: request.candidate.uri,
      fileName: normalizeFileName(request.candidate.fileName),
      depth: 0,
    },
  ];

  while (frontier.length > 0) {
    const nextFrontier: typeof frontier = [];
    const results = await mapWithConcurrency(
      frontier,
      request.limits.includeReadConcurrency,
      async (item) => {
        if (item.depth > request.limits.maxDepth) {
          return { next: [], reason: "depth-limit" };
        }
        const fileKey = fileIdentityKeyFromFileName(item.fileName);
        if (files.has(fileKey)) {
          return { next: [] };
        }
        files.add(fileKey);
        if (files.size > request.limits.maxDocuments) {
          return { next: [], reason: "document-limit" };
        }
        const includeRefs = await readWorkspaceIncludeRefs(item.fileName, request, openDocuments);
        if (!includeRefs) {
          return { next: [], reason: "read-missing" };
        }
        if (includeRefs.openDocument) {
          openHits += 1;
        }
        if (includeRefs.cacheHit) {
          cacheStats.includeRefsHits += 1;
        }
        textLength += includeRefs.size;
        if (textLength > request.limits.maxTextLength) {
          return { next: [], reason: "text-limit" };
        }
        const next = await mapWithConcurrency(
          includeRefs.includeRefs,
          request.limits.includeReadConcurrency,
          async (include) => {
            const resolved = await resolveIncludePath(
              item.uri,
              include,
              request.settings,
              request.workspaceRoots,
            );
            return resolved.exists
              ? {
                  uri: pathToFileUri(resolved.fileName),
                  fileName: resolved.fileName,
                  depth: item.depth + 1,
                }
              : undefined;
          },
        );
        if (next.some((item) => !item)) {
          return {
            next: next.filter((item): item is NonNullable<typeof item> => Boolean(item)),
            reason: "missing-include",
          };
        }
        return { next: next.filter((item): item is NonNullable<typeof item> => Boolean(item)) };
      },
    );
    for (const result of results) {
      if (result.reason) {
        complete = false;
        reasons.push(result.reason);
      }
      nextFrontier.push(...result.next);
    }
    frontier = nextFrontier.filter(
      (item) => !files.has(fileIdentityKeyFromFileName(item.fileName)),
    );
  }

  return { files, complete, reasons: [...new Set(reasons)], openHits };
}

async function fullFallbackReferencesForTargets(
  request: VbReferencesWorkerRequest,
  openDocuments: Map<string, VbReferencesWorkerOpenDocument>,
  targets: VbReferencesWorkerTargetSymbol[],
  cacheStats: WorkerDiskCacheStats,
): Promise<Record<string, VbReference[]>> {
  const analysis = await cachedFullFallbackAnalysis(request, openDocuments, cacheStats);
  const referencesByTarget = emptyReferenceMap(targets);
  if (!analysis) {
    return referencesByTarget;
  }
  const equivalentTargets: VbSymbol[] = [];
  const equivalentKeys = new Map<string, string>();
  const targetByEquivalentKey = new Map<string, VbReferencesWorkerTargetSymbol>();
  const visibilityMemoByTargetKey = new Map<string, IncludeVisibilityMemo>();
  const supplementalReferencesByTarget = new Map<string, VbReference[]>();
  for (const target of targets) {
    const targetKey = targetSymbolKey(target);
    const visibilityMemo = createIncludeVisibilityMemo();
    visibilityMemoByTargetKey.set(targetKey, visibilityMemo);
    const supplementalReferences = fallbackWorkspaceExternalReferences(
      analysis,
      target,
      visibilityMemo,
    );
    const equivalent = equivalentVbSymbol(analysis.symbols, target);
    if (equivalent) {
      const equivalentKey = vbscriptReferenceSymbolKey(equivalent);
      equivalentTargets.push(equivalent);
      equivalentKeys.set(equivalentKey, targetKey);
      targetByEquivalentKey.set(equivalentKey, target);
      supplementalReferencesByTarget.set(targetKey, supplementalReferences);
      continue;
    }
    referencesByTarget[targetKey] = supplementalReferences;
  }
  const batch = getVbscriptReferencesForSymbols(
    equivalentTargets,
    analysis.context,
    request.options,
  );
  for (const [equivalentKey, references] of batch) {
    const targetKey = equivalentKeys.get(equivalentKey);
    if (targetKey) {
      const target = targetByEquivalentKey.get(equivalentKey);
      const visibilityMemo =
        visibilityMemoByTargetKey.get(targetKey) ?? createIncludeVisibilityMemo();
      const visibleReferences = target
        ? references.filter((reference) =>
            isFallbackTargetVisibleAt(
              analysis,
              reference.uri,
              target,
              reference.range,
              visibilityMemo,
            ),
          )
        : references;
      referencesByTarget[targetKey] = mergeReferences(
        visibleReferences,
        supplementalReferencesByTarget.get(targetKey) ?? [],
      );
    }
  }
  return referencesByTarget;
}

interface FullFallbackAnalysis {
  context: VbProjectContext;
  summaries: FileAnalysisSummary[];
  symbols: VbSymbol[];
  includeGraph: FullFallbackIncludeGraph;
}

interface FullFallbackIncludeGraph {
  directIncludesByOwnerUri: Map<string, Array<{ range: VbReference["range"]; targetUri: string }>>;
  parentIncludesByTargetUri: Map<string, Array<{ ownerUri: string; range: VbReference["range"] }>>;
}

interface IncludeVisibilityMemo {
  cache: Map<string, boolean>;
  visiting: Set<string>;
}

interface FullFallbackAnalysisCacheEntry {
  key: string;
  analysis: FullFallbackAnalysis;
  lastUsed: number;
}

const fullFallbackAnalysisCache = new Map<string, FullFallbackAnalysisCacheEntry>();
const maxFullFallbackAnalysisCacheEntries = 64;

async function cachedFullFallbackAnalysis(
  request: VbReferencesWorkerRequest,
  openDocuments: Map<string, VbReferencesWorkerOpenDocument>,
  cacheStats: WorkerDiskCacheStats,
): Promise<FullFallbackAnalysis | undefined> {
  const key = fullFallbackAnalysisCacheKey(request);
  const cached = fullFallbackAnalysisCache.get(key);
  if (cached) {
    cached.lastUsed = Date.now();
    return cached.analysis;
  }
  const documents = await collectFullDocuments(request, openDocuments, cacheStats);
  if (documents.length === 0) {
    return undefined;
  }
  const contextSettings = vbProjectContextSettings(request.settings);
  const summaries = await Promise.all(
    documents.map((document) => summarizeAspFileAnalysisAsync(document, contextSettings)),
  );
  await writeDiskParsedDocuments(documents, summaries, request, openDocuments);
  const includeGraph = await fullFallbackIncludeGraph(documents, request);
  const symbols = summaries.flatMap((summary) => summary.vbscript?.localSymbols ?? []);
  symbols.push(...configuredVbscriptGlobals(documents[0], request.settings));
  const typeEnvironment = mergeVbTypeEnvironment(
    buildVbTypeEnvironment(documents[0], { ...contextSettings, symbols }),
    summaries.flatMap((summary) => summary.vbscript?.typeFacts ?? []),
    symbols,
  );
  const context: VbProjectContext = {
    documents,
    symbols,
    typeEnvironment,
    externalRefUsages: summaries.flatMap((summary) => summaryVbReferenceUsages(summary)),
    ...contextSettings,
  };
  const analysis = { context, summaries, symbols, includeGraph };
  fullFallbackAnalysisCache.set(key, { key, analysis, lastUsed: Date.now() });
  pruneFullFallbackAnalysisCache();
  return analysis;
}

async function fullFallbackIncludeGraph(
  documents: AspParsedDocument[],
  request: VbReferencesWorkerRequest,
): Promise<FullFallbackIncludeGraph> {
  const graph: FullFallbackIncludeGraph = {
    directIncludesByOwnerUri: new Map(),
    parentIncludesByTargetUri: new Map(),
  };
  await mapWithConcurrency(documents, request.limits.includeReadConcurrency, async (document) => {
    for (const include of document.includes) {
      const resolved = await resolveIncludePath(
        document.uri,
        include,
        request.settings,
        request.workspaceRoots,
      );
      if (!resolved.exists) {
        continue;
      }
      const ownerKey = fileIdentityKeyFromUri(document.uri);
      const targetKey = fileIdentityKeyFromFileName(resolved.fileName);
      pushMapItem(graph.directIncludesByOwnerUri, ownerKey, {
        range: include.range,
        targetUri: targetKey,
      });
      pushMapItem(graph.parentIncludesByTargetUri, targetKey, {
        ownerUri: ownerKey,
        range: include.range,
      });
    }
  });
  return graph;
}

function fullFallbackAnalysisCacheKey(request: VbReferencesWorkerRequest): string {
  return JSON.stringify({
    candidate: {
      ...request.candidate,
      uri: fileIdentityKeyFromUri(request.candidate.uri),
      fileName: fileIdentityKeyFromFileName(request.candidate.fileName),
      source: {
        ...request.candidate.source,
        fileName: fileIdentityKeyFromFileName(request.candidate.source.fileName),
      },
    },
    settings: request.settings,
    workspaceRoots: request.workspaceRoots.map(fileIdentityKeyFromFileName),
    openDocuments: request.openDocuments.map((document) => ({
      uri: fileIdentityKeyFromUri(document.uri),
      fileName: fileIdentityKeyFromFileName(document.fileName),
      version: document.version,
      length: document.text.length,
      fingerprint: textFingerprint(document.text),
    })),
    limits: request.limits,
  });
}

function emptyReferenceMap(
  targets: VbReferencesWorkerTargetSymbol[],
): Record<string, VbReference[]> {
  return Object.fromEntries(targets.map((target) => [targetSymbolKey(target), []]));
}

function targetSymbolKey(symbol: VbReferencesWorkerTargetSymbol): string {
  const range = targetSymbolIdentityRange(symbol);
  return [
    fileIdentityKeyFromUri(symbol.sourceUri),
    symbol.kind,
    symbol.memberOf ?? "",
    symbol.name.toLowerCase(),
    range?.start.line ?? "",
    range?.start.character ?? "",
  ].join("|");
}

function targetSymbolIdentityRange(
  symbol: VbReferencesWorkerTargetSymbol,
): VbReferencesWorkerTargetSymbol["range"] | undefined {
  return symbol.kind === "property" ? undefined : symbol.range;
}

function textFingerprint(text: string): string {
  let hash = 0x811c9dc5;
  for (let index = 0; index < text.length; index += 1) {
    hash ^= text.charCodeAt(index);
    hash = Math.imul(hash, 0x01000193);
  }
  return `${text.length}:${(hash >>> 0).toString(16)}`;
}

function pruneFullFallbackAnalysisCache(): void {
  while (fullFallbackAnalysisCache.size > maxFullFallbackAnalysisCacheEntries) {
    const oldest = [...fullFallbackAnalysisCache.entries()].sort(
      (left, right) => left[1].lastUsed - right[1].lastUsed,
    )[0];
    if (!oldest) {
      return;
    }
    fullFallbackAnalysisCache.delete(oldest[0]);
  }
}

async function collectFullDocuments(
  request: VbReferencesWorkerRequest,
  openDocuments: Map<string, VbReferencesWorkerOpenDocument>,
  cacheStats: WorkerDiskCacheStats,
): Promise<AspParsedDocument[]> {
  const documents: AspParsedDocument[] = [];
  const visited = new Set<string>();
  let textLength = 0;

  const visit = async (uri: string, fileName: string, depth: number): Promise<void> => {
    const normalized = normalizeFileName(fileName);
    const fileKey = fileIdentityKeyFromFileName(normalized);
    if (
      depth > request.limits.maxDepth ||
      visited.has(fileKey) ||
      documents.length >= request.limits.maxDocuments ||
      textLength > request.limits.maxTextLength
    ) {
      return;
    }
    const cachedParsed = await readDiskParsedDocument(normalized, request, openDocuments);
    if (cachedParsed) {
      cacheStats.parsedDocumentHits += 1;
      visited.add(fileKey);
      textLength += cachedParsed.parsed.text.length;
      documents.push(cachedParsed.parsed);
      for (const include of cachedParsed.parsed.includes) {
        const resolved = await resolveIncludePath(
          cachedParsed.parsed.uri,
          include,
          request.settings,
          request.workspaceRoots,
        );
        if (resolved.exists) {
          await visit(pathToFileUri(resolved.fileName), resolved.fileName, depth + 1);
        }
      }
      return;
    }
    const text = await readWorkspaceText(normalized, request.settings, openDocuments);
    if (!text) {
      return;
    }
    visited.add(fileKey);
    textLength += text.text.length;
    const parsed = await parseAspDocumentAsync(uri, text.text, request.settings);
    await hydrateVbscriptCst(parsed, request.settings);
    documents.push(parsed);
    for (const include of parsed.includes) {
      const resolved = await resolveIncludePath(
        parsed.uri,
        include,
        request.settings,
        request.workspaceRoots,
      );
      if (resolved.exists) {
        await visit(pathToFileUri(resolved.fileName), resolved.fileName, depth + 1);
      }
    }
  };

  await visit(request.candidate.uri, request.candidate.fileName, 0);
  return documents;
}

async function readWorkspaceIncludeRefs(
  fileName: string,
  request: VbReferencesWorkerRequest,
  openDocuments: Map<string, VbReferencesWorkerOpenDocument>,
): Promise<WorkspaceIncludeRefsRead | undefined> {
  const normalized = normalizeFileName(fileName);
  const openDocument = openDocuments.get(normalized);
  if (openDocument) {
    return {
      includeRefs: extractAspIncludeRefs(openDocument.text),
      size: openDocument.text.length,
      openDocument: true,
      cacheHit: false,
    };
  }
  const cached = await readDiskIncludeRefs(normalized, request);
  if (cached) {
    return {
      includeRefs: cached.includeRefs,
      size: cached.source.size,
      openDocument: false,
      cacheHit: true,
    };
  }
  const text = await readWorkspaceText(normalized, request.settings, openDocuments);
  return text
    ? {
        includeRefs: extractAspIncludeRefs(text.text),
        size: text.text.length,
        openDocument: text.openDocument,
        cacheHit: false,
      }
    : undefined;
}

async function readDiskIncludeRefs(
  fileName: string,
  request: VbReferencesWorkerRequest,
): Promise<DiskIncludeRefsCacheEntry | undefined> {
  const cache = diskCacheForRequest(request);
  if (!cache) {
    return undefined;
  }
  const source = await sourceMetadataForDiskRead(fileName, request);
  return source
    ? cache
        .readIncludeRefs({ source, settingsKey: includeRefsSettingsKey(request.settings) })
        .catch(() => undefined)
    : undefined;
}

async function readDiskParsedDocument(
  fileName: string,
  request: VbReferencesWorkerRequest,
  openDocuments: Map<string, VbReferencesWorkerOpenDocument>,
): Promise<DiskParsedDocumentCacheEntry | undefined> {
  if (openDocuments.has(fileIdentityKeyFromFileName(fileName))) {
    return undefined;
  }
  const cache = diskCacheForRequest(request);
  if (!cache) {
    return undefined;
  }
  const source = await sourceMetadataForDiskRead(fileName, request);
  return source
    ? cache
        .readParsedDocument({ source, settingsKey: includeSummarySettingsKey(request.settings) })
        .catch(() => undefined)
    : undefined;
}

async function writeDiskParsedDocuments(
  documents: AspParsedDocument[],
  summaries: FileAnalysisSummary[],
  request: VbReferencesWorkerRequest,
  openDocuments: Map<string, VbReferencesWorkerOpenDocument>,
): Promise<void> {
  const cache = diskCacheForRequest(request);
  if (!cache) {
    return;
  }
  await Promise.all(
    documents.map(async (document, index) => {
      const fileName = normalizeFileName(uriToFileName(document.uri));
      if (openDocuments.has(fileIdentityKeyFromFileName(fileName))) {
        return;
      }
      const source = await sourceMetadataForDiskRead(fileName, request);
      const summary = summaries[index];
      if (!source || !summary) {
        return;
      }
      const contentSource = { ...source, contentHash: diskContentHash(document.text) };
      await Promise.all([
        cache.writeParsedDocument({
          source: contentSource,
          settingsKey: includeSummarySettingsKey(request.settings),
          parsed: document,
          summary,
        }),
        cache.writeSummary({
          source: contentSource,
          settingsKey: includeSummarySettingsKey(request.settings),
          summary,
        }),
        cache.writeIncludeRefs({
          source: contentSource,
          settingsKey: includeRefsSettingsKey(request.settings),
          includeRefs: summary.includeRefs,
          fingerprint: includeRefsFingerprint(summary.includeRefs),
        }),
      ]).catch(() => undefined);
    }),
  );
}

function diskCacheForRequest(request: VbReferencesWorkerRequest): DiskAnalysisCache | undefined {
  return request.cache?.disk.enabled === false || !request.cache
    ? undefined
    : new DiskAnalysisCache(request.cache.disk);
}

async function sourceMetadataForDiskRead(
  fileName: string,
  request: VbReferencesWorkerRequest,
): Promise<DiskAnalysisSourceMetadata | undefined> {
  const normalized = normalizeFileName(fileName);
  if (request.cache?.freshness === "watch") {
    const source = sourceManifestForRequest(request).get(fileIdentityKeyFromFileName(normalized));
    if (source) {
      return source;
    }
  }
  const stat = await fs.promises.stat(normalized).catch(() => undefined);
  return stat?.isFile()
    ? {
        fileName: normalized,
        mtimeMs: stat.mtimeMs,
        size: stat.size,
      }
    : undefined;
}

const sourceManifestCache = new WeakMap<
  VbReferencesWorkerRequest,
  Map<string, DiskAnalysisSourceMetadata>
>();

function sourceManifestForRequest(
  request: VbReferencesWorkerRequest,
): Map<string, DiskAnalysisSourceMetadata> {
  const cached = sourceManifestCache.get(request);
  if (cached) {
    return cached;
  }
  const manifest = new Map(
    (request.cache?.sourceManifest ?? []).map((source) => [
      fileIdentityKeyFromFileName(source.fileName),
      {
        fileName: normalizeFileName(source.fileName),
        mtimeMs: source.mtimeMs,
        size: source.size,
        contentHash: source.contentHash,
      },
    ]),
  );
  sourceManifestCache.set(request, manifest);
  return manifest;
}

interface ReadWorkspaceText {
  text: string;
  openDocument: boolean;
}

async function readWorkspaceText(
  fileName: string,
  settings: AspSettings,
  openDocuments: Map<string, VbReferencesWorkerOpenDocument>,
): Promise<ReadWorkspaceText | undefined> {
  const normalized = normalizeFileName(fileName);
  const openDocument = openDocuments.get(fileIdentityKeyFromFileName(normalized));
  if (openDocument) {
    return { text: openDocument.text, openDocument: true };
  }
  const stat = await fs.promises.stat(normalized).catch(() => undefined);
  if (!stat?.isFile()) {
    return undefined;
  }
  return {
    text: decodeLegacyText(await fs.promises.readFile(normalized), settings.legacyEncoding),
    openDocument: false,
  };
}

interface IncludeResolution {
  fileName: string;
  exists: boolean;
}

async function resolveIncludePath(
  ownerUri: string,
  include: AspInclude,
  settings: AspSettings,
  workspaceRoots: string[],
): Promise<IncludeResolution> {
  if (include.mode === "virtual") {
    const normalizedInclude = include.path.replace(/^\/+/, "");
    const ownerRoot =
      workspaceRootFromUri(ownerUri, workspaceRoots) ?? path.dirname(uriToFileName(ownerUri));
    for (const root of [
      ...(settings.virtualRoots ?? []),
      settings.virtualRoot,
      ...workspaceRoots,
      ownerRoot,
    ]) {
      if (!root) {
        continue;
      }
      const candidate = await resolveIncludeCandidate(
        root,
        normalizedInclude,
        settings,
        workspaceRoots,
      );
      if (candidate.exists) {
        return candidate;
      }
    }
    return resolveIncludeCandidate(
      settings.virtualRoot ?? ownerRoot,
      normalizedInclude,
      settings,
      workspaceRoots,
    );
  }

  const ownerDirectory = path.dirname(uriToFileName(ownerUri));
  const local = await resolveIncludeCandidate(
    ownerDirectory,
    include.path,
    settings,
    workspaceRoots,
  );
  if (local.exists) {
    return local;
  }
  for (const root of [...(settings.includePaths ?? []), ...(settings.virtualRoots ?? [])]) {
    const candidate = await resolveIncludeCandidate(root, include.path, settings, workspaceRoots);
    if (candidate.exists) {
      return candidate;
    }
  }
  return local;
}

async function resolveIncludeCandidate(
  baseDirectory: string,
  requestedPath: string,
  settings: AspSettings,
  workspaceRoots: string[],
): Promise<IncludeResolution> {
  const fileName = path.resolve(baseDirectory, requestedPath);
  if (settings.windowsPathResolution === false) {
    return { fileName, exists: await pathExists(fileName) };
  }
  if (workerCaseResolution(settings, workspaceRoots) === "fast") {
    const stat = await fs.promises.stat(fileName).catch(() => undefined);
    if (stat) {
      return { fileName, exists: true };
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
    return { fileName, exists: await pathExists(fileName) };
  }
  let current = start;
  for (const segment of relative.split(path.sep).filter((part) => part.length > 0)) {
    const entries = await fs.promises
      .readdir(current, { withFileTypes: true })
      .catch(() => undefined);
    if (!entries) {
      return { fileName, exists: false };
    }
    const exact = entries.find((entry) => entry.name === segment);
    if (exact) {
      current = path.join(current, exact.name);
      continue;
    }
    const lower = segment.toLowerCase();
    const insensitive = entries.filter((entry) => entry.name.toLowerCase() === lower);
    if (insensitive.length !== 1) {
      return { fileName, exists: false };
    }
    current = path.join(current, insensitive[0].name);
  }
  return { fileName: current, exists: true };
}

function workerCaseResolution(settings: AspSettings, workspaceRoots: string[]): "full" | "fast" {
  if (settings.network?.caseResolution === "full" || settings.network?.caseResolution === "fast") {
    return settings.network.caseResolution;
  }
  if (settings.network?.profile === "network") {
    return "fast";
  }
  if (settings.network?.profile === "local") {
    return "full";
  }
  return [
    ...workspaceRoots,
    ...(settings.virtualRoots ?? []),
    ...(settings.includePaths ?? []),
    settings.virtualRoot,
  ].some((candidate) => typeof candidate === "string" && looksLikeNetworkPath(candidate))
    ? "fast"
    : "full";
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

function parseSettingsIdentity(settings: AspSettings): string {
  return JSON.stringify({
    defaultLanguage: settings.defaultLanguage ?? "VBScript",
    resolvedLocale: settings.resolvedLocale ?? "en",
    incremental: settings.incremental?.mode ?? "legacy",
  });
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

function includeRefsFingerprint(includeRefs: AspInclude[]): string {
  return textFingerprint(
    JSON.stringify(includeRefs.map((include) => ({ mode: include.mode, path: include.path }))),
  );
}

function configuredVbscriptGlobals(parsed: AspParsedDocument, settings: AspSettings): VbSymbol[] {
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
        sourceUri: `${parsed.uri}#runtime-global`,
        typeName: type.name,
        type,
      } satisfies VbSymbol,
    ];
  });
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

function equivalentVbSymbol(
  symbols: VbSymbol[],
  target: VbReferencesWorkerTargetSymbol,
): VbSymbol | undefined {
  return symbols.find((symbol) => sameVbSymbolIdentity(symbol, target));
}

function sameVbSymbolIdentity(left: VbSymbol, right: VbReferencesWorkerTargetSymbol): boolean {
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

function fallbackWorkspaceExternalReferences(
  analysis: FullFallbackAnalysis,
  symbol: VbReferencesWorkerTargetSymbol,
  visibilityMemo: IncludeVisibilityMemo,
): VbReference[] {
  if (!isGlobalWorkspaceReferenceFallbackSymbol(symbol)) {
    return [];
  }
  return analysis.summaries.flatMap((summary) =>
    summaryVbReferenceUsages(summary)
      .filter((usage) => usage.key === symbol.name.toLowerCase())
      .flatMap((usage) =>
        usage.ranges
          .filter((range) =>
            isFallbackTargetVisibleAt(analysis, summary.uri, symbol, range, visibilityMemo),
          )
          .map((range) => ({ uri: summary.uri, range })),
      ),
  );
}

function summaryVbReferenceUsages(
  summary: FileAnalysisSummary,
): NonNullable<NonNullable<FileAnalysisSummary["vbscript"]>["externalRefUsages"]> {
  return summary.vbscript?.externalRefUsages ?? [];
}

function isGlobalWorkspaceReferenceFallbackSymbol(symbol: VbReferencesWorkerTargetSymbol): boolean {
  return (
    !symbol.scopeName &&
    !symbol.memberOf &&
    symbol.visibility !== "private" &&
    ["function", "sub", "class", "variable", "constant"].includes(symbol.kind)
  );
}

function createIncludeVisibilityMemo(): IncludeVisibilityMemo {
  return { cache: new Map(), visiting: new Set() };
}

function includeVisibilityMemoKey(
  ownerKey: string,
  targetKey: string,
  range: VbReference["range"],
): string {
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

function isFallbackTargetVisibleAt(
  analysis: FullFallbackAnalysis,
  ownerUri: string,
  target: VbReferencesWorkerTargetSymbol,
  referenceRange: VbReference["range"],
  visibilityMemo: IncludeVisibilityMemo = createIncludeVisibilityMemo(),
): boolean {
  if (!target.sourceUri.startsWith("file://")) {
    return true;
  }
  const ownerKey = fileIdentityKeyFromUri(ownerUri);
  const targetKey = fileIdentityKeyFromUri(target.sourceUri);
  if (targetKey === ownerKey) {
    return true;
  }
  return isFallbackTargetVisibleFromFileAt(
    analysis.includeGraph,
    ownerKey,
    target,
    targetKey,
    referenceRange,
    visibilityMemo,
    new Set([ownerKey]),
  );
}

function isFallbackTargetVisibleFromFileAt(
  graph: FullFallbackIncludeGraph,
  ownerKey: string,
  target: VbReferencesWorkerTargetSymbol,
  targetKey: string,
  referenceRange: VbReference["range"],
  visibilityMemo: IncludeVisibilityMemo,
  visited: Set<string>,
): boolean {
  const key = includeVisibilityMemoKey(ownerKey, targetSymbolKey(target), referenceRange);
  return memoizedIncludeVisibility(visibilityMemo, key, () => {
    if (targetKey === ownerKey) {
      return positionBeforeOrEqual(target.range.start, referenceRange.start);
    }
    if (hasEarlierReachableFallbackInclude(graph, ownerKey, targetKey, referenceRange)) {
      return true;
    }
    for (const parentInclude of graph.parentIncludesByTargetUri.get(ownerKey) ?? []) {
      if (visited.has(parentInclude.ownerUri)) {
        continue;
      }
      visited.add(parentInclude.ownerUri);
      const visible = isFallbackTargetVisibleFromFileAt(
        graph,
        parentInclude.ownerUri,
        target,
        targetKey,
        parentInclude.range,
        visibilityMemo,
        visited,
      );
      visited.delete(parentInclude.ownerUri);
      if (visible) {
        return true;
      }
    }
    return false;
  });
}

function hasEarlierReachableFallbackInclude(
  graph: FullFallbackIncludeGraph,
  ownerUri: string,
  targetUri: string,
  referenceRange: VbReference["range"],
): boolean {
  const includes = graph.directIncludesByOwnerUri.get(ownerUri) ?? [];
  return includes.some(
    (include) =>
      positionBeforeOrEqual(include.range.start, referenceRange.start) &&
      (include.targetUri === targetUri ||
        isFallbackIncludeReachable(graph, include.targetUri, targetUri, new Set([ownerUri]))),
  );
}

function isFallbackIncludeReachable(
  graph: FullFallbackIncludeGraph,
  startUri: string,
  targetUri: string,
  visited: Set<string>,
): boolean {
  if (startUri === targetUri) {
    return true;
  }
  if (visited.has(startUri)) {
    return false;
  }
  visited.add(startUri);
  return (graph.directIncludesByOwnerUri.get(startUri) ?? []).some(
    (include) =>
      include.targetUri === targetUri ||
      isFallbackIncludeReachable(graph, include.targetUri, targetUri, visited),
  );
}

function positionBeforeOrEqual(
  left: VbReference["range"]["start"],
  right: VbReference["range"]["start"],
): boolean {
  return left.line < right.line || (left.line === right.line && left.character <= right.character);
}

function mergeReferences(left: VbReference[], right: VbReference[]): VbReference[] {
  const merged = new Map<string, VbReference>();
  for (const reference of [...left, ...right]) {
    merged.set(JSON.stringify({ uri: reference.uri, range: reference.range }), reference);
  }
  return [...merged.values()];
}

function pushMapItem<K, V>(map: Map<K, V[]>, key: K, value: V): void {
  const existing = map.get(key);
  if (existing) {
    existing.push(value);
  } else {
    map.set(key, [value]);
  }
}

async function mapWithConcurrency<T, U>(
  items: readonly T[],
  concurrency: number,
  callback: (item: T, index: number) => Promise<U>,
): Promise<U[]> {
  const results = Array.from<U>({ length: items.length });
  let next = 0;
  const workerCount = Math.max(1, Math.min(Math.floor(concurrency), items.length));
  const workers = Array.from({ length: workerCount }, async () => {
    while (next < items.length) {
      const index = next;
      next += 1;
      results[index] = await callback(items[index], index);
    }
  });
  await Promise.all(workers);
  return results;
}

function openDocumentMap(
  openDocuments: VbReferencesWorkerOpenDocument[],
): Map<string, VbReferencesWorkerOpenDocument> {
  return new Map(
    openDocuments.map((document) => [fileIdentityKeyFromFileName(document.fileName), document]),
  );
}

function targetFileNameFromSymbol(symbol: VbReferencesWorkerTargetSymbol): string | undefined {
  if (!symbol.sourceUri.startsWith("file://")) {
    return undefined;
  }
  return normalizeFileName(uriToFileName(symbol.sourceUri));
}

function workspaceRootFromUri(uri: string, workspaceRoots: string[]): string | undefined {
  const fileName = normalizeFileName(uriToFileName(uri));
  return workspaceRoots
    .map(normalizeFileName)
    .filter((root) => {
      const fileKey = fileIdentityKeyFromFileName(fileName);
      const rootKey = fileIdentityKeyFromFileName(root);
      return fileKey === rootKey || fileKey.startsWith(`${rootKey}/`);
    })
    .sort((left, right) => right.length - left.length)[0];
}

function pathExists(fileName: string): Promise<boolean> {
  return fs.promises.stat(fileName).then(
    (stat) => Boolean(stat),
    () => false,
  );
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

function uriToFileName(uri: string): string {
  return uri.startsWith("file://") ? fileURLToPath(uri) : uri;
}

function pathToFileUri(fileName: string): string {
  return pathToFileURL(fileName).toString();
}

function normalizeFileName(fileName: string): string {
  return path.resolve(fileName);
}
