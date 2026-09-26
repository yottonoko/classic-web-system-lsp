package lspserver

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestCallHierarchyAndMonikerRequestsCancelWhileJavaScriptSerializationIsHeld(t *testing.T) {
	server := newCallHierarchyMonikerCancellationServer(t)
	position := positionAtSuffix(callHierarchyMonikerJavaScriptSource, "callee")
	items := server.prepareCallHierarchy(server.callHierarchyMonikerURI, position)
	if len(items) != 1 {
		t.Fatalf("prepare call hierarchy = %#v", items)
	}

	tests := []struct {
		name   string
		method string
		params json.RawMessage
	}{
		{
			name:   "prepare call hierarchy",
			method: "textDocument/prepareCallHierarchy",
			params: requestParams(server.callHierarchyMonikerURI, position),
		},
		{
			name:   "incoming calls",
			method: "callHierarchy/incomingCalls",
			params: mustRaw(map[string]any{"item": items[0]}),
		},
		{
			name:   "outgoing calls",
			method: "callHierarchy/outgoingCalls",
			params: mustRaw(map[string]any{"item": items[0]}),
		},
		{
			name:   "moniker",
			method: "textDocument/moniker",
			params: requestParams(server.callHierarchyMonikerURI, position),
		},
		{
			name:   "selection range",
			method: "textDocument/selectionRange",
			params: mustRaw(map[string]any{
				"textDocument": map[string]any{"uri": server.callHierarchyMonikerURI},
				"positions":    []lsp.Position{position},
			}),
		},
		{
			name:   "document symbols",
			method: "textDocument/documentSymbol",
			params: mustRaw(map[string]any{"textDocument": map[string]any{"uri": server.callHierarchyMonikerURI}}),
		},
		{
			name:   "folding range",
			method: "textDocument/foldingRange",
			params: mustRaw(map[string]any{"textDocument": map[string]any{"uri": server.callHierarchyMonikerURI}}),
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			server.javascriptMu.Lock()
			defer server.javascriptMu.Unlock()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			started := make(chan struct{})
			server.requestDispatchTestHook = func(_ context.Context, method string) {
				if method == testCase.method {
					close(started)
				}
			}
			defer func() { server.requestDispatchTestHook = nil }()
			result := make(chan struct {
				value any
				err   *rpcError
			}, 1)
			go func() {
				value, err := server.handleRequest(ctx, testCase.method, testCase.params)
				result <- struct {
					value any
					err   *rpcError
				}{value: value, err: err}
			}()
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("request did not start")
			}
			cancel()
			select {
			case response := <-result:
				if response.err != nil && response.err.Code != requestCancelledError().Code {
					t.Fatalf("cancelled %s error = %#v", testCase.method, response.err)
				}
				if resultLength(response.value) != 0 {
					t.Fatalf("cancelled %s returned partial result: %#v", testCase.method, response.value)
				}
			case <-time.After(time.Second):
				t.Fatalf("cancelled %s remained queued behind JavaScript serialization", testCase.method)
			}
		})
	}
}

func TestCallHierarchyAndMonikerRequestsSucceedForJavaScriptAndVBScript(t *testing.T) {
	js := newCallHierarchyMonikerCancellationServer(t)
	jsPosition := positionAtSuffix(callHierarchyMonikerJavaScriptSource, "callee")
	jsItems := js.prepareCallHierarchyContext(context.Background(), js.callHierarchyMonikerURI, jsPosition)
	if len(jsItems) != 1 {
		t.Fatalf("JavaScript prepare call hierarchy = %#v", jsItems)
	}
	incoming := js.incomingCallsContext(context.Background(), jsItems[0])
	if len(incoming) != 1 || incoming[0].From.Name != "caller" {
		t.Fatalf("JavaScript incoming calls = %#v", incoming)
	}
	callerItems := js.prepareCallHierarchyContext(context.Background(), js.callHierarchyMonikerURI, positionAtSuffix(callHierarchyMonikerJavaScriptSource, "caller"))
	if len(callerItems) != 1 {
		t.Fatalf("JavaScript caller hierarchy = %#v", callerItems)
	}
	outgoing := js.outgoingCallsContext(context.Background(), callerItems[0])
	if len(outgoing) != 1 || outgoing[0].To.Name != "callee" {
		t.Fatalf("JavaScript outgoing calls = %#v", outgoing)
	}
	if monikers := js.monikersContext(context.Background(), js.callHierarchyMonikerURI, jsPosition); len(monikers) != 1 {
		t.Fatalf("JavaScript monikers = %#v", monikers)
	}

	vb := newCallHierarchyMonikerVBServer(t)
	vbPosition := positionAtSuffix(callHierarchyMonikerVBSource, "BuildName")
	vbItems := vb.prepareCallHierarchyContext(context.Background(), vb.callHierarchyMonikerURI, vbPosition)
	if len(vbItems) != 1 {
		t.Fatalf("VBScript prepare call hierarchy = %#v", vbItems)
	}
	if incoming := vb.incomingCallsContext(context.Background(), vbItems[0]); len(incoming) != 1 || incoming[0].From.Name != "Save" {
		t.Fatalf("VBScript incoming calls = %#v", incoming)
	}
	if outgoing := vb.outgoingCallsContext(context.Background(), vb.prepareCallHierarchy(vb.callHierarchyMonikerURI, positionAtSuffix(callHierarchyMonikerVBSource, "Save"))[0]); len(outgoing) != 1 || outgoing[0].To.Name != "BuildName" {
		t.Fatalf("VBScript outgoing calls = %#v", outgoing)
	}
	if monikers := vb.monikersContext(context.Background(), vb.callHierarchyMonikerURI, positionAtSuffix(callHierarchyMonikerVBSource, "BuildName")); len(monikers) != 1 {
		t.Fatalf("VBScript monikers = %#v", monikers)
	}
}

