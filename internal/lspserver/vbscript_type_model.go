package lspserver

import (
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// vbscriptLiteralUnionLimit bounds the amount of value-level information kept
// for mutable variables. A bounded set keeps repeated assignment analysis
// predictable while still making common state variables useful to consumers.
const vbscriptLiteralUnionLimit = 32

type vbscriptTypeKind uint8

const (
	vbscriptTypeInvalid vbscriptTypeKind = iota
	vbscriptTypeUnknown
	vbscriptTypePrimitive
	vbscriptTypeObject
	vbscriptTypeStringLiteral
	vbscriptTypeNumberLiteral
	vbscriptTypeBooleanLiteral
	vbscriptTypeTemplate
	vbscriptTypeUnion
)

// vbscriptType is the structured representation used by type annotations and
// value inference. The existing string-based type maps remain the public
// compatibility surface; this model carries information those maps cannot.
type vbscriptType struct {
	kind     vbscriptTypeKind
	name     string
	value    string
	parts    []vbscriptType
	template []vbscriptTemplatePart
}

type vbscriptTemplatePart struct {
	// literal retains the annotation spelling for display and round-tripping.
	literal string
	decoded string
	expr    *vbscriptType
}

func (t vbscriptType) String() string {
	switch t.kind {
	case vbscriptTypePrimitive, vbscriptTypeObject:
		return t.name
	case vbscriptTypeUnknown:
		return "Variant"
	case vbscriptTypeStringLiteral:
		return `"` + escapeVBScriptAnnotationString(t.value) + `"`
	case vbscriptTypeNumberLiteral, vbscriptTypeBooleanLiteral:
		return t.value
	case vbscriptTypeTemplate:
		var builder strings.Builder
		builder.WriteByte('`')
		for _, part := range t.template {
			builder.WriteString(part.literal)
			if part.expr != nil {
				builder.WriteString("${")
				builder.WriteString(part.expr.String())
				builder.WriteByte('}')
			}
		}
		builder.WriteByte('`')
		return builder.String()
	case vbscriptTypeUnion:
		values := make([]string, 0, len(t.parts))
		for _, part := range t.parts {
			values = append(values, part.String())
		}
		return strings.Join(values, " | ")
	default:
		return "Variant"
	}
}

func escapeVBScriptAnnotationString(value string) string {
	var builder strings.Builder
	builder.Grow(len(value))
	for index := 0; index < len(value); index++ {
		switch value[index] {
		case '\\':
			builder.WriteString(`\\`)
		case '"':
			builder.WriteString(`""`)
		default:
			builder.WriteByte(value[index])
		}
	}
	return builder.String()
}

func decodeVBScriptTemplateStaticLiteral(text string) (string, error) {
	var builder strings.Builder
	builder.Grow(len(text))
	for index := 0; index < len(text); index++ {
		if text[index] != '\\' {
			builder.WriteByte(text[index])
			continue
		}
		if index+1 >= len(text) {
			return "", errors.New("trailing template escape")
		}
		switch text[index+1] {
		case '\\', '}', '`':
			builder.WriteByte(text[index+1])
			index++
		case '$':
			if index+2 >= len(text) || text[index+2] != '{' {
				return "", errors.New("invalid template escape")
			}
			builder.WriteString("${")
			index += 2
		default:
			return "", errors.New("invalid template escape")
		}
	}
	return builder.String(), nil
}

func (t vbscriptType) isUnknown() bool {
	return t.kind == vbscriptTypeUnknown || t.kind == vbscriptTypeInvalid
}

func (t vbscriptType) isLiteral() bool {
	switch t.kind {
	case vbscriptTypeStringLiteral, vbscriptTypeNumberLiteral, vbscriptTypeBooleanLiteral:
		return true
	default:
		return false
	}
}

func (t vbscriptType) primitiveName() string {
	switch t.kind {
	case vbscriptTypeStringLiteral, vbscriptTypeTemplate:
		return "String"
	case vbscriptTypeNumberLiteral:
		return "Number"
	case vbscriptTypeBooleanLiteral:
		return "Boolean"
	case vbscriptTypePrimitive, vbscriptTypeObject:
		return t.name
	default:
		return "Variant"
	}
}

func (t vbscriptType) identity() string {
	if t.kind == vbscriptTypeUnion {
		parts := make([]string, 0, len(t.parts))
		for _, part := range t.parts {
			parts = append(parts, part.identity())
		}
		// Union order is presentation/source-order data. Sort only the
		// semantic identities so equivalent nested unions deduplicate even when
		// their arms were written in a different order.
		sort.Strings(parts)
		var builder strings.Builder
		builder.WriteString(fmt.Sprintf("%d:", t.kind))
		builder.WriteString(strconv.Itoa(len(parts)))
		builder.WriteByte(':')
		for _, part := range parts {
			builder.WriteString(strconv.Itoa(len(part)))
			builder.WriteByte(':')
			builder.WriteString(part)
		}
		return builder.String()
	}
	if t.kind == vbscriptTypeStringLiteral {
		return "string:" + t.value
	}
	if t.kind == vbscriptTypeNumberLiteral {
		return "number:" + vbscriptNumericLiteralIdentity(t.value)
	}
	if t.kind == vbscriptTypeBooleanLiteral {
		return "boolean:" + strings.ToLower(t.value)
	}
	if t.kind == vbscriptTypeObject {
		// VBScript object type names are case-insensitive. Keep the source
		// spelling on the value for display, but use only the normalized name
		// for semantic identity so equivalent union arms deduplicate.
		return fmt.Sprintf("%d:%s", t.kind, strings.ToLower(t.name))
	}
	if t.kind == vbscriptTypeTemplate {
		// Template spelling is presentation data. Identity must follow the
		// decoded static segments and the semantic identities of interpolated
		// types, including nested templates and unions. Length framing keeps
		// adjacent segments and embedded delimiters unambiguous.
		var builder strings.Builder
		builder.WriteString(fmt.Sprintf("%d:", t.kind))
		builder.WriteString(strconv.Itoa(len(t.template)))
		builder.WriteByte(':')
		for _, part := range t.template {
			if part.expr == nil {
				builder.WriteString("s:")
				builder.WriteString(strconv.Itoa(len(part.decoded)))
				builder.WriteByte(':')
				builder.WriteString(part.decoded)
				continue
			}
			child := part.expr.identity()
			builder.WriteString("e:")
			builder.WriteString(strconv.Itoa(len(child)))
			builder.WriteByte(':')
			builder.WriteString(child)
		}
		return builder.String()
	}
	return fmt.Sprintf("%d:%s", t.kind, strings.ToLower(t.name)) + t.String()
}

const (
	// Type expressions originate in comments and configuration, so keep the
	// parser bounded before any recursive or value-level work begins.
	vbscriptTypeParseInputLengthLimit  = 64 * 1024
	vbscriptTypeParseNestingLimit      = 64
	vbscriptTypeParseNodeLimit         = 4096
	vbscriptTypeParseTemplatePartLimit = 1024
	vbscriptTypeParseUnionArmLimit     = 256
)

type vbscriptTypeParseError struct {
	code   string
	limit  int
	actual int
}

func (err *vbscriptTypeParseError) Error() string {
	return fmt.Sprintf("VBScript type expression exceeds %s limit (%d > %d)", err.code, err.actual, err.limit)
}

func newVBScriptTypeParseError(code string, limit, actual int) error {
	return &vbscriptTypeParseError{code: code, limit: limit, actual: actual}
}

func isVBScriptTypeParseLimitError(err error) bool {
	var limitError *vbscriptTypeParseError
	return errors.As(err, &limitError)
}

type vbscriptTypeParseBudget struct {
	depth             int
	nodes             int
	templateParts     int
	templateScanDepth int
}

func (budget *vbscriptTypeParseBudget) enter() error {
	if budget.depth >= vbscriptTypeParseNestingLimit {
		return newVBScriptTypeParseError("nesting depth", vbscriptTypeParseNestingLimit, budget.depth+1)
	}
	budget.depth++
	return nil
}

func (budget *vbscriptTypeParseBudget) leave() {
	if budget.depth > 0 {
		budget.depth--
	}
}

func (budget *vbscriptTypeParseBudget) consumeNodes(count int) error {
	if count <= 0 {
		return nil
	}
	if count > vbscriptTypeParseNodeLimit-budget.nodes {
		return newVBScriptTypeParseError("parsed nodes", vbscriptTypeParseNodeLimit, budget.nodes+count)
	}
	budget.nodes += count
	return nil
}

func (budget *vbscriptTypeParseBudget) consumeTemplateParts(count int) error {
	if count <= 0 {
		return nil
	}
	if count > vbscriptTypeParseTemplatePartLimit-budget.templateParts {
		return newVBScriptTypeParseError("template parts", vbscriptTypeParseTemplatePartLimit, budget.templateParts+count)
	}
	budget.templateParts += count
	return nil
}

func (budget *vbscriptTypeParseBudget) enterTemplateScan() error {
	if budget.templateScanDepth >= vbscriptTypeParseNestingLimit {
		return newVBScriptTypeParseError("nesting depth", vbscriptTypeParseNestingLimit, budget.templateScanDepth+1)
	}
	budget.templateScanDepth++
	return nil
}

func (budget *vbscriptTypeParseBudget) leaveTemplateScan() {
	if budget.templateScanDepth > 0 {
		budget.templateScanDepth--
	}
}

func parseVBScriptType(text string) (vbscriptType, error) {
	if len(text) > vbscriptTypeParseInputLengthLimit {
		return vbscriptType{}, newVBScriptTypeParseError("input length", vbscriptTypeParseInputLengthLimit, len(text))
	}
	return parseVBScriptTypeWithBudget(strings.TrimSpace(text), &vbscriptTypeParseBudget{})
}

func parseVBScriptTypeWithBudget(text string, budget *vbscriptTypeParseBudget) (vbscriptType, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return vbscriptType{}, errors.New("empty type expression")
	}
	if err := budget.enter(); err != nil {
		return vbscriptType{}, err
	}
	defer budget.leave()
	parts, err := splitVBScriptTypeUnionWithBudget(text, budget)
	if err != nil {
		return vbscriptType{}, err
	}
	parsed := make([]vbscriptType, 0, len(parts))
	for _, part := range parts {
		value, err := parseVBScriptTypeAtomWithBudget(strings.TrimSpace(part), budget)
		if err != nil {
			return vbscriptType{}, err
		}
		parsed = append(parsed, value)
	}
	return makeVBScriptTypeUnion(parsed...), nil
}

