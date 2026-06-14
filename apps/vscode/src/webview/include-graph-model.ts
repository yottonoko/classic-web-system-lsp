import SpriteText from "three-spritetext";
import type { ForceGraphMethods as ForceGraph2DMethods } from "react-force-graph-2d";
import type { ForceGraphMethods as ForceGraph3DMethods } from "react-force-graph-3d";
import type {
  AspGraphLink,
  AspGraphLinkFilterCategory,
  AspGraphNode,
  AspGraphNodeCategory,
  AspGraphPayload,
  AspGraphRange,
} from "../include-graph-webview";

type ViewMode = "3d" | "2d";
type NodeColorCategory = AspGraphNodeCategory;
type LinkFilterCategory = AspGraphLinkFilterCategory;

type GraphNode = AspGraphNode & {
  color: string;
  category: NodeColorCategory;
  referenceCount: number;
  value: number;
  x?: number;
  y?: number;
  z?: number;
  vx?: number;
  vy?: number;
  vz?: number;
  fx?: number;
  fy?: number;
  fz?: number;
};

type GraphLink = Omit<AspGraphLink, "source" | "target"> & {
  source: string | GraphNode;
  target: string | GraphNode;
  color: string;
};

type GraphData = {
  nodes: GraphNode[];
  links: GraphLink[];
};

type LegacyImplicitGlobalNodeFields = {
  implicitLocal?: boolean;
  unresolvedGlobal?: boolean;
};

type Selection = { type: "node"; item: GraphNode } | { type: "link"; item: GraphLink } | undefined;
type GraphStatsTarget = { type: "node"; id: string } | { type: "link"; id: string };

type HighlightState = {
  activeNodeIds: Set<string>;
  activeLinkIds: Set<string>;
};

type CenteredSpriteText = SpriteText & {
  center: {
    y: number;
  };
};

interface PositionSyncEntry {
  id: string;
  sourceMode: ViewMode;
  x?: number;
  y?: number;
  z?: number;
  screenX?: number;
  screenY?: number;
  cameraDistance?: number;
}

interface PendingPositionSync {
  from: ViewMode;
  to: ViewMode;
  generation: number;
  entries: Map<string, PositionSyncEntry>;
}

interface PositionSyncTransform {
  centerX: number;
  centerY: number;
  scale: number;
}

interface GraphThemePalette {
  canvasBackground: string;
  mutedLink: string;
  mutedNode: string;
  nodeColors: Record<NodeColorCategory, string>;
  linkFilterColors: Record<LinkFilterCategory, string>;
}

const minimumNodeValue = 0.6;
const maximumNodeValue = 16;
const maximumNodeScaleReferenceCount = 144;
const graphFocusDurationMs = 900;
const graph3dMinimumFocusDistance = 70;
const graph3dLinkDistanceScale = 1.35;
const graph3dSyncSpan = 160;
const graphInitialNodeSpacing = 28;
const graphInitialNodeZSpacing = 12;
const graphGoldenAngle = Math.PI * (3 - Math.sqrt(5));
const graphChargeBaseStrength = 45;
const graphChargeValueStrength = 8;
const graphLinkDistanceBase = 36;
const graphLinkDistanceValueScale = 2.5;
const inspectorMinimumWidth = 260;
const inspectorMaximumWidth = 560;
const graphMinimumWidth = 360;
const paneResizeHandleWidth = 6;

const graph2dMinimumFocusZoom = 2.2;
const graph2dLinkFocusPadding = 120;

export function isFileLikeGraphNode(node: Pick<AspGraphNode, "kind">): boolean {
  return node.kind === "file" || node.kind === "missingInclude";
}

function positiveModulo(value: number, divisor: number): number {
  return ((value % divisor) + divisor) % divisor;
}

function isCallableUnresolvedRole(role: string | undefined): boolean {
  return role === "function" || role === "procedure" || role === "unknown";
}

function detailParts(...values: Array<string | undefined>): string[] {
  return values.filter((value): value is string => Boolean(value));
}

export function graphNodeMap(nodes: GraphNode[]): Map<string, GraphNode> {
  return new Map(nodes.map((node) => [node.id, node]));
}

function initialGraphNodePosition(
  index: number,
  totalCount: number,
  value: number,
): { x: number; y: number; z: number } {
  if (totalCount <= 1) {
    return { x: 0, y: 0, z: 0 };
  }
  const angle = index * graphGoldenAngle;
  const radius = Math.sqrt(index + 1) * graphInitialNodeSpacing + value;
  return {
    x: Math.cos(angle) * radius,
    y: Math.sin(angle) * radius,
    z: (positiveModulo(index, 7) - 3) * graphInitialNodeZSpacing,
  };
}

export function graphDataFor(
  payload: AspGraphPayload | undefined,
  themePalette: GraphThemePalette,
): GraphData {
  if (!payload) {
    return { nodes: [], links: [] };
  }
  const referenceCounts = graphReferenceCounts(payload.links);
  return {
    nodes: payload.nodes.map((node, index) => {
      const normalizedNode = normalizeGraphNode(node);
      const category = nodeCategoryForColor(normalizedNode);
      const referenceCount = referenceCounts.get(normalizedNode.id) ?? 0;
      const value = nodeValue(referenceCount);
      const position = initialGraphNodePosition(index, payload.nodes.length, value);
      return {
        ...normalizedNode,
        category,
        referenceCount,
        value,
        color: themePalette.nodeColors[category],
        x: position.x,
        y: position.y,
        z: position.z,
      };
    }),
    links: payload.links.map((link) => ({
      ...link,
      color: graphLinkColor(link, themePalette),
    })),
  };
}

function normalizeGraphNode(node: AspGraphNode): AspGraphNode {
  const legacy = node as AspGraphNode & LegacyImplicitGlobalNodeFields;
  if (node.declarationKind !== "variable") {
    return node;
  }
  if (
    node.implicitGlobal !== true &&
    legacy.implicitLocal !== true &&
    legacy.unresolvedGlobal !== true
  ) {
    return node;
  }
  const { implicitLocal: _implicitLocal, unresolvedGlobal: _unresolvedGlobal, ...rest } = legacy;
  return {
    ...rest,
    implicitGlobal: true,
    implicitGlobalCandidate:
      node.implicitGlobalCandidate === true ||
      legacy.implicitLocal === true ||
      legacy.unresolvedGlobal === true
        ? true
        : undefined,
  };
}

