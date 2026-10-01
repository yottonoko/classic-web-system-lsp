package lspserver

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
)

type inlayScopeMarkerSettings struct {
	Global    bool
	Local     bool
	Uncertain bool
}

type vbscriptVariableTypeInlayOptions struct {
	VariableTypes       bool
	ScopeMarkers        inlayScopeMarkerSettings
	IncludeAware        bool
	IncludedGlobalNames map[string]struct{}
}

func vbscriptVariableTypeInlayHints(parsed *core.ParsedDocument, r lsp.Range, options vbscriptVariableTypeInlayOptions) []lsp.InlayHint {
	if !options.VariableTypes {
		return nil
	}
	doc := core.SourceDocument(parsed)
	startOffset := doc.OffsetAt(r.Start)
	endOffset := doc.OffsetAt(r.End)
	declarations := variableInlayDeclarations(parsed, options.IncludeAware, options.IncludedGlobalNames)
	var hints []lsp.InlayHint
	for _, declaration := range declarations {
		if declaration.End < startOffset || declaration.End > endOffset {
			continue
		}
		label := ""
		if declaration.Uncertain {
			if options.ScopeMarkers.Uncertain {
				label += " (?)"
			}
		} else if declaration.Local && options.ScopeMarkers.Local {
			label += " (local)"
		} else if !declaration.Local && options.ScopeMarkers.Global {
			label += " (global)"
		}
		label += " As " + inferVBDeclarationType(parsed, declaration)
		if label == "" {
			continue
		}
		hints = append(hints, lsp.InlayHint{
			Position:     doc.PositionAt(declaration.End),
			Label:        label,
			Kind:         1,
			PaddingLeft:  lsp.BoolPtr(false),
			PaddingRight: lsp.BoolPtr(true),
		})
	}
	return hints
}

func vbscriptFunctionReturnTypeInlayHints(parsed *core.ParsedDocument, r lsp.Range, enabled bool) []lsp.InlayHint {
	if !enabled || parsed == nil {
		return nil
	}
	doc := core.SourceDocument(parsed)
	startOffset := doc.OffsetAt(r.Start)
	endOffset := doc.OffsetAt(r.End)
	analysis := graphAnalysisTypes(parsed)
	var hints []lsp.InlayHint
	for _, signature := range vbscript.Signatures(parsed) {
		if signature.Kind != "function" && signature.Kind != "property" {
			continue
		}
		hintOffset := doc.OffsetAt(signature.Range.End)
		if hintOffset < startOffset || hintOffset > endOffset {
			continue
		}
		typeName := graphReturnTypeForSignature(parsed, signature, &analysis)
		if typeName == "" {
			typeName = precedingVBReturnsType(parsed.Text, doc.OffsetAt(signature.Range.Start))
		}
		if typeName == "" {
			continue
		}
		hints = append(hints, lsp.InlayHint{
			Position:     signature.Range.End,
			Label:        " As " + typeName,
			Kind:         1,
			PaddingLeft:  lsp.BoolPtr(false),
			PaddingRight: lsp.BoolPtr(true),
		})
	}
	return hints
}

func graphReturnTypeForSignature(parsed *core.ParsedDocument, signature vbscript.Signature, details *vbGraphAnalysisTypes) string {
	if details == nil {
		return ""
	}
	if typeName := details.ScopedReturns[graphSignatureKey(graphSignatureOwner(parsed, signature), signature.Name, graphSignatureAccessor(parsed, signature))]; typeName != "" {
		return strings.TrimSpace(typeName)
	}
	return strings.TrimSpace(details.Returns[strings.ToLower(signature.Name)])
}

func precedingVBReturnsType(text string, declarationOffset int) string {
	if declarationOffset <= 0 || declarationOffset > len(text) {
		return ""
	}
	lineStart := strings.LastIndex(text[:declarationOffset], "\n") + 1
	for lineStart > 0 {
		previousEnd := lineStart - 1
		if previousEnd > 0 && text[previousEnd-1] == '\r' {
			previousEnd--
		}
		previousStart := strings.LastIndex(text[:previousEnd], "\n") + 1
		line := strings.TrimSpace(text[previousStart:previousEnd])
		if line == "" {
			lineStart = previousStart
			continue
		}
		if !strings.HasPrefix(line, "'") {
			return ""
		}
		annotation := strings.TrimSpace(strings.TrimPrefix(line, "'"))
		if strings.HasPrefix(strings.ToLower(annotation), "@returns") {
			typeName := strings.TrimSpace(annotation[len("@returns"):])
			if _, err := parseVBScriptType(typeName); err == nil {
				return typeName
			}
		}
		lineStart = previousStart
	}
	return ""
}

