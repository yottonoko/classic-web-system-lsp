import { DiagnosticSeverity } from "vscode-languageserver-types";
import type {
  AspCstNode,
  AspDirective,
  AspEditImpact,
  AspInclude,
  AspIncrementalChange,
  AspIncrementalUpdateResult,
  AspParsedDocument,
  AspRegion,
  AspServerObject,
  AspSettings,
  AspToken,
  VbCstNode,
  VbToken,
} from "./types";
import { offsetAt, rangeFromOffsets } from "./position";
import {
  scanHtmlAndAsp,
  scanHtmlAndAspRange,
  normalizeScriptLanguage,
  parseAttributes,
} from "./asp-scanner";
import {
  applyIncrementalChanges,
  changeHull,
  changesOverlap,
  flattenString,
  normalizeIncrementalChange,
  normalizeIncrementalChanges,
  rangeOverlaps,
  rangeOverlapsOrTouches,
  resolveAspIncrementalMode,
  shiftOffsetAfterChange,
  type DamageSpan,
  type IncrementalReparseResult,
  type NormalizedIncrementalChange,
} from "./incremental";
import { parseVbscriptCst } from "./vbscript-cst";
export { normalizeScriptLanguage, parseAttributes } from "./asp-scanner";
export { flattenString } from "./incremental";
export type { DamageSpan, IncrementalReparseResult } from "./incremental";

const asyncParseCacheMaxEntries = 64;
const asyncParseCache = new Map<string, AspParsedDocument>();
const asyncParseCacheEntryBytes = new Map<string, number>();
let asyncParseCacheEstimatedBytes = 0;
const textFingerprintCacheMaxEntries = 256;
const textFingerprintCache = new Map<string, string>();
const textFingerprintCacheEntryBytes = new Map<string, number>();
let textFingerprintCacheEstimatedBytes = 0;
const parseSettingsStringCache = new WeakMap<AspSettings, string>();

export interface AspParserMemoryCacheRegistration {
  name: string;
  priority: number;
  estimateBytes(): number;
  evict(targetBytes: number): number;
  entryCount?(): number;
}

export function parseAspDocument(
  uri: string,
  text: string,
  settings: AspSettings = {},
): AspParsedDocument {
  return parseAspDocumentTypeScript(uri, flattenString(text), settings);
}

export function parseAspDocumentSkeleton(
  uri: string,
  text: string,
  settings: AspSettings = {},
): AspParsedDocument {
  return parseAspDocumentSkeletonTypeScript(uri, flattenString(text), settings);
}

/**
 * Parses an ASP document for async/server workflows.
 */
export async function parseAspDocumentAsync(
  uri: string,
  text: string,
  settings: AspSettings = {},
): Promise<AspParsedDocument> {
  const flattenedText = flattenString(text);
  const cacheKey = parseCacheKey(uri, flattenedText, settings);
  const cached = asyncParseCache.get(cacheKey);
  if (cached) {
    touchAsyncParseCacheEntry(cacheKey, cached);
    return cached;
  }
  const parsed = parseAspDocumentTypeScript(uri, flattenedText, settings);
  setAsyncParseCache(cacheKey, parsed);
  return parsed;
}

export async function parseAspDocumentSkeletonAsync(
  uri: string,
  text: string,
  settings: AspSettings = {},
): Promise<AspParsedDocument> {
  return parseAspDocumentSkeletonTypeScript(uri, flattenString(text), settings);
}

export function registerParserMemoryCaches(
  register: (cache: AspParserMemoryCacheRegistration) => void,
): void {
  register({
    name: "core.asyncParse",
    priority: 45,
    estimateBytes: () => asyncParseCacheEstimatedBytes,
    evict: evictAsyncParseCache,
    entryCount: () => asyncParseCache.size,
  });
  register({
    name: "core.textFingerprint",
    priority: 5,
    estimateBytes: () => textFingerprintCacheEstimatedBytes,
    evict: evictTextFingerprintCache,
    entryCount: () => textFingerprintCache.size,
  });
}

const hydratedVbscriptDocuments = new WeakSet<AspParsedDocument>();

export function needsVbscriptCstHydration(parsed: AspParsedDocument): boolean {
  return (
    parsed.regions.some((region) => region.language === "vbscript") && !cstHasVbscript(parsed.cst)
  );
}

/**
 * Attaches VBScript CST subtrees to an async/skeleton parse.
 *
 * Consumers that directly walk `parsed.cst` for VBScript references, tokens, or declarations
 * should await this first. Full sync parses and documents without VBScript are returned as-is.
 */
export async function hydrateVbscriptCst(
  parsed: AspParsedDocument,
  _settings: AspSettings = {},
): Promise<AspParsedDocument> {
  if (hydratedVbscriptDocuments.has(parsed) || !needsVbscriptCstHydration(parsed)) {
    return parsed;
  }
  attachVbscriptFromTypeScriptParser(parsed.cst, parsed.text);
  hydratedVbscriptDocuments.add(parsed);
  return parsed;
}

function cstHasVbscript(node: AspCstNode): boolean {
  if (node.vbscript) {
    return true;
  }
  return (node.children ?? []).some((child) => cstHasVbscript(child));
}

function attachVbscriptFromTypeScriptParser(node: AspCstNode, sourceText: string): void {
  if (node.language === "vbscript" && !node.vbscript) {
    node.vbscript = parseVbscriptCst(
      sourceText.slice(node.contentStart, node.contentEnd),
      sourceText,
      node.contentStart,
    );
  }
  for (const child of node.children ?? []) {
    attachVbscriptFromTypeScriptParser(child, sourceText);
  }
}

function parseAspDocumentTypeScript(
  uri: string,
  text: string,
  settings: AspSettings = {},
): AspParsedDocument {
  const cst = parseAspCstTypeScript(uri, text, settings);
  const diagnostics =
    cst.errors?.map((error) => ({
      severity: DiagnosticSeverity.Error,
      range: rangeFromOffsets(text, error.start, error.end),
      message: error.message,
      source: "asp-lsp",
    })) ?? [];
  const inlineRegions = cst.children
    .filter((node) => node.regionKind)
    .map(nodeToRegion)
    .filter((region): region is AspRegion => region !== undefined);
  const directives = cst.children
    .map((node) => node.directive)
    .filter((directive): directive is AspDirective => directive !== undefined);
  const includes = cst.children
    .map((node) => node.include)
    .filter((include): include is AspInclude => include !== undefined);
  const serverObjects = cst.serverObjects ?? [];
  const directiveLanguage = directives
    .map((directive) => directive.attributes.language ?? directive.attributes.LANGUAGE)
    .find((value): value is string => typeof value === "string");
  const defaultLanguage = normalizeScriptLanguage(
    directiveLanguage ?? settings.defaultLanguage ?? "VBScript",
  );
  const regions = buildRegions(text, inlineRegions, defaultLanguage);
  return {
    uri,
    text,
    cst,
    regions,
    directives,
    includes,
    serverObjects,
    defaultLanguage,
    diagnostics,
  };
}

function parseAspDocumentSkeletonTypeScript(
  uri: string,
  text: string,
  settings: AspSettings = {},
): AspParsedDocument {
  const diagnostics: AspParsedDocument["diagnostics"] = [];
  const scan = scanHtmlAndAsp(text, diagnostics, settings);
  const { inlineRegions, tagRegions, includes, serverObjects } = scan;
  const directives = inlineRegions
    .filter((region) => region.kind === "asp-directive")
    .map((region): AspDirective => {
      const raw = text.slice(region.contentStart, region.contentEnd).trim();
      const normalized = raw.startsWith("@") ? raw.slice(1).trim() : raw;
      const [first = "Page", ...rest] = normalized.split(/\s+/);
      const hasExplicitName = !first.includes("=");
      const name = hasExplicitName ? first : "Page";
      const attributeText = hasExplicitName ? rest.join(" ") : normalized;
      return {
        offset: region.start,
        range: rangeFromOffsets(text, region.start, region.end),
        name,
        attributes: parseAttributes(attributeText),
      };
    });
  const directiveLanguage = directives
    .map((directive) => directive.attributes.language ?? directive.attributes.LANGUAGE)
    .find((value): value is string => typeof value === "string");
  const defaultLanguage = normalizeScriptLanguage(
    directiveLanguage ?? settings.defaultLanguage ?? "VBScript",
  );
  const scriptRegions = tagRegions.map((region): AspRegion => {
    if (region.kind !== "server-script") {
      return region;
    }
    return {
      ...region,
      language:
        normalizeScriptLanguage(
          String(region.attributes?.language ?? defaultLanguage),
        ).toLowerCase() === "jscript"
          ? "jscript"
          : "vbscript",
    };
  });
  const regions = buildRegions(text, [...inlineRegions, ...scriptRegions], defaultLanguage);
  const nodes: AspCstNode[] = [
    ...regions.map(skeletonRegionToNode),
    ...includes.map((include) => skeletonIncludeToNode(text, include)),
  ].sort(
    (left, right) => left.start - right.start || left.end - left.start - (right.end - right.start),
  );
  for (const directive of directives) {
    const node = nodes.find(
      (item) => item.start === directive.offset && item.kind === "AspDirective",
    );
    if (node) {
      node.directive = directive;
      node.attributes = directive.attributes;
    }
  }
  const topLevelRegions = nodes
    .map(nodeToRegion)
    .filter((region): region is AspRegion => region !== undefined);
  const cst: AspCstNode = {
    kind: "Document",
    start: 0,
    end: text.length,
    contentStart: 0,
    contentEnd: text.length,
    tokens: [],
    children: nodes,
    serverObjects,
    errors: diagnostics.map((diagnostic) => ({
      message: diagnostic.message,
      start: offsetFromRange(text, diagnostic.range.start),
      end: offsetFromRange(text, diagnostic.range.end),
    })),
  };
  return {
    uri,
    text,
    cst,
    regions: topLevelRegions,
    directives,
    includes,
    serverObjects,
    defaultLanguage,
    diagnostics,
  };
}

