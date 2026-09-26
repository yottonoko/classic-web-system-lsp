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

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestVBScriptIncludeExpansionBoundsSharedFanoutAndCancellation(t *testing.T) {
	root := t.TempDir()
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
	const depth = 14
	for index := depth; index >= 1; index-- {
		name := fmt.Sprintf("level-%d.inc", index)
		next := ""
		if index > 1 {
			next = fmt.Sprintf("<!-- #include file=\"level-%d.inc\" -->\n<!-- #include file=\"level-%d.inc\" -->\n", index-1, index-1)
		}
		if err := os.WriteFile(filepath.Join(root, name), []byte(next+"<% value = \"level\" %>"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "default.asp"), []byte("<!-- #include file=\"level-14.inc\" -->\n<!-- #include file=\"level-14.inc\" -->\n<% value = \"root\" %>"), 0o600); err != nil {
		t.Fatal(err)
	}
	ownerURI := filePathURI(filepath.Join(root, "default.asp"))
	ownerSource, err := os.ReadFile(filepath.Join(root, "default.asp"))
	if err != nil {
		t.Fatal(err)
	}
	parsed := core.ParseDocument(ownerURI, string(ownerSource), core.Settings{DefaultLanguage: "VBScript"})
	units, complete := server.vbscriptIncludeExecutionUnitsContext(context.Background(), parsed)
	if complete {
		t.Fatalf("fanout expansion unexpectedly completed with %d units", len(units))
	}
	if len(units) > includeExpansionUnitBudget {
		t.Fatalf("fanout expansion materialized %d units, budget is %d", len(units), includeExpansionUnitBudget)
	}
	included := server.includedDocumentsContext(context.Background(), parsed)
	if len(included) > depth {
		t.Fatalf("fanout included-document index materialized %d documents, want at most %d", len(included), depth)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	units, complete = server.vbscriptIncludeExecutionUnitsContext(cancelled, parsed)
	if complete || len(units) != 0 {
		t.Fatalf("cancelled include expansion = (%d units, complete=%v), want no publication", len(units), complete)
	}
	if included := server.includedDocumentsContext(cancelled, parsed); included != nil {
		t.Fatalf("cancelled included-document index = %#v, want no publication", included)
	}
}

func TestVBScriptIncompleteIncludeExpansionDoesNotPublishPrefixTypes(t *testing.T) {
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
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("level-%d.inc", index)), []byte(next+"<% value = New PrefixType %>"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ownerSource := "<!-- #include file=\"level-14.inc\" -->\n<!-- #include file=\"level-14.inc\" -->\n<% value = New PostIncludeType %>"
	ownerPath := filepath.Join(root, "default.asp")
	if err := os.WriteFile(ownerPath, []byte(ownerSource), 0o600); err != nil {
		t.Fatal(err)
	}
	ownerURI := filePathURI(ownerPath)
	parsed := core.ParseDocument(ownerURI, ownerSource, core.Settings{DefaultLanguage: "VBScript"})
	info := server.vbscriptTypeInfo(parsed)
	if got := info.variableTypes["value"]; got != "" {
		t.Fatalf("incomplete include expansion published stale prefix type %q", got)
	}
	if got := server.includedVBScriptVariableHover(parsed, strings.LastIndex(ownerSource, "value")); got != nil {
		t.Fatalf("incomplete include expansion published included hover: %#v", got)
	}
}

func TestIncludeAwareRequestsDropResultsAfterLiveCancellation(t *testing.T) {
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
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("level-%d.inc", index)), []byte(next+"<% value = New PrefixType %>"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ownerSource := "<!-- #include file=\"level-14.inc\" -->\n<!-- #include file=\"level-14.inc\" -->\n<% value = New PostIncludeType %>"
	ownerPath := filepath.Join(root, "default.asp")
	if err := os.WriteFile(ownerPath, []byte(ownerSource), 0o600); err != nil {
		t.Fatal(err)
	}
	ownerURI := filePathURI(ownerPath)
	server.documents[ownerURI] = core.NewTextDocument(ownerURI, "classic-asp", 1, ownerSource)
	position := core.NewTextDocument(ownerURI, "classic-asp", 0, ownerSource).PositionAt(strings.LastIndex(ownerSource, "value"))
	params := mustRaw(map[string]any{
		"textDocument": map[string]any{"uri": ownerURI},
		"position":     position,
	})
	for _, method := range []string{"textDocument/hover", "textDocument/definition", "textDocument/typeDefinition"} {
		t.Run(method, func(t *testing.T) {
			base, cancel := context.WithCancel(context.Background())
			defer cancel()
			ctx := &cancelAfterErrContext{Context: base, cancel: cancel, limit: 1000}
			result, rpcErr := server.handleRequest(ctx, method, params)
			if rpcErr != nil {
				t.Fatalf("cancelled %s request error = %#v", method, rpcErr)
			}
			switch value := result.(type) {
			case nil:
			case []lsp.Location:
				if value != nil {
					t.Fatalf("cancelled %s request published partial result: %#v", method, result)
				}
			default:
				t.Fatalf("cancelled %s request published partial result: %#v", method, result)
			}
			if ctx.checks.Load() < ctx.limit {
				t.Fatalf("%s request did not cancel during bounded traversal: checks=%d limit=%d", method, ctx.checks.Load(), ctx.limit)
			}
		})
	}
}

type cancelAfterErrContext struct {
	context.Context
	cancel context.CancelFunc
	checks atomic.Int64
	limit  int64
}

func (c *cancelAfterErrContext) Err() error {
	checks := c.checks.Add(1)
	if checks >= c.limit {
		c.cancel()
	}
	return c.Context.Err()
}

func TestIncludedVBScriptHoverFollowsNestedIncludeExecutionOrder(t *testing.T) {
	tests := []struct {
		name        string
		ownerSource string
		wantType    string
	}{
		{
			name: "nested include supersedes parent assignment before include",
			ownerSource: `<%
Set shared = New Parent
%>
<!-- #include file="child.inc" -->
<% Response.Write shared %>`,
			wantType: "Nested",
		},
		{
			name: "parent assignment supersedes nested include",
			ownerSource: `<!-- #include file="child.inc" -->
<%
Set shared = New Parent
Response.Write shared
%>`,
			wantType: "Parent",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server, parsed, offset := includeExecutionOrderFixture(t, test.ownerSource, `<!-- #include file="nested.inc" -->`, `<% Set shared = New Nested %>`)
			hover := server.includedVBScriptVariableHover(parsed, offset)
			if hover == nil {
				t.Fatal("included variable hover is nil")
			}
			serialized := mustJSONForTest(t, hover)
			if !strings.Contains(serialized, "shared As "+test.wantType) {
				t.Fatalf("included variable hover = %s, want %s", serialized, test.wantType)
			}
			position := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text).PositionAt(offset)
			fullHover := server.hover(parsed.URI, position)
			if fullHover == nil || !strings.Contains(mustJSONForTest(t, fullHover), "shared As "+test.wantType) {
				t.Fatalf("full variable hover = %#v, want %s", fullHover, test.wantType)
			}
		})
	}
}

