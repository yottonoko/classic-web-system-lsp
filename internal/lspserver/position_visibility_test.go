package lspserver

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

func TestVBScriptCompletionAndDefinitionHideFutureDeclarations(t *testing.T) {
	source := `<%
Response.Write futureValue
Call FutureSub()
Response.Write FutureClass
Dim futureValue
Class FutureClass
End Class
Sub FutureSub()
End Sub
%>`
	server, uri, document := positionVisibilityServer(t, source)
	beforeDim := strings.Index(source, "futureValue")
	completion := server.completion(context.Background(), uri, document.PositionAt(beforeDim+len("future")), nil)
	if hasCompletionLabel(completion.Items, "futureValue") {
		t.Fatalf("completion exposed future Dim declaration: %#v", completion.Items)
	}
	if hasCompletionLabel(completion.Items, "FutureClass") {
		t.Fatalf("completion exposed future class declaration: %#v", completion.Items)
	}
	if !hasCompletionLabel(completion.Items, "FutureSub") {
		t.Fatalf("completion omitted forward-call Sub: %#v", completion.Items)
	}
	if locations := server.definitionContext(context.Background(), uri, document.PositionAt(beforeDim+len("futureValue")-1)); locations != nil {
		t.Fatalf("definition exposed future Dim declaration: %#v", locations)
	}
	afterDim := strings.Index(source, "Dim futureValue") + len("Dim ")
	locations := server.definitionContext(context.Background(), uri, document.PositionAt(afterDim+2))
	if len(locations) != 1 || locations[0].Range.Start.Line != 4 {
		t.Fatalf("definition after Dim = %#v, want Dim declaration", locations)
	}
	callOffset := strings.Index(source, "FutureSub")
	locations = server.definitionContext(context.Background(), uri, document.PositionAt(callOffset+2))
	if len(locations) != 1 || locations[0].Range.Start.Line != 7 {
		t.Fatalf("forward-call definition = %#v, want Sub declaration", locations)
	}
}

func TestVBScriptDefinitionRespectsLocalScopeAndSourceOrder(t *testing.T) {
	source := `<%
Sub Render()
Response.Write localValue
Dim localValue
Response.Write localValue
End Sub
%>`
	server, uri, document := positionVisibilityServer(t, source)
	first := strings.Index(source, "localValue")
	completion := server.completion(context.Background(), uri, document.PositionAt(first+2), nil)
	if hasCompletionLabel(completion.Items, "localValue") {
		t.Fatalf("completion exposed future local Dim: %#v", completion.Items)
	}
	if locations := server.definitionContext(context.Background(), uri, document.PositionAt(first+2)); locations != nil {
		t.Fatalf("definition exposed future local Dim: %#v", locations)
	}
	second := strings.LastIndex(source, "localValue")
	locations := server.definitionContext(context.Background(), uri, document.PositionAt(second+2))
	if len(locations) != 1 || locations[0].Range.Start.Line != 3 {
		t.Fatalf("definition after local Dim = %#v, want local declaration", locations)
	}
}

func TestVBScriptCompletionKeepsProcedureLocalsLexicallyScoped(t *testing.T) {
	source := `<%
Dim globalValue
Class GlobalClass
End Class
Sub First()
  Dim firstOnly
  fir
End Sub
Sub Second()
  fir
End Sub
Sub ForwardSub()
End Sub
%>`
	server, uri, document := positionVisibilityServer(t, source)
	secondOffset := strings.LastIndex(source, "fir") + len("fir")
	firstOffset := strings.Index(source, "\n  fir") + len("\n  fir")
	firstCompletion := server.completion(context.Background(), uri, document.PositionAt(firstOffset), nil)
	if !hasCompletionLabel(firstCompletion.Items, "firstOnly") {
		t.Fatalf("same-procedure local missing: %#v", firstCompletion.Items)
	}
	secondCompletion := server.completion(context.Background(), uri, document.PositionAt(secondOffset), nil)
	if hasCompletionLabel(secondCompletion.Items, "firstOnly") {
		t.Fatalf("sibling local leaked: %#v", secondCompletion.Items)
	}
	for _, label := range []string{"globalValue", "GlobalClass", "ForwardSub"} {
		if !hasCompletionLabel(secondCompletion.Items, label) {
			t.Fatalf("visible %s missing from sibling completion: %#v", label, secondCompletion.Items)
		}
	}
}