func splitVBScriptTypeUnion(text string) ([]string, error) {
	if len(text) > vbscriptTypeParseInputLengthLimit {
		return nil, newVBScriptTypeParseError("input length", vbscriptTypeParseInputLengthLimit, len(text))
	}
	return splitVBScriptTypeUnionWithBudget(strings.TrimSpace(text), &vbscriptTypeParseBudget{})
}

func splitVBScriptTypeUnionWithBudget(text string, budget *vbscriptTypeParseBudget) ([]string, error) {
	parts := make([]string, 0, 2)
	start := 0
	quote := byte(0)
	braceDepth := 0
	parenDepth := 0
	bracketDepth := 0
	armHasContent := false
	for index := 0; index < len(text); index++ {
		char := text[index]
		if char != '|' && char != ' ' && char != '\t' && char != '\r' && char != '\n' {
			armHasContent = true
		}
		if quote != 0 {
			if char == '\\' {
				index++
				continue
			}
			if char == quote {
				quote = 0
			}
			continue
		}
		switch char {
		case '\'', '"':
			quote = char
		case '`':
			end, err := scanVBScriptTemplateWithBudget(text, index, budget)
			if err != nil {
				return nil, err
			}
			index = end - 1
		case '{':
			braceDepth++
		case '}':
			if braceDepth == 0 {
				return nil, errors.New("unexpected closing brace")
			}
			braceDepth--
		case '(':
			parenDepth++
		case ')':
			if parenDepth == 0 {
				return nil, errors.New("unexpected closing parenthesis")
			}
			parenDepth--
		case '[':
			bracketDepth++
		case ']':
			if bracketDepth == 0 {
				return nil, errors.New("unexpected closing bracket")
			}
			bracketDepth--
		case '|':
			if braceDepth == 0 && parenDepth == 0 && bracketDepth == 0 {
				if len(parts)+1 > vbscriptTypeParseUnionArmLimit {
					return nil, newVBScriptTypeParseError("union arms", vbscriptTypeParseUnionArmLimit, len(parts)+1)
				}
				if !armHasContent {
					return nil, errors.New("empty union member")
				}
				parts = append(parts, text[start:index])
				start = index + 1
				armHasContent = false
			}
		}
	}
	if quote != 0 {
		return nil, errors.New("unterminated quoted literal")
	}
	if braceDepth != 0 || parenDepth != 0 || bracketDepth != 0 {
		return nil, errors.New("unbalanced type expression")
	}
	if !armHasContent {
		return nil, errors.New("empty union member")
	}
	if len(parts)+1 > vbscriptTypeParseUnionArmLimit {
		return nil, newVBScriptTypeParseError("union arms", vbscriptTypeParseUnionArmLimit, len(parts)+1)
	}
	parts = append(parts, text[start:])
	return parts, nil
}

