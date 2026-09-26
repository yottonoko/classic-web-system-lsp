package lspserver

import (
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestDidOpenInitialSyntaxAndFullAnalysisShareOneParse(t *testing.T) {
	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "single-parse.asp"))
	server := newDocumentOpenCancellationTestServer(t, root, io.Discard)
	var parses atomic.Int32
	server.documentParseTestHook = func(string) { parses.Add(1) }

	if err := server.handleNotification(context.Background(), "textDocument/didOpen", mustRaw(map[string]any{
		"textDocument": map[string]any{
			"uri": uri, "languageId": "classic-asp", "version": 1,
			"text": "<%\nIf True Then\nResponse.Write missingName\n%>",
		},
	})); err != nil {
		t.Fatal(err)
	}
	waitForDocumentOpenAnalysisWorkers(t, server)

	if got := parses.Load(); got != 1 {
		t.Fatalf("didOpen syntax parses = %d, want one parse shared by initial syntax and full analysis", got)
	}
	if manifest := workspaceArtifactForURI(server, uri); manifest == nil {
		t.Fatal("full document-open analysis did not publish a workspace artifact")
	}
}

func TestDidCloseCancelsBlockedDocumentOpenAnalysisWithoutRestoringStaleState(t *testing.T) {
	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "closed.asp"))
	output := &documentOpenDiagnosticRecorder{}
	server := newDocumentOpenCancellationTestServer(t, root, output)
	started, release := blockFirstDocumentOpenSnapshot(t, server)

	if err := server.handleNotification(context.Background(), "textDocument/didOpen", mustRaw(map[string]any{
		"textDocument": map[string]any{
			"uri":        uri,
			"languageId": "classic-asp",
			"version":    1,
			"text":       "<%\nResponse.Write missingName",
		},
	})); err != nil {
		t.Fatal(err)
	}
	waitForDocumentOpenSnapshotStart(t, started)
	if manifest := workspaceArtifactForURI(server, uri); manifest != nil {
		t.Fatal("document-open artifact was published while its snapshot build was blocked")
	}

	if err := server.handleNotification(context.Background(), "textDocument/didClose", mustRaw(map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})); err != nil {
		t.Fatal(err)
	}
	afterClose := output.count()
	release()
	waitForDocumentOpenAnalysisWorkers(t, server)

	if manifest := workspaceArtifactForURI(server, uri); manifest != nil {
		t.Fatalf("cancelled document-open analysis restored a stale artifact: %#v", manifest)
	}
	for _, published := range output.since(afterClose) {
		if published.URI == uri && len(published.Diagnostics) > 0 {
			t.Fatalf("cancelled document-open analysis published diagnostics after didClose: %#v", published)
		}
	}
}

func TestDidChangeCancelsBlockedDocumentOpenDiagnosticsForPreviousVersion(t *testing.T) {
	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "changed.asp"))
	output := &documentOpenDiagnosticRecorder{}
	server := newDocumentOpenCancellationTestServer(t, root, output)
	started, release := blockFirstDocumentOpenSnapshot(t, server)

	if err := server.handleNotification(context.Background(), "textDocument/didOpen", mustRaw(map[string]any{
		"textDocument": map[string]any{
			"uri":        uri,
			"languageId": "classic-asp",
			"version":    1,
			"text":       "<%\nResponse.Write missingName",
		},
	})); err != nil {
		t.Fatal(err)
	}
	waitForDocumentOpenSnapshotStart(t, started)

	changedText := "<% Dim currentValue : currentValue = 2 %>"
	if err := server.handleNotification(context.Background(), "textDocument/didChange", mustRaw(map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 2},
		"contentChanges": []map[string]any{{
			"text": changedText,
		}},
	})); err != nil {
		t.Fatal(err)
	}
	afterChange := output.count()
	release()
	waitForDocumentOpenAnalysisWorkers(t, server)
	server.cancelScheduledDiagnostics(uri)

	manifest := workspaceArtifactForURI(server, uri)
	if manifest == nil {
		t.Fatal("didChange did not publish the current workspace artifact")
	}
	if got, want := manifest.SourceFingerprint, workspaceFingerprint(changedText); got != want {
		t.Fatalf("workspace artifact fingerprint = %q, want current source %q", got, want)
	}
	for _, published := range output.since(afterChange) {
		if published.URI == uri && published.Version != nil && *published.Version == 1 {
			t.Fatalf("cancelled document-open analysis published version 1 diagnostics after didChange: %#v", published)
		}
	}
}

