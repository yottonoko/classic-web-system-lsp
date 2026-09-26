package lspserver

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
)

func (s *Server) semanticTokens(uri string) lsp.SemanticTokens {
	return s.semanticTokensContext(context.Background(), uri)
}

func (s *Server) semanticTokensContext(ctx context.Context, uri string) lsp.SemanticTokens {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return lsp.SemanticTokens{Data: []int{}}
	}
	doc, parsed := s.openParsed(uri)
	if doc == nil || parsed == nil || ctx.Err() != nil {
		return lsp.SemanticTokens{Data: []int{}}
	}
	version := doc.Version
	text := doc.Text
	s.mu.Lock()
	if s.shutdown {
		s.mu.Unlock()
		return lsp.SemanticTokens{Data: []int{}}
	}
	generation := s.semanticTokensGenerationLocked(uri)
	s.cancelStaleSemanticTokensLocked(uri, doc, generation)
	if cached, ok := s.semantic[uri]; ok && cached.Version == version {
		s.mu.Unlock()
		if ctx.Err() != nil {
			return lsp.SemanticTokens{Data: []int{}}
		}
		s.logDebugVerbose("[asp-lsp] semanticTokens.full.cacheHit: " + uri)
		return cached.Tokens
	}
	refreshSupported := s.semanticTokensRefreshSupported
	inflightKey := semanticTokensResultID(uri, version)
	if inflight, ok := s.semanticInflight[inflightKey]; ok {
		s.mu.Unlock()
		if ctx.Err() != nil {
			return lsp.SemanticTokens{Data: []int{}}
		}
		if refreshSupported {
			tokens := inflight.snapshot()
			if strings.HasSuffix(tokens.ResultID, ":partial") && ctx.Err() == nil {
				return tokens
			}
		}
		if !inflight.wait(ctx) || ctx.Err() != nil {
			return lsp.SemanticTokens{Data: []int{}}
		}
		s.logDebugVerbose("[asp-lsp] semanticTokens.full.inflightReuse: " + uri)
		tokens := inflight.snapshot()
		if ctx.Err() != nil {
			return lsp.SemanticTokens{Data: []int{}}
		}
		return tokens
	}
	inflight := newSemanticTokensInflight()
	inflight.uri = uri
	inflight.document = doc
	inflight.version = version
	inflight.text = text
	inflight.generation = generation
	s.semanticInflight[inflightKey] = inflight
	s.mu.Unlock()
	if ctx.Err() != nil {
		s.abortSemanticTokensFlight(inflightKey, inflight)
		return lsp.SemanticTokens{Data: []int{}}
	}

	extra, complete := s.includedVBScriptSemanticDeclarationsContext(ctx, parsed)
	if !complete || ctx.Err() != nil {
		s.abortSemanticTokensFlight(inflightKey, inflight)
		return lsp.SemanticTokens{Data: []int{}}
	}
	resultID := semanticTokensResultID(uri, version)
	if deferLargeJavaScriptSemanticTokens(parsed) && refreshSupported {
		partial := vbscript.SemanticTokensWithoutJavaScriptWithExtraDeclarations(parsed, extra)
		if ctx.Err() != nil {
			s.abortSemanticTokensFlight(inflightKey, inflight)
			return lsp.SemanticTokens{Data: []int{}}
		}
		partial.ResultID = resultID + ":partial"
		s.mu.Lock()
		if s.semanticTokensFlightCurrentLocked(inflightKey, inflight) {
			s.semantic[uri] = semanticTokenCache{Version: version, Tokens: partial}
			s.semanticHistory[partial.ResultID] = append([]int(nil), partial.Data...)
			s.scheduleMemoryPressureCheckLocked("semanticTokens.store")
		}
		s.mu.Unlock()
		if ctx.Err() != nil {
			s.abortSemanticTokensFlight(inflightKey, inflight)
			return lsp.SemanticTokens{Data: []int{}}
		}
		inflight.setTokens(partial)
		if !inflight.active() {
			if ctx.Err() != nil {
				s.abortSemanticTokensFlight(inflightKey, inflight)
				return lsp.SemanticTokens{Data: []int{}}
			}
			return partial
		}
		if ctx.Err() != nil {
			s.abortSemanticTokensFlight(inflightKey, inflight)
			return lsp.SemanticTokens{Data: []int{}}
		}
		s.mu.Lock()
		if !s.semanticTokensFlightCurrentLocked(inflightKey, inflight) {
			s.mu.Unlock()
			s.abortSemanticTokensFlight(inflightKey, inflight)
			return lsp.SemanticTokens{Data: []int{}}
		}
		s.backgroundAnalysisWorkers.Add(1)
		s.mu.Unlock()
		go func() {
			defer s.backgroundAnalysisWorkers.Done()
			s.finishDeferredSemanticTokens(uri, version, parsed, extra, resultID, inflightKey, inflight)
		}()
		return partial
	}

	tokens := vbscript.SemanticTokensWithoutJavaScriptWithExtraDeclarations(parsed, extra)
	tokens.Data = mergeSemanticTokenData(tokens.Data, s.javaScriptSemanticTokensContext(ctx, uri))
	if ctx.Err() != nil {
		s.abortSemanticTokensFlight(inflightKey, inflight)
		return lsp.SemanticTokens{Data: []int{}}
	}
	tokens.ResultID = resultID
	returned := tokens
	if deferLargeJavaScriptSemanticTokens(parsed) {
		returned = vbscript.SemanticTokensWithoutJavaScriptWithExtraDeclarations(parsed, extra)
		returned.ResultID = resultID + ":partial"
	}
	s.mu.Lock()
	if s.semanticTokensFlightCurrentLocked(inflightKey, inflight) {
		s.semantic[uri] = semanticTokenCache{Version: version, Tokens: tokens}
		s.semanticHistory[tokens.ResultID] = append([]int(nil), tokens.Data...)
		if returned.ResultID != tokens.ResultID {
			s.semanticHistory[returned.ResultID] = append([]int(nil), returned.Data...)
		}
		s.scheduleMemoryPressureCheckLocked("semanticTokens.store")
	}
	s.removeSemanticTokensInflightLocked(inflightKey, inflight)
	s.mu.Unlock()
	if ctx.Err() != nil {
		s.abortSemanticTokensFlight(inflightKey, inflight)
		return lsp.SemanticTokens{Data: []int{}}
	}
	inflight.complete(returned)
	if ctx.Err() != nil {
		s.abortSemanticTokensFlight(inflightKey, inflight)
		return lsp.SemanticTokens{Data: []int{}}
	}
	return returned
}

