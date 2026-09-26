package lspserver

import (
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
)

func evaluateNavigationVBStringFunction(name string, args [][]vbscript.Token, state *navigationVBState) navigationVBValue {
	// Evaluate each argument once, including its effects. Only finite literal
	// inputs are transformed; slicing placeholders would invent a known path.
	if len(args) > 6 {
		for _, arg := range args {
			if navigationVBExpressionBudgetExhausted(state) || navigationVBCancelled(state) {
				break
			}
			_ = evaluateNavigationVBExpression(arg, state)
		}
		return navigationVBExpressionUnknown()
	}
	inputs := make([][]navigationVBValue, len(args))
	for index, arg := range args {
		inputs[index] = evaluateNavigationVBExpression(arg, state).finiteCandidates()
	}
	combinations := [][]navigationVBValue{{}}
	truncated := false
	for _, values := range inputs {
		next := make([][]navigationVBValue, 0)
		for _, prefix := range combinations {
			for _, value := range values {
				if len(next) >= navigationVBFiniteValueLimit || !navigationVBExpressionWork(state, len(prefix)+1) {
					truncated = true
					break
				}
				next = append(next, append(append([]navigationVBValue(nil), prefix...), value))
			}
		}
		combinations = next
	}
	results := make([]navigationVBValue, 0, len(combinations)+1)
	for _, values := range combinations {
		result := navigationVBExpressionUnknown()
		if text, ok := navigationVBTransformString(name, values, state); ok {
			result = navigationVBValue{Kind: navigationVBValueLiteral, Primitive: navigationPrimitiveString, Text: text}
		}
		results = append(results, result)
	}
	if truncated {
		results = append(results, navigationVBExpressionUnknown())
	}
	return navigationMergeVBValueList(results)
}

func navigationVBTransformString(name string, values []navigationVBValue, state *navigationVBState) (string, bool) {
	for _, value := range values {
		if value.Kind != navigationVBValueLiteral || !navigationVBExpressionWork(state, len(value.Text)+1) {
			return "", false
		}
	}
	integer := func(index int) (int, bool) {
		if index >= len(values) || values[index].Primitive != navigationPrimitiveNumber {
			return 0, false
		}
		n, err := strconv.ParseInt(values[index].Text, 10, 32)
		return int(n), err == nil
	}
	if name == "chr" || name == "chrw" {
		n, ok := integer(0)
		if len(values) != 1 || !ok {
			return "", false
		}
		// Chr outside ASCII depends on the server code page. Keep it unknown.
		if name == "chr" {
			if n < 0 || n > 127 {
				return "", false
			}
		} else {
			if n < -32768 || n > 65535 {
				return "", false
			}
			n &= 0xffff
			if n >= 0xd800 && n <= 0xdfff {
				return "", false
			}
		}
		return string(rune(n)), true
	}
	if len(values) == 0 || values[0].Primitive != navigationPrimitiveString {
		return "", false
	}
	text := values[0].Text
	if name == "ltrim" || name == "rtrim" {
		if len(values) != 1 {
			return "", false
		}
		if name == "ltrim" {
			return strings.TrimLeft(text, " "), true
		}
		return strings.TrimRight(text, " "), true
	}
	// VBScript offsets count UTF-16 code units, not bytes or Unicode scalars.
	units := utf16.Encode([]rune(text))
	start, end := 0, len(units)
	if name == "replace" {
		if len(values) < 3 || len(values) > 6 || values[1].Primitive != navigationPrimitiveString || values[2].Primitive != navigationPrimitiveString {
			return "", false
		}
		count := -1
		if len(values) >= 4 {
			n, ok := integer(3)
			if !ok || n < 1 {
				return "", false
			}
			start = n - 1
		}
		if len(values) >= 5 {
			n, ok := integer(4)
			if !ok || n < -1 {
				return "", false
			}
			count = n
		}
		if len(values) == 6 {
			n, ok := integer(5)
			if !ok || n != 0 {
				return "", false
			}
		}
		suffix, ok := navigationVBUTF16Slice(units, start, end)
		if !ok {
			return "", false
		}
		find, replacement := values[1].Text, values[2].Text
		if find == "" || count == 0 {
			return suffix, true
		}
		occurrences := strings.Count(suffix, find)
		if count >= 0 && occurrences > count {
			occurrences = count
		}
		growth := len(replacement) - len(find)
		if growth > 0 && occurrences > navigationVBExpressionWorkLimit/growth {
			return "", false
		}
		outputSize := len(suffix) + occurrences*growth
		if !navigationVBExpressionWork(state, outputSize) {
			return "", false
		}
		return strings.Replace(suffix, find, replacement, count), true
	}
	n, ok := integer(1)
	if !ok || n < 0 {
		return "", false
	}
	switch name {
	case "left":
		if len(values) != 2 {
			return "", false
		}
		end = min(end, n)
	case "right":
		if len(values) != 2 {
			return "", false
		}
		start = max(0, end-n)
	case "mid":
		if len(values) < 2 || len(values) > 3 || n < 1 {
			return "", false
		}
		start = min(n-1, end)
		if len(values) == 3 {
			length, ok := integer(2)
			if !ok || length < 0 {
				return "", false
			}
			end = start + min(length, end-start)
		}
	default:
		return "", false
	}
	return navigationVBUTF16Slice(units, start, end)
}

func navigationVBUTF16Slice(units []uint16, start, end int) (string, bool) {
	start = min(start, len(units))
	if start > 0 && start < len(units) && units[start] >= 0xdc00 && units[start] <= 0xdfff {
		return "", false
	}
	if end > start && units[end-1] >= 0xd800 && units[end-1] <= 0xdbff {
		return "", false
	}
	return string(utf16.Decode(units[start:end])), true
}
