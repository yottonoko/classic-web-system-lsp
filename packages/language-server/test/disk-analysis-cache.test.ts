import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { describe, expect, it } from "vitest";
import { DiskAnalysisCache } from "../src/disk-analysis-cache";
import type { AspParsedDocument, FileAnalysisSummary } from "@asp-lsp/core";

describe("DiskAnalysisCache", () => {
  it("restores matching diagnostics and rejects stale metadata", async () => {
    const directory = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-disk-cache-"));
    try {
      const cache = new DiskAnalysisCache({
        enabled: true,
        directory,
        namespace: "test",
        toolVersion: "1",
      });
      const lookup = {
        source: { fileName: "/site/default.asp", mtimeMs: 1, size: 10 },
        settingsKey: "settings",
      };
      await cache.write({
        ...lookup,
        diagnostics: [
          {
            message: "cached",
            range: {
              start: { line: 0, character: 0 },
              end: { line: 0, character: 1 },
            },
          },
        ],
      });
      expect((await cache.read(lookup))?.[0]?.message).toBe("cached");
      expect(
        await cache.read({
          ...lookup,
          source: { ...lookup.source, size: 11 },
        }),
      ).toBeUndefined();
    } finally {
      fs.rmSync(directory, { recursive: true, force: true });
    }
  });

  it("restores matching include refs and rejects stale metadata", async () => {
    const directory = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-disk-cache-"));
    try {
      const cache = new DiskAnalysisCache({
        enabled: true,
        directory,
        namespace: "test",
        toolVersion: "1",
      });
      const lookup = {
        source: { fileName: "/site/default.asp", mtimeMs: 1, size: 10 },
        settingsKey: "include-refs",
      };
      await cache.writeIncludeRefs({
        ...lookup,
        fingerprint: "refs",
        includeRefs: [
          {
            offset: 0,
            range: {
              start: { line: 0, character: 0 },
              end: { line: 0, character: 36 },
            },
            directiveRange: {
              start: { line: 0, character: 5 },
              end: { line: 0, character: 13 },
            },
            mode: "file",
            modeRange: {
              start: { line: 0, character: 14 },
              end: { line: 0, character: 18 },
            },
            path: "shared.inc",
            pathRange: {
              start: { line: 0, character: 19 },
              end: { line: 0, character: 31 },
            },
          },
        ],
      });
      expect((await cache.readIncludeRefs(lookup))?.includeRefs[0]?.path).toBe("shared.inc");
      expect(
        await cache.readIncludeRefs({
          ...lookup,
          source: { ...lookup.source, size: 11 },
        }),
      ).toBeUndefined();
    } finally {
      fs.rmSync(directory, { recursive: true, force: true });
    }
  });

  it("restores matching VB symbol indexes and rejects stale metadata", async () => {
    const directory = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-disk-cache-"));
    try {
      const cache = new DiskAnalysisCache({
        enabled: true,
        directory,
        namespace: "test",
        toolVersion: "1",
      });
      const lookup = {
        source: { fileName: "/site/default.asp", mtimeMs: 1, size: 10 },
        settingsKey: "graph-index",
      };
      await cache.writeVbSymbolIndex({
        ...lookup,
        fingerprint: "vb",
        index: {
          uri: "file:///site/default.asp",
          declarations: [
            {
              id: "file:///site/default.asp#sub:main:0",
              name: "Main",
              normalizedName: "main",
              kind: "sub",
              range: {
                start: { line: 0, character: 0 },
                end: { line: 1, character: 7 },
              },
              nameRange: {
                start: { line: 0, character: 4 },
                end: { line: 0, character: 8 },
              },
            },
          ],
          references: [],
          callSites: [],
          deferredExternalRefs: [],
          includeRefs: [],
          stats: {
            regions: 1,
            tokens: 2,
            declarations: 1,
            references: 0,
            callSites: 0,
            deferredExternalRefs: 0,
          },
        },
      });
      expect((await cache.readVbSymbolIndex(lookup))?.index.declarations[0]?.name).toBe("Main");
      expect(
        await cache.readVbSymbolIndex({
          ...lookup,
          source: { ...lookup.source, size: 11 },
        }),
      ).toBeUndefined();
    } finally {
      fs.rmSync(directory, { recursive: true, force: true });
    }
  });

  it("restores parsed documents and rejects stale metadata or settings", async () => {
    const directory = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-disk-cache-"));
    try {
      const cache = new DiskAnalysisCache({
        enabled: true,
        directory,
        namespace: "test",
        toolVersion: "1",
      });
      const lookup = {
        source: { fileName: "/site/default.asp", mtimeMs: 1, size: 10 },
        settingsKey: "parsed-settings",
      };
      const parsed: AspParsedDocument = {
        uri: "file:///site/default.asp",
        text: "<% Sub Main()\nEnd Sub %>",
        cst: {
          kind: "Document",
          start: 0,
          end: 24,
          contentStart: 0,
          contentEnd: 24,
          tokens: [],
          children: [],
        },
        regions: [],
        directives: [],
        includes: [],
        serverObjects: [],
        defaultLanguage: "VBScript",
        diagnostics: [],
      };
      const summary: FileAnalysisSummary = {
        uri: parsed.uri,
        fingerprint: "parsed",
        defaultLanguage: "VBScript",
        languageRegions: [],
        includeRefs: [],
        diagnostics: [],
      };
      await cache.writeParsedDocument({
        ...lookup,
        parsed,
        summary,
        publicSignature: { fingerprint: "public" },
      });
      expect((await cache.readParsedDocument(lookup))?.parsed.text).toBe(parsed.text);
      expect(
        await cache.readParsedDocument({
          ...lookup,
          source: { ...lookup.source, size: 11 },
        }),
      ).toBeUndefined();
      expect(
        await cache.readParsedDocument({
          ...lookup,
          settingsKey: "different",
        }),
      ).toBeUndefined();
      const expiredCache = new DiskAnalysisCache({
        enabled: true,
        directory,
        ttlHours: 0.000001,
        namespace: "test",
        toolVersion: "1",
      });
      await new Promise((resolve) => setTimeout(resolve, 10));
      expect(await expiredCache.readParsedDocument(lookup)).toBeUndefined();
    } finally {
      fs.rmSync(directory, { recursive: true, force: true });
    }
  });

  it("restores workspace indexes and rejects stale settings", async () => {
    const directory = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-disk-cache-"));
    try {
      const cache = new DiskAnalysisCache({
        enabled: true,
        directory,
        namespace: "test",
        toolVersion: "1",
      });
      await cache.writeWorkspaceIndex({
        settingsKey: "workspace-settings",
        truncated: false,
        entries: [
          {
            uri: "file:///site/default.asp",
            fileName: "/site/default.asp",
            mtimeMs: 1,
            size: 10,
          },
        ],
      });
      expect((await cache.readWorkspaceIndex("workspace-settings"))?.entries[0]?.uri).toBe(
        "file:///site/default.asp",
      );
      expect(await cache.readWorkspaceIndex("different")).toBeUndefined();
      const expiredCache = new DiskAnalysisCache({
        enabled: true,
        directory,
        ttlHours: 0.000001,
        namespace: "test",
        toolVersion: "1",
      });
      await new Promise((resolve) => setTimeout(resolve, 10));
      expect(await expiredCache.readWorkspaceIndex("workspace-settings")).toBeUndefined();
    } finally {
      fs.rmSync(directory, { recursive: true, force: true });
    }
  });

  it("restores workspace include graphs and rejects stale settings", async () => {
    const directory = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-disk-cache-"));
    try {
      const cache = new DiskAnalysisCache({
        enabled: true,
        directory,
        namespace: "test",
        toolVersion: "1",
      });
      await cache.writeWorkspaceIncludeGraph({
        settingsKey: "include-graph-settings",
        entries: [
          {
            fileName: "/site/default.asp",
            source: { fileName: "/site/default.asp", mtimeMs: 1, size: 10 },
            targetFileNames: ["/site/shared.inc"],
            refsFingerprint: "refs",
          },
        ],
      });
      expect(
        (await cache.readWorkspaceIncludeGraph("include-graph-settings"))?.entries[0]
          ?.targetFileNames[0],
      ).toBe("/site/shared.inc");
      expect(await cache.readWorkspaceIncludeGraph("different")).toBeUndefined();
      const expiredCache = new DiskAnalysisCache({
        enabled: true,
        directory,
        ttlHours: 0.000001,
        namespace: "test",
        toolVersion: "1",
      });
      await new Promise((resolve) => setTimeout(resolve, 10));
      expect(
        await expiredCache.readWorkspaceIncludeGraph("include-graph-settings"),
      ).toBeUndefined();
    } finally {
      fs.rmSync(directory, { recursive: true, force: true });
    }
  });

  it("drops corrupt and expired entries during read and sweep", async () => {
    const directory = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-disk-cache-"));
    try {
      let cache = new DiskAnalysisCache({
        enabled: true,
        directory,
        ttlHours: 1,
        namespace: "test",
        toolVersion: "1",
      });
      const lookup = {
        source: { fileName: "/site/default.asp", mtimeMs: 1, size: 10 },
        settingsKey: "settings",
      };
      await cache.write({ ...lookup, diagnostics: [] });
      const fileName = cacheFiles(directory)[0];
      fs.writeFileSync(fileName, "not-cbor");
      expect(await cache.read(lookup)).toBeUndefined();

      await cache.write({ ...lookup, diagnostics: [] });
      cache = new DiskAnalysisCache({
        enabled: true,
        directory,
        ttlHours: 0.000001,
        namespace: "test",
        toolVersion: "1",
      });
      await new Promise((resolve) => setTimeout(resolve, 10));
      await cache.sweep();
      expect(cacheFiles(directory)).toHaveLength(0);
    } finally {
      fs.rmSync(directory, { recursive: true, force: true });
    }
  });

  it("clears and sweeps by maximum size", async () => {
    const directory = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-disk-cache-"));
    try {
      const cache = new DiskAnalysisCache({
        enabled: true,
        directory,
        maxSizeMb: 0.000001,
        namespace: "test",
        toolVersion: "1",
      });
      for (let index = 0; index < 32; index += 1) {
        await cache.write({
          source: { fileName: `/site/${index}.asp`, mtimeMs: index, size: 10 },
          settingsKey: "settings",
          diagnostics: Array.from({ length: 64 }, (_, diagnosticIndex) => ({
            message: `diagnostic-${index}-${diagnosticIndex}`,
            range: {
              start: { line: diagnosticIndex, character: 0 },
              end: { line: diagnosticIndex, character: 1 },
            },
          })),
        });
      }
      const beforeSweep = cacheFiles(directory).length;
      await cache.sweep();
      expect(beforeSweep).toBeGreaterThan(0);
      expect(cacheFiles(directory).length).toBeLessThan(beforeSweep);
      await cache.clear();
      expect(fs.existsSync(directory)).toBe(false);
    } finally {
      fs.rmSync(directory, { recursive: true, force: true });
    }
  });

  it("stores entries under v6 shard directories", async () => {
    const directory = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-disk-cache-"));
    try {
      const cache = new DiskAnalysisCache({
        enabled: true,
        directory,
        namespace: "test",
        toolVersion: "1",
      });
      const lookup = {
        source: { fileName: "/site/default.asp", mtimeMs: 1, size: 10 },
        settingsKey: "settings",
      };
      await cache.write({ ...lookup, diagnostics: [] });
      const files = cacheFiles(directory);
      expect(files).toHaveLength(1);
      expect(path.relative(directory, files[0])).toMatch(/^[0-9a-f]{2}[/\\][0-9a-f]{64}\.cbor$/);
      expect(await cache.read(lookup)).toEqual([]);
    } finally {
      fs.rmSync(directory, { recursive: true, force: true });
    }
  });

  it("uses content hashes as secondary source verification", async () => {
    const directory = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-disk-cache-"));
    try {
      const cache = new DiskAnalysisCache({
        enabled: true,
        directory,
        namespace: "test",
        toolVersion: "1",
      });
      const lookup = {
        source: {
          fileName: "/site/default.asp",
          mtimeMs: 1,
          size: 10,
          contentHash: "same-content",
        },
        settingsKey: "settings",
      };
      await cache.write({
        ...lookup,
        diagnostics: [
          {
            message: "cached",
            range: {
              start: { line: 0, character: 0 },
              end: { line: 0, character: 1 },
            },
          },
        ],
      });
      expect(
        (
          await cache.read({
            ...lookup,
            source: { ...lookup.source, mtimeMs: 2, size: 99 },
          })
        )?.[0]?.message,
      ).toBe("cached");
      expect(
        await cache.read({
          ...lookup,
          source: { ...lookup.source, contentHash: "different-content" },
        }),
      ).toBeUndefined();
    } finally {
      fs.rmSync(directory, { recursive: true, force: true });
    }
  });

  it("drops oversized large-kind entries before decoding", async () => {
    const directory = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-disk-cache-"));
    try {
      const cache = new DiskAnalysisCache({
        enabled: true,
        directory,
        maxSizeMb: 0.01,
        namespace: "test",
        toolVersion: "1",
      });
      const lookup = {
        source: { fileName: "/site/default.asp", mtimeMs: 1, size: 10 },
        settingsKey: "parsed-settings",
      };
      const parsed: AspParsedDocument = {
        uri: "file:///site/default.asp",
        text: "x".repeat(600 * 1024),
        cst: {
          kind: "Document",
          start: 0,
          end: 600 * 1024,
          contentStart: 0,
          contentEnd: 600 * 1024,
          tokens: [],
          children: [],
        },
        regions: [],
        directives: [],
        includes: [],
        serverObjects: [],
        defaultLanguage: "VBScript",
        diagnostics: [],
      };
      const summary: FileAnalysisSummary = {
        uri: parsed.uri,
        fingerprint: "parsed",
        defaultLanguage: "VBScript",
        languageRegions: [],
        includeRefs: [],
        diagnostics: [],
      };
      await cache.writeParsedDocument({ ...lookup, parsed, summary });
      expect(cacheFiles(directory)).toHaveLength(1);
      expect(await cache.readParsedDocument(lookup)).toBeUndefined();
      expect(cacheFiles(directory)).toHaveLength(0);
    } finally {
      fs.rmSync(directory, { recursive: true, force: true });
    }
  });
});

function cacheFiles(directory: string): string[] {
  if (!fs.existsSync(directory)) {
    return [];
  }
  const files: string[] = [];
  for (const entry of fs.readdirSync(directory, { withFileTypes: true })) {
    const entryPath = path.join(directory, entry.name);
    if (entry.isFile() && entry.name.endsWith(".cbor")) {
      files.push(entryPath);
    } else if (entry.isDirectory()) {
      for (const child of fs.readdirSync(entryPath, { withFileTypes: true })) {
        if (child.isFile() && child.name.endsWith(".cbor")) {
          files.push(path.join(entryPath, child.name));
        }
      }
    }
  }
  return files;
}