func TestCallHierarchyAndMonikerVBScriptRequestsBypassJavaScriptSerialization(t *testing.T) {
	server := newCallHierarchyMonikerVBServer(t)
	position := positionAtSuffix(callHierarchyMonikerVBSource, "BuildName")
	items := server.prepareCallHierarchy(server.callHierarchyMonikerURI, position)
	if len(items) != 1 {
		t.Fatalf("prepare call hierarchy = %#v", items)
	}
	tests := []struct {
		name   string
		method string
		params json.RawMessage
	}{
		{
			name:   "prepare call hierarchy",
			method: "textDocument/prepareCallHierarchy",
			params: requestParams(server.callHierarchyMonikerURI, position),
		},
		{
			name:   "incoming calls",
			method: "callHierarchy/incomingCalls",
			params: mustRaw(map[string]any{"item": items[0]}),
		},
		{
			name:   "outgoing calls",
			method: "callHierarchy/outgoingCalls",
			params: mustRaw(map[string]any{"item": server.prepareCallHierarchy(server.callHierarchyMonikerURI, positionAtSuffix(callHierarchyMonikerVBSource, "Save"))[0]}),
		},
		{
			name:   "moniker",
			method: "textDocument/moniker",
			params: requestParams(server.callHierarchyMonikerURI, position),
		},
		{
			name:   "selection range",
			method: "textDocument/selectionRange",
			params: mustRaw(map[string]any{
				"textDocument": map[string]any{"uri": server.callHierarchyMonikerURI},
				"positions":    []lsp.Position{position},
			}),
		},
		{
			name:   "document symbols",
			method: "textDocument/documentSymbol",
			params: mustRaw(map[string]any{"textDocument": map[string]any{"uri": server.callHierarchyMonikerURI}}),
		},
		{
			name:   "folding range",
			method: "textDocument/foldingRange",
			params: mustRaw(map[string]any{"textDocument": map[string]any{"uri": server.callHierarchyMonikerURI}}),
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			server.javascriptMu.Lock()
			result := make(chan struct {
				value any
				err   *rpcError
			}, 1)
			go func() {
				value, err := server.handleRequest(context.Background(), testCase.method, testCase.params)
				result <- struct {
					value any
					err   *rpcError
				}{value: value, err: err}
			}()
			select {
			case response := <-result:
				if response.err != nil || resultLength(response.value) == 0 {
					t.Fatalf("VBScript %s response = value %#v, error %#v", testCase.method, response.value, response.err)
				}
			case <-time.After(time.Second):
				server.javascriptMu.Unlock()
				t.Fatalf("VBScript %s waited on JavaScript serialization", testCase.method)
			}
			server.javascriptMu.Unlock()
		})
	}
}

func resultLength(value any) int {
	if value == nil {
		return 0
	}
	encoded, err := json.Marshal(value)
	if err != nil || string(encoded) == "null" || string(encoded) == "[]" {
		return 0
	}
	var values []any
	if json.Unmarshal(encoded, &values) == nil {
		return len(values)
	}
	return 1
}

const callHierarchyMonikerJavaScriptSource = `<script>
function callee() { return 1; }
function caller() { return callee(); }
</script>`

const callHierarchyMonikerVBSource = `<%
Function BuildName(firstName)
  BuildName = firstName
End Function
Sub Save()
  Response.Write BuildName("Ada")
End Sub
%>`

type callHierarchyMonikerServer struct {
	*Server
	callHierarchyMonikerURI string
}

func newCallHierarchyMonikerCancellationServer(t *testing.T) *callHierarchyMonikerServer {
	t.Helper()
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	uri := "file:///tmp/call-hierarchy-moniker-cancel.asp"
	server.rootPath = t.TempDir()
	server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 1, callHierarchyMonikerJavaScriptSource)
	return &callHierarchyMonikerServer{Server: server, callHierarchyMonikerURI: uri}
}

func newCallHierarchyMonikerVBServer(t *testing.T) *callHierarchyMonikerServer {
	t.Helper()
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	uri := "file:///tmp/call-hierarchy-moniker-vb.asp"
	server.rootPath = t.TempDir()
	server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 1, callHierarchyMonikerVBSource)
	return &callHierarchyMonikerServer{Server: server, callHierarchyMonikerURI: uri}
}