func parseVBScriptTypeAtomWithBudget(text string, budget *vbscriptTypeParseBudget) (vbscriptType, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return vbscriptType{}, errors.New("empty type expression")
	}
	if err := budget.consumeNodes(1); err != nil {
		return vbscriptType{}, err
	}
	if text[0] == '"' || text[0] == '\'' {
		value, end, err := parseVBScriptAnnotationQuotedLiteral(text, 0)
		if err != nil {
			return vbscriptType{}, err
		}
		if end != len(text) {
			return vbscriptType{}, errors.New("unexpected text after string literal")
		}
		return vbscriptType{kind: vbscriptTypeStringLiteral, value: value}, nil
	}
	if text[0] == '`' {
		return parseVBScriptTemplateWithBudget(text, budget)
	}
	if isVBScriptNumberTypeLiteral(text) {
		return vbscriptType{kind: vbscriptTypeNumberLiteral, value: text}, nil
	}
	if strings.EqualFold(text, "true") || strings.EqualFold(text, "false") {
		value := "False"
		if strings.EqualFold(text, "true") {
			value = "True"
		}
		return vbscriptType{kind: vbscriptTypeBooleanLiteral, value: value}, nil
	}
	if typeName := canonicalVBScriptPrimitiveType(text); typeName != "" {
		if strings.EqualFold(typeName, "Variant") {
			return vbscriptType{kind: vbscriptTypeUnknown, name: "Variant"}, nil
		}
		return vbscriptType{kind: vbscriptTypePrimitive, name: typeName}, nil
	}
	if !isVBScriptTypeName(text) {
		return vbscriptType{}, fmt.Errorf("invalid type name %q", text)
	}
	return vbscriptType{kind: vbscriptTypeObject, name: text}, nil
}

func canonicalVBScriptPrimitiveType(text string) string {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "variant":
		return "Variant"
	case "string":
		return "String"
	case "number":
		return "Number"
	case "boolean":
		return "Boolean"
	case "currency":
		return "Currency"
	case "date":
		return "Date"
	case "integer":
		return "Integer"
	case "long":
		return "Long"
	case "double":
		return "Double"
	case "single":
		return "Single"
	case "byte":
		return "Byte"
	case "decimal":
		return "Decimal"
	case "array":
		return "Array"
	case "error":
		return "Error"
	case "empty":
		return "Empty"
	case "null":
		return "Null"
	case "nothing":
		return "Nothing"
	case "object":
		return "Object"
	default:
		return ""
	}
}

func isVBScriptTypeName(text string) bool {
	if text == "" {
		return false
	}
	for index, char := range text {
		if index == 0 {
			if !isVBScriptTypeNameStart(char) {
				return false
			}
			continue
		}
		if !isVBScriptTypeNamePart(char) {
			return false
		}
	}
	return true
}

func isVBScriptTypeNameStart(char rune) bool {
	return char == '_' || char == '$' || char >= 'A' && char <= 'Z' || char >= 'a' && char <= 'z'
}

func isVBScriptTypeNamePart(char rune) bool {
	return isVBScriptTypeNameStart(char) || char >= '0' && char <= '9' || strings.ContainsRune(".:-", char)
}

func parseVBScriptAnnotationQuotedLiteral(text string, start int) (string, int, error) {
	if start < 0 || start >= len(text) || (text[start] != '\'' && text[start] != '"') {
		return "", start, errors.New("expected quoted literal")
	}
	quote := text[start]
	var builder strings.Builder
	for index := start + 1; index < len(text); index++ {
		char := text[index]
		if char == '\\' {
			if index+1 >= len(text) {
				return "", index, errors.New("unterminated quoted literal")
			}
			builder.WriteByte(text[index+1])
			index++
			continue
		}
		if char == quote {
			if quote == '"' && index+1 < len(text) && text[index+1] == '"' {
				builder.WriteByte('"')
				index++
				continue
			}
			return builder.String(), index + 1, nil
		}
		builder.WriteByte(char)
	}
	return "", len(text), errors.New("unterminated quoted literal")
}

func parseVBScriptSourceQuotedLiteral(text string, start int) (string, int, error) {
	if start < 0 || start >= len(text) || text[start] != '"' {
		return "", start, errors.New("expected quoted literal")
	}
	quote := text[start]
	var builder strings.Builder
	for index := start + 1; index < len(text); index++ {
		char := text[index]
		if char == quote {
			if index+1 < len(text) && text[index+1] == quote {
				builder.WriteByte(quote)
				index++
				continue
			}
			return builder.String(), index + 1, nil
		}
		builder.WriteByte(char)
	}
	return "", len(text), errors.New("unterminated quoted literal")
}

func isVBScriptNumberTypeLiteral(text string) bool {
	if text == "" {
		return false
	}
	_, ok := parseVBScriptNumericLiteral(text)
	return ok
}

func vbscriptNumericLiteralIdentity(text string) string {
	value, ok := parseVBScriptNumericLiteral(text)
	if !ok {
		return text
	}
	return value.identity()
}

func vbscriptNumericLiteralsEqual(left, right string) bool {
	return vbscriptNumericLiteralIdentity(left) == vbscriptNumericLiteralIdentity(right)
}

var vbscriptDecimalLiteralPattern = regexp.MustCompile(`^[+-]?(?:(?:[0-9]+(?:\.[0-9]*)?)|(?:\.[0-9]+))(?:[eE][+-]?[0-9]+)?$`)

const (
	// Decimal values are expanded only when both their coefficient and their
	// rendered decimal form fit this budget. Larger values stay symbolic so a
	// source literal cannot turn into an unbounded big.Int allocation.
	vbscriptNumericMaterializationDigitLimit = 4096
	// Exponent arithmetic is kept as bounded decimal text. This also prevents
	// an out-of-policy exponent spelling from causing proportional work beyond
	// the source text that introduced it.
	vbscriptNumericExponentDigitLimit = 4096
	// Radix literals do not have a symbolic conversion that can be compared to
	// decimal literals without expanding the value. Keep their conversion
	// bounded and reject larger spellings conservatively.
	vbscriptNumericRadixDigitLimit = 4096
)

type vbscriptNumericValue struct {
	numerator   *big.Int
	denominator *big.Int
	// fallback is a deterministic symbolic identity for a valid decimal whose
	// finite expansion is outside the materialization budget.
	fallback string
}

type vbscriptSignedDecimal struct {
	negative bool
	digits   string
}

type vbscriptDecimalCanonical struct {
	negative bool
	digits   string
	exponent vbscriptSignedDecimal
}

func zeroVBScriptSignedDecimal() vbscriptSignedDecimal {
	return vbscriptSignedDecimal{digits: "0"}
}

func parseVBScriptSignedDecimal(text string) (vbscriptSignedDecimal, bool) {
	if text == "" {
		return zeroVBScriptSignedDecimal(), true
	}
	negative := false
	if text[0] == '+' || text[0] == '-' {
		negative = text[0] == '-'
		text = text[1:]
	}
	if text == "" || len(text) > vbscriptNumericExponentDigitLimit {
		return vbscriptSignedDecimal{}, false
	}
	for _, char := range text {
		if char < '0' || char > '9' {
			return vbscriptSignedDecimal{}, false
		}
	}
	text = strings.TrimLeft(text, "0")
	if text == "" {
		return zeroVBScriptSignedDecimal(), true
	}
	return vbscriptSignedDecimal{negative: negative, digits: text}, true
}