func (s *Server) abortSemanticTokensFlight(inflightKey string, inflight *semanticTokensInflight) {
	if inflight == nil {
		return
	}
	inflight.invalidate()
	s.mu.Lock()
	current := s.semanticInflight[inflightKey]
	ownsRevision := (current == nil || current == inflight) && s.semanticTokensDocumentCurrentLocked(inflight)
	s.removeSemanticTokensInflightLocked(inflightKey, inflight)
	if ownsRevision && inflight.uri != "" {
		if cached, ok := s.semantic[inflight.uri]; ok && cached.Version == inflight.version {
			delete(s.semantic, inflight.uri)
		}
		prefix := semanticTokensResultID(inflight.uri, inflight.version)
		for resultID := range s.semanticHistory {
			if resultID == prefix || resultID == prefix+":partial" {
				delete(s.semanticHistory, resultID)
			}
		}
	}
	s.mu.Unlock()
}

func (s *Server) finishDeferredSemanticTokens(uri string, version int, parsed *core.ParsedDocument, extra map[string]string, resultID, inflightKey string, inflight *semanticTokensInflight) {
	ctx := inflight.context()
	if !inflight.active() || ctx.Err() != nil {
		if inflight.active() {
			inflight.invalidate()
		}
		s.mu.Lock()
		s.removeSemanticTokensInflightLocked(inflightKey, inflight)
		s.mu.Unlock()
		return
	}
	tokens := vbscript.SemanticTokensWithoutJavaScriptWithExtraDeclarations(parsed, extra)
	tokens.Data = mergeSemanticTokenData(tokens.Data, s.javaScriptSemanticTokensContext(ctx, uri))
	if !inflight.active() || ctx.Err() != nil {
		if inflight.active() {
			inflight.invalidate()
		}
		s.mu.Lock()
		s.removeSemanticTokensInflightLocked(inflightKey, inflight)
		s.mu.Unlock()
		return
	}
	tokens.ResultID = resultID
	s.mu.Lock()
	refreshSupported := s.semanticTokensRefreshSupported
	published := false
	notifyRefresh := false
	if s.semanticTokensFlightCurrentLocked(inflightKey, inflight) {
		s.semantic[uri] = semanticTokenCache{Version: version, Tokens: tokens}
		s.semanticHistory[tokens.ResultID] = append([]int(nil), tokens.Data...)
		s.scheduleMemoryPressureCheckLocked("semanticTokens.store")
		published = true
	}
	s.removeSemanticTokensInflightLocked(inflightKey, inflight)
	if published && refreshSupported {
		notifyRefresh = s.semanticTokensDocumentCurrentLocked(inflight)
	}
	s.mu.Unlock()
	inflight.complete(tokens)
	if notifyRefresh {
		s.reportAsyncRPCWriteError(s.sendNotification("workspace/semanticTokens/refresh", map[string]any{}))
	}
}

