package lspserver

import (
	"context"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
)

var vbscriptSQLPhrases = []string{
	"ORDER BY", "GROUP BY", "INNER JOIN", "LEFT JOIN", "RIGHT JOIN", "LEFT OUTER JOIN", "INSERT INTO", "DELETE FROM",
	"UNION ALL", "IS NULL", "IS NOT NULL", "NOT IN", "NOT EXISTS",
}

// vbscriptStringCompletionsContext handles completion inside a VBScript string
// literal. SQL strings offer SQL keywords plus the table and column names seen
// in the document and its includes; other strings offer nothing, because
// VBScript keywords and symbols are not meaningful inside a literal.
func (s *Server) vbscriptStringCompletionsContext(ctx context.Context, parsed *core.ParsedDocument, offset int) ([]lsp.CompletionItem, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if parsed == nil || ctx.Err() != nil {
		return nil, false
	}
	if _, _, inString := vbscript.StringLiteralContentAt(parsed, offset); !inString {
		return nil, false
	}
	fragment, isSQL := vbscript.SQLStringAt(parsed, offset)
	if !isSQL || vbscript.SQLInsideQuoteAt(parsed.Text, fragment, offset) {
		return []lsp.CompletionItem{}, true
	}
	text := parsed.Text
	prefixStart := offset
	for prefixStart > fragment.Start && isVBIdentifierByte(text[prefixStart-1]) {
		prefixStart--
	}
	prefix := text[prefixStart:offset]
	lowerCase := prefix != "" && prefix == strings.ToLower(prefix) && prefix != strings.ToUpper(prefix)
	keywordCase := func(word string) string {
		if lowerCase {
			return strings.ToLower(word)
		}
		return strings.ToUpper(word)
	}
	previousEnd := prefixStart
	for previousEnd > fragment.Start && (text[previousEnd-1] == ' ' || text[previousEnd-1] == '\t') {
		previousEnd--
	}
	previousStart := previousEnd
	for previousStart > fragment.Start && isVBIdentifierByte(text[previousStart-1]) {
		previousStart--
	}
	tableContext := vbscript.IsSQLTableContextWord(text[previousStart:previousEnd])
	afterDot := prefixStart > fragment.Start && text[prefixStart-1] == '.'

	documents := []*core.ParsedDocument{parsed}
	if included, complete := s.includedDocumentsForDiagnosticsContext(ctx, parsed); complete {
		documents = append(documents, included...)
	}
	if ctx.Err() != nil {
		return nil, false
	}
	items := []lsp.CompletionItem{}
	seen := map[string]struct{}{}
	add := func(label string, kind lsp.CompletionItemKind, detail string, sortGroup string) {
		key := strings.ToLower(label)
		if _, duplicate := seen[key]; duplicate {
			return
		}
		seen[key] = struct{}{}
		items = append(items, lsp.CompletionItem{Label: label, Kind: kind, Detail: detail, SortText: sortGroup + key})
	}
	tableGroup, columnGroup := "2", "1"
	if tableContext {
		tableGroup, columnGroup = "0", "5"
	}
	for _, document := range documents {
		names := vbscript.CollectSQLNames(document)
		for _, table := range names.Tables {
			if document == parsed && strings.EqualFold(table, prefix) {
				continue
			}
			add(table, lsp.CompletionItemKindClass, "SQL table", tableGroup)
		}
		if tableContext {
			continue
		}
		for _, column := range names.Columns {
			if document == parsed && strings.EqualFold(column, prefix) {
				continue
			}
			add(column, lsp.CompletionItemKindField, "SQL column", columnGroup)
		}
	}
	if afterDot {
		return items, true
	}
	for _, keyword := range vbscript.SQLKeywordNames() {
		add(keywordCase(keyword), lsp.CompletionItemKindKeyword, "SQL keyword", "3")
	}
	for _, phrase := range vbscriptSQLPhrases {
		add(keywordCase(phrase), lsp.CompletionItemKindKeyword, "SQL keyword", "3")
	}
	for _, function := range vbscript.SQLFunctionNames() {
		add(keywordCase(function), lsp.CompletionItemKindFunction, "SQL function", "4")
	}
	return items, true
}
