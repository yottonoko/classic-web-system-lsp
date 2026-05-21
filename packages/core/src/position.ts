import type { Position, Range } from "vscode-languageserver-types";

export function positionAt(text: string, offset: number): Position {
  const safeOffset = Math.max(0, Math.min(offset, text.length));
  let line = 0;
  let character = 0;
  for (let index = 0; index < safeOffset; index += 1) {
    if (text.charCodeAt(index) === 10) {
      line += 1;
      character = 0;
    } else {
      character += 1;
    }
  }
  return { line, character };
}

export function offsetAt(text: string, position: Position): number {
  let line = 0;
  let character = 0;
  for (let index = 0; index < text.length; index += 1) {
    if (line === position.line && character === position.character) {
      return index;
    }
    if (text.charCodeAt(index) === 10) {
      if (line === position.line) {
        return index;
      }
      line += 1;
      character = 0;
    } else {
      character += 1;
    }
  }
  return text.length;
}

export function rangeFromOffsets(text: string, start: number, end: number): Range {
  return { start: positionAt(text, start), end: positionAt(text, end) };
}
