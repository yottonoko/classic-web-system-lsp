package embedded

import (
	"context"
	"sort"
	"strings"

	cssls "github.com/yottonoko/vscode-css-languageservice-go"
	csslsp "github.com/yottonoko/vscode-css-languageservice-go/lsp"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

type CSS struct {
	service cssls.LanguageService
}

func NewCSS() CSS {
	return CSS{service: cssls.GetCSSLanguageService()}
}

func (c CSS) Hover(parsed *core.ParsedDocument, position lsp.Position) (guarded *lsp.Hover) {
	defer recoverService("css.Hover", &guarded)
	cache := c.cachedCSSDocument(parsed)
	virtual, source := cache.virtual, cache.source
	virtualPosition, ok := virtual.ToVirtualPosition(position, source)
	if !ok {
		return nil
	}
	result := c.service.DoHover(cache.service.document, csslsp.Position(virtualPosition), cache.service.stylesheet, nil)
	if result == nil {
		return nil
	}
	hover := &lsp.Hover{Contents: result.Contents}
	if result.Range != nil {
		if r, ok := remapCSSRange(virtual, source, *result.Range); ok {
			hover.Range = &r
		}
	} else if r, ok := cssIdentifierRangeAt(source, position); ok {
		hover.Range = &r
	}
	return hover
}

// Definition returns the CSS language service definition mapped to the ASP source document.
func (c CSS) Definition(parsed *core.ParsedDocument, position lsp.Position) (guarded *lsp.Location) {
	defer recoverService("css.Definition", &guarded)
	virtual, source, doc, stylesheet, virtualPosition, ok := c.contextAt(parsed, position)
	if !ok {
		return nil
	}
	location := c.service.FindDefinition(doc, csslsp.Position(virtualPosition), stylesheet)
	if location == nil {
		return nil
	}
	r, ok := remapCSSRange(virtual, source, location.Range)
	if !ok {
		return nil
	}
	return &lsp.Location{URI: parsed.URI, Range: r}
}

func (c CSS) Complete(ctx context.Context, parsed *core.ParsedDocument, position lsp.Position) (guarded lsp.CompletionList) {
	defer recoverService("css.Complete", &guarded)
	cache := c.cachedCSSDocument(parsed)
	virtual, source := cache.virtual, cache.source
	virtualPosition, ok := virtual.ToVirtualPosition(position, source)
	if !ok {
		return lsp.CompletionList{Items: []lsp.CompletionItem{}}
	}
	result, err := c.service.DoComplete(ctx, cache.service.document, csslsp.Position(virtualPosition), cache.service.stylesheet, nil)
	if err != nil {
		return lsp.CompletionList{Items: fallbackCSSCompletions(source, position)}
	}
	items := make([]lsp.CompletionItem, 0, len(result.Items))
	for _, item := range result.Items {
		var textEdit *lsp.TextEdit
		if item.TextEdit != nil {
			if edit, ok := remapCSSCompletionTextEdit(virtual, source, *item.TextEdit); ok {
				textEdit = &edit
			}
		}
		if textEdit == nil {
			if edit, ok := fallbackCSSCompletionTextEdit(source, position, item); ok {
				textEdit = &edit
			}
		}
		insertTextFormat := int(item.InsertTextFormat)
		if textEdit != nil && strings.Contains(textEdit.NewText, "$0") && insertTextFormat == 0 {
			insertTextFormat = 2
		}
		items = append(items, lsp.CompletionItem{
			Label:            item.Label,
			Kind:             lsp.CompletionItemKind(item.Kind),
			Detail:           item.Detail,
			Documentation:    item.Documentation,
			InsertText:       item.InsertText,
			InsertTextFormat: insertTextFormat,
			TextEdit:         textEdit,
			SortText:         item.SortText,
		})
	}
	if len(items) == 0 {
		virtualSource := core.NewTextDocument(virtual.URI, "css", source.Version, virtual.Text)
		fallback := fallbackCSSCompletions(virtualSource, virtualPosition)
		items = make([]lsp.CompletionItem, 0, len(fallback))
		for _, item := range fallback {
			if item.TextEdit == nil {
				items = append(items, item)
				continue
			}
			r, ok := virtual.SourceRangeForVirtualRange(source, item.TextEdit.Range)
			if !ok {
				continue
			}
			item.TextEdit = &lsp.TextEdit{Range: r, NewText: item.TextEdit.NewText}
			items = append(items, item)
		}
	}
	items = appendCSSCrossRegionCompletions(items, parsed, source, position)
	return lsp.CompletionList{IsIncomplete: result.IsIncomplete, Items: items}
}

const cssCrossRegionCompletionIndexKey = "embedded.css-cross-region-completion-index.v1"

type cssCrossRegionCompletionIndex struct {
	customProperties []string
	keyframes        []string
}

func appendCSSCrossRegionCompletions(items []lsp.CompletionItem, parsed *core.ParsedDocument, source *core.TextDocument, position lsp.Position) []lsp.CompletionItem {
	offset := source.OffsetAt(position)
	region := core.RegionAt(parsed, offset)
	if region == nil || region.Language != core.LanguageCSS || region.ContentStart < 0 || region.ContentEnd < region.ContentStart || region.ContentEnd > len(source.Text) {
		return items
	}
	regionText := source.Text[region.ContentStart:region.ContentEnd]
	regionOffset := offset - region.ContentStart
	if !cssPositionIsCode(regionText, regionOffset) {
		return items
	}
	start := offset
	for start > 0 && isCSSCompletionIdentifier(source.Text[start-1]) {
		start--
	}
	prefix := source.Text[start:offset]
	regionStart := start - region.ContentStart
	maskedRegionText := maskCSSNonCode(regionText)
	customPropertyContext := strings.HasPrefix(prefix, "--") && strings.Contains(maskedRegionText[max(0, regionStart-16):regionStart], "var(")
	keyframeContext := cssKeyframeValueCompletionContext(maskedRegionText, regionOffset)
	if !customPropertyContext && !keyframeContext {
		return items
	}
	index := cssCrossRegionCompletions(parsed)
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		seen[item.Label] = struct{}{}
	}
	add := func(label string, kind lsp.CompletionItemKind, detail string) {
		if _, exists := seen[label]; exists || prefix != "" && !strings.HasPrefix(strings.ToLower(label), strings.ToLower(prefix)) {
			return
		}
		seen[label] = struct{}{}
		edit := lsp.TextEdit{Range: source.Range(start, offset), NewText: label}
		items = append(items, lsp.CompletionItem{Label: label, Kind: kind, Detail: detail, TextEdit: &edit})
	}
	if customPropertyContext {
		for _, name := range index.customProperties {
			add(name, lsp.CompletionItemKindVariable, "CSS custom property")
		}
	}
	if keyframeContext {
		for _, name := range index.keyframes {
			add(name, lsp.CompletionItemKindValue, "CSS keyframes")
		}
	}
	return items
}

