import path from "node:path";
import { describe, expect, it } from "vitest";
import { WorkspaceIncludeGraph } from "../src/include-graph-index";

describe("WorkspaceIncludeGraph", () => {
  it("tracks reverse candidates and swaps changed edges", () => {
    const graph = new WorkspaceIncludeGraph();
    const parent = path.resolve("/site/default.asp");
    const shared = path.resolve("/site/shared.inc");
    const other = path.resolve("/site/other.inc");

    graph.reset("settings");
    graph.upsert(parent, { fileName: parent, mtimeMs: 1, size: 10 }, [shared], "shared-refs");
    expect(graph.candidatesForTargets([shared])).toEqual([parent]);
    expect(graph.candidatesForTargets([other])).toEqual([]);

    graph.upsert(parent, { fileName: parent, mtimeMs: 2, size: 12 }, [other], "other-refs");
    expect(graph.candidatesForTargets([shared])).toEqual([]);
    expect(graph.candidatesForTargets([other])).toEqual([parent]);
    expect(graph.get(parent)?.refsFingerprint).toBe("other-refs");

    graph.delete(parent);
    expect(graph.candidatesForTargets([other])).toEqual([]);
  });

  it("round-trips through snapshots", () => {
    const graph = new WorkspaceIncludeGraph();
    const parent = path.resolve("/site/default.asp");
    const shared = path.resolve("/site/shared.inc");
    graph.reset("settings");
    graph.upsert(parent, { fileName: parent, mtimeMs: 1, size: 10 }, [shared], "shared-refs");

    const restored = new WorkspaceIncludeGraph();
    const snapshot = graph.snapshot();
    expect(snapshot?.settingsKey).toBe("settings");
    restored.restore(snapshot!);

    expect(restored.settingsKey).toBe("settings");
    expect(restored.candidatesForTargets([shared])).toEqual([parent]);
    expect(restored.get(parent)?.source.size).toBe(10);
  });

  it("returns transitive dependents without changing direct candidates", () => {
    const graph = new WorkspaceIncludeGraph();
    const root = path.resolve("/site/default.asp");
    const first = path.resolve("/site/includes/first.inc");
    const shared = path.resolve("/site/includes/shared.inc");
    graph.reset("settings");
    graph.upsert(root, { fileName: root, mtimeMs: 1, size: 10 }, [first], "root-refs");
    graph.upsert(first, { fileName: first, mtimeMs: 1, size: 10 }, [shared], "first-refs");

    expect(graph.candidatesForTargets([shared])).toEqual([first]);
    expect(graph.dependentFileNamesForTargets([shared], { transitive: true })).toEqual([
      first,
      root,
    ]);
    expect(graph.dependsOnAnyTarget(root, [shared], { transitive: true })).toBe(true);
    expect(graph.dependsOnAnyTarget(root, [shared])).toBe(false);
  });

  it("updates transitive dependents after edges are swapped", () => {
    const graph = new WorkspaceIncludeGraph();
    const root = path.resolve("/site/default.asp");
    const first = path.resolve("/site/includes/first.inc");
    const shared = path.resolve("/site/includes/shared.inc");
    const other = path.resolve("/site/includes/other.inc");
    graph.reset("settings");
    graph.upsert(root, { fileName: root, mtimeMs: 1, size: 10 }, [first], "root-refs");
    graph.upsert(first, { fileName: first, mtimeMs: 1, size: 10 }, [shared], "first-refs");
    expect(graph.dependentFileNamesForTargets([shared], { transitive: true })).toContain(root);

    graph.upsert(first, { fileName: first, mtimeMs: 2, size: 11 }, [other], "other-refs");
    expect(graph.dependentFileNamesForTargets([shared], { transitive: true })).toEqual([]);
    expect(graph.dependentFileNamesForTargets([other], { transitive: true })).toEqual([
      first,
      root,
    ]);
  });

  it("handles transitive cycles without looping", () => {
    const graph = new WorkspaceIncludeGraph();
    const a = path.resolve("/site/a.inc");
    const b = path.resolve("/site/b.inc");
    const c = path.resolve("/site/c.inc");
    graph.reset("settings");
    graph.upsert(a, { fileName: a, mtimeMs: 1, size: 10 }, [b], "a-refs");
    graph.upsert(b, { fileName: b, mtimeMs: 1, size: 10 }, [c], "b-refs");
    graph.upsert(c, { fileName: c, mtimeMs: 1, size: 10 }, [a], "c-refs");

    expect(new Set(graph.dependentFileNamesForTargets([c], { transitive: true }))).toEqual(
      new Set([a, b, c]),
    );
    expect(graph.dependsOnAnyTarget(a, [c], { transitive: true })).toBe(true);
  });

  it("keeps ephemeral open-document edges out of disk snapshots", () => {
    const graph = new WorkspaceIncludeGraph();
    const root = path.resolve("/site/default.asp");
    const open = path.resolve("/site/open.asp");
    const shared = path.resolve("/site/shared.inc");
    graph.reset("settings");
    graph.upsert(root, { fileName: root, mtimeMs: 1, size: 10 }, [shared], "root-refs");
    graph.upsertEphemeral(open, [shared], "open-refs");

    expect(new Set(graph.candidatesForTargets([shared]))).toEqual(new Set([root, open]));
    expect(graph.snapshot()?.entries.map((entry) => entry.fileName)).toEqual([root]);
    graph.clearEphemeral();
    expect(graph.candidatesForTargets([shared])).toEqual([root]);
  });
});
