package lspserver

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
)

const (
	navigationVBValueUnknown  = navigationValueUnknown
	navigationVBValueLiteral  = navigationValueLiteral
	navigationVBValueTemplate = navigationValueTemplate
)

type navigationVBValue = navigationValue

func cloneNavigationVBValue(value navigationVBValue) navigationVBValue {
	return cloneNavigationValue(value)
}

type navigationVBCandidate struct {
	Kind       string
	Value      navigationVBValue
	Values     []navigationVBValue
	Range      lsp.Range
	ValueRange *lsp.Range
	Snippet    string
}

type navigationVBFunction struct {
	Name       string
	Parameters []navigationVBParameter
	Body       [][]vbscript.Token
	IsFunction bool
}

type navigationVBParameter struct {
	Name     string
	Optional bool
	ByRef    bool
	Default  []vbscript.Token
}

type navigationVBBranchFrame struct {
	base                      map[string]navigationVBValue
	baseGlobals               map[string]navigationVBValue
	baseGlobalWrites          map[string]struct{}
	baseTerminated            map[string]navigationVBValue
	baseLoops                 []navigationVBLoopFrame
	baseEffects               []navigationVBEffectPath
	baseEffectsTruncated      bool
	baseReturning             bool
	baseReturnAssigned        bool
	conditionBase             map[string]navigationVBValue
	conditionGlobals          map[string]navigationVBValue
	conditionWrites           map[string]struct{}
	conditionTerminated       map[string]navigationVBValue
	conditionLoops            []navigationVBLoopFrame
	conditionEffects          []navigationVBEffectPath
	conditionEffectsTruncated bool
	conditionReturning        bool
	conditionAssigned         bool
	order                     int
	branches                  []map[string]navigationVBValue
	globalBranches            []map[string]navigationVBValue
	globalWriteBranches       []map[string]struct{}
	effectBranches            [][]navigationVBEffectPath
	effectBranchTruncated     []bool
	terminated                []bool
	terminalBranches          []map[string]navigationVBValue
	returnAssigned            []bool
	hasElse                   bool
	activeTerminated          bool
	activeExitKind            string
	skippedControls           []string
}

type navigationVBSelectFrame struct {
	base                      map[string]navigationVBValue
	baseGlobals               map[string]navigationVBValue
	baseGlobalWrites          map[string]struct{}
	baseTerminated            map[string]navigationVBValue
	baseLoops                 []navigationVBLoopFrame
	baseEffects               []navigationVBEffectPath
	baseEffectsTruncated      bool
	baseReturning             bool
	baseReturnAssigned        bool
	conditionBase             map[string]navigationVBValue
	conditionGlobals          map[string]navigationVBValue
	conditionWrites           map[string]struct{}
	conditionTerminated       map[string]navigationVBValue
	conditionLoops            []navigationVBLoopFrame
	conditionEffects          []navigationVBEffectPath
	conditionEffectsTruncated bool
	conditionReturning        bool
	conditionAssigned         bool
	order                     int
	branches                  []map[string]navigationVBValue
	globalBranches            []map[string]navigationVBValue
	globalWriteBranches       []map[string]struct{}
	effectBranches            [][]navigationVBEffectPath
	effectBranchTruncated     []bool
	terminated                []bool
	terminalBranches          []map[string]navigationVBValue
	returnAssigned            []bool
	active                    bool
	hasElse                   bool
	activeTerminated          bool
	activeExitKind            string
	skippedControls           []string
}

type navigationVBLoopFrame struct {
	kind                 string
	base                 map[string]navigationVBValue
	baseGlobals          map[string]navigationVBValue
	baseGlobalWrites     map[string]struct{}
	baseEffects          []navigationVBEffectPath
	baseEffectsTruncated bool
	writes               map[string]struct{}
	exited               bool
}

// navigationVBEffect records one externally observable assignment. ByRef
// writes and global writes intentionally retain their assignment mode: the
// same variable name can denote a local variable for a caller while still
// denoting a global variable for a nested procedure.
type navigationVBEffect struct {
	target string
	value  navigationVBValue
	byRef  bool
}

type navigationVBEffectPath struct {
	effects   []navigationVBEffect
	truncated bool
}

// navigationVBExpressionBudget bounds the recursive, token-scanning part of
// VBScript navigation evaluation. It is shared by cloned states so branches
// and nested procedure calls cannot each receive a fresh request budget.
type navigationVBExpressionBudget struct {
	depth     int
	nodes     int
	work      int
	exhausted bool
}

type navigationVBState struct {
	variables            map[string]navigationVBValue
	globals              map[string]navigationVBValue
	localNames           map[string]struct{}
	references           map[string]string
	globalWrites         map[string]struct{}
	functions            map[string]navigationVBFunction
	currentFunction      string
	currentValue         navigationVBValue
	currentParams        []navigationVBParameter
	classDepth           int
	functionClassDepth   int
	returnName           string
	returnAssigned       bool
	branches             []navigationVBBranchFrame
	selects              []navigationVBSelectFrame
	loops                []navigationVBLoopFrame
	nextControlOrder     int
	returning            bool
	returnedUnknown      bool
	localScope           bool
	terminated           map[string]navigationVBValue
	callDepth            int
	callStack            map[string]int
	candidateSink        *[]navigationVBCandidate
	expressionValueSink  *[]navigationVBValue
	callEvidence         navigationVBCallEvidence
	effectPaths          []navigationVBEffectPath
	effectPathsTruncated bool
	baseOffset           int
	sourceText           string
	sourceDocument       *core.TextDocument
	cancelContext        context.Context
	cancelled            bool
	expressionBudget     *navigationVBExpressionBudget
}

type navigationVBCallEvidence struct {
	rangeValue lsp.Range
	snippet    string
}

func extractVBScriptNavigationCandidates(content string, baseOffset int, sourceText string) []navigationVBCandidate {
	state := newNavigationVBState()
	return extractVBScriptNavigationCandidatesWithState(content, baseOffset, sourceText, state)
}

func extractVBScriptNavigationCandidatesWithContext(ctx context.Context, content string, baseOffset int, sourceText string) []navigationVBCandidate {
	state := newNavigationVBState()
	state.cancelContext = ctx
	return extractVBScriptNavigationCandidatesWithState(content, baseOffset, sourceText, state)
}

func extractVBScriptNavigationExpressionWithState(content string, baseOffset int, sourceText string, state *navigationVBState) ([]navigationVBCandidate, []navigationVBValue) {
	if state == nil {
		state = newNavigationVBState()
	}
	values := make([]navigationVBValue, 0, 1)
	previousSink := state.expressionValueSink
	state.expressionValueSink = &values
	candidates := extractVBScriptNavigationCandidatesWithState(content, baseOffset, sourceText, state)
	state.expressionValueSink = previousSink
	return candidates, values
}

func newNavigationVBState() *navigationVBState {
	return &navigationVBState{
		variables: map[string]navigationVBValue{}, globals: map[string]navigationVBValue{}, localNames: map[string]struct{}{}, references: map[string]string{}, globalWrites: map[string]struct{}{}, functions: map[string]navigationVBFunction{}, terminated: map[string]navigationVBValue{}, callStack: map[string]int{}, effectPaths: []navigationVBEffectPath{{}}, expressionBudget: &navigationVBExpressionBudget{},
	}
}

func navigationVBCancelled(state *navigationVBState) bool {
	if state == nil {
		return false
	}
	if state.cancelled {
		return true
	}
	if state.cancelContext != nil && state.cancelContext.Err() != nil {
		state.cancelled = true
		return true
	}
	return false
}

const (
	// The function-call cap remains the semantic recursion guard for user
	// functions. These limits additionally cover parenthesized expressions and
	// token work that does not enter a function body.
	navigationVBExpressionDepthLimit = 128
	navigationVBExpressionNodeLimit  = 16384
	navigationVBExpressionWorkLimit  = 1 << 20
)

func navigationVBExpressionBudgetFor(state *navigationVBState) *navigationVBExpressionBudget {
	if state == nil {
		return nil
	}
	if state.expressionBudget == nil {
		state.expressionBudget = &navigationVBExpressionBudget{}
	}
	return state.expressionBudget
}

func navigationVBExpressionBudgetExhausted(state *navigationVBState) bool {
	budget := navigationVBExpressionBudgetFor(state)
	return budget != nil && budget.exhausted
}

func navigationVBExpressionUnknown() navigationVBValue {
	return navigationVBValue{Kind: navigationVBValueUnknown, Text: "{unknown}"}
}

// navigationVBEnterExpression reserves one recursive evaluation node and one
// stack frame. A failed reservation is a conservative unknown result rather
// than a request cancellation, so other statements can still be analyzed.
func navigationVBEnterExpression(state *navigationVBState) bool {
	if navigationVBCancelled(state) {
		return false
	}
	budget := navigationVBExpressionBudgetFor(state)
	if budget == nil || budget.exhausted || budget.depth >= navigationVBExpressionDepthLimit || budget.nodes >= navigationVBExpressionNodeLimit {
		if budget != nil {
			budget.exhausted = true
		}
		return false
	}
	budget.depth++
	budget.nodes++
	return true
}

func navigationVBLeaveExpression(state *navigationVBState) {
	budget := navigationVBExpressionBudgetFor(state)
	if budget != nil && budget.depth > 0 {
		budget.depth--
	}
}

// navigationVBExpressionWork charges token scans against the shared request
// budget before starting them. This keeps repeated scans of nested token
// slices bounded even when the input is much larger than the depth limit.
func navigationVBExpressionWork(state *navigationVBState, amount int) bool {
	if navigationVBCancelled(state) {
		return false
	}
	if amount <= 0 {
		return true
	}
	budget := navigationVBExpressionBudgetFor(state)
	if budget == nil || budget.exhausted || budget.work > navigationVBExpressionWorkLimit || amount > navigationVBExpressionWorkLimit-budget.work {
		if budget != nil {
			budget.exhausted = true
		}
		return false
	}
	budget.work += amount
	return true
}

func navigationVBExpressionScanCancelled(state *navigationVBState, index int) bool {
	// Checking every 64 tokens keeps cancellation responsive without adding a
	// context lookup to every ordinary token operation.
	return index&63 == 0 && navigationVBCancelled(state)
}

const (
	navigationVBEffectPathLimit  = 64
	navigationVBEffectEventLimit = 256
)

func cloneNavigationVBEffectPaths(paths []navigationVBEffectPath) []navigationVBEffectPath {
	if paths == nil {
		return nil
	}
	clone := make([]navigationVBEffectPath, len(paths))
	for index, path := range paths {
		effects := make([]navigationVBEffect, len(path.effects))
		for effectIndex, effect := range path.effects {
			effects[effectIndex] = effect
			effects[effectIndex].value = cloneNavigationVBValue(effect.value)
		}
		clone[index] = navigationVBEffectPath{
			effects:   effects,
			truncated: path.truncated,
		}
	}
	return clone
}

func navigationVBEffectKey(effect navigationVBEffect) string {
	mode := "global"
	if effect.byRef {
		mode = "byref"
	}
	return mode + "\x00" + effect.target + "\x00" + strconv.Itoa(int(effect.value.Kind)) + "\x00" + effect.value.Text + "\x00" + navigationParameterKey(effect.value.Parameters)
}

func navigationVBEffectPathKey(path navigationVBEffectPath) string {
	parts := make([]string, 0, len(path.effects)+1)
	if path.truncated {
		parts = append(parts, "truncated")
	}
	for _, effect := range path.effects {
		parts = append(parts, navigationVBEffectKey(effect))
	}
	return strings.Join(parts, "\x01")
}

func mergeNavigationVBEffectPaths(sets ...[]navigationVBEffectPath) ([]navigationVBEffectPath, bool) {
	merged := make([]navigationVBEffectPath, 0, navigationVBEffectPathLimit)
	seen := map[string]struct{}{}
	truncated := false
	for _, set := range sets {
		if len(set) == 0 {
			set = []navigationVBEffectPath{{}}
		}
		for _, path := range set {
			if path.truncated {
				truncated = true
			}
			key := navigationVBEffectPathKey(path)
			if _, ok := seen[key]; ok {
				continue
			}
			if len(merged) >= navigationVBEffectPathLimit {
				truncated = true
				continue
			}
			seen[key] = struct{}{}
			merged = append(merged, cloneNavigationVBEffectPaths([]navigationVBEffectPath{path})[0])
		}
	}
	if len(merged) == 0 {
		merged = []navigationVBEffectPath{{truncated: truncated}}
	} else if truncated {
		// Keep the overflow bit attached to the bounded result. A boolean on
		// the producing state is not enough because branch frames and nested
		// calls carry only the path slice forward.
		merged[len(merged)-1].truncated = true
	}
	return merged, truncated
}

type navigationVBEffectPathSet struct {
	paths     []navigationVBEffectPath
	truncated bool
}

func mergeNavigationVBEffectPathSets(sets ...navigationVBEffectPathSet) ([]navigationVBEffectPath, bool) {
	truncated := false
	paths := make([][]navigationVBEffectPath, 0, len(sets))
	for _, set := range sets {
		setPaths := cloneNavigationVBEffectPaths(set.paths)
		if set.truncated {
			truncated = true
			if len(setPaths) > 0 {
				setPaths[len(setPaths)-1].truncated = true
			}
		}
		paths = append(paths, setPaths)
	}
	merged, mergedTruncated := mergeNavigationVBEffectPaths(paths...)
	truncated = truncated || mergedTruncated
	if truncated && len(merged) > 0 {
		merged[len(merged)-1].truncated = true
	}
	return merged, truncated
}

func navigationVBAppendEffect(state *navigationVBState, effect navigationVBEffect) {
	if state == nil || navigationVBCancelled(state) {
		return
	}
	if len(state.effectPaths) == 0 {
		state.effectPaths = []navigationVBEffectPath{{}}
	}
	for index := range state.effectPaths {
		path := &state.effectPaths[index]
		if len(path.effects) >= navigationVBEffectEventLimit {
			path.truncated = true
			state.effectPathsTruncated = true
			continue
		}
		path.effects = append(path.effects, navigationVBEffect{target: effect.target, value: cloneNavigationVBValue(effect.value), byRef: effect.byRef})
	}
}

func navigationVBResolveReference(state *navigationVBState, name string) string {
	name = strings.ToLower(name)
	if state == nil || name == "" {
		return name
	}
	seen := map[string]struct{}{}
	for index := 0; index < navigationVBEffectPathLimit; index++ {
		if _, ok := seen[name]; ok {
			break
		}
		seen[name] = struct{}{}
		reference, ok := state.references[name]
		if !ok || reference == "" {
			break
		}
		name = strings.ToLower(reference)
	}
	return name
}

func navigationVBRecordAssignmentEffect(state *navigationVBState, name string, value navigationVBValue) {
	if state == nil || !state.localScope || name == "" || navigationVBCancelled(state) {
		return
	}
	name = strings.ToLower(name)
	if _, byRef := state.references[name]; byRef {
		navigationVBAppendEffect(state, navigationVBEffect{target: navigationVBResolveReference(state, name), value: value, byRef: true})
		return
	}
	if _, local := state.localNames[name]; local {
		return
	}
	navigationVBAppendEffect(state, navigationVBEffect{target: name, value: value})
}