func variableInlayDeclarations(parsed *core.ParsedDocument, includeAware bool, includedGlobalNames map[string]struct{}) []vbUsageDeclaration {
	const defaultAnalysisKey = "lspserver.vb-variable-inlay-declarations.v1"
	cacheable := includeAware && includedGlobalNames == nil
	if cacheable {
		if value, ok := parsed.LoadRuntimeAnalysis(defaultAnalysisKey); ok {
			if cached, ok := value.([]vbUsageDeclaration); ok {
				return cached
			}
		}
	}
	byRange := map[offsetRange]int{}
	var declarations []vbUsageDeclaration
	for _, declaration := range collectVBUsageDeclarations(parsed).Declarations {
		if !isVBVariableInlayDeclaration(declaration) {
			continue
		}
		key := offsetRangeKey(declaration.Start, declaration.End)
		if index, ok := byRange[key]; ok {
			if shouldPreferVBVariableInlayDeclaration(declaration, declarations[index]) {
				declarations[index] = declaration
			}
			continue
		}
		byRange[key] = len(declarations)
		declarations = append(declarations, declaration)
	}
	for _, declaration := range serverObjectDeclarations(parsed) {
		key := offsetRangeKey(declaration.Start, declaration.End)
		if _, ok := byRange[key]; ok {
			continue
		}
		byRange[key] = len(declarations)
		declarations = append(declarations, declaration)
	}
	for _, declaration := range implicitVBDeclarations(parsed) {
		if implicitAssignmentDuplicateOfEarlierIncludeGlobal(parsed, declaration, includedGlobalNames) {
			continue
		}
		if !includeAware && len(parsed.Includes) > 0 && !declaration.Local {
			declaration.Uncertain = true
		}
		key := offsetRangeKey(declaration.Start, declaration.End)
		if _, ok := byRange[key]; ok {
			continue
		}
		byRange[key] = len(declarations)
		declarations = append(declarations, declaration)
	}
	if cacheable {
		parsed.StoreRuntimeAnalysis(defaultAnalysisKey, declarations)
	}
	return declarations
}

func includedVBVariableInlayGlobalNames(documents []*core.ParsedDocument) map[string]struct{} {
	return includedVBVariableInlayGlobalNamesContext(context.Background(), documents)
}

func includedVBVariableInlayGlobalNamesContext(ctx context.Context, documents []*core.ParsedDocument) map[string]struct{} {
	if ctx == nil {
		ctx = context.Background()
	}
	names := map[string]struct{}{}
	for _, document := range documents {
		if ctx.Err() != nil {
			return nil
		}
		for _, declaration := range variableInlayDeclarations(document, true, nil) {
			if ctx.Err() != nil {
				return nil
			}
			if declaration.Local {
				continue
			}
			names[strings.ToLower(declaration.Name)] = struct{}{}
		}
	}
	if ctx.Err() != nil {
		return nil
	}
	return names
}

func shouldPreferVBVariableInlayDeclaration(candidate, current vbUsageDeclaration) bool {
	if candidate.Local != current.Local {
		return candidate.Local
	}
	if current.Scope == "" && candidate.Scope != "" {
		return true
	}
	return current.Kind == "" && candidate.Kind != ""
}

func isVBVariableInlayDeclaration(declaration vbUsageDeclaration) bool {
	switch declaration.Kind {
	case "variable", "constant", "const":
		return true
	default:
		return false
	}
}

func inferVBDeclarationType(parsed *core.ParsedDocument, declaration vbUsageDeclaration) string {
	if declaration.TypeName != "" {
		return declaration.TypeName
	}
	if typeName := precedingVBTypeAnnotation(parsed, declaration.Line, declaration.Name); typeName != "" {
		return typeName
	}
	if typeName := vbscriptLiteralUnionTypeForDeclaration(parsed, declaration); typeName != "" {
		return typeName
	}
	types := assignedVBValueTypesForDocument(parsed, parsed.Text, declaration.Name)
	if declaration.Implicit {
		types = assignedVBValueTypesForImplicitDeclaration(parsed, declaration)
	}
	if len(types) > 0 {
		return strings.Join(types, " | ")
	}
	if declaration.AssignedValue != "" {
		return inferVBValueTypeForDocument(parsed, declaration.AssignedValue)
	}
	if declaration.Kind == "constant" || declaration.Kind == "const" {
		if line := lineTextAtOffset(parsed.Text, declaration.Start); line != "" {
			if value, ok := valueAfterAssignment(line); ok {
				return inferVBExpressionType(parsed, value)
			}
		}
	}
	if value, ok := firstAssignmentValueAfter(parsed.Text, declaration.Name, declaration.End); ok {
		return inferVBValueTypeForDocument(parsed, value)
	}
	return "Variant"
}

