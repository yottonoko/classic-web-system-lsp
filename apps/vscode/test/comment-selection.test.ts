import { describe, expect, it } from "vitest";
import { expandCommentSelectionOffsetsAfterEdits } from "../src/comment-selection";

describe("expandCommentSelectionOffsetsAfterEdits", () => {
  it("includes comment text added around a selected line", () => {
    const source = "🍰 <%= title %>";
    const commented = "<!-- 🍰 <%'= title %> -->";

    expect(
      expandCommentSelectionOffsetsAfterEdits(
        [{ start: 0, end: source.length }],
        [{ start: 0, end: source.length, newTextLength: commented.length }],
      ),
    ).toEqual([{ start: 0, end: commented.length }]);
  });

  it("accounts for earlier edits across multiple selections", () => {
    expect(
      expandCommentSelectionOffsetsAfterEdits(
        [
          { start: 0, end: 5 },
          { start: 11, end: 16 },
        ],
        [
          { start: 0, end: 5, newTextLength: 9 },
          { start: 11, end: 16, newTextLength: 13 },
        ],
      ),
    ).toEqual([
      { start: 0, end: 9 },
      { start: 15, end: 28 },
    ]);
  });

  it("includes insertions at both selection boundaries", () => {
    expect(
      expandCommentSelectionOffsetsAfterEdits(
        [{ start: 10, end: 20 }],
        [
          { start: 10, end: 10, newTextLength: 4 },
          { start: 20, end: 20, newTextLength: 3 },
        ],
      ),
    ).toEqual([{ start: 10, end: 27 }]);
  });

  it("keeps empty cursor entries under VS Code tracking", () => {
    expect(
      expandCommentSelectionOffsetsAfterEdits(
        [undefined, { start: 10, end: 20 }],
        [
          { start: 0, end: 5, newTextLength: 0 },
          { start: 10, end: 20, newTextLength: 16 },
        ],
      ),
    ).toEqual([undefined, { start: 5, end: 21 }]);
  });
});
