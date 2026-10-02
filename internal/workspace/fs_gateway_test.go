package workspace

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type fakeFsBackend struct {
	statCalls    map[string]int
	readdirCalls map[string]int
	stats        map[string]FsGatewayStats
	directories  map[string][]FsGatewayDirent
	statGate     chan struct{}
}

func newFakeFsBackend() *fakeFsBackend {
	return &fakeFsBackend{
		statCalls:    map[string]int{},
		readdirCalls: map[string]int{},
		stats:        map[string]FsGatewayStats{},
		directories:  map[string][]FsGatewayDirent{},
	}
}

func (b *fakeFsBackend) setStat(fileName string, value FsGatewayStats) {
	b.stats[fsCacheKey(fileName)] = value
}

func (b *fakeFsBackend) setDirectory(directory string, entries []FsGatewayDirent) {
	b.directories[fsCacheKey(directory)] = entries
}

func (b *fakeFsBackend) Stat(fileName string) (FsGatewayStats, error) {
	b.statCalls[fileName]++
	if b.statGate != nil {
		<-b.statGate
	}
	stat, ok := b.stats[fileName]
	if !ok {
		return FsGatewayStats{}, errMissingFS
	}
	return stat, nil
}

func (b *fakeFsBackend) ReadDir(directory string) ([]FsGatewayDirent, error) {
	b.readdirCalls[directory]++
	entries, ok := b.directories[directory]
	if !ok {
		return nil, errMissingFS
	}
	return entries, nil
}

type missingFSError struct{}

func (missingFSError) Error() string { return "missing" }

var errMissingFS error = missingFSError{}

type blockingFsBackend struct {
	mu             sync.Mutex
	stats          map[string]FsGatewayStats
	directories    map[string][]FsGatewayDirent
	statCalls      int
	readDirCalls   int
	statStarted    chan<- struct{}
	statRelease    <-chan struct{}
	readDirStarted chan<- struct{}
	readDirRelease <-chan struct{}
}

func (b *blockingFsBackend) Stat(fileName string) (FsGatewayStats, error) {
	b.mu.Lock()
	b.statCalls++
	value, ok := b.stats[fileName]
	started := b.statStarted
	release := b.statRelease
	b.mu.Unlock()
	if started != nil {
		started <- struct{}{}
	}
	if release != nil {
		<-release
	}
	if !ok {
		return FsGatewayStats{}, errMissingFS
	}
	return value, nil
}

func (b *blockingFsBackend) ReadDir(directory string) ([]FsGatewayDirent, error) {
	b.mu.Lock()
	b.readDirCalls++
	entries, ok := b.directories[directory]
	entries = append([]FsGatewayDirent(nil), entries...)
	started := b.readDirStarted
	release := b.readDirRelease
	b.mu.Unlock()
	if started != nil {
		started <- struct{}{}
	}
	if release != nil {
		<-release
	}
	if !ok {
		return nil, errMissingFS
	}
	return entries, nil
}

func (b *blockingFsBackend) setStat(fileName string, value FsGatewayStats) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stats[fsCacheKey(fileName)] = value
}

func (b *blockingFsBackend) setDirectory(directory string, entries []FsGatewayDirent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.directories[fsCacheKey(directory)] = append([]FsGatewayDirent(nil), entries...)
}

func (b *blockingFsBackend) callCounts() (int, int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.statCalls, b.readDirCalls
}

type constantFsBackend struct {
	stat    FsGatewayStats
	entries []FsGatewayDirent
}

func (b constantFsBackend) Stat(string) (FsGatewayStats, error) {
	return b.stat, nil
}

func (b constantFsBackend) ReadDir(string) ([]FsGatewayDirent, error) {
	return b.entries, nil
}