func vbscriptLiteralUnionTypeForDeclaration(parsed *core.ParsedDocument, declaration vbUsageDeclaration) string {
	if parsed == nil || declaration.TypeName != "" || declaration.Kind == "constant" || declaration.Kind == "const" || precedingVBTypeAnnotation(parsed, declaration.Line, declaration.Name) != "" {
		return ""
	}
	var inferred vbscriptType
	foundAssignment := false
	unknownAssignment := false
	assignments := vbscriptAssignments(parsed)
	for _, index := range vbscriptLiteralAssignmentIndices(parsed, assignments, declaration) {
		assignment := assignments[index]
		foundAssignment = true
		value := inferVBScriptValueLiteralType(assignment.Value)
		if value.isUnknown() {
			// A finite literal union is only sound while every visible write is
			// known. Keep an unknown write as a conservative fallback instead of
			// silently presenting the known writes as an exhaustive union.
			unknownAssignment = true
			continue
		}
		inferred = mergeVBScriptMutableTypes(inferred, value)
	}
	if !foundAssignment || unknownAssignment || inferred.isUnknown() {
		if foundAssignment {
			return "Variant"
		}
		return ""
	}
	parts := inferred.parts
	if inferred.kind != vbscriptTypeUnion {
		parts = []vbscriptType{inferred}
	}
	base := ""
	for _, part := range parts {
		if !part.isLiteral() {
			return ""
		}
		if base == "" {
			base = part.primitiveName()
		} else if !strings.EqualFold(base, part.primitiveName()) {
			return ""
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return inferred.String()
}

type vbLiteralAssignmentIndex struct {
	indices map[[2]string][]int
	bytes   int64
}

// EstimateBytes reports the immutable index size without traversing its map.
func (index *vbLiteralAssignmentIndex) EstimateBytes() int64 { return index.bytes }

// Index writes once per immutable revision instead of scanning every assignment
// for every declaration during the first summary and type checks.
func vbscriptLiteralAssignmentIndices(parsed *core.ParsedDocument, assignments []vbAssignment, declaration vbUsageDeclaration) []int {
	const key = "lspserver.vb-literal-assignment-index.v1"
	var index map[[2]string][]int
	if value, ok := parsed.LoadRuntimeAnalysis(key); ok {
		if cached, ok := value.(*vbLiteralAssignmentIndex); ok {
			index = cached.indices
		}
	}
	if index == nil {
		index = make(map[[2]string][]int)
		for position, assignment := range assignments {
			identity := [2]string{literalAssignmentFoldKey(assignment.Name), literalAssignmentFoldKey(assignment.Scope)}
			index[identity] = append(index[identity], position)
		}
		bytes := int64(64)
		for identity, positions := range index {
			bytes += 96 + int64(len(identity[0])+len(identity[1]))*2 + int64(cap(positions))*8
		}
		parsed.StoreRuntimeAnalysis(key, &vbLiteralAssignmentIndex{indices: index, bytes: bytes})
	}
	return index[[2]string{literalAssignmentFoldKey(declaration.Name), literalAssignmentFoldKey(declaration.Scope)}]
}

// Use the same Unicode simple-fold equivalence as strings.EqualFold.
func literalAssignmentFoldKey(value string) string {
	return strings.Map(func(r rune) rune {
		if r < unicode.MaxASCII {
			if r >= 'A' && r <= 'Z' {
				return r + ('a' - 'A')
			}
			return r
		}
		smallest := r
		for next := unicode.SimpleFold(r); next != r; next = unicode.SimpleFold(next) {
			if next < smallest {
				smallest = next
			}
		}
		if smallest >= 'A' && smallest <= 'Z' {
			return smallest + ('a' - 'A')
		}
		return smallest
	}, value)
}

const assignedVBValueTypesAnalysisKey = "lspserver.vb-assigned-value-types.v1"

func vbGraphProcedureScopeAtLine(procedures []graphVBProcedureRange, line int) string {
	for _, procedure := range procedures {
		if line >= procedure.startLine && line <= procedure.endLine {
			return procedure.key()
		}
	}
	return ""
}

// assignedVBValueTypesForImplicitDeclaration indexes assignment values once
// per immutable document. The old implementation repeated the complete source
// scan for every implicit declaration, including a scope-shadow lookup for
// every line. Global declarations intentionally retain values from procedure
// assignments unless that procedure declares a local with the same name;
// implicit procedure declarations retain their procedure-local view as well.
func assignedVBValueTypesForImplicitDeclaration(parsed *core.ParsedDocument, declaration vbUsageDeclaration) []string {
	if parsed == nil {
		return nil
	}
	values := assignedVBValueTypesForDocumentIndex(parsed)
	return values[implicitDeclarationScopeKey(declaration.Local, declaration.Scope, declaration.Name)]
}

func assignedVBValueTypesForDocumentIndex(parsed *core.ParsedDocument) map[string][]string {
	if parsed == nil {
		return nil
	}
	if value, ok := parsed.LoadRuntimeAnalysis(assignedVBValueTypesAnalysisKey); ok {
		if cached, ok := value.(map[string][]string); ok {
			return cached
		}
	}
	values := map[string][]string{}
	procedures := graphVBProcedureRanges(parsed)
	localNames := vbLocalDeclarationShadowNames(parsed)
	appendType := func(local bool, scope, name, typeName string) {
		key := implicitDeclarationScopeKey(local, scope, name)
		values[key] = append(values[key], typeName)
	}
	for _, assignment := range vbscriptAssignments(parsed) {
		name := strings.ToLower(assignment.Name)
		scope := vbGraphProcedureScopeAtLine(procedures, assignment.NameRange.Start.Line)
		typeName := inferVBValueTypeForDocument(parsed, assignment.Value)
		if scope == "" {
			appendType(false, "", name, typeName)
			// Keep the same behavior for the degenerate local-empty scope as the
			// previous line-based matcher.
			appendType(true, "", name, typeName)
			continue
		}
		if localNames[scope][name] {
			appendType(true, scope, name, typeName)
			continue
		}
		appendType(false, "", name, typeName)
		appendType(true, scope, name, typeName)
	}
	for key, types := range values {
		seen := make(map[string]struct{}, len(types))
		unique := types[:0]
		for _, typeName := range types {
			if _, exists := seen[typeName]; exists {
				continue
			}
			seen[typeName] = struct{}{}
			unique = append(unique, typeName)
		}
		values[key] = unique
	}
	parsed.StoreRuntimeAnalysis(assignedVBValueTypesAnalysisKey, values)
	return values
}

func inferVBExpressionType(parsed *core.ParsedDocument, value string) string {
	typeName := inferVBValueTypeForDocument(parsed, value)
	if typeName != "" && !strings.EqualFold(typeName, "Variant") {
		return typeName
	}
	if parsed != nil {
		if callee := vbscriptSimpleCallName(value); callee != "" {
			if types := assignedVBValueTypesForDocument(parsed, parsed.Text, callee); len(types) > 0 {
				return strings.Join(types, " | ")
			}
		}
	}
	return typeName
}

func precedingVBTypeAnnotation(parsed *core.ParsedDocument, declarationLine int, name string) string {
	if parsed == nil {
		return ""
	}
	if name == "" || declarationLine <= 0 {
		return ""
	}
	lines := vbscriptSourceLines(parsed)
	if declarationLine > len(lines) {
		declarationLine = len(lines)
	}
	regionLines := vbscriptRegionLineSet(parsed)
	for i := declarationLine - 1; i >= 0; i-- {
		if _, ok := regionLines[i]; !ok {
			return ""
		}
		line := strings.TrimSpace(strings.TrimRight(lines[i], "\r"))
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "'") {
			return ""
		}
		annotation := strings.TrimSpace(strings.TrimPrefix(line, "'"))
		parsed, ok := parseVBScriptTypeAnnotationText(annotation)
		if ok && parsed.err == nil && parsed.kind == "type" && strings.EqualFold(parsed.name, name) {
			return parsed.typeText
		}
	}
	return ""
}

const vbscriptSourceLinesAnalysisKey = "lspserver.vb-source-lines.v1"

// vbSourceLines is the runtime-cached line split of a document. The lines are
// substrings of the document text, which the parsed document already charges,
// so only the slice of string headers is retained here.
type vbSourceLines []string

func (lines vbSourceLines) EstimateBytes() int64 {
	return int64(cap(lines)) * 16
}

func vbscriptSourceLines(parsed *core.ParsedDocument) []string {
	if parsed == nil {
		return nil
	}
	if value, ok := parsed.LoadRuntimeAnalysis(vbscriptSourceLinesAnalysisKey); ok {
		if cached, ok := value.(vbSourceLines); ok {
			return cached
		}
	}
	lines := strings.Split(parsed.Text, "\n")
	parsed.StoreRuntimeAnalysis(vbscriptSourceLinesAnalysisKey, vbSourceLines(lines))
	return lines
}

const vbscriptRegionLineSetAnalysisKey = "lspserver.vb-region-line-set.v1"

func vbscriptRegionLineSet(parsed *core.ParsedDocument) map[int]struct{} {
	if parsed == nil {
		return map[int]struct{}{}
	}
	if value, ok := parsed.LoadRuntimeAnalysis(vbscriptRegionLineSetAnalysisKey); ok {
		if cached, ok := value.(map[int]struct{}); ok {
			return cached
		}
	}
	lines := map[int]struct{}{}
	doc := vbTextDocument(parsed)
	for _, region := range parsed.Regions {
		if region.Language != core.LanguageVBScript {
			continue
		}
		for lineStart := region.ContentStart; lineStart < region.ContentEnd; {
			lines[doc.PositionAt(lineStart).Line] = struct{}{}
			lineEnd := lineStart
			for lineEnd < region.ContentEnd && parsed.Text[lineEnd] != '\n' && parsed.Text[lineEnd] != '\r' {
				lineEnd++
			}
			if lineEnd >= region.ContentEnd {
				break
			}
			lineStart = lineEnd + 1
			if parsed.Text[lineEnd] == '\r' && lineStart < region.ContentEnd && parsed.Text[lineStart] == '\n' {
				lineStart++
			}
		}
	}
	parsed.StoreRuntimeAnalysis(vbscriptRegionLineSetAnalysisKey, lines)
	return lines
}

const assignedVBValueTypesByNameAnalysisKey = "lspserver.vb-assigned-value-types-by-name.v1"

func assignedVBValueTypesForDocument(parsed *core.ParsedDocument, text string, name string) []string {
	if parsed != nil && text == parsed.Text {
		if value, ok := parsed.LoadRuntimeAnalysis(assignedVBValueTypesByNameAnalysisKey); ok {
			if cached, ok := value.(map[string][]string); ok {
				return cached[strings.ToLower(name)]
			}
		}
	}
	values := map[string][]string{}
	seen := map[string]map[string]struct{}{}
	for _, line := range strings.Split(text, "\n") {
		for _, statement := range splitVBStatements(strings.TrimRight(line, "\r")) {
			lowerName, typeName, ok := assignedVBValueFromStatementForDocument(parsed, statement)
			if !ok {
				continue
			}
			if seen[lowerName] == nil {
				seen[lowerName] = map[string]struct{}{}
			}
			if _, ok := seen[lowerName][typeName]; ok {
				continue
			}
			seen[lowerName][typeName] = struct{}{}
			values[lowerName] = append(values[lowerName], typeName)
		}
	}
	if parsed != nil && text == parsed.Text {
		parsed.StoreRuntimeAnalysis(assignedVBValueTypesByNameAnalysisKey, values)
	}
	return values[strings.ToLower(name)]
}

func assignedVBValueTypeFromStatement(statement string, lowerName string) (string, bool) {
	return assignedVBValueTypeFromStatementForDocument(nil, statement, lowerName)
}

func assignedVBValueTypeFromStatementForDocument(parsed *core.ParsedDocument, statement string, lowerName string) (string, bool) {
	name, typeName, ok := assignedVBValueFromStatementForDocument(parsed, statement)
	if !ok || name != lowerName {
		return "", false
	}
	return typeName, true
}

func assignedVBValueFromStatementForDocument(parsed *core.ParsedDocument, statement string) (string, string, bool) {
	trimmed := strings.TrimSpace(statement)
	if trimmed == "" || strings.HasPrefix(trimmed, "'") {
		return "", "", false
	}
	equalIndex := topLevelEqual(trimmed)
	if equalIndex < 0 {
		return "", "", false
	}
	target := strings.TrimSpace(trimmed[:equalIndex])
	expression := strings.TrimSpace(trimmed[equalIndex+1:])
	if expression == "" {
		return "", "", false
	}
	cursor := 0
	firstEnd := readVBIdentifier(target, cursor)
	if firstEnd == 0 {
		return "", "", false
	}
	first := strings.ToLower(target[:firstEnd])
	if first == "set" || first == "let" {
		cursor = firstEnd
		for cursor < len(target) && isVBWhitespace(target[cursor]) {
			cursor++
		}
	}
	targetStart := cursor
	targetEnd := readVBIdentifier(target, targetStart)
	if targetEnd == targetStart {
		return "", "", false
	}
	lowerName := strings.ToLower(target[targetStart:targetEnd])
	cursor = targetEnd
	for cursor < len(target) && isVBWhitespace(target[cursor]) {
		cursor++
	}
	if cursor < len(target) {
		return "", "", false
	}
	return lowerName, inferVBValueTypeForDocument(parsed, expression), true
}

func implicitAssignmentInlayDeclarations(parsed *core.ParsedDocument, includeAware bool, includedGlobalNames map[string]struct{}) []vbUsageDeclaration {
	if hasVBOptionExplicit(parsed) {
		return nil
	}
	doc := core.SourceDocument(parsed)
	declared := map[string]struct{}{}
	for _, declaration := range collectVBUsageDeclarations(parsed).Declarations {
		declared[implicitDeclarationScopeKey(declaration.Local, declaration.Scope, declaration.Name)] = struct{}{}
	}
	isBuiltin := func(lower string) bool {
		return isDeclaredVBBuiltinOrKeywordForDocument(parsed, lower)
	}
	candidates := map[string]vbUsageDeclaration{}
	procedureScopes := vbProcedureScopes(parsed)
	procedureScopeIndex := newVBProcedureScopeIndex(procedureScopes)
	for _, region := range parsed.Regions {
		if region.Language != core.LanguageVBScript {
			continue
		}
		for lineStart := region.ContentStart; lineStart < region.ContentEnd; {
			lineEnd := lineStart
			for lineEnd < region.ContentEnd && parsed.Text[lineEnd] != '\n' && parsed.Text[lineEnd] != '\r' {
				lineEnd++
			}
			line := parsed.Text[lineStart:lineEnd]
			currentScope := procedureScopeIndex.at(lineStart)
			inProcedure := currentScope != ""
			if declaration, ok := implicitAssignmentInlayDeclaration(doc, line, lineStart, currentScope, inProcedure, len(parsed.Includes) > 0 && !includeAware, declared, isBuiltin); ok {
				if !implicitAssignmentDuplicateOfEarlierIncludeGlobal(parsed, declaration, includedGlobalNames) {
					key := implicitAssignmentInlayKey(declaration)
					candidates[key] = preferredImplicitAssignmentInlayDeclaration(declaration, candidates[key])
				}
			}
			for _, declaration := range implicitSingleLineIfAssignmentDeclarations(doc, line, lineStart, currentScope, inProcedure, len(parsed.Includes) > 0 && !includeAware, declared, isBuiltin) {
				if !implicitAssignmentDuplicateOfEarlierIncludeGlobal(parsed, declaration, includedGlobalNames) {
					key := implicitAssignmentInlayKey(declaration)
					candidates[key] = preferredImplicitAssignmentInlayDeclaration(declaration, candidates[key])
				}
			}
			if lineEnd >= region.ContentEnd {
				break
			}
			lineStart = lineEnd + 1
			if parsed.Text[lineEnd] == '\r' && lineStart < region.ContentEnd && parsed.Text[lineStart] == '\n' {
				lineStart++
			}
		}
	}
	declarations := make([]vbUsageDeclaration, 0, len(candidates))
	for _, declaration := range candidates {
		declarations = append(declarations, declaration)
	}
	sort.SliceStable(declarations, func(i, j int) bool {
		return declarations[i].Start < declarations[j].Start
	})
	return declarations
}

func implicitSingleLineIfAssignmentDeclarations(doc *core.TextDocument, line string, lineOffset int, scope string, inProcedure bool, uncertain bool, declared map[string]struct{}, isBuiltin func(string) bool) []vbUsageDeclaration {
	segments := singleLineIfStatementSegments(line)
	if len(segments) == 0 {
		return nil
	}
	declarations := make([]vbUsageDeclaration, 0, len(segments))
	for _, segment := range segments {
		declaration, ok := implicitAssignmentInlayDeclaration(doc, line[segment.Start:segment.End], lineOffset+segment.Start, scope, inProcedure, uncertain, declared, isBuiltin)
		if ok {
			declarations = append(declarations, declaration)
		}
	}
	return declarations
}

type vbLineSegment struct {
	Start int
	End   int
}

func singleLineIfStatementSegments(line string) []vbLineSegment {
	cursor := leadingVBWhitespace(line)
	if end := readVBIdentifier(line, cursor); end == cursor || !strings.EqualFold(line[cursor:end], "if") {
		return nil
	}
	if _, thenEnd := vbKeywordSpanOutsideStrings(line, "then", 0); thenEnd >= 0 {
		statementStart := thenEnd
		for statementStart < len(line) && isVBWhitespace(line[statementStart]) {
			statementStart++
		}
		if statementStart >= len(line) || line[statementStart] == '_' {
			return nil
		}
		elseStart, elseEnd := vbKeywordSpanOutsideStrings(line, "else", statementStart)
		if elseStart < 0 {
			return []vbLineSegment{{Start: statementStart, End: len(line)}}
		}
		segments := []vbLineSegment{{Start: statementStart, End: trimSegmentEnd(line, elseStart)}}
		elseStatementStart := elseEnd
		for elseStatementStart < len(line) && isVBWhitespace(line[elseStatementStart]) {
			elseStatementStart++
		}
		if elseStatementStart < len(line) {
			segments = append(segments, vbLineSegment{Start: elseStatementStart, End: len(line)})
		}
		return segments
	}
	return nil
}

func trimSegmentEnd(line string, end int) int {
	for end > 0 && (line[end-1] == ' ' || line[end-1] == '\t') {
		end--
	}
	return end
}

func vbKeywordSpanOutsideStrings(line string, keyword string, start int) (int, int) {
	lowerKeyword := strings.ToLower(keyword)
	inString := false
	for i := start; i < len(line); {
		if line[i] == '"' {
			if inString && i+1 < len(line) && line[i+1] == '"' {
				i += 2
				continue
			}
			inString = !inString
			i++
			continue
		}
		if inString {
			i++
			continue
		}
		if isVBIdentifierStart(line[i]) {
			wordStart := i
			i = readVBIdentifier(line, i)
			if strings.ToLower(line[wordStart:i]) == lowerKeyword {
				return wordStart, i
			}
			continue
		}
		i++
	}
	return -1, -1
}

func implicitAssignmentDuplicateOfEarlierIncludeGlobal(parsed *core.ParsedDocument, declaration vbUsageDeclaration, includedGlobalNames map[string]struct{}) bool {
	if !declaration.Implicit || len(includedGlobalNames) == 0 {
		return false
	}
	if _, ok := includedGlobalNames[strings.ToLower(declaration.Name)]; !ok {
		return false
	}
	return hasEarlierIncludeDirectiveAt(parsed, declaration.Start)
}

func hasEarlierIncludeDirectiveAt(parsed *core.ParsedDocument, offset int) bool {
	if len(parsed.Includes) == 0 {
		return false
	}
	doc := core.SourceDocument(parsed)
	for _, include := range parsed.Includes {
		if doc.OffsetAt(include.Range.Start) <= offset {
			return true
		}
	}
	return false
}

func implicitAssignmentInlayDeclaration(doc *core.TextDocument, line string, lineOffset int, scope string, inProcedure bool, uncertain bool, declared map[string]struct{}, isBuiltin func(string) bool) (vbUsageDeclaration, bool) {
	cursor := leadingVBWhitespace(line)
	if cursor >= len(line) || line[cursor] == '\'' {
		return vbUsageDeclaration{}, false
	}
	firstStart := cursor
	firstEnd := readVBIdentifier(line, firstStart)
	if firstEnd == firstStart {
		return vbUsageDeclaration{}, false
	}
	targetStart := firstStart
	targetEnd := firstEnd
	first := strings.ToLower(line[firstStart:firstEnd])
	if first == "set" || first == "let" {
		cursor = firstEnd
		for cursor < len(line) && isVBWhitespace(line[cursor]) {
			cursor++
		}
		targetStart = cursor
		targetEnd = readVBIdentifier(line, targetStart)
		if targetEnd == targetStart {
			return vbUsageDeclaration{}, false
		}
	} else if isImplicitAssignmentKeywordName(first) {
		return vbUsageDeclaration{}, false
	}
	name := line[targetStart:targetEnd]
	lowerName := strings.ToLower(name)
	if _, ok := declared[implicitDeclarationScopeKey(false, "", lowerName)]; ok {
		return vbUsageDeclaration{}, false
	}
	if inProcedure {
		if _, ok := declared[implicitDeclarationScopeKey(true, scope, lowerName)]; ok {
			return vbUsageDeclaration{}, false
		}
	}
	if _, legacy := declared[lowerName]; legacy {
		return vbUsageDeclaration{}, false
	}
	if isBuiltin != nil && isBuiltin(lowerName) {
		return vbUsageDeclaration{}, false
	}
	cursor = targetEnd
	for cursor < len(line) && isVBWhitespace(line[cursor]) {
		cursor++
	}
	if cursor < len(line) && line[cursor] == '.' {
		return vbUsageDeclaration{}, false
	}
	if cursor < len(line) && line[cursor] == '(' {
		depth := 0
	targetArguments:
		for cursor < len(line) {
			switch line[cursor] {
			case '"':
				cursor = skipVBString(line, cursor)
				continue
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 {
					cursor++
					for cursor < len(line) && isVBWhitespace(line[cursor]) {
						cursor++
					}
					break targetArguments
				}
			}
			cursor++
		}
		if depth != 0 {
			return vbUsageDeclaration{}, false
		}
	}
	if cursor >= len(line) || line[cursor] != '=' {
		return vbUsageDeclaration{}, false
	}
	value := strings.TrimSpace(line[cursor+1:])
	start := lineOffset + targetStart
	end := lineOffset + targetEnd
	return vbUsageDeclaration{
		Name:          name,
		Kind:          "variable",
		Range:         doc.Range(start, end),
		Start:         start,
		End:           end,
		Line:          doc.PositionAt(start).Line,
		Implicit:      true,
		Uncertain:     uncertain,
		AssignedValue: value,
	}, true
}

func implicitAssignmentInlayKey(declaration vbUsageDeclaration) string {
	return implicitDeclarationScopeKey(declaration.Local, declaration.Scope, declaration.Name)
}

func implicitDeclarationScopeKey(local bool, scope, name string) string {
	scopeKind := "global"
	if local {
		scopeKind = "procedure"
	}
	return scopeKind + ":" + strings.ToLower(scope) + ":" + strings.ToLower(name)
}

func preferredImplicitAssignmentInlayDeclaration(candidate, current vbUsageDeclaration) vbUsageDeclaration {
	if current.Name == "" {
		return candidate
	}
	if candidate.Start < current.Start {
		return candidate
	}
	return current
}

func isImplicitAssignmentKeywordName(name string) bool {
	switch strings.ToLower(name) {
	case "true", "false", "nothing", "empty", "null", "me":
		return true
	default:
		return false
	}
}

func lineTextAtOffset(text string, offset int) string {
	if offset < 0 || offset > len(text) {
		return ""
	}
	start := strings.LastIndexAny(text[:offset], "\r\n")
	if start < 0 {
		start = 0
	} else {
		start++
	}
	end := offset
	for end < len(text) && text[end] != '\r' && text[end] != '\n' {
		end++
	}
	return text[start:end]
}

func firstAssignmentValueAfter(text, name string, offset int) (string, bool) {
	if offset < 0 {
		offset = 0
	}
	if offset > len(text) {
		offset = len(text)
	}
	lowerName := strings.ToLower(name)
	for _, line := range strings.Split(text[offset:], "\n") {
		for _, statement := range splitVBStatements(strings.TrimRight(line, "\r")) {
			value, ok := assignmentValueFromStatement(statement, lowerName)
			if ok {
				return value, true
			}
		}
	}
	return "", false
}

func assignmentValueFromStatement(statement string, lowerName string) (string, bool) {
	trimmed := strings.TrimSpace(statement)
	if trimmed == "" || strings.HasPrefix(trimmed, "'") {
		return "", false
	}
	equalIndex := topLevelEqual(trimmed)
	if equalIndex < 0 {
		return "", false
	}
	target := strings.TrimSpace(trimmed[:equalIndex])
	value := strings.TrimSpace(trimmed[equalIndex+1:])
	if value == "" {
		return "", false
	}
	cursor := 0
	firstEnd := readVBIdentifier(target, cursor)
	if firstEnd == 0 {
		return "", false
	}
	first := strings.ToLower(target[:firstEnd])
	if first == "set" || first == "let" {
		cursor = firstEnd
		for cursor < len(target) && isVBWhitespace(target[cursor]) {
			cursor++
		}
	}
	identifierEnd := readVBIdentifier(target, cursor)
	if identifierEnd == cursor || strings.ToLower(target[cursor:identifierEnd]) != lowerName {
		return "", false
	}
	cursor = identifierEnd
	for cursor < len(target) && isVBWhitespace(target[cursor]) {
		cursor++
	}
	if cursor < len(target) {
		return "", false
	}
	return value, true
}

func splitVBStatements(text string) []string {
	var statements []string
	start := 0
	depth := 0
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '"':
			i = skipVBString(text, i) - 1
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ':':
			if depth == 0 {
				statements = appendTrimmedVBStatement(statements, text, start, i)
				start = i + 1
			}
		}
	}
	return appendTrimmedVBStatement(statements, text, start, len(text))
}

