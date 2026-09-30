package embedded

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	htmlservice "github.com/yottonoko/vscode-html-languageservice-go"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

// FormatHTML formats a complete HTML fragment with the Go HTML language service.
func FormatHTML(source string, options core.FormattingOptions) (string, error) {
	options = core.SanitizeFormattingOptions(options)
	tabSize, insertSpaces := htmlIndentOptions(options)
	input, restore := htmlFormatterInput(source, tabSize, insertSpaces, options.FragmentMode)
	doc := htmlservice.NewTextDocument("file:///format.html", "html", 0, input)
	config := htmlservice.HTMLFormatConfiguration{
		TabSize:      tabSize,
		InsertSpaces: insertSpaces,
	}
	config.IndentEmptyLines = options.IndentEmptyLines
	config.MaxPreserveNewLines = options.MaxPreserveNewLines
	config.PreserveNewLines = options.PreserveNewLines
	config.ContentUnformatted = stringPtrOrNil(options.HTMLContentUnformatted)
	config.ExtraLiners = stringPtrOrNil(options.HTMLExtraLiners)
	config.IndentInnerHTML = options.HTMLIndentInnerHTML
	config.Unformatted = stringPtrOrNil(options.HTMLUnformatted)
	config.CSS = &htmlservice.EmbeddedCSSFormatConfiguration{
		NewlineBetweenSelectors:      options.CSSNewlineBetweenSelectors,
		NewlineBetweenRules:          options.CSSNewlineBetweenRules,
		SpaceAroundSelectorSeparator: options.CSSSpaceAroundSelectorSeparator,
		BraceStyle:                   stringPtrOrNil(options.CSSBraceStyle),
		PreserveNewLines:             options.PreserveNewLines,
		MaxPreserveNewLines:          options.MaxPreserveNewLines,
	}
	if wrapLineLength := htmlWrapLineLength(options); wrapLineLength > 0 {
		config.WrapLineLength = &wrapLineLength
	}
	if options.HTMLWrapAttributes != "" {
		config.WrapAttributes = &options.HTMLWrapAttributes
	}
	if options.HTMLWrapAttributesIndentSize > 0 {
		config.WrapAttributesIndentSize = &options.HTMLWrapAttributesIndentSize
	}
	edits := htmlservice.GetLanguageService().Format(doc, nil, config)
	formatted := applyHTMLTextEdits(doc, edits)
	if restore == nil {
		return formatted, nil
	}
	restored, ok := restore(formatted)
	if !ok {
		return source, nil
	}
	return restored, nil
}

func htmlIndentOptions(options core.FormattingOptions) (int, bool) {
	tabSize := options.TabSize
	if options.HTMLTabSize > 0 {
		tabSize = options.HTMLTabSize
	}
	insertSpaces := options.InsertSpaces
	if options.HTMLInsertSpaces != nil {
		insertSpaces = *options.HTMLInsertSpaces
	}
	return tabSize, insertSpaces
}

func htmlWrapLineLength(options core.FormattingOptions) int {
	if options.HTMLWrapLineLength > 0 {
		return options.HTMLWrapLineLength
	}
	return options.PrintWidth
}

func stringPtrOrNil(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func htmlFormatterInput(source string, tabSize int, insertSpaces bool, fragmentMode string) (string, func(string) (string, bool)) {
	if fragmentMode != "fragment" {
		return source, nil
	}
	tag := htmlFragmentWrapperTag(source)
	return "<" + tag + ">\n" + source + "\n</" + tag + ">", func(formatted string) (string, bool) {
		return unwrapHTMLFragmentText(formatted, tag, tabSize, insertSpaces)
	}
}

func htmlFragmentWrapperTag(source string) string {
	index := 0
	tag := "asp-lsp-fragment"
	for {
		pattern := regexp.MustCompile(`(?i)<\s*/?\s*` + regexp.QuoteMeta(tag) + `\b`)
		if !pattern.MatchString(source) {
			return tag
		}
		index++
		tag = "asp-lsp-fragment-" + strconv.Itoa(index)
	}
}

func unwrapHTMLFragmentText(formatted, tag string, tabSize int, insertSpaces bool) (string, bool) {
	normalized := strings.ReplaceAll(strings.ReplaceAll(formatted, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(normalized, "\n")
	escaped := regexp.QuoteMeta(tag)
	opening := regexp.MustCompile(`(?i)^\s*<` + escaped + `(?:\s[^>]*)?>\s*$`)
	closing := regexp.MustCompile(`(?i)^\s*</` + escaped + `>\s*$`)
	openingIndex := -1
	for i, line := range lines {
		if opening.MatchString(line) {
			openingIndex = i
			break
		}
	}
	closingIndex := -1
	for i := len(lines) - 1; i >= 0; i-- {
		if closing.MatchString(lines[i]) {
			closingIndex = i
			break
		}
	}
	if openingIndex < 0 || closingIndex <= openingIndex {
		return "", false
	}
	inner := append([]string(nil), lines[openingIndex+1:closingIndex]...)
	for len(inner) > 0 && strings.TrimSpace(inner[0]) == "" {
		inner = inner[1:]
	}
	for len(inner) > 0 && strings.TrimSpace(inner[len(inner)-1]) == "" {
		inner = inner[:len(inner)-1]
	}
	indent := htmlIndentUnit(tabSize, insertSpaces)
	for i, line := range inner {
		inner[i] = strings.TrimPrefix(line, indent)
	}
	return strings.Join(inner, "\n"), true
}

func htmlIndentUnit(tabSize int, insertSpaces bool) string {
	if !insertSpaces {
		return "\t"
	}
	if tabSize <= 0 {
		tabSize = 4
	}
	return strings.Repeat(" ", tabSize)
}

func applyHTMLTextEdits(doc *htmlservice.TextDocument, edits []htmlservice.TextEdit) string {
	text := doc.GetText()
	if len(edits) == 0 {
		return text
	}
	sort.SliceStable(edits, func(i, j int) bool {
		return doc.OffsetAt(edits[i].Range.Start) > doc.OffsetAt(edits[j].Range.Start)
	})
	for _, edit := range edits {
		start := doc.OffsetAt(edit.Range.Start)
		end := doc.OffsetAt(edit.Range.End)
		if start < 0 || end < start || end > len(text) {
			continue
		}
		text = text[:start] + edit.NewText + text[end:]
	}
	return text
}
