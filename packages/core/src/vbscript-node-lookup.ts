import type { VbCstNode } from "./types";

export interface VbNodeLookup {
  nodes: VbCstNode[];
  maxEndByIndex: number[];
  preOrderIndexByNode: WeakMap<VbCstNode, number>;
}

export function buildVbNodeLookup(
  nodes: VbCstNode[],
  preOrderIndexByNode = preOrderIndexForNodes(nodes),
): VbNodeLookup {
  const sorted = nodesAreSortedByStart(nodes, preOrderIndexByNode)
    ? nodes
    : [...nodes].sort((left, right) =>
        compareNodeStartAndPreOrder(left, right, preOrderIndexByNode),
      );
  const maxEndByIndex: number[] = [];
  let maxEnd = Number.NEGATIVE_INFINITY;
  for (const [index, node] of sorted.entries()) {
    maxEnd = Math.max(maxEnd, node.end);
    maxEndByIndex[index] = maxEnd;
  }
  return { nodes: sorted, maxEndByIndex, preOrderIndexByNode };
}

function nodesAreSortedByStart(
  nodes: VbCstNode[],
  preOrderIndexByNode: WeakMap<VbCstNode, number>,
): boolean {
  for (let index = 1; index < nodes.length; index += 1) {
    if (compareNodeStartAndPreOrder(nodes[index - 1], nodes[index], preOrderIndexByNode) > 0) {
      return false;
    }
  }
  return true;
}

function compareNodeStartAndPreOrder(
  left: VbCstNode,
  right: VbCstNode,
  preOrderIndexByNode: WeakMap<VbCstNode, number>,
): number {
  return (
    left.start - right.start ||
    (preOrderIndexByNode.get(left) ?? 0) - (preOrderIndexByNode.get(right) ?? 0)
  );
}

export function preOrderIndexForNodes(nodes: VbCstNode[]): WeakMap<VbCstNode, number> {
  const indexByNode = new WeakMap<VbCstNode, number>();
  nodes.forEach((node, index) => indexByNode.set(node, index));
  return indexByNode;
}

export function smallestContainingVbNode(
  lookup: VbNodeLookup,
  offset: number,
): VbCstNode | undefined {
  let index = lastNodeStartingAtOrBefore(lookup.nodes, offset);
  let best: VbCstNode | undefined;
  let bestSpan = Number.POSITIVE_INFINITY;
  let bestPreOrderIndex = Number.POSITIVE_INFINITY;
  while (index >= 0 && lookup.maxEndByIndex[index] >= offset) {
    const node = lookup.nodes[index];
    if (offset >= node.start && offset <= node.end) {
      const span = node.end - node.start;
      const preOrderIndex = lookup.preOrderIndexByNode.get(node) ?? Number.POSITIVE_INFINITY;
      if (span < bestSpan || (span === bestSpan && preOrderIndex < bestPreOrderIndex)) {
        best = node;
        bestSpan = span;
        bestPreOrderIndex = preOrderIndex;
      }
      if (index === 0 || lookup.nodes[index - 1].start < node.start) {
        break;
      }
    }
    index -= 1;
  }
  return best;
}

function lastNodeStartingAtOrBefore(nodes: VbCstNode[], offset: number): number {
  let low = 0;
  let high = nodes.length - 1;
  let found = -1;
  while (low <= high) {
    const middle = Math.floor((low + high) / 2);
    if (nodes[middle].start <= offset) {
      found = middle;
      low = middle + 1;
    } else {
      high = middle - 1;
    }
  }
  return found;
}
