package lspserver

import (
	"sort"
	"strconv"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
)

const flowchartCSTRuntimeKey = "flowchart.vbscriptCST.v1"

type flowchartCSTGroup struct {
	nodes      []*vbscript.CSTNode
	start, end int
	scope      string
}

func flowchartDocumentCST(parsed *core.ParsedDocument) *vbscript.CSTNode {
	if cached, ok := parsed.LoadRuntimeAnalysis(flowchartCSTRuntimeKey); ok {
		if root, ok := cached.(*vbscript.CSTNode); ok {
			return root
		}
	}
	root := vbscript.ParseDocumentCST(parsed)
	parsed.StoreRuntimeAnalysis(flowchartCSTRuntimeKey, root)
	return root
}

func flowchartSections(parsed *core.ParsedDocument) []map[string]any {
	root := flowchartDocumentCST(parsed)
	document := core.SourceDocument(parsed)
	sections := make([]map[string]any, 0)
	appendTopLevel := func(nodes []*vbscript.CSTNode) {
		if len(nodes) == 0 {
			return
		}
		sections = append(sections, map[string]any{
			"id":    "section-" + strconv.Itoa(len(sections)),
			"kind":  "topLevel",
			"label": "Top level",
			"range": document.Range(nodes[0].Start, nodes[len(nodes)-1].End),
		})
	}
	var walk func([]*vbscript.CSTNode, bool)
	walk = func(nodes []*vbscript.CSTNode, topLevel bool) {
		pending := make([]*vbscript.CSTNode, 0)
		flush := func() {
			if topLevel {
				appendTopLevel(pending)
			}
			pending = pending[:0]
		}
		for _, node := range nodes {
			kind := flowchartCSTStatementKind(node)
			switch kind {
			case vbscript.CSTStatementSub, vbscript.CSTStatementFunction,
				vbscript.CSTStatementPropertyGet, vbscript.CSTStatementPropertyLet, vbscript.CSTStatementPropertySet:
				flush()
				label, sectionKind := flowchartCSTSectionLabel(node)
				sections = append(sections, map[string]any{
					"id":    "section-" + strconv.Itoa(len(sections)),
					"kind":  sectionKind,
					"label": label,
					"range": document.Range(node.Start, node.End),
				})
			case vbscript.CSTStatementClass:
				flush()
				walk(node.Children, false)
			default:
				if topLevel && flowchartCSTExecutableNode(node) {
					pending = append(pending, node)
				}
			}
		}
		flush()
	}
	walk(root.Children, true)
	return sections
}

func flowchartCSTSectionLabel(node *vbscript.CSTNode) (string, string) {
	name := ""
	if node.NameToken != nil {
		name = node.NameToken.Text
	}
	switch flowchartCSTStatementKind(node) {
	case vbscript.CSTStatementFunction:
		return "Function " + name, "procedure"
	case vbscript.CSTStatementSub:
		return "Sub " + name, "procedure"
	default:
		return "Property " + name, "property"
	}
}

func flowchartVBScriptNodes(parsed *core.ParsedDocument, targets map[string]flowchartSymbolTarget, labelMode string, locale string, labelLineLength int) ([]map[string]any, []map[string]any) {
	root := flowchartDocumentCST(parsed)
	builder := &flowchartCFGBuilder{
		document:  core.SourceDocument(parsed),
		targets:   targets,
		symbols:   newFlowchartSymbolTable(parsed),
		labelMode: labelMode,
		locale:    locale,
	}
	for _, group := range flowchartCSTGroups(root) {
		start := builder.addNode("start", "Start", nil, group.start, group.start)
		end := builder.addNode("end", "End", nil, group.end, group.end)
		context := flowchartBuildContext{procedureEnd: end, scope: group.scope}
		fragment := builder.buildCSTSequence(group.nodes, context)
		if fragment.entry == "" {
			builder.connect(start, end, "")
			continue
		}
		builder.connect(start, fragment.entry, "")
		for _, tail := range fragment.tails {
			builder.connect(tail, end, "")
		}
	}
	flowchartAttachStaticOutput(parsed, builder)
	return builder.nodes, builder.edges
}

