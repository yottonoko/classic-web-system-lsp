package lspserver

import (
	"path/filepath"
	"sort"
	"strings"

	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

type workspaceRoot struct {
	URI  string
	Path string
}

func workspaceRootsFromInitializeParams(params initializeParams) []workspaceRoot {
	roots := make([]workspaceRoot, 0, len(params.WorkspaceFolders)+2)
	seen := map[string]struct{}{}
	addPath := func(path string) {
		if path == "" {
			return
		}
		cleanPath := filepath.Clean(path)
		uri := filePathURI(cleanPath)
		key := workspacepkg.FileIdentityKeyFromURI(uri)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		roots = append(roots, workspaceRoot{URI: uri, Path: cleanPath})
	}
	addURI := func(uri string) {
		path := fileURIPath(uri)
		if path == "" {
			return
		}
		addPath(path)
	}
	for _, folder := range params.WorkspaceFolders {
		addURI(folder.URI)
	}
	if params.RootURI != "" {
		addURI(params.RootURI)
	}
	if params.RootPath != "" {
		addPath(params.RootPath)
	}
	return roots
}

func (s *Server) workspaceRootsSnapshot() []workspaceRoot {
	s.mu.Lock()
	defer s.mu.Unlock()
	roots := append([]workspaceRoot(nil), s.workspaceRoots...)
	if len(roots) == 0 && s.rootPath != "" {
		roots = []workspaceRoot{{URI: s.rootURI, Path: s.rootPath}}
	}
	if len(roots) == 0 {
		seen := map[string]struct{}{}
		for _, document := range s.documents {
			if document == nil || !strings.HasPrefix(strings.ToLower(document.URI), "file:") {
				continue
			}
			path := fileURIPath(document.URI)
			if path == "" {
				continue
			}
			rootPath := filepath.Dir(filepath.Clean(path))
			key := workspacepkg.FileIdentityKeyFromURI(filePathURI(rootPath))
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			roots = append(roots, workspaceRoot{URI: filePathURI(rootPath), Path: rootPath})
		}
		sort.Slice(roots, func(i, j int) bool { return roots[i].Path < roots[j].Path })
	}
	return roots
}

func (s *Server) gitIgnoreGlobsByWorkspaceRoot(roots []workspaceRoot, respectGitIgnore bool) map[string][]string {
	globs := make(map[string][]string, len(roots))
	if !respectGitIgnore {
		return globs
	}
	for _, root := range roots {
		if root.Path == "" {
			continue
		}
		globs[root.Path] = s.readGitIgnoreGlobs(root.Path)
	}
	return globs
}

func workspaceGraphURIAllowedInRoots(uri string, roots []workspaceRoot, includeGlobs, excludeGlobs []string, gitIgnoreGlobs map[string][]string) bool {
	path := fileURIPath(uri)
	if path == "" {
		return false
	}
	return workspaceGraphPathAllowedInRoots(path, roots, includeGlobs, excludeGlobs, gitIgnoreGlobs)
}

func workspaceGraphURIAllowedInRootsWithFilter(uri string, roots []workspaceRoot, filter workspaceGraphFileFilter) bool {
	path := fileURIPath(uri)
	if path == "" {
		return false
	}
	return workspaceGraphPathAllowedInRootsWithFilter(path, roots, filter)
}

func workspaceGraphPathAllowedInRoots(path string, roots []workspaceRoot, includeGlobs, excludeGlobs []string, gitIgnoreGlobs map[string][]string) bool {
	for _, root := range roots {
		if root.Path == "" {
			continue
		}
		if workspaceGraphPathAllowed(path, root.Path, includeGlobs, excludeGlobs, gitIgnoreGlobs[root.Path]) {
			return true
		}
	}
	return false
}

func workspaceGraphPathAllowedInRootsWithFilter(path string, roots []workspaceRoot, filter workspaceGraphFileFilter) bool {
	for _, root := range roots {
		if root.Path == "" {
			continue
		}
		if workspaceGraphPathAllowedWithFilter(path, root.Path, filter) {
			return true
		}
	}
	return false
}

func workspaceRootPathForPath(path string, roots []workspaceRoot) string {
	cleanPath := filepath.Clean(path)
	best := ""
	for _, root := range roots {
		if root.Path == "" || !pathWithinRoot(root.Path, cleanPath) {
			continue
		}
		if best == "" || len(root.Path) > len(best) {
			best = root.Path
		}
	}
	return best
}
