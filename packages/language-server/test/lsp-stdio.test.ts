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

interface DecodedSemanticToken {
  line: number;
  character: number;
  length: number;
  tokenType: number;
  tokenModifiers: number;
}

const rpcTimeoutMs = 30_000;
const semanticTokenType = {
  keyword: 0,
  variable: 1,
  parameter: 2,
  function: 3,
  class: 4,
  method: 5,
  property: 6,
  comment: 7,
} as const;
const semanticTokenModifier = {
  public: 1 << 0,
  private: 1 << 1,
  readonly: 1 << 2,
  library: 1 << 3,
} as const;

describe(
  "stdio LSP server",
  () => {
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

    it("renames HTML class selectors across indexed workspace files", async () => {
      const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-"));
      const owner = path.join(tempDir, "default.asp");
      const style = path.join(tempDir, "style.inc");
      const script = path.join(tempDir, "script.asp");
      const marked = markedDocument(`<div class="card ol▮dName"></div>`);
      fs.writeFileSync(owner, marked.text, "utf8");
      fs.writeFileSync(style, "<style>.oldName { color: red; }</style>", "utf8");
      fs.writeFileSync(script, '<script>document.querySelector(".oldName");</script>', "utf8");

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

        const rename = await server.request("textDocument/rename", {
          textDocument: { uri: `file://${owner}` },
          position: marked.position,
          newName: "newName",
        });
        const serialized = JSON.stringify(rename);
        expect(serialized).toContain("default.asp");
        expect(serialized).toContain("style.inc");
        expect(serialized).toContain("script.asp");
        expect(serialized.match(/newName/g)?.length ?? 0).toBeGreaterThanOrEqual(3);

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
        fs.rmSync(tempDir, { recursive: true, force: true });
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
        const serializedHover = JSON.stringify(hover);
        expect(serializedHover).toContain('"kind":"markdown"');
        expect(serializedHover).toContain("```vbscript");
        expect(serializedHover).toContain("Function BuildName()");
        expect(serializedHover).toContain("VBScript function.");

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

        const semanticTokens = await server.request("textDocument/semanticTokens/full", {
          textDocument: { uri: `file://${owner}` },
        });
        const decoded = decodeSemanticTokens((semanticTokens as { data?: number[] }).data);
        expect(
          decoded.some(
            (token) =>
              token.line === marked.position.line &&
              token.character === marked.position.character - "Shared".length &&
              token.tokenType === semanticTokenType.function,
          ),
        ).toBe(true);

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
        fs.rmSync(tempDir, { recursive: true, force: true });
      }
    });

    it("returns hover, inlay hints and semantic tokens for implicit VBScript variables", async () => {
      const source = `<%
a = 1
Response.Write a
%>`;
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: "file:///tmp",
          capabilities: {},
        });
        const uri = "file:///tmp/implicit-vbscript.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        await server.waitForNotification("textDocument/publishDiagnostics");

        const hover = await server.request("textDocument/hover", {
          textDocument: { uri },
          position: { line: 1, character: 0 },
        });
        expect(JSON.stringify(hover)).toContain("Implicit VBScript variable.");

        const inlayHints = await server.request("textDocument/inlayHint", {
          textDocument: { uri },
          range: { start: { line: 0, character: 0 }, end: { line: 4, character: 0 } },
        });
        expect(JSON.stringify(inlayHints)).toContain("As Number");

        const semanticTokens = await server.request("textDocument/semanticTokens/full", {
          textDocument: { uri },
        });
        const decoded = decodeSemanticTokens((semanticTokens as { data?: number[] }).data);
        expect(
          decoded.some(
            (token) =>
              token.line === 2 &&
              token.character === "Response.Write ".length &&
              token.tokenType === semanticTokenType.variable,
          ),
        ).toBe(true);
        expect(
          decoded.some(
            (token) =>
              token.line === 2 &&
              token.character === 0 &&
              token.tokenType === semanticTokenType.variable &&
              token.tokenModifiers === semanticTokenModifier.library,
          ),
        ).toBe(true);

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
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
        const initialize = await server.request("initialize", {
          processId: process.pid,
          rootUri: "file:///tmp",
          capabilities: {},
        });
        const initializeText = JSON.stringify(initialize);
        expect(initializeText).toContain('"parameter"');
        expect(initializeText).toContain('"public"');
        expect(initializeText).toContain('"private"');
        expect(initializeText).toContain('"readonly"');
        expect(initializeText).toContain('"library"');
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
        const decodedSemanticTokens = decodeSemanticTokens(
          (semanticTokens as { data?: number[] }).data,
        );
        expect(decodedSemanticTokens.length).toBeGreaterThan(6);
        expect(
          decodedSemanticTokens.some(
            (token) =>
              token.line === marked.position.line &&
              token.character === marked.position.character - "Build".length &&
              token.tokenType === semanticTokenType.function,
          ),
        ).toBe(true);
        expect(
          decodedSemanticTokens.some(
            (token) =>
              token.line === 2 &&
              token.character === 14 &&
              token.tokenType === semanticTokenType.parameter,
          ),
        ).toBe(true);
        expect(
          decodedSemanticTokens.some(
            (token) =>
              token.line === 1 &&
              token.character === "Function BuildName(".length &&
              token.tokenType === semanticTokenType.parameter,
          ),
        ).toBe(true);
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
        expect(initializeText).toContain("typeHierarchyProvider");
        expect(initializeText).toContain("monikerProvider");
        expect(initializeText).toContain("inlineValueProvider");
        expect(initializeText).toContain("willSaveWaitUntil");

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
        const willSaveEdits = await server.request("textDocument/willSaveWaitUntil", {
          textDocument: { uri },
          reason: 1,
        });
        expect(willSaveEdits).toEqual([]);

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
        const resolvedHint = await server.request(
          "inlayHint/resolve",
          (inlayHints as Array<Record<string, unknown>>)[0],
        );
        expect(JSON.stringify(resolvedHint)).toContain("label");

        const hierarchyItems = await server.request("textDocument/prepareCallHierarchy", {
          textDocument: { uri },
          position: marked.position,
        });
        expect(JSON.stringify(hierarchyItems)).toContain("BuildName");
        const hierarchyItem = (hierarchyItems as unknown[])[0];
        const incoming = await server.request("callHierarchy/incomingCalls", {
          item: hierarchyItem,
        });
        expect(JSON.stringify(incoming)).toContain("Save");
        const saveHierarchyItems = await server.request("textDocument/prepareCallHierarchy", {
          textDocument: { uri },
          position: positionAt(marked.text, marked.text.indexOf("Save()") + 2),
        });
        const outgoing = await server.request("callHierarchy/outgoingCalls", {
          item: (saveHierarchyItems as unknown[])[0],
        });
        expect(JSON.stringify(outgoing)).toContain("BuildName");

        const typeHierarchy = await server.request("textDocument/prepareTypeHierarchy", {
          textDocument: { uri },
          position: positionAt(marked.text, marked.text.indexOf("Customer") + 2),
        });
        expect(JSON.stringify(typeHierarchy)).toContain("Customer");
        const supertypes = await server.request("typeHierarchy/supertypes", {
          item: (typeHierarchy as unknown[])[0],
        });
        expect(supertypes).toEqual([]);

        const monikers = await server.request("textDocument/moniker", {
          textDocument: { uri },
          position: marked.position,
        });
        expect(JSON.stringify(monikers)).toContain("BuildName");

        const inlineValues = await server.request("textDocument/inlineValue", {
          textDocument: { uri },
          range: { start: { line: 0, character: 0 }, end: { line: 12, character: 0 } },
          context: {
            frameId: 1,
            stoppedLocation: { start: marked.position, end: marked.position },
          },
        });
        expect(JSON.stringify(inlineValues)).toContain("firstName");

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
        const resolvedCodeLens = await server.request(
          "codeLens/resolve",
          (codeLens as Array<Record<string, unknown>>)[0],
        );
        expect(JSON.stringify(resolvedCodeLens)).toContain("command");

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

    it("returns VBScript extract variable refactors", async () => {
      const source = `<%
Response.Write Request.QueryString("name")
%>`;
      const selectionStart = source.indexOf('Request.QueryString("name")');
      const selectionEnd = selectionStart + 'Request.QueryString("name")'.length;
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: "file:///tmp",
          capabilities: {},
        });
        const uri = "file:///tmp/vb-refactor.asp";
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
            start: positionAt(source, selectionStart),
            end: positionAt(source, selectionEnd),
          },
          context: { diagnostics: [], only: ["refactor.extract"] },
        });
        const serialized = JSON.stringify(actions);
        expect(serialized).toContain("Extract VBScript variable");
        expect(serialized).toContain("Dim extractedValue");
        expect(serialized).toContain('extractedValue = Request.QueryString(\\"name\\")');
        expect(serialized).toContain('"newText":"extractedValue"');

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("avoids existing VBScript extract variable names", async () => {
      const source = `<%
Dim extractedValue
Response.Write Request.QueryString("name")
%>`;
      const selectionStart = source.indexOf('Request.QueryString("name")');
      const selectionEnd = selectionStart + 'Request.QueryString("name")'.length;
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: "file:///tmp",
          capabilities: {},
        });
        const uri = "file:///tmp/vb-refactor-collision.asp";
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
            start: positionAt(source, selectionStart),
            end: positionAt(source, selectionEnd),
          },
          context: { diagnostics: [], only: ["refactor.extract"] },
        });
        const serialized = JSON.stringify(actions);
        expect(serialized).toContain("Dim extractedValue1");
        expect(serialized).toContain('extractedValue1 = Request.QueryString(\\"name\\")');
        expect(serialized).toContain('"newText":"extractedValue1"');

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("does not return VBScript extract refactors for unsupported selections", async () => {
      const source = `<div>Request.QueryString("name")</div>
<%
Response.Write Request.QueryString("name")
Response.Write Request.Form("name")
%>`;
      const htmlStart = source.indexOf('Request.QueryString("name")');
      const htmlEnd = htmlStart + 'Request.QueryString("name")'.length;
      const vbStart = source.lastIndexOf('Request.QueryString("name")');
      const vbEnd = vbStart + 'Request.QueryString("name")'.length;
      const multilineEnd = source.indexOf('Request.Form("name")') + 'Request.Form("name")'.length;
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: "file:///tmp",
          capabilities: {},
        });
        const uri = "file:///tmp/vb-refactor-unsupported.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        await server.waitForNotification("textDocument/publishDiagnostics");

        const htmlActions = await server.request("textDocument/codeAction", {
          textDocument: { uri },
          range: {
            start: positionAt(source, htmlStart),
            end: positionAt(source, htmlEnd),
          },
          context: { diagnostics: [], only: ["refactor.extract"] },
        });
        expect(htmlActions).toEqual([]);

        const emptyActions = await server.request("textDocument/codeAction", {
          textDocument: { uri },
          range: {
            start: positionAt(source, vbStart),
            end: positionAt(source, vbStart),
          },
          context: { diagnostics: [], only: ["refactor.extract"] },
        });
        expect(emptyActions).toEqual([]);

        const whitespaceActions = await server.request("textDocument/codeAction", {
          textDocument: { uri },
          range: {
            start: positionAt(source, vbStart - 1),
            end: positionAt(source, vbEnd),
          },
          context: { diagnostics: [], only: ["refactor.extract"] },
        });
        expect(whitespaceActions).toEqual([]);

        const multilineActions = await server.request("textDocument/codeAction", {
          textDocument: { uri },
          range: {
            start: positionAt(source, vbStart),
            end: positionAt(source, multilineEnd),
          },
          context: { diagnostics: [], only: ["refactor.extract"] },
        });
        expect(multilineActions).toEqual([]);

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("returns VBScript quick fixes for unused declarations", async () => {
      const source = `<%
Dim unusedValue
Const usedValue = 1
Response.Write usedValue
%>`;
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: "file:///tmp",
          capabilities: {},
        });
        const uri = "file:///tmp/vb-unused-code-actions.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        const diagnostics = await server.waitForNotification("textDocument/publishDiagnostics");
        const vbDiagnostics = (
          diagnostics.params as { diagnostics: Array<Record<string, unknown>> }
        ).diagnostics;
        expect(JSON.stringify(vbDiagnostics)).toContain("unusedValue");
        const actions = await server.request("textDocument/codeAction", {
          textDocument: { uri },
          range: {
            start: { line: 1, character: 4 },
            end: { line: 1, character: 15 },
          },
          context: { diagnostics: vbDiagnostics },
        });
        const serialized = JSON.stringify(actions);
        expect(serialized).toContain("Remove unused declaration unusedValue");
        expect(serialized).toContain('"newText":""');

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

    it("returns VBScript naming hints and reference rename quick fixes", async () => {
      const source = `<%
Dim foo
foo = 1
Response.Write FOO
%>`;
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: "file:///tmp",
          capabilities: {},
        });
        const uri = "file:///tmp/vb-naming.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        const diagnostics = await server.waitForNotification("textDocument/publishDiagnostics");
        const namingDiagnostics = (
          diagnostics.params as { diagnostics: Array<Record<string, unknown>> }
        ).diagnostics.filter((diagnostic) => diagnostic.source === "asp-lsp-vbscript-naming");
        expect(JSON.stringify(namingDiagnostics)).toContain("Foo");

        const actions = await server.request("textDocument/codeAction", {
          textDocument: { uri },
          range: {
            start: { line: 1, character: 4 },
            end: { line: 1, character: 7 },
          },
          context: { diagnostics: namingDiagnostics },
        });
        const serialized = JSON.stringify(actions);
        expect(serialized).toContain("Rename foo to Foo");
        expect(serialized.match(/"newText":"Foo"/g)).toHaveLength(3);

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("returns VBScript naming quick fixes for configured casing styles", async () => {
      const cases = [
        { identifierCase: "upper", expectedName: "USERNAME" },
        { identifierCase: "camel", expectedName: "userName" },
        { identifierCase: "lower", expectedName: "username" },
      ] as const;
      for (const testCase of cases) {
        const source = `<%
Dim user_name
Response.Write USER_NAME
%>`;
        const server = new RpcServer();
        try {
          await server.start();
          await server.request("initialize", {
            processId: process.pid,
            rootUri: "file:///tmp",
            capabilities: {},
          });
          server.notify("workspace/didChangeConfiguration", {
            settings: { aspLsp: { vbscript: { identifierCase: testCase.identifierCase } } },
          });
          const uri = `file:///tmp/vb-naming-${testCase.identifierCase}.asp`;
          server.notify("textDocument/didOpen", {
            textDocument: {
              uri,
              languageId: "classic-asp",
              version: 1,
              text: source,
            },
          });
          const diagnostics = await server.waitForNotification("textDocument/publishDiagnostics");
          const namingDiagnostics = (
            diagnostics.params as { diagnostics: Array<Record<string, unknown>> }
          ).diagnostics.filter((diagnostic) => diagnostic.source === "asp-lsp-vbscript-naming");
          expect(JSON.stringify(namingDiagnostics)).toContain(testCase.expectedName);

          const actions = await server.request("textDocument/codeAction", {
            textDocument: { uri },
            range: {
              start: { line: 1, character: 4 },
              end: { line: 1, character: 13 },
            },
            context: { diagnostics: namingDiagnostics },
          });
          const serialized = JSON.stringify(actions);
          expect(serialized).toContain(`Rename user_name to ${testCase.expectedName}`);
          expect(
            serialized.match(new RegExp(`"newText":"${testCase.expectedName}"`, "g")),
          ).toHaveLength(2);

          await server.request("shutdown", null);
          server.notify("exit", undefined);
        } finally {
          server.stop();
        }
      }
    });

    it("returns VBScript naming quick fixes for declaration kinds", async () => {
      const source = `<%
Class customer_record
  Public customer_name
  Public Property Get display_name()
    display_name = 1
  End Property
End Class
Sub save_order(item_name)
  Response.Write item_name
End Sub
Function build_total()
  build_total = 1
End Function
Dim record
Set record = New customer_record
save_order "x"
Response.Write build_total()
Response.Write record.display_name
%>`;
      const cases = [
        { name: "customer_record", expectedName: "CustomerRecord" },
        { name: "customer_name", expectedName: "CustomerName" },
        { name: "display_name", expectedName: "DisplayName" },
        { name: "save_order", expectedName: "SaveOrder" },
        { name: "item_name", expectedName: "ItemName" },
        { name: "build_total", expectedName: "BuildTotal" },
      ];
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: "file:///tmp",
          capabilities: {},
        });
        const uri = "file:///tmp/vb-naming-declaration-kinds.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        const diagnostics = await server.waitForNotification("textDocument/publishDiagnostics");
        const namingDiagnostics = (
          diagnostics.params as { diagnostics: Array<Record<string, unknown>> }
        ).diagnostics.filter((diagnostic) => diagnostic.source === "asp-lsp-vbscript-naming");
        for (const testCase of cases) {
          const diagnostic = namingDiagnostics.find((item) =>
            JSON.stringify(item.data).includes(`"name":"${testCase.name}"`),
          );
          expect(diagnostic, testCase.name).toBeTruthy();
          expect(JSON.stringify(diagnostic)).toContain(testCase.expectedName);
          const actions = await server.request("textDocument/codeAction", {
            textDocument: { uri },
            range: diagnostic?.range,
            context: { diagnostics: [diagnostic] },
          });
          expect(JSON.stringify(actions), testCase.name).toContain(
            `Rename ${testCase.name} to ${testCase.expectedName}`,
          );
        }

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("renames VBScript class member references from naming quick fixes", async () => {
      const source = `<%
Class customer_record
  Public customer_name
  Public Property Get display_name()
    display_name = customer_name
  End Property
  Public Sub show_name()
    Response.Write Me.display_name
    Response.Write Me.customer_name
  End Sub
End Class
Dim record
Set record = New customer_record
Response.Write record.display_name
Response.Write record.customer_name
%>`;
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: "file:///tmp",
          capabilities: {},
        });
        const uri = "file:///tmp/vb-naming-member-references.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        const diagnostics = await server.waitForNotification("textDocument/publishDiagnostics");
        const namingDiagnostics = (
          diagnostics.params as { diagnostics: Array<Record<string, unknown>> }
        ).diagnostics.filter((diagnostic) => diagnostic.source === "asp-lsp-vbscript-naming");
        const propertyDiagnostic = namingDiagnostics.find((diagnostic) =>
          JSON.stringify(diagnostic.data).includes('"name":"display_name"'),
        );
        const propertyActions = await server.request("textDocument/codeAction", {
          textDocument: { uri },
          range: propertyDiagnostic?.range,
          context: { diagnostics: [propertyDiagnostic] },
        });
        expect(JSON.stringify(propertyActions).match(/"newText":"DisplayName"/g)).toHaveLength(4);

        const fieldDiagnostic = namingDiagnostics.find((diagnostic) =>
          JSON.stringify(diagnostic.data).includes('"name":"customer_name"'),
        );
        const fieldActions = await server.request("textDocument/codeAction", {
          textDocument: { uri },
          range: fieldDiagnostic?.range,
          context: { diagnostics: [fieldDiagnostic] },
        });
        expect(JSON.stringify(fieldActions)).toContain("Rename customer_name to CustomerName");
        expect(JSON.stringify(fieldActions).match(/"newText":"CustomerName"/g)).toHaveLength(3);

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("renames included VBScript references from naming quick fixes", async () => {
      const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-"));
      const owner = path.join(tempDir, "default.asp");
      const include = path.join(tempDir, "common.inc");
      fs.writeFileSync(include, `<%\nResponse.Write FOO\n%>`, "utf8");
      const source = `<!-- #include file="common.inc" -->
<%
Dim foo
%>`;
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: `file://${tempDir}`,
          capabilities: {},
        });
        const uri = `file://${owner}`;
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        const diagnostics = await server.waitForNotification("textDocument/publishDiagnostics");
        const namingDiagnostics = (
          diagnostics.params as { diagnostics: Array<Record<string, unknown>> }
        ).diagnostics.filter((diagnostic) => diagnostic.source === "asp-lsp-vbscript-naming");
        const actions = await server.request("textDocument/codeAction", {
          textDocument: { uri },
          range: {
            start: { line: 2, character: 4 },
            end: { line: 2, character: 7 },
          },
          context: { diagnostics: namingDiagnostics },
        });
        const serialized = JSON.stringify(actions);
        expect(serialized).toContain("Rename foo to Foo");
        expect(serialized).toContain("common.inc");
        expect(serialized.match(/"newText":"Foo"/g)).toHaveLength(2);

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
        fs.rmSync(tempDir, { recursive: true, force: true });
      }
    });

    it("does not return VBScript naming quick fixes when the expected name collides", async () => {
      const source = `<%
Dim foo
Dim Foo
Response.Write foo
%>`;
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: "file:///tmp",
          capabilities: {},
        });
        const uri = "file:///tmp/vb-naming-collision.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        const diagnostics = await server.waitForNotification("textDocument/publishDiagnostics");
        const namingDiagnostics = (
          diagnostics.params as { diagnostics: Array<Record<string, unknown>> }
        ).diagnostics.filter((diagnostic) => diagnostic.source === "asp-lsp-vbscript-naming");
        expect(JSON.stringify(namingDiagnostics)).toContain("Foo");

        const actions = await server.request("textDocument/codeAction", {
          textDocument: { uri },
          range: {
            start: { line: 1, character: 4 },
            end: { line: 1, character: 7 },
          },
          context: { diagnostics: namingDiagnostics },
        });
        expect(JSON.stringify(actions)).not.toContain("Rename foo to Foo");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("allows VBScript naming quick fixes when same-case names are in different scopes", async () => {
      const source = `<%
Sub First()
  Dim foo
  Response.Write foo
End Sub
Sub Second()
  Dim Foo
  Response.Write Foo
End Sub
%>`;
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: "file:///tmp",
          capabilities: {},
        });
        const uri = "file:///tmp/vb-naming-scope-collision.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        const diagnostics = await server.waitForNotification("textDocument/publishDiagnostics");
        const namingDiagnostics = (
          diagnostics.params as { diagnostics: Array<Record<string, unknown>> }
        ).diagnostics.filter((diagnostic) => diagnostic.source === "asp-lsp-vbscript-naming");
        const fooDiagnostic = namingDiagnostics.find((diagnostic) =>
          JSON.stringify(diagnostic.data).includes('"name":"foo"'),
        );
        const actions = await server.request("textDocument/codeAction", {
          textDocument: { uri },
          range: fooDiagnostic?.range,
          context: { diagnostics: [fooDiagnostic] },
        });
        const serialized = JSON.stringify(actions);
        expect(serialized).toContain("Rename foo to Foo");
        expect(serialized.match(/"newText":"Foo"/g)).toHaveLength(2);

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("does not rename VBScript identifier text in strings or comments", async () => {
      const source = `<%
Dim foo
' foo should stay in this comment
Response.Write "foo should stay in this string"
Response.Write foo
%>`;
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: "file:///tmp",
          capabilities: {},
        });
        const uri = "file:///tmp/vb-naming-string-comment.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        const diagnostics = await server.waitForNotification("textDocument/publishDiagnostics");
        const namingDiagnostics = (
          diagnostics.params as { diagnostics: Array<Record<string, unknown>> }
        ).diagnostics.filter((diagnostic) => diagnostic.source === "asp-lsp-vbscript-naming");
        const fooDiagnostic = namingDiagnostics.find((diagnostic) =>
          JSON.stringify(diagnostic.data).includes('"name":"foo"'),
        );
        const actions = await server.request("textDocument/codeAction", {
          textDocument: { uri },
          range: fooDiagnostic?.range,
          context: { diagnostics: [fooDiagnostic] },
        });
        const serialized = JSON.stringify(actions);
        expect(serialized).toContain("Rename foo to Foo");
        expect(serialized.match(/"newText":"Foo"/g)).toHaveLength(2);

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("does not report VBScript naming hints when identifier casing is ignored", async () => {
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: "file:///tmp",
          capabilities: {},
        });
        server.notify("workspace/didChangeConfiguration", {
          settings: { aspLsp: { vbscript: { identifierCase: "ignore" } } },
        });
        const uri = "file:///tmp/vb-naming-ignore.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: `<%\nDim foo\n%>`,
          },
        });
        const diagnostics = await server.waitForNotification("textDocument/publishDiagnostics");
        expect(
          (diagnostics.params as { diagnostics: Array<Record<string, unknown>> }).diagnostics.some(
            (diagnostic) => diagnostic.source === "asp-lsp-vbscript-naming",
          ),
        ).toBe(false);

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
<style>.x{color:red}</style>
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
        expect(fullText).toContain(".x {");
        expect(fullText).toContain("color: red");

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

    it("returns format edits from willSaveWaitUntil when format-on-save is enabled", async () => {
      const source = `<style>.x{color:red}</style>
<%
If enabled Then
Response.Write "ok"
End If
%>`;
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: "file:///tmp",
          capabilities: {},
        });
        server.notify("workspace/didChangeConfiguration", {
          settings: { aspLsp: { format: { onSave: true, indentSize: 2 } } },
        });
        const uri = "file:///tmp/will-save-format.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        await server.waitForNotification("textDocument/publishDiagnostics");

        const edits = await server.request("textDocument/willSaveWaitUntil", {
          textDocument: { uri },
          reason: 1,
        });
        const serialized = JSON.stringify(edits);
        expect(serialized).toContain("color: red");
        expect(serialized).toContain("  Response.Write");

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
                      Child: "Custom.Child",
                      Title: "String",
                      Ping: {
                        kind: "method",
                        returnType: "Boolean",
                        parameters: [{ name: "name", type: "String" }],
                      },
                    },
                  },
                  "Custom.Child": {
                    members: {
                      Name: "String",
                    },
                  },
                },
                globals: {
                  Repository: "Custom.Widget",
                },
              },
            },
          },
        });
        const uri = "file:///tmp/vb-type.asp";
        const source = `<%
Dim widget
Set widget = Server.CreateObject("Custom.Widget")
widget.
widget.Missing
widget.Ping("a", "b")
Repository.
%>`;
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
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

        const globalCompletions = await server.request("textDocument/completion", {
          textDocument: { uri },
          position: { line: 6, character: 11 },
        });
        expect(JSON.stringify(globalCompletions)).toContain("Title");
        expect(diagnosticText).not.toContain("Repository");

        const typeHierarchy = await server.request("textDocument/prepareTypeHierarchy", {
          textDocument: { uri },
          position: positionAt(source, source.indexOf("widget") + 2),
        });
        expect(JSON.stringify(typeHierarchy)).toContain("Custom.Widget");
        const subtypes = await server.request("typeHierarchy/subtypes", {
          item: (typeHierarchy as unknown[])[0],
        });
        expect(JSON.stringify(subtypes)).toContain("Custom.Child");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("returns VBScript type diagnostic quick fixes", async () => {
      const source = `<%
Dim widget
Dim title
' @type typedValue As Number
Dim typedValue
widget = Server.CreateObject("Custom.Widget")
Set title = "hello"
typedValue = "hello"
%>`;
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
                comTypes: { "Custom.Widget": { members: {} } },
              },
            },
          },
        });
        const uri = "file:///tmp/vb-type-fixes.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        const diagnostics = await server.waitForNotification("textDocument/publishDiagnostics");
        const typeDiagnostics = (
          diagnostics.params as { diagnostics: Array<Record<string, unknown>> }
        ).diagnostics.filter((diagnostic) => diagnostic.source === "asp-lsp-vbscript-type");
        expect(JSON.stringify(typeDiagnostics)).toContain("objectNeedsSet");
        expect(JSON.stringify(typeDiagnostics)).toContain("setScalar");
        expect(JSON.stringify(typeDiagnostics)).toContain("typeMismatch");

        const actions = await server.request("textDocument/codeAction", {
          textDocument: { uri },
          range: { start: { line: 5, character: 0 }, end: { line: 7, character: 20 } },
          context: { diagnostics: typeDiagnostics },
        });
        const serialized = JSON.stringify(actions);
        expect(serialized).toContain("Use Set for object assignment to widget");
        expect(serialized).toContain("Remove Set from scalar assignment to title");
        expect(serialized).toContain("Annotate typedValue as String");

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
  },
  rpcTimeoutMs,
);

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
      setTimeout(
        () => reject(new Error(`Timed out waiting for ${method}: ${this.stderr}`)),
        rpcTimeoutMs,
      );
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
      setTimeout(
        () => reject(new Error(`Timed out waiting for ${method}: ${this.stderr}`)),
        rpcTimeoutMs,
      );
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

function decodeSemanticTokens(data: number[] | undefined): DecodedSemanticToken[] {
  const result: DecodedSemanticToken[] = [];
  let line = 0;
  let character = 0;
  for (let index = 0; data && index + 4 < data.length; index += 5) {
    line += data[index];
    character = data[index] === 0 ? character + data[index + 1] : data[index + 1];
    result.push({
      line,
      character,
      length: data[index + 2],
      tokenType: data[index + 3],
      tokenModifiers: data[index + 4],
    });
  }
  return result;
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
