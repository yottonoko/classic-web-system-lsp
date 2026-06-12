import fs from "node:fs";
import crypto from "node:crypto";
import os from "node:os";
import path from "node:path";
import { decode, encode } from "cbor-x";
import type {
  AspInclude,
  AspParsedDocument,
  FileAnalysisSummary,
  VbSymbolIndex,
} from "@asp-lsp/core";
import type { Diagnostic } from "vscode-languageserver-types";

const formatVersion = 6;
const defaultTtlHours = 24 * 14;
const defaultMaxSizeMb = 128;
const defaultSweepBatchSize = 128;
type DiskCacheEntryKind =
  | "diagnostics"
  | "summary"
  | "includeRefs"
  | "vbSymbolIndex"
  | "parsedDocument"
  | "workspaceIndex"
  | "workspaceIncludeGraph";

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
  contentHash?: string;
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

export interface DiskIncludeRefsCacheEntry extends DiskAnalysisCacheLookup {
  includeRefs: AspInclude[];
  fingerprint: string;
}

export interface DiskVbSymbolIndexCacheEntry extends DiskAnalysisCacheLookup {
  index: VbSymbolIndex;
  fingerprint: string;
}

export interface DiskParsedDocumentCacheEntry extends DiskAnalysisCacheLookup {
  parsed: AspParsedDocument;
  summary: FileAnalysisSummary;
  publicSignature?: unknown;
}

export interface DiskWorkspaceIndexedDocument {
  uri: string;
  fileName: string;
  mtimeMs: number;
  size: number;
  contentHash?: string;
}

export interface DiskWorkspaceIndexCacheEntry {
  settingsKey: string;
  entries: DiskWorkspaceIndexedDocument[];
  truncated: boolean;
}

export interface DiskWorkspaceIncludeGraphEntry {
  fileName: string;
  source: DiskAnalysisSourceMetadata;
  targetFileNames: string[];
  refsFingerprint: string;
}

