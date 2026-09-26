package excel

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"sort"
	"strings"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/graph"
)

func summarySheet(payload graph.Payload, context analysisContext, locale Locale, options AnalysisSheetsOptions) AnalysisSheet {
	root := context.targetFileName
	if context.workspaceWide {
		root = text(locale, "workspace")
	}
	generatedAt := options.GeneratedAt
	if generatedAt.IsZero() {
		generatedAt = time.Now()
	}
	rows := [][]Cell{
		{text(locale, "name"), text(locale, "value")},
		{text(locale, "scope"), scopeLabel(payload.Scope, locale)},
		{text(locale, "root"), root},
		{text(locale, "generatedAt"), generatedAt.Format(time.RFC3339)},
		{text(locale, "declarations"), len(context.targetDeclarations)},
		{text(locale, "references"), totalReferences(context.targetUsageCounts)},
		{text(locale, "assignments"), totalAssignments(context.targetUsageCounts)},
		{text(locale, "calls"), totalCalls(context.targetUsageCounts)},
		{text(locale, "includes"), len(context.includedURIs)},
		{text(locale, "unresolved"), context.unresolvedCount},
		{text(locale, "implicitGlobals"), len(context.implicitGlobals)},
		{text(locale, "implicitGlobalAssignments"), usageRowsCount(context.implicitAssignments)},
		{text(locale, "unused"), len(context.unusedDeclarations)},
		{text(locale, "includeRelatedIncludeTreesForUnresolved"), enabledText(options.Settings != nil && options.Settings.IncludeRelatedIncludeTreesForUnresolved, locale)},
	}
	if options.Settings != nil {
		settings := *options.Settings
		if settings.AnalysisFileCount == 0 {
			settings.AnalysisFileCount = options.AnalysisFileCount
		}
		if len(settings.IncludeGlobs) == 0 {
			settings.IncludeGlobs = append([]string(nil), options.IncludeGlobs...)
		}
		if len(settings.ExcludeGlobs) == 0 {
			settings.ExcludeGlobs = append([]string(nil), options.ExcludeGlobs...)
		}
		rows = append(rows, analysisSettingsRows(locale, settings)...)
	} else {
		if options.AnalysisFileCount > 0 {
			rows = append(rows, []Cell{text(locale, "analysisFileCount"), options.AnalysisFileCount})
		}
		if len(options.IncludeGlobs) > 0 {
			rows = append(rows, []Cell{text(locale, "includeGlobs"), strings.Join(options.IncludeGlobs, "\n")})
		}
		if len(options.ExcludeGlobs) > 0 {
			rows = append(rows, []Cell{text(locale, "excludeGlobs"), strings.Join(options.ExcludeGlobs, "\n")})
		}
	}
	tableRowCount := len(rows)
	rows = describedTableRows(locale, "summary", rows[0], rows[1:])
	return AnalysisSheet{Sheet: text(locale, "summary"), Data: rows, AutoFilterRef: autoFilterRef(tableRowCount, 2), StickyRowsCount: 1}
}

func analysisSettingsRows(locale Locale, settings AnalysisWorkbookSettings) [][]Cell {
	excelLocale := settings.ExcelLocale
	if excelLocale == "" {
		excelLocale = "auto"
	}
	rows := [][]Cell{
		{text(locale, "includeRelatedIncludeTreesForUnresolved"), enabledText(settings.IncludeRelatedIncludeTreesForUnresolved, locale)},
		{text(locale, "forceRelatedIncludeTreeAnalysis"), forcedText(settings.ForceRelatedIncludeTreeAnalysis, locale)},
		{text(locale, "skipTypeInference"), enabledText(settings.SkipTypeInference, locale)},
		{text(locale, "includeAnalysisTypeDetails"), enabledText(settings.IncludeAnalysisTypeDetails, locale)},
		{text(locale, "excelLocale"), excelLocaleText(excelLocale, locale)},
	}
	if settings.AnalysisFileCount > 0 {
		rows = append(rows, []Cell{text(locale, "analysisFileCount"), settings.AnalysisFileCount})
	}
	if len(settings.IncludeGlobs) > 0 {
		rows = append(rows, []Cell{text(locale, "includeGlobs"), strings.Join(settings.IncludeGlobs, "\n")})
	}
	if len(settings.ExcludeGlobs) > 0 {
		rows = append(rows, []Cell{text(locale, "excludeGlobs"), strings.Join(settings.ExcludeGlobs, "\n")})
	}
	rows = append(rows, []Cell{text(locale, "respectGitIgnore"), enabledText(settings.RespectGitIgnore, locale)})
	return rows
}