func (value vbscriptSignedDecimal) String() string {
	if value.digits == "" || value.digits == "0" {
		return "0"
	}
	if value.negative {
		return "-" + value.digits
	}
	return value.digits
}

func compareVBScriptDecimalMagnitudes(left, right string) int {
	if len(left) < len(right) {
		return -1
	}
	if len(left) > len(right) {
		return 1
	}
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}

func addVBScriptDecimalMagnitudes(left, right string) string {
	if left == "0" {
		return right
	}
	if right == "0" {
		return left
	}
	length := len(left)
	if len(right) > length {
		length = len(right)
	}
	result := make([]byte, length+1)
	leftIndex, rightIndex, resultIndex := len(left)-1, len(right)-1, len(result)-1
	carry := byte(0)
	for resultIndex >= 0 {
		sum := carry
		if leftIndex >= 0 {
			sum += left[leftIndex] - '0'
			leftIndex--
		}
		if rightIndex >= 0 {
			sum += right[rightIndex] - '0'
			rightIndex--
		}
		result[resultIndex] = '0' + sum%10
		carry = sum / 10
		resultIndex--
	}
	return strings.TrimLeft(string(result), "0")
}

// subtractVBScriptDecimalMagnitudes subtracts right from left. The caller
// must provide magnitudes where left >= right.
func subtractVBScriptDecimalMagnitudes(left, right string) string {
	result := make([]byte, len(left))
	leftIndex, rightIndex, resultIndex := len(left)-1, len(right)-1, len(result)-1
	borrow := 0
	for resultIndex >= 0 {
		difference := int(left[leftIndex]-'0') - borrow
		if rightIndex >= 0 {
			difference -= int(right[rightIndex] - '0')
			rightIndex--
		}
		if difference < 0 {
			difference += 10
			borrow = 1
		} else {
			borrow = 0
		}
		result[resultIndex] = byte('0' + difference)
		leftIndex--
		resultIndex--
	}
	return strings.TrimLeft(string(result), "0")
}

func (value vbscriptSignedDecimal) addUnsignedMagnitude(negative bool, magnitude uint64) (vbscriptSignedDecimal, bool) {
	if magnitude == 0 {
		return value, true
	}
	right := strconv.FormatUint(magnitude, 10)
	if len(right) > vbscriptNumericExponentDigitLimit {
		return vbscriptSignedDecimal{}, false
	}
	if value.digits == "" || value.digits == "0" {
		return vbscriptSignedDecimal{negative: negative, digits: right}, true
	}
	if value.negative == negative {
		digits := addVBScriptDecimalMagnitudes(value.digits, right)
		if len(digits) > vbscriptNumericExponentDigitLimit {
			return vbscriptSignedDecimal{}, false
		}
		return vbscriptSignedDecimal{negative: negative, digits: digits}, true
	}
	comparison := compareVBScriptDecimalMagnitudes(value.digits, right)
	if comparison == 0 {
		return zeroVBScriptSignedDecimal(), true
	}
	if comparison > 0 {
		return vbscriptSignedDecimal{negative: value.negative, digits: subtractVBScriptDecimalMagnitudes(value.digits, right)}, true
	}
	return vbscriptSignedDecimal{negative: negative, digits: subtractVBScriptDecimalMagnitudes(right, value.digits)}, true
}

func canonicalVBScriptDecimalParts(negative bool, digits string, fractionDigits int, exponent string) (vbscriptDecimalCanonical, bool) {
	if fractionDigits < 0 {
		return vbscriptDecimalCanonical{}, false
	}
	digits = strings.TrimLeft(digits, "0")
	if digits == "" {
		return vbscriptDecimalCanonical{digits: "0", exponent: zeroVBScriptSignedDecimal()}, true
	}
	for _, char := range digits {
		if char < '0' || char > '9' {
			return vbscriptDecimalCanonical{}, false
		}
	}
	exponentValue, ok := parseVBScriptSignedDecimal(exponent)
	if !ok {
		return vbscriptDecimalCanonical{}, false
	}
	coefficientEnd := len(digits)
	for coefficientEnd > 0 && digits[coefficientEnd-1] == '0' {
		coefficientEnd--
	}
	trailingZeros := len(digits) - coefficientEnd
	exponentValue, ok = exponentValue.addUnsignedMagnitude(true, uint64(fractionDigits))
	if !ok {
		return vbscriptDecimalCanonical{}, false
	}
	exponentValue, ok = exponentValue.addUnsignedMagnitude(false, uint64(trailingZeros))
	if !ok {
		return vbscriptDecimalCanonical{}, false
	}
	return vbscriptDecimalCanonical{
		negative: negative,
		digits:   digits[:coefficientEnd],
		exponent: exponentValue,
	}, true
}

func (value vbscriptDecimalCanonical) symbolicIdentity() string {
	if value.digits == "" || value.digits == "0" {
		return "0"
	}
	if value.negative {
		return "symbolic:-" + value.digits + "e" + value.exponent.String()
	}
	return "symbolic:" + value.digits + "e" + value.exponent.String()
}

func (value vbscriptDecimalCanonical) materializationExponent() (int, bool) {
	if value.exponent.digits == "" || value.exponent.digits == "0" {
		return 0, true
	}
	magnitude, err := strconv.Atoi(value.exponent.digits)
	if err != nil || magnitude > vbscriptNumericMaterializationDigitLimit {
		return 0, false
	}
	if value.exponent.negative {
		return -magnitude, true
	}
	return magnitude, true
}

func (value vbscriptDecimalCanonical) materializable() (int, bool) {
	if value.digits == "" || value.digits == "0" || len(value.digits) > vbscriptNumericMaterializationDigitLimit {
		return 0, false
	}
	exponent, ok := value.materializationExponent()
	if !ok {
		return 0, false
	}
	if exponent >= 0 {
		if exponent > vbscriptNumericMaterializationDigitLimit-len(value.digits) {
			return 0, false
		}
		return exponent, true
	}
	// A negative exponent renders at least abs(exponent)+1 decimal digits
	// (including the leading zero before the decimal point). Keep that output
	// bounded as well as the denominator's power of ten.
	if -exponent+1 > vbscriptNumericMaterializationDigitLimit {
		return 0, false
	}
	return exponent, true
}

