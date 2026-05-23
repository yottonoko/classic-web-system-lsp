import type { Position, Range, TextEdit } from "vscode-languageserver-types";
import { offsetAt, positionAt } from "./position";
import { parseAspDocument } from "./parser";
import type { AspParsedDocument, AspRegion, AspSettings } from "./types";

type CommentStyle = "html" | "css" | "javascript" | "vbscript";

interface CommentLine {
  line: number;
  text: string;
  style: CommentStyle;
  commentColumn: number;
}

interface CommentMarkers {
  line?: string;
  block?: { open: string; close: string };
}

const markersByStyle: Record<CommentStyle, CommentMarkers> = {
  css: { block: { open: "/*", close: "*/" } },
  html: { block: { open: "<!--", close: "-->" } },
  javascript: { line: "//" },
  vbscript: { line: "'" },
};

export function getClassicAspLineCommentEdits(
  uri: string,
  text: string,
  selections: readonly Range[],
  settings: AspSettings = {},
): TextEdit[] {
  const parsed = parseAspDocument(uri, text, settings);
  const lineStarts = lineStartOffsets(text);
  const lines = selectedLines(text, selections, lineStarts)
    .map((selection) => commentLine(parsed, text, lineStarts, selection.line, selection.context))
    .filter((line): line is CommentLine => Boolean(line));
  const uniqueLines = dedupeCommentLines(lines);
  if (uniqueLines.length === 0) {
    return [];
  }
  const shouldUncomment = uniqueLines.every((line) => isCommented(line));
  const edits = shouldUncomment
    ? uniqueLines.flatMap((line) => uncommentLineEdits(line))
    : uniqueLines.flatMap((line) => commentLineEdits(line));
  return edits.sort((left, right) => {
    const leftOffset = offsetAt(text, left.range.start);
    const rightOffset = offsetAt(text, right.range.start);
    return rightOffset - leftOffset;
  });
}

function selectedLines(
  text: string,
  selections: readonly Range[],
  lineStarts: readonly number[],
): Array<{ line: number; context: Position }> {
  const lineCount = lineStarts.length;
  const result: Array<{ line: number; context: Position }> = [];
  for (const selection of selections.length > 0
    ? selections
    : [{ start: { line: 0, character: 0 }, end: { line: 0, character: 0 } }]) {
    const empty =
      selection.start.line === selection.end.line &&
      selection.start.character === selection.end.character;
    const first = Math.max(0, Math.min(selection.start.line, lineCount - 1));
    const last = Math.max(
      first,
      Math.min(
        selection.end.character === 0 && selection.end.line > selection.start.line
          ? selection.end.line - 1
          : selection.end.line,
        lineCount - 1,
      ),
    );
    for (let line = first; line <= last; line += 1) {
      result.push({
        line,
        context:
          empty && line === selection.start.line
            ? selection.start
            : firstContentPosition(text, lineStarts, line),
      });
    }
  }
  return result;
}

function commentLine(
  parsed: AspParsedDocument,
  text: string,
  lineStarts: readonly number[],
  line: number,
  context: Position,
): CommentLine | undefined {
  const startOffset = lineStarts[line];
  const endOffset = line + 1 < lineStarts.length ? lineStarts[line + 1] : text.length;
  const rawText = text.slice(startOffset, endOffset).replace(/\r?\n$/, "");
  const firstContent = rawText.search(/\S/);
  if (firstContent === -1) {
    return undefined;
  }
  const contextOffset = Math.max(
    startOffset,
    Math.min(offsetAt(text, context), startOffset + rawText.length),
  );
  const region =
    findRegionAt(parsed, contextOffset) ?? findRegionAt(parsed, startOffset + firstContent);
  const style = commentStyle(region, contextOffset);
  const commentColumn =
    style === "vbscript" && region && contextOffset < region.contentStart
      ? positionAt(text, region.contentStart).character
      : firstContent;
  return { line, text: rawText, style, commentColumn };
}

