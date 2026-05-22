import { spawn, type ChildProcessWithoutNullStreams } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { describe, expect, it } from "vitest";

interface JsonRpcMessage {
  id?: number;
  method?: string;
  params?: unknown;
  result?: unknown;
}

interface MarkedDocument {
  text: string;
  position: { line: number; character: number };
}

describe("stdio LSP server", () => {
  it("handles initialize, didOpen, diagnostics and completion over JSON-RPC", async () => {
    const server = new RpcServer();
    try {
      await server.start();
      const initialize = await server.request("initialize", {
        processId: process.pid,
        rootUri: `file://${process.cwd()}`,
        capabilities: {},
      });
      expect(JSON.stringify(initialize)).toContain("completionProvider");

      const uri = "file:///tmp/default.asp";
      server.notify("textDocument/didOpen", {
        textDocument: {
          uri,
          languageId: "classic-asp",
          version: 1,
          text: `<% Option Explicit\nResponse.\nResponse.Write missingName\n%>`,
        },
      });

      const diagnostics = await server.waitForNotification("textDocument/publishDiagnostics");
      expect(JSON.stringify(diagnostics.params)).toContain("missingName");

      const completions = await server.request("textDocument/completion", {
        textDocument: { uri },
        position: { line: 1, character: 9 },
      });
      expect(JSON.stringify(completions)).toContain("Write");

      await server.request("shutdown", null);
      server.notify("exit", undefined);
    } finally {
      server.stop();
    }
  });

  it("returns HTML, CSS and JavaScript completions over JSON-RPC", async () => {
    const cases = [
      {
        uri: "file:///tmp/html.asp",
        markedSource: "<▮",
        expected: "div",
      },
      {
        uri: "file:///tmp/css-block.asp",
        markedSource: "<style>.x { colo▮ }</style>",
        expected: "color",
      },
      {
        uri: "file:///tmp/css-attribute.asp",
        markedSource: '<div style="colo▮"></div>',
        expected: "color",
      },
      {
        uri: "file:///tmp/client-js.asp",
        markedSource: "<script>const alphaBeta = 1; alpha▮</script>",
        expected: "alphaBeta",
      },
    ];

    const server = new RpcServer();
    try {
      await server.start();
      await server.request("initialize", {
        processId: process.pid,
        rootUri: "file:///tmp",
        capabilities: {},
      });
      for (const testCase of cases) {
        const document = markedDocument(testCase.markedSource);
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri: testCase.uri,
            languageId: "classic-asp",
            version: 1,
            text: document.text,
          },
        });
        await server.waitForNotification("textDocument/publishDiagnostics");
        const completions = await server.request("textDocument/completion", {
          textDocument: { uri: testCase.uri },
          position: document.position,
        });
        expect(JSON.stringify(completions)).toContain(testCase.expected);
      }
      await server.request("shutdown", null);
      server.notify("exit", undefined);
    } finally {
      server.stop();
    }
  });

  it("resolves HTML and CSS completion items", async () => {
    const server = new RpcServer();
    try {
      await server.start();
      await server.request("initialize", {
        processId: process.pid,
        rootUri: "file:///tmp",
        capabilities: {},
      });
      const uri = "file:///tmp/html-css-resolve.asp";
      const source = "<\n<style>.x { colo }</style>";
      server.notify("textDocument/didOpen", {
        textDocument: {
          uri,
          languageId: "classic-asp",
          version: 1,
          text: source,
        },
      });
      await server.waitForNotification("textDocument/publishDiagnostics");
      const htmlCompletions = await server.request("textDocument/completion", {
        textDocument: { uri },
        position: { line: 0, character: 1 },
      });
      const htmlItem = (htmlCompletions as Array<Record<string, unknown>>).find(
        (item) => item.label === "div",
      );
      expect(JSON.stringify(await server.request("completionItem/resolve", htmlItem))).toContain(
        "HTML completion",
      );

      const cssCompletions = await server.request("textDocument/completion", {
        textDocument: { uri },
        position: { line: 1, character: 16 },
      });
      const cssItem = (cssCompletions as Array<Record<string, unknown>>).find(
        (item) => item.label === "color",
      );
      expect(JSON.stringify(await server.request("completionItem/resolve", cssItem))).toContain(
        "CSS",
      );

      await server.request("shutdown", null);
      server.notify("exit", undefined);
    } finally {
      server.stop();
    }
  });

  it("renames HTML tags and CSS selectors through embedded language services", async () => {
    const source = `<div><span>name</span></div>
<style>.oldName { color: red; }</style>`;
    const server = new RpcServer();
    try {
      await server.start();
      await server.request("initialize", {
        processId: process.pid,
        rootUri: "file:///tmp",
        capabilities: {},
      });
      const uri = "file:///tmp/html-css-rename.asp";
      server.notify("textDocument/didOpen", {
        textDocument: {
          uri,
          languageId: "classic-asp",
          version: 1,
          text: source,
        },
      });
      await server.waitForNotification("textDocument/publishDiagnostics");

      const htmlRename = await server.request("textDocument/rename", {
        textDocument: { uri },
        position: { line: 0, character: 7 },
        newName: "strong",
      });
      expect(JSON.stringify(htmlRename)).toContain("strong");

      const cssRename = await server.request("textDocument/rename", {
        textDocument: { uri },
        position: { line: 1, character: 9 },
        newName: "newName",
      });
      expect(JSON.stringify(cssRename)).toContain("newName");

      await server.request("shutdown", null);
      server.notify("exit", undefined);
    } finally {
      server.stop();
    }
  });

  it("renames HTML class selectors across CSS and JavaScript selector strings", async () => {
    const marked = markedDocument(`<div class="card ol▮dName"></div>
<style>.oldName { color: red; }</style>
<script>
document.querySelector(".oldName");
</script>`);
    const server = new RpcServer();
    try {
      await server.start();
      await server.request("initialize", {
        processId: process.pid,
        rootUri: "file:///tmp",
        capabilities: {},
      });
      const uri = "file:///tmp/cross-rename.asp";
      server.notify("textDocument/didOpen", {
        textDocument: {
          uri,
          languageId: "classic-asp",
          version: 1,
          text: marked.text,
        },
      });
      await server.waitForNotification("textDocument/publishDiagnostics");

      const rename = await server.request("textDocument/rename", {
        textDocument: { uri },
        position: marked.position,
        newName: "newName",
      });
      const serialized = JSON.stringify(rename);
      expect(serialized.match(/newName/g)?.length ?? 0).toBeGreaterThanOrEqual(3);
      expect(serialized).toContain('"line":0');
      expect(serialized).toContain('"line":1');
      expect(serialized).toContain('"line":3');

      await server.request("shutdown", null);
      server.notify("exit", undefined);
    } finally {
      server.stop();
    }
  });

  it("delegates JavaScript hover, navigation, rename and signature help to TypeScript", async () => {
    const marked = markedDocument(`<script>
function greet(name) {
  return name.toUpperCase();
}
const message = gre▮et("Ada");
</script>`);
    const server = new RpcServer();
    try {
      await server.start();
      await server.request("initialize", {
        processId: process.pid,
        rootUri: "file:///tmp",
        capabilities: {},
      });
      const uri = "file:///tmp/js-lsp.asp";
      server.notify("textDocument/didOpen", {
        textDocument: {
          uri,
          languageId: "classic-asp",
          version: 1,
          text: marked.text,
        },
      });
      await server.waitForNotification("textDocument/publishDiagnostics");

      const hover = await server.request("textDocument/hover", {
        textDocument: { uri },
        position: marked.position,
      });
      expect(JSON.stringify(hover)).toContain("greet");

      const definition = await server.request("textDocument/definition", {
        textDocument: { uri },
        position: marked.position,
      });
      expect(JSON.stringify(definition)).toContain('"line":1');

      const references = await server.request("textDocument/references", {
        textDocument: { uri },
        position: marked.position,
        context: { includeDeclaration: true },
      });
      expect(Array.isArray(references) ? references.length : 0).toBeGreaterThan(1);

      const prepareRename = await server.request("textDocument/prepareRename", {
        textDocument: { uri },
        position: marked.position,
      });
      expect(JSON.stringify(prepareRename)).toContain('"line":4');

      const rename = await server.request("textDocument/rename", {
        textDocument: { uri },
        position: marked.position,
        newName: "formatName",
      });
      expect(JSON.stringify(rename)).toContain("formatName");

      const signature = await server.request("textDocument/signatureHelp", {
        textDocument: { uri },
        position: positionAt(marked.text, marked.text.indexOf('"Ada"')),
      });
      expect(JSON.stringify(signature)).toContain("name");

      await server.request("shutdown", null);
      server.notify("exit", undefined);
    } finally {
      server.stop();
    }
  });

  it("delegates server-side JScript navigation to TypeScript", async () => {
    const marked = markedDocument(`<%@ LANGUAGE="JScript" %>
<%
function serverGreet(name) {
  return name;
}
var message = serverGre▮et("Ada");
%>`);
    const server = new RpcServer();
    try {
      await server.start();
      await server.request("initialize", {
        processId: process.pid,
        rootUri: "file:///tmp",
        capabilities: {},
      });
      const uri = "file:///tmp/server-jscript.asp";
      server.notify("textDocument/didOpen", {
        textDocument: {
          uri,
          languageId: "classic-asp",
          version: 1,
          text: marked.text,
        },
      });
      await server.waitForNotification("textDocument/publishDiagnostics");

      const definition = await server.request("textDocument/definition", {
        textDocument: { uri },
        position: marked.position,
      });
      expect(JSON.stringify(definition)).toContain('"line":2');

      const typeDefinition = await server.request("textDocument/typeDefinition", {
        textDocument: { uri },
        position: marked.position,
      });
      expect(JSON.stringify(typeDefinition)).toContain('"line":2');

      await server.request("shutdown", null);
      server.notify("exit", undefined);
    } finally {
      server.stop();
    }
  });

  it("uses workspace JavaScript files in the TypeScript project model", async () => {
    const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-js-project-"));
    fs.writeFileSync(
      path.join(tempDir, "helper.js"),
      `export function externalHelper(value) {
  return value.toUpperCase();
}
`,
      "utf8",
    );
    const marked = markedDocument(`<script type="module">
import { externalHelper } from "./helper.js";
const value = externalHe▮lper("ada");
</script>`);
    const server = new RpcServer();
    try {
      await server.start();
      await server.request("initialize", {
        processId: process.pid,
        rootUri: `file://${tempDir}`,
        capabilities: {},
      });
      const uri = `file://${path.join(tempDir, "page.asp")}`;
      server.notify("textDocument/didOpen", {
        textDocument: {
          uri,
          languageId: "classic-asp",
          version: 1,
          text: marked.text,
        },
      });
      await server.waitForNotification("textDocument/publishDiagnostics");
      const definition = await server.request("textDocument/definition", {
        textDocument: { uri },
        position: marked.position,
      });
      expect(JSON.stringify(definition)).toContain("helper.js");

      await server.request("shutdown", null);
      server.notify("exit", undefined);
    } finally {
      server.stop();
      fs.rmSync(tempDir, { recursive: true, force: true });
    }
  });

  it("returns JavaScript call hierarchy plus CSS and JavaScript symbols", async () => {
    const marked = markedDocument(`<style>
.panel { color: red; }
</style>
<script>
function renderCard(value) {
  return value;
}
function boot() {
  return render▮Card("ready");
}
</script>`);
    const server = new RpcServer();
    try {
      await server.start();
      await server.request("initialize", {
        processId: process.pid,
        rootUri: "file:///tmp",
        capabilities: {},
      });
      const uri = "file:///tmp/js-symbols.asp";
      server.notify("textDocument/didOpen", {
        textDocument: {
          uri,
          languageId: "classic-asp",
          version: 1,
          text: marked.text,
        },
      });
      await server.waitForNotification("textDocument/publishDiagnostics");
      const symbols = await server.request("textDocument/documentSymbol", {
        textDocument: { uri },
      });
      expect(JSON.stringify(symbols)).toContain("panel");
      expect(JSON.stringify(symbols)).toContain("renderCard");

      const folding = await server.request("textDocument/foldingRange", {
        textDocument: { uri },
      });
      expect(Array.isArray(folding) ? folding.length : 0).toBeGreaterThan(1);

      const hierarchy = await server.request("textDocument/prepareCallHierarchy", {
        textDocument: { uri },
        position: marked.position,
      });
      expect(JSON.stringify(hierarchy)).toContain("renderCard");
      const incoming = await server.request("callHierarchy/incomingCalls", {
        item: (hierarchy as unknown[])[0],
      });
      expect(JSON.stringify(incoming)).toContain("boot");

      const workspaceSymbols = await server.request("workspace/symbol", { query: "render" });
      expect(JSON.stringify(workspaceSymbols)).toContain("renderCard");

      await server.request("shutdown", null);
      server.notify("exit", undefined);
    } finally {
      server.stop();
    }
  });

  it("publishes CSS and JavaScript diagnostics over JSON-RPC", async () => {
    const server = new RpcServer();
    try {
      await server.start();
      await server.request("initialize", {
        processId: process.pid,
        rootUri: "file:///tmp",
        capabilities: {},
      });

      const uri = "file:///tmp/diagnostics.asp";
      server.notify("textDocument/didOpen", {
        textDocument: {
          uri,
          languageId: "classic-asp",
          version: 1,
          text: `<style>.x { color: }</style>\n<script>const = ;</script>`,
        },
      });
      const diagnostics = await server.waitForNotification("textDocument/publishDiagnostics");
      const serialized = JSON.stringify(diagnostics.params);
      expect(serialized).toContain("asp-lsp-css");
      expect(serialized).toContain("asp-lsp-typescript");

      const pulled = await server.request("textDocument/diagnostic", {
        textDocument: { uri },
      });
      expect(JSON.stringify(pulled)).toContain("asp-lsp-typescript");

      await server.request("shutdown", null);
      server.notify("exit", undefined);
    } finally {
      server.stop();
    }
  });

  it("supports VBScript hover, definition, references, scoped symbols and class member completion", async () => {
    const marked = markedDocument(`<%
Class Customer
  Public Name
  Public Sub Save()
  End Sub
End Class
Dim customer
Set customer = New Customer
customer.▮
Function BuildName()
End Function
Response.Write BuildName()
%>`);
    const source = marked.text.replace("customer.", "customer.");
    const callPosition = positionAt(source, source.indexOf("BuildName()") + 2);
    const server = new RpcServer();
    try {
      await server.start();
      await server.request("initialize", {
        processId: process.pid,
        rootUri: "file:///tmp",
        capabilities: {},
      });
      const uri = "file:///tmp/vbscript.asp";
      server.notify("textDocument/didOpen", {
        textDocument: {
          uri,
          languageId: "classic-asp",
          version: 1,
          text: source,
        },
      });
      await server.waitForNotification("textDocument/publishDiagnostics");

      const memberCompletions = await server.request("textDocument/completion", {
        textDocument: { uri },
        position: marked.position,
      });
      expect(JSON.stringify(memberCompletions)).toContain("Save");
      expect(JSON.stringify(memberCompletions)).toContain("Name");

      const hover = await server.request("textDocument/hover", {
        textDocument: { uri },
        position: callPosition,
      });
      expect(JSON.stringify(hover)).toContain("function BuildName");

      const definition = await server.request("textDocument/definition", {
        textDocument: { uri },
        position: callPosition,
      });
      expect(JSON.stringify(definition)).toContain('"line":9');

      const references = await server.request("textDocument/references", {
        textDocument: { uri },
        position: callPosition,
        context: { includeDeclaration: true },
      });
      expect(Array.isArray(references) ? references.length : 0).toBeGreaterThan(1);

      await server.request("shutdown", null);
      server.notify("exit", undefined);
    } finally {
      server.stop();
    }
  });

  it("supports VBScript XML documentation comments over JSON-RPC", async () => {
    const source = `<%
''' <summary>Builds a display name.</summary>
''' <param name="first">First name.</param>
''' <returns>Display name.</returns>
Function BuildName(first)
  BuildName = first
End Function
Response.Write BuildName("Ada")
''' <▮
''' <param name="▮"></param>
''' <see ▮/>
''' <summary>Text</▮
''' <see cref="▮" />
%>`;
    const firstMarker = markedDocument(source);
    const text = firstMarker.text.replaceAll("▮", "");
    const tagPosition = firstMarker.position;
    const paramPosition = positionAt(text, text.indexOf('name=""></param>') + 'name="'.length);
    const attrPosition = positionAt(text, text.indexOf("<see />") + "<see ".length);
    const closingPosition = positionAt(
      text,
      text.indexOf("<summary>Text</") + "<summary>Text</".length,
    );
    const crefPosition = positionAt(text, text.indexOf('cref=""') + 'cref="'.length);
    const callPosition = positionAt(text, text.indexOf('BuildName("Ada")') + 2);
    const signaturePosition = positionAt(text, text.indexOf('"Ada"') + 2);
    const server = new RpcServer();
    try {
      await server.start();
      await server.request("initialize", {
        processId: process.pid,
        rootUri: "file:///tmp",
        capabilities: {},
      });
      const uri = "file:///tmp/vbscript-doc-comments.asp";
      server.notify("textDocument/didOpen", {
        textDocument: {
          uri,
          languageId: "classic-asp",
          version: 1,
          text,
        },
      });
      await server.waitForNotification("textDocument/publishDiagnostics");

      const hover = await server.request("textDocument/hover", {
        textDocument: { uri },
        position: callPosition,
      });
      expect(JSON.stringify(hover)).toContain("Builds a display name.");
      expect(JSON.stringify(hover)).toContain("First name.");

      const completions = await server.request("textDocument/completion", {
        textDocument: { uri },
        position: callPosition,
      });
      const buildCompletion = (completions as Array<Record<string, unknown>>).find(
        (item) => item.label === "BuildName",
      );
      expect(
        JSON.stringify(await server.request("completionItem/resolve", buildCompletion)),
      ).toContain("Builds a display name.");

      const signature = await server.request("textDocument/signatureHelp", {
        textDocument: { uri },
        position: signaturePosition,
      });
      expect(JSON.stringify(signature)).toContain("First name.");

      const tagCompletions = await server.request("textDocument/completion", {
        textDocument: { uri },
        position: tagPosition,
      });
      expect(JSON.stringify(tagCompletions)).toContain("summary");

      const paramCompletions = await server.request("textDocument/completion", {
        textDocument: { uri },
        position: paramPosition,
      });
      expect(JSON.stringify(paramCompletions)).toContain("first");

      const attrCompletions = await server.request("textDocument/completion", {
        textDocument: { uri },
        position: attrPosition,
      });
      expect(JSON.stringify(attrCompletions)).toContain("cref");

      const closingCompletions = await server.request("textDocument/completion", {
        textDocument: { uri },
        position: closingPosition,
      });
      expect(JSON.stringify(closingCompletions)).toContain("summary");

      const crefCompletions = await server.request("textDocument/completion", {
        textDocument: { uri },
        position: crefPosition,
      });
      expect(JSON.stringify(crefCompletions)).toContain("BuildName");

      await server.request("shutdown", null);
      server.notify("exit", undefined);
    } finally {
      server.stop();
    }
  });

  it("uses included VBScript symbols for completion and definition", async () => {
    const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-"));
    const owner = path.join(tempDir, "default.asp");
    const include = path.join(tempDir, "common.inc");
    fs.writeFileSync(
      include,
      `<%
Function SharedTitle()
End Function
%>`,
      "utf8",
    );
    const marked = markedDocument(`<!-- #include file="common.inc" -->
<%
Response.Write Shared▮Title()
%>`);
    fs.writeFileSync(owner, marked.text, "utf8");

    const server = new RpcServer();
    try {
      await server.start();
      await server.request("initialize", {
        processId: process.pid,
        rootUri: `file://${tempDir}`,
        capabilities: {},
      });
      server.notify("textDocument/didOpen", {
        textDocument: {
          uri: `file://${owner}`,
          languageId: "classic-asp",
          version: 1,
          text: marked.text,
        },
      });
      await server.waitForNotification("textDocument/publishDiagnostics");

      const completions = await server.request("textDocument/completion", {
        textDocument: { uri: `file://${owner}` },
        position: marked.position,
      });
      expect(JSON.stringify(completions)).toContain("SharedTitle");

      const definition = await server.request("textDocument/definition", {
        textDocument: { uri: `file://${owner}` },
        position: marked.position,
      });
      expect(JSON.stringify(definition)).toContain("common.inc");

      await server.request("shutdown", null);
      server.notify("exit", undefined);
    } finally {
      server.stop();
      fs.rmSync(tempDir, { recursive: true, force: true });
    }
  });

  it("updates include directives for file rename operations", async () => {
    const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-rename-"));
    const owner = path.join(tempDir, "default.asp");
    const include = path.join(tempDir, "common.inc");
    const renamed = path.join(tempDir, "renamed.inc");
    fs.writeFileSync(include, `<% Dim sharedValue %>`, "utf8");
    const source = `<!-- #include file="common.inc" -->\n<% Response.Write sharedValue %>`;
    fs.writeFileSync(owner, source, "utf8");
    const server = new RpcServer();
    try {
      await server.start();
      await server.request("initialize", {
        processId: process.pid,
        rootUri: `file://${tempDir}`,
        capabilities: {},
      });
      server.notify("textDocument/didOpen", {
        textDocument: {
          uri: `file://${owner}`,
          languageId: "classic-asp",
          version: 1,
          text: source,
        },
      });
      await server.waitForNotification("textDocument/publishDiagnostics");
      const edit = await server.request("workspace/willRenameFiles", {
        files: [{ oldUri: `file://${include}`, newUri: `file://${renamed}` }],
      });
      expect(JSON.stringify(edit)).toContain("renamed.inc");

      await server.request("shutdown", null);
      server.notify("exit", undefined);
    } finally {
      server.stop();
      fs.rmSync(tempDir, { recursive: true, force: true });
    }
  });

  it("supports VBScript rename, highlights, signature help, workspace symbols and semantic tokens", async () => {
    const marked = markedDocument(`<%
Function BuildName(firstName, lastName)
  BuildName = firstName & " " & lastName
End Function
Response.Write Build▮Name("Ada", "Lovelace")
%>`);
    const server = new RpcServer();
    try {
      await server.start();
      await server.request("initialize", {
        processId: process.pid,
        rootUri: "file:///tmp",
        capabilities: {},
      });
      const uri = "file:///tmp/vbscript-editing.asp";
      server.notify("textDocument/didOpen", {
        textDocument: {
          uri,
          languageId: "classic-asp",
          version: 1,
          text: marked.text,
        },
      });
      await server.waitForNotification("textDocument/publishDiagnostics");

      const prepareRename = await server.request("textDocument/prepareRename", {
        textDocument: { uri },
        position: marked.position,
      });
      expect(JSON.stringify(prepareRename)).toContain('"line":4');

      const rename = await server.request("textDocument/rename", {
        textDocument: { uri },
        position: marked.position,
        newName: "FormatName",
      });
      expect(JSON.stringify(rename)).toContain("FormatName");
      expect(JSON.stringify(rename)).toContain('"changes"');

      const highlights = await server.request("textDocument/documentHighlight", {
        textDocument: { uri },
        position: marked.position,
      });
      expect(Array.isArray(highlights) ? highlights.length : 0).toBeGreaterThan(1);

      const signature = await server.request("textDocument/signatureHelp", {
        textDocument: { uri },
        position: positionAt(marked.text, marked.text.indexOf('"Ada"')),
      });
      expect(JSON.stringify(signature)).toContain("BuildName(firstName, lastName)");

      const workspaceSymbols = await server.request("workspace/symbol", { query: "Build" });
      expect(JSON.stringify(workspaceSymbols)).toContain("BuildName");

      const semanticTokens = await server.request("textDocument/semanticTokens/full", {
        textDocument: { uri },
      });
      expect(JSON.stringify(semanticTokens)).toContain("data");
      expect((semanticTokens as { data?: unknown[] }).data?.length ?? 0).toBeGreaterThan(0);
      const semanticDelta = await server.request("textDocument/semanticTokens/full/delta", {
        textDocument: { uri },
        previousResultId: (semanticTokens as { resultId?: string }).resultId,
      });
      expect(JSON.stringify(semanticDelta)).toContain("edits");

      await server.request("shutdown", null);
      server.notify("exit", undefined);
    } finally {
      server.stop();
    }
  });

  it("supports extended LSP features over JSON-RPC", async () => {
    const marked = markedDocument(`<%
Class Customer
  Public Name
End Class
Function BuildName(firstName)
Dim c
  Set c = New Customer
  BuildName = c.Name
End Function
Sub Save()
  Response.Write Build▮Name("Ada")
End Sub
%>
<div><span>linked</span></div>
<style>.x { color: #ff0000; }</style>`);
    const server = new RpcServer();
    try {
      await server.start();
      const initialize = await server.request("initialize", {
        processId: process.pid,
        rootUri: "file:///tmp",
        capabilities: {},
      });
      const initializeText = JSON.stringify(initialize);
      expect(initializeText).toContain("selectionRangeProvider");
      expect(initializeText).toContain("inlayHintProvider");
      expect(initializeText).toContain("callHierarchyProvider");

      const uri = "file:///tmp/extended-lsp.asp";
      server.notify("textDocument/didOpen", {
        textDocument: {
          uri,
          languageId: "classic-asp",
          version: 1,
          text: marked.text,
        },
      });
      await server.waitForNotification("textDocument/publishDiagnostics");

      const completions = await server.request("textDocument/completion", {
        textDocument: { uri },
        position: marked.position,
      });
      const buildCompletion = (completions as { items?: Array<Record<string, unknown>> }).items
        ? (completions as { items: Array<Record<string, unknown>> }).items.find(
            (item) => item.label === "BuildName",
          )
        : (completions as Array<Record<string, unknown>>).find(
            (item) => item.label === "BuildName",
          );
      const resolved = await server.request("completionItem/resolve", buildCompletion);
      expect(JSON.stringify(resolved)).toContain("Defined in file:///tmp/extended-lsp.asp");

      const selection = await server.request("textDocument/selectionRange", {
        textDocument: { uri },
        positions: [marked.position],
      });
      expect(JSON.stringify(selection)).toContain("parent");

      const inlayHints = await server.request("textDocument/inlayHint", {
        textDocument: { uri },
        range: { start: { line: 0, character: 0 }, end: { line: 12, character: 0 } },
      });
      expect(JSON.stringify(inlayHints)).toContain("As Customer");
      expect(JSON.stringify(inlayHints)).toContain("firstName:");

      const hierarchyItems = await server.request("textDocument/prepareCallHierarchy", {
        textDocument: { uri },
        position: marked.position,
      });
      expect(JSON.stringify(hierarchyItems)).toContain("BuildName");
      const hierarchyItem = (hierarchyItems as unknown[])[0];
      const incoming = await server.request("callHierarchy/incomingCalls", { item: hierarchyItem });
      expect(JSON.stringify(incoming)).toContain("Save");
      const saveHierarchyItems = await server.request("textDocument/prepareCallHierarchy", {
        textDocument: { uri },
        position: positionAt(marked.text, marked.text.indexOf("Save()") + 2),
      });
      const outgoing = await server.request("callHierarchy/outgoingCalls", {
        item: (saveHierarchyItems as unknown[])[0],
      });
      expect(JSON.stringify(outgoing)).toContain("BuildName");

      const declaration = await server.request("textDocument/declaration", {
        textDocument: { uri },
        position: marked.position,
      });
      expect(JSON.stringify(declaration)).toContain('"line":4');

      const typeDefinition = await server.request("textDocument/typeDefinition", {
        textDocument: { uri },
        position: positionAt(marked.text, marked.text.indexOf("c.Name")),
      });
      expect(JSON.stringify(typeDefinition)).toContain('"line":1');

      const implementation = await server.request("textDocument/implementation", {
        textDocument: { uri },
        position: marked.position,
      });
      expect(JSON.stringify(implementation)).toContain('"line":4');

      const linked = await server.request("textDocument/linkedEditingRange", {
        textDocument: { uri },
        position: positionAt(marked.text, marked.text.indexOf("span") + 1),
      });
      expect(JSON.stringify(linked)).toContain("ranges");

      const colors = await server.request("textDocument/documentColor", {
        textDocument: { uri },
      });
      expect(JSON.stringify(colors)).toContain("red");
      const colorPresentation = await server.request("textDocument/colorPresentation", {
        textDocument: { uri },
        color: { red: 1, green: 0, blue: 0, alpha: 1 },
        range: (colors as Array<{ range: unknown }>)[0].range,
      });
      expect(JSON.stringify(colorPresentation)).toContain("#ff0000");

      const onType = await server.request("textDocument/onTypeFormatting", {
        textDocument: { uri },
        position: { line: 5, character: 0 },
        ch: "\n",
        options: { tabSize: 2, insertSpaces: true },
      });
      expect(JSON.stringify(onType)).toContain("newText");

      const rangeTokens = await server.request("textDocument/semanticTokens/range", {
        textDocument: { uri },
        range: { start: { line: 0, character: 0 }, end: { line: 12, character: 0 } },
      });
      expect((rangeTokens as { data?: unknown[] }).data?.length ?? 0).toBeGreaterThan(0);
      const deltaTokens = await server.request("textDocument/semanticTokens/full/delta", {
        textDocument: { uri },
        previousResultId: "missing",
      });
      expect((deltaTokens as { data?: unknown[] }).data?.length ?? 0).toBeGreaterThan(0);

      const codeLens = await server.request("textDocument/codeLens", {
        textDocument: { uri },
      });
      expect(JSON.stringify(codeLens)).toContain("references");

      await server.request("shutdown", null);
      server.notify("exit", undefined);
    } finally {
      server.stop();
    }
  });

  it("delegates JavaScript highlights and inlay hints", async () => {
    const marked = markedDocument(`<script>
function add(first, second) {
  return first + second;
}
const result = ad▮d(1, 2);
</script>`);
    const server = new RpcServer();
    try {
      await server.start();
      await server.request("initialize", {
        processId: process.pid,
        rootUri: "file:///tmp",
        capabilities: {},
      });
      const uri = "file:///tmp/js-highlight-inlay.asp";
      server.notify("textDocument/didOpen", {
        textDocument: {
          uri,
          languageId: "classic-asp",
          version: 1,
          text: marked.text,
        },
      });
      await server.waitForNotification("textDocument/publishDiagnostics");

      const highlights = await server.request("textDocument/documentHighlight", {
        textDocument: { uri },
        position: marked.position,
      });
      expect(Array.isArray(highlights) ? highlights.length : 0).toBeGreaterThan(1);

      const hints = await server.request("textDocument/inlayHint", {
        textDocument: { uri },
        range: { start: { line: 0, character: 0 }, end: { line: 6, character: 0 } },
      });
      expect(JSON.stringify(hints)).toContain("first:");
      expect(JSON.stringify(hints)).toContain("second:");

      await server.request("shutdown", null);
      server.notify("exit", undefined);
    } finally {
      server.stop();
    }
  });

  it("indexes unopened workspace ASP files for workspace symbols and supports reindex command", async () => {
    const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-index-"));
    const page = path.join(tempDir, "unopened.asp");
    fs.writeFileSync(
      page,
      `<%
Function IndexedTitle()
End Function
%>`,
      "utf8",
    );
    const server = new RpcServer();
    try {
      await server.start();
      await server.request("initialize", {
        processId: process.pid,
        rootUri: `file://${tempDir}`,
        capabilities: {},
      });
      const symbols = await server.request("workspace/symbol", { query: "Indexed" });
      expect(JSON.stringify(symbols)).toContain("IndexedTitle");

      fs.writeFileSync(
        page,
        `<%
Function ReindexedTitle()
End Function
%>`,
        "utf8",
      );
      await server.request("workspace/executeCommand", { command: "aspLsp.reindexWorkspace" });
      const reindexed = await server.request("workspace/symbol", { query: "Reindexed" });
      expect(JSON.stringify(reindexed)).toContain("ReindexedTitle");

      await server.request("shutdown", null);
      server.notify("exit", undefined);
    } finally {
      server.stop();
      fs.rmSync(tempDir, { recursive: true, force: true });
    }
  });

  it("returns workspace diagnostics for unopened ASP files", async () => {
    const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-workspace-diag-"));
    fs.writeFileSync(path.join(tempDir, "broken.asp"), `<style>.x { color: }</style>`, "utf8");
    const server = new RpcServer();
    try {
      await server.start();
      await server.request("initialize", {
        processId: process.pid,
        rootUri: `file://${tempDir}`,
        capabilities: {},
      });
      const diagnostics = await server.request("workspace/diagnostic", {
        previousResultIds: [],
      });
      expect(JSON.stringify(diagnostics)).toContain("broken.asp");
      expect(JSON.stringify(diagnostics)).toContain("asp-lsp-css");

      await server.request("shutdown", null);
      server.notify("exit", undefined);
    } finally {
      server.stop();
      fs.rmSync(tempDir, { recursive: true, force: true });
    }
  });

  it("returns CSS and JavaScript source code actions", async () => {
    const source = `<style>.x { color: #ff0000; }</style>
<script>
import { z } from "z";
import { a } from "a";
console.log(z, a);
</script>`;
    const server = new RpcServer();
    try {
      await server.start();
      await server.request("initialize", {
        processId: process.pid,
        rootUri: "file:///tmp",
        capabilities: {},
      });
      const uri = "file:///tmp/code-actions.asp";
      server.notify("textDocument/didOpen", {
        textDocument: {
          uri,
          languageId: "classic-asp",
          version: 1,
          text: source,
        },
      });
      await server.waitForNotification("textDocument/publishDiagnostics");
      const actions = await server.request("textDocument/codeAction", {
        textDocument: { uri },
        range: {
          start: { line: 1, character: 0 },
          end: { line: 5, character: 0 },
        },
        context: { diagnostics: [], only: ["source.organizeImports"] },
      });
      expect(JSON.stringify(actions)).toContain("Organize JavaScript imports");

      await server.request("shutdown", null);
      server.notify("exit", undefined);
    } finally {
      server.stop();
    }
  });

  it("returns quick fixes for undeclared VBScript variables", async () => {
    const marked = markedDocument(`<%
Option Explicit
Response.Write miss▮ingName
%>`);
    const server = new RpcServer();
    try {
      await server.start();
      await server.request("initialize", {
        processId: process.pid,
        rootUri: "file:///tmp",
        capabilities: {},
      });
      const uri = "file:///tmp/code-action.asp";
      server.notify("textDocument/didOpen", {
        textDocument: {
          uri,
          languageId: "classic-asp",
          version: 1,
          text: marked.text,
        },
      });
      const diagnostics = await server.waitForNotification("textDocument/publishDiagnostics");
      const actions = await server.request("textDocument/codeAction", {
        textDocument: { uri },
        range: {
          start: marked.position,
          end: marked.position,
        },
        context: {
          diagnostics: (diagnostics.params as { diagnostics: unknown[] }).diagnostics,
        },
      });
      expect(JSON.stringify(actions)).toContain("Declare missingName with Dim");

      await server.request("shutdown", null);
      server.notify("exit", undefined);
    } finally {
      server.stop();
    }
  });

  it("localizes asp-lsp diagnostics, code actions and CodeLens", async () => {
    const marked = markedDocument(`<!-- #include file="missing.inc" -->
<%
Option Explicit
Function Save()
End Function
Response.Write miss▮ingName
%>`);
    const server = new RpcServer();
    try {
      await server.start();
      const initialize = await server.request("initialize", {
        processId: process.pid,
        rootUri: "file:///tmp",
        locale: "ja-JP",
        capabilities: {},
      });
      expect(JSON.stringify(initialize)).toContain("codeLensProvider");
      const uri = "file:///tmp/localized.asp";
      server.notify("textDocument/didOpen", {
        textDocument: {
          uri,
          languageId: "classic-asp",
          version: 1,
          text: marked.text,
        },
      });
      const diagnostics = await server.waitForNotification("textDocument/publishDiagnostics");
      const serializedDiagnostics = JSON.stringify(diagnostics.params);
      expect(serializedDiagnostics).toContain("解決できません");
      expect(serializedDiagnostics).toContain("宣言されていません");

      const actions = await server.request("textDocument/codeAction", {
        textDocument: { uri },
        range: { start: marked.position, end: marked.position },
        context: {
          diagnostics: (diagnostics.params as { diagnostics: unknown[] }).diagnostics,
        },
      });
      const serializedActions = JSON.stringify(actions);
      expect(serializedActions).toContain("missingName を Dim で宣言");
      expect(serializedActions).toContain("不足している include missing.inc を作成");

      const codeLens = await server.request("textDocument/codeLens", {
        textDocument: { uri },
      });
      expect(JSON.stringify(codeLens)).toContain("件の参照");

      const unknown = await server.request("workspace/executeCommand", {
        command: "aspLsp.unknown",
      });
      expect(JSON.stringify(unknown)).toContain("不明なコマンド");

      server.notify("workspace/didChangeConfiguration", {
        settings: { aspLsp: { locale: "en" } },
      });
      const englishDiagnostics = await server.request("textDocument/diagnostic", {
        textDocument: { uri },
      });
      expect(JSON.stringify(englishDiagnostics)).toContain("could not be resolved");

      await server.request("shutdown", null);
      server.notify("exit", undefined);
    } finally {
      server.stop();
    }
  });

  it("returns an executable create-file edit for missing includes", async () => {
    const source = `<!-- #include file="missing.inc" -->\n<% Response.Write "x" %>`;
    const server = new RpcServer();
    try {
      await server.start();
      await server.request("initialize", {
        processId: process.pid,
        rootUri: "file:///tmp",
        capabilities: {},
      });
      const uri = "file:///tmp/missing-include.asp";
      server.notify("textDocument/didOpen", {
        textDocument: {
          uri,
          languageId: "classic-asp",
          version: 1,
          text: source,
        },
      });
      const diagnostics = await server.waitForNotification("textDocument/publishDiagnostics");
      const actions = await server.request("textDocument/codeAction", {
        textDocument: { uri },
        range: { start: { line: 0, character: 5 }, end: { line: 0, character: 15 } },
        context: {
          diagnostics: (diagnostics.params as { diagnostics: unknown[] }).diagnostics,
        },
      });
      expect(JSON.stringify(actions)).toContain("Create missing include missing.inc");
      expect(JSON.stringify(actions)).toContain('"kind":"create"');
      expect(JSON.stringify(actions)).toContain("missing.inc");

      await server.request("shutdown", null);
      server.notify("exit", undefined);
    } finally {
      server.stop();
    }
  });

  it("suggests VBScript includes for undeclared symbols found in workspace files", async () => {
    const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-include-suggest-"));
    const incDir = path.join(tempDir, "inc");
    fs.mkdirSync(incDir);
    fs.writeFileSync(
      path.join(incDir, "helpers.inc"),
      `<%
Function SharedHelper()
End Function
%>`,
      "utf8",
    );
    const marked = markedDocument(`<%
Option Explicit
Response.Write Shared▮Helper
%>`);
    const server = new RpcServer();
    try {
      await server.start();
      await server.request("initialize", {
        processId: process.pid,
        rootUri: `file://${tempDir}`,
        capabilities: {},
      });
      const uri = `file://${path.join(tempDir, "default.asp")}`;
      server.notify("textDocument/didOpen", {
        textDocument: {
          uri,
          languageId: "classic-asp",
          version: 1,
          text: marked.text,
        },
      });
      const diagnostics = await server.waitForNotification("textDocument/publishDiagnostics");
      const actions = await server.request("textDocument/codeAction", {
        textDocument: { uri },
        range: { start: marked.position, end: marked.position },
        context: {
          diagnostics: (diagnostics.params as { diagnostics: unknown[] }).diagnostics,
        },
      });
      const serialized = JSON.stringify(actions);
      expect(serialized).toContain("Include /inc/helpers.inc for SharedHelper");
      expect(serialized).toContain('<!-- #include virtual=\\"/inc/helpers.inc\\" -->');

      await server.request("shutdown", null);
      server.notify("exit", undefined);
    } finally {
      server.stop();
      fs.rmSync(tempDir, { recursive: true, force: true });
    }
  });

  it("reports JavaScript unused diagnostics as hints even when checkJs is off", async () => {
    const server = new RpcServer();
    try {
      await server.start();
      await server.request("initialize", {
        processId: process.pid,
        rootUri: "file:///tmp",
        capabilities: {},
      });
      const uri = "file:///tmp/js-unused.asp";
      server.notify("textDocument/didOpen", {
        textDocument: {
          uri,
          languageId: "classic-asp",
          version: 1,
          text: `<script>
function demo(unusedParam) {
  const unusedLocal = 1;
  return 1;
}
</script>`,
        },
      });
      const diagnostics = await server.waitForNotification("textDocument/publishDiagnostics");
      const serialized = JSON.stringify(diagnostics.params);
      expect(serialized).toContain("asp-lsp-typescript-unused");
      expect(serialized).toContain("unusedLocal");
      expect(serialized).toContain('"severity":4');

      await server.request("shutdown", null);
      server.notify("exit", undefined);
    } finally {
      server.stop();
    }
  });

  it("returns JavaScript auto import completion edits and add import quick fixes", async () => {
    const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-js-auto-import-"));
    fs.writeFileSync(
      path.join(tempDir, "helpers.js"),
      `export function helperThing() {
  return "ok";
}
`,
      "utf8",
    );
    const marked = markedDocument(`<script type="module">
help▮
</script>`);
    const server = new RpcServer();
    try {
      await server.start();
      await server.request("initialize", {
        processId: process.pid,
        rootUri: `file://${tempDir}`,
        capabilities: {},
      });
      server.notify("workspace/didChangeConfiguration", {
        settings: { aspLsp: { checkJs: true, javascript: { autoImports: true } } },
      });
      const uri = `file://${path.join(tempDir, "default.asp")}`;
      server.notify("textDocument/didOpen", {
        textDocument: {
          uri,
          languageId: "classic-asp",
          version: 1,
          text: marked.text,
        },
      });
      await server.waitForNotification("textDocument/publishDiagnostics");
      const completions = await server.request("textDocument/completion", {
        textDocument: { uri },
        position: marked.position,
      });
      const items = (completions as { items?: Array<Record<string, unknown>> }).items
        ? (completions as { items: Array<Record<string, unknown>> }).items
        : (completions as Array<Record<string, unknown>>);
      const helperItem = items.find((item) => item.label === "helperThing");
      expect(helperItem).toBeTruthy();
      const resolved = await server.request("completionItem/resolve", helperItem);
      expect(JSON.stringify(resolved)).toContain("additionalTextEdits");
      expect(JSON.stringify(resolved)).toContain("./helpers");

      const callDocument = markedDocument(`<script type="module">
helper▮Thing();
</script>`);
      server.notify("textDocument/didChange", {
        textDocument: { uri, version: 2 },
        contentChanges: [{ text: callDocument.text }],
      });
      const diagnostics = await server.waitForNotification("textDocument/publishDiagnostics");
      const actions = await server.request("textDocument/codeAction", {
        textDocument: { uri },
        range: { start: callDocument.position, end: callDocument.position },
        context: {
          diagnostics: (diagnostics.params as { diagnostics: unknown[] }).diagnostics,
        },
      });
      expect(JSON.stringify(actions)).toContain("Add import");

      await server.request("shutdown", null);
      server.notify("exit", undefined);
    } finally {
      server.stop();
      fs.rmSync(tempDir, { recursive: true, force: true });
    }
  });

  it("formats full ASP documents and ASP ranges over JSON-RPC", async () => {
    const source = `<html>
<body>
<% Option Explicit
If enabled Then
Response.Write "ok"
End If
%>
<div>done</div>
</body>
</html>`;
    const server = new RpcServer();
    try {
      await server.start();
      await server.request("initialize", {
        processId: process.pid,
        rootUri: "file:///tmp",
        capabilities: {},
      });
      const uri = "file:///tmp/format.asp";
      server.notify("textDocument/didOpen", {
        textDocument: {
          uri,
          languageId: "classic-asp",
          version: 1,
          text: source,
        },
      });
      await server.waitForNotification("textDocument/publishDiagnostics");

      const fullEdits = await server.request("textDocument/formatting", {
        textDocument: { uri },
        options: { tabSize: 2, insertSpaces: true },
      });
      const fullText = JSON.stringify(fullEdits);
      expect(fullText).toContain("<%");
      expect(fullText).toContain("  Response.Write");

      const rangeEdits = await server.request("textDocument/rangeFormatting", {
        textDocument: { uri },
        range: {
          start: { line: 2, character: 0 },
          end: { line: 6, character: 2 },
        },
        options: { tabSize: 2, insertSpaces: true },
      });
      const rangeText = JSON.stringify(rangeEdits);
      expect(rangeText).toContain("  Response.Write");
      expect(rangeText).not.toContain("<html>");

      await server.request("shutdown", null);
      server.notify("exit", undefined);
    } finally {
      server.stop();
    }
  });

  it("delegates server-side JScript formatting to the TypeScript formatter", async () => {
    const source = `<%@ LANGUAGE="JScript" %>
<%
function greet(name){return "a=b";}
%>`;
    const server = new RpcServer();
    try {
      await server.start();
      await server.request("initialize", {
        processId: process.pid,
        rootUri: "file:///tmp",
        capabilities: {},
      });
      const uri = "file:///tmp/jscript-format.asp";
      server.notify("textDocument/didOpen", {
        textDocument: {
          uri,
          languageId: "classic-asp",
          version: 1,
          text: source,
        },
      });
      await server.waitForNotification("textDocument/publishDiagnostics");

      const fullEdits = await server.request("textDocument/formatting", {
        textDocument: { uri },
        options: { tabSize: 2, insertSpaces: true },
      });
      const fullText = (fullEdits as Array<{ newText: string }>)[0]?.newText ?? "";
      expect(fullText).toContain("function greet(name) {");
      expect(fullText).toContain(`"a=b"`);

      const rangeEdits = await server.request("textDocument/rangeFormatting", {
        textDocument: { uri },
        range: {
          start: { line: 2, character: 0 },
          end: { line: 2, character: 36 },
        },
        options: { tabSize: 2, insertSpaces: true },
      });
      const rangeText = (rangeEdits as Array<{ newText: string }>)[0]?.newText ?? "";
      expect(rangeText).toContain("function greet(name) {");
      expect(rangeText).not.toContain("<%@");

      await server.request("shutdown", null);
      server.notify("exit", undefined);
    } finally {
      server.stop();
    }
  });

  it("publishes strict VBScript type diagnostics and custom COM completions", async () => {
    const server = new RpcServer();
    try {
      await server.start();
      await server.request("initialize", {
        processId: process.pid,
        rootUri: "file:///tmp",
        capabilities: {},
      });
      server.notify("workspace/didChangeConfiguration", {
        settings: {
          aspLsp: {
            vbscript: {
              typeChecking: "strict",
              comTypes: {
                "Custom.Widget": {
                  members: {
                    Title: "String",
                    Ping: {
                      kind: "method",
                      returnType: "Boolean",
                      parameters: [{ name: "name", type: "String" }],
                    },
                  },
                },
              },
            },
          },
        },
      });
      const uri = "file:///tmp/vb-type.asp";
      server.notify("textDocument/didOpen", {
        textDocument: {
          uri,
          languageId: "classic-asp",
          version: 1,
          text: `<%
Dim widget
Set widget = Server.CreateObject("Custom.Widget")
widget.
widget.Missing
widget.Ping("a", "b")
%>`,
        },
      });
      const diagnostics = await server.waitForNotification("textDocument/publishDiagnostics");
      const diagnosticText = JSON.stringify(diagnostics.params);
      expect(diagnosticText).toContain("no member");
      expect(diagnosticText).toContain("Argument count");

      const completions = await server.request("textDocument/completion", {
        textDocument: { uri },
        position: { line: 3, character: 7 },
      });
      const completionText = JSON.stringify(completions);
      expect(completionText).toContain("Title");
      expect(completionText).toContain("Ping");

      await server.request("shutdown", null);
      server.notify("exit", undefined);
    } finally {
      server.stop();
    }
  });

  it("resolves virtual includes from configured roots", async () => {
    const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-"));
    const pageDir = path.join(tempDir, "pages");
    const sharedDir = path.join(tempDir, "shared");
    fs.mkdirSync(pageDir);
    fs.mkdirSync(sharedDir);
    const owner = path.join(pageDir, "default.asp");
    const include = path.join(sharedDir, "common.inc");
    fs.writeFileSync(include, "<%\nFunction SharedTitle()\nEnd Function\n%>", "utf8");
    const marked = markedDocument(`<!-- #include virtual="/shared/common.inc" -->
<%
Response.Write Shared▮Title()
%>`);
    fs.writeFileSync(owner, marked.text, "utf8");

    const server = new RpcServer();
    try {
      await server.start();
      await server.request("initialize", {
        processId: process.pid,
        rootUri: `file://${pageDir}`,
        capabilities: {},
      });
      server.notify("workspace/didChangeConfiguration", {
        settings: { aspLsp: { virtualRoots: [tempDir], legacyEncoding: "shift_jis" } },
      });
      server.notify("textDocument/didOpen", {
        textDocument: {
          uri: `file://${owner}`,
          languageId: "classic-asp",
          version: 1,
          text: marked.text,
        },
      });
      await server.waitForNotification("textDocument/publishDiagnostics");

      const definition = await server.request("textDocument/definition", {
        textDocument: { uri: `file://${owner}` },
        position: marked.position,
      });
      expect(JSON.stringify(definition)).toContain("common.inc");

      await server.request("shutdown", null);
      server.notify("exit", undefined);
    } finally {
      server.stop();
      fs.rmSync(tempDir, { recursive: true, force: true });
    }
  });

  it("reports include cycles through publishDiagnostics", async () => {
    const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-"));
    const owner = path.join(tempDir, "default.asp");
    const include = path.join(tempDir, "loop.inc");
    fs.writeFileSync(owner, '<!-- #include file="loop.inc" -->\n<% Response.Write 1 %>', "utf8");
    fs.writeFileSync(include, '<!-- #include file="default.asp" -->', "utf8");

    const server = new RpcServer();
    try {
      await server.start();
      await server.request("initialize", {
        processId: process.pid,
        rootUri: `file://${tempDir}`,
        capabilities: {},
      });
      server.notify("textDocument/didOpen", {
        textDocument: {
          uri: `file://${owner}`,
          languageId: "classic-asp",
          version: 1,
          text: fs.readFileSync(owner, "utf8"),
        },
      });

      const diagnostics = await server.waitForNotification("textDocument/publishDiagnostics");
      expect(JSON.stringify(diagnostics.params)).toContain("Include cycle detected");

      await server.request("shutdown", null);
      server.notify("exit", undefined);
    } finally {
      server.stop();
      fs.rmSync(tempDir, { recursive: true, force: true });
    }
  });

  it("reports transitive include cycles that do not return to the owner", async () => {
    const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-"));
    const owner = path.join(tempDir, "default.asp");
    const first = path.join(tempDir, "first.inc");
    const second = path.join(tempDir, "second.inc");
    fs.writeFileSync(owner, '<!-- #include file="first.inc" -->', "utf8");
    fs.writeFileSync(first, '<!-- #include file="second.inc" -->', "utf8");
    fs.writeFileSync(second, '<!-- #include file="first.inc" -->', "utf8");

    const server = new RpcServer();
    try {
      await server.start();
      await server.request("initialize", {
        processId: process.pid,
        rootUri: `file://${tempDir}`,
        capabilities: {},
      });
      server.notify("textDocument/didOpen", {
        textDocument: {
          uri: `file://${owner}`,
          languageId: "classic-asp",
          version: 1,
          text: fs.readFileSync(owner, "utf8"),
        },
      });

      const diagnostics = await server.waitForNotification("textDocument/publishDiagnostics");
      expect(JSON.stringify(diagnostics.params)).toContain("Include cycle detected");

      await server.request("shutdown", null);
      server.notify("exit", undefined);
    } finally {
      server.stop();
      fs.rmSync(tempDir, { recursive: true, force: true });
    }
  });
});

