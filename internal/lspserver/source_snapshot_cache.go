package lspserver

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

const maxSourceFileBytes int64 = 16 * 1024 * 1024

type sourceReadBoundaryContextKey struct{}
type sourceReadRootsContextKey struct{}
type sourceReadGenerationContextKey struct{}

var (
	errSourceFileTooLarge           = errors.New("source file exceeds maximum size")
	errSourceFileSymlink            = errors.New("source file is a symlink")
	errSourceFileNotRegular         = errors.New("source path is not a regular file")
	errWorkspacePathOutsideBoundary = errors.New("source path is outside the configured workspace boundaries")
)

type sourceFileSnapshot struct {
	path       string
	raw        []byte
	decoded    map[string]string
	loadedAt   time.Time
	size       int64
	modifiedAt int64
	generation uint64
	valid      bool
}

type sourceReadInflight struct {
	done       chan struct{}
	raw        []byte
	err        error
	generation uint64
}

type sourceFileMetadata struct {
	size       int64
	modifiedAt int64
}

func sourceSnapshotKey(path string) string {
	return workspacepkg.FileIdentityKeyFromURI(filePathURI(filepath.Clean(path)))
}

func (s *Server) readSourceFileBytes(ctx context.Context, path string, limiter chan struct{}) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	cleanPath := filepath.Clean(path)
	trustedRoots, preparedRoots := sourceReadRoots(ctx)
	if preparedGeneration, ok := sourceReadGeneration(ctx); ok {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !s.workspaceIndexGenerationCurrent(ctx, preparedGeneration) {
			return nil, errWorkspaceIndexGeneration
		}
	}
	if preparedRoots {
		if !sourcePathWithinPreparedRoots(cleanPath, trustedRoots) {
			return nil, &os.PathError{Op: "read", Path: cleanPath, Err: errWorkspacePathOutsideBoundary}
		}
	} else if !s.workspaceSourcePathAllowed(cleanPath) && !workspacePathWithinAnyBoundary(cleanPath, nil, nil, sourceReadBoundaries(ctx)) {
		return nil, &os.PathError{Op: "read", Path: cleanPath, Err: errWorkspacePathOutsideBoundary}
	}
	key := sourceSnapshotKey(cleanPath)
	s.mu.Lock()
	snapshot := s.sourceSnapshots[key]
	if snapshot != nil && snapshot.valid {
		raw := snapshot.raw
		generation := snapshot.generation
		s.mu.Unlock()
		fields := map[string]any{"bytes": len(raw), "generation": generation, "path": cleanPath}
		s.logDebugVerboseEvent("sourceSnapshot", "[asp-lsp] sourceSnapshot.hit"+formatLogFields(fields), fields)
		return bytes.Clone(raw), nil
	}
	generation := sourceSnapshotGeneration(snapshot)
	if inflight := s.sourceReadInflight[key]; inflight != nil && inflight.generation == generation {
		s.mu.Unlock()
		select {
		case <-inflight.done:
			return bytes.Clone(inflight.raw), inflight.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	inflight := &sourceReadInflight{done: make(chan struct{}), generation: generation}
	s.sourceReadInflight[key] = inflight
	testHook := s.workspaceFileReadTestHook
	s.mu.Unlock()

	if limiter != nil {
		select {
		case limiter <- struct{}{}:
			defer func() { <-limiter }()
		case <-ctx.Done():
			s.finishSourceRead(key, cleanPath, inflight, nil, sourceFileMetadata{}, false, ctx.Err())
			return nil, ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		s.finishSourceRead(key, cleanPath, inflight, nil, sourceFileMetadata{}, false, err)
		return nil, err
	}
	if testHook != nil {
		testHook(cleanPath)
	}
	started := time.Now()
	if !preparedRoots {
		trustedRoots = s.trustedFilesystemRootEntries(sourceReadBoundaries(ctx))
	}
	var raw []byte
	var after sourceFileMetadata
	var stable bool
	var err error
	if preparedRoots {
		raw, after, stable, err = readStableSourceFileWithPreparedRoots(cleanPath, trustedRoots)
	} else {
		raw, after, stable, err = readStableSourceFile(cleanPath, trustedRoots)
	}
	if err == nil {
		fields := map[string]any{
			"bytes": len(raw), "cached": stable, "durationMs": float64(time.Since(started).Microseconds()) / 1000, "path": cleanPath,
		}
		s.logDebugVerboseEvent("sourceSnapshot", "[asp-lsp] sourceSnapshot.read"+formatLogFields(fields), fields)
	}
	s.finishSourceRead(key, cleanPath, inflight, raw, after, stable, err)
	return raw, err
}

func withSourceReadBoundaries(ctx context.Context, boundaries ...string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	merged := append([]string(nil), sourceReadBoundaries(ctx)...)
	for _, boundary := range boundaries {
		if boundary != "" {
			merged = append(merged, filepath.Clean(boundary))
		}
	}
	if len(merged) == 0 {
		return ctx
	}
	return context.WithValue(ctx, sourceReadBoundaryContextKey{}, merged)
}

func sourceReadBoundaries(ctx context.Context) []string {
	if ctx == nil {
		return nil
	}
	boundaries, _ := ctx.Value(sourceReadBoundaryContextKey{}).([]string)
	return boundaries
}

func withSourceReadRoots(ctx context.Context, roots []trustedFilesystemRoot) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	prepared := append([]trustedFilesystemRoot(nil), roots...)
	return context.WithValue(ctx, sourceReadRootsContextKey{}, prepared)
}

func withSourceReadGeneration(ctx context.Context, generation uint64) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, sourceReadGenerationContextKey{}, generation)
}

