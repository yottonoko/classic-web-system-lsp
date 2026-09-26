package lspserver

import (
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func BenchmarkClassicASPLSPDidOpenColdStagedDiagnostics(b *testing.B) {
	benchmarkClassicASPLSPDidOpenColdStagedDiagnostics(b, 0)
}

func BenchmarkClassicASPLSPDidOpenColdStagedDiagnosticsWorkspace100(b *testing.B) {
	benchmarkClassicASPLSPDidOpenColdStagedDiagnostics(b, 100)
}

func benchmarkClassicASPLSPDidOpenColdStagedDiagnostics(b *testing.B, workspaceFiles int) {
	root := b.TempDir()
	source := benchmarkClassicASPLSPDocument(500) + "\n<%\nResponse.Write missingName"
	workspaceSource := benchmarkClassicASPLSPDocument(20)
	var firstSyntaxTotal time.Duration
	var handlerReturnTotal time.Duration
	var finalDiagnosticsTotal time.Duration
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		observer := &diagnosticLatencyObserver{}
		server := New(nil, observer, nil)
		server.rootPath = root
		for workspaceIndex := 0; workspaceIndex < workspaceFiles; workspaceIndex++ {
			workspaceURI := pathToFileURI(filepath.Join(root, "workspace-"+strconv.Itoa(workspaceIndex)+".asp"))
			server.workspace[workspaceURI] = core.NewTextDocument(workspaceURI, "classic-asp", 1, workspaceSource)
		}
		uri := pathToFileURI(filepath.Join(root, "cold-open-"+strconv.Itoa(i)+".asp"))
		params := mustRaw(map[string]any{
			"textDocument": map[string]any{
				"uri":        uri,
				"languageId": "classic-asp",
				"version":    1,
				"text":       source,
			},
		})
		observer.start()
		if err := server.handleNotification(context.Background(), "textDocument/didOpen", params); err != nil {
			b.Fatal(err)
		}
		handlerReturnTotal += time.Since(observer.started())
		server.documentOpenAnalysisWorkers.Wait()
		firstSyntax, finalDiagnostics, publishCount := observer.result()
		if firstSyntax <= 0 {
			b.Fatal("cold didOpen did not publish parser diagnostics")
		}
		if finalDiagnostics < firstSyntax || publishCount < 2 {
			b.Fatalf("cold didOpen did not publish staged diagnostics: first=%s final=%s publishes=%d", firstSyntax, finalDiagnostics, publishCount)
		}
		firstSyntaxTotal += firstSyntax
		finalDiagnosticsTotal += finalDiagnostics
		b.StopTimer()
		server.shutdownRuntimeCaches()
		b.StartTimer()
	}
	b.ReportMetric(float64(firstSyntaxTotal.Microseconds())/float64(b.N), "first-syntax-us/op")
	b.ReportMetric(float64(handlerReturnTotal.Microseconds())/float64(b.N), "handler-return-us/op")
	b.ReportMetric(float64(finalDiagnosticsTotal.Microseconds())/float64(b.N), "final-diagnostics-us/op")
}

func TestColdDidOpenDiagnosticsLatencyObserverSeparatesFirstSyntaxFromFinal(t *testing.T) {
	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "staged.asp"))
	observer := &diagnosticLatencyObserver{}
	server := New(nil, observer, nil)
	server.rootPath = root
	observer.start()
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
	server.documentOpenAnalysisWorkers.Wait()
	firstSyntax, finalDiagnostics, publishCount := observer.result()
	if firstSyntax <= 0 {
		t.Fatal("parser diagnostics were not observed")
	}
	if publishCount < 2 || finalDiagnostics < firstSyntax {
		t.Fatalf("diagnostic stages were not separated: first=%s final=%s publishes=%d", firstSyntax, finalDiagnostics, publishCount)
	}
}

