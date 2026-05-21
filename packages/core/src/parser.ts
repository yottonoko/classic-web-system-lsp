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

const includePattern =
  /<!--\s*#include\s+(file|virtual)\s*=\s*("([^"]*)"|'([^']*)'|([^\s>]+))\s*-->/gi;
const attrPattern = /([A-Za-z_:][-A-Za-z0-9_:.]*)\s*(?:=\s*("([^"]*)"|'([^']*)'|([^\s>]+)))?/g;

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
  const inlineRegions = parseInlineAspRegions(text, diagnostics);
  const tagRegions = [...parseTagRegions(text), ...parseStyleAttributeRegions(text)];
  const includes = parseIncludes(text);
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

function parseInlineAspRegions(
  text: string,
  diagnostics: AspParsedDocument["diagnostics"],
): AspRegion[] {
  const regions: AspRegion[] = [];
  let cursor = 0;
  while (cursor < text.length) {
    const start = text.indexOf("<%", cursor);
    if (start === -1) {
      break;
    }
    const close = text.indexOf("%>", start + 2);
    if (close === -1) {
      diagnostics.push({
        severity: DiagnosticSeverity.Error,
        range: rangeFromOffsets(text, start, text.length),
        message: "Classic ASP block is missing a closing %> delimiter.",
        source: "asp-lsp",
      });
      regions.push({
        kind: "asp-block",
        language: "vbscript",
        start,
        end: text.length,
        contentStart: start + 2,
        contentEnd: text.length,
      });
      break;
    }
    const marker = text[start + 2];
    const kind = marker === "=" ? "asp-expression" : marker === "@" ? "asp-directive" : "asp-block";
    const contentStart = start + (marker === "=" || marker === "@" ? 3 : 2);
    regions.push({
      kind,
      language: kind === "asp-directive" ? "asp-directive" : "vbscript",
      start,
      end: close + 2,
      contentStart,
      contentEnd: close,
    });
    cursor = close + 2;
  }
  return regions;
}

function parseTagRegions(text: string): AspRegion[] {
  const regions: AspRegion[] = [];
  const tagPattern = /<(script|style)\b([^>]*)>/gi;
  let match: RegExpExecArray | null;
  while ((match = tagPattern.exec(text)) !== null) {
    const tagName = match[1].toLowerCase();
    const openEnd = tagPattern.lastIndex;
    const closePattern = new RegExp(`</${tagName}\\s*>`, "gi");
    closePattern.lastIndex = openEnd;
    const close = closePattern.exec(text);
    if (!close) {
      continue;
    }
    const attributes = parseAttributes(match[2] ?? "");
    const start = match.index;
    const end = close.index + close[0].length;
    if (tagName === "style") {
      regions.push({
        kind: "style",
        language: "css",
        start,
        end,
        contentStart: openEnd,
        contentEnd: close.index,
        attributes,
      });
    } else {
      const runatServer = String(attributes.runat ?? "").toLowerCase() === "server";
      const language = String(attributes.language ?? attributes.type ?? "");
      regions.push({
        kind: runatServer ? "server-script" : "client-script",
        language: runatServer
          ? normalizeScriptLanguage(language || "VBScript").toLowerCase() === "jscript"
            ? "jscript"
            : "vbscript"
          : "javascript",
        start,
        end,
        contentStart: openEnd,
        contentEnd: close.index,
        attributes,
      });
    }
    tagPattern.lastIndex = end;
  }
  return regions;
}

function parseStyleAttributeRegions(text: string): AspRegion[] {
  const regions: AspRegion[] = [];
  const tagPattern = /<([A-Za-z][A-Za-z0-9:_-]*)([^<>]*)>/g;
  let tagMatch: RegExpExecArray | null;
  while ((tagMatch = tagPattern.exec(text)) !== null) {
    const tagName = tagMatch[1].toLowerCase();
    if (tagName === "script" || tagName === "style") {
      continue;
    }
    const attributesText = tagMatch[2] ?? "";
    const attributesStart = tagMatch.index + tagMatch[0].indexOf(attributesText);
    const stylePattern = /\bstyle\s*=\s*("([^"]*)"|'([^']*)')/gi;
    let styleMatch: RegExpExecArray | null;
    while ((styleMatch = stylePattern.exec(attributesText)) !== null) {
      const quoted = styleMatch[1];
      const value = styleMatch[2] ?? styleMatch[3] ?? "";
      const valueStartInMatch = styleMatch[0].indexOf(quoted) + 1;
      const contentStart = attributesStart + styleMatch.index + valueStartInMatch;
      regions.push({
        kind: "style-attribute",
        language: "css",
        start: contentStart,
        end: contentStart + value.length,
        contentStart,
        contentEnd: contentStart + value.length,
        attributes: { tagName },
      });
    }
  }
  return regions;
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
  if (region.language === "vbscript" || region.language === "jscript") {
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

function parseIncludes(text: string): AspInclude[] {
  const includes: AspInclude[] = [];
  let match: RegExpExecArray | null;
  includePattern.lastIndex = 0;
  while ((match = includePattern.exec(text)) !== null) {
    includes.push({
      offset: match.index,
      range: rangeFromOffsets(text, match.index, match.index + match[0].length),
      mode: match[1].toLowerCase() === "virtual" ? "virtual" : "file",
      path: match[3] ?? match[4] ?? match[5] ?? "",
    });
  }
  return includes;
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
