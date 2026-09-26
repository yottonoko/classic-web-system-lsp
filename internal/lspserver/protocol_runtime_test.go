package lspserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"
)

func TestInitializeResultMatchesTypeScriptProtocolCapabilities(t *testing.T) {
	result := initializeResult()
	capabilities, ok := result["capabilities"].(map[string]any)
	if !ok {
		t.Fatalf("initialize capabilities = %#v", result["capabilities"])
	}
	codeActionProvider, ok := capabilities["codeActionProvider"].(map[string]any)
	if !ok {
		t.Fatalf("codeActionProvider = %#v", capabilities["codeActionProvider"])
	}
	wantKinds := []string{
		"quickfix",
		"refactor",
		"source",
		"source.organizeImports",
		"source.organizeImports.aspLsp.javascript",
	}
	if got := codeActionProvider["codeActionKinds"]; !equalJSONValue(got, wantKinds) {
		t.Fatalf("codeActionKinds = %#v, want %#v", got, wantKinds)
	}
	onType, ok := capabilities["documentOnTypeFormattingProvider"].(map[string]any)
	if !ok {
		t.Fatalf("documentOnTypeFormattingProvider = %#v", capabilities["documentOnTypeFormattingProvider"])
	}
	if got := onType["firstTriggerCharacter"]; got != "\n" {
		t.Fatalf("firstTriggerCharacter = %#v, want newline", got)
	}
	if got := onType["moreTriggerCharacter"]; !equalJSONValue(got, []string{">"}) {
		t.Fatalf("moreTriggerCharacter = %#v, want [>]", got)
	}
}

func TestServerPreservesRenameSettingsOnPartialConfiguration(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.settings.UpdateIncludesOnFileRename = true
	server.settings.WorkspaceSymbolRename = true
	params := mustRaw(map[string]any{
		"settings": map[string]any{
			"aspLsp": map[string]any{
				"rename": map[string]any{"workspaceSymbolRename": false},
			},
		},
	})
	if err := server.handleNotification(context.Background(), "workspace/didChangeConfiguration", params); err != nil {
		t.Fatal(err)
	}
	if !server.settings.UpdateIncludesOnFileRename {
		t.Fatal("partial rename configuration reset updateIncludesOnFileRename")
	}
	if server.settings.WorkspaceSymbolRename {
		t.Fatal("partial rename configuration did not update workspaceSymbolRename")
	}

	params = mustRaw(map[string]any{
		"settings": map[string]any{
			"aspLsp": map[string]any{
				"rename": map[string]any{"updateIncludesOnFileRename": false},
			},
		},
	})
	if err := server.handleNotification(context.Background(), "workspace/didChangeConfiguration", params); err != nil {
		t.Fatal(err)
	}
	if server.settings.UpdateIncludesOnFileRename {
		t.Fatal("partial rename configuration did not update updateIncludesOnFileRename")
	}
	if server.settings.WorkspaceSymbolRename {
		t.Fatal("partial rename configuration reset workspaceSymbolRename")
	}
}

func TestServerMergesPartialFormatAndInlaySettings(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.settings.FormatPrintWidth = 120
	server.settings.FormatTabSize = 4
	server.settings.InlayScopeMarkers = inlayScopeMarkerSettings{Global: true, Local: true, Uncertain: true}
	params := mustRaw(map[string]any{
		"settings": map[string]any{
			"aspLsp": map[string]any{
				"format":     map[string]any{"printWidth": 88},
				"inlayHints": map[string]any{"scopeMarkers": map[string]any{"global": false}},
			},
		},
	})
	if err := server.handleNotification(context.Background(), "workspace/didChangeConfiguration", params); err != nil {
		t.Fatal(err)
	}
	if server.settings.FormatPrintWidth != 88 || server.settings.FormatTabSize != 4 {
		t.Fatalf("partial format configuration lost existing values: %#v", server.settings)
	}
	if server.settings.InlayScopeMarkers != (inlayScopeMarkerSettings{Global: false, Local: true, Uncertain: true}) {
		t.Fatalf("partial inlay configuration lost existing values: %#v", server.settings.InlayScopeMarkers)
	}
}

func TestServerDecodesPublicParitySettingsAndPreservesZeroInvalidValues(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	params := mustRaw(map[string]any{
		"settings": map[string]any{
			"aspLsp": map[string]any{
				"diagnostics": map[string]any{"debounceMs": -1},
				"incremental": map[string]any{"mode": "legacy", "analysis": false},
				"workspace":   map[string]any{"scanChunkSize": 11, "busyAnalysisConcurrency": 2},
				"graph": map[string]any{
					"useReverseIncludeIndex": false, "workerSymbolExtraction": true,
				},
				"flowchart": map[string]any{"labelLineLength": 27},
			},
		},
	})
	if err := server.handleNotification(context.Background(), "workspace/didChangeConfiguration", params); err != nil {
		t.Fatal(err)
	}
	if server.settings.DiagnosticsDebounceMS != defaultDiagnosticsDebounceMS {
		t.Fatalf("invalid diagnostics debounce reset the setting: %d", server.settings.DiagnosticsDebounceMS)
	}
	if server.settings.IncrementalMode != "legacy" || server.settings.IncrementalAnalysis {
		t.Fatalf("incremental settings = %q/%v", server.settings.IncrementalMode, server.settings.IncrementalAnalysis)
	}
	if server.settings.WorkspaceScanChunkSize != 11 || server.settings.WorkspaceBusyAnalysisConcurrency != 2 {
		t.Fatalf("workspace settings = %d/%d", server.settings.WorkspaceScanChunkSize, server.settings.WorkspaceBusyAnalysisConcurrency)
	}
	if server.settings.GraphUseReverseIncludeIndex || !server.settings.GraphWorkerSymbolExtraction || server.settings.FlowchartLabelLineLength != 27 {
		t.Fatalf("graph/flowchart settings = %#v", server.settings)
	}
}