func extractVBScriptNavigationCandidatesWithState(content string, baseOffset int, sourceText string, state *navigationVBState) []navigationVBCandidate {
	if state == nil {
		state = newNavigationVBState()
	}
	if navigationVBCancelled(state) {
		return nil
	}
	state.baseOffset = baseOffset
	state.sourceText = sourceText
	if state.sourceDocument == nil || state.sourceDocument.Text != sourceText {
		state.sourceDocument = core.NewTextDocument("", "classic-asp", 0, sourceText)
	}
	statements := splitNavigationVBStatements(vbscript.Tokenize(content))
	collectNavigationVBFunctions(statements, state)
	if navigationVBCancelled(state) {
		return nil
	}
	candidates := make([]navigationVBCandidate, 0)
	calledCandidates := make([]navigationVBCandidate, 0)
	previousSink := state.candidateSink
	state.candidateSink = &calledCandidates
	previousEvidence := state.callEvidence
	defer func() {
		state.callEvidence = previousEvidence
		state.candidateSink = previousSink
	}()
	inProcedure := false
	for _, statement := range statements {
		if navigationVBCancelled(state) {
			return nil
		}
		if navigationVBExpressionBudgetExhausted(state) {
			break
		}
		tokens := navigationVBSignificantTokens(statement)
		if len(tokens) == 0 {
			continue
		}
		first, second := navigationVBLower(tokens, 0), navigationVBLower(tokens, 1)
		if first == "class" {
			state.classDepth++
			continue
		}
		if first == "end" && second == "class" {
			if state.classDepth > 0 {
				state.classDepth--
			}
			continue
		}
		if state.classDepth > 0 {
			continue
		}
		procedure, nameIndex, isProcedure := navigationVBProcedureHeader(tokens)
		if isProcedure {
			inProcedure = true
			if procedure == "function" {
				state.currentFunction = strings.ToLower(tokens[nameIndex].Text)
				state.currentParams = navigationVBFunctionParameters(tokens)
			} else {
				state.currentFunction = ""
				state.currentParams = nil
			}
			state.currentValue = navigationVBValue{}
			continue
		}
		if first == "end" && (second == "function" || second == "sub") {
			inProcedure = false
			state.currentFunction = ""
			state.currentValue = navigationVBValue{}
			state.currentParams = nil
			continue
		}
		if inProcedure {
			continue
		}
		reachable := navigationVBStatementReachable(tokens, state)
		if !reachable {
			continue
		}
		evidence := navigationVBStatementEvidenceWithSource(statement, baseOffset, state.sourceDocument)
		state.callEvidence = evidence
		updateNavigationVBState(tokens, state)
		if navigationVBCancelled(state) {
			return nil
		}
		if len(calledCandidates) > 0 {
			candidates = append(candidates, calledCandidates...)
			calledCandidates = calledCandidates[:0]
		}
		candidate, ok := navigationVBSinkCandidate(tokens, state, evidence)
		if ok {
			candidates = append(candidates, candidate)
			continue
		}
		if state.expressionValueSink != nil {
			value := evaluateNavigationVBExpression(tokens, state)
			if navigationVBCancelled(state) {
				return nil
			}
			*state.expressionValueSink = append(*state.expressionValueSink, cloneNavigationVBValue(value))
		}
	}
	if len(calledCandidates) > 0 {
		candidates = append(candidates, calledCandidates...)
	}
	return candidates
}

func navigationVBSinkCandidate(tokens []vbscript.Token, state *navigationVBState, evidence navigationVBCallEvidence) (navigationVBCandidate, bool) {
	path, cursor, ok := navigationVBMemberPath(tokens, navigationVBCallOffset(tokens))
	if !ok {
		return navigationVBCandidate{}, false
	}
	if cursor < len(tokens) && tokens[cursor].Text == "=" {
		return navigationVBCandidate{}, false
	}
	arguments, ok := navigationVBArgumentsWithState(tokens, cursor, state)
	if !ok {
		if navigationVBCancelled(state) {
			return navigationVBCandidate{}, false
		}
		// Keep a sink with an unknown argument when the shared evaluation
		// budget is exhausted. Dropping it would be less conservative than
		// publishing the unknown target.
		arguments = nil
	}
	memberName := strings.Join(path, ".")
	var kind string
	var value navigationVBValue
	argumentIndex := 0
	switch memberName {
	case "response.redirect", "response.redirectpermanent", "server.transfer":
		kind = "redirect"
		value = evaluateNavigationVBExpression(navigationVBArgument(arguments, 0), state)
	case "response.addheader":
		argumentIndex = 1
		header := evaluateNavigationVBExpression(navigationVBArgument(arguments, 0), state)
		if header.Kind != navigationVBValueLiteral || !strings.EqualFold(header.Text, "location") {
			return navigationVBCandidate{}, false
		}
		kind = "redirect"
		value = evaluateNavigationVBExpression(navigationVBArgument(arguments, 1), state)
	case "response.write":
		kind = "write"
		value = evaluateNavigationVBExpression(navigationVBArgument(arguments, 0), state)
	default:
		return navigationVBCandidate{}, false
	}
	value = cloneNavigationVBValue(value)
	var valueRange *lsp.Range
	argumentEvidence := navigationVBStatementEvidenceWithSource(navigationVBArgument(arguments, argumentIndex), state.baseOffset, state.sourceDocument)
	if argumentEvidence.snippet != "" && navigationRangeContains(evidence.rangeValue, argumentEvidence.rangeValue) {
		valueRange = &argumentEvidence.rangeValue
	}
	return navigationVBCandidate{
		Kind: kind, Value: value, Values: value.finiteCandidates(), Range: evidence.rangeValue, ValueRange: valueRange, Snippet: evidence.snippet,
	}, true
}

func navigationVBStateEvidence(tokens []vbscript.Token, state *navigationVBState, fallback navigationVBCallEvidence) navigationVBCallEvidence {
	if state == nil || state.sourceText == "" || len(tokens) == 0 {
		return fallback
	}
	evidence := navigationVBStatementEvidenceWithSource(tokens, state.baseOffset, state.sourceDocument)
	if evidence.snippet == "" {
		return fallback
	}
	return evidence
}

func navigationVBAppendDirectSink(tokens []vbscript.Token, state *navigationVBState) {
	if state == nil || state.candidateSink == nil {
		return
	}
	evidence := navigationVBStateEvidence(tokens, state, state.callEvidence)
	previousEvidence := state.callEvidence
	state.callEvidence = evidence
	defer func() {
		state.callEvidence = previousEvidence
	}()
	candidate, ok := navigationVBSinkCandidate(tokens, state, evidence)
	if !ok {
		return
	}
	*state.candidateSink = append(*state.candidateSink, candidate)
}

func navigationVBStatementEvidence(statement []vbscript.Token, baseOffset int, sourceText string) navigationVBCallEvidence {
	return navigationVBStatementEvidenceWithSource(statement, baseOffset, core.NewTextDocument("", "classic-asp", 0, sourceText))
}

func navigationVBStatementEvidenceWithSource(statement []vbscript.Token, baseOffset int, document *core.TextDocument) navigationVBCallEvidence {
	if document == nil || len(statement) == 0 {
		return navigationVBCallEvidence{}
	}
	start, end := statement[0].Start+baseOffset, statement[len(statement)-1].End+baseOffset
	if start < 0 || end < start || end > len(document.Text) {
		return navigationVBCallEvidence{}
	}
	return navigationVBCallEvidence{
		rangeValue: document.Range(start, end),
		snippet:    strings.TrimSpace(document.Text[start:end]),
	}
}

func navigationVBFunctionDefinitions(parsed *core.ParsedDocument) map[string]navigationVBFunction {
	functions := map[string]navigationVBFunction{}
	if parsed == nil {
		return functions
	}
	state := &navigationVBState{functions: map[string]navigationVBFunction{}}
	for _, region := range parsed.Regions {
		if region.Language != core.LanguageVBScript || region.Kind == core.RegionASPExpression || region.ContentStart < 0 || region.ContentEnd < region.ContentStart || region.ContentEnd > len(parsed.Text) {
			continue
		}
		collectNavigationVBFunctions(splitNavigationVBStatements(vbscript.Tokenize(parsed.Text[region.ContentStart:region.ContentEnd])), state)
	}
	for name, function := range state.functions {
		functions[name] = function
	}
	return functions
}

func splitNavigationVBStatements(tokens []vbscript.Token) [][]vbscript.Token {
	statements := make([][]vbscript.Token, 0)
	current := make([]vbscript.Token, 0)
	depth := 0
	for _, token := range tokens {
		if token.Text == "(" {
			depth++
		}
		if token.Text == ")" && depth > 0 {
			depth--
		}
		lineContinuation := token.Kind == "newline" && navigationVBLastSignificantText(current) == "_" && depth == 0
		if depth == 0 && !lineContinuation && token.Text == ":" && navigationVBInlineIfStatement(current) {
			current = append(current, token)
			continue
		}
		if depth == 0 && !lineContinuation && (token.Kind == "newline" || token.Text == ":") {
			if navigationVBHasSignificantTokens(current) {
				statements = append(statements, current)
			}
			current = nil
			continue
		}
		current = append(current, token)
	}
	if navigationVBHasSignificantTokens(current) {
		statements = append(statements, current)
	}
	return statements
}

func navigationVBHasSignificantTokens(tokens []vbscript.Token) bool {
	for _, token := range tokens {
		if token.Kind != "whitespace" && token.Kind != "comment" && token.Kind != "newline" && token.Text != "_" {
			return true
		}
	}
	return false
}

func navigationVBInlineIfStatement(tokens []vbscript.Token) bool {
	significant := navigationVBSignificantTokens(tokens)
	if navigationVBLower(significant, 0) != "if" {
		return false
	}
	thenIndex := navigationVBTopLevelToken(significant, "then")
	return thenIndex >= 0
}

func navigationVBSignificantTokens(tokens []vbscript.Token) []vbscript.Token {
	result := make([]vbscript.Token, 0, len(tokens))
	for _, token := range tokens {
		if token.Kind != "whitespace" && token.Kind != "comment" && token.Kind != "newline" && token.Text != "_" {
			result = append(result, token)
		}
	}
	return result
}

func navigationVBLastSignificantText(tokens []vbscript.Token) string {
	for index := len(tokens) - 1; index >= 0; index-- {
		if tokens[index].Kind != "whitespace" && tokens[index].Kind != "comment" && tokens[index].Kind != "newline" {
			return tokens[index].Text
		}
	}
	return ""
}

func collectNavigationVBFunctions(statements [][]vbscript.Token, state *navigationVBState) {
	if state == nil {
		return
	}
	for index := 0; index < len(statements); index++ {
		if navigationVBCancelled(state) {
			return
		}
		tokens := navigationVBSignificantTokens(statements[index])
		first, second := navigationVBLower(tokens, 0), navigationVBLower(tokens, 1)
		if first == "class" {
			state.functionClassDepth++
			continue
		}
		if first == "end" && second == "class" {
			if state.functionClassDepth > 0 {
				state.functionClassDepth--
			}
			continue
		}
		if state.functionClassDepth > 0 {
			continue
		}
		procedure, nameIndex, ok := navigationVBProcedureHeader(tokens)
		if !ok {
			continue
		}
		name := strings.ToLower(tokens[nameIndex].Text)
		function := navigationVBFunction{Name: name, Parameters: navigationVBFunctionParameters(tokens), IsFunction: procedure == "function"}
		for index++; index < len(statements); index++ {
			body := navigationVBSignificantTokens(statements[index])
			if len(body) >= 2 && navigationVBLower(body, 0) == "end" && navigationVBLower(body, 1) == procedure {
				break
			}
			function.Body = append(function.Body, body)
		}
		state.functions[name] = function
	}
}

func navigationVBProcedureHeader(tokens []vbscript.Token) (string, int, bool) {
	index := 0
	for index < len(tokens) {
		switch navigationVBLower(tokens, index) {
		case "public", "private", "default":
			index++
		default:
			goto modifiersDone
		}
	}
modifiersDone:
	procedure := navigationVBLower(tokens, index)
	if (procedure != "function" && procedure != "sub") || index+1 >= len(tokens) {
		return "", 0, false
	}
	if tokens[index+1].Kind != "identifier" && tokens[index+1].Kind != "keyword" {
		return "", 0, false
	}
	return procedure, index + 1, true
}

func navigationVBFunctionParameters(tokens []vbscript.Token) []navigationVBParameter {
	_, nameIndex, ok := navigationVBProcedureHeader(tokens)
	if !ok {
		return nil
	}
	_, cursor, ok := navigationVBMemberPath(tokens, nameIndex)
	if !ok || cursor >= len(tokens) || tokens[cursor].Text != "(" {
		return nil
	}
	args := navigationVBArguments(tokens, cursor)
	parameters := make([]navigationVBParameter, 0, len(args))
	for _, argument := range args {
		argument = navigationVBSignificantTokens(argument)
		if len(argument) == 0 {
			continue
		}
		optional := false
		byRef := true
		name := ""
		equal := -1
		depth := 0
		for index, token := range argument {
			switch token.Text {
			case "(":
				depth++
			case ")":
				if depth > 0 {
					depth--
				}
			case "=":
				if depth == 0 && equal < 0 {
					equal = index
				}
			}
			if equal >= 0 {
				break
			}
			if token.Kind == "identifier" || token.Kind == "keyword" {
				lower := strings.ToLower(token.Text)
				switch lower {
				case "optional":
					optional = true
				case "byval":
					byRef = false
				case "byref":
					byRef = true
				case "paramarray":
				default:
					if name == "" {
						name = lower
					}
				}
			}
		}
		if name == "" {
			continue
		}
		parameter := navigationVBParameter{Name: name, Optional: optional, ByRef: byRef}
		if equal >= 0 {
			parameter.Optional = true
			parameter.Default = append([]vbscript.Token(nil), argument[equal+1:]...)
		}
		parameters = append(parameters, parameter)
	}
	return parameters
}

func updateNavigationVBState(tokens []vbscript.Token, state *navigationVBState) {
	if state == nil || navigationVBCancelled(state) {
		return
	}
	if navigationApplyVBControlFlow(tokens, state) {
		navigationVBSyncGlobalVariables(state)
		return
	}
	offset, ok := navigationVBAssignmentOffset(tokens)
	if !ok {
		return
	}
	name := strings.ToLower(tokens[offset].Text)
	value := evaluateNavigationVBExpression(tokens[offset+2:], state)
	if terminal, ok := state.terminated[name]; ok {
		// A procedure exit terminates only the path that reached it. Assignments
		// after a branch/select therefore update live paths while retaining the
		// final value from paths that already returned.
		value = navigationMergeVBValues(terminal, value)
	}
	if name == state.returnName {
		state.returnAssigned = true
	}
	if name == state.currentFunction {
		state.currentValue = cloneNavigationVBValue(value)
		state.variables[name] = cloneNavigationVBValue(value)
	} else if state.localScope && name != state.returnName {
		if _, local := state.localNames[name]; local {
			state.variables[name] = cloneNavigationVBValue(value)
		} else {
			state.variables[name] = cloneNavigationVBValue(value)
			state.globals[name] = cloneNavigationVBValue(value)
			state.globalWrites[name] = struct{}{}
		}
	} else {
		state.variables[name] = cloneNavigationVBValue(value)
		if !state.localScope {
			state.globals[name] = cloneNavigationVBValue(value)
		}
	}
	for index := range state.loops {
		state.loops[index].writes[name] = struct{}{}
	}
	navigationVBRecordAssignmentEffect(state, name, value)
	navigationVBSyncGlobalVariables(state)
}

