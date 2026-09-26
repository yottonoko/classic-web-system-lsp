package lspserver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/tsgoadapter"
)

const navigationJavaScriptInterpolationVariantLimit = 64
const navigationJavaScriptInterpolationElementLimit = 64

type navigationJavaScriptInterpolationVariant struct {
	replacements   []core.VirtualDocumentReplacement
	regions        []core.Region
	values         []navigationValue
	ambiguous      []bool
	budgetFallback bool
	sourceEvidence []navigationJavaScriptSourceEvidence
}

type navigationJavaScriptSourceEvidence struct {
	uri        string
	rangeValue lsp.Range
	snippet    string
}

func navigationJavaScriptInterpolationDependencyVariantContext(ctx context.Context, parsed *core.ParsedDocument, owner core.Region) (navigationJavaScriptInterpolationVariant, string, int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	interpolations := navigationJavaScriptNestedExpressions(parsed, owner)
	states, err := navigationJavaScriptInterpolationStatesContext(ctx, parsed, owner, interpolations)
	if err != nil {
		return navigationJavaScriptInterpolationVariant{}, "", 0, err
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return navigationJavaScriptInterpolationVariant{}, "", 0, err
	}
	markerPrefix := "\x00ASP_NAV_DEP_" + hex.EncodeToString(nonce) + "_"
	variant := navigationJavaScriptInterpolationVariant{}
	markerCount := 0
	for index, expression := range interpolations {
		if err := ctx.Err(); err != nil {
			return navigationJavaScriptInterpolationVariant{}, "", 0, err
		}
		if states[index] == navigationJavaScriptInterpolationLineComment || states[index] == navigationJavaScriptInterpolationBlockComment {
			continue
		}
		marker := markerPrefix + strconv.Itoa(markerCount) + "\x00"
		value := navigationValue{Kind: navigationValueLiteral, Primitive: navigationPrimitiveString, Text: marker}
		rendered := navigationJavaScriptRenderValue(parsed.Text, expression, states[index], value)
		if states[index] == navigationJavaScriptInterpolationTemplate {
			rendered = navigationJavaScriptEscapeTemplateContent(marker)
		}
		variant.replacements = append(variant.replacements, core.VirtualDocumentReplacement{
			SourceStart: expression.Start,
			SourceEnd:   expression.End,
			Text:        rendered,
		})
		variant.regions = append(variant.regions, expression)
		markerCount++
	}
	return variant, markerPrefix, markerCount, nil
}

func navigationJavaScriptEscapeTemplateContent(text string) string {
	escaped := strings.ReplaceAll(text, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, "`", "\\`")
	return strings.ReplaceAll(escaped, "${", "\\${")
}

