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

  let text = "";
  const segments: SourceMapSegment[] = [];
  for (const region of regions) {
    const prefix =
      languageId === "css" ? (region.kind === "style-attribute" ? "__asp_lsp__{" : "\n") : "";
    const suffix = languageId === "css" && region.kind === "style-attribute" ? "}\n" : "\n";
    const start = text.length + prefix.length;
    const content = maskNestedRegions(sourceText, region, allRegions, languageId);
    text += prefix + content + suffix;
    segments.push({
      virtualStart: start,
      virtualEnd: start + content.length,
      sourceStart: region.contentStart,
      sourceEnd: region.contentEnd,
    });
  }
  return {
    uri: `${uri}.${languageId}.virtual`,
    languageId,
    text,
    sourceMap: createSourceMap(sourceText, text, segments),
  };
}

function maskNestedRegions(
  sourceText: string,
  owner: AspRegion,
  allRegions: AspRegion[],
  languageId: AspEmbeddedLanguage,
): string {
  const chars = sourceText.slice(owner.contentStart, owner.contentEnd).split("");
  for (const nested of allRegions) {
    if (
      nested === owner ||
      nested.language === languageId ||
      nested.start < owner.contentStart ||
      nested.end > owner.contentEnd
    ) {
      continue;
    }
    const start = Math.max(0, nested.start - owner.contentStart);
    const end = Math.min(chars.length, nested.end - owner.contentStart);
    const mask = nestedRegionMask(sourceText, owner, nested, languageId).split("");
    chars.splice(start, end - start, ...mask);
  }
  return chars.join("");
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
  return text
    .split("")
    .map((char) => (char === "\n" || char === "\r" ? char : fill))
    .join("");
}

function buildMaskedDocument(
  uri: string,
  sourceText: string,
  languageId: AspEmbeddedLanguage,
  regions: AspRegion[],
): VirtualDocument {
  const htmlRanges = regions.map((region) => [region.contentStart, region.contentEnd] as const);
  const chars = Array.from(sourceText);
  const segments: SourceMapSegment[] = [];
  let rangeIndex = 0;
  let current = htmlRanges[rangeIndex];
  for (let index = 0; index < sourceText.length; index += 1) {
    while (current && index >= current[1]) {
      rangeIndex += 1;
      current = htmlRanges[rangeIndex];
    }
    if (!current || index < current[0] || index >= current[1]) {
      chars[index] =
        sourceText[index] === "\n" || sourceText[index] === "\r" ? sourceText[index] : " ";
    }
  }
  for (const [start, end] of htmlRanges) {
    segments.push({ virtualStart: start, virtualEnd: end, sourceStart: start, sourceEnd: end });
  }
  const text = chars.join("");
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
  const map = {
    segments,
    toSourceOffset(offset: number): number | undefined {
      const segment = segments.find(
        (candidate) => offset >= candidate.virtualStart && offset <= candidate.virtualEnd,
      );
      if (!segment) {
        return undefined;
      }
      return segment.sourceStart + (offset - segment.virtualStart);
    },
    toVirtualOffset(offset: number): number | undefined {
      const segment = segments.find(
        (candidate) => offset >= candidate.sourceStart && offset <= candidate.sourceEnd,
      );
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