export function updateAspParsedDocument(
  previous: AspParsedDocument,
  changes: readonly AspIncrementalChange[],
  settings: AspSettings = {},
): AspIncrementalUpdateResult {
  const attempt = tryUpdateAspParsedDocumentIncremental(previous, changes, settings, "full");
  if (attempt.status === "updated") {
    return attempt.result;
  }
  return {
    parsed: parseAspDocument(previous.uri, attempt.nextText, settings),
    impact: editImpact("full", attempt.reason, previous.text, attempt.nextText, attempt.change),
  };
}

export async function updateAspParsedDocumentSkeletonAsync(
  previous: AspParsedDocument,
  changes: readonly AspIncrementalChange[],
  settings: AspSettings = {},
): Promise<AspIncrementalUpdateResult> {
  const attempt = tryUpdateAspParsedDocumentIncremental(previous, changes, settings, "skeleton");
  if (attempt.status === "updated") {
    return attempt.result;
  }
  return {
    parsed: await parseAspDocumentSkeletonAsync(previous.uri, attempt.nextText, settings),
    impact: editImpact("full", attempt.reason, previous.text, attempt.nextText, attempt.change),
  };
}

type ParsedDocumentBuildMode = "full" | "skeleton";

interface IncrementalUpdateSuccess {
  status: "updated";
  result: AspIncrementalUpdateResult;
}

interface IncrementalUpdateFallback {
  status: "fallback";
  reason: string;
  change?: AspIncrementalChange;
  nextText: string;
}

function tryUpdateAspParsedDocumentIncremental(
  previous: AspParsedDocument,
  changes: readonly AspIncrementalChange[],
  settings: AspSettings,
  buildMode: ParsedDocumentBuildMode,
): IncrementalUpdateSuccess | IncrementalUpdateFallback {
  const mode = resolveAspIncrementalMode(settings);
  if (mode === "off") {
    return {
      status: "fallback",
      reason: "incremental disabled",
      change: changes[0],
      nextText: applyIncrementalChanges(previous.text, changes),
    };
  }
  if (mode === "full") {
    return tryUpdateAspParsedDocumentFull(previous, changes, settings, buildMode);
  }
  return tryUpdateAspParsedDocumentLegacy(previous, changes, buildMode);
}

function tryUpdateAspParsedDocumentLegacy(
  previous: AspParsedDocument,
  changes: readonly AspIncrementalChange[],
  buildMode: ParsedDocumentBuildMode,
): IncrementalUpdateSuccess | IncrementalUpdateFallback {
  const fallback = (
    reason: string,
    change = changes[0],
    nextText = applyIncrementalChanges(previous.text, changes),
  ): IncrementalUpdateFallback => ({
    status: "fallback",
    reason,
    change,
    nextText,
  });
  if (changes.length !== 1) {
    return fallback("multiple changes");
  }
  const change = normalizeIncrementalChange(previous.text, changes[0]);
  if (!change) {
    return fallback("invalid change range");
  }
  const nextText =
    previous.text.slice(0, change.startOffset) +
    change.text +
    previous.text.slice(change.endOffset);
  if (change.text.length > 1024 || change.endOffset - change.startOffset > 1024) {
    return fallback("large edit", change, nextText);
  }
  if (boundarySensitiveText(previous.text.slice(change.startOffset, change.endOffset))) {
    return fallback("boundary text deleted", change, nextText);
  }
  if (boundarySensitiveText(change.text)) {
    return fallback("boundary text inserted", change, nextText);
  }
  if (changeOverlapsInclude(previous, change.startOffset, change.endOffset)) {
    return fallback("include directive edit", change, nextText);
  }
  if (changeOverlapsServerObject(previous, change.startOffset, change.endOffset)) {
    return fallback("server object tag edit", change, nextText);
  }
  if (changeOverlapsDirective(previous, change.startOffset, change.endOffset)) {
    return fallback("ASP directive edit", change, nextText);
  }
  if (changeTouchesEmbeddedContentBoundary(previous, change.startOffset, change.endOffset)) {
    return fallback("region boundary edit", change, nextText);
  }
  const region = incrementalRegionForChange(previous, change.startOffset, change.endOffset);
  if (!region) {
    return fallback("edit crosses language boundary", change, nextText);
  }
  if (region.kind === "asp-directive") {
    return fallback("ASP directive region edit", change, nextText);
  }
  if (changeCreatesStructuralMarker(previous.text, nextText, change, region)) {
    return fallback("structural marker edit", change, nextText);
  }
  if (changeTouchesRegionBoundary(region, change.startOffset, change.endOffset)) {
    return fallback("region boundary edit", change, nextText);
  }
  const shiftedRegions = shiftRegionsForChange(
    previous.regions,
    region,
    change.startOffset,
    change.endOffset,
    change.text.length - (change.endOffset - change.startOffset),
  );
  const shiftedDirectives = previous.directives.map((directive) =>
    shiftDirectiveAfterChange(directive, previous.text, nextText, change),
  );
  const shiftedIncludes = previous.includes.map((include) =>
    shiftIncludeAfterChange(include, previous.text, nextText, change),
  );
  const shiftedServerObjects = previous.serverObjects.map((serverObject) =>
    shiftServerObjectAfterChange(serverObject, previous.text, nextText, change),
  );
  const shiftedDiagnostics = previous.diagnostics.map((diagnostic) => ({
    ...diagnostic,
    range: shiftAspRangeAfterChange(diagnostic.range, previous.text, nextText, change),
  }));
  return {
    status: "updated",
    result: {
      parsed: buildParsedDocument(
        previous.uri,
        nextText,
        shiftedRegions,
        shiftedDirectives,
        shiftedIncludes,
        shiftedServerObjects,
        previous.defaultLanguage,
        shiftedDiagnostics,
        buildMode,
      ),
      impact: editImpact(
        "incremental",
        "safe content edit",
        previous.text,
        nextText,
        change,
        region,
      ),
    },
  };
}

