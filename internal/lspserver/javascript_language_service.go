package lspserver

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/tsgoadapter"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

func (s *Server) javaScriptMonikers(ctx context.Context, sourceURI string, position lsp.Position) []lsp.Moniker {
	if ctx == nil {
		ctx = context.Background()
	}
	if !lockMutexContext(ctx, &s.javascriptMu) {
		return nil
	}
	defer s.javascriptMu.Unlock()
	if ctx.Err() != nil {
		return nil
	}
	request, ok := s.prepareJavaScriptRequestContext(ctx, sourceURI, position)
	if !ok || ctx.Err() != nil {
		return []lsp.Moniker{}
	}
	body, err := json.Marshal(map[string]any{
		"textDocument": map[string]any{"uri": request.active.uri},
		"position":     request.virtualPosition(position),
	})
	if err != nil || ctx.Err() != nil {
		return []lsp.Moniker{}
	}
	raw, err := request.project.Request(ctx, "textDocument/hover", body)
	if err != nil || ctx.Err() != nil {
		return []lsp.Moniker{}
	}
	var hover *struct {
		Contents lsp.MarkupContent `json:"contents"`
		Range    *lsp.Range        `json:"range"`
	}
	if json.Unmarshal(raw, &hover) != nil || hover == nil || hover.Range == nil || ctx.Err() != nil {
		return []lsp.Moniker{}
	}
	quickInfo := javaScriptQuickInfoLine(hover.Contents.Value)
	if quickInfo == "" || quickInfo == "any" {
		return []lsp.Moniker{}
	}
	virtualDocument := request.active.virtualDocument
	if virtualDocument == nil {
		virtualDocument = core.NewTextDocument(request.active.virtual.URI, request.active.virtual.LanguageID, 0, request.active.virtual.Text)
	}
	virtualStart := virtualDocument.OffsetAt(hover.Range.Start)
	virtualEnd := virtualDocument.OffsetAt(hover.Range.End)
	sourceStart, startOK := request.active.virtual.ToSourceOffset(virtualStart)
	sourceEnd, endOK := request.active.virtual.ToSourceOffset(virtualEnd)
	if !startOK || !endOK || sourceStart < 0 || sourceEnd < sourceStart || sourceEnd > len(request.active.source.Text) {
		return []lsp.Moniker{}
	}
	if ctx.Err() != nil {
		return nil
	}
	name := strings.TrimSpace(request.active.source.Text[sourceStart:sourceEnd])
	if name == "" {
		return []lsp.Moniker{}
	}
	if ctx.Err() != nil {
		return nil
	}
	spanStart := javaScriptUTF16Offset(request.active.virtual.Text, virtualStart)
	spanEnd := javaScriptUTF16Offset(request.active.virtual.Text, virtualEnd)
	kind := "local"
	if javaScriptQuickInfoIsExport(quickInfo, name) {
		kind = "export"
	}
	if ctx.Err() != nil {
		return nil
	}
	return []lsp.Moniker{{
		Scheme:     "asp-lsp-js",
		Identifier: sourceURI + "#" + request.active.virtual.LanguageID + "#" + name + "#" + strconv.Itoa(spanStart) + "#" + strconv.Itoa(spanEnd-spanStart),
		Unique:     "project",
		Kind:       kind,
	}}
}

func javaScriptUTF16Offset(text string, byteOffset int) int {
	byteOffset = min(max(byteOffset, 0), len(text))
	units := 0
	for _, value := range text[:byteOffset] {
		units++
		if value > 0xffff {
			units++
		}
	}
	return units
}

func javaScriptQuickInfoLine(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "```") {
		if _, body, ok := strings.Cut(value, "\n"); ok {
			value = body
		}
	}
	line, _, _ := strings.Cut(strings.TrimSpace(value), "\n")
	return strings.TrimSpace(line)
}

func javaScriptQuickInfoIsExport(line, name string) bool {
	return strings.HasPrefix(line, "class ") || strings.HasPrefix(line, "function ") ||
		strings.HasPrefix(line, "constructor ") && !strings.EqualFold(name, "constructor")
}

func (s *Server) javaScriptLanguageServiceRequest(ctx context.Context, sourceURI string, position lsp.Position, method string, extra map[string]any, target any) bool {
	return s.javaScriptLanguageServiceRequestWithLockContext(ctx, ctx, sourceURI, position, method, extra, target)
}

