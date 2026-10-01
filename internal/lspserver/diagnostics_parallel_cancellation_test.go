package lspserver

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestDiagnosticsCancellationDoesNotPoisonDiskCache(t *testing.T) {
	server, uri, source := newDiagnosticsCancellationTestServer(t)
	server.settings.VBScriptTypeChecking = "strict"

	base, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := &diagnosticsCancellationContext{Context: base, cancel: cancel, limit: 1000}
	first := server.diagnosticsSnapshot(ctx, uri)
	if first.ok {
		t.Fatalf("cancelled diagnostics snapshot = %#v, want incomplete snapshot", first)
	}
	server.waitForAsyncDiskCacheWrites()
	if _, ok := server.diskCacheForUse().ReadFileBundle(server.diagnosticsDiskLookup(server.documents[uri], core.ParseDocument(uri, source, core.Settings{DefaultLanguage: "VBScript"}))); ok {
		t.Fatal("cancelled diagnostics snapshot populated the disk cache")
	}

	second := server.diagnosticsSnapshot(context.Background(), uri)
	if !second.ok {
		t.Fatalf("unchanged diagnostics snapshot = %#v, want complete snapshot", second)
	}
	if !diagnosticsHasSource(second.diagnostics, vbscriptTypeDiagnosticSource) {
		t.Fatalf("unchanged diagnostics snapshot lost strict type diagnostics: %#v", second.diagnostics)
	}
}

func TestLanguageDiagnosticsOwnerCacheTracksCompletedItems(t *testing.T) {
	const key = "test.language-diagnostics-owner-generation"
	parsed := core.ParseDocument("file:///diagnostics-owner-generation.asp", "<main></main>", core.Settings{})
	candidate := &languageDiagnosticsCache{}
	parsed.LoadOrStoreRuntimeAnalysis(key, candidate)
	before := runtimeOwnerTotal(parsed)

	items := diagnosticsForEmbeddedLanguage(parsed, core.LanguageHTML, key, func() []lsp.Diagnostic {
		return []lsp.Diagnostic{{Source: "html", Message: strings.Repeat("diagnostic", 64)}}
	})
	after := runtimeOwnerTotal(parsed)
	if len(items) != 1 || after <= before {
		t.Fatalf("completed diagnostics owner bytes = %d, before=%d items=%#v", after, before, items)
	}
}

func runtimeOwnerTotal(parsed *core.ParsedDocument) int64 {
	var total int64
	for _, owner := range parsed.RuntimeAnalysisMemoryOwners() {
		total += owner.Bytes
	}
	return total
}

func TestDiagnosticsSnapshotRejectsCancellationBeforeAndAfterWorkers(t *testing.T) {
	server, uri, _ := newDiagnosticsCancellationTestServer(t)
	var output bytes.Buffer
	server.out = &output
	server.settings.DebugOutput = "verbose"

	before, cancelBefore := context.WithCancel(context.Background())
	cancelBefore()
	if snapshot := server.diagnosticsSnapshot(before, uri); snapshot.ok {
		t.Fatalf("pre-cancelled diagnostics snapshot = %#v, want incomplete snapshot", snapshot)
	}

	base, cancelAfter := context.WithCancel(context.Background())
	var once sync.Once
	progress := func(_ string, current, _ int) {
		if current >= 14 {
			once.Do(cancelAfter)
		}
	}
	snapshot := server.diagnosticsSnapshotWithProgress(base, uri, progress)
	if snapshot.ok {
		t.Fatalf("post-worker-cancelled diagnostics snapshot = %#v, want incomplete snapshot", snapshot)
	}
	logs := output.String()
	for _, expected := range []string{"check.diagnostics.started", "check.diagnostics.cancelled"} {
		if !strings.Contains(logs, expected) {
			t.Fatalf("cancelled diagnostics logs missing %q: %s", expected, logs)
		}
	}
}

