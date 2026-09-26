package lspserver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

func (s *Server) javascriptProjectFingerprint() string {
	return s.javascriptProjectFingerprintContext(context.Background())
}

func (s *Server) javascriptProjectFingerprintContext(ctx context.Context) string {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return ""
	}
	roots, gateway := s.javascriptProjectIdentityRoots()
	cache := s.diskCacheForUse()
	settings, ok := s.javascriptProjectDiscoverySettingsContext(ctx, roots)
	if !ok {
		return ""
	}
	settingsKey := javascriptProjectIdentitySettingsKeyForSettings(roots, settings)
	const maxAttempts = 3
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if ctx.Err() != nil {
			return ""
		}
		generation := 0
		if gateway != nil {
			generation = gateway.Generation()
		}
		if cache != nil {
			if cached, ok := javascriptProjectIdentityMemory.Load(cache); ok {
				entry := cached.(javascriptProjectIdentityMemoryEntry)
				if entry.fsGeneration == generation && entry.recordHash == javascriptProjectIdentityRecordHash(settingsKey, entry.record) {
					return entry.fingerprint
				}
			}
		}
		if cache != nil && cache.Enabled() {
			if entry, ok := cache.ReadGraphPayload(settingsKey); ok {
				var record javascriptProjectIdentityRecord
				if ctx.Err() != nil {
					return ""
				}
				if json.Unmarshal(entry.Payload, &record) == nil && javascriptProjectIdentityRecordMatchesSettings(record, roots, settings) && s.javascriptProjectIdentityStillCurrentContext(ctx, record) {
					if ctx.Err() != nil {
						return ""
					}
					if gateway != nil && gateway.Generation() != generation {
						if ctx.Err() != nil {
							return ""
						}
						if attempt+1 < maxAttempts {
							continue
						}
						return workspacepkg.DiskContentHash("javascript-project-identity-unstable-v1\x00" + record.Fingerprint + "\x00" + strconv.Itoa(generation) + "\x00" + strconv.Itoa(gateway.Generation()))
					}
					recordHash := javascriptProjectIdentityRecordHash(settingsKey, record)
					javascriptProjectIdentityMemory.Store(cache, javascriptProjectIdentityMemoryEntry{fsGeneration: generation, fingerprint: record.Fingerprint, recordHash: recordHash, record: record})
					s.logAnalysisDatabaseEvent("javascriptProjectIdentity", "restore", map[string]any{
						"files": len(record.Files), "fingerprint": shortLogKey(record.Fingerprint), "settingsKey": shortLogKey(settingsKey),
					})
					return record.Fingerprint
				}
			}
		}
		record := s.scanJavaScriptProjectIdentityContextWithSettings(ctx, roots, settings)
		if ctx.Err() != nil {
			return ""
		}
		currentGeneration := generation
		if gateway != nil {
			currentGeneration = gateway.Generation()
		}
		if currentGeneration != generation {
			if attempt+1 < maxAttempts {
				continue
			}
			return workspacepkg.DiskContentHash("javascript-project-identity-unstable-v1\x00" + record.Fingerprint + "\x00" + strconv.Itoa(generation) + "\x00" + strconv.Itoa(currentGeneration))
		}
		if cache != nil {
			s.storeJavaScriptProjectIdentity(cache, settingsKey, generation, record, true)
		}
		return record.Fingerprint
	}
	return ""
}

func (s *Server) javascriptProjectIdentityRoots() ([]string, *workspacepkg.FsGateway) {
	s.mu.Lock()
	roots := make([]string, 0, len(s.workspaceRoots)+1)
	for _, root := range s.workspaceRoots {
		if root.Path != "" {
			roots = append(roots, filepath.Clean(root.Path))
		}
	}
	if len(roots) == 0 && s.rootPath != "" {
		roots = append(roots, filepath.Clean(s.rootPath))
	}
	gateway := s.fsGateway
	s.mu.Unlock()
	sort.Strings(roots)
	return roots, gateway
}

type javascriptProjectDiscoverySettings struct {
	includeGlobs         []string
	excludeGlobs         []string
	respectGitIgnore     bool
	gitIgnoreGlobsByRoot map[string][]string
	cacheDirectory       string
	includeJavaScript    bool
}

