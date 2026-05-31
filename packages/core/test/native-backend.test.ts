import fs from "node:fs";
import path from "node:path";
import { afterAll, describe, expect, it } from "vitest";
import {
  analyzeVbscript,
  analyzeVbscriptAsync,
  analyzeVbscriptFromTextAsync,
  aspAnalysisBackendInfo,
  collectVbscriptSymbols,
  collectVbscriptSymbolsAsync,
  collectVbscriptSymbolsFromTextAsync,
  hydrateVbscriptCst,
  parseAspCst,
  parseAspDocument,
  parseAspDocumentAsync,
  parseAspDocumentSkeletonAsync,
  summarizeAspFileAnalysis,
  summarizeAspFileAnalysisFromTextAsync,
} from "../src/index";
import type { AspCstNode } from "../src/types";

const previousSourceNative = process.env.ASP_LSP_ENABLE_SOURCE_NATIVE;
const previousBackend = process.env.ASP_LSP_ANALYSIS_BACKEND;
const previousNativeCorePath = process.env.ASP_LSP_NATIVE_CORE_PATH;

afterAll(() => {
  restoreEnv("ASP_LSP_ENABLE_SOURCE_NATIVE", previousSourceNative);
  restoreEnv("ASP_LSP_ANALYSIS_BACKEND", previousBackend);
  restoreEnv("ASP_LSP_NATIVE_CORE_PATH", previousNativeCorePath);
});

function restoreEnv(name: string, value: string | undefined): void {
  if (value === undefined) {
    delete process.env[name];
  } else {
    process.env[name] = value;
  }
}

const platform =
  process.platform === "win32"
    ? "win32"
    : process.platform === "darwin"
      ? "darwin"
      : process.platform === "linux"
        ? "linux"
        : process.platform;
const arch = process.arch === "x64" ? "x64" : process.arch === "arm64" ? "arm64" : process.arch;
const executable = process.platform === "win32" ? "asp-lsp-core.exe" : "asp-lsp-core";
const packagedNativeBinary = path.join(
  import.meta.dirname,
  "..",
  "native",
  `${platform}-${arch}`,
  executable,
);
const sourceNativeBinary = path.join(
  import.meta.dirname,
  "..",
  "..",
  "..",
  "target",
  "debug",
  executable,
);
const nativeBinary = fs.existsSync(sourceNativeBinary) ? sourceNativeBinary : packagedNativeBinary;
const hasNative = fs.existsSync(nativeBinary);

// Force a real native binary to load. Source checkouts normally disable native from src/dist
// paths, and stale packaged binaries should not mask the Rust code under test.
process.env.ASP_LSP_ENABLE_SOURCE_NATIVE = "1";
process.env.ASP_LSP_ANALYSIS_BACKEND = "native";
if (hasNative) {
  process.env.ASP_LSP_NATIVE_CORE_PATH = nativeBinary;
}

// Missing artifact => skip (CI/build may not have produced it). Present artifact that fails to
// load, or a silent TypeScript fallback, is a real failure (asserted via backend === "native").
const describeNative = hasNative ? describe : describe.skip;

function withBackend<T>(mode: string, run: () => T): T {
  const previous = process.env.ASP_LSP_ANALYSIS_BACKEND;
  process.env.ASP_LSP_ANALYSIS_BACKEND = mode;
  try {
    return run();
  } finally {
    if (previous === undefined) {
      delete process.env.ASP_LSP_ANALYSIS_BACKEND;
    } else {
      process.env.ASP_LSP_ANALYSIS_BACKEND = previous;
    }
  }
}

interface FlatToken {
  kind: string;
  start: number;
  end: number;
  text: string;
}

function collectVbTokens(node: AspCstNode): FlatToken[] {
  const out: FlatToken[] = [];
  const visit = (current: AspCstNode): void => {
    const tokens = current.vbscript?.tokens;
    if (tokens) {
      for (const token of tokens) {
        out.push({ kind: token.kind, start: token.start, end: token.end, text: token.text });
      }
    }
    for (const child of current.children) {
      visit(child);
    }
  };
  visit(node);
  return out;
}

function collectRegions(node: AspCstNode): Array<{ kind: string; start: number; end: number }> {
  return node.children.map((child) => ({ kind: child.kind, start: child.start, end: child.end }));
}

