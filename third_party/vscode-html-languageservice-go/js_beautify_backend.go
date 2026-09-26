package htmlservice

import beautify "github.com/yottonoko/js-beautify-go"

var defaultHTMLBeautifier HTMLBeautifier = jsBeautifyHTMLBackend{}

type jsBeautifyHTMLBackend struct{}

func (jsBeautifyHTMLBackend) BeautifyHTML(source string, options BeautifyHTMLOptions) (string, error) {
	return beautify.HTMLWithBeautifiers(source, jsBeautifyOptions(options), jsBeautifyNoop, beautify.CSS)
}

func jsBeautifyNoop(source string, _ beautify.Options) (string, error) {
	return source, nil
}

func jsBeautifyOptions(options BeautifyHTMLOptions) beautify.Options {
	result := beautify.Options{
		"indent_size":                   options.IndentSize,
		"indent_char":                   options.IndentChar,
		"indent_empty_lines":            options.IndentEmptyLines,
		"wrap_line_length":              options.WrapLineLength,
		"indent_inner_html":             options.IndentInnerHTML,
		"preserve_newlines":             options.PreserveNewLines,
		"max_preserve_newlines":         options.MaxPreserveNewLines,
		"indent_handlebars":             options.IndentHandlebars,
		"end_with_newline":              options.EndWithNewline,
		"wrap_attributes":               options.WrapAttributes,
		"eol":                           options.EOL,
		"indent_scripts":                options.IndentScripts,
		"templating":                    append([]string(nil), options.Templating...),
		"unformatted_content_delimiter": options.UnformattedContentDelimiter,
	}
	if options.Unformatted != nil {
		result["unformatted"] = append([]string(nil), options.Unformatted...)
	}
	if options.ContentUnformatted != nil {
		result["content_unformatted"] = append([]string(nil), options.ContentUnformatted...)
	}
	if options.ExtraLiners != nil {
		result["extra_liners"] = append([]string(nil), options.ExtraLiners...)
	}
	if options.WrapAttributesIndentSize != nil {
		result["wrap_attributes_indent_size"] = *options.WrapAttributesIndentSize
	}
	if options.CSS != nil {
		result["css"] = jsBeautifyCSSOptions(options.CSS)
	}
	return result
}

func jsBeautifyCSSOptions(options *BeautifyCSSOptions) map[string]any {
	result := map[string]any{
		"selector_separator_newline":      true,
		"newline_between_rules":           true,
		"space_around_selector_separator": false,
		"brace_style":                     "collapse",
		"preserve_newlines":               true,
		"max_preserve_newlines":           32786,
	}
	if options.SelectorSeparatorNewline != nil {
		result["selector_separator_newline"] = *options.SelectorSeparatorNewline
	}
	if options.NewlineBetweenRules != nil {
		result["newline_between_rules"] = *options.NewlineBetweenRules
	}
	if options.SpaceAroundSelectorSeparator != nil {
		result["space_around_selector_separator"] = *options.SpaceAroundSelectorSeparator
	}
	if options.BraceStyle != nil {
		result["brace_style"] = *options.BraceStyle
	}
	if options.PreserveNewLines != nil {
		result["preserve_newlines"] = *options.PreserveNewLines
	}
	if options.MaxPreserveNewLines != nil {
		result["max_preserve_newlines"] = *options.MaxPreserveNewLines
	}
	return result
}
