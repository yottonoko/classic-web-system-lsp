package lspserver

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

const (
	defaultLocalReadDirCacheTTL   = 5 * time.Second
	defaultNetworkStatCacheTTL    = 30 * time.Second
	defaultNetworkNegativeStatTTL = 5 * time.Second
	defaultNetworkReadDirCacheTTL = 30 * time.Second
	maxNetworkCacheTTL            = 24 * time.Hour
	maxNetworkIncludeConcurrency  = 256
)

type resolvedNetworkProfile struct {
	Kind                   string
	StatTTL                time.Duration
	NegativeStatTTL        time.Duration
	ReadDirTTL             time.Duration
	IncludeReadConcurrency int
	CaseResolution         string
}

type osFsGatewayBackend struct {
	server *Server
}

func (backend osFsGatewayBackend) Stat(fileName string) (workspacepkg.FsGatewayStats, error) {
	var (
		info os.FileInfo
		err  error
	)
	if backend.server != nil {
		info, err = backend.server.trustedFilesystemStat(fileName)
	} else {
		info, err = os.Stat(fileName)
	}
	if err != nil {
		return workspacepkg.FsGatewayStats{}, err
	}
	return workspacepkg.FsGatewayStats{
		MtimeMS:   info.ModTime().UnixMilli(),
		Size:      info.Size(),
		File:      !info.IsDir(),
		Directory: info.IsDir(),
	}, nil
}

func (backend osFsGatewayBackend) ReadDir(directory string) ([]workspacepkg.FsGatewayDirent, error) {
	var (
		entries []os.DirEntry
		err     error
	)
	if backend.server != nil {
		entries, err = backend.server.trustedFilesystemReadDir(directory)
	} else {
		entries, err = os.ReadDir(directory)
	}
	if err != nil {
		return nil, err
	}
	result := make([]workspacepkg.FsGatewayDirent, 0, len(entries))
	for _, entry := range entries {
		result = append(result, workspacepkg.FsGatewayDirent{
			Name:      entry.Name(),
			File:      !entry.IsDir(),
			Directory: entry.IsDir(),
		})
	}
	return result, nil
}

func (s *Server) configureFsGateway() {
	profile := s.resolveNetworkProfile()
	options := workspacepkg.FsGatewayOptions{
		StatTTL:         profile.StatTTL,
		NegativeStatTTL: profile.NegativeStatTTL,
		ReadDirTTL:      profile.ReadDirTTL,
	}
	s.mu.Lock()
	if s.fsGateway == nil {
		s.fsGateway = workspacepkg.NewFsGateway(osFsGatewayBackend{server: s}, options)
	} else {
		s.fsGateway.Configure(options, osFsGatewayBackend{server: s})
	}
	s.includeReadLimiter = make(chan struct{}, max(1, profile.IncludeReadConcurrency))
	s.mu.Unlock()
	s.trustedPaths.setTTL(profile.StatTTL)
}

