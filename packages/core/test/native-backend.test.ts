import fs from "node:fs";
import path from "node:path";
import { afterAll, describe, expect, it } from "vitest";
import {
  analyzeVbscript,
  analyzeVbscriptAsync,
  analyzeVbscriptFromTextAsync,
  aspAnalysisBackendInfo,
  collectVbscriptSymbols,
  hydrateVbscriptCst,
  parseAspCst,
  parseAspDocument,
  parseAspDocumentAsync,
  parseAspDocumentSkeletonAsync,
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

  it("emits Array type information for VBScript array declarations", () => {
    const source = `<%
Dim fixedItems(10)
Dim dynamicItems()
ReDim resizedItems(5)
Dim a(1), plain, matrix(2, 3)
%>`;
    const parsed = parseAspDocument("file:///site/native-arrays.asp", source);
    expect(aspAnalysisBackendInfo().backend).toBe("native");
    const symbols = collectVbscriptSymbols(parsed);
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
});
