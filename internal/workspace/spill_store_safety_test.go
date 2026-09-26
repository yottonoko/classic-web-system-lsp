package workspace

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

func TestNewSpillStoreUsesPrivateUnpredictableTemporaryRoots(t *testing.T) {
	first := NewSpillStore("", "same-namespace", 24)
	second := NewSpillStore("", "same-namespace", 24)
	t.Cleanup(func() {
		_ = first.Clear()
		_ = second.Clear()
	})
	if first.Directory() == "" || second.Directory() == "" || first.Directory() == second.Directory() {
		t.Fatalf("temporary spill directories = %q and %q, want distinct non-empty roots", first.Directory(), second.Directory())
	}
	for _, directory := range []string{first.Directory(), second.Directory()} {
		info, err := os.Stat(directory)
		if err != nil {
			t.Fatal(err)
		}
		if !info.IsDir() || runtime.GOOS != "windows" && info.Mode().Perm() != 0o700 {
			t.Fatalf("temporary spill directory mode = %v, want private directory", info.Mode())
		}
	}
}

func TestSpillStoreRejectsOutsideAndInvalidRecordReferences(t *testing.T) {
	directory := t.TempDir()
	store := NewSpillStore(directory, "", 24)
	cleanupSpillStore(t, store)
	valid, err := store.WriteRecord("decls", map[string]string{"name": "valid"})
	if err != nil {
		t.Fatal(err)
	}

	var value map[string]string
	outside := valid
	outside.FileName = filepath.Join(directory, "..", "outside.cborl")
	if err := store.ReadRecord(outside, &value); !errors.Is(err, errSpillRecordReferenceOutsideRoot) {
		t.Fatalf("outside reference error = %v, want %v", err, errSpillRecordReferenceOutsideRoot)
	}

	wrongBytes := valid
	wrongBytes.Bytes++
	if err := store.ReadRecord(wrongBytes, &value); !errors.Is(err, errSpillRecordReferenceInvalid) {
		t.Fatalf("mismatched length error = %v, want %v", err, errSpillRecordReferenceInvalid)
	}

	negativeOffset := valid
	negativeOffset.Offset = -1
	if err := store.ReadRecord(negativeOffset, &value); !errors.Is(err, errSpillRecordReferenceInvalid) {
		t.Fatalf("negative offset error = %v, want %v", err, errSpillRecordReferenceInvalid)
	}

	forgedFile := filepath.Join(store.Directory(), "forged.cborl")
	header := make([]byte, recordLengthBytes)
	binary.BigEndian.PutUint32(header, uint32(maxSpillRecordBytes)+1)
	if err := os.WriteFile(forgedFile, header, 0o600); err != nil {
		t.Fatal(err)
	}
	forged := SpillRecordRef{Kind: "forged", FileName: forgedFile, Bytes: uint32(maxSpillRecordBytes) + 1}
	if err := store.ReadRecord(forged, &value); !errors.Is(err, errSpillRecordReferenceInvalid) {
		t.Fatalf("oversized record error = %v, want %v", err, errSpillRecordReferenceInvalid)
	}
}

func TestSpillStoreRejectsSymlinkedWritePaths(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "outside.cborl")
	if err := os.WriteFile(target, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}

	linkedRoot := filepath.Join(root, "linked-root")
	if err := os.Symlink(filepath.Dir(target), linkedRoot); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	if _, err := NewSpillStore(linkedRoot, "", 24).WriteRecord("decls", map[string]string{"name": "root-symlink"}); !errors.Is(err, errSpillStoreSymlink) {
		t.Fatalf("root symlink write error = %v, want %v", err, errSpillStoreSymlink)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "outside" {
		t.Fatalf("outside target after root symlink write = %q, %v", got, err)
	}

	store := NewSpillStore(root, "", 24)
	cleanupSpillStore(t, store)
	linkedRecord := filepath.Join(store.Directory(), "decls.cborl")
	if err := os.Symlink(target, linkedRecord); err != nil {
		t.Skipf("record symlink creation unavailable: %v", err)
	}
	if _, err := store.WriteRecord("decls", map[string]string{"name": "record-symlink"}); !errors.Is(err, errSpillStoreSymlink) {
		t.Fatalf("record symlink write error = %v, want %v", err, errSpillStoreSymlink)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "outside" {
		t.Fatalf("outside target after record symlink write = %q, %v", got, err)
	}
}

func TestSpillStoreRejectsParentSymlinkBeforeCreatingRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	linkedParent := filepath.Join(root, "linked-parent")
	if err := os.Symlink(outside, linkedParent); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	storeRoot := filepath.Join(linkedParent, "new-store")
	if _, err := NewSpillStore(storeRoot, "", 24).WriteRecord("decls", map[string]string{"name": "parent-symlink"}); !errors.Is(err, errSpillStoreSymlink) {
		t.Fatalf("parent symlink write error = %v, want %v", err, errSpillStoreSymlink)
	}
	if _, err := os.Stat(filepath.Join(outside, "new-store")); !os.IsNotExist(err) {
		t.Fatalf("spill store created a root through the parent symlink: %v", err)
	}
}

