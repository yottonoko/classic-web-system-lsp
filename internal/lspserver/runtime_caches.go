package lspserver

import (
	"maps"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

const (
	// diskCacheToolVersion changes only when persisted analysis semantics become incompatible.
	diskCacheToolVersion = "0.9.2-go"
	// diskReferenceCacheSchemaVersion changes when reference-only persisted records become incompatible.
	diskReferenceCacheSchemaVersion uint32 = 1
)

const diskCacheSweepInterval = 5 * time.Minute

const workspaceIncludeGraphPersistDebounce = 50 * time.Millisecond

const runtimeMemoryPressureDebounce = 100 * time.Millisecond

const (
	legacyDiskCacheCleanupBatchSize = 128
	legacyDiskCacheCleanupPause     = 25 * time.Millisecond
	defaultDiskCacheTTLHours        = 14 * 24
	defaultDiskCacheMaxSizeMB       = 16 * 1024
	maxDiskCacheTTLHours            = 24 * 365 * 5
	maxDiskCacheMaxSizeMB           = 64 * 1024
)

type diskCacheSweepKey struct {
	directory string
	ttl       time.Duration
	maxSize   int64
}

var diskCacheSweeps = struct {
	sync.Mutex
	last     map[diskCacheSweepKey]time.Time
	inFlight map[diskCacheSweepKey]struct{}
}{
	last:     map[diskCacheSweepKey]time.Time{},
	inFlight: map[diskCacheSweepKey]struct{}{},
}

type javascriptProjectIdentityRecord struct {
	SchemaVersion int                                       `json:"schemaVersion"`
	Fingerprint   string                                    `json:"fingerprint"`
	Files         []workspacepkg.DiskAnalysisSourceMetadata `json:"files"`
	Directories   []workspacepkg.DiskAnalysisSourceMetadata `json:"directories"`
}

type javascriptProjectIdentityMemoryEntry struct {
	fsGeneration int
	fingerprint  string
	recordHash   string
	record       javascriptProjectIdentityRecord
}

var javascriptProjectIdentityMemory sync.Map

type javascriptProjectIdentityTestHook struct {
	walkDir  func(string)
	readFile func(string)
}

type diskCacheWriteTask struct {
	key string
	fn  func()
}

var javascriptProjectIdentityTestHooks atomic.Pointer[javascriptProjectIdentityTestHook]

type serverRegisteredCache struct {
	name     string
	priority int
	estimate func() (int64, int)
	evict    func(int64) int64
}

func (c *serverRegisteredCache) Name() string         { return c.name }
func (c *serverRegisteredCache) Priority() int        { return c.priority }
func (c *serverRegisteredCache) EstimateBytes() int64 { bytes, _ := c.estimate(); return bytes }
func (c *serverRegisteredCache) EntryCount() int      { _, entries := c.estimate(); return entries }
func (c *serverRegisteredCache) MemoryEstimate() (int64, int) {
	return c.estimate()
}
func (c *serverRegisteredCache) Evict(target int64) int64 {
	return c.evict(target)
}

func (s *Server) configureRuntimeCaches() {
	manager := workspacepkg.NewMemoryBudgetManager(workspacepkg.MemoryBudgetManagerOptions{
		HeapStatsProvider: func() workspacepkg.HeapStats {
			var stats runtime.MemStats
			runtime.ReadMemStats(&stats)
			return workspacepkg.HeapStats{HeapUsed: int64(stats.HeapAlloc), HeapSizeLimit: finiteRuntimeMemoryLimit(debug.SetMemoryLimit(-1))}
		},
	})
	manager.Register(s.registeredAnalysisSnapshotCache())
	manager.Register(s.registeredSourceSnapshotCache())
	manager.Register(s.registeredParsedCache())
	manager.Register(s.registeredDocumentStoreCache())
	manager.Register(s.registeredWorkspaceArtifactCache())
	manager.Register(s.registeredSemanticCache())
	manager.Register(s.registeredGraphCache())
	manager.Register(s.registeredReferenceCache())
	s.memoryBudget = manager
}

func finiteRuntimeMemoryLimit(limit int64) int64 {
	if limit <= 0 || limit == math.MaxInt64 {
		return 0
	}
	return limit
}

func (s *Server) registeredAnalysisSnapshotCache() workspacepkg.RegisteredCache {
	return &serverRegisteredCache{name: "analysisSnapshots", priority: 5,
		estimate: func() (int64, int) {
			// The walk over cached analysis is the slowest part of a pressure
			// check, so run it outside Server.mu. A stale owner set only skews
			// this estimate; eviction still takes both locks together.
			s.mu.Lock()
			external := s.analysisExternalParsedOwnerSetLocked()
			s.mu.Unlock()
			return s.analysisCache.memoryEstimateWithExternalParsed(external)
		},
		evict: func(target int64) int64 {
			freed, _ := s.withParsedCacheOwnership(func(external map[*core.ParsedDocument]struct{}) (int64, int) {
				return s.analysisCache.evictWithExternalParsed(target, external), 0
			})
			return freed
		},
	}
}

// withParsedCacheOwnership keeps parsed-cache ownership and analysis-cache
// accounting in one transaction. The lock order is Server.mu followed by
// analysisCache.mu (acquired by the callback), and callbacks must not acquire
// Server.mu again.
func (s *Server) withParsedCacheOwnership(fn func(map[*core.ParsedDocument]struct{}) (int64, int)) (int64, int) {
	if s == nil || s.analysisCache == nil || fn == nil {
		return 0, 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return fn(s.analysisExternalParsedOwnerSetLocked())
}

func (s *Server) parsedCacheOwnerSetLocked() map[*core.ParsedDocument]struct{} {
	owners := map[*core.ParsedDocument]struct{}{}
	if s == nil {
		return owners
	}
	for _, entry := range s.parsedCache {
		if entry.Parsed != nil {
			owners[entry.Parsed] = struct{}{}
		}
	}
	return owners
}

func (s *Server) analysisExternalParsedOwnerSetLocked() map[*core.ParsedDocument]struct{} {
	owners := s.parsedCacheOwnerSetLocked()
	for parsed := range documentStoreParsedOwnerSet(s.documentStore) {
		owners[parsed] = struct{}{}
	}
	return owners
}

func (s *Server) registeredSourceSnapshotCache() workspacepkg.RegisteredCache {
	return &serverRegisteredCache{name: "sourceSnapshots", priority: 8,
		estimate: func() (int64, int) {
			s.mu.Lock()
			defer s.mu.Unlock()
			var bytes int64
			for key, snapshot := range s.sourceSnapshots {
				bytes += estimateSourceSnapshotBytes(key, snapshot)
			}
			return bytes, len(s.sourceSnapshots)
		},
		evict: func(target int64) int64 {
			s.mu.Lock()
			defer s.mu.Unlock()
			type candidate struct {
				key      string
				snapshot *sourceFileSnapshot
				open     bool
			}
			candidates := make([]candidate, 0, len(s.sourceSnapshots))
			for key, snapshot := range s.sourceSnapshots {
				if snapshot == nil || !snapshot.valid || len(snapshot.decoded) == 0 {
					continue
				}
				open := s.openDocumentByURILocked(filePathURI(snapshot.path)) != nil
				candidates = append(candidates, candidate{key: key, snapshot: snapshot, open: open})
			}
			sort.Slice(candidates, func(i, j int) bool {
				if candidates[i].open != candidates[j].open {
					return !candidates[i].open
				}
				if !candidates[i].snapshot.loadedAt.Equal(candidates[j].snapshot.loadedAt) {
					return candidates[i].snapshot.loadedAt.Before(candidates[j].snapshot.loadedAt)
				}
				return candidates[i].key < candidates[j].key
			})
			var freed int64
			for _, candidate := range candidates {
				if target > 0 && freed >= target {
					break
				}
				if s.sourceSnapshots[candidate.key] != candidate.snapshot || len(candidate.snapshot.decoded) == 0 {
					continue
				}
				for encoding, text := range candidate.snapshot.decoded {
					freed += int64(len(encoding)+len(text))*2 + 64
				}
				// The raw immutable source is the one-read guarantee. Memory
				// pressure may discard decoded variants, but only an actual file
				// invalidation may discard raw and permit another physical read.
				candidate.snapshot.decoded = map[string]string{}
			}
			return freed
		},
	}
}

func estimateSourceSnapshotBytes(key string, snapshot *sourceFileSnapshot) int64 {
	if snapshot == nil {
		return int64(len(key))*2 + 32
	}
	bytes := int64(len(key)+len(snapshot.path))*2 + int64(len(snapshot.raw)) + 128
	for encoding, text := range snapshot.decoded {
		bytes += int64(len(encoding)+len(text))*2 + 64
	}
	return bytes
}

func (s *Server) registeredWorkspaceArtifactCache() workspacepkg.RegisteredCache {
	return &serverRegisteredCache{name: "workspaceArtifacts", priority: 15,
		estimate: func() (int64, int) {
			s.mu.Lock()
			defer s.mu.Unlock()
			var bytes int64
			for _, manifest := range s.workspaceArtifacts {
				bytes += estimateWorkspaceDocumentArtifactBytes(manifest)
			}
			return bytes, len(s.workspaceArtifacts)
		},
		evict: func(target int64) int64 {
			s.mu.Lock()
			defer s.mu.Unlock()
			keys := make([]workspaceDocumentID, 0, len(s.workspaceArtifacts))
			for key := range s.workspaceArtifacts {
				keys = append(keys, key)
			}
			sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
			var freed int64
			for _, key := range keys {
				freed += estimateWorkspaceDocumentArtifactBytes(s.workspaceArtifacts[key])
				delete(s.workspaceArtifacts, key)
				delete(s.workspaceArtifactRevisions, key)
				if freed >= target {
					break
				}
			}
			return freed
		},
	}
}

func (s *Server) configureDiskAnalysisCache() {
	s.workspaceIndexDiskCacheUseMu.Lock()
	defer s.workspaceIndexDiskCacheUseMu.Unlock()
	s.mu.Lock()
	if s.shutdown {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	s.pauseAsyncDiskCacheWrites()
	cache, settings := s.replaceDiskAnalysisCache()
	s.resumeAsyncDiskCacheWrites()
	if cache == nil {
		return
	}
	ttl := time.Duration(settings.CacheTTLHours) * time.Hour
	maxSize := int64(settings.CacheMaxSizeMB) * 1024 * 1024
	s.sweepDiskCacheIfDue(cache, ttl, maxSize)
	s.scheduleLegacyDiskCacheCleanup(cache, settings.CacheDirectory)
	s.mu.Lock()
	includeGraphSettingsKey := ""
	if s.workspaceIncludeGraph != nil && s.workspaceIncludeGraphComplete {
		includeGraphSettingsKey = s.workspaceIncludeGraph.SettingsKey()
	}
	s.mu.Unlock()
	if includeGraphSettingsKey != "" && cache.Enabled() {
		if settingsKey := s.workspaceDiskSettingsKey(); includeGraphSettingsKey == settingsKey {
			s.scheduleWorkspaceIncludeGraphPersistence(settingsKey)
		}
	}
}

func (s *Server) replaceDiskAnalysisCache() (*workspacepkg.DiskAnalysisCache, serverSettings) {
	s.mu.Lock()
	if s.shutdown {
		settings := s.settings
		s.mu.Unlock()
		return nil, settings
	}
	settings := s.settings
	roots := make([]string, 0, len(s.workspaceRoots)+1)
	for _, root := range s.workspaceRoots {
		if root.Path != "" {
			roots = append(roots, root.Path)
		}
	}
	if len(roots) == 0 && s.rootPath != "" {
		roots = append(roots, s.rootPath)
	}
	s.mu.Unlock()
	if len(roots) == 0 {
		if cwd, err := os.Getwd(); err == nil && cwd != "" {
			roots = append(roots, cwd)
		}
	}
	configuredCacheDirectory := strings.TrimSpace(settings.CacheDirectory)
	settings.CacheTTLHours = boundedDiskCacheTTLHours(settings.CacheTTLHours)
	settings.CacheMaxSizeMB = boundedDiskCacheMaxSizeMB(settings.CacheMaxSizeMB)
	settings.CacheDirectory = s.resolveCacheDirectory(configuredCacheDirectory, roots)
	if settings.CacheDirectory == "" {
		settings.CacheEnabled = false
	}
	s.mu.Lock()
	s.settings.CacheTTLHours = settings.CacheTTLHours
	s.settings.CacheMaxSizeMB = settings.CacheMaxSizeMB
	if configuredCacheDirectory == "" || settings.CacheDirectory == "" {
		s.settings.CacheDirectory = ""
	} else {
		s.settings.CacheDirectory = settings.CacheDirectory
	}
	s.mu.Unlock()
	normalizedRoots := make([]string, 0, len(roots))
	seenRoots := make(map[string]struct{}, len(roots))
	for _, root := range roots {
		absolute, err := filepath.Abs(root)
		if err != nil {
			absolute = root
		}
		normalized := filepath.Clean(absolute)
		if _, seen := seenRoots[normalized]; seen {
			continue
		}
		seenRoots[normalized] = struct{}{}
		normalizedRoots = append(normalizedRoots, normalized)
	}
	sort.Strings(normalizedRoots)
	namespace := workspacepkg.DiskContentHash(strings.Join(normalizedRoots, "\x00"))
	ttl := time.Duration(settings.CacheTTLHours) * time.Hour
	maxSize := int64(settings.CacheMaxSizeMB) * 1024 * 1024
	s.workspaceIndexDiskCommitMu.Lock()
	s.mu.Lock()
	previous := s.diskAnalysisCache
	s.diskAnalysisCache = nil
	s.mu.Unlock()
	s.workspaceIndexDiskCommitMu.Unlock()
	if previous != nil {
		javascriptProjectIdentityMemory.Delete(previous)
		if err := previous.Flush(); err != nil {
			s.logServerWarning("[asp-lsp] analysisDatabase.flush.failed")
		}
		if err := previous.Close(); err != nil {
			s.logServerWarning("[asp-lsp] analysisDatabase.close.failed")
		}
	}
	cache, err := workspacepkg.NewDiskAnalysisCache(workspacepkg.DiskAnalysisCacheOptions{
		Enabled:     settings.CacheEnabled,
		Directory:   settings.CacheDirectory,
		TTL:         ttl,
		MaxSize:     maxSize,
		Namespace:   namespace,
		ToolVersion: diskCacheToolVersion,
		Gzip:        settings.CacheGzip,
	})
	if err != nil {
		s.logDiskCacheOpenWarningOnce(err)
		cache = nil
	}
	if cache != nil {
		reset, schemaErr := cache.EnsureReferenceSchema(diskReferenceCacheSchemaVersion)
		if schemaErr != nil {
			s.logServerWarning("[asp-lsp] analysisDatabase.referenceSchema.failed" + formatLogFields(map[string]any{
				"action": "ignored", "schemaVersion": diskReferenceCacheSchemaVersion,
			}))
		} else if reset {
			s.logAnalysisDatabaseEvent("referenceSchema", "reset", map[string]any{
				"action": "rebuild", "reason": "missingOrStale", "schemaVersion": diskReferenceCacheSchemaVersion,
			})
		}
	}
	s.workspaceIndexDiskCommitMu.Lock()
	s.mu.Lock()
	if s.shutdown {
		s.mu.Unlock()
		s.workspaceIndexDiskCommitMu.Unlock()
		if cache != nil {
			_ = cache.Close()
		}
		return nil, settings
	}
	s.diskAnalysisCache = cache
	s.mu.Unlock()
	s.workspaceIndexDiskCommitMu.Unlock()
	if cache == nil {
		return nil, settings
	}
	return cache, settings
}

func boundedDiskCacheTTLHours(value int) int {
	if value <= 0 {
		return defaultDiskCacheTTLHours
	}
	return min(value, maxDiskCacheTTLHours)
}

func boundedDiskCacheMaxSizeMB(value int) int {
	if value <= 0 {
		return defaultDiskCacheMaxSizeMB
	}
	return min(value, maxDiskCacheMaxSizeMB)
}

func (s *Server) resolveCacheDirectory(configured string, workspaceRoots []string) string {
	trustedRoots := append(append([]string(nil), workspaceRoots...), os.TempDir())
	if configured == "" {
		return trustedDefaultCacheDirectory()
	}
	if pathHasParentTraversal(configured) {
		return trustedDefaultCacheDirectory()
	}
	candidate := configured
	if !filepath.IsAbs(candidate) {
		base := ""
		if len(workspaceRoots) > 0 {
			base = workspaceRoots[0]
		}
		if base == "" {
			base = os.TempDir()
		}
		candidate = filepath.Join(base, candidate)
	}
	resolved, ok := trustedPathForRoots(candidate, trustedRoots)
	if !ok {
		return trustedDefaultCacheDirectory()
	}
	if info, err := os.Stat(resolved); err == nil && !info.IsDir() {
		return trustedDefaultCacheDirectory()
	} else if err != nil && !os.IsNotExist(err) {
		return trustedDefaultCacheDirectory()
	}
	return resolved
}

func (s *Server) normalizeConfiguredCacheDirectory(configured string) string {
	configured = strings.TrimSpace(configured)
	if configured == "" {
		return ""
	}
	s.mu.Lock()
	roots := make([]string, 0, len(s.workspaceRoots)+1)
	for _, root := range s.workspaceRoots {
		if root.Path != "" {
			roots = append(roots, root.Path)
		}
	}
	if len(roots) == 0 && s.rootPath != "" {
		roots = append(roots, s.rootPath)
	}
	s.mu.Unlock()
	if len(roots) == 0 {
		if cwd, err := os.Getwd(); err == nil && cwd != "" {
			roots = append(roots, cwd)
		}
	}
	return s.resolveCacheDirectory(configured, roots)
}

func trustedDefaultCacheDirectory() string {
	tempRoot := os.TempDir()
	candidate := filepath.Join(tempRoot, "asp-lsp-analysis-cache")
	resolved, ok := trustedPathForRoots(candidate, []string{tempRoot})
	if !ok {
		return ""
	}
	return resolved
}

func (s *Server) logDiskCacheOpenWarningOnce(err error) {
	if err == nil {
		return
	}
	s.mu.Lock()
	if s.diskCacheOpenWarningLogged {
		s.mu.Unlock()
		return
	}
	s.diskCacheOpenWarningLogged = true
	s.mu.Unlock()
	s.logServerWarning("[asp-lsp] analysisDatabase.open.failed")
}

// closeDiskAnalysisCache flushes and closes the active database exactly once.
func (s *Server) registeredParsedCache() workspacepkg.RegisteredCache {
	return &serverRegisteredCache{name: "parsedDocuments", priority: 10,
		estimate: func() (int64, int) {
			// Parsed documents guard their own analysis state, so only the
			// entry snapshot needs Server.mu.
			s.mu.Lock()
			entries := maps.Clone(s.parsedCache)
			s.mu.Unlock()
			ownership := parsedDocumentCacheOwnershipForEntries(entries)
			return ownership.bytes(), ownership.entries
		},
		evict: func(target int64) int64 {
			s.mu.Lock()
			keys := sortedMapKeys(s.parsedCache)
			ledger := s.runtimeCacheOwnerLedgerLocked()
			identities := s.documentStoreIdentityIndexLocked()
			beforeTotal := ledger.total()
			external := s.analysisExternalParsedOwnerSetLocked()
			removed := false
			var parsedAndStoreFreed int64
			for _, key := range keys {
				if _, ok := s.parsedCache[key]; !ok {
					continue
				}
				if target > 0 && removed && parsedAndStoreFreed >= target {
					break
				}
				entry := s.parsedCache[key]
				// Keep a skeleton in DocumentStore so the next request can reuse
				// identity/text metadata without retaining the full analysis graph.
				s.demoteParsedCacheEntryLocked(key, entry, identities, ledger)
				delete(s.parsedCache, key)
				ledger.removeParsedEntry(key)
				s.advanceParsedCacheRevisionLocked(key)
				removed = true
				parsedAndStoreFreed = subtractRuntimeCacheBytes(beforeTotal, ledger.total())
				if target > 0 && parsedAndStoreFreed >= target {
					break
				}
			}
			if !removed {
				s.mu.Unlock()
				return 0
			}
			// Releasing a parsed revision invalidates all analysis layers that
			// refer to it. Keep the cache lock nested under Server.mu so the
			// ownership snapshot above remains stable. The parsed/store owner
			// bytes are excluded from this release because they were already
			// accounted for by beforeTotal and afterTotal.
			var analysisFreed int64
			if s.analysisCache != nil {
				analysisFreed = s.analysisCache.evictWithExternalParsed(0, external)
			}
			s.mu.Unlock()
			return addRuntimeCacheBytes(parsedAndStoreFreed, analysisFreed)
		},
	}
}

func (s *Server) registeredDocumentStoreCache() workspacepkg.RegisteredCache {
	return &serverRegisteredCache{name: "documentStore", priority: 12,
		estimate: func() (int64, int) {
			s.mu.Lock()
			defer s.mu.Unlock()
			parsed := parsedDocumentCacheOwnershipForEntries(s.parsedCache)
			ownership := documentStoreCacheOwnershipForStore(s.documentStore, parsed)
			return ownership.bytes(), ownership.entries
		},
		evict: func(target int64) int64 {
			s.mu.Lock()
			defer s.mu.Unlock()
			if s.documentStore == nil {
				return 0
			}
			// The ledger also holds parsed-cache owners, which stay constant here,
			// so its delta equals the DocumentStore delta. Analysis bytes depend
			// on the whole owner set and are settled once after the loop.
			ledger := s.runtimeCacheOwnerLedgerLocked()
			beforeLedger := ledger.total()
			beforeAnalysis := s.analysisCacheBytesLocked()
			keys := sortedMapKeys(s.documentStoreCache())
			var freed int64
			for _, key := range keys {
				cached, ok := s.documentStore.Cache[key]
				if !ok {
					continue
				}
				if target > 0 && freed >= target {
					break
				}
				ledger.removeStoreEntry(key)
				if cached != nil && s.documentStoreEntryIsOpenLocked(cached) {
					s.demoteDocumentStoreEntryLocked(cached)
					ledger.addStoreEntry(key, cached)
				} else {
					s.documentStore.Delete(key)
				}
				freed = subtractRuntimeCacheBytes(beforeLedger, ledger.total())
			}
			return subtractRuntimeCacheBytes(addRuntimeCacheBytes(beforeLedger, beforeAnalysis), addRuntimeCacheBytes(ledger.total(), s.analysisCacheBytesLocked()))
		},
	}
}

func (s *Server) documentStoreCache() map[string]*workspacepkg.CachedDocument {
	if s == nil || s.documentStore == nil {
		return nil
	}
	return s.documentStore.Cache
}

func (s *Server) documentStoreEntryIsOpenLocked(cached *workspacepkg.CachedDocument) bool {
	if cached == nil {
		return false
	}
	return s.openDocumentByURILocked(cached.URI) != nil || s.workspaceDocumentByURILocked(cached.URI) != nil
}

func (s *Server) demoteDocumentStoreEntryLocked(cached *workspacepkg.CachedDocument) bool {
	if s == nil || s.documentStore == nil || cached == nil {
		return false
	}
	return s.documentStore.Demote(cached, 0, func(uri, text string) any {
		language := "VBScript"
		if parsed, ok := cached.Parsed.(*core.ParsedDocument); ok && parsed.DefaultLanguage != "" {
			language = string(parsed.DefaultLanguage)
		}
		return skeletonParsedDocument(uri, text, language)
	})
}

// parsedDocumentCacheOwnership tracks the owners charged to the parsed cache.
// Parsed revisions and their source/runtime backings may be shared by several
// cache entries, so each backing is counted once in the aggregate estimate.
type parsedDocumentCacheOwnership struct {
	entries    int
	entryBytes int64
	parsed     map[*core.ParsedDocument]int64
	source     map[parsedDocumentSourceOwner]int64
	runtime    map[any]int64
}

type parsedDocumentSourceOwner struct {
	pointer uintptr
	length  int
}

func parsedDocumentCacheOwnershipForEntries(entries map[string]parsedDocumentCacheEntry) parsedDocumentCacheOwnership {
	// Size the owner maps up front; rehashing them dominated estimates of a
	// large parsed cache.
	runtimeOwners := 0
	for _, entry := range entries {
		if entry.Parsed != nil {
			runtimeOwners += len(entry.Parsed.RuntimeAnalysisMemoryOwners())
		}
	}
	ownership := parsedDocumentCacheOwnership{
		entries: len(entries),
		parsed:  make(map[*core.ParsedDocument]int64, len(entries)),
		source:  make(map[parsedDocumentSourceOwner]int64, len(entries)),
		runtime: make(map[any]int64, runtimeOwners),
	}
	for key, entry := range entries {
		ownership.entryBytes = addRuntimeCacheBytes(ownership.entryBytes, int64(len(key))*2+128)
		ownership.addEntry(entry)
	}
	return ownership
}

func (ownership *parsedDocumentCacheOwnership) addEntry(entry parsedDocumentCacheEntry) {
	if entry.Parsed == nil {
		ownership.addSource(entry.Text)
		ownership.entryBytes = addRuntimeCacheBytes(ownership.entryBytes, int64(len(entry.DefaultLanguage))*2+16)
		return
	}
	ownership.addParsed(entry.Parsed)
	if !stringBackingShared(entry.Text, entry.Parsed.Text) {
		ownership.addSource(entry.Text)
	}
}

func (ownership *parsedDocumentCacheOwnership) addParsed(parsed *core.ParsedDocument) {
	if parsed == nil {
		return
	}
	if _, exists := ownership.parsed[parsed]; exists {
		return
	}
	ownership.parsed[parsed] = nonNegativeParsedDocumentBytes(parsed.EstimateStructuralBytesWithoutRevisionText())
	ownership.addSource(parsed.Text)
	if previous, ok := parsed.PreviousRevisionText(); ok {
		ownership.addSource(previous)
	}
	for _, owner := range parsed.RuntimeAnalysisMemoryOwners() {
		ownership.addRuntime(owner.Identity, owner.Bytes)
	}
}

func (ownership *parsedDocumentCacheOwnership) addSource(text string) {
	owner, ok := parsedDocumentSourceOwnerForText(text)
	if !ok {
		return
	}
	bytes := int64(len(text))*2 + 16
	if current := ownership.source[owner]; bytes > current {
		ownership.source[owner] = bytes
	}
}

func (ownership *parsedDocumentCacheOwnership) addRuntime(identity any, bytes int64) {
	if identity == nil {
		return
	}
	if bytes < 0 {
		bytes = 0
	}
	if bytes > ownership.runtime[identity] {
		ownership.runtime[identity] = bytes
	}
}

func (ownership parsedDocumentCacheOwnership) bytes() int64 {
	bytes := nonNegativeParsedDocumentBytes(ownership.entryBytes)
	for _, ownerBytes := range ownership.parsed {
		bytes = addRuntimeCacheBytes(bytes, ownerBytes)
	}
	for _, ownerBytes := range ownership.source {
		bytes = addRuntimeCacheBytes(bytes, ownerBytes)
	}
	for _, ownerBytes := range ownership.runtime {
		bytes = addRuntimeCacheBytes(bytes, ownerBytes)
	}
	return nonNegativeParsedDocumentBytes(bytes)
}

func parsedDocumentSourceOwnerForText(text string) (parsedDocumentSourceOwner, bool) {
	if text == "" {
		return parsedDocumentSourceOwner{}, false
	}
	pointer := uintptr(unsafe.Pointer(unsafe.StringData(text)))
	if pointer == 0 {
		return parsedDocumentSourceOwner{}, false
	}
	return parsedDocumentSourceOwner{pointer: pointer, length: len(text)}, true
}

// documentStoreCacheOwnership describes the one registered layer that keeps
// document metadata, source text, and the parsed/runtime payload after the
// parsed cache has had first ownership. Its parsed/source/runtime maps exclude
// owners already present in parsedCache.
type documentStoreCacheOwnership struct {
	entries    int
	entryBytes int64
	parsed     map[*core.ParsedDocument]int64
	source     map[parsedDocumentSourceOwner]int64
	runtime    map[any]int64
}

func documentStoreCacheOwnershipForStore(store *workspacepkg.DocumentStore, excluded parsedDocumentCacheOwnership) documentStoreCacheOwnership {
	ownership := documentStoreCacheOwnership{
		parsed:  make(map[*core.ParsedDocument]int64),
		source:  make(map[parsedDocumentSourceOwner]int64),
		runtime: make(map[any]int64),
	}
	if store == nil {
		return ownership
	}
	for key, cached := range store.Cache {
		ownership.entries++
		ownership.entryBytes = addRuntimeCacheBytes(ownership.entryBytes, estimateDocumentStoreEntryMetadataBytes(key, cached))
		if cached == nil {
			continue
		}
		ownership.entryBytes = addRuntimeCacheBytes(ownership.entryBytes, estimateDocumentStoreAuxiliaryBytes(cached))
		if parsed, ok := cached.Parsed.(*core.ParsedDocument); ok {
			ownership.addParsed(parsed, excluded)
		} else if cached.Parsed != nil {
			ownership.entryBytes = addRuntimeCacheBytes(ownership.entryBytes, workspacepkg.EstimateJSONBytes(cached.Parsed, 256))
		}
		ownership.addSource(cached.Text, excluded)
	}
	return ownership
}

func (ownership documentStoreCacheOwnership) bytes() int64 {
	bytes := nonNegativeParsedDocumentBytes(ownership.entryBytes)
	for _, ownerBytes := range ownership.parsed {
		bytes = addRuntimeCacheBytes(bytes, ownerBytes)
	}
	for _, ownerBytes := range ownership.source {
		bytes = addRuntimeCacheBytes(bytes, ownerBytes)
	}
	for _, ownerBytes := range ownership.runtime {
		bytes = addRuntimeCacheBytes(bytes, ownerBytes)
	}
	return bytes
}

func (ownership *documentStoreCacheOwnership) addParsed(parsed *core.ParsedDocument, excluded parsedDocumentCacheOwnership) {
	if parsed == nil {
		return
	}
	if _, alreadyOwned := excluded.parsed[parsed]; alreadyOwned {
		return
	}
	if _, alreadyAdded := ownership.parsed[parsed]; alreadyAdded {
		return
	}
	ownership.parsed[parsed] = nonNegativeParsedDocumentBytes(parsed.EstimateStructuralBytesWithoutRevisionText())
	ownership.addSource(parsed.Text, excluded)
	if previous, ok := parsed.PreviousRevisionText(); ok {
		ownership.addSource(previous, excluded)
	}
	for _, owner := range parsed.RuntimeAnalysisMemoryOwners() {
		if _, alreadyOwned := excluded.runtime[owner.Identity]; alreadyOwned {
			continue
		}
		if current := ownership.runtime[owner.Identity]; owner.Bytes > current {
			ownership.runtime[owner.Identity] = nonNegativeParsedDocumentBytes(owner.Bytes)
		}
	}
}

func (ownership *documentStoreCacheOwnership) addSource(text string, excluded parsedDocumentCacheOwnership) {
	owner, ok := parsedDocumentSourceOwnerForText(text)
	if !ok {
		return
	}
	ownership.addSourceOwner(owner, int64(len(text))*2+16, excluded.source)
}

func (ownership *documentStoreCacheOwnership) addSourceOwner(owner parsedDocumentSourceOwner, bytes int64, excluded map[parsedDocumentSourceOwner]int64) {
	if _, alreadyOwned := excluded[owner]; alreadyOwned {
		return
	}
	bytes = nonNegativeParsedDocumentBytes(bytes)
	if current := ownership.source[owner]; bytes > current {
		ownership.source[owner] = bytes
	}
}

func documentStoreParsedOwnerSet(store *workspacepkg.DocumentStore) map[*core.ParsedDocument]struct{} {
	owners := map[*core.ParsedDocument]struct{}{}
	if store == nil {
		return owners
	}
	for _, cached := range store.Cache {
		if cached == nil {
			continue
		}
		if parsed, ok := cached.Parsed.(*core.ParsedDocument); ok && parsed != nil {
			owners[parsed] = struct{}{}
		}
	}
	return owners
}

func estimateDocumentStoreEntryMetadataBytes(key string, cached *workspacepkg.CachedDocument) int64 {
	bytes := int64(len(key))*2 + 64
	if cached == nil {
		return bytes
	}
	bytes = addRuntimeCacheBytes(bytes, int64(len(cached.URI)+len(cached.ParseDepth))*2+64)
	// Version, generation, access timestamps, demotion timestamp, and the
	// materialization flag are scalar fields retained with every entry.
	return addRuntimeCacheBytes(bytes, 64)
}

func estimateDocumentStoreAuxiliaryBytes(cached *workspacepkg.CachedDocument) int64 {
	if cached == nil {
		return 0
	}
	var bytes int64
	if cached.Virtuals != nil {
		bytes = addRuntimeCacheBytes(bytes, workspacepkg.EstimateJSONBytes(cached.Virtuals, 256))
	}
	if cached.Analysis != nil {
		bytes = addRuntimeCacheBytes(bytes, workspacepkg.EstimateJSONBytes(cached.Analysis, 256))
	}
	if cached.CSSContext != nil {
		bytes = addRuntimeCacheBytes(bytes, workspacepkg.EstimateJSONBytes(cached.CSSContext, 256))
	}
	return bytes
}

func (s *Server) analysisCacheBytesLocked() int64 {
	if s == nil || s.analysisCache == nil {
		return 0
	}
	bytes, _ := s.analysisCache.memoryEstimateWithExternalParsed(s.analysisExternalParsedOwnerSetLocked())
	return bytes
}

func addRuntimeCacheBytes(left, right int64) int64 {
	left = nonNegativeParsedDocumentBytes(left)
	right = nonNegativeParsedDocumentBytes(right)
	if right > math.MaxInt64-left {
		return math.MaxInt64
	}
	return left + right
}

func subtractRuntimeCacheBytes(total, retained int64) int64 {
	total = nonNegativeParsedDocumentBytes(total)
	retained = nonNegativeParsedDocumentBytes(retained)
	if retained >= total {
		return 0
	}
	return total - retained
}

func nonNegativeParsedDocumentBytes(bytes int64) int64 {
	if bytes < 0 {
		return 0
	}
	return bytes
}

func estimateParsedDocumentCacheEntryBytes(key string, entry parsedDocumentCacheEntry) int64 {
	return parsedDocumentCacheOwnershipForEntries(map[string]parsedDocumentCacheEntry{key: entry}).bytes()
}

func stringBackingShared(first, second string) bool {
	if len(first) == 0 || len(second) == 0 {
		return len(first) == 0 && len(second) == 0
	}
	return len(first) == len(second) && unsafe.StringData(first) == unsafe.StringData(second)
}

func (s *Server) registeredSemanticCache() workspacepkg.RegisteredCache {
	return &serverRegisteredCache{name: "semanticTokens", priority: 20,
		estimate: func() (int64, int) {
			s.mu.Lock()
			defer s.mu.Unlock()
			var bytes int64
			for key, entry := range s.semantic {
				bytes += int64(len(key))*2 + int64(len(entry.Tokens.Data))*8 + 256
			}
			for resultID, data := range s.semanticHistory {
				bytes += int64(len(resultID))*2 + int64(len(data))*8 + 64
			}
			return bytes, len(s.semantic) + len(s.semanticHistory)
		},
		evict: func(target int64) int64 {
			s.mu.Lock()
			defer s.mu.Unlock()
			keys := sortedMapKeys(s.semantic)
			var freed int64
			for _, key := range keys {
				entry := s.semantic[key]
				delete(s.semantic, key)
				freed += int64(len(key))*2 + int64(len(entry.Tokens.Data))*8 + 256
				prefix := key + "#"
				for resultID, data := range s.semanticHistory {
					if strings.HasPrefix(resultID, prefix) {
						freed += int64(len(resultID))*2 + int64(len(data))*8 + 64
						delete(s.semanticHistory, resultID)
					}
				}
				if freed >= target {
					break
				}
			}
			if target <= 0 || freed < target {
				for _, resultID := range sortedMapKeys(s.semanticHistory) {
					data := s.semanticHistory[resultID]
					freed += int64(len(resultID))*2 + int64(len(data))*8 + 64
					delete(s.semanticHistory, resultID)
					if target > 0 && freed >= target {
						break
					}
				}
			}
			return freed
		},
	}
}

func (s *Server) registeredGraphCache() workspacepkg.RegisteredCache {
	return &serverRegisteredCache{name: "graphPayloads", priority: 30,
		estimate: func() (int64, int) {
			s.mu.Lock()
			defer s.mu.Unlock()
			return workspacepkg.EstimateJSONBytes(s.graphCache, 4096), len(s.graphCache)
		},
		evict: func(target int64) int64 {
			s.mu.Lock()
			defer s.mu.Unlock()
			keys := sortedMapKeys(s.graphCache)
			var freed int64
			for _, key := range keys {
				bytes := workspacepkg.EstimateJSONBytes(s.graphCache[key], 4096)
				delete(s.graphCache, key)
				freed += bytes
				if freed >= target {
					break
				}
			}
			return freed
		},
	}
}

func (s *Server) registeredReferenceCache() workspacepkg.RegisteredCache {
	return &serverRegisteredCache{name: "workspaceReferences", priority: 40,
		estimate: func() (int64, int) {
			s.mu.Lock()
			var bytes int64
			for key, locations := range s.referenceResults {
				bytes += estimateWorkspaceReferenceResultBytes(key, locations)
			}
			bytes += int64(len(s.referenceCounts)+len(s.referencePartialCounts)) * 96
			bytes += int64(len(s.referencePreviousCounts)+len(s.referenceNameRevisions)) * 96
			for _, documents := range s.referenceDocuments {
				bytes += int64(len(documents)) * 16
			}
			for _, scope := range s.referenceScopes {
				bytes += int64(len(scope.DocumentKeys)+len(scope.Membership)) * 64
			}
			for _, plan := range s.referenceDeclarationPlans {
				bytes += int64(len(plan.declarations))*256 + int64(len(plan.memberOwners))*32
			}
			for _, fingerprints := range s.referenceDescriptorFingerprints {
				bytes += 128 + int64(len(fingerprints.names))*160
			}
			nameIndexBytes, nameIndexEntries := s.estimateWorkspaceReferenceNameIndexLocked()
			bytes += nameIndexBytes
			bytes += int64(len(s.referenceCountSummariesRestored)) * 16
			bytes += int64(len(s.referenceBatch)+len(s.referenceInflight)) * 256
			entries := len(s.referenceResults) + len(s.referenceCounts) + len(s.referencePartialCounts) + len(s.referencePreviousCounts) + len(s.referenceDocuments) + len(s.referenceScopes) + len(s.referenceDeclarationPlans) + len(s.referenceDescriptorFingerprints) + len(s.referenceCountSummariesRestored) + len(s.referenceBatch) + len(s.referenceInflight) + nameIndexEntries
			s.mu.Unlock()
			indexBytes, indexEntries := s.referenceWorkspaceIndex.estimateMemory()
			return bytes + indexBytes, entries + indexEntries
		},
		evict: func(target int64) int64 {
			s.mu.Lock()
			defer s.mu.Unlock()
			freed, _ := s.estimateWorkspaceReferenceNameIndexLocked()
			s.resetWorkspaceReferenceNameIndexLocked()
			keys := sortedWorkspaceReferenceKeys(s.referenceResults)
			for _, key := range keys {
				bytes := estimateWorkspaceReferenceResultBytes(key, s.referenceResults[key])
				delete(s.referenceResults, key)
				freed += bytes
				if freed >= target {
					break
				}
			}
			if freed > 0 {
				for _, state := range s.referenceBatch {
					if state.cancel != nil {
						state.cancel()
					}
				}
				s.referenceBatch = map[workspaceReferenceBatchKey]*workspaceReferenceBatchState{}
			}
			if freed < target {
				for _, state := range s.referenceBatch {
					if state.cancel != nil {
						state.cancel()
					}
				}
				s.referenceBatch = map[workspaceReferenceBatchKey]*workspaceReferenceBatchState{}
				indexBytes, _ := s.referenceWorkspaceIndex.estimateMemory()
				s.referenceWorkspaceIndex.clear()
				s.referenceDocuments = map[string][]*core.ParsedDocument{}
				s.referenceScopes = map[workspaceReferenceScopeCacheKey]workspaceReferenceScopeSnapshot{}
				s.referenceImplicitPlans = map[workspaceReferenceImplicitPlanKey]map[string]map[string]struct{}{}
				s.referenceDeclarationPlans = map[*core.ParsedDocument]workspaceReferenceDeclarationPlan{}
				s.referenceDescriptorFingerprints = map[string]*workspaceReferenceDescriptorFingerprintCache{}
				s.referenceCountSummariesRestored = map[*core.ParsedDocument]struct{}{}
				freed += indexBytes
			}
			if freed < target {
				for _, key := range sortedWorkspaceReferenceKeys(s.referenceCounts) {
					delete(s.referenceCounts, key)
					freed += 64
					if freed >= target {
						break
					}
				}
			}
			return freed
		},
	}
}

func (s *Server) estimateWorkspaceReferenceNameIndexLocked() (int64, int) {
	if !s.referenceNameIndexReady {
		return 0, 0
	}
	var bytes int64
	entries := len(s.referenceUnnamedTargets)
	bytes += int64(len(s.referenceUnnamedTargets)) * 128
	for name, targets := range s.referenceTargetsByName {
		bytes += int64(len(name))*2 + 64 + int64(len(targets))*128
		entries += len(targets)
	}
	for name, batches := range s.referenceBatchesByName {
		bytes += int64(len(name))*2 + 64 + int64(len(batches))*192
		entries += len(batches)
	}
	for name, plans := range s.referenceImplicitPlansByName {
		bytes += int64(len(name))*2 + 64 + int64(len(plans))*128
		entries += len(plans)
	}
	return bytes, entries
}

func estimateWorkspaceReferenceResultBytes(key workspaceReferenceTargetKey, locations []lsp.Location) int64 {
	bytes := int64(len(key.URI)+len(key.Name)+len(key.SymbolKind))*2 + 96
	for _, location := range locations {
		bytes += int64(len(location.URI))*2 + 48
	}
	return bytes
}

func sortedWorkspaceReferenceKeys[V any](values map[workspaceReferenceTargetKey]V) []workspaceReferenceTargetKey {
	keys := make([]workspaceReferenceTargetKey, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].URI != keys[j].URI {
			return keys[i].URI < keys[j].URI
		}
		if keys[i].Name != keys[j].Name {
			return keys[i].Name < keys[j].Name
		}
		if keys[i].Line != keys[j].Line {
			return keys[i].Line < keys[j].Line
		}
		if keys[i].Character != keys[j].Character {
			return keys[i].Character < keys[j].Character
		}
		return keys[i].SymbolKind < keys[j].SymbolKind
	})
	return keys
}

func sortedMapKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