func parseVBScriptNumericLiteral(text string) (vbscriptNumericValue, bool) {
	if text == "" {
		return vbscriptNumericValue{}, false
	}
	lower := strings.ToLower(text)
	if strings.HasPrefix(lower, "&h") {
		if len(text) <= 2 {
			return vbscriptNumericValue{}, false
		}
		return parseVBScriptRadixLiteral(text[2:], 16)
	}
	if strings.HasPrefix(lower, "&o") {
		if len(text) <= 2 {
			return vbscriptNumericValue{}, false
		}
		return parseVBScriptRadixLiteral(text[2:], 8)
	}
	// The lexer accepts the legacy VB octal spelling &077 as a numeric token.
	// Keep the type model consistent with that tokenization while rejecting a
	// bare ampersand or any digit outside the octal range.
	if strings.HasPrefix(text, "&") {
		if len(text) <= 1 {
			return vbscriptNumericValue{}, false
		}
		return parseVBScriptRadixLiteral(text[1:], 8)
	}
	if !vbscriptDecimalLiteralPattern.MatchString(text) {
		return vbscriptNumericValue{}, false
	}
	return parseVBScriptDecimalLiteral(text)
}

func parseVBScriptRadixLiteral(digits string, base int) (vbscriptNumericValue, bool) {
	if digits == "" || len(digits) > vbscriptNumericRadixDigitLimit {
		return vbscriptNumericValue{}, false
	}
	value := new(big.Int)
	if _, ok := value.SetString(digits, base); !ok {
		return vbscriptNumericValue{}, false
	}
	return vbscriptNumericValue{numerator: value, denominator: big.NewInt(1)}, true
}

func parseVBScriptDecimalLiteral(text string) (vbscriptNumericValue, bool) {
	unsigned := text
	negative := false
	if unsigned[0] == '+' || unsigned[0] == '-' {
		negative = unsigned[0] == '-'
		unsigned = unsigned[1:]
	}
	mantissa := unsigned
	exponent := ""
	if index := strings.IndexAny(mantissa, "eE"); index >= 0 {
		exponent = mantissa[index+1:]
		mantissa = mantissa[:index]
	}
	exponentDigits := exponent
	if strings.HasPrefix(exponentDigits, "+") || strings.HasPrefix(exponentDigits, "-") {
		exponentDigits = exponentDigits[1:]
	}
	if len(exponentDigits) > vbscriptNumericExponentDigitLimit {
		return vbscriptNumericValue{}, false
	}
	integerPart, fractionPart := mantissa, ""
	if index := strings.IndexByte(mantissa, '.'); index >= 0 {
		integerPart, fractionPart = mantissa[:index], mantissa[index+1:]
	}
	digits := integerPart + fractionPart
	digits = strings.TrimLeft(digits, "0")
	if digits == "" {
		digits = "0"
	}
	canonical, ok := canonicalVBScriptDecimalParts(negative, digits, len(fractionPart), exponent)
	if !ok {
		return vbscriptNumericValue{}, false
	}
	if canonical.digits == "0" {
		return vbscriptNumericValue{numerator: big.NewInt(0), denominator: big.NewInt(1)}, true
	}
	exponentValue, materializable := canonical.materializable()
	if !materializable {
		return vbscriptNumericValue{fallback: canonical.symbolicIdentity()}, true
	}
	numerator := new(big.Int)
	if _, ok := numerator.SetString(canonical.digits, 10); !ok {
		return vbscriptNumericValue{}, false
	}
	if canonical.negative {
		numerator.Neg(numerator)
	}
	if exponentValue >= 0 {
		factor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(exponentValue)), nil)
		numerator.Mul(numerator, factor)
		return vbscriptNumericValue{numerator: numerator, denominator: big.NewInt(1)}, true
	}
	denominator := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(-exponentValue)), nil)
	return vbscriptNumericValue{numerator: numerator, denominator: denominator}, true
}

func (value vbscriptNumericValue) identity() string {
	if value.fallback != "" {
		return value.fallback
	}
	if value.numerator == nil || value.denominator == nil {
		return ""
	}
	rational := new(big.Rat).SetFrac(value.numerator, value.denominator)
	if rational.Sign() == 0 {
		return "0"
	}
	if rational.Denom().Cmp(big.NewInt(1)) == 0 {
		return rational.Num().String()
	}
	// Decimal and radix literals only produce denominators composed of 2 and
	// 5. Render the reduced value as a finite decimal so ordinary identity
	// strings retain the historical 1.0 -> 1 and 0.50 -> 0.5 behavior.
	// Keep this defensive guard in place for values constructed by callers
	// other than the bounded decimal parser as well.
	if rational.Denom().BitLen() > vbscriptNumericMaterializationDigitLimit*4 {
		return rational.Num().String() + "/" + rational.Denom().String()
	}
	denominator := new(big.Int).Set(rational.Denom())
	twoFactors, fiveFactors := 0, 0
	for new(big.Int).Mod(denominator, big.NewInt(2)).Sign() == 0 {
		denominator.Div(denominator, big.NewInt(2))
		twoFactors++
	}
	for new(big.Int).Mod(denominator, big.NewInt(5)).Sign() == 0 {
		denominator.Div(denominator, big.NewInt(5))
		fiveFactors++
	}
	if denominator.Cmp(big.NewInt(1)) != 0 {
		return rational.Num().String() + "/" + rational.Denom().String()
	}
	decimals := twoFactors
	if fiveFactors > decimals {
		decimals = fiveFactors
	}
	multiplier := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
	scaled := new(big.Int).Mul(rational.Num(), multiplier)
	scaled.Quo(scaled, rational.Denom())
	text := scaled.String()
	negative := strings.HasPrefix(text, "-")
	if negative {
		text = text[1:]
	}
	if decimals > 0 {
		for len(text) <= decimals {
			text = "0" + text
		}
		text = text[:len(text)-decimals] + "." + text[len(text)-decimals:]
		text = strings.TrimRight(strings.TrimRight(text, "0"), ".")
	}
	if negative {
		text = "-" + text
	}
	return text
}

func scanVBScriptTemplateWithBudget(text string, start int, budget *vbscriptTypeParseBudget) (int, error) {
	if start < 0 || start >= len(text) || text[start] != '`' {
		return start, errors.New("expected template literal")
	}
	if err := budget.enterTemplateScan(); err != nil {
		return start, err
	}
	defer budget.leaveTemplateScan()
	for index := start + 1; index < len(text); index++ {
		if text[index] == '\\' {
			index++
			continue
		}
		if text[index] == '`' {
			return index + 1, nil
		}
		if text[index] != '$' || index+1 >= len(text) || text[index+1] != '{' {
			continue
		}
		end, err := scanVBScriptTemplateExpressionWithBudget(text, index+2, budget)
		if err != nil {
			return start, err
		}
		index = end
	}
	return len(text), errors.New("unterminated template literal")
}

