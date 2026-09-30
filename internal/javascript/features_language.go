package javascript

import (
	"strconv"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/tsgoadapter"
)

func Hover(parsed *core.ParsedDocument, position lsp.Position) *lsp.Hover {
	name, ok := identifierAt(parsed, position)
	if !ok {
		return nil
	}
	if typeInfo := inferredTypeInfo(parsed, name); typeInfo != "" {
		return &lsp.Hover{Contents: lsp.MarkupContent{Kind: "markdown", Value: "```javascript\n" + name + ": " + typeInfo + "\n```"}}
	}
	index := BuildIndex(parsed)
	symbol, ok := index.Declarations[strings.ToLower(name)]
	if !ok {
		return nil
	}
	value := symbol.Name
	if symbol.Kind == "function" {
		value = signatureLabel(parsed, symbol.Name)
	}
	return &lsp.Hover{Contents: lsp.MarkupContent{Kind: "markdown", Value: "```javascript\n" + value + "\n```"}}
}

func inferredTypeInfo(parsed *core.ParsedDocument, name string) string {
	for _, language := range []core.EmbeddedLanguage{core.LanguageJavaScript, core.LanguageJScript} {
		virtual := core.BuildVirtualDocument(parsed, language)
		if virtual.Text == "" {
			continue
		}
		for _, match := range jsDocParamPattern.FindAllStringSubmatch(virtual.Text, -1) {
			if match[2] == name {
				return match[1]
			}
		}
		for _, match := range newExpressionPattern.FindAllStringSubmatch(virtual.Text, -1) {
			if match[1] == name {
				return match[2]
			}
		}
		for _, match := range querySelectorPattern.FindAllStringSubmatch(virtual.Text, -1) {
			if match[1] == name {
				return "Element"
			}
		}
		for _, match := range forEachParamPattern.FindAllStringSubmatch(virtual.Text, -1) {
			if match[1] == name {
				return "Element"
			}
		}
	}
	return ""
}

func Definition(parsed *core.ParsedDocument, position lsp.Position) []lsp.Location {
	name, ok := identifierAt(parsed, position)
	if !ok {
		return nil
	}
	symbol, ok := BuildIndex(parsed).Declarations[strings.ToLower(name)]
	if !ok {
		return nil
	}
	return []lsp.Location{{URI: parsed.URI, Range: symbol.Range}}
}

func References(parsed *core.ParsedDocument, position lsp.Position, includeDeclaration bool) []lsp.Location {
	name, ok := identifierAt(parsed, position)
	if !ok {
		return nil
	}
	index := BuildIndex(parsed)
	lower := strings.ToLower(name)
	var locations []lsp.Location
	for _, occurrence := range index.Occurrences[lower] {
		if !includeDeclaration {
			if declaration, ok := index.Declarations[lower]; ok && declaration.Range == occurrence.Range {
				continue
			}
		}
		locations = append(locations, lsp.Location{URI: parsed.URI, Range: occurrence.Range})
	}
	return locations
}

func RenameRange(parsed *core.ParsedDocument, position lsp.Position) *lsp.Range {
	_, occurrence, ok := occurrenceAt(parsed, position)
	if !ok {
		return nil
	}
	r := occurrence.Range
	return &r
}

func RenameEdit(parsed *core.ParsedDocument, position lsp.Position, newName string) map[string]any {
	if !isValidIdentifier(newName) {
		return map[string]any{"changes": map[string]any{}}
	}
	refs := References(parsed, position, true)
	if len(refs) == 0 {
		return map[string]any{"changes": map[string]any{}}
	}
	edits := make([]lsp.TextEdit, 0, len(refs))
	for _, ref := range refs {
		edits = append(edits, lsp.TextEdit{Range: ref.Range, NewText: newName})
	}
	return map[string]any{"changes": map[string]any{parsed.URI: edits}}
}