func TestCancelledWorkspaceDiagnosticsLogsCancelledTerminalState(t *testing.T) {
	server, _, _ := newDiagnosticsCancellationTestServer(t)
	var output bytes.Buffer
	server.out = &output
	server.settings.DebugOutput = "summary"
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	server.workspaceDiagnostics(ctx, nil)

	logs := output.String()
	for _, expected := range []string{"vbscript.worker.started", "vbscript.worker.cancelled"} {
		if !strings.Contains(logs, expected) {
			t.Fatalf("cancelled workspace diagnostics logs missing %q: %s", expected, logs)
		}
	}
	if strings.Contains(logs, "vbscript.worker.completed") {
		t.Fatalf("cancelled workspace diagnostics reported successful completion: %s", logs)
	}
}

func TestIncludeDiagnosticsContextRejectsLargeCancelledScan(t *testing.T) {
	server, uri, _ := newDiagnosticsCancellationTestServer(t)
	_, parsed := server.parsed(uri)
	if parsed == nil {
		t.Fatal("cancellation test document was not parsed")
	}

	base, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := &diagnosticsCancellationContext{Context: base, cancel: cancel, limit: 100}
	diagnostics, complete := server.includeDiagnosticsContext(ctx, parsed)
	if complete || diagnostics != nil {
		t.Fatalf("large cancelled include diagnostics = (%#v, %t), want nil and incomplete", diagnostics, complete)
	}
	if ctx.Err() == nil {
		t.Fatal("large include scan did not observe cancellation")
	}
}

func TestDiagnosticsCancellationDoesNotLeaveWorkerGoroutines(t *testing.T) {
	server, uri, _ := newDiagnosticsCancellationTestServer(t)
	server.settings.VBScriptTypeChecking = "strict"

	for range 3 {
		base, cancel := context.WithCancel(context.Background())
		ctx := &diagnosticsCancellationContext{Context: base, cancel: cancel, limit: 1000}
		if snapshot := server.diagnosticsSnapshot(ctx, uri); snapshot.ok {
			t.Fatal("cancelled diagnostics unexpectedly completed")
		}
		cancel()
	}

	probeContext, cancelProbe := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelProbe()
	probeDone := make(chan struct{})
	go func() {
		server.analysisWorkers.parallelForBulk(probeContext, 1, func(context.Context, int) {})
		close(probeDone)
	}()
	select {
	case <-probeDone:
	case <-probeContext.Done():
		t.Fatal("diagnostics cancellation left an analysis worker slot occupied")
	}
}

type diagnosticsCancellationContext struct {
	context.Context
	cancel context.CancelFunc
	checks atomic.Int64
	limit  int64
}

func (c *diagnosticsCancellationContext) Err() error {
	if c.checks.Add(1) >= c.limit {
		c.cancel()
	}
	return c.Context.Err()
}

func newDiagnosticsCancellationTestServer(t *testing.T) (*Server, string, string) {
	t.Helper()
	root := t.TempDir()
	ownerPath := filepath.Join(root, "default.asp")
	includePath := filepath.Join(root, "customer.inc")
	includeSource := `<%
Class Customer
  Public Name
End Class
%>`
	const includeCount = 1200
	source := strings.Repeat(`<!-- #include file="customer.inc" -->
`, includeCount) + `<%
Dim customer
Set customer = New Customer
customer.Missing
%>`
	if err := os.WriteFile(includePath, []byte(includeSource), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ownerPath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}

	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
	server.settings.CacheEnabled = true
	server.settings.CacheDirectory = filepath.Join(root, "cache")
	server.settings.CacheTTLHours = 24
	server.settings.CacheMaxSizeMB = 16
	server.configureFsGateway()
	server.configureDiskAnalysisCache()
	server.documents[filePathURI(ownerPath)] = core.NewTextDocument(filePathURI(ownerPath), "classic-asp", 1, source)
	t.Cleanup(func() {
		server.closeDiskAnalysisCache()
	})
	return server, filePathURI(ownerPath), source
}

func diagnosticsHasSource(diagnostics []lsp.Diagnostic, source string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Source == source {
			return true
		}
	}
	return false
}