export function graphDataForRender(
  graphData: GraphData,
  positions: ReadonlyMap<string, PositionSyncEntry>,
  mode: ViewMode,
): GraphData {
  const transform =
    mode === "3d" ? positionSyncTransformFor3d(graphData.nodes, positions) : undefined;
  return {
    nodes: graphData.nodes.map((node) => {
      const position = positions.get(node.id);
      const renderPosition =
        mode === "3d" ? positionSyncPositionFor3d(position, transform) : position;
      return {
        ...node,
        x: renderPosition?.x ?? node.x,
        y: renderPosition?.y ?? node.y,
        z: renderPosition?.z ?? node.z,
      };
    }),
    links: graphData.links.map((link) => ({
      ...link,
      source: nodeIdForEndpoint(link.source),
      target: nodeIdForEndpoint(link.target),
    })),
  };
}

function positionSyncTransformFor3d(
  nodes: GraphNode[],
  positions: ReadonlyMap<string, PositionSyncEntry>,
): PositionSyncTransform | undefined {
  let minimumX = Infinity;
  let maximumX = -Infinity;
  let minimumY = Infinity;
  let maximumY = -Infinity;
  for (const node of nodes) {
    const position = positions.get(node.id);
    const x = finiteNumber(position?.x);
    const y = finiteNumber(position?.y);
    if (x === undefined || y === undefined) {
      continue;
    }
    minimumX = Math.min(minimumX, x);
    maximumX = Math.max(maximumX, x);
    minimumY = Math.min(minimumY, y);
    maximumY = Math.max(maximumY, y);
  }
  if (
    !Number.isFinite(minimumX) ||
    !Number.isFinite(maximumX) ||
    !Number.isFinite(minimumY) ||
    !Number.isFinite(maximumY)
  ) {
    return undefined;
  }
  const span = Math.max(maximumX - minimumX, maximumY - minimumY);
  return {
    centerX: (minimumX + maximumX) / 2,
    centerY: (minimumY + maximumY) / 2,
    scale: span > 0 ? graph3dSyncSpan / span : 1,
  };
}

function positionSyncPositionFor3d(
  position: PositionSyncEntry | undefined,
  transform: PositionSyncTransform | undefined,
): PositionSyncEntry | undefined {
  if (!position) {
    return undefined;
  }
  const x = finiteNumber(position?.x);
  const y = finiteNumber(position?.y);
  if (x === undefined || y === undefined) {
    return position;
  }
  if (!transform) {
    return position;
  }
  return {
    ...position,
    x: (x - transform.centerX) * transform.scale,
    y: (position.sourceMode === "2d" ? -1 : 1) * (y - transform.centerY) * transform.scale,
  };
}

function graphLinkColor(link: AspGraphLink, themePalette: GraphThemePalette): string {
  return themePalette.linkFilterColors[graphLinkFilterCategory(link)];
}

export function linkSwatchWidth(link: Pick<AspGraphLink, "kind" | "role">): number {
  return graphLinkFilterCategory(link) === "include" ? 4 : 2;
}

export function graphLinkFilterCategory(
  link: Pick<AspGraphLink, "kind" | "role">,
): LinkFilterCategory {
  return link.role === "member" ? "member" : link.kind;
}

export function filterGraphData(
  graphData: GraphData,
  hiddenNodeCategories: ReadonlySet<NodeColorCategory>,
  hiddenLinkCategories: ReadonlySet<LinkFilterCategory>,
  hideSingleNodes: boolean,
  hideUnreferencedGlobalSymbols: boolean,
): GraphData {
  const hasPayloadRoot = graphData.nodes.some((node) => node.isRoot);
  const canHideUnreferencedGlobalSymbols = hideUnreferencedGlobalSymbols && hasPayloadRoot;
  const retainedGlobalNodeIds = canHideUnreferencedGlobalSymbols
    ? retainedGlobalSymbolNodeIds(graphData.nodes, graphData.links)
    : undefined;
  let visibleNodes = graphData.nodes.filter(
    (node) =>
      !hiddenNodeCategories.has(node.category) &&
      (!canHideUnreferencedGlobalSymbols ||
        !isHideableGlobalSymbolNode(node) ||
        retainedGlobalNodeIds?.has(node.id) === true),
  );
  let visibleNodeIds = new Set(visibleNodes.map((node) => node.id));
  let visibleLinks = graphData.links.filter((link) => {
    const sourceId = nodeIdForEndpoint(link.source);
    const targetId = nodeIdForEndpoint(link.target);
    return (
      visibleNodeIds.has(sourceId) &&
      visibleNodeIds.has(targetId) &&
      !hiddenLinkCategories.has(graphLinkFilterCategory(link))
    );
  });
  if (hideSingleNodes) {
    const connectedNodeIds = connectedNodeIdsFor(visibleLinks);
    visibleNodes = visibleNodes.filter(
      (node) =>
        node.isRoot ||
        connectedNodeIds.has(node.id) ||
        retainedGlobalNodeIds?.has(node.id) === true ||
        (!hasPayloadRoot && isFileLikeGraphNode(node)),
    );
    visibleNodeIds = new Set(visibleNodes.map((node) => node.id));
    visibleLinks = visibleLinks.filter(
      (link) =>
        visibleNodeIds.has(nodeIdForEndpoint(link.source)) &&
        visibleNodeIds.has(nodeIdForEndpoint(link.target)),
    );
  }
  const referenceCounts = graphReferenceCountsForGraphLinks(visibleLinks);
  for (const node of visibleNodes) {
    const referenceCount = referenceCounts.get(node.id) ?? 0;
    node.value = nodeValue(referenceCount);
  }
  return {
    nodes: visibleNodes,
    links: visibleLinks,
  };
}

