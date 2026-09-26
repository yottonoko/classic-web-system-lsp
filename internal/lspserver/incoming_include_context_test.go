package lspserver

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

type incomingIncludeFixture struct {
	server     *Server
	root       string
	targetPath string
	ownerPath  string
	targetURI  string
	ownerURI   string
}

func newIncomingIncludeFixture(t *testing.T) incomingIncludeFixture {
	t.Helper()
	root := t.TempDir()
	server := New(nil, io.Discard, io.Discard)
	server.settings.CacheEnabled = false
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
	server.configureFsGateway()
	t.Cleanup(func() { server.shutdownRuntimeCaches() })
	targetPath := filepath.Join(root, "target.inc")
	ownerPath := filepath.Join(root, "owner.asp")
	targetURI := filePathURI(targetPath)
	ownerURI := filePathURI(ownerPath)
	return incomingIncludeFixture{
		server: server, root: root,
		targetPath: targetPath, ownerPath: ownerPath,
		targetURI: targetURI, ownerURI: ownerURI,
	}
}

func (f incomingIncludeFixture) installDocuments(ownerSource, targetSource string) {
	f.server.workspace[f.ownerURI] = core.NewTextDocument(f.ownerURI, "classic-asp", 1, ownerSource)
	f.server.workspace[f.targetURI] = core.NewTextDocument(f.targetURI, "classic-asp", 1, targetSource)
}

func (f incomingIncludeFixture) installCompleteGraph(ownerPath string, targetPath string) {
	f.server.workspaceIncludeGraph.Reset("test")
	f.server.workspaceIncludeGraph.Upsert(ownerPath, workspacepkg.SourceMetadata{FileName: ownerPath}, []string{targetPath}, "refs")
	f.server.workspaceIncludeGraphComplete = true
	f.server.workspaceIncludeGraphRevision++
}

func TestIncomingIncludeDocumentsContextCompleteSuccess(t *testing.T) {
	fixture := newIncomingIncludeFixture(t)
	fixture.installDocuments(`<!-- #include file="target.inc" -->`, "<% Dim TargetValue %>")
	fixture.installCompleteGraph(fixture.ownerPath, fixture.targetPath)

	result := fixture.server.incomingIncludeDocumentsForTargetsContextResult(context.Background(),
		map[string]struct{}{fixture.targetPath: {}}, map[string]struct{}{fixture.targetURI: {}})
	if !result.complete || result.err != nil {
		t.Fatalf("incoming result = complete:%t err:%v; want complete success", result.complete, result.err)
	}
	if len(result.documents) != 1 || !workspacepkg.SameFileIdentityURI(result.documents[0].URI, fixture.ownerURI) {
		t.Fatalf("incoming documents = %#v; want owner %q", result.documents, fixture.ownerURI)
	}
}

func TestIncomingIncludeDocumentsContextDisabledReverseIndexScansCurrentDocuments(t *testing.T) {
	fixture := newIncomingIncludeFixture(t)
	fixture.installDocuments(`<!-- #include file="target.inc" -->`, "<% Dim TargetValue %>")
	fixture.installCompleteGraph(fixture.ownerPath, fixture.targetPath)
	fixture.server.settings.GraphUseReverseIncludeIndex = false

	beforeGraph := fixture.server.workspaceIncludeGraph
	beforeRevision := fixture.server.workspaceIncludeGraphRevision
	result := fixture.server.incomingIncludeDocumentsForTargetsContextResult(context.Background(),
		map[string]struct{}{fixture.targetPath: {}}, map[string]struct{}{fixture.targetURI: {}})
	if !result.complete || result.err != nil {
		t.Fatalf("disabled reverse-index result = complete:%t err:%v; want complete success", result.complete, result.err)
	}
	if got := incomingIncludeDocumentKeys(result.documents); len(got) != 1 || got[0] != workspacepkg.FileIdentityKeyFromURI(fixture.ownerURI) {
		t.Fatalf("disabled reverse-index incoming documents = %#v; want owner %q", got, fixture.ownerURI)
	}
	if fixture.server.workspaceIncludeGraph != beforeGraph || fixture.server.workspaceIncludeGraphRevision != beforeRevision || !fixture.server.workspaceIncludeGraphComplete {
		t.Fatalf("disabled reverse-index scan mutated include graph: pointerChanged=%v revision=%d/%d complete=%v", fixture.server.workspaceIncludeGraph != beforeGraph, fixture.server.workspaceIncludeGraphRevision, beforeRevision, fixture.server.workspaceIncludeGraphComplete)
	}
}