// includeTreeRelation is the subset of an include edge that belongs to the
// target document's include tree.  The TypeScript implementation computes
// these relations with breadth-first distances instead of treating every edge
// as a direct child of the target.
type includeTreeRelation struct {
	edge      graph.Edge
	source    graph.Node
	target    graph.Node
	direction string
	depth     int
}

func includeTreeSheet(context analysisContext, locale Locale, relations []includeTreeRelation) AnalysisSheet {
	header := []Cell{
		text(locale, "direction"), text(locale, "depth"), text(locale, "sourceFile"),
		text(locale, "includeFile"), text(locale, "includePath"), text(locale, "includeMode"),
		text(locale, "exists"), text(locale, "resolvedTarget"), text(locale, "line"), text(locale, "column"),
	}
	rows := [][]Cell{header}
	sort.SliceStable(relations, func(i, j int) bool {
		order := map[string]int{"ancestor": 0, "descendant": 1, "relative": 2}
		if order[relations[i].direction] != order[relations[j].direction] {
			return order[relations[i].direction] < order[relations[j].direction]
		}
		if relations[i].depth != relations[j].depth {
			return relations[i].depth < relations[j].depth
		}
		leftSource := fileNameForNode(relations[i].source)
		rightSource := fileNameForNode(relations[j].source)
		if leftSource != rightSource {
			return leftSource < rightSource
		}
		return fileNameForNode(relations[i].target) < fileNameForNode(relations[j].target)
	})
	for _, relation := range relations {
		edge := relation.edge
		path := edge.Label
		mode := ""
		exists := false
		resolved := ""
		if edge.Include != nil {
			if edge.Include.Path != "" {
				path = edge.Include.Path
			}
			mode = edge.Include.Mode
			exists = edge.Include.Exists
			resolved = fileNameForURI(edge.Include.ResolvedURI, context.filesByURI)
			if resolved == "" {
				resolved = edge.Include.ActualPath
			}
			if resolved == "" {
				resolved = edge.Include.ResolvedPath
			}
		}
		if !exists && relation.target.Exists != nil {
			exists = *relation.target.Exists
		}
		if resolved == "" {
			resolved = fileNameForNode(relation.target)
		}
		line, column := 0, 0
		if len(edge.Ranges) > 0 {
			line = edge.Ranges[0].Range.Start.Line + 1
			column = edge.Ranges[0].Range.Start.Character + 1
		}
		existsText := text(locale, "no")
		if exists {
			existsText = text(locale, "yes")
		}
		row := []Cell{
			includeTreeDirectionText(relation.direction, locale), relation.depth,
			fileNameForNode(relation.source), fileNameForNode(relation.target), path,
		}
		// Legacy hand-built payloads may not carry include metadata at all. Keep
		// their compact rows readable while real server graph edges (which always
		// include a range) expose the complete TypeScript-compatible columns.
		if mode != "" || len(edge.Ranges) > 0 || edge.Include != nil && (edge.Include.ActualPath != "" || edge.Include.PathCaseMatches) {
			row = append(row, mode, existsText, resolved, line, column)
		} else {
			row = append(row, existsText, resolved)
		}
		rows = append(rows, row)
	}
	tableRowCount := len(rows)
	rows = describedTableRows(locale, "includeTree", header, rows[1:])
	return AnalysisSheet{Sheet: text(locale, "includeTree"), Data: rows, AutoFilterRef: autoFilterRef(tableRowCount, len(header)), StickyRowsCount: 1}
}

func includeTreeRelations(payload graph.Payload, context analysisContext) []includeTreeRelation {
	includeEdges := context.includeGraph.edges
	outgoing := context.includeGraph.outgoing
	incoming := context.includeGraph.incoming
	if includeEdges == nil {
		includeIndex := newIncludeGraphIndex(payload, context.nodesByID)
		includeEdges = includeIndex.edges
		outgoing = includeIndex.outgoing
		incoming = includeIndex.incoming
	}
	if len(includeEdges) == 0 {
		return nil
	}
	if context.workspaceWide {
		result := make([]includeTreeRelation, 0, len(includeEdges))
		for _, edge := range includeEdges {
			source, sourceOK := context.nodesByID[edge.Source]
			target, targetOK := context.nodesByID[edge.Target]
			if !sourceOK || !targetOK {
				continue
			}
			result = append(result, includeTreeRelation{edge: edge, source: source, target: target, direction: "descendant", depth: 1})
		}
		return result
	}
	targetIDs := map[string]struct{}{}
	for id, node := range context.nodesByID {
		if node.URI == context.targetURI || id == context.targetURI {
			targetIDs[id] = struct{}{}
		}
	}
	if len(targetIDs) == 0 {
		return nil
	}
	descendantDepths := includeTreeDepths(targetIDs, outgoing, true)
	ancestorDepths := includeTreeDepths(targetIDs, incoming, false)
	relativeDepths := relatedIncludeTreeDepths(ancestorDepths, outgoing, descendantDepths)
	result := make([]includeTreeRelation, 0, len(includeEdges))
	for _, edge := range includeEdges {
		source, sourceOK := context.nodesByID[edge.Source]
		target, targetOK := context.nodesByID[edge.Target]
		if !sourceOK || !targetOK {
			continue
		}
		direction, depth, ok := includeTreeRelationForEdge(edge, targetIDs, descendantDepths, ancestorDepths, relativeDepths)
		if !ok {
			continue
		}
		result = append(result, includeTreeRelation{edge: edge, source: source, target: target, direction: direction, depth: depth})
	}
	return result
}