func sourceReadGeneration(ctx context.Context) (uint64, bool) {
	if ctx == nil {
		return 0, false
	}
	generation, ok := ctx.Value(sourceReadGenerationContextKey{}).(uint64)
	return generation, ok
}

func sourceReadRoots(ctx context.Context) ([]trustedFilesystemRoot, bool) {
	if ctx == nil {
		return nil, false
	}
	roots, ok := ctx.Value(sourceReadRootsContextKey{}).([]trustedFilesystemRoot)
	return roots, ok
}

func sourcePathWithinPreparedRoots(path string, roots []trustedFilesystemRoot) bool {
	if pathHasParentTraversal(path) {
		return false
	}
	cleaned, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return false
	}
	root, ok := longestTrustedPathRoot(filepath.Clean(cleaned), roots)
	if !ok {
		return false
	}
	return trustedFilesystemRootPathCurrent(root) && !pathContainsSymlinkWithinRoot(filepath.Clean(cleaned), root.path)
}

func readStableSourceFile(path string, roots []trustedFilesystemRoot) ([]byte, sourceFileMetadata, bool, error) {
	root, relative, ok := openTrustedFilesystemPathWithRoots(path, roots)
	if !ok {
		return nil, sourceFileMetadata{}, false, &os.PathError{Op: "read", Path: path, Err: errWorkspacePathOutsideBoundary}
	}
	defer root.Close()
	return readStableSourceFileFromRoot(path, root, relative)
}

func readStableSourceFileWithPreparedRoots(path string, roots []trustedFilesystemRoot) ([]byte, sourceFileMetadata, bool, error) {
	rootEntry, relative, ok := trustedFilesystemRelativePath(path, roots)
	if !ok || rootEntry.opened == nil || !trustedFilesystemRootPathCurrent(rootEntry) {
		return nil, sourceFileMetadata{}, false, &os.PathError{Op: "read", Path: path, Err: errWorkspacePathOutsideBoundary}
	}
	raw, metadata, stable, err := readStableSourceFileFromRoot(path, rootEntry.opened, relative)
	if err != nil {
		return raw, metadata, stable, err
	}
	if !trustedFilesystemRootPathCurrent(rootEntry) {
		return nil, sourceFileMetadata{}, false, &os.PathError{Op: "read", Path: path, Err: errWorkspacePathOutsideBoundary}
	}
	return raw, metadata, stable, nil
}

func readStableSourceFileFromRoot(path string, root *os.Root, relative string) ([]byte, sourceFileMetadata, bool, error) {
	pathInfo, err := root.Lstat(relative)
	if err != nil {
		return nil, sourceFileMetadata{}, false, err
	}
	if pathInfo.Mode()&os.ModeSymlink != 0 {
		return nil, sourceFileMetadata{}, false, &os.PathError{Op: "read", Path: path, Err: errSourceFileSymlink}
	}
	if !pathInfo.Mode().IsRegular() {
		return nil, sourceFileMetadata{}, false, &os.PathError{Op: "read", Path: path, Err: errSourceFileNotRegular}
	}
	if pathInfo.Size() > maxSourceFileBytes {
		return nil, sourceFileMetadata{}, false, &os.PathError{Op: "read", Path: path, Err: errSourceFileTooLarge}
	}
	file, err := root.Open(relative)
	if err != nil {
		return nil, sourceFileMetadata{}, false, err
	}
	defer file.Close()
	beforeInfo, err := file.Stat()
	if err != nil || !beforeInfo.Mode().IsRegular() || !os.SameFile(pathInfo, beforeInfo) {
		if err == nil {
			err = errSourceFileNotRegular
		}
		return nil, sourceFileMetadata{}, false, &os.PathError{Op: "read", Path: path, Err: err}
	}
	raw, err := io.ReadAll(io.LimitReader(file, maxSourceFileBytes+1))
	if err != nil {
		return nil, sourceFileMetadata{}, false, err
	}
	if int64(len(raw)) > maxSourceFileBytes {
		return nil, sourceFileMetadata{}, false, &os.PathError{Op: "read", Path: path, Err: errSourceFileTooLarge}
	}
	afterInfo, err := file.Stat()
	if err != nil {
		return raw, sourceFileMetadata{}, false, err
	}
	if !afterInfo.Mode().IsRegular() {
		return raw, sourceFileMetadata{}, false, &os.PathError{Op: "read", Path: path, Err: errSourceFileNotRegular}
	}
	if afterInfo.Size() > maxSourceFileBytes {
		return nil, sourceFileMetadata{}, false, &os.PathError{Op: "read", Path: path, Err: errSourceFileTooLarge}
	}
	before := sourceFileMetadata{size: beforeInfo.Size(), modifiedAt: beforeInfo.ModTime().UnixNano()}
	after := sourceFileMetadata{size: afterInfo.Size(), modifiedAt: afterInfo.ModTime().UnixNano()}
	return raw, after, before == after && int64(len(raw)) == after.size, nil
}