type flowchartStaticOutput struct {
	start, end int
	fragments  []map[string]any
}

func flowchartAttachStaticOutput(parsed *core.ParsedDocument, builder *flowchartCFGBuilder) {
	outputs := flowchartStaticOutputs(parsed, builder.document)
	ids := make([]string, len(outputs))
	for index, output := range outputs {
		id := builder.addNode("output", flowchartStaticOutputLabel(output.fragments), nil, output.start, output.end)
		builder.nodes[len(builder.nodes)-1]["outputFragments"] = output.fragments
		flowchartSpliceOutputNode(builder, id, output.start, output.end)
		ids[index] = id
	}
	flowchartConnectDetachedOutputs(builder, outputs, ids)
	for index := range builder.edges {
		builder.edges[index]["id"] = "edge-" + strconv.Itoa(index)
	}
}

func flowchartConnectDetachedOutputs(builder *flowchartCFGBuilder, outputs []flowchartStaticOutput, ids []string) {
	if len(outputs) == 0 {
		return
	}
	connected := make(map[string]bool, len(builder.edges)*2)
	for _, edge := range builder.edges {
		source, _ := edge["source"].(string)
		target, _ := edge["target"].(string)
		connected[source] = true
		connected[target] = true
	}
	staticOutput := make(map[string]int, len(ids))
	for index, id := range ids {
		staticOutput[id] = index
	}
	detached := make([]string, 0)
	first, last := -1, -1
	for _, node := range builder.nodes {
		if node["kind"] != "output" {
			continue
		}
		id, _ := node["id"].(string)
		if connected[id] {
			continue
		}
		detached = append(detached, id)
		if index, ok := staticOutput[id]; ok {
			if first < 0 {
				first = index
			}
			last = index
		}
	}
	if len(detached) == 0 {
		return
	}
	// Sections are assigned by position, so the synthetic Start/End must sit at
	// the detached outputs; anchoring them at an output that was already
	// spliced into the top level would move Start into that other section.
	if first < 0 {
		first, last = 0, len(outputs)-1
	}
	start := builder.addNode("start", "Start", nil, outputs[first].start, outputs[first].start)
	end := builder.addNode("end", "End", nil, outputs[last].end, outputs[last].end)
	previous := start
	for _, id := range detached {
		builder.connect(previous, id, "")
		previous = id
	}
	builder.connect(previous, end, "")
}

func flowchartStaticOutputs(parsed *core.ParsedDocument, document *core.TextDocument) []flowchartStaticOutput {
	outputs := make([]flowchartStaticOutput, 0)
	for _, region := range parsed.Regions {
		if region.Kind == core.RegionASPExpression && region.ContentStart < region.ContentEnd {
			text := strings.TrimSpace(parsed.Text[region.ContentStart:region.ContentEnd])
			if text != "" {
				outputs = append(outputs, flowchartStaticOutput{
					start: region.Start,
					end:   region.End,
					fragments: []map[string]any{{
						"language": "text",
						"text":     text,
						"range":    document.Range(region.ContentStart, region.ContentEnd),
					}},
				})
			}
			continue
		}
		if region.Kind != core.RegionHTML || region.Start >= region.End {
			continue
		}
		boundaries := []int{region.Start, region.End}
		for _, embedded := range parsed.Regions {
			if embedded.Kind != core.RegionStyle && embedded.Kind != core.RegionClientScript && embedded.Kind != core.RegionStyleAttribute {
				continue
			}
			if embedded.ContentStart > region.Start && embedded.ContentStart < region.End {
				boundaries = append(boundaries, embedded.ContentStart)
			}
			if embedded.ContentEnd > region.Start && embedded.ContentEnd < region.End {
				boundaries = append(boundaries, embedded.ContentEnd)
			}
		}
		sort.Ints(boundaries)
		fragments := make([]map[string]any, 0, len(boundaries)-1)
		for index := 0; index+1 < len(boundaries); index++ {
			start, end := boundaries[index], boundaries[index+1]
			if start >= end || strings.TrimSpace(parsed.Text[start:end]) == "" {
				continue
			}
			language := "html"
			midpoint := start + (end-start)/2
			for _, embedded := range parsed.Regions {
				if midpoint < embedded.ContentStart || midpoint >= embedded.ContentEnd {
					continue
				}
				switch embedded.Kind {
				case core.RegionStyle, core.RegionStyleAttribute:
					language = "css"
				case core.RegionClientScript:
					language = "javascript"
				}
			}
			fragments = append(fragments, map[string]any{
				"language": language,
				"text":     parsed.Text[start:end],
				"range":    document.Range(start, end),
			})
		}
		if len(fragments) > 0 {
			outputs = append(outputs, flowchartStaticOutput{start: region.Start, end: region.End, fragments: fragments})
		}
	}
	sort.SliceStable(outputs, func(i, j int) bool { return outputs[i].start < outputs[j].start })
	return outputs
}