function tryUpdateAspParsedDocumentFull(
  previous: AspParsedDocument,
  changes: readonly AspIncrementalChange[],
  settings: AspSettings,
  buildMode: ParsedDocumentBuildMode,
): IncrementalUpdateSuccess | IncrementalUpdateFallback {
  const fallback = (
    reason: string,
    change = changes[0],
    nextText = applyIncrementalChanges(previous.text, changes),
  ): IncrementalUpdateFallback => ({
    status: "fallback",
    reason,
    change,
    nextText,
  });
  if (changes.length === 0) {
    return fallback("no changes");
  }
  const normalizedChanges = normalizeIncrementalChanges(previous.text, changes);
  if (!normalizedChanges) {
    return fallback("invalid change range");
  }
  if (changesOverlap(normalizedChanges)) {
    return fallback("overlapping changes");
  }
  const nextText = applyIncrementalChanges(previous.text, normalizedChanges);
  const changeSpan = changeHull(normalizedChanges);
  if (changeSpan.start === 0 && changeSpan.end === previous.text.length) {
    return fallback("full document replacement", normalizedChanges[0], nextText);
  }
  if (changesCanCloseEarlierStructure(previous.text, normalizedChanges)) {
    return fallback("structural close resync", normalizedChanges[0], nextText);
  }
  const oldDamage = expandDamageSpan(previous, changeSpan);
  if (damageOverlapsDirective(previous, oldDamage)) {
    return fallback("ASP directive edit", normalizedChanges[0], nextText);
  }
  const delta = nextText.length - previous.text.length;
  const nextDamage: DamageSpan = {
    start: oldDamage.start,
    end: Math.max(oldDamage.start, oldDamage.end + delta),
  };
  if (nextDamage.end > nextText.length) {
    return fallback("invalid damage span", normalizedChanges[0], nextText);
  }
  const scan = scanHtmlAndAspRange(nextText, nextDamage.start, nextDamage.end, settings);
  if (!scan.complete) {
    return fallback("incremental resync failed", normalizedChanges[0], nextText);
  }
  if (scan.inlineRegions.some((region) => region.kind === "asp-directive")) {
    return fallback("ASP directive edit", normalizedChanges[0], nextText);
  }
  const shiftedEmbeddedRegions = previous.cst.children
    .map(cstNodeToIncrementalRegion)
    .filter((region): region is AspRegion => region !== undefined)
    .filter((region) => region.kind !== "html")
    .filter((region) => !rangeOverlaps(region.start, region.end, oldDamage.start, oldDamage.end))
    .map((region) => shiftRegionAfterDamage(region, oldDamage, delta));
  const scannedTagRegions = scan.tagRegions.map((region): AspRegion => {
    if (region.kind !== "server-script") {
      return region;
    }
    return {
      ...region,
      language:
        normalizeScriptLanguage(
          String(region.attributes?.language ?? previous.defaultLanguage),
        ).toLowerCase() === "jscript"
          ? "jscript"
          : "vbscript",
    };
  });
  const regions = buildRegions(
    nextText,
    [...shiftedEmbeddedRegions, ...scan.inlineRegions, ...scannedTagRegions],
    previous.defaultLanguage,
  );
  const directives = previous.directives
    .filter(
      (directive) =>
        !rangeOverlaps(
          metadataStart(directive),
          metadataEnd(previous.text, directive),
          oldDamage.start,
          oldDamage.end,
        ),
    )
    .map((directive) =>
      shiftDirectiveAfterDamage(directive, previous.text, nextText, oldDamage, delta),
    )
    .sort((left, right) => left.offset - right.offset);
  const includes = [
    ...previous.includes
      .filter(
        (include) =>
          !rangeOverlaps(
            include.offset,
            metadataEnd(previous.text, include),
            oldDamage.start,
            oldDamage.end,
          ),
      )
      .map((include) =>
        shiftIncludeAfterDamage(include, previous.text, nextText, oldDamage, delta),
      ),
    ...scan.includes,
  ].sort((left, right) => left.offset - right.offset);
  const serverObjects = [
    ...previous.serverObjects
      .filter(
        (serverObject) =>
          !rangeOverlaps(
            serverObject.offset,
            metadataEnd(previous.text, serverObject),
            oldDamage.start,
            oldDamage.end,
          ),
      )
      .map((serverObject) =>
        shiftServerObjectAfterDamage(serverObject, previous.text, nextText, oldDamage, delta),
      ),
    ...scan.serverObjects,
  ].sort((left, right) => left.offset - right.offset);
  const diagnostics = [
    ...previous.diagnostics
      .filter((diagnostic) => {
        const start = offsetFromRange(previous.text, diagnostic.range.start);
        const end = offsetFromRange(previous.text, diagnostic.range.end);
        return !rangeOverlaps(start, end, oldDamage.start, oldDamage.end);
      })
      .map((diagnostic) =>
        shiftDiagnosticAfterDamage(diagnostic, previous.text, nextText, oldDamage, delta),
      ),
    ...scan.diagnostics,
  ];
  const reused = buildParsedDocumentWithReuse(
    previous.uri,
    nextText,
    regions,
    directives,
    includes,
    serverObjects,
    previous.defaultLanguage,
    diagnostics,
    buildMode,
    previous,
    oldDamage,
    nextDamage,
    delta,
  );
  const invalidReason = validateIncrementalParsedDocument(reused.parsed);
  if (invalidReason) {
    return fallback(invalidReason, normalizedChanges[0], nextText);
  }
  const impact = editImpactForDamage(
    "incremental",
    `range rescan; reused ${reused.reusedRegionCount}/${reused.reuseCandidateCount} nodes`,
    previous.text,
    nextText,
    oldDamage,
    nextDamage,
    damageLanguage(previous, oldDamage),
  );
  const result: IncrementalReparseResult = {
    parsed: reused.parsed,
    impact,
    reusedRegionCount: reused.reusedRegionCount,
    rescannedSpan: nextDamage,
  };
  return {
    status: "updated",
    result,
  };
}

interface ParsedDocumentReuseBuildResult {
  parsed: AspParsedDocument;
  reusedRegionCount: number;
  reuseCandidateCount: number;
}

function expandDamageSpan(parsed: AspParsedDocument, initial: DamageSpan): DamageSpan {
  const spans = [
    ...parsed.regions.map((region) => ({ start: region.start, end: region.end })),
    ...parsed.includes.map((include) => ({
      start: include.offset,
      end: offsetFromRange(parsed.text, include.range.end),
    })),
    ...parsed.serverObjects.map((serverObject) => ({
      start: serverObject.offset,
      end: offsetFromRange(parsed.text, serverObject.range.end),
    })),
  ];
  let damage = { ...initial };
  if (initial.start === initial.end) {
    for (const span of spans) {
      if (initial.start >= span.start && initial.start <= span.end) {
        damage = {
          start: Math.min(damage.start, span.start),
          end: Math.max(damage.end, span.end),
        };
      }
    }
  }
  let changed = true;
  while (changed) {
    changed = false;
    for (const span of spans) {
      if (!rangeOverlaps(damage.start, damage.end, span.start, span.end)) {
        continue;
      }
      const nextStart = Math.min(damage.start, span.start);
      const nextEnd = Math.max(damage.end, span.end);
      if (nextStart !== damage.start || nextEnd !== damage.end) {
        damage = { start: nextStart, end: nextEnd };
        changed = true;
      }
    }
  }
  return damage;
}

function damageOverlapsDirective(parsed: AspParsedDocument, damage: DamageSpan): boolean {
  return parsed.directives.some((directive) =>
    rangeOverlaps(
      directive.offset,
      offsetFromRange(parsed.text, directive.range.end),
      damage.start,
      damage.end,
    ),
  );
}

function cstNodeToIncrementalRegion(node: AspCstNode): AspRegion | undefined {
  if (!node.regionKind || !node.language) {
    return undefined;
  }
  const region: AspRegion = {
    kind: node.regionKind,
    language: node.language,
    start: node.start,
    end: node.end,
    contentStart: node.contentStart,
    contentEnd: node.contentEnd,
  };
  if (Object.hasOwn(node, "attributes")) {
    region.attributes = node.attributes;
  }
  return region;
}

function changesCanCloseEarlierStructure(
  previousText: string,
  changes: readonly NormalizedIncrementalChange[],
): boolean {
  for (const change of changes) {
    const deletedText = previousText.slice(change.startOffset, change.endOffset);
    const changedText = `${deletedText}${change.text}`;
    if (
      /<\s*\/\s*script\b/i.test(changedText) &&
      hasUnclosedElementBefore(previousText, "script", change.startOffset)
    ) {
      return true;
    }
    if (
      /<\s*\/\s*style\b/i.test(changedText) &&
      hasUnclosedElementBefore(previousText, "style", change.startOffset)
    ) {
      return true;
    }
    if (/%>/.test(changedText) && hasUnclosedAspBefore(previousText, change.startOffset)) {
      return true;
    }
    if (/-->/.test(changedText) && hasUnclosedCommentBefore(previousText, change.startOffset)) {
      return true;
    }
  }
  return false;
}

function hasUnclosedElementBefore(
  text: string,
  tagName: "script" | "style",
  offset: number,
): boolean {
  const pattern = new RegExp(`<\\s*(/?)\\s*${tagName}\\b`, "gi");
  let open = false;
  let match: RegExpExecArray | null;
  while ((match = pattern.exec(text)) !== null && match.index < offset) {
    open = match[1] !== "/";
  }
  return open;
}

function hasUnclosedAspBefore(text: string, offset: number): boolean {
  const open = text.lastIndexOf("<%", offset);
  const close = text.lastIndexOf("%>", offset);
  return open !== -1 && open > close;
}

function hasUnclosedCommentBefore(text: string, offset: number): boolean {
  const open = text.lastIndexOf("<!--", offset);
  const close = text.lastIndexOf("-->", offset);
  return open !== -1 && open > close;
}

function metadataStart(item: AspDirective | AspInclude | AspServerObject): number {
  return item.offset;
}

