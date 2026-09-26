package lspserver

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestInboundLSPLoggingIncludesLifecycleMetadataWithoutSourceText(t *testing.T) {
	var output bytes.Buffer
	server := New(nil, &output, io.Discard)
	defer server.closeDiskAnalysisCache()
	server.settings.DebugOutput = "summary"
	const sourceSentinel = "SECRET_SOURCE_TEXT_MUST_NOT_BE_LOGGED"
	params, err := json.Marshal(map[string]any{
		"textDocument": map[string]any{
			"uri": "file:///workspace/default.asp", "version": 7, "text": sourceSentinel,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	receivedAt := time.Now()
	server.logInboundLSPEvent(&rpcMessage{Method: "textDocument/didOpen", Params: params}, "notification", receivedAt)
	server.logCompletedLSPEvent(nil, "textDocument/didOpen", "notification", receivedAt, nil, nil)

	logged := output.String()
	for _, expected := range []string{
		"lsp.notification.received", "lsp.notification.completed", "method=textDocument/didOpen",
		"uri=file:///workspace/default.asp", "version=7", "paramsBytes=", "durationMs=", "status=ok",
	} {
		if !strings.Contains(logged, expected) {
			t.Fatalf("lifecycle log missing %q: %s", expected, logged)
		}
	}
	if strings.Contains(logged, sourceSentinel) {
		t.Fatalf("lifecycle log leaked source text: %s", logged)
	}
}

func TestServeLifecycleLogsPairConcurrentRequestsAndClientResponsesWithoutPayloads(t *testing.T) {
	const sourceSentinel = "SECRET_DID_CHANGE_TEXT_MUST_NOT_BE_LOGGED"
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	var server *Server
	client := startStdioTestClientWithServer(t, func(configured *Server) {
		server = configured
		configured.settings.DebugOutput = "summary"
		configured.requestDispatchTestHook = func(ctx context.Context, method string) {
			if method != "workspace/symbol" {
				return
			}
			started <- struct{}{}
			select {
			case <-release:
			case <-ctx.Done():
			}
		}
	})
	defer client.close()

	first := client.requestAsync("workspace/symbol", map[string]any{"query": "first secret query"})
	second := client.requestAsync("workspace/symbol", map[string]any{"query": "second secret query"})
	<-started
	<-started
	firstReceived := client.waitForLogContaining("lsp.request.received")
	secondReceived := client.waitForLogContaining("lsp.request.received")
	receivedText := mustJSONText(t, firstReceived.Params) + mustJSONText(t, secondReceived.Params)
	spanIDs := regexp.MustCompile(`spanId=([a-z0-9-]+)`).FindAllStringSubmatch(receivedText, -1)
	if len(spanIDs) != 2 || spanIDs[0][1] == spanIDs[1][1] {
		t.Fatalf("concurrent request span IDs are not distinct: %s", receivedText)
	}
	for _, expected := range []string{"method=workspace/symbol", "requestId=1", "requestId=2"} {
		if !strings.Contains(receivedText, expected) {
			t.Fatalf("concurrent request logs missing %q: %s", expected, receivedText)
		}
	}
	if strings.Contains(receivedText, "secret query") {
		t.Fatalf("request lifecycle log leaked query text: %s", receivedText)
	}
	close(release)
	client.waitForResponse("workspace/symbol", first)
	client.waitForResponse("workspace/symbol", second)
	unknown := client.requestAsync("workspace/unknownMethod", map[string]any{"secret": "must not be logged"})
	select {
	case response := <-unknown:
		if response.Error == nil {
			t.Fatalf("unknown method returned no error: %#v", response)
		}
	case <-time.After(time.Second):
		t.Fatal("unknown method response timed out")
	}
	errorLog := client.waitForLogContaining("status=error")
	errorText := mustJSONText(t, errorLog.Params)
	for _, expected := range []string{"method=workspace/unknownMethod", "requestId=3", "code=-32601"} {
		if !strings.Contains(errorText, expected) {
			t.Fatalf("request error lifecycle log missing %q: %s", expected, errorText)
		}
	}
	if strings.Contains(errorText, "must not be logged") {
		t.Fatalf("request error lifecycle log leaked parameters: %s", errorText)
	}

	if err := client.notify("textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": "file:///workspace/logging.asp", "version": 9},
		"contentChanges": []map[string]any{{"text": sourceSentinel}},
	}); err != nil {
		t.Fatal(err)
	}
	didChangeLog := client.waitForLogContaining("method=textDocument/didChange")
	didChangeText := mustJSONText(t, didChangeLog.Params)
	if !strings.Contains(didChangeText, "changes=1") || strings.Contains(didChangeText, sourceSentinel) {
		t.Fatalf("unsafe didChange lifecycle log: %s", didChangeText)
	}

	var response rpcMessage
	var responseErr error
	var responseGroup sync.WaitGroup
	responseGroup.Add(1)
	go func() {
		defer responseGroup.Done()
		response, responseErr = server.requestClient(context.Background(), "workspace/configuration", map[string]any{"items": []any{}})
	}()
	serverRequest := client.waitForServerRequest("workspace/configuration")
	time.Sleep(25 * time.Millisecond)
	client.respondToServerRequest(serverRequest, []any{map[string]any{"enabled": true}})
	responseGroup.Wait()
	if responseErr != nil || response.Error != nil {
		t.Fatalf("client response delivery failed: response=%#v err=%v", response, responseErr)
	}
	responseLog := client.waitForLogContaining("lsp.response.received")
	responseText := mustJSONText(t, responseLog.Params)
	for _, expected := range []string{"method=workspace/configuration", "requestId=asp-lsp-go-1", "status=ok", "resultBytes=", "roundTripMs="} {
		if !strings.Contains(responseText, expected) {
			t.Fatalf("client response log missing %q: %s", expected, responseText)
		}
	}
	if strings.Contains(responseText, "enabled") {
		t.Fatalf("client response lifecycle log leaked result content: %s", responseText)
	}
	match := regexp.MustCompile(`roundTripMs=([0-9.]+)`).FindStringSubmatch(responseText)
	if len(match) != 2 {
		t.Fatalf("client response lifecycle log missing parseable round trip: %s", responseText)
	}
	roundTripMS, err := strconv.ParseFloat(match[1], 64)
	if err != nil || roundTripMS < 20 {
		t.Fatalf("client response round trip = %q, want at least 20ms; log=%s", match[1], responseText)
	}
	completed := client.waitForLogContaining("lsp.response.completed")
	if text := mustJSONText(t, completed.Params); !strings.Contains(text, "matched=true") || !strings.Contains(text, "deliveryMs=") {
		t.Fatalf("client response completion log missing match state: %s", text)
	}
}

func TestServeSynchronizesDebugConfigurationWithInboundLogging(t *testing.T) {
	client := startStdioTestClientWithServer(t, func(server *Server) {
		server.settings.DebugOutput = "summary"
	})
	defer client.close()
	for index := 0; index < 50; index++ {
		if err := client.notify("workspace/didChangeConfiguration", map[string]any{"settings": map[string]any{"aspLsp": map[string]any{
			"debug": map[string]any{
				"output":  "summary",
				"logFile": map[string]any{"enabled": false, "path": filepath.Join(t.TempDir(), "runtime.log")},
			},
		}}}); err != nil {
			t.Fatal(err)
		}
		response := client.requestAsync("codeAction/resolve", map[string]any{"title": "configuration barrier"})
		client.waitForResponse("codeAction/resolve", response)
	}
}
