import type { Position } from "vscode-languageserver-types";
import { offsetAt, positionAt } from "./position";
import type {
  AspEmbeddedLanguage,
  AspParsedDocument,
  AspRegion,
  SourceMap,
  SourceMapSegment,
  VirtualDocument,
} from "./types";

export function buildVirtualDocuments(
  parsed: AspParsedDocument,
): Map<AspEmbeddedLanguage, VirtualDocument> {
  const languages: AspEmbeddedLanguage[] = [
    "html",
    "css",
    "javascript",
    "vbscript",
    "jscript",
    "asp-directive",
  ];
  const result = new Map<AspEmbeddedLanguage, VirtualDocument>();
  for (const language of languages) {
    const regions = parsed.regions.filter((region) => region.language === language);
    if (regions.length === 0 && language !== "html") {
      continue;
    }
    result.set(
      language,
      buildVirtualDocument(parsed.uri, parsed.text, language, regions, parsed.regions),
    );
  }
  return result;
}

export function buildVirtualDocument(
  uri: string,
  sourceText: string,
  languageId: AspEmbeddedLanguage,
  regions: AspRegion[],
  allRegions: AspRegion[] = regions,
): VirtualDocument {
  if (languageId === "html") {
    return buildMaskedDocument(uri, sourceText, languageId, regions);
  }

  const sortedRegions = [...allRegions].sort(
    (left, right) => left.start - right.start || left.end - right.end,
  );
  const chunks: string[] = [];
  const segments: SourceMapSegment[] = [];
  let textLength = 0;
  for (const region of regions) {
    const prefix =
      languageId === "css" ? (region.kind === "style-attribute" ? "__asp_lsp__{" : "\n") : "";
    const suffix = languageId === "css" && region.kind === "style-attribute" ? "}\n" : "\n";
    const start = textLength + prefix.length;
    const nestedRegions = nestedRegionsForOwner(region, sortedRegions);
    const content = maskNestedRegions(sourceText, region, nestedRegions, languageId);
    chunks.push(prefix, content, suffix);
    textLength += prefix.length + content.length + suffix.length;
    segments.push(...sourceMapSegmentsForRegion(region, nestedRegions, start));
  }
  const text = chunks.join("");
  return {
    uri: `${uri}.${languageId}.virtual`,
    languageId,
    text,
    sourceMap: createSourceMap(sourceText, text, segments),
  };
}

function sourceMapSegmentsForRegion(
  owner: AspRegion,
  nestedRegions: AspRegion[],
  virtualStart: number,
): SourceMapSegment[] {
  const holes = nestedRegions.filter(isAspRegionHole);
  const segments: SourceMapSegment[] = [];
  let cursor = owner.contentStart;
  for (const hole of holes) {
    if (cursor < hole.start) {
      segments.push(sourceMapSegment(owner, virtualStart, cursor, hole.start));
    }
    cursor = Math.max(cursor, hole.end);
  }
  if (cursor < owner.contentEnd) {
    segments.push(sourceMapSegment(owner, virtualStart, cursor, owner.contentEnd));
  }
  return segments;
}

function sourceMapSegment(
  owner: AspRegion,
  virtualStart: number,
  sourceStart: number,
  sourceEnd: number,
): SourceMapSegment {
  const offset = sourceStart - owner.contentStart;
  return {
    virtualStart: virtualStart + offset,
    virtualEnd: virtualStart + offset + (sourceEnd - sourceStart),
    sourceStart,
    sourceEnd,
  };
}

function isNestedAspRegion(owner: AspRegion, nested: AspRegion): boolean {
  return nested !== owner && nested.start >= owner.contentStart && nested.end <= owner.contentEnd;
}

function isAspRegionHole(region: AspRegion): boolean {
  return (
    region.kind === "asp-block" ||
    region.kind === "asp-expression" ||
    region.kind === "asp-directive"
  );
}

function nestedRegionsForOwner(owner: AspRegion, sortedRegions: AspRegion[]): AspRegion[] {
  const regions: AspRegion[] = [];
  let index = lowerBoundRegionStart(sortedRegions, owner.contentStart);
  while (index < sortedRegions.length) {
    const nested = sortedRegions[index];
    if (nested.start >= owner.contentEnd) {
      break;
    }
    if (isNestedAspRegion(owner, nested)) {
      regions.push(nested);
    }
    index += 1;
  }
  return regions;
}

function lowerBoundRegionStart(regions: AspRegion[], offset: number): number {
  let low = 0;
  let high = regions.length;
  while (low < high) {
    const middle = Math.floor((low + high) / 2);
    if (regions[middle].start < offset) {
      low = middle + 1;
    } else {
      high = middle;
    }
  }
  return low;
}

function maskNestedRegions(
  sourceText: string,
  owner: AspRegion,
  nestedRegions: AspRegion[],
  languageId: AspEmbeddedLanguage,
): string {
  const chunks: string[] = [];
  let cursor = owner.contentStart;
  for (const nested of nestedRegions) {
    if (nested === owner || nested.language === languageId || nested.end <= cursor) {
      continue;
    }
    if (cursor < nested.start) {
      chunks.push(sourceText.slice(cursor, nested.start));
    }
    chunks.push(nestedRegionMask(sourceText, owner, nested, languageId));
    cursor = nested.end;
  }
  if (cursor < owner.contentEnd) {
    chunks.push(sourceText.slice(cursor, owner.contentEnd));
  }
  return chunks.join("");
}