describeNative("native analysis backend", () => {
  it("loads the native core instead of the TypeScript fallback", () => {
    parseAspDocument("file:///site/native.asp", "<% Dim x %>");
    expect(aspAnalysisBackendInfo().backend).toBe("native");
  });

  it("keeps native type inference on parsed and from-text documents", async () => {
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
Function MakeValue(flag)
  If flag Then
    MakeValue = 1
  Else
    MakeValue = "x"
  End If
End Function
' @member Holder.Value As String | Number
Class Holder
  Public Value
End Class
' @type typed As Number
Dim typed
typed = "oops"
%>`;
    const uri = "file:///site/native-inference.asp";
    const parsed = parseAspDocument(uri, source);
    expect(aspAnalysisBackendInfo().backend).toBe("native");

    const symbols = collectVbscriptSymbols(parsed);
    expect(symbols.find((symbol) => symbol.name === "x")?.typeName).toBe("Number | String");
    expect(symbols.find((symbol) => symbol.name === "unknownGlobal")?.typeName).toBe("Variant");
    expect(symbols.find((symbol) => symbol.name === "MakeValue")?.typeName).toBe("Number | String");

    const analyzed = analyzeVbscript(parsed);
    expect(analyzed.symbols.find((symbol) => symbol.name === "x")?.typeName).toBe(
      "Number | String",
    );
    expect(
      analyzeVbscript(parsed, { typeChecking: "strict" }).diagnostics.some((diagnostic) =>
        diagnostic.message.includes("is Number, but assigned String"),
      ),
    ).toBe(true);

    const summary = summarizeAspFileAnalysis(parsed);
    expect(summary.vbscript?.localSymbols.find((symbol) => symbol.name === "x")?.typeName).toBe(
      "Number | String",
    );
    expect(
      summary.vbscript?.typeFacts
        .find((type) => type.name === "Holder")
        ?.members.find((member) => member.name === "Value")?.type?.name,
    ).toBe("String | Number");

    const fromTextSymbols = await collectVbscriptSymbolsFromTextAsync(uri, source);
    expect(fromTextSymbols.find((symbol) => symbol.name === "x")?.typeName).toBe("Number | String");
    const fromTextSummary = await summarizeAspFileAnalysisFromTextAsync(uri, source);
    expect(
      fromTextSummary.vbscript?.typeFacts
        .find((type) => type.name === "Holder")
        ?.members.find((member) => member.name === "Value")?.type?.name,
    ).toBe("String | Number");
  });

  it("emits native strict type diagnostics for custom COM types", async () => {
    const source = `<%
Dim widget
widget = Server.CreateObject("Custom.Widget")
widget.Missing
widget.Ping("a", "b")
Set title = "hello"
' @type typedValue As Number
Dim typedValue
typedValue = "hello"
unknownlower()
%>`;
    const uri = "file:///site/native-strict.asp";
    const context = {
      typeChecking: "strict" as const,
      comTypes: {
        "Custom.Widget": {
          members: {
            Ping: {
              kind: "method" as const,
              returnType: "Boolean",
              parameters: [{ name: "name", type: "String" }],
            },
          },
        },
      },
    };
    const parsed = parseAspDocument(uri, source);
    expect(aspAnalysisBackendInfo().backend).toBe("native");

    const diagnostics = analyzeVbscript(parsed, context).diagnostics;
    const codes = diagnostics
      .filter((diagnostic) => diagnostic.source === "asp-lsp-vbscript-type")
      .map((diagnostic) => diagnostic.code);
    expect(codes).toEqual(
      expect.arrayContaining([
        "objectNeedsSet",
        "missingMember",
        "argumentCountMismatch",
        "setScalar",
        "typeMismatch",
        "unknownCall",
      ]),
    );

    const fromTextDiagnostics = (await analyzeVbscriptFromTextAsync(uri, source, {}, context))
      .diagnostics;
    expect(fromTextDiagnostics.map((diagnostic) => diagnostic.code)).toEqual(
      expect.arrayContaining(["objectNeedsSet", "missingMember", "argumentCountMismatch"]),
    );
  });

  it("passes project context to native collectVbscriptSymbols", async () => {
    const source = `<%
Dim widget
widget = Server.CreateObject("Custom.Widget")
result = widget.Ping("x")
%>`;
    const context = {
      comTypes: {
        "Custom.Widget": {
          members: {
            Ping: {
              kind: "method" as const,
              returnType: "Boolean",
              parameters: [{ name: "name", type: "String" }],
            },
          },
        },
      },
    };
    const parsed = parseAspDocument("file:///site/native-context-collect.asp", source);
    expect(aspAnalysisBackendInfo().backend).toBe("native");

    const collected = collectVbscriptSymbols(parsed, context);
    const collectedAsync = await collectVbscriptSymbolsAsync(parsed, context);
    const analyzed = analyzeVbscript(parsed, context).symbols;
    expect(collected.find((symbol) => symbol.name === "result")?.typeName).toBe("Boolean");
    expect(collectedAsync.find((symbol) => symbol.name === "result")?.typeName).toBe("Boolean");
    expect(analyzed.find((symbol) => symbol.name === "result")?.typeName).toBe("Boolean");
  });

  it("uses the shared builtin catalog in native type facts and strict member diagnostics", () => {
    const source = `<%
Dim rs
Set rs = Server.CreateObject("ADODB.Recordset")
rs.MissingMember
%>`;
    const uri = "file:///site/native-builtin-catalog.asp";
    const native = withBackend("native", () => parseAspDocument(uri, source));
    const fallback = withBackend("typescript", () => parseAspDocument(uri, source));

    const nativeSummary = summarizeAspFileAnalysis(native);
    const fallbackSummary = summarizeAspFileAnalysis(fallback);
    const nativeRecordset = nativeSummary.vbscript?.typeFacts.find(
      (type) => type.name === "ADODB.Recordset",
    );
    const fallbackRecordset = fallbackSummary.vbscript?.typeFacts.find(
      (type) => type.name === "ADODB.Recordset",
    );
    expect(nativeRecordset?.members.map((member) => member.name)).toEqual(
      fallbackRecordset?.members.map((member) => member.name),
    );
    expect(
      analyzeVbscript(native, { typeChecking: "strict" }).diagnostics.map(
        (diagnostic) => diagnostic.code,
      ),
    ).toContain("missingMember");
  });

  it("matches TypeScript summary exports and fingerprints in native mode", () => {
    const source = `<%
Dim ExplicitValue
' @type TypedValue As ADODB.Recordset
Dim TypedValue
InferredValue = Server.CreateObject("ADODB.Recordset")
Function PublicFactory()
  Set PublicFactory = Server.CreateObject("ADODB.Recordset")
End Function
%>`;
    const uri = "file:///site/native-summary.asp";
    const native = withBackend("native", () => parseAspDocument(uri, source));
    const fallback = withBackend("typescript", () => parseAspDocument(uri, source));
    const nativeSummary = summarizeAspFileAnalysis(native);
    const fallbackSummary = summarizeAspFileAnalysis(fallback);

    expect(nativeSummary.vbscript?.publicSymbols.map((symbol) => symbol.name)).toEqual(
      fallbackSummary.vbscript?.publicSymbols.map((symbol) => symbol.name),
    );
    expect(nativeSummary.vbscript?.exports).toEqual(fallbackSummary.vbscript?.exports);

    const htmlA = `日本語😀<p>a</p>\n<% Dim ExplicitValue %>`;
    const htmlB = `日本語😀<p>b changed</p>\n<% Dim ExplicitValue %>`;
    const vbChanged = `日本語😀<p>a</p>\n<% Dim ExplicitValue : ExplicitValue = 1 %>`;
    const nativeHtmlA = withBackend("native", () =>
      summarizeAspFileAnalysis(parseAspDocument(uri, htmlA)),
    );
    const nativeHtmlB = withBackend("native", () =>
      summarizeAspFileAnalysis(parseAspDocument(uri, htmlB)),
    );
    const nativeVbChanged = withBackend("native", () =>
      summarizeAspFileAnalysis(parseAspDocument(uri, vbChanged)),
    );
    const fallbackHtmlA = withBackend("typescript", () =>
      summarizeAspFileAnalysis(parseAspDocument(uri, htmlA)),
    );

    expect(nativeHtmlA.languageRegions).toEqual(fallbackHtmlA.languageRegions);
    expect(nativeHtmlA.vbscript?.fingerprint).toBe(fallbackHtmlA.vbscript?.fingerprint);
    expect(nativeHtmlA.vbscript?.fingerprint).toBe(nativeHtmlB.vbscript?.fingerprint);
    expect(nativeHtmlA.vbscript?.fingerprint).not.toBe(nativeVbChanged.vbscript?.fingerprint);
  });

  it("keeps async VBScript analysis on TypeScript semantics for project context", async () => {
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
    const context = { documents: [include, page], symbols };
    const syncDiagnostics = analyzeVbscript(include, context).diagnostics;
    expect(syncDiagnostics).toHaveLength(0);

    const asyncDiagnostics = (await analyzeVbscriptAsync(include, context)).diagnostics;
    const fromTextDiagnostics = (
      await analyzeVbscriptFromTextAsync(include.uri, include.text, {}, context)
    ).diagnostics;
    expect(asyncDiagnostics).toEqual(syncDiagnostics);
    expect(fromTextDiagnostics).toEqual(syncDiagnostics);
  });

  it("reconstructs the VB CST via the binary columnar hydration path", async () => {
    const source = `<%
Class Foo
  Public Sub Bar(x, y)
    Dim total
    total = x + y * 2
    Response.Write "値: " & total
  End Sub
End Class
%>
<p>テキスト 😀</p>
<% Dim arr(3) : Set obj = Server.CreateObject("ADODB.Recordset") %>`;
    const uri = "file:///site/native-hydrate.asp";
    // 同期フル parse（native, JSON 経路で VB CST を完全に持つ）を基準にする。
    const full = parseAspDocument(uri, source);
    expect(aspAnalysisBackendInfo().backend).toBe("native");
    const fullTokens = collectVbTokens(full.cst);
    expect(fullTokens.length).toBeGreaterThan(0);

    // 非同期 shallow parse は VB CST を持たない。
    const shallow = await parseAspDocumentAsync(uri, source);
    expect(collectVbTokens(shallow.cst)).toHaveLength(0);

    // バイナリ列指向経路で VB CST を hydrate し、フル parse とトークン一致を確認する。
    await hydrateVbscriptCst(shallow);
    expect(collectVbTokens(shallow.cst)).toEqual(fullTokens);
  });

  it("returns native embedded skeleton parses and hydrates VBScript on demand", async () => {
    const source = `<%@ Language=VBScript %>
<style>.x{color:red}</style>
<script>const value = 1;</script>
<%
Dim total
total = 1
%>`;
    const uri = "file:///site/native-skeleton.asp";
    const full = parseAspDocument(uri, source);
    const skeleton = await parseAspDocumentSkeletonAsync(uri, source);
    expect(aspAnalysisBackendInfo().backend).toBe("native");
    expect(skeleton.text).toBe(source);
    expect(skeleton.regions).toEqual(full.regions);
    expect(skeleton.directives).toEqual(full.directives);
    expect(skeleton.includes).toEqual(full.includes);
    expect(skeleton.serverObjects).toEqual(full.serverObjects);
    expect(skeleton.defaultLanguage).toBe(full.defaultLanguage);
    expect(skeleton.diagnostics).toEqual(full.diagnostics);
    expect(collectVbTokens(skeleton.cst)).toHaveLength(0);

    await hydrateVbscriptCst(skeleton);
    expect(collectVbTokens(skeleton.cst)).toEqual(collectVbTokens(full.cst));
  });

  it("uses the native skeleton path for async shallow parses", async () => {
    const source = `<%@ Language=VBScript %>
<!--#include file="shared.inc"-->
<style>.name{content:"値 😀"}</style>
<script language="JScript">var value = 1;</script>
<%
Dim total
total = 1
%>`;
    const uri = "file:///site/native-async-skeleton.asp";
    const shallow = await parseAspDocumentAsync(uri, source);
    const skeleton = await parseAspDocumentSkeletonAsync(uri, source);
    expect(aspAnalysisBackendInfo().backend).toBe("native");
    expect(shallow.regions).toEqual(skeleton.regions);
    expect(shallow.directives).toEqual(skeleton.directives);
    expect(shallow.includes).toEqual(skeleton.includes);
    expect(shallow.serverObjects).toEqual(skeleton.serverObjects);
    expect(shallow.defaultLanguage).toBe(skeleton.defaultLanguage);
    expect(shallow.diagnostics).toEqual(skeleton.diagnostics);
    expect(collectRegions(shallow.cst)).toEqual(collectRegions(skeleton.cst));
    expect(collectVbTokens(shallow.cst)).toHaveLength(0);
  });

  it("emits Array type information for VBScript array declarations", () => {
    const source = `<%
Dim fixedItems(10)
Dim dynamicItems()
ReDim resizedItems(5)
Dim a(1), plain, matrix(2, 3)
%>`;
    const parsed = parseAspDocument("file:///site/native-arrays.asp", source);
    expect(aspAnalysisBackendInfo().backend).toBe("native");
    const symbols = collectVbscriptSymbols(
      parsed,
      {},
      { inferTypes: false, variantFallback: false },
    );
    expect(symbols.find((symbol) => symbol.name === "fixedItems")).toMatchObject({
      typeName: "Array",
      array: { kind: "fixed", dimensions: ["10"] },
    });
    expect(symbols.find((symbol) => symbol.name === "dynamicItems")).toMatchObject({
      typeName: "Array",
      array: { kind: "dynamic", dimensions: [] },
    });
    expect(symbols.find((symbol) => symbol.name === "resizedItems")).toMatchObject({
      typeName: "Array",
      array: { kind: "dynamic", dimensions: ["5"] },
    });
    expect(symbols.find((symbol) => symbol.name === "matrix")).toMatchObject({
      typeName: "Array",
      array: { kind: "fixed", dimensions: ["2", "3"] },
    });
    // A bare identifier in a multi-name Dim must not be typed as an array.
    expect(symbols.find((symbol) => symbol.name === "plain")?.typeName).not.toBe("Array");
  });

  it("keeps VBScript diagnostics equivalent between parsed native and from-text entrypoints", async () => {
    const source = `<%
' @returns String
Function BuildName(unusedArg)
  implicitLocal = 1
  BuildName = MissingFactory(implicitLocal)
End Function
%>`;
    const uri = "file:///site/native-vb-analysis.asp";
    const parsed = parseAspDocument(uri, source);
    const direct = analyzeVbscript(parsed, { unusedDiagnostics: true });
    expect(aspAnalysisBackendInfo().backend).toBe("native");
    const fromText = await analyzeVbscriptFromTextAsync(
      uri,
      source,
      {},
      { unusedDiagnostics: true },
    );
    expect(fromText.symbols).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ name: "BuildName", typeName: "String", explicitType: true }),
        expect.objectContaining({ name: "implicitLocal", implicit: true }),
      ]),
    );
    expect(direct.symbols).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ name: "BuildName", typeName: "String", explicitType: true }),
        expect.objectContaining({ name: "implicitLocal", implicit: true }),
      ]),
    );
    expect(direct.diagnostics).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ source: "asp-lsp-vbscript-unused", code: "vbscript:unused" }),
      ]),
    );

    const strictSource = `<%
Option Explicit
Response.Write MissingValue
%>`;
    const strictUri = "file:///site/native-vb-strict.asp";
    const strictParsed = parseAspDocument(strictUri, strictSource);
    expect((await analyzeVbscriptFromTextAsync(strictUri, strictSource)).diagnostics).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          message: expect.stringContaining("MissingValue"),
          source: "asp-lsp-vbscript",
        }),
      ]),
    );
    expect(analyzeVbscript(strictParsed).diagnostics).toEqual(
      expect.arrayContaining([expect.objectContaining({ code: "vbscript:undeclared" })]),
    );

    expect(summarizeAspFileAnalysis(parsed).vbscript?.externalRefs).toEqual(
      expect.arrayContaining([expect.objectContaining({ name: "MissingFactory" })]),
    );
  });

  it("matches the TypeScript parser token offsets, including non-ASCII prefixes", () => {
    const source = `<%
' 日本語コメント 😀 prefix
Dim arr(10)
Response.Write "値は %> ではない"
%>
<p>ます</p>
<% Set obj = Server.CreateObject("ADODB.Recordset") %>`;
    const uri = "file:///site/native-offsets.asp";
    const native = withBackend("native", () => parseAspCst(uri, source));
    const fallback = withBackend("typescript", () => parseAspCst(uri, source));
    expect(collectVbTokens(native)).toEqual(collectVbTokens(fallback));
    expect(collectRegions(native)).toEqual(collectRegions(fallback));
  });

  it("closes ASP delimiters at the same offsets as the TypeScript parser", () => {
    const source = `<%
Response.Write "%>"
' comment with %>
Response.Write "done"
%>
<%@ LANGUAGE="JScript" %><% var text = '%>'; Response.Write(text); %>`;
    const uri = "file:///site/native-delimiters.asp";
    const native = withBackend("native", () => parseAspCst(uri, source));
    const fallback = withBackend("typescript", () => parseAspCst(uri, source));
    expect(collectRegions(native)).toEqual(collectRegions(fallback));
    const nativeBlocks = native.children.filter(
      (child) => child.kind === "AspBlock" || child.kind === "AspDirective",
    );
    expect(nativeBlocks).toHaveLength(3);
    const firstBlock = source.slice(nativeBlocks[0].contentStart, nativeBlocks[0].contentEnd);
    expect(nativeBlocks[0].end).toBe(source.indexOf("%>") + "%>".length);
    expect(firstBlock).toContain('Response.Write "');
    expect(firstBlock).not.toContain("' comment with ");
    expect(firstBlock).not.toContain('Response.Write "done"');
    const jscriptBlock = source.slice(nativeBlocks[2].contentStart, nativeBlocks[2].contentEnd);
    expect(jscriptBlock).toContain("var text = '");
    expect(jscriptBlock).not.toContain("Response.Write(text)");
  });

  it("closes JScript strings and comments at the same ASP delimiter offsets as the TypeScript parser", () => {
    const source = `<%@ LANGUAGE="JScript" %>
<%
// line comment with %>
Response.Write("line")
%>
<%
/* block comment with %> */
Response.Write("block")
%>
<%
var text = '%>';
Response.Write(text)
%>`;
    const uri = "file:///site/native-jscript-comment-delimiters.asp";
    const native = withBackend("native", () => parseAspCst(uri, source));
    const fallback = withBackend("typescript", () => parseAspCst(uri, source));
    expect(collectRegions(native)).toEqual(collectRegions(fallback));
    const nativeBlocks = native.children.filter(
      (child) => child.kind === "AspBlock" || child.kind === "AspDirective",
    );
    expect(nativeBlocks).toHaveLength(4);
    const lineBlock = source.slice(nativeBlocks[1].contentStart, nativeBlocks[1].contentEnd);
    const blockCommentBlock = source.slice(
      nativeBlocks[2].contentStart,
      nativeBlocks[2].contentEnd,
    );
    expect(lineBlock).toContain("// line comment with ");
    expect(lineBlock).not.toContain('Response.Write("line")');
    expect(blockCommentBlock).toContain("/* block comment with ");
    expect(blockCommentBlock).not.toContain('Response.Write("block")');
    const stringBlock = source.slice(nativeBlocks[3].contentStart, nativeBlocks[3].contentEnd);
    expect(stringBlock).toContain("var text = '");
    expect(stringBlock).not.toContain("Response.Write(text)");
  });

  it("matches the TypeScript parser for script and style close boundaries", () => {
    const source = `日本語😀
<script>const prefix = "</scriptx>"; const afterPrefix = true;</SCRIPT>
<style>.x { color: red; }</style data-x="1">
<script>const raw = "</script>"; const afterRaw = false;</script>`;
    const uri = "file:///site/native-element-close-boundaries.asp";
    const native = withBackend("native", () => parseAspCst(uri, source));
    const fallback = withBackend("typescript", () => parseAspCst(uri, source));
    expect(collectRegions(native)).toEqual(collectRegions(fallback));

    const scripts = native.children.filter((child) => child.kind === "ClientScriptElement");
    expect(scripts).toHaveLength(2);
    const prefixScript = source.slice(scripts[0].contentStart, scripts[0].contentEnd);
    const rawScript = source.slice(scripts[1].contentStart, scripts[1].contentEnd);
    expect(prefixScript).toContain("afterPrefix");
    expect(rawScript).toContain('const raw = "');
    expect(rawScript).not.toContain("afterRaw");

    const style = native.children.find((child) => child.kind === "StyleElement");
    expect(style).toBeTruthy();
    expect(source.slice(style!.start, style!.end)).toContain('</style data-x="1">');
  });
});
