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
)

func TestHoverRootDeclarationsReturnBeforeIncludeFanout(t *testing.T) {
	server, uri, source := newHoverDynamicUpdateFixture(t, hoverDynamicSource("RootFunction", "RootVariable", "RootConstant"))
	defer server.shutdownRuntimeCaches()

	for _, test := range []struct {
		name     string
		wantName string
		want     string
	}{
		{name: "Function", wantName: "RootFunction", want: "Function RootFunction"},
		{name: "Dim", wantName: "RootVariable", want: "Dim RootVariable As"},
		{name: "Const", wantName: "RootConstant", want: "Const RootConstant As"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := hoverWithin(t, server, uri, source, test.wantName)
			if !strings.Contains(got, test.want) {
				t.Fatalf("hover = %s, want substring %q", got, test.want)
			}
		})
	}
}

func TestHoverRootFunctionReferenceReturnsBeforeIncludeFanout(t *testing.T) {
	const functionName = "RootFunction"
	source := hoverDynamicFunctionReferenceSource(functionName)
	server, uri, _ := newHoverDynamicUpdateFixture(t, source)
	defer server.shutdownRuntimeCaches()
	var expansions atomic.Int64
	server.includeExpansionTestHook = func() { expansions.Add(1) }

	offset := strings.Index(source, "Call "+functionName) + len("Call ")
	got := hoverWithinAtOffset(t, server, uri, source, offset, functionName)
	if !strings.Contains(got, "Function "+functionName) {
		t.Fatalf("function reference hover = %s, want local signature", got)
	}
	if got := expansions.Load(); got != 0 {
		t.Fatalf("function reference include expansion count = %d, want zero", got)
	}
}

func TestHoverRootFunctionReferenceIncludedGlobalsShadowLocalFunction(t *testing.T) {
	for _, test := range []struct {
		name        string
		includeText string
		want        string
	}{
		{name: "Dim", includeText: "<% Dim Shared %>\n", want: "Dim Shared As"},
		{name: "Const", includeText: "<% Const Shared = 1 %>\n", want: "Const Shared As"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ownerSource := `<!-- #include file="objects.inc" -->
<%
Function Shared(firstName)
  Shared = firstName
End Function
Call Shared("value")
%>
`
			server, uri := newIncludedObjectHoverFixture(t, ownerSource, test.includeText)
			defer server.shutdownRuntimeCaches()
			offset := strings.Index(ownerSource, `Call Shared`) + len("Call ")
			got := hoverWithinAtOffset(t, server, uri, ownerSource, offset, "Shared")
			if !strings.Contains(got, test.want) || strings.Contains(got, "Function Shared") {
				t.Fatalf("included global hover = %s, want %q without local function", got, test.want)
			}
		})
	}
}

func TestHoverRootFunctionReferenceIncludedGlobalShadowsAfterBoundedFanout(t *testing.T) {
	for _, test := range []struct {
		name        string
		declaration string
		want        string
	}{
		{name: "Dim", declaration: "<% Dim Shared %>", want: "Dim Shared As"},
		{name: "Const", declaration: "<% Const Shared = 1 %>", want: "Const Shared As"},
	} {
		t.Run(test.name, func(t *testing.T) {
			const depth = 14
			root := t.TempDir()
			server := New(strings.NewReader(""), io.Discard, io.Discard)
			defer server.shutdownRuntimeCaches()
			server.rootPath = root
			server.rootURI = filePathURI(root)
			server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
			for level := 1; level <= depth; level++ {
				content := ""
				if level == depth {
					content += test.declaration + "\n"
				}
				if level > 1 {
					content += fmt.Sprintf("<!-- #include file=\"level-%d.inc\" -->\n<!-- #include file=\"level-%d.inc\" -->\n", level-1, level-1)
				}
				if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("level-%d.inc", level)), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			source := "<!-- #include file=\"level-14.inc\" -->\n<%\nFunction Shared(firstName)\n  Shared = firstName\nEnd Function\nCall Shared(\"value\")\n%>\n"
			ownerPath := filepath.Join(root, "default.asp")
			if err := os.WriteFile(ownerPath, []byte(source), 0o600); err != nil {
				t.Fatal(err)
			}
			uri := filePathURI(ownerPath)
			parsed := core.ParseDocument(uri, source, core.Settings{DefaultLanguage: "VBScript"})
			server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 1, source)
			offset := strings.Index(source, "Call Shared") + len("Call ")
			units, complete := server.vbscriptIncludeExecutionUnitsThroughOffsetContextResult(context.Background(), parsed, offset)
			if complete || len(units) != includeExpansionUnitBudget {
				t.Fatalf("bounded fanout expansion = (%d units, complete=%v), want exactly budget %d and incomplete", len(units), complete, includeExpansionUnitBudget)
			}

			got := hoverWithinAtOffset(t, server, uri, source, offset, "Shared")
			if !strings.Contains(got, test.want) || strings.Contains(got, "Function Shared") {
				t.Fatalf("bounded fanout hover = %s, want %q without root function", got, test.want)
			}
		})
	}
}

