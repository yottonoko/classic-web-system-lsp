import { describe, expect, it } from "vitest";
import { highlightedCodeLines, type HighlightedCode } from "../src/webview/highlighted-code";

function source(tokens: HighlightedCode["tokens"]): HighlightedCode {
  return {
    tokens,
    value: "",
    code: "",
    lang: "asp",
    meta: "",
    themeName: "light",
    style: {},
    annotations: [],
  };
}

describe("Solid source rendering data", () => {
  it("preserves blank and trailing lines, indentation, and Unicode source text", () => {
    const lines = highlightedCodeLines(
      source([["  ' 日本語 😀\r\n", "#888"], "\r\n", ["value = 42", "#222"], "\n"]),
    );
    expect(lines.map((line) => line.map((token) => token.text).join(""))).toEqual([
      "  ' 日本語 😀",
      "",
      "value = 42",
      "",
    ]);
    expect(lines[0][0].color).toBe("#888");
  });

  it("keeps CRLF as one newline when tokens split at the boundary", () => {
    const lines = highlightedCodeLines(source(["first\r", "", ["\nsecond", "#222"]]));
    expect(lines.map((line) => line.map((token) => token.text).join(""))).toEqual([
      "first",
      "second",
    ]);
  });

  it("retains HTML-like code as literal text for the renderer", () => {
    const literal = '<script>alert("example")</script>&nbsp;';
    expect(highlightedCodeLines(source([[literal, "#222"]]))).toEqual([
      [{ text: literal, color: "#222" }],
    ]);
  });
});
