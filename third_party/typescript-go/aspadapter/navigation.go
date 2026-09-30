// Package aspadapter provides the Classic ASP adapter layer for TypeScript-Go.
package aspadapter

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/core"
	"github.com/microsoft/typescript-go/internal/evaluator"
	"github.com/microsoft/typescript-go/internal/parser"
	"github.com/microsoft/typescript-go/internal/scanner"
)

// NavigationValueKind describes how a navigation value was derived.
type NavigationValueKind string

const (
	NavigationValueLiteral  NavigationValueKind = "literal"
	NavigationValueTemplate NavigationValueKind = "template"
	NavigationValueUnknown  NavigationValueKind = "unknown"
)

// NavigationValueLimit is the maximum number of candidates returned for one
// expression, including an unknown fallback when the analysis is truncated.
const NavigationValueLimit = 32

const navigationValueLimit = NavigationValueLimit

// navigationCallDepthLimit bounds direct function evaluation. Calls beyond
// this depth are treated as unknown instead of attempting to execute code.
const navigationCallDepthLimit = 8

// NavigationEvaluationWorkLimit bounds the amount of finite evaluation work
// performed for one navigation request. The budget is shared by all control
// flow paths and recursive calls, so path multiplication cannot grow without
// limit. Exhaustion keeps navigation conservative by reporting unknown values.
const NavigationEvaluationWorkLimit uint64 = 200_000

const navigationEvaluationWorkCheckInterval uint64 = 64

// navigationSourceByteLimit bounds parser and UTF-16 preprocessing memory.
// SourceFileParseOptions has no cancellation hook, so accepting arbitrarily
// large virtual documents would make a cancelled request wait for an
// uncancellable parser invocation and an unbounded offset map allocation.
const navigationSourceByteLimit = 8 << 20

const navigationPreprocessingCheckInterval = 64 << 10

// The structural fallback runs only after finite evaluation is exhausted. It
// is deliberately bounded independently of the evaluator budget so a large
// or malformed AST cannot turn conservative reporting into an unbounded walk.
const navigationUnknownFallbackNodeLimit uint64 = NavigationEvaluationWorkLimit
const navigationUnknownFallbackSinkLimit = 16_384

// navigationSinkCountLimit bounds both the exact primary results and the
// conservative results appended by the structural fallback. The primary
// traversal cannot append more than its shared work budget, while the
// fallback has its own sink cap.
const navigationSinkCountLimit = NavigationEvaluationWorkLimit + navigationUnknownFallbackSinkLimit

// navigationOrderingWorkLimit accounts for the bounded merge sort's element
// copies and comparisons. It is deliberately larger than the worst-case
// work for navigationSinkCountLimit elements, but remains a fixed bound.
const navigationOrderingWorkLimit = navigationSinkCountLimit * 64

var errNavigationSinkCountLimit = errors.New("javascript navigation sink count limit exceeded")
var errNavigationSourceByteLimit = errors.New("javascript navigation source exceeds preprocessing limit")

// navigationExpressionPathLimit prevents a deeply chained member expression
// from bypassing the fallback's cancellation and traversal bounds.
const navigationExpressionPathLimit = 1_024

// SourceRange contains offsets into the input source. Start and End are UTF-16
// offsets, which is the offset encoding used by LSP clients. ByteStart and
// ByteEnd are provided for callers that need to slice the original Go string.
type SourceRange struct {
	Start     int
	End       int
	ByteStart int
	ByteEnd   int
}

// NavigationValue is one finite candidate produced for a navigation
// expression. Unknown candidates use the literal text "{unknown}".
type NavigationValue struct {
	Text         string
	Kind         NavigationValueKind
	Dynamic      bool
	Dependencies []string
}

// NavigationExpression contains the source range and finite candidates for a
// sink argument. Values are ordered by source/branch order and are deduped.
type NavigationExpression struct {
	Text   string
	Range  SourceRange
	Values []NavigationValue
}

// NavigationSink describes a JavaScript navigation operation found in source.
// Range covers the containing expression statement when one exists; the
// expression range covers only the value being evaluated.
type NavigationSink struct {
	Kind        string
	Range       SourceRange
	Expression  NavigationExpression
	TargetFrame string
	FormName    string
	Method      string
	Snippet     string
	// occurrence retains the exact sink AST range for fallback identity while
	// Range continues to describe the containing source statement.
	occurrence SourceRange
	called     bool
}

// NavigationAnalysis is the result of analyzing one virtual JavaScript text.
type NavigationAnalysis struct {
	Sinks     []NavigationSink
	Cancelled bool
}

// JavaScriptNavigationAnalysis is a descriptive alias retained for callers
// that prefer the language-qualified name.
type JavaScriptNavigationAnalysis = NavigationAnalysis

// JavaScriptNavigationSink is a descriptive alias for NavigationSink.
type JavaScriptNavigationSink = NavigationSink

// JavaScriptNavigationExpression is a descriptive alias for
// NavigationExpression.
type JavaScriptNavigationExpression = NavigationExpression

// JavaScriptNavigationValue is a descriptive alias for NavigationValue.
type JavaScriptNavigationValue = NavigationValue

type navigationCandidate struct {
	text    string
	kind    NavigationValueKind
	truth   navigationTruthiness
	nullish navigationNullish
}

type navigationTruthiness uint8

const (
	navigationTruthinessUnknown navigationTruthiness = iota
	navigationTruthinessFalse
	navigationTruthinessTrue
)

type navigationNullish uint8

const (
	navigationNullishUnknown navigationNullish = iota
	navigationNullishNo
	navigationNullishYes
)

type navigationFiniteSet struct {
	values       []navigationCandidate
	unknown      bool
	dependencies []string
}

func unknownNavigationCandidate() navigationCandidate {
	return navigationCandidate{text: "{unknown}", kind: NavigationValueUnknown}
}

func unknownNavigationSet() navigationFiniteSet {
	return navigationFiniteSet{
		values:  []navigationCandidate{{text: "{unknown}", kind: NavigationValueUnknown}},
		unknown: true,
	}
}

func literalNavigationSet(text string) navigationFiniteSet {
	truth := navigationTruthinessTrue
	if text == "" {
		truth = navigationTruthinessFalse
	}
	return navigationFiniteSet{
		values:       []navigationCandidate{{text: text, kind: NavigationValueLiteral, truth: truth, nullish: navigationNullishNo}},
		dependencies: navigationDependencyMarkers(text),
	}
}

func booleanNavigationSet(value bool) navigationFiniteSet {
	text := "false"
	truth := navigationTruthinessFalse
	if value {
		text = "true"
		truth = navigationTruthinessTrue
	}
	return navigationFiniteSet{values: []navigationCandidate{{text: text, kind: NavigationValueLiteral, truth: truth, nullish: navigationNullishNo}}}
}

func nullNavigationSet(text string) navigationFiniteSet {
	return navigationFiniteSet{values: []navigationCandidate{{text: text, kind: NavigationValueLiteral, truth: navigationTruthinessFalse, nullish: navigationNullishYes}}}
}

func navigationTruthinessForString(value string) navigationTruthiness {
	if value == "" {
		return navigationTruthinessFalse
	}
	return navigationTruthinessTrue
}

func (s *navigationFiniteSet) ensureUnknownCandidate() {
	if !s.unknown {
		return
	}
	values := make([]navigationCandidate, 0, minNavigationValueCapacity(len(s.values)+1))
	seen := make(map[navigationCandidateKey]struct{}, len(s.values))
	for _, candidate := range s.values {
		if candidate.kind == NavigationValueUnknown {
			continue
		}
		key := navigationCandidateKey{text: candidate.text, kind: candidate.kind}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if len(values) >= navigationValueLimit-1 {
			break
		}
		values = append(values, candidate)
	}
	values = append(values, unknownNavigationCandidate())
	s.values = values
}

func minNavigationValueCapacity(length int) int {
	if length < navigationValueLimit {
		return length
	}
	return navigationValueLimit
}

func (s *navigationFiniteSet) add(candidate navigationCandidate) {
	if candidate.kind == NavigationValueUnknown {
		s.unknown = true
	}
	if s.unknown {
		s.ensureUnknownCandidate()
	}
	for index, existing := range s.values {
		if existing.text == candidate.text && existing.kind == candidate.kind {
			if existing.truth != candidate.truth {
				s.values[index].truth = navigationTruthinessUnknown
			}
			if existing.nullish != candidate.nullish {
				s.values[index].nullish = navigationNullishUnknown
			}
			return
		}
	}
	if len(s.values) >= navigationValueLimit {
		s.unknown = true
		s.ensureUnknownCandidate()
		return
	}
	s.values = append(s.values, candidate)
	if s.unknown {
		s.ensureUnknownCandidate()
	}
}

func (s *navigationFiniteSet) merge(other navigationFiniteSet) {
	for _, candidate := range other.values {
		s.add(candidate)
	}
	s.unknown = s.unknown || other.unknown
	s.mergeDependencies(other)
	s.ensureUnknownCandidate()
}

func (s *navigationFiniteSet) mergeDependencies(other navigationFiniteSet) {
	seen := make(map[string]struct{}, len(s.dependencies)+len(other.dependencies))
	for _, dependency := range s.dependencies {
		seen[dependency] = struct{}{}
	}
	for _, dependency := range other.dependencies {
		if _, ok := seen[dependency]; ok {
			continue
		}
		seen[dependency] = struct{}{}
		s.dependencies = append(s.dependencies, dependency)
	}
}

func navigationDependencyMarkers(text string) []string {
	const prefix = "\x00ASP_NAV_DEP_"
	dependencies := make([]string, 0)
	for cursor := 0; cursor < len(text); {
		relative := strings.Index(text[cursor:], prefix)
		if relative < 0 {
			break
		}
		start := cursor + relative
		endOffset := strings.IndexByte(text[start+len(prefix):], 0)
		if endOffset < 0 {
			break
		}
		end := start + len(prefix) + endOffset + 1
		body := text[start+len(prefix) : end-1]
		underscore := strings.LastIndexByte(body, '_')
		if underscore == 32 && navigationDependencyHex(body[:underscore]) && navigationDependencyDigits(body[underscore+1:]) {
			dependencies = append(dependencies, text[start:end])
		}
		cursor = end
	}
	return dependencies
}

func navigationRegexDependencyMarkers(text string) []string {
	const escapedPrefix = `\x00ASP_NAV_DEP_`
	const escapedEnd = `\x00`
	dependencies := make([]string, 0)
	for cursor := 0; cursor < len(text); {
		relative := strings.Index(text[cursor:], escapedPrefix)
		if relative < 0 {
			break
		}
		start := cursor + relative + len(escapedPrefix)
		endOffset := strings.Index(text[start:], escapedEnd)
		if endOffset < 0 {
			break
		}
		end := start + endOffset
		body := text[start:end]
		underscore := strings.LastIndexByte(body, '_')
		if underscore == 32 && navigationDependencyHex(body[:underscore]) && navigationDependencyDigits(body[underscore+1:]) {
			dependencies = append(dependencies, "\x00ASP_NAV_DEP_"+body+"\x00")
		}
		cursor = end + len(escapedEnd)
	}
	return dependencies
}

func navigationDependencyHex(text string) bool {
	for _, char := range text {
		if char < '0' || char > '9' && char < 'a' || char > 'f' {
			return false
		}
	}
	return len(text) == 32
}

