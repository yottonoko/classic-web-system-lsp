import { describe, expect, it } from "vitest";
import type { AspNavigationGraphPayload } from "../src/protocol-types";
import {
  layoutNavigationGraphWithElk,
  navigationFlowElementsFromElk,
  navigationGraphToElkGraph,
} from "../src/webview/navigation-graph-layout";

function graph(): AspNavigationGraphPayload {
  return {
    scope: "workspace",
    nodes: ["A", "B", "C"].map((id) => ({ id, label: id, kind: "page", isRoot: id === "A" })),
    edges: [
      ["A", "B"],
      ["A", "B"],
      ["B", "C"],
      ["B", "A"],
      ["B", "B"],
    ].map(([source, target], i) => ({
      id: `edge-${i}`,
      source,
      target,
      kind: "htmlForm",
      method: i === 0 ? "GET" : "POST",
      confidence: "certain",
      ranges: [],
      evidence: [],
    })),
    stats: {
      documents: 3,
      nodes: 3,
      edges: 5,
      certain: 5,
      probable: 0,
      possible: 0,
      unknown: 0,
      external: 0,
    },
  };
}

const points = (path: string) =>
  Array.from(path.matchAll(/[ML] ([\d.e+-]+) ([\d.e+-]+)/g), (match) => ({
    x: Number(match[1]),
    y: Number(match[2]),
  }));

describe("navigation connection layout", () => {
  it("keeps a simple transition direct and places longer chains vertically", async () => {
    const payload = graph();
    payload.nodes = payload.nodes.slice(0, 2);
    payload.edges = payload.edges.slice(0, 1);
    const simple = await layoutNavigationGraphWithElk(payload);
    expect(simple.edges[0].data.points).toHaveLength(2);
    expect(simple.width).toBeLessThan(1000);
    const chain = graph();
    chain.edges = [chain.edges[0], chain.edges[2]];
    expect(navigationGraphToElkGraph(chain).layoutOptions?.["elk.direction"]).toBe("DOWN");
    const result = await layoutNavigationGraphWithElk(chain);
    const byId = new Map(result.nodes.map((node) => [node.id, node]));
    expect(byId.get("A")!.position.y).toBeLessThan(byId.get("B")!.position.y);
    expect(byId.get("B")!.position.y).toBeLessThan(byId.get("C")!.position.y);
    expect(result.width).toBeLessThan(1000);
  });
  it("routes parallel transitions and returns to separate handles at their actual ELK coordinates", async () => {
    const layout = await layoutNavigationGraphWithElk(graph());
    expect(layout.edges).toHaveLength(5);
    expect(new Set(layout.edges.map((edge) => edge.data?.path)).size).toBe(5);
    for (const edge of layout.edges) {
      expect(edge.data?.path).toBeTruthy();
      const path = points(edge.data!.path!);
      for (const [id, handleId, endpoint] of [
        [edge.source, edge.sourceHandle, path[0]],
        [edge.target, edge.targetHandle, path.at(-1)!],
      ] as const) {
        const node = layout.nodes.find((node) => node.id === id)!;
        const port = node.data.ports.find((port) => port.id === handleId)!;
        expect(port).toBeDefined();
        expect(endpoint.x).toBeCloseTo(node.position.x + port.x, 4);
        expect(endpoint.y).toBeCloseTo(node.position.y + port.y, 4);
      }
    }
  });

  it("keeps label boxes clear of pages and one another on branching, parallel and cyclic routes", async () => {
    const layout = await layoutNavigationGraphWithElk(graph());
    const labels = layout.edges.map((edge) => ({
      x: edge.data!.labelX! - 70,
      y: edge.data!.labelY! - 14,
      width: 140,
      height: 28,
    }));
    const boxes = layout.nodes.map((node) => ({
      ...node.position,
      width: Number(node.style!.width),
      height: Number(node.style!.height),
    }));
    const overlaps = (a: (typeof labels)[number], b: (typeof labels)[number]) =>
      a.x < b.x + b.width && a.x + a.width > b.x && a.y < b.y + b.height && a.y + a.height > b.y;
    for (const [index, label] of labels.entries()) {
      expect(Number.isFinite(label.x) && Number.isFinite(label.y)).toBe(true);
      expect(boxes.some((box) => overlaps(label, box))).toBe(false);
      expect(labels.slice(index + 1).some((other) => overlaps(label, other))).toBe(false);
    }
  });

  it("uses reserved label coordinates instead of a bend point", () => {
    const payload = graph();
    const input = navigationGraphToElkGraph(payload);
    const layout = navigationFlowElementsFromElk(payload, {
      ...input,
      edges: input.edges?.map((edge) => ({
        ...edge,
        labels: [{ x: 400, y: 100, width: 220, height: 28 }],
        sections: [
          {
            id: "s",
            startPoint: { x: 238, y: 44 },
            bendPoints: [{ x: 300, y: 44 }],
            endPoint: { x: 600, y: 180 },
          },
        ],
      })),
    });
    expect(layout.edges[0].data?.labelX).toBe(510);
    expect(layout.edges[0].data?.labelY).toBe(114);
  });

  it("grows busy pages without merging any connection ports", () => {
    const payload = graph();
    payload.edges = Array.from({ length: 24 }, (_, i) => ({
      ...payload.edges[0],
      id: `parallel-${i}`,
    }));
    const graphInput = navigationGraphToElkGraph(payload);
    const source = graphInput.children!.find((node) => node.id === "A")!;
    expect(source.height).toBeGreaterThan(88);
    expect(source.ports).toHaveLength(24);
    expect(new Set(source.ports!.map((port) => port.y)).size).toBe(24);
  });
});
