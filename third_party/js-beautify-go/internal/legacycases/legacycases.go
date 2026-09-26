// Package legacycases loads the imported js-beautify parity corpus.
package legacycases

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Option represents one option entry in the legacy corpus.
type Option struct {
	// Name is the js-beautify option name.
	Name string `json:"name"`
	// Value is the raw JSON option value.
	Value any `json:"value"`
}

// TestCase represents one raw legacy corpus test.
type TestCase struct {
	// Comment stores the optional legacy test comment.
	Comment any `json:"comment"`
	// Fragment marks an HTML fragment test.
	Fragment bool `json:"fragment"`
	// Options stores test-specific options.
	Options []Option `json:"options"`
	// Input stores the primary input template.
	Input any `json:"input"`
	// InputAlt stores the alternate legacy input key.
	InputAlt any `json:"input_"`
	// Output stores the expected output template.
	Output any `json:"output"`
	// Unchanged stores input that should remain unchanged.
	Unchanged any `json:"unchanged"`
}

// Group represents a named legacy corpus group.
type Group struct {
	// Name is the group name used in generated case names.
	Name string `json:"name"`
	// Template is the optional template applied to matrix values.
	Template string `json:"template"`
	// Options stores group-level options.
	Options []Option `json:"options"`
	// Matrix stores per-group parameter combinations.
	Matrix []map[string]any `json:"matrix"`
	// Tests stores raw tests in this group.
	Tests []TestCase `json:"tests"`
}

// Data is the top-level legacy corpus JSON shape.
type Data struct {
	// DefaultOptions stores corpus-wide default options.
	DefaultOptions []Option `json:"default_options"`
	// Groups stores all test groups.
	Groups []Group `json:"groups"`
}

// Case is a fully expanded table-driven test case.
type Case struct {
	// Name is the stable generated test name.
	Name string
	// Options stores merged formatter options.
	Options map[string]any
	// Input is the rendered input source.
	Input string
	// Expected is the rendered expected output.
	Expected string
}

// Load returns expanded legacy cases for language.
func Load(language string) ([]Case, error) {
	path := filepath.Join("..", "..", "testdata", "legacy", language+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		path = filepath.Join("testdata", "legacy", language+".json")
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, err
	}
	var parsed Data
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, err
	}
	defaults := optionsFrom(parsed.DefaultOptions)
	var out []Case
	for _, group := range parsed.Groups {
		matrix := group.Matrix
		if len(matrix) == 0 {
			matrix = []map[string]any{{}}
		}
		for _, context := range matrix {
			opts := clone(defaults)
			for _, opt := range group.Options {
				opts[opt.Name] = parseValue(renderWithTemplate(valueString(opt.Value), context, group.Template))
			}
			if matrixOptions, ok := context["options"].([]any); ok {
				for _, item := range matrixOptions {
					raw, _ := item.(map[string]any)
					name, _ := raw["name"].(string)
					opts[name] = parseValue(renderWithTemplate(valueString(raw["value"]), context, group.Template))
				}
			}
			for index, test := range group.Tests {
				caseOpts := clone(opts)
				for _, opt := range test.Options {
					caseOpts[opt.Name] = parseValue(renderWithTemplate(valueString(opt.Value), context, group.Template))
				}
				inputAny := firstNonNil(test.Input, test.InputAlt, test.Unchanged)
				if inputAny == nil {
					continue
				}
				input := renderWithTemplate(valueString(inputAny), context, group.Template)
				expectedAny := test.Output
				if expectedAny == nil {
					expectedAny = test.Unchanged
				}
				expected := input
				if expectedAny != nil {
					expected = renderWithTemplate(valueString(expectedAny), context, group.Template)
				}
				out = append(out, Case{
					Name:     fmt.Sprintf("%s/%03d", group.Name, index),
					Options:  caseOpts,
					Input:    input,
					Expected: expected,
				})
			}
		}
	}
	return out, nil
}

func optionsFrom(options []Option) map[string]any {
	out := map[string]any{}
	for _, option := range options {
		out[option.Name] = parseValue(valueString(option.Value))
	}
	return out
}

