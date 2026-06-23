import ELK, { type ElkExtendedEdge, type ElkNode } from "elkjs/lib/elk.bundled.js";
import { MarkerType, Position, type Edge, type Node } from "@xyflow/react";
import type {
  AspNavigationEdge,
  AspNavigationGraphPayload,
  AspNavigationNode,
} from "@asp-lsp/core";

export interface NavigationFlowNodeData extends Record<string, unknown> {
  node: AspNavigationNode;
  layer: number;
  revealIndex: number;
  selected?: boolean;
  searchHit?: boolean;
  dimmed?: boolean;
  revealDelayMs?: number;
  onHover?: () => void;
  onHoverEnd?: () => void;
}

export interface NavigationFlowEdgeData extends Record<string, unknown> {
  edge: AspNavigationEdge;
  label: string;
  path?: string;
  labelX?: number;
  labelY?: number;
  confidence: AspNavigationEdge["confidence"];
  edgeKind: AspNavigationEdge["kind"];
  method?: string;
  parameters: AspNavigationEdge["parameters"];
  revealIndex: number;
  selected?: boolean;
  searchHit?: boolean;
  dimmed?: boolean;
  uncertain?: boolean;
  revealDelayMs?: number;
  onSelect?: () => void;
  onHover?: () => void;
  onHoverEnd?: () => void;
}

export type NavigationFlowNode = Node<NavigationFlowNodeData, "navigationPage">;
export type NavigationFlowEdge = Edge<NavigationFlowEdgeData, "navigationTransition">;

export interface NavigationFlowLayout {
  width: number;
  height: number;
  nodes: NavigationFlowNode[];
  edges: NavigationFlowEdge[];
}

const elk = new ELK();
const nodeWidth = 238;
const nodeHeight = 88;
const sourceHandleId = "source";
const targetHandleId = "target";

export function navigationGraphToElkGraph(payload: AspNavigationGraphPayload): ElkNode {
  const sortedNodes = sortedNavigationNodes(payload.nodes);
  const nodeIds = new Set(sortedNodes.map((node) => node.id));
  const children = sortedNodes.map((node) => navigationNodeToElkNode(node));
  const edges = [...payload.edges]
    .filter((edge) => nodeIds.has(edge.source) && nodeIds.has(edge.target))
    .sort((left, right) => left.id.localeCompare(right.id))
    .map(
      (edge): ElkExtendedEdge => ({
        id: edge.id,
        sources: [elkPortId(edge.source, sourceHandleId)],
        targets: [elkPortId(edge.target, targetHandleId)],
        labels: [{ text: edgeLabel(edge), width: edgeLabel(edge).length * 7 + 24, height: 24 }],
      }),
    );

  return {
    id: "navigation-graph-root",
    layoutOptions: {
      "elk.algorithm": "org.eclipse.elk.layered",
      "elk.direction": "RIGHT",
      "elk.edgeRouting": "ORTHOGONAL",
      "elk.layered.considerModelOrder.strategy": "NODES_AND_EDGES",
      "elk.layered.crossingMinimization.strategy": "LAYER_SWEEP",
      "elk.layered.spacing.edgeNodeBetweenLayers": "42",
      "elk.layered.spacing.nodeNodeBetweenLayers": "116",
      "elk.spacing.nodeNode": "44",
      "elk.padding": "[top=44,left=44,bottom=44,right=44]",
    },
    children,
    edges,
  };
}

export async function layoutNavigationGraphWithElk(
  payload: AspNavigationGraphPayload,
): Promise<NavigationFlowLayout> {
  if (payload.nodes.length === 0) {
    return { width: 720, height: 520, nodes: [], edges: [] };
  }
  const graph = navigationGraphToElkGraph(payload);
  const layout = await elk.layout(graph);
  return navigationFlowElementsFromElk(payload, layout);
}

