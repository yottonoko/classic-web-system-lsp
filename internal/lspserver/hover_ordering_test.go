package lspserver

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestHoverDeclarationFastPathsSkipIncludeExpansion(t *testing.T) {
	source := `<!-- #include file="level-2.inc" -->
<%
Function RootFunction(firstName)
  RootFunction = firstName
End Function
Dim RootVariable
Const RootConstant = 1
Sub RenderValue()
  Dim LocalValue
  Response.Write LocalValue
End Sub
%>
`
	server, uri := newHoverOrderingFixture(t, source)
	defer server.shutdownRuntimeCaches()
	var expansions atomic.Int64
	var legacyScans atomic.Int64
	server.includeExpansionTestHook = func() { expansions.Add(1) }
	server.settings.VBScriptAssumeUndefinedGlobals = true
	server.legacyUndefinedGlobalProgressTestHook = func(_ string, _ string, _, _ int) { legacyScans.Add(1) }
	document := core.NewTextDocument(uri, "classic-asp", 0, source)

	for _, test := range []struct {
		name   string
		needle string
		delta  int
		want   string
	}{
		{name: "root function declaration", needle: "Function RootFunction", delta: len("Function "), want: "Function RootFunction"},
		{name: "root variable declaration", needle: "Dim RootVariable", delta: len("Dim "), want: "(global) Dim RootVariable As"},
		{name: "root constant declaration", needle: "Const RootConstant", delta: len("Const "), want: "(global) Const RootConstant As"},
		{name: "local variable declaration", needle: "Dim LocalValue", delta: len("Dim "), want: "(local) Dim LocalValue As"},
		{name: "local variable reference", needle: "Response.Write LocalValue", delta: len("Response.Write "), want: "(local) Dim LocalValue As"},
	} {
		t.Run(test.name, func(t *testing.T) {
			expansions.Store(0)
			offset := strings.Index(source, test.needle)
			if offset < 0 {
				t.Fatalf("source does not contain %q", test.needle)
			}
			value := server.hoverContext(context.Background(), uri, document.PositionAt(offset+test.delta))
			if value == nil {
				t.Fatalf("hover = nil, want %q", test.want)
			}
			encoded := mustJSONForTest(t, value)
			if !strings.Contains(encoded, test.want) {
				t.Fatalf("hover = %s, want substring %q", encoded, test.want)
			}
			if got := expansions.Load(); got != 0 {
				t.Fatalf("include expansion count = %d, want zero", got)
			}
			if got := legacyScans.Load(); got != 0 {
				t.Fatalf("legacy global scan count = %d, want zero", got)
			}
		})
	}
}

func TestHoverIgnoresIncludedServerObjectAfterRequestOffset(t *testing.T) {
	ownerSource := `<%
Dim Shared
Response.Write Shared
%>
<!-- #include file="objects.inc" -->
`
	server, uri := newIncludedObjectHoverFixture(t, ownerSource, `<object id="Shared" runat="server" progid="Repository.Type"></object>`)
	defer server.shutdownRuntimeCaches()
	document := core.NewTextDocument(uri, "classic-asp", 0, ownerSource)
	offset := strings.Index(ownerSource, "Response.Write Shared") + len("Response.Write ")
	value := server.hoverContext(context.Background(), uri, document.PositionAt(offset))
	if value == nil {
		t.Fatal("root hover before include = nil")
	}
	serialized := mustJSONForTest(t, value)
	if !strings.Contains(serialized, "(global) Dim Shared As") || strings.Contains(serialized, "Server OBJECT declaration.") {
		t.Fatalf("hover before include = %s, want source-visible root declaration", serialized)
	}
}

func TestHoverRootDeclarationsWinOverLaterIncludesBySourceOrder(t *testing.T) {
	tests := []struct {
		name        string
		declaration string
		include     string
		want        string
	}{
		{
			name:        "function before Dim include",
			declaration: "Function Shared(firstName)\n  Shared = firstName\nEnd Function",
			include:     "<% Dim Shared %>\n",
			want:        "Function Shared",
		},
		{
			name:        "function before Const include",
			declaration: "Function Shared(firstName)\n  Shared = firstName\nEnd Function",
			include:     "<% Const Shared = 1 %>\n",
			want:        "Function Shared",
		},
		{
			name:        "Dim before Function include",
			declaration: "Dim Shared",
			include:     "<% Function Shared()\nEnd Function %>\n",
			want:        "(global) Dim Shared As",
		},
		{
			name:        "Const before Dim include",
			declaration: "Const Shared = 1",
			include:     "<% Dim Shared %>\n",
			want:        "(global) Const Shared As",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ownerSource := "<%\n" + test.declaration + "\n%>\n<!-- #include file=\"objects.inc\" -->\n<%\nResponse.Write Shared\n%>\n"
			server, uri := newIncludedObjectHoverFixture(t, ownerSource, test.include)
			defer server.shutdownRuntimeCaches()
			document := core.NewTextDocument(uri, "classic-asp", 0, ownerSource)
			offset := strings.LastIndex(ownerSource, "Response.Write Shared") + len("Response.Write ")
			value := server.hoverContext(context.Background(), uri, document.PositionAt(offset))
			if value == nil || !strings.Contains(mustJSONForTest(t, value), test.want) {
				t.Fatalf("hover = %#v, want %q", value, test.want)
			}
		})
	}
}

