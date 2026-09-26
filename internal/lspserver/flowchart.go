package lspserver

import (
	"context"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
)

type flowchartSymbolTarget struct {
	Label     string
	URI       string
	Range     lsp.Range
	NameRange lsp.Range
}

type flowchartSymbolTable struct {
	uri    string
	byName map[string][]vbUsageDeclaration
	byLine map[int][]vbUsageDeclaration
}

// flowchartFinalAssemblyTestHookContextKey allows focused tests to invalidate
// the graph after Mermaid assembly and before publication.
type flowchartFinalAssemblyTestHookContextKey struct{}

func runFlowchartFinalAssemblyTestHook(ctx context.Context, server *Server) {
	if ctx == nil {
		return
	}
	if hook, ok := ctx.Value(flowchartFinalAssemblyTestHookContextKey{}).(func(*Server)); ok && hook != nil {
		hook(server)
	}
}

func incompleteFlowchartPayload(payload map[string]any, ctx context.Context) map[string]any {
	for _, key := range []string{"sourceText", "sections", "nodes", "edges", "includes", "mermaid", "stats"} {
		delete(payload, key)
	}
	payload["cancelled"] = ctx != nil && ctx.Err() != nil
	payload["incomplete"] = true
	return payload
}

func newFlowchartSymbolTable(parsed *core.ParsedDocument) *flowchartSymbolTable {
	table := &flowchartSymbolTable{uri: parsed.URI, byName: map[string][]vbUsageDeclaration{}, byLine: map[int][]vbUsageDeclaration{}}
	for _, declaration := range graphVBDeclarations(parsed) {
		table.add(declaration)
	}
	for _, declaration := range collectVBUsageDeclarations(parsed).Declarations {
		table.add(declaration)
	}
	return table
}

func (t *flowchartSymbolTable) add(declaration vbUsageDeclaration) {
	if declaration.Name == "" {
		return
	}
	key := strings.ToLower(declaration.Name)
	existing := t.byName[key]
	for index, current := range existing {
		if current.Start == declaration.Start && current.End == declaration.End {
			if flowchartPreferDeclaration(declaration, current) {
				t.byName[key][index] = declaration
				lineItems := t.byLine[current.Line]
				for lineIndex, item := range lineItems {
					if item.Start == current.Start && item.End == current.End && strings.EqualFold(item.Name, current.Name) {
						t.byLine[current.Line][lineIndex] = declaration
						break
					}
				}
			}
			return
		}
	}
	t.byName[key] = append(t.byName[key], declaration)
	t.byLine[declaration.Line] = append(t.byLine[declaration.Line], declaration)
}

func flowchartPreferDeclaration(candidate vbUsageDeclaration, current vbUsageDeclaration) bool {
	if candidate.Kind != current.Kind {
		return candidate.Kind == "constant" || current.Kind == ""
	}
	if candidate.Local != current.Local {
		return candidate.Local
	}
	return false
}

func (t *flowchartSymbolTable) resolve(name string, scope string) (vbUsageDeclaration, bool) {
	if t == nil {
		return vbUsageDeclaration{}, false
	}
	candidates := t.byName[strings.ToLower(name)]
	for _, candidate := range candidates {
		if candidate.Local && candidate.Scope != "" && strings.EqualFold(candidate.Scope, scope) {
			return candidate, true
		}
	}
	for _, candidate := range candidates {
		if !candidate.Local {
			return candidate, true
		}
	}
	if len(candidates) > 0 {
		return candidates[0], true
	}
	return vbUsageDeclaration{}, false
}

func (t *flowchartSymbolTable) declarationsOnLine(line int) []vbUsageDeclaration {
	if t == nil {
		return nil
	}
	return t.byLine[line]
}

