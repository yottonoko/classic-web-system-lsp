package workspace

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestDiskAnalysisCacheOwnsAllQueuedWriteData(t *testing.T) {
	cache := newTestDiskAnalysisCache(t, t.TempDir(), 0, 0, false)
	lookup := testDiskLookup("queued-ownership")
	parsed := core.ParseDocument("file:///site/queued.asp", "<% Dim value %>", core.Settings{})
	parsed.ChangeImpact.Languages = []core.EmbeddedLanguage{core.LanguageHTML}
	parsed.StoreAnalysis("original", map[string]any{"value": "original"})
	signature := map[string]any{"nested": map[string]any{"value": "original"}}
	diagnostics := []lsp.Diagnostic{{
		Message: "original",
		Tags:    []lsp.DiagnosticTag{lsp.DiagnosticTagUnnecessary},
		Data:    map[string]any{"nested": map[string]any{"value": "original"}},
	}}
	includeDeps := []any{map[string]any{"value": "original"}}
	summary := DiskFileAnalysisSummary{
		LanguageRegions: []core.Region{{Start: 1, End: 2}},
		IncludeRefs:     []DiskIncludeRef{{Path: "original.inc"}},
		Diagnostics:     []lsp.Diagnostic{{Message: "summary-original"}},
	}
	analysisSnapshot := json.RawMessage(`{"version":1}`)
	if err := cache.WriteFileBundle(DiskFileBundleCacheEntry{
		DiskAnalysisCacheLookup: lookup,
		Parsed:                  parsed,
		Summary:                 summary,
		PublicSignature:         signature,
		AnalysisSnapshot:        analysisSnapshot,
		Diagnostics:             diagnostics,
		BuilderState: &DiskAnalysisBuilderState{
			PublicSignature:              signature,
			IncludeDeps:                  includeDeps,
			ExternalRefUsageKeys:         []string{"original"},
			DiagnosticsLayerFingerprints: map[string]string{"layer": "original"},
		},
		UpdateParsed:      true,
		UpdateDiagnostics: true,
	}); err != nil {
		t.Fatal(err)
	}

	parsed.Regions[0].Start = 99
	parsed.ChangeImpact.Languages[0] = core.LanguageCSS
	parsed.StoreAnalysis("caller", map[string]any{"value": "changed"})
	signature["nested"].(map[string]any)["value"] = "changed"
	diagnostics[0].Tags[0] = lsp.DiagnosticTagDeprecated
	diagnostics[0].Data.(map[string]any)["nested"].(map[string]any)["value"] = "changed"
	includeDeps[0].(map[string]any)["value"] = "changed"
	summary.LanguageRegions[0].Start = 99
	summary.IncludeRefs[0].Path = "changed.inc"
	summary.Diagnostics[0].Message = "summary-changed"
	analysisSnapshot[0] = '{'

	assertQueuedDiskAnalysisCacheDataUnchanged(t, cache, lookup)
	if err := cache.Flush(); err != nil {
		t.Fatal(err)
	}
	assertQueuedDiskAnalysisCacheDataUnchanged(t, cache, lookup)
}

func assertQueuedDiskAnalysisCacheDataUnchanged(t *testing.T, cache *DiskAnalysisCache, lookup DiskAnalysisCacheLookup) {
	t.Helper()
	bundle, ok := cache.ReadFileBundle(lookup)
	if !ok || bundle.Parsed == nil || len(bundle.Diagnostics) != 1 || bundle.BuilderState == nil {
		t.Fatalf("ReadFileBundle() = %#v, %v", bundle, ok)
	}
	if len(bundle.Parsed.Regions) != 1 || bundle.Parsed.Regions[0].Start != 0 || (len(bundle.Parsed.ChangeImpact.Languages) > 0 && bundle.Parsed.ChangeImpact.Languages[0] != core.LanguageHTML) {
		t.Fatalf("parsed data was aliased: %#v", bundle.Parsed)
	}
	var analysis map[string]any
	if bundle.Parsed.LoadAnalysis("caller", &analysis) {
		t.Fatal("queued parsed analysis retained a caller mutation")
	}
	if got := bundle.PublicSignature.(map[string]any)["nested"].(map[string]any)["value"]; got != "original" {
		t.Fatalf("public signature = %v, want original", got)
	}
	if bundle.Diagnostics[0].Tags[0] != lsp.DiagnosticTagUnnecessary || bundle.Diagnostics[0].Data.(map[string]any)["nested"].(map[string]any)["value"] != "original" {
		t.Fatalf("diagnostics were aliased: %#v", bundle.Diagnostics[0])
	}
	if bundle.BuilderState.IncludeDeps[0].(map[string]any)["value"] != "original" || bundle.BuilderState.DiagnosticsLayerFingerprints["layer"] != "original" {
		t.Fatalf("builder state was aliased: %#v", bundle.BuilderState)
	}
	if bundle.Summary.LanguageRegions[0].Start != 1 || bundle.Summary.IncludeRefs[0].Path != "original.inc" || bundle.Summary.Diagnostics[0].Message != "summary-original" {
		t.Fatalf("summary was aliased: %#v", bundle.Summary)
	}
	if !bytes.Equal(bundle.AnalysisSnapshot, json.RawMessage(`{"version":1}`)) {
		t.Fatalf("analysis snapshot = %s", bundle.AnalysisSnapshot)
	}
}