func TestDocumentChangeAnalysisPublishesOnlyLatestRevision(t *testing.T) {
	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "latest.asp"))
	server := newDocumentOpenCancellationTestServer(t, root, io.Discard)
	var builds atomic.Int32
	var active atomic.Int32
	var maxActive atomic.Int32
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	var releaseOnce sync.Once
	server.fileAnalysisSnapshotTestHook = func() {
		current := active.Add(1)
		for {
			previous := maxActive.Load()
			if current <= previous || maxActive.CompareAndSwap(previous, current) {
				break
			}
		}
		build := builds.Add(1)
		if build == 1 {
			close(firstStarted)
			<-releaseFirst
		}
		active.Add(-1)
	}
	release := func() { releaseOnce.Do(func() { close(releaseFirst) }) }
	t.Cleanup(release)

	setCurrent := func(version int, text string) *core.ParsedDocument {
		document := core.NewTextDocument(uri, "classic-asp", version, text)
		parsed := core.ParseDocument(uri, text, core.Settings{DefaultLanguage: server.settings.DefaultLanguage})
		server.mu.Lock()
		server.rememberOpenDocumentLocked(uri, document)
		server.mu.Unlock()
		return parsed
	}
	firstText := "<% Dim firstValue : firstValue = 1 %>"
	first := core.NewTextDocument(uri, "classic-asp", 1, firstText)
	firstParsed := core.ParseDocument(uri, firstText, core.Settings{DefaultLanguage: server.settings.DefaultLanguage})
	server.mu.Lock()
	server.rememberOpenDocumentLocked(uri, first)
	server.mu.Unlock()
	server.scheduleDocumentChangeAnalysis(first, firstParsed)
	waitForDocumentOpenSnapshotStart(t, firstStarted)

	secondText := "<% Dim secondValue : secondValue = 2 %>"
	secondParsed := setCurrent(2, secondText)
	server.scheduleDocumentChangeAnalysis(core.NewTextDocument(uri, "classic-asp", 2, secondText), secondParsed)
	latestText := "<% Dim latestValue : latestValue = 3 %>"
	latestParsed := setCurrent(3, latestText)
	server.scheduleDocumentChangeAnalysis(core.NewTextDocument(uri, "classic-asp", 3, latestText), latestParsed)
	release()
	waitForDocumentOpenAnalysisWorkers(t, server)

	manifest := workspaceArtifactForURI(server, uri)
	if manifest == nil || manifest.SourceFingerprint != workspaceFingerprint(latestText) {
		t.Fatalf("latest artifact = %#v, want source fingerprint for version 3", manifest)
	}
	if got := builds.Load(); got != 2 {
		t.Fatalf("effective artifact snapshot builds = %d, want first and latest only", got)
	}
	if got := maxActive.Load(); got != 1 {
		t.Fatalf("concurrent artifact snapshot workers = %d, want one per URI", got)
	}
}

