package vbscript

import (
	"sort"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

const returnValueProceduresAnalysisKey = "vbscript.return-value-procedures.v1"
const returnValueTextDocumentAnalysisKey = "vbscript.return-value-text-document.v1"

type returnValueProcedureSpan struct {
	Kind      string
	Name      string
	NameStart int
	NameEnd   int
	Start     int
	End       int
}

// IsReturnValueSlot reports whether position names the active Function or
// Property Get return-value slot. Calls to the procedure itself are excluded.
func IsReturnValueSlot(parsed *core.ParsedDocument, position lsp.Position) bool {
	if parsed == nil {
		return false
	}
	document := returnValueTextDocument(parsed)
	offset := document.OffsetAt(position)
	nameStart, nameEnd := identifierBoundsAt(parsed.Text, offset)
	if nameStart == nameEnd {
		return false
	}

	procedures := returnValueProcedureSpans(parsed)
	index := sort.Search(len(procedures), func(index int) bool {
		return procedures[index].End > offset
	})
	if index >= len(procedures) || offset < procedures[index].Start {
		return false
	}
	active := procedures[index]
	if !strings.EqualFold(parsed.Text[nameStart:nameEnd], active.Name) ||
		nameStart == active.NameStart && nameEnd == active.NameEnd {
		return false
	}
	if precededByCallKeyword(parsed.Text, nameStart) {
		return false
	}
	for cursor := nameEnd; cursor < len(parsed.Text); cursor++ {
		switch parsed.Text[cursor] {
		case ' ', '\t':
			continue
		case '\r', '\n', ':':
			return true
		case '&', '+', '-', '*', '/', '\\', '^', '=', '<', '>', ',', ')', '.':
			return true
		case '(':
			return false
		default:
			if isIdentifierStart(parsed.Text[cursor]) {
				end := cursor + 1
				for end < len(parsed.Text) && isIdent(parsed.Text[end]) {
					end++
				}
				switch strings.ToLower(parsed.Text[cursor:end]) {
				case "_", "and", "eqv", "imp", "is", "like", "mod", "or", "xor":
					return true
				}
			}
			return false
		}
	}
	return true
}

func returnValueTextDocument(parsed *core.ParsedDocument) *core.TextDocument {
	if value, ok := parsed.LoadRuntimeAnalysis(returnValueTextDocumentAnalysisKey); ok {
		if cached, ok := value.(*core.TextDocument); ok {
			return cached
		}
	}
	document := core.SourceDocument(parsed)
	parsed.StoreRuntimeAnalysis(returnValueTextDocumentAnalysisKey, document)
	return document
}

func returnValueProcedureSpans(parsed *core.ParsedDocument) []returnValueProcedureSpan {
	if value, ok := parsed.LoadRuntimeAnalysis(returnValueProceduresAnalysisKey); ok {
		if cached, ok := value.([]returnValueProcedureSpan); ok {
			return cached
		}
	}
	var cached []returnValueProcedureSpan
	if parsed.LoadAnalysis(returnValueProceduresAnalysisKey, &cached) {
		parsed.StoreRuntimeAnalysis(returnValueProceduresAnalysisKey, cached)
		return cached
	}
	spans := make([]returnValueProcedureSpan, 0)
	var active returnValueProcedureSpan
	closeActive := func(end int) {
		if active.Name == "" {
			return
		}
		active.End = end
		spans = append(spans, active)
		active = returnValueProcedureSpan{}
	}
	for _, statement := range vbStatements(parsed) {
		if len(statement.Tokens) == 0 {
			continue
		}
		if procedure, ok := returnValueProcedureDeclaration(statement.Tokens); ok {
			closeActive(statement.Tokens[0].Start)
			active = returnValueProcedureSpan{
				Kind:      procedure.kind,
				Name:      procedure.name,
				NameStart: procedure.nameToken.Start,
				NameEnd:   procedure.nameToken.End,
				Start:     statement.Tokens[0].Start,
			}
			continue
		}
		if returnValueProcedureEnd(statement.Tokens, active.Kind) {
			closeActive(statement.Tokens[0].Start)
		}
	}
	closeActive(len(parsed.Text) + 1)
	parsed.StoreRuntimeAnalysis(returnValueProceduresAnalysisKey, spans)
	parsed.StoreAnalysis(returnValueProceduresAnalysisKey, spans)
	return spans
}

func precededByCallKeyword(text string, nameStart int) bool {
	cursor := nameStart
	for cursor > 0 && (text[cursor-1] == ' ' || text[cursor-1] == '\t') {
		cursor--
	}
	end := cursor
	for cursor > 0 && isIdent(text[cursor-1]) {
		cursor--
	}
	if cursor == end || !strings.EqualFold(text[cursor:end], "call") {
		return false
	}
	for cursor > 0 {
		cursor--
		switch text[cursor] {
		case ' ', '\t':
			continue
		case '\r', '\n', ':':
			return true
		default:
			return false
		}
	}
	return true
}

type returnValueProcedure struct {
	kind      string
	name      string
	nameToken Token
}

func returnValueProcedureDeclaration(tokens []Token) (returnValueProcedure, bool) {
	index := 0
	for index < len(tokens) {
		switch strings.ToLower(tokens[index].Text) {
		case "public", "private", "default":
			index++
		default:
			goto declaration
		}
	}

declaration:
	if index >= len(tokens) {
		return returnValueProcedure{}, false
	}
	switch strings.ToLower(tokens[index].Text) {
	case "function":
		if index+1 < len(tokens) && tokens[index+1].Kind == "identifier" {
			return returnValueProcedure{kind: "function", name: tokens[index+1].Text, nameToken: tokens[index+1]}, true
		}
	case "property":
		if index+2 < len(tokens) && strings.EqualFold(tokens[index+1].Text, "get") && tokens[index+2].Kind == "identifier" {
			return returnValueProcedure{kind: "property", name: tokens[index+2].Text, nameToken: tokens[index+2]}, true
		}
	}
	return returnValueProcedure{}, false
}

func returnValueProcedureEnd(tokens []Token, activeKind string) bool {
	if activeKind == "" || len(tokens) < 2 || !strings.EqualFold(tokens[0].Text, "end") {
		return false
	}
	if activeKind == "function" {
		return strings.EqualFold(tokens[1].Text, "function")
	}
	return strings.EqualFold(tokens[1].Text, "property")
}

func identifierBoundsAt(text string, offset int) (int, int) {
	if offset < 0 {
		offset = 0
	}
	if offset > len(text) {
		offset = len(text)
	}
	start := offset
	for start > 0 && isIdent(text[start-1]) {
		start--
	}
	end := offset
	for end < len(text) && isIdent(text[end]) {
		end++
	}
	return start, end
}
