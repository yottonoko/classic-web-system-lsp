import { spawn, type ChildProcessWithoutNullStreams } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { pathToFileURL } from "node:url";
import { describe, expect, it } from "vitest";
import {
  CompletionItemKind,
  DiagnosticTag,
  DiagnosticSeverity,
  InsertTextFormat,
} from "vscode-languageserver-types";
import type { TextEdit } from "vscode-languageserver-types";

interface JsonRpcMessage {
  id?: number;
  method?: string;
  params?: unknown;
  result?: unknown;
  error?: JsonRpcError;
}

interface JsonRpcError {
  code?: number;
  message?: string;
  data?: unknown;
}

interface PendingResponse {
  method: string;
  resolve: (message: unknown) => void;
  reject: (error: Error) => void;
  timer: ReturnType<typeof setTimeout>;
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
  string: 8,
  operator: 9,
  namespace: 10,
  interface: 11,
  enum: 12,
  enumMember: 13,
  typeAlias: 14,
  typeParameter: 15,
  constant: 16,
} as const;
const semanticTokenModifier = {
  public: 1 << 0,
  private: 1 << 1,
  readonly: 1 << 2,
  library: 1 << 3,
  byref: 1 << 4,
  byval: 1 << 5,
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
        const initializeText = JSON.stringify(initialize);
        expect(initializeText).toContain("completionProvider");
        expect(initializeText).toContain('"aspLsp.server.reindexWorkspace"');
        expect(initializeText).toContain('"aspLsp.server.clearCache"');
        expect(initializeText).toContain('"aspLsp.server.clearDiskCache"');
        expect(initializeText).toContain('"aspLsp.server.clearProcessCache"');
        expect(initializeText).toContain('"aspLsp.server.buildGraph"');
        expect(initializeText).not.toContain('"aspLsp.reindexWorkspace"');
        expect(initializeText).not.toContain('"aspLsp.clearCache"');
        expect(initializeText).not.toContain('"aspLsp.clearDiskCache"');
        expect(initializeText).not.toContain('"aspLsp.clearProcessCache"');
        expect(initializeText).not.toContain("diagnosticProvider");

        const uri = "file:///tmp/default.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: `<% Option Explicit\nResponse.\nResponse.Write missingName\n%>`,
          },
        });

        const diagnostics = await waitForDiagnosticsContaining(server, "missingName");
        expect(diagnostics.method).toBe("textDocument/publishDiagnostics");
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

    it("returns VBScript member completions for partial member names over JSON-RPC", async () => {
      const document = markedDocument(`<%
Server.HTMLEe▮
%>`);
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: "file:///tmp",
          capabilities: {},
        });
        const uri = "file:///tmp/partial-member.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: document.text,
          },
        });
        await server.waitForNotification("textDocument/publishDiagnostics");

        const completions = await server.request("textDocument/completion", {
          textDocument: { uri },
          position: document.position,
        });
        const serialized = JSON.stringify(completions);
        expect(serialized).toContain("HTMLEncode");
        expect(serialized).not.toContain("If Then");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("returns configurable VBScript syntax snippet completions over JSON-RPC", async () => {
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: "file:///tmp",
          capabilities: {},
        });

        const uri = "file:///tmp/vbscript-syntax-snippets.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: `<% Option Explicit

%>`,
          },
        });
        await server.waitForNotification("textDocument/publishDiagnostics");

        const completions = await server.request("textDocument/completion", {
          textDocument: { uri },
          position: { line: 1, character: 0 },
        });
        const ifSnippet = completionItems(completions).find((item) => item.label === "If Then");
        expect(ifSnippet).toMatchObject({
          kind: CompletionItemKind.Snippet,
          insertTextFormat: InsertTextFormat.Snippet,
        });
        expect(String(ifSnippet?.insertText)).toContain("End If");

        server.notify("workspace/didChangeConfiguration", {
          settings: { aspLsp: { vbscript: { syntaxSnippets: false } } },
        });
        const disabledCompletions = await server.request("textDocument/completion", {
          textDocument: { uri },
          position: { line: 1, character: 0 },
        });
        const disabledLabels = completionLabels(disabledCompletions);
        expect(disabledLabels).not.toContain("If Then");
        expect(disabledLabels).toContain("Response");
        expect(disabledLabels).toContain("Dim");

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

    it("returns CSS completions and colors inside HTML style attributes", async () => {
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: "file:///tmp",
          capabilities: {},
        });
        const uri = "file:///tmp/css-style-attribute.asp";
        const marked = markedDocument('<div style="colo▮; background: #ff0000"></div>');
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
        expect(completionLabels(completions)).toContain("color");

        const colors = (await server.request("textDocument/documentColor", {
          textDocument: { uri },
        })) as Array<{ range: { start: { line: number; character: number } }; color: unknown }>;
        expect(colors).toHaveLength(1);
        expect(colors[0].range.start).toEqual({
          line: 0,
          character: marked.text.indexOf("#ff0000"),
        });

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

    it("maps CSS completion, hover and style close tag positions back to ASP source", async () => {
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: "file:///tmp",
          capabilities: {},
        });
        const cssDocument = markedDocument(`<style>
.card { colo▮r: red; }
</style>`);
        const uri = "file:///tmp/css-source-map.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: cssDocument.text,
          },
        });
        await server.waitForNotification("textDocument/publishDiagnostics");

        const completions = await server.request("textDocument/completion", {
          textDocument: { uri },
          position: cssDocument.position,
        });
        const colorCompletion = completionItems(completions).find((item) => item.label === "color");
        expect(colorCompletion).toBeTruthy();
        expect(completionEditRange(colorCompletion)?.start.line).toBe(1);

        const hover = await server.request("textDocument/hover", {
          textDocument: { uri },
          position: cssDocument.position,
        });
        expect(JSON.stringify(hover)).toContain("Sets the color");
        expect((hover as { range?: { start: { line: number } } }).range?.start.line).toBe(1);

        const closeTagDocument = markedDocument(`<style>
.card { color: red; }
</▮style>`);
        const closeTagUri = "file:///tmp/css-close-tag.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri: closeTagUri,
            languageId: "classic-asp",
            version: 1,
            text: closeTagDocument.text,
          },
        });
        await server.waitForNotification("textDocument/publishDiagnostics");
        const closeTagCompletions = await server.request("textDocument/completion", {
          textDocument: { uri: closeTagUri },
          position: closeTagDocument.position,
        });
        expect(completionLabels(closeTagCompletions)).toContain("/style");

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

    it("returns rich JavaScript type info and semantic tokens for script tags", async () => {
      const source = `<script>
/** @param {HTMLElement} element */
function activate(element) {
  element.dataset.active = "true";
}
class DashboardWidget {
  render(row) {
    return row.textContent;
  }
}
const formatter = new Intl.DateTimeFormat("en");
const clock = document.querySelector("#clientClock");
document.querySelectorAll(".customer-row").forEach((row) => {
  activate(row);
});
</script>`;
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: "file:///tmp",
          capabilities: {},
        });
        const uri = "file:///tmp/js-rich-types.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        await server.waitForNotification("textDocument/publishDiagnostics");

        const formatterHover = await server.request("textDocument/hover", {
          textDocument: { uri },
          position: positionAt(source, source.indexOf("formatter")),
        });
        expect(JSON.stringify(formatterHover)).toContain("Intl.DateTimeFormat");
        const clockHover = await server.request("textDocument/hover", {
          textDocument: { uri },
          position: positionAt(source, source.indexOf("clock")),
        });
        expect(JSON.stringify(clockHover)).toContain("Element");
        const rowHover = await server.request("textDocument/hover", {
          textDocument: { uri },
          position: positionAt(source, source.indexOf("forEach((row)") + "forEach((".length),
        });
        expect(JSON.stringify(rowHover)).toContain("Element");
        const elementHover = await server.request("textDocument/hover", {
          textDocument: { uri },
          position: positionAt(source, source.indexOf("element)")),
        });
        expect(JSON.stringify(elementHover)).toContain("HTMLElement");

        const semanticTokens = await server.request("textDocument/semanticTokens/full", {
          textDocument: { uri },
        });
        const decoded = decodeSemanticTokens((semanticTokens as { data?: number[] }).data);
        expect(
          decoded.some((token) =>
            tokenMatches(source, token, "DashboardWidget", semanticTokenType.class),
          ),
        ).toBe(true);
        expect(
          decoded.some((token) => tokenMatches(source, token, "render", semanticTokenType.method)),
        ).toBe(true);
        expect(
          decoded.some((token) =>
            tokenMatches(source, token, "(row)", semanticTokenType.parameter, 1),
          ),
        ).toBe(true);
        expect(
          decoded.some((token) =>
            tokenMatches(source, token, "dataset", semanticTokenType.property),
          ),
        ).toBe(true);

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("treats root script tags between ASP procedure blocks as JavaScript", async () => {
      const source = `<% Sub A() %>
<script>
const aValue = 10;
let bValue = aValue + 1;
console.log(aValue, bValue);
missingThing();
</script>
<% End Sub %>`;
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
              checkJs: true,
              diagnostics: { debounceMs: 0 },
              javascript: { ignoreProjectConfig: true },
            },
          },
        });
        const uri = "file:///tmp/root-sub-script.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        const diagnostics = await waitForDiagnosticsContaining(server, "missingThing");
        expect(diagnosticText(diagnostics)).toContain("asp-lsp-typescript");

        const completions = await server.request("textDocument/completion", {
          textDocument: { uri },
          position: positionAt(source, source.indexOf("console.") + "console.".length),
        });
        expect(completionLabels(completions)).toContain("log");

        const hover = await server.request("textDocument/hover", {
          textDocument: { uri },
          position: positionAt(source, source.indexOf("aValue")),
        });
        expect(JSON.stringify(hover)).toContain("const aValue: 10");

        const semanticTokens = await server.request("textDocument/semanticTokens/full", {
          textDocument: { uri },
        });
        const decoded = decodeSemanticTokens((semanticTokens as { data?: number[] }).data);
        expect(
          decoded.some((token) =>
            tokenMatches(source, token, "aValue", semanticTokenType.variable),
          ),
        ).toBe(true);

        const formatSource = source
          .replace("const aValue = 10;", "const aValue=10;")
          .replace("let bValue = aValue + 1;", "let bValue=aValue+1;");
        server.notify("textDocument/didChange", {
          textDocument: { uri, version: 2 },
          contentChanges: [{ text: formatSource }],
        });
        await waitForDiagnosticsContaining(server, "missingThing");

        const fullEdits = (await server.request("textDocument/formatting", {
          textDocument: { uri },
          options: { tabSize: 2, insertSpaces: true },
        })) as TextEdit[];
        const formatted =
          fullEdits.length === 1 ? applyTextEdit(formatSource, fullEdits[0]) : formatSource;
        expect(formatted).toContain("const aValue = 10;");
        expect(formatted).toContain("let bValue = aValue + 1;");

        const rangeEdits = (await server.request("textDocument/rangeFormatting", {
          textDocument: { uri },
          range: {
            start: positionAt(formatSource, formatSource.indexOf("const aValue")),
            end: positionAt(formatSource, formatSource.indexOf("console.log")),
          },
          options: { tabSize: 2, insertSpaces: true },
        })) as TextEdit[];
        const rangeFormatted =
          rangeEdits.length === 1 ? applyTextEdit(formatSource, rangeEdits[0]) : formatSource;
        expect(rangeFormatted).toContain("const aValue = 10;");
        expect(rangeFormatted).toContain("let bValue = aValue + 1;");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("defers full JavaScript semantic tokens for large script tags while range stays immediate", async () => {
      const filler = " ".repeat(512);
      const source = `<script>
${filler}
class LargeDashboardWidget {
  render(row) {
    return row.textContent;
  }
}
</script>`;
      const server = new RpcServer({
        env: {
          NODE_ENV: "test",
          ASP_LSP_TEST_SEMANTIC_TOKENS_LARGE_JAVASCRIPT_THRESHOLD: "128",
        },
      });
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
              debug: { output: "verbose" },
              diagnostics: { debounceMs: 0 },
            },
          },
        });
        const uri = "file:///tmp/js-large-semantic.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        await server.waitForNotification("textDocument/publishDiagnostics");

        const classPosition = positionAt(source, source.indexOf("LargeDashboardWidget"));
        const rangeTokens = await server.request("textDocument/semanticTokens/range", {
          textDocument: { uri },
          range: {
            start: { line: classPosition.line, character: 0 },
            end: { line: classPosition.line + 4, character: 0 },
          },
        });
        const decodedRange = decodeSemanticTokens((rangeTokens as { data?: number[] }).data);
        expect(
          decodedRange.some((token) =>
            tokenMatches(source, token, "LargeDashboardWidget", semanticTokenType.class),
          ),
        ).toBe(true);
        expect(
          decodedRange.some((token) =>
            tokenMatches(source, token, "render", semanticTokenType.method),
          ),
        ).toBe(true);

        const firstFull = await server.request("textDocument/semanticTokens/full", {
          textDocument: { uri },
        });
        const decodedFirstFull = decodeSemanticTokens((firstFull as { data?: number[] }).data);
        expect(
          decodedFirstFull.some((token) =>
            tokenMatches(source, token, "LargeDashboardWidget", semanticTokenType.class),
          ),
        ).toBe(false);
        const decodedSecondFull = await waitForSemanticTokenAsync(
          server,
          uri,
          source,
          "LargeDashboardWidget",
          semanticTokenType.class,
        );
        expect(
          decodedSecondFull.some((token) =>
            tokenMatches(source, token, "LargeDashboardWidget", semanticTokenType.class),
          ),
        ).toBe(true);
        expect(
          decodedSecondFull.some((token) =>
            tokenMatches(source, token, "render", semanticTokenType.method),
          ),
        ).toBe(true);

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("keeps browser JavaScript globals available without a project config", async () => {
      const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-empty-js-"));
      const source = `<script>
