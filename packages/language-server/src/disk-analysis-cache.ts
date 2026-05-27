import crypto from "node:crypto";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { decode, encode } from "cbor-x";
import type {
  AspInclude,
  AspParsedDocument,
  FileAnalysisSummary,
  VbExportSummary,
  VbExternalRef,
  VbSymbol,
} from "@asp-lsp/core";
import type { Diagnostic } from "vscode-languageserver/node";

export const diskAnalysisCacheFormatVersion = 4;
export const fileAnalysisSummaryFormatVersion = 1;
export const vbPublicSymbolSummaryFormatVersion = 2;

export interface DiskAnalysisCacheOptions {
  enabled: boolean;
  directory?: string;
  workspaceRoots: string[];
  ttlHours: number;
  maxSizeMb: number;
}

export interface DiskSourceMetadata {
  uri: string;
  fileName: string;
  mtimeMs: number;
  size: number;
}

export interface DiskDiagnosticEntry {
  key: string;
  items: Diagnostic[];
}

export interface DiskAnalysisCachePayload {
  version: number;
  source: DiskSourceMetadata;
  settingsKey: string;
  parsed: AspParsedDocument;
  diagnostics?: DiskDiagnosticEntry;
  fastDiagnostics?: DiskDiagnosticEntry;
  includeDiagnostics?: DiskDiagnosticEntry;
  syntaxDiagnostics?: DiskDiagnosticEntry;
  projectDiagnostics?: DiskDiagnosticEntry;
  slowDiagnostics?: DiskDiagnosticEntry;
  fileSummary?: FileAnalysisSummary;
}

export interface DiskDiagnosticsCachePayload {
  version: number;
  source: DiskSourceMetadata;
  settingsKey: string;
  diagnostics?: DiskDiagnosticEntry;
  fastDiagnostics?: DiskDiagnosticEntry;
  includeDiagnostics?: DiskDiagnosticEntry;
  syntaxDiagnostics?: DiskDiagnosticEntry;
  projectDiagnostics?: DiskDiagnosticEntry;
  slowDiagnostics?: DiskDiagnosticEntry;
}

export interface VbPublicSymbolSummary {
  version: number;
  source: DiskSourceMetadata;
  settingsKey: string;
  defaultLanguage: string;
  legacyEncoding?: string;
  includes: AspInclude[];
  publicSymbols: VbSymbol[];
  exports?: VbExportSummary[];
  externalRefs?: VbExternalRef[];
  fileSummary?: FileAnalysisSummary;
}

export interface DiskFileAnalysisSummaryPayload {
  version: number;
  source: DiskSourceMetadata;
  settingsKey: string;
  summary: FileAnalysisSummary;
}

export class DiskAnalysisCache {
  private readonly enabled: boolean;
  private readonly namespaceRoot: string;
  private readonly ttlMs: number;
  private readonly maxSizeBytes: number;

  constructor(options: DiskAnalysisCacheOptions) {
    this.enabled = options.enabled;
    const namespace = hashString(
      JSON.stringify({
        version: diskAnalysisCacheFormatVersion,
        roots: options.workspaceRoots.map(normalizePath).sort(),
      }),
    );
    this.namespaceRoot = path.join(
      options.directory && options.directory.length > 0
        ? path.resolve(options.directory)
        : defaultCacheDirectory(),
      `v${diskAnalysisCacheFormatVersion}`,
      namespace,
    );
    this.ttlMs = Math.max(1, options.ttlHours) * 60 * 60 * 1000;
    this.maxSizeBytes = Math.max(1, options.maxSizeMb) * 1024 * 1024;
  }

  get root(): string {
    return this.namespaceRoot;
  }

  readFreshSync(
    source: DiskSourceMetadata,
    settingsKey: string,
  ): DiskAnalysisCachePayload | undefined {
    if (!this.enabled) {
      return undefined;
    }
    try {
      const fileName = this.entryPath(source.fileName);
      const payload = decode(fs.readFileSync(fileName)) as unknown;
      if (!isPayload(payload) || !matchesPayload(payload, source, settingsKey)) {
        return undefined;
      }
      touchSync(fileName);
      return payload;
    } catch {
      return undefined;
    }
  }

  async readFresh(
    source: DiskSourceMetadata,
    settingsKey: string,
  ): Promise<DiskAnalysisCachePayload | undefined> {
    if (!this.enabled) {
      return undefined;
    }
    try {
      const fileName = this.entryPath(source.fileName);
      const payload = decode(await fs.promises.readFile(fileName)) as unknown;
      if (!isPayload(payload) || !matchesPayload(payload, source, settingsKey)) {
        return undefined;
      }
      await touch(fileName);
      return payload;
    } catch {
      return undefined;
    }
  }