func (s *Server) finishSourceRead(key, path string, inflight *sourceReadInflight, raw []byte, metadata sourceFileMetadata, cacheable bool, err error) {
	s.mu.Lock()
	currentInflight := s.sourceReadInflight[key]
	currentGeneration := sourceSnapshotGeneration(s.sourceSnapshots[key])
	current := currentInflight == inflight && currentGeneration == inflight.generation
	if cacheable && current {
		cachedRaw := bytes.Clone(raw)
		s.sourceSnapshots[key] = &sourceFileSnapshot{
			path: filepath.Clean(path), raw: cachedRaw, decoded: map[string]string{}, loadedAt: time.Now(), size: metadata.size, modifiedAt: metadata.modifiedAt, generation: inflight.generation, valid: true,
		}
		inflight.raw = cachedRaw
	} else {
		inflight.raw = raw
	}
	inflight.err = err
	if currentInflight == inflight {
		delete(s.sourceReadInflight, key)
	}
	close(inflight.done)
	s.mu.Unlock()
}

func (s *Server) sourceSnapshotMetadataCurrent(path string) (known bool, current bool) {
	key := sourceSnapshotKey(path)
	s.mu.Lock()
	snapshot := s.sourceSnapshots[key]
	if snapshot == nil || !snapshot.valid || snapshot.modifiedAt == 0 {
		s.mu.Unlock()
		return false, false
	}
	size := snapshot.size
	modifiedAt := snapshot.modifiedAt
	s.mu.Unlock()
	cleanPath := filepath.Clean(path)
	info, err := s.trustedFilesystemLstat(cleanPath)
	if err != nil || !info.Mode().IsRegular() || !s.workspaceSourcePathAllowed(cleanPath) {
		return true, false
	}
	return true, info.Size() == size && info.ModTime().UnixNano() == modifiedAt
}

func pathContainsSymlinkWithinRoot(path, root string) bool {
	cleanPath := filepath.Clean(path)
	cleanRoot := filepath.Clean(root)
	relative, err := filepath.Rel(cleanRoot, cleanPath)
	if err != nil || relative == ".." || filepath.IsAbs(relative) ||
		len(relative) >= 3 && relative[:3] == ".."+string(filepath.Separator) {
		return true
	}
	for current := cleanPath; ; current = filepath.Dir(current) {
		if workspacepkg.FileIdentityKeyFromFileName(current) == workspacepkg.FileIdentityKeyFromFileName(cleanRoot) {
			// A configured workspace root may itself be a symlink. The root is
			// the trust boundary; only symlinks below it can escape that boundary.
			return false
		}
		if info, err := os.Lstat(current); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return true
		}
		parent := filepath.Dir(current)
		if parent == current {
			return true
		}
	}
}

func (s *Server) invalidateChangedSourceSnapshots() {
	s.mu.Lock()
	paths := make([]string, 0, len(s.sourceSnapshots))
	for _, snapshot := range s.sourceSnapshots {
		if snapshot != nil && snapshot.valid && snapshot.path != "" {
			paths = append(paths, snapshot.path)
		}
	}
	s.mu.Unlock()
	for _, path := range paths {
		if _, current := s.sourceSnapshotMetadataCurrent(path); !current {
			s.invalidateSourceSnapshot(path)
		}
	}
}

func sourceSnapshotGeneration(snapshot *sourceFileSnapshot) uint64 {
	if snapshot == nil {
		return 0
	}
	return snapshot.generation
}

