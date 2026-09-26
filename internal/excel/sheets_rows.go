package excel

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/graph"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func graphEdges(payload graph.Payload) []graph.Edge {
	if len(payload.Links) > 0 {
		return payload.Links
	}
	return payload.Edges
}

func sortAnalysisContext(context *analysisContext) {
	sortNodes(context.targetDeclarations)
	sortNodes(context.includedDeclarations)
	sortNodes(context.implicitGlobals)
	sortNodes(context.unusedDeclarations)
	sortNodes(context.unresolved)
	sortNodes(context.memberUsages)
	sortUnresolvedUsages(context.unresolvedUsages)
	sortUsages(context.internalUsages)
	sortUsages(context.externalUsages)
	sortUsages(context.includedUsages)
	sortUsages(context.implicitAssignments)
}

func sortUnresolvedUsages(rows []unresolvedRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].file != rows[j].file {
			return rows[i].file < rows[j].file
		}
		if rows[i].line != rows[j].line {
			return rows[i].line < rows[j].line
		}
		return rows[i].name < rows[j].name
	})
}

func sortNodes(nodes []graph.Node) {
	sort.SliceStable(nodes, func(i, j int) bool {
		if nodes[i].URI != nodes[j].URI {
			return nodes[i].URI < nodes[j].URI
		}
		if lineNumber(nodes[i]) != lineNumber(nodes[j]) {
			return lineNumber(nodes[i]) < lineNumber(nodes[j])
		}
		return nodes[i].Label < nodes[j].Label
	})
}

func sortUsages(rows []usageRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].sourceFile != rows[j].sourceFile {
			return rows[i].sourceFile < rows[j].sourceFile
		}
		if rows[i].line != rows[j].line {
			return rows[i].line < rows[j].line
		}
		return rows[i].name < rows[j].name
	})
}

func usageRowsForEdge(edge graph.Edge, target graph.Node, nodesByID map[string]graph.Node, filesByURI map[string]graph.Node) []usageRow {
	sourceNode := nodesByID[edge.Source]
	sourceFile := fileNameForNode(sourceNode)
	if sourceFile == "" {
		sourceFile = edge.Source
	}
	sourceOwner := sourceNode.Label
	if sourceOwner == "" {
		sourceOwner = sourceNode.FileName
	}
	if sourceOwner == "" {
		sourceOwner = edge.Source
	}
	role := edge.Role
	if role == "" {
		role = edge.Label
	}
	rows := make([]usageRow, 0, max(1, len(edge.Ranges)))
	sourceURI := sourceURIForEdge(edge, nodesByID)
	targetFile := fileNameForURI(target.URI, filesByURI)
	if len(edge.Ranges) == 0 {
		rows = append(rows, usageRow{
			sourceID: edge.Source, targetID: edge.Target, sourceURI: sourceURI, targetURI: target.URI,
			kind: edge.Kind, role: role, sourceFile: sourceFile, sourceOwner: sourceOwner,
			targetFile: targetFile, name: target.Label, declKind: target.DeclarationKind, typeName: target.TypeName,
			line: lineNumberFromRange(target.Range), column: columnNumberFromRange(target.Range), count: countOrOne(edge.Count),
		})
		return rows
	}
	count := countOrOne(edge.Count)
	if len(edge.Ranges) > 1 {
		count = 1
	}
	for _, location := range edge.Ranges {
		rowSourceFile := sourceFile
		rowSourceURI := sourceURI
		if location.URI != "" {
			rowSourceFile = fileNameForURI(location.URI, filesByURI)
			rowSourceURI = location.URI
		}
		rows = append(rows, usageRow{
			sourceID:    edge.Source,
			targetID:    edge.Target,
			sourceURI:   rowSourceURI,
			targetURI:   target.URI,
			kind:        edge.Kind,
			role:        role,
			sourceFile:  rowSourceFile,
			sourceOwner: sourceOwner,
			targetFile:  targetFile,
			name:        target.Label,
			declKind:    target.DeclarationKind,
			typeName:    target.TypeName,
			line:        location.Range.Start.Line + 1,
			column:      location.Range.Start.Character + 1,
			count:       count,
		})
	}
	return rows
}

func unresolvedRowsForEdge(edge graph.Edge, nodesByID map[string]graph.Node, filesByURI map[string]graph.Node) []unresolvedRow {
	source := nodesByID[edge.Source]
	target := nodesByID[edge.Target]
	sourceName := source.Label
	if sourceName == "" {
		sourceName = source.FileName
	}
	if sourceName == "" {
		sourceName = edge.Source
	}
	kind := target.Group
	if kind == "" {
		kind = "unresolvedReference"
	}
	if target.Role == "new" {
		kind = "unresolvedReference"
	}
	role := edge.Role
	if role == "" {
		role = edge.Label
	}
	locations := make([]unresolvedRow, 0, max(1, len(edge.Ranges)))
	count := countOrOne(edge.Count)
	if len(edge.Ranges) > 1 {
		count = 1
	}
	if len(edge.Ranges) == 0 {
		locations = append(locations, unresolvedRow{
			usageKind: edge.Kind,
			role:      role,
			kind:      kind,
			source:    sourceName,
			name:      target.Label,
			file:      fileNameForURI(target.URI, filesByURI),
			line:      lineNumber(target),
			column:    columnNumber(target),
			count:     count,
		})
		return locations
	}
	for _, location := range edge.Ranges {
		locations = append(locations, unresolvedRow{
			usageKind: edge.Kind,
			role:      role,
			kind:      kind,
			source:    sourceName,
			name:      target.Label,
			file:      fileNameForURI(location.URI, filesByURI),
			line:      location.Range.Start.Line + 1,
			column:    location.Range.Start.Character + 1,
			count:     count,
		})
	}
	return locations
}