func TestSpillStoreRejectsSymlinkedReadPaths(t *testing.T) {
	outside := t.TempDir()
	outsideStore := NewSpillStore(outside, "", 24)
	cleanupSpillStore(t, outsideStore)
	valid, err := outsideStore.WriteRecord("decls", map[string]string{"name": "outside"})
	if err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	linkedRoot := filepath.Join(root, "linked-root")
	if err := os.Symlink(outside, linkedRoot); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	store := NewSpillStore(linkedRoot, "", 24)
	linkedRef := valid
	linkedRef.FileName = filepath.Join(store.Directory(), filepath.Base(valid.FileName))
	var linkedValue map[string]string
	if err := store.ReadRecord(linkedRef, &linkedValue); !errors.Is(err, errSpillStoreSymlink) {
		t.Fatalf("symlinked root read error = %v, want %v", err, errSpillStoreSymlink)
	}

	regularRoot := t.TempDir()
	regularStore := NewSpillStore(regularRoot, "", 24)
	cleanupSpillStore(t, regularStore)
	linkedRecord := filepath.Join(regularStore.Directory(), filepath.Base(valid.FileName))
	if err := os.Symlink(valid.FileName, linkedRecord); err != nil {
		t.Skipf("record symlink creation unavailable: %v", err)
	}
	linkedRef.FileName = linkedRecord
	if err := regularStore.ReadRecord(linkedRef, &linkedValue); !errors.Is(err, errSpillStoreSymlink) {
		t.Fatalf("symlinked record read error = %v, want %v", err, errSpillStoreSymlink)
	}
}

func TestSpillStoreSerializesConcurrentWrites(t *testing.T) {
	store := NewSpillStore(t.TempDir(), "", 24)
	cleanupSpillStore(t, store)
	const writes = 64
	refs := make(chan SpillRecordRef, writes)
	errs := make(chan error, writes)
	var group sync.WaitGroup
	group.Add(writes)
	for index := 0; index < writes; index++ {
		go func(index int) {
			defer group.Done()
			ref, err := store.WriteRecord("concurrent", map[string]int{"index": index})
			if err != nil {
				errs <- err
				return
			}
			refs <- ref
		}(index)
	}
	group.Wait()
	close(refs)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if len(refs) != writes {
		t.Fatalf("concurrent write references = %d, want %d", len(refs), writes)
	}
	seen := make(map[int]bool, writes)
	for ref := range refs {
		var value map[string]int
		if err := store.ReadRecord(ref, &value); err != nil {
			t.Fatal(err)
		}
		index, ok := value["index"]
		if !ok || index < 0 || index >= writes || seen[index] {
			t.Fatalf("concurrent record value = %#v, duplicate or invalid index", value)
		}
		seen[index] = true
	}
}

func TestSpillStoreSerializesConcurrentWritesAcrossInstances(t *testing.T) {
	directory := t.TempDir()
	first := NewSpillStore(directory, "", 24)
	second := NewSpillStore(directory, "", 24)
	t.Cleanup(func() {
		if err := second.Close(); err != nil {
			t.Errorf("close second spill store: %v", err)
		}
		if err := first.Clear(); err != nil && !os.IsNotExist(err) {
			t.Errorf("clear shared spill store: %v", err)
		}
	})
	const writesPerStore = 32
	refs := make(chan SpillRecordRef, writesPerStore*2)
	errs := make(chan error, writesPerStore*2)
	var group sync.WaitGroup
	for _, store := range []*SpillStore{first, second} {
		group.Add(writesPerStore)
		for index := 0; index < writesPerStore; index++ {
			go func(store *SpillStore, index int) {
				defer group.Done()
				ref, err := store.WriteRecord("shared", map[string]int{"index": index})
				if err != nil {
					errs <- err
					return
				}
				refs <- ref
			}(store, index)
		}
	}
	group.Wait()
	close(refs)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if len(refs) != writesPerStore*2 {
		t.Fatalf("cross-instance write references = %d, want %d", len(refs), writesPerStore*2)
	}
	for ref := range refs {
		var value map[string]int
		if err := first.ReadRecord(ref, &value); err != nil {
			t.Fatal(err)
		}
		if _, ok := value["index"]; !ok {
			t.Fatalf("cross-instance record value = %#v, want index", value)
		}
	}
}

func TestSpillStoreRepairsIncompleteTailBeforeWriting(t *testing.T) {
	store := NewSpillStore(t.TempDir(), "", 24)
	cleanupSpillStore(t, store)
	first, err := store.WriteRecord("tail", map[string]string{"value": "first"})
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(first.FileName, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte{0, 0}); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := store.WriteRecord("tail", map[string]string{"value": "second"})
	if err != nil {
		t.Fatal(err)
	}
	if second.Offset != first.Offset+recordLengthBytes+int64(first.Bytes) {
		t.Fatalf("repaired tail offset = %d, want %d", second.Offset, first.Offset+recordLengthBytes+int64(first.Bytes))
	}
	var value map[string]string
	if err := store.ReadRecord(second, &value); err != nil || value["value"] != "second" {
		t.Fatalf("repaired tail record = %#v, %v", value, err)
	}
}

func cleanupSpillStore(t *testing.T, store *SpillStore) {
	t.Helper()
	t.Cleanup(func() {
		if err := store.Clear(); err != nil && !os.IsNotExist(err) {
			t.Errorf("clear spill store: %v", err)
		}
	})
}