func (s *Server) resolveNetworkProfile() resolvedNetworkProfile {
	s.mu.Lock()
	settings := struct {
		NetworkProfile                string
		NetworkStatCacheTTLMS         int
		NetworkReadDirCacheTTLMS      int
		NetworkIncludeReadConcurrency int
		NetworkCaseResolution         string
	}{
		s.settings.NetworkProfile,
		s.settings.NetworkStatCacheTTLMS,
		s.settings.NetworkReadDirCacheTTLMS,
		s.settings.NetworkIncludeReadConcurrency,
		s.settings.NetworkCaseResolution,
	}
	paths := make([]string, 0, len(s.workspaceRoots)+len(s.settings.VirtualRoots)+len(s.settings.IncludePaths)+1)
	for _, root := range s.workspaceRoots {
		paths = append(paths, root.Path)
	}
	paths = append(paths, s.settings.VirtualRoots...)
	paths = append(paths, s.settings.IncludePaths...)
	paths = append(paths, s.settings.VirtualRoot)
	s.mu.Unlock()
	network := settings.NetworkProfile == "network"
	if settings.NetworkProfile == "auto" || settings.NetworkProfile == "" {
		for _, path := range paths {
			if looksLikeNetworkPath(path) {
				network = true
				break
			}
		}
	}
	profile := resolvedNetworkProfile{
		Kind:                   "local",
		ReadDirTTL:             defaultLocalReadDirCacheTTL,
		IncludeReadConcurrency: boundedIncludeReadConcurrency(runtime.NumCPU()),
		CaseResolution:         "full",
	}
	if network {
		profile = resolvedNetworkProfile{
			Kind:                   "network",
			StatTTL:                defaultNetworkStatCacheTTL,
			NegativeStatTTL:        defaultNetworkNegativeStatTTL,
			ReadDirTTL:             defaultNetworkReadDirCacheTTL,
			IncludeReadConcurrency: boundedIncludeReadConcurrency(runtime.NumCPU()),
			CaseResolution:         "fast",
		}
	}
	if settings.NetworkStatCacheTTLMS >= 0 {
		profile.StatTTL = boundedNetworkCacheTTL(settings.NetworkStatCacheTTLMS)
		profile.NegativeStatTTL = min(profile.StatTTL, defaultNetworkNegativeStatTTL)
	}
	if settings.NetworkReadDirCacheTTLMS >= 0 {
		profile.ReadDirTTL = boundedNetworkCacheTTL(settings.NetworkReadDirCacheTTLMS)
	}
	if settings.NetworkIncludeReadConcurrency > 0 {
		profile.IncludeReadConcurrency = boundedIncludeReadConcurrency(settings.NetworkIncludeReadConcurrency)
	}
	if settings.NetworkCaseResolution == "full" || settings.NetworkCaseResolution == "fast" {
		profile.CaseResolution = settings.NetworkCaseResolution
	}
	return profile
}

func boundedNetworkCacheTTL(milliseconds int) time.Duration {
	if milliseconds <= 0 {
		return 0
	}
	maximum := maxNetworkCacheTTL / time.Millisecond
	if int64(milliseconds) > int64(maximum) {
		return maxNetworkCacheTTL
	}
	return time.Duration(milliseconds) * time.Millisecond
}

func boundedIncludeReadConcurrency(value int) int {
	if value <= 0 {
		value = 1
	}
	if value > maxNetworkIncludeConcurrency {
		return maxNetworkIncludeConcurrency
	}
	return value
}

func looksLikeNetworkPath(fileName string) bool {
	normalized := strings.ReplaceAll(fileName, "\\", "/")
	return strings.HasPrefix(fileName, `\\`) || strings.HasPrefix(normalized, "//") ||
		strings.HasPrefix(normalized, "/Volumes/") || strings.HasPrefix(normalized, "/mnt/") ||
		strings.HasPrefix(normalized, "/net/") || isRemoteDrivePath(fileName)
}

func (s *Server) fsStat(path string) (*workspacepkg.FsGatewayStats, bool) {
	return s.fsStatContext(context.Background(), path)
}

func (s *Server) fsStatContext(ctx context.Context, path string) (*workspacepkg.FsGatewayStats, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil, false
	}
	trustedPath, ok := s.trustedFilesystemPath(path)
	if !ok {
		return nil, false
	}
	s.mu.Lock()
	gateway := s.fsGateway
	s.mu.Unlock()
	if gateway == nil {
		info, err := s.trustedFilesystemStat(trustedPath)
		if err != nil || ctx.Err() != nil {
			return nil, false
		}
		return &workspacepkg.FsGatewayStats{MtimeMS: info.ModTime().UnixMilli(), Size: info.Size(), File: !info.IsDir(), Directory: info.IsDir()}, true
	}
	return gateway.StatContext(ctx, trustedPath)
}

func (s *Server) fsReadDir(path string) (*workspacepkg.FsGatewayDirectoryListing, bool) {
	return s.fsReadDirContext(context.Background(), path)
}

func (s *Server) fsReadDirContext(ctx context.Context, path string) (*workspacepkg.FsGatewayDirectoryListing, bool) {
	if ctx.Err() != nil {
		return nil, false
	}
	trustedPath, ok := s.trustedFilesystemPath(path)
	if !ok {
		return nil, false
	}
	s.mu.Lock()
	gateway := s.fsGateway
	s.mu.Unlock()
	if gateway == nil {
		gateway = workspacepkg.NewFsGateway(osFsGatewayBackend{server: s}, workspacepkg.FsGatewayOptions{})
	}
	return gateway.ReadDirContext(ctx, trustedPath)
}

