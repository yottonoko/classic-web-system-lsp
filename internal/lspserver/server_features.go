package lspserver

import (
	"context"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/javascript"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

func (s *Server) codeActions(ctx context.Context, params codeActionParams) []lsp.CodeAction {
	doc, parsed := s.parsed(params.TextDocument.URI)
	if doc == nil {
		return nil
	}
	start := doc.OffsetAt(params.Range.Start)
	end := doc.OffsetAt(params.Range.End)
	if start < 0 || end > len(doc.Text) || start > end {
		return nil
	}
	var actions []lsp.CodeAction
	if codeActionAllows(params.Context.Only, "quickfix") {
		s.cssMu.Lock()
		actions = append(actions, s.css.CodeActions(parsed, params.Range, params.Context.Diagnostics, params.Context.Only)...)
		s.cssMu.Unlock()
	}
	if isJavaScriptPosition(doc, parsed, params.Range.Start) {
		var javaScriptActions []lsp.CodeAction
		if s.javaScriptLanguageServiceRequest(ctx, params.TextDocument.URI, params.Range.Start, "textDocument/codeAction", map[string]any{
			"range":   params.Range,
			"context": params.Context,
		}, &javaScriptActions) {
			actions = append(actions, javaScriptActions...)
		}
	}
	for _, diagnostic := range params.Context.Diagnostics {
		if s.settings.JavaScriptAutoImports && codeActionAllows(params.Context.Only, "quickfix") && diagnostic.Source == "asp-lsp-typescript" {
			if name, ok := missingJavaScriptName(diagnostic.Message); ok {
				if action, ok := s.javascriptAutoImportCodeAction(ctx, params.TextDocument.URI, name, diagnostic); ok {
					actions = append(actions, action)
				}
			}
		}
		if diagnostic.Source == "asp-lsp-include" {
			if includePath, targetURI, ok := s.missingIncludeAt(params.TextDocument.URI, diagnostic.Range); ok {
				diag := diagnostic
				actions = append(actions, lsp.CodeAction{
					Title:       s.createMissingIncludeTitle(includePath),
					Kind:        "quickfix",
					Diagnostics: []lsp.Diagnostic{diag},
					Edit: &lsp.WorkspaceEdit{DocumentChanges: []any{
						map[string]any{"kind": "create", "uri": targetURI, "options": map[string]bool{"ignoreIfExists": true}},
					}},
				})
			}
		}
	}
	if codeActionAllows(params.Context.Only, "quickfix") {
		actions = append(actions, s.vbscriptAutoIncludeCodeActions(params)...)
		actions = append(actions, s.dimDeclarationCodeActions(params, start, end)...)
		actions = append(actions, s.vbscriptUsageCodeActions(params)...)
		actions = append(actions, s.vbscriptNamingCodeActions(params)...)
		actions = append(actions, s.vbscriptTypeCodeActions(params)...)
		actions = append(actions, s.documentationCodeActions(params)...)
		actions = append(actions, s.callSyntaxCodeActions(params)...)
	}
	if codeActionAllows(params.Context.Only, "refactor.extract") {
		actions = append(actions, s.inlineStyleExtractionCodeActions(params, start)...)
		actions = append(actions, s.extractVariableCodeActions(params, start, end)...)
	}
	if codeActionAllows(params.Context.Only, "source.organizeImports") {
		if action, ok := s.organizeJavaScriptImportsAction(params.TextDocument.URI); ok {
			actions = append(actions, action)
		}
	}
	if actions == nil {
		return []lsp.CodeAction{}
	}
	return actions
}

func (s *Server) missingIncludeAt(uri string, r lsp.Range) (string, string, bool) {
	_, parsed := s.parsed(uri)
	if parsed == nil {
		return "", "", false
	}
	for _, include := range parsed.Includes {
		if include.Range == r {
			targetPath, ok := s.includeTargetPathForMode(parsed.URI, include.Path, include.Mode)
			if !ok {
				return "", "", false
			}
			return include.Path, filePathURI(targetPath), true
		}
	}
	return "", "", false
}

// prepareCallHierarchy keeps the legacy context-free helper used by internal
// callers while request dispatch uses the context-aware variant below.
func (s *Server) prepareCallHierarchy(uri string, position lsp.Position) []lsp.CallHierarchyItem {
	return s.prepareCallHierarchyContext(context.Background(), uri, position)
}

func (s *Server) prepareCallHierarchyContext(ctx context.Context, uri string, position lsp.Position) []lsp.CallHierarchyItem {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil
	}
	doc, parsed := s.parsed(uri)
	if doc == nil || parsed == nil || ctx.Err() != nil {
		return nil
	}
	if doc != nil && isJavaScriptPosition(doc, parsed, position) {
		var items []lsp.CallHierarchyItem
		if !s.javaScriptLanguageServiceRequest(ctx, uri, position, "textDocument/prepareCallHierarchy", nil, &items) || ctx.Err() != nil {
			return nil
		}
		for index := range items {
			if ctx.Err() != nil {
				return nil
			}
			if data, ok := items[index].Data.(map[string]any); ok {
				data["language"] = "javascript"
				items[index].Data = data
			} else {
				items[index].Data = map[string]any{"language": "javascript", "typescript": items[index].Data}
			}
		}
		if ctx.Err() != nil {
			return nil
		}
		return items
	}
	signature, ok := vbscript.SignatureAt(parsed, position)
	if ctx.Err() != nil {
		return nil
	}
	if !ok {
		if property, propertyOK := vbPropertyAccessorSignatureAt(parsed, position); propertyOK {
			if ctx.Err() != nil {
				return nil
			}
			return []lsp.CallHierarchyItem{callHierarchyItem(parsed.URI, property)}
		}
		if vbProcedureScopeAtOffset(vbProcedureScopes(parsed), doc.OffsetAt(position)) == "" {
			return nil
		}
	}
	if !ok {
		signature, ok = vbscript.EnclosingSignature(parsed, position)
		if ctx.Err() != nil {
			return nil
		}
	}
	if !ok {
		return nil
	}
	if ctx.Err() != nil {
		return nil
	}
	return []lsp.CallHierarchyItem{callHierarchyItem(parsed.URI, signature)}
}