function metadataEnd(text: string, item: AspDirective | AspInclude | AspServerObject): number {
  return offsetFromRange(text, item.range.end);
}

function editImpactForDamage(
  kind: AspEditImpact["kind"],
  reason: string,
  previousText: string,
  nextText: string,
  oldDamage: DamageSpan,
  nextDamage: DamageSpan,
  language?: AspEditImpact["language"],
): AspEditImpact {
  return {
    kind,
    reason,
    startOffset: oldDamage.start,
    endOffset: oldDamage.end,
    insertedLength: nextDamage.end - nextDamage.start,
    deletedLength: oldDamage.end - oldDamage.start,
    delta: nextText.length - previousText.length,
    language,
  };
}

function damageLanguage(
  parsed: AspParsedDocument,
  damage: DamageSpan,
): AspEditImpact["language"] | undefined {
  const languages = new Set(
    parsed.regions
      .filter((region) => rangeOverlaps(region.start, region.end, damage.start, damage.end))
      .map((region) => region.language),
  );
  if (languages.size === 0) {
    return undefined;
  }
  if (languages.size === 1) {
    return [...languages][0];
  }
  return "mixed";
}

function shiftRegionAfterDamage(region: AspRegion, damage: DamageSpan, delta: number): AspRegion {
  if (region.end <= damage.start) {
    return region;
  }
  if (region.start >= damage.end) {
    return shiftRegion(region, delta);
  }
  return {
    ...region,
    start: shiftOffsetAfterChange(region.start, damage.start, damage.end, delta),
    end: shiftOffsetAfterChange(region.end, damage.start, damage.end, delta),
    contentStart: shiftOffsetAfterChange(region.contentStart, damage.start, damage.end, delta),
    contentEnd: shiftOffsetAfterChange(region.contentEnd, damage.start, damage.end, delta),
  };
}

function shiftRangeAfterDamage(
  range: { start: { line: number; character: number }; end: { line: number; character: number } },
  previousText: string,
  nextText: string,
  damage: DamageSpan,
  delta: number,
) {
  const start = shiftOffsetAfterChange(
    offsetAt(previousText, range.start),
    damage.start,
    damage.end,
    delta,
  );
  const end = shiftOffsetAfterChange(
    offsetAt(previousText, range.end),
    damage.start,
    damage.end,
    delta,
  );
  return rangeFromOffsets(nextText, start, Math.max(start, end));
}

function shiftDirectiveAfterDamage(
  directive: AspDirective,
  previousText: string,
  nextText: string,
  damage: DamageSpan,
  delta: number,
): AspDirective {
  return {
    ...directive,
    offset: shiftOffsetAfterChange(directive.offset, damage.start, damage.end, delta),
    range: shiftRangeAfterDamage(directive.range, previousText, nextText, damage, delta),
  };
}

function shiftIncludeAfterDamage(
  include: AspInclude,
  previousText: string,
  nextText: string,
  damage: DamageSpan,
  delta: number,
): AspInclude {
  return {
    ...include,
    offset: shiftOffsetAfterChange(include.offset, damage.start, damage.end, delta),
    range: shiftRangeAfterDamage(include.range, previousText, nextText, damage, delta),
    directiveRange: shiftRangeAfterDamage(
      include.directiveRange,
      previousText,
      nextText,
      damage,
      delta,
    ),
    modeRange: shiftRangeAfterDamage(include.modeRange, previousText, nextText, damage, delta),
    pathRange: shiftRangeAfterDamage(include.pathRange, previousText, nextText, damage, delta),
  };
}

function shiftServerObjectAfterDamage(
  serverObject: AspServerObject,
  previousText: string,
  nextText: string,
  damage: DamageSpan,
  delta: number,
): AspServerObject {
  return {
    ...serverObject,
    offset: shiftOffsetAfterChange(serverObject.offset, damage.start, damage.end, delta),
    range: shiftRangeAfterDamage(serverObject.range, previousText, nextText, damage, delta),
    idRange: shiftRangeAfterDamage(serverObject.idRange, previousText, nextText, damage, delta),
    progIdRange: serverObject.progIdRange
      ? shiftRangeAfterDamage(serverObject.progIdRange, previousText, nextText, damage, delta)
      : undefined,
    classIdRange: serverObject.classIdRange
      ? shiftRangeAfterDamage(serverObject.classIdRange, previousText, nextText, damage, delta)
      : undefined,
  };
}

function shiftDiagnosticAfterDamage(
  diagnostic: AspParsedDocument["diagnostics"][number],
  previousText: string,
  nextText: string,
  damage: DamageSpan,
  delta: number,
): AspParsedDocument["diagnostics"][number] {
  return {
    ...diagnostic,
    range: shiftRangeAfterDamage(diagnostic.range, previousText, nextText, damage, delta),
  };
}

function buildParsedDocumentWithReuse(
  uri: string,
  text: string,
  regions: AspRegion[],
  directives: AspDirective[],
  includes: AspInclude[],
  serverObjects: AspServerObject[],
  defaultLanguage: "VBScript" | "JScript",
  diagnostics: AspParsedDocument["diagnostics"],
  buildMode: ParsedDocumentBuildMode,
  previous: AspParsedDocument,
  oldDamage: DamageSpan,
  _nextDamage: DamageSpan,
  delta: number,
): ParsedDocumentReuseBuildResult {
  const reusableNodes = reusableNodeMap(previous, oldDamage, delta);
  let reusedRegionCount = 0;
  const items = [
    ...regions.map((region) => ({
      key: nodeReuseKeyForRegion(region),
      build: () =>
        buildMode === "skeleton" ? skeletonRegionToNode(region) : regionToNode(text, region),
      apply: (node: AspCstNode) => {
        if (region.attributes && Object.keys(region.attributes).length > 0) {
          node.attributes = region.attributes;
        }
      },
    })),
    ...includes.map((include) => ({
      key: nodeReuseKeyForInclude(text, include),
      build: () =>
        buildMode === "skeleton"
          ? skeletonIncludeToNode(text, include)
          : includeToNode(text, include),
      apply: (node: AspCstNode) => {
        node.include = include;
      },
    })),
  ];
  const nodes = items
    .map((item) => {
      const reused = takeReusableNode(reusableNodes, item.key);
      const node = reused ?? item.build();
      if (reused) {
        reusedRegionCount += 1;
      }
      item.apply(node);
      return node;
    })
    .sort(
      (left, right) =>
        left.start - right.start || left.end - left.start - (right.end - right.start),
    );
  for (const directive of directives) {
    const node = nodes.find(
      (item) => item.start === directive.offset && item.kind === "AspDirective",
    );
    if (node) {
      node.directive = directive;
      node.attributes = directive.attributes;
    }
  }
  const cst: AspCstNode = {
    kind: "Document",
    start: 0,
    end: text.length,
    contentStart: 0,
    contentEnd: text.length,
    tokens: buildMode === "skeleton" ? [] : nodes.flatMap((node) => node.tokens),
    children: nodes,
    serverObjects,
    errors: diagnostics.map((diagnostic) => ({
      message: diagnostic.message,
      start: offsetFromRange(text, diagnostic.range.start),
      end: offsetFromRange(text, diagnostic.range.end),
    })),
  };
  if (buildMode === "full") {
    cst.text = text;
  }
  return {
    parsed: {
      uri,
      text,
      cst,
      regions: regions.map(stripEmptyRegionAttributes),
      directives,
      includes,
      serverObjects,
      defaultLanguage,
      diagnostics,
    },
    reusedRegionCount,
    reuseCandidateCount: items.length,
  };
}

function reusableNodeMap(
  previous: AspParsedDocument,
  oldDamage: DamageSpan,
  delta: number,
): Map<string, AspCstNode[]> {
  const reusableNodes = new Map<string, AspCstNode[]>();
  for (const node of previous.cst.children) {
    let reusable: AspCstNode | undefined;
    if (node.end <= oldDamage.start) {
      reusable = node;
    } else if (node.start >= oldDamage.end) {
      reusable = shiftAspCstNodeAfterDamage(node, delta);
    }
    if (!reusable) {
      continue;
    }
    const key = nodeReuseKey(reusable);
    if (!key) {
      continue;
    }
    const bucket = reusableNodes.get(key);
    if (bucket) {
      bucket.push(reusable);
    } else {
      reusableNodes.set(key, [reusable]);
    }
  }
  return reusableNodes;
}

function takeReusableNode(nodes: Map<string, AspCstNode[]>, key: string): AspCstNode | undefined {
  const bucket = nodes.get(key);
  if (!bucket || bucket.length === 0) {
    return undefined;
  }
  const node = bucket.shift();
  if (bucket.length === 0) {
    nodes.delete(key);
  }
  return node;
}

