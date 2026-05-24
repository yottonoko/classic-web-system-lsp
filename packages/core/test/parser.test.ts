import { describe, expect, it } from "vitest";
import {
  analyzeVbscript,
  buildVbTypeEnvironment,
  buildVirtualDocuments,
  collectVbscriptSymbols,
  formatAspDocument,
  formatAspRange,
  getClassicAspLineCommentEdits,
  getVbscriptCompletions,
  getVbscriptDefinition,
  getVbscriptHover,
  getVbscriptInlayHints,
  getVbscriptReferences,
  getVbscriptSelectionRanges,
  getVbscriptSemanticTokens,
  getVbscriptSignatureHelp,
  getVbscriptTypeDefinition,
  parseAspCst,
  parseAspDocument,
  parseVbscriptCst,
  prepareVbscriptCallHierarchy,
  resolveVbscriptCompletionItem,
  updateAspParsedDocument,
} from "../src";
import type { VbCstNode } from "../src";

describe("parseAspDocument", () => {
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

  it("builds virtual documents with source maps", () => {
    const source = `<div><%= title %></div><style>.x { color: red }</style>`;
    const parsed = parseAspDocument("file:///site/default.asp", source);
    const docs = buildVirtualDocuments(parsed);
    expect(docs.get("html")?.text).toContain("<div>");
    expect(docs.get("html")?.text).not.toContain("title");
    expect(docs.get("css")?.text).toContain("color");
  });

  it("updates ASP documents incrementally for safe region content edits", () => {
    const source = `<html>
<div>Hello</div>
<style>.x { color: red; }</style>
<script>const answer = 42;</script>
<% Option Explicit
Dim name
Response.Write name
%>
</html>`;
    const cases = [
      { needle: "Hello", insert: " there", label: "html text" },
      { needle: "Dim name", insert: "\nDim title", label: "vbscript" },
      { needle: "color: red", insert: "; background: white", label: "css" },
      { needle: "answer = 42", insert: " + 1", label: "javascript" },
    ];
    for (const testCase of cases) {
      const previous = parseAspDocument("file:///site/incremental.asp", source);
      const start = source.indexOf(testCase.needle) + testCase.needle.length;
      const nextText = source.slice(0, start) + testCase.insert + source.slice(start);
      const result = updateAspParsedDocument(previous, nextText, [
        {
          range: { start: positionAt(source, start), end: positionAt(source, start) },
          text: testCase.insert,
        },
      ]);
      const full = parseAspDocument("file:///site/incremental.asp", nextText);
      expect(result.incremental, testCase.label).toBe(true);
      expect(parsedShape(result.parsed), testCase.label).toEqual(parsedShape(full));
    }
  });

  it("falls back to full ASP parsing when structure may change", () => {
    const source = `<html>
<!-- #include file="inc/common.inc" -->
<script>const answer = 42;</script>
<% Response.Write answer %>
</html>`;
    const cases = [
      { needle: "answer %>", insert: "%>", label: "asp close delimiter" },
      { needle: "const answer", insert: "</script>", label: "script close tag" },
      { needle: "inc/common.inc", insert: "/shared", label: "include path" },
      { needle: "</script>\n<%", insert: "<style>", label: "cross region" },
    ];
    for (const testCase of cases) {
      const previous = parseAspDocument("file:///site/fallback.asp", source);
      const start = source.indexOf(testCase.needle);
      const end = start + testCase.needle.length;
      const nextText = source.slice(0, start) + testCase.insert + source.slice(end);
      const result = updateAspParsedDocument(previous, nextText, [
        {
          range: { start: positionAt(source, start), end: positionAt(source, end) },
          text: testCase.insert,
        },
      ]);
      expect(result.incremental, testCase.label).toBe(false);
      expect(parsedShape(result.parsed), testCase.label).toEqual(
        parsedShape(parseAspDocument("file:///site/fallback.asp", nextText)),
      );
    }
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
    expect(css).toContain("__asp_lsp__{color: x");
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
    expect(docs.get("css")?.text).toContain("__asp_lsp__{color: red; display: block}");
  });

  it("reports missing ASP close delimiter", () => {
    const parsed = parseAspDocument("file:///broken.asp", "<html><% Response.Write 1");
    expect(parsed.diagnostics[0]?.message).toContain("closing %>");
  });

  it("localizes ASP parser diagnostics", () => {
    const parsed = parseAspDocument("file:///broken.asp", "<html><% Response.Write 1", {
      resolvedLocale: "ja",
    });
    expect(parsed.diagnostics[0]?.message).toContain("閉じ区切り");
  });

  it("keeps ASP delimiters inside script strings and comments from ending regions", () => {
    const source = `<%
Response.Write "%>"
' comment with %>
Response.Write "done"
%>
<%@ LANGUAGE="JScript" %><% var text = '%>'; Response.Write(text); %>`;
    const parsed = parseAspDocument("file:///site/delimiters.asp", source);
    expect(parsed.diagnostics).toHaveLength(0);
    const blocks = parsed.regions.filter(
      (region) => region.kind === "asp-block" || region.kind === "asp-directive",
    );
    expect(blocks).toHaveLength(3);
    expect(source.slice(blocks[0].contentStart, blocks[0].contentEnd)).toContain(
      'Response.Write "done"',
    );
  });

  it("keeps script and style closing tags inside strings or comments from ending regions", () => {
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
    expect(script && source.slice(script.contentStart, script.contentEnd)).toContain(
      "const ok = true;",
    );
    expect(style && source.slice(style.contentStart, style.contentEnd)).toContain("color: red");
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
    expect(
      getVbscriptCompletions(parsed, { line: 1, character: 4 }).some(
        (item) => item.label === "customerName",
      ),
    ).toBe(true);
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

  it("reports unused VBScript declarations as hints", () => {
    const parsed = parseAspDocument(
      "file:///site/default.asp",
      `<%
Dim unusedValue
Const unusedConst = 1
Sub Save(usedArg, unusedArg)
  Response.Write usedArg
End Sub
Class Lonely
End Class
%>`,
    );
    const diagnostics = analyzeVbscript(parsed).diagnostics;
    expect(diagnostics.some((diagnostic) => diagnostic.message.includes("unusedValue"))).toBe(true);
    expect(diagnostics.some((diagnostic) => diagnostic.message.includes("unusedConst"))).toBe(true);
    expect(diagnostics.some((diagnostic) => diagnostic.message.includes("unusedArg"))).toBe(true);
    expect(diagnostics.some((diagnostic) => diagnostic.message.includes("Lonely"))).toBe(true);
    expect(diagnostics.every((diagnostic) => diagnostic.severity === 4)).toBe(true);
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
Dim unusedValue
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
    expect(hover).toContain("VBScript function.");
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
    expect(hover).toContain("VBScript property. Member of `DashboardCustomer`.");
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
Dim textValue, items, upperBound
textValue = CStr(42)
items = Array("a", "b")
upperBound = UBound(items)
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
    expect(tokenAt("CStr")).toEqual(
      expect.objectContaining({ tokenType: "function", tokenModifiers: ["library"] }),
    );
    expect(tokenAt("Array")).toEqual(
      expect.objectContaining({ tokenType: "function", tokenModifiers: ["library"] }),
    );
    expect(tokenAt("UBound")).toEqual(
      expect.objectContaining({ tokenType: "function", tokenModifiers: ["library"] }),
    );
    expect(getVbscriptHover(parsed, { line: 2, character: 13 })).toContain(
      "Function CStr(value) As String",
    );
    expect(getVbscriptSignatureHelp(parsed, { line: 4, character: 21 })).toEqual(
      expect.objectContaining({
        signatures: [expect.objectContaining({ label: "UBound(array, dimension)" })],
      }),
    );
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
    expect(
      String(resolveVbscriptCompletionItem(completion!, parsed, { symbols }).documentation),
    ).toContain("Builds a display name.");
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

  it("ignores single-quote XML comments and tolerates broken XML documentation", () => {
    const single = parseAspDocument(
      "file:///site/default.asp",
      `<%
' <summary>Not documentation.</summary>
Function BuildName()
End Function
%>`,
    );
    expect(getVbscriptHover(single, { line: 2, character: 10 })).not.toContain("Not documentation");

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
Sub Save(Optional ByVal firstName, ByRef lastName, defaultName)
End Sub
%>`,
    );
    const symbols = collectVbscriptSymbols(parsed);
    expect(symbols.find((symbol) => symbol.name === "label")?.typeName).toBe("String");
    expect(symbols.find((symbol) => symbol.name === "count")?.typeName).toBe("Number");
    expect(symbols.find((symbol) => symbol.name === "flags")?.typeName).toBe("Boolean");
    expect(symbols.find((symbol) => symbol.name === "items")?.typeName).toBe("Array");
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
      "Implicit VBScript variable.",
    );
    expect(
      getVbscriptInlayHints(
        parsed,
        { start: { line: 0, character: 0 }, end: { line: 4, character: 0 } },
        { symbols },
      ).some(
        (hint) =>
          hint.label === " As Number" && hint.paddingLeft === false && hint.paddingRight === true,
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
          token.tokenType === "variable" &&
          token.tokenModifiers?.includes("library"),
      ),
    ).toBe(true);
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
  Response.Write MaxCount + 1
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
      expect.objectContaining({ tokenType: "variable", tokenModifiers: ["readonly"] }),
    );
    expect(tokenAt("Name")).toEqual(
      expect.objectContaining({ tokenType: "property", tokenModifiers: ["public"] }),
    );
    expect(tokenAt("DisplayName")).toEqual(
      expect.objectContaining({ tokenType: "property", tokenModifiers: ["public"] }),
    );
    expect(tokenAt("Response")).toEqual(
      expect.objectContaining({ tokenType: "variable", tokenModifiers: ["library"] }),
    );
    expect(tokenAt("Write")).toEqual(
      expect.objectContaining({ tokenType: "method", tokenModifiers: ["library"] }),
    );
    expect(tokenAt("+")).toEqual(expect.objectContaining({ tokenType: "operator" }));
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

    const resolved = resolveVbscriptCompletionItem({ label: "BuildName" }, parsed, { symbols });
    expect(String(resolved.documentation)).toContain("Defined in file:///site/default.asp");
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

function parsedShape(parsed: ReturnType<typeof parseAspDocument>) {
  return {
    defaultLanguage: parsed.defaultLanguage,
    diagnostics: parsed.diagnostics.map((diagnostic) => ({
      range: diagnostic.range,
      message: diagnostic.message,
      source: diagnostic.source,
    })),
    includes: parsed.includes,
    directives: parsed.directives,
    regions: parsed.regions,
    children: parsed.cst.children.map((node) => ({
      kind: node.kind,
      start: node.start,
      end: node.end,
      contentStart: node.contentStart,
      contentEnd: node.contentEnd,
      language: node.language,
      regionKind: node.regionKind,
      text: node.text,
      include: node.include,
      directive: node.directive,
    })),
  };
}

function flattenVbNodes(node: VbCstNode): VbCstNode[] {
  return [node, ...node.children.flatMap(flattenVbNodes)];
}