func includeTreeDepths(roots map[string]struct{}, links map[string][]graph.Edge, forward bool) map[string]int {
	depths := make(map[string]int, len(roots))
	queue := make([]string, 0, len(roots))
	for root := range roots {
		depths[root] = 0
		queue = append(queue, root)
	}
	for index := 0; index < len(queue); index++ {
		current := queue[index]
		for _, edge := range links[current] {
			next := edge.Source
			if forward {
				next = edge.Target
			}
			if _, seen := depths[next]; seen {
				continue
			}
			depths[next] = depths[current] + 1
			queue = append(queue, next)
		}
	}
	return depths
}

func relatedIncludeTreeDepths(ancestors map[string]int, outgoing map[string][]graph.Edge, descendants map[string]int) map[string]int {
	relative := map[string]int{}
	for ancestor, ancestorDepth := range ancestors {
		if ancestorDepth == 0 {
			continue
		}
		queue := []struct {
			id    string
			depth int
		}{{ancestor, ancestorDepth}}
		visited := map[string]struct{}{ancestor: {}}
		for index := 0; index < len(queue); index++ {
			current := queue[index]
			for _, edge := range outgoing[current.id] {
				next := edge.Target
				if _, seen := visited[next]; seen {
					continue
				}
				visited[next] = struct{}{}
				nextDepth := current.depth + 1
				if _, isAncestor := ancestors[next]; !isAncestor {
					if _, isDescendant := descendants[next]; !isDescendant {
						if existing, ok := relative[next]; !ok || nextDepth < existing {
							relative[next] = nextDepth
						}
					}
				}
				queue = append(queue, struct {
					id    string
					depth int
				}{next, nextDepth})
			}
		}
	}
	return relative
}

func includeTreeRelationForEdge(edge graph.Edge, targets map[string]struct{}, descendants, ancestors, relatives map[string]int) (string, int, bool) {
	sourceDesc, sourceDescOK := descendants[edge.Source]
	targetDesc, targetDescOK := descendants[edge.Target]
	if sourceDescOK && targetDescOK && targetDesc > sourceDesc {
		return "descendant", targetDesc, true
	}
	sourceAnc, sourceAncOK := ancestors[edge.Source]
	_, targetAncOK := ancestors[edge.Target]
	_, targetIsRoot := targets[edge.Target]
	if sourceAncOK && (targetAncOK || targetIsRoot) {
		return "ancestor", sourceAnc, true
	}
	sourceRel, sourceRelOK := relatives[edge.Source]
	targetRel, targetRelOK := relatives[edge.Target]
	if sourceAncOK && targetRelOK {
		return "relative", targetRel, true
	}
	if sourceAncOK && targetDescOK && !targetIsRoot {
		return "relative", sourceAnc + targetDesc, true
	}
	if sourceRelOK && targetRelOK {
		return "relative", targetRel, true
	}
	if sourceRelOK && targetDescOK {
		return "relative", sourceRel + targetDesc, true
	}
	return "", 0, false
}

func includeTreeDirectionText(direction string, locale Locale) string {
	switch direction {
	case "ancestor":
		return text(locale, "ancestor")
	case "relative":
		return text(locale, "relative")
	default:
		return text(locale, "descendant")
	}
}

type analysisSummaryModel struct {
	reviewRows       [][]Cell
	declarationRows  [][]Cell
	includeUsageRows [][]Cell
	topRows          [][]Cell
	unusedRows       [][]Cell
}

func newAnalysisSummaryModel(context analysisContext, locale Locale) analysisSummaryModel {
	return analysisSummaryModel{
		reviewRows:       reviewPriorityRows(context, locale),
		declarationRows:  declarationKindSummaryRows(context, locale),
		includeUsageRows: includeUsageSummaryRows(context, locale),
		topRows:          topReferencedRows(context, locale),
		unusedRows:       unusedByKindRows(context, locale),
	}
}

