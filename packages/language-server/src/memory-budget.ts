import { getHeapStatistics } from "node:v8";

export const defaultMemoryMaxCacheBytes = 512 * 1024 * 1024;
const defaultHeapHighRatio = 0.75;
const defaultHeapCriticalRatio = 0.9;
const defaultHeapHighTargetRatio = 0.5;

export interface RegisteredCache {
  name: string;
  priority: number;
  estimateBytes(): number;
  evict(targetBytes: number): number;
  entryCount?(): number;
}

export interface MemoryCacheSnapshot {
  name: string;
  priority: number;
  estimatedBytes: number;
  entries?: number;
}

export interface MemorySnapshot {
  totalEstimatedBytes: number;
  heapUsed: number;
  heapSizeLimit: number;
  heapUsedRatio: number;
  maxCacheBytes: number;
  caches: MemoryCacheSnapshot[];
}

export type MemoryPressureKind = "none" | "budget" | "heap-high" | "heap-critical";

export interface MemoryEvictionRecord {
  name: string;
  priority: number;
  requestedBytes: number;
  evictedBytes: number;
  beforeBytes: number;
  afterBytes: number;
  beforeEntries?: number;
  afterEntries?: number;
}

export interface MemoryPressureResult {
  reason: string;
  pressure: MemoryPressureKind;
  targetBytes: number;
  requestedBytes: number;
  evictedBytes: number;
  before: MemorySnapshot;
  after: MemorySnapshot;
  evictions: MemoryEvictionRecord[];
}

export interface MemoryBudgetCheckOptions {
  reason?: string;
  maxCacheBytes?: number;
}

export interface MemoryBudgetManagerOptions {
  heapStatsProvider?: () => { heapUsed: number; heapSizeLimit: number };
  heapHighRatio?: number;
  heapCriticalRatio?: number;
  heapHighTargetRatio?: number;
  defaultMaxCacheBytes?: number;
}

export class MemoryBudgetManager {
  private readonly caches = new Map<string, RegisteredCache>();
  private readonly allocationAdjustments = new Map<string, number>();
  private readonly heapStatsProvider: () => { heapUsed: number; heapSizeLimit: number };
  private readonly heapHighRatio: number;
  private readonly heapCriticalRatio: number;
  private readonly heapHighTargetRatio: number;
  private readonly defaultMaxCacheBytes: number;
  private lastEviction: MemoryPressureResult | undefined;

  constructor(options: MemoryBudgetManagerOptions = {}) {
    this.heapStatsProvider =
      options.heapStatsProvider ??
      (() => ({
        heapUsed: process.memoryUsage().heapUsed,
        heapSizeLimit: getHeapStatistics().heap_size_limit,
      }));
    this.heapHighRatio = options.heapHighRatio ?? defaultHeapHighRatio;
    this.heapCriticalRatio = options.heapCriticalRatio ?? defaultHeapCriticalRatio;
    this.heapHighTargetRatio = options.heapHighTargetRatio ?? defaultHeapHighTargetRatio;
    this.defaultMaxCacheBytes = options.defaultMaxCacheBytes ?? defaultMemoryMaxCacheBytes;
  }

  register(cache: RegisteredCache): void {
    this.caches.set(cache.name, cache);
  }

  unregister(name: string): void {
    this.caches.delete(name);
    this.allocationAdjustments.delete(name);
  }

  noteAllocation(name: string, deltaBytes: number): void {
    if (!Number.isFinite(deltaBytes) || deltaBytes === 0) {
      return;
    }
    const next = (this.allocationAdjustments.get(name) ?? 0) + Math.trunc(deltaBytes);
    if (next === 0) {
      this.allocationAdjustments.delete(name);
    } else {
      this.allocationAdjustments.set(name, next);
    }
  }

  snapshot(options: MemoryBudgetCheckOptions = {}): MemorySnapshot {
    const maxCacheBytes = positiveBytes(options.maxCacheBytes, this.defaultMaxCacheBytes);
    const heap = this.heapStatsProvider();
    const caches = [...this.caches.values()]
      .map((cache) => this.cacheSnapshot(cache))
      .sort((left, right) => left.priority - right.priority || left.name.localeCompare(right.name));
    const totalEstimatedBytes = caches.reduce((sum, cache) => sum + cache.estimatedBytes, 0);
    const heapSizeLimit = Math.max(0, heap.heapSizeLimit);
    const heapUsed = Math.max(0, heap.heapUsed);
    return {
      totalEstimatedBytes,
      heapUsed,
      heapSizeLimit,
      heapUsedRatio: heapSizeLimit > 0 ? heapUsed / heapSizeLimit : 0,
      maxCacheBytes,
      caches,
    };
  }