func TestFsGatewayCachesPositiveStatsUntilInvalidated(t *testing.T) {
	backend := newFakeFsBackend()
	fileName := filepath.Clean("/site/default.asp")
	backend.setStat(fileName, FsGatewayStats{File: true, Size: 10})
	gateway := NewFsGateway(backend, FsGatewayOptions{StatTTL: 30 * time.Second})

	if stat, ok := gateway.Stat(fileName); !ok || stat.Size != 10 {
		t.Fatalf("first stat = %#v, %v", stat, ok)
	}
	if stat, ok := gateway.Stat(fileName); !ok || stat.Size != 10 {
		t.Fatalf("second stat = %#v, %v", stat, ok)
	}
	if backend.statCalls[fsCacheKey(fileName)] != 1 {
		t.Fatalf("stat calls = %d", backend.statCalls[fsCacheKey(fileName)])
	}

	gateway.InvalidatePath(fileName)
	gateway.Stat(fileName)
	if backend.statCalls[fsCacheKey(fileName)] != 2 {
		t.Fatalf("stat calls after invalidate = %d", backend.statCalls[fsCacheKey(fileName)])
	}
}

func TestFsGatewayCachesNegativeStats(t *testing.T) {
	backend := newFakeFsBackend()
	fileName := filepath.Clean("/site/missing.inc")
	gateway := NewFsGateway(backend, FsGatewayOptions{StatTTL: 30 * time.Second, NegativeStatTTL: 30 * time.Second})

	if _, ok := gateway.Stat(fileName); ok {
		t.Fatalf("missing file should not stat")
	}
	if _, ok := gateway.Stat(fileName); ok {
		t.Fatalf("missing file should not stat on cache hit")
	}
	if backend.statCalls[fsCacheKey(fileName)] != 1 {
		t.Fatalf("stat calls = %d", backend.statCalls[fsCacheKey(fileName)])
	}
}

func TestFsGatewayDeduplicatesInflightStats(t *testing.T) {
	backend := newFakeFsBackend()
	fileName := filepath.Clean("/site/default.asp")
	backend.setStat(fileName, FsGatewayStats{File: true, Size: 10})
	backend.statGate = make(chan struct{})
	gateway := NewFsGateway(backend, FsGatewayOptions{StatTTL: 30 * time.Second})

	left := make(chan bool, 1)
	right := make(chan bool, 1)
	go func() {
		stat, ok := gateway.Stat(fileName)
		left <- ok && stat.Size == 10
	}()
	go func() {
		stat, ok := gateway.Stat(fileName)
		right <- ok && stat.Size == 10
	}()
	close(backend.statGate)
	if !<-left || !<-right {
		t.Fatalf("expected both stat calls to succeed")
	}
	if backend.statCalls[fsCacheKey(fileName)] != 1 {
		t.Fatalf("stat calls = %d", backend.statCalls[fsCacheKey(fileName)])
	}
}

func TestFsGatewayStatContextCancelsInflightWait(t *testing.T) {
	fileName := filepath.Clean("/site/default.asp")
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	backend := &blockingFsBackend{
		stats:       map[string]FsGatewayStats{fsCacheKey(fileName): {File: true, Size: 10}},
		directories: map[string][]FsGatewayDirent{},
		statStarted: started,
		statRelease: release,
	}
	gateway := NewFsGateway(backend, FsGatewayOptions{StatTTL: 30 * time.Second})
	first := make(chan bool, 1)
	go func() {
		_, ok := gateway.Stat(fileName)
		first <- ok
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("first stat did not reach the backend")
	}
	ctx, cancel := context.WithCancel(context.Background())
	second := make(chan bool, 1)
	go func() {
		_, ok := gateway.StatContext(ctx, fileName)
		second <- ok
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case ok := <-second:
		if ok {
			t.Fatal("cancelled in-flight stat wait succeeded")
		}
	case <-time.After(time.Second):
		close(release)
		t.Fatal("cancelled in-flight stat wait did not return")
	}
	close(release)
	if !<-first {
		t.Fatal("first stat failed after backend release")
	}
}

