package excel

import (
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/graph"
)

type Locale string

const (
	LocaleEnglish  Locale = "en"
	LocaleJapanese Locale = "ja"
)

// Cell is a worksheet cell value in the analysis sheet intermediate model.
type Cell = any

// AnalysisSheet describes a generated worksheet before it is written to XLSX.
type AnalysisSheet struct {
	Sheet           string
	Data            [][]Cell
	Images          []AnalysisImage
	Shapes          []AnalysisShape
	Hidden          bool
	AutoFilterRef   string
	StickyRowsCount int
}

// AnalysisShape is an Excel-native diagram shape anchored to a worksheet cell.
type AnalysisShape struct {
	Cell      string
	Type      string
	Width     uint
	Height    uint
	OffsetX   int
	OffsetY   int
	Text      string
	AltText   string
	Name      string
	FillColor string
	LineColor string
	TextColor string
	Bold      bool
}

// AnalysisImage is a workbook image placed over a worksheet cell.  PNG keeps
// the writer self-contained and avoids requiring an SVG decoder at runtime.
type AnalysisImage struct {
	File      []byte
	Extension string
	Cell      string
	AltText   string
	Name      string
}

// AnalysisProgressEvent reports analysis sheet generation progress.
type AnalysisProgressEvent struct {
	Label       string
	Current     int
	Total       int
	Detail      string
	ActiveItems []string
}

// AnalysisSheetsOptions controls graph-to-sheet rendering metadata.
type AnalysisSheetsOptions struct {
	GeneratedAt       time.Time
	TargetURI         string
	AnalysisFileCount int
	IncludeGlobs      []string
	ExcludeGlobs      []string
	Settings          *AnalysisWorkbookSettings
	Progress          func(AnalysisProgressEvent)
	// Cancelled allows callers that own a long-running export to stop sheet
	// generation between expensive graph-to-row phases.
	Cancelled func() bool
}

// AnalysisWorkbookSettings records the export settings shown on the summary sheet.
type AnalysisWorkbookSettings struct {
	ExcelLocale                             string
	IncludeRelatedIncludeTreesForUnresolved bool
	ForceRelatedIncludeTreeAnalysis         bool
	SkipTypeInference                       bool
	IncludeAnalysisTypeDetails              bool
	AnalysisFileCount                       int
	IncludeGlobs                            []string
	ExcludeGlobs                            []string
	RespectGitIgnore                        bool
}

// CreateAnalysisSheets converts an ASP graph payload into analysis workbook sheets.
func CreateAnalysisSheets(payload graph.Payload, locale Locale, options AnalysisSheetsOptions) []AnalysisSheet {
	if locale != LocaleEnglish {
		locale = LocaleJapanese
	}
	reportAnalysisProgress(options, "excel.normalizeGraph", 0, 1, "graph", []string{"graph"})
	if analysisCancelled(options) {
		return nil
	}
	context := newAnalysisContext(payload, options)
	includeRelations := includeTreeRelations(payload, context)
	var summaryData *analysisSummaryModel
	getSummaryData := func() *analysisSummaryModel {
		if summaryData == nil {
			model := newAnalysisSummaryModel(context, locale)
			summaryData = &model
		}
		return summaryData
	}
	reportAnalysisProgress(options, "excel.normalizeGraph", 1, 1, "graph", nil)
	reportAnalysisProgress(options, "excel.analysisContext", 1, 1, context.targetFileName, nil)
	builders := []struct {
		name  string
		build func() AnalysisSheet
	}{
		{text(locale, "summary"), func() AnalysisSheet { return summarySheet(payload, context, locale, options) }},
		{text(locale, "includeTree"), func() AnalysisSheet { return includeTreeSheet(context, locale, includeRelations) }},
		{text(locale, "includeTreeDiagram"), func() AnalysisSheet { return includeTreeDiagramSheet(context, locale, includeRelations) }},
		{text(locale, "analysisSummary"), func() AnalysisSheet { return analysisSummarySheetFromModel(*getSummaryData(), locale) }},
		{text(locale, "chartData"), func() AnalysisSheet { return chartDataSheetFromModel(*getSummaryData(), locale) }},
		{text(locale, "declarationsSheet"), func() AnalysisSheet { return declarationsSheet(context, locale) }},
		{text(locale, "fileLocalUsage"), func() AnalysisSheet {
			return usageSheet(context.internalUsages, text(locale, "fileLocalUsage"), locale)
		}},
		{text(locale, "externalUsage"), func() AnalysisSheet { return usageSheet(context.externalUsages, text(locale, "externalUsage"), locale) }},
		{text(locale, "includedUsage"), func() AnalysisSheet { return includedUsageSheet(context.includedUsages, locale) }},
		{text(locale, "memberUsage"), func() AnalysisSheet { return memberUsageSheet(context, locale) }},
		{text(locale, "implicitGlobalsSheet"), func() AnalysisSheet { return implicitGlobalsSheet(context, locale) }},
		{text(locale, "implicitAssignments"), func() AnalysisSheet { return implicitGlobalAssignmentsSheet(context, locale) }},
		{text(locale, "unusedSheet"), func() AnalysisSheet { return unusedSheet(context, locale) }},
		{text(locale, "unresolvedSheet"), func() AnalysisSheet { return unresolvedSheet(context, locale) }},
	}
	sheets := make([]AnalysisSheet, 0, len(builders))
	for index, builder := range builders {
		if analysisCancelled(options) {
			return nil
		}
		sheets = append(sheets, builder.build())
		reportAnalysisProgress(options, "excel.sheet", index+1, len(builders), builder.name, []string{builder.name})
		if builder.name == text(locale, "analysisSummary") {
			reportAnalysisProgress(options, "excel.analysisSummary", 1, 1, builder.name, nil)
		}
	}
	reportAnalysisProgress(options, "excel.sheets", len(builders), len(builders), "", nil)
	return sheets
}

