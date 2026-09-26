package lspserver

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

func TestNavigationFolderDocumentsRejectsExternalFileURI(t *testing.T) {
	workspace := t.TempDir()
	external := t.TempDir()
	externalFile := filepath.Join(external, "outside.asp")
	if err := os.WriteFile(externalFile, []byte("<% Dim outside %>"), 0o644); err != nil {
		t.Fatal(err)
	}

	server := New(nil, io.Discard, io.Discard)
	server.rootPath = workspace
	server.rootURI = filePathURI(workspace)
	server.workspaceRoots = []workspaceRoot{{Path: workspace, URI: server.rootURI}}

	documents := server.navigationFolderDocumentsContextWithProgress(context.Background(), filePathURI(externalFile), nil)
	if len(documents) != 0 {
		t.Fatalf("external folder navigation returned %d documents, want 0", len(documents))
	}
}

func TestNavigationFolderDocumentsAcceptsFileInsideWorkspace(t *testing.T) {
	workspace := t.TempDir()
	folder := filepath.Join(workspace, "app")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	page := filepath.Join(folder, "default.asp")
	if err := os.WriteFile(page, []byte("<% Dim inside %>"), 0o644); err != nil {
		t.Fatal(err)
	}

	server := New(nil, io.Discard, io.Discard)
	server.rootPath = workspace
	server.rootURI = filePathURI(workspace)
	server.workspaceRoots = []workspaceRoot{{Path: workspace, URI: server.rootURI}}

	documents := server.navigationFolderDocumentsContextWithProgress(context.Background(), filePathURI(page), nil)
	if len(documents) != 1 || documents[0] == nil || documents[0].URI != filePathURI(page) {
		t.Fatalf("workspace folder navigation = %#v, want %s", documents, filePathURI(page))
	}
}

