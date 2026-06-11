import type { DiskAnalysisSourceMetadata } from "./disk-analysis-cache";
import { fileIdentityKeyFromFileName } from "./file-identity";

export interface WorkspaceIncludeGraphEntry {
  fileName: string;
  source: DiskAnalysisSourceMetadata;
  targetFileNames: string[];
  refsFingerprint: string;
}

export interface WorkspaceIncludeGraphSnapshot {
  settingsKey: string;
  entries: WorkspaceIncludeGraphEntry[];
}

interface ForwardEntry {
  fileName: string;
  source: DiskAnalysisSourceMetadata;
  targetFileNames: Set<string>;
  refsFingerprint: string;
}

export class WorkspaceIncludeGraph {
  private readonly forward = new Map<string, ForwardEntry>();
  private readonly reverse = new Map<string, Set<string>>();
  private currentSettingsKey: string | undefined;

  get settingsKey(): string | undefined {
    return this.currentSettingsKey;
  }

  get size(): number {
    return this.forward.size;
  }

  reset(settingsKey?: string): void {
    this.forward.clear();
    this.reverse.clear();
    this.currentSettingsKey = settingsKey;
  }

  restore(snapshot: WorkspaceIncludeGraphSnapshot): void {
    this.reset(snapshot.settingsKey);
    for (const entry of snapshot.entries) {
      this.upsert(entry.fileName, entry.source, entry.targetFileNames, entry.refsFingerprint);
    }
  }

  snapshot(settingsKey = this.currentSettingsKey): WorkspaceIncludeGraphSnapshot | undefined {
    if (!settingsKey) {
      return undefined;
    }
    return {
      settingsKey,
      entries: [...this.forward.values()].map((entry) => ({
        fileName: entry.fileName,
        source: entry.source,
        targetFileNames: [...entry.targetFileNames],
        refsFingerprint: entry.refsFingerprint,
      })),
    };
  }

  get(fileName: string): WorkspaceIncludeGraphEntry | undefined {
    const entry = this.forward.get(fileIdentityKeyFromFileName(fileName));
    return entry
      ? {
          fileName: entry.fileName,
          source: entry.source,
          targetFileNames: [...entry.targetFileNames],
          refsFingerprint: entry.refsFingerprint,
        }
      : undefined;
  }

  upsert(
    fileName: string,
    source: DiskAnalysisSourceMetadata,
    targetFileNames: Iterable<string>,
    refsFingerprint: string,
  ): void {
    const ownerKey = fileIdentityKeyFromFileName(fileName);
    this.remove(ownerKey);
    const targets = new Set([...targetFileNames].map(fileIdentityKeyFromFileName));
    this.forward.set(ownerKey, {
      fileName,
      source,
      targetFileNames: targets,
      refsFingerprint,
    });
    for (const targetKey of targets) {
      const owners = this.reverse.get(targetKey);
      if (owners) {
        owners.add(ownerKey);
      } else {
        this.reverse.set(targetKey, new Set([ownerKey]));
      }
    }
  }

  delete(fileName: string): void {
    this.remove(fileIdentityKeyFromFileName(fileName));
  }

  candidatesForTargets(targetFileNames: Iterable<string>): string[] {
    const ownerKeys = new Set<string>();
    for (const targetFileName of targetFileNames) {
      const targetKey = fileIdentityKeyFromFileName(targetFileName);
      for (const ownerKey of this.reverse.get(targetKey) ?? []) {
        ownerKeys.add(ownerKey);
      }
    }
    return [...ownerKeys]
      .map((ownerKey) => this.forward.get(ownerKey)?.fileName)
      .filter((fileName): fileName is string => fileName !== undefined);
  }

  private remove(ownerKey: string): void {
    const existing = this.forward.get(ownerKey);
    if (!existing) {
      return;
    }
    for (const targetKey of existing.targetFileNames) {
      const owners = this.reverse.get(targetKey);
      owners?.delete(ownerKey);
      if (owners?.size === 0) {
        this.reverse.delete(targetKey);
      }
    }
    this.forward.delete(ownerKey);
  }
}