  async readDiagnosticsFresh(
    source: DiskSourceMetadata,
    settingsKey: string,
  ): Promise<DiskDiagnosticsCachePayload | undefined> {
    if (!this.enabled) {
      return undefined;
    }
    try {
      const fileName = this.entryPath(source.fileName, "diagnostics");
      const payload = decode(await fs.promises.readFile(fileName)) as unknown;
      if (!isDiagnosticsPayload(payload) || !matchesPayload(payload, source, settingsKey)) {
        return undefined;
      }
      await touch(fileName);
      return payload;
    } catch {
      return undefined;
    }
  }

  async readVbPublicSymbolSummaryFresh(
    source: DiskSourceMetadata,
    settingsKey: string,
  ): Promise<VbPublicSymbolSummary | undefined> {
    if (!this.enabled) {
      return undefined;
    }
    try {
      const fileName = this.entryPath(source.fileName, "vb-public-symbols");
      const payload = decode(await fs.promises.readFile(fileName)) as unknown;
      if (!isVbPublicSymbolSummary(payload) || !matchesPayload(payload, source, settingsKey)) {
        return undefined;
      }
      await touch(fileName);
      return payload;
    } catch {
      return undefined;
    }
  }

  async readFileAnalysisSummaryFresh(
    source: DiskSourceMetadata,
    settingsKey: string,
  ): Promise<DiskFileAnalysisSummaryPayload | undefined> {
    if (!this.enabled) {
      return undefined;
    }
    try {
      const fileName = this.entryPath(source.fileName, "file-summary");
      const payload = decode(await fs.promises.readFile(fileName)) as unknown;
      if (!isFileAnalysisSummaryPayload(payload) || !matchesPayload(payload, source, settingsKey)) {
        return undefined;
      }
      await touch(fileName);
      return payload;
    } catch {
      return undefined;
    }
  }

  async write(payload: DiskAnalysisCachePayload): Promise<void> {
    if (!this.enabled) {
      return;
    }
    try {
      const fileName = this.entryPath(payload.source.fileName);
      await fs.promises.mkdir(path.dirname(fileName), { recursive: true });
      const temporary = `${fileName}.${process.pid}.${Date.now()}.tmp`;
      await fs.promises.writeFile(temporary, Buffer.from(encode(payload)));
      await fs.promises.rename(temporary, fileName);
      await this.writeDiagnostics(diagnosticsPayload(payload));
    } catch {
      return;
    }
  }

  async writeDiagnostics(payload: DiskDiagnosticsCachePayload): Promise<void> {
    if (!this.enabled) {
      return;
    }
    try {
      const fileName = this.entryPath(payload.source.fileName, "diagnostics");
      await fs.promises.mkdir(path.dirname(fileName), { recursive: true });
      const temporary = `${fileName}.${process.pid}.${Date.now()}.tmp`;
      await fs.promises.writeFile(temporary, Buffer.from(encode(payload)));
      await fs.promises.rename(temporary, fileName);
    } catch {
      return;
    }
  }

  async writeFileAnalysisSummary(payload: DiskFileAnalysisSummaryPayload): Promise<void> {
    if (!this.enabled) {
      return;
    }
    try {
      const fileName = this.entryPath(payload.source.fileName, "file-summary");
      await fs.promises.mkdir(path.dirname(fileName), { recursive: true });
      const temporary = `${fileName}.${process.pid}.${Date.now()}.tmp`;
      await fs.promises.writeFile(temporary, Buffer.from(encode(payload)));
      await fs.promises.rename(temporary, fileName);
    } catch {
      return;
    }
  }

  async writeVbPublicSymbolSummary(payload: VbPublicSymbolSummary): Promise<void> {
    if (!this.enabled) {
      return;
    }
    try {
      const fileName = this.entryPath(payload.source.fileName, "vb-public-symbols");
      await fs.promises.mkdir(path.dirname(fileName), { recursive: true });
      const temporary = `${fileName}.${process.pid}.${Date.now()}.tmp`;
      await fs.promises.writeFile(temporary, Buffer.from(encode(payload)));
      await fs.promises.rename(temporary, fileName);
    } catch {
      return;
    }
  }

  async clear(): Promise<void> {
    if (!this.enabled) {
      return;
    }
    await fs.promises
      .rm(this.namespaceRoot, { recursive: true, force: true })
      .catch(() => undefined);
  }

  async sweep(): Promise<void> {
    if (!this.enabled) {
      return;
    }
    const now = Date.now();
    const entries = await this.cacheEntries();
    let total = 0;
    const retained: Array<{ fileName: string; size: number; atimeMs: number }> = [];
    for (const entry of entries) {
      if (now - entry.atimeMs > this.ttlMs) {
        await fs.promises.rm(entry.fileName, { force: true }).catch(() => undefined);
        continue;
      }
      total += entry.size;
      retained.push(entry);
    }
    if (total <= this.maxSizeBytes) {
      return;
    }
    retained.sort((left, right) => left.atimeMs - right.atimeMs);
    for (const entry of retained) {
      if (total <= this.maxSizeBytes) {
        return;
      }
      await fs.promises.rm(entry.fileName, { force: true }).catch(() => undefined);
      total -= entry.size;
    }
  }