const clock = document.querySelector("#clientClock");
const formatter = new Intl.DateTimeFormat("en");
</script>`;
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: `file://${tempDir}`,
          capabilities: {},
        });
        server.notify("workspace/didChangeConfiguration", {
          settings: { aspLsp: { checkJs: true, diagnostics: { debounceMs: 0 } } },
        });
        const uri = `file://${path.join(tempDir, "default.asp")}`;
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        await server.waitForNotification("textDocument/publishDiagnostics");

        const pulled = await server.request("textDocument/diagnostic", {
          textDocument: { uri },
        });
        const serialized = JSON.stringify(pulled);
        expect(serialized).not.toContain("Cannot find name 'document'");
        expect(serialized).not.toContain("Cannot find name 'Intl'");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
        fs.rmSync(tempDir, { recursive: true, force: true });
      }
    });

    it("keeps browser JavaScript libs when project config disables default libs", async () => {
      const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-js-nolib-"));
      fs.writeFileSync(
        path.join(tempDir, "jsconfig.json"),
        JSON.stringify({ compilerOptions: { checkJs: true, noLib: true, lib: [] } }),
        "utf8",
      );
      const source = `<script>
document.querySelector("#clientClock");
new Intl.DateTimeFormat("en");
</script>`;
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: `file://${tempDir}`,
          capabilities: {},
        });
        server.notify("workspace/didChangeConfiguration", {
          settings: { aspLsp: { checkJs: true, diagnostics: { debounceMs: 0 } } },
        });
        const uri = `file://${path.join(tempDir, "default.asp")}`;
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        await server.waitForNotification("textDocument/publishDiagnostics");

        const pulled = await server.request("textDocument/diagnostic", {
          textDocument: { uri },
        });
        const serialized = JSON.stringify(pulled);
        expect(serialized).not.toContain("Cannot find name 'document'");
        expect(serialized).not.toContain("Cannot find name 'Intl'");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
        fs.rmSync(tempDir, { recursive: true, force: true });
      }
    });

    it("keeps embedded JavaScript browser-focused when Node ambient types exist", async () => {
      const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-js-browser-"));
      const nodeTypes = path.join(tempDir, "node_modules", "@types", "node");
      const jqueryTypes = path.join(tempDir, "node_modules", "@types", "jquery");
      fs.mkdirSync(nodeTypes, { recursive: true });
      fs.mkdirSync(jqueryTypes, { recursive: true });
      fs.writeFileSync(
        path.join(nodeTypes, "package.json"),
        JSON.stringify({ name: "@types/node", version: "1.0.0", types: "index.d.ts" }),
        "utf8",
      );
      fs.writeFileSync(
        path.join(nodeTypes, "index.d.ts"),
        "declare var __dirname: string;\ndeclare var __filename: string;\n",
        "utf8",
      );
      fs.writeFileSync(
        path.join(jqueryTypes, "package.json"),
        JSON.stringify({ name: "@types/jquery", version: "1.0.0", types: "index.d.ts" }),
        "utf8",
      );
      fs.writeFileSync(
        path.join(jqueryTypes, "index.d.ts"),
        "declare const $: { ready(callback: () => void): void };\n",
        "utf8",
      );
      fs.writeFileSync(
        path.join(tempDir, "jsconfig.json"),
        JSON.stringify({
          compilerOptions: {
            types: ["node", "jquery"],
            module: "ESNext",
            moduleResolution: "Bundler",
          },
        }),
        "utf8",
      );
      const nodeMarked = markedDocument(`<script>
__▮
</script>`);
      const domMarked = markedDocument(`<script>
docu▮
</script>`);
      const jqueryMarked = markedDocument(`<script>
$▮
</script>`);
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
            text: nodeMarked.text,
          },
        });
        await server.waitForNotification("textDocument/publishDiagnostics");
        const nodeCompletions = await server.request("textDocument/completion", {
          textDocument: { uri },
          position: nodeMarked.position,
        });
        expect(completionLabels(nodeCompletions)).not.toContain("__dirname");
        expect(completionLabels(nodeCompletions)).not.toContain("__filename");

        server.notify("textDocument/didChange", {
          textDocument: { uri, version: 2 },
          contentChanges: [{ text: domMarked.text }],
        });
        await server.waitForNotification("textDocument/publishDiagnostics");
        const domCompletions = await server.request("textDocument/completion", {
          textDocument: { uri },
          position: domMarked.position,
        });
        expect(completionLabels(domCompletions)).toContain("document");

        server.notify("textDocument/didChange", {
          textDocument: { uri, version: 3 },
          contentChanges: [{ text: jqueryMarked.text }],
        });
        await server.waitForNotification("textDocument/publishDiagnostics");
        const jqueryCompletions = await server.request("textDocument/completion", {
          textDocument: { uri },
          position: jqueryMarked.position,
        });
        expect(completionLabels(jqueryCompletions)).toContain("$");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
        fs.rmSync(tempDir, { recursive: true, force: true });
      }
    });

    it("can ignore JavaScript project config files", async () => {
      const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-js-ignore-config-"));
      fs.writeFileSync(
        path.join(tempDir, "jsconfig.json"),
        JSON.stringify({ compilerOptions: { lib: ["es5"] } }),
        "utf8",
      );
      const marked = markedDocument(`<script>
docu▮
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
          settings: {
            aspLsp: {
              javascript: { ignoreProjectConfig: true },
              diagnostics: { debounceMs: 0 },
            },
          },
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
        expect(completionLabels(completions)).toContain("document");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
        fs.rmSync(tempDir, { recursive: true, force: true });
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
      fs.writeFileSync(
        path.join(tempDir, "jsconfig.json"),
        JSON.stringify({ include: ["*.js", "*.asp"] }),
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

    it("returns VBScript folding ranges for If branches and loops", async () => {
      const source = `<%
If ready Then
  Response.Write 1
ElseIf other Then
  Response.Write 2
Else
  Response.Write 3
End If
If inlineReady Then Response.Write inlineReady
Do While ready
  Response.Write 4
Loop
While ready
  Response.Write 5
Wend
For index = 1 To 3
  Response.Write index
Next
For Each item In items
  Response.Write item
Next
%>`;
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: "file:///tmp",
          capabilities: {},
        });
        const uri = "file:///tmp/vbscript-folding.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        await server.waitForNotification("textDocument/publishDiagnostics");
        const folding = (await server.request("textDocument/foldingRange", {
          textDocument: { uri },
        })) as Array<{ startLine: number; endLine: number }>;
        const ranges = folding.map((range) => [range.startLine, range.endLine]);
        expect(ranges).toContainEqual([1, 2]);
        expect(ranges).toContainEqual([3, 4]);
        expect(ranges).toContainEqual([5, 7]);
        expect(ranges).toContainEqual([9, 11]);
        expect(ranges).toContainEqual([12, 14]);
        expect(ranges).toContainEqual([15, 17]);
        expect(ranges).toContainEqual([18, 20]);
        expect(ranges.some(([startLine]) => startLine === 8)).toBe(false);

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("keeps remapped selection range parents containing child ranges", async () => {
      const source = `<style>
.panel { <% If ok Then %>color: red;<% End If %> }
</style>
<script>function boot(){ return <%= value %>; }</script>`;
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: "file:///tmp",
          capabilities: {},
        });
        const uri = "file:///tmp/selection-ranges.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        await server.waitForNotification("textDocument/publishDiagnostics");

        const selection = await server.request("textDocument/selectionRange", {
          textDocument: { uri },
          positions: [
            positionAt(source, source.indexOf("color")),
            positionAt(source, source.indexOf("return")),
          ],
        });
        expectSelectionRangesToBeNested(selection);

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
        const diagnostics = await waitForDiagnosticsContaining(server, "asp-lsp-css");
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

    it("reuses CSS context across diagnostics and CSS feature requests", async () => {
      const marked = markedDocument(`<style>
.card { color: red; }
.broken { color: }
.next { colo▮ }
</style>`);
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
              debug: { output: "verbose" },
              diagnostics: { debounceMs: 0 },
            },
          },
        });

        const uri = "file:///tmp/css-context-cache.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: marked.text,
          },
        });
        const diagnostics = await waitForDiagnosticsContaining(server, "asp-lsp-css");
        const cssDiagnostic = (
          (
            diagnostics.params as {
              diagnostics?: Array<{
                source?: string;
                range: { start: { line: number; character: number } };
              }>;
            }
          )?.diagnostics ?? []
        ).find((diagnostic) => diagnostic.source === "asp-lsp-css");
        expect(cssDiagnostic?.range.start.line).toBe(2);
        await waitForLogContaining(server, "css.context.create");
        server.takePendingNotifications("window/logMessage");

        const hover = await server.request("textDocument/hover", {
          textDocument: { uri },
          position: positionAt(marked.text, marked.text.indexOf("color: red") + 1),
        });
        expect(JSON.stringify(hover)).toContain("Sets the color");
        expect((hover as { range?: { start: { line: number } } }).range?.start.line).toBe(1);
        await waitForLogContaining(server, "css.context.reuse");
        server.takePendingNotifications("window/logMessage");

        const completions = await server.request("textDocument/completion", {
          textDocument: { uri },
          position: marked.position,
        });
        const colorItem = completionItems(completions).find((item) => item.label === "color");
        expect(colorItem).toBeDefined();
        expect(completionEditRange(colorItem)?.start.line).toBe(3);
        await waitForLogContaining(server, "css.context.reuse");
        server.takePendingNotifications("window/logMessage");

        const noCssUri = "file:///tmp/css-context-cache-no-css.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri: noCssUri,
            languageId: "classic-asp",
            version: 1,
            text: `<% Response.Write "ok" %>`,
          },
        });
        await waitForDiagnosticsPublished(server, noCssUri);
        const firstColors = await server.request("textDocument/documentColor", {
          textDocument: { uri: noCssUri },
        });
        const nextColors = await server.request("textDocument/documentColor", {
          textDocument: { uri: noCssUri },
        });
        expect(firstColors).toEqual([]);
        expect(nextColors).toEqual([]);
        expect(JSON.stringify(server.takePendingNotifications("window/logMessage"))).not.toContain(
          "css.context.",
        );

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("keeps JavaScript diagnostics stable when virtual file names are normalized", async () => {
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
              checkJs: true,
              diagnostics: { debounceMs: 0 },
              javascript: { ignoreProjectConfig: true },
            },
          },
        });

        const uri = "relative/a.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: `<script>const title = document.title;</script>`,
          },
        });

        await server.waitForNotification("textDocument/publishDiagnostics");
        const diagnostics = await server.request("textDocument/diagnostic", {
          textDocument: { uri },
        });
        expect(JSON.stringify(diagnostics)).not.toContain("Could not find source file");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("skips unreadable workspace directories when building JavaScript projects", async () => {
      const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-unreadable-"));
      const unreadableDir = path.join(tempDir, "blocked");
      fs.mkdirSync(unreadableDir);
      let restoreUnreadableDir = false;
      try {
        fs.chmodSync(unreadableDir, 0o000);
        restoreUnreadableDir = true;
      } catch {
        restoreUnreadableDir = false;
      }

      const marked = markedDocument("<script>const safeName = 1; safe▮</script>");
      const uri = pathToFileURL(path.join(tempDir, "page.asp")).toString();
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: pathToFileURL(tempDir).toString(),
          capabilities: {},
        });
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
        expect(completionLabels(completions)).toContain("safeName");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
        if (restoreUnreadableDir) {
          fs.chmodSync(unreadableDir, 0o700);
        }
        fs.rmSync(tempDir, { recursive: true, force: true });
      }
    });

    it("keeps ASP delimiters inside CSS and JavaScript from producing embedded diagnostics", async () => {
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: "file:///tmp",
          capabilities: {},
        });
        server.notify("workspace/didChangeConfiguration", {
          settings: { aspLsp: { diagnostics: { debounceMs: 0 } } },
        });

        const uri = "file:///tmp/embedded-asp-islands.asp";
        const source = `<style>
.card-<%= className %> { color: <%= themeColor %>; width: <% Response.Write width %>px; }
</style>
<div style="color: <%= themeColor %>; background: red"></div>
<input type="checkbox" name="inactive" value="1" <%= CheckedAttribute(filter.IncludeInactive) %>>
<input title='<%= Response.Write("x") %>'>
<input <%= Response.Write("disabled") %> data-state="<%= Response.Write("active") %>">
<script>
const clientValue = <%= serverValue %>;
const label = "<%= serverLabel %>";
const fromServer = <% Response.Write clientValue %>;
const n = "<%= RenderTierOptions(selectedTier: filter.Tier) %>";
const selectedTierValue = "gold";
function markTier(tier) { return tier; }
markTier(selectedTierValue);
console.log(label, fromServer, client, document.querySelector(".card"));
</script>`;
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        await server.waitForNotification("textDocument/publishDiagnostics");

        const pulled = await server.request("textDocument/diagnostic", {
          textDocument: { uri },
        });
        const serialized = JSON.stringify(pulled);
        expect(serialized).not.toContain("asp-lsp-css");
        expect(serialized).not.toContain("asp-lsp-typescript");
        expect(serialized).not.toContain("asp-lsp-html");

        const cssCompletions = await server.request("textDocument/completion", {
          textDocument: { uri },
          position: positionAt(source, source.indexOf("backg") + "backg".length),
        });
        expect(completionLabels(cssCompletions)).toContain("background");

        const jsCompletions = await server.request("textDocument/completion", {
          textDocument: { uri },
          position: positionAt(
            source,
            source.indexOf("client", source.indexOf("console.log")) + "client".length,
          ),
        });
        expect(completionLabels(jsCompletions)).toContain("clientValue");

        const aspCompletions = await server.request("textDocument/completion", {
          textDocument: { uri },
          position: positionAt(source, source.indexOf("Response.") + "Response.".length),
        });
        expect(completionLabels(aspCompletions)).toContain("Write");

        const tagAspCompletions = await server.request("textDocument/completion", {
          textDocument: { uri },
          position: positionAt(
            source,
            source.indexOf("Response.", source.indexOf("<input title")) + "Response.".length,
          ),
        });
        expect(completionLabels(tagAspCompletions)).toContain("Write");

        const bareTagAspCompletions = await server.request("textDocument/completion", {
          textDocument: { uri },
          position: positionAt(
            source,
            source.indexOf("Response.", source.indexOf("<input <%=")) + "Response.".length,
          ),
        });
        expect(completionLabels(bareTagAspCompletions)).toContain("Write");

        const inlayHints = await server.request("textDocument/inlayHint", {
          textDocument: { uri },
          range: { start: { line: 0, character: 0 }, end: positionAt(source, source.length) },
        });
        const serializedInlayHints = JSON.stringify(inlayHints);
        expect(serializedInlayHints).not.toContain("selectedTier");
        expect(serializedInlayHints).toContain("tier:");

        let editedSource = notifyRangedReplacement(
          server,
          uri,
          source,
          2,
          'const n = "<%=',
          "const n = <%=",
        );
        editedSource = notifyRangedReplacement(
          server,
          uri,
          editedSource,
          3,
          "const n = <%=",
          'const n = "<%=',
        );
        const editedInlayHints = await server.request("textDocument/inlayHint", {
          textDocument: { uri },
          range: {
            start: { line: 0, character: 0 },
            end: positionAt(editedSource, editedSource.length),
          },
        });
        const serializedEditedInlayHints = JSON.stringify(editedInlayHints);
        expect(serializedEditedInlayHints).not.toContain("selectedTier");
        expect(serializedEditedInlayHints).toContain("tier:");

        const semanticTokens = await server.request("textDocument/semanticTokens/full", {
          textDocument: { uri },
        });
        const decoded = decodeSemanticTokens((semanticTokens as { data?: number[] }).data);
        const selectedTier = positionAt(source, source.indexOf("selectedTier"));
        expect(
          decoded.some(
            (token) =>
              token.line === selectedTier.line &&
              token.character === selectedTier.character &&
              token.tokenType === semanticTokenType.parameter,
          ),
        ).toBe(false);
        expect(
          decoded.some((token) =>
            tokenMatches(source, token, "querySelector", semanticTokenType.method),
          ),
        ).toBe(true);
        expect(
          decoded.some((token) => tokenMatches(source, token, "color", semanticTokenType.property)),
        ).toBe(true);
        const styleAspExpression = positionAt(
          source,
          source.indexOf("<%=", source.indexOf("color")),
        );
        expect(
          decoded.some(
            (token) =>
              token.line === styleAspExpression.line &&
              token.character === styleAspExpression.character &&
              token.tokenType === semanticTokenType.keyword,
          ),
        ).toBe(true);
        const scriptAspExpression = positionAt(
          source,
          source.indexOf("<%=", source.indexOf("const clientValue")),
        );
        expect(
          decoded.some(
            (token) =>
              token.line === scriptAspExpression.line &&
              token.character === scriptAspExpression.character &&
              token.tokenType === semanticTokenType.keyword,
          ),
        ).toBe(true);

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("debounces diagnostics after rapid text changes and publishes only the latest version", async () => {
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: "file:///tmp",
          capabilities: {},
        });
        server.notify("workspace/didChangeConfiguration", {
          settings: { aspLsp: { diagnostics: { debounceMs: 80 } } },
        });

        const uri = "file:///tmp/debounced-diagnostics.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: `<% Option Explicit
Dim known
Response.Write known
%>`,
          },
        });
        await server.waitForNotification("textDocument/publishDiagnostics");

        server.notify("textDocument/didChange", {
          textDocument: { uri, version: 2 },
          contentChanges: [{ text: `<% Option Explicit\nResponse.Write staleName\n%>` }],
        });
        server.notify("textDocument/didChange", {
          textDocument: { uri, version: 3 },
          contentChanges: [{ text: `<% Option Explicit\nResponse.Write finalName\n%>` }],
        });

        const diagnostics = await waitForDiagnosticsContaining(server, "finalName");
        const serialized = JSON.stringify(diagnostics.params);
        expect(serialized).toContain("finalName");
        expect(serialized).not.toContain("staleName");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("debounces slow diagnostics after rapid text changes while publishing fast diagnostics", async () => {
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
              debug: { output: "verbose" },
              diagnostics: { debounceMs: 80 },
            },
          },
        });

        const uri = "file:///tmp/debounced-analysis.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: `<% Option Explicit
Dim known
Response.Write known
%>`,
          },
        });
        await server.waitForNotification("textDocument/publishDiagnostics");
        await waitForLogContaining(server, "LSP check completed");
        server.takePendingNotifications("window/logMessage");

        server.notify("textDocument/didChange", {
          textDocument: { uri, version: 2 },
          contentChanges: [{ text: `<% Option Explicit\nResponse.Write staleName\n%>` }],
        });
        server.notify("textDocument/didChange", {
          textDocument: { uri, version: 3 },
          contentChanges: [{ text: `<% Option Explicit\nResponse.Write finalName\n%>` }],
        });

        const diagnostics = await waitForDiagnosticsContaining(server, "finalName");
        const serializedDiagnostics = JSON.stringify(diagnostics.params);
        expect(serializedDiagnostics).toContain("finalName");
        expect(serializedDiagnostics).not.toContain("staleName");

        const checkLog = await waitForLogContaining(server, "LSP check completed");
        const serializedLogs = JSON.stringify([
          checkLog,
          ...server.takePendingNotifications("window/logMessage"),
        ]);
        expect(countOccurrences(serializedLogs, "LSP analysis started")).toBe(2);
        expect(serializedLogs).toContain("LSP analysis completed");
        expect(countOccurrences(serializedLogs, "diagnostics.fast.published")).toBe(2);
        expect(countOccurrences(serializedLogs, "diagnostics.final.published")).toBe(1);
        expect(countOccurrences(serializedLogs, "LSP check completed")).toBe(1);

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("refreshes pending changes before hover during diagnostic debounce", async () => {
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
              diagnostics: { debounceMs: 1000 },
            },
          },
        });

        const uri = "file:///tmp/hover-before-debounce.asp";
        let source = `<% Response.Write CStr(1) %>`;
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        await server.waitForNotification("textDocument/publishDiagnostics");

        source = notifyRangedReplacement(server, uri, source, 2, "CStr", "Date");
        const hover = await server.request("textDocument/hover", {
          textDocument: { uri },
          position: positionAt(source, source.indexOf("Date")),
        });

        const serialized = JSON.stringify(hover);
        expect(serialized).toContain("Date()");
        expect(serialized).not.toContain("CStr(value)");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("publishes diagnostics immediately when debounce is disabled", async () => {
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
              diagnostics: { debounceMs: 0 },
              inlayHints: { globalVariableMarkers: "all" },
            },
          },
        });

        const uri = "file:///tmp/immediate-diagnostics.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: `<% Option Explicit
Dim known
Response.Write known
%>`,
          },
        });
        await server.waitForNotification("textDocument/publishDiagnostics");

        server.notify("textDocument/didChange", {
          textDocument: { uri, version: 2 },
          contentChanges: [{ text: `<% Option Explicit\nResponse.Write immediateName\n%>` }],
        });

        await waitForDiagnosticsContaining(server, "immediateName");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("logs detailed document change timing steps in verbose debug output", async () => {
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
              debug: { output: "verbose" },
              diagnostics: { debounceMs: 0 },
            },
          },
        });

        const uri = "file:///tmp/document-change-timing.asp";
        let source = `<% Option Explicit
' benchmark x
Dim known
Response.Write known
%>`;
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        await server.waitForNotification("textDocument/publishDiagnostics");
        await waitForLogContaining(server, "LSP check completed");
        server.takePendingNotifications("window/logMessage");

        source = notifyRangedReplacement(
          server,
          uri,
          source,
          2,
          "Response.Write known",
          "Response.Write known2",
        );

        await server.waitForNotification("textDocument/publishDiagnostics");
        await waitForLogContaining(server, "LSP analysis completed");
        await waitForLogContaining(server, "analysis.parse.incremental");
        await waitForLogContaining(server, "documentChange.scheduleDiagnostics");
        await waitForLogContaining(server, "check.parserDiagnostics");
        await waitForLogContaining(server, "check.vbscript.diagnostics");
        await waitForLogContaining(server, "LSP check completed");
        expect(source).toContain("Response.Write known2");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("falls back to skeleton parsing for boundary-sensitive document edits", async () => {
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
              debug: { output: "verbose" },
              diagnostics: { debounceMs: 0 },
            },
          },
        });

        const uri = "file:///tmp/boundary-edit-fallback.asp";
        let source = `<div>safe</div>`;
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        await server.waitForNotification("textDocument/publishDiagnostics");
        await waitForLogContaining(server, "LSP check completed");
        server.takePendingNotifications("window/logMessage");

        source = notifyRangedReplacement(server, uri, source, 0, "safe", "safe <%");

        await server.waitForNotification("textDocument/publishDiagnostics");
        await waitForLogContaining(server, "analysis.parse.skeleton");
        const impactLog = await waitForLogContaining(server, "analysis.parse.impact");
        expect(JSON.stringify(impactLog.params)).toContain("boundary text inserted");
        expect(source).toContain("safe <%");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("does not treat full document replacements as incremental edits", async () => {
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
              debug: { output: "verbose" },
              diagnostics: { debounceMs: 0 },
            },
          },
        });

        const uri = "file:///tmp/full-replacement-fallback.asp";
        const source = `<div>safe</div>`;
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        await server.waitForNotification("textDocument/publishDiagnostics");
        await waitForLogContaining(server, "LSP check completed");
        server.takePendingNotifications("window/logMessage");

        server.notify("textDocument/didChange", {
          textDocument: { uri, version: 2 },
          contentChanges: [{ text: `<div>changed</div>` }],
        });

        await server.waitForNotification("textDocument/publishDiagnostics");
        await waitForLogContaining(server, "analysis.parse.skeleton");
        const impactLog = await waitForLogContaining(server, "analysis.parse.impact");
        const logs = JSON.stringify([
          impactLog,
          ...server.takePendingNotifications("window/logMessage"),
        ]);
        expect(logs).toContain("full document replacement");
        expect(logs).not.toContain("analysis.parse.incremental");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("reuses VBScript diagnostics after ordinary VBScript comment edits", async () => {
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
              debug: { output: "verbose" },
              diagnostics: { debounceMs: 0 },
            },
          },
        });

        const uri = "file:///tmp/vb-reuse-after-comment-edit.asp";
        let source = `<% Option Explicit
' benchmark x
Dim known
Response.Write missingName
%>`;
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        await waitForDiagnosticsContaining(server, "missingName");
        await waitForLogContaining(server, "LSP check completed");
        server.takePendingNotifications("window/logMessage");
        server.takePendingNotifications("textDocument/publishDiagnostics");

        source = notifyRangedReplacement(server, uri, source, 2, "benchmark x", "benchmark y");

        await waitForDiagnosticsContaining(server, "missingName");
        const reuseLog = await waitForLogContaining(server, "analysis.vbscript.reuse");
        const diagnosticsReuseLog = await waitForLogContaining(
          server,
          "check.vbscript.diagnostics.reuse",
        );
        const logs = JSON.stringify([
          reuseLog,
          diagnosticsReuseLog,
          ...server.takePendingNotifications("window/logMessage"),
        ]);
        expect(logs).not.toContain("analysis.vbscript.hydrate");
        expect(logs).not.toContain("check.vbscript.diagnostics.symbols");
        expect(logs).not.toContain("check.vbscript.projectContext");
        expect(logs).not.toContain("projectUpdate.scheduled");
        expect(source).toContain("' benchmark y");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("reuses and shifts HTML and CSS diagnostics after VBScript edits", async () => {
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
              debug: { output: "verbose" },
              diagnostics: { debounceMs: 0 },
            },
          },
        });

        const uri = "file:///tmp/embedded-diagnostics-reuse-after-vb-edit.asp";
        let source = `<%
Dim value
Response.Write value
%>
<style>
.broken { color: }
</style>`;
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        const firstDiagnostics = await waitForDiagnosticsContaining(server, "asp-lsp-css");
        await waitForLogContaining(server, "LSP check completed");
        const firstCss = diagnosticFromSource(firstDiagnostics, "asp-lsp-css");
        expect(firstCss?.range.start.line).toBe(5);
        server.takePendingNotifications("window/logMessage");
        server.takePendingNotifications("textDocument/publishDiagnostics");

        source = notifyRangedReplacement(
          server,
          uri,
          source,
          2,
          "Dim value",
          "Dim value\nDim nextValue",
        );

        const nextDiagnostics = await waitForDiagnosticsContaining(server, "asp-lsp-css");
        const htmlReuseLog = await waitForLogContaining(server, "htmlDiagnostics.reuse");
        const cssReuseLog = await waitForLogContaining(server, "cssDiagnostics.reuse");
        const nextCss = diagnosticFromSource(nextDiagnostics, "asp-lsp-css");
        expect(nextCss?.range.start.line).toBe(6);
        const logs = JSON.stringify([
          htmlReuseLog,
          cssReuseLog,
          ...server.takePendingNotifications("window/logMessage"),
        ]);
        expect(logs).not.toContain("css.context.create");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("reuses and shifts include diagnostics after safe incremental edits", async () => {
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
              debug: { output: "verbose" },
              diagnostics: { debounceMs: 0 },
            },
          },
        });

        const uri = "file:///tmp/include-reuse-after-html-edit.asp";
        let source = `<div>top</div>
<!--#include file="missing.inc"-->
<% Response.Write "ok" %>`;
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        const firstDiagnostics = await waitForDiagnosticsContaining(server, "missing.inc");
        await waitForLogContaining(server, "LSP check completed");
        const firstMissing = diagnosticContaining(firstDiagnostics, "missing.inc");
        expect(firstMissing?.range.start.line).toBe(1);
        server.takePendingNotifications("window/logMessage");
        server.takePendingNotifications("textDocument/publishDiagnostics");

        source = notifyRangedReplacement(server, uri, source, 2, "top", "top\nnext");

        const nextDiagnostics = await waitForDiagnosticsContaining(server, "missing.inc");
        const reuseLog = await waitForLogContaining(server, "includeDiagnostics.reuse");
        const nextMissing = diagnosticContaining(nextDiagnostics, "missing.inc");
        expect(nextMissing?.range.start.line).toBe(2);
        expect(JSON.stringify(reuseLog.params)).toContain(uri);
        expect(source).toContain("top\nnext");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("reuses VBScript diagnostics after HTML edits outside VBScript regions", async () => {
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
              debug: { output: "verbose" },
              diagnostics: { debounceMs: 0 },
            },
          },
        });

        const uri = "file:///tmp/vb-reuse-after-html-edit.asp";
        let source = `<div>top</div>
<%
Option Explicit
Response.Write missingName
%>`;
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        const firstDiagnostics = await waitForDiagnosticsContaining(server, "missingName");
        await waitForLogContaining(server, "LSP check completed");
        server.takePendingNotifications("window/logMessage");
        server.takePendingNotifications("textDocument/publishDiagnostics");
        const firstMissing = diagnosticContaining(firstDiagnostics, "missingName");
        expect(firstMissing?.range.start.line).toBe(3);

        source = notifyRangedReplacement(server, uri, source, 2, "top", "top\nnext");

        const nextDiagnostics = await waitForDiagnosticsContaining(server, "missingName");
        const reuseLog = await waitForLogContaining(server, "analysis.vbscript.reuse");
        const diagnosticsReuseLog = await waitForLogContaining(
          server,
          "check.vbscript.diagnostics.reuse",
        );
        const nextMissing = diagnosticContaining(nextDiagnostics, "missingName");
        expect(nextMissing?.range.start.line).toBe(4);
        const logs = JSON.stringify([
          reuseLog,
          diagnosticsReuseLog,
          ...server.takePendingNotifications("window/logMessage"),
        ]);
        expect(logs).not.toContain("check.vbscript.diagnostics.symbols");
        expect(logs).not.toContain("check.vbscript.projectContext");
        expect(source).toContain("top\nnext");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("reuses VBScript diagnostics after CSS and client JavaScript edits", async () => {
      const cases = [
        {
          uri: "file:///tmp/vb-reuse-after-css-edit.asp",
          source: `<style>.card { color: red; }</style>
<%
Option Explicit
Response.Write missingName
%>`,
          needle: "red",
          replacement: "blue",
        },
        {
          uri: "file:///tmp/vb-reuse-after-client-js-edit.asp",
          source: `<script>const clientValue = 1;</script>
<%
Option Explicit
Response.Write missingName
%>`,
          needle: "clientValue",
          replacement: "renamedClientValue",
        },
      ];

      for (const testCase of cases) {
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
                debug: { output: "verbose" },
                diagnostics: { debounceMs: 0 },
              },
            },
          });
          let source = testCase.source;
          server.notify("textDocument/didOpen", {
            textDocument: {
              uri: testCase.uri,
              languageId: "classic-asp",
              version: 1,
              text: source,
            },
          });
          await waitForDiagnosticsContaining(server, "missingName");
          await waitForLogContaining(server, "LSP check completed");
          server.takePendingNotifications("window/logMessage");
          server.takePendingNotifications("textDocument/publishDiagnostics");

          source = notifyRangedReplacement(
            server,
            testCase.uri,
            source,
            2,
            testCase.needle,
            testCase.replacement,
          );

          await waitForDiagnosticsContaining(server, "missingName");
          const reuseLog = await waitForLogContaining(server, "analysis.vbscript.reuse");
          const diagnosticsReuseLog = await waitForLogContaining(
            server,
            "check.vbscript.diagnostics.reuse",
          );
          const logs = JSON.stringify([
            reuseLog,
            diagnosticsReuseLog,
            ...server.takePendingNotifications("window/logMessage"),
          ]);
          expect(logs).not.toContain("check.vbscript.diagnostics.symbols");
          expect(logs).not.toContain("check.vbscript.projectContext");
          expect(source).toContain(testCase.replacement);

          await server.request("shutdown", null);
          server.notify("exit", undefined);
        } finally {
          server.stop();
        }
      }
    });

    it("publishes complete diagnostics once for push diagnostics", async () => {
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: "file:///tmp",
          capabilities: {},
        });

        const uri = "file:///tmp/complete-diagnostics.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: `<style>.broken { color: }</style>
<!-- #include file="missing.inc" -->
<% Option Explicit
Response.Write missingName
%>`,
          },
        });

        const diagnostics = await waitForDiagnosticsContaining(server, "missingName");
        const text = JSON.stringify(diagnostics.params);
        expect(text).toContain("missing.inc");
        expect(text).toContain("asp-lsp-css");
        expect(text).toContain("missingName");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("does not publish stale diagnostics after rapid immediate changes", async () => {
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: "file:///tmp",
          capabilities: {},
        });
        server.notify("workspace/didChangeConfiguration", {
          settings: { aspLsp: { diagnostics: { debounceMs: 0 } } },
        });

        const uri = "file:///tmp/stale-diagnostics.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: `<% Option Explicit
Dim known
Response.Write known
%>`,
          },
        });
        await server.waitForNotification("textDocument/publishDiagnostics");

        server.notify("textDocument/didChange", {
          textDocument: { uri, version: 2 },
          contentChanges: [{ text: `<% Option Explicit\nResponse.Write staleName\n%>` }],
        });
        server.notify("textDocument/didChange", {
          textDocument: { uri, version: 3 },
          contentChanges: [{ text: `<% Option Explicit\nResponse.Write finalName\n%>` }],
        });

        const diagnostics = await waitForDiagnosticsContaining(server, "finalName");
        expect(JSON.stringify(diagnostics.params)).not.toContain("staleName");
        await delay(30);
        const pendingText = JSON.stringify(
          server
            .takePendingNotifications("textDocument/publishDiagnostics")
            .map((item) => item.params),
        );
        expect(pendingText).not.toContain("staleName");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("drops stale staged async diagnostics when a newer document version wins", async () => {
      const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-staged-stale-"));
      for (let index = 0; index < 80; index += 1) {
        const next = index === 79 ? "inc0.inc" : `inc${index + 1}.inc`;
        fs.writeFileSync(
          path.join(tempDir, `inc${index}.inc`),
          `<!-- #include file="${next}" -->\n<% Const Value${index} = ${index} %>`,
          "utf8",
        );
      }
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: `file://${tempDir}`,
          capabilities: {},
        });
        server.notify("workspace/didChangeConfiguration", {
          settings: {
            aspLsp: {
              debug: { output: "verbose" },
              diagnostics: { debounceMs: 0 },
            },
          },
        });
        const uri = `file://${path.join(tempDir, "default.asp")}`;
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: `<!-- #include file="inc0.inc" -->
<%
Option Explicit
Response.Write staleName
%>`,
          },
        });
        await waitForDiagnosticsPublished(server, uri);
        server.notify("textDocument/didChange", {
          textDocument: { uri, version: 2 },
          contentChanges: [
            {
              text: `<%
Option Explicit
Response.Write finalName
%>`,
            },
          ],
        });

        const diagnostics = await waitForDiagnosticsContaining(server, "finalName");
        expect((diagnostics.params as { version?: number }).version).toBe(2);
        await waitForLogContaining(server, "diagnostics.include.stale");
        await delay(80);
        const pendingText = JSON.stringify(
          server
            .takePendingNotifications("textDocument/publishDiagnostics")
            .map((item) => item.params),
        );
        expect(pendingText).not.toContain("staleName");
        expect(pendingText).not.toContain("Include cycle detected");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
        fs.rmSync(tempDir, { recursive: true, force: true });
      }
    });

    it("does not publish pending diagnostics after a document closes", async () => {
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: "file:///tmp",
          capabilities: {},
        });

        const uri = "file:///tmp/closed-diagnostics.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: `<% Option Explicit\nResponse.Write closedName\n%>`,
          },
        });
        server.notify("textDocument/didClose", {
          textDocument: { uri },
        });

        await delay(30);
        const diagnostics = server
          .takePendingNotifications("textDocument/publishDiagnostics")
          .map((item) => item.params as { diagnostics?: unknown[]; uri?: string });
        expect(diagnostics.at(-1)).toMatchObject({ uri, diagnostics: [] });

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("keeps language features current after ranged ASP edits", async () => {
      const source = `<%
Option Explicit
Dim known
Response.Write missingName
%>
<style>.x { color: }</style>
<script>const = ;</script>`;
      let current = source;
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: "file:///tmp",
          capabilities: {},
        });
        server.notify("workspace/didChangeConfiguration", {
          settings: { aspLsp: { diagnostics: { debounceMs: 0 } } },
        });
        const uri = "file:///tmp/ranged-edits.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: current,
          },
        });
        await server.waitForNotification("textDocument/publishDiagnostics");

        current = notifyRangedReplacement(server, uri, current, 2, "missingName", "known");
        current = notifyRangedReplacement(server, uri, current, 3, "color:", "color: red");
        current = notifyRangedReplacement(server, uri, current, 4, "const = ;", "const ok = 1;");

        const pulled = await server.request("textDocument/diagnostic", {
          textDocument: { uri },
        });
        expect(JSON.stringify(pulled)).not.toContain("missingName");
        expect(JSON.stringify(pulled)).not.toContain("asp-lsp-css");
        expect(JSON.stringify(pulled)).not.toContain("asp-lsp-typescript");

        const completions = await server.request("textDocument/completion", {
          textDocument: { uri },
          position: positionAt(current, current.indexOf("Response.") + "Response.".length),
        });
        expect(JSON.stringify(completions)).toContain("Write");

        const hover = await server.request("textDocument/hover", {
          textDocument: { uri },
          position: positionAt(
            current,
            current.indexOf("known", current.indexOf("Response.Write")),
          ),
        });
        expect(JSON.stringify(hover)).toContain("Dim known");

        const semanticTokens = await server.request("textDocument/semanticTokens/full", {
          textDocument: { uri },
        });
        const decoded = decodeSemanticTokens((semanticTokens as { data?: number[] }).data);
        const knownPosition = positionAt(
          current,
          current.indexOf("known", current.indexOf("Response.Write")),
        );
        expect(
          decoded.some(
            (token) =>
              token.line === knownPosition.line &&
              token.character === knownPosition.character &&
              token.tokenType === semanticTokenType.variable,
          ),
        ).toBe(true);

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
        expect(serializedHover).not.toContain("VBScript function.");

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
        const usageReferences = await server.request("textDocument/references", {
          textDocument: { uri },
          position: callPosition,
          context: { includeDeclaration: false },
        });
        expect(Array.isArray(usageReferences) ? usageReferences.length : 0).toBe(1);

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
''' <param name="▮"></param>
''' <returns>Display name.</returns>
Function BuildName(first)
  BuildName = first
End Function
Response.Write BuildName("Ada")
''' <▮
''' <see ▮/>
''' <summary>Text</▮
''' <see cref="▮" />
%>`;
      const text = source.replaceAll("▮", "");
      const tagPosition = positionAt(
        text,
        text.indexOf("''' <\n", text.indexOf('BuildName("Ada")')) + "''' <".length,
      );
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

    it("uses plain comment docs and annotation completions over JSON-RPC", async () => {
      const source = `<%