func deferLargeJavaScriptSemanticTokens(parsed *core.ParsedDocument) bool {
	if parsed == nil {
		return false
	}
	if sourceThreshold := largeSourceSemanticTokenThreshold(); sourceThreshold > 0 && len(parsed.Text) >= sourceThreshold {
		return true
	}
	threshold := largeJavaScriptSemanticTokenThreshold()
	if threshold <= 0 {
		return false
	}
	for _, region := range parsed.Regions {
		if region.Language != core.LanguageJavaScript && region.Language != core.LanguageJScript {
			continue
		}
		if region.ContentEnd-region.ContentStart >= threshold {
			return true
		}
	}
	return false
}

func largeJavaScriptSemanticTokenThreshold() int {
	raw := strings.TrimSpace(os.Getenv("ASP_LSP_TEST_SEMANTIC_TOKENS_LARGE_JAVASCRIPT_THRESHOLD"))
	if raw == "" {
		return 50 * 1024
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 50 * 1024
	}
	return value
}

func largeSourceSemanticTokenThreshold() int {
	raw := strings.TrimSpace(os.Getenv("ASP_LSP_TEST_SEMANTIC_TOKENS_LARGE_SOURCE_THRESHOLD"))
	if raw == "" {
		return 1024 * 1024
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 1024 * 1024
	}
	return value
}

func (s *Server) semanticTokensGenerationLocked(uri string) semanticTokensGeneration {
	return semanticTokensGeneration{
		parsedRevision:               s.parsedCacheRevisions[parsedDocumentCacheKey(uri)],
		javascriptDocumentGeneration: s.javascriptDocumentGeneration,
		javascriptMappingGeneration:  s.javascriptMappingGeneration,
	}
}

func (s *Server) semanticTokensFlightCurrentLocked(inflightKey string, inflight *semanticTokensInflight) bool {
	if inflight == nil || !inflight.active() || s.semanticInflight[inflightKey] != inflight || s.shutdown {
		return false
	}
	return s.semanticTokensDocumentCurrentLocked(inflight)
}

func (s *Server) semanticTokensDocumentCurrentLocked(inflight *semanticTokensInflight) bool {
	if inflight == nil || s.shutdown {
		return false
	}
	current := s.openDocumentByURILocked(inflight.uri)
	if current == nil || current != inflight.document || current.Version != inflight.version || current.Text != inflight.text {
		return false
	}
	return s.semanticTokensGenerationLocked(inflight.uri) == inflight.generation
}

func (s *Server) removeSemanticTokensInflightLocked(inflightKey string, inflight *semanticTokensInflight) {
	if s.semanticInflight[inflightKey] == inflight {
		delete(s.semanticInflight, inflightKey)
	}
}

