import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { decode, encode } from "cbor-x";
import type { Diagnostic } from "vscode-languageserver-types";

const formatVersion = 2;
const defaultTtlHours = 24 * 14;
const defaultMaxSizeMb = 128;

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

export interface DiskAnalysisBuilderState {
  publicSignature?: unknown;
  includeDeps?: unknown[];
  externalRefUsageKeys?: string[];
  diagnosticsLayerFingerprints?: Record<string, string>;
}

interface PersistedDiskAnalysisEntry extends DiskAnalysisCacheEntry {
  formatVersion: number;
  toolVersion: string;
  namespace: string;
  writtenAt: number;
}

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

  read(lookup: DiskAnalysisCacheLookup): Diagnostic[] | undefined {
    return this.readAnalysis(lookup)?.diagnostics;
  }

  readAnalysis(lookup: DiskAnalysisCacheLookup): DiskAnalysisCacheEntry | undefined {
    if (!this.enabled) {
      return undefined;
    }
    const entry = this.readEntry(this.fileNameForLookup(lookup));
    if (!entry || !this.matches(entry, lookup)) {
      return undefined;
    }
    return entry;
  }

  write(entry: DiskAnalysisCacheEntry): void {
    if (!this.enabled) {
      return;
    }
    fs.mkdirSync(this.root, { recursive: true });
    const payload: PersistedDiskAnalysisEntry = {
      ...entry,
      formatVersion,
      toolVersion: this.options.toolVersion,
      namespace: this.options.namespace,
      writtenAt: Date.now(),
    };
    fs.writeFileSync(this.fileNameForLookup(entry), encode(payload));
  }

  clear(): void {
    fs.rmSync(this.root, { recursive: true, force: true });
  }

  sweep(): void {
    if (!this.enabled) {
      return;
    }
    const files = this.cacheFiles();
    const now = Date.now();
    let entries = files
      .map((fileName) => {
        const stat = fs.statSync(fileName, { throwIfNoEntry: false });
        const entry = this.readEntry(fileName);
        const expired = !entry || now - entry.writtenAt > this.ttlMs;
        if (expired) {
          fs.rmSync(fileName, { force: true });
          return undefined;
        }
        return {
          fileName,
          size: stat?.size ?? 0,
          writtenAt: entry.writtenAt,
        };
      })
      .filter((entry): entry is { fileName: string; size: number; writtenAt: number } =>
        Boolean(entry),
      );
    let total = entries.reduce((sum, entry) => sum + entry.size, 0);
    entries = entries.sort((left, right) => left.writtenAt - right.writtenAt);
    for (const entry of entries) {
      if (total <= this.maxSizeBytes) {
        break;
      }
      fs.rmSync(entry.fileName, { force: true });
      total -= entry.size;
    }
  }

  private matches(entry: PersistedDiskAnalysisEntry, lookup: DiskAnalysisCacheLookup): boolean {
    return (
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

  private readEntry(fileName: string): PersistedDiskAnalysisEntry | undefined {
    try {
      return decode(fs.readFileSync(fileName)) as PersistedDiskAnalysisEntry;
    } catch {
      fs.rmSync(fileName, { force: true });
      return undefined;
    }
  }

  private fileNameForLookup(lookup: DiskAnalysisCacheLookup): string {
    return path.join(
      this.root,
      `${stableHash(
        JSON.stringify({
          namespace: this.options.namespace,
          fileName: lookup.source.fileName,
          settingsKey: lookup.settingsKey,
        }),
      )}.cbor`,
    );
  }

  private cacheFiles(): string[] {
    try {
      return fs
        .readdirSync(this.root)
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
