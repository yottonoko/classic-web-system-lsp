import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { decode, encode } from "cbor-x";
import type { FileAnalysisSummary } from "@asp-lsp/core";
import type { Diagnostic } from "vscode-languageserver-types";

const formatVersion = 3;
const defaultTtlHours = 24 * 14;
const defaultMaxSizeMb = 128;
type DiskCacheEntryKind = "diagnostics" | "summary";

export interface DiskAnalysisCacheOptions {
  enabled: boolean;
  directory?: string;
  ttlHours?: number;
  maxSizeMb?: number;
  namespace: string;
  toolVersion: string;
}

export interface DiskAnalysisSourceMetadata {
  fileName: string;
  mtimeMs: number;
  size: number;
}

export interface DiskAnalysisCacheLookup {
  source: DiskAnalysisSourceMetadata;
  settingsKey: string;
}

export interface DiskAnalysisCacheEntry extends DiskAnalysisCacheLookup {
  diagnostics: Diagnostic[];
  builderState?: DiskAnalysisBuilderState;
}

export interface DiskSummaryCacheEntry extends DiskAnalysisCacheLookup {
  summary: FileAnalysisSummary;
  publicSignature?: unknown;
}

export interface DiskAnalysisBuilderState {
  publicSignature?: unknown;
  includeDeps?: unknown[];
  externalRefUsageKeys?: string[];
  diagnosticsLayerFingerprints?: Record<string, string>;
}

interface PersistedDiskAnalysisEntry extends DiskAnalysisCacheEntry {
  kind: "diagnostics";
  formatVersion: number;
  toolVersion: string;
  namespace: string;
  writtenAt: number;
}

interface PersistedDiskSummaryEntry extends DiskSummaryCacheEntry {
  kind: "summary";
  formatVersion: number;
  toolVersion: string;
  namespace: string;
  writtenAt: number;
}

type PersistedDiskEntry = PersistedDiskAnalysisEntry | PersistedDiskSummaryEntry;

export class DiskAnalysisCache {
  private readonly root: string;
  private readonly ttlMs: number;
  private readonly maxSizeBytes: number;

  constructor(private readonly options: DiskAnalysisCacheOptions) {
    this.root =
      options.directory && options.directory.trim().length > 0
        ? path.resolve(options.directory)
        : path.join(os.tmpdir(), "asp-lsp-analysis-cache");
    this.ttlMs = Math.max(0.000001, options.ttlHours ?? defaultTtlHours) * 60 * 60 * 1000;
    this.maxSizeBytes = Math.max(0.000001, options.maxSizeMb ?? defaultMaxSizeMb) * 1024 * 1024;
  }

  get enabled(): boolean {
    return this.options.enabled;
  }

  get directory(): string {
    return this.root;
  }

  async read(lookup: DiskAnalysisCacheLookup): Promise<Diagnostic[] | undefined> {
    return (await this.readAnalysis(lookup))?.diagnostics;
  }

  async readAnalysis(lookup: DiskAnalysisCacheLookup): Promise<DiskAnalysisCacheEntry | undefined> {
    if (!this.enabled) {
      return undefined;
    }
    const entry = await this.readEntry(this.fileNameForLookup(lookup, "diagnostics"));
    if (!entry || entry.kind !== "diagnostics" || !this.matches(entry, lookup, "diagnostics")) {
      return undefined;
    }
    return entry;
  }

  async readSummary(lookup: DiskAnalysisCacheLookup): Promise<DiskSummaryCacheEntry | undefined> {
    if (!this.enabled) {
      return undefined;
    }
    const entry = await this.readEntry(this.fileNameForLookup(lookup, "summary"));
    if (!entry || entry.kind !== "summary" || !this.matches(entry, lookup, "summary")) {
      return undefined;
    }
    return entry;
  }