class RpcServer {
  private child: ChildProcessWithoutNullStreams | undefined;
  private nextId = 1;
  private buffer = Buffer.alloc(0);
  private stderr = "";
  private responses = new Map<number, (message: JsonRpcMessage) => void>();
  private notifications = new Map<string, Array<(message: JsonRpcMessage) => void>>();
  private pendingNotifications = new Map<string, JsonRpcMessage[]>();

  async start(): Promise<void> {
    const serverPath = path.join(process.cwd(), "dist", "server.js");
    this.child = spawn(process.execPath, [serverPath, "--stdio"], {
      cwd: process.cwd(),
      stdio: ["pipe", "pipe", "pipe"],
    });
    this.child.stdout.on("data", (chunk: Buffer) => this.read(chunk));
    this.child.stderr.on("data", (chunk: Buffer) => {
      this.stderr += chunk.toString("utf8");
    });
  }

  request(method: string, params: unknown): Promise<unknown> {
    const id = this.nextId;
    this.nextId += 1;
    this.write({ jsonrpc: "2.0", id, method, params });
    return new Promise((resolve, reject) => {
      this.responses.set(id, (message) => resolve(message.result));
      setTimeout(() => reject(new Error(`Timed out waiting for ${method}: ${this.stderr}`)), 3000);
    });
  }

