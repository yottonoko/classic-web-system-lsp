package workspace

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	bolt "go.etcd.io/bbolt"
	bolterrors "go.etcd.io/bbolt/errors"
)

func TestDiskAnalysisCacheUsesSixteenGiBDefaultAndAcceptsOverride(t *testing.T) {
	defaultCache := newTestDiskAnalysisCache(t, t.TempDir(), 0, 0, false)
	if defaultCache.maxSize != 16*1024*1024*1024 {
		t.Fatalf("default target size = %d, want 16 GiB", defaultCache.maxSize)
	}

	overriddenCache := newTestDiskAnalysisCache(t, t.TempDir(), 0, 256*1024*1024, false)
	if overriddenCache.maxSize != 256*1024*1024 {
		t.Fatalf("overridden target size = %d, want 256 MiB", overriddenCache.maxSize)
	}
	if got, want := overriddenCache.cleanupTriggerSize(), int64(320*1024*1024); got != want {
		t.Fatalf("cleanup trigger size = %d, want %d", got, want)
	}
	if got, want := overriddenCache.nextCleanupSize(384*1024*1024), int64(448*1024*1024); got != want {
		t.Fatalf("next cleanup size = %d, want %d", got, want)
	}
}

func TestDiskAnalysisCacheStoresOneBboltDatabaseAndNoCBORFiles(t *testing.T) {
	directory := t.TempDir()
	cache := newTestDiskAnalysisCache(t, directory, 0, 0, false)
	lookup := testDiskLookup("settings")
	if err := cache.WriteFileBundle(DiskFileBundleCacheEntry{DiskAnalysisCacheLookup: lookup, UpdateDiagnostics: true}); err != nil {
		t.Fatal(err)
	}
	if err := cache.Flush(); err != nil {
		t.Fatal(err)
	}
	wantPath := filepath.Join(directory, "bbolt-v1", "test.db")
	if cache.databasePath != wantPath {
		t.Fatalf("database path = %q, want %q", cache.databasePath, wantPath)
	}
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("Stat(database) error = %v", err)
	}
	var cborFiles []string
	err := filepath.WalkDir(directory, func(path string, entry os.DirEntry, err error) error {
		if err == nil && !entry.IsDir() && filepath.Ext(path) == ".cbor" {
			cborFiles = append(cborFiles, path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(cborFiles) != 0 {
		t.Fatalf("legacy CBOR files = %v, want none", cborFiles)
	}
}

func TestInitializeDiskCacheDatabaseDoesNotWriteExistingDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.db")
	db, err := bolt.Open(path, 0o600, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := initializeDiskCacheDatabase(db, "test"); err != nil {
		t.Fatal(err)
	}
	statsBefore := db.Stats()
	writesBefore := statsBefore.TxStats.GetWrite()
	if err := initializeDiskCacheDatabase(db, "test"); err != nil {
		t.Fatal(err)
	}
	statsAfter := db.Stats()
	if writesAfter := statsAfter.TxStats.GetWrite(); writesAfter != writesBefore {
		t.Fatalf("existing database initialization writes = %d, want %d", writesAfter, writesBefore)
	}
}

func TestDiskAnalysisCacheReopensAndRejectsStaleMetadata(t *testing.T) {
	directory := t.TempDir()
	lookup := testDiskLookup("settings")
	cache := newTestDiskAnalysisCache(t, directory, 0, 0, false)
	if err := cache.WriteFileBundle(DiskFileBundleCacheEntry{
		DiskAnalysisCacheLookup: lookup,
		Diagnostics:             []lsp.Diagnostic{{Message: "cached"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := cache.Close(); err != nil {
		t.Fatal(err)
	}

	reopened := newTestDiskAnalysisCache(t, directory, 0, 0, false)
	bundle, ok := reopened.ReadFileBundle(lookup)
	if !ok || len(bundle.Diagnostics) != 1 || bundle.Diagnostics[0].Message != "cached" {
		t.Fatalf("ReadFileBundle() = %#v, %v; want cached", bundle, ok)
	}
	stale := lookup
	stale.Source.Size++
	if _, ok := reopened.ReadFileBundle(stale); ok {
		t.Fatal("ReadFileBundle() restored stale source metadata")
	}
	lookup.Source.ContentHash = "same"
	if err := reopened.WriteFileBundle(DiskFileBundleCacheEntry{DiskAnalysisCacheLookup: lookup, Diagnostics: []lsp.Diagnostic{{Message: "hash"}}}); err != nil {
		t.Fatal(err)
	}
	changedMetadata := lookup
	changedMetadata.Source.MtimeMS++
	changedMetadata.Source.Size++
	if bundle, ok := reopened.ReadFileBundle(changedMetadata); !ok || bundle.Diagnostics[0].Message != "hash" {
		t.Fatalf("ReadFileBundle() with matching content hash = %#v, %v", bundle, ok)
	}
}

func TestDiskAnalysisCacheFileComponentsMergeIntoOneBundle(t *testing.T) {
	cache := newTestDiskAnalysisCache(t, t.TempDir(), 0, 0, false)
	lookup := testDiskLookup("parsed-settings")
	diagnosticsLookup := lookup
	diagnosticsLookup.SettingsKey = "diagnostics-settings"
	parsed := core.ParseDocument("file:///site/default.asp", "<% Dim first, second %>", core.Settings{})
	summary := DiskFileAnalysisSummary{URI: parsed.URI, Fingerprint: "parsed", IncludeRefs: []DiskIncludeRef{{Path: "shared.inc"}}}
	if err := cache.WriteFileBundle(DiskFileBundleCacheEntry{
		DiskAnalysisCacheLookup: lookup,
		Parsed:                  parsed,
		Summary:                 summary,
		PublicSignature:         map[string]string{"hash": "public"},
		AnalysisSnapshot:        json.RawMessage(`{"version":1}`),
	}); err != nil {
		t.Fatal(err)
	}
	if err := cache.WriteFileBundle(DiskFileBundleCacheEntry{
		DiskAnalysisCacheLookup: diagnosticsLookup,
		Diagnostics:             []lsp.Diagnostic{{Message: "cached"}},
		UpdateDiagnostics:       true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := cache.Flush(); err != nil {
		t.Fatal(err)
	}
	parsedBundle, ok := cache.ReadFileBundle(lookup)
	if !ok || parsedBundle.Parsed == nil || parsedBundle.Parsed.Text != parsed.Text || string(parsedBundle.AnalysisSnapshot) != `{"version":1}` || parsedBundle.Diagnostics != nil {
		t.Fatalf("ReadFileBundle(parsed) = %#v, %v; want parsed component only", parsedBundle, ok)
	}
	diagnosticsBundle, ok := cache.ReadFileBundle(diagnosticsLookup)
	if !ok || diagnosticsBundle.Parsed != nil || len(diagnosticsBundle.Diagnostics) != 1 || diagnosticsBundle.Diagnostics[0].Message != "cached" {
		t.Fatalf("ReadFileBundle(diagnostics) = %#v, %v; want diagnostics component only", diagnosticsBundle, ok)
	}
	staleLookup := lookup
	staleLookup.SettingsKey = "stale-settings"
	if staleBundle, ok := cache.ReadFileBundle(staleLookup); ok {
		t.Fatalf("ReadFileBundle(stale settings) = %#v, true; want miss", staleBundle)
	}
	assertBucketKeyCount(t, cache, diskCacheFilesBucket, 1)
}

func TestDiskAnalysisCacheFileComponentsMergeWithStoredBundle(t *testing.T) {
	cache := newTestDiskAnalysisCache(t, t.TempDir(), 0, 0, false)
	parsedLookup := testDiskLookup("parsed-settings")
	diagnosticsLookup := parsedLookup
	diagnosticsLookup.SettingsKey = "diagnostics-settings"
	parsed := core.ParseDocument("file:///site/default.asp", "<% Dim first, second %>", core.Settings{})
	if err := cache.WriteFileBundle(DiskFileBundleCacheEntry{
		DiskAnalysisCacheLookup: parsedLookup,
		Parsed:                  parsed,
		AnalysisSnapshot:        json.RawMessage(`{"version":1}`),
	}); err != nil {
		t.Fatal(err)
	}
	if bundle, ok := cache.ReadFileBundle(parsedLookup); !ok || bundle.Parsed == nil || string(bundle.AnalysisSnapshot) != `{"version":1}` {
		t.Fatalf("ReadFileBundle(parsed pending) = %#v, %v; want stored parsed component", bundle, ok)
	}
	if err := cache.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := cache.WriteFileBundle(DiskFileBundleCacheEntry{
		DiskAnalysisCacheLookup: diagnosticsLookup,
		Diagnostics:             []lsp.Diagnostic{{Message: "cached"}},
		UpdateDiagnostics:       true,
	}); err != nil {
		t.Fatal(err)
	}
	if bundle, ok := cache.ReadFileBundle(parsedLookup); !ok || bundle.Parsed == nil || string(bundle.AnalysisSnapshot) != `{"version":1}` {
		t.Fatalf("ReadFileBundle(parsed while diagnostics pending) = %#v, %v; want stored parsed component", bundle, ok)
	}
	if err := cache.Flush(); err != nil {
		t.Fatal(err)
	}
	if bundle, ok := cache.ReadFileBundle(parsedLookup); !ok || bundle.Parsed == nil || string(bundle.AnalysisSnapshot) != `{"version":1}` {
		t.Fatalf("ReadFileBundle(parsed) = %#v, %v; want stored parsed component", bundle, ok)
	}
	if bundle, ok := cache.ReadFileBundle(diagnosticsLookup); !ok || len(bundle.Diagnostics) != 1 || bundle.Diagnostics[0].Message != "cached" {
		t.Fatalf("ReadFileBundle(diagnostics) = %#v, %v; want updated diagnostics component", bundle, ok)
	}
}

func TestDiskAnalysisCacheOwnsQueuedAndReturnedParsedDocuments(t *testing.T) {
	cache := newTestDiskAnalysisCache(t, t.TempDir(), 0, 0, false)
	lookup := testDiskLookup("parsed-settings")
	parsed := core.ParseDocument("file:///site/default.asp", "<% Dim value %>", core.Settings{})
	parsed.StoreAnalysis("test", map[string]int{"original": 1})
	if err := cache.WriteFileBundle(DiskFileBundleCacheEntry{
		DiskAnalysisCacheLookup: lookup,
		Parsed:                  parsed,
		AnalysisSnapshot:        json.RawMessage(`{"version":1}`),
	}); err != nil {
		t.Fatal(err)
	}
	parsed.StoreAnalysis("caller", map[string]int{"changed": 1})
	returned, ok := cache.ReadFileBundle(lookup)
	if !ok || returned.Parsed == nil {
		t.Fatalf("ReadFileBundle() = %#v, %v", returned, ok)
	}
	returned.Parsed.StoreAnalysis("reader", map[string]int{"changed": 1})
	if err := cache.Flush(); err != nil {
		t.Fatal(err)
	}
	stored, ok := cache.ReadFileBundle(lookup)
	if !ok || stored.Parsed == nil {
		t.Fatalf("stored bundle = %#v, %v", stored, ok)
	}
	if _, exists := stored.Parsed.Analysis["caller"]; exists {
		t.Fatal("queued parsed document retained a caller mutation")
	}
	if _, exists := stored.Parsed.Analysis["reader"]; exists {
		t.Fatal("queued parsed document retained a returned-value mutation")
	}
}

func TestDiskAnalysisCachePendingReadsReturnIsolatedParsedDocuments(t *testing.T) {
	cache := newTestDiskAnalysisCache(t, t.TempDir(), 0, 0, false)
	lookup := testDiskLookup("pending-isolation")
	parsed := core.ParseDocument("file:///site/pending.asp", "<% Dim value %>", core.Settings{})
	parsed.StoreAnalysis("original", map[string]any{"value": true})
	if err := cache.WriteFileBundle(DiskFileBundleCacheEntry{
		DiskAnalysisCacheLookup: lookup,
		Parsed:                  parsed,
		PublicSignature:         map[string]any{"nested": map[string]any{"value": "original"}},
		Diagnostics: []lsp.Diagnostic{{
			Message: "original", Tags: []lsp.DiagnosticTag{lsp.DiagnosticTagUnnecessary},
			Data: map[string]any{"nested": map[string]any{"value": "original"}},
		}},
		BuilderState: &DiskAnalysisBuilderState{
			IncludeDeps:                  []any{map[string]any{"value": "original"}},
			DiagnosticsLayerFingerprints: map[string]string{"layer": "original"},
		},
		UpdateParsed:      true,
		UpdateDiagnostics: true,
	}); err != nil {
		t.Fatal(err)
	}

	first, ok := cache.ReadFileBundle(lookup)
	if !ok || first.Parsed == nil {
		t.Fatalf("ReadFileBundle() = %#v, %v; want pending parsed document", first, ok)
	}
	first.Parsed.StoreAnalysis("reader", map[string]any{"value": true})
	first.PublicSignature.(map[string]any)["nested"].(map[string]any)["value"] = "changed"
	first.Diagnostics[0].Tags[0] = lsp.DiagnosticTagDeprecated
	first.Diagnostics[0].Data.(map[string]any)["nested"].(map[string]any)["value"] = "changed"
	first.BuilderState.IncludeDeps[0].(map[string]any)["value"] = "changed"
	first.BuilderState.DiagnosticsLayerFingerprints["layer"] = "changed"
	second, ok := cache.ReadFileBundle(lookup)
	if !ok || second.Parsed == nil {
		t.Fatalf("second ReadFileBundle() = %#v, %v", second, ok)
	}
	var readerValue map[string]any
	if second.Parsed.LoadAnalysis("reader", &readerValue) {
		t.Fatal("pending cache read mutated the queued parsed document")
	}
	if got := second.PublicSignature.(map[string]any)["nested"].(map[string]any)["value"]; got != "original" {
		t.Fatalf("pending public signature = %v, want original", got)
	}
	if second.Diagnostics[0].Tags[0] != lsp.DiagnosticTagUnnecessary || second.Diagnostics[0].Data.(map[string]any)["nested"].(map[string]any)["value"] != "original" {
		t.Fatalf("pending diagnostics were mutated: %#v", second.Diagnostics[0])
	}
	if second.BuilderState.IncludeDeps[0].(map[string]any)["value"] != "original" || second.BuilderState.DiagnosticsLayerFingerprints["layer"] != "original" {
		t.Fatalf("pending builder state was mutated: %#v", second.BuilderState)
	}
	if err := cache.Flush(); err != nil {
		t.Fatal(err)
	}
}

func TestDiskAnalysisCacheWriteFileBundleReplacesAtomically(t *testing.T) {
	cache := newTestDiskAnalysisCache(t, t.TempDir(), 0, 0, false)
	lookup := testDiskLookup("bundle")
	parsed := core.ParseDocument("file:///site/default.asp", "<% Const answer = 42 %>", core.Settings{})
	want := DiskFileBundleCacheEntry{
		DiskAnalysisCacheLookup: lookup,
		Parsed:                  parsed,
		Summary:                 DiskFileAnalysisSummary{URI: parsed.URI, Fingerprint: "summary"},
		AnalysisSnapshot:        json.RawMessage(`{"symbols":2}`),
		Diagnostics:             []lsp.Diagnostic{{Message: "bundle"}},
	}
	if err := cache.WriteFileBundle(want); err != nil {
		t.Fatal(err)
	}
	got, ok := cache.ReadFileBundle(lookup)
	if !ok || got.Parsed.Text != want.Parsed.Text || got.Summary.Fingerprint != "summary" || string(got.AnalysisSnapshot) != `{"symbols":2}` || got.Diagnostics[0].Message != "bundle" {
		t.Fatalf("ReadFileBundle() = %#v, %v", got, ok)
	}
}

func TestDiskAnalysisCacheReadsGzipAndPlainValuesAcrossSettingChanges(t *testing.T) {
	directory := t.TempDir()
	lookup := testDiskLookup("gzip")
	plain := newTestDiskAnalysisCache(t, directory, 0, 0, false)
	if err := plain.WriteFileBundle(DiskFileBundleCacheEntry{DiskAnalysisCacheLookup: lookup, Diagnostics: []lsp.Diagnostic{{Message: "plain"}}}); err != nil {
		t.Fatal(err)
	}
	if err := plain.Close(); err != nil {
		t.Fatal(err)
	}
	if payload := readOnlyFileValue(t, plain.databasePath); len(payload) >= 2 && payload[0] == 0x1f && payload[1] == 0x8b {
		t.Fatal("plain value is gzip encoded")
	}

	gzipped := newTestDiskAnalysisCache(t, directory, 0, 0, true)
	if bundle, ok := gzipped.ReadFileBundle(lookup); !ok || bundle.Diagnostics[0].Message != "plain" {
		t.Fatalf("gzip cache did not restore plain value: %#v, %v", bundle, ok)
	}
	if err := gzipped.WriteFileBundle(DiskFileBundleCacheEntry{DiskAnalysisCacheLookup: lookup, Diagnostics: []lsp.Diagnostic{{Message: "gzip"}}}); err != nil {
		t.Fatal(err)
	}
	if err := gzipped.Close(); err != nil {
		t.Fatal(err)
	}
	if payload := readOnlyFileValue(t, gzipped.databasePath); len(payload) < 2 || payload[0] != 0x1f || payload[1] != 0x8b {
		t.Fatal("gzip value is missing gzip magic")
	}

	plainAgain := newTestDiskAnalysisCache(t, directory, 0, 0, false)
	if bundle, ok := plainAgain.ReadFileBundle(lookup); !ok || bundle.Diagnostics[0].Message != "gzip" {
		t.Fatalf("plain cache did not restore gzip value: %#v, %v", bundle, ok)
	}
}

func TestDiskAnalysisCacheSoftTargetDoesNotLimitGzipEntryDecoding(t *testing.T) {
	directory := t.TempDir()
	lookup := testDiskLookup("large-gzip")
	want := strings.Repeat("cached", 256*1024)
	cache := newTestDiskAnalysisCache(t, directory, 0, 512, true)
	if err := cache.WriteFileBundle(DiskFileBundleCacheEntry{
		DiskAnalysisCacheLookup: lookup,
		Diagnostics:             []lsp.Diagnostic{{Message: want}},
		UpdateDiagnostics:       true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := cache.Close(); err != nil {
		t.Fatal(err)
	}

	reopened := newTestDiskAnalysisCache(t, directory, 0, 512, true)
	bundle, ok := reopened.ReadFileBundle(lookup)
	if !ok || len(bundle.Diagnostics) != 1 || bundle.Diagnostics[0].Message != want {
		t.Fatalf("large entry above soft target was not restored: diagnostics=%d, ok=%v", len(bundle.Diagnostics), ok)
	}
}

func TestDiskAnalysisCacheWarmReadDoesNotWriteDatabase(t *testing.T) {
	cache := newTestDiskAnalysisCache(t, t.TempDir(), 0, 0, false)
	lookup := testDiskLookup("read-only")
	if err := cache.WriteFileBundle(DiskFileBundleCacheEntry{DiskAnalysisCacheLookup: lookup, Diagnostics: []lsp.Diagnostic{{Message: "cached"}}}); err != nil {
		t.Fatal(err)
	}
	if err := cache.Flush(); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(cache.databasePath)
	if err != nil {
		t.Fatal(err)
	}
	for range 100 {
		if _, ok := cache.ReadFileBundle(lookup); !ok {
			t.Fatal("warm cache read missed")
		}
	}
	after, err := os.Stat(cache.databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) || after.Size() != before.Size() {
		t.Fatalf("warm reads changed database metadata: before=%v/%d after=%v/%d", before.ModTime(), before.Size(), after.ModTime(), after.Size())
	}
}

func TestDiskAnalysisCacheRestoresWorkspaceEntries(t *testing.T) {
	cache := newTestDiskAnalysisCache(t, t.TempDir(), 0, 0, false)
	if err := cache.WriteWorkspaceIndex(DiskWorkspaceIndexCacheEntry{
		SettingsKey: "workspace",
		Entries:     []DiskWorkspaceIndexedDocument{{URI: "file:///site/default.asp", FileName: "/site/default.asp"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := cache.WriteWorkspaceIncludeGraph(DiskWorkspaceIncludeGraphCacheEntry{
		SettingsKey: "includes",
		Entries:     []IncludeGraphEntry{{FileName: "/site/default.asp", TargetFileNames: []string{"/site/shared.inc"}}},
	}); err != nil {
		t.Fatal(err)
	}
	graphPayload := json.RawMessage(`{"nodes":[]}`)
	if err := cache.WriteGraphPayload(DiskGraphPayloadCacheEntry{SettingsKey: "graph", Payload: graphPayload}); err != nil {
		t.Fatal(err)
	}
	referenceBatchPayload := json.RawMessage(`{"generation":7,"references":{"sharedTitle":2}}`)
	if err := cache.WriteWorkspaceReferenceBatch(DiskWorkspaceReferenceBatchCacheEntry{
		SettingsKey: "references",
		Payload:     referenceBatchPayload,
	}); err != nil {
		t.Fatal(err)
	}
	legacyUndefinedGlobalsPayload := json.RawMessage(`{"documents":2,"globals":["LegacyValue"]}`)
	if err := cache.WriteWorkspaceLegacyUndefinedGlobals(DiskWorkspaceLegacyUndefinedGlobalsCacheEntry{
		SettingsKey: "legacy-undefined-globals",
		Payload:     legacyUndefinedGlobalsPayload,
	}); err != nil {
		t.Fatal(err)
	}
	if entry, ok := cache.ReadWorkspaceIndex("workspace"); !ok || entry.Entries[0].URI != "file:///site/default.asp" {
		t.Fatalf("ReadWorkspaceIndex() = %#v, %v", entry, ok)
	}
	if entry, ok := cache.ReadWorkspaceIncludeGraph("includes"); !ok || entry.Entries[0].TargetFileNames[0] != "/site/shared.inc" {
		t.Fatalf("ReadWorkspaceIncludeGraph() = %#v, %v", entry, ok)
	}
	if entry, ok := cache.ReadGraphPayload("graph"); !ok || !bytes.Equal(entry.Payload, graphPayload) {
		t.Fatalf("ReadGraphPayload() = %#v, %v", entry, ok)
	}
	if entry, ok := cache.ReadWorkspaceReferenceBatch("references"); !ok || !bytes.Equal(entry.Payload, referenceBatchPayload) {
		t.Fatalf("ReadWorkspaceReferenceBatch() = %#v, %v", entry, ok)
	}
	if entry, ok := cache.ReadWorkspaceLegacyUndefinedGlobals("legacy-undefined-globals"); !ok || !bytes.Equal(entry.Payload, legacyUndefinedGlobalsPayload) {
		t.Fatalf("ReadWorkspaceLegacyUndefinedGlobals() = %#v, %v", entry, ok)
	}
	assertBucketKeyCount(t, cache, diskCacheWorkspaceBucket, 5)
}

func TestDiskAnalysisCacheNormalizesAndOwnsWorkspaceMembershipManifest(t *testing.T) {
	cache := newTestDiskAnalysisCache(t, t.TempDir(), 0, 0, false)
	manifest := DiskWorkspaceMembershipManifest{
		SettingsKey: "workspace-v1",
		DocumentIDs: []string{
			"file:///C:/Site/Middle.inc",
			"file:///c:/site/middle.inc",
			"file://Server/Share/Leaf.inc",
		},
	}
	if err := cache.QueueWorkspaceMembershipManifest(manifest); err != nil {
		t.Fatal(err)
	}
	manifest.DocumentIDs[0] = "caller-mutated"

	first, ok := cache.ReadWorkspaceMembershipManifest("workspace-v1")
	if !ok {
		t.Fatal("ReadWorkspaceMembershipManifest() missed pending manifest")
	}
	want := []string{"//server/share/leaf.inc", "c:/site/middle.inc"}
	if !reflect.DeepEqual(first.DocumentIDs, want) || first.SchemaVersion != 1 || first.Fingerprint == "" {
		t.Fatalf("manifest = %#v, want normalized %#v", first, want)
	}
	first.DocumentIDs[0] = "reader-mutated"
	if err := cache.Flush(); err != nil {
		t.Fatal(err)
	}
	second, ok := cache.ReadWorkspaceMembershipManifest("workspace-v1")
	if !ok || !reflect.DeepEqual(second.DocumentIDs, want) {
		t.Fatalf("stored manifest = %#v, %v; want isolated normalized IDs", second, ok)
	}
}

func TestDiskAnalysisCacheDocumentDeltasUseIndependentArtifactKeys(t *testing.T) {
	cache := newTestDiskAnalysisCache(t, t.TempDir(), 0, 0, false)
	documentID := "file:///C:/Site/Leaf.inc"
	firstPayload := json.RawMessage(`{"name":"first"}`)
	secondPayload := json.RawMessage(`{"name":"second"}`)
	statsBefore := cache.db.Stats().TxN
	if err := cache.WriteDocumentCacheDeltas([]DiskDocumentCacheDelta{
		{
			DocumentID: documentID,
			Head:       &DiskDocumentHead{SchemaVersion: 2, SourceHash: "source-v1", Fingerprint: "head-v1", Payload: json.RawMessage(`{"mtime":1}`)},
			ArtifactUpserts: []DiskDocumentArtifact{
				{Kind: "exports:first", SchemaVersion: 3, SourceHash: "source-v1", Fingerprint: "first-v1", Payload: firstPayload},
				{Kind: "exports:second", SchemaVersion: 3, SourceHash: "source-v1", Fingerprint: "second-v1", Payload: secondPayload},
			},
			IncludeEdges: &DiskDocumentIncludeEdges{
				SchemaVersion: 4,
				SourceHash:    "source-v1",
				Fingerprint:   "edges-v1",
				Edges: []DiskIncludeEdge{{
					TargetDocumentID: "file:///C:/Site/Shared.inc", Offset: 12, Mode: "file", Path: "Shared.inc",
				}},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if got := cache.db.Stats().TxN - statsBefore; got != 2 {
		t.Fatalf("document delta database transactions = %d, want one merge read and one atomic write", got)
	}
	firstPayload[0] = '['
	secondPayload[0] = '['

	keys := []DiskDocumentArtifactKey{
		{DocumentID: documentID, Kind: "exports:first", SchemaVersion: 3},
		{DocumentID: documentID, Kind: "exports:second", SchemaVersion: 3},
	}
	artifacts := cache.ReadDocumentArtifactsAligned(keys)
	if artifacts[0] == nil || string(artifacts[0].Payload) != `{"name":"first"}` || artifacts[1] == nil || string(artifacts[1].Payload) != `{"name":"second"}` {
		t.Fatalf("artifacts = %#v", artifacts)
	}
	artifacts[1].Payload[0] = '['

	if err := cache.WriteDocumentCacheDeltas([]DiskDocumentCacheDelta{{
		DocumentID: documentID,
		ArtifactUpserts: []DiskDocumentArtifact{{
			Kind: "exports:first", SchemaVersion: 3, SourceHash: "source-v2", Fingerprint: "first-v2", Payload: json.RawMessage(`{"name":"updated"}`),
		}},
	}}); err != nil {
		t.Fatal(err)
	}
	artifacts = cache.ReadDocumentArtifactsAligned(keys)
	if artifacts[0] == nil || string(artifacts[0].Payload) != `{"name":"updated"}` || artifacts[1] == nil || string(artifacts[1].Payload) != `{"name":"second"}` {
		t.Fatalf("artifacts after one-kind delta = %#v", artifacts)
	}
	heads := cache.ReadDocumentHeadsAligned([]string{documentID})
	edges := cache.ReadIncludeEdgesAligned([]string{documentID})
	if heads[0] == nil || heads[0].SourceHash != "source-v1" || edges[0] == nil || edges[0].Edges[0].TargetDocumentID != "c:/site/shared.inc" {
		t.Fatalf("unchanged head/edges = %#v / %#v", heads[0], edges[0])
	}
}

func TestDiskAnalysisCacheDocumentDeltaDeletesAreAlignedAndAtomic(t *testing.T) {
	cache := newTestDiskAnalysisCache(t, t.TempDir(), 0, 0, false)
	const documentID = "file:///site/leaf.inc"
	if err := cache.WriteDocumentCacheDeltas([]DiskDocumentCacheDelta{{
		DocumentID: documentID,
		Head:       &DiskDocumentHead{SchemaVersion: 1, SourceHash: "source", Payload: json.RawMessage(`{}`)},
		ArtifactUpserts: []DiskDocumentArtifact{
			{Kind: "keep", SchemaVersion: 1, SourceHash: "source", Payload: json.RawMessage(`{"keep":true}`)},
			{Kind: "remove", SchemaVersion: 1, SourceHash: "source", Payload: json.RawMessage(`{"remove":true}`)},
		},
		IncludeEdges: &DiskDocumentIncludeEdges{SchemaVersion: 1, SourceHash: "source"},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := cache.QueueDocumentCacheDeltas([]DiskDocumentCacheDelta{{
		DocumentID:         documentID,
		DeleteHead:         true,
		ArtifactDeletes:    []DiskDocumentArtifactKey{{Kind: "remove"}},
		DeleteIncludeEdges: true,
	}}); err != nil {
		t.Fatal(err)
	}
	if heads := cache.ReadDocumentHeadsAligned([]string{documentID}); heads[0] != nil {
		t.Fatalf("queued deleted head = %#v, want nil", heads[0])
	}
	artifacts := cache.ReadDocumentArtifactsAligned([]DiskDocumentArtifactKey{
		{DocumentID: documentID, Kind: "keep", SchemaVersion: 1},
		{DocumentID: documentID, Kind: "remove", SchemaVersion: 1},
	})
	if artifacts[0] == nil || artifacts[1] != nil {
		t.Fatalf("queued artifact deletes = %#v", artifacts)
	}
	if edges := cache.ReadIncludeEdgesAligned([]string{documentID}); edges[0] != nil {
		t.Fatalf("queued deleted edges = %#v, want nil", edges[0])
	}
}

func TestNormalizeDiskDocumentIDSupportsWindowsAndUnixPaths(t *testing.T) {
	tests := []struct {
		name  string
		left  string
		right string
		want  string
	}{
		{name: "Windows path", left: `C:\Site\Shared.inc`, right: "file:///c:/site/shared.inc", want: "c:/site/shared.inc"},
		{name: "Unix path", left: "/srv/site/shared.inc", right: "file:///srv/site/shared.inc", want: "/srv/site/shared.inc"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := normalizeDiskDocumentID(test.left); got != test.want {
				t.Fatalf("normalizeDiskDocumentID(%q) = %q, want %q", test.left, got, test.want)
			}
			if got := normalizeDiskDocumentID(test.right); got != test.want {
				t.Fatalf("normalizeDiskDocumentID(%q) = %q, want %q", test.right, got, test.want)
			}
			if got := normalizeDiskDocumentID(test.want); got != test.want {
				t.Fatalf("normalized document ID %q changed to %q", test.want, got)
			}
		})
	}
}

func TestDiskAnalysisCacheCorruptDocumentArtifactIsIndependentMiss(t *testing.T) {
	cache := newTestDiskAnalysisCache(t, t.TempDir(), 0, 0, false)
	const documentID = "file:///site/leaf.inc"
	if err := cache.WriteDocumentCacheDeltas([]DiskDocumentCacheDelta{{
		DocumentID: documentID,
		ArtifactUpserts: []DiskDocumentArtifact{
			{Kind: "broken", SchemaVersion: 1, SourceHash: "source", Payload: json.RawMessage(`{"value":1}`)},
			{Kind: "valid", SchemaVersion: 1, SourceHash: "source", Payload: json.RawMessage(`{"value":2}`)},
		},
	}}); err != nil {
		t.Fatal(err)
	}
	cache.dbMu.RLock()
	err := cache.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(diskDocumentArtifactsBucket).Put(
			diskDocumentArtifactKey("/site/leaf.inc", "broken"), []byte(`{"storageVersion":1,"payload":"corrupt"}`),
		)
	})
	cache.dbMu.RUnlock()
	if err != nil {
		t.Fatal(err)
	}

	artifacts := cache.ReadDocumentArtifactsAligned([]DiskDocumentArtifactKey{
		{DocumentID: documentID, Kind: "broken", SchemaVersion: 1},
		{DocumentID: documentID, Kind: "valid", SchemaVersion: 1},
		{DocumentID: documentID, Kind: "valid", SchemaVersion: 99},
	})
	if artifacts[0] != nil || artifacts[1] == nil || artifacts[2] != nil {
		t.Fatalf("artifact corruption/schema isolation = %#v", artifacts)
	}
	if err := cache.Flush(); err != nil {
		t.Fatal(err)
	}
	if values := cache.readDocumentValuesAligned(diskDocumentArtifactsBucket, [][]byte{diskDocumentArtifactKey("/site/leaf.inc", "broken")}); values[0] != nil {
		t.Fatalf("corrupt artifact was not removed: %q", values[0])
	}
}

func TestDiskAnalysisCacheOwnsWorkspaceReferenceBatchPayload(t *testing.T) {
	cache := newTestDiskAnalysisCache(t, t.TempDir(), 0, 0, false)
	payload := json.RawMessage(`{"symbols":[{"name":"SharedTitle","count":2}]}`)
	if err := cache.WriteWorkspaceReferenceBatch(DiskWorkspaceReferenceBatchCacheEntry{
		SettingsKey: "references",
		Payload:     payload,
	}); err != nil {
		t.Fatal(err)
	}
	payload[0] = '['

	first, ok := cache.ReadWorkspaceReferenceBatch("references")
	if !ok {
		t.Fatal("ReadWorkspaceReferenceBatch() missed pending write")
	}
	if got := string(first.Payload); got != `{"symbols":[{"name":"SharedTitle","count":2}]}` {
		t.Fatalf("pending payload = %q, want original value", got)
	}
	first.Payload[0] = '['

	second, ok := cache.ReadWorkspaceReferenceBatch("references")
	if !ok {
		t.Fatal("second ReadWorkspaceReferenceBatch() missed pending write")
	}
	if got := string(second.Payload); got != `{"symbols":[{"name":"SharedTitle","count":2}]}` {
		t.Fatalf("second pending payload = %q, want original value", got)
	}
	if err := cache.Flush(); err != nil {
		t.Fatal(err)
	}
	second.Payload[0] = '['
	stored, ok := cache.ReadWorkspaceReferenceBatch("references")
	if !ok || string(stored.Payload) != `{"symbols":[{"name":"SharedTitle","count":2}]}` {
		t.Fatalf("stored reference batch = %#v, %v; want isolated original payload", stored, ok)
	}
}

func TestDiskAnalysisCacheRestoresWorkspaceReferenceBatchAfterRestart(t *testing.T) {
	directory := t.TempDir()
	payload := json.RawMessage(`{"generation":11,"documents":8,"references":{"sharedTitle":3}}`)
	cache := newTestDiskAnalysisCache(t, directory, 0, 0, false)
	if err := cache.WriteWorkspaceReferenceBatch(DiskWorkspaceReferenceBatchCacheEntry{
		SettingsKey: "reference-settings",
		Payload:     payload,
	}); err != nil {
		t.Fatal(err)
	}
	if err := cache.Close(); err != nil {
		t.Fatal(err)
	}

	reopened := newTestDiskAnalysisCache(t, directory, 0, 0, false)
	entry, ok := reopened.ReadWorkspaceReferenceBatch("reference-settings")
	if !ok || !bytes.Equal(entry.Payload, payload) {
		t.Fatalf("ReadWorkspaceReferenceBatch() after restart = %#v, %v; want %s", entry, ok, payload)
	}
	if _, ok := reopened.ReadWorkspaceReferenceBatch("different-settings"); ok {
		t.Fatal("ReadWorkspaceReferenceBatch() restored a batch for different settings")
	}
}

func TestDiskAnalysisCacheReferenceSchemaResetKeepsUnrelatedData(t *testing.T) {
	cache := newTestDiskAnalysisCache(t, t.TempDir(), 0, 0, false)
	graphPayload := json.RawMessage(`{"nodes":[{"id":"keep"}]}`)
	if err := cache.WriteGraphPayload(DiskGraphPayloadCacheEntry{SettingsKey: "graph", Payload: graphPayload}); err != nil {
		t.Fatal(err)
	}
	if err := cache.Flush(); err != nil {
		t.Fatal(err)
	}
	if reset, err := cache.EnsureReferenceSchema(1); err != nil || !reset {
		t.Fatalf("EnsureReferenceSchema(1) = %v, %v; want true, nil", reset, err)
	}
	if err := cache.ReplaceReferenceDocument(
		[]byte("document"),
		[]byte("manifest-v1"),
		nil,
		map[string][]byte{"symbol/document": []byte("posting-v1")},
	); err != nil {
		t.Fatal(err)
	}
	if err := cache.WriteReferenceQuery([]byte("query"), []byte("result-v1")); err != nil {
		t.Fatal(err)
	}
	if reset, err := cache.EnsureReferenceSchema(1); err != nil || reset {
		t.Fatalf("EnsureReferenceSchema(1) second call = %v, %v; want false, nil", reset, err)
	}
	if reset, err := cache.EnsureReferenceSchema(2); err != nil || !reset {
		t.Fatalf("EnsureReferenceSchema(2) = %v, %v; want true, nil", reset, err)
	}
	for _, bucket := range []DiskReferenceBucket{DiskReferenceDocuments, DiskReferencePostings, DiskReferenceQueries} {
		if values := cache.ReadReferenceValues(bucket, [][]byte{[]byte("document"), []byte("symbol/document"), []byte("query")}); len(values) != 0 {
			t.Fatalf("reference bucket %v survived schema reset: %#v", bucket, values)
		}
	}
	if graph, ok := cache.ReadGraphPayload("graph"); !ok || !bytes.Equal(graph.Payload, graphPayload) {
		t.Fatalf("unrelated graph payload = %#v, %v; want preserved", graph, ok)
	}
}

func TestDiskAnalysisCacheMalformedReferenceSchemaIsGracefullyReset(t *testing.T) {
	cache := newTestDiskAnalysisCache(t, t.TempDir(), 0, 0, false)
	if reset, err := cache.EnsureReferenceSchema(1); err != nil || !reset {
		t.Fatalf("EnsureReferenceSchema(1) = %v, %v; want true, nil", reset, err)
	}
	if err := cache.ReplaceReferenceDocument([]byte("old"), []byte("manifest"), nil, map[string][]byte{"old/posting": []byte("posting")}); err != nil {
		t.Fatal(err)
	}
	cache.dbMu.RLock()
	err := cache.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(diskReferenceMetaBucket).Put(diskReferenceSchemaKey, []byte{0xff})
	})
	cache.dbMu.RUnlock()
	if err != nil {
		t.Fatal(err)
	}
	if reset, err := cache.EnsureReferenceSchema(1); err != nil || !reset {
		t.Fatalf("EnsureReferenceSchema(1) with malformed schema = %v, %v; want true, nil", reset, err)
	}
	if values := cache.ReadReferenceValues(DiskReferenceDocuments, [][]byte{[]byte("old")}); len(values) != 0 {
		t.Fatalf("documents after malformed schema reset = %#v, want empty", values)
	}
}

func TestDiskAnalysisCacheReferenceBucketConflictDoesNotRecreateDatabase(t *testing.T) {
	directory := t.TempDir()
	cache := newTestDiskAnalysisCache(t, directory, 0, 0, false)
	graphPayload := json.RawMessage(`{"nodes":[{"id":"keep"}]}`)
	if err := cache.WriteGraphPayload(DiskGraphPayloadCacheEntry{SettingsKey: "graph", Payload: graphPayload}); err != nil {
		t.Fatal(err)
	}
	if err := cache.Close(); err != nil {
		t.Fatal(err)
	}

	db, err := bolt.Open(cache.databasePath, 0o600, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(tx *bolt.Tx) error {
		if err := tx.DeleteBucket(diskReferenceMetaBucket); err != nil {
			return err
		}
		return tx.Cursor().Bucket().Put(diskReferenceMetaBucket, []byte("legacy-reference-cache"))
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	reopened := newTestDiskAnalysisCache(t, directory, 0, 0, false)
	if reset, err := reopened.EnsureReferenceSchema(1); err != nil || !reset {
		t.Fatalf("EnsureReferenceSchema(1) = %v, %v; want true, nil", reset, err)
	}
	if graph, ok := reopened.ReadGraphPayload("graph"); !ok || !bytes.Equal(graph.Payload, graphPayload) {
		t.Fatalf("unrelated graph payload = %#v, %v; want preserved", graph, ok)
	}
}

func TestDiskAnalysisCacheReferenceValuesOwnInputAndOutputBytes(t *testing.T) {
	cache := newTestDiskAnalysisCache(t, t.TempDir(), 0, 0, false)
	if _, err := cache.EnsureReferenceSchema(1); err != nil {
		t.Fatal(err)
	}
	documentKey := []byte("document")
	documentValue := []byte("manifest")
	postingValue := []byte("posting")
	if err := cache.ReplaceReferenceDocument(documentKey, documentValue, nil, map[string][]byte{"symbol/document": postingValue}); err != nil {
		t.Fatal(err)
	}
	queryKey := []byte("query")
	queryValue := []byte("result")
	if err := cache.WriteReferenceQuery(queryKey, queryValue); err != nil {
		t.Fatal(err)
	}
	documentKey[0], documentValue[0], postingValue[0], queryKey[0], queryValue[0] = 'X', 'X', 'X', 'X', 'X'

	documents := cache.ReadReferenceValues(DiskReferenceDocuments, [][]byte{[]byte("document")})
	postings := cache.ReadReferenceValues(DiskReferencePostings, [][]byte{[]byte("symbol/document")})
	queries := cache.ReadReferenceValues(DiskReferenceQueries, [][]byte{[]byte("query")})
	if string(documents["document"]) != "manifest" || string(postings["symbol/document"]) != "posting" || string(queries["query"]) != "result" {
		t.Fatalf("stored reference values changed through caller input: documents=%q postings=%q queries=%q", documents["document"], postings["symbol/document"], queries["query"])
	}
	documents["document"][0] = 'X'
	postings["symbol/document"][0] = 'X'
	queries["query"][0] = 'X'
	if got := cache.ReadReferenceValues(DiskReferenceDocuments, [][]byte{[]byte("document")}); string(got["document"]) != "manifest" {
		t.Fatalf("document changed through returned bytes: %q", got["document"])
	}
	if got := cache.ReadReferenceValues(DiskReferencePostings, [][]byte{[]byte("symbol/document")}); string(got["symbol/document"]) != "posting" {
		t.Fatalf("posting changed through returned bytes: %q", got["symbol/document"])
	}
	if got := cache.ReadReferenceValues(DiskReferenceQueries, [][]byte{[]byte("query")}); string(got["query"]) != "result" {
		t.Fatalf("query changed through returned bytes: %q", got["query"])
	}
}

func TestDiskAnalysisCacheReferenceValuesSurviveRestartAndClearWithoutSchemaReset(t *testing.T) {
	directory := t.TempDir()
	cache := newTestDiskAnalysisCache(t, directory, 0, 0, false)
	if _, err := cache.EnsureReferenceSchema(7); err != nil {
		t.Fatal(err)
	}
	if err := cache.ReplaceReferenceDocument([]byte("document"), []byte("manifest"), nil, map[string][]byte{"posting": []byte("value")}); err != nil {
		t.Fatal(err)
	}
	if err := cache.WriteReferenceQuery([]byte("query"), []byte("result")); err != nil {
		t.Fatal(err)
	}
	if err := cache.Close(); err != nil {
		t.Fatal(err)
	}

	reopened := newTestDiskAnalysisCache(t, directory, 0, 0, false)
	if reset, err := reopened.EnsureReferenceSchema(7); err != nil || reset {
		t.Fatalf("EnsureReferenceSchema(7) after restart = %v, %v; want false, nil", reset, err)
	}
	if got := reopened.ReadReferenceValues(DiskReferenceDocuments, [][]byte{[]byte("document")}); string(got["document"]) != "manifest" {
		t.Fatalf("document after restart = %q, want manifest", got["document"])
	}
	if err := reopened.ClearReferenceData(); err != nil {
		t.Fatal(err)
	}
	if reset, err := reopened.EnsureReferenceSchema(7); err != nil || reset {
		t.Fatalf("EnsureReferenceSchema(7) after clear = %v, %v; want false, nil", reset, err)
	}
	for _, bucket := range []DiskReferenceBucket{DiskReferenceDocuments, DiskReferencePostings, DiskReferenceQueries} {
		if values := reopened.ReadReferenceValues(bucket, [][]byte{[]byte("document"), []byte("posting"), []byte("query")}); len(values) != 0 {
			t.Fatalf("reference bucket %v after clear = %#v, want empty", bucket, values)
		}
	}
}

func TestDiskAnalysisCacheReplaceReferenceDocumentIsAtomic(t *testing.T) {
	cache := newTestDiskAnalysisCache(t, t.TempDir(), 0, 0, false)
	if _, err := cache.EnsureReferenceSchema(1); err != nil {
		t.Fatal(err)
	}
	if err := cache.ReplaceReferenceDocument([]byte("document"), []byte("v1"), nil, map[string][]byte{"old": []byte("old-value")}); err != nil {
		t.Fatal(err)
	}
	if err := cache.ReplaceReferenceDocument([]byte("document"), []byte("v2"), [][]byte{[]byte("old")}, map[string][]byte{"new": []byte("new-value")}); err != nil {
		t.Fatal(err)
	}
	if got := cache.ReadReferenceValues(DiskReferenceDocuments, [][]byte{[]byte("document")}); string(got["document"]) != "v2" {
		t.Fatalf("document manifest = %q, want v2", got["document"])
	}
	postings := cache.ReadReferenceValues(DiskReferencePostings, [][]byte{[]byte("old"), []byte("new")})
	if _, found := postings["old"]; found || string(postings["new"]) != "new-value" {
		t.Fatalf("postings after replacement = %#v", postings)
	}
}

func TestDiskAnalysisCacheReferenceValuesAlignedPreserveOrderAndOwnership(t *testing.T) {
	cache := newTestDiskAnalysisCache(t, t.TempDir(), 0, 0, false)
	if _, err := cache.EnsureReferenceSchema(1); err != nil {
		t.Fatal(err)
	}
	if err := cache.WriteReferenceQueries([]DiskReferenceQueryWrite{
		{Key: []byte("first"), Value: []byte("one")},
		{Key: []byte("second"), Value: []byte("two")},
	}); err != nil {
		t.Fatal(err)
	}
	keys := [][]byte{[]byte("second"), []byte("missing"), []byte("first"), []byte("second")}
	values := cache.ReadReferenceValuesAligned(DiskReferenceQueries, keys)
	if len(values) != len(keys) || string(values[0]) != "two" || values[1] != nil || string(values[2]) != "one" || string(values[3]) != "two" {
		t.Fatalf("ReadReferenceValuesAligned() = %q", values)
	}
	values[0][0] = 'X'
	if got := cache.ReadReferenceValuesAligned(DiskReferenceQueries, [][]byte{[]byte("second")}); string(got[0]) != "two" {
		t.Fatalf("stored value changed through aligned result: %q", got[0])
	}
	if string(keys[0]) != "second" {
		t.Fatalf("input key changed: %q", keys[0])
	}
}

func TestDiskAnalysisCacheReferenceValuesPresentAlignedHonorsQueuedWritesAndExpiry(t *testing.T) {
	cache := newTestDiskAnalysisCache(t, t.TempDir(), time.Minute, 0, false)
	if _, err := cache.EnsureReferenceSchema(1); err != nil {
		t.Fatal(err)
	}
	keys := [][]byte{[]byte("present"), []byte("missing")}
	if err := cache.QueueReferenceDocumentReplacements([]DiskReferenceDocumentReplacement{{
		DocumentKey: []byte("document"), DocumentValue: []byte("manifest"),
		NewPostings: map[string][]byte{"present": []byte("value")}, CurrentPostingKeys: keys[:1],
	}}); err != nil {
		t.Fatal(err)
	}
	if got := cache.ReferenceValuesPresentAligned(DiskReferencePostings, keys); !reflect.DeepEqual(got, []bool{true, false}) {
		t.Fatalf("queued posting presence = %v, want [true false]", got)
	}
	if err := cache.Flush(); err != nil {
		t.Fatal(err)
	}
	if got := cache.ReferenceValuesPresentAligned(DiskReferencePostings, keys); !reflect.DeepEqual(got, []bool{true, false}) {
		t.Fatalf("stored posting presence = %v, want [true false]", got)
	}
	cache.dbMu.Lock()
	err := cache.db.Update(func(tx *bolt.Tx) error {
		meta := tx.Bucket(diskCacheMetaBucket)
		key := diskCacheEntryMetaKey(diskCacheBucketID(diskReferencePostingsBucket), []byte("present"))
		value := append([]byte(nil), meta.Get(key)...)
		binary.BigEndian.PutUint64(value[:8], uint64(time.Now().Add(-2*time.Minute).UnixMilli()))
		return meta.Put(key, value)
	})
	cache.dbMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if got := cache.ReferenceValuesPresentAligned(DiskReferencePostings, keys); !reflect.DeepEqual(got, []bool{false, false}) {
		t.Fatalf("expired posting presence = %v, want [false false]", got)
	}
}

func TestDiskAnalysisCacheBulkReferenceQueriesUseBatchedDatabaseWrites(t *testing.T) {
	cache := newTestDiskAnalysisCache(t, t.TempDir(), 0, 0, false)
	if _, err := cache.EnsureReferenceSchema(1); err != nil {
		t.Fatal(err)
	}
	writes := make([]DiskReferenceQueryWrite, 100)
	keys := make([][]byte, len(writes))
	for index := range writes {
		key := []byte(fmt.Sprintf("query-%03d", index))
		keys[index] = key
		writes[index] = DiskReferenceQueryWrite{Key: key, Value: []byte(fmt.Sprintf("result-%03d", index))}
	}
	statsBefore := cache.db.Stats()
	writesBefore := statsBefore.TxStats.GetWrite()
	if err := cache.WriteReferenceQueries(writes); err != nil {
		t.Fatal(err)
	}
	statsAfter := cache.db.Stats()
	writesAfter := statsAfter.TxStats.GetWrite()
	if got := writesAfter - writesBefore; got >= int64(len(writes)) {
		t.Fatalf("bulk reference database writes = %d, want fewer than %d", got, len(writes))
	}
	values := cache.ReadReferenceValuesAligned(DiskReferenceQueries, keys)
	for index, value := range values {
		if want := fmt.Sprintf("result-%03d", index); string(value) != want {
			t.Fatalf("value %d = %q, want %q", index, value, want)
		}
	}
}

func TestDiskAnalysisCacheQueuedReferenceReplacementCoalescesPostings(t *testing.T) {
	cache := newTestDiskAnalysisCache(t, t.TempDir(), 0, 0, false)
	if _, err := cache.EnsureReferenceSchema(1); err != nil {
		t.Fatal(err)
	}
	firstValue := []byte("first-value")
	if err := cache.QueueReferenceDocumentReplacements([]DiskReferenceDocumentReplacement{{
		DocumentKey: []byte("document"), DocumentValue: []byte("v1"), NewPostings: map[string][]byte{"first": firstValue},
	}}); err != nil {
		t.Fatal(err)
	}
	firstValue[0] = 'X'
	if err := cache.QueueReferenceDocumentReplacements([]DiskReferenceDocumentReplacement{{
		DocumentKey: []byte("document"), DocumentValue: []byte("v2"), NewPostings: map[string][]byte{"second": []byte("second-value")},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := cache.Flush(); err != nil {
		t.Fatal(err)
	}
	document := cache.ReadReferenceValuesAligned(DiskReferenceDocuments, [][]byte{[]byte("document")})
	postings := cache.ReadReferenceValuesAligned(DiskReferencePostings, [][]byte{[]byte("first"), []byte("second")})
	if string(document[0]) != "v2" || postings[0] != nil || string(postings[1]) != "second-value" {
		t.Fatalf("coalesced replacement = document %q, postings %q", document[0], postings)
	}
}

func TestDiskAnalysisCacheQueuedReferenceDeltaRetainsUnchangedPostings(t *testing.T) {
	cache := newTestDiskAnalysisCache(t, t.TempDir(), 0, 0, false)
	if _, err := cache.EnsureReferenceSchema(1); err != nil {
		t.Fatal(err)
	}
	if err := cache.QueueReferenceDocumentReplacements([]DiskReferenceDocumentReplacement{{
		DocumentKey: []byte("document"), DocumentValue: []byte("v1"),
		NewPostings:        map[string][]byte{"first": []byte("first-v1"), "second": []byte("second-v1")},
		CurrentPostingKeys: [][]byte{[]byte("first"), []byte("second")},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := cache.QueueReferenceDocumentReplacements([]DiskReferenceDocumentReplacement{{
		DocumentKey: []byte("document"), DocumentValue: []byte("v2"),
		OldPostingKeys: [][]byte{[]byte("second")}, NewPostings: map[string][]byte{"third": []byte("third-v2")},
		CurrentPostingKeys: [][]byte{[]byte("first"), []byte("third")},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := cache.Flush(); err != nil {
		t.Fatal(err)
	}
	document := cache.ReadReferenceValuesAligned(DiskReferenceDocuments, [][]byte{[]byte("document")})
	postings := cache.ReadReferenceValuesAligned(DiskReferencePostings, [][]byte{[]byte("first"), []byte("second"), []byte("third")})
	if string(document[0]) != "v2" || string(postings[0]) != "first-v1" || postings[1] != nil || string(postings[2]) != "third-v2" {
		t.Fatalf("coalesced delta = document %q, postings %q", document[0], postings)
	}
}

func TestDiskAnalysisCacheReferenceDeltaDoesNotRewriteUnchangedPosting(t *testing.T) {
	cache := newTestDiskAnalysisCache(t, t.TempDir(), 0, 0, false)
	if _, err := cache.EnsureReferenceSchema(1); err != nil {
		t.Fatal(err)
	}
	if err := cache.ReplaceReferenceDocument(
		[]byte("document"), []byte("v1"), nil, map[string][]byte{"first": []byte("first-v1")},
	); err != nil {
		t.Fatal(err)
	}
	readWrittenAt := func() uint64 {
		t.Helper()
		var writtenAt uint64
		var metadataLength int
		cache.dbMu.RLock()
		err := cache.db.View(func(tx *bolt.Tx) error {
			meta := tx.Bucket(diskCacheMetaBucket)
			encoded := meta.Get(diskCacheEntryMetaKey(diskCacheBucketID(diskReferencePostingsBucket), []byte("first")))
			metadataLength = len(encoded)
			if metadataLength == 16 {
				writtenAt = binary.BigEndian.Uint64(encoded[:8])
			}
			return nil
		})
		cache.dbMu.RUnlock()
		if err != nil {
			t.Fatal(err)
		}
		if metadataLength != 16 {
			t.Fatalf("posting metadata length = %d, want 16", metadataLength)
		}
		return writtenAt
	}
	firstWrittenAt := readWrittenAt()
	time.Sleep(2 * time.Millisecond)
	if err := cache.ReplaceReferenceDocuments([]DiskReferenceDocumentReplacement{{
		DocumentKey: []byte("document"), DocumentValue: []byte("v2"), CurrentPostingKeys: [][]byte{[]byte("first")},
	}}); err != nil {
		t.Fatal(err)
	}
	if secondWrittenAt := readWrittenAt(); secondWrittenAt != firstWrittenAt {
		t.Fatalf("unchanged posting writtenAt = %d, want unchanged %d", secondWrittenAt, firstWrittenAt)
	}
	if values := cache.ReadReferenceValuesAligned(DiskReferencePostings, [][]byte{[]byte("first")}); string(values[0]) != "first-v1" {
		t.Fatalf("unchanged posting = %q, want first-v1", values[0])
	}
}

func TestDiskAnalysisCacheReferenceDeltaPreservesTombstoneOrdering(t *testing.T) {
	for _, test := range []struct {
		name             string
		replacements     []DiskReferenceDocumentReplacement
		wantDocument     string
		wantPosting      string
		wantDocumentGone bool
	}{
		{
			name: "tombstone wins",
			replacements: []DiskReferenceDocumentReplacement{
				{DocumentKey: []byte("document"), DocumentValue: []byte("v1"), NewPostings: map[string][]byte{"first": []byte("first-v1")}, CurrentPostingKeys: [][]byte{[]byte("first")}},
				{DocumentKey: []byte("document"), OldPostingKeys: [][]byte{[]byte("first")}, CurrentPostingKeys: make([][]byte, 0)},
			},
			wantDocumentGone: true,
		},
		{
			name: "replacement after tombstone wins",
			replacements: []DiskReferenceDocumentReplacement{
				{DocumentKey: []byte("document"), DocumentValue: []byte("v1"), NewPostings: map[string][]byte{"first": []byte("first-v1")}, CurrentPostingKeys: [][]byte{[]byte("first")}},
				{DocumentKey: []byte("document"), OldPostingKeys: [][]byte{[]byte("first")}, CurrentPostingKeys: make([][]byte, 0)},
				{DocumentKey: []byte("document"), DocumentValue: []byte("v2"), NewPostings: map[string][]byte{"first": []byte("first-v2")}, CurrentPostingKeys: [][]byte{[]byte("first")}},
			},
			wantDocument: "v2",
			wantPosting:  "first-v2",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			cache := newTestDiskAnalysisCache(t, t.TempDir(), 0, 0, false)
			if _, err := cache.EnsureReferenceSchema(1); err != nil {
				t.Fatal(err)
			}
			for _, replacement := range test.replacements {
				if err := cache.QueueReferenceDocumentReplacements([]DiskReferenceDocumentReplacement{replacement}); err != nil {
					t.Fatal(err)
				}
			}
			if err := cache.Flush(); err != nil {
				t.Fatal(err)
			}
			document := cache.ReadReferenceValuesAligned(DiskReferenceDocuments, [][]byte{[]byte("document")})[0]
			posting := cache.ReadReferenceValuesAligned(DiskReferencePostings, [][]byte{[]byte("first")})[0]
			if test.wantDocumentGone {
				if document != nil || posting != nil {
					t.Fatalf("tombstoned values = document %q, posting %q", document, posting)
				}
				return
			}
			if string(document) != test.wantDocument || string(posting) != test.wantPosting {
				t.Fatalf("ordered values = document %q, posting %q; want %q, %q", document, posting, test.wantDocument, test.wantPosting)
			}
		})
	}
}

func TestDiskAnalysisCacheReferenceEntriesUseLogicalSizeAndTTL(t *testing.T) {
	cache := newTestDiskAnalysisCache(t, t.TempDir(), 0, 0, false)
	if _, err := cache.EnsureReferenceSchema(1); err != nil {
		t.Fatal(err)
	}
	if err := cache.ReplaceReferenceDocument(
		[]byte("document"), []byte("manifest"), nil, map[string][]byte{"posting": []byte("posting-value")},
	); err != nil {
		t.Fatal(err)
	}
	if err := cache.WriteReferenceQuery([]byte("query"), []byte("result")); err != nil {
		t.Fatal(err)
	}
	wantSize := int64(len("manifest") + len("posting-value") + len("result"))
	if size, err := cache.currentLogicalSize(); err != nil || size != wantSize {
		t.Fatalf("logical size = %d, %v; want %d, nil", size, err, wantSize)
	}
	cache.ttl = time.Nanosecond
	time.Sleep(time.Millisecond)
	for bucket, key := range map[DiskReferenceBucket][]byte{
		DiskReferenceDocuments: []byte("document"),
		DiskReferencePostings:  []byte("posting"),
		DiskReferenceQueries:   []byte("query"),
	} {
		if got := cache.ReadReferenceValuesAligned(bucket, [][]byte{key}); got[0] != nil {
			t.Fatalf("expired reference value returned before sweep in bucket %d = %q", bucket, got[0])
		}
	}
	if err := cache.SweepBatch(1); err != nil {
		t.Fatal(err)
	}
	for bucket, key := range map[DiskReferenceBucket][]byte{
		DiskReferenceDocuments: []byte("document"),
		DiskReferencePostings:  []byte("posting"),
		DiskReferenceQueries:   []byte("query"),
	} {
		if got := cache.ReadReferenceValuesAligned(bucket, [][]byte{key}); got[0] != nil {
			t.Fatalf("expired reference value in bucket %d = %q, want nil", bucket, got[0])
		}
	}
	if size, err := cache.currentLogicalSize(); err != nil || size != 0 {
		t.Fatalf("logical size after sweep = %d, %v; want 0, nil", size, err)
	}
}

func TestDiskAnalysisCacheMatchingReferenceSchemaDoesNotWrite(t *testing.T) {
	cache := newTestDiskAnalysisCache(t, t.TempDir(), 0, 0, false)
	if _, err := cache.EnsureReferenceSchema(1); err != nil {
		t.Fatal(err)
	}
	statsBefore := cache.db.Stats()
	writesBefore := statsBefore.TxStats.GetWrite()
	if reset, err := cache.EnsureReferenceSchema(1); err != nil || reset {
		t.Fatalf("EnsureReferenceSchema(1) = %v, %v; want false, nil", reset, err)
	}
	statsAfter := cache.db.Stats()
	if writesAfter := statsAfter.TxStats.GetWrite(); writesAfter != writesBefore {
		t.Fatalf("matching reference schema writes = %d, want %d", writesAfter, writesBefore)
	}
}

func TestDiskAnalysisCacheOldReferenceStorageIsGracefullyReset(t *testing.T) {
	cache := newTestDiskAnalysisCache(t, t.TempDir(), 0, 0, false)
	if _, err := cache.EnsureReferenceSchema(1); err != nil {
		t.Fatal(err)
	}
	if err := cache.WriteReferenceQuery([]byte("old"), []byte("value")); err != nil {
		t.Fatal(err)
	}
	cache.dbMu.RLock()
	err := cache.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(diskReferenceMetaBucket).Put(diskReferenceSchemaKey, []byte{0, 0, 0, 1})
	})
	cache.dbMu.RUnlock()
	if err != nil {
		t.Fatal(err)
	}
	if reset, err := cache.EnsureReferenceSchema(1); err != nil || !reset {
		t.Fatalf("EnsureReferenceSchema(1) with old storage = %v, %v; want true, nil", reset, err)
	}
	if got := cache.ReadReferenceValuesAligned(DiskReferenceQueries, [][]byte{[]byte("old")}); got[0] != nil {
		t.Fatalf("old reference value survived reset: %q", got[0])
	}
	if size, err := cache.currentLogicalSize(); err != nil || size != 0 {
		t.Fatalf("logical size after old storage reset = %d, %v; want 0, nil", size, err)
	}
}

func TestDiskAnalysisCacheOwnsWorkspaceLegacyUndefinedGlobalsPayload(t *testing.T) {
	cache := newTestDiskAnalysisCache(t, t.TempDir(), 0, 0, false)
	payload := json.RawMessage(`{"documents":["default.asp"],"globals":[{"name":"LegacyValue"}]}`)
	if err := cache.WriteWorkspaceLegacyUndefinedGlobals(DiskWorkspaceLegacyUndefinedGlobalsCacheEntry{
		SettingsKey: "legacy-catalog",
		Payload:     payload,
	}); err != nil {
		t.Fatal(err)
	}
	payload[0] = '['

	first, ok := cache.ReadWorkspaceLegacyUndefinedGlobals("legacy-catalog")
	if !ok {
		t.Fatal("ReadWorkspaceLegacyUndefinedGlobals() missed pending write")
	}
	const original = `{"documents":["default.asp"],"globals":[{"name":"LegacyValue"}]}`
	if got := string(first.Payload); got != original {
		t.Fatalf("pending payload = %q, want original value", got)
	}
	first.Payload[0] = '['

	second, ok := cache.ReadWorkspaceLegacyUndefinedGlobals("legacy-catalog")
	if !ok || string(second.Payload) != original {
		t.Fatalf("second pending payload = %#v, %v; want isolated original payload", second, ok)
	}
	if err := cache.Flush(); err != nil {
		t.Fatal(err)
	}
	second.Payload[0] = '['
	stored, ok := cache.ReadWorkspaceLegacyUndefinedGlobals("legacy-catalog")
	if !ok || string(stored.Payload) != original {
		t.Fatalf("stored legacy undefined-global catalog = %#v, %v; want isolated original payload", stored, ok)
	}
}

func TestDiskAnalysisCacheKeepsLegacyUndefinedGlobalsSeparateFromReferenceBatch(t *testing.T) {
	cache := newTestDiskAnalysisCache(t, t.TempDir(), 0, 0, false)
	const settingsKey = "shared-settings-key"
	references := json.RawMessage(`{"references":3}`)
	legacyGlobals := json.RawMessage(`{"legacyGlobals":2}`)
	if err := cache.WriteWorkspaceReferenceBatch(DiskWorkspaceReferenceBatchCacheEntry{SettingsKey: settingsKey, Payload: references}); err != nil {
		t.Fatal(err)
	}
	if err := cache.WriteWorkspaceLegacyUndefinedGlobals(DiskWorkspaceLegacyUndefinedGlobalsCacheEntry{SettingsKey: settingsKey, Payload: legacyGlobals}); err != nil {
		t.Fatal(err)
	}
	if err := cache.Flush(); err != nil {
		t.Fatal(err)
	}

	referenceEntry, referenceOK := cache.ReadWorkspaceReferenceBatch(settingsKey)
	legacyEntry, legacyOK := cache.ReadWorkspaceLegacyUndefinedGlobals(settingsKey)
	if !referenceOK || !bytes.Equal(referenceEntry.Payload, references) {
		t.Fatalf("reference batch = %#v, %v; want %s", referenceEntry, referenceOK, references)
	}
	if !legacyOK || !bytes.Equal(legacyEntry.Payload, legacyGlobals) {
		t.Fatalf("legacy undefined-global catalog = %#v, %v; want %s", legacyEntry, legacyOK, legacyGlobals)
	}
	assertBucketKeyCount(t, cache, diskCacheWorkspaceBucket, 2)
}

func TestDiskAnalysisCacheRestoresAndInvalidatesWorkspaceLegacyUndefinedGlobals(t *testing.T) {
	directory := t.TempDir()
	payload := json.RawMessage(`{"generation":17,"globals":["LegacyValue"]}`)
	cache := newTestDiskAnalysisCache(t, directory, 0, 0, false)
	if err := cache.WriteWorkspaceLegacyUndefinedGlobals(DiskWorkspaceLegacyUndefinedGlobalsCacheEntry{
		SettingsKey: "legacy-settings-v1",
		Payload:     payload,
	}); err != nil {
		t.Fatal(err)
	}
	if err := cache.Close(); err != nil {
		t.Fatal(err)
	}

	reopened := newTestDiskAnalysisCache(t, directory, 0, 0, false)
	entry, ok := reopened.ReadWorkspaceLegacyUndefinedGlobals("legacy-settings-v1")
	if !ok || !bytes.Equal(entry.Payload, payload) {
		t.Fatalf("ReadWorkspaceLegacyUndefinedGlobals() after restart = %#v, %v; want %s", entry, ok, payload)
	}
	if _, ok := reopened.ReadWorkspaceLegacyUndefinedGlobals("legacy-settings-v2"); ok {
		t.Fatal("legacy undefined-global catalog survived settings-key invalidation")
	}
	reopened.ttl = time.Nanosecond
	time.Sleep(time.Millisecond)
	if _, ok := reopened.ReadWorkspaceLegacyUndefinedGlobals("legacy-settings-v1"); ok {
		t.Fatal("legacy undefined-global catalog survived TTL invalidation")
	}
}

func TestDiskAnalysisCacheOwnsWorkspaceIncludeGraphEntries(t *testing.T) {
	cache := newTestDiskAnalysisCache(t, t.TempDir(), 0, 0, false)
	entries := []IncludeGraphEntry{{
		FileName:        "/site/default.asp",
		TargetFileNames: []string{"/site/shared.inc"},
		References:      []IncludeReference{{Path: "shared.inc", Mode: "file"}},
	}}
	if err := cache.WriteWorkspaceIncludeGraph(DiskWorkspaceIncludeGraphCacheEntry{
		SettingsKey: "includes",
		Entries:     entries,
	}); err != nil {
		t.Fatal(err)
	}
	entries[0].TargetFileNames[0] = "/site/caller-mutated.inc"
	entries[0].References[0].Path = "caller-mutated.inc"

	first, ok := cache.ReadWorkspaceIncludeGraph("includes")
	if !ok || len(first.Entries) != 1 {
		t.Fatalf("ReadWorkspaceIncludeGraph() = %#v, %v", first, ok)
	}
	first.Entries[0].TargetFileNames[0] = "/site/reader-mutated.inc"
	first.Entries[0].References[0].Path = "reader-mutated.inc"

	second, ok := cache.ReadWorkspaceIncludeGraph("includes")
	if !ok || len(second.Entries) != 1 {
		t.Fatalf("second ReadWorkspaceIncludeGraph() = %#v, %v", second, ok)
	}
	if got := second.Entries[0].TargetFileNames[0]; got != "/site/shared.inc" {
		t.Fatalf("target file name = %q, want original value", got)
	}
	if got := second.Entries[0].References[0]; got != (IncludeReference{Path: "shared.inc", Mode: "file"}) {
		t.Fatalf("include reference = %#v, want original value", got)
	}
}

func TestDiskAnalysisCacheSweepRemovesExpiredEntriesAndRetainsFreshEntriesOverTarget(t *testing.T) {
	cache := newTestDiskAnalysisCache(t, t.TempDir(), time.Microsecond, 0, false)
	for index := range 8 {
		lookup := DiskAnalysisCacheLookup{
			Source:      DiskAnalysisSourceMetadata{FileName: fmt.Sprintf("/site/%d.asp", index), MtimeMS: int64(index), Size: 10},
			SettingsKey: "settings",
		}
		if err := cache.WriteFileBundle(DiskFileBundleCacheEntry{DiskAnalysisCacheLookup: lookup, Diagnostics: []lsp.Diagnostic{{Message: "cached"}}}); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(10 * time.Millisecond)
	if err := cache.SweepBatch(3); err != nil {
		t.Fatal(err)
	}
	assertBucketKeyCount(t, cache, diskCacheFilesBucket, 0)

	cache.ttl = time.Hour
	cache.maxSize = 1
	for index := range 8 {
		lookup := DiskAnalysisCacheLookup{
			Source:      DiskAnalysisSourceMetadata{FileName: fmt.Sprintf("/large/%d.asp", index), MtimeMS: int64(index), Size: 10},
			SettingsKey: "settings",
		}
		if err := cache.WriteFileBundle(DiskFileBundleCacheEntry{DiskAnalysisCacheLookup: lookup, Diagnostics: []lsp.Diagnostic{{Message: "large"}}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := cache.SweepBatch(2); err != nil {
		t.Fatal(err)
	}
	assertBucketKeyCount(t, cache, diskCacheFilesBucket, 8)
}

func TestDiskAnalysisCacheCloseWaitsForConcurrentSweep(t *testing.T) {
	cache := newTestDiskAnalysisCache(t, t.TempDir(), time.Hour, 1024, false)
	lookup := testDiskLookup("settings")
	for index := range 64 {
		lookup.SettingsKey = fmt.Sprintf("settings-%d", index)
		if err := cache.WriteFileBundle(DiskFileBundleCacheEntry{
			DiskAnalysisCacheLookup: lookup,
			Diagnostics:             []lsp.Diagnostic{{Message: strings.Repeat("cached", 64)}},
			UpdateDiagnostics:       true,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := cache.Flush(); err != nil {
		t.Fatal(err)
	}
	sweepDone := make(chan error, 1)
	go func() { sweepDone <- cache.SweepBatch(1) }()
	if err := cache.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-sweepDone; err != nil && !errors.Is(err, bolterrors.ErrDatabaseNotOpen) {
		t.Fatalf("SweepBatch() error = %v", err)
	}
}

func TestDiskAnalysisCacheSweepEnforcesRootWideDatabaseQuota(t *testing.T) {
	directory := t.TempDir()
	firstOptions := testDiskCacheOptions(directory, 0, 0, false)
	firstOptions.Namespace = "first"
	first, err := NewDiskAnalysisCache(firstOptions)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.WriteFileBundle(DiskFileBundleCacheEntry{DiskAnalysisCacheLookup: testDiskLookup("first"), Diagnostics: []lsp.Diagnostic{{Message: string(make([]byte, 8192))}}}); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	firstPath := first.databasePath
	time.Sleep(10 * time.Millisecond)

	secondOptions := testDiskCacheOptions(directory, 0, 0, false)
	secondOptions.Namespace = "second"
	second, err := NewDiskAnalysisCache(secondOptions)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	if err := second.WriteFileBundle(DiskFileBundleCacheEntry{DiskAnalysisCacheLookup: testDiskLookup("second"), UpdateDiagnostics: true}); err != nil {
		t.Fatal(err)
	}
	if err := second.Flush(); err != nil {
		t.Fatal(err)
	}
	secondInfo, err := os.Stat(second.databasePath)
	if err != nil {
		t.Fatal(err)
	}
	second.maxSize = secondInfo.Size() + 1
	if err := second.Sweep(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(firstPath); !os.IsNotExist(err) {
		t.Fatalf("old workspace database was not removed: %v", err)
	}
	if _, err := os.Stat(second.databasePath); err != nil {
		t.Fatalf("active workspace database was removed: %v", err)
	}
}

func TestDiskAnalysisCacheCompactsAfterLargeEviction(t *testing.T) {
	cache := newTestDiskAnalysisCache(t, t.TempDir(), time.Microsecond, 0, false)
	payload := json.RawMessage(bytes.Repeat([]byte("x"), 8*1024))
	for index := range 512 {
		lookup := DiskAnalysisCacheLookup{
			Source:      DiskAnalysisSourceMetadata{FileName: fmt.Sprintf("/compact/%d.asp", index), MtimeMS: int64(index), Size: int64(len(payload))},
			SettingsKey: "compact",
		}
		if err := cache.WriteFileBundle(DiskFileBundleCacheEntry{DiskAnalysisCacheLookup: lookup, AnalysisSnapshot: payload}); err != nil {
			t.Fatal(err)
		}
	}
	if err := cache.Flush(); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(cache.databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if before.Size() < diskCacheCompactThreshold {
		t.Fatalf("database size before compaction = %d, want at least %d", before.Size(), diskCacheCompactThreshold)
	}
	time.Sleep(10 * time.Millisecond)
	cache.maxSize = 256 * 1024
	if err := cache.SweepBatch(64); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(cache.databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if after.Size() >= before.Size() {
		t.Fatalf("database size before/after compaction = %d/%d, want reduced", before.Size(), after.Size())
	}
}

func TestDiskAnalysisCacheFlushCoalescesConcurrentWrites(t *testing.T) {
	cache := newTestDiskAnalysisCache(t, t.TempDir(), 0, 0, false)
	const entries = 256
	var wait sync.WaitGroup
	for index := range entries {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			lookup := DiskAnalysisCacheLookup{
				Source:      DiskAnalysisSourceMetadata{FileName: fmt.Sprintf("/site/%d.asp", index), MtimeMS: int64(index), Size: 10},
				SettingsKey: "settings",
			}
			if err := cache.WriteFileBundle(DiskFileBundleCacheEntry{DiskAnalysisCacheLookup: lookup, Diagnostics: []lsp.Diagnostic{{Message: fmt.Sprint(index)}}}); err != nil {
				t.Errorf("Write(%d) error = %v", index, err)
			}
		}(index)
	}
	wait.Wait()
	if err := cache.Flush(); err != nil {
		t.Fatal(err)
	}
	assertBucketKeyCount(t, cache, diskCacheFilesBucket, entries)
	for _, index := range []int{0, 63, 127, 255} {
		lookup := DiskAnalysisCacheLookup{
			Source:      DiskAnalysisSourceMetadata{FileName: fmt.Sprintf("/site/%d.asp", index), MtimeMS: int64(index), Size: 10},
			SettingsKey: "settings",
		}
		if bundle, ok := cache.ReadFileBundle(lookup); !ok || bundle.Diagnostics[0].Message != fmt.Sprint(index) {
			t.Fatalf("ReadFileBundle(%d) = %#v, %v", index, bundle, ok)
		}
	}
}

func TestDiskAnalysisCacheSameFileWritesSurviveInFlightFlush(t *testing.T) {
	cache := newTestDiskAnalysisCache(t, t.TempDir(), 0, 0, false)
	parsedLookup := testDiskLookup("parsed")
	diagnosticsLookup := parsedLookup
	diagnosticsLookup.SettingsKey = "diagnostics"
	for version := range 100 {
		oldParsed := core.ParseDocument("file:///site/default.asp", fmt.Sprintf("<%% Const value = %d %%>", version), core.Settings{})
		if err := cache.WriteFileBundle(DiskFileBundleCacheEntry{DiskAnalysisCacheLookup: parsedLookup, Parsed: oldParsed, UpdateParsed: true}); err != nil {
			t.Fatal(err)
		}
		flushDone := make(chan error, 1)
		go func() { flushDone <- cache.Flush() }()
		newParsed := core.ParseDocument("file:///site/default.asp", fmt.Sprintf("<%% Const value = %d %%>", version+1), core.Settings{})
		if err := cache.WriteFileBundle(DiskFileBundleCacheEntry{DiskAnalysisCacheLookup: parsedLookup, Parsed: newParsed, UpdateParsed: true}); err != nil {
			t.Fatal(err)
		}
		if err := cache.WriteFileBundle(DiskFileBundleCacheEntry{
			DiskAnalysisCacheLookup: diagnosticsLookup,
			Diagnostics:             []lsp.Diagnostic{{Message: fmt.Sprint(version)}},
			UpdateDiagnostics:       true,
		}); err != nil {
			t.Fatal(err)
		}
		if err := <-flushDone; err != nil {
			t.Fatal(err)
		}
		if err := cache.Flush(); err != nil {
			t.Fatal(err)
		}
		if bundle, ok := cache.ReadFileBundle(parsedLookup); !ok || bundle.Parsed == nil || bundle.Parsed.Text != newParsed.Text {
			t.Fatalf("parsed bundle after in-flight flush = %#v, %v", bundle, ok)
		}
		if bundle, ok := cache.ReadFileBundle(diagnosticsLookup); !ok || len(bundle.Diagnostics) != 1 || bundle.Diagnostics[0].Message != fmt.Sprint(version) {
			t.Fatalf("diagnostics bundle after in-flight flush = %#v, %v", bundle, ok)
		}
	}
	assertBucketKeyCount(t, cache, diskCacheFilesBucket, 1)
}

func TestDiskAnalysisCacheOpenTimesOutWhenWorkspaceDatabaseIsLocked(t *testing.T) {
	directory := t.TempDir()
	cache := newTestDiskAnalysisCache(t, directory, 0, 0, false)
	started := time.Now()
	second, err := NewDiskAnalysisCache(testDiskCacheOptions(directory, 0, 0, false))
	if second != nil {
		_ = second.Close()
	}
	if err == nil {
		t.Fatal("second NewDiskAnalysisCache() succeeded while database was locked")
	}
	if !errors.Is(err, bolterrors.ErrTimeout) {
		t.Fatalf("NewDiskAnalysisCache() error = %v, want ErrTimeout", err)
	}
	if elapsed := time.Since(started); elapsed < 200*time.Millisecond || elapsed > 2*time.Second {
		t.Fatalf("lock timeout elapsed = %v, want near 250ms", elapsed)
	}
	if !cache.Enabled() {
		t.Fatal("first cache unexpectedly disabled")
	}
}

func TestDiskAnalysisCacheRecreatesCorruptOrIncompatibleDatabase(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		corrupt func(*testing.T, string)
	}{
		{name: "invalid database", corrupt: func(t *testing.T, path string) {
			if err := os.WriteFile(path, []byte("not-bbolt"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "incompatible schema", corrupt: func(t *testing.T, path string) {
			db, err := bolt.Open(path, 0o600, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Update(func(tx *bolt.Tx) error {
				return tx.Bucket(diskCacheMetaBucket).Put(diskCacheSchemaKey, []byte("future"))
			}); err != nil {
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "incompatible tool version", corrupt: func(t *testing.T, path string) {
			db, err := bolt.Open(path, 0o600, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Update(func(tx *bolt.Tx) error {
				return tx.Bucket(diskCacheMetaBucket).Put(diskCacheToolVersionKey, []byte("older"))
			}); err != nil {
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			directory := t.TempDir()
			cache := newTestDiskAnalysisCache(t, directory, 0, 0, false)
			path := cache.databasePath
			if err := cache.Close(); err != nil {
				t.Fatal(err)
			}
			testCase.corrupt(t, path)
			recreated := newTestDiskAnalysisCache(t, directory, 0, 0, false)
			if err := recreated.WriteFileBundle(DiskFileBundleCacheEntry{DiskAnalysisCacheLookup: testDiskLookup("recreated"), UpdateDiagnostics: true}); err != nil {
				t.Fatal(err)
			}
			if err := recreated.Flush(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDiskAnalysisCacheClearClosesAndRemovesDirectory(t *testing.T) {
	directory := t.TempDir()
	unrelatedPath := filepath.Join(directory, "keep.txt")
	if err := os.WriteFile(unrelatedPath, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	legacyShard := filepath.Join(directory, "aa")
	if err := os.MkdirAll(legacyShard, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(directory, "legacy.cbor"), filepath.Join(legacyShard, "entry.cbor")} {
		if err := os.WriteFile(path, []byte("legacy"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cache := newTestDiskAnalysisCache(t, directory, 0, 0, false)
	if err := cache.WriteFileBundle(DiskFileBundleCacheEntry{DiskAnalysisCacheLookup: testDiskLookup("clear"), UpdateDiagnostics: true}); err != nil {
		t.Fatal(err)
	}
	if err := cache.Clear(); err != nil {
		t.Fatal(err)
	}
	if content, err := os.ReadFile(unrelatedPath); err != nil || string(content) != "keep" {
		t.Fatalf("Clear removed unrelated file: content=%q err=%v", content, err)
	}
	if _, err := os.Stat(filepath.Join(directory, "bbolt-v1")); !os.IsNotExist(err) {
		t.Fatalf("bbolt directory still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(directory, "legacy.cbor")); !os.IsNotExist(err) {
		t.Fatalf("legacy loose cache still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(legacyShard, "entry.cbor")); !os.IsNotExist(err) {
		t.Fatalf("legacy shard cache still exists: %v", err)
	}
	if err := cache.WriteFileBundle(DiskFileBundleCacheEntry{DiskAnalysisCacheLookup: testDiskLookup("closed"), UpdateDiagnostics: true}); !errors.Is(err, bolterrors.ErrDatabaseNotOpen) {
		t.Fatalf("Write() after Clear error = %v, want ErrDatabaseNotOpen", err)
	}
}

func BenchmarkDiskAnalysisCacheBatchWrite10000(b *testing.B) {
	for b.Loop() {
		cache, err := NewDiskAnalysisCache(testDiskCacheOptions(b.TempDir(), 0, 0, false))
		if err != nil {
			b.Fatal(err)
		}
		for index := range 10_000 {
			lookup := DiskAnalysisCacheLookup{
				Source:      DiskAnalysisSourceMetadata{FileName: fmt.Sprintf("/site/%d.asp", index), MtimeMS: int64(index), Size: 10},
				SettingsKey: "settings",
			}
			if err := cache.WriteFileBundle(DiskFileBundleCacheEntry{DiskAnalysisCacheLookup: lookup, UpdateDiagnostics: true}); err != nil {
				b.Fatal(err)
			}
		}
		if err := cache.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func TestStableDiskHashBytesMatchesPersistedStringEncoding(t *testing.T) {
	for _, value := range []string{"", "/site/default.asp", "日本語/ページ.asp"} {
		if got, want := string(stableDiskHashBytes(value)), stableDiskHash(value); got != want {
			t.Fatalf("stableDiskHashBytes(%q) = %q, want %q", value, got, want)
		}
	}
}

func BenchmarkDiskAnalysisCacheWarmRead(b *testing.B) {
	cache, err := NewDiskAnalysisCache(testDiskCacheOptions(b.TempDir(), 0, 0, false))
	if err != nil {
		b.Fatal(err)
	}
	defer cache.Close()
	lookup := testDiskLookup("benchmark")
	if err := cache.WriteFileBundle(DiskFileBundleCacheEntry{DiskAnalysisCacheLookup: lookup, Diagnostics: []lsp.Diagnostic{{Message: "cached"}}}); err != nil {
		b.Fatal(err)
	}
	if err := cache.Flush(); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, ok := cache.ReadFileBundle(lookup); !ok {
			b.Fatal("cache miss")
		}
	}
}

func BenchmarkDiskAnalysisCacheTTLSweep10000(b *testing.B) {
	directory := b.TempDir()
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		b.StopTimer()
		cache, err := NewDiskAnalysisCache(testDiskCacheOptions(directory, time.Nanosecond, 0, false))
		if err != nil {
			b.Fatal(err)
		}
		for index := range 10_000 {
			lookup := DiskAnalysisCacheLookup{
				Source:      DiskAnalysisSourceMetadata{FileName: fmt.Sprintf("/ttl/%d.asp", index), MtimeMS: int64(index), Size: 10},
				SettingsKey: "settings",
			}
			if err := cache.WriteFileBundle(DiskFileBundleCacheEntry{DiskAnalysisCacheLookup: lookup, UpdateDiagnostics: true}); err != nil {
				b.Fatal(err)
			}
		}
		if err := cache.Flush(); err != nil {
			b.Fatal(err)
		}
		time.Sleep(time.Millisecond)
		b.StartTimer()
		if err := cache.SweepBatch(10_000); err != nil {
			b.Fatal(err)
		}
		b.StopTimer()
		if err := cache.Close(); err != nil {
			b.Fatal(err)
		}
		if err := os.RemoveAll(filepath.Join(directory, "bbolt-v1")); err != nil {
			b.Fatal(err)
		}
	}
}

func newTestDiskAnalysisCache(t testing.TB, directory string, ttl time.Duration, maxSize int64, gzip bool) *DiskAnalysisCache {
	t.Helper()
	cache, err := NewDiskAnalysisCache(testDiskCacheOptions(directory, ttl, maxSize, gzip))
	if err != nil {
		t.Fatalf("NewDiskAnalysisCache() error = %v", err)
	}
	t.Cleanup(func() { _ = cache.Close() })
	return cache
}

func testDiskCacheOptions(directory string, ttl time.Duration, maxSize int64, gzip bool) DiskAnalysisCacheOptions {
	return DiskAnalysisCacheOptions{
		Enabled:     true,
		Directory:   directory,
		TTL:         ttl,
		MaxSize:     maxSize,
		Namespace:   "test",
		ToolVersion: "1",
		Gzip:        gzip,
	}
}

func testDiskLookup(settingsKey string) DiskAnalysisCacheLookup {
	return DiskAnalysisCacheLookup{
		Source:      DiskAnalysisSourceMetadata{FileName: "/site/default.asp", MtimeMS: 1, Size: 10},
		SettingsKey: settingsKey,
	}
}

func assertBucketKeyCount(t testing.TB, cache *DiskAnalysisCache, bucketName []byte, want int) {
	t.Helper()
	if err := cache.Flush(); err != nil {
		t.Fatal(err)
	}
	cache.dbMu.RLock()
	defer cache.dbMu.RUnlock()
	err := cache.db.View(func(tx *bolt.Tx) error {
		got := tx.Bucket(bucketName).Stats().KeyN
		if got != want {
			t.Fatalf("bucket %q key count = %d, want %d", bucketName, got, want)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func readOnlyFileValue(t testing.TB, databasePath string) []byte {
	t.Helper()
	db, err := bolt.Open(databasePath, 0o600, &bolt.Options{ReadOnly: true, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var result []byte
	if err := db.View(func(tx *bolt.Tx) error {
		_, value := tx.Bucket(diskCacheFilesBucket).Cursor().First()
		result = append([]byte(nil), value...)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return result
}
