import { describe, expect, it } from "vitest";
import type { VbCstNode } from "../src";
import { buildVbNodeLookup, smallestContainingVbNode } from "../src/vbscript-node-lookup";

describe("smallestContainingVbNode", () => {
  it("selects the smallest containing node for nested spans", () => {
    const outer = vbNode("Procedure", 0, 100);
    const middle = vbNode("Procedure", 10, 90);
    const inner = vbNode("Procedure", 20, 30);
    const lookup = buildVbNodeLookup([outer, middle, inner]);

    expect(smallestContainingVbNode(lookup, 25)).toBe(inner);
    expect(smallestContainingVbNode(lookup, 50)).toBe(middle);
    expect(smallestContainingVbNode(lookup, 5)).toBe(outer);
    expect(smallestContainingVbNode(lookup, 101)).toBeUndefined();
  });

  it("keeps pre-order tie-breaking for same-span nodes", () => {
    const first = vbNode("Procedure", 0, 10);
    const second = vbNode("Property", 0, 10);
    const lookup = buildVbNodeLookup([first, second]);

    expect(smallestContainingVbNode(lookup, 5)).toBe(first);
  });
});

function vbNode(kind: VbCstNode["kind"], start: number, end: number): VbCstNode {
  return {
    kind,
    start,
    end,
    tokens: [],
    children: [],
  };
}