func navigationVBSyncGlobalVariables(state *navigationVBState) {
	if state == nil {
		return
	}
	if !state.localScope {
		state.globals = cloneNavigationVBVariables(state.variables)
		return
	}
	for name, value := range state.variables {
		if _, local := state.localNames[name]; local {
			continue
		}
		state.globals[name] = cloneNavigationVBValue(value)
	}
}

// evaluateNavigationVBConditionEffects walks a control-flow expression for
// calls whose return value is not otherwise useful to navigation. Calls are
// still evaluated because VBScript evaluates their arguments and bodies in
// source order, and those evaluations can mutate ByRef arguments or globals.
func evaluateNavigationVBConditionEffects(tokens []vbscript.Token, state *navigationVBState) {
	if state == nil || navigationVBCancelled(state) {
		return
	}
	if !navigationVBEnterExpression(state) {
		return
	}
	defer navigationVBLeaveExpression(state)
	tokens = trimNavigationVBTokens(tokens)
	for index := 0; index < len(tokens); {
		if navigationVBCancelled(state) {
			return
		}
		if navigationVBExpressionBudgetExhausted(state) {
			return
		}
		if strings.EqualFold(tokens[index].Text, "call") {
			callTokens := tokens[index:]
			if _, _, ok := navigationVBProcedureCall(callTokens, state); ok {
				navigationExecuteVBProcedureCall(callTokens, state)
				if navigationVBCancelled(state) {
					return
				}
				if end := navigationVBCallEndWithState(callTokens, state); end > 0 {
					index += end
					continue
				}
			}
			index++
			continue
		}
		if tokens[index].Kind == "identifier" || tokens[index].Kind == "keyword" {
			path, cursor, ok := navigationVBMemberPath(tokens, index)
			if ok && cursor < len(tokens) && tokens[cursor].Text == "(" {
				close := navigationVBMatchingParenWithState(tokens, cursor, state)
				if close < 0 {
					close = len(tokens)
				}
				call := tokens[index:close]
				if navigationVBConditionCallable(path, state) {
					_ = evaluateNavigationVBExpression(call, state)
				} else if cursor+1 < close {
					evaluateNavigationVBConditionEffects(tokens[cursor+1:close], state)
				}
				index = close
				continue
			}
			if navigationVBEvaluateBareFunction(tokens, path, index, state) {
				index++
				continue
			}
		}
		if tokens[index].Text == "(" {
			close := navigationVBMatchingParenWithState(tokens, index, state)
			if close > index {
				evaluateNavigationVBConditionEffects(tokens[index+1:close], state)
				index = close + 1
				continue
			}
		}
		index++
	}
}

// navigationVBEvaluateBareFunction evaluates a zero-argument user function
// used in a control expression without parentheses. VBScript also uses bare
// identifiers for variables, so only declared functions that are not shadowed
// by a variable are eligible here.
func navigationVBEvaluateBareFunction(tokens []vbscript.Token, path []string, index int, state *navigationVBState) bool {
	if state == nil || len(path) != 1 || index < 0 || index >= len(tokens) {
		return false
	}
	if index > 0 && tokens[index-1].Text == "." {
		return false
	}
	if index+1 < len(tokens) && tokens[index+1].Text == "." {
		return false
	}
	if index > 0 && !navigationVBBareFunctionOperator(tokens[index-1]) {
		return false
	}
	if index+1 < len(tokens) && !navigationVBBareFunctionOperator(tokens[index+1]) {
		return false
	}
	name := path[0]
	function, ok := state.functions[name]
	if !ok || !function.IsFunction {
		return false
	}
	for _, parameter := range function.Parameters {
		if !parameter.Optional {
			return false
		}
	}
	if _, ok := state.variables[name]; ok {
		return false
	}
	if _, ok := state.globals[name]; ok {
		return false
	}
	_ = evaluateNavigationVBFunction(function, nil, state)
	return true
}

func navigationVBBareFunctionOperator(token vbscript.Token) bool {
	if token.Kind == "identifier" || token.Kind == "string" || token.Kind == "number" || token.Kind == "date" {
		return false
	}
	switch strings.ToLower(token.Text) {
	case "and", "or", "xor", "eqv", "imp", "is", "mod", "not", "then", "else", "case", "to", "step", "in":
		return true
	default:
		return token.Kind == "symbol" || token.Kind == "keyword"
	}
}

func navigationVBConditionCallable(path []string, state *navigationVBState) bool {
	if len(path) == 0 {
		return false
	}
	if len(path) == 1 {
		if function, ok := state.functions[path[0]]; ok && function.IsFunction {
			return true
		}
	}
	switch strings.Join(path, ".") {
	case "server.urlencode", "server.htmlencode", "cstr", "trim", "lcase", "ucase", "iif", "request", "request.querystring", "request.form":
		return true
	default:
		return false
	}
}

func navigationVBCallEndWithState(tokens []vbscript.Token, state *navigationVBState) int {
	if len(tokens) == 0 {
		return 0
	}
	start := navigationVBCallOffset(tokens)
	_, cursor, ok := navigationVBMemberPath(tokens, start)
	if !ok {
		return 0
	}
	if cursor < len(tokens) && tokens[cursor].Text == "(" {
		close := navigationVBMatchingParenWithState(tokens, cursor, state)
		if close >= 0 {
			return close + 1
		}
		if state != nil {
			budget := navigationVBExpressionBudgetFor(state)
			if navigationVBCancelled(state) || (budget != nil && budget.exhausted) {
				return 0
			}
		}
	}
	return len(tokens)
}

func navigationVBEvaluateIfCondition(tokens []vbscript.Token, state *navigationVBState) {
	thenIndex := navigationVBTopLevelToken(tokens, "then")
	if thenIndex > 1 {
		evaluateNavigationVBConditionEffects(tokens[1:thenIndex], state)
	}
}

func navigationVBTopLevelToken(tokens []vbscript.Token, text string) int {
	return navigationVBTopLevelTokenAfter(tokens, text, 0)
}

func navigationVBTopLevelTokenAfter(tokens []vbscript.Token, text string, start int) int {
	if start < 0 {
		start = 0
	}
	depth := 0
	for index := start; index < len(tokens); index++ {
		token := tokens[index]
		switch token.Text {
		case "(":
			depth++
		case ")":
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 && strings.EqualFold(token.Text, text) {
				return index
			}
		}
	}
	return -1
}

func navigationVBSelectCondition(tokens []vbscript.Token, state *navigationVBState) {
	if len(tokens) > 2 {
		evaluateNavigationVBConditionEffects(tokens[2:], state)
	}
}

func navigationVBCaseCondition(tokens []vbscript.Token, state *navigationVBState) {
	if len(tokens) > 1 && navigationVBLower(tokens, 1) != "else" {
		evaluateNavigationVBConditionEffects(tokens[1:], state)
	}
}

