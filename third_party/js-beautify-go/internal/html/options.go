// Package html implements the HTML formatter.
package html

import (
	"strings"

	"github.com/yottonoko/js-beautify-go/internal/core"
)

// Options contains HTML-specific formatter options.
type Options struct {
	// BaseOptions stores options shared by all formatters.
	*core.BaseOptions
	// IndentInnerHTML indents content inside the html element.
	IndentInnerHTML bool
	// IndentBodyInnerHTML indents content inside the body element.
	IndentBodyInnerHTML bool
	// IndentHeadInnerHTML indents content inside the head element.
	IndentHeadInnerHTML bool
	// IndentHandlebars indents multiline Handlebars blocks.
	IndentHandlebars bool
	// WrapAttributes selects the HTML attribute wrapping mode.
	WrapAttributes string
	// WrapAttributesMinAttrs is the minimum attribute count before forced wrapping.
	WrapAttributesMinAttrs int
	// WrapAttributesIndentSize is the indentation width for wrapped attributes.
	WrapAttributesIndentSize int
	// ExtraLiners lists tags that receive an empty line before them.
	ExtraLiners []string
	// Inline lists tag names treated as inline content.
	Inline []string
	// InlineCustomElements treats custom elements as inline by default.
	InlineCustomElements bool
	// VoidElements lists tag names that do not require closing tags.
	VoidElements []string
	// Unformatted lists tags whose contents are not reformatted.
	Unformatted []string
	// ContentUnformatted lists tags whose text content is preserved.
	ContentUnformatted []string
	// UnformattedContentDelimiter separates preserved unformatted content.
	UnformattedContentDelimiter string
	// IndentScripts selects indentation behavior for script and style content.
	IndentScripts string

	extraLinerSet         map[string]struct{}
	inlineSet             map[string]struct{}
	voidElementSet        map[string]struct{}
	unformattedSet        map[string]struct{}
	contentUnformattedSet map[string]struct{}
}

// NewOptions normalizes HTML options and applies defaults.
func NewOptions(options map[string]any) (*Options, error) {
	base, err := core.NewBaseOptions(options, "html")
	if err != nil {
		return nil, err
	}
	if len(base.Templating) == 1 && base.Templating[0] == "auto" {
		base.Templating = []string{"django", "erb", "handlebars", "php"}
	}
	o := &Options{BaseOptions: base}
	o.IndentInnerHTML = base.GetBoolean("indent_inner_html", false)
	o.IndentBodyInnerHTML = base.GetBoolean("indent_body_inner_html", true)
	o.IndentHeadInnerHTML = base.GetBoolean("indent_head_inner_html", true)
	o.IndentHandlebars = base.GetBoolean("indent_handlebars", true)
	o.WrapAttributes, err = base.GetSelection("wrap_attributes",
		[]string{"auto", "force", "force-aligned", "force-expand-multiline", "aligned-multiple", "preserve", "preserve-aligned"},
		[]string{"auto"})
	if err != nil {
		return nil, err
	}
	o.WrapAttributesMinAttrs = base.GetNumber("wrap_attributes_min_attrs", 2)
	o.WrapAttributesIndentSize = base.GetNumber("wrap_attributes_indent_size", base.IndentSize)
	o.ExtraLiners = base.GetArray("extra_liners", []string{"head", "body", "/html"})
	o.extraLinerSet = lowerSet(o.ExtraLiners)
	o.Inline = base.GetArray("inline", []string{
		"a", "abbr", "area", "audio", "b", "bdi", "bdo", "br", "button", "canvas", "cite",
		"code", "data", "datalist", "del", "dfn", "em", "embed", "i", "iframe", "img",
		"input", "ins", "kbd", "keygen", "label", "map", "mark", "math", "meter", "noscript",
		"object", "output", "progress", "q", "ruby", "s", "samp", "select", "small",
		"span", "strong", "sub", "sup", "svg", "template", "textarea", "time", "u", "var",
		"video", "wbr", "text", "acronym", "big", "strike", "tt",
	})
	o.inlineSet = lowerSet(o.Inline)
	o.InlineCustomElements = base.GetBoolean("inline_custom_elements", true)
	o.VoidElements = base.GetArray("void_elements", []string{
		"area", "base", "br", "col", "embed", "hr", "img", "input", "keygen",
		"link", "menuitem", "meta", "param", "source", "track", "wbr",
		"!doctype", "?xml", "basefont", "isindex",
	})
	o.voidElementSet = lowerSet(o.VoidElements)
	o.Unformatted = base.GetArray("unformatted", []string{})
	o.unformattedSet = lowerSet(o.Unformatted)
	o.ContentUnformatted = base.GetArray("content_unformatted", []string{"pre", "textarea"})
	o.contentUnformattedSet = lowerSet(o.ContentUnformatted)
	o.UnformattedContentDelimiter = base.GetCharacters("unformatted_content_delimiter", "")
	o.IndentScripts, err = base.GetSelection("indent_scripts", []string{"normal", "keep", "separate"}, []string{"normal"})
	if err != nil {
		return nil, err
	}
	return o, nil
}

// DefaultOptions returns HTML formatter defaults.
func DefaultOptions() map[string]any {
	return map[string]any{
		"indent_size":               4,
		"indent_char":               " ",
		"indent_level":              0,
		"indent_with_tabs":          false,
		"preserve_newlines":         true,
		"max_preserve_newlines":     10,
		"brace_style":               "collapse",
		"wrap_line_length":          0,
		"indent_empty_lines":        false,
		"templating":                []string{"auto"},
		"indent_inner_html":         false,
		"indent_body_inner_html":    true,
		"indent_head_inner_html":    true,
		"indent_handlebars":         true,
		"wrap_attributes":           "auto",
		"wrap_attributes_min_attrs": 2,
		"extra_liners":              []string{"head", "body", "/html"},
		"inline_custom_elements":    true,
		"content_unformatted":       []string{"pre", "textarea"},
		"indent_scripts":            "normal",
		"end_with_newline":          false,
	}
}

func lowerSet(items []string) map[string]struct{} {
	set := make(map[string]struct{}, len(items))
	for _, item := range items {
		set[strings.ToLower(item)] = struct{}{}
	}
	return set
}
