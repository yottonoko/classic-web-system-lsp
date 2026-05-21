import type { Range, TextEdit } from "vscode-languageserver-types";
import type { AspFormattingOptions, AspParsedDocument, AspRegion } from "./types";
import { offsetAt, rangeFromOffsets } from "./position";

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
    .sort((left, right) => left.start - right.start || left.end - right.end);
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

function formatRegion(
  parsed: AspParsedDocument,
  region: AspRegion,
  options: AspFormattingOptions,
  start: number,
  end: number,
): string {
  if (region.language !== "vbscript" && region.language !== "jscript") {
    return parsed.text.slice(start, end);
  }
  if (start !== region.start || end !== region.end) {
    return formatVbscriptBlock(parsed.text.slice(start, end), options, 0);
  }
  if (region.kind === "asp-expression") {
    const expression = oneLine(parsed.text.slice(region.contentStart, region.contentEnd));
    return `<%= ${expression} %>`;
  }
  if (region.kind === "asp-directive") {
    const directive = oneLine(parsed.text.slice(region.contentStart, region.contentEnd));
    const normalized = directive.startsWith("@") ? directive.slice(1).trim() : directive;
    return `<%@ ${normalized} %>`;
  }
  if (region.kind === "asp-block") {
    const content = parsed.text.slice(region.contentStart, region.contentEnd);
    if (!content.includes("\n") && !content.includes("\r")) {
      return `<% ${formatVbscriptLine(oneLine(content), options)} %>`;
    }
    return `<%\n${formatVbscriptBlock(content, options, leadingIndent(parsed.text, region.start))}\n%>`;
  }
  const before = parsed.text.slice(region.start, region.contentStart);
  const after = parsed.text.slice(region.contentEnd, region.end);
  return `${before}${formatVbscriptBlock(
    parsed.text.slice(region.contentStart, region.contentEnd),
    options,
    leadingIndent(parsed.text, region.start) + 1,
  )}${after}`;
}

function formatVbscriptBlock(
  text: string,
  options: AspFormattingOptions,
  baseIndentLevel: number,
): string {
  const unit = indentUnit(options);
  const lines = text
    .replace(/^\s*\r?\n/, "")
    .replace(/\r?\n\s*$/, "")
    .split(/\r?\n/);
  let indentLevel = baseIndentLevel;
  const formatted: string[] = [];
  for (const line of lines) {
    const trimmed = line.trim();
    if (trimmed.length === 0) {
      formatted.push("");
      continue;
    }
    if (dedentsBeforeLine(trimmed)) {
      indentLevel = Math.max(baseIndentLevel, indentLevel - 1);
    }
    formatted.push(`${unit.repeat(indentLevel)}${formatVbscriptLine(trimmed, options)}`);
    if (indentsAfterLine(trimmed)) {
      indentLevel += 1;
    }
  }
  return options.alignAssignments ? alignAssignments(formatted).join("\n") : formatted.join("\n");
}

function formatVbscriptLine(line: string, options: AspFormattingOptions): string {
  let result = line
    .replace(/\s+/g, " ")
    .replace(/\s*=\s*/g, " = ")
    .trim();
  if (options.uppercaseKeywords) {
    for (const keyword of [
      "Option",
      "Explicit",
      "Dim",
      "ReDim",
      "Preserve",
      "Set",
      "Const",
      "Sub",
      "Function",
      "Class",
      "Property",
      "Get",
      "Let",
      "End",
      "If",
      "Then",
      "Else",
      "For",
      "Each",
      "In",
      "Next",
      "With",
      "New",
    ]) {
      result = result.replace(new RegExp(`\\b${keyword}\\b`, "gi"), keyword.toUpperCase());
    }
  }
  return result;
}

function dedentsBeforeLine(line: string): boolean {
  return /^(End\b|Else\b|ElseIf\b|Next\b|Loop\b|Wend\b)/i.test(line);
}

function indentsAfterLine(line: string): boolean {
  return (
    (/^(Class|Sub|Function|Property\b.*\b(Get|Let|Set)|With|For\b|Do\b|While\b|Select\b)/i.test(
      line,
    ) ||
      /\bThen$/i.test(line)) &&
    !/^End\b/i.test(line)
  );
}

function oneLine(text: string): string {
  return text.replace(/\s+/g, " ").trim();
}

function indentUnit(options: AspFormattingOptions): string {
  const style = options.indentStyle ?? (options.insertSpaces ? "space" : "tab");
  if (style === "tab") {
    return "\t";
  }
  return " ".repeat(options.indentSize ?? options.tabSize);
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

function leadingIndent(text: string, offset: number): number {
  const lineStart = lineStartOffset(text, offset);
  return Math.floor(
    (text
      .slice(lineStart, offset)
      .match(/^[\t ]*/)?.[0]
      .replaceAll("\t", "  ").length ?? 0) / 2,
  );
}

function lineStartOffset(text: string, offset: number): number {
  return text.lastIndexOf("\n", Math.max(0, offset - 1)) + 1;
}

function lineEndOffset(text: string, offset: number): number {
  const end = text.indexOf("\n", offset);
  return end === -1 ? text.length : end;
}