func flowchartStaticOutputLabel(fragments []map[string]any) string {
	if len(fragments) == 1 {
		switch fragments[0]["language"] {
		case "css":
			return "Render CSS"
		case "javascript":
			return "Render JavaScript"
		case "text":
			return "Render expression"
		}
	}
	return "Render HTML"
}

func flowchartSpliceOutputNode(builder *flowchartCFGBuilder, outputID string, start, end int) {
	bestIndex := -1
	bestSourceStart := -1
	bestTargetStart := int(^uint(0) >> 1)
	for index, edge := range builder.edges {
		sourceID, _ := edge["source"].(string)
		targetID, _ := edge["target"].(string)
		source := flowchartNodeByID(builder.nodes, sourceID)
		target := flowchartNodeByID(builder.nodes, targetID)
		if target != nil && (target["kind"] == "else" || target["kind"] == "elseif" || target["kind"] == "case") {
			continue
		}
		sourceStart, okSource := flowchartNodeStartOffset(builder.document, source)
		targetStart, okTarget := flowchartNodeStartOffset(builder.document, target)
		if !okSource || !okTarget || sourceStart > start || targetStart < end {
			continue
		}
		if targetStart < bestTargetStart || targetStart == bestTargetStart && sourceStart > bestSourceStart {
			bestIndex, bestSourceStart, bestTargetStart = index, sourceStart, targetStart
		}
	}
	if bestIndex < 0 {
		return
	}
	edge := builder.edges[bestIndex]
	source, target, label := edge["source"], edge["target"], ""
	if value, ok := edge["label"].(string); ok {
		label = value
	}
	builder.edges = append(builder.edges[:bestIndex], builder.edges[bestIndex+1:]...)
	builder.edges = append(builder.edges,
		flowchartEdge(0, source, outputID, label),
		flowchartEdge(0, outputID, target, ""),
	)
}

func flowchartNodeByID(nodes []map[string]any, id string) map[string]any {
	for _, node := range nodes {
		if node["id"] == id {
			return node
		}
	}
	return nil
}

func flowchartNodeStartOffset(document *core.TextDocument, node map[string]any) (int, bool) {
	if node == nil {
		return 0, false
	}
	rangeValue, ok := flowchartRangeFromAny(node["range"])
	if !ok {
		return 0, false
	}
	return document.OffsetAt(rangeValue.Start), true
}