func appendTrimmedVBStatement(statements []string, text string, start int, end int) []string {
	for start < end && isVBWhitespace(text[start]) {
		start++
	}
	for end > start && isVBWhitespace(text[end-1]) {
		end--
	}
	if start < end {
		statements = append(statements, text[start:end])
	}
	return statements
}

func valueAfterAssignment(line string) (string, bool) {
	if index := strings.Index(line, "="); index >= 0 {
		return strings.TrimSpace(line[index+1:]), true
	}
	return "", false
}

var vbNumberLiteralPattern = regexp.MustCompile(`^[+-]?(?:\d+(?:\.\d*)?|\.\d+)$`)

func inferVBValueTypeForDocument(parsed *core.ParsedDocument, value string) string {
	if parsed == nil {
		return inferVBValueType(value)
	}
	trimmed := strings.TrimSpace(value)
	ownerEnd := readVBIdentifier(trimmed, 0)
	if ownerEnd > 0 {
		owner := trimmed[:ownerEnd]
		if _, builtin := vbscriptBuiltinGlobalObjectTypeForName(owner); builtin {
			if _, available := vbscriptBuiltinGlobalObjectType(parsed, owner); !available {
				return ""
			}
		}
	}
	return inferVBValueType(value)
}

func inferVBValueType(value string) string {
	value = strings.TrimSpace(value)
	if typeName := vbscriptBuiltinValueType(value); typeName != "" {
		return typeName
	}
	lower := strings.ToLower(value)
	switch {
	case strings.HasPrefix(lower, "ascb("), strings.HasPrefix(lower, "ascw("),
		strings.HasPrefix(lower, "cbyte("), strings.HasPrefix(lower, "cint("),
		strings.HasPrefix(lower, "clng("), strings.HasPrefix(lower, "datepart("),
		strings.HasPrefix(lower, "getlocale("), strings.HasPrefix(lower, "instrb("),
		strings.HasPrefix(lower, "lenb("), strings.HasPrefix(lower, "msgbox("),
		strings.HasPrefix(lower, "setlocale("), strings.HasPrefix(lower, "vartype("):
		return "Number"
	case strings.HasPrefix(lower, "chrb("), strings.HasPrefix(lower, "chrw("),
		strings.HasPrefix(lower, "cstr("), strings.HasPrefix(lower, "formatcurrency("),
		strings.HasPrefix(lower, "inputbox("), strings.HasPrefix(lower, "join("),
		strings.HasPrefix(lower, "leftb("), strings.HasPrefix(lower, "midb("),
		strings.HasPrefix(lower, "rightb("):
		return "String"
	case strings.HasPrefix(lower, "split("):
		return "Array"
	case strings.HasPrefix(lower, "array("):
		return "Array"
	case strings.HasPrefix(lower, "isobject("):
		return "Boolean"
	case strings.HasPrefix(lower, "getobject("), strings.HasPrefix(lower, "getref("),
		strings.HasPrefix(lower, "loadpicture("):
		return "Object"
	case strings.HasPrefix(lower, "cdec("):
		return "Decimal"
	case strings.HasPrefix(lower, "cvar("):
		return "Variant"
	case strings.HasPrefix(lower, "cverr("):
		return "Error"
	case strings.HasPrefix(value, "#"):
		return "Date"
	case strings.HasPrefix(value, `"`):
		return "String"
	case vbNumberLiteralPattern.MatchString(value):
		return "Number"
	case strings.HasPrefix(lower, "ccur("):
		return "Currency"
	case strings.HasPrefix(lower, "new "):
		rest := strings.TrimSpace(value[len("new "):])
		if end := readVBIdentifier(rest, 0); end > 0 {
			return rest[:end]
		}
		return "Variant"
	case strings.HasPrefix(lower, "createobject("), strings.HasPrefix(lower, "server.createobject("):
		if typeName := quotedVBArgument(value); typeName != "" {
			return typeName
		}
		return "Object"
	case strings.EqualFold(value, "null"):
		return "Null"
	case strings.EqualFold(value, "empty"):
		return "Empty"
	case strings.EqualFold(value, "nothing"):
		return "Nothing"
	case strings.EqualFold(value, "true") || strings.EqualFold(value, "false"):
		return "Boolean"
	case isVBStringBuiltinConstant(value):
		return "String"
	case isVBNumericBuiltinConstant(value):
		return "Number"
	case inferVBBooleanExpression(value):
		return "Boolean"
	case inferVBNumericExpression(value):
		return "Number"
	case strings.Contains(value, "&"):
		return "String"
	default:
		return "Variant"
	}
}