func TestSemanticTokenThresholdsMatchTypeScriptDefaults(t *testing.T) {
	if got := largeSourceSemanticTokenThreshold(); got != 1024*1024 {
		t.Fatalf("large source semantic threshold = %d, want 1 MiB", got)
	}
	if got := largeJavaScriptSemanticTokenThreshold(); got != 50*1024 {
		t.Fatalf("large JavaScript semantic threshold = %d, want 50 KiB", got)
	}
	t.Setenv("ASP_LSP_TEST_SEMANTIC_TOKENS_LARGE_SOURCE_THRESHOLD", "123")
	t.Setenv("ASP_LSP_TEST_SEMANTIC_TOKENS_LARGE_JAVASCRIPT_THRESHOLD", "456")
	if got := largeSourceSemanticTokenThreshold(); got != 123 {
		t.Fatalf("source semantic threshold override = %d", got)
	}
	if got := largeJavaScriptSemanticTokenThreshold(); got != 456 {
		t.Fatalf("JavaScript semantic threshold override = %d", got)
	}
}

func TestServerVisualRefreshHonorsClientCapabilities(t *testing.T) {
	outputReader, outputWriter := io.Pipe()
	defer outputReader.Close()
	defer outputWriter.Close()
	server := New(strings.NewReader(""), outputWriter, io.Discard)
	defer server.shutdownRuntimeCaches()
	if _, rpcErr := server.handleRequest(context.Background(), "initialize", mustRaw(map[string]any{
		"capabilities": map[string]any{
			"workspace": map[string]any{
				"semanticTokens": map[string]any{"refreshSupport": true},
				"inlayHint":      map[string]any{"refreshSupport": true},
				"codeLens":       map[string]any{"refreshSupport": true},
			},
		},
	})); rpcErr != nil {
		t.Fatalf("initialize failed: %#v", rpcErr)
	}
	refreshDone := make(chan struct{})
	go func() {
		server.requestVisualRefresh("test")
		close(refreshDone)
	}()

	reader := bufio.NewReader(outputReader)
	methods := make([]string, 0, 3)
	var codeLensRefresh *rpcMessage
	for len(methods) < 3 {
		message, err := readMessage(reader)
		if err != nil {
			t.Fatal(err)
		}
		methods = append(methods, message.Method)
		if message.Method == "workspace/codeLens/refresh" {
			codeLensRefresh = message
		}
	}
	if !containsProtocolString(methods, "workspace/semanticTokens/refresh") {
		t.Fatalf("visual refresh methods = %#v", methods)
	}
	if !containsProtocolString(methods, "workspace/inlayHint/refresh") {
		t.Fatalf("visual refresh methods = %#v", methods)
	}
	if !containsProtocolString(methods, "workspace/codeLens/refresh") {
		t.Fatalf("visual refresh methods = %#v", methods)
	}
	if codeLensRefresh == nil || codeLensRefresh.ID == nil {
		t.Fatalf("CodeLens refresh must be a client request, got %#v", codeLensRefresh)
	}
	server.deliverClientResponse(rpcMessage{ID: codeLensRefresh.ID, Result: json.RawMessage("null")})
	<-refreshDone
}

func TestServerCancelProgressTaskCancelsRegisteredTask(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	ctx := server.registerProgressCancellation("graph-task")
	result := server.executeCommand(executeCommandParams{
		Command:   "aspLsp.server.cancelProgressTask",
		Arguments: []any{map[string]any{"id": "graph-task"}},
	})
	if got := mustJSONText(t, result); got != `{"ok":true}` {
		t.Fatalf("cancelProgressTask result = %s", got)
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("cancelProgressTask did not cancel registered task")
	}
	if result := server.executeCommand(executeCommandParams{
		Command:   "aspLsp.server.cancelProgressTask",
		Arguments: []any{map[string]any{"id": "missing-task"}},
	}); mustJSONText(t, result) != `{"ok":false}` {
		t.Fatalf("missing cancelProgressTask result = %s", mustJSONText(t, result))
	}
}

func TestServerAcceptsShortCommandAliases(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	for _, command := range []string{
		"aspLsp.reindexWorkspace",
		"aspLsp.clearCache",
		"aspLsp.clearDiskCache",
		"aspLsp.clearProcessCache",
	} {
		result := server.executeCommand(executeCommandParams{Command: command})
		if result == nil {
			t.Fatalf("short command alias %q returned nil", command)
		}
		var object map[string]any
		if err := json.Unmarshal([]byte(mustJSONText(t, result)), &object); err != nil {
			t.Fatalf("short command alias %q result = %v: %v", command, result, err)
		}
		if object["ok"] != true {
			t.Fatalf("short command alias %q result = %#v", command, object)
		}
	}
}

func equalJSONValue(left, right any) bool {
	leftJSON, err := json.Marshal(left)
	if err != nil {
		return false
	}
	rightJSON, err := json.Marshal(right)
	if err != nil {
		return false
	}
	return bytes.Equal(leftJSON, rightJSON)
}

func containsProtocolString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