func flowchartCSTGroups(root *vbscript.CSTNode) []flowchartCSTGroup {
	groups := make([]flowchartCSTGroup, 0)
	var walk func([]*vbscript.CSTNode, bool)
	walk = func(nodes []*vbscript.CSTNode, topLevel bool) {
		pending := make([]*vbscript.CSTNode, 0)
		flush := func() {
			if len(pending) == 0 {
				return
			}
			groups = append(groups, flowchartCSTGroup{nodes: append([]*vbscript.CSTNode(nil), pending...), start: pending[0].Start, end: pending[len(pending)-1].End})
			pending = pending[:0]
		}
		for _, node := range nodes {
			switch flowchartCSTStatementKind(node) {
			case vbscript.CSTStatementSub, vbscript.CSTStatementFunction,
				vbscript.CSTStatementPropertyGet, vbscript.CSTStatementPropertyLet, vbscript.CSTStatementPropertySet:
				flush()
				groups = append(groups, flowchartCSTGroup{nodes: flowchartCSTBody(node), start: node.Start, end: node.End, scope: flowchartCSTNodeName(node)})
			case vbscript.CSTStatementClass:
				flush()
				walk(node.Children, false)
			default:
				if topLevel && flowchartCSTExecutableNode(node) {
					pending = append(pending, node)
				}
			}
		}
		flush()
	}
	walk(root.Children, true)
	return groups
}

func flowchartCSTNodeName(node *vbscript.CSTNode) string {
	if node.NameToken == nil {
		return ""
	}
	return strings.ToLower(node.NameToken.Text)
}

func flowchartCSTStatementKind(node *vbscript.CSTNode) vbscript.CSTStatementKind {
	if node == nil || node.Statement == nil {
		return ""
	}
	return node.Statement.Kind
}

func flowchartCSTExecutableNode(node *vbscript.CSTNode) bool {
	return node != nil && node.Statement != nil && node.Statement.Role != vbscript.CSTStatementRoleTerminator
}

func flowchartCSTBody(node *vbscript.CSTNode) []*vbscript.CSTNode {
	result := make([]*vbscript.CSTNode, 0, len(node.Children))
	for _, child := range node.Children {
		if child.Statement != nil && child.Statement.Role == vbscript.CSTStatementRoleTerminator {
			continue
		}
		result = append(result, child)
	}
	return result
}

func flowchartCSTTerminator(node *vbscript.CSTNode) *vbscript.CSTNode {
	for index := len(node.Children) - 1; index >= 0; index-- {
		child := node.Children[index]
		if child.Statement != nil && child.Statement.Role == vbscript.CSTStatementRoleTerminator {
			return child
		}
	}
	return nil
}

func (b *flowchartCFGBuilder) buildCSTSequence(nodes []*vbscript.CSTNode, context flowchartBuildContext) flowchartFragment {
	result := flowchartFragment{}
	reachable := true
	for _, node := range nodes {
		fragment := b.buildCSTNode(node, context)
		if fragment.entry == "" {
			continue
		}
		if result.entry == "" {
			result = fragment
			reachable = len(fragment.tails) > 0
			continue
		}
		if reachable {
			for _, tail := range result.tails {
				b.connect(tail, fragment.entry, "")
			}
			result.tails = fragment.tails
			reachable = len(fragment.tails) > 0
		}
	}
	return result
}

func (b *flowchartCFGBuilder) buildCSTNode(node *vbscript.CSTNode, context flowchartBuildContext) flowchartFragment {
	if node == nil || node.Statement == nil || node.Statement.Role == vbscript.CSTStatementRoleTerminator || node.Statement.Role == vbscript.CSTStatementRoleBranch {
		return flowchartFragment{}
	}
	switch node.Statement.Kind {
	case vbscript.CSTStatementIf:
		return b.buildCSTIf(node, context)
	case vbscript.CSTStatementSelect:
		return b.buildCSTSelect(node, context)
	case vbscript.CSTStatementFor, vbscript.CSTStatementForEach, vbscript.CSTStatementDo, vbscript.CSTStatementWhile:
		return b.buildCSTLoop(node, context)
	default:
		return b.buildCSTSimple(node, context)
	}
}