func (s *Server) javaScriptLanguageServiceRequestWithLockContext(lockContext, serviceContext context.Context, sourceURI string, position lsp.Position, method string, extra map[string]any, target any) bool {
	if serviceContext == nil {
		serviceContext = context.Background()
	}
	if !lockMutexContext(lockContext, &s.javascriptMu) {
		return false
	}
	locked := true
	defer func() {
		if locked {
			s.javascriptMu.Unlock()
		}
	}()
	if serviceContext.Err() != nil {
		return false
	}
	request, ok := s.prepareJavaScriptRequestContext(serviceContext, sourceURI, position)
	if !ok || serviceContext.Err() != nil {
		return false
	}
	params := map[string]any{
		"textDocument": map[string]any{"uri": request.active.uri},
		"position":     request.virtualPosition(position),
	}
	for key, value := range extra {
		params[key] = value
	}
	if method == "textDocument/selectionRange" {
		if positions, ok := params["positions"].([]lsp.Position); ok {
			virtualPositions := make([]lsp.Position, 0, len(positions))
			for _, sourcePosition := range positions {
				if virtualPosition, mapped := request.active.virtual.ToVirtualPosition(sourcePosition, request.active.source); mapped {
					virtualPositions = append(virtualPositions, virtualPosition)
				}
			}
			params["positions"] = virtualPositions
		}
	}
	if method == "textDocument/inlayHint" || method == "textDocument/codeAction" || method == "textDocument/semanticTokens/range" {
		if sourceRange, ok := params["range"].(lsp.Range); ok {
			start, startOK := request.active.virtual.ToVirtualPosition(sourceRange.Start, request.active.source)
			end, endOK := request.active.virtual.ToVirtualPosition(sourceRange.End, request.active.source)
			if startOK && endOK {
				params["range"] = lsp.Range{Start: start, End: end}
			}
		}
	}
	if method == "textDocument/codeAction" {
		if serviceContext, ok := toVirtualJavaScriptServiceValue(params["context"], request.active); ok {
			params["context"] = serviceContext
		}
	}
	body, err := json.Marshal(params)
	if err != nil || serviceContext.Err() != nil {
		return false
	}
	if isJavaScriptDiagnosticMethod(method) {
		s.javascriptMu.Unlock()
		locked = false
	}
	raw, err := request.serviceRequest(serviceContext, method, body)
	if err != nil || serviceContext.Err() != nil {
		if err != nil {
			s.logDebugSummary("[asp-lsp] javascript.languageService.error: " + err.Error())
		}
		return false
	}
	if serviceContext.Err() != nil {
		return false
	}
	if isJavaScriptDiagnosticMethod(method) {
		if !remapJavaScriptDiagnosticResponse(raw, target, request.active) || serviceContext.Err() != nil {
			return false
		}
		return true
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil || serviceContext.Err() != nil {
		return false
	}
	value = remapJavaScriptServiceValue(value, request.active, request.files)
	if serviceContext.Err() != nil {
		return false
	}
	remapped, err := json.Marshal(value)
	if err != nil || serviceContext.Err() != nil {
		return false
	}
	if json.Unmarshal(remapped, target) != nil || serviceContext.Err() != nil {
		return false
	}
	return true
}

func lockMutexContext(ctx context.Context, mutex *sync.Mutex) bool {
	if ctx == nil {
		mutex.Lock()
		return true
	}
	for {
		if ctx.Err() != nil {
			return false
		}
		if mutex.TryLock() {
			return true
		}
		timer := time.NewTimer(time.Millisecond)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return false
		case <-timer.C:
		}
	}
}

