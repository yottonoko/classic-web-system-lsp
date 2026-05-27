import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { pathToFileURL } from "node:url";
import { parseAspDocument, summarizeAspFileAnalysis } from "@asp-lsp/core";
import { describe, expect, it } from "vitest";
import {
  DiskAnalysisCache,
  diskAnalysisCacheFormatVersion,
  fileAnalysisSummaryFormatVersion,
  vbPublicSymbolSummaryFormatVersion,
  type DiskAnalysisCachePayload,
  type DiskFileAnalysisSummaryPayload,
  type VbPublicSymbolSummary,
} from "../src/disk-analysis-cache";

describe("DiskAnalysisCache", () => {
  it("stores fresh CBOR entries and treats stale or corrupt entries as misses", async () => {
    const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-cbor-cache-"));
    try {
      const sourceFile = path.join(tempDir, "default.asp");
      fs.writeFileSync(sourceFile, `<% Response.Write "ok" %>`, "utf8");
      const stat = fs.statSync(sourceFile);
      const source = {
        uri: pathToFileURL(sourceFile).href,
        fileName: sourceFile,
        mtimeMs: stat.mtimeMs,
        size: stat.size,
      };
      const cache = new DiskAnalysisCache({
        enabled: true,
        directory: path.join(tempDir, "cache"),
        workspaceRoots: [tempDir],
        ttlHours: 168,
        maxSizeMb: 1024,
      });
      const payload: DiskAnalysisCachePayload = {
        version: diskAnalysisCacheFormatVersion,
        source,
        settingsKey: "settings",
        parsed: parseAspDocument(source.uri, fs.readFileSync(sourceFile, "utf8")),
        diagnostics: { key: "diagnostics", items: [] },
        fastDiagnostics: { key: "fast", items: [] },
        includeDiagnostics: { key: "include", items: [] },
        syntaxDiagnostics: { key: "syntax", items: [] },
        projectDiagnostics: { key: "project", items: [] },
        fileSummary: summarizeAspFileAnalysis(
          parseAspDocument(source.uri, fs.readFileSync(sourceFile, "utf8")),
        ),
      };

      await cache.write(payload);
      const files = collectFiles(cache.root);
      expect(files.filter((fileName) => fileName.endsWith(".cbor"))).toHaveLength(2);
      expect(files.some((fileName) => fileName.endsWith(".diagnostics.cbor"))).toBe(true);
      expect(files.some((fileName) => fileName.endsWith(".json"))).toBe(false);
      expect(await cache.readFresh(source, "settings")).toMatchObject({
        settingsKey: "settings",
        source: { fileName: sourceFile },
        diagnostics: { key: "diagnostics" },
        fastDiagnostics: { key: "fast" },
        includeDiagnostics: { key: "include" },
        syntaxDiagnostics: { key: "syntax" },
        projectDiagnostics: { key: "project" },
      });
      const diagnostics = await cache.readDiagnosticsFresh(source, "settings");
      expect(diagnostics).toMatchObject({
        settingsKey: "settings",
        source: { fileName: sourceFile },
        diagnostics: { key: "diagnostics" },
      });
      expect(diagnostics).not.toHaveProperty("parsed");

      expect(
        await cache.readFresh({ ...source, size: source.size + 1 }, "settings"),
      ).toBeUndefined();
      expect(
        await cache.readDiagnosticsFresh({ ...source, size: source.size + 1 }, "settings"),
      ).toBeUndefined();

      fs.writeFileSync(
        files.find(
          (fileName) => fileName.endsWith(".cbor") && !fileName.endsWith(".diagnostics.cbor"),
        ) ?? "",
        "broken",
      );
      expect(await cache.readFresh(source, "settings")).toBeUndefined();
    } finally {
      fs.rmSync(tempDir, { recursive: true, force: true });
    }
  });

  it("stores file analysis summaries in a separate CBOR entry", async () => {
    const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-file-summary-cache-"));
    try {
      const sourceFile = path.join(tempDir, "default.asp");
      fs.writeFileSync(sourceFile, `<% Response.Write SharedTitle() %>`, "utf8");
      const stat = fs.statSync(sourceFile);
      const source = {
        uri: pathToFileURL(sourceFile).href,
        fileName: sourceFile,
        mtimeMs: stat.mtimeMs,
        size: stat.size,
      };
      const cache = new DiskAnalysisCache({
        enabled: true,
        directory: path.join(tempDir, "cache"),
        workspaceRoots: [tempDir],
        ttlHours: 168,
        maxSizeMb: 1024,
      });
      const summary = summarizeAspFileAnalysis(
        parseAspDocument(source.uri, fs.readFileSync(sourceFile, "utf8")),
      );
      const payload: DiskFileAnalysisSummaryPayload = {
        version: fileAnalysisSummaryFormatVersion,
        source,
        settingsKey: "summary-settings",
        summary,
      };

      await cache.writeFileAnalysisSummary(payload);

      const files = collectFiles(cache.root).filter((fileName) => fileName.endsWith(".cbor"));
      expect(files).toHaveLength(1);
      expect(files[0]).toContain(".file-summary.cbor");
      await expect(
        cache.readFileAnalysisSummaryFresh(source, "summary-settings"),
      ).resolves.toMatchObject({
        settingsKey: "summary-settings",
        summary: {
          uri: source.uri,
          vbscript: {
            externalRefs: [expect.objectContaining({ name: "SharedTitle" })],
          },
        },
      });
    } finally {
      fs.rmSync(tempDir, { recursive: true, force: true });
    }
  });

  it("sweeps old and oversized CBOR entries", async () => {
    const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-cbor-sweep-"));
    try {
      const cache = new DiskAnalysisCache({
        enabled: true,
        directory: path.join(tempDir, "cache"),
        workspaceRoots: [tempDir],
        ttlHours: 1,
        maxSizeMb: 1,
      });
      const oldFile = path.join(cache.root, "old.cbor");
      const largeFile = path.join(cache.root, "large.cbor");
      await fs.promises.mkdir(cache.root, { recursive: true });
      fs.writeFileSync(oldFile, Buffer.alloc(128));
      fs.writeFileSync(largeFile, Buffer.alloc(2 * 1024 * 1024));
      const oldDate = new Date(Date.now() - 2 * 60 * 60 * 1000);
      fs.utimesSync(oldFile, oldDate, oldDate);

      await cache.sweep();

      expect(fs.existsSync(oldFile)).toBe(false);
      expect(fs.existsSync(largeFile)).toBe(false);
    } finally {
      fs.rmSync(tempDir, { recursive: true, force: true });
    }
  });

  it("stores VBScript public symbol summaries in a separate CBOR entry", async () => {
    const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-public-summary-cache-"));
    try {
      const sourceFile = path.join(tempDir, "common.inc");
      fs.writeFileSync(sourceFile, `<% Function SharedTitle()\nEnd Function %>`, "utf8");
      const stat = fs.statSync(sourceFile);
      const source = {
        uri: pathToFileURL(sourceFile).href,
        fileName: sourceFile,
        mtimeMs: stat.mtimeMs,
        size: stat.size,
      };
      const cache = new DiskAnalysisCache({
        enabled: true,
        directory: path.join(tempDir, "cache"),
        workspaceRoots: [tempDir],
        ttlHours: 168,
        maxSizeMb: 1024,
      });
      const payload: VbPublicSymbolSummary = {
        version: vbPublicSymbolSummaryFormatVersion,
        source,
        settingsKey: "public-settings",
        defaultLanguage: "vbscript",
        legacyEncoding: "utf8",
        includes: [],
        publicSymbols: [
          {
            name: "SharedTitle",
            kind: "function",
            range: { start: { line: 0, character: 12 }, end: { line: 0, character: 23 } },
            sourceUri: source.uri,
            typeName: "Variant",
            type: { name: "Variant" },
          },
        ],
        exports: [
          {
            name: "SharedTitle",
            kind: "function",
            range: { start: { line: 0, character: 12 }, end: { line: 0, character: 23 } },
          },
        ],
        externalRefs: [],
      };

      await cache.writeVbPublicSymbolSummary(payload);
      const files = collectFiles(cache.root).filter((fileName) => fileName.endsWith(".cbor"));
      expect(files).toHaveLength(1);
      expect(files[0]).toContain(".vb-public-symbols.cbor");
      expect(await cache.readFresh(source, "public-settings")).toBeUndefined();
      expect(await cache.readVbPublicSymbolSummaryFresh(source, "public-settings")).toMatchObject({
        settingsKey: "public-settings",
        publicSymbols: [{ name: "SharedTitle" }],
      });
      expect(
        await cache.readVbPublicSymbolSummaryFresh(
          { ...source, mtimeMs: source.mtimeMs + 1 },
          "public-settings",
        ),
      ).toBeUndefined();
    } finally {
      fs.rmSync(tempDir, { recursive: true, force: true });
    }
  });
});

function collectFiles(root: string): string[] {
  if (!fs.existsSync(root)) {
    return [];
  }
  return fs.readdirSync(root).flatMap((entry) => {
    const fileName = path.join(root, entry);
    return fs.statSync(fileName).isDirectory() ? collectFiles(fileName) : [fileName];
  });
}
