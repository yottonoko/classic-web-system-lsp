// Package javascript implements the JavaScript formatter.
package javascript

import "github.com/yottonoko/js-beautify-go/internal/core"

var validOperatorPositionValues = []string{"before-newline", "after-newline", "preserve-newline"}

// Options contains JavaScript-specific formatter options.
type Options struct {
	// BaseOptions stores options shared by all formatters.
	*core.BaseOptions
	// BracePreserveInline keeps inline blocks on one line when possible.
	BracePreserveInline bool
	// BraceStyle selects collapse, expand, end-expand, or none brace layout.
	BraceStyle string
	// UnindentChainedMethods aligns chained method calls with the base expression.
	UnindentChainedMethods bool
	// BreakChainedMethods prints chained method calls on separate lines.
	BreakChainedMethods bool
	// SpaceInParen inserts spaces inside parentheses.
	SpaceInParen bool
	// SpaceInEmptyParen inserts spaces inside empty parentheses.
	SpaceInEmptyParen bool
	// JSLintHappy applies the historical jslint_happy spacing behavior.
	JSLintHappy bool
	// SpaceAfterAnonFunction inserts a space before anonymous function parens.
	SpaceAfterAnonFunction bool
	// SpaceAfterNamedFunction inserts a space before named function parens.
	SpaceAfterNamedFunction bool
	// KeepArrayIndentation preserves existing array indentation.
	KeepArrayIndentation bool
	// SpaceBeforeConditional inserts a space before conditional parens.
	SpaceBeforeConditional bool
	// UnescapeStrings converts supported escape sequences to characters.
	UnescapeStrings bool
	// E4X enables legacy E4X token preservation.
	E4X bool
	// CommaFirst places commas at the start of continued lines.
	CommaFirst bool
	// OperatorPosition controls newline placement around operators.
	OperatorPosition string
	// TestOutputRaw bypasses normal output cleanup for parity tests.
	TestOutputRaw bool
}

// NewOptions normalizes JavaScript options and applies defaults.
func NewOptions(options map[string]any) (*Options, error) {
	base, err := core.NewBaseOptions(options, "js")
	if err != nil {
		return nil, err
	}
	rawBraceStyle, _ := base.RawOptions["brace_style"].(string)
	switch rawBraceStyle {
	case "expand-strict":
		base.RawOptions["brace_style"] = "expand"
	case "collapse-preserve-inline":
		base.RawOptions["brace_style"] = "collapse,preserve-inline"
	default:
		if _, ok := base.RawOptions["brace_style"]; !ok {
			if value, exists := base.RawOptions["braces_on_own_line"]; exists {
				if truthy(value) {
					base.RawOptions["brace_style"] = "expand"
				} else {
					base.RawOptions["brace_style"] = "collapse"
				}
			}
		}
	}
	braceStyleSplit, err := base.GetSelectionList("brace_style",
		[]string{"collapse", "expand", "end-expand", "none", "preserve-inline"},
		[]string{"collapse"})
	if err != nil {
		return nil, err
	}
	o := &Options{BaseOptions: base, BraceStyle: "collapse"}
	for _, item := range braceStyleSplit {
		if item == "preserve-inline" {
			o.BracePreserveInline = true
		} else {
			o.BraceStyle = item
		}
	}
	o.UnindentChainedMethods = base.GetBoolean("unindent_chained_methods", false)
	o.BreakChainedMethods = base.GetBoolean("break_chained_methods", false)
	o.SpaceInParen = base.GetBoolean("space_in_paren", false)
	o.SpaceInEmptyParen = base.GetBoolean("space_in_empty_paren", false)
	o.JSLintHappy = base.GetBoolean("jslint_happy", false)
	o.SpaceAfterAnonFunction = base.GetBoolean("space_after_anon_function", false)
	o.SpaceAfterNamedFunction = base.GetBoolean("space_after_named_function", false)
	o.KeepArrayIndentation = base.GetBoolean("keep_array_indentation", false)
	o.SpaceBeforeConditional = base.GetBoolean("space_before_conditional", true)
	o.UnescapeStrings = base.GetBoolean("unescape_strings", false)
	o.E4X = base.GetBoolean("e4x", false)
	o.CommaFirst = base.GetBoolean("comma_first", false)
	o.OperatorPosition, err = base.GetSelection("operator_position", validOperatorPositionValues, []string{"before-newline"})
	if err != nil {
		return nil, err
	}
	o.TestOutputRaw = base.GetBoolean("test_output_raw", false)
	if o.JSLintHappy {
		o.SpaceAfterAnonFunction = true
	}
	return o, nil
}

// DefaultOptions returns JavaScript formatter defaults.
func DefaultOptions() map[string]any {
	return map[string]any{
		"indent_size":                4,
		"indent_char":                " ",
		"indent_level":               0,
		"indent_with_tabs":           false,
		"preserve_newlines":          true,
		"max_preserve_newlines":      10,
		"jslint_happy":               false,
		"space_after_named_function": false,
		"space_after_anon_function":  false,
		"brace_style":                "collapse",
		"keep_array_indentation":     false,
		"keep_function_indentation":  false,
		"space_before_conditional":   true,
		"break_chained_methods":      false,
		"eval_code":                  false,
		"unescape_strings":           false,
		"wrap_line_length":           0,
		"indent_empty_lines":         false,
		"templating":                 []string{"auto"},
		"unindent_chained_methods":   false,
		"space_in_paren":             false,
		"space_in_empty_paren":       false,
		"e4x":                        false,
		"end_with_newline":           false,
		"comma_first":                false,
		"operator_position":          "before-newline",
	}
}

func truthy(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return v != ""
	case int:
		return v != 0
	case float64:
		return v != 0
	default:
		return value != nil
	}
}
