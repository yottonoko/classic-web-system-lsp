import { DiagnosticSeverity } from "vscode-languageserver-types";
import type {
  AspCstNode,
  AspDirective,
  AspInclude,
  AspParsedDocument,
  AspRegion,
  AspSettings,
  AspToken,
} from "./types";
import { rangeFromOffsets } from "./position";
import { parseVbscriptCst } from "./vbscript";

const attrPattern = /([A-Za-z_:][-A-Za-z0-9_:.]*)\s*(?:=\s*("([^"]*)"|'([^']*)'|([^\s>]+)))?/g;

interface ParsedHtmlScan {
  inlineRegions: AspRegion[];
  tagRegions: AspRegion[];
  includes: AspInclude[];
}

interface HtmlTag {
  name: string;
  start: number;
  end: number;
  attributesStart: number;
  attributesEnd: number;
  attributes: Record<string, string | true>;
  attributeSpans: AttributeSpan[];
  closing: boolean;
  selfClosing: boolean;
}

interface AttributeSpan {
  name: string;
  value: string | true;
  valueStart: number;
  valueEnd: number;
}

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

export function parseAspCst(uri: string, text: string, settings: AspSettings = {}): AspCstNode {
  const diagnostics: AspParsedDocument["diagnostics"] = [];
  const scan = scanHtmlAndAsp(text, diagnostics);
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

function scanHtmlAndAsp(
  text: string,
  diagnostics: AspParsedDocument["diagnostics"],
): ParsedHtmlScan {
  const inlineRegions: AspRegion[] = [];
  const tagRegions: AspRegion[] = [];
  const includes: AspInclude[] = [];
  let cursor = 0;
  while (cursor < text.length) {
    if (text.startsWith("<%", cursor)) {
      const region = parseAspRegionAt(text, cursor, diagnostics);
      inlineRegions.push(region);
      cursor = region.end;
      continue;
    }
    if (text.startsWith("<!--", cursor)) {
      const commentEnd = text.indexOf("-->", cursor + 4);
      const end = commentEnd === -1 ? text.length : commentEnd + 3;
      const include = parseIncludeComment(text, cursor, end);
      if (include) {
        includes.push(include);
      }
      cursor = end;
      continue;
    }
    if (text[cursor] !== "<") {
      cursor += 1;
      continue;
    }
    const tag = readHtmlTag(text, cursor);
    if (!tag) {
      cursor += 1;
      continue;
    }
    if (!tag.closing) {
      tagRegions.push(...styleAttributeRegionsFromTag(tag));
    }
    if ((tag.name === "script" || tag.name === "style") && !tag.closing && !tag.selfClosing) {
      const close = findElementClose(text, tag.name, tag.end);
      if (close) {
        const region = elementRegionFromTag(tag, close);
        tagRegions.push(region);
        inlineRegions.push(...scanAspRegionsInRange(text, tag.end, close.start, diagnostics));
        cursor = close.end;
        continue;
      }
    }
    cursor = tag.end;
  }
  return { inlineRegions, tagRegions, includes };
}

function scanAspRegionsInRange(
  text: string,
  start: number,
  end: number,
  diagnostics: AspParsedDocument["diagnostics"],
): AspRegion[] {
  const regions: AspRegion[] = [];
  let cursor = start;
  while (cursor < end) {
    const next = text.indexOf("<%", cursor);
    if (next === -1 || next >= end) {
      break;
    }
    const region = parseAspRegionAt(text, next, diagnostics, end);
    regions.push(region);
    cursor = Math.max(region.end, next + 2);
  }
  return regions;
}

function parseAspRegionAt(
  text: string,
  start: number,
  diagnostics: AspParsedDocument["diagnostics"],
  maxEnd = text.length,
): AspRegion {
  const close = findAspClose(text, start + 2, maxEnd);
  if (close === -1) {
    diagnostics.push({
      severity: DiagnosticSeverity.Error,
      range: rangeFromOffsets(text, start, maxEnd),
      message: "Classic ASP block is missing a closing %> delimiter.",
      source: "asp-lsp",
    });
  }
  const marker = text[start + 2];
  const kind = marker === "=" ? "asp-expression" : marker === "@" ? "asp-directive" : "asp-block";
  const contentStart = start + (marker === "=" || marker === "@" ? 3 : 2);
  const contentEnd = close === -1 ? maxEnd : close;
  return {
    kind,
    language: kind === "asp-directive" ? "asp-directive" : "vbscript",
    start,
    end: close === -1 ? maxEnd : close + 2,
    contentStart,
    contentEnd,
  };
}

function findAspClose(text: string, offset: number, maxEnd: number): number {
  const vbClose = findAspCloseInMode(text, offset, maxEnd, "vbscript");
  const jsClose = findAspCloseInMode(text, offset, maxEnd, "jscript");
  if (vbClose === -1) {
    return jsClose;
  }
  if (jsClose === -1) {
    return vbClose;
  }
  return Math.min(vbClose, jsClose);
}

function findAspCloseInMode(
  text: string,
  offset: number,
  maxEnd: number,
  mode: "vbscript" | "jscript",
): number {
  let quote: string | undefined;
  let lineComment = false;
  let blockComment = false;
  for (let index = offset; index < maxEnd; index += 1) {
    const char = text[index];
    const next = text[index + 1];
    if (lineComment) {
      if (char === "\r" || char === "\n") {
        lineComment = false;
      }
      continue;
    }
    if (blockComment) {
      if (char === "*" && next === "/") {
        blockComment = false;
        index += 1;
      }
      continue;
    }
    if (quote) {
      if (mode === "jscript" && quote === "'" && (char === "\r" || char === "\n")) {
        quote = undefined;
        continue;
      }
      if (mode === "jscript" && char === "\\") {
        index += 1;
        continue;
      }
      if (char === quote) {
        if (quote === '"' && next === '"') {
          index += 1;
          continue;
        }
        quote = undefined;
      }
      continue;
    }
    if (char === "%") {
      if (next === ">") {
        return index;
      }
      continue;
    }
    if (char === '"' || char === "'" || char === "`") {
      if (mode === "vbscript" && char === "'") {
        lineComment = true;
      } else {
        quote = char;
      }
      continue;
    }
    if (char === "/" && next === "/") {
      lineComment = true;
      index += 1;
      continue;
    }
    if (char === "/" && next === "*") {
      blockComment = true;
      index += 1;
    }
  }
  return -1;
}

function readHtmlTag(text: string, start: number): HtmlTag | undefined {
  if (text[start] !== "<" || text.startsWith("<!--", start) || text[start + 1] === "%") {
    return undefined;
  }
  let cursor = start + 1;
  const closing = text[cursor] === "/";
  if (closing) {
    cursor += 1;
  }
  while (cursor < text.length && /\s/.test(text[cursor])) {
    cursor += 1;
  }
  const nameStart = cursor;
  if (!/[A-Za-z]/.test(text[cursor] ?? "")) {
    return undefined;
  }
  cursor += 1;
  while (cursor < text.length && /[A-Za-z0-9:_-]/.test(text[cursor])) {
    cursor += 1;
  }
  const name = text.slice(nameStart, cursor).toLowerCase();
  const tagEnd = findTagEnd(text, cursor);
  if (tagEnd === -1) {
    return undefined;
  }
  const attributesStart = cursor;
  const attributesEnd = tagEnd;
  const attributeSpans = parseAttributeSpans(text, attributesStart, attributesEnd);
  const attributes: Record<string, string | true> = {};
  for (const attribute of attributeSpans) {
    attributes[attribute.name] = attribute.value;
    attributes[attribute.name.toLowerCase()] = attribute.value;
  }
  return {
    name,
    start,
    end: tagEnd + 1,
    attributesStart,
    attributesEnd,
    attributes,
    attributeSpans,
    closing,
    selfClosing: text.slice(attributesStart, attributesEnd).trimEnd().endsWith("/"),
  };
}

function findTagEnd(text: string, offset: number): number {
  let quote: string | undefined;
  for (let index = offset; index < text.length; index += 1) {
    const char = text[index];
    if (quote) {
      if (char === quote) {
        quote = undefined;
      }
      continue;
    }
    if (char === '"' || char === "'") {
      quote = char;
      continue;
    }
    if (char === ">") {
      return index;
    }
  }
  return -1;
}

function parseAttributeSpans(text: string, start: number, end: number): AttributeSpan[] {
  const attributes: AttributeSpan[] = [];
  let cursor = start;
  while (cursor < end) {
    while (cursor < end && /[\s/]/.test(text[cursor])) {
      cursor += 1;
    }
    const nameStart = cursor;
    if (!/[A-Za-z_:]/.test(text[cursor] ?? "")) {
      cursor += 1;
      continue;
    }
    cursor += 1;
    while (cursor < end && /[-A-Za-z0-9_:.]/.test(text[cursor])) {
      cursor += 1;
    }
    const name = text.slice(nameStart, cursor);
    while (cursor < end && /\s/.test(text[cursor])) {
      cursor += 1;
    }
    if (text[cursor] !== "=") {
      attributes.push({ name, value: true, valueStart: cursor, valueEnd: cursor });
      continue;
    }
    cursor += 1;
    while (cursor < end && /\s/.test(text[cursor])) {
      cursor += 1;
    }
    const quote = text[cursor] === '"' || text[cursor] === "'" ? text[cursor] : undefined;
    const valueStart = quote ? cursor + 1 : cursor;
    if (quote) {
      cursor += 1;
      while (cursor < end && text[cursor] !== quote) {
        cursor += 1;
      }
      const valueEnd = cursor;
      if (cursor < end) {
        cursor += 1;
      }
      attributes.push({ name, value: text.slice(valueStart, valueEnd), valueStart, valueEnd });
      continue;
    }
    while (cursor < end && !/[\s>]/.test(text[cursor])) {
      cursor += 1;
    }
    attributes.push({ name, value: text.slice(valueStart, cursor), valueStart, valueEnd: cursor });
  }
  return attributes;
}

function styleAttributeRegionsFromTag(tag: HtmlTag): AspRegion[] {
  return tag.attributeSpans
    .filter(
      (attribute) =>
        attribute.name.toLowerCase() === "style" && typeof attribute.value === "string",
    )
    .map((attribute) => ({
      kind: "style-attribute" as const,
      language: "css" as const,
      start: attribute.valueStart,
      end: attribute.valueEnd,
      contentStart: attribute.valueStart,
      contentEnd: attribute.valueEnd,
      attributes: { tagName: tag.name },
    }));
}

function elementRegionFromTag(tag: HtmlTag, close: { start: number; end: number }): AspRegion {
  if (tag.name === "style") {
    return {
      kind: "style",
      language: "css",
      start: tag.start,
      end: close.end,
      contentStart: tag.end,
      contentEnd: close.start,
      attributes: tag.attributes,
    };
  }
  const runatServer = String(tag.attributes.runat ?? "").toLowerCase() === "server";
  const language = String(tag.attributes.language ?? tag.attributes.type ?? "");
  return {
    kind: runatServer ? "server-script" : "client-script",
    language: runatServer
      ? normalizeScriptLanguage(language || "VBScript").toLowerCase() === "jscript"
        ? "jscript"
        : "vbscript"
      : "javascript",
    start: tag.start,
    end: close.end,
    contentStart: tag.end,
    contentEnd: close.start,
    attributes: tag.attributes,
  };
}

function findElementClose(
  text: string,
  tagName: "script" | "style",
  offset: number,
): { start: number; end: number } | undefined {
  return tagName === "script"
    ? findScriptClose(text, tagName, offset)
    : findStyleClose(text, tagName, offset);
}

function findScriptClose(
  text: string,
  tagName: string,
  offset: number,
): { start: number; end: number } | undefined {
  let quote: string | undefined;
  let lineComment = false;
  let blockComment = false;
  for (let index = offset; index < text.length; index += 1) {
    const char = text[index];
    const next = text[index + 1];
    if (lineComment) {
      if (char === "\r" || char === "\n") {
        lineComment = false;
      }
      continue;
    }
    if (blockComment) {
      if (char === "*" && next === "/") {
        blockComment = false;
        index += 1;
      }
      continue;
    }
    if (quote) {
      if (char === "\\" && quote !== "`") {
        index += 1;
        continue;
      }
      if (char === quote) {
        quote = undefined;
      }
      continue;
    }
    if (isClosingTagAt(text, index, tagName)) {
      const closeEnd = findTagEnd(text, index + 2);
      return closeEnd === -1 ? undefined : { start: index, end: closeEnd + 1 };
    }
    if (char === '"' || char === "'" || char === "`") {
      quote = char;
      continue;
    }
    if (char === "/" && next === "/") {
      lineComment = true;
      index += 1;
      continue;
    }
    if (char === "/" && next === "*") {
      blockComment = true;
      index += 1;
    }
  }
  return undefined;
}

function findStyleClose(
  text: string,
  tagName: string,
  offset: number,
): { start: number; end: number } | undefined {
  let quote: string | undefined;
  let blockComment = false;
  for (let index = offset; index < text.length; index += 1) {
    const char = text[index];
    const next = text[index + 1];
    if (blockComment) {
      if (char === "*" && next === "/") {
        blockComment = false;
        index += 1;
      }
      continue;
    }
    if (quote) {
      if (char === "\\") {
        index += 1;
        continue;
      }
      if (char === quote) {
        quote = undefined;
      }
      continue;
    }
    if (isClosingTagAt(text, index, tagName)) {
      const closeEnd = findTagEnd(text, index + 2);
      return closeEnd === -1 ? undefined : { start: index, end: closeEnd + 1 };
    }
    if (char === '"' || char === "'") {
      quote = char;
      continue;
    }
    if (char === "/" && next === "*") {
      blockComment = true;
      index += 1;
    }
  }
  return undefined;
}

function isClosingTagAt(text: string, index: number, tagName: string): boolean {
  if (text[index] !== "<" || text[index + 1] !== "/") {
    return false;
  }
  const candidate = text.slice(index + 2, index + 2 + tagName.length);
  const next = text[index + 2 + tagName.length];
  return candidate.toLowerCase() === tagName && (next === ">" || /\s/.test(next ?? ""));
}

function parseIncludeComment(text: string, start: number, end: number): AspInclude | undefined {
  const body = text.slice(start + 4, Math.max(start + 4, end - 3)).trim();
  if (!body.toLowerCase().startsWith("#include")) {
    return undefined;
  }
  const attributes = parseAttributes(body.slice("#include".length));
  const mode = attributes.virtual ? "virtual" : attributes.file ? "file" : undefined;
  const includePath = mode ? attributes[mode] : undefined;
  return mode && typeof includePath === "string"
    ? {
        offset: start,
        range: rangeFromOffsets(text, start, end),
        mode,
        path: includePath,
      }
    : undefined;
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
  let line = 0;
  let character = 0;
  for (let index = 0; index < text.length; index += 1) {
    if (line === position.line && character === position.character) {
      return index;
    }
    if (text[index] === "\n") {
      line += 1;
      character = 0;
    } else {
      character += 1;
    }
  }
  return text.length;
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

export function parseAttributes(text: string): Record<string, string | true> {
  const attributes: Record<string, string | true> = {};
  let match: RegExpExecArray | null;
  attrPattern.lastIndex = 0;
  while ((match = attrPattern.exec(text)) !== null) {
    const name = match[1];
    const value = match[3] ?? match[4] ?? match[5] ?? true;
    attributes[name] = value;
    attributes[name.toLowerCase()] = value;
  }
  return attributes;
}

export function normalizeScriptLanguage(language: string): "VBScript" | "JScript" {
  return /^(java|j)script$/i.test(language) ? "JScript" : "VBScript";
}