function retainedGlobalSymbolNodeIds(nodes: GraphNode[], links: GraphLink[]): Set<string> {
  const nodesById = graphNodeMap(nodes);
  const retainedNodeIds = new Set<string>();
  const rootNodes = nodes.filter((node) => node.isRoot);
  const rootNodeIds = new Set(rootNodes.map((node) => node.id));
  const rootUris = new Set(
    rootNodes.map((node) => node.uri).filter((uri): uri is string => Boolean(uri)),
  );
  for (const node of nodes) {
    if (node.uri && rootUris.has(node.uri) && isHideableGlobalSymbolNode(node)) {
      retainedNodeIds.add(node.id);
    }
  }
  for (const link of links) {
    const sourceId = nodeIdForEndpoint(link.source);
    const targetId = nodeIdForEndpoint(link.target);
    const sourceNode = nodesById.get(sourceId);
    const targetNode = nodesById.get(targetId);
    if (rootNodeIds.has(sourceId) && targetNode && isHideableGlobalSymbolNode(targetNode)) {
      retainedNodeIds.add(targetNode.id);
    }
    if (rootNodeIds.has(targetId) && sourceNode && isHideableGlobalSymbolNode(sourceNode)) {
      retainedNodeIds.add(sourceNode.id);
    }
    if (
      !isSymbolReferenceGraphLink(link) ||
      !sourceNode?.uri ||
      !targetNode?.uri ||
      sourceNode.uri === targetNode.uri
    ) {
      continue;
    }
    if (isHideableGlobalSymbolNode(targetNode)) {
      retainedNodeIds.add(targetNode.id);
    }
    if (isHideableGlobalSymbolNode(sourceNode)) {
      retainedNodeIds.add(sourceNode.id);
    }
  }
  return retainedNodeIds;
}

function isSymbolReferenceGraphLink(link: GraphLink): boolean {
  return link.kind === "references" || link.kind === "assignments" || link.kind === "calls";
}

function isHideableGlobalSymbolNode(node: GraphNode): boolean {
  if (node.kind !== "vbDeclaration" || node.origin !== "source") {
    return false;
  }
  switch (node.category) {
    case "function":
    case "sub":
    case "class":
    case "globalVariable":
    case "implicitGlobalVariable":
    case "globalConstant":
      return true;
    default:
      return false;
  }
}

export function isImplicitGlobalVariableNode(
  node: Pick<AspGraphNode, "declarationKind" | "implicitGlobal">,
): boolean {
  return node.declarationKind === "variable" && node.implicitGlobal === true;
}

function connectedNodeIdsFor(links: GraphLink[]): Set<string> {
  const ids = new Set<string>();
  for (const link of links) {
    ids.add(nodeIdForEndpoint(link.source));
    ids.add(nodeIdForEndpoint(link.target));
  }
  return ids;
}

export function graphStatsFor(graphData: GraphData): AspGraphPayload["stats"] {
  const stats: AspGraphPayload["stats"] = {
    files: 0,
    declarations: 0,
    references: 0,
    assignments: 0,
    calls: 0,
    unresolvedReferences: 0,
    includes: 0,
    missingIncludes: 0,
    nodes: graphData.nodes.length,
    links: graphData.links.length,
  };
  for (const node of graphData.nodes) {
    if (isFileLikeGraphNode(node)) {
      stats.files += 1;
    } else if (node.kind === "vbDeclaration") {
      stats.declarations += 1;
    }
  }
  for (const link of graphData.links) {
    if (link.kind === "include") {
      stats.includes += 1;
      if (link.include?.exists === false) {
        stats.missingIncludes += 1;
      }
    } else if (link.kind === "references") {
      stats.references += 1;
    } else if (link.kind === "assignments") {
      stats.assignments += 1;
    } else if (link.kind === "calls") {
      stats.calls += 1;
    } else if (link.kind === "unresolvedReference") {
      stats.unresolvedReferences += 1;
    }
  }
  return stats;
}

function graphReferenceCounts(links: AspGraphLink[]): Map<string, number> {
  const counts = new Map<string, number>();
  for (const link of links) {
    if (
      link.kind !== "include" &&
      link.kind !== "references" &&
      link.kind !== "assignments" &&
      link.kind !== "calls" &&
      link.kind !== "unresolvedReference"
    ) {
      continue;
    }
    counts.set(link.target, (counts.get(link.target) ?? 0) + link.count);
  }
  return counts;
}

function graphReferenceCountsForGraphLinks(links: GraphLink[]): Map<string, number> {
  const counts = new Map<string, number>();
  for (const link of links) {
    if (
      link.kind !== "include" &&
      link.kind !== "references" &&
      link.kind !== "assignments" &&
      link.kind !== "calls" &&
      link.kind !== "unresolvedReference"
    ) {
      continue;
    }
    const targetId = nodeIdForEndpoint(link.target);
    counts.set(targetId, (counts.get(targetId) ?? 0) + link.count);
  }
  return counts;
}

export function toggledSet<T>(set: ReadonlySet<T>, value: T): Set<T> {
  const nextSet = new Set(set);
  if (nextSet.has(value)) {
    nextSet.delete(value);
  } else {
    nextSet.add(value);
  }
  return nextSet;
}

export function capturePositionSyncEntries(
  mode: ViewMode,
  nodes: GraphNode[],
  graph2d: ForceGraph2DMethods<GraphNode, GraphLink> | undefined,
  graph3d: ForceGraph3DMethods<GraphNode, GraphLink> | undefined,
  store: Map<string, PositionSyncEntry>,
): Map<string, PositionSyncEntry> {
  const entries = new Map<string, PositionSyncEntry>();
  for (const node of nodes) {
    const previous = store.get(node.id);
    const captured =
      mode === "3d"
        ? capturePositionSyncEntry3d(node, graph3d, previous)
        : capturePositionSyncEntry2d(node, graph2d, previous);
    if (captured) {
      store.set(node.id, captured);
      entries.set(node.id, { ...captured });
    } else if (previous) {
      entries.set(node.id, { ...previous });
    }
  }
  return entries;
}

function capturePositionSyncEntry3d(
  node: GraphNode,
  graph3d: ForceGraph3DMethods<GraphNode, GraphLink> | undefined,
  previous: PositionSyncEntry | undefined,
): PositionSyncEntry | undefined {
  const x = finiteNumber(node.x) ?? previous?.x;
  const y = finiteNumber(node.y) ?? previous?.y;
  const z = finiteNumber(node.z) ?? previous?.z ?? 0;
  if (x === undefined || y === undefined) {
    return previous;
  }
  const screen = graph3dScreenCoords(graph3d, x, y, z);
  const cameraDistance = graph3dCameraDistanceToPoint(graph3d, x, y, z);
  return {
    id: node.id,
    sourceMode: "3d",
    x,
    y,
    z,
    screenX: finiteNumber(screen?.x) ?? previous?.screenX,
    screenY: finiteNumber(screen?.y) ?? previous?.screenY,
    cameraDistance: cameraDistance ?? previous?.cameraDistance,
  };
}

