import { describe, expect, it } from "vitest";
import {
  analyzeVbscript,
  buildVirtualDocuments,
  getVbscriptCompletions,
  parseAspDocument,
  parseVbscriptDocument,
} from "../../core/src";

describe("language server building blocks", () => {
  it("can parse and route a representative Classic ASP document", () => {
    const source = `<%@ LANGUAGE="VBScript" %>
<html>
<head><style>body { color: red; }</style></head>
<body>
<script>const value = document.title;</script>
<% Option Explicit
Dim userName
Response.Write userName
%>
</body>
</html>`;
    const parsed = parseAspDocument("file:///site/default.asp", source);
    const virtuals = buildVirtualDocuments(parsed);
    expect(virtuals.get("html")?.text).toContain("<html>");
    expect(virtuals.get("css")?.text).toContain("color");
    expect(virtuals.get("javascript")?.text).toContain("document.title");
    expect(analyzeVbscript(parsed).diagnostics).toHaveLength(0);
    expect(
      getVbscriptCompletions(parsed, { line: 7, character: 9 }).some(
        (item) => item.label === "Write",
      ),
    ).toBe(true);
  });

  it("can parse and route a standalone VBS document", () => {
    const parsed = parseVbscriptDocument("file:///site/script.vbs", "WScript.\n");
    const completions = getVbscriptCompletions(parsed, { line: 0, character: 8 });
    expect(completions.some((item) => item.label === "Echo")).toBe(true);
    expect(
      getVbscriptCompletions(parsed, { line: 1, character: 0 }).some(
        (item) => item.label === "Response",
      ),
    ).toBe(false);
  });
});
