package lspserver

import (
	"context"
	"io/fs"
	"path/filepath"
	"sync"
	"time"

	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

// workspaceListedStats serves file stats from one listing per directory.
// Validating a warm workspace index stats every indexed file, while listing
// their directories costs one call per directory; on Windows a listing already
// carries each file's size and time. A file the listing does not show as a
// regular file falls back to fsStat, which also reports why it is missing.
type workspaceListedStats struct {
	server     *Server
	gateway    *workspacepkg.FsGateway
	generation int
	mu         sync.Mutex
	dirs       map[string]*workspaceListedDirectory
}

type workspaceListedDirectory struct {
	done    chan struct{}
	entries map[string]fs.DirEntry
}

func newWorkspaceListedStats(s *Server) *workspaceListedStats {
	listed := &workspaceListedStats{server: s, dirs: map[string]*workspaceListedDirectory{}}
	s.mu.Lock()
	listed.gateway = s.fsGateway
	s.mu.Unlock()
	if listed.gateway != nil {
		listed.generation = listed.gateway.Generation()
	}
	return listed
}

func (l *workspaceListedStats) stat(ctx context.Context, path string) (*workspacepkg.FsGatewayStats, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	cleaned := filepath.Clean(path)
	if l.gateway != nil {
		// Another pass may have stated or listed this file already.
		if stats, ok := l.gateway.CachedStat(cleaned); ok && stats.File {
			return stats, true
		}
	}
	if entries := l.directory(ctx, filepath.Dir(cleaned)); entries != nil {
		if entry, ok := entries[filepath.Base(cleaned)]; ok && entry.Type().IsRegular() {
			if info, err := entry.Info(); err == nil && info.Mode().IsRegular() {
				stats := workspacepkg.FsGatewayStats{MtimeMS: info.ModTime().UnixMilli(), Size: info.Size(), File: true}
				if l.gateway != nil {
					l.gateway.RememberStat(cleaned, stats, l.generation)
				}
				return &stats, true
			}
		}
	}
	return l.server.fsStatContext(ctx, cleaned)
}

func (l *workspaceListedStats) directory(ctx context.Context, directory string) map[string]fs.DirEntry {
	l.mu.Lock()
	listing := l.dirs[directory]
	if listing != nil {
		l.mu.Unlock()
		select {
		case <-listing.done:
			return listing.entries
		case <-ctx.Done():
			return nil
		}
	}
	listing = &workspaceListedDirectory{done: make(chan struct{})}
	l.dirs[directory] = listing
	l.mu.Unlock()
	defer close(listing.done)
	trusted, ok := l.server.trustedFilesystemPathContext(ctx, directory)
	if !ok {
		return nil
	}
	entries, err := l.server.trustedFilesystemReadDir(trusted)
	if err != nil || ctx.Err() != nil {
		return nil
	}
	byName := make(map[string]fs.DirEntry, len(entries))
	for _, entry := range entries {
		byName[entry.Name()] = entry
	}
	listing.entries = byName
	return byName
}

// sourceSnapshotStats returns the size and modification time of the bytes
// behind path's valid source snapshot.
func (s *Server) sourceSnapshotStats(path string) (*workspacepkg.FsGatewayStats, bool) {
	key := sourceSnapshotKey(path)
	s.mu.Lock()
	snapshot := s.sourceSnapshots[key]
	if snapshot == nil || !snapshot.valid {
		s.mu.Unlock()
		return nil, false
	}
	stats := &workspacepkg.FsGatewayStats{MtimeMS: time.Unix(0, snapshot.modifiedAt).UnixMilli(), Size: snapshot.size, File: true}
	s.mu.Unlock()
	return stats, true
}

// ownerSourceStats returns path's metadata from its source snapshot, or from
// the filesystem when no snapshot is valid.
func (s *Server) ownerSourceStats(path string) (*workspacepkg.FsGatewayStats, bool) {
	if stats, ok := s.sourceSnapshotStats(path); ok {
		return stats, true
	}
	return s.fsStat(path)
}
