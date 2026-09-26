import { describe, expect, it } from "vitest";
import type { AspLspProgressTask } from "../src/protocol-types";
import { progressTasksForActiveDocument, referenceProgressLabel } from "../src/reference-progress";

const activeDocument = {
  uri: "file:///workspace/default.asp",
  version: 7,
  label: "default.asp",
};

function task(overrides: Partial<AspLspProgressTask> = {}): AspLspProgressTask {
  return {
    id: "references.batch-1",
    kind: "analyzing",
    label: "references.workspace",
    detail: "old-detail.asp",
    current: 4,
    total: 10,
    activeItems: ["SharedValue"],
    cancellable: false,
    state: "running",
    startedAt: 100,
    updatedAt: 200,
    documentUri: activeDocument.uri,
    documentVersion: activeDocument.version,
    ...overrides,
  };
}

describe("reference progress lifecycle", () => {
  it("keeps matching active-file progress with current, total, and file context", () => {
    expect(progressTasksForActiveDocument([task()], activeDocument)).toEqual([
      expect.objectContaining({
        current: 4,
        total: 10,
        detail: "old-detail.asp",
        documentUri: activeDocument.uri,
        documentVersion: 7,
      }),
    ]);
  });

  it("keeps the server's current reference item visible in the status bar", () => {
    expect(
      progressTasksForActiveDocument(
        [task({ detail: "include.inc", current: 32, total: 96 })],
        activeDocument,
      ),
    ).toEqual([expect.objectContaining({ detail: "include.inc", current: 32, total: 96 })]);
  });

  it("rejects progress from an old document, old version, or missing document identity", () => {
    expect(
      progressTasksForActiveDocument(
        [
          task({ id: "other", documentUri: "file:///workspace/include.inc" }),
          task({ id: "old", documentVersion: 6 }),
          task({ id: "missing-uri", documentUri: undefined }),
          task({ id: "missing-version", documentVersion: undefined }),
        ],
        activeDocument,
      ),
    ).toEqual([]);
  });

  it("clears reference progress on completion, cancellation, error, stale result, and editor switch", () => {
    for (const state of ["completed", "cancelled", "failed", "stale"] as const) {
      expect(progressTasksForActiveDocument([task({ state })], activeDocument)).toEqual([]);
    }
    expect(progressTasksForActiveDocument([task()], undefined)).toEqual([]);
  });

  it("keeps unrelated server progress when the active reference document changes", () => {
    const workspaceTask = task({
      id: "workspace-index",
      label: "workspace.index",
      documentUri: undefined,
      documentVersion: undefined,
    });
    expect(progressTasksForActiveDocument([workspaceTask], undefined)).toEqual([workspaceTask]);
  });
});

describe("reference progress localization", () => {
  it.each([
    ["references.count", "Reference count analysis", "参照数解析"],
    ["references.workspace", "Counting workspace references", "ワークスペース参照数を解析中"],
    [
      "references.relatedIncludeTree",
      "Preparing related include files",
      "関連 include ファイルを準備中（ファイル数）",
    ],
    ["references.finalize", "Finalizing reference count", "参照数解析を仕上げ中"],
  ])("localizes %s", (label, english, japanese) => {
    expect(referenceProgressLabel(label, "en")).toBe(english);
    expect(referenceProgressLabel(label, "ja")).toBe(japanese);
  });
});
