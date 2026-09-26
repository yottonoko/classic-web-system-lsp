import type { AspNavigationEdge, AspNavigationNode } from "../protocol-types";

/** A connected set of pages, or the separate collection of pages without transitions. */
export interface NavigationComponent {
  id: string;
  label: string;
  nodes: AspNavigationNode[];
  edges: AspNavigationEdge[];
  isolated: boolean;
}

/** Partition workspace graphs without treating a shared external destination as a new entry. */
export function navigationComponents(
  nodes: AspNavigationNode[],
  edges: AspNavigationEdge[],
): NavigationComponent[] {
  const byId = new Map(nodes.map((node) => [node.id, node]));
  const adjacency = new Map(nodes.map((node) => [node.id, [] as string[]]));
  const validEdges = edges.filter((edge) => byId.has(edge.source) && byId.has(edge.target));
  for (const edge of validEdges) {
    adjacency.get(edge.source)!.push(edge.target);
    adjacency.get(edge.target)!.push(edge.source);
  }
  const visited = new Set<string>();
  const componentByNode = new Map<string, NavigationComponent>();
  const components: NavigationComponent[] = [];
  const isolated: AspNavigationNode[] = [];
  for (const node of [...nodes].sort((a, b) => a.id.localeCompare(b.id))) {
    if (visited.has(node.id)) continue;
    if (!adjacency.get(node.id)!.length) {
      isolated.push(node);
      continue;
    }
    const members: AspNavigationNode[] = [];
    const stack = [node.id];
    visited.add(node.id);
    while (stack.length) {
      const id = stack.pop()!;
      members.push(byId.get(id)!);
      for (const next of adjacency.get(id)!) {
        if (!visited.has(next)) {
          visited.add(next);
          stack.push(next);
        }
      }
    }
    members.sort((a, b) => Number(!!b.isRoot) - Number(!!a.isRoot) || a.id.localeCompare(b.id));
    const component = {
      id: `connected:${node.id}`,
      label: members[0].label,
      nodes: members,
      edges: [],
      isolated: false,
    };
    components.push(component);
    for (const member of members) componentByNode.set(member.id, component);
  }
  for (const edge of validEdges) componentByNode.get(edge.source)!.edges.push(edge);
  components.sort(
    (a, b) =>
      Number(!!b.nodes[0].isRoot) - Number(!!a.nodes[0].isRoot) ||
      b.nodes.length - a.nodes.length ||
      a.id.localeCompare(b.id),
  );
  if (isolated.length)
    components.push({ id: "isolated", label: "", nodes: isolated, edges: [], isolated: true });
  return components;
}