func (s *Server) resolveJavaScriptCompletionItem(ctx context.Context, item lsp.CompletionItem) (lsp.CompletionItem, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	var data struct {
		FileName   string `json:"fileName"`
		Position   int    `json:"position"`
		AutoImport any    `json:"autoImport"`
	}
	if remarshal(item.Data, &data) != nil || data.FileName == "" || data.Position < 0 {
		return item, false
	}
	language, ok := javaScriptCompletionLanguage(data.FileName)
	if !ok || ctx.Err() != nil {
		if ctx.Err() != nil {
			return lsp.CompletionItem{}, false
		}
		return item, false
	}
	s.mu.Lock()
	var owner *core.TextDocument
	for _, doc := range s.documents {
		if doc != nil && data.FileName == javaScriptVirtualPath(doc.URI, language) {
			owner = doc
			break
		}
	}
	s.mu.Unlock()
	if owner == nil || ctx.Err() != nil {
		if ctx.Err() != nil {
			return lsp.CompletionItem{}, false
		}
		return item, false
	}
	if !lockMutexContext(ctx, &s.javascriptMu) {
		return lsp.CompletionItem{}, false
	}
	defer s.javascriptMu.Unlock()
	if ctx.Err() != nil {
		return lsp.CompletionItem{}, false
	}
	_, parsed := s.parsed(owner.URI)
	position, ok := firstJavaScriptPositionForLanguage(owner, parsed, language)
	if !ok {
		return item, false
	}
	request, ok := s.prepareJavaScriptRequestContext(ctx, owner.URI, position)
	if !ok || request.active.path != data.FileName {
		if ctx.Err() != nil {
			return lsp.CompletionItem{}, false
		}
		return item, false
	}
	serviceItem, ok := toVirtualJavaScriptServiceValue(item, request.active)
	if !ok {
		return item, false
	}
	body, err := json.Marshal(serviceItem)
	if err != nil {
		return item, false
	}
	raw, err := request.project.Request(ctx, "completionItem/resolve", body)
	if err != nil || ctx.Err() != nil {
		if ctx.Err() != nil {
			return lsp.CompletionItem{}, false
		}
		return item, false
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return item, false
	}
	if ctx.Err() != nil {
		return lsp.CompletionItem{}, false
	}
	if data.AutoImport != nil {
		relocateJavaScriptCompletionImportEdits(value, request.active, data.Position)
	}
	value = remapJavaScriptServiceValue(value, request.active, request.files)
	if ctx.Err() != nil {
		return lsp.CompletionItem{}, false
	}
	remapped, err := json.Marshal(value)
	if err != nil || json.Unmarshal(remapped, &item) != nil {
		return item, false
	}
	if ctx.Err() != nil {
		return lsp.CompletionItem{}, false
	}
	return item, true
}

func javaScriptCompletionLanguage(fileName string) (core.EmbeddedLanguage, bool) {
	fileName = strings.ReplaceAll(fileName, "\\", "/")
	switch {
	case strings.HasSuffix(fileName, ".__asp_client.js"):
		return core.LanguageJavaScript, true
	case strings.HasSuffix(fileName, ".__asp_server.js"):
		return core.LanguageJScript, true
	default:
		return "", false
	}
}

func (s *Server) javaScriptCallHierarchyRequest(ctx context.Context, item lsp.CallHierarchyItem, method string, target any) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	if !lockMutexContext(ctx, &s.javascriptMu) {
		return false
	}
	defer s.javascriptMu.Unlock()
	if ctx.Err() != nil {
		return false
	}
	doc, parsed := s.parsed(item.URI)
	if doc == nil || parsed == nil || ctx.Err() != nil {
		return false
	}
	request, ok := s.prepareJavaScriptRequestContext(ctx, item.URI, item.SelectionRange.Start)
	if !ok || ctx.Err() != nil {
		return false
	}
	virtualRange := func(sourceRange lsp.Range) (lsp.Range, bool) {
		start, startOK := request.active.virtual.ToVirtualPosition(sourceRange.Start, doc)
		end, endOK := request.active.virtual.ToVirtualPosition(sourceRange.End, doc)
		return lsp.Range{Start: start, End: end}, startOK && endOK
	}
	selectionRange, selectionOK := virtualRange(item.SelectionRange)
	rangeValue, rangeOK := virtualRange(item.Range)
	if !selectionOK || !rangeOK || ctx.Err() != nil {
		return false
	}
	item.URI = request.active.uri
	item.SelectionRange = selectionRange
	item.Range = rangeValue
	body, err := json.Marshal(map[string]any{"item": item})
	if err != nil || ctx.Err() != nil {
		return false
	}
	raw, err := request.serviceRequest(ctx, method, body)
	if err != nil || ctx.Err() != nil {
		return false
	}
	var value any
	if json.Unmarshal(raw, &value) != nil || ctx.Err() != nil {
		return false
	}
	value = remapJavaScriptServiceValue(value, request.active, request.files)
	if ctx.Err() != nil {
		return false
	}
	remapped, err := json.Marshal(value)
	if err != nil || ctx.Err() != nil {
		return false
	}
	if json.Unmarshal(remapped, target) != nil || ctx.Err() != nil {
		return false
	}
	return true
}

func (s *Server) javaScriptDocumentsLocked() []*core.TextDocument {
	if s.javascriptDocumentsTestHook != nil {
		s.javascriptDocumentsTestHook(len(s.documents) + len(s.workspace))
	}
	return uniqueJavaScriptDocuments(s.documents, s.workspace)
}

func uniqueJavaScriptDocuments(open, workspace map[string]*core.TextDocument) []*core.TextDocument {
	documents := make([]*core.TextDocument, 0, len(open)+len(workspace))
	seen := make(map[string]struct{}, len(open))
	for _, doc := range open {
		if doc == nil {
			continue
		}
		key := workspacepkg.FileIdentityKeyFromURI(doc.URI)
		if _, ok := seen[key]; ok {
			continue
		}
		documents = append(documents, doc)
		seen[key] = struct{}{}
	}
	for _, doc := range workspace {
		if doc == nil {
			continue
		}
		key := workspacepkg.FileIdentityKeyFromURI(doc.URI)
		if _, ok := seen[key]; ok {
			continue
		}
		documents = append(documents, doc)
		seen[key] = struct{}{}
	}
	return documents
}

