import type {
  AspNavigationEdge,
  AspNavigationGraphPayload,
  AspNavigationNode,
} from "@asp-lsp/core";

export interface NavigationLayoutNode extends AspNavigationNode {
  x: number;
  y: number;
  width: number;
  height: number;
  layer: number;
}

export interface NavigationLayoutEdge extends AspNavigationEdge {
  path: string;
  labelX: number;
  labelY: number;
}

export interface NavigationGraphLayout {
  width: number;
  height: number;
  nodes: NavigationLayoutNode[];
  edges: NavigationLayoutEdge[];
}

const nodeWidth = 190;
const nodeHeight = 64;
const horizontalGap = 128;
const verticalGap = 34;
const margin = 56;

export function layoutNavigationGraph(payload: AspNavigationGraphPayload): NavigationGraphLayout {
  const nodes = [...payload.nodes].sort((left, right) => {
    if (left.isRoot !== right.isRoot) {
      return left.isRoot ? -1 : 1;
    }
    if (left.kind !== right.kind) {
      return nodeKindOrder(left.kind) - nodeKindOrder(right.kind);
    }
    return left.label.localeCompare(right.label);
  });
  const layers = navigationLayers(nodes, payload.edges);
  const maxLayer = Math.max(0, ...layers.values());
  for (const node of nodes) {
    if (node.kind === "external" || node.kind === "unknown") {
      layers.set(node.id, maxLayer + 1);
    }
  }
  const byLayer = new Map<number, AspNavigationNode[]>();
  for (const node of nodes) {
    const layer = layers.get(node.id) ?? 0;
    const items = byLayer.get(layer) ?? [];
    items.push(node);
    byLayer.set(layer, items);
  }
  const layoutNodes: NavigationLayoutNode[] = [];
  for (const [layer, items] of [...byLayer.entries()].sort((left, right) => left[0] - right[0])) {
    items.sort((left, right) => left.label.localeCompare(right.label));
    items.forEach((node, index) => {
      layoutNodes.push({
        ...node,
        x: margin + layer * (nodeWidth + horizontalGap),
        y: margin + index * (nodeHeight + verticalGap),
        width: nodeWidth,
        height: nodeHeight,
        layer,
      });
    });
  }
  const layoutNodeById = new Map(layoutNodes.map((node) => [node.id, node]));
  const groupedEdgeOffsets = edgeOffsets(payload.edges);
  const layoutEdges = payload.edges.flatMap((edge): NavigationLayoutEdge[] => {
    const source = layoutNodeById.get(edge.source);
    const target = layoutNodeById.get(edge.target);
    if (!source || !target) {
      return [];
    }
    const offset = groupedEdgeOffsets.get(edge.id) ?? 0;
    const path = edgePath(source, target, offset);
    const labelX = (source.x + source.width + target.x) / 2 + offset;
    const labelY = (source.y + source.height / 2 + target.y + target.height / 2) / 2 - 10;
    return [{ ...edge, path, labelX, labelY }];
  });
  const width =
    margin * 2 +
    (Math.max(0, ...layoutNodes.map((node) => node.layer)) + 1) * nodeWidth +
    Math.max(0, ...layoutNodes.map((node) => node.layer)) * horizontalGap;
  const maxLayerHeight = Math.max(
    nodeHeight,
    ...[...byLayer.values()].map(
      (items) => items.length * nodeHeight + Math.max(0, items.length - 1) * verticalGap,
    ),
  );
  return {
    width: Math.max(720, width),
    height: Math.max(520, margin * 2 + maxLayerHeight),
    nodes: layoutNodes,
    edges: layoutEdges,
  };
}

function navigationLayers(
  nodes: AspNavigationNode[],
  edges: AspNavigationEdge[],
): Map<string, number> {
  const adjacency = new Map<string, string[]>();
  const incoming = new Map<string, number>();
  for (const node of nodes) {
    adjacency.set(node.id, []);
    incoming.set(node.id, 0);
  }
  for (const edge of edges) {
    adjacency.get(edge.source)?.push(edge.target);
    incoming.set(edge.target, (incoming.get(edge.target) ?? 0) + 1);
  }
  const roots = nodes
    .filter((node) => node.isRoot || (incoming.get(node.id) ?? 0) === 0)
    .filter((node) => node.kind !== "external" && node.kind !== "unknown");
  const queue =
    roots.length > 0 ? roots.map((node) => node.id) : nodes.slice(0, 1).map((node) => node.id);
  const layers = new Map<string, number>();
  for (const id of queue) {
    layers.set(id, 0);
  }
  for (let cursor = 0; cursor < queue.length; cursor += 1) {
    const id = queue[cursor];
    const layer = layers.get(id) ?? 0;
    for (const target of adjacency.get(id) ?? []) {
      const nextLayer = Math.max(layers.get(target) ?? 0, layer + 1);
      if (nextLayer !== layers.get(target)) {
        layers.set(target, Math.min(nextLayer, 12));
        queue.push(target);
      }
    }
  }
  for (const node of nodes) {
    if (!layers.has(node.id)) {
      layers.set(node.id, node.kind === "external" || node.kind === "unknown" ? 12 : 0);
    }
  }
  return layers;
}

function edgeOffsets(edges: AspNavigationEdge[]): Map<string, number> {
  const groups = new Map<string, AspNavigationEdge[]>();
  for (const edge of edges) {
    const key = `${edge.source}|${edge.target}`;
    const items = groups.get(key) ?? [];
    items.push(edge);
    groups.set(key, items);
  }
  const offsets = new Map<string, number>();
  for (const items of groups.values()) {
    items.sort((left, right) => left.kind.localeCompare(right.kind));
    const center = (items.length - 1) / 2;
    items.forEach((edge, index) => offsets.set(edge.id, (index - center) * 18));
  }
  return offsets;
}

function edgePath(
  source: NavigationLayoutNode,
  target: NavigationLayoutNode,
  offset: number,
): string {
  const sourceX = source.x + source.width;
  const sourceY = source.y + source.height / 2 + offset;
  const targetX = target.x;
  const targetY = target.y + target.height / 2 + offset;
  if (target.x <= source.x) {
    const loopX = Math.max(source.x, target.x) + source.width + 42 + Math.abs(offset);
    return `M ${sourceX} ${sourceY} C ${loopX} ${sourceY - 70} ${loopX} ${targetY + 70} ${targetX} ${targetY}`;
  }
  const midX = (sourceX + targetX) / 2 + offset;
  return `M ${sourceX} ${sourceY} C ${midX} ${sourceY} ${midX} ${targetY} ${targetX} ${targetY}`;
}

function nodeKindOrder(kind: AspNavigationNode["kind"]): number {
  switch (kind) {
    case "page":
      return 0;
    case "fragment":
      return 1;
    case "external":
      return 2;
    case "unknown":
      return 3;
  }
}
