import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { pathToFileURL } from "node:url";
import { parseAspDocument } from "@asp-lsp/core";
import { describe, expect, it } from "vitest";
import {
  DiskAnalysisCache,
  diskAnalysisCacheFormatVersion,
  type DiskAnalysisCachePayload,
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
      };

      await cache.write(payload);
      const files = collectFiles(cache.root);
      expect(files.filter((fileName) => fileName.endsWith(".cbor"))).toHaveLength(1);
      expect(files.some((fileName) => fileName.endsWith(".json"))).toBe(false);
      expect(await cache.readFresh(source, "settings")).toMatchObject({
        settingsKey: "settings",
        source: { fileName: sourceFile },
      });

      expect(
        await cache.readFresh({ ...source, size: source.size + 1 }, "settings"),
      ).toBeUndefined();

      fs.writeFileSync(files.find((fileName) => fileName.endsWith(".cbor")) ?? "", "broken");
      expect(await cache.readFresh(source, "settings")).toBeUndefined();
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
