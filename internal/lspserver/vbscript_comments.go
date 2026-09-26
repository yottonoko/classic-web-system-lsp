package lspserver

import (
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func vbscriptCommentCompletions(parsed *core.ParsedDocument, text string, offset int) ([]lsp.CompletionItem, bool) {
	lineStart := currentLineStart(text, offset)
	line := text[lineStart:offset]
	comment := strings.IndexByte(line, '\'')
	if comment < 0 {
		return nil, false
	}
	if strings.HasPrefix(strings.TrimSpace(line[comment:]), "'''") {
		return vbscriptXMLDocCompletions(parsed, text, offset, line[comment+3:])
	}
	prefix := strings.TrimSpace(line[comment+1:])
	if !strings.HasPrefix(prefix, "@") {
		return nil, true
	}
	return []lsp.CompletionItem{
		vbscriptAnnotationCompletion("@type", "' @type name As Type"),
		vbscriptAnnotationCompletion("@param", "' @param Function.parameter As Type"),
		vbscriptAnnotationCompletion("@returns", "' @returns Type"),
	}, true
}

func vbscriptAnnotationCompletion(label string, insertText string) lsp.CompletionItem {
	return lsp.CompletionItem{
		Label:         label,
		Kind:          lsp.CompletionItemKindSnippet,
		Detail:        "VBScript type annotation",
		Documentation: insertText,
		InsertText:    insertText,
	}
}

func vbscriptCommentHover(text string, offset int) *lsp.Hover {
	if offset < 0 {
		offset = 0
	}
	if offset > len(text) {
		offset = len(text)
	}
	lineStart := currentLineStart(text, offset)
	lineEnd := offset + strings.IndexAny(text[offset:], "\r\n")
	if lineEnd < offset {
		lineEnd = len(text)
	}
	line := text[lineStart:lineEnd]
	relative := offset - lineStart
	for _, annotation := range []string{"@type", "@param", "@returns"} {
		start := strings.Index(line, annotation)
		if start < 0 || relative < start || relative > start+len(annotation) {
			continue
		}
		return &lsp.Hover{Contents: lsp.MarkupContent{
			Kind:  "markdown",
			Value: "```vbscript\n" + annotationSnippet(annotation) + "\n```",
		}}
	}
	return nil
}

func annotationSnippet(annotation string) string {
	switch annotation {
	case "@param":
		return "' @param Function.parameter As Type"
	case "@returns":
		return "' @returns Type"
	default:
		return "' @type name As Type"
	}
}

func currentLineStart(text string, offset int) int {
	if offset < 0 {
		offset = 0
	}
	if offset > len(text) {
		offset = len(text)
	}
	if lineStart := strings.LastIndexAny(text[:offset], "\r\n"); lineStart >= 0 {
		return lineStart + 1
	}
	return 0
}