type javascriptProjectDiscoverySettingsCacheEntry struct {
	key      string
	settings javascriptProjectDiscoverySettings
}

// Discovery settings include the effective .gitignore rules. Keep them apart
// from the project identity cache so a warm request can reuse the rules without
// walking every workspace root again.
var javascriptProjectDiscoverySettingsMemory sync.Map // map[*Server]javascriptProjectDiscoverySettingsCacheEntry

func (s *Server) javascriptProjectDiscoverySettingsContext(ctx context.Context, roots []string) (javascriptProjectDiscoverySettings, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return javascriptProjectDiscoverySettings{}, false
	}
	s.mu.Lock()
	settings := javascriptProjectDiscoverySettings{
		includeGlobs:         append([]string(nil), s.settings.WorkspaceIncludeGlobs...),
		excludeGlobs:         append([]string(nil), s.settings.WorkspaceExcludeGlobs...),
		respectGitIgnore:     s.settings.WorkspaceRespectGitIgnore,
		gitIgnoreGlobsByRoot: make(map[string][]string),
		cacheDirectory:       strings.TrimSpace(s.settings.CacheDirectory),
		includeJavaScript:    !javascriptProjectDefaultWorkspaceIncludes(s.settings.WorkspaceIncludeGlobs),
	}
	s.mu.Unlock()
	cacheKey := s.javascriptProjectDiscoverySettingsCacheKey(roots, settings)
	if cached, ok := javascriptProjectDiscoverySettingsMemory.Load(s); ok {
		entry := cached.(javascriptProjectDiscoverySettingsCacheEntry)
		if entry.key == cacheKey {
			return cloneJavaScriptProjectDiscoverySettings(entry.settings), true
		}
	}
	for _, root := range roots {
		if ctx.Err() != nil {
			return javascriptProjectDiscoverySettings{}, false
		}
		root = filepath.Clean(root)
		if root == "." || root == "" {
			continue
		}
		if settings.respectGitIgnore {
			settings.gitIgnoreGlobsByRoot[root] = s.readGitIgnoreGlobsContext(ctx, root)
			if ctx.Err() != nil {
				return javascriptProjectDiscoverySettings{}, false
			}
		}
	}
	if ctx.Err() != nil {
		return javascriptProjectDiscoverySettings{}, false
	}
	javascriptProjectDiscoverySettingsMemory.Store(s, javascriptProjectDiscoverySettingsCacheEntry{
		key: cacheKey, settings: cloneJavaScriptProjectDiscoverySettings(settings),
	})
	return settings, true
}

func cloneJavaScriptProjectDiscoverySettings(settings javascriptProjectDiscoverySettings) javascriptProjectDiscoverySettings {
	clone := javascriptProjectDiscoverySettings{
		includeGlobs:         append([]string(nil), settings.includeGlobs...),
		excludeGlobs:         append([]string(nil), settings.excludeGlobs...),
		respectGitIgnore:     settings.respectGitIgnore,
		gitIgnoreGlobsByRoot: make(map[string][]string, len(settings.gitIgnoreGlobsByRoot)),
		cacheDirectory:       settings.cacheDirectory,
		includeJavaScript:    settings.includeJavaScript,
	}
	for root, globs := range settings.gitIgnoreGlobsByRoot {
		clone.gitIgnoreGlobsByRoot[root] = append([]string(nil), globs...)
	}
	return clone
}

func (s *Server) javascriptProjectDiscoverySettingsCacheKey(roots []string, settings javascriptProjectDiscoverySettings) string {
	generation := -1
	s.mu.Lock()
	gateway := s.fsGateway
	s.mu.Unlock()
	if gateway != nil {
		generation = gateway.Generation()
	}
	keyRoots := make([]string, 0, len(roots))
	for _, root := range roots {
		root = filepath.Clean(root)
		if root != "." && root != "" {
			keyRoots = append(keyRoots, root)
		}
	}
	sort.Strings(keyRoots)
	payload, err := json.Marshal(struct {
		Roots             []string `json:"roots"`
		Includes          []string `json:"includes,omitempty"`
		Excludes          []string `json:"excludes,omitempty"`
		RespectGitIgnore  bool     `json:"respectGitIgnore"`
		CacheDirectory    string   `json:"cacheDirectory,omitempty"`
		IncludeJavaScript bool     `json:"includeJavaScript"`
		FsGeneration      int      `json:"fsGeneration"`
	}{
		Roots: keyRoots, Includes: settings.includeGlobs, Excludes: settings.excludeGlobs,
		RespectGitIgnore: settings.respectGitIgnore, CacheDirectory: settings.cacheDirectory,
		IncludeJavaScript: settings.includeJavaScript, FsGeneration: generation,
	})
	if err != nil {
		return workspacepkg.DiskContentHash("javascript-project-discovery-settings-v1\x00" + strings.Join(keyRoots, "\x00") + "\x00" + strconv.Itoa(generation))
	}
	return workspacepkg.DiskContentHash("javascript-project-discovery-settings-v1\x00" + string(payload))
}