func scanVBScriptTemplateExpressionWithBudget(text string, start int, budget *vbscriptTypeParseBudget) (int, error) {
	if start < 0 || start > len(text) {
		return start, errors.New("invalid template expression start")
	}
	depth := 1
	var quote byte
	for index := start; index < len(text); index++ {
		char := text[index]
		if quote != 0 {
			if char == '\\' {
				index++
				continue
			}
			if char == quote {
				if quote == '"' && index+1 < len(text) && text[index+1] == '"' {
					index++
					continue
				}
				quote = 0
			}
			continue
		}
		if char == '\'' || char == '"' {
			quote = char
			continue
		}
		if char == '\\' {
			index++
			continue
		}
		if char == '`' {
			end, err := scanVBScriptTemplateWithBudget(text, index, budget)
			if err != nil {
				return len(text), err
			}
			index = end - 1
			continue
		}
		switch char {
		case '{':
			depth++
			if depth > vbscriptTypeParseNestingLimit {
				return len(text), newVBScriptTypeParseError("nesting depth", vbscriptTypeParseNestingLimit, depth)
			}
		case '}':
			depth--
			if depth == 0 {
				return index, nil
			}
		}
	}
	return len(text), errors.New("unterminated template expression")
}

func parseVBScriptTemplateWithBudget(text string, budget *vbscriptTypeParseBudget) (vbscriptType, error) {
	end, err := scanVBScriptTemplateWithBudget(text, 0, budget)
	if err != nil {
		return vbscriptType{}, err
	}
	if end != len(text) {
		return vbscriptType{}, errors.New("unexpected text after template literal")
	}
	parts := make([]vbscriptTemplatePart, 0, 2)
	staticStart := 1
	for index := 1; index < end-1; index++ {
		if text[index] == '\\' {
			index++
			continue
		}
		if text[index] != '$' || index+1 >= end-1 || text[index+1] != '{' {
			continue
		}
		exprEnd, exprErr := scanVBScriptTemplateExpressionWithBudget(text, index+2, budget)
		if exprErr != nil {
			return vbscriptType{}, fmt.Errorf("invalid template expression: %w", exprErr)
		}
		if exprEnd >= end-1 {
			return vbscriptType{}, errors.New("invalid template expression")
		}
		literal := text[staticStart:index]
		decoded, decodeErr := decodeVBScriptTemplateStaticLiteral(literal)
		if decodeErr != nil {
			return vbscriptType{}, fmt.Errorf("invalid template literal: %w", decodeErr)
		}
		parts = append(parts, vbscriptTemplatePart{literal: literal, decoded: decoded})
		expr, exprErr := parseVBScriptTypeWithBudget(text[index+2:exprEnd], budget)
		if exprErr != nil {
			return vbscriptType{}, fmt.Errorf("invalid template expression: %w", exprErr)
		}
		if err := budget.consumeTemplateParts(2); err != nil {
			return vbscriptType{}, err
		}
		parts = append(parts, vbscriptTemplatePart{expr: &expr})
		index = exprEnd
		staticStart = index + 1
	}
	if err := budget.consumeTemplateParts(1); err != nil {
		return vbscriptType{}, err
	}
	literal := text[staticStart : end-1]
	decoded, decodeErr := decodeVBScriptTemplateStaticLiteral(literal)
	if decodeErr != nil {
		return vbscriptType{}, fmt.Errorf("invalid template literal: %w", decodeErr)
	}
	parts = append(parts, vbscriptTemplatePart{literal: literal, decoded: decoded})
	return vbscriptType{kind: vbscriptTypeTemplate, template: parts}, nil
}