func TestFsGatewayInvalidatePathDoesNotCacheInflightStat(t *testing.T) {
	fileName := filepath.Clean("/site/default.asp")
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	backend := &blockingFsBackend{
		stats:       map[string]FsGatewayStats{fsCacheKey(fileName): {File: true, Size: 10}},
		directories: map[string][]FsGatewayDirent{},
		statStarted: started,
		statRelease: release,
	}
	gateway := NewFsGateway(backend, FsGatewayOptions{StatTTL: 30 * time.Second})

	first := make(chan struct {
		stat *FsGatewayStats
		ok   bool
	}, 1)
	go func() {
		stat, ok := gateway.Stat(fileName)
		first <- struct {
			stat *FsGatewayStats
			ok   bool
		}{stat: stat, ok: ok}
	}()
	<-started

	backend.setStat(fileName, FsGatewayStats{File: true, Size: 20})
	gateway.InvalidatePath(fileName)
	close(release)
	if result := <-first; !result.ok || result.stat.Size != 10 {
		t.Fatalf("inflight stat = %#v, %v", result.stat, result.ok)
	}

	if stat, ok := gateway.Stat(fileName); !ok || stat.Size != 20 {
		t.Fatalf("stat after invalidate = %#v, %v", stat, ok)
	}
	statCalls, _ := backend.callCounts()
	if statCalls != 2 {
		t.Fatalf("stat calls = %d", statCalls)
	}
}

func TestFsGatewayUnrelatedInvalidationKeepsInflightStatCacheable(t *testing.T) {
	fileName := filepath.Clean("/site/default.asp")
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	backend := &blockingFsBackend{
		stats:       map[string]FsGatewayStats{fsCacheKey(fileName): {File: true, Size: 10}},
		directories: map[string][]FsGatewayDirent{},
		statStarted: started,
		statRelease: release,
	}
	gateway := NewFsGateway(backend, FsGatewayOptions{StatTTL: 30 * time.Second})

	first := make(chan bool, 1)
	go func() {
		stat, ok := gateway.Stat(fileName)
		first <- ok && stat.Size == 10
	}()
	<-started

	gateway.InvalidatePath("/other/changed.asp")
	close(release)
	if !<-first {
		t.Fatalf("inflight stat should succeed")
	}
	if stat, ok := gateway.Stat(fileName); !ok || stat.Size != 10 {
		t.Fatalf("cached stat = %#v, %v", stat, ok)
	}
	statCalls, _ := backend.callCounts()
	if statCalls != 1 {
		t.Fatalf("stat calls = %d", statCalls)
	}
}

func TestFsGatewayCachesDirectoryListings(t *testing.T) {
	backend := newFakeFsBackend()
	directory := filepath.Clean("/site")
	backend.setDirectory(directory, []FsGatewayDirent{{Name: "Shared.inc", File: true}, {Name: "default.asp", File: true}})
	gateway := NewFsGateway(backend, FsGatewayOptions{ReadDirTTL: 30 * time.Second})

	listing, ok := gateway.ReadDir(directory)
	if !ok || listing.ByLowerName["shared.inc"][0].Name != "Shared.inc" {
		t.Fatalf("listing = %#v, %v", listing, ok)
	}
	if listing, ok := gateway.ReadDir(directory); !ok || len(listing.Entries) != 2 {
		t.Fatalf("second listing = %#v, %v", listing, ok)
	}
	if backend.readdirCalls[fsCacheKey(directory)] != 1 {
		t.Fatalf("readdir calls = %d", backend.readdirCalls[fsCacheKey(directory)])
	}
	gateway.InvalidatePath(filepath.Join(directory, "default.asp"))
	gateway.ReadDir(directory)
	if backend.readdirCalls[fsCacheKey(directory)] != 2 {
		t.Fatalf("readdir calls after invalidate = %d", backend.readdirCalls[fsCacheKey(directory)])
	}
}