function nodeReuseKeyForRegion(region: AspRegion): string {
  return [
    region.kind === "html"
      ? "HtmlText"
      : region.kind === "asp-expression"
        ? "AspExpression"
        : region.kind === "asp-directive"
          ? "AspDirective"
          : region.kind === "style"
            ? "StyleElement"
            : region.kind === "client-script"
              ? "ClientScriptElement"
              : region.kind === "server-script"
                ? "ServerScriptElement"
                : region.kind === "style-attribute"
                  ? "StyleAttribute"
                  : "AspBlock",
    region.kind,
    region.language,
    region.start,
    region.end,
    region.contentStart,
    region.contentEnd,
    attributesKey(region.attributes),
  ].join(":");
}

function nodeReuseKeyForInclude(text: string, include: AspInclude): string {
  return [
    "IncludeDirective",
    "include",
    "html",
    include.offset,
    offsetFromRange(text, include.range.end),
    include.offset,
    offsetFromRange(text, include.range.end),
    include.mode,
    include.path,
  ].join(":");
}

function nodeReuseKey(node: AspCstNode): string | undefined {
  if (node.kind === "IncludeDirective" && node.include) {
    return nodeReuseKeyForIncludeFromNode(node);
  }
  if (!node.regionKind || !node.language) {
    return undefined;
  }
  return [
    node.kind,
    node.regionKind,
    node.language,
    node.start,
    node.end,
    node.contentStart,
    node.contentEnd,
    attributesKey(node.attributes),
  ].join(":");
}

function nodeReuseKeyForIncludeFromNode(node: AspCstNode): string {
  return [
    "IncludeDirective",
    "include",
    "html",
    node.start,
    node.end,
    node.contentStart,
    node.contentEnd,
    node.include?.mode ?? "",
    node.include?.path ?? "",
  ].join(":");
}

function attributesKey(attributes: Record<string, string | true> | undefined): string {
  return attributes ? JSON.stringify(attributes) : "";
}

function stripEmptyRegionAttributes(region: AspRegion): AspRegion {
  if (!region.attributes || Object.keys(region.attributes).length > 0) {
    return region;
  }
  const { attributes: _attributes, ...rest } = region;
  return rest;
}

function shiftAspCstNodeAfterDamage(node: AspCstNode, delta: number): AspCstNode {
  const shifted: AspCstNode = {
    ...node,
    start: node.start + delta,
    end: node.end + delta,
    contentStart: node.contentStart + delta,
    contentEnd: node.contentEnd + delta,
    tokens: node.tokens.map((token) => shiftAspTokenAfterDamage(token, delta)),
    children: node.children.map((child) => shiftAspCstNodeAfterDamage(child, delta)),
  };
  if (node.vbscript) {
    shifted.vbscript = shiftVbCstNodeAfterDamage(node.vbscript, delta);
  }
  if (node.errors) {
    shifted.errors = node.errors.map((error) => ({
      ...error,
      start: error.start + delta,
      end: error.end + delta,
    }));
  }
  if (node.serverObjects) {
    shifted.serverObjects = node.serverObjects.map((serverObject) => ({
      ...serverObject,
      offset: serverObject.offset + delta,
    }));
  }
  return shifted;
}

function shiftAspTokenAfterDamage(token: AspToken, delta: number): AspToken {
  const shifted: AspToken = {
    ...token,
    start: token.start + delta,
    end: token.end + delta,
  };
  if (token.leadingTrivia) {
    shifted.leadingTrivia = token.leadingTrivia.map((trivia) => ({
      ...trivia,
      start: trivia.start + delta,
      end: trivia.end + delta,
    }));
  }
  if (token.trailingTrivia) {
    shifted.trailingTrivia = token.trailingTrivia.map((trivia) => ({
      ...trivia,
      start: trivia.start + delta,
      end: trivia.end + delta,
    }));
  }
  return shifted;
}

function shiftVbTokenAfterDamage(token: VbToken, delta: number): VbToken {
  return {
    ...token,
    start: token.start + delta,
    end: token.end + delta,
  };
}

function shiftVbCstNodeAfterDamage(node: VbCstNode, delta: number): VbCstNode {
  const shifted: VbCstNode = {
    ...node,
    start: node.start + delta,
    end: node.end + delta,
    tokens: node.tokens.map((token) => shiftVbTokenAfterDamage(token, delta)),
    children: node.children.map((child) => shiftVbCstNodeAfterDamage(child, delta)),
  };
  if (node.contentStart !== undefined) {
    shifted.contentStart = node.contentStart + delta;
  }
  if (node.contentEnd !== undefined) {
    shifted.contentEnd = node.contentEnd + delta;
  }
  if (node.nameToken) {
    shifted.nameToken = shiftVbTokenAfterDamage(node.nameToken, delta);
  }
  if (node.identifiers) {
    shifted.identifiers = node.identifiers.map((token) => shiftVbTokenAfterDamage(token, delta));
  }
  if (node.arrayDeclarations) {
    shifted.arrayDeclarations = node.arrayDeclarations.map((declaration) => ({
      ...declaration,
      name: shiftVbTokenAfterDamage(declaration.name, delta),
    }));
  }
  if (node.parameters) {
    shifted.parameters = node.parameters.map((token) => shiftVbTokenAfterDamage(token, delta));
  }
  if (node.parameterMetadata) {
    shifted.parameterMetadata = node.parameterMetadata.map((parameter) => ({
      ...parameter,
      token: shiftVbTokenAfterDamage(parameter.token, delta),
    }));
  }
  if (node.scopeStart !== undefined) {
    shifted.scopeStart = node.scopeStart + delta;
  }
  if (node.scopeEnd !== undefined) {
    shifted.scopeEnd = node.scopeEnd + delta;
  }
  if (node.errors) {
    shifted.errors = node.errors.map((error) => ({
      ...error,
      start: error.start + delta,
      end: error.end + delta,
    }));
  }
  return shifted;
}

function validateIncrementalParsedDocument(parsed: AspParsedDocument): string | undefined {
  if (parsed.cst.start !== 0 || parsed.cst.end !== parsed.text.length) {
    return "invalid incremental CST span";
  }
  for (const region of parsed.regions) {
    if (
      region.start < 0 ||
      region.start > region.contentStart ||
      region.contentStart > region.contentEnd ||
      region.contentEnd > region.end ||
      region.end > parsed.text.length
    ) {
      return "invalid incremental region span";
    }
  }
  for (let index = 1; index < parsed.cst.children.length; index += 1) {
    const previous = parsed.cst.children[index - 1];
    const current = parsed.cst.children[index];
    if (
      previous.start > current.start ||
      (previous.start === current.start &&
        previous.end - previous.start > current.end - current.start)
    ) {
      return "invalid incremental CST order";
    }
  }
  return undefined;
}

export function shiftAspRangeAfterChange(
  range: { start: { line: number; character: number }; end: { line: number; character: number } },
  previousText: string,
  nextText: string,
  change: AspIncrementalChange,
) {
  const normalized = normalizeIncrementalChange(previousText, change);
  if (!normalized) {
    return range;
  }
  const delta = normalized.text.length - (normalized.endOffset - normalized.startOffset);
  const start = shiftOffsetAfterChange(
    offsetAt(previousText, range.start),
    normalized.startOffset,
    normalized.endOffset,
    delta,
  );
  const end = shiftOffsetAfterChange(
    offsetAt(previousText, range.end),
    normalized.startOffset,
    normalized.endOffset,
    delta,
  );
  return rangeFromOffsets(nextText, start, Math.max(start, end));
}

function buildParsedDocument(
  uri: string,
  text: string,
  regions: AspRegion[],
  directives: AspDirective[],
  includes: AspInclude[],
  serverObjects: AspServerObject[],
  defaultLanguage: "VBScript" | "JScript",
  diagnostics: AspParsedDocument["diagnostics"],
  buildMode: ParsedDocumentBuildMode = "full",
): AspParsedDocument {
  const nodes: AspCstNode[] = [
    ...regions.map((region) =>
      buildMode === "skeleton" ? skeletonRegionToNode(region) : regionToNode(text, region),
    ),
    ...includes.map((include) =>
      buildMode === "skeleton"
        ? skeletonIncludeToNode(text, include)
        : includeToNode(text, include),
    ),
  ].sort(
    (left, right) => left.start - right.start || left.end - left.start - (right.end - right.start),
  );
  for (const directive of directives) {
    const node = nodes.find(
      (item) => item.start === directive.offset && item.kind === "AspDirective",
    );
    if (node) {
      node.directive = directive;
      node.attributes = directive.attributes;
    }
  }
  const cst: AspCstNode = {
    kind: "Document",
    start: 0,
    end: text.length,
    contentStart: 0,
    contentEnd: text.length,
    tokens: buildMode === "skeleton" ? [] : nodes.flatMap((node) => node.tokens),
    children: nodes,
    serverObjects,
    errors: diagnostics.map((diagnostic) => ({
      message: diagnostic.message,
      start: offsetFromRange(text, diagnostic.range.start),
      end: offsetFromRange(text, diagnostic.range.end),
    })),
  };
  if (buildMode === "full") {
    cst.text = text;
  }
  return {
    uri,
    text,
    cst,
    regions,
    directives,
    includes,
    serverObjects,
    defaultLanguage,
    diagnostics,
  };
}

