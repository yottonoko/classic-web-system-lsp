import type { Range, TextEdit } from "vscode-languageserver-types";
import type { AspFormattingOptions, AspParsedDocument, AspRegion, VbToken } from "./types";
import { offsetAt, rangeFromOffsets } from "./position";
import { parseVbscriptCst, tokenizeVbscript } from "./vbscript-cst";

export function formatAspDocument(
  parsed: AspParsedDocument,
  options: AspFormattingOptions,
): TextEdit[] {
  const formatted = formatText(parsed, options, 0, parsed.text.length);
  return formatted === parsed.text
    ? []
    : [
        {
          range: rangeFromOffsets(parsed.text, 0, parsed.text.length),
          newText: formatted,
        },
      ];
}

export function formatAspRange(
  parsed: AspParsedDocument,
  range: Range,
  options: AspFormattingOptions,
): TextEdit[] {
  const start = lineStartOffset(parsed.text, offsetAt(parsed.text, range.start));
  const end = lineEndOffset(parsed.text, offsetAt(parsed.text, range.end));
  const formatted = formatText(parsed, options, start, end);
  const original = parsed.text.slice(start, end);
  return formatted === original
    ? []
    : [{ range: rangeFromOffsets(parsed.text, start, end), newText: formatted }];
}

function formatText(
  parsed: AspParsedDocument,
  options: AspFormattingOptions,
  start: number,
  end: number,
): string {
  const pieces: string[] = [];
  let cursor = start;
  const regions = parsed.regions
    .filter((region) => region.end > start && region.start < end)
    .sort(
      (left, right) =>
        left.start - right.start ||
        formatRegionPriority(left) - formatRegionPriority(right) ||
        right.end - left.end,
    );
  for (const region of regions) {
    if (region.start < cursor) {
      continue;
    }
    if (region.start > cursor) {
      pieces.push(parsed.text.slice(cursor, Math.min(region.start, end)));
    }
    const regionStart = Math.max(region.start, start);
    const regionEnd = Math.min(region.end, end);
    if (regionStart < regionEnd) {
      pieces.push(formatRegion(parsed, region, options, regionStart, regionEnd));
    }
    cursor = Math.max(cursor, regionEnd);
  }
  if (cursor < end) {
    pieces.push(parsed.text.slice(cursor, end));
  }
  return pieces.join("");
}

function formatRegionPriority(region: AspRegion): number {
  return region.kind === "html" ? 1 : 0;
}

function formatRegion(
  parsed: AspParsedDocument,
  region: AspRegion,
  options: AspFormattingOptions,
  start: number,
  end: number,
): string {
  if (region.language !== "vbscript") {
    return parsed.text.slice(start, end);
  }
  if (start !== region.start || end !== region.end) {
    return formatVbscriptBlock(parsed.text.slice(start, end), options, "");
  }
  if (region.kind === "asp-expression") {
    const expression = formatVbscriptLine(
      parsed.text.slice(region.contentStart, region.contentEnd).trim(),
      options,
    );
    return aspExpressionText(expression, options);
  }
  if (region.kind === "asp-directive") {
    const directive = oneLine(parsed.text.slice(region.contentStart, region.contentEnd));
    const normalized = directive.startsWith("@") ? directive.slice(1).trim() : directive;
    return aspDirectiveText(normalized, options);
  }
  if (region.kind === "asp-block") {
    const content = parsed.text.slice(region.contentStart, region.contentEnd);
    const hasLineBreak = content.includes("\n") || content.includes("\r");
    if (!hasLineBreak && options.aspBlockNewline === "alwaysMultiline") {
      const baseIndent = vbscriptTagIndent(parsed.text, region, options);
      const contentIndent = baseIndent + vbscriptBlockIndent(options);
      return `<%\n${formatVbscriptBlock(content, options, contentIndent)}\n${baseIndent}%>`;
    }
    if (!hasLineBreak) {
      return aspBlockText(formatVbscriptLine(content.trim(), options), options);
    }
    const baseIndent = vbscriptTagIndent(parsed.text, region, options);
    const contentIndent = baseIndent + vbscriptBlockIndent(options);
    const formattedBlock = formatVbscriptBlock(content, options, contentIndent);
    if (
      options.aspBlockNewline === "singleLineWhenPossible" &&
      !formattedBlock.includes("\n") &&
      !formattedBlock.includes("\r")
    ) {
      return aspBlockText(formattedBlock.trim(), options);
    }
    return `<%\n${formattedBlock}\n${baseIndent}%>`;
  }
  const before = parsed.text.slice(region.start, region.contentStart);
  const after = parsed.text.slice(region.contentEnd, region.end);
  const content = parsed.text.slice(region.contentStart, region.contentEnd);
  if (!content.includes("\n") && !content.includes("\r")) {
    return `${before}${formatVbscriptLine(content.trim(), options)}${after}`;
  }
  const baseIndent = vbscriptTagIndent(parsed.text, region, options);
  const contentIndent =
    baseIndent + (options.ignoreVbscriptTagIndent === true ? "" : vbscriptIndentUnit(options));
  return `${before}\n${formatVbscriptBlock(
    content,
    options,
    contentIndent,
  )}\n${baseIndent}${after}`;
}