func navigationApplyVBControlFlow(tokens []vbscript.Token, state *navigationVBState) bool {
	if state == nil || len(tokens) == 0 || navigationVBCancelled(state) {
		return false
	}
	if navigationVBSkipTerminatedSelect(tokens, state) {
		return true
	}
	if navigationVBSkipTerminatedBranch(tokens, state) {
		return true
	}
	first, second := navigationVBLower(tokens, 0), navigationVBLower(tokens, 1)
	branchBoundary := navigationVBBranchBoundary(tokens, state)
	selectBoundary := navigationVBSelectBoundary(tokens, state)
	if !branchBoundary && !selectBoundary {
		if state.returning {
			return true
		}
		if navigationVBLoopBodySkipped(tokens, state) {
			return true
		}
	}
	if first == "dim" || first == "redim" {
		navigationDeclareVBVariables(tokens, state)
		return true
	}
	if first == "exit" {
		switch second {
		case "function":
			if !state.returnAssigned {
				state.returnedUnknown = true
			}
			state.returning = true
			navigationMarkVBExitPath(state, "function")
			return true
		case "sub":
			state.returning = true
			navigationMarkVBExitPath(state, "sub")
			return true
		case "for":
			navigationMarkVBExitPath(state, "for")
			return navigationExitVBLoop(state, "for")
		case "do":
			navigationMarkVBExitPath(state, "do")
			return navigationExitVBLoop(state, "do")
		case "while":
			navigationMarkVBExitPath(state, "while")
			return navigationExitVBLoop(state, "while")
		default:
			return true
		}
	}
	if navigationExecuteVBProcedureCall(tokens, state) {
		return true
	}
	switch first {
	case "for":
		navigationStartVBLoop(tokens, state, "for")
		return true
	case "while":
		navigationStartVBLoop(tokens, state, "while")
		return true
	case "do":
		navigationStartVBLoop(tokens, state, "do")
		return true
	case "next":
		return navigationFinishVBLoop(state, "for")
	case "loop":
		if second := navigationVBLower(tokens, 1); second == "while" || second == "until" {
			evaluateNavigationVBConditionEffects(tokens[2:], state)
			if navigationVBCancelled(state) {
				return true
			}
		}
		return navigationFinishVBLoop(state, "do")
	case "wend":
		return navigationFinishVBLoop(state, "while")
	case "if":
		navigationVBEvaluateIfCondition(tokens, state)
		if navigationVBCancelled(state) {
			return true
		}
		thenIndex := navigationVBTopLevelToken(tokens, "then")
		if thenIndex >= 0 && thenIndex < len(tokens)-1 {
			thenTokens, elseTokens := tokens[thenIndex+1:], []vbscript.Token(nil)
			if elseIndex := navigationVBInlineElseIndex(tokens, thenIndex); elseIndex >= 0 {
				elseTokens = append([]vbscript.Token{}, tokens[elseIndex+1:]...)
				thenTokens = append([]vbscript.Token(nil), tokens[thenIndex+1:elseIndex]...)
			}
			base := cloneNavigationVBVariables(state.variables)
			baseGlobals := cloneNavigationVBVariables(state.globals)
			baseGlobalWrites := cloneNavigationVBWriteSet(state.globalWrites)
			baseTerminated := cloneNavigationVBVariables(state.terminated)
			baseLoops := cloneNavigationVBLoops(state.loops)
			baseReturning := state.returning
			baseReturnAssigned := state.returnAssigned
			thenState := cloneNavigationVBState(state)
			thenState.variables = cloneNavigationVBVariables(base)
			thenState.globals = cloneNavigationVBVariables(baseGlobals)
			thenState.globalWrites = cloneNavigationVBWriteSet(baseGlobalWrites)
			thenState.terminated = cloneNavigationVBVariables(baseTerminated)
			thenState.loops = cloneNavigationVBLoops(baseLoops)
			thenState.returning = baseReturning
			thenState.returnAssigned = baseReturnAssigned
			navigationVBUpdateInlineBranch(thenTokens, thenState)
			branches := []map[string]navigationVBValue{cloneNavigationVBVariables(thenState.variables)}
			branchStates := []*navigationVBState{thenState}
			if elseTokens != nil {
				elseState := cloneNavigationVBState(state)
				elseState.variables = cloneNavigationVBVariables(base)
				elseState.globals = cloneNavigationVBVariables(baseGlobals)
				elseState.globalWrites = cloneNavigationVBWriteSet(baseGlobalWrites)
				elseState.terminated = cloneNavigationVBVariables(baseTerminated)
				elseState.loops = cloneNavigationVBLoops(baseLoops)
				elseState.returning = baseReturning
				elseState.returnAssigned = baseReturnAssigned
				navigationVBUpdateInlineBranch(elseTokens, elseState)
				branches = append(branches, cloneNavigationVBVariables(elseState.variables))
				branchStates = append(branchStates, elseState)
			} else {
				branches = append(branches, base)
				baseState := cloneNavigationVBState(state)
				baseState.variables = cloneNavigationVBVariables(base)
				baseState.globals = cloneNavigationVBVariables(baseGlobals)
				baseState.globalWrites = cloneNavigationVBWriteSet(baseGlobalWrites)
				baseState.terminated = cloneNavigationVBVariables(baseTerminated)
				baseState.loops = cloneNavigationVBLoops(baseLoops)
				baseState.returning = baseReturning
				baseState.returnAssigned = baseReturnAssigned
				branchStates = append(branchStates, baseState)
			}
			state.variables = mergeNavigationVBBranchVariables(base, branches)
			state.globals = mergeNavigationVBGlobalBranches(baseGlobals, branchStates...)
			state.globalWrites = mergeNavigationVBGlobalWrites(baseGlobalWrites, branchStates...)
			state.terminated = navigationMergeVBTerminatedStates(baseTerminated, branchStates...)
			state.loops = mergeNavigationVBLoopStates(baseLoops, branchStates...)
			sets := make([]navigationVBEffectPathSet, 0, len(branchStates))
			for _, branchState := range branchStates {
				if branchState != nil {
					sets = append(sets, navigationVBEffectPathSet{paths: branchState.effectPaths, truncated: branchState.effectPathsTruncated})
				}
			}
			state.effectPaths, state.effectPathsTruncated = mergeNavigationVBEffectPathSets(sets...)
			state.returning = baseReturning || navigationVBAllStatesReturning(branchStates)
			state.returnAssigned = true
			for _, branchState := range branchStates {
				state.returnedUnknown = state.returnedUnknown || branchState.returnedUnknown
				state.returnAssigned = state.returnAssigned && branchState.returnAssigned
			}
			return true
		}
		if thenIndex >= 0 && thenIndex == len(tokens)-1 {
			order := state.nextControlOrder
			state.nextControlOrder++
			state.branches = append(state.branches, navigationVBBranchFrame{
				base:                      cloneNavigationVBVariables(state.variables),
				baseGlobals:               cloneNavigationVBVariables(state.globals),
				baseGlobalWrites:          cloneNavigationVBWriteSet(state.globalWrites),
				baseTerminated:            cloneNavigationVBVariables(state.terminated),
				baseLoops:                 cloneNavigationVBLoops(state.loops),
				baseEffects:               cloneNavigationVBEffectPaths(state.effectPaths),
				baseEffectsTruncated:      state.effectPathsTruncated,
				baseReturning:             state.returning,
				baseReturnAssigned:        state.returnAssigned,
				conditionBase:             cloneNavigationVBVariables(state.variables),
				conditionGlobals:          cloneNavigationVBVariables(state.globals),
				conditionWrites:           cloneNavigationVBWriteSet(state.globalWrites),
				conditionTerminated:       cloneNavigationVBVariables(state.terminated),
				conditionLoops:            cloneNavigationVBLoops(state.loops),
				conditionEffects:          cloneNavigationVBEffectPaths(state.effectPaths),
				conditionEffectsTruncated: state.effectPathsTruncated,
				conditionReturning:        state.returning,
				conditionAssigned:         state.returnAssigned,
				order:                     order,
			})
			return true
		}
	case "select":
		if second != "case" {
			return false
		}
		navigationVBSelectCondition(tokens, state)
		if navigationVBCancelled(state) {
			return true
		}
		order := state.nextControlOrder
		state.nextControlOrder++
		state.selects = append(state.selects, navigationVBSelectFrame{
			base:                      cloneNavigationVBVariables(state.variables),
			baseGlobals:               cloneNavigationVBVariables(state.globals),
			baseGlobalWrites:          cloneNavigationVBWriteSet(state.globalWrites),
			baseTerminated:            cloneNavigationVBVariables(state.terminated),
			baseLoops:                 cloneNavigationVBLoops(state.loops),
			baseEffects:               cloneNavigationVBEffectPaths(state.effectPaths),
			baseEffectsTruncated:      state.effectPathsTruncated,
			baseReturning:             state.returning,
			baseReturnAssigned:        state.returnAssigned,
			conditionBase:             cloneNavigationVBVariables(state.variables),
			conditionGlobals:          cloneNavigationVBVariables(state.globals),
			conditionWrites:           cloneNavigationVBWriteSet(state.globalWrites),
			conditionTerminated:       cloneNavigationVBVariables(state.terminated),
			conditionLoops:            cloneNavigationVBLoops(state.loops),
			conditionEffects:          cloneNavigationVBEffectPaths(state.effectPaths),
			conditionEffectsTruncated: state.effectPathsTruncated,
			conditionReturning:        state.returning,
			conditionAssigned:         state.returnAssigned,
			order:                     order,
		})
		return true
	case "case":
		if len(state.selects) == 0 {
			return false
		}
		frame := &state.selects[len(state.selects)-1]
		if navigationVBLower(tokens, 1) == "else" {
			frame.hasElse = true
		}
		if frame.active {
			frame.branches = append(frame.branches, cloneNavigationVBVariables(state.variables))
			frame.globalBranches = append(frame.globalBranches, cloneNavigationVBVariables(state.globals))
			frame.globalWriteBranches = append(frame.globalWriteBranches, cloneNavigationVBWriteSet(state.globalWrites))
			frame.effectBranches = append(frame.effectBranches, cloneNavigationVBEffectPaths(state.effectPaths))
			frame.effectBranchTruncated = append(frame.effectBranchTruncated, state.effectPathsTruncated)
			terminated := state.returning || navigationVBProcedurePathTerminated(frame.activeTerminated, frame.activeExitKind)
			frame.terminated = append(frame.terminated, terminated)
			if terminated || len(state.terminated) > 0 {
				frame.terminalBranches = append(frame.terminalBranches, navigationVBBranchTerminalValues(state, terminated))
			}
			frame.returnAssigned = append(frame.returnAssigned, state.returnAssigned)
		}
		state.variables = cloneNavigationVBVariables(frame.conditionBase)
		state.globals = cloneNavigationVBVariables(frame.conditionGlobals)
		state.globalWrites = cloneNavigationVBWriteSet(frame.conditionWrites)
		state.terminated = cloneNavigationVBVariables(frame.conditionTerminated)
		state.loops = cloneNavigationVBLoops(frame.conditionLoops)
		state.effectPaths = cloneNavigationVBEffectPaths(frame.conditionEffects)
		state.effectPathsTruncated = frame.conditionEffectsTruncated
		state.returning = frame.conditionReturning
		state.returnAssigned = frame.conditionAssigned
		navigationVBCaseCondition(tokens, state)
		if navigationVBCancelled(state) {
			return true
		}
		frame.conditionBase = cloneNavigationVBVariables(state.variables)
		frame.conditionGlobals = cloneNavigationVBVariables(state.globals)
		frame.conditionWrites = cloneNavigationVBWriteSet(state.globalWrites)
		frame.conditionTerminated = cloneNavigationVBVariables(state.terminated)
		frame.conditionLoops = cloneNavigationVBLoops(state.loops)
		frame.conditionEffects = cloneNavigationVBEffectPaths(state.effectPaths)
		frame.conditionEffectsTruncated = state.effectPathsTruncated
		frame.conditionReturning = state.returning
		frame.conditionAssigned = state.returnAssigned
		frame.active = true
		frame.activeTerminated = false
		frame.activeExitKind = ""
		frame.skippedControls = nil
		return true
	case "elseif", "else":
		if len(state.branches) == 0 {
			return false
		}
		frame := &state.branches[len(state.branches)-1]
		if first == "else" {
			frame.hasElse = true
		}
		terminated := state.returning || navigationVBProcedurePathTerminated(frame.activeTerminated, frame.activeExitKind)
		navigationAppendVBBranch(frame, state.variables, terminated, state.returnAssigned)
		frame.globalBranches = append(frame.globalBranches, cloneNavigationVBVariables(state.globals))
		frame.globalWriteBranches = append(frame.globalWriteBranches, cloneNavigationVBWriteSet(state.globalWrites))
		frame.effectBranches = append(frame.effectBranches, cloneNavigationVBEffectPaths(state.effectPaths))
		frame.effectBranchTruncated = append(frame.effectBranchTruncated, state.effectPathsTruncated)
		if terminated || len(state.terminated) > 0 {
			frame.terminalBranches = append(frame.terminalBranches, navigationVBBranchTerminalValues(state, terminated))
		}
		state.variables = cloneNavigationVBVariables(frame.conditionBase)
		state.globals = cloneNavigationVBVariables(frame.conditionGlobals)
		state.globalWrites = cloneNavigationVBWriteSet(frame.conditionWrites)
		state.terminated = cloneNavigationVBVariables(frame.conditionTerminated)
		state.loops = cloneNavigationVBLoops(frame.conditionLoops)
		state.effectPaths = cloneNavigationVBEffectPaths(frame.conditionEffects)
		state.effectPathsTruncated = frame.conditionEffectsTruncated
		state.returning = frame.conditionReturning
		state.returnAssigned = frame.conditionAssigned
		if first == "elseif" {
			navigationVBEvaluateIfCondition(tokens, state)
			if navigationVBCancelled(state) {
				return true
			}
			frame.conditionBase = cloneNavigationVBVariables(state.variables)
			frame.conditionGlobals = cloneNavigationVBVariables(state.globals)
			frame.conditionWrites = cloneNavigationVBWriteSet(state.globalWrites)
			frame.conditionTerminated = cloneNavigationVBVariables(state.terminated)
			frame.conditionLoops = cloneNavigationVBLoops(state.loops)
			frame.conditionEffects = cloneNavigationVBEffectPaths(state.effectPaths)
			frame.conditionEffectsTruncated = state.effectPathsTruncated
			frame.conditionReturning = state.returning
			frame.conditionAssigned = state.returnAssigned
		}
		frame.activeTerminated = false
		frame.activeExitKind = ""
		frame.skippedControls = nil
		inlineBodyStart := -1
		if first == "elseif" {
			thenIndex := navigationVBTopLevelToken(tokens, "then")
			if thenIndex >= 0 && thenIndex < len(tokens)-1 {
				inlineBodyStart = thenIndex + 1
			}
		} else if len(tokens) > 1 {
			inlineBodyStart = 1
		}
		if inlineBodyStart >= 0 {
			navigationVBUpdateInlineBranch(tokens[inlineBodyStart:], state)
		}
		return true
	case "end":
		if second == "select" && len(state.selects) > 0 {
			frame := state.selects[len(state.selects)-1]
			if frame.active {
				frame.branches = append(frame.branches, cloneNavigationVBVariables(state.variables))
				frame.globalBranches = append(frame.globalBranches, cloneNavigationVBVariables(state.globals))
				frame.globalWriteBranches = append(frame.globalWriteBranches, cloneNavigationVBWriteSet(state.globalWrites))
				frame.effectBranches = append(frame.effectBranches, cloneNavigationVBEffectPaths(state.effectPaths))
				frame.effectBranchTruncated = append(frame.effectBranchTruncated, state.effectPathsTruncated)
				terminated := state.returning || navigationVBProcedurePathTerminated(frame.activeTerminated, frame.activeExitKind)
				frame.terminated = append(frame.terminated, terminated)
				if terminated || len(state.terminated) > 0 {
					frame.terminalBranches = append(frame.terminalBranches, navigationVBBranchTerminalValues(state, terminated))
				}
				frame.returnAssigned = append(frame.returnAssigned, state.returnAssigned)
			}
			if !frame.hasElse {
				// A Select Case without Case Else has an unselected path. Keep
				// the pre-select values on that path instead of treating the
				// listed cases as exhaustive.
				frame.branches = append(frame.branches, cloneNavigationVBVariables(frame.conditionBase))
				frame.globalBranches = append(frame.globalBranches, cloneNavigationVBVariables(frame.conditionGlobals))
				frame.globalWriteBranches = append(frame.globalWriteBranches, cloneNavigationVBWriteSet(frame.conditionWrites))
				frame.effectBranches = append(frame.effectBranches, cloneNavigationVBEffectPaths(frame.conditionEffects))
				frame.effectBranchTruncated = append(frame.effectBranchTruncated, frame.conditionEffectsTruncated)
				frame.terminated = append(frame.terminated, false)
				frame.returnAssigned = append(frame.returnAssigned, frame.conditionAssigned)
			}
			state.selects = state.selects[:len(state.selects)-1]
			state.variables = mergeNavigationVBBranchVariables(frame.base, frame.branches)
			state.globals = mergeNavigationVBGlobalBranchMaps(frame.baseGlobals, frame.globalBranches)
			state.globalWrites = mergeNavigationVBWriteBranches(frame.baseGlobalWrites, frame.globalWriteBranches)
			sets := make([]navigationVBEffectPathSet, 0, len(frame.effectBranches))
			for index, paths := range frame.effectBranches {
				truncated := index < len(frame.effectBranchTruncated) && frame.effectBranchTruncated[index]
				sets = append(sets, navigationVBEffectPathSet{paths: paths, truncated: truncated})
			}
			state.effectPaths, state.effectPathsTruncated = mergeNavigationVBEffectPathSets(sets...)
			state.terminated = navigationMergeVBTerminatedValues(frame.baseTerminated, frame.terminalBranches...)
			if frame.activeTerminated {
				state.loops = cloneNavigationVBLoops(frame.baseLoops)
			}
			state.returning = frame.baseReturning || navigationVBAllBranchesTerminated(frame.terminated)
			state.returnAssigned = navigationVBAllBranchesReturnAssigned(frame.returnAssigned)
			return true
		}
		if second != "if" || len(state.branches) == 0 {
			return false
		}
		frame := state.branches[len(state.branches)-1]
		terminated := state.returning || navigationVBProcedurePathTerminated(frame.activeTerminated, frame.activeExitKind)
		navigationAppendVBBranch(&frame, state.variables, terminated, state.returnAssigned)
		frame.globalBranches = append(frame.globalBranches, cloneNavigationVBVariables(state.globals))
		frame.globalWriteBranches = append(frame.globalWriteBranches, cloneNavigationVBWriteSet(state.globalWrites))
		frame.effectBranches = append(frame.effectBranches, cloneNavigationVBEffectPaths(state.effectPaths))
		frame.effectBranchTruncated = append(frame.effectBranchTruncated, state.effectPathsTruncated)
		if terminated || len(state.terminated) > 0 {
			frame.terminalBranches = append(frame.terminalBranches, navigationVBBranchTerminalValues(state, terminated))
		}
		if !frame.hasElse {
			// A multiline If without Else has an unselected path. Keep the
			// pre-If values on that path instead of treating Then as exhaustive.
			navigationAppendVBBranch(&frame, frame.conditionBase, false, frame.conditionAssigned)
			frame.globalBranches = append(frame.globalBranches, cloneNavigationVBVariables(frame.conditionGlobals))
			frame.globalWriteBranches = append(frame.globalWriteBranches, cloneNavigationVBWriteSet(frame.conditionWrites))
			frame.effectBranches = append(frame.effectBranches, cloneNavigationVBEffectPaths(frame.conditionEffects))
			frame.effectBranchTruncated = append(frame.effectBranchTruncated, frame.conditionEffectsTruncated)
		}
		state.branches = state.branches[:len(state.branches)-1]
		state.variables = mergeNavigationVBBranchVariables(frame.base, frame.branches)
		state.globals = mergeNavigationVBGlobalBranchMaps(frame.baseGlobals, frame.globalBranches)
		state.globalWrites = mergeNavigationVBWriteBranches(frame.baseGlobalWrites, frame.globalWriteBranches)
		sets := make([]navigationVBEffectPathSet, 0, len(frame.effectBranches))
		for index, paths := range frame.effectBranches {
			truncated := index < len(frame.effectBranchTruncated) && frame.effectBranchTruncated[index]
			sets = append(sets, navigationVBEffectPathSet{paths: paths, truncated: truncated})
		}
		state.effectPaths, state.effectPathsTruncated = mergeNavigationVBEffectPathSets(sets...)
		state.terminated = navigationMergeVBTerminatedValues(frame.baseTerminated, frame.terminalBranches...)
		if frame.activeTerminated {
			state.loops = cloneNavigationVBLoops(frame.baseLoops)
		}
		state.returning = frame.baseReturning || navigationVBAllBranchesTerminated(frame.terminated)
		state.returnAssigned = navigationVBAllBranchesReturnAssigned(frame.returnAssigned)
		return true
	}
	return false
}

func navigationVBInlineElseIndex(tokens []vbscript.Token, thenIndex int) int {
	if thenIndex < 0 || thenIndex >= len(tokens) {
		return -1
	}
	nested := 0
	statementStart := true
	for index := thenIndex + 1; index < len(tokens); index++ {
		token := tokens[index]
		switch {
		case token.Text == ":":
			statementStart = true
			continue
		case statementStart && strings.EqualFold(token.Text, "if"):
			nested++
			statementStart = false
			continue
		case strings.EqualFold(token.Text, "else"):
			if nested > 0 {
				nested--
				statementStart = false
				continue
			}
			return index
		default:
			statementStart = false
		}
	}
	return -1
}

func navigationVBUpdateInlineBranch(tokens []vbscript.Token, state *navigationVBState) {
	if state == nil || navigationVBCancelled(state) {
		return
	}
	for _, statement := range navigationVBSplitInlineBranchStatements(tokens) {
		if navigationVBCancelled(state) {
			return
		}
		statement = navigationVBSignificantTokens(statement)
		if len(statement) == 0 {
			continue
		}
		reachable := navigationVBStatementReachable(statement, state)
		if !reachable {
			continue
		}
		statementEvidence := navigationVBStateEvidence(statement, state, state.callEvidence)
		previousEvidence := state.callEvidence
		state.callEvidence = statementEvidence
		updateNavigationVBState(statement, state)
		state.callEvidence = previousEvidence
		if navigationVBCancelled(state) {
			continue
		}
		navigationVBAppendDirectSink(statement, state)
	}
}

func navigationVBSplitInlineBranchStatements(tokens []vbscript.Token) [][]vbscript.Token {
	statements := make([][]vbscript.Token, 0, 1)
	current := make([]vbscript.Token, 0, len(tokens))
	depth := 0
	for _, token := range tokens {
		switch token.Text {
		case "(":
			depth++
		case ")":
			if depth > 0 {
				depth--
			}
		}
		if depth == 0 && token.Text == ":" {
			if len(navigationVBSignificantTokens(current)) > 0 {
				statements = append(statements, current)
			}
			current = nil
			continue
		}
		current = append(current, token)
	}
	if len(navigationVBSignificantTokens(current)) > 0 {
		statements = append(statements, current)
	}
	return statements
}