func invalidateJavaScriptProjectDiscoverySettings(s *Server) {
	if s != nil {
		javascriptProjectDiscoverySettingsMemory.Delete(s)
	}
}

func javascriptProjectDefaultWorkspaceIncludes(patterns []string) bool {
	return len(patterns) == 1 && patterns[0] == "**/*.{asp,asa,inc,vbs}"
}

func (settings javascriptProjectDiscoverySettings) filterForRoot(root string) workspaceIndexFileDiscoveryFilter {
	root = filepath.Clean(root)
	includeGlobs := settings.includeGlobs
	if !settings.includeJavaScript {
		includeGlobs = nil
	}
	return newWorkspaceIndexFileDiscoveryFilter(root, includeGlobs, settings.excludeGlobs, settings.gitIgnoreGlobsByRoot[root], settings.cacheDirectory)
}

func javascriptProjectIdentitySettingsKeyForSettings(roots []string, settings javascriptProjectDiscoverySettings) string {
	type rootRules struct {
		Root  string   `json:"root"`
		Rules []string `json:"rules,omitempty"`
	}
	rules := make([]rootRules, 0, len(roots))
	for _, root := range roots {
		root = filepath.Clean(root)
		if root == "." || root == "" {
			continue
		}
		rules = append(rules, rootRules{Root: root, Rules: append([]string(nil), settings.gitIgnoreGlobsByRoot[root]...)})
	}
	sort.Slice(rules, func(i, j int) bool { return rules[i].Root < rules[j].Root })
	payload, err := json.Marshal(struct {
		Roots             []rootRules `json:"roots"`
		Includes          []string    `json:"includes,omitempty"`
		Excludes          []string    `json:"excludes,omitempty"`
		RespectGitIgnore  bool        `json:"respectGitIgnore"`
		CacheDirectory    string      `json:"cacheDirectory,omitempty"`
		IncludeJavaScript bool        `json:"includeJavaScript"`
	}{
		Roots: rules, Includes: settings.includeGlobs, Excludes: settings.excludeGlobs,
		RespectGitIgnore: settings.respectGitIgnore, CacheDirectory: settings.cacheDirectory,
		IncludeJavaScript: settings.includeJavaScript,
	})
	if err != nil {
		return workspacepkg.DiskContentHash("javascript-project-identity-v2\x00" + strings.Join(roots, "\x00"))
	}
	return workspacepkg.DiskContentHash("javascript-project-identity-v2\x00" + string(payload))
}

func javascriptProjectIdentityRecordHash(settingsKey string, record javascriptProjectIdentityRecord) string {
	payload, err := json.Marshal(record)
	if err != nil {
		return ""
	}
	return workspacepkg.DiskContentHash(settingsKey + "\x00" + string(payload))
}

func javascriptProjectIdentityRecordMatchesSettings(record javascriptProjectIdentityRecord, roots []string, settings javascriptProjectDiscoverySettings) bool {
	if record.SchemaVersion != 1 || record.Fingerprint == "" {
		return false
	}
	for _, metadata := range record.Files {
		if !javascriptProjectIdentityPathAllowed(metadata.FileName, roots, settings) {
			return false
		}
	}
	for _, metadata := range record.Directories {
		if !javascriptProjectIdentityDirectoryAllowed(metadata.FileName, roots, settings) {
			return false
		}
	}
	return true
}

