import { describe, expect, it } from "vitest";
import {
  analyzeVbscriptFromTextAsync,
  aspAnalysisBackendInfo,
  parseAspDocument,
  parseAspDocumentAsync,
  shouldUseNativeAsyncSkeletonParse,
  tryNativeParseAspDocumentAsync,
} from "../src";

describe("native analysis backend compatibility shim", () => {
  it("falls back to the TypeScript parser after the Rust LSP cutover", () => {
    withBackend("native", () => {
      const parsed = parseAspDocument("file:///site/native.asp", `<% Response.Write "ok" %>`);

      expect(parsed.defaultLanguage).toBe("VBScript");
      expect(aspAnalysisBackendInfo()).toMatchObject({
        backend: "typescript-fallback",
        engine: "typescript",
        reason: "native backend removed after Rust LSP cutover",
      });
    });
  });

  it("keeps direct native entrypoints available as no-op fallbacks", async () => {
    await withBackend("auto", async () => {
      await expect(
        tryNativeParseAspDocumentAsync("file:///site/native.asp", `<% Response.Write "ok" %>`, {}),
      ).resolves.toBeUndefined();
      expect(aspAnalysisBackendInfo()).toMatchObject({
        backend: "typescript-fallback",
        engine: "typescript",
        reason: "native backend removed after Rust LSP cutover",
      });
    });
  });

  it("does not use the retired native async skeleton path", async () => {
    await withBackend("native", async () => {
      expect(shouldUseNativeAsyncSkeletonParse()).toBe(false);
      const parsed = await parseAspDocumentAsync(
        "file:///site/native-async.asp",
        `<% Response.Write "ok" %>`,
      );

      expect(parsed.defaultLanguage).toBe("VBScript");
      expect(aspAnalysisBackendInfo()).toMatchObject({
        backend: "typescript-fallback",
        engine: "typescript",
        reason: "native backend removed after Rust LSP cutover",
      });
    });
  });

  it("preserves explicit TypeScript and unsupported backend status reasons", async () => {
    await withBackend("typescript", async () => {
      await analyzeVbscriptFromTextAsync(
        "file:///site/typescript.asp",
        `<% Response.Write "ok" %>`,
      );
      expect(aspAnalysisBackendInfo()).toMatchObject({
        backend: "typescript-fallback",
        engine: "typescript",
        reason: "disabled by ASP_LSP_ANALYSIS_BACKEND=typescript",
      });
    });

    await withBackend("wasm", () => {
      parseAspDocument("file:///site/wasm.asp", `<% Response.Write "ok" %>`);
      expect(aspAnalysisBackendInfo()).toMatchObject({
        backend: "typescript-fallback",
        engine: "typescript",
        reason: "unsupported ASP_LSP_ANALYSIS_BACKEND=wasm",
      });
    });
  });
});

async function withBackend<T>(mode: string, run: () => T | Promise<T>): Promise<T> {
  const previous = process.env.ASP_LSP_ANALYSIS_BACKEND;
  process.env.ASP_LSP_ANALYSIS_BACKEND = mode;
  try {
    return await run();
  } finally {
    if (previous === undefined) {
      delete process.env.ASP_LSP_ANALYSIS_BACKEND;
    } else {
      process.env.ASP_LSP_ANALYSIS_BACKEND = previous;
    }
  }
}