func navigationVBProcedureCall(tokens []vbscript.Token, state *navigationVBState) (navigationVBFunction, [][]vbscript.Token, bool) {
	if state == nil || len(tokens) == 0 {
		return navigationVBFunction{}, nil, false
	}
	if _, ok := navigationVBAssignmentOffset(tokens); ok {
		return navigationVBFunction{}, nil, false
	}
	start := navigationVBCallOffset(tokens)
	path, cursor, ok := navigationVBMemberPath(tokens, start)
	if !ok || len(path) != 1 {
		return navigationVBFunction{}, nil, false
	}
	procedure, ok := state.functions[path[0]]
	if !ok || procedure.IsFunction {
		return navigationVBFunction{}, nil, false
	}
	args, ok := navigationVBArgumentsWithState(tokens, cursor, state)
	if !ok {
		return navigationVBFunction{}, nil, false
	}
	return procedure, args, true
}

func navigationMarkVBExitPath(state *navigationVBState, kind string) {
	if state == nil {
		return
	}
	branchOrder, selectOrder := -1, -1
	if len(state.branches) > 0 {
		branchOrder = state.branches[len(state.branches)-1].order
	}
	if len(state.selects) > 0 {
		selectOrder = state.selects[len(state.selects)-1].order
	}
	if branchOrder >= selectOrder && branchOrder >= 0 {
		frame := &state.branches[len(state.branches)-1]
		frame.activeTerminated = true
		frame.activeExitKind = kind
		return
	}
	if selectOrder >= 0 {
		frame := &state.selects[len(state.selects)-1]
		frame.activeTerminated = true
		frame.activeExitKind = kind
	}
}

func navigationExecuteVBProcedureCall(tokens []vbscript.Token, state *navigationVBState) bool {
	procedure, args, ok := navigationVBProcedureCall(tokens, state)
	if !ok {
		return false
	}
	candidates := evaluateNavigationVBProcedureSinks(procedure, args, state, state.callEvidence)
	if state.candidateSink != nil {
		*state.candidateSink = append(*state.candidateSink, candidates...)
	}
	return true
}

func navigationVBBranchBoundary(tokens []vbscript.Token, state *navigationVBState) bool {
	if state == nil || len(state.branches) == 0 {
		return false
	}
	first, second := navigationVBLower(tokens, 0), navigationVBLower(tokens, 1)
	return first == "else" || first == "elseif" || (first == "end" && second == "if")
}

func navigationVBSelectBoundary(tokens []vbscript.Token, state *navigationVBState) bool {
	if state == nil || len(state.selects) == 0 {
		return false
	}
	first, second := navigationVBLower(tokens, 0), navigationVBLower(tokens, 1)
	return first == "case" || (first == "end" && second == "select")
}

func navigationVBSkipTerminatedBranch(tokens []vbscript.Token, state *navigationVBState) bool {
	if state == nil || len(state.branches) == 0 {
		return false
	}
	frame := &state.branches[len(state.branches)-1]
	if !frame.activeTerminated {
		return false
	}
	first, second := navigationVBLower(tokens, 0), navigationVBLower(tokens, 1)
	if len(frame.skippedControls) > 0 {
		top := frame.skippedControls[len(frame.skippedControls)-1]
		if (first == "else" || first == "elseif") && top == "if" {
			return true
		}
		if first == "end" && second == "if" && top == "if" {
			frame.skippedControls = frame.skippedControls[:len(frame.skippedControls)-1]
			return true
		}
		if first == "end" && second == "select" && top == "select" {
			frame.skippedControls = frame.skippedControls[:len(frame.skippedControls)-1]
			return true
		}
		if navigationVBLoopEndKind(tokens) == top {
			frame.skippedControls = frame.skippedControls[:len(frame.skippedControls)-1]
			return true
		}
		return true
	}
	if first == "else" || first == "elseif" || (first == "end" && second == "if") {
		return false
	}
	if !state.returning && !navigationVBLoopBodySkipped(tokens, state) {
		return false
	}
	switch first {
	case "if":
		if navigationVBMultilineIfHeader(tokens) {
			frame.skippedControls = append(frame.skippedControls, "if")
		}
	case "for":
		frame.skippedControls = append(frame.skippedControls, "for")
	case "do":
		frame.skippedControls = append(frame.skippedControls, "do")
	case "while":
		frame.skippedControls = append(frame.skippedControls, "while")
	case "select":
		if second == "case" {
			frame.skippedControls = append(frame.skippedControls, "select")
		}
	}
	return true
}

func navigationVBSkipTerminatedSelect(tokens []vbscript.Token, state *navigationVBState) bool {
	if state == nil || len(state.selects) == 0 {
		return false
	}
	frame := &state.selects[len(state.selects)-1]
	if !frame.activeTerminated {
		return false
	}
	first, second := navigationVBLower(tokens, 0), navigationVBLower(tokens, 1)
	if len(frame.skippedControls) > 0 {
		top := frame.skippedControls[len(frame.skippedControls)-1]
		switch {
		case first == "case" && top == "select":
			return true
		case first == "end" && second == "select" && top == "select":
			frame.skippedControls = frame.skippedControls[:len(frame.skippedControls)-1]
			return true
		case (first == "else" || first == "elseif") && top == "if":
			return true
		case first == "end" && second == "if" && top == "if":
			frame.skippedControls = frame.skippedControls[:len(frame.skippedControls)-1]
			return true
		case navigationVBLoopEndKind(tokens) == top:
			frame.skippedControls = frame.skippedControls[:len(frame.skippedControls)-1]
			return true
		default:
			return true
		}
	}
	if first == "case" || (first == "end" && second == "select") {
		return false
	}
	if !state.returning && !navigationVBLoopBodySkipped(tokens, state) {
		return false
	}
	switch first {
	case "if":
		if navigationVBMultilineIfHeader(tokens) {
			frame.skippedControls = append(frame.skippedControls, "if")
		}
	case "for":
		frame.skippedControls = append(frame.skippedControls, "for")
	case "do":
		frame.skippedControls = append(frame.skippedControls, "do")
	case "while":
		frame.skippedControls = append(frame.skippedControls, "while")
	case "select":
		if second == "case" {
			frame.skippedControls = append(frame.skippedControls, "select")
		}
	}
	return true
}

func navigationVBMultilineIfHeader(tokens []vbscript.Token) bool {
	if navigationVBLower(tokens, 0) != "if" {
		return false
	}
	for index, token := range tokens {
		if strings.EqualFold(token.Text, "then") {
			return index == len(tokens)-1
		}
	}
	return false
}

func navigationAppendVBBranch(frame *navigationVBBranchFrame, variables map[string]navigationVBValue, terminated, returnAssigned bool) {
	if frame == nil {
		return
	}
	frame.branches = append(frame.branches, cloneNavigationVBVariables(variables))
	frame.terminated = append(frame.terminated, terminated)
	frame.returnAssigned = append(frame.returnAssigned, returnAssigned)
}

func navigationVBProcedurePathTerminated(active bool, kind string) bool {
	return active && (kind == "function" || kind == "sub")
}

func navigationVBTerminalValues(state *navigationVBState) map[string]navigationVBValue {
	if state == nil {
		return nil
	}
	values := cloneNavigationVBVariables(state.variables)
	if state.returnName != "" {
		if _, ok := values[state.returnName]; !ok {
			values[state.returnName] = navigationVBValue{Kind: navigationVBValueUnknown, Text: "{unknown}"}
		}
	}
	return values
}

func navigationVBBranchTerminalValues(state *navigationVBState, terminated bool) map[string]navigationVBValue {
	if terminated {
		return navigationVBTerminalValues(state)
	}
	if state == nil {
		return nil
	}
	return cloneNavigationVBVariables(state.terminated)
}

func navigationMergeVBTerminatedValues(base map[string]navigationVBValue, branches ...map[string]navigationVBValue) map[string]navigationVBValue {
	merged := cloneNavigationVBVariables(base)
	for _, branch := range branches {
		for name, value := range branch {
			if previous, ok := merged[name]; ok {
				merged[name] = navigationMergeVBValues(previous, value)
			} else {
				merged[name] = cloneNavigationVBValue(value)
			}
		}
	}
	return merged
}

func navigationMergeVBTerminatedStates(base map[string]navigationVBValue, states ...*navigationVBState) map[string]navigationVBValue {
	merged := cloneNavigationVBVariables(base)
	for _, state := range states {
		if state == nil || !state.returning {
			continue
		}
		merged = navigationMergeVBTerminatedValues(merged, navigationVBTerminalValues(state))
	}
	return merged
}

func navigationVBAllStatesReturning(states []*navigationVBState) bool {
	if len(states) == 0 {
		return false
	}
	for _, state := range states {
		if state == nil || !state.returning {
			return false
		}
	}
	return true
}

func navigationVBAllBranchesTerminated(terminated []bool) bool {
	if len(terminated) == 0 {
		return false
	}
	for _, branch := range terminated {
		if !branch {
			return false
		}
	}
	return true
}

func navigationVBAllBranchesReturnAssigned(branches []bool) bool {
	if len(branches) == 0 {
		return false
	}
	for _, assigned := range branches {
		if !assigned {
			return false
		}
	}
	return true
}

func navigationDeclareVBVariables(tokens []vbscript.Token, state *navigationVBState) {
	for index := 1; index < len(tokens); index++ {
		if tokens[index].Text == "," {
			continue
		}
		name := navigationVBLower(tokens, index)
		if name == "as" {
			for index+1 < len(tokens) && tokens[index+1].Text != "," {
				index++
			}
			continue
		}
		if tokens[index].Kind != "identifier" && tokens[index].Kind != "keyword" {
			continue
		}
		if state.localScope {
			state.localNames[name] = struct{}{}
		}
		state.variables[name] = navigationVBValue{Kind: navigationVBValueUnknown, Text: "{unknown}"}
		if !state.localScope {
			state.globals[name] = state.variables[name]
		}
		for loopIndex := range state.loops {
			state.loops[loopIndex].writes[name] = struct{}{}
		}
		for index+1 < len(tokens) && tokens[index+1].Text != "," {
			index++
		}
	}
}

func navigationVBLoopBodySkipped(tokens []vbscript.Token, state *navigationVBState) bool {
	if len(state.loops) == 0 {
		return false
	}
	frame := state.loops[len(state.loops)-1]
	if !frame.exited {
		return false
	}
	return navigationVBLoopEndKind(tokens) != frame.kind
}

func navigationVBLoopEndKind(tokens []vbscript.Token) string {
	switch navigationVBLower(tokens, 0) {
	case "next":
		return "for"
	case "loop":
		return "do"
	case "wend":
		return "while"
	default:
		return ""
	}
}

func navigationStartVBLoop(tokens []vbscript.Token, state *navigationVBState, kind string) {
	frame := navigationVBLoopFrame{
		kind:                 kind,
		base:                 cloneNavigationVBVariables(state.variables),
		baseGlobals:          cloneNavigationVBVariables(state.globals),
		baseGlobalWrites:     cloneNavigationVBWriteSet(state.globalWrites),
		baseEffects:          cloneNavigationVBEffectPaths(state.effectPaths),
		baseEffectsTruncated: state.effectPathsTruncated,
		writes:               map[string]struct{}{},
	}
	state.loops = append(state.loops, frame)
	if kind == "while" {
		evaluateNavigationVBConditionEffects(tokens[1:], state)
		navigationVBRefreshLoopBase(state)
		return
	}
	if kind == "do" {
		if second := navigationVBLower(tokens, 1); second == "while" || second == "until" {
			evaluateNavigationVBConditionEffects(tokens[2:], state)
			navigationVBRefreshLoopBase(state)
		}
		return
	}
	if len(tokens) < 2 {
		return
	}
	if navigationVBLower(tokens, 1) == "each" {
		inIndex := navigationVBTopLevelTokenAfter(tokens, "in", 3)
		if inIndex >= 0 {
			evaluateNavigationVBExpression(tokens[inIndex+1:], state)
		}
		if len(tokens) > 2 && (tokens[2].Kind == "identifier" || tokens[2].Kind == "keyword") {
			navigationAssignVBLoopVariable(state, strings.ToLower(tokens[2].Text), navigationVBValue{Kind: navigationVBValueUnknown, Text: "{unknown}"})
		}
		return
	}
	if len(tokens) < 4 || (tokens[1].Kind != "identifier" && tokens[1].Kind != "keyword") || tokens[2].Text != "=" {
		return
	}
	toIndex := navigationVBTopLevelTokenAfter(tokens, "to", 3)
	if toIndex < 0 {
		return
	}
	stepIndex := navigationVBTopLevelTokenAfter(tokens, "step", toIndex+1)
	value := evaluateNavigationVBExpression(tokens[3:toIndex], state)
	navigationAssignVBLoopVariable(state, strings.ToLower(tokens[1].Text), value)
	end := len(tokens)
	if stepIndex >= 0 {
		end = stepIndex
	}
	evaluateNavigationVBExpression(tokens[toIndex+1:end], state)
	if stepIndex >= 0 {
		evaluateNavigationVBExpression(tokens[stepIndex+1:], state)
	}
}

func navigationVBRefreshLoopBase(state *navigationVBState) {
	if state == nil || len(state.loops) == 0 {
		return
	}
	frame := &state.loops[len(state.loops)-1]
	frame.base = cloneNavigationVBVariables(state.variables)
	frame.baseGlobals = cloneNavigationVBVariables(state.globals)
	frame.baseGlobalWrites = cloneNavigationVBWriteSet(state.globalWrites)
	frame.baseEffects = cloneNavigationVBEffectPaths(state.effectPaths)
	frame.baseEffectsTruncated = state.effectPathsTruncated
}

func navigationAssignVBLoopVariable(state *navigationVBState, name string, value navigationVBValue) {
	if name == "" {
		return
	}
	state.variables[name] = cloneNavigationVBValue(value)
	if state.localScope {
		if _, local := state.localNames[name]; !local {
			state.globals[name] = cloneNavigationVBValue(value)
			state.globalWrites[name] = struct{}{}
		}
	} else {
		state.globals[name] = cloneNavigationVBValue(value)
	}
	for index := range state.loops {
		state.loops[index].writes[name] = struct{}{}
	}
	navigationVBRecordAssignmentEffect(state, name, value)
}

func navigationExitVBLoop(state *navigationVBState, kind string) bool {
	target := -1
	for index := len(state.loops) - 1; index >= 0; index-- {
		if state.loops[index].kind == kind {
			target = index
			break
		}
	}
	if target < 0 {
		return true
	}
	for index := len(state.loops) - 1; index >= target; index-- {
		frame := &state.loops[index]
		if !frame.exited {
			state.variables = mergeNavigationVBLoopVariables(frame.base, state.variables, frame.writes)
			state.globals = mergeNavigationVBLoopVariables(frame.baseGlobals, state.globals, frame.writes)
			state.globalWrites = navigationMergeVBWriteSets(frame.baseGlobalWrites, state.globalWrites)
			state.effectPaths, state.effectPathsTruncated = mergeNavigationVBEffectPathSets(
				navigationVBEffectPathSet{paths: frame.baseEffects, truncated: frame.baseEffectsTruncated},
				navigationVBEffectPathSet{paths: state.effectPaths, truncated: state.effectPathsTruncated},
			)
			frame.exited = true
		}
	}
	return true
}

