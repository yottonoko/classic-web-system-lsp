package lspserver

import (
	"reflect"
	"slices"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

// workspaceURIIndex maps file identity keys to the s.workspace URIs that share
// them. Lookups with a differently encoded URI (a lower-case, escaped Windows
// drive letter or a case-mismatched include path) used to parse every
// workspace URI while holding s.mu.
//
// The index is rebuilt when s.workspace is replaced or its size no longer
// matches, and the helpers below keep it current for in-place changes.
type workspaceURIIndex struct {
	workspace  uintptr
	size       int
	byIdentity map[string][]string
}

func (s *Server) workspaceURIIndexCurrentLocked() bool {
	index := &s.workspaceURIs
	return index.byIdentity != nil && index.workspace == reflect.ValueOf(s.workspace).Pointer() && index.size == len(s.workspace)
}

// workspaceURIsForIdentityLocked returns the workspace URIs with the same file
// identity as uri. Callers must not modify the result.
func (s *Server) workspaceURIsForIdentityLocked(uri string) []string {
	if !s.workspaceURIIndexCurrentLocked() {
		byIdentity := make(map[string][]string, len(s.workspace))
		for candidate := range s.workspace {
			key := workspacepkg.FileIdentityKeyFromURI(candidate)
			byIdentity[key] = append(byIdentity[key], candidate)
		}
		s.workspaceURIs = workspaceURIIndex{workspace: reflect.ValueOf(s.workspace).Pointer(), size: len(s.workspace), byIdentity: byIdentity}
	}
	return s.workspaceURIs.byIdentity[workspacepkg.FileIdentityKeyFromURI(uri)]
}

func (s *Server) setWorkspaceDocumentLocked(uri string, doc *core.TextDocument) {
	current := s.workspaceURIIndexCurrentLocked()
	_, existed := s.workspace[uri]
	s.workspace[uri] = doc
	if !current || existed {
		return
	}
	key := workspacepkg.FileIdentityKeyFromURI(uri)
	s.workspaceURIs.byIdentity[key] = append(slices.Clone(s.workspaceURIs.byIdentity[key]), uri)
	s.workspaceURIs.size = len(s.workspace)
}

func (s *Server) deleteWorkspaceDocumentLocked(uri string) {
	if _, existed := s.workspace[uri]; !existed {
		return
	}
	current := s.workspaceURIIndexCurrentLocked()
	delete(s.workspace, uri)
	if !current {
		return
	}
	key := workspacepkg.FileIdentityKeyFromURI(uri)
	remaining := slices.DeleteFunc(slices.Clone(s.workspaceURIs.byIdentity[key]), func(candidate string) bool { return candidate == uri })
	if len(remaining) == 0 {
		delete(s.workspaceURIs.byIdentity, key)
	} else {
		s.workspaceURIs.byIdentity[key] = remaining
	}
	s.workspaceURIs.size = len(s.workspace)
}

// deleteWorkspaceDocumentsWithIdentityLocked removes every workspace URI that
// names the same file as uri.
func (s *Server) deleteWorkspaceDocumentsWithIdentityLocked(uri string) {
	for _, candidate := range slices.Clone(s.workspaceURIsForIdentityLocked(uri)) {
		s.deleteWorkspaceDocumentLocked(candidate)
	}
}

// workspaceDocumentWithIdentityLocked returns the first non-nil workspace
// document that names the same file as uri.
func (s *Server) workspaceDocumentWithIdentityLocked(uri string) *core.TextDocument {
	for _, candidate := range s.workspaceURIsForIdentityLocked(uri) {
		if doc := s.workspace[candidate]; doc != nil {
			return doc
		}
	}
	return nil
}