function formatVbscriptBlock(
  text: string,
  options: AspFormattingOptions,
  baseIndent: string,
): string {
  const unit = vbscriptIndentUnit(options);
  const lines = text
    .replace(/^\s*\r?\n/, "")
    .replace(/\r?\n\s*$/, "")
    .split(/\r?\n/);
  const trimmedLines = lines.map((line) => line.trim());
  const tokensByLine = tokenizeVbscriptLines(trimmedLines);
  let indentLevel = 0;
  const formatted: string[] = [];
  const selectIndentStack: number[] = [];
  let previousSignificantLine: string | undefined;
  for (let lineIndex = 0; lineIndex < lines.length; lineIndex += 1) {
    const trimmed = trimmedLines[lineIndex];
    if (trimmed.length === 0) {
      formatted.push("");
      continue;
    }
    const tokens = tokensByLine[lineIndex] ?? [];
    const continuesPreviousLine =
      previousSignificantLine !== undefined && isLineContinuation(previousSignificantLine);
    const code = codeBeforeCommentFromTokens(trimmed, tokens);
    if (!continuesPreviousLine && /^End\s+Select\b/i.test(code)) {
      indentLevel = selectIndentStack.pop() ?? Math.max(0, indentLevel - 1);
    } else if (!continuesPreviousLine && isCaseLine(code)) {
      const selectIndent = selectIndentStack.at(-1);
      if (selectIndent !== undefined) {
        indentLevel =
          options.vbscriptSelectCaseIndent === "caseAligned"
            ? selectIndent
            : previousSignificantLine
              ? selectIndent + 1
              : Math.max(0, indentLevel);
      }
    } else if (!continuesPreviousLine && dedentsBeforeLine(code)) {
      indentLevel = Math.max(0, indentLevel - 1);
    }
    const formattedLine = formatVbscriptTokens(tokens, options);
    formatted.push(
      `${baseIndent}${unit.repeat(indentLevel)}${continuesPreviousLine ? vbscriptLineContinuationIndent(options) : ""}${formattedLine}`,
    );
    if (/^Select\b/i.test(code)) {
      selectIndentStack.push(indentLevel);
      indentLevel += 1;
    } else if (isCaseLine(code)) {
      indentLevel += 1;
    } else if (indentsAfterLine(code)) {
      indentLevel += 1;
    }
    previousSignificantLine = formattedLine;
  }
  return options.alignAssignments ? alignAssignments(formatted).join("\n") : formatted.join("\n");
}

function formatVbscriptLine(line: string, options: AspFormattingOptions): string {
  return formatVbscriptTokens(parseVbscriptCst(line).tokens, options);
}