func (s *Server) cancelStaleSemanticTokensLocked(uri string, document *core.TextDocument, generation semanticTokensGeneration) {
	for key, inflight := range s.semanticInflight {
		if inflight == nil || inflight.uri != uri {
			continue
		}
		if inflight.document == document && inflight.generation == generation {
			continue
		}
		inflight.invalidate()
		s.removeSemanticTokensInflightLocked(key, inflight)
	}
}

func (s *Server) clearSemanticTokenCache() {
	s.mu.Lock()
	for key, inflight := range s.semanticInflight {
		inflight.invalidate()
		delete(s.semanticInflight, key)
	}
	s.semanticInflight = map[string]*semanticTokensInflight{}
	s.semantic = map[string]semanticTokenCache{}
	s.semanticHistory = map[string][]int{}
	s.mu.Unlock()
}

func semanticTokensResultID(uri string, version int) string {
	return uri + "#" + strconv.Itoa(version)
}

func semanticTokenResultURI(resultID string) (string, bool) {
	index := strings.LastIndex(resultID, "#")
	if index <= 0 {
		return "", false
	}
	uri := resultID[:index]
	uri = strings.TrimSuffix(uri, ":partial")
	return uri, uri != ""
}

func semanticTokenDeltaEdits(previous, next []int) []any {
	prefix := 0
	for prefix < len(previous) && prefix < len(next) && previous[prefix] == next[prefix] {
		prefix++
	}
	previousEnd := len(previous)
	nextEnd := len(next)
	for previousEnd > prefix && nextEnd > prefix && previous[previousEnd-1] == next[nextEnd-1] {
		previousEnd--
		nextEnd--
	}
	if prefix == previousEnd && prefix == nextEnd {
		return []any{}
	}
	data := append([]int(nil), next[prefix:nextEnd]...)
	if data == nil {
		data = []int{}
	}
	return []any{map[string]any{
		"start":       prefix,
		"deleteCount": previousEnd - prefix,
		"data":        data,
	}}
}

func (s *Server) semanticTokensRange(uri string, r lsp.Range) lsp.SemanticTokens {
	return s.semanticTokensRangeContext(context.Background(), uri, r)
}

func (s *Server) semanticTokensRangeContext(ctx context.Context, uri string, r lsp.Range) lsp.SemanticTokens {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return lsp.SemanticTokens{Data: []int{}}
	}
	doc, parsed := s.openParsed(uri)
	if doc == nil || parsed == nil || ctx.Err() != nil {
		return lsp.SemanticTokens{Data: []int{}}
	}
	extra, complete := s.includedVBScriptSemanticDeclarationsContext(ctx, parsed)
	if !complete || ctx.Err() != nil {
		return lsp.SemanticTokens{Data: []int{}}
	}
	tokens := vbscript.SemanticTokensRangeWithExtraDeclarations(parsed, r, extra)
	base := decodeAbsoluteSemanticTokenData(tokens.Data)
	filtered := base[:0]
	for _, token := range base {
		if ctx.Err() != nil {
			return lsp.SemanticTokens{Data: []int{}}
		}
		position := lsp.Position{Line: token.line, Character: token.character}
		region := core.RegionAt(parsed, doc.OffsetAt(position))
		if region == nil || (region.Language != core.LanguageJavaScript && region.Language != core.LanguageJScript) {
			filtered = append(filtered, token)
		}
	}
	javaScriptExtra := s.javaScriptSemanticTokensContext(ctx, uri)
	if ctx.Err() != nil {
		return lsp.SemanticTokens{Data: []int{}}
	}
	filteredExtra := javaScriptExtra[:0]
	for _, token := range javaScriptExtra {
		if ctx.Err() != nil {
			return lsp.SemanticTokens{Data: []int{}}
		}
		position := lsp.Position{Line: token.line, Character: token.character}
		if compareLSPPositions(position, r.Start) >= 0 && compareLSPPositions(position, r.End) <= 0 {
			filteredExtra = append(filteredExtra, token)
		}
	}
	tokens.Data = mergeSemanticTokenData(mergeSemanticTokenData(nil, filtered), filteredExtra)
	if ctx.Err() != nil {
		return lsp.SemanticTokens{Data: []int{}}
	}
	return tokens
}

