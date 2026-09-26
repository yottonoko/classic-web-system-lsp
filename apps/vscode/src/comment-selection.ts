interface OffsetRange {
  start: number;
  end: number;
}

interface OffsetTextEdit extends OffsetRange {
  newTextLength: number;
}

/** Maps selected line spans through comment edits, including text inserted at either boundary. */
export function expandCommentSelectionOffsetsAfterEdits(
  selections: readonly (OffsetRange | undefined)[],
  edits: readonly OffsetTextEdit[],
): Array<OffsetRange | undefined> {
  const orderedEdits = [...edits].sort(
    (left, right) => left.start - right.start || left.end - right.end,
  );
  return selections.map((selection) => {
    if (!selection) {
      return undefined;
    }
    return {
      start: mapSelectionBoundary(selection.start, orderedEdits, "start"),
      end: mapSelectionBoundary(selection.end, orderedEdits, "end"),
    };
  });
}

function mapSelectionBoundary(
  offset: number,
  edits: readonly OffsetTextEdit[],
  boundary: "start" | "end",
): number {
  let delta = 0;
  for (const edit of edits) {
    const finalStart = edit.start + delta;
    if (boundary === "start") {
      if (offset <= edit.start) {
        return offset + delta;
      }
      if (offset < edit.end) {
        return finalStart;
      }
    } else {
      if (offset < edit.start) {
        return offset + delta;
      }
      if (offset <= edit.end) {
        return finalStart + edit.newTextLength;
      }
    }
    delta += edit.newTextLength - (edit.end - edit.start);
  }
  return offset + delta;
}
