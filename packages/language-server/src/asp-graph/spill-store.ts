import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { decode, encode } from "cbor-x";

const recordLengthBytes = 4;
const spillRootPrefix = "asp-lsp-bulk-";

export interface SpillStoreOptions {
  directory?: string;
  namespace?: string;
  ttlHours?: number;
}

export interface SpillRecordRef {
  kind: string;
  fileName: string;
  offset: number;
  bytes: number;
}

export class SpillStore {
  private readonly root: string;
  private readonly ttlMs: number;
  private readonly writeQueues = new Map<string, Promise<void>>();

  constructor(options: SpillStoreOptions = {}) {
    const namespace = safePathSegment(options.namespace ?? "default");
    this.root =
      options.directory && options.directory.trim().length > 0
        ? path.resolve(options.directory)
        : path.join(os.tmpdir(), `${spillRootPrefix}${process.pid}-${namespace}`);
    this.ttlMs = Math.max(0.000001, options.ttlHours ?? 24) * 60 * 60 * 1000;
  }

  static async sweepStaleTemporaryRoots(
    options: {
      directory?: string;
      ttlHours?: number;
    } = {},
  ): Promise<number> {
    const root = options.directory ? path.resolve(options.directory) : os.tmpdir();
    const cutoff = Date.now() - Math.max(0.000001, options.ttlHours ?? 24) * 60 * 60 * 1000;
    let removed = 0;
    const entries = await fs.promises.readdir(root, { withFileTypes: true }).catch(() => []);
    for (const entry of entries) {
      if (!entry.isDirectory() || !entry.name.startsWith(spillRootPrefix)) {
        continue;
      }
      const fileName = path.join(root, entry.name);
      const stat = await fs.promises.stat(fileName).catch(() => undefined);
      if (stat && stat.mtimeMs <= cutoff) {
        await fs.promises.rm(fileName, { recursive: true, force: true });
        removed += 1;
      }
    }
    return removed;
  }

  get directory(): string {
    return this.root;
  }

  async writeRecord(kind: string, value: unknown): Promise<SpillRecordRef> {
    const safeKind = safePathSegment(kind);
    const payload = encode(value);
    if (payload.byteLength > 0xffffffff) {
      throw new Error(`Spill record is too large: ${payload.byteLength} bytes.`);
    }
    const header = Buffer.allocUnsafe(recordLengthBytes);
    header.writeUInt32BE(payload.byteLength, 0);
    const record = Buffer.concat([header, Buffer.from(payload)]);
    const fileName = path.join(this.root, `${safeKind}.cborl`);
    return this.enqueueWrite(safeKind, async () => {
      await fs.promises.mkdir(this.root, { recursive: true });
      const stat = await fs.promises.stat(fileName).catch(() => undefined);
      const offset = stat?.size ?? 0;
      await fs.promises.appendFile(fileName, record);
      return { kind: safeKind, fileName, offset, bytes: payload.byteLength };
    });
  }

  async readRecord<T>(ref: SpillRecordRef): Promise<T> {
    const file = await fs.promises.open(ref.fileName, "r");
    try {
      const header = Buffer.allocUnsafe(recordLengthBytes);
      const headerRead = await file.read(header, 0, recordLengthBytes, ref.offset);
      if (headerRead.bytesRead !== recordLengthBytes) {
        throw new Error(`Spill record header is incomplete: ${ref.fileName}@${ref.offset}.`);
      }
      const bytes = header.readUInt32BE(0);
      if (bytes !== ref.bytes) {
        throw new Error(
          `Spill record length mismatch: expected ${ref.bytes} bytes, found ${bytes}.`,
        );
      }
      const payload = Buffer.allocUnsafe(bytes);
      const payloadRead = await file.read(payload, 0, bytes, ref.offset + recordLengthBytes);
      if (payloadRead.bytesRead !== bytes) {
        throw new Error(`Spill record payload is incomplete: ${ref.fileName}@${ref.offset}.`);
      }
      return decode(payload) as T;
    } finally {
      await file.close();
    }
  }

  async clear(): Promise<void> {
    await fs.promises.rm(this.root, { recursive: true, force: true });
  }

  async sweep(): Promise<number> {
    const cutoff = Date.now() - this.ttlMs;
    let removed = 0;
    const visit = async (directory: string): Promise<void> => {
      const entries = await fs.promises.readdir(directory, { withFileTypes: true }).catch(() => []);
      for (const entry of entries) {
        const fileName = path.join(directory, entry.name);
        if (entry.isDirectory()) {
          await visit(fileName);
          const nested = await fs.promises.readdir(fileName).catch(() => []);
          if (nested.length === 0) {
            await fs.promises.rm(fileName, { force: true });
          }
          continue;
        }
        const stat = await fs.promises.stat(fileName).catch(() => undefined);
        if (!stat || stat.mtimeMs > cutoff) {
          continue;
        }
        await fs.promises.rm(fileName, { force: true });
        removed += 1;
      }
    };
    await visit(this.root);
    return removed;
  }

  private enqueueWrite<T>(kind: string, task: () => Promise<T>): Promise<T> {
    const previous = this.writeQueues.get(kind) ?? Promise.resolve();
    const pending = previous.catch(() => undefined).then(task);
    const marker = pending.then(
      () => undefined,
      () => undefined,
    );
    this.writeQueues.set(kind, marker);
    void marker.finally(() => {
      if (this.writeQueues.get(kind) === marker) {
        this.writeQueues.delete(kind);
      }
    });
    return pending;
  }
}

function safePathSegment(value: string): string {
  const safe = value.replace(/[^A-Za-z0-9_.-]+/g, "-").replace(/^-+|-+$/g, "");
  return safe || "default";
}
