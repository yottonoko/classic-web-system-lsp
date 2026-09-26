package lspserver

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

func TestVBScriptPositionSensitiveRequestsUseContractActivationOrder(t *testing.T) {
	root := t.TempDir()
	firstSource := `<%
Class First
  Property Get FirstOnly()
  End Property
End Class
Set state = New First
%>`
	secondSource := `<%
Class Second
  Property Get SecondOnly()
  End Property
End Class
Set state = New Second
%>`
	ownerSource := `<!-- #include file="first.inc" -->
<%
Response.Write "日本語"
Response.Write state
Response.Write state.FirstOnly
%>
<%
' @type state As Second
Dim state
%>
<!-- #include file="second.inc" -->
<%
Response.Write state.SecondOnly
%>`
	writeTypeDefinitionFile(t, filepath.Join(root, "first.inc"), firstSource)
	writeTypeDefinitionFile(t, filepath.Join(root, "second.inc"), secondSource)
	ownerPath := filepath.Join(root, "default.asp")
	writeTypeDefinitionFile(t, ownerPath, ownerSource)
	server, ownerURI := typeDefinitionFixtureServer(t, root, ownerPath, ownerSource)
	document := core.NewTextDocument(ownerURI, "classic-asp", 0, ownerSource)

	beforeTypeOffset := strings.Index(ownerSource, "state\n")
	if beforeTypeOffset < 0 {
		t.Fatal("before-contract state reference not found")
	}
	afterTypeOffset := strings.LastIndex(ownerSource, "state.SecondOnly")
	if afterTypeOffset < 0 {
		t.Fatal("after-contract state reference not found")
	}
	for _, test := range []struct {
		name       string
		offset     int
		wantHover  string
		wantMember string
		wantClass  string
	}{
		{name: "before contract", offset: beforeTypeOffset, wantHover: "First", wantMember: "FirstOnly", wantClass: "first.inc"},
		{name: "after contract", offset: afterTypeOffset, wantHover: "Second", wantMember: "SecondOnly", wantClass: "second.inc"},
	} {
		t.Run(test.name, func(t *testing.T) {
			position := document.PositionAt(test.offset)
			hover := server.hover(ownerURI, position)
			if hover == nil || !strings.Contains(mustJSONForTest(t, hover), "state As "+test.wantHover) {
				t.Fatalf("hover = %#v, want state As %s", hover, test.wantHover)
			}

			memberExpression := "state." + test.wantMember
			memberOffset := strings.Index(ownerSource, memberExpression)
			if test.name == "after contract" {
				memberOffset = strings.LastIndex(ownerSource, memberExpression)
			}
			if memberOffset < 0 {
				t.Fatalf("member expression %q not found", memberExpression)
			}
			memberHover := server.hover(ownerURI, document.PositionAt(memberOffset+len("state.")))
			if memberHover == nil || !strings.Contains(mustJSONForTest(t, memberHover), test.wantMember) {
				t.Fatalf("member hover = %#v, want %s", memberHover, test.wantMember)
			}

			completionOffset := memberOffset + len("state.")
			completion := server.completion(context.Background(), ownerURI, document.PositionAt(completionOffset), nil)
			if !hasCompletionLabel(completion.Items, test.wantMember) {
				t.Fatalf("completion labels = %#v, want %s", completion.Items, test.wantMember)
			}
			unexpectedMember := "SecondOnly"
			if test.name == "after contract" {
				unexpectedMember = "FirstOnly"
			}
			if hasCompletionLabel(completion.Items, unexpectedMember) {
				t.Fatalf("completion exposed future member %s: %#v", unexpectedMember, completion.Items)
			}

			typeLocations := server.typeDefinitionContext(context.Background(), ownerURI, position)
			if len(typeLocations) != 1 || typeLocations[0].URI != filePathURI(filepath.Join(root, test.wantClass)) {
				t.Fatalf("typeDefinition = %#v, want %s", typeLocations, test.wantClass)
			}
		})
	}
}

func TestVBScriptFullReplayActivatesFutureContractWithoutReinterpretingCopies(t *testing.T) {
	source := `<%
Class First
End Class
Class Second
End Class
Set source = New First
Set snapshot = source
' @type source As Second
Dim source
%>`
	parsed := core.ParseDocument("file:///site/future-contract-replay.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	server := New(strings.NewReader(""), io.Discard, io.Discard)

	full := server.vbscriptTypeInfo(parsed)
	if got := full.variableTypes["source"]; got != "Second" {
		t.Fatalf("final source type = %q, want Second: %#v", got, full.variableTypes)
	}
	if got := full.variableTypes["snapshot"]; got != "First" {
		t.Fatalf("copy type = %q, want First: %#v", got, full.variableTypes)
	}

	contractOffset := strings.Index(source, "' @type source")
	if contractOffset < 0 {
		t.Fatal("future contract not found")
	}
	prefix, complete := server.vbscriptTypeInfoAtOffsetContext(context.Background(), parsed, contractOffset)
	if !complete {
		t.Fatal("prefix type replay was incomplete")
	}
	if got := prefix.variableTypes["source"]; got != "First" {
		t.Fatalf("prefix source type = %q, want First: %#v", got, prefix.variableTypes)
	}
	if got := prefix.variableTypes["snapshot"]; got != "First" {
		t.Fatalf("prefix copy type = %q, want First: %#v", got, prefix.variableTypes)
	}
}

func TestVBScriptGlobalContractsExcludeInferredScalarState(t *testing.T) {
	root := t.TempDir()
	ownerSource := `<% Dim state
state = "ready" %>
<!-- #include file="dynamic.inc" -->`
	dynamicSource := `<% state = ResolveAtRuntime() %>`
	ownerPath := filepath.Join(root, "default.asp")
	writeTypeDefinitionFile(t, ownerPath, ownerSource)
	writeTypeDefinitionFile(t, filepath.Join(root, "dynamic.inc"), dynamicSource)
	server, ownerURI := typeDefinitionFixtureServer(t, root, ownerPath, ownerSource)
	parsed := core.ParseDocument(ownerURI, ownerSource, core.Settings{DefaultLanguage: "VBScript"})
	info := server.vbscriptTypeInfo(parsed)
	if _, ok := info.globalContracts["state"]; ok {
		t.Fatalf("inferred scalar state became a global contract: %#v", info.globalContracts["state"])
	}
	if _, ok := info.variableTypes["state"]; ok {
		t.Fatalf("dynamic include retained inferred scalar state: %#v", info.variableTypes)
	}
}

func TestVBScriptExplicitGlobalContractSurvivesDynamicInclude(t *testing.T) {
	root := t.TempDir()
	ownerSource := `<%
' @type state As First
Dim state
%>
<!-- #include file="dynamic.inc" -->
<% state %>`
	dynamicSource := `<% state = ResolveAtRuntime() %>`
	ownerPath := filepath.Join(root, "default.asp")
	writeTypeDefinitionFile(t, ownerPath, ownerSource)
	writeTypeDefinitionFile(t, filepath.Join(root, "dynamic.inc"), dynamicSource)
	server, ownerURI := typeDefinitionFixtureServer(t, root, ownerPath, ownerSource)
	parsed := core.ParseDocument(ownerURI, ownerSource, core.Settings{DefaultLanguage: "VBScript"})
	info := server.vbscriptTypeInfo(parsed)
	if got := info.globalContracts["state"].TypeName; got != "First" {
		t.Fatalf("explicit global contract = %q, want First", got)
	}
	if got := info.variableTypes["state"]; got != "First" {
		t.Fatalf("dynamic include replaced explicit contract with %q", got)
	}
}