func navigationFinishVBLoop(state *navigationVBState, kind string) bool {
	if len(state.loops) == 0 || state.loops[len(state.loops)-1].kind != kind {
		return false
	}
	frame := state.loops[len(state.loops)-1]
	state.loops = state.loops[:len(state.loops)-1]
	if frame.exited {
		return true
	}
	state.variables = mergeNavigationVBLoopVariables(frame.base, state.variables, frame.writes)
	state.globals = mergeNavigationVBLoopVariables(frame.baseGlobals, state.globals, frame.writes)
	state.globalWrites = navigationMergeVBWriteSets(frame.baseGlobalWrites, state.globalWrites)
	state.effectPaths, state.effectPathsTruncated = mergeNavigationVBEffectPathSets(
		navigationVBEffectPathSet{paths: frame.baseEffects, truncated: frame.baseEffectsTruncated},
		navigationVBEffectPathSet{paths: state.effectPaths, truncated: state.effectPathsTruncated},
	)
	return true
}

func mergeNavigationVBLoopVariables(base, current map[string]navigationVBValue, writes map[string]struct{}) map[string]navigationVBValue {
	merged := cloneNavigationVBVariables(base)
	names := map[string]struct{}{}
	for name := range base {
		names[name] = struct{}{}
	}
	for name := range current {
		names[name] = struct{}{}
	}
	for name := range writes {
		names[name] = struct{}{}
	}
	for name := range names {
		values := make([]navigationVBValue, 0, 3)
		if value, ok := base[name]; ok {
			values = append(values, value.finiteCandidates()...)
		} else {
			values = append(values, navigationVBValue{Kind: navigationVBValueUnknown, Text: "{unknown}"})
		}
		if value, ok := current[name]; ok {
			values = append(values, value.finiteCandidates()...)
		} else {
			values = append(values, navigationVBValue{Kind: navigationVBValueUnknown, Text: "{unknown}"})
		}
		if _, written := writes[name]; written {
			values = append(values, navigationVBValue{Kind: navigationVBValueUnknown, Text: "{unknown}"})
		}
		merged[name] = navigationMergeVBValueList(values)
	}
	return merged
}

func cloneNavigationVBVariables(variables map[string]navigationVBValue) map[string]navigationVBValue {
	clone := make(map[string]navigationVBValue, len(variables))
	for name, value := range variables {
		clone[name] = cloneNavigationVBValue(value)
	}
	return clone
}

func cloneNavigationVBWriteSet(writes map[string]struct{}) map[string]struct{} {
	clone := make(map[string]struct{}, len(writes))
	for name := range writes {
		clone[name] = struct{}{}
	}
	return clone
}

func navigationMergeVBWriteSets(base, current map[string]struct{}) map[string]struct{} {
	merged := cloneNavigationVBWriteSet(base)
	for name := range current {
		merged[name] = struct{}{}
	}
	return merged
}

func mergeNavigationVBGlobalBranchMaps(base map[string]navigationVBValue, branches []map[string]navigationVBValue) map[string]navigationVBValue {
	values := make([]map[string]navigationVBValue, 0, len(branches)+1)
	values = append(values, base)
	values = append(values, branches...)
	return mergeNavigationVBBranchVariables(base, values)
}

func mergeNavigationVBWriteBranches(base map[string]struct{}, branches []map[string]struct{}) map[string]struct{} {
	merged := cloneNavigationVBWriteSet(base)
	for _, branch := range branches {
		for name := range branch {
			merged[name] = struct{}{}
		}
	}
	return merged
}

func mergeNavigationVBGlobalBranches(base map[string]navigationVBValue, branches ...*navigationVBState) map[string]navigationVBValue {
	maps := make([]map[string]navigationVBValue, 0, len(branches))
	for _, branch := range branches {
		if branch != nil {
			maps = append(maps, branch.globals)
		}
	}
	return mergeNavigationVBGlobalBranchMaps(base, maps)
}

func mergeNavigationVBGlobalWrites(base map[string]struct{}, branches ...*navigationVBState) map[string]struct{} {
	merged := cloneNavigationVBWriteSet(base)
	for _, branch := range branches {
		if branch == nil {
			continue
		}
		for name := range branch.globalWrites {
			merged[name] = struct{}{}
		}
	}
	return merged
}

func cloneNavigationVBLoops(loops []navigationVBLoopFrame) []navigationVBLoopFrame {
	clone := make([]navigationVBLoopFrame, len(loops))
	for index, frame := range loops {
		clone[index] = navigationVBLoopFrame{
			kind:                 frame.kind,
			base:                 cloneNavigationVBVariables(frame.base),
			baseGlobals:          cloneNavigationVBVariables(frame.baseGlobals),
			baseGlobalWrites:     cloneNavigationVBWriteSet(frame.baseGlobalWrites),
			baseEffects:          cloneNavigationVBEffectPaths(frame.baseEffects),
			baseEffectsTruncated: frame.baseEffectsTruncated,
			writes:               map[string]struct{}{},
			exited:               frame.exited,
		}
		for name := range frame.writes {
			clone[index].writes[name] = struct{}{}
		}
	}
	return clone
}

func mergeNavigationVBLoopStates(base []navigationVBLoopFrame, branches ...*navigationVBState) []navigationVBLoopFrame {
	merged := cloneNavigationVBLoops(base)
	for index := range merged {
		exited := true
		for _, branch := range branches {
			if branch == nil || index >= len(branch.loops) {
				exited = false
				continue
			}
			for name := range branch.loops[index].writes {
				merged[index].writes[name] = struct{}{}
			}
			if !branch.loops[index].exited {
				exited = false
			}
			merged[index].baseEffects, merged[index].baseEffectsTruncated = mergeNavigationVBEffectPathSets(
				navigationVBEffectPathSet{paths: merged[index].baseEffects, truncated: merged[index].baseEffectsTruncated},
				navigationVBEffectPathSet{paths: branch.loops[index].baseEffects, truncated: branch.loops[index].baseEffectsTruncated},
			)
		}
		merged[index].exited = exited
	}
	return merged
}

func mergeNavigationVBBranchVariables(base map[string]navigationVBValue, branches []map[string]navigationVBValue) map[string]navigationVBValue {
	merged := cloneNavigationVBVariables(base)
	if len(branches) == 0 {
		return merged
	}
	names := map[string]struct{}{}
	for _, branch := range branches {
		for name := range branch {
			names[name] = struct{}{}
		}
	}
	for name := range names {
		values := make([]navigationVBValue, 0, len(branches))
		for _, branch := range branches {
			value, ok := branch[name]
			if !ok {
				value, ok = base[name]
				if !ok {
					value = navigationVBValue{Kind: navigationVBValueUnknown, Text: "{unknown}"}
				}
			}
			values = append(values, value.finiteCandidates()...)
		}
		merged[name] = navigationMergeVBValueList(values)
	}
	return merged
}

func navigationVBAssignmentOffset(tokens []vbscript.Token) (int, bool) {
	offset := 0
	first := navigationVBLower(tokens, 0)
	if first == "const" || first == "let" {
		offset = 1
	}
	if offset >= len(tokens) || tokens[offset].Kind != "identifier" || offset+1 >= len(tokens) || tokens[offset+1].Text != "=" {
		return 0, false
	}
	return offset, true
}

func evaluateNavigationVBExpression(tokens []vbscript.Token, state *navigationVBState) navigationVBValue {
	if state == nil || !navigationVBEnterExpression(state) {
		return navigationVBExpressionUnknown()
	}
	defer navigationVBLeaveExpression(state)
	tokens = trimNavigationVBTokens(tokens)
	if len(tokens) == 0 {
		return navigationVBExpressionUnknown()
	}
	parts, ok := splitNavigationVBConcatenationWithState(tokens, state)
	if !ok {
		return navigationVBExpressionUnknown()
	}
	if len(parts) > 1 {
		values := make([]navigationVBValue, 0, len(parts))
		for _, part := range parts {
			if navigationVBCancelled(state) {
				return navigationVBExpressionUnknown()
			}
			if navigationVBExpressionBudgetExhausted(state) {
				return navigationVBExpressionUnknown()
			}
			values = append(values, evaluateNavigationVBExpression(part, state))
		}
		return combineNavigationVBValues(values)
	}
	wrapped, ok := navigationVBWrappedWithState(tokens, state)
	if !ok {
		return navigationVBExpressionUnknown()
	}
	if wrapped {
		return evaluateNavigationVBExpression(tokens[1:len(tokens)-1], state)
	}
	if len(tokens) == 2 && (tokens[0].Text == "+" || tokens[0].Text == "-") && tokens[1].Kind == "number" {
		return navigationVBValue{Kind: navigationVBValueLiteral, Primitive: navigationPrimitiveNumber, Text: tokens[0].Text + tokens[1].Text}
	}
	if len(tokens) == 1 {
		token := tokens[0]
		switch token.Kind {
		case "string":
			return navigationVBValue{Kind: navigationVBValueLiteral, Primitive: navigationPrimitiveString, Text: navigationVBStringValue(token.Text)}
		case "number":
			return navigationVBValue{Kind: navigationVBValueLiteral, Primitive: navigationPrimitiveNumber, Text: token.Text}
		}
		name := strings.ToLower(token.Text)
		if name == "true" || name == "false" {
			return navigationVBValue{Kind: navigationVBValueLiteral, Primitive: navigationPrimitiveBoolean, Text: name}
		}
		if value, ok := state.variables[name]; ok {
			return cloneNavigationVBValue(value)
		}
		if value, ok := state.globals[name]; ok {
			return cloneNavigationVBValue(value)
		}
	}
	path, cursor, ok := navigationVBMemberPath(tokens, 0)
	if !ok {
		return navigationVBExpressionUnknown()
	}
	memberName := strings.Join(path, ".")
	args, ok := navigationVBArgumentsWithState(tokens, cursor, state)
	if !ok {
		return navigationVBExpressionUnknown()
	}
	if len(path) == 1 {
		if function, ok := state.functions[path[0]]; ok && function.IsFunction {
			return evaluateNavigationVBFunction(function, args, state)
		}
	}
	switch memberName {
	case "replace", "left", "right", "mid", "ltrim", "rtrim", "chr", "chrw":
		return evaluateNavigationVBStringFunction(memberName, args, state)
	case "server.urlencode", "server.htmlencode", "cstr", "trim", "lcase", "ucase":
		return evaluateNavigationVBConversion(memberName, navigationVBArgument(args, 0), state)
	case "iif":
		// IIf evaluates all three arguments in VBScript, even though only the
		// selected value is returned. Preserve side effects in the condition
		// before evaluating both bounded result alternatives.
		if navigationVBCancelled(state) {
			return navigationVBValue{Kind: navigationVBValueUnknown, Text: "{unknown}"}
		}
		_ = evaluateNavigationVBExpression(navigationVBArgument(args, 0), state)
		values := make([]navigationVBValue, 0, 2)
		for _, index := range []int{1, 2} {
			if navigationVBCancelled(state) || navigationVBExpressionBudgetExhausted(state) {
				return navigationVBExpressionUnknown()
			}
			values = append(values, evaluateNavigationVBExpression(navigationVBArgument(args, index), state))
		}
		return navigationMergeVBValueList(values)
	case "request", "request.querystring", "request.form":
		source := "request"
		if memberName == "request.querystring" {
			source = "queryString"
		}
		if memberName == "request.form" {
			source = "form"
		}
		name := "value"
		key := navigationVBArgument(args, 0)
		if len(key) > 0 {
			_ = evaluateNavigationVBExpression(key, state)
		}
		if len(key) == 1 && key[0].Kind == "string" {
			name = navigationVBStringValue(key[0].Text)
		}
		return navigationVBValue{Kind: navigationVBValueTemplate, Primitive: navigationPrimitiveString, Text: "{" + source + ":" + name + "}", Parameters: []map[string]any{{"name": name, "source": source, "confidence": "possible"}}}
	}
	return navigationVBExpressionUnknown()
}

func evaluateNavigationVBConversion(name string, tokens []vbscript.Token, state *navigationVBState) navigationVBValue {
	value := evaluateNavigationVBExpression(tokens, state)
	values := value.finiteCandidates()
	converted := make([]navigationVBValue, 0, len(values))
	for _, candidate := range values {
		if navigationVBCancelled(state) {
			return navigationVBValue{Kind: navigationVBValueUnknown, Text: "{unknown}"}
		}
		if candidate.Kind == navigationVBValueUnknown {
			converted = append(converted, candidate)
			continue
		}
		text := navigationVBStringifiedText(candidate)
		candidate.Primitive = navigationPrimitiveString
		switch name {
		case "trim":
			text = strings.TrimSpace(text)
		case "lcase":
			text = strings.ToLower(text)
		case "ucase":
			text = strings.ToUpper(text)
		}
		candidate.Text = text
		converted = append(converted, candidate)
	}
	return navigationMergeVBValueList(converted)
}

func bindNavigationVBParameters(local *navigationVBState, procedure navigationVBFunction, args [][]vbscript.Token, caller *navigationVBState) {
	if local == nil || navigationVBCancelled(local) {
		return
	}
	argumentState := caller
	if argumentState == nil {
		argumentState = local
	}
	for index, parameter := range procedure.Parameters {
		if navigationVBCancelled(local) || navigationVBCancelled(argumentState) {
			return
		}
		if navigationVBExpressionBudgetExhausted(local) || navigationVBExpressionBudgetExhausted(argumentState) {
			return
		}
		local.localNames[parameter.Name] = struct{}{}
		if index < len(args) && len(trimNavigationVBTokens(args[index])) > 0 {
			if parameter.ByRef {
				if reference := navigationVBReferenceName(args[index]); reference != "" {
					local.references[parameter.Name] = reference
				}
			}
			local.variables[parameter.Name] = cloneNavigationVBValue(evaluateNavigationVBExpression(args[index], argumentState))
			continue
		}
		defaultState := local
		if parameter.Default != nil {
			defaultState = cloneNavigationVBState(local)
			// The omitted parameter still shadows an outer same-name variable
			// while its default expression is evaluated.
			defaultState.variables[parameter.Name] = navigationVBValue{Kind: navigationVBValueUnknown, Text: "{unknown}"}
			local.variables[parameter.Name] = cloneNavigationVBValue(evaluateNavigationVBExpression(parameter.Default, defaultState))
		} else {
			local.variables[parameter.Name] = navigationVBValue{Kind: navigationVBValueUnknown, Text: "{unknown}"}
		}
	}
}

func navigationVBReferenceName(tokens []vbscript.Token) string {
	tokens = navigationVBSignificantTokens(tokens)
	if len(tokens) == 1 && (tokens[0].Kind == "identifier" || tokens[0].Kind == "keyword") {
		return strings.ToLower(tokens[0].Text)
	}
	return ""
}