' **Plain** docs with <summary>tag</summary>.
' Second plain line.
Function PlainDocumented()
End Function
Response.Write PlainDocumented()
' Response.▮
' @▮
' @type customerId As String
%>`;
      const firstMarker = markedDocument(source);
      const text = firstMarker.text.replaceAll("▮", "");
      const ordinaryCommentPosition = firstMarker.position;
      const annotationPosition = positionAt(text, text.indexOf("' @") + "' @".length);
      const callPosition = positionAt(text, text.indexOf("PlainDocumented()") + 2);
      const annotationHoverPosition = positionAt(text, text.indexOf("@type") + 1);
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: "file:///tmp",
          capabilities: {},
        });
        const uri = "file:///tmp/vbscript-plain-comments.asp";
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
        const serializedHover = JSON.stringify(hover);
        expect(serializedHover).toContain("\\\\*\\\\*Plain\\\\*\\\\* docs");
        expect(serializedHover).toContain("&lt;summary&gt;tag&lt;/summary&gt;");
        expect(serializedHover).toContain("&lt;/summary&gt;\\\\.  ");
        expect(serializedHover).toContain("Second plain line\\\\.");
        expect(serializedHover).not.toContain("**Plain** docs");
        expect(serializedHover).not.toContain("<summary>tag</summary>");

        const ordinaryCommentCompletions = await server.request("textDocument/completion", {
          textDocument: { uri },
          position: ordinaryCommentPosition,
        });
        expect(completionLabels(ordinaryCommentCompletions)).not.toContain("Write");

        const annotationCompletions = await server.request("textDocument/completion", {
          textDocument: { uri },
          position: annotationPosition,
        });
        expect(completionLabels(annotationCompletions)).toEqual(
          expect.arrayContaining(["@type", "@param", "@returns"]),
        );
        expect(JSON.stringify(annotationCompletions)).toContain("VBScript type annotation");
        expect(JSON.stringify(annotationCompletions)).toContain("' @type name As Type");

        const annotationHover = await server.request("textDocument/hover", {
          textDocument: { uri },
          position: annotationHoverPosition,
        });
        expect(JSON.stringify(annotationHover)).toContain("' @type name As Type");

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

        const completions = await waitForCompletionContaining(
          server,
          { uri: `file://${owner}`, position: marked.position },
          "SharedTitle",
        );
        expect(JSON.stringify(completions)).toContain("SharedTitle");

        const definition = await waitForDefinitionContaining(
          server,
          { uri: `file://${owner}`, position: marked.position },
          "common.inc",
        );
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

    it("does not add fallback VBScript semantic tokens inside strings or comments", async () => {
      const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-semantic-"));
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
      const source = `<!-- #include file="common.inc" -->
<%
Response.Write "SharedTitle"
' SharedTitle should stay a comment
Rem SharedTitle should stay a comment
Response.Write SharedTitle()
%>`;
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
        await waitForCompletionContaining(
          server,
          {
            uri: `file://${owner}`,
            position: positionAt(source, source.lastIndexOf("SharedTitle") + "Shared".length),
          },
          "SharedTitle",
        );

        const semanticTokens = await server.request("textDocument/semanticTokens/full", {
          textDocument: { uri: `file://${owner}` },
        });
        const decoded = decodeSemanticTokens((semanticTokens as { data?: number[] }).data);
        const hasFunctionTokenAt = (offset: number) => {
          const position = positionAt(source, offset);
          return decoded.some(
            (token) =>
              token.line === position.line &&
              token.character === position.character &&
              token.tokenType === semanticTokenType.function,
          );
        };

        expect(hasFunctionTokenAt(source.indexOf("SharedTitle"))).toBe(false);
        expect(hasFunctionTokenAt(source.indexOf("SharedTitle", source.indexOf("' ")))).toBe(false);
        expect(hasFunctionTokenAt(source.indexOf("SharedTitle", source.indexOf("Rem ")))).toBe(
          false,
        );
        expect(hasFunctionTokenAt(source.lastIndexOf("SharedTitle"))).toBe(true);

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
        fs.rmSync(tempDir, { recursive: true, force: true });
      }
    });

    it("returns current-file and include VBScript help", async () => {
      const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-progressive-"));
      const owner = path.join(tempDir, "default.asp");
      const include = path.join(tempDir, "common.inc");
      fs.writeFileSync(
        include,
        `<%
Function IncludedOnly()
End Function
%>`,
        "utf8",
      );
      const localFiller = Array.from(
        { length: 1_200 },
        (_, index) => `localFastValue${index} = ${index}`,
      ).join("\n");
      const marked = markedDocument(`<!-- #include file="common.inc" -->
<%
Function LocalOnly(ByVal value)
LocalOnly = value
End Function
Sub UsesLocal()
LocalOnly(localValue)
End Sub
${localFiller}
localValue = 1
Response.Write ▮
Response.Write CStr(localValue)
Response.Write IncludedOnly()
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
        server.notify("workspace/didChangeConfiguration", {
          settings: {
            aspLsp: {
              inlayHints: { functionReturnTypes: true, variableTypes: true },
            },
          },
        });
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri: `file://${owner}`,
            languageId: "classic-asp",
            version: 1,
            text: marked.text,
          },
        });

        const immediateCompletions = await server.request("textDocument/completion", {
          textDocument: { uri: `file://${owner}` },
          position: marked.position,
        });
        expect(completionLabels(immediateCompletions)).toContain("LocalOnly");

        const responseHoverStartedAt = Date.now();
        const responseHover = await server.request("textDocument/hover", {
          textDocument: { uri: `file://${owner}` },
          position: positionAt(marked.text, marked.text.indexOf("Write CStr")),
        });
        expect(Date.now() - responseHoverStartedAt).toBeLessThan(1_000);
        expect(JSON.stringify(responseHover)).toContain("Response.Write");

        const cstrHoverStartedAt = Date.now();
        const cstrHover = await server.request("textDocument/hover", {
          textDocument: { uri: `file://${owner}` },
          position: positionAt(marked.text, marked.text.indexOf("CStr")),
        });
        expect(Date.now() - cstrHoverStartedAt).toBeLessThan(1_000);
        expect(JSON.stringify(cstrHover)).toContain("Function CStr(value) As String");

        const hover = await server.request("textDocument/hover", {
          textDocument: { uri: `file://${owner}` },
          position: positionAt(marked.text, marked.text.indexOf("LocalOnly")),
        });
        expect(JSON.stringify(hover)).toContain("Function LocalOnly(ByVal value)");

        const localCallOffset = marked.text.indexOf("LocalOnly(localValue)");
        const signature = await server.request("textDocument/signatureHelp", {
          textDocument: { uri: `file://${owner}` },
          position: positionAt(marked.text, localCallOffset + "LocalOnly(".length),
        });
        expect(JSON.stringify(signature)).toContain("Function LocalOnly(ByVal value)");

        const definition = await server.request("textDocument/definition", {
          textDocument: { uri: `file://${owner}` },
          position: positionAt(marked.text, localCallOffset + "Local".length),
        });
        expect(JSON.stringify(definition)).toContain(`"uri":"file://${owner}"`);

        const inlayHints = await server.request("textDocument/inlayHint", {
          textDocument: { uri: `file://${owner}` },
          range: { start: { line: 0, character: 0 }, end: { line: 1_210, character: 0 } },
        });
        expect(JSON.stringify(inlayHints)).toContain("As Number");

        await server.request("textDocument/diagnostic", {
          textDocument: { uri: `file://${owner}` },
        });
        const includeCompletions = await server.request("textDocument/completion", {
          textDocument: { uri: `file://${owner}` },
          position: marked.position,
        });
        expect(completionLabels(includeCompletions)).toContain("IncludedOnly");

        const includeHover = await server.request("textDocument/hover", {
          textDocument: { uri: `file://${owner}` },
          position: positionAt(marked.text, marked.text.indexOf("IncludedOnly()")),
        });
        expect(JSON.stringify(includeHover)).toContain("Function IncludedOnly()");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
        fs.rmSync(tempDir, { recursive: true, force: true });
      }
    });

    it("keeps disk-restored include summaries include-aware for inlay markers", async () => {
      const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-include-summary-"));
      const owner = path.join(tempDir, "default.asp");
      const include = path.join(tempDir, "common.inc");
      const cacheDirectory = path.join(tempDir, ".analysis-cache");
      const source = `<!-- #include file="common.inc" -->
<%
implicitValue = 1
Response.Write implicitValue
%>`;
      fs.writeFileSync(owner, source, "utf8");
      fs.writeFileSync(include, "<!-- shared markup only -->", "utf8");
      const uri = `file://${owner}`;

      const readInlayHints = async () => {
        const server = new RpcServer();
        try {
          await server.start();
          await server.request("initialize", {
            processId: process.pid,
            rootUri: `file://${tempDir}`,
            capabilities: {},
          });
          server.notify("workspace/didChangeConfiguration", {
            settings: {
              aspLsp: {
                cache: { directory: cacheDirectory },
                debug: { output: "summary" },
                diagnostics: { debounceMs: 0 },
                inlayHints: {
                  functionReturnTypes: true,
                  globalVariableMarkers: "all",
                  variableTypes: true,
                },
              },
            },
          });
          server.notify("textDocument/didOpen", {
            textDocument: {
              uri,
              languageId: "classic-asp",
              version: 1,
              text: source,
            },
          });
          const inlayHints = await server.request("textDocument/inlayHint", {
            textDocument: { uri },
            range: {
              start: { line: 0, character: 0 },
              end: positionAt(source, source.length),
            },
          });
          return { server, inlayHints };
        } catch (error) {
          server.stop();
          throw error;
        }
      };
      const shutdown = async (server: RpcServer | undefined) => {
        if (!server) {
          return;
        }
        await server.request("shutdown", null).catch(() => undefined);
        server.notify("exit", undefined);
        server.stop();
      };

      try {
        let warm: Awaited<ReturnType<typeof readInlayHints>> | undefined;
        try {
          warm = await readInlayHints();
          expect(JSON.stringify(warm.inlayHints)).not.toContain("(?)");
        } finally {
          await shutdown(warm?.server);
        }

        let restored: Awaited<ReturnType<typeof readInlayHints>> | undefined;
        try {
          restored = await readInlayHints();
          const restoredLog = JSON.stringify(
            restored.server.takePendingNotifications("window/logMessage"),
          );
          expect(restoredLog).toContain("diskSummary.hit");
          expect(JSON.stringify(restored.inlayHints)).not.toContain("(?)");
        } finally {
          await shutdown(restored?.server);
        }
      } finally {
        fs.rmSync(tempDir, { recursive: true, force: true });
      }
    });

    it("reuses summary-backed VB completion for include-heavy files after edits", async () => {
      const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-heavy-include-edit-"));
      const owner = path.join(tempDir, "default.asp");
      const includeDirectives: string[] = [];
      for (let index = 0; index < 30; index += 1) {
        const includeName = `shared-${index}.inc`;
        includeDirectives.push(`<!-- #include file="${includeName}" -->`);
        fs.writeFileSync(
          path.join(tempDir, includeName),
          `<%
Function SharedExport${index}()
End Function
%>`,
          "utf8",
        );
      }
      let source = `${includeDirectives.join("\n")}
<%
Dim localValue
Response.Write Sha
%>`;
      const uri = `file://${owner}`;
      fs.writeFileSync(owner, source, "utf8");

      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: `file://${tempDir}`,
          capabilities: {},
        });
        server.notify("workspace/didChangeConfiguration", {
          settings: {
            aspLsp: {
              debug: { output: "verbose" },
              diagnostics: { debounceMs: 0 },
            },
          },
        });
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        await waitForLogContaining(server, "LSP check completed");

        const completionPosition = () =>
          positionAt(source, source.indexOf("Response.Write Sha") + "Response.Write Sha".length);
        const warmedCompletions = await server.request("textDocument/completion", {
          textDocument: { uri },
          position: completionPosition(),
        });
        expect(completionLabels(warmedCompletions)).toContain("SharedExport29");
        server.takePendingNotifications("window/logMessage");

        source = notifyRangedReplacement(server, uri, source, 2, "localValue", "localOther");
        const immediateCompletions = await server.request("textDocument/completion", {
          textDocument: { uri },
          position: completionPosition(),
        });
        expect(completionLabels(immediateCompletions)).toContain("SharedExport29");
        await waitForLogContaining(server, "vbProject.summaryGraph.built");

        await waitForLogContaining(server, "LSP check completed");
        const refreshedCompletions = await server.request("textDocument/completion", {
          textDocument: { uri },
          position: completionPosition(),
        });
        expect(completionLabels(refreshedCompletions)).toContain("SharedExport29");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
        fs.rmSync(tempDir, { recursive: true, force: true });
      }
    });

    it("caps large VB project contexts from settings and keeps edit-time inlay hints current", async () => {
      const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-capped-include-edit-"));
      const owner = path.join(tempDir, "default.asp");
      const includeDirectives: string[] = [];
      for (let index = 0; index < 8; index += 1) {
        const includeName = `shared-${index}.inc`;
        if (index === 0) {
          includeDirectives.push(`<!-- #include file="${includeName}" -->`);
        }
        const nextInclude = index < 7 ? `<!-- #include file="shared-${index + 1}.inc" -->\n` : "";
        fs.writeFileSync(
          path.join(tempDir, includeName),
          `${nextInclude}<%
Function SharedExport${index}()
End Function
%>`,
          "utf8",
        );
      }
      let source = `${includeDirectives.join("\n")}
<%
Dim localValue
localValue = 1
Response.Write localValue
%>`;
      const uri = `file://${owner}`;
      fs.writeFileSync(owner, source, "utf8");

      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: `file://${tempDir}`,
          capabilities: {},
        });
        server.notify("workspace/didChangeConfiguration", {
          settings: {
            aspLsp: {
              debug: { output: "summary" },
              diagnostics: { debounceMs: 0 },
              inlayHints: {
                functionReturnTypes: true,
                globalVariableMarkers: "all",
                variableTypes: true,
              },
              workspace: {
                vbProjectMaxDocuments: 4,
                vbProjectMaxTextLength: 1048576,
              },
            },
          },
        });
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        await waitForLogContaining(server, "vbProject.documents.truncated");
        await waitForLogContaining(server, "includeCycle.truncated");

        const initialHints = await server.request("textDocument/inlayHint", {
          textDocument: { uri },
          range: {
            start: { line: 0, character: 0 },
            end: positionAt(source, source.length),
          },
        });
        expect(JSON.stringify(initialHints)).toContain("As Number");

        source = notifyRangedReplacement(
          server,
          uri,
          source,
          2,
          "localValue = 1",
          'localValue = "one"',
        );
        const editedHints = await server.request("textDocument/inlayHint", {
          textDocument: { uri },
          range: {
            start: { line: 0, character: 0 },
            end: positionAt(source, source.length),
          },
        });
        const serializedEditedHints = JSON.stringify(editedHints);
        expect(serializedEditedHints).toContain("As String");
        expect(serializedEditedHints).not.toContain("As Number");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
        fs.rmSync(tempDir, { recursive: true, force: true });
      }
    });

    it("invalidates VB completion cache after watched include export changes", async () => {
      const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-include-completion-"));
      const owner = path.join(tempDir, "default.asp");
      const include = path.join(tempDir, "shared.inc");
      fs.writeFileSync(
        include,
        `<%
Function SharedOld()
End Function
%>`,
        "utf8",
      );
      const source = `<!-- #include file="shared.inc" -->
<%
Response.Write Sha
%>`;
      const uri = `file://${owner}`;
      fs.writeFileSync(owner, source, "utf8");

      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: `file://${tempDir}`,
          capabilities: {},
        });
        server.notify("workspace/didChangeConfiguration", {
          settings: {
            aspLsp: {
              debug: { output: "verbose" },
              diagnostics: { debounceMs: 0 },
            },
          },
        });
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        await waitForLogContaining(server, "LSP check completed");

        const position = positionAt(
          source,
          source.indexOf("Response.Write Sha") + "Response.Write Sha".length,
        );
        const oldCompletions = await server.request("textDocument/completion", {
          textDocument: { uri },
          position,
        });
        expect(completionLabels(oldCompletions)).toContain("SharedOld");
        server.takePendingNotifications("window/logMessage");

        fs.writeFileSync(
          include,
          `<%
Function SharedNew()
End Function
%>`,
          "utf8",
        );
        server.notify("workspace/didChangeWatchedFiles", {
          changes: [{ uri: `file://${include}`, type: 2 }],
        });
        await waitForLogContaining(server, "completion.cache.invalidate");
        await waitForLogContaining(server, "LSP check completed");

        const newCompletions = await server.request("textDocument/completion", {
          textDocument: { uri },
          position,
        });
        const labels = completionLabels(newCompletions);
        expect(labels).toContain("SharedNew");
        expect(labels).not.toContain("SharedOld");

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
currencyValue = CCur(1)
nullValue = Null
emptyValue = Empty
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
        server.notify("workspace/didChangeConfiguration", {
          settings: {
            aspLsp: {
              inlayHints: { functionReturnTypes: true, variableTypes: true },
            },
          },
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
        expect(JSON.stringify(hover)).toContain("(global) Dim a As Number");
        expect(JSON.stringify(hover)).not.toContain("Implicit VBScript variable");
        const currencyHover = await server.request("textDocument/hover", {
          textDocument: { uri },
          position: positionAt(source, source.indexOf("CCur")),
        });
        expect(JSON.stringify(currencyHover)).toContain("Function CCur(value) As Currency");

        const inlayHints = await server.request("textDocument/inlayHint", {
          textDocument: { uri },
          range: { start: { line: 0, character: 0 }, end: { line: 7, character: 0 } },
        });
        const serializedInlayHints = JSON.stringify(inlayHints);
        expect(serializedInlayHints).toContain("As Number");
        expect(serializedInlayHints).toContain("As Currency");
        expect(serializedInlayHints).toContain("As Null");
        expect(serializedInlayHints).toContain("As Empty");

        const semanticTokens = await server.request("textDocument/semanticTokens/full", {
          textDocument: { uri },
        });
        const decoded = decodeSemanticTokens((semanticTokens as { data?: number[] }).data);
        expect(
          decoded.some(
            (token) =>
              token.line === 5 &&
              token.character === "Response.Write ".length &&
              token.tokenType === semanticTokenType.variable,
          ),
        ).toBe(true);
        expect(
          decoded.some(
            (token) =>
              token.line === 5 &&
              token.character === 0 &&
              token.tokenType === semanticTokenType.constant &&
              token.tokenModifiers ===
                (semanticTokenModifier.readonly | semanticTokenModifier.library),
          ),
        ).toBe(true);

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("returns hover, inlay hints and completions for VBScript union types", async () => {
      const source = `<%
Class FirstThing
  Public SharedName
  Public OnlyFirst
End Class
Class SecondThing
  Public SharedName
End Class
x = 1
x = "a"
Dim unknownGlobal
Function UnknownReturn()
End Function
Dim both
Set both = New FirstThing
Set both = New SecondThing
both.SharedName
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
              inlayHints: {
                functionReturnTypes: true,
                globalVariableMarkers: "global",
                variableTypes: true,
              },
            },
          },
        });
        const uri = "file:///tmp/union-vbscript.asp";
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
          position: positionAt(source, source.indexOf("x = 1")),
        });
        expect(JSON.stringify(hover)).toContain("(global) Dim x As Number | String");
        expect(JSON.stringify(hover)).not.toContain("variable (global)");

        const inlayHints = await server.request("textDocument/inlayHint", {
          textDocument: { uri },
          range: { start: { line: 0, character: 0 }, end: { line: 20, character: 0 } },
        });
        const serializedInlayHints = JSON.stringify(inlayHints);
        expect(serializedInlayHints).toContain("(global) As Number | String");
        expect(serializedInlayHints).toContain("(global) As Variant");
        expect(serializedInlayHints).toContain("As Variant");

        const completions = await server.request("textDocument/completion", {
          textDocument: { uri },
          position: positionAt(source, source.indexOf("both.SharedName") + "both.".length),
        });
        const labels = completionLabels(completions);
        expect(labels).toContain("SharedName");
        expect(labels).not.toContain("OnlyFirst");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("returns semantic tokens for Classic ASP include directives", async () => {
      const source = `<!-- #include file="includes/data.inc" -->
<% Response.Write "ok" %>`;
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: "file:///tmp",
          capabilities: {},
        });
        const uri = "file:///tmp/include-semantic.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        await server.waitForNotification("textDocument/publishDiagnostics");

        const links = await server.request("textDocument/documentLink", {
          textDocument: { uri },
        });
        const link = (links as Array<{ range?: unknown }>)[0] as { range?: unknown };
        expect(link.range).toEqual({
          start: positionAt(source, source.indexOf('"includes/data.inc"')),
          end: positionAt(
            source,
            source.indexOf('"includes/data.inc"') + '"includes/data.inc"'.length,
          ),
        });

        const semanticTokens = await server.request("textDocument/semanticTokens/full", {
          textDocument: { uri },
        });
        const decoded = decodeSemanticTokens((semanticTokens as { data?: number[] }).data);
        const tokenAt = (text: string) => {
          const position = positionAt(source, source.indexOf(text));
          return decoded.find(
            (token) => token.line === position.line && token.character === position.character,
          );
        };
        expect(tokenAt("#include")).toEqual(
          expect.objectContaining({ tokenType: semanticTokenType.keyword }),
        );
        expect(tokenAt("file")).toEqual(
          expect.objectContaining({ tokenType: semanticTokenType.property }),
        );
        expect(tokenAt('"includes/data.inc"')).toEqual(
          expect.objectContaining({ tokenType: semanticTokenType.string }),
        );

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("marks the ASP expression equals delimiter as a keyword semantic token", async () => {
      const source = `<%= title %>`;
      const equalsPosition = positionAt(source, source.indexOf("="));
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: "file:///tmp",
          capabilities: {},
        });
        const uri = "file:///tmp/asp-expression-semantic.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        await server.waitForNotification("textDocument/publishDiagnostics");

        const semanticTokens = await server.request("textDocument/semanticTokens/full", {
          textDocument: { uri },
        });
        const decoded = decodeSemanticTokens((semanticTokens as { data?: number[] }).data);
        expect(decoded).toContainEqual(
          expect.objectContaining({
            line: equalsPosition.line,
            character: equalsPosition.character,
            length: 1,
            tokenType: semanticTokenType.keyword,
          }),
        );

        const rangeTokens = await server.request("textDocument/semanticTokens/range", {
          textDocument: { uri },
          range: { start: { line: 0, character: 0 }, end: { line: 0, character: source.length } },
        });
        expect(decodeSemanticTokens((rangeTokens as { data?: number[] }).data)).toContainEqual(
          expect.objectContaining({
            line: equalsPosition.line,
            character: equalsPosition.character,
            length: 1,
            tokenType: semanticTokenType.keyword,
          }),
        );

        const deltaTokens = await server.request("textDocument/semanticTokens/full/delta", {
          textDocument: { uri },
          previousResultId: (semanticTokens as { resultId?: string }).resultId,
        });
        expect(JSON.stringify(deltaTokens)).toContain("edits");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("does not mark HTML tag names as keyword semantic tokens", async () => {
      const source = `<div class="card"><span><%= title %></span></div>`;
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: "file:///tmp",
          capabilities: {},
        });
        const uri = "file:///tmp/html-tag-semantic.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        await server.waitForNotification("textDocument/publishDiagnostics");

        const semanticTokens = await server.request("textDocument/semanticTokens/full", {
          textDocument: { uri },
        });
        const decoded = decodeSemanticTokens((semanticTokens as { data?: number[] }).data);
        expect(
          decoded.some((token) => tokenMatches(source, token, "div", semanticTokenType.keyword)),
        ).toBe(false);
        expect(
          decoded.some((token) => tokenMatches(source, token, "span", semanticTokenType.keyword)),
        ).toBe(false);
        expect(
          decoded.some((token) => tokenMatches(source, token, "<%=", semanticTokenType.keyword, 2)),
        ).toBe(true);

        const rangeTokens = await server.request("textDocument/semanticTokens/range", {
          textDocument: { uri },
          range: { start: { line: 0, character: 0 }, end: { line: 0, character: source.length } },
        });
        const decodedRange = decodeSemanticTokens((rangeTokens as { data?: number[] }).data);
        expect(
          decodedRange.some((token) =>
            tokenMatches(source, token, "div", semanticTokenType.keyword),
          ),
        ).toBe(false);
        expect(
          decodedRange.some((token) =>
            tokenMatches(source, token, "span", semanticTokenType.keyword),
          ),
        ).toBe(false);

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("limits range semantic tokens to the requested ASP and embedded spans", async () => {
      const source = `<style>
.a { color: red; }
</style>
<%
Dim beforeValue
Dim targetValue
targetValue = beforeValue + 1
Dim afterValue
%>`;
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: "file:///tmp",
          capabilities: {},
        });
        const uri = "file:///tmp/range-semantic.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        await server.waitForNotification("textDocument/publishDiagnostics");

        const cssTokens = await server.request("textDocument/semanticTokens/range", {
          textDocument: { uri },
          range: { start: { line: 1, character: 0 }, end: { line: 1, character: 20 } },
        });
        const decodedCss = decodeSemanticTokens((cssTokens as { data?: number[] }).data);
        expect(decodedCss.length).toBeGreaterThan(0);
        expect(decodedCss.every((token) => token.line === 1)).toBe(true);

        const vbTokens = await server.request("textDocument/semanticTokens/range", {
          textDocument: { uri },
          range: { start: { line: 6, character: 0 }, end: { line: 6, character: 32 } },
        });
        const decodedVb = decodeSemanticTokens((vbTokens as { data?: number[] }).data);
        expect(decodedVb.length).toBeGreaterThan(0);
        expect(decodedVb.every((token) => token.line === 6)).toBe(true);
        expect(decodedVb).toContainEqual(
          expect.objectContaining({
            line: 6,
            character: 12,
            tokenType: semanticTokenType.operator,
          }),
        );

        const boundaryTokens = await server.request("textDocument/semanticTokens/range", {
          textDocument: { uri },
          range: { start: { line: 5, character: 0 }, end: { line: 6, character: 0 } },
        });
        const decodedBoundary = decodeSemanticTokens((boundaryTokens as { data?: number[] }).data);
        expect(decodedBoundary.length).toBeGreaterThan(0);
        expect(decodedBoundary.every((token) => token.line === 5)).toBe(true);

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

    it("resolves Windows-style include paths and reports path casing mismatches", async () => {
      const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-path-case-"));
      const mismatchOwner = path.join(tempDir, "mismatch.asp");
      const exactOwner = path.join(tempDir, "exact.asp");
      const mixedCaseInclude = path.join(tempDir, "aB.asp");
      const upperCaseInclude = path.join(tempDir, "BA.asp");
      fs.writeFileSync(
        mixedCaseInclude,
        `<%
Function SharedFromMixed()
End Function
%>`,
        "utf8",
      );
      fs.writeFileSync(
        upperCaseInclude,
        `<%
Function SharedFromUpper()
End Function
%>`,
        "utf8",
      );
      const mismatch = markedDocument(`<!-- #include file="ab.asp" -->
<%
Response.Write Shared▮FromMixed()
%>`);
      const exact = `<!-- #include file="BA.asp" -->
<%
Response.Write SharedFromUpper()
%>`;
      fs.writeFileSync(mismatchOwner, mismatch.text, "utf8");
      fs.writeFileSync(exactOwner, exact, "utf8");

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
            uri: `file://${mismatchOwner}`,
            languageId: "classic-asp",
            version: 1,
            text: mismatch.text,
          },
        });

        const diagnostics = await waitForDiagnosticsContaining(server, "file system casing");
        const includeDiagnostics = (
          diagnostics.params as { diagnostics: Array<Record<string, unknown>> }
        ).diagnostics.filter((diagnostic) => diagnostic.source === "asp-lsp-include");
        expect(includeDiagnostics).toEqual(
          expect.arrayContaining([
            expect.objectContaining({
              code: "include.pathCaseMismatch",
              severity: DiagnosticSeverity.Warning,
            }),
          ]),
        );
        expect(JSON.stringify(includeDiagnostics)).toContain("aB.asp");

        await waitForDefinitionContaining(
          server,
          { uri: `file://${mismatchOwner}`, position: mismatch.position },
          "aB.asp",
        );
        const completions = await server.request("textDocument/completion", {
          textDocument: { uri: `file://${mismatchOwner}` },
          position: mismatch.position,
        });
        expect(completionLabels(completions)).toContain("SharedFromMixed");

        server.notify("textDocument/didOpen", {
          textDocument: {
            uri: `file://${exactOwner}`,
            languageId: "classic-asp",
            version: 1,
            text: exact,
          },
        });
        const exactDiagnostics = await server.request("textDocument/diagnostic", {
          textDocument: { uri: `file://${exactOwner}` },
        });
        expect(JSON.stringify(exactDiagnostics)).not.toContain("include.pathCaseMismatch");
        expect(JSON.stringify(exactDiagnostics)).not.toContain("could not be resolved");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
        fs.rmSync(tempDir, { recursive: true, force: true });
      }
    });

    it("can disable Windows-style include path casing diagnostics", async () => {
      const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-path-case-off-"));
      const owner = path.join(tempDir, "default.asp");
      fs.writeFileSync(
        path.join(tempDir, "aB.asp"),
        `<%
Function SharedFromMixed()
End Function
%>`,
        "utf8",
      );
      const source = `<!-- #include file="ab.asp" -->
<%
Response.Write SharedFromMixed()
%>`;
      fs.writeFileSync(owner, source, "utf8");

      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: `file://${tempDir}`,
          capabilities: {},
        });
        server.notify("workspace/didChangeConfiguration", {
          settings: { aspLsp: { windowsPathResolution: false } },
        });
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri: `file://${owner}`,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });

        const diagnostics = await server.request("textDocument/diagnostic", {
          textDocument: { uri: `file://${owner}` },
        });
        expect(JSON.stringify(diagnostics)).not.toContain("include.pathCaseMismatch");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
        fs.rmSync(tempDir, { recursive: true, force: true });
      }
    });

    it("returns hover and signature help for VBScript built-in functions", async () => {
      const source = `<%
Dim textValue
textValue = CStr(42)
Response.Write UBound(Array("a", "b"))
%>`;
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: "file:///tmp",
          capabilities: {},
        });
        const uri = "file:///tmp/vbscript-builtins.asp";
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
          position: positionAt(source, source.indexOf("CStr") + 1),
        });
        expect(JSON.stringify(hover)).toContain("Function CStr(value) As String");
        expect(JSON.stringify(hover)).toContain("Converts a value to String.");
        expect(JSON.stringify(hover)).not.toContain("VBScript built-in function.");

        const signature = await server.request("textDocument/signatureHelp", {
          textDocument: { uri },
          position: positionAt(source, source.indexOf('Array("a"') + 'Array("'.length),
        });
        expect(JSON.stringify(signature)).toContain("Array(values)");

        const semanticTokens = await server.request("textDocument/semanticTokens/full", {
          textDocument: { uri },
        });
        const decoded = decodeSemanticTokens((semanticTokens as { data?: number[] }).data);
        const tokenAt = (text: string) => {
          const position = positionAt(source, source.indexOf(text));
          return decoded.find(
            (token) => token.line === position.line && token.character === position.character,
          );
        };
        expect(tokenAt("CStr")).toEqual(
          expect.objectContaining({
            tokenType: semanticTokenType.function,
            tokenModifiers: semanticTokenModifier.library,
          }),
        );
        expect(tokenAt("UBound")).toEqual(
          expect.objectContaining({
            tokenType: semanticTokenType.function,
            tokenModifiers: semanticTokenModifier.library,
          }),
        );
        expect(tokenAt("Array")).toEqual(
          expect.objectContaining({
            tokenType: semanticTokenType.function,
            tokenModifiers: semanticTokenModifier.library,
          }),
        );
        expect(tokenAt("Write")).toEqual(
          expect.objectContaining({
            tokenType: semanticTokenType.method,
            tokenModifiers: semanticTokenModifier.library,
          }),
        );

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("supports VBScript On Error completions and Err built-ins", async () => {
      const source = `<%
Option Explicit
On Error Resume Next
Dim captured, errNumber
Set captured = Err
errNumber = Err.Number
Err.Raise(vbObjectError + 513, "App", "Boom")
Err.
%>`;
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: "file:///tmp",
          capabilities: {},
        });
        const uri = "file:///tmp/vbscript-err-builtins.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        const published = await server.waitForNotification("textDocument/publishDiagnostics");
        expect(JSON.stringify(published)).not.toContain("Err");

        const memberCompletions = await server.request("textDocument/completion", {
          textDocument: { uri },
          position: positionAt(source, source.lastIndexOf("Err.") + "Err.".length),
        });
        expect(completionLabels(memberCompletions)).toEqual(
          expect.arrayContaining(["Number", "Description", "Clear", "Raise"]),
        );

        const hover = await server.request("textDocument/hover", {
          textDocument: { uri },
          position: positionAt(source, source.indexOf("Err.Number") + "Err.".length),
        });
        expect(JSON.stringify(hover)).toContain("property ErrObject.Number As Number");

        const signature = await server.request("textDocument/signatureHelp", {
          textDocument: { uri },
          position: positionAt(source, source.indexOf("Err.Raise(") + "Err.Raise(".length),
        });
        expect(JSON.stringify(signature)).toContain(
          "Err.Raise(number, source, description, helpfile, helpcontext)",
        );

        const semanticTokens = await server.request("textDocument/semanticTokens/full", {
          textDocument: { uri },
        });
        const decoded = decodeSemanticTokens((semanticTokens as { data?: number[] }).data);
        const tokenAtOffset = (offset: number) => {
          const position = positionAt(source, offset);
          return decoded.find(
            (token) => token.line === position.line && token.character === position.character,
          );
        };
        expect(
          tokenAtOffset(source.indexOf("Set captured = Err") + "Set captured = ".length),
        ).toEqual(
          expect.objectContaining({
            tokenType: semanticTokenType.constant,
            tokenModifiers: semanticTokenModifier.readonly | semanticTokenModifier.library,
          }),
        );
        expect(tokenAtOffset(source.indexOf("Err.Number") + "Err.".length)).toEqual(
          expect.objectContaining({
            tokenType: semanticTokenType.property,
            tokenModifiers: semanticTokenModifier.library,
          }),
        );
        expect(tokenAtOffset(source.indexOf("Err.Raise") + "Err.".length)).toEqual(
          expect.objectContaining({
            tokenType: semanticTokenType.method,
            tokenModifiers: semanticTokenModifier.library,
          }),
        );

        const completionSource = `<%
On Error R
%>`;
        const completionUri = "file:///tmp/vbscript-on-error-completion.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri: completionUri,
            languageId: "classic-asp",
            version: 1,
            text: completionSource,
          },
        });
        await server.waitForNotification("textDocument/publishDiagnostics");
        const onErrorCompletions = await server.request("textDocument/completion", {
          textDocument: { uri: completionUri },
          position: positionAt(
            completionSource,
            completionSource.indexOf("On Error R") + "On Error R".length,
          ),
        });
        expect(completionLabels(onErrorCompletions)).toEqual(["On Error Resume Next"]);

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("supports VBScript rename, highlights, signature help, workspace symbols and semantic tokens", async () => {
      const marked = markedDocument(`<%
Function BuildName(ByVal firstName, lastName)
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
        expect(initializeText).toContain('"byref"');
        expect(initializeText).toContain('"byval"');
        expect(initializeText).toContain('"string"');
        expect(initializeText).toContain('"operator"');
        expect(initializeText).toContain('"constant"');
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
        expect(JSON.stringify(signature)).toContain("BuildName(ByVal firstName, ByRef lastName)");

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
              token.tokenType === semanticTokenType.parameter &&
              token.tokenModifiers === semanticTokenModifier.byval,
          ),
        ).toBe(true);
        expect(
          decodedSemanticTokens.some(
            (token) =>
              token.line === 1 &&
              token.character === "Function BuildName(ByVal ".length &&
              token.tokenType === semanticTokenType.parameter &&
              token.tokenModifiers === semanticTokenModifier.byval,
          ),
        ).toBe(true);
        expect(
          decodedSemanticTokens.some(
            (token) =>
              token.line === 1 &&
              token.character === "Function BuildName(ByVal firstName, ".length &&
              token.tokenType === semanticTokenType.parameter &&
              token.tokenModifiers === semanticTokenModifier.byref,
          ),
        ).toBe(true);
        const ampersand = positionAt(marked.text, marked.text.indexOf("&"));
        expect(
          decodedSemanticTokens.some(
            (token) =>
              token.line === ampersand.line &&
              token.character === ampersand.character &&
              token.tokenType === semanticTokenType.operator,
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
        server.notify("workspace/didChangeConfiguration", {
          settings: {
            aspLsp: {
              inlayHints: {
                functionReturnTypes: true,
                implicitByRef: true,
                variableTypes: true,
              },
            },
          },
        });

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
        expect(JSON.stringify(resolved)).toContain("Defined in [extended-lsp.asp]");
        expect(JSON.stringify(resolved)).toContain("file:///tmp/extended-lsp.asp");

        const selection = await server.request("textDocument/selectionRange", {
          textDocument: { uri },
          positions: [marked.position],
        });
        expect(JSON.stringify(selection)).toContain("parent");

        const inlayHints = await server.request("textDocument/inlayHint", {
          textDocument: { uri },
          range: { start: { line: 0, character: 0 }, end: { line: 12, character: 0 } },
        });
        const typeHint = (
          inlayHints as Array<{ label?: unknown; paddingLeft?: unknown; paddingRight?: unknown }>
        ).find((hint) => hint.label === " As Customer");
        expect(typeHint).toEqual(
          expect.objectContaining({ paddingLeft: false, paddingRight: true }),
        );
        expect(JSON.stringify(inlayHints)).toContain("ByRef");
        expect(JSON.stringify(inlayHints)).toContain("firstName:");
        const firstNameTypeHintPosition = positionAt(
          marked.text,
          marked.text.indexOf("firstName") + "firstName".length,
        );
        expect(
          (
            inlayHints as Array<{
              label?: unknown;
              position?: { line?: unknown; character?: unknown };
            }>
          ).some(
            (hint) =>
              hint.label === " As Variant" &&
              hint.position?.line === firstNameTypeHintPosition.line &&
              hint.position?.character === firstNameTypeHintPosition.character,
          ),
        ).toBe(false);
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
        const referencesDataAtLine = (lens: Record<string, unknown>, line: number) => {
          const data = lens.data as { kind?: unknown; line?: unknown } | undefined;
          return data?.kind === "vbscript-reference" && data.line === line;
        };
        const referencesCodeLens = (codeLens as Array<Record<string, unknown>>).find((lens) =>
          referencesDataAtLine(lens, 4),
        );
        const resolvedCodeLens = (await server.request(
          "codeLens/resolve",
          referencesCodeLens,
        )) as Record<string, unknown>;
        const referencesArguments = (
          resolvedCodeLens.command as { arguments?: unknown[] } | undefined
        )?.arguments;
        expect(resolvedCodeLens).toEqual(
          expect.objectContaining({
            command: expect.objectContaining({
              command: "aspLsp.showReferences",
              arguments: expect.arrayContaining([
                uri,
                expect.objectContaining({ line: 4, character: 9 }),
              ]),
            }),
          }),
        );
        expect(JSON.stringify(resolvedCodeLens.command)).toContain("1 reference");
        expect(referencesArguments?.[2]).toEqual([
          expect.objectContaining({
            uri,
            range: expect.objectContaining({
              start: expect.objectContaining({ line: 10, character: 17 }),
            }),
          }),
        ]);
        expect(JSON.stringify(resolvedCodeLens)).toContain("aspLsp.showReferences");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("hides include CodeLens by default and shows it when enabled", async () => {
      const source = `<!-- #include file="common.inc" -->
<%
Function BuildName()
End Function
Response.Write BuildName()
%>`;
      const uri = "file:///tmp/include-codelens-default.asp";
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: "file:///tmp",
          capabilities: {},
        });
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        await server.waitForNotification("textDocument/publishDiagnostics");

        const defaultCodeLens = await server.request("textDocument/codeLens", {
          textDocument: { uri },
        });
        expect(JSON.stringify(defaultCodeLens)).toContain("reference");
        expect(JSON.stringify(defaultCodeLens)).not.toContain("vscode.open");
        expect(JSON.stringify(defaultCodeLens)).not.toContain("common.inc");

        server.notify("workspace/didChangeConfiguration", {
          settings: { aspLsp: { codeLens: { includes: true } } },
        });

        const enabledCodeLens = await server.request("textDocument/codeLens", {
          textDocument: { uri },
        });
        expect(JSON.stringify(enabledCodeLens)).toContain("reference");
        expect(JSON.stringify(enabledCodeLens)).toContain("vscode.open");
        expect(JSON.stringify(enabledCodeLens)).toContain("common.inc");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("counts VBScript references from unopened workspace files", async () => {
      const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-workspace-refs-"));
      const common = path.join(tempDir, "common.inc");
      const page = path.join(tempDir, "default.asp");
      const commonSource = `<%
Function SharedTitle()
  SharedTitle = "Dashboard"
End Function
%>`;
      fs.writeFileSync(common, commonSource, "utf8");
      fs.writeFileSync(
        page,
        `<!-- #include file="common.inc" -->
<%
Response.Write SharedTitle()
%>`,
        "utf8",
      );
      const commonUri = pathToFileURL(common).href;
      const pageUri = pathToFileURL(page).href;
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: pathToFileURL(tempDir).href,
          capabilities: {},
        });
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri: commonUri,
            languageId: "classic-asp",
            version: 1,
            text: commonSource,
          },
        });
        await server.waitForNotification("textDocument/publishDiagnostics");

        const codeLens = (await server.request("textDocument/codeLens", {
          textDocument: { uri: commonUri },
        })) as Array<Record<string, unknown>>;
        const referencesCodeLens = codeLens.find((lens) => {
          const data = lens.data as { kind?: unknown; line?: unknown } | undefined;
          return data?.kind === "vbscript-reference" && data.line === 1;
        });
        const resolvedCodeLens = (await server.request(
          "codeLens/resolve",
          referencesCodeLens,
        )) as Record<string, unknown>;
        expect(JSON.stringify(resolvedCodeLens.command)).toContain("0 references");
        expect(JSON.stringify(resolvedCodeLens.command)).not.toContain(pageUri);

        server.notify("workspace/didChangeConfiguration", {
          settings: { aspLsp: { codeLens: { referenceScope: "workspace" } } },
        });
        const workspaceResolvedCodeLens = (await server.request(
          "codeLens/resolve",
          referencesCodeLens,
        )) as Record<string, unknown>;
        expect(JSON.stringify(workspaceResolvedCodeLens.command)).toContain("1 reference");
        expect(JSON.stringify(workspaceResolvedCodeLens.command)).toContain(pageUri);

        const references = await server.request("textDocument/references", {
          textDocument: { uri: commonUri },
          position: { line: 1, character: 10 },
          context: { includeDeclaration: false },
        });
        expect(JSON.stringify(references)).toContain(pageUri);

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
        fs.rmSync(tempDir, { recursive: true, force: true });
      }
    });

    it("honors the global variable marker inlay hint setting", async () => {
      const source = `<%
Dim pageTitle
pageTitle = "Dashboard"
Sub Render()
  Dim localTitle
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
        server.notify("workspace/didChangeConfiguration", {
          settings: {
            aspLsp: {
              inlayHints: {
                functionReturnTypes: true,
                globalVariableMarkers: false,
                variableTypes: true,
              },
            },
          },
        });
        const uri = "file:///tmp/global-marker-inlay-disabled.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        await server.waitForNotification("textDocument/publishDiagnostics");

        const inlayHints = await server.request("textDocument/inlayHint", {
          textDocument: { uri },
          range: { start: { line: 0, character: 0 }, end: { line: 8, character: 0 } },
        });
        const serialized = JSON.stringify(inlayHints);
        expect(serialized).not.toContain("(global)");
        expect(serialized).toContain("As String");
        expect(serialized).toContain("As Variant");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("defaults type, scope marker, and ByRef inlay hints off", async () => {
      const source = `<%
Function BuildName(firstName)
  BuildName = firstName
End Function
Dim pageTitle
pageTitle = BuildName("Dashboard")
%>`;
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: "file:///tmp",
          capabilities: {},
        });
        const uri = "file:///tmp/default-inlay-hints.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        await server.waitForNotification("textDocument/publishDiagnostics");

        const inlayHints = await server.request("textDocument/inlayHint", {
          textDocument: { uri },
          range: { start: { line: 0, character: 0 }, end: { line: 7, character: 0 } },
        });
        const serialized = JSON.stringify(inlayHints);
        expect(serialized).not.toContain(" As ");
        expect(serialized).not.toContain("(global)");
        expect(serialized).not.toContain("(local)");
        expect(serialized).not.toContain("ByRef");
        expect(serialized).toContain("firstName:");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("shows local and uncertain VBScript variable inlay hint markers when configured", async () => {
      const source = `<!-- #include file="shared.inc" -->
<%
a = 1
Sub Render()
  b = "local"
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
        server.notify("workspace/didChangeConfiguration", {
          settings: {
            aspLsp: {
              inlayHints: {
                functionReturnTypes: true,
                globalVariableMarkers: "all",
                variableTypes: true,
              },
            },
          },
        });
        const uri = "file:///tmp/include-uncertain-inlay.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        await server.waitForNotification("textDocument/publishDiagnostics");

        const inlayHints = await server.request("textDocument/inlayHint", {
          textDocument: { uri },
          range: { start: { line: 0, character: 0 }, end: { line: 8, character: 0 } },
        });
        const serialized = JSON.stringify(inlayHints);
        expect(serialized).toContain("(?)");
        expect(serialized).not.toContain("(global) As Number");
        expect(serialized).toContain("(local) As String");

        server.notify("workspace/didChangeConfiguration", {
          settings: {
            aspLsp: {
              inlayHints: {
                functionReturnTypes: true,
                globalVariableMarkers: "local",
                variableTypes: true,
              },
            },
          },
        });
        const localOnlyUri = "file:///tmp/local-marker-inlay.asp";
        const localOnlySource = `<%
Dim pageTitle
Sub Render()
  Dim localTitle
End Sub
%>`;
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri: localOnlyUri,
            languageId: "classic-asp",
            version: 1,
            text: localOnlySource,
          },
        });
        await server.waitForNotification("textDocument/publishDiagnostics");
        const localHints = await server.request("textDocument/inlayHint", {
          textDocument: { uri: localOnlyUri },
          range: { start: { line: 0, character: 0 }, end: { line: 7, character: 0 } },
        });
        const serializedLocalHints = JSON.stringify(localHints);
        expect(serializedLocalHints).not.toContain("(global) As Variant");
        expect(serializedLocalHints).toContain("(local) As Variant");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("keeps include-aware inlay markers after a non-include full reparse edit", async () => {
      const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-include-marker-reuse-"));
      const include = path.join(tempDir, "shared.inc");
      fs.writeFileSync(include, `<%\na = 1\n%>`, "utf8");
      const source = `<!-- #include file="shared.inc" -->
<div>top</div>
<%
Response.Write a
a = 2
%>`;
      const uri = pathToFileURL(path.join(tempDir, "default.asp")).href;
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: pathToFileURL(tempDir).href,
          capabilities: {},
        });
        server.notify("workspace/didChangeConfiguration", {
          settings: {
            aspLsp: {
              diagnostics: { debounceMs: 0 },
              inlayHints: {
                functionReturnTypes: true,
                globalVariableMarkers: "all",
                variableTypes: true,
              },
            },
          },
        });
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        await server.waitForNotification("textDocument/publishDiagnostics");
        await server.request("textDocument/diagnostic", {
          textDocument: { uri },
        });

        const warmedHints = await server.request("textDocument/inlayHint", {
          textDocument: { uri },
          range: {
            start: { line: 0, character: 0 },
            end: positionAt(source, source.length),
          },
        });
        const serializedWarmedHints = JSON.stringify(warmedHints);
        expect(serializedWarmedHints).not.toContain("(global) As Number");
        expect(serializedWarmedHints).not.toContain("(?)");

        const editedSource = notifyRangedReplacement(
          server,
          uri,
          source,
          2,
          "<div>top</div>",
          '<div data-note="<script>">top</div>',
        );
        const immediateHints = await server.request("textDocument/inlayHint", {
          textDocument: { uri },
          range: {
            start: { line: 0, character: 0 },
            end: positionAt(editedSource, editedSource.length),
          },
        });
        const serializedImmediateHints = JSON.stringify(immediateHints);
        expect(serializedImmediateHints).not.toContain("(global) As Number");
        expect(serializedImmediateHints).not.toContain("(?)");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("reads include summaries for the first inlay request without diagnostics warmup", async () => {
      const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-include-first-inlay-"));
      const include = path.join(tempDir, "shared.inc");
      fs.writeFileSync(include, `<%\na = 1\nsharedTitle = "include"\n%>`, "utf8");
      const source = `<!-- #include file="shared.inc" -->
<%
Response.Write sharedTitle
a = 2
Sub Render()
  b = "local"
End Sub
%>`;
      const uri = pathToFileURL(path.join(tempDir, "default.asp")).href;
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: pathToFileURL(tempDir).href,
          capabilities: {},
        });
        server.notify("workspace/didChangeConfiguration", {
          settings: {
            aspLsp: {
              diagnostics: { debounceMs: 10_000 },
              inlayHints: {
                functionReturnTypes: true,
                globalVariableMarkers: "all",
                variableTypes: true,
              },
            },
          },
        });
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });

        const firstHints = await server.request("textDocument/inlayHint", {
          textDocument: { uri },
          range: {
            start: { line: 0, character: 0 },
            end: positionAt(source, source.length),
          },
        });
        const serializedHints = JSON.stringify(firstHints);
        expect(serializedHints).not.toContain("(?)");
        expect(serializedHints).not.toContain("(global) As Number");
        expect(serializedHints).toContain("(local) As String");

        const hover = await server.request("textDocument/hover", {
          textDocument: { uri },
          position: positionAt(source, source.indexOf("sharedTitle")),
        });
        expect(JSON.stringify(hover)).toContain("(global) Dim sharedTitle As String");
        expect(JSON.stringify(hover)).toContain("shared.inc");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
        fs.rmSync(tempDir, { recursive: true, force: true });
      }
    });

    it("jumps from assignments to include-defined implicit globals", async () => {
      const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-include-global-"));
      const include = path.join(tempDir, "shared.inc");
      const owner = path.join(tempDir, "default.asp");
      fs.writeFileSync(include, `<%\nsharedTitle = "include"\n%>`, "utf8");
      const source = `<!-- #include file="shared.inc" -->
<%
sharedTitle = "page"
Function Render()
  sharedTitle = "function"
End Function
Class Widget
  Public Sub Save()
    sharedTitle = "method"
  End Sub
End Class
%>`;
      fs.writeFileSync(owner, source, "utf8");
      const uri = pathToFileURL(owner).href;
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: pathToFileURL(tempDir).href,
          capabilities: {},
        });
        server.notify("workspace/didChangeConfiguration", {
          settings: { aspLsp: { diagnostics: { debounceMs: 0 } } },
        });
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        await server.waitForNotification("textDocument/publishDiagnostics");
        await server.request("textDocument/diagnostic", {
          textDocument: { uri },
        });

        const assignmentOffsets = [
          source.indexOf("sharedTitle ="),
          source.indexOf("sharedTitle =", source.indexOf("Function Render")),
          source.lastIndexOf("sharedTitle ="),
        ];
        for (const offset of assignmentOffsets) {
          const definition = await waitForDefinitionContaining(
            server,
            { uri, position: positionAt(source, offset) },
            "shared.inc",
          );
          const serialized = JSON.stringify(definition);
          expect(serialized).toContain("shared.inc");
          expect(serialized).not.toContain("default.asp");
        }

        const hints = await server.request("textDocument/inlayHint", {
          textDocument: { uri },
          range: {
            start: { line: 0, character: 0 },
            end: positionAt(source, source.length),
          },
        });
        const serializedHints = JSON.stringify(hints);
        expect(serializedHints).not.toContain("(global) As String");
        expect(serializedHints).not.toContain("(local) As String");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
        fs.rmSync(tempDir, { recursive: true, force: true });
      }
    });

    it("honors the implicit ByRef inlay hint setting", async () => {
      const source = `<%
Function BuildName(firstName)
End Function
Response.Write BuildName("Ada")
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
          settings: { aspLsp: { inlayHints: { implicitByRef: false } } },
        });
        const uri = "file:///tmp/implicit-byref-disabled.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        await server.waitForNotification("textDocument/publishDiagnostics");

        const inlayHints = await server.request("textDocument/inlayHint", {
          textDocument: { uri },
          range: { start: { line: 0, character: 0 }, end: { line: 5, character: 0 } },
        });
        const serialized = JSON.stringify(inlayHints);
        expect(serialized).not.toContain("ByRef");
        expect(serialized).toContain("firstName:");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("does not show call-site parameter name hints on VBScript declarations", async () => {
      const source = `<%
Function RenderCustomerRows(ByVal customerList, activeCustomerId)
End Function
Response.Write RenderCustomerRows(customers, activeId)
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
          settings: { aspLsp: { inlayHints: { implicitByRef: true } } },
        });
        const uri = "file:///tmp/formal-parameter-inlay.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        await server.waitForNotification("textDocument/publishDiagnostics");

        const declarationHints = await server.request("textDocument/inlayHint", {
          textDocument: { uri },
          range: { start: { line: 1, character: 0 }, end: { line: 2, character: 0 } },
        });
        const serializedDeclarationHints = JSON.stringify(declarationHints);
        expect(serializedDeclarationHints).not.toContain("customerList:");
        expect(serializedDeclarationHints).not.toContain("activeCustomerId:");
        expect(serializedDeclarationHints).toContain('"label":"ByRef "');
        expect(serializedDeclarationHints).toContain('"paddingRight":false');

        const callHints = await server.request("textDocument/inlayHint", {
          textDocument: { uri },
          range: { start: { line: 3, character: 0 }, end: { line: 4, character: 0 } },
        });
        const serializedCallHints = JSON.stringify(callHints);
        expect(serializedCallHints).toContain("customerList:");
        expect(serializedCallHints).toContain("activeCustomerId:");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("honors the VBScript parameter name inlay hint setting", async () => {
      const source = `<%
Function BuildName(firstName)
End Function
Response.Write BuildName("Ada")
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
          settings: { aspLsp: { inlayHints: { implicitByRef: true, parameterNames: false } } },
        });
        const uri = "file:///tmp/parameter-name-inlay-disabled.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        await server.waitForNotification("textDocument/publishDiagnostics");

        const inlayHints = await server.request("textDocument/inlayHint", {
          textDocument: { uri },
          range: { start: { line: 0, character: 0 }, end: { line: 5, character: 0 } },
        });
        const serialized = JSON.stringify(inlayHints);
        expect(serialized).not.toContain("firstName:");
        expect(serialized).toContain('"label":"ByRef "');
        expect(serialized).toContain('"paddingRight":false');

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
        await server.request("workspace/executeCommand", {
          command: "aspLsp.server.reindexWorkspace",
        });
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

    it("builds a graph for the active document include tree and VB index", async () => {
      const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-document-graph-"));
      const page = path.join(tempDir, "default.asp");
      fs.writeFileSync(path.join(tempDir, "common.inc"), `<%\nConst IncludedValue = 1\n%>`, "utf8");
      const uri = `file://${page}`;
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
            uri,
            languageId: "classic-asp",
            version: 1,
            text: `<!-- #include file="common.inc" -->
<!-- #include file="missing.inc" -->
<%
Dim PageValue
Function Render(value)
  PageValue = value
  Render = value
End Function
Sub Main()
  Render "x"
  MissingName
End Sub
%>`,
          },
        });

        const graph = (await server.request("workspace/executeCommand", {
          command: "aspLsp.server.buildGraph",
          arguments: [{ scope: "document", uri }],
        })) as {
          scope?: string;
          rootUri?: string;
          nodes?: Array<Record<string, unknown>>;
          links?: Array<Record<string, unknown>>;
          stats?: Record<string, unknown>;
        };

        expect(graph.scope).toBe("document");
        expect(graph.rootUri).toBe(uri);
        expect(
          graph.nodes?.some((node) => node.kind === "file" && node.label === "default.asp"),
        ).toBe(true);
        expect(
          graph.nodes?.some(
            (node) => node.kind === "file" && node.label === "default.asp" && node.isRoot === true,
          ),
        ).toBe(true);
        expect(
          graph.nodes?.some((node) => node.kind === "file" && node.label === "common.inc"),
        ).toBe(true);
        expect(
          graph.nodes?.some(
            (node) => node.kind === "file" && node.label === "common.inc" && node.isRoot === true,
          ),
        ).toBe(false);
        expect(graph.nodes?.some((node) => node.kind === "file" && node.exists === false)).toBe(
          true,
        );
        expect(
          graph.nodes?.some((node) => node.kind === "vbDeclaration" && node.label === "Render"),
        ).toBe(true);
        expect(
          graph.nodes?.some((node) => node.kind === "vbUnresolved" && node.label === "MissingName"),
        ).toBe(true);
        expect(graph.links?.some((link) => link.kind === "include")).toBe(true);
        expect(graph.links?.some((link) => link.kind === "declares")).toBe(true);
        expect(graph.links?.some((link) => link.kind === "references")).toBe(true);
        const renderNode = graph.nodes?.find(
          (node) => node.kind === "vbDeclaration" && node.label === "Render",
        );
        expect(
          graph.links?.some(
            (link) =>
              link.kind === "references" &&
              link.source === renderNode?.id &&
              link.target === renderNode?.id,
          ),
        ).toBe(false);
        expect(graph.links?.some((link) => link.kind === "calls")).toBe(true);
        expect(graph.links?.some((link) => link.kind === "unresolvedReference")).toBe(true);
        expect(graph.stats?.missingIncludes).toBe(1);

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
        fs.rmSync(tempDir, { recursive: true, force: true });
      }
    });

    it("filters graph nodes and links from graph settings", async () => {
      const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-graph-settings-"));
      fs.writeFileSync(
        path.join(tempDir, "common.inc"),
        `<%
Dim IncludedGlobal
Const IncludedConst = 5
Sub Shared()
End Sub
%>`,
        "utf8",
      );
      const owner = path.join(tempDir, "default.asp");
      const uri = `file://${owner}`;
      fs.writeFileSync(
        owner,
        `<!-- #include file="common.inc" -->
<!-- #include file="missing.inc" -->
<%
Dim GlobalValue
Const GlobalConst = 1
Class Customer
  Public Const Kind = "retail"
  Public Name
  Public Function BuildLabel()
    BuildLabel = Name
  End Function
  Public Sub Save(item)
    Dim localValue
    Const localConst = 2
    Response.Write CStr(item)
    Repository.Find item
    localValue = localConst
  End Sub
End Class
Function Render(value)
  Dim localRender
  Const localRenderConst = 3
  Render = value
End Function
Sub Main(arg)
  Dim localMain
  Const localMainConst = 4
  If arg Then
    localMain = 1
  End If
  localMain = arg
  localMain("value") = arg
  localMain = IncludedGlobal
  localMain = IncludedConst
  localMain = Err.Number
  Call Shared()
  Err.Clear
  Response.Write CStr(GlobalConst)
  Repository.Find arg
  MissingName
End Sub
%>`,
        "utf8",
      );
      const server = new RpcServer();
      const vbscriptSettings = {
        globals: {
          Repository: "RepositoryType",
        },
        comTypes: {
          RepositoryType: {
            members: {
              Find: {
                kind: "method",
                returnType: "Variant",
                parameters: [{ name: "id", type: "String" }],
              },
            },
          },
        },
      };
      const allGraphSettings = {
        showBuiltinSymbols: true,
        showConfiguredGlobals: true,
        showConfiguredComTypes: true,
        showObjectMembers: true,
        showFunctionParameters: true,
        showLocalVariables: true,
        showLocalConstants: true,
        showClassFields: true,
        showClassMethods: true,
        showClassProperties: true,
        showClassConstants: true,
      };
      type TestGraph = {
        nodes?: Array<Record<string, unknown>>;
        links?: Array<Record<string, unknown>>;
        stats?: Record<string, unknown>;
      };
      const configure = (graph?: Record<string, unknown>) => {
        server.notify("workspace/didChangeConfiguration", {
          settings: {
            aspLsp: {
              vbscript: vbscriptSettings,
              diagnostics: { debounceMs: 0 },
              graph,
            },
          },
        });
      };
      const buildGraph = async (): Promise<TestGraph> =>
        (await server.request("workspace/executeCommand", {
          command: "aspLsp.server.buildGraph",
          arguments: [{ scope: "document", uri }],
        })) as TestGraph;
      const hasNode = (graph: TestGraph, predicate: (node: Record<string, unknown>) => boolean) =>
        (graph.nodes ?? []).some(predicate);
      const hasLink = (graph: TestGraph, predicate: (link: Record<string, unknown>) => boolean) =>
        (graph.links ?? []).some(predicate);
      const nodeByLabel = (graph: TestGraph, label: string) =>
        (graph.nodes ?? []).find((node) => node.label === label);
      const hasGraphLink = (
        graph: TestGraph,
        kind: string,
        sourceLabel: string,
        targetLabel: string,
      ) => {
        const source = nodeByLabel(graph, sourceLabel);
        const target = nodeByLabel(graph, targetLabel);
        return Boolean(
          source &&
          target &&
          hasLink(
            graph,
            (link) => link.kind === kind && link.source === source.id && link.target === target.id,
          ),
        );
      };
      const hasDeclaresLink = (graph: TestGraph, sourceLabel: string, targetLabel: string) =>
        hasGraphLink(graph, "declares", sourceLabel, targetLabel);
      const expectNode = (graph: TestGraph, label: string, expected: Record<string, unknown>) => {
        expect(nodeByLabel(graph, label)).toEqual(expect.objectContaining(expected));
      };

      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: `file://${tempDir}`,
          capabilities: {},
        });

        configure();
        const defaultGraph = await buildGraph();
        expectNode(defaultGraph, "default.asp", { kind: "file", isRoot: true });
        expectNode(defaultGraph, "common.inc", { kind: "file" });
        expect(nodeByLabel(defaultGraph, "common.inc")).not.toHaveProperty("isRoot", true);
        expect(hasNode(defaultGraph, (node) => node.label === "GlobalValue")).toBe(true);
        expect(hasNode(defaultGraph, (node) => node.label === "GlobalConst")).toBe(true);
        expect(hasNode(defaultGraph, (node) => node.label === "IncludedGlobal")).toBe(true);
        expect(hasNode(defaultGraph, (node) => node.label === "IncludedConst")).toBe(true);
        expect(hasNode(defaultGraph, (node) => node.label === "Customer.Name")).toBe(false);
        expect(hasNode(defaultGraph, (node) => node.label === "Customer.Save")).toBe(false);
        expect(hasNode(defaultGraph, (node) => node.label === "Customer.Kind")).toBe(false);
        expect(hasNode(defaultGraph, (node) => node.label === "localValue")).toBe(false);
        expect(hasNode(defaultGraph, (node) => node.label === "localConst")).toBe(false);
        expect(hasNode(defaultGraph, (node) => node.label === "localMain")).toBe(false);
        expect(hasNode(defaultGraph, (node) => node.label === "arg")).toBe(false);
        expect(hasNode(defaultGraph, (node) => node.label === "Response")).toBe(false);
        expect(hasNode(defaultGraph, (node) => node.label === "Response.Write")).toBe(false);
        expect(hasNode(defaultGraph, (node) => node.label === "CStr")).toBe(false);
        expect(hasNode(defaultGraph, (node) => node.label === "Repository")).toBe(false);
        expect(hasNode(defaultGraph, (node) => node.label === "RepositoryType.Find")).toBe(false);
        expect(hasNode(defaultGraph, (node) => node.role === "member")).toBe(false);
        expect(hasLink(defaultGraph, (link) => link.role === "member")).toBe(false);
        expect(
          hasNode(
            defaultGraph,
            (node) => node.kind === "vbUnresolved" && node.label === "IncludedGlobal",
          ),
        ).toBe(false);
        expect(
          hasNode(
            defaultGraph,
            (node) => node.kind === "vbUnresolved" && node.label === "IncludedConst",
          ),
        ).toBe(false);
        expect(
          hasNode(defaultGraph, (node) => node.kind === "vbUnresolved" && node.label === "Shared"),
        ).toBe(false);
        expect(
          hasNode(defaultGraph, (node) => node.kind === "vbUnresolved" && node.label === "Err"),
        ).toBe(false);
        expect(hasNode(defaultGraph, (node) => node.label === "MissingName")).toBe(true);

        configure(allGraphSettings);
        const visibleGraph = await buildGraph();
        expectNode(visibleGraph, "GlobalValue", {
          declarationKind: "variable",
          bindingScope: "global",
          origin: "source",
        });
        expectNode(visibleGraph, "GlobalConst", {
          declarationKind: "constant",
          bindingScope: "global",
          origin: "source",
        });
        expectNode(visibleGraph, "localValue", {
          declarationKind: "variable",
          bindingScope: "local",
          origin: "source",
        });
        expectNode(visibleGraph, "localConst", {
          declarationKind: "constant",
          bindingScope: "local",
          origin: "source",
        });
        expectNode(visibleGraph, "arg", { declarationKind: "parameter", bindingScope: "local" });
        expectNode(visibleGraph, "Customer", { declarationKind: "class", origin: "source" });
        expectNode(visibleGraph, "Render", { declarationKind: "function", origin: "source" });
        expectNode(visibleGraph, "Shared", { declarationKind: "sub", origin: "source" });
        expectNode(visibleGraph, "Customer.Save", {
          declarationKind: "method",
          memberOf: "Customer",
          procedureKind: "sub",
          origin: "source",
        });
        expectNode(visibleGraph, "Customer.BuildLabel", {
          declarationKind: "method",
          memberOf: "Customer",
          procedureKind: "function",
          origin: "source",
        });
        expect(hasNode(visibleGraph, (node) => node.label === "Customer.Name")).toBe(true);
        expect(hasNode(visibleGraph, (node) => node.label === "Customer.Save")).toBe(true);
        expect(hasNode(visibleGraph, (node) => node.label === "Customer.Kind")).toBe(true);
        expect(hasNode(visibleGraph, (node) => node.label === "localValue")).toBe(true);
        expect(hasNode(visibleGraph, (node) => node.label === "localConst")).toBe(true);
        expect(hasNode(visibleGraph, (node) => node.label === "arg")).toBe(true);
        expect(hasDeclaresLink(visibleGraph, "GlobalValue", "default.asp")).toBe(true);
        expect(hasDeclaresLink(visibleGraph, "GlobalConst", "default.asp")).toBe(true);
        expect(hasDeclaresLink(visibleGraph, "IncludedGlobal", "common.inc")).toBe(true);
        expect(hasDeclaresLink(visibleGraph, "IncludedConst", "common.inc")).toBe(true);
        expect(hasDeclaresLink(visibleGraph, "Customer.Name", "Customer")).toBe(true);
        expect(hasDeclaresLink(visibleGraph, "Customer.Kind", "Customer")).toBe(true);
        expect(hasDeclaresLink(visibleGraph, "Customer.Save", "Customer")).toBe(true);
        expect(hasDeclaresLink(visibleGraph, "localValue", "Customer.Save")).toBe(true);
        expect(hasDeclaresLink(visibleGraph, "localConst", "Customer.Save")).toBe(true);
        expect(hasDeclaresLink(visibleGraph, "arg", "Main")).toBe(true);
        expect(hasDeclaresLink(visibleGraph, "localMain", "Main")).toBe(true);
        expect(hasDeclaresLink(visibleGraph, "localMainConst", "Main")).toBe(true);
        expect(hasGraphLink(visibleGraph, "references", "Main", "arg")).toBe(true);
        expect(hasGraphLink(visibleGraph, "references", "Main", "localMain")).toBe(true);
        expect(hasGraphLink(visibleGraph, "references", "Customer.Save", "localConst")).toBe(true);
        expect(hasGraphLink(visibleGraph, "references", "Main", "IncludedGlobal")).toBe(true);
        expect(hasGraphLink(visibleGraph, "references", "Main", "IncludedConst")).toBe(true);
        expect(hasGraphLink(visibleGraph, "references", "Main", "Err")).toBe(true);
        expect(hasGraphLink(visibleGraph, "calls", "Main", "Shared")).toBe(true);
        expect(hasGraphLink(visibleGraph, "calls", "Main", "ErrObject.Clear")).toBe(true);
        expect(hasGraphLink(visibleGraph, "calls", "Main", "Response.Write")).toBe(true);
        expect(hasGraphLink(visibleGraph, "calls", "Main", "CStr")).toBe(true);
        expect(
          hasNode(
            visibleGraph,
            (node) =>
              node.label === "Err" && node.origin === "builtin" && node.externalKind === "object",
          ),
        ).toBe(true);
        expect(
          hasNode(
            visibleGraph,
            (node) =>
              node.label === "Response" &&
              node.origin === "builtin" &&
              node.externalKind === "object",
          ),
        ).toBe(true);
        expect(
          hasNode(
            visibleGraph,
            (node) =>
              node.label === "Response.Write" &&
              node.origin === "builtin" &&
              node.externalKind === "member",
          ),
        ).toBe(true);
        expect(hasNode(visibleGraph, (node) => node.label === "CStr")).toBe(true);
        expect(
          hasNode(
            visibleGraph,
            (node) =>
              node.label === "Repository" &&
              node.origin === "configured" &&
              node.group === "configuredGlobal",
          ),
        ).toBe(true);
        expect(
          hasNode(
            visibleGraph,
            (node) =>
              node.label === "RepositoryType.Find" &&
              node.origin === "configured" &&
              node.externalKind === "member",
          ),
        ).toBe(true);

        configure({ ...allGraphSettings, showIncludeLinks: false });
        expect(hasLink(await buildGraph(), (link) => link.kind === "include")).toBe(false);
        configure({ ...allGraphSettings, showDeclarationLinks: false });
        expect(hasLink(await buildGraph(), (link) => link.kind === "declares")).toBe(false);
        configure({ ...allGraphSettings, showReferenceLinks: false });
        expect(hasLink(await buildGraph(), (link) => link.kind === "references")).toBe(false);
        configure({ ...allGraphSettings, showCallLinks: false });
        expect(hasLink(await buildGraph(), (link) => link.kind === "calls")).toBe(false);
        configure({ ...allGraphSettings, showUnresolvedReferences: false });
        const noUnresolvedGraph = await buildGraph();
        expect(hasNode(noUnresolvedGraph, (node) => node.kind === "vbUnresolved")).toBe(false);
        expect(hasLink(noUnresolvedGraph, (link) => link.kind === "unresolvedReference")).toBe(
          false,
        );
        configure({ ...allGraphSettings, showMissingFiles: false });
        const noMissingGraph = await buildGraph();
        expect(
          hasNode(noMissingGraph, (node) => node.kind === "file" && node.exists === false),
        ).toBe(false);
        expect(
          hasLink(
            noMissingGraph,
            (link) =>
              link.kind === "include" &&
              typeof link.include === "object" &&
              link.include !== null &&
              (link.include as Record<string, unknown>).exists === false,
          ),
        ).toBe(false);

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
        fs.rmSync(tempDir, { recursive: true, force: true });
      }
    });

    it("builds a workspace graph from unopened ASP files", async () => {
      const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-workspace-graph-"));
      fs.writeFileSync(
        path.join(tempDir, "default.asp"),
        `<!-- #include file="common.inc" -->\n<%\nSub PageEntry()\nEnd Sub\n%>`,
        "utf8",
      );
      fs.writeFileSync(
        path.join(tempDir, "common.inc"),
        `<%\nFunction SharedEntry()\nEnd Function\n%>`,
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

        const graph = (await server.request("workspace/executeCommand", {
          command: "aspLsp.server.buildGraph",
          arguments: [{ scope: "workspace" }],
        })) as {
          scope?: string;
          nodes?: Array<Record<string, unknown>>;
          links?: Array<Record<string, unknown>>;
          stats?: Record<string, unknown>;
        };

        expect(graph.scope).toBe("workspace");
        expect(
          graph.nodes?.some((node) => node.kind === "file" && node.label === "default.asp"),
        ).toBe(true);
        expect(
          graph.nodes?.some((node) => node.kind === "file" && node.label === "common.inc"),
        ).toBe(true);
        expect(
          graph.nodes?.some((node) => node.kind === "vbDeclaration" && node.label === "PageEntry"),
        ).toBe(true);
        expect(
          graph.nodes?.some(
            (node) => node.kind === "vbDeclaration" && node.label === "SharedEntry",
          ),
        ).toBe(true);
        expect(graph.links?.some((link) => link.kind === "include")).toBe(true);
        expect(typeof graph.stats?.files).toBe("number");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
        fs.rmSync(tempDir, { recursive: true, force: true });
      }
    });

    it("restores graph include refs and VB symbol indexes from disk cache after server restart", async () => {
      const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-graph-cache-"));
      const cacheDir = path.join(tempDir, ".cache");
      fs.writeFileSync(
        path.join(tempDir, "default.asp"),
        `<!-- #include file="common.inc" -->
<%
Function Render(value)
  Render = value
End Function
Sub PageEntry()
  Render "x"
End Sub
%>`,
        "utf8",
      );
      fs.writeFileSync(
        path.join(tempDir, "common.inc"),
        `<%
Sub SharedEntry()
End Sub
%>`,
        "utf8",
      );
      const settings = {
        aspLsp: {
          debug: { output: "verbose" },
          cache: { enabled: true, directory: cacheDir },
          diagnostics: { debounceMs: 0 },
        },
      };
      const normalizeGraph = (graph: {
        nodes?: Array<Record<string, unknown>>;
        links?: Array<Record<string, unknown>>;
        stats?: Record<string, unknown>;
      }) => ({
        nodes: (graph.nodes ?? [])
          .map((node) => ({
            id: node.id,
            kind: node.kind,
            label: node.label,
            exists: node.exists,
          }))
          .sort((left, right) => String(left.id).localeCompare(String(right.id))),
        links: (graph.links ?? [])
          .map((link) => ({
            source: link.source,
            target: link.target,
            kind: link.kind,
            label: link.label,
            role: link.role,
            count: link.count,
          }))
          .sort((left, right) => JSON.stringify(left).localeCompare(JSON.stringify(right))),
        stats: graph.stats,
      });
      let server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: `file://${tempDir}`,
          capabilities: {},
        });
        server.notify("workspace/didChangeConfiguration", { settings });
        const firstGraph = (await server.request("workspace/executeCommand", {
          command: "aspLsp.server.buildGraph",
          arguments: [{ scope: "workspace" }],
        })) as {
          nodes?: Array<Record<string, unknown>>;
          links?: Array<Record<string, unknown>>;
          stats?: Record<string, unknown>;
        };
        expect(firstGraph.links?.some((link) => link.kind === "include")).toBe(true);
        expect(
          firstGraph.nodes?.some(
            (node) => node.kind === "vbDeclaration" && node.label === "PageEntry",
          ),
        ).toBe(true);
        await waitForLogContaining(server, "diskIncludeRefs.write");
        await waitForLogContaining(server, "graphVbIndex.write");
        await server.request("shutdown", null);
        server.notify("exit", undefined);
        server.stop();

        server = new RpcServer();
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: `file://${tempDir}`,
          capabilities: {},
        });
        server.notify("workspace/didChangeConfiguration", { settings });
        const secondGraph = (await server.request("workspace/executeCommand", {
          command: "aspLsp.server.buildGraph",
          arguments: [{ scope: "workspace" }],
        })) as {
          nodes?: Array<Record<string, unknown>>;
          links?: Array<Record<string, unknown>>;
          stats?: Record<string, unknown>;
        };
        expect(normalizeGraph(secondGraph)).toEqual(normalizeGraph(firstGraph));
        await waitForLogContaining(server, "diskIncludeRefs.hit");
        await waitForLogContaining(server, "graphVbIndex.hit");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
        fs.rmSync(tempDir, { recursive: true, force: true });
      }
    });

    it("restores workspace diagnostics from disk cache and clears it by command", async () => {
      const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-workspace-cache-"));
      const cacheDir = path.join(tempDir, ".cache");
      fs.writeFileSync(
        path.join(tempDir, "broken.asp"),
        `<%\nOption Explicit\nResponse.Write missingName\n%>`,
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
        server.notify("workspace/didChangeConfiguration", {
          settings: {
            aspLsp: {
              checkJs: true,
              debug: { output: "verbose" },
              cache: { enabled: true, directory: cacheDir },
            },
          },
        });
        const first = await server.request("workspace/diagnostic", { previousResultIds: [] });
        expect(JSON.stringify(first)).toContain("missingName");
        await waitForLogContaining(server, "diskCache.write");
        server.takePendingNotifications("window/logMessage");

        const second = await server.request("workspace/diagnostic", { previousResultIds: [] });
        expect(JSON.stringify(second)).toContain("missingName");
        await waitForLogContaining(server, "diskCache.hit");

        const processClear = await server.request("workspace/executeCommand", {
          command: "aspLsp.server.clearProcessCache",
        });
        expect(processClear).toEqual({ ok: true, cleared: "process" });
        server.takePendingNotifications("window/logMessage");
        const third = await server.request("workspace/diagnostic", { previousResultIds: [] });
        expect(JSON.stringify(third)).toContain("missingName");
        await waitForLogContaining(server, "diskCache.hit");

        const diskClear = await server.request("workspace/executeCommand", {
          command: "aspLsp.server.clearDiskCache",
        });
        expect(diskClear).toEqual({ ok: true, cleared: "disk" });
        server.takePendingNotifications("window/logMessage");
        const fourth = await server.request("workspace/diagnostic", { previousResultIds: [] });
        expect(JSON.stringify(fourth)).toContain("missingName");
        await waitForLogContaining(server, "diskCache.miss");

        const allClear = await server.request("workspace/executeCommand", {
          command: "aspLsp.server.clearCache",
        });
        expect(allClear).toEqual({ ok: true, cleared: "all" });
        server.takePendingNotifications("window/logMessage");
        const fifth = await server.request("workspace/diagnostic", { previousResultIds: [] });
        expect(JSON.stringify(fifth)).toContain("missingName");
        await waitForLogContaining(server, "diskCache.miss");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
        fs.rmSync(tempDir, { recursive: true, force: true });
      }
    });

    it("invalidates disk cache when include dependencies change", async () => {
      const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-include-cache-"));
      const cacheDir = path.join(tempDir, ".cache");
      const owner = path.join(tempDir, "default.asp");
      const include = path.join(tempDir, "shared.inc");
      fs.writeFileSync(
        owner,
        '<!-- #include file="shared.inc" -->\n<% Response.Write "ok" %>',
        "utf8",
      );
      fs.writeFileSync(include, '<% Const SharedValue = "ok" %>', "utf8");
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: `file://${tempDir}`,
          capabilities: {},
        });
        server.notify("workspace/didChangeConfiguration", {
          settings: {
            aspLsp: {
              debug: { output: "verbose" },
              cache: { enabled: true, directory: cacheDir },
            },
          },
        });

        const first = await server.request("workspace/diagnostic", { previousResultIds: [] });
        expect(JSON.stringify(first)).not.toContain("include.missing");
        await waitForLogContaining(server, "diskCache.write");
        server.takePendingNotifications("window/logMessage");

        fs.rmSync(include);
        server.notify("workspace/didChangeWatchedFiles", {
          changes: [{ uri: `file://${include}`, type: 3 }],
        });
        const second = await server.request("workspace/diagnostic", { previousResultIds: [] });
        expect(JSON.stringify(second)).toContain("include.missing");
        await waitForLogContaining(server, "diskCache.miss");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
        fs.rmSync(tempDir, { recursive: true, force: true });
      }
    });

    it("restores include summaries from disk cache after server restart", async () => {
      const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-summary-cache-"));
      const cacheDir = path.join(tempDir, ".cache");
      const owner = path.join(tempDir, "default.asp");
      const include = path.join(tempDir, "shared.inc");
      fs.writeFileSync(
        owner,
        '<!-- #include file="shared.inc" -->\n<%\nResponse.Write Sha\n%>',
        "utf8",
      );
      fs.writeFileSync(
        include,
        `<%
Function SharedCached()
End Function
%>`,
        "utf8",
      );
      const uri = `file://${owner}`;
      const settings = {
        aspLsp: {
          debug: { output: "verbose" },
          cache: { enabled: true, directory: cacheDir },
          diagnostics: { debounceMs: 0 },
        },
      };
      let server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: `file://${tempDir}`,
          capabilities: {},
        });
        server.notify("workspace/didChangeConfiguration", { settings });
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: fs.readFileSync(owner, "utf8"),
          },
        });
        await server.request("textDocument/completion", {
          textDocument: { uri },
          position: { line: 2, character: "Response.Write Sha".length },
        });
        await waitForLogContaining(server, "diskSummary.write");
        await server.request("shutdown", null);
        server.notify("exit", undefined);
        server.stop();

        server = new RpcServer();
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: `file://${tempDir}`,
          capabilities: {},
        });
        server.notify("workspace/didChangeConfiguration", { settings });
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: fs.readFileSync(owner, "utf8"),
          },
        });
        const completions = await server.request("textDocument/completion", {
          textDocument: { uri },
          position: { line: 2, character: "Response.Write Sha".length },
        });
        expect(completionLabels(completions)).toContain("SharedCached");
        await waitForLogContaining(server, "diskSummary.hit");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
        fs.rmSync(tempDir, { recursive: true, force: true });
      }
    });

    it("restores include refs from disk cache after server restart", async () => {
      const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-include-refs-cache-"));
      const cacheDir = path.join(tempDir, ".cache");
      const owner = path.join(tempDir, "default.asp");
      const include = path.join(tempDir, "loop.inc");
      fs.writeFileSync(owner, '<!-- #include file="loop.inc" -->', "utf8");
      fs.writeFileSync(include, '<!-- #include file="default.asp" -->', "utf8");
      const uri = `file://${owner}`;
      const settings = {
        aspLsp: {
          debug: { output: "verbose" },
          cache: { enabled: true, directory: cacheDir },
          diagnostics: { debounceMs: 0 },
        },
      };
      let server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: `file://${tempDir}`,
          capabilities: {},
        });
        server.notify("workspace/didChangeConfiguration", { settings });
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: fs.readFileSync(owner, "utf8"),
          },
        });
        await waitForDiagnosticsContaining(server, "Include cycle detected");
        await waitForLogContaining(server, "diskIncludeRefs.write");
        await server.request("shutdown", null);
        server.notify("exit", undefined);
        server.stop();

        server = new RpcServer();
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: `file://${tempDir}`,
          capabilities: {},
        });
        server.notify("workspace/didChangeConfiguration", { settings });
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: fs.readFileSync(owner, "utf8"),
          },
        });
        await waitForDiagnosticsContaining(server, "Include cycle detected");
        await waitForLogContaining(server, "diskIncludeRefs.hit");

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

    it("returns a VBScript quick fix for initialized Dim declarations", async () => {
      const source = `<%
Dim value = 1
Response.Write value
%>`;
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: "file:///tmp",
          capabilities: {},
        });
        const uri = "file:///tmp/vb-split-dim.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        const diagnostics = await waitForDiagnosticsContaining(server, "initializers");
        const syntaxDiagnostics = (
          diagnostics.params as { diagnostics: Array<Record<string, unknown>> }
        ).diagnostics.filter((diagnostic) => diagnostic.source === "asp-lsp-vbscript-syntax");
        expect(JSON.stringify(syntaxDiagnostics)).toContain("initializedDeclaration");
        const actions = await server.request("textDocument/codeAction", {
          textDocument: { uri },
          range: {
            start: positionAt(source, source.indexOf("value")),
            end: positionAt(source, source.indexOf("value") + "value".length),
          },
          context: { diagnostics: syntaxDiagnostics, only: ["quickfix"] },
        });
        const serialized = JSON.stringify(actions);
        expect(serialized).toContain("Split initialized Dim declaration");
        expect(serialized).toContain("Dim value\\nvalue = 1");
        expect((actions as unknown[]).length).toBe(1);

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("supports same-line colon style for initialized Dim quick fixes", async () => {
      const source = `<%
Dim value = 1
Response.Write value
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
                initializedDimQuickFixStyle: "sameLineColon",
              },
            },
          },
        });
        const uri = "file:///tmp/vb-split-dim-colon.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        const diagnostics = await waitForDiagnosticsContaining(server, "initializers");
        const syntaxDiagnostics = (
          diagnostics.params as { diagnostics: Array<Record<string, unknown>> }
        ).diagnostics.filter((diagnostic) => diagnostic.source === "asp-lsp-vbscript-syntax");
        const actions = await server.request("textDocument/codeAction", {
          textDocument: { uri },
          range: {
            start: positionAt(source, source.indexOf("value")),
            end: positionAt(source, source.indexOf("value") + "value".length),
          },
          context: { diagnostics: syntaxDiagnostics, only: ["quickfix"] },
        });
        const serialized = JSON.stringify(actions);
        expect(serialized).toContain("Split initialized Dim declaration");
        expect(serialized).toContain("Dim value : value = 1");
        expect(serialized).not.toContain("Dim value\\nvalue = 1");
        expect((actions as unknown[]).length).toBe(1);

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("returns a VBScript quick fix for multi-name Dim declarations", async () => {
      const source = `<%
Dim first, second
Response.Write first
%>`;
      const arraySource = `<%
Dim first, items(1, 2), dynamicItems()
Response.Write first
%>`;
      const unsupportedSource = `<%
Dim single
ReDim first, second
%>`;
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: "file:///tmp",
          capabilities: {},
        });
        const uri = "file:///tmp/vb-split-multi-dim.asp";
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
            start: positionAt(source, source.indexOf("first")),
            end: positionAt(source, source.indexOf("first") + "first".length),
          },
          context: { diagnostics: [], only: ["quickfix"] },
        });
        const serialized = JSON.stringify(actions);
        expect(serialized).toContain("Split Dim declarations");
        expect(serialized).toContain("Dim first\\nDim second");

        const arrayUri = "file:///tmp/vb-split-multi-array-dim.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri: arrayUri,
            languageId: "classic-asp",
            version: 1,
            text: arraySource,
          },
        });
        await server.waitForNotification("textDocument/publishDiagnostics");
        const arrayActions = await server.request("textDocument/codeAction", {
          textDocument: { uri: arrayUri },
          range: {
            start: positionAt(arraySource, arraySource.indexOf("items")),
            end: positionAt(arraySource, arraySource.indexOf("items") + "items".length),
          },
          context: { diagnostics: [], only: ["quickfix"] },
        });
        expect(JSON.stringify(arrayActions)).toContain(
          "Dim first\\nDim items(1, 2)\\nDim dynamicItems()",
        );

        const unsupportedUri = "file:///tmp/vb-split-multi-dim-unsupported.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri: unsupportedUri,
            languageId: "classic-asp",
            version: 1,
            text: unsupportedSource,
          },
        });
        await server.waitForNotification("textDocument/publishDiagnostics");
        const singleActions = await server.request("textDocument/codeAction", {
          textDocument: { uri: unsupportedUri },
          range: {
            start: positionAt(unsupportedSource, unsupportedSource.indexOf("single")),
            end: positionAt(
              unsupportedSource,
              unsupportedSource.indexOf("single") + "single".length,
            ),
          },
          context: { diagnostics: [], only: ["quickfix"] },
        });
        const redimActions = await server.request("textDocument/codeAction", {
          textDocument: { uri: unsupportedUri },
          range: {
            start: positionAt(unsupportedSource, unsupportedSource.indexOf("first")),
            end: positionAt(unsupportedSource, unsupportedSource.indexOf("first") + "first".length),
          },
          context: { diagnostics: [], only: ["quickfix"] },
        });
        expect(JSON.stringify(singleActions)).not.toContain("Split Dim declarations");
        expect(JSON.stringify(redimActions)).not.toContain("Split Dim declarations");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("returns a VBScript documentation generation quick fix without diagnostics", async () => {
      const source = `<%
Function BuildName(first)
  BuildName = first
End Function
%>`;
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: "file:///tmp",
          capabilities: {},
        });
        const uri = "file:///tmp/vb-doc-action.asp";
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
            start: positionAt(source, source.indexOf("BuildName")),
            end: positionAt(source, source.indexOf("BuildName")),
          },
          context: { diagnostics: [], only: ["quickfix"] },
        });
        const serialized = JSON.stringify(actions);
        expect(serialized).toContain("Generate VBScript documentation");
        expect(serialized).toContain("' @param BuildName.first As Variant");
        expect(serialized).toContain("''' <summary>TODO: Describe BuildName.</summary>");
        expect(serialized).toContain("''' <returns>TODO: Describe return value.</returns>");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("does not return split quick fixes for unsupported declaration syntax errors", async () => {
      const source = `<%
Public value = 1
Dim typed As Integer
%>`;
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: "file:///tmp",
          capabilities: {},
        });
        const uri = "file:///tmp/vb-declaration-syntax.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        const diagnostics = await waitForDiagnosticsContaining(server, "As types");
        const syntaxDiagnostics = (
          diagnostics.params as { diagnostics: Array<Record<string, unknown>> }
        ).diagnostics.filter((diagnostic) => diagnostic.source === "asp-lsp-vbscript-syntax");
        expect(JSON.stringify(syntaxDiagnostics)).toContain("initializedDeclaration");
        expect(JSON.stringify(syntaxDiagnostics)).toContain("typedDeclaration");
        const actions = await server.request("textDocument/codeAction", {
          textDocument: { uri },
          range: {
            start: positionAt(source, source.indexOf("Public value")),
            end: positionAt(source, source.indexOf("Public value") + "Public value".length),
          },
          context: { diagnostics: syntaxDiagnostics, only: ["quickfix"] },
        });
        expect(JSON.stringify(actions)).not.toContain("Split initialized Dim declaration");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("returns VBScript quick fixes for invalid procedure call syntax", async () => {
      const source = `<%
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
%>`;
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: "file:///tmp",
          capabilities: {},
        });
        const uri = "file:///tmp/vb-call-syntax.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        const diagnostics = await waitForDiagnosticsContaining(server, "call syntax");
        const syntaxDiagnostics = (
          diagnostics.params as { diagnostics: Array<Record<string, unknown>> }
        ).diagnostics.filter((diagnostic) => diagnostic.source === "asp-lsp-vbscript-syntax");
        expect(JSON.stringify(syntaxDiagnostics)).toContain("callStatementRequiresParentheses");
        expect(JSON.stringify(syntaxDiagnostics)).toContain("expressionCallRequiresParentheses");
        expect(JSON.stringify(syntaxDiagnostics)).toContain(
          "statementCallDisallowsParenthesizedArguments",
        );

        const actions = await server.request("textDocument/codeAction", {
          textDocument: { uri },
          range: {
            start: positionAt(source, source.indexOf("Call Func1")),
            end: positionAt(source, source.indexOf("Call Func1") + "Call Func1".length),
          },
          context: { diagnostics: syntaxDiagnostics, only: ["quickfix"] },
        });
        const serialized = JSON.stringify(actions);
        expect(serialized).toContain("Fix VBScript call syntax");
        expect(serialized).toContain('"newText":"Call Func1(hoge)"');
        expect(serialized).toContain('"newText":"Z = Func1(hoge)"');
        expect(serialized).toContain('"newText":"Func2 hoge, fuga"');
        expect(serialized).toContain('"newText":"Call Func2(hoge, fuga)"');
        expect(serialized).toContain('"newText":"Z = Func2(hoge, fuga)"');

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("does not report statement call syntax diagnostics for ASP expression output", async () => {
      const source = `<%
Function RenderCustomerRows(ByVal customerList, ByVal activeCustomerId)
  RenderCustomerRows = ""
End Function
%>
<%= RenderCustomerRows(filteredCustomers, selectedCustomerId) %>`;
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: "file:///tmp",
          capabilities: {},
        });
        const uri = "file:///tmp/asp-expression-call.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        const diagnostics = await server.waitForNotification("textDocument/publishDiagnostics");
        const serialized = JSON.stringify(diagnostics.params);
        expect(serialized).not.toContain("statementCallDisallowsParenthesizedArguments");
        expect(serialized).not.toContain("VBScript call syntax is invalid");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("returns VBScript completions inside HTML attribute ASP islands", async () => {
      const source = `<input value="<%= Response. %>" <% Response. %>>`;
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: "file:///tmp",
          capabilities: {},
        });
        const uri = "file:///tmp/html-attribute-asp-completion.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        await server.waitForNotification("textDocument/publishDiagnostics");

        for (const offset of [
          source.indexOf("Response.") + "Response.".length,
          source.lastIndexOf("Response.") + "Response.".length,
        ]) {
          const completions = await server.request("textDocument/completion", {
            textDocument: { uri },
            position: positionAt(source, offset),
          });
          expect(completionLabels(completions)).toContain("Write");
        }

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
Const usedValue = 1
Sub Save(usedArg, ByRef unusedArg)
  Dim unusedValue
  Response.Write usedArg
End Sub
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
        const diagnostics = await waitForDiagnosticsContaining(server, "unusedValue");
        const vbDiagnostics = (
          diagnostics.params as { diagnostics: Array<Record<string, unknown>> }
        ).diagnostics;
        expect(JSON.stringify(vbDiagnostics)).toContain("unusedValue");
        expect(
          vbDiagnostics
            .filter((diagnostic) => diagnostic.source === "asp-lsp-vbscript-unused")
            .every((diagnostic) =>
              (diagnostic.tags as unknown[] | undefined)?.includes(DiagnosticTag.Unnecessary),
            ),
        ).toBe(true);
        const valueActions = await server.request("textDocument/codeAction", {
          textDocument: { uri },
          range: {
            start: { line: 1, character: 4 },
            end: { line: 1, character: 15 },
          },
          context: { diagnostics: vbDiagnostics },
        });
        const serialized = JSON.stringify(valueActions);
        expect(serialized).toContain("Remove unused declaration unusedValue");
        expect(serialized).toContain('"newText":""');

        const unusedArgDiagnostic = vbDiagnostics.find((diagnostic) =>
          String(diagnostic.message).includes("unusedArg"),
        );
        expect(unusedArgDiagnostic).toBeDefined();
        const parameterActions = await server.request("textDocument/codeAction", {
          textDocument: { uri },
          range: unusedArgDiagnostic?.range,
          context: { diagnostics: unusedArgDiagnostic ? [unusedArgDiagnostic] : [] },
        });
        const parameterAction = (parameterActions as Array<Record<string, unknown>>).find(
          (action) => JSON.stringify(action).includes("unusedArg"),
        );
        const parameterEdit = ((
          parameterAction?.edit as { changes?: Record<string, TextEdit[]> } | undefined
        )?.changes?.[uri] ?? [])[0];
        expect(parameterEdit).toBeDefined();
        const updated = parameterEdit ? applyTextEdit(source, parameterEdit) : source;
        expect(updated).toContain("Sub Save(usedArg)");
        expect(updated).not.toContain("ByRef unusedArg");
        expect(updated).not.toContain("ByRef )");

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
        const diagnostics = await waitForDiagnosticsContaining(server, "missingName");
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

    it("deduplicates published diagnostics and ignores line continuation underscores", async () => {
      const source = `<%
Option Explicit
Dim message
message = "hello" & _
  missingName
Response.Write missingName
%>`;
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: "file:///tmp",
          capabilities: {},
        });
        const uri = "file:///tmp/vb-diagnostic-dedupe.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        const diagnostics = await waitForDiagnosticsContaining(server, "missingName");
        const vbDiagnostics = (
          diagnostics.params as { diagnostics: Array<Record<string, unknown>> }
        ).diagnostics.filter((diagnostic) => diagnostic.source === "asp-lsp-vbscript");
        expect(JSON.stringify(vbDiagnostics)).not.toContain("'_'");
        const keys = vbDiagnostics.map((diagnostic) =>
          JSON.stringify({
            source: diagnostic.source,
            code: diagnostic.code,
            severity: diagnostic.severity,
            range: diagnostic.range,
            message: diagnostic.message,
          }),
        );
        expect(new Set(keys).size).toBe(keys.length);

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
        server.notify("workspace/didChangeConfiguration", {
          settings: { aspLsp: { vbscript: { identifierCase: "PascalCase" } } },
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
        const diagnostics = await waitForDiagnosticsContaining(server, "Foo");
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
        {
          identifierCase: "UPPERCASE",
          sourceName: "user_name",
          referenceName: "USER_NAME",
          expectedName: "USERNAME",
        },
        {
          identifierCase: "camelCase",
          sourceName: "user_name",
          referenceName: "USER_NAME",
          expectedName: "userName",
        },
        {
          identifierCase: "lowercase",
          sourceName: "user_name",
          referenceName: "USER_NAME",
          expectedName: "username",
        },
        {
          identifierCase: "snake_case",
          sourceName: "userName",
          referenceName: "userName",
          expectedName: "user_name",
        },
        {
          identifierCase: "UPPER_SNAKE",
          sourceName: "user_name",
          referenceName: "USER_NAME",
          expectedName: "USER_NAME",
        },
      ] as const;
      for (const testCase of cases) {
        const source = `<%
Dim ${testCase.sourceName}
Response.Write ${testCase.referenceName}
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
          const diagnostics = await waitForDiagnosticsContaining(server, testCase.expectedName);
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
          expect(serialized).toContain(`Rename ${testCase.sourceName} to ${testCase.expectedName}`);
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

    it("accepts legacy VBScript identifier casing setting aliases", async () => {
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
          settings: {
            aspLsp: {
              vbscript: {
                identifierCase: "camel",
                identifierCaseByKind: { variable: "upperSnake" },
              },
            },
          },
        });
        const uri = "file:///tmp/vb-naming-legacy-alias.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        const diagnostics = await waitForDiagnosticsContaining(server, "USER_NAME");
        expect(JSON.stringify(diagnostics.params)).toContain("USER_NAME");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("uses VBScript identifier casing settings per declaration kind", async () => {
      const source = `<%
Dim selected_customer
Class customer_record
End Class
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
                identifierCaseByKind: { variable: "snake_case", class: "PascalCase" },
              },
            },
          },
        });
        const uri = "file:///tmp/vb-naming-by-kind.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        const diagnostics = await waitForDiagnosticsContaining(server, "CustomerRecord");
        const namingDiagnostics = (
          diagnostics.params as { diagnostics: Array<Record<string, unknown>> }
        ).diagnostics.filter((diagnostic) => diagnostic.source === "asp-lsp-vbscript-naming");
        expect(JSON.stringify(namingDiagnostics)).not.toContain("selected_customer");
        expect(JSON.stringify(namingDiagnostics)).toContain("CustomerRecord");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
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
        server.notify("workspace/didChangeConfiguration", {
          settings: { aspLsp: { vbscript: { identifierCase: "PascalCase" } } },
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
        const diagnostics = await waitForDiagnosticsContaining(server, "CustomerRecord");
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
        server.notify("workspace/didChangeConfiguration", {
          settings: { aspLsp: { vbscript: { identifierCase: "PascalCase" } } },
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
        const diagnostics = await waitForDiagnosticsContaining(server, "DisplayName");
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
        server.notify("workspace/didChangeConfiguration", {
          settings: { aspLsp: { vbscript: { identifierCase: "PascalCase" } } },
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
        const diagnostics = await waitForDiagnosticsContaining(server, "Foo");
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
        server.notify("workspace/didChangeConfiguration", {
          settings: { aspLsp: { vbscript: { identifierCase: "PascalCase" } } },
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
        const diagnostics = await waitForDiagnosticsContaining(server, "Foo");
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
        server.notify("workspace/didChangeConfiguration", {
          settings: { aspLsp: { vbscript: { identifierCase: "PascalCase" } } },
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
        const diagnostics = await waitForDiagnosticsContaining(server, "Foo");
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
        server.notify("workspace/didChangeConfiguration", {
          settings: { aspLsp: { vbscript: { identifierCase: "PascalCase" } } },
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
        const diagnostics = await waitForDiagnosticsContaining(server, "Foo");
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
Dim initialized = 1
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
        const diagnostics = await waitForDiagnosticsContaining(server, "宣言されていません");
        const serializedDiagnostics = JSON.stringify(diagnostics.params);
        expect(serializedDiagnostics).toContain("解決できません");
        expect(serializedDiagnostics).toContain("宣言されていません");
        expect(serializedDiagnostics).toContain("初期値を含められません");

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
        const splitDimActions = await server.request("textDocument/codeAction", {
          textDocument: { uri },
          range: {
            start: positionAt(marked.text, marked.text.indexOf("initialized")),
            end: positionAt(marked.text, marked.text.indexOf("initialized") + "initialized".length),
          },
          context: { diagnostics: [], only: ["quickfix"] },
        });
        expect(JSON.stringify(splitDimActions)).toContain("初期化つき Dim 宣言を分割");

        const codeLens = await server.request("textDocument/codeLens", {
          textDocument: { uri },
        });
        const referencesCodeLens = (codeLens as Array<Record<string, unknown>>).find((lens) => {
          const data = lens.data as { kind?: unknown } | undefined;
          return data?.kind === "vbscript-reference";
        });
        const resolvedCodeLens = await server.request("codeLens/resolve", referencesCodeLens);
        expect(JSON.stringify(resolvedCodeLens)).toContain("件の参照");

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
        const diagnostics = await waitForDiagnosticsContaining(server, "missing.inc");
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

    it("does not suggest includes for undeclared VBScript symbols", async () => {
      const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-include-suggest-removed-"));
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
        const diagnostics = await waitForDiagnosticsContaining(server, "SharedHelper");
        const actions = await server.request("textDocument/codeAction", {
          textDocument: { uri },
          range: { start: marked.position, end: marked.position },
          context: {
            diagnostics: (diagnostics.params as { diagnostics: unknown[] }).diagnostics,
          },
        });
        const serialized = JSON.stringify(actions);
        expect(serialized).toContain("Declare SharedHelper with Dim");
        expect(serialized).not.toContain("Include /inc/helpers.inc for SharedHelper");
        expect(serialized).not.toContain("<!-- #include");

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
        const diagnostics = await waitForDiagnosticsContaining(server, "asp-lsp-typescript-unused");
        const serialized = JSON.stringify(diagnostics.params);
        expect(serialized).toContain("asp-lsp-typescript-unused");
        expect(serialized).toContain("unusedLocal");
        expect(serialized).toContain('"severity":4');
        expect(serialized).toContain(`"tags":[${DiagnosticTag.Unnecessary}]`);

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("maps JavaScript semantic diagnostics directly to source ranges", async () => {
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
              checkJs: true,
              diagnostics: { debounceMs: 0 },
              javascript: { ignoreProjectConfig: true },
            },
          },
        });
        const uri = "file:///tmp/js-semantic-range.asp";
        const source = `<div>before</div>
<script>
const fromAsp = <%= Request("id") %>;
missingThing.toFixed();
</script>`;
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        const diagnostics = await waitForDiagnosticsContaining(server, "missingThing");
        const semanticDiagnostics =
          (
            diagnostics.params as {
              diagnostics?: Array<{
                message?: string;
                range: {
                  start: { line: number; character: number };
                  end: { line: number; character: number };
                };
                source?: string;
              }>;
            }
          ).diagnostics?.filter((diagnostic) => diagnostic.source === "asp-lsp-typescript") ?? [];
        const missingThing = semanticDiagnostics.find((diagnostic) =>
          diagnostic.message?.includes("missingThing"),
        );
        expect(missingThing?.range).toEqual({
          start: { line: 3, character: 0 },
          end: { line: 3, character: "missingThing".length },
        });
        const aspStart = source.indexOf("<%=");
        const aspEnd = source.indexOf("%>", aspStart) + "%>".length;
        expect(
          semanticDiagnostics.every((diagnostic) => {
            const start = offsetAt(source, diagnostic.range.start);
            const end = offsetAt(source, diagnostic.range.end);
            return end <= aspStart || start >= aspEnd;
          }),
        ).toBe(true);

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("does not duplicate the active virtual document in JavaScript worker payloads", async () => {
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
              checkJs: true,
              debug: { output: "verbose" },
              diagnostics: { debounceMs: 0 },
              javascript: { ignoreProjectConfig: true },
            },
          },
        });

        const filler = "x".repeat(24_000);
        const uri = "file:///tmp/js-worker-payload.asp";
        const source = `<script>
const payloadMarker = "${filler}";
missingPayloadName.toFixed();
</script>`;
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });

        const diagnostics = await waitForDiagnosticsContaining(server, "missingPayloadName");
        expect(JSON.stringify(diagnostics.params)).toContain("asp-lsp-typescript");
        const workerLog = await waitForLogContaining(server, "javascript.diagnostics.worker");
        const payloadBytes = jsWorkerPayloadBytesFromLog(workerLog);

        expect(payloadBytes).toBeGreaterThan(filler.length);
        expect(payloadBytes).toBeLessThan(source.length * 1.7);

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("reuses JavaScript diagnostics after HTML-only source shifts", async () => {
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
              debug: { output: "verbose" },
              diagnostics: { debounceMs: 0 },
            },
          },
        });

        const uri = "file:///tmp/js-diagnostics-cache.asp";
        let source = `<div>before</div>
<script>
function demo(unusedParam) {
  const unusedLocal = 1;
  return 1;
}
</script>
<script runat="server" language="JScript">
var broken = ;
</script>`;
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        const firstDiagnostics = await waitForDiagnosticsContaining(
          server,
          "asp-lsp-typescript-unused",
        );
        const firstUnused = diagnosticContaining(firstDiagnostics, "unusedLocal");
        expect(firstUnused?.range.start.line).toBe(3);
        expect(JSON.stringify(firstDiagnostics.params)).toContain("Expression expected");
        await waitForLogContaining(server, "LSP check completed");
        server.takePendingNotifications("window/logMessage");
        server.takePendingNotifications("textDocument/publishDiagnostics");

        source = notifyRangedReplacement(server, uri, source, 2, "before", "before\nshifted");

        const nextDiagnostics = await waitForDiagnosticsContaining(
          server,
          "asp-lsp-typescript-unused",
        );
        const syntaxReuseLog = await waitForLogContaining(server, "check.javascriptSyntax.reuse");
        const diagnosticsReuseLog = await waitForLogContaining(
          server,
          "check.javascriptDiagnostics.reuse",
        );
        const nextUnused = diagnosticContaining(nextDiagnostics, "unusedLocal");
        expect(nextUnused?.range.start.line).toBe(4);
        expect(JSON.stringify(nextDiagnostics.params)).toContain("Expression expected");
        const logs = JSON.stringify([
          syntaxReuseLog,
          diagnosticsReuseLog,
          ...server.takePendingNotifications("window/logMessage"),
        ]);
        expect(logs).not.toContain("check.javascriptSemantic");
        expect(logs).not.toContain("check.javascriptUnused");
        expect(source).toContain("before\nshifted");

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
      fs.writeFileSync(
        path.join(tempDir, "jsconfig.json"),
        JSON.stringify({ include: ["*.js", "*.asp"] }),
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
        const diagnostics = await waitForDiagnosticsContaining(server, "helperThing");
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

    it("returns HTML close-tag edits only for HTML opening tags on >", async () => {
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: "file:///tmp",
          capabilities: {},
        });
        const requestOnType = async (uri: string, text: string): Promise<TextEdit[]> => {
          server.notify("textDocument/didOpen", {
            textDocument: {
              uri,
              languageId: "classic-asp",
              version: 1,
              text,
            },
          });
          await server.waitForNotification("textDocument/publishDiagnostics");
          return (await server.request("textDocument/onTypeFormatting", {
            textDocument: { uri },
            position: positionAt(text, text.length),
            ch: ">",
            options: { tabSize: 2, insertSpaces: true },
          })) as TextEdit[];
        };

        const divEdits = await requestOnType("file:///tmp/tag-complete-div.asp", "<div>");
        expect(divEdits).toEqual([
          {
            range: { start: { line: 0, character: 5 }, end: { line: 0, character: 5 } },
            newText: "</div>",
          },
        ]);

        const brEdits = await requestOnType("file:///tmp/tag-complete-br.asp", "<br>");
        expect(brEdits).toEqual([]);

        const closingEdits = await requestOnType("file:///tmp/tag-complete-close.asp", "</div>");
        expect(closingEdits).toEqual([]);

        const quotedGreaterEdits = await requestOnType(
          "file:///tmp/tag-complete-quoted-greater.asp",
          '<div title=">">',
        );
        expect(quotedGreaterEdits).toEqual([
          {
            range: { start: { line: 0, character: 15 }, end: { line: 0, character: 15 } },
            newText: "</div>",
          },
        ]);

        const aspCloseEdits = await requestOnType(
          "file:///tmp/tag-complete-asp-close.asp",
          '<% Response.Write "ok" %>',
        );
        expect(aspCloseEdits).toEqual([]);

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("does not auto-close apostrophes on type", async () => {
      const server = new RpcServer();
      try {
        await server.start();
        const initialize = await server.request("initialize", {
          processId: process.pid,
          rootUri: "file:///tmp",
          capabilities: {},
        });
        const onTypeFormattingProvider = (
          initialize as {
            capabilities?: {
              documentOnTypeFormattingProvider?: { moreTriggerCharacter?: string[] };
            };
          }
        ).capabilities?.documentOnTypeFormattingProvider;
        expect(onTypeFormattingProvider?.moreTriggerCharacter).not.toContain("'");
        const requestOnType = async (
          uri: string,
          text: string,
          quoteOffset = text.indexOf("'") + 1,
        ): Promise<TextEdit[]> => {
          server.notify("textDocument/didOpen", {
            textDocument: {
              uri,
              languageId: "classic-asp",
              version: 1,
              text,
            },
          });
          await server.waitForNotification("textDocument/publishDiagnostics");
          return (await server.request("textDocument/onTypeFormatting", {
            textDocument: { uri },
            position: positionAt(text, quoteOffset),
            ch: "'",
            options: { tabSize: 2, insertSpaces: true },
          })) as TextEdit[];
        };

        const vbscriptEdits = await requestOnType(
          "file:///tmp/apostrophe-vbscript.asp",
          `<% Response.Write '
%>`,
        );
        expect(vbscriptEdits).toEqual([]);

        const htmlEdits = await requestOnType("file:///tmp/apostrophe-html.asp", "<div title='>");
        expect(htmlEdits).toEqual([]);

        const cssEdits = await requestOnType(
          "file:///tmp/apostrophe-css.asp",
          "<style>.x::before { content: '</style>",
        );
        expect(cssEdits).toEqual([]);

        const javascriptEdits = await requestOnType(
          "file:///tmp/apostrophe-javascript.asp",
          "<script>const value = '</script>",
        );
        expect(javascriptEdits).toEqual([]);

        const jscriptEdits = await requestOnType(
          "file:///tmp/apostrophe-jscript.asp",
          `<%@ LANGUAGE="JScript" %>
<% var value = '
%>`,
        );
        expect(jscriptEdits).toEqual([]);

        const existingCloseEdits = await requestOnType(
          "file:///tmp/apostrophe-existing-close.asp",
          "<script>const value = '';</script>",
        );
        expect(existingCloseEdits).toEqual([]);

        const generatedCloseEdits = await requestOnType(
          "file:///tmp/apostrophe-generated-close.asp",
          "<script>const value = '';</script>",
          "<script>const value = ''".length,
        );
        expect(generatedCloseEdits).toEqual([]);

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
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
        server.notify("workspace/didChangeConfiguration", {
          settings: { aspLsp: { debug: { output: "summary" } } },
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
        await waitForLogContaining(server, "LSP analysis started");
        const analysisCompletedLog = await waitForLogContaining(server, "LSP analysis completed");
        expectElapsedLogWithoutHeat(analysisCompletedLog);
        await waitForLogContaining(server, "LSP check started");
        const checkCompletedLog = await waitForLogContaining(server, "LSP check completed");
        expectElapsedLogWithoutHeat(checkCompletedLog);

        const fullEdits = await server.request("textDocument/formatting", {
          textDocument: { uri },
          options: { tabSize: 2, insertSpaces: true },
        });
        await waitForLogContaining(server, "Formatting conversion started (document)");
        const fullCompletedLog = await waitForLogContaining(
          server,
          "Formatting conversion completed (document)",
        );
        expectElapsedLogWithoutHeat(fullCompletedLog);
        const fullText = JSON.stringify(fullEdits);
        expect(fullText).toContain("<%");
        expect(fullText).toContain("  Response.Write");
        expect(fullText).toContain(".x {");
        expect(fullText).toContain("color: red");

        const islandUri = "file:///tmp/format-embedded-asp-islands.asp";
        const islandSource = `<style>.x{color:<%= themeColor %>;width:<% Response.Write width %>px}</style>
<div style="color:<%= themeColor %>;background:red"></div>
<script>const value=<%= serverValue %>;const dynamic=<% Response.Write clientValue %>;</script>`;
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri: islandUri,
            languageId: "classic-asp",
            version: 1,
            text: islandSource,
          },
        });
        await server.waitForNotification("textDocument/publishDiagnostics");
        const islandEdits = await server.request("textDocument/formatting", {
          textDocument: { uri: islandUri },
          options: { tabSize: 2, insertSpaces: true },
        });
        const islandText = (islandEdits as Array<{ newText?: string }>)[0]?.newText ?? islandSource;
        expect(islandText).toContain("<%= themeColor %>");
        expect(islandText).toContain("<% Response.Write width %>");
        expect(islandText).toContain("<% Response.Write clientValue %>");

        const rangeEdits = await server.request("textDocument/rangeFormatting", {
          textDocument: { uri },
          range: {
            start: { line: 2, character: 0 },
            end: { line: 6, character: 2 },
          },
          options: { tabSize: 2, insertSpaces: true },
        });
        await waitForLogContaining(server, "Formatting conversion started (range)");
        const rangeCompletedLog = await waitForLogContaining(
          server,
          "Formatting conversion completed (range)",
        );
        expectElapsedLogWithoutHeat(rangeCompletedLog);
        const rangeText = JSON.stringify(rangeEdits);
        expect(rangeText).toContain("  Response.Write");
        expect(rangeText).not.toContain("<html>");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("formats embedded languages relative to tag indentation by default", async () => {
      const source = `<html>
<body>
  <style>.x{color:red}</style>
  <script>
function greet(){
console.log("x");
}
  </script>
  <%
If enabled Then
Response.Write "ok"
End If
  %>
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
        server.notify("workspace/didChangeConfiguration", {
          settings: { aspLsp: { format: { indentSize: 2 } } },
        });
        const uri = "file:///tmp/format-tag-indent.asp";
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
        const fullText = (fullEdits as Array<{ newText?: string }>)[0]?.newText ?? source;
        expect(fullText).toContain(`  <style>
    .x {
      color: red
    }
  </style>`);
        expect(fullText).toContain(`  <script>
    function greet() {
      console.log("x");
    }
  </script>`);
        expect(fullText).toContain(`  <%
  If enabled Then
    Response.Write "ok"
  End If
  %>`);

        const rangeEdits = await server.request("textDocument/rangeFormatting", {
          textDocument: { uri },
          range: {
            start: { line: 2, character: 0 },
            end: { line: 3, character: 0 },
          },
          options: { tabSize: 2, insertSpaces: true },
        });
        const rangeText = JSON.stringify(rangeEdits);
        expect(rangeText).toContain(".x {");
        expect(rangeText).not.toContain("<html>");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("can ignore embedded tag indentation per language", async () => {
      const source = `<html>
<body>
  <style>.x{color:red}</style>
  <script>
function greet(){
console.log("x");
}
  </script>
  <%
If enabled Then
Response.Write "ok"
End If
  %>
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

        const formatWithSettings = async (uri: string, format: Record<string, unknown>) => {
          server.notify("workspace/didChangeConfiguration", {
            settings: { aspLsp: { format: { indentSize: 2, ...format } } },
          });
          server.notify("textDocument/didOpen", {
            textDocument: {
              uri,
              languageId: "classic-asp",
              version: 1,
              text: source,
            },
          });
          await server.waitForNotification("textDocument/publishDiagnostics");
          const edits = await server.request("textDocument/formatting", {
            textDocument: { uri },
            options: { tabSize: 2, insertSpaces: true },
          });
          return (edits as Array<{ newText?: string }>)[0]?.newText ?? source;
        };

        const cssIgnored = await formatWithSettings("file:///tmp/format-ignore-css.asp", {
          ignoreCssTagIndent: true,
        });
        expect(cssIgnored).toContain(`  <style>
.x {`);
        expect(cssIgnored).toContain(`  <script>
    function greet() {`);
        expect(cssIgnored).toContain(`  <%
  If enabled Then`);

        const jsIgnored = await formatWithSettings("file:///tmp/format-ignore-js.asp", {
          ignoreJavaScriptTagIndent: true,
        });
        expect(jsIgnored).toContain(`  <style>
    .x {`);
        expect(jsIgnored).toContain(`  <script>
function greet() {`);
        expect(jsIgnored).toContain(`  <%
  If enabled Then`);

        const vbscriptIgnored = await formatWithSettings("file:///tmp/format-ignore-vbs.asp", {
          ignoreVbscriptTagIndent: true,
        });
        expect(vbscriptIgnored).toContain(`  <style>
    .x {`);
        expect(vbscriptIgnored).toContain(`  <script>
    function greet() {`);
        expect(vbscriptIgnored).toContain(`  <%
If enabled Then`);

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("emits verbose LSP timing breakdowns when debug output is verbose", async () => {
      const source = `<html>
<head>
<style>.x{color:red}</style>
<script>
const value = 1;
</script>
</head>
<body>
<% Option Explicit
Dim enabled
enabled = True
Response.Write enabled
%>
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
        server.notify("workspace/didChangeConfiguration", {
          settings: { aspLsp: { debug: { output: "verbose" } } },
        });
        const uri = "file:///tmp/debug-verbose.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        await server.waitForNotification("textDocument/publishDiagnostics");

        for (const expected of [
          "analysis.parse",
          "analysis.virtualDocuments",
          "check.cssDiagnostics",
          "check.javascriptSyntax",
          "check.vbscript.projectContext",
          "check.vbscript.diagnostics",
          "check.javascriptUnused",
          "check.vbscript.diagnostics.unusedSymbols",
        ]) {
          const log = await waitForLogContaining(server, expected);
          expectElapsedLogWithoutHeat(log);
        }

        await server.request("textDocument/formatting", {
          textDocument: { uri },
          options: { tabSize: 2, insertSpaces: true },
        });
        const embeddedLog = await waitForLogContaining(server, "format.embedded");
        expectElapsedLogWithoutHeat(embeddedLog);

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("publishes staged diagnostics from parser to final layers", async () => {
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
              debug: { output: "verbose" },
              diagnostics: { debounceMs: 0 },
            },
          },
        });
        const uri = "file:///tmp/staged-diagnostics.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: `<!-- #include file="missing.inc" -->
<style>.broken { color: }</style>
<%
Option Explicit
Response.Write missingName`,
          },
        });

        const fast = await waitForDiagnosticsPublished(server, uri);
        expect(diagnosticMessages(fast).join("\n")).toContain("closing %>");
        expect(diagnosticText(fast)).not.toContain("missing.inc");
        const include = await waitForDiagnosticsContaining(server, "missing.inc");
        expect(diagnosticMessages(include)).toContain(
          "Include file 'missing.inc' could not be resolved.",
        );
        const syntax = await waitForDiagnosticsContaining(server, "asp-lsp-css");
        expect(diagnosticText(syntax)).toContain("asp-lsp-css");
        const final = await waitForDiagnosticsContaining(server, "missingName");
        expect(diagnosticMessages(final)).toContain(
          "Include file 'missing.inc' could not be resolved.",
        );
        expect(JSON.stringify(final.params)).toContain("missingName");

        const logs = [
          await waitForLogContaining(server, "diagnostics.fast.published"),
          await waitForLogContaining(server, "diagnostics.include.published"),
          await waitForLogContaining(server, "diagnostics.syntax.published"),
          await waitForLogContaining(server, "diagnostics.project.published"),
          await waitForLogContaining(server, "diagnostics.final.published"),
          await waitForLogContaining(server, "LSP check completed"),
        ];
        const logText = logs.map((log) => JSON.stringify(log.params)).join("\n");
        expect(logText.indexOf("diagnostics.fast.published")).toBeLessThan(
          logText.indexOf("diagnostics.include.published"),
        );
        expect(logText.indexOf("diagnostics.include.published")).toBeLessThan(
          logText.indexOf("diagnostics.syntax.published"),
        );
        expect(logText.indexOf("diagnostics.syntax.published")).toBeLessThan(
          logText.indexOf("diagnostics.project.published"),
        );
        expect(logText.indexOf("diagnostics.project.published")).toBeLessThan(
          logText.indexOf("diagnostics.final.published"),
        );

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("does not clear visible diagnostics while change diagnostics are debounced", async () => {
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
              debug: { output: "verbose" },
              diagnostics: { debounceMs: 120 },
            },
          },
        });
        const uri = "file:///tmp/diagnostics-preserve-on-change.asp";
        const initial = `<%
Option Explicit
Response.Write missingName
%>
<div>hello</div>`;
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: initial,
          },
        });

        await waitForDiagnosticsContaining(server, "missingName");
        await waitForLogContaining(server, "LSP check completed");
        server.takePendingNotifications("textDocument/publishDiagnostics");
        server.takePendingNotifications("window/logMessage");

        server.notify("textDocument/didChange", {
          textDocument: { uri, version: 2 },
          contentChanges: [{ text: initial.replace("hello", "hello!") }],
        });

        await delay(30);
        expect(server.takePendingNotifications("textDocument/publishDiagnostics")).toHaveLength(0);

        const final = await waitForDiagnosticsContaining(server, "missingName");
        expect(JSON.stringify(final.params)).toContain("missingName");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("runs VBScript project diagnostics through the worker pool", async () => {
      const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-vb-worker-"));
      const page = path.join(tempDir, "default.asp");
      const workerSource = `<%
Sub Worker()
  Dim unusedValue
End Sub
Response.Write "ok"
%>`;
      fs.writeFileSync(page, workerSource, "utf8");
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: `file://${tempDir}`,
          capabilities: {},
        });
        server.notify("workspace/didChangeConfiguration", {
          settings: {
            aspLsp: {
              debug: { output: "verbose" },
              diagnostics: { debounceMs: 0 },
              workspace: { busyAnalysisConcurrency: 1 },
            },
          },
        });

        const diagnostics = await server.request("workspace/diagnostic", {
          previousResultIds: [],
        });
        expect(JSON.stringify(diagnostics)).toContain("unusedValue");
        expect(JSON.stringify(diagnostics)).toContain("asp-lsp-vbscript-unused");
        await waitForLogContaining(server, "vbscript.worker.dispatch");
        await waitForLogContaining(server, "check.workspace.vbscript.diagnostics.worker");
        await waitForLogContaining(server, "vbscript.worker.complete");
        const payloadLog = await waitForLogContaining(server, "worker.payload.bytes");
        expect(payloadBytesFromLog(payloadLog)).toBeLessThan(109_500);

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
        fs.rmSync(tempDir, { recursive: true, force: true });
      }
    });

    it("ignores unrelated watched file changes without refreshing open ASP documents", async () => {
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
            aspLsp: { debug: { output: "verbose" } },
          },
        });
        const uri = "file:///tmp/watched-noop.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: `<% Response.Write "ok" %>`,
          },
        });
        await server.waitForNotification("textDocument/publishDiagnostics");
        await delay(350);
        server.takePendingNotifications("window/logMessage");

        server.notify("workspace/didChangeWatchedFiles", {
          changes: [{ uri: "file:///tmp/readme.txt", type: 2 }],
        });
        await delay(350);

        const logs = server.takePendingNotifications("window/logMessage");
        expect(JSON.stringify(logs)).not.toContain("analysis.parse");
        expect(JSON.stringify(logs)).not.toContain("LSP check started");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("keeps parse and diagnostics idle for formatting-only settings changes", async () => {
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
              debug: { output: "verbose" },
              diagnostics: { debounceMs: 0 },
            },
          },
        });
        const uri = "file:///tmp/format-only-settings.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: `<% Response.Write "ok" %>`,
          },
        });
        await server.waitForNotification("textDocument/publishDiagnostics");
        await waitForLogContaining(server, "LSP check completed");
        server.takePendingNotifications("window/logMessage");

        server.notify("workspace/didChangeConfiguration", {
          settings: {
            aspLsp: {
              debug: { output: "verbose" },
              diagnostics: { debounceMs: 0 },
              format: { uppercaseKeywords: true },
            },
          },
        });
        await delay(350);

        const logs = JSON.stringify(server.takePendingNotifications("window/logMessage"));
        expect(logs).not.toContain("analysis.parse");
        expect(logs).not.toContain("LSP check started");
        expect(logs).not.toContain("invalidation.");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("invalidates include resolution settings without reparsing or clearing JavaScript projects", async () => {
      const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-include-settings-"));
      const pageDir = path.join(tempDir, "pages");
      const includeDir = path.join(tempDir, "includes");
      fs.mkdirSync(pageDir, { recursive: true });
      fs.mkdirSync(includeDir, { recursive: true });
      const page = path.join(pageDir, "default.asp");
      const include = path.join(includeDir, "shared.inc");
      fs.writeFileSync(
        page,
        `<!-- #include file="shared.inc" -->\n<% Response.Write "ok" %>`,
        "utf8",
      );
      fs.writeFileSync(include, `<% Const SharedValue = "ok" %>`, "utf8");
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: `file://${tempDir}`,
          capabilities: {},
        });
        server.notify("workspace/didChangeConfiguration", {
          settings: {
            aspLsp: {
              debug: { output: "verbose" },
              diagnostics: { debounceMs: 0 },
            },
          },
        });
        const uri = `file://${page}`;
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: fs.readFileSync(page, "utf8"),
          },
        });
        await waitForDiagnosticsContaining(server, "shared.inc");
        await waitForLogContaining(server, "LSP check completed");
        server.takePendingNotifications("window/logMessage");
        server.takePendingNotifications("textDocument/publishDiagnostics");

        server.notify("workspace/didChangeConfiguration", {
          settings: {
            aspLsp: {
              debug: { output: "verbose" },
              diagnostics: { debounceMs: 0 },
              includePaths: [includeDir],
            },
          },
        });
        const includeLog = await waitForLogContaining(server, "invalidation.includeResolution");
        const checkLog = await waitForLogContaining(server, "LSP check completed");
        const pulled = await server.request("textDocument/diagnostic", {
          textDocument: { uri },
        });

        expect(JSON.stringify(pulled)).not.toContain("shared.inc");
        const logs = JSON.stringify([
          includeLog,
          checkLog,
          ...server.takePendingNotifications("window/logMessage"),
        ]);
        expect(logs).not.toContain("analysis.parse.full");
        expect(logs).not.toContain("invalidation.jsProject");
        expect(logs).not.toContain("invalidation.workspaceIndex");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
        fs.rmSync(tempDir, { recursive: true, force: true });
      }
    });

    it("keeps JavaScript project changes fresh without dropping ASP parse caches", async () => {
      const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-js-invalidation-"));
      const helper = path.join(tempDir, "helper.js");
      const page = path.join(tempDir, "default.asp");
      fs.writeFileSync(helper, `const oldProjectGlobal = 1;\n`, "utf8");
      fs.writeFileSync(
        path.join(tempDir, "jsconfig.json"),
        JSON.stringify({ include: ["*.js", "*.asp"] }),
        "utf8",
      );
      const source = `<script>
newProjectGlobal;
</script>`;
      fs.writeFileSync(page, source, "utf8");
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: `file://${tempDir}`,
          capabilities: {},
        });
        server.notify("workspace/didChangeConfiguration", {
          settings: {
            aspLsp: {
              checkJs: true,
              debug: { output: "verbose" },
              diagnostics: { debounceMs: 0 },
            },
          },
        });
        const uri = `file://${page}`;
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        await waitForDiagnosticsContaining(server, "newProjectGlobal");
        await waitForLogContaining(server, "javascriptSemantic.worker");
        await waitForLogContaining(server, "LSP check completed");
        server.takePendingNotifications("window/logMessage");

        fs.writeFileSync(helper, `const newProjectGlobal = 1;\n`, "utf8");
        server.notify("workspace/didChangeWatchedFiles", {
          changes: [{ uri: `file://${helper}`, type: 2 }],
        });
        const jsLog = await waitForLogContaining(server, "invalidation.jsProject");
        const checkLog = await waitForLogContaining(server, "LSP check completed");
        const pulled = await server.request("textDocument/diagnostic", {
          textDocument: { uri },
        });

        expect(JSON.stringify(pulled)).not.toContain("Cannot find name 'newProjectGlobal'");
        const logs = JSON.stringify([
          jsLog,
          checkLog,
          ...server.takePendingNotifications("window/logMessage"),
        ]);
        expect(logs).not.toContain("analysis.parse.full");
        expect(logs).not.toContain("invalidation.workspaceIndex");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
        fs.rmSync(tempDir, { recursive: true, force: true });
      }
    });

    it("reuses the TypeScript language service across JavaScript document edits", async () => {
      const source = `<script>
const alphaValue = 1;
alph
</script>`;
      const updated = `<script>
const betaValue = 1;
beta
</script>`;
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
              debug: { output: "verbose" },
              diagnostics: { debounceMs: 0 },
            },
          },
        });
        const uri = "file:///tmp/js-ls-reuse.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        await waitForLogContaining(server, "LSP check completed");
        server.takePendingNotifications("window/logMessage");

        const alpha = await server.request("textDocument/completion", {
          textDocument: { uri },
          position: positionAt(source, source.lastIndexOf("alph") + "alph".length),
        });
        expect(JSON.stringify(alpha)).toContain("alphaValue");
        await waitForLogContaining(server, "javascript.languageService.create");
        server.takePendingNotifications("window/logMessage");

        const alphaReused = await server.request("textDocument/completion", {
          textDocument: { uri },
          position: positionAt(source, source.lastIndexOf("alph") + "alph".length),
        });
        expect(JSON.stringify(alphaReused)).toContain("alphaValue");
        await waitForLogContaining(server, "javascript.languageService.reuse");
        server.takePendingNotifications("window/logMessage");

        server.notify("textDocument/didChange", {
          textDocument: { uri, version: 2 },
          contentChanges: [{ text: updated }],
        });
        await waitForLogContaining(server, "LSP check completed");
        server.takePendingNotifications("window/logMessage");

        const beta = await server.request("textDocument/completion", {
          textDocument: { uri },
          position: positionAt(updated, updated.lastIndexOf("beta") + "beta".length),
        });
        expect(JSON.stringify(beta)).toContain("betaValue");
        expect(JSON.stringify(beta)).not.toContain("alphaValue");
        await waitForLogContaining(server, "javascript.languageService.reuse");
        server.takePendingNotifications("window/logMessage");

        const htmlEdited = `<div>shifted</div>
${updated}`;
        server.notify("textDocument/didChange", {
          textDocument: { uri, version: 3 },
          contentChanges: [{ text: htmlEdited }],
        });
        await waitForLogContaining(server, "LSP check completed");
        server.takePendingNotifications("window/logMessage");

        const shifted = await server.request("textDocument/completion", {
          textDocument: { uri },
          position: positionAt(htmlEdited, htmlEdited.lastIndexOf("beta") + "beta".length),
        });
        expect(JSON.stringify(shifted)).toContain("betaValue");
        await waitForLogContaining(server, "javascript.languageService.reuse");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("answers JavaScript completions after edits without flushing project prewarm", async () => {
      const source = `<script>
const alphaValue = 1;
alph
</script>`;
      const updated = `<script>
const betaValue = 1;
bet
</script>`;
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
              debug: { output: "verbose" },
              diagnostics: { debounceMs: 1000 },
            },
          },
        });
        const uri = "file:///tmp/js-interactive-no-prewarm-flush.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        await waitForLogContaining(server, "LSP check completed");

        const alpha = await server.request("textDocument/completion", {
          textDocument: { uri },
          position: positionAt(source, source.lastIndexOf("alph") + "alph".length),
        });
        expect(JSON.stringify(alpha)).toContain("alphaValue");
        await waitForLogContaining(server, "javascript.languageService.create");
        server.takePendingNotifications("window/logMessage");

        server.notify("textDocument/didChange", {
          textDocument: { uri, version: 2 },
          contentChanges: [{ text: updated }],
        });
        const beta = await server.request("textDocument/completion", {
          textDocument: { uri },
          position: positionAt(updated, updated.lastIndexOf("bet") + "bet".length),
        });
        const serialized = JSON.stringify(beta);
        expect(serialized).toContain("betaValue");
        expect(serialized).not.toContain("alphaValue");
        const logs = JSON.stringify(server.takePendingNotifications("window/logMessage"));
        expect(logs).toContain("projectUpdate.scheduled");
        expect(logs).toContain("javascript.languageService.reuse");
        expect(logs).not.toContain("projectUpdate.flushed: reason=document.change");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("reports virtual JavaScript snapshot change range reuse", async () => {
      let source = `<script>
const alphaValue = 1;
alph
</script>`;
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
              debug: { output: "verbose" },
              diagnostics: { debounceMs: 0 },
            },
          },
        });
        const uri = "file:///tmp/js-snapshot-change-range.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        await waitForLogContaining(server, "LSP check completed");
        await server.request("textDocument/completion", {
          textDocument: { uri },
          position: positionAt(source, source.lastIndexOf("alph") + "alph".length),
        });
        await waitForLogContaining(server, "js.snapshot.changeRange.miss");
        server.takePendingNotifications("window/logMessage");

        source = notifyRangedReplacement(server, uri, source, 2, "alphaValue", "betaValue");
        await waitForLogContaining(server, "LSP check completed");
        await server.request("textDocument/completion", {
          textDocument: { uri },
          position: positionAt(source, source.lastIndexOf("alph") + "alph".length),
        });
        await waitForLogContaining(server, "js.snapshot.changeRange.hit");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("reuses completion results for prefix continuation", async () => {
      let source = `<%
Response.W
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
              debug: { output: "verbose" },
              diagnostics: { debounceMs: 0 },
            },
          },
        });
        const uri = "file:///tmp/completion-cache.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        await waitForLogContaining(server, "LSP check completed");
        const first = await server.request("textDocument/completion", {
          textDocument: { uri },
          position: positionAt(source, source.indexOf("Response.W") + "Response.W".length),
        });
        expect(JSON.stringify(first)).toContain("Write");
        await waitForLogContaining(server, "completion.cache.miss");
        server.takePendingNotifications("window/logMessage");

        const insertOffset = source.indexOf("Response.W") + "Response.W".length;
        server.notify("textDocument/didChange", {
          textDocument: { uri, version: 2 },
          contentChanges: [
            {
              range: {
                start: positionAt(source, insertOffset),
                end: positionAt(source, insertOffset),
              },
              text: "r",
            },
          ],
        });
        source = `${source.slice(0, insertOffset)}r${source.slice(insertOffset)}`;
        await waitForLogContaining(server, "LSP check completed");
        const reused = await server.request("textDocument/completion", {
          textDocument: { uri },
          position: positionAt(source, source.indexOf("Response.Wr") + "Response.Wr".length),
        });
        expect(JSON.stringify(reused)).toContain("Write");
        await waitForLogContaining(server, "completion.cache.hit");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("does not advance workspace generation for document edits", async () => {
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
              debug: { output: "verbose" },
              diagnostics: { debounceMs: 0 },
            },
          },
        });
        const uri = "file:///tmp/document-edit-generation.asp";
        let source = `<%
Option Explicit
Dim known
Response.Write known
%>`;
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });
        await server.waitForNotification("textDocument/publishDiagnostics");
        await waitForLogContaining(server, "LSP check completed");
        server.takePendingNotifications("window/logMessage");

        source = notifyRangedReplacement(server, uri, source, 2, "known", "renamed");
        await server.waitForNotification("textDocument/publishDiagnostics");
        await waitForLogContaining(server, "LSP check completed");

        const logs = JSON.stringify(server.takePendingNotifications("window/logMessage"));
        expect(source).toContain("Dim renamed");
        expect(logs).not.toContain("invalidation.workspaceIndex");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("refreshes open files after changed include exports", async () => {
      const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-watched-usage-index-"));
      const include = path.join(tempDir, "shared.inc");
      const affected = path.join(tempDir, "affected.asp");
      const unaffected = path.join(tempDir, "unaffected.asp");
      const unrelated = path.join(tempDir, "unrelated.asp");
      fs.writeFileSync(
        include,
        `<%
Function SharedValue()
  SharedValue = "old"
End Function

Function OtherValue()
  OtherValue = "same"
End Function
%>`,
        "utf8",
      );
      fs.writeFileSync(
        affected,
        `<!-- #include file="shared.inc" -->\n<% Response.Write SharedValue() %>`,
        "utf8",
      );
      fs.writeFileSync(
        unaffected,
        `<!-- #include file="shared.inc" -->\n<% Response.Write OtherValue() %>`,
        "utf8",
      );
      fs.writeFileSync(unrelated, `<% Response.Write "standalone" %>`, "utf8");
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: `file://${tempDir}`,
          capabilities: {},
        });
        server.notify("workspace/didChangeConfiguration", {
          settings: {
            aspLsp: {
              debug: { output: "verbose" },
              diagnostics: { debounceMs: 0 },
            },
          },
        });
        const affectedUri = `file://${affected}`;
        const unaffectedUri = `file://${unaffected}`;
        const unrelatedUri = `file://${unrelated}`;
        for (const fileName of [affected, unaffected, unrelated]) {
          server.notify("textDocument/didOpen", {
            textDocument: {
              uri: `file://${fileName}`,
              languageId: "classic-asp",
              version: 1,
              text: fs.readFileSync(fileName, "utf8"),
            },
          });
        }
        await waitForLogContaining(server, "LSP check completed");
        await waitForLogContaining(server, "LSP check completed");
        await waitForLogContaining(server, "LSP check completed");
        server.takePendingNotifications("window/logMessage");

        fs.writeFileSync(
          include,
          `<%
Function SharedValueRenamed()
  SharedValueRenamed = "new"
End Function

Function OtherValue()
  OtherValue = "same"
End Function
%>`,
          "utf8",
        );
        server.notify("workspace/didChangeWatchedFiles", {
          changes: [{ uri: `file://${include}`, type: 2 }],
        });
        const firstAnalysisLog = await waitForLogContaining(server, "LSP check completed");
        await delay(500);

        const logs = [firstAnalysisLog, ...server.takePendingNotifications("window/logMessage")];
        const serialized = JSON.stringify(logs);
        expect(serialized).toContain(affectedUri);
        expect(serialized).toContain(unaffectedUri);
        expect(serialized).not.toContain(unrelatedUri);

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
        fs.rmSync(tempDir, { recursive: true, force: true });
      }
    });

    it("keeps dependent diagnostics idle after private include implementation changes", async () => {
      const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-include-private-"));
      const include = path.join(tempDir, "shared.inc");
      const page = path.join(tempDir, "default.asp");
      fs.writeFileSync(
        include,
        `<%
Function SharedValue()
  Dim privateValue
  privateValue = "old"
  SharedValue = privateValue
End Function
%>`,
        "utf8",
      );
      fs.writeFileSync(
        page,
        `<!-- #include file="shared.inc" -->\n<% Response.Write SharedValue() %>`,
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
        server.notify("workspace/didChangeConfiguration", {
          settings: {
            aspLsp: {
              debug: { output: "verbose" },
              diagnostics: { debounceMs: 0 },
            },
          },
        });
        const uri = `file://${page}`;
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: fs.readFileSync(page, "utf8"),
          },
        });
        await waitForLogContaining(server, "LSP check completed");
        server.takePendingNotifications("window/logMessage");
        server.takePendingNotifications("textDocument/publishDiagnostics");

        fs.writeFileSync(
          include,
          `<%
Function SharedValue()
  Dim privateValue
  privateValue = "new"
  SharedValue = privateValue
End Function
%>`,
          "utf8",
        );
        server.notify("workspace/didChangeWatchedFiles", {
          changes: [{ uri: `file://${include}`, type: 2 }],
        });
        await delay(350);

        const logs = JSON.stringify(server.takePendingNotifications("window/logMessage"));
        const diagnostics = JSON.stringify(
          server.takePendingNotifications("textDocument/publishDiagnostics"),
        );
        expect(logs).toContain("include.publicBoundary.reuse");
        expect(logs).not.toContain(`LSP check started: ${uri}`);
        expect(diagnostics).not.toContain(uri);

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
        fs.rmSync(tempDir, { recursive: true, force: true });
      }
    });

    it("logs a single diagnostics check in the classic ASP dashboard smoke scenario", async () => {
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: `file://${path.resolve(process.cwd(), "../..")}`,
          capabilities: {},
        });
        server.notify("workspace/didChangeConfiguration", {
          settings: { aspLsp: { debug: { output: "verbose" } } },
        });

        const samplePath = path.resolve(
          process.cwd(),
          "../../samples/classic-asp-dashboard/customers.asp",
        );
        const uri = `file://${samplePath}`;
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: fs.readFileSync(samplePath, "utf8"),
          },
        });

        const checkLog = await waitForLogContaining(server, "LSP check completed");
        expectElapsedLogWithoutHeat(checkLog);
        await waitForLogContaining(server, "check.javascriptUnused");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
      }
    });

    it("does not emit LSP timing logs when debug output is off", async () => {
      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: "file:///tmp",
          capabilities: {},
        });
        server.notify("workspace/didChangeConfiguration", {
          settings: { aspLsp: { debug: { output: "off" } } },
        });
        const uri = "file:///tmp/debug-off.asp";
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri,
            languageId: "classic-asp",
            version: 1,
            text: `<style>.x{color:red}</style>
<script>const value = 1;</script>
<% Response.Write value %>`,
          },
        });
        await server.waitForNotification("textDocument/publishDiagnostics");
        await server.request("textDocument/formatting", {
          textDocument: { uri },
          options: { tabSize: 2, insertSpaces: true },
        });

        const debugLogs = server
          .takePendingNotifications("window/logMessage")
          .filter((message) =>
            JSON.stringify(message.params).match(
              /LSP analysis|LSP check|Formatting conversion|analysis\.|check\.|format\.|heat=duration-/,
            ),
          );
        expect(debugLogs).toHaveLength(0);

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
        const diagnostics = await waitForDiagnosticsContaining(server, "no member");
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

    it("keeps include-backed VBScript types out of progressive diagnostics", async () => {
      const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-progressive-types-"));
      const owner = path.join(tempDir, "default.asp");
      const include = path.join(tempDir, "customer.inc");
      fs.writeFileSync(
        include,
        `<%
Class IncludedCustomer
  Public Name
End Class
%>`,
        "utf8",
      );
      const source = `<!-- #include file="customer.inc" -->
<%
' @type customer As IncludedCustomer
Dim customer
customer.Name
%>`;
      fs.writeFileSync(owner, source, "utf8");

      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: `file://${tempDir}`,
          capabilities: {},
        });
        server.notify("workspace/didChangeConfiguration", {
          settings: { aspLsp: { vbscript: { typeChecking: "strict" } } },
        });
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri: `file://${owner}`,
            languageId: "classic-asp",
            version: 1,
            text: source,
          },
        });

        await server.request("textDocument/hover", {
          textDocument: { uri: `file://${owner}` },
          position: positionAt(source, source.indexOf("customer.Name")),
        });
        const diagnostics = await server.request("textDocument/diagnostic", {
          textDocument: { uri: `file://${owner}` },
        });
        const serialized = JSON.stringify(diagnostics);
        expect(serialized).not.toContain("no member");
        expect(serialized).not.toContain("missingMember");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
        fs.rmSync(tempDir, { recursive: true, force: true });
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
        const diagnostics = await waitForDiagnosticsContaining(server, "objectNeedsSet");
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

        const definition = await waitForDefinitionContaining(
          server,
          { uri: `file://${owner}`, position: marked.position },
          "common.inc",
        );
        expect(JSON.stringify(definition)).toContain("common.inc");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
        fs.rmSync(tempDir, { recursive: true, force: true });
      }
    });

    it("auto-detects Shift_JIS include directives in unopened include files", async () => {
      const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-auto-include-"));
      const owner = path.join(tempDir, "default.asp");
      const first = path.join(tempDir, "first.inc");
      const next = path.join(tempDir, "次.inc");
      const marked = markedDocument(`<!-- #include file="first.inc" -->
<%
Response.Write Shared▮Thing()
%>`);
      fs.writeFileSync(owner, marked.text, "utf8");
      fs.writeFileSync(
        first,
        Buffer.concat([
          Buffer.from('<!-- #include file="', "ascii"),
          Buffer.from([0x8e, 0x9f]),
          Buffer.from('.inc" -->', "ascii"),
        ]),
      );
      fs.writeFileSync(next, "<%\nFunction SharedThing()\nEnd Function\n%>", "utf8");

      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: `file://${tempDir}`,
          capabilities: {},
        });
        server.notify("workspace/didChangeConfiguration", {
          settings: { aspLsp: { legacyEncoding: "auto" } },
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

        const completions = await waitForCompletionContaining(
          server,
          { uri: `file://${owner}`, position: marked.position },
          "SharedThing",
        );
        expect(completionLabels(completions)).toContain("SharedThing");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
        fs.rmSync(tempDir, { recursive: true, force: true });
      }
    });

    it("keeps explicit utf8 include decoding deterministic", async () => {
      const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-utf8-include-"));
      const owner = path.join(tempDir, "default.asp");
      const first = path.join(tempDir, "first.inc");
      const next = path.join(tempDir, "次.inc");
      const marked = markedDocument(`<!-- #include file="first.inc" -->
<%
Response.Write Shared▮Thing()
%>`);
      fs.writeFileSync(owner, marked.text, "utf8");
      fs.writeFileSync(
        first,
        Buffer.concat([
          Buffer.from('<!-- #include file="', "ascii"),
          Buffer.from([0x8e, 0x9f]),
          Buffer.from('.inc" -->', "ascii"),
        ]),
      );
      fs.writeFileSync(next, "<%\nFunction SharedThing()\nEnd Function\n%>", "utf8");

      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: `file://${tempDir}`,
          capabilities: {},
        });
        server.notify("workspace/didChangeConfiguration", {
          settings: { aspLsp: { legacyEncoding: "utf8" } },
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
        expect(completionLabels(completions)).not.toContain("SharedThing");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
        fs.rmSync(tempDir, { recursive: true, force: true });
      }
    });

    it("auto-detects Shift_JIS unopened workspace files for workspace symbols", async () => {
      const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-auto-index-"));
      fs.writeFileSync(
        path.join(tempDir, "unopened.asp"),
        Buffer.concat([
          Buffer.from('<div id="', "ascii"),
          Buffer.from([0x93, 0xfa, 0x96, 0x7b, 0x8c, 0xea]),
          Buffer.from('"></div>', "ascii"),
        ]),
      );

      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: `file://${tempDir}`,
          capabilities: {},
        });
        server.notify("workspace/didChangeConfiguration", {
          settings: { aspLsp: { legacyEncoding: "auto" } },
        });

        const symbols = await server.request("workspace/symbol", { query: "日本語" });
        expect(JSON.stringify(symbols)).toContain("日本語");
        expect(JSON.stringify(symbols)).toContain("unopened.asp");

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

        const diagnostics = await waitForDiagnosticsContaining(server, "Include cycle detected");
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

        const diagnostics = await waitForDiagnosticsContaining(server, "Include cycle detected");
        expect(JSON.stringify(diagnostics.params)).toContain("Include cycle detected");

        await server.request("shutdown", null);
        server.notify("exit", undefined);
      } finally {
        server.stop();
        fs.rmSync(tempDir, { recursive: true, force: true });
      }
    });

    it("refreshes dependent diagnostics after include directive changes", async () => {
      const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-include-graph-"));
      const owner = path.join(tempDir, "default.asp");
      const include = path.join(tempDir, "shared.inc");
      fs.writeFileSync(owner, '<!-- #include file="shared.inc" -->', "utf8");
      fs.writeFileSync(include, '<% Const SharedValue = "ok" %>', "utf8");

      const server = new RpcServer();
      try {
        await server.start();
        await server.request("initialize", {
          processId: process.pid,
          rootUri: `file://${tempDir}`,
          capabilities: {},
        });
        server.notify("workspace/didChangeConfiguration", {
          settings: {
            aspLsp: {
              debug: { output: "verbose" },
              diagnostics: { debounceMs: 0 },
            },
          },
        });
        server.notify("textDocument/didOpen", {
          textDocument: {
            uri: `file://${owner}`,
            languageId: "classic-asp",
            version: 1,
            text: fs.readFileSync(owner, "utf8"),
          },
        });
        await server.waitForNotification("textDocument/publishDiagnostics");
        server.takePendingNotifications("window/logMessage");

        fs.writeFileSync(include, '<!-- #include file="default.asp" -->', "utf8");
        server.notify("workspace/didChangeWatchedFiles", {
          changes: [{ uri: `file://${include}`, type: 2 }],
        });

        await waitForDiagnosticsContaining(server, "Include cycle detected");
        await waitForLogContaining(server, "invalidation.includeRefs");

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
  private responses = new Map<number, PendingResponse>();
  private notifications = new Map<string, Array<(message: JsonRpcMessage) => void>>();
  private pendingNotifications = new Map<string, JsonRpcMessage[]>();

  constructor(private readonly options: { env?: Record<string, string> } = {}) {}

  async start(): Promise<void> {
    const serverPath = path.join(process.cwd(), "dist", "server.js");
    this.child = spawn(process.execPath, [serverPath, "--stdio"], {
      cwd: process.cwd(),
      env: { ...process.env, ...this.options.env },
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
      const timer = setTimeout(
        () => reject(new Error(`Timed out waiting for ${method}: ${this.stderr}`)),
        rpcTimeoutMs,
      );
      this.responses.set(id, { method, resolve, reject, timer });
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
      let timer: ReturnType<typeof setTimeout>;
      const callback = (notification: JsonRpcMessage) => {
        clearTimeout(timer);
        resolve(notification);
      };
      callbacks.push(callback);
      this.notifications.set(method, callbacks);
      timer = setTimeout(() => {
        const activeCallbacks = this.notifications.get(method) ?? [];
        this.notifications.set(
          method,
          activeCallbacks.filter((active) => active !== callback),
        );
        reject(new Error(`Timed out waiting for ${method}: ${this.stderr}`));
      }, rpcTimeoutMs);
    });
  }

  takePendingNotifications(method: string): JsonRpcMessage[] {
    const pending = this.pendingNotifications.get(method) ?? [];
    this.pendingNotifications.set(method, []);
    return pending;
  }

  prependPendingNotifications(method: string, messages: JsonRpcMessage[]): void {
    if (messages.length === 0) {
      return;
    }
    const pending = this.pendingNotifications.get(method) ?? [];
    this.pendingNotifications.set(method, [...messages, ...pending]);
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
        const pending = this.responses.get(message.id);
        if (pending) {
          clearTimeout(pending.timer);
          if (message.error) {
            pending.reject(
              new Error(
                `Request ${pending.method} failed: ${JSON.stringify(message.error)} ${this.stderr}`,
              ),
            );
          } else {
            pending.resolve(message.result);
          }
        }
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

function tokenMatches(
  text: string,
  token: DecodedSemanticToken,
  needle: string,
  tokenType: number,
  offset = 0,
): boolean {
  const position = positionAt(text, text.indexOf(needle) + offset);
  return (
    token.line === position.line &&
    token.character === position.character &&
    token.tokenType === tokenType
  );
}

async function waitForSemanticTokenAsync(
  server: RpcServer,
  uri: string,
  source: string,
  needle: string,
  tokenType: number,
): Promise<DecodedSemanticToken[]> {
  const deadline = Date.now() + rpcTimeoutMs;
  while (Date.now() < deadline) {
    const semanticTokens = await server.request("textDocument/semanticTokens/full", {
      textDocument: { uri },
    });
    const decoded = decodeSemanticTokens((semanticTokens as { data?: number[] }).data);
    if (decoded.some((token) => tokenMatches(source, token, needle, tokenType))) {
      return decoded;
    }
    await delay(1000);
  }
  throw new Error(`Timed out waiting for semantic token ${needle}.`);
}

function completionLabels(completions: unknown): string[] {
  return completionItems(completions)
    .map((item) => (item as { label?: unknown }).label)
    .filter((label): label is string => typeof label === "string");
}

function completionItems(completions: unknown): Array<Record<string, unknown>> {
  const items = Array.isArray(completions)
    ? completions
    : ((completions as { items?: unknown[] }).items ?? []);
  return items.filter(
    (item): item is Record<string, unknown> => item !== null && typeof item === "object",
  );
}

function completionEditRange(
  item: Record<string, unknown> | undefined,
):
  | { start: { line: number; character: number }; end: { line: number; character: number } }
  | undefined {
  const textEdit = item?.textEdit as
    | {
        range?: {
          start: { line: number; character: number };
          end: { line: number; character: number };
        };
        insert?: {
          start: { line: number; character: number };
          end: { line: number; character: number };
        };
        replace?: {
          start: { line: number; character: number };
          end: { line: number; character: number };
        };
      }
    | undefined;
  return textEdit?.range ?? textEdit?.insert ?? textEdit?.replace;
}

function expectSelectionRangesToBeNested(selection: unknown): void {
  expect(Array.isArray(selection)).toBe(true);
  for (const item of selection as LspSelectionRange[]) {
    let child = item;
    let parent = item.parent;
    while (parent) {
      expect(rangeContainsRange(parent.range, child.range)).toBe(true);
      child = parent;
      parent = parent.parent;
    }
  }
}

interface LspSelectionRange {
  range: LspRange;
  parent?: LspSelectionRange;
}

interface LspRange {
  start: { line: number; character: number };
  end: { line: number; character: number };
}

function rangeContainsRange(outer: LspRange, inner: LspRange): boolean {
  return (
    comparePositions(outer.start, inner.start) <= 0 && comparePositions(outer.end, inner.end) >= 0
  );
}

function comparePositions(
  left: { line: number; character: number },
  right: { line: number; character: number },
): number {
  return left.line === right.line ? left.character - right.character : left.line - right.line;
}

async function waitForDiagnosticsContaining(
  server: RpcServer,
  expected: string,
): Promise<JsonRpcMessage> {
  for (let attempt = 0; attempt < 10; attempt += 1) {
    const diagnostics = await server.waitForNotification("textDocument/publishDiagnostics");
    if (JSON.stringify(diagnostics.params).includes(expected)) {
      return diagnostics;
    }
  }
  throw new Error(`Timed out waiting for diagnostics containing ${expected}.`);
}

async function waitForDiagnosticsPublished(
  server: RpcServer,
  uri: string,
): Promise<JsonRpcMessage> {
  for (let attempt = 0; attempt < 10; attempt += 1) {
    const diagnostics = await server.waitForNotification("textDocument/publishDiagnostics");
    if ((diagnostics.params as { uri?: string })?.uri === uri) {
      return diagnostics;
    }
  }
  throw new Error(`Timed out waiting for diagnostics published for ${uri}.`);
}

function diagnosticMessages(message: JsonRpcMessage): string[] {
  return (
    (message.params as { diagnostics?: Array<{ message?: string }> })?.diagnostics ?? []
  ).flatMap((diagnostic) => (diagnostic.message ? [diagnostic.message] : []));
}

function diagnosticText(message: JsonRpcMessage): string {
  return JSON.stringify(message.params);
}

function diagnosticContaining(message: JsonRpcMessage, expected: string) {
  const diagnostics =
    (
      message.params as {
        diagnostics?: Array<{
          message?: string;
          range: { start: { line: number; character: number } };
        }>;
      }
    )?.diagnostics ?? [];
  return diagnostics.find((diagnostic) => diagnostic.message?.includes(expected));
}

function diagnosticFromSource(message: JsonRpcMessage, source: string) {
  const diagnostics =
    (
      message.params as {
        diagnostics?: Array<{
          source?: string;
          range: { start: { line: number; character: number } };
        }>;
      }
    )?.diagnostics ?? [];
  return diagnostics.find((diagnostic) => diagnostic.source === source);
}

async function waitForCompletionContaining(
  server: RpcServer,
  params: { uri: string; position: { line: number; character: number } },
  expected: string,
): Promise<unknown> {
  return waitForCompletionSatisfying(
    server,
    params,
    (completions) => JSON.stringify(completions).includes(expected),
    `completion containing ${expected}`,
  );
}

async function waitForCompletionSatisfying(
  server: RpcServer,
  params: { uri: string; position: { line: number; character: number } },
  predicate: (completions: unknown) => boolean,
  description: string,
): Promise<unknown> {
  for (let attempt = 0; attempt < 20; attempt += 1) {
    const completions = await server.request("textDocument/completion", {
      textDocument: { uri: params.uri },
      position: params.position,
    });
    if (predicate(completions)) {
      return completions;
    }
    await delay(50);
  }
  throw new Error(`Timed out waiting for ${description}.`);
}

async function waitForDefinitionContaining(
  server: RpcServer,
  params: { uri: string; position: { line: number; character: number } },
  expected: string,
): Promise<unknown> {
  for (let attempt = 0; attempt < 20; attempt += 1) {
    const definition = await server.request("textDocument/definition", {
      textDocument: { uri: params.uri },
      position: params.position,
    });
    if (JSON.stringify(definition).includes(expected)) {
      return definition;
    }
    await delay(50);
  }
  throw new Error(`Timed out waiting for definition containing ${expected}.`);
}

async function waitForLogContaining(server: RpcServer, expected: string): Promise<JsonRpcMessage> {
  const deadline = Date.now() + rpcTimeoutMs;
  const skipped: JsonRpcMessage[] = [];
  while (Date.now() < deadline) {
    const pending = server.takePendingNotifications("window/logMessage");
    for (const [index, message] of pending.entries()) {
      if (JSON.stringify(message.params).includes(expected)) {
        server.prependPendingNotifications("window/logMessage", [
          ...skipped,
          ...pending.slice(index + 1),
        ]);
        return message;
      }
      skipped.push(message);
    }
    const message = await server.waitForNotification("window/logMessage");
    if (JSON.stringify(message.params).includes(expected)) {
      server.prependPendingNotifications("window/logMessage", skipped);
      return message;
    }
    skipped.push(message);
  }
  server.prependPendingNotifications("window/logMessage", skipped);
  throw new Error(`Timed out waiting for log containing ${expected}.`);
}

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function countOccurrences(text: string, needle: string): number {
  return text.split(needle).length - 1;
}

function expectElapsedLogWithoutHeat(message: JsonRpcMessage): void {
  const text = JSON.stringify(message.params);
  expect(text).toMatch(/in \d+\.\d ms/);
  expect(text).not.toContain("heat=duration-");
}

function payloadBytesFromLog(message: JsonRpcMessage): number {
  const text = JSON.stringify(message.params);
  const payload = /payload=(\d+)/.exec(text)?.[1];
  if (!payload) {
    throw new Error(`Missing payload bytes in log: ${text}`);
  }
  return Number(payload);
}

function jsWorkerPayloadBytesFromLog(message: JsonRpcMessage): number {
  const text = JSON.stringify(message.params);
  const payload = /payloadBytes=(\d+)/.exec(text)?.[1];
  if (!payload) {
    throw new Error(`Missing JavaScript worker payload bytes in log: ${text}`);
  }
  return Number(payload);
}

function notifyRangedReplacement(
  server: RpcServer,
  uri: string,
  current: string,
  version: number,
  needle: string,
  replacement: string,
): string {
  const start = current.indexOf(needle);
  if (start === -1) {
    throw new Error(`Missing text: ${needle}`);
  }
  const end = start + needle.length;
  server.notify("textDocument/didChange", {
    textDocument: { uri, version },
    contentChanges: [
      {
        range: { start: positionAt(current, start), end: positionAt(current, end) },
        text: replacement,
      },
    ],
  });
  return current.slice(0, start) + replacement + current.slice(end);
}

function applyTextEdit(text: string, edit: TextEdit): string {
  const start = offsetAt(text, edit.range.start);
  const end = offsetAt(text, edit.range.end);
  return text.slice(0, start) + edit.newText + text.slice(end);
}

function offsetAt(text: string, position: { line: number; character: number }): number {
  let line = 0;
  let character = 0;
  for (let index = 0; index < text.length; index += 1) {
    if (line === position.line && character === position.character) {
      return index;
    }
    if (text[index] === "\n") {
      line += 1;
      character = 0;
    } else {
      character += 1;
    }
  }
  return text.length;
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