func TestVBScriptTypeInfoFollowsNestedIncludeExecutionOrder(t *testing.T) {
	tests := []struct {
		name        string
		ownerSource string
		wantType    string
	}{
		{
			name: "nested include supersedes parent assignment before include",
			ownerSource: `<%
Set shared = New Parent
%>
<!-- #include file="child.inc" -->
<% Response.Write shared %>`,
			wantType: "Nested",
		},
		{
			name: "parent assignment supersedes nested include",
			ownerSource: `<!-- #include file="child.inc" -->
<%
Set shared = New Parent
Response.Write shared
%>`,
			wantType: "Parent",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server, parsed, _ := includeExecutionOrderFixture(t, test.ownerSource, `<!-- #include file="nested.inc" -->`, `<% Set shared = New Nested %>`)
			info := server.vbscriptTypeInfo(parsed)
			if got := info.variableTypes["shared"]; got != test.wantType {
				t.Fatalf("shared type = %q, want %q; all types = %#v", got, test.wantType, info.variableTypes)
			}
		})
	}
}

func TestVBScriptUnknownAssignmentClearsIncludedInferredState(t *testing.T) {
	server, parsed, _ := includeExecutionOrderFixture(t,
		`<!-- #include file="child.inc" -->
<% Response.Write shared %>`,
		`<%
Set shared = New First
%>
<!-- #include file="nested.inc" -->`,
		`<% Set shared = ResolveAtRuntime() %>`,
	)

	info := server.vbscriptTypeInfo(parsed)
	if _, ok := info.variableTypes["shared"]; ok {
		t.Fatalf("dynamic include retained legacy inferred type: %#v", info.variableTypes)
	}
	if _, ok := info.variableTypeExpressions["shared"]; ok {
		t.Fatalf("dynamic include retained legacy expression type: %#v", info.variableTypeExpressions)
	}
	key := vbscriptTypeScopeKey(parsed, "", "shared")
	if _, ok := info.scopedVariableTypeNames[key]; ok {
		t.Fatalf("dynamic include retained root scoped type: %#v", info.scopedVariableTypeNames)
	}
	var child *core.ParsedDocument
	for _, unit := range mustIncludeUnits(t, server, parsed) {
		if unit.document != nil && strings.HasSuffix(unit.document.URI, "/child.inc") {
			child = unit.document
			break
		}
	}
	if child == nil {
		t.Fatal("child include document was not expanded")
	}
	childKey := vbscriptTypeScopeKey(child, "", "shared")
	if _, ok := info.scopedVariableTypeNames[childKey]; ok {
		t.Fatalf("dynamic include retained child scoped type: %#v", info.scopedVariableTypeNames)
	}
	if _, ok := info.scopedVariableTypes[childKey]; ok {
		t.Fatalf("dynamic include retained child structured type: %#v", info.scopedVariableTypes)
	}
}