function editImpact(
  kind: AspEditImpact["kind"],
  reason: string,
  previousText: string,
  nextText: string,
  change: AspIncrementalChange | undefined,
  region?: AspRegion,
): AspEditImpact {
  const normalized = change ? normalizeIncrementalChange(previousText, change) : undefined;
  const startOffset = normalized?.startOffset ?? 0;
  const endOffset = normalized?.endOffset ?? previousText.length;
  return {
    kind,
    reason,
    startOffset,
    endOffset,
    insertedLength: normalized?.text.length ?? nextText.length,
    deletedLength: endOffset - startOffset,
    delta: nextText.length - previousText.length,
    language: region?.language,
  };
}

function boundarySensitiveText(text: string): boolean {
  return /<%|%>|<!--|#\s*include|<\s*\/?\s*script\b|<\s*\/?\s*style\b|<\s*\/?\s*object\b|\brunat\s*=|\blanguage\s*=|\bprogid\s*=|\bclassid\s*=/i.test(
    text,
  );
}

function changeCreatesStructuralMarker(
  previousText: string,
  nextText: string,
  change: NormalizedIncrementalChange,
  region: AspRegion,
): boolean {
  if (changeClosesStyleAttributeQuote(previousText, change, region)) {
    return true;
  }
  const pattern = structuralMarkerPatternForRegion(region);
  if (!pattern) {
    return false;
  }
  const nextChangeStart = change.startOffset;
  const nextChangeEnd = Math.max(change.startOffset + change.text.length, change.startOffset + 1);
  const scanStart = Math.max(0, nextChangeStart - 64);
  const scanEnd = Math.min(nextText.length, nextChangeEnd + 64);
  const segment = nextText.slice(scanStart, scanEnd);
  let match: RegExpExecArray | null;
  while ((match = pattern.exec(segment)) !== null) {
    const markerStart = scanStart + match.index;
    const markerEnd = markerStart + match[0].length;
    if (rangeOverlaps(markerStart, markerEnd, nextChangeStart, nextChangeEnd)) {
      return true;
    }
  }
  return false;
}

function changeClosesStyleAttributeQuote(
  previousText: string,
  change: NormalizedIncrementalChange,
  region: AspRegion,
): boolean {
  if (region.kind !== "style-attribute" || change.text.length === 0) {
    return false;
  }
  const quote = previousText[region.contentStart - 1];
  return (
    (quote === '"' || quote === "'") &&
    change.startOffset >= region.contentStart &&
    change.text.includes(quote)
  );
}

function structuralMarkerPatternForRegion(region: AspRegion): RegExp | undefined {
  if (region.kind === "html") {
    return /<%|%>|<!--|#\s*include|<\s*\/?\s*script\b|<\s*\/?\s*style\b|<\s*\/?\s*object\b|(?:^|[\s<])(?:runat|language|progid|classid|style)\s*=\s*(?:"|'?)?/gi;
  }
  if (region.kind === "style") {
    return /<%|<\s*\/?\s*style\b/gi;
  }
  if (region.kind === "style-attribute") {
    return /<%/gi;
  }
  if (region.kind === "client-script") {
    return /<%|<\s*\/?\s*script\b/gi;
  }
  if (region.language === "vbscript") {
    return /%>/g;
  }
  return undefined;
}

function changeOverlapsInclude(
  parsed: AspParsedDocument,
  startOffset: number,
  endOffset: number,
): boolean {
  return parsed.includes.some((include) =>
    rangeOverlapsOrTouches(
      startOffset,
      endOffset,
      offsetFromRange(parsed.text, include.range.start),
      offsetFromRange(parsed.text, include.range.end),
    ),
  );
}

function changeOverlapsServerObject(
  parsed: AspParsedDocument,
  startOffset: number,
  endOffset: number,
): boolean {
  return parsed.serverObjects.some((serverObject) =>
    rangeOverlapsOrTouches(
      startOffset,
      endOffset,
      serverObject.offset,
      offsetFromRange(parsed.text, serverObject.range.end),
    ),
  );
}

function changeOverlapsDirective(
  parsed: AspParsedDocument,
  startOffset: number,
  endOffset: number,
): boolean {
  return parsed.directives.some((directive) =>
    rangeOverlapsOrTouches(
      startOffset,
      endOffset,
      directive.offset,
      offsetFromRange(parsed.text, directive.range.end),
    ),
  );
}

function changeTouchesEmbeddedContentBoundary(
  parsed: AspParsedDocument,
  startOffset: number,
  endOffset: number,
): boolean {
  return (
    startOffset === endOffset &&
    parsed.regions.some(
      (region) =>
        region.kind !== "html" &&
        (startOffset === region.contentStart || startOffset === region.contentEnd),
    )
  );
}

function incrementalRegionForChange(
  parsed: AspParsedDocument,
  startOffset: number,
  endOffset: number,
): AspRegion | undefined {
  let best: AspRegion | undefined;
  for (const region of parsed.regions) {
    if (region.contentStart > startOffset || region.contentEnd < endOffset) {
      continue;
    }
    const containsChange =
      startOffset === endOffset
        ? startOffset >= region.contentStart && startOffset < region.contentEnd
        : startOffset >= region.contentStart && endOffset <= region.contentEnd;
    if (!containsChange) {
      continue;
    }
    if (!best || region.contentEnd - region.contentStart < best.contentEnd - best.contentStart) {
      best = region;
    }
  }
  return best;
}

function changeTouchesRegionBoundary(
  region: AspRegion,
  startOffset: number,
  endOffset: number,
): boolean {
  return (
    region.kind !== "html" &&
    (startOffset === region.contentStart || endOffset === region.contentEnd)
  );
}

function shiftRegionsForChange(
  regions: readonly AspRegion[],
  editedRegion: AspRegion,
  startOffset: number,
  endOffset: number,
  delta: number,
): AspRegion[] {
  return regions.map((region) => {
    if (regionContainsChange(region, startOffset, endOffset)) {
      return {
        ...region,
        end: region.end + delta,
        contentEnd: region.contentEnd + delta,
      };
    }
    if (region.start >= endOffset) {
      return shiftRegion(region, delta);
    }
    if (region !== editedRegion && region.end >= endOffset && region.contentEnd >= endOffset) {
      return {
        ...region,
        end: region.end + delta,
        contentEnd: region.contentEnd + delta,
      };
    }
    return region;
  });
}

function regionContainsChange(region: AspRegion, startOffset: number, endOffset: number): boolean {
  return startOffset >= region.contentStart && endOffset <= region.contentEnd;
}

function shiftRegion(region: AspRegion, delta: number): AspRegion {
  return {
    ...region,
    start: region.start + delta,
    end: region.end + delta,
    contentStart: region.contentStart + delta,
    contentEnd: region.contentEnd + delta,
  };
}

function shiftDirectiveAfterChange(
  directive: AspDirective,
  previousText: string,
  nextText: string,
  change: NormalizedIncrementalChange,
): AspDirective {
  return {
    ...directive,
    offset: shiftOffsetAfterChange(
      directive.offset,
      change.startOffset,
      change.endOffset,
      change.text.length - (change.endOffset - change.startOffset),
    ),
    range: shiftAspRangeAfterChange(directive.range, previousText, nextText, change),
  };
}

function shiftIncludeAfterChange(
  include: AspInclude,
  previousText: string,
  nextText: string,
  change: NormalizedIncrementalChange,
): AspInclude {
  const delta = change.text.length - (change.endOffset - change.startOffset);
  return {
    ...include,
    offset: shiftOffsetAfterChange(include.offset, change.startOffset, change.endOffset, delta),
    range: shiftAspRangeAfterChange(include.range, previousText, nextText, change),
    directiveRange: shiftAspRangeAfterChange(
      include.directiveRange,
      previousText,
      nextText,
      change,
    ),
    modeRange: shiftAspRangeAfterChange(include.modeRange, previousText, nextText, change),
    pathRange: shiftAspRangeAfterChange(include.pathRange, previousText, nextText, change),
  };
}

