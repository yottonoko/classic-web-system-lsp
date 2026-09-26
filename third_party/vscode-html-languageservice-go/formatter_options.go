package htmlservice

import (
	"fmt"
	"strings"
)

// NewBeautifyHTMLOptions converts language-service format settings to js-beautify HTML options.
func NewBeautifyHTMLOptions(options HTMLFormatConfiguration, tabSize int, includesEnd bool) BeautifyHTMLOptions {
	if tabSize == 0 {
		tabSize = 4
	}
	indentChar := " "
	if !options.InsertSpaces {
		indentChar = "\t"
	}
	return BeautifyHTMLOptions{
		IndentSize:                  tabSize,
		IndentChar:                  indentChar,
		IndentEmptyLines:            boolFormatOption(options.IndentEmptyLines, false),
		WrapLineLength:              intFormatOption(options.WrapLineLength, 120),
		Unformatted:                 tagListFormatOption(options.Unformatted),
		ContentUnformatted:          tagListFormatOption(options.ContentUnformatted),
		IndentInnerHTML:             boolFormatOption(options.IndentInnerHTML, false),
		PreserveNewLines:            boolFormatOption(options.PreserveNewLines, true),
		MaxPreserveNewLines:         intFormatOption(options.MaxPreserveNewLines, 32786),
		IndentHandlebars:            boolFormatOption(options.IndentHandlebars, false),
		EndWithNewline:              includesEnd && boolFormatOption(options.EndWithNewline, false),
		ExtraLiners:                 tagListFormatOption(options.ExtraLiners),
		WrapAttributes:              stringPtrFormatOption(options.WrapAttributes, "auto"),
		WrapAttributesIndentSize:    options.WrapAttributesIndentSize,
		EOL:                         "\n",
		IndentScripts:               stringPtrFormatOption(options.IndentScripts, "normal"),
		Templating:                  templatingFormatOption(options.Templating),
		UnformattedContentDelimiter: options.UnformattedContentDelimiter,
		CSS:                         beautifyCSSOptions(options.CSS),
	}
}

func boolFormatOption(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func intFormatOption(value *int, fallback int) int {
	if value == nil {
		return fallback
	}
	return *value
}

func stringPtrFormatOption(value *string, fallback string) string {
	if value == nil {
		return fallback
	}
	return *value
}

func stringPtrValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func tagListFormatOption(value *string) []string {
	if value == nil {
		return nil
	}
	if *value == "" {
		return []string{}
	}
	parts := strings.Split(*value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		result = append(result, strings.ToLower(strings.TrimSpace(part)))
	}
	return result
}

func templatingFormatOption(value any) []string {
	switch v := value.(type) {
	case bool:
		if v {
			return []string{"auto"}
		}
		return []string{"none"}
	case []string:
		result := make([]string, len(v))
		copy(result, v)
		return result
	case []any:
		result := make([]string, len(v))
		for i, item := range v {
			if text, ok := item.(string); ok {
				result[i] = text
			} else {
				result[i] = fmt.Sprint(item)
			}
		}
		return result
	default:
		return []string{"none"}
	}
}

func beautifyCSSOptions(options *EmbeddedCSSFormatConfiguration) *BeautifyCSSOptions {
	if options == nil {
		return nil
	}
	return &BeautifyCSSOptions{
		SelectorSeparatorNewline:     options.NewlineBetweenSelectors,
		NewlineBetweenRules:          options.NewlineBetweenRules,
		SpaceAroundSelectorSeparator: options.SpaceAroundSelectorSeparator,
		BraceStyle:                   options.BraceStyle,
		PreserveNewLines:             options.PreserveNewLines,
		MaxPreserveNewLines:          options.MaxPreserveNewLines,
	}
}