func (s *Server) incomingCalls(item lsp.CallHierarchyItem) []lsp.CallHierarchyIncomingCall {
	return s.incomingCallsContext(context.Background(), item)
}

func (s *Server) incomingCallsContext(ctx context.Context, item lsp.CallHierarchyItem) []lsp.CallHierarchyIncomingCall {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil
	}
	_, parsed := s.parsed(item.URI)
	if parsed == nil || ctx.Err() != nil {
		return nil
	}
	if isJavaScriptCallHierarchyItem(item) {
		var calls []lsp.CallHierarchyIncomingCall
		if !s.javaScriptCallHierarchyRequest(ctx, item, "callHierarchy/incomingCalls", &calls) || ctx.Err() != nil {
			return nil
		}
		return calls
	}
	calls := vbscriptIncomingCallsFromShardContext(ctx, parsed, item)
	if ctx.Err() != nil {
		return nil
	}
	return calls
}

//lint:ignore U1000 retained for callers that use the legacy context-free helper.
func (s *Server) outgoingCalls(item lsp.CallHierarchyItem) []lsp.CallHierarchyOutgoingCall {
	return s.outgoingCallsContext(context.Background(), item)
}

func (s *Server) outgoingCallsContext(ctx context.Context, item lsp.CallHierarchyItem) []lsp.CallHierarchyOutgoingCall {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil
	}
	_, parsed := s.parsed(item.URI)
	if parsed == nil || ctx.Err() != nil {
		return nil
	}
	if isJavaScriptCallHierarchyItem(item) {
		var calls []lsp.CallHierarchyOutgoingCall
		if !s.javaScriptCallHierarchyRequest(ctx, item, "callHierarchy/outgoingCalls", &calls) || ctx.Err() != nil {
			return nil
		}
		return calls
	}
	calls := vbscriptOutgoingCallsFromShardContext(ctx, parsed, item)
	if ctx.Err() != nil {
		return nil
	}
	return calls
}

func isJavaScriptCallHierarchyItem(item lsp.CallHierarchyItem) bool {
	data, ok := item.Data.(map[string]any)
	if !ok {
		return false
	}
	language, ok := data["language"].(string)
	return ok && language == "javascript"
}

//lint:ignore U1000 retained for callers that use the legacy context-free helper.
func (s *Server) prepareTypeHierarchy(uri string, position lsp.Position) []lsp.TypeHierarchyItem {
	return s.prepareTypeHierarchyContext(context.Background(), uri, position)
}

func (s *Server) prepareTypeHierarchyContext(ctx context.Context, uri string, position lsp.Position) []lsp.TypeHierarchyItem {
	if ctx == nil || ctx.Err() != nil {
		return nil
	}
	doc, parsed := s.parsed(uri)
	if parsed == nil || ctx.Err() != nil {
		return nil
	}
	if item, ok := s.vbscriptConfiguredTypeHierarchyItemContext(ctx, doc, parsed, position); ok {
		if ctx.Err() != nil {
			return nil
		}
		return []lsp.TypeHierarchyItem{item}
	}
	if ctx.Err() != nil {
		return nil
	}
	items := vbscript.TypeHierarchyItems(parsed, position)
	if ctx.Err() != nil {
		return nil
	}
	if items == nil {
		return []lsp.TypeHierarchyItem{}
	}
	return items
}

//lint:ignore U1000 retained for callers that use the legacy context-free helper.
func (s *Server) typeHierarchyRelations(item lsp.TypeHierarchyItem) []lsp.TypeHierarchyItem {
	return s.typeHierarchyRelationsContext(context.Background(), item)
}

func (s *Server) typeHierarchyRelationsContext(ctx context.Context, item lsp.TypeHierarchyItem) []lsp.TypeHierarchyItem {
	if ctx == nil || ctx.Err() != nil {
		return nil
	}
	if items := s.vbscriptConfiguredTypeHierarchyRelationsContext(ctx, item); len(items) > 0 {
		if ctx.Err() != nil {
			return nil
		}
		return items
	}
	if ctx.Err() != nil {
		return nil
	}
	return []lsp.TypeHierarchyItem{}
}

//lint:ignore U1000 retained for callers that use the legacy context-free helper.
func (s *Server) monikers(uri string, position lsp.Position) []lsp.Moniker {
	return s.monikersContext(context.Background(), uri, position)
}