func Highlights(parsed *core.ParsedDocument, position lsp.Position) []lsp.DocumentHighlight {
	refs := References(parsed, position, true)
	highlights := make([]lsp.DocumentHighlight, 0, len(refs))
	for _, ref := range refs {
		highlights = append(highlights, lsp.DocumentHighlight{Range: ref.Range, Kind: 2})
	}
	return highlights
}

func FoldingRanges(parsed *core.ParsedDocument) []lsp.FoldingRange {
	source := core.SourceDocument(parsed)
	var ranges []lsp.FoldingRange
	for _, region := range parsed.Regions {
		if region.Language != core.LanguageJavaScript && region.Language != core.LanguageJScript {
			continue
		}
		stack := []int{}
		quote := byte(0)
		lineComment := false
		blockComment := false
		for i := region.ContentStart; i < region.ContentEnd; i++ {
			ch := parsed.Text[i]
			if lineComment {
				if ch == '\n' || ch == '\r' {
					lineComment = false
				}
				continue
			}
			if blockComment {
				if ch == '*' && i+1 < region.ContentEnd && parsed.Text[i+1] == '/' {
					blockComment = false
					i++
				}
				continue
			}
			if quote != 0 {
				if ch == quote && !jsIsEscaped(parsed.Text, i) {
					quote = 0
				}
				continue
			}
			if ch == '/' && i+1 < region.ContentEnd {
				switch parsed.Text[i+1] {
				case '/':
					lineComment = true
					i++
					continue
				case '*':
					blockComment = true
					i++
					continue
				}
			}
			switch ch {
			case '\'', '"', '`':
				quote = ch
			case '{', '[', '(':
				stack = append(stack, i)
			case '}', ']', ')':
				if len(stack) == 0 || !matchingJavaScriptDelimiter(parsed.Text[stack[len(stack)-1]], ch) {
					continue
				}
				open := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				start := source.PositionAt(open)
				end := source.PositionAt(i + 1)
				if end.Line > start.Line {
					ranges = append(ranges, lsp.FoldingRange{StartLine: start.Line, StartCharacter: start.Character, EndLine: end.Line, EndCharacter: end.Character})
				}
			}
		}
	}
	return ranges
}

func SelectionRange(parsed *core.ParsedDocument, position lsp.Position) *lsp.SelectionRange {
	source := core.SourceDocument(parsed)
	offset := source.OffsetAt(position)
	region := core.RegionAt(parsed, offset)
	if region == nil || (region.Language != core.LanguageJavaScript && region.Language != core.LanguageJScript) {
		return nil
	}
	regionRange := source.Range(region.ContentStart, region.ContentEnd)
	result := lsp.SelectionRange{Range: regionRange}
	if open, close, ok := enclosingJavaScriptDelimiters(parsed.Text, region.ContentStart, region.ContentEnd, offset); ok {
		parent := result
		result = lsp.SelectionRange{Range: source.Range(open, close+1), Parent: &parent}
	}
	if _, occurrence, ok := occurrenceAt(parsed, position); ok {
		parent := result
		result = lsp.SelectionRange{Range: occurrence.Range, Parent: &parent}
	}
	return &result
}

func InlineValues(parsed *core.ParsedDocument, r lsp.Range) []lsp.InlineValueVariableLookup {
	source := core.SourceDocument(parsed)
	start := source.OffsetAt(r.Start)
	end := source.OffsetAt(r.End)
	seen := map[string]struct{}{}
	var values []lsp.InlineValueVariableLookup
	for _, language := range []core.EmbeddedLanguage{core.LanguageJavaScript, core.LanguageJScript} {
		virtual := core.BuildVirtualDocument(parsed, language)
		for _, identifier := range tsgoadapter.AnalyzeJavaScriptAST(virtual.Text).Identifiers {
			sourceStart, ok := virtual.ToSourceOffset(identifier.Start)
			if !ok || sourceStart < start || sourceStart >= end {
				continue
			}
			sourceEnd, ok := virtual.ToSourceOffset(identifier.End)
			if !ok || sourceEnd > end {
				continue
			}
			key := identifier.Text + ":" + strconv.Itoa(sourceStart)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			values = append(values, lsp.InlineValueVariableLookup{Range: source.Range(sourceStart, sourceEnd), VariableName: identifier.Text, CaseSensitiveLookup: true})
		}
	}
	return values
}

