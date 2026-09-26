import { afterEach, describe, expect, it, vi } from "vitest";
import type { AspNavigationGraphPayload } from "../src/protocol-types";
import {
  layoutNavigationGraphWithElk,
  navigationGraphLayoutTimeoutMs,
} from "../src/webview/navigation-graph-layout";

const layout = vi.hoisted(() => vi.fn());
vi.mock("elkjs/lib/elk.bundled.js", () => ({
  default: class {
    layout = layout;
  },
}));
afterEach(() => {
  vi.useRealTimers();
  layout.mockReset();
});
const payload: AspNavigationGraphPayload = {
  scope: "workspace",
  nodes: ["Z", "M", "A"].map((id) => ({ id, label: id, kind: "page" })),
  edges: [
    ["Z", "M"],
    ["M", "A"],
    ["M", "M"],
  ].map(([source, target], i) => ({
    id: String(i),
    source,
    target,
    kind: "htmlAnchor",
    confidence: "certain",
    ranges: [],
    evidence: [],
  })),
  stats: {
    documents: 3,
    nodes: 3,
    edges: 3,
    certain: 3,
    probable: 0,
    possible: 0,
    unknown: 0,
    external: 0,
  },
};

describe("navigation layout fallback", () => {
  it("preserves transition order and routes self-returns outside the page when ELK fails", async () => {
    layout.mockRejectedValueOnce(new Error("layout unavailable"));
    const result = await layoutNavigationGraphWithElk(payload);
    const byId = new Map(result.nodes.map((node) => [node.id, node]));
    expect(byId.get("Z")!.position.x).toBeLessThan(byId.get("M")!.position.x);
    expect(byId.get("M")!.position.x).toBeLessThan(byId.get("A")!.position.x);
    const loop = result.edges.find((edge) => edge.source === edge.target)!;
    expect(loop.data?.labelY).toBeLessThan(byId.get("M")!.position.y);
    expect(loop.data?.path).toContain(`L ${byId.get("M")!.position.x - 28}`);
    for (const edge of result.edges) {
      const source = byId.get(edge.source)!;
      const target = byId.get(edge.target)!;
      const start = edge.data.points![0],
        end = edge.data.points!.at(-1)!;
      expect(start.x).toBe(source.position.x + source.style.width);
      expect(end.x).toBe(target.position.x);
      expect(byId.get(edge.source)!.data.ports.some((port) => port.id === edge.sourceHandle)).toBe(
        true,
      );
      expect(byId.get(edge.target)!.data.ports.some((port) => port.id === edge.targetHandle)).toBe(
        true,
      );
    }
  });

  it("routes cycles around page interiors after a layout failure", async () => {
    layout.mockRejectedValueOnce(new Error("layout unavailable"));
    const cyclic = {
      ...payload,
      edges: [...payload.edges, { ...payload.edges[0], id: "back", source: "A", target: "Z" }],
    };
    const result = await layoutNavigationGraphWithElk(cyclic);
    for (const edge of result.edges) {
      const points = edge.data.points!;
      for (let index = 1; index < points.length; index++) {
        const a = points[index - 1],
          b = points[index];
        for (const node of result.nodes) {
          const left = node.position.x,
            right = left + node.style.width;
          const top = node.position.y,
            bottom = top + node.style.height;
          const crosses =
            a.x === b.x
              ? a.x > left && a.x < right && Math.max(a.y, b.y) > top && Math.min(a.y, b.y) < bottom
              : a.y > top &&
                a.y < bottom &&
                Math.max(a.x, b.x) > left &&
                Math.min(a.x, b.x) < right;
          expect(crosses).toBe(false);
        }
      }
    }
  });

  it("returns usable connections at the layout deadline", async () => {
    vi.useFakeTimers();
    layout.mockReturnValueOnce(new Promise(() => {}));
    const pending = layoutNavigationGraphWithElk(payload);
    await vi.advanceTimersByTimeAsync(navigationGraphLayoutTimeoutMs);
    const result = await pending;
    expect(result.nodes).toHaveLength(3);
    expect(result.edges).toHaveLength(3);
    expect(vi.getTimerCount()).toBe(0);
  });
});