  notify(method: string, params: unknown): void {
    this.write({ jsonrpc: "2.0", method, params });
  }

  waitForNotification(method: string): Promise<JsonRpcMessage> {
    const pending = this.pendingNotifications.get(method);
    const message = pending?.shift();
    if (message) {
      return Promise.resolve(message);
    }
    return new Promise((resolve, reject) => {
      const callbacks = this.notifications.get(method) ?? [];
      callbacks.push(resolve);
      this.notifications.set(method, callbacks);
      setTimeout(() => reject(new Error(`Timed out waiting for ${method}: ${this.stderr}`)), 3000);
    });
  }

  stop(): void {
    this.child?.kill();
  }

  private write(message: unknown): void {
    const body = JSON.stringify(message);
    this.child?.stdin.write(`Content-Length: ${Buffer.byteLength(body, "utf8")}\r\n\r\n${body}`);
  }

  private read(chunk: Buffer): void {
    this.buffer = Buffer.concat([this.buffer, chunk]);
    while (true) {
      const headerEnd = this.buffer.indexOf("\r\n\r\n");
      if (headerEnd === -1) {
        return;
      }
      const header = this.buffer.slice(0, headerEnd).toString("utf8");
      const length = /Content-Length:\s*(\d+)/i.exec(header)?.[1];
      if (!length) {
        throw new Error(`Missing Content-Length header: ${header}`);
      }
      const bodyStart = headerEnd + 4;
      const bodyEnd = bodyStart + Number(length);
      if (this.buffer.length < bodyEnd) {
        return;
      }
      const message = JSON.parse(
        this.buffer.slice(bodyStart, bodyEnd).toString("utf8"),
      ) as JsonRpcMessage;
      this.buffer = this.buffer.slice(bodyEnd);
      if (message.id !== undefined) {
        this.responses.get(message.id)?.(message);
        this.responses.delete(message.id);
      } else if (message.method) {
        const callbacks = this.notifications.get(message.method) ?? [];
        const callback = callbacks.shift();
        if (callback) {
          callback(message);
        } else {
          const pending = this.pendingNotifications.get(message.method) ?? [];
          pending.push(message);
          this.pendingNotifications.set(message.method, pending);
        }
      }
    }
  }
}

function markedDocument(source: string): MarkedDocument {
  const offset = source.indexOf("▮");
  if (offset === -1) {
    throw new Error("Marked source is missing a cursor marker.");
  }
  const text = source.slice(0, offset) + source.slice(offset + "▮".length);
  return { text, position: positionAt(text, offset) };
}

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