func Monikers(parsed *core.ParsedDocument, position lsp.Position) []lsp.Moniker {
	_, occurrence, ok := occurrenceAt(parsed, position)
	if !ok {
		return nil
	}
	source := core.SourceDocument(parsed)
	offset := source.OffsetAt(occurrence.Range.Start)
	region := core.RegionAt(parsed, offset)
	if region == nil {
		return nil
	}
	kind := "local"
	if symbol, ok := BuildIndex(parsed).Declarations[strings.ToLower(occurrence.Name)]; ok && symbol.Kind == "function" {
		kind = "export"
	}
	return []lsp.Moniker{{
		Scheme:     "asp-lsp-js",
		Identifier: parsed.URI + "#" + string(region.Language) + "#" + occurrence.Name + "#" + strconv.Itoa(offset),
		Unique:     "project",
		Kind:       kind,
	}}
}

func OnTypeFormatting(parsed *core.ParsedDocument, position lsp.Position, character string, options core.FormattingOptions) []lsp.TextEdit {
	virtual, ok := virtualForPosition(parsed, position)
	if !ok {
		return nil
	}
	source := core.SourceDocument(parsed)
	virtualPosition, ok := virtual.ToVirtualPosition(position, source)
	if !ok {
		return nil
	}
	virtualDoc := core.NewTextDocument(virtual.URI, virtual.LanguageID, 0, virtual.Text)
	key := character
	if key == "\r" {
		key = "\n"
	}
	changes := tsgoadapter.FormatJavaScriptAfterKeystroke(virtual.Text, virtualDoc.OffsetAt(virtualPosition), key, javaScriptFormattingOptions(options, virtual.LanguageID))
	edits := make([]lsp.TextEdit, 0, len(changes))
	for _, change := range changes {
		start, ok := virtual.ToSourceOffset(change.Start)
		if !ok {
			continue
		}
		end, ok := virtual.ToSourceOffset(change.End)
		if !ok {
			continue
		}
		edits = append(edits, lsp.TextEdit{Range: source.Range(start, end), NewText: change.NewText})
	}
	return edits
}

func javaScriptFormattingOptions(options core.FormattingOptions, languageID string) tsgoadapter.FormattingOptions {
	tabSize := options.JavaScriptTabSize
	insertSpaces := options.InsertSpaces
	if options.JavaScriptInsertSpaces != nil {
		insertSpaces = *options.JavaScriptInsertSpaces
	}
	if languageID == string(core.LanguageJScript) {
		if options.JScriptTabSize > 0 {
			tabSize = options.JScriptTabSize
		}
		if options.JScriptInsertSpaces != nil {
			insertSpaces = *options.JScriptInsertSpaces
		}
	}
	if tabSize <= 0 {
		tabSize = options.TabSize
	}
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

func matchingJavaScriptDelimiter(open, close byte) bool {
	return open == '{' && close == '}' || open == '[' && close == ']' || open == '(' && close == ')'
}

func enclosingJavaScriptDelimiters(text string, start, end, offset int) (int, int, bool) {
	stack := []int{}
	bestOpen, bestClose := -1, -1
	quote := byte(0)
	for i := start; i < end; i++ {
		ch := text[i]
		if quote != 0 {
			if ch == quote && !jsIsEscaped(text, i) {
				quote = 0
			}
			continue
		}
		switch ch {
		case '\'', '"', '`':
			quote = ch
		case '{', '[', '(':
			stack = append(stack, i)
		case '}', ']', ')':
			if len(stack) == 0 || !matchingJavaScriptDelimiter(text[stack[len(stack)-1]], ch) {
				continue
			}
			open := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if open <= offset && offset <= i && (bestOpen < 0 || open > bestOpen) {
				bestOpen, bestClose = open, i
			}
		}
	}
	return bestOpen, bestClose, bestOpen >= 0
}
