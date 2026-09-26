package lspserver

import (
	"fmt"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/fxamacker/cbor/v2"
	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

func TestWorkspaceReferenceTombstoneRejectsStaleRemovalRevision(t *testing.T) {
	root := t.TempDir()
	server := newWorkspaceReferencePersistenceServer(t, root, filepath.Join(root, "cache"))
	t.Cleanup(server.closeDiskAnalysisCache)
	uri := filePathURI(filepath.Join(root, "default.asp"))
	first := core.NewTextDocument(uri, "classic-asp", 1, `<% Dim PreviousValue %>`)
	server.mu.Lock()
	server.workspace[uri] = first
	server.mu.Unlock()
	if result := server.applyWorkspaceDocumentRevision(first, server.parseTextDocument(first, "VBScript")); result.Manifest == nil || result.Stale {
		t.Fatalf("first workspace revision = %#v", result)
	}

	reachedQueue := make(chan struct{})
	releaseQueue := make(chan struct{})
	var once sync.Once
	server.workspaceReferenceTombstoneBeforeQueueTestHook = func() {
		once.Do(func() { close(reachedQueue) })
		<-releaseQueue
	}
	tombstoneDone := make(chan struct{})
	go func() {
		defer close(tombstoneDone)
		server.removeWorkspaceDocumentRevision(uri)
	}()
	<-reachedQueue

	latest := core.NewTextDocument(uri, "classic-asp", 2, `<% Dim LatestValue : LatestValue = 1 %>`)
	server.mu.Lock()
	server.workspace[uri] = latest
	server.mu.Unlock()
	latestParsed := server.parseTextDocument(latest, "VBScript")
	if result := server.applyWorkspaceDocumentRevision(latest, latestParsed); result.Manifest == nil || result.Stale {
		t.Fatalf("latest workspace revision = %#v", result)
	}
	close(releaseQueue)
	<-tombstoneDone
	server.workspaceReferenceTombstoneBeforeQueueTestHook = nil
	server.waitForAsyncDiskCacheWrites()
	if err := server.diskCacheForUse().Flush(); err != nil {
		t.Fatal(err)
	}
	manifest := persistedReferenceManifestForURI(t, server, uri)
	if manifest == nil || manifest.SourceHash != workspacepkg.DiskContentHash(latest.Text) {
		t.Fatalf("persisted latest manifest = %#v", manifest)
	}
	segments := server.referenceWorkspaceIndex.segmentsForName("LatestValue", []*core.ParsedDocument{latestParsed})
	if len(segments) != 1 {
		t.Fatalf("latest in-memory segments = %#v, want one", segments)
	}
}

func TestWorkspaceReferencePostingDeltaWritesOnlyChangedNames(t *testing.T) {
	documentKey := "file:///workspace/default.asp"
	oldManifest := persistedWorkspaceReferenceDocument{
		DocumentKey: documentKey,
		PostingKeys: []string{"first", "removed"},
		PostingFingerprints: map[string]string{
			"first":   "first-fingerprint",
			"removed": "removed-fingerprint",
		},
	}
	names := []string{"changed", "first"}
	fingerprints := map[string]string{
		"changed": "changed-fingerprint",
		"first":   "first-fingerprint",
	}

	removed, current, changed := workspaceReferencePostingDelta(documentKey, oldManifest, names, fingerprints)
	wantRemoved := [][]byte{[]byte(workspaceReferencePostingCacheKey(documentKey, "removed"))}
	wantCurrent := [][]byte{
		[]byte(workspaceReferencePostingCacheKey(documentKey, "changed")),
		[]byte(workspaceReferencePostingCacheKey(documentKey, "first")),
	}
	wantChanged := []string{"changed"}
	if !reflect.DeepEqual(removed, wantRemoved) || !reflect.DeepEqual(current, wantCurrent) || !reflect.DeepEqual(changed, wantChanged) {
		t.Fatalf("posting delta = removed %q, current %q, changed %q", removed, current, changed)
	}
}

func BenchmarkWorkspaceReferencePostingDelta1000Names(b *testing.B) {
	const nameCount = 1000
	documentKey := "file:///workspace/default.asp"
	names := make([]string, nameCount)
	fingerprints := make(map[string]string, nameCount)
	oldManifest := persistedWorkspaceReferenceDocument{
		DocumentKey: documentKey, PostingKeys: make([]string, nameCount), PostingFingerprints: make(map[string]string, nameCount),
	}
	for index := range names {
		name := fmt.Sprintf("symbol%04d", index)
		fingerprint := fmt.Sprintf("fingerprint-%04d", index)
		names[index], oldManifest.PostingKeys[index] = name, name
		fingerprints[name], oldManifest.PostingFingerprints[name] = fingerprint, fingerprint
	}
	oldManifest.PostingFingerprints[names[len(names)/2]] = "previous-fingerprint"
	b.ReportAllocs()
	for b.Loop() {
		removed, current, changed := workspaceReferencePostingDelta(documentKey, oldManifest, names, fingerprints)
		if len(removed) != 0 || len(current) != nameCount || len(changed) != 1 {
			b.Fatalf("posting delta = %d removed, %d current, %d changed", len(removed), len(current), len(changed))
		}
	}
}

func TestWorkspaceReferencePostingDeltaRewritesLegacyManifestNames(t *testing.T) {
	documentKey := "file:///workspace/default.asp"
	legacyPayload, err := cbor.Marshal(struct {
		SchemaVersion int      `cbor:"schemaVersion"`
		DocumentKey   string   `cbor:"documentKey"`
		PostingKeys   []string `cbor:"postingKeys"`
	}{SchemaVersion: workspaceReferenceDocumentSchemaVersion, DocumentKey: documentKey, PostingKeys: []string{"shared"}})
	if err != nil {
		t.Fatal(err)
	}
	var oldManifest persistedWorkspaceReferenceDocument
	if err := cbor.Unmarshal(legacyPayload, &oldManifest); err != nil {
		t.Fatal(err)
	}
	if oldManifest.PostingFingerprints != nil {
		t.Fatalf("legacy posting fingerprints = %#v, want nil", oldManifest.PostingFingerprints)
	}

	_, _, changed := workspaceReferencePostingDelta(
		documentKey,
		oldManifest,
		[]string{"shared"},
		map[string]string{"shared": "shared-fingerprint"},
	)
	if !reflect.DeepEqual(changed, []string{"shared"}) {
		t.Fatalf("legacy manifest changed postings = %q, want shared", changed)
	}
}

func TestWorkspaceReferenceRepairMissingPostingsRewritesOnlyAbsentValues(t *testing.T) {
	names := []string{"changed", "missing", "present"}
	changed := workspaceReferenceRepairMissingPostings(names, []string{"changed"}, []bool{false, false, true})
	if !reflect.DeepEqual(changed, []string{"changed", "missing"}) {
		t.Fatalf("repaired postings = %q, want changed and missing", changed)
	}
}