func (s *Server) invalidateFsPath(path string) {
	s.trustedPaths.forget(path)
	if strings.EqualFold(filepath.Base(filepath.Clean(path)), ".gitignore") {
		s.invalidateGitIgnoreGlobs()
	}
	trustedPath, ok := s.trustedFilesystemPath(path)
	if !ok {
		return
	}
	s.mu.Lock()
	gateway := s.fsGateway
	s.mu.Unlock()
	if gateway != nil {
		gateway.InvalidatePath(trustedPath)
	}
}

func (s *Server) resolveCaseInsensitivePath(path string) (string, bool) {
	return s.resolveCaseInsensitivePathContext(context.Background(), path)
}

func (s *Server) resolveCaseInsensitivePathContext(ctx context.Context, path string) (string, bool) {
	if ctx.Err() != nil {
		return "", false
	}
	cleaned := filepath.Clean(path)
	root, _, ok := s.trustedFilesystemRootForPath(cleaned)
	if !ok {
		return "", false
	}
	relative, err := filepath.Rel(root, cleaned)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", false
	}
	current := root
	if relative == "." {
		stats, ok := s.fsStatContext(ctx, current)
		return current, ok && stats != nil
	}
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		listing, ok := s.fsReadDirContext(ctx, current)
		if !ok {
			return "", false
		}
		matches := listing.ByLowerName[strings.ToLower(part)]
		if len(matches) != 1 {
			return "", false
		}
		current = filepath.Join(current, matches[0].Name)
	}
	stats, ok := s.fsStatContext(ctx, current)
	return current, ok && stats != nil
}

func (s *Server) includeCaseResolutionEnabled() bool {
	profile := s.resolveNetworkProfile()
	return profile.CaseResolution != "fast"
}

func (s *Server) effectiveCacheFreshness() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.effectiveCacheFreshnessLocked()
}

func (s *Server) effectiveCacheFreshnessLocked() string {
	value := strings.ToLower(strings.TrimSpace(s.settings.CacheFreshness))
	if value != "" && value != "auto" {
		return value
	}
	network := s.settings.NetworkProfile == "network"
	if s.settings.NetworkProfile == "auto" || s.settings.NetworkProfile == "" {
		paths := make([]string, 0, len(s.workspaceRoots)+len(s.settings.VirtualRoots)+len(s.settings.IncludePaths)+1)
		for _, root := range s.workspaceRoots {
			paths = append(paths, root.Path)
		}
		paths = append(paths, s.settings.VirtualRoots...)
		paths = append(paths, s.settings.IncludePaths...)
		paths = append(paths, s.settings.VirtualRoot)
		for _, path := range paths {
			if looksLikeNetworkPath(path) {
				network = true
				break
			}
		}
	}
	if network {
		return "watch"
	}
	return "metadata"
}

type trustedFilesystemRoot struct {
	path        string
	canonical   string
	info        os.FileInfo
	opened      *os.Root
	directories *preparedDirectoryRoots
}

type trustedFilesystemOpenRootTestHook struct {
	fn func(string)
}

var trustedFilesystemOpenRootTestHooks atomic.Pointer[trustedFilesystemOpenRootTestHook]

func (s *Server) trustedFilesystemRootEntries(extra []string) []trustedFilesystemRoot {
	roots, _ := s.trustedFilesystemRootEntriesContext(context.Background(), extra)
	return roots
}

func (s *Server) trustedFilesystemRootEntriesContext(ctx context.Context, extra []string) ([]trustedFilesystemRoot, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil, false
	}
	return s.trustedFilesystemRootEntriesForPathsContext(ctx, s.trustedFilesystemRootPaths(extra))
}

// trustedFilesystemLongestRootContext returns the longest valid root that
// contains path. It validates only the roots containing path, longest first,
// which matches choosing the longest root among all validated roots.
func (s *Server) trustedFilesystemLongestRootContext(ctx context.Context, path string) (trustedFilesystemRoot, bool, bool) {
	candidates := []string{}
	for _, absolute := range normalizedTrustedRootPaths(s.trustedFilesystemRootPaths(nil)) {
		if pathWithinRoot(absolute, path) {
			candidates = append(candidates, absolute)
		}
	}
	sort.SliceStable(candidates, func(left, right int) bool { return len(candidates[left]) > len(candidates[right]) })
	for _, candidate := range candidates {
		roots, complete := s.trustedFilesystemRootEntriesForPathsContext(ctx, []string{candidate})
		if !complete {
			return trustedFilesystemRoot{}, false, false
		}
		if len(roots) > 0 {
			return roots[0], true, true
		}
	}
	return trustedFilesystemRoot{}, false, ctx.Err() == nil
}