func TestDiskAnalysisCacheCapsGzipDecompression(t *testing.T) {
	cache := newTestDiskAnalysisCache(t, t.TempDir(), 0, 0, true)
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	chunk := bytes.Repeat([]byte{'x'}, 64*1024)
	remaining := maxDiskCacheRecordBytes + 1
	for remaining > 0 {
		writeSize := len(chunk)
		if writeSize > remaining {
			writeSize = remaining
		}
		if _, err := writer.Write(chunk[:writeSize]); err != nil {
			t.Fatal(err)
		}
		remaining -= writeSize
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.decodeEntry(compressed.Bytes()); err != errDiskCacheRecordTooLarge {
		t.Fatalf("decodeEntry() error = %v, want %v", err, errDiskCacheRecordTooLarge)
	}
}

func TestDiskAnalysisCacheRejectsDatabaseSymlinkComponents(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	databaseDirectory := filepath.Join(root, "bbolt-v1")
	if err := os.Symlink(outside, databaseDirectory); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}

	_, err := NewDiskAnalysisCache(testDiskCacheOptions(root, 0, 0, false))
	if err == nil {
		t.Fatal("NewDiskAnalysisCache() accepted a symlinked database directory")
	}
	if _, statErr := os.Stat(filepath.Join(outside, "test.db")); !os.IsNotExist(statErr) {
		t.Fatalf("cache created a database through the symlink: %v", statErr)
	}
}

func TestDiskAnalysisCacheRejectsParentSymlinkBeforeCreatingRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	linkedParent := filepath.Join(root, "linked-parent")
	if err := os.Symlink(outside, linkedParent); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	cacheRoot := filepath.Join(linkedParent, "new-cache")
	_, err := NewDiskAnalysisCache(testDiskCacheOptions(cacheRoot, 0, 0, false))
	if err == nil {
		t.Fatal("NewDiskAnalysisCache() accepted a parent symlink for a missing root")
	}
	if _, statErr := os.Stat(filepath.Join(outside, "new-cache")); !os.IsNotExist(statErr) {
		t.Fatalf("cache created a database through the parent symlink: %v", statErr)
	}
}

func TestDiskAnalysisCacheRejectsBackupSymlinkRecovery(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.db")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	databaseDirectory := filepath.Join(root, "bbolt-v1")
	if err := os.MkdirAll(databaseDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	backupPath := filepath.Join(databaseDirectory, "test.db.backup")
	if err := os.Symlink(outside, backupPath); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}

	cache, err := NewDiskAnalysisCache(testDiskCacheOptions(root, 0, 0, false))
	if cache != nil {
		_ = cache.Close()
	}
	if err == nil {
		t.Fatal("NewDiskAnalysisCache() accepted a symlinked backup")
	}
	if got, readErr := os.ReadFile(outside); readErr != nil || string(got) != "outside" {
		t.Fatalf("outside backup target after recovery = %q, %v", got, readErr)
	}
	if info, statErr := os.Lstat(backupPath); statErr != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("backup symlink after rejected recovery = %v, %v", info, statErr)
	}
}

func TestDiskAnalysisCacheRestoresValidBackupWhenPrimaryIsCorrupt(t *testing.T) {
	root := t.TempDir()
	cache := newTestDiskAnalysisCache(t, root, 0, 0, false)
	lookup := testDiskLookup("backup-recovery")
	if err := cache.WriteFileBundle(DiskFileBundleCacheEntry{
		DiskAnalysisCacheLookup: lookup,
		Diagnostics:             []lsp.Diagnostic{{Message: "backup-value"}},
		UpdateDiagnostics:       true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := cache.Flush(); err != nil {
		t.Fatal(err)
	}
	databasePath := cache.databasePath
	if err := cache.Close(); err != nil {
		t.Fatal(err)
	}
	backupPath := databasePath + ".backup"
	if err := os.Rename(databasePath, backupPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(databasePath, []byte("corrupt primary"), 0o600); err != nil {
		t.Fatal(err)
	}

	reopened, err := NewDiskAnalysisCache(testDiskCacheOptions(root, 0, 0, false))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	entry, ok := reopened.ReadFileBundle(lookup)
	if !ok || len(entry.Diagnostics) != 1 || entry.Diagnostics[0].Message != "backup-value" {
		t.Fatalf("backup recovery entry = %#v, %v", entry, ok)
	}
	if _, err := os.Lstat(backupPath); !os.IsNotExist(err) {
		t.Fatalf("backup artifact after successful recovery = %v", err)
	}
}

func TestDiskAnalysisCacheOwnsQueuedWorkspaceIndexEntries(t *testing.T) {
	cache := newTestDiskAnalysisCache(t, t.TempDir(), 0, 0, false)
	entries := []DiskWorkspaceIndexedDocument{{
		URI:      "file:///site/queued.asp",
		FileName: "/site/queued.asp",
		Text:     "original",
	}}
	if err := cache.WriteWorkspaceIndex(DiskWorkspaceIndexCacheEntry{SettingsKey: "queued", Entries: entries}); err != nil {
		t.Fatal(err)
	}
	entries[0].Text = "changed"
	entry, ok := cache.ReadWorkspaceIndex("queued")
	if !ok || len(entry.Entries) != 1 || entry.Entries[0].Text != "original" {
		t.Fatalf("ReadWorkspaceIndex() = %#v, %v", entry, ok)
	}
}