function capturePositionSyncEntry2d(
  node: GraphNode,
  graph2d: ForceGraph2DMethods<GraphNode, GraphLink> | undefined,
  previous: PositionSyncEntry | undefined,
): PositionSyncEntry | undefined {
  const x = finiteNumber(node.x) ?? previous?.x;
  const y = finiteNumber(node.y) ?? previous?.y;
  if (x === undefined || y === undefined) {
    return previous;
  }
  const screen = graph2dScreenCoords(graph2d, x, y);
  return {
    id: node.id,
    sourceMode: "2d",
    x,
    y,
    z: finiteNumber(node.z) ?? previous?.z ?? 0,
    screenX: finiteNumber(screen?.x) ?? previous?.screenX,
    screenY: finiteNumber(screen?.y) ?? previous?.screenY,
    cameraDistance: previous?.cameraDistance,
  };
}

export function applyPositionSyncTo2d(
  pending: PendingPositionSync,
  nodes: GraphNode[],
  graph2d: ForceGraph2DMethods<GraphNode, GraphLink> | undefined,
): Set<string> {
  const pinnedNodeIds = new Set<string>();
  for (const node of nodes) {
    const entry = pending.entries.get(node.id);
    if (!entry) {
      continue;
    }
    const projected =
      pending.from === "3d"
        ? graph2dCoordsFromScreen(graph2d, entry.screenX, entry.screenY)
        : undefined;
    const x = finiteNumber(projected?.x) ?? entry.x;
    const y = finiteNumber(projected?.y) ?? entry.y;
    if (x === undefined || y === undefined) {
      continue;
    }
    node.x = x;
    node.y = y;
    node.z = entry.z;
    node.vx = 0;
    node.vy = 0;
    node.vz = 0;
    node.fx = x;
    node.fy = y;
    delete node.fz;
    pinnedNodeIds.add(node.id);
  }
  return pinnedNodeIds;
}

export function applyPositionSyncTo3d(
  pending: PendingPositionSync,
  nodes: GraphNode[],
  graph3d: ForceGraph3DMethods<GraphNode, GraphLink> | undefined,
): Set<string> {
  const pinnedNodeIds = new Set<string>();
  const transform = positionSyncTransformFor3dFromEntries(nodes, pending.entries);
  const fallbackDistance = graph3dFallbackCameraDistance(graph3d, nodes);
  for (const node of nodes) {
    const capturedEntry = pending.entries.get(node.id);
    const entry = positionSyncPositionFor3d(capturedEntry, transform);
    if (!entry) {
      continue;
    }
    const projected =
      pending.from === "2d"
        ? graph3dCoordsFromScreen(
            graph3d,
            capturedEntry?.screenX,
            capturedEntry?.screenY,
            finiteNumber(capturedEntry?.cameraDistance) ?? fallbackDistance,
          )
        : undefined;
    const x = finiteNumber(projected?.x) ?? finiteNumber(entry.x);
    const y = finiteNumber(projected?.y) ?? finiteNumber(entry.y);
    if (x === undefined || y === undefined) {
      continue;
    }
    const z = finiteNumber(projected?.z) ?? finiteNumber(entry.z) ?? 0;
    node.x = x;
    node.y = y;
    node.z = z;
    node.vx = 0;
    node.vy = 0;
    node.vz = 0;
    node.fx = x;
    node.fy = y;
    node.fz = z;
    pinnedNodeIds.add(node.id);
  }
  return pinnedNodeIds;
}

function positionSyncTransformFor3dFromEntries(
  nodes: GraphNode[],
  entries: ReadonlyMap<string, PositionSyncEntry>,
): PositionSyncTransform | undefined {
  return positionSyncTransformFor3d(nodes, entries);
}

export function releasePositionSyncPins(
  nodes: GraphNode[],
  pinnedNodeIds: ReadonlySet<string>,
): void {
  for (const node of nodes) {
    if (!pinnedNodeIds.has(node.id)) {
      continue;
    }
    delete node.fx;
    delete node.fy;
    delete node.fz;
  }
}

export function selectionForStatsTarget(target: GraphStatsTarget, graphData: GraphData): Selection {
  if (target.type === "node") {
    const node = graphData.nodes.find((candidate) => candidate.id === target.id);
    return node ? { type: "node", item: node } : undefined;
  }
  const link = graphData.links.find((candidate) => candidate.id === target.id);
  return link ? { type: "link", item: link } : undefined;
}

export function graphStatsTargetForRange(
  graphData: GraphData,
  range: AspGraphRange,
  targetUri: string | undefined,
): GraphStatsTarget | undefined {
  const nodeTarget = bestGraphNodeForRange(graphData.nodes, range, targetUri);
  if (nodeTarget) {
    return { type: "node", id: nodeTarget.id };
  }
  const linkTarget = bestGraphLinkForRange(graphData.links, range, targetUri);
  return linkTarget ? { type: "link", id: linkTarget.id } : undefined;
}

function bestGraphNodeForRange(
  nodes: GraphNode[],
  range: AspGraphRange,
  targetUri: string | undefined,
): GraphNode | undefined {
  let best: { node: GraphNode; score: number } | undefined;
  for (const node of nodes) {
    if (!node.range || (targetUri && node.uri !== targetUri)) {
      continue;
    }
    const score = graphRangeMatchScore(node.range, range);
    if (score === undefined) {
      continue;
    }
    if (!best || score < best.score) {
      best = { node, score };
    }
  }
  return best?.node;
}

function bestGraphLinkForRange(
  links: GraphLink[],
  range: AspGraphRange,
  targetUri: string | undefined,
): GraphLink | undefined {
  let best: { link: GraphLink; score: number } | undefined;
  for (const link of links) {
    for (const occurrence of link.ranges) {
      if (targetUri && occurrence.uri !== targetUri) {
        continue;
      }
      const score = graphRangeMatchScore(occurrence.range, range);
      if (score === undefined) {
        continue;
      }
      if (!best || score < best.score) {
        best = { link, score };
      }
    }
  }
  return best?.link;
}

function graphRangeMatchScore(candidate: AspGraphRange, target: AspGraphRange): number | undefined {
  if (graphRangeEquals(candidate, target)) {
    return 0;
  }
  if (graphRangeContains(candidate, target) || graphRangeContains(target, candidate)) {
    return 10 + graphRangeLineSpan(candidate);
  }
  if (graphRangesOverlap(candidate, target)) {
    return 100 + graphRangeLineSpan(candidate);
  }
  return undefined;
}

