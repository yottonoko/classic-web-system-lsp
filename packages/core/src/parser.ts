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
} from "./types";
import { offsetAt, rangeFromOffsets } from "./position";
import { scanHtmlAndAsp, normalizeScriptLanguage, parseAttributes } from "./asp-scanner";
import { parseVbscriptCst } from "./vbscript-cst";
export { normalizeScriptLanguage, parseAttributes } from "./asp-scanner";

const asyncParseCacheMaxEntries = 64;
const asyncParseCache = new Map<string, { text: string; parsed: AspParsedDocument }>();
const asyncParseContentCacheMaxTexts = 64;
const asyncParseContentCache = new Map<string, Map<string, AspParsedDocument>>();

export function clearAspParseCaches(): void {
  asyncParseCache.clear();
  asyncParseContentCache.clear();
}

export function parseAspDocument(
  uri: string,
  text: string,
  settings: AspSettings = {},
): AspParsedDocument {
  return parseAspDocumentTypeScript(uri, text, settings);
}

/**
 * Parses an ASP document for async/server workflows.
 *
 * Async callers may receive a skeleton CST without attached VBScript CST subtrees.
 * Call `needsVbscriptCstHydration` and then `await hydrateVbscriptCst` before walking
 * `parsed.cst` for VBScript tokens or declarations.
 */
export async function parseAspDocumentAsync(
  uri: string,
  text: string,
  settings: AspSettings = {},
): Promise<AspParsedDocument> {
  const cacheKey = parseCacheKey(uri, settings);
  const cached = asyncParseCache.get(cacheKey);
  if (cached && cached.text === text) {
    asyncParseCache.delete(cacheKey);
    asyncParseCache.set(cacheKey, cached);
    return cached.parsed;
  }
  const settingsKey = parseSettingsCacheKey(settings);
  const cachedByText = asyncParseContentCache.get(text);
  const cachedBySettings = cachedByText?.get(settingsKey);
  if (cachedByText && cachedBySettings) {
    refreshAsyncParseContentCache(text, cachedByText);
    const parsed = withParsedDocumentUri(cachedBySettings, uri);
    setAsyncParseCache(cacheKey, text, parsed);
    return parsed;
  }
  const parsed = parseAspDocumentSkeletonTypeScript(uri, text, settings);
  setAsyncParseCache(cacheKey, text, parsed);
  setAsyncParseContentCache(text, settingsKey, parsed);
  return parsed;
}