func (s *Server) trustedFilesystemRootPaths(extra []string) []string {
	s.mu.Lock()
	workspaceRoots := append([]workspaceRoot(nil), s.workspaceRoots...)
	rootPath := s.rootPath
	includePaths := append([]string(nil), s.settings.IncludePaths...)
	virtualRoots := append([]string(nil), s.settings.VirtualRoots...)
	virtualRoot := s.settings.VirtualRoot
	openDocumentRoots := s.openDocumentBoundaryRootsLocked()
	s.mu.Unlock()

	paths := make([]string, 0, len(workspaceRoots)+len(includePaths)+len(virtualRoots)+2)
	hasWorkspaceRoot := false
	for _, root := range workspaceRoots {
		if root.Path != "" {
			paths = append(paths, root.Path)
			hasWorkspaceRoot = true
		}
	}
	if rootPath != "" {
		paths = append(paths, rootPath)
		hasWorkspaceRoot = true
	}
	paths = append(paths, includePaths...)
	paths = append(paths, virtualRoots...)
	if virtualRoot != "" {
		paths = append(paths, virtualRoot)
	}
	paths = append(paths, openDocumentRoots...)
	// A server without an initialized workspace still needs to serve direct
	// filesystem requests in tests and standalone use, but the fallback must
	// not turn the entire host filesystem into a readable root.
	if !hasWorkspaceRoot {
		paths = append(paths, os.TempDir())
	}
	return append(paths, extra...)
}

func normalizedTrustedRootPaths(paths []string) []string {
	absolutes := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, raw := range paths {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || pathHasParentTraversal(trimmed) {
			continue
		}
		absolute, err := filepath.Abs(trimmed)
		if err != nil {
			continue
		}
		absolute = filepath.Clean(absolute)
		if _, ok := seen[absolute]; ok {
			continue
		}
		seen[absolute] = struct{}{}
		absolutes = append(absolutes, absolute)
	}
	return absolutes
}

func (s *Server) resetTrustedFilesystemRootCacheLocked() {
	s.trustedFilesystemRootCache = nil
	s.trustedFilesystemRootCacheGeneration++
}