func TestVBScriptUnknownAssignmentPreservesOwnerGlobalContract(t *testing.T) {
	server, parsed, _ := includeExecutionOrderFixture(t,
		`<%
' @type shared As First
Dim shared
%>
<!-- #include file="child.inc" -->
<% Response.Write shared %>`,
		`<% Set shared = ResolveAtRuntime() %>`,
		"",
	)

	info := server.vbscriptTypeInfo(parsed)
	if got := info.variableTypes["shared"]; got != "First" {
		t.Fatalf("dynamic include replaced owner contract: %q; all types = %#v", got, info.variableTypes)
	}
	if got := info.scopedExplicitTypeNames[vbscriptTypeScopeKey(parsed, "", "shared")]; got != "First" {
		t.Fatalf("owner scoped explicit type = %q, want First", got)
	}
	if got := info.scopedVariableTypeNames[vbscriptTypeScopeKey(parsed, "", "shared")]; got != "First" {
		t.Fatalf("owner scoped effective type = %q, want First", got)
	}
}

func TestVBScriptUnknownAssignmentResetsPriorUnionBeforeLaterKnownAssignment(t *testing.T) {
	server, parsed, _ := includeExecutionOrderFixture(t,
		`<!-- #include file="child.inc" -->
<% Response.Write shared %>`,
		`<%
Set shared = New First
%>
<!-- #include file="nested.inc" -->
<% Set shared = New Second %>`,
		`<% Set shared = ResolveAtRuntime() %>`,
	)

	info := server.vbscriptTypeInfo(parsed)
	if got := info.variableTypes["shared"]; got != "Second" {
		t.Fatalf("later known assignment retained pre-dynamic union: %q; all types = %#v", got, info.variableTypes)
	}
}