export function navigationFlowElementsFromElk(
  payload: AspNavigationGraphPayload,
  layout: ElkNode,
): NavigationFlowLayout {
  const originalNodeById = new Map(payload.nodes.map((node) => [node.id, node]));
  const originalEdgeById = new Map(payload.edges.map((edge) => [edge.id, edge]));
  const layers = navigationLayers(payload.nodes, payload.edges);
  const layoutNodeById = new Map((layout.children ?? []).map((node) => [node.id, node]));
  const layoutEdgeById = new Map((layout.edges ?? []).map((edge) => [edge.id, edge]));
  const nodes = sortedNavigationNodes(payload.nodes).flatMap(
    (node, index): NavigationFlowNode[] => {
      const elkNode = layoutNodeById.get(node.id);
      if (!elkNode) {
        return [];
      }
      return [
        {
          id: node.id,
          type: "navigationPage",
          position: { x: elkNode.x ?? 0, y: elkNode.y ?? 0 },
          sourcePosition: Position.Right,
          targetPosition: Position.Left,
          draggable: false,
          selectable: true,
          data: {
            node: originalNodeById.get(node.id) ?? node,
            layer: layers.get(node.id) ?? 0,
            revealIndex: index,
          },
        },
      ];
    },
  );
  const edges = [...payload.edges]
    .filter((edge) => layoutNodeById.has(edge.source) && layoutNodeById.has(edge.target))
    .sort((left, right) => left.id.localeCompare(right.id))
    .map((edge, index): NavigationFlowEdge => {
      const elkEdge = layoutEdgeById.get(edge.id);
      const path = elkEdge ? elkEdgePath(elkEdge) : undefined;
      const labelPoint = elkEdge ? elkEdgeLabelPoint(elkEdge) : undefined;
      return {
        id: edge.id,
        source: edge.source,
        target: edge.target,
        sourceHandle: sourceHandleId,
        targetHandle: targetHandleId,
        type: "navigationTransition",
        markerEnd: { type: MarkerType.ArrowClosed },
        selectable: true,
        data: {
          edge: originalEdgeById.get(edge.id) ?? edge,
          label: edgeLabel(edge),
          path,
          labelX: labelPoint?.x,
          labelY: labelPoint?.y,
          confidence: edge.confidence,
          edgeKind: edge.kind,
          method: edge.method,
          parameters: edge.parameters,
          revealIndex: index,
        },
      };
    });

  return {
    width: Math.max(720, (layout.width ?? 720) + 72),
    height: Math.max(520, (layout.height ?? 520) + 72),
    nodes,
    edges,
  };
}

function navigationNodeToElkNode(node: AspNavigationNode): ElkNode {
  const laneConstraint = node.isRoot
    ? "FIRST"
    : node.kind === "external" || node.kind === "unknown"
      ? "LAST"
      : undefined;
  return {
    id: node.id,
    width: nodeWidth,
    height: nodeHeight,
    labels: [{ text: node.label, width: nodeWidth, height: 18 }],
    layoutOptions: {
      "elk.portConstraints": "FIXED_SIDE",
      ...(laneConstraint
        ? { "org.eclipse.elk.layered.layering.layerConstraint": laneConstraint }
        : {}),
    },
    ports: [
      {
        id: elkPortId(node.id, targetHandleId),
        x: 0,
        y: nodeHeight / 2,
        width: 1,
        height: 1,
        layoutOptions: { "elk.port.side": "WEST" },
      },
      {
        id: elkPortId(node.id, sourceHandleId),
        x: nodeWidth,
        y: nodeHeight / 2,
        width: 1,
        height: 1,
        layoutOptions: { "elk.port.side": "EAST" },
      },
    ],
  };
}

function sortedNavigationNodes(nodes: AspNavigationNode[]): AspNavigationNode[] {
  return [...nodes].sort((left, right) => {
    if (left.isRoot !== right.isRoot) {
      return left.isRoot ? -1 : 1;
    }
    const kindOrder = nodeKindOrder(left.kind) - nodeKindOrder(right.kind);
    if (kindOrder !== 0) {
      return kindOrder;
    }
    return left.label.localeCompare(right.label);
  });
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
  const maxLayer = Math.max(0, ...layers.values());
  for (const node of nodes) {
    if (node.kind === "external" || node.kind === "unknown") {
      layers.set(node.id, maxLayer + 1);
    } else if (!layers.has(node.id)) {
      layers.set(node.id, 0);
    }
  }
  return layers;
}

function elkPortId(nodeId: string, handleId: string): string {
  return `${nodeId}:${handleId}`;
}

function elkEdgePath(edge: ElkExtendedEdge): string | undefined {
  const section = edge.sections?.[0];
  if (!section) {
    return undefined;
  }
  const points = [section.startPoint, ...(section.bendPoints ?? []), section.endPoint];
  return points.map((point, index) => `${index === 0 ? "M" : "L"} ${point.x} ${point.y}`).join(" ");
}

function elkEdgeLabelPoint(edge: ElkExtendedEdge): { x: number; y: number } | undefined {
  const section = edge.sections?.[0];
  if (!section) {
    return undefined;
  }
  const points = [section.startPoint, ...(section.bendPoints ?? []), section.endPoint];
  const middle = points[Math.floor(points.length / 2)];
  if (points.length % 2 === 1) {
    return middle;
  }
  const previous = points[points.length / 2 - 1];
  return { x: (previous.x + middle.x) / 2, y: (previous.y + middle.y) / 2 };
}

function edgeLabel(edge: AspNavigationEdge): string {
  return `${edgeKindLabel(edge.kind)}${edge.count && edge.count > 1 ? ` x${edge.count}` : ""}`;
}

function edgeKindLabel(kind: AspNavigationEdge["kind"]): string {
  return kind
    .replace(/^html/, "HTML ")
    .replace(/^javascript/, "JS ")
    .replace(/^server/, "Server ")
    .replace(/([a-z])([A-Z])/g, "$1 $2");
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