// trustedFilesystemRootEntriesForPathsContext validates each configured root
// against the identity first authorized for it. The symlink and stat calls run
// without s.mu because every filesystem access re-validates the roots; only
// the authorization cache is read and updated under the lock.
func (s *Server) trustedFilesystemRootEntriesForPathsContext(ctx context.Context, paths []string) ([]trustedFilesystemRoot, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	absolutes := normalizedTrustedRootPaths(paths)
	s.mu.Lock()
	generation := s.trustedFilesystemRootCacheGeneration
	authorized := make(map[string]trustedFilesystemRoot, len(absolutes))
	for _, absolute := range absolutes {
		if cached, ok := s.trustedFilesystemRootCache[absolute]; ok {
			authorized[absolute] = cached
		}
	}
	s.mu.Unlock()

	roots := make([]trustedFilesystemRoot, 0, len(absolutes))
	var added []trustedFilesystemRoot
	for _, absolute := range absolutes {
		if ctx.Err() != nil {
			return nil, false
		}
		if cached, ok := authorized[absolute]; ok && cached.info != nil {
			// A root that still is the directory first authorized for it needs
			// no symlink walk; any other outcome takes the full check below.
			if info, err := s.trustedRootStat(absolute); err == nil && info.IsDir() && os.SameFile(cached.info, info) {
				roots = append(roots, cached)
				continue
			}
		}
		canonical, err := evalTrustedRootPath(absolute)
		if ctx.Err() != nil {
			return nil, false
		}
		if err != nil {
			continue
		}
		canonical = filepath.Clean(canonical)
		info, err := os.Stat(canonical)
		if ctx.Err() != nil {
			return nil, false
		}
		if err != nil || !info.IsDir() {
			continue
		}
		// Materialize the file ID before a later root replacement can make
		// Windows os.SameFile reopen the path and observe a different directory.
		if !os.SameFile(info, info) {
			continue
		}
		if cached, ok := authorized[absolute]; ok {
			// The configured root changed identity after authorization. Do not
			// silently authorize the replacement target.
			if trustedFilesystemRootIdentityMatches(cached, canonical, info) {
				roots = append(roots, cached)
			}
			continue
		}
		entry := trustedFilesystemRoot{path: absolute, canonical: canonical, info: info}
		added = append(added, entry)
		roots = append(roots, entry)
	}
	if ctx.Err() != nil {
		return nil, false
	}
	if len(added) == 0 {
		return roots, true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.trustedFilesystemRootCacheGeneration != generation {
		return roots, true
	}
	if s.trustedFilesystemRootCache == nil {
		s.trustedFilesystemRootCache = make(map[string]trustedFilesystemRoot)
	}
	for _, entry := range added {
		cached, ok := s.trustedFilesystemRootCache[entry.path]
		if !ok {
			s.trustedFilesystemRootCache[entry.path] = entry
			continue
		}
		// A concurrent caller authorized this root first; keep its identity.
		for index := range roots {
			if roots[index].path != entry.path || roots[index].info != entry.info {
				continue
			}
			if trustedFilesystemRootIdentityMatches(cached, entry.canonical, entry.info) {
				roots[index] = cached
			} else {
				roots = append(roots[:index], roots[index+1:]...)
			}
			break
		}
	}
	return roots, true
}

// trustedRootStatCoalescer shares root stat results between concurrent
// callers without serving stale data: a caller only uses a stat that started
// after the caller arrived. Every filesystem access re-validates its root, so
// parallel workers would otherwise contend on the same directory in the kernel.
type trustedRootStatCoalescer struct {
	tickets atomic.Uint64
	roots   sync.Map
}

type trustedRootStatState struct {
	mu       sync.Mutex
	inflight *trustedRootStatCall
	last     *trustedRootStatCall
}

type trustedRootStatCall struct {
	ticket uint64
	done   chan struct{}
	info   os.FileInfo
	err    error
}

func (c *trustedRootStatCoalescer) stat(path string) (os.FileInfo, error) {
	arrived := c.tickets.Load()
	value, _ := c.roots.LoadOrStore(path, &trustedRootStatState{})
	state := value.(*trustedRootStatState)
	for {
		state.mu.Lock()
		if last := state.last; last != nil && last.ticket > arrived {
			state.mu.Unlock()
			return last.info, last.err
		}
		if call := state.inflight; call != nil {
			state.mu.Unlock()
			<-call.done
			if call.ticket > arrived {
				return call.info, call.err
			}
			continue
		}
		call := &trustedRootStatCall{ticket: c.tickets.Add(1), done: make(chan struct{})}
		state.inflight = call
		state.mu.Unlock()
		call.info, call.err = os.Stat(path)
		state.mu.Lock()
		state.inflight = nil
		state.last = call
		state.mu.Unlock()
		close(call.done)
		return call.info, call.err
	}
}

// trustedRootStat re-validates a root. Network profiles reuse a recent stat;
// otherwise every caller sees a stat that started after it arrived.
func (s *Server) trustedRootStat(path string) (os.FileInfo, error) {
	if s.trustedPaths.enabled() {
		return s.trustedPaths.statDirectory(path)
	}
	return s.trustedRootStats.stat(path)
}

func trustedFilesystemRootIdentityMatches(cached trustedFilesystemRoot, canonical string, info os.FileInfo) bool {
	return cached.canonical == canonical && cached.info != nil && os.SameFile(cached.info, info)
}

func (s *Server) trustedFilesystemRootForPath(path string) (string, string, bool) {
	return s.trustedFilesystemRootForPathContext(context.Background(), path)
}

func (s *Server) trustedFilesystemRootForPathContext(ctx context.Context, path string) (string, string, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return "", "", false
	}
	if pathHasParentTraversal(path) {
		return "", "", false
	}
	cleaned, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", "", false
	}
	cleaned = filepath.Clean(cleaned)
	root, ok, complete := s.trustedFilesystemLongestRootContext(ctx, cleaned)
	if !complete || !ok || ctx.Err() != nil {
		return "", "", false
	}
	if s.trustedPaths.pathContainsSymlinkWithinRoot(cleaned, root.path) {
		return "", "", false
	}
	if ctx.Err() != nil {
		return "", "", false
	}
	resolved, ok := s.trustedPaths.resolvePathForTrust(ctx, cleaned)
	if !ok || !pathWithinRoot(root.canonical, resolved) {
		return "", "", false
	}
	if ctx.Err() != nil {
		return "", "", false
	}
	return root.path, cleaned, true
}