func (s *Server) buildFlowchartContext(ctx context.Context, params executeCommandParams) any {
	var arg struct {
		URI             string `json:"uri"`
		LabelMode       string `json:"labelMode"`
		Locale          string `json:"locale"`
		LabelLineLength int    `json:"labelLineLength"`
	}
	if len(params.Arguments) > 0 {
		_ = remarshal(params.Arguments[0], &arg)
	}
	labelMode := normalizeFlowchartLabelMode(arg.LabelMode)
	if labelMode == "" {
		labelMode = s.settings.FlowchartLabelMode
	}
	if labelMode == "" {
		labelMode = "normal"
	}
	labelLineLength := arg.LabelLineLength
	if labelLineLength < 8 {
		s.mu.Lock()
		labelLineLength = s.settings.FlowchartLabelLineLength
		s.mu.Unlock()
		if labelLineLength < 8 {
			labelLineLength = defaultFlowchartLabelLineLength
		}
	}
	payload := map[string]any{
		"uri":       arg.URI,
		"fileName":  filepath.Base(fileURIPath(arg.URI)),
		"labelMode": labelMode,
		"settings": map[string]any{
			"labelLineLength": labelLineLength,
		},
	}
	taskID, _ := s.beginProgressTask("flowchart.document", "analyzing", "flowchart", "flowchart.document.loadDocument", progressDetailForURI(arg.URI), 1, true)
	taskContext := s.registerProgressCancellationWithParent(taskID, ctx)
	defer s.unregisterProgressCancellation(taskID)
	defer func() {
		state := "completed"
		if taskContext.Err() != nil {
			state = "cancelled"
		}
		s.finishProgressTask(taskID, "flowchart", state)
	}()
	if !s.waitForDocumentOpenAnalysisContext(taskContext) {
		return incompleteFlowchartPayload(payload, taskContext)
	}
	report := func(label, uri string, current, total int) {
		detail := progressDetailForURI(uri)
		activeItems := []string{}
		if detail != "" {
			activeItems = append(activeItems, detail)
		}
		s.updateProgressTask(taskID, "flowchart", label, detail, current, total, activeItems, "running")
	}
	generation := s.graphGenerationSnapshot()
	parsed, ok := s.parsedGraphDocumentContext(taskContext, arg.URI)
	if !ok {
		report("flowchart.document.loadDocument", arg.URI, 1, 1)
		return incompleteFlowchartPayload(payload, taskContext)
	}
	if taskContext.Err() != nil || !s.graphGenerationCurrent(taskContext, generation) {
		return incompleteFlowchartPayload(payload, taskContext)
	}
	report("flowchart.document.loadDocument", parsed.URI, 1, 1)
	if taskContext.Err() != nil {
		return incompleteFlowchartPayload(payload, taskContext)
	}
	sections := flowchartSections(parsed)
	for index := range sections {
		report("flowchart.document.buildSections", parsed.URI, index+1, len(sections))
	}
	targetResult := s.flowchartSymbolTargetsWithProgressContextResult(taskContext, parsed, generation, func(uri string, current, total int) {
		report("flowchart.document.collectTargets", uri, current, total)
	})
	if !targetResult.complete {
		return incompleteFlowchartPayload(payload, taskContext)
	}
	targets := targetResult.targets
	nodes, edges := flowchartVBScriptNodes(parsed, targets, labelMode, arg.Locale, labelLineLength)
	graphItems := len(nodes) + len(edges)
	for index := 0; index < graphItems; index++ {
		report("flowchart.document.buildNodes", parsed.URI, index+1, graphItems)
	}
	if taskContext.Err() != nil {
		return incompleteFlowchartPayload(payload, taskContext)
	}
	sections = flowchartEnsureOutputSection(sections, nodes)
	sections, nodes, edges = flowchartAttachSectionMembership(sections, nodes, edges)
	includes, includesComplete := s.flowchartIncludesContext(taskContext, parsed)
	if !includesComplete || !s.graphGenerationCurrent(taskContext, generation) {
		return incompleteFlowchartPayload(payload, taskContext)
	}
	for index := range includes {
		report("flowchart.document.collectIncludes", parsed.URI, index+1, len(includes))
	}
	mermaid := flowchartMermaid(sections, nodes, edges, labelLineLength)
	stats := map[string]int{
		"sections": len(sections),
		"nodes":    len(nodes),
		"edges":    len(edges),
		"includes": len(includes),
	}
	runFlowchartFinalAssemblyTestHook(taskContext, s)
	if taskContext.Err() != nil || !s.graphGenerationCurrent(taskContext, generation) {
		return incompleteFlowchartPayload(payload, taskContext)
	}
	report("flowchart.document.buildPayload", parsed.URI, 1, 1)
	if taskContext.Err() != nil || !s.graphGenerationCurrent(taskContext, generation) {
		return incompleteFlowchartPayload(payload, taskContext)
	}
	payload["sourceText"] = parsed.Text
	payload["sections"] = sections
	payload["nodes"] = nodes
	payload["edges"] = edges
	payload["includes"] = includes
	payload["mermaid"] = mermaid
	payload["stats"] = stats
	return payload
}

