import { DiagnosticSeverity } from "vscode-languageserver-types";
import type {
  AspInclude,
  AspParsedDocument,
  AspRegion,
  AspServerObject,
  AspSettings,
} from "./types";
import { rangeFromOffsets } from "./position";
import { createLocalizer } from "./localize";

const attrPattern = /([A-Za-z_:][-A-Za-z0-9_:.]*)\s*(?:=\s*("([^"]*)"|'([^']*)'|([^\s>]+)))?/g;

export interface AspHtmlScan {
  inlineRegions: AspRegion[];
  tagRegions: AspRegion[];
  includes: AspInclude[];
  serverObjects: AspServerObject[];
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

interface ReadHtmlTagOptions {
  allowIncomplete?: boolean;
}

export function extractAspIncludeRefs(text: string): AspInclude[] {
  const includes: AspInclude[] = [];
  const specialPattern = /<!--|<%|<\/?script\b|<\/?style\b/gi;
  let match: RegExpExecArray | null;
  while ((match = specialPattern.exec(text)) !== null) {
    const start = match.index;
    if (isInsideHtmlTagAt(text, start)) {
      continue;
    }
    const token = match[0].toLowerCase();
    if (token === "<%") {
      const close = findAspClose(text, start + 2, text.length);
      specialPattern.lastIndex = close === -1 ? text.length : close + 2;
      continue;
    }
    if (token === "<!--") {
      const commentEnd = text.indexOf("-->", start + 4);
      const end = commentEnd === -1 ? text.length : commentEnd + 3;
      const include = parseIncludeComment(text, start, end);
      if (include) {
        includes.push(include);
      }
      specialPattern.lastIndex = end;
      continue;
    }
    const tag = readHtmlTag(text, start);
    if (!tag) {
      continue;
    }
    if ((tag.name === "script" || tag.name === "style") && !tag.closing && !tag.selfClosing) {
      const close = findElementClose(text, tag.name, tag.end);
      if (close) {
        specialPattern.lastIndex = close.end;
      }
    }
  }
  return includes;
}

function isInsideHtmlTagAt(text: string, index: number): boolean {
  return containingHtmlTagStartAt(text, index) !== undefined;
}

function containingHtmlTagStartAt(text: string, index: number): number | undefined {
  if (index <= 0) {
    return undefined;
  }
  const tagStart = text.lastIndexOf("<", index - 1);
  if (tagStart === -1 || text.startsWith("<!--", tagStart) || text.startsWith("<%", tagStart)) {
    return undefined;
  }
  const tagEnd = text.lastIndexOf(">", index - 1);
  return tagStart > tagEnd ? tagStart : undefined;
}

export function scanHtmlAndAsp(
  text: string,
  diagnostics: AspParsedDocument["diagnostics"],
  settings: AspSettings,
): AspHtmlScan {
  const inlineRegions: AspRegion[] = [];
  const tagRegions: AspRegion[] = [];
  const includes: AspInclude[] = [];
  const serverObjects: AspServerObject[] = [];
  let cursor = 0;
  while (cursor < text.length) {
    if (text.startsWith("<%", cursor)) {
      const region = parseAspRegionAt(text, cursor, diagnostics, text.length, settings);
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
    const tag = readHtmlTag(text, cursor, { allowIncomplete: true });
    if (!tag) {
      cursor += 1;
      continue;
    }
    if (!tag.closing) {
      const styleAttributeRegions = styleAttributeRegionsFromTag(tag);
      tagRegions.push(...styleAttributeRegions);
      const serverObject = serverObjectFromTag(text, tag);
      if (serverObject) {
        serverObjects.push(serverObject);
      }
      inlineRegions.push(
        ...scanAspRegionsInRange(
          text,
          tag.attributesStart,
          tag.attributesEnd,
          diagnostics,
          settings,
        ),
      );
    }
    if ((tag.name === "script" || tag.name === "style") && !tag.closing && !tag.selfClosing) {
      const close = findElementClose(text, tag.name, tag.end);
      if (close) {
        const region = elementRegionFromTag(tag, close);
        tagRegions.push(region);
        inlineRegions.push(
          ...scanAspRegionsInRange(
            text,
            tag.end,
            close.start,
            diagnostics,
            settings,
            tag.name === "script" ? "javascript" : "css",
          ),
        );
        cursor = close.end;
        continue;
      }
    }
    cursor = tag.end;
  }
  return { inlineRegions, tagRegions, includes, serverObjects };
}

export function scanVbscriptIndexInput(
  text: string,
  diagnostics: AspParsedDocument["diagnostics"],
  settings: AspSettings,
): AspHtmlScan {
  const inlineRegions: AspRegion[] = [];
  const tagRegions: AspRegion[] = [];
  const includes: AspInclude[] = [];
  const serverObjects: AspServerObject[] = [];
  const specialPattern = /<!--|<%|<\s*(?:script|style|object)\b/gi;
  let match: RegExpExecArray | null;
  while ((match = specialPattern.exec(text)) !== null) {
    const start = match.index;
    const containingTagStart = containingHtmlTagStartAt(text, start);
    if (containingTagStart !== undefined) {
      const tag = readHtmlTag(text, containingTagStart);
      if (tag && tag.end > start) {
        addVbscriptIndexTagInput(text, tag, diagnostics, settings, inlineRegions, serverObjects);
        specialPattern.lastIndex = tag.end;
      }
      continue;
    }

    const token = match[0].toLowerCase();
    if (token === "<%") {
      const region = parseAspRegionAt(text, start, diagnostics, text.length, settings);
      inlineRegions.push(region);
      specialPattern.lastIndex = region.end;
      continue;
    }
    if (token === "<!--") {
      const commentEnd = text.indexOf("-->", start + 4);
      const end = commentEnd === -1 ? text.length : commentEnd + 3;
      const include = parseIncludeComment(text, start, end);
      if (include) {
        includes.push(include);
      }
      specialPattern.lastIndex = end;
      continue;
    }

    const tag = readHtmlTag(text, start);
    if (!tag) {
      continue;
    }
    addVbscriptIndexTagInput(text, tag, diagnostics, settings, inlineRegions, serverObjects);
    if ((tag.name === "script" || tag.name === "style") && !tag.closing && !tag.selfClosing) {
      const close = findElementClose(text, tag.name, tag.end);
      if (close) {
        const region = elementRegionFromTag(tag, close);
        if (region.kind === "server-script") {
          tagRegions.push(region);
        }
        inlineRegions.push(
          ...scanAspRegionsInRange(
            text,
            tag.end,
            close.start,
            diagnostics,
            settings,
            tag.name === "script" ? "javascript" : "css",
          ),
        );
        specialPattern.lastIndex = close.end;
        continue;
      }
    }
    specialPattern.lastIndex = tag.end;
  }
  return { inlineRegions, tagRegions, includes, serverObjects };
}

function addVbscriptIndexTagInput(
  text: string,
  tag: HtmlTag,
  diagnostics: AspParsedDocument["diagnostics"],
  settings: AspSettings,
  inlineRegions: AspRegion[],
  serverObjects: AspServerObject[],
): void {
  if (tag.closing) {
    return;
  }
  const serverObject = serverObjectFromTag(text, tag);
  if (serverObject) {
    serverObjects.push(serverObject);
  }
  inlineRegions.push(
    ...scanAspRegionsInRange(text, tag.attributesStart, tag.attributesEnd, diagnostics, settings),
  );
}

function scanAspRegionsInRange(
  text: string,
  start: number,
  end: number,
  diagnostics: AspParsedDocument["diagnostics"],
  settings: AspSettings = {},
  embeddedLanguage?: "javascript" | "css",
): AspRegion[] {
  const regions: AspRegion[] = [];
  let cursor = start;
  while (cursor < end) {
    const next = findAspOpenInRange(text, cursor, end, embeddedLanguage);
    if (next === -1 || next >= end) {
      break;
    }
    if (embeddedLanguage) {
      const state = embeddedContentStateAt(text, start, next, embeddedLanguage);
      if (state.kind !== "normal") {
        const close = findAspClose(text, next + 2, end);
        const stateEnd = embeddedContentStateEnd(text, next, end, embeddedLanguage, state);
        if (close === -1 || close >= stateEnd) {
          cursor = next + 2;
          continue;
        }
      }
    }
    const region = parseAspRegionAt(text, next, diagnostics, end, settings);
    regions.push(region);
    cursor = Math.max(region.end, next + 2);
  }
  return regions;
}

type EmbeddedContentState =
  | { kind: "normal" }
  | { kind: "string"; quote: string; escaped: boolean }
  | { kind: "lineComment" }
  | { kind: "blockComment" };

function findAspOpenInRange(
  text: string,
  start: number,
  end: number,
  embeddedLanguage?: "javascript" | "css",
): number {
  if (!embeddedLanguage) {
    const next = text.indexOf("<%", start);
    return next === -1 || next >= end ? -1 : next;
  }
  for (let index = start; index < end; index += 1) {
    if (text.startsWith("<%", index)) {
      return index;
    }
  }
  return -1;
}

function embeddedContentStateAt(
  text: string,
  start: number,
  offset: number,
  embeddedLanguage: "javascript" | "css",
): EmbeddedContentState {
  let state: EmbeddedContentState = { kind: "normal" };
  for (let index = start; index < offset; index += 1) {
    const aspEnd = embeddedAspRegionEndAt(text, index, offset, state);
    if (aspEnd !== undefined) {
      index = aspEnd - 1;
      continue;
    }
    state = advanceEmbeddedContentState(text, index, offset, embeddedLanguage, state);
  }
  return state;
}

function embeddedContentStateEnd(
  text: string,
  start: number,
  end: number,
  embeddedLanguage: "javascript" | "css",
  state: EmbeddedContentState,
): number {
  let current = state;
  for (let index = start; index < end; index += 1) {
    const aspEnd = embeddedAspRegionEndAt(text, index, end, current);
    if (aspEnd !== undefined) {
      index = aspEnd - 1;
      continue;
    }
    current = advanceEmbeddedContentState(text, index, end, embeddedLanguage, current);
    if (current.kind === "normal") {
      return index + 1;
    }
  }
  return end;
}

function embeddedAspRegionEndAt(
  text: string,
  start: number,
  end: number,
  state: EmbeddedContentState,
): number | undefined {
  if (!text.startsWith("<%", start)) {
    return undefined;
  }
  const close = findAspClose(text, start + 2, embeddedAspCloseSearchEnd(text, start, end, state));
  return close === -1 ? undefined : close + 2;
}

function embeddedAspCloseSearchEnd(
  text: string,
  start: number,
  end: number,
  state: EmbeddedContentState,
): number {
  if (state.kind === "lineComment") {
    return lineEndOffset(text, start, end);
  }
  if (state.kind === "blockComment") {
    const commentEnd = text.indexOf("*/", start + 2);
    return commentEnd === -1 || commentEnd > end ? end : commentEnd;
  }
  if (state.kind === "string") {
    return lineEndOffset(text, start, end);
  }
  return end;
}

function lineEndOffset(text: string, start: number, end: number): number {
  for (let index = start; index < end; index += 1) {
    const char = text[index];
    if (char === "\n" || char === "\r") {
      return index;
    }
  }
  return end;
}

function advanceEmbeddedContentState(
  text: string,
  index: number,
  end: number,
  embeddedLanguage: "javascript" | "css",
  state: EmbeddedContentState,
): EmbeddedContentState {
  const char = text[index];
  if (state.kind === "lineComment") {
    return char === "\n" || char === "\r" ? { kind: "normal" } : state;
  }
  if (state.kind === "blockComment") {
    return char === "*" && index + 1 < end && text[index + 1] === "/" ? { kind: "normal" } : state;
  }
  if (state.kind === "string") {
    if (state.escaped) {
      return { ...state, escaped: false };
    }
    if (char === "\\") {
      return { ...state, escaped: true };
    }
    return char === state.quote ? { kind: "normal" } : state;
  }
  if (char === '"' || char === "'" || (embeddedLanguage === "javascript" && char === "`")) {
    return { kind: "string", quote: char, escaped: false };
  }
  if (char === "/" && index + 1 < end) {
    const next = text[index + 1];
    if (next === "*") {
      return { kind: "blockComment" };
    }
    if (embeddedLanguage === "javascript" && next === "/") {
      return { kind: "lineComment" };
    }
  }
  return state;
}

function parseAspRegionAt(
  text: string,
  start: number,
  diagnostics: AspParsedDocument["diagnostics"],
  maxEnd = text.length,
  settings: AspSettings = {},
): AspRegion {
  const close = findAspClose(text, start + 2, maxEnd);
  if (close === -1) {
    diagnostics.push({
      severity: DiagnosticSeverity.Error,
      range: rangeFromOffsets(text, start, maxEnd),
      message: createLocalizer(settings.resolvedLocale).t("parser.missingAspClose"),
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
  for (let index = offset; index < maxEnd; index += 1) {
    if (text[index] === "%" && text[index + 1] === ">") {
      return index;
    }
  }
  return -1;
}

function readHtmlTag(
  text: string,
  start: number,
  options: ReadHtmlTagOptions = {},
): HtmlTag | undefined {
  if (text[start] !== "<" || text.startsWith("<!--", start) || text[start + 1] === "%") {
    return undefined;
  }
  let cursor = start + 1;
  const closing = text[cursor] === "/";
  if (closing) {
    cursor += 1;
  }
  while (cursor < text.length && isHtmlWhitespaceCode(text.charCodeAt(cursor))) {
    cursor += 1;
  }
  const nameStart = cursor;
  if (!isAsciiAlphaCode(text.charCodeAt(cursor))) {
    return undefined;
  }
  cursor += 1;
  while (cursor < text.length && isHtmlTagNamePartCode(text.charCodeAt(cursor))) {
    cursor += 1;
  }
  const name = text.slice(nameStart, cursor).toLowerCase();
  const tagEnd = findTagEnd(text, cursor);
  if (tagEnd === -1 && !options.allowIncomplete) {
    return undefined;
  }
  const attributesEnd = tagEnd === -1 ? text.length : tagEnd;
  const attributesStart = cursor;
  const attributeSpans = parseAttributeSpans(text, attributesStart, attributesEnd);
  const attributes: Record<string, string | true> = {};
  for (const attribute of attributeSpans) {
    attributes[attribute.name] = attribute.value;
    attributes[attribute.name.toLowerCase()] = attribute.value;
  }
  return {
    name,
    start,
    end: tagEnd === -1 ? text.length : tagEnd + 1,
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
      if (text.startsWith("<%", index)) {
        const close = findAspClose(text, index + 2, text.length);
        if (close === -1) {
          return -1;
        }
        index = close + 1;
        continue;
      }
      if (char === quote) {
        quote = undefined;
      }
      continue;
    }
    if (char === '"' || char === "'") {
      quote = char;
      continue;
    }
    if (text.startsWith("<%", index)) {
      const close = findAspClose(text, index + 2, text.length);
      if (close === -1) {
        return -1;
      }
      index = close + 1;
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
    while (cursor < end) {
      const code = text.charCodeAt(cursor);
      if (code !== 47 && !isHtmlWhitespaceCode(code)) {
        break;
      }
      cursor += 1;
    }
    if (text.startsWith("<%", cursor)) {
      const close = findAspClose(text, cursor + 2, end);
      cursor = close === -1 ? end : close + 2;
      continue;
    }
    const nameStart = cursor;
    if (!isAttributeNameStartCode(text.charCodeAt(cursor))) {
      cursor += 1;
      continue;
    }
    cursor += 1;
    while (cursor < end && isAttributeNamePartCode(text.charCodeAt(cursor))) {
      cursor += 1;
    }
    const name = text.slice(nameStart, cursor);
    while (cursor < end && isHtmlWhitespaceCode(text.charCodeAt(cursor))) {
      cursor += 1;
    }
    if (text[cursor] !== "=") {
      attributes.push({ name, value: true, valueStart: cursor, valueEnd: cursor });
      continue;
    }
    cursor += 1;
    while (cursor < end && isHtmlWhitespaceCode(text.charCodeAt(cursor))) {
      cursor += 1;
    }
    const quote = text[cursor] === '"' || text[cursor] === "'" ? text[cursor] : undefined;
    const valueStart = quote ? cursor + 1 : cursor;
    if (quote) {
      cursor += 1;
      while (cursor < end) {
        if (text.startsWith("<%", cursor)) {
          const close = findAspClose(text, cursor + 2, end);
          cursor = close === -1 ? end : close + 2;
          continue;
        }
        if (text[cursor] === quote) {
          break;
        }
        cursor += 1;
      }
      const valueEnd = cursor;
      if (cursor < end) {
        cursor += 1;
      }
      attributes.push({ name, value: text.slice(valueStart, valueEnd), valueStart, valueEnd });
      continue;
    }
    while (cursor < end) {
      if (text.startsWith("<%", cursor)) {
        const close = findAspClose(text, cursor + 2, end);
        cursor = close === -1 ? end : close + 2;
        continue;
      }
      const code = text.charCodeAt(cursor);
      if (code === 62 || isHtmlWhitespaceCode(code)) {
        break;
      }
      cursor += 1;
    }
    attributes.push({ name, value: text.slice(valueStart, cursor), valueStart, valueEnd: cursor });
  }
  return attributes;
}

function isHtmlWhitespaceCode(code: number): boolean {
  return (
    code === 9 ||
    code === 10 ||
    code === 11 ||
    code === 12 ||
    code === 13 ||
    code === 32 ||
    code === 160 ||
    code === 5760 ||
    (code >= 8192 && code <= 8202) ||
    code === 8232 ||
    code === 8233 ||
    code === 8239 ||
    code === 8287 ||
    code === 12288 ||
    code === 65279
  );
}

function isAsciiAlphaCode(code: number): boolean {
  return (code >= 65 && code <= 90) || (code >= 97 && code <= 122);
}

function isAsciiDigitCode(code: number): boolean {
  return code >= 48 && code <= 57;
}

function isHtmlTagNamePartCode(code: number): boolean {
  return (
    isAsciiAlphaCode(code) || isAsciiDigitCode(code) || code === 58 || code === 95 || code === 45
  );
}

function isAttributeNameStartCode(code: number): boolean {
  return isAsciiAlphaCode(code) || code === 95 || code === 58;
}

function isAttributeNamePartCode(code: number): boolean {
  return (
    isAsciiAlphaCode(code) ||
    isAsciiDigitCode(code) ||
    code === 45 ||
    code === 95 ||
    code === 58 ||
    code === 46
  );
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

function serverObjectFromTag(text: string, tag: HtmlTag): AspServerObject | undefined {
  if (tag.name !== "object" || String(tag.attributes.runat ?? "").toLowerCase() !== "server") {
    return undefined;
  }
  const id = attributeStringValue(tag, "id");
  const idSpan = attributeSpanByName(tag, "id");
  if (!id || !idSpan || idSpan.value === true) {
    return undefined;
  }
  const progId = attributeStringValue(tag, "progid");
  const progIdSpan = attributeSpanByName(tag, "progid");
  const classId = attributeStringValue(tag, "classid");
  const classIdSpan = attributeSpanByName(tag, "classid");
  return {
    range: rangeFromOffsets(text, tag.start, tag.end),
    offset: tag.start,
    id,
    idRange: rangeFromOffsets(text, idSpan.valueStart, idSpan.valueEnd),
    progId,
    progIdRange:
      progId && progIdSpan && progIdSpan.value !== true
        ? rangeFromOffsets(text, progIdSpan.valueStart, progIdSpan.valueEnd)
        : undefined,
    classId,
    classIdRange:
      classId && classIdSpan && classIdSpan.value !== true
        ? rangeFromOffsets(text, classIdSpan.valueStart, classIdSpan.valueEnd)
        : undefined,
    attributes: tag.attributes,
  };
}

function attributeStringValue(tag: HtmlTag, name: string): string | undefined {
  const value = attributeSpanByName(tag, name)?.value;
  return typeof value === "string" && value.length > 0 ? value : undefined;
}

function attributeSpanByName(tag: HtmlTag, name: string): AttributeSpan | undefined {
  const lowerName = name.toLowerCase();
  return tag.attributeSpans.find((attribute) => attribute.name.toLowerCase() === lowerName);
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
  const embeddedLanguage = tagName === "script" ? "javascript" : "css";
  let state: EmbeddedContentState = { kind: "normal" };
  for (let index = offset; index < text.length; index += 1) {
    if (isClosingTagAt(text, index, tagName)) {
      const closeEnd = findTagEnd(text, index + 2);
      return closeEnd === -1 ? undefined : { start: index, end: closeEnd + 1 };
    }
    if (text.startsWith("<%", index)) {
      const close = findAspClose(text, index + 2, text.length);
      if (state.kind !== "normal") {
        const stateEnd = embeddedContentStateEnd(text, index, text.length, embeddedLanguage, state);
        if (close === -1 || close >= stateEnd) {
          state = advanceEmbeddedContentState(text, index, text.length, embeddedLanguage, state);
          continue;
        }
      }
      if (close === -1) {
        return undefined;
      }
      if (state.kind !== "normal") {
        state = advanceEmbeddedContentState(text, index, text.length, embeddedLanguage, state);
      }
      index = close + 1;
      continue;
    }
    state = advanceEmbeddedContentState(text, index, text.length, embeddedLanguage, state);
  }
  return undefined;
}

function isClosingTagAt(text: string, index: number, tagName: string): boolean {
  if (text[index] !== "<" || text[index + 1] !== "/") {
    return false;
  }
  const candidate = text.slice(index + 2, index + 2 + tagName.length);
  const next = text.charCodeAt(index + 2 + tagName.length);
  return candidate.toLowerCase() === tagName && (next === 62 || isHtmlWhitespaceCode(next));
}

function parseIncludeComment(text: string, start: number, end: number): AspInclude | undefined {
  const contentStart = start + 4;
  const contentEnd = Math.max(contentStart, end - 3);
  const commentBody = text.slice(contentStart, contentEnd);
  const leadingWhitespace = commentBody.match(/^\s*/)?.[0].length ?? 0;
  const bodyStart = contentStart + leadingWhitespace;
  const body = commentBody.slice(leadingWhitespace).trimEnd();
  if (!body.toLowerCase().startsWith("#include")) {
    return undefined;
  }
  const directiveStart = bodyStart;
  const directiveEnd = directiveStart + "#include".length;
  const attributeTextStart = directiveEnd;
  const attributeText = text.slice(attributeTextStart, contentEnd);
  let file: Pick<AspInclude, "path" | "modeRange" | "pathRange"> | undefined;
  let virtual: Pick<AspInclude, "path" | "modeRange" | "pathRange"> | undefined;
  let match: RegExpExecArray | null;
  attrPattern.lastIndex = 0;
  while ((match = attrPattern.exec(attributeText)) !== null) {
    const mode = match[1].toLowerCase();
    if (mode !== "file" && mode !== "virtual") {
      continue;
    }
    const value = match[3] ?? match[4] ?? match[5];
    const rawValue = match[2];
    if (!value || !rawValue) {
      continue;
    }
    const nameStart = attributeTextStart + match.index;
    const rawValueStart = attributeTextStart + match.index + match[0].indexOf(rawValue);
    const candidate = {
      path: value,
      modeRange: rangeFromOffsets(text, nameStart, nameStart + match[1].length),
      pathRange: rangeFromOffsets(text, rawValueStart, rawValueStart + rawValue.length),
    };
    if (mode === "file") {
      file = candidate;
    } else {
      virtual = candidate;
    }
  }
  const chosenMode = virtual ? "virtual" : file ? "file" : undefined;
  const chosen = chosenMode === "virtual" ? virtual : file;
  return chosenMode && chosen
    ? {
        offset: start,
        range: rangeFromOffsets(text, start, end),
        mode: chosenMode,
        path: chosen.path,
        directiveRange: rangeFromOffsets(text, directiveStart, directiveEnd),
        modeRange: chosen.modeRange,
        pathRange: chosen.pathRange,
      }
    : undefined;
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