function graphRangeEquals(left: AspGraphRange, right: AspGraphRange): boolean {
  return (
    left.start.line === right.start.line &&
    left.start.character === right.start.character &&
    left.end.line === right.end.line &&
    left.end.character === right.end.character
  );
}

function graphRangeContains(container: AspGraphRange, item: AspGraphRange): boolean {
  return (
    compareGraphPositions(container.start, item.start) <= 0 &&
    compareGraphPositions(container.end, item.end) >= 0
  );
}

function graphRangesOverlap(left: AspGraphRange, right: AspGraphRange): boolean {
  return (
    compareGraphPositions(left.start, right.end) <= 0 &&
    compareGraphPositions(right.start, left.end) <= 0
  );
}

function compareGraphPositions(
  left: AspGraphRange["start"],
  right: AspGraphRange["start"],
): number {
  return left.line === right.line ? left.character - right.character : left.line - right.line;
}

function graphRangeLineSpan(range: AspGraphRange): number {
  return Math.max(0, range.end.line - range.start.line);
}

export function isGraphRange(value: unknown): value is AspGraphRange {
  if (!value || typeof value !== "object") {
    return false;
  }
  const candidate = value as Partial<AspGraphRange>;
  return isGraphPosition(candidate.start) && isGraphPosition(candidate.end);
}

function isGraphPosition(value: unknown): value is AspGraphRange["start"] {
  return (
    Boolean(value) &&
    typeof value === "object" &&
    Number.isInteger((value as { line?: unknown }).line) &&
    Number.isInteger((value as { character?: unknown }).character)
  );
}

export function focusGraphTarget(
  target: GraphStatsTarget,
  mode: ViewMode,
  graphData: GraphData,
  graph2d: ForceGraph2DMethods<GraphNode, GraphLink> | undefined,
  graph3d: ForceGraph3DMethods<GraphNode, GraphLink> | undefined,
): void {
  if (mode === "3d") {
    focusGraphTarget3d(target, graphData, graph3d);
  } else {
    focusGraphTarget2d(target, graphData, graph2d);
  }
}

function focusGraphTarget2d(
  target: GraphStatsTarget,
  graphData: GraphData,
  graph2d: ForceGraph2DMethods<GraphNode, GraphLink> | undefined,
): void {
  if (!graph2d) {
    return;
  }
  if (target.type === "node") {
    const node = graphData.nodes.find((candidate) => candidate.id === target.id);
    const point = node ? graphNodePoint2d(node) : undefined;
    if (!point) {
      return;
    }
    try {
      graph2d.centerAt(point.x, point.y, graphFocusDurationMs);
      graph2d.zoom(
        Math.max(finiteNumber(graph2d.zoom()) ?? 1, graph2dMinimumFocusZoom),
        graphFocusDurationMs,
      );
    } catch {
      return;
    }
    return;
  }
  const link = graphData.links.find((candidate) => candidate.id === target.id);
  const endpoints = linkEndpointNodes(link, graphData.nodes);
  const sourcePoint = endpoints ? graphNodePoint2d(endpoints.source) : undefined;
  const targetPoint = endpoints ? graphNodePoint2d(endpoints.target) : undefined;
  if (!endpoints || !sourcePoint || !targetPoint) {
    return;
  }
  const focusPoint = midpoint2d(sourcePoint, targetPoint);
  try {
    graph2d.centerAt(focusPoint.x, focusPoint.y, graphFocusDurationMs);
    graph2d.zoomToFit(
      graphFocusDurationMs,
      graph2dLinkFocusPadding,
      (node) => node.id === endpoints.source.id || node.id === endpoints.target.id,
    );
  } catch {
    return;
  }
}

function focusGraphTarget3d(
  target: GraphStatsTarget,
  graphData: GraphData,
  graph3d: ForceGraph3DMethods<GraphNode, GraphLink> | undefined,
): void {
  if (!graph3d) {
    return;
  }
  if (target.type === "node") {
    const node = graphData.nodes.find((candidate) => candidate.id === target.id);
    const point = node ? graphNodePoint3d(node) : undefined;
    if (!point) {
      return;
    }
    focusGraph3dPoint(graph3d, graphData.nodes, point, graph3dMinimumFocusDistance);
    return;
  }
  const link = graphData.links.find((candidate) => candidate.id === target.id);
  const endpoints = linkEndpointNodes(link, graphData.nodes);
  const sourcePoint = endpoints ? graphNodePoint3d(endpoints.source) : undefined;
  const targetPoint = endpoints ? graphNodePoint3d(endpoints.target) : undefined;
  if (!sourcePoint || !targetPoint) {
    return;
  }
  const focusPoint = midpoint3d(sourcePoint, targetPoint);
  focusGraph3dPoint(
    graph3d,
    graphData.nodes,
    focusPoint,
    Math.max(
      graph3dMinimumFocusDistance,
      distance3d(sourcePoint, targetPoint) * graph3dLinkDistanceScale,
    ),
  );
}

function focusGraph3dPoint(
  graph3d: ForceGraph3DMethods<GraphNode, GraphLink>,
  nodes: GraphNode[],
  focusPoint: { x: number; y: number; z: number },
  distance: number,
): void {
  const direction = graph3dViewDirection(graph3d, nodes, focusPoint);
  if (!direction) {
    return;
  }
  const nextPosition = {
    x: focusPoint.x + direction.x * distance,
    y: focusPoint.y + direction.y * distance,
    z: focusPoint.z + direction.z * distance,
  };
  try {
    graph3d.cameraPosition(nextPosition, focusPoint, graphFocusDurationMs);
  } catch {
    return;
  }
}

function graph3dViewDirection(
  graph3d: ForceGraph3DMethods<GraphNode, GraphLink>,
  nodes: GraphNode[],
  focusPoint: { x: number; y: number; z: number },
): { x: number; y: number; z: number } | undefined {
  const cameraPosition = graph3dCameraPosition(graph3d);
  if (!cameraPosition) {
    return undefined;
  }
  const currentTarget = graph3dControlsTarget(graph3d) ?? graphNodeCenter3d(nodes) ?? focusPoint;
  return (
    normalize3d({
      x: cameraPosition.x - currentTarget.x,
      y: cameraPosition.y - currentTarget.y,
      z: cameraPosition.z - currentTarget.z,
    }) ??
    normalize3d({
      x: cameraPosition.x - focusPoint.x,
      y: cameraPosition.y - focusPoint.y,
      z: cameraPosition.z - focusPoint.z,
    }) ?? { x: 0, y: 0, z: 1 }
  );
}