func flowchartEnsureOutputSection(sections []map[string]any, nodes []map[string]any) []map[string]any {
	var first, last *lsp.Range
	for _, node := range nodes {
		if node["kind"] != "output" {
			continue
		}
		nodeRange, ok := flowchartRangeFromAny(node["range"])
		if !ok {
			continue
		}
		covered := false
		for _, section := range sections {
			sectionRange, rangeOK := flowchartRangeFromAny(section["range"])
			if rangeOK && flowchartRangeContainsPosition(sectionRange, nodeRange.Start) {
				covered = true
				break
			}
		}
		if covered {
			continue
		}
		if first == nil {
			copy := nodeRange
			first, last = &copy, &copy
			continue
		}
		if flowchartPositionBeforeOrEqual(nodeRange.Start, first.Start) {
			first.Start = nodeRange.Start
		}
		if flowchartPositionBeforeOrEqual(last.End, nodeRange.End) {
			last.End = nodeRange.End
		}
	}
	if first == nil {
		return sections
	}
	sections = append(sections, map[string]any{
		"id":    "section-" + strconv.Itoa(len(sections)),
		"kind":  "topLevel",
		"label": "Rendered output",
		"range": lsp.Range{Start: first.Start, End: last.End},
	})
	return sections
}

func flowchartAttachSectionMembership(sections []map[string]any, nodes []map[string]any, edges []map[string]any) ([]map[string]any, []map[string]any, []map[string]any) {
	if len(sections) == 0 {
		return sections, nodes, edges
	}
	defaultSectionID, _ := sections[0]["id"].(string)
	sectionNodeIDs := make([][]string, len(sections))
	nodeSectionByID := make(map[string]string, len(nodes))
	for _, node := range nodes {
		nodeID, _ := node["id"].(string)
		sectionIndex := flowchartSectionIndexForNode(sections, node)
		sectionID, _ := sections[sectionIndex]["id"].(string)
		if sectionID == "" {
			sectionID = defaultSectionID
		}
		node["sectionId"] = sectionID
		if nodeID != "" {
			sectionNodeIDs[sectionIndex] = append(sectionNodeIDs[sectionIndex], nodeID)
			nodeSectionByID[nodeID] = sectionID
		}
	}
	for index := range sections {
		sections[index]["nodeIds"] = sectionNodeIDs[index]
	}
	for _, edge := range edges {
		sourceID, _ := edge["source"].(string)
		sectionID := nodeSectionByID[sourceID]
		if sectionID == "" {
			sectionID = defaultSectionID
		}
		edge["sectionId"] = sectionID
	}
	return sections, nodes, edges
}