func (s *Server) trustedFilesystemPath(path string) (string, bool) {
	return s.trustedFilesystemPathContext(context.Background(), path)
}

func (s *Server) trustedFilesystemPathContext(ctx context.Context, path string) (string, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return "", false
	}
	if pathHasParentTraversal(path) {
		return "", false
	}
	cleaned, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", false
	}
	cleaned = filepath.Clean(cleaned)
	root, ok, complete := s.trustedFilesystemLongestRootContext(ctx, cleaned)
	if !complete || !ok || ctx.Err() != nil {
		return "", false
	}
	if s.trustedPaths.pathContainsSymlinkWithinRoot(cleaned, root.path) {
		return "", false
	}
	if ctx.Err() != nil {
		return "", false
	}
	resolved, ok := s.trustedPaths.resolvePathForTrust(ctx, cleaned)
	if !ok || !pathWithinRoot(root.canonical, resolved) {
		return "", false
	}
	if ctx.Err() != nil {
		return "", false
	}
	return cleaned, true
}

// openTrustedFilesystemPath opens the trusted root that contains path. The
// returned release must be called once the root is no longer used.
func (s *Server) openTrustedFilesystemPath(path string) (*os.Root, string, func(), bool) {
	return s.trustedPaths.openPathWithRoots(path, s.trustedFilesystemRootEntries(nil))
}

func (c *trustedPathCache) openPathWithRoots(path string, roots []trustedFilesystemRoot) (*os.Root, string, func(), bool) {
	root, relative, ok := trustedFilesystemRelativePath(path, roots)
	if !ok {
		return nil, "", nil, false
	}
	rootHandle, release, ok := c.openRoot(root)
	if !ok {
		return nil, "", nil, false
	}
	return rootHandle, relative, release, true
}

func openTrustedFilesystemPathWithRoots(path string, roots []trustedFilesystemRoot) (*os.Root, string, bool) {
	root, relative, ok := trustedFilesystemRelativePath(path, roots)
	if !ok {
		return nil, "", false
	}
	rootHandle, ok := openTrustedFilesystemRoot(root)
	if !ok {
		return nil, "", false
	}
	return rootHandle, relative, true
}

func trustedFilesystemRelativePath(path string, roots []trustedFilesystemRoot) (trustedFilesystemRoot, string, bool) {
	if pathHasParentTraversal(path) {
		return trustedFilesystemRoot{}, "", false
	}
	cleaned, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return trustedFilesystemRoot{}, "", false
	}
	cleaned = filepath.Clean(cleaned)
	root, ok := longestTrustedPathRoot(cleaned, roots)
	if !ok {
		return trustedFilesystemRoot{}, "", false
	}
	relative, err := filepath.Rel(root.path, cleaned)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return trustedFilesystemRoot{}, "", false
	}
	if root.info == nil {
		return trustedFilesystemRoot{}, "", false
	}
	return root, filepath.ToSlash(relative), true
}

func openTrustedFilesystemRoot(root trustedFilesystemRoot) (*os.Root, bool) {
	if root.info == nil {
		return nil, false
	}
	if hook := trustedFilesystemOpenRootTestHooks.Load(); hook != nil && hook.fn != nil {
		hook.fn(root.canonical)
	}
	rootHandle, err := os.OpenRoot(root.canonical)
	if err != nil {
		return nil, false
	}
	openedInfo, err := rootHandle.Stat(".")
	if err != nil || !os.SameFile(root.info, openedInfo) {
		_ = rootHandle.Close()
		return nil, false
	}
	return rootHandle, true
}

