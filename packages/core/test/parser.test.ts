import { describe, expect, it } from "vitest";
import { CompletionItemKind, DiagnosticTag, InsertTextFormat } from "vscode-languageserver-types";
import {
  analyzeVbscript,
  buildVbTypeEnvironment,
  buildVirtualDocuments,
  collectVbscriptPublicSymbols,
  collectVbscriptSymbols,
  collectVbscriptSymbolsAsync,
  extractAspIncludeRefs,
  extractVbscriptSymbolIndex,
  formatAspDocument,
  formatAspRange,
  getClassicAspLineCommentEdits,
  getVbscriptCompletions,
  getVbscriptDefinition,
  getVbscriptDocumentationQuickAction,
  getVbscriptHover,
  getVbscriptInlayHints,
  getVbscriptReferences,
  getVbscriptReferencesForSymbol,
  getVbscriptReferencesForSymbols,
  getVbscriptSelectionRanges,
  getVbscriptSemanticTokens,
  getVbscriptSignatureHelp,
  getVbscriptTypeDefinition,
  hydrateVbscriptCst,
  needsVbscriptCstHydration,
  parseAspCst,
  parseAspDocument,
  parseAspDocumentSkeletonAsync,
  parseVbscriptTypeRef,
  parseVbscriptCst,
  prepareVbscriptCallHierarchy,
  resolveVbscriptCompletionItem,
  shiftAspRangeAfterChange,
  summarizeAspFileAnalysis,
  summarizeAspFileAnalysisAsync,
  updateAspParsedDocument,
  vbscriptReferenceSymbolKey,
} from "../src";
import type { AspCstNode, VbCstNode } from "../src";

function collectVbTokenTexts(node: AspCstNode): string[] {
  return [
    ...(node.vbscript?.tokens.map((token) => token.text) ?? []),
    ...node.children.flatMap((child) => collectVbTokenTexts(child)),
  ];
}

describe("parseAspDocument", () => {
  it("marks skeleton parses as needing VBScript CST hydration before direct CST walks", async () => {
    const parsed = await parseAspDocumentSkeletonAsync(
      "file:///site/skeleton.asp",
      `<%
Dim Greeting
Greeting = "hello"
%>`,
    );
    expect(needsVbscriptCstHydration(parsed)).toBe(true);
    expect(collectVbTokenTexts(parsed.cst)).toHaveLength(0);

    await hydrateVbscriptCst(parsed);
    expect(needsVbscriptCstHydration(parsed)).toBe(false);
    expect(collectVbTokenTexts(parsed.cst)).toEqual(
      expect.arrayContaining(["Dim", "Greeting", '"hello"']),
    );
  });

  it("keeps async VBScript summary and symbol helpers usable for skeleton parses", async () => {
    const parsed = await parseAspDocumentSkeletonAsync(
      "file:///site/async-analysis-skeleton.asp",
      `<%
Function BuildTitle(name)
  BuildTitle = "Hello " & name
End Function
%>`,
    );
    expect(needsVbscriptCstHydration(parsed)).toBe(true);

    const symbols = await collectVbscriptSymbolsAsync(parsed);
    const summary = await summarizeAspFileAnalysisAsync(parsed);

    expect(symbols.find((symbol) => symbol.name === "BuildTitle")?.kind).toBe("function");
    expect(
      summary.vbscript?.localSymbols.find((symbol) => symbol.name === "BuildTitle")?.kind,
    ).toBe("function");
    expect(needsVbscriptCstHydration(parsed)).toBe(false);
  });

  it("updates safe HTML edits incrementally while shifting later include ranges", () => {
    const source = `<div>hello</div>
<!-- #include file="common.inc" -->
<% Response.Write "ok" %>`;
    const parsed = parseAspDocument("file:///site/default.asp", source);
    const start = source.indexOf("hello") + "hello".length;
    const change = {
      range: { start: positionAt(source, start), end: positionAt(source, start) },
      rangeOffset: start,
      rangeLength: 0,
      text: " world",
    };
    const updated = updateAspParsedDocument(parsed, [change]);
    const expectedText = source.replace("hello", "hello world");

    expect(updated.impact).toMatchObject({
      kind: "incremental",
      reason: "safe content edit",
      language: "html",
    });
    expect(updated.parsed).toEqual(parseAspDocument("file:///site/default.asp", expectedText));
  });

  it("updates safe VBScript edits incrementally", () => {
    const source = `<%
Dim message
message = "ok"
Response.Write message
%>`;
    const parsed = parseAspDocument("file:///site/default.asp", source);
    const start = source.indexOf('"ok"') + 1;
    const end = start + "ok".length;
    const change = {
      range: { start: positionAt(source, start), end: positionAt(source, end) },
      rangeOffset: start,
      rangeLength: end - start,
      text: "ready",
    };
    const updated = updateAspParsedDocument(parsed, [change]);
    const expectedText = `${source.slice(0, start)}ready${source.slice(end)}`;

    expect(updated.impact).toMatchObject({ kind: "incremental", language: "vbscript" });
    expect(updated.parsed).toEqual(parseAspDocument("file:///site/default.asp", expectedText));
  });

  it("falls back to full parse for unsafe incremental edits", () => {
    const source = `<%@ LANGUAGE="VBScript" %>
<script>const value = 1;</script>
<!-- #include file="common.inc" -->
<% Response.Write "ok" %>`;
    const parsed = parseAspDocument("file:///site/default.asp", source);
    const cases = [
      { needle: "VBScript", text: "JScript", reason: "ASP directive edit" },
      { needle: "value = 1", text: "value = '</script>'", reason: "boundary text inserted" },
      { needle: "common.inc", text: "other.inc", reason: "include directive edit" },
      { needle: '"ok"', text: "a".repeat(257), reason: "large edit" },
    ];

    for (const testCase of cases) {
      const start = source.indexOf(testCase.needle);
      const end = start + testCase.needle.length;
      const updated = updateAspParsedDocument(parsed, [
        {
          range: { start: positionAt(source, start), end: positionAt(source, end) },
          rangeOffset: start,
          rangeLength: end - start,
          text: testCase.text,
        },
      ]);
      expect(updated.impact).toMatchObject({ kind: "full", reason: testCase.reason });
    }

    const first = source.indexOf("value = 1");
    const second = source.indexOf('"ok"');
    const multi = updateAspParsedDocument(parsed, [
      {
        range: {
          start: positionAt(source, first),
          end: positionAt(source, first + "value".length),
        },
        rangeOffset: first,
        rangeLength: "value".length,
        text: "clientValue",
      },
      {
        range: {
          start: positionAt(source, second),
          end: positionAt(source, second + '"ok"'.length),
        },
        rangeOffset: second,
        rangeLength: '"ok"'.length,
        text: '"ready"',
      },
    ]);
    expect(multi.impact).toMatchObject({ kind: "full", reason: "multiple changes" });
  });

  it("falls back when an edit touches an embedded region content boundary", () => {
    const source = `<script>const value = 1;</script>`;
    const parsed = parseAspDocument("file:///site/default.asp", source);
    const script = parsed.regions.find((region) => region.kind === "client-script");
    expect(script).toBeTruthy();
    const updated = updateAspParsedDocument(parsed, [
      {
        range: {
          start: positionAt(source, script!.contentStart),
          end: positionAt(source, script!.contentStart),
        },
        rangeOffset: script!.contentStart,
        rangeLength: 0,
        text: "/* header */",
      },
    ]);

    expect(updated.impact).toMatchObject({ kind: "full", reason: "region boundary edit" });
  });

  it("shifts UTF-16 ranges after incremental changes", () => {
    const source = "😀\nResponse.Write value";
    const start = source.indexOf("Response");
    const range = {
      start: positionAt(source, start),
      end: positionAt(source, start + "Response".length),
    };
    const change = {
      range: { start: positionAt(source, 0), end: positionAt(source, 0) },
      rangeOffset: 0,
      rangeLength: 0,
      text: "prefix\n",
    };
    const shifted = shiftAspRangeAfterChange(range, source, `${change.text}${source}`, change);

    expect(shifted.start).toEqual({ line: 2, character: 0 });
    expect(shifted.end).toEqual({ line: 2, character: "Response".length });
  });

  it("detects ASP blocks, directives, includes, style and script regions", () => {
    const source = `<%@ LANGUAGE="VBScript" %>
<!-- #include file="inc/common.inc" -->
<html>
<style>body { color: red; }</style>
<script>const answer = 42;</script>
<% Option Explicit
Dim name
Response.Write name
%>
</html>`;
    const parsed = parseAspDocument("file:///site/default.asp", source);
    expect(parsed.defaultLanguage).toBe("VBScript");
    expect(parsed.includes).toHaveLength(1);
    expect(parsed.includes[0].directiveRange.start).toEqual(
      positionAt(source, source.indexOf("#include")),
    );
    expect(parsed.includes[0].modeRange.start).toEqual(positionAt(source, source.indexOf("file")));
    expect(parsed.includes[0].pathRange.start).toEqual(
      positionAt(source, source.indexOf('"inc/common.inc"')),
    );
    expect(parsed.directives[0].attributes.language).toBe("VBScript");
    expect(
      parsed.regions.some((region) => region.kind === "style" && region.language === "css"),
    ).toBe(true);
    expect(
      parsed.regions.some(
        (region) => region.kind === "client-script" && region.language === "javascript",
      ),
    ).toBe(true);
    expect(
      parsed.regions.some(
        (region) => region.kind === "asp-block" && region.language === "vbscript",
      ),
    ).toBe(true);
  });

  it("extracts include references without full ASP parsing", () => {
    const source = `<%@ LANGUAGE="VBScript" %>\r
<!-- #include file="inc/共通.inc" -->\r
<!-- #INCLUDE file="fallback.inc" virtual="/shared/virtual.inc" -->\r
<script>
const ignoredString = '<!-- #include file="script-string.inc" -->';
// <!-- #include file="script-line-comment.inc" -->
/* <!-- #include file="script-block-comment.inc" --> */
</script>\r
<style>
body::before { content: "<!-- #include file='style-string.inc' -->"; }
/* <!-- #include file="style-comment.inc" --> */
</style>\r
<div data-include="<!-- #include file='attribute.inc' -->"></div>\r
<% Response.Write "<!-- #include file=""asp.inc"" -->" %>\r
<% ' <!-- #include file="vbscript-comment.inc" --> %>\r
<!-- #include file='single.inc' -->\r
<!-- #include file=plain.inc -->`;
    const parsed = parseAspDocument("file:///site/default.asp", source);
    const includes = extractAspIncludeRefs(source);

    expect(includes).toEqual(parsed.includes);
    expect(includes.map((include) => `${include.mode}:${include.path}`)).toEqual([
      "file:inc/共通.inc",
      "virtual:/shared/virtual.inc",
      "file:single.inc",
      "file:plain.inc",
    ]);
  });

  it("detects root script regions between ASP procedure blocks", () => {
    const source = `<% Sub A() %>
<script>
const a = 10;
console.log(a);
</script>
<% End Sub %>`;
    const parsed = parseAspDocument("file:///site/sub-script.asp", source);
    const script = parsed.regions.find((region) => region.kind === "client-script");
    expect(script?.language).toBe("javascript");
    expect(script && source.slice(script.contentStart, script.contentEnd)).toContain(
      "console.log(a);",
    );

    const javascript = buildVirtualDocuments(parsed).get("javascript");
    expect(javascript?.text).toContain("const a = 10;");
    expect(javascript?.text).toContain("console.log(a);");
  });

  it("builds virtual documents with source maps", () => {
    const source = `<div><%= title %></div><style>.x { color: red }</style>`;
    const parsed = parseAspDocument("file:///site/default.asp", source);
    const docs = buildVirtualDocuments(parsed);
    expect(docs.get("html")?.text).toContain("<div>");
    expect(docs.get("html")?.text).toContain("<style>");
    expect(docs.get("html")?.text).toContain("</style>");
    expect(docs.get("html")?.text).not.toContain(".x { color: red }");
    expect(docs.get("html")?.text).not.toContain("title");
    expect(docs.get("css")?.text).toContain("color");
    expect(docs.get("css")?.text).not.toContain("</style>");
  });

  it("masks inline ASP inside CSS regions while keeping ASP completions routable", () => {
    const source = `<style>.x { color: <%= themeColor %>; }</style>`;
    const parsed = parseAspDocument("file:///site/default.asp", source);
    const docs = buildVirtualDocuments(parsed);
    expect(docs.get("css")?.text).toContain(".x { color:");
    expect(docs.get("css")?.text).not.toContain("themeColor");
    expect(parsed.regions.some((region) => region.kind === "asp-expression")).toBe(true);
  });

  it("masks inline ASP inside JavaScript regions with source-map stable placeholders", () => {
    const source = `<script>const value = <%= serverValue %>;
const label = "<%= serverLabel %>";
const dynamic = <% Response.Write clientValue %>;</script>`;
    const parsed = parseAspDocument("file:///site/default.asp", source);
    const docs = buildVirtualDocuments(parsed);
    const javascript = docs.get("javascript");
    expect(javascript?.text).toContain("const value = 0");
    expect(javascript?.text).toContain('const label = "0');
    expect(javascript?.text).toContain("const dynamic = 0");
    expect(javascript?.text).not.toContain("serverValue");
    expect(javascript?.text).not.toContain("Response.Write");
    expect(javascript?.text).toHaveLength(
      source.slice(source.indexOf(">") + 1, source.lastIndexOf("</script>")).length + 1,
    );
    expect(parsed.regions.filter((region) => region.kind === "asp-expression")).toHaveLength(2);
    expect(parsed.regions.filter((region) => region.kind === "asp-block")).toHaveLength(1);
  });

  it("keeps CSS and JavaScript virtual documents stable after ASP comments inside comments", () => {
    const source = `<style>
/* <% 'css comment %> */
.next { color: red; }
</style>
<script>
// <% 'js line comment %>
const lineCommentNext = 1;
/* <% 'js block comment %> */
const blockCommentNext = 2;
</script>`;
    const parsed = parseAspDocument("file:///site/commented-asp-islands.asp", source);
    const docs = buildVirtualDocuments(parsed);
    const aspBlocks = parsed.regions.filter((region) => region.kind === "asp-block");
    expect(aspBlocks.map((region) => source.slice(region.start, region.end))).toEqual([
      "<% 'css comment %>",
      "<% 'js line comment %>",
      "<% 'js block comment %>",
    ]);
    expect(docs.get("css")?.text).toContain(".next { color: red; }");
    expect(docs.get("css")?.text).not.toContain("css comment");
    expect(docs.get("javascript")?.text).toContain("const lineCommentNext = 1;");
    expect(docs.get("javascript")?.text).toContain("const blockCommentNext = 2;");
    expect(docs.get("javascript")?.text).not.toContain("js line comment");
    expect(docs.get("javascript")?.text).not.toContain("js block comment");
  });

  it("leaves inline ASP islands unmapped inside JavaScript virtual documents", () => {
    const source = `<script>
const n = "<%= RenderTierOptions(selectedTier: filter.Tier) %>";
document.querySelectorAll(".customer-row").forEach((row) => row.classList.add("is-hovered"));
</script>`;
    const parsed = parseAspDocument("file:///site/default.asp", source);
    const javascript = buildVirtualDocuments(parsed).get("javascript");
    expect(javascript).toBeTruthy();
    const expression = parsed.regions.find((region) => region.kind === "asp-expression");
    expect(expression).toBeTruthy();
    expect(
      javascript!.sourceMap.toVirtualOffset(source.indexOf("RenderTierOptions")),
    ).toBeUndefined();
    expect(javascript!.sourceMap.toVirtualOffset(source.indexOf("selectedTier"))).toBeUndefined();
    expect(
      javascript!.sourceMap.toSourceOffset(source.indexOf("<%=") - source.indexOf("<script>") - 7),
    ).toBeUndefined();
    expect(javascript!.sourceMap.toVirtualOffset(source.indexOf("const n"))).toBeDefined();
    expect(
      javascript!.sourceMap.toVirtualOffset(source.indexOf("document.querySelectorAll")),
    ).toBeDefined();
  });

  it("masks ASP expressions inside HTML tag attributes", () => {
    const source =
      '<input type="checkbox" name="inactive" value="1" <%= CheckedAttribute(filter.IncludeInactive) %>>';
    const parsed = parseAspDocument("file:///site/default.asp", source);
    const docs = buildVirtualDocuments(parsed);
    const html = docs.get("html");
    expect(parsed.regions.filter((region) => region.kind === "asp-expression")).toHaveLength(1);
    expect(html?.text).not.toContain("CheckedAttribute");
    expect(html?.sourceMap.toVirtualOffset(source.indexOf("CheckedAttribute"))).toBeUndefined();
    expect(html?.sourceMap.toVirtualOffset(source.lastIndexOf(">"))).toBeDefined();
  });

  it("masks quoted and generated ASP tag attributes with delimiter-like content", () => {
    const source = `<input <%= AttributeWithText("data-end=>") %> data-state="<%= StateName() %>" <% Response.Write "disabled" %>>`;
    const parsed = parseAspDocument("file:///site/default.asp", source);
    const html = buildVirtualDocuments(parsed).get("html");
    expect(parsed.regions.filter((region) => region.kind === "asp-expression")).toHaveLength(2);
    expect(parsed.regions.filter((region) => region.kind === "asp-block")).toHaveLength(1);
    expect(html?.text).not.toContain("AttributeWithText");
    expect(html?.text).not.toContain("StateName");
    expect(html?.text).not.toContain("Response.Write");
    expect(html?.sourceMap.toVirtualOffset(source.indexOf("data-state"))).toBeDefined();
    expect(html?.sourceMap.toVirtualOffset(source.lastIndexOf(">"))).toBeDefined();
  });

  it("masks inline ASP inside CSS values and style attributes", () => {
    const source = `<style>.x { color: <%= themeColor %>; width: <% Response.Write width %>px; }</style>
<div style="color: <%= themeColor %>; background: red"></div>`;
    const parsed = parseAspDocument("file:///site/default.asp", source);
    const docs = buildVirtualDocuments(parsed);
    const css = docs.get("css")?.text ?? "";
    expect(css).toContain(".x { color: x");
    expect(css).toContain("width: x");
    expect(css).toContain("*{color: x");
    expect(css).not.toContain("themeColor");
    expect(css).not.toContain("Response.Write");
    expect(
      parsed.regions.some(
        (region) => region.kind === "style-attribute" && region.language === "css",
      ),
    ).toBe(true);
    expect(parsed.regions.filter((region) => region.kind === "asp-expression")).toHaveLength(2);
    expect(parsed.regions.filter((region) => region.kind === "asp-block")).toHaveLength(1);
  });

  it("extracts inline style attributes as CSS virtual documents", () => {
    const source = `<div style="color: red; display: block"></div>`;
    const parsed = parseAspDocument("file:///site/default.asp", source);
    const docs = buildVirtualDocuments(parsed);
    expect(
      parsed.regions.some(
        (region) => region.kind === "style-attribute" && region.language === "css",
      ),
    ).toBe(true);
    expect(docs.get("css")?.text).toContain("*{color: red; display: block}");
  });

  it("extracts inline style attributes before and after the html root", () => {
    const source = `<header style="color: #00f"></header>
<html><body></body></html>
<footer style='background: #fff'></footer>
<% If showRow Then %>
<tr style="border-color: #0f0"><td>x</td></tr>
<% End If %>`;
    const parsed = parseAspDocument("file:///site/outside-root.asp", source);
    const docs = buildVirtualDocuments(parsed);
    const styles = parsed.regions.filter((region) => region.kind === "style-attribute");
    expect(styles.map((region) => source.slice(region.contentStart, region.contentEnd))).toEqual([
      "color: #00f",
      "background: #fff",
      "border-color: #0f0",
    ]);
    expect(docs.get("css")?.text).toContain("*{color: #00f}");
    expect(docs.get("css")?.text).toContain("*{background: #fff}");
    expect(docs.get("css")?.text).toContain("*{border-color: #0f0}");
  });

  it("reports missing ASP close delimiter", () => {
    const parsed = parseAspDocument("file:///broken.asp", "<html><% Response.Write 1");
    expect(parsed.diagnostics[0]?.message).toContain("closing %>");
  });

  it("ignores ASP open delimiters inside client script and style strings or comments", () => {
    const source = `<script>
const literal = "<%";
const angleText = "<>";
// <% not an ASP island
const value = <%= serverValue %>;
</script>
<style>
.literal::before { content: "<%"; }
/* <% not an ASP island */
.dynamic { width: <%= width %>px; }
</style>`;
    const parsed = parseAspDocument("file:///site/client-literal-delimiters.asp", source);
    expect(parsed.diagnostics).toHaveLength(0);
    expect(parsed.regions.filter((region) => region.kind === "asp-expression")).toHaveLength(2);
    expect(parsed.regions.some((region) => region.kind === "client-script")).toBe(true);
    expect(parsed.regions.some((region) => region.kind === "style")).toBe(true);
    const docs = buildVirtualDocuments(parsed);
    expect(docs.get("javascript")?.text).toContain('const literal = "<%";');
    expect(docs.get("css")?.text).toContain('content: "<%";');
  });

  it("localizes ASP parser diagnostics", () => {
    const parsed = parseAspDocument("file:///broken.asp", "<html><% Response.Write 1", {
      resolvedLocale: "ja",
    });
    expect(parsed.diagnostics[0]?.message).toContain("閉じ区切り");
  });

  it("closes ASP regions at raw delimiters inside script strings", () => {
    const source = `<%
Response.Write "%>"
Response.Write "done"
%>
<%@ LANGUAGE="JScript" %><% var text = '%>'; Response.Write(text); %>`;
    const parsed = parseAspDocument("file:///site/delimiters.asp", source);
    expect(parsed.diagnostics).toHaveLength(0);
    const blocks = parsed.regions.filter(
      (region) => region.kind === "asp-block" || region.kind === "asp-directive",
    );
    expect(blocks).toHaveLength(3);
    expect(blocks[0].end).toBe(source.indexOf("%>") + "%>".length);
    expect(source.slice(blocks[0].contentStart, blocks[0].contentEnd)).toContain(
      'Response.Write "',
    );
    expect(source.slice(blocks[0].contentStart, blocks[0].contentEnd)).not.toContain(
      'Response.Write "done"',
    );
    expect(source.slice(blocks[2].contentStart, blocks[2].contentEnd)).toContain("var text = '");
    expect(source.slice(blocks[2].contentStart, blocks[2].contentEnd)).not.toContain(
      "Response.Write(text)",
    );
    const html = parsed.regions
      .filter((region) => region.kind === "html")
      .map((region) => source.slice(region.start, region.end))
      .join("");
    expect(html).toContain('Response.Write "done"');
    expect(html).toContain("Response.Write(text)");
  });

  it("closes ASP regions at delimiters on VBScript comment lines", () => {
    const source = `<%
' comment with %>
<div>done</div>`;
    const parsed = parseAspDocument("file:///site/vb-comment-close.asp", source);
    expect(parsed.diagnostics).toHaveLength(0);
    const block = parsed.regions.find((region) => region.kind === "asp-block");
    expect(block).toBeDefined();
    expect(block?.end).toBe(source.indexOf("%>") + "%>".length);
    expect(source.slice(block?.contentStart, block?.contentEnd)).toContain("' comment with ");
    expect(source.slice(block?.contentStart, block?.contentEnd)).not.toContain("<div>");
    expect(parsed.regions.some((region) => region.kind === "html")).toBe(true);
  });

  it("keeps HTML text after inline VBScript comment delimiters", () => {
    const source = `<div>
<%' あいうえお %>
テキスト
</div>`;
    const parsed = parseAspDocument("file:///site/inline-vb-comment-close.asp", source);
    expect(parsed.diagnostics).toHaveLength(0);
    const block = parsed.regions.find((region) => region.kind === "asp-block");
    expect(block?.end).toBe(source.indexOf("%>") + "%>".length);
    const html = parsed.regions
      .filter((region) => region.kind === "html")
      .map((region) => source.slice(region.start, region.end))
      .join("");
    expect(html).toContain("テキスト");
  });

  it("closes JScript ASP regions at delimiters inside comments", () => {
    const source = `<%@ LANGUAGE="JScript" %>
<%
// line comment with %>
Response.Write("line")
%>
<%
/* block comment with %> */
Response.Write("block")
%>`;
    const parsed = parseAspDocument("file:///site/jscript-comment-close.asp", source);
    expect(parsed.diagnostics).toHaveLength(0);
    const blocks = parsed.regions.filter((region) => region.kind === "asp-block");
    expect(blocks).toHaveLength(2);
    const lineBlock = source.slice(blocks[0].contentStart, blocks[0].contentEnd);
    const blockCommentBlock = source.slice(blocks[1].contentStart, blocks[1].contentEnd);
    expect(lineBlock).toContain("// line comment with ");
    expect(lineBlock).not.toContain('Response.Write("line")');
    expect(blockCommentBlock).toContain("/* block comment with ");
    expect(blockCommentBlock).not.toContain('Response.Write("block")');
    const html = parsed.regions
      .filter((region) => region.kind === "html")
      .map((region) => source.slice(region.start, region.end))
      .join("");
    expect(html).toContain('Response.Write("line")');
    expect(html).toContain('Response.Write("block")');
  });

  it("closes ASP regions at raw delimiters inside non-primary comment syntax", () => {
    const source = `<%
/* block comment with %> */
// line comment with %>
Response.Write "done"
%>`;
    const parsed = parseAspDocument("file:///site/comment-delimiters.asp", source);
    expect(parsed.diagnostics).toHaveLength(0);
    const blocks = parsed.regions.filter((region) => region.kind === "asp-block");
    expect(blocks).toHaveLength(1);
    expect(blocks[0].end).toBe(source.indexOf("%>") + "%>".length);
    expect(source.slice(blocks[0].contentStart, blocks[0].contentEnd)).toContain(
      "/* block comment with ",
    );
    expect(source.slice(blocks[0].contentStart, blocks[0].contentEnd)).not.toContain(
      'Response.Write "done"',
    );
  });

  it("closes script and style regions at raw closing tags inside strings or comments", () => {
    const source = `<script>
const literal = "</script>";
// </script>
const ok = true;
</script>
<style>
.x::before { content: "</style>"; }
/* </style> */
.x { color: red; }
</style>`;
    const parsed = parseAspDocument("file:///site/tags.asp", source);
    const script = parsed.regions.find((region) => region.kind === "client-script");
    const style = parsed.regions.find((region) => region.kind === "style");
    expect(script?.end).toBe(source.indexOf("</script>") + "</script>".length);
    expect(style?.end).toBe(source.indexOf("</style>") + "</style>".length);
    expect(script && source.slice(script.contentStart, script.contentEnd)).not.toContain(
      "const ok = true;",
    );
    expect(style && source.slice(style.contentStart, style.contentEnd)).not.toContain("color: red");
  });

  it("does not close script and style regions at raw closing tags inside ASP islands", () => {
    const source = `<script>
<%
Do While ready
Response.Write "</script>"
%>
const afterAsp = true;
<%
Loop
%>
</script>
<style>
<%
Do While ready
Response.Write "</style>"
%>
.after-asp { color: red; }
<%
Loop
%>
</style>`;
    const parsed = parseAspDocument("file:///site/asp-island-raw-script-close.asp", source);
    expect(parsed.diagnostics).toHaveLength(0);
    const script = parsed.regions.find((region) => region.kind === "client-script");
    const style = parsed.regions.find((region) => region.kind === "style");
    expect(script?.end).toBe(
      source.indexOf("</script>", source.indexOf("const afterAsp")) + "</script>".length,
    );
    expect(style?.end).toBe(
      source.indexOf("</style>", source.indexOf(".after-asp")) + "</style>".length,
    );
    expect(script && source.slice(script.contentStart, script.contentEnd)).toContain(
      "const afterAsp = true;",
    );
    expect(style && source.slice(style.contentStart, style.contentEnd)).toContain(
      ".after-asp { color: red; }",
    );
    expect(parsed.regions.filter((region) => region.kind === "asp-block")).toHaveLength(4);
  });

  it("closes server-side JScript regions at raw script end tags inside strings", () => {
    const source = `<%@ LANGUAGE="JScript" %>
<script runat="server" language="JScript">
function render() {
  var literal = "</script>";
  Response.Write(literal);
}
</script>`;
    const parsed = parseAspDocument("file:///site/server-jscript-script-close.asp", source);
    expect(parsed.diagnostics).toHaveLength(0);
    const script = parsed.regions.find((region) => region.kind === "server-script");
    expect(script?.language).toBe("jscript");
    expect(script?.end).toBe(source.indexOf("</script>") + "</script>".length);
    expect(script && source.slice(script.contentStart, script.contentEnd)).toContain(
      'var literal = "',
    );
    expect(script && source.slice(script.contentStart, script.contentEnd)).not.toContain(
      "Response.Write(literal)",
    );
  });

  it("keeps explicit server script language even when page default is different", () => {
    const parsed = parseAspDocument(
      "file:///mixed.asp",
      `<%@ LANGUAGE="JScript" %><script runat="server" language="VBScript">Dim value</script>`,
    );
    const serverScript = parsed.regions.find((region) => region.kind === "server-script");
    expect(serverScript?.language).toBe("vbscript");
  });

  it("does not parse server-side JScript as VBScript CST", () => {
    const parsed = parseAspDocument(
      "file:///server-jscript.asp",
      `<%@ LANGUAGE="JScript" %><% var missingName = 1; %>`,
    );
    const jscriptNode = parsed.cst.children.find((node) => node.language === "jscript");
    expect(jscriptNode?.vbscript).toBeUndefined();
    expect(analyzeVbscript(parsed).diagnostics).toHaveLength(0);
  });

  it("builds a lossless ASP CST with embedded VBScript CST nodes", () => {
    const source = `<%@ LANGUAGE="VBScript" %>
<!-- #include file="inc/common.inc" -->
<script runat="server">
Sub Save(value)
End Sub
</script>
<style>.x { color: red; }</style>
<div style="display: block"><%= title %></div>`;
    const cst = parseAspCst("file:///site/default.asp", source);
    expect(cst.text).toBe(source);
    expect(cst.children.some((node) => node.kind === "IncludeDirective")).toBe(true);
    expect(cst.children.some((node) => node.kind === "StyleAttribute")).toBe(true);
    const server = cst.children.find((node) => node.kind === "ServerScriptElement");
    expect(server?.vbscript?.children.some((node) => node.kind === "Procedure")).toBe(true);
  });
});