func (s *Server) monikersContext(ctx context.Context, uri string, position lsp.Position) []lsp.Moniker {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil
	}
	doc, parsed := s.parsed(uri)
	if doc == nil || parsed == nil || ctx.Err() != nil {
		return []lsp.Moniker{}
	}
	if isJavaScriptPosition(doc, parsed, position) {
		monikers := s.javaScriptMonikers(ctx, uri, position)
		if ctx.Err() != nil {
			return nil
		}
		return monikers
	}
	locations := s.definitionContext(ctx, uri, position)
	if ctx.Err() != nil {
		return nil
	}
	if len(locations) == 0 {
		locations = s.typeDefinitionContext(ctx, uri, position)
		if ctx.Err() != nil {
			return nil
		}
	}
	if len(locations) == 0 {
		return []lsp.Moniker{}
	}
	target := s.monikerParsedDocumentContext(ctx, parsed, locations[0].URI)
	if target == nil || ctx.Err() != nil {
		return []lsp.Moniker{}
	}
	declaration, ok := vbMonikerDeclaration(target, locations[0].Range)
	if !ok || ctx.Err() != nil {
		return []lsp.Moniker{}
	}
	scopeName := declaration.Scope
	if scopeName != "" {
		for _, procedure := range graphVBProcedureRanges(target) {
			if ctx.Err() != nil {
				return nil
			}
			if strings.EqualFold(procedure.name, scopeName) {
				scopeName = procedure.name
				break
			}
		}
	}
	parts := []string{target.URI, declaration.MemberOf, declaration.MemberOf, scopeName, declaration.Name, strconv.Itoa(declaration.Range.Start.Line), strconv.Itoa(declaration.Range.Start.Character)}
	identifier := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			identifier = append(identifier, part)
		}
	}
	kind := "export"
	if scopeName != "" {
		kind = "local"
	}
	if ctx.Err() != nil {
		return nil
	}
	return []lsp.Moniker{{Scheme: "asp-lsp", Identifier: strings.Join(identifier, "#"), Unique: "project", Kind: kind}}
}

//lint:ignore U1000 retained for callers that use the legacy context-free helper.
func (s *Server) monikerParsedDocument(owner *core.ParsedDocument, uri string) *core.ParsedDocument {
	return s.monikerParsedDocumentContext(context.Background(), owner, uri)
}

func (s *Server) monikerParsedDocumentContext(ctx context.Context, owner *core.ParsedDocument, uri string) *core.ParsedDocument {
	if ctx == nil {
		ctx = context.Background()
	}
	if owner == nil || ctx.Err() != nil {
		return nil
	}
	if workspacepkg.SameFileIdentityURI(owner.URI, uri) {
		return owner
	}
	includedDocuments, complete := s.includedDocumentsContextResult(ctx, owner)
	if !complete || ctx.Err() != nil {
		return nil
	}
	for _, included := range includedDocuments {
		if ctx.Err() != nil {
			return nil
		}
		if workspacepkg.SameFileIdentityURI(included.URI, uri) {
			return included
		}
	}
	if doc := s.documentByURI(uri); doc != nil {
		parsed := s.parseTextDocument(doc, s.settings.DefaultLanguage)
		if ctx.Err() != nil {
			return nil
		}
		return parsed
	}
	path := fileURIPath(uri)
	if path == "" || ctx.Err() != nil {
		return nil
	}
	content, err := s.readWorkspaceTextFileWithinBoundaries(ctx, path, filepath.Dir(filepath.Clean(path)))
	if err != nil {
		return nil
	}
	if ctx.Err() != nil {
		return nil
	}
	parsed := s.parseText(uri, content, s.settings.DefaultLanguage)
	if ctx.Err() != nil {
		return nil
	}
	return parsed
}

func vbMonikerDeclaration(parsed *core.ParsedDocument, targetRange lsp.Range) (vbUsageDeclaration, bool) {
	for _, declaration := range graphVBDeclarations(parsed) {
		if !declaration.Implicit && lspRangeEqual(declaration.Range, targetRange) {
			return declaration, true
		}
	}
	return vbUsageDeclaration{}, false
}

//lint:ignore U1000 retained for callers that use the legacy context-free helper.
func (s *Server) selectionRanges(uri string, positions []lsp.Position) []lsp.SelectionRange {
	return s.selectionRangesContext(context.Background(), uri, positions)
}

func (s *Server) selectionRangesContext(ctx context.Context, uri string, positions []lsp.Position) []lsp.SelectionRange {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil
	}
	doc, parsed := s.parsed(uri)
	if doc == nil || ctx.Err() != nil {
		return nil
	}
	result := make([]lsp.SelectionRange, 0, len(positions))
	documentRange := doc.Range(0, len(doc.Text))
	for _, position := range positions {
		if ctx.Err() != nil {
			return nil
		}
		region := core.RegionAt(parsed, doc.OffsetAt(position))
		if region != nil {
			var selection *lsp.SelectionRange
			switch region.Language {
			case core.LanguageHTML:
				s.htmlMu.Lock()
				selection = s.html.SelectionRange(parsed, position)
				s.htmlMu.Unlock()
				if ctx.Err() != nil {
					return nil
				}
			case core.LanguageCSS:
				s.cssMu.Lock()
				selection = s.css.SelectionRange(parsed, position)
				s.cssMu.Unlock()
				if ctx.Err() != nil {
					return nil
				}
			case core.LanguageJavaScript, core.LanguageJScript:
				var selections []lsp.SelectionRange
				ok := s.javaScriptLanguageServiceRequest(ctx, uri, position, "textDocument/selectionRange", map[string]any{"positions": []lsp.Position{position}}, &selections)
				if ctx.Err() != nil {
					return nil
				}
				if ok && len(selections) > 0 {
					selection = &selections[0]
				}
			case core.LanguageVBScript:
				selection = vbscript.SelectionRange(parsed, position)
				if ctx.Err() != nil {
					return nil
				}
			}
			if selection != nil {
				result = append(result, *selection)
				continue
			}
		}
		offset := doc.OffsetAt(position)
		word := vbscript.WordAt(doc.Text, offset)
		if word == "" {
			empty := lsp.Range{Start: position, End: position}
			result = append(result, lsp.SelectionRange{Range: empty, Parent: &lsp.SelectionRange{Range: documentRange}})
			continue
		}
		start := offset
		for start > 0 && isServerIdent(doc.Text[start-1]) {
			start--
		}
		end := start + len(word)
		result = append(result, lsp.SelectionRange{Range: doc.Range(start, end), Parent: &lsp.SelectionRange{Range: documentRange}})
	}
	if ctx.Err() != nil {
		return nil
	}
	return result
}

