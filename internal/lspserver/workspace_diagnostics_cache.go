package lspserver

import (
	"encoding/json"
	"hash/fnv"
	"path/filepath"
	"strconv"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

type workspaceDiagnosticsItemCacheEntry struct {
	document           *core.TextDocument
	version            int
	dependencyRevision uint64
	settingsKey        string
	diagnostics        []lsp.Diagnostic
}

// workspaceDiagnosticsPassState records the last completed workspace/diagnostic
// pass. VS Code pulls workspace diagnostics again two seconds after every
// response, so an unchanged workspace must not be re-checked on each pull.
type workspaceDiagnosticsPassState struct {
	epoch       uint64
	settingsKey string
	// closedResultIDs maps file identity keys of closed documents to the
	// result IDs sent for them. Clients omit result IDs for documents they pull
	// individually, so open documents are not compared.
	closedResultIDs map[string]string
}

// markWorkspaceDiagnosticsChanged makes the next workspace/diagnostic pull run
// a full pass instead of reporting the previous results as current.
func (s *Server) markWorkspaceDiagnosticsChanged() {
	s.workspaceDiagnosticsEpoch.Add(1)
}

func workspaceDiagnosticsResultID(diagnostics []lsp.Diagnostic) string {
	payload, _ := json.Marshal(diagnostics)
	hash := fnv.New64a()
	_, _ = hash.Write(payload)
	return strconv.FormatUint(hash.Sum64(), 36)
}

// workspaceDiagnosticsPreviousResultIDs keys the client's result IDs by file
// identity because VS Code re-encodes URIs (for example a lower-case, escaped
// Windows drive letter).
func workspaceDiagnosticsPreviousResultIDs(params json.RawMessage) map[string]string {
	var request struct {
		PreviousResultIDs []struct {
			URI   string `json:"uri"`
			Value string `json:"value"`
		} `json:"previousResultIds"`
	}
	if len(params) == 0 || json.Unmarshal(params, &request) != nil || len(request.PreviousResultIDs) == 0 {
		return nil
	}
	previous := make(map[string]string, len(request.PreviousResultIDs))
	for _, item := range request.PreviousResultIDs {
		if item.URI != "" && item.Value != "" {
			previous[workspacepkg.FileIdentityKeyFromURI(item.URI)] = item.Value
		}
	}
	return previous
}

// workspaceDiagnosticsPassCurrent reports whether the client already holds the
// results of a completed pass and nothing has changed since that pass started.
func (s *Server) workspaceDiagnosticsPassCurrent(epoch uint64, settingsKey string, previous map[string]string) bool {
	if len(previous) == 0 {
		return false
	}
	s.mu.Lock()
	last := s.workspaceDiagnosticsLastPass
	s.mu.Unlock()
	if last.closedResultIDs == nil || last.epoch != epoch || last.settingsKey != settingsKey {
		return false
	}
	for key, resultID := range last.closedResultIDs {
		if previous[key] != resultID {
			return false
		}
	}
	return true
}

func (s *Server) workspaceDiagnosticsSettingsFingerprint() string {
	s.mu.Lock()
	settings := s.settings
	s.mu.Unlock()
	payload, _ := json.Marshal(map[string]any{
		"checkJs":                           settings.CheckJS,
		"defaultLanguage":                   settings.DefaultLanguage,
		"includePaths":                      settings.IncludePaths,
		"javascriptCompilerOptions":         settings.JavaScriptCompilerOptions,
		"javascriptCompilerOptionTypes":     settings.JavaScriptCompilerOptionTypes,
		"javascriptIgnoreProjectConfig":     settings.JavaScriptIgnoreProjectConfig,
		"javascriptUnusedDiagnostics":       settings.JavaScriptUnusedDiagnostics,
		"locale":                            settings.Locale,
		"vbscriptDeadCodeDiagnostics":       settings.VBScriptDeadCodeDiagnostics,
		"vbscriptGlobals":                   settings.VBScriptGlobals,
		"vbscriptIfSyntaxDiagnostics":       settings.VBScriptIfSyntaxDiagnostics,
		"vbscriptSqlInjectionDiagnostics":   settings.VBScriptSQLInjectionDiagnostics,
		"vbscriptTypeChecking":              settings.VBScriptTypeChecking,
		"vbscriptUnusedDiagnostics":         settings.VBScriptUnusedDiagnostics,
		"vbscriptImplicitGlobalDiagnostics": settings.VBScriptImplicitGlobalDiagnostics,
	})
	return workspacepkg.DiskContentHash(string(payload))
}

func (s *Server) cachedWorkspaceDiagnosticsItem(uri string, document *core.TextDocument, settingsKey string) ([]lsp.Diagnostic, bool) {
	key := workspacepkg.FileIdentityKeyFromURI(uri)
	s.mu.Lock()
	entry, ok := s.workspaceDiagnosticsItems[key]
	revision := s.workspaceDiagnosticsRevisions[key]
	s.mu.Unlock()
	if !ok || document == nil || entry.document != document || entry.version != document.Version || entry.dependencyRevision != revision || entry.settingsKey != settingsKey {
		return nil, false
	}
	return entry.diagnostics, true
}

func (s *Server) rememberWorkspaceDiagnosticsItem(uri string, document *core.TextDocument, settingsKey string, diagnostics []lsp.Diagnostic) bool {
	if document == nil {
		return false
	}
	key := workspacepkg.FileIdentityKeyFromURI(uri)
	s.mu.Lock()
	current := s.openDocumentByURILocked(uri)
	if current == nil {
		current = s.workspaceDocumentByURILocked(uri)
	}
	if current != document || current.Version != document.Version {
		s.mu.Unlock()
		return false
	}
	s.workspaceDiagnosticsItems[key] = workspaceDiagnosticsItemCacheEntry{
		document: document, version: document.Version, dependencyRevision: s.workspaceDiagnosticsRevisions[key],
		settingsKey: settingsKey, diagnostics: diagnostics,
	}
	s.mu.Unlock()
	s.rememberWorkspaceDocumentDiagnosticArtifact(uri, diagnostics)
	return true
}

func (s *Server) rememberWorkspaceDocumentDiagnosticArtifact(uri string, diagnostics []lsp.Diagnostic) {
	documentID := workspaceDocumentIDFromURI(uri)
	s.mu.Lock()
	previous := s.workspaceArtifacts[documentID]
	if previous == nil {
		s.mu.Unlock()
		return
	}
	current := workspaceDocumentArtifactManifestWithDiagnostics(previous, map[string][]lsp.Diagnostic{"combined": diagnostics})
	delta := compareWorkspaceDocumentArtifacts(previous, current)
	if len(delta.ChangedDiagnosticLayers) == 0 {
		s.mu.Unlock()
		return
	}
	s.workspaceArtifacts[documentID] = current
	s.workspaceArtifactRevisions[documentID]++
	s.mu.Unlock()
	s.queueWorkspaceDocumentArtifactDelta(current, delta)
}

func (s *Server) invalidateWorkspaceDiagnosticsPaths(paths map[string]struct{}) {
	if len(paths) == 0 {
		return
	}
	s.mu.Lock()
	for path := range paths {
		key := workspacepkg.FileIdentityKeyFromFileName(filepath.Clean(path))
		s.workspaceDiagnosticsRevisions[key]++
		delete(s.workspaceDiagnosticsItems, key)
	}
	s.workspaceDiagnosticsProcessCache = false
	s.mu.Unlock()
	s.markWorkspaceDiagnosticsChanged()
}

func (s *Server) invalidateWorkspaceDiagnosticsURI(uri string) {
	if path := fileURIPath(uri); path != "" {
		s.invalidateWorkspaceDiagnosticsPaths(map[string]struct{}{filepath.Clean(path): {}})
		return
	}
	key := workspacepkg.FileIdentityKeyFromURI(uri)
	s.mu.Lock()
	s.workspaceDiagnosticsRevisions[key]++
	delete(s.workspaceDiagnosticsItems, key)
	s.workspaceDiagnosticsProcessCache = false
	s.mu.Unlock()
	s.markWorkspaceDiagnosticsChanged()
}

func (s *Server) invalidateWorkspaceDiagnosticsForParsedChange(previous, current *core.ParsedDocument) {
	if current == nil {
		return
	}
	path := cleanFileURIPath(current.URI)
	if path == "" {
		s.invalidateWorkspaceDiagnosticsURI(current.URI)
		return
	}
	affected := map[string]struct{}{filepath.Clean(path): {}}
	if previous == nil || includePublicBoundaryFingerprint(previous.Text) != includePublicBoundaryFingerprint(current.Text) {
		if dependents, ready := s.includeInvalidationPaths(affected); ready {
			affected = dependents
		}
	}
	s.invalidateWorkspaceDiagnosticsPaths(affected)
}
