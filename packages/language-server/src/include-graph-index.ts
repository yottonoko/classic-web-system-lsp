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
  ephemeral?: boolean;
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
      entries: [...this.forward.values()]
        .filter((entry) => entry.ephemeral !== true)
        .map((entry) => ({
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
    this.upsertEntry(fileName, source, targetFileNames, refsFingerprint, false);
  }

  upsertEphemeral(
    fileName: string,
    targetFileNames: Iterable<string>,
    refsFingerprint = "ephemeral",
  ): void {
    this.upsertEntry(
      fileName,
      { fileName, mtimeMs: 0, size: 0 },
      targetFileNames,
      refsFingerprint,
      true,
    );
  }

  recordEphemeralDependency(fileName: string, targetFileName: string): void {
    const ownerKey = fileIdentityKeyFromFileName(fileName);
    const targetKey = fileIdentityKeyFromFileName(targetFileName);
    const existing = this.forward.get(ownerKey);
    if (!existing) {
      this.upsertEphemeral(fileName, [targetFileName]);
      return;
    }
    if (existing.targetFileNames.has(targetKey)) {
      return;
    }
    existing.targetFileNames.add(targetKey);
    existing.ephemeral = true;
    const owners = this.reverse.get(targetKey);
    if (owners) {
      owners.add(ownerKey);
    } else {
      this.reverse.set(targetKey, new Set([ownerKey]));
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

  targetFileNamesForOwner(fileName: string): string[] {
    const entry = this.forward.get(fileIdentityKeyFromFileName(fileName));
    return entry ? [...entry.targetFileNames] : [];
  }

  dependsOnAnyTarget(
    ownerFileName: string,
    targetFileNames: Iterable<string>,
    options: { transitive?: boolean } = {},
  ): boolean {
    const targetKeys = new Set([...targetFileNames].map(fileIdentityKeyFromFileName));
    if (targetKeys.size === 0) {
      return false;
    }
    const queue = [fileIdentityKeyFromFileName(ownerFileName)];
    const visited = new Set<string>();
    while (queue.length > 0) {
      const ownerKey = queue.shift()!;
      if (visited.has(ownerKey)) {
        continue;
      }
      visited.add(ownerKey);
      const entry = this.forward.get(ownerKey);
      if (!entry) {
        continue;
      }
      for (const targetKey of entry.targetFileNames) {
        if (targetKeys.has(targetKey)) {
          return true;
        }
        if (options.transitive === true && !visited.has(targetKey)) {
          queue.push(targetKey);
        }
      }
    }
    return false;
  }

  dependentFileNamesForTargets(
    targetFileNames: Iterable<string>,
    options: { transitive?: boolean } = {},
  ): string[] {
    const resultKeys = new Set<string>();
    const queue = [...targetFileNames].map(fileIdentityKeyFromFileName);
    const visitedTargets = new Set<string>();
    while (queue.length > 0) {
      const targetKey = queue.shift()!;
      if (visitedTargets.has(targetKey)) {
        continue;
      }
      visitedTargets.add(targetKey);
      for (const ownerKey of this.reverse.get(targetKey) ?? []) {
        resultKeys.add(ownerKey);
        if (options.transitive === true && !visitedTargets.has(ownerKey)) {
          queue.push(ownerKey);
        }
      }
    }
    return [...resultKeys]
      .map((ownerKey) => this.forward.get(ownerKey)?.fileName)
      .filter((fileName): fileName is string => fileName !== undefined);
  }

  clearEphemeral(): void {
    for (const [ownerKey, entry] of Array.from(this.forward)) {
      if (entry.ephemeral === true) {
        this.remove(ownerKey);
      }
    }
  }

  private upsertEntry(
    fileName: string,
    source: DiskAnalysisSourceMetadata,
    targetFileNames: Iterable<string>,
    refsFingerprint: string,
    ephemeral: boolean,
  ): void {
    const ownerKey = fileIdentityKeyFromFileName(fileName);
    this.remove(ownerKey);
    const targets = new Set([...targetFileNames].map(fileIdentityKeyFromFileName));
    this.forward.set(ownerKey, {
      fileName,
      source,
      targetFileNames: targets,
      refsFingerprint,
      ephemeral,
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