func (s *Server) linkedEditingRange(uri string, position lsp.Position) *lsp.LinkedEditingRanges {
	doc, parsed := s.parsed(uri)
	if doc == nil {
		return nil
	}
	s.htmlMu.Lock()
	ranges := s.html.LinkedEditingRanges(parsed, position)
	s.htmlMu.Unlock()
	if len(ranges) < 2 {
		return nil
	}
	return &lsp.LinkedEditingRanges{Ranges: ranges, WordPattern: `[A-Za-z][A-Za-z0-9:-]*`}
}

func (s *Server) onTypeFormatting(params onTypeFormattingParams) []lsp.TextEdit {
	doc, parsed := s.parsed(params.TextDocument.URI)
	if doc == nil {
		return []lsp.TextEdit{}
	}
	offset := doc.OffsetAt(params.Position)
	probe := offset
	if params.Ch == ">" && probe > 0 {
		probe--
	}
	region := core.RegionAt(parsed, probe)
	if region != nil && (region.Language == core.LanguageJavaScript || region.Language == core.LanguageJScript) {
		options := s.formattingOptions(core.FormattingOptions{TabSize: params.Options.TabSize, InsertSpaces: params.Options.InsertSpaces})
		return javascript.OnTypeFormatting(parsed, params.Position, params.Ch, options)
	}
	if params.Ch == ">" {
		if region != nil && region.Language == core.LanguageHTML {
			s.htmlMu.Lock()
			completion := s.html.TagComplete(parsed, params.Position)
			s.htmlMu.Unlock()
			if completion != "" {
				return []lsp.TextEdit{{Range: lsp.Range{Start: params.Position, End: params.Position}, NewText: completion}}
			}
			return []lsp.TextEdit{}
		}
		return s.aspCloseOnTypeFormatting(doc, params.Position, params.Options)
	}
	if region == nil || region.Language != core.LanguageVBScript {
		return []lsp.TextEdit{}
	}
	if params.Position.Line <= 0 {
		return []lsp.TextEdit{}
	}
	previous, ok := previousNonEmptyDocumentLine(doc, params.Position.Line-1)
	if !ok {
		return []lsp.TextEdit{}
	}
	current := documentLineText(doc, params.Position.Line)
	options := s.formattingOptions(core.FormattingOptions{TabSize: params.Options.TabSize, InsertSpaces: params.Options.InsertSpaces})
	unit := formattingIndentUnit(options)
	baseIndent := leadingWhitespace(previous)
	trimmedPrevious := strings.TrimSpace(previous)
	trimmedCurrent := strings.TrimSpace(current)
	shouldIndent := vbscriptOnTypeIndentPattern.MatchString(trimmedPrevious) && !vbscriptEndPattern.MatchString(trimmedPrevious)
	shouldOutdent := vbscriptOnTypeOutdentPattern.MatchString(trimmedCurrent)
	desired := baseIndent
	if shouldOutdent {
		desired = trimIndentUnit(baseIndent, unit)
	} else if shouldIndent {
		desired += unit
	}
	return onTypeLineIndentEdit(params.Position.Line, current, desired)
}

var vbscriptOnTypeIndentPattern = regexp.MustCompile(`(?i)^(If|For|For\s+Each|Do|While|With|Sub|Function|Class|Property)\b`)
var vbscriptOnTypeOutdentPattern = regexp.MustCompile(`(?i)^(End|Else|ElseIf|Next|Loop|Wend)\b`)
var vbscriptEndPattern = regexp.MustCompile(`(?i)^End\b`)

func (s *Server) aspCloseOnTypeFormatting(doc *core.TextDocument, position lsp.Position, requestOptions core.FormattingOptions) []lsp.TextEdit {
	current := documentLineText(doc, position.Line)
	lineStart := doc.OffsetAt(lsp.Position{Line: position.Line, Character: 0})
	positionOffset := doc.OffsetAt(position)
	if positionOffset < lineStart || positionOffset-lineStart > len(current) || !strings.HasSuffix(strings.TrimRight(current[:positionOffset-lineStart], " \t"), "%>") {
		return []lsp.TextEdit{}
	}
	previous, ok := previousNonEmptyDocumentLine(doc, position.Line-1)
	if !ok {
		return []lsp.TextEdit{}
	}
	options := s.formattingOptions(core.FormattingOptions{TabSize: requestOptions.TabSize, InsertSpaces: requestOptions.InsertSpaces})
	unit := formattingIndentUnit(options)
	desired := leadingWhitespace(previous)
	if vbscriptEndPattern.MatchString(strings.TrimSpace(previous)) {
		desired = trimIndentUnit(desired, unit)
	}
	return onTypeLineIndentEdit(position.Line, current, desired)
}