func TestHoverIncludedProcedureAndObjectRemainPositiveOnBoundedPrefix(t *testing.T) {
	for _, test := range []struct {
		name        string
		declaration string
		want        string
	}{
		{
			name: "Function",
			declaration: `<% Function Shared(firstName)
  Shared = firstName
End Function %>
`,
			want: "Function Shared(ByRef firstName)",
		},
		{
			name: "Sub",
			declaration: `<% Sub Shared(secondName)
End Sub %>
`,
			want: "Sub Shared(ByRef secondName)",
		},
		{
			name: "OBJECT",
			declaration: `<object id="Shared" runat="server" progid="Included.Type"></object>
`,
			want: "Included.Type",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			const depth = 14
			root := t.TempDir()
			server := New(strings.NewReader(""), io.Discard, io.Discard)
			defer server.shutdownRuntimeCaches()
			server.rootPath = root
			server.rootURI = filePathURI(root)
			server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
			for level := 1; level <= depth; level++ {
				content := ""
				if level == depth {
					content += test.declaration
				}
				if level > 1 {
					content += fmt.Sprintf("<!-- #include file=\"level-%d.inc\" -->\n<!-- #include file=\"level-%d.inc\" -->\n", level-1, level-1)
				}
				if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("level-%d.inc", level)), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			source := "<!-- #include file=\"level-14.inc\" -->\n<% Response.Write Shared %>\n"
			ownerPath := filepath.Join(root, "default.asp")
			if err := os.WriteFile(ownerPath, []byte(source), 0o600); err != nil {
				t.Fatal(err)
			}
			uri := filePathURI(ownerPath)
			parsed := core.ParseDocument(uri, source, core.Settings{DefaultLanguage: "VBScript"})
			server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 1, source)
			offset := strings.Index(source, "Response.Write Shared") + len("Response.Write ")
			units, complete := server.vbscriptIncludeExecutionUnitsThroughOffsetContextResult(context.Background(), parsed, offset)
			if complete || len(units) != includeExpansionUnitBudget {
				t.Fatalf("bounded include prefix = (%d units, complete=%v), want exactly budget %d and incomplete", len(units), complete, includeExpansionUnitBudget)
			}
			value := hoverWithinAtOffset(t, server, uri, source, offset, "Shared")
			if !strings.Contains(value, test.want) {
				t.Fatalf("bounded include hover = %s, want %q", value, test.want)
			}
		})
	}
}

func TestHoverRootFunctionReferenceBeforeLaterIncludeKeepsLocalSignature(t *testing.T) {
	ownerSource := `<%
Function Shared(firstName)
  Shared = firstName
End Function
Call Shared("value")
%>
<!-- #include file="objects.inc" -->
`
	server, uri := newIncludedObjectHoverFixture(t, ownerSource, "<% Dim Shared %>\n")
	defer server.shutdownRuntimeCaches()
	var expansions atomic.Int64
	server.includeExpansionTestHook = func() { expansions.Add(1) }
	offset := strings.Index(ownerSource, `Call Shared`) + len("Call ")
	got := hoverWithinAtOffset(t, server, uri, ownerSource, offset, "Shared")
	if !strings.Contains(got, "Function Shared") || strings.Contains(got, "Dim Shared As") {
		t.Fatalf("function reference hover = %s, want local function before later include", got)
	}
	if got := expansions.Load(); got != 0 {
		t.Fatalf("later include expansion count = %d, want zero", got)
	}
}

func TestHoverRootDeclarationsRemainCurrentAfterImmediateDidChange(t *testing.T) {
	initial := hoverDynamicSource("OldFunction", "OldVariable", "OldConstant")
	updated := hoverDynamicSource("NewFunction", "NewVariable", "NewConstant")
	server, uri, _ := newHoverDynamicUpdateFixture(t, initial)
	defer server.shutdownRuntimeCaches()

	if got := hoverWithin(t, server, uri, initial, "OldFunction"); !strings.Contains(got, "Function OldFunction") {
		t.Fatalf("initial hover = %s, want old declaration", got)
	}
	if err := server.handleNotification(context.Background(), "textDocument/didChange", mustRaw(map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 2},
		"contentChanges": []map[string]any{{
			"text": updated,
		}},
	})); err != nil {
		t.Fatal(err)
	}

	for attempt := 0; attempt < 2; attempt++ {
		for _, test := range []struct {
			name string
			want string
		}{
			{name: "NewFunction", want: "Function NewFunction"},
			{name: "NewVariable", want: "Dim NewVariable As"},
			{name: "NewConstant", want: "Const NewConstant As"},
		} {
			got := hoverWithin(t, server, uri, updated, test.name)
			if !strings.Contains(got, test.want) || strings.Contains(got, "OldFunction") || strings.Contains(got, "OldVariable") || strings.Contains(got, "OldConstant") {
				t.Fatalf("attempt %d %s hover = %s, want current declaration %q", attempt+1, test.name, got, test.want)
			}
		}
	}
}