func TestFsGatewayCachedDirectoryListingsOwnReturnedMutableData(t *testing.T) {
	backend := newFakeFsBackend()
	directory := filepath.Clean("/site")
	backend.setDirectory(directory, []FsGatewayDirent{
		{Name: "Shared.inc", File: true},
		{Name: "default.asp", File: true},
	})
	gateway := NewFsGateway(backend, FsGatewayOptions{ReadDirTTL: 30 * time.Second})

	first, ok := gateway.ReadDir(directory)
	if !ok {
		t.Fatal("first directory listing failed")
	}
	first.Entries[0].Name = "mutated-entry"
	first.ByLowerName["shared.inc"][0].Name = "mutated-index"
	delete(first.ByLowerName, "default.asp")

	second, ok := gateway.ReadDir(directory)
	if !ok {
		t.Fatal("second directory listing failed")
	}
	if second.Entries[0].Name != "Shared.inc" || second.ByLowerName["shared.inc"][0].Name != "Shared.inc" {
		t.Fatalf("cached listing changed after first result mutation: %#v", second)
	}
	if _, ok := second.ByLowerName["default.asp"]; !ok {
		t.Fatal("cached listing lost default.asp after first result mutation")
	}

	second.Entries[0].Name = "mutated-second-entry"
	second.ByLowerName["shared.inc"][0].Name = "mutated-second-index"
	third, ok := gateway.ReadDir(directory)
	if !ok {
		t.Fatal("third directory listing failed")
	}
	if third.Entries[0].Name != "Shared.inc" || third.ByLowerName["shared.inc"][0].Name != "Shared.inc" {
		t.Fatalf("cached listing changed after second result mutation: %#v", third)
	}
}

func TestFsGatewayInvalidatePathDoesNotCacheInflightReadDir(t *testing.T) {
	directory := filepath.Clean("/site")
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	backend := &blockingFsBackend{
		stats:          map[string]FsGatewayStats{},
		directories:    map[string][]FsGatewayDirent{fsCacheKey(directory): {{Name: "old.inc", File: true}}},
		readDirStarted: started,
		readDirRelease: release,
	}
	gateway := NewFsGateway(backend, FsGatewayOptions{ReadDirTTL: 30 * time.Second})

	first := make(chan struct {
		listing *FsGatewayDirectoryListing
		ok      bool
	}, 1)
	go func() {
		listing, ok := gateway.ReadDir(directory)
		first <- struct {
			listing *FsGatewayDirectoryListing
			ok      bool
		}{listing: listing, ok: ok}
	}()
	<-started

	backend.setDirectory(directory, []FsGatewayDirent{{Name: "new.inc", File: true}})
	gateway.InvalidatePath(filepath.Join(directory, "changed.inc"))
	close(release)
	if result := <-first; !result.ok || result.listing.Entries[0].Name != "old.inc" {
		t.Fatalf("inflight listing = %#v, %v", result.listing, result.ok)
	}

	if listing, ok := gateway.ReadDir(directory); !ok || listing.Entries[0].Name != "new.inc" {
		t.Fatalf("listing after invalidate = %#v, %v", listing, ok)
	}
	_, readDirCalls := backend.callCounts()
	if readDirCalls != 2 {
		t.Fatalf("readdir calls = %d", readDirCalls)
	}
}

func TestFsGatewayConfigureConcurrentWithAccess(t *testing.T) {
	first := constantFsBackend{
		stat:    FsGatewayStats{File: true, Size: 10},
		entries: []FsGatewayDirent{{Name: "first.asp", File: true}},
	}
	second := constantFsBackend{
		stat:    FsGatewayStats{File: true, Size: 20},
		entries: []FsGatewayDirent{{Name: "second.asp", File: true}},
	}
	gateway := NewFsGateway(first, FsGatewayOptions{
		StatTTL:    time.Second,
		ReadDirTTL: time.Second,
	})
	start := make(chan struct{})
	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		<-start
		for i := 0; i < 1000; i++ {
			backend := FsGatewayBackend(first)
			options := FsGatewayOptions{
				StatTTL:    time.Second,
				ReadDirTTL: time.Second,
			}
			if i%2 != 0 {
				backend = second
				options = FsGatewayOptions{}
			}
			gateway.Configure(options, backend)
		}
	}()
	go func() {
		defer wait.Done()
		<-start
		for i := 0; i < 1000; i++ {
			gateway.Stat("/site/default.asp")
			gateway.ReadDir("/site")
		}
	}()
	close(start)
	wait.Wait()
}