func (b *flowchartCFGBuilder) buildCSTSimple(node *vbscript.CSTNode, context flowchartBuildContext) flowchartFragment {
	statement := node.Statement
	text := flowchartCSTTokensText(statement.Tokens)
	first, second := flowchartCSTFirstTokens(statement.Tokens)
	if node.Kind == "VariableDeclaration" || first == "const" {
		result := flowchartFragment{}
		line := b.document.PositionAt(statement.Start).Line
		for _, declaration := range b.symbols.declarationsOnLine(line) {
			if declaration.Start < statement.Start || declaration.End > statement.End || declaration.Kind != "variable" && declaration.Kind != "constant" {
				continue
			}
			id := b.addNode("declaration", "Declare "+flowchartSymbolLabel(declaration), nil, declaration.Start, declaration.End)
			if result.entry == "" {
				result.entry = id
			} else {
				b.connect(result.tails[0], id, "")
			}
			result.tails = []string{id}
		}
		if result.entry != "" {
			return result
		}
	}
	kind := "statement"
	label := flowchartStatementLabel(text, b.labelMode, b.locale, b.symbols, context.scope)
	links := flowchartExpressionLinks(text, b.targets, b.symbols, context.scope)
	if first == "on" && second == "error" {
		kind = "exceptionHandling"
		label = flowchartOnErrorLabel(text, b.labelMode, b.locale)
	}
	if node.Kind == "Call" || len(statement.Parts.Callee) > 0 && first != "exit" {
		if callName, ok := flowchartCallName(text); ok {
			kind = "call"
			labels := flowchartCallLabels(text, b.labelMode, b.locale, b.symbols, context.scope)
			if target, found := b.targets[strings.ToLower(callName)]; found && target.URI != "" {
				links = append(links, map[string]any{
					"id":    "link-" + strings.ToLower(callName) + "-call",
					"label": target.Label,
					"role":  "call",
					"target": map[string]any{
						"uri": target.URI, "range": target.Range, "nameRange": target.NameRange,
					},
				})
			}
			if len(labels) > 1 {
				fragment := flowchartFragment{}
				for index, callLabel := range labels {
					nodeLinks := links
					if index > 0 {
						nodeLinks = nil
					}
					callID := b.addNode(kind, callLabel, nodeLinks, statement.Start, statement.End)
					if fragment.entry == "" {
						fragment.entry = callID
					} else {
						b.connect(fragment.tails[0], callID, "")
					}
					fragment.tails = []string{callID}
				}
				return fragment
			}
			if len(labels) == 1 {
				label = labels[0]
			}
		}
	}
	if first == "exit" {
		kind = "exit"
		label = text
	}
	id := b.addNode(kind, label, links, statement.Start, statement.End)
	b.attachOutputFragments(id, statement.Tokens)
	if kind != "exit" {
		return flowchartFragment{entry: id, tails: []string{id}}
	}
	target := context.procedureEnd
	if second == "for" || second == "do" {
		for index := len(context.loops) - 1; index >= 0; index-- {
			if context.loops[index].kind == second {
				target = context.loops[index].id
				break
			}
		}
	}
	b.connect(id, target, "Exit")
	return flowchartFragment{entry: id}
}

func (b *flowchartCFGBuilder) attachOutputFragments(nodeID string, tokens []vbscript.Token) {
	fragments := flowchartResponseWriteFragments(b.document, tokens)
	if len(fragments) == 0 {
		return
	}
	for _, node := range b.nodes {
		if node["id"] == nodeID {
			node["kind"] = "output"
			node["outputFragments"] = fragments
			return
		}
	}
}

func flowchartResponseWriteFragments(document *core.TextDocument, tokens []vbscript.Token) []map[string]any {
	writeIndex := -1
	for index := 0; index+2 < len(tokens); index++ {
		if strings.EqualFold(tokens[index].Text, "response") && tokens[index+1].Text == "." && strings.EqualFold(tokens[index+2].Text, "write") {
			writeIndex = index + 3
			break
		}
	}
	if writeIndex < 0 {
		return nil
	}
	fragments := make([]map[string]any, 0)
	for _, token := range tokens[writeIndex:] {
		if token.Kind != "string" {
			continue
		}
		text, start, end := flowchartVBStringContent(token)
		fragment := map[string]any{
			"language": flowchartOutputLanguage(text),
			"text":     text,
			"range":    document.Range(start, end),
		}
		fragments = append(fragments, fragment)
	}
	return fragments
}