func analysisCancelled(options AnalysisSheetsOptions) bool {
	return options.Cancelled != nil && options.Cancelled()
}

func reportAnalysisProgress(options AnalysisSheetsOptions, label string, current int, total int, detail string, activeItems []string) {
	if options.Progress == nil {
		return
	}
	options.Progress(AnalysisProgressEvent{
		Label:       label,
		Current:     current,
		Total:       total,
		Detail:      detail,
		ActiveItems: append([]string(nil), activeItems...),
	})
}

type analysisContext struct {
	targetURI            string
	targetFileName       string
	workspaceWide        bool
	includeGraph         includeGraphIndex
	nodesByID            map[string]graph.Node
	filesByURI           map[string]graph.Node
	includedURIs         map[string]struct{}
	declarations         []graph.Node
	targetDeclarations   []graph.Node
	includedDeclarations []graph.Node
	implicitGlobals      []graph.Node
	unusedDeclarations   []graph.Node
	unresolved           []graph.Node
	unresolvedCount      int
	unresolvedUsages     []unresolvedRow
	memberUsages         []graph.Node
	internalUsages       []usageRow
	externalUsages       []usageRow
	includedUsages       []usageRow
	implicitAssignments  []usageRow
	usageCounts          map[string]usageCounts
	targetUsageCounts    map[string]usageCounts
}

type usageCounts struct {
	references  int
	assignments int
	calls       int
}

type usageRow struct {
	sourceID     string
	targetID     string
	sourceURI    string
	targetURI    string
	kind         string
	role         string
	sourceFile   string
	sourceOwner  string
	targetFile   string
	name         string
	declKind     string
	typeName     string
	line         int
	column       int
	count        int
	includeDepth int
}

type unresolvedRow struct {
	usageKind string
	role      string
	kind      string
	source    string
	name      string
	file      string
	line      int
	column    int
	count     int
}

