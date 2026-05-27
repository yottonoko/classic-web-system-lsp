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
  AspSettings,
  AspToken,
} from "./types";
import { offsetAt, rangeFromOffsets } from "./position";
import { scanHtmlAndAsp, normalizeScriptLanguage, parseAttributes } from "./asp-scanner";
import { parseVbscriptCst } from "./vbscript-cst";
export { normalizeScriptLanguage, parseAttributes } from "./asp-scanner";

export function parseAspDocument(
  uri: string,
  text: string,
  settings: AspSettings = {},
): AspParsedDocument {
  const cst = parseAspCst(uri, text, settings);
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
    defaultLanguage,
    diagnostics,
  };
}

export function updateAspParsedDocument(
  previous: AspParsedDocument,
  changes: readonly AspIncrementalChange[],
  settings: AspSettings = {},
): AspIncrementalUpdateResult {
  const fallback = (
    reason: string,
    change = changes[0],
    nextText = applyIncrementalChanges(previous.text, changes),
  ): AspIncrementalUpdateResult => ({
    parsed: parseAspDocument(previous.uri, nextText, settings),
    impact: editImpact("full", reason, previous.text, nextText, change),
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
  if (changeOverlapsDirective(previous, change.startOffset, change.endOffset)) {
    return fallback("ASP directive edit", change, nextText);
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
  const shiftedDiagnostics = previous.diagnostics.map((diagnostic) => ({
    ...diagnostic,
    range: shiftAspRangeAfterChange(diagnostic.range, previous.text, nextText, change),
  }));
  return {
    parsed: buildParsedDocument(
      previous.uri,
      nextText,
      shiftedRegions,
      shiftedDirectives,
      shiftedIncludes,
      previous.defaultLanguage,
      shiftedDiagnostics,
    ),
    impact: editImpact("incremental", "safe content edit", previous.text, nextText, change, region),
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
  defaultLanguage: "VBScript" | "JScript",
  diagnostics: AspParsedDocument["diagnostics"],
): AspParsedDocument {
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
  const cst: AspCstNode = {
    kind: "Document",
    start: 0,
    end: text.length,
    contentStart: 0,
    contentEnd: text.length,
    text,
    tokens: nodes.flatMap((node) => node.tokens),
    children: nodes,
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
  return /<%|%>|<!--|#\s*include|<\s*\/?\s*script\b|<\s*\/?\s*style\b|\brunat\s*=|\blanguage\s*=/i.test(
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
        ? startOffset >= region.contentStart && startOffset <= region.contentEnd
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
  const diagnostics: AspParsedDocument["diagnostics"] = [];
  const scan = scanHtmlAndAsp(text, diagnostics, settings);
  const { inlineRegions, tagRegions, includes } = scan;
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

function nodeToRegion(node: AspCstNode): AspRegion | undefined {
  if (!node.regionKind || !node.language) {
    return undefined;
  }
  return {
    kind: node.regionKind,
    language: node.language,
    start: node.start,
    end: node.end,
    contentStart: node.contentStart,
    contentEnd: node.contentEnd,
    attributes: node.attributes,
  };
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
    const normalized = { ...region, language };
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
      regions.push({
        kind: "html",
        language: "html",
        start: cursor,
        end: region.start,
        contentStart: cursor,
        contentEnd: region.start,
      });
    }
    regions.push(region);
    cursor = region.end;
  }
  if (cursor < text.length) {
    regions.push({
      kind: "html",
      language: "html",
      start: cursor,
      end: text.length,
      contentStart: cursor,
      contentEnd: text.length,
    });
  }
  const topLevelSet = new Set(topLevel);
  return [...regions, ...accepted.filter((region) => !topLevelSet.has(region))].sort(
    (left, right) => left.start - right.start || left.end - left.start - (right.end - right.start),
  );
}
