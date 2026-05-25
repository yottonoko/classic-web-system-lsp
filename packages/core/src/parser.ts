import { DiagnosticSeverity } from "vscode-languageserver-types";
import type {
  AspCstNode,
  AspDirective,
  AspDocumentChange,
  AspInclude,
  AspParsedDocument,
  AspParsedDocumentUpdate,
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
  nextText: string,
  changes: AspDocumentChange[],
  settings: AspSettings = {},
): AspParsedDocumentUpdate {
  const fallback = (reason: string): AspParsedDocumentUpdate => ({
    parsed: parseAspDocument(previous.uri, nextText, settings),
    incremental: false,
    fallbackReason: reason,
  });
  if (changes.length !== 1) {
    return fallback("multiple changes");
  }
  const change = changes[0];
  if (!change.range) {
    return fallback("full text change");
  }
  const start = offsetFromRange(previous.text, change.range.start);
  const end = offsetFromRange(previous.text, change.range.end);
  if (!appliesChange(previous.text, nextText, start, end, change.text)) {
    return fallback("change text mismatch");
  }
  const delta = change.text.length - (end - start);
  const owner = editableRegionAt(previous.regions, start, end);
  if (!owner) {
    return fallback("change is outside a reusable region");
  }
  const unsafeReason = unsafeIncrementalChangeReason(previous, owner, start, end, change.text);
  if (unsafeReason) {
    return fallback(unsafeReason);
  }
  const shiftedRegions = previous.regions.map((region) =>
    shiftRegionForChange(region, owner, end, delta),
  );
  const shiftedDirectives = previous.directives.map((directive) =>
    shiftDirectiveForChange(previous.text, nextText, directive, end, delta),
  );
  const shiftedIncludes = previous.includes.map((include) =>
    shiftIncludeForChange(previous.text, nextText, include, end, delta),
  );
  const defaultLanguage = defaultLanguageFromDirectives(
    shiftedDirectives,
    settings.defaultLanguage ?? previous.defaultLanguage,
  );
  const children = [
    ...shiftedRegions.map((region) => regionToNode(nextText, region)),
    ...shiftedIncludes.map((include) => includeToNode(nextText, include)),
  ].sort(
    (left, right) => left.start - right.start || left.end - left.start - (right.end - right.start),
  );
  const root: AspCstNode = {
    kind: "Document",
    start: 0,
    end: nextText.length,
    contentStart: 0,
    contentEnd: nextText.length,
    text: nextText,
    tokens: children.flatMap((node) => node.tokens),
    children,
    errors: shiftParseErrors(previous, nextText, end, delta),
  };
  return {
    parsed: {
      uri: previous.uri,
      text: nextText,
      cst: root,
      regions: shiftedRegions,
      directives: shiftedDirectives,
      includes: shiftedIncludes,
      defaultLanguage,
      diagnostics: shiftDiagnostics(previous, nextText, end, delta),
    },
    incremental: true,
    change: {
      start,
      end,
      delta,
      language: owner.language,
      regionKind: owner.kind,
    },
  };
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

function appliesChange(
  previousText: string,
  nextText: string,
  start: number,
  end: number,
  inserted: string,
): boolean {
  if (nextText.length !== previousText.length - (end - start) + inserted.length) {
    return false;
  }
  if (!textMatchesAt(nextText, start, inserted)) {
    return false;
  }
  for (let index = 0; index < start; index += 1) {
    if (previousText.charCodeAt(index) !== nextText.charCodeAt(index)) {
      return false;
    }
  }
  const suffixLength = previousText.length - end;
  const nextSuffixStart = start + inserted.length;
  for (let index = 0; index < suffixLength; index += 1) {
    if (previousText.charCodeAt(end + index) !== nextText.charCodeAt(nextSuffixStart + index)) {
      return false;
    }
  }
  return true;
}

function textMatchesAt(text: string, offset: number, expected: string): boolean {
  for (let index = 0; index < expected.length; index += 1) {
    if (text.charCodeAt(offset + index) !== expected.charCodeAt(index)) {
      return false;
    }
  }
  return true;
}

function editableRegionAt(regions: AspRegion[], start: number, end: number): AspRegion | undefined {
  const candidates = regions
    .filter((region) => {
      if (region.kind === "asp-directive") {
        return false;
      }
      return start >= region.contentStart && end <= region.contentEnd;
    })
    .sort(
      (left, right) =>
        left.contentEnd - left.contentStart - (right.contentEnd - right.contentStart),
    );
  return candidates[0];
}

function unsafeIncrementalChangeReason(
  previous: AspParsedDocument,
  owner: AspRegion,
  start: number,
  end: number,
  inserted: string,
): string | undefined {
  const removed = previous.text.slice(start, end);
  const changedText = removed + inserted;
  if (
    previous.includes.some((include) =>
      changeTouchesRange(start, end, include.offset, includeEnd(previous.text, include)),
    )
  ) {
    return "include directive changed";
  }
  if (
    previous.regions.some(
      (region) =>
        region !== owner &&
        region.start < end &&
        region.end > start &&
        (start < region.contentStart || end > region.contentEnd),
    )
  ) {
    return "change overlaps a nested region boundary";
  }
  if (owner.kind === "html" && /<|>/.test(changedText)) {
    return "HTML structure may have changed";
  }
  if (
    (owner.kind === "style" || owner.kind === "client-script" || owner.kind === "server-script") &&
    (/<%|<!--|<\/\s*(script|style)\b/i.test(changedText) || /["'`/*]/.test(changedText))
  ) {
    return "embedded structure may have changed";
  }
  if (
    (owner.kind === "asp-block" || owner.kind === "asp-expression") &&
    (/<%|%>/.test(changedText) || /["'`/]/.test(changedText))
  ) {
    return "ASP delimiter may have changed";
  }
  if (owner.kind === "style-attribute" && /["'<>]/.test(changedText)) {
    return "style attribute boundary may have changed";
  }
  return undefined;
}

function changeTouchesRange(
  changeStart: number,
  changeEnd: number,
  rangeStart: number,
  rangeEnd: number,
) {
  return changeStart === changeEnd
    ? changeStart >= rangeStart && changeStart <= rangeEnd
    : changeStart < rangeEnd && rangeStart < changeEnd;
}

function includeEnd(text: string, include: AspInclude): number {
  return offsetFromRange(text, include.range.end);
}

function shiftRegionForChange(
  region: AspRegion,
  owner: AspRegion,
  changeEnd: number,
  delta: number,
): AspRegion {
  if (region === owner) {
    return { ...region, end: region.end + delta, contentEnd: region.contentEnd + delta };
  }
  if (region.start >= changeEnd) {
    return shiftRegion(region, delta);
  }
  if (region.end >= changeEnd && containsRegion(region, owner)) {
    return { ...region, end: region.end + delta, contentEnd: region.contentEnd + delta };
  }
  return region;
}

function containsRegion(container: AspRegion, child: AspRegion): boolean {
  return container.start <= child.start && container.end >= child.end;
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

function shiftDirectiveForChange(
  previousText: string,
  nextText: string,
  directive: AspDirective,
  changeEnd: number,
  delta: number,
): AspDirective {
  const start = shiftOffset(directive.offset, changeEnd, delta);
  const end = shiftOffset(offsetFromRange(previousText, directive.range.end), changeEnd, delta);
  return {
    ...directive,
    offset: start,
    range: rangeFromOffsets(nextText, start, end),
  };
}

function shiftIncludeForChange(
  previousText: string,
  nextText: string,
  include: AspInclude,
  changeEnd: number,
  delta: number,
): AspInclude {
  const start = shiftOffset(include.offset, changeEnd, delta);
  const end = shiftOffset(offsetFromRange(previousText, include.range.end), changeEnd, delta);
  return {
    ...include,
    offset: start,
    range: rangeFromOffsets(nextText, start, end),
    directiveRange: shiftRange(previousText, nextText, include.directiveRange, changeEnd, delta),
    modeRange: shiftRange(previousText, nextText, include.modeRange, changeEnd, delta),
    pathRange: shiftRange(previousText, nextText, include.pathRange, changeEnd, delta),
  };
}

function shiftRange(
  previousText: string,
  nextText: string,
  range: AspInclude["range"],
  changeEnd: number,
  delta: number,
) {
  const start = shiftOffset(offsetFromRange(previousText, range.start), changeEnd, delta);
  const end = shiftOffset(offsetFromRange(previousText, range.end), changeEnd, delta);
  return rangeFromOffsets(nextText, start, end);
}

function shiftOffset(offset: number, changeEnd: number, delta: number): number {
  return offset >= changeEnd ? offset + delta : offset;
}

function defaultLanguageFromDirectives(
  directives: AspDirective[],
  fallback: "VBScript" | "JScript",
): "VBScript" | "JScript" {
  const directiveLanguage = directives
    .map((directive) => directive.attributes.language ?? directive.attributes.LANGUAGE)
    .find((value): value is string => typeof value === "string");
  return normalizeScriptLanguage(directiveLanguage ?? fallback);
}

function shiftParseErrors(
  previous: AspParsedDocument,
  nextText: string,
  changeEnd: number,
  delta: number,
): NonNullable<AspCstNode["errors"]> {
  return (
    previous.cst.errors?.map((error) => ({
      ...error,
      start: shiftOffset(error.start, changeEnd, delta),
      end: shiftOffset(error.end, changeEnd, delta),
    })) ?? []
  );
}

function shiftDiagnostics(
  previous: AspParsedDocument,
  nextText: string,
  changeEnd: number,
  delta: number,
): AspParsedDocument["diagnostics"] {
  return previous.diagnostics.map((diagnostic) => {
    const start = shiftOffset(
      offsetFromRange(previous.text, diagnostic.range.start),
      changeEnd,
      delta,
    );
    const end = shiftOffset(offsetFromRange(previous.text, diagnostic.range.end), changeEnd, delta);
    return { ...diagnostic, range: rangeFromOffsets(nextText, start, end) };
  });
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
