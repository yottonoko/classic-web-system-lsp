package lspserver

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTrustedPathCacheSharesRootHandlesOnlyWhileEnabled(t *testing.T) {
	root := t.TempDir()
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	entries := []trustedFilesystemRoot{trustedRootEntryForTest(t, server, root)}
	cache := &trustedPathCache{}

	first, releaseFirst, ok := cache.openRoot(entries[0])
	if !ok {
		t.Fatal("opening the root failed")
	}
	second, releaseSecond, ok := cache.openRoot(entries[0])
	if !ok {
		t.Fatal("opening the root again failed")
	}
	if first == second {
		t.Fatal("a disabled cache shared a root handle")
	}
	releaseFirst()
	releaseSecond()

	cache.setTTL(defaultNetworkStatCacheTTL)
	first, releaseFirst, ok = cache.openRoot(entries[0])
	if !ok {
		t.Fatal("opening the shared root failed")
	}
	second, releaseSecond, ok = cache.openRoot(entries[0])
	if !ok {
		t.Fatal("opening the shared root again failed")
	}
	if first != second {
		t.Fatal("an enabled cache opened the same root twice")
	}
	releaseFirst()
	cache.closeRoots()
	if _, err := second.Stat("."); err != nil {
		t.Fatalf("closing shared roots closed a handle that was still in use: %v", err)
	}
	releaseSecond()
	if _, err := second.Stat("."); err == nil {
		t.Fatal("a retired root handle stayed open after its last release")
	}

	third, releaseThird, ok := cache.openRoot(entries[0])
	if !ok {
		t.Fatal("reopening the root after closing shared handles failed")
	}
	defer releaseThird()
	if third == second {
		t.Fatal("a retired root handle was shared again")
	}
}

func TestNetworkProfileCanonicalDirectoryResolvesSymlinkedAncestors(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "real", "nested", "pages")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	cache := &trustedPathCache{}
	cache.setTTL(defaultNetworkStatCacheTTL)
	for _, directory := range []string{nested, filepath.Join(root, "link", "nested", "pages"), filepath.Join(root, "link", "nested")} {
		want, err := filepath.EvalSymlinks(directory)
		if err != nil {
			t.Fatal(err)
		}
		// The second lookup builds on cached ancestors.
		for round := 0; round < 2; round++ {
			got, ok := cache.canonicalDirectory(directory)
			if !ok || got != filepath.Clean(want) {
				t.Fatalf("round %d: canonicalDirectory(%s) = (%q, %t), want %q", round, directory, got, ok, want)
			}
		}
	}
}

func TestPreparedDirectoryRootsReuseAndBoundHandles(t *testing.T) {
	root := t.TempDir()
	for index := 0; index <= maxPreparedDirectoryRoots; index++ {
		if err := os.MkdirAll(filepath.Join(root, "area", "dir"+strings.Repeat("x", index)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	parent, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	directories := &preparedDirectoryRoots{}

	first, releaseFirst, ok := directories.acquire(parent, "area/dir")
	if !ok {
		t.Fatal("opening a directory failed")
	}
	again, releaseAgain, ok := directories.acquire(parent, "area/dir")
	if !ok || again != first {
		t.Fatal("the same directory was opened twice")
	}
	releaseAgain()
	releaseFirst()

	for index := 1; index <= maxPreparedDirectoryRoots; index++ {
		_, release, ok := directories.acquire(parent, "area/dir"+strings.Repeat("x", index))
		if !ok {
			t.Fatalf("opening directory %d failed", index)
		}
		release()
	}
	directories.mu.Lock()
	open := len(directories.entries)
	directories.mu.Unlock()
	if open > maxPreparedDirectoryRoots {
		t.Fatalf("%d directory handles stayed open, want at most %d", open, maxPreparedDirectoryRoots)
	}
	if _, err := first.Stat("."); err == nil {
		t.Fatal("the least recently used directory handle stayed open after eviction")
	}

	held, releaseHeld, ok := directories.acquire(parent, "area/dir")
	if !ok {
		t.Fatal("reopening an evicted directory failed")
	}
	directories.close()
	if _, err := held.Stat("."); err != nil {
		t.Fatalf("closing closed a directory handle that was still in use: %v", err)
	}
	releaseHeld()
	if _, err := held.Stat("."); err == nil {
		t.Fatal("a closed directory handle stayed open after its last release")
	}
	if _, _, ok := directories.acquire(parent, "area/dir"); ok {
		t.Fatal("closed directory handles were reopened")
	}
}

func TestPreparedSourceReadsNestedFilesThroughDirectoryHandles(t *testing.T) {
	root := t.TempDir()
	page := filepath.Join(root, "area", "pages", "default.asp")
	if err := os.MkdirAll(filepath.Dir(page), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(page, []byte("nested"), 0o644); err != nil {
		t.Fatal(err)
	}
	server := New(nil, io.Discard, nil)
	configureSourceSnapshotBoundary(server, filepath.Join(root, "boundary.asp"))
	trustedRoots := []trustedFilesystemRoot{trustedRootEntryForTest(t, server, root)}
	preparedRoots, complete := prepareTrustedFilesystemRoots(context.Background(), trustedRoots, []string{root})
	if !complete {
		t.Fatal("preparing trusted roots was unexpectedly cancelled")
	}
	defer closeTrustedFilesystemRoots(preparedRoots)
	ctx := withSourceReadRoots(withSourceReadBoundaries(context.Background(), root), preparedRoots)
	raw, err := server.readSourceFileBytes(ctx, page, nil)
	if err != nil || string(raw) != "nested" {
		t.Fatalf("prepared nested read = (%q, %v), want nested", raw, err)
	}
	directories := preparedRoots[0].directories
	directories.mu.Lock()
	_, shared := directories.entries["area/pages"]
	directories.mu.Unlock()
	if !shared {
		t.Fatal("the prepared read did not keep its directory handle")
	}
}

func trustedRootEntryForTest(t *testing.T, server *Server, root string) trustedFilesystemRoot {
	t.Helper()
	entries, complete := server.trustedFilesystemRootEntriesContext(context.Background(), []string{root})
	if !complete {
		t.Fatal("trusted roots were incomplete")
	}
	for _, entry := range entries {
		if entry.path == filepath.Clean(root) {
			return entry
		}
	}
	t.Fatalf("trusted roots %#v do not include %s", entries, root)
	return trustedFilesystemRoot{}
}
