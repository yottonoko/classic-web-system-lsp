import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import { clearCacheFiles, isIndexCacheFile, pruneCacheDir } from "../src/cacheManager.js";

const tmpDirs: string[] = [];

function makeTempCacheDir(): string {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-cache-test-"));
  tmpDirs.push(dir);
  return dir;
}

afterEach(() => {
  while (tmpDirs.length > 0) {
    const dir = tmpDirs.pop();
    if (dir) {
      fs.rmSync(dir, { recursive: true, force: true });
    }
  }
});

describe("cacheManager", () => {
  it("recognizes ws_idx_*.dat workspace index cache files", () => {
    expect(isIndexCacheFile("ws_idx_1234abcd.dat")).toBe(true);
    expect(isIndexCacheFile("cache.mdb")).toBe(false);
    expect(isIndexCacheFile(".cache-version")).toBe(false);
  });

  it("clearCacheFiles('index') deletes only ws_idx_*.dat files", () => {
    const dir = makeTempCacheDir();
    fs.writeFileSync(path.join(dir, "ws_idx_111.dat"), "idx1");
    fs.writeFileSync(path.join(dir, "ws_idx_222.dat"), "idx2");
    fs.writeFileSync(path.join(dir, "cache.mdb"), "db");
    fs.writeFileSync(path.join(dir, ".cache-version"), "0.12.10");

    const removed = clearCacheFiles(dir, "index");
    expect(removed).toBe(2);
    expect(fs.existsSync(path.join(dir, "ws_idx_111.dat"))).toBe(false);
    expect(fs.existsSync(path.join(dir, "ws_idx_222.dat"))).toBe(false);
    expect(fs.existsSync(path.join(dir, "cache.mdb"))).toBe(true);
    expect(fs.existsSync(path.join(dir, ".cache-version"))).toBe(true);

    const removedAll = clearCacheFiles(dir, "all");
    expect(removedAll).toBe(2);
    expect(fs.readdirSync(dir)).toEqual([]);
  });

  it("pruneCacheDir invalidates ws_idx_*.dat on extension version change", () => {
    const dir = makeTempCacheDir();
    fs.writeFileSync(path.join(dir, ".cache-version"), "0.12.9");
    fs.writeFileSync(path.join(dir, "ws_idx_abc.dat"), "stale");
    fs.writeFileSync(path.join(dir, "cache.mdb"), "db");

    const res = pruneCacheDir(dir, {
      extensionVersion: "0.12.10",
      maxAgeDays: 14,
      maxSizeMb: 256,
    });
    expect(res.versionCleared).toBe(1);
    expect(fs.existsSync(path.join(dir, "ws_idx_abc.dat"))).toBe(false);
    expect(fs.existsSync(path.join(dir, "cache.mdb"))).toBe(true);
    expect(fs.readFileSync(path.join(dir, ".cache-version"), "utf8")).toBe("0.12.10");
  });

  it("pruneCacheDir removes expired files and prunes oldest when exceeding size cap", () => {
    const dir = makeTempCacheDir();
    fs.writeFileSync(path.join(dir, ".cache-version"), "0.12.10");

    const nowMs = Date.UTC(2026, 10, 27, 12, 0, 0);
    const oldFile = path.join(dir, "ws_idx_old.dat");
    const midFile = path.join(dir, "ws_idx_mid.dat");
    const newFile = path.join(dir, "ws_idx_new.dat");

    fs.writeFileSync(oldFile, Buffer.alloc(1024));
    fs.writeFileSync(midFile, Buffer.alloc(600 * 1024));
    fs.writeFileSync(newFile, Buffer.alloc(600 * 1024));

    const twentyDaysAgo = new Date(nowMs - 20 * 86_400_000);
    const twoDaysAgo = new Date(nowMs - 2 * 86_400_000);
    const oneHourAgo = new Date(nowMs - 3_600_000);
    fs.utimesSync(oldFile, twentyDaysAgo, twentyDaysAgo);
    fs.utimesSync(midFile, twoDaysAgo, twoDaysAgo);
    fs.utimesSync(newFile, oneHourAgo, oneHourAgo);

    const res = pruneCacheDir(dir, {
      extensionVersion: "0.12.10",
      maxAgeDays: 14,
      maxSizeMb: 1,
      nowMs,
    });
    expect(res.versionCleared).toBe(0);
    expect(res.expiredRemoved).toBe(1);
    expect(res.sizePruned).toBe(1);
    expect(fs.existsSync(oldFile)).toBe(false);
    expect(fs.existsSync(midFile)).toBe(false);
    expect(fs.existsSync(newFile)).toBe(true);
  });
});
