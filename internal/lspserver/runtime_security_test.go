package lspserver

import (
	"context"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeSecurityRejectsFilesystemTraversalAndSymlinkEscapes(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.inc")
	if err := os.WriteFile(secret, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}

	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
	server.configureFsGateway()

	if _, ok := server.fsStat(filepath.Join(link, "secret.inc")); ok {
		t.Fatal("fsStat followed a symlink outside the workspace")
	}
	if _, ok := server.fsReadDir(link); ok {
		t.Fatal("fsReadDir followed a symlink outside the workspace")
	}
	if _, ok := server.fsStat(filepath.Join(root, "..", filepath.Base(secret))); ok {
		t.Fatal("fsStat accepted a parent traversal")
	}

	owner := filepath.Join(root, "default.asp")
	if err := os.WriteFile(owner, []byte("<% %>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if details, ok := server.includeTargetDetailsForMode(filePathURI(owner), "link/secret.inc", "file"); ok || details.Exists {
		t.Fatalf("include resolver accepted a symlink escape: %#v, %v", details, ok)
	}
}

func TestTrustedPreparedRootsRejectRetargetedWorkspaceSymlink(t *testing.T) {
	base := t.TempDir()
	first := filepath.Join(base, "first")
	second := filepath.Join(base, "second")
	for _, directory := range []string{first, second} {
		if err := os.Mkdir(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(base, "workspace")
	if err := os.Symlink(first, link); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	firstTarget := filepath.Join(link, "first.asp")
	if err := os.WriteFile(filepath.Join(first, "first.asp"), []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	roots, complete := trustedPathRootsContext(context.Background(), []string{link})
	if !complete || len(roots) != 1 {
		t.Fatalf("prepared roots = (%#v, %v), want one complete root", roots, complete)
	}
	if _, ok := trustedPathForPreparedRootsContext(context.Background(), firstTarget, roots); !ok {
		t.Fatal("prepared root rejected its original workspace target")
	}

	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(second, link); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(second, "second.asp"), []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := trustedPathForPreparedRootsContext(context.Background(), filepath.Join(link, "second.asp"), roots); ok {
		t.Fatal("prepared root followed a retargeted workspace symlink")
	}
}

func TestRuntimeSecurityRejectsWorkspaceRootReplacement(t *testing.T) {
	base := t.TempDir()
	workspace := filepath.Join(base, "workspace")
	movedWorkspace := filepath.Join(base, "workspace-moved")
	if err := os.Mkdir(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "inside.inc"), []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = workspace
	server.rootURI = filePathURI(workspace)
	server.workspaceRoots = []workspaceRoot{{Path: workspace, URI: filePathURI(workspace)}}
	server.configureFsGateway()
	if _, ok := server.fsStat(filepath.Join(workspace, "inside.inc")); !ok {
		t.Fatal("configured workspace root was not readable before replacement")
	}
	if err := os.Rename(workspace, movedWorkspace); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "replacement.inc"), []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, ok := server.fsStat(filepath.Join(workspace, "replacement.inc")); ok {
		t.Fatal("filesystem trust boundary followed a replaced workspace root")
	}
	if _, ok := server.fsReadDir(workspace); ok {
		t.Fatal("directory trust boundary followed a replaced workspace root")
	}
}

func TestRuntimeSecurityRejectsRootReplacementBetweenValidationAndOpenRoot(t *testing.T) {
	base := t.TempDir()
	workspace := filepath.Join(base, "workspace")
	movedWorkspace := filepath.Join(base, "workspace-moved")
	replacement := filepath.Join(base, "workspace-replacement")
	if err := os.Mkdir(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(replacement, 0o755); err != nil {
		t.Fatal(err)
	}
	targetName := "target.inc"
	if err := os.WriteFile(filepath.Join(workspace, targetName), []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(replacement, targetName), []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}

	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = workspace
	server.rootURI = filePathURI(workspace)
	server.workspaceRoots = []workspaceRoot{{Path: workspace, URI: filePathURI(workspace)}}
	canonicalWorkspace, err := evalTrustedRootPath(workspace)
	if err != nil {
		t.Fatal(err)
	}

	var hookCalls int
	trustedFilesystemOpenRootTestHooks.Store(&trustedFilesystemOpenRootTestHook{fn: func(path string) {
		hookCalls++
		if filepath.Clean(path) != filepath.Clean(canonicalWorkspace) {
			t.Errorf("OpenRoot path = %q, want %q", path, canonicalWorkspace)
			return
		}
		if err := os.Rename(workspace, movedWorkspace); err != nil {
			t.Errorf("rename configured root: %v", err)
			return
		}
		if err := os.Rename(replacement, workspace); err != nil {
			t.Errorf("replace configured root: %v", err)
		}
	}})
	defer trustedFilesystemOpenRootTestHooks.Store(nil)

	info, err := server.trustedFilesystemStat(filepath.Join(workspace, targetName))
	if err == nil || info != nil {
		t.Fatalf("trusted stat through replaced root = (%v, %v), want rejection", info, err)
	}
	if hookCalls != 1 {
		t.Fatalf("OpenRoot test hook calls = %d, want 1", hookCalls)
	}
}

func TestRuntimeSecurityRejectsCacheTraversalAndSymlinkEscapes(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "cache-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}

	server := New(strings.NewReader(""), io.Discard, io.Discard)
	defer server.closeDiskAnalysisCache()
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
	server.settings.CacheEnabled = true
	server.settings.CacheDirectory = link
	server.configureDiskAnalysisCache()
	cache := server.diskCacheForUse()
	if cache == nil {
		t.Fatal("cache was not configured")
	}
	if filepath.Clean(cache.Directory()) == filepath.Clean(outside) {
		t.Fatalf("cache followed a symlink outside the workspace: %q", cache.Directory())
	}
	if _, err := os.Stat(filepath.Join(outside, "bbolt-v1")); err == nil {
		t.Fatal("cache created files through an escaping symlink")
	}

	server.settings.CacheDirectory = root + string(filepath.Separator) + ".." + string(filepath.Separator) + "cache-escape"
	server.configureDiskAnalysisCache()
	if filepath.Clean(server.diskCacheForUse().Directory()) == filepath.Clean(filepath.Join(root, "..", "cache-escape")) {
		t.Fatal("cache accepted a parent traversal")
	}
}

func TestRuntimeSecurityBoundsConfigurableRuntimeLimits(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.settings.CacheTTLHours = math.MaxInt
	server.settings.CacheMaxSizeMB = math.MaxInt
	server.settings.NetworkProfile = "network"
	server.settings.NetworkStatCacheTTLMS = math.MaxInt
	server.settings.NetworkReadDirCacheTTLMS = math.MaxInt
	server.settings.NetworkIncludeReadConcurrency = math.MaxInt
	server.configureDiskAnalysisCache()
	defer server.closeDiskAnalysisCache()

	if server.settings.CacheTTLHours != maxDiskCacheTTLHours {
		t.Fatalf("cache TTL = %d, want bound %d", server.settings.CacheTTLHours, maxDiskCacheTTLHours)
	}
	if server.settings.CacheMaxSizeMB != maxDiskCacheMaxSizeMB {
		t.Fatalf("cache size = %d, want bound %d", server.settings.CacheMaxSizeMB, maxDiskCacheMaxSizeMB)
	}
	profile := server.resolveNetworkProfile()
	if profile.StatTTL != maxNetworkCacheTTL || profile.ReadDirTTL != maxNetworkCacheTTL {
		t.Fatalf("network TTLs = %#v, want %s", profile, maxNetworkCacheTTL)
	}
	if profile.IncludeReadConcurrency != maxNetworkIncludeConcurrency {
		t.Fatalf("include concurrency = %d, want bound %d", profile.IncludeReadConcurrency, maxNetworkIncludeConcurrency)
	}
}

func TestRuntimeSecurityRejectsUntrustedDebugLogPaths(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "log-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}

	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
	server.settings.DebugLogFileEnabled = true
	server.settings.DebugLogFilePath = filepath.Join(link, "debug.log")
	if got := server.debugLogFilePath(); got != "" {
		t.Fatalf("debug log path escaped through symlink: %q", got)
	}
	server.settings.DebugLogFilePath = root + string(filepath.Separator) + ".." + string(filepath.Separator) + "debug.log"
	if got := server.debugLogFilePath(); got != "" {
		t.Fatalf("debug log path accepted parent traversal: %q", got)
	}

	t.Setenv("ASP_LSP_TEST_DEBUG_LOG_MAX_BYTES", "999999999999999999999")
	t.Setenv("ASP_LSP_TEST_DEBUG_LOG_MAX_BACKUPS", "999999999999999999999")
	if got := debugLogFileMaxBytes(); got != 1<<30 {
		t.Fatalf("debug log max bytes = %d, want bound %d", got, 1<<30)
	}
	if got := debugLogFileMaxBackups(); got != 100 {
		t.Fatalf("debug log max backups = %d, want bound 100", got)
	}
}

func TestRuntimeSecurityUsesTrustedExtensionDebugLogPathOutsideWorkspace(t *testing.T) {
	root := t.TempDir()
	globalStorage, err := os.MkdirTemp(".", ".asp-lsp-global-storage-")
	if err != nil {
		t.Fatal(err)
	}
	globalStorage, err = filepath.Abs(globalStorage)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(globalStorage) })
	if pathWithinRoot(root, globalStorage) || pathWithinRoot(os.TempDir(), globalStorage) {
		t.Fatalf("test global storage must be outside workspace and temp roots: %q", globalStorage)
	}

	defaultLogPath := filepath.Join(globalStorage, "asp-lsp-debug.log")
	maliciousLogPath := filepath.Join(filepath.Dir(globalStorage), ".asp-lsp-malicious-debug.log")
	t.Setenv("ASP_LSP_DEFAULT_DEBUG_LOG_FILE", defaultLogPath)

	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
	server.settings.DebugLogFileEnabled = true
	if got := server.debugLogFilePath(); got != defaultLogPath {
		t.Fatalf("trusted extension debug log path = %q, want %q", got, defaultLogPath)
	}

	server.logDebugFile("DEBUG", "security.test", "trusted default path")
	server.debugLogWriter.wait()
	logText, err := os.ReadFile(defaultLogPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logText), "trusted default path") {
		t.Fatalf("trusted extension debug log missing test entry: %s", logText)
	}

	server.settings.DebugLogFilePath = maliciousLogPath
	if got := server.debugLogFilePath(); got != "" {
		t.Fatalf("malicious configured path was not rejected: %q", got)
	}
	server.settings.DebugLogFilePath = ""
	t.Setenv("ASP_LSP_DEFAULT_DEBUG_LOG_FILE", "")
	if got := server.debugLogFilePath(); got != filepath.Join(os.TempDir(), "asp-lsp-debug.log") {
		t.Fatalf("default debug log path did not fall back to temp: %q", got)
	}
}

func TestGraphSourceRejectsUnscopedDiskReads(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "unscoped.asp")
	if err := os.WriteFile(path, []byte("<% Dim value %>"), 0o644); err != nil {
		t.Fatal(err)
	}

	server := New(strings.NewReader(""), io.Discard, io.Discard)
	if _, ok := server.parsedGraphDocument(filePathURI(path)); ok {
		t.Fatal("graph source read succeeded without a workspace boundary")
	}

	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: filePathURI(root)}}
	if _, ok := server.parsedGraphDocument(filePathURI(path)); !ok {
		t.Fatal("graph source read failed inside the configured workspace")
	}
}

func TestShutdownRejectsFurtherWorkAndCacheReconfiguration(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	if _, rpcErr := server.handleRequest(context.Background(), "shutdown", nil); rpcErr != nil {
		t.Fatalf("shutdown returned an error: %#v", rpcErr)
	}

	server.settings.CacheEnabled = true
	server.settings.CacheDirectory = t.TempDir()
	server.configureDiskAnalysisCache()
	server.mu.Lock()
	cache := server.diskAnalysisCache
	server.mu.Unlock()
	if cache != nil {
		t.Fatal("cache was reopened after shutdown")
	}
	if _, rpcErr := server.handleRequest(context.Background(), "initialize", mustRaw(map[string]any{})); rpcErr == nil || rpcErr.Code != -32600 {
		t.Fatalf("post-shutdown request error = %#v, want Invalid Request", rpcErr)
	}
	if err := server.handleNotification(context.Background(), "workspace/didChangeConfiguration", mustRaw(map[string]any{
		"settings": map[string]any{"aspLsp": map[string]any{"cache": map[string]any{"enabled": true}}},
	})); err != nil {
		t.Fatalf("post-shutdown notification returned an error: %v", err)
	}
}