func parseValue(value string) any {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "{") || strings.HasPrefix(value, "[") {
		jsonish := strings.ReplaceAll(value, `'`, `"`)
		var parsed any
		if err := json.Unmarshal([]byte(jsonish), &parsed); err == nil {
			return parsed
		}
	}
	if len(value) >= 2 && ((value[0] == '\'' && value[len(value)-1] == '\'') || (value[0] == '"' && value[len(value)-1] == '"')) {
		unquoted, err := strconv.Unquote(strings.ReplaceAll(value, `'`, `"`))
		if err == nil {
			return unquoted
		}
		return value[1 : len(value)-1]
	}
	switch value {
	case "true":
		return true
	case "false":
		return false
	case "null":
		return nil
	}
	if n, err := strconv.Atoi(value); err == nil {
		return n
	}
	return value
}

func render(value string, context map[string]any) string {
	return renderWithTemplate(value, context, "")
}

func renderWithTemplate(value string, context map[string]any, template string) string {
	open, close := "{{", "}}"
	if template != "" {
		parts := strings.Split(template, " ")
		if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
			open, close = parts[0], parts[1]
		}
	}
	for key, item := range context {
		if key == "options" {
			continue
		}
		replacement := unescapeTemplateValue(valueString(item))
		value = strings.ReplaceAll(value, open+key+close, replacement)
		value = strings.ReplaceAll(value, open+"&"+key+close, replacement)
	}
	value = removeMissingPlaceholders(value, open, close)
	return value
}

func removeMissingPlaceholders(value, open, close string) string {
	for {
		start := strings.Index(value, open)
		if start < 0 {
			return value
		}
		end := strings.Index(value[start+len(open):], close)
		if end < 0 {
			return value
		}
		name := value[start+len(open) : start+len(open)+end]
		if strings.ContainsAny(name, " \n\t/") {
			return value
		}
		value = value[:start] + value[start+len(open)+end+len(close):]
	}
}

func unescapeTemplateValue(value string) string {
	value = strings.ReplaceAll(value, `\n`, "\n")
	value = strings.ReplaceAll(value, `\t`, "\t")
	value = strings.ReplaceAll(value, `\r`, "\r")
	return value
}

func valueString(value any) string {
	switch v := value.(type) {
	case string:
		return decodeLegacyStringExpression(v)
	case []any:
		parts := make([]string, len(v))
		for i, item := range v {
			parts[i] = valueString(item)
		}
		return strings.Join(parts, "\n")
	default:
		return fmt.Sprint(v)
	}
}