func newAnalysisContext(payload graph.Payload, options AnalysisSheetsOptions) analysisContext {
	nodesByID := map[string]graph.Node{}
	filesByURI := map[string]graph.Node{}
	for _, node := range payload.Nodes {
		if skipAnalysisNode(node) {
			continue
		}
		nodesByID[node.ID] = node
		if node.Kind == "file" && node.URI != "" {
			filesByURI[node.URI] = node
		}
	}
	workspaceWide := payload.Scope == "workspace" && options.TargetURI == ""
	targetURI := ""
	if !workspaceWide {
		targetURI = options.TargetURI
		if targetURI == "" {
			targetURI = payload.URI
		}
		if targetURI == "" {
			targetURI = payload.RootURI
		}
	}
	includeIndex := newIncludeGraphIndex(payload, nodesByID)
	includedURIs := includedFileURIsForTargetWithIndex(targetURI, nodesByID, includeIndex)
	context := analysisContext{
		targetURI:         targetURI,
		targetFileName:    fileNameForURI(targetURI, filesByURI),
		workspaceWide:     workspaceWide,
		includeGraph:      includeIndex,
		nodesByID:         nodesByID,
		filesByURI:        filesByURI,
		includedURIs:      includedURIs,
		usageCounts:       map[string]usageCounts{},
		targetUsageCounts: map[string]usageCounts{},
	}
	implicitAncestorDepths := map[string]map[string]int{}
	seenUsages := map[usageIdentity]struct{}{}
	for _, node := range nodesByID {
		if node.Kind != "vbDeclaration" || (!node.Implicit && !node.ImplicitGlobal) || (!workspaceWide && node.URI != targetURI) {
			continue
		}
		if _, exists := implicitAncestorDepths[node.URI]; exists {
			continue
		}
		implicitAncestorDepths[node.URI] = includeAncestorDepthsWithIndex(node.URI, nodesByID, includeIndex)
	}
	for _, edge := range graphEdges(payload) {
		if edge.Kind == "include" {
			continue
		}
		if edge.Kind == "unresolvedReference" {
			sourceURI := sourceURIForEdge(edge, nodesByID)
			if (workspaceWide || sourceURI == targetURI) && isUnresolvedTarget(edge, nodesByID) {
				context.unresolvedCount += countOrOne(edge.Count)
				context.unresolvedUsages = append(context.unresolvedUsages, unresolvedRowsForEdge(edge, nodesByID, filesByURI)...)
			}
			continue
		}
		if !isUsageKind(edge.Kind) {
			continue
		}
		target, ok := nodesByID[edge.Target]
		if !ok || target.Kind != "vbDeclaration" {
			continue
		}
		rows := dedupeUsageRows(usageRowsForEdge(edge, target, nodesByID, filesByURI), seenUsages)
		if len(rows) == 0 {
			continue
		}
		usageCount := usageRowsCount(rows)
		counts := context.usageCounts[target.ID]
		switch edge.Kind {
		case "references":
			counts.references += usageCount
		case "assignments":
			counts.assignments += usageCount
		case "calls":
			counts.calls += usageCount
		}
		context.usageCounts[target.ID] = counts
		if workspaceWide || target.URI == targetURI {
			targetCounts := context.targetUsageCounts[target.ID]
			switch edge.Kind {
			case "references":
				targetCounts.references += usageCount
			case "assignments":
				targetCounts.assignments += usageCount
			case "calls":
				targetCounts.calls += usageCount
			}
			context.targetUsageCounts[target.ID] = targetCounts
		} else if _, included := includedURIs[target.URI]; included {
			targetCounts := context.targetUsageCounts[target.ID]
			switch edge.Kind {
			case "references":
				targetCounts.references += usageCount
			case "assignments":
				targetCounts.assignments += usageCount
			case "calls":
				targetCounts.calls += usageCount
			}
			context.targetUsageCounts[target.ID] = targetCounts
		}
		for _, row := range rows {
			switch {
			case workspaceWide:
				context.internalUsages = append(context.internalUsages, row)
			case target.URI == targetURI && row.sourceURI == targetURI:
				context.internalUsages = append(context.internalUsages, row)
			case target.URI == targetURI:
				context.externalUsages = append(context.externalUsages, row)
			case row.sourceURI == targetURI && includedURIForNode(target, includedURIs):
				context.includedUsages = append(context.includedUsages, row)
			}
			if (target.Implicit || target.ImplicitGlobal) && edge.Kind == "assignments" && row.sourceURI != "" && row.sourceURI != target.URI {
				if depths := implicitAncestorDepths[target.URI]; depths != nil {
					if depth, ok := depths[row.sourceURI]; ok {
						row.includeDepth = depth
						context.implicitAssignments = append(context.implicitAssignments, row)
					}
				}
			}
		}
	}
	for _, node := range payload.Nodes {
		if skipAnalysisNode(node) {
			continue
		}
		switch node.Kind {
		case "vbDeclaration":
			context.declarations = append(context.declarations, node)
			if workspaceWide || node.URI == targetURI {
				context.targetDeclarations = append(context.targetDeclarations, node)
			}
			if _, ok := includedURIs[node.URI]; ok {
				context.includedDeclarations = append(context.includedDeclarations, node)
			}
			if (workspaceWide || node.URI == targetURI) && (node.Implicit || node.ImplicitGlobal) {
				context.implicitGlobals = append(context.implicitGlobals, node)
			}
			if isUnusedDeclaration(node, context.targetUsageCounts[node.ID]) && (workspaceWide || node.URI == targetURI) {
				context.unusedDeclarations = append(context.unusedDeclarations, node)
			}
		case "vbUnresolved":
			if workspaceWide || node.URI == targetURI {
				context.unresolved = append(context.unresolved, node)
			}
		case "vbMemberReference":
			if workspaceWide || node.URI == targetURI {
				context.memberUsages = append(context.memberUsages, node)
			}
		}
	}
	sortAnalysisContext(&context)
	return context
}