func navigationVBMergeCallEffects(caller, callee *navigationVBState) {
	if caller == nil || callee == nil || navigationVBCancelled(caller) || navigationVBCancelled(callee) {
		return
	}
	paths := make([]navigationVBEffectPath, 0, len(callee.effectPaths))
	for _, path := range callee.effectPaths {
		resolved := navigationVBEffectPath{truncated: path.truncated}
		resolved.effects = make([]navigationVBEffect, 0, len(path.effects))
		for _, effect := range path.effects {
			if navigationVBCancelled(caller) || navigationVBCancelled(callee) {
				return
			}
			if effect.byRef {
				effect.target = navigationVBResolveReference(callee, effect.target)
			}
			effect.value = cloneNavigationVBValue(effect.value)
			resolved.effects = append(resolved.effects, effect)
		}
		paths = append(paths, resolved)
	}
	if len(paths) == 0 {
		paths = []navigationVBEffectPath{{}}
	}
	pathsTruncated := callee.effectPathsTruncated
	for _, path := range paths {
		pathsTruncated = pathsTruncated || path.truncated
	}
	if pathsTruncated {
		// Carry a state-level overflow marker through the nested-call path
		// slice, including the exact-at-cap (64) case.
		paths[len(paths)-1].truncated = true
	}

	// Replay every possible callee path against the caller's pre-call maps.
	// Values are merged only after each path has been replayed, so two writes
	// to the same alias retain their source order instead of being sorted by
	// parameter/global map keys.
	baseVariables := cloneNavigationVBVariables(caller.variables)
	baseGlobals := cloneNavigationVBVariables(caller.globals)
	baseGlobalWrites := cloneNavigationVBWriteSet(caller.globalWrites)
	targets := map[string]struct{}{}
	for _, path := range paths {
		for _, effect := range path.effects {
			targets[effect.target] = struct{}{}
		}
	}
	if len(targets) > 0 {
		variableTouched := map[string]struct{}{}
		globalTouched := map[string]struct{}{}
		for target := range targets {
			for _, path := range paths {
				for _, effect := range path.effects {
					if effect.target != target {
						continue
					}
					if navigationVBEffectAffectsCallerMap(caller, effect, false) {
						variableTouched[target] = struct{}{}
					}
					if navigationVBEffectAffectsCallerMap(caller, effect, true) {
						globalTouched[target] = struct{}{}
					}
				}
			}
		}
		for target := range targets {
			var variableValues, globalValues []navigationVBValue
			for _, path := range paths {
				pathVariables := cloneNavigationVBVariables(baseVariables)
				pathGlobals := cloneNavigationVBVariables(baseGlobals)
				pathGlobalWrites := cloneNavigationVBWriteSet(baseGlobalWrites)
				for _, effect := range path.effects {
					if navigationVBCancelled(caller) || navigationVBCancelled(callee) {
						return
					}
					navigationVBApplyEffectToMaps(caller, pathVariables, pathGlobals, pathGlobalWrites, effect)
				}
				if _, touched := variableTouched[target]; touched {
					if navigationVBPathTouchesMap(caller, path, target, false) {
						variableValues = append(variableValues, navigationVBMapValue(pathVariables, target))
					} else {
						variableValues = append(variableValues, navigationVBMapValue(baseVariables, target))
					}
				}
				if _, touched := globalTouched[target]; touched {
					if navigationVBPathTouchesMap(caller, path, target, true) {
						globalValues = append(globalValues, navigationVBMapValue(pathGlobals, target))
					} else {
						globalValues = append(globalValues, navigationVBMapValue(baseGlobals, target))
					}
				}
				if path.truncated {
					unknown := navigationVBValue{Kind: navigationVBValueUnknown, Text: "{unknown}"}
					if _, touched := variableTouched[target]; touched {
						variableValues = append(variableValues, unknown)
					}
					if _, touched := globalTouched[target]; touched {
						globalValues = append(globalValues, unknown)
					}
				}
			}
			if len(variableValues) > 0 {
				caller.variables[target] = navigationMergeVBValueList(variableValues)
			}
			if len(globalValues) > 0 {
				caller.globals[target] = navigationMergeVBValueList(globalValues)
				caller.globalWrites[target] = struct{}{}
			}
			navigationVBMarkLoopWrite(caller, target)
		}
	}

	// A bounded fallback keeps conservative behavior for any effect that was
	// intentionally capped or came from legacy state without an event record.
	// Only names marked as written are considered, avoiding inherited globals
	// from the caller's initial snapshot.
	globalNames := make([]string, 0, len(callee.globalWrites))
	for name := range callee.globalWrites {
		globalNames = append(globalNames, name)
	}
	sort.Strings(globalNames)
	for _, name := range globalNames {
		if navigationVBCancelled(caller) || navigationVBCancelled(callee) {
			return
		}
		if _, touched := targets[name]; touched {
			continue
		}
		value, ok := callee.globals[name]
		if !ok {
			value, ok = callee.variables[name]
		}
		if ok {
			if pathsTruncated {
				value = navigationMergeVBValues(value, navigationVBValue{Kind: navigationVBValueUnknown, Text: "{unknown}"})
			}
			navigationVBSetCallerGlobal(caller, name, value)
		}
	}
	parameters := make([]string, 0, len(callee.references))
	for parameter := range callee.references {
		parameters = append(parameters, parameter)
	}
	sort.Strings(parameters)
	for _, parameter := range parameters {
		if navigationVBCancelled(caller) || navigationVBCancelled(callee) {
			return
		}
		reference := navigationVBResolveReference(callee, parameter)
		if _, touched := targets[reference]; touched {
			continue
		}
		value, ok := callee.variables[parameter]
		if !ok {
			value, ok = callee.globals[parameter]
		}
		if ok {
			if pathsTruncated {
				value = navigationMergeVBValues(value, navigationVBValue{Kind: navigationVBValueUnknown, Text: "{unknown}"})
			}
			navigationVBSetCallerReference(caller, reference, value)
		}
	}

	// Preserve the ordered suffix for an enclosing procedure call. Paths are
	// combined in source order and capped deterministically.
	callerPaths := caller.effectPaths
	if len(callerPaths) == 0 {
		callerPaths = []navigationVBEffectPath{{}}
	}
	combinedTruncated := caller.effectPathsTruncated || pathsTruncated
	if len(callerPaths) > 0 && len(paths) > navigationVBEffectPathLimit/len(callerPaths) {
		combinedTruncated = true
	}
	combined := make([]navigationVBEffectPath, 0, navigationVBEffectPathLimit)
	for _, callerPath := range callerPaths {
		if navigationVBCancelled(caller) || navigationVBCancelled(callee) {
			return
		}
		for _, calleePath := range paths {
			if navigationVBCancelled(caller) || navigationVBCancelled(callee) {
				return
			}
			path := navigationVBEffectPath{truncated: callerPath.truncated || calleePath.truncated}
			path.effects = make([]navigationVBEffect, 0, len(callerPath.effects)+len(calleePath.effects))
			path.effects = append(path.effects, callerPath.effects...)
			path.effects = append(path.effects, calleePath.effects...)
			if len(path.effects) > navigationVBEffectEventLimit {
				path.effects = path.effects[:navigationVBEffectEventLimit]
				path.truncated = true
			}
			combined = append(combined, path)
			if len(combined) >= navigationVBEffectPathLimit {
				break
			}
		}
		if len(combined) >= navigationVBEffectPathLimit {
			break
		}
	}
	if combinedTruncated && len(combined) > 0 {
		combined[len(combined)-1].truncated = true
	}
	merged, mergedTruncated := mergeNavigationVBEffectPaths(combined)
	caller.effectPaths = merged
	caller.effectPathsTruncated = combinedTruncated || mergedTruncated
}

func navigationVBEffectAffectsCallerMap(caller *navigationVBState, effect navigationVBEffect, globals bool) bool {
	if caller == nil {
		return false
	}
	if caller.localScope {
		if _, local := caller.localNames[effect.target]; local {
			return effect.byRef != globals
		}
	}
	return true
}

func navigationVBPathTouchesMap(caller *navigationVBState, path navigationVBEffectPath, target string, globals bool) bool {
	for _, effect := range path.effects {
		if effect.target == target && navigationVBEffectAffectsCallerMap(caller, effect, globals) {
			return true
		}
	}
	return false
}

func navigationVBApplyEffectToMaps(caller *navigationVBState, variables, globals map[string]navigationVBValue, globalWrites map[string]struct{}, effect navigationVBEffect) {
	if caller == nil || effect.target == "" {
		return
	}
	if effect.byRef {
		variables[effect.target] = cloneNavigationVBValue(effect.value)
		if !caller.localScope {
			globals[effect.target] = cloneNavigationVBValue(effect.value)
			globalWrites[effect.target] = struct{}{}
		} else if _, local := caller.localNames[effect.target]; !local {
			globals[effect.target] = cloneNavigationVBValue(effect.value)
			globalWrites[effect.target] = struct{}{}
		}
		return
	}
	if caller.localScope {
		if _, local := caller.localNames[effect.target]; local {
			globals[effect.target] = cloneNavigationVBValue(effect.value)
			globalWrites[effect.target] = struct{}{}
			return
		}
	}
	variables[effect.target] = cloneNavigationVBValue(effect.value)
	globals[effect.target] = cloneNavigationVBValue(effect.value)
	globalWrites[effect.target] = struct{}{}
}

func navigationVBMapValue(values map[string]navigationVBValue, name string) navigationVBValue {
	if value, ok := values[name]; ok {
		return cloneNavigationVBValue(value)
	}
	return navigationVBValue{Kind: navigationVBValueUnknown, Text: "{unknown}"}
}

func navigationVBSetCallerGlobal(caller *navigationVBState, name string, value navigationVBValue) {
	if caller == nil || name == "" {
		return
	}
	if caller.localScope {
		if _, local := caller.localNames[name]; local {
			caller.globals[name] = cloneNavigationVBValue(value)
			caller.globalWrites[name] = struct{}{}
			navigationVBMarkLoopWrite(caller, name)
			return
		}
	}
	caller.variables[name] = cloneNavigationVBValue(value)
	caller.globals[name] = cloneNavigationVBValue(value)
	caller.globalWrites[name] = struct{}{}
	navigationVBMarkLoopWrite(caller, name)
}

func navigationVBSetCallerReference(caller *navigationVBState, name string, value navigationVBValue) {
	if caller == nil || name == "" {
		return
	}
	caller.variables[name] = cloneNavigationVBValue(value)
	if caller.localScope {
		if _, local := caller.localNames[name]; local {
			navigationVBMarkLoopWrite(caller, name)
			return
		}
	}
	caller.globals[name] = cloneNavigationVBValue(value)
	caller.globalWrites[name] = struct{}{}
	navigationVBMarkLoopWrite(caller, name)
}

func navigationVBMarkLoopWrite(state *navigationVBState, name string) {
	if state == nil || name == "" {
		return
	}
	for index := range state.loops {
		state.loops[index].writes[name] = struct{}{}
	}
}

func evaluateNavigationVBProcedureSinks(procedure navigationVBFunction, args [][]vbscript.Token, state *navigationVBState, evidence navigationVBCallEvidence) []navigationVBCandidate {
	if procedure.Name == "" || procedure.IsFunction || state == nil || navigationVBCancelled(state) {
		return nil
	}
	if state.callDepth >= navigationVBFunctionCallDepthLimit || state.callStack[procedure.Name] > 0 {
		return nil
	}
	if !navigationVBEnterExpression(state) {
		return nil
	}
	defer navigationVBLeaveExpression(state)
	local := cloneNavigationVBState(state)
	local.callDepth++
	local.callStack[procedure.Name]++
	local.variables = map[string]navigationVBValue{}
	local.localNames = map[string]struct{}{}
	local.references = map[string]string{}
	local.globalWrites = map[string]struct{}{}
	local.localScope = true
	local.effectPaths = []navigationVBEffectPath{{}}
	local.effectPathsTruncated = false
	local.currentFunction = ""
	local.returnName = ""
	local.returnAssigned = false
	local.returnedUnknown = false
	local.returning = false
	local.terminated = map[string]navigationVBValue{}
	local.callEvidence = evidence
	sinks := make([]navigationVBCandidate, 0)
	local.candidateSink = &sinks
	bindNavigationVBParameters(local, procedure, args, state)
	if navigationVBCancelled(local) || navigationVBCancelled(state) {
		state.cancelled = true
		return nil
	}
	for _, statement := range procedure.Body {
		if navigationVBCancelled(local) {
			state.cancelled = true
			return nil
		}
		if navigationVBExpressionBudgetExhausted(local) {
			break
		}
		if len(statement) == 0 {
			continue
		}
		reachable := navigationVBStatementReachable(statement, local)
		if !reachable {
			continue
		}
		updateNavigationVBState(statement, local)
		if navigationVBCancelled(local) {
			state.cancelled = true
			return nil
		}
		if candidate, ok := navigationVBSinkCandidate(statement, local, evidence); ok {
			sinks = append(sinks, candidate)
		}
	}
	navigationVBMergeCallEffects(state, local)
	if navigationVBCancelled(state) {
		return nil
	}
	return sinks
}

func evaluateNavigationVBFunction(function navigationVBFunction, args [][]vbscript.Token, state *navigationVBState) navigationVBValue {
	if function.Name == "" || !function.IsFunction || state == nil || navigationVBCancelled(state) {
		return navigationVBExpressionUnknown()
	}
	if state.callDepth >= navigationVBFunctionCallDepthLimit || state.callStack[function.Name] > 0 {
		return navigationVBExpressionUnknown()
	}
	if !navigationVBEnterExpression(state) {
		return navigationVBExpressionUnknown()
	}
	defer navigationVBLeaveExpression(state)
	local := cloneNavigationVBState(state)
	local.callDepth++
	local.callStack[function.Name]++
	local.variables = map[string]navigationVBValue{}
	local.localNames = map[string]struct{}{}
	local.references = map[string]string{}
	local.globalWrites = map[string]struct{}{}
	local.localScope = true
	local.effectPaths = []navigationVBEffectPath{{}}
	local.effectPathsTruncated = false
	// A function return is represented by its function-name assignment. Keep it
	// in the local variable map so control-flow merges retain the final value on
	// each reachable path.
	local.currentFunction = ""
	local.returnName = function.Name
	local.localNames[function.Name] = struct{}{}
	local.returnAssigned = false
	local.returnedUnknown = false
	local.returning = false
	local.terminated = map[string]navigationVBValue{}
	bindNavigationVBParameters(local, function, args, state)
	if navigationVBCancelled(local) || navigationVBCancelled(state) {
		state.cancelled = true
		return navigationVBValue{Kind: navigationVBValueUnknown, Text: "{unknown}"}
	}
	for _, statement := range function.Body {
		if navigationVBCancelled(local) {
			state.cancelled = true
			return navigationVBValue{Kind: navigationVBValueUnknown, Text: "{unknown}"}
		}
		if navigationVBExpressionBudgetExhausted(local) {
			local.returnedUnknown = true
			break
		}
		if len(statement) == 0 {
			continue
		}
		reachable := navigationVBStatementReachable(statement, local)
		if !reachable {
			continue
		}
		updateNavigationVBState(statement, local)
		if navigationVBCancelled(local) {
			state.cancelled = true
			return navigationVBValue{Kind: navigationVBValueUnknown, Text: "{unknown}"}
		}
		if local.candidateSink != nil {
			if candidate, ok := navigationVBSinkCandidate(statement, local, local.callEvidence); ok {
				*local.candidateSink = append(*local.candidateSink, candidate)
			}
		}
	}
	if navigationVBExpressionBudgetExhausted(local) {
		local.returnedUnknown = true
	}
	result, ok := local.variables[function.Name]
	if !ok {
		result = navigationVBValue{Kind: navigationVBValueUnknown, Text: "{unknown}"}
	}
	if local.returnedUnknown {
		result = navigationMergeVBValues(result, navigationVBValue{Kind: navigationVBValueUnknown, Text: "{unknown}"})
	}
	if result.Kind == navigationValueUnknown && result.Text == "" {
		result = navigationVBValue{Kind: navigationVBValueUnknown, Text: "{unknown}"}
	}
	if navigationVBCancelled(local) {
		state.cancelled = true
		return navigationVBValue{Kind: navigationVBValueUnknown, Text: "{unknown}"}
	}
	navigationVBMergeCallEffects(state, local)
	if navigationVBCancelled(state) {
		return navigationVBValue{Kind: navigationVBValueUnknown, Text: "{unknown}"}
	}
	return cloneNavigationVBValue(result)
}

