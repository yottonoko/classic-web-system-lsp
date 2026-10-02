package lspserver

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestJavaScriptProjectConfigListedMatchesStat(t *testing.T) {
	cases := map[string]func(string) error{
		"none":   func(string) error { return nil },
		"config": func(dir string) error { return os.WriteFile(filepath.Join(dir, "tsconfig.json"), []byte("{}"), 0o644) },
		"directoryNamedConfig": func(dir string) error {
			return os.Mkdir(filepath.Join(dir, "jsconfig.json"), 0o755)
		},
		"otherCase": func(dir string) error { return os.WriteFile(filepath.Join(dir, "JSConfig.json"), []byte("{}"), 0o644) },
		"symlink": func(dir string) error {
			target := filepath.Join(dir, "base.json")
			if err := os.WriteFile(target, []byte("{}"), 0o644); err != nil {
				return err
			}
			return os.Symlink(target, filepath.Join(dir, "jsconfig.json"))
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := setup(dir); err != nil {
				t.Skipf("setup unavailable: %v", err)
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if got, want := javaScriptProjectConfigListed(dir, entries), directoryHasJavaScriptProjectConfig(dir); got != want {
				t.Fatalf("javaScriptProjectConfigListed = %t, want %t", got, want)
			}
		})
	}
}

func TestJavaScriptProjectIdentityFollowsOtherWatchedChanges(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "app.js"), []byte(`export const app = 1;`), 0o644); err != nil {
		t.Fatal(err)
	}
	library := filepath.Join(root, "library.inc")
	if err := os.WriteFile(library, []byte("<% Function Lib() : End Function %>"), 0o644); err != nil {
		t.Fatal(err)
	}
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
	server.settings.CacheEnabled = true
	server.settings.CacheDirectory = t.TempDir()
	server.configureDiskAnalysisCache()
	server.configureFsGateway()
	defer func() {
		server.waitForAsyncDiskCacheWrites()
		server.closeDiskAnalysisCache()
	}()
	before := server.javascriptProjectFingerprint()
	server.waitForAsyncDiskCacheWrites()

	var hookMu sync.Mutex
	walked := 0
	javascriptProjectIdentityTestHooks.Store(&javascriptProjectIdentityTestHook{walkDir: func(string) {
		hookMu.Lock()
		walked++
		hookMu.Unlock()
	}})
	defer javascriptProjectIdentityTestHooks.Store(nil)

	if err := os.WriteFile(library, []byte("<% Function Lib2() : End Function %>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := server.didChangeWatchedFiles(didChangeWatchedFilesParams{Changes: []fileEvent{{URI: filePathURI(library), Type: fileChangeChanged}}}); err != nil {
		t.Fatal(err)
	}
	cached, ok := javascriptProjectIdentityMemory.Load(server.diskCacheForUse())
	if !ok || cached.(javascriptProjectIdentityMemoryEntry).fsGeneration != server.fsGateway.Generation() {
		t.Fatal("a non-JavaScript change left the JavaScript project identity behind the filesystem generation")
	}
	if after := server.javascriptProjectFingerprint(); after != before {
		t.Fatalf("JavaScript project fingerprint changed after a non-JavaScript edit: %q -> %q", before, after)
	}
	hookMu.Lock()
	defer hookMu.Unlock()
	if walked != 0 {
		t.Fatalf("a non-JavaScript edit rescanned the JavaScript project %d times", walked)
	}
}