func TestFsGatewayBypassesCachesWhenTTLIsZero(t *testing.T) {
	backend := newFakeFsBackend()
	fileName := filepath.Clean("/site/default.asp")
	directory := filepath.Dir(fileName)
	backend.setStat(fileName, FsGatewayStats{File: true, Size: 10})
	backend.setDirectory(directory, []FsGatewayDirent{{Name: "default.asp", File: true}})
	gateway := NewFsGateway(backend, FsGatewayOptions{})

	gateway.Stat(fileName)
	gateway.Stat(fileName)
	gateway.ReadDir(directory)
	gateway.ReadDir(directory)
	if backend.statCalls[fsCacheKey(fileName)] != 2 || backend.readdirCalls[fsCacheKey(directory)] != 2 {
		t.Fatalf("calls = %d/%d", backend.statCalls[fsCacheKey(fileName)], backend.readdirCalls[fsCacheKey(directory)])
	}
}

func TestFsGatewayEvictsLRUEntries(t *testing.T) {
	backend := newFakeFsBackend()
	first := filepath.Clean("/site/first.asp")
	second := filepath.Clean("/site/second.asp")
	firstDir := filepath.Clean("/site/a")
	secondDir := filepath.Clean("/site/b")
	backend.setStat(first, FsGatewayStats{File: true, Size: 1})
	backend.setStat(second, FsGatewayStats{File: true, Size: 2})
	backend.setDirectory(firstDir, []FsGatewayDirent{{Name: "first.asp", File: true}})
	backend.setDirectory(secondDir, []FsGatewayDirent{{Name: "second.asp", File: true}})
	gateway := NewFsGateway(backend, FsGatewayOptions{StatTTL: 30 * time.Second, ReadDirTTL: 30 * time.Second, StatMaxEntries: 1, ReadDirMaxEntries: 1})

	gateway.Stat(first)
	gateway.Stat(second)
	gateway.Stat(first)
	gateway.ReadDir(firstDir)
	gateway.ReadDir(secondDir)
	gateway.ReadDir(firstDir)

	if backend.statCalls[fsCacheKey(first)] != 2 || backend.statCalls[fsCacheKey(second)] != 1 {
		t.Fatalf("stat calls = %d/%d", backend.statCalls[fsCacheKey(first)], backend.statCalls[fsCacheKey(second)])
	}
	if backend.readdirCalls[fsCacheKey(firstDir)] != 2 || backend.readdirCalls[fsCacheKey(secondDir)] != 1 {
		t.Fatalf("readdir calls = %d/%d", backend.readdirCalls[fsCacheKey(firstDir)], backend.readdirCalls[fsCacheKey(secondDir)])
	}
}

type directoryWaitContext struct {
	context.Context
	waiting chan struct{}
	once    sync.Once
}

func (ctx *directoryWaitContext) Done() <-chan struct{} {
	ctx.once.Do(func() { close(ctx.waiting) })
	return ctx.Context.Done()
}