describe("VBScript analysis", () => {
  it("builds error-tolerant VBScript CST declarations and preserves trivia tokens", () => {
    const cst = parseVbscriptCst(`Class Broken
  Public Name
  Sub Save(value)
' trailing comment`);
    expect(cst.tokens.some((token) => token.kind === "comment")).toBe(true);
    expect(
      cst.children.some((node) => node.kind === "Class" && node.nameToken?.text === "Broken"),
    ).toBe(true);
    expect(
      cst.children
        .flatMap((node) => node.children)
        .some((node) => node.kind === "Procedure" && node.nameToken?.text === "Save"),
    ).toBe(true);
  });

  it("treats Rem as a VBScript line comment only at statement starts", () => {
    const cst = parseVbscriptCst(`Rem leading comment
value = 1 : Rem trailing comment
rEm mixed case comment
Reminder = 2
value = Rem`);
    const comments = cst.tokens
      .filter((token) => token.kind === "comment")
      .map((token) => token.text);
    expect(comments).toEqual([
      "Rem leading comment",
      "Rem trailing comment",
      "rEm mixed case comment",
    ]);
    expect(
      cst.tokens.some((token) => token.kind === "identifier" && token.text === "Reminder"),
    ).toBe(true);
    expect(cst.tokens.some((token) => token.kind === "keyword" && token.text === "Rem")).toBe(true);
  });

  it("builds VBScript statement CST nodes for blocks, calls and assignments", () => {
    const cst = parseVbscriptCst(`If ready Then
  Call Save(name)
End If
Select Case kind
End Select
Do While ready
Loop
While ready
Wend
For index = 1 To 3
Next
For Each item In items
Next
value = _
  other`);
    const allNodes = flattenVbNodes(cst).map((node) => node.kind);
    expect(allNodes).toContain("If");
    expect(allNodes).toContain("Call");
    expect(allNodes).toContain("Select");
    expect(allNodes).toContain("DoLoop");
    expect(allNodes).toContain("While");
    expect(allNodes).toContain("For");
    expect(allNodes).toContain("ForEach");
    expect(allNodes).toContain("Assignment");
  });

  it("completes built-ins and declared symbols", () => {
    const source = `<% Option Explicit
Dim customerName
Response. %>`;
    const parsed = parseAspDocument("file:///site/default.asp", source);
    const completions = getVbscriptCompletions(parsed, { line: 2, character: 9 });
    expect(completions.some((item) => item.label === "Write")).toBe(true);
    expect(completions.find((item) => item.label === "Write")?.labelDetails?.description).toBe(
      "built-in",
    );
    const topLevelCompletions = getVbscriptCompletions(parsed, { line: 1, character: 4 });
    expect(
      topLevelCompletions.find((item) => item.label === "Response")?.labelDetails?.description,
    ).toBe("built-in");
    expect(
      topLevelCompletions.find((item) => item.label === "CStr")?.labelDetails?.description,
    ).toBe("built-in");
    expect(topLevelCompletions.find((item) => item.label === "Nothing")).toMatchObject({
      kind: CompletionItemKind.Constant,
      labelDetails: { description: "built-in" },
    });
    expect(resolveVbscriptCompletionItem({ label: "CStr" }, parsed).labelDetails?.description).toBe(
      "built-in",
    );
    expect(topLevelCompletions.find((item) => item.label === "Dim")?.labelDetails).toBeUndefined();
    expect(
      topLevelCompletions.find((item) => item.label === "customerName")?.labelDetails,
    ).toBeUndefined();
    expect(topLevelCompletions.some((item) => item.label === "customerName")).toBe(true);
  });

  it("completes VBScript syntax snippets when enabled", () => {
    const source = `<% Option Explicit

Response.
%>`;
    const parsed = parseAspDocument("file:///site/default.asp", source);
    const completions = getVbscriptCompletions(parsed, { line: 1, character: 0 });
    const snippetLabels = [
      "If Then",
      "If Then Else",
      "Do Loop",
      "Do While Loop",
      "Do Until Loop",
      "Do Loop While",
      "Do Loop Until",
      "For Next",
      "For Each Next",
      "Select Case",
      "With",
      "Sub",
      "Function",
      "Class",
      "Property Get",
      "Property Let",
      "Property Set",
    ];
    expect(completions.map((item) => item.label)).toEqual(expect.arrayContaining(snippetLabels));
    const ifSnippet = completions.find((item) => item.label === "If Then");
    expect(ifSnippet).toMatchObject({
      kind: CompletionItemKind.Snippet,
      insertTextFormat: InsertTextFormat.Snippet,
      detail: "VBScript syntax snippet",
    });
    expect(ifSnippet?.insertText).toContain("End If");
    expect(completions.find((item) => item.label === "If")).toMatchObject({
      kind: CompletionItemKind.Keyword,
      detail: "VBScript syntax keyword",
    });
    const doSnippet = completions.find((item) => item.label === "Do While Loop");
    expect(doSnippet).toMatchObject({
      kind: CompletionItemKind.Snippet,
      insertTextFormat: InsertTextFormat.Snippet,
      detail: "VBScript syntax snippet",
    });
    expect(doSnippet?.insertText).toContain("Loop");

    const disabled = getVbscriptCompletions(
      parsed,
      { line: 1, character: 0 },
      {
        syntaxSnippets: false,
      },
    );
    expect(disabled.some((item) => item.label === "If Then")).toBe(false);
    expect(disabled.some((item) => item.label === "If")).toBe(true);
    expect(disabled.some((item) => item.label === "Response")).toBe(true);
    expect(disabled.some((item) => item.label === "Dim")).toBe(true);

    const keywordsDisabled = getVbscriptCompletions(
      parsed,
      { line: 1, character: 0 },
      {
        syntaxKeywords: false,
      },
    );
    expect(keywordsDisabled.some((item) => item.label === "If Then")).toBe(true);
    expect(keywordsDisabled.some((item) => item.label === "If")).toBe(false);

    const memberCompletions = getVbscriptCompletions(parsed, { line: 2, character: 9 });
    expect(memberCompletions.some((item) => item.label === "Write")).toBe(true);
    expect(memberCompletions.some((item) => item.label === "If Then")).toBe(false);
    expect(memberCompletions.some((item) => item.label === "If")).toBe(false);
  });

  it("keeps VBScript syntax snippets available while typing statement prefixes", () => {
    const source = `<% Option Explicit
Do▮
%>`;
    const marked = markedDocument(source);
    const parsed = parseAspDocument("file:///site/default.asp", marked.text);
    const doCompletions = getVbscriptCompletions(parsed, marked.position);
    expect(doCompletions.find((item) => item.label === "Do")).toMatchObject({
      kind: CompletionItemKind.Keyword,
    });
    expect(doCompletions.find((item) => item.label === "Do Loop")).toMatchObject({
      kind: CompletionItemKind.Snippet,
      insertTextFormat: InsertTextFormat.Snippet,
    });
    expect(doCompletions.find((item) => item.label === "Do While Loop")).toMatchObject({
      kind: CompletionItemKind.Snippet,
    });
    expect(doCompletions.find((item) => item.label === "Do Until Loop")).toMatchObject({
      kind: CompletionItemKind.Snippet,
    });

    const sub = markedDocument(`<% Option Explicit
Sub▮
%>`);
    const subCompletions = getVbscriptCompletions(
      parseAspDocument("file:///site/default.asp", sub.text),
      sub.position,
    );
    expect(subCompletions.find((item) => item.label === "Sub")).toMatchObject({
      kind: CompletionItemKind.Snippet,
      insertTextFormat: InsertTextFormat.Snippet,
    });

    const fn = markedDocument(`<% Option Explicit
Funct▮
%>`);
    const functionCompletions = getVbscriptCompletions(
      parseAspDocument("file:///site/default.asp", fn.text),
      fn.position,
    );
    expect(functionCompletions.find((item) => item.label === "Function")).toMatchObject({
      kind: CompletionItemKind.Snippet,
      insertTextFormat: InsertTextFormat.Snippet,
    });

    const disabled = getVbscriptCompletions(parsed, marked.position, {
      syntaxSnippets: false,
    });
    expect(disabled.some((item) => item.label === "Do Loop")).toBe(false);
    expect(disabled.some((item) => item.label === "Do")).toBe(true);
  });

  it("completes VBScript On Error statements as syntax keywords", () => {
    const source = `<%
On
On Error${" "}
On Error R
On Error G
%>`;
    const parsed = parseAspDocument("file:///site/on-error-completion.asp", source);
    const onCompletions = getVbscriptCompletions(
      parsed,
      positionAt(source, source.indexOf("On") + "On".length),
    );
    expect(onCompletions.map((item) => item.label)).toEqual(
      expect.arrayContaining(["On Error Resume Next", "On Error GoTo 0"]),
    );
    expect(onCompletions.find((item) => item.label === "On Error Resume Next")).toMatchObject({
      kind: CompletionItemKind.Keyword,
      detail: "VBScript syntax keyword",
      textEdit: {
        newText: "On Error Resume Next",
      },
    });

    const resumeCompletions = getVbscriptCompletions(
      parsed,
      positionAt(source, source.indexOf("On Error R") + "On Error R".length),
    );
    expect(resumeCompletions.map((item) => item.label)).toEqual(["On Error Resume Next"]);

    const gotoCompletions = getVbscriptCompletions(
      parsed,
      positionAt(source, source.indexOf("On Error G") + "On Error G".length),
    );
    expect(gotoCompletions.map((item) => item.label)).toEqual(["On Error GoTo 0"]);

    const disabled = getVbscriptCompletions(
      parsed,
      positionAt(source, source.indexOf("On Error R") + "On Error R".length),
      { syntaxKeywords: false },
    );
    expect(disabled.some((item) => item.label === "On Error Resume Next")).toBe(false);
  });

  it("completes matching End block labels for open VBScript blocks", () => {
    const source = `<%
Function Render()
If ready Then
end
%>`;
    const parsed = parseAspDocument("file:///site/end-completion.asp", source);
    const endOffset = source.lastIndexOf("end") + "end".length;
    const completions = getVbscriptCompletions(parsed, positionAt(source, endOffset));
    expect(completions.map((item) => item.label)).toEqual(
      expect.arrayContaining(["End", "End If", "End Function"]),
    );
    expect(completions.find((item) => item.label === "End")).toMatchObject({
      kind: CompletionItemKind.Keyword,
      detail: "VBScript syntax keyword",
      textEdit: {
        newText: "End",
      },
    });
    expect(completions.find((item) => item.label === "End If")).toMatchObject({
      kind: CompletionItemKind.Snippet,
      detail: "VBScript syntax snippet",
      textEdit: {
        newText: "End If",
      },
    });

    const suffixSource = `<%
Function Render()
End F
%>`;
    const suffixParsed = parseAspDocument("file:///site/end-function-completion.asp", suffixSource);
    const suffixCompletions = getVbscriptCompletions(
      suffixParsed,
      positionAt(suffixSource, suffixSource.indexOf("End F") + "End F".length),
    );
    expect(suffixCompletions.find((item) => item.label === "End Function")).toMatchObject({
      filterText: "Function",
      textEdit: {
        newText: "End Function",
      },
    });

    const afterEndSource = `<%
Function Render()
If ready Then
${"End "}
%>`;
    const afterEndParsed = parseAspDocument(
      "file:///site/after-end-completion.asp",
      afterEndSource,
    );
    const afterEndCompletions = getVbscriptCompletions(
      afterEndParsed,
      positionAt(afterEndSource, afterEndSource.indexOf("End ") + "End ".length),
    );
    expect(afterEndCompletions.map((item) => item.label)).toEqual(["End If", "End Function"]);
    expect(afterEndCompletions.some((item) => item.kind === CompletionItemKind.Keyword)).toBe(
      false,
    );

    const trailingSpaceSource = `<%
Function Render()
If ready Then
  end   
%>`;
    const trailingSpaceParsed = parseAspDocument(
      "file:///site/end-completion-trailing-space.asp",
      trailingSpaceSource,
    );
    const trailingSpaceCompletions = getVbscriptCompletions(
      trailingSpaceParsed,
      positionAt(trailingSpaceSource, trailingSpaceSource.indexOf("end   ") + "end   ".length),
    );
    expect(trailingSpaceCompletions.map((item) => item.label)).toEqual(
      expect.arrayContaining(["End If", "End Function"]),
    );

    const blockedSource = `<%
Function Render()
Do
end
%>`;
    const blockedParsed = parseAspDocument(
      "file:///site/end-blocked-completion.asp",
      blockedSource,
    );
    const blockedEndOffset = blockedSource.lastIndexOf("end") + "end".length;
    const blockedCompletions = getVbscriptCompletions(
      blockedParsed,
      positionAt(blockedSource, blockedEndOffset),
    );
    expect(blockedCompletions.some((item) => item.label === "End Function")).toBe(false);

    const disabled = getVbscriptCompletions(parsed, positionAt(source, endOffset), {
      syntaxSnippets: false,
    });
    expect(disabled.some((item) => item.label === "End If")).toBe(false);
    expect(disabled.find((item) => item.label === "End")).toMatchObject({
      kind: CompletionItemKind.Keyword,
    });

    const keywordsDisabled = getVbscriptCompletions(parsed, positionAt(source, endOffset), {
      syntaxKeywords: false,
    });
    expect(keywordsDisabled.some((item) => item.label === "End")).toBe(false);
    expect(keywordsDisabled.some((item) => item.label === "End If")).toBe(true);
  });

  it("completes matching End labels for VBScript procedures, classes and members", () => {
    const subSource = `<%
Sub Render()
end s
%>`;
    const subParsed = parseAspDocument("file:///site/end-sub-completion.asp", subSource);
    const subCompletions = getVbscriptCompletions(
      subParsed,
      positionAt(subSource, subSource.indexOf("end s") + "end s".length),
    );
    expect(subCompletions.find((item) => item.label === "End Sub")).toMatchObject({
      filterText: "Sub",
      textEdit: {
        newText: "End Sub",
      },
    });

    const classSource = `<%
Class Widget
end c
%>`;
    const classParsed = parseAspDocument("file:///site/end-class-completion.asp", classSource);
    const classCompletions = getVbscriptCompletions(
      classParsed,
      positionAt(classSource, classSource.indexOf("end c") + "end c".length),
    );
    expect(classCompletions.find((item) => item.label === "End Class")).toMatchObject({
      filterText: "Class",
      textEdit: {
        newText: "End Class",
      },
    });

    const methodSource = `<%
Class Widget
Public Function Render()
end f
End Class
%>`;
    const methodParsed = parseAspDocument(
      "file:///site/end-class-method-completion.asp",
      methodSource,
    );
    const methodCompletions = getVbscriptCompletions(
      methodParsed,
      positionAt(methodSource, methodSource.indexOf("end f") + "end f".length),
    );
    expect(methodCompletions.map((item) => item.label)).toEqual(["End Function"]);
    expect(methodCompletions[0]?.filterText).toBe("Function");

    const propertySource = `<%
Class Widget
Property Get Name()
end p
End Class
%>`;
    const propertyParsed = parseAspDocument(
      "file:///site/end-property-completion.asp",
      propertySource,
    );
    const propertyCompletions = getVbscriptCompletions(
      propertyParsed,
      positionAt(propertySource, propertySource.indexOf("end p") + "end p".length),
    );
    expect(propertyCompletions.map((item) => item.label)).toEqual(["End Property"]);
    expect(propertyCompletions[0]?.filterText).toBe("Property");
  });

  it("completes VBScript block continuation and closing keywords only when usable", () => {
    const thenSource = `<%
If ready 
%>`;
    const thenParsed = parseAspDocument("file:///site/then-completion.asp", thenSource);
    const thenCompletions = getVbscriptCompletions(
      thenParsed,
      positionAt(thenSource, thenSource.indexOf("ready") + "ready ".length),
    );
    expect(thenCompletions.map((item) => item.label)).toEqual(["Then"]);
    expect(thenCompletions[0]).toMatchObject({
      kind: CompletionItemKind.Keyword,
      textEdit: {
        newText: "Then",
      },
    });

    const partialThenSource = `<%
ElseIf ready th
%>`;
    const partialThenParsed = parseAspDocument(
      "file:///site/partial-then-completion.asp",
      partialThenSource,
    );
    const partialThenCompletions = getVbscriptCompletions(
      partialThenParsed,
      positionAt(partialThenSource, partialThenSource.indexOf("th") + "th".length),
    );
    expect(partialThenCompletions[0]?.textEdit).toMatchObject({
      newText: "Then",
    });

    const completeThenSource = `<%
If ready Then
%>`;
    const completeThenParsed = parseAspDocument(
      "file:///site/complete-then-completion.asp",
      completeThenSource,
    );
    const completeThenCompletions = getVbscriptCompletions(
      completeThenParsed,
      positionAt(completeThenSource, completeThenSource.indexOf("Then") + "Then".length),
    );
    expect(completeThenCompletions.some((item) => item.label === "Then")).toBe(false);

    const ifBlockSource = `<%
If ready Then
e
End If
%>`;
    const ifBlockParsed = parseAspDocument("file:///site/if-keyword-completion.asp", ifBlockSource);
    const ifBlockCompletions = getVbscriptCompletions(
      ifBlockParsed,
      positionAt(ifBlockSource, ifBlockSource.indexOf("\ne\n") + "\ne".length),
    );
    expect(ifBlockCompletions.map((item) => item.label)).toEqual(
      expect.arrayContaining(["ElseIf", "Else", "End", "End If"]),
    );
    expect(ifBlockCompletions.find((item) => item.label === "ElseIf")).toMatchObject({
      kind: CompletionItemKind.Keyword,
    });
    expect(ifBlockCompletions.find((item) => item.label === "End")).toMatchObject({
      kind: CompletionItemKind.Keyword,
    });

    const selectSource = `<%
Select Case value
c
End Select
%>`;
    const selectParsed = parseAspDocument(
      "file:///site/select-keyword-completion.asp",
      selectSource,
    );
    const selectCompletions = getVbscriptCompletions(
      selectParsed,
      positionAt(selectSource, selectSource.indexOf("\nc\n") + "\nc".length),
    );
    expect(selectCompletions.find((item) => item.label === "Case")).toMatchObject({
      kind: CompletionItemKind.Keyword,
      textEdit: {
        newText: "Case",
      },
    });

    const loopSource = `<%
Do
lo
%>`;
    const loopParsed = parseAspDocument("file:///site/loop-completion.asp", loopSource);
    const loopCompletions = getVbscriptCompletions(
      loopParsed,
      positionAt(loopSource, loopSource.indexOf("lo") + "lo".length),
    );
    expect(loopCompletions.map((item) => item.label)).toEqual(["Loop"]);
    expect(loopCompletions.find((item) => item.kind === CompletionItemKind.Keyword)).toMatchObject({
      label: "Loop",
      textEdit: {
        newText: "Loop",
      },
    });

    const loopTrailingSpaceSource = `<%
Do
  lo  
%>`;
    const loopTrailingSpaceParsed = parseAspDocument(
      "file:///site/loop-completion-trailing-space.asp",
      loopTrailingSpaceSource,
    );
    const loopTrailingSpaceCompletions = getVbscriptCompletions(
      loopTrailingSpaceParsed,
      positionAt(loopTrailingSpaceSource, loopTrailingSpaceSource.indexOf("lo  ") + "lo  ".length),
    );
    expect(loopTrailingSpaceCompletions.map((item) => item.label)).toEqual(["Loop"]);

    const whileSource = `<%
While ready
we
%>`;
    const whileParsed = parseAspDocument("file:///site/wend-completion.asp", whileSource);
    const whileCompletions = getVbscriptCompletions(
      whileParsed,
      positionAt(whileSource, whileSource.indexOf("we") + "we".length),
    );
    expect(whileCompletions.map((item) => item.label)).toEqual(["Wend"]);
    expect(whileCompletions.find((item) => item.kind === CompletionItemKind.Keyword)).toMatchObject(
      {
        label: "Wend",
      },
    );

    const forSource = `<%
Sub Render()
For index = 1 To 3
n
%>`;
    const forParsed = parseAspDocument("file:///site/next-completion.asp", forSource);
    const forCompletions = getVbscriptCompletions(
      forParsed,
      positionAt(forSource, forSource.lastIndexOf("n") + "n".length),
    );
    expect(forCompletions.map((item) => item.label)).toEqual(["Next"]);
    expect(forCompletions.find((item) => item.kind === CompletionItemKind.Keyword)).toMatchObject({
      label: "Next",
    });
    expect(forCompletions.some((item) => item.label === "End Sub")).toBe(false);

    const blockedEndSource = `<%
Sub Render()
For index = 1 To 3
end
%>`;
    const blockedEndParsed = parseAspDocument(
      "file:///site/blocked-end-completion.asp",
      blockedEndSource,
    );
    const blockedEndCompletions = getVbscriptCompletions(
      blockedEndParsed,
      positionAt(blockedEndSource, blockedEndSource.lastIndexOf("end") + "end".length),
    );
    expect(blockedEndCompletions.some((item) => item.label === "End Sub")).toBe(false);

    const disabled = getVbscriptCompletions(
      thenParsed,
      positionAt(thenSource, thenSource.indexOf("ready") + "ready ".length),
      { syntaxKeywords: false },
    );
    expect(disabled.some((item) => item.label === "Then")).toBe(false);

    const snippetDisabledLoopCompletions = getVbscriptCompletions(
      loopParsed,
      positionAt(loopSource, loopSource.indexOf("lo") + "lo".length),
      { syntaxSnippets: false },
    );
    expect(snippetDisabledLoopCompletions.map((item) => item.label)).toEqual(["Loop"]);
    expect(snippetDisabledLoopCompletions[0]?.kind).toBe(CompletionItemKind.Keyword);

    const keywordDisabledLoopCompletions = getVbscriptCompletions(
      loopParsed,
      positionAt(loopSource, loopSource.indexOf("lo") + "lo".length),
      { syntaxKeywords: false },
    );
    expect(keywordDisabledLoopCompletions.map((item) => item.label)).toEqual(["Loop"]);
    expect(keywordDisabledLoopCompletions[0]?.kind).toBe(CompletionItemKind.Snippet);
  });

  it("treats server-side object tags as typed VBScript globals", () => {
    const source = `<object runat="server" id="rs" progid="ADODB.Recordset"></object>
<%
rs.
%>`;
    const parsed = parseAspDocument("file:///site/server-object.asp", source);
    expect(parsed.serverObjects).toHaveLength(1);
    expect(parsed.serverObjects[0]).toMatchObject({
      id: "rs",
      progId: "ADODB.Recordset",
    });
    const symbols = collectVbscriptSymbols(parsed);
    expect(symbols.find((symbol) => symbol.name === "rs")).toMatchObject({
      kind: "variable",
      typeName: "ADODB.Recordset",
      explicitType: true,
    });
    const completions = getVbscriptCompletions(
      parsed,
      positionAt(source, source.indexOf("rs.") + "rs.".length),
      { symbols },
    );
    expect(completions.some((item) => item.label === "MoveNext")).toBe(true);
    expect(
      getVbscriptHover(parsed, positionAt(source, source.indexOf("rs.")), { symbols }),
    ).toContain("Dim rs As ADODB.Recordset");
  });

  it("identifies fixed and dynamic VBScript arrays as Array symbols", () => {
    const source = `<%
Dim fixedItems(10)
Dim dynamicItems()
ReDim resizedItems(5)
%>`;
    const parsed = parseAspDocument("file:///site/arrays.asp", source);
    const symbols = collectVbscriptSymbols(parsed);
    const fixed = symbols.find((symbol) => symbol.name === "fixedItems");
    const dynamic = symbols.find((symbol) => symbol.name === "dynamicItems");
    const resized = symbols.find((symbol) => symbol.name === "resizedItems");
    expect(fixed).toMatchObject({
      typeName: "Array",
      array: { kind: "fixed", dimensions: ["10"] },
    });
    expect(dynamic).toMatchObject({
      typeName: "Array",
      array: { kind: "dynamic", dimensions: [] },
    });
    expect(resized).toMatchObject({
      typeName: "Array",
      array: { kind: "dynamic", dimensions: ["5"] },
    });
    expect(
      getVbscriptHover(parsed, positionAt(source, source.indexOf("fixedItems") + 2), { symbols }),
    ).toContain("Dim fixedItems(10) As Array(10)");
    expect(
      getVbscriptHover(parsed, positionAt(source, source.indexOf("dynamicItems") + 2), {
        symbols,
      }),
    ).toContain("Dim dynamicItems() As Array");
    const hints = getVbscriptInlayHints(
      parsed,
      { start: { line: 0, character: 0 }, end: { line: 5, character: 0 } },
      { symbols },
    );
    const arrayHintPositions = hints
      .filter((hint) => typeof hint.label === "string" && hint.label.includes(" As Array"))
      .map((hint) => hint.position);
    expect(arrayHintPositions).toEqual(
      expect.arrayContaining([
        positionAt(source, source.indexOf("fixedItems(10)") + "fixedItems(10)".length),
        positionAt(source, source.indexOf("dynamicItems()") + "dynamicItems()".length),
        positionAt(source, source.indexOf("resizedItems(5)") + "resizedItems(5)".length),
      ]),
    );
    // The fixed-size array shows its dimensions and the hint sits after the array suffix.
    expect(hints.find((hint) => hint.label === " (global) As Array(10)")?.position).toEqual(
      positionAt(source, source.indexOf("fixedItems(10)") + "fixedItems(10)".length),
    );
  });

  it("renders fixed multi-dimensional arrays as Array(rows, cols)", () => {
    const source = `<%
Dim matrix(2, 3)
%>`;
    const parsed = parseAspDocument("file:///site/array-display.asp", source);
    const symbols = collectVbscriptSymbols(parsed);
    // The canonical type stays "Array"; only the displayed type carries dimensions.
    expect(symbols.find((symbol) => symbol.name === "matrix")).toMatchObject({
      typeName: "Array",
      array: { kind: "fixed", dimensions: ["2", "3"] },
    });
    expect(
      getVbscriptHover(parsed, positionAt(source, source.indexOf("matrix") + 2), { symbols }),
    ).toContain("Dim matrix(2, 3) As Array(2, 3)");
  });

  it("builds a VBScript symbol index with declarations, references, calls, and deferred include candidates", () => {
    const source = `<!-- #include file="common.inc" -->
<%
Const SiteName = "demo"
Dim GlobalValue
Class User
  Public displayName
  Private Const Kind = "user"
  Public Function Render(value)
    Dim localValue
    Render = value & displayName
  End Function
  Public Property Get Title()
    Title = displayName
  End Property
  Public Sub Save()
  End Sub
End Class

Sub Log(message)
  Dim localValue
  localValue = message
End Sub

Function MakeUser(name)
  Dim user
  Set user = New User
  Call Log(name)
  Log name
  user.Render(name)
  MissingRead
  MissingWrite = name
  MakeUser = user
End Function
%>`;
    const index = extractVbscriptSymbolIndex("file:///site/default.asp", source);
    const declaration = (name: string, kind: string) =>
      index.declarations.find((item) => item.name === name && item.kind === kind);

    expect(index.includeRefs.map((include) => include.path)).toEqual(["common.inc"]);
    expect(declaration("User", "class")).toBeDefined();
    expect(declaration("Render", "method")).toMatchObject({ memberOf: "User" });
    expect(declaration("Render", "method")).toMatchObject({ procedureKind: "function" });
    expect(declaration("Title", "property")).toMatchObject({ memberOf: "User" });
    expect(declaration("Title", "property")).toMatchObject({ procedureKind: "property" });
    expect(declaration("Save", "method")).toMatchObject({
      memberOf: "User",
      procedureKind: "sub",
    });
    expect(declaration("displayName", "field")).toMatchObject({ memberOf: "User" });
    expect(declaration("SiteName", "constant")).toMatchObject({ bindingScope: "global" });
    expect(declaration("GlobalValue", "variable")).toMatchObject({ bindingScope: "global" });
    expect(declaration("name", "parameter")).toMatchObject({ bindingScope: "local" });
    expect(declaration("Render", "method")?.sourceRange).toMatchObject({
      start: { line: 7 },
      end: { line: 10 },
    });
    expect(declaration("Title", "property")?.sourceRange).toMatchObject({
      start: { line: 11 },
      end: { line: 13 },
    });
    expect(declaration("Log", "sub")?.sourceRange).toMatchObject({
      start: { line: 18 },
      end: { line: 21 },
    });
    expect(declaration("MakeUser", "function")?.sourceRange).toMatchObject({
      start: { line: 23 },
      end: { line: 32 },
    });
    expect(declaration("SiteName", "constant")?.sourceRange).toBeUndefined();
    expect(
      index.declarations.filter(
        (item) => item.name === "localValue" && item.bindingScope === "local",
      ),
    ).toHaveLength(2);

    const userClass = declaration("User", "class");
    expect(
      index.references.find((item) => item.name === "User" && item.role === "new"),
    ).toMatchObject({
      resolvedId: userClass?.id,
    });
    expect(index.callSites.filter((item) => item.name === "Log" && item.resolvedId)).toHaveLength(
      2,
    );
    expect(index.callSites.find((item) => item.name === "Render")).toMatchObject({
      callKind: "member",
      receiverName: "user",
      deferredKey: expect.any(String),
    });
    expect(index.deferredExternalRefs.find((item) => item.name === "MissingRead")).toMatchObject({
      bindingScope: "unknown",
      expectedKinds: ["variable", "constant"],
      role: "read",
    });
    expect(index.deferredExternalRefs.find((item) => item.name === "MissingWrite")).toMatchObject({
      bindingScope: "unknown",
      expectedKinds: ["variable"],
      role: "write",
    });
  });

  type SymbolIndexExpected = Record<string, unknown> & {
    name: string;
    kind?: string;
    role?: string;
  };
  type SymbolIndexAbsent = {
    name: string;
    kind?: string;
    role?: string;
  };
  type SymbolIndexSpecialCase = {
    name: string;
    source: string;
    settings?: Parameters<typeof extractVbscriptSymbolIndex>[2];
    options?: Parameters<typeof extractVbscriptSymbolIndex>[3];
    declarations?: SymbolIndexExpected[];
    absentDeclarations?: SymbolIndexAbsent[];
    references?: SymbolIndexExpected[];
    callSites?: SymbolIndexExpected[];
    deferredExternalRefs?: SymbolIndexExpected[];
  };
  const symbolIndexSpecialCases: SymbolIndexSpecialCase[] = [
    {
      name: "server object with lowercase attributes becomes a typed global",
      source: `<object runat="server" id="rs" progid="ADODB.Recordset"></object>
<% rs.MoveNext %>`,
      declarations: [
        {
          name: "rs",
          kind: "variable",
          bindingScope: "global",
          typeName: "ADODB.Recordset",
        },
      ],
      references: [{ name: "rs", role: "read", bindingScope: "global" }],
      callSites: [{ name: "MoveNext", callKind: "member", receiverName: "rs" }],
    },
    {
      name: "RegExp assignments add internal types without source declarations for built-ins",
      source: `<%
Dim re, created, matches, firstMatch
Set re = New RegExp
Set created = CreateObject("VBScript.RegExp")
Set matches = re.Execute("abc")
Set firstMatch = matches.Item(0)
%>`,
      declarations: [
        { name: "re", kind: "variable", typeName: "RegExp" },
        { name: "created", kind: "variable", typeName: "RegExp" },
        { name: "matches", kind: "variable", typeName: "Matches" },
        { name: "firstMatch", kind: "variable", typeName: "Match" },
      ],
      absentDeclarations: [{ name: "RegExp" }],
      callSites: [
        { name: "RegExp", callKind: "constructor" },
        { name: "Execute", callKind: "member", receiverName: "re" },
        { name: "Item", callKind: "member", receiverName: "matches" },
      ],
    },
    {
      name: "server object with uppercase tag and attributes keeps the id",
      source: `<OBJECT RUNAT="server" ID="UpperObj" CLASSID="clsid:00000000"></OBJECT>`,
      declarations: [{ name: "UpperObj", kind: "variable", bindingScope: "global" }],
    },
    {
      name: "server object accepts single quoted attributes",
      source: `<object runat='server' id='singleObj' progid='Scripting.Dictionary'></object>`,
      declarations: [
        {
          name: "singleObj",
          kind: "variable",
          bindingScope: "global",
          typeName: "Scripting.Dictionary",
        },
      ],
    },
    {
      name: "server object accepts unquoted attributes",
      source: `<object runat=server id=unquotedObj progid=RepositoryType></object>`,
      declarations: [
        {
          name: "unquotedObj",
          kind: "variable",
          bindingScope: "global",
          typeName: "RepositoryType",
        },
      ],
    },
    {
      name: "self-closing server object still becomes a declaration",
      source: `<object runat="server" id="selfObj" progid="RepositoryType" />`,
      declarations: [
        {
          name: "selfObj",
          kind: "variable",
          bindingScope: "global",
          typeName: "RepositoryType",
        },
      ],
    },
    {
      name: "server object tolerates multiline attributes",
      source: `<object
  runat="server"
  id="multiObj"
  progid="RepositoryType">
</object>`,
      declarations: [
        {
          name: "multiObj",
          kind: "variable",
          bindingScope: "global",
          typeName: "RepositoryType",
        },
      ],
    },
    {
      name: "server object id may start with underscore",
      source: `<object runat="server" id="_sessionObj" progid="RepositoryType"></object>`,
      declarations: [
        {
          name: "_sessionObj",
          kind: "variable",
          bindingScope: "global",
          typeName: "RepositoryType",
        },
      ],
    },
    {
      name: "server object id with a hyphen is ignored for VBScript",
      source: `<object runat="server" id="bad-name" progid="RepositoryType"></object>`,
      absentDeclarations: [{ name: "bad-name" }],
    },
    {
      name: "server object id starting with a digit is ignored for VBScript",
      source: `<object runat="server" id="1bad" progid="RepositoryType"></object>`,
      absentDeclarations: [{ name: "1bad" }],
    },
    {
      name: "object without runat server is ignored",
      source: `<object id="clientObj" progid="RepositoryType"></object>`,
      absentDeclarations: [{ name: "clientObj" }],
    },
    {
      name: "object with client runat is ignored",
      source: `<object runat="client" id="clientObj" progid="RepositoryType"></object>`,
      absentDeclarations: [{ name: "clientObj" }],
    },
    {
      name: "server object without id is ignored",
      source: `<object runat="server" progid="RepositoryType"></object>`,
      absentDeclarations: [{ name: "RepositoryType" }],
    },
    {
      name: "object inside an HTML comment is ignored",
      source: `<!-- <object runat="server" id="commentObj" progid="RepositoryType"></object> -->`,
      absentDeclarations: [{ name: "commentObj" }],
    },
    {
      name: "object markup inside an ASP string is ignored as HTML",
      source: `<%
Dim markup
markup = "<object runat=""server"" id=""stringObj"" progid=""RepositoryType""></object>"
%>`,
      declarations: [{ name: "markup", kind: "variable", bindingScope: "global" }],
      absentDeclarations: [{ name: "stringObj" }],
    },
    {
      name: "fixed Dim array records array metadata",
      source: `<%
Dim fixedItems(10)
%>`,
      declarations: [
        {
          name: "fixedItems",
          kind: "variable",
          bindingScope: "global",
          typeName: "Array",
          arrayKind: "fixed",
          arrayDimensions: ["10"],
        },
      ],
    },
    {
      name: "multi-dimensional Dim array keeps every dimension",
      source: `<%
Dim matrix(2, 3)
%>`,
      declarations: [
        {
          name: "matrix",
          kind: "variable",
          typeName: "Array",
          arrayKind: "fixed",
          arrayDimensions: ["2", "3"],
        },
      ],
    },
    {
      name: "dynamic Dim array records an empty dimension list",
      source: `<%
Dim dynamicItems()
%>`,
      declarations: [
        {
          name: "dynamicItems",
          kind: "variable",
          typeName: "Array",
          arrayKind: "dynamic",
          arrayDimensions: [],
        },
      ],
    },
    {
      name: "mixed Dim statement keeps scalar fixed and dynamic declarations separate",
      source: `<%
Dim scalarValue, fixedItems(1), dynamicItems()
%>`,
      declarations: [
        { name: "scalarValue", kind: "variable", bindingScope: "global" },
        {
          name: "fixedItems",
          kind: "variable",
          typeName: "Array",
          arrayKind: "fixed",
          arrayDimensions: ["1"],
        },
        {
          name: "dynamicItems",
          kind: "variable",
          typeName: "Array",
          arrayKind: "dynamic",
          arrayDimensions: [],
        },
      ],
    },
    {
      name: "array dimension expressions are preserved without whitespace",
      source: `<%
Dim values(count + 1, maxValue - 1)
%>`,
      declarations: [
        {
          name: "values",
          kind: "variable",
          arrayDimensions: ["count+1", "maxValue-1"],
        },
      ],
    },
    {
      name: "nested calls inside array dimensions stay within one dimension",
      source: `<%
Dim buckets(Left(name, 1), Right(name, 2))
%>`,
      declarations: [
        {
          name: "buckets",
          kind: "variable",
          arrayDimensions: ["Left(name,1)", "Right(name,2)"],
        },
      ],
    },
    {
      name: "sized ReDim is tracked as a dynamic array resize",
      source: `<%
ReDim resized(20)
%>`,
      declarations: [
        {
          name: "resized",
          kind: "variable",
          bindingScope: "global",
          typeName: "Array",
          arrayKind: "dynamic",
          arrayDimensions: ["20"],
        },
      ],
    },
    {
      name: "empty ReDim is tracked as a dynamic array",
      source: `<%
ReDim resized()
%>`,
      declarations: [
        {
          name: "resized",
          kind: "variable",
          typeName: "Array",
          arrayKind: "dynamic",
          arrayDimensions: [],
        },
      ],
    },
    {
      name: "ReDim Preserve skips Preserve and keeps the resized array",
      source: `<%
ReDim Preserve resized(30)
%>`,
      declarations: [
        {
          name: "resized",
          kind: "variable",
          typeName: "Array",
          arrayKind: "dynamic",
          arrayDimensions: ["30"],
        },
      ],
      absentDeclarations: [{ name: "Preserve" }],
    },
    {
      name: "lowercase redim preserve also skips preserve",
      source: `<%
redim preserve lowercaseItems(40)
%>`,
      declarations: [
        {
          name: "lowercaseItems",
          kind: "variable",
          arrayKind: "dynamic",
          arrayDimensions: ["40"],
        },
      ],
      absentDeclarations: [{ name: "preserve" }],
    },
    {
      name: "global ReDim keeps global binding scope",
      source: `<%
ReDim globalItems(1)
globalItems = Array()
%>`,
      declarations: [{ name: "globalItems", kind: "variable", bindingScope: "global" }],
      references: [{ name: "globalItems", role: "write", bindingScope: "global" }],
    },
    {
      name: "local ReDim keeps procedure-local binding scope",
      source: `<%
Sub Resize()
  ReDim localItems(1)
  localItems = Array()
End Sub
%>`,
      declarations: [{ name: "localItems", kind: "variable", bindingScope: "local" }],
      references: [{ name: "localItems", role: "write", bindingScope: "local" }],
    },
    {
      name: "public class array field keeps member metadata",
      source: `<%
Class Bag
  Public Items(5)
End Class
%>`,
      declarations: [
        {
          name: "Items",
          kind: "field",
          memberOf: "Bag",
          visibility: "public",
          typeName: "Array",
          arrayKind: "fixed",
          arrayDimensions: ["5"],
        },
      ],
    },
    {
      name: "private class dynamic array field keeps member metadata",
      source: `<%
Class Bag
  Private Items()
End Class
%>`,
      declarations: [
        {
          name: "Items",
          kind: "field",
          memberOf: "Bag",
          visibility: "private",
          typeName: "Array",
          arrayKind: "dynamic",
          arrayDimensions: [],
        },
      ],
    },
    {
      name: "public class constant remains a constant member",
      source: `<%
Class Bag
  Public Const Kind = "bag"
End Class
%>`,
      declarations: [
        {
          name: "Kind",
          kind: "constant",
          memberOf: "Bag",
          visibility: "public",
        },
      ],
    },
    {
      name: "colon separated Dim and ReDim statements keep array declarations",
      source: `<%
Sub Resize()
  Dim colonItems() : ReDim Preserve colonItems(2)
End Sub
%>`,
      declarations: [
        {
          name: "colonItems",
          kind: "variable",
          bindingScope: "local",
          typeName: "Array",
          arrayKind: "dynamic",
        },
      ],
      absentDeclarations: [{ name: "Preserve" }],
    },
    {
      name: "local Dim inside Sub stays local",
      source: `<%
Sub Work()
  Dim localValue
  localValue = 1
End Sub
%>`,
      declarations: [{ name: "localValue", kind: "variable", bindingScope: "local" }],
      references: [{ name: "localValue", role: "write", bindingScope: "local" }],
    },
    {
      name: "local Const inside Function stays local",
      source: `<%
Function Work()
  Const localConst = 1
  Work = localConst
End Function
%>`,
      declarations: [{ name: "localConst", kind: "constant", bindingScope: "local" }],
      references: [{ name: "localConst", role: "read", bindingScope: "local" }],
    },
    {
      name: "ByVal parameter is indexed as local",
      source: `<%
Sub Save(ByVal item)
  Response.Write item
End Sub
%>`,
      declarations: [{ name: "item", kind: "parameter", bindingScope: "local" }],
      references: [{ name: "item", role: "read", bindingScope: "local" }],
    },
    {
      name: "Optional ByRef parameter is indexed as local",
      source: `<%
Sub Save(Optional ByRef item)
  item = 1
End Sub
%>`,
      declarations: [{ name: "item", kind: "parameter", bindingScope: "local" }],
      references: [{ name: "item", role: "write", bindingScope: "local" }],
    },
    {
      name: "ParamArray parameter is indexed as local",
      source: `<%
Sub Save(ParamArray items)
  Response.Write items
End Sub
%>`,
      declarations: [{ name: "items", kind: "parameter", bindingScope: "local" }],
      references: [{ name: "items", role: "read", bindingScope: "local" }],
    },
    {
      name: "typed parameter records only the parameter name",
      source: `<%
Sub Save(ByVal amount As Integer)
  Response.Write amount
End Sub
%>`,
      declarations: [{ name: "amount", kind: "parameter", bindingScope: "local" }],
      absentDeclarations: [{ name: "Integer" }],
    },
    {
      name: "property Get is indexed as a class property",
      source: `<%
Class Customer
  Public Property Get Title()
    Title = "A"
  End Property
End Class
%>`,
      declarations: [
        {
          name: "Title",
          kind: "property",
          memberOf: "Customer",
          procedureKind: "property",
          visibility: "public",
        },
      ],
    },
    {
      name: "property Let parameter is indexed inside the property scope",
      source: `<%
Class Customer
  Public Property Let Title(ByVal value)
    m_title = value
  End Property
End Class
%>`,
      declarations: [
        { name: "Title", kind: "property", memberOf: "Customer" },
        { name: "value", kind: "parameter", bindingScope: "local" },
      ],
      references: [{ name: "value", role: "read", bindingScope: "local" }],
    },
    {
      name: "property Set parameter is indexed inside the property scope",
      source: `<%
Class Customer
  Public Property Set Repository(ByRef value)
    Set m_repository = value
  End Property
End Class
%>`,
      declarations: [
        { name: "Repository", kind: "property", memberOf: "Customer" },
        { name: "value", kind: "parameter", bindingScope: "local" },
      ],
      references: [{ name: "value", role: "read", bindingScope: "local" }],
    },
    {
      name: "private class Function is indexed as a private method",
      source: `<%
Class Customer
  Private Function Build()
  End Function
End Class
%>`,
      declarations: [
        {
          name: "Build",
          kind: "method",
          memberOf: "Customer",
          procedureKind: "function",
          visibility: "private",
        },
      ],
    },
    {
      name: "public class Sub is indexed as a public method",
      source: `<%
Class Customer
  Public Sub Save()
  End Sub
End Class
%>`,
      declarations: [
        {
          name: "Save",
          kind: "method",
          memberOf: "Customer",
          procedureKind: "sub",
          visibility: "public",
        },
      ],
    },
    {
      name: "plain class field becomes a member field",
      source: `<%
Class Customer
  Public Name
End Class
%>`,
      declarations: [
        {
          name: "Name",
          kind: "field",
          memberOf: "Customer",
          visibility: "public",
        },
      ],
    },
    {
      name: "Me member access resolves to the class field",
      source: `<%
Class Customer
  Public Name
  Public Sub Save()
    Me.Name = "A"
  End Sub
End Class
%>`,
      declarations: [{ name: "Name", kind: "field", memberOf: "Customer" }],
      references: [{ name: "Name", role: "member" }],
    },
    {
      name: "local declaration shadows a global declaration",
      source: `<%
Dim Value
Sub Save()
  Dim Value
  Value = 1
End Sub
%>`,
      declarations: [{ name: "Value", kind: "variable", bindingScope: "local" }],
      references: [{ name: "Value", role: "write", bindingScope: "local" }],
    },
    {
      name: "class field shadows a global declaration inside a method",
      source: `<%
Dim Name
Class Customer
  Public Name
  Public Sub Save()
    Name = "A"
  End Sub
End Class
%>`,
      declarations: [{ name: "Name", kind: "field", memberOf: "Customer" }],
      references: [{ name: "Name", role: "write" }],
    },
    {
      name: "missing read becomes a deferred include candidate",
      source: `<%
Sub Save()
  Response.Write MissingRead
End Sub
%>`,
      deferredExternalRefs: [
        {
          name: "MissingRead",
          role: "read",
          bindingScope: "unknown",
          expectedKinds: ["variable", "constant"],
        },
      ],
    },
    {
      name: "missing write becomes a deferred include candidate",
      source: `<%
Sub Save()
  MissingWrite = 1
End Sub
%>`,
      deferredExternalRefs: [
        {
          name: "MissingWrite",
          role: "write",
          bindingScope: "unknown",
          expectedKinds: ["variable"],
        },
      ],
    },
    {
      name: "New class usage is indexed as a constructor call site",
      source: `<%
Class Widget
End Class
Sub Make()
  Set item = New Widget
End Sub
%>`,
      declarations: [{ name: "Widget", kind: "class" }],
      callSites: [{ name: "Widget", callKind: "constructor" }],
    },
    {
      name: "Call and bare Sub calls are both indexed",
      source: `<%
Sub Log(message)
End Sub
Sub Run()
  Call Log("a")
  Log "b"
End Sub
%>`,
      declarations: [{ name: "Log", kind: "sub" }],
      callSites: [{ name: "Log", callKind: "procedure" }],
    },
    {
      name: "server-side VBScript script tag contributes declarations",
      source: `<script runat="server" language="VBScript">
Sub FromServerScript()
End Sub
</script>
<script runat="server" type="JScript">
function fromJScriptTag() {}
</script>
<script>
function fromClientScript() {}
</script>`,
      declarations: [{ name: "FromServerScript", kind: "sub" }],
      absentDeclarations: [{ name: "fromJScriptTag" }, { name: "fromClientScript" }],
    },
  ];

  it.each(symbolIndexSpecialCases)("covers VBScript symbol index edge case: $name", (testCase) => {
    const index = extractVbscriptSymbolIndex(
      "file:///site/special-case.asp",
      testCase.source,
      testCase.settings,
      testCase.options,
    );
    const matchesExpected = (actual: Record<string, unknown>, expected: Record<string, unknown>) =>
      Object.entries(expected).every(([key, value]) => {
        if (key === "name" || key === "kind" || key === "role") {
          return true;
        }
        return JSON.stringify(actual[key]) === JSON.stringify(value);
      });
    const findDeclaration = (expected: SymbolIndexExpected | SymbolIndexAbsent) =>
      index.declarations.find(
        (item) =>
          item.name === expected.name &&
          (expected.kind === undefined || item.kind === expected.kind) &&
          matchesExpected(item as unknown as Record<string, unknown>, expected),
      );
    const findReference = (expected: SymbolIndexExpected | SymbolIndexAbsent) =>
      index.references.find(
        (item) =>
          item.name === expected.name &&
          (expected.role === undefined || item.role === expected.role) &&
          matchesExpected(item as unknown as Record<string, unknown>, expected),
      );
    const findCallSite = (expected: SymbolIndexExpected) =>
      index.callSites.find(
        (item) =>
          item.name === expected.name &&
          matchesExpected(item as unknown as Record<string, unknown>, expected),
      );
    const findDeferredExternalRef = (expected: SymbolIndexExpected) =>
      index.deferredExternalRefs.find(
        (item) =>
          item.name === expected.name &&
          (expected.role === undefined || item.role === expected.role) &&
          matchesExpected(item as unknown as Record<string, unknown>, expected),
      );

    for (const expected of testCase.declarations ?? []) {
      expect(findDeclaration(expected), `declaration ${expected.name}`).toEqual(
        expect.objectContaining(expected),
      );
    }
    for (const expected of testCase.absentDeclarations ?? []) {
      expect(findDeclaration(expected), `absent declaration ${expected.name}`).toBeUndefined();
    }
    for (const expected of testCase.references ?? []) {
      expect(findReference(expected), `reference ${expected.name}`).toEqual(
        expect.objectContaining(expected),
      );
    }
    for (const expected of testCase.callSites ?? []) {
      expect(findCallSite(expected), `call site ${expected.name}`).toEqual(
        expect.objectContaining(expected),
      );
    }
    for (const expected of testCase.deferredExternalRefs ?? []) {
      expect(findDeferredExternalRef(expected), `deferred ref ${expected.name}`).toEqual(
        expect.objectContaining(expected),
      );
    }
  });

  it("ignores symbol index declarations inside strings, comments, and client content", () => {
    const source = `<script>
const ignored = "Function ClientFake()";
// Dim ClientComment
</script>
<style>
.fake { content: "Class StyleFake"; }
/* Const StyleComment = 1 */
</style>
<%
Dim RealValue
RealValue = "Class StringFake"
' Function CommentFake()
Rem Const RemFake = 1
%>`;
    const index = extractVbscriptSymbolIndex("file:///site/ignored.asp", source);
    expect(index.declarations.map((item) => item.name)).toEqual(["RealValue"]);
    expect(index.deferredExternalRefs.map((item) => item.name)).not.toEqual(
      expect.arrayContaining([
        "ClientFake",
        "ClientComment",
        "StyleFake",
        "StyleComment",
        "StringFake",
        "CommentFake",
        "RemFake",
      ]),
    );
  });

  it("keeps local symbol index shadowing ahead of include-deferred globals", () => {
    const source = `<%
Dim Value
Sub UseValue()
  Dim Value
  Value = IncludedValue
End Sub
%>`;
    const index = extractVbscriptSymbolIndex("file:///site/shadow.asp", source);
    const localValue = index.declarations.find(
      (item) => item.name === "Value" && item.bindingScope === "local",
    );
    expect(
      index.references.find((item) => item.name === "Value" && item.role === "write"),
    ).toMatchObject({
      resolvedId: localValue?.id,
      bindingScope: "local",
    });
    expect(index.deferredExternalRefs.find((item) => item.name === "IncludedValue")).toMatchObject({
      bindingScope: "unknown",
      expectedKinds: ["variable", "constant"],
    });
  });

  it("adds implicit variable declarations to the symbol index only when opted in", () => {
    const source = `<%
implicitGlobal = "global"
Function BuildValue()
  implicitLocal = implicitGlobal
  BuildValue = implicitLocal
End Function
Class Widget
  Public Sub Save()
    methodLocal = "method"
  End Sub
End Class
obj.Member = "member"
Response = "builtin"
CStr = "builtin"
%>`;
    const defaultIndex = extractVbscriptSymbolIndex("file:///site/implicit-default.asp", source);
    expect(defaultIndex.declarations.map((item) => item.name)).not.toEqual(
      expect.arrayContaining(["implicitGlobal", "implicitLocal", "methodLocal"]),
    );

    const index = extractVbscriptSymbolIndex(
      "file:///site/implicit.asp",
      source,
      {},
      {
        includeImplicitVariables: true,
      },
    );
    const implicitGlobal = index.declarations.find((item) => item.name === "implicitGlobal");

    expect(implicitGlobal).toMatchObject({
      kind: "variable",
      bindingScope: "global",
      implicit: true,
    });
    expect(index.declarations.filter((item) => item.name === "implicitGlobal")).toHaveLength(1);
    expect(index.declarations.find((item) => item.name === "implicitLocal")).toMatchObject({
      kind: "variable",
      bindingScope: "local",
      implicit: true,
    });
    expect(index.declarations.find((item) => item.name === "methodLocal")).toMatchObject({
      kind: "variable",
      bindingScope: "local",
      implicit: true,
    });
    expect(index.declarations.some((item) => item.name === "Member" && item.implicit)).toBe(false);
    expect(index.declarations.some((item) => item.name === "Response" && item.implicit)).toBe(
      false,
    );
    expect(index.declarations.some((item) => item.name === "CStr" && item.implicit)).toBe(false);
    expect(index.declarations.some((item) => item.name === "BuildValue" && item.implicit)).toBe(
      false,
    );
    expect(
      index.references
        .filter((item) => item.name === "implicitGlobal")
        .every((item) => item.resolvedId === implicitGlobal?.id),
    ).toBe(true);
    expect(index.deferredExternalRefs.map((item) => item.name)).not.toEqual(
      expect.arrayContaining(["implicitGlobal", "implicitLocal", "methodLocal"]),
    );

    const explicitIndex = extractVbscriptSymbolIndex(
      "file:///site/explicit.asp",
      `<%
Option Explicit
missingValue = 1
%>`,
      {},
      { includeImplicitVariables: true },
    );
    expect(explicitIndex.declarations.some((item) => item.name === "missingValue")).toBe(false);
  });

  it("adds implicit variable declarations from single-line If assignments to the symbol index", () => {
    const source = `<%
If enabled Then oneLineValue = 1
If enabled Then branchValue = 2 Else fallbackValue = "fallback"
If enabled Then Let letValue = 3
If enabled Then _
  continuedValue = 4
Sub Render()
  If enabled Then localValue = "local"
End Sub
Class Widget
End Class
If enabled Then Set objectValue = New Widget
%>`;
    const index = extractVbscriptSymbolIndex(
      "file:///site/single-line-if-implicit-index.asp",
      source,
      {},
      { includeImplicitVariables: true },
    );
    const declaration = (name: string) => index.declarations.find((item) => item.name === name);
    const writeReference = (name: string) =>
      index.references.find((item) => item.name === name && item.role === "write");

    expect(declaration("oneLineValue")).toMatchObject({
      kind: "variable",
      bindingScope: "global",
      implicit: true,
    });
    expect(declaration("branchValue")).toMatchObject({
      kind: "variable",
      bindingScope: "global",
      implicit: true,
    });
    expect(declaration("fallbackValue")).toMatchObject({
      kind: "variable",
      bindingScope: "global",
      implicit: true,
    });
    expect(declaration("letValue")).toMatchObject({
      kind: "variable",
      bindingScope: "global",
      implicit: true,
    });
    expect(declaration("continuedValue")).toMatchObject({
      kind: "variable",
      bindingScope: "global",
      implicit: true,
    });
    expect(declaration("localValue")).toMatchObject({
      kind: "variable",
      bindingScope: "local",
      implicit: true,
    });
    expect(declaration("objectValue")).toMatchObject({
      kind: "variable",
      bindingScope: "global",
      implicit: true,
    });
    for (const name of [
      "oneLineValue",
      "branchValue",
      "fallbackValue",
      "letValue",
      "continuedValue",
      "localValue",
      "objectValue",
    ]) {
      expect(writeReference(name)).toMatchObject({ resolvedId: declaration(name)?.id });
    }
    expect(index.deferredExternalRefs.map((item) => item.name)).not.toEqual(
      expect.arrayContaining([
        "oneLineValue",
        "branchValue",
        "fallbackValue",
        "letValue",
        "continuedValue",
        "localValue",
        "objectValue",
      ]),
    );
  });

  it("marks loop variables and procedure implicit assignments as local in the symbol index", () => {
    const source = `<%
Sub Render()
  For index = 1 To 3
    loopValue = index
  Next
  For Each item In items
    eachValue = item
  Next
End Sub
%>`;
    const index = extractVbscriptSymbolIndex(
      "file:///site/local-loop-implicit-index.asp",
      source,
      {},
      { includeImplicitVariables: true },
    );
    const declaration = (name: string) => index.declarations.find((item) => item.name === name);
    const reference = (name: string) =>
      index.references.find((item) => item.name === name && item.role !== "call");

    expect(declaration("index")).toMatchObject({ kind: "variable", bindingScope: "local" });
    expect(declaration("item")).toMatchObject({ kind: "variable", bindingScope: "local" });
    expect(declaration("loopValue")).toMatchObject({
      kind: "variable",
      bindingScope: "local",
      implicit: true,
    });
    expect(declaration("eachValue")).toMatchObject({
      kind: "variable",
      bindingScope: "local",
      implicit: true,
    });
    expect(reference("index")).toMatchObject({
      resolvedId: declaration("index")?.id,
      bindingScope: "local",
    });
    expect(reference("item")).toMatchObject({
      resolvedId: declaration("item")?.id,
      bindingScope: "local",
    });
    expect(index.deferredExternalRefs.map((item) => item.name)).not.toEqual(
      expect.arrayContaining(["index", "item", "loopValue", "eachValue"]),
    );
  });

  it("keeps function return assignments out of VBScript symbol index references", () => {
    const source = `<%
Class Customer
End Class
Function MakeCustomer()
  Set MakeCustomer = New Customer
End Function
Function BuildValue(ByVal n)
  Let BuildValue = n
  BuildValue = BuildValue + n
End Function
result = MakeCustomer()
result = BuildValue(2)
%>`;
    const index = extractVbscriptSymbolIndex("file:///site/return-assignment.asp", source);
    const references = (name: string) => index.references.filter((item) => item.name === name);

    expect(references("MakeCustomer").some((item) => item.role === "write")).toBe(false);
    expect(references("BuildValue").some((item) => item.role === "write")).toBe(false);
    expect(references("BuildValue").some((item) => item.role === "read")).toBe(true);
    expect(index.callSites.some((item) => item.name === "MakeCustomer" && item.resolvedId)).toBe(
      true,
    );
    expect(index.callSites.some((item) => item.name === "BuildValue" && item.resolvedId)).toBe(
      true,
    );
  });

  it("keeps VBScript symbol index procedure scope after block End statements", () => {
    const source = `<%
Function BuildValue(ByVal input)
  Dim values
  If input Then
    values = input
  End If
  values = values & input
  values("total") = values("total") & input
  With values
  End With
  BuildValue = values
End Function
%>`;
    const index = extractVbscriptSymbolIndex("file:///site/block-scope.asp", source);
    const input = index.declarations.find((item) => item.name === "input");
    const values = index.declarations.find((item) => item.name === "values");
    expect(input).toMatchObject({ kind: "parameter", bindingScope: "local" });
    expect(values).toMatchObject({ kind: "variable", bindingScope: "local" });
    expect(
      index.references.filter((item) => item.name === "input" && item.role === "read"),
    ).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ resolvedId: input?.id, bindingScope: "local" }),
      ]),
    );
    expect(
      index.references.filter((item) => item.name === "values" && item.role === "read"),
    ).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ resolvedId: values?.id, bindingScope: "local" }),
      ]),
    );
    expect(index.deferredExternalRefs.map((item) => item.name)).not.toEqual(
      expect.arrayContaining(["input", "values"]),
    );
  });

  it("collects only public VBScript symbols for include summaries", () => {
    const source = `<%
Dim PublicValue
Set PublicValue = Server.CreateObject("ADODB.Recordset")
' @type ExplicitRecordset As ADODB.Recordset
Dim ExplicitRecordset
Class PublicClass
  Public publicField
  Private privateField
  Public Function PublicMember()
  End Function
  Private Function PrivateMember()
  End Function
End Class
Function PublicFunction(arg)
  Dim localValue
  localValue = 1
End Function
' @returns PublicFactory ADODB.Recordset
Function PublicFactory()
  Set PublicFactory = Server.CreateObject("ADODB.Recordset")
End Function
Private Sub PrivateProcedure()
End Sub
%>`;
    const parsed = parseAspDocument("file:///site/common.inc", source);
    const symbols = collectVbscriptPublicSymbols(parsed);
    const names = symbols.map((symbol) => symbol.name);

    expect(names).toEqual(
      expect.arrayContaining([
        "PublicValue",
        "ExplicitRecordset",
        "PublicClass",
        "publicField",
        "PublicMember",
        "PublicFunction",
        "PublicFactory",
      ]),
    );
    expect(names).not.toEqual(
      expect.arrayContaining([
        "arg",
        "localValue",
        "privateField",
        "PrivateMember",
        "PrivateProcedure",
      ]),
    );
    expect(symbols.find((symbol) => symbol.name === "PublicValue")?.typeName).toBe("Variant");
    expect(symbols.find((symbol) => symbol.name === "ExplicitRecordset")).toMatchObject({
      typeName: "ADODB.Recordset",
      explicitType: true,
    });
    expect(symbols.find((symbol) => symbol.name === "PublicFactory")).toMatchObject({
      typeName: "ADODB.Recordset",
      explicitType: true,
    });
    expect(symbols.find((symbol) => symbol.name === "PublicFunction")?.typeName).toBe("Variant");
  });

  it("summarizes VBScript exports and unresolved external references", () => {
    const source = `<%
Option Explicit
Dim localValue
Function LocalTitle()
End Function
Response.Write SharedTitle(localValue)
Response.Write SharedCatalog.Name
%>`;
    const parsed = parseAspDocument("file:///site/default.asp", source);
    const summary = summarizeAspFileAnalysis(parsed);

    expect(summary.languageRegions.some((region) => region.language === "vbscript")).toBe(true);
    expect(summary.vbscript?.exports.map((item) => item.name)).toContain("LocalTitle");
    expect(summary.vbscript?.externalRefs).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ name: "SharedTitle", kindHint: "function" }),
        expect.objectContaining({ name: "SharedCatalog", memberName: "Name" }),
      ]),
    );
    expect(summary.vbscript?.externalRefs.map((item) => item.name)).not.toContain("Response");
    expect(summary.vbscript?.externalRefs.map((item) => item.name)).not.toContain("localValue");
    expect(summary.vbscript?.externalRefUsages).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          key: "sharedtitle",
          count: 1,
          ranges: [expect.objectContaining({ start: expect.objectContaining({ line: 5 }) })],
        }),
        expect.objectContaining({
          key: "sharedcatalog.name",
          count: 1,
          ranges: [expect.objectContaining({ start: expect.objectContaining({ line: 6 }) })],
        }),
      ]),
    );
  });

  it("summarizes VBScript public symbols without implicit or inferred exports", () => {
    const source = `<%
Dim ExplicitValue
' @type TypedValue As ADODB.Recordset
Dim TypedValue
InferredValue = Server.CreateObject("ADODB.Recordset")
Function PublicFactory()
  Set PublicFactory = Server.CreateObject("ADODB.Recordset")
End Function
%>`;
    const parsed = parseAspDocument("file:///site/common.inc", source);
    const summary = summarizeAspFileAnalysis(parsed);
    const symbols = summary.vbscript?.publicSymbols ?? [];
    const names = symbols.map((symbol) => symbol.name);

    expect(names).toEqual(expect.arrayContaining(["ExplicitValue", "TypedValue", "PublicFactory"]));
    expect(names).not.toContain("InferredValue");
    expect(symbols.find((symbol) => symbol.name === "ExplicitValue")?.typeName).toBe("Variant");
    expect(symbols.find((symbol) => symbol.name === "TypedValue")).toMatchObject({
      typeName: "ADODB.Recordset",
      explicitType: true,
    });
    expect(symbols.find((symbol) => symbol.name === "PublicFactory")?.typeName).toBe("Variant");
  });

  it("keeps VBScript lookup and completions case-insensitive", () => {
    const source = `<%
Dim CustomerName
response.
Response.Write customername
%>`;
    const parsed = parseAspDocument("file:///site/default.asp", source);
    const symbols = collectVbscriptSymbols(parsed);
    const completions = getVbscriptCompletions(parsed, { line: 2, character: 9 }, { symbols });
    expect(completions.some((item) => item.label === "Write")).toBe(true);
    const definition = getVbscriptDefinition(parsed, { line: 3, character: 18 }, { symbols });
    expect(definition?.name).toBe("CustomerName");
    const references = getVbscriptReferences(parsed, { line: 3, character: 18 }, { symbols });
    expect(references).toHaveLength(2);
  });

  it("collects VBScript declaration scopes from CST containment", () => {
    const source = `<%
Dim topLevel
Class Customer
  Public Name
  Public Sub Save()
    Dim localValue
    For Each item In items
    Next
    For index = 1 To 3
    Next
  End Sub
End Class
%>`;
    const parsed = parseAspDocument("file:///site/default.asp", source);
    const symbols = collectVbscriptSymbols(parsed);
    const symbol = (name: string) => {
      const found = symbols.find((item) => item.name === name);
      expect(found, name).toBeDefined();
      return found;
    };

    const topLevel = symbol("topLevel");
    expect(topLevel?.kind).toBe("variable");
    expect(topLevel?.memberOf).toBeUndefined();
    expect(topLevel?.scopeName).toBeUndefined();

    const field = symbol("Name");
    expect(field?.kind).toBe("field");
    expect(field?.memberOf).toBe("Customer");
    expect(field?.scopeName).toBeUndefined();

    const local = symbol("localValue");
    expect(local?.kind).toBe("variable");
    expect(local?.memberOf).toBeUndefined();
    expect(local?.scopeName).toBe("Save");

    const loopItem = symbol("item");
    expect(loopItem?.kind).toBe("variable");
    expect(loopItem?.memberOf).toBeUndefined();
    expect(loopItem?.scopeName).toBe("Save");

    const loopIndex = symbol("index");
    expect(loopIndex?.kind).toBe("variable");
    expect(loopIndex?.memberOf).toBeUndefined();
    expect(loopIndex?.scopeName).toBe("Save");
  });

  it("can count only VBScript usage references for functions", () => {
    const source = `<%
Function MakeCustomer()
  Set MakeCustomer = New Customer
End Function

Function Factorial(ByVal n)
  If n <= 1 Then
    Factorial = 1
  Else
    Factorial = n * Factorial(n - 1)
  End If
End Function

Set c = MakeCustomer()
%>`;
    const parsed = parseAspDocument("file:///site/default.asp", source);
    const symbols = collectVbscriptSymbols(parsed);
    const allMakeCustomerReferences = getVbscriptReferences(
      parsed,
      { line: 13, character: "Set c = Make".length },
      { symbols },
    );
    expect(allMakeCustomerReferences).toHaveLength(3);
    const makeCustomerUsages = getVbscriptReferences(
      parsed,
      { line: 13, character: "Set c = Make".length },
      { symbols },
      { includeDeclaration: false, includeFunctionReturnAssignments: false },
    );
    expect(makeCustomerUsages).toHaveLength(1);
    expect(makeCustomerUsages[0].range.start.line).toBe(13);

    const factorialUsages = getVbscriptReferences(
      parsed,
      { line: 9, character: "    Factorial = n * Fact".length },
      { symbols },
      { includeDeclaration: false, includeFunctionReturnAssignments: false },
    );
    expect(factorialUsages).toHaveLength(1);
    expect(factorialUsages[0].range.start.line).toBe(9);
    expect(factorialUsages[0].range.start.character).toBe("    Factorial = n * ".length);
  });

  it("matches single-symbol VBScript references when resolving symbols in a batch", () => {
    const source = `<%
''' <summary>Uses <see cref="MakeCustomer" /> and <see cref="Customer.Name" />.</summary>
Class Customer
  Public Name
End Class

Function MakeCustomer()
  Set MakeCustomer = New Customer
End Function

Set c = MakeCustomer()
Response.Write c.Name
%>`;
    const parsed = parseAspDocument("file:///site/batch-references.asp", source);
    const symbols = collectVbscriptSymbols(parsed);
    const targets = symbols.filter(
      (symbol) =>
        (symbol.name === "MakeCustomer" && symbol.kind === "function") ||
        (symbol.name === "Name" && symbol.memberOf === "Customer"),
    );
    expect(targets).toHaveLength(2);

    const options = {
      includeDeclaration: false,
      includeFunctionReturnAssignments: false,
    };
    const batch = getVbscriptReferencesForSymbols(
      targets,
      { documents: [parsed], symbols },
      options,
    );

    for (const target of targets) {
      const single = getVbscriptReferencesForSymbol(
        target,
        { documents: [parsed], symbols },
        options,
      );
      expect(batch.get(vbscriptReferenceSymbolKey(target))).toEqual(single);
    }
  });

  it("keeps user-defined completion candidates for mixed-case prefixes", () => {
    const source = `<%
Dim CustomerName
cust
CUST
CuSt
%>`;
    const parsed = parseAspDocument("file:///site/default.asp", source);
    const symbols = collectVbscriptSymbols(parsed);
    for (const position of [
      { line: 2, character: 4 },
      { line: 3, character: 4 },
      { line: 4, character: 4 },
    ]) {
      expect(
        getVbscriptCompletions(parsed, position, { symbols }).some(
          (item) => item.label === "CustomerName",
        ),
      ).toBe(true);
    }
  });

  it("warns about undeclared variables under Option Explicit", () => {
    const parsed = parseAspDocument(
      "file:///site/default.asp",
      `<% Option Explicit
Response.Write missingName
%>`,
    );
    const result = analyzeVbscript(parsed);
    expect(
      result.diagnostics.some((diagnostic) => diagnostic.message.includes("missingName")),
    ).toBe(true);
  });

  it("reports invalid VBScript variable declaration syntax as errors", () => {
    const parsed = parseAspDocument(
      "file:///site/default.asp",
      `<%
Dim initialized = 1
Dim first, second = 2
Public publicValue = 3
Private privateValue = 4
ReDim resized = 5
Dim typed As Integer
Public publicTyped As String
Private privateTyped As Object
%>`,
    );
    const syntaxDiagnostics = analyzeVbscript(parsed, {
      unusedDiagnostics: false,
    }).diagnostics.filter((diagnostic) => diagnostic.source === "asp-lsp-vbscript-syntax");
    expect(
      syntaxDiagnostics.filter((diagnostic) => diagnostic.code === "initializedDeclaration"),
    ).toHaveLength(5);
    expect(
      syntaxDiagnostics.filter((diagnostic) => diagnostic.code === "typedDeclaration"),
    ).toHaveLength(3);
    expect(syntaxDiagnostics.every((diagnostic) => diagnostic.severity === 1)).toBe(true);
  });

  it("keeps valid VBScript variable declarations out of syntax diagnostics", () => {
    const parsed = parseAspDocument(
      "file:///site/default.asp",
      `<%
Dim value
value = 1
Dim fixedItems(10)
Dim dynamicItems()
ReDim fixedItems(20)
ReDim Preserve fixedItems(30)
Const knownValue = 1
%>`,
    );
    expect(
      analyzeVbscript(parsed, { unusedDiagnostics: false }).diagnostics.some(
        (diagnostic) => diagnostic.source === "asp-lsp-vbscript-syntax",
      ),
    ).toBe(false);
  });

  it("reports invalid VBScript procedure call syntax as errors", () => {
    const parsed = parseAspDocument(
      "file:///site/default.asp",
      `<%
Function Func1(hoge)
  Func1 = hoge
End Function
Sub Func2(hoge, fuga)
End Sub
Call Func1 hoge
Z = Func1 hoge
Func2(hoge, fuga)
Call Func2 hoge, fuga
Z = Func2 hoge, fuga
%>`,
    );
    const symbols = collectVbscriptSymbols(parsed);
    const syntaxDiagnostics = analyzeVbscript(parsed, {
      symbols,
      unusedDiagnostics: false,
    }).diagnostics.filter((diagnostic) => diagnostic.source === "asp-lsp-vbscript-syntax");
    expect(syntaxDiagnostics.map((diagnostic) => diagnostic.code)).toEqual([
      "callStatementRequiresParentheses",
      "expressionCallRequiresParentheses",
      "statementCallDisallowsParenthesizedArguments",
      "callStatementRequiresParentheses",
      "expressionCallRequiresParentheses",
    ]);
    expect(syntaxDiagnostics.map((diagnostic) => diagnostic.data?.newText)).toEqual([
      "Call Func1(hoge)",
      "Z = Func1(hoge)",
      "Func2 hoge, fuga",
      "Call Func2(hoge, fuga)",
      "Z = Func2(hoge, fuga)",
    ]);
    expect(syntaxDiagnostics.every((diagnostic) => diagnostic.severity === 1)).toBe(true);
  });

  it("allows parenthesized function calls in ASP expression output", () => {
    const source = `<%
Function RenderCustomerRows(ByVal customerList, ByVal activeCustomerId)
  RenderCustomerRows = ""
End Function
%>
<%= RenderCustomerRows(filteredCustomers, selectedCustomerId) %>
<% RenderCustomerRows(filteredCustomers, selectedCustomerId) %>`;
    const parsed = parseAspDocument("file:///site/default.asp", source);
    const symbols = collectVbscriptSymbols(parsed);
    const syntaxDiagnostics = analyzeVbscript(parsed, {
      symbols,
      unusedDiagnostics: false,
    }).diagnostics.filter((diagnostic) => diagnostic.source === "asp-lsp-vbscript-syntax");

    expect(syntaxDiagnostics.map((diagnostic) => diagnostic.code)).toEqual([
      "statementCallDisallowsParenthesizedArguments",
    ]);
    expect(syntaxDiagnostics[0]?.range.start).toEqual(
      positionAt(source, source.lastIndexOf("RenderCustomerRows")),
    );
  });

  it("keeps valid VBScript procedure call syntax out of syntax diagnostics", () => {
    const parsed = parseAspDocument(
      "file:///site/default.asp",
      `<%
Function Func1(hoge)
  Func1 = hoge
End Function
Sub Func2(hoge, fuga)
End Sub
Call Func1(hoge)
Func1 hoge
Z = Func1(hoge)
Func2 hoge, fuga
Call Func2(hoge, fuga)
Response.Write("x")
%>`,
    );
    const symbols = collectVbscriptSymbols(parsed);
    const syntaxDiagnostics = analyzeVbscript(parsed, {
      symbols,
      unusedDiagnostics: false,
    }).diagnostics.filter((diagnostic) => diagnostic.source === "asp-lsp-vbscript-syntax");
    expect(syntaxDiagnostics).toHaveLength(0);
  });

  it("reports VBScript If syntax errors according to the configured strictness", () => {
    const parsed = parseAspDocument(
      "file:///site/default.asp",
      `<%
If ready
ElseIf ready
If Then
If (ready Then
If openBlock Then
  Response.Write openBlock
If outer Then
  If inner Then
    Response.Write inner
%>`,
    );
    const basicDiagnostics = analyzeVbscript(parsed, {
      ifSyntaxDiagnostics: "basic",
      unusedDiagnostics: false,
    }).diagnostics.filter((diagnostic) => diagnostic.source === "asp-lsp-vbscript-syntax");
    expect(basicDiagnostics.map((diagnostic) => diagnostic.code)).toEqual([
      "missingThen",
      "missingThen",
      "missingIfCondition",
      "invalidIfCondition",
      "missingEndIf",
      "missingEndIf",
      "missingEndIf",
    ]);
    expect(basicDiagnostics.every((diagnostic) => diagnostic.severity === 1)).toBe(true);

    const strictParsed = parseAspDocument(
      "file:///site/strict-if.asp",
      `<%
If value = Then
%>`,
    );
    expect(
      analyzeVbscript(strictParsed, {
        ifSyntaxDiagnostics: "basic",
        unusedDiagnostics: false,
      }).diagnostics.some((diagnostic) => diagnostic.code === "invalidIfCondition"),
    ).toBe(false);
    expect(
      analyzeVbscript(strictParsed, {
        ifSyntaxDiagnostics: "strict",
        unusedDiagnostics: false,
      })
        .diagnostics.filter((diagnostic) => diagnostic.source === "asp-lsp-vbscript-syntax")
        .map((diagnostic) => diagnostic.code),
    ).toEqual(["invalidIfCondition"]);

    expect(
      analyzeVbscript(parsed, {
        ifSyntaxDiagnostics: "off",
        unusedDiagnostics: false,
      }).diagnostics.some((diagnostic) =>
        ["missingThen", "missingIfCondition", "invalidIfCondition", "missingEndIf"].includes(
          String(diagnostic.code),
        ),
      ),
    ).toBe(false);
  });

  it("reports missing VBScript block terminators", () => {
    const parsed = parseAspDocument(
      "file:///site/missing-block-ends.asp",
      `<%
Sub MissingSub()
Function MissingFunction()
Class MissingClass
Property Get MissingProperty()
Select Case value
With obj
Do
While ready
For index = 0 To 1
For Each item In items
%>`,
    );
    const syntaxDiagnostics = analyzeVbscript(parsed, {
      unusedDiagnostics: false,
    }).diagnostics.filter((diagnostic) => diagnostic.source === "asp-lsp-vbscript-syntax");
    expect(syntaxDiagnostics.map((diagnostic) => diagnostic.code)).toEqual([
      "missingEndSub",
      "missingEndFunction",
      "missingEndClass",
      "missingEndProperty",
      "missingEndSelect",
      "missingEndWith",
      "missingLoop",
      "missingWend",
      "missingNext",
      "missingNext",
    ]);
    expect(syntaxDiagnostics.every((diagnostic) => diagnostic.severity === 1)).toBe(true);

    const valid = parseAspDocument(
      "file:///site/closed-blocks.asp",
      `<%
Sub ClosedSub()
End Sub
Function ClosedFunction()
End Function
Class ClosedClass
  Property Get Name()
  End Property
End Class
Select Case value
End Select
With obj
End With
Do
Loop
While ready
Wend
For index = 0 To 1
Next
For Each item In items
Next
%>`,
    );
    expect(
      analyzeVbscript(valid, {
        unusedDiagnostics: false,
      }).diagnostics.some(
        (diagnostic) =>
          String(diagnostic.code ?? "")
            .toLowerCase()
            .includes("missingend") ||
          diagnostic.code === "missingLoop" ||
          diagnostic.code === "missingWend" ||
          diagnostic.code === "missingNext",
      ),
    ).toBe(false);
  });

  it("does not treat standalone End across ASP islands as a VBScript block terminator", () => {
    const parsed = parseAspDocument(
      "file:///site/island-standalone-end.asp",
      `<%
Sub Render()
%>
<p>body</p>
<% end %>
<%
Function BuildTitle()
%>
<%= "title" %>
<% End %>`,
    );
    const syntaxCodes = analyzeVbscript(parsed, {
      unusedDiagnostics: false,
    })
      .diagnostics.filter((diagnostic) => diagnostic.source === "asp-lsp-vbscript-syntax")
      .map((diagnostic) => diagnostic.code);
    expect(syntaxCodes).toEqual(expect.arrayContaining(["missingEndSub", "missingEndFunction"]));
  });

  it("keeps explicit ASP-island-spanning VBScript block terminators valid", () => {
    const parsed = parseAspDocument(
      "file:///site/island-block-ends.asp",
      `<%
Sub Render()
%>
<p>body</p>
<% End Sub %>
<%
Function BuildTitle()
%>
<%= "title" %>
<% End Function %>
<%
If ready Then
%>
<span>ready</span>
<% Else %>
<span>fallback</span>
<% End If %>`,
    );
    const syntaxCodes = analyzeVbscript(parsed, {
      ifSyntaxDiagnostics: "strict",
      unusedDiagnostics: false,
    })
      .diagnostics.filter((diagnostic) => diagnostic.source === "asp-lsp-vbscript-syntax")
      .map((diagnostic) => diagnostic.code);
    expect(syntaxCodes).not.toEqual(
      expect.arrayContaining(["missingEndSub", "missingEndFunction", "missingEndIf"]),
    );
  });

  it("keeps valid VBScript If syntax out of syntax diagnostics", () => {
    const parsed = parseAspDocument(
      "file:///site/default.asp",
      `<%
If ready Then Response.Write "ok"
If ready Then: Response.Write "ok"
If ready Then _
  Response.Write "continued"
If first _
  And second _
  And third Then
  Response.Write "continued condition"
End If
If ready Then
  Response.Write "ready"
ElseIf other Then
  Response.Write "other"
End If
%>`,
    );
    const syntaxDiagnostics = analyzeVbscript(parsed, {
      ifSyntaxDiagnostics: "strict",
      unusedDiagnostics: false,
    }).diagnostics.filter((diagnostic) => diagnostic.source === "asp-lsp-vbscript-syntax");
    expect(syntaxDiagnostics).toHaveLength(0);
  });

  it("reports invalid VBScript On Error statements", () => {
    const parsed = parseAspDocument(
      "file:///site/on-error-syntax.asp",
      `<%
On Error Resume Next
On Error GoTo 0
On Error Resume
On Error GoTo
On Error GoTo label
On Error GoTo -1
On Error Next
%>`,
    );
    const syntaxDiagnostics = analyzeVbscript(parsed, {
      unusedDiagnostics: false,
    }).diagnostics.filter((diagnostic) => diagnostic.source === "asp-lsp-vbscript-syntax");
    expect(syntaxDiagnostics.map((diagnostic) => diagnostic.code)).toEqual([
      "invalidOnErrorStatement",
      "invalidOnErrorStatement",
      "invalidOnErrorStatement",
      "invalidOnErrorStatement",
      "invalidOnErrorStatement",
    ]);
    expect(syntaxDiagnostics.every((diagnostic) => diagnostic.severity === 1)).toBe(true);
  });

  it("allows function return value self references in assignments", () => {
    const parsed = parseAspDocument(
      "file:///site/default.asp",
      `<%
Option Explicit
Function A(v)
  A = ""
  A = A & v
End
%>`,
    );
    const symbols = collectVbscriptSymbols(parsed);
    const diagnostics = analyzeVbscript(parsed, {
      symbols,
      unusedDiagnostics: false,
    }).diagnostics;
    expect(diagnostics).toHaveLength(0);
  });

  it("localizes VBScript declaration syntax diagnostics", () => {
    const parsed = parseAspDocument(
      "file:///site/default.asp",
      `<%
Dim typed As Integer
%>`,
    );
    const result = analyzeVbscript(parsed, { locale: "ja", unusedDiagnostics: false });
    expect(result.diagnostics.some((diagnostic) => diagnostic.message.includes("As 型指定"))).toBe(
      true,
    );
  });

  it("localizes VBScript procedure call syntax diagnostics", () => {
    const parsed = parseAspDocument(
      "file:///site/default.asp",
      `<%
Function Func1(hoge)
  Func1 = hoge
End Function
Call Func1 hoge
%>`,
    );
    const symbols = collectVbscriptSymbols(parsed);
    const result = analyzeVbscript(parsed, { symbols, locale: "ja", unusedDiagnostics: false });
    expect(
      result.diagnostics.some((diagnostic) => diagnostic.message.includes("呼び出し構文")),
    ).toBe(true);
  });

  it("does not report VBScript line continuation as undeclared", () => {
    const parsed = parseAspDocument(
      "file:///site/default.asp",
      `<% Option Explicit
Dim message
message = "hello" & _
  " world"
%>`,
    );
    const result = analyzeVbscript(parsed);
    expect(result.diagnostics.some((diagnostic) => diagnostic.message.includes("'_'"))).toBe(false);
  });

  it("does not report the Is operator as undeclared under Option Explicit", () => {
    const parsed = parseAspDocument(
      "file:///site/default.asp",
      `<% Option Explicit
Dim value
Set value = Nothing
If value Is Nothing Then
  Response.Write "empty"
End If
%>`,
    );
    const result = analyzeVbscript(parsed);
    expect(result.diagnostics.some((diagnostic) => diagnostic.message.includes("'Is'"))).toBe(
      false,
    );
  });

  it("localizes VBScript diagnostics, XML docs and completion details", () => {
    const parsed = parseAspDocument(
      "file:///site/default.asp",
      `<% Option Explicit
''' <summary>名前を作ります。</summary>
''' <param name="first">名。</param>
''' <returns>表示名。</returns>
Function BuildName(first)
  BuildName = missingName
End Function
%>`,
      { resolvedLocale: "ja" },
    );
    const context = { locale: "ja" as const };
    const result = analyzeVbscript(parsed, context);
    expect(
      result.diagnostics.some((diagnostic) => diagnostic.message.includes("宣言されていません")),
    ).toBe(true);
    expect(
      result.diagnostics.some((diagnostic) => diagnostic.message.includes("使われていません")),
    ).toBe(true);
    const hover = getVbscriptHover(parsed, { line: 4, character: 10 }, context);
    expect(hover).toContain("戻り値");
    expect(hover).toContain("説明用です");
    const completions = getVbscriptCompletions(parsed, { line: 5, character: 2 }, context);
    expect(completions.some((item) => String(item.detail).includes("オブジェクト"))).toBe(true);
  });

  it("does not treat strings or comments as undeclared variables", () => {
    const parsed = parseAspDocument(
      "file:///site/default.asp",
      `<% Option Explicit
' comment words should be ignored
Response.Write "hello world"
%>`,
    );
    const result = analyzeVbscript(parsed);
    expect(result.diagnostics).toHaveLength(0);
  });

  it("reports scoped unused VBScript declarations as hints and leaves globals active", () => {
    const parsed = parseAspDocument(
      "file:///site/default.asp",
      `<%
Dim globalValue
Const GlobalConst = 1
Sub GlobalSave()
End Sub
Class GlobalLonely
End Class
Sub Save(usedArg, unusedArg)
  Dim unusedValue
  Const unusedConst = 1
  Response.Write usedArg
End Sub
%>`,
    );
    const diagnostics = analyzeVbscript(parsed).diagnostics;
    expect(diagnostics.some((diagnostic) => diagnostic.message.includes("unusedValue"))).toBe(true);
    expect(diagnostics.some((diagnostic) => diagnostic.message.includes("unusedConst"))).toBe(true);
    expect(diagnostics.some((diagnostic) => diagnostic.message.includes("unusedArg"))).toBe(true);
    expect(diagnostics.some((diagnostic) => diagnostic.message.includes("globalValue"))).toBe(
      false,
    );
    expect(diagnostics.some((diagnostic) => diagnostic.message.includes("GlobalConst"))).toBe(
      false,
    );
    expect(diagnostics.some((diagnostic) => diagnostic.message.includes("GlobalSave"))).toBe(false);
    expect(diagnostics.some((diagnostic) => diagnostic.message.includes("GlobalLonely"))).toBe(
      false,
    );
    expect(diagnostics.every((diagnostic) => diagnostic.severity === 4)).toBe(true);
    expect(
      diagnostics
        .filter((diagnostic) => diagnostic.source === "asp-lsp-vbscript-unused")
        .every((diagnostic) => diagnostic.tags?.includes(DiagnosticTag.Unnecessary)),
    ).toBe(true);
  });

  it("reports VBScript identifier casing hints", () => {
    const parsed = parseAspDocument(
      "file:///site/default.asp",
      `<%
Dim do_work, CUSTOMER_NAME
Class customer_record
End Class
%>`,
    );
    const diagnostics = analyzeVbscript(parsed).diagnostics.filter(
      (diagnostic) => diagnostic.source === "asp-lsp-vbscript-naming",
    );
    expect(diagnostics.map((diagnostic) => diagnostic.data)).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ name: "do_work", expectedName: "doWork", style: "camelCase" }),
        expect.objectContaining({
          name: "CUSTOMER_NAME",
          expectedName: "customerName",
          style: "camelCase",
        }),
        expect.objectContaining({ name: "customer_record", expectedName: "CustomerRecord" }),
      ]),
    );
    const workDiagnostic = diagnostics.find((diagnostic) => diagnostic.data?.name === "do_work");
    expect(workDiagnostic?.range).toEqual({
      start: { line: 1, character: 4 },
      end: { line: 1, character: 11 },
    });
    expect(diagnostics.every((diagnostic) => diagnostic.severity === 4)).toBe(true);
  });

  it("supports configurable VBScript identifier casing styles", () => {
    const expectedByStyle = [
      ["UPPERCASE", "user_name", "USERNAME"],
      ["camelCase", "user_name", "userName"],
      ["lowercase", "user_name", "username"],
      ["snake_case", "userName", "user_name"],
      ["UPPER_SNAKE", "user_name", "USER_NAME"],
    ] as const;
    for (const [identifierCase, sourceName, expectedName] of expectedByStyle) {
      const parsed = parseAspDocument(
        "file:///site/default.asp",
        `<%
Dim ${sourceName}
%>`,
      );
      const diagnostics = analyzeVbscript(parsed, { identifierCase }).diagnostics.filter(
        (diagnostic) => diagnostic.source === "asp-lsp-vbscript-naming",
      );
      expect(
        diagnostics.some((diagnostic) => JSON.stringify(diagnostic.data).includes(expectedName)),
      ).toBe(true);
    }
    expect(
      analyzeVbscript(parseAspDocument("file:///site/default.asp", `<%\nDim user_name\n%>`), {
        identifierCase: "ignore",
      }).diagnostics.some((diagnostic) => diagnostic.source === "asp-lsp-vbscript-naming"),
    ).toBe(false);
    expect(
      analyzeVbscript(
        parseAspDocument(
          "file:///site/default.asp",
          `<%
Dim userName
%>`,
        ),
      ).diagnostics.some((diagnostic) => diagnostic.source === "asp-lsp-vbscript-naming"),
    ).toBe(false);
  });

  it("reports VBScript identifier casing hints for declaration kinds", () => {
    const parsed = parseAspDocument(
      "file:///site/default.asp",
      `<%
Class customer_record
  Public customer_name
  Public Property Get display_name()
    display_name = customer_name
  End Property
End Class
Sub save_order(item_name)
End Sub
Function build_total()
End Function
%>`,
    );
    const diagnostics = analyzeVbscript(parsed, {
      identifierCaseByKind: {
        variable: "PascalCase",
        parameter: "PascalCase",
        field: "PascalCase",
        property: "PascalCase",
        function: "PascalCase",
        sub: "PascalCase",
        class: "PascalCase",
      },
    }).diagnostics.filter((diagnostic) => diagnostic.source === "asp-lsp-vbscript-naming");
    expect(diagnostics.map((diagnostic) => diagnostic.data)).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ name: "customer_record", expectedName: "CustomerRecord" }),
        expect.objectContaining({ name: "customer_name", expectedName: "CustomerName" }),
        expect.objectContaining({ name: "display_name", expectedName: "DisplayName" }),
        expect.objectContaining({ name: "save_order", expectedName: "SaveOrder" }),
        expect.objectContaining({ name: "item_name", expectedName: "ItemName" }),
        expect.objectContaining({ name: "build_total", expectedName: "BuildTotal" }),
      ]),
    );
  });

  it("deduplicates exact VBScript diagnostics", () => {
    const parsed = parseAspDocument(
      "file:///site/default.asp",
      `<%
Sub Save()
  Dim unusedValue
End Sub
%>`,
    );
    const symbols = collectVbscriptSymbols(parsed);
    const diagnostics = analyzeVbscript(parsed, { symbols: [...symbols, ...symbols] }).diagnostics;
    expect(
      diagnostics.filter((diagnostic) => diagnostic.message.includes("unusedValue")),
    ).toHaveLength(1);
  });

  it("keeps include references and Global.asa event handlers out of unused diagnostics", () => {
    const include = parseAspDocument(
      "file:///site/inc/shared.inc",
      `<%
Function SharedName()
End Function
%>`,
    );
    const page = parseAspDocument(
      "file:///site/default.asp",
      `<%
Response.Write SharedName()
%>`,
    );
    const symbols = [include, page].flatMap((document) => collectVbscriptSymbols(document));
    expect(
      analyzeVbscript(include, { documents: [include, page], symbols }).diagnostics,
    ).toHaveLength(0);

    const global = parseAspDocument(
      "file:///site/Global.asa",
      `<script runat="server" language="VBScript">
Sub Application_OnStart()
End Sub
</script>`,
    );
    expect(analyzeVbscript(global).diagnostics).toHaveLength(0);
  });

  it("keeps private class members referenced through typed variables out of unused diagnostics", () => {
    const parsed = parseAspDocument(
      "file:///site/member-reference.asp",
      `<%
Class Customer
  Private Sub Save()
  End Sub
End Class

Dim customer
Set customer = New Customer
customer.Save
%>`,
    );
    const symbols = collectVbscriptSymbols(parsed);
    const unused = analyzeVbscript(parsed, { symbols }).diagnostics.filter(
      (diagnostic) => diagnostic.source === "asp-lsp-vbscript-unused",
    );
    expect(unused.map((diagnostic) => diagnostic.message)).not.toEqual(
      expect.arrayContaining([expect.stringContaining("Save")]),
    );
  });

  it("tracks VBScript class members and object member completion", () => {
    const parsed = parseAspDocument(
      "file:///site/default.asp",
      `<%
Class Customer
  Public Name
  Public Sub Save()
  End Sub
End Class
Dim c
Set c = New Customer
c.
%>`,
    );
    const symbols = collectVbscriptSymbols(parsed);
    expect(symbols.some((symbol) => symbol.kind === "class" && symbol.name === "Customer")).toBe(
      true,
    );
    expect(
      symbols.some(
        (symbol) =>
          symbol.kind === "field" && symbol.name === "Name" && symbol.memberOf === "Customer",
      ),
    ).toBe(true);
    expect(
      symbols.some(
        (symbol) =>
          symbol.kind === "method" && symbol.name === "Save" && symbol.memberOf === "Customer",
      ),
    ).toBe(true);
    const completions = getVbscriptCompletions(parsed, { line: 8, character: 2 }, { symbols });
    expect(completions.some((item) => item.label === "Name")).toBe(true);
    expect(completions.some((item) => item.label === "Save")).toBe(true);
  });

  it("completes VBScript object members from partial member names", () => {
    const parsed = parseAspDocument(
      "file:///site/default.asp",
      `<%
Class Customer
  Public Name
  Public Sub Save()
  End Sub
End Class
Dim c
Set c = New Customer
Server.HTMLEe
Server.
c.Na
With c
  .Sa
End With
%>`,
    );
    const symbols = collectVbscriptSymbols(parsed);
    const completionLabelsAt = (text: string): string[] =>
      getVbscriptCompletions(
        parsed,
        positionAt(parsed.text, parsed.text.indexOf(text) + text.length),
        { symbols },
      ).map((item) => item.label);

    const partialServerCompletions = completionLabelsAt("Server.HTMLEe");
    expect(partialServerCompletions).toContain("HTMLEncode");
    expect(partialServerCompletions).not.toContain("If Then");
    expect(completionLabelsAt("Server.")).toContain("HTMLEncode");
    expect(completionLabelsAt("c.Na")).toEqual(expect.arrayContaining(["Name", "Save"]));
    expect(completionLabelsAt(".Sa")).toContain("Save");
  });

  it("keeps local variables scoped to their procedure", () => {
    const parsed = parseAspDocument(
      "file:///site/default.asp",
      `<%
Sub First()
  Dim firstOnly
End Sub
Sub Second()

End Sub
%>`,
    );
    const symbols = collectVbscriptSymbols(parsed);
    const completions = getVbscriptCompletions(parsed, { line: 5, character: 2 }, { symbols });
    expect(completions.some((item) => item.label === "firstOnly")).toBe(false);
  });

  it("resolves hover and definition for user-defined symbols", () => {
    const parsed = parseAspDocument(
      "file:///site/default.asp",
      `<%
Function BuildName()
End Function
Response.Write BuildName()
%>`,
    );
    const symbols = collectVbscriptSymbols(parsed);
    const hover = getVbscriptHover(parsed, { line: 3, character: 17 }, { symbols });
    expect(hover).toContain("```vbscript");
    expect(hover).toContain("Function BuildName()");
    expect(hover).not.toContain("VBScript function.");
    expect(getVbscriptDefinition(parsed, { line: 3, character: 17 }, { symbols })?.name).toBe(
      "BuildName",
    );
  });

  it("builds rich hover signatures for class properties", () => {
    const parsed = parseAspDocument(
      "file:///site/default.asp",
      `<%
Class DashboardCustomer
  Public Property Get HasItems()
    HasItems = True
  End Property
End Class
%>`,
    );
    const symbols = collectVbscriptSymbols(parsed);
    const property = symbols.find((symbol) => symbol.name === "HasItems");
    expect(property?.propertyAccessor).toBe("get");
    const hover = getVbscriptHover(parsed, { line: 3, character: 5 }, { symbols });
    expect(hover).toContain("Public Property Get HasItems() As Boolean");
    expect(hover).not.toContain("VBScript property.");
    expect(hover).not.toContain("Member of `DashboardCustomer`.");
  });

  it("resolves built-in hover and signature help from CST tokens", () => {
    const parsed = parseAspDocument(
      "file:///site/default.asp",
      `<%
Response.Write "ok"
Server.MapPath("/tmp")
%>`,
    );
    expect(getVbscriptHover(parsed, { line: 1, character: 2 })).toContain(
      "Classic ASP Response object",
    );
    expect(getVbscriptHover(parsed, { line: 1, character: 2 })).toContain(
      "```vbscript\nDim Response As Response\n```",
    );
    expect(getVbscriptSignatureHelp(parsed, { line: 2, character: 18 })).toEqual(
      expect.objectContaining({
        signatures: [expect.objectContaining({ label: "Server.MapPath(path)" })],
      }),
    );
  });

  it("resolves VBScript built-in function hover, signature help and types", () => {
    const parsed = parseAspDocument(
      "file:///site/default.asp",
      `<%
Dim textValue, items, upperBound, byteValue, intValue, longValue, singleValue, doubleValue
Dim currencyValue, decimalValue, variantValue, errorValue
textValue = CStr(42)
items = Array("a", "b")
upperBound = UBound(items)
byteValue = CByte(1)
intValue = CInt(1)
longValue = CLng(1)
singleValue = CSng(1)
doubleValue = CDbl(1)
currencyValue = CCur(1)
decimalValue = CDec(1)
variantValue = CVar(1)
errorValue = CVErr(1)
datePartValue = DatePart("yyyy", Date())
splitItems = Split("a,b", ",")
joinedText = Join(splitItems, ",")
formatText = FormatCurrency(12)
isObjectValue = IsObject(CreateObject("Scripting.Dictionary"))
typeCode = VarType(joinedText)
%>`,
    );
    const result = analyzeVbscript(parsed);
    const tokens = getVbscriptSemanticTokens(parsed, { symbols: result.symbols });
    const tokenAt = (text: string) => {
      const position = positionAt(parsed.text, parsed.text.indexOf(text));
      return tokens.find(
        (token) =>
          token.range.start.line === position.line &&
          token.range.start.character === position.character,
      );
    };
    expect(result.symbols.find((symbol) => symbol.name === "textValue")?.typeName).toBe("String");
    expect(result.symbols.find((symbol) => symbol.name === "items")?.typeName).toBe("Array");
    expect(result.symbols.find((symbol) => symbol.name === "upperBound")?.typeName).toBe("Number");
    expect(result.symbols.find((symbol) => symbol.name === "byteValue")?.typeName).toBe("Number");
    expect(result.symbols.find((symbol) => symbol.name === "intValue")?.typeName).toBe("Number");
    expect(result.symbols.find((symbol) => symbol.name === "longValue")?.typeName).toBe("Number");
    expect(result.symbols.find((symbol) => symbol.name === "singleValue")?.typeName).toBe("Number");
    expect(result.symbols.find((symbol) => symbol.name === "doubleValue")?.typeName).toBe("Number");
    expect(result.symbols.find((symbol) => symbol.name === "currencyValue")?.typeName).toBe(
      "Currency",
    );
    expect(result.symbols.find((symbol) => symbol.name === "decimalValue")?.typeName).toBe(
      "Decimal",
    );
    expect(result.symbols.find((symbol) => symbol.name === "variantValue")?.typeName).toBe(
      "Variant",
    );
    expect(result.symbols.find((symbol) => symbol.name === "errorValue")?.typeName).toBe("Error");
    expect(result.symbols.find((symbol) => symbol.name === "datePartValue")?.typeName).toBe(
      "Number",
    );
    expect(result.symbols.find((symbol) => symbol.name === "splitItems")?.typeName).toBe("Array");
    expect(result.symbols.find((symbol) => symbol.name === "joinedText")?.typeName).toBe("String");
    expect(result.symbols.find((symbol) => symbol.name === "formatText")?.typeName).toBe("String");
    expect(result.symbols.find((symbol) => symbol.name === "isObjectValue")?.typeName).toBe(
      "Boolean",
    );
    expect(result.symbols.find((symbol) => symbol.name === "typeCode")?.typeName).toBe("Number");
    expect(tokenAt("CStr")).toEqual(
      expect.objectContaining({ tokenType: "function", tokenModifiers: ["library"] }),
    );
    expect(tokenAt("Array")).toEqual(
      expect.objectContaining({ tokenType: "function", tokenModifiers: ["library"] }),
    );
    expect(tokenAt("UBound")).toEqual(
      expect.objectContaining({ tokenType: "function", tokenModifiers: ["library"] }),
    );
    expect(tokenAt("CCur")).toEqual(
      expect.objectContaining({ tokenType: "function", tokenModifiers: ["library"] }),
    );
    expect(
      getVbscriptHover(parsed, positionAt(parsed.text, parsed.text.indexOf("CStr"))),
    ).toContain("Function CStr(value) As String");
    expect(
      getVbscriptHover(parsed, positionAt(parsed.text, parsed.text.indexOf("CStr"))),
    ).toContain("Converts a value to String.");
    expect(
      getVbscriptHover(parsed, positionAt(parsed.text, parsed.text.indexOf("CStr"))),
    ).not.toContain("VBScript built-in function.");
    expect(
      getVbscriptHover(parsed, positionAt(parsed.text, parsed.text.indexOf("CCur"))),
    ).toContain("Function CCur(value) As Currency");
    expect(
      getVbscriptHover(parsed, positionAt(parsed.text, parsed.text.indexOf("Split"))),
    ).toContain("Function Split(expression, delimiter, count, compare) As Array");
    expect(getVbscriptSignatureHelp(parsed, { line: 5, character: 21 })).toEqual(
      expect.objectContaining({
        signatures: [expect.objectContaining({ label: "UBound(array, dimension)" })],
      }),
    );
    expect(
      getVbscriptSignatureHelp(
        parsed,
        positionAt(parsed.text, parsed.text.indexOf('DatePart("yyyy"') + 'DatePart("yyyy"'.length),
      ),
    ).toEqual(
      expect.objectContaining({
        signatures: [
          expect.objectContaining({
            label: "DatePart(interval, date, firstDayOfWeek, firstWeekOfYear)",
          }),
        ],
      }),
    );
  });

  it("covers W3Schools ASP object members, runtime events and member hover", () => {
    const parsed = parseAspDocument(
      "file:///site/Global.asa",
      `<script runat="server" language="VBScript">
Sub Application_OnStart()
End Sub
Dim lastError
Set lastError = Server.GetLastError()
Response.
Server.
lastError.
Response.Buffer = True
Response.Write lastError.Description
</script>`,
    );
    const symbols = collectVbscriptSymbols(parsed);
    const responseCompletions = getVbscriptCompletions(
      parsed,
      positionAt(parsed.text, parsed.text.indexOf("Response.") + "Response.".length),
      { symbols },
    );
    const serverCompletions = getVbscriptCompletions(
      parsed,
      positionAt(parsed.text, parsed.text.indexOf("Server.") + "Server.".length),
      { symbols },
    );
    const errorCompletions = getVbscriptCompletions(
      parsed,
      positionAt(parsed.text, parsed.text.indexOf("lastError.") + "lastError.".length),
      { symbols },
    );
    const topLevelCompletions = getVbscriptCompletions(
      parsed,
      { line: 1, character: 0 },
      { symbols },
    );
    expect(responseCompletions.some((item) => item.label === "Buffer")).toBe(true);
    expect(serverCompletions.some((item) => item.label === "Execute")).toBe(true);
    expect(errorCompletions.some((item) => item.label === "Description")).toBe(true);
    expect(topLevelCompletions.some((item) => item.label === "Application_OnStart")).toBe(true);
    expect(
      getVbscriptHover(parsed, positionAt(parsed.text, parsed.text.indexOf("Buffer"))),
    ).toContain("property Response.Buffer As Boolean");
    expect(
      getVbscriptHover(parsed, positionAt(parsed.text, parsed.text.lastIndexOf("Description"))),
    ).toContain("property ASPError.Description As String");
    expect(
      getVbscriptHover(parsed, positionAt(parsed.text, parsed.text.indexOf("Application_OnStart"))),
    ).toContain("Sub Application_OnStart()");
    expect(symbols.find((symbol) => symbol.name === "lastError")?.typeName).toBe("ASPError");
  });

  it("treats Err as a typed VBScript built-in object", () => {
    const parsed = parseAspDocument(
      "file:///site/err-object.asp",
      `<%
Option Explicit
On Error Resume Next
Dim captured, errNumber, errDescription
Set captured = Err
errNumber = Err.Number
errDescription = Err.Description
Err.Clear
Err.Raise(vbObjectError + 513, "App", "Boom")
Err.
%>`,
    );
    const symbols = collectVbscriptSymbols(parsed);
    const diagnostics = analyzeVbscript(parsed, {
      symbols,
      unusedDiagnostics: false,
    }).diagnostics;
    expect(
      diagnostics.filter((diagnostic) => diagnostic.source === "asp-lsp-vbscript"),
    ).toHaveLength(0);
    expect(
      diagnostics.filter((diagnostic) => diagnostic.source === "asp-lsp-vbscript-syntax"),
    ).toHaveLength(0);

    const memberCompletions = getVbscriptCompletions(
      parsed,
      positionAt(parsed.text, parsed.text.lastIndexOf("Err.") + "Err.".length),
      { symbols },
    );
    expect(memberCompletions.map((item) => item.label)).toEqual(
      expect.arrayContaining(["Number", "Description", "Clear", "Raise"]),
    );

    const topLevelCompletions = getVbscriptCompletions(
      parsed,
      { line: 1, character: 0 },
      { symbols },
    );
    const errCompletion = topLevelCompletions.find((item) => item.label === "Err");
    expect(errCompletion).toMatchObject({
      kind: CompletionItemKind.Variable,
    });
    expect(resolveVbscriptCompletionItem(errCompletion!, parsed, { symbols })).toMatchObject({
      detail: "VBScript Err object",
    });

    expect(symbols.find((symbol) => symbol.name === "captured")?.typeName).toBe("ErrObject");
    expect(symbols.find((symbol) => symbol.name === "errNumber")?.typeName).toBe("Number");
    expect(symbols.find((symbol) => symbol.name === "errDescription")?.typeName).toBe("String");
    expect(
      getVbscriptHover(
        parsed,
        positionAt(
          parsed.text,
          parsed.text.indexOf("Set captured = Err") + "Set captured = ".length,
        ),
      ),
    ).toContain("Dim Err As ErrObject");
    expect(
      getVbscriptHover(
        parsed,
        positionAt(parsed.text, parsed.text.indexOf("Err.Number") + "Err.".length),
      ),
    ).toContain("property ErrObject.Number As Number");
    expect(
      getVbscriptHover(parsed, positionAt(parsed.text, parsed.text.indexOf("Raise"))),
    ).toContain("ErrObject.Raise(number, source, description, helpfile, helpcontext)");
    expect(
      getVbscriptSignatureHelp(
        parsed,
        positionAt(parsed.text, parsed.text.indexOf("Err.Raise(") + "Err.Raise(".length),
        { symbols },
      ),
    ).toEqual(
      expect.objectContaining({
        signatures: [
          expect.objectContaining({
            label: "Err.Raise(number, source, description, helpfile, helpcontext)",
          }),
        ],
      }),
    );
  });

  it("covers W3Schools COM and ADO catalog completions and return types", () => {
    const parsed = parseAspDocument(
      "file:///site/default.asp",
      `<%
Dim fso, file, dict, stream, rs, command, rows, textRows, parameter
Set fso = Server.CreateObject("Scripting.FileSystemObject")
Set file = fso.GetFile("default.asp")
Set dict = CreateObject("Scripting.Dictionary")
Set stream = Server.CreateObject("ADODB.Stream")
Set rs = Server.CreateObject("ADODB.Recordset")
Set command = Server.CreateObject("ADODB.Command")
rows = rs.GetRows()
textRows = rs.GetString()
Set parameter = command.CreateParameter("id", adInteger)
fso.
file.
dict.
stream.
rs.
command.
%>`,
    );
    const symbols = collectVbscriptSymbols(parsed);
    const completionLabelsAt = (text: string): string[] =>
      getVbscriptCompletions(
        parsed,
        positionAt(parsed.text, parsed.text.indexOf(text) + text.length),
        { symbols },
      ).map((item) => item.label);
    expect(completionLabelsAt("fso.")).toContain("OpenTextFile");
    expect(completionLabelsAt("file.")).toContain("OpenAsTextStream");
    expect(completionLabelsAt("dict.")).toContain("Exists");
    expect(completionLabelsAt("stream.")).toContain("ReadText");
    expect(completionLabelsAt("rs.")).toContain("GetRows");
    expect(completionLabelsAt("command.")).toContain("CreateParameter");
    expect(symbols.find((symbol) => symbol.name === "file")?.typeName).toBe("Scripting.File");
    expect(symbols.find((symbol) => symbol.name === "rows")?.typeName).toBe("Array");
    expect(symbols.find((symbol) => symbol.name === "textRows")?.typeName).toBe("String");
    expect(symbols.find((symbol) => symbol.name === "parameter")?.typeName).toBe("ADODB.Parameter");
    expect(
      getVbscriptHover(parsed, positionAt(parsed.text, parsed.text.indexOf("adInteger"))),
    ).toContain("Const adInteger As Number");
  });

  it("treats RegExp as a typed VBScript built-in object", () => {
    const parsed = parseAspDocument(
      "file:///site/regexp.asp",
      `<%
Option Explicit
Dim re, created, matches, firstMatch, subItems, subText, replaced, found
Set re = New RegExp
Set created = CreateObject("VBScript.RegExp")
re.Pattern = "(\\w+)-(\\d+)"
re.Global = True
re.IgnoreCase = True
re.MultiLine = True
Set matches = re.Execute("abc-123")
Set firstMatch = matches.Item(0)
Set subItems = firstMatch.SubMatches
subText = subItems.Item(0)
replaced = re.Replace("abc-123", "$1")
found = created.Test("abc-123")
re.
created.
matches.
firstMatch.
%>`,
    );
    const symbols = collectVbscriptSymbols(parsed);
    const diagnostics = analyzeVbscript(parsed, {
      symbols,
      unusedDiagnostics: false,
    }).diagnostics;
    expect(
      diagnostics.filter((diagnostic) => diagnostic.source === "asp-lsp-vbscript"),
    ).toHaveLength(0);
    expect(symbols.find((symbol) => symbol.name === "re")?.typeName).toBe("RegExp");
    expect(symbols.find((symbol) => symbol.name === "created")?.typeName).toBe("RegExp");
    expect(symbols.find((symbol) => symbol.name === "matches")?.typeName).toBe("Matches");
    expect(symbols.find((symbol) => symbol.name === "firstMatch")?.typeName).toBe("Match");
    expect(symbols.find((symbol) => symbol.name === "subItems")?.typeName).toBe("SubMatches");
    expect(symbols.find((symbol) => symbol.name === "subText")?.typeName).toBe("String");
    expect(symbols.find((symbol) => symbol.name === "replaced")?.typeName).toBe("String");
    expect(symbols.find((symbol) => symbol.name === "found")?.typeName).toBe("Boolean");

    const completionLabelsAt = (text: string): string[] =>
      getVbscriptCompletions(
        parsed,
        positionAt(parsed.text, parsed.text.indexOf(text) + text.length),
        { symbols },
      ).map((item) => item.label);
    expect(completionLabelsAt("re.")).toEqual(
      expect.arrayContaining(["Pattern", "Global", "IgnoreCase", "MultiLine", "Execute"]),
    );
    expect(completionLabelsAt("created.")).toContain("Test");
    expect(completionLabelsAt("matches.")).toEqual(expect.arrayContaining(["Count", "Item"]));
    expect(completionLabelsAt("firstMatch.")).toEqual(
      expect.arrayContaining(["FirstIndex", "Length", "Value", "SubMatches"]),
    );

    const topLevelCompletions = getVbscriptCompletions(
      parsed,
      { line: 1, character: 0 },
      { symbols },
    );
    expect(topLevelCompletions.find((item) => item.label === "RegExp")).toMatchObject({
      kind: CompletionItemKind.Class,
    });
    const topLevelCompletionsJa = getVbscriptCompletions(
      parsed,
      { line: 1, character: 0 },
      { symbols, locale: "ja" },
    );
    expect(
      String(
        resolveVbscriptCompletionItem(
          topLevelCompletionsJa.find((item) => item.label === "RegExp")!,
          parsed,
          { symbols, locale: "ja" },
        ).documentation,
      ),
    ).toContain("Pattern を使って");

    expect(
      getVbscriptHover(
        parsed,
        positionAt(parsed.text, parsed.text.indexOf("New RegExp") + "New ".length),
        { symbols, locale: "en" },
      ),
    ).toContain("Class RegExp");
    expect(
      getVbscriptHover(
        parsed,
        positionAt(parsed.text, parsed.text.indexOf('re.Execute("') + "re.".length),
        { symbols, locale: "en" },
      ),
    ).toContain("RegExp.Execute(source)");
    expect(
      getVbscriptSignatureHelp(
        parsed,
        positionAt(parsed.text, parsed.text.indexOf('re.Execute("') + "re.Execute(".length),
        { symbols, locale: "en" },
      )?.signatures[0],
    ).toEqual(
      expect.objectContaining({
        label: "re.Execute(source)",
        documentation: expect.stringContaining("Matches collection"),
        parameters: [
          expect.objectContaining({
            label: "source",
            documentation: "String to search with the current Pattern.",
          }),
        ],
      }),
    );
  });

  it("keeps ADO constants built-in without treating member names as globals", () => {
    const parsed = parseAspDocument(
      "file:///site/default.asp",
      `<% Option Explicit
Dim fieldType
fieldType = adInteger
Name = "Ada"
%>`,
    );
    const diagnostics = analyzeVbscript(parsed).diagnostics.filter(
      (diagnostic) => diagnostic.source === "asp-lsp-vbscript",
    );
    expect(diagnostics.some((diagnostic) => diagnostic.message.includes("adInteger"))).toBe(false);
    expect(diagnostics.some((diagnostic) => diagnostic.message.includes("Name"))).toBe(true);
    expect(
      getVbscriptCompletions(parsed, { line: 2, character: 12 }).some(
        (item) => item.label === "adInteger",
      ),
    ).toBe(true);
  });

  it("keeps declaration-free VBScript constants built-in", () => {
    const parsed = parseAspDocument(
      "file:///site/default.asp",
      `<% Option Explicit
Dim lineBreak, compareMode, promptStyle, excluded
lineBreak = vbCrLf
compareMode = vbTextCompare
promptStyle = vbOKOnly
excluded = vbOK
%>`,
    );
    const symbols = collectVbscriptSymbols(parsed);
    const diagnostics = analyzeVbscript(parsed).diagnostics.filter(
      (diagnostic) => diagnostic.source === "asp-lsp-vbscript",
    );
    expect(diagnostics.some((diagnostic) => diagnostic.message.includes("vbCrLf"))).toBe(false);
    expect(diagnostics.some((diagnostic) => diagnostic.message.includes("vbTextCompare"))).toBe(
      false,
    );
    expect(diagnostics.some((diagnostic) => diagnostic.message.includes("vbOKOnly"))).toBe(false);
    expect(diagnostics.some((diagnostic) => diagnostic.message.includes("vbOK"))).toBe(true);

    const completions = getVbscriptCompletions(parsed, { line: 2, character: 0 }, { symbols });
    expect(completions.some((item) => item.label === "vbCrLf")).toBe(true);
    expect(completions.some((item) => item.label === "vbTextCompare")).toBe(true);
    expect(completions.some((item) => item.label === "vbOKOnly")).toBe(true);
    expect(completions.some((item) => item.label === "vbOK")).toBe(false);

    const crlfHover = getVbscriptHover(
      parsed,
      positionAt(parsed.text, parsed.text.indexOf("vbCrLf")),
      { symbols, locale: "en" },
    );
    expect(crlfHover).toContain("Const vbCrLf As String");
    expect(crlfHover).toContain("VBScript string constant");
    expect(crlfHover).toContain("Value: Chr(13) + Chr(10).");
    const okOnlyHover = getVbscriptHover(
      parsed,
      positionAt(parsed.text, parsed.text.indexOf("vbOKOnly")),
      { symbols, locale: "en" },
    );
    expect(okOnlyHover).toContain("Const vbOKOnly As Number");
    expect(okOnlyHover).toContain("VBScript MsgBox argument constant");
    expect(okOnlyHover).toContain("Value: 0.");

    const okOnlyCompletion = completions.find((item) => item.label === "vbOKOnly");
    expect(
      String(
        resolveVbscriptCompletionItem(okOnlyCompletion!, parsed, { symbols, locale: "en" })
          .documentation,
      ),
    ).toContain("VBScript MsgBox argument constant");
  });

  it("localizes built-in function documentation and parameter help", () => {
    const parsed = parseAspDocument(
      "file:///site/default.asp",
      `<%
Dim datePartValue
datePartValue = DatePart("yyyy", Date())
%>`,
    );
    const datePartOffset = parsed.text.indexOf("DatePart");
    const hoverEn = getVbscriptHover(parsed, positionAt(parsed.text, datePartOffset), {
      locale: "en",
    });
    const hoverJa = getVbscriptHover(parsed, positionAt(parsed.text, datePartOffset), {
      locale: "ja",
    });
    expect(hoverEn).toContain("Returns the requested interval part");
    expect(hoverEn).toContain("**Parameters**");
    expect(hoverEn).toContain("`interval`: Interval code");
    expect(hoverEn).toContain("**Returns**");
    expect(hoverJa).toContain("date expression から指定した interval part");
    expect(hoverJa).toContain("**パラメーター**");
    expect(hoverJa).toContain("`interval`: 返す interval code");
    expect(hoverJa).toContain("date の指定部分");

    const signature = getVbscriptSignatureHelp(
      parsed,
      positionAt(parsed.text, parsed.text.indexOf('DatePart("yyyy"') + 'DatePart("yyyy"'.length),
      { locale: "en" },
    );
    expect(signature).toEqual(
      expect.objectContaining({
        signatures: [
          expect.objectContaining({
            documentation: expect.stringContaining("Returns the requested interval part"),
            parameters: [
              expect.objectContaining({ documentation: expect.stringContaining("Interval code") }),
              expect.objectContaining({ documentation: "Date expression to evaluate." }),
              expect.objectContaining({
                documentation: expect.stringContaining("first day of the week"),
              }),
              expect.objectContaining({
                documentation: expect.stringContaining("first week of the year"),
              }),
            ],
          }),
        ],
      }),
    );

    const completion = getVbscriptCompletions(
      parsed,
      { line: 1, character: 0 },
      { locale: "ja" },
    ).find((item) => item.label === "DatePart");
    const resolved = resolveVbscriptCompletionItem(completion!, parsed, { locale: "ja" });
    expect(String(resolved.documentation)).toContain("**パラメーター**");
    expect(String(resolved.documentation)).toContain("date の指定部分");
  });

  it("localizes ASP and ADO member documentation and parameter help", () => {
    const parsed = parseAspDocument(
      "file:///site/default.asp",
      `<%
Dim rs, rows, fieldType
Set rs = Server.CreateObject("ADODB.Recordset")
Response.Buffer = True
Server.Execute("next.asp")
rows = rs.GetRows(10, 0)
fieldType = adInteger
Response.
Server.
rs.
%>`,
    );
    const symbols = collectVbscriptSymbols(parsed);
    const bufferHover = getVbscriptHover(
      parsed,
      positionAt(parsed.text, parsed.text.indexOf("Buffer")),
      { symbols, locale: "ja" },
    );
    expect(bufferHover).toContain("page output を buffer");
    expect(bufferHover).toContain("**値**");
    expect(bufferHover).toContain("html tag より前");

    const responseCompletions = getVbscriptCompletions(
      parsed,
      positionAt(parsed.text, parsed.text.indexOf("Response.") + "Response.".length),
      { symbols },
    );
    const bufferCompletion = responseCompletions.find((item) => item.label === "Buffer");
    expect(
      String(
        resolveVbscriptCompletionItem(bufferCompletion!, parsed, {
          symbols,
          locale: "en",
        }).documentation,
      ),
    ).toContain("Controls whether ASP buffers page output");

    const serverCompletions = getVbscriptCompletions(
      parsed,
      positionAt(parsed.text, parsed.text.indexOf("Server.") + "Server.".length),
      { symbols },
    );
    const executeCompletion = serverCompletions.find((item) => item.label === "Execute");
    expect(
      String(
        resolveVbscriptCompletionItem(executeCompletion!, parsed, {
          symbols,
          locale: "ja",
        }).documentation,
      ),
    ).toContain("別の ASP page を実行");

    const executeSignature = getVbscriptSignatureHelp(
      parsed,
      positionAt(
        parsed.text,
        parsed.text.indexOf('Server.Execute("next.asp"') + "Server.Execute(".length,
      ),
      { symbols, locale: "en" },
    );
    expect(executeSignature?.signatures[0]).toEqual(
      expect.objectContaining({
        documentation: expect.stringContaining("Runs another ASP page"),
        parameters: [
          expect.objectContaining({
            label: "path",
            documentation: "Relative or absolute path of the ASP page to execute.",
          }),
        ],
      }),
    );

    const getRowsSignature = getVbscriptSignatureHelp(
      parsed,
      positionAt(parsed.text, parsed.text.indexOf("rs.GetRows(10,") + "rs.GetRows(10,".length),
      { symbols, locale: "ja" },
    );
    expect(getRowsSignature?.signatures[0]).toEqual(
      expect.objectContaining({
        documentation: expect.stringContaining("2 次元 array"),
        parameters: [
          expect.objectContaining({
            label: "rows",
            documentation: "取得する records 数です。省略すると Recordset の残りを取得します。",
          }),
          expect.objectContaining({
            label: "start",
            documentation: "copy を開始する record number または bookmark です。",
          }),
          expect.objectContaining({
            label: "fields",
            documentation: "含める field name/number、または field names/numbers の array です。",
          }),
        ],
      }),
    );

    const adIntegerHover = getVbscriptHover(
      parsed,
      positionAt(parsed.text, parsed.text.indexOf("adInteger")),
      { symbols, locale: "ja" },
    );
    expect(adIntegerHover).toContain("32-bit signed integer value");
    expect(adIntegerHover).toContain("**値**");
    const adIntegerCompletion = getVbscriptCompletions(
      parsed,
      { line: 1, character: 0 },
      {
        symbols,
        locale: "en",
      },
    ).find((item) => item.label === "adInteger");
    expect(
      String(
        resolveVbscriptCompletionItem(adIntegerCompletion!, parsed, { symbols, locale: "en" })
          .documentation,
      ),
    ).toContain("ADO data type constant for a 32-bit signed integer value.");
  });

  it("uses triple-quote XML documentation comments for hover, resolve and signature help", () => {
    const parsed = parseAspDocument(
      "file:///site/default.asp",
      `<%
''' <summary>Builds a display name.</summary>
''' <param name="first">First name.</param>
''' <returns>Display name.</returns>
Function BuildName(first)
  BuildName = first
End Function
Response.Write BuildName("Ada")
%>`,
    );
    const symbols = collectVbscriptSymbols(parsed);
    const hover = getVbscriptHover(parsed, { line: 7, character: 17 }, { symbols });
    expect(hover).toContain("```vbscript");
    expect(hover).toContain("Function BuildName(ByRef first)");
    expect(hover).toContain("Builds a display name.");
    expect(hover).toContain("First name.");
    expect(hover).toContain("Display name.");
    const completion = getVbscriptCompletions(parsed, { line: 7, character: 16 }, { symbols }).find(
      (item) => item.label === "BuildName",
    );
    expect(completion?.labelDetails).toBeUndefined();
    const completionDocumentation = String(
      resolveVbscriptCompletionItem(completion!, parsed, { symbols }).documentation,
    );
    expect(completionDocumentation).toContain("Builds a display name.");
    expect(completionDocumentation).not.toContain("XML documentation is descriptive only.");
    expect(getVbscriptSignatureHelp(parsed, { line: 7, character: 26 }, { symbols })).toEqual(
      expect.objectContaining({
        signatures: [
          expect.objectContaining({
            documentation: expect.stringContaining("Builds a display name."),
            parameters: [
              expect.objectContaining({
                label: "ByRef first",
                documentation: "First name.",
              }),
            ],
          }),
        ],
      }),
    );
  });

  it("uses regular comment blocks as fallback VBScript documentation", () => {
    const source = `<%
' Build a display name.
' Used by dashboard headers.
Function BuildName(first)
  BuildName = first
End Function
Response.Write BuildName("Ada")

' @type customerId As String
' Customer id from the request.
Dim customerId
Dim trailingValue ' Trailing value docs.
Const TrailingConst = 10 ' Trailing constant docs.
Dim firstValue, secondValue ' Ambiguous trailing docs.
Response.Write "not docs" ' Inline code comment before declaration.
Dim inlineNeighbor

' Plain fallback.
''' <summary>XML summary wins.</summary>
Function XmlDocumented()
End Function
%>`;
    const parsed = parseAspDocument("file:///site/default.asp", source);
    const symbols = collectVbscriptSymbols(parsed);
    const functionHover = getVbscriptHover(
      parsed,
      positionAt(source, source.indexOf("BuildName(first)")),
      { symbols },
    );
    expect(functionHover).toContain("Build a display name\\.");
    expect(functionHover).toContain("Used by dashboard headers\\.");

    const completion = getVbscriptCompletions(
      parsed,
      positionAt(source, source.indexOf('BuildName("Ada")') + "BuildName".length),
      { symbols },
    ).find((item) => item.label === "BuildName");
    expect(
      String(resolveVbscriptCompletionItem(completion!, parsed, { symbols }).documentation),
    ).toContain("Build a display name\\.");

    const variableHover = getVbscriptHover(
      parsed,
      positionAt(source, source.indexOf("Dim customerId") + "Dim ".length),
      { symbols },
    );
    expect(variableHover).not.toContain("Customer id from the request.");
    expect(variableHover).not.toContain("@type customerId");

    const trailingValueHover = getVbscriptHover(
      parsed,
      positionAt(source, source.indexOf("trailingValue")),
      { symbols },
    );
    expect(trailingValueHover).toContain("Trailing value docs\\.");

    const trailingConstHover = getVbscriptHover(
      parsed,
      positionAt(source, source.indexOf("TrailingConst")),
      { symbols },
    );
    expect(trailingConstHover).toContain("Trailing constant docs\\.");

    const firstValueHover = getVbscriptHover(
      parsed,
      positionAt(source, source.indexOf("firstValue")),
      { symbols },
    );
    const secondValueHover = getVbscriptHover(
      parsed,
      positionAt(source, source.indexOf("secondValue")),
      { symbols },
    );
    expect(firstValueHover).not.toContain("Ambiguous trailing docs.");
    expect(secondValueHover).not.toContain("Ambiguous trailing docs.");

    const inlineNeighborHover = getVbscriptHover(
      parsed,
      positionAt(source, source.indexOf("inlineNeighbor")),
      { symbols },
    );
    expect(inlineNeighborHover).not.toContain("Inline code comment before declaration.");

    const xmlHover = getVbscriptHover(
      parsed,
      positionAt(source, source.indexOf("XmlDocumented()")),
      { symbols },
    );
    expect(xmlHover).toContain("XML summary wins.");
    expect(xmlHover).not.toContain("Plain fallback.");
  });

  it("keeps plain comment docs escaped and tagless triple-quote docs as markdown", () => {
    const source = `<%
''' **Markdown** docs with \`code\`.
''' Second markdown line.
Function MarkdownDocumented()
End Function

' **Plain** docs with <summary>tag</summary>.
' Second plain line.
Function PlainDocumented()
End Function
Dim trailingPlain ' trailing **plain** <value>
%>`;
    const parsed = parseAspDocument("file:///site/default.asp", source);
    const symbols = collectVbscriptSymbols(parsed);
    const markdownHover = getVbscriptHover(
      parsed,
      positionAt(source, source.indexOf("MarkdownDocumented()")),
      { symbols },
    );
    expect(markdownHover).toContain("**Markdown** docs with `code`.  \nSecond markdown line.");

    const plainHover = getVbscriptHover(
      parsed,
      positionAt(source, source.indexOf("PlainDocumented()")),
      { symbols },
    );
    expect(plainHover).toContain("\\*\\*Plain\\*\\* docs");
    expect(plainHover).toContain("&lt;summary&gt;tag&lt;/summary&gt;");
    expect(plainHover).toContain("&lt;/summary&gt;\\.  \nSecond plain line\\.");
    expect(plainHover).not.toContain("**Plain** docs");
    expect(plainHover).not.toContain("<summary>tag</summary>");

    const trailingHover = getVbscriptHover(
      parsed,
      positionAt(source, source.indexOf("trailingPlain")),
      { symbols },
    );
    expect(trailingHover).toContain("\\*\\*plain\\*\\*");
    expect(trailingHover).toContain("&lt;value&gt;");
  });

  it("generates VBScript documentation and type annotations for undocumented functions", () => {
    const source = `<%
Function BuildName(first)
  BuildName = first
End Function
%>`;
    const parsed = parseAspDocument("file:///site/default.asp", source);
    const action = getVbscriptDocumentationQuickAction(
      parsed,
      positionAt(source, source.indexOf("BuildName")),
    );
    expect(action?.edits).toHaveLength(1);
    const updated = applyTextEdits(source, action?.edits ?? []);
    expect(updated).toContain("' @param BuildName.first As Variant");
    expect(updated).toContain("' @returns BuildName Variant");
    expect(updated).toContain("''' <summary>TODO: Describe BuildName.</summary>");
    expect(updated).toContain("''' <param name=\"first\">TODO: Describe first.</param>");
    expect(updated).toContain("''' <returns>TODO: Describe return value.</returns>");
  });

  it("adds only missing VBScript documentation items to an existing summary block", () => {
    const source = `<%
''' <summary>Builds a display name.</summary>
Function BuildName(first)
  BuildName = first
End Function
%>`;
    const parsed = parseAspDocument("file:///site/default.asp", source);
    const action = getVbscriptDocumentationQuickAction(
      parsed,
      positionAt(source, source.indexOf("BuildName(first)")),
    );
    const updated = applyTextEdits(source, action?.edits ?? []);
    expect(updated.match(/<summary>/g)).toHaveLength(1);
    expect(updated).toContain("''' <summary>Builds a display name.</summary>");
    expect(updated).toContain("''' <param name=\"first\">TODO: Describe first.</param>");
    expect(updated).toContain("''' <returns>TODO: Describe return value.</returns>");
    const updatedSymbols = collectVbscriptSymbols(
      parseAspDocument("file:///site/default.asp", updated),
    );
    expect(
      updatedSymbols.find((symbol) => symbol.name === "BuildName")?.documentation?.summary,
    ).toBe("Builds a display name.");
  });

  it("generates VBScript documentation for broad declaration symbol kinds", () => {
    const cases = [
      {
        source: `<%
Dim ▮customerName
%>`,
        expected: ["' @type customerName As Variant", "TODO: Describe customerName.", "<value>"],
      },
      {
        source: `<%
Const ▮MaxItems = 10
%>`,
        expected: ["' @type MaxItems As Number", "TODO: Describe MaxItems.", "<value>"],
      },
      {
        source: `<%
Class ▮Customer
End Class
%>`,
        expected: ["<summary>TODO: Describe Customer.</summary>"],
      },
      {
        source: `<%
Class Customer
  Public ▮Name
End Class
%>`,
        expected: ["' @type Name As Variant", "TODO: Describe Name.", "<value>"],
      },
      {
        source: `<%
Class Customer
  Public Property Get ▮Name()
    Name = mName
  End Property
End Class
%>`,
        expected: ["' @returns Name Variant", "TODO: Describe Name.", "<returns>", "<value>"],
      },
      {
        source: `<%
Sub Save(▮force)
End Sub
%>`,
        expected: ["' @param Save.force As Variant", '<param name="force">'],
      },
    ];
    for (const item of cases) {
      const marked = markedDocument(item.source);
      const parsed = parseAspDocument("file:///site/default.asp", marked.text);
      const action = getVbscriptDocumentationQuickAction(parsed, marked.position);
      const updated = applyTextEdits(marked.text, action?.edits ?? []);
      for (const expected of item.expected) {
        expect(updated).toContain(expected);
      }
    }
  });

  it("avoids ambiguous XML documentation for multi-name VBScript declarations", () => {
    const marked = markedDocument(`<%
Dim ▮first, second
%>`);
    const parsed = parseAspDocument("file:///site/default.asp", marked.text);
    const action = getVbscriptDocumentationQuickAction(parsed, marked.position);
    const updated = applyTextEdits(marked.text, action?.edits ?? []);
    expect(updated).toContain("' @type first As Variant");
    expect(updated).not.toContain("''' <summary>");
  });

  it("uses XML parameter documentation for declaration hover and no-paren signature help", () => {
    const source = `<%
''' <summary>Renders the metric cards for the dashboard.</summary>
''' <param name="metricMap">A dictionary of metric keys and values to render.</param>
Sub RenderMetricCards(ByVal metricMap)
End Sub
RenderMetricCards metrics
%>`;
    const parsed = parseAspDocument("file:///site/default.asp", source);
    const symbols = collectVbscriptSymbols(parsed);
    const parameterHover = getVbscriptHover(
      parsed,
      positionAt(source, source.indexOf("metricMap)")),
      { symbols },
    );
    expect(parameterHover).toContain("ByVal metricMap");
    expect(parameterHover).toContain("A dictionary of metric keys and values to render.");
    expect(parameterHover).not.toContain("XML documentation is descriptive only.");

    const signature = getVbscriptSignatureHelp(
      parsed,
      positionAt(source, source.indexOf("metrics") + 2),
      { symbols },
    );
    expect(signature).toEqual(
      expect.objectContaining({
        signatures: [
          expect.objectContaining({
            label: "Sub RenderMetricCards(ByVal metricMap)",
            parameters: [
              expect.objectContaining({
                label: "ByVal metricMap",
                documentation: "A dictionary of metric keys and values to render.",
              }),
            ],
          }),
        ],
      }),
    );
    expect(JSON.stringify(signature)).not.toContain("XML documentation is descriptive only.");
    expect(
      getVbscriptSignatureHelp(
        parsed,
        positionAt(
          source,
          source.indexOf("RenderMetricCards metrics") + "RenderMetricCards ".length,
        ),
        { symbols },
      )?.signatures[0]?.label,
    ).toBe("Sub RenderMetricCards(ByVal metricMap)");
  });

  it("shows the XML documentation type note only on declarations missing metadata", () => {
    const source = `<%
''' <summary>Customer id.</summary>
Dim customerId

' @type typedValue As String
''' <summary>Typed value.</summary>
Dim typedValue

''' <summary>Renders metrics.</summary>
''' <param name="metricMap">Metric values.</param>
Sub RenderMetricCards(ByVal metricMap)
End Sub
RenderMetricCards metrics

' @param first As String
' @returns BuildName String
''' <summary>Builds a display name.</summary>
''' <param name="first">First name.</param>
''' <returns>Display name.</returns>
Function BuildName(first)
End Function
Response.Write BuildName("Ada")
%>`;
    const parsed = parseAspDocument("file:///site/default.asp", source);
    const symbols = collectVbscriptSymbols(parsed);
    const missingVariableHover = getVbscriptHover(
      parsed,
      positionAt(source, source.indexOf("customerId")),
      { symbols },
    );
    expect(missingVariableHover).toContain("XML documentation is descriptive only.");

    const typedVariableHover = getVbscriptHover(
      parsed,
      positionAt(source, source.indexOf("Dim typedValue") + "Dim ".length),
      { symbols },
    );
    expect(typedVariableHover).not.toContain("XML documentation is descriptive only.");

    const missingParamHover = getVbscriptHover(
      parsed,
      positionAt(source, source.indexOf("RenderMetricCards(ByVal")),
      { symbols },
    );
    expect(missingParamHover).toContain("XML documentation is descriptive only.");

    const callHover = getVbscriptHover(
      parsed,
      positionAt(source, source.lastIndexOf("RenderMetricCards")),
      { symbols },
    );
    expect(callHover).not.toContain("XML documentation is descriptive only.");

    const typedFunctionHover = getVbscriptHover(
      parsed,
      positionAt(source, source.indexOf("BuildName(first)")),
      { symbols },
    );
    expect(typedFunctionHover).not.toContain("XML documentation is descriptive only.");
  });

  it("parses XML documentation with a tokenizer and keeps variable docs unambiguous", () => {
    const parsed = parseAspDocument(
      "file:///site/default.asp",
      `<%
''' <summary>Outer <summary>inner</summary> tail</summary>
Function BuildName()
End Function

''' <summary>One value.</summary>
Dim oneValue

''' <summary>Ambiguous values.</summary>
Dim firstValue, secondValue
%>`,
    );
    expect(getVbscriptHover(parsed, { line: 2, character: 10 })).toContain("Outer inner tail");
    expect(getVbscriptHover(parsed, { line: 6, character: 5 })).toContain("One value.");
    expect(getVbscriptHover(parsed, { line: 9, character: 5 })).not.toContain("Ambiguous values.");
  });

  it("uses single-quote XML-looking comments as plain text and tolerates broken XML documentation", () => {
    const single = parseAspDocument(
      "file:///site/default.asp",
      `<%
' <summary>Not documentation.</summary>
Function BuildName()
End Function
%>`,
    );
    const singleHover = getVbscriptHover(single, { line: 2, character: 10 });
    expect(singleHover).toContain("&lt;summary&gt;Not documentation\\.&lt;/summary&gt;");
    expect(singleHover).not.toContain("<summary>Not documentation.</summary>");

    const broken = parseAspDocument(
      "file:///site/default.asp",
      `<%
''' <summary>Broken but useful
Function BuildName()
End Function
%>`,
    );
    expect(getVbscriptHover(broken, { line: 2, character: 10 })).toContain("Broken but useful");
  });

  it("completes VBScript XML documentation tags, attributes and symbol references", () => {
    const tag = markedDocument(`<%
''' <▮
Function BuildName(first)
End Function
%>`);
    expect(
      getVbscriptCompletions(parseAspDocument("file:///site/default.asp", tag.text), tag.position)
        .map((item) => item.label)
        .slice(0, 4),
    ).toContain("summary");

    const param = markedDocument(`<%
''' <param name="▮"></param>
Function BuildName(first)
End Function
%>`);
    expect(
      getVbscriptCompletions(
        parseAspDocument("file:///site/default.asp", param.text),
        param.position,
      ).some((item) => item.label === "first"),
    ).toBe(true);

    const attribute = markedDocument(`<%
''' <see ▮/>
Function BuildName(first)
End Function
%>`);
    expect(
      getVbscriptCompletions(
        parseAspDocument("file:///site/default.asp", attribute.text),
        attribute.position,
      ).some((item) => item.label === "cref"),
    ).toBe(true);

    const closing = markedDocument(`<%
''' <remarks>Old unclosed docs.
Function OldName()
End Function

''' <summary>Text</▮
Function BuildName(first)
End Function
%>`);
    const closingLabels = getVbscriptCompletions(
      parseAspDocument("file:///site/default.asp", closing.text),
      closing.position,
    ).map((item) => item.label);
    expect(closingLabels).toContain("summary");
    expect(closingLabels).not.toContain("remarks");

    const cref = markedDocument(`<%
Function BuildName(first)
End Function
''' <see cref="▮" />
Sub Save()
End Sub
%>`);
    const crefParsed = parseAspDocument("file:///site/default.asp", cref.text);
    const symbols = collectVbscriptSymbols(crefParsed);
    expect(
      getVbscriptCompletions(crefParsed, cref.position, { symbols }).some(
        (item) => item.label === "BuildName",
      ),
    ).toBe(true);
  });

  it("resolves VBScript XML documentation symbol references like normal references", () => {
    const marked = markedDocument(`<%
Function BuildName(first)
End Function
''' <see cref="Build▮Name" />
Sub Save()
End Sub
%>`);
    const parsed = parseAspDocument("file:///site/default.asp", marked.text);
    const symbols = collectVbscriptSymbols(parsed);
    const definition = getVbscriptDefinition(parsed, marked.position, { symbols });
    expect(definition?.name).toBe("BuildName");
    expect(getVbscriptHover(parsed, marked.position, { symbols })).toContain("Function BuildName");
    const references = getVbscriptReferences(parsed, marked.position, {
      symbols,
      documents: [parsed],
    });
    expect(references).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          range: expect.objectContaining({
            start: positionAt(marked.text, marked.text.indexOf("BuildName")),
          }),
        }),
        expect.objectContaining({
          range: expect.objectContaining({
            start: positionAt(marked.text, marked.text.indexOf('BuildName"')),
          }),
        }),
      ]),
    );
  });

  it("limits comment completions to documentation annotations and hovers annotation tags", () => {
    const plainAnnotation = markedDocument(`<%
' @▮
Response.Write "ok"
%>`);
    const plainLabels = getVbscriptCompletions(
      parseAspDocument("file:///site/default.asp", plainAnnotation.text),
      plainAnnotation.position,
    ).map((item) => item.label);
    expect(plainLabels).toEqual(expect.arrayContaining(["@type", "@param", "@returns"]));
    expect(plainLabels).not.toContain("Write");
    expect(plainLabels).not.toContain("Response");
    const plainTypeCompletion = getVbscriptCompletions(
      parseAspDocument("file:///site/default.asp", plainAnnotation.text),
      plainAnnotation.position,
    ).find((item) => item.label === "@type");
    expect(plainTypeCompletion?.detail).toBe("VBScript type annotation");
    expect(String(plainTypeCompletion?.documentation)).toContain("' @type name As Type");

    const docAnnotation = markedDocument(`<%
''' @▮
Function BuildName(first)
End Function
%>`);
    const docLabels = getVbscriptCompletions(
      parseAspDocument("file:///site/default.asp", docAnnotation.text),
      docAnnotation.position,
    ).map((item) => item.label);
    expect(docLabels).toEqual(expect.arrayContaining(["@type", "@param", "@returns"]));
    expect(docLabels).not.toContain("Function");

    const ordinaryComment = markedDocument(`<%
' Response.▮
Response.Write "ok"
%>`);
    expect(
      getVbscriptCompletions(
        parseAspDocument("file:///site/default.asp", ordinaryComment.text),
        ordinaryComment.position,
      ).map((item) => item.label),
    ).not.toContain("Write");

    const hoverSource = `<%
' @type customerId As String
''' @returns BuildName String
Function BuildName()
End Function
%>`;
    const parsed = parseAspDocument("file:///site/default.asp", hoverSource);
    expect(
      getVbscriptHover(parsed, positionAt(hoverSource, hoverSource.indexOf("@type") + 1)),
    ).toContain("' @type name As Type");
    expect(
      getVbscriptHover(parsed, positionAt(hoverSource, hoverSource.indexOf("@returns") + 1)),
    ).toContain("' @returns [procedure] Type");
  });

  it("tracks common VBScript statements and conservative external COM members", () => {
    const parsed = parseAspDocument(
      "file:///site/default.asp",
      `<%
Class Customer
  Public Name
End Class
Dim c
Set c = New Customer
With c
  .
End With
ReDim items(10)
For Each item In items
Next
Dim rs
Set rs = Server.CreateObject("ADODB.Recordset")
rs.
Function BuildName(firstName, lastName)
End Function
Response.Write BuildName("Ada", "Lovelace")
%>`,
    );
    const symbols = collectVbscriptSymbols(parsed);
    const withNode = parsed.cst.children
      .flatMap((node) => node.vbscript?.children ?? [])
      .find((node) => node.kind === "With");
    expect(withNode?.end).toBeLessThan(parsed.text.indexOf("ReDim"));
    expect(symbols.some((symbol) => symbol.name === "items" && symbol.kind === "variable")).toBe(
      true,
    );
    expect(symbols.some((symbol) => symbol.name === "item" && symbol.kind === "variable")).toBe(
      true,
    );
    const withCompletions = getVbscriptCompletions(parsed, { line: 7, character: 3 }, { symbols });
    expect(withCompletions.some((item) => item.label === "Name")).toBe(true);
    const adoCompletions = getVbscriptCompletions(parsed, { line: 14, character: 3 }, { symbols });
    expect(adoCompletions.some((item) => item.label === "MoveNext")).toBe(true);
    expect(getVbscriptSignatureHelp(parsed, { line: 17, character: 33 }, { symbols })).toEqual(
      expect.objectContaining({
        activeParameter: 1,
      }),
    );
  });

  it("builds richer VBScript type information from assignments and annotations", () => {
    const parsed = parseAspDocument(
      "file:///site/default.asp",
      `<%
Class Customer
  Public Name
End Class
' @member Customer.Name As String
' @returns Customer
Function MakeCustomer()
  Set MakeCustomer = New Customer
End Function
' @type rs As ADODB.Recordset
Dim rs
Dim c
Set c = MakeCustomer()
Dim d
Set d = c
%>`,
    );
    const symbols = collectVbscriptSymbols(parsed);
    const env = buildVbTypeEnvironment(parsed, { symbols });
    expect(symbols.find((symbol) => symbol.name === "rs")?.typeName).toBe("ADODB.Recordset");
    expect(symbols.find((symbol) => symbol.name === "MakeCustomer")?.typeName).toBe("Customer");
    expect(symbols.find((symbol) => symbol.name === "d")?.typeName).toBe("Customer");
    expect(
      env.types
        .find((type) => type.name === "Customer")
        ?.members.find((member) => member.name === "Name")?.type?.name,
    ).toBe("String");
  });

  it("uses custom COM type settings for completion and strict diagnostics", () => {
    const parsed = parseAspDocument(
      "file:///site/default.asp",
      `<%
Dim widget
Set widget = Server.CreateObject("Custom.Widget")
widget.Title = 1
widget.Ping("a", "b")
widget.Missing
Dim label
' @type label As String
Set label = "x"
%>`,
    );
    const context = {
      typeChecking: "strict" as const,
      comTypes: {
        "Custom.Widget": {
          members: {
            Title: "String",
            Ping: {
              kind: "method" as const,
              returnType: "Boolean",
              parameters: [{ name: "name", type: "String" }],
            },
          },
        },
      },
    };
    const symbols = collectVbscriptSymbols(parsed, context);
    const completions = getVbscriptCompletions(
      parsed,
      { line: 3, character: 7 },
      {
        ...context,
        symbols,
      },
    );
    expect(completions.some((item) => item.label === "Title")).toBe(true);
    const result = analyzeVbscript(parsed, { ...context, symbols });
    expect(result.diagnostics.some((diagnostic) => diagnostic.message.includes("no member"))).toBe(
      true,
    );
    expect(
      result.diagnostics.some((diagnostic) => diagnostic.message.includes("Argument count")),
    ).toBe(true);
    expect(
      result.diagnostics.some((diagnostic) => diagnostic.message.includes("Set assigns")),
    ).toBe(true);
  });

  it("infers VBScript expression types across operators, arrays and optional parameters", () => {
    const parsed = parseAspDocument(
      "file:///site/default.asp",
      `<%
' @type label As String
Dim label
label = "a" & "b"
' @type count As Number
Dim count
count = (1 + 2) * 3
' @type flags As Boolean
Dim flags
flags = count > 1 And True
' @type items As Array
Dim items
items = Array("a", "b")
' @type sharedName As String
Dim sharedName
Sub Save(Optional ByVal firstName, ByRef lastName, defaultName)
  ' @type sharedName As Number
  Dim sharedName
End Sub
%>`,
    );
    const symbols = collectVbscriptSymbols(parsed);
    expect(symbols.find((symbol) => symbol.name === "label")?.typeName).toBe("String");
    expect(symbols.find((symbol) => symbol.name === "count")?.typeName).toBe("Number");
    expect(symbols.find((symbol) => symbol.name === "flags")?.typeName).toBe("Boolean");
    expect(symbols.find((symbol) => symbol.name === "items")?.typeName).toBe("Array");
    expect(
      symbols.find((symbol) => symbol.name === "sharedName" && !symbol.scopeName)?.typeName,
    ).toBe("String");
    expect(
      symbols.find((symbol) => symbol.name === "sharedName" && symbol.scopeName === "Save")
        ?.typeName,
    ).toBe("Number");
    expect(symbols.find((symbol) => symbol.name === "Save")?.parameters).toEqual([
      "firstName",
      "lastName",
      "defaultName",
    ]);
    expect(symbols.find((symbol) => symbol.name === "Save")?.parameterDetails).toEqual([
      { name: "firstName", mode: "byval", optional: true },
      { name: "lastName", mode: "byref", optional: undefined },
      { name: "defaultName", mode: "byref", optional: undefined },
    ]);
    expect(symbols.find((symbol) => symbol.name === "firstName")).toEqual(
      expect.objectContaining({ parameterMode: "byval", optional: true }),
    );
    expect(symbols.find((symbol) => symbol.name === "defaultName")).toEqual(
      expect.objectContaining({ parameterMode: "byref" }),
    );
    const labelDefinition = getVbscriptDefinition(parsed, { line: 2, character: 4 }, { symbols });
    const labelUse = getVbscriptDefinition(parsed, { line: 3, character: 2 }, { symbols });
    expect(labelUse?.range).toEqual(labelDefinition?.range);
  });

  it("uses TypeName-style VBScript literal and numeric family types", () => {
    const source = `<%
Dim emptyValue, nullValue, objectValue, numericValue, currencyValue
emptyValue = Empty
nullValue = Null
Set objectValue = Nothing
' @type numericValue As Number
numericValue = CCur(1)
' @type currencyValue As Currency
currencyValue = 1
%>`;
    const parsed = parseAspDocument("file:///site/default.asp", source);
    const symbols = collectVbscriptSymbols(parsed);
    expect(symbols.find((symbol) => symbol.name === "emptyValue")?.typeName).toBe("Empty");
    expect(symbols.find((symbol) => symbol.name === "nullValue")?.typeName).toBe("Null");
    expect(symbols.find((symbol) => symbol.name === "objectValue")?.typeName).toBe("Nothing");
    expect(symbols.find((symbol) => symbol.name === "numericValue")?.typeName).toBe("Number");
    expect(symbols.find((symbol) => symbol.name === "currencyValue")?.typeName).toBe("Currency");
    expect(parseVbscriptTypeRef("Integer").object).toBe(false);
    expect(parseVbscriptTypeRef("Decimal").object).toBe(false);
    expect(parseVbscriptTypeRef("Error").object).toBe(false);
    expect(
      analyzeVbscript(parsed, { symbols, typeChecking: "strict" }).diagnostics.some(
        (diagnostic) => diagnostic.source === "asp-lsp-vbscript-type",
      ),
    ).toBe(false);
  });

  it("keeps VBScript object inference after Set Nothing assignments", () => {
    const source = `<%
Class Customer
  Public Name
End Class
Sub Demo()
  Dim a
  Set a = New Customer
  Set a = Nothing
  a.Name
End Sub
%>`;
    const parsed = parseAspDocument("file:///site/default.asp", source);
    const symbols = collectVbscriptSymbols(parsed);
    const variable = symbols.find((symbol) => symbol.name === "a" && symbol.scopeName === "Demo");

    expect(variable?.typeName).toBe("Customer | Nothing");

    expect(
      getVbscriptCompletions(parsed, positionAt(source, source.indexOf("a.Name") + "a.".length), {
        symbols,
      }).some((item) => item.label === "Name"),
    ).toBe(true);
    expect(
      getVbscriptTypeDefinition(parsed, positionAt(source, source.indexOf("a.Name")), {
        symbols,
      })?.name,
    ).toBe("Customer");

    const hints = getVbscriptInlayHints(
      parsed,
      { start: { line: 0, character: 0 }, end: { line: 11, character: 0 } },
      { symbols },
    );
    expect(hints.some((hint) => hint.label === " As Customer | Nothing")).toBe(true);

    const typeCodes = analyzeVbscript(parsed, { symbols, typeChecking: "strict" })
      .diagnostics.filter((diagnostic) => diagnostic.source === "asp-lsp-vbscript-type")
      .map((diagnostic) => diagnostic.code);
    expect(typeCodes).not.toContain("setScalar");
    expect(typeCodes).not.toContain("typeMismatch");
    expect(typeCodes).not.toContain("missingMember");
  });

  it("infers and checks VBScript union types", () => {
    const parsed = parseAspDocument(
      "file:///site/default.asp",
      `<%
Class FirstThing
  Public SharedName
  Public OnlyFirst
End Class
Class SecondThing
  Public SharedName
End Class
x = 1
x = "a"
' @type annotated As Number
Dim annotated
annotated = "oops"
' @type maybeName As String | Number
Dim maybeName
Class Holder
  Public Value
End Class
' @member Holder.Value As String | Number
Function MakeValue(flag)
  If flag Then
    MakeValue = 1
  Else
    MakeValue = "x"
  End If
End Function
Dim both
Set both = New FirstThing
Set both = New SecondThing
both.SharedName
both.OnlyFirst
%>`,
    );
    const symbols = collectVbscriptSymbols(parsed);
    expect(symbols.find((symbol) => symbol.name === "x")?.typeName).toBe("Number | String");
    expect(symbols.find((symbol) => symbol.name === "maybeName")?.typeName).toBe("String | Number");
    expect(symbols.find((symbol) => symbol.name === "annotated")?.typeName).toBe("Number");
    expect(symbols.find((symbol) => symbol.name === "MakeValue")?.typeName).toBe("Number | String");
    expect(symbols.find((symbol) => symbol.name === "both")?.typeName).toBe(
      "FirstThing | SecondThing",
    );

    const env = buildVbTypeEnvironment(parsed, { symbols });
    expect(
      env.types
        .find((type) => type.name === "Holder")
        ?.members.find((member) => member.name === "Value")?.type?.name,
    ).toBe("String | Number");

    const completions = getVbscriptCompletions(
      parsed,
      positionAt(parsed.text, parsed.text.indexOf("both.SharedName") + "both.".length),
      { symbols },
    );
    expect(completions.some((item) => item.label === "SharedName")).toBe(true);
    expect(completions.some((item) => item.label === "OnlyFirst")).toBe(false);

    const hints = getVbscriptInlayHints(
      parsed,
      { start: { line: 0, character: 0 }, end: { line: 32, character: 0 } },
      { symbols },
    );
    expect(JSON.stringify(hints)).toContain("As String | Number");

    const result = analyzeVbscript(parsed, { symbols, typeChecking: "strict" });
    expect(
      result.diagnostics.some((diagnostic) =>
        diagnostic.message.includes("is Number, but assigned String"),
      ),
    ).toBe(true);
    expect(
      result.diagnostics.some((diagnostic) => diagnostic.message.includes("no member 'OnlyFirst'")),
    ).toBe(true);
    expect(
      result.diagnostics.some((diagnostic) =>
        diagnostic.message.includes("no member 'SharedName'"),
      ),
    ).toBe(false);
  });

  it("falls back unknown VBScript types to Variant and marks global variables", () => {
    const source = `<%
Dim rowClass
rowClass = "customer-row"
rowClass = 1
Dim unknownGlobal
Const UnknownConst = MissingValue
Function UnknownReturn()
End Function
Dim sharedValue
Sub Render()
  sharedValue = 1
  Dim localValue
  implicitLocal = 1
End Sub
%>`;
    const parsed = parseAspDocument("file:///site/default.asp", source);
    const sourceRange = {
      start: { line: 0, character: 0 },
      end: positionAt(source, source.length),
    };
    const symbols = collectVbscriptSymbols(parsed);
    expect(symbols.find((symbol) => symbol.name === "rowClass")?.typeName).toBe("String | Number");
    expect(symbols.find((symbol) => symbol.name === "unknownGlobal")?.typeName).toBe("Variant");
    expect(symbols.find((symbol) => symbol.name === "UnknownConst")?.typeName).toBe("Variant");
    expect(symbols.find((symbol) => symbol.name === "UnknownReturn")?.typeName).toBe("Variant");
    expect(
      symbols.find((symbol) => symbol.name === "localValue" && symbol.scopeName === "Render")
        ?.typeName,
    ).toBe("Variant");
    expect(
      symbols.find((symbol) => symbol.name === "implicitLocal" && symbol.scopeName === "Render")
        ?.typeName,
    ).toBe("Number");
    expect(
      symbols.find((symbol) => symbol.name === "sharedValue" && symbol.scopeName === "Render"),
    ).toBeUndefined();
    expect(
      symbols.find((symbol) => symbol.name === "sharedValue" && !symbol.scopeName)?.typeName,
    ).toBe("Number");

    const hints = getVbscriptInlayHints(parsed, sourceRange, { symbols });
    expect(hints.some((hint) => hint.label === " (global) As String | Number")).toBe(true);
    expect(hints.some((hint) => hint.label === " (global) As Variant")).toBe(true);
    const hintsWithoutGlobalMarkers = getVbscriptInlayHints(
      parsed,
      sourceRange,
      { symbols },
      { globalVariableMarkers: "off" },
    );
    expect(JSON.stringify(hintsWithoutGlobalMarkers)).not.toContain("(global)");
    expect(hintsWithoutGlobalMarkers.some((hint) => hint.label === " As String | Number")).toBe(
      true,
    );
    expect(hintsWithoutGlobalMarkers.some((hint) => hint.label === " As Variant")).toBe(true);
    expect(
      hints.some(
        (hint) =>
          hint.label === " As Variant" &&
          hint.position.line === positionAt(source, source.indexOf("localValue")).line,
      ),
    ).toBe(true);
    const hintsWithLocalMarkers = getVbscriptInlayHints(
      parsed,
      sourceRange,
      { symbols },
      { globalVariableMarkers: "all" },
    );
    expect(
      hintsWithLocalMarkers.some(
        (hint) =>
          hint.label === " (local) As Variant" &&
          hint.position.line === positionAt(source, source.indexOf("localValue")).line,
      ),
    ).toBe(true);
    expect(
      hintsWithLocalMarkers.some(
        (hint) =>
          hint.label === " (local) As Number" &&
          hint.position.line === positionAt(source, source.indexOf("implicitLocal")).line,
      ),
    ).toBe(true);
    const localOnlyHints = getVbscriptInlayHints(
      parsed,
      sourceRange,
      { symbols },
      { globalVariableMarkers: "local" },
    );
    expect(JSON.stringify(localOnlyHints)).not.toContain("(global)");
    expect(
      localOnlyHints.some(
        (hint) =>
          hint.label === " (local) As Variant" &&
          hint.position.line === positionAt(source, source.indexOf("localValue")).line,
      ),
    ).toBe(true);
    expect(
      localOnlyHints.some(
        (hint) =>
          hint.label === " (local) As Number" &&
          hint.position.line === positionAt(source, source.indexOf("implicitLocal")).line,
      ),
    ).toBe(true);

    expect(
      getVbscriptHover(parsed, positionAt(source, source.indexOf("unknownGlobal")), { symbols }),
    ).toContain("(global) Dim unknownGlobal As Variant");
    expect(
      getVbscriptHover(parsed, positionAt(source, source.indexOf("UnknownConst")), { symbols }),
    ).toContain("(global) Const UnknownConst As Variant");
    expect(
      getVbscriptHover(parsed, positionAt(source, source.indexOf("sharedValue =")), { symbols }),
    ).toContain("(global) Dim sharedValue As Number");
    expect(
      getVbscriptHover(parsed, positionAt(source, source.indexOf("implicitLocal")), { symbols }),
    ).toContain("(local) Dim implicitLocal As Number");
    expect(
      getVbscriptHover(parsed, positionAt(source, source.indexOf("unknownGlobal")), { symbols }),
    ).not.toContain("VBScript variable");
    expect(
      analyzeVbscript(parsed, { symbols, typeChecking: "strict" }).diagnostics.some(
        (diagnostic) => diagnostic.source === "asp-lsp-vbscript-type",
      ),
    ).toBe(false);
  });

  it("infers VBScript Const declaration types from expressions", () => {
    const source = `<%
Function MakeTitle()
  MakeTitle = "ok"
End Function
Const TextValue = "hello"
Const NumericValue = 10
Const DateValue = #2026-06-01#
Const BooleanValue = True
Const BuiltinValue = adInteger
Const StringBuiltinValue = vbCrLf
Const NumericBuiltinValue = vbOKOnly
Const FunctionValue = MakeTitle()
Const UnknownValue = MissingValue
%>`;
    const parsed = parseAspDocument("file:///site/default.asp", source);
    const symbols = collectVbscriptSymbols(parsed);
    const typeName = (name: string) => symbols.find((symbol) => symbol.name === name)?.typeName;
    expect(typeName("TextValue")).toBe("String");
    expect(typeName("NumericValue")).toBe("Number");
    expect(typeName("DateValue")).toBe("Date");
    expect(typeName("BooleanValue")).toBe("Boolean");
    expect(typeName("BuiltinValue")).toBe("Number");
    expect(typeName("StringBuiltinValue")).toBe("String");
    expect(typeName("NumericBuiltinValue")).toBe("Number");
    expect(typeName("FunctionValue")).toBe("String");
    expect(typeName("UnknownValue")).toBe("Variant");
  });

  it("creates implicit variables without Option Explicit for hover, inlay and semantic tokens", () => {
    const parsed = parseAspDocument(
      "file:///site/default.asp",
      `<%
a = 1
Response.Write a
%>`,
    );
    const symbols = collectVbscriptSymbols(parsed);
    const implicit = symbols.find((symbol) => symbol.name === "a");
    expect(implicit).toEqual(expect.objectContaining({ implicit: true, typeName: "Number" }));
    expect(getVbscriptHover(parsed, { line: 1, character: 0 }, { symbols })).toContain(
      "(global) Dim a As Number",
    );
    expect(getVbscriptHover(parsed, { line: 1, character: 0 }, { symbols })).not.toContain(
      "Implicit VBScript variable",
    );
    expect(
      getVbscriptInlayHints(
        parsed,
        { start: { line: 0, character: 0 }, end: { line: 4, character: 0 } },
        { symbols },
      ).some(
        (hint) =>
          hint.label === " (global) As Number" &&
          hint.paddingLeft === false &&
          hint.paddingRight === true,
      ),
    ).toBe(true);
    expect(
      getVbscriptSemanticTokens(parsed, { symbols }).some(
        (token) => token.range.start.line === 2 && token.tokenType === "variable",
      ),
    ).toBe(true);
    expect(
      getVbscriptSemanticTokens(parsed, { symbols }).some(
        (token) =>
          token.range.start.line === 2 &&
          token.range.start.character === 0 &&
          token.tokenType === "constant" &&
          token.tokenModifiers?.includes("readonly") &&
          token.tokenModifiers?.includes("library"),
      ),
    ).toBe(true);
  });

  it("creates implicit variables from single-line If assignments for hover and inlay hints", () => {
    const source = `<%
If enabled Then oneLineValue = 1
If enabled Then branchValue = 2 Else fallbackValue = "fallback"
If enabled Then Let letValue = 3
If enabled Then _
  continuedValue = 4
Sub Render()
  If enabled Then localValue = "local"
End Sub
%>`;
    const parsed = parseAspDocument("file:///site/single-line-if-implicit.asp", source);
    const symbols = collectVbscriptSymbols(parsed);

    expect(symbols.find((symbol) => symbol.name === "oneLineValue")).toMatchObject({
      implicit: true,
      typeName: "Number",
    });
    expect(symbols.find((symbol) => symbol.name === "branchValue")).toMatchObject({
      implicit: true,
      typeName: "Number",
    });
    expect(symbols.find((symbol) => symbol.name === "fallbackValue")).toMatchObject({
      implicit: true,
      typeName: "String",
    });
    expect(symbols.find((symbol) => symbol.name === "letValue")).toMatchObject({
      implicit: true,
      typeName: "Number",
    });
    expect(symbols.find((symbol) => symbol.name === "continuedValue")).toMatchObject({
      implicit: true,
      typeName: "Number",
    });
    expect(symbols.find((symbol) => symbol.name === "localValue")).toMatchObject({
      implicit: true,
      scopeName: "Render",
      typeName: "String",
    });
    expect(
      getVbscriptHover(parsed, positionAt(source, source.indexOf("oneLineValue")), { symbols }),
    ).toContain("(global) Dim oneLineValue As Number");
    expect(
      getVbscriptHover(parsed, positionAt(source, source.indexOf("localValue")), { symbols }),
    ).toContain("(local) Dim localValue As String");
    expect(
      getVbscriptInlayHints(
        parsed,
        { start: { line: 0, character: 0 }, end: positionAt(source, source.length) },
        { symbols },
        { globalVariableMarkers: "all" },
      ).map((hint) => hint.label),
    ).toEqual(expect.arrayContaining([" (global) As Number", " (local) As String"]));
  });

  it("uses uncertain markers only before include-aware implicit variable analysis is available", () => {
    const includeSource = `<%
a = 1
sharedTitle = "include"
Sub Render()
  b = "local"
End Sub
%>`;
    const includeParsed = parseAspDocument("file:///site/shared.inc", includeSource);
    const includeSymbols = collectVbscriptSymbols(includeParsed);
    const includeHints = getVbscriptInlayHints(
      includeParsed,
      { start: { line: 0, character: 0 }, end: positionAt(includeSource, includeSource.length) },
      { symbols: includeSymbols },
      { globalVariableMarkers: "all" },
    );
    expect(includeHints.filter((hint) => hint.label === " (global) As Number")).toHaveLength(1);
    expect(includeHints.filter((hint) => hint.label === " (global) As String")).toHaveLength(1);
    expect(includeHints.filter((hint) => hint.label === " (local) As String")).toHaveLength(1);
    expect(JSON.stringify(includeHints)).not.toContain("(?)");
    expect(
      getVbscriptHover(includeParsed, positionAt(includeSource, includeSource.indexOf("a =")), {
        symbols: includeSymbols,
      }),
    ).toContain("(global) Dim a As Number");
    expect(
      getVbscriptHover(includeParsed, positionAt(includeSource, includeSource.indexOf("b =")), {
        symbols: includeSymbols,
      }),
    ).toContain("(local) Dim b As String");

    const pageSource = `<!-- #include file="shared.inc" -->
<%
Response.Write sharedTitle
a = 1
Sub Render()
  b = "page local"
End Sub
Response.Write b
%>`;
    const pageParsed = parseAspDocument("file:///site/default.asp", pageSource);
    const pageSymbols = collectVbscriptSymbols(pageParsed);
    const pageHints = getVbscriptInlayHints(
      pageParsed,
      { start: { line: 0, character: 0 }, end: positionAt(pageSource, pageSource.length) },
      { symbols: pageSymbols },
      { globalVariableMarkers: "all" },
    );
    expect(pageHints.some((hint) => hint.label === " (?) As Number")).toBe(true);
    expect(pageHints.some((hint) => hint.label === " (local) As String")).toBe(true);
    expect(JSON.stringify(pageHints)).not.toContain("(global) As Number");

    const includeAwarePageHints = getVbscriptInlayHints(
      pageParsed,
      { start: { line: 0, character: 0 }, end: positionAt(pageSource, pageSource.length) },
      { symbols: [...pageSymbols, ...includeSymbols], documents: [pageParsed, includeParsed] },
      { globalVariableMarkers: "all" },
    );
    expect(JSON.stringify(includeAwarePageHints)).not.toContain("(global) As Number");
    expect(includeAwarePageHints.some((hint) => hint.label === " (local) As String")).toBe(true);
    expect(JSON.stringify(includeAwarePageHints)).not.toContain("(?)");
    expect(
      getVbscriptHover(pageParsed, positionAt(pageSource, pageSource.indexOf("sharedTitle")), {
        symbols: [...pageSymbols, ...includeSymbols],
        documents: [pageParsed, includeParsed],
      }),
    ).toContain("(global) Dim sharedTitle As String");
    expect(
      getVbscriptHover(pageParsed, positionAt(pageSource, pageSource.lastIndexOf("b")), {
        symbols: [...pageSymbols, ...includeSymbols],
        documents: [pageParsed, includeParsed],
      }),
    ).toBeUndefined();

    const disabled = getVbscriptInlayHints(
      includeParsed,
      { start: { line: 0, character: 0 }, end: positionAt(includeSource, includeSource.length) },
      { symbols: includeSymbols },
      { globalVariableMarkers: "off" },
    );
    expect(JSON.stringify(disabled)).not.toContain("(?)");
  });

  it("resolves assignments after includes to include-defined implicit globals", () => {
    const includeSource = `<%
sharedTitle = "include"
%>`;
    const includeParsed = parseAspDocument("file:///site/shared.inc", includeSource);
    const includeSymbols = collectVbscriptSymbols(includeParsed);
    const pageSource = `<!-- #include file="shared.inc" -->
<%
sharedTitle = "page"
%>`;
    const pageParsed = parseAspDocument("file:///site/default.asp", pageSource);
    const pageSymbols = collectVbscriptSymbols(pageParsed);
    const context = {
      symbols: [...pageSymbols, ...includeSymbols],
      documents: [pageParsed, includeParsed],
    };

    expect(
      getVbscriptDefinition(
        pageParsed,
        positionAt(pageSource, pageSource.indexOf("sharedTitle =")),
        context,
      )?.sourceUri,
    ).toBe(includeParsed.uri);
    expect(
      getVbscriptInlayHints(
        pageParsed,
        { start: { line: 0, character: 0 }, end: positionAt(pageSource, pageSource.length) },
        context,
        { globalVariableMarkers: "all" },
      ).some((hint) => hint.label === " (global) As String"),
    ).toBe(false);

    const beforeIncludeSource = `<%
sharedTitle = "page"
%>
<!-- #include file="shared.inc" -->`;
    const beforeIncludeParsed = parseAspDocument("file:///site/before.asp", beforeIncludeSource);
    const beforeIncludeSymbols = collectVbscriptSymbols(beforeIncludeParsed);
    expect(
      getVbscriptDefinition(
        beforeIncludeParsed,
        positionAt(beforeIncludeSource, beforeIncludeSource.indexOf("sharedTitle =")),
        {
          symbols: [...beforeIncludeSymbols, ...includeSymbols],
          documents: [beforeIncludeParsed, includeParsed],
        },
      )?.sourceUri,
    ).toBe(beforeIncludeParsed.uri);
    expect(
      getVbscriptInlayHints(
        beforeIncludeParsed,
        {
          start: { line: 0, character: 0 },
          end: positionAt(beforeIncludeSource, beforeIncludeSource.length),
        },
        {
          symbols: [...beforeIncludeSymbols, ...includeSymbols],
          documents: [beforeIncludeParsed, includeParsed],
        },
        { globalVariableMarkers: "all" },
      ).some((hint) => hint.label === " (global) As String"),
    ).toBe(true);
  });

  it("resolves procedure assignments after includes to include-defined implicit globals", () => {
    const includeSource = `<%
sharedTitle = "include"
%>`;
    const includeParsed = parseAspDocument("file:///site/shared.inc", includeSource);
    const includeSymbols = collectVbscriptSymbols(includeParsed);
    const pageSource = `<!-- #include file="shared.inc" -->
<%
Function Render()
  sharedTitle = "function"
End Function
Class Widget
  Public Sub Save()
    sharedTitle = "method"
  End Sub
End Class
%>`;
    const pageParsed = parseAspDocument("file:///site/default.asp", pageSource);
    const pageSymbols = collectVbscriptSymbols(pageParsed);
    const context = {
      symbols: [...pageSymbols, ...includeSymbols],
      documents: [pageParsed, includeParsed],
    };

    expect(
      getVbscriptDefinition(
        pageParsed,
        positionAt(pageSource, pageSource.indexOf("sharedTitle =")),
        context,
      )?.sourceUri,
    ).toBe(includeParsed.uri);
    expect(
      getVbscriptDefinition(
        pageParsed,
        positionAt(pageSource, pageSource.lastIndexOf("sharedTitle =")),
        context,
      )?.sourceUri,
    ).toBe(includeParsed.uri);

    const hints = getVbscriptInlayHints(
      pageParsed,
      { start: { line: 0, character: 0 }, end: positionAt(pageSource, pageSource.length) },
      context,
      { globalVariableMarkers: "all" },
    );
    expect(JSON.stringify(hints)).not.toContain("(local) As String");
    expect(JSON.stringify(hints)).not.toContain("(global) As String");
  });

  it("excludes include-defined implicit global declarations from reference counts", () => {
    const includeSource = `<%
sharedTitle = "include"
%>`;
    const includeParsed = parseAspDocument("file:///site/shared.inc", includeSource);
    const includeSymbols = collectVbscriptSymbols(includeParsed);
    const pageSource = `<!-- #include file="shared.inc" -->
<%
Response.Write sharedTitle
sharedTitle = "page"
Function Render()
  sharedTitle = "function"
End Function
%>`;
    const pageParsed = parseAspDocument("file:///site/default.asp", pageSource);
    const pageSymbols = collectVbscriptSymbols(pageParsed);
    const sharedTitle = includeSymbols.find(
      (symbol) => symbol.name === "sharedTitle" && symbol.implicit,
    );
    if (!sharedTitle) {
      throw new Error("missing implicit include symbol");
    }

    const references = getVbscriptReferencesForSymbol(
      sharedTitle,
      {
        symbols: [...pageSymbols, ...includeSymbols],
        documents: [pageParsed, includeParsed],
      },
      { includeDeclaration: false, includeFunctionReturnAssignments: false },
    );

    expect(references).toHaveLength(3);
    expect(references.map((reference) => reference.uri)).toEqual([
      pageParsed.uri,
      pageParsed.uri,
      pageParsed.uri,
    ]);
  });

  it("adds semantic token types and modifiers for VBScript symbols", () => {
    const source = `<%
Class Customer
  Public Name
  Public Property Get DisplayName()
    DisplayName = Name
  End Property
End Class
Const MaxCount = 10
Sub Render(ByVal metricMap, output)
  Response.Write MaxCount + vbCrLf
  Set output = Nothing
End Sub
%>`;
    const parsed = parseAspDocument("file:///site/default.asp", source);
    const symbols = collectVbscriptSymbols(parsed);
    const tokens = getVbscriptSemanticTokens(parsed, { symbols });
    const tokenAt = (text: string) => {
      const position = positionAt(source, source.indexOf(text));
      return tokens.find(
        (token) =>
          token.range.start.line === position.line &&
          token.range.start.character === position.character,
      );
    };
    expect(tokenAt("metricMap")).toEqual(
      expect.objectContaining({ tokenType: "parameter", tokenModifiers: ["byval"] }),
    );
    expect(tokenAt("output")).toEqual(
      expect.objectContaining({ tokenType: "parameter", tokenModifiers: ["byref"] }),
    );
    expect(tokenAt("MaxCount")).toEqual(
      expect.objectContaining({ tokenType: "constant", tokenModifiers: ["readonly"] }),
    );
    expect(tokenAt("Name")).toEqual(
      expect.objectContaining({ tokenType: "property", tokenModifiers: ["public"] }),
    );
    expect(tokenAt("DisplayName")).toEqual(
      expect.objectContaining({ tokenType: "property", tokenModifiers: ["public"] }),
    );
    expect(tokenAt("Response")).toEqual(
      expect.objectContaining({ tokenType: "constant", tokenModifiers: ["readonly", "library"] }),
    );
    expect(tokenAt("Write")).toEqual(
      expect.objectContaining({ tokenType: "method", tokenModifiers: ["library"] }),
    );
    expect(tokenAt("vbCrLf")).toEqual(
      expect.objectContaining({ tokenType: "constant", tokenModifiers: ["readonly", "library"] }),
    );
    expect(tokenAt("Nothing")).toEqual(
      expect.objectContaining({ tokenType: "constant", tokenModifiers: ["readonly", "library"] }),
    );
    expect(tokenAt("+")).toEqual(expect.objectContaining({ tokenType: "operator" }));
  });

  it("returns VBScript semantic tokens only inside the requested range", () => {
    const source = `<%
Dim beforeValue
Dim targetValue
targetValue = beforeValue + 1
Dim afterValue
%>`;
    const parsed = parseAspDocument("file:///site/default.asp", source);
    const symbols = collectVbscriptSymbols(parsed);
    const tokens = getVbscriptSemanticTokens(
      parsed,
      { symbols },
      {
        start: { line: 3, character: 0 },
        end: { line: 3, character: 32 },
      },
    );
    expect(tokens.length).toBeGreaterThan(0);
    expect(tokens.every((token) => token.range.start.line === 3)).toBe(true);
    expect(
      tokens.some((token) => token.range.start.character === 14 && token.tokenType === "variable"),
    ).toBe(true);
    expect(tokens.some((token) => token.tokenType === "operator")).toBe(true);
  });

  it("keeps undeclared assignments undeclared under Option Explicit", () => {
    const parsed = parseAspDocument(
      "file:///site/default.asp",
      `<%
Option Explicit
a = 1
%>`,
    );
    const result = analyzeVbscript(parsed);
    expect(result.symbols.some((symbol) => symbol.name === "a" && symbol.implicit)).toBe(false);
    expect(
      result.diagnostics.some((diagnostic) => diagnostic.message.includes("not declared")),
    ).toBe(true);
  });

  it("builds LSP helper data for resolve, selection, inlay hints and type definitions", () => {
    const source = `<%
Class Customer
  Public Name
End Class
Function BuildName(firstName)
  Dim c
  Set c = New Customer
  BuildName = c.Name
End Function
Response.Write BuildName("Ada")
%>`;
    const parsed = parseAspDocument("file:///site/default.asp", source);
    const symbols = collectVbscriptSymbols(parsed);
    const typeDefinition = getVbscriptTypeDefinition(
      parsed,
      positionAt(source, source.indexOf("c.Name")),
      { symbols },
    );
    expect(typeDefinition?.name).toBe("Customer");

    const hints = getVbscriptInlayHints(
      parsed,
      { start: { line: 0, character: 0 }, end: { line: 10, character: 0 } },
      { symbols },
    );
    expect(JSON.stringify(hints)).toContain("As Customer");
    expect(JSON.stringify(hints)).toContain("firstName:");
    expect(hints.filter((hint) => hint.label === "firstName:")).toEqual([
      expect.objectContaining({
        position: positionAt(source, source.indexOf('"Ada"')),
      }),
    ]);
    const customerSymbol = symbols.find((symbol) => symbol.name === "c");
    const customerHint = hints.find((hint) => hint.label === " As Customer");
    expect(customerHint).toEqual(
      expect.objectContaining({ position: customerSymbol?.range.end, paddingLeft: false }),
    );
    expect(customerHint?.paddingRight).toBe(true);
    const firstNameOffset = source.indexOf("firstName");
    expect(
      hints.some(
        (hint) =>
          hint.label === " As Variant" &&
          hint.position.line === positionAt(source, firstNameOffset).line &&
          hint.position.character ===
            positionAt(source, firstNameOffset + "firstName".length).character,
      ),
    ).toBe(false);

    const returnHint = getVbscriptInlayHints(
      parseAspDocument(
        "file:///site/returns.asp",
        `<%
' @returns String
Function BuildHtml()
End Function
%>`,
      ),
      { start: { line: 0, character: 0 }, end: { line: 5, character: 0 } },
    ).find((hint) => hint.label === " As String");
    expect(returnHint).toEqual(
      expect.objectContaining({ label: " As String", paddingLeft: false, paddingRight: true }),
    );
    expect(returnHint?.position).toEqual({ line: 2, character: "Function BuildHtml()".length });

    const implicitByRefSource = `<%
Function BuildDisplayName(first, ByVal last, ByRef explicitRef)
End Function
Response.Write BuildDisplayName("Ada", "Lovelace", value)
%>`;
    const implicitByRefParsed = parseAspDocument(
      "file:///site/implicit-byref.asp",
      implicitByRefSource,
    );
    const implicitByRefHints = getVbscriptInlayHints(
      implicitByRefParsed,
      { start: { line: 0, character: 0 }, end: { line: 5, character: 0 } },
      { symbols: collectVbscriptSymbols(implicitByRefParsed) },
    );
    expect(implicitByRefHints.filter((hint) => hint.label === "ByRef ")).toEqual([
      expect.objectContaining({
        position: positionAt(implicitByRefSource, implicitByRefSource.indexOf("first")),
        paddingRight: false,
      }),
    ]);
    const disabledByRefHints = getVbscriptInlayHints(
      implicitByRefParsed,
      { start: { line: 0, character: 0 }, end: { line: 5, character: 0 } },
      { symbols: collectVbscriptSymbols(implicitByRefParsed) },
      { implicitByRef: false },
    );
    expect(JSON.stringify(disabledByRefHints)).not.toContain("ByRef");
    expect(JSON.stringify(disabledByRefHints)).toContain("first:");

    const declarationParameterSource = `<%
Public Function RenderCustomerRows(ByVal customerList, activeCustomerId)
End Function
Private Sub SaveCustomer(customer)
End Sub
Property Get SelectedCustomer(activeCustomerId)
End Property
Property Let SelectedCustomer(activeCustomerId, value)
End Property
Property Set CurrentCustomer(value)
End Property
Response.Write RenderCustomerRows(customers, activeId)
Call SaveCustomer(currentCustomer)
Response.Write SelectedCustomer(activeId)
SelectedCustomer(activeId) = currentCustomer
%>`;
    const declarationParameterParsed = parseAspDocument(
      "file:///site/declaration-parameters.asp",
      declarationParameterSource,
    );
    const declarationParameterHints = getVbscriptInlayHints(
      declarationParameterParsed,
      { start: { line: 0, character: 0 }, end: { line: 16, character: 0 } },
      { symbols: collectVbscriptSymbols(declarationParameterParsed) },
    );
    const labelPositions = (label: string) =>
      declarationParameterHints.filter((hint) => hint.label === label).map((hint) => hint.position);
    expect(labelPositions("customerList:").length).toBe(1);
    expect(labelPositions("activeCustomerId:").length).toBe(1);
    expect(labelPositions("customer:").length).toBe(1);
    expect(
      declarationParameterHints
        .filter((hint) => typeof hint.label === "string" && hint.label.endsWith(":"))
        .some((hint) => hint.position.line >= 1 && hint.position.line <= 9),
    ).toBe(false);
    expect(
      declarationParameterHints.filter((hint) => hint.label === "ByRef ").length,
    ).toBeGreaterThan(0);

    const selection = getVbscriptSelectionRanges(parsed, [
      positionAt(source, source.indexOf("BuildName =") + 2),
    ])[0];
    expect(selection.parent).toBeTruthy();

    const resolved = resolveVbscriptCompletionItem({ label: "BuildName" }, parsed, {
      symbols,
      sourceUriFormatter: (uri) => `[default.asp](${uri})`,
    });
    expect(String(resolved.documentation)).toContain(
      "Defined in [default.asp](file:///site/default.asp)",
    );
  });

  it("builds VBScript call hierarchy items from user-defined procedures", () => {
    const source = `<%
Sub Save()
  BuildName("Ada")
End Sub
Function BuildName(firstName)
  BuildName = firstName
End Function
%>`;
    const parsed = parseAspDocument("file:///site/default.asp", source);
    const symbols = collectVbscriptSymbols(parsed);
    const items = prepareVbscriptCallHierarchy(
      parsed,
      positionAt(source, source.indexOf("BuildName(firstName)") + 2),
      { symbols },
    );
    expect(items[0]?.name).toBe("BuildName");
  });
});

