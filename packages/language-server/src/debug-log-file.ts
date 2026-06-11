import fs from "node:fs/promises";
import path from "node:path";

export type DebugLogFileLevel = "debug" | "trace" | "warn";

export interface DebugLogFileEntry {
  filePath: string;
  level: DebugLogFileLevel;
  category: string;
  message: string;
  metadata?: Record<string, unknown>;
}

export interface DebugLogFileWriterOptions {
  maxBytes?: number;
  maxBackups?: number;
  maxQueuedEntries?: number;
  maxQueuedBytes?: number;
}

interface QueuedDebugLogFileEntry extends DebugLogFileEntry {
  line: string;
  byteLength: number;
}

const defaultMaxBytes = 10 * 1024 * 1024;
const defaultMaxBackups = 5;
const defaultMaxQueuedEntries = 10_000;
const defaultMaxQueuedBytes = 1024 * 1024;

export class DebugLogFileWriter {
  private readonly maxBytes: number;
  private readonly maxBackups: number;
  private readonly maxQueuedEntries: number;
  private readonly maxQueuedBytes: number;
  private readonly queue: QueuedDebugLogFileEntry[] = [];
  private readonly currentSizes = new Map<string, number>();
  private readonly failedPaths = new Set<string>();
  private flushing = false;
  private queuedBytes = 0;
  private droppedTraceCount = 0;
  private droppedLogCount = 0;

  constructor(
    private readonly warn: (message: string) => void,
    options: DebugLogFileWriterOptions = {},
  ) {
    this.maxBytes = positiveIntegerOption(options.maxBytes, defaultMaxBytes);
    this.maxBackups = positiveIntegerOption(options.maxBackups, defaultMaxBackups);
    this.maxQueuedEntries = positiveIntegerOption(
      options.maxQueuedEntries,
      defaultMaxQueuedEntries,
    );
    this.maxQueuedBytes = positiveIntegerOption(options.maxQueuedBytes, defaultMaxQueuedBytes);
  }

  enqueue(entry: DebugLogFileEntry): void {
    if (this.failedPaths.has(entry.filePath)) {
      return;
    }
    const line = formatDebugLogFileLine(entry);
    const queued: QueuedDebugLogFileEntry = {
      ...entry,
      line,
      byteLength: Buffer.byteLength(line, "utf8"),
    };
    if (!this.canQueue(queued)) {
      if (queued.level === "trace") {
        this.droppedTraceCount += 1;
        return;
      }
      const droppedTrace = this.dropOldestTraceEntry();
      if (!droppedTrace || !this.canQueue(queued)) {
        this.droppedLogCount += 1;
        return;
      }
    }
    this.queue.push(queued);
    this.queuedBytes += queued.byteLength;
    this.scheduleFlush();
  }

  private canQueue(entry: QueuedDebugLogFileEntry): boolean {
    return (
      this.queue.length < this.maxQueuedEntries &&
      this.queuedBytes + entry.byteLength <= this.maxQueuedBytes
    );
  }

  private dropOldestTraceEntry(): boolean {
    const index = this.queue.findIndex((entry) => entry.level === "trace");
    if (index === -1) {
      return false;
    }
    const [dropped] = this.queue.splice(index, 1);
    if (dropped) {
      this.queuedBytes -= dropped.byteLength;
      this.droppedTraceCount += 1;
      return true;
    }
    return false;
  }

  private scheduleFlush(): void {
    if (this.flushing) {
      return;
    }
    this.flushing = true;
    setTimeout(() => void this.flushAsync(), 0);
  }

  private async flushAsync(): Promise<void> {
    while (this.queue.length > 0) {
      const entry = this.queue.shift();
      if (!entry) {
        continue;
      }
      this.queuedBytes -= entry.byteLength;
      try {
        await this.writeDroppedNoticeAsync(entry.filePath);
        await this.writeLineAsync(entry.filePath, entry.line, entry.byteLength);
      } catch (error) {
        this.failedPaths.add(entry.filePath);
        this.warn(`[asp-lsp] debugLogFile.write.failed: ${entry.filePath}: ${errorMessage(error)}`);
      }
    }
    this.flushing = false;
    if (this.queue.length > 0) {
      this.scheduleFlush();
    }
  }

  private async writeDroppedNoticeAsync(filePath: string): Promise<void> {
    if (this.droppedTraceCount === 0 && this.droppedLogCount === 0) {
      return;
    }
    const line = formatDebugLogFileLine({
      filePath,
      level: "warn",
      category: "debugLogFile.queue",
      message: "[asp-lsp] debugLogFile.queue.dropped",
      metadata: {
        trace: this.droppedTraceCount,
        other: this.droppedLogCount,
      },
    });
    this.droppedTraceCount = 0;
    this.droppedLogCount = 0;
    await this.writeLineAsync(filePath, line, Buffer.byteLength(line, "utf8"));
  }

  private async writeLineAsync(filePath: string, line: string, byteLength: number): Promise<void> {
    await fs.mkdir(path.dirname(filePath), { recursive: true });
    await this.rotateIfNeededAsync(filePath, byteLength);
    await fs.appendFile(filePath, line, "utf8");
    this.currentSizes.set(filePath, (this.currentSizes.get(filePath) ?? 0) + byteLength);
  }

  private async rotateIfNeededAsync(filePath: string, incomingBytes: number): Promise<void> {
    const currentSize = await this.currentSizeAsync(filePath);
    if (currentSize + incomingBytes <= this.maxBytes) {
      return;
    }
    if (this.maxBackups <= 0) {
      await fs.rm(filePath, { force: true });
      this.currentSizes.set(filePath, 0);
      return;
    }
    await fs.rm(backupPath(filePath, this.maxBackups), { force: true });
    for (let index = this.maxBackups - 1; index >= 1; index -= 1) {
      await renameIfExistsAsync(backupPath(filePath, index), backupPath(filePath, index + 1));
    }
    await renameIfExistsAsync(filePath, backupPath(filePath, 1));
    this.currentSizes.set(filePath, 0);
  }

  private async currentSizeAsync(filePath: string): Promise<number> {
    const cached = this.currentSizes.get(filePath);
    if (cached !== undefined) {
      return cached;
    }
    try {
      const stat = await fs.stat(filePath);
      this.currentSizes.set(filePath, stat.size);
      return stat.size;
    } catch (error) {
      if (isNodeError(error) && error.code === "ENOENT") {
        this.currentSizes.set(filePath, 0);
        return 0;
      }
      throw error;
    }
  }
}

function formatDebugLogFileLine(entry: DebugLogFileEntry): string {
  const metadata =
    entry.metadata && Object.keys(entry.metadata).length > 0
      ? ` ${safeJsonStringify(entry.metadata)}`
      : "";
  return `${new Date().toISOString()} ${entry.level.toUpperCase()} ${entry.category} ${entry.message}${metadata}\n`;
}

function safeJsonStringify(value: unknown): string {
  try {
    return JSON.stringify(value);
  } catch {
    return '{"serialization":"failed"}';
  }
}

function backupPath(filePath: string, index: number): string {
  return `${filePath}.${index}`;
}

async function renameIfExistsAsync(source: string, target: string): Promise<void> {
  try {
    await fs.rename(source, target);
  } catch (error) {
    if (!isNodeError(error) || error.code !== "ENOENT") {
      throw error;
    }
  }
}

function positiveIntegerOption(value: number | undefined, fallback: number): number {
  return typeof value === "number" && Number.isFinite(value) && value > 0
    ? Math.floor(value)
    : fallback;
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function isNodeError(error: unknown): error is NodeJS.ErrnoException {
  return error instanceof Error && "code" in error;
}