func javascriptProjectIdentityPathAllowed(path string, roots []string, settings javascriptProjectDiscoverySettings) bool {
	cleanPath := filepath.Clean(path)
	if !isJavaScriptProjectFingerprintFile(cleanPath) {
		return false
	}
	for _, root := range roots {
		root = filepath.Clean(root)
		if root == "." || root == "" || !pathWithinRoot(root, cleanPath) {
			continue
		}
		relative, err := filepath.Rel(root, cleanPath)
		if err != nil || relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
			continue
		}
		if settings.filterForRoot(root).allowsFile(filepath.ToSlash(relative)) {
			return true
		}
	}
	return false
}

func javascriptProjectIdentityDirectoryAllowed(path string, roots []string, settings javascriptProjectDiscoverySettings) bool {
	cleanPath := filepath.Clean(path)
	for _, root := range roots {
		root = filepath.Clean(root)
		if root == "." || root == "" || !pathWithinRoot(root, cleanPath) {
			continue
		}
		relative, err := filepath.Rel(root, cleanPath)
		if err != nil || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
			continue
		}
		if relative == "." {
			return true
		}
		filter := settings.filterForRoot(root)
		if !filter.skipsDirectory(filepath.ToSlash(relative)) {
			return true
		}
	}
	return false
}

func (s *Server) storeJavaScriptProjectIdentity(cache *workspacepkg.DiskAnalysisCache, settingsKey string, generation int, record javascriptProjectIdentityRecord, persist bool) {
	payload, err := json.Marshal(record)
	if err != nil {
		return
	}
	recordHash := workspacepkg.DiskContentHash(settingsKey + "\x00" + string(payload))
	javascriptProjectIdentityMemory.Store(cache, javascriptProjectIdentityMemoryEntry{
		fsGeneration: generation,
		fingerprint:  record.Fingerprint,
		recordHash:   recordHash,
		record:       record,
	})
	if !persist || !cache.Enabled() {
		return
	}
	s.runAsyncDiskCacheWrite(func() {
		if s.diskCacheForUse() != cache {
			return
		}
		current, ok := javascriptProjectIdentityMemory.Load(cache)
		if !ok || current.(javascriptProjectIdentityMemoryEntry).recordHash != recordHash {
			return
		}
		if err := cache.WriteGraphPayload(workspacepkg.DiskGraphPayloadCacheEntry{SettingsKey: settingsKey, Payload: payload}); err != nil {
			s.logServerWarning("[asp-lsp] javascriptProjectIdentity.write.failed: " + err.Error())
		}
	})
}

func (s *Server) updateJavaScriptProjectIdentityForWatchedFiles(changes []fileEvent) {
	cache := s.diskCacheForUse()
	if cache == nil || !cache.Enabled() || len(changes) == 0 {
		return
	}
	roots, gateway := s.javascriptProjectIdentityRoots()
	settings, ok := s.javascriptProjectDiscoverySettingsContext(context.Background(), roots)
	if !ok {
		return
	}
	settingsKey := javascriptProjectIdentitySettingsKeyForSettings(roots, settings)
	var record javascriptProjectIdentityRecord
	if cached, ok := javascriptProjectIdentityMemory.Load(cache); ok {
		entry := cached.(javascriptProjectIdentityMemoryEntry)
		if entry.recordHash == javascriptProjectIdentityRecordHash(settingsKey, entry.record) {
			record = entry.record
		}
	}
	if record.Fingerprint == "" {
		entry, ok := cache.ReadGraphPayload(settingsKey)
		if !ok || json.Unmarshal(entry.Payload, &record) != nil || !javascriptProjectIdentityRecordMatchesSettings(record, roots, settings) {
			return
		}
	}
	files := make(map[string]workspacepkg.DiskAnalysisSourceMetadata, len(record.Files))
	for _, metadata := range record.Files {
		files[filepath.Clean(metadata.FileName)] = metadata
	}
	directories := make(map[string]workspacepkg.DiskAnalysisSourceMetadata, len(record.Directories))
	for _, metadata := range record.Directories {
		directories[filepath.Clean(metadata.FileName)] = metadata
	}
	identityChanged := false
	for _, change := range changes {
		path := filepath.Clean(fileURIPath(change.URI))
		if path == "." || !javascriptProjectIdentityPathAllowed(path, roots, settings) {
			continue
		}
		if change.Type == fileChangeDeleted {
			if _, ok := files[path]; ok {
				delete(files, path)
				identityChanged = true
			}
		} else if metadata, ok := s.readJavaScriptProjectIdentityFile(path); ok {
			if previous, exists := files[path]; !exists || previous != metadata {
				files[path] = metadata
				identityChanged = true
			}
		} else if _, ok := files[path]; ok {
			delete(files, path)
			identityChanged = true
		}
		for _, directory := range javascriptProjectIdentityParentDirectories(path, roots) {
			if info, ok := s.fsStat(directory); ok && info.Directory {
				metadata := workspacepkg.DiskAnalysisSourceMetadata{FileName: directory, MtimeMS: info.MtimeMS}
				if previous, exists := directories[directory]; !exists || previous != metadata {
					directories[directory] = metadata
					identityChanged = true
				}
			} else if _, exists := directories[directory]; exists {
				delete(directories, directory)
				identityChanged = true
			}
		}
	}
	if identityChanged {
		record.Files = make([]workspacepkg.DiskAnalysisSourceMetadata, 0, len(files))
		for _, metadata := range files {
			record.Files = append(record.Files, metadata)
		}
		record.Directories = make([]workspacepkg.DiskAnalysisSourceMetadata, 0, len(directories))
		for _, metadata := range directories {
			record.Directories = append(record.Directories, metadata)
		}
		sort.Slice(record.Files, func(i, j int) bool { return record.Files[i].FileName < record.Files[j].FileName })
		sort.Slice(record.Directories, func(i, j int) bool { return record.Directories[i].FileName < record.Directories[j].FileName })
		record.Fingerprint = javascriptProjectIdentityFingerprint(record.Files)
	}
	generation := 0
	if gateway != nil {
		generation = gateway.Generation()
	}
	s.storeJavaScriptProjectIdentity(cache, settingsKey, generation, record, identityChanged)
}

