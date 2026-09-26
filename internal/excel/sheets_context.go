package excel

import "github.com/yottonoko/classic-web-system-lsp/internal/graph"

type usageIdentity struct {
	sourceURI, targetURI, kind, role, sourceOwner string
	name, declKind, typeName                      string
	line, column                                  int
}

type includeGraphIndex struct {
	edges        []graph.Edge
	outgoing     map[string][]graph.Edge
	incoming     map[string][]graph.Edge
	fileIDsByURI map[string][]string
}

func newIncludeGraphIndex(payload graph.Payload, nodesByID map[string]graph.Node) includeGraphIndex {
	index := includeGraphIndex{}
	for _, edge := range graphEdges(payload) {
		if edge.Kind != "include" {
			continue
		}
		if index.outgoing == nil {
			index.outgoing = make(map[string][]graph.Edge)
			index.incoming = make(map[string][]graph.Edge)
		}
		index.edges = append(index.edges, edge)
		index.outgoing[edge.Source] = append(index.outgoing[edge.Source], edge)
		index.incoming[edge.Target] = append(index.incoming[edge.Target], edge)
	}
	for id, node := range nodesByID {
		if node.Kind == "file" && node.URI != "" {
			if index.fileIDsByURI == nil {
				index.fileIDsByURI = make(map[string][]string)
			}
			index.fileIDsByURI[node.URI] = append(index.fileIDsByURI[node.URI], id)
		}
	}
	return index
}

func dedupeUsageRows(rows []usageRow, seen map[usageIdentity]struct{}) []usageRow {
	result := rows
	deduplicated := false
	for index, row := range rows {
		identity := usageIdentity{
			sourceURI: row.sourceURI, targetURI: row.targetURI, kind: row.kind, role: row.role,
			sourceOwner: row.sourceOwner, name: row.name, declKind: row.declKind, typeName: row.typeName,
			line: row.line, column: row.column,
		}
		if _, duplicate := seen[identity]; duplicate {
			if !deduplicated {
				result = make([]usageRow, index, len(rows))
				copy(result, rows[:index])
				deduplicated = true
			}
			continue
		}
		seen[identity] = struct{}{}
		if deduplicated {
			result = append(result, row)
		}
	}
	return result
}

func includedFileURIsForTargetWithIndex(targetURI string, nodesByID map[string]graph.Node, graphIndex includeGraphIndex) map[string]struct{} {
	result := map[string]struct{}{}
	if targetURI == "" {
		return result
	}
	queue := includeRootIDs(targetURI, nodesByID, graphIndex)
	visited := map[string]struct{}{}
	for _, id := range queue {
		visited[id] = struct{}{}
	}
	for queueIndex := 0; queueIndex < len(queue); queueIndex++ {
		for _, edge := range graphIndex.outgoing[queue[queueIndex]] {
			targetID := edge.Target
			if _, seen := visited[targetID]; seen {
				continue
			}
			visited[targetID] = struct{}{}
			target, ok := nodesByID[targetID]
			if ok && target.URI != "" && target.Kind == "file" {
				result[target.URI] = struct{}{}
			}
			queue = append(queue, targetID)
		}
	}
	return result
}

func includeAncestorDepthsWithIndex(targetURI string, nodesByID map[string]graph.Node, graphIndex includeGraphIndex) map[string]int {
	result := map[string]int{}
	if targetURI == "" {
		return result
	}
	queue := []struct {
		id    string
		depth int
	}{}
	visited := map[string]struct{}{}
	for _, id := range includeRootIDs(targetURI, nodesByID, graphIndex) {
		queue = append(queue, struct {
			id    string
			depth int
		}{id: id, depth: 0})
		visited[id] = struct{}{}
	}
	for queueIndex := 0; queueIndex < len(queue); queueIndex++ {
		current := queue[queueIndex]
		for _, edge := range graphIndex.incoming[current.id] {
			parentID := edge.Source
			if _, seen := visited[parentID]; seen {
				continue
			}
			visited[parentID] = struct{}{}
			depth := current.depth + 1
			if parent, ok := nodesByID[parentID]; ok && parent.URI != "" {
				if previous, exists := result[parent.URI]; !exists || depth < previous {
					result[parent.URI] = depth
				}
			}
			queue = append(queue, struct {
				id    string
				depth int
			}{id: parentID, depth: depth})
		}
	}
	return result
}

func includeRootIDs(targetURI string, nodesByID map[string]graph.Node, index includeGraphIndex) []string {
	roots := append([]string(nil), index.fileIDsByURI[targetURI]...)
	if node, ok := nodesByID[targetURI]; ok && node.Kind == "file" {
		for _, id := range roots {
			if id == targetURI {
				return roots
			}
		}
		roots = append(roots, targetURI)
	}
	return roots
}

func isUnresolvedTarget(edge graph.Edge, nodesByID map[string]graph.Node) bool {
	target, ok := nodesByID[edge.Target]
	return ok && target.Kind == "vbUnresolved"
}

func includedURIForNode(node graph.Node, includedURIs map[string]struct{}) bool {
	_, ok := includedURIs[node.URI]
	return node.URI != "" && ok
}
