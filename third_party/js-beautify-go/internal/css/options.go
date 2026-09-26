// Package css implements the CSS formatter.
package css

import "github.com/yottonoko/js-beautify-go/internal/core"

// Options contains CSS-specific formatter options.
type Options struct {
	// BaseOptions stores options shared by all formatters.
	*core.BaseOptions
	// SelectorSeparatorNewline prints each selector on its own line.
	SelectorSeparatorNewline bool
	// NewlineBetweenRules inserts a blank line between CSS rules.
	NewlineBetweenRules bool
	// SpaceAroundCombinator inserts spaces around selector combinators.
	SpaceAroundCombinator bool
	// BraceStyle selects collapse or expand brace layout.
	BraceStyle string
}

// NewOptions normalizes CSS options and applies defaults.
func NewOptions(options map[string]any) (*Options, error) {
	base, err := core.NewBaseOptions(options, "css")
	if err != nil {
		return nil, err
	}
	o := &Options{BaseOptions: base}
	o.SelectorSeparatorNewline = base.GetBoolean("selector_separator_newline", true)
	o.NewlineBetweenRules = base.GetBoolean("newline_between_rules", true)
	legacySeparator := base.GetBoolean("space_around_selector_separator", false)
	o.SpaceAroundCombinator = base.GetBoolean("space_around_combinator", false) || legacySeparator
	braceStyleSplit, err := base.GetSelectionList("brace_style",
		[]string{"collapse", "expand", "end-expand", "none", "preserve-inline"},
		[]string{"collapse"})
	if err != nil {
		return nil, err
	}
	o.BraceStyle = "collapse"
	for _, item := range braceStyleSplit {
		if item == "expand" {
			o.BraceStyle = "expand"
		} else if item != "preserve-inline" {
			o.BraceStyle = "collapse"
		}
	}
	return o, nil
}

// DefaultOptions returns CSS formatter defaults.
func DefaultOptions() map[string]any {
	return map[string]any{
		"indent_size":                     4,
		"indent_char":                     " ",
		"indent_level":                    0,
		"indent_with_tabs":                false,
		"preserve_newlines":               true,
		"max_preserve_newlines":           10,
		"brace_style":                     "collapse",
		"selector_separator_newline":      true,
		"newline_between_rules":           true,
		"space_around_combinator":         false,
		"space_around_selector_separator": false,
		"wrap_line_length":                0,
		"indent_empty_lines":              false,
		"templating":                      []string{"auto"},
		"end_with_newline":                false,
	}
}