func TestHoverRootDeclarationWinnerUsesFirstSourceVisibleDeclaration(t *testing.T) {
	tests := []struct {
		name    string
		source  string
		want    string
		notWant string
	}{
		{
			name: "Function before Dim",
			source: `<%
Function Shared(firstName)
  Shared = firstName
End Function
Dim Shared
Response.Write Shared
%>
`,
			want:    "Function Shared(ByRef firstName)",
			notWant: "(global) Dim Shared As",
		},
		{
			name: "Dim before Function",
			source: `<%
Dim Shared
Function Shared()
End Function
Response.Write Shared
%>
`,
			want:    "(global) Dim Shared As",
			notWant: "Function Shared",
		},
		{
			name: "Function before OBJECT",
			source: `<%
Function Shared(firstName)
  Shared = firstName
End Function
%>
<object id="Shared" runat="server" progid="Later.Type"></object>
<% Response.Write Shared %>
`,
			want:    "Function Shared(ByRef firstName)",
			notWant: "Later.Type",
		},
		{
			name: "OBJECT before Function",
			source: `<object id="Shared" runat="server" progid="Earlier.Type"></object>
<%
Function Shared()
End Function
Response.Write Shared
%>
`,
			want:    "Earlier.Type",
			notWant: "Function Shared",
		},
		{
			name: "duplicate Function and Sub",
			source: `<%
Function Shared(firstName)
  Shared = firstName
End Function
Sub Shared(secondName)
End Sub
Response.Write Shared
%>
`,
			want:    "Function Shared(ByRef firstName)",
			notWant: "Sub Shared(ByRef secondName)",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server, uri := newHoverOrderingFixture(t, test.source)
			defer server.shutdownRuntimeCaches()
			document := core.NewTextDocument(uri, "classic-asp", 0, test.source)
			offset := strings.LastIndex(test.source, "Response.Write Shared") + len("Response.Write ")
			value := server.hoverContext(context.Background(), uri, document.PositionAt(offset))
			if value == nil {
				t.Fatal("root declaration winner hover = nil")
			}
			serialized := mustJSONForTest(t, value)
			if !strings.Contains(serialized, test.want) || test.notWant != "" && strings.Contains(serialized, test.notWant) {
				t.Fatalf("root declaration winner hover = %s, want %q", serialized, test.want)
			}
		})
	}
}

func TestHoverEarlierIncludesWinOverLaterRootDeclarationsBySourceOrder(t *testing.T) {
	tests := []struct {
		name        string
		declaration string
		include     string
		want        string
	}{
		{
			name:        "Dim include before Function",
			declaration: "Function Shared(firstName)\n  Shared = firstName\nEnd Function",
			include:     "<% Dim Shared %>\n",
			want:        "Dim Shared As",
		},
		{
			name:        "Function include before Dim",
			declaration: "Dim Shared",
			include:     "<% Function Shared()\nEnd Function %>\n",
			want:        "Function Shared",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ownerSource := "<!-- #include file=\"objects.inc\" -->\n<%\n" + test.declaration + "\nResponse.Write Shared\n%>\n"
			server, uri := newIncludedObjectHoverFixture(t, ownerSource, test.include)
			defer server.shutdownRuntimeCaches()
			document := core.NewTextDocument(uri, "classic-asp", 0, ownerSource)
			offset := strings.LastIndex(ownerSource, "Response.Write Shared") + len("Response.Write ")
			value := server.hoverContext(context.Background(), uri, document.PositionAt(offset))
			if value == nil || !strings.Contains(mustJSONForTest(t, value), test.want) {
				t.Fatalf("hover = %#v, want %q", value, test.want)
			}
		})
	}
}

func TestHoverIncludedNameKeepsFirstDeclarationAcrossKinds(t *testing.T) {
	ownerSource := `<!-- #include file="objects.inc" -->
<% Response.Write Shared %>
`
	includeSource := `<%
Dim Shared
Function Shared()
End Function
%>
`
	server, uri := newIncludedObjectHoverFixture(t, ownerSource, includeSource)
	defer server.shutdownRuntimeCaches()
	document := core.NewTextDocument(uri, "classic-asp", 0, ownerSource)
	offset := strings.Index(ownerSource, "Response.Write Shared") + len("Response.Write ")
	value := server.hoverContext(context.Background(), uri, document.PositionAt(offset))
	if value == nil || !strings.Contains(mustJSONForTest(t, value), "Dim Shared As") {
		t.Fatalf("included mixed-kind hover = %#v, want first Dim declaration", value)
	}
}

func TestHoverNestedIncludeExpansionPrecedesLaterParentDeclaration(t *testing.T) {
	tests := []struct {
		name         string
		parentSource string
		nestedSource string
		want         string
		notWant      string
	}{
		{
			name: "nested Dim before later parent Function",
			parentSource: `<!-- #include file="nested.inc" -->
<%
Function Shared(firstName)
  Shared = firstName
End Function
%>
`,
			nestedSource: strings.Repeat("\n", 256) + `<% Dim Shared %>
`,
			want:    "(global) Dim Shared As",
			notWant: "Function Shared",
		},
		{
			name: "nested Const before later parent Function",
			parentSource: `<!-- #include file="nested.inc" -->
<%
Function Shared(firstName)
  Shared = firstName
End Function
%>
`,
			nestedSource: strings.Repeat("\n", 256) + `<% Const Shared = 1 %>
`,
			want:    "(global) Const Shared As",
			notWant: "Function Shared",
		},
		{
			name: "nested OBJECT before later parent Function",
			parentSource: `<!-- #include file="nested.inc" -->
<%
Function Shared(firstName)
  Shared = firstName
End Function
%>
`,
			nestedSource: strings.Repeat("\n", 256) + `<object id="Shared" runat="server" progid="Nested.Type"></object>
`,
			want:    "Nested.Type",
			notWant: "Function Shared",
		},
		{
			name: "parent Function before nested Dim",
			parentSource: `<%
Function Shared(firstName)
  Shared = firstName
End Function
%>
<!-- #include file="nested.inc" -->
`,
			nestedSource: strings.Repeat("\n", 256) + `<% Dim Shared %>
`,
			want:    "Function Shared",
			notWant: "(global) Dim Shared As",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			ownerSource := `<!-- #include file="parent.inc" -->
<% Response.Write Shared %>
`
			for name, source := range map[string]string{
				"default.asp": ownerSource,
				"parent.inc":  test.parentSource,
				"nested.inc":  test.nestedSource,
			} {
				if err := os.WriteFile(filepath.Join(root, name), []byte(source), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			server := New(strings.NewReader(""), io.Discard, io.Discard)
			defer server.shutdownRuntimeCaches()
			server.rootPath = root
			server.rootURI = filePathURI(root)
			server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
			uri := filePathURI(filepath.Join(root, "default.asp"))
			server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 1, ownerSource)
			document := core.NewTextDocument(uri, "classic-asp", 0, ownerSource)
			offset := strings.Index(ownerSource, "Response.Write Shared") + len("Response.Write ")
			value := server.hoverContext(context.Background(), uri, document.PositionAt(offset))
			if value == nil {
				t.Fatal("nested declaration hover = nil")
			}
			serialized := mustJSONForTest(t, value)
			if !strings.Contains(serialized, test.want) || test.notWant != "" && strings.Contains(serialized, test.notWant) {
				t.Fatalf("nested declaration hover = %s, want %q", serialized, test.want)
			}
		})
	}
}