func cssCrossRegionCompletions(parsed *core.ParsedDocument) cssCrossRegionCompletionIndex {
	if value, ok := parsed.LoadRuntimeAnalysis(cssCrossRegionCompletionIndexKey); ok {
		return value.(cssCrossRegionCompletionIndex)
	}
	customProperties := map[string]struct{}{}
	keyframes := map[string]struct{}{}
	for _, region := range parsed.Regions {
		if region.Language != core.LanguageCSS || region.ContentStart < 0 || region.ContentEnd > len(parsed.Text) {
			continue
		}
		virtual := core.BuildEmbeddedRegionVirtualDocument(parsed, region)
		text := maskCSSNonCode(virtual.Text)
		for offset := 0; offset+2 < len(text); offset++ {
			if text[offset] != '-' || text[offset+1] != '-' {
				continue
			}
			end := offset + 2
			for end < len(text) && isCSSCompletionIdentifier(text[end]) {
				end++
			}
			cursor := end
			for cursor < len(text) && (text[cursor] == ' ' || text[cursor] == '\t' || text[cursor] == '\r' || text[cursor] == '\n') {
				cursor++
			}
			if end > offset+2 && cursor < len(text) && text[cursor] == ':' {
				customProperties[text[offset:end]] = struct{}{}
			}
			offset = max(offset, end-1)
		}
		lower := strings.ToLower(text)
		for offset := 0; ; {
			found := strings.Index(lower[offset:], "@keyframes")
			if found < 0 {
				break
			}
			cursor := offset + found + len("@keyframes")
			for cursor < len(text) && (text[cursor] == ' ' || text[cursor] == '\t' || text[cursor] == '\r' || text[cursor] == '\n') {
				cursor++
			}
			end := cursor
			for end < len(text) && isCSSCompletionIdentifier(text[end]) {
				end++
			}
			if end > cursor {
				keyframes[text[cursor:end]] = struct{}{}
			}
			offset = max(end, cursor+1)
		}
	}
	index := cssCrossRegionCompletionIndex{
		customProperties: sortedCSSCompletionNames(customProperties),
		keyframes:        sortedCSSCompletionNames(keyframes),
	}
	parsed.StoreRuntimeAnalysis(cssCrossRegionCompletionIndexKey, index)
	return index
}