func formattingIndentUnit(options core.FormattingOptions) string {
	if !options.InsertSpaces {
		return "\t"
	}
	size := options.TabSize
	if size <= 0 {
		size = 2
	}
	return strings.Repeat(" ", size)
}

func documentLineText(doc *core.TextDocument, line int) string {
	if doc == nil || line < 0 {
		return ""
	}
	start := doc.OffsetAt(lsp.Position{Line: line, Character: 0})
	end := doc.OffsetAt(lsp.Position{Line: line + 1, Character: 0})
	return strings.TrimSuffix(strings.TrimSuffix(doc.Text[start:end], "\n"), "\r")
}

func previousNonEmptyDocumentLine(doc *core.TextDocument, line int) (string, bool) {
	for ; line >= 0; line-- {
		text := documentLineText(doc, line)
		if strings.TrimSpace(text) != "" {
			return text, true
		}
	}
	return "", false
}

func leadingWhitespace(text string) string {
	return text[:len(text)-len(strings.TrimLeft(text, " \t"))]
}

func trimIndentUnit(indent, unit string) string {
	if len(indent) <= len(unit) {
		return ""
	}
	return indent[:len(indent)-len(unit)]
}

func onTypeLineIndentEdit(line int, current, desired string) []lsp.TextEdit {
	existing := leadingWhitespace(current)
	if existing == desired {
		return []lsp.TextEdit{}
	}
	return []lsp.TextEdit{{
		Range: lsp.Range{
			Start: lsp.Position{Line: line, Character: 0},
			End:   lsp.Position{Line: line, Character: len(existing)},
		},
		NewText: desired,
	}}
}

func (s *Server) willRenameFiles(params willRenameFilesParams) any {
	if !s.settings.UpdateIncludesOnFileRename {
		return nil
	}
	type renamePair struct{ oldPath, newPath string }
	renames := make([]renamePair, 0, len(params.Files))
	for _, file := range params.Files {
		oldPath := fileURIPath(file.OldURI)
		newPath := fileURIPath(file.NewURI)
		if oldPath == "" || newPath == "" {
			continue
		}
		renames = append(renames, renamePair{oldPath: filepath.Clean(oldPath), newPath: filepath.Clean(newPath)})
	}
	if len(renames) == 0 {
		return map[string]any{"changes": map[string]any{}}
	}
	changes := map[string][]lsp.TextEdit{}
	s.mu.Lock()
	candidates := make(map[string]*core.TextDocument, len(s.workspace)+len(s.documents))
	for uri, doc := range s.workspace {
		candidates[uri] = doc
	}
	for uri, doc := range s.documents {
		candidates[uri] = doc
	}
	s.mu.Unlock()
	for uri, doc := range candidates {
		parsed := s.parseTextDocument(doc, s.settings.DefaultLanguage)
		if parsed == nil {
			continue
		}
		for _, include := range parsed.Includes {
			resolved, ok := s.includeTargetPathForMode(uri, include.Path, include.Mode)
			if !ok {
				continue
			}
			for _, rename := range renames {
				if workspacepkg.FileIdentityKeyFromFileName(resolved) != workspacepkg.FileIdentityKeyFromFileName(rename.oldPath) {
					continue
				}
				replacement := s.includePathForRenamedTarget(uri, include.Mode, rename.newPath)
				changes[uri] = append(changes[uri], lsp.TextEdit{Range: include.Range, NewText: replacement})
				break
			}
		}
	}
	if len(changes) == 0 {
		return nil
	}
	return map[string]any{"changes": changes}
}

func (s *Server) includePathForRenamedTarget(ownerURI, mode, targetPath string) string {
	if mode != "virtual" {
		ownerPath := fileURIPath(ownerURI)
		if relative, err := filepath.Rel(filepath.Dir(ownerPath), targetPath); err == nil {
			return filepath.ToSlash(relative)
		}
		return filepath.ToSlash(targetPath)
	}
	s.mu.Lock()
	virtualRoots := append([]string(nil), s.settings.VirtualRoots...)
	virtualRoot := s.settings.VirtualRoot
	roots := append([]workspaceRoot(nil), s.workspaceRoots...)
	s.mu.Unlock()
	candidates := append([]string(nil), virtualRoots...)
	if virtualRoot != "" {
		candidates = append(candidates, virtualRoot)
	}
	for _, root := range roots {
		candidates = append(candidates, root.Path)
	}
	for _, root := range candidates {
		relative, err := filepath.Rel(root, targetPath)
		if err == nil && relative != ".." && !filepath.IsAbs(relative) && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return "/" + filepath.ToSlash(relative)
		}
	}
	return "/" + filepath.Base(targetPath)
}

func callHierarchyItem(uri string, signature vbscript.Signature) lsp.CallHierarchyItem {
	return lsp.CallHierarchyItem{
		Name:           signature.Name,
		Kind:           12,
		URI:            uri,
		Range:          signature.Range,
		SelectionRange: signature.Range,
		Data:           map[string]any{"uri": uri, "name": signature.Name},
	}
}

func (s *Server) documentSymbols(uri string) []lsp.DocumentSymbol {
	return s.documentSymbolsContext(context.Background(), uri)
}

