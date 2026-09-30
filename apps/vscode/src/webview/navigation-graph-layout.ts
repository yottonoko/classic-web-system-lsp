import ELK, { type ElkExtendedEdge, type ElkNode } from "elkjs/lib/elk.bundled.js";
import { navigationComponents } from "./navigation-graph-components";
import type {
  AspNavigationEdge,
  AspNavigationGraphPayload,
  AspNavigationNode,
} from "../protocol-types";

/** A distinct connection point for one transition. Coordinates are local to the node. */
export interface NavigationFlowPort {
  id: string;
  type: "source" | "target";
  x: number;
  y: number;
}

export interface NavigationFlowNodeData extends Record<string, unknown> {
  node: AspNavigationNode;
  locale?: "en" | "ja";
  /** Transitions into this page that the graph folded away because a shared include declares them. */
  sharedIncoming?: number;
  degree?: { incoming: number; outgoing: number };
  ports: NavigationFlowPort[];
  layer: number;
  revealIndex: number;
  selected?: boolean;
  searchHit?: boolean;
  dimmed?: boolean;
  revealDelayMs?: number;
  onSelect?: () => void;
  onHover?: () => void;
  onHoverEnd?: () => void;
}

export interface NavigationFlowEdgeData extends Record<string, unknown> {
  edge: AspNavigationEdge;
  locale?: "en" | "ja";
  label: string;
  path?: string;
  points?: { x: number; y: number }[];
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
  showLabel?: boolean;
  color?: string;
  revealDelayMs?: number;
  onSelect?: () => void;
  onHover?: () => void;
  onHoverEnd?: () => void;
}

/** Positioned page data shared by the layout engine and Solid renderer. */
export interface NavigationFlowNode {
  id: string;
  type: "navigationPage";
  position: { x: number; y: number };
  style: { width: number; height: number };
  selected?: boolean;
  className?: string;
  data: NavigationFlowNodeData;
}

/** Routed transition data independent of the rendering framework. */
export interface NavigationFlowEdge {
  id: string;
  source: string;
  target: string;
  sourceHandle: string;
  targetHandle: string;
  type: "navigationTransition";
  selected?: boolean;
  className?: string;
  zIndex?: number;
  data: NavigationFlowEdgeData;
}