func (s *Server) readWorkspaceTextFileCached(ctx context.Context, path, legacyEncoding string, limiter chan struct{}, preferDocuments bool) (string, error) {
	cleanPath := filepath.Clean(path)
	uri := filePathURI(cleanPath)
	key := sourceSnapshotKey(cleanPath)
	if preferDocuments {
		s.mu.Lock()
		if doc := s.openDocumentByURILocked(uri); doc != nil {
			text := doc.Text
			s.mu.Unlock()
			return text, nil
		}
		if doc := s.workspaceDocumentByURILocked(uri); doc != nil {
			text := doc.Text
			s.mu.Unlock()
			return text, nil
		}
		if s.documentStore != nil {
			if cached := s.documentStore.CachedDocumentForURI(uri); cached != nil && cached.Text != "" {
				text := cached.Text
				s.mu.Unlock()
				return text, nil
			}
		}
		s.mu.Unlock()
	}
	s.mu.Lock()
	if snapshot := s.sourceSnapshots[key]; snapshot != nil && snapshot.valid {
		if text, ok := snapshot.decoded[legacyEncoding]; ok {
			s.mu.Unlock()
			return text, nil
		}
	}
	s.mu.Unlock()
	raw, err := s.readSourceFileBytes(ctx, cleanPath, limiter)
	if err != nil {
		return "", err
	}
	text, err := workspacepkg.DecodeLegacy(raw, legacyEncoding)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	if snapshot := s.sourceSnapshots[key]; snapshot != nil && snapshot.valid && bytes.Equal(snapshot.raw, raw) {
		if snapshot.decoded == nil {
			snapshot.decoded = map[string]string{}
		}
		snapshot.path = cleanPath
		snapshot.decoded[legacyEncoding] = text
		s.rememberSourceTextLocked(uri, text)
	}
	s.mu.Unlock()
	return text, nil
}

func (s *Server) readWorkspaceTextFileWithinBoundaries(ctx context.Context, path string, boundaries ...string) (string, error) {
	s.mu.Lock()
	limiter := s.includeReadLimiter
	legacyEncoding := s.settings.LegacyEncoding
	s.mu.Unlock()
	return s.readWorkspaceTextFileCached(withSourceReadBoundaries(ctx, boundaries...), path, legacyEncoding, limiter, true)
}

func (s *Server) workspaceDocumentByURILocked(uri string) *core.TextDocument {
	if doc := s.workspace[uri]; doc != nil {
		return doc
	}
	for candidateURI, doc := range s.workspace {
		if workspacepkg.SameFileIdentityURI(candidateURI, uri) {
			return doc
		}
	}
	return nil
}

func (s *Server) rememberSourceTextLocked(uri, text string) {
	if s.documentStore == nil {
		return
	}
	cached := s.documentStore.CachedDocumentForURI(uri)
	if cached == nil {
		cached = &workspacepkg.CachedDocument{URI: uri}
		s.documentStore.Cache[uri] = cached
	}
	if cached.Text != text {
		cached.Text = text
		cached.Version = 0
		cached.Parsed = nil
		cached.ParseDepth = "skeleton"
		cached.Generation++
	}
	s.documentStore.Touch(cached, 0)
}

func (s *Server) invalidateSourceSnapshot(path string) {
	cleanPath := filepath.Clean(path)
	uri := filePathURI(cleanPath)
	key := sourceSnapshotKey(cleanPath)
	s.mu.Lock()
	if inflight := s.sourceReadInflight[key]; inflight != nil {
		generation := sourceSnapshotGeneration(s.sourceSnapshots[key])
		if inflight.generation > generation {
			generation = inflight.generation
		}
		s.sourceSnapshots[key] = &sourceFileSnapshot{path: cleanPath, decoded: map[string]string{}, generation: generation + 1}
	} else {
		delete(s.sourceSnapshots, key)
	}
	if s.documentStore != nil {
		s.documentStore.DeleteCachedDocumentsForURI(uri)
	}
	s.mu.Unlock()
	s.logDebugVerboseEvent("sourceSnapshot", "[asp-lsp] sourceSnapshot.invalidate path="+cleanPath, map[string]any{"path": cleanPath})
}

func (s *Server) clearSourceSnapshots() {
	s.mu.Lock()
	s.clearSourceSnapshotsLocked()
	if s.documentStore != nil {
		s.documentStore = workspacepkg.NewDocumentStore()
	}
	s.mu.Unlock()
}

func (s *Server) clearSourceSnapshotsLocked() {
	snapshots := make(map[string]*sourceFileSnapshot, len(s.sourceReadInflight))
	for key, inflight := range s.sourceReadInflight {
		generation := sourceSnapshotGeneration(s.sourceSnapshots[key])
		if inflight.generation > generation {
			generation = inflight.generation
		}
		snapshots[key] = &sourceFileSnapshot{generation: generation + 1}
	}
	s.sourceSnapshots = snapshots
}
