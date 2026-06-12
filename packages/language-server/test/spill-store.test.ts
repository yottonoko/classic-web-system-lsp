import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { describe, expect, it } from "vitest";
import { SpillStore } from "../src/asp-graph/spill-store";

describe("SpillStore", () => {
  it("appends length-prefixed CBOR records and reads them by offset", async () => {
    const directory = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-spill-store-"));
    try {
      const store = new SpillStore({ directory });
      const first = await store.writeRecord("decls", { name: "first" });
      const second = await store.writeRecord("decls", { name: "second", count: 2 });

      expect(first.fileName).toBe(second.fileName);
      expect(second.offset).toBeGreaterThan(first.offset);
      await expect(store.readRecord(first)).resolves.toEqual({ name: "first" });
      await expect(store.readRecord(second)).resolves.toEqual({ name: "second", count: 2 });

      await store.clear();
      expect(fs.existsSync(directory)).toBe(false);
    } finally {
      fs.rmSync(directory, { recursive: true, force: true });
    }
  });

  it("sweeps stale bulk spill roots", async () => {
    const directory = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-spill-sweep-"));
    try {
      const stale = path.join(directory, "asp-lsp-bulk-1-stale");
      const fresh = path.join(directory, "asp-lsp-bulk-1-fresh");
      fs.mkdirSync(stale);
      fs.mkdirSync(fresh);
      const old = new Date(Date.now() - 48 * 60 * 60 * 1000);
      fs.utimesSync(stale, old, old);

      await expect(SpillStore.sweepStaleTemporaryRoots({ directory, ttlHours: 1 })).resolves.toBe(
        1,
      );
      expect(fs.existsSync(stale)).toBe(false);
      expect(fs.existsSync(fresh)).toBe(true);
    } finally {
      fs.rmSync(directory, { recursive: true, force: true });
    }
  });
});
