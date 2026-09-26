import { describe, expect, it } from "vitest";
import type { AspNavigationEdge, AspNavigationNode } from "../src/protocol-types";
import {
  navigationSourcePath,
  navigationTreeEntries,
  visibleNavigationTreeEntries,
} from "../src/webview/navigation-graph-tree";

const node = (id: string, isRoot = false): AspNavigationNode => ({
  id,
  label: id,
  kind: "page",
  isRoot,
});
const edge = (id: string, source: string, target: string): AspNavigationEdge => ({
  id,
  source,
  target,
  kind: "htmlAnchor",
  confidence: "certain",
  ranges: [],
  evidence: [],
});

describe("navigation outline", () => {
  it("keeps cycles, shared destinations and disconnected pages visible exactly once per edge", () => {
    const entries = navigationTreeEntries({
      nodes: [node("D"), node("C"), node("B"), node("A", true)],
      edges: [
        edge("ab", "A", "B"),
        edge("ac", "A", "C"),
        edge("bc", "B", "C"),
        edge("ca", "C", "A"),
      ],
    });
    expect(entries.map(({ node, depth, reference }) => [node.id, depth, reference])).toEqual([
      ["A", 0, undefined],
      ["B", 1, undefined],
      ["C", 2, "shared"],
      ["C", 1, undefined],
      ["A", 2, "cycle"],
      ["D", 0, undefined],
    ]);
    expect(
      visibleNavigationTreeEntries(entries, new Set(["edge:ab"])).map((entry) => entry.node.id),
    ).toEqual(["A", "B", "C", "A", "D"]);
    expect(
      visibleNavigationTreeEntries(entries, new Set(["root:A"])).map((entry) => entry.node.id),
    ).toEqual(["A", "D"]);
  });

  it("handles very deep graphs iteratively and omits dangling edges", () => {
    const nodes = Array.from({ length: 12000 }, (_, i) => node(String(i), i === 0));
    const edges = nodes.slice(1).map((n, i) => edge(String(i), nodes[i].id, n.id));
    edges.push(edge("dangling", "0", "missing"));
    const entries = navigationTreeEntries({ nodes, edges });
    expect(entries).toHaveLength(nodes.length);
    expect(entries.at(-1)?.depth).toBe(11999);
    expect(visibleNavigationTreeEntries(entries, new Set(["root:0"]))).toHaveLength(1);
  });

  it("preserves distinct transitions to the same target", () => {
    const entries = navigationTreeEntries({
      nodes: [node("A", true), node("B")],
      edges: [edge("get", "A", "B"), edge("post", "A", "B")],
    });
    expect(entries).toHaveLength(3);
    expect(new Set(entries.map((entry) => entry.id)).size).toBe(3);
    expect(entries[2].reference).toBe("shared");
  });

  it("decodes Japanese and Windows source paths without rejecting malformed URIs", () => {
    expect(navigationSourcePath("file:///site/%E8%A1%A8%E7%A4%BA%20page.asp")).toBe(
      "/site/表示 page.asp",
    );
    expect(navigationSourcePath("file:///C:/site/default.asp")).toBe("C:/site/default.asp");
    expect(navigationSourcePath("%broken")).toBe("%broken");
  });
});
