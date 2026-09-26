package lspserver

import (
	"reflect"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
)

func TestReadOnlyLSPAnalysesPreserveSharedSourceCoordinates(t *testing.T) {
	const source = `😀<%
Option Explicit
Sub Render(value)
  Response.Write missingValue
End Sub
Call Render 1
%>
<div>after</div>`
	settings := core.Settings{DefaultLanguage: "VBScript"}
	parsed := core.ParseDocument("file:///shared-lsp-source.asp", source, settings)
	shared := core.SourceDocument(parsed)
	if shared == nil {
		t.Fatal("SourceDocument returned nil")
	}

	diagnostics := vbscript.SyntaxDiagnostics(parsed, vbscript.SyntaxOptions{IfSyntaxDiagnostics: "strict"})
	sections := flowchartSections(parsed)
	facts := legacyUndefinedGlobalFacts(parsed)
	fixes := callSyntaxFixes(parsed)
	scopes := vbProcedureScopes(parsed)
	if len(fixes) != 1 || len(scopes) != 1 {
		t.Fatalf("expected one call fix and procedure scope, got %#v and %#v", fixes, scopes)
	}
	if got := core.SourceDocument(parsed); got != shared {
		t.Fatal("read-only LSP analyses replaced the shared source document")
	}

	fresh := core.ParseDocument(parsed.URI, source, settings)
	wantDiagnostics := vbscript.SyntaxDiagnostics(fresh, vbscript.SyntaxOptions{IfSyntaxDiagnostics: "strict"})
	wantSections := flowchartSections(fresh)
	wantFacts := legacyUndefinedGlobalFacts(fresh)
	if want := callSyntaxFixes(fresh); !reflect.DeepEqual(fixes, want) {
		t.Fatalf("call fixes differ from fresh document: got %#v want %#v", fixes, want)
	}
	if want := vbProcedureScopes(fresh); !reflect.DeepEqual(scopes, want) {
		t.Fatalf("procedure scopes differ from fresh document: got %#v want %#v", scopes, want)
	}
	if !reflect.DeepEqual(diagnostics, wantDiagnostics) {
		t.Fatalf("syntax diagnostics differ from fresh document: got %#v want %#v", diagnostics, wantDiagnostics)
	}
	if !reflect.DeepEqual(sections, wantSections) {
		t.Fatalf("flowchart sections differ from fresh document: got %#v want %#v", sections, wantSections)
	}
	if !reflect.DeepEqual(facts, wantFacts) {
		t.Fatalf("legacy undefined-global facts differ from fresh document: got %#v want %#v", facts, wantFacts)
	}

	freshDocument := core.NewTextDocument(parsed.URI, "classic-asp", 0, source)
	for offset := 0; offset <= len(source); offset++ {
		if got, want := shared.PositionAt(offset), freshDocument.PositionAt(offset); got != want {
			t.Fatalf("shared PositionAt(%d) = %#v, want %#v", offset, got, want)
		}
	}
}