func analysisSummarySheet(context analysisContext, locale Locale) AnalysisSheet {
	return analysisSummarySheetFromModel(newAnalysisSummaryModel(context, locale), locale)
}

func analysisSummarySheetFromModel(model analysisSummaryModel, locale Locale) AnalysisSheet {
	rows := analysisSummaryRowsFromModel(model, locale)
	chartStartRow := len(rows) + 3
	rows = append(rows, []Cell{}, []Cell{StyledCell{Value: text(locale, "analysisCharts"), Style: cellPresentationSection, ColumnSpan: 1}})
	rows = append(rows, make([][]Cell, 34)...)
	return AnalysisSheet{Sheet: text(locale, "analysisSummary"), Data: rows, Images: analysisSummaryImagesFromModel(model, locale, chartStartRow)}
}

func chartDataSheetFromModel(model analysisSummaryModel, locale Locale) AnalysisSheet {
	rows := chartDataRowsFromModel(model, locale)
	return AnalysisSheet{Sheet: text(locale, "chartData"), Data: rows, Hidden: true, StickyRowsCount: 2}
}

// analysisSummaryRows mirrors the TypeScript workbook's review-oriented
// sections.  Keeping each section as a normal row table lets both the XLSX
// writer and CSV-like callers consume the same intermediate representation.
func analysisSummaryRows(context analysisContext, locale Locale) [][]Cell {
	return analysisSummaryRowsFromModel(newAnalysisSummaryModel(context, locale), locale)
}

func analysisSummaryRowsFromModel(model analysisSummaryModel, locale Locale) [][]Cell {
	rows := [][]Cell{}
	appendSection := func(title, descriptionKey string, headers []Cell, values [][]Cell) {
		if len(rows) > 0 {
			rows = append(rows, []Cell{})
		}
		rows = append(rows, describedSectionRows(title, locale, descriptionKey, headers, values)...)
	}
	appendSection(text(locale, "reviewPriority"), "reviewPriority",
		[]Cell{text(locale, "metric"), text(locale, "count"), text(locale, "status"), text(locale, "action")},
		model.reviewRows)
	appendSection(text(locale, "externalReferenceSummary"), "externalReferenceSummary",
		[]Cell{text(locale, "kind"), text(locale, "total"), text(locale, "usedCount"), text(locale, "unusedCount"), text(locale, "usageCount"), text(locale, "bar")},
		model.declarationRows)
	appendSection(text(locale, "includeUsageSummary"), "includeUsageSummary",
		[]Cell{text(locale, "usageKind"), text(locale, "count"), text(locale, "bar")},
		model.includeUsageRows)
	appendSection(text(locale, "topReferencedDeclarations"), "topReferencedDeclarations",
		[]Cell{text(locale, "name"), text(locale, "kind"), text(locale, "file"), text(locale, "line"), text(locale, "usageCount"), text(locale, "referenceCount"), text(locale, "assignmentCount"), text(locale, "callCount")},
		model.topRows)
	appendSection(text(locale, "unusedByKind"), "unusedByKind",
		[]Cell{text(locale, "kind"), text(locale, "unusedCount"), text(locale, "total"), text(locale, "unusedRate"), text(locale, "bar")},
		model.unusedRows)
	return rows
}

func chartDataRowsFromModel(model analysisSummaryModel, locale Locale) [][]Cell {
	rows := [][]Cell{}
	appendSection := func(title, descriptionKey string, headers []Cell, values [][]Cell) {
		if len(rows) > 0 {
			rows = append(rows, []Cell{})
		}
		rows = append(rows, describedSectionRows(title, locale, descriptionKey, headers, values)...)
	}
	appendSection(text(locale, "reviewPriority"), "chartData",
		[]Cell{text(locale, "metric"), text(locale, "count"), text(locale, "status")},
		trimRows(model.reviewRows, 3))
	appendSection(text(locale, "externalReferenceSummary"), "chartData",
		[]Cell{text(locale, "kind"), text(locale, "total"), text(locale, "usedCount"), text(locale, "unusedCount"), text(locale, "usageCount")},
		trimRows(model.declarationRows, 5))
	appendSection(text(locale, "includeUsageSummary"), "chartData",
		[]Cell{text(locale, "usageKind"), text(locale, "count")},
		trimRows(model.includeUsageRows, 2))
	appendSection(text(locale, "unusedByKind"), "chartData",
		[]Cell{text(locale, "kind"), text(locale, "unusedCount"), text(locale, "total"), text(locale, "unusedRate")},
		trimRows(model.unusedRows, 4))
	return rows
}

