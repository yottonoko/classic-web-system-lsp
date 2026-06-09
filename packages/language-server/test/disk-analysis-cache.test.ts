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
      const fileName = path.join(directory, fs.readdirSync(directory)[0]);
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
      expect(fs.readdirSync(directory)).toHaveLength(0);
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
      const beforeSweep = fs.readdirSync(directory).length;
      await cache.sweep();
      expect(beforeSweep).toBeGreaterThan(0);
      expect(fs.readdirSync(directory).length).toBeLessThan(beforeSweep);
      await cache.clear();
      expect(fs.existsSync(directory)).toBe(false);
    } finally {
      fs.rmSync(directory, { recursive: true, force: true });
    }
  });
});
