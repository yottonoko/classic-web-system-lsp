package lspserver

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestWorkspaceListedStatsMatchesStatAndPrimesGateway(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "pages")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	page := filepath.Join(directory, "default.asp")
	if err := os.WriteFile(page, []byte("<% Response.Write 1 %>"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "secret.asp")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	symlinks := os.Symlink(outside, filepath.Join(directory, "linked.asp")) == nil
	server := newNetworkProfileTrustTestServer(t, root)
	listed := newWorkspaceListedStats(server)

	info, err := os.Stat(page)
	if err != nil {
		t.Fatal(err)
	}
	stats, ok := listed.stat(context.Background(), page)
	if !ok || !stats.File || stats.Size != info.Size() || stats.MtimeMS != info.ModTime().UnixMilli() {
		t.Fatalf("listed stat = (%#v, %t), want size %d and mtime %d", stats, ok, info.Size(), info.ModTime().UnixMilli())
	}
	if cached, ok := server.fsGateway.CachedStat(page); !ok || *cached != *stats {
		t.Fatalf("listed stat was not remembered by the gateway: (%#v, %t)", cached, ok)
	}
	if _, ok := listed.stat(context.Background(), filepath.Join(directory, "missing.asp")); ok {
		t.Fatal("a missing file had stats")
	}
	if symlinks {
		if _, ok := listed.stat(context.Background(), filepath.Join(directory, "linked.asp")); ok {
			t.Fatal("a symlinked file had stats")
		}
	}
}

func TestWorkspaceListedStatsSkipsGatewayAfterInvalidation(t *testing.T) {
	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	if err := os.WriteFile(page, []byte("<% %>"), 0o644); err != nil {
		t.Fatal(err)
	}
	server := newNetworkProfileTrustTestServer(t, root)
	listed := newWorkspaceListedStats(server)
	server.fsGateway.InvalidatePath(filepath.Join(root, "other.asp"))
	if _, ok := listed.stat(context.Background(), page); !ok {
		t.Fatal("listed stat failed")
	}
	if _, ok := server.fsGateway.CachedStat(page); ok {
		t.Fatal("stats listed before an invalidation were remembered after it")
	}
}