func reviewPriorityRows(context analysisContext, locale Locale) [][]Cell {
	implicitAssignments := 0
	for _, usage := range context.implicitAssignments {
		implicitAssignments += countOrOne(usage.count)
	}
	items := []struct {
		label  string
		count  int
		action string
		tone   cellPresentation
	}{
		{text(locale, "unreferencedDeclarations"), len(context.unusedDeclarations), text(locale, "reviewUnusedAction"), cellPresentationToneDanger},
		{text(locale, "unresolved"), context.unresolvedCount, text(locale, "reviewUnresolvedAction"), cellPresentationToneDanger},
		{text(locale, "implicitGlobals"), len(context.implicitGlobals), text(locale, "reviewImplicitGlobalsAction"), cellPresentationToneWarning},
		{text(locale, "implicitGlobalAssignments"), implicitAssignments, text(locale, "reviewImplicitGlobalAssignmentsAction"), cellPresentationToneInfo},
		{text(locale, "externalUsageCount"), usageRowsCount(context.externalUsages), text(locale, "reviewExternalUsagesAction"), cellPresentationToneInfo},
		{text(locale, "includedUsageCount"), usageRowsCount(context.includedUsages), text(locale, "reviewIncludedUsagesAction"), cellPresentationToneInfo},
	}
	rows := make([][]Cell, 0, len(items))
	for _, item := range items {
		status := reviewStatus(item.count, locale)
		if item.count == 0 && (item.label == text(locale, "externalUsageCount") || item.label == text(locale, "includedUsageCount")) {
			status = text(locale, "none")
		}
		tone := item.tone
		if item.count == 0 {
			tone = cellPresentationToneGood
			if item.label == text(locale, "implicitGlobalAssignments") || item.label == text(locale, "includedUsageCount") {
				tone = cellPresentationToneNeutral
			} else if item.label == text(locale, "externalUsageCount") {
				tone = cellPresentationToneWarning
			}
		}
		rows = append(rows, []Cell{item.label, item.count, toneCell(status, tone), item.action})
	}
	return rows
}

func usageRowsCount(rows []usageRow) int {
	total := 0
	for _, row := range rows {
		total += countOrOne(row.count)
	}
	return total
}

func declarationKindSummaryRows(context analysisContext, locale Locale) [][]Cell {
	type summary struct{ total, used, unused, usage int }
	counts := map[string]summary{}
	for _, declaration := range context.targetDeclarations {
		key := declarationKindLabel(declaration.DeclarationKind, locale)
		entry := counts[key]
		entry.total++
		usage := context.targetUsageCounts[declaration.ID]
		entry.usage += usage.references + usage.assignments + usage.calls
		if usage.references+usage.assignments+usage.calls > 0 {
			entry.used++
		} else {
			entry.unused++
		}
		counts[key] = entry
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	max := 0
	for _, key := range keys {
		if counts[key].total > max {
			max = counts[key].total
		}
	}
	rows := make([][]Cell, 0, len(keys))
	for _, key := range keys {
		entry := counts[key]
		rows = append(rows, []Cell{key, entry.total, entry.used, entry.unused, entry.usage, countBar(entry.total, max)})
	}
	return rows
}

func includeUsageSummaryRows(context analysisContext, locale Locale) [][]Cell {
	counts := map[string]int{}
	for _, usage := range context.includedUsages {
		counts[usage.kind] += countOrOne(usage.count)
	}
	rows := make([][]Cell, 0, len(counts))
	keys := make([]string, 0, len(counts))
	for kind := range counts {
		keys = append(keys, kind)
	}
	sort.Strings(keys)
	max := 0
	for _, key := range keys {
		if counts[key] > max {
			max = counts[key]
		}
	}
	for _, key := range keys {
		rows = append(rows, []Cell{usageKindLabel(key, locale), counts[key], countBar(counts[key], max)})
	}
	return rows
}

func topReferencedRows(context analysisContext, locale Locale) [][]Cell {
	type row struct {
		declaration graph.Node
		counts      usageCounts
		total       int
	}
	items := make([]row, 0, len(context.targetDeclarations))
	for _, declaration := range context.targetDeclarations {
		counts := context.usageCounts[declaration.ID]
		total := counts.references + counts.assignments + counts.calls
		if total > 0 {
			items = append(items, row{declaration: declaration, counts: counts, total: total})
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].total != items[j].total {
			return items[i].total > items[j].total
		}
		return items[i].declaration.Label < items[j].declaration.Label
	})
	if len(items) > 10 {
		items = items[:10]
	}
	rows := make([][]Cell, 0, len(items))
	for _, item := range items {
		rows = append(rows, []Cell{
			item.declaration.Label, declarationKindLabel(item.declaration.DeclarationKind, locale),
			fileNameForURI(item.declaration.URI, context.filesByURI), lineNumber(item.declaration), item.total,
			item.counts.references, item.counts.assignments, item.counts.calls,
		})
	}
	return rows
}

