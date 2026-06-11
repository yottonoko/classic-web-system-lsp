import { describe, expect, it } from "vitest";
import { offsetAt, positionAt, rangeFromOffsets } from "../src/position";

describe("position helpers", () => {
  it("round-trips UTF-16 offsets and positions", () => {
    for (const text of ["", "a\nb", "a\r\nb", "😀\nvalue"]) {
      for (let offset = 0; offset <= text.length; offset += 1) {
        expect(offsetAt(text, positionAt(text, offset))).toBe(offset);
      }
    }
  });

  it("builds ranges from offsets", () => {
    const text = "😀\nvalue";
    const start = text.indexOf("value");
    const end = text.length;

    expect(rangeFromOffsets(text, start, end)).toEqual({
      start: { line: 1, character: 0 },
      end: { line: 1, character: "value".length },
    });
  });
});
