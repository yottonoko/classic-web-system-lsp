import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { describe, expect, it } from "vitest";
import { DiskAnalysisCache } from "../src/disk-analysis-cache";

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