func sortedCSSCompletionNames(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func cssKeyframeValueCompletionContext(text string, offset int) bool {
	text = maskCSSNonCode(text)
	if offset < 0 {
		return false
	}
	if offset > len(text) {
		offset = len(text)
	}
	start := strings.LastIndexAny(text[:max(0, min(offset, len(text)))], "{;") + 1
	declaration := strings.ToLower(text[start:offset])
	colon := strings.IndexByte(declaration, ':')
	if colon < 0 {
		return false
	}
	property := strings.TrimSpace(declaration[:colon])
	return property == "animation" || property == "animation-name"
}

// CompleteDocument answers an interactive completion from only the containing
// CSS region. Whole-document CSS analysis remains available to diagnostics and
// cross-region queries, while typing in one style block does not reparse every
// unrelated style block in the ASP file.
func (c CSS) CompleteDocument(ctx context.Context, parsed *core.ParsedDocument, source *core.TextDocument, position lsp.Position) (guarded lsp.CompletionList) {
	defer recoverService("css.CompleteDocument", &guarded)
	if parsed == nil || source == nil || source.Text != parsed.Text {
		return c.Complete(ctx, parsed, position)
	}
	sourceOffset := source.OffsetAt(position)
	region := core.RegionAt(parsed, sourceOffset)
	if region == nil || region.Language != core.LanguageCSS || region.ContentStart < 0 || region.ContentEnd < region.ContentStart || region.ContentEnd > len(source.Text) {
		return lsp.CompletionList{Items: []lsp.CompletionItem{}}
	}
	virtual := core.BuildEmbeddedRegionVirtualDocument(parsed, *region)
	virtualPosition, ok := virtual.ToVirtualPosition(position, source)
	if !ok {
		return lsp.CompletionList{Items: []lsp.CompletionItem{}}
	}
	serviceDocument := csslsp.NewTextDocument(csslsp.DocumentURI(virtual.URI), "css", source.Version, virtual.Text)
	stylesheet := c.service.ParseStylesheet(serviceDocument)
	result, err := c.service.DoComplete(ctx, serviceDocument, csslsp.Position(virtualPosition), stylesheet, nil)
	if err != nil {
		return lsp.CompletionList{Items: fallbackCSSCompletions(source, position)}
	}
	items := make([]lsp.CompletionItem, 0, len(result.Items))
	for _, item := range result.Items {
		var textEdit *lsp.TextEdit
		if item.TextEdit != nil {
			if edit, ok := remapCSSCompletionTextEdit(virtual, source, *item.TextEdit); ok {
				textEdit = &edit
			}
		}
		if textEdit == nil {
			if edit, ok := fallbackCSSCompletionTextEdit(source, position, item); ok {
				textEdit = &edit
			}
		}
		insertTextFormat := int(item.InsertTextFormat)
		if textEdit != nil && strings.Contains(textEdit.NewText, "$0") && insertTextFormat == 0 {
			insertTextFormat = 2
		}
		items = append(items, lsp.CompletionItem{
			Label:            item.Label,
			Kind:             lsp.CompletionItemKind(item.Kind),
			Detail:           item.Detail,
			Documentation:    item.Documentation,
			InsertText:       item.InsertText,
			InsertTextFormat: insertTextFormat,
			TextEdit:         textEdit,
			SortText:         item.SortText,
		})
	}
	if len(items) == 0 {
		virtualSource := core.NewTextDocument(virtual.URI, "css", source.Version, virtual.Text)
		fallback := fallbackCSSCompletions(virtualSource, virtualPosition)
		items = make([]lsp.CompletionItem, 0, len(fallback))
		for _, item := range fallback {
			if item.TextEdit == nil {
				items = append(items, item)
				continue
			}
			r, ok := virtual.SourceRangeForVirtualRange(source, item.TextEdit.Range)
			if !ok {
				continue
			}
			item.TextEdit = &lsp.TextEdit{Range: r, NewText: item.TextEdit.NewText}
			items = append(items, item)
		}
	}
	items = appendCSSCrossRegionCompletions(items, parsed, source, position)
	return lsp.CompletionList{IsIncomplete: result.IsIncomplete, Items: items}
}

func fallbackCSSCompletions(source *core.TextDocument, position lsp.Position) []lsp.CompletionItem {
	if items := fallbackCSSValueCompletions(source, position); len(items) > 0 {
		return items
	}
	return fallbackCSSPropertyCompletions(source, position)
}

func remapCSSRange(virtual core.VirtualDocument, source *core.TextDocument, r csslsp.Range) (lsp.Range, bool) {
	return virtual.SourceRangeForVirtualRange(source, lsp.Range{
		Start: lsp.Position{Line: r.Start.Line, Character: r.Start.Character},
		End:   lsp.Position{Line: r.End.Line, Character: r.End.Character},
	})
}

func remapCSSCompletionTextEdit(virtual core.VirtualDocument, source *core.TextDocument, edit csslsp.TextEdit) (lsp.TextEdit, bool) {
	r, ok := remapCSSRange(virtual, source, edit.Range)
	if !ok {
		return lsp.TextEdit{}, false
	}
	return lsp.TextEdit{Range: r, NewText: normalizeCSSCompletionNewText(edit.NewText)}, true
}

func fallbackCSSCompletionTextEdit(source *core.TextDocument, position lsp.Position, item csslsp.CompletionItem) (lsp.TextEdit, bool) {
	newText := item.InsertText
	if newText == "" {
		newText = item.Label
	}
	if newText == "" {
		return lsp.TextEdit{}, false
	}
	return cssCompletionTextEdit(source, position, newText), true
}

func fallbackCSSPropertyCompletions(source *core.TextDocument, position lsp.Position) []lsp.CompletionItem {
	if !isCSSPropertyCompletionContext(source.Text, source.OffsetAt(position)) {
		return nil
	}
	properties := []string{"display", "color", "background", "background-color", "font-size", "margin", "padding"}
	items := make([]lsp.CompletionItem, 0, len(properties))
	for _, property := range properties {
		newText := property + ": "
		edit := cssCompletionTextEdit(source, position, newText)
		items = append(items, lsp.CompletionItem{
			Label:            property,
			Kind:             lsp.CompletionItemKindProperty,
			Detail:           "CSS property",
			InsertText:       edit.NewText,
			InsertTextFormat: 2,
			TextEdit:         &edit,
			SortText:         property,
		})
	}
	return items
}

func fallbackCSSValueCompletions(source *core.TextDocument, position lsp.Position) []lsp.CompletionItem {
	if !isCSSValueCompletionContext(source.Text, source.OffsetAt(position)) {
		return nil
	}
	values := []string{"red", "green", "blue", "black", "white", "transparent", "block", "inline", "none"}
	items := make([]lsp.CompletionItem, 0, len(values))
	for _, value := range values {
		edit := cssCompletionTextEdit(source, position, value)
		items = append(items, lsp.CompletionItem{
			Label:      value,
			Kind:       lsp.CompletionItemKindValue,
			Detail:     "CSS value",
			InsertText: value,
			TextEdit:   &edit,
			SortText:   value,
		})
	}
	return items
}

func cssCompletionTextEdit(source *core.TextDocument, position lsp.Position, newText string) lsp.TextEdit {
	offset := source.OffsetAt(position)
	start := offset
	for start > 0 && isCSSCompletionIdentifier(source.Text[start-1]) {
		start--
	}
	if offset > 0 && source.Text[offset-1] == ';' && !strings.HasPrefix(newText, " ") {
		newText = " " + newText
	}
	return lsp.TextEdit{Range: source.Range(start, offset), NewText: normalizeCSSCompletionNewText(newText)}
}

func normalizeCSSCompletionNewText(text string) string {
	if strings.HasSuffix(text, ": ") {
		return text + "$0;"
	}
	return text
}

func isCSSCompletionIdentifier(b byte) bool {
	return b == '-' || b == '_' || b >= '0' && b <= '9' || b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z'
}

func cssIdentifierRangeAt(source *core.TextDocument, position lsp.Position) (lsp.Range, bool) {
	offset := source.OffsetAt(position)
	if offset < 0 {
		offset = 0
	}
	if offset > len(source.Text) {
		offset = len(source.Text)
	}
	start := offset
	for start > 0 && isCSSCompletionIdentifier(source.Text[start-1]) {
		start--
	}
	end := offset
	for end < len(source.Text) && isCSSCompletionIdentifier(source.Text[end]) {
		end++
	}
	if start == end {
		return lsp.Range{}, false
	}
	return source.Range(start, end), true
}

func isCSSPropertyCompletionContext(text string, offset int) bool {
	lastBoundary, lastColon := cssCompletionBoundary(text, offset)
	return lastColon < 0 || lastBoundary > lastColon
}

func isCSSValueCompletionContext(text string, offset int) bool {
	lastBoundary, lastColon := cssCompletionBoundary(text, offset)
	return lastColon >= 0 && lastColon > lastBoundary
}

func cssCompletionBoundary(text string, offset int) (int, int) {
	text = maskCSSNonCode(text)
	if offset < 0 {
		offset = 0
	}
	if offset > len(text) {
		offset = len(text)
	}
	lastBoundary := -1
	lastColon := -1
	for i := offset - 1; i >= 0; i-- {
		switch text[i] {
		case ';', '"', '\'', '{', '\n', '\r':
			lastBoundary = i
			i = -1
		case ':':
			if lastColon < 0 {
				lastColon = i
			}
		}
	}
	return lastBoundary, lastColon
}

func (c CSS) Diagnostics(parsed *core.ParsedDocument) (guarded []lsp.Diagnostic) {
	defer recoverService("css.Diagnostics", &guarded)
	cache := c.cachedCSSDocument(parsed)
	virtual := cache.virtual
	if virtual.Text == "" {
		return nil
	}
	result := c.service.DoValidation(cache.service.document, cache.service.stylesheet, nil)
	extra := cssEmptyValueDiagnostics(virtual.Text, result)
	allDiagnostics := make([]csslsp.Diagnostic, 0, len(result)+len(extra))
	allDiagnostics = append(allDiagnostics, result...)
	allDiagnostics = append(allDiagnostics, extra...)
	diagnostics := make([]lsp.Diagnostic, 0, len(allDiagnostics))
	source := cache.source
	for _, diagnostic := range allDiagnostics {
		sourceRange, ok := remapCSSRange(virtual, source, diagnostic.Range)
		if !ok {
			continue
		}
		diagnostics = append(diagnostics, lsp.Diagnostic{
			Range:    sourceRange,
			Severity: lsp.DiagnosticSeverity(diagnostic.Severity),
			Code:     diagnostic.Code,
			Source:   "asp-lsp-css",
			Message:  diagnostic.Message,
		})
	}
	return diagnostics
}

// DocumentColors returns CSS language service colors mapped to the ASP source document.
func (c CSS) DocumentColors(parsed *core.ParsedDocument) (guarded []lsp.ColorInformation) {
	defer recoverService("css.DocumentColors", &guarded)
	cache := c.cachedCSSDocument(parsed)
	virtual := cache.virtual
	if strings.TrimSpace(virtual.Text) == "" {
		return []lsp.ColorInformation{}
	}
	source := cache.source
	items := c.service.FindDocumentColors(cache.service.document, cache.service.stylesheet)
	result := make([]lsp.ColorInformation, 0, len(items))
	for _, item := range items {
		r, ok := remapCSSRange(virtual, source, item.Range)
		if !ok {
			continue
		}
		result = append(result, lsp.ColorInformation{
			Range: r,
			Color: lsp.Color{
				Red: item.Color.Red, Green: item.Color.Green, Blue: item.Color.Blue, Alpha: item.Color.Alpha,
			},
		})
	}
	return result
}

// ColorPresentations returns CSS language service presentations mapped to the ASP source document.
func (c CSS) ColorPresentations(parsed *core.ParsedDocument, color lsp.Color, r lsp.Range) (guarded []lsp.ColorPresentation) {
	defer recoverService("css.ColorPresentations", &guarded)
	cache := c.cachedCSSDocument(parsed)
	virtual, source := cache.virtual, cache.source
	start, startOK := virtual.ToVirtualPosition(r.Start, source)
	end, endOK := virtual.ToVirtualPosition(r.End, source)
	if !startOK || !endOK {
		return []lsp.ColorPresentation{}
	}
	items := c.service.GetColorPresentations(cache.service.document, cache.service.stylesheet, csslsp.Color{
		Red: color.Red, Green: color.Green, Blue: color.Blue, Alpha: color.Alpha,
	}, csslsp.Range{Start: csslsp.Position(start), End: csslsp.Position(end)})
	result := make([]lsp.ColorPresentation, 0, len(items))
	for _, item := range items {
		presentation := lsp.ColorPresentation{Label: item.Label}
		if item.TextEdit != nil {
			mappedRange, ok := remapCSSRange(virtual, source, item.TextEdit.Range)
			if !ok {
				mappedRange = r
			}
			presentation.TextEdit = &lsp.TextEdit{Range: mappedRange, NewText: item.TextEdit.NewText}
		}
		result = append(result, presentation)
	}
	return result
}

// CodeActions returns CSS language service actions mapped to the ASP source document.
func (c CSS) CodeActions(parsed *core.ParsedDocument, r lsp.Range, diagnostics []lsp.Diagnostic, only []string) (guarded []lsp.CodeAction) {
	defer recoverService("css.CodeActions", &guarded)
	cache := c.cachedCSSDocument(parsed)
	virtual, source := cache.virtual, cache.source
	start, startOK := virtual.ToVirtualPosition(r.Start, source)
	end, endOK := virtual.ToVirtualPosition(r.End, source)
	if !startOK || !endOK {
		return nil
	}
	virtualDiagnostics := make([]csslsp.Diagnostic, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		virtualStart, startOK := virtual.ToVirtualPosition(diagnostic.Range.Start, source)
		virtualEnd, endOK := virtual.ToVirtualPosition(diagnostic.Range.End, source)
		if !startOK || !endOK {
			continue
		}
		virtualDiagnostics = append(virtualDiagnostics, csslsp.Diagnostic{
			Range: csslsp.Range{
				Start: csslsp.Position(virtualStart),
				End:   csslsp.Position(virtualEnd),
			},
			Severity: csslsp.DiagnosticSeverity(diagnostic.Severity),
			Code:     diagnostic.Code,
			Source:   diagnostic.Source,
			Message:  diagnostic.Message,
		})
	}
	virtualOnly := make([]csslsp.CodeActionKind, len(only))
	for index, kind := range only {
		virtualOnly[index] = csslsp.CodeActionKind(kind)
	}
	actions := c.service.DoCodeActions2(cache.service.document, csslsp.Range{
		Start: csslsp.Position(start),
		End:   csslsp.Position(end),
	}, csslsp.CodeActionContext{Diagnostics: virtualDiagnostics, Only: virtualOnly}, cache.service.stylesheet)
	result := make([]lsp.CodeAction, 0, len(actions))
	for _, action := range actions {
		mapped, ok := remapCSSCodeAction(virtual, source, parsed.URI, action)
		if ok {
			result = append(result, mapped)
		}
	}
	return result
}

func remapCSSCodeAction(virtual core.VirtualDocument, source *core.TextDocument, sourceURI string, action csslsp.CodeAction) (lsp.CodeAction, bool) {
	result := lsp.CodeAction{
		Title: action.Title,
		Kind:  string(action.Kind),
	}
	for _, diagnostic := range action.Diagnostics {
		r, ok := remapCSSRange(virtual, source, diagnostic.Range)
		if !ok {
			continue
		}
		sourceName := diagnostic.Source
		if sourceName == "" {
			sourceName = "asp-lsp-css"
		}
		result.Diagnostics = append(result.Diagnostics, lsp.Diagnostic{
			Range:    r,
			Severity: lsp.DiagnosticSeverity(diagnostic.Severity),
			Code:     diagnostic.Code,
			Source:   sourceName,
			Message:  diagnostic.Message,
		})
	}
	if action.Edit != nil {
		edit := &lsp.WorkspaceEdit{}
		for _, edits := range action.Edit.Changes {
			for _, item := range edits {
				r, ok := remapCSSRange(virtual, source, item.Range)
				if !ok {
					continue
				}
				if edit.Changes == nil {
					edit.Changes = map[string][]lsp.TextEdit{}
				}
				edit.Changes[sourceURI] = append(edit.Changes[sourceURI], lsp.TextEdit{Range: r, NewText: item.NewText})
			}
		}
		for _, change := range action.Edit.DocumentChanges {
			edits := make([]lsp.TextEdit, 0, len(change.Edits))
			for _, item := range change.Edits {
				r, ok := remapCSSRange(virtual, source, item.Range)
				if !ok {
					continue
				}
				edits = append(edits, lsp.TextEdit{Range: r, NewText: item.NewText})
			}
			if len(edits) == 0 {
				continue
			}
			edit.DocumentChanges = append(edit.DocumentChanges, map[string]any{
				"textDocument": map[string]any{
					"uri":     sourceURI,
					"version": change.TextDocument.Version,
				},
				"edits": edits,
			})
		}
		result.Edit = edit
	}
	if action.Command != nil {
		result.Command = &lsp.Command{
			Title:     action.Command.Title,
			Command:   action.Command.Command,
			Arguments: action.Command.Arguments,
		}
	}
	return result, true
}

func (c CSS) Highlights(parsed *core.ParsedDocument, position lsp.Position) (guarded []lsp.DocumentHighlight) {
	defer recoverService("css.Highlights", &guarded)
	virtual, source, doc, stylesheet, virtualPosition, ok := c.contextAt(parsed, position)
	if !ok {
		return nil
	}
	items := c.service.FindDocumentHighlights(doc, csslsp.Position(virtualPosition), stylesheet)
	result := make([]lsp.DocumentHighlight, 0, len(items))
	for _, item := range items {
		if r, ok := remapCSSRange(virtual, source, item.Range); ok {
			result = append(result, lsp.DocumentHighlight{Range: r, Kind: int(item.Kind)})
		}
	}
	return result
}

func (c CSS) DocumentSymbols(parsed *core.ParsedDocument) (guarded []lsp.DocumentSymbol) {
	defer recoverService("css.DocumentSymbols", &guarded)
	cache := c.cachedCSSDocument(parsed)
	virtual := cache.virtual
	if strings.TrimSpace(virtual.Text) == "" {
		return nil
	}
	source := cache.source
	items := c.service.FindDocumentSymbols2(cache.service.document, cache.service.stylesheet)
	result := make([]lsp.DocumentSymbol, 0, len(items))
	for _, item := range items {
		if symbol, ok := remapCSSDocumentSymbol(virtual, source, item); ok {
			result = append(result, symbol)
		}
	}
	return result
}

func (c CSS) FoldingRanges(parsed *core.ParsedDocument) (guarded []lsp.FoldingRange) {
	defer recoverService("css.FoldingRanges", &guarded)
	cache := c.cachedCSSSource(parsed)
	virtual := cache.virtual
	if strings.TrimSpace(virtual.Text) == "" {
		return nil
	}
	items := c.service.GetFoldingRanges(cache.service.document, nil)
	result := make([]lsp.FoldingRange, 0, len(items))
	source := cache.source
	for _, item := range items {
		start, startOK := virtualFoldingPositionToSource(virtual, source, item.StartLine, item.StartCharacter, true)
		end, endOK := virtualFoldingPositionToSource(virtual, source, item.EndLine, item.EndCharacter, false)
		if !startOK || !endOK {
			continue
		}
		kind := ""
		if item.Kind != nil {
			kind = string(*item.Kind)
		}
		result = append(result, lsp.FoldingRange{
			StartLine:      start.Line,
			StartCharacter: start.Character,
			EndLine:        end.Line,
			EndCharacter:   end.Character,
			Kind:           kind,
		})
	}
	return result
}

func (c CSS) SelectionRange(parsed *core.ParsedDocument, position lsp.Position) (guarded *lsp.SelectionRange) {
	defer recoverService("css.SelectionRange", &guarded)
	virtual, source, doc, stylesheet, virtualPosition, ok := c.contextAt(parsed, position)
	if !ok {
		return nil
	}
	items := c.service.GetSelectionRanges(doc, []csslsp.Position{csslsp.Position(virtualPosition)}, stylesheet)
	if len(items) == 0 {
		return nil
	}
	result, ok := remapCSSSelectionRange(virtual, source, items[0])
	if !ok {
		return nil
	}
	return &result
}

func (c CSS) PrepareRename(parsed *core.ParsedDocument, position lsp.Position) (guarded *lsp.Range) {
	defer recoverService("css.PrepareRename", &guarded)
	virtual, source, doc, stylesheet, virtualPosition, ok := c.contextAt(parsed, position)
	if !ok {
		return nil
	}
	r := c.service.PrepareRename(doc, csslsp.Position(virtualPosition), stylesheet)
	if r == nil {
		return nil
	}
	mapped, ok := remapCSSRange(virtual, source, *r)
	if !ok {
		return nil
	}
	return &mapped
}

func (c CSS) Rename(parsed *core.ParsedDocument, position lsp.Position, newName string) (guarded *lsp.WorkspaceEdit) {
	defer recoverService("css.Rename", &guarded)
	virtual, source, doc, stylesheet, virtualPosition, ok := c.contextAt(parsed, position)
	if !ok {
		return nil
	}
	edit := c.service.DoRename(doc, csslsp.Position(virtualPosition), newName, stylesheet)
	changes := map[string][]lsp.TextEdit{}
	for _, edits := range edit.Changes {
		for _, item := range edits {
			if r, ok := remapCSSRange(virtual, source, item.Range); ok {
				changes[parsed.URI] = append(changes[parsed.URI], lsp.TextEdit{Range: r, NewText: item.NewText})
			}
		}
	}
	if len(changes) == 0 {
		return nil
	}
	return &lsp.WorkspaceEdit{Changes: changes}
}

func (c CSS) contextAt(parsed *core.ParsedDocument, position lsp.Position) (core.VirtualDocument, *core.TextDocument, *csslsp.TextDocument, *cssls.Stylesheet, lsp.Position, bool) {
	cache := c.cachedCSSDocument(parsed)
	virtual, source := cache.virtual, cache.source
	virtualPosition, ok := virtual.ToVirtualPosition(position, source)
	if !ok {
		return virtual, source, nil, nil, lsp.Position{}, false
	}
	return virtual, source, cache.service.document, cache.service.stylesheet, virtualPosition, true
}

func remapCSSDocumentSymbol(virtual core.VirtualDocument, source *core.TextDocument, item csslsp.DocumentSymbol) (lsp.DocumentSymbol, bool) {
	r, ok := remapCSSRange(virtual, source, item.Range)
	if !ok {
		return lsp.DocumentSymbol{}, false
	}
	selection, ok := remapCSSRange(virtual, source, item.SelectionRange)
	if !ok {
		return lsp.DocumentSymbol{}, false
	}
	children := make([]lsp.DocumentSymbol, 0, len(item.Children))
	for _, child := range item.Children {
		if mapped, ok := remapCSSDocumentSymbol(virtual, source, child); ok {
			children = append(children, mapped)
		}
	}
	return lsp.DocumentSymbol{Name: item.Name, Kind: int(item.Kind), Range: r, SelectionRange: selection, Children: children}, true
}

func remapCSSSelectionRange(virtual core.VirtualDocument, source *core.TextDocument, item csslsp.SelectionRange) (lsp.SelectionRange, bool) {
	r, ok := remapCSSRange(virtual, source, item.Range)
	if !ok {
		return lsp.SelectionRange{}, false
	}
	result := lsp.SelectionRange{Range: r}
	if item.Parent != nil {
		parent, ok := remapCSSSelectionRange(virtual, source, *item.Parent)
		if ok {
			result.Parent = &parent
		}
	}
	return result, true
}

func virtualFoldingPositionToSource(virtual core.VirtualDocument, source *core.TextDocument, line int, character *int, first bool) (lsp.Position, bool) {
	if character != nil {
		if mapped, ok := virtual.ToSourcePosition(lsp.Position{Line: line, Character: *character}, source); ok {
			return mapped, true
		}
	}
	doc := core.NewTextDocument(virtual.URI, virtual.LanguageID, 0, virtual.Text)
	lineStart := doc.OffsetAt(lsp.Position{Line: line})
	lineEnd := len(virtual.Text)
	if line < doc.PositionAt(len(virtual.Text)).Line {
		lineEnd = doc.OffsetAt(lsp.Position{Line: line + 1})
	}
	selectedVirtual := -1
	for _, segment := range virtual.Segments {
		start := max(lineStart, segment.VirtualStart)
		end := min(lineEnd, segment.VirtualEnd)
		if start > end {
			continue
		}
		virtualOffset := start
		if !first {
			virtualOffset = end
		}
		selectedVirtual = virtualOffset
		if first {
			break
		}
	}
	if selectedVirtual < 0 {
		return lsp.Position{}, false
	}
	sourceOffset, ok := virtual.ToSourceOffset(selectedVirtual)
	if !ok {
		return lsp.Position{}, false
	}
	return source.PositionAt(sourceOffset), true
}

func cssEmptyValueDiagnostics(text string, existing []csslsp.Diagnostic) []csslsp.Diagnostic {
	diagnostics := []csslsp.Diagnostic{}
	document := core.NewTextDocument("", "css", 0, text)
	depth := 0
	var quote byte
	inComment := false
	for i := 0; i < len(text); i++ {
		if inComment {
			if text[i] == '*' && i+1 < len(text) && text[i+1] == '/' {
				inComment = false
				i++
			}
			continue
		}
		if quote != 0 {
			switch text[i] {
			case '\\':
				if i+1 < len(text) {
					i++
				}
			case quote:
				quote = 0
			case '\r', '\n':
				quote = 0
			}
			continue
		}
		switch text[i] {
		case '/':
			if i+1 < len(text) && text[i+1] == '*' {
				inComment = true
				i++
			}
		case '"', '\'':
			quote = text[i]
		case '{':
			depth++
		case '}':
			if depth > 0 {
				depth--
			}
		case ':':
			if depth <= 0 {
				continue
			}
			valueOffset := nextNonCSSValueToken(text, i+1)
			if valueOffset < 0 || valueOffset >= len(text) {
				continue
			}
			if text[valueOffset] != '}' && text[valueOffset] != ';' {
				continue
			}
			diagnosticRange := cssRangeForOffsets(document, valueOffset, valueOffset+1)
			if hasCSSDiagnosticRange(existing, diagnosticRange) {
				continue
			}
			diagnostics = append(diagnostics, csslsp.Diagnostic{
				Range:    diagnosticRange,
				Severity: csslsp.DiagnosticSeverity(lsp.DiagnosticSeverityError),
				Code:     "css-propertyvalueexpected",
				Source:   "css",
				Message:  "property value expected",
			})
		}
	}
	return diagnostics
}

func nextNonCSSValueToken(text string, offset int) int {
	for offset < len(text) {
		switch text[offset] {
		case ' ', '\t', '\r', '\n', '\f':
			offset++
		case '/':
			if offset+1 >= len(text) || text[offset+1] != '*' {
				return offset
			}
			end := strings.Index(text[offset+2:], "*/")
			if end < 0 {
				return -1
			}
			offset += end + 4
		default:
			return offset
		}
	}
	return -1
}

func cssRangeForOffsets(document *core.TextDocument, start int, end int) csslsp.Range {
	sourceRange := document.Range(start, end)
	return csslsp.Range{
		Start: csslsp.Position(sourceRange.Start),
		End:   csslsp.Position(sourceRange.End),
	}
}

func hasCSSDiagnosticRange(diagnostics []csslsp.Diagnostic, r csslsp.Range) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Range.Start.Line == r.Start.Line &&
			diagnostic.Range.Start.Character == r.Start.Character &&
			diagnostic.Range.End.Line == r.End.Line &&
			diagnostic.Range.End.Character == r.End.Character {
			return true
		}
	}
	return false
}
