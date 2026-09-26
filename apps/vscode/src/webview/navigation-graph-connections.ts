import type { AspNavigationEdge, AspNavigationNode } from "../protocol-types";

/** The page or transition whose immediate connections are being inspected. */
export type NavigationFocus = { kind: "node" | "edge"; id: string } | undefined;

/** Direction relative to the selected page; loops enter and leave the same page. */
export type NavigationDirection = "incoming" | "outgoing" | "both" | "selected";

/** Keep selection focused across pointer movement, ignoring targets removed by filters. */
export function navigationConnections(
  nodes: Pick<AspNavigationNode, "id">[],
  edges: Pick<AspNavigationEdge, "id" | "source" | "target">[],
  selection: NavigationFocus,
  hovered?: NavigationFocus,
): { active: NavigationFocus; nodes: Set<string>; edges: Map<string, NavigationDirection> } {
  const nodeIds = new Set(nodes.map((node) => node.id));
  const visibleEdges = edges.filter((edge) => nodeIds.has(edge.source) && nodeIds.has(edge.target));
  const exists = (target: NavigationFocus) =>
    target &&
    (target.kind === "node"
      ? nodeIds.has(target.id)
      : visibleEdges.some((edge) => edge.id === target.id));
  const active = exists(selection) ? selection : exists(hovered) ? hovered : undefined;
  const relatedNodes = new Set<string>();
  const relatedEdges = new Map<string, NavigationDirection>();
  if (active?.kind === "node") relatedNodes.add(active.id);
  for (const edge of visibleEdges) {
    if (active?.kind === "edge" && edge.id === active.id) {
      relatedEdges.set(edge.id, "selected");
    } else if (active?.kind === "node") {
      const incoming = edge.target === active.id;
      const outgoing = edge.source === active.id;
      if (incoming || outgoing) {
        relatedEdges.set(
          edge.id,
          incoming && outgoing ? "both" : incoming ? "incoming" : "outgoing",
        );
      }
    }
    if (relatedEdges.has(edge.id)) {
      relatedNodes.add(edge.source);
      relatedNodes.add(edge.target);
    }
  }
  return { active, nodes: relatedNodes, edges: relatedEdges };
}

/** Identify return edges in a deterministic DFS without labeling ordinary shared destinations as loops. */
export function navigationLoopEdgeIds(
  nodes: Pick<AspNavigationNode, "id">[],
  edges: Pick<AspNavigationEdge, "id" | "source" | "target">[],
): Set<string> {
  const outgoing = new Map(nodes.map((node) => [node.id, [] as typeof edges]));
  for (const edge of edges) if (outgoing.has(edge.target)) outgoing.get(edge.source)?.push(edge);
  for (const list of outgoing.values()) list.sort((a, b) => a.id.localeCompare(b.id));
  const visited = new Set<string>(),
    active = new Set<string>(),
    loops = new Set<string>();
  for (const id of [...outgoing.keys()].sort()) {
    if (visited.has(id)) continue;
    const stack = [{ id, index: 0 }];
    visited.add(id);
    active.add(id);
    while (stack.length) {
      const frame = stack[stack.length - 1];
      const edge = outgoing.get(frame.id)![frame.index++];
      if (!edge) {
        active.delete(frame.id);
        stack.pop();
        continue;
      }
      if (active.has(edge.target)) {
        loops.add(edge.id);
        continue;
      }
      if (!visited.has(edge.target)) {
        visited.add(edge.target);
        active.add(edge.target);
        stack.push({ id: edge.target, index: 0 });
      }
    }
  }
  return loops;
}