func (s *Server) readJavaScriptProjectIdentityFile(path string) (workspacepkg.DiskAnalysisSourceMetadata, bool) {
	return s.readJavaScriptProjectIdentityFileContext(context.Background(), path)
}

func (s *Server) readJavaScriptProjectIdentityFileContext(ctx context.Context, path string) (workspacepkg.DiskAnalysisSourceMetadata, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return workspacepkg.DiskAnalysisSourceMetadata{}, false
	}
	if hook := javascriptProjectIdentityTestHooks.Load(); hook != nil && hook.readFile != nil {
		hook.readFile(path)
	}
	if ctx.Err() != nil {
		return workspacepkg.DiskAnalysisSourceMetadata{}, false
	}
	content, err := s.readSourceFileBytes(ctx, path, s.includeReadLimiter)
	if err != nil {
		return workspacepkg.DiskAnalysisSourceMetadata{}, false
	}
	metadata := workspacepkg.DiskAnalysisSourceMetadata{
		FileName:    filepath.Clean(path),
		Size:        int64(len(content)),
		ContentHash: workspacepkg.DiskContentHash(string(content)),
	}
	if info, ok := s.fsStatContext(ctx, path); ok {
		metadata.MtimeMS = info.MtimeMS
		metadata.Size = info.Size
	}
	return metadata, true
}

func javascriptProjectIdentityFingerprint(files []workspacepkg.DiskAnalysisSourceMetadata) string {
	parts := make([]string, 0, len(files))
	for _, metadata := range files {
		parts = append(parts, filepath.Clean(metadata.FileName)+"\x00"+metadata.ContentHash)
	}
	sort.Strings(parts)
	return workspacepkg.DiskContentHash(strings.Join(parts, "\x00"))
}

func javascriptProjectIdentityParentDirectories(path string, roots []string) []string {
	for _, root := range roots {
		relative, err := filepath.Rel(root, path)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			continue
		}
		directories := []string{}
		for current := filepath.Dir(path); ; current = filepath.Dir(current) {
			directories = append(directories, current)
			if current == root {
				break
			}
		}
		return directories
	}
	return nil
}

func (s *Server) scanJavaScriptProjectIdentity(roots []string) javascriptProjectIdentityRecord {
	return s.scanJavaScriptProjectIdentityContext(context.Background(), roots)
}

