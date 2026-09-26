package lspserver

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

func TestVBScriptHoverUsesInlayDetailWhileHierarchyUsesPositionState(t *testing.T) {
	tests := []struct {
		name            string
		source          string
		wantDeclaration string
		wantHierarchy   bool
		wantReference   string
	}{
		{
			name: "declaration display includes inferred assignment",
			source: `<% Dim item
Set item = New FutureType
%>`,
			wantDeclaration: "FutureType",
			wantHierarchy:   false,
			wantReference:   "FutureType",
		},
		{
			name: "preceding annotation is active at declaration",
			source: `<%
' @type item As FutureType
Dim item
Set item = New FutureType
%>`,
			wantDeclaration: "FutureType",
			wantHierarchy:   true,
			wantReference:   "FutureType",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			uri := "file:///site/declaration-position-" + strings.ReplaceAll(test.name, " ", "-") + ".asp"
			doc := core.NewTextDocument(uri, "classic-asp", 1, test.source)
			parsed := core.ParseDocument(uri, test.source, core.Settings{DefaultLanguage: "VBScript"})
			server := New(strings.NewReader(""), io.Discard, io.Discard)
			server.documents[uri] = doc
			server.settings.VBScriptComTypes = map[string]vbscriptComTypeSetting{
				"FutureType": {Members: map[string]vbscriptComMemberSetting{}},
			}

			declarationStart := strings.Index(test.source, "Dim item")
			if declarationStart < 0 {
				t.Fatal("declaration token not found")
			}
			declarationOffset := declarationStart + len("Dim ")
			declarationPosition := doc.PositionAt(declarationOffset)
			declarationHover := server.hover(uri, declarationPosition)
			if declarationHover == nil {
				t.Fatal("declaration hover is nil")
			}
			declarationText := mustJSONForTest(t, declarationHover)
			if !strings.Contains(declarationText, "As "+test.wantDeclaration) {
				t.Fatalf("declaration hover = %s, want As %s", declarationText, test.wantDeclaration)
			}
			if test.wantDeclaration == "Variant" && strings.Contains(declarationText, "FutureType") {
				t.Fatalf("declaration hover leaked future type: %s", declarationText)
			}

			hierarchy, ok := server.vbscriptConfiguredTypeHierarchyItemContext(context.Background(), doc, parsed, declarationPosition)
			if ok != test.wantHierarchy {
				t.Fatalf("declaration hierarchy ok = %t, want %t: %#v", ok, test.wantHierarchy, hierarchy)
			}

			referenceOffset := strings.LastIndex(test.source, "item") + len("item") - 1
			referenceHover := server.hover(uri, doc.PositionAt(referenceOffset))
			if referenceHover == nil || !strings.Contains(mustJSONForTest(t, referenceHover), "As "+test.wantReference) {
				t.Fatalf("reference hover = %#v, want As %s", referenceHover, test.wantReference)
			}
			referenceHierarchy, ok := server.vbscriptConfiguredTypeHierarchyItemContext(context.Background(), doc, parsed, doc.PositionAt(referenceOffset))
			if !ok || referenceHierarchy.Name != "FutureType" {
				t.Fatalf("reference hierarchy = %#v, ok=%t, want FutureType", referenceHierarchy, ok)
			}
		})
	}
}

func TestIncludedVBScriptVariableHoverSkipsScopedNameCollisions(t *testing.T) {
	tests := []struct {
		name          string
		includeSource string
		wantType      string
	}{
		{
			name: "parameter before global",
			includeSource: `<%
Function Before(ByVal sharedTitle)
  Response.Write sharedTitle
End Function
sharedTitle = "included"
%>`,
			wantType: "included",
		},
		{
			name: "local before global",
			includeSource: `<%
Function Before()
  Dim sharedTitle
  sharedTitle = "local"
End Function
sharedTitle = "` + "`page-${String}.asp`" + `"
%>`,
			wantType: "`page-${String}.asp`",
		},
		{
			name: "class property before global",
			includeSource: `<%
Class Before
  Property Get sharedTitle()
    sharedTitle = "member"
  End Property
End Class
Dim sharedTitle
sharedTitle = "global"
%>`,
			wantType: "global",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server, parsed, referenceOffset := includedVBScriptHoverFixture(t, test.includeSource)
			hover := server.includedVBScriptVariableHover(parsed, referenceOffset)
			if hover == nil {
				t.Fatal("included variable hover is nil")
			}
			serialized := mustJSONForTest(t, hover)
			if !strings.Contains(serialized, test.wantType) {
				t.Fatalf("included variable hover missing %q: %s", test.wantType, serialized)
			}
			if strings.Contains(serialized, "(local)") {
				t.Fatalf("included variable hover resolved a local declaration: %s", serialized)
			}
		})
	}
}

func TestIncludedVBScriptVariableHoverPreservesIncludePrecedence(t *testing.T) {
	root := t.TempDir()
	ownerURI := filePathURI(filepath.Join(root, "default.asp"))
	firstURI := filePathURI(filepath.Join(root, "first.inc"))
	secondURI := filePathURI(filepath.Join(root, "second.inc"))
	ownerSource := `<!-- #include file="first.inc" -->
<%
Response.Write sharedTitle
%>`
	firstSource := `<!-- #include file="second.inc" -->
<%
Function Before(ByVal sharedTitle)
End Function
sharedTitle = "first"
%>`
	secondSource := `<%
sharedTitle = "second"
%>`
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
	server.documents[firstURI] = core.NewTextDocument(firstURI, "classic-asp", 1, firstSource)
	server.documents[secondURI] = core.NewTextDocument(secondURI, "classic-asp", 1, secondSource)
	parsed := core.ParseDocument(ownerURI, ownerSource, core.Settings{DefaultLanguage: "VBScript"})
	hover := server.includedVBScriptVariableHover(parsed, strings.LastIndex(ownerSource, "sharedTitle"))
	if hover == nil {
		t.Fatal("included variable hover is nil")
	}
	serialized := mustJSONForTest(t, hover)
	if !strings.Contains(serialized, "first") || strings.Contains(serialized, "second") {
		t.Fatalf("include precedence selected the wrong global declaration: %s", serialized)
	}
}

func includedVBScriptHoverFixture(t *testing.T, includeSource string) (*Server, *core.ParsedDocument, int) {
	t.Helper()
	root := t.TempDir()
	ownerURI := filePathURI(filepath.Join(root, "default.asp"))
	includeURI := filePathURI(filepath.Join(root, "common.inc"))
	ownerSource := `<!-- #include file="common.inc" -->
<%
Response.Write sharedTitle
%>`
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
	server.documents[includeURI] = core.NewTextDocument(includeURI, "classic-asp", 1, includeSource)
	return server, core.ParseDocument(ownerURI, ownerSource, core.Settings{DefaultLanguage: "VBScript"}), strings.LastIndex(ownerSource, "sharedTitle")
}
