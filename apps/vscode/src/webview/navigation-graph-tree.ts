import type {
  AspNavigationEdge,
  AspNavigationGraphPayload,
  AspNavigationNode,
} from "../protocol-types";

/** A bounded forest: each page is expanded once; repeated targets remain visible as references. */
export interface NavigationTreeEntry {
  id: string;
  node: AspNavigationNode;
  edge?: AspNavigationEdge;
  depth: number;
  parentIndex?: number;
  reference?: "cycle" | "shared";
  subtreeEnd: number;
}

/** Builds an iterative forest without recursive or exponential expansion of cycles and shared pages. */
export function navigationTreeEntries(
  payload: Pick<AspNavigationGraphPayload, "nodes" | "edges">,
): NavigationTreeEntry[] {
  const nodes = new Map(payload.nodes.map((node) => [node.id, node]));
  const outgoing = new Map<string, AspNavigationEdge[]>();
  const incoming = new Set<string>();
  for (const edge of payload.edges) {
    if (!nodes.has(edge.source) || !nodes.has(edge.target)) continue;
    const list = outgoing.get(edge.source) ?? [];
    list.push(edge);
    outgoing.set(edge.source, list);
    incoming.add(edge.target);
  }
  const nodeOrder = (left: AspNavigationNode, right: AspNavigationNode) =>
    left.label.localeCompare(right.label) || left.id.localeCompare(right.id);
  for (const edges of outgoing.values()) {
    edges.sort(
      (left, right) =>
        nodeOrder(nodes.get(left.target)!, nodes.get(right.target)!) ||
        left.id.localeCompare(right.id),
    );
  }
  const priority = (node: AspNavigationNode) => (node.isRoot ? 0 : incoming.has(node.id) ? 2 : 1);
  const roots = [...nodes.values()].sort((a, b) => priority(a) - priority(b) || nodeOrder(a, b));
  // Choose the shortest route from each entry before rendering the forest.
  // A link from a help page must not move a directly reachable page deeper.
  const primaryEdge = new Map<string, string | undefined>();
  for (const root of roots) {
    if (primaryEdge.has(root.id)) continue;
    primaryEdge.set(root.id, undefined);
    const queue = [root.id];
    for (let index = 0; index < queue.length; index++) {
      for (const edge of outgoing.get(queue[index]) ?? []) {
        if (primaryEdge.has(edge.target)) continue;
        primaryEdge.set(edge.target, edge.id);
        queue.push(edge.target);
      }
    }
  }
  const entries: NavigationTreeEntry[] = [];
  const visited = new Set<string>();
  const active = new Set<string>();
  type Work = {
    node: AspNavigationNode;
    edge?: AspNavigationEdge;
    depth: number;
    parentIndex?: number;
    exitIndex?: number;
  };
  for (const root of roots) {
    if (visited.has(root.id)) continue;
    const work: Work[] = [{ node: root, depth: 0 }];
    while (work.length) {
      const item = work.pop()!;
      if (item.exitIndex !== undefined) {
        entries[item.exitIndex].subtreeEnd = entries.length;
        active.delete(item.node.id);
        continue;
      }
      const reference = active.has(item.node.id)
        ? "cycle"
        : (item.edge && primaryEdge.get(item.node.id) !== item.edge.id) || visited.has(item.node.id)
          ? "shared"
          : undefined;
      const index = entries.length;
      entries.push({
        id: item.edge ? `edge:${item.edge.id}` : `root:${item.node.id}`,
        node: item.node,
        edge: item.edge,
        depth: item.depth,
        parentIndex: item.parentIndex,
        reference,
        subtreeEnd: index + 1,
      });
      if (reference) continue;
      visited.add(item.node.id);
      active.add(item.node.id);
      work.push({ ...item, exitIndex: index });
      const children = outgoing.get(item.node.id) ?? [];
      for (let child = children.length - 1; child >= 0; child--) {
        const edge = children[child];
        work.push({
          node: nodes.get(edge.target)!,
          edge,
          depth: item.depth + 1,
          parentIndex: index,
        });
      }
    }
  }
  return entries;
}

/** Removes collapsed subtrees without turning hidden descendants into additional roots. */
export function visibleNavigationTreeEntries(
  entries: NavigationTreeEntry[],
  collapsed: ReadonlySet<string>,
): NavigationTreeEntry[] {
  const visible: NavigationTreeEntry[] = [];
  for (let index = 0; index < entries.length;) {
    const entry = entries[index];
    visible.push(entry);
    index = collapsed.has(entry.id) ? entry.subtreeEnd : index + 1;
  }
  return visible;
}

/** Decodes a source URI for display while leaving the original URI intact for editor messages. */
export function navigationSourcePath(uri: string): string {
  try {
    return decodeURIComponent(new URL(uri).pathname).replace(/^\/(?:([A-Za-z]:))/, "$1");
  } catch {
    return uri;
  }
}
