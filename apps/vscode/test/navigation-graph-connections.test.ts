import { describe, expect, it } from "vitest";
import {
  navigationConnections,
  navigationLoopEdgeIds,
} from "../src/webview/navigation-graph-connections";

const nodes = ["A", "B", "C", "D"].map((id) => ({ id }));
const edges = [
  { id: "ab", source: "A", target: "B" },
  { id: "ab-post", source: "A", target: "B" },
  { id: "ba", source: "B", target: "A" },
  { id: "aa", source: "A", target: "A" },
  { id: "bc", source: "B", target: "C" },
  { id: "missing", source: "A", target: "missing" },
];

describe("navigation connections", () => {
  it("preserves selection across pointer movement and separates incoming, outgoing and self-return", () => {
    const result = navigationConnections(
      nodes,
      edges,
      { kind: "node", id: "A" },
      { kind: "node", id: "C" },
    );
    expect(result.active).toEqual({ kind: "node", id: "A" });
    expect([...result.nodes].sort()).toEqual(["A", "B"]);
    expect([...result.edges]).toEqual([
      ["ab", "outgoing"],
      ["ab-post", "outgoing"],
      ["ba", "incoming"],
      ["aa", "both"],
    ]);
  });

  it("selects exactly one parallel transition with both endpoints", () => {
    const result = navigationConnections(nodes, edges, { kind: "edge", id: "ab-post" });
    expect([...result.nodes].sort()).toEqual(["A", "B"]);
    expect([...result.edges]).toEqual([["ab-post", "selected"]]);
  });

  it("ignores filtered-out selections and dangling edges without dimming the whole graph", () => {
    const result = navigationConnections(nodes, edges, { kind: "edge", id: "missing" });
    expect(result.active).toBeUndefined();
    expect(result.nodes.size).toBe(0);
    expect(result.edges.size).toBe(0);
    const hovered = navigationConnections(
      nodes,
      edges,
      { kind: "node", id: "filtered" },
      { kind: "node", id: "D" },
    );
    expect([...hovered.nodes]).toEqual(["D"]);
    expect(hovered.edges.size).toBe(0);
  });
});

it("identifies self loops and cyclic returns but not parallel or shared destinations", () => {
  expect([...navigationLoopEdgeIds(nodes, edges)].sort()).toEqual(["aa", "ba"]);
  expect(
    navigationLoopEdgeIds(nodes, [
      { id: "ab", source: "A", target: "B" },
      { id: "ac", source: "A", target: "C" },
      { id: "bc", source: "B", target: "C" },
    ]).size,
  ).toBe(0);
  const chain = Array.from({ length: 12000 }, (_, i) => ({ id: String(i) }));
  const links = chain
    .slice(1)
    .map((n, i) => ({ id: String(i), source: chain[i].id, target: n.id }));
  links.push({ id: "return", source: "11999", target: "0" });
  expect([...navigationLoopEdgeIds(chain, links)]).toEqual(["return"]);
});
