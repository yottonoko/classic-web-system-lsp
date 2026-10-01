package lspserver

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

type caseSensitiveFilesystemBackend struct{}

func (caseSensitiveFilesystemBackend) Stat(fileName string) (workspacepkg.FsGatewayStats, error) {
	info, err := os.Stat(fileName)
	if err != nil {
		return workspacepkg.FsGatewayStats{}, err
	}
	if strings.EqualFold(filepath.Base(fileName), "shared.inc") && filepath.Base(fileName) != "Shared.inc" {
		return workspacepkg.FsGatewayStats{}, errors.New("case-sensitive path mismatch")
	}
	return workspacepkg.FsGatewayStats{MtimeMS: info.ModTime().UnixMilli(), Size: info.Size(), File: !info.IsDir(), Directory: info.IsDir()}, nil
}

func (caseSensitiveFilesystemBackend) ReadDir(directory string) ([]workspacepkg.FsGatewayDirent, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	result := make([]workspacepkg.FsGatewayDirent, 0, len(entries))
	for _, entry := range entries {
		result = append(result, workspacepkg.FsGatewayDirent{Name: entry.Name(), File: !entry.IsDir(), Directory: entry.IsDir()})
	}
	return result, nil
}

func TestRuntimeNetworkProfileConfiguresGatewayAndReadLimit(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	params := mustRaw(map[string]any{"settings": map[string]any{"aspLsp": map[string]any{
		"network": map[string]any{
			"profile":                "network",
			"statCacheTtlMs":         1200,
			"readdirCacheTtlMs":      2300,
			"includeReadConcurrency": 3,
			"caseResolution":         "full",
		},
	}}})
	if err := server.handleNotification(t.Context(), "workspace/didChangeConfiguration", params); err != nil {
		t.Fatal(err)
	}
	profile := server.resolveNetworkProfile()
	if profile.Kind != "network" || profile.StatTTL.Milliseconds() != 1200 || profile.ReadDirTTL.Milliseconds() != 2300 ||
		profile.IncludeReadConcurrency != 3 || profile.CaseResolution != "full" {
		t.Fatalf("resolved profile mismatch: %#v", profile)
	}
	if server.fsGateway == nil || cap(server.includeReadLimiter) != 3 {
		t.Fatalf("filesystem runtime was not configured: gateway=%#v limiter=%d", server.fsGateway, cap(server.includeReadLimiter))
	}
}

func TestRuntimeLocalProfileUsesShortDirectoryCache(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	profile := server.resolveNetworkProfile()
	if profile.Kind != "local" || profile.StatTTL != 0 || profile.ReadDirTTL != defaultLocalReadDirCacheTTL {
		t.Fatalf("resolved local profile mismatch: %#v", profile)
	}
	if profile.IncludeReadConcurrency != max(1, runtime.NumCPU()) {
		t.Fatalf("local include concurrency = %d, want %d", profile.IncludeReadConcurrency, max(1, runtime.NumCPU()))
	}
	server.settings.NetworkProfile = "network"
	if network := server.resolveNetworkProfile(); network.IncludeReadConcurrency != max(1, runtime.NumCPU()) {
		t.Fatalf("network include concurrency = %d, want %d", network.IncludeReadConcurrency, max(1, runtime.NumCPU()))
	}
}

