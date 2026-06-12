import type {
  AspEditImpact,
  AspIncrementalChange,
  AspIncrementalMode,
  AspParsedDocument,
  AspSettings,
} from "./types";
import { offsetAt } from "./position";

export interface DamageSpan {
  start: number;
  end: number;
}

export interface IncrementalReparseResult {
  parsed: AspParsedDocument;
  impact: AspEditImpact;
  reusedRegionCount: number;
  rescannedSpan: DamageSpan;
}

export interface NormalizedIncrementalChange extends AspIncrementalChange {
  startOffset: number;
  endOffset: number;
}

export function resolveAspIncrementalMode(settings: AspSettings = {}): AspIncrementalMode {
  const mode = settings.incremental?.mode;
  return mode === "full" || mode === "off" ? mode : "legacy";
}

export function normalizeIncrementalChange(
  previousText: string,
  change: AspIncrementalChange,
): NormalizedIncrementalChange | undefined {
  const startOffset = change.rangeOffset ?? offsetAt(previousText, change.range.start);
  const rangeLength = change.rangeLength ?? offsetAt(previousText, change.range.end) - startOffset;
  const endOffset = startOffset + rangeLength;
  if (
    startOffset < 0 ||
    endOffset < startOffset ||
    startOffset > previousText.length ||
    endOffset > previousText.length
  ) {
    return undefined;
  }
  return { ...change, startOffset, endOffset, rangeOffset: startOffset, rangeLength };
}

export function normalizeIncrementalChanges(
  previousText: string,
  changes: readonly AspIncrementalChange[],
): NormalizedIncrementalChange[] | undefined {
  const normalized = changes.map((change) => normalizeIncrementalChange(previousText, change));
  if (normalized.some((change) => change === undefined)) {
    return undefined;
  }
  return normalized as NormalizedIncrementalChange[];
}

export function applyIncrementalChanges(
  previousText: string,
  changes: readonly AspIncrementalChange[],
): string {
  return [...changes]
    .map((change) => normalizeIncrementalChange(previousText, change))
    .filter((change): change is NormalizedIncrementalChange => Boolean(change))
    .sort((left, right) => right.startOffset - left.startOffset)
    .reduce(
      (text, change) =>
        `${text.slice(0, change.startOffset)}${change.text}${text.slice(change.endOffset)}`,
      previousText,
    );
}

export function changesOverlap(changes: readonly NormalizedIncrementalChange[]): boolean {
  const sorted = [...changes].sort((left, right) => left.startOffset - right.startOffset);
  for (let index = 1; index < sorted.length; index += 1) {
    if (sorted[index - 1].endOffset > sorted[index].startOffset) {
      return true;
    }
  }
  return false;
}

export function changeHull(changes: readonly NormalizedIncrementalChange[]): DamageSpan {
  return {
    start: Math.min(...changes.map((change) => change.startOffset)),
    end: Math.max(...changes.map((change) => change.endOffset)),
  };
}

export function rangeOverlaps(
  startOffset: number,
  endOffset: number,
  rangeStart: number,
  rangeEnd: number,
): boolean {
  return startOffset < rangeEnd && endOffset > rangeStart;
}

export function rangeOverlapsOrTouches(
  startOffset: number,
  endOffset: number,
  rangeStart: number,
  rangeEnd: number,
): boolean {
  return startOffset === endOffset
    ? startOffset >= rangeStart && startOffset <= rangeEnd
    : startOffset < rangeEnd && endOffset > rangeStart;
}

export function shiftOffsetAfterChange(
  offset: number,
  startOffset: number,
  endOffset: number,
  delta: number,
): number {
  if (offset < startOffset) {
    return offset;
  }
  if (offset >= endOffset) {
    return offset + delta;
  }
  return startOffset;
}
