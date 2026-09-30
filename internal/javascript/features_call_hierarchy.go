package javascript

import (
	"regexp"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func SignatureHelp(parsed *core.ParsedDocument, position lsp.Position) *lsp.SignatureHelp {
	virtual, ok := virtualForPosition(parsed, position)
	if !ok {
		return nil
	}
	sourceDoc := core.SourceDocument(parsed)
	virtualPosition, ok := virtual.ToVirtualPosition(position, sourceDoc)
	if !ok {
		return nil
	}
	virtualDoc := core.NewTextDocument(virtual.URI, virtual.LanguageID, 0, virtual.Text)
	open := callOpenParenBefore(virtual.Text, virtualDoc.OffsetAt(virtualPosition))
	if open < 0 {
		return nil
	}
	name := identifierBefore(virtual.Text, open)
	if name == "" {
		return nil
	}
	label := signatureLabel(parsed, name)
	if label == name {
		return nil
	}
	params := parameterInformation(label)
	return &lsp.SignatureHelp{Signatures: []lsp.SignatureInformation{{Label: label, Parameters: params}}}
}

type functionDeclaration struct {
	Name           string
	Range          lsp.Range
	SelectionRange lsp.Range
	BodyStart      int
	BodyEnd        int
	Parameters     []string
}

type callOccurrence struct {
	Range lsp.Range
	Start int
	Open  int
}

func PrepareCallHierarchy(parsed *core.ParsedDocument, position lsp.Position) []lsp.CallHierarchyItem {
	name, ok := identifierAt(parsed, position)
	if !ok {
		return nil
	}
	for _, declaration := range functionDeclarations(parsed) {
		if declaration.Name != name {
			continue
		}
		return []lsp.CallHierarchyItem{javascriptCallHierarchyItem(parsed.URI, declaration)}
	}
	return nil
}

func IncomingCalls(parsed *core.ParsedDocument, name string) []lsp.CallHierarchyIncomingCall {
	declarations := functionDeclarations(parsed)
	var calls []lsp.CallHierarchyIncomingCall
	for _, call := range callOccurrences(parsed, name) {
		caller, ok := enclosingFunction(declarations, call.Start)
		if !ok || caller.Name == name {
			continue
		}
		calls = append(calls, lsp.CallHierarchyIncomingCall{
			From:       javascriptCallHierarchyItem(parsed.URI, caller),
			FromRanges: []lsp.Range{call.Range},
		})
	}
	return calls
}

func OutgoingCalls(parsed *core.ParsedDocument, name string) []lsp.CallHierarchyOutgoingCall {
	declarations := functionDeclarations(parsed)
	var source functionDeclaration
	var found bool
	for _, declaration := range declarations {
		if declaration.Name == name {
			source = declaration
			found = true
			break
		}
	}
	if !found {
		return nil
	}
	var calls []lsp.CallHierarchyOutgoingCall
	for _, target := range declarations {
		if target.Name == name {
			continue
		}
		var ranges []lsp.Range
		for _, call := range callOccurrences(parsed, target.Name) {
			if call.Start > source.BodyStart && call.Start < source.BodyEnd {
				ranges = append(ranges, call.Range)
			}
		}
		if len(ranges) == 0 {
			continue
		}
		calls = append(calls, lsp.CallHierarchyOutgoingCall{
			To:         javascriptCallHierarchyItem(parsed.URI, target),
			FromRanges: ranges,
		})
	}
	return calls
}

func InlayHints(parsed *core.ParsedDocument, r lsp.Range) []lsp.InlayHint {
	source := core.SourceDocument(parsed)
	startOffset := source.OffsetAt(r.Start)
	endOffset := source.OffsetAt(r.End)
	var hints []lsp.InlayHint
	for _, declaration := range functionDeclarations(parsed) {
		if len(declaration.Parameters) == 0 {
			continue
		}
		for _, call := range callOccurrences(parsed, declaration.Name) {
			if call.Open < 0 {
				continue
			}
			for i, argStart := range javascriptArgumentStarts(parsed.Text, call.Open+1) {
				if i >= len(declaration.Parameters) || argStart < startOffset || argStart > endOffset {
					continue
				}
				hints = append(hints, lsp.InlayHint{
					Position:     source.PositionAt(argStart),
					Label:        declaration.Parameters[i] + ":",
					Kind:         2,
					PaddingRight: lsp.BoolPtr(true),
				})
			}
		}
	}
	return hints
}

func javascriptCallHierarchyItem(uri string, declaration functionDeclaration) lsp.CallHierarchyItem {
	return lsp.CallHierarchyItem{
		Name:           declaration.Name,
		Kind:           12,
		URI:            uri,
		Range:          declaration.Range,
		SelectionRange: declaration.SelectionRange,
		Data:           map[string]any{"uri": uri, "name": declaration.Name, "language": "javascript"},
	}
}

func functionDeclarations(parsed *core.ParsedDocument) []functionDeclaration {
	source := core.SourceDocument(parsed)
	var declarations []functionDeclaration
	for _, region := range parsed.Regions {
		if region.Language != core.LanguageJavaScript && region.Language != core.LanguageJScript {
			continue
		}
		content := parsed.Text[region.ContentStart:region.ContentEnd]
		for _, match := range functionPattern.FindAllStringSubmatchIndex(content, -1) {
			if len(match) < 4 || match[2] < 0 || match[3] < 0 {
				continue
			}
			declStart := region.ContentStart + match[0]
			declEnd := region.ContentStart + match[1]
			nameStart := region.ContentStart + match[2]
			nameEnd := region.ContentStart + match[3]
			paramsStart := region.ContentStart + match[4]
			paramsEnd := region.ContentStart + match[5]
			bodyStart := firstJavaScriptBrace(parsed.Text, declEnd, region.ContentEnd)
			bodyEnd := region.ContentEnd
			if bodyStart >= 0 {
				bodyEnd = matchingJavaScriptBraceEnd(parsed.Text, bodyStart, region.ContentEnd)
			}
			declarations = append(declarations, functionDeclaration{
				Name:           parsed.Text[nameStart:nameEnd],
				Range:          source.Range(declStart, declEnd),
				SelectionRange: source.Range(nameStart, nameEnd),
				BodyStart:      bodyStart,
				BodyEnd:        bodyEnd,
				Parameters:     parameterNames(parsed.Text[paramsStart:paramsEnd]),
			})
		}
	}
	return declarations
}

func callOccurrences(parsed *core.ParsedDocument, name string) []callOccurrence {
	source := core.SourceDocument(parsed)
	pattern := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\s*\(`)
	var calls []callOccurrence
	for _, region := range parsed.Regions {
		if region.Language != core.LanguageJavaScript && region.Language != core.LanguageJScript {
			continue
		}
		content := parsed.Text[region.ContentStart:region.ContentEnd]
		for _, match := range pattern.FindAllStringIndex(content, -1) {
			start := region.ContentStart + match[0]
			if strings.EqualFold(previousWord(parsed.Text, start), "function") {
				continue
			}
			calls = append(calls, callOccurrence{
				Range: source.Range(start, start+len(name)),
				Start: start,
				Open:  region.ContentStart + match[1] - 1,
			})
		}
	}
	return calls
}

func javascriptArgumentStarts(text string, start int) []int {
	var starts []int
	cursor := nextJavaScriptArgumentStart(text, start)
	if cursor < 0 || cursor >= len(text) || text[cursor] == ')' {
		return starts
	}
	starts = append(starts, cursor)
	depth := 0
	quote := byte(0)
	for i := cursor; i < len(text); i++ {
		ch := text[i]
		if quote != 0 {
			if ch == quote && !jsIsEscaped(text, i) {
				quote = 0
			}
			continue
		}
		switch ch {
		case '"', '\'', '`':
			quote = ch
		case '(', '[', '{':
			depth++
		case ')':
			if depth == 0 {
				return starts
			}
			depth--
		case ']', '}':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				next := nextJavaScriptArgumentStart(text, i+1)
				if next >= 0 && next < len(text) && text[next] != ')' {
					starts = append(starts, next)
				}
			}
		}
	}
	return starts
}

