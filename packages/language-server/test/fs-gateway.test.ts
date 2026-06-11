import path from "node:path";
import { describe, expect, it } from "vitest";
import { FsGateway, type FsGatewayBackend, type FsGatewayDirent } from "../src/fs-gateway";

class FakeStats {
  constructor(
    readonly kind: "file" | "directory",
    readonly size = 0,
    readonly mtimeMs = 1,
  ) {}

  isFile(): boolean {
    return this.kind === "file";
  }

  isDirectory(): boolean {
    return this.kind === "directory";
  }
}

class FakeDirent implements FsGatewayDirent {
  constructor(
    readonly name: string,
    private readonly kind: "file" | "directory",
  ) {}

  isFile(): boolean {
    return this.kind === "file";
  }

  isDirectory(): boolean {
    return this.kind === "directory";
  }
}

class FakeBackend implements FsGatewayBackend {
  readonly statCalls = new Map<string, number>();
  readonly readdirCalls = new Map<string, number>();
  readonly stats = new Map<string, FakeStats>();
  readonly directories = new Map<string, FakeDirent[]>();
  statGate?: Promise<void>;

  async stat(fileName: string): Promise<FakeStats> {
    this.statCalls.set(fileName, (this.statCalls.get(fileName) ?? 0) + 1);
    await this.statGate;
    const stat = this.stats.get(fileName);
    if (!stat) {
      throw new Error(`missing ${fileName}`);
    }
    return stat;
  }

  async readdir(directory: string): Promise<FakeDirent[]> {
    this.readdirCalls.set(directory, (this.readdirCalls.get(directory) ?? 0) + 1);
    const entries = this.directories.get(directory);
    if (!entries) {
      throw new Error(`missing ${directory}`);
    }
    return entries;
  }
}

describe("FsGateway", () => {
  it("caches positive stats until invalidated", async () => {
    const backend = new FakeBackend();
    const fileName = path.resolve("/site/default.asp");
    backend.stats.set(fileName, new FakeStats("file", 10));
    const gateway = new FsGateway();
    gateway.configure({ statTtlMs: 30_000 }, backend);

    expect((await gateway.statAsync(fileName))?.size).toBe(10);
    expect((await gateway.statAsync(fileName))?.size).toBe(10);
    expect(backend.statCalls.get(fileName)).toBe(1);

    gateway.invalidatePath(fileName);
    expect((await gateway.statAsync(fileName))?.size).toBe(10);
    expect(backend.statCalls.get(fileName)).toBe(2);
  });

  it("caches negative stats with the negative ttl", async () => {
    const backend = new FakeBackend();
    const fileName = path.resolve("/site/missing.inc");
    const gateway = new FsGateway();
    gateway.configure({ statTtlMs: 30_000, negativeStatTtlMs: 30_000 }, backend);

    expect(await gateway.statAsync(fileName)).toBeUndefined();
    expect(await gateway.statAsync(fileName)).toBeUndefined();
    expect(backend.statCalls.get(fileName)).toBe(1);
  });

  it("deduplicates in-flight stat calls", async () => {
    const backend = new FakeBackend();
    const fileName = path.resolve("/site/default.asp");
    backend.stats.set(fileName, new FakeStats("file", 10));
    let release: (() => void) | undefined;
    backend.statGate = new Promise((resolve) => {
      release = resolve;
    });
    const gateway = new FsGateway();
    gateway.configure({ statTtlMs: 30_000 }, backend);

    const left = gateway.statAsync(fileName);
    const right = gateway.statAsync(fileName);
    release?.();
    expect((await left)?.size).toBe(10);
    expect((await right)?.size).toBe(10);
    expect(backend.statCalls.get(fileName)).toBe(1);
  });

  it("caches directory listings with lowercase lookup maps", async () => {
    const backend = new FakeBackend();
    const directory = path.resolve("/site");
    backend.directories.set(directory, [
      new FakeDirent("Shared.inc", "file"),
      new FakeDirent("default.asp", "file"),
    ]);
    const gateway = new FsGateway();
    gateway.configure({ readdirTtlMs: 30_000 }, backend);

    const listing = await gateway.readdirAsync(directory);
    expect(listing?.byLowerName.get("shared.inc")?.[0]?.name).toBe("Shared.inc");
    expect((await gateway.readdirAsync(directory))?.entries).toHaveLength(2);
    expect(backend.readdirCalls.get(directory)).toBe(1);

    gateway.invalidatePath(path.join(directory, "default.asp"));
    expect((await gateway.readdirAsync(directory))?.entries).toHaveLength(2);
    expect(backend.readdirCalls.get(directory)).toBe(2);
  });

  it("bypasses caches when ttl is zero", async () => {
    const backend = new FakeBackend();
    const fileName = path.resolve("/site/default.asp");
    const directory = path.dirname(fileName);
    backend.stats.set(fileName, new FakeStats("file", 10));
    backend.directories.set(directory, [new FakeDirent("default.asp", "file")]);
    const gateway = new FsGateway();
    gateway.configure({ statTtlMs: 0, readdirTtlMs: 0 }, backend);

    await gateway.statAsync(fileName);
    await gateway.statAsync(fileName);
    await gateway.readdirAsync(directory);
    await gateway.readdirAsync(directory);

    expect(backend.statCalls.get(fileName)).toBe(2);
    expect(backend.readdirCalls.get(directory)).toBe(2);
  });

  it("evicts least recently used stat and directory entries", async () => {
    const backend = new FakeBackend();
    const first = path.resolve("/site/first.asp");
    const second = path.resolve("/site/second.asp");
    const firstDir = path.resolve("/site/a");
    const secondDir = path.resolve("/site/b");
    backend.stats.set(first, new FakeStats("file", 1));
    backend.stats.set(second, new FakeStats("file", 2));
    backend.directories.set(firstDir, [new FakeDirent("first.asp", "file")]);
    backend.directories.set(secondDir, [new FakeDirent("second.asp", "file")]);
    const gateway = new FsGateway();
    gateway.configure(
      {
        statTtlMs: 30_000,
        readdirTtlMs: 30_000,
        statMaxEntries: 1,
        readdirMaxEntries: 1,
      },
      backend,
    );

    await gateway.statAsync(first);
    await gateway.statAsync(second);
    await gateway.statAsync(first);
    await gateway.readdirAsync(firstDir);
    await gateway.readdirAsync(secondDir);
    await gateway.readdirAsync(firstDir);

    expect(backend.statCalls.get(first)).toBe(2);
    expect(backend.statCalls.get(second)).toBe(1);
    expect(backend.readdirCalls.get(firstDir)).toBe(2);
    expect(backend.readdirCalls.get(secondDir)).toBe(1);
  });
});