func (s *Server) openParsed(uri string) (*core.TextDocument, *core.ParsedDocument) {
	s.mu.Lock()
	document := s.openDocumentByURILocked(uri)
	s.mu.Unlock()
	if document == nil {
		return nil, nil
	}
	return document, s.parseTextDocument(document, s.settings.DefaultLanguage)
}

func (s *Server) includedVBScriptSemanticDeclarationsContext(ctx context.Context, parsed *core.ParsedDocument) (map[string]string, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	documents, complete := s.includedDocumentsContextResult(ctx, parsed)
	if !complete || ctx.Err() != nil {
		return nil, false
	}
	extra := map[string]string{}
	for _, included := range documents {
		if ctx.Err() != nil {
			return nil, false
		}
		for _, signature := range vbscript.Signatures(included) {
			if ctx.Err() != nil {
				return nil, false
			}
			extra[strings.ToLower(signature.Name)] = signature.Kind
		}
	}
	if ctx.Err() != nil {
		return nil, false
	}
	return extra, true
}

func (s *Server) inlayHints(uri string, r lsp.Range) []lsp.InlayHint {
	return s.inlayHintsContext(context.Background(), uri, r)
}

func (s *Server) inlayHintsContext(ctx context.Context, uri string, r lsp.Range) []lsp.InlayHint {
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
	s.mu.Lock()
	parameterNames := s.settings.InlayParameterNames
	implicitByRef := s.settings.InlayImplicitByRef
	functionReturnTypes := s.settings.InlayFunctionReturnTypes
	variableTypes := s.settings.InlayVariableTypes
	scopeMarkers := s.settings.InlayScopeMarkers
	s.mu.Unlock()
	if ctx.Err() != nil {
		return nil
	}
	included, complete := s.includedDocumentsContextResult(ctx, parsed)
	if !complete || ctx.Err() != nil {
		return nil
	}
	includeAware := len(included) > 0
	includedGlobalNames := includedVBVariableInlayGlobalNamesContext(ctx, included)
	if ctx.Err() != nil {
		return nil
	}
	hints := vbscript.InlayHintsWithOptions(parsed, r, vbscript.InlayHintOptions{ImplicitByRef: implicitByRef, ParameterNames: parameterNames})
	if ctx.Err() != nil {
		return nil
	}
	hints = append(hints, vbscriptFunctionReturnTypeInlayHints(parsed, r, functionReturnTypes)...)
	if ctx.Err() != nil {
		return nil
	}
	hints = append(hints, vbscriptVariableTypeInlayHints(parsed, r, vbscriptVariableTypeInlayOptions{VariableTypes: variableTypes, ScopeMarkers: scopeMarkers, IncludeAware: includeAware, IncludedGlobalNames: includedGlobalNames})...)
	if ctx.Err() != nil {
		return nil
	}
	for _, region := range parsed.Regions {
		if ctx.Err() != nil {
			return nil
		}
		if region.Language != core.LanguageJavaScript && region.Language != core.LanguageJScript {
			continue
		}
		regionRange := doc.Range(region.ContentStart, region.ContentEnd)
		if !lspRangesOverlap(regionRange, r) {
			continue
		}
		queryRange := intersectLSPRange(regionRange, r)
		var javaScriptHints []lsp.InlayHint
		if s.javaScriptLanguageServiceRequest(ctx, uri, queryRange.Start, "textDocument/inlayHint", map[string]any{"range": queryRange}, &javaScriptHints) {
			if ctx.Err() != nil {
				return nil
			}
			hints = append(hints, javaScriptHints...)
		}
		if ctx.Err() != nil {
			return nil
		}
	}
	return hints
}

func intersectLSPRange(left, right lsp.Range) lsp.Range {
	start := left.Start
	if compareLSPPositions(right.Start, start) > 0 {
		start = right.Start
	}
	end := left.End
	if compareLSPPositions(right.End, end) < 0 {
		end = right.End
	}
	return lsp.Range{Start: start, End: end}
}

