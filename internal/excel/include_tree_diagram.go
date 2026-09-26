package excel

import (
	"fmt"
	"sort"

	"github.com/xuri/excelize/v2"
)

type includeDiagramPosition struct {
	column int
	row    int
}

func includeTreeDiagramSheet(context analysisContext, locale Locale, relations []includeTreeRelation) AnalysisSheet {
	nodes := map[string]string{}
	adjacent := map[string][]string{}
	indegree := map[string]int{}
	fillByNode := map[string]string{}
	for _, relation := range relations {
		sourceID := relation.edge.Source
		targetID := relation.edge.Target
		nodes[sourceID] = fileNameForNode(relation.source)
		nodes[targetID] = fileNameForNode(relation.target)
		adjacent[sourceID] = append(adjacent[sourceID], targetID)
		indegree[targetID]++
		if _, ok := indegree[sourceID]; !ok {
			indegree[sourceID] = 0
		}
		switch relation.direction {
		case "ancestor":
			fillByNode[sourceID] = "FCE5CD"
		case "relative":
			if fillByNode[targetID] == "" {
				fillByNode[targetID] = "E7E6F6"
			}
		default:
			if fillByNode[targetID] == "" {
				fillByNode[targetID] = "D9EAD3"
			}
		}
	}
	for id, node := range context.nodesByID {
		if node.Kind == "file" && context.targetURI != "" && (node.URI == context.targetURI || id == context.targetURI) {
			nodes[id] = fileNameForNode(node)
			fillByNode[id] = "CFE2F3"
		}
	}
	for source := range adjacent {
		sort.SliceStable(adjacent[source], func(i, j int) bool {
			left, right := adjacent[source][i], adjacent[source][j]
			if nodes[left] != nodes[right] {
				return nodes[left] < nodes[right]
			}
			return left < right
		})
	}
	ids := make([]string, 0, len(nodes))
	for id := range nodes {
		ids = append(ids, id)
	}
	sort.SliceStable(ids, func(i, j int) bool {
		if nodes[ids[i]] != nodes[ids[j]] {
			return nodes[ids[i]] < nodes[ids[j]]
		}
		return ids[i] < ids[j]
	})
	roots := make([]string, 0, len(ids))
	for _, id := range ids {
		if indegree[id] == 0 {
			roots = append(roots, id)
		}
	}
	visited := map[string]struct{}{}
	depths := map[string]int{}
	order := make([]string, 0, len(ids))
	visit := func(root string) {
		queue := []string{root}
		if _, seen := visited[root]; seen {
			return
		}
		visited[root] = struct{}{}
		for index := 0; index < len(queue); index++ {
			current := queue[index]
			order = append(order, current)
			for _, target := range adjacent[current] {
				if _, seen := visited[target]; seen {
					continue
				}
				visited[target] = struct{}{}
				depths[target] = depths[current] + 1
				queue = append(queue, target)
			}
		}
	}
	for _, root := range roots {
		visit(root)
	}
	for _, id := range ids {
		visit(id)
	}

	positions := make(map[string]includeDiagramPosition, len(order))
	nodeShapes := make([]AnalysisShape, 0, len(order))
	for index, id := range order {
		position := includeDiagramPosition{column: 2 + depths[id]*4, row: 3 + index*4}
		positions[id] = position
		cell, _ := excelize.CoordinatesToCellName(position.column, position.row)
		fill := fillByNode[id]
		if fill == "" {
			fill = "EAF2F8"
		}
		label := nodes[id]
		if label == "" {
			label = id
		}
		nodeShapes = append(nodeShapes, AnalysisShape{
			Cell: cell, Type: "roundRect", Width: 170, Height: 44,
			Text: label, AltText: label, Name: fmt.Sprintf("include-node-%03d", index+1),
			FillColor: fill, LineColor: "5B9BD5", TextColor: "17365D", Bold: true,
		})
	}
	seenEdges := map[string]struct{}{}
	connectorIndex := 0
	connectorShapes := make([]AnalysisShape, 0, len(relations))
	for _, relation := range relations {
		key := relation.edge.Source + "\x00" + relation.edge.Target
		if _, duplicate := seenEdges[key]; duplicate {
			continue
		}
		seenEdges[key] = struct{}{}
		source, sourceOK := positions[relation.edge.Source]
		target, targetOK := positions[relation.edge.Target]
		if !sourceOK || !targetOK {
			continue
		}
		anchorColumn := min(source.column, target.column)
		anchorRow := min(source.row, target.row)
		cell, _ := excelize.CoordinatesToCellName(anchorColumn, anchorRow)
		width := uint(max(40, absInt(target.column-source.column)*64))
		height := uint(max(4, absInt(target.row-source.row)*20))
		connectorIndex++
		connectorShapes = append(connectorShapes, AnalysisShape{
			Cell: cell, Type: "straightConnector1", Width: width, Height: height,
			OffsetX: 84, OffsetY: 22,
			AltText: nodes[relation.edge.Source] + " -> " + nodes[relation.edge.Target],
			Name:    fmt.Sprintf("include-connector-%03d", connectorIndex), LineColor: "7F8C8D",
		})
	}
	return AnalysisSheet{
		Sheet:  text(locale, "includeTreeDiagram"),
		Data:   [][]Cell{{text(locale, "includeTreeDiagram")}},
		Shapes: append(connectorShapes, nodeShapes...),
	}
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