describe("ASP formatting", () => {
  it("formats full documents without erasing ASP delimiters", () => {
    const parsed = parseAspDocument(
      "file:///site/default.asp",
      `<html>
<body>
<% Option Explicit
If enabled Then
Response.Write "ok"
End If
%>
<%= title %>
</body>
</html>`,
    );
    const edits = formatAspDocument(parsed, { tabSize: 2, insertSpaces: true });
    expect(edits).toHaveLength(1);
    expect(edits[0].newText).toContain("<%");
    expect(edits[0].newText).toContain("%>");
    expect(edits[0].newText).toContain("  Response.Write");
    expect(edits[0].newText).toContain("<%= title %>");
  });

  it("formats ASP ranges on CST node boundaries", () => {
    const parsed = parseAspDocument(
      "file:///site/default.asp",
      `<div></div>
<%
If enabled Then
Response.Write "ok"
End If
%>`,
    );
    const edits = formatAspRange(
      parsed,
      {
        start: { line: 1, character: 0 },
        end: { line: 5, character: 2 },
      },
      { tabSize: 2, insertSpaces: true },
    );
    expect(edits[0].newText).toContain("  Response.Write");
    expect(edits[0].newText).not.toContain("<div>");
  });

  it("does not duplicate nested ASP expressions inside non-server regions", () => {
    const parsed = parseAspDocument(
      "file:///site/default.asp",
      `<style>.x { color: <%= themeColor %>; }</style>
<%value=1%>`,
    );
    const edits = formatAspDocument(parsed, { tabSize: 2, insertSpaces: true });
    const formatted = edits[0].newText;
    expect(formatted.match(/themeColor/g)).toHaveLength(1);
    expect(formatted).toContain("<% value = 1 %>");
  });

  it("preserves VBScript strings and comments while formatting operators", () => {
    const parsed = parseAspDocument(
      "file:///site/default.asp",
      `<%
Response.Write "a=b"
' keep x=y
value=1
%>
<%= "x=y" %>`,
    );
    const edits = formatAspDocument(parsed, { tabSize: 2, insertSpaces: true });
    const formatted = edits[0].newText;
    expect(formatted).toContain(`Response.Write "a=b"`);
    expect(formatted).toContain(`' keep x=y`);
    expect(formatted).toContain(`value = 1`);
    expect(formatted).toContain(`<%= "x=y" %>`);
  });

  it("preserves string whitespace inside single-line ASP blocks", () => {
    const parsed = parseAspDocument("file:///site/default.asp", `<%Response.Write "a   b"%>`);
    const edits = formatAspDocument(parsed, { tabSize: 2, insertSpaces: true });
    expect(edits[0]?.newText).toBe(`<% Response.Write "a   b" %>`);
  });

  it("leaves server-side JScript regions unchanged", () => {
    const source = `<%@ LANGUAGE="JScript" %>
<%
var s = "a=b";
%>`;
    const parsed = parseAspDocument("file:///site/default.asp", source);
    const edits = formatAspDocument(parsed, { tabSize: 2, insertSpaces: true });
    expect(edits[0]?.newText ?? source).toBe(source);
  });

  it("aligns simple VBScript assignments when requested", () => {
    const parsed = parseAspDocument(
      "file:///site/default.asp",
      `<%
Dim first
Dim longerName
first=1
longerName=2
%>`,
    );
    const edits = formatAspDocument(parsed, {
      tabSize: 2,
      insertSpaces: true,
      alignAssignments: true,
    });
    expect(edits[0].newText).toContain("first      = 1\nlongerName = 2");
  });

  it("indents VBScript line continuations one level deeper", () => {
    const parsed = parseAspDocument(
      "file:///site/default.asp",
      `<%
a = _
"aaa" & _
"bbb"
%>`,
    );
    const edits = formatAspDocument(parsed, { tabSize: 4, insertSpaces: true });
    expect(edits[0].newText).toContain(`a = _
    "aaa" & _
    "bbb"`);
  });

  it("formats indented ASP blocks relative to their tag indentation", () => {
    const parsed = parseAspDocument(
      "file:///site/default.asp",
      `<div>
    <%
If enabled Then
Response.Write "ok"
End If
    %>
</div>`,
    );
    const edits = formatAspDocument(parsed, { tabSize: 2, insertSpaces: true });
    expect(edits[0].newText).toContain(`    <%
    If enabled Then
      Response.Write "ok"
    End If
    %>`);
  });

  it("can ignore tag indentation when formatting ASP blocks", () => {
    const parsed = parseAspDocument(
      "file:///site/default.asp",
      `<div>
    <%
If enabled Then
Response.Write "ok"
End If
    %>
</div>`,
    );
    const edits = formatAspDocument(parsed, {
      tabSize: 2,
      insertSpaces: true,
      ignoreVbscriptTagIndent: true,
    });
    expect(edits[0].newText).toContain(`    <%
If enabled Then
  Response.Write "ok"
End If
%>`);
  });

  it("formats server-side VBScript tags relative to their tag indentation", () => {
    const parsed = parseAspDocument(
      "file:///site/default.asp",
      `<div>
  <script runat="server">
If enabled Then
Response.Write "ok"
End If
  </script>
</div>`,
    );
    const edits = formatAspDocument(parsed, { tabSize: 2, insertSpaces: true });
    expect(edits[0].newText).toContain(`  <script runat="server">
    If enabled Then
      Response.Write "ok"
    End If
  </script>`);
  });

  it("formats Select Case blocks with case bodies indented", () => {
    const parsed = parseAspDocument(
      "file:///site/default.asp",
      `<%
Select Case kind
Case "a"
Response.Write "a"
Case Else
Response.Write "else"
End Select
%>`,
    );
    const edits = formatAspDocument(parsed, { tabSize: 2, insertSpaces: true });
    expect(edits[0].newText).toContain(`Select Case kind
  Case "a"
    Response.Write "a"
  Case Else
    Response.Write "else"
End Select`);
  });

  it("toggles line comments by embedded Classic ASP region", () => {
    const source = `<div>hello</div>
<script>
  const value = 1;
</script>
<style>
  .x { color: red; }
</style>
<%
  Response.Write value
%>`;
    const selections = [
      rangeAt(source, "hello"),
      rangeAt(source, "const value"),
      rangeAt(source, ".x {"),
      rangeAt(source, "Response.Write"),
    ];
    const commented = applyTextEdits(
      source,
      getClassicAspLineCommentEdits("file:///site/default.asp", source, selections),
    );
    expect(commented).toContain("<!-- <div>hello</div> -->");
    expect(commented).toContain("  // const value = 1;");
    expect(commented).toContain("  /* .x { color: red; } */");
    expect(commented).toContain("  ' Response.Write value");

    const uncommented = applyTextEdits(
      commented,
      getClassicAspLineCommentEdits("file:///site/default.asp", commented, selections),
    );
    expect(uncommented).toBe(source);
  });

  it("toggles whole selected non-empty lines once", () => {
    const source = `<script>
  const first = 1;

  const second = 2;
</script>`;
    const commented = applyTextEdits(
      source,
      getClassicAspLineCommentEdits("file:///site/default.asp", source, [
        {
          start: positionAt(source, source.indexOf("const first")),
          end: positionAt(source, source.indexOf("</script>")),
        },
      ]),
    );
    expect(commented).toContain("  // const first = 1;");
    expect(commented).toContain("\n\n");
    expect(commented).toContain("  // const second = 2;");
  });
});