func unusedByKindRows(context analysisContext, locale Locale) [][]Cell {
	type summary struct{ unused, total int }
	counts := map[string]summary{}
	for _, declaration := range context.targetDeclarations {
		key := declarationKindLabel(declaration.DeclarationKind, locale)
		entry := counts[key]
		entry.total++
		if isUnusedDeclaration(declaration, context.targetUsageCounts[declaration.ID]) {
			entry.unused++
		}
		counts[key] = entry
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	max := 0
	for _, key := range keys {
		if counts[key].unused > max {
			max = counts[key].unused
		}
	}
	rows := make([][]Cell, 0, len(keys))
	for _, key := range keys {
		entry := counts[key]
		rate := 0.0
		if entry.total > 0 {
			rate = float64(entry.unused) / float64(entry.total)
		}
		rows = append(rows, []Cell{key, entry.unused, entry.total, percentCell(rate), countBar(entry.unused, max)})
	}
	return rows
}

func countBar(value, max int) string {
	if value <= 0 || max <= 0 {
		return ""
	}
	const width = 10
	count := value * width / max
	if count == 0 {
		count = 1
	}
	return strings.Repeat("█", count)
}

func trimRows(rows [][]Cell, columns int) [][]Cell {
	result := make([][]Cell, 0, len(rows))
	for _, row := range rows {
		if len(row) > columns {
			result = append(result, row[:columns])
		} else {
			result = append(result, row)
		}
	}
	return result
}

func analysisSummaryImages(context analysisContext, locale Locale, startRow int) []AnalysisImage {
	return analysisSummaryImagesFromModel(newAnalysisSummaryModel(context, locale), locale, startRow)
}

func analysisSummaryImagesFromModel(model analysisSummaryModel, locale Locale, startRow int) []AnalysisImage {
	pool := &singlePNGEncoderBufferPool{}
	return []AnalysisImage{
		{File: chartPNG(model.declarationRows, 1, pool), Extension: ".png", Cell: fmt.Sprintf("J%d", startRow), AltText: text(locale, "externalReferenceSummary"), Name: "external-reference-summary"},
		{File: chartPNG(model.reviewRows, 1, pool), Extension: ".png", Cell: fmt.Sprintf("J%d", startRow+17), AltText: text(locale, "issueSummary"), Name: "issue-summary"},
	}
}

// chartPNG renders a deliberately small, dependency-free bar image.  Labels
// and exact values remain available in the adjacent worksheet rows; the image
// exists for parity with the TypeScript workbook's visual summary charts.
func chartPNG(rows [][]Cell, valueColumn int, pool png.EncoderBufferPool) []byte {
	const width, height = 640, 240
	background := color.RGBA{R: 248, G: 251, B: 255, A: 255}
	bar := color.RGBA{R: 37, G: 99, B: 235, A: 255}
	canvas := image.NewPaletted(image.Rect(0, 0, width, height), color.Palette{background, bar})
	max := 1
	for _, row := range rows {
		if valueColumn >= len(row) {
			continue
		}
		if value, ok := row[valueColumn].(int); ok && value > max {
			max = value
		}
	}
	barHeight := 24
	for index, row := range rows {
		if valueColumn >= len(row) || index >= 8 {
			break
		}
		value, ok := row[valueColumn].(int)
		if !ok || value <= 0 {
			continue
		}
		y := 12 + index*barHeight
		barWidth := value * 560 / max
		if barWidth < 3 {
			barWidth = 3
		}
		draw.Draw(canvas, image.Rect(24, y, 24+barWidth, y+14), &image.Uniform{C: bar}, image.Point{}, draw.Src)
	}
	var output bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.BestSpeed, BufferPool: pool}
	if err := encoder.Encode(&output, canvas); err != nil {
		return nil
	}
	return output.Bytes()
}

type singlePNGEncoderBufferPool struct {
	buffer *png.EncoderBuffer
}

func (pool *singlePNGEncoderBufferPool) Get() *png.EncoderBuffer {
	buffer := pool.buffer
	pool.buffer = nil
	return buffer
}

func (pool *singlePNGEncoderBufferPool) Put(buffer *png.EncoderBuffer) {
	pool.buffer = buffer
}