func prepareTrustedFilesystemRoots(ctx context.Context, roots []trustedFilesystemRoot, indexedPaths []string) ([]trustedFilesystemRoot, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	prepared := append([]trustedFilesystemRoot(nil), roots...)
	for index := range prepared {
		if ctx.Err() != nil {
			closeTrustedFilesystemRoots(prepared)
			return nil, false
		}
		if !trustedFilesystemRootRelevantToPaths(prepared[index].path, indexedPaths) {
			continue
		}
		root, ok := openTrustedFilesystemRoot(prepared[index])
		if ok {
			prepared[index].opened = root
			prepared[index].directories = &preparedDirectoryRoots{}
		}
	}
	if ctx.Err() != nil {
		closeTrustedFilesystemRoots(prepared)
		return nil, false
	}
	return prepared, true
}

func trustedFilesystemRootRelevantToPaths(root string, indexedPaths []string) bool {
	cleanRoot, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return false
	}
	for _, indexedPath := range indexedPaths {
		cleanPath, err := filepath.Abs(filepath.Clean(indexedPath))
		if err != nil {
			continue
		}
		// A root is relevant when it is the indexed root or a nested root that
		// can win longest-root selection for an indexed file. Ancestor roots
		// are intentionally skipped when an indexed root is already explicit;
		// the explicit boundary is prepared instead.
		if pathWithinRoot(filepath.Clean(cleanPath), filepath.Clean(cleanRoot)) {
			return true
		}
	}
	return false
}

func closeTrustedFilesystemRoots(roots []trustedFilesystemRoot) {
	seen := make(map[*os.Root]struct{}, len(roots))
	for _, root := range roots {
		if root.opened == nil {
			continue
		}
		if _, ok := seen[root.opened]; ok {
			continue
		}
		seen[root.opened] = struct{}{}
		if root.directories != nil {
			root.directories.close()
		}
		_ = root.opened.Close()
	}
}

func (s *Server) trustedFilesystemStat(path string) (os.FileInfo, error) {
	root, relative, release, ok := s.openTrustedFilesystemPath(path)
	if !ok {
		return nil, &os.PathError{Op: "stat", Path: path, Err: os.ErrPermission}
	}
	defer release()
	return root.Stat(relative)
}

func (s *Server) trustedFilesystemLstat(path string) (os.FileInfo, error) {
	root, relative, release, ok := s.openTrustedFilesystemPath(path)
	if !ok {
		return nil, &os.PathError{Op: "lstat", Path: path, Err: os.ErrPermission}
	}
	defer release()
	return root.Lstat(relative)
}

func (s *Server) trustedFilesystemReadDir(path string) ([]os.DirEntry, error) {
	root, relative, release, ok := s.openTrustedFilesystemPath(path)
	if !ok {
		return nil, &os.PathError{Op: "readdir", Path: path, Err: os.ErrPermission}
	}
	defer release()
	return fs.ReadDir(root.FS(), relative)
}

func pathHasParentTraversal(path string) bool {
	for _, part := range strings.FieldsFunc(path, func(r rune) bool { return r == '/' || r == '\\' }) {
		if part == ".." {
			return true
		}
	}
	return false
}

func trustedPathRootsContext(ctx context.Context, paths []string) ([]trustedFilesystemRoot, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	roots := make([]trustedFilesystemRoot, 0, len(paths))
	seen := map[string]struct{}{}
	for _, raw := range paths {
		if ctx.Err() != nil {
			return nil, false
		}
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || pathHasParentTraversal(trimmed) {
			continue
		}
		absolute, err := filepath.Abs(trimmed)
		if err != nil {
			continue
		}
		absolute = filepath.Clean(absolute)
		canonical, err := evalTrustedRootPath(absolute)
		if ctx.Err() != nil {
			return nil, false
		}
		if err != nil {
			continue
		}
		canonical = filepath.Clean(canonical)
		info, err := os.Stat(canonical)
		if ctx.Err() != nil {
			return nil, false
		}
		if err != nil || !info.IsDir() {
			continue
		}
		key := absolute + "\x00" + canonical
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		roots = append(roots, trustedFilesystemRoot{path: absolute, canonical: canonical, info: info})
	}
	if ctx.Err() != nil {
		return nil, false
	}
	return roots, true
}

func evalTrustedRootPath(absolute string) (string, error) {
	canonical, err := filepath.EvalSymlinks(absolute)
	if err == nil {
		return filepath.Clean(canonical), nil
	}
	if runtime.GOOS == "windows" && errors.Is(err, fs.ErrPermission) {
		return filepath.Clean(absolute), nil
	}
	return "", err
}