func decodeLegacyStringExpression(value string) string {
	if !strings.Contains(value, `\`) && !strings.Contains(value, "unicode_char(") && !strings.Contains(value, " + ") {
		return value
	}
	expr := "'" + value + "'"
	var out strings.Builder
	for i := 0; i < len(expr); {
		i = skipExpressionSpace(expr, i)
		if i >= len(expr) {
			break
		}
		switch {
		case expr[i] == '\'':
			part, next := readSingleQuotedExpressionString(expr, i)
			out.WriteString(part)
			i = next
		case strings.HasPrefix(expr[i:], "unicode_char("):
			i += len("unicode_char(")
			start := i
			for i < len(expr) && expr[i] >= '0' && expr[i] <= '9' {
				i++
			}
			codepoint, err := strconv.Atoi(expr[start:i])
			if err == nil {
				out.WriteRune(rune(codepoint))
			}
			if i < len(expr) && expr[i] == ')' {
				i++
			}
		case expr[i] == '+':
			i++
		case isLegacyIdentifierStart(expr[i]):
			start := i
			i++
			for i < len(expr) && isLegacyIdentifierPart(expr[i]) {
				i++
			}
			name := expr[start:i]
			if replacement, ok := legacyExpressionVariables[name]; ok {
				out.WriteString(replacement)
			} else {
				out.WriteString(name)
			}
		default:
			_, size := utf8.DecodeRuneInString(expr[i:])
			out.WriteString(expr[i : i+size])
			i += size
		}
	}
	return out.String()
}

var legacyExpressionVariables = map[string]string{
	"wrap_input_1": strings.Join([]string{
		`foo.bar().baz().cucumber((f && "sass") || (leans && mean));`,
		`Test_very_long_variable_name_this_should_never_wrap`,
		`.but_this_can`,
		`return between_return_and_expression_should_never_wrap.but_this_can`,
		`throw between_throw_and_expression_should_never_wrap.but_this_can`,
		`if (wraps_can_occur && inside_an_if_block) that_is_`,
		`.okay();`,
		`object_literal = {`,
		`    propertx: first_token + 12345678.99999E-6,`,
		`    property: first_token_should_never_wrap + but_this_can,`,
		`    propertz: first_token_should_never_wrap + !but_this_can,`,
		`    proper: "first_token_should_never_wrap" + "but_this_can"`,
		`}`,
	}, "\n"),
	"wrap_input_2": strings.Join([]string{
		`{`,
		`    foo.bar().baz().cucumber((f && "sass") || (leans && mean));`,
		`    Test_very_long_variable_name_this_should_never_wrap`,
		`.but_this_can`,
		`    return between_return_and_expression_should_never_wrap.but_this_can`,
		`    throw between_throw_and_expression_should_never_wrap.but_this_can`,
		`    if (wraps_can_occur && inside_an_if_block) that_is_`,
		`.okay();`,
		`    object_literal = {`,
		`        propertx: first_token + 12345678.99999E-6,`,
		`        property: first_token_should_never_wrap + but_this_can,`,
		`        propertz: first_token_should_never_wrap + !but_this_can,`,
		`        proper: "first_token_should_never_wrap" + "but_this_can"`,
		`    }`,
		`}`,
	}, "\n"),
}

func isLegacyIdentifierStart(ch byte) bool {
	return ch == '_' || (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z')
}

func isLegacyIdentifierPart(ch byte) bool {
	return isLegacyIdentifierStart(ch) || (ch >= '0' && ch <= '9')
}

func skipExpressionSpace(expr string, i int) int {
	for i < len(expr) && (expr[i] == ' ' || expr[i] == '\t' || expr[i] == '\n' || expr[i] == '\r') {
		i++
	}
	return i
}

func readSingleQuotedExpressionString(expr string, i int) (string, int) {
	i++
	var out strings.Builder
	for i < len(expr) {
		ch := expr[i]
		if ch == '\'' {
			return out.String(), i + 1
		}
		if ch == '\\' && i+1 < len(expr) {
			i++
			switch expr[i] {
			case 'n':
				out.WriteByte('\n')
			case 'r':
				out.WriteByte('\r')
			case 't':
				out.WriteByte('\t')
			case 'x':
				if i+2 < len(expr) {
					if value, err := strconv.ParseInt(expr[i+1:i+3], 16, 32); err == nil {
						out.WriteRune(rune(value))
						i += 2
					} else {
						out.WriteByte(expr[i])
					}
				} else {
					out.WriteByte(expr[i])
				}
			case 'u':
				if i+1 < len(expr) && expr[i+1] == '{' {
					end := strings.IndexByte(expr[i+2:], '}')
					if end >= 0 {
						raw := expr[i+2 : i+2+end]
						if value, err := strconv.ParseInt(raw, 16, 32); err == nil {
							out.WriteRune(rune(value))
							i += end + 2
						} else {
							out.WriteByte(expr[i])
						}
					} else {
						out.WriteByte(expr[i])
					}
				} else if i+4 < len(expr) {
					if value, err := strconv.ParseInt(expr[i+1:i+5], 16, 32); err == nil {
						out.WriteRune(rune(value))
						i += 4
					} else {
						out.WriteByte(expr[i])
					}
				} else {
					out.WriteByte(expr[i])
				}
			default:
				out.WriteByte(expr[i])
			}
			i++
			continue
		}
		out.WriteByte(ch)
		i++
	}
	return out.String(), i
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func clone(input map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range input {
		out[key] = value
	}
	return out
}