func TestHoverLocalDeclarationShadowsIncludedGlobal(t *testing.T) {
	source := "<!-- #include file=\"level-14.inc\" -->\n<%\nSub RenderValue()\n  Dim ShadowedValue\n  ShadowedValue = 1\n  Response.Write ShadowedValue\nEnd Sub\n%>\n"
	server, uri, _ := newHoverDynamicUpdateFixture(t, source)
	defer server.shutdownRuntimeCaches()
	root := fileURIPath(uri)
	if err := os.WriteFile(filepath.Join(filepath.Dir(root), "level-1.inc"), []byte("<% Dim ShadowedValue %>\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got := hoverWithinAtOffset(t, server, uri, source, strings.LastIndex(source, "ShadowedValue"), "ShadowedValue")
	if !strings.Contains(got, "(local) Dim ShadowedValue As") || strings.Contains(got, "Defined in") {
		t.Fatalf("local hover did not shadow the included global: %s", got)
	}
}

func TestHoverLocalFastPathMatchesDeclarationInlayType(t *testing.T) {
	source := "<!-- #include file=\"level-14.inc\" -->\n<%\nSub RenderValue()\n  Dim SourceValue\n  Response.Write SourceValue\n  SourceValue = \"later\"\nEnd Sub\n%>\n"
	server, uri, _ := newHoverDynamicUpdateFixture(t, source)
	defer server.shutdownRuntimeCaches()
	offset := strings.Index(source, "Response.Write SourceValue") + len("Response.Write ")

	got := hoverWithinAtOffset(t, server, uri, source, offset, "SourceValue")
	if !strings.Contains(got, `Dim SourceValue As \"later\"`) {
		t.Fatalf("local hover differs from declaration inlay type: %s", got)
	}
}

func TestHoverRootDeclarationWithIncludesMatchesInlayType(t *testing.T) {
	source := "<!-- #include file=\"level-14.inc\" -->\n<%\nDim FutureValue\nSet FutureValue = New FutureType\n%>\n"
	server, uri, _ := newHoverDynamicUpdateFixture(t, source)
	defer server.shutdownRuntimeCaches()
	offset := strings.Index(source, "Dim FutureValue") + len("Dim ")

	got := hoverWithinAtOffset(t, server, uri, source, offset, "FutureValue")
	if !strings.Contains(got, "Dim FutureValue As FutureType") {
		t.Fatalf("root declaration hover differs from inlay type: %s", got)
	}
}

func newHoverDynamicUpdateFixture(t *testing.T, source string) (*Server, string, string) {
	t.Helper()
	root := t.TempDir()
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
	const depth = 14
	for index := depth; index >= 1; index-- {
		next := ""
		if index > 1 {
			next = fmt.Sprintf("<!-- #include file=\"level-%d.inc\" -->\n<!-- #include file=\"level-%d.inc\" -->\n", index-1, index-1)
		}
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("level-%d.inc", index)), []byte(next+"<% Dim includedValue %>\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ownerPath := filepath.Join(root, "default.asp")
	if err := os.WriteFile(ownerPath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	uri := filePathURI(ownerPath)
	server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 1, source)
	return server, uri, source
}

func hoverDynamicSource(functionName, variableName, constantName string) string {
	return fmt.Sprintf("<!-- #include file=\"level-14.inc\" -->\n<!-- #include file=\"level-14.inc\" -->\n<%%\nFunction %s(firstName)\n  %s = firstName\nEnd Function\nDim %s\nConst %s = 1\n%%>\n", functionName, functionName, variableName, constantName)
}

func hoverDynamicFunctionReferenceSource(functionName string) string {
	return fmt.Sprintf("<!-- #include file=\"level-14.inc\" -->\n<!-- #include file=\"level-14.inc\" -->\n<%%\nFunction %s(firstName)\n  %s = firstName\nEnd Function\nCall %s(\"value\")\nDim RootVariable\nConst RootConstant = 1\n%%>\n", functionName, functionName, functionName)
}

func hoverWithin(t *testing.T, server *Server, uri, source, name string) string {
	t.Helper()
	offset := strings.Index(source, name)
	return hoverWithinAtOffset(t, server, uri, source, offset, name)
}

func hoverWithinAtOffset(t *testing.T, server *Server, uri, source string, offset int, name string) string {
	t.Helper()
	if offset < 0 {
		t.Fatalf("source does not contain %q", name)
	}
	document := core.NewTextDocument(uri, "classic-asp", 0, source)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result := make(chan any, 1)
	go func() {
		result <- server.hoverContext(ctx, uri, document.PositionAt(offset+1))
	}()
	select {
	case value := <-result:
		if value == nil {
			t.Fatalf("hover for %q returned nil", name)
		}
		return mustJSONForTest(t, value)
	case <-time.After(2 * time.Second):
		t.Fatalf("hover for %q did not return within timeout", name)
		return ""
	}
}