function positionAt(text: string, offset: number): { line: number; character: number } {
  let line = 0;
  let character = 0;
  for (let index = 0; index < offset; index += 1) {
    if (text[index] === "\n") {
      line += 1;
      character = 0;
    } else {
      character += 1;
    }
  }
  return { line, character };
}

function offsetAt(text: string, position: { line: number; character: number }): number {
  let line = 0;
  let character = 0;
  for (let index = 0; index < text.length; index += 1) {
    if (line === position.line && character === position.character) {
      return index;
    }
    if (text[index] === "\n") {
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

function rangeAt(text: string, needle: string) {
  const offset = text.indexOf(needle);
  if (offset === -1) {
    throw new Error(`Missing text: ${needle}`);
  }
  const position = positionAt(text, offset);
  return { start: position, end: position };
}

function applyTextEdits(
  text: string,
  edits: Array<{
    range: { start: { line: number; character: number }; end: { line: number; character: number } };
    newText: string;
  }>,
): string {
  return [...edits]
    .sort((left, right) => offsetAt(text, right.range.start) - offsetAt(text, left.range.start))
    .reduce((current, edit) => {
      const start = offsetAt(current, edit.range.start);
      const end = offsetAt(current, edit.range.end);
      return current.slice(0, start) + edit.newText + current.slice(end);
    }, text);
}

function markedDocument(source: string): {
  text: string;
  position: { line: number; character: number };
} {
  const offset = source.indexOf("▮");
  if (offset === -1) {
    throw new Error("Marked source is missing a cursor marker.");
  }
  const text = source.slice(0, offset) + source.slice(offset + "▮".length);
  return { text, position: positionAt(text, offset) };
}

function flattenVbNodes(node: VbCstNode): VbCstNode[] {
  return [node, ...node.children.flatMap(flattenVbNodes)];
}
