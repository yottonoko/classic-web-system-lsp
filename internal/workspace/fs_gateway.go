package workspace

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type FsGatewayStats struct {
	MtimeMS   int64
	Size      int64
	File      bool
	Directory bool
}

type FsGatewayDirent struct {
	Name      string
	File      bool
	Directory bool
}

type FsGatewayBackend interface {
	Stat(fileName string) (FsGatewayStats, error)
	ReadDir(directory string) ([]FsGatewayDirent, error)
}

type FsGatewayOptions struct {
	StatTTL           time.Duration
	NegativeStatTTL   time.Duration
	ReadDirTTL        time.Duration
	StatMaxEntries    int
	ReadDirMaxEntries int
}

type FsGatewayDirectoryListing struct {
	Entries     []FsGatewayDirent
	ByLowerName map[string][]FsGatewayDirent
}

type fsCacheEntry[T any] struct {
	value     T
	ok        bool
	expiresAt time.Time
}

type fsCall[T any] struct {
	done       chan struct{}
	value      T
	ok         bool
	cacheEpoch int
}

type FsGateway struct {
	mu           sync.Mutex
	backend      FsGatewayBackend
	options      FsGatewayOptions
	statCache    fsLRU[FsGatewayStats]
	statInFlight map[string]*fsCall[FsGatewayStats]
	dirCache     fsLRU[FsGatewayDirectoryListing]
	dirInFlight  map[string]*fsCall[FsGatewayDirectoryListing]
	generation   int
	cacheEpoch   int
}

func NewFsGateway(backend FsGatewayBackend, options FsGatewayOptions) *FsGateway {
	g := &FsGateway{}
	g.Configure(options, backend)
	return g
}

func (g *FsGateway) Configure(options FsGatewayOptions, backend FsGatewayBackend) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if backend != nil {
		g.backend = backend
	}
	if options.StatMaxEntries <= 0 {
		options.StatMaxEntries = 20000
	}
	if options.ReadDirMaxEntries <= 0 {
		options.ReadDirMaxEntries = 4000
	}
	g.options = options
	g.invalidateAllLocked()
}

func (g *FsGateway) Generation() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.generation
}

func (g *FsGateway) Stat(fileName string) (*FsGatewayStats, bool) {
	return g.StatContext(context.Background(), fileName)
}

func (g *FsGateway) StatContext(ctx context.Context, fileName string) (*FsGatewayStats, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil, false
	}
	key := fsCacheKey(fileName)
	g.mu.Lock()
	options := g.options
	backend := g.backend
	if options.StatTTL <= 0 && options.NegativeStatTTL <= 0 {
		g.mu.Unlock()
		value, ok := readStatUncached(backend, key)
		if ctx.Err() != nil {
			return nil, false
		}
		return value, ok
	}
	if entry, ok := g.validStatLocked(key); ok {
		g.mu.Unlock()
		if !entry.ok {
			return nil, false
		}
		value := entry.value
		return &value, true
	}
	if call := g.statInFlight[key]; call != nil {
		g.mu.Unlock()
		select {
		case <-call.done:
		case <-ctx.Done():
			return nil, false
		}
		if !call.ok {
			return nil, false
		}
		value := call.value
		return &value, true
	}
	call := &fsCall[FsGatewayStats]{
		done:       make(chan struct{}),
		cacheEpoch: g.cacheEpoch,
	}
	g.statInFlight[key] = call
	g.mu.Unlock()

	value, ok := readStatUncached(backend, key)
	if ok {
		call.value = *value
	}
	call.ok = ok

	g.mu.Lock()
	if g.statInFlight[key] == call {
		if g.cacheEpoch == call.cacheEpoch {
			ttl := options.NegativeStatTTL
			if ok {
				ttl = options.StatTTL
			}
			if ttl > 0 {
				entry := fsCacheEntry[FsGatewayStats]{ok: ok, expiresAt: time.Now().Add(ttl)}
				if ok {
					entry.value = *value
				}
				g.setStatLocked(key, entry)
			}
		}
		delete(g.statInFlight, key)
	}
	close(call.done)
	g.mu.Unlock()
	if ctx.Err() != nil {
		return nil, false
	}
	return value, ok
}

// CachedStat returns the cached stat for fileName without calling the backend.
func (g *FsGateway) CachedStat(fileName string) (*FsGatewayStats, bool) {
	key := fsCacheKey(fileName)
	g.mu.Lock()
	defer g.mu.Unlock()
	entry, ok := g.validStatLocked(key)
	if !ok || !entry.ok {
		return nil, false
	}
	value := entry.value
	return &value, true
}

// RememberStat caches stats the caller read for fileName some other way, such
// as from a directory listing. It does nothing when stats are not cached or
// when any path was invalidated since generation, because the stats may
// predate that change.
func (g *FsGateway) RememberStat(fileName string, stats FsGatewayStats, generation int) {
	key := fsCacheKey(fileName)
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.options.StatTTL <= 0 || g.generation != generation || g.statInFlight[key] != nil {
		return
	}
	g.setStatLocked(key, fsCacheEntry[FsGatewayStats]{value: stats, ok: true, expiresAt: time.Now().Add(g.options.StatTTL)})
}

func (g *FsGateway) ReadDir(directory string) (*FsGatewayDirectoryListing, bool) {
	return g.ReadDirContext(context.Background(), directory)
}