func TestFsGatewayReadDirWaitCanBeCancelled(t *testing.T) {
	directory := filepath.Clean("/site")
	started, release := make(chan struct{}, 1), make(chan struct{})
	backend := &blockingFsBackend{
		stats:          map[string]FsGatewayStats{},
		directories:    map[string][]FsGatewayDirent{fsCacheKey(directory): {{Name: "page.asp", File: true}}},
		readDirStarted: started, readDirRelease: release,
	}
	gateway := NewFsGateway(backend, FsGatewayOptions{ReadDirTTL: time.Minute})
	leader := make(chan bool, 1)
	go func() { _, ok := gateway.ReadDir(directory); leader <- ok }()
	<-started
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer func() { unblock(); <-leader }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	waitCtx := &directoryWaitContext{Context: ctx, waiting: make(chan struct{})}
	follower := make(chan bool, 1)
	go func() { _, ok := gateway.ReadDirContext(waitCtx, directory); follower <- ok }()
	select {
	case <-waitCtx.waiting:
	case <-time.After(time.Second):
		unblock()
		<-follower
		t.Fatal("follower did not reach shared read wait")
	}
	cancel()
	select {
	case ok := <-follower:
		if ok {
			t.Fatal("cancelled directory wait succeeded")
		}
	case <-time.After(time.Second):
		unblock()
		<-follower
		t.Fatal("directory wait ignored cancellation")
	}
	unblock()
	if listing, ok := gateway.ReadDir(directory); !ok || listing.Entries[0].Name != "page.asp" {
		t.Fatalf("shared read did not survive follower cancellation: %#v, %v", listing, ok)
	}
}

func TestFsGatewayEvictsLeastRecentlyUsedStats(t *testing.T) {
	backend := newFakeFsBackend()
	names := []string{filepath.Clean("/site/a.asp"), filepath.Clean("/site/b.asp"), filepath.Clean("/site/c.asp")}
	for _, name := range names {
		backend.setStat(name, FsGatewayStats{File: true})
	}
	gateway := NewFsGateway(backend, FsGatewayOptions{StatTTL: 30 * time.Second, StatMaxEntries: 2})
	gateway.Stat(names[0])
	gateway.Stat(names[1])
	// Using a.asp again makes b.asp the entry to evict.
	gateway.Stat(names[0])
	gateway.Stat(names[2])
	for _, check := range []struct {
		index int
		calls int
	}{{0, 1}, {2, 1}, {1, 2}} {
		name := names[check.index]
		gateway.Stat(name)
		if got := backend.statCalls[fsCacheKey(name)]; got != check.calls {
			t.Fatalf("%s stat calls = %d, want %d", name, got, check.calls)
		}
	}
}

func TestFsGatewayRememberStatServesCachedStats(t *testing.T) {
	backend := newFakeFsBackend()
	fileName := filepath.Clean("/site/listed.asp")
	backend.setStat(fileName, FsGatewayStats{File: true, Size: 1})
	gateway := NewFsGateway(backend, FsGatewayOptions{StatTTL: 30 * time.Second})
	generation := gateway.Generation()
	gateway.RememberStat(fileName, FsGatewayStats{File: true, Size: 7}, generation)
	if stat, ok := gateway.CachedStat(fileName); !ok || stat.Size != 7 {
		t.Fatalf("cached stat = %#v, %v", stat, ok)
	}
	if stat, ok := gateway.Stat(fileName); !ok || stat.Size != 7 || backend.statCalls[fsCacheKey(fileName)] != 0 {
		t.Fatalf("stat after remember = %#v, %v with %d backend calls", stat, ok, backend.statCalls[fsCacheKey(fileName)])
	}

	other := filepath.Clean("/site/other.asp")
	gateway.InvalidatePath(other)
	gateway.RememberStat(other, FsGatewayStats{File: true}, generation)
	if _, ok := gateway.CachedStat(other); ok {
		t.Fatal("stats from before an invalidation were remembered")
	}

	uncached := NewFsGateway(backend, FsGatewayOptions{})
	uncached.RememberStat(fileName, FsGatewayStats{File: true}, uncached.Generation())
	if _, ok := uncached.CachedStat(fileName); ok {
		t.Fatal("a gateway without a stat TTL remembered stats")
	}
}