func makeVBScriptTypeUnion(types ...vbscriptType) vbscriptType {
	parts := make([]vbscriptType, 0, len(types))
	seen := map[string]struct{}{}
	for _, value := range types {
		if value.kind == vbscriptTypeUnion {
			valueParts := value.parts
			for _, part := range valueParts {
				appendVBScriptTypeUnionPart(&parts, seen, part)
			}
			continue
		}
		appendVBScriptTypeUnionPart(&parts, seen, value)
	}
	if len(parts) == 0 {
		return vbscriptType{kind: vbscriptTypeUnknown, name: "Variant"}
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return vbscriptType{kind: vbscriptTypeUnion, parts: parts}
}

func appendVBScriptTypeUnionPart(parts *[]vbscriptType, seen map[string]struct{}, value vbscriptType) {
	base := value.primitiveName()
	if value.isLiteral() {
		for _, existing := range *parts {
			if existing.kind == vbscriptTypePrimitive && strings.EqualFold(existing.primitiveName(), base) {
				return
			}
		}
	}
	key := value.identity()
	if _, exists := seen[key]; exists {
		return
	}
	// A broad primitive subsumes its literal forms. Keep the first occurrence
	// so annotation display remains stable and deterministic.
	if value.kind == vbscriptTypePrimitive {
		for index := 0; index < len(*parts); index++ {
			if (*parts)[index].isLiteral() && strings.EqualFold((*parts)[index].primitiveName(), base) {
				delete(seen, (*parts)[index].identity())
				*parts = append((*parts)[:index], (*parts)[index+1:]...)
				index--
			}
		}
	}
	seen[key] = struct{}{}
	*parts = append(*parts, value)
}

func vbscriptTypeUnionFromStrings(values ...string) vbscriptType {
	parsed := make([]vbscriptType, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		part, err := parseVBScriptType(value)
		if err != nil {
			continue
		}
		parsed = append(parsed, part)
	}
	return widenVBScriptLiteralUnion(makeVBScriptTypeUnion(parsed...))
}

func mergeVBScriptMutableTypes(current, next vbscriptType) vbscriptType {
	if current.kind == vbscriptTypeInvalid {
		return next
	}
	if current.isUnknown() || next.isUnknown() {
		// Unknown is an alternative value, not an absent observation. Keeping it
		// in the merged state prevents a finite literal union from claiming that
		// known assignments are exhaustive.
		return vbscriptType{kind: vbscriptTypeUnknown, name: "Variant"}
	}
	return widenVBScriptLiteralUnion(makeVBScriptTypeUnion(current, next))
}

func widenVBScriptLiteralUnion(value vbscriptType) vbscriptType {
	if value.kind != vbscriptTypeUnion {
		return value
	}
	literalCounts := map[string]int{}
	totalLiterals := 0
	for _, part := range value.parts {
		if part.isLiteral() {
			literalCounts[strings.ToLower(part.primitiveName())]++
			totalLiterals++
		}
	}
	if totalLiterals == 0 || totalLiterals <= vbscriptLiteralUnionLimit {
		return value
	}
	parts := make([]vbscriptType, 0, len(value.parts))
	widened := map[string]bool{}
	for base := range literalCounts {
		widened[base] = true
	}
	for _, part := range value.parts {
		base := strings.ToLower(part.primitiveName())
		if widened[base] && part.isLiteral() {
			part = vbscriptType{kind: vbscriptTypePrimitive, name: part.primitiveName()}
		}
		parts = append(parts, part)
	}
	return makeVBScriptTypeUnion(parts...)
}

func inferVBScriptValueLiteralType(value string) vbscriptType {
	value = strings.TrimSpace(value)
	if value == "" {
		return vbscriptType{kind: vbscriptTypeUnknown, name: "Variant"}
	}
	if value[0] == '"' || value[0] == '\'' {
		if literal, end, err := parseVBScriptSourceQuotedLiteral(value, 0); err == nil && end == len(value) {
			return vbscriptType{kind: vbscriptTypeStringLiteral, value: literal}
		}
	}
	if isVBScriptNumberTypeLiteral(value) {
		return vbscriptType{kind: vbscriptTypeNumberLiteral, value: value}
	}
	if strings.EqualFold(value, "true") || strings.EqualFold(value, "false") {
		parsed, _ := parseVBScriptType(value)
		return parsed
	}
	if broad := inferVBValueType(value); broad != "" {
		if parsed, err := parseVBScriptType(broad); err == nil {
			return parsed
		}
	}
	return vbscriptType{kind: vbscriptTypeUnknown, name: "Variant"}
}

func vbscriptTypeExpressionsCompatible(expectedText, actualText string) bool {
	expected, expectedErr := parseVBScriptType(expectedText)
	actualAnnotation, actualAnnotationErr := parseVBScriptType(actualText)
	actualSource, actualSourceErr := parseVBScriptSourceStringType(actualText)
	if isVBScriptTypeParseLimitError(expectedErr) || isVBScriptTypeParseLimitError(actualAnnotationErr) || isVBScriptTypeParseLimitError(actualSourceErr) {
		return false
	}
	if expectedErr == nil {
		if actualAnnotationErr == nil && vbscriptTypeAssignable(expected, actualAnnotation) {
			return true
		}
		if actualSourceErr == nil && vbscriptTypeAssignable(expected, actualSource) {
			return true
		}
	}
	if expectedErr != nil || actualAnnotationErr != nil {
		return vbscriptTypesCompatible(expectedText, actualText)
	}
	return vbscriptTypeAssignable(expected, actualAnnotation)
}

func parseVBScriptSourceStringType(text string) (vbscriptType, error) {
	text = strings.TrimSpace(text)
	if text == "" || (text[0] != '"' && text[0] != '\'') {
		return vbscriptType{}, errors.New("expected source string literal")
	}
	literal, end, err := parseVBScriptSourceQuotedLiteral(text, 0)
	if err != nil {
		return vbscriptType{}, err
	}
	if end != len(text) {
		return vbscriptType{}, errors.New("unexpected text after source string literal")
	}
	return vbscriptType{kind: vbscriptTypeStringLiteral, value: literal}, nil
}

func vbscriptCommonReturnType(leftText, rightText string) (string, bool) {
	leftText = strings.TrimSpace(leftText)
	rightText = strings.TrimSpace(rightText)
	if leftText == "" {
		return rightText, true
	}
	if rightText == "" {
		return leftText, true
	}
	left, leftErr := parseVBScriptType(leftText)
	right, rightErr := parseVBScriptType(rightText)
	if leftErr != nil || rightErr != nil {
		if isVBScriptTypeParseLimitError(leftErr) || isVBScriptTypeParseLimitError(rightErr) {
			return "", false
		}
		if strings.EqualFold(leftText, rightText) {
			return canonicalVBScriptReturnText(leftText, rightText), true
		}
		return "", false
	}
	common, ok := commonVBScriptReturnTypes(left, right)
	if !ok {
		return "", false
	}
	return common.String(), true
}

func commonVBScriptReturnTypes(left, right vbscriptType) (vbscriptType, bool) {
	if left.isUnknown() || right.isUnknown() {
		return vbscriptType{kind: vbscriptTypeUnknown, name: "Variant"}, true
	}
	if left.kind == vbscriptTypeUnion || right.kind == vbscriptTypeUnion {
		return canonicalVBScriptReturnUnion(left, right), true
	}
	if left.kind == vbscriptTypePrimitive && right.kind == vbscriptTypePrimitive &&
		!strings.EqualFold(left.name, right.name) &&
		isVBScriptNumericFamilyType(left.name) && isVBScriptNumericFamilyType(right.name) {
		return vbscriptType{kind: vbscriptTypePrimitive, name: "Number"}, true
	}
	if strings.EqualFold(left.String(), right.String()) {
		if left.String() <= right.String() {
			return left, true
		}
		return right, true
	}
	if vbscriptTypeAssignable(left, right) {
		return left, true
	}
	if vbscriptTypeAssignable(right, left) {
		return right, true
	}
	if strings.EqualFold(left.primitiveName(), right.primitiveName()) && (left.isLiteral() && right.isLiteral() || left.kind == vbscriptTypeTemplate || right.kind == vbscriptTypeTemplate) {
		return canonicalVBScriptReturnUnion(left, right), true
	}
	return vbscriptType{}, false
}

func canonicalVBScriptReturnUnion(values ...vbscriptType) vbscriptType {
	merged := widenVBScriptLiteralUnion(makeVBScriptTypeUnion(values...))
	if merged.kind != vbscriptTypeUnion {
		return merged
	}
	sort.SliceStable(merged.parts, func(i, j int) bool {
		left := strings.ToLower(merged.parts[i].String())
		right := strings.ToLower(merged.parts[j].String())
		if left != right {
			return left < right
		}
		return merged.parts[i].String() < merged.parts[j].String()
	})
	return merged
}

func canonicalVBScriptReturnText(left, right string) string {
	leftLower := strings.ToLower(left)
	rightLower := strings.ToLower(right)
	if leftLower < rightLower || strings.EqualFold(left, right) && left <= right {
		return left
	}
	return right
}

func vbscriptTypeAssignable(expected, actual vbscriptType) bool {
	if expected.isUnknown() || actual.isUnknown() {
		return true
	}
	if expected.kind == vbscriptTypeUnion {
		if actual.kind == vbscriptTypeUnion {
			for _, actualPart := range actual.parts {
				if !vbscriptTypeAssignable(expected, actualPart) {
					return false
				}
			}
			return true
		}
		for _, expectedPart := range expected.parts {
			if vbscriptTypeAssignable(expectedPart, actual) {
				return true
			}
		}
		return false
	}
	if actual.kind == vbscriptTypeUnion {
		for _, actualPart := range actual.parts {
			if !vbscriptTypeAssignable(expected, actualPart) {
				return false
			}
		}
		return true
	}
	if expected.kind == vbscriptTypeTemplate {
		if actual.kind == vbscriptTypeTemplate {
			return expected.String() == actual.String()
		}
		if actual.kind != vbscriptTypeStringLiteral {
			return false
		}
		return vbscriptTemplateMatches(expected, actual.value)
	}
	if expected.kind == vbscriptTypeStringLiteral {
		return actual.kind == vbscriptTypeStringLiteral && expected.value == actual.value
	}
	if expected.kind == vbscriptTypeNumberLiteral {
		return actual.kind == vbscriptTypeNumberLiteral && vbscriptNumericLiteralsEqual(expected.value, actual.value)
	}
	if expected.kind == vbscriptTypeBooleanLiteral {
		return actual.kind == vbscriptTypeBooleanLiteral && strings.EqualFold(expected.value, actual.value)
	}
	if actual.kind == vbscriptTypeTemplate {
		return expected.kind == vbscriptTypePrimitive && strings.EqualFold(expected.name, "String")
	}
	if actual.isLiteral() {
		return strings.EqualFold(expected.primitiveName(), actual.primitiveName()) ||
			isVBScriptNumericFamilyType(expected.primitiveName()) && isVBScriptNumericFamilyType(actual.primitiveName())
	}
	if expected.kind == vbscriptTypePrimitive && actual.kind == vbscriptTypePrimitive {
		return strings.EqualFold(expected.name, actual.name) ||
			isVBScriptNumericFamilyType(expected.name) && isVBScriptNumericFamilyType(actual.name)
	}
	return strings.EqualFold(expected.name, actual.name)
}

func vbscriptTemplateMatches(template vbscriptType, value string) bool {
	context := &vbscriptTemplateMatchContext{}
	return vbscriptTemplateMatchesAt(context, template, value, 0, "", true, func(end int) bool {
		return end == len(value)
	})
}

// Matching is deliberately bounded independently of parsing. An inferred
// string can be larger than an annotation, and an unconstrained interpolation
// can otherwise revisit every suffix for every template part.
const vbscriptTemplateMatchStepLimit = 1 << 20

type vbscriptTemplateMatchContext struct {
	steps int
	depth int
}

func (context *vbscriptTemplateMatchContext) consumeStep() bool {
	if context.steps >= vbscriptTemplateMatchStepLimit {
		return false
	}
	context.steps++
	return true
}

func (context *vbscriptTemplateMatchContext) enterTemplate() bool {
	if context.depth >= vbscriptTypeParseNestingLimit {
		return false
	}
	context.depth++
	return true
}

func (context *vbscriptTemplateMatchContext) leaveTemplate() {
	if context.depth > 0 {
		context.depth--
	}
}

func vbscriptTemplateMatchesAt(context *vbscriptTemplateMatchContext, template vbscriptType, value string, start int, outerNextLiteral string, outerTerminal bool, continuation func(int) bool) bool {
	if start < 0 || start > len(value) || !context.enterTemplate() {
		return false
	}
	defer context.leaveTemplate()
	type matchState struct {
		part   int
		offset int
	}
	memo := map[matchState]bool{}
	visited := map[matchState]bool{}
	var matchPart func(int, int) bool
	matchPart = func(partIndex, offset int) bool {
		state := matchState{part: partIndex, offset: offset}
		if visited[state] {
			return memo[state]
		}
		visited[state] = true
		if !context.consumeStep() {
			memo[state] = false
			return false
		}
		if partIndex >= len(template.template) {
			memo[state] = continuation(offset)
			return memo[state]
		}
		part := template.template[partIndex]
		if part.expr == nil {
			memo[state] = offset <= len(value) && strings.HasPrefix(value[offset:], part.decoded) &&
				matchPart(partIndex+1, offset+len(part.decoded))
			return memo[state]
		}
		nextLiteral := ""
		terminal := partIndex+1 >= len(template.template)
		if !terminal && template.template[partIndex+1].expr == nil {
			nextLiteral = template.template[partIndex+1].decoded
			terminal = partIndex+2 >= len(template.template)
		}
		if nextLiteral == "" && terminal {
			nextLiteral = outerNextLiteral
			terminal = outerTerminal
		}
		memo[state] = vbscriptTemplateExpressionMatches(context, *part.expr, value, offset, nextLiteral, terminal, func(end int) bool {
			return matchPart(partIndex+1, end)
		})
		return memo[state]
	}
	return matchPart(0, start)
}

const vbscriptTemplateNumericCandidateLimit = vbscriptNumericMaterializationDigitLimit*2 + 64

func vbscriptTemplateExpressionMatches(context *vbscriptTemplateMatchContext, value vbscriptType, text string, start int, nextLiteral string, terminal bool, continuation func(int) bool) bool {
	if value.kind == vbscriptTypeUnion {
		for _, part := range value.parts {
			if vbscriptTemplateExpressionMatches(context, part, text, start, nextLiteral, terminal, continuation) {
				return true
			}
		}
		return false
	}
	switch value.kind {
	case vbscriptTypeStringLiteral:
		end := start + len(value.value)
		return end <= len(text) && text[start:end] == value.value && continuation(end)
	case vbscriptTypeNumberLiteral:
		return vbscriptTemplateNumericMatches(text, start, vbscriptNumericLiteralIdentity(value.value), nextLiteral, terminal, continuation)
	case vbscriptTypeBooleanLiteral:
		end := start + len(value.value)
		return end <= len(text) && text[start:end] == value.value && continuation(end)
	case vbscriptTypePrimitive:
		switch strings.ToLower(value.name) {
		case "number", "integer", "long", "double", "single", "byte", "currency", "decimal":
			return vbscriptTemplateNumericMatches(text, start, "", nextLiteral, terminal, continuation)
		case "boolean":
			for _, literal := range []string{"True", "False"} {
				end := start + len(literal)
				if end <= len(text) && strings.EqualFold(text[start:end], literal) && continuation(end) {
					return true
				}
			}
			return false
		}
	case vbscriptTypeTemplate:
		return vbscriptTemplateMatchesAt(context, value, text, start, nextLiteral, terminal, continuation)
	}
	// An unconstrained String or object expression retains the historical
	// wildcard behavior. Matching is structural and memoized by the caller, so
	// a large template never needs a generated regular expression.
	for end := start; end <= len(text); end++ {
		if continuation(end) {
			return true
		}
	}
	return false
}

func vbscriptTemplateNumericMatches(text string, start int, expectedIdentity, nextLiteral string, terminal bool, continuation func(int) bool) bool {
	if nextLiteral != "" {
		for search := start; search < len(text); {
			relative := strings.Index(text[search:], nextLiteral)
			if relative < 0 {
				break
			}
			end := search + relative
			if end > start {
				candidate, ok := parseVBScriptNumericLiteral(text[start:end])
				if ok && (expectedIdentity == "" || candidate.identity() == expectedIdentity) && continuation(end) {
					return true
				}
			}
			search = end + 1
		}
		return false
	}
	if terminal {
		candidate, ok := parseVBScriptNumericLiteral(text[start:])
		if !ok || expectedIdentity != "" && candidate.identity() != expectedIdentity {
			return false
		}
		return continuation(len(text))
	}
	endLimit := len(text)
	if maximum := start + vbscriptTemplateNumericCandidateLimit; endLimit > maximum {
		endLimit = maximum
	}
	for end := start + 1; end <= endLimit; end++ {
		candidate, ok := parseVBScriptNumericLiteral(text[start:end])
		if !ok || expectedIdentity != "" && candidate.identity() != expectedIdentity {
			continue
		}
		if continuation(end) {
			return true
		}
	}
	return false
}