func TestDidOpenPublishesUTF16SyntaxBeforeBackgroundAnalysisAndKeepsRequestsUsable(t *testing.T) {
	root := t.TempDir()
	uri := pathToFileURI(filepath.Join(root, "syntax-first.asp"))
	source := "<% Response.Write \"😀\" : Dim initialized = 1\n%>"
	observer := &diagnosticLatencyObserver{}
	server := New(nil, observer, nil)
	server.rootPath = root
	started := make(chan struct{})
	release := make(chan struct{})
	var startedOnce sync.Once
	var releaseOnce sync.Once
	server.fileAnalysisSnapshotTestHook = func() {
		startedOnce.Do(func() { close(started) })
		<-release
	}
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(release) })
		server.stopDocumentOpenAnalysisWorkers()
	})

	observer.start()
	if err := server.handleNotification(context.Background(), "textDocument/didOpen", mustRaw(map[string]any{
		"textDocument": map[string]any{
			"uri": uri, "languageId": "classic-asp", "version": 1, "text": source,
		},
	})); err != nil {
		t.Fatal(err)
	}
	initial := observer.firstPublish(t)
	if initial.Version != 1 {
		t.Fatalf("initial diagnostics version = %d, want 1", initial.Version)
	}
	var initialized *lsp.Diagnostic
	for index := range initial.Diagnostics {
		if initial.Diagnostics[index].Code == "initializedDeclaration" {
			initialized = &initial.Diagnostics[index]
			break
		}
	}
	if initialized == nil {
		t.Fatalf("initial diagnostics = %#v, want initializedDeclaration", initial.Diagnostics)
	}
	doc := core.NewTextDocument(uri, "classic-asp", 1, source)
	startOffset := strings.Index(source, "Dim initialized")
	wantRange := doc.Range(startOffset, strings.Index(source, "\n"))
	if initialized.Range != wantRange {
		t.Fatalf("UTF-16 diagnostic range = %#v, want %#v", initialized.Range, wantRange)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("background analysis did not start")
	}

	requestDone := make(chan *rpcError, 1)
	go func() {
		_, rpcErr := server.handleRequest(context.Background(), "textDocument/foldingRange", mustRaw(map[string]any{
			"textDocument": map[string]any{"uri": uri},
		}))
		requestDone <- rpcErr
	}()
	select {
	case rpcErr := <-requestDone:
		if rpcErr != nil {
			t.Fatalf("folding request failed while background analysis was blocked: %#v", rpcErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("folding request waited for background document analysis")
	}

	releaseOnce.Do(func() { close(release) })
	server.documentOpenAnalysisWorkers.Wait()
	_, _, publishCount := observer.result()
	if publishCount < 2 {
		t.Fatalf("diagnostic publish count = %d, want initial syntax and final diagnostics", publishCount)
	}
}

type observedDiagnosticPublish struct {
	Version     int              `json:"version"`
	Diagnostics []lsp.Diagnostic `json:"diagnostics"`
}

type diagnosticLatencyObserver struct {
	mu               sync.Mutex
	startedAt        time.Time
	firstSyntax      time.Duration
	finalDiagnostics time.Duration
	publishCount     int
	publishes        []observedDiagnosticPublish
}

func (o *diagnosticLatencyObserver) start() {
	o.mu.Lock()
	o.startedAt = time.Now()
	o.mu.Unlock()
}

func (o *diagnosticLatencyObserver) Write(encoded []byte) (int, error) {
	headerEnd := strings.Index(string(encoded), "\r\n\r\n")
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
	var published observedDiagnosticPublish
	if err := json.Unmarshal(message.Params, &published); err != nil {
		return 0, err
	}
	now := time.Now()
	o.mu.Lock()
	defer o.mu.Unlock()
	o.publishCount++
	o.publishes = append(o.publishes, published)
	elapsed := now.Sub(o.startedAt)
	o.finalDiagnostics = elapsed
	if o.firstSyntax == 0 && containsParserDiagnostic(message.Params) {
		o.firstSyntax = elapsed
	}
	return len(encoded), nil
}

func (o *diagnosticLatencyObserver) firstPublish(t *testing.T) observedDiagnosticPublish {
	t.Helper()
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.publishes) == 0 {
		t.Fatal("no diagnostics were published")
	}
	return o.publishes[0]
}

func (o *diagnosticLatencyObserver) started() time.Time {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.startedAt
}

func containsParserDiagnostic(params json.RawMessage) bool {
	var published struct {
		Diagnostics []struct {
			Source string `json:"source"`
		} `json:"diagnostics"`
	}
	if err := json.Unmarshal(params, &published); err != nil {
		return false
	}
	for _, diagnostic := range published.Diagnostics {
		if diagnostic.Source == "asp-lsp-go" {
			return true
		}
	}
	return false
}

func (o *diagnosticLatencyObserver) result() (time.Duration, time.Duration, int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.firstSyntax, o.finalDiagnostics, o.publishCount
}

func BenchmarkInitialDiagnosticsChecks(b *testing.B) {
	server := New(nil, io.Discard, nil)
	defer server.shutdownRuntimeCaches()
	source := benchmarkClassicASPLSPDocument(500)
	server.documents["file:///cold-checks.asp"] = core.NewTextDocument("file:///cold-checks.asp", "classic-asp", 1, source)
	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		parsed := core.ParseDocument("file:///cold-checks.asp", source, core.Settings{DefaultLanguage: "VBScript"})
		b.StartTimer()
		diagnostics, _, complete := server.diagnosticsForParsedWithProgressResult(context.Background(), parsed, 0, 12, nil)
		if !complete {
			b.Fatal("cold diagnostics did not complete")
		}
		lspPerformanceBenchmarkSink = diagnostics
	}
}