  checkPressure(options: MemoryBudgetCheckOptions = {}): MemoryPressureResult {
    const reason = options.reason ?? "manual";
    const before = this.snapshot(options);
    const { pressure, targetBytes } = this.pressureTarget(before);
    const requestedBytes = Math.max(0, before.totalEstimatedBytes - targetBytes);
    const evictions: MemoryEvictionRecord[] = [];
    let remainingBytes = requestedBytes;
    let evictedBytes = 0;

    if (remainingBytes > 0) {
      const caches = [...this.caches.values()].sort(
        (left, right) => left.priority - right.priority || left.name.localeCompare(right.name),
      );
      for (const cache of caches) {
        if (remainingBytes <= 0) {
          break;
        }
        const beforeBytes = this.cacheEstimatedBytes(cache);
        const beforeEntries = cache.entryCount?.();
        if (beforeBytes <= 0 && beforeEntries === 0) {
          continue;
        }
        const freed = Math.max(0, safeNumber(cache.evict(remainingBytes)));
        if (freed <= 0) {
          continue;
        }
        const afterBytes = this.cacheEstimatedBytes(cache);
        const afterEntries = cache.entryCount?.();
        evictions.push({
          name: cache.name,
          priority: cache.priority,
          requestedBytes: remainingBytes,
          evictedBytes: freed,
          beforeBytes,
          afterBytes,
          beforeEntries,
          afterEntries,
        });
        remainingBytes = Math.max(0, remainingBytes - freed);
        evictedBytes += freed;
      }
    }

    const after = this.snapshot(options);
    const result: MemoryPressureResult = {
      reason,
      pressure,
      targetBytes,
      requestedBytes,
      evictedBytes,
      before,
      after,
      evictions,
    };
    if (evictedBytes > 0) {
      this.lastEviction = result;
    }
    return result;
  }

  lastEvictionResult(): MemoryPressureResult | undefined {
    return this.lastEviction;
  }

  private pressureTarget(snapshot: MemorySnapshot): {
    pressure: MemoryPressureKind;
    targetBytes: number;
  } {
    if (snapshot.heapUsedRatio >= this.heapCriticalRatio) {
      return { pressure: "heap-critical", targetBytes: 0 };
    }
    if (snapshot.heapUsedRatio >= this.heapHighRatio) {
      return {
        pressure: "heap-high",
        targetBytes: Math.floor(snapshot.maxCacheBytes * this.heapHighTargetRatio),
      };
    }
    if (snapshot.totalEstimatedBytes > snapshot.maxCacheBytes) {
      return { pressure: "budget", targetBytes: snapshot.maxCacheBytes };
    }
    return { pressure: "none", targetBytes: snapshot.maxCacheBytes };
  }

  private cacheSnapshot(cache: RegisteredCache): MemoryCacheSnapshot {
    const estimatedBytes = this.cacheEstimatedBytes(cache);
    const entries = cache.entryCount?.();
    return {
      name: cache.name,
      priority: cache.priority,
      estimatedBytes,
      entries,
    };
  }

  private cacheEstimatedBytes(cache: RegisteredCache): number {
    const baseBytes = safeNumber(cache.estimateBytes());
    const adjustedBytes = baseBytes + (this.allocationAdjustments.get(cache.name) ?? 0);
    return Math.max(0, adjustedBytes);
  }
}

export interface SizedLruCacheOptions<K, V> {
  priority: number;
  maxEntries?: number;
  estimateEntryBytes?: (key: K, value: V) => number;
  disposeEntry?: (key: K, value: V) => void;
}

export class SizedLruCache<K, V> implements RegisteredCache, Iterable<[K, V]> {
  private readonly items = new Map<K, { value: V; bytes: number }>();
  private estimatedBytes = 0;
  readonly priority: number;
  private readonly maxEntries: number | undefined;
  private readonly estimateEntryBytesFn: (key: K, value: V) => number;
  private readonly disposeEntry: ((key: K, value: V) => void) | undefined;

  constructor(
    readonly name: string,
    options: SizedLruCacheOptions<K, V>,
  ) {
    this.priority = options.priority;
    this.maxEntries = options.maxEntries;
    this.estimateEntryBytesFn =
      options.estimateEntryBytes ??
      ((key, value) => estimateJsonBytes(key, 128) + estimateJsonBytes(value, 512) + 64);
    this.disposeEntry = options.disposeEntry;
  }

  get size(): number {
    return this.items.size;
  }

  get(key: K): V | undefined {
    const entry = this.items.get(key);
    if (!entry) {
      return undefined;
    }
    this.items.delete(key);
    this.items.set(key, entry);
    return entry.value;
  }

  has(key: K): boolean {
    return this.items.has(key);
  }

  set(key: K, value: V): this {
    this.delete(key);
    const bytes = Math.max(0, safeNumber(this.estimateEntryBytesFn(key, value)));
    this.items.set(key, { value, bytes });
    this.estimatedBytes += bytes;
    this.pruneToMaxEntries();
    return this;
  }