function linkEndpointNodes(
  link: GraphLink | undefined,
  nodes: GraphNode[],
): { source: GraphNode; target: GraphNode } | undefined {
  if (!link) {
    return undefined;
  }
  const nodesById = graphNodeMap(nodes);
  const source = nodesById.get(nodeIdForEndpoint(link.source));
  const target = nodesById.get(nodeIdForEndpoint(link.target));
  return source && target ? { source, target } : undefined;
}

function graphNodePoint2d(node: GraphNode): { x: number; y: number } | undefined {
  const x = finiteNumber(node.x);
  const y = finiteNumber(node.y);
  return x !== undefined && y !== undefined ? { x, y } : undefined;
}

function graphNodePoint3d(node: GraphNode): { x: number; y: number; z: number } | undefined {
  const x = finiteNumber(node.x);
  const y = finiteNumber(node.y);
  const z = finiteNumber(node.z) ?? 0;
  return x !== undefined && y !== undefined ? { x, y, z } : undefined;
}

function midpoint2d(
  source: { x: number; y: number },
  target: { x: number; y: number },
): { x: number; y: number } {
  return {
    x: (source.x + target.x) / 2,
    y: (source.y + target.y) / 2,
  };
}

function midpoint3d(
  source: { x: number; y: number; z: number },
  target: { x: number; y: number; z: number },
): { x: number; y: number; z: number } {
  return {
    x: (source.x + target.x) / 2,
    y: (source.y + target.y) / 2,
    z: (source.z + target.z) / 2,
  };
}

function graph3dScreenCoords(
  graph3d: ForceGraph3DMethods<GraphNode, GraphLink> | undefined,
  x: number,
  y: number,
  z: number,
): { x: number; y: number } | undefined {
  try {
    return graph3d?.graph2ScreenCoords(x, y, z);
  } catch {
    return undefined;
  }
}

function graph3dCoordsFromScreen(
  graph3d: ForceGraph3DMethods<GraphNode, GraphLink> | undefined,
  screenX: number | undefined,
  screenY: number | undefined,
  distance: number | undefined,
): { x: number; y: number; z?: number } | undefined {
  if (screenX === undefined || screenY === undefined || distance === undefined) {
    return undefined;
  }
  try {
    return graph3d?.screen2GraphCoords(screenX, screenY, distance);
  } catch {
    return undefined;
  }
}

function graph2dScreenCoords(
  graph2d: ForceGraph2DMethods<GraphNode, GraphLink> | undefined,
  x: number,
  y: number,
): { x: number; y: number } | undefined {
  try {
    return graph2d?.graph2ScreenCoords(x, y);
  } catch {
    return undefined;
  }
}

function graph2dCoordsFromScreen(
  graph2d: ForceGraph2DMethods<GraphNode, GraphLink> | undefined,
  screenX: number | undefined,
  screenY: number | undefined,
): { x: number; y: number } | undefined {
  if (screenX === undefined || screenY === undefined) {
    return undefined;
  }
  try {
    return graph2d?.screen2GraphCoords(screenX, screenY);
  } catch {
    return undefined;
  }
}

function graph3dCameraDistanceToPoint(
  graph3d: ForceGraph3DMethods<GraphNode, GraphLink> | undefined,
  x: number,
  y: number,
  z: number,
): number | undefined {
  const cameraPosition = graph3dCameraPosition(graph3d);
  if (!cameraPosition) {
    return undefined;
  }
  return distance3d(cameraPosition, { x, y, z });
}

function graph3dFallbackCameraDistance(
  graph3d: ForceGraph3DMethods<GraphNode, GraphLink> | undefined,
  nodes: GraphNode[],
): number | undefined {
  const cameraPosition = graph3dCameraPosition(graph3d);
  if (!cameraPosition) {
    return undefined;
  }
  const controlsTarget = graph3dControlsTarget(graph3d);
  const center = controlsTarget ?? graphNodeCenter3d(nodes);
  return center ? distance3d(cameraPosition, center) : undefined;
}

function graph3dCameraPosition(
  graph3d: ForceGraph3DMethods<GraphNode, GraphLink> | undefined,
): { x: number; y: number; z: number } | undefined {
  try {
    const position = graph3d?.camera().position;
    const x = finiteNumber(position?.x);
    const y = finiteNumber(position?.y);
    const z = finiteNumber(position?.z);
    return x !== undefined && y !== undefined && z !== undefined ? { x, y, z } : undefined;
  } catch {
    return undefined;
  }
}

function graph3dControlsTarget(
  graph3d: ForceGraph3DMethods<GraphNode, GraphLink> | undefined,
): { x: number; y: number; z: number } | undefined {
  try {
    const controls = graph3d?.controls() as { target?: { x?: number; y?: number; z?: number } };
    const x = finiteNumber(controls?.target?.x);
    const y = finiteNumber(controls?.target?.y);
    const z = finiteNumber(controls?.target?.z);
    return x !== undefined && y !== undefined && z !== undefined ? { x, y, z } : undefined;
  } catch {
    return undefined;
  }
}

function graphNodeCenter3d(nodes: GraphNode[]): { x: number; y: number; z: number } | undefined {
  let count = 0;
  let totalX = 0;
  let totalY = 0;
  let totalZ = 0;
  for (const node of nodes) {
    const x = finiteNumber(node.x);
    const y = finiteNumber(node.y);
    const z = finiteNumber(node.z) ?? 0;
    if (x === undefined || y === undefined) {
      continue;
    }
    count += 1;
    totalX += x;
    totalY += y;
    totalZ += z;
  }
  return count > 0 ? { x: totalX / count, y: totalY / count, z: totalZ / count } : undefined;
}

function distance3d(
  from: { x: number; y: number; z: number },
  to: { x: number; y: number; z: number },
): number {
  return Math.hypot(from.x - to.x, from.y - to.y, from.z - to.z);
}

function normalize3d(vector: {
  x: number;
  y: number;
  z: number;
}): { x: number; y: number; z: number } | undefined {
  const length = Math.hypot(vector.x, vector.y, vector.z);
  return length > 0
    ? {
        x: vector.x / length,
        y: vector.y / length,
        z: vector.z / length,
      }
    : undefined;
}