  async write(entry: DiskAnalysisCacheEntry): Promise<void> {
    if (!this.enabled) {
      return;
    }
    await fs.promises.mkdir(this.root, { recursive: true });
    const payload: PersistedDiskAnalysisEntry = {
      ...entry,
      kind: "diagnostics",
      formatVersion,
      toolVersion: this.options.toolVersion,
      namespace: this.options.namespace,
      writtenAt: Date.now(),
    };
    await fs.promises.writeFile(this.fileNameForLookup(entry, "diagnostics"), encode(payload));
  }

  async writeSummary(entry: DiskSummaryCacheEntry): Promise<void> {
    if (!this.enabled) {
      return;
    }
    await fs.promises.mkdir(this.root, { recursive: true });
    const payload: PersistedDiskSummaryEntry = {
      ...entry,
      kind: "summary",
      formatVersion,
      toolVersion: this.options.toolVersion,
      namespace: this.options.namespace,
      writtenAt: Date.now(),
    };
    await fs.promises.writeFile(this.fileNameForLookup(entry, "summary"), encode(payload));
  }

  async clear(): Promise<void> {
    await fs.promises.rm(this.root, { recursive: true, force: true });
  }

  async sweep(): Promise<void> {
    if (!this.enabled) {
      return;
    }
    const files = await this.cacheFiles();
    const now = Date.now();
    let entries: Array<{ fileName: string; size: number; writtenAt: number }> = [];
    for (const fileName of files) {
      const stat = await fs.promises.stat(fileName).catch(() => undefined);
      const entry = await this.readEntry(fileName);
      const expired = !entry || now - entry.writtenAt > this.ttlMs;
      if (expired) {
        await fs.promises.rm(fileName, { force: true });
        continue;
      }
      entries.push({
        fileName,
        size: stat?.size ?? 0,
        writtenAt: entry.writtenAt,
      });
    }
    let total = entries.reduce((sum, entry) => sum + entry.size, 0);
    entries = entries.sort((left, right) => left.writtenAt - right.writtenAt);
    for (const entry of entries) {
      if (total <= this.maxSizeBytes) {
        break;
      }
      await fs.promises.rm(entry.fileName, { force: true });
      total -= entry.size;
    }
  }

  private matches(
    entry: PersistedDiskEntry,
    lookup: DiskAnalysisCacheLookup,
    kind: DiskCacheEntryKind,
  ): boolean {
    return (
      entry.kind === kind &&
      entry.formatVersion === formatVersion &&
      entry.toolVersion === this.options.toolVersion &&
      entry.namespace === this.options.namespace &&
      entry.settingsKey === lookup.settingsKey &&
      entry.source.fileName === lookup.source.fileName &&
      entry.source.mtimeMs === lookup.source.mtimeMs &&
      entry.source.size === lookup.source.size &&
      Date.now() - entry.writtenAt <= this.ttlMs
    );
  }

  private async readEntry(fileName: string): Promise<PersistedDiskEntry | undefined> {
    try {
      return decode(await fs.promises.readFile(fileName)) as PersistedDiskEntry;
    } catch {
      await fs.promises.rm(fileName, { force: true }).catch(() => undefined);
      return undefined;
    }
  }

  private fileNameForLookup(lookup: DiskAnalysisCacheLookup, kind: DiskCacheEntryKind): string {
    return path.join(
      this.root,
      `${stableHash(
        JSON.stringify({
          kind,
          namespace: this.options.namespace,
          fileName: lookup.source.fileName,
          settingsKey: lookup.settingsKey,
        }),
      )}.cbor`,
    );
  }

  private async cacheFiles(): Promise<string[]> {
    try {
      return (await fs.promises.readdir(this.root))
        .filter((entry) => entry.endsWith(".cbor"))
        .map((entry) => path.join(this.root, entry));
    } catch {
      return [];
    }
  }
}

function stableHash(text: string): string {
  let hash = 2166136261;
  for (let index = 0; index < text.length; index += 1) {
    hash ^= text.charCodeAt(index);
    hash = Math.imul(hash, 16777619);
  }
  return (hash >>> 0).toString(16).padStart(8, "0");
}
