import { describe, expect, it } from "vitest";
import {
  analyzeVbscript,
  buildVirtualDocuments,
  collectVbscriptSymbols,
  getVbscriptCompletions,
  getVbscriptDefinition,
  getVbscriptHover,
  parseAspDocument,
} from "../src";

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
});

describe("VBScript analysis", () => {
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
});