func flowchartVBStringContent(token vbscript.Token) (string, int, int) {
	start, end := token.Start, token.End
	text := token.Text
	if strings.HasPrefix(text, `"`) {
		start++
		text = text[1:]
	}
	if strings.HasSuffix(text, `"`) {
		end--
		text = text[:len(text)-1]
	}
	return strings.ReplaceAll(text, `""`, `"`), start, end
}

func flowchartOutputLanguage(text string) string {
	trimmed := strings.TrimSpace(text)
	lower := strings.ToLower(trimmed)
	if strings.Contains(lower, "<script") || strings.Contains(lower, "</script") ||
		strings.Contains(lower, "<style") || strings.Contains(lower, "</style") ||
		strings.Contains(lower, "<!doctype") || flowchartLooksLikeHTML(trimmed) {
		return "html"
	}
	if flowchartLooksLikeCSS(trimmed) {
		return "css"
	}
	if flowchartLooksLikeJavaScript(trimmed) {
		return "javascript"
	}
	return "text"
}

func flowchartLooksLikeHTML(value string) bool {
	if len(value) < 3 || value[0] != '<' {
		return false
	}
	index := 1
	if value[index] == '/' {
		index++
	}
	return index < len(value) && (value[index] >= 'A' && value[index] <= 'Z' || value[index] >= 'a' && value[index] <= 'z') && strings.Contains(value[index:], ">")
}

func flowchartLooksLikeCSS(value string) bool {
	open := strings.IndexByte(value, '{')
	close := strings.LastIndexByte(value, '}')
	if open > 0 && close > open {
		return strings.Contains(value[open+1:close], ":")
	}
	colon := strings.IndexByte(value, ':')
	return colon > 0 && strings.Contains(value[colon+1:], ";") && !strings.Contains(value[:colon], "//")
}

func flowchartLooksLikeJavaScript(value string) bool {
	lower := strings.ToLower(value)
	for _, prefix := range []string{"const ", "let ", "var ", "function ", "class ", "import ", "export ", "return ", "document.", "window."} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return strings.Contains(value, "=>") || strings.HasSuffix(value, ");")
}

func (b *flowchartCFGBuilder) buildCSTIf(node *vbscript.CSTNode, context flowchartBuildContext) flowchartFragment {
	condition := flowchartCSTTokensText(node.Statement.Parts.Condition)
	conditionID := b.addNode("if", flowchartIfLabel(condition, b.labelMode, b.locale), flowchartExpressionLinks(condition, b.targets, b.symbols, context.scope), node.Start, node.End)
	joinStart, joinEnd := node.End, node.End
	if terminator := flowchartCSTTerminator(node); terminator != nil {
		joinStart, joinEnd = terminator.Statement.Start, terminator.Statement.End
	}
	joinID := b.addNode("merge", "Continue", nil, joinStart, joinEnd)
	thenNodes := make([]*vbscript.CSTNode, 0)
	branches := make([]*vbscript.CSTNode, 0)
	for _, child := range node.Children {
		if child.Statement == nil {
			continue
		}
		switch child.Statement.Role {
		case vbscript.CSTStatementRoleBranch:
			branches = append(branches, child)
		case vbscript.CSTStatementRoleExecutable, vbscript.CSTStatementRoleHeader:
			thenNodes = append(thenNodes, child)
		}
	}
	joinReachable := b.connectCSTBranch(conditionID, thenNodes, "Yes", joinID, context)
	currentCondition := conditionID
	hasElse := false
	for _, branch := range branches {
		switch branch.Statement.Kind {
		case vbscript.CSTStatementElseIf:
			branchCondition := flowchartCSTTokensText(branch.Statement.Parts.Condition)
			nextCondition := b.addNode("elseif", flowchartIfLabel(branchCondition, b.labelMode, b.locale), flowchartExpressionLinks(branchCondition, b.targets, b.symbols, context.scope), branch.Statement.Start, branch.Statement.End)
			b.connect(currentCondition, nextCondition, "No")
			currentCondition = nextCondition
			joinReachable = b.connectCSTBranch(currentCondition, flowchartCSTBody(branch), "Yes", joinID, context) || joinReachable
		case vbscript.CSTStatementElse:
			hasElse = true
			elseID := b.addNode("else", "Else", nil, branch.Statement.Start, branch.Statement.End)
			b.connect(currentCondition, elseID, "No")
			joinReachable = b.connectCSTBranch(elseID, flowchartCSTBody(branch), "", joinID, context) || joinReachable
		}
	}
	if !hasElse {
		b.connect(currentCondition, joinID, "No")
		joinReachable = true
	}
	tails := []string(nil)
	if joinReachable {
		tails = []string{joinID}
	}
	return flowchartFragment{entry: conditionID, tails: tails}
}