func TestVBScriptUnknownLocalAssignmentDoesNotClearGlobalInference(t *testing.T) {
	source := `<%
Set shared = New Global
Sub Uses()
  Dim shared
  Set shared = New Local
  Set shared = ResolveAtRuntime()
  Response.Write shared
End Sub
%>`
	parsed := core.ParseDocument("file:///site/dynamic-local.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	info := server.vbscriptTypeInfo(parsed)
	if got := info.variableTypes["shared"]; got != "Global" {
		t.Fatalf("dynamic local assignment cleared global type: %q; all types = %#v", got, info.variableTypes)
	}
	scope := vbscriptScopeAtOffset(parsed, strings.LastIndex(source, "Response.Write shared"))
	if scope == "" {
		t.Fatal("local procedure scope was not detected")
	}
	if _, ok := info.scopedVariableTypeNames[vbscriptTypeScopeKey(parsed, scope, "shared")]; ok {
		t.Fatalf("dynamic local assignment retained local type: %#v", info.scopedVariableTypeNames)
	}
}

func TestVBScriptUnknownExplicitLocalAssignmentPreservesContract(t *testing.T) {
	source := `<%
Sub Uses()
  ' @type shared As First
  Dim shared
  Set shared = New First
  Set shared = ResolveAtRuntime()
  Response.Write shared
End Sub
%>`
	parsed := core.ParseDocument("file:///site/dynamic-explicit-local.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	info := server.vbscriptTypeInfo(parsed)
	scope := vbscriptScopeAtOffset(parsed, strings.LastIndex(source, "Response.Write shared"))
	if got := info.scopedExplicitTypeNames[vbscriptTypeScopeKey(parsed, scope, "shared")]; got != "First" {
		t.Fatalf("explicit local contract = %q, want First", got)
	}
	if got := info.scopedVariableTypeNames[vbscriptTypeScopeKey(parsed, scope, "shared")]; got != "First" {
		t.Fatalf("dynamic explicit local assignment replaced contract: %q", got)
	}
}

func TestVBScriptCompletionDropsEarlierIncludeClassAfterDynamicAssignment(t *testing.T) {
	server, parsed, offset := dynamicIncludeCompletionFixture(t, `<%
Response.Write shared.OnlyFirst
%>`, map[string]string{
		"first.inc": `<%
Class First
  Public OnlyFirst
End Class
Set shared = New First
%>`,
		"dynamic.inc": `<% Set shared = ResolveAtRuntime() %>`,
	})
	items := server.vbscriptTypedMemberCompletions(parsed, "shared", offset)
	if len(items) != 0 {
		t.Fatalf("dynamic include retained earlier class members: %#v", items)
	}
}

func TestVBScriptCompletionPreservesOwnerContractAfterDynamicAssignment(t *testing.T) {
	server, parsed, offset := dynamicIncludeCompletionFixture(t, `<%
' @type shared As First
Dim shared
Response.Write shared.OnlyFirst
%>`, map[string]string{
		"first.inc": `<%
Class First
  Public OnlyFirst
End Class
%>`,
		"dynamic.inc": `<% Set shared = ResolveAtRuntime() %>`,
	})
	items := server.vbscriptTypedMemberCompletions(parsed, "shared", offset)
	if !completionHasLabel(items, "OnlyFirst") {
		t.Fatalf("dynamic include dropped explicit owner contract: %#v", items)
	}
}

func TestVBScriptCompletionUsesLaterKnownIncludeAfterDynamicAssignment(t *testing.T) {
	server, parsed, offset := dynamicIncludeCompletionFixture(t, `<%
Response.Write shared.OnlySecond
%>`, map[string]string{
		"first.inc": `<%
Class First
  Public OnlyFirst
End Class
Set shared = New First
%>`,
		"dynamic.inc": `<% Set shared = ResolveAtRuntime() %>`,
		"second.inc": `<%
Class Second
  Public OnlySecond
End Class
Set shared = New Second
%>`,
	})
	items := server.vbscriptTypedMemberCompletions(parsed, "shared", offset)
	if !completionHasLabel(items, "OnlySecond") || completionHasLabel(items, "OnlyFirst") {
		t.Fatalf("later known include completion = %#v, want only Second members", items)
	}
}

func dynamicIncludeCompletionFixture(t *testing.T, trailingSource string, files map[string]string) (*Server, *core.ParsedDocument, int) {
	t.Helper()
	root := t.TempDir()
	ownerPath := filepath.Join(root, "default.asp")
	includeNames := make([]string, 0, len(files))
	for _, name := range []string{"first.inc", "dynamic.inc", "second.inc"} {
		source, ok := files[name]
		if !ok {
			continue
		}
		includeNames = append(includeNames, name)
		if err := os.WriteFile(filepath.Join(root, name), []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var builder strings.Builder
	for _, name := range includeNames {
		builder.WriteString(`<!-- #include file="`)
		builder.WriteString(name)
		builder.WriteString(`" -->
`)
	}
	trailingStart := builder.Len()
	builder.WriteString(trailingSource)
	ownerSource := builder.String()
	if err := os.WriteFile(ownerPath, []byte(ownerSource), 0o600); err != nil {
		t.Fatal(err)
	}
	ownerURI := filePathURI(ownerPath)
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
	parsed := core.ParseDocument(ownerURI, ownerSource, core.Settings{DefaultLanguage: "VBScript"})
	return server, parsed, trailingStart + strings.Index(trailingSource, "shared.") + len("shared.")
}

func mustIncludeUnits(t *testing.T, server *Server, parsed *core.ParsedDocument) []vbscriptIncludeExecutionUnit {
	t.Helper()
	units, complete := server.vbscriptIncludeExecutionUnitsContext(context.Background(), parsed)
	if !complete {
		t.Fatal("include expansion was incomplete")
	}
	return units
}

func TestIncludedVBScriptHoverKeepsSiblingCycleAndLocalFilteringDeterministic(t *testing.T) {
	root := t.TempDir()
	ownerURI := filePathURI(filepath.Join(root, "default.asp"))
	ownerSource := `<!-- #include file="first.inc" -->
<!-- #include file="second.inc" -->
<!-- #include file="first.inc" -->
<% Response.Write shared %>`
	files := map[string]string{
		"first.inc": `<!-- #include file="cycle.inc" -->
<%
Function LocalValue()
  Dim shared
  shared = New Local
End Function
shared = New First
%>`,
		"second.inc": `<% shared = New Second %>`,
		"cycle.inc": `<!-- #include file="first.inc" -->
<% shared = New Cycle %>`,
	}
	for name, source := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
	parsed := core.ParseDocument(ownerURI, ownerSource, core.Settings{DefaultLanguage: "VBScript"})
	offset := strings.LastIndex(ownerSource, "shared")
	candidate, ok := server.vbscriptIncludedGlobalHoverCandidate(parsed, offset, "shared")
	if !ok {
		t.Fatal("included global hover candidate is missing")
	}
	if candidate.typeName != "First" || !strings.HasSuffix(candidate.document.URI, "first.inc") {
		t.Fatalf("sibling/repeated/cycle candidate = %#v, want first.inc As First", candidate)
	}
	info := server.vbscriptTypeInfo(parsed)
	if got := info.variableTypes["shared"]; got != "First" {
		t.Fatalf("sibling/repeated/cycle type = %q, want First", got)
	}
}

func includeExecutionOrderFixture(t *testing.T, ownerSource, childSource, nestedSource string) (*Server, *core.ParsedDocument, int) {
	t.Helper()
	root := t.TempDir()
	ownerURI := filePathURI(filepath.Join(root, "default.asp"))
	childURI := filepath.Join(root, "child.inc")
	nestedURI := filepath.Join(root, "nested.inc")
	if err := os.WriteFile(childURI, []byte(childSource), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nestedURI, []byte(nestedSource), 0o644); err != nil {
		t.Fatal(err)
	}
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	server.workspaceRoots = []workspaceRoot{{Path: root, URI: server.rootURI}}
	parsed := core.ParseDocument(ownerURI, ownerSource, core.Settings{DefaultLanguage: "VBScript"})
	server.documents[ownerURI] = core.NewTextDocument(ownerURI, "classic-asp", 1, ownerSource)
	return server, parsed, strings.LastIndex(ownerSource, "shared")
}