func TestDocumentChangeAnalysisKeepsInteractiveFeaturesOnCurrentParsedRevision(t *testing.T) {
	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "interactive.asp"))
	server := newDocumentOpenCancellationTestServer(t, root, io.Discard)
	oldText := "<% Response.Write oldValue %>"
	old := core.NewTextDocument(uri, "classic-asp", 1, oldText)
	oldParsed := core.ParseDocument(uri, oldText, core.Settings{DefaultLanguage: server.settings.DefaultLanguage})
	server.mu.Lock()
	server.rememberOpenDocumentLocked(uri, old)
	server.parsedCache[parsedDocumentCacheKey(uri)] = parsedDocumentCacheEntry{
		Version: 1, Text: oldText, DefaultLanguage: server.settings.DefaultLanguage, Parsed: oldParsed,
	}
	server.mu.Unlock()
	started, release := blockFirstDocumentOpenSnapshot(t, server)
	if err := server.handleNotification(context.Background(), "textDocument/didChange", mustRaw(map[string]any{
		"textDocument":   map[string]any{"uri": uri, "version": 2},
		"contentChanges": []map[string]any{{"text": "<% Dim latestValue : Request.Write latestValue %>"}},
	})); err != nil {
		t.Fatal(err)
	}
	waitForDocumentOpenSnapshotStart(t, started)

	currentText := "<% Dim latestValue : Request.Write latestValue %>"
	document, parsed := server.parsed(uri)
	if document == nil || parsed == nil || document.Version != 2 || document.Text != currentText || parsed.Text != currentText {
		t.Fatalf("interactive revision = document:%#v parsed:%#v, want version 2", document, parsed)
	}
	latestOffset := strings.Index(currentText, "latestValue")
	completions := server.completion(context.Background(), uri, document.PositionAt(latestOffset+len("latest")), nil)
	if !completionHasLabel(completions.Items, "latestValue") {
		t.Fatalf("completion did not use current parsed revision: %#v", completions.Items)
	}
	requestOffset := strings.Index(currentText, "Request")
	hover, ok := server.hover(uri, document.PositionAt(requestOffset)).(*lsp.Hover)
	if !ok || hover == nil {
		t.Fatalf("hover for current revision = %#v", hover)
	}
	encoded, err := json.Marshal(hover)
	if err != nil || !strings.Contains(strings.ToLower(string(encoded)), "request object") {
		t.Fatalf("hover did not use current parsed revision: %s", encoded)
	}
	release()
	waitForDocumentOpenAnalysisWorkers(t, server)
}

type recordedDocumentOpenDiagnostics struct {
	URI         string
	Version     *int
	Diagnostics []lsp.Diagnostic
}

type documentOpenDiagnosticRecorder struct {
	mu        sync.Mutex
	published []recordedDocumentOpenDiagnostics
}

func (r *documentOpenDiagnosticRecorder) Write(encoded []byte) (int, error) {
	headerEnd := -1
	for index := 0; index+3 < len(encoded); index++ {
		if string(encoded[index:index+4]) == "\r\n\r\n" {
			headerEnd = index
			break
		}
	}
	if headerEnd < 0 {
		return len(encoded), nil
	}
	var message rpcMessage
	if err := json.Unmarshal(encoded[headerEnd+4:], &message); err != nil {
		return 0, err
	}
	if message.Method != "textDocument/publishDiagnostics" {
		return len(encoded), nil
	}
	var published recordedDocumentOpenDiagnostics
	if err := json.Unmarshal(message.Params, &published); err != nil {
		return 0, err
	}
	r.mu.Lock()
	r.published = append(r.published, published)
	r.mu.Unlock()
	return len(encoded), nil
}

func (r *documentOpenDiagnosticRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.published)
}

func (r *documentOpenDiagnosticRecorder) since(index int) []recordedDocumentOpenDiagnostics {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]recordedDocumentOpenDiagnostics(nil), r.published[index:]...)
}

func newDocumentOpenCancellationTestServer(t *testing.T, root string, output io.Writer) *Server {
	t.Helper()
	server := New(nil, output, io.Discard)
	server.rootPath = root
	server.rootURI = pathToFileURI(root)
	server.settings.CacheEnabled = false
	server.configureDiskAnalysisCache()
	t.Cleanup(server.shutdownRuntimeCaches)
	return server
}

func blockFirstDocumentOpenSnapshot(t *testing.T, server *Server) (<-chan struct{}, func()) {
	t.Helper()
	started := make(chan struct{})
	release := make(chan struct{})
	var first sync.Once
	var releaseOnce sync.Once
	server.fileAnalysisSnapshotTestHook = func() {
		blocked := false
		first.Do(func() {
			blocked = true
			close(started)
		})
		if blocked {
			<-release
		}
	}
	releaseSnapshot := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseSnapshot)
	return started, releaseSnapshot
}

func waitForDocumentOpenSnapshotStart(t *testing.T, started <-chan struct{}) {
	t.Helper()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("document-open snapshot build did not start")
	}
}

func waitForDocumentOpenAnalysisWorkers(t *testing.T, server *Server) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		server.documentOpenAnalysisWorkers.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("document-open analysis worker did not stop after cancellation")
	}
}

func workspaceArtifactForURI(server *Server, uri string) *workspaceDocumentArtifactManifest {
	server.mu.Lock()
	defer server.mu.Unlock()
	return server.workspaceArtifacts[workspaceDocumentIDFromURI(uri)]
}
