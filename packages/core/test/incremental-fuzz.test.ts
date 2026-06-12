import { describe, expect, it } from "vitest";
import { parseAspDocument, updateAspParsedDocument } from "../src";
import { positionAt } from "../src/position";

const seed = 0x5eed_2026;

describe("full incremental parser fuzz", () => {
  it("matches a fresh parse after seeded random edits", () => {
    let random = mulberry32(seed);
    let text = `<%@ LANGUAGE="VBScript" %>
<html>
<head>
<style>body { color: red; }</style>
<script>const value = "😀";</script>
</head>
<body>
<!-- #include file="common.inc" -->
<% Option Explicit
Dim message
message = "hello"
Response.Write message
%>
</body>
</html>`;
    let parsed = parseAspDocument("file:///site/incremental-fuzz.asp", text);

    for (let step = 0; step < 2_000; step += 1) {
      const start = randomOffset(text, random);
      const maxDelete = text.length > 2_500 ? 24 : 8;
      const deleteLength = Math.min(text.length - start, Math.floor(random() * maxDelete));
      const end = start + deleteLength;
      const replacement = randomReplacement(random, text.length);
      const change = {
        range: { start: positionAt(text, start), end: positionAt(text, end) },
        rangeOffset: start,
        rangeLength: end - start,
        text: replacement,
      };
      text = `${text.slice(0, start)}${replacement}${text.slice(end)}`;
      const updated = updateAspParsedDocument(parsed, [change], {
        incremental: { mode: "full" },
      });
      const fresh = parseAspDocument("file:///site/incremental-fuzz.asp", text);

      try {
        expect(updated.parsed).toEqual(fresh);
      } catch (error) {
        throw new Error(
          `incremental fuzz mismatch at step ${step}, seed ${seed}, impact=${JSON.stringify(
            updated.impact,
          )}: ${error instanceof Error ? error.message : String(error)}`,
        );
      }
      parsed = updated.parsed;
      random = mulberry32((seed + step + text.length) >>> 0);
    }
  });
});

function randomOffset(text: string, random: () => number): number {
  return Math.floor(random() * (text.length + 1));
}

function randomReplacement(random: () => number, currentLength: number): string {
  const fragments = [
    "",
    "x",
    " value",
    "\n",
    "😀",
    "<%",
    "%>",
    `<% Response.Write "fuzz" %>`,
    `<!-- #include file="fuzz.inc" -->`,
    `<script>const fuzz = 1;</script>`,
    `<style>.fuzz { color: blue; }</style>`,
    `style="color: coral;"`,
    currentLength > 2_500 ? "" : `"${Math.floor(random() * 1000)}"`,
  ];
  return fragments[Math.floor(random() * fragments.length)];
}

function mulberry32(seedValue: number): () => number {
  let value = seedValue;
  return () => {
    value += 0x6d2b79f5;
    let result = value;
    result = Math.imul(result ^ (result >>> 15), result | 1);
    result ^= result + Math.imul(result ^ (result >>> 7), result | 61);
    return ((result ^ (result >>> 14)) >>> 0) / 4294967296;
  };
}