func (b *flowchartCFGBuilder) connectCSTBranch(source string, nodes []*vbscript.CSTNode, label, join string, context flowchartBuildContext) bool {
	fragment := b.buildCSTSequence(nodes, context)
	if fragment.entry == "" {
		b.connect(source, join, label)
		return true
	}
	b.connect(source, fragment.entry, label)
	for _, tail := range fragment.tails {
		b.connect(tail, join, "")
	}
	return len(fragment.tails) > 0
}

func (b *flowchartCFGBuilder) buildCSTSelect(node *vbscript.CSTNode, context flowchartBuildContext) flowchartFragment {
	selector := flowchartCSTTokensText(node.Statement.Parts.Selector)
	selectID := b.addNode("select", selector, flowchartExpressionLinks(selector, b.targets, b.symbols, context.scope), node.Start, node.End)
	joinStart, joinEnd := node.End, node.End
	if terminator := flowchartCSTTerminator(node); terminator != nil {
		joinStart, joinEnd = terminator.Statement.Start, terminator.Statement.End
	}
	joinID := b.addNode("merge", "Continue", nil, joinStart, joinEnd)
	hasElse := false
	joinReachable := false
	for _, branch := range node.Children {
		if branch.Statement == nil || branch.Statement.Role != vbscript.CSTStatementRoleBranch {
			continue
		}
		value := flowchartCSTTokensText(branch.Statement.Parts.CaseValues)
		label := "When " + value
		if branch.Statement.Kind == vbscript.CSTStatementCaseElse {
			label = "Else"
			hasElse = true
		}
		caseID := b.addNode("case", label, flowchartExpressionLinks(value, b.targets, b.symbols, context.scope), branch.Statement.Start, branch.Statement.End)
		b.connect(selectID, caseID, label)
		joinReachable = b.connectCSTBranch(caseID, flowchartCSTBody(branch), "", joinID, context) || joinReachable
	}
	if !hasElse {
		b.connect(selectID, joinID, "No match")
		joinReachable = true
	}
	tails := []string(nil)
	if joinReachable {
		tails = []string{joinID}
	}
	return flowchartFragment{entry: selectID, tails: tails}
}