func flowchartSectionIndexForNode(sections []map[string]any, node map[string]any) int {
	nodeRange, ok := flowchartRangeFromAny(node["range"])
	if !ok {
		return 0
	}
	for index, section := range sections {
		sectionRange, ok := flowchartRangeFromAny(section["range"])
		if ok && flowchartRangeContainsPosition(sectionRange, nodeRange.Start) {
			return index
		}
	}
	return 0
}

func flowchartRangeFromAny(value any) (lsp.Range, bool) {
	switch r := value.(type) {
	case lsp.Range:
		return r, true
	case *lsp.Range:
		if r != nil {
			return *r, true
		}
	}
	return lsp.Range{}, false
}

func flowchartRangeContainsPosition(r lsp.Range, position lsp.Position) bool {
	return flowchartPositionBeforeOrEqual(r.Start, position) && flowchartPositionBeforeOrEqual(position, r.End)
}

func flowchartPositionBeforeOrEqual(left lsp.Position, right lsp.Position) bool {
	return left.Line < right.Line || (left.Line == right.Line && left.Character <= right.Character)
}

func flowchartSignatureLabel(signature vbscript.Signature) string {
	switch strings.ToLower(signature.Kind) {
	case "function":
		return "Function " + signature.Name
	case "property":
		return "Property " + signature.Name
	default:
		return "Sub " + signature.Name
	}
}

type flowchartSymbolTargetsResult struct {
	targets    map[string]flowchartSymbolTarget
	generation uint64
	complete   bool
	err        error
}

func (s *Server) flowchartSymbolTargetsWithProgress(ctx context.Context, parsed *core.ParsedDocument, report func(uri string, current, total int)) map[string]flowchartSymbolTarget {
	result := s.flowchartSymbolTargetsWithProgressContextResult(ctx, parsed, s.graphGenerationSnapshot(), report)
	if !result.complete {
		return nil
	}
	return result.targets
}

func (s *Server) flowchartSymbolTargetsWithProgressContextResult(ctx context.Context, parsed *core.ParsedDocument, generation uint64, report func(uri string, current, total int)) flowchartSymbolTargetsResult {
	targets := map[string]flowchartSymbolTarget{}
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return flowchartSymbolTargetsResult{generation: generation, err: ctx.Err()}
	}
	if !s.graphGenerationCurrent(ctx, generation) {
		return flowchartSymbolTargetsResult{generation: generation, err: errGraphCollectionGeneration}
	}
	addTargets := func(owner *core.ParsedDocument) {
		for _, signature := range vbscript.Signatures(owner) {
			targets[strings.ToLower(signature.Name)] = flowchartSymbolTarget{
				Label:     flowchartSignatureLabel(signature),
				URI:       owner.URI,
				Range:     signature.Range,
				NameRange: signature.NameRange,
			}
		}
	}
	collection := s.documentGraphIncludeTreeContextCollectionResult(ctx, parsed, generation)
	if !collection.complete {
		return flowchartSymbolTargetsResult{generation: collection.generation, err: collection.err}
	}
	documents := collection.documents
	documentByURI := map[string]*core.ParsedDocument{}
	for _, document := range documents {
		if ctx.Err() != nil {
			return flowchartSymbolTargetsResult{generation: generation, err: ctx.Err()}
		}
		if !s.graphGenerationCurrent(ctx, generation) {
			return flowchartSymbolTargetsResult{generation: generation, err: errGraphCollectionGeneration}
		}
		if document == nil {
			continue
		}
		documentByURI[document.URI] = document
		addTargets(document)
	}
	canonicalImplicitIDs := s.graphCanonicalImplicitDeclarationIDs(documents, documentByURI)
	for index, document := range documents {
		if ctx.Err() != nil {
			return flowchartSymbolTargetsResult{generation: generation, err: ctx.Err()}
		}
		if !s.graphGenerationCurrent(ctx, generation) {
			return flowchartSymbolTargetsResult{generation: generation, err: errGraphCollectionGeneration}
		}
		if document == nil {
			if report != nil {
				report("", index+1, len(documents))
			}
			continue
		}
		for _, declaration := range graphVBDeclarations(document) {
			if !declaration.Implicit {
				continue
			}
			lower := strings.ToLower(declaration.Name)
			if _, exists := targets[lower]; exists {
				continue
			}
			if canonicalImplicitIDs[lower] != graphDeclarationNodeID(document.URI, declaration.Name, declaration.Range) {
				continue
			}
			targets[lower] = flowchartSymbolTarget{
				Label:     "implicit global variable " + declaration.Name,
				URI:       document.URI,
				Range:     declaration.Range,
				NameRange: declaration.Range,
			}
		}
		if report != nil {
			report(document.URI, index+1, len(documents))
		}
	}
	if ctx.Err() != nil {
		return flowchartSymbolTargetsResult{generation: generation, err: ctx.Err()}
	}
	if !s.graphGenerationCurrent(ctx, generation) {
		return flowchartSymbolTargetsResult{generation: generation, err: errGraphCollectionGeneration}
	}
	return flowchartSymbolTargetsResult{targets: targets, generation: generation, complete: true}
}