/** Background bounds for independent groups in a workspace or folder graph. */
export interface NavigationFlowGroup {
  id: string;
  label: string;
  isolated: boolean;
  nodeCount: number;
  edgeCount: number;
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface NavigationFlowLayout {
  width: number;
  height: number;
  nodes: NavigationFlowNode[];
  edges: NavigationFlowEdge[];
  groups?: NavigationFlowGroup[];
}

const elk = new ELK();
const nodeWidth = 238;
const nodeHeight = 88;
const sourceHandleId = "source";
const targetHandleId = "target";
export const navigationGraphLayoutTimeoutMs = 2500;
const fallbackHorizontalGap = 180;
const fallbackVerticalGap = 48;

export function navigationGraphToElkGraph(payload: AspNavigationGraphPayload): ElkNode {
  const layers = navigationLayers(payload.nodes, payload.edges);
  const vertical = Math.max(0, ...layers.values()) >= 2;
  const sortedNodes = sortedNavigationNodes(payload.nodes);
  const nodeIds = new Set(sortedNodes.map((node) => node.id));
  const validEdges = [...payload.edges]
    .filter((edge) => nodeIds.has(edge.source) && nodeIds.has(edge.target))
    .sort((left, right) => left.id.localeCompare(right.id));
  const incoming = new Map<string, string[]>();
  const outgoing = new Map<string, string[]>();
  for (const edge of validEdges) {
    const sources = outgoing.get(edge.source) ?? [];
    sources.push(edge.id);
    outgoing.set(edge.source, sources);
    const targets = incoming.get(edge.target) ?? [];
    targets.push(edge.id);
    incoming.set(edge.target, targets);
  }
  const children = sortedNodes.map((node) =>
    navigationNodeToElkNode(
      node,
      incoming.get(node.id) ?? [],
      outgoing.get(node.id) ?? [],
      vertical,
    ),
  );
  const edges = validEdges.map(
    (edge): ElkExtendedEdge => ({
      id: edge.id,
      sources: [elkPortId(edge.source, `${sourceHandleId}:${edge.id}`)],
      targets: [elkPortId(edge.target, `${targetHandleId}:${edge.id}`)],
      labels: [{ text: edgeLabel(edge), width: 140, height: 24 }],
    }),
  );

  return {
    id: "navigation-graph-root",
    layoutOptions: {
      "elk.algorithm": "org.eclipse.elk.layered",
      "elk.direction": vertical ? "DOWN" : "RIGHT",
      "elk.edgeRouting": "ORTHOGONAL",
      "elk.aspectRatio": "1.6",
      "elk.layered.considerModelOrder.strategy": "NODES_AND_EDGES",
      "elk.layered.crossingMinimization.strategy": "LAYER_SWEEP",
      "elk.layered.nodePlacement.bk.fixedAlignment": "BALANCED",
      "elk.layered.spacing.edgeNodeBetweenLayers": "20",
      "elk.layered.spacing.nodeNodeBetweenLayers": "56",
      "elk.layered.spacing.edgeEdgeBetweenLayers": "16",
      "elk.spacing.edgeEdge": "16",
      "elk.spacing.nodeNode": "48",
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
  const components = navigationComponents(payload.nodes, payload.edges);
  const layouts: ElkNode[] = [];
  const deadline = Date.now() + navigationGraphLayoutTimeoutMs;
  // Independent groups get their own direction and routing. A small group must
  // not inherit empty layers from a longer, unrelated navigation chain.
  for (const component of components) {
    const part = { ...payload, nodes: component.nodes, edges: component.edges };
    const graph = navigationGraphToElkGraph(part);
    try {
      layouts.push(
        component.isolated || Date.now() >= deadline
          ? fallbackNavigationGraphLayout(part, graph)
          : await elkLayoutWithTimeout(graph, Math.max(1, deadline - Date.now())),
      );
    } catch {
      layouts.push(fallbackNavigationGraphLayout(part, graph));
    }
  }
  return navigationFlowElementsFromElk(payload, {
    id: "navigation-graph-root",
    children: layouts.flatMap((layout) => layout.children ?? []),
    edges: layouts.flatMap((layout) => layout.edges ?? []),
    width: Math.max(...layouts.map((layout) => layout.width ?? 0)),
    height: Math.max(...layouts.map((layout) => layout.height ?? 0)),
  });
}

async function elkLayoutWithTimeout(graph: ElkNode, timeoutMs: number): Promise<ElkNode> {
  let timeoutHandle: ReturnType<typeof setTimeout> | undefined;
  const timeout = new Promise<never>((_, reject) => {
    timeoutHandle = setTimeout(
      () => reject(new Error(`navigation graph layout exceeded ${timeoutMs}ms`)),
      timeoutMs,
    );
  });
  try {
    return await Promise.race([elk.layout(graph), timeout]);
  } finally {
    if (timeoutHandle !== undefined) {
      clearTimeout(timeoutHandle);
    }
  }
}

function fallbackNavigationGraphLayout(
  payload: AspNavigationGraphPayload,
  graph: ElkNode,
): ElkNode {
  const layers = navigationLayers(payload.nodes, payload.edges);
  const nextY = new Map<number, number>();
  let width = 0;
  let height = 0;
  const children = (graph.children ?? []).map((node) => {
    const layer = layers.get(node.id) ?? 0;
    const x = layer * (nodeWidth + fallbackHorizontalGap) + 44;
    const y = nextY.get(layer) ?? 44;
    nextY.set(layer, y + (node.height ?? nodeHeight) + fallbackVerticalGap);
    width = Math.max(width, x + nodeWidth + 44);
    height = Math.max(height, y + (node.height ?? nodeHeight) + 44);
    const incoming = (node.ports ?? []).filter((port) =>
      port.id.startsWith(`${node.id}:${targetHandleId}:`),
    );
    const outgoing = (node.ports ?? []).filter(
      (port) => !port.id.startsWith(`${node.id}:${targetHandleId}:`),
    );
    let sourceIndex = 0,
      targetIndex = 0;
    const ports = (node.ports ?? []).map((port) => {
      const target = port.id.startsWith(`${node.id}:${targetHandleId}:`);
      const count = target ? incoming.length : outgoing.length;
      const index = target ? ++targetIndex : ++sourceIndex;
      return {
        ...port,
        x: target ? 0 : (node.width ?? nodeWidth),
        y: ((node.height ?? nodeHeight) * index) / (count + 1),
        layoutOptions: { ...port.layoutOptions, "elk.port.side": target ? "WEST" : "EAST" },
      };
    });
    return { ...node, x, y, ports };
  });
  const byId = new Map(children.map((node) => [node.id, node]));
  const edges = payload.edges.flatMap((edge): ElkExtendedEdge[] => {
    const source = byId.get(edge.source),
      target = byId.get(edge.target);
    if (!source || !target) return [];
    const route =
      edge.source === edge.target
        ? fallbackLoop(edge, source)
        : fallbackConnection(edge, source, target);
    return [
      {
        id: edge.id,
        sources: [source.id],
        targets: [target.id],
        sections: [
          {
            id: `${edge.id}:fallback`,
            startPoint: route.points[0],
            endPoint: route.points.at(-1)!,
            bendPoints: route.points.slice(1, -1),
          },
        ],
        labels: [{ x: route.label.x - 70, y: route.label.y - 12, width: 140, height: 24 }],
      },
    ];
  });
  return { ...graph, width, height, children, edges };
}

export function navigationFlowElementsFromElk(
  payload: AspNavigationGraphPayload,
  layout: ElkNode,
): NavigationFlowLayout {
  const packed = packNavigationComponents(payload, layout);
  layout = packed.layout;
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
          style: { width: elkNode.width ?? nodeWidth, height: elkNode.height ?? nodeHeight },
          data: {
            node: originalNodeById.get(node.id) ?? node,
            ports: (elkNode.ports ?? []).map((port) => ({
              id: port.id.slice(node.id.length + 1),
              type: port.id.startsWith(`${node.id}:${targetHandleId}:`) ? "target" : "source",
              x: (port.x ?? 0) + (port.width ?? 0) / 2,
              y: (port.y ?? nodeHeight / 2) + (port.height ?? 0) / 2,
            })),
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
      const loop =
        !elkEdge && edge.source === edge.target
          ? fallbackLoop(edge, layoutNodeById.get(edge.source)!)
          : undefined;
      const fallback =
        loop ??
        fallbackConnection(
          edge,
          layoutNodeById.get(edge.source)!,
          layoutNodeById.get(edge.target)!,
        );
      const path = elkEdge ? (elkEdgePath(elkEdge) ?? fallback.path) : fallback.path;
      const labelPoint = elkEdge ? (elkEdgeLabelPoint(elkEdge) ?? fallback.label) : fallback.label;
      return {
        id: edge.id,
        source: edge.source,
        target: edge.target,
        sourceHandle: `${sourceHandleId}:${edge.id}`,
        targetHandle: `${targetHandleId}:${edge.id}`,
        type: "navigationTransition",
        data: {
          edge: originalEdgeById.get(edge.id) ?? edge,
          label: edgeLabel(edge),
          path,
          points:
            elkEdge?.sections?.flatMap((section) => [
              section.startPoint,
              ...(section.bendPoints ?? []),
              section.endPoint,
            ]) ?? fallback.points,
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
    groups: packed.groups,
  };
}

function packNavigationComponents(
  payload: AspNavigationGraphPayload,
  layout: ElkNode,
): {
  layout: ElkNode;
  groups: NavigationFlowGroup[];
} {
  const components = navigationComponents(payload.nodes, payload.edges);
  if (components.length <= 1 && !components[0]?.isolated) return { layout, groups: [] };
  const nodeById = new Map((layout.children ?? []).map((node) => [node.id, node]));
  const edgeById = new Map((layout.edges ?? []).map((edge) => [edge.id, edge]));
  const padding = 28;
  const heading = 52;
  const measured = components
    .map((component) => {
      let nodes = component.nodes.flatMap((node) =>
        nodeById.get(node.id) ? [nodeById.get(node.id)!] : [],
      );
      const edges = component.edges.flatMap((edge) =>
        edgeById.get(edge.id) ? [edgeById.get(edge.id)!] : [],
      );
      if (component.isolated) {
        const columns = Math.max(1, Math.min(6, Math.ceil(Math.sqrt(nodes.length))));
        nodes = nodes.map((node, index) => ({
          ...node,
          x: (index % columns) * (nodeWidth + 32),
          y: Math.floor(index / columns) * (nodeHeight + 32),
        }));
      }
      let minX = Infinity,
        minY = Infinity,
        maxX = -Infinity,
        maxY = -Infinity;
      const include = (x: number, y: number, width = 0, height = 0) => {
        minX = Math.min(minX, x);
        minY = Math.min(minY, y);
        maxX = Math.max(maxX, x + width);
        maxY = Math.max(maxY, y + height);
      };
      for (const node of nodes)
        include(node.x ?? 0, node.y ?? 0, node.width ?? nodeWidth, node.height ?? nodeHeight);
      for (const edge of edges) {
        for (const section of edge.sections ?? []) {
          for (const point of [section.startPoint, ...(section.bendPoints ?? []), section.endPoint])
            include(point.x, point.y);
        }
        for (const label of edge.labels ?? []) {
          if (label.x !== undefined && label.y !== undefined)
            include(label.x, label.y, label.width, label.height);
        }
      }
      return {
        component,
        nodes,
        edges,
        minX,
        minY,
        width: maxX - minX + padding * 2,
        height: maxY - minY + heading + padding,
      };
    })
    .filter((group) => group.nodes.length);
  const targetWidth = Math.max(
    720,
    ...measured.map((group) => group.width),
    Math.sqrt(measured.reduce((area, group) => area + group.width * group.height, 0) * 1.6),
  );
  const children: ElkNode[] = [];
  const edges: ElkExtendedEdge[] = [];
  const groups: NavigationFlowGroup[] = [];
  let x = 24,
    y = 24,
    rowHeight = 0,
    width = 0;
  for (const group of measured) {
    if (x > 24 && x + group.width > targetWidth + 24) {
      x = 24;
      y += rowHeight + 32;
      rowHeight = 0;
    }
    const dx = x + padding - group.minX,
      dy = y + heading - group.minY;
    children.push(
      ...group.nodes.map((node) => ({ ...node, x: (node.x ?? 0) + dx, y: (node.y ?? 0) + dy })),
    );
    const translate = (point: { x: number; y: number }) => ({ x: point.x + dx, y: point.y + dy });
    edges.push(
      ...group.edges.map((edge) => ({
        ...edge,
        sections: edge.sections?.map((section) => ({
          ...section,
          startPoint: translate(section.startPoint),
          endPoint: translate(section.endPoint),
          bendPoints: section.bendPoints?.map(translate),
        })),
        labels: edge.labels?.map((label) => ({
          ...label,
          x: label.x === undefined ? undefined : label.x + dx,
          y: label.y === undefined ? undefined : label.y + dy,
        })),
      })),
    );
    groups.push({
      id: group.component.id,
      label: group.component.label,
      isolated: group.component.isolated,
      nodeCount: group.nodes.length,
      edgeCount: group.component.edges.length,
      x,
      y,
      width: group.width,
      height: group.height,
    });
    width = Math.max(width, x + group.width + 24);
    x += group.width + 32;
    rowHeight = Math.max(rowHeight, group.height);
  }
  return { layout: { ...layout, children, edges, width, height: y + rowHeight + 24 }, groups };
}

function navigationNodeToElkNode(
  node: AspNavigationNode,
  incoming: string[],
  outgoing: string[],
  vertical: boolean,
): ElkNode {
  const height = Math.max(
    nodeHeight,
    Math.min(280, 28 + Math.max(incoming.length, outgoing.length) * 14),
  );
  const ports = (ids: string[], type: "source" | "target") =>
    ids.map((edgeId, index) => ({
      id: elkPortId(node.id, `${type}:${edgeId}`),
      x: vertical
        ? (nodeWidth * (index + 1)) / (ids.length + 1)
        : type === "target"
          ? 0
          : nodeWidth,
      y: vertical ? (type === "target" ? 0 : height) : (height * (index + 1)) / (ids.length + 1),
      width: 0,
      height: 0,
      layoutOptions: {
        "elk.port.side": vertical
          ? type === "target"
            ? "NORTH"
            : "SOUTH"
          : type === "target"
            ? "WEST"
            : "EAST",
      },
    }));
  const laneConstraint = node.isRoot
    ? "FIRST"
    : node.kind === "external" || node.kind === "unknown"
      ? "LAST"
      : undefined;
  return {
    id: node.id,
    width: nodeWidth,
    height,
    labels: [{ text: node.label, width: nodeWidth, height: 18 }],
    layoutOptions: {
      "elk.portConstraints": "FIXED_SIDE",
      ...(laneConstraint
        ? { "org.eclipse.elk.layered.layering.layerConstraint": laneConstraint }
        : {}),
    },
    ports: [...ports(incoming, "target"), ...ports(outgoing, "source")],
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
  const nodeIds = [...new Set(nodes.map((node) => node.id))].sort();
  const nodeIdSet = new Set(nodeIds);
  const adjacency = navigationAdjacency(nodeIds, edges, false);
  const reverseAdjacency = navigationAdjacency(nodeIds, edges, true);
  const finishOrder = navigationFinishOrder(nodeIds, adjacency);
  const componentByNode = new Map<string, number>();
  const components: string[][] = [];
  for (const start of finishOrder.reverse()) {
    if (componentByNode.has(start)) {
      continue;
    }
    const componentId = components.length;
    const component: string[] = [];
    const stack = [start];
    componentByNode.set(start, componentId);
    while (stack.length > 0) {
      const id = stack.pop()!;
      component.push(id);
      for (const source of reverseAdjacency.get(id) ?? []) {
        if (!componentByNode.has(source)) {
          componentByNode.set(source, componentId);
          stack.push(source);
        }
      }
    }
    components.push(component.sort());
  }

  const componentEdges = new Map<number, Set<number>>();
  const componentIncoming = new Map<number, number>();
  for (let component = 0; component < components.length; component += 1) {
    componentEdges.set(component, new Set());
    componentIncoming.set(component, 0);
  }
  for (const edge of edges) {
    if (!nodeIdSet.has(edge.source) || !nodeIdSet.has(edge.target)) {
      continue;
    }
    const source = componentByNode.get(edge.source)!;
    const target = componentByNode.get(edge.target)!;
    if (source === target || componentEdges.get(source)!.has(target)) {
      continue;
    }
    componentEdges.get(source)!.add(target);
    componentIncoming.set(target, (componentIncoming.get(target) ?? 0) + 1);
  }

  const componentLayers = new Map<number, number>();
  const queue = [...componentIncoming.entries()]
    .filter(([, incoming]) => incoming === 0)
    .map(([component]) => component)
    .sort((left, right) =>
      componentSortKey(components[left]).localeCompare(componentSortKey(components[right])),
    );
  for (const component of queue) {
    componentLayers.set(component, 0);
  }
  while (queue.length > 0) {
    const component = queue.shift()!;
    const nextComponents = [...(componentEdges.get(component) ?? [])].sort((left, right) =>
      componentSortKey(components[left]).localeCompare(componentSortKey(components[right])),
    );
    for (const target of nextComponents) {
      componentLayers.set(
        target,
        Math.max(componentLayers.get(target) ?? 0, (componentLayers.get(component) ?? 0) + 1),
      );
      const incoming = (componentIncoming.get(target) ?? 0) - 1;
      componentIncoming.set(target, incoming);
      if (incoming === 0) {
        queue.push(target);
        queue.sort((left, right) =>
          componentSortKey(components[left]).localeCompare(componentSortKey(components[right])),
        );
      }
    }
  }

  const layers = new Map<string, number>();
  for (const id of nodeIds) {
    layers.set(id, componentLayers.get(componentByNode.get(id)!) ?? 0);
  }
  const regularLayers = nodes
    .filter((node) => node.kind !== "external" && node.kind !== "unknown")
    .map((node) => layers.get(node.id) ?? 0);
  const maxLayer = Math.max(0, ...regularLayers);
  for (const node of nodes) {
    if (node.kind === "external" || node.kind === "unknown") {
      layers.set(node.id, maxLayer + 1);
    }
  }
  return layers;
}

function navigationAdjacency(
  nodeIds: string[],
  edges: AspNavigationEdge[],
  reverse: boolean,
): Map<string, string[]> {
  const nodeIdSet = new Set(nodeIds);
  const adjacency = new Map(nodeIds.map((id) => [id, new Set<string>()]));
  for (const edge of edges) {
    if (!nodeIdSet.has(edge.source) || !nodeIdSet.has(edge.target)) {
      continue;
    }
    const source = reverse ? edge.target : edge.source;
    const target = reverse ? edge.source : edge.target;
    adjacency.get(source)!.add(target);
  }
  return new Map(
    [...adjacency.entries()].map(([id, targets]) => [id, [...targets].sort()] as const),
  );
}

function navigationFinishOrder(nodeIds: string[], adjacency: Map<string, string[]>): string[] {
  const visited = new Set<string>();
  const order: string[] = [];
  for (const start of nodeIds) {
    if (visited.has(start)) {
      continue;
    }
    visited.add(start);
    const stack: Array<{ id: string; next: number }> = [{ id: start, next: 0 }];
    while (stack.length > 0) {
      const frame = stack.at(-1)!;
      const targets = adjacency.get(frame.id) ?? [];
      const target = targets[frame.next];
      if (target !== undefined) {
        frame.next += 1;
        if (!visited.has(target)) {
          visited.add(target);
          stack.push({ id: target, next: 0 });
        }
        continue;
      }
      order.push(frame.id);
      stack.pop();
    }
  }
  return order;
}

function componentSortKey(component: string[]): string {
  return component[0] ?? "";
}

function elkPortId(nodeId: string, handleId: string): string {
  return `${nodeId}:${handleId}`;
}

function elkEdgePath(edge: ElkExtendedEdge): string | undefined {
  const section = edge.sections?.[0];
  if (!section) {
    return undefined;
  }
  return roundedNavigationPath([
    section.startPoint,
    ...(section.bendPoints ?? []),
    section.endPoint,
  ]);
}

const edgeCornerRadius = 10;

/** Draws an orthogonal route with softened corners; every segment still ends at its route point. */
export function roundedNavigationPath(points: readonly { x: number; y: number }[]): string {
  if (points.length === 0) return "";
  const commands = [`M ${points[0].x} ${points[0].y}`];
  for (let index = 1; index < points.length - 1; index++) {
    const previous = points[index - 1];
    const corner = points[index];
    const next = points[index + 1];
    const inLength = Math.hypot(corner.x - previous.x, corner.y - previous.y);
    const outLength = Math.hypot(next.x - corner.x, next.y - corner.y);
    const radius = Math.min(edgeCornerRadius, inLength / 2, outLength / 2);
    if (radius < 0.5) {
      commands.push(`L ${corner.x} ${corner.y}`);
      continue;
    }
    const entry = {
      x: corner.x + ((previous.x - corner.x) * radius) / inLength,
      y: corner.y + ((previous.y - corner.y) * radius) / inLength,
    };
    const exit = {
      x: corner.x + ((next.x - corner.x) * radius) / outLength,
      y: corner.y + ((next.y - corner.y) * radius) / outLength,
    };
    commands.push(`L ${entry.x} ${entry.y}`, `Q ${corner.x} ${corner.y} ${exit.x} ${exit.y}`);
  }
  if (points.length > 1) {
    const last = points.at(-1)!;
    commands.push(`L ${last.x} ${last.y}`);
  }
  return commands.join(" ");
}

function elkEdgeLabelPoint(edge: ElkExtendedEdge): { x: number; y: number } | undefined {
  const label = edge.labels?.find((item) => item.x !== undefined && item.y !== undefined);
  if (label) {
    return { x: label.x! + (label.width ?? 0) / 2, y: label.y! + (label.height ?? 0) / 2 };
  }
  const section = edge.sections?.[0];
  if (!section) return undefined;
  const points = [section.startPoint, ...(section.bendPoints ?? []), section.endPoint];
  let length = -1;
  let middle = section.startPoint;
  for (let index = 1; index < points.length; index++) {
    const start = points[index - 1];
    const end = points[index];
    const distance = Math.abs(start.x - end.x) + Math.abs(start.y - end.y);
    if (distance > length) {
      length = distance;
      middle = { x: (start.x + end.x) / 2, y: (start.y + end.y) / 2 };
    }
  }
  return middle;
}

function fallbackConnection(
  edge: AspNavigationEdge,
  source: ElkNode,
  target: ElkNode,
): { path: string; label: { x: number; y: number }; points: { x: number; y: number }[] } {
  const from = source.ports?.find((port) => port.id === elkPortId(source.id, `source:${edge.id}`));
  const to = target.ports?.find((port) => port.id === elkPortId(target.id, `target:${edge.id}`));
  const sx = (source.x ?? 0) + (from?.x ?? nodeWidth),
    sy = (source.y ?? 0) + (from?.y ?? nodeHeight / 2);
  const tx = (target.x ?? 0) + (to?.x ?? 0),
    ty = (target.y ?? 0) + (to?.y ?? nodeHeight / 2);
  const middle = (sx + tx) / 2;
  const top = Math.min(source.y ?? 0, target.y ?? 0) - 28;
  const points =
    tx > sx
      ? [
          { x: sx, y: sy },
          { x: middle, y: sy },
          { x: middle, y: ty },
          { x: tx, y: ty },
        ]
      : [
          { x: sx, y: sy },
          { x: sx + 28, y: sy },
          { x: sx + 28, y: top },
          { x: tx - 28, y: top },
          { x: tx - 28, y: ty },
          { x: tx, y: ty },
        ];
  return {
    path: roundedNavigationPath(points),
    points,
    label: { x: middle, y: tx > sx ? (sy + ty) / 2 : top },
  };
}

function fallbackLoop(
  edge: AspNavigationEdge,
  node: ElkNode,
): {
  path: string;
  label: { x: number; y: number };
  points: { x: number; y: number }[];
} {
  const portY = (type: string) =>
    node.ports?.find((port) => port.id === elkPortId(node.id, `${type}:${edge.id}`))?.y ??
    nodeHeight / 2;
  const x = node.x ?? 0;
  const y = node.y ?? 0;
  const right = x + (node.width ?? nodeWidth);
  const top = y - 28;
  const points = [
    { x: right, y: y + portY("source") },
    { x: right + 28, y: y + portY("source") },
    { x: right + 28, y: top },
    { x: x - 28, y: top },
    { x: x - 28, y: y + portY("target") },
    { x, y: y + portY("target") },
  ];
  return {
    path: roundedNavigationPath(points),
    points,
    label: { x: x + nodeWidth / 2, y: top },
  };
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