// navigationJavaScriptInterpolationVariantsContext expands every ASP
// expression nested in an embedded JavaScript region. Values are keyed by the
// expression's source start so callers can preserve include-occurrence state.
// The expansion order is source order followed by a stable value order.
func navigationJavaScriptInterpolationVariantsContext(ctx context.Context, parsed *core.ParsedDocument, owner core.Region, valuesByOffset map[int][]navigationValue) ([]navigationJavaScriptInterpolationVariant, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if parsed == nil || owner.ContentStart < 0 || owner.ContentEnd < owner.ContentStart || owner.ContentEnd > len(parsed.Text) {
		return nil, nil
	}
	interpolations := navigationJavaScriptNestedExpressions(parsed, owner)
	if len(interpolations) == 0 {
		return nil, nil
	}

	states, err := navigationJavaScriptInterpolationStatesContext(ctx, parsed, owner, interpolations)
	if err != nil {
		return nil, err
	}
	activeInterpolations := make([]core.Region, 0, len(interpolations))
	activeStates := make([]navigationJavaScriptInterpolationState, 0, len(states))
	for index, state := range states {
		if state == navigationJavaScriptInterpolationLineComment || state == navigationJavaScriptInterpolationBlockComment {
			continue
		}
		activeInterpolations = append(activeInterpolations, interpolations[index])
		activeStates = append(activeStates, state)
	}
	interpolations, states = activeInterpolations, activeStates
	if len(interpolations) == 0 {
		return nil, nil
	}
	if len(interpolations) > navigationJavaScriptInterpolationElementLimit {
		fallback, err := navigationJavaScriptUnknownInterpolationVariant(ctx, parsed, interpolations, states, valuesByOffset)
		if err != nil {
			return nil, err
		}
		return []navigationJavaScriptInterpolationVariant{fallback}, nil
	}
	candidates := make([][]navigationValue, len(interpolations))
	ambiguous := make([]bool, len(interpolations))
	combinations := 1
	truncated := false
	for index, expression := range interpolations {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		values := valuesByOffset[expression.Start]
		if len(values) == 0 {
			values = valuesByOffset[expression.ContentStart]
		}
		if len(values) == 0 {
			values = []navigationValue{{Kind: navigationValueUnknown, Text: "{unknown}"}}
		}
		ambiguous[index] = len(values) > 1
		values = navigationJavaScriptFiniteValues(values)
		if len(values) == 0 {
			values = []navigationValue{{Kind: navigationValueUnknown, Text: "{unknown}"}}
		}
		if states[index] == navigationJavaScriptInterpolationTemplate {
			parameters := []map[string]any(nil)
			for _, value := range values {
				parameters = mergeNavigationParameterMaps(parameters, value.Parameters)
			}
			values = []navigationValue{{Kind: navigationValueUnknown, Text: "{unknown}", Parameters: parameters}}
		}
		candidates[index] = values
		if combinations > navigationJavaScriptInterpolationVariantLimit/len(values) {
			truncated = true
			combinations = navigationJavaScriptInterpolationVariantLimit
		} else {
			combinations *= len(values)
		}
	}

	variants := make([]navigationJavaScriptInterpolationVariant, 0, navigationJavaScriptInterpolationVariantLimit)
	concreteLimit := navigationJavaScriptInterpolationVariantLimit
	if truncated {
		concreteLimit--
	}
	selected := make([]navigationValue, len(interpolations))
	var visit func(int) error
	visit = func(index int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(variants) >= concreteLimit {
			return nil
		}
		if index == len(interpolations) {
			replacements := make([]core.VirtualDocumentReplacement, len(interpolations))
			for expressionIndex, expression := range interpolations {
				replacements[expressionIndex] = core.VirtualDocumentReplacement{
					SourceStart: expression.Start,
					SourceEnd:   expression.End,
					Text:        navigationJavaScriptRenderValue(parsed.Text, expression, states[expressionIndex], selected[expressionIndex]),
				}
			}
			values := make([]navigationValue, len(selected))
			copy(values, selected)
			regions := append([]core.Region(nil), interpolations...)
			variants = append(variants, navigationJavaScriptInterpolationVariant{
				replacements: replacements,
				regions:      regions,
				values:       values,
				ambiguous:    append([]bool(nil), ambiguous...),
			})
			return nil
		}
		for _, value := range candidates[index] {
			if err := ctx.Err(); err != nil {
				return err
			}
			selected[index] = value
			if err := visit(index + 1); err != nil {
				return err
			}
			if len(variants) >= concreteLimit {
				break
			}
		}
		return nil
	}
	if err := visit(0); err != nil {
		return nil, err
	}
	if truncated {
		fallback, err := navigationJavaScriptUnknownInterpolationVariant(ctx, parsed, interpolations, states, valuesByOffset)
		if err != nil {
			return nil, err
		}
		variants = append(variants, fallback)
	}
	return variants, nil
}