type flowchartFragment struct {
	entry string
	tails []string
}

type flowchartLoopTarget struct {
	kind string
	id   string
}

type flowchartBuildContext struct {
	procedureEnd string
	loops        []flowchartLoopTarget
	scope        string
}

type flowchartCFGBuilder struct {
	document  *core.TextDocument
	targets   map[string]flowchartSymbolTarget
	symbols   *flowchartSymbolTable
	labelMode string
	locale    string
	nodes     []map[string]any
	edges     []map[string]any
}

func (b *flowchartCFGBuilder) addNode(kind, label string, links []map[string]any, start, end int) string {
	node := flowchartNodeWithRange(b.document, len(b.nodes), kind, label, links, start, end)
	b.nodes = append(b.nodes, node)
	id, _ := node["id"].(string)
	return id
}

func (b *flowchartCFGBuilder) connect(source, target, label string) {
	if source == "" || target == "" {
		return
	}
	b.edges = append(b.edges, flowchartEdge(len(b.edges), source, target, label))
}

func flowchartLoopDisplayName(kind string) string {
	switch kind {
	case "for", "forEach":
		return "For"
	case "while":
		return "While"
	default:
		return "Do"
	}
}

func splitFlowchartLabel(label string, limit int) []string {
	if limit <= 0 || utf16LabelLength(label) <= limit {
		return []string{label}
	}
	parts := []string{}
	current := strings.Builder{}
	currentUnits := 0
	for _, r := range label {
		units := 1
		if r >= 0x10000 {
			units = 2
		}
		if currentUnits > 0 && currentUnits+units > limit {
			parts = append(parts, current.String())
			current.Reset()
			currentUnits = 0
		}
		current.WriteRune(r)
		currentUnits += units
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}
	return parts
}

func utf16LabelLength(label string) int {
	length := 0
	for _, r := range label {
		if r >= 0x10000 {
			length += 2
		} else {
			length++
		}
	}
	return length
}

func flowchartEdge(index int, source any, target any, label string) map[string]any {
	return map[string]any{
		"id":     "edge-" + strconv.Itoa(index),
		"source": source,
		"target": target,
		"label":  label,
	}
}

func flowchartNode(index int, kind string, label string, links []map[string]any) map[string]any {
	node := map[string]any{
		"id":    "node-" + strconv.Itoa(index),
		"kind":  kind,
		"label": label,
	}
	if links != nil {
		node["links"] = links
	}
	return node
}

func flowchartNodeWithRange(document *core.TextDocument, index int, kind string, label string, links []map[string]any, start int, end int) map[string]any {
	node := flowchartNode(index, kind, label, links)
	node["range"] = document.Range(start, end)
	return node
}