function formatVbscriptTokens(tokens: readonly VbToken[], options: AspFormattingOptions): string {
  const significantTokens = tokens.filter(
    (token) => token.kind !== "whitespace" && token.kind !== "newline",
  );
  let result = "";
  let previous: VbToken | undefined;
  for (const token of significantTokens) {
    const text = formatToken(token, options);
    if (result.length === 0) {
      result = text;
      previous = token;
      continue;
    }
    if (token.kind === "comment") {
      result += ` ${text}`;
      previous = token;
      continue;
    }
    if (previous && needsSpaceBetween(previous, token)) {
      result += " ";
    }
    result += text;
    previous = token;
  }
  return result;
}

function tokenizeVbscriptLines(lines: string[]): VbToken[][] {
  const tokensByLine = lines.map(() => [] as VbToken[]);
  if (lines.length === 0) {
    return tokensByLine;
  }
  const source = lines.join("\n");
  const tokens = tokenizeVbscript(source, 0);
  let line = 0;
  let lineStart = 0;
  for (const token of tokens) {
    if (token.kind === "newline") {
      line += 1;
      lineStart = token.end;
      continue;
    }
    if (line >= tokensByLine.length) {
      break;
    }
    tokensByLine[line].push({
      ...token,
      start: token.start - lineStart,
      end: token.end - lineStart,
    });
  }
  return tokensByLine;
}

function formatToken(token: VbToken, options: AspFormattingOptions): string {
  if (token.kind !== "keyword") {
    return token.text;
  }
  switch (vbscriptKeywordCase(options)) {
    case "upper":
      return token.text.toUpperCase();
    case "lower":
      return token.text.toLowerCase();
    case "title":
      return `${token.text.slice(0, 1).toUpperCase()}${token.text.slice(1).toLowerCase()}`;
    case "preserve":
      return token.text;
  }
}

function needsSpaceBetween(left: VbToken, right: VbToken): boolean {
  if (left.text === "." || right.text === ".") {
    return false;
  }
  if (left.text === "(" || left.text === "[") {
    return false;
  }
  if (right.text === ")" || right.text === "]" || right.text === ",") {
    return false;
  }
  if (left.text === "," || left.text === "=" || right.text === "=" || left.text === ":") {
    return true;
  }
  if (right.text === ":") {
    return true;
  }
  if (right.text === "(") {
    return false;
  }
  return true;
}

function dedentsBeforeLine(line: string): boolean {
  return /^(End\b|Else\b|ElseIf\b|Next\b|Loop\b|Wend\b)/i.test(line);
}

function indentsAfterLine(line: string): boolean {
  const statement = withoutDeclarationModifiers(line);
  return (
    /^Else$/i.test(line) ||
    ((/^(Class|Sub|Function|Property\b.*\b(Get|Let|Set)|With|For\b|Do\b|While\b)/i.test(
      statement,
    ) ||
      /\bThen$/i.test(line)) &&
      !/^End\b/i.test(line))
  );
}

function withoutDeclarationModifiers(line: string): string {
  return line.replace(/^(?:(?:Public|Private|Default|Static)\s+)+/i, "");
}

function codeBeforeCommentFromTokens(line: string, tokens: readonly VbToken[]): string {
  const comment = tokens.find((token) => token.kind === "comment");
  return (comment ? line.slice(0, comment.start) : line).trim();
}

function isCaseLine(line: string): boolean {
  return /^Case\b/i.test(line);
}

