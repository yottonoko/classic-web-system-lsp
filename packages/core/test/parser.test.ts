import { describe, expect, it } from "vitest";
import {
  analyzeVbscript,
  buildVbTypeEnvironment,
  buildVirtualDocuments,
  collectVbscriptSymbols,
  formatAspDocument,
  formatAspRange,
  getVbscriptCompletions,
  getVbscriptDefinition,
  getVbscriptHover,
  getVbscriptInlayHints,
  getVbscriptSelectionRanges,
  getVbscriptSignatureHelp,
  getVbscriptTypeDefinition,
  parseAspCst,
  parseAspDocument,
  parseVbscriptCst,
  prepareVbscriptCallHierarchy,
  resolveVbscriptCompletionItem,
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

  it("masks inline ASP inside CSS regions while keeping ASP completions routable", () => {
    const source = `<style>.x { color: <%= themeColor %>; }</style>`;
    const parsed = parseAspDocument("file:///site/default.asp", source);
    const docs = buildVirtualDocuments(parsed);
    expect(docs.get("css")?.text).toContain(".x { color:");
    expect(docs.get("css")?.text).not.toContain("themeColor");
    expect(parsed.regions.some((region) => region.kind === "asp-expression")).toBe(true);
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
    expect(getVbscriptHover(parsed, { line: 3, character: 17 }, { symbols })).toContain(
      "function BuildName",
    );
    expect(getVbscriptDefinition(parsed, { line: 3, character: 17 }, { symbols })?.name).toBe(
      "BuildName",
    );
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
    expect(getVbscriptSignatureHelp(parsed, { line: 2, character: 18 })).toEqual(
      expect.objectContaining({
        signatures: [expect.objectContaining({ label: "Server.MapPath(path)" })],
      }),
    );
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

function flattenVbNodes(node: VbCstNode): VbCstNode[] {
  return [node, ...node.children.flatMap(flattenVbNodes)];
}