func (s *Server) documentSymbolsContext(ctx context.Context, uri string) []lsp.DocumentSymbol {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil
	}
	doc, parsed := s.parsed(uri)
	if parsed == nil || ctx.Err() != nil {
		return nil
	}
	s.htmlMu.Lock()
	symbols := s.html.DocumentSymbols(parsed)
	s.htmlMu.Unlock()
	if ctx.Err() != nil {
		return nil
	}
	s.cssMu.Lock()
	symbols = append(symbols, s.css.DocumentSymbols(parsed)...)
	s.cssMu.Unlock()
	if ctx.Err() != nil {
		return nil
	}
	symbols = append(symbols, s.javaScriptDocumentSymbols(ctx, doc, parsed)...)
	if ctx.Err() != nil {
		return nil
	}
	symbols = append(symbols, s.vbDocumentSymbols(parsed)...)
	if ctx.Err() != nil {
		return nil
	}
	symbols = append(symbols, s.legacyUndefinedGlobalDocumentSymbols(ctx, uri)...)
	if ctx.Err() != nil {
		return nil
	}
	return symbols
}

func (s *Server) vbDocumentSymbols(parsed *core.ParsedDocument) []lsp.DocumentSymbol {
	declarations := s.cachedVBDeclarations(parsed)
	symbols := make([]lsp.DocumentSymbol, 0, len(declarations))
	for _, declaration := range declarations {
		kind := 12
		switch declaration.Kind {
		case "class":
			kind = 5
		case "property":
			kind = 7
		case "function", "sub", "method":
		default:
			continue
		}
		name := declaration.Name
		if declaration.MemberOf != "" {
			name = declaration.MemberOf + "." + name
		}
		symbols = append(symbols, lsp.DocumentSymbol{
			Name:           name,
			Kind:           kind,
			Range:          declaration.Range,
			SelectionRange: declaration.Range,
		})
	}
	for _, object := range serverObjectSymbols(parsed) {
		declaration := object.Declaration
		symbols = append(symbols, lsp.DocumentSymbol{
			Name:           declaration.Name,
			Detail:         declaration.TypeName,
			Kind:           13,
			Range:          object.TagRange,
			SelectionRange: declaration.Range,
		})
	}
	return symbols
}

func (s *Server) javaScriptDocumentSymbols(ctx context.Context, doc *core.TextDocument, parsed *core.ParsedDocument) []lsp.DocumentSymbol {
	if ctx == nil {
		ctx = context.Background()
	}
	result := make([]lsp.DocumentSymbol, 0)
	for _, language := range []core.EmbeddedLanguage{core.LanguageJavaScript, core.LanguageJScript} {
		if ctx.Err() != nil {
			return nil
		}
		position, ok := firstEmbeddedLanguagePosition(doc, parsed, language)
		if !ok {
			continue
		}
		var symbols []lsp.DocumentSymbol
		if !s.javaScriptLanguageServiceRequest(ctx, parsed.URI, position, "textDocument/documentSymbol", nil, &symbols) {
			if ctx.Err() != nil {
				return nil
			}
			continue
		}
		if ctx.Err() != nil {
			return nil
		}
		normalizeJavaScriptDocumentSymbols(doc, symbols)
		result = append(result, symbols...)
	}
	if ctx.Err() != nil {
		return nil
	}
	return result
}

func firstEmbeddedLanguagePosition(doc *core.TextDocument, parsed *core.ParsedDocument, language core.EmbeddedLanguage) (lsp.Position, bool) {
	if doc == nil || parsed == nil {
		return lsp.Position{}, false
	}
	for _, region := range parsed.Regions {
		if region.Language == language {
			return doc.PositionAt(region.ContentStart), true
		}
	}
	return lsp.Position{}, false
}

func normalizeJavaScriptDocumentSymbols(doc *core.TextDocument, symbols []lsp.DocumentSymbol) {
	for index := range symbols {
		symbol := &symbols[index]
		switch symbol.Kind {
		case 5:
			symbol.Kind = 5
		case 6, 12:
			symbol.Kind = 12
		case 7:
			symbol.Kind = 7
		case 13, 14:
			if javaScriptSymbolIsConst(doc, *symbol) {
				symbol.Kind = 14
			} else {
				symbol.Kind = 13
			}
		case 2, 3:
			symbol.Kind = 2
		default:
			symbol.Kind = 19
		}
		normalizeJavaScriptDocumentSymbols(doc, symbol.Children)
	}
}

func javaScriptSymbolIsConst(doc *core.TextDocument, symbol lsp.DocumentSymbol) bool {
	if doc == nil {
		return false
	}
	start := doc.OffsetAt(symbol.Range.Start)
	selectionStart := doc.OffsetAt(symbol.SelectionRange.Start)
	if selectionStart < start {
		return false
	}
	if start == selectionStart {
		for start > 0 && doc.Text[start-1] != '\n' && doc.Text[start-1] != '\r' {
			start--
		}
	}
	return javaScriptConstDeclarationWordPattern.MatchString(doc.Text[start:selectionStart])
}

func (s *Server) foldingRanges(uri string) []lsp.FoldingRange {
	return s.foldingRangesContext(context.Background(), uri)
}