func (s *Server) scanJavaScriptProjectIdentityContext(ctx context.Context, roots []string) javascriptProjectIdentityRecord {
	if ctx == nil {
		ctx = context.Background()
	}
	settings, ok := s.javascriptProjectDiscoverySettingsContext(ctx, roots)
	if !ok {
		return javascriptProjectIdentityRecord{SchemaVersion: 1}
	}
	return s.scanJavaScriptProjectIdentityContextWithSettings(ctx, roots, settings)
}

func (s *Server) scanJavaScriptProjectIdentityContextWithSettings(ctx context.Context, roots []string, settings javascriptProjectDiscoverySettings) javascriptProjectIdentityRecord {
	if ctx == nil {
		ctx = context.Background()
	}
	seen := map[string]struct{}{}
	seenDirectories := map[string]struct{}{}
	record := javascriptProjectIdentityRecord{SchemaVersion: 1}
	filePaths := make([]string, 0)
	for _, root := range roots {
		if ctx.Err() != nil {
			break
		}
		if root == "" {
			continue
		}
		root = filepath.Clean(root)
		filter := settings.filterForRoot(root)
		if hook := javascriptProjectIdentityTestHooks.Load(); hook != nil && hook.walkDir != nil {
			hook.walkDir(root)
		}
		_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err != nil {
				return nil
			}
			if entry.IsDir() {
				relative, relativeErr := filepath.Rel(root, path)
				if relativeErr != nil {
					return nil
				}
				relativeSlash := filepath.ToSlash(relative)
				if relative != "." && filter.skipsDirectory(relativeSlash) {
					return filepath.SkipDir
				}
				switch strings.ToLower(entry.Name()) {
				case ".git", "dist", "out":
					return filepath.SkipDir
				case "node_modules":
					return filepath.SkipDir
				}
				normalized := filepath.ToSlash(filepath.Clean(path))
				if strings.Contains(normalized, "/node_modules/") {
					return filepath.SkipDir
				}
				cleaned := filepath.Clean(path)
				if _, ok := seenDirectories[cleaned]; ok {
					return nil
				}
				seenDirectories[cleaned] = struct{}{}
				if info, infoErr := entry.Info(); infoErr == nil {
					record.Directories = append(record.Directories, workspacepkg.DiskAnalysisSourceMetadata{
						FileName: cleaned,
						MtimeMS:  info.ModTime().UnixMilli(),
					})
				}
				return nil
			}
			if !isJavaScriptProjectFingerprintFile(path) {
				return nil
			}
			relative, relativeErr := filepath.Rel(root, path)
			if relativeErr != nil || relative == "." || !filter.allowsFile(filepath.ToSlash(relative)) {
				return nil
			}
			cleaned := filepath.Clean(path)
			if _, ok := seen[cleaned]; ok {
				return nil
			}
			seen[cleaned] = struct{}{}
			filePaths = append(filePaths, cleaned)
			return nil
		})
	}
	sort.Strings(filePaths)
	type fileResult struct {
		metadata workspacepkg.DiskAnalysisSourceMetadata
		ok       bool
	}
	fileResults := make([]fileResult, len(filePaths))
	s.analysisWorkers.parallelForBulk(ctx, len(filePaths), func(workerCtx context.Context, index int) {
		metadata, ok := s.readJavaScriptProjectIdentityFileContext(workerCtx, filePaths[index])
		if !ok {
			return
		}
		fileResults[index] = fileResult{metadata: metadata, ok: true}
	})
	for _, result := range fileResults {
		if result.ok {
			record.Files = append(record.Files, result.metadata)
		}
	}
	sort.Slice(record.Files, func(i, j int) bool { return record.Files[i].FileName < record.Files[j].FileName })
	sort.Slice(record.Directories, func(i, j int) bool { return record.Directories[i].FileName < record.Directories[j].FileName })
	record.Fingerprint = javascriptProjectIdentityFingerprint(record.Files)
	return record
}