function shiftServerObjectAfterChange(
  serverObject: AspServerObject,
  previousText: string,
  nextText: string,
  change: NormalizedIncrementalChange,
): AspServerObject {
  const delta = change.text.length - (change.endOffset - change.startOffset);
  return {
    ...serverObject,
    offset: shiftOffsetAfterChange(
      serverObject.offset,
      change.startOffset,
      change.endOffset,
      delta,
    ),
    range: shiftAspRangeAfterChange(serverObject.range, previousText, nextText, change),
    idRange: shiftAspRangeAfterChange(serverObject.idRange, previousText, nextText, change),
    progIdRange: serverObject.progIdRange
      ? shiftAspRangeAfterChange(serverObject.progIdRange, previousText, nextText, change)
      : undefined,
    classIdRange: serverObject.classIdRange
      ? shiftAspRangeAfterChange(serverObject.classIdRange, previousText, nextText, change)
      : undefined,
  };
}

export function parseAspCst(uri: string, text: string, settings: AspSettings = {}): AspCstNode {
  return parseAspCstTypeScript(uri, text, settings);
}

export async function parseAspCstAsync(
  uri: string,
  text: string,
  settings: AspSettings = {},
): Promise<AspCstNode> {
  return parseAspCstTypeScript(uri, text, settings);
}

function parseAspCstTypeScript(uri: string, text: string, settings: AspSettings = {}): AspCstNode {
  const diagnostics: AspParsedDocument["diagnostics"] = [];
  const scan = scanHtmlAndAsp(text, diagnostics, settings);
  const { inlineRegions, tagRegions, includes, serverObjects } = scan;
  const directives = inlineRegions
    .filter((region) => region.kind === "asp-directive")
    .map((region): AspDirective => {
      const raw = text.slice(region.contentStart, region.contentEnd).trim();
      const normalized = raw.startsWith("@") ? raw.slice(1).trim() : raw;
      const [first = "Page", ...rest] = normalized.split(/\s+/);
      const hasExplicitName = !first.includes("=");
      const name = hasExplicitName ? first : "Page";
      const attributeText = hasExplicitName ? rest.join(" ") : normalized;
      return {
        offset: region.start,
        range: rangeFromOffsets(text, region.start, region.end),
        name,
        attributes: parseAttributes(attributeText),
      };
    });
  const directiveLanguage = directives
    .map((directive) => directive.attributes.language ?? directive.attributes.LANGUAGE)
    .find((value): value is string => typeof value === "string");
  const defaultLanguage = normalizeScriptLanguage(
    directiveLanguage ?? settings.defaultLanguage ?? "VBScript",
  );
  const scriptRegions = tagRegions.map((region): AspRegion => {
    if (region.kind !== "server-script") {
      return region;
    }
    return {
      ...region,
      language:
        normalizeScriptLanguage(
          String(region.attributes?.language ?? defaultLanguage),
        ).toLowerCase() === "jscript"
          ? "jscript"
          : "vbscript",
    };
  });
  const regions = buildRegions(text, [...inlineRegions, ...scriptRegions], defaultLanguage);
  const nodes: AspCstNode[] = [
    ...regions.map((region) => regionToNode(text, region)),
    ...includes.map((include) => includeToNode(text, include)),
  ].sort(
    (left, right) => left.start - right.start || left.end - left.start - (right.end - right.start),
  );
  for (const directive of directives) {
    const node = nodes.find(
      (item) => item.start === directive.offset && item.kind === "AspDirective",
    );
    if (node) {
      node.directive = directive;
      node.attributes = directive.attributes;
    }
  }
  const root: AspCstNode = {
    kind: "Document",
    start: 0,
    end: text.length,
    contentStart: 0,
    contentEnd: text.length,
    text,
    tokens: nodes.flatMap((node) => node.tokens),
    children: nodes,
    serverObjects,
    errors: diagnostics.map((diagnostic) => ({
      message: diagnostic.message,
      start: offsetFromRange(text, diagnostic.range.start),
      end: offsetFromRange(text, diagnostic.range.end),
    })),
  };
  void uri;
  return root;
}

function regionToNode(text: string, region: AspRegion): AspCstNode {
  const kind: AspCstNode["kind"] =
    region.kind === "html"
      ? "HtmlText"
      : region.kind === "asp-expression"
        ? "AspExpression"
        : region.kind === "asp-directive"
          ? "AspDirective"
          : region.kind === "style"
            ? "StyleElement"
            : region.kind === "client-script"
              ? "ClientScriptElement"
              : region.kind === "server-script"
                ? "ServerScriptElement"
                : region.kind === "style-attribute"
                  ? "StyleAttribute"
                  : "AspBlock";
  const node: AspCstNode = {
    kind,
    start: region.start,
    end: region.end,
    contentStart: region.contentStart,
    contentEnd: region.contentEnd,
    language: region.language,
    text: text.slice(region.start, region.end),
    tokens: regionTokens(text, region),
    children: [],
    attributes: region.attributes,
    regionKind: region.kind,
  };
  if (region.language === "vbscript") {
    node.vbscript = parseVbscriptCst(
      text.slice(region.contentStart, region.contentEnd),
      text,
      region.contentStart,
    );
  }
  return node;
}

function skeletonRegionToNode(region: AspRegion): AspCstNode {
  const kind: AspCstNode["kind"] =
    region.kind === "html"
      ? "HtmlText"
      : region.kind === "asp-expression"
        ? "AspExpression"
        : region.kind === "asp-directive"
          ? "AspDirective"
          : region.kind === "style"
            ? "StyleElement"
            : region.kind === "client-script"
              ? "ClientScriptElement"
              : region.kind === "server-script"
                ? "ServerScriptElement"
                : region.kind === "style-attribute"
                  ? "StyleAttribute"
                  : "AspBlock";
  const node: AspCstNode = {
    kind,
    start: region.start,
    end: region.end,
    contentStart: region.contentStart,
    contentEnd: region.contentEnd,
    language: region.language,
    tokens: [],
    children: [],
    regionKind: region.kind,
  };
  if (region.attributes && Object.keys(region.attributes).length > 0) {
    node.attributes = region.attributes;
  }
  return node;
}

function includeToNode(text: string, include: AspInclude): AspCstNode {
  return {
    kind: "IncludeDirective",
    start: include.offset,
    end: offsetFromRange(text, include.range.end),
    contentStart: include.offset,
    contentEnd: offsetFromRange(text, include.range.end),
    language: "html",
    text: text.slice(include.offset, offsetFromRange(text, include.range.end)),
    tokens: [
      {
        kind: "includeDirective",
        start: include.offset,
        end: offsetFromRange(text, include.range.end),
        text: text.slice(include.offset, offsetFromRange(text, include.range.end)),
      },
    ],
    children: [],
    include,
  };
}

function skeletonIncludeToNode(text: string, include: AspInclude): AspCstNode {
  const end = offsetFromRange(text, include.range.end);
  return {
    kind: "IncludeDirective",
    start: include.offset,
    end,
    contentStart: include.offset,
    contentEnd: end,
    language: "html",
    tokens: [],
    children: [],
    include,
  };
}

function parseCacheKey(uri: string, text: string, settings: AspSettings): string {
  return `{"uri":${JSON.stringify(uri)},"text":${JSON.stringify(textFingerprint(text))},"settings":${parseSettingsString(settings)}}`;
}

function parseSettingsString(settings: AspSettings): string {
  const cached = parseSettingsStringCache.get(settings);
  if (cached !== undefined) {
    return cached;
  }
  const value = JSON.stringify(settings);
  parseSettingsStringCache.set(settings, value);
  return value;
}

function setAsyncParseCache(key: string, parsed: AspParsedDocument): void {
  if (asyncParseCache.has(key)) {
    deleteAsyncParseCacheEntry(key);
  }
  const bytes = estimateAsyncParseCacheEntryBytes(key, parsed);
  asyncParseCache.set(key, parsed);
  asyncParseCacheEntryBytes.set(key, bytes);
  asyncParseCacheEstimatedBytes += bytes;
  while (asyncParseCache.size > asyncParseCacheMaxEntries) {
    const oldest = asyncParseCache.keys().next().value;
    if (oldest === undefined) {
      break;
    }
    deleteAsyncParseCacheEntry(oldest);
  }
}

function touchAsyncParseCacheEntry(key: string, parsed: AspParsedDocument): void {
  const bytes =
    asyncParseCacheEntryBytes.get(key) ?? estimateAsyncParseCacheEntryBytes(key, parsed);
  deleteAsyncParseCacheEntry(key);
  asyncParseCache.set(key, parsed);
  asyncParseCacheEntryBytes.set(key, bytes);
  asyncParseCacheEstimatedBytes += bytes;
}

