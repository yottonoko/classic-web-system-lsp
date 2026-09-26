package lspserver

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestVBScriptMonikerResolvesIncludedDefinitionWithContextTraversal(t *testing.T) {
	root := t.TempDir()
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
	includePath := filepath.Join(root, "shared.inc")
	includeSource := `<%
Function BuildName(firstName)
  BuildName = firstName
End Function
%>`
	if err := os.WriteFile(includePath, []byte(includeSource), 0o600); err != nil {
		t.Fatal(err)
	}
	ownerPath := filepath.Join(root, "default.asp")
	ownerSource := `<!-- #include file="shared.inc" -->
<%
Response.Write BuildName("Ada")
%>`
	if err := os.WriteFile(ownerPath, []byte(ownerSource), 0o600); err != nil {
		t.Fatal(err)
	}
	ownerURI := filePathURI(ownerPath)
	includeURI := filePathURI(includePath)
	server.documents[ownerURI] = core.NewTextDocument(ownerURI, "classic-asp", 1, ownerSource)
	position := core.NewTextDocument(ownerURI, "classic-asp", 0, ownerSource).PositionAt(strings.Index(ownerSource, "BuildName") + 2)
	monikers, rpcErr := server.handleRequest(context.Background(), "textDocument/moniker", mustRaw(map[string]any{
		"textDocument": map[string]any{"uri": ownerURI},
		"position":     position,
	}))
	if rpcErr != nil {
		t.Fatalf("included moniker request error = %#v", rpcErr)
	}
	values, ok := monikers.([]lsp.Moniker)
	if !ok || len(values) != 1 {
		t.Fatalf("included moniker result = %#v", monikers)
	}
	wantIdentifier := includeURI + "#BuildName#1#9"
	if values[0].Scheme != "asp-lsp" || values[0].Identifier != wantIdentifier || values[0].Unique != "project" || values[0].Kind != "export" {
		t.Fatalf("included moniker = %#v, want identifier %q", values[0], wantIdentifier)
	}
}

func TestVBScriptMonikerDropsResultAfterDeepRepeatedIncludeCancellation(t *testing.T) {
	root := t.TempDir()
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
	const depth = 14
	for index := depth; index >= 1; index-- {
		body := ""
		if index > 1 {
			body = fmt.Sprintf("<!-- #include file=\"level-%d.inc\" -->\n<!-- #include file=\"level-%d.inc\" -->\n", index-1, index-1)
		} else {
			body = `<%
Function BuildName(firstName)
  BuildName = firstName
End Function
%>`
		}
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("level-%d.inc", index)), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ownerPath := filepath.Join(root, "default.asp")
	ownerSource := `<!-- #include file="level-14.inc" -->
<!-- #include file="level-14.inc" -->
<%
Response.Write BuildName("Ada")
%>`
	if err := os.WriteFile(ownerPath, []byte(ownerSource), 0o600); err != nil {
		t.Fatal(err)
	}
	ownerURI := filePathURI(ownerPath)
	server.documents[ownerURI] = core.NewTextDocument(ownerURI, "classic-asp", 1, ownerSource)
	position := core.NewTextDocument(ownerURI, "classic-asp", 0, ownerSource).PositionAt(strings.Index(ownerSource, "BuildName") + 2)
	base, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := &cancelAfterErrContext{Context: base, cancel: cancel, limit: 1000}
	result, rpcErr := server.handleRequest(ctx, "textDocument/moniker", mustRaw(map[string]any{
		"textDocument": map[string]any{"uri": ownerURI},
		"position":     position,
	}))
	if rpcErr != nil {
		t.Fatalf("cancelled moniker request error = %#v", rpcErr)
	}
	if values, ok := result.([]lsp.Moniker); !ok || values != nil {
		t.Fatalf("cancelled moniker result = %#v, want no publication", result)
	}
	if ctx.checks.Load() < ctx.limit {
		t.Fatalf("moniker traversal did not cancel mid-traversal: checks=%d limit=%d", ctx.checks.Load(), ctx.limit)
	}
}
