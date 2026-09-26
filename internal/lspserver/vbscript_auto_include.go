package lspserver

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

type vbscriptAutoIncludeCompletionData struct {
	Kind      string `json:"kind"`
	OwnerURI  string `json:"ownerUri"`
	TargetURI string `json:"targetUri"`
	Name      string `json:"name"`
}

func (s *Server) vbscriptAutoIncludeCompletions(parsed *core.ParsedDocument, offset int) ([]lsp.CompletionItem, bool) {
	snapshot := s.workspaceVBAutoIncludeSnapshot()
	if !snapshot.Complete {
		return nil, true
	}
	if parsed == nil || isStandaloneVBScriptDocument(parsed) {
		return nil, false
	}
	start := offset
	for start > 0 && isCompletionIdentifier(parsed.Text[start-1]) {
		start--
	}
	prefix := parsed.Text[start:offset]
	exports := snapshot.ExportsForPrefix(prefix)
	allowedTargets := s.vbscriptAutoIncludeCompletionTargetsAllowed(parsed.URI, exports)
	items := make([]lsp.CompletionItem, 0, len(exports))
	for _, export := range exports {
		if !allowedTargets[workspacepkg.SourceURIIdentityKey(export.URI)] {
			continue
		}
		path := s.vbscriptAutoIncludeDisplayPath(parsed.URI, export.URI)
		items = append(items, lsp.CompletionItem{
			Label:    export.Name,
			Kind:     vbscriptCompletionKind(export.Kind),
			Detail:   s.autoIncludeCompletionDetail(path),
			SortText: "zz-auto-include-" + strings.ToLower(export.Name) + "-" + strings.ToLower(path),
			Data: vbscriptAutoIncludeCompletionData{
				Kind:      "vbscript-auto-include",
				OwnerURI:  parsed.URI,
				TargetURI: export.URI,
				Name:      export.Name,
			},
		})
	}
	return items, false
}

func mergeVBScriptAutoIncludeCompletions(items, autoIncludes []lsp.CompletionItem) []lsp.CompletionItem {
	visible := dedupeCompletionItems(items)
	visibleLabels := make(map[string]struct{}, len(visible))
	for _, item := range visible {
		visibleLabels[strings.ToLower(item.Label)] = struct{}{}
	}
	result := append([]lsp.CompletionItem(nil), visible...)
	seenAutoIncludes := map[string]struct{}{}
	for _, item := range autoIncludes {
		label := strings.ToLower(item.Label)
		if _, ok := visibleLabels[label]; ok {
			continue
		}
		var data vbscriptAutoIncludeCompletionData
		if remarshal(item.Data, &data) != nil {
			continue
		}
		key := label + "\x00" + workspacepkg.SourceURIIdentityKey(data.TargetURI)
		if _, ok := seenAutoIncludes[key]; ok {
			continue
		}
		seenAutoIncludes[key] = struct{}{}
		result = append(result, item)
	}
	return result
}

func (s *Server) resolveVBScriptAutoIncludeCompletionItem(item lsp.CompletionItem) (lsp.CompletionItem, bool) {
	var data vbscriptAutoIncludeCompletionData
	if remarshal(item.Data, &data) != nil || data.Kind != "vbscript-auto-include" {
		return item, false
	}
	if !s.settings.VBScriptAutoIncludes {
		return item, true
	}
	_, owner := s.parsed(data.OwnerURI)
	if owner == nil || !s.vbscriptAutoIncludeTopologyAllows(owner.URI, data.TargetURI) {
		return item, true
	}
	current := false
	for _, export := range s.workspaceVBAutoIncludeSnapshot().Exports(data.Name) {
		if strings.EqualFold(export.Name, data.Name) &&
			workspacepkg.SameFileIdentityURI(export.URI, data.TargetURI) {
			current = true
			break
		}
	}
	if !current {
		return item, true
	}
	edit, err := s.vbscriptAutoIncludeTextEdit(owner, data.TargetURI)
	if err != nil {
		return item, true
	}
	item.AdditionalTextEdits = append(item.AdditionalTextEdits, edit)
	item.Documentation = appendCompletionDocumentation(item.Documentation, s.vbscriptDefinedInNote(data.TargetURI))
	return item, true
}

