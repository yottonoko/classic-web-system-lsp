package lspserver

import (
	"context"
	"io"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

func TestBuildFlowchartContextPublishesCompletePayloadForCurrentGeneration(t *testing.T) {
	server, params := newFlowchartGenerationTestServer(t)

	rawResult := server.buildFlowchartContext(context.Background(), params)
	result, ok := rawResult.(map[string]any)
	if !ok {
		t.Fatalf("flowchart result type = %T, want map[string]any", rawResult)
	}
	if result["cancelled"] != nil || result["incomplete"] != nil {
		t.Fatalf("current flowchart result = %#v, want complete payload", result)
	}
	if result["sourceText"] == nil || result["mermaid"] == "flowchart TB\n" {
		t.Fatalf("current flowchart result = %#v, want assembled payload", result)
	}
	for _, key := range []string{"sections", "nodes", "edges", "includes", "stats"} {
		if result[key] == nil {
			t.Fatalf("current flowchart result missing %q: %#v", key, result)
		}
	}
}

func TestBuildFlowchartContextDropsLateGenerationInvalidationAndCancellation(t *testing.T) {
	t.Run("generation invalidation", func(t *testing.T) {
		server, params := newFlowchartGenerationTestServer(t)
		ctx := context.WithValue(context.Background(), flowchartFinalAssemblyTestHookContextKey{}, func(server *Server) {
			server.mu.Lock()
			server.graphGeneration++
			server.mu.Unlock()
		})

		rawResult := server.buildFlowchartContext(ctx, params)
		result, ok := rawResult.(map[string]any)
		if !ok {
			t.Fatalf("flowchart result type = %T, want map[string]any", rawResult)
		}
		if result["incomplete"] != true || result["cancelled"] == true {
			t.Fatalf("late-invalidated flowchart result = %#v, want incomplete non-cancelled payload", result)
		}
		assertIncompleteFlowchartPayload(t, result)
	})

	t.Run("cancellation", func(t *testing.T) {
		server, params := newFlowchartGenerationTestServer(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		ctx = context.WithValue(ctx, flowchartFinalAssemblyTestHookContextKey{}, func(*Server) {
			cancel()
		})

		rawResult := server.buildFlowchartContext(ctx, params)
		result, ok := rawResult.(map[string]any)
		if !ok {
			t.Fatalf("flowchart result type = %T, want map[string]any", rawResult)
		}
		if result["cancelled"] != true || result["incomplete"] != true {
			t.Fatalf("late-cancelled flowchart result = %#v, want incomplete cancelled payload", result)
		}
		assertIncompleteFlowchartPayload(t, result)
	})
}

func assertIncompleteFlowchartPayload(t *testing.T, result map[string]any) {
	t.Helper()
	for _, key := range []string{"sourceText", "sections", "nodes", "edges", "includes", "mermaid", "stats"} {
		if _, present := result[key]; present {
			t.Fatalf("incomplete flowchart result contains stale %q: %#v", key, result[key])
		}
	}
}

func newFlowchartGenerationTestServer(t *testing.T) (*Server, executeCommandParams) {
	t.Helper()
	server := New(nil, io.Discard, io.Discard)
	uri := "file:///workspace/flowchart-generation.asp"
	source := `<% Sub Main() : Response.Write "ready" : End Sub %>`
	server.mu.Lock()
	server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 1, source)
	server.mu.Unlock()
	return server, executeCommandParams{
		Command: "aspLsp.server.buildFlowchart",
		Arguments: []any{map[string]any{
			"uri": uri,
		}},
	}
}