func TestIncomingIncludeDocumentsContextDisabledReverseIndexMatchesRestoredDiskOwners(t *testing.T) {
	fixture := newIncomingIncludeFixture(t)
	secondRoot := filepath.Join(fixture.root, "second-root")
	if err := os.MkdirAll(secondRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	secondOwnerPath := filepath.Join(secondRoot, "owner-b.asp")
	secondOwnerURI := filePathURI(secondOwnerPath)
	excludedOwnerPath := filepath.Join(fixture.root, "excluded.asp")
	ignoredOwnerPath := filepath.Join(fixture.root, "ignored.asp")
	for path, source := range map[string]string{
		fixture.targetPath: "<% Dim TargetValue %>",
		fixture.ownerPath:  `<!-- #include file="target.inc" -->`,
		secondOwnerPath:    `<!-- #include file="../target.inc" -->`,
		excludedOwnerPath:  `<!-- #include file="target.inc" -->`,
		ignoredOwnerPath:   `<!-- #include file="target.inc" -->`,
	} {
		if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(fixture.root, ".gitignore"), []byte("ignored.asp\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.server.workspaceRoots = []workspaceRoot{
		{Path: fixture.root, URI: fixture.server.rootURI},
		{Path: secondRoot, URI: filePathURI(secondRoot)},
	}
	fixture.server.settings.WorkspaceExcludeGlobs = []string{"excluded.asp"}
	fixture.server.settings.WorkspaceRespectGitIgnore = true
	// Keep the target open while both owners remain disk-only. This models a
	// restored graph before the workspace index has hydrated its sources.
	fixture.server.workspace = map[string]*core.TextDocument{}
	fixture.server.documents[fixture.targetURI] = core.NewTextDocument(fixture.targetURI, "classic-asp", 2, "<% Dim OpenTargetValue %>")
	fixture.installCompleteGraph(fixture.ownerPath, fixture.targetPath)
	fixture.server.workspaceIncludeGraph.Upsert(secondOwnerPath, workspacepkg.SourceMetadata{FileName: secondOwnerPath}, []string{fixture.targetPath}, "second-root")
	fixture.server.workspaceIncludeGraphRevision++

	beforeGraph := fixture.server.workspaceIncludeGraph
	beforeRevision := fixture.server.workspaceIncludeGraphRevision
	targetPaths := map[string]struct{}{fixture.targetPath: {}}
	excludedURIs := map[string]struct{}{fixture.targetURI: {}}
	results := make([][]string, 0, 2)
	for _, useReverseIndex := range []bool{true, false} {
		fixture.server.settings.GraphUseReverseIncludeIndex = useReverseIndex
		result := fixture.server.incomingIncludeDocumentsForTargetsContextResult(context.Background(), targetPaths, excludedURIs)
		if !result.complete || result.err != nil {
			t.Fatalf("useReverseIncludeIndex=%t result = complete:%t err:%v; want complete success", useReverseIndex, result.complete, result.err)
		}
		results = append(results, incomingIncludeDocumentKeys(result.documents))
	}
	want := []string{
		workspacepkg.FileIdentityKeyFromURI(fixture.ownerURI),
		workspacepkg.FileIdentityKeyFromURI(secondOwnerURI),
	}
	sort.Strings(want)
	for index, result := range results {
		if !sameStringSlices(result, want) {
			t.Fatalf("useReverseIndex=%t incoming disk owners = %#v; want %#v", index == 0, result, want)
		}
	}
	if !sameStringSlices(results[0], results[1]) {
		t.Fatalf("reverse-index disk parity mismatch: enabled=%#v disabled=%#v", results[0], results[1])
	}
	if fixture.server.workspaceIncludeGraph != beforeGraph || fixture.server.workspaceIncludeGraphRevision != beforeRevision || !fixture.server.workspaceIncludeGraphComplete {
		t.Fatalf("disabled reverse-index disk scan mutated restored graph: pointerChanged=%v revision=%d/%d complete=%v", fixture.server.workspaceIncludeGraph != beforeGraph, fixture.server.workspaceIncludeGraphRevision, beforeRevision, fixture.server.workspaceIncludeGraphComplete)
	}
}

func TestIncomingIncludeDocumentsContextDisabledReverseIndexDiskCancellationReturnsNoPrefix(t *testing.T) {
	fixture := newIncomingIncludeFixture(t)
	for path, source := range map[string]string{
		fixture.targetPath: "<% Dim TargetValue %>",
		fixture.ownerPath:  `<!-- #include file="target.inc" -->`,
	} {
		if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	fixture.server.workspace = map[string]*core.TextDocument{}
	fixture.server.documents[fixture.targetURI] = core.NewTextDocument(fixture.targetURI, "classic-asp", 2, "<% Dim OpenTargetValue %>")
	fixture.installCompleteGraph(fixture.ownerPath, fixture.targetPath)
	fixture.server.settings.GraphUseReverseIncludeIndex = false
	beforeGraph := fixture.server.workspaceIncludeGraph
	beforeRevision := fixture.server.workspaceIncludeGraphRevision
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fixture.server.workspaceFileReadTestHook = func(path string) {
		if filepath.Clean(path) == filepath.Clean(fixture.ownerPath) {
			cancel()
		}
	}

	result := fixture.server.incomingIncludeDocumentsForTargetsContextResult(ctx,
		map[string]struct{}{fixture.targetPath: {}}, map[string]struct{}{fixture.targetURI: {}})
	if result.complete || result.err != context.Canceled || result.documents != nil {
		t.Fatalf("disabled reverse-index disk cancellation result = %#v; want empty incomplete cancellation", result)
	}
	if fixture.server.workspaceIncludeGraph != beforeGraph || fixture.server.workspaceIncludeGraphRevision != beforeRevision || !fixture.server.workspaceIncludeGraphComplete {
		t.Fatalf("disabled reverse-index disk cancellation mutated graph: pointerChanged=%v revision=%d/%d complete=%v", fixture.server.workspaceIncludeGraph != beforeGraph, fixture.server.workspaceIncludeGraphRevision, beforeRevision, fixture.server.workspaceIncludeGraphComplete)
	}
}

func TestIncomingIncludeDocumentsContextDisabledReverseIndexDiskGenerationDriftReturnsNoPrefix(t *testing.T) {
	fixture := newIncomingIncludeFixture(t)
	for path, source := range map[string]string{
		fixture.targetPath: "<% Dim TargetValue %>",
		fixture.ownerPath:  `<!-- #include file="target.inc" -->`,
	} {
		if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	fixture.server.workspace = map[string]*core.TextDocument{}
	fixture.server.documents[fixture.targetURI] = core.NewTextDocument(fixture.targetURI, "classic-asp", 2, "<% Dim OpenTargetValue %>")
	fixture.installCompleteGraph(fixture.ownerPath, fixture.targetPath)
	fixture.server.settings.GraphUseReverseIncludeIndex = false
	beforeGraph := fixture.server.workspaceIncludeGraph
	beforeRevision := fixture.server.workspaceIncludeGraphRevision
	var driftOnce sync.Once
	fixture.server.workspaceFileReadTestHook = func(path string) {
		if filepath.Clean(path) != filepath.Clean(fixture.ownerPath) {
			return
		}
		driftOnce.Do(func() {
			fixture.server.mu.Lock()
			fixture.server.graphGeneration++
			fixture.server.mu.Unlock()
		})
	}

	result := fixture.server.incomingIncludeDocumentsForTargetsContextResult(context.Background(),
		map[string]struct{}{fixture.targetPath: {}}, map[string]struct{}{fixture.targetURI: {}})
	if result.complete || result.err != errGraphCollectionGeneration || result.documents != nil {
		t.Fatalf("disabled reverse-index disk generation drift result = %#v; want empty incomplete generation result", result)
	}
	if fixture.server.workspaceIncludeGraph != beforeGraph || fixture.server.workspaceIncludeGraphRevision != beforeRevision || !fixture.server.workspaceIncludeGraphComplete {
		t.Fatalf("disabled reverse-index disk generation drift mutated graph: pointerChanged=%v revision=%d/%d complete=%v", fixture.server.workspaceIncludeGraph != beforeGraph, fixture.server.workspaceIncludeGraphRevision, beforeRevision, fixture.server.workspaceIncludeGraphComplete)
	}
}

func TestIncomingIncludeDocumentsContextDisabledReverseIndexMatchesEnabledAcrossRootsAndExclusions(t *testing.T) {
	fixture := newIncomingIncludeFixture(t)
	secondRoot := filepath.Join(fixture.root, "second-root")
	if err := os.MkdirAll(secondRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	secondOwnerPath := filepath.Join(secondRoot, "owner-b.asp")
	secondOwnerURI := filePathURI(secondOwnerPath)
	fixture.server.workspaceRoots = []workspaceRoot{
		{Path: fixture.root, URI: fixture.server.rootURI},
		{Path: secondRoot, URI: filePathURI(secondRoot)},
	}
	fixture.installDocuments(`<!-- #include file="target.inc" -->`, "<% Dim TargetValue %>")
	fixture.server.workspace[secondOwnerURI] = core.NewTextDocument(secondOwnerURI, "classic-asp", 1, `<!-- #include file="../target.inc" -->`)
	fixture.installCompleteGraph(fixture.ownerPath, fixture.targetPath)
	fixture.server.workspaceIncludeGraph.Upsert(secondOwnerPath, workspacepkg.SourceMetadata{FileName: secondOwnerPath}, []string{fixture.targetPath}, "second-root")
	fixture.server.workspaceIncludeGraphRevision++

	targetPaths := map[string]struct{}{fixture.targetPath: {}}
	excludedURIs := map[string]struct{}{fixture.targetURI: {}, secondOwnerURI: {}}
	results := make([][]string, 0, 2)
	for _, useReverseIndex := range []bool{true, false} {
		fixture.server.settings.GraphUseReverseIncludeIndex = useReverseIndex
		result := fixture.server.incomingIncludeDocumentsForTargetsContextResult(context.Background(), targetPaths, excludedURIs)
		if !result.complete || result.err != nil {
			t.Fatalf("useReverseIncludeIndex=%t result = complete:%t err:%v; want complete success", useReverseIndex, result.complete, result.err)
		}
		results = append(results, incomingIncludeDocumentKeys(result.documents))
	}
	if !sort.StringsAreSorted(results[0]) || !sort.StringsAreSorted(results[1]) {
		t.Fatalf("incoming document keys are not deterministic: enabled=%#v disabled=%#v", results[0], results[1])
	}
	if len(results[0]) != 1 || results[0][0] != workspacepkg.FileIdentityKeyFromURI(fixture.ownerURI) {
		t.Fatalf("enabled reverse-index incoming documents = %#v; want only owner %q after exclusion", results[0], fixture.ownerURI)
	}
	if len(results[1]) != 1 || results[1][0] != workspacepkg.FileIdentityKeyFromURI(fixture.ownerURI) {
		t.Fatalf("disabled reverse-index incoming documents = %#v; want only owner %q after exclusion", results[1], fixture.ownerURI)
	}
	if !sameStringSlices(results[0], results[1]) {
		t.Fatalf("reverse-index parity mismatch: enabled=%#v disabled=%#v", results[0], results[1])
	}
}

func TestIncomingIncludeDocumentsContextDisabledReverseIndexCancellationLeavesNoPartialState(t *testing.T) {
	fixture := newIncomingIncludeFixture(t)
	fixture.installDocuments(`<!-- #include file="target.inc" -->`, "<% Dim TargetValue %>")
	fixture.installCompleteGraph(fixture.ownerPath, fixture.targetPath)
	fixture.server.settings.GraphUseReverseIncludeIndex = false
	beforeGraph := fixture.server.workspaceIncludeGraph
	beforeRevision := fixture.server.workspaceIncludeGraphRevision
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fixture.server.documentParseTestHook = func(uri string) {
		if workspacepkg.SameFileIdentityURI(uri, fixture.ownerURI) {
			cancel()
		}
	}

	result := fixture.server.incomingIncludeDocumentsForTargetsContextResult(ctx,
		map[string]struct{}{fixture.targetPath: {}}, map[string]struct{}{fixture.targetURI: {}})
	if result.complete || result.err != context.Canceled || result.documents != nil {
		t.Fatalf("disabled reverse-index cancellation result = %#v; want empty incomplete cancellation", result)
	}
	if fixture.server.workspaceIncludeGraph != beforeGraph || fixture.server.workspaceIncludeGraphRevision != beforeRevision || !fixture.server.workspaceIncludeGraphComplete {
		t.Fatalf("disabled reverse-index cancellation mutated include graph: pointerChanged=%v revision=%d/%d complete=%v", fixture.server.workspaceIncludeGraph != beforeGraph, fixture.server.workspaceIncludeGraphRevision, beforeRevision, fixture.server.workspaceIncludeGraphComplete)
	}
}

func TestIncomingIncludeDocumentsContextDisabledReverseIndexGenerationDriftLeavesNoPartialState(t *testing.T) {
	fixture := newIncomingIncludeFixture(t)
	fixture.installDocuments(`<!-- #include file="target.inc" -->`, "<% Dim TargetValue %>")
	fixture.installCompleteGraph(fixture.ownerPath, fixture.targetPath)
	fixture.server.settings.GraphUseReverseIncludeIndex = false
	beforeGraph := fixture.server.workspaceIncludeGraph
	beforeRevision := fixture.server.workspaceIncludeGraphRevision
	var driftOnce sync.Once
	fixture.server.documentParseTestHook = func(uri string) {
		if !workspacepkg.SameFileIdentityURI(uri, fixture.ownerURI) {
			return
		}
		driftOnce.Do(func() {
			fixture.server.mu.Lock()
			fixture.server.graphGeneration++
			fixture.server.mu.Unlock()
		})
	}

	result := fixture.server.incomingIncludeDocumentsForTargetsContextResult(context.Background(),
		map[string]struct{}{fixture.targetPath: {}}, map[string]struct{}{fixture.targetURI: {}})
	if result.complete || result.err != errGraphCollectionGeneration || result.documents != nil {
		t.Fatalf("disabled reverse-index generation drift result = %#v; want empty incomplete generation result", result)
	}
	if fixture.server.workspaceIncludeGraph != beforeGraph || fixture.server.workspaceIncludeGraphRevision != beforeRevision || !fixture.server.workspaceIncludeGraphComplete {
		t.Fatalf("disabled reverse-index generation drift mutated include graph: pointerChanged=%v revision=%d/%d complete=%v", fixture.server.workspaceIncludeGraph != beforeGraph, fixture.server.workspaceIncludeGraphRevision, beforeRevision, fixture.server.workspaceIncludeGraphComplete)
	}
}

func incomingIncludeDocumentKeys(documents []*core.ParsedDocument) []string {
	keys := make([]string, 0, len(documents))
	for _, document := range documents {
		if document != nil {
			keys = append(keys, workspacepkg.FileIdentityKeyFromURI(document.URI))
		}
	}
	sort.Strings(keys)
	return keys
}

func sameStringSlices(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func TestIncomingIncludeDocumentsContextDoesNotTrustIncompleteReverseGraph(t *testing.T) {
	fixture := newIncomingIncludeFixture(t)
	ownerBPath := filepath.Join(fixture.root, "owner-b.asp")
	ownerBURI := filePathURI(ownerBPath)
	fixture.server.workspace[ownerBURI] = core.NewTextDocument(ownerBURI, "classic-asp", 1, `<!-- #include file="target.inc" -->`)
	fixture.server.workspace[fixture.targetURI] = core.NewTextDocument(fixture.targetURI, "classic-asp", 1, "<% Dim TargetValue %>")
	fixture.server.workspaceIncludeGraph.Reset("test")
	staleOwnerPath := filepath.Join(fixture.root, "stale-owner.asp")
	fixture.server.workspaceIncludeGraph.Upsert(staleOwnerPath, workspacepkg.SourceMetadata{FileName: staleOwnerPath}, []string{fixture.targetPath}, "stale")
	fixture.server.workspaceIncludeGraphComplete = false

	result := fixture.server.incomingIncludeDocumentsForTargetsContextResult(context.Background(),
		map[string]struct{}{fixture.targetPath: {}}, map[string]struct{}{fixture.targetURI: {}})
	if !result.complete || result.err != nil {
		t.Fatalf("incomplete reverse graph result = complete:%t err:%v; want recovered complete traversal", result.complete, result.err)
	}
	if len(result.documents) != 1 || !workspacepkg.SameFileIdentityURI(result.documents[0].URI, ownerBURI) {
		t.Fatalf("incoming documents = %#v; want current owner %q, not stale owner", result.documents, ownerBURI)
	}
}

func TestIncomingIncludeDocumentsContextIncludeSyncFailurePreservesGraph(t *testing.T) {
	fixture := newIncomingIncludeFixture(t)
	fixture.installDocuments(`<!-- #include file="target.inc" -->`, "<% Dim TargetValue %>")
	fixture.server.installIncompleteIncomingGraphForTest(fixture.ownerPath, fixture.targetPath)
	fixture.server.workspaceIncludeGraphSyncTestHook = func() bool { return false }

	beforeGraph := fixture.server.workspaceIncludeGraph
	beforeRevision := fixture.server.workspaceIncludeGraphRevision
	result := fixture.server.incomingIncludeDocumentsForTargetsContextResult(context.Background(),
		map[string]struct{}{fixture.targetPath: {}}, map[string]struct{}{fixture.targetURI: {}})
	if result.complete || result.err != errGraphCollectionIncludeSync {
		t.Fatalf("sync failure result = complete:%t err:%v; want incomplete include-sync failure", result.complete, result.err)
	}
	if fixture.server.workspaceIncludeGraph != beforeGraph || fixture.server.workspaceIncludeGraphRevision != beforeRevision || fixture.server.workspaceIncludeGraphComplete {
		t.Fatalf("include graph changed after sync failure: pointerChanged=%v revision=%d/%d complete=%v", fixture.server.workspaceIncludeGraph != beforeGraph, fixture.server.workspaceIncludeGraphRevision, beforeRevision, fixture.server.workspaceIncludeGraphComplete)
	}
}

func TestIncomingIncludeDocumentsContextGenerationDriftReturnsIncomplete(t *testing.T) {
	fixture := newIncomingIncludeFixture(t)
	fixture.installDocuments(`<!-- #include file="target.inc" -->`, "<% Dim TargetValue %>")
	fixture.installCompleteGraph(fixture.ownerPath, fixture.targetPath)
	var once sync.Once
	fixture.server.documentParseTestHook = func(uri string) {
		if uri != fixture.ownerURI {
			return
		}
		once.Do(func() {
			fixture.server.mu.Lock()
			fixture.server.graphGeneration++
			fixture.server.mu.Unlock()
		})
	}

	result := fixture.server.incomingIncludeDocumentsForTargetsContextResult(context.Background(),
		map[string]struct{}{fixture.targetPath: {}}, map[string]struct{}{fixture.targetURI: {}})
	if result.complete || result.err != errGraphCollectionGeneration || result.documents != nil {
		t.Fatalf("generation drift result = %#v; want empty incomplete generation result", result)
	}
}

func TestIncomingIncludeDocumentsContextMidTraversalCancellationReturnsNoPrefix(t *testing.T) {
	fixture := newIncomingIncludeFixture(t)
	ownerBPath := filepath.Join(fixture.root, "owner-b.asp")
	if err := os.WriteFile(fixture.targetPath, []byte("<% Dim TargetValue %>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.ownerPath, []byte(`<!-- #include file="target.inc" -->`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ownerBPath, []byte(`<!-- #include file="target.inc" -->`), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.server.workspaceIncludeGraph.Reset("test")
	fixture.server.workspaceIncludeGraph.Upsert(fixture.ownerPath, workspacepkg.SourceMetadata{FileName: fixture.ownerPath}, []string{fixture.targetPath}, "owner-a")
	fixture.server.workspaceIncludeGraph.Upsert(ownerBPath, workspacepkg.SourceMetadata{FileName: ownerBPath}, []string{fixture.targetPath}, "owner-b")
	fixture.server.workspaceIncludeGraphComplete = true
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var once sync.Once
	fixture.server.workspaceFileReadTestHook = func(path string) {
		if filepath.Clean(path) == filepath.Clean(fixture.ownerPath) || filepath.Clean(path) == filepath.Clean(ownerBPath) {
			once.Do(cancel)
		}
	}

	result := fixture.server.incomingIncludeDocumentsForTargetsContextResult(ctx,
		map[string]struct{}{fixture.targetPath: {}}, nil)
	if result.complete || result.err != context.Canceled || result.documents != nil {
		t.Fatalf("mid-traversal cancellation result = %#v; want empty incomplete cancellation", result)
	}
}

func (s *Server) installIncompleteIncomingGraphForTest(ownerPath, targetPath string) {
	s.workspaceIncludeGraph.Reset("test")
	s.workspaceIncludeGraph.Upsert(ownerPath, workspacepkg.SourceMetadata{FileName: ownerPath}, []string{targetPath}, "stale")
	s.workspaceIncludeGraphComplete = false
}