  private entryPath(fileName: string, kind = "analysis"): string {
    const suffix = kind === "analysis" ? "" : `.${kind}`;
    return path.join(this.namespaceRoot, `${hashString(normalizePath(fileName))}${suffix}.cbor`);
  }

  private async cacheEntries(): Promise<
    Array<{ fileName: string; size: number; atimeMs: number }>
  > {
    const names = await fs.promises.readdir(this.namespaceRoot).catch(() => []);
    const entries: Array<{ fileName: string; size: number; atimeMs: number }> = [];
    for (const name of names) {
      if (!name.endsWith(".cbor")) {
        continue;
      }
      const fileName = path.join(this.namespaceRoot, name);
      const stat = await fs.promises.stat(fileName).catch(() => undefined);
      if (stat?.isFile()) {
        entries.push({ fileName, size: stat.size, atimeMs: stat.atimeMs });
      }
    }
    return entries;
  }
}

export function defaultCacheDirectory(): string {
  if (process.platform === "darwin") {
    return path.join(os.homedir(), "Library", "Caches", "asp-lsp");
  }
  if (process.platform === "win32") {
    return path.join(process.env.LOCALAPPDATA ?? os.tmpdir(), "asp-lsp", "Cache");
  }
  return path.join(process.env.XDG_CACHE_HOME ?? path.join(os.homedir(), ".cache"), "asp-lsp");
}

function isPayload(value: unknown): value is DiskAnalysisCachePayload {
  if (!value || typeof value !== "object") {
    return false;
  }
  const record = value as Partial<DiskAnalysisCachePayload>;
  return (
    record.version === diskAnalysisCacheFormatVersion &&
    typeof record.settingsKey === "string" &&
    Boolean(record.source) &&
    typeof record.parsed?.text === "string"
  );
}

function isDiagnosticsPayload(value: unknown): value is DiskDiagnosticsCachePayload {
  if (!value || typeof value !== "object") {
    return false;
  }
  const record = value as Partial<DiskDiagnosticsCachePayload>;
  return (
    record.version === diskAnalysisCacheFormatVersion &&
    typeof record.settingsKey === "string" &&
    Boolean(record.source)
  );
}

function isVbPublicSymbolSummary(value: unknown): value is VbPublicSymbolSummary {
  if (!value || typeof value !== "object") {
    return false;
  }
  const record = value as Partial<VbPublicSymbolSummary>;
  return (
    record.version === vbPublicSymbolSummaryFormatVersion &&
    typeof record.settingsKey === "string" &&
    typeof record.defaultLanguage === "string" &&
    Boolean(record.source) &&
    Array.isArray(record.includes) &&
    Array.isArray(record.publicSymbols)
  );
}

function isFileAnalysisSummaryPayload(value: unknown): value is DiskFileAnalysisSummaryPayload {
  if (!value || typeof value !== "object") {
    return false;
  }
  const record = value as Partial<DiskFileAnalysisSummaryPayload>;
  return (
    record.version === fileAnalysisSummaryFormatVersion &&
    typeof record.settingsKey === "string" &&
    Boolean(record.source) &&
    typeof record.summary?.fingerprint === "string" &&
    typeof record.summary?.uri === "string"
  );
}

function matchesPayload(
  payload: Pick<DiskAnalysisCachePayload, "settingsKey" | "source">,
  source: DiskSourceMetadata,
  settingsKey: string,
): boolean {
  return (
    payload.settingsKey === settingsKey &&
    normalizePath(payload.source.fileName) === normalizePath(source.fileName) &&
    payload.source.uri === source.uri &&
    payload.source.mtimeMs === source.mtimeMs &&
    payload.source.size === source.size
  );
}

async function touch(fileName: string): Promise<void> {
  const stat = await fs.promises.stat(fileName).catch(() => undefined);
  if (!stat) {
    return;
  }
  await fs.promises.utimes(fileName, new Date(), stat.mtime).catch(() => undefined);
}

function touchSync(fileName: string): void {
  try {
    const stat = fs.statSync(fileName);
    fs.utimesSync(fileName, new Date(), stat.mtime);
  } catch {
    return;
  }
}

function hashString(value: string): string {
  return crypto.createHash("sha256").update(value).digest("hex");
}

function normalizePath(value: string): string {
  return path.resolve(value).toLowerCase();
}

function diagnosticsPayload(payload: DiskAnalysisCachePayload): DiskDiagnosticsCachePayload {
  return {
    version: payload.version,
    source: payload.source,
    settingsKey: payload.settingsKey,
    diagnostics: payload.diagnostics,
    fastDiagnostics: payload.fastDiagnostics,
    includeDiagnostics: payload.includeDiagnostics,
    syntaxDiagnostics: payload.syntaxDiagnostics,
    projectDiagnostics: payload.projectDiagnostics,
    slowDiagnostics: payload.slowDiagnostics,
  };
}