func navigationVBStatementReachable(tokens []vbscript.Token, state *navigationVBState) bool {
	if state == nil {
		return false
	}
	// A terminated path still has to process the boundary that selects an
	// alternate branch or closes the current branch/select. The boundary itself
	// is reachable control flow; statements inside the terminated path are not.
	if navigationVBSkipTerminatedSelect(tokens, state) {
		return false
	}
	if navigationVBSkipTerminatedBranch(tokens, state) {
		return false
	}
	if navigationVBBranchBoundary(tokens, state) || navigationVBSelectBoundary(tokens, state) {
		return true
	}
	return !state.returning && !navigationVBLoopBodySkipped(tokens, state)
}

func cloneNavigationVBState(state *navigationVBState) *navigationVBState {
	clone := newNavigationVBState()
	if state == nil {
		return clone
	}
	clone.variables = cloneNavigationVBVariables(state.variables)
	clone.globals = cloneNavigationVBVariables(state.globals)
	for name := range state.localNames {
		clone.localNames[name] = struct{}{}
	}
	for name, reference := range state.references {
		clone.references[name] = reference
	}
	for name := range state.globalWrites {
		clone.globalWrites[name] = struct{}{}
	}
	for name, function := range state.functions {
		clone.functions[name] = function
	}
	clone.currentFunction = state.currentFunction
	clone.currentValue = cloneNavigationVBValue(state.currentValue)
	clone.currentParams = append([]navigationVBParameter(nil), state.currentParams...)
	clone.classDepth = state.classDepth
	clone.functionClassDepth = state.functionClassDepth
	clone.returnName = state.returnName
	clone.returnAssigned = state.returnAssigned
	clone.returning = state.returning
	clone.returnedUnknown = state.returnedUnknown
	clone.localScope = state.localScope
	clone.nextControlOrder = state.nextControlOrder
	clone.terminated = cloneNavigationVBVariables(state.terminated)
	clone.callDepth = state.callDepth
	for name, count := range state.callStack {
		clone.callStack[name] = count
	}
	clone.candidateSink = state.candidateSink
	clone.callEvidence = state.callEvidence
	clone.effectPaths = cloneNavigationVBEffectPaths(state.effectPaths)
	clone.effectPathsTruncated = state.effectPathsTruncated
	clone.baseOffset = state.baseOffset
	clone.sourceText = state.sourceText
	clone.sourceDocument = state.sourceDocument
	clone.cancelContext = state.cancelContext
	clone.cancelled = state.cancelled
	clone.expressionBudget = state.expressionBudget
	return clone
}

func splitNavigationVBConcatenationWithState(tokens []vbscript.Token, state *navigationVBState) ([][]vbscript.Token, bool) {
	if !navigationVBExpressionWork(state, len(tokens)) {
		return nil, false
	}
	parts := make([][]vbscript.Token, 0)
	current := make([]vbscript.Token, 0)
	depth := 0
	for index, token := range tokens {
		if navigationVBExpressionScanCancelled(state, index) {
			return nil, false
		}
		if token.Text == "(" {
			depth++
		}
		if token.Text == ")" && depth > 0 {
			depth--
		}
		if depth == 0 && (token.Text == "&" || token.Text == "+" && len(current) > 0) {
			parts = append(parts, current)
			current = nil
			continue
		}
		current = append(current, token)
	}
	parts = append(parts, current)
	return parts, true
}

func combineNavigationVBValues(values []navigationVBValue) navigationVBValue {
	if len(values) == 0 {
		return navigationVBValue{Kind: navigationVBValueUnknown, Text: "{unknown}"}
	}
	choices := [][]navigationVBValue{nil}
	truncated := false
	var unknownParameters []map[string]any
	for _, value := range values {
		candidates := value.finiteCandidates()
		if len(candidates) == 0 {
			candidates = []navigationVBValue{value}
		}
		filtered := make([]navigationVBValue, 0, len(candidates))
		for _, candidate := range candidates {
			if candidate.Kind == navigationVBValueUnknown {
				// Unknown alternatives cannot form a complete concatenation, so
				// propagate the bounded fallback instead of rendering their marker.
				truncated = true
				unknownParameters = mergeNavigationParameterMaps(unknownParameters, candidate.Parameters)
				continue
			}
			filtered = append(filtered, candidate)
		}
		candidates = filtered
		if len(candidates) == 0 {
			choices = nil
			continue
		}
		if len(candidates) > navigationVBFiniteValueLimit {
			candidates = candidates[:navigationVBFiniteValueLimit]
			truncated = true
		}
		capacity := len(choices) * len(candidates)
		if capacity > navigationVBFiniteValueLimit {
			truncated = true
			capacity = navigationVBFiniteValueLimit
		}
		next := make([][]navigationVBValue, 0, capacity)
		for _, prefix := range choices {
			for _, candidate := range candidates {
				if len(next) >= capacity {
					break
				}
				combined := append(append([]navigationVBValue(nil), prefix...), candidate)
				next = append(next, combined)
			}
			if len(next) >= capacity {
				break
			}
		}
		choices = next
	}
	combined := make([]navigationVBValue, 0, len(choices))
	for _, choice := range choices {
		result := navigationVBValue{Kind: navigationVBValueLiteral, Primitive: navigationPrimitiveString}
		for _, value := range choice {
			result.Text += navigationVBStringifiedText(value)
			result.Parameters = mergeNavigationParameterMaps(result.Parameters, value.Parameters)
			if value.Kind != navigationVBValueLiteral {
				result.Kind = navigationVBValueTemplate
			}
		}
		for _, parameter := range result.Parameters {
			parameter["targetUsage"] = result.Text
		}
		combined = append(combined, cloneNavigationVBValue(result))
	}
	if truncated {
		if len(combined) >= navigationVBFiniteValueLimit {
			combined = combined[:navigationVBFiniteValueLimit-1]
		}
		combined = append(combined, navigationVBValue{Kind: navigationVBValueUnknown, Text: "{unknown}", Parameters: cloneNavigationParameterMaps(unknownParameters)})
	}
	return navigationMergeVBValueList(combined)
}

func navigationVBStringifiedText(value navigationVBValue) string {
	if value.Primitive == navigationPrimitiveBoolean {
		if strings.EqualFold(strings.TrimSpace(value.Text), "true") {
			return "True"
		}
		return "False"
	}
	if value.Primitive == navigationPrimitiveNumber {
		if literal, ok := navigationJavaScriptNumberLiteral(value.Text); ok {
			if parsed, ok := parseVBScriptNumericLiteral(literal); ok {
				identity := parsed.identity()
				if identity != "" && !strings.HasPrefix(identity, "symbolic:") && !strings.Contains(identity, "/") {
					return identity
				}
			}
		}
	}
	return value.Text
}

func navigationMergeVBValues(left, right navigationVBValue) navigationVBValue {
	if left.Text == "" && left.Kind == navigationVBValueUnknown && len(left.Alternatives) == 0 {
		return cloneNavigationVBValue(right)
	}
	values := append(left.finiteCandidates(), right.finiteCandidates()...)
	return navigationMergeVBValueList(values)
}

func navigationMergeVBValueList(values []navigationVBValue) navigationVBValue {
	if len(values) == 0 {
		return navigationVBValue{Kind: navigationVBValueUnknown, Text: "{unknown}"}
	}
	unique := make([]navigationVBValue, 0, minNavigationVBValueCapacity(len(values)))
	seen := map[string]struct{}{}
	truncated := false
	for _, value := range values {
		if len(unique) >= navigationVBFiniteValueLimit {
			truncated = true
			break
		}
		value = cloneNavigationVBValue(value)
		value.Alternatives = nil
		key := strconv.Itoa(int(value.Kind)) + "\x00" + strconv.Itoa(int(value.Primitive)) + "\x00" + value.Text + "\x00" + navigationParameterKey(value.Parameters)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, value)
	}
	if truncated {
		unknown := navigationVBValue{Kind: navigationVBValueUnknown, Text: "{unknown}"}
		unknownKey := strconv.Itoa(int(unknown.Kind)) + "\x00" + strconv.Itoa(int(unknown.Primitive)) + "\x00" + unknown.Text + "\x00"
		if _, ok := seen[unknownKey]; !ok {
			if len(unique) >= navigationVBFiniteValueLimit {
				unique = unique[:navigationVBFiniteValueLimit-1]
			}
			unique = append(unique, unknown)
		}
	}
	if len(unique) == 1 {
		return unique[0]
	}
	result := unique[0]
	result.Alternatives = unique
	return result
}

const navigationVBFiniteValueLimit = 64

const navigationVBFunctionCallDepthLimit = 32

func minNavigationVBValueCapacity(length int) int {
	if length < navigationVBFiniteValueLimit {
		return length
	}
	return navigationVBFiniteValueLimit
}

func navigationVBMemberPath(tokens []vbscript.Token, start int) ([]string, int, bool) {
	if start >= len(tokens) || (tokens[start].Kind != "identifier" && tokens[start].Kind != "keyword") {
		return nil, start, false
	}
	path := []string{strings.ToLower(tokens[start].Text)}
	cursor := start + 1
	for cursor+1 < len(tokens) && tokens[cursor].Text == "." && (tokens[cursor+1].Kind == "identifier" || tokens[cursor+1].Kind == "keyword") {
		path = append(path, strings.ToLower(tokens[cursor+1].Text))
		cursor += 2
	}
	return path, cursor, true
}

func navigationVBArguments(tokens []vbscript.Token, start int) [][]vbscript.Token {
	args, _ := navigationVBArgumentsWithState(tokens, start, nil)
	return args
}

func navigationVBArgumentsWithState(tokens []vbscript.Token, start int, state *navigationVBState) ([][]vbscript.Token, bool) {
	if start >= len(tokens) {
		return nil, true
	}
	if tokens[start].Text == "(" {
		close := navigationVBMatchingParenWithState(tokens, start, state)
		if close < 0 {
			if state != nil {
				budget := navigationVBExpressionBudgetFor(state)
				if navigationVBCancelled(state) || (budget != nil && budget.exhausted) {
					return nil, false
				}
			}
			close = len(tokens)
		}
		return splitNavigationVBArgumentsWithState(tokens[start+1:close], state)
	}
	return splitNavigationVBArgumentsWithState(tokens[start:], state)
}

func splitNavigationVBArguments(tokens []vbscript.Token) [][]vbscript.Token {
	args := make([][]vbscript.Token, 0)
	current := make([]vbscript.Token, 0)
	depth := 0
	for _, token := range tokens {
		if token.Text == "(" {
			depth++
		}
		if token.Text == ")" && depth > 0 {
			depth--
		}
		if depth == 0 && token.Text == "," {
			args = append(args, trimNavigationVBTokens(current))
			current = nil
			continue
		}
		current = append(current, token)
	}
	if len(current) > 0 || len(tokens) > 0 {
		args = append(args, trimNavigationVBTokens(current))
	}
	return args
}

func splitNavigationVBArgumentsWithState(tokens []vbscript.Token, state *navigationVBState) ([][]vbscript.Token, bool) {
	if state == nil {
		return splitNavigationVBArguments(tokens), true
	}
	if !navigationVBExpressionWork(state, len(tokens)) {
		return nil, false
	}
	args := make([][]vbscript.Token, 0)
	current := make([]vbscript.Token, 0)
	depth := 0
	for index, token := range tokens {
		if navigationVBExpressionScanCancelled(state, index) {
			return nil, false
		}
		if token.Text == "(" {
			depth++
		}
		if token.Text == ")" && depth > 0 {
			depth--
		}
		if depth == 0 && token.Text == "," {
			args = append(args, trimNavigationVBTokens(current))
			current = nil
			continue
		}
		current = append(current, token)
	}
	if len(current) > 0 || len(tokens) > 0 {
		args = append(args, trimNavigationVBTokens(current))
	}
	return args, true
}

func navigationVBMatchingParen(tokens []vbscript.Token, open int) int {
	depth := 0
	for index := open; index < len(tokens); index++ {
		if tokens[index].Text == "(" {
			depth++
		}
		if tokens[index].Text == ")" {
			depth--
			if depth == 0 {
				return index
			}
		}
	}
	return -1
}

func navigationVBMatchingParenWithState(tokens []vbscript.Token, open int, state *navigationVBState) int {
	if state == nil {
		return navigationVBMatchingParen(tokens, open)
	}
	if open < 0 || open >= len(tokens) || !navigationVBExpressionWork(state, len(tokens)-open) {
		return -1
	}
	depth := 0
	for index := open; index < len(tokens); index++ {
		if navigationVBExpressionScanCancelled(state, index-open) {
			return -1
		}
		if tokens[index].Text == "(" {
			depth++
		}
		if tokens[index].Text == ")" {
			depth--
			if depth == 0 {
				return index
			}
		}
	}
	return -1
}

func navigationVBWrappedWithState(tokens []vbscript.Token, state *navigationVBState) (bool, bool) {
	if len(tokens) < 2 || tokens[0].Text != "(" || tokens[len(tokens)-1].Text != ")" {
		return false, true
	}
	close := navigationVBMatchingParenWithState(tokens, 0, state)
	if close < 0 {
		budget := navigationVBExpressionBudgetFor(state)
		if navigationVBCancelled(state) || (budget != nil && budget.exhausted) {
			return false, false
		}
		return false, true
	}
	return close == len(tokens)-1, true
}

func trimNavigationVBTokens(tokens []vbscript.Token) []vbscript.Token {
	for len(tokens) > 0 && tokens[0].Text == "_" {
		tokens = tokens[1:]
	}
	for len(tokens) > 0 && tokens[len(tokens)-1].Text == "_" {
		tokens = tokens[:len(tokens)-1]
	}
	return tokens
}

func navigationVBArgument(args [][]vbscript.Token, index int) []vbscript.Token {
	if index < 0 || index >= len(args) {
		return nil
	}
	return args[index]
}

func navigationVBCallOffset(tokens []vbscript.Token) int {
	if navigationVBLower(tokens, 0) == "call" {
		return 1
	}
	return 0
}

func navigationVBLower(tokens []vbscript.Token, index int) string {
	if index < 0 || index >= len(tokens) {
		return ""
	}
	return strings.ToLower(tokens[index].Text)
}

func navigationVBStringValue(text string) string {
	if len(text) >= 2 && text[0] == '"' && text[len(text)-1] == '"' {
		text = text[1 : len(text)-1]
	}
	return strings.ReplaceAll(text, `""`, `"`)
}
