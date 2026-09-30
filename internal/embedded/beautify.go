package embedded

import (
	"sort"

	beautify "github.com/yottonoko/js-beautify-go"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/tsgoadapter"
)

func FormatJavaScript(source string, options core.FormattingOptions, language core.EmbeddedLanguage) (string, error) {
	options = core.SanitizeFormattingOptions(options)
	tabSize, insertSpaces := javascriptIndentOptions(options, language)
	changes := tsgoadapter.FormatJavaScript(source, typeScriptFormattingOptions(options, tabSize, insertSpaces))
	return applyTypeScriptChanges(source, changes), nil
}

func typeScriptFormattingOptions(options core.FormattingOptions, tabSize int, insertSpaces bool) tsgoadapter.FormattingOptions {
	return tsgoadapter.FormattingOptions{
		IndentSize:                               tabSize,
		TabSize:                                  tabSize,
		ConvertTabsToSpaces:                      insertSpaces,
		Semicolons:                               options.JavaScriptSemicolons,
		IndentSwitchCase:                         options.JavaScriptIndentSwitchCase,
		PlaceOpenBraceOnNewLineForFunctions:      options.JavaScriptBraceFunctionsNewLine,
		PlaceOpenBraceOnNewLineForControlBlocks:  options.JavaScriptBraceControlNewLine,
		InsertSpaceAfterCommaDelimiter:           options.JavaScriptSpaceAfterComma,
		InsertSpaceAfterSemicolonInForStatements: options.JavaScriptSpaceAfterForSemicolon,
		InsertSpaceBeforeAndAfterBinaryOperators: options.JavaScriptSpaceAroundBinaryOps,
		InsertSpaceAfterKeywordsInControlFlow:    options.JavaScriptSpaceBeforeConditional,
		InsertSpaceAfterAnonymousFunctionKeyword: options.JavaScriptSpaceAfterAnonFunction,
		InsertSpaceInsideNonemptyParentheses:     options.JavaScriptSpaceInsideParentheses,
		InsertSpaceInsideNonemptyBrackets:        options.JavaScriptSpaceInsideBrackets,
		InsertSpaceInsideNonemptyBraces:          options.JavaScriptSpaceInsideBraces,
		InsertSpaceInsideEmptyBraces:             options.JavaScriptSpaceInsideEmptyBraces,
		InsertSpaceBeforeFunctionParenthesis:     options.JavaScriptSpaceAfterNamedFunction,
	}
}

func applyTypeScriptChanges(source string, changes []tsgoadapter.TextChange) string {
	ordered := append([]tsgoadapter.TextChange(nil), changes...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Start > ordered[j].Start })
	formatted := source
	for _, change := range ordered {
		if change.Start < 0 || change.End < change.Start || change.End > len(formatted) {
			continue
		}
		formatted = formatted[:change.Start] + change.NewText + formatted[change.End:]
	}
	return formatted
}

func FormatCSS(source string, options core.FormattingOptions) (string, error) {
	options = core.SanitizeFormattingOptions(options)
	tabSize, insertSpaces := cssIndentOptions(options)
	beautifyOptions := beautify.Options{
		"indent_size": tabSize,
		"indent_char": indentChar(insertSpaces),
	}
	if wrapLineLength := cssWrapLineLength(options); wrapLineLength > 0 {
		beautifyOptions["wrap_line_length"] = wrapLineLength
	}
	if options.PreserveNewLines != nil {
		beautifyOptions["preserve_newlines"] = *options.PreserveNewLines
	}
	if options.MaxPreserveNewLines != nil {
		beautifyOptions["max_preserve_newlines"] = *options.MaxPreserveNewLines
	}
	if options.IndentEmptyLines != nil {
		beautifyOptions["indent_empty_lines"] = *options.IndentEmptyLines
	}
	if options.CSSNewlineBetweenRules != nil {
		beautifyOptions["newline_between_rules"] = *options.CSSNewlineBetweenRules
	}
	if options.CSSNewlineBetweenSelectors != nil {
		beautifyOptions["selector_separator_newline"] = *options.CSSNewlineBetweenSelectors
	}
	if options.CSSSpaceAroundSelectorSeparator != nil {
		beautifyOptions["space_around_selector_separator"] = *options.CSSSpaceAroundSelectorSeparator
	}
	if options.CSSBraceStyle != "" {
		beautifyOptions["brace_style"] = options.CSSBraceStyle
	}
	return beautify.CSS(source, beautifyOptions)
}

func indentChar(insertSpaces bool) string {
	if insertSpaces {
		return " "
	}
	return "\t"
}

func cssIndentOptions(options core.FormattingOptions) (int, bool) {
	tabSize := options.TabSize
	if options.CSSTabSize > 0 {
		tabSize = options.CSSTabSize
	}
	insertSpaces := options.InsertSpaces
	if options.CSSInsertSpaces != nil {
		insertSpaces = *options.CSSInsertSpaces
	}
	return tabSize, insertSpaces
}

func cssWrapLineLength(options core.FormattingOptions) int {
	if options.CSSWrapLineLength > 0 {
		return options.CSSWrapLineLength
	}
	return options.PrintWidth
}

func javascriptIndentOptions(options core.FormattingOptions, language core.EmbeddedLanguage) (int, bool) {
	tabSize := options.TabSize
	if options.JavaScriptTabSize > 0 {
		tabSize = options.JavaScriptTabSize
	}
	if language == core.LanguageJScript && options.JScriptTabSize > 0 {
		tabSize = options.JScriptTabSize
	}
	insertSpaces := options.InsertSpaces
	if options.JavaScriptInsertSpaces != nil {
		insertSpaces = *options.JavaScriptInsertSpaces
	}
	if language == core.LanguageJScript && options.JScriptInsertSpaces != nil {
		insertSpaces = *options.JScriptInsertSpaces
	}
	return tabSize, insertSpaces
}
