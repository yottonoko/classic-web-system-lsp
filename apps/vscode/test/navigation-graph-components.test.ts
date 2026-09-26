import { describe, expect, it } from "vitest";
import type {
  AspNavigationEdge,
  AspNavigationNode,
  AspNavigationGraphPayload,
} from "../src/protocol-types";
import { navigationComponents } from "../src/webview/navigation-graph-components";
import { layoutNavigationGraphWithElk } from "../src/webview/navigation-graph-layout";
const node = (id: string): AspNavigationNode => ({ id, label: id, kind: "page" });
const edge = (id: string, source: string, target: string): AspNavigationEdge => ({
  id,
  source,
  target,
  kind: "htmlAnchor",
  confidence: "certain",
  ranges: [],
  evidence: [],
});

describe("workspace navigation groups", () => {
  it("keeps cycles and parallel edges together, packs isolates separately and is input-order independent", () => {
    const nodes = ["A", "B", "C", "D", "E", "F"].map(node);
    const edges = [
      edge("ab", "A", "B"),
      edge("ba", "B", "A"),
      edge("ab2", "A", "B"),
      edge("cd", "C", "D"),
      edge("missing", "D", "missing"),
    ];
    const groups = navigationComponents(nodes, edges);
    expect(groups.map((group) => group.nodes.map((node) => node.id))).toEqual([
      ["A", "B"],
      ["C", "D"],
      ["E", "F"],
    ]);
    expect(groups.map((group) => group.edges.length)).toEqual([3, 1, 0]);
    expect(groups.at(-1)?.isolated).toBe(true);
    expect(
      navigationComponents([...nodes].reverse(), [...edges].reverse()).map((group) => group.id),
    ).toEqual(groups.map((group) => group.id));
  });

  it("partitions deep workspace graphs without recursive traversal", () => {
    const nodes = Array.from({ length: 12000 }, (_, i) => node(String(i)));
    const edges = nodes.slice(1).map((n, i) => edge(String(i), String(i), n.id));
    const groups = navigationComponents(nodes, edges);
    expect(groups).toHaveLength(1);
    expect(groups[0].nodes).toHaveLength(12000);
    expect(groups[0].edges).toHaveLength(11999);
  });

  it("packs disconnected workspace groups without overlap and keeps pages inside their group", async () => {
    const nodes = Array.from({ length: 24 }, (_, i) => node(String(i)));
    const edges = Array.from({ length: 8 }, (_, i) =>
      edge(String(i), String(i * 2), String(i * 2 + 1)),
    );
    const payload: AspNavigationGraphPayload = {
      scope: "workspace",
      nodes,
      edges,
      stats: {
        documents: 24,
        nodes: 24,
        edges: 8,
        certain: 8,
        probable: 0,
        possible: 0,
        unknown: 0,
        external: 0,
      },
    };
    const layout = await layoutNavigationGraphWithElk(payload);
    const groups = layout.groups!;
    expect(groups).toHaveLength(9);
    for (const [index, group] of groups.entries()) {
      for (const other of groups.slice(index + 1)) {
        expect(
          group.x < other.x + other.width &&
            group.x + group.width > other.x &&
            group.y < other.y + other.height &&
            group.y + group.height > other.y,
        ).toBe(false);
      }
    }
    for (const node of layout.nodes) {
      expect(
        groups.filter(
          (group) =>
            node.position.x >= group.x &&
            node.position.y >= group.y + 40 &&
            node.position.x + Number(node.style!.width) <= group.x + group.width &&
            node.position.y + Number(node.style!.height) <= group.y + group.height,
        ),
      ).toHaveLength(1);
    }
    expect(layout.width / layout.height).toBeLessThan(3);
  });
});