function commentStyle(region: AspRegion | undefined, offset: number): CommentStyle {
  if (!region || offset < region.contentStart || offset > region.contentEnd) {
    if (region?.kind === "asp-block" || region?.kind === "asp-expression") {
      return "vbscript";
    }
    return "html";
  }
  if (region.language === "css") {
    return "css";
  }
  if (region.language === "javascript" || region.language === "jscript") {
    return "javascript";
  }
  if (region.language === "vbscript") {
    return "vbscript";
  }
  return "html";
}

function findRegionAt(parsed: AspParsedDocument, offset: number): AspRegion | undefined {
  return parsed.regions.find((region) => region.start <= offset && offset <= region.end);
}

function isCommented(line: CommentLine): boolean {
  const markers = markersByStyle[line.style];
  if (markers.line) {
    return line.text.slice(line.commentColumn).trimStart().startsWith(markers.line);
  }
  if (markers.block) {
    const content = line.text.slice(line.commentColumn).trim();
    return content.startsWith(markers.block.open) && content.endsWith(markers.block.close);
  }
  return false;
}

function commentLineEdits(line: CommentLine): TextEdit[] {
  const markers = markersByStyle[line.style];
  if (markers.line) {
    return [
      {
        range: zeroWidthRange(line.line, line.commentColumn),
        newText: `${markers.line} `,
      },
    ];
  }
  if (!markers.block) {
    return [];
  }
  return [
    { range: zeroWidthRange(line.line, line.text.length), newText: ` ${markers.block.close}` },
    { range: zeroWidthRange(line.line, line.commentColumn), newText: `${markers.block.open} ` },
  ];
}

function uncommentLineEdits(line: CommentLine): TextEdit[] {
  const markers = markersByStyle[line.style];
  if (markers.line) {
    const markerIndex = line.text.indexOf(markers.line, line.commentColumn);
    if (markerIndex === -1) {
      return [];
    }
    const end =
      markerIndex +
      markers.line.length +
      (line.text[markerIndex + markers.line.length] === " " ? 1 : 0);
    return [{ range: range(line.line, markerIndex, end), newText: "" }];
  }
  if (!markers.block) {
    return [];
  }
  const openIndex = line.text.indexOf(markers.block.open, line.commentColumn);
  const closeIndex = line.text.lastIndexOf(markers.block.close);
  if (openIndex === -1 || closeIndex <= openIndex) {
    return [];
  }
  const openEnd =
    openIndex +
    markers.block.open.length +
    (line.text[openIndex + markers.block.open.length] === " " ? 1 : 0);
  const closeStart =
    closeIndex > 0 && line.text[closeIndex - 1] === " " ? closeIndex - 1 : closeIndex;
  return [
    { range: range(line.line, closeStart, closeIndex + markers.block.close.length), newText: "" },
    { range: range(line.line, openIndex, openEnd), newText: "" },
  ];
}

function dedupeCommentLines(lines: readonly CommentLine[]): CommentLine[] {
  const seen = new Set<number>();
  return lines.filter((line) => {
    if (seen.has(line.line)) {
      return false;
    }
    seen.add(line.line);
    return true;
  });
}

function firstContentPosition(text: string, lineStarts: readonly number[], line: number): Position {
  const start = lineStarts[line];
  const end = line + 1 < lineStarts.length ? lineStarts[line + 1] : text.length;
  const rawText = text.slice(start, end).replace(/\r?\n$/, "");
  const firstContent = rawText.search(/\S/);
  return { line, character: firstContent === -1 ? 0 : firstContent };
}

function lineStartOffsets(text: string): number[] {
  const offsets = [0];
  for (let index = 0; index < text.length; index += 1) {
    if (text[index] === "\n") {
      offsets.push(index + 1);
    }
  }
  return offsets;
}

function zeroWidthRange(line: number, character: number): Range {
  return range(line, character, character);
}

function range(line: number, start: number, end: number): Range {
  return { start: { line, character: start }, end: { line, character: end } };
}