func TestHoverRootServerObjectWinsOverLaterConflictingInclude(t *testing.T) {
	ownerSource := `<object id="Shared" runat="server" progid="Root.Type"></object>
<% Response.Write Shared %>
<!-- #include file="objects.inc" -->
`
	server, uri := newIncludedObjectHoverFixture(t, ownerSource, `<object id="Shared" runat="server" progid="Included.Type"></object>`)
	defer server.shutdownRuntimeCaches()
	document := core.NewTextDocument(uri, "classic-asp", 0, ownerSource)
	offset := strings.Index(ownerSource, "Response.Write Shared") + len("Response.Write ")
	value := server.hoverContext(context.Background(), uri, document.PositionAt(offset))
	if value == nil {
		t.Fatal("root server object hover = nil")
	}
	serialized := mustJSONForTest(t, value)
	if !strings.Contains(serialized, "Root.Type") || strings.Contains(serialized, "Included.Type") {
		t.Fatalf("root server object hover = %s, want Root.Type", serialized)
	}
}

func TestHoverEarlierServerObjectIncludeWinsOverLaterRootObject(t *testing.T) {
	ownerSource := `<!-- #include file="objects.inc" -->
<object id="Shared" runat="server" progid="Root.Type"></object>
<% Response.Write Shared %>
`
	server, uri := newIncludedObjectHoverFixture(t, ownerSource, `<object id="Shared" runat="server" progid="Included.Type"></object>`)
	defer server.shutdownRuntimeCaches()
	document := core.NewTextDocument(uri, "classic-asp", 0, ownerSource)
	offset := strings.Index(ownerSource, "Response.Write Shared") + len("Response.Write ")
	value := server.hoverContext(context.Background(), uri, document.PositionAt(offset))
	if value == nil {
		t.Fatal("included server object hover = nil")
	}
	serialized := mustJSONForTest(t, value)
	if !strings.Contains(serialized, "Included.Type") || strings.Contains(serialized, "Root.Type") {
		t.Fatalf("included server object hover = %s, want Included.Type", serialized)
	}
}

func TestHoverIncompleteIncludePrefixDoesNotReturnRootProcedure(t *testing.T) {
	root := t.TempDir()
	ownerSource := strings.Repeat("<!-- #include file=\"missing.inc\" -->\n", includeExpansionUnitBudget+1) +
		"<!-- #include file=\"shadow.inc\" -->\n<%\nFunction Shared(firstName)\n  Shared = firstName\nEnd Function\nResponse.Write Shared\n%>\n"
	shadowPath := filepath.Join(root, "shadow.inc")
	ownerPath := filepath.Join(root, "default.asp")
	if err := os.WriteFile(shadowPath, []byte("<% Dim Shared %>\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ownerPath, []byte(ownerSource), 0o600); err != nil {
		t.Fatal(err)
	}
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	defer server.shutdownRuntimeCaches()
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
	uri := filePathURI(ownerPath)
	server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 1, ownerSource)
	parsed := core.ParseDocument(uri, ownerSource, core.Settings{DefaultLanguage: "VBScript"})
	index, complete := server.includedServerObjectIndexContext(context.Background(), parsed)
	if complete || index == nil {
		t.Fatalf("included index = (%#v, %v), want incomplete partial index", index, complete)
	}
	if _, found := index.Globals["shared"]; found {
		t.Fatal("incomplete prefix unexpectedly reached shadow declaration")
	}
	offset := strings.LastIndex(ownerSource, "Response.Write Shared") + len("Response.Write ")
	value := server.hoverContext(context.Background(), uri, core.NewTextDocument(uri, "classic-asp", 0, ownerSource).PositionAt(offset))
	if value != nil && strings.Contains(mustJSONForTest(t, value), "Function Shared") {
		t.Fatalf("incomplete-prefix hover = %s, must not return root function", mustJSONForTest(t, value))
	}
}

