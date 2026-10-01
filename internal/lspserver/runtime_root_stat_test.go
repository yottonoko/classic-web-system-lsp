package lspserver

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestTrustedRootStatCoalescerReobservesReplacedRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	var coalescer trustedRootStatCoalescer
	first, err := coalescer.stat(root)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			info, err := coalescer.stat(root)
			if err != nil || !os.SameFile(first, info) {
				t.Errorf("concurrent root stat = %v, %v; want the unchanged root", info, err)
			}
		}()
	}
	wg.Wait()

	// Keep the old directory alive so the replacement cannot reuse its inode.
	if err := os.Rename(root, root+".old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	replaced, err := coalescer.stat(root)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(first, replaced) {
		t.Fatal("root stat reused a result observed before the root was replaced")
	}
	if err := os.Remove(root); err != nil {
		t.Fatal(err)
	}
	if _, err := coalescer.stat(root); !os.IsNotExist(err) {
		t.Fatalf("removed root stat error = %v, want not-exist", err)
	}
}