var (
	flowchartCallPattern       = regexp.MustCompile(`(?i)^\s*(?:Call\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*(?:\(|$)`)
	flowchartAssignmentPattern = regexp.MustCompile(`(?i)^\s*([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.+)$`)
	flowchartIdentifierPattern = regexp.MustCompile(`\b[A-Za-z_][A-Za-z0-9_]*\b`)
)

func flowchartCallName(line string) (string, bool) {
	match := flowchartCallPattern.FindStringSubmatch(line)
	if match == nil {
		return "", false
	}
	name := match[1]
	if flowchartReservedIdentifier(name) {
		return "", false
	}
	return name, true
}

func flowchartIfLabel(condition string, labelMode string, locale string) string {
	condition = strings.TrimSpace(condition)
	if labelMode == "raw" {
		return "If " + condition + " Then"
	}
	if labelMode == "description" && locale == "ja" {
		if left, right, ok := strings.Cut(condition, "="); ok {
			return strings.TrimSpace(left) + "が" + strings.TrimSpace(right) + "と等しいを判定"
		}
	}
	return "Check " + strings.TrimSpace(condition)
}

func flowchartStatementLabel(line string, labelMode string, locale string, symbols *flowchartSymbolTable, scope string) string {
	if labelMode == "raw" {
		return line
	}
	match := flowchartAssignmentPattern.FindStringSubmatch(line)
	if match == nil {
		return line
	}
	target := strings.TrimSpace(match[1])
	expression := strings.TrimSpace(match[2])
	if targetSymbol, ok := symbols.resolve(target, scope); ok {
		if expressionSymbol, ok := symbols.resolve(expression, scope); ok {
			return "Assign " + flowchartSymbolLabel(expressionSymbol) + " to " + flowchartSymbolLabel(targetSymbol)
		}
	}
	if locale == "ja" {
		if labelMode == "description" {
			if left, right, ok := flowchartAdditionExpression(expression); ok && strings.EqualFold(left, target) {
				return target + "に" + right + "を加算"
			}
		}
		return target + "に" + expression + "を代入"
	}
	return line
}

func flowchartAdditionExpression(expression string) (string, string, bool) {
	left, right, ok := strings.Cut(expression, "+")
	if !ok {
		return "", "", false
	}
	return strings.TrimSpace(left), strings.TrimSpace(right), true
}

type flowchartCallExpr struct {
	Name string
	Args string
}

func flowchartCallLabels(line string, labelMode string, locale string, symbols *flowchartSymbolTable, scope string) []string {
	trimmed := strings.TrimSpace(line)
	raw := trimmed
	if strings.HasPrefix(strings.ToLower(raw), "call ") {
		raw = strings.TrimSpace(raw[len("call "):])
	}
	if labelMode == "raw" {
		return []string{"Call " + raw}
	}
	calls := flowchartNestedCalls(raw)
	if len(calls) == 0 {
		if name, ok := flowchartCallName(line); ok {
			calls = []flowchartCallExpr{{Name: name}}
		}
	}
	labels := make([]string, 0, len(calls))
	for _, call := range calls {
		labels = append(labels, flowchartCallExprLabel(call, labelMode, locale, symbols, scope))
	}
	return labels
}

func flowchartCallExprLabel(call flowchartCallExpr, labelMode string, locale string, symbols *flowchartSymbolTable, scope string) string {
	args := strings.TrimSpace(call.Args)
	displayName := call.Name
	if declaration, ok := symbols.resolve(call.Name, scope); ok && (declaration.Kind == "sub" || declaration.Kind == "function") {
		displayName = flowchartProcedureKindLabel(declaration.Kind) + " " + declaration.Name
	}
	argLabels := flowchartArgumentLabels(args, symbols, scope)
	if labelMode == "description" && displayName != call.Name {
		if len(argLabels) == 0 {
			return "Call " + displayName
		}
		return "Call " + displayName + " with " + strings.Join(argLabels, ", ")
	}
	if labelMode == "description" && locale == "ja" {
		if args == "" {
			return call.Name + "を呼び出し"
		}
		return call.Name + "を引数" + strings.Join(argLabels, "、") + "で呼び出し"
	}
	if args == "" {
		return "Call " + displayName
	}
	if displayName != call.Name {
		return "Call " + displayName + "(" + strings.Join(argLabels, ", ") + ")"
	}
	return "Call " + call.Name + "(" + args + ")"
}

