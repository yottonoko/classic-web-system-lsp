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
	statCache    map[string]fsCacheEntry[FsGatewayStats]
	statOrder    []string
	statInFlight map[string]*fsCall[FsGatewayStats]
	dirCache     map[string]fsCacheEntry[FsGatewayDirectoryListing]
	dirOrder     []string
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
	delete(g.statCache, key)
	g.statOrder = removeString(g.statOrder, key)
	delete(g.statInFlight, key)
	delete(g.dirCache, key)
	g.dirOrder = removeString(g.dirOrder, key)
	delete(g.dirInFlight, key)
	delete(g.dirCache, parent)
	g.dirOrder = removeString(g.dirOrder, parent)
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
	entry, ok := g.statCache[key]
	if !ok {
		return entry, false
	}
	if time.Now().After(entry.expiresAt) {
		delete(g.statCache, key)
		g.statOrder = removeString(g.statOrder, key)
		return entry, false
	}
	g.statOrder = append(removeString(g.statOrder, key), key)
	return entry, true
}

func (g *FsGateway) validDirLocked(key string) (fsCacheEntry[FsGatewayDirectoryListing], bool) {
	entry, ok := g.dirCache[key]
	if !ok {
		return entry, false
	}
	if time.Now().After(entry.expiresAt) {
		delete(g.dirCache, key)
		g.dirOrder = removeString(g.dirOrder, key)
		return entry, false
	}
	g.dirOrder = append(removeString(g.dirOrder, key), key)
	return entry, true
}

func (g *FsGateway) setStatLocked(key string, entry fsCacheEntry[FsGatewayStats]) {
	g.statCache[key] = entry
	g.statOrder = append(removeString(g.statOrder, key), key)
	for len(g.statOrder) > g.options.StatMaxEntries {
		oldest := g.statOrder[0]
		g.statOrder = g.statOrder[1:]
		delete(g.statCache, oldest)
	}
}

func (g *FsGateway) setDirLocked(key string, entry fsCacheEntry[FsGatewayDirectoryListing]) {
	g.dirCache[key] = entry
	g.dirOrder = append(removeString(g.dirOrder, key), key)
	for len(g.dirOrder) > g.options.ReadDirMaxEntries {
		oldest := g.dirOrder[0]
		g.dirOrder = g.dirOrder[1:]
		delete(g.dirCache, oldest)
	}
}

func (g *FsGateway) invalidateAllLocked() {
	g.statCache = map[string]fsCacheEntry[FsGatewayStats]{}
	g.statOrder = nil
	g.statInFlight = map[string]*fsCall[FsGatewayStats]{}
	g.dirCache = map[string]fsCacheEntry[FsGatewayDirectoryListing]{}
	g.dirOrder = nil
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

func removeString(values []string, target string) []string {
	for i, value := range values {
		if value == target {
			return append(values[:i], values[i+1:]...)
		}
	}
	return values
}
