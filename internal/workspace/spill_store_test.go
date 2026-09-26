package workspace

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSpillStoreWritesAndReadsLengthPrefixedCBORRecords(t *testing.T) {
	directory := t.TempDir()
	store := NewSpillStore(directory, "", 24)
	first, err := store.WriteRecord("decls", map[string]any{"name": "first"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.WriteRecord("decls", map[string]any{"name": "second", "count": uint64(2)})
	if err != nil {
		t.Fatal(err)
	}
	if first.FileName != second.FileName || second.Offset <= first.Offset {
		t.Fatalf("record refs = %#v %#v", first, second)
	}
	var firstValue map[string]any
	if err := store.ReadRecord(first, &firstValue); err != nil {
		t.Fatal(err)
	}
	if firstValue["name"] != "first" {
		t.Fatalf("first value = %#v", firstValue)
	}
	var secondValue map[string]any
	if err := store.ReadRecord(second, &secondValue); err != nil {
		t.Fatal(err)
	}
	if secondValue["name"] != "second" || secondValue["count"] != uint64(2) {
		t.Fatalf("second value = %#v", secondValue)
	}
	if err := store.Clear(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatalf("directory should be removed, stat err = %v", err)
	}
}

func TestSpillStoreSweepsStaleTemporaryRoots(t *testing.T) {
	directory := t.TempDir()
	stale := filepath.Join(directory, "asp-lsp-bulk-1-stale")
	fresh := filepath.Join(directory, "asp-lsp-bulk-1-fresh")
	if err := os.Mkdir(stale, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(fresh, 0o755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}

	removed, err := SweepStaleTemporaryRoots(directory, 1)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d", removed)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale root should be gone, stat err = %v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("fresh root should remain: %v", err)
	}
}