func nextJavaScriptArgumentStart(text string, offset int) int {
	for offset < len(text) {
		if !isSpace(text[offset]) {
			return offset
		}
		offset++
	}
	return -1
}

func enclosingFunction(declarations []functionDeclaration, offset int) (functionDeclaration, bool) {
	var selected functionDeclaration
	found := false
	for _, declaration := range declarations {
		if declaration.BodyStart < 0 || offset <= declaration.BodyStart || offset >= declaration.BodyEnd {
			continue
		}
		if !found || declaration.BodyStart > selected.BodyStart {
			selected = declaration
			found = true
		}
	}
	return selected, found
}

func firstJavaScriptBrace(text string, start int, end int) int {
	for i := start; i < end && i < len(text); i++ {
		if text[i] == '{' {
			return i
		}
	}
	return -1
}

func matchingJavaScriptBraceEnd(text string, open int, limit int) int {
	depth := 0
	quote := byte(0)
	for i := open; i < limit && i < len(text); i++ {
		ch := text[i]
		if quote != 0 {
			if ch == quote && !jsIsEscaped(text, i) {
				quote = 0
			}
			continue
		}
		switch ch {
		case '"', '\'', '`':
			quote = ch
		case '{':
			depth++
		case '}':
			if depth > 0 {
				depth--
				if depth == 0 {
					return i + 1
				}
			}
		}
	}
	return limit
}