function isLineContinuation(line: string): boolean {
  return /(?:^|\s)_\s*(?:'.*)?$/.test(line);
}

function oneLine(text: string): string {
  return text.replace(/\s+/g, " ").trim();
}

function aspExpressionText(expression: string, options: AspFormattingOptions): string {
  return options.aspDelimiterSpacing === "compact" ? `<%=${expression}%>` : `<%= ${expression} %>`;
}

function aspDirectiveText(directive: string, options: AspFormattingOptions): string {
  return options.aspDelimiterSpacing === "compact" ? `<%@${directive}%>` : `<%@ ${directive} %>`;
}

function aspBlockText(code: string, options: AspFormattingOptions): string {
  return options.aspDelimiterSpacing === "compact" ? `<%${code}%>` : `<% ${code} %>`;
}

function vbscriptKeywordCase(
  options: AspFormattingOptions,
): NonNullable<AspFormattingOptions["vbscriptKeywordCase"]> {
  return options.vbscriptKeywordCase ?? (options.uppercaseKeywords ? "upper" : "preserve");
}

function vbscriptIndentUnit(options: AspFormattingOptions): string {
  const style =
    options.vbscriptIndentStyle ?? options.indentStyle ?? (options.insertSpaces ? "space" : "tab");
  if (style === "tab") {
    return "\t";
  }
  return " ".repeat(options.vbscriptIndentSize ?? options.indentSize ?? options.tabSize);
}

function vbscriptLineContinuationIndent(options: AspFormattingOptions): string {
  return typeof options.vbscriptLineContinuationIndentSize === "number" &&
    options.vbscriptLineContinuationIndentSize > 0
    ? " ".repeat(options.vbscriptLineContinuationIndentSize)
    : vbscriptIndentUnit(options);
}

function indentUnit(options: AspFormattingOptions): string {
  const style = options.indentStyle ?? (options.insertSpaces ? "space" : "tab");
  if (style === "tab") {
    return "\t";
  }
  return " ".repeat(indentSize(options));
}

function alignAssignments(lines: string[]): string[] {
  const result = [...lines];
  let groupStart = -1;
  let maxLeft = 0;
  const flush = (exclusiveEnd: number): void => {
    if (groupStart === -1 || exclusiveEnd - groupStart < 2) {
      groupStart = -1;
      maxLeft = 0;
      return;
    }
    for (let index = groupStart; index < exclusiveEnd; index += 1) {
      const match = /^(\s*(?:Set\s+)?[A-Za-z_][A-Za-z0-9_.]*) = (.+)$/i.exec(result[index]);
      if (match) {
        result[index] = `${match[1].padEnd(maxLeft)} = ${match[2]}`;
      }
    }
    groupStart = -1;
    maxLeft = 0;
  };
  for (let index = 0; index <= result.length; index += 1) {
    const match =
      index < result.length
        ? /^(\s*(?:Set\s+)?[A-Za-z_][A-Za-z0-9_.]*) = (.+)$/i.exec(result[index])
        : undefined;
    if (!match) {
      flush(index);
      continue;
    }
    if (groupStart === -1) {
      groupStart = index;
    }
    maxLeft = Math.max(maxLeft, match[1].length);
  }
  return result;
}

function vbscriptTagIndent(text: string, region: AspRegion, options: AspFormattingOptions): string {
  const mode =
    options.vbscriptTagIndentMode ?? (options.ignoreVbscriptTagIndent ? "ignoreTag" : undefined);
  if (mode === "ignoreTag") {
    return "";
  }
  if (mode === "preserveExisting" || hasSeparateVbscriptIndent(options)) {
    return leadingIndentText(text, region.start);
  }
  return indentUnit(options).repeat(leadingIndentLevel(text, region.start, options));
}

function vbscriptBlockIndent(options: AspFormattingOptions): string {
  return options.vbscriptBlockIndent === "alignWithDelimiter" ? "" : vbscriptIndentUnit(options);
}

function leadingIndentText(text: string, offset: number): string {
  const lineStart = lineStartOffset(text, offset);
  return text.slice(lineStart, offset).match(/^[\t ]*/)?.[0] ?? "";
}

function leadingIndentLevel(text: string, offset: number, options: AspFormattingOptions): number {
  return Math.floor(indentWidth(leadingIndentText(text, offset), options) / indentSize(options));
}

function indentSize(options: AspFormattingOptions): number {
  return options.indentSize ?? options.tabSize;
}

function indentWidth(indent: string, options: AspFormattingOptions): number {
  let width = 0;
  for (const char of indent) {
    width += char === "\t" ? options.tabSize : 1;
  }
  return width;
}

function hasSeparateVbscriptIndent(options: AspFormattingOptions): boolean {
  return options.vbscriptIndentSize !== undefined || options.vbscriptIndentStyle !== undefined;
}

function lineStartOffset(text: string, offset: number): number {
  return text.lastIndexOf("\n", Math.max(0, offset - 1)) + 1;
}

function lineEndOffset(text: string, offset: number): number {
  const end = text.indexOf("\n", offset);
  return end === -1 ? text.length : end;
}
