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
});