func navigationDependencyDigits(text string) bool {
	if text == "" {
		return false
	}
	for _, char := range text {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func navigationCandidateKind(left, right NavigationValueKind) NavigationValueKind {
	if left == NavigationValueUnknown || right == NavigationValueUnknown {
		return NavigationValueTemplate
	}
	if left == NavigationValueTemplate || right == NavigationValueTemplate {
		return NavigationValueTemplate
	}
	return NavigationValueLiteral
}

func navigationCandidateTruthiness(candidate navigationCandidate) (canFalse, canTrue bool) {
	if candidate.kind == NavigationValueUnknown {
		return true, true
	}
	switch candidate.truth {
	case navigationTruthinessFalse:
		return true, false
	case navigationTruthinessTrue:
		return false, true
	default:
		// Values produced by older state paths may not carry primitive metadata.
		// A non-empty finite value is conservatively truthy; an unclassified value
		// remains possible on both paths.
		if candidate.text == "" {
			return true, false
		}
		return true, true
	}
}

func navigationCandidateNullish(candidate navigationCandidate) (canNullish, canNonNullish bool) {
	if candidate.kind == NavigationValueUnknown {
		return true, true
	}
	switch candidate.nullish {
	case navigationNullishYes:
		return true, false
	case navigationNullishNo:
		return false, true
	default:
		return true, true
	}
}

func navigationSetMayBeTruthy(value navigationFiniteSet) (canFalse, canTrue bool) {
	for _, candidate := range value.values {
		candidateFalse, candidateTrue := navigationCandidateTruthiness(candidate)
		canFalse = canFalse || candidateFalse
		canTrue = canTrue || candidateTrue
	}
	if value.unknown || len(value.values) == 0 {
		canFalse, canTrue = true, true
	}
	return canFalse, canTrue
}

func navigationSetMayBeNullish(value navigationFiniteSet) (canNullish, canNonNullish bool) {
	for _, candidate := range value.values {
		candidateNullish, candidateNonNullish := navigationCandidateNullish(candidate)
		canNullish = canNullish || candidateNullish
		canNonNullish = canNonNullish || candidateNonNullish
	}
	if value.unknown || len(value.values) == 0 {
		canNullish, canNonNullish = true, true
	}
	return canNullish, canNonNullish
}

func filterNavigationSetByTruthiness(value navigationFiniteSet, wantTrue bool) navigationFiniteSet {
	result := navigationFiniteSet{dependencies: append([]string(nil), value.dependencies...)}
	for _, candidate := range value.values {
		canFalse, canTrue := navigationCandidateTruthiness(candidate)
		if (wantTrue && canTrue) || (!wantTrue && canFalse) {
			result.add(candidate)
		}
	}
	if value.unknown {
		result.add(navigationCandidate{text: "{unknown}", kind: NavigationValueUnknown})
		result.unknown = true
	}
	return result
}

func filterNavigationSetByNullish(value navigationFiniteSet, wantNullish bool) navigationFiniteSet {
	result := navigationFiniteSet{dependencies: append([]string(nil), value.dependencies...)}
	for _, candidate := range value.values {
		canNullish, canNonNullish := navigationCandidateNullish(candidate)
		if (wantNullish && canNullish) || (!wantNullish && canNonNullish) {
			result.add(candidate)
		}
	}
	if value.unknown {
		result.add(navigationCandidate{text: "{unknown}", kind: NavigationValueUnknown})
		result.unknown = true
	}
	return result
}

func concatenateNavigationSets(left, right navigationFiniteSet) navigationFiniteSet {
	result := navigationFiniteSet{}
	result.mergeDependencies(left)
	result.mergeDependencies(right)
	for _, leftValue := range left.values {
		for _, rightValue := range right.values {
			if leftValue.kind == NavigationValueUnknown {
				result.add(navigationCandidate{text: "{unknown}", kind: NavigationValueUnknown})
				continue
			}
			rightText, rightKind := rightValue.text, rightValue.kind
			if rightKind == NavigationValueUnknown {
				// A known prefix such as "list.asp?id=" + id keeps its destination;
				// only the unknown operand becomes a placeholder. An unknown operand
				// with no literal prefix still leaves the whole value unknown.
				if strings.TrimSpace(leftValue.text) == "" {
					result.add(navigationCandidate{text: "{unknown}", kind: NavigationValueUnknown})
					continue
				}
				rightText, rightKind = navigationUnknownOperandPlaceholder, NavigationValueTemplate
			}
			text := leftValue.text + rightText
			result.add(navigationCandidate{
				text:    text,
				kind:    navigationCandidateKind(leftValue.kind, rightKind),
				truth:   navigationTruthinessForString(text),
				nullish: navigationNullishNo,
			})
		}
	}
	result.unknown = left.unknown || result.unknown
	result.ensureUnknownCandidate()
	return result
}

const navigationUnknownOperandPlaceholder = "{value}"

type navigationAnalyzer struct {
	ctx                       context.Context
	source                    string
	file                      *ast.SourceFile
	utf16                     []int
	bindings                  map[string]*ast.Node
	values                    map[string]navigationFiniteSet
	functions                 map[string]*ast.Node
	rootShadowed              map[string]struct{}
	rootScope                 navigationBlockScope
	rootScopeActive           bool
	functionScopes            []navigationFunctionScope
	blockScopes               []navigationBlockScope
	functionBlockBoundary     int
	formActions               map[string]*ast.Node
	formActionValues          map[string]navigationFiniteSet
	formMethods               map[string]navigationFiniteSet
	evaluating                map[string]bool
	callStack                 map[*ast.Node]bool
	globalState               navigationState
	functionEnvironments      map[*ast.Node]navigationFunctionEnvironment
	functionParameterTokens   map[*ast.Node][]string
	parameterDependencies     map[string][]string
	observedParameters        map[string]bool
	nextParameterToken        int
	currentFunction           *ast.Node
	callSite                  *ast.Node
	transparentFunction       *ast.Node
	callDepth                 int
	sinks                     []NavigationSink
	expressionScopes          []map[*ast.Node]navigationFiniteSet
	expressionEffects         []map[*ast.Node]struct{}
	cancelErr                 error
	work                      *navigationWorkBudget
	unknownFallbackDone       bool
	collectingUnknownFallback bool
}

type navigationWorkBudget struct {
	limit     uint64
	used      uint64
	pending   uint64
	exhausted bool
	owner     *navigationAnalyzer
}

type navigationFunctionScope struct {
	identity    *navigationScopeIdentity
	functions   map[string]*ast.Node
	shadowed    map[string]struct{}
	varDeclared map[string]struct{}
	work        *navigationWorkBudget
}

type navigationBlockScope struct {
	identity    *navigationScopeIdentity
	functions   map[string]*ast.Node
	shadowed    map[string]struct{}
	declared    map[string]struct{}
	varDeclared map[string]struct{}
	work        *navigationWorkBudget
}

type navigationScopeIdentity struct {
	_ byte
}

type navigationFunctionEnvironment struct {
	state          navigationState
	functionScopes []navigationFunctionScope
	blockScopes    []navigationBlockScope
	rootActive     bool
	owner          *ast.Node
	work           *navigationWorkBudget
}

type navigationDeclarationSnapshot struct {
	binding     *ast.Node
	bindingOK   bool
	value       navigationFiniteSet
	valueOK     bool
	formAction  *ast.Node
	formOK      bool
	formValue   navigationFiniteSet
	formValueOK bool
	formMethod  navigationFiniteSet
	methodOK    bool
}

// navigationState is the finite abstract state at one control-flow point.
// Finite value initializers are snapshotted when their declaration executes;
// bindings remain only for callable or otherwise deferred expressions.
type navigationState struct {
	bindings         map[string]*ast.Node
	values           map[string]navigationFiniteSet
	formActions      map[string]*ast.Node
	formActionValues map[string]navigationFiniteSet
	formMethods      map[string]navigationFiniteSet
	work             *navigationWorkBudget
}

type navigationExecution struct {
	state        navigationState
	effectStates []navigationState
	hasNormal    bool
	returns      navigationFiniteSet
	hasReturn    bool
	hasBreak     bool
	hasContinue  bool
	unknown      bool
}

// appendNavigationStates charges the linear slice growth performed while
// collecting path effects. A branch can contribute many states, so charging
// only the enclosing branch would hide another multiplicative work source.
func (a *navigationAnalyzer) appendNavigationStates(target *[]navigationState, states []navigationState) bool {
	for _, state := range states {
		if !a.consumeNavigationWork(1) {
			return false
		}
		*target = append(*target, state)
	}
	return true
}

type navigationCallResult struct {
	value    navigationFiniteSet
	states   []navigationState
	global   navigationState
	env      navigationFunctionEnvironment
	executed bool
}

// navigationFlow records the paths that can leave a statement. A statement
// may have both a normal path and an abrupt path when the abrupt path comes
// from one branch of a conditional.
type navigationFlow struct {
	normal    bool
	returns   bool
	breaks    bool
	continues bool
}

func normalNavigationFlow() navigationFlow {
	return navigationFlow{normal: true}
}

func (flow *navigationFlow) merge(other navigationFlow) {
	flow.normal = flow.normal || other.normal
	flow.returns = flow.returns || other.returns
	flow.breaks = flow.breaks || other.breaks
	flow.continues = flow.continues || other.continues
}

func buildNavigationUTF16Offsets(ctx context.Context, source string) ([]int, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(source) > navigationSourceByteLimit {
		return nil, errNavigationSourceByteLimit
	}
	offsets := make([]int, len(source)+1)
	utf16Offset := 0
	for byteOffset := 0; byteOffset < len(source); {
		if byteOffset%navigationPreprocessingCheckInterval == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		r, size := utf8.DecodeRuneInString(source[byteOffset:])
		if size == 0 {
			size = 1
		}
		end := byteOffset + size
		if end > len(source) {
			end = len(source)
		}
		for index := byteOffset + 1; index < end; index++ {
			offsets[index] = utf16Offset
		}
		if r > 0xffff {
			utf16Offset += 2
		} else {
			utf16Offset++
		}
		byteOffset = end
		offsets[byteOffset] = utf16Offset
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return offsets, nil
}

func newNavigationAnalyzer(ctx context.Context, source string, file *ast.SourceFile, utf16Offsets []int) *navigationAnalyzer {
	analyzer := &navigationAnalyzer{
		ctx:                   ctx,
		source:                source,
		file:                  file,
		utf16:                 utf16Offsets,
		bindings:              map[string]*ast.Node{},
		values:                map[string]navigationFiniteSet{},
		functions:             map[string]*ast.Node{},
		rootShadowed:          map[string]struct{}{},
		rootScope:             navigationBlockScope{},
		rootScopeActive:       false,
		functionScopes:        nil,
		blockScopes:           nil,
		functionBlockBoundary: 0,
		formActions:           map[string]*ast.Node{},
		formActionValues:      map[string]navigationFiniteSet{},
		formMethods:           map[string]navigationFiniteSet{},
		evaluating:            map[string]bool{},
		callStack:             map[*ast.Node]bool{},
		globalState: navigationState{
			bindings:         map[string]*ast.Node{},
			values:           map[string]navigationFiniteSet{},
			formActions:      map[string]*ast.Node{},
			formActionValues: map[string]navigationFiniteSet{},
			formMethods:      map[string]navigationFiniteSet{},
			work:             nil,
		},
		functionEnvironments:    map[*ast.Node]navigationFunctionEnvironment{},
		functionParameterTokens: map[*ast.Node][]string{},
		parameterDependencies:   map[string][]string{},
		observedParameters:      map[string]bool{},
		expressionScopes:        nil,
		expressionEffects:       nil,
		work:                    &navigationWorkBudget{limit: NavigationEvaluationWorkLimit},
	}
	analyzer.globalState.work = analyzer.work
	analyzer.work.owner = analyzer
	analyzer.rootScope = analyzer.navigationSourceScope()
	for name, function := range analyzer.rootScope.functions {
		if !analyzer.consumeNavigationWork(1) {
			break
		}
		analyzer.functions[name] = function
	}
	for name := range analyzer.rootScope.shadowed {
		if !analyzer.consumeNavigationWork(1) {
			break
		}
		analyzer.rootShadowed[name] = struct{}{}
	}
	return analyzer
}

func (a *navigationAnalyzer) checkContext() error {
	if a.cancelErr != nil {
		return a.cancelErr
	}
	if err := a.ctx.Err(); err != nil {
		a.cancelErr = err
		return err
	}
	return nil
}

// consumeNavigationWork charges one unit of evaluator work. The shared
// counter is deliberately separate from context cancellation: exhausting the
// finite-analysis budget is a conservative result, not a cancelled request.
func (a *navigationAnalyzer) consumeNavigationWork(units uint64) bool {
	if a.cancelErr != nil {
		return false
	}
	if a.work == nil {
		return true
	}
	if a.work.exhausted {
		return false
	}
	if units == 0 {
		return true
	}
	if a.work.used >= a.work.limit || units > a.work.limit-a.work.used {
		a.work.used = a.work.limit
		a.work.pending = 0
		a.work.exhausted = true
		return false
	}
	a.work.used += units
	a.work.pending += units
	if a.work.pending < navigationEvaluationWorkCheckInterval && a.work.used < a.work.limit {
		return true
	}
	a.work.pending = 0
	if err := a.ctx.Err(); err != nil {
		a.cancelErr = err
		return false
	}
	if a.work.used >= a.work.limit {
		a.work.used = a.work.limit
		a.work.exhausted = true
		return false
	}
	return true
}

func (a *navigationAnalyzer) navigationWorkExhausted() bool {
	return a.work != nil && a.work.exhausted
}

func (b *navigationWorkBudget) consume(units uint64) bool {
	if b == nil {
		return true
	}
	if b.owner != nil {
		return b.owner.consumeNavigationWork(units)
	}
	if b.exhausted {
		return false
	}
	if b.used >= b.limit || units > b.limit-b.used {
		b.used = b.limit
		b.pending = 0
		b.exhausted = true
		return false
	}
	b.used += units
	b.pending += units
	if b.used >= b.limit {
		b.used = b.limit
		b.pending = 0
		b.exhausted = true
		return false
	}
	return true
}

func navigationCloneBudget(budgets []*navigationWorkBudget) *navigationWorkBudget {
	if len(budgets) == 0 {
		return nil
	}
	return budgets[0]
}

func cloneNavigationFiniteSet(value navigationFiniteSet, budgets ...*navigationWorkBudget) navigationFiniteSet {
	work := navigationCloneBudget(budgets)
	if work != nil && !work.consume(1) {
		return unknownNavigationSet()
	}
	cloned := make([]navigationCandidate, 0, len(value.values))
	for _, candidate := range value.values {
		if work != nil && !work.consume(1) {
			return unknownNavigationSet()
		}
		cloned = append(cloned, candidate)
	}
	value.values = cloned
	value.dependencies = append([]string(nil), value.dependencies...)
	return value
}

// beginNavigationExpressionScope gives one source expression occurrence a
// stable result while it is being registered, effect-evaluated, and reported
// as a sink. Nested function and branch evaluations receive independent
// scopes, so the same AST node can execute again when its source path really
// is visited again.
func (a *navigationAnalyzer) beginNavigationExpressionScope() func() {
	a.expressionScopes = append(a.expressionScopes, map[*ast.Node]navigationFiniteSet{})
	a.expressionEffects = append(a.expressionEffects, map[*ast.Node]struct{}{})
	return func() {
		if len(a.expressionScopes) > 0 {
			a.expressionScopes = a.expressionScopes[:len(a.expressionScopes)-1]
		}
		if len(a.expressionEffects) > 0 {
			a.expressionEffects = a.expressionEffects[:len(a.expressionEffects)-1]
		}
	}
}

func (a *navigationAnalyzer) navigationExpressionEffectWasEvaluated(expr *ast.Node) bool {
	if expr == nil || len(a.expressionEffects) == 0 {
		return false
	}
	_, ok := a.expressionEffects[len(a.expressionEffects)-1][expr]
	return ok
}

func (a *navigationAnalyzer) markNavigationExpressionEffect(expr *ast.Node) {
	if expr == nil || len(a.expressionEffects) == 0 {
		return
	}
	a.expressionEffects[len(a.expressionEffects)-1][expr] = struct{}{}
}

func (a *navigationAnalyzer) cachedNavigationExpression(expr *ast.Node) (navigationFiniteSet, bool) {
	if len(a.expressionScopes) == 0 {
		return navigationFiniteSet{}, false
	}
	value, ok := a.expressionScopes[len(a.expressionScopes)-1][expr]
	if !ok {
		return navigationFiniteSet{}, false
	}
	return cloneNavigationFiniteSet(value, a.work), true
}

func (a *navigationAnalyzer) cacheNavigationExpression(expr *ast.Node, value navigationFiniteSet) {
	if len(a.expressionScopes) == 0 {
		return
	}
	a.expressionScopes[len(a.expressionScopes)-1][expr] = cloneNavigationFiniteSet(value, a.work)
}

func isNavigationCallable(node *ast.Node) bool {
	if node == nil {
		return false
	}
	switch node.Kind {
	case ast.KindFunctionDeclaration, ast.KindFunctionExpression, ast.KindArrowFunction:
		return true
	default:
		return false
	}
}

func navigationCallableName(node *ast.Node) string {
	if node == nil || (node.Kind != ast.KindFunctionDeclaration && node.Kind != ast.KindFunctionExpression) {
		return ""
	}
	name := node.Name()
	if name == nil || name.Kind != ast.KindIdentifier {
		return ""
	}
	return name.Text()
}

func (a *navigationAnalyzer) indexFunctions() error {
	var walk func(*ast.Node, bool) error
	walk = func(node *ast.Node, inBlock bool) error {
		if node == nil {
			return nil
		}
		if err := a.checkContext(); err != nil {
			return err
		}
		if !a.consumeNavigationWork(1) {
			if err := a.checkContext(); err != nil {
				return err
			}
			return nil
		}
		if isNavigationFunction(node) {
			// Top-level declarations are copied from navigationSourceScope.
			// Nested declarations must remain in their lexical block/function
			// scope and must not overwrite the global lookup table.
			return nil
		}
		switch node.Kind {
		case ast.KindVariableStatement, ast.KindVariableDeclarationList:
			// Variable initializers are activated while the source is walked.
			// Indexing them here would make a callable var visible before its
			// assignment executes.
		case ast.KindBinaryExpression:
			if !inBlock && !a.rootLexicalName(node.AsBinaryExpression()) && !a.rootVarName(node.AsBinaryExpression()) {
				a.indexFunctionAssignment(node.AsBinaryExpression(), a.functions, a.rootShadowed)
			}
		}
		childInBlock := inBlock || node.Kind == ast.KindBlock
		var childErr error
		node.ForEachChild(func(child *ast.Node) bool {
			if a.navigationWorkExhausted() {
				return true
			}
			if err := walk(child, childInBlock); err != nil {
				childErr = err
				return true
			}
			return false
		})
		return childErr
	}
	return walk(a.file.AsNode(), false)
}

func navigationDeclarationListIsBlockScoped(node *ast.Node) bool {
	if node == nil {
		return false
	}
	var declarationList *ast.Node
	switch node.Kind {
	case ast.KindVariableStatement:
		declarationList = node.AsVariableStatement().DeclarationList
	case ast.KindVariableDeclarationList:
		declarationList = node
	}
	return declarationList != nil && declarationList.Flags&ast.NodeFlagsBlockScoped != 0
}

func (a *navigationAnalyzer) rootLexicalName(binary *ast.BinaryExpression) bool {
	if binary == nil || binary.Left == nil || binary.Left.Kind != ast.KindIdentifier {
		return false
	}
	_, ok := a.rootScope.shadowed[binary.Left.Text()]
	return ok
}

func (a *navigationAnalyzer) rootVarName(binary *ast.BinaryExpression) bool {
	if binary == nil || binary.Left == nil || binary.Left.Kind != ast.KindIdentifier {
		return false
	}
	_, ok := a.rootScope.varDeclared[binary.Left.Text()]
	return ok
}

func (a *navigationAnalyzer) navigationBlockScope(node *ast.Node) navigationBlockScope {
	scope := navigationBlockScope{
		identity:    &navigationScopeIdentity{},
		functions:   map[string]*ast.Node{},
		shadowed:    map[string]struct{}{},
		declared:    map[string]struct{}{},
		varDeclared: map[string]struct{}{},
		work:        a.work,
	}
	if node == nil || node.Kind != ast.KindBlock || node.AsBlock().Statements == nil {
		return scope
	}
	for _, statement := range node.AsBlock().Statements.Nodes {
		if statement == nil {
			continue
		}
		if !a.consumeNavigationWork(1) {
			break
		}
		switch statement.Kind {
		case ast.KindVariableStatement:
			if !navigationDeclarationListIsBlockScoped(statement) {
				continue
			}
			declarations := statement.AsVariableStatement().DeclarationList
			a.indexNavigationBlockDeclarations(&scope, declarations)
		case ast.KindFunctionDeclaration:
			name := statement.Name()
			if name != nil && name.Kind == ast.KindIdentifier {
				nameText := name.Text()
				scope.declared[nameText] = struct{}{}
				scope.functions[nameText] = statement
				delete(scope.shadowed, nameText)
			}
		case ast.KindImportDeclaration:
			a.indexNavigationImportBindings(&scope, statement)
		case ast.KindClassDeclaration:
			name := statement.AsClassDeclaration().Name()
			if name != nil && name.Kind == ast.KindIdentifier {
				nameText := name.Text()
				scope.declared[nameText] = struct{}{}
				delete(scope.functions, nameText)
				scope.shadowed[nameText] = struct{}{}
			}
		}
	}
	return scope
}

func (a *navigationAnalyzer) navigationSourceScope() navigationBlockScope {
	scope := navigationBlockScope{
		identity:    &navigationScopeIdentity{},
		functions:   map[string]*ast.Node{},
		shadowed:    map[string]struct{}{},
		declared:    map[string]struct{}{},
		varDeclared: map[string]struct{}{},
		work:        a.work,
	}
	if a.file == nil || a.file.Statements == nil {
		return scope
	}
	for _, statement := range a.file.Statements.Nodes {
		if statement == nil {
			continue
		}
		if !a.consumeNavigationWork(1) {
			break
		}
		switch statement.Kind {
		case ast.KindVariableStatement:
			if navigationDeclarationListIsBlockScoped(statement) {
				a.indexNavigationBlockDeclarations(&scope, statement.AsVariableStatement().DeclarationList)
			}
		case ast.KindFunctionDeclaration:
			if name := navigationCallableName(statement); name != "" {
				scope.functions[name] = statement
				delete(scope.shadowed, name)
			}
		case ast.KindImportDeclaration:
			a.indexNavigationImportBindings(&scope, statement)
		case ast.KindClassDeclaration:
			name := statement.AsClassDeclaration().Name()
			if name != nil && name.Kind == ast.KindIdentifier {
				nameText := name.Text()
				scope.declared[nameText] = struct{}{}
				delete(scope.functions, nameText)
				scope.shadowed[nameText] = struct{}{}
			}
		}
	}
	var collectVarDeclarations func(*ast.Node)
	collectVarDeclarations = func(node *ast.Node) {
		if node == nil || isNavigationFunction(node) || node.Kind == ast.KindClassDeclaration {
			return
		}
		if !a.consumeNavigationWork(1) {
			return
		}
		if (node.Kind == ast.KindVariableStatement || node.Kind == ast.KindVariableDeclarationList) && !navigationDeclarationListIsBlockScoped(node) {
			declarations := node
			if node.Kind == ast.KindVariableStatement {
				declarations = node.AsVariableStatement().DeclarationList
			}
			if declarations != nil && declarations.AsVariableDeclarationList().Declarations != nil {
				for _, declaration := range declarations.AsVariableDeclarationList().Declarations.Nodes {
					if !a.consumeNavigationWork(1) {
						return
					}
					name := declaration.Name()
					if name == nil || name.Kind != ast.KindIdentifier {
						continue
					}
					nameText := name.Text()
					scope.varDeclared[nameText] = struct{}{}
					if _, function := scope.functions[nameText]; !function {
						scope.shadowed[nameText] = struct{}{}
					}
				}
			}
		}
		node.ForEachChild(func(child *ast.Node) bool {
			collectVarDeclarations(child)
			return a.cancelErr != nil || a.navigationWorkExhausted()
		})
	}
	for _, statement := range a.file.Statements.Nodes {
		collectVarDeclarations(statement)
	}
	return scope
}

// Imports shadow same-named globals even when their module cannot be resolved.
func (a *navigationAnalyzer) indexNavigationImportBindings(scope *navigationBlockScope, statement *ast.Node) {
	clause := statement.AsImportDeclaration().ImportClause
	if clause == nil {
		return
	}
	add := func(name *ast.Node) {
		if name != nil && name.Kind == ast.KindIdentifier && a.consumeNavigationWork(1) {
			scope.declared[name.Text()] = struct{}{}
			scope.shadowed[name.Text()] = struct{}{}
			delete(scope.functions, name.Text())
		}
	}
	add(clause.Name())
	bindings := clause.AsImportClause().NamedBindings
	if bindings == nil {
		return
	}
	if bindings.Kind == ast.KindNamespaceImport {
		add(bindings.Name())
		return
	}
	for _, element := range bindings.Elements() {
		add(element.Name())
	}
}

func (a *navigationAnalyzer) indexNavigationBlockDeclarations(scope *navigationBlockScope, declarations *ast.Node) {
	if scope == nil || declarations == nil || declarations.AsVariableDeclarationList().Declarations == nil {
		return
	}
	for _, declaration := range declarations.AsVariableDeclarationList().Declarations.Nodes {
		if !a.consumeNavigationWork(1) {
			return
		}
		name := declaration.Name()
		if name == nil || name.Kind != ast.KindIdentifier {
			continue
		}
		nameText := name.Text()
		scope.declared[nameText] = struct{}{}
		// let/const bindings shadow outer functions from block entry, but
		// remain in TDZ until their declaration is reached.
		delete(scope.functions, nameText)
		scope.shadowed[nameText] = struct{}{}
	}
}

func navigationLoopBlockScope(node *ast.Node, budgets ...*navigationWorkBudget) navigationBlockScope {
	work := navigationCloneBudget(budgets)
	scope := navigationBlockScope{
		identity:    &navigationScopeIdentity{},
		functions:   map[string]*ast.Node{},
		shadowed:    map[string]struct{}{},
		declared:    map[string]struct{}{},
		varDeclared: map[string]struct{}{},
		work:        work,
	}
	if node != nil && node.Kind == ast.KindVariableDeclarationList && node.Flags&ast.NodeFlagsBlockScoped != 0 {
		declarations := node.AsVariableDeclarationList()
		if declarations.Declarations != nil {
			for _, declaration := range declarations.Declarations.Nodes {
				if work != nil && !work.consume(1) {
					break
				}
				name := declaration.Name()
				if name == nil || name.Kind != ast.KindIdentifier {
					continue
				}
				nameText := name.Text()
				scope.declared[nameText] = struct{}{}
				// The loop declaration is activated by visitForInitializer after
				// its initializer has been evaluated.
				scope.shadowed[nameText] = struct{}{}
			}
		}
	}
	return scope
}

func cloneNavigationBlockScope(scope navigationBlockScope) navigationBlockScope {
	work := scope.work
	if work != nil && !work.consume(uint64(len(scope.functions)+len(scope.shadowed)+len(scope.declared)+len(scope.varDeclared)+1)) {
		return navigationBlockScope{identity: scope.identity, work: work}
	}
	cloned := navigationBlockScope{
		identity:    scope.identity,
		functions:   make(map[string]*ast.Node, len(scope.functions)),
		shadowed:    make(map[string]struct{}, len(scope.shadowed)),
		declared:    make(map[string]struct{}, len(scope.declared)),
		varDeclared: make(map[string]struct{}, len(scope.varDeclared)),
		work:        work,
	}
	for name, function := range scope.functions {
		if work != nil && !work.consume(1) {
			return navigationBlockScope{identity: scope.identity, work: work}
		}
		cloned.functions[name] = function
	}
	for name := range scope.shadowed {
		if work != nil && !work.consume(1) {
			return navigationBlockScope{identity: scope.identity, work: work}
		}
		cloned.shadowed[name] = struct{}{}
	}
	for name := range scope.declared {
		if work != nil && !work.consume(1) {
			return navigationBlockScope{identity: scope.identity, work: work}
		}
		cloned.declared[name] = struct{}{}
	}
	for name := range scope.varDeclared {
		if work != nil && !work.consume(1) {
			return navigationBlockScope{identity: scope.identity, work: work}
		}
		cloned.varDeclared[name] = struct{}{}
	}
	return cloned
}

func cloneNavigationBlockScopes(scopes []navigationBlockScope) []navigationBlockScope {
	var work *navigationWorkBudget
	if len(scopes) > 0 {
		work = scopes[0].work
	}
	if work != nil && !work.consume(uint64(len(scopes)+1)) {
		return nil
	}
	cloned := make([]navigationBlockScope, len(scopes))
	for index, scope := range scopes {
		if work != nil && !work.consume(1) {
			return nil
		}
		cloned[index] = cloneNavigationBlockScope(scope)
	}
	return cloned
}

// copyNavigationBlockScopes copies only the scope stack. The maps inside a
// stack frame remain shared because ordinary lexical entry/exit relies on
// updates to the active frame being visible to its saved caller.
func copyNavigationBlockScopes(scopes []navigationBlockScope) []navigationBlockScope {
	var work *navigationWorkBudget
	if len(scopes) > 0 {
		work = scopes[0].work
	}
	if work != nil && !work.consume(uint64(len(scopes)+1)) {
		return nil
	}
	copied := make([]navigationBlockScope, len(scopes))
	for index, scope := range scopes {
		if work != nil && !work.consume(1) {
			return nil
		}
		copied[index] = scope
	}
	return copied
}

func cloneNavigationFunctionScope(scope navigationFunctionScope) navigationFunctionScope {
	work := scope.work
	if work != nil && !work.consume(uint64(len(scope.functions)+len(scope.shadowed)+len(scope.varDeclared)+1)) {
		return navigationFunctionScope{identity: scope.identity, work: work}
	}
	cloned := navigationFunctionScope{
		identity:    scope.identity,
		functions:   make(map[string]*ast.Node, len(scope.functions)),
		shadowed:    make(map[string]struct{}, len(scope.shadowed)),
		varDeclared: make(map[string]struct{}, len(scope.varDeclared)),
		work:        work,
	}
	for name, function := range scope.functions {
		if work != nil && !work.consume(1) {
			return navigationFunctionScope{identity: scope.identity, work: work}
		}
		cloned.functions[name] = function
	}
	for name := range scope.shadowed {
		if work != nil && !work.consume(1) {
			return navigationFunctionScope{identity: scope.identity, work: work}
		}
		cloned.shadowed[name] = struct{}{}
	}
	for name := range scope.varDeclared {
		if work != nil && !work.consume(1) {
			return navigationFunctionScope{identity: scope.identity, work: work}
		}
		cloned.varDeclared[name] = struct{}{}
	}
	return cloned
}

func cloneNavigationFunctionScopes(scopes []navigationFunctionScope) []navigationFunctionScope {
	var work *navigationWorkBudget
	if len(scopes) > 0 {
		work = scopes[0].work
	}
	if work != nil && !work.consume(uint64(len(scopes)+1)) {
		return nil
	}
	cloned := make([]navigationFunctionScope, len(scopes))
	for index, scope := range scopes {
		if work != nil && !work.consume(1) {
			return nil
		}
		cloned[index] = cloneNavigationFunctionScope(scope)
	}
	return cloned
}

// copyNavigationFunctionScopes is the shallow counterpart used when entering
// a lexical function scope. Captured map identity is part of the evaluator's
// scope semantics; only the outer slice needs independent append capacity.
func copyNavigationFunctionScopes(scopes []navigationFunctionScope) []navigationFunctionScope {
	var work *navigationWorkBudget
	if len(scopes) > 0 {
		work = scopes[0].work
	}
	if work != nil && !work.consume(uint64(len(scopes)+1)) {
		return nil
	}
	copied := make([]navigationFunctionScope, len(scopes))
	for index, scope := range scopes {
		if work != nil && !work.consume(1) {
			return nil
		}
		copied[index] = scope
	}
	return copied
}

func cloneNavigationFunctionEnvironment(environment navigationFunctionEnvironment) navigationFunctionEnvironment {
	return navigationFunctionEnvironment{
		state:          cloneNavigationState(environment.state),
		functionScopes: cloneNavigationFunctionScopes(environment.functionScopes),
		blockScopes:    cloneNavigationBlockScopes(environment.blockScopes),
		rootActive:     environment.rootActive,
		owner:          environment.owner,
		work:           environment.work,
	}
}

func (a *navigationAnalyzer) captureNavigationFunctionEnvironment(function *ast.Node) {
	if function == nil {
		return
	}
	// Direct source-level functions resolve global/module bindings at call
	// time. A function defined in a block or another function captures the
	// lexical state visible at its definition.
	if a.rootScopeActive && len(a.functionScopes) == 0 && len(a.blockScopes) <= 1 {
		return
	}
	a.functionEnvironments[function] = navigationFunctionEnvironment{
		state:          a.navigationState(),
		functionScopes: cloneNavigationFunctionScopes(a.functionScopes),
		blockScopes:    cloneNavigationBlockScopes(a.blockScopes),
		rootActive:     a.rootScopeActive,
		owner:          a.currentFunction,
		work:           a.work,
	}
}

func (a *navigationAnalyzer) refreshNavigationFunctionEnvironments() {
	functions := make(map[*ast.Node]struct{})
	boundary := a.functionBlockBoundary
	if boundary < 0 {
		boundary = 0
	}
	if boundary > len(a.blockScopes) {
		boundary = len(a.blockScopes)
	}
	start := boundary
	if a.currentFunction == nil && a.rootScopeActive && start == 0 && len(a.blockScopes) > 0 {
		// The first root scope is the module/global environment, not a
		// top-level block that can capture or shadow its declarations.
		start = 1
	}
	for index := len(a.blockScopes) - 1; index >= start; index-- {
		for _, function := range a.blockScopes[index].functions {
			if !a.consumeNavigationWork(1) {
				return
			}
			functions[function] = struct{}{}
		}
	}
	if len(a.functionScopes) > 0 {
		for _, function := range a.functionScopes[len(a.functionScopes)-1].functions {
			if !a.consumeNavigationWork(1) {
				return
			}
			functions[function] = struct{}{}
		}
	}
	for function := range functions {
		if !a.consumeNavigationWork(1) {
			return
		}
		if function == nil || function == a.currentFunction {
			continue
		}
		a.captureNavigationFunctionEnvironment(function)
	}
}

func mergeNavigationCallableScopes(blockScopes []navigationBlockScope, blockPaths [][]navigationBlockScope, functionScopes []navigationFunctionScope, functionPaths [][]navigationFunctionScope, budgets ...*navigationWorkBudget) {
	work := navigationCloneBudget(budgets)
	if work == nil {
		if len(blockScopes) > 0 {
			work = blockScopes[0].work
		} else if len(functionScopes) > 0 {
			work = functionScopes[0].work
		}
	}
	for index := range blockScopes {
		if work != nil && !work.consume(1) {
			return
		}
		if work != nil && !work.consume(uint64(len(blockPaths)+1)) {
			return
		}
		paths := make([]navigationBlockScope, 0, len(blockPaths))
		for _, path := range blockPaths {
			if work != nil && !work.consume(1) {
				return
			}
			if index < len(path) {
				paths = append(paths, path[index])
			}
		}
		mergeNavigationCallableBlockScope(&blockScopes[index], paths, work)
	}
	for index := range functionScopes {
		if work != nil && !work.consume(1) {
			return
		}
		if work != nil && !work.consume(uint64(len(functionPaths)+1)) {
			return
		}
		paths := make([]navigationFunctionScope, 0, len(functionPaths))
		for _, path := range functionPaths {
			if work != nil && !work.consume(1) {
				return
			}
			if index < len(path) {
				paths = append(paths, path[index])
			}
		}
		mergeNavigationCallableFunctionScope(&functionScopes[index], paths, work)
	}
}

func mergeNavigationCallableBlockScope(target *navigationBlockScope, paths []navigationBlockScope, budgets ...*navigationWorkBudget) {
	if target == nil || len(paths) == 0 {
		return
	}
	work := navigationCloneBudget(budgets)
	if work == nil {
		work = target.work
	}
	if work != nil && !work.consume(uint64(len(target.functions)+len(target.shadowed)+1)) {
		return
	}
	names := make(map[string]struct{}, len(target.functions)+len(target.shadowed))
	for name := range target.functions {
		if work != nil && !work.consume(1) {
			return
		}
		names[name] = struct{}{}
	}
	for name := range target.shadowed {
		if work != nil && !work.consume(1) {
			return
		}
		names[name] = struct{}{}
	}
	for _, path := range paths {
		for name := range path.functions {
			if work != nil && !work.consume(1) {
				return
			}
			names[name] = struct{}{}
		}
		for name := range path.shadowed {
			if work != nil && !work.consume(1) {
				return
			}
			names[name] = struct{}{}
		}
	}
	for name := range names {
		if work != nil && !work.consume(uint64(len(paths)+1)) {
			return
		}
		var function *ast.Node
		found := false
		allSame := true
		for _, path := range paths {
			if work != nil && !work.consume(1) {
				return
			}
			candidate, ok := path.functions[name]
			if !ok {
				allSame = false
				continue
			}
			if !found {
				function = candidate
				found = true
				continue
			}
			if function != candidate {
				allSame = false
			}
		}
		if found && allSame {
			target.functions[name] = function
			delete(target.shadowed, name)
		} else {
			delete(target.functions, name)
			target.shadowed[name] = struct{}{}
		}
	}
}

func mergeNavigationCallableFunctionScope(target *navigationFunctionScope, paths []navigationFunctionScope, budgets ...*navigationWorkBudget) {
	if target == nil || len(paths) == 0 {
		return
	}
	work := navigationCloneBudget(budgets)
	if work == nil {
		work = target.work
	}
	if work != nil && !work.consume(uint64(len(target.functions)+len(target.shadowed)+1)) {
		return
	}
	names := make(map[string]struct{}, len(target.functions)+len(target.shadowed))
	for name := range target.functions {
		if work != nil && !work.consume(1) {
			return
		}
		names[name] = struct{}{}
	}
	for name := range target.shadowed {
		if work != nil && !work.consume(1) {
			return
		}
		names[name] = struct{}{}
	}
	for _, path := range paths {
		for name := range path.functions {
			if work != nil && !work.consume(1) {
				return
			}
			names[name] = struct{}{}
		}
		for name := range path.shadowed {
			if work != nil && !work.consume(1) {
				return
			}
			names[name] = struct{}{}
		}
	}
	for name := range names {
		if work != nil && !work.consume(uint64(len(paths)+1)) {
			return
		}
		var function *ast.Node
		found := false
		allSame := true
		for _, path := range paths {
			if work != nil && !work.consume(1) {
				return
			}
			candidate, ok := path.functions[name]
			if !ok {
				allSame = false
				continue
			}
			if !found {
				function = candidate
				found = true
				continue
			}
			if function != candidate {
				allSame = false
			}
		}
		if found && allSame {
			target.functions[name] = function
			delete(target.shadowed, name)
		} else {
			delete(target.functions, name)
			target.shadowed[name] = struct{}{}
		}
	}
}

func (a *navigationAnalyzer) navigationSwitchScope(clauses []*ast.Node) navigationBlockScope {
	scope := navigationBlockScope{
		identity:    &navigationScopeIdentity{},
		functions:   map[string]*ast.Node{},
		shadowed:    map[string]struct{}{},
		declared:    map[string]struct{}{},
		varDeclared: map[string]struct{}{},
		work:        a.work,
	}
	for _, clause := range clauses {
		if !a.consumeNavigationWork(1) {
			break
		}
		if clause == nil || clause.AsCaseOrDefaultClause().Statements == nil {
			continue
		}
		for _, statement := range clause.AsCaseOrDefaultClause().Statements.Nodes {
			if !a.consumeNavigationWork(1) {
				break
			}
			if statement == nil {
				continue
			}
			switch statement.Kind {
			case ast.KindVariableStatement:
				if navigationDeclarationListIsBlockScoped(statement) {
					a.indexNavigationBlockDeclarations(&scope, statement.AsVariableStatement().DeclarationList)
				}
			case ast.KindFunctionDeclaration:
				if name := navigationCallableName(statement); name != "" {
					scope.declared[name] = struct{}{}
					scope.functions[name] = statement
					delete(scope.shadowed, name)
				}
			case ast.KindClassDeclaration:
				if name := statement.AsClassDeclaration().Name(); name != nil && name.Kind == ast.KindIdentifier {
					nameText := name.Text()
					scope.declared[nameText] = struct{}{}
					delete(scope.functions, nameText)
					scope.shadowed[nameText] = struct{}{}
				}
			}
		}
	}
	return scope
}

func (a *navigationAnalyzer) activateNavigationDeclarations(node *ast.Node) {
	if node == nil {
		return
	}
	var declarations *ast.Node
	if node.Kind == ast.KindVariableStatement {
		declarations = node.AsVariableStatement().DeclarationList
	} else {
		declarations = node
	}
	if declarations == nil || declarations.AsVariableDeclarationList().Declarations == nil {
		return
	}
	if navigationDeclarationListIsBlockScoped(node) {
		if len(a.blockScopes) == 0 {
			return
		}
		scope := &a.blockScopes[len(a.blockScopes)-1]
		a.activateNavigationBlockDeclarations(scope, declarations)
		return
	}
	for _, declaration := range declarations.AsVariableDeclarationList().Declarations.Nodes {
		if !a.consumeNavigationWork(1) {
			return
		}
		name := declaration.Name()
		if name == nil || name.Kind != ast.KindIdentifier {
			continue
		}
		a.activateNavigationVar(name.Text(), declaration.Initializer())
	}
}

func (a *navigationAnalyzer) activateNavigationBlockDeclarations(scope *navigationBlockScope, declarations *ast.Node) {
	if scope == nil || declarations == nil || declarations.AsVariableDeclarationList().Declarations == nil {
		return
	}
	for _, declaration := range declarations.AsVariableDeclarationList().Declarations.Nodes {
		if scope.work != nil && !scope.work.consume(1) {
			return
		}
		name := declaration.Name()
		if name == nil || name.Kind != ast.KindIdentifier {
			continue
		}
		nameText := name.Text()
		if _, declared := scope.declared[nameText]; !declared {
			continue
		}
		initializer := declaration.Initializer()
		if initializer != nil {
			initializer = ast.SkipOuterExpressions(initializer, ast.OEKAll)
		}
		if initializer != nil && isNavigationCallable(initializer) {
			scope.functions[nameText] = initializer
			delete(scope.shadowed, nameText)
		} else {
			delete(scope.functions, nameText)
			// Reaching the declaration ends its TDZ. The declared entry still
			// prevents callable lookup from falling through to an outer scope,
			// while value lookup can now observe later assignments.
			delete(scope.shadowed, nameText)
		}
	}
}

func (a *navigationAnalyzer) activateNavigationVar(name string, initializer *ast.Node) {
	if name == "" {
		return
	}
	var blockScope *navigationBlockScope
	boundary := a.functionBlockBoundary
	if boundary < 0 {
		boundary = 0
	}
	if boundary > len(a.blockScopes) {
		boundary = len(a.blockScopes)
	}
	for index := len(a.blockScopes) - 1; index >= boundary; index-- {
		if _, declared := a.blockScopes[index].varDeclared[name]; declared {
			blockScope = &a.blockScopes[index]
			break
		}
	}
	if blockScope != nil {
		a.applyNavigationVarValue(name, initializer, blockScope.functions, blockScope.shadowed)
		return
	}
	if len(a.functionScopes) > 0 {
		if _, declared := a.functionScopes[len(a.functionScopes)-1].varDeclared[name]; declared {
			a.applyNavigationVarValue(name, initializer, a.functionScopes[len(a.functionScopes)-1].functions, a.functionScopes[len(a.functionScopes)-1].shadowed)
			return
		}
	}
	for index := boundary - 1; index >= 0; index-- {
		if _, declared := a.blockScopes[index].varDeclared[name]; declared {
			a.applyNavigationVarValue(name, initializer, a.blockScopes[index].functions, a.blockScopes[index].shadowed)
			return
		}
	}
	for index := len(a.functionScopes) - 2; index >= 0; index-- {
		if _, declared := a.functionScopes[index].varDeclared[name]; declared {
			a.applyNavigationVarValue(name, initializer, a.functionScopes[index].functions, a.functionScopes[index].shadowed)
			return
		}
	}
}

func (a *navigationAnalyzer) applyNavigationVarValue(name string, initializer *ast.Node, functions map[string]*ast.Node, shadowed map[string]struct{}) {
	if initializer == nil {
		// A var declaration without an initializer does not overwrite an
		// existing function declaration or an earlier assignment.
		return
	}
	initializer = ast.SkipOuterExpressions(initializer, ast.OEKAll)
	if isNavigationCallable(initializer) {
		functions[name] = initializer
		delete(shadowed, name)
		return
	}
	delete(functions, name)
	delete(shadowed, name)
}

func (a *navigationAnalyzer) applyNavigationFunctionAssignment(binary *ast.BinaryExpression, name string, functions map[string]*ast.Node, shadowed map[string]struct{}) {
	if binary.OperatorToken.Kind == ast.KindPlusEqualsToken {
		delete(functions, name)
		delete(shadowed, name)
		return
	}
	a.applyNavigationVarValue(name, binary.Right, functions, shadowed)
}

func (a *navigationAnalyzer) activateNavigationFunctionAssignment(binary *ast.BinaryExpression) {
	if binary == nil || binary.Left == nil || binary.Left.Kind != ast.KindIdentifier || binary.OperatorToken == nil {
		return
	}
	name := binary.Left.Text()
	if binary.OperatorToken.Kind != ast.KindEqualsToken && binary.OperatorToken.Kind != ast.KindPlusEqualsToken {
		return
	}
	applyBlockAssignment := func(scope *navigationBlockScope) bool {
		if _, declared := scope.varDeclared[name]; declared {
			a.applyNavigationFunctionAssignment(binary, name, scope.functions, scope.shadowed)
			return true
		}
		if _, declared := scope.declared[name]; declared {
			a.applyNavigationFunctionAssignment(binary, name, scope.functions, scope.shadowed)
			return true
		}
		if _, declared := scope.functions[name]; declared {
			a.applyNavigationFunctionAssignment(binary, name, scope.functions, scope.shadowed)
			return true
		}
		if _, declared := scope.shadowed[name]; declared {
			a.applyNavigationFunctionAssignment(binary, name, scope.functions, scope.shadowed)
			return true
		}
		return false
	}
	boundary := a.functionBlockBoundary
	if boundary < 0 {
		boundary = 0
	}
	if boundary > len(a.blockScopes) {
		boundary = len(a.blockScopes)
	}
	for index := len(a.blockScopes) - 1; index >= boundary; index-- {
		if applyBlockAssignment(&a.blockScopes[index]) {
			return
		}
	}
	if len(a.functionScopes) > 0 {
		scope := &a.functionScopes[len(a.functionScopes)-1]
		if _, declared := scope.varDeclared[name]; declared {
			a.applyNavigationFunctionAssignment(binary, name, scope.functions, scope.shadowed)
			return
		}
		if _, declared := scope.functions[name]; declared {
			a.applyNavigationFunctionAssignment(binary, name, scope.functions, scope.shadowed)
			return
		}
		if _, declared := scope.shadowed[name]; declared {
			a.applyNavigationFunctionAssignment(binary, name, scope.functions, scope.shadowed)
			return
		}
	}
	for index := boundary - 1; index >= 0; index-- {
		if applyBlockAssignment(&a.blockScopes[index]) {
			return
		}
	}
	for index := len(a.functionScopes) - 2; index >= 0; index-- {
		scope := &a.functionScopes[index]
		if _, declared := scope.varDeclared[name]; declared {
			a.applyNavigationFunctionAssignment(binary, name, scope.functions, scope.shadowed)
			return
		}
		if _, declared := scope.functions[name]; declared {
			a.applyNavigationFunctionAssignment(binary, name, scope.functions, scope.shadowed)
			return
		}
		if _, declared := scope.shadowed[name]; declared {
			a.applyNavigationFunctionAssignment(binary, name, scope.functions, scope.shadowed)
			return
		}
	}
}

func (a *navigationAnalyzer) snapshotNavigationDeclarations(scope navigationBlockScope) map[string]navigationDeclarationSnapshot {
	if !a.consumeNavigationWork(uint64(len(scope.declared) + 1)) {
		return map[string]navigationDeclarationSnapshot{}
	}
	snapshots := make(map[string]navigationDeclarationSnapshot, len(scope.declared))
	for name := range scope.declared {
		if !a.consumeNavigationWork(1) {
			return map[string]navigationDeclarationSnapshot{}
		}
		snapshot := navigationDeclarationSnapshot{}
		snapshot.binding, snapshot.bindingOK = a.bindings[name]
		if value, ok := a.values[name]; ok {
			snapshot.value = cloneNavigationFiniteSet(value, a.work)
			snapshot.valueOK = true
		}
		snapshot.formAction, snapshot.formOK = a.formActions[name]
		if value, ok := a.formActionValues[name]; ok {
			snapshot.formValue = cloneNavigationFiniteSet(value, a.work)
			snapshot.formValueOK = true
		}
		if method, ok := a.formMethods[name]; ok {
			snapshot.formMethod = cloneNavigationFiniteSet(method, a.work)
			snapshot.methodOK = true
		}
		snapshots[name] = snapshot
	}
	return snapshots
}

func (a *navigationAnalyzer) restoreNavigationDeclarations(scope navigationBlockScope, snapshots map[string]navigationDeclarationSnapshot) {
	for name := range scope.declared {
		if !a.consumeNavigationWork(1) {
			return
		}
		snapshot := snapshots[name]
		if snapshot.bindingOK {
			a.bindings[name] = snapshot.binding
		} else {
			delete(a.bindings, name)
		}
		if snapshot.valueOK {
			a.values[name] = snapshot.value
		} else {
			delete(a.values, name)
		}
		if snapshot.formOK {
			a.formActions[name] = snapshot.formAction
		} else {
			delete(a.formActions, name)
		}
		if snapshot.formValueOK {
			a.formActionValues[name] = cloneNavigationFiniteSet(snapshot.formValue, a.work)
		} else {
			delete(a.formActionValues, name)
		}
		if snapshot.methodOK {
			a.formMethods[name] = snapshot.formMethod
		} else {
			delete(a.formMethods, name)
		}
	}
}

func (a *navigationAnalyzer) indexFunctionAssignment(binary *ast.BinaryExpression, functions map[string]*ast.Node, shadowed map[string]struct{}) {
	if binary == nil || binary.OperatorToken == nil || binary.OperatorToken.Kind != ast.KindEqualsToken || binary.Left == nil || binary.Left.Kind != ast.KindIdentifier {
		return
	}
	name := binary.Left.Text()
	if binary.Right != nil {
		if callable := ast.SkipOuterExpressions(binary.Right, ast.OEKAll); isNavigationCallable(callable) {
			functions[name] = callable
			delete(shadowed, name)
			return
		}
	}
	delete(functions, name)
	shadowed[name] = struct{}{}
}

func (a *navigationAnalyzer) navigationFunctionScope(function *ast.Node) navigationFunctionScope {
	scope := navigationFunctionScope{
		identity:    &navigationScopeIdentity{},
		functions:   map[string]*ast.Node{},
		shadowed:    map[string]struct{}{},
		varDeclared: map[string]struct{}{},
		work:        a.work,
	}
	for _, parameter := range function.Parameters() {
		if !a.consumeNavigationWork(1) {
			break
		}
		if err := a.checkContext(); err != nil {
			break
		}
		name := parameter.Name()
		if name != nil && name.Kind == ast.KindIdentifier {
			scope.varDeclared[name.Text()] = struct{}{}
			scope.shadowed[name.Text()] = struct{}{}
		}
	}
	if name := navigationCallableName(function); name != "" {
		scope.functions[name] = function
		delete(scope.shadowed, name)
	}
	body := function.Body()
	if body == nil {
		return scope
	}
	if body.Kind != ast.KindBlock || body.AsBlock().Statements == nil {
		return scope
	}
	for _, statement := range body.AsBlock().Statements.Nodes {
		if !a.consumeNavigationWork(1) {
			break
		}
		if err := a.checkContext(); err != nil {
			break
		}
		if statement == nil {
			continue
		}
		switch statement.Kind {
		case ast.KindFunctionDeclaration:
			if name := navigationCallableName(statement); name != "" {
				scope.functions[name] = statement
				delete(scope.shadowed, name)
			}
		case ast.KindClassDeclaration:
			if name := statement.AsClassDeclaration().Name(); name != nil && name.Kind == ast.KindIdentifier {
				delete(scope.functions, name.Text())
				scope.shadowed[name.Text()] = struct{}{}
			}
		}
	}
	var collectVarDeclarations func(*ast.Node)
	collectVarDeclarations = func(node *ast.Node) {
		if node == nil || isNavigationFunction(node) || node.Kind == ast.KindClassDeclaration {
			return
		}
		if !a.consumeNavigationWork(1) {
			return
		}
		if err := a.checkContext(); err != nil {
			return
		}
		if (node.Kind == ast.KindVariableStatement || node.Kind == ast.KindVariableDeclarationList) && !navigationDeclarationListIsBlockScoped(node) {
			declarations := node
			if node.Kind == ast.KindVariableStatement {
				declarations = node.AsVariableStatement().DeclarationList
			}
			if declarations != nil && declarations.AsVariableDeclarationList().Declarations != nil {
				for _, declaration := range declarations.AsVariableDeclarationList().Declarations.Nodes {
					if !a.consumeNavigationWork(1) {
						return
					}
					name := declaration.Name()
					if name == nil || name.Kind != ast.KindIdentifier {
						continue
					}
					nameText := name.Text()
					scope.varDeclared[nameText] = struct{}{}
					if _, function := scope.functions[nameText]; !function {
						scope.shadowed[nameText] = struct{}{}
					}
				}
			}
		}
		node.ForEachChild(func(child *ast.Node) bool {
			if a.cancelErr != nil {
				return true
			}
			collectVarDeclarations(child)
			return a.cancelErr != nil || a.navigationWorkExhausted()
		})
	}
	collectVarDeclarations(body)
	return scope
}

func addNavigationScopeNames(names map[string]struct{}, scope navigationBlockScope, budgets ...*navigationWorkBudget) {
	work := navigationCloneBudget(budgets)
	for name := range scope.functions {
		if work != nil && !work.consume(1) {
			return
		}
		names[name] = struct{}{}
	}
	for name := range scope.shadowed {
		if work != nil && !work.consume(1) {
			return
		}
		names[name] = struct{}{}
	}
	for name := range scope.declared {
		if work != nil && !work.consume(1) {
			return
		}
		names[name] = struct{}{}
	}
	for name := range scope.varDeclared {
		if work != nil && !work.consume(1) {
			return
		}
		names[name] = struct{}{}
	}
}

func addNavigationFunctionScopeNames(names map[string]struct{}, scope navigationFunctionScope, budgets ...*navigationWorkBudget) {
	work := navigationCloneBudget(budgets)
	for name := range scope.functions {
		if work != nil && !work.consume(1) {
			return
		}
		names[name] = struct{}{}
	}
	for name := range scope.shadowed {
		if work != nil && !work.consume(1) {
			return
		}
		names[name] = struct{}{}
	}
	for name := range scope.varDeclared {
		if work != nil && !work.consume(1) {
			return
		}
		names[name] = struct{}{}
	}
}

func (a *navigationAnalyzer) navigationFunctionLocalNames(function *ast.Node) map[string]struct{} {
	names := map[string]struct{}{}
	if function == nil {
		return names
	}
	functionScope := a.navigationFunctionScope(function)
	addNavigationFunctionScopeNames(names, functionScope, a.work)
	body := function.Body()
	if body == nil {
		return names
	}
	var collect func(*ast.Node)
	collect = func(node *ast.Node) {
		if node == nil {
			return
		}
		if !a.consumeNavigationWork(1) {
			return
		}
		if err := a.checkContext(); err != nil {
			return
		}
		if node != body && isNavigationFunction(node) {
			return
		}
		if node.Kind == ast.KindBlock {
			addNavigationScopeNames(names, a.navigationBlockScope(node), a.work)
		}
		node.ForEachChild(func(child *ast.Node) bool {
			if a.cancelErr != nil || a.navigationWorkExhausted() {
				return true
			}
			collect(child)
			return a.cancelErr != nil
		})
	}
	collect(body)
	return names
}

func navigationStateNames(state navigationState, budgets ...*navigationWorkBudget) map[string]struct{} {
	work := navigationCloneBudget(budgets)
	if work != nil && !work.consume(uint64(len(state.bindings)+len(state.values)+len(state.formActions)+len(state.formActionValues)+len(state.formMethods)+1)) {
		return map[string]struct{}{}
	}
	names := make(map[string]struct{}, len(state.bindings)+len(state.values)+len(state.formActions)+len(state.formActionValues)+len(state.formMethods))
	for name := range state.bindings {
		if work != nil && !work.consume(1) {
			return names
		}
		names[name] = struct{}{}
	}
	for name := range state.values {
		if work != nil && !work.consume(1) {
			return names
		}
		names[name] = struct{}{}
	}
	for name := range state.formActions {
		if work != nil && !work.consume(1) {
			return names
		}
		names[name] = struct{}{}
	}
	for name := range state.formActionValues {
		if work != nil && !work.consume(1) {
			return names
		}
		names[name] = struct{}{}
	}
	for name := range state.formMethods {
		if work != nil && !work.consume(1) {
			return names
		}
		names[name] = struct{}{}
	}
	return names
}

func (a *navigationAnalyzer) lookupNavigationFunction(name string) (*ast.Node, bool) {
	boundary := a.functionBlockBoundary
	if boundary < 0 {
		boundary = 0
	}
	if boundary > len(a.blockScopes) {
		boundary = len(a.blockScopes)
	}
	for index := len(a.blockScopes) - 1; index >= boundary; index-- {
		scope := a.blockScopes[index]
		if function, ok := scope.functions[name]; ok {
			return function, true
		}
		if _, ok := scope.shadowed[name]; ok {
			return nil, false
		}
		if _, ok := scope.declared[name]; ok {
			return nil, false
		}
		if _, ok := scope.varDeclared[name]; ok {
			return nil, false
		}
	}
	if len(a.functionScopes) > 0 {
		scope := a.functionScopes[len(a.functionScopes)-1]
		if function, ok := scope.functions[name]; ok {
			return function, true
		}
		if _, ok := scope.shadowed[name]; ok {
			return nil, false
		}
		if _, ok := scope.varDeclared[name]; ok {
			return nil, false
		}
	}
	for index := boundary - 1; index >= 0; index-- {
		scope := a.blockScopes[index]
		if function, ok := scope.functions[name]; ok {
			return function, true
		}
		if _, ok := scope.shadowed[name]; ok {
			return nil, false
		}
		if _, ok := scope.declared[name]; ok {
			return nil, false
		}
		if _, ok := scope.varDeclared[name]; ok {
			return nil, false
		}
	}
	for index := len(a.functionScopes) - 2; index >= 0; index-- {
		scope := a.functionScopes[index]
		if function, ok := scope.functions[name]; ok {
			return function, true
		}
		if _, ok := scope.shadowed[name]; ok {
			return nil, false
		}
		if _, ok := scope.varDeclared[name]; ok {
			return nil, false
		}
	}
	if _, ok := a.rootShadowed[name]; ok {
		return nil, false
	}
	function, ok := a.functions[name]
	return function, ok
}

func (a *navigationAnalyzer) navigationNameShadowed(name string) bool {
	boundary := a.functionBlockBoundary
	if boundary < 0 {
		boundary = 0
	}
	if boundary > len(a.blockScopes) {
		boundary = len(a.blockScopes)
	}
	for index := len(a.blockScopes) - 1; index >= boundary; index-- {
		scope := a.blockScopes[index]
		if _, ok := scope.shadowed[name]; ok {
			return true
		}
		if _, ok := scope.declared[name]; ok {
			return false
		}
		if _, ok := scope.functions[name]; ok {
			return false
		}
		if _, ok := scope.varDeclared[name]; ok {
			return false
		}
	}
	if len(a.functionScopes) > 0 {
		scope := a.functionScopes[len(a.functionScopes)-1]
		if _, ok := scope.shadowed[name]; ok {
			return true
		}
		if _, ok := scope.functions[name]; ok {
			return false
		}
		if _, ok := scope.varDeclared[name]; ok {
			return false
		}
	}
	for index := boundary - 1; index >= 0; index-- {
		scope := a.blockScopes[index]
		if _, ok := scope.shadowed[name]; ok {
			return true
		}
		if _, ok := scope.declared[name]; ok {
			return false
		}
		if _, ok := scope.functions[name]; ok {
			return false
		}
		if _, ok := scope.varDeclared[name]; ok {
			return false
		}
	}
	for index := len(a.functionScopes) - 2; index >= 0; index-- {
		scope := a.functionScopes[index]
		if _, ok := scope.shadowed[name]; ok {
			return true
		}
		if _, ok := scope.functions[name]; ok {
			return false
		}
		if _, ok := scope.varDeclared[name]; ok {
			return false
		}
	}
	if !a.rootScopeActive {
		_, ok := a.rootShadowed[name]
		return ok
	}
	return false
}

func (a *navigationAnalyzer) sourceRange(node *ast.Node) SourceRange {
	if node == nil {
		return SourceRange{}
	}
	start, end := node.Pos(), node.End()
	if start < 0 {
		start = 0
	}
	if end < start {
		end = start
	}
	if start > len(a.source) {
		start = len(a.source)
	}
	if end > len(a.source) {
		end = len(a.source)
	}
	return SourceRange{Start: a.utf16[start], End: a.utf16[end], ByteStart: start, ByteEnd: end}
}

func (a *navigationAnalyzer) sourceText(r SourceRange) string {
	if r.ByteStart < 0 || r.ByteStart > r.ByteEnd || r.ByteEnd > len(a.source) {
		return ""
	}
	return a.source[r.ByteStart:r.ByteEnd]
}

func (a *navigationAnalyzer) visit(node *ast.Node) (navigationFlow, error) {
	if node == nil {
		return normalNavigationFlow(), nil
	}
	if err := a.checkContext(); err != nil {
		return navigationFlow{}, err
	}
	if !a.consumeNavigationWork(1) {
		if err := a.checkContext(); err != nil {
			return navigationFlow{}, err
		}
		return normalNavigationFlow(), nil
	}
	var endExpressionScope func()
	switch node.Kind {
	case ast.KindVariableStatement, ast.KindExpressionStatement, ast.KindReturnStatement:
		endExpressionScope = a.beginNavigationExpressionScope()
		defer endExpressionScope()
	}
	if isNavigationFunction(node) {
		// Function bodies are inspected for sinks, but defining a function does
		// not itself make the containing statement sequence abrupt.
		a.captureNavigationFunctionEnvironment(node)
		savedBindings := a.bindings
		savedValues := a.values
		savedFormActions := a.formActions
		savedFormActionValues := a.formActionValues
		savedFormMethods := a.formMethods
		savedFunctionScopes := a.functionScopes
		savedBlockScopes := a.blockScopes
		savedFunctionBlockBoundary := a.functionBlockBoundary
		savedRootScopeActive := a.rootScopeActive
		savedCurrentFunction := a.currentFunction
		a.bindings = cloneNavigationBindings(savedBindings, a.work)
		a.values = cloneNavigationValues(savedValues, a.work)
		a.formActions = cloneNavigationBindings(savedFormActions, a.work)
		a.formActionValues = cloneNavigationFiniteSets(savedFormActionValues, a.work)
		a.formMethods = cloneNavigationFiniteSets(savedFormMethods, a.work)
		a.functionScopes = append(copyNavigationFunctionScopes(savedFunctionScopes), a.navigationFunctionScope(node))
		a.blockScopes = nil
		a.functionBlockBoundary = 0
		a.rootScopeActive = false
		a.currentFunction = node
		a.bindNavigationFunctionParameters(node)
		body := node.Body()
		if body != nil {
			if _, err := a.visit(body); err != nil {
				a.bindings = savedBindings
				a.values = savedValues
				a.formActions = savedFormActions
				a.formActionValues = savedFormActionValues
				a.formMethods = savedFormMethods
				a.functionScopes = savedFunctionScopes
				a.blockScopes = savedBlockScopes
				a.functionBlockBoundary = savedFunctionBlockBoundary
				a.rootScopeActive = savedRootScopeActive
				a.currentFunction = savedCurrentFunction
				return navigationFlow{}, err
			}
		}
		a.bindings = savedBindings
		a.values = savedValues
		a.formActions = savedFormActions
		a.formActionValues = savedFormActionValues
		a.formMethods = savedFormMethods
		a.functionScopes = savedFunctionScopes
		a.blockScopes = savedBlockScopes
		a.functionBlockBoundary = savedFunctionBlockBoundary
		a.rootScopeActive = savedRootScopeActive
		a.currentFunction = savedCurrentFunction
		return normalNavigationFlow(), nil
	}
	switch node.Kind {
	case ast.KindBlock:
		scope := a.navigationBlockScope(node)
		snapshots := a.snapshotNavigationDeclarations(scope)
		savedBlockScopes := a.blockScopes
		a.blockScopes = append(copyNavigationBlockScopes(savedBlockScopes), scope)
		flow, err := a.visitStatementSequence(node.AsBlock().Statements.Nodes)
		a.updateGlobalStateFromNavigationState(a.navigationState())
		a.restoreNavigationDeclarations(scope, snapshots)
		a.blockScopes = savedBlockScopes
		a.applyGlobalStateToCurrentState()
		return flow, err
	case ast.KindIfStatement:
		return a.visitIfStatement(node)
	case ast.KindSwitchStatement:
		return a.visitSwitchStatement(node)
	case ast.KindDoStatement, ast.KindWhileStatement, ast.KindForStatement,
		ast.KindForInStatement, ast.KindForOfStatement:
		return a.visitLoopStatement(node)
	case ast.KindReturnStatement:
		if expression := node.AsReturnStatement().Expression; expression != nil {
			if _, err := a.visit(expression); err != nil {
				return navigationFlow{}, err
			}
		}
		return navigationFlow{returns: true}, nil
	case ast.KindBreakStatement:
		return navigationFlow{breaks: true}, nil
	case ast.KindContinueStatement:
		return navigationFlow{continues: true}, nil
	}
	if node.Kind == ast.KindVariableStatement {
		a.registerVariableStatement(node)
		a.activateNavigationDeclarations(node)
		a.refreshNavigationFunctionEnvironments()
	}
	if ast.IsAssignmentExpression(node, false) && !a.navigationExpressionEffectWasEvaluated(node) {
		a.registerAssignment(node.AsBinaryExpression())
		a.activateNavigationFunctionAssignment(node.AsBinaryExpression())
		a.refreshNavigationFunctionEnvironments()
		a.registerNavigationFormMethodAssignment(node.AsBinaryExpression())
	}
	if node.Kind == ast.KindExpressionStatement && a.currentFunction == nil {
		a.evaluateNavigationExpressionEffects(node.AsExpressionStatement().Expression)
	}
	if sink, ok := a.navigationSink(node); ok {
		a.sinks = append(a.sinks, sink)
	}
	flow := normalNavigationFlow()
	var childErr error
	node.ForEachChild(func(child *ast.Node) bool {
		if a.navigationWorkExhausted() {
			return true
		}
		childFlow, err := a.visit(child)
		if err != nil {
			childErr = err
			return true
		}
		flow.merge(childFlow)
		return !childFlow.normal
	})
	if childErr != nil {
		return navigationFlow{}, childErr
	}
	return flow, nil
}

func (a *navigationAnalyzer) visitStatementSequence(statements []*ast.Node) (navigationFlow, error) {
	flow := normalNavigationFlow()
	for _, statement := range statements {
		if err := a.checkContext(); err != nil {
			return navigationFlow{}, err
		}
		if !flow.normal {
			break
		}
		step, err := a.visit(statement)
		if err != nil {
			return navigationFlow{}, err
		}
		if a.navigationWorkExhausted() {
			break
		}
		flow = step
	}
	return flow, nil
}

func (a *navigationAnalyzer) visitIfStatement(node *ast.Node) (navigationFlow, error) {
	statement := node.AsIfStatement()
	conditionValue := navigationFiniteSet{}
	if statement.Expression != nil {
		endExpressionScope := a.beginNavigationExpressionScope()
		if _, err := a.visit(statement.Expression); err != nil {
			endExpressionScope()
			return navigationFlow{}, err
		}
		conditionValue = a.evaluateExpression(statement.Expression)
		endExpressionScope()
		if err := a.checkContext(); err != nil {
			return navigationFlow{}, err
		}
	}
	sinkStart := len(a.sinks)
	base := a.navigationState()
	branches := make([]navigationFlow, 0, 2)
	states := make([]navigationState, 0, 2)

	a.restoreNavigationState(cloneNavigationState(base))
	thenFlow := normalNavigationFlow()
	if statement.ThenStatement != nil {
		var err error
		thenFlow, err = a.visit(statement.ThenStatement)
		if err != nil {
			return navigationFlow{}, err
		}
	}
	branches = append(branches, thenFlow)
	if thenFlow.normal {
		states = append(states, a.materializeNavigationState(a.navigationState()))
	}

	elseFlow := normalNavigationFlow()
	if statement.ElseStatement == nil {
		states = append(states, a.materializeNavigationState(cloneNavigationState(base)))
	} else {
		a.restoreNavigationState(cloneNavigationState(base))
		var err error
		elseFlow, err = a.visit(statement.ElseStatement)
		if err != nil {
			return navigationFlow{}, err
		}
		if elseFlow.normal {
			states = append(states, a.materializeNavigationState(a.navigationState()))
		}
	}
	branches = append(branches, elseFlow)
	if len(states) > 0 {
		merged := a.mergeNavigationStates(states...)
		taintNavigationStateAlternatives(&merged, states, conditionValue.dependencies)
		a.restoreNavigationState(merged)
	} else {
		a.restoreNavigationState(cloneNavigationState(base))
	}
	flow := navigationFlow{}
	for _, branch := range branches {
		flow.merge(branch)
	}
	taintNavigationSinkAlternatives(a.sinks[sinkStart:], conditionValue.dependencies)
	return flow, nil
}

func (a *navigationAnalyzer) visitSwitchStatement(node *ast.Node) (navigationFlow, error) {
	statement := node.AsSwitchStatement()
	conditionValue := navigationFiniteSet{}
	if statement.Expression != nil {
		endExpressionScope := a.beginNavigationExpressionScope()
		if _, err := a.visit(statement.Expression); err != nil {
			endExpressionScope()
			return navigationFlow{}, err
		}
		conditionValue = a.evaluateExpression(statement.Expression)
		endExpressionScope()
		if err := a.checkContext(); err != nil {
			return navigationFlow{}, err
		}
	}
	base := a.navigationState()
	var clauses []*ast.Node
	if statement.CaseBlock != nil && statement.CaseBlock.AsCaseBlock().Clauses != nil {
		clauses = statement.CaseBlock.AsCaseBlock().Clauses.Nodes
	}
	if !a.consumeNavigationWork(uint64(len(clauses))) {
		if err := a.checkContext(); err != nil {
			return navigationFlow{}, err
		}
		return normalNavigationFlow(), nil
	}
	starts := navigationSwitchPathStarts(clauses)
	if !a.consumeNavigationWork(uint64(len(starts))) {
		if err := a.checkContext(); err != nil {
			return navigationFlow{}, err
		}
		return normalNavigationFlow(), nil
	}
	if !a.consumeNavigationWork(uint64(len(starts))) {
		if err := a.checkContext(); err != nil {
			return navigationFlow{}, err
		}
		return normalNavigationFlow(), nil
	}
	if !a.consumeNavigationWork(uint64(len(starts))) {
		if err := a.checkContext(); err != nil {
			return navigationFlow{}, err
		}
		return normalNavigationFlow(), nil
	}
	states := make([]navigationState, 0, len(starts))
	flow := navigationFlow{}
	sinkStart := len(a.sinks)
	mergedSinks := newNavigationSinkAccumulator(len(starts))
	savedBlockScopes := a.blockScopes
	savedFunctionScopes := a.functionScopes
	switchScope := a.navigationSwitchScope(clauses)
	snapshots := a.snapshotNavigationDeclarations(switchScope)
	blockPaths := make([][]navigationBlockScope, 0, len(starts))
	functionPaths := make([][]navigationFunctionScope, 0, len(starts))
	defer func() {
		a.blockScopes = savedBlockScopes
		a.functionScopes = savedFunctionScopes
	}()
	for _, start := range starts {
		if !a.consumeNavigationWork(1) {
			if err := a.checkContext(); err != nil {
				a.sinks = a.sinks[:sinkStart]
				return navigationFlow{}, err
			}
			break
		}
		a.restoreNavigationState(cloneNavigationState(base))
		a.sinks = a.sinks[:sinkStart]
		a.blockScopes = append(cloneNavigationBlockScopes(savedBlockScopes), cloneNavigationBlockScope(switchScope))
		a.functionScopes = cloneNavigationFunctionScopes(savedFunctionScopes)
		pathFlow := normalNavigationFlow()
		if start >= 0 {
			var err error
			pathFlow, err = a.visitSwitchPath(clauses, start)
			if err != nil {
				a.sinks = a.sinks[:sinkStart]
				return navigationFlow{}, err
			}
		}
		if a.navigationWorkExhausted() {
			// Sinks collected before exhaustion may still be held in the
			// current path slice. Preserve them before the next path reset;
			// the structural fallback must not replace these exact results.
			a.mergeNavigationSinksAfterExhaustion(&mergedSinks, a.sinks[sinkStart:])
			break
		}
		if pathFlow.normal {
			if !a.consumeNavigationWork(1) {
				// The path has already been visited and may have produced exact
				// sinks even though state snapshot bookkeeping cannot finish.
				a.mergeNavigationSinksAfterExhaustion(&mergedSinks, a.sinks[sinkStart:])
				break
			}
			blockPaths = append(blockPaths, cloneNavigationBlockScopes(a.blockScopes))
			if !a.consumeNavigationWork(1) {
				a.mergeNavigationSinksAfterExhaustion(&mergedSinks, a.sinks[sinkStart:])
				break
			}
			functionPaths = append(functionPaths, cloneNavigationFunctionScopes(a.functionScopes))
		}
		flow.merge(pathFlow)
		pathSinks := a.sinks[sinkStart:]
		for index, sink := range pathSinks {
			if !a.consumeNavigationWork(uint64(len(sink.Expression.Values) + 1)) {
				// The current path already resolved every sink in this slice.
				// Keep the remainder exact even when charging merge bookkeeping
				// exhausts the shared budget. The accumulator keeps this
				// recovery merge indexed rather than rescanning prior sinks.
				a.mergeNavigationSinksAfterExhaustion(&mergedSinks, pathSinks[index:])
				break
			}
			mergedSinks.merge(sink)
		}
		if pathFlow.normal {
			if !a.consumeNavigationWork(1) {
				break
			}
			states = append(states, a.materializeNavigationState(a.navigationState()))
		}
	}
	if !a.navigationWorkExhausted() {
		// Charge the copy when possible, but retain the already-resolved
		// sinks if this final bookkeeping charge reaches the limit.
		_ = a.consumeNavigationWork(uint64(len(mergedSinks.sinks) + 1))
	}
	a.sinks = append(a.sinks[:sinkStart], mergedSinks.sinks...)
	taintNavigationSinkAlternatives(a.sinks[sinkStart:], conditionValue.dependencies)
	// An unlabelled break is consumed by this switch, including breaks from
	// one branch of a conditional clause.
	flow.breaks = false
	if len(states) > 0 {
		merged := a.mergeNavigationStates(states...)
		taintNavigationStateAlternatives(&merged, states, conditionValue.dependencies)
		a.restoreNavigationState(merged)
	} else {
		a.restoreNavigationState(cloneNavigationState(base))
	}
	mergeNavigationCallableScopes(savedBlockScopes, blockPaths, savedFunctionScopes, functionPaths, a.work)
	a.restoreNavigationDeclarations(switchScope, snapshots)
	return flow, nil
}

func navigationSwitchPathStarts(clauses []*ast.Node) []int {
	starts := make([]int, 0, len(clauses))
	hasDefault := false
	for index, clause := range clauses {
		if clause == nil {
			continue
		}
		if clause.Kind == ast.KindDefaultClause {
			hasDefault = true
		}
		starts = append(starts, index)
	}
	if !hasDefault {
		starts = append([]int{-1}, starts...)
	}
	return starts
}

func (a *navigationAnalyzer) visitSwitchPath(clauses []*ast.Node, start int) (navigationFlow, error) {
	pathFlow := normalNavigationFlow()
	matchingEnd := start
	if matchingEnd < 0 || (matchingEnd < len(clauses) && clauses[matchingEnd] != nil && clauses[matchingEnd].Kind == ast.KindDefaultClause) {
		matchingEnd = len(clauses) - 1
	}
	for index := 0; index <= matchingEnd && index < len(clauses); index++ {
		if !a.consumeNavigationWork(1) {
			if err := a.checkContext(); err != nil {
				return navigationFlow{}, err
			}
			return normalNavigationFlow(), nil
		}
		clause := clauses[index]
		if clause == nil {
			continue
		}
		caseClause := clause.AsCaseOrDefaultClause()
		if caseClause.Expression != nil {
			endExpressionScope := a.beginNavigationExpressionScope()
			if _, err := a.visit(caseClause.Expression); err != nil {
				endExpressionScope()
				return navigationFlow{}, err
			}
			if a.currentFunction == nil {
				a.evaluateNavigationExpressionEffects(caseClause.Expression)
			}
			endExpressionScope()
			if err := a.checkContext(); err != nil {
				return navigationFlow{}, err
			}
		}
	}
	if start < 0 {
		return pathFlow, nil
	}
	for index := start; index < len(clauses); index++ {
		if !a.consumeNavigationWork(1) {
			if err := a.checkContext(); err != nil {
				return navigationFlow{}, err
			}
			return normalNavigationFlow(), nil
		}
		clause := clauses[index]
		if clause == nil {
			continue
		}
		caseClause := clause.AsCaseOrDefaultClause()
		if caseClause.Statements == nil {
			continue
		}
		for _, statement := range caseClause.Statements.Nodes {
			step, err := a.visit(statement)
			if err != nil {
				return navigationFlow{}, err
			}
			pathFlow.returns = pathFlow.returns || step.returns
			pathFlow.continues = pathFlow.continues || step.continues
			if !step.normal {
				if step.breaks {
					// break exits the switch and is normal to the enclosing
					// statement sequence; return/continue still propagate.
					pathFlow.normal = true
					pathFlow.breaks = false
					return pathFlow, nil
				}
				pathFlow.normal = false
				pathFlow.breaks = false
				return pathFlow, nil
			}
		}
	}
	return pathFlow, nil
}

type navigationSinkKey struct {
	kind       string
	rangeStart int
	rangeEnd   int
	exprStart  int
	exprEnd    int
	formName   string
	method     string
	occurrence SourceRange
}

// navigationSinkOccurrenceKey identifies one source sink occurrence without
// including path-dependent expression or form method metadata. This lets
// fallback reporting skip an occurrence that already has an exact primary
// result, even when a later structural pass cannot reconstruct its state.
type navigationSinkOccurrenceKey struct {
	kind       string
	formName   string
	occurrence SourceRange
}

func navigationSinkOccurrenceKeyFor(sink NavigationSink) navigationSinkOccurrenceKey {
	return navigationSinkOccurrenceKey{
		kind:       sink.Kind,
		formName:   sink.FormName,
		occurrence: sink.occurrence,
	}
}

func navigationSinkKeyFor(sink NavigationSink) navigationSinkKey {
	return navigationSinkKey{
		kind:       sink.Kind,
		rangeStart: sink.Range.Start,
		rangeEnd:   sink.Range.End,
		exprStart:  sink.Expression.Range.Start,
		exprEnd:    sink.Expression.Range.End,
		formName:   sink.FormName,
		method:     sink.Method,
		occurrence: sink.occurrence,
	}
}

type navigationSinkAccumulator struct {
	sinks   []NavigationSink
	indices map[navigationSinkKey]int
}

func newNavigationSinkAccumulator(capacity int) navigationSinkAccumulator {
	if capacity < 0 {
		capacity = 0
	}
	return navigationSinkAccumulator{
		sinks:   make([]NavigationSink, 0, capacity),
		indices: make(map[navigationSinkKey]int, capacity),
	}
}

func (accumulator *navigationSinkAccumulator) merge(sink NavigationSink) {
	if accumulator == nil {
		return
	}
	key := navigationSinkKeyFor(sink)
	if index, ok := accumulator.indices[key]; ok {
		mergeNavigationExpression(&accumulator.sinks[index].Expression, sink.Expression)
		return
	}
	accumulator.indices[key] = len(accumulator.sinks)
	accumulator.sinks = append(accumulator.sinks, sink)
}

// mergeNavigationSinksAfterExhaustion preserves sinks already resolved by a
// path whose bookkeeping charge reached the shared limit. The path slice is
// itself budget-bounded, and the indexed accumulator keeps this recovery pass
// linear instead of rescanning the accumulated output for every sink.
func (a *navigationAnalyzer) mergeNavigationSinksAfterExhaustion(accumulator *navigationSinkAccumulator, sinks []NavigationSink) {
	if accumulator == nil {
		return
	}
	for index, sink := range sinks {
		if index%int(navigationEvaluationWorkCheckInterval) == 0 {
			if err := a.checkContext(); err != nil {
				return
			}
		}
		accumulator.merge(sink)
	}
}

// indexResolvedNavigationSinks builds the fallback's exact-occurrence index
// with an explicit element bound. The primary traversal's shared budget keeps
// this list below the bound in normal operation, but retaining the check here
// makes the allocation and cancellation behavior independent of that proof.
func (a *navigationAnalyzer) indexResolvedNavigationSinks() map[navigationSinkOccurrenceKey]struct{} {
	indexWork := uint64(len(a.sinks)) // one bounded unit per preallocated/indexed entry
	if indexWork > navigationSinkCountLimit {
		a.cancelErr = errNavigationSinkCountLimit
		return nil
	}
	if err := a.checkContext(); err != nil {
		return nil
	}
	resolved := make(map[navigationSinkOccurrenceKey]struct{}, len(a.sinks))
	for index, sink := range a.sinks {
		if index%int(navigationEvaluationWorkCheckInterval) == 0 {
			if err := a.checkContext(); err != nil {
				return nil
			}
		}
		resolved[navigationSinkOccurrenceKeyFor(sink)] = struct{}{}
	}
	if err := a.checkContext(); err != nil {
		return nil
	}
	return resolved
}

func mergeNavigationExpression(target *NavigationExpression, source NavigationExpression) {
	if target == nil {
		return
	}
	values := make([]NavigationValue, 0, len(target.Values)+len(source.Values))
	values = append(values, target.Values...)
	values = append(values, source.Values...)
	target.Values = boundedNavigationValues(values, false)
}

func (a *navigationAnalyzer) visitLoopStatement(node *ast.Node) (navigationFlow, error) {
	var body *ast.Node
	var base navigationState
	isDo := node.Kind == ast.KindDoStatement
	var loopInitializer *ast.Node
	switch node.Kind {
	case ast.KindForStatement:
		loopInitializer = node.AsForStatement().Initializer
	case ast.KindForInStatement, ast.KindForOfStatement:
		loopInitializer = node.AsForInOrOfStatement().Initializer
	}
	savedBlockScopes := a.blockScopes
	var loopScope navigationBlockScope
	var loopSnapshots map[string]navigationDeclarationSnapshot
	if loopInitializer != nil && loopInitializer.Kind == ast.KindVariableDeclarationList && loopInitializer.Flags&ast.NodeFlagsBlockScoped != 0 {
		loopScope = navigationLoopBlockScope(loopInitializer, a.work)
		loopSnapshots = a.snapshotNavigationDeclarations(loopScope)
		a.blockScopes = append(copyNavigationBlockScopes(savedBlockScopes), loopScope)
	}
	defer func() {
		if loopSnapshots != nil {
			a.restoreNavigationDeclarations(loopScope, loopSnapshots)
		}
		a.blockScopes = savedBlockScopes
	}()
	switch node.Kind {
	case ast.KindDoStatement:
		statement := node.AsDoStatement()
		base = a.navigationState()
		body = statement.Statement
	case ast.KindWhileStatement:
		statement := node.AsWhileStatement()
		if statement.Expression != nil {
			endExpressionScope := a.beginNavigationExpressionScope()
			if _, err := a.visit(statement.Expression); err != nil {
				endExpressionScope()
				return navigationFlow{}, err
			}
			if a.currentFunction == nil {
				a.evaluateNavigationExpressionEffects(statement.Expression)
			}
			endExpressionScope()
			if err := a.checkContext(); err != nil {
				return navigationFlow{}, err
			}
		}
		base = a.navigationState()
		body = statement.Statement
	case ast.KindForStatement:
		statement := node.AsForStatement()
		if err := a.visitForInitializer(statement.Initializer); err != nil {
			return navigationFlow{}, err
		}
		if statement.Condition != nil {
			endExpressionScope := a.beginNavigationExpressionScope()
			if _, err := a.visit(statement.Condition); err != nil {
				endExpressionScope()
				return navigationFlow{}, err
			}
			if a.currentFunction == nil {
				a.evaluateNavigationExpressionEffects(statement.Condition)
			}
			endExpressionScope()
			if err := a.checkContext(); err != nil {
				return navigationFlow{}, err
			}
		}
		base = a.navigationState()
		body = statement.Statement
		if body != nil {
			a.restoreNavigationState(cloneNavigationState(base))
			bodyFlow, err := a.visit(body)
			if err != nil {
				return navigationFlow{}, err
			}
			bodyState := a.navigationState()
			states := []navigationState{base}
			if bodyFlow.normal {
				if statement.Incrementor != nil {
					endExpressionScope := a.beginNavigationExpressionScope()
					if _, err := a.visit(statement.Incrementor); err != nil {
						endExpressionScope()
						return navigationFlow{}, err
					}
					if a.currentFunction == nil {
						a.evaluateNavigationExpressionEffects(statement.Incrementor)
					}
					endExpressionScope()
					if err := a.checkContext(); err != nil {
						return navigationFlow{}, err
					}
					bodyState = a.navigationState()
				}
				states = append(states, bodyState)
			} else if bodyFlow.breaks || bodyFlow.continues {
				states = append(states, bodyState)
			}
			merged := a.mergeNavigationStates(states...)
			merged = a.addLoopUnknownValues(base, merged)
			a.restoreNavigationState(merged)
			bodyFlow.breaks = false
			bodyFlow.continues = false
			bodyFlow.normal = true
			return bodyFlow, nil
		}
	case ast.KindForInStatement, ast.KindForOfStatement:
		statement := node.AsForInOrOfStatement()
		if err := a.visitForInitializer(statement.Initializer); err != nil {
			return navigationFlow{}, err
		}
		if statement.Expression != nil {
			if _, err := a.visit(statement.Expression); err != nil {
				return navigationFlow{}, err
			}
		}
		base = a.navigationState()
		body = statement.Statement
	}
	if body == nil {
		return normalNavigationFlow(), nil
	}
	a.restoreNavigationState(cloneNavigationState(base))
	bodyFlow, err := a.visit(body)
	if err != nil {
		return navigationFlow{}, err
	}
	bodyState := a.navigationState()
	if isDo && node.AsDoStatement().Expression != nil {
		endExpressionScope := a.beginNavigationExpressionScope()
		if _, err := a.visit(node.AsDoStatement().Expression); err != nil {
			endExpressionScope()
			return navigationFlow{}, err
		}
		if a.currentFunction == nil {
			a.evaluateNavigationExpressionEffects(node.AsDoStatement().Expression)
		}
		endExpressionScope()
		if err := a.checkContext(); err != nil {
			return navigationFlow{}, err
		}
		bodyState = a.navigationState()
	}
	states := make([]navigationState, 0, 2)
	if !isDo {
		states = append(states, base)
	}
	if bodyFlow.normal || bodyFlow.breaks || bodyFlow.continues {
		states = append(states, bodyState)
	}
	if len(states) == 0 {
		a.restoreNavigationState(cloneNavigationState(base))
	} else {
		merged := a.mergeNavigationStates(states...)
		merged = a.addLoopUnknownValues(base, merged)
		a.restoreNavigationState(merged)
	}
	bodyFlow.breaks = false
	bodyFlow.continues = false
	bodyFlow.normal = len(states) > 0
	return bodyFlow, nil
}

func (a *navigationAnalyzer) visitForInitializer(node *ast.Node) error {
	if node == nil {
		return nil
	}
	endExpressionScope := a.beginNavigationExpressionScope()
	defer endExpressionScope()
	if node.Kind != ast.KindVariableDeclarationList {
		_, err := a.visit(node)
		return err
	}
	declarationList := node.AsVariableDeclarationList()
	if declarationList.Declarations == nil {
		return nil
	}
	for _, declaration := range declarationList.Declarations.Nodes {
		name := declaration.Name()
		if name != nil && name.Kind == ast.KindIdentifier {
			a.registerNavigationInitializer(name.Text(), declaration.Initializer())
		}
		a.registerNavigationAssignments(declaration.Initializer())
	}
	a.activateNavigationDeclarations(node)
	a.refreshNavigationFunctionEnvironments()
	return nil
}

func isNavigationFunction(node *ast.Node) bool {
	switch node.Kind {
	case ast.KindFunctionDeclaration, ast.KindFunctionExpression, ast.KindArrowFunction,
		ast.KindMethodDeclaration, ast.KindGetAccessor, ast.KindSetAccessor:
		return true
	default:
		return false
	}
}

func (a *navigationAnalyzer) navigationFunctionParameterTokens(function *ast.Node) []string {
	if tokens, ok := a.functionParameterTokens[function]; ok {
		return tokens
	}
	parameters := function.Parameters()
	tokens := make([]string, len(parameters))
	for index := range parameters {
		a.nextParameterToken++
		tokens[index] = "\x00asp-navigation-parameter:" + strconv.Itoa(a.nextParameterToken) + "\x00"
	}
	a.functionParameterTokens[function] = tokens
	return tokens
}

func (a *navigationAnalyzer) bindNavigationFunctionParameters(function *ast.Node) {
	tokens := a.navigationFunctionParameterTokens(function)
	for index, parameter := range function.Parameters() {
		if parameter == nil || index >= len(tokens) {
			continue
		}
		name := parameter.Name()
		if name == nil || name.Kind != ast.KindIdentifier {
			continue
		}
		a.values[name.Text()] = navigationFiniteSet{
			values:       []navigationCandidate{unknownNavigationCandidate()},
			unknown:      true,
			dependencies: []string{tokens[index]},
		}
		delete(a.bindings, name.Text())
		delete(a.functionScopes[len(a.functionScopes)-1].shadowed, name.Text())
	}
}

func (a *navigationAnalyzer) recordNavigationFunctionParameterDependencies(function *ast.Node, arguments []navigationFiniteSet) {
	tokens := a.navigationFunctionParameterTokens(function)
	for index, token := range tokens {
		a.observedParameters[token] = true
		if index < len(arguments) {
			a.parameterDependencies[token] = mergeNavigationDependencies(a.parameterDependencies[token], arguments[index].dependencies)
		}
	}
}

func (a *navigationAnalyzer) resolveNavigationFunctionParameterDependencies() error {
	knownTokens := make(map[string]struct{}, len(a.parameterDependencies))
	for _, tokens := range a.functionParameterTokens {
		for _, token := range tokens {
			knownTokens[token] = struct{}{}
		}
	}
	fallbackDependencies := []string(nil)
	if a.navigationWorkExhausted() {
		fallbackDependencies = navigationDependencyMarkers(a.source)
	}
	processed := 0
	for sinkIndex := range a.sinks {
		for valueIndex := range a.sinks[sinkIndex].Expression.Values {
			dependencies := []string(nil)
			for _, dependency := range a.sinks[sinkIndex].Expression.Values[valueIndex].Dependencies {
				processed++
				if processed%int(navigationEvaluationWorkCheckInterval) == 0 {
					if err := a.checkContext(); err != nil {
						return err
					}
				}
				if _, ok := knownTokens[dependency]; !ok {
					dependencies = mergeNavigationDependencies(dependencies, []string{dependency})
					continue
				}
				resolved := a.parameterDependencies[dependency]
				if !a.observedParameters[dependency] {
					resolved = mergeNavigationDependencies(resolved, fallbackDependencies)
				}
				dependencies = mergeNavigationDependencies(dependencies, resolved)
			}
			a.sinks[sinkIndex].Expression.Values[valueIndex].Dependencies = dependencies
		}
	}
	return a.checkContext()
}

func cloneNavigationBindings(bindings map[string]*ast.Node, budgets ...*navigationWorkBudget) map[string]*ast.Node {
	work := navigationCloneBudget(budgets)
	if work != nil && !work.consume(uint64(len(bindings)+1)) {
		return map[string]*ast.Node{}
	}
	cloned := make(map[string]*ast.Node, len(bindings))
	for name, expr := range bindings {
		if work != nil && !work.consume(1) {
			return map[string]*ast.Node{}
		}
		cloned[name] = expr
	}
	return cloned
}

func cloneNavigationValues(values map[string]navigationFiniteSet, budgets ...*navigationWorkBudget) map[string]navigationFiniteSet {
	work := navigationCloneBudget(budgets)
	if work != nil && !work.consume(uint64(len(values)+1)) {
		return map[string]navigationFiniteSet{}
	}
	cloned := make(map[string]navigationFiniteSet, len(values))
	for name, value := range values {
		if work != nil && !work.consume(1) {
			return map[string]navigationFiniteSet{}
		}
		value = cloneNavigationFiniteSet(value, work)
		cloned[name] = value
	}
	return cloned
}

func cloneNavigationFiniteSets(values map[string]navigationFiniteSet, budgets ...*navigationWorkBudget) map[string]navigationFiniteSet {
	work := navigationCloneBudget(budgets)
	if work != nil && !work.consume(uint64(len(values)+1)) {
		return map[string]navigationFiniteSet{}
	}
	cloned := make(map[string]navigationFiniteSet, len(values))
	for name, value := range values {
		if work != nil && !work.consume(1) {
			return map[string]navigationFiniteSet{}
		}
		value = cloneNavigationFiniteSet(value, work)
		cloned[name] = value
	}
	return cloned
}

func cloneNavigationState(state navigationState) navigationState {
	work := state.work
	formMethods := cloneNavigationFiniteSets(state.formMethods, work)
	formActionValues := cloneNavigationFiniteSets(state.formActionValues, work)
	return navigationState{
		bindings:         cloneNavigationBindings(state.bindings, work),
		values:           cloneNavigationValues(state.values, work),
		formActions:      cloneNavigationBindings(state.formActions, work),
		formActionValues: formActionValues,
		formMethods:      formMethods,
		work:             state.work,
	}
}

func (a *navigationAnalyzer) navigationState() navigationState {
	formMethods := cloneNavigationFiniteSets(a.formMethods, a.work)
	formActionValues := cloneNavigationFiniteSets(a.formActionValues, a.work)
	return navigationState{
		bindings:         cloneNavigationBindings(a.bindings, a.work),
		values:           cloneNavigationValues(a.values, a.work),
		formActions:      cloneNavigationBindings(a.formActions, a.work),
		formActionValues: formActionValues,
		formMethods:      formMethods,
		work:             a.work,
	}
}

func (a *navigationAnalyzer) restoreNavigationState(state navigationState) {
	a.bindings = state.bindings
	a.values = state.values
	a.formActions = state.formActions
	a.formActionValues = state.formActionValues
	a.formMethods = state.formMethods
}

func (a *navigationAnalyzer) materializeNavigationState(state navigationState) navigationState {
	state = cloneNavigationState(state)
	previousBindings := a.bindings
	previousValues := a.values
	previousEvaluating := a.evaluating
	a.bindings = state.bindings
	a.values = state.values
	a.evaluating = map[string]bool{}
	defer func() {
		a.bindings = previousBindings
		a.values = previousValues
		a.evaluating = previousEvaluating
	}()

	if !a.consumeNavigationWork(uint64(len(state.bindings) + len(state.values) + 1)) {
		return state
	}
	names := make(map[string]struct{}, len(state.bindings)+len(state.values))
	for name := range state.bindings {
		if !a.consumeNavigationWork(1) {
			return state
		}
		names[name] = struct{}{}
	}
	for name := range state.values {
		if !a.consumeNavigationWork(1) {
			return state
		}
		names[name] = struct{}{}
	}
	for name := range names {
		if !a.consumeNavigationWork(1) {
			return state
		}
		if err := a.checkContext(); err != nil {
			return state
		}
		state.values[name] = a.evaluateIdentifier(name)
		delete(state.bindings, name)
	}

	return state
}

func (a *navigationAnalyzer) mergeNavigationStates(states ...navigationState) navigationState {
	if len(states) == 0 {
		return navigationState{
			bindings:         map[string]*ast.Node{},
			values:           map[string]navigationFiniteSet{},
			formActions:      map[string]*ast.Node{},
			formActionValues: map[string]navigationFiniteSet{},
			formMethods:      map[string]navigationFiniteSet{},
			work:             a.work,
		}
	}
	if len(states) == 1 {
		return cloneNavigationState(states[0])
	}
	if !a.consumeNavigationWork(uint64(len(states))) {
		return cloneNavigationState(states[0])
	}

	materialized := make([]navigationState, len(states))
	names := map[string]struct{}{}
	formNames := map[string]struct{}{}
	formMethodNames := map[string]struct{}{}
	for index, state := range states {
		if err := a.checkContext(); err != nil {
			return cloneNavigationState(states[0])
		}
		materialized[index] = a.materializeNavigationState(state)
		for name := range materialized[index].values {
			if !a.consumeNavigationWork(1) {
				return materialized[0]
			}
			names[name] = struct{}{}
		}
		for name := range materialized[index].formActions {
			if !a.consumeNavigationWork(1) {
				return materialized[0]
			}
			formNames[name] = struct{}{}
		}
		for name := range materialized[index].formMethods {
			if !a.consumeNavigationWork(1) {
				return materialized[0]
			}
			formMethodNames[name] = struct{}{}
		}
	}
	if !a.consumeNavigationWork(uint64(len(names) + len(formNames) + len(formMethodNames) + 1)) {
		return materialized[0]
	}

	merged := navigationState{
		bindings:         map[string]*ast.Node{},
		values:           make(map[string]navigationFiniteSet, len(names)),
		formActions:      make(map[string]*ast.Node, len(formNames)),
		formActionValues: make(map[string]navigationFiniteSet, len(formNames)),
		formMethods:      make(map[string]navigationFiniteSet, len(formMethodNames)),
		work:             a.work,
	}
	for name := range names {
		if !a.consumeNavigationWork(uint64(len(materialized) + 1)) {
			return materialized[0]
		}
		if err := a.checkContext(); err != nil {
			return materialized[0]
		}
		value := navigationFiniteSet{}
		for _, state := range materialized {
			if !a.consumeNavigationWork(1) {
				return materialized[0]
			}
			candidate, ok := state.values[name]
			if !ok {
				value.merge(unknownNavigationSet())
				continue
			}
			value.merge(candidate)
		}
		if len(value.values) == 0 {
			value = unknownNavigationSet()
		}
		merged.values[name] = value
	}
	for name := range formMethodNames {
		if !a.consumeNavigationWork(uint64(len(materialized) + 1)) {
			return materialized[0]
		}
		method := navigationFiniteSet{}
		for _, state := range materialized {
			if !a.consumeNavigationWork(1) {
				return materialized[0]
			}
			candidate, ok := state.formMethods[name]
			if !ok {
				candidate = literalNavigationSet("GET")
			}
			method.merge(candidate)
		}
		if len(method.values) == 0 {
			method = unknownNavigationSet()
		}
		merged.formMethods[name] = method
	}
	for name := range formNames {
		if !a.consumeNavigationWork(uint64(len(materialized) + 1)) {
			return materialized[0]
		}
		var expression *ast.Node
		consistent := true
		actionValue := navigationFiniteSet{}
		for index, state := range materialized {
			if !a.consumeNavigationWork(1) {
				return materialized[0]
			}
			candidate, ok := state.formActions[name]
			if !ok {
				consistent = false
			} else if index == 0 {
				expression = candidate
			} else if candidate != expression {
				consistent = false
			}
			value, ok := state.formActionValues[name]
			if !ok {
				value = unknownNavigationSet()
			}
			actionValue.merge(value)
		}
		if len(actionValue.values) == 0 {
			actionValue = unknownNavigationSet()
		}
		merged.formActionValues[name] = actionValue
		if consistent {
			merged.formActions[name] = expression
		} else {
			// A form action with distinct branch expressions is intentionally
			// kept as an unknown marker for a later submit sink.
			merged.formActions[name] = nil
		}
	}
	return merged
}

func navigationFiniteSetsEqual(left, right navigationFiniteSet) bool {
	if left.unknown != right.unknown || len(left.values) != len(right.values) {
		return false
	}
	for _, candidate := range left.values {
		found := false
		for _, other := range right.values {
			if candidate.text == other.text && candidate.kind == other.kind {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func taintNavigationStateAlternatives(merged *navigationState, alternatives []navigationState, dependencies []string) {
	if merged == nil || len(alternatives) < 2 || len(dependencies) == 0 {
		return
	}
	dependencySet := navigationFiniteSet{dependencies: append([]string(nil), dependencies...)}
	taint := func(values map[string]navigationFiniteSet, selectValues func(navigationState) map[string]navigationFiniteSet) {
		for name, value := range values {
			firstValues := selectValues(alternatives[0])
			first, firstOK := firstValues[name]
			varies := false
			for _, alternative := range alternatives[1:] {
				other, otherOK := selectValues(alternative)[name]
				if firstOK != otherOK || firstOK && !navigationFiniteSetsEqual(first, other) {
					varies = true
					break
				}
			}
			if varies {
				value.mergeDependencies(dependencySet)
				values[name] = value
			}
		}
	}
	taint(merged.values, func(state navigationState) map[string]navigationFiniteSet { return state.values })
	taint(merged.formActionValues, func(state navigationState) map[string]navigationFiniteSet { return state.formActionValues })
	taint(merged.formMethods, func(state navigationState) map[string]navigationFiniteSet { return state.formMethods })
}

func taintNavigationSinkAlternatives(sinks []NavigationSink, dependencies []string) {
	if len(dependencies) == 0 {
		return
	}
	for sinkIndex := range sinks {
		values := sinks[sinkIndex].Expression.Values
		for valueIndex := range values {
			values[valueIndex].Dependencies = mergeNavigationDependencies(values[valueIndex].Dependencies, dependencies)
		}
		sinks[sinkIndex].Expression.Values = values
	}
}

func mergeNavigationDependencies(left, right []string) []string {
	merged := append([]string(nil), left...)
	seen := make(map[string]struct{}, len(left)+len(right))
	for _, dependency := range left {
		seen[dependency] = struct{}{}
	}
	for _, dependency := range right {
		if _, ok := seen[dependency]; ok {
			continue
		}
		seen[dependency] = struct{}{}
		merged = append(merged, dependency)
	}
	return merged
}

func (a *navigationAnalyzer) addLoopUnknownValues(base, merged navigationState) navigationState {
	base = a.materializeNavigationState(base)
	merged = a.materializeNavigationState(merged)
	if !a.consumeNavigationWork(uint64(len(base.values) + len(merged.values) + 1)) {
		return merged
	}
	names := make(map[string]struct{}, len(base.values)+len(merged.values))
	for name := range base.values {
		if !a.consumeNavigationWork(1) {
			return merged
		}
		names[name] = struct{}{}
	}
	for name := range merged.values {
		if !a.consumeNavigationWork(1) {
			return merged
		}
		names[name] = struct{}{}
	}
	if !a.consumeNavigationWork(uint64(len(names) + 1)) {
		return merged
	}
	for name := range names {
		if !a.consumeNavigationWork(1) {
			return merged
		}
		if err := a.checkContext(); err != nil {
			return merged
		}
		before, beforeOK := base.values[name]
		after, afterOK := merged.values[name]
		if beforeOK && afterOK && navigationFiniteSetsEqual(before, after) {
			continue
		}
		if !afterOK {
			after = navigationFiniteSet{}
		}
		after.merge(unknownNavigationSet())
		merged.values[name] = after
	}
	return merged
}

func (a *navigationAnalyzer) walk() error {
	scope := a.rootScope
	snapshots := a.snapshotNavigationDeclarations(scope)
	savedBlockScopes := a.blockScopes
	savedFunctionBlockBoundary := a.functionBlockBoundary
	savedRootScopeActive := a.rootScopeActive
	a.blockScopes = append(copyNavigationBlockScopes(savedBlockScopes), scope)
	a.functionBlockBoundary = 0
	a.rootScopeActive = true
	defer func() {
		a.restoreNavigationDeclarations(scope, snapshots)
		a.blockScopes = savedBlockScopes
		a.functionBlockBoundary = savedFunctionBlockBoundary
		a.rootScopeActive = savedRootScopeActive
	}()
	for _, statement := range a.file.Statements.Nodes {
		flow, err := a.visit(statement)
		if err != nil {
			return err
		}
		if statement.Kind != ast.KindBlock {
			a.globalState = a.navigationState()
		}
		if a.navigationWorkExhausted() {
			break
		}
		if !flow.normal {
			break
		}
	}
	return a.checkContext()
}

func (a *navigationAnalyzer) registerVariableStatement(node *ast.Node) {
	declarationList := node.AsVariableStatement().DeclarationList
	if declarationList == nil {
		return
	}
	for _, declaration := range declarationList.AsVariableDeclarationList().Declarations.Nodes {
		name := declaration.Name()
		if name == nil || name.Kind != ast.KindIdentifier {
			continue
		}
		a.registerNavigationInitializer(name.Text(), declaration.Initializer())
	}
}

func (a *navigationAnalyzer) registerNavigationInitializer(name string, initializer *ast.Node) {
	if initializer == nil {
		a.bindings[name] = nil
		delete(a.values, name)
		return
	}
	initializer = ast.SkipOuterExpressions(initializer, ast.OEKAll)
	if isNavigationCallable(initializer) {
		a.captureNavigationFunctionEnvironment(initializer)
		a.bindings[name] = initializer
		delete(a.values, name)
		return
	}
	a.values[name] = a.evaluateExpression(initializer)
	delete(a.bindings, name)
}

func (a *navigationAnalyzer) registerAssignment(node *ast.BinaryExpression) {
	if node == nil {
		return
	}
	_ = a.evaluateExpression(node.AsNode())
}

func (a *navigationAnalyzer) evaluateIdentifier(name string) navigationFiniteSet {
	if a.navigationNameShadowed(name) {
		return unknownNavigationSet()
	}
	if value, ok := a.values[name]; ok {
		return value
	}
	if a.evaluating[name] {
		return unknownNavigationSet()
	}
	expr, ok := a.bindings[name]
	if !ok || expr == nil {
		return unknownNavigationSet()
	}
	a.evaluating[name] = true
	value := a.evaluateExpression(expr)
	delete(a.evaluating, name)
	return value
}

func (a *navigationAnalyzer) evaluateCallExpression(expr *ast.Node) navigationFiniteSet {
	if err := a.checkContext(); err != nil {
		return unknownNavigationSet()
	}
	if expr == nil || expr.Kind != ast.KindCallExpression {
		return unknownNavigationSet()
	}
	call := expr.AsCallExpression()
	path := a.navigationExpressionPath(call.Expression)
	if call.Arguments == nil {
		return unknownNavigationSet()
	}
	// JavaScript evaluates the call target, including a member receiver or a
	// computed property key, before it evaluates any argument. Keep this
	// separate from function lookup: member calls remain unsupported here, but
	// their receiver/key effects are still observable.
	callTarget := a.evaluateExpression(call.Expression)
	if err := a.checkContext(); err != nil {
		return unknownNavigationSet()
	}
	var function *ast.Node
	var ok bool
	direct := a.skipNavigationOuterExpressions(call.Expression)
	if isNavigationCallable(direct) {
		function, ok = direct, true
		a.captureNavigationFunctionEnvironment(function)
	}
	if len(path) == 1 {
		// Resolve the callable now. Argument expressions run after the
		// callee has been selected and must not replace it retroactively.
		function, ok = a.lookupNavigationFunction(path[0])
	}
	argumentSets := make([]navigationFiniteSet, 0, len(call.Arguments.Nodes))
	for _, argument := range call.Arguments.Nodes {
		if !a.consumeNavigationWork(1) {
			return unknownNavigationSet()
		}
		if err := a.checkContext(); err != nil {
			return unknownNavigationSet()
		}
		if argument == nil {
			return unknownNavigationSet()
		}
		if argument.Kind == ast.KindSpreadElement {
			argumentSets = append(argumentSets, a.evaluateExpression(argument.Expression()))
			continue
		}
		argumentSets = append(argumentSets, a.evaluateExpression(argument))
	}
	unknownCallResult := func() navigationFiniteSet {
		result := unknownNavigationSet()
		result.mergeDependencies(callTarget)
		for _, argument := range argumentSets {
			result.mergeDependencies(argument)
		}
		return result
	}
	if len(path) != 1 && !ok {
		// The call target is unsupported, but JavaScript still evaluates every
		// argument before entering the external call.
		return unknownCallResult()
	}
	if !ok {
		return unknownCallResult()
	}
	previousCallSite := a.callSite
	previousTransparent := a.transparentFunction
	if direct == function && a.callSite == nil {
		// An immediately invoked lexical scope has no meaningful wrapper
		// location. Keep direct sinks and nested calls at their own source.
		a.transparentFunction = function
	} else if a.callSite == nil && (a.currentFunction == nil || a.currentFunction == a.transparentFunction) {
		a.callSite = expr
	}
	defer func() { a.callSite, a.transparentFunction = previousCallSite, previousTransparent }()
	a.recordNavigationFunctionParameterDependencies(function, argumentSets)
	if !a.consumeNavigationWork(uint64(len(argumentSets) + 1)) {
		return unknownNavigationSet()
	}
	argumentState := a.navigationState()
	globalState := cloneNavigationState(a.globalState)
	result := navigationFiniteSet{}
	resultStates := make([]navigationState, 0)
	resultGlobalStates := make([]navigationState, 0)
	arguments := make([]navigationFiniteSet, len(argumentSets))
	combinations := 0
	truncated := false
	var visit func(int)
	visit = func(index int) {
		if err := a.checkContext(); err != nil {
			truncated = true
			return
		}
		if !a.consumeNavigationWork(1) {
			truncated = true
			return
		}
		if truncated {
			return
		}
		if index == len(argumentSets) {
			combinations++
			a.restoreNavigationState(cloneNavigationState(argumentState))
			a.globalState = cloneNavigationState(globalState)
			callResult := a.evaluateNavigationFunction(function, arguments)
			result.merge(callResult.value)
			if !callResult.executed || len(callResult.states) == 0 {
				projected, projectedGlobal := a.projectUnknownNavigationFunctionState(function, callResult.env, argumentState, globalState)
				if !a.consumeNavigationWork(1) {
					truncated = true
					return
				}
				resultStates = append(resultStates, projected)
				if !a.consumeNavigationWork(1) {
					truncated = true
					return
				}
				resultGlobalStates = append(resultGlobalStates, projectedGlobal)
			} else {
				for _, state := range callResult.states {
					if !a.consumeNavigationWork(1) {
						truncated = true
						return
					}
					callGlobal := callResult.global
					if len(callGlobal.values) == 0 && len(callGlobal.bindings) == 0 && len(callGlobal.formActions) == 0 && len(callGlobal.formActionValues) == 0 && len(callGlobal.formMethods) == 0 {
						callGlobal = globalState
					}
					projected, projectedGlobal := a.projectNavigationFunctionState(function, callResult.env, state, argumentState, callGlobal)
					if !a.consumeNavigationWork(1) {
						truncated = true
						return
					}
					resultStates = append(resultStates, projected)
					if !a.consumeNavigationWork(1) {
						truncated = true
						return
					}
					resultGlobalStates = append(resultGlobalStates, projectedGlobal)
				}
			}
			if combinations >= navigationValueLimit {
				truncated = true
			}
			return
		}
		set := argumentSets[index]
		for _, candidate := range set.values {
			if !a.consumeNavigationWork(1) {
				truncated = true
				return
			}
			if err := a.checkContext(); err != nil {
				truncated = true
				return
			}
			arguments[index] = navigationFiniteSet{values: []navigationCandidate{candidate}, dependencies: append([]string(nil), set.dependencies...)}
			visit(index + 1)
			if truncated {
				return
			}
		}
		if set.unknown {
			unknown := false
			for _, candidate := range set.values {
				if !a.consumeNavigationWork(1) {
					truncated = true
					return
				}
				if candidate.kind == NavigationValueUnknown {
					unknown = true
					break
				}
			}
			if !unknown {
				arguments[index] = navigationFiniteSet{values: []navigationCandidate{{text: "{unknown}", kind: NavigationValueUnknown}}, dependencies: append([]string(nil), set.dependencies...)}
				visit(index + 1)
			}
		}
	}
	visit(0)
	if truncated {
		unknownEnvironment := a.navigationFunctionEnvironment(function)
		projected, projectedGlobal := a.projectUnknownNavigationFunctionState(function, unknownEnvironment, argumentState, globalState)
		if !a.consumeNavigationWork(1) {
			return unknownNavigationSet()
		}
		resultStates = append(resultStates, projected)
		if !a.consumeNavigationWork(1) {
			return unknownNavigationSet()
		}
		resultGlobalStates = append(resultGlobalStates, projectedGlobal)
	}
	if len(resultStates) == 0 {
		if !a.consumeNavigationWork(1) {
			return unknownNavigationSet()
		}
		resultStates = append(resultStates, cloneNavigationState(argumentState))
		if !a.consumeNavigationWork(1) {
			return unknownNavigationSet()
		}
		resultGlobalStates = append(resultGlobalStates, cloneNavigationState(globalState))
	}
	a.restoreNavigationState(a.mergeNavigationStates(resultStates...))
	a.globalState = a.mergeNavigationStates(resultGlobalStates...)
	a.applyGlobalStateToCurrentState()
	if truncated {
		result.unknown = true
	}
	result.ensureUnknownCandidate()
	if len(result.values) == 0 {
		return unknownNavigationSet()
	}
	return result
}

func navigationScopeContainsName(scope navigationBlockScope, name string) bool {
	if _, ok := scope.functions[name]; ok {
		return true
	}
	if _, ok := scope.shadowed[name]; ok {
		return true
	}
	if _, ok := scope.declared[name]; ok {
		return true
	}
	if _, ok := scope.varDeclared[name]; ok {
		return true
	}
	return false
}

func navigationFunctionScopeContainsName(scope navigationFunctionScope, name string) bool {
	if _, ok := scope.functions[name]; ok {
		return true
	}
	if _, ok := scope.shadowed[name]; ok {
		return true
	}
	if _, ok := scope.varDeclared[name]; ok {
		return true
	}
	return false
}

func (a *navigationAnalyzer) navigationFunctionNameIsGlobal(environment navigationFunctionEnvironment, name string) bool {
	for index := len(environment.blockScopes) - 1; index > 0; index-- {
		if navigationScopeContainsName(environment.blockScopes[index], name) {
			return false
		}
	}
	for index := len(environment.functionScopes) - 1; index >= 0; index-- {
		if navigationFunctionScopeContainsName(environment.functionScopes[index], name) {
			return false
		}
	}
	if len(environment.blockScopes) > 0 && navigationScopeContainsName(environment.blockScopes[0], name) {
		return true
	}
	if navigationScopeContainsName(a.rootScope, name) {
		return true
	}
	return true
}

func (a *navigationAnalyzer) navigationCallerHasLocalName(name string) bool {
	boundary := a.functionBlockBoundary
	if boundary < 0 {
		boundary = 0
	}
	if boundary > len(a.blockScopes) {
		boundary = len(a.blockScopes)
	}
	start := boundary
	if a.currentFunction == nil && a.rootScopeActive && start == 0 && len(a.blockScopes) > 0 {
		start = 1
	}
	for index := len(a.blockScopes) - 1; index >= start; index-- {
		if navigationScopeContainsName(a.blockScopes[index], name) {
			return true
		}
	}
	if len(a.functionScopes) > 0 && navigationFunctionScopeContainsName(a.functionScopes[len(a.functionScopes)-1], name) {
		return true
	}
	return false
}

func (a *navigationAnalyzer) navigationCallerNameOwner(name string) *navigationScopeIdentity {
	boundary := a.functionBlockBoundary
	if boundary < 0 {
		boundary = 0
	}
	if boundary > len(a.blockScopes) {
		boundary = len(a.blockScopes)
	}
	for index := len(a.blockScopes) - 1; index >= boundary; index-- {
		if navigationScopeContainsName(a.blockScopes[index], name) {
			return a.blockScopes[index].identity
		}
	}
	if len(a.functionScopes) > 0 && navigationFunctionScopeContainsName(a.functionScopes[len(a.functionScopes)-1], name) {
		return a.functionScopes[len(a.functionScopes)-1].identity
	}
	for index := boundary - 1; index >= 0; index-- {
		if navigationScopeContainsName(a.blockScopes[index], name) {
			return a.blockScopes[index].identity
		}
	}
	for index := len(a.functionScopes) - 2; index >= 0; index-- {
		if navigationFunctionScopeContainsName(a.functionScopes[index], name) {
			return a.functionScopes[index].identity
		}
	}
	if navigationScopeContainsName(a.rootScope, name) {
		return a.rootScope.identity
	}
	return nil
}

func navigationFunctionNameOwner(environment navigationFunctionEnvironment, name string) *navigationScopeIdentity {
	for index := len(environment.blockScopes) - 1; index > 0; index-- {
		if navigationScopeContainsName(environment.blockScopes[index], name) {
			return environment.blockScopes[index].identity
		}
	}
	for index := len(environment.functionScopes) - 1; index >= 0; index-- {
		if navigationFunctionScopeContainsName(environment.functionScopes[index], name) {
			return environment.functionScopes[index].identity
		}
	}
	if len(environment.blockScopes) > 0 && navigationScopeContainsName(environment.blockScopes[0], name) {
		return environment.blockScopes[0].identity
	}
	return nil
}

func (a *navigationAnalyzer) updateGlobalStateFromNavigationState(state navigationState) {
	global := cloneNavigationState(a.globalState)
	names := navigationStateNames(state, a.work)
	addNavigationScopeNames(names, a.rootScope, a.work)
	for name := range names {
		if !a.consumeNavigationWork(1) {
			return
		}
		if a.navigationCallerHasLocalName(name) {
			continue
		}
		copyNavigationStateName(&global, state, name)
	}
	a.globalState = global
}

func (a *navigationAnalyzer) applyGlobalStateToCurrentState() {
	names := navigationStateNames(a.globalState, a.work)
	addNavigationScopeNames(names, a.rootScope, a.work)
	state := a.navigationState()
	for name := range names {
		if !a.consumeNavigationWork(1) {
			return
		}
		if a.navigationCallerHasLocalName(name) {
			continue
		}
		copyNavigationStateName(&state, a.globalState, name)
	}
	a.restoreNavigationState(state)
}

func copyNavigationStateName(target *navigationState, source navigationState, name string) {
	if value, ok := source.values[name]; ok {
		if target.work != nil && !target.work.consume(uint64(len(value.values)+1)) {
			return
		}
		value.values = append([]navigationCandidate(nil), value.values...)
		target.values[name] = value
		delete(target.bindings, name)
	} else if binding, ok := source.bindings[name]; ok {
		target.bindings[name] = binding
		delete(target.values, name)
	}
	if action, ok := source.formActions[name]; ok {
		target.formActions[name] = action
	}
	if value, ok := source.formActionValues[name]; ok {
		target.formActionValues[name] = cloneNavigationFiniteSet(value, target.work)
	}
	if method, ok := source.formMethods[name]; ok {
		if target.work != nil && !target.work.consume(uint64(len(method.values)+1)) {
			return
		}
		method.values = append([]navigationCandidate(nil), method.values...)
		target.formMethods[name] = method
	}
}

func (a *navigationAnalyzer) projectNavigationFunctionState(function *ast.Node, environment navigationFunctionEnvironment, finalState, callerState, callerGlobal navigationState) (navigationState, navigationState) {
	projected := cloneNavigationState(callerState)
	projectedGlobal := cloneNavigationState(callerGlobal)
	localNames := a.navigationFunctionLocalNames(function)
	names := navigationStateNames(environment.state, a.work)
	for name := range navigationStateNames(finalState, a.work) {
		if !a.consumeNavigationWork(1) {
			return projected, projectedGlobal
		}
		names[name] = struct{}{}
	}
	for name := range names {
		if !a.consumeNavigationWork(1) {
			return projected, projectedGlobal
		}
		if _, local := localNames[name]; local {
			if a.navigationFunctionNameIsGlobal(environment, name) {
				copyNavigationStateName(&projectedGlobal, callerGlobal, name)
			}
			continue
		}
		if a.navigationFunctionNameIsGlobal(environment, name) {
			copyNavigationStateName(&projectedGlobal, finalState, name)
			if navigationFunctionNameOwner(environment, name) == a.navigationCallerNameOwner(name) {
				copyNavigationStateName(&projected, finalState, name)
			}
			continue
		}
		if navigationFunctionNameOwner(environment, name) == a.navigationCallerNameOwner(name) {
			copyNavigationStateName(&projected, finalState, name)
		}
	}
	return projected, projectedGlobal
}

func (a *navigationAnalyzer) projectUnknownNavigationFunctionState(function *ast.Node, environment navigationFunctionEnvironment, callerState, callerGlobal navigationState) (navigationState, navigationState) {
	projected := cloneNavigationState(callerState)
	projectedGlobal := cloneNavigationState(callerGlobal)
	localNames := a.navigationFunctionLocalNames(function)
	names := navigationStateNames(environment.state, a.work)
	addNavigationScopeNames(names, a.rootScope, a.work)
	if !a.consumeNavigationWork(uint64(len(environment.state.formActions) + len(environment.state.formActionValues) + len(environment.state.formMethods) + len(callerState.formActions) + len(callerState.formActionValues) + len(callerState.formMethods) + len(callerGlobal.formActions) + len(callerGlobal.formActionValues) + len(callerGlobal.formMethods) + 1)) {
		return projected, projectedGlobal
	}
	formActionNames := map[string]struct{}{}
	formMethodNames := map[string]struct{}{}
	for name := range environment.state.formActions {
		if !a.consumeNavigationWork(1) {
			return projected, projectedGlobal
		}
		formActionNames[name] = struct{}{}
	}
	for name := range environment.state.formActionValues {
		if !a.consumeNavigationWork(1) {
			return projected, projectedGlobal
		}
		formActionNames[name] = struct{}{}
	}
	for name := range environment.state.formMethods {
		if !a.consumeNavigationWork(1) {
			return projected, projectedGlobal
		}
		formMethodNames[name] = struct{}{}
	}
	for name := range callerState.formActions {
		if !a.consumeNavigationWork(1) {
			return projected, projectedGlobal
		}
		formActionNames[name] = struct{}{}
	}
	for name := range callerState.formActionValues {
		if !a.consumeNavigationWork(1) {
			return projected, projectedGlobal
		}
		formActionNames[name] = struct{}{}
	}
	for name := range callerState.formMethods {
		if !a.consumeNavigationWork(1) {
			return projected, projectedGlobal
		}
		formMethodNames[name] = struct{}{}
	}
	for name := range callerGlobal.formActions {
		if !a.consumeNavigationWork(1) {
			return projected, projectedGlobal
		}
		formActionNames[name] = struct{}{}
	}
	for name := range callerGlobal.formActionValues {
		if !a.consumeNavigationWork(1) {
			return projected, projectedGlobal
		}
		formActionNames[name] = struct{}{}
	}
	for name := range callerGlobal.formMethods {
		if !a.consumeNavigationWork(1) {
			return projected, projectedGlobal
		}
		formMethodNames[name] = struct{}{}
	}
	for name := range names {
		if !a.consumeNavigationWork(1) {
			return projected, projectedGlobal
		}
		if _, local := localNames[name]; local {
			if a.navigationFunctionNameIsGlobal(environment, name) {
				copyNavigationStateName(&projectedGlobal, callerGlobal, name)
			}
			continue
		}
		unknown := unknownNavigationSet()
		if a.navigationFunctionNameIsGlobal(environment, name) {
			projectedGlobal.values[name] = unknown
			delete(projectedGlobal.bindings, name)
			if _, ok := formMethodNames[name]; ok {
				projectedGlobal.formMethods[name] = unknown
			}
			if _, ok := formActionNames[name]; ok {
				projectedGlobal.formActions[name] = nil
				projectedGlobal.formActionValues[name] = unknown
			}
			if navigationFunctionNameOwner(environment, name) == a.navigationCallerNameOwner(name) {
				projected.values[name] = unknown
				delete(projected.bindings, name)
				if _, ok := formMethodNames[name]; ok {
					projected.formMethods[name] = unknown
				}
				if _, ok := formActionNames[name]; ok {
					projected.formActions[name] = nil
					projected.formActionValues[name] = unknown
				}
			}
			continue
		}
		if navigationFunctionNameOwner(environment, name) == a.navigationCallerNameOwner(name) {
			projected.values[name] = unknown
			delete(projected.bindings, name)
			if _, ok := formMethodNames[name]; ok {
				projected.formMethods[name] = unknown
			}
			if _, ok := formActionNames[name]; ok {
				projected.formActions[name] = nil
				projected.formActionValues[name] = unknown
			}
		}
	}
	return projected, projectedGlobal
}

func (a *navigationAnalyzer) navigationFunctionEnvironment(function *ast.Node) navigationFunctionEnvironment {
	if function != nil {
		if captured, ok := a.functionEnvironments[function]; ok {
			return cloneNavigationFunctionEnvironment(captured)
		}
	}
	return navigationFunctionEnvironment{
		state:       cloneNavigationState(a.globalState),
		blockScopes: []navigationBlockScope{cloneNavigationBlockScope(a.rootScope)},
		rootActive:  true,
		work:        a.work,
	}
}

func (a *navigationAnalyzer) evaluateNavigationFunction(function *ast.Node, arguments []navigationFiniteSet) navigationCallResult {
	if err := a.checkContext(); err != nil {
		environment := a.navigationFunctionEnvironment(function)
		return navigationCallResult{value: unknownNavigationSet(), global: cloneNavigationState(a.globalState), env: environment}
	}
	environment := a.navigationFunctionEnvironment(function)
	if function == nil || !isNavigationCallable(function) || a.callDepth >= navigationCallDepthLimit || a.callStack[function] {
		return navigationCallResult{value: unknownNavigationSet(), global: cloneNavigationState(a.globalState), env: environment}
	}
	endExpressionScope := a.beginNavigationExpressionScope()
	defer endExpressionScope()
	previousBindings := a.bindings
	previousValues := a.values
	previousFormActions := a.formActions
	previousFormActionValues := a.formActionValues
	previousFormMethods := a.formMethods
	previousFunctionScopes := a.functionScopes
	previousBlockScopes := a.blockScopes
	previousFunctionBlockBoundary := a.functionBlockBoundary
	previousRootScopeActive := a.rootScopeActive
	previousCurrentFunction := a.currentFunction
	previousEvaluating := a.evaluating
	previousDepth := a.callDepth
	a.bindings = cloneNavigationBindings(environment.state.bindings, a.work)
	a.values = cloneNavigationValues(environment.state.values, a.work)
	a.formActions = cloneNavigationBindings(environment.state.formActions, a.work)
	a.formActionValues = cloneNavigationFiniteSets(environment.state.formActionValues, a.work)
	a.formMethods = cloneNavigationFiniteSets(environment.state.formMethods, a.work)
	a.functionScopes = append(copyNavigationFunctionScopes(environment.functionScopes), a.navigationFunctionScope(function))
	a.blockScopes = environment.blockScopes
	a.functionBlockBoundary = len(environment.blockScopes)
	a.rootScopeActive = environment.rootActive
	a.currentFunction = function
	a.evaluating = map[string]bool{}
	a.callDepth++
	a.callStack[function] = true
	defer func() {
		delete(a.callStack, function)
		a.callDepth = previousDepth
		a.bindings = previousBindings
		a.values = previousValues
		a.formActions = previousFormActions
		a.formActionValues = previousFormActionValues
		a.formMethods = previousFormMethods
		a.functionScopes = previousFunctionScopes
		a.blockScopes = previousBlockScopes
		a.functionBlockBoundary = previousFunctionBlockBoundary
		a.rootScopeActive = previousRootScopeActive
		a.currentFunction = previousCurrentFunction
		a.evaluating = previousEvaluating
	}()

	for index, parameter := range function.Parameters() {
		if !a.consumeNavigationWork(1) {
			return navigationCallResult{value: unknownNavigationSet(), global: cloneNavigationState(a.globalState), env: environment}
		}
		if err := a.checkContext(); err != nil {
			return navigationCallResult{value: unknownNavigationSet(), global: cloneNavigationState(a.globalState), env: environment}
		}
		name := parameter.Name()
		if name == nil || name.Kind != ast.KindIdentifier {
			continue
		}
		if index < len(arguments) {
			a.values[name.Text()] = arguments[index]
			delete(a.bindings, name.Text())
			delete(a.functionScopes[len(a.functionScopes)-1].shadowed, name.Text())
			continue
		}
		if initializer := parameter.AsParameterDeclaration().Initializer; initializer != nil {
			a.values[name.Text()] = a.evaluateExpression(initializer)
		} else {
			a.values[name.Text()] = unknownNavigationSet()
		}
		delete(a.bindings, name.Text())
		delete(a.functionScopes[len(a.functionScopes)-1].shadowed, name.Text())
	}
	body := function.Body()
	if body == nil {
		return navigationCallResult{value: unknownNavigationSet(), global: cloneNavigationState(a.globalState), env: environment, executed: true}
	}
	if err := a.checkContext(); err != nil {
		return navigationCallResult{value: unknownNavigationSet(), global: cloneNavigationState(a.globalState), env: environment}
	}
	if body.Kind != ast.KindBlock {
		value := a.evaluateExpression(body)
		return navigationCallResult{value: value, states: []navigationState{a.navigationState()}, global: cloneNavigationState(a.globalState), env: environment, executed: true}
	}
	scope := a.navigationBlockScope(body)
	snapshots := a.snapshotNavigationDeclarations(scope)
	previousBlocks := a.blockScopes
	a.blockScopes = append(copyNavigationBlockScopes(previousBlocks), scope)
	execution := a.evaluateNavigationStatements(body.AsBlock().Statements.Nodes, a.navigationState())
	a.restoreNavigationState(execution.state)
	a.restoreNavigationDeclarations(scope, snapshots)
	execution.state = a.navigationState()
	a.blockScopes = previousBlocks
	if !execution.hasReturn {
		states := make([]navigationState, 0, 2)
		if !a.appendNavigationStates(&states, execution.effectStates) {
			return navigationCallResult{value: unknownNavigationSet(), states: states, global: cloneNavigationState(a.globalState), env: environment}
		}
		if execution.hasNormal {
			if !a.consumeNavigationWork(1) {
				return navigationCallResult{value: unknownNavigationSet(), states: states, global: cloneNavigationState(a.globalState), env: environment}
			}
			states = append(states, execution.state)
		}
		return navigationCallResult{value: unknownNavigationSet(), states: states, global: cloneNavigationState(a.globalState), env: environment, executed: true}
	}
	result := execution.returns
	if execution.hasNormal || execution.unknown {
		result.merge(unknownNavigationSet())
	}
	if len(result.values) == 0 {
		result = unknownNavigationSet()
	}
	states := make([]navigationState, 0, 2)
	if !a.appendNavigationStates(&states, execution.effectStates) {
		return navigationCallResult{value: unknownNavigationSet(), states: states, global: cloneNavigationState(a.globalState), env: environment}
	}
	if execution.hasNormal {
		if !a.consumeNavigationWork(1) {
			return navigationCallResult{value: unknownNavigationSet(), states: states, global: cloneNavigationState(a.globalState), env: environment}
		}
		states = append(states, execution.state)
	}
	return navigationCallResult{value: result, states: states, global: cloneNavigationState(a.globalState), env: environment, executed: true}
}

func (a *navigationAnalyzer) evaluateNavigationStatements(statements []*ast.Node, initial navigationState) navigationExecution {
	result := navigationExecution{state: cloneNavigationState(initial), hasNormal: true}
	for _, statement := range statements {
		if err := a.checkContext(); err != nil {
			result.unknown = true
			return result
		}
		if !result.hasNormal {
			break
		}
		step := a.evaluateNavigationStatement(statement, result.state)
		if err := a.checkContext(); err != nil {
			result.unknown = true
			return result
		}
		if a.navigationWorkExhausted() {
			result.unknown = true
			return result
		}
		result.returns.merge(step.returns)
		result.hasReturn = result.hasReturn || step.hasReturn
		result.hasBreak = result.hasBreak || step.hasBreak
		result.hasContinue = result.hasContinue || step.hasContinue
		result.unknown = result.unknown || step.unknown
		if !a.appendNavigationStates(&result.effectStates, step.effectStates) {
			result.unknown = true
			return result
		}
		if step.hasNormal {
			result.state = step.state
		} else {
			if len(step.effectStates) == 0 {
				if !a.consumeNavigationWork(1) {
					result.unknown = true
					return result
				}
				result.effectStates = append(result.effectStates, step.state)
			}
			result.hasNormal = false
		}
	}
	return result
}

func (a *navigationAnalyzer) evaluateNavigationStatement(node *ast.Node, initial navigationState) navigationExecution {
	if err := a.checkContext(); err != nil {
		return navigationExecution{state: cloneNavigationState(initial), unknown: true}
	}
	if !a.consumeNavigationWork(1) {
		return navigationExecution{state: cloneNavigationState(initial), hasNormal: true, unknown: true}
	}
	endExpressionScope := a.beginNavigationExpressionScope()
	defer endExpressionScope()
	if node == nil {
		return navigationExecution{state: cloneNavigationState(initial), hasNormal: true}
	}
	previous := a.navigationState()
	a.restoreNavigationState(cloneNavigationState(initial))
	defer a.restoreNavigationState(previous)

	var result navigationExecution
	switch node.Kind {
	case ast.KindBlock:
		scope := a.navigationBlockScope(node)
		snapshots := a.snapshotNavigationDeclarations(scope)
		previousBlocks := a.blockScopes
		a.blockScopes = append(copyNavigationBlockScopes(previousBlocks), scope)
		result := a.evaluateNavigationStatements(node.AsBlock().Statements.Nodes, initial)
		a.restoreNavigationState(result.state)
		a.restoreNavigationDeclarations(scope, snapshots)
		result.state = a.navigationState()
		a.blockScopes = previousBlocks
		return result
	case ast.KindVariableStatement:
		a.registerVariableStatement(node)
		a.activateNavigationDeclarations(node)
		a.refreshNavigationFunctionEnvironments()
		result = navigationExecution{state: a.navigationState(), hasNormal: true}
	case ast.KindExpressionStatement:
		expression := node.AsExpressionStatement().Expression
		a.registerNavigationAssignments(expression)
		a.evaluateNavigationExpressionEffects(expression)
		result = navigationExecution{state: a.navigationState(), hasNormal: true}
	case ast.KindReturnStatement:
		expression := node.AsReturnStatement().Expression
		a.registerNavigationAssignments(expression)
		value := a.evaluateExpression(expression)
		state := a.navigationState()
		result = navigationExecution{
			state:        state,
			effectStates: []navigationState{state},
			hasReturn:    true,
			returns:      value,
		}
	case ast.KindIfStatement:
		return a.evaluateNavigationIfStatement(node, initial)
	case ast.KindSwitchStatement:
		return a.evaluateNavigationSwitchStatement(node, initial)
	case ast.KindDoStatement, ast.KindWhileStatement, ast.KindForStatement,
		ast.KindForInStatement, ast.KindForOfStatement:
		return a.evaluateNavigationLoopStatement(node, initial)
	case ast.KindCaseClause, ast.KindDefaultClause:
		clause := node.AsCaseOrDefaultClause()
		a.registerNavigationAssignments(clause.Expression)
		result = a.evaluateNavigationStatements(clause.Statements.Nodes, a.navigationState())
	case ast.KindFunctionDeclaration, ast.KindFunctionExpression, ast.KindArrowFunction:
		// Function declarations do not execute while evaluating the containing
		// function body; their definitions are indexed separately.
		a.captureNavigationFunctionEnvironment(node)
		result = navigationExecution{state: a.navigationState(), hasNormal: true}
	case ast.KindEmptyStatement, ast.KindDebuggerStatement:
		result = navigationExecution{state: a.navigationState(), hasNormal: true}
	case ast.KindBreakStatement:
		result = navigationExecution{state: a.navigationState(), effectStates: []navigationState{a.navigationState()}, hasBreak: true}
	case ast.KindContinueStatement:
		result = navigationExecution{state: a.navigationState(), effectStates: []navigationState{a.navigationState()}, hasContinue: true}
	default:
		a.registerNavigationAssignments(node)
		result = navigationExecution{state: a.navigationState(), hasNormal: true, unknown: true}
	}
	return result
}

func (a *navigationAnalyzer) evaluateNavigationIfStatement(node *ast.Node, initial navigationState) navigationExecution {
	if err := a.checkContext(); err != nil {
		return navigationExecution{state: cloneNavigationState(initial), unknown: true}
	}
	statement := node.AsIfStatement()
	a.registerNavigationAssignments(statement.Expression)
	a.evaluateNavigationExpressionEffects(statement.Expression)
	condition := a.evaluateExpression(statement.Expression)
	sinkStart := len(a.sinks)
	defer func() { taintNavigationSinkAlternatives(a.sinks[sinkStart:], condition.dependencies) }()
	base := a.navigationState()
	baseGlobal := cloneNavigationState(a.globalState)
	branches := make([]navigationExecution, 0, 2)
	globalStates := make([]navigationState, 0, 2)
	if statement.ThenStatement != nil {
		if err := a.checkContext(); err != nil {
			return navigationExecution{state: cloneNavigationState(initial), unknown: true}
		}
		a.globalState = cloneNavigationState(baseGlobal)
		branches = append(branches, a.evaluateNavigationStatement(statement.ThenStatement, base))
		globalStates = append(globalStates, cloneNavigationState(a.globalState))
	} else {
		branches = append(branches, navigationExecution{state: cloneNavigationState(base), hasNormal: true})
		globalStates = append(globalStates, cloneNavigationState(baseGlobal))
	}
	if statement.ElseStatement == nil {
		branches = append(branches, navigationExecution{state: cloneNavigationState(base), hasNormal: true})
		globalStates = append(globalStates, cloneNavigationState(baseGlobal))
	} else {
		if err := a.checkContext(); err != nil {
			return navigationExecution{state: cloneNavigationState(initial), unknown: true}
		}
		a.globalState = cloneNavigationState(baseGlobal)
		branches = append(branches, a.evaluateNavigationStatement(statement.ElseStatement, base))
		globalStates = append(globalStates, cloneNavigationState(a.globalState))
	}
	a.globalState = a.mergeNavigationStates(globalStates...)
	return a.mergeNavigationExecutions(branches...)
}

func (a *navigationAnalyzer) evaluateNavigationSwitchStatement(node *ast.Node, initial navigationState) navigationExecution {
	if err := a.checkContext(); err != nil {
		return navigationExecution{state: cloneNavigationState(initial), unknown: true}
	}
	statement := node.AsSwitchStatement()
	a.registerNavigationAssignments(statement.Expression)
	a.evaluateNavigationExpressionEffects(statement.Expression)
	condition := a.evaluateExpression(statement.Expression)
	sinkStart := len(a.sinks)
	defer func() { taintNavigationSinkAlternatives(a.sinks[sinkStart:], condition.dependencies) }()
	base := a.navigationState()
	var clauses []*ast.Node
	if statement.CaseBlock != nil && statement.CaseBlock.AsCaseBlock().Clauses != nil {
		clauses = statement.CaseBlock.AsCaseBlock().Clauses.Nodes
	}
	if !a.consumeNavigationWork(uint64(len(clauses))) {
		return navigationExecution{state: cloneNavigationState(initial), hasNormal: true, unknown: true}
	}
	starts := navigationSwitchPathStarts(clauses)
	for allocation := 0; allocation < 4; allocation++ {
		if !a.consumeNavigationWork(uint64(len(starts))) {
			return navigationExecution{state: cloneNavigationState(initial), hasNormal: true, unknown: true}
		}
	}
	branches := make([]navigationExecution, 0, len(starts))
	baseGlobal := cloneNavigationState(a.globalState)
	globalStates := make([]navigationState, 0, len(starts))
	savedBlockScopes := a.blockScopes
	savedFunctionScopes := a.functionScopes
	switchScope := a.navigationSwitchScope(clauses)
	snapshots := a.snapshotNavigationDeclarations(switchScope)
	blockPaths := make([][]navigationBlockScope, 0, len(starts))
	functionPaths := make([][]navigationFunctionScope, 0, len(starts))
	defer func() {
		a.blockScopes = savedBlockScopes
		a.functionScopes = savedFunctionScopes
	}()
	for _, start := range starts {
		if !a.consumeNavigationWork(1) {
			if err := a.checkContext(); err != nil {
				return navigationExecution{state: cloneNavigationState(initial), unknown: true}
			}
			break
		}
		if err := a.checkContext(); err != nil {
			return navigationExecution{state: cloneNavigationState(initial), unknown: true}
		}
		a.blockScopes = append(cloneNavigationBlockScopes(savedBlockScopes), cloneNavigationBlockScope(switchScope))
		a.functionScopes = cloneNavigationFunctionScopes(savedFunctionScopes)
		a.globalState = cloneNavigationState(baseGlobal)
		branch := a.evaluateNavigationSwitchPath(clauses, start, base)
		if !a.consumeNavigationWork(1) {
			break
		}
		branches = append(branches, branch)
		globalStates = append(globalStates, cloneNavigationState(a.globalState))
		if a.navigationWorkExhausted() {
			break
		}
		if branch.hasNormal {
			if !a.consumeNavigationWork(1) {
				break
			}
			blockPaths = append(blockPaths, cloneNavigationBlockScopes(a.blockScopes))
			if !a.consumeNavigationWork(1) {
				break
			}
			functionPaths = append(functionPaths, cloneNavigationFunctionScopes(a.functionScopes))
		}
	}
	result := a.mergeNavigationExecutions(branches...)
	a.globalState = a.mergeNavigationStates(globalStates...)
	a.restoreNavigationState(result.state)
	mergeNavigationCallableScopes(savedBlockScopes, blockPaths, savedFunctionScopes, functionPaths, a.work)
	a.restoreNavigationDeclarations(switchScope, snapshots)
	result.state = a.navigationState()
	return result
}

func (a *navigationAnalyzer) evaluateNavigationSwitchPath(clauses []*ast.Node, start int, initial navigationState) navigationExecution {
	if err := a.checkContext(); err != nil {
		return navigationExecution{state: cloneNavigationState(initial), unknown: true}
	}
	endExpressionScope := a.beginNavigationExpressionScope()
	defer endExpressionScope()
	previous := a.navigationState()
	defer a.restoreNavigationState(previous)

	state := cloneNavigationState(initial)
	result := navigationExecution{state: state, hasNormal: true}
	matchingEnd := start
	if matchingEnd < 0 || (matchingEnd < len(clauses) && clauses[matchingEnd] != nil && clauses[matchingEnd].Kind == ast.KindDefaultClause) {
		matchingEnd = len(clauses) - 1
	}
	for index := 0; index <= matchingEnd && index < len(clauses); index++ {
		if !a.consumeNavigationWork(1) {
			return navigationExecution{state: cloneNavigationState(initial), hasNormal: true, unknown: true}
		}
		if err := a.checkContext(); err != nil {
			return navigationExecution{state: cloneNavigationState(initial), unknown: true}
		}
		clause := clauses[index]
		if clause == nil {
			continue
		}
		a.restoreNavigationState(cloneNavigationState(state))
		caseClause := clause.AsCaseOrDefaultClause()
		if caseClause.Expression != nil {
			a.registerNavigationAssignments(caseClause.Expression)
			a.evaluateNavigationExpressionEffects(caseClause.Expression)
			state = a.navigationState()
		}
	}
	if start < 0 {
		result.state = state
		result.hasBreak = false
		return result
	}
	for index := start; index < len(clauses); index++ {
		if !a.consumeNavigationWork(1) {
			return navigationExecution{state: cloneNavigationState(initial), hasNormal: true, unknown: true}
		}
		if err := a.checkContext(); err != nil {
			return navigationExecution{state: cloneNavigationState(initial), unknown: true}
		}
		clause := clauses[index]
		if clause == nil {
			continue
		}
		a.restoreNavigationState(cloneNavigationState(state))
		caseClause := clause.AsCaseOrDefaultClause()
		if caseClause.Statements == nil {
			continue
		}
		for _, statement := range caseClause.Statements.Nodes {
			if err := a.checkContext(); err != nil {
				return navigationExecution{state: cloneNavigationState(initial), unknown: true}
			}
			step := a.evaluateNavigationStatement(statement, state)
			result.returns.merge(step.returns)
			result.hasReturn = result.hasReturn || step.hasReturn
			result.hasBreak = result.hasBreak || step.hasBreak
			result.hasContinue = result.hasContinue || step.hasContinue
			result.unknown = result.unknown || step.unknown
			if !a.appendNavigationStates(&result.effectStates, step.effectStates) {
				result.unknown = true
				return result
			}
			if !step.hasNormal {
				result.state = step.state
				if step.hasBreak {
					// break exits this switch and is normal to the enclosing
					// statement sequence; other abrupt paths still propagate.
					result.hasNormal = true
					result.hasBreak = false
				} else {
					if len(step.effectStates) == 0 {
						if !a.consumeNavigationWork(1) {
							result.unknown = true
							return result
						}
						result.effectStates = append(result.effectStates, step.state)
					}
					result.hasNormal = false
					result.hasBreak = false
				}
				return result
			}
			state = step.state
		}
	}
	result.state = state
	result.hasBreak = false
	return result
}

func (a *navigationAnalyzer) evaluateNavigationLoopStatement(node *ast.Node, initial navigationState) navigationExecution {
	if err := a.checkContext(); err != nil {
		return navigationExecution{state: cloneNavigationState(initial), unknown: true}
	}
	base := cloneNavigationState(initial)
	var body *ast.Node
	var incrementor *ast.Node
	var condition *ast.Node
	isDo := node.Kind == ast.KindDoStatement
	var loopInitializer *ast.Node
	switch node.Kind {
	case ast.KindForStatement:
		loopInitializer = node.AsForStatement().Initializer
	case ast.KindForInStatement, ast.KindForOfStatement:
		loopInitializer = node.AsForInOrOfStatement().Initializer
	}
	previousBlocks := a.blockScopes
	var loopScope navigationBlockScope
	var loopSnapshots map[string]navigationDeclarationSnapshot
	if loopInitializer != nil && loopInitializer.Kind == ast.KindVariableDeclarationList && loopInitializer.Flags&ast.NodeFlagsBlockScoped != 0 {
		loopScope = navigationLoopBlockScope(loopInitializer, a.work)
		loopSnapshots = a.snapshotNavigationDeclarations(loopScope)
		a.blockScopes = append(copyNavigationBlockScopes(previousBlocks), loopScope)
	}
	defer func() {
		if loopSnapshots != nil {
			a.restoreNavigationDeclarations(loopScope, loopSnapshots)
		}
		a.blockScopes = previousBlocks
	}()
	switch node.Kind {
	case ast.KindDoStatement:
		body = node.AsDoStatement().Statement
	case ast.KindWhileStatement:
		statement := node.AsWhileStatement()
		condition = statement.Expression
		a.registerNavigationAssignments(statement.Expression)
		a.evaluateNavigationExpressionEffects(statement.Expression)
		base = a.navigationState()
		body = statement.Statement
	case ast.KindForStatement:
		statement := node.AsForStatement()
		condition = statement.Condition
		a.registerNavigationForInitializer(statement.Initializer)
		a.registerNavigationAssignments(statement.Condition)
		a.evaluateNavigationExpressionEffects(statement.Condition)
		base = a.navigationState()
		body = statement.Statement
		incrementor = statement.Incrementor
	case ast.KindForInStatement, ast.KindForOfStatement:
		statement := node.AsForInOrOfStatement()
		condition = statement.Expression
		a.registerNavigationForInitializer(statement.Initializer)
		a.registerNavigationAssignments(statement.Expression)
		a.evaluateNavigationExpressionEffects(statement.Expression)
		base = a.navigationState()
		body = statement.Statement
	}

	result := navigationExecution{unknown: true}
	if body == nil {
		if isDo {
			result.state = base
			return result
		}
		result.state = base
		result.hasNormal = true
		return result
	}
	conditionValue := navigationFiniteSet{}
	if condition != nil {
		conditionValue = a.evaluateExpression(condition)
	}
	sinkStart := len(a.sinks)
	defer func() { taintNavigationSinkAlternatives(a.sinks[sinkStart:], conditionValue.dependencies) }()
	bodyResult := a.evaluateNavigationStatement(body, base)
	result.returns = bodyResult.returns
	result.hasReturn = bodyResult.hasReturn
	result.hasBreak = bodyResult.hasBreak
	result.hasContinue = bodyResult.hasContinue
	if !a.appendNavigationStates(&result.effectStates, bodyResult.effectStates) {
		result.unknown = true
		result.state = base
		return result
	}
	result.unknown = true
	normalStates := make([]navigationState, 0, 2)
	if !isDo {
		normalStates = append(normalStates, base)
	}
	if bodyResult.hasNormal {
		bodyState := bodyResult.state
		if incrementor != nil {
			a.restoreNavigationState(cloneNavigationState(bodyState))
			a.registerNavigationAssignments(incrementor)
			a.evaluateNavigationExpressionEffects(incrementor)
			bodyState = a.navigationState()
		}
		if isDo {
			// A do/while body executes before its condition, whose assignment
			// effects are conservatively included in the same one-pass state.
			if node.AsDoStatement().Expression != nil {
				a.restoreNavigationState(cloneNavigationState(bodyState))
				a.registerNavigationAssignments(node.AsDoStatement().Expression)
				a.evaluateNavigationExpressionEffects(node.AsDoStatement().Expression)
				bodyState = a.navigationState()
			}
		}
		normalStates = append(normalStates, bodyState)
	} else if bodyResult.hasBreak || bodyResult.hasContinue {
		// break exits the loop and continue may eventually reach its exit;
		// both provide a normal path after the loop.
		normalStates = append(normalStates, bodyResult.state)
	}
	if len(normalStates) > 0 {
		result.state = a.mergeNavigationStates(normalStates...)
		result.state = a.addLoopUnknownValues(base, result.state)
		result.hasNormal = true
	} else {
		result.state = cloneNavigationState(base)
	}
	result.hasBreak = false
	result.hasContinue = false
	return result
}

func (a *navigationAnalyzer) registerNavigationForInitializer(node *ast.Node) {
	if node == nil {
		return
	}
	if node.Kind == ast.KindVariableDeclarationList {
		declarationList := node.AsVariableDeclarationList()
		if declarationList.Declarations == nil {
			return
		}
		for _, declaration := range declarationList.Declarations.Nodes {
			if !a.consumeNavigationWork(1) {
				return
			}
			name := declaration.Name()
			if name != nil && name.Kind == ast.KindIdentifier {
				a.registerNavigationInitializer(name.Text(), declaration.Initializer())
			}
			a.registerNavigationAssignments(declaration.Initializer())
		}
		a.activateNavigationDeclarations(node)
		a.refreshNavigationFunctionEnvironments()
		return
	}
	a.registerNavigationAssignments(node)
}

func (a *navigationAnalyzer) mergeNavigationExecutions(executions ...navigationExecution) navigationExecution {
	result := navigationExecution{}
	if !a.consumeNavigationWork(uint64(len(executions))) {
		result.unknown = true
		if len(executions) > 0 {
			result.state = cloneNavigationState(executions[0].state)
		}
		return result
	}
	normalStates := make([]navigationState, 0, len(executions))
	for _, execution := range executions {
		if !a.consumeNavigationWork(1) {
			result.unknown = true
			return result
		}
		if err := a.checkContext(); err != nil {
			result.unknown = true
			return result
		}
		result.returns.merge(execution.returns)
		result.hasReturn = result.hasReturn || execution.hasReturn
		result.hasBreak = result.hasBreak || execution.hasBreak
		result.hasContinue = result.hasContinue || execution.hasContinue
		result.unknown = result.unknown || execution.unknown
		if !a.appendNavigationStates(&result.effectStates, execution.effectStates) {
			result.unknown = true
			return result
		}
		if execution.hasNormal {
			if !a.consumeNavigationWork(1) {
				result.unknown = true
				return result
			}
			normalStates = append(normalStates, execution.state)
		}
	}
	if len(normalStates) > 0 {
		result.state = a.mergeNavigationStates(normalStates...)
		result.hasNormal = true
	} else if len(executions) > 0 {
		result.state = cloneNavigationState(executions[0].state)
	}
	return result
}

func (a *navigationAnalyzer) registerNavigationAssignments(node *ast.Node) {
	if node == nil {
		return
	}
	if isNavigationFunction(node) {
		return
	}
	_ = a.evaluateExpression(node)
}

func (a *navigationAnalyzer) evaluateNavigationExpressionEffects(node *ast.Node) {
	if err := a.checkContext(); err != nil {
		return
	}
	if node == nil || isNavigationFunction(node) {
		return
	}
	node = ast.SkipOuterExpressions(node, ast.OEKAll)
	_ = a.evaluateExpression(node)
}

func (a *navigationAnalyzer) evaluateAssignmentExpression(node *ast.Node) navigationFiniteSet {
	if node == nil || node.Kind != ast.KindBinaryExpression {
		return unknownNavigationSet()
	}
	if a.navigationExpressionEffectWasEvaluated(node) {
		if binary := node.AsBinaryExpression(); binary.Left != nil && binary.Left.Kind == ast.KindIdentifier {
			return a.evaluateIdentifier(binary.Left.Text())
		}
		return unknownNavigationSet()
	}
	binary := node.AsBinaryExpression()
	if binary.OperatorToken == nil || binary.Right == nil {
		a.markNavigationExpressionEffect(node)
		return unknownNavigationSet()
	}
	if binary.Left == nil || binary.Left.Kind != ast.KindIdentifier {
		value := a.evaluateExpression(binary.Right)
		a.registerNavigationFormActionAssignment(binary)
		a.registerNavigationFormMethodAssignment(binary)
		a.markNavigationExpressionEffect(node)
		return value
	}

	name := binary.Left.Text()
	var value navigationFiniteSet
	switch binary.OperatorToken.Kind {
	case ast.KindEqualsToken:
		initializer := ast.SkipOuterExpressions(binary.Right, ast.OEKAll)
		if isNavigationCallable(initializer) {
			a.captureNavigationFunctionEnvironment(initializer)
			a.bindings[name] = initializer
			delete(a.values, name)
			value = unknownNavigationSet()
		} else {
			value = a.evaluateExpression(binary.Right)
			a.values[name] = value
			delete(a.bindings, name)
		}
	case ast.KindPlusEqualsToken:
		left := a.evaluateIdentifier(name)
		right := a.evaluateExpression(binary.Right)
		value = concatenateNavigationSets(left, right)
		a.values[name] = value
		delete(a.bindings, name)
	default:
		// Logical assignment operators use the same path-sensitive expression
		// evaluator as their non-assignment forms. Keep the target conservative
		// when its branch condition cannot be classified.
		left := a.evaluateIdentifier(name)
		preservePossible, assignPossible := false, false
		base := a.navigationState()
		baseGlobal := cloneNavigationState(a.globalState)
		states := make([]navigationState, 0, 2)
		globals := make([]navigationState, 0, 2)
		result := navigationFiniteSet{}
		result.mergeDependencies(left)
		var preserved navigationFiniteSet
		switch binary.OperatorToken.Kind {
		case ast.KindAmpersandAmpersandEqualsToken:
			canFalse, canTrue := navigationSetMayBeTruthy(left)
			preservePossible, assignPossible = canFalse, canTrue
			preserved = filterNavigationSetByTruthiness(left, false)
		case ast.KindBarBarEqualsToken:
			canFalse, canTrue := navigationSetMayBeTruthy(left)
			preservePossible, assignPossible = canTrue, canFalse
			preserved = filterNavigationSetByTruthiness(left, true)
		case ast.KindQuestionQuestionEqualsToken:
			canNullish, canNonNullish := navigationSetMayBeNullish(left)
			preservePossible, assignPossible = canNonNullish, canNullish
			preserved = filterNavigationSetByNullish(left, false)
		}
		if preservePossible {
			a.restoreNavigationState(cloneNavigationState(base))
			a.globalState = cloneNavigationState(baseGlobal)
			result.merge(preserved)
			states = append(states, a.navigationState())
			globals = append(globals, cloneNavigationState(a.globalState))
		}
		if assignPossible {
			a.restoreNavigationState(cloneNavigationState(base))
			a.globalState = cloneNavigationState(baseGlobal)
			assigned := a.evaluateExpression(binary.Right)
			assigned.mergeDependencies(left)
			result.merge(assigned)
			a.values[name] = assigned
			delete(a.bindings, name)
			states = append(states, a.navigationState())
			globals = append(globals, cloneNavigationState(a.globalState))
		}
		if len(states) > 0 {
			a.restoreNavigationState(a.mergeNavigationStates(states...))
			a.globalState = a.mergeNavigationStates(globals...)
		} else {
			a.restoreNavigationState(base)
			a.globalState = baseGlobal
		}
		value = result
	}
	a.activateNavigationFunctionAssignment(binary)
	a.registerNavigationFormActionAssignment(binary)
	a.registerNavigationFormMethodAssignment(binary)
	a.refreshNavigationFunctionEnvironments()
	a.markNavigationExpressionEffect(node)
	return value
}

func (a *navigationAnalyzer) evaluateExpression(expr *ast.Node) navigationFiniteSet {
	if expr == nil {
		return unknownNavigationSet()
	}
	expr = a.skipNavigationOuterExpressions(expr)
	if expr == nil {
		return unknownNavigationSet()
	}
	if value, ok := a.cachedNavigationExpression(expr); ok {
		return value
	}
	if !a.consumeNavigationWork(1) {
		return unknownNavigationSet()
	}
	value := a.evaluateExpressionUncached(expr)
	a.cacheNavigationExpression(expr, value)
	if (a.callSite != nil || a.currentFunction != nil && a.currentFunction == a.transparentFunction) && (expr.Kind == ast.KindBinaryExpression || expr.Kind == ast.KindCallExpression) {
		if sink, ok := a.navigationSink(expr); ok {
			// Resolve inside the callee but report the outer invocation, where its
			// arguments and the executing page are known.
			if a.callSite != nil {
				sink.Range = a.statementRange(a.callSite)
				sink.Expression.Range = a.expressionRange(a.callSite)
			}
			sink.Snippet = a.sourceText(sink.Range)
			sink.called = true
			a.sinks = append(a.sinks, sink)
		}
	}
	return value
}

func (a *navigationAnalyzer) evaluateExpressionUncached(expr *ast.Node) navigationFiniteSet {
	if err := a.checkContext(); err != nil {
		return unknownNavigationSet()
	}
	switch expr.Kind {
	case ast.KindStringLiteral:
		return literalNavigationSet(expr.Text())
	case ast.KindNoSubstitutionTemplateLiteral:
		return literalNavigationSet(expr.Text())
	case ast.KindTrueKeyword:
		return booleanNavigationSet(true)
	case ast.KindFalseKeyword:
		return booleanNavigationSet(false)
	case ast.KindNullKeyword:
		return nullNavigationSet("null")
	case ast.KindNumericLiteral, ast.KindBigIntLiteral:
		if result, ok := evaluateNavigationScalarResult(expr); ok {
			return navigationFiniteSet{values: []navigationCandidate{{
				text:    evaluator.AnyToString(result.Value),
				kind:    NavigationValueLiteral,
				truth:   navigationTruthinessFromScalar(result.Value),
				nullish: navigationNullishNo,
			}}}
		}
		return unknownNavigationSet()
	case ast.KindRegularExpressionLiteral:
		result := unknownNavigationSet()
		result.dependencies = navigationRegexDependencyMarkers(expr.Text())
		return result
	case ast.KindIdentifier:
		return a.evaluateIdentifier(expr.Text())
	case ast.KindCallExpression:
		value := a.evaluateCallExpression(expr)
		a.markNavigationExpressionEffect(expr)
		return value
	case ast.KindNewExpression:
		newExpression := expr.AsNewExpression()
		// JavaScript evaluates the constructor expression before any
		// arguments, even when the constructor itself is unsupported by the
		// finite evaluator. Preserve those observable effects before walking
		// the argument list left-to-right.
		if newExpression.Expression != nil {
			_ = a.evaluateExpression(newExpression.Expression)
		}
		if newExpression.Arguments != nil {
			for _, argument := range newExpression.Arguments.Nodes {
				if err := a.checkContext(); err != nil {
					return unknownNavigationSet()
				}
				if argument != nil && argument.Kind == ast.KindSpreadElement {
					_ = a.evaluateExpression(argument.Expression())
				} else {
					_ = a.evaluateExpression(argument)
				}
			}
		}
		a.markNavigationExpressionEffect(expr)
		return unknownNavigationSet()
	case ast.KindBinaryExpression:
		binary := expr.AsBinaryExpression()
		if binary.OperatorToken != nil {
			switch binary.OperatorToken.Kind {
			case ast.KindCommaToken:
				// The comma operator evaluates both operands left-to-right and
				// yields the right operand.
				a.evaluateExpression(binary.Left)
				return a.evaluateExpression(binary.Right)
			case ast.KindEqualsToken, ast.KindPlusEqualsToken,
				ast.KindAmpersandAmpersandEqualsToken, ast.KindBarBarEqualsToken,
				ast.KindQuestionQuestionEqualsToken:
				return a.evaluateAssignmentExpression(expr)
			case ast.KindPlusToken:
				if result, ok := evaluateNavigationScalarResult(expr); ok {
					return navigationFiniteSet{values: []navigationCandidate{{
						text:    evaluator.AnyToString(result.Value),
						kind:    NavigationValueLiteral,
						truth:   navigationTruthinessFromScalar(result.Value),
						nullish: navigationNullishNo,
					}}}
				}
				return concatenateNavigationSets(a.evaluateExpression(binary.Left), a.evaluateExpression(binary.Right))
			case ast.KindAmpersandAmpersandToken, ast.KindBarBarToken, ast.KindQuestionQuestionToken:
				return a.evaluateLogicalExpression(binary)
			}
		}
	case ast.KindPropertyAccessExpression:
		// A property read can execute the receiver expression (for example,
		// getObject().method). The property itself is not resolved by this
		// finite evaluator, so preserve the conservative unknown result.
		receiver := a.evaluateExpression(expr.AsPropertyAccessExpression().Expression)
		result := unknownNavigationSet()
		result.mergeDependencies(receiver)
		return result
	case ast.KindElementAccessExpression:
		// Computed member access evaluates its receiver and property key in
		// that order, even when the resulting member is unsupported.
		element := expr.AsElementAccessExpression()
		receiver := a.evaluateExpression(element.Expression)
		argument := a.evaluateExpression(element.ArgumentExpression)
		result := unknownNavigationSet()
		result.mergeDependencies(receiver)
		result.mergeDependencies(argument)
		return result
	case ast.KindConditionalExpression:
		conditional := expr.AsConditionalExpression()
		return a.evaluateConditionalExpression(conditional.Condition, conditional.WhenTrue, conditional.WhenFalse)
	case ast.KindTemplateExpression:
		result := literalNavigationSet(expr.AsTemplateExpression().Head.Text())
		for _, span := range expr.AsTemplateExpression().TemplateSpans.Nodes {
			if err := a.checkContext(); err != nil {
				return unknownNavigationSet()
			}
			result = concatenateNavigationSets(result, a.evaluateExpression(span.AsTemplateSpan().Expression))
			result = concatenateNavigationSets(result, literalNavigationSet(span.AsTemplateSpan().Literal.Text()))
		}
		for index := range result.values {
			if result.values[index].kind == NavigationValueLiteral {
				result.values[index].kind = NavigationValueTemplate
			}
		}
		return result
	}
	if result, ok := evaluateNavigationScalarResult(expr); ok {
		return navigationFiniteSet{values: []navigationCandidate{{
			text:    evaluator.AnyToString(result.Value),
			kind:    NavigationValueLiteral,
			truth:   navigationTruthinessFromScalar(result.Value),
			nullish: navigationNullishNo,
		}}}
	}
	return unknownNavigationSet()
}

func (a *navigationAnalyzer) evaluateConditionalExpression(condition, whenTrue, whenFalse *ast.Node) navigationFiniteSet {
	conditionValue := a.evaluateExpression(condition)
	base := a.navigationState()
	baseGlobal := cloneNavigationState(a.globalState)
	canFalse, canTrue := navigationSetMayBeTruthy(conditionValue)
	values := navigationFiniteSet{}
	values.mergeDependencies(conditionValue)
	states := make([]navigationState, 0, 2)
	globals := make([]navigationState, 0, 2)
	// Keep both candidate values for conservative navigation reporting. Only
	// truthiness-compatible arms contribute effects to the merged state; an
	// incompatible arm is evaluated against a snapshot solely to retain its
	// finite output candidate without leaking side effects.
	a.restoreNavigationState(cloneNavigationState(base))
	a.globalState = cloneNavigationState(baseGlobal)
	trueValue := a.evaluateExpression(whenTrue)
	values.merge(trueValue)
	if canTrue {
		states = append(states, a.navigationState())
		globals = append(globals, cloneNavigationState(a.globalState))
	}
	a.restoreNavigationState(cloneNavigationState(base))
	a.globalState = cloneNavigationState(baseGlobal)
	falseValue := a.evaluateExpression(whenFalse)
	values.merge(falseValue)
	if canFalse {
		states = append(states, a.navigationState())
		globals = append(globals, cloneNavigationState(a.globalState))
	}
	if len(states) == 0 {
		return unknownNavigationSet()
	}
	a.restoreNavigationState(a.mergeNavigationStates(states...))
	a.globalState = a.mergeNavigationStates(globals...)
	return values
}

func (a *navigationAnalyzer) evaluateLogicalExpression(binary *ast.BinaryExpression) navigationFiniteSet {
	if binary == nil || binary.OperatorToken == nil {
		return unknownNavigationSet()
	}
	left := a.evaluateExpression(binary.Left)
	base := a.navigationState()
	baseGlobal := cloneNavigationState(a.globalState)
	values := navigationFiniteSet{}
	values.mergeDependencies(left)
	states := make([]navigationState, 0, 2)
	globals := make([]navigationState, 0, 2)
	var preserve, evaluateRight bool
	var preserved navigationFiniteSet
	switch binary.OperatorToken.Kind {
	case ast.KindAmpersandAmpersandToken:
		canFalse, canTrue := navigationSetMayBeTruthy(left)
		preserve, evaluateRight = canFalse, canTrue
		preserved = filterNavigationSetByTruthiness(left, false)
	case ast.KindBarBarToken:
		canFalse, canTrue := navigationSetMayBeTruthy(left)
		preserve, evaluateRight = canTrue, canFalse
		preserved = filterNavigationSetByTruthiness(left, true)
	case ast.KindQuestionQuestionToken:
		canNullish, canNonNullish := navigationSetMayBeNullish(left)
		preserve, evaluateRight = canNonNullish, canNullish
		preserved = filterNavigationSetByNullish(left, false)
	default:
		return unknownNavigationSet()
	}
	if preserve {
		a.restoreNavigationState(cloneNavigationState(base))
		a.globalState = cloneNavigationState(baseGlobal)
		values.merge(preserved)
		states = append(states, a.navigationState())
		globals = append(globals, cloneNavigationState(a.globalState))
	}
	if evaluateRight {
		a.restoreNavigationState(cloneNavigationState(base))
		a.globalState = cloneNavigationState(baseGlobal)
		values.merge(a.evaluateExpression(binary.Right))
		states = append(states, a.navigationState())
		globals = append(globals, cloneNavigationState(a.globalState))
	}
	if len(states) == 0 {
		return unknownNavigationSet()
	}
	a.restoreNavigationState(a.mergeNavigationStates(states...))
	a.globalState = a.mergeNavigationStates(globals...)
	return values
}

func evaluateNavigationScalarResult(expr *ast.Node) (result evaluator.Result, ok bool) {
	if expr == nil {
		return evaluator.Result{}, false
	}
	// TypeScript-Go's evaluator already handles JavaScript string/number
	// coercion and escape decoding. The binding-aware finite evaluator above
	// handles aliases and conditionals first, then uses this for scalar forms.
	defer func() {
		if recover() != nil {
			result, ok = evaluator.Result{}, false
		}
	}()
	evaluate := evaluator.NewEvaluator(func(*ast.Node, *ast.Node) evaluator.Result {
		return evaluator.Result{}
	}, ast.OEKAll)
	result = evaluate(expr, expr)
	if result.Value == nil {
		return evaluator.Result{}, false
	}
	return result, true
}

func navigationTruthinessFromScalar(value any) navigationTruthiness {
	if evaluator.IsTruthy(value) {
		return navigationTruthinessTrue
	}
	return navigationTruthinessFalse
}

func (a *navigationAnalyzer) navigationExpressionPath(node *ast.Node) []string {
	if node == nil {
		return nil
	}
	path := make([]string, 0, 4)
	for depth := 0; node != nil; depth++ {
		if depth >= navigationExpressionPathLimit {
			return nil
		}
		if err := a.checkContext(); err != nil {
			return nil
		}
		if !a.collectingUnknownFallback && !a.consumeNavigationWork(1) {
			return nil
		}
		node = a.skipNavigationOuterExpressions(node)
		if node == nil {
			return nil
		}
		switch node.Kind {
		case ast.KindIdentifier:
			path = append(path, node.Text())
			if !a.reverseNavigationStrings(path) {
				return nil
			}
			return path
		case ast.KindPropertyAccessExpression:
			name := node.AsPropertyAccessExpression().Name()
			if name == nil {
				return nil
			}
			path = append(path, name.Text())
			node = node.AsPropertyAccessExpression().Expression
		case ast.KindElementAccessExpression:
			argument := node.AsElementAccessExpression().ArgumentExpression
			if argument == nil {
				return nil
			}
			argument = a.skipNavigationOuterExpressions(argument)
			if argument == nil || (argument.Kind != ast.KindStringLiteral && argument.Kind != ast.KindNoSubstitutionTemplateLiteral && argument.Kind != ast.KindNumericLiteral) {
				return nil
			}
			path = append(path, argument.Text())
			node = node.AsElementAccessExpression().Expression
		case ast.KindCallExpression:
			// document.getElementById("form1") names a form as reliably as
			// document.form1, so treat the lookup as the root of the path.
			id, ok := a.navigationElementLookupID(node.AsCallExpression())
			if !ok {
				return nil
			}
			path = append(path, "#"+id)
			if !a.reverseNavigationStrings(path) {
				return nil
			}
			return path
		default:
			return nil
		}
	}
	return nil
}

func (a *navigationAnalyzer) navigationElementLookupID(call *ast.CallExpression) (string, bool) {
	if call == nil || call.Arguments == nil || len(call.Arguments.Nodes) != 1 {
		return "", false
	}
	callee := a.skipNavigationOuterExpressions(call.Expression)
	if callee == nil || callee.Kind != ast.KindPropertyAccessExpression {
		return "", false
	}
	name := callee.AsPropertyAccessExpression().Name()
	if name == nil || name.Text() != "getElementById" {
		return "", false
	}
	receiver := a.skipNavigationOuterExpressions(callee.AsPropertyAccessExpression().Expression)
	if receiver == nil || receiver.Kind != ast.KindIdentifier || receiver.Text() != "document" {
		return "", false
	}
	argument := a.skipNavigationOuterExpressions(call.Arguments.Nodes[0])
	if argument == nil || (argument.Kind != ast.KindStringLiteral && argument.Kind != ast.KindNoSubstitutionTemplateLiteral) || argument.Text() == "" {
		return "", false
	}
	return argument.Text(), true
}

func (a *navigationAnalyzer) skipNavigationOuterExpressions(node *ast.Node) *ast.Node {
	for depth := 0; node != nil; depth++ {
		if depth >= navigationExpressionPathLimit {
			return nil
		}
		if err := a.checkContext(); err != nil {
			return nil
		}
		if !ast.IsOuterExpression(node, ast.OEKAll) {
			return node
		}
		node = node.Expression()
	}
	return nil
}

func (a *navigationAnalyzer) reverseNavigationStrings(values []string) bool {
	for left, right := 0, len(values)-1; left < right; left, right = left+1, right-1 {
		if err := a.checkContext(); err != nil {
			return false
		}
		if !a.collectingUnknownFallback && !a.consumeNavigationWork(1) {
			return false
		}
		values[left], values[right] = values[right], values[left]
	}
	return true
}

func lowerPath(path []string) string {
	parts := make([]string, len(path))
	for index, part := range path {
		parts[index] = strings.ToLower(part)
	}
	return strings.Join(parts, ".")
}

// navigationWindowRelativePath strips leading window, self, top, and parent
// references from a member path. It returns the remaining lower-case path and
// the frame target implied by top or parent.
func navigationWindowRelativePath(path []string) (string, string) {
	frame := ""
	index := 0
	for index < len(path)-1 {
		switch strings.ToLower(path[index]) {
		case "window", "self":
		case "top":
			frame = "_top"
		case "parent":
			frame = "_parent"
		default:
			return lowerPath(path[index:]), frame
		}
		index++
	}
	return lowerPath(path[index:]), frame
}

func navigationFormName(path []string, property string) string {
	if len(path) < 2 || !strings.EqualFold(path[len(path)-1], property) {
		return ""
	}
	return path[len(path)-2]
}

func normalizeNavigationFormMethodSet(methods navigationFiniteSet) navigationFiniteSet {
	result := navigationFiniteSet{}
	for _, candidate := range methods.values {
		if candidate.kind == NavigationValueUnknown {
			result.add(navigationCandidate{text: "{unknown}", kind: NavigationValueUnknown})
			continue
		}
		method := strings.ToUpper(strings.TrimSpace(candidate.text))
		if method != "GET" && method != "POST" {
			result.add(navigationCandidate{text: "{unknown}", kind: NavigationValueUnknown})
			continue
		}
		result.add(navigationCandidate{text: method, kind: NavigationValueLiteral})
	}
	result.unknown = result.unknown || methods.unknown
	result.ensureUnknownCandidate()
	if len(result.values) == 0 {
		return unknownNavigationSet()
	}
	return result
}

func (a *navigationAnalyzer) navigationFormSubmitMethod(formName string) string {
	if a.navigationWorkExhausted() {
		return "{unknown}"
	}
	methods, ok := a.formMethods[formName]
	if !ok {
		return "GET"
	}
	methods = normalizeNavigationFormMethodSet(methods)
	if methods.unknown || len(methods.values) != 1 {
		return "{unknown}"
	}
	method := methods.values[0]
	if method.kind == NavigationValueUnknown || method.text == "{unknown}" {
		return "{unknown}"
	}
	return method.text
}

func (a *navigationAnalyzer) registerNavigationFormMethodAssignment(binary *ast.BinaryExpression) {
	if binary == nil || binary.Left == nil || binary.OperatorToken == nil {
		return
	}
	formName := navigationFormName(a.navigationExpressionPath(binary.Left), "method")
	if formName == "" {
		return
	}
	if binary.OperatorToken.Kind != ast.KindEqualsToken || binary.Right == nil {
		a.formMethods[formName] = unknownNavigationSet()
		return
	}
	a.formMethods[formName] = normalizeNavigationFormMethodSet(a.evaluateExpression(binary.Right))
}

func (a *navigationAnalyzer) registerNavigationFormActionAssignment(binary *ast.BinaryExpression) {
	if binary == nil || binary.Left == nil || binary.OperatorToken == nil {
		return
	}
	formName := navigationFormName(a.navigationExpressionPath(binary.Left), "action")
	if formName == "" {
		return
	}
	if binary.OperatorToken.Kind != ast.KindEqualsToken || binary.Right == nil {
		a.formActions[formName] = nil
		a.formActionValues[formName] = unknownNavigationSet()
		return
	}
	a.formActions[formName] = binary.Right
	a.formActionValues[formName] = a.evaluateExpression(binary.Right)
}

func (a *navigationAnalyzer) statementRange(node *ast.Node) SourceRange {
	for depth, current := 0, node; current != nil; depth, current = depth+1, current.Parent {
		if depth >= navigationExpressionPathLimit {
			break
		}
		if err := a.checkContext(); err != nil {
			break
		}
		if current.Kind == ast.KindExpressionStatement || current.Kind == ast.KindVariableStatement {
			rangeValue := a.sourceRange(current)
			start := scanner.GetTokenPosOfNode(current, a.file, false)
			if start >= rangeValue.ByteStart && start <= rangeValue.ByteEnd {
				rangeValue.ByteStart = start
				rangeValue.Start = a.utf16[start]
			}
			return rangeValue
		}
	}
	return a.sourceRange(node)
}

func (a *navigationAnalyzer) expressionResult(node *ast.Node) NavigationExpression {
	rangeValue := a.expressionRange(node)
	finite := a.evaluateExpression(node)
	return a.navigationExpressionFromFinite(node, rangeValue, finite)
}

func (a *navigationAnalyzer) navigationExpressionFromFinite(node *ast.Node, rangeValue SourceRange, finite navigationFiniteSet) NavigationExpression {
	values := make([]NavigationValue, 0, len(finite.values)+1)
	for _, candidate := range finite.values {
		values = append(values, NavigationValue{Text: candidate.text, Kind: candidate.kind, Dynamic: candidate.kind != NavigationValueLiteral, Dependencies: append([]string(nil), finite.dependencies...)})
	}
	values = boundedNavigationValues(values, finite.unknown)
	return NavigationExpression{Text: a.sourceText(rangeValue), Range: rangeValue, Values: values}
}

type navigationCandidateKey struct {
	text string
	kind NavigationValueKind
}

func boundedNavigationValues(values []NavigationValue, incomplete bool) []NavigationValue {
	bounded := make([]NavigationValue, 0, minNavigationValueCapacity(len(values)+1))
	seen := make(map[navigationCandidateKey]int, len(values))
	unknownDependencies := []string(nil)
	for _, value := range values {
		if value.Kind == NavigationValueUnknown {
			incomplete = true
			unknownDependencies = mergeNavigationDependencies(unknownDependencies, value.Dependencies)
			continue
		}
		key := navigationCandidateKey{text: value.Text, kind: value.Kind}
		if index, ok := seen[key]; ok {
			bounded[index].Dependencies = mergeNavigationDependencies(bounded[index].Dependencies, value.Dependencies)
			bounded[index].Dynamic = bounded[index].Dynamic || value.Dynamic
			continue
		}
		if len(bounded) >= navigationValueLimit {
			incomplete = true
			unknownDependencies = mergeNavigationDependencies(unknownDependencies, value.Dependencies)
			continue
		}
		seen[key] = len(bounded)
		bounded = append(bounded, value)
	}
	if incomplete {
		if len(bounded) >= navigationValueLimit {
			bounded = bounded[:navigationValueLimit-1]
		}
		bounded = append(bounded, NavigationValue{Text: "{unknown}", Kind: NavigationValueUnknown, Dynamic: true, Dependencies: unknownDependencies})
	}
	if len(bounded) == 0 {
		bounded = append(bounded, NavigationValue{Text: "{unknown}", Kind: NavigationValueUnknown, Dynamic: true})
	}
	return bounded
}

func (a *navigationAnalyzer) expressionRange(node *ast.Node) SourceRange {
	rangeValue := a.sourceRange(node)
	if node != nil {
		start := scanner.GetTokenPosOfNode(node, a.file, false)
		if start >= rangeValue.ByteStart && start <= rangeValue.ByteEnd {
			rangeValue.ByteStart = start
		}
	}
	trimmed := 0
	for rangeValue.ByteEnd > rangeValue.ByteStart {
		if trimmed%int(navigationEvaluationWorkCheckInterval) == 0 {
			if err := a.checkContext(); err != nil {
				break
			}
		}
		r, size := utf8.DecodeLastRuneInString(a.source[rangeValue.ByteStart:rangeValue.ByteEnd])
		if size == 0 || !unicode.IsSpace(r) {
			break
		}
		rangeValue.ByteEnd -= size
		trimmed++
	}
	if rangeValue.ByteStart <= len(a.utf16)-1 && rangeValue.ByteEnd <= len(a.utf16)-1 {
		rangeValue.Start = a.utf16[rangeValue.ByteStart]
		rangeValue.End = a.utf16[rangeValue.ByteEnd]
	}
	return rangeValue
}

func (a *navigationAnalyzer) navigationSink(node *ast.Node) (NavigationSink, bool) {
	if node == nil {
		return NavigationSink{}, false
	}
	occurrence := a.sourceRange(node)
	var kind string
	var valueExpr *ast.Node
	var targetFrame string
	var formName string
	var method string
	var formActionValue navigationFiniteSet
	var hasFormActionValue bool
	switch node.Kind {
	case ast.KindBinaryExpression:
		binary := node.AsBinaryExpression()
		if binary.OperatorToken == nil || binary.OperatorToken.Kind != ast.KindEqualsToken {
			return NavigationSink{}, false
		}
		path := a.navigationExpressionPath(binary.Left)
		if navigationFormName(path, "method") != "" {
			a.registerNavigationFormMethodAssignment(binary)
			return NavigationSink{}, false
		}
		if !a.collectingUnknownFallback && !a.consumeNavigationWork(uint64(len(path)+1)) {
			return NavigationSink{}, false
		}
		locationPath, locationFrame := navigationWindowRelativePath(path)
		switch locationPath {
		case "location", "location.href", "document.location", "document.location.href":
			kind, valueExpr, targetFrame = "javascriptLocation", binary.Right, locationFrame
		}
		if kind != "" {
			break
		}
		switch lowerPath(path) {
		case "form.action":
			kind, valueExpr, formName = "javascriptFormAction", binary.Right, path[0]
			a.formActions[formName] = valueExpr
			if valueExpr != nil {
				a.formActionValues[formName] = a.evaluateExpression(valueExpr)
			}
		default:
			if len(path) >= 2 && strings.EqualFold(path[len(path)-1], "action") {
				kind, valueExpr, formName = "javascriptFormAction", binary.Right, path[len(path)-2]
				a.formActions[formName] = valueExpr
				if valueExpr != nil {
					a.formActionValues[formName] = a.evaluateExpression(valueExpr)
				}
			} else {
				return NavigationSink{}, false
			}
		}
	case ast.KindCallExpression:
		call := node.AsCallExpression()
		path := a.navigationExpressionPath(call.Expression)
		arguments := call.Arguments
		if arguments == nil {
			return NavigationSink{}, false
		}
		if !a.collectingUnknownFallback && !a.consumeNavigationWork(uint64(len(path)+1)) {
			return NavigationSink{}, false
		}
		locationPath, locationFrame := navigationWindowRelativePath(path)
		switch locationPath {
		case "location.assign", "location.replace", "document.location.assign", "document.location.replace":
			if len(arguments.Nodes) == 0 {
				return NavigationSink{}, false
			}
			kind, valueExpr, targetFrame = "javascriptLocation", arguments.Nodes[0], locationFrame
		case "navigate":
			// window.navigate is the legacy Internet Explorer equivalent of assign.
			if len(path) < 2 || len(arguments.Nodes) == 0 {
				return NavigationSink{}, false
			}
			kind, valueExpr, targetFrame = "javascriptLocation", arguments.Nodes[0], locationFrame
		}
		if kind != "" {
			break
		}
		switch lowerPath(path) {
		case "window.open":
			if len(arguments.Nodes) == 0 {
				return NavigationSink{}, false
			}
			kind, valueExpr = "javascriptLocation", arguments.Nodes[0]
			if len(arguments.Nodes) > 1 {
				frame := a.skipNavigationOuterExpressions(arguments.Nodes[1])
				if frame != nil && (frame.Kind == ast.KindStringLiteral || frame.Kind == ast.KindNoSubstitutionTemplateLiteral) {
					targetFrame = frame.Text()
				}
			}
		case "history.pushstate", "history.replacestate":
			if len(arguments.Nodes) < 3 {
				return NavigationSink{}, false
			}
			kind, valueExpr = "javascriptHistory", arguments.Nodes[2]
		default:
			if len(path) >= 2 && strings.EqualFold(path[len(path)-1], "submit") {
				kind, formName, method = "javascriptFormSubmit", path[len(path)-2], a.navigationFormSubmitMethod(path[len(path)-2])
				if actionExpr, ok := a.formActions[formName]; ok {
					valueExpr = actionExpr
					formActionValue, hasFormActionValue = a.formActionValues[formName]
				}
				if valueExpr == nil {
					return NavigationSink{Kind: kind, Range: a.statementRange(node), FormName: formName, Method: method, Snippet: a.sourceText(a.statementRange(node)), occurrence: occurrence}, true
				}
			} else {
				return NavigationSink{}, false
			}
		}
	default:
		return NavigationSink{}, false
	}
	if valueExpr == nil {
		return NavigationSink{}, false
	}
	var expression NavigationExpression
	if hasFormActionValue {
		expression = a.navigationExpressionFromFinite(valueExpr, a.expressionRange(valueExpr), formActionValue)
	} else {
		expression = a.expressionResult(valueExpr)
	}
	controlDependencies := a.navigationExpressionControlDependencies(node)
	for index := range expression.Values {
		expression.Values[index].Dependencies = mergeNavigationDependencies(expression.Values[index].Dependencies, controlDependencies)
	}
	return NavigationSink{
		Kind: kind, Range: a.statementRange(node), Expression: expression,
		TargetFrame: targetFrame, FormName: formName, Method: method,
		Snippet: a.sourceText(a.statementRange(node)), occurrence: occurrence,
	}, true
}

func (a *navigationAnalyzer) navigationExpressionControlDependencies(node *ast.Node) []string {
	dependencies := []string(nil)
	for current, parent := node, node.Parent; parent != nil; current, parent = parent, parent.Parent {
		switch parent.Kind {
		case ast.KindConditionalExpression:
			conditional := parent.AsConditionalExpression()
			if current == conditional.WhenTrue || current == conditional.WhenFalse {
				dependencies = mergeNavigationDependencies(dependencies, a.evaluateExpression(conditional.Condition).dependencies)
			}
		case ast.KindBinaryExpression:
			binary := parent.AsBinaryExpression()
			if current != binary.Right || binary.OperatorToken == nil {
				continue
			}
			switch binary.OperatorToken.Kind {
			case ast.KindAmpersandAmpersandToken, ast.KindBarBarToken, ast.KindQuestionQuestionToken:
				dependencies = mergeNavigationDependencies(dependencies, a.evaluateExpression(binary.Left).dependencies)
			}
		}
		if isNavigationFunction(parent) {
			break
		}
	}
	return dependencies
}

// collectUnknownNavigationSinks preserves useful structural results after the
// evaluator budget is exhausted. It intentionally does not attempt to execute
// expressions; every collected expression is represented by the deterministic
// unknown sentinel while source order and sink metadata remain available.
func (a *navigationAnalyzer) collectUnknownNavigationSinks() {
	if !a.navigationWorkExhausted() || a.unknownFallbackDone || a.file == nil {
		return
	}
	a.unknownFallbackDone = true
	previousScopes := a.expressionScopes
	previousEffects := a.expressionEffects
	previousFallback := a.collectingUnknownFallback
	a.expressionScopes = nil
	a.expressionEffects = nil
	a.collectingUnknownFallback = true
	defer func() {
		a.expressionScopes = previousScopes
		a.expressionEffects = previousEffects
		a.collectingUnknownFallback = previousFallback
	}()

	resolved := a.indexResolvedNavigationSinks()
	if resolved == nil {
		return
	}
	fallbackOccurrences := make(map[navigationSinkOccurrenceKey]struct{})
	stack := []*ast.Node{a.file.AsNode()}
	var nodes uint64
	var queued uint64 = 1
	var fallbackSinks int
	for len(stack) > 0 && nodes < navigationUnknownFallbackNodeLimit && fallbackSinks < navigationUnknownFallbackSinkLimit {
		if err := a.checkContext(); err != nil {
			break
		}
		last := len(stack) - 1
		node := stack[last]
		stack = stack[:last]
		queued--
		if node == nil {
			continue
		}
		nodes++
		if sink, ok := a.navigationSink(node); ok {
			key := navigationSinkOccurrenceKeyFor(sink)
			if _, alreadyResolved := resolved[key]; !alreadyResolved {
				if _, alreadyCollected := fallbackOccurrences[key]; !alreadyCollected {
					fallbackOccurrences[key] = struct{}{}
					a.sinks = append(a.sinks, a.unknownNavigationFallbackSink(sink))
					fallbackSinks++
				}
			}
		}
		if a.cancelErr != nil || nodes >= navigationUnknownFallbackNodeLimit || fallbackSinks >= navigationUnknownFallbackSinkLimit {
			break
		}
		children := make([]*ast.Node, 0, 4)
		childChecks := uint64(0)
		stopChildren := false
		node.ForEachChild(func(child *ast.Node) bool {
			if child == nil {
				return false
			}
			childChecks++
			if childChecks%navigationEvaluationWorkCheckInterval == 0 {
				if err := a.checkContext(); err != nil {
					stopChildren = true
					return true
				}
			}
			if nodes+queued >= navigationUnknownFallbackNodeLimit {
				stopChildren = true
				return true
			}
			children = append(children, child)
			queued++
			return false
		})
		if a.cancelErr != nil {
			break
		}
		for index := len(children) - 1; index >= 0; index-- {
			stack = append(stack, children[index])
		}
		if stopChildren && nodes+queued >= navigationUnknownFallbackNodeLimit {
			break
		}
	}
}

func (a *navigationAnalyzer) unknownNavigationFallbackSink(sink NavigationSink) NavigationSink {
	if sink.Kind == "javascriptFormSubmit" {
		sink.Method = "{unknown}"
		if sink.Expression.Range == (SourceRange{}) {
			sink.Expression.Range = sink.Range
			sink.Expression.Text = a.sourceText(sink.Range)
		}
	}
	sink.Expression.Values = []NavigationValue{{Text: "{unknown}", Kind: NavigationValueUnknown, Dynamic: true}}
	return sink
}

func navigationSinkLess(left, right NavigationSink) bool {
	if left.Range.Start != right.Range.Start {
		return left.Range.Start < right.Range.Start
	}
	if left.Range.End != right.Range.End {
		return left.Range.End < right.Range.End
	}
	if left.occurrence.ByteStart != right.occurrence.ByteStart {
		return left.occurrence.ByteStart < right.occurrence.ByteStart
	}
	if left.occurrence.ByteEnd != right.occurrence.ByteEnd {
		return left.occurrence.ByteEnd < right.occurrence.ByteEnd
	}
	if left.Expression.Range.Start != right.Expression.Range.Start {
		return left.Expression.Range.Start < right.Expression.Range.Start
	}
	if left.Expression.Range.End != right.Expression.Range.End {
		return left.Expression.Range.End < right.Expression.Range.End
	}
	if left.Kind != right.Kind {
		return left.Kind < right.Kind
	}
	if left.FormName != right.FormName {
		return left.FormName < right.FormName
	}
	return left.Method < right.Method
}

// sortNavigationSinks uses a bottom-up stable merge sort so cancellation can
// return from the sort itself. Both the element count and each copy/compare
// are bounded; the standard-library comparator contract cannot provide that
// abort behavior because it cannot return an error.
func (a *navigationAnalyzer) sortNavigationSinks() error {
	if err := a.checkContext(); err != nil {
		return err
	}
	if uint64(len(a.sinks)) > navigationSinkCountLimit {
		return errNavigationSinkCountLimit
	}
	if len(a.sinks) < 2 {
		return a.checkContext()
	}

	work := uint64(len(a.sinks)) // account for the bounded scratch allocation
	if work > navigationOrderingWorkLimit {
		return errNavigationSinkCountLimit
	}
	scratch := make([]NavigationSink, len(a.sinks))
	charge := func(units uint64) error {
		if units > navigationOrderingWorkLimit-work {
			return errNavigationSinkCountLimit
		}
		work += units
		if work%navigationEvaluationWorkCheckInterval == 0 {
			return a.checkContext()
		}
		return nil
	}

	fromPrimary := true
	for width := 1; width < len(a.sinks); width *= 2 {
		if err := a.checkContext(); err != nil {
			return err
		}
		var source, destination []NavigationSink
		if fromPrimary {
			source, destination = a.sinks, scratch
		} else {
			source, destination = scratch, a.sinks
		}
		for left := 0; left < len(source); left += width * 2 {
			mid := left + width
			if mid > len(source) {
				mid = len(source)
			}
			right := left + width*2
			if right > len(source) {
				right = len(source)
			}
			i, j, k := left, mid, left
			for i < mid && j < right {
				if err := charge(1); err != nil {
					return err
				}
				if navigationSinkLess(source[j], source[i]) {
					destination[k] = source[j]
					j++
				} else {
					destination[k] = source[i]
					i++
				}
				if err := charge(1); err != nil {
					return err
				}
				k++
			}
			for i < mid {
				destination[k] = source[i]
				i++
				k++
				if err := charge(1); err != nil {
					return err
				}
			}
			for j < right {
				destination[k] = source[j]
				j++
				k++
				if err := charge(1); err != nil {
					return err
				}
			}
		}
		fromPrimary = !fromPrimary
		if err := a.checkContext(); err != nil {
			return err
		}
		if width > len(a.sinks)/2 {
			break
		}
	}
	if !fromPrimary {
		for index := range a.sinks {
			a.sinks[index] = scratch[index]
			if err := charge(1); err != nil {
				return err
			}
		}
	}
	return a.checkContext()
}

// AnalyzeJavaScriptNavigation parses virtual JavaScript text with the
// TypeScript-Go AST and reports navigation sinks without inspecting hover or
// display text. It never requires a workspace project or filesystem access.
func AnalyzeJavaScriptNavigation(ctx context.Context, source string) (NavigationAnalysis, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	utf16Offsets, err := buildNavigationUTF16Offsets(ctx, source)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return NavigationAnalysis{Cancelled: true}, err
		}
		return NavigationAnalysis{}, err
	}
	if err := ctx.Err(); err != nil {
		return NavigationAnalysis{Cancelled: true}, err
	}
	file, err := parser.ParseSourceFileWithCancellation(
		ast.SourceFileParseOptions{FileName: "/__asp_lsp_navigation.ts", Path: "/__asp_lsp_navigation.ts"},
		source,
		core.ScriptKindTS,
		func() bool { return ctx.Err() != nil },
	)
	if err != nil {
		if errors.Is(err, parser.ErrCancelled) {
			cancelErr := ctx.Err()
			if cancelErr == nil {
				cancelErr = context.Canceled
			}
			return NavigationAnalysis{Cancelled: true}, cancelErr
		}
		return NavigationAnalysis{}, err
	}
	analyzer := newNavigationAnalyzer(ctx, source, file, utf16Offsets)
	if err := analyzer.indexFunctions(); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return NavigationAnalysis{Cancelled: true}, err
		}
		return NavigationAnalysis{}, err
	}
	if err := analyzer.checkContext(); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return NavigationAnalysis{Cancelled: true}, err
		}
		return NavigationAnalysis{}, err
	}
	if err := analyzer.walk(); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return NavigationAnalysis{Cancelled: true}, err
		}
		return NavigationAnalysis{}, err
	}
	if err := analyzer.resolveNavigationFunctionParameterDependencies(); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return NavigationAnalysis{Cancelled: true}, err
		}
		return NavigationAnalysis{}, err
	}
	analyzer.collectUnknownNavigationSinks()
	if err := analyzer.checkContext(); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return NavigationAnalysis{Cancelled: true}, err
		}
		return NavigationAnalysis{}, err
	}
	if err := analyzer.sortNavigationSinks(); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return NavigationAnalysis{Cancelled: true}, err
		}
		return NavigationAnalysis{}, err
	}
	calledOccurrences := make(map[SourceRange]bool)
	for _, sink := range analyzer.sinks {
		if sink.called {
			calledOccurrences[sink.occurrence] = true
		}
	}
	if len(calledOccurrences) > 0 {
		kept := analyzer.sinks[:0]
		for _, sink := range analyzer.sinks {
			// Keep conservative fallbacks for statements the evaluator could not
			// execute, even when another sink in the same function was resolved.
			if sink.called || !calledOccurrences[sink.occurrence] {
				kept = append(kept, sink)
			}
		}
		analyzer.sinks = kept
	}
	return NavigationAnalysis{Sinks: analyzer.sinks}, nil
}

// AnalyzeJavaScriptNavigationNoContext is a convenience wrapper for callers
// that do not need cancellation.
func AnalyzeJavaScriptNavigationNoContext(source string) NavigationAnalysis {
	result, _ := AnalyzeJavaScriptNavigation(context.Background(), source)
	return result
}

// AnalyzeJavaScriptNavigationContext is an explicit alias for the context
// taking API, useful when adapting generic analysis call sites.
func AnalyzeJavaScriptNavigationContext(ctx context.Context, source string) (NavigationAnalysis, error) {
	return AnalyzeJavaScriptNavigation(ctx, source)
}