func flowchartArgumentLabels(args string, symbols *flowchartSymbolTable, scope string) []string {
	parts := flowchartArgumentParts(args)
	labels := make([]string, 0, len(parts))
	for _, part := range parts {
		if declaration, ok := symbols.resolve(part, scope); ok {
			if declaration.Implicit {
				labels = append(labels, part)
				continue
			}
			labels = append(labels, flowchartSymbolLabel(declaration))
			continue
		}
		labels = append(labels, part)
	}
	return labels
}

func flowchartSymbolLabel(declaration vbUsageDeclaration) string {
	if declaration.Implicit {
		return "implicit global variable " + declaration.Name
	}
	kind := declaration.Kind
	if kind == "" {
		kind = "variable"
	}
	scope := ""
	if kind != "parameter" && kind != "sub" && kind != "function" {
		if declaration.Local {
			scope = "local "
		} else {
			scope = "global "
		}
	}
	return scope + flowchartDeclarationKindLabel(kind) + " " + declaration.Name
}

func flowchartDeclarationKindLabel(kind string) string {
	switch strings.ToLower(kind) {
	case "sub":
		return "Sub"
	case "function":
		return "Function"
	case "constant":
		return "constant"
	case "parameter":
		return "parameter"
	default:
		return "variable"
	}
}

func flowchartProcedureKindLabel(kind string) string {
	switch strings.ToLower(kind) {
	case "function":
		return "Function"
	default:
		return "Sub"
	}
}

func flowchartNestedCalls(expression string) []flowchartCallExpr {
	calls := []flowchartCallExpr{}
	for index := 0; index < len(expression); index++ {
		if !flowchartIdentifierStart(expression[index]) {
			continue
		}
		nameStart := index
		index++
		for index < len(expression) && flowchartIdentifierPart(expression[index]) {
			index++
		}
		name := expression[nameStart:index]
		for index < len(expression) && (expression[index] == ' ' || expression[index] == '\t') {
			index++
		}
		if index >= len(expression) || expression[index] != '(' {
			continue
		}
		close := flowchartMatchingParen(expression, index)
		if close < 0 {
			continue
		}
		args := expression[index+1 : close]
		calls = append(calls, flowchartNestedCalls(args)...)
		calls = append(calls, flowchartCallExpr{Name: name, Args: args})
		index = close
	}
	return calls
}

func flowchartMatchingParen(value string, open int) int {
	depth := 0
	inString := false
	for index := open; index < len(value); index++ {
		ch := value[index]
		if ch == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		switch ch {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return index
			}
		}
	}
	return -1
}

func flowchartArgumentParts(args string) []string {
	parts := []string{}
	start := 0
	depth := 0
	inString := false
	for index := 0; index < len(args); index++ {
		ch := args[index]
		if ch == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		switch ch {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				parts = append(parts, strings.TrimSpace(args[start:index]))
				start = index + 1
			}
		}
	}
	last := strings.TrimSpace(args[start:])
	if last != "" {
		parts = append(parts, last)
	}
	return parts
}