// ReadDirContext lets callers cancel their wait for a shared directory read.
// Backend filesystem calls remain synchronous.
func (g *FsGateway) ReadDirContext(ctx context.Context, directory string) (*FsGatewayDirectoryListing, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil, false
	}
	key := fsCacheKey(directory)
	g.mu.Lock()
	options := g.options
	backend := g.backend
	if options.ReadDirTTL <= 0 {
		g.mu.Unlock()
		value, ok := readDirUncached(backend, key)
		if ctx.Err() != nil {
			return nil, false
		}
		return value, ok
	}
	if entry, ok := g.validDirLocked(key); ok {
		g.mu.Unlock()
		if !entry.ok {
			return nil, false
		}
		value := cloneFsGatewayDirectoryListing(entry.value)
		return &value, true
	}
	if call := g.dirInFlight[key]; call != nil {
		g.mu.Unlock()
		select {
		case <-call.done:
		case <-ctx.Done():
			return nil, false
		}
		if !call.ok {
			return nil, false
		}
		value := cloneFsGatewayDirectoryListing(call.value)
		return &value, true
	}
	call := &fsCall[FsGatewayDirectoryListing]{
		done:       make(chan struct{}),
		cacheEpoch: g.cacheEpoch,
	}
	g.dirInFlight[key] = call
	g.mu.Unlock()

	value, ok := readDirUncached(backend, key)
	if ok {
		call.value = cloneFsGatewayDirectoryListing(*value)
	}
	call.ok = ok

	g.mu.Lock()
	if g.dirInFlight[key] == call {
		if g.cacheEpoch == call.cacheEpoch {
			g.setDirLocked(key, fsCacheEntry[FsGatewayDirectoryListing]{
				value:     call.value,
				ok:        ok,
				expiresAt: time.Now().Add(options.ReadDirTTL),
			})
		}
		delete(g.dirInFlight, key)
	}
	close(call.done)
	g.mu.Unlock()
	if ctx.Err() != nil {
		return nil, false
	}
	return value, ok
}

func (g *FsGateway) InvalidatePath(fileName string) {
	key := fsCacheKey(fileName)
	parent := fsCacheKey(filepath.Dir(key))
	g.mu.Lock()
	defer g.mu.Unlock()
	g.statCache.delete(key)
	delete(g.statInFlight, key)
	g.dirCache.delete(key)
	delete(g.dirInFlight, key)
	g.dirCache.delete(parent)
	delete(g.dirInFlight, parent)
	g.generation++
}

func (g *FsGateway) InvalidateAll() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.invalidateAllLocked()
}

func readStatUncached(backend FsGatewayBackend, fileName string) (*FsGatewayStats, bool) {
	if backend == nil {
		return nil, false
	}
	value, err := backend.Stat(fileName)
	if err != nil {
		return nil, false
	}
	return &value, true
}

func readDirUncached(backend FsGatewayBackend, directory string) (*FsGatewayDirectoryListing, bool) {
	if backend == nil {
		return nil, false
	}
	entries, err := backend.ReadDir(directory)
	if err != nil {
		return nil, false
	}
	byLower := map[string][]FsGatewayDirent{}
	for _, entry := range entries {
		lower := strings.ToLower(entry.Name)
		byLower[lower] = append(byLower[lower], entry)
	}
	return &FsGatewayDirectoryListing{Entries: entries, ByLowerName: byLower}, true
}

func cloneFsGatewayDirectoryListing(listing FsGatewayDirectoryListing) FsGatewayDirectoryListing {
	clone := FsGatewayDirectoryListing{
		Entries: append([]FsGatewayDirent(nil), listing.Entries...),
	}
	if listing.ByLowerName == nil {
		return clone
	}
	clone.ByLowerName = make(map[string][]FsGatewayDirent, len(listing.ByLowerName))
	for lowerName, entries := range listing.ByLowerName {
		clone.ByLowerName[lowerName] = append([]FsGatewayDirent(nil), entries...)
	}
	return clone
}

func (g *FsGateway) validStatLocked(key string) (fsCacheEntry[FsGatewayStats], bool) {
	return g.statCache.get(key, time.Now())
}

func (g *FsGateway) validDirLocked(key string) (fsCacheEntry[FsGatewayDirectoryListing], bool) {
	return g.dirCache.get(key, time.Now())
}

func (g *FsGateway) setStatLocked(key string, entry fsCacheEntry[FsGatewayStats]) {
	g.statCache.set(key, entry, g.options.StatMaxEntries)
}

func (g *FsGateway) setDirLocked(key string, entry fsCacheEntry[FsGatewayDirectoryListing]) {
	g.dirCache.set(key, entry, g.options.ReadDirMaxEntries)
}

func (g *FsGateway) invalidateAllLocked() {
	g.statCache.clear()
	g.statInFlight = map[string]*fsCall[FsGatewayStats]{}
	g.dirCache.clear()
	g.dirInFlight = map[string]*fsCall[FsGatewayDirectoryListing]{}
	g.generation++
	g.cacheEpoch++
}

func fsCacheKey(fileName string) string {
	absolute, err := filepath.Abs(fileName)
	if err != nil {
		return filepath.Clean(fileName)
	}
	return absolute
}