func TestRuntimeFilesystemWatcherInvalidatesNegativeStatCache(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "created.inc")
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{URI: server.rootURI, Path: root}}
	server.settings.NetworkProfile = "network"
	server.configureFsGateway()
	if _, ok := server.fsStat(path); ok {
		t.Fatal("missing file unexpectedly existed")
	}
	if err := os.WriteFile(path, []byte("created"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := server.fsStat(path); ok {
		t.Fatal("negative stat cache did not retain the missing result before invalidation")
	}
	params := mustRaw(map[string]any{"changes": []map[string]any{{"uri": filePathURI(path), "type": fileChangeCreated}}})
	if err := server.handleNotification(t.Context(), "workspace/didChangeWatchedFiles", params); err != nil {
		t.Fatal(err)
	}
	if stats, ok := server.fsStat(path); !ok || stats == nil || !stats.File {
		t.Fatalf("watch invalidation did not expose created file: %#v, %v", stats, ok)
	}
}

func TestRuntimeCaseResolutionUsesGatewayDirectoryCache(t *testing.T) {
	root := t.TempDir()
	mixed := filepath.Join(root, "MixedCase.inc")
	if err := os.WriteFile(mixed, []byte("<% Const Value = 1 %>"), 0o644); err != nil {
		t.Fatal(err)
	}
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.settings.NetworkProfile = "network"
	server.configureFsGateway()
	resolved, ok := server.resolveCaseInsensitivePath(filepath.Join(root, "mixedcase.inc"))
	if !ok || resolved != mixed {
		t.Fatalf("resolveCaseInsensitivePath() = %q, %v; want %q", resolved, ok, mixed)
	}
}

func TestRuntimeIncludeResolutionSupportsNestedMixedCaseWindowsPath(t *testing.T) {
	root := t.TempDir()
	shared := filepath.Join(root, "Shared")
	if err := os.Mkdir(shared, 0o755); err != nil {
		t.Fatal(err)
	}
	owner := filepath.Join(root, "default.asp")
	target := filepath.Join(shared, "MixedCase.INC")
	if err := os.WriteFile(owner, []byte("<% %>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("<% Const Value = 1 %>"), 0o644); err != nil {
		t.Fatal(err)
	}
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{URI: server.rootURI, Path: root}, {URI: filePathURI(shared), Path: shared}}
	server.configureFsGateway()
	details, ok := server.includeTargetDetailsForMode(filePathURI(owner), "sHaReD/mIxEdCaSe.InC", "file")
	if !ok || !details.Exists || filepath.Clean(details.Path) != filepath.Clean(target) {
		t.Fatalf("mixed-case include details = %#v, %v; want existing %q", details, ok, target)
	}
}

func TestRuntimeNetworkCaseResolutionAffectsIncludeResolution(t *testing.T) {
	root := t.TempDir()
	owner := filepath.Join(root, "default.asp")
	mixed := filepath.Join(root, "Shared.inc")
	if err := os.WriteFile(owner, []byte("<!-- #include file=\"shared.inc\" -->"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mixed, []byte("<% Const Value = 1 %>"), 0o644); err != nil {
		t.Fatal(err)
	}
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = root
	server.workspaceRoots = []workspaceRoot{{URI: filePathURI(root), Path: root}}
	server.settings.NetworkProfile = "network"
	server.settings.NetworkCaseResolution = "fast"
	server.fsGateway = workspacepkg.NewFsGateway(caseSensitiveFilesystemBackend{}, workspacepkg.FsGatewayOptions{})
	fast, ok := server.includeTargetDetailsForMode(filePathURI(owner), "shared.inc", "file")
	if !ok || fast.Exists {
		t.Fatalf("fast network include resolution = %#v, %v; want missing exact-case path", fast, ok)
	}
	server.settings.NetworkCaseResolution = "full"
	server.configureFsGateway()
	full, ok := server.includeTargetDetailsForMode(filePathURI(owner), "shared.inc", "file")
	if !ok || !full.Exists || filepath.Clean(full.Path) != filepath.Clean(mixed) || !full.CaseMismatch {
		t.Fatalf("full network include resolution = %#v, %v; want %q with case mismatch", full, ok, mixed)
	}
}

func TestRuntimeFilesystemAllowsSiblingIncludesForExternalOpenDocuments(t *testing.T) {
	workspaceDir := t.TempDir()
	externalRoot := t.TempDir()
	ownerPath := filepath.Join(externalRoot, "default.asp")
	includePath := filepath.Join(externalRoot, "shared.inc")

	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = workspaceDir
	server.rootURI = filePathURI(workspaceDir)
	server.workspaceRoots = []workspaceRoot{{Path: workspaceDir, URI: filePathURI(workspaceDir)}}
	owner := core.NewTextDocument(filePathURI(ownerPath), "classic-asp", 1, `<!-- #include file="shared.inc" -->`)
	included := core.NewTextDocument(filePathURI(includePath), "classic-asp", 1, `<% Const SharedValue = 1 %>`)
	server.mu.Lock()
	server.rememberOpenDocumentLocked(owner.URI, owner)
	server.rememberOpenDocumentLocked(included.URI, included)
	server.mu.Unlock()

	if !server.workspaceSourcePathAllowed(includePath) {
		t.Fatal("an explicitly opened external document's directory was not trusted")
	}
	details, ok := server.includeTargetDetailsForMode(owner.URI, "shared.inc", "file")
	if !ok || !details.Exists || filepath.Clean(details.Path) != filepath.Clean(includePath) {
		t.Fatalf("external sibling include = %#v, %v; want %q", details, ok, includePath)
	}
}

func newNetworkProfileTrustTestServer(t *testing.T, root string) *Server {
	t.Helper()
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{URI: server.rootURI, Path: root}}
	server.settings.NetworkProfile = "network"
	server.configureFsGateway()
	if !server.trustedPaths.enabled() {
		t.Fatal("network profile did not enable the trusted path cache")
	}
	return server
}

func TestRuntimeNetworkProfileTrustChecksStillRejectSymlinks(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.inc"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "pages"), 0o755); err != nil {
		t.Fatal(err)
	}
	page := filepath.Join(root, "pages", "default.asp")
	if err := os.WriteFile(page, []byte("<% %>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.inc"), filepath.Join(root, "pages", "file-link.inc")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "pages", "dir-link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	server := newNetworkProfileTrustTestServer(t, root)
	// The second round is served from the cache and must give the same answers.
	for round := 0; round < 2; round++ {
		if stats, ok := server.fsStat(page); !ok || stats == nil || !stats.File {
			t.Fatalf("round %d: regular page was rejected: %#v, %v", round, stats, ok)
		}
		for _, linked := range []string{filepath.Join(root, "pages", "file-link.inc"), filepath.Join(root, "pages", "dir-link", "secret.inc")} {
			if _, ok := server.trustedFilesystemPath(linked); ok {
				t.Fatalf("round %d: symlinked path %s was trusted", round, linked)
			}
		}
	}
}

func TestRuntimeNetworkProfileWatchedChangesDropCachedTrustChecks(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	directory := filepath.Join(root, "pages")
	if err := os.WriteFile(filepath.Join(outside, "default.asp"), []byte("<% %>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	page := filepath.Join(directory, "default.asp")
	if err := os.WriteFile(page, []byte("<% %>"), 0o644); err != nil {
		t.Fatal(err)
	}
	server := newNetworkProfileTrustTestServer(t, root)
	if _, ok := server.trustedFilesystemPath(page); !ok {
		t.Fatal("regular page was rejected")
	}
	if err := os.Rename(directory, directory+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, directory); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	// Watchers report the file event; the replaced parent directory must not stay cached.
	params := mustRaw(map[string]any{"changes": []map[string]any{{"uri": filePathURI(page), "type": fileChangeChanged}}})
	if err := server.handleNotification(t.Context(), "workspace/didChangeWatchedFiles", params); err != nil {
		t.Fatal(err)
	}
	if _, ok := server.trustedFilesystemPath(page); ok {
		t.Fatal("page under a directory replaced by a symlink stayed trusted after the watched change")
	}
}