func (b *flowchartCFGBuilder) buildCSTLoop(node *vbscript.CSTNode, context flowchartBuildContext) flowchartFragment {
	statement := node.Statement
	kind := "for"
	loopTarget := "for"
	switch statement.Kind {
	case vbscript.CSTStatementForEach:
		kind = "forEach"
	case vbscript.CSTStatementDo:
		kind, loopTarget = "do", "do"
	case vbscript.CSTStatementWhile:
		kind = "while"
	}
	headerText := flowchartCSTTokensText(statement.Tokens)
	headerID := b.addNode(kind, headerText, flowchartExpressionLinks(headerText, b.targets, b.symbols, context.scope), node.Start, node.End)
	terminator := flowchartCSTTerminator(node)
	exitStart, exitEnd := node.End, node.End
	if terminator != nil {
		exitStart, exitEnd = terminator.Statement.Start, terminator.Statement.End
	}
	exitID := b.addNode("merge", "After "+flowchartLoopDisplayName(kind), nil, exitStart, exitEnd)
	context.loops = append(context.loops, flowchartLoopTarget{kind: loopTarget, id: exitID})
	body := b.buildCSTSequence(flowchartCSTBody(node), context)
	if statement.Kind == vbscript.CSTStatementDo && terminator != nil && len(terminator.Statement.Parts.LoopCondition) > 0 {
		condition := flowchartCSTTokensText(terminator.Statement.Parts.LoopCondition)
		conditionText := flowchartCSTTokensText(terminator.Statement.Tokens)
		conditionID := b.addNode("do", conditionText, flowchartExpressionLinks(condition, b.targets, b.symbols, context.scope), terminator.Statement.Start, terminator.Statement.End)
		if body.entry == "" {
			b.connect(headerID, conditionID, "")
		} else {
			b.connect(headerID, body.entry, "")
			for _, tail := range body.tails {
				b.connect(tail, conditionID, "")
			}
		}
		continueLabel, exitLabel := "Yes", "No"
		if terminator.Statement.Parts.Until {
			continueLabel, exitLabel = "No", "Yes"
		}
		b.connect(conditionID, headerID, continueLabel)
		b.connect(conditionID, exitID, exitLabel)
		return flowchartFragment{entry: headerID, tails: []string{exitID}}
	}
	if statement.Kind == vbscript.CSTStatementDo && len(statement.Parts.LoopCondition) == 0 {
		if body.entry == "" {
			b.connect(headerID, headerID, "Repeat")
		} else {
			b.connect(headerID, body.entry, "")
			for _, tail := range body.tails {
				b.connect(tail, headerID, "Repeat")
			}
		}
		if flowchartCSTContainsExit(node, "do") {
			return flowchartFragment{entry: headerID, tails: []string{exitID}}
		}
		return flowchartFragment{entry: headerID}
	}
	continueLabel, exitLabel := "Yes", "No"
	if statement.Parts.Until {
		continueLabel, exitLabel = "No", "Yes"
	}
	if body.entry == "" {
		b.connect(headerID, headerID, continueLabel)
	} else {
		b.connect(headerID, body.entry, continueLabel)
		for _, tail := range body.tails {
			b.connect(tail, headerID, "Repeat")
		}
	}
	b.connect(headerID, exitID, exitLabel)
	return flowchartFragment{entry: headerID, tails: []string{exitID}}
}

func flowchartCSTContainsExit(node *vbscript.CSTNode, target string) bool {
	for _, child := range node.Children {
		if child.Statement != nil {
			first, second := flowchartCSTFirstTokens(child.Statement.Tokens)
			if first == "exit" && second == target {
				return true
			}
		}
		if flowchartCSTContainsExit(child, target) {
			return true
		}
	}
	return false
}

func flowchartCSTFirstTokens(tokens []vbscript.Token) (string, string) {
	first, second := "", ""
	for _, token := range tokens {
		if token.Kind == "newline" || token.Text == "_" {
			continue
		}
		if first == "" {
			first = strings.ToLower(token.Text)
		} else {
			second = strings.ToLower(token.Text)
			break
		}
	}
	return first, second
}

func flowchartCSTTokensText(tokens []vbscript.Token) string {
	var result strings.Builder
	previous := ""
	for index, token := range tokens {
		if token.Kind == "newline" || token.Text == "_" && index+1 < len(tokens) && tokens[index+1].Kind == "newline" {
			continue
		}
		text := token.Text
		if result.Len() > 0 && flowchartCSTNeedsSpace(previous, text) {
			result.WriteByte(' ')
		}
		result.WriteString(text)
		previous = text
	}
	return strings.TrimSpace(result.String())
}

func flowchartCSTNeedsSpace(previous, current string) bool {
	if previous == "" || current == "" {
		return false
	}
	if current == ")" || current == "," || current == "." || current == ":" {
		return false
	}
	if previous == "(" || previous == "." {
		return false
	}
	if current == "(" {
		return false
	}
	return true
}
