package lspserver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

func directoryHasJavaScriptProjectConfig(path string) bool {
	for _, fileName := range []string{"jsconfig.json", "tsconfig.json"} {
		if info, err := os.Stat(filepath.Join(path, fileName)); err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}

type javaScriptProjectConfig struct {
	Options map[string]any
	Types   []string
}

type javaScriptProjectConfigCacheKey struct {
	root      string
	directory string
}

type javaScriptConfigFileSnapshot struct {
	path        string
	exists      bool
	size        int64
	modifiedAt  int64
	contentHash string
}

type javaScriptProjectConfigCacheEntry struct {
	config javaScriptProjectConfig
	files  []javaScriptConfigFileSnapshot
}

func (s *Server) cachedJavaScriptProjectConfig(root, sourceURI string) javaScriptProjectConfig {
	root, directory := javascriptProjectConfigLocation(root, sourceURI)
	return s.cachedJavaScriptProjectConfigAt(root, directory)
}

func (s *Server) cachedJavaScriptProjectConfigAt(root, directory string) javaScriptProjectConfig {
	root = filepath.Clean(root)
	directory = filepath.Clean(directory)
	key := javaScriptProjectConfigCacheKey{root: root, directory: directory}
	s.mu.Lock()
	entry, cached := s.javascriptProjectConfigCache[key]
	s.mu.Unlock()
	if cached {
		valid, refreshedFiles := s.validateJavaScriptConfigSnapshots(entry.files)
		if valid {
			if refreshedFiles != nil {
				s.mu.Lock()
				if current, ok := s.javascriptProjectConfigCache[key]; ok && reflect.DeepEqual(current.files, entry.files) {
					current.files = refreshedFiles
					s.javascriptProjectConfigCache[key] = current
				}
				s.mu.Unlock()
			}
			return cloneJavaScriptProjectConfig(entry.config)
		}
	}
	config, files := s.readJavaScriptProjectConfigAt(root, directory)
	s.mu.Lock()
	if s.javascriptProjectConfigCache == nil {
		s.javascriptProjectConfigCache = map[javaScriptProjectConfigCacheKey]javaScriptProjectConfigCacheEntry{}
	}
	s.javascriptProjectConfigCache[key] = javaScriptProjectConfigCacheEntry{config: cloneJavaScriptProjectConfig(config), files: files}
	s.mu.Unlock()
	return config
}

func (s *Server) clearJavaScriptProjectConfigCacheLocked() {
	s.javascriptProjectConfigCache = map[javaScriptProjectConfigCacheKey]javaScriptProjectConfigCacheEntry{}
}

func (s *Server) markJavaScriptDocumentsChangedLocked() {
	s.javascriptDocumentGeneration++
	s.javascriptMappingGeneration++
	if s.javascriptPreparation != nil {
		s.javascriptPreparation.dirtyOwners = map[string]struct{}{}
	}
}

func (s *Server) markJavaScriptDocumentChangedLocked(uri string) {
	s.javascriptDocumentGeneration++
	s.markJavaScriptDocumentMappingChangedLocked(uri)
}

func (s *Server) markJavaScriptDocumentMappingChangedLocked(uri string) {
	s.javascriptMappingGeneration++
	if s.javascriptPreparation == nil || uri == "" {
		return
	}
	if s.javascriptPreparation.dirtyOwners == nil {
		s.javascriptPreparation.dirtyOwners = map[string]struct{}{}
	}
	s.javascriptPreparation.dirtyOwners[uri] = struct{}{}
}

func (s *Server) resetJavaScriptProjectLocked() {
	s.javascriptProject = nil
	s.javascriptPreparation = nil
	s.javascriptDocumentGeneration++
	s.javascriptMappingGeneration++
	s.clearJavaScriptProjectConfigCacheLocked()
}

func cloneJavaScriptProjectConfig(config javaScriptProjectConfig) javaScriptProjectConfig {
	types := append([]string(nil), config.Types...)
	if config.Types != nil && types == nil {
		types = []string{}
	}
	return javaScriptProjectConfig{
		Options: cloneJavaScriptCompilerOptions(config.Options),
		Types:   types,
	}
}

func javascriptProjectConfigLocation(root, sourceURI string) (string, string) {
	if root == "" {
		root = filepath.Dir(fileURIPath(sourceURI))
	}
	root = filepath.Clean(root)
	sourcePath := fileURIPath(sourceURI)
	directory := root
	if sourcePath != "" && pathContainsFile(root, sourcePath) {
		directory = filepath.Dir(sourcePath)
	}
	return root, filepath.Clean(directory)
}

func javascriptProjectConfigPaths(root, directory string) []string {
	paths := make([]string, 0, 4)
	for current := directory; ; {
		for _, fileName := range []string{"tsconfig.json", "jsconfig.json"} {
			paths = append(paths, filepath.Join(current, fileName))
		}
		if current == root {
			break
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return paths
}

func (s *Server) readJavaScriptProjectConfigAt(root, directory string) (javaScriptProjectConfig, []javaScriptConfigFileSnapshot) {
	paths := javascriptProjectConfigPaths(root, directory)
	snapshots := make([]javaScriptConfigFileSnapshot, 0, len(paths))
	for _, path := range paths {
		path = filepath.Clean(path)
		info, statErr := os.Stat(path)
		snapshot := javaScriptConfigFileSnapshot{path: path}
		if statErr == nil && !info.IsDir() {
			snapshot.exists = true
			snapshot.size = info.Size()
			snapshot.modifiedAt = info.ModTime().UnixNano()
		}
		data, err := s.readSourceFileBytes(withSourceReadBoundaries(context.Background(), root), path, s.includeReadLimiter)
		if err != nil {
			snapshots = append(snapshots, snapshot)
			continue
		}
		snapshot.exists = true
		snapshot.contentHash = workspacepkg.DiskContentHash(string(data))
		snapshots = append(snapshots, snapshot)
		var config struct {
			CompilerOptions map[string]any `json:"compilerOptions"`
		}
		if json.Unmarshal(data, &config) != nil {
			continue
		}
		result := javaScriptProjectConfig{Options: cloneJavaScriptCompilerOptions(config.CompilerOptions)}
		if values, ok := config.CompilerOptions["types"].([]any); ok {
			result.Types = make([]string, 0, len(values))
			for _, value := range values {
				if name, ok := value.(string); ok && strings.TrimSpace(name) != "" {
					result.Types = append(result.Types, name)
				}
			}
		}
		return result, snapshots
	}
	return javaScriptProjectConfig{}, snapshots
}

func (s *Server) validateJavaScriptConfigSnapshots(snapshots []javaScriptConfigFileSnapshot) (bool, []javaScriptConfigFileSnapshot) {
	refreshed := append([]javaScriptConfigFileSnapshot(nil), snapshots...)
	changed := false
	for index := range refreshed {
		snapshot := &refreshed[index]
		info, err := os.Stat(snapshot.path)
		exists := err == nil && !info.IsDir()
		if !exists {
			if snapshot.exists {
				s.invalidateSourceSnapshot(snapshot.path)
				return false, nil
			}
			continue
		}
		if !snapshot.exists || info.Size() != snapshot.size || info.ModTime().UnixNano() != snapshot.modifiedAt {
			s.invalidateSourceSnapshot(snapshot.path)
			data, readErr := s.readSourceFileBytes(withSourceReadBoundaries(context.Background(), filepath.Dir(snapshot.path)), snapshot.path, s.includeReadLimiter)
			if readErr != nil || workspacepkg.DiskContentHash(string(data)) != snapshot.contentHash {
				return false, nil
			}
			snapshot.exists = true
			snapshot.size = info.Size()
			snapshot.modifiedAt = info.ModTime().UnixNano()
			changed = true
		}
	}
	if !changed {
		return true, nil
	}
	return true, refreshed
}

func cloneJavaScriptCompilerOptions(options map[string]any) map[string]any {
	if options == nil {
		return nil
	}
	cloned := make(map[string]any, len(options))
	for key, value := range options {
		cloned[key] = value
	}
	return cloned
}

func javaScriptCompilerOptionTypes(options map[string]any) ([]string, bool) {
	values, ok := options["types"]
	if !ok {
		return nil, false
	}
	items, ok := values.([]any)
	if !ok {
		if strings, ok := values.([]string); ok {
			return append([]string(nil), strings...), true
		}
		return nil, false
	}
	types := make([]string, 0, len(items))
	for _, item := range items {
		if name, ok := item.(string); ok && strings.TrimSpace(name) != "" {
			types = append(types, name)
		}
	}
	return types, true
}

func pathContainsFile(directory, fileName string) bool {
	if directory == "" || fileName == "" {
		return false
	}
	relative, err := filepath.Rel(directory, fileName)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func javaScriptAmbientPackageName(path string) (string, bool) {
	const marker = "/node_modules/@types/"
	index := strings.Index(path, marker)
	if index < 0 {
		return "", false
	}
	remainder := path[index+len(marker):]
	packageName, _, _ := strings.Cut(remainder, "/")
	return packageName, packageName != ""
}

func javaScriptAmbientDirectoryName(typeName string) string {
	name := strings.TrimPrefix(strings.ToLower(typeName), "@")
	return strings.ReplaceAll(name, "/", "__")
}