function nestedRegionMask(
  sourceText: string,
  owner: AspRegion,
  nested: AspRegion,
  languageId: AspEmbeddedLanguage,
): string {
  if (
    nested.kind !== "asp-block" &&
    nested.kind !== "asp-expression" &&
    nested.kind !== "asp-directive"
  ) {
    return preserveLineEndings(sourceText.slice(nested.start, nested.end), " ");
  }
  if (languageId === "css") {
    return preserveLineEndings(sourceText.slice(nested.start, nested.end), "x");
  }
  if (languageId === "javascript" || languageId === "jscript") {
    return javascriptAspMask(sourceText, owner, nested);
  }
  return preserveLineEndings(sourceText.slice(nested.start, nested.end), " ");
}

function javascriptAspMask(sourceText: string, owner: AspRegion, nested: AspRegion): string {
  if (
    nested.kind === "asp-block" &&
    !javascriptBlockNeedsValuePlaceholder(sourceText, owner, nested)
  ) {
    return preserveLineEndings(sourceText.slice(nested.start, nested.end), " ");
  }
  return firstValuePlaceholder(sourceText.slice(nested.start, nested.end), "0");
}

function javascriptBlockNeedsValuePlaceholder(
  sourceText: string,
  owner: AspRegion,
  nested: AspRegion,
): boolean {
  const previous = previousSignificantChar(sourceText, owner.contentStart, nested.start);
  return previous !== undefined && /=|\(|\[|,|:|\?|!|~|\+|-|\*|\/|%|&|\||\^|<|>/.test(previous);
}

function previousSignificantChar(
  sourceText: string,
  start: number,
  end: number,
): string | undefined {
  for (let index = end - 1; index >= start; index -= 1) {
    const char = sourceText[index];
    if (char && !/\s/.test(char)) {
      return char;
    }
  }
  return undefined;
}

function firstValuePlaceholder(text: string, valueChar: string): string {
  let placed = false;
  return text
    .split("")
    .map((char) => {
      if (char === "\n" || char === "\r") {
        return char;
      }
      if (!placed) {
        placed = true;
        return valueChar;
      }
      return " ";
    })
    .join("");
}

function preserveLineEndings(text: string, fill: string): string {
  return text.replace(/[^\r\n]/g, fill);
}

function buildMaskedDocument(
  uri: string,
  sourceText: string,
  languageId: AspEmbeddedLanguage,
  regions: AspRegion[],
): VirtualDocument {
  const htmlRanges = regions.map((region) => [region.contentStart, region.contentEnd] as const);
  const segments: SourceMapSegment[] = [];
  const chunks: string[] = [];
  let cursor = 0;
  for (const [start, end] of htmlRanges) {
    if (cursor < start) {
      chunks.push(preserveLineEndings(sourceText.slice(cursor, start), " "));
    }
    chunks.push(sourceText.slice(start, end));
    segments.push({ virtualStart: start, virtualEnd: end, sourceStart: start, sourceEnd: end });
    cursor = end;
  }
  if (cursor < sourceText.length) {
    chunks.push(preserveLineEndings(sourceText.slice(cursor), " "));
  }
  const text = chunks.join("");
  return {
    uri: `${uri}.${languageId}.virtual`,
    languageId,
    text,
    sourceMap: createSourceMap(sourceText, text, segments),
  };
}

export function createSourceMap(
  sourceText: string,
  virtualText: string,
  segments: SourceMapSegment[],
): SourceMap {
  const sourceSegments = [...segments].sort(
    (left, right) => left.sourceStart - right.sourceStart || left.sourceEnd - right.sourceEnd,
  );
  const map = {
    segments,
    toSourceOffset(offset: number): number | undefined {
      const segment = findSegmentContaining(segments, offset, "virtual");
      if (!segment) {
        return undefined;
      }
      return segment.sourceStart + (offset - segment.virtualStart);
    },
    toVirtualOffset(offset: number): number | undefined {
      const segment = findSegmentContaining(sourceSegments, offset, "source");
      if (!segment) {
        return undefined;
      }
      return segment.virtualStart + (offset - segment.sourceStart);
    },
    toSourcePosition(position: Position): Position | undefined {
      const offset = offsetAt(virtualText, position);
      const sourceOffset = map.toSourceOffset(offset);
      return sourceOffset === undefined ? undefined : positionAt(sourceText, sourceOffset);
    },
    toVirtualPosition(position: Position): Position | undefined {
      const offset = offsetAt(sourceText, position);
      const virtualOffset = map.toVirtualOffset(offset);
      return virtualOffset === undefined ? undefined : positionAt(virtualText, virtualOffset);
    },
  };
  return map;
}

function findSegmentContaining(
  segments: SourceMapSegment[],
  offset: number,
  axis: "source" | "virtual",
): SourceMapSegment | undefined {
  const startKey = axis === "source" ? "sourceStart" : "virtualStart";
  const endKey = axis === "source" ? "sourceEnd" : "virtualEnd";
  let low = 0;
  let high = segments.length - 1;
  while (low <= high) {
    const middle = Math.floor((low + high) / 2);
    const segment = segments[middle];
    if (offset < segment[startKey]) {
      high = middle - 1;
    } else if (offset > segment[endKey]) {
      low = middle + 1;
    } else {
      return segment;
    }
  }
  return undefined;
}
