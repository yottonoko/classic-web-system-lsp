import * as fs from "node:fs";
import * as path from "node:path";

const CACHE_VERSION_FILE = ".cache-version";
const MS_PER_DAY = 86_400_000;
const BYTES_PER_MB = 1024 * 1024;

export interface PruneCacheOptions {
  extensionVersion: string;
  maxAgeDays: number;
  maxSizeMb: number;
  nowMs?: number;
}

export interface PruneCacheResult {
  versionCleared: number;
  expiredRemoved: number;
  sizePruned: number;
}

export function isIndexCacheFile(name: string): boolean {
  return name.startsWith("ws_idx_") && name.endsWith(".dat");
}

function isManagedCacheFile(name: string): boolean {
  return isIndexCacheFile(name) || name.endsWith(".mdb") || name.includes(".mdb-");
}

/**
 * Deletes cached files inside `cacheDir`.
 * - `"all"`: removes every entry inside `cacheDir`.
 * - `"index"`: removes only workspace index cache files (`ws_idx_*.dat`).
 */
export function clearCacheFiles(cacheDir: string, mode: "all" | "index"): number {
  if (!fs.existsSync(cacheDir)) {
    return 0;
  }
  let removed = 0;
  for (const entry of fs.readdirSync(cacheDir)) {
    if (mode === "index" && !isIndexCacheFile(entry)) {
      continue;
    }
    fs.rmSync(path.join(cacheDir, entry), { recursive: true, force: true });
    removed += 1;
  }
  return removed;
}

/**
 * Automatically invalidates stale `ws_idx_*.dat` files on extension version
 * changes, removes entries older than `maxAgeDays`, and evicts oldest cache
 * files when total size exceeds `maxSizeMb`.
 */
export function pruneCacheDir(cacheDir: string, opts: PruneCacheOptions): PruneCacheResult {
  const result: PruneCacheResult = {
    versionCleared: 0,
    expiredRemoved: 0,
    sizePruned: 0,
  };
  if (!fs.existsSync(cacheDir)) {
    return result;
  }

  const versionPath = path.join(cacheDir, CACHE_VERSION_FILE);
  let storedVersion = "";
  try {
    storedVersion = fs.readFileSync(versionPath, "utf8").trim();
  } catch {
    storedVersion = "";
  }

  if (storedVersion !== opts.extensionVersion) {
    result.versionCleared = clearCacheFiles(cacheDir, "index");
    try {
      fs.writeFileSync(versionPath, opts.extensionVersion, "utf8");
    } catch {
      // Ignore write failures if storage is read-only.
    }
  }

  const nowMs = opts.nowMs ?? Date.now();
  const maxAgeMs = opts.maxAgeDays > 0 ? opts.maxAgeDays * MS_PER_DAY : 0;
  const maxBytes = opts.maxSizeMb > 0 ? opts.maxSizeMb * BYTES_PER_MB : 0;

  const survivors: Array<{ fullPath: string; mtimeMs: number; size: number }> = [];
  for (const entry of fs.readdirSync(cacheDir)) {
    if (!isManagedCacheFile(entry)) {
      continue;
    }
    const fullPath = path.join(cacheDir, entry);
    let stat: fs.Stats;
    try {
      stat = fs.statSync(fullPath);
    } catch {
      continue;
    }
    if (!stat.isFile()) {
      continue;
    }
    if (maxAgeMs > 0 && nowMs - stat.mtimeMs > maxAgeMs) {
      fs.rmSync(fullPath, { force: true });
      result.expiredRemoved += 1;
      continue;
    }
    survivors.push({ fullPath, mtimeMs: stat.mtimeMs, size: stat.size });
  }

  if (maxBytes > 0) {
    let totalBytes = survivors.reduce((sum, f) => sum + f.size, 0);
    if (totalBytes > maxBytes) {
      survivors.sort((a, b) => a.mtimeMs - b.mtimeMs);
      for (const file of survivors) {
        if (totalBytes <= maxBytes) {
          break;
        }
        fs.rmSync(file.fullPath, { force: true });
        totalBytes -= file.size;
        result.sizePruned += 1;
      }
    }
  }

  return result;
}