export async function parseAspDocumentSkeletonAsync(
  uri: string,
  text: string,
  settings: AspSettings = {},
): Promise<AspParsedDocument> {
  return parseAspDocumentSkeletonTypeScript(uri, text, settings);
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
        range: rangeFromOffsetsLinear(text, region.start, region.end),
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
  const directiveByOffset = new Map(directives.map((directive) => [directive.offset, directive]));
  const regions = buildRegions(text, [...inlineRegions, ...scriptRegions], defaultLanguage);
  for (const region of regions) {
    const directive = directiveByOffset.get(region.start);
    if (directive) {
      region.attributes = directive.attributes;
    }
  }
  const nodes: AspCstNode[] = [
    ...regions.map(skeletonRegionToNode),
    ...includes.map((include) => skeletonIncludeToNode(text, include)),
  ].sort(
    (left, right) => left.start - right.start || left.end - left.start - (right.end - right.start),
  );
  for (const node of nodes) {
    if (node.kind === "AspDirective") {
      const directive = directiveByOffset.get(node.start);
      if (directive) {
        node.directive = directive;
        node.attributes = directive.attributes;
      }
    }
  }
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
    regions,
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
  const attempt = tryUpdateAspParsedDocumentIncremental(previous, changes, "full");
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
  const attempt = tryUpdateAspParsedDocumentIncremental(previous, changes, "skeleton");
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
  if (change.text.length > 256 || change.endOffset - change.startOffset > 256) {
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

function applyIncrementalChanges(
  previousText: string,
  changes: readonly AspIncrementalChange[],
): string {
  return [...changes]
    .map((change) => normalizeIncrementalChange(previousText, change))
    .filter((change): change is NormalizedIncrementalChange => Boolean(change))
    .sort((left, right) => right.startOffset - left.startOffset)
    .reduce(
      (text, change) =>
        `${text.slice(0, change.startOffset)}${change.text}${text.slice(change.endOffset)}`,
      previousText,
    );
}

interface NormalizedIncrementalChange extends AspIncrementalChange {
  startOffset: number;
  endOffset: number;
}

function normalizeIncrementalChange(
  previousText: string,
  change: AspIncrementalChange,
): NormalizedIncrementalChange | undefined {
  const startOffset = change.rangeOffset ?? offsetAt(previousText, change.range.start);
  const rangeLength = change.rangeLength ?? offsetAt(previousText, change.range.end) - startOffset;
  const endOffset = startOffset + rangeLength;
  if (
    startOffset < 0 ||
    endOffset < startOffset ||
    startOffset > previousText.length ||
    endOffset > previousText.length
  ) {
    return undefined;
  }
  return { ...change, startOffset, endOffset, rangeOffset: startOffset, rangeLength };
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

function rangeOverlapsOrTouches(
  startOffset: number,
  endOffset: number,
  rangeStart: number,
  rangeEnd: number,
): boolean {
  return startOffset === endOffset
    ? startOffset >= rangeStart && startOffset <= rangeEnd
    : startOffset < rangeEnd && endOffset > rangeStart;
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
  return parsed.regions
    .filter((region) => {
      if (region.contentStart > startOffset || region.contentEnd < endOffset) {
        return false;
      }
      return startOffset === endOffset
        ? startOffset >= region.contentStart && startOffset < region.contentEnd
        : startOffset >= region.contentStart && endOffset <= region.contentEnd;
    })
    .sort(
      (left, right) =>
        left.contentEnd - left.contentStart - (right.contentEnd - right.contentStart),
    )[0];
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

function shiftOffsetAfterChange(
  offset: number,
  startOffset: number,
  endOffset: number,
  delta: number,
): number {
  if (offset < startOffset) {
    return offset;
  }
  if (offset >= endOffset) {
    return offset + delta;
  }
  return startOffset;
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

function rangeFromOffsetsLinear(text: string, start: number, end: number) {
  const safeStart = Math.max(0, Math.min(start, text.length));
  const safeEnd = Math.max(0, Math.min(end, text.length));
  let line = 0;
  let lineStart = 0;
  let startPosition = { line: 0, character: safeStart };
  let endPosition = { line: 0, character: safeEnd };
  for (let index = 0; index <= safeEnd; index += 1) {
    if (index === safeStart) {
      startPosition = { line, character: index - lineStart };
    }
    if (index === safeEnd) {
      endPosition = { line, character: index - lineStart };
      break;
    }
    if (text.charCodeAt(index) === 10) {
      line += 1;
      lineStart = index + 1;
    }
  }
  return { start: startPosition, end: endPosition };
}

function parseCacheKey(uri: string, settings: AspSettings): string {
  return JSON.stringify({
    uri,
    settings,
  });
}

function parseSettingsCacheKey(settings: AspSettings): string {
  return JSON.stringify(settings);
}

function withParsedDocumentUri(parsed: AspParsedDocument, uri: string): AspParsedDocument {
  return parsed.uri === uri ? parsed : { ...parsed, uri };
}

function setAsyncParseCache(key: string, text: string, parsed: AspParsedDocument): void {
  if (asyncParseCache.has(key)) {
    asyncParseCache.delete(key);
  }
  asyncParseCache.set(key, { text, parsed });
  while (asyncParseCache.size > asyncParseCacheMaxEntries) {
    const oldest = asyncParseCache.keys().next().value;
    if (oldest === undefined) {
      break;
    }
    asyncParseCache.delete(oldest);
  }
}

function setAsyncParseContentCache(
  text: string,
  settingsKey: string,
  parsed: AspParsedDocument,
): void {
  let cachedBySettings = asyncParseContentCache.get(text);
  if (!cachedBySettings) {
    cachedBySettings = new Map();
    asyncParseContentCache.set(text, cachedBySettings);
  } else {
    refreshAsyncParseContentCache(text, cachedBySettings);
  }
  cachedBySettings.set(settingsKey, parsed);
  while (asyncParseContentCache.size > asyncParseContentCacheMaxTexts) {
    const oldest = asyncParseContentCache.keys().next().value;
    if (oldest === undefined) {
      break;
    }
    asyncParseContentCache.delete(oldest);
  }
}

function refreshAsyncParseContentCache(
  text: string,
  cachedBySettings: Map<string, AspParsedDocument>,
): void {
  asyncParseContentCache.delete(text);
  asyncParseContentCache.set(text, cachedBySettings);
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
    .filter((region) => region.end > region.start)
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
    const normalized = language === region.language ? region : { ...region, language };
    if (region.start < coveredEnd) {
      if (
        region.kind === "asp-block" ||
        region.kind === "asp-expression" ||
        region.kind === "asp-directive"
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
  const nestedRegions = accepted
    .filter((region) => !topLevelSet.has(region))
    .sort(compareRegionsBySourceRange);
  return mergeRegionsBySourceRange(regions, nestedRegions);
}

function mergeRegionsBySourceRange(left: AspRegion[], right: AspRegion[]): AspRegion[] {
  const result: AspRegion[] = [];
  let leftIndex = 0;
  let rightIndex = 0;
  while (leftIndex < left.length && rightIndex < right.length) {
    if (compareRegionsBySourceRange(left[leftIndex], right[rightIndex]) <= 0) {
      result.push(left[leftIndex]);
      leftIndex += 1;
    } else {
      result.push(right[rightIndex]);
      rightIndex += 1;
    }
  }
  while (leftIndex < left.length) {
    result.push(left[leftIndex]);
    leftIndex += 1;
  }
  while (rightIndex < right.length) {
    result.push(right[rightIndex]);
    rightIndex += 1;
  }
  return result;
}

function compareRegionsBySourceRange(left: AspRegion, right: AspRegion): number {
  return left.start - right.start || left.end - left.start - (right.end - right.start);
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
