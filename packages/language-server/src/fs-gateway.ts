import fs from "node:fs";
import path from "node:path";

export interface FsGatewayBackend {
  stat(fileName: string): Promise<FsGatewayStats>;
  readdir(directory: string, options: { withFileTypes: true }): Promise<FsGatewayDirent[]>;
}

export interface FsGatewayStats {
  mtimeMs: number;
  size: number;
  isFile(): boolean;
  isDirectory(): boolean;
}

export interface FsGatewayDirent {
  name: string;
  isFile(): boolean;
  isDirectory(): boolean;
}

export interface FsGatewayOptions {
  statTtlMs?: number;
  negativeStatTtlMs?: number;
  readdirTtlMs?: number;
  statMaxEntries?: number;
  readdirMaxEntries?: number;
}

export interface FsGatewayDirectoryListing {
  entries: FsGatewayDirent[];
  byLowerName: Map<string, FsGatewayDirent[]>;
}

interface CacheEntry<T> {
  value: T;
  expiresAt: number;
}

const defaultStatMaxEntries = 20_000;
const defaultReaddirMaxEntries = 4_000;

export class FsGateway {
  private backend: FsGatewayBackend = fs.promises;
  private options: Required<FsGatewayOptions> = {
    statTtlMs: 0,
    negativeStatTtlMs: 0,
    readdirTtlMs: 0,
    statMaxEntries: defaultStatMaxEntries,
    readdirMaxEntries: defaultReaddirMaxEntries,
  };
  private readonly statCache = new Map<string, CacheEntry<FsGatewayStats | undefined>>();
  private readonly statInFlight = new Map<string, Promise<FsGatewayStats | undefined>>();
  private readonly readdirCache = new Map<
    string,
    CacheEntry<FsGatewayDirectoryListing | undefined>
  >();
  private readonly readdirInFlight = new Map<
    string,
    Promise<FsGatewayDirectoryListing | undefined>
  >();
  private currentGeneration = 0;

  configure(options: FsGatewayOptions, backend?: FsGatewayBackend): void {
    const previous = JSON.stringify(this.options);
    this.options = {
      statTtlMs: nonNegativeInteger(options.statTtlMs),
      negativeStatTtlMs: nonNegativeInteger(options.negativeStatTtlMs),
      readdirTtlMs: nonNegativeInteger(options.readdirTtlMs),
      statMaxEntries: positiveInteger(options.statMaxEntries, defaultStatMaxEntries),
      readdirMaxEntries: positiveInteger(options.readdirMaxEntries, defaultReaddirMaxEntries),
    };
    if (backend || previous !== JSON.stringify(this.options)) {
      if (backend) {
        this.backend = backend;
      }
      this.invalidateAll();
    }
  }

  setBackendForTest(backend: FsGatewayBackend): void {
    this.backend = backend;
    this.invalidateAll();
  }

  get generation(): number {
    return this.currentGeneration;
  }

  async statAsync(fileName: string): Promise<FsGatewayStats | undefined> {
    const key = cacheKey(fileName);
    if (this.options.statTtlMs <= 0 && this.options.negativeStatTtlMs <= 0) {
      return this.readStatUncached(key);
    }
    const cached = this.getValid(this.statCache, key);
    if (cached.hit) {
      return cached.value;
    }
    const existing = this.statInFlight.get(key);
    if (existing) {
      return existing;
    }
    const promise = this.readStatUncached(key).then((value) => {
      const ttl = value ? this.options.statTtlMs : this.options.negativeStatTtlMs;
      if (ttl > 0) {
        this.setLru(
          this.statCache,
          key,
          { value, expiresAt: Date.now() + ttl },
          this.options.statMaxEntries,
        );
      }
      return value;
    });
    this.statInFlight.set(key, promise);
    try {
      return await promise;
    } finally {
      if (this.statInFlight.get(key) === promise) {
        this.statInFlight.delete(key);
      }
    }
  }

  async readdirAsync(directory: string): Promise<FsGatewayDirectoryListing | undefined> {
    const key = cacheKey(directory);
    if (this.options.readdirTtlMs <= 0) {
      return this.readDirectoryUncached(key);
    }
    const cached = this.getValid(this.readdirCache, key);
    if (cached.hit) {
      return cached.value;
    }
    const existing = this.readdirInFlight.get(key);
    if (existing) {
      return existing;
    }
    const promise = this.readDirectoryUncached(key).then((value) => {
      this.setLru(
        this.readdirCache,
        key,
        { value, expiresAt: Date.now() + this.options.readdirTtlMs },
        this.options.readdirMaxEntries,
      );
      return value;
    });
    this.readdirInFlight.set(key, promise);
    try {
      return await promise;
    } finally {
      if (this.readdirInFlight.get(key) === promise) {
        this.readdirInFlight.delete(key);
      }
    }
  }

  invalidatePath(fileName: string): void {
    const key = cacheKey(fileName);
    this.statCache.delete(key);
    this.statInFlight.delete(key);
    this.readdirCache.delete(key);
    this.readdirInFlight.delete(key);
    const parent = cacheKey(path.dirname(key));
    this.readdirCache.delete(parent);
    this.readdirInFlight.delete(parent);
    this.currentGeneration += 1;
  }

  invalidateAll(): void {
    this.statCache.clear();
    this.statInFlight.clear();
    this.readdirCache.clear();
    this.readdirInFlight.clear();
    this.currentGeneration += 1;
  }

  private async readStatUncached(fileName: string): Promise<FsGatewayStats | undefined> {
    return this.backend.stat(fileName).catch(() => undefined);
  }

  private async readDirectoryUncached(
    directory: string,
  ): Promise<FsGatewayDirectoryListing | undefined> {
    const entries = await this.backend
      .readdir(directory, { withFileTypes: true })
      .catch(() => undefined);
    if (!entries) {
      return undefined;
    }
    const byLowerName = new Map<string, FsGatewayDirent[]>();
    for (const entry of entries) {
      const lower = entry.name.toLowerCase();
      const existing = byLowerName.get(lower);
      if (existing) {
        existing.push(entry);
      } else {
        byLowerName.set(lower, [entry]);
      }
    }
    return { entries, byLowerName };
  }

  private getValid<T>(
    cache: Map<string, CacheEntry<T>>,
    key: string,
  ): { hit: true; value: T } | { hit: false } {
    const entry = cache.get(key);
    if (!entry) {
      return { hit: false };
    }
    if (Date.now() > entry.expiresAt) {
      cache.delete(key);
      return { hit: false };
    }
    cache.delete(key);
    cache.set(key, entry);
    return { hit: true, value: entry.value };
  }

  private setLru<T>(
    cache: Map<string, CacheEntry<T>>,
    key: string,
    entry: CacheEntry<T>,
    maxEntries: number,
  ): void {
    cache.delete(key);
    cache.set(key, entry);
    while (cache.size > maxEntries) {
      const oldest = cache.keys().next().value;
      if (typeof oldest !== "string") {
        break;
      }
      cache.delete(oldest);
    }
  }
}

export const fsGateway = new FsGateway();

function cacheKey(fileName: string): string {
  return path.resolve(fileName);
}

function nonNegativeInteger(value: number | undefined): number {
  return typeof value === "number" && Number.isFinite(value) && value > 0 ? Math.floor(value) : 0;
}

function positiveInteger(value: number | undefined, fallback: number): number {
  return typeof value === "number" && Number.isFinite(value) && value > 0
    ? Math.floor(value)
    : fallback;
}