func navigationJavaScriptUnknownInterpolationVariant(ctx context.Context, parsed *core.ParsedDocument, interpolations []core.Region, states []navigationJavaScriptInterpolationState, valuesByOffset map[int][]navigationValue) (navigationJavaScriptInterpolationVariant, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	fallbackValues := make([]navigationValue, len(interpolations))
	fallbackReplacements := make([]core.VirtualDocumentReplacement, len(interpolations))
	for index, expression := range interpolations {
		if err := ctx.Err(); err != nil {
			return navigationJavaScriptInterpolationVariant{}, err
		}
		parameters := []map[string]any(nil)
		for _, value := range valuesByOffset[expression.Start] {
			if err := ctx.Err(); err != nil {
				return navigationJavaScriptInterpolationVariant{}, err
			}
			for _, candidate := range value.finiteCandidates() {
				parameters = mergeNavigationParameterMaps(parameters, candidate.Parameters)
			}
		}
		fallbackValues[index] = navigationValue{Kind: navigationValueUnknown, Text: "{unknown}", Parameters: parameters}
		fallbackReplacements[index] = core.VirtualDocumentReplacement{
			SourceStart: expression.Start,
			SourceEnd:   expression.End,
			Text:        navigationJavaScriptRenderValue(parsed.Text, expression, states[index], fallbackValues[index]),
		}
	}
	return navigationJavaScriptInterpolationVariant{
		replacements: fallbackReplacements,
		regions:      append([]core.Region(nil), interpolations...),
		values:       fallbackValues,
	}, nil
}

func navigationJavaScriptUnknownInterpolationVariantForRegion(ctx context.Context, parsed *core.ParsedDocument, owner core.Region, valuesByOffset map[int][]navigationValue) (navigationJavaScriptInterpolationVariant, bool, error) {
	interpolations := navigationJavaScriptNestedExpressions(parsed, owner)
	if len(interpolations) == 0 {
		return navigationJavaScriptInterpolationVariant{}, false, nil
	}
	states, err := navigationJavaScriptInterpolationStatesContext(ctx, parsed, owner, interpolations)
	if err != nil {
		return navigationJavaScriptInterpolationVariant{}, false, err
	}
	activeInterpolations := make([]core.Region, 0, len(interpolations))
	activeStates := make([]navigationJavaScriptInterpolationState, 0, len(states))
	for index, state := range states {
		if state == navigationJavaScriptInterpolationLineComment || state == navigationJavaScriptInterpolationBlockComment {
			continue
		}
		activeInterpolations = append(activeInterpolations, interpolations[index])
		activeStates = append(activeStates, state)
	}
	if len(activeInterpolations) == 0 {
		return navigationJavaScriptInterpolationVariant{}, false, nil
	}
	fallback, err := navigationJavaScriptUnknownInterpolationVariant(ctx, parsed, activeInterpolations, activeStates, valuesByOffset)
	if err != nil {
		return navigationJavaScriptInterpolationVariant{}, false, err
	}
	return fallback, true, nil
}

func navigationJavaScriptNestedExpressions(parsed *core.ParsedDocument, owner core.Region) []core.Region {
	if parsed == nil {
		return nil
	}
	expressions := make([]core.Region, 0)
	for _, region := range parsed.Regions {
		if region.Kind != core.RegionASPExpression || region.Start < owner.ContentStart || region.End > owner.ContentEnd {
			continue
		}
		expressions = append(expressions, region)
	}
	sort.SliceStable(expressions, func(left, right int) bool {
		if expressions[left].Start != expressions[right].Start {
			return expressions[left].Start < expressions[right].Start
		}
		return expressions[left].End < expressions[right].End
	})
	return expressions
}

func navigationJavaScriptFiniteValues(values []navigationValue) []navigationValue {
	finite := make([]navigationValue, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		for _, candidate := range value.finiteCandidates() {
			candidate = cloneNavigationValue(candidate)
			candidate.Alternatives = nil
			key := strconv.Itoa(int(candidate.Kind)) + "\x00" + strconv.Itoa(int(candidate.Primitive)) + "\x00" + candidate.Text + "\x00" + navigationParameterKey(candidate.Parameters)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			finite = append(finite, candidate)
		}
	}
	sort.SliceStable(finite, func(left, right int) bool {
		if finite[left].Text != finite[right].Text {
			return finite[left].Text < finite[right].Text
		}
		if finite[left].Kind != finite[right].Kind {
			return finite[left].Kind < finite[right].Kind
		}
		if finite[left].Primitive != finite[right].Primitive {
			return finite[left].Primitive < finite[right].Primitive
		}
		return navigationParameterKey(finite[left].Parameters) < navigationParameterKey(finite[right].Parameters)
	})
	return finite
}

