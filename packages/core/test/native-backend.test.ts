import fs from "node:fs";
import path from "node:path";
import { afterAll, describe, expect, it } from "vitest";
import {
  aspAnalysisBackendInfo,
  collectVbscriptSymbols,
  parseAspCst,
  parseAspDocument,
} from "../src/index";
import type { AspCstNode } from "../src/types";

// Force the source-checkout native binary to load. Without this the backend resolver
// disables native in src/dist checkouts, so the tests below would silently run on the
// TypeScript fallback (see native-backend.ts sourceCheckoutCoreDist).
const previousSourceNative = process.env.ASP_LSP_ENABLE_SOURCE_NATIVE;
const previousBackend = process.env.ASP_LSP_ANALYSIS_BACKEND;
process.env.ASP_LSP_ENABLE_SOURCE_NATIVE = "1";
process.env.ASP_LSP_ANALYSIS_BACKEND = "native";

afterAll(() => {
  restoreEnv("ASP_LSP_ENABLE_SOURCE_NATIVE", previousSourceNative);
  restoreEnv("ASP_LSP_ANALYSIS_BACKEND", previousBackend);
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
const nativeBinary = path.join(
  import.meta.dirname,
  "..",
  "native",
  `${platform}-${arch}`,
  executable,
);
const hasNative = fs.existsSync(nativeBinary);

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
    expect(source.slice(nativeBlocks[0].contentStart, nativeBlocks[0].contentEnd)).toContain(
      'Response.Write "done"',
    );
  });
});