func declarationsSheet(context analysisContext, locale Locale) AnalysisSheet {
	rows := [][]Cell{{text(locale, "file"), text(locale, "name"), text(locale, "kind"), text(locale, "memberOf"), text(locale, "bindingScope"), text(locale, "procedureKind"), text(locale, "inferredType"), text(locale, "returnType"), text(locale, "parameters"), text(locale, "implicit"), text(locale, "array"), text(locale, "line"), text(locale, "column"), text(locale, "references"), text(locale, "assignments"), text(locale, "calls"), text(locale, "status")}}
	for _, node := range context.targetDeclarations {
		counts := context.usageCounts[node.ID]
		rows = append(rows, []Cell{
			fileNameForURI(node.URI, context.filesByURI),
			node.Label,
			declarationKindLabel(node.DeclarationKind, locale),
			node.MemberOf,
			scopeText(node.BindingScope, locale),
			node.ProcedureKind,
			declarationTypeDisplay(node),
			declarationReturnTypeDisplay(node),
			declarationParametersDisplay(node),
			yesNoText(node.Implicit, locale),
			arrayDisplay(node, locale),
			lineNumber(node),
			columnNumber(node),
			counts.references,
			counts.assignments,
			counts.calls,
			usedStatus(counts, locale),
		})
	}
	tableRowCount := len(rows)
	rows = describedTableRows(locale, "declarations", rows[0], rows[1:])
	return AnalysisSheet{Sheet: text(locale, "declarationsSheet"), Data: rows, AutoFilterRef: autoFilterRef(tableRowCount, 17), StickyRowsCount: 1}
}

func usageSheet(usages []usageRow, name string, locale Locale) AnalysisSheet {
	rows := [][]Cell{{text(locale, "usageKind"), text(locale, "role"), text(locale, "usageFile"), text(locale, "usageOwner"), text(locale, "declarationFile"), text(locale, "declarationName"), text(locale, "declarationKind"), text(locale, "inferredType"), text(locale, "line"), text(locale, "column"), text(locale, "count")}}
	for _, usage := range usages {
		rows = append(rows, []Cell{usageKindLabel(usage.kind, locale), roleLabel(usage.role, locale), usage.sourceFile, usage.sourceOwner, usage.targetFile, usage.name, declarationKindLabel(usage.declKind, locale), declarationUsageTypeName(usage.declKind, usage.typeName), usage.line, usage.column, usage.count})
	}
	tableRowCount := len(rows)
	key := "internalUsages"
	if name == text(locale, "externalUsage") {
		key = "externalFileUsages"
	}
	rows = describedTableRows(locale, key, rows[0], rows[1:])
	return AnalysisSheet{Sheet: name, Data: rows, AutoFilterRef: autoFilterRef(tableRowCount, 11), StickyRowsCount: 1}
}

func includedUsageSheet(usages []usageRow, locale Locale) AnalysisSheet {
	rows := [][]Cell{{text(locale, "usageKind"), text(locale, "role"), text(locale, "includeFile"), text(locale, "name"), text(locale, "kind"), text(locale, "inferredType"), text(locale, "usedFromFile"), text(locale, "line"), text(locale, "column"), text(locale, "count")}}
	for _, usage := range usages {
		rows = append(rows, []Cell{usageKindLabel(usage.kind, locale), roleLabel(usage.role, locale), usage.targetFile, usage.name, declarationKindLabel(usage.declKind, locale), declarationUsageTypeName(usage.declKind, usage.typeName), usage.sourceFile, usage.line, usage.column, usage.count})
	}
	tableRowCount := len(rows)
	rows = describedTableRows(locale, "includedSymbolUsages", rows[0], rows[1:])
	return AnalysisSheet{Sheet: text(locale, "includedUsage"), Data: rows, AutoFilterRef: autoFilterRef(tableRowCount, 10), StickyRowsCount: 1}
}

func memberUsageSheet(context analysisContext, locale Locale) AnalysisSheet {
	rows := [][]Cell{{text(locale, "usageKind"), text(locale, "role"), text(locale, "receiver"), text(locale, "memberName"), text(locale, "expression"), text(locale, "usageFile"), text(locale, "line"), text(locale, "column"), text(locale, "count")}}
	for _, node := range context.memberUsages {
		role := node.Role
		if role == "" {
			role = "member"
		}
		rows = append(rows, []Cell{text(locale, "member"), roleLabel(role, locale), node.ReceiverName, node.MemberName, node.FullPath, fileNameForURI(node.URI, context.filesByURI), lineNumber(node), columnNumber(node), 1})
	}
	tableRowCount := len(rows)
	rows = describedTableRows(locale, "memberUsages", rows[0], rows[1:])
	return AnalysisSheet{Sheet: text(locale, "memberUsage"), Data: rows, AutoFilterRef: autoFilterRef(tableRowCount, 9), StickyRowsCount: 1}
}