func javaScriptDocumentSnapshots(documents []*core.TextDocument) map[string]javaScriptDocumentSnapshot {
	snapshots := make(map[string]javaScriptDocumentSnapshot, len(documents))
	for _, document := range documents {
		snapshots[document.URI] = javaScriptDocumentSnapshot{document: document, version: document.Version, text: document.Text}
	}
	return snapshots
}

func (p *javaScriptProjectPreparation) matchesLocked(s *Server, root string, defaultLanguage string, explicitTypes []string, explicitTypesSet bool, explicitCompilerOptions map[string]any, ignoreProjectConfig bool) bool {
	if p == nil || p.root != root || p.defaultLanguage != defaultLanguage || p.explicitTypesSet != explicitTypesSet ||
		p.ignoreProjectConfig != ignoreProjectConfig || !slices.Equal(p.explicitTypes, explicitTypes) || !reflect.DeepEqual(p.compilerOptions, explicitCompilerOptions) {
		return false
	}
	if len(p.dirtyOwners) > 0 {
		return false
	}
	if p.documentGeneration == s.javascriptDocumentGeneration && s.javascriptDocumentGeneration != 0 {
		return true
	}
	documents := uniqueJavaScriptDocuments(s.documents, s.workspace)
	if len(p.documents) != len(documents) {
		return false
	}
	for _, document := range documents {
		if !matchesJavaScriptDocumentSnapshot(p.documents[document.URI], document) {
			return false
		}
	}
	return true
}

func (p *javaScriptProjectPreparation) matchesWorkspace(root string, defaultLanguage string, explicitTypes []string, explicitTypesSet bool, explicitCompilerOptions map[string]any, ignoreProjectConfig bool) bool {
	return p != nil && p.root == root && p.defaultLanguage == defaultLanguage && p.explicitTypesSet == explicitTypesSet &&
		p.ignoreProjectConfig == ignoreProjectConfig && slices.Equal(p.explicitTypes, explicitTypes) && reflect.DeepEqual(p.compilerOptions, explicitCompilerOptions)
}

func matchesJavaScriptDocumentSnapshot(snapshot javaScriptDocumentSnapshot, document *core.TextDocument) bool {
	return snapshot.document == document && snapshot.version == document.Version && snapshot.text == document.Text
}

func (p *javaScriptProjectPreparation) activeMapping(sourceURI string, position lsp.Position) *javaScriptVirtualFile {
	activeMapping := func(mappings []*javaScriptVirtualFile) *javaScriptVirtualFile {
		for _, mapping := range mappings {
			if _, ok := mapping.virtual.ToVirtualPosition(position, mapping.source); ok {
				return mapping
			}
		}
		return nil
	}
	if mapping := activeMapping(p.mappingsByOwner[sourceURI]); mapping != nil {
		return mapping
	}
	for ownerURI, mappings := range p.mappingsByOwner {
		if ownerURI == sourceURI || !workspacepkg.SameFileIdentityURI(ownerURI, sourceURI) {
			continue
		}
		if mapping := activeMapping(mappings); mapping != nil {
			return mapping
		}
	}
	return nil
}

func (s *Server) logJavaScriptProjectPreparation(sourceURI string, created bool, state tsgoadapter.ProjectState) {
	if created {
		s.logDebugSummary("[asp-lsp] javascript.openProjectFiles.collect: " + sourceURI)
		s.logDebugSummary("[asp-lsp] javascript.languageService.create: " + sourceURI)
	} else {
		collection := "reuse"
		if state.Rebuilt {
			collection = "collect"
		}
		s.logDebugSummary("[asp-lsp] javascript.openProjectFiles." + collection + ": " + sourceURI)
		s.logDebugSummary("[asp-lsp] javascript.languageService.reuse: " + sourceURI)
	}
	if state.Rebuilt && state.Builds > 1 {
		s.logDebugVerbose(fmt.Sprintf("[asp-lsp] javascript.languageService.rebuild: generation=%d, builds=%d", state.Generation, state.Builds))
	}
	if created || !state.Rebuilt {
		s.logDebugSummary("[asp-lsp] js.snapshot.changeRange.miss: " + sourceURI)
	} else {
		s.logDebugSummary("[asp-lsp] js.snapshot.changeRange.hit: " + sourceURI)
	}
}