func TestHoverIncompleteIncludePrefixDoesNotReturnBuiltinLen(t *testing.T) {
	root := t.TempDir()
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	defer server.shutdownRuntimeCaches()
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
	const depth = 14
	for index := depth; index >= 1; index-- {
		next := ""
		if index > 1 {
			next = fmt.Sprintf("<!-- #include file=\"level-%d.inc\" -->\n<!-- #include file=\"level-%d.inc\" -->\n", index-1, index-1)
		} else {
			next = strings.Repeat("<!-- #include file=\"missing.inc\" -->\n", includeExpansionUnitBudget)
		}
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("level-%d.inc", index)), []byte(next+"<% value = \"level\" %>\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ownerSource := "<!-- #include file=\"level-14.inc\" -->\n" +
		"<!-- #include file=\"level-14.inc\" -->\n" +
		"<!-- #include file=\"late.inc\" -->\n" +
		"<%\nResponse.Write Len(\"value\")\n%>\n"
	if err := os.WriteFile(filepath.Join(root, "late.inc"), []byte("<%\nFunction Len(value)\nEnd Function\n%>\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ownerPath := filepath.Join(root, "default.asp")
	if err := os.WriteFile(ownerPath, []byte(ownerSource), 0o600); err != nil {
		t.Fatal(err)
	}
	uri := filePathURI(ownerPath)
	server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 1, ownerSource)
	parsed := core.ParseDocument(uri, ownerSource, core.Settings{DefaultLanguage: "VBScript"})
	index, complete := server.includedServerObjectIndexContext(context.Background(), parsed)
	if complete || index == nil {
		t.Fatalf("included index complete = %v, want incomplete index", complete)
	}
	if _, found := index.Procedures["len"]; found {
		t.Fatal("incomplete include prefix unexpectedly reached the late Len procedure")
	}
	offset := strings.Index(ownerSource, "Response.Write Len") + len("Response.Write ")
	value := server.hoverContext(context.Background(), uri, core.NewTextDocument(uri, "classic-asp", 0, ownerSource).PositionAt(offset))
	if value != nil {
		t.Fatalf("incomplete-prefix builtin hover = %s, want nil", mustJSONForTest(t, value))
	}
}

func TestHoverIncludedPositivePrecedesForwardRootDeclarationGuard(t *testing.T) {
	ownerSource := `<!-- #include file="objects.inc" -->
<%
Response.Write state
%>
<%
Dim state
%>
`
	server, uri := newIncludedObjectHoverFixture(t, ownerSource, `<% Set state = New First %>
`)
	defer server.shutdownRuntimeCaches()
	document := core.NewTextDocument(uri, "classic-asp", 0, ownerSource)
	offset := strings.Index(ownerSource, "Response.Write state") + len("Response.Write ")
	value := server.hoverContext(context.Background(), uri, document.PositionAt(offset))
	if value == nil {
		t.Fatal("included positive hover = nil, want included variable hover")
	}
	serialized := mustJSONForTest(t, value)
	if !strings.Contains(serialized, "(global) Dim state As") {
		t.Fatalf("included positive hover = %s, want included state declaration", serialized)
	}
}

func TestIncludedServerObjectIndexCancellationPublishesNoPartialState(t *testing.T) {
	ownerSource := "<!-- #include file=\"objects.inc\" -->\n<% Response.Write Shared %>\n"
	server, uri := newIncludedObjectHoverFixture(t, ownerSource, `<% Dim Shared %>\n`)
	defer server.shutdownRuntimeCaches()
	parsed := core.ParseDocument(uri, ownerSource, core.Settings{DefaultLanguage: "VBScript"})
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	index, complete := server.includedServerObjectIndexContext(cancelled, parsed)
	if index != nil || complete {
		t.Fatalf("cancelled included index = (%#v, %v), want nil and incomplete", index, complete)
	}
}

func TestIncludedServerObjectIndexRecordsDistinctEdgesToLoadedDocument(t *testing.T) {
	root := t.TempDir()
	ownerSource := "<!-- #include file=\"a.inc\" -->\n<!-- #include file=\"b.inc\" -->\n<% Shared %>\n"
	files := map[string]string{
		"default.asp": ownerSource,
		"a.inc":       "<!-- #include file=\"shared.inc\" -->\n",
		"b.inc":       "<!-- #include file=\"shared.inc\" -->\n",
		"shared.inc":  `<object id="Shared" runat="server" progid="Repository.Type"></object>`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	defer server.shutdownRuntimeCaches()
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
	uri := filePathURI(filepath.Join(root, "default.asp"))
	parsed := core.ParseDocument(uri, ownerSource, core.Settings{DefaultLanguage: "VBScript"})
	index, complete := server.includedServerObjectIndexContext(context.Background(), parsed)
	if !complete || index == nil {
		t.Fatal("included object index was incomplete")
	}
	if got := len(index.Dependencies); got != 4 {
		t.Fatalf("dependency edge count = %d, want 4", got)
	}
}

func TestIncludedServerObjectIndexWarmValidationDeduplicatesRepeatedEdges(t *testing.T) {
	const repeatedIncludes = 1024
	ownerSource := strings.Repeat("<!-- #include file=\"objects.inc\" -->\n", repeatedIncludes) + "<% Shared %>\n"
	server, uri := newIncludedObjectHoverFixture(t, ownerSource, "<% Dim Shared %>\n")
	defer server.shutdownRuntimeCaches()
	parsed := core.ParseDocument(uri, ownerSource, core.Settings{DefaultLanguage: "VBScript"})

	builds := 0
	server.serverObjectLookupTestHook = func() {
		builds++
	}
	first, complete := server.includedServerObjectIndexContext(context.Background(), parsed)
	if !complete || first == nil {
		t.Fatal("initial included object index was incomplete")
	}
	if got := len(first.Dependencies); got != repeatedIncludes {
		t.Fatalf("dependency edge count = %d, want %d", got, repeatedIncludes)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	second, complete := server.includedServerObjectIndexContext(ctx, parsed)
	if !complete || second != first {
		t.Fatal("warm included object index was not reused")
	}
	if builds != 1 {
		t.Fatalf("included object index builds = %d, want 1", builds)
	}
}
func TestHoverIncludedServerObjectUsesIncludeAwareFallback(t *testing.T) {
	root := t.TempDir()
	includePath := filepath.Join(root, "objects.inc")
	ownerPath := filepath.Join(root, "default.asp")
	includeSource := `<object id="Repo" runat="server" progid="Repository.Type"></object>`
	ownerSource := `<!-- #include file="objects.inc" -->
<%
Response.Write Repo
%>
`
	if err := os.WriteFile(includePath, []byte(includeSource), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ownerPath, []byte(ownerSource), 0o600); err != nil {
		t.Fatal(err)
	}

	server := New(strings.NewReader(""), io.Discard, io.Discard)
	defer server.shutdownRuntimeCaches()
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
	uri := filePathURI(ownerPath)
	document := core.NewTextDocument(uri, "classic-asp", 0, ownerSource)
	server.documents[uri] = document
	var expansions atomic.Int64
	server.includeExpansionTestHook = func() { expansions.Add(1) }

	offset := strings.LastIndex(ownerSource, "Repo")
	value := server.hoverContext(context.Background(), uri, document.PositionAt(offset))
	if value == nil {
		t.Fatal("included server object hover = nil")
	}
	serialized := mustJSONForTest(t, value)
	for _, expected := range []string{
		"Server OBJECT declaration.",
		"Repository.Type",
		"- `progid`: `Repository.Type`",
		"References: 1",
	} {
		if !strings.Contains(serialized, expected) {
			t.Fatalf("included server object hover = %s, want %q", serialized, expected)
		}
	}
	if got := expansions.Load(); got == 0 {
		t.Fatal("included server object hover did not use include-aware resolution")
	}
}

func TestHoverCurrentDocumentServerObjectPrecedesRootDeclaration(t *testing.T) {
	source := `<!-- #include file="level-2.inc" -->
<object id="SharedValue" runat="server" progid="Repository.Type"></object>
<%
Dim SharedValue
Response.Write SharedValue
Sub RenderValue()
  Dim SharedValue
  Response.Write SharedValue
End Sub
%>
`
	server, uri := newHoverOrderingFixture(t, source)
	defer server.shutdownRuntimeCaches()
	var expansions atomic.Int64
	server.includeExpansionTestHook = func() { expansions.Add(1) }
	document := core.NewTextDocument(uri, "classic-asp", 0, source)

	rootOffset := strings.Index(source, "Dim SharedValue") + len("Dim ")
	rootValue := server.hoverContext(context.Background(), uri, document.PositionAt(rootOffset))
	if rootValue == nil {
		t.Fatal("root declaration hover = nil")
	}
	rootHover := mustJSONForTest(t, rootValue)
	if !strings.Contains(rootHover, "Server OBJECT declaration.") || !strings.Contains(rootHover, "Repository.Type") {
		t.Fatalf("root declaration hover = %s, want current-document server object", rootHover)
	}
	if got := expansions.Load(); got == 0 {
		t.Fatal("server object hover did not use include-aware resolution")
	}

	expansions.Store(0)
	localOffset := strings.LastIndex(source, "Response.Write SharedValue") + len("Response.Write ")
	localValue := server.hoverContext(context.Background(), uri, document.PositionAt(localOffset))
	if localValue == nil {
		t.Fatal("local reference hover = nil")
	}
	localHover := mustJSONForTest(t, localValue)
	if !strings.Contains(localHover, "(local) Dim SharedValue As") || strings.Contains(localHover, "Server OBJECT declaration.") {
		t.Fatalf("local reference hover = %s, want local shadow", localHover)
	}
	if got := expansions.Load(); got != 0 {
		t.Fatalf("local reference include expansion count = %d, want zero", got)
	}
}

func TestHoverIncludedServerObjectPrecedesConflictingRootDeclarations(t *testing.T) {
	ownerSource := `<!-- #include file="objects.inc" -->
<%
Function Shared(firstName)
  Shared = firstName
End Function
Dim Shared
Const Shared = 1
%>
`
	server, uri := newIncludedObjectHoverFixture(t, ownerSource, `<object id="Shared" runat="server" progid="Repository.Type"></object>`)
	defer server.shutdownRuntimeCaches()
	var lookups atomic.Int64
	var expansions atomic.Int64
	server.serverObjectLookupTestHook = func() { lookups.Add(1) }
	server.includeExpansionTestHook = func() { expansions.Add(1) }
	document := core.NewTextDocument(uri, "classic-asp", 0, ownerSource)

	for _, test := range []struct {
		name   string
		needle string
		delta  int
	}{
		{name: "root function", needle: "Function Shared", delta: len("Function ")},
		{name: "root variable", needle: "Dim Shared", delta: len("Dim ")},
		{name: "root constant", needle: "Const Shared", delta: len("Const ")},
	} {
		t.Run(test.name, func(t *testing.T) {
			offset := strings.Index(ownerSource, test.needle) + test.delta
			value := server.hoverContext(context.Background(), uri, document.PositionAt(offset))
			if value == nil {
				t.Fatal("included server object hover = nil")
			}
			serialized := mustJSONForTest(t, value)
			for _, expected := range []string{"Server OBJECT declaration.", "Repository.Type", "- `progid`: `Repository.Type`"} {
				if !strings.Contains(serialized, expected) {
					t.Fatalf("included server object hover = %s, want %q", serialized, expected)
				}
			}
		})
	}
	if got := lookups.Load(); got != 1 {
		t.Fatalf("included server object lookup count = %d, want one cached scan", got)
	}
	if got := expansions.Load(); got == 0 {
		t.Fatal("included server object hover did not use the full object resolution path")
	}
}

func TestHoverIncludedServerObjectDoesNotOverrideLocalShadow(t *testing.T) {
	ownerSource := `<!-- #include file="objects.inc" -->
<%
Sub RenderValue()
  Dim Shared
  Response.Write Shared
End Sub
%>
`
	server, uri := newIncludedObjectHoverFixture(t, ownerSource, `<object id="Shared" runat="server" progid="Repository.Type"></object>`)
	defer server.shutdownRuntimeCaches()
	var lookups atomic.Int64
	var expansions atomic.Int64
	server.serverObjectLookupTestHook = func() { lookups.Add(1) }
	server.includeExpansionTestHook = func() { expansions.Add(1) }
	document := core.NewTextDocument(uri, "classic-asp", 0, ownerSource)

	for _, offset := range []int{
		strings.Index(ownerSource, "Dim Shared") + len("Dim "),
		strings.LastIndex(ownerSource, "Response.Write Shared") + len("Response.Write "),
	} {
		value := server.hoverContext(context.Background(), uri, document.PositionAt(offset))
		if value == nil {
			t.Fatal("local hover = nil")
		}
		serialized := mustJSONForTest(t, value)
		if !strings.Contains(serialized, "(local) Dim Shared As") || strings.Contains(serialized, "Server OBJECT declaration.") {
			t.Fatalf("local hover = %s, want local shadow", serialized)
		}
	}
	if got := lookups.Load(); got != 0 {
		t.Fatalf("local shadow triggered included object lookup count = %d, want zero", got)
	}
	if got := expansions.Load(); got != 0 {
		t.Fatalf("local shadow triggered full include expansion count = %d, want zero", got)
	}
}

func TestHoverIncludedServerObjectPrecedesLegacyUndefinedGlobal(t *testing.T) {
	ownerSource := `<!-- #include file="objects.inc" -->
<%
Response.Write Repo
%>
`
	server, uri := newIncludedObjectHoverFixture(t, ownerSource, `<object id="Repo" runat="server" progid="Repository.Type"></object>`)
	defer server.shutdownRuntimeCaches()
	server.settings.VBScriptAssumeUndefinedGlobals = true
	document := core.NewTextDocument(uri, "classic-asp", 0, ownerSource)
	offset := strings.Index(ownerSource, "Response.Write Repo") + len("Response.Write ")
	value := server.hoverContext(context.Background(), uri, document.PositionAt(offset))
	if value == nil {
		t.Fatal("included server object hover = nil")
	}
	serialized := mustJSONForTest(t, value)
	if !strings.Contains(serialized, "Server OBJECT declaration.") || !strings.Contains(serialized, "Repository.Type") || strings.Contains(serialized, "legacy global") {
		t.Fatalf("included server object hover = %s, want OBJECT metadata before legacy global", serialized)
	}
}

func TestHoverIncludedServerObjectLookupRefreshesAfterOwnerDidChange(t *testing.T) {
	initial := `<!-- #include file="objects.inc" -->
<%
Dim Shared
%>
`
	updated := `<!-- #include file="objects.inc" -->
<%
Dim Fresh
%>
`
	server, uri := newIncludedObjectHoverFixture(t, initial, `<object id="Shared" runat="server" progid="Repository.Type"></object>`)
	defer server.shutdownRuntimeCaches()
	var lookups atomic.Int64
	server.serverObjectLookupTestHook = func() { lookups.Add(1) }
	document := core.NewTextDocument(uri, "classic-asp", 0, initial)

	sharedOffset := strings.Index(initial, "Dim Shared") + len("Dim ")
	sharedHover := server.hoverContext(context.Background(), uri, document.PositionAt(sharedOffset))
	if sharedHover == nil || !strings.Contains(mustJSONForTest(t, sharedHover), "Repository.Type") {
		t.Fatalf("initial object hover = %#v, want Repository.Type", sharedHover)
	}
	if err := server.handleNotification(context.Background(), "textDocument/didChange", mustRaw(map[string]any{
		"textDocument":   map[string]any{"uri": uri, "version": 2},
		"contentChanges": []map[string]any{{"text": updated}},
	})); err != nil {
		t.Fatal(err)
	}
	freshOffset := strings.Index(updated, "Dim Fresh") + len("Dim ")
	freshDocument := core.NewTextDocument(uri, "classic-asp", 0, updated)
	freshHover := server.hoverContext(context.Background(), uri, freshDocument.PositionAt(freshOffset))
	if freshHover == nil {
		t.Fatal("updated root declaration hover = nil")
	}
	freshSerialized := mustJSONForTest(t, freshHover)
	if !strings.Contains(freshSerialized, "(global) Dim Fresh As") || strings.Contains(freshSerialized, "Server OBJECT declaration.") || strings.Contains(freshSerialized, "Shared") {
		t.Fatalf("updated root declaration hover = %s, want current Fresh declaration", freshSerialized)
	}
	if got := lookups.Load(); got != 2 {
		t.Fatalf("included object lookup count after didChange = %d, want current revision scan", got)
	}
}

func TestHoverIncludedServerObjectLookupRefreshesAfterIncludedRevisionChange(t *testing.T) {
	ownerSource := `<!-- #include file="objects.inc" -->
<%
Dim Shared
%>
`
	server, uri := newIncludedObjectHoverFixture(t, ownerSource, `<object id="Shared" runat="server" progid="Repository.Type"></object>`)
	defer server.shutdownRuntimeCaches()
	var lookups atomic.Int64
	server.serverObjectLookupTestHook = func() { lookups.Add(1) }
	document := core.NewTextDocument(uri, "classic-asp", 0, ownerSource)
	sharedOffset := strings.Index(ownerSource, "Dim Shared") + len("Dim ")
	objectHover := server.hoverContext(context.Background(), uri, document.PositionAt(sharedOffset))
	if objectHover == nil || !strings.Contains(mustJSONForTest(t, objectHover), "Repository.Type") {
		t.Fatalf("initial object hover = %#v, want Repository.Type", objectHover)
	}

	includeURI := filePathURI(filepath.Join(filepath.Dir(fileURIPath(uri)), "objects.inc"))
	server.documents[includeURI] = core.NewTextDocument(includeURI, "classic-asp", 2, `<object id="Other" runat="server" progid="Other.Type"></object>`)
	rootHover := server.hoverContext(context.Background(), uri, document.PositionAt(sharedOffset))
	if rootHover == nil {
		t.Fatal("updated included object hover = nil")
	}
	serialized := mustJSONForTest(t, rootHover)
	if !strings.Contains(serialized, "(global) Dim Shared As") || strings.Contains(serialized, "Server OBJECT declaration.") || strings.Contains(serialized, "Repository.Type") {
		t.Fatalf("updated included object hover = %s, want current root declaration", serialized)
	}
	if got := lookups.Load(); got != 2 {
		t.Fatalf("included object lookup count after included revision = %d, want refreshed scan", got)
	}
}

func TestHoverIncludedServerObjectLookupRefreshesAfterNestedWatchedChanges(t *testing.T) {
	root := t.TempDir()
	ownerPath := filepath.Join(root, "default.asp")
	levelPath := filepath.Join(root, "level.inc")
	nestedPath := filepath.Join(root, "nested.inc")
	ownerSource := "<!-- #include file=\"level.inc\" -->\n<% Dim Shared %>\n"
	if err := os.WriteFile(ownerPath, []byte(ownerSource), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(levelPath, []byte("<!-- #include file=\"nested.inc\" -->\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	defer server.shutdownRuntimeCaches()
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
	uri := filePathURI(ownerPath)
	server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 1, ownerSource)
	var lookups atomic.Int64
	server.serverObjectLookupTestHook = func() { lookups.Add(1) }
	document := core.NewTextDocument(uri, "classic-asp", 0, ownerSource)
	position := document.PositionAt(strings.Index(ownerSource, "Dim Shared") + len("Dim "))

	initial := server.hoverContext(context.Background(), uri, position)
	if initial == nil || !strings.Contains(mustJSONForTest(t, initial), "(global) Dim Shared As") {
		t.Fatalf("initial hover = %#v, want root declaration while nested include is missing", initial)
	}
	if err := os.WriteFile(nestedPath, []byte(`<object id="Shared" runat="server" progid="Nested.Type"></object>`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := server.didChangeWatchedFiles(didChangeWatchedFilesParams{Changes: []fileEvent{{URI: filePathURI(nestedPath), Type: fileChangeCreated}}}); err != nil {
		t.Fatal(err)
	}
	created := server.hoverContext(context.Background(), uri, position)
	if created == nil || !strings.Contains(mustJSONForTest(t, created), "Nested.Type") {
		t.Fatalf("hover after nested creation = %#v, want included Server OBJECT", created)
	}
	if err := os.WriteFile(nestedPath, []byte(`<object id="Other" runat="server" progid="Other.Type"></object>`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := server.didChangeWatchedFiles(didChangeWatchedFilesParams{Changes: []fileEvent{{URI: filePathURI(nestedPath), Type: fileChangeChanged}}}); err != nil {
		t.Fatal(err)
	}
	changed := server.hoverContext(context.Background(), uri, position)
	if changed == nil {
		t.Fatal("hover after nested content change = nil")
	}
	serialized := mustJSONForTest(t, changed)
	if !strings.Contains(serialized, "(global) Dim Shared As") || strings.Contains(serialized, "Server OBJECT declaration.") {
		t.Fatalf("hover after nested content change = %s, want root declaration", serialized)
	}
	if got := lookups.Load(); got != 3 {
		t.Fatalf("nested object index rebuild count = %d, want 3 revisions", got)
	}
}

func newIncludedObjectHoverFixture(t *testing.T, ownerSource, includeSource string) (*Server, string) {
	t.Helper()
	root := t.TempDir()
	includePath := filepath.Join(root, "objects.inc")
	ownerPath := filepath.Join(root, "default.asp")
	if err := os.WriteFile(includePath, []byte(includeSource), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ownerPath, []byte(ownerSource), 0o600); err != nil {
		t.Fatal(err)
	}
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
	uri := filePathURI(ownerPath)
	server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 1, ownerSource)
	return server, uri
}

func newHoverOrderingFixture(t *testing.T, source string) (*Server, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "level-1.inc"), []byte("<% Dim includedValue %>\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "level-2.inc"), []byte("<!-- #include file=\"level-1.inc\" -->\n<% Dim nestedValue %>\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ownerPath := filepath.Join(root, "default.asp")
	if err := os.WriteFile(ownerPath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
	uri := filePathURI(ownerPath)
	document := core.NewTextDocument(uri, "classic-asp", 1, source)
	server.documents[uri] = document
	server.parseTextDocumentWithSnapshotSchedule(document, server.settings.DefaultLanguage, false)
	return server, uri
}

func TestHoverClassMembersPrecedeIncludedNames(t *testing.T) {
	ownerSource := `<!-- #include file="objects.inc" -->
<%
Class Holder
  Public SharedField
  Public Property Get SharedProperty()
    SharedProperty = 1
  End Property
  Public Sub SharedMethod()
    SharedField = 1
    SharedProperty = 1
    SharedMethod
  End Sub
End Class
%>`
	includeSource := `<%
Dim SharedField
Function SharedProperty()
End Function
%>
<object id="SharedMethod" runat="server" progid="Included.Method"></object>`
	server, uri := newIncludedObjectHoverFixture(t, ownerSource, includeSource)
	defer server.shutdownRuntimeCaches()
	document := core.NewTextDocument(uri, "classic-asp", 0, ownerSource)
	for _, test := range []struct {
		name  string
		text  string
		delta int
		want  string
	}{
		{name: "field declaration", text: "Public SharedField", delta: len("Public "), want: "(member) Holder.SharedField"},
		{name: "property declaration", text: "Property Get SharedProperty", delta: len("Property Get "), want: "Property Get SharedProperty"},
		{name: "method declaration", text: "Sub SharedMethod", delta: len("Sub "), want: "Sub SharedMethod"},
		{name: "field self reference", text: "SharedField = 1", want: "(member) Holder.SharedField"},
		{name: "property self reference", text: "SharedProperty = 1", want: "Property Get SharedProperty"},
		{name: "method self reference", text: "    SharedMethod\n", delta: len("    "), want: "Sub SharedMethod"},
	} {
		t.Run(test.name, func(t *testing.T) {
			offset := strings.Index(ownerSource, test.text)
			if offset < 0 {
				t.Fatalf("source does not contain %q", test.text)
			}
			offset += test.delta
			value := server.hoverContext(context.Background(), uri, document.PositionAt(offset))
			if value == nil || !strings.Contains(mustJSONForTest(t, value), test.want) {
				t.Fatalf("hover = %#v, want %q", value, test.want)
			}
		})
	}
}

func TestHoverCompleteIncludedSourcesOmitForeignRanges(t *testing.T) {
	ownerSource := `<!-- #include file="objects.inc" -->
<%
Response.Write SharedDim
Response.Write SharedConst
Response.Write SharedFunction
Response.Write SharedSub
Response.Write SharedObject
%>`
	includeSource := `<%
Dim SharedDim
Const SharedConst = 1
Function SharedFunction()
End Function
Sub SharedSub()
End Sub
%>
<object id="SharedObject" runat="server" progid="Included.Object"></object>`
	server, uri := newIncludedObjectHoverFixture(t, ownerSource, includeSource)
	defer server.shutdownRuntimeCaches()
	document := core.NewTextDocument(uri, "classic-asp", 0, ownerSource)
	for _, test := range []struct {
		name    string
		word    string
		content string
	}{
		{name: "Dim", word: "SharedDim", content: "(global) Dim SharedDim As"},
		{name: "Const", word: "SharedConst", content: "(global) Const SharedConst As"},
		{name: "Function", word: "SharedFunction", content: "Function SharedFunction"},
		{name: "Sub", word: "SharedSub", content: "Sub SharedSub"},
		{name: "OBJECT", word: "SharedObject", content: "Server OBJECT declaration."},
	} {
		t.Run(test.name, func(t *testing.T) {
			offset := strings.Index(ownerSource, test.word)
			if offset < 0 {
				t.Fatalf("source does not contain %q", test.word)
			}
			position := document.PositionAt(offset)
			value := server.hoverContext(context.Background(), uri, position)
			hover, ok := value.(*lsp.Hover)
			if !ok || hover == nil {
				t.Fatalf("hover = %#v, want *lsp.Hover", value)
			}
			serialized := mustJSONForTest(t, value)
			if !strings.Contains(serialized, test.content) || !strings.Contains(serialized, "Defined in [objects.inc]") {
				t.Fatalf("hover = %s, want %q and included-source note", serialized, test.content)
			}
			assertHoverRangeRootLocal(t, document, position, hover.Range)
		})
	}
}

func TestHoverPartialIncludedSourcesOmitForeignRanges(t *testing.T) {
	ownerSource := `<!-- #include file="objects.inc" -->
` + strings.Repeat(`<!-- #include file="missing.inc" -->
`, includeExpansionUnitBudget) + `<%
Response.Write PartialDim
Response.Write PartialConst
Response.Write PartialFunction
Response.Write PartialSub
Response.Write PartialObject
%>`
	includeSource := `<%
Dim PartialDim
Const PartialConst = 1
Function PartialFunction()
End Function
Sub PartialSub()
End Sub
%>
<object id="PartialObject" runat="server" progid="Partial.Object"></object>`
	server, uri := newIncludedObjectHoverFixture(t, ownerSource, includeSource)
	defer server.shutdownRuntimeCaches()
	document := core.NewTextDocument(uri, "classic-asp", 0, ownerSource)
	for _, test := range []struct {
		name    string
		word    string
		content string
	}{
		{name: "Dim", word: "PartialDim", content: "(global) Dim PartialDim As"},
		{name: "Const", word: "PartialConst", content: "(global) Const PartialConst As"},
		{name: "Function", word: "PartialFunction", content: "Function PartialFunction"},
		{name: "Sub", word: "PartialSub", content: "Sub PartialSub"},
		{name: "OBJECT", word: "PartialObject", content: "Server OBJECT declaration."},
	} {
		t.Run(test.name, func(t *testing.T) {
			offset := strings.Index(ownerSource, test.word)
			if offset < 0 {
				t.Fatalf("source does not contain %q", test.word)
			}
			position := document.PositionAt(offset)
			value := server.hoverContext(context.Background(), uri, position)
			hover, ok := value.(*lsp.Hover)
			if !ok || hover == nil {
				t.Fatalf("hover = %#v, want *lsp.Hover", value)
			}
			serialized := mustJSONForTest(t, value)
			if !strings.Contains(serialized, test.content) || !strings.Contains(serialized, "Defined in [objects.inc]") {
				t.Fatalf("hover = %s, want %q and included-source note", serialized, test.content)
			}
			assertHoverRangeRootLocal(t, document, position, hover.Range)
		})
	}
}

func assertHoverRangeRootLocal(t *testing.T, document *core.TextDocument, position lsp.Position, hoverRange *lsp.Range) {
	t.Helper()
	if hoverRange == nil {
		return
	}
	end := document.PositionAt(len(document.Text))
	if hoverRange.Start.Line < 0 || hoverRange.Start.Line > end.Line ||
		hoverRange.End.Line < hoverRange.Start.Line || hoverRange.End.Line > end.Line ||
		hoverRange.Start.Line == end.Line && hoverRange.Start.Character > end.Character ||
		hoverRange.End.Line == end.Line && hoverRange.End.Character > end.Character {
		t.Fatalf("hover range = %#v, outside root document ending at %#v", hoverRange, end)
	}
	if position.Line < hoverRange.Start.Line || position.Line > hoverRange.End.Line ||
		position.Line == hoverRange.Start.Line && position.Character < hoverRange.Start.Character ||
		position.Line == hoverRange.End.Line && position.Character > hoverRange.End.Character {
		t.Fatalf("hover range = %#v, does not contain requested position %#v", hoverRange, position)
	}
}