func (s *Server) foldingRangesContext(ctx context.Context, uri string) []lsp.FoldingRange {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil
	}
	doc, parsed := s.parsed(uri)
	if doc == nil || ctx.Err() != nil {
		return nil
	}
	s.htmlMu.Lock()
	ranges := s.html.FoldingRanges(parsed)
	s.htmlMu.Unlock()
	if ctx.Err() != nil {
		return nil
	}
	s.cssMu.Lock()
	ranges = append(ranges, s.css.FoldingRanges(parsed)...)
	s.cssMu.Unlock()
	if ctx.Err() != nil {
		return nil
	}
	for _, language := range []core.EmbeddedLanguage{core.LanguageJavaScript, core.LanguageJScript} {
		if ctx.Err() != nil {
			return nil
		}
		position, ok := firstJavaScriptPositionForLanguage(doc, parsed, language)
		if !ok {
			continue
		}
		var javaScriptRanges []lsp.FoldingRange
		ok = s.javaScriptLanguageServiceRequest(ctx, uri, position, "textDocument/foldingRange", nil, &javaScriptRanges)
		if ctx.Err() != nil {
			return nil
		}
		if ok {
			ranges = append(ranges, javaScriptRanges...)
		}
	}
	for _, region := range parsed.Regions {
		if ctx.Err() != nil {
			return nil
		}
		if region.Language == core.LanguageHTML {
			continue
		}
		start := doc.PositionAt(region.Start)
		end := doc.PositionAt(region.End)
		if end.Line > start.Line {
			ranges = append(ranges, lsp.FoldingRange{StartLine: start.Line, StartCharacter: start.Character, EndLine: end.Line, EndCharacter: end.Character})
		}
	}
	ranges = append(ranges, vbscript.FoldingRanges(parsed)...)
	if ctx.Err() != nil {
		return nil
	}
	deduplicated := make([]lsp.FoldingRange, 0, len(ranges))
	seen := make(map[lsp.FoldingRange]struct{}, len(ranges))
	for _, foldingRange := range ranges {
		if _, exists := seen[foldingRange]; exists {
			continue
		}
		seen[foldingRange] = struct{}{}
		deduplicated = append(deduplicated, foldingRange)
	}
	ranges = deduplicated
	sort.SliceStable(ranges, func(i, j int) bool {
		if ranges[i].StartLine != ranges[j].StartLine {
			return ranges[i].StartLine < ranges[j].StartLine
		}
		return ranges[i].EndLine < ranges[j].EndLine
	})
	if ctx.Err() != nil {
		return nil
	}
	return ranges
}

func (s *Server) documentLinks(uri string) []lsp.DocumentLink {
	doc, parsed := s.parsed(uri)
	if parsed == nil {
		return nil
	}
	links := make([]lsp.DocumentLink, 0, len(parsed.Includes))
	for _, include := range parsed.Includes {
		target := include.Path
		if targetPath, ok := s.includeTargetPathForMode(parsed.URI, include.Path, include.Mode); ok {
			target = filePathURI(targetPath)
		}
		links = append(links, lsp.DocumentLink{Range: includeDocumentLinkRange(doc, include.Range), Target: target})
	}
	return links
}

func includeDocumentLinkRange(doc *core.TextDocument, r lsp.Range) lsp.Range {
	if doc == nil {
		return r
	}
	start := doc.OffsetAt(r.Start)
	end := doc.OffsetAt(r.End)
	if start <= 0 || end >= len(doc.Text) {
		return r
	}
	quote := doc.Text[start-1]
	if (quote != '"' && quote != '\'') || doc.Text[end] != quote {
		return r
	}
	return doc.Range(start-1, end+1)
}

func (s *Server) diagnostics(uri string) []lsp.Diagnostic {
	snapshot := s.diagnosticsSnapshot(context.Background(), uri)
	if !snapshot.ok {
		return nil
	}
	return snapshot.diagnostics
}

func (s *Server) logJavaScriptDiagnosticsWorker(parsed *core.ParsedDocument) {
	if !parsedHasJavaScript(parsed) {
		return
	}
	if !s.isDebugSummaryEnabled() && !s.debugLogFileEnabled() {
		return
	}
	s.logDebugSummary("[asp-lsp] javascript.diagnostics.worker: " + parsed.URI + ", payloadBytes=" + strconv.Itoa(javaScriptWorkerPayloadBytes(parsed)))
	s.logDebugSummary("[asp-lsp] javascriptSemantic.worker: " + parsed.URI)
}

