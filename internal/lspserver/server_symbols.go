package lspserver

import (
	"context"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

func (s *Server) workspaceSymbols(ctx context.Context, query string) []lsp.SymbolInformation {
	s.mu.Lock()
	openDocs := make([]*core.TextDocument, 0, len(s.documents))
	openURIs := map[string]struct{}{}
	for uri, doc := range s.documents {
		openDocs = append(openDocs, doc)
		openURIs[uri] = struct{}{}
	}
	workspaceDocs := make([]*core.TextDocument, 0, len(s.workspace))
	for uri, doc := range s.workspace {
		if _, open := openURIs[uri]; !open {
			workspaceDocs = append(workspaceDocs, doc)
		}
	}
	cacheDirectory := s.settings.CacheDirectory
	cacheFreshness := s.settings.CacheFreshness
	s.mu.Unlock()
	workspaceDocs = s.refreshWorkspaceSymbolDocuments(workspaceDocs, cacheDirectory, cacheFreshness)
	normalizedQuery := strings.ToLower(query)
	symbols := make([]lsp.SymbolInformation, 0)
	for _, doc := range openDocs {
		if ctx.Err() != nil {
			return symbols
		}
		parsed := s.parseTextDocument(doc, s.settings.DefaultLanguage)
		symbols = append(symbols, s.workspaceSymbolsForOpenDocument(ctx, doc, parsed, query)...)
	}
	for _, doc := range workspaceDocs {
		if ctx.Err() != nil {
			return symbols
		}
		if !workspaceDocumentMayMatchQuery(doc, query, normalizedQuery) {
			continue
		}
		parsed := s.parseTextDocument(doc, s.settings.DefaultLanguage)
		symbols = append(symbols, s.workspaceSymbolsForDocument(ctx, doc, parsed, query)...)
	}
	symbols = append(symbols, s.legacyUndefinedGlobalWorkspaceSymbols(ctx, query)...)
	return symbols
}

func workspaceDocumentMayMatchQuery(doc *core.TextDocument, query, normalizedQuery string) bool {
	if doc == nil || query == "" {
		return doc != nil
	}
	if strings.Contains(doc.Text, query) {
		return true
	}
	if isASCII(normalizedQuery) {
		return core.ContainsASCIIFold(doc.Text, normalizedQuery)
	}
	return strings.Contains(strings.ToLower(doc.Text), normalizedQuery)
}

func isASCII(value string) bool {
	for index := range len(value) {
		if value[index] >= 0x80 {
			return false
		}
	}
	return true
}

func (s *Server) workspaceSymbolsForDocument(ctx context.Context, doc *core.TextDocument, parsed *core.ParsedDocument, query string) []lsp.SymbolInformation {
	return s.workspaceSymbolsForDocumentWithProject(ctx, doc, parsed, query, false)
}

func (s *Server) workspaceSymbolsForOpenDocument(ctx context.Context, doc *core.TextDocument, parsed *core.ParsedDocument, query string) []lsp.SymbolInformation {
	return s.workspaceSymbolsForDocumentWithProject(ctx, doc, parsed, query, true)
}

func (s *Server) workspaceSymbolsForDocumentWithProject(ctx context.Context, doc *core.TextDocument, parsed *core.ParsedDocument, query string, includeProject bool) []lsp.SymbolInformation {
	symbols := make([]lsp.SymbolInformation, 0)
	symbols = append(symbols, s.vbWorkspaceSymbols(ctx, parsed, query, includeProject)...)
	if includeProject && !workspaceDocumentMayMatchQuery(doc, query, strings.ToLower(query)) {
		return symbols
	}
	symbols = append(symbols, includeWorkspaceSymbols(parsed, query)...)
	symbols = append(symbols, htmlAttributeWorkspaceSymbols(parsed, query)...)
	if ctx.Err() != nil {
		return symbols
	}
	s.cssMu.Lock()
	cssSymbols := s.css.DocumentSymbols(parsed)
	s.cssMu.Unlock()
	symbols = append(symbols, documentWorkspaceSymbols(parsed.URI, "css", cssSymbols, query)...)
	javaScriptSymbols := s.javaScriptDocumentSymbols(ctx, doc, parsed)
	symbols = append(symbols, documentWorkspaceSymbols(parsed.URI, "javascript", javaScriptSymbols, query)...)
	return symbols
}

func (s *Server) vbWorkspaceSymbols(ctx context.Context, parsed *core.ParsedDocument, query string, includeProject bool) []lsp.SymbolInformation {
	if parsed == nil {
		return nil
	}
	documents := []*core.ParsedDocument{parsed}
	if includeProject {
		included, complete := s.includedDocumentsContextResult(ctx, parsed)
		if !complete || ctx.Err() != nil {
			return nil
		}
		documents = append(documents, included...)
	}
	documentByURI := make(map[string]*core.ParsedDocument, len(documents))
	for _, document := range documents {
		if document != nil {
			documentByURI[document.URI] = document
		}
	}
	canonicalImplicitIDs := map[string]string{}
	if includeProject {
		canonicalImplicitIDs = s.graphCanonicalImplicitDeclarationIDs(documents, documentByURI)
	}
	normalizedQuery := strings.ToLower(query)
	symbols := make([]lsp.SymbolInformation, 0)
	for _, document := range documents {
		if ctx != nil && ctx.Err() != nil {
			return nil
		}
		if document == nil {
			continue
		}
		if normalizedQuery != "" && !workspaceTextMayMatchQuery(document.Text, query, normalizedQuery) {
			continue
		}
		for _, declaration := range s.cachedVBDeclarations(document) {
			if normalizedQuery != "" && !strings.Contains(strings.ToLower(declaration.Name), normalizedQuery) {
				continue
			}
			if declaration.Implicit && includeProject {
				canonicalID := canonicalImplicitIDs[strings.ToLower(declaration.Name)]
				if canonicalID != "" && canonicalID != graphDeclarationNodeID(document.URI, declaration.Name, declaration.Range) {
					continue
				}
			}
			symbols = append(symbols, lsp.SymbolInformation{
				Name:          declaration.Name,
				Kind:          vbWorkspaceSymbolKind(declaration.Kind),
				Location:      lsp.Location{URI: document.URI, Range: declaration.Range},
				ContainerName: declaration.MemberOf,
			})
		}
	}
	if includeProject {
		zeroRange := lsp.Range{}
		for name, setting := range s.settings.VBScriptGlobals {
			if ctx != nil && ctx.Err() != nil {
				return nil
			}
			if !validVBWorkspaceGlobal(name) || setting.typeName() == "" || normalizedQuery != "" && !strings.Contains(strings.ToLower(name), normalizedQuery) {
				continue
			}
			kind := 13
			if setting.Kind == "constant" {
				kind = 14
			}
			symbols = append(symbols, lsp.SymbolInformation{
				Name:     name,
				Kind:     kind,
				Location: lsp.Location{URI: parsed.URI + "#runtime-global", Range: zeroRange},
			})
		}
	}
	return symbols
}

func workspaceTextMayMatchQuery(text, query, normalizedQuery string) bool {
	if query == "" || strings.Contains(text, query) {
		return true
	}
	if isASCII(normalizedQuery) {
		return core.ContainsASCIIFold(text, normalizedQuery)
	}
	return strings.Contains(strings.ToLower(text), normalizedQuery)
}

func vbWorkspaceSymbolKind(kind string) int {
	switch kind {
	case "class":
		return 5
	case "property":
		return 7
	case "field":
		return 8
	case "constant", "const":
		return 14
	case "parameter", "variable":
		return 13
	default:
		return 12
	}
}

func validVBWorkspaceGlobal(name string) bool {
	if name == "" || !(name[0] >= 'A' && name[0] <= 'Z' || name[0] >= 'a' && name[0] <= 'z') {
		return false
	}
	for index := 1; index < len(name); index++ {
		if !isCompletionIdentifier(name[index]) {
			return false
		}
	}
	return true
}

func (s *Server) refreshWorkspaceSymbolDocuments(docs []*core.TextDocument, cacheDirectory, cacheFreshness string) []*core.TextDocument {
	if cacheFreshness != "metadata" || len(docs) == 0 {
		return docs
	}
	refreshed := make([]*core.TextDocument, 0, len(docs))
	stale := false
	for _, doc := range docs {
		if doc == nil {
			continue
		}
		path := fileURIPath(doc.URI)
		if path == "" {
			refreshed = append(refreshed, doc)
			continue
		}
		if known, current := s.sourceSnapshotMetadataCurrent(path); known && current {
			refreshed = append(refreshed, doc)
			continue
		} else if known {
			s.invalidateSourceSnapshot(path)
		}
		content, err := s.readChangedWorkspaceTextFile(path)
		if err != nil || content == doc.Text {
			refreshed = append(refreshed, doc)
			continue
		}
		updated := core.NewTextDocument(doc.URI, doc.LanguageID, doc.Version+1, content)
		refreshed = append(refreshed, updated)
		stale = true
		s.mu.Lock()
		if current := s.workspace[doc.URI]; current == doc || (current != nil && current.Text == doc.Text) {
			s.workspace[doc.URI] = updated
			s.markJavaScriptDocumentsChangedLocked()
			s.markWorkspaceDiagnosticsChanged()
		}
		s.mu.Unlock()
	}
	if stale && cacheDirectory != "" {
		s.logAnalysisDatabaseEvent("workspaceIndex", "stale", map[string]any{"documents": len(refreshed)})
	}
	return refreshed
}

var (
	htmlIDAttributePattern                = regexp.MustCompile(`(?is)\bid\s*=\s*(?:"([^"]*)"|'([^']*)')`)
	htmlWorkspaceAttributePattern         = regexp.MustCompile(`(?i)\b(?:id|name)=(?:"([^"]+)"|'([^']+)')`)
	javaScriptConstDeclarationWordPattern = regexp.MustCompile(`\bconst\b`)
)

func includeWorkspaceSymbols(parsed *core.ParsedDocument, query string) []lsp.SymbolInformation {
	if parsed == nil {
		return nil
	}
	normalizedQuery := strings.ToLower(query)
	symbols := []lsp.SymbolInformation{}
	for _, include := range parsed.Includes {
		if normalizedQuery != "" && !strings.Contains(strings.ToLower(include.Path), normalizedQuery) {
			continue
		}
		symbols = append(symbols, lsp.SymbolInformation{
			Name:          include.Path,
			Kind:          1,
			Location:      lsp.Location{URI: parsed.URI, Range: include.Range},
			ContainerName: "include",
		})
	}
	return symbols
}

func htmlAttributeWorkspaceSymbols(parsed *core.ParsedDocument, query string) []lsp.SymbolInformation {
	if parsed == nil {
		return nil
	}
	normalizedQuery := strings.ToLower(query)
	virtual := core.BuildVirtualDocument(parsed, core.LanguageHTML)
	virtualText := maskEmbeddedHTMLComments(virtual.Text)
	source := core.SourceDocument(parsed)
	symbols := []lsp.SymbolInformation{}
	for _, match := range htmlWorkspaceAttributePattern.FindAllStringSubmatchIndex(virtualText, -1) {
		nameStart, nameEnd := -1, -1
		if len(match) >= 4 && match[2] >= 0 && match[3] >= 0 {
			nameStart, nameEnd = match[2], match[3]
		} else if len(match) >= 6 && match[4] >= 0 && match[5] >= 0 {
			nameStart, nameEnd = match[4], match[5]
		}
		if nameStart < 0 || nameEnd < 0 {
			continue
		}
		sourceStart, startOK := virtual.ToSourceOffset(nameStart)
		sourceEnd, endOK := virtual.ToSourceOffset(nameEnd)
		if !startOK || !endOK {
			continue
		}
		region := core.RegionAt(parsed, sourceStart)
		if region == nil || region.Language != core.LanguageHTML {
			continue
		}
		name := virtualText[nameStart:nameEnd]
		if normalizedQuery != "" && !strings.Contains(strings.ToLower(name), normalizedQuery) {
			continue
		}
		symbols = append(symbols, lsp.SymbolInformation{
			Name:          name,
			Kind:          20,
			Location:      lsp.Location{URI: parsed.URI, Range: source.Range(sourceStart, sourceEnd)},
			ContainerName: "html",
		})
	}
	return symbols
}

func documentWorkspaceSymbols(uri, container string, documentSymbols []lsp.DocumentSymbol, query string) []lsp.SymbolInformation {
	normalizedQuery := strings.ToLower(query)
	symbols := make([]lsp.SymbolInformation, 0, len(documentSymbols))
	for _, symbol := range documentSymbols {
		if normalizedQuery != "" && !strings.Contains(strings.ToLower(symbol.Name), normalizedQuery) {
			continue
		}
		symbols = append(symbols, lsp.SymbolInformation{
			Name:          symbol.Name,
			Kind:          symbol.Kind,
			Location:      lsp.Location{URI: uri, Range: symbol.SelectionRange},
			ContainerName: container,
		})
	}
	return symbols
}

type aspIncludeCompletionContext struct {
	kind         string
	prefix       string
	replaceStart int
}

type aspDirectiveValueCompletionContext struct {
	attribute    string
	replaceStart int
}

const insertTextFormatSnippet = 2

var (
	includeModeCompletionPattern    = regexp.MustCompile(`(?i)^(\s*#include\s+)([A-Za-z]*)$`)
	includeKeywordCompletionPattern = regexp.MustCompile(`(?i)^(\s*)(#?[A-Za-z]*)$`)
	aspDirectiveValuePattern        = regexp.MustCompile(`(?i)([A-Za-z][A-Za-z0-9]*)\s*=\s*(?:"[^"]*|'[^']*|[^\s%>]*)$`)
)

func aspIncludeCompletions(doc *core.TextDocument, position lsp.Position) []lsp.CompletionItem {
	offset := doc.OffsetAt(position)
	context := aspIncludeCompletionContextAt(doc.Text, offset)
	if context == nil {
		return nil
	}
	r := doc.Range(context.replaceStart, offset)
	detail := "Classic ASP include directive"
	documentation := "Inserts a Classic ASP include directive."
	if context.kind == "includeMode" {
		return []lsp.CompletionItem{
			includeModeCompletion("file", detail, documentation, r),
			includeModeCompletion("virtual", detail, documentation, r),
		}
	}
	prefix := "<!-- "
	if context.kind == "comment" {
		prefix = ""
	}
	snippets := []lsp.CompletionItem{
		includeSnippetCompletion("file", prefix, context.prefix, detail, documentation, r),
		includeSnippetCompletion("virtual", prefix, context.prefix, detail, documentation, r),
	}
	if context.kind != "comment" {
		return snippets
	}
	return append([]lsp.CompletionItem{{
		Label:         "#include",
		Kind:          lsp.CompletionItemKindKeyword,
		Detail:        detail,
		Documentation: documentation,
		InsertText:    "#include ",
		TextEdit:      &lsp.TextEdit{Range: r, NewText: "#include "},
		FilterText:    includeKeywordFilterText(context.prefix),
		SortText:      "0_include",
	}}, snippets...)
}

func includeModeCompletion(mode, detail, documentation string, r lsp.Range) lsp.CompletionItem {
	newText := mode + `="${1:path}"`
	return lsp.CompletionItem{
		Label:            mode,
		Kind:             lsp.CompletionItemKindProperty,
		Detail:           detail,
		Documentation:    documentation,
		InsertText:       newText,
		TextEdit:         &lsp.TextEdit{Range: r, NewText: newText},
		InsertTextFormat: insertTextFormatSnippet,
		SortText:         "0_" + mode,
	}
}

func includeSnippetCompletion(mode, prefix, typedPrefix, detail, documentation string, r lsp.Range) lsp.CompletionItem {
	newText := prefix + "#include " + mode + `="${1:path}" -->`
	return lsp.CompletionItem{
		Label:            "#include " + mode,
		Kind:             lsp.CompletionItemKindSnippet,
		Detail:           detail,
		Documentation:    documentation,
		InsertText:       newText,
		TextEdit:         &lsp.TextEdit{Range: r, NewText: newText},
		InsertTextFormat: insertTextFormatSnippet,
		FilterText:       includeSnippetFilterText(mode, typedPrefix),
		SortText:         "0_include_" + mode,
	}
}

func aspIncludeCompletionContextAt(text string, offset int) *aspIncludeCompletionContext {
	if offset < 0 {
		offset = 0
	}
	if offset > len(text) {
		offset = len(text)
	}
	before := text[:offset]
	commentStart := strings.LastIndex(before, "<!--")
	if commentStart >= 0 && strings.LastIndex(before, "-->") < commentStart {
		bodyStart := commentStart + len("<!--")
		body := before[bodyStart:]
		if match := includeModeCompletionPattern.FindStringSubmatchIndex(body); match != nil {
			return &aspIncludeCompletionContext{
				kind:         "includeMode",
				prefix:       body[match[4]:match[5]],
				replaceStart: bodyStart + match[3],
			}
		}
		if match := includeKeywordCompletionPattern.FindStringSubmatchIndex(body); match != nil {
			return &aspIncludeCompletionContext{
				kind:         "comment",
				prefix:       body[match[4]:match[5]],
				replaceStart: bodyStart + match[3],
			}
		}
		return nil
	}
	if isHTMLTextCompletionContext(text, offset) {
		replaceStart := htmlTextCompletionReplaceStart(text, offset)
		return &aspIncludeCompletionContext{kind: "html", prefix: text[replaceStart:offset], replaceStart: replaceStart}
	}
	return nil
}

func includeKeywordFilterText(prefix string) string {
	if strings.HasPrefix(prefix, "#") {
		return "#include include inc"
	}
	return "include inc #include"
}

func includeSnippetFilterText(mode, prefix string) string {
	if strings.HasPrefix(prefix, "#") {
		return "#include " + mode + " include " + mode + " inc"
	}
	return "include " + mode + " inc #include " + mode
}

func htmlTextCompletionReplaceStart(text string, offset int) int {
	start := offset
	for start > 0 {
		b := text[start-1]
		if b != '#' && (b < 'A' || b > 'Z') && (b < 'a' || b > 'z') {
			break
		}
		start--
	}
	return start
}

func isHTMLTextCompletionContext(text string, offset int) bool {
	lineStart := 0
	if newline := strings.LastIndexAny(text[:offset], "\n\r"); newline >= 0 {
		lineStart = newline + 1
	}
	prefix := text[lineStart:offset]
	return strings.LastIndex(prefix, "<") <= strings.LastIndex(prefix, ">")
}

func aspDirectiveCompletionItems(doc *core.TextDocument, parsed *core.ParsedDocument, position lsp.Position, offset int) []lsp.CompletionItem {
	if items := aspDirectiveOpenCompletions(doc, position, offset); len(items) > 0 {
		return items
	}
	region := core.RegionAt(parsed, offset)
	if region == nil || region.Language != core.LanguageASPDirective || offset < region.ContentStart || offset > region.ContentEnd {
		return nil
	}
	if context := aspDirectiveValueContextAt(doc.Text, *region, offset); context != nil {
		return aspDirectiveValueCompletions(doc, position, context)
	}
	r := aspDirectiveWordRange(doc, offset)
	names := []string{"Language", "CodePage", "LCID", "Transaction", "EnableSessionState"}
	items := make([]lsp.CompletionItem, 0, len(names))
	for i, name := range names {
		items = append(items, lsp.CompletionItem{
			Label:    name,
			Kind:     lsp.CompletionItemKindProperty,
			Detail:   "Classic ASP directive attribute",
			TextEdit: &lsp.TextEdit{Range: r, NewText: name},
			SortText: "1_" + leftPadInt(i, 2) + "_" + name,
		})
	}
	return items
}

func aspDirectiveOpenCompletions(doc *core.TextDocument, position lsp.Position, offset int) []lsp.CompletionItem {
	before := doc.Text[:offset]
	start := strings.LastIndex(before, "<%")
	if start < 0 {
		return nil
	}
	lineStart := 0
	if newline := strings.LastIndexAny(before, "\n\r"); newline >= 0 {
		lineStart = newline + 1
	}
	if start < lineStart {
		return nil
	}
	prefix := doc.Text[start+2 : offset]
	prefix = strings.TrimPrefix(prefix, "@")
	if !isASCIIAlphaString(prefix) {
		return nil
	}
	r := doc.Range(start, offset)
	newText := `<%@ Language="${1:VBScript}" CodePage=${2:65001} %>`
	return []lsp.CompletionItem{{
		Label:            `<%@ Language="VBScript" CodePage=65001 %>`,
		Kind:             lsp.CompletionItemKindSnippet,
		Detail:           "Classic ASP page directive",
		Documentation:    "Inserts a Classic ASP page directive with language and code page.",
		InsertText:       newText,
		TextEdit:         &lsp.TextEdit{Range: r, NewText: newText},
		InsertTextFormat: insertTextFormatSnippet,
		FilterText:       "asp directive language codepage page <%@",
		SortText:         "0_asp_directive_page",
	}}
}

func aspDirectiveValueCompletions(doc *core.TextDocument, position lsp.Position, context *aspDirectiveValueCompletionContext) []lsp.CompletionItem {
	values := aspDirectiveValues(context.attribute)
	r := lsp.Range{Start: doc.PositionAt(context.replaceStart), End: position}
	items := make([]lsp.CompletionItem, 0, len(values))
	for i, value := range values {
		items = append(items, lsp.CompletionItem{
			Label:    value,
			Kind:     lsp.CompletionItemKindValue,
			Detail:   context.attribute + " value",
			TextEdit: &lsp.TextEdit{Range: r, NewText: value},
			SortText: "0_" + leftPadInt(i, 2) + "_" + value,
		})
	}
	return items
}

func aspDirectiveValueContextAt(text string, region core.Region, offset int) *aspDirectiveValueCompletionContext {
	before := text[region.ContentStart:offset]
	match := aspDirectiveValuePattern.FindStringSubmatchIndex(before)
	if match == nil {
		return nil
	}
	attribute := aspDirectiveAttributeName(before[match[2]:match[3]])
	if attribute == "" {
		return nil
	}
	equals := strings.LastIndex(before, "=")
	replaceStart := region.ContentStart + equals + 1
	for replaceStart < offset && (text[replaceStart] == ' ' || text[replaceStart] == '\t' || text[replaceStart] == '\r' || text[replaceStart] == '\n') {
		replaceStart++
	}
	if replaceStart < len(text) && (text[replaceStart] == '"' || text[replaceStart] == '\'') {
		replaceStart++
	}
	return &aspDirectiveValueCompletionContext{attribute: attribute, replaceStart: replaceStart}
}

func aspDirectiveAttributeName(value string) string {
	for _, name := range []string{"Language", "CodePage", "LCID", "Transaction", "EnableSessionState"} {
		if strings.EqualFold(value, name) {
			return name
		}
	}
	return ""
}

func aspDirectiveValues(attribute string) []string {
	switch strings.ToLower(attribute) {
	case "language":
		return []string{"VBScript", "JScript", "JavaScript"}
	case "codepage":
		return []string{"65001", "932", "1252"}
	case "lcid":
		return []string{"1041", "1033"}
	case "transaction":
		return []string{"Required", "Requires_New", "Supported", "Not_Supported"}
	case "enablesessionstate":
		return []string{"True", "False"}
	default:
		return nil
	}
}

func aspDirectiveWordRange(doc *core.TextDocument, offset int) lsp.Range {
	start := offset
	for start > 0 && isCompletionIdentifier(doc.Text[start-1]) {
		start--
	}
	return doc.Range(start, offset)
}

func isASCIIAlphaString(value string) bool {
	for i := 0; i < len(value); i++ {
		if (value[i] < 'A' || value[i] > 'Z') && (value[i] < 'a' || value[i] > 'z') {
			return false
		}
	}
	return true
}

func isCompletionIdentifier(b byte) bool {
	return b == '_' || b >= '0' && b <= '9' || b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z'
}

func leftPadInt(value, width int) string {
	text := strconv.Itoa(value)
	for len(text) < width {
		text = "0" + text
	}
	return text
}

func (s *Server) vbscriptSymbolCompletions(parsed *core.ParsedDocument, offset int) []lsp.CompletionItem {
	return s.vbscriptSymbolCompletionsContext(context.Background(), parsed, offset)
}

func (s *Server) vbscriptSymbolCompletionsContext(ctx context.Context, parsed *core.ParsedDocument, offset int) []lsp.CompletionItem {
	if ctx == nil {
		ctx = context.Background()
	}
	if parsed == nil || ctx.Err() != nil {
		return nil
	}
	docs, complete := s.vbscriptDocumentsThroughExecutionOffsetContext(ctx, parsed, offset)
	if ctx.Err() != nil || !complete {
		return nil
	}
	items := []lsp.CompletionItem{}
	for docIndex, doc := range docs {
		if ctx.Err() != nil {
			return nil
		}
		index := vbscript.BuildSymbolIndex(doc)
		signatureLookup := vbscriptCompletionSignatureLookup(doc)
		documentOffset := -1
		if docIndex == 0 || workspacepkg.SameFileIdentityURI(doc.URI, parsed.URI) {
			documentOffset = offset
		}
		visibleSymbols := vbscriptCompletionVisibleSymbols(doc, documentOffset)
		completionSource := core.SourceDocument(doc)
		completionPosition := completionSource.PositionAt(documentOffset)
		if documentOffset < 0 {
			completionPosition = completionSource.PositionAt(len(doc.Text))
		}
		for _, symbol := range index.Declarations {
			if ctx.Err() != nil {
				return nil
			}
			if visible, ok := visibleSymbols[strings.ToLower(symbol.Name)]; ok && !visible {
				continue
			}
			item := lsp.CompletionItem{
				Label:  symbol.Name,
				Kind:   vbscriptCompletionKind(symbol.Kind),
				Detail: "VBScript",
				Data:   vbscriptCompletionData{Kind: "vbscript-symbol", URI: doc.URI, Position: &completionPosition},
			}
			if signature, ok := vbscriptCompletionSignatureForSymbol(signatureLookup, symbol); ok {
				if documentation := vbscriptXMLDocForSignature(doc, signature); documentation.hasContent() {
					item.Documentation = documentation.markdown(signature, s.settings.Locale)
				}
			}
			items = append(items, item)
		}
		for _, object := range serverObjectSymbols(doc) {
			if ctx.Err() != nil {
				return nil
			}
			if documentOffset >= 0 && object.Declaration.Start > documentOffset {
				continue
			}
			declaration := object.Declaration
			if documentOffset >= 0 && vbLocalDeclarationShadowsNameAt(doc, declaration.Name, core.SourceDocument(doc).PositionAt(documentOffset)) {
				continue
			}
			items = append(items, lsp.CompletionItem{
				Label:         declaration.Name,
				Kind:          lsp.CompletionItemKindVariable,
				Detail:        "Server OBJECT · " + declaration.TypeName,
				Documentation: lsp.MarkupContent{Kind: "markdown", Value: "Global server OBJECT declared by the `id` or `name` attribute."},
				Data:          vbscriptCompletionData{Kind: "vbscript-symbol", URI: doc.URI},
			})
		}
		if docIndex > 0 {
			for _, declaration := range variableInlayDeclarations(doc, true, nil) {
				if ctx.Err() != nil {
					return nil
				}
				if declaration.Local {
					continue
				}
				lower := strings.ToLower(declaration.Name)
				if _, ok := index.Declarations[lower]; ok {
					continue
				}
				items = append(items, lsp.CompletionItem{
					Label:  declaration.Name,
					Kind:   lsp.CompletionItemKindVariable,
					Detail: "Implicit global variable",
					Data:   vbscriptCompletionData{Kind: "vbscript-symbol", URI: doc.URI},
				})
			}
		}
		if !s.settings.ShowUnresolvedSymbolsInCompletion {
			continue
		}
		source := core.SourceDocument(doc)
		for lower, occurrences := range index.Occurrences {
			if ctx.Err() != nil {
				return nil
			}
			if _, ok := index.Declarations[lower]; ok {
				continue
			}
			for _, occurrence := range occurrences {
				start := source.OffsetAt(occurrence.Range.Start)
				end := source.OffsetAt(occurrence.Range.End)
				if documentOffset >= 0 && start > documentOffset {
					continue
				}
				if documentOffset >= 0 && documentOffset >= start && documentOffset <= end {
					continue
				}
				kind, detail := unresolvedCompletionKind(doc.Text, start, end)
				items = append(items, lsp.CompletionItem{
					Label:  occurrence.Name,
					Kind:   kind,
					Detail: detail,
				})
				break
			}
		}
	}
	if ctx.Err() != nil {
		return nil
	}
	return items
}

type vbscriptCompletionSignatureKey struct {
	name      string
	nameRange lsp.Range
}

func vbscriptCompletionSignatureLookup(parsed *core.ParsedDocument) map[vbscriptCompletionSignatureKey]vbscript.Signature {
	if parsed == nil {
		return nil
	}
	signatures := graphSignatures(parsed)
	lookup := make(map[vbscriptCompletionSignatureKey]vbscript.Signature, len(signatures))
	for _, signature := range signatures {
		key := vbscriptCompletionSignatureKey{name: strings.ToLower(signature.Name), nameRange: signature.NameRange}
		if _, exists := lookup[key]; !exists {
			lookup[key] = signature
		}
	}
	return lookup
}

func vbscriptCompletionSignatureForSymbol(lookup map[vbscriptCompletionSignatureKey]vbscript.Signature, symbol vbscript.Symbol) (vbscript.Signature, bool) {
	if lookup == nil {
		return vbscript.Signature{}, false
	}
	signature, ok := lookup[vbscriptCompletionSignatureKey{name: strings.ToLower(symbol.Name), nameRange: symbol.Range}]
	return signature, ok
}

func vbscriptCompletionVisibleSymbols(parsed *core.ParsedDocument, offset int) map[string]bool {
	usage := vbUsageDeclarations{Declarations: normalizedVBUsageDeclarations(parsed)}
	currentScope := ""
	if offset >= 0 {
		currentScope = vbProcedureScopeAtOffset(vbProcedureScopes(parsed), offset)
	}
	localNames := map[string]struct{}{}
	otherLocalNames := map[string]struct{}{}
	localDeclarationRanges := map[offsetRange]struct{}{}
	for _, declaration := range usage.Declarations {
		if !declaration.Local {
			continue
		}
		localDeclarationRanges[offsetRangeKey(declaration.Start, declaration.End)] = struct{}{}
		if offset >= 0 && declaration.Start > offset {
			continue
		}
		lower := strings.ToLower(declaration.Name)
		if offset >= 0 && currentScope != "" && strings.EqualFold(declaration.Scope, currentScope) {
			localNames[lower] = struct{}{}
		} else {
			otherLocalNames[lower] = struct{}{}
		}
	}
	visible := map[string]bool{}
	for _, declaration := range usage.Declarations {
		lower := strings.ToLower(declaration.Name)
		if !declaration.Local {
			if _, localDuplicate := localDeclarationRanges[offsetRangeKey(declaration.Start, declaration.End)]; localDuplicate {
				continue
			}
		}
		if declaration.Local {
			if offset < 0 || currentScope == "" || !strings.EqualFold(declaration.Scope, currentScope) {
				if offset >= 0 && declaration.Start > offset {
					if _, ok := visible[lower]; !ok {
						visible[lower] = false
					}
				}
				continue
			}
			if declaration.Start > offset {
				if _, ok := visible[lower]; !ok {
					visible[lower] = false
				}
				continue
			}
			visible[lower] = true
			continue
		}
		if _, local := localNames[lower]; local {
			continue
		}
		if offset >= 0 && declaration.Start > offset && !vbscriptForwardCallableDeclaration(declaration) {
			if _, ok := visible[lower]; !ok {
				visible[lower] = false
			}
			continue
		}
		visible[lower] = true
	}
	for lower := range otherLocalNames {
		if _, ok := visible[lower]; !ok {
			visible[lower] = false
		}
	}
	return visible
}

func vbscriptForwardCallableDeclaration(declaration vbUsageDeclaration) bool {
	if declaration.Local || declaration.MemberOf != "" {
		return false
	}
	switch strings.ToLower(declaration.Kind) {
	case "function", "sub":
		return true
	default:
		return false
	}
}

func unresolvedCompletionKind(text string, start, end int) (lsp.CompletionItemKind, string) {
	if isCallTarget(text, start) || isFunctionLikeUsage(text, end) {
		return lsp.CompletionItemKindFunction, "Unresolved Function/Sub"
	}
	return lsp.CompletionItemKindVariable, "Implicit global variable"
}

func isCallTarget(text string, start int) bool {
	cursor := start
	for cursor > 0 && (text[cursor-1] == ' ' || text[cursor-1] == '\t') {
		cursor--
	}
	end := cursor
	for cursor > 0 && isCompletionIdentifier(text[cursor-1]) {
		cursor--
	}
	return strings.EqualFold(text[cursor:end], "Call")
}

func isFunctionLikeUsage(text string, end int) bool {
	cursor := end
	for cursor < len(text) && (text[cursor] == ' ' || text[cursor] == '\t') {
		cursor++
	}
	return cursor < len(text) && text[cursor] == '('
}

func (s *Server) includedVBScriptDefinitionContext(ctx context.Context, parsed *core.ParsedDocument, position lsp.Position) []lsp.Location {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil || parsed == nil {
		return nil
	}
	source := core.SourceDocument(parsed)
	word := vbscript.WordAt(parsed.Text, source.OffsetAt(position))
	if word == "" {
		return nil
	}
	lower := strings.ToLower(word)
	offset := source.OffsetAt(position)
	documents, complete := s.vbscriptDocumentsThroughExecutionOffsetContext(ctx, parsed, offset)
	if ctx.Err() != nil || !complete {
		return nil
	}
	for _, included := range documents {
		if ctx.Err() != nil {
			return nil
		}
		if included == nil || workspacepkg.SameFileIdentityURI(included.URI, parsed.URI) {
			continue
		}
		for _, declaration := range normalizedVBUsageDeclarations(included) {
			if !strings.EqualFold(declaration.Name, word) || declaration.Local || declaration.Scope != "" || declaration.MemberOf != "" {
				continue
			}
			return []lsp.Location{{URI: included.URI, Range: declaration.Range}}
		}
	}
	// The prefix traversal already returns the root first, followed by the
	// include documents reached before the request offset.
	documentByURI := map[string]*core.ParsedDocument{}
	for _, document := range documents {
		if ctx.Err() != nil {
			return nil
		}
		if document != nil {
			documentByURI[document.URI] = document
		}
	}
	canonicalImplicitIDs := s.graphCanonicalImplicitDeclarationIDs(documents, documentByURI)
	if canonicalID := canonicalImplicitIDs[lower]; canonicalID != "" {
		for _, document := range documents {
			if ctx.Err() != nil {
				return nil
			}
			if document == nil {
				continue
			}
			for _, declaration := range graphVBDeclarations(document) {
				if !declaration.Implicit || !strings.EqualFold(declaration.Name, word) {
					continue
				}
				if workspacepkg.SameFileIdentityURI(document.URI, parsed.URI) && declaration.Start > offset {
					continue
				}
				if graphDeclarationNodeID(document.URI, declaration.Name, declaration.Range) == canonicalID {
					return []lsp.Location{{URI: document.URI, Range: declaration.Range}}
				}
			}
		}
	}
	return nil
}

// vbscriptIncludeExecutionUnit is one source span executed in textual order.
// Include directives split a parent into separate units so assignments before
// and after a nested include retain their runtime precedence.
type vbscriptIncludeExecutionUnit struct {
	document *core.ParsedDocument
	start    int
	end      int
}

// includeExpansionUnitBudget bounds textual include expansion. A repeated
// include is still expanded in source order until this finite budget is
// reached; after that point the traversal is marked incomplete and callers
// must discard the materialized prefix. This keeps ordinary repeated includes
// exact without allowing a shared fanout graph to grow exponentially.
const includeExpansionUnitBudget = 4096

func (s *Server) includedDocuments(parsed *core.ParsedDocument) []*core.ParsedDocument {
	documents, _ := s.includedDocumentsContextResult(context.Background(), parsed)
	return documents
}

func (s *Server) includedDocumentsContext(ctx context.Context, parsed *core.ParsedDocument) []*core.ParsedDocument {
	documents, _ := s.includedDocumentsContextResult(ctx, parsed)
	return documents
}

// includedDocumentsContextResult returns the transitive include documents and
// whether expansion completed without cancellation or budget truncation.
func (s *Server) includedDocumentsContextResult(ctx context.Context, parsed *core.ParsedDocument) ([]*core.ParsedDocument, bool) {
	if parsed == nil {
		return nil, true
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil, false
	}
	if hook := s.includeExpansionTestHook; hook != nil {
		hook()
	}
	seen := map[string]struct{}{workspacepkg.FileIdentityKeyFromURI(parsed.URI): {}}
	documents := make([]*core.ParsedDocument, 0)
	units, complete := s.vbscriptIncludeExecutionUnitsContext(ctx, parsed)
	if ctx.Err() != nil || !complete {
		return nil, false
	}
	for _, unit := range units {
		if unit.document == nil {
			continue
		}
		key := workspacepkg.FileIdentityKeyFromURI(unit.document.URI)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		documents = append(documents, unit.document)
	}
	if ctx.Err() != nil {
		return nil, false
	}
	return documents, true
}

func (s *Server) vbscriptIncludeExecutionUnitsContext(ctx context.Context, root *core.ParsedDocument) ([]vbscriptIncludeExecutionUnit, bool) {
	if root == nil {
		return nil, true
	}
	if ctx == nil {
		ctx = context.Background()
	}
	units := make([]vbscriptIncludeExecutionUnit, 0)
	active := map[string]struct{}{}
	documents := map[string]*core.ParsedDocument{workspacepkg.FileIdentityKeyFromURI(root.URI): root}
	includeDocuments := map[string]*core.ParsedDocument{}
	includeDocumentsResolved := map[string]struct{}{}
	complete := true
	resolveInclude := func(parent *core.ParsedDocument, include core.Include) *core.ParsedDocument {
		if parent == nil || ctx.Err() != nil {
			return nil
		}
		includeKey := workspacepkg.FileIdentityKeyFromURI(parent.URI) + "\x00" + include.Mode + "\x00" + include.Path
		if _, resolved := includeDocumentsResolved[includeKey]; resolved {
			return includeDocuments[includeKey]
		}
		includeDocumentsResolved[includeKey] = struct{}{}
		details, resolved := s.includeTargetDetailsForModeContext(ctx, parent.URI, include.Path, include.Mode)
		if resolved && details.Path != "" {
			key := workspacepkg.FileIdentityKeyFromURI(filePathURI(details.Path))
			if document := documents[key]; document != nil {
				includeDocuments[includeKey] = document
				return document
			}
		}
		var included *core.ParsedDocument
		if resolved && details.Path != "" {
			included = s.vbscriptIncludedDocumentAtPathContext(ctx, parent, details.Path)
		}
		if included != nil {
			documents[workspacepkg.FileIdentityKeyFromURI(included.URI)] = included
			includeDocuments[includeKey] = included
		}
		return included
	}
	appendUnit := func(unit vbscriptIncludeExecutionUnit) bool {
		if !complete || ctx.Err() != nil {
			complete = false
			return false
		}
		if len(units) >= includeExpansionUnitBudget {
			complete = false
			return false
		}
		units = append(units, unit)
		return true
	}
	var appendDocument func(*core.ParsedDocument)
	appendDocument = func(parsed *core.ParsedDocument) {
		if ctx.Err() != nil {
			complete = false
			return
		}
		if parsed == nil {
			return
		}
		key := workspacepkg.FileIdentityKeyFromURI(parsed.URI)
		if _, exists := active[key]; exists {
			return
		}
		active[key] = struct{}{}
		defer delete(active, key)

		textDocument := core.SourceDocument(parsed)
		unitStart := len(units)
		includes := append([]core.Include(nil), parsed.Includes...)
		sort.SliceStable(includes, func(left, right int) bool {
			leftStart := textDocument.OffsetAt(includes[left].Range.Start)
			rightStart := textDocument.OffsetAt(includes[right].Range.Start)
			if leftStart != rightStart {
				return leftStart < rightStart
			}
			leftEnd := textDocument.OffsetAt(includes[left].Range.End)
			rightEnd := textDocument.OffsetAt(includes[right].Range.End)
			return leftEnd < rightEnd
		})
		cursor := 0
		for _, include := range includes {
			if ctx.Err() != nil {
				complete = false
				return
			}
			start := textDocument.OffsetAt(include.Range.Start)
			if start < cursor {
				start = cursor
			}
			if start > len(parsed.Text) {
				start = len(parsed.Text)
			}
			if cursor < start {
				if !appendUnit(vbscriptIncludeExecutionUnit{document: parsed, start: cursor, end: start}) {
					return
				}
			}
			if included := resolveInclude(parsed, include); included != nil && ctx.Err() == nil {
				appendDocument(included)
				if !complete {
					return
				}
			}
			cursor = start
		}
		if cursor < len(parsed.Text) {
			if !appendUnit(vbscriptIncludeExecutionUnit{document: parsed, start: cursor, end: len(parsed.Text)}) {
				return
			}
		}
		if len(units) == unitStart && len(units) < includeExpansionUnitBudget {
			appendUnit(vbscriptIncludeExecutionUnit{document: parsed, start: cursor, end: cursor})
		}
	}
	appendDocument(root)
	if ctx.Err() != nil || !complete {
		return nil, false
	}
	return units, true
}

// vbscriptIncludeExecutionUnitsThroughOffsetContextResult expands only the
// include occurrences that execute before the root request offset. Included
// documents reached by that prefix execute in full, while later root
// occurrences are not resolved at all. Cancellation remains atomic, while a
// budget hit returns the materialized prefix with complete=false so callers
// that only need an already-visible declaration can preserve its precedence.
func (s *Server) vbscriptIncludeExecutionUnitsThroughOffsetContextResult(ctx context.Context, root *core.ParsedDocument, offset int) ([]vbscriptIncludeExecutionUnit, bool) {
	if root == nil {
		return nil, true
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil, false
	}
	if hook := s.includeExpansionTestHook; hook != nil {
		hook()
	}
	if offset < 0 {
		offset = 0
	}
	if offset > len(root.Text) {
		offset = len(root.Text)
	}

	units := make([]vbscriptIncludeExecutionUnit, 0)
	active := map[string]struct{}{}
	documents := map[string]*core.ParsedDocument{workspacepkg.FileIdentityKeyFromURI(root.URI): root}
	includeDocuments := map[string]*core.ParsedDocument{}
	includeDocumentsResolved := map[string]struct{}{}
	complete := true
	resolveInclude := func(parent *core.ParsedDocument, include core.Include) *core.ParsedDocument {
		if parent == nil || ctx.Err() != nil {
			return nil
		}
		includeKey := workspacepkg.FileIdentityKeyFromURI(parent.URI) + "\x00" + include.Mode + "\x00" + include.Path
		if _, resolved := includeDocumentsResolved[includeKey]; resolved {
			return includeDocuments[includeKey]
		}
		includeDocumentsResolved[includeKey] = struct{}{}
		details, resolved := s.includeTargetDetailsForModeContext(ctx, parent.URI, include.Path, include.Mode)
		if resolved && details.Path != "" {
			key := workspacepkg.FileIdentityKeyFromURI(filePathURI(details.Path))
			if document := documents[key]; document != nil {
				includeDocuments[includeKey] = document
				return document
			}
		}
		var included *core.ParsedDocument
		if resolved && details.Path != "" {
			included = s.vbscriptIncludedDocumentAtPathContext(ctx, parent, details.Path)
		}
		if included != nil {
			documents[workspacepkg.FileIdentityKeyFromURI(included.URI)] = included
			includeDocuments[includeKey] = included
		}
		return included
	}
	appendUnit := func(unit vbscriptIncludeExecutionUnit) bool {
		if !complete || ctx.Err() != nil {
			complete = false
			return false
		}
		if len(units) >= includeExpansionUnitBudget {
			complete = false
			return false
		}
		units = append(units, unit)
		return true
	}
	var appendDocument func(*core.ParsedDocument, bool)
	appendDocument = func(parsed *core.ParsedDocument, rootDocument bool) {
		if !complete || ctx.Err() != nil {
			complete = false
			return
		}
		if parsed == nil {
			return
		}
		key := workspacepkg.FileIdentityKeyFromURI(parsed.URI)
		if _, exists := active[key]; exists {
			return
		}
		active[key] = struct{}{}
		defer delete(active, key)

		textDocument := core.SourceDocument(parsed)
		unitStart := len(units)
		limit := len(parsed.Text)
		if rootDocument {
			limit = offset
		}
		includes := append([]core.Include(nil), parsed.Includes...)
		sort.SliceStable(includes, func(left, right int) bool {
			leftStart := textDocument.OffsetAt(includes[left].Range.Start)
			rightStart := textDocument.OffsetAt(includes[right].Range.Start)
			if leftStart != rightStart {
				return leftStart < rightStart
			}
			leftEnd := textDocument.OffsetAt(includes[left].Range.End)
			rightEnd := textDocument.OffsetAt(includes[right].Range.End)
			return leftEnd < rightEnd
		})
		cursor := 0
		for _, include := range includes {
			if ctx.Err() != nil {
				complete = false
				return
			}
			start := textDocument.OffsetAt(include.Range.Start)
			if start >= limit {
				break
			}
			if start < cursor {
				start = cursor
			}
			if start > limit {
				start = limit
			}
			if cursor < start {
				if !appendUnit(vbscriptIncludeExecutionUnit{document: parsed, start: cursor, end: start}) {
					return
				}
			}
			if included := resolveInclude(parsed, include); included != nil && ctx.Err() == nil {
				appendDocument(included, false)
				if !complete {
					return
				}
			}
			cursor = start
		}
		if cursor < limit {
			if !appendUnit(vbscriptIncludeExecutionUnit{document: parsed, start: cursor, end: limit}) {
				return
			}
		}
		if len(units) == unitStart && len(units) < includeExpansionUnitBudget {
			appendUnit(vbscriptIncludeExecutionUnit{document: parsed, start: cursor, end: cursor})
		}
	}
	appendDocument(root, true)
	if ctx.Err() != nil {
		return nil, false
	}
	if !complete {
		return units, false
	}
	return units, true
}

func (s *Server) vbscriptDocumentsThroughExecutionOffsetContext(ctx context.Context, root *core.ParsedDocument, offset int) ([]*core.ParsedDocument, bool) {
	if root == nil {
		return nil, false
	}
	units, complete := s.vbscriptIncludeExecutionUnitsThroughOffsetContextResult(ctx, root, offset)
	if !complete || ctx != nil && ctx.Err() != nil {
		return nil, false
	}
	documents := []*core.ParsedDocument{root}
	seen := map[string]struct{}{workspacepkg.FileIdentityKeyFromURI(root.URI): {}}
	for _, unit := range units {
		if ctx != nil && ctx.Err() != nil {
			return nil, false
		}
		if unit.document == nil {
			continue
		}
		key := workspacepkg.FileIdentityKeyFromURI(unit.document.URI)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		documents = append(documents, unit.document)
	}
	return documents, true
}

func (s *Server) vbscriptIncludedDocumentContext(ctx context.Context, parent *core.ParsedDocument, include core.Include) *core.ParsedDocument {
	if parent == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil
	}
	targetPath, ok := s.includeTargetPathForModeContext(ctx, parent.URI, include.Path, include.Mode)
	if !ok || targetPath == "" {
		return nil
	}
	return s.vbscriptIncludedDocumentAtPathContext(ctx, parent, targetPath)
}

func (s *Server) vbscriptIncludedDocumentAtPathContext(ctx context.Context, parent *core.ParsedDocument, targetPath string) *core.ParsedDocument {
	if parent == nil || targetPath == "" {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil
	}
	uri := filePathURI(targetPath)
	if doc := s.documentByURI(uri); doc != nil {
		return s.parseTextDocument(doc, s.settings.DefaultLanguage)
	}
	ownerPath := filepath.Clean(fileURIPath(parent.URI))
	content, err := s.readWorkspaceTextFileWithinBoundaries(ctx, targetPath, filepath.Dir(ownerPath))
	if err != nil {
		return nil
	}
	if ctx.Err() != nil {
		return nil
	}
	return s.parseText(uri, content, s.settings.DefaultLanguage)
}