func (s *Server) javascriptProjectIdentityStillCurrentContext(ctx context.Context, record javascriptProjectIdentityRecord) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	if record.SchemaVersion != 1 || record.Fingerprint == "" {
		return false
	}
	total := len(record.Files) + len(record.Directories)
	var current atomic.Bool
	current.Store(true)
	workers := s.analysisWorkers
	if workers == nil {
		workers = &analysisWorkerPool{}
	}
	workers.parallelForBulk(ctx, total, func(workerCtx context.Context, index int) {
		if !current.Load() || workerCtx.Err() != nil {
			return
		}
		if index < len(record.Files) {
			metadata := record.Files[index]
			info, ok := s.fsStatContext(workerCtx, metadata.FileName)
			if !ok || info.MtimeMS != metadata.MtimeMS || info.Size != metadata.Size {
				current.Store(false)
			}
			return
		}
		metadata := record.Directories[index-len(record.Files)]
		info, ok := s.fsStatContext(workerCtx, metadata.FileName)
		if !ok || !info.Directory || info.MtimeMS != metadata.MtimeMS {
			current.Store(false)
		}
	})
	return ctx.Err() == nil && current.Load()
}

func isJavaScriptProjectFingerprintFile(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	if base == "jsconfig.json" || base == "tsconfig.json" || base == "package.json" {
		return true
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx", ".mts", ".cts":
		return true
	default:
		return strings.HasSuffix(base, ".d.ts")
	}
}

func (s *Server) diskIncludeFingerprint(parsed *core.ParsedDocument) string {
	if parsed == nil {
		return ""
	}
	if len(parsed.Includes) == 0 {
		return workspacepkg.DiskContentHash("")
	}
	type resolvedInclude struct {
		include core.Include
		path    string
		found   bool
	}
	resolved := make([]resolvedInclude, 0, len(parsed.Includes))
	for _, include := range parsed.Includes {
		details, ok := s.includeTargetDetailsForMode(parsed.URI, include.Path, include.Mode)
		resolved = append(resolved, resolvedInclude{
			include: include,
			path:    details.Path,
			found:   ok && details.Exists && details.Path != "",
		})
	}
	s.mu.Lock()
	graph := s.workspaceIncludeGraph
	entries := make(map[string]workspacepkg.IncludeGraphEntry)
	var collect func(string)
	collect = func(path string) {
		identity := workspacepkg.FileIdentityKeyFromFileName(path)
		if _, seen := entries[identity]; seen || graph == nil {
			return
		}
		entry, ok := graph.Get(path)
		if !ok {
			return
		}
		entries[identity] = entry
		for _, target := range entry.TargetFileNames {
			collect(target)
		}
	}
	for _, include := range resolved {
		if include.found {
			collect(include.path)
		}
	}
	s.mu.Unlock()
	visited := map[string]struct{}{}
	parts := make([]string, 0, len(parsed.Includes))
	for _, resolved := range resolved {
		if !resolved.found {
			parts = append(parts, resolved.include.Path+"\x00missing")
			continue
		}
		parts = append(parts, resolved.include.Mode+"\x00"+resolved.include.Path+"\x00"+s.diskIncludePathFingerprint(resolved.path, entries, visited))
	}
	sort.Strings(parts)
	return workspacepkg.DiskContentHash(strings.Join(parts, "\x00"))
}

func (s *Server) diskIncludePathFingerprint(path string, entries map[string]workspacepkg.IncludeGraphEntry, visited map[string]struct{}) string {
	cleaned := filepath.Clean(path)
	identity := workspacepkg.FileIdentityKeyFromFileName(cleaned)
	if _, seen := visited[identity]; seen {
		return cleaned + "\x00cycle"
	}
	visited[identity] = struct{}{}
	defer delete(visited, identity)
	parts := []string{cleaned}
	if open := s.documentByURI(filePathURI(cleaned)); open != nil {
		parts = append(parts, "content", workspacepkg.DiskContentHash(open.Text))
	} else if entry, ok := entries[identity]; ok && entry.Source.ContentHash != "" {
		parts = append(parts, "content", entry.Source.ContentHash)
	} else if info, ok := s.fsStat(cleaned); ok {
		parts = append(parts, "metadata", strconv.FormatInt(info.MtimeMS, 10), strconv.FormatInt(info.Size, 10))
	} else {
		parts = append(parts, "missing")
	}
	if entry, ok := entries[identity]; ok {
		parts = append(parts, "refs", entry.RefsFingerprint)
		targets := make([]string, 0, len(entry.TargetFileNames))
		for _, target := range entry.TargetFileNames {
			targets = append(targets, s.diskIncludePathFingerprint(target, entries, visited))
		}
		sort.Strings(targets)
		parts = append(parts, targets...)
	}
	return workspacepkg.DiskContentHash(strings.Join(parts, "\x00"))
}