func implicitGlobalsSheet(context analysisContext, locale Locale) AnalysisSheet {
	rows := [][]Cell{{text(locale, "file"), text(locale, "name"), text(locale, "kind"), text(locale, "inferredType"), text(locale, "bindingScope"), text(locale, "line"), text(locale, "column"), text(locale, "usageCount"), text(locale, "references"), text(locale, "assignments"), text(locale, "calls")}}
	for _, node := range context.implicitGlobals {
		counts := context.usageCounts[node.ID]
		rows = append(rows, []Cell{fileNameForURI(node.URI, context.filesByURI), node.Label, declarationKindLabel(node.DeclarationKind, locale), declarationTypeDisplay(node), scopeText(node.BindingScope, locale), lineNumber(node), columnNumber(node), counts.references + counts.assignments + counts.calls, counts.references, counts.assignments, counts.calls})
	}
	tableRowCount := len(rows)
	rows = describedTableRows(locale, "implicitGlobals", rows[0], rows[1:])
	return AnalysisSheet{Sheet: text(locale, "implicitGlobalsSheet"), Data: rows, AutoFilterRef: autoFilterRef(tableRowCount, 11), StickyRowsCount: 1}
}

func implicitGlobalAssignmentsSheet(context analysisContext, locale Locale) AnalysisSheet {
	rows := [][]Cell{{text(locale, "implicitGlobalFile"), text(locale, "implicitGlobalName"), text(locale, "assignmentFile"), text(locale, "assignmentTarget"), text(locale, "assignmentTargetFile"), text(locale, "includeDepth"), text(locale, "line"), text(locale, "column"), text(locale, "count")}}
	for _, usage := range context.implicitAssignments {
		rows = append(rows, []Cell{usage.targetFile, usage.name, usage.sourceFile, usage.name, usage.targetFile, usage.includeDepth, usage.line, usage.column, usage.count})
	}
	tableRowCount := len(rows)
	rows = describedTableRows(locale, "implicitGlobalAssignments", rows[0], rows[1:])
	return AnalysisSheet{Sheet: text(locale, "implicitAssignments"), Data: rows, AutoFilterRef: autoFilterRef(tableRowCount, 9), StickyRowsCount: 1}
}

func unusedSheet(context analysisContext, locale Locale) AnalysisSheet {
	rows := [][]Cell{{text(locale, "file"), text(locale, "name"), text(locale, "kind"), text(locale, "memberOf"), text(locale, "bindingScope"), text(locale, "inferredType"), text(locale, "implicit"), text(locale, "line"), text(locale, "column"), text(locale, "references"), text(locale, "assignments"), text(locale, "calls"), text(locale, "status")}}
	for _, node := range context.unusedDeclarations {
		counts := context.usageCounts[node.ID]
		rows = append(rows, []Cell{fileNameForURI(node.URI, context.filesByURI), node.Label, declarationKindLabel(node.DeclarationKind, locale), node.MemberOf, scopeText(node.BindingScope, locale), declarationTypeDisplay(node), yesNoText(node.Implicit, locale), lineNumber(node), columnNumber(node), counts.references, counts.assignments, counts.calls, text(locale, "unusedStatus")})
	}
	tableRowCount := len(rows)
	rows = describedTableRows(locale, "unused", rows[0], rows[1:])
	return AnalysisSheet{Sheet: text(locale, "unusedSheet"), Data: rows, AutoFilterRef: autoFilterRef(tableRowCount, 13), StickyRowsCount: 1}
}

func unresolvedSheet(context analysisContext, locale Locale) AnalysisSheet {
	rows := [][]Cell{{text(locale, "usageKind"), text(locale, "role"), text(locale, "kind"), text(locale, "source"), text(locale, "name"), text(locale, "file"), text(locale, "line"), text(locale, "column"), text(locale, "count")}}
	for _, unresolved := range context.unresolvedUsages {
		rows = append(rows, []Cell{usageKindLabel(unresolved.usageKind, locale), roleLabel(unresolved.role, locale), unresolvedKindLabel(unresolved.kind, locale), unresolved.source, unresolved.name, unresolved.file, unresolved.line, unresolved.column, unresolved.count})
	}
	tableRowCount := len(rows)
	rows = describedTableRows(locale, "unresolved", rows[0], rows[1:])
	return AnalysisSheet{Sheet: text(locale, "unresolvedSheet"), Data: rows, AutoFilterRef: autoFilterRef(tableRowCount, 9), StickyRowsCount: 1}
}
