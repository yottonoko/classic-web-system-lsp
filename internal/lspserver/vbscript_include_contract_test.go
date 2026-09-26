package lspserver

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestVBScriptOwnerGlobalUnionConstrainsIncludedAssignments(t *testing.T) {
	root := t.TempDir()
	ownerPath := filepath.Join(root, "default.asp")
	childPath := filepath.Join(root, "child.inc")
	ownerSource := `<%
' @type state As "ready" | 200
Dim state
state = "ready"
%>
<!-- #include file="child.inc" -->
<%
Response.Write state
%>`
	childSource := `<%
state = True
state = 200
%>`
	if err := os.WriteFile(ownerPath, []byte(ownerSource), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(childPath, []byte(childSource), 0o600); err != nil {
		t.Fatal(err)
	}

	ownerURI := filePathURI(ownerPath)
	childURI := filePathURI(childPath)
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
	server.settings.VBScriptTypeChecking = "strict"
	parsed := core.ParseDocument(ownerURI, ownerSource, core.Settings{DefaultLanguage: "VBScript"})
	server.documents[ownerURI] = core.NewTextDocument(ownerURI, "classic-asp", 1, ownerSource)

	info := server.vbscriptTypeInfo(parsed)
	if got, want := info.variableTypes["state"], `"ready" | 200`; got != want {
		t.Fatalf("owner state type = %q, want %q; info=%#v", got, want, info)
	}

	diagnostics := server.vbscriptTypeDiagnostics(parsed)
	var mismatch []lspDiagnosticFixture
	for _, diagnostic := range diagnostics {
		if diagnostic.Code != "typeMismatch" {
			continue
		}
		data, _ := diagnostic.Data.(map[string]any)
		mismatch = append(mismatch, lspDiagnosticFixture{uri: dataString(data, "uri"), line: diagnostic.Range.Start.Line, actual: dataString(data, "actualType")})
	}
	if len(mismatch) != 1 || mismatch[0].uri != childURI || mismatch[0].actual != "Boolean" {
		t.Fatalf("owner/include mismatches = %#v, want one Boolean mismatch in %s; diagnostics=%#v", mismatch, childURI, diagnostics)
	}
	childDocument := core.NewTextDocument(childURI, "classic-asp", 0, childSource)
	if mismatch[0].line != childDocument.PositionAt(strings.Index(childSource, "True")).Line {
		t.Fatalf("included mismatch line = %d, want child source line; diagnostics=%#v", mismatch[0].line, diagnostics)
	}

	hoverOffset := strings.LastIndex(ownerSource, "state")
	hover := server.includedVBScriptVariableHover(parsed, hoverOffset)
	contents, _ := hoverContentsValue(hover)
	if hover == nil || !strings.Contains(contents, `state As "ready" | 200`) {
		t.Fatalf("owner/include hover = %#v, want owner union", hover)
	}
}

func TestVBScriptIncludeContractsArePerOwner(t *testing.T) {
	root := t.TempDir()
	childPath := filepath.Join(root, "shared.inc")
	if err := os.WriteFile(childPath, []byte(`<%
shared = 1
%>`), 0o600); err != nil {
		t.Fatal(err)
	}
	owners := []struct {
		name     string
		typeName string
	}{
		{name: "first.asp", typeName: "String"},
		{name: "second.asp", typeName: "Number"},
	}
	for _, owner := range owners {
		t.Run(owner.name, func(t *testing.T) {
			ownerPath := filepath.Join(root, owner.name)
			ownerSource := "<%\n' @type shared As " + owner.typeName + "\nDim shared\n%>\n<!-- #include file=\"shared.inc\" -->"
			if err := os.WriteFile(ownerPath, []byte(ownerSource), 0o600); err != nil {
				t.Fatal(err)
			}
			ownerURI := filePathURI(ownerPath)
			server := New(strings.NewReader(""), io.Discard, io.Discard)
			server.rootPath = root
			server.rootURI = filePathURI(root)
			server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
			server.settings.VBScriptTypeChecking = "strict"
			parsed := core.ParseDocument(ownerURI, ownerSource, core.Settings{DefaultLanguage: "VBScript"})
			server.documents[ownerURI] = core.NewTextDocument(ownerURI, "classic-asp", 1, ownerSource)
			info := server.vbscriptTypeInfo(parsed)
			if got := info.variableTypes["shared"]; got != owner.typeName {
				t.Fatalf("owner type = %q, want %q", got, owner.typeName)
			}
			mismatch := false
			for _, diagnostic := range server.vbscriptTypeDiagnostics(parsed) {
				if diagnostic.Code == "typeMismatch" {
					mismatch = true
				}
			}
			if mismatch != (owner.typeName == "String") {
				t.Fatalf("owner %s mismatch = %t, want %t", owner.typeName, mismatch, owner.typeName == "String")
			}
		})
	}
}

func TestVBScriptIncludeOwnerContractSkipsProcedureLocalShadow(t *testing.T) {
	root := t.TempDir()
	ownerPath := filepath.Join(root, "default.asp")
	childPath := filepath.Join(root, "child.inc")
	ownerSource := `<%
' @type shared As String
Dim shared
%>
<!-- #include file="child.inc" -->`
	childSource := `<%
Sub Update()
  Dim shared
  shared = 1
End Sub
%>`
	if err := os.WriteFile(ownerPath, []byte(ownerSource), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(childPath, []byte(childSource), 0o600); err != nil {
		t.Fatal(err)
	}
	ownerURI := filePathURI(ownerPath)
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
	server.settings.VBScriptTypeChecking = "strict"
	parsed := core.ParseDocument(ownerURI, ownerSource, core.Settings{DefaultLanguage: "VBScript"})
	server.documents[ownerURI] = core.NewTextDocument(ownerURI, "classic-asp", 1, ownerSource)

	childURI := filePathURI(childPath)
	child := server.vbscriptIncludedDocumentContext(context.Background(), parsed, parsed.Includes[0])
	if child == nil || child.URI != childURI {
		t.Fatalf("included local fixture = %#v, want %s", child, childURI)
	}
	assignments := vbscriptAssignments(child)
	if len(assignments) != 1 || assignments[0].Scope == "" {
		t.Fatalf("included local assignment scope = %#v, want procedure scope", assignments)
	}
	for _, diagnostic := range server.vbscriptTypeDiagnostics(parsed) {
		if diagnostic.Code == "typeMismatch" {
			t.Fatalf("procedure-local assignment inherited owner contract: %#v", diagnostic)
		}
	}
}

func hoverContentsValue(hover *lsp.Hover) (string, bool) {
	if hover == nil {
		return "", false
	}
	contents, ok := hover.Contents.(lsp.MarkupContent)
	if !ok {
		return "", false
	}
	return contents.Value, true
}

type lspDiagnosticFixture struct {
	uri    string
	line   int
	actual string
}

func dataString(data map[string]any, key string) string {
	if data == nil {
		return ""
	}
	value, _ := data[key].(string)
	return value
}