func sourceURIForEdge(edge graph.Edge, nodesByID map[string]graph.Node) string {
	source := nodesByID[edge.Source]
	if source.URI != "" {
		return source.URI
	}
	return source.ID
}

func skipAnalysisNode(node graph.Node) bool {
	return node.Kind == "exceptionHandling"
}

func isUsageKind(kind string) bool {
	return kind == "references" || kind == "assignments" || kind == "calls"
}

func isUnusedDeclaration(node graph.Node, counts usageCounts) bool {
	if node.Implicit || node.ImplicitGlobal {
		return false
	}
	if node.Origin != "" && node.Origin != "source" {
		return false
	}
	return counts.references == 0 && counts.assignments == 0 && counts.calls == 0
}

func countOrOne(count int) int {
	if count > 0 {
		return count
	}
	return 1
}

func totalReferences(counts map[string]usageCounts) int {
	total := 0
	for _, count := range counts {
		total += count.references
	}
	return total
}

func totalAssignments(counts map[string]usageCounts) int {
	total := 0
	for _, count := range counts {
		total += count.assignments
	}
	return total
}

func totalCalls(counts map[string]usageCounts) int {
	total := 0
	for _, count := range counts {
		total += count.calls
	}
	return total
}

func fileNameForNode(node graph.Node) string {
	if node.FileName != "" {
		return filepath.ToSlash(node.FileName)
	}
	if node.URI != "" {
		return pathBaseFromURI(node.URI)
	}
	return node.Label
}

func fileNameForURI(uri string, filesByURI map[string]graph.Node) string {
	if node, ok := filesByURI[uri]; ok {
		return fileNameForNode(node)
	}
	return pathBaseFromURI(uri)
}

func pathBaseFromURI(uri string) string {
	if uri == "" {
		return ""
	}
	cleaned := strings.TrimPrefix(uri, "file://")
	cleaned = strings.TrimPrefix(cleaned, "file:")
	cleaned = strings.TrimPrefix(cleaned, "/")
	if cleaned == "" {
		return uri
	}
	return filepath.ToSlash(filepath.Base(cleaned))
}

func lineNumber(node graph.Node) int {
	return lineNumberFromRange(node.Range)
}

func columnNumber(node graph.Node) int {
	return columnNumberFromRange(node.Range)
}

func lineNumberFromRange(r *lsp.Range) int {
	if r == nil {
		return 0
	}
	return r.Start.Line + 1
}

func columnNumberFromRange(r *lsp.Range) int {
	if r == nil {
		return 0
	}
	return r.Start.Character + 1
}

func typeNameOrVariant(typeName string) string {
	if typeName == "" {
		return "Variant"
	}
	return typeName
}

func declarationTypeDisplay(node graph.Node) string {
	switch node.DeclarationKind {
	case "variable", "constant", "field", "parameter":
		return typeNameOrVariant(node.TypeName)
	default:
		return ""
	}
}

func declarationUsageTypeName(kind, typeName string) string {
	switch kind {
	case "function", "sub", "method", "property":
		return typeName
	default:
		return declarationTypeDisplay(graph.Node{DeclarationKind: kind, TypeName: typeName})
	}
}

func declarationReturnTypeDisplay(node graph.Node) string {
	if !isCallableDeclaration(node) || (node.DeclarationKind == "method" && node.ProcedureKind == "sub") {
		return ""
	}
	return typeNameOrVariant(node.TypeName)
}

func isCallableDeclaration(node graph.Node) bool {
	switch node.DeclarationKind {
	case "function", "sub", "method", "property":
		return true
	default:
		return false
	}
}

func declarationParametersDisplay(node graph.Node) string {
	if !isCallableDeclaration(node) || len(node.Parameters) == 0 {
		return ""
	}
	parameters := make([]string, 0, len(node.Parameters))
	for _, parameter := range node.Parameters {
		prefix := ""
		if parameter.Optional {
			prefix = "Optional "
		}
		mode := parameter.Mode
		if strings.EqualFold(mode, "byref") {
			mode = "ByRef"
		} else if strings.EqualFold(mode, "byval") {
			mode = "ByVal"
		}
		if mode != "" {
			prefix += mode + " "
		}
		parameters = append(parameters, prefix+parameter.Name+" As "+typeNameOrVariant(parameter.TypeName))
	}
	return strings.Join(parameters, ", ")
}

func arrayDisplay(node graph.Node, locale Locale) string {
	if node.ArrayKind == "" && (node.ArrayDimensions == nil || len(*node.ArrayDimensions) == 0) {
		return ""
	}
	kind := node.ArrayKind
	if kind == "" || kind == "array" {
		kind = text(locale, "array")
	}
	if node.ArrayDimensions == nil || len(*node.ArrayDimensions) == 0 {
		return kind
	}
	dimensions := make([]string, len(*node.ArrayDimensions))
	copy(dimensions, *node.ArrayDimensions)
	return kind + "(" + strings.Join(dimensions, ", ") + ")"
}

func yesNoText(value bool, locale Locale) string {
	if value {
		return text(locale, "yes")
	}
	return text(locale, "no")
}

func autoFilterRef(rows int, columns int) string {
	if rows <= 0 || columns <= 0 {
		return ""
	}
	return fmt.Sprintf("A1:%s%d", excelColumn(columns), rows)
}

func excelColumn(column int) string {
	result := ""
	for column > 0 {
		column--
		result = string(rune('A'+column%26)) + result
		column /= 26
	}
	return result
}