type navigationJavaScriptInterpolationState uint8

const (
	navigationJavaScriptInterpolationRaw navigationJavaScriptInterpolationState = iota
	navigationJavaScriptInterpolationSingleQuoted
	navigationJavaScriptInterpolationDoubleQuoted
	navigationJavaScriptInterpolationLineComment
	navigationJavaScriptInterpolationBlockComment
	navigationJavaScriptInterpolationTemplate
	navigationJavaScriptInterpolationRegex
)

func navigationJavaScriptInterpolationStatesContext(ctx context.Context, parsed *core.ParsedDocument, owner core.Region, expressions []core.Region) ([]navigationJavaScriptInterpolationState, error) {
	states := make([]navigationJavaScriptInterpolationState, len(expressions))
	if parsed == nil {
		return states, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	source := parsed.Text
	expressionIndex := 0
	holes := make([]core.Region, 0)
	// Skip every ASP island while determining JavaScript lexical state. Server
	// blocks can contain quote/comment characters that are not JavaScript.
	for _, region := range parsed.Regions {
		if !navigationJavaScriptASPHole(region) || region.Start < owner.ContentStart || region.End > owner.ContentEnd {
			continue
		}
		holes = append(holes, region)
	}
	sort.SliceStable(holes, func(left, right int) bool {
		if holes[left].Start != holes[right].Start {
			return holes[left].Start < holes[right].Start
		}
		return holes[left].End < holes[right].End
	})
	state := navigationJavaScriptInterpolationRaw
	escaped := false
	regexCharacterClass := false
	var lastSignificant byte
	for cursor := owner.ContentStart; cursor < owner.ContentEnd; {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if expressionIndex < len(expressions) && cursor == expressions[expressionIndex].Start {
			states[expressionIndex] = state
			holeStart, holeEnd := expressions[expressionIndex].Start, expressions[expressionIndex].End
			cursor = expressions[expressionIndex].End
			if len(holes) > 0 && holes[0].Start == expressions[expressionIndex].Start {
				holes = holes[1:]
			}
			expressionIndex++
			if state == navigationJavaScriptInterpolationLineComment && strings.ContainsAny(source[holeStart:holeEnd], "\r\n") {
				state = navigationJavaScriptInterpolationRaw
			}
			escaped = false
			continue
		}
		if len(holes) > 0 && cursor == holes[0].Start {
			holeStart, holeEnd := holes[0].Start, holes[0].End
			cursor = holes[0].End
			holes = holes[1:]
			if state == navigationJavaScriptInterpolationLineComment && strings.ContainsAny(source[holeStart:holeEnd], "\r\n") {
				state = navigationJavaScriptInterpolationRaw
			}
			escaped = false
			continue
		}
		if cursor >= len(source) {
			break
		}
		ch := source[cursor]
		switch state {
		case navigationJavaScriptInterpolationLineComment:
			if ch == '\n' || ch == '\r' {
				state = navigationJavaScriptInterpolationRaw
			}
			cursor++
		case navigationJavaScriptInterpolationBlockComment:
			if ch == '*' && cursor+1 < owner.ContentEnd && source[cursor+1] == '/' {
				state = navigationJavaScriptInterpolationRaw
				cursor += 2
			} else {
				cursor++
			}
		case navigationJavaScriptInterpolationSingleQuoted:
			if escaped {
				escaped = false
				cursor++
			} else if ch == '\\' {
				escaped = true
				cursor++
			} else if ch == '\'' {
				state = navigationJavaScriptInterpolationRaw
				lastSignificant = 'v'
				cursor++
			} else {
				cursor++
			}
		case navigationJavaScriptInterpolationDoubleQuoted:
			if escaped {
				escaped = false
				cursor++
			} else if ch == '\\' {
				escaped = true
				cursor++
			} else if ch == '"' {
				state = navigationJavaScriptInterpolationRaw
				lastSignificant = 'v'
				cursor++
			} else {
				cursor++
			}
		case navigationJavaScriptInterpolationTemplate:
			if escaped {
				escaped = false
				cursor++
			} else if ch == '\\' {
				escaped = true
				cursor++
			} else if ch == '`' {
				state = navigationJavaScriptInterpolationRaw
				lastSignificant = 'v'
				cursor++
			} else {
				cursor++
			}
		case navigationJavaScriptInterpolationRegex:
			if escaped {
				escaped = false
				cursor++
			} else if ch == '\\' {
				escaped = true
				cursor++
			} else if ch == '[' {
				regexCharacterClass = true
				cursor++
			} else if ch == ']' && regexCharacterClass {
				regexCharacterClass = false
				cursor++
			} else if ch == '/' && !regexCharacterClass {
				state = navigationJavaScriptInterpolationRaw
				lastSignificant = 'v'
				cursor++
			} else if ch == '\n' || ch == '\r' {
				state = navigationJavaScriptInterpolationRaw
				lastSignificant = 0
				regexCharacterClass = false
				cursor++
			} else {
				cursor++
			}
		default:
			if (ch == '+' || ch == '-') && cursor+1 < owner.ContentEnd && source[cursor+1] == ch {
				lastSignificant = 'v'
				cursor += 2
				continue
			}
			if navigationJavaScriptIdentifierStart(ch) {
				start := cursor
				cursor++
				for cursor < owner.ContentEnd && navigationJavaScriptIdentifierPart(source[cursor]) {
					cursor++
				}
				if navigationJavaScriptKeywordAllowsRegex(source[start:cursor]) {
					lastSignificant = '='
				} else {
					lastSignificant = 'v'
				}
				continue
			}
			if ch == '/' && cursor+1 < owner.ContentEnd {
				switch source[cursor+1] {
				case '/':
					state = navigationJavaScriptInterpolationLineComment
					cursor += 2
					continue
				case '*':
					state = navigationJavaScriptInterpolationBlockComment
					cursor += 2
					continue
				}
				if navigationJavaScriptRegexCanStartAfter(lastSignificant) {
					state = navigationJavaScriptInterpolationRegex
					escaped = false
					regexCharacterClass = false
					cursor++
					continue
				}
			}
			switch ch {
			case '\'':
				state = navigationJavaScriptInterpolationSingleQuoted
			case '"':
				state = navigationJavaScriptInterpolationDoubleQuoted
			case '`':
				state = navigationJavaScriptInterpolationTemplate
			case ' ', '\t', '\n', '\r':
				cursor++
				continue
			default:
				if navigationJavaScriptRegexCanStartAfter(ch) {
					lastSignificant = ch
				} else {
					lastSignificant = 'v'
				}
			}
			cursor++
		}
	}
	virtual, err := core.BuildEmbeddedRegionVirtualDocumentWithReplacementsContext(ctx, parsed, owner, nil)
	if err != nil {
		return nil, err
	}
	offsets := make([]int, len(expressions))
	for index, expression := range expressions {
		offsets[index] = expression.Start - owner.ContentStart
	}
	literalContexts, err := tsgoadapter.ClassifyJavaScriptLiteralContextsContext(ctx, virtual.Text, offsets)
	if err != nil {
		return nil, err
	}
	for index, literalContext := range literalContexts {
		if states[index] == navigationJavaScriptInterpolationLineComment || states[index] == navigationJavaScriptInterpolationBlockComment {
			continue
		}
		switch literalContext {
		case tsgoadapter.JavaScriptLiteralSingleQuoted:
			states[index] = navigationJavaScriptInterpolationSingleQuoted
		case tsgoadapter.JavaScriptLiteralDoubleQuoted:
			states[index] = navigationJavaScriptInterpolationDoubleQuoted
		case tsgoadapter.JavaScriptLiteralTemplate:
			states[index] = navigationJavaScriptInterpolationTemplate
		case tsgoadapter.JavaScriptLiteralRegex:
			states[index] = navigationJavaScriptInterpolationRegex
		default:
			states[index] = navigationJavaScriptInterpolationRaw
		}
	}
	return states, nil
}

func navigationJavaScriptRegexCanStartAfter(lastSignificant byte) bool {
	if lastSignificant == 0 {
		return true
	}
	return strings.ContainsRune("=([{,:;!?&|+-*/%^~<>", rune(lastSignificant))
}

func navigationJavaScriptIdentifierStart(ch byte) bool {
	return ch == '_' || ch == '$' || ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z'
}

func navigationJavaScriptIdentifierPart(ch byte) bool {
	return navigationJavaScriptIdentifierStart(ch) || ch >= '0' && ch <= '9'
}

func navigationJavaScriptKeywordAllowsRegex(word string) bool {
	switch word {
	case "await", "case", "delete", "do", "else", "in", "instanceof", "new", "of", "return", "throw", "typeof", "void", "yield":
		return true
	default:
		return false
	}
}

func navigationJavaScriptASPHole(region core.Region) bool {
	return region.Kind == core.RegionASPBlock || region.Kind == core.RegionASPExpression || region.Kind == core.RegionASPDirective
}

func navigationJavaScriptRenderValue(source string, expression core.Region, state navigationJavaScriptInterpolationState, value navigationValue) string {
	if state == navigationJavaScriptInterpolationLineComment || state == navigationJavaScriptInterpolationBlockComment {
		return navigationJavaScriptPreserveLineEndings(source, expression.Start, expression.End)
	}
	if state == navigationJavaScriptInterpolationTemplate {
		return "undefined"
	}
	if state == navigationJavaScriptInterpolationRegex {
		return navigationJavaScriptEscapeRegexContent(navigationJavaScriptRenderedContent(value))
	}
	if state == navigationJavaScriptInterpolationSingleQuoted || state == navigationJavaScriptInterpolationDoubleQuoted {
		quote := byte('\'')
		if state == navigationJavaScriptInterpolationDoubleQuoted {
			quote = '"'
		}
		content := navigationJavaScriptRenderedContent(value)
		if value.Kind == navigationValueUnknown || value.Kind == navigationValueTemplate {
			content = "{unknown}"
		}
		escaped := navigationJavaScriptEscapeStringContent(content, quote)
		if navigationJavaScriptOddBackslashPrefix(source, expression.Start) {
			escaped = `\` + escaped
		}
		return escaped
	}
	return navigationJavaScriptRawValue(value)
}

func navigationJavaScriptEscapeRegexContent(text string) string {
	const hexDigits = "0123456789abcdef"
	var escaped strings.Builder
	for _, char := range text {
		switch char {
		case '\u2028':
			escaped.WriteString(`\u2028`)
		case '\u2029':
			escaped.WriteString(`\u2029`)
		default:
			if char <= 0x7f && !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '_') {
				escaped.WriteString(`\x`)
				escaped.WriteByte(hexDigits[byte(char)>>4])
				escaped.WriteByte(hexDigits[byte(char)&0x0f])
			} else if char >= 0x20 {
				escaped.WriteRune(char)
			}
		}
	}
	return escaped.String()
}

func navigationJavaScriptOddBackslashPrefix(source string, offset int) bool {
	count := 0
	for index := offset - 1; index >= 0 && source[index] == '\\'; index-- {
		count++
	}
	return count%2 == 1
}

func navigationJavaScriptRawValue(value navigationValue) string {
	if value.Kind == navigationValueUnknown || value.Kind == navigationValueTemplate {
		return "undefined"
	}
	primitive := value.Primitive
	if primitive == navigationPrimitiveUnknown {
		primitive = navigationJavaScriptInferPrimitive(value.Text)
	}
	switch primitive {
	case navigationPrimitiveNumber:
		if number, ok := navigationJavaScriptNumberLiteral(value.Text); ok {
			return number
		}
		return "undefined"
	case navigationPrimitiveBoolean:
		if strings.EqualFold(strings.TrimSpace(value.Text), "true") {
			return "true"
		}
		return "false"
	default:
		return `"` + navigationJavaScriptEscapeStringContent(value.Text, '"') + `"`
	}
}

func navigationJavaScriptRenderedContent(value navigationValue) string {
	if value.Kind == navigationValueUnknown || value.Kind == navigationValueTemplate {
		return "undefined"
	}
	primitive := value.Primitive
	if primitive == navigationPrimitiveUnknown {
		primitive = navigationJavaScriptInferPrimitive(value.Text)
	}
	switch primitive {
	case navigationPrimitiveNumber:
		return navigationVBStringifiedText(value)
	case navigationPrimitiveBoolean:
		if strings.EqualFold(strings.TrimSpace(value.Text), "true") {
			return "True"
		}
		return "False"
	default:
		return value.Text
	}
}

func navigationJavaScriptInferPrimitive(text string) navigationPrimitiveKind {
	trimmed := strings.TrimSpace(text)
	if strings.EqualFold(trimmed, "true") || strings.EqualFold(trimmed, "false") {
		return navigationPrimitiveBoolean
	}
	if _, ok := navigationJavaScriptNumberLiteral(trimmed); ok {
		return navigationPrimitiveNumber
	}
	return navigationPrimitiveString
}

func navigationJavaScriptNumberLiteral(text string) (string, bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return "", false
	}
	sign := ""
	if trimmed[0] == '+' || trimmed[0] == '-' {
		sign, trimmed = trimmed[:1], strings.TrimSpace(trimmed[1:])
	}
	if trimmed == "" {
		return "", false
	}
	// VBScript accepts a small family of numeric type suffixes. Strip only a
	// final suffix; rejecting other characters keeps the generated JavaScript
	// syntax valid and avoids treating arbitrary expressions as numbers.
	if suffix := trimmed[len(trimmed)-1]; strings.ContainsRune("%&!#@", rune(suffix)) {
		trimmed = strings.TrimSpace(trimmed[:len(trimmed)-1])
		if trimmed == "" {
			return "", false
		}
	}
	numberText := sign + trimmed
	lower := strings.ToLower(trimmed)
	radix := strings.HasPrefix(lower, "&h") || strings.HasPrefix(lower, "&o") || strings.HasPrefix(lower, "&")
	if radix {
		numberText = trimmed
	}
	parsed, ok := parseVBScriptNumericLiteral(numberText)
	if !ok {
		return "", false
	}
	if radix {
		identity := parsed.identity()
		if identity == "" || strings.HasPrefix(identity, "symbolic:") || strings.Contains(identity, "/") {
			return "", false
		}
		return sign + identity, true
	}
	// Decimal VB literals are already valid JavaScript numeric syntax. Keep the
	// exact spelling so large integers and precise decimals are not rounded via
	// float64 during analysis.
	return sign + trimmed, true
}

func navigationJavaScriptPreserveLineEndings(source string, start, end int) string {
	if start < 0 {
		start = 0
	}
	if end < start {
		end = start
	}
	if end > len(source) {
		end = len(source)
	}
	var builder strings.Builder
	builder.Grow(end - start)
	for index := start; index < end; index++ {
		switch source[index] {
		case '\n', '\r':
			builder.WriteByte(source[index])
		default:
			builder.WriteByte(' ')
		}
	}
	return builder.String()
}

func navigationJavaScriptEscapeStringContent(text string, quote byte) string {
	var builder strings.Builder
	for index := 0; index < len(text); {
		runeValue, size := utf8.DecodeRuneInString(text[index:])
		if runeValue == utf8.RuneError && size == 1 {
			runeValue = '\ufffd'
		}
		index += size
		switch runeValue {
		case '\\':
			builder.WriteString(`\\`)
		case '\n':
			builder.WriteString(`\n`)
		case '\r':
			builder.WriteString(`\r`)
		case '\t':
			builder.WriteString(`\t`)
		case '\b':
			builder.WriteString(`\b`)
		case '\f':
			builder.WriteString(`\f`)
		case '\v':
			builder.WriteString(`\v`)
		case '\u2028':
			builder.WriteString(`\u2028`)
		case '\u2029':
			builder.WriteString(`\u2029`)
		default:
			if runeValue == rune(quote) {
				builder.WriteByte('\\')
			}
			builder.WriteRune(runeValue)
		}
	}
	return builder.String()
}

type navigationJavaScriptReplacementSpan struct {
	virtualStart int
	virtualEnd   int
}

func navigationJavaScriptReplacementSpans(owner core.Region, replacements []core.VirtualDocumentReplacement) []navigationJavaScriptReplacementSpan {
	if len(replacements) == 0 {
		return nil
	}
	spans := make([]navigationJavaScriptReplacementSpan, 0, len(replacements))
	delta := 0
	previousEnd := -1
	for _, replacement := range replacements {
		if replacement.SourceStart < owner.ContentStart || replacement.SourceEnd <= replacement.SourceStart || replacement.SourceEnd > owner.ContentEnd {
			continue
		}
		if previousEnd > replacement.SourceStart {
			return nil
		}
		start := replacement.SourceStart - owner.ContentStart + delta
		end := start + len(replacement.Text)
		spans = append(spans, navigationJavaScriptReplacementSpan{virtualStart: start, virtualEnd: end})
		delta += len(replacement.Text) - (replacement.SourceEnd - replacement.SourceStart)
		previousEnd = replacement.SourceEnd
	}
	return spans
}

func navigationJavaScriptReplacementSourceRanges(ctx context.Context, source *core.TextDocument, spans []navigationJavaScriptReplacementSpan, expressionRegions []core.Region, sinkStart, sinkEnd int) ([]lsp.Range, []string, []int, error) {
	if source == nil || len(expressionRegions) == 0 {
		return nil, nil, nil, nil
	}
	selected := make([]core.Region, 0, len(expressionRegions))
	selectedIndices := make([]int, 0, len(expressionRegions))
	first := sort.Search(len(spans), func(index int) bool {
		return spans[index].virtualEnd >= sinkStart
	})
	for index := first; index < len(spans) && index < len(expressionRegions) && spans[index].virtualStart <= sinkEnd; index++ {
		if err := ctx.Err(); err != nil {
			return nil, nil, nil, err
		}
		if navigationJavaScriptRangesOverlap(sinkStart, sinkEnd, spans[index].virtualStart, spans[index].virtualEnd) {
			expression := expressionRegions[index]
			selected = append(selected, expression)
			selectedIndices = append(selectedIndices, index)
		}
	}
	if len(selected) == 0 {
		return nil, nil, nil, nil
	}
	ranges := make([]lsp.Range, 0, len(selected))
	snippets := make([]string, 0, len(selected))
	for _, expression := range selected {
		if err := ctx.Err(); err != nil {
			return nil, nil, nil, err
		}
		rangeValue := source.Range(expression.Start, expression.End)
		ranges = append(ranges, rangeValue)
		snippets = append(snippets, navigationJavaScriptEvidenceSnippet(source.Text, expression.Start, expression.End))
	}
	return ranges, snippets, selectedIndices, nil
}

func navigationJavaScriptEvidenceSnippet(text string, start, end int) string {
	const limit = 256
	if start < 0 {
		start = 0
	}
	if end < start {
		end = start
	}
	if end > len(text) {
		end = len(text)
	}
	if end-start > limit {
		end = start + limit
	}
	remaining := limit - (end - start)
	left := remaining / 2
	if left > start {
		left = start
	}
	start -= left
	remaining -= left
	if remaining > len(text)-end {
		remaining = len(text) - end
	}
	end += remaining
	return strings.TrimSpace(text[start:end])
}

func navigationJavaScriptRangesOverlap(leftStart, leftEnd, rightStart, rightEnd int) bool {
	if leftEnd < leftStart {
		leftEnd = leftStart
	}
	if rightEnd < rightStart {
		rightEnd = rightStart
	}
	if leftStart == leftEnd {
		return leftStart >= rightStart && leftStart <= rightEnd
	}
	if rightStart == rightEnd {
		return rightStart >= leftStart && rightStart <= leftEnd
	}
	return leftStart < rightEnd && rightStart < leftEnd
}
