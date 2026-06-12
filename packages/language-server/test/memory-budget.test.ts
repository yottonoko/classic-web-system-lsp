import { describe, expect, it } from "vitest";
import {
  MemoryBudgetManager,
  SizedLruCache,
  registeredMapCache,
  type RegisteredCache,
} from "../src/memory-budget";

describe("MemoryBudgetManager", () => {
  it("evicts registered caches by priority until the budget is satisfied", () => {
    const evicted: string[] = [];
    const fast = fakeCache("fast", 10, 80, evicted);
    const later = fakeCache("later", 20, 80, evicted);
    const manager = new MemoryBudgetManager({
      heapStatsProvider: () => ({ heapUsed: 10, heapSizeLimit: 100 }),
      defaultMaxCacheBytes: 100,
    });
    manager.register(later);
    manager.register(fast);

    const result = manager.checkPressure({ reason: "test", maxCacheBytes: 100 });

    expect(result.pressure).toBe("budget");
    expect(result.requestedBytes).toBe(60);
    expect(result.evictedBytes).toBe(80);
    expect(evicted).toEqual(["fast"]);
    expect(result.after.totalEstimatedBytes).toBe(80);
  });

  it("uses heap pressure thresholds and evicts to half budget on high heap usage", () => {
    const evicted: string[] = [];
    const manager = new MemoryBudgetManager({
      heapStatsProvider: () => ({ heapUsed: 80, heapSizeLimit: 100 }),
      defaultMaxCacheBytes: 200,
    });
    manager.register(fakeCache("cache", 10, 180, evicted));

    const result = manager.checkPressure({ reason: "heap" });

    expect(result.pressure).toBe("heap-high");
    expect(result.targetBytes).toBe(100);
    expect(result.requestedBytes).toBe(80);
    expect(result.evictedBytes).toBe(180);
  });

  it("tracks explicit allocation notes in snapshots", () => {
    const manager = new MemoryBudgetManager({
      heapStatsProvider: () => ({ heapUsed: 1, heapSizeLimit: 100 }),
    });
    manager.register(fakeCache("cache", 10, 10, []));
    manager.noteAllocation("cache", 15);
    manager.noteAllocation("cache", -5);

    expect(manager.snapshot().caches.find((cache) => cache.name === "cache")?.estimatedBytes).toBe(
      20,
    );
  });

  it("wraps regular maps as evictable registered caches", () => {
    const map = new Map<string, string>([
      ["a", "aaa"],
      ["b", "bbb"],
    ]);
    const cache = registeredMapCache("map", map, {
      priority: 1,
      estimateEntryBytes: (key, value) => key.length + value.length,
    });

    expect(cache.estimateBytes()).toBe(8);
    expect(cache.evict(4)).toBe(4);
    expect([...map.keys()]).toEqual(["b"]);
  });
});

describe("SizedLruCache", () => {
  it("tracks entry sizes, refreshes LRU order, and disposes evicted entries", () => {
    const disposed: string[] = [];
    const cache = new SizedLruCache<string, string>("sized", {
      priority: 10,
      maxEntries: 2,
      estimateEntryBytes: (_key, value) => value.length,
      disposeEntry: (key) => disposed.push(key),
    });

    cache.set("a", "aaaa");
    cache.set("b", "bb");
    expect(cache.estimateBytes()).toBe(6);
    expect(cache.get("a")).toBe("aaaa");
    cache.set("c", "ccc");

    expect([...cache.keys()]).toEqual(["a", "c"]);
    expect(cache.estimateBytes()).toBe(7);
    expect(disposed).toEqual(["b"]);

    expect(cache.evict(4)).toBe(4);
    expect([...cache.keys()]).toEqual(["c"]);
    expect(disposed).toEqual(["b", "a"]);
  });
});

function fakeCache(
  name: string,
  priority: number,
  bytes: number,
  evicted: string[],
): RegisteredCache {
  let currentBytes = bytes;
  return {
    name,
    priority,
    estimateBytes: () => currentBytes,
    evict: () => {
      const freed = currentBytes;
      currentBytes = 0;
      evicted.push(name);
      return freed;
    },
    entryCount: () => (currentBytes > 0 ? 1 : 0),
  };
}
