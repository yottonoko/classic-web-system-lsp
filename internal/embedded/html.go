package embedded

import (
	"strings"

	htmlservice "github.com/yottonoko/vscode-html-languageservice-go"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

type HTML struct {
	service htmlservice.LanguageService
}

func NewHTML() HTML {
	return HTML{service: htmlservice.GetLanguageService()}
}

func (h HTML) Complete(parsed *core.ParsedDocument, position lsp.Position) (guarded lsp.CompletionList) {
	defer recoverService("html.Complete", &guarded)
	cache := h.cachedHTMLDocument(parsed)
	virtual, source := cache.virtual, cache.source
	virtualPosition, ok := virtual.ToVirtualPosition(position, source)
	if !ok {
		return lsp.CompletionList{Items: []lsp.CompletionItem{}}
	}
	result := h.service.DoComplete(cache.service.document, htmlservice.Position(virtualPosition), cache.service.htmlDocument, nil)
	items := make([]lsp.CompletionItem, 0, len(result.Items))
	for _, item := range result.Items {
		var textEdit *lsp.TextEdit
		if item.TextEdit != nil {
			if edit, ok := remapHTMLCompletionTextEdit(virtual, source, *item.TextEdit); ok {
				textEdit = &edit
			}
		}
		items = append(items, lsp.CompletionItem{
			Label:            item.Label,
			Kind:             lsp.CompletionItemKind(item.Kind),
			Detail:           item.Detail,
			Documentation:    item.Documentation,
			InsertText:       item.InsertText,
			InsertTextFormat: int(item.InsertTextFormat),
			TextEdit:         textEdit,
			FilterText:       item.FilterText,
			SortText:         item.SortText,
		})
	}
	return lsp.CompletionList{IsIncomplete: result.IsIncomplete, Items: items}
}

func remapHTMLCompletionTextEdit(virtual core.VirtualDocument, source *core.TextDocument, edit htmlservice.TextEdit) (lsp.TextEdit, bool) {
	r, ok := remapHTMLRange(virtual, source, edit.Range)
	if !ok {
		return lsp.TextEdit{}, false
	}
	return lsp.TextEdit{Range: r, NewText: edit.NewText}, true
}

func (h HTML) Hover(parsed *core.ParsedDocument, position lsp.Position) (guarded *lsp.Hover) {
	defer recoverService("html.Hover", &guarded)
	cache := h.cachedHTMLDocument(parsed)
	virtual, source := cache.virtual, cache.source
	virtualPosition, ok := virtual.ToVirtualPosition(position, source)
	if !ok {
		return nil
	}
	result := h.service.DoHover(cache.service.document, htmlservice.Position(virtualPosition), cache.service.htmlDocument, nil)
	if result == nil {
		return nil
	}
	hover := &lsp.Hover{Contents: result.Contents}
	if result.Range != nil {
		if r, ok := remapHTMLRange(virtual, source, *result.Range); ok {
			hover.Range = &r
		}
	}
	return hover
}

func (h HTML) Diagnostics(parsed *core.ParsedDocument) (guarded []lsp.Diagnostic) {
	defer recoverService("html.Diagnostics", &guarded)
	cache := h.cachedHTMLSource(parsed)
	virtual, source := cache.virtual, cache.source
	scanner := h.service.CreateScanner(virtual.Text)
	var diagnostics []lsp.Diagnostic
	for token := scanner.Scan(); token != htmlservice.TokenTypeEOS; token = scanner.Scan() {
		message := scanner.GetTokenError()
		if message == "" {
			continue
		}
		r, ok := virtual.SourceRange(source, scanner.GetTokenByteOffset(), scanner.GetTokenByteEnd())
		if !ok {
			continue
		}
		diagnostics = append(diagnostics, lsp.Diagnostic{
			Range:    r,
			Severity: lsp.DiagnosticSeverityWarning,
			Source:   "asp-lsp-html",
			Message:  message,
		})
	}
	return diagnostics
}

func (h HTML) Highlights(parsed *core.ParsedDocument, position lsp.Position) (guarded []lsp.DocumentHighlight) {
	defer recoverService("html.Highlights", &guarded)
	virtual, source, doc, htmlDoc, virtualPosition, ok := h.contextAt(parsed, position)
	if !ok {
		return nil
	}
	items := h.service.FindDocumentHighlights(doc, htmlservice.Position(virtualPosition), htmlDoc)
	result := make([]lsp.DocumentHighlight, 0, len(items))
	for _, item := range items {
		if r, ok := remapHTMLRange(virtual, source, item.Range); ok {
			result = append(result, lsp.DocumentHighlight{Range: r, Kind: int(item.Kind)})
		}
	}
	return result
}

func (h HTML) DocumentSymbols(parsed *core.ParsedDocument) (guarded []lsp.DocumentSymbol) {
	defer recoverService("html.DocumentSymbols", &guarded)
	cache := h.cachedHTMLDocument(parsed)
	virtual := cache.virtual
	if strings.TrimSpace(virtual.Text) == "" {
		return nil
	}
	source := cache.source
	items := h.service.FindDocumentSymbols2(cache.service.document, cache.service.htmlDocument)
	result := make([]lsp.DocumentSymbol, 0, len(items))
	for _, item := range items {
		if symbol, ok := remapHTMLDocumentSymbol(virtual, source, item); ok {
			result = append(result, symbol)
		}
	}
	return result
}

func (h HTML) FoldingRanges(parsed *core.ParsedDocument) (guarded []lsp.FoldingRange) {
	defer recoverService("html.FoldingRanges", &guarded)
	cache := h.cachedHTMLSource(parsed)
	virtual := cache.virtual
	if strings.TrimSpace(virtual.Text) == "" {
		return nil
	}
	items := h.service.GetFoldingRanges(cache.service.document)
	result := make([]lsp.FoldingRange, 0, len(items))
	for _, item := range items {
		result = append(result, lsp.FoldingRange{
			StartLine:      item.StartLine,
			StartCharacter: intValue(item.StartCharacter),
			EndLine:        item.EndLine,
			EndCharacter:   intValue(item.EndCharacter),
			Kind:           string(item.Kind),
		})
	}
	return result
}

func (h HTML) SelectionRange(parsed *core.ParsedDocument, position lsp.Position) (guarded *lsp.SelectionRange) {
	defer recoverService("html.SelectionRange", &guarded)
	virtual, source, doc, _, virtualPosition, ok := h.contextAt(parsed, position)
	if !ok {
		return nil
	}
	items := h.service.GetSelectionRanges(doc, []htmlservice.Position{htmlservice.Position(virtualPosition)})
	if len(items) == 0 {
		return nil
	}
	result, ok := remapHTMLSelectionRange(virtual, source, items[0])
	if !ok {
		return nil
	}
	return &result
}

func (h HTML) LinkedEditingRanges(parsed *core.ParsedDocument, position lsp.Position) (guarded []lsp.Range) {
	defer recoverService("html.LinkedEditingRanges", &guarded)
	virtual, source, doc, htmlDoc, virtualPosition, ok := h.contextAt(parsed, position)
	if !ok {
		return nil
	}
	items := h.service.FindLinkedEditingRanges(doc, htmlservice.Position(virtualPosition), htmlDoc)
	result := make([]lsp.Range, 0, len(items))
	for _, item := range items {
		if r, ok := remapHTMLRange(virtual, source, item); ok {
			result = append(result, r)
		}
	}
	return result
}

func (h HTML) Rename(parsed *core.ParsedDocument, position lsp.Position, newName string) (guarded *lsp.WorkspaceEdit) {
	defer recoverService("html.Rename", &guarded)
	virtual, source, doc, htmlDoc, virtualPosition, ok := h.contextAt(parsed, position)
	if !ok {
		return nil
	}
	edit := h.service.DoRename(doc, htmlservice.Position(virtualPosition), newName, htmlDoc)
	if edit == nil {
		return nil
	}
	changes := make(map[string][]lsp.TextEdit, len(edit.Changes))
	for _, edits := range edit.Changes {
		for _, item := range edits {
			if r, ok := remapHTMLRange(virtual, source, item.Range); ok {
				changes[parsed.URI] = append(changes[parsed.URI], lsp.TextEdit{Range: r, NewText: item.NewText})
			}
		}
	}
	if len(changes) == 0 {
		return nil
	}
	return &lsp.WorkspaceEdit{Changes: changes}
}

func (h HTML) PrepareRename(parsed *core.ParsedDocument, position lsp.Position) (guarded *lsp.Range) {
	defer recoverService("html.PrepareRename", &guarded)
	edit := h.Rename(parsed, position, "asp-lsp-rename-probe")
	if edit == nil {
		return nil
	}
	for _, edits := range edit.Changes {
		if len(edits) > 0 {
			r := edits[0].Range
			return &r
		}
	}
	return nil
}

func (h HTML) TagComplete(parsed *core.ParsedDocument, position lsp.Position) (guarded string) {
	defer recoverService("html.TagComplete", &guarded)
	_, _, doc, htmlDoc, virtualPosition, ok := h.contextAt(parsed, position)
	if !ok {
		return ""
	}
	completion := h.service.DoTagComplete(doc, htmlservice.Position(virtualPosition), htmlDoc)
	if completion == nil {
		return ""
	}
	return strings.ReplaceAll(*completion, "$0", "")
}

func (h HTML) contextAt(parsed *core.ParsedDocument, position lsp.Position) (core.VirtualDocument, *core.TextDocument, *htmlservice.TextDocument, *htmlservice.HTMLDocument, lsp.Position, bool) {
	cache := h.cachedHTMLDocument(parsed)
	virtual, source := cache.virtual, cache.source
	virtualPosition, ok := virtual.ToVirtualPosition(position, source)
	if !ok {
		return virtual, source, nil, nil, lsp.Position{}, false
	}
	return virtual, source, cache.service.document, cache.service.htmlDocument, virtualPosition, true
}

func remapHTMLRange(virtual core.VirtualDocument, source *core.TextDocument, r htmlservice.Range) (lsp.Range, bool) {
	return virtual.SourceRangeForVirtualRange(source, lsp.Range{
		Start: lsp.Position{Line: r.Start.Line, Character: r.Start.Character},
		End:   lsp.Position{Line: r.End.Line, Character: r.End.Character},
	})
}

func remapHTMLDocumentSymbol(virtual core.VirtualDocument, source *core.TextDocument, item htmlservice.DocumentSymbol) (lsp.DocumentSymbol, bool) {
	r, ok := remapHTMLRange(virtual, source, item.Range)
	if !ok {
		return lsp.DocumentSymbol{}, false
	}
	selection, ok := remapHTMLRange(virtual, source, item.SelectionRange)
	if !ok {
		return lsp.DocumentSymbol{}, false
	}
	children := make([]lsp.DocumentSymbol, 0, len(item.Children))
	for _, child := range item.Children {
		if mapped, ok := remapHTMLDocumentSymbol(virtual, source, child); ok {
			children = append(children, mapped)
		}
	}
	return lsp.DocumentSymbol{Name: item.Name, Detail: item.Detail, Kind: int(item.Kind), Range: r, SelectionRange: selection, Children: children}, true
}

func remapHTMLSelectionRange(virtual core.VirtualDocument, source *core.TextDocument, item htmlservice.SelectionRange) (lsp.SelectionRange, bool) {
	r, ok := remapHTMLRange(virtual, source, item.Range)
	if !ok {
		return lsp.SelectionRange{}, false
	}
	result := lsp.SelectionRange{Range: r}
	if item.Parent != nil {
		parent, ok := remapHTMLSelectionRange(virtual, source, *item.Parent)
		if ok {
			result.Parent = &parent
		}
	}
	return result, true
}

func intValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func (h HTML) Format(text string, options lspFormattingOptions) (guarded []lsp.TextEdit) {
	defer recoverService("html.Format", &guarded)
	doc := htmlservice.NewTextDocument("file:///format.html", "html", 0, text)
	tabSize := options.TabSize
	config := htmlservice.HTMLFormatConfiguration{TabSize: tabSize, InsertSpaces: options.InsertSpaces}
	edits := h.service.Format(doc, nil, config)
	result := make([]lsp.TextEdit, 0, len(edits))
	for _, edit := range edits {
		result = append(result, lsp.TextEdit{
			Range:   lsp.Range{Start: lsp.Position(edit.Range.Start), End: lsp.Position(edit.Range.End)},
			NewText: edit.NewText,
		})
	}
	return result
}

type lspFormattingOptions struct {
	TabSize      int
	InsertSpaces bool
}