func flowchartIdentifierStart(ch byte) bool {
	return (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') || ch == '_'
}

func flowchartIdentifierPart(ch byte) bool {
	return flowchartIdentifierStart(ch) || (ch >= '0' && ch <= '9')
}

func flowchartOnErrorLabel(line string, labelMode string, locale string) string {
	lower := strings.ToLower(strings.TrimSpace(line))
	if labelMode == "raw" {
		return strings.TrimSpace(line)
	}
	resumeNext := strings.HasPrefix(lower, "on error resume next")
	if locale == "ja" {
		if labelMode == "description" {
			if resumeNext {
				return "エラーが発生しても処理を止めず、次のステートメントから続行"
			}
			return "現在の例外処理を解除し、以後のエラーを通常どおり発生"
		}
		if resumeNext {
			return "例外処理: エラー時は次へ進む"
		}
		return "例外処理を解除"
	}
	if labelMode == "description" {
		if resumeNext {
			return "Continue with the next statement when an error occurs"
		}
		return "Clear the current error handler"
	}
	if resumeNext {
		return "Exception handling: resume next"
	}
	return "Clear exception handling"
}

func flowchartExpressionLinks(expression string, targets map[string]flowchartSymbolTarget, symbols *flowchartSymbolTable, scope string) []map[string]any {
	matches := flowchartIdentifierPattern.FindAllString(flowchartCodeOutsideStrings(expression), -1)
	links := make([]map[string]any, 0, len(matches))
	seen := map[string]struct{}{}
	writeName := ""
	if assignment := flowchartAssignmentPattern.FindStringSubmatch(expression); assignment != nil {
		writeName = strings.ToLower(strings.TrimSpace(assignment[1]))
	}
	for _, name := range matches {
		key := strings.ToLower(name)
		if flowchartReservedIdentifier(key) {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		role := "read"
		if key == writeName {
			role = "write"
		}
		link := map[string]any{"id": "link-" + key + "-" + role, "role": role}
		if declaration, ok := symbols.resolve(name, scope); ok {
			link["label"] = flowchartSymbolLabel(declaration)
			link["symbolKind"] = flowchartSymbolKind(declaration)
			link["target"] = map[string]any{"uri": symbols.uri, "range": declaration.Range, "nameRange": declaration.Range}
		} else {
			link["label"] = "implicit global variable " + name
			link["symbolKind"] = "implicitGlobalVariable"
		}
		if target, found := targets[key]; found && target.URI != "" {
			link["target"] = map[string]any{"uri": target.URI, "range": target.Range, "nameRange": target.NameRange}
		}
		links = append(links, link)
	}
	return links
}

func flowchartCodeOutsideStrings(value string) string {
	var out strings.Builder
	out.Grow(len(value))
	inString := false
	for index := 0; index < len(value); index++ {
		ch := value[index]
		if ch == '"' {
			if inString && index+1 < len(value) && value[index+1] == '"' {
				out.WriteString("  ")
				index++
				continue
			}
			inString = !inString
			out.WriteByte(' ')
			continue
		}
		if !inString && ch == '\'' {
			for ; index < len(value); index++ {
				out.WriteByte(' ')
			}
			break
		}
		if inString {
			out.WriteByte(' ')
		} else {
			out.WriteByte(ch)
		}
	}
	return out.String()
}

func flowchartSymbolKind(declaration vbUsageDeclaration) string {
	if declaration.Implicit {
		return "implicitGlobalVariable"
	}
	switch strings.ToLower(declaration.Kind) {
	case "constant":
		return "constant"
	case "parameter":
		return "parameter"
	case "sub", "function":
		return "procedure"
	default:
		return "variable"
	}
}

func flowchartReservedIdentifier(name string) bool {
	switch strings.ToLower(name) {
	case "if", "then", "else", "elseif", "end", "sub", "function", "call", "exit", "on", "error", "resume", "next",
		"and", "or", "not", "xor", "eqv", "imp", "mod", "is", "true", "false", "nothing", "empty", "null",
		"case", "const", "default", "dim", "do", "each", "erase", "for", "in", "loop", "select", "set", "step", "stop",
		"to", "until", "wend", "while", "with", "response", "request", "write":
		return true
	default:
		return false
	}
}
