package lspserver

import (
	"context"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

// gitIgnoreGlobsLocalTTL bounds how long local .gitignore rules are reused
// when a client does not report .gitignore changes.
const gitIgnoreGlobsLocalTTL = 2 * time.Second

// gitIgnoreGlobsCache keeps the .gitignore rules of each workspace root.
// Collecting them lists every directory under the root, and indexing, file
// events, navigation, and JavaScript discovery all ask for them. On a network
// drive each listing is a round trip.
type gitIgnoreGlobsCache struct {
	mu         sync.Mutex
	generation uint64
	entries    map[string]gitIgnoreGlobsCacheEntry
	inflight   map[string]*gitIgnoreGlobsScan
}

type gitIgnoreGlobsCacheEntry struct {
	generation uint64
	expires    time.Time
	globs      []string
}

type gitIgnoreGlobsScan struct {
	done     chan struct{}
	globs    []string
	complete bool
}

// invalidateGitIgnoreGlobs drops the cached rules after a .gitignore change.
func (s *Server) invalidateGitIgnoreGlobs() {
	cache := &s.gitIgnoreGlobs
	cache.mu.Lock()
	cache.generation++
	cache.entries = nil
	cache.mu.Unlock()
}

func (s *Server) gitIgnoreGlobsTTL() time.Duration {
	if ttl := time.Duration(s.trustedPaths.ttl.Load()); ttl > gitIgnoreGlobsLocalTTL {
		return ttl
	}
	return gitIgnoreGlobsLocalTTL
}

// readGitIgnoreGlobsContext returns the .gitignore rules under rootPath. Callers
// asking for the same root at the same time share one scan.
func (s *Server) readGitIgnoreGlobsContext(ctx context.Context, rootPath string) []string {
	if ctx == nil {
		ctx = context.Background()
	}
	key := filepath.Clean(rootPath)
	cache := &s.gitIgnoreGlobs
	for {
		cache.mu.Lock()
		generation := cache.generation
		if entry, ok := cache.entries[key]; ok && entry.generation == generation && time.Now().Before(entry.expires) {
			cache.mu.Unlock()
			return slices.Clone(entry.globs)
		}
		if scan := cache.inflight[key]; scan != nil {
			cache.mu.Unlock()
			select {
			case <-scan.done:
				if scan.complete {
					return slices.Clone(scan.globs)
				}
				// The scanning caller was cancelled; scan again for this one.
				continue
			case <-ctx.Done():
				return nil
			}
		}
		scan := &gitIgnoreGlobsScan{done: make(chan struct{})}
		if cache.inflight == nil {
			cache.inflight = map[string]*gitIgnoreGlobsScan{}
		}
		cache.inflight[key] = scan
		cache.mu.Unlock()

		globs := s.scanGitIgnoreGlobsContext(ctx, rootPath)
		scan.globs, scan.complete = globs, ctx.Err() == nil
		cache.mu.Lock()
		delete(cache.inflight, key)
		if scan.complete && cache.generation == generation {
			if cache.entries == nil {
				cache.entries = map[string]gitIgnoreGlobsCacheEntry{}
			}
			cache.entries[key] = gitIgnoreGlobsCacheEntry{generation: generation, expires: time.Now().Add(s.gitIgnoreGlobsTTL()), globs: globs}
		}
		cache.mu.Unlock()
		close(scan.done)
		return slices.Clone(globs)
	}
}