function finiteNumber(value: number | undefined): number | undefined {
  return typeof value === "number" && Number.isFinite(value) ? value : undefined;
}

export function searchTargetsForSearch(
  query: string,
  matchCase: boolean,
  nodes: GraphNode[],
): GraphStatsTarget[] {
  const normalizedQuery = normalizeSearchText(query.trim(), matchCase);
  if (!normalizedQuery) {
    return [];
  }
  return nodes
    .filter((node) => searchableNodeText(node, matchCase).includes(normalizedQuery))
    .map((node) => ({ type: "node" as const, id: node.id }));
}

export function highlightForSearchTargets(
  targets: GraphStatsTarget[],
  links: GraphLink[],
  query: string,
): HighlightState | undefined {
  if (!query) {
    return undefined;
  }
  const activeNodeIds = new Set(targets.map((target) => target.id));
  const activeLinkIds = new Set(
    links
      .filter(
        (link) =>
          activeNodeIds.has(nodeIdForEndpoint(link.source)) &&
          activeNodeIds.has(nodeIdForEndpoint(link.target)),
      )
      .map((link) => link.id),
  );
  return { activeNodeIds, activeLinkIds };
}

export function isSearchFocusShortcut(event: KeyboardEvent): boolean {
  return isPrimaryModifierShortcut(event, "f") && !event.shiftKey;
}

export function isSearchClearShortcut(
  event: KeyboardEvent,
  searchInput: HTMLInputElement | null,
): boolean {
  return event.key === "Escape" && document.activeElement === searchInput;
}

export function searchNavigationDirection(
  event: KeyboardEvent,
  searchInput: HTMLInputElement | null,
): 1 | -1 | undefined {
  if (event.key === "Enter" && document.activeElement === searchInput) {
    return event.shiftKey ? -1 : 1;
  }
  if (event.key === "F3" && !event.metaKey && !event.ctrlKey && !event.altKey) {
    return event.shiftKey ? -1 : 1;
  }
  if (isPrimaryModifierShortcut(event, "g")) {
    return event.shiftKey ? -1 : 1;
  }
  return undefined;
}

function isPrimaryModifierShortcut(event: KeyboardEvent, key: string): boolean {
  return event.key.toLowerCase() === key && (event.metaKey || event.ctrlKey) && !event.altKey;
}

function searchableNodeText(node: GraphNode, matchCase: boolean): string {
  return normalizeSearchText(detailParts(node.label, node.fullPath).join(" "), matchCase);
}

function normalizeSearchText(value: string, matchCase: boolean): string {
  return matchCase ? value : value.toLowerCase();
}

export function configureGraphForces(
  graph:
    | ForceGraph2DMethods<GraphNode, GraphLink>
    | ForceGraph3DMethods<GraphNode, GraphLink>
    | undefined,
): void {
  if (!graph) {
    return;
  }
  try {
    const chargeForce = graph.d3Force("charge") as
      | { strength?: (strength: (node: GraphNode) => number) => unknown }
      | undefined;
    chargeForce?.strength?.(graphNodeChargeStrength);
    const linkForce = graph.d3Force("link") as
      | { distance?: (distance: (link: GraphLink) => number) => unknown }
      | undefined;
    linkForce?.distance?.(graphLinkDistance);
    graph.d3ReheatSimulation();
  } catch {
    return;
  }
}

function graphNodeChargeStrength(node: GraphNode): number {
  return -(graphChargeBaseStrength + graphNodeForceValue(node) * graphChargeValueStrength);
}

function graphLinkDistance(link: GraphLink): number {
  return (
    graphLinkDistanceBase +
    (graphEndpointForceValue(link.source) + graphEndpointForceValue(link.target)) *
      graphLinkDistanceValueScale
  );
}

function graphEndpointForceValue(endpoint: string | GraphNode): number {
  return typeof endpoint === "string" ? minimumNodeValue : graphNodeForceValue(endpoint);
}

function graphNodeForceValue(node: GraphNode): number {
  return finiteNumber(node.value) ?? minimumNodeValue;
}

export function highlightForSelection(
  selection: Selection,
  links: GraphLink[],
  showOutgoingLinks: boolean,
): HighlightState | undefined {
  if (!selection) {
    return undefined;
  }
  if (selection.type === "link") {
    const selectedLink = links.find((link) => link.id === selection.item.id) ?? selection.item;
    return {
      activeNodeIds: new Set([
        nodeIdForEndpoint(selectedLink.source),
        nodeIdForEndpoint(selectedLink.target),
      ]),
      activeLinkIds: new Set([selectedLink.id]),
    };
  }
  const selectedNodeId = selection.item.id;
  const activeNodeIds = new Set<string>([selectedNodeId]);
  const activeLinkIds = new Set<string>();
  for (const link of links) {
    if (nodeIdForEndpoint(link.target) !== selectedNodeId) {
      continue;
    }
    const sourceNodeId = nodeIdForEndpoint(link.source);
    activeNodeIds.add(sourceNodeId);
    activeLinkIds.add(link.id);
  }
  if (showOutgoingLinks) {
    for (const link of links) {
      if (nodeIdForEndpoint(link.source) !== selectedNodeId) {
        continue;
      }
      const targetNodeId = nodeIdForEndpoint(link.target);
      activeNodeIds.add(targetNodeId);
      activeLinkIds.add(link.id);
    }
  }
  return { activeNodeIds, activeLinkIds };
}

export function nodeIdForEndpoint(endpoint: string | GraphNode): string {
  return typeof endpoint === "string" ? endpoint : endpoint.id;
}

export function isActiveNode(node: GraphNode, highlight: HighlightState | undefined): boolean {
  return !highlight || highlight.activeNodeIds.has(node.id);
}

export function isActiveLink(link: GraphLink, highlight: HighlightState | undefined): boolean {
  return !highlight || highlight.activeLinkIds.has(link.id);
}

export function nodeColor(
  node: GraphNode,
  highlight: HighlightState | undefined,
  themePalette: GraphThemePalette,
): string {
  return isActiveNode(node, highlight) ? node.color : themePalette.mutedNode;
}

export function linkColor(
  link: GraphLink,
  highlight: HighlightState | undefined,
  themePalette: GraphThemePalette,
): string {
  return isActiveLink(link, highlight) ? link.color : themePalette.mutedLink;
}

