import type { Position, Range } from "vscode-languageserver-types";

const lineStartsCache = new Map<string, number[]>();
const maxLineStartsCacheEntries = 64;

export function positionAt(text: string, offset: number): Position {
  const safeOffset = Math.max(0, Math.min(offset, text.length));
  const starts = lineStarts(text);
  let low = 0;
  let high = starts.length - 1;
  let line = 0;
  while (low <= high) {
    const middle = Math.floor((low + high) / 2);
    if (starts[middle] <= safeOffset) {
      line = middle;
      low = middle + 1;
    } else {
      high = middle - 1;
    }
  }
  const character = safeOffset - starts[line];
  return { line, character };
}

export function offsetAt(text: string, position: Position): number {
  const starts = lineStarts(text);
  if (position.line < 0) {
    return 0;
  }
  if (position.line >= starts.length) {
    return text.length;
  }
  const lineStart = starts[position.line];
  const nextLineStart = starts[position.line + 1] ?? text.length + 1;
  return Math.max(lineStart, Math.min(lineStart + position.character, nextLineStart - 1));
}

export function rangeFromOffsets(text: string, start: number, end: number): Range {
  return { start: positionAt(text, start), end: positionAt(text, end) };
}

function lineStarts(text: string): number[] {
  const cached = lineStartsCache.get(text);
  if (cached) {
    return cached;
  }
  const starts = [0];
  for (let index = 0; index < text.length; index += 1) {
    if (text.charCodeAt(index) === 10) {
      starts.push(index + 1);
    }
  }
  lineStartsCache.set(text, starts);
  if (lineStartsCache.size > maxLineStartsCacheEntries) {
    const oldest = lineStartsCache.keys().next().value;
    if (oldest !== undefined) {
      lineStartsCache.delete(oldest);
    }
  }
  return starts;
}