func (s *Server) includeDiagnosticsContext(ctx context.Context, parsed *core.ParsedDocument) ([]lsp.Diagnostic, bool) {
	if parsed == nil {
		return nil, true
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil, false
	}
	diagnostics := []lsp.Diagnostic{}
	if len(parsed.Includes) > 0 {
		s.logDebugSummary("[asp-lsp] includeDiagnostics.directIncludes: " + parsed.URI)
		s.logDebugSummary("[asp-lsp] vbProject.summaryGraph.collect: " + parsed.URI)
		s.logDebugSummary("[asp-lsp] vbProject.summaryGraph.built: " + parsed.URI)
		s.logDebugSummary("[asp-lsp] vbProject.summaryGraph.reuse: " + parsed.URI)
	}
	results := make([][]lsp.Diagnostic, len(parsed.Includes))
	complete := make([]bool, len(parsed.Includes))
	var acyclic sync.Map
	s.analysisWorkers.parallelFor(ctx, len(parsed.Includes), func(workerCtx context.Context, index int) {
		include := parsed.Includes[index]
		if workerCtx.Err() != nil {
			return
		}
		details, ok := s.includeTargetDetailsForModeContext(workerCtx, parsed.URI, include.Path, include.Mode)
		if workerCtx.Err() != nil {
			return
		}
		if !ok {
			complete[index] = true
			return
		}
		if !details.Exists {
			results[index] = append(results[index], lsp.Diagnostic{
				Range:    include.Range,
				Severity: lsp.DiagnosticSeverityWarning,
				Code:     "include.missing",
				Source:   "asp-lsp-include",
				Message:  s.missingIncludeMessage(include.Path),
				Data:     map[string]any{"path": include.Path},
			})
			complete[index] = true
			return
		}
		if details.CaseMismatch {
			results[index] = append(results[index], lsp.Diagnostic{
				Range:    include.Range,
				Severity: lsp.DiagnosticSeverityWarning,
				Code:     "include.pathCaseMismatch",
				Source:   "asp-lsp-include",
				Message:  "Include path '" + include.Path + "' differs from the file system casing '" + details.ActualIncludePath + "'.",
				Data:     map[string]any{"path": include.Path, "actualPath": details.ActualIncludePath},
			})
		}
		if workspacepkg.SameFileIdentityURI(parsed.URI, filePathURI(details.Path)) {
			results[index] = append(results[index], lsp.Diagnostic{
				Range:    include.Range,
				Severity: lsp.DiagnosticSeverityWarning,
				Code:     "include.currentDocument",
				Source:   "asp-lsp-include",
				Message:  "Include file references the current document.",
			})
			complete[index] = true
			return
		}
		cycle, hasCycle, cycleComplete := s.includeCycleContextWithMemo(workerCtx, parsed.URI, details.Path, &acyclic)
		if !cycleComplete || workerCtx.Err() != nil {
			return
		}
		if hasCycle {
			s.logDebugSummary("[asp-lsp] includeDiagnostics.cycleGraph: " + parsed.URI)
			results[index] = append(results[index], lsp.Diagnostic{
				Range:    include.Range,
				Severity: lsp.DiagnosticSeverityWarning,
				Code:     "include.cycle",
				Source:   "asp-lsp-include",
				Message:  "Include cycle detected: " + strings.Join(cycle, " -> ") + ".",
				Data:     map[string]any{"cycle": strings.Join(cycle, " -> ")},
			})
		}
		complete[index] = true
	})
	for index := range results {
		if !complete[index] || ctx.Err() != nil {
			return nil, false
		}
		diagnostics = append(diagnostics, results[index]...)
	}
	if ctx.Err() != nil {
		return nil, false
	}
	return diagnostics, true
}

func (s *Server) includeCycleContextWithMemo(ctx context.Context, ownerURI, startPath string, acyclic *sync.Map) ([]string, bool, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil, false, false
	}
	ownerPath := fileURIPath(ownerURI)
	if ownerPath == "" {
		return nil, false, true
	}
	ownerPath = filepath.Clean(ownerPath)
	startPath = filepath.Clean(startPath)
	stack := map[string]int{}
	var pathStack []string
	var search func(string) ([]string, bool, bool)
	search = func(path string) ([]string, bool, bool) {
		if ctx.Err() != nil {
			return nil, false, false
		}
		path = filepath.Clean(path)
		identity := workspacepkg.FileIdentityKeyFromFileName(path)
		if acyclic != nil {
			if _, ok := acyclic.Load(identity); ok {
				return nil, false, true
			}
		}
		if path == ownerPath && len(pathStack) > 0 {
			return append(baseNames(pathStack), filepath.Base(ownerPath)), true, true
		}
		if index, ok := stack[path]; ok {
			return baseNames(pathStack[index:]), true, true
		}
		stack[path] = len(pathStack)
		pathStack = append(pathStack, path)
		defer func() {
			delete(stack, path)
			pathStack = pathStack[:len(pathStack)-1]
		}()
		parsed := s.parsedIncludeFileContext(ctx, path)
		if parsed == nil {
			return nil, false, false
		}
		for _, include := range parsed.Includes {
			if ctx.Err() != nil {
				return nil, false, false
			}
			details, ok := s.includeTargetDetailsForModeContext(ctx, parsed.URI, include.Path, include.Mode)
			if ctx.Err() != nil {
				return nil, false, false
			}
			if !ok {
				continue
			}
			if !details.Exists {
				continue
			}
			if cycle, ok, complete := search(details.Path); !complete {
				return nil, false, false
			} else if ok {
				return cycle, true, true
			}
		}
		if ctx.Err() != nil {
			return nil, false, false
		}
		if acyclic != nil {
			acyclic.Store(identity, struct{}{})
		}
		return nil, false, true
	}
	return search(startPath)
}

func (s *Server) parsedIncludeFileContext(ctx context.Context, path string) *core.ParsedDocument {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil
	}
	uri := filePathURI(path)
	s.mu.Lock()
	defaultLanguage := s.settings.DefaultLanguage
	s.mu.Unlock()
	if doc := s.documentByURI(uri); doc != nil {
		parsed := s.parseTextDocument(doc, defaultLanguage)
		if ctx.Err() != nil {
			return nil
		}
		return parsed
	}
	content, err := s.readWorkspaceTextFileWithinBoundaries(ctx, path, filepath.Dir(filepath.Clean(path)))
	if err != nil {
		return nil
	}
	if ctx.Err() != nil {
		return nil
	}
	parsed := s.parseTextContext(ctx, uri, content, defaultLanguage)
	if ctx.Err() != nil {
		return nil
	}
	return parsed
}

func (s *Server) parsedIncludeFile(path string) *core.ParsedDocument {
	return s.parsedIncludeFileContext(context.Background(), path)
}

func baseNames(paths []string) []string {
	names := make([]string, 0, len(paths))
	for _, path := range paths {
		names = append(names, filepath.Base(path))
	}
	return names
}