func TestNavigationFolderDocumentsCancellationDropsPartialScan(t *testing.T) {
	workspace := t.TempDir()
	for index := 0; index < 256; index++ {
		path := filepath.Join(workspace, "page-"+strconv.Itoa(index)+".asp")
		if err := os.WriteFile(path, []byte("<% Dim value %>"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	server := New(nil, io.Discard, io.Discard)
	server.rootPath = workspace
	server.rootURI = filePathURI(workspace)
	server.workspaceRoots = []workspaceRoot{{Path: workspace, URI: server.rootURI}}
	ctx := &navigationCancelAfterErrContext{Context: context.Background(), cancelAfter: 24}
	documents := server.navigationFolderDocumentsContextWithProgress(ctx, filePathURI(workspace), nil)
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatal("folder scan context was not cancelled")
	}
	if documents != nil {
		t.Fatalf("cancelled folder scan returned partial documents: %d", len(documents))
	}
}

func TestNavigationTargetCancellationDropsTrustAndStatTraversal(t *testing.T) {
	root := t.TempDir()
	owner := filepath.Join(root, "default.asp")
	if err := os.WriteFile(filepath.Join(root, "next.asp"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := &navigationCancelAfterErrContext{Context: context.Background(), cancelAfter: 21}
	builder := newNavigationGraphBuilder("document", filePathURI(owner), []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	builder.cancelContext = ctx
	parsed := core.ParseDocument(filePathURI(owner), `<a href="next.asp">next</a>`, core.Settings{})
	builder.addDocument(parsed, parsed.URI)
	if !errors.Is(builder.navigationError, context.Canceled) {
		t.Fatalf("cancelled target traversal error = %v, want context.Canceled", builder.navigationError)
	}
	if len(builder.edges) != 0 {
		t.Fatalf("cancelled target traversal published partial edges: %#v", builder.edges)
	}
}

func TestNavigationDocumentMaterializesExistingTargetAsSource(t *testing.T) {
	root := t.TempDir()
	rootURI := filePathURI(filepath.Join(root, "target.asp"))
	sourceURI := filePathURI(filepath.Join(root, "referrer.asp"))
	builder := newNavigationGraphBuilder("document", rootURI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})

	sourceID := "page:" + workspacepkg.FileIdentityKeyFromURI(sourceURI)
	builder.addNode(map[string]any{
		"id": sourceID, "label": "referrer.asp", "kind": "page",
		"uri": sourceURI, "fileName": "referrer.asp", "exists": false,
	})
	if got := builder.navigationSourceID(sourceURI); got != sourceID {
		t.Fatalf("source ID = %q, want %q", got, sourceID)
	}
	builder.materializeNavigationSourceNode(sourceID)

	node := builder.nodeByID[sourceID]
	if node["exists"] != true || node["isRoot"] != false {
		t.Fatalf("materialized source node = %#v, want authoritative source metadata", node)
	}
}

func TestNavigationSourceMaterializationRestoresLatestExactAlias(t *testing.T) {
	upperURI := "file://SERVER/Share/Page.asp"
	lowerURI := "file://server/share/page.asp"
	if !workspacepkg.SameFileIdentityURI(upperURI, lowerURI) {
		t.Fatalf("test aliases do not share a file identity: %q and %q", upperURI, lowerURI)
	}
	builder := newNavigationGraphBuilder("workspace", "", nil)
	upperID := builder.addURINode(upperURI)
	lowerID := builder.addURINode(lowerURI)
	if upperID != lowerID {
		t.Fatalf("alias node IDs differ: %q and %q", upperID, lowerID)
	}
	if got := navigationString(builder.nodeByID[upperID]["uri"]); got != canonicalGraphURI(lowerURI) {
		t.Fatalf("materialized lower alias URI = %q, want %q", got, canonicalGraphURI(lowerURI))
	}

	builder.addURINode(upperURI)
	if got := navigationString(builder.nodeByID[upperID]["uri"]); got != canonicalGraphURI(upperURI) {
		t.Fatalf("restored upper alias URI = %q, want %q", got, canonicalGraphURI(upperURI))
	}
}

func TestNavigationDocumentTargetMatchesRootVariants(t *testing.T) {
	root := t.TempDir()
	rootURI := filePathURI(filepath.Join(root, "target.asp"))
	ownerURI := filePathURI(filepath.Join(root, "nested", "referrer.asp"))
	workspaceRoots := []workspaceRoot{{Path: root, URI: filePathURI(root)}}
	tests := []struct {
		name      string
		ownerURI  string
		target    string
		edgeKind  string
		dynamic   bool
		pathKnown bool
		want      bool
	}{
		{name: "relative query and fragment", ownerURI: ownerURI, target: "../target.asp?from=relative#details", want: true},
		{name: "root relative", ownerURI: ownerURI, target: "/target.asp?from=root", want: true},
		{name: "query only", ownerURI: rootURI, target: "?tab=self", want: true},
		{name: "fragment only", ownerURI: rootURI, target: "#details", want: true},
		{name: "empty form", ownerURI: rootURI, edgeKind: "htmlForm", want: true},
		{name: "external", ownerURI: ownerURI, target: "https://example.com/target.asp", want: false},
		{name: "javascript", ownerURI: ownerURI, target: "javascript:void(0)", want: false},
		{name: "unknown dynamic", ownerURI: ownerURI, target: "{target}", dynamic: true, want: false},
		{name: "known template path", ownerURI: ownerURI, target: "../target.asp?from={value}", dynamic: true, pathKnown: true, want: true},
		{name: "different file", ownerURI: ownerURI, target: "other.asp", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := navigationDocumentTargetMatchesRoot(test.ownerURI, test.target, test.edgeKind, workspaceRoots, test.dynamic, test.pathKnown, rootURI)
			if got != test.want {
				t.Fatalf("navigationDocumentTargetMatchesRoot() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestNavigationTargetIsExternal(t *testing.T) {
	tests := []struct {
		name   string
		target string
		want   bool
	}{
		{name: "relative", target: "next.asp", want: false},
		{name: "parent relative", target: "../next.asp", want: false},
		{name: "root relative", target: "/next.asp", want: false},
		{name: "absolute URL", target: "https://example.com/next.asp", want: true},
		{name: "protocol relative", target: "//example.com/next.asp", want: true},
		{name: "custom scheme", target: "custom:next.asp", want: true},
		{name: "invalid protocol relative URL", target: "//[", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := navigationTargetIsExternal(test.target); got != test.want {
				t.Fatalf("navigationTargetIsExternal(%q) = %v, want %v", test.target, got, test.want)
			}
		})
	}
}

type navigationCancelAfterErrContext struct {
	context.Context
	cancelAfter int
	calls       int
}

func (ctx *navigationCancelAfterErrContext) Err() error {
	ctx.calls++
	if ctx.calls >= ctx.cancelAfter {
		return context.Canceled
	}
	return ctx.Context.Err()
}
