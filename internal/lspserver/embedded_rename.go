package lspserver

import (
	"sort"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

type embeddedRenameKind int

const (
	embeddedRenameTag embeddedRenameKind = iota
	embeddedRenameClass
)

type embeddedRename struct {
	Kind  embeddedRenameKind
	Name  string
	Range lsp.Range
}

const embeddedReferenceRangesAnalysisKey = "lspserver.embeddedReferenceRanges.v1"

type embeddedReferenceRanges struct {
	tags              map[string][]lsp.Range
	cssClasses        map[string][]lsp.Range
	htmlClasses       map[string][]lsp.Range
	javascriptClasses map[string][]lsp.Range
}

func embeddedReferenceRangesFor(parsed *core.ParsedDocument) *embeddedReferenceRanges {
	if parsed == nil {
		return &embeddedReferenceRanges{}
	}
	if cached, ok := parsed.LoadRuntimeAnalysis(embeddedReferenceRangesAnalysisKey); ok {
		if ranges, ok := cached.(*embeddedReferenceRanges); ok {
			return ranges
		}
	}
	doc := core.SourceDocument(parsed)
	ranges := &embeddedReferenceRanges{
		tags:              indexHTMLTagNameRanges(parsed, doc),
		cssClasses:        indexCSSClassNameRanges(parsed, doc),
		htmlClasses:       indexHTMLClassNameRanges(parsed, doc),
		javascriptClasses: indexJavaScriptSelectorClassRanges(parsed, doc),
	}
	parsed.StoreRuntimeAnalysis(embeddedReferenceRangesAnalysisKey, ranges)
	return ranges
}

func (r *embeddedReferenceRanges) tagNames(name string) []lsp.Range {
	if r == nil {
		return nil
	}
	return r.tags[strings.ToLower(name)]
}

func (r *embeddedReferenceRanges) classNames(name string) []lsp.Range {
	if r == nil {
		return nil
	}
	result := make([]lsp.Range, 0, len(r.cssClasses[name])+len(r.htmlClasses[name])+len(r.javascriptClasses[name]))
	result = append(result, r.cssClasses[name]...)
	result = append(result, r.htmlClasses[name]...)
	result = append(result, r.javascriptClasses[name]...)
	return result
}

func embeddedRenameTarget(parsed *core.ParsedDocument, offset int) (embeddedRename, bool) {
	doc := core.SourceDocument(parsed)
	htmlVirtual := core.BuildVirtualDocument(parsed, core.LanguageHTML)
	if virtualOffset, ok := htmlVirtual.ToVirtualOffset(offset); ok {
		htmlText := maskEmbeddedHTMLComments(htmlVirtual.Text)
		if name, start, end, ok := htmlTagNameAt(htmlText, virtualOffset); ok {
			if r, ok := htmlVirtual.SourceRange(doc, start, end); ok {
				return embeddedRename{Kind: embeddedRenameTag, Name: name, Range: r}, true
			}
		}
	}
	cssVirtual := core.BuildVirtualDocument(parsed, core.LanguageCSS)
	if virtualOffset, ok := cssVirtual.ToVirtualOffset(offset); ok {
		cssText := maskEmbeddedCSSCompletionText(parsed, cssVirtual)
		if name, start, end, ok := cssClassNameAt(cssText, virtualOffset); ok {
			if r, ok := cssVirtual.SourceRange(doc, start, end); ok {
				return embeddedRename{Kind: embeddedRenameClass, Name: name, Range: r}, true
			}
		}
	}
	if virtualOffset, ok := htmlVirtual.ToVirtualOffset(offset); ok {
		htmlText := maskEmbeddedHTMLComments(htmlVirtual.Text)
		if name, start, end, ok := htmlClassNameAt(htmlText, virtualOffset); ok {
			if r, ok := htmlVirtual.SourceRange(doc, start, end); ok {
				return embeddedRename{Kind: embeddedRenameClass, Name: name, Range: r}, true
			}
		}
	}
	for _, language := range []core.EmbeddedLanguage{core.LanguageJavaScript, core.LanguageJScript} {
		virtual := core.BuildVirtualDocument(parsed, language)
		if virtualOffset, ok := virtual.ToVirtualOffset(offset); ok {
			text := maskEmbeddedJavaScriptComments(virtual.Text)
			if name, start, end, ok := javaScriptSelectorClassNameAt(text, virtualOffset); ok {
				if r, ok := virtual.SourceRange(doc, start, end); ok {
					return embeddedRename{Kind: embeddedRenameClass, Name: name, Range: r}, true
				}
			}
		}
	}
	return embeddedRename{}, false
}

func (s *Server) embeddedRenameEdit(parsed *core.ParsedDocument, offset int, newName string) (map[string]any, bool) {
	target, ok := embeddedRenameTarget(parsed, offset)
	if !ok || target.Name == "" || newName == "" {
		return nil, false
	}
	changes := map[string][]lsp.TextEdit{}
	switch target.Kind {
	case embeddedRenameTag:
		changes[parsed.URI] = textEditsForRanges(embeddedReferenceRangesFor(parsed).tagNames(target.Name), newName)
	case embeddedRenameClass:
		for uri, edits := range s.embeddedClassRenameChanges(parsed, target.Name, newName) {
			changes[uri] = append(changes[uri], edits...)
		}
	}
	if len(changes) == 0 {
		return nil, false
	}
	return map[string]any{"changes": changes}, true
}

func (s *Server) embeddedClassRenameChanges(parsed *core.ParsedDocument, name string, newName string) map[string][]lsp.TextEdit {
	changes := map[string][]lsp.TextEdit{}
	if !s.settings.WorkspaceSymbolRename {
		if edits := embeddedClassRenameEdits(parsed, name, newName); len(edits) > 0 {
			changes[parsed.URI] = edits
		}
		return changes
	}
	documents := append([]*core.ParsedDocument{parsed}, s.allWorkspaceReferenceDocuments()...)
	documents = dedupeParsedDocumentsByFileIdentity(documents)
	sort.Slice(documents, func(i, j int) bool { return documents[i].URI < documents[j].URI })
	for _, documentRanges := range s.referenceWorkspaceIndex.embeddedClassRanges(name, documents) {
		changes[documentRanges.URI] = textEditsForRanges(documentRanges.Ranges, newName)
	}
	return changes
}

func embeddedClassRenameEdits(parsed *core.ParsedDocument, name string, newName string) []lsp.TextEdit {
	return textEditsForRanges(embeddedReferenceRangesFor(parsed).classNames(name), newName)
}

func indexHTMLTagNameRanges(parsed *core.ParsedDocument, doc *core.TextDocument) map[string][]lsp.Range {
	virtual := core.BuildVirtualDocument(parsed, core.LanguageHTML)
	text := maskEmbeddedHTMLComments(virtual.Text)
	ranges := map[string][]lsp.Range{}
	for index := 0; index < len(text); index++ {
		if text[index] != '<' || index+1 >= len(text) {
			continue
		}
		start := index + 1
		if text[start] == '/' {
			start++
		}
		end := start
		for end < len(text) && isHTMLName(text[end]) {
			end++
		}
		if end <= start {
			continue
		}
		r, ok := virtual.SourceRange(doc, start, end)
		if !ok {
			continue
		}
		name := strings.ToLower(text[start:end])
		ranges[name] = append(ranges[name], r)
		index = end - 1
	}
	return ranges
}

func indexCSSClassNameRanges(parsed *core.ParsedDocument, doc *core.TextDocument) map[string][]lsp.Range {
	virtual := core.BuildVirtualDocument(parsed, core.LanguageCSS)
	text := maskEmbeddedCSSCompletionText(parsed, virtual)
	ranges := map[string][]lsp.Range{}
	for index := 1; index < len(text); index++ {
		if text[index-1] != '.' || !isClassNameStart(text[index]) {
			continue
		}
		start := index
		end := index + 1
		for end < len(text) && isClassName(text[end]) {
			end++
		}
		if r, ok := virtual.SourceRange(doc, start, end); ok {
			name := text[start:end]
			ranges[name] = append(ranges[name], r)
		}
		index = end
	}
	return ranges
}

func indexHTMLClassNameRanges(parsed *core.ParsedDocument, doc *core.TextDocument) map[string][]lsp.Range {
	virtual := core.BuildVirtualDocument(parsed, core.LanguageHTML)
	text := maskEmbeddedHTMLComments(virtual.Text)
	ranges := map[string][]lsp.Range{}
	for index := 0; index < len(text); index++ {
		if !isClassNameStart(text[index]) {
			continue
		}
		start := index
		end := index + 1
		for end < len(text) && isClassName(text[end]) {
			end++
		}
		if insideHTMLClassAttribute(text, start, end) {
			if r, ok := virtual.SourceRange(doc, start, end); ok {
				name := text[start:end]
				ranges[name] = append(ranges[name], r)
			}
		}
		index = end
	}
	return ranges
}

func indexJavaScriptSelectorClassRanges(parsed *core.ParsedDocument, doc *core.TextDocument) map[string][]lsp.Range {
	ranges := map[string][]lsp.Range{}
	for _, language := range []core.EmbeddedLanguage{core.LanguageJavaScript, core.LanguageJScript} {
		virtual := core.BuildVirtualDocument(parsed, language)
		text := maskEmbeddedJavaScriptComments(virtual.Text)
		for index := 0; index+1 < len(text); index++ {
			if text[index] != '.' || index == 0 || (text[index-1] != '"' && text[index-1] != '\'') || !isClassNameStart(text[index+1]) {
				continue
			}
			start := index + 1
			end := start + 1
			for end < len(text) && isClassName(text[end]) {
				end++
			}
			r, ok := virtual.SourceRange(doc, start, end)
			if !ok {
				continue
			}
			name := text[start:end]
			ranges[name] = append(ranges[name], r)
			index = end - 1
		}
	}
	return ranges
}

func textEditsForRanges(ranges []lsp.Range, newName string) []lsp.TextEdit {
	edits := make([]lsp.TextEdit, 0, len(ranges))
	for _, r := range ranges {
		edits = append(edits, lsp.TextEdit{Range: r, NewText: newName})
	}
	return edits
}

func appendDistinctTextEdits(existing []lsp.TextEdit, additions ...lsp.TextEdit) []lsp.TextEdit {
	for _, addition := range additions {
		duplicate := false
		for _, current := range existing {
			if current.Range == addition.Range && current.NewText == addition.NewText {
				duplicate = true
				break
			}
		}
		if !duplicate {
			existing = append(existing, addition)
		}
	}
	return existing
}

func htmlTagNameAt(text string, offset int) (string, int, int, bool) {
	open := strings.LastIndexByte(text[:clampOffset(text, offset)], '<')
	if open < 0 {
		return "", 0, 0, false
	}
	close := strings.IndexByte(text[open:], '>')
	if close < 0 || open+close < offset {
		return "", 0, 0, false
	}
	start := open + 1
	if start < len(text) && text[start] == '/' {
		start++
	}
	if start >= len(text) || !isHTMLNameStart(text[start]) {
		return "", 0, 0, false
	}
	end := start + 1
	for end < len(text) && isHTMLName(text[end]) {
		end++
	}
	if offset < start || offset > end {
		return "", 0, 0, false
	}
	return text[start:end], start, end, true
}

func cssClassNameAt(text string, offset int) (string, int, int, bool) {
	start, end := classTokenBounds(text, offset)
	if start <= 0 || end <= start || text[start-1] != '.' {
		return "", 0, 0, false
	}
	return text[start:end], start, end, true
}

func javaScriptSelectorClassNameAt(text string, offset int) (string, int, int, bool) {
	start, end := classTokenBounds(text, offset)
	if start < 2 || end <= start || text[start-1] != '.' || (text[start-2] != '"' && text[start-2] != '\'') {
		return "", 0, 0, false
	}
	return text[start:end], start, end, true
}

func htmlClassNameAt(text string, offset int) (string, int, int, bool) {
	start, end := classTokenBounds(text, offset)
	if end <= start {
		return "", 0, 0, false
	}
	if !insideHTMLClassAttribute(text, start, end) {
		return "", 0, 0, false
	}
	return text[start:end], start, end, true
}

func htmlClassCompletionItems(parsed *core.ParsedDocument, doc *core.TextDocument, position lsp.Position) []lsp.CompletionItem {
	offset := doc.OffsetAt(position)
	start, end := classTokenBounds(doc.Text, offset)
	if !insideHTMLClassAttribute(doc.Text, start, end) {
		return nil
	}
	names := collectHTMLClassCompletionNames(parsed, doc)
	existing := classTokensInCurrentAttribute(doc.Text, start, end)
	items := make([]lsp.CompletionItem, 0, len(names))
	editRange := doc.Range(start, end)
	for _, name := range names {
		if _, ok := existing[name]; ok {
			continue
		}
		items = append(items, lsp.CompletionItem{
			Label:    name,
			Kind:     lsp.CompletionItemKindValue,
			Detail:   "HTML class",
			TextEdit: &lsp.TextEdit{Range: editRange, NewText: name},
		})
	}
	return items
}

func htmlIDCompletionItems(parsed *core.ParsedDocument, doc *core.TextDocument, position lsp.Position) []lsp.CompletionItem {
	offset := doc.OffsetAt(position)
	start, end := classTokenBounds(doc.Text, offset)
	if !insideHTMLAttribute(doc.Text, start, end, "id") {
		return nil
	}
	names := collectHTMLIDCompletionNames(parsed, doc)
	items := make([]lsp.CompletionItem, 0, len(names))
	editRange := doc.Range(start, end)
	for _, name := range names {
		items = append(items, lsp.CompletionItem{
			Label:    name,
			Kind:     lsp.CompletionItemKindValue,
			Detail:   "HTML id",
			TextEdit: &lsp.TextEdit{Range: editRange, NewText: name},
		})
	}
	return items
}

func cssSelectorCompletionItems(parsed *core.ParsedDocument, doc *core.TextDocument, position lsp.Position) []lsp.CompletionItem {
	offset := doc.OffsetAt(position)
	region := completionRegionAt(parsed, offset)
	if region == nil || region.Language != core.LanguageCSS {
		return nil
	}
	start, end := classTokenBounds(doc.Text, offset)
	if start <= 0 {
		return nil
	}
	marker := doc.Text[start-1]
	var names []string
	detail := ""
	switch marker {
	case '.':
		names = collectHTMLClassCompletionNames(parsed, doc)
		detail = "CSS class"
	case '#':
		names = collectHTMLIDCompletionNames(parsed, doc)
		detail = "CSS id"
	default:
		return nil
	}
	items := make([]lsp.CompletionItem, 0, len(names))
	editRange := doc.Range(start, end)
	for _, name := range names {
		items = append(items, lsp.CompletionItem{
			Label:    name,
			Kind:     lsp.CompletionItemKindValue,
			Detail:   detail,
			TextEdit: &lsp.TextEdit{Range: editRange, NewText: name},
		})
	}
	return items
}

func collectHTMLClassCompletionNames(parsed *core.ParsedDocument, doc *core.TextDocument) []string {
	seen := map[string]struct{}{}
	var names []string
	add := func(name string) {
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	virtual := core.BuildVirtualDocument(parsed, core.LanguageCSS)
	text := maskEmbeddedCSSCompletionText(parsed, virtual)
	for i := 1; i < len(text); i++ {
		if text[i-1] != '.' || !isClassNameStart(text[i]) {
			continue
		}
		start := i
		end := i + 1
		for end < len(text) && isClassName(text[end]) {
			end++
		}
		add(text[start:end])
		i = end
	}
	htmlVirtual := core.BuildVirtualDocument(parsed, core.LanguageHTML)
	htmlText := maskEmbeddedHTMLComments(htmlVirtual.Text)
	for i := 0; i < len(htmlText); i++ {
		if !isClassNameStart(htmlText[i]) {
			continue
		}
		start := i
		end := i + 1
		for end < len(htmlText) && isClassName(htmlText[end]) {
			end++
		}
		if insideHTMLClassAttribute(htmlText, start, end) {
			add(htmlText[start:end])
		}
		i = end
	}
	sort.Strings(names)
	return names
}

func collectHTMLIDCompletionNames(parsed *core.ParsedDocument, doc *core.TextDocument) []string {
	seen := map[string]struct{}{}
	var names []string
	add := func(name string) {
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	virtual := core.BuildVirtualDocument(parsed, core.LanguageCSS)
	text := maskEmbeddedCSSCompletionText(parsed, virtual)
	for i := 1; i < len(text); i++ {
		if text[i-1] != '#' || !isClassNameStart(text[i]) {
			continue
		}
		start := i
		end := i + 1
		for end < len(text) && isClassName(text[end]) {
			end++
		}
		add(text[start:end])
		i = end
	}
	htmlVirtual := core.BuildVirtualDocument(parsed, core.LanguageHTML)
	htmlText := maskEmbeddedHTMLComments(htmlVirtual.Text)
	for _, match := range htmlIDAttributePattern.FindAllStringSubmatchIndex(htmlText, -1) {
		nameStart, nameEnd := -1, -1
		if len(match) >= 4 && match[2] >= 0 && match[3] >= 0 {
			nameStart, nameEnd = match[2], match[3]
		} else if len(match) >= 6 && match[4] >= 0 && match[5] >= 0 {
			nameStart, nameEnd = match[4], match[5]
		}
		if nameStart >= 0 && nameEnd >= 0 {
			add(htmlText[nameStart:nameEnd])
		}
	}
	sort.Strings(names)
	return names
}

func maskEmbeddedCSSCompletionText(parsed *core.ParsedDocument, virtual core.VirtualDocument) string {
	ends := cssVirtualRegionEnds(parsed, len(virtual.Text))
	if len(ends) == 0 {
		return maskEmbeddedCSSNonCode(virtual.Text)
	}
	return maskEmbeddedCSSNonCodeAtRegionBoundaries(virtual.Text, ends)
}

func cssVirtualRegionEnds(parsed *core.ParsedDocument, textLength int) []int {
	if parsed == nil || textLength == 0 {
		return nil
	}
	sorted := append([]core.Region(nil), parsed.Regions...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Start == sorted[j].Start {
			return sorted[i].End < sorted[j].End
		}
		return sorted[i].Start < sorted[j].Start
	})
	ends := make([]int, 0)
	virtualOffset := 0
	for _, region := range sorted {
		if region.Language != core.LanguageCSS {
			continue
		}
		if region.ContentStart < 0 || region.ContentEnd < region.ContentStart {
			return nil
		}
		prefixLength := 1
		suffixLength := 1
		if region.Kind == core.RegionStyleAttribute {
			prefixLength = 2
			suffixLength = 2
		}
		virtualOffset += prefixLength + (region.ContentEnd - region.ContentStart) + suffixLength
		ends = append(ends, virtualOffset)
	}
	if len(ends) == 0 || virtualOffset != textLength {
		return nil
	}
	return ends
}

func maskEmbeddedCSSNonCodeAtRegionBoundaries(text string, ends []int) string {
	if len(ends) == 0 {
		return maskEmbeddedCSSNonCode(text)
	}
	previousEnd := 0
	for _, end := range ends {
		if end <= previousEnd || end > len(text) {
			return maskEmbeddedCSSNonCode(text)
		}
		previousEnd = end
	}
	if previousEnd != len(text) {
		return maskEmbeddedCSSNonCode(text)
	}

	masked := []byte(text)
	regionIndex := 0
	regionEnd := ends[regionIndex]
	var comment bool
	var quote byte
	for index := 0; index < len(masked); {
		if index >= regionEnd {
			regionIndex++
			if regionIndex >= len(ends) {
				break
			}
			regionEnd = ends[regionIndex]
			comment = false
			quote = 0
			continue
		}
		if comment {
			if index+1 < regionEnd && masked[index] == '*' && masked[index+1] == '/' {
				masked[index], masked[index+1] = ' ', ' '
				index += 2
				comment = false
				continue
			}
			if masked[index] != '\n' && masked[index] != '\r' {
				masked[index] = ' '
			}
			index++
			continue
		}
		if quote != 0 {
			if masked[index] == '\\' {
				masked[index] = ' '
				index++
				if index < regionEnd {
					if masked[index] != '\n' && masked[index] != '\r' {
						masked[index] = ' '
					}
					index++
				}
				continue
			}
			if masked[index] == quote {
				masked[index] = ' '
				index++
				quote = 0
				continue
			}
			if masked[index] != '\n' && masked[index] != '\r' {
				masked[index] = ' '
			}
			index++
			continue
		}
		if index+1 < regionEnd && masked[index] == '/' && masked[index+1] == '*' {
			masked[index], masked[index+1] = ' ', ' '
			index += 2
			comment = true
			continue
		}
		if masked[index] == '\'' || masked[index] == '"' {
			quote = masked[index]
			masked[index] = ' '
			index++
			continue
		}
		index++
	}
	return string(masked)
}

func classTokensInCurrentAttribute(text string, start, end int) map[string]struct{} {
	result := map[string]struct{}{}
	quoteStart, quoteEnd, ok := htmlAttributeQuoteBounds(text, start, end, "class")
	if !ok {
		return result
	}
	for i := quoteStart + 1; i < quoteEnd; i++ {
		if !isClassNameStart(text[i]) {
			continue
		}
		tokenStart := i
		tokenEnd := i + 1
		for tokenEnd < quoteEnd && isClassName(text[tokenEnd]) {
			tokenEnd++
		}
		if tokenStart != start || tokenEnd != end {
			result[text[tokenStart:tokenEnd]] = struct{}{}
		}
		i = tokenEnd
	}
	return result
}

func classTokenBounds(text string, offset int) (int, int) {
	offset = clampOffset(text, offset)
	start := offset
	for start > 0 && isClassName(text[start-1]) {
		start--
	}
	end := offset
	for end < len(text) && isClassName(text[end]) {
		end++
	}
	return start, end
}

func insideHTMLClassAttribute(text string, start, end int) bool {
	return insideHTMLAttribute(text, start, end, "class")
}

func insideHTMLAttribute(text string, start, end int, attrName string) bool {
	_, _, ok := htmlAttributeQuoteBounds(text, start, end, attrName)
	return ok
}

func htmlAttributeQuoteBounds(text string, start, end int, attrName string) (int, int, bool) {
	quoteStart := -1
	for i := start - 1; i >= 0; i-- {
		if text[i] == '"' || text[i] == '\'' {
			quoteStart = i
			break
		}
		if text[i] == '<' || text[i] == '>' {
			return 0, 0, false
		}
	}
	if quoteStart < 0 {
		return 0, 0, false
	}
	quote := text[quoteStart]
	for i := end; i < len(text); i++ {
		if text[i] == quote {
			break
		}
		if text[i] == '<' || text[i] == '>' {
			return 0, 0, false
		}
	}
	attrStart := strings.LastIndexByte(text[:quoteStart], '<')
	if attrStart < 0 {
		return 0, 0, false
	}
	eq := strings.LastIndexByte(text[attrStart:quoteStart], '=')
	if eq < 0 {
		return 0, 0, false
	}
	eq += attrStart
	nameEnd := eq
	for nameEnd > attrStart && (text[nameEnd-1] == ' ' || text[nameEnd-1] == '\t' || text[nameEnd-1] == '\r' || text[nameEnd-1] == '\n') {
		nameEnd--
	}
	nameStart := nameEnd
	for nameStart > attrStart && isHTMLName(text[nameStart-1]) {
		nameStart--
	}
	if nameStart == nameEnd || !strings.EqualFold(text[nameStart:nameEnd], attrName) {
		return 0, 0, false
	}
	quoteEnd := end
	for quoteEnd < len(text) && text[quoteEnd] != quote {
		quoteEnd++
	}
	if quoteEnd >= len(text) {
		return 0, 0, false
	}
	return quoteStart, quoteEnd, true
}

func clampOffset(text string, offset int) int {
	if offset < 0 {
		return 0
	}
	if offset > len(text) {
		return len(text)
	}
	return offset
}

func isHTMLNameStart(b byte) bool {
	return b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z'
}

func isHTMLName(b byte) bool {
	return isHTMLNameStart(b) || b >= '0' && b <= '9' || b == '-' || b == ':'
}

func isClassNameStart(b byte) bool {
	return b == '_' || b == '-' || b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z'
}

func isClassName(b byte) bool {
	return isClassNameStart(b) || b >= '0' && b <= '9'
}