export interface DiskWorkspaceIncludeGraphCacheEntry {
  settingsKey: string;
  entries: DiskWorkspaceIncludeGraphEntry[];
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

interface PersistedDiskIncludeRefsEntry extends DiskIncludeRefsCacheEntry {
  kind: "includeRefs";
  formatVersion: number;
  toolVersion: string;
  namespace: string;
  writtenAt: number;
}

interface PersistedDiskVbSymbolIndexEntry extends DiskVbSymbolIndexCacheEntry {
  kind: "vbSymbolIndex";
  formatVersion: number;
  toolVersion: string;
  namespace: string;
  writtenAt: number;
}

interface PersistedDiskParsedDocumentEntry extends DiskParsedDocumentCacheEntry {
  kind: "parsedDocument";
  formatVersion: number;
  toolVersion: string;
  namespace: string;
  writtenAt: number;
}

interface PersistedDiskWorkspaceIndexEntry extends DiskWorkspaceIndexCacheEntry {
  kind: "workspaceIndex";
  formatVersion: number;
  toolVersion: string;
  namespace: string;
  writtenAt: number;
}

interface PersistedDiskWorkspaceIncludeGraphEntry extends DiskWorkspaceIncludeGraphCacheEntry {
  kind: "workspaceIncludeGraph";
  formatVersion: number;
  toolVersion: string;
  namespace: string;
  writtenAt: number;
}

type PersistedDiskEntry =
  | PersistedDiskAnalysisEntry
  | PersistedDiskSummaryEntry
  | PersistedDiskIncludeRefsEntry
  | PersistedDiskVbSymbolIndexEntry
  | PersistedDiskParsedDocumentEntry
  | PersistedDiskWorkspaceIndexEntry
  | PersistedDiskWorkspaceIncludeGraphEntry;

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
    const entry = await this.readEntry(
      this.fileNameForLookup(lookup, "diagnostics"),
      "diagnostics",
    );
    if (!entry || entry.kind !== "diagnostics" || !this.matches(entry, lookup, "diagnostics")) {
      return undefined;
    }
    return entry;
  }

  async readSummary(lookup: DiskAnalysisCacheLookup): Promise<DiskSummaryCacheEntry | undefined> {
    if (!this.enabled) {
      return undefined;
    }
    const entry = await this.readEntry(this.fileNameForLookup(lookup, "summary"), "summary");
    if (!entry || entry.kind !== "summary" || !this.matches(entry, lookup, "summary")) {
      return undefined;
    }
    return entry;
  }

  async readIncludeRefs(
    lookup: DiskAnalysisCacheLookup,
  ): Promise<DiskIncludeRefsCacheEntry | undefined> {
    if (!this.enabled) {
      return undefined;
    }
    const entry = await this.readEntry(
      this.fileNameForLookup(lookup, "includeRefs"),
      "includeRefs",
    );
    if (!entry || entry.kind !== "includeRefs" || !this.matches(entry, lookup, "includeRefs")) {
      return undefined;
    }
    return entry;
  }

  async readVbSymbolIndex(
    lookup: DiskAnalysisCacheLookup,
  ): Promise<DiskVbSymbolIndexCacheEntry | undefined> {
    if (!this.enabled) {
      return undefined;
    }
    const entry = await this.readEntry(
      this.fileNameForLookup(lookup, "vbSymbolIndex"),
      "vbSymbolIndex",
    );
    if (!entry || entry.kind !== "vbSymbolIndex" || !this.matches(entry, lookup, "vbSymbolIndex")) {
      return undefined;
    }
    return entry;
  }

  async readParsedDocument(
    lookup: DiskAnalysisCacheLookup,
  ): Promise<DiskParsedDocumentCacheEntry | undefined> {
    if (!this.enabled) {
      return undefined;
    }
    const entry = await this.readEntry(
      this.fileNameForLookup(lookup, "parsedDocument"),
      "parsedDocument",
    );
    if (
      !entry ||
      entry.kind !== "parsedDocument" ||
      !this.matches(entry, lookup, "parsedDocument")
    ) {
      return undefined;
    }
    return entry;
  }

  async readWorkspaceIndex(settingsKey: string): Promise<DiskWorkspaceIndexCacheEntry | undefined> {
    if (!this.enabled) {
      return undefined;
    }
    const entry = await this.readEntry(
      this.fileNameForKey(settingsKey, "workspaceIndex"),
      "workspaceIndex",
    );
    if (
      !entry ||
      entry.kind !== "workspaceIndex" ||
      !this.matchesWorkspaceIndex(entry, settingsKey)
    ) {
      return undefined;
    }
    return entry;
  }

  async readWorkspaceIncludeGraph(
    settingsKey: string,
  ): Promise<DiskWorkspaceIncludeGraphCacheEntry | undefined> {
    if (!this.enabled) {
      return undefined;
    }
    const entry = await this.readEntry(
      this.fileNameForKey(settingsKey, "workspaceIncludeGraph"),
      "workspaceIncludeGraph",
    );
    if (
      !entry ||
      entry.kind !== "workspaceIncludeGraph" ||
      !this.matchesWorkspaceIncludeGraph(entry, settingsKey)
    ) {
      return undefined;
    }
    return entry;
  }

  async write(entry: DiskAnalysisCacheEntry): Promise<void> {
    if (!this.enabled) {
      return;
    }
    const payload: PersistedDiskAnalysisEntry = {
      ...entry,
      kind: "diagnostics",
      formatVersion,
      toolVersion: this.options.toolVersion,
      namespace: this.options.namespace,
      writtenAt: Date.now(),
    };
    await this.writeEntry(this.fileNameForLookup(entry, "diagnostics"), payload);
  }

  async writeSummary(entry: DiskSummaryCacheEntry): Promise<void> {
    if (!this.enabled) {
      return;
    }
    const payload: PersistedDiskSummaryEntry = {
      ...entry,
      kind: "summary",
      formatVersion,
      toolVersion: this.options.toolVersion,
      namespace: this.options.namespace,
      writtenAt: Date.now(),
    };
    await this.writeEntry(this.fileNameForLookup(entry, "summary"), payload);
  }

  async writeIncludeRefs(entry: DiskIncludeRefsCacheEntry): Promise<void> {
    if (!this.enabled) {
      return;
    }
    const payload: PersistedDiskIncludeRefsEntry = {
      ...entry,
      kind: "includeRefs",
      formatVersion,
      toolVersion: this.options.toolVersion,
      namespace: this.options.namespace,
      writtenAt: Date.now(),
    };
    await this.writeEntry(this.fileNameForLookup(entry, "includeRefs"), payload);
  }

  async writeVbSymbolIndex(entry: DiskVbSymbolIndexCacheEntry): Promise<void> {
    if (!this.enabled) {
      return;
    }
    const payload: PersistedDiskVbSymbolIndexEntry = {
      ...entry,
      kind: "vbSymbolIndex",
      formatVersion,
      toolVersion: this.options.toolVersion,
      namespace: this.options.namespace,
      writtenAt: Date.now(),
    };
    await this.writeEntry(this.fileNameForLookup(entry, "vbSymbolIndex"), payload);
  }

  async writeParsedDocument(entry: DiskParsedDocumentCacheEntry): Promise<void> {
    if (!this.enabled) {
      return;
    }
    const payload: PersistedDiskParsedDocumentEntry = {
      ...entry,
      kind: "parsedDocument",
      formatVersion,
      toolVersion: this.options.toolVersion,
      namespace: this.options.namespace,
      writtenAt: Date.now(),
    };
    await this.writeEntry(this.fileNameForLookup(entry, "parsedDocument"), payload);
  }

  async writeWorkspaceIndex(entry: DiskWorkspaceIndexCacheEntry): Promise<void> {
    if (!this.enabled) {
      return;
    }
    const payload: PersistedDiskWorkspaceIndexEntry = {
      ...entry,
      kind: "workspaceIndex",
      formatVersion,
      toolVersion: this.options.toolVersion,
      namespace: this.options.namespace,
      writtenAt: Date.now(),
    };
    await this.writeEntry(this.fileNameForKey(entry.settingsKey, "workspaceIndex"), payload);
  }

  async writeWorkspaceIncludeGraph(entry: DiskWorkspaceIncludeGraphCacheEntry): Promise<void> {
    if (!this.enabled) {
      return;
    }
    const payload: PersistedDiskWorkspaceIncludeGraphEntry = {
      ...entry,
      kind: "workspaceIncludeGraph",
      formatVersion,
      toolVersion: this.options.toolVersion,
      namespace: this.options.namespace,
      writtenAt: Date.now(),
    };
    await this.writeEntry(this.fileNameForKey(entry.settingsKey, "workspaceIncludeGraph"), payload);
  }

  async clear(): Promise<void> {
    await fs.promises.rm(this.root, { recursive: true, force: true });
  }

  async sweep(options: { batchSize?: number } = {}): Promise<void> {
    if (!this.enabled) {
      return;
    }
    const batchSize = Math.max(1, Math.floor(options.batchSize ?? defaultSweepBatchSize));
    const files = await this.cacheFiles();
    const now = Date.now();
    let entries: Array<{ fileName: string; size: number; writtenAt: number }> = [];
    for (let index = 0; index < files.length; index += 1) {
      const fileName = files[index];
      const stat = await fs.promises.stat(fileName).catch(() => undefined);
      const entry = await this.readEntry(fileName);
      const expired = !entry || now - entry.writtenAt > this.ttlMs;
      if (expired) {
        await fs.promises.rm(fileName, { force: true });
      } else {
        entries.push({
          fileName,
          size: stat?.size ?? 0,
          writtenAt: entry.writtenAt,
        });
      }
      if ((index + 1) % batchSize === 0) {
        await yieldToEventLoop();
      }
    }
    let total = entries.reduce((sum, entry) => sum + entry.size, 0);
    entries = entries.sort((left, right) => left.writtenAt - right.writtenAt);
    for (const entry of entries) {
      if (total <= this.maxSizeBytes) {
        break;
      }
      await fs.promises.rm(entry.fileName, { force: true });
      total -= entry.size;
      await yieldToEventLoop();
    }
  }

  private matches(
    entry: PersistedDiskEntry,
    lookup: DiskAnalysisCacheLookup,
    kind: DiskCacheEntryKind,
  ): boolean {
    return (
      entry.kind === kind &&
      "source" in entry &&
      entry.formatVersion === formatVersion &&
      entry.toolVersion === this.options.toolVersion &&
      entry.namespace === this.options.namespace &&
      entry.settingsKey === lookup.settingsKey &&
      this.sourceMatches(entry.source, lookup.source) &&
      Date.now() - entry.writtenAt <= this.ttlMs
    );
  }

  private sourceMatches(
    entrySource: DiskAnalysisSourceMetadata,
    lookupSource: DiskAnalysisSourceMetadata,
  ): boolean {
    if (entrySource.fileName !== lookupSource.fileName) {
      return false;
    }
    const entryHash = entrySource.contentHash;
    const lookupHash = lookupSource.contentHash;
    if (entryHash !== undefined && lookupHash !== undefined) {
      return entryHash === lookupHash;
    }
    return entrySource.mtimeMs === lookupSource.mtimeMs && entrySource.size === lookupSource.size;
  }

  private matchesWorkspaceIndex(
    entry: PersistedDiskWorkspaceIndexEntry,
    settingsKey: string,
  ): boolean {
    return (
      entry.kind === "workspaceIndex" &&
      entry.formatVersion === formatVersion &&
      entry.toolVersion === this.options.toolVersion &&
      entry.namespace === this.options.namespace &&
      entry.settingsKey === settingsKey &&
      Date.now() - entry.writtenAt <= this.ttlMs
    );
  }

  private matchesWorkspaceIncludeGraph(
    entry: PersistedDiskWorkspaceIncludeGraphEntry,
    settingsKey: string,
  ): boolean {
    return (
      entry.kind === "workspaceIncludeGraph" &&
      entry.formatVersion === formatVersion &&
      entry.toolVersion === this.options.toolVersion &&
      entry.namespace === this.options.namespace &&
      entry.settingsKey === settingsKey &&
      Date.now() - entry.writtenAt <= this.ttlMs
    );
  }

  private async readEntry(
    fileName: string,
    kind?: DiskCacheEntryKind,
  ): Promise<PersistedDiskEntry | undefined> {
    try {
      if (kind && isLargeEntryKind(kind)) {
        const stat = await fs.promises.stat(fileName).catch(() => undefined);
        if (stat && stat.size > this.largeEntryReadLimitBytes(kind)) {
          await fs.promises.rm(fileName, { force: true }).catch(() => undefined);
          return undefined;
        }
      }
      return decode(await fs.promises.readFile(fileName)) as PersistedDiskEntry;
    } catch {
      await fs.promises.rm(fileName, { force: true }).catch(() => undefined);
      return undefined;
    }
  }

  private async writeEntry(fileName: string, payload: PersistedDiskEntry): Promise<void> {
    await fs.promises.mkdir(path.dirname(fileName), { recursive: true });
    await fs.promises.writeFile(fileName, encode(payload));
  }

  private fileNameForLookup(lookup: DiskAnalysisCacheLookup, kind: DiskCacheEntryKind): string {
    return this.fileNameForKey(
      JSON.stringify({
        fileName: lookup.source.fileName,
        settingsKey: lookup.settingsKey,
      }),
      kind,
    );
  }

  private fileNameForKey(key: string, kind: DiskCacheEntryKind): string {
    const hash = stableHash(
      JSON.stringify({
        kind,
        namespace: this.options.namespace,
        key,
      }),
    );
    return path.join(this.root, hash.slice(0, 2), `${hash}.cbor`);
  }

  private async cacheFiles(): Promise<string[]> {
    try {
      const rootEntries = await fs.promises.readdir(this.root, { withFileTypes: true });
      const files: string[] = [];
      for (const entry of rootEntries) {
        const entryPath = path.join(this.root, entry.name);
        if (entry.isFile() && entry.name.endsWith(".cbor")) {
          files.push(entryPath);
          continue;
        }
        if (!entry.isDirectory() || !/^[0-9a-f]{2}$/i.test(entry.name)) {
          continue;
        }
        for (const shardEntry of await fs.promises.readdir(entryPath, { withFileTypes: true })) {
          if (shardEntry.isFile() && shardEntry.name.endsWith(".cbor")) {
            files.push(path.join(entryPath, shardEntry.name));
          }
        }
      }
      return files;
    } catch {
      return [];
    }
  }

  private largeEntryReadLimitBytes(kind: DiskCacheEntryKind): number {
    const floor = kind === "workspaceIndex" || kind === "workspaceIncludeGraph" ? 512 : 256;
    return Math.max(floor * 1024, Math.floor(this.maxSizeBytes * 0.5));
  }
}

function stableHash(text: string): string {
  return crypto.createHash("sha256").update(text).digest("hex");
}

export function diskContentHash(text: string): string {
  return crypto.createHash("sha256").update(text).digest("hex");
}

function isLargeEntryKind(kind: DiskCacheEntryKind): boolean {
  return (
    kind === "parsedDocument" ||
    kind === "vbSymbolIndex" ||
    kind === "workspaceIndex" ||
    kind === "workspaceIncludeGraph"
  );
}

function yieldToEventLoop(): Promise<void> {
  return new Promise((resolve) => setImmediate(resolve));
}