export function nodeTextObject(
  node: GraphNode,
  highlight: HighlightState | undefined,
  themePalette: GraphThemePalette,
): SpriteText {
  const offset = nodeTextOffset3d(node);
  const textHeight = nodeTextHeight(node);
  const sprite = new SpriteText(node.label, textHeight, nodeColor(node, highlight, themePalette));
  sprite.fontFace = "system-ui, sans-serif";
  sprite.fontWeight = nodeTextFontWeight(node);
  sprite.backgroundColor = false;
  sprite.padding = 0.5;
  (sprite as CenteredSpriteText).center.y = nodeTextAnchor(offset, textHeight);
  return sprite;
}

export function nodeTextHeight(node: GraphNode): number {
  return isFileLikeGraphNode(node) ? 5 : 4;
}

export function nodeTextOffset(node: GraphNode): number {
  return nodeRadius(node) + (isFileLikeGraphNode(node) ? 2.5 : 1.5);
}

export function nodeTextOffset3d(node: GraphNode): number {
  return nodeRadius(node) + (isFileLikeGraphNode(node) ? 1.5 : 0.75);
}

function nodeTextAnchor(offset: number, textHeight: number): number {
  return -offset / textHeight;
}

function nodeTextFontWeight(node: GraphNode): "500" | "600" {
  return isFileLikeGraphNode(node) ? "600" : "500";
}

export function nodeValue(referenceCount: number): number {
  const scale = Math.sqrt(
    clamp(referenceCount, 0, maximumNodeScaleReferenceCount) / maximumNodeScaleReferenceCount,
  );
  return minimumNodeValue + scale * (maximumNodeValue - minimumNodeValue);
}

export function linkArrowLength(link: GraphLink): number {
  return link.kind === "include" ? 7 : 3.5;
}

export function linkWidth2d(link: GraphLink, highlight: HighlightState | undefined): number {
  const width = clamp(0.8 + Math.log2(link.count + 1) * 0.4, 0.8, 3);
  const visibleWidth = link.kind === "include" ? Math.max(3.5, width + 1.5) : width;
  return isActiveLink(link, highlight) ? visibleWidth : Math.max(0.35, visibleWidth * 0.35);
}

export function linkWidth3d(link: GraphLink, highlight: HighlightState | undefined): number {
  if (!isActiveLink(link, highlight)) {
    return 0.05;
  }
  return link.kind === "include" ? 1.5 : 0;
}

export function linkParticleCount(link: GraphLink, highlight: HighlightState | undefined): number {
  if (!isActiveLink(link, highlight)) {
    return 0;
  }
  return clamp(Math.ceil(Math.sqrt(link.count)), 1, 8);
}

export function linkParticleWidth3d(link: GraphLink): number {
  return link.kind === "include" ? 2.75 : 1.25;
}

export function clamp(value: number, minimum: number, maximum: number): number {
  return Math.min(maximum, Math.max(minimum, value));
}

export function maxInspectorWidthForLayout(containerWidth: number): number {
  if (containerWidth <= 0) {
    return inspectorMaximumWidth;
  }
  return Math.max(
    inspectorMinimumWidth,
    Math.min(inspectorMaximumWidth, containerWidth - graphMinimumWidth - paneResizeHandleWidth),
  );
}

export function nodeCategoryForColor(node: AspGraphNode): NodeColorCategory {
  if (node.isRoot) {
    return "root";
  }
  if (node.kind === "missingInclude") {
    return "missingInclude";
  }
  if (node.kind === "file") {
    return "file";
  }
  if (node.kind === "vbUnresolved") {
    if (node.role === "member") {
      return "member";
    }
    return isCallableUnresolvedRole(node.role) ? "unresolvedFunction" : "unresolved";
  }
  if (node.kind === "vbMemberReference") {
    return "member";
  }
  if (node.externalKind === "member") {
    return "member";
  }
  switch (node.declarationKind) {
    case "function":
      return "function";
    case "sub":
      return "sub";
    case "class":
      return "class";
    case "method":
      if (node.procedureKind === "function") {
        return "methodFunction";
      }
      if (node.procedureKind === "sub") {
        return "methodSub";
      }
      return "method";
    case "property":
      return "property";
    case "field":
      return "localVariable";
    case "parameter":
      return "parameter";
    case "variable":
      if (isImplicitGlobalVariableNode(node)) {
        return "implicitGlobalVariable";
      }
      return node.bindingScope === "local" ? "localVariable" : "globalVariable";
    case "constant":
      if (node.bindingScope === "local") {
        return "localConstant";
      }
      return node.memberOf ? "localConstant" : "globalConstant";
    case "object":
      return "globalVariable";
    default:
      return externalNodeCategory(node);
  }
}

function externalNodeCategory(node: AspGraphNode): NodeColorCategory {
  switch (node.externalKind) {
    case "function":
      return "function";
    case "constant":
      return "globalConstant";
    case "member":
      return "member";
    case "object":
      return "globalVariable";
    default:
      return "globalVariable";
  }
}

export function paintNode(
  node: GraphNode,
  canvas: CanvasRenderingContext2D,
  highlight: HighlightState | undefined,
  themePalette: GraphThemePalette,
): void {
  const radius = nodeRadius(node);
  const active = isActiveNode(node, highlight);
  canvas.beginPath();
  canvas.arc(node.x ?? 0, node.y ?? 0, radius, 0, 2 * Math.PI, false);
  canvas.fillStyle = active ? node.color : themePalette.mutedNode;
  canvas.fill();
  const offset = nodeTextOffset(node);
  canvas.save();
  canvas.font = `${nodeTextFontWeight(node)} ${nodeTextHeight(node)}px system-ui, sans-serif`;
  canvas.fillStyle = nodeColor(node, highlight, themePalette);
  canvas.textAlign = "center";
  canvas.textBaseline = "bottom";
  canvas.fillText(node.label, node.x ?? 0, (node.y ?? 0) - offset);
  canvas.restore();
}

export function paintNodePointerArea(
  node: GraphNode,
  color: string,
  canvas: CanvasRenderingContext2D,
): void {
  canvas.fillStyle = color;
  canvas.beginPath();
  canvas.arc(node.x ?? 0, node.y ?? 0, nodeRadius(node) + 2, 0, 2 * Math.PI, false);
  canvas.fill();
}

export function nodeRadius(node: GraphNode): number {
  return (isFileLikeGraphNode(node) ? 3.4 : 3) + node.value;
}