func trustedPathForRoots(path string, roots []string) (string, bool) {
	return trustedPathForRootsContext(context.Background(), path, roots)
}

func trustedPathForRootsContext(ctx context.Context, path string, roots []string) (string, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return "", false
	}
	if pathHasParentTraversal(path) {
		return "", false
	}
	cleaned, err := filepath.Abs(filepath.Clean(path))
	if ctx.Err() != nil {
		return "", false
	}
	if err != nil {
		return "", false
	}
	cleaned = filepath.Clean(cleaned)
	trustedRoots, complete := trustedPathRootsContext(ctx, roots)
	if !complete || ctx.Err() != nil {
		return "", false
	}
	return trustedPreparedPathContext(ctx, cleaned, trustedRoots)
}

func trustedPathForPreparedRootsContext(ctx context.Context, path string, roots []trustedFilesystemRoot) (string, bool) {
	return trustedPathForPreparedRootsCachedContext(ctx, path, roots, nil)
}

// trustedPathForPreparedRootsCachedContext is trustedPathForPreparedRootsContext
// with symlink checks served from cache, which may be nil.
func trustedPathForPreparedRootsCachedContext(ctx context.Context, path string, roots []trustedFilesystemRoot, cache *trustedPathCache) (string, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil || pathHasParentTraversal(path) {
		return "", false
	}
	cleaned, err := filepath.Abs(filepath.Clean(path))
	if err != nil || ctx.Err() != nil {
		return "", false
	}
	return trustedPreparedPathCachedContext(ctx, filepath.Clean(cleaned), roots, cache)
}

func trustedPreparedPathContext(ctx context.Context, cleaned string, roots []trustedFilesystemRoot) (string, bool) {
	return trustedPreparedPathCachedContext(ctx, cleaned, roots, nil)
}

func trustedPreparedPathCachedContext(ctx context.Context, cleaned string, roots []trustedFilesystemRoot, cache *trustedPathCache) (string, bool) {
	root, ok := longestTrustedPathRootContext(ctx, cleaned, roots)
	if !ok {
		return "", false
	}
	if cache.pathContainsSymlinkWithinRoot(cleaned, root.path) {
		return "", false
	}
	if ctx.Err() != nil {
		return "", false
	}
	resolved, ok := cache.resolvePathForTrust(ctx, cleaned)
	if !ok || !pathWithinRoot(root.canonical, resolved) {
		return "", false
	}
	if ctx.Err() != nil {
		return "", false
	}
	return cleaned, true
}

func longestTrustedPathRoot(path string, roots []trustedFilesystemRoot) (trustedFilesystemRoot, bool) {
	return longestTrustedPathRootContext(context.Background(), path, roots)
}

func longestTrustedPathRootContext(ctx context.Context, path string, roots []trustedFilesystemRoot) (trustedFilesystemRoot, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	var best trustedFilesystemRoot
	found := false
	for _, root := range roots {
		if ctx.Err() != nil {
			return trustedFilesystemRoot{}, false
		}
		if !pathWithinRoot(root.path, path) {
			continue
		}
		if !found || len(root.path) > len(best.path) {
			best = root
			found = true
		}
	}
	return best, found
}

func resolvePathForTrustContext(ctx context.Context, path string) (string, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	cleaned := filepath.Clean(path)
	missing := []string{}
	for current := cleaned; ; current = filepath.Dir(current) {
		if ctx.Err() != nil {
			return "", false
		}
		if _, err := os.Lstat(current); err == nil {
			if ctx.Err() != nil {
				return "", false
			}
			resolved, err := filepath.EvalSymlinks(current)
			if ctx.Err() != nil {
				return "", false
			}
			if err != nil && runtime.GOOS == "windows" && errors.Is(err, fs.ErrPermission) {
				resolved = current
			} else if err != nil {
				return "", false
			}
			for index := len(missing) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, missing[index])
				if ctx.Err() != nil {
					return "", false
				}
			}
			return filepath.Clean(resolved), true
		} else if !os.IsNotExist(err) {
			return "", false
		}
		if ctx.Err() != nil {
			return "", false
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", false
		}
		missing = append(missing, filepath.Base(current))
	}
}