  delete(key: K): boolean {
    return this.deleteEntry(key) !== undefined;
  }

  clear(): void {
    for (const [key, entry] of this.items) {
      this.disposeEntry?.(key, entry.value);
    }
    this.items.clear();
    this.estimatedBytes = 0;
  }

  *keys(): IterableIterator<K> {
    for (const key of this.items.keys()) {
      yield key;
    }
  }

  *values(): IterableIterator<V> {
    for (const entry of this.items.values()) {
      yield entry.value;
    }
  }

  *entries(): IterableIterator<[K, V]> {
    for (const [key, entry] of this.items) {
      yield [key, entry.value];
    }
  }

  [Symbol.iterator](): IterableIterator<[K, V]> {
    return this.entries();
  }

  forEach(callback: (value: V, key: K, cache: this) => void, thisArg?: unknown): void {
    for (const [key, value] of this.entries()) {
      callback.call(thisArg, value, key, this);
    }
  }

  estimateBytes(): number {
    return this.estimatedBytes;
  }

  entryCount(): number {
    return this.items.size;
  }

  evict(targetBytes: number): number {
    let freed = 0;
    while (this.items.size > 0 && freed < targetBytes) {
      const oldestKey = this.items.keys().next().value;
      if (oldestKey === undefined) {
        break;
      }
      freed += this.deleteEntry(oldestKey) ?? 0;
    }
    return freed;
  }

  pruneToMaxEntries(maxEntries = this.maxEntries): number {
    if (maxEntries === undefined) {
      return 0;
    }
    let freed = 0;
    while (this.items.size > maxEntries) {
      const oldestKey = this.items.keys().next().value;
      if (oldestKey === undefined) {
        break;
      }
      freed += this.deleteEntry(oldestKey) ?? 0;
    }
    return freed;
  }

  private deleteEntry(key: K): number | undefined {
    const entry = this.items.get(key);
    if (!entry) {
      return undefined;
    }
    this.items.delete(key);
    this.estimatedBytes = Math.max(0, this.estimatedBytes - entry.bytes);
    this.disposeEntry?.(key, entry.value);
    return entry.bytes;
  }
}

export interface RegisteredMapCacheOptions<K, V> {
  priority: number;
  bytesPerEntry?: number;
  estimateEntryBytes?: (key: K, value: V) => number;
  disposeEntry?: (key: K, value: V) => void;
}

export function registeredMapCache<K, V>(
  name: string,
  map: Map<K, V>,
  options: RegisteredMapCacheOptions<K, V>,
): RegisteredCache {
  const bytesPerEntry = options.bytesPerEntry ?? 1024;
  const estimateEntryBytes = options.estimateEntryBytes;
  const entryBytes = (key: K, value: V): number =>
    Math.max(0, safeNumber(estimateEntryBytes ? estimateEntryBytes(key, value) : bytesPerEntry));
  return {
    name,
    priority: options.priority,
    estimateBytes: () => {
      if (!estimateEntryBytes) {
        return map.size * bytesPerEntry;
      }
      let total = 0;
      for (const [key, value] of map) {
        total += entryBytes(key, value);
      }
      return total;
    },
    evict: (targetBytes) => {
      let freed = 0;
      while (map.size > 0 && freed < targetBytes) {
        const oldestKey = map.keys().next().value;
        if (oldestKey === undefined) {
          break;
        }
        const value = map.get(oldestKey);
        if (value === undefined && !map.has(oldestKey)) {
          break;
        }
        const bytes = entryBytes(oldestKey, value as V);
        map.delete(oldestKey);
        options.disposeEntry?.(oldestKey, value as V);
        freed += bytes;
      }
      return freed;
    },
    entryCount: () => map.size,
  };
}

export function estimateStringBytes(value: string | undefined): number {
  return value === undefined ? 0 : value.length * 2 + 40;
}

export function estimateStringArrayBytes(values: readonly string[] | undefined): number {
  if (!values) {
    return 0;
  }
  return 32 + values.reduce((sum, value) => sum + estimateStringBytes(value), 0);
}

export function estimateJsonBytes(value: unknown, fallbackBytes = 1024): number {
  try {
    const seen = new WeakSet<object>();
    const json = JSON.stringify(value, (_key, item) => {
      if (typeof item === "function") {
        return undefined;
      }
      if (typeof item === "bigint") {
        return item.toString();
      }
      if (item && typeof item === "object") {
        if (seen.has(item)) {
          return "[Circular]";
        }
        seen.add(item);
      }
      return item;
    });
    return json ? estimateStringBytes(json) : fallbackBytes;
  } catch {
    return fallbackBytes;
  }
}

function positiveBytes(value: number | undefined, fallback: number): number {
  return value !== undefined && Number.isFinite(value) && value > 0 ? Math.floor(value) : fallback;
}

function safeNumber(value: number): number {
  return Number.isFinite(value) ? Math.trunc(value) : 0;
}