func (s *Server) vbscriptAutoIncludeCodeActions(params codeActionParams) []lsp.CodeAction {
	if !s.settings.VBScriptAutoIncludes || !codeActionAllows(params.Context.Only, "quickfix") {
		return nil
	}
	doc, parsed := s.parsed(params.TextDocument.URI)
	if doc == nil || parsed == nil || isStandaloneVBScriptDocument(parsed) {
		return nil
	}
	offset := doc.OffsetAt(params.Range.Start)
	region := core.RegionAt(parsed, offset)
	if region == nil || region.Language != core.LanguageVBScript {
		return nil
	}

	diagnosticsByName := map[string][]lsp.Diagnostic{}
	for _, diagnostic := range params.Context.Diagnostics {
		name := ""
		recognized := false
		switch {
		case diagnostic.Source == "asp-lsp-vbscript":
			recognized = true
			name = diagnosticDataString(diagnostic, "name")
		case diagnostic.Source == vbscriptTypeDiagnosticSource && diagnosticCodeString(diagnostic) == "unknownCall":
			recognized = true
			name = diagnosticDataString(diagnostic, "name")
		}
		if !recognized {
			continue
		}
		if name == "" {
			name = strings.TrimSpace(textInRange(doc, diagnostic.Range))
		}
		if name == "" || strings.Contains(name, ".") {
			continue
		}
		lower := strings.ToLower(name)
		diagnosticsByName[lower] = append(diagnosticsByName[lower], diagnostic)
	}

	names := make([]string, 0, len(diagnosticsByName)+1)
	for name := range diagnosticsByName {
		names = append(names, name)
	}
	selectedName := vbscript.WordAt(parsed.Text, offset)
	if selectedName != "" && !vbscriptAutoIncludeMemberIdentifier(parsed.Text, offset) &&
		!isDeclaredVBBuiltinOrKeywordForDocument(parsed, strings.ToLower(selectedName)) {
		names = append(names, selectedName)
	}
	sort.SliceStable(names, func(i, j int) bool {
		return strings.ToLower(names[i]) < strings.ToLower(names[j])
	})

	visibleNames := map[string]struct{}{}
	for _, completion := range s.vbscriptSymbolCompletions(parsed, offset) {
		var data vbscriptCompletionData
		if remarshal(completion.Data, &data) == nil && data.Kind == "vbscript-symbol" {
			visibleNames[strings.ToLower(completion.Label)] = struct{}{}
		}
	}

	seenNames := map[string]struct{}{}
	seenActions := map[string]struct{}{}
	var actions []lsp.CodeAction
	snapshot := s.workspaceVBAutoIncludeSnapshot()
	if !snapshot.Complete {
		return nil
	}
	for _, name := range names {
		lower := strings.ToLower(name)
		if _, seen := seenNames[lower]; seen {
			continue
		}
		seenNames[lower] = struct{}{}
		if _, visible := visibleNames[lower]; visible {
			continue
		}
		for _, export := range snapshot.Exports(name) {
			if !s.vbscriptAutoIncludeTopologyAllows(parsed.URI, export.URI) {
				continue
			}
			edit, err := s.vbscriptAutoIncludeTextEdit(parsed, export.URI)
			if err != nil {
				continue
			}
			key := lower + "\x00" + workspacepkg.SourceURIIdentityKey(export.URI)
			if _, seen := seenActions[key]; seen {
				continue
			}
			seenActions[key] = struct{}{}
			path := s.vbscriptAutoIncludeDisplayPath(parsed.URI, export.URI)
			action := lsp.CodeAction{
				Title: s.autoIncludeCodeActionTitle(export.Name, path),
				Kind:  "quickfix",
				Edit: &lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{
					parsed.URI: {edit},
				}},
			}
			if diagnostics := diagnosticsByName[lower]; len(diagnostics) > 0 {
				action.Diagnostics = append([]lsp.Diagnostic(nil), diagnostics...)
			}
			actions = append(actions, action)
		}
	}
	return actions
}

func (s *Server) vbscriptAutoIncludeTopologyAllows(ownerURI, targetURI string) bool {
	ownerPath := fileURIPath(ownerURI)
	targetPath := fileURIPath(targetURI)
	if ownerPath == "" || targetPath == "" ||
		workspacepkg.SameFileIdentityURI(ownerURI, targetURI) {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	graph := s.workspaceIncludeGraph
	if graph == nil || !s.workspaceIncludeGraphComplete {
		return false
	}
	if graph.DependsOnAnyTarget(ownerPath, []string{targetPath}, true) {
		return false
	}
	return !graph.DependsOnAnyTarget(targetPath, []string{ownerPath}, true)
}

func (s *Server) vbscriptAutoIncludeCompletionTargetsAllowed(ownerURI string, exports []WorkspaceVBAutoIncludeExport) map[string]bool {
	allowed := make(map[string]bool, len(exports))
	ownerPath := fileURIPath(ownerURI)
	if ownerPath == "" {
		return allowed
	}
	targetPaths := make([]string, len(exports))
	for index, export := range exports {
		targetPaths[index] = fileURIPath(export.URI)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	graph := s.workspaceIncludeGraph
	if graph == nil || !s.workspaceIncludeGraphComplete {
		return allowed
	}
	for index, export := range exports {
		targetPath := targetPaths[index]
		if targetPath == "" || workspacepkg.SameFileIdentityURI(ownerURI, export.URI) ||
			graph.DependsOnAnyTarget(ownerPath, []string{targetPath}, true) ||
			graph.DependsOnAnyTarget(targetPath, []string{ownerPath}, true) {
			continue
		}
		allowed[workspacepkg.SourceURIIdentityKey(export.URI)] = true
	}
	return allowed
}

func (s *Server) vbscriptAutoIncludeDisplayPath(ownerURI, targetURI string) string {
	ownerPath := fileURIPath(ownerURI)
	targetPath := fileURIPath(targetURI)
	if ownerPath == "" || targetPath == "" {
		return targetURI
	}
	path, err := filepath.Rel(filepath.Dir(ownerPath), targetPath)
	if err != nil || path == "" || filepath.IsAbs(path) {
		return filepath.ToSlash(targetPath)
	}
	return filepath.ToSlash(path)
}

func vbscriptAutoIncludeMemberIdentifier(text string, offset int) bool {
	if offset < 0 {
		return false
	}
	if offset > len(text) {
		offset = len(text)
	}
	start := offset
	if start == len(text) || start < len(text) && !isCompletionIdentifier(text[start]) {
		start--
	}
	for start >= 0 && isCompletionIdentifier(text[start]) {
		start--
	}
	for start >= 0 {
		switch text[start] {
		case ' ', '\t':
			start--
			continue
		default:
			return text[start] == '.'
		}
	}
	return false
}