func TestVBScriptCompletionKeepsPropertyLocalsScopedToAccessor(t *testing.T) {
	source := `<%
Class Widget
  Property Get First()
    Dim firstPropertyOnly
    pro
  End Property
  Property Get Second()
    pro
  End Property
End Class
%>`
	server, uri, document := positionVisibilityServer(t, source)
	firstOffset := strings.Index(source, "\n    pro") + len("\n    pro")
	firstCompletion := server.completion(context.Background(), uri, document.PositionAt(firstOffset), nil)
	if !hasCompletionLabel(firstCompletion.Items, "firstPropertyOnly") {
		t.Fatalf("same-property local missing: %#v", firstCompletion.Items)
	}
	secondOffset := strings.LastIndex(source, "\n    pro") + len("\n    pro")
	secondCompletion := server.completion(context.Background(), uri, document.PositionAt(secondOffset), nil)
	if hasCompletionLabel(secondCompletion.Items, "firstPropertyOnly") {
		t.Fatalf("sibling-property local leaked: %#v", secondCompletion.Items)
	}
}

func TestVBScriptIncludeVisibilityUsesExecutedTransitivePrefix(t *testing.T) {
	root := t.TempDir()
	ownerPath := filepath.Join(root, "default.asp")
	firstPath := filepath.Join(root, "first.inc")
	secondPath := filepath.Join(root, "second.inc")
	ownerSource := `<%
Option Explicit
Response.Write SharedValue
%>
<!-- #include file="first.inc" -->
<% Response.Write SharedValue %>`
	writePositionVisibilityFile(t, ownerPath, ownerSource)
	writePositionVisibilityFile(t, firstPath, `<!-- #include file="second.inc" -->`)
	writePositionVisibilityFile(t, secondPath, `<% Dim SharedValue %>`)
	server, uri, document := positionVisibilityServerWithRoot(t, root, ownerPath, ownerSource)
	beforeInclude := strings.Index(ownerSource, "SharedValue")
	completion := server.completion(context.Background(), uri, document.PositionAt(beforeInclude+2), nil)
	if hasCompletionLabel(completion.Items, "SharedValue") {
		t.Fatalf("completion exposed future transitive include: %#v", completion.Items)
	}
	if locations := server.definitionContext(context.Background(), uri, document.PositionAt(beforeInclude+2)); locations != nil {
		t.Fatalf("definition exposed future include: %#v", locations)
	}
	if locations := server.typeDefinitionContext(context.Background(), uri, document.PositionAt(beforeInclude+2)); locations != nil {
		t.Fatalf("typeDefinition fallback exposed future include: %#v", locations)
	}
	afterInclude := strings.LastIndex(ownerSource, "SharedValue")
	completion = server.completion(context.Background(), uri, document.PositionAt(afterInclude+2), nil)
	if !hasCompletionLabel(completion.Items, "SharedValue") {
		t.Fatalf("completion omitted transitive include declaration: %#v", completion.Items)
	}
	locations := server.definitionContext(context.Background(), uri, document.PositionAt(afterInclude+2))
	if len(locations) != 1 || locations[0].URI != filePathURI(secondPath) {
		t.Fatalf("transitive include definition = %#v, want %q", locations, filePathURI(secondPath))
	}
	locations = server.typeDefinitionContext(context.Background(), uri, document.PositionAt(afterInclude+2))
	if len(locations) != 1 || locations[0].URI != filePathURI(secondPath) {
		t.Fatalf("transitive include typeDefinition fallback = %#v, want %q", locations, filePathURI(secondPath))
	}
}

func TestVBScriptPositionSensitiveRequestsHonorCancellation(t *testing.T) {
	server, uri, document := positionVisibilityServer(t, `<% Dim value : value = 1 %>`)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	position := document.PositionAt(strings.Index(document.Text, "value"))
	if completion := server.completion(cancelled, uri, position, nil); len(completion.Items) != 0 {
		t.Fatalf("cancelled completion = %#v, want empty", completion.Items)
	}
	if locations := server.definitionContext(cancelled, uri, position); locations != nil {
		t.Fatalf("cancelled definition = %#v, want nil", locations)
	}
	parsed := core.ParseDocument(uri, document.Text, core.Settings{DefaultLanguage: "VBScript"})
	if units, complete := server.vbscriptIncludeExecutionUnitsThroughOffsetContextResult(cancelled, parsed, 1); units != nil || complete {
		t.Fatalf("cancelled include prefix = (%#v, %t), want nil and incomplete", units, complete)
	}
}

func positionVisibilityServer(t *testing.T, source string) (*Server, string, *core.TextDocument) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "default.asp")
	writePositionVisibilityFile(t, path, source)
	return positionVisibilityServerWithRoot(t, root, path, source)
}

func positionVisibilityServerWithRoot(t *testing.T, root, path, source string) (*Server, string, *core.TextDocument) {
	t.Helper()
	uri := filePathURI(path)
	document := core.NewTextDocument(uri, "classic-asp", 1, source)
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
	server.documents[uri] = document
	return server, uri, document
}

func writePositionVisibilityFile(t *testing.T, path, source string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
}
