package lspserver

import (
	"encoding/json"
	"io"
	"math"
	"runtime"
	"strconv"
	"sync"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

func TestAnalysisWorkersEnvOverride(t *testing.T) {
	t.Setenv("ASP_LSP_ANALYSIS_WORKERS", "2")
	server := New(nil, io.Discard, nil)
	if got := server.analysisWorkers.workers; got != 2 {
		t.Fatalf("analysis workers = %d, want 2", got)
	}

	t.Setenv("ASP_LSP_ANALYSIS_WORKERS", "0")
	server = New(nil, io.Discard, nil)
	if got := server.analysisWorkers.workers; got != 1 {
		t.Fatalf("analysis workers = %d, want 1", got)
	}
}

func TestAnalysisWorkersDefaultToLogicalCPUCountAndRetainReasonableExplicitValues(t *testing.T) {
	t.Setenv("ASP_LSP_ANALYSIS_WORKERS", "")
	server := New(nil, io.Discard, nil)
	if got, want := server.analysisWorkers.workerCount(), boundedAnalysisWorkers(runtime.NumCPU()); got != want {
		t.Fatalf("default analysis workers = %d, want logical CPU count %d", got, want)
	}
	server.analysisWorkers.setWorkers(48)
	if got := server.analysisWorkers.workerCount(); got != 48 {
		t.Fatalf("explicit analysis workers = %d, want 48", got)
	}
}

func TestAnalysisWorkersBoundHugeEnvironmentValueBeforeAllocation(t *testing.T) {
	t.Setenv("ASP_LSP_ANALYSIS_WORKERS", strconv.Itoa(math.MaxInt))
	server := New(nil, io.Discard, nil)
	if got := server.analysisWorkers.workerCount(); got != maxAnalysisWorkers {
		t.Fatalf("environment analysis workers = %d, want bound %d", got, maxAnalysisWorkers)
	}
	if got := cap(server.analysisWorkers.slots); got != maxAnalysisWorkers+1 {
		t.Fatalf("analysis worker slots capacity = %d, want %d", got, maxAnalysisWorkers+1)
	}
	if got := cap(server.analysisWorkers.bulkSlots); got != maxAnalysisWorkers {
		t.Fatalf("bulk analysis worker slots capacity = %d, want %d", got, maxAnalysisWorkers)
	}
}

func TestAnalysisWorkersBoundHugeWorkspaceValueBeforeAllocation(t *testing.T) {
	t.Setenv("ASP_LSP_ANALYSIS_WORKERS", "")
	server := New(nil, io.Discard, nil)
	server.settings.WorkspaceBusyAnalysisConcurrency = math.MaxInt
	server.configureAnalysisWorkers()
	if got := server.analysisWorkers.workerCount(); got != maxAnalysisWorkers {
		t.Fatalf("workspace analysis workers = %d, want bound %d", got, maxAnalysisWorkers)
	}
	if got := cap(server.analysisWorkers.slots); got != maxAnalysisWorkers+1 {
		t.Fatalf("analysis worker slots capacity = %d, want %d", got, maxAnalysisWorkers+1)
	}
	if got := cap(server.analysisWorkers.bulkSlots); got != maxAnalysisWorkers {
		t.Fatalf("bulk analysis worker slots capacity = %d, want %d", got, maxAnalysisWorkers)
	}
}

func TestParallelDiagnosticsMatchSequentialDiagnostics(t *testing.T) {
	uri := "file:///parallel/default.asp"
	source := `<style>.broken { color: }</style>
<script>var clientValue = ;</script>
<%
Option Explicit
Dim title
Response.Write title
%>`
	sequential := New(nil, io.Discard, nil)
	sequential.analysisWorkers = &analysisWorkerPool{workers: 1}
	sequential.settings.CheckJS = true
	sequential.documents[uri] = core.NewTextDocument(uri, "classic-asp", 1, source)

	parallel := New(nil, io.Discard, nil)
	parallel.analysisWorkers = &analysisWorkerPool{workers: 4}
	parallel.settings.CheckJS = true
	parallel.documents[uri] = core.NewTextDocument(uri, "classic-asp", 1, source)

	want := mustJSONForParallelTest(t, sequential.diagnostics(uri))
	got := mustJSONForParallelTest(t, parallel.diagnostics(uri))
	if got != want {
		t.Fatalf("parallel diagnostics mismatch\n got: %s\nwant: %s", got, want)
	}
}

func TestParallelWorkspaceGraphMatchesSequentialWorkspaceGraph(t *testing.T) {
	sequential := parallelGraphTestServer(1)
	parallel := parallelGraphTestServer(4)

	want := mustJSONForParallelTest(t, sequential.buildWorkspaceGraph(false))
	got := mustJSONForParallelTest(t, parallel.buildWorkspaceGraph(false))
	if got != want {
		t.Fatalf("parallel workspace graph mismatch\n got: %s\nwant: %s", got, want)
	}
}

func TestConcurrentSemanticTokensFullRequestsMatch(t *testing.T) {
	uri := "file:///parallel/semantic.asp"
	source := `<%
Dim title
title = "Dashboard"
Response.Write title
%>`
	server := New(nil, io.Discard, nil)
	server.analysisWorkers = &analysisWorkerPool{workers: 4}
	server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 1, source)

	const requests = 8
	results := make([]string, requests)
	var wg sync.WaitGroup
	wg.Add(requests)
	for index := 0; index < requests; index++ {
		go func(index int) {
			defer wg.Done()
			results[index] = mustJSONForParallelTest(t, server.semanticTokens(uri))
		}(index)
	}
	wg.Wait()
	for index := 1; index < len(results); index++ {
		if results[index] != results[0] {
			t.Fatalf("semantic token response %d mismatch\n got: %s\nwant: %s", index, results[index], results[0])
		}
	}
}

func parallelGraphTestServer(workers int) *Server {
	server := New(nil, io.Discard, nil)
	server.analysisWorkers = &analysisWorkerPool{workers: workers}
	server.workspace["file:///parallel/a.asp"] = core.NewTextDocument("file:///parallel/a.asp", "classic-asp", 1, `<%
Function SharedTitle()
  SharedTitle = "A"
End Function
Response.Write SharedTitle()
%>`)
	server.workspace["file:///parallel/b.asp"] = core.NewTextDocument("file:///parallel/b.asp", "classic-asp", 1, `<%
Function LocalTitle()
  LocalTitle = SharedTitle()
End Function
%>`)
	return server
}

func mustJSONForParallelTest(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