func (s *Server) logDocumentOpenPrewarm(uri string) {
	_, parsed := s.parsed(uri)
	if parsed == nil {
		return
	}
	s.mu.Lock()
	checkJS := s.settings.CheckJS
	s.mu.Unlock()
	if checkJS && parsedHasJavaScript(parsed) {
		s.logDebugSummary("[asp-lsp] javascript.diagnostics.prewarm.completed: " + uri)
	}
	if len(parsed.Includes) > 0 {
		included, complete := s.includedDocumentsContextResult(context.Background(), parsed)
		if complete && len(included) > 0 {
			s.logDebugVerbose("[asp-lsp] vbProject.context.refresh.completed: " + uri + ", reason=document.open.prewarm")
		}
	}
}

func parsedHasJavaScript(parsed *core.ParsedDocument) bool {
	if parsed == nil {
		return false
	}
	for _, region := range parsed.Regions {
		if region.Language == core.LanguageJavaScript || region.Language == core.LanguageJScript {
			return true
		}
	}
	return false
}

func javaScriptWorkerPayloadBytes(parsed *core.ParsedDocument) int {
	if parsed == nil {
		return 0
	}
	payloadBytes := 0
	for _, language := range []core.EmbeddedLanguage{core.LanguageJavaScript, core.LanguageJScript} {
		virtual := core.BuildVirtualDocument(parsed, language)
		payloadBytes += len(virtual.Text)
	}
	return payloadBytes
}

func documentTextHasJavaScript(text string) bool {
	return strings.Contains(strings.ToLower(text), "<script")
}

func isJavaScriptProjectFile(path string) bool {
	normalized := filepath.ToSlash(filepath.Clean(strings.ReplaceAll(path, "\\", "/")))
	if isJavaScriptProjectConfigFile(normalized) {
		return true
	}
	if strings.Contains(strings.ToLower(normalized), "/node_modules/@types/") {
		return true
	}
	return isJavaScriptModuleFile(normalized)
}

func isJavaScriptProjectConfigFile(path string) bool {
	base := strings.ToLower(filepath.Base(filepath.ToSlash(filepath.Clean(strings.ReplaceAll(path, "\\", "/")))))
	return base == "jsconfig.json" || base == "tsconfig.json" || base == "package.json"
}

var includePublicDeclarationPattern = regexp.MustCompile(`(?im)^\s*(?:Public\s+|Private\s+)?(?:Function|Sub|Class)\s+([A-Za-z_][A-Za-z0-9_]*)\b`)
var includeDimDeclarationPattern = regexp.MustCompile(`(?im)^\s*Dim\s+([A-Za-z_][A-Za-z0-9_]*)\b`)
var includeAssignmentPattern = regexp.MustCompile(`(?im)^\s*([A-Za-z_][A-Za-z0-9_]*)\s*=`)
var includeDirectiveFingerprintPattern = regexp.MustCompile(`(?is)<!--\s*#include\s+(?:file|virtual)\s*=\s*["']([^"']+)["']\s*-->`)

func includePublicBoundaryFingerprint(text string) string {
	names := map[string]struct{}{}
	for _, match := range includePublicDeclarationPattern.FindAllStringSubmatch(text, -1) {
		if len(match) == 2 {
			names["decl:"+strings.ToLower(match[1])] = struct{}{}
		}
	}
	for _, match := range includeDirectiveFingerprintPattern.FindAllStringSubmatch(text, -1) {
		if len(match) == 2 {
			names["include:"+strings.ToLower(match[1])] = struct{}{}
		}
	}
	dimNames := map[string]struct{}{}
	for _, match := range includeDimDeclarationPattern.FindAllStringSubmatch(text, -1) {
		if len(match) == 2 {
			dimNames[strings.ToLower(match[1])] = struct{}{}
		}
	}
	for _, match := range includeAssignmentPattern.FindAllStringSubmatch(text, -1) {
		if len(match) != 2 {
			continue
		}
		name := strings.ToLower(match[1])
		if _, declared := dimNames[name]; declared {
			continue
		}
		names["implicit:"+name] = struct{}{}
	}
	parts := make([]string, 0, len(names))
	for name := range names {
		parts = append(parts, name)
	}
	sort.Strings(parts)
	return strings.Join(parts, "\n")
}
