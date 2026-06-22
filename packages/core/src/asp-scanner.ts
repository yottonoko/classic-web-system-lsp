import { DiagnosticSeverity } from "vscode-languageserver-types";
import type {
  AspInclude,
  AspNavigationCandidate,
  AspNavigationEdgeKind,
  AspNavigationParameterFlow,
  AspNavigationParameterSource,
  AspNavigationUrlValue,
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
  complete: boolean;
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
  maxEnd?: number;
}

export interface AspHtmlRangeScan extends AspHtmlScan {
  diagnostics: AspParsedDocument["diagnostics"];
  complete: boolean;
  scannedStart: number;
  scannedEnd: number;
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

export function extractHtmlNavigationCandidates(text: string, uri = ""): AspNavigationCandidate[] {
  const candidates: AspNavigationCandidate[] = [];
  let cursor = 0;
  while (cursor < text.length) {
    if (text.startsWith("<%", cursor)) {
      const close = findAspClose(text, cursor + 2, text.length);
      cursor = close === -1 ? text.length : close + 2;
      continue;
    }
    if (text.startsWith("<!--", cursor)) {
      const commentEnd = text.indexOf("-->", cursor + 4);
      cursor = commentEnd === -1 ? text.length : commentEnd + 3;
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
      addHtmlNavigationCandidatesFromTag(text, uri, tag, candidates);
    }
    if ((tag.name === "script" || tag.name === "style") && !tag.closing && !tag.selfClosing) {
      const close = findElementClose(text, tag.name, tag.end);
      if (close) {
        cursor = close.end;
        continue;
      }
    }
    cursor = tag.end;
  }
  return candidates;
}

function addHtmlNavigationCandidatesFromTag(
  text: string,
  uri: string,
  tag: HtmlTag,
  candidates: AspNavigationCandidate[],
): void {
  if (tag.name === "a" || tag.name === "area") {
    addAttributeNavigationCandidate(text, uri, tag, candidates, {
      kind: "htmlAnchor",
      attributeName: "href",
      label: `${tag.name} href`,
      targetFrame: attributeStringValue(tag, "target"),
    });
    return;
  }
  if (tag.name === "iframe" || tag.name === "frame") {
    addAttributeNavigationCandidate(text, uri, tag, candidates, {
      kind: "htmlFrame",
      attributeName: "src",
      label: `${tag.name} src`,
      targetFrame: attributeStringValue(tag, "name") ?? attributeStringValue(tag, "id"),
    });
    return;
  }
  if (tag.name === "form") {
    addFormNavigationCandidate(text, uri, tag, candidates);
    return;
  }
  if (tag.name === "button" || tag.name === "input") {
    addAttributeNavigationCandidate(text, uri, tag, candidates, {
      kind: "htmlForm",
      attributeName: "formaction",
      label: `${tag.name} formaction`,
      method: normalizeHtmlMethod(attributeStringValue(tag, "formmethod")),
      targetFrame: attributeStringValue(tag, "formtarget"),
      parameters: formControlParameterFromTag(text, tag),
    });
    return;
  }
  if (tag.name === "meta") {
    addMetaRefreshNavigationCandidate(text, uri, tag, candidates);
  }
}

function addAttributeNavigationCandidate(
  text: string,
  uri: string,
  tag: HtmlTag,
  candidates: AspNavigationCandidate[],
  options: {
    kind: AspNavigationEdgeKind;
    attributeName: string;
    label: string;
    method?: string;
    targetFrame?: string;
    parameters?: AspNavigationParameterFlow[];
  },
): void {
  const span = attributeSpanByName(tag, options.attributeName);
  if (!span || span.value === true) {
    return;
  }
  const target = navigationUrlValueFromHtmlValue(span.value);
  candidates.push({
    kind: options.kind,
    target,
    range: rangeFromOffsets(text, tag.start, tag.end),
    valueRange: rangeFromOffsets(text, span.valueStart, span.valueEnd),
    method: options.method,
    targetFrame: options.targetFrame,
    parameters: options.parameters,
    declaredInUri: uri,
    evidence: [
      {
        uri,
        range: rangeFromOffsets(text, tag.start, tag.end),
        valueRange: rangeFromOffsets(text, span.valueStart, span.valueEnd),
        label: options.label,
        snippet: snippetForRange(text, tag.start, tag.end),
        extractor: "html",
      },
    ],
    confidence: target.kind === "literal" ? "certain" : "possible",
    source: "html",
  });
}

function addFormNavigationCandidate(
  text: string,
  uri: string,
  tag: HtmlTag,
  candidates: AspNavigationCandidate[],
): void {
  const actionSpan = attributeSpanByName(tag, "action");
  const target =
    actionSpan && actionSpan.value !== true
      ? navigationUrlValueFromHtmlValue(actionSpan.value)
      : ({ kind: "literal", text: "" } satisfies AspNavigationUrlValue);
  const close = findGenericElementClose(text, tag.name, tag.end);
  const formEnd = close?.start ?? tag.end;
  const parameters = extractFormControlParameters(text, tag.end, formEnd);
  candidates.push({
    kind: "htmlForm",
    target,
    range: rangeFromOffsets(text, tag.start, close?.end ?? tag.end),
    valueRange:
      actionSpan && actionSpan.value !== true
        ? rangeFromOffsets(text, actionSpan.valueStart, actionSpan.valueEnd)
        : undefined,
    method: normalizeHtmlMethod(attributeStringValue(tag, "method")) ?? "GET",
    targetFrame: attributeStringValue(tag, "target"),
    parameters,
    declaredInUri: uri,
    evidence: [
      {
        uri,
        range: rangeFromOffsets(text, tag.start, tag.end),
        valueRange:
          actionSpan && actionSpan.value !== true
            ? rangeFromOffsets(text, actionSpan.valueStart, actionSpan.valueEnd)
            : undefined,
        label: "form action",
        snippet: snippetForRange(text, tag.start, tag.end),
        extractor: "html",
      },
    ],
    confidence: target.kind === "literal" ? "certain" : "possible",
    source: "html",
  });
}

function addMetaRefreshNavigationCandidate(
  text: string,
  uri: string,
  tag: HtmlTag,
  candidates: AspNavigationCandidate[],
): void {
  const httpEquiv = attributeStringValue(tag, "http-equiv");
  if (httpEquiv?.toLowerCase() !== "refresh") {
    return;
  }
  const contentSpan = attributeSpanByName(tag, "content");
  if (!contentSpan || contentSpan.value === true) {
    return;
  }
  const refreshTarget = parseMetaRefreshTarget(contentSpan.value);
  if (!refreshTarget) {
    return;
  }
  candidates.push({
    kind: "metaRefresh",
    target: navigationUrlValueFromHtmlValue(refreshTarget),
    range: rangeFromOffsets(text, tag.start, tag.end),
    valueRange: rangeFromOffsets(text, contentSpan.valueStart, contentSpan.valueEnd),
    declaredInUri: uri,
    evidence: [
      {
        uri,
        range: rangeFromOffsets(text, tag.start, tag.end),
        valueRange: rangeFromOffsets(text, contentSpan.valueStart, contentSpan.valueEnd),
        label: "meta refresh",
        snippet: snippetForRange(text, tag.start, tag.end),
        extractor: "html",
      },
    ],
    confidence: refreshTarget.includes("<%") ? "possible" : "certain",
    source: "html",
  });
}

function extractFormControlParameters(
  text: string,
  start: number,
  end: number,
): AspNavigationParameterFlow[] {
  const parameters: AspNavigationParameterFlow[] = [];
  let cursor = start;
  while (cursor < end) {
    if (text.startsWith("<%", cursor)) {
      const close = findAspClose(text, cursor + 2, end);
      cursor = close === -1 ? end : close + 2;
      continue;
    }
    if (text[cursor] !== "<") {
      cursor += 1;
      continue;
    }
    const tag = readHtmlTag(text, cursor, { maxEnd: end });
    if (!tag) {
      cursor += 1;
      continue;
    }
    if (!tag.closing) {
      parameters.push(...formControlParameterFromTag(text, tag));
    }
    cursor = tag.end;
  }
  return parameters;
}

function formControlParameterFromTag(text: string, tag: HtmlTag): AspNavigationParameterFlow[] {
  if (
    tag.name !== "input" &&
    tag.name !== "select" &&
    tag.name !== "textarea" &&
    tag.name !== "button"
  ) {
    return [];
  }
  const nameSpan = attributeSpanByName(tag, "name");
  if (!nameSpan || nameSpan.value === true || nameSpan.value.length === 0) {
    return [];
  }
  const valueSpan = attributeSpanByName(tag, "value");
  const inputType = attributeStringValue(tag, "type")?.toLowerCase();
  const source: AspNavigationParameterSource =
    tag.name === "input" && inputType === "hidden" ? "hiddenInput" : "formControl";
  return [
    {
      name: nameSpan.value,
      source,
      value: valueSpan && valueSpan.value !== true ? valueSpan.value : undefined,
      confidence:
        valueSpan && valueSpan.value !== true && !valueSpan.value.includes("<%")
          ? "certain"
          : "possible",
      range: rangeFromOffsets(text, nameSpan.valueStart, nameSpan.valueEnd),
    },
  ];
}

function navigationUrlValueFromHtmlValue(value: string): AspNavigationUrlValue {
  if (!value.includes("<%")) {
    return { kind: "literal", text: decodeHtmlAttributeValue(value.trim()) };
  }
  const parts = value
    .split(/(<%[\s\S]*?%>)/g)
    .filter((part) => part.length > 0)
    .map((part) =>
      part.startsWith("<%")
        ? { kind: "unknown" as const, text: part }
        : { kind: "text" as const, text: decodeHtmlAttributeValue(part) },
    );
  return { kind: "template", text: parts.map((part) => part.text ?? "").join(""), parts };
}

function parseMetaRefreshTarget(value: string): string | undefined {
  const match = value.match(/(?:^|;)\s*url\s*=\s*(?:"([^"]*)"|'([^']*)'|([^;]+))/i);
  return (match?.[1] ?? match?.[2] ?? match?.[3])?.trim();
}

function normalizeHtmlMethod(method: string | undefined): string | undefined {
  if (!method) {
    return undefined;
  }
  const normalized = method.trim().toUpperCase();
  return normalized.length > 0 ? normalized : undefined;
}

function decodeHtmlAttributeValue(value: string): string {
  return value
    .replace(/&amp;/gi, "&")
    .replace(/&quot;/gi, '"')
    .replace(/&#39;/g, "'")
    .replace(/&lt;/gi, "<")
    .replace(/&gt;/gi, ">");
}

function snippetForRange(text: string, start: number, end: number): string {
  return text.slice(start, end).replace(/\s+/g, " ").trim().slice(0, 240);
}

function findGenericElementClose(
  text: string,
  tagName: string,
  offset: number,
  maxEnd = text.length,
): { start: number; end: number } | undefined {
  let cursor = offset;
  while (cursor < maxEnd) {
    if (text.startsWith("<%", cursor)) {
      const close = findAspClose(text, cursor + 2, maxEnd);
      cursor = close === -1 ? maxEnd : close + 2;
      continue;
    }
    if (text[cursor] !== "<") {
      cursor += 1;
      continue;
    }
    const tag = readHtmlTag(text, cursor, { maxEnd });
    if (!tag) {
      cursor += 1;
      continue;
    }
    if (tag.closing && tag.name === tagName) {
      return { start: tag.start, end: tag.end };
    }
    cursor = tag.end;
  }
  return undefined;
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

export function scanHtmlAndAspRange(
  text: string,
  startOffset: number,
  endOffset: number,
  settings: AspSettings,
): AspHtmlRangeScan {
  const diagnostics: AspParsedDocument["diagnostics"] = [];
  const inlineRegions: AspRegion[] = [];
  const tagRegions: AspRegion[] = [];
  const includes: AspInclude[] = [];
  const serverObjects: AspServerObject[] = [];
  const scannedStart = Math.max(0, Math.min(startOffset, text.length));
  const scannedEnd = Math.max(scannedStart, Math.min(endOffset, text.length));
  let complete = true;
  let cursor = scannedStart;
  while (cursor < scannedEnd) {
    if (text.startsWith("<%", cursor)) {
      const region = parseAspRegionAt(text, cursor, diagnostics, scannedEnd, settings);
      inlineRegions.push(region);
      if (!text.startsWith("%>", region.contentEnd) && region.end === scannedEnd) {
        complete = false;
        break;
      }
      cursor = region.end;
      continue;
    }
    if (text.startsWith("<!--", cursor)) {
      const commentEnd = text.indexOf("-->", cursor + 4);
      if (commentEnd === -1 || commentEnd + 3 > scannedEnd) {
        complete = false;
        break;
      }
      const end = commentEnd + 3;
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
    const tag = readHtmlTag(text, cursor, { allowIncomplete: true, maxEnd: scannedEnd });
    if (!tag) {
      cursor += 1;
      continue;
    }
    if (!tag.complete) {
      complete = false;
      break;
    }
    if (!tag.closing) {
      const styleAttributeRegions = styleAttributeRegionsFromTag(tag);
      tagRegions.push(...styleAttributeRegions);
      const serverObject = serverObjectFromTag(text, tag);
      if (serverObject) {
        serverObjects.push(serverObject);
      }
      const nestedRegions = scanAspRegionsInRange(
        text,
        tag.attributesStart,
        tag.attributesEnd,
        diagnostics,
        settings,
      );
      if (nestedRegions.some((region) => isRangeCutAspRegion(text, region, scannedEnd))) {
        complete = false;
        break;
      }
      inlineRegions.push(...nestedRegions);
    }
    if ((tag.name === "script" || tag.name === "style") && !tag.closing && !tag.selfClosing) {
      const close = findElementClose(text, tag.name, tag.end, scannedEnd);
      if (!close) {
        complete = false;
        break;
      }
      const region = elementRegionFromTag(tag, close);
      tagRegions.push(region);
      const nestedRegions = scanAspRegionsInRange(
        text,
        tag.end,
        close.start,
        diagnostics,
        settings,
        tag.name === "script" ? "javascript" : "css",
      );
      if (nestedRegions.some((item) => isRangeCutAspRegion(text, item, close.start))) {
        complete = false;
        break;
      }
      inlineRegions.push(...nestedRegions);
      cursor = close.end;
      continue;
    }
    cursor = tag.end;
  }
  return {
    inlineRegions,
    tagRegions,
    includes,
    serverObjects,
    diagnostics,
    complete,
    scannedStart,
    scannedEnd,
  };
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
  let embeddedState: EmbeddedContentState = { kind: "normal" };
  let embeddedStateOffset = start;
  while (cursor < end) {
    const next = findAspOpenInRange(text, cursor, end, embeddedLanguage);
    if (next === -1 || next >= end) {
      break;
    }
    if (embeddedLanguage) {
      embeddedState = embeddedContentStateBetween(
        text,
        embeddedStateOffset,
        next,
        embeddedLanguage,
        embeddedState,
      );
      embeddedStateOffset = next;
      if (embeddedState.kind !== "normal") {
        const close = findAspClose(text, next + 2, end);
        const stateEnd = embeddedContentStateEnd(text, next, end, embeddedLanguage, embeddedState);
        if (close === -1 || close >= stateEnd) {
          cursor = next + 2;
          continue;
        }
      }
    }
    const region = parseAspRegionAt(text, next, diagnostics, end, settings);
    regions.push(region);
    cursor = Math.max(region.end, next + 2);
    if (embeddedLanguage) {
      embeddedStateOffset = cursor;
    }
  }
  return regions;
}

function isRangeCutAspRegion(text: string, region: AspRegion, endOffset: number): boolean {
  return region.end === endOffset && !text.startsWith("%>", region.contentEnd);
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

function embeddedContentStateBetween(
  text: string,
  start: number,
  offset: number,
  embeddedLanguage: "javascript" | "css",
  state: EmbeddedContentState,
): EmbeddedContentState {
  let current = state;
  for (let index = start; index < offset; index += 1) {
    const aspEnd = embeddedAspRegionEndAt(text, index, offset, current);
    if (aspEnd !== undefined) {
      index = aspEnd - 1;
      continue;
    }
    current = advanceEmbeddedContentState(text, index, offset, embeddedLanguage, current);
  }
  return current;
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
  const maxEnd = options.maxEnd ?? text.length;
  while (cursor < maxEnd && isHtmlWhitespaceCode(text.charCodeAt(cursor))) {
    cursor += 1;
  }
  const nameStart = cursor;
  if (!isAsciiAlphaCode(text.charCodeAt(cursor))) {
    return undefined;
  }
  cursor += 1;
  while (cursor < maxEnd && isHtmlTagNamePartCode(text.charCodeAt(cursor))) {
    cursor += 1;
  }
  const name = text.slice(nameStart, cursor).toLowerCase();
  const tagEnd = findTagEnd(text, cursor, maxEnd);
  if (tagEnd === -1 && !options.allowIncomplete) {
    return undefined;
  }
  const attributesEnd = tagEnd === -1 ? maxEnd : tagEnd;
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
    end: tagEnd === -1 ? maxEnd : tagEnd + 1,
    complete: tagEnd !== -1,
    attributesStart,
    attributesEnd,
    attributes,
    attributeSpans,
    closing,
    selfClosing: text.slice(attributesStart, attributesEnd).trimEnd().endsWith("/"),
  };
}

function findTagEnd(text: string, offset: number, maxEnd = text.length): number {
  let quote: string | undefined;
  for (let index = offset; index < maxEnd; index += 1) {
    const char = text[index];
    if (quote) {
      if (text.startsWith("<%", index)) {
        const close = findAspClose(text, index + 2, maxEnd);
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
      const close = findAspClose(text, index + 2, maxEnd);
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
  maxEnd = text.length,
): { start: number; end: number } | undefined {
  const embeddedLanguage = tagName === "script" ? "javascript" : "css";
  let state: EmbeddedContentState = { kind: "normal" };
  for (let index = offset; index < maxEnd; index += 1) {
    if (isClosingTagAt(text, index, tagName)) {
      const closeEnd = findTagEnd(text, index + 2, maxEnd);
      return closeEnd === -1 ? undefined : { start: index, end: closeEnd + 1 };
    }
    if (text.startsWith("<%", index)) {
      const close = findAspClose(text, index + 2, maxEnd);
      if (state.kind !== "normal") {
        const stateEnd = embeddedContentStateEnd(text, index, maxEnd, embeddedLanguage, state);
        if (close === -1 || close >= stateEnd) {
          state = advanceEmbeddedContentState(text, index, maxEnd, embeddedLanguage, state);
          continue;
        }
      }
      if (close === -1) {
        return undefined;
      }
      if (state.kind !== "normal") {
        state = advanceEmbeddedContentState(text, index, maxEnd, embeddedLanguage, state);
      }
      index = close + 1;
      continue;
    }
    state = advanceEmbeddedContentState(text, index, maxEnd, embeddedLanguage, state);
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
