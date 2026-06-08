import fs from "node:fs";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { parentPort } from "node:worker_threads";
import { analyse, detect } from "chardet";
import {
  buildVbTypeEnvironment,
  extractAspIncludeRefs,
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
import type {
  VbReferencesWorkerOpenDocument,
  VbReferencesWorkerRequest,
  VbReferencesWorkerResponse,
  VbReferencesWorkerTargetSymbol,
} from "./vb-references-protocol";

if (!parentPort) {
  throw new Error("VBScript references worker requires a parent port.");
}

parentPort.on("message", (request: VbReferencesWorkerRequest) => {
  void handleRequest(request);
});

async function handleRequest(request: VbReferencesWorkerRequest): Promise<void> {
  try {
    const result = await runWorkspaceReferenceAnalysis(request);
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

async function runWorkspaceReferenceAnalysis(
  request: VbReferencesWorkerRequest,
): Promise<VbReferencesWorkerResponse> {
  await testDelayFromEnv();
  const openDocuments = openDocumentMap(request.openDocuments);
  const closure = await collectLightweightIncludeClosure(request, openDocuments);
  const targets = request.targets?.length ? request.targets : [request.target];
  const targetFileNames = targets
    .map(targetFileNameFromSymbol)
    .filter((fileName): fileName is string => Boolean(fileName));
  if (
    closure.complete &&
    targetFileNames.length > 0 &&
    !targetFileNames.some((fileName) => closure.files.has(fileName))
  ) {
    return {
      id: request.id,
      candidate: request.candidate,
      references: [],
      referencesByTarget: emptyReferenceMap(targets),
      fallbackReasons: [],
      scannedFiles: closure.files.size,
      cacheHits: closure.openHits,
    };
  }

  const referencesByTarget = await fullFallbackReferencesForTargets(
    request,
    openDocuments,
    targets,
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
    cacheHits: closure.openHits,
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

async function collectLightweightIncludeClosure(
  request: VbReferencesWorkerRequest,
  openDocuments: Map<string, VbReferencesWorkerOpenDocument>,
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
        if (files.has(item.fileName)) {
          return { next: [] };
        }
        files.add(item.fileName);
        if (files.size > request.limits.maxDocuments) {
          return { next: [], reason: "document-limit" };
        }
        const text = await readWorkspaceText(item.fileName, request.settings, openDocuments);
        if (!text) {
          return { next: [], reason: "read-missing" };
        }
        if (text.openDocument) {
          openHits += 1;
        }
        textLength += text.text.length;
        if (textLength > request.limits.maxTextLength) {
          return { next: [], reason: "text-limit" };
        }
        const includeRefs = extractAspIncludeRefs(text.text);
        const next = await mapWithConcurrency(
          includeRefs,
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
    frontier = nextFrontier.filter((item) => !files.has(item.fileName));
  }

  return { files, complete, reasons: [...new Set(reasons)], openHits };
}

async function fullFallbackReferencesForTargets(
  request: VbReferencesWorkerRequest,
  openDocuments: Map<string, VbReferencesWorkerOpenDocument>,
  targets: VbReferencesWorkerTargetSymbol[],
): Promise<Record<string, VbReference[]>> {
  const analysis = await cachedFullFallbackAnalysis(request, openDocuments);
  const referencesByTarget = emptyReferenceMap(targets);
  if (!analysis) {
    return referencesByTarget;
  }
  const equivalentTargets: VbSymbol[] = [];
  const equivalentKeys = new Map<string, string>();
  for (const target of targets) {
    const targetKey = targetSymbolKey(target);
    const equivalent = equivalentVbSymbol(analysis.symbols, target);
    if (equivalent) {
      equivalentTargets.push(equivalent);
      equivalentKeys.set(vbscriptReferenceSymbolKey(equivalent), targetKey);
      continue;
    }
    referencesByTarget[targetKey] = fallbackWorkspaceExternalReferences(analysis.summaries, target);
  }
  const batch = getVbscriptReferencesForSymbols(
    equivalentTargets,
    analysis.context,
    request.options,
  );
  for (const [equivalentKey, references] of batch) {
    const targetKey = equivalentKeys.get(equivalentKey);
    if (targetKey) {
      referencesByTarget[targetKey] = references;
    }
  }
  return referencesByTarget;
}

interface FullFallbackAnalysis {
  context: VbProjectContext;
  summaries: FileAnalysisSummary[];
  symbols: VbSymbol[];
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
): Promise<FullFallbackAnalysis | undefined> {
  const key = fullFallbackAnalysisCacheKey(request);
  const cached = fullFallbackAnalysisCache.get(key);
  if (cached) {
    cached.lastUsed = Date.now();
    return cached.analysis;
  }
  const documents = await collectFullDocuments(request, openDocuments);
  if (documents.length === 0) {
    return undefined;
  }
  const contextSettings = vbProjectContextSettings(request.settings);
  const summaries = await Promise.all(
    documents.map((document) => summarizeAspFileAnalysisAsync(document, contextSettings)),
  );
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
    externalRefUsages: summaries.flatMap((summary) => summary.vbscript?.externalRefUsages ?? []),
    ...contextSettings,
  };
  const analysis = { context, summaries, symbols };
  fullFallbackAnalysisCache.set(key, { key, analysis, lastUsed: Date.now() });
  pruneFullFallbackAnalysisCache();
  return analysis;
}

function fullFallbackAnalysisCacheKey(request: VbReferencesWorkerRequest): string {
  return JSON.stringify({
    candidate: request.candidate,
    settings: request.settings,
    workspaceRoots: request.workspaceRoots,
    openDocuments: request.openDocuments.map((document) => ({
      uri: document.uri,
      fileName: document.fileName,
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
  return [
    symbol.sourceUri,
    symbol.kind,
    symbol.memberOf ?? "",
    symbol.name.toLowerCase(),
    symbol.range.start.line,
    symbol.range.start.character,
  ].join("|");
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
): Promise<AspParsedDocument[]> {
  const documents: AspParsedDocument[] = [];
  const visited = new Set<string>();
  let textLength = 0;

  const visit = async (uri: string, fileName: string, depth: number): Promise<void> => {
    const normalized = normalizeFileName(fileName);
    if (
      depth > request.limits.maxDepth ||
      visited.has(normalized) ||
      documents.length >= request.limits.maxDocuments ||
      textLength > request.limits.maxTextLength
    ) {
      return;
    }
    const text = await readWorkspaceText(normalized, request.settings, openDocuments);
    if (!text) {
      return;
    }
    visited.add(normalized);
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
  const openDocument = openDocuments.get(normalized);
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
      const candidate = await resolveIncludeCandidate(root, normalizedInclude, settings);
      if (candidate.exists) {
        return candidate;
      }
    }
    return resolveIncludeCandidate(settings.virtualRoot ?? ownerRoot, normalizedInclude, settings);
  }

  const ownerDirectory = path.dirname(uriToFileName(ownerUri));
  const local = await resolveIncludeCandidate(ownerDirectory, include.path, settings);
  if (local.exists) {
    return local;
  }
  for (const root of [...(settings.includePaths ?? []), ...(settings.virtualRoots ?? [])]) {
    const candidate = await resolveIncludeCandidate(root, include.path, settings);
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
): Promise<IncludeResolution> {
  const fileName = path.resolve(baseDirectory, requestedPath);
  if (settings.windowsPathResolution === false) {
    return { fileName, exists: await pathExists(fileName) };
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

function fallbackWorkspaceExternalReferences(
  summaries: FileAnalysisSummary[],
  symbol: VbReferencesWorkerTargetSymbol,
): VbReference[] {
  if (!isGlobalWorkspaceReferenceFallbackSymbol(symbol)) {
    return [];
  }
  return summaries.flatMap((summary) =>
    (summary.vbscript?.externalRefUsages ?? [])
      .filter((usage) => usage.key === symbol.name.toLowerCase())
      .flatMap((usage) => usage.ranges.map((range) => ({ uri: summary.uri, range }))),
  );
}

function isGlobalWorkspaceReferenceFallbackSymbol(symbol: VbReferencesWorkerTargetSymbol): boolean {
  return (
    !symbol.scopeName &&
    !symbol.memberOf &&
    symbol.visibility !== "private" &&
    ["function", "sub", "class"].includes(symbol.kind)
  );
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
  return new Map(openDocuments.map((document) => [normalizeFileName(document.fileName), document]));
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
    .filter((root) => fileName === root || fileName.startsWith(`${root}${path.sep}`))
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
