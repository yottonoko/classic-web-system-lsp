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

type AspScriptScanLanguage = "VBScript" | "JScript";

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

export function scanHtmlAndAsp(
  text: string,
  diagnostics: AspParsedDocument["diagnostics"],
  settings: AspSettings,
): AspHtmlScan {
  const inlineRegions: AspRegion[] = [];
  const tagRegions: AspRegion[] = [];
  const includes: AspInclude[] = [];
  const serverObjects: AspServerObject[] = [];
  let scriptLanguage = normalizeScriptLanguage(settings.defaultLanguage ?? "VBScript");
  let cursor = 0;
  while (cursor < text.length) {
    if (text.startsWith("<%", cursor)) {
      const region = parseAspRegionAt(
        text,
        cursor,
        diagnostics,
        text.length,
        settings,
        scriptLanguage,
      );
      inlineRegions.push(region);
      scriptLanguage = scriptLanguageFromDirectiveRegion(text, region) ?? scriptLanguage;
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
    const tag = readHtmlTag(text, cursor, scriptLanguage);
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
          scriptLanguage,
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
            scriptLanguage,
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

function scanAspRegionsInRange(
  text: string,
  start: number,
  end: number,
  diagnostics: AspParsedDocument["diagnostics"],
  settings: AspSettings = {},
  scriptLanguage: AspScriptScanLanguage = normalizeScriptLanguage(
    settings.defaultLanguage ?? "VBScript",
  ),
): AspRegion[] {
  const regions: AspRegion[] = [];
  let cursor = start;
  while (cursor < end) {
    const next = text.indexOf("<%", cursor);
    if (next === -1 || next >= end) {
      break;
    }
    const region = parseAspRegionAt(text, next, diagnostics, end, settings, scriptLanguage);
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
  settings: AspSettings = {},
  scriptLanguage: AspScriptScanLanguage = normalizeScriptLanguage(
    settings.defaultLanguage ?? "VBScript",
  ),
): AspRegion {
  const close = findAspClose(text, start + 2, maxEnd, scriptLanguage);
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

function scriptLanguageFromDirectiveRegion(
  text: string,
  region: AspRegion,
): AspScriptScanLanguage | undefined {
  if (region.kind !== "asp-directive") {
    return undefined;
  }
  const raw = text.slice(region.contentStart, region.contentEnd).trim();
  const normalized = raw.startsWith("@") ? raw.slice(1).trim() : raw;
  const [first = "Page", ...rest] = normalized.split(/\s+/);
  const attributeText = first.includes("=") ? normalized : rest.join(" ");
  const language = parseAttributes(attributeText).language;
  return typeof language === "string" ? normalizeScriptLanguage(language) : undefined;
}

function findAspClose(
  text: string,
  offset: number,
  maxEnd: number,
  _scriptLanguage: AspScriptScanLanguage,
): number {
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
  scriptLanguage: AspScriptScanLanguage,
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
  const tagEnd = findTagEnd(text, cursor, scriptLanguage);
  if (tagEnd === -1) {
    return undefined;
  }
  const attributesStart = cursor;
  const attributesEnd = tagEnd;
  const attributeSpans = parseAttributeSpans(text, attributesStart, attributesEnd, scriptLanguage);
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

function findTagEnd(
  text: string,
  offset: number,
  scriptLanguage: AspScriptScanLanguage = "VBScript",
): number {
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
    if (text.startsWith("<%", index)) {
      const close = findAspClose(text, index + 2, text.length, scriptLanguage);
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

function parseAttributeSpans(
  text: string,
  start: number,
  end: number,
  scriptLanguage: AspScriptScanLanguage,
): AttributeSpan[] {
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
      const close = findAspClose(text, cursor + 2, end, scriptLanguage);
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
    while (cursor < end) {
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
  for (let index = offset; index < text.length; index += 1) {
    if (isClosingTagAt(text, index, tagName)) {
      const closeEnd = findTagEnd(text, index + 2);
      return closeEnd === -1 ? undefined : { start: index, end: closeEnd + 1 };
    }
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