function textFingerprint(text: string): string {
  const cached = textFingerprintCache.get(text);
  if (cached) {
    deleteTextFingerprintCacheEntry(text);
    textFingerprintCache.set(text, cached);
    const bytes = estimateTextFingerprintCacheEntryBytes(text, cached);
    textFingerprintCacheEntryBytes.set(text, bytes);
    textFingerprintCacheEstimatedBytes += bytes;
    return cached;
  }
  let hash = 2166136261;
  for (let index = 0; index < text.length; index += 1) {
    hash ^= text.charCodeAt(index);
    hash = Math.imul(hash, 16777619);
  }
  const fingerprint = `${text.length}:${(hash >>> 0).toString(16)}`;
  const bytes = estimateTextFingerprintCacheEntryBytes(text, fingerprint);
  textFingerprintCache.set(text, fingerprint);
  textFingerprintCacheEntryBytes.set(text, bytes);
  textFingerprintCacheEstimatedBytes += bytes;
  while (textFingerprintCache.size > textFingerprintCacheMaxEntries) {
    const oldest = textFingerprintCache.keys().next().value;
    if (oldest === undefined) {
      break;
    }
    deleteTextFingerprintCacheEntry(oldest);
  }
  return fingerprint;
}

function evictAsyncParseCache(targetBytes: number): number {
  let freed = 0;
  while (asyncParseCache.size > 0 && freed < targetBytes) {
    const oldest = asyncParseCache.keys().next().value;
    if (oldest === undefined) {
      break;
    }
    freed += deleteAsyncParseCacheEntry(oldest);
  }
  return freed;
}

function deleteAsyncParseCacheEntry(key: string): number {
  const bytes = asyncParseCacheEntryBytes.get(key) ?? 0;
  if (!asyncParseCache.delete(key)) {
    return 0;
  }
  asyncParseCacheEntryBytes.delete(key);
  asyncParseCacheEstimatedBytes = Math.max(0, asyncParseCacheEstimatedBytes - bytes);
  return bytes;
}

function estimateAsyncParseCacheEntryBytes(key: string, parsed: AspParsedDocument): number {
  return (
    estimateCacheStringBytes(key) +
    estimateCacheStringBytes(parsed.text) +
    parsed.regions.length * 256 +
    parsed.includes.length * 192 +
    parsed.directives.length * 192 +
    parsed.serverObjects.length * 192 +
    estimateCstNodeBytes(parsed.cst)
  );
}

function estimateCstNodeBytes(node: AspCstNode): number {
  let total = 160 + estimateCacheStringBytes(node.text);
  if (node.tokens) {
    total += node.tokens.length * 96;
  }
  if (node.vbscript?.tokens) {
    total += estimateVbCstNodeBytes(node.vbscript);
  }
  for (const child of node.children) {
    total += estimateCstNodeBytes(child);
  }
  return total;
}

function estimateVbCstNodeBytes(node: VbCstNode): number {
  let total =
    128 +
    node.tokens.length * 96 +
    estimateCacheStringBytes(node.typeName) +
    estimateCacheStringBytes(node.memberOf) +
    estimateCacheStringBytes(node.scopeName);
  for (const child of node.children) {
    total += estimateVbCstNodeBytes(child);
  }
  return total;
}

function evictTextFingerprintCache(targetBytes: number): number {
  let freed = 0;
  while (textFingerprintCache.size > 0 && freed < targetBytes) {
    const oldest = textFingerprintCache.keys().next().value;
    if (oldest === undefined) {
      break;
    }
    freed += deleteTextFingerprintCacheEntry(oldest);
  }
  return freed;
}

function deleteTextFingerprintCacheEntry(text: string): number {
  const bytes = textFingerprintCacheEntryBytes.get(text) ?? 0;
  if (!textFingerprintCache.delete(text)) {
    return 0;
  }
  textFingerprintCacheEntryBytes.delete(text);
  textFingerprintCacheEstimatedBytes = Math.max(0, textFingerprintCacheEstimatedBytes - bytes);
  return bytes;
}

function estimateTextFingerprintCacheEntryBytes(text: string, fingerprint: string): number {
  return estimateCacheStringBytes(text) + estimateCacheStringBytes(fingerprint) + 64;
}

function estimateCacheStringBytes(value: string | undefined): number {
  return value === undefined ? 0 : value.length * 2 + 40;
}

function nodeToRegion(node: AspCstNode): AspRegion | undefined {
  if (!node.regionKind || !node.language) {
    return undefined;
  }
  const region: AspRegion = {
    kind: node.regionKind,
    language: node.language,
    start: node.start,
    end: node.end,
    contentStart: node.contentStart,
    contentEnd: node.contentEnd,
  };
  if (node.attributes && Object.keys(node.attributes).length > 0) {
    region.attributes = node.attributes;
  }
  return region;
}

function regionTokens(text: string, region: AspRegion): AspToken[] {
  if (
    region.kind === "asp-block" ||
    region.kind === "asp-expression" ||
    region.kind === "asp-directive"
  ) {
    const openKind =
      region.kind === "asp-expression"
        ? "aspExpressionOpen"
        : region.kind === "asp-directive"
          ? "aspDirectiveOpen"
          : "aspOpen";
    return [
      {
        kind: openKind,
        start: region.start,
        end: region.contentStart,
        text: text.slice(region.start, region.contentStart),
      },
      {
        kind: "text",
        start: region.contentStart,
        end: region.contentEnd,
        text: text.slice(region.contentStart, region.contentEnd),
      },
      {
        kind: "aspClose",
        start: region.contentEnd,
        end: region.end,
        text: text.slice(region.contentEnd, region.end),
      },
    ];
  }
  if (region.kind === "html") {
    return [
      {
        kind: "text",
        start: region.start,
        end: region.end,
        text: text.slice(region.start, region.end),
      },
    ];
  }
  return [
    {
      kind: "tagOpen",
      start: region.start,
      end: region.contentStart,
      text: text.slice(region.start, region.contentStart),
    },
    {
      kind: "text",
      start: region.contentStart,
      end: region.contentEnd,
      text: text.slice(region.contentStart, region.contentEnd),
    },
    {
      kind: "tagClose",
      start: region.contentEnd,
      end: region.end,
      text: text.slice(region.contentEnd, region.end),
    },
  ];
}

function offsetFromRange(text: string, position: { line: number; character: number }): number {
  return offsetAt(text, position);
}

function buildRegions(
  text: string,
  embeddedRegions: AspRegion[],
  defaultLanguage: "VBScript" | "JScript",
): AspRegion[] {
  const sorted = embeddedRegions
    .filter((region) => region.end > region.start || region.kind === "style-attribute")
    .sort((left, right) => left.start - right.start || right.end - left.end);
  const accepted: AspRegion[] = [];
  const topLevel: AspRegion[] = [];
  let coveredEnd = -1;
  for (const region of sorted) {
    const language =
      region.language === "vbscript" &&
      defaultLanguage === "JScript" &&
      (region.kind === "asp-block" || region.kind === "asp-expression")
        ? "jscript"
        : region.language;
    const normalized = { ...region, language };
    if (region.start < coveredEnd) {
      if (
        region.kind === "asp-block" ||
        region.kind === "asp-expression" ||
        region.kind === "asp-directive" ||
        region.kind === "style-attribute"
      ) {
        accepted.push(normalized);
      }
      continue;
    }
    accepted.push(normalized);
    topLevel.push(normalized);
    coveredEnd = region.end;
  }

  const regions: AspRegion[] = [];
  let cursor = 0;
  for (const region of topLevel) {
    if (cursor < region.start) {
      regions.push(htmlRegion(cursor, region.start));
    }
    if (regionHasHtmlTagWrapper(region) && region.start < region.contentStart) {
      regions.push(htmlRegion(region.start, region.contentStart));
    }
    regions.push(region);
    if (regionHasHtmlTagWrapper(region) && region.contentEnd < region.end) {
      regions.push(htmlRegion(region.contentEnd, region.end));
    }
    cursor = region.end;
  }
  if (cursor < text.length) {
    regions.push(htmlRegion(cursor, text.length));
  }
  const topLevelSet = new Set(topLevel);
  return [...regions, ...accepted.filter((region) => !topLevelSet.has(region))].sort(
    (left, right) => left.start - right.start || left.end - left.start - (right.end - right.start),
  );
}

function regionHasHtmlTagWrapper(region: AspRegion): boolean {
  return (
    region.kind === "style" || region.kind === "client-script" || region.kind === "server-script"
  );
}

function htmlRegion(start: number, end: number): AspRegion {
  return {
    kind: "html",
    language: "html",
    start,
    end,
    contentStart: start,
    contentEnd: end,
  };
}