func isVBStringBuiltinConstant(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "vbcrlf", "vbcr", "vblf", "vbnewline", "vbtab", "vbback":
		return true
	default:
		return false
	}
}

func isVBNumericBuiltinConstant(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	if strings.HasPrefix(lower, "ad") {
		return true
	}
	switch lower {
	case "vbokonly", "vbokcancel", "vbyesno", "vbyesnocancel", "vbretrycancel", "vbabortretryignore",
		"vbtextcompare", "vbbinarycompare", "vbdatabasecompare":
		return true
	default:
		return false
	}
}

func inferVBBooleanExpression(value string) bool {
	lower := strings.ToLower(value)
	for _, marker := range []string{" and ", " or ", " xor ", " eqv ", " imp ", "<>", "<=", ">=", "<", ">"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func inferVBNumericExpression(value string) bool {
	hasDigit := false
	hasOperator := false
	for i := 0; i < len(value); i++ {
		switch {
		case value[i] >= '0' && value[i] <= '9':
			hasDigit = true
		case strings.ContainsRune("+-*/\\", rune(value[i])):
			hasOperator = true
		case value[i] == '"':
			return false
		}
	}
	return hasDigit && hasOperator
}

func quotedVBArgument(value string) string {
	start := strings.IndexByte(value, '"')
	if start < 0 {
		return ""
	}
	end := strings.IndexByte(value[start+1:], '"')
	if end < 0 {
		return ""
	}
	return value[start+1 : start+1+end]
}
