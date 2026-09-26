import type { AspLspProgressTask } from "./protocol-types";

export interface ActiveProgressDocument {
  uri: string;
  version: number;
  label: string;
}

export type ReferenceProgressLocale = "en" | "ja";

const referenceProgressMessages = {
  en: {
    "references.count": "Reference count analysis",
    "references.waitIndex": "Waiting for workspace analysis",
    "references.restoreCache": "Restoring cached reference counts",
    "references.prepareQueries": "Preparing symbol queries",
    "references.countDocuments": "Counting references (files)",
    "references.countSegments": "Matching references (symbol/file pairs)",
    "references.countSymbols": "Finalizing reference counts (symbols)",
    "references.workspace": "Counting workspace references",
    "references.relatedIncludeTree": "Preparing related include files",
    "references.finalize": "Finalizing reference count",
  },
  ja: {
    "references.count": "参照数解析",
    "references.waitIndex": "ワークスペース解析の完了待ち",
    "references.restoreCache": "参照数キャッシュを復元中",
    "references.prepareQueries": "シンボルの検索条件を準備中",
    "references.countDocuments": "参照数を計算中（ファイル数）",
    "references.countSegments": "参照を照合中（シンボルとファイルの組数）",
    "references.countSymbols": "参照数を確定中（シンボル数）",
    "references.workspace": "ワークスペース参照数を解析中",
    "references.relatedIncludeTree": "関連 include ファイルを準備中（ファイル数）",
    "references.finalize": "参照数解析を仕上げ中",
  },
} as const;

export function isReferenceProgressTask(task: Pick<AspLspProgressTask, "label">): boolean {
  return task.label === "references.count" || task.label.startsWith("references.");
}

export function isTerminalProgressTask(task: Pick<AspLspProgressTask, "state">): boolean {
  return (
    task.state === "completed" ||
    task.state === "cancelled" ||
    task.state === "failed" ||
    task.state === "stale"
  );
}

/**
 * Keeps document reference progress exact: an old editor, an old version, or a
 * terminal task must never keep the current editor in a calculating state.
 */
export function progressTasksForActiveDocument<T extends AspLspProgressTask>(
  tasks: readonly T[],
  activeDocument: ActiveProgressDocument | undefined,
): T[] {
  return tasks.flatMap((task) => {
    if (!isReferenceProgressTask(task)) {
      return [task];
    }
    if (
      !activeDocument ||
      isTerminalProgressTask(task) ||
      task.documentUri !== activeDocument.uri ||
      task.documentVersion !== activeDocument.version
    ) {
      return [];
    }
    return [{ ...task, detail: task.detail || activeDocument.label }];
  });
}

export function referenceProgressLabel(
  label: string,
  locale: ReferenceProgressLocale,
): string | undefined {
  const messages = referenceProgressMessages[locale];
  return messages[label as keyof typeof messages];
}
