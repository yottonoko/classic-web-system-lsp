package lspserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

func TestNavigationVBInfersFiniteRedirectTemplatesAndFunctions(t *testing.T) {
	const source = `<%
Const base = "next.asp"
id = Request.QueryString("id")
Function BuildTarget()
  BuildTarget = base & "?id=" & Server.URLEncode(id)
End Function
Response.Redirect BuildTarget()
%>`
	candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
	if len(candidates) != 1 {
		t.Fatalf("navigation candidates = %#v", candidates)
	}
	redirect := candidates[0]
	if redirect.Kind != "redirect" || redirect.Value.Kind != navigationVBValueTemplate || redirect.Value.Text != "next.asp?id={queryString:id}" || redirect.Value.confidence() != "possible" {
		t.Fatalf("redirect template = %#v", redirect)
	}
	if len(redirect.Value.Parameters) != 1 || redirect.Value.Parameters[0]["name"] != "id" || redirect.Value.Parameters[0]["source"] != "queryString" {
		t.Fatalf("redirect parameters = %#v", redirect.Value.Parameters)
	}
	if redirect.Range.Start.Line != 6 || redirect.Range.End.Line != 6 {
		t.Fatalf("redirect range = %#v", redirect.Range)
	}
}

func TestNavigationVBProducesOneEdgePerFiniteAssignedTarget(t *testing.T) {
	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	for _, name := range []string{"new.asp", "old.asp"} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	source := `<%
If enabled Then
  target = "new.asp"
Else
  target = "old.asp"
End If
Response.Redirect target
%>`
	parsed := core.ParseDocument(filePathURI(page), source, core.Settings{})
	builder := newNavigationGraphBuilder("document", parsed.URI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	builder.addDocument(parsed, parsed.URI)
	payload := builder.payload(1)
	for _, name := range []string{"new.asp", "old.asp"} {
		if !navigationPayloadNodeMatching(payload, func(node map[string]any) bool { return node["label"] == name }) {
			t.Fatalf("finite navigation target %s missing: %#v", name, payload)
		}
	}
	edges, _ := payload["edges"].([]map[string]any)
	if len(edges) != 2 {
		t.Fatalf("finite branch edges = %d, want 2: %#v", len(edges), payload)
	}
}

func TestNavigationVBSingleLineIfProducesOneEdgePerBranch(t *testing.T) {
	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	for _, name := range []string{"new.asp", "old.asp"} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	source := `<%
If enabled Then target = "new.asp" Else target = "old.asp"
Response.Redirect target
%>`
	parsed := core.ParseDocument(filePathURI(page), source, core.Settings{})
	builder := newNavigationGraphBuilder("document", parsed.URI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	builder.addDocument(parsed, parsed.URI)
	for _, name := range []string{"new.asp", "old.asp"} {
		if !navigationPayloadNodeMatching(builder.payload(1), func(node map[string]any) bool { return node["label"] == name }) {
			t.Fatalf("single-line If target %s missing: %#v", name, builder.payload(1))
		}
	}
	if len(builder.edges) != 2 {
		t.Fatalf("single-line If edges = %d, want 2: %#v", len(builder.edges), builder.edges)
	}
}

func TestNavigationVBSelectCaseProducesOneEdgePerCaseIncludingElse(t *testing.T) {
	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	for _, name := range []string{"one.asp", "two.asp", "fallback.asp"} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	source := `<%
Select Case mode
Case 1
  target = "one.asp"
Case 2
  target = "two.asp"
Case Else
  target = "fallback.asp"
End Select
Response.Redirect target
%>`
	parsed := core.ParseDocument(filePathURI(page), source, core.Settings{})
	builder := newNavigationGraphBuilder("document", parsed.URI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	builder.addDocument(parsed, parsed.URI)
	for _, name := range []string{"one.asp", "two.asp", "fallback.asp"} {
		if !navigationPayloadNodeMatching(builder.payload(1), func(node map[string]any) bool { return node["label"] == name }) {
			t.Fatalf("Select Case target %s missing: %#v", name, builder.payload(1))
		}
	}
	if len(builder.edges) != 3 {
		t.Fatalf("Select Case edges = %d, want 3: %#v", len(builder.edges), builder.edges)
	}
}

func TestNavigationVBIIfProducesFiniteExpressionValues(t *testing.T) {
	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	for _, name := range []string{"new.asp", "old.asp"} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	source := `<%
target = IIf(enabled, "new.asp", "old.asp")
Response.Redirect target
%>`
	parsed := core.ParseDocument(filePathURI(page), source, core.Settings{})
	builder := newNavigationGraphBuilder("document", parsed.URI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	builder.addDocument(parsed, parsed.URI)
	for _, name := range []string{"new.asp", "old.asp"} {
		if !navigationPayloadNodeMatching(builder.payload(1), func(node map[string]any) bool { return node["label"] == name }) {
			t.Fatalf("IIf target %s missing: %#v", name, builder.payload(1))
		}
	}
	if len(builder.edges) != 2 {
		t.Fatalf("IIf edges = %d, want 2: %#v", len(builder.edges), builder.edges)
	}
}

func TestNavigationVBRedirectParameterMetadataIsOwnedPerEdge(t *testing.T) {
	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	for _, name := range []string{"first.asp", "second.asp"} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	source := `<%
id = Request.QueryString("id")
Response.Redirect "first.asp?id=" & id
Response.Redirect "second.asp?id=" & id
%>`
	parsed := core.ParseDocument(filePathURI(page), source, core.Settings{})
	builder := newNavigationGraphBuilder("document", parsed.URI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	builder.addDocument(parsed, parsed.URI)
	if len(builder.edges) != 2 {
		t.Fatalf("redirect edges = %#v, want two", builder.edges)
	}
	wantUsage := map[string]string{
		"first.asp":  "first.asp?id={queryString:id}",
		"second.asp": "second.asp?id={queryString:id}",
	}
	for _, edge := range builder.edges {
		targetID, _ := edge["target"].(string)
		target := builder.nodeByID[targetID]
		label := navigationString(target["label"])
		parameters, _ := edge["parameters"].([]map[string]any)
		if len(parameters) == 0 {
			t.Fatalf("redirect %s has no parameters: %#v", label, edge)
		}
		found := false
		for _, parameter := range parameters {
			if parameter["name"] != "id" || navigationString(parameter["targetUsage"]) == "" {
				continue
			}
			found = true
			if got := navigationString(parameter["targetUsage"]); got != wantUsage[label] {
				t.Fatalf("redirect %s targetUsage = %q, want %q: %#v", label, got, wantUsage[label], edge)
			}
		}
		if !found {
			t.Fatalf("redirect %s has no owned id parameter: %#v", label, edge)
		}
	}
}

func TestNavigationVBIIfAlternativeParameterMetadataIsOwned(t *testing.T) {
	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	for _, name := range []string{"a.asp", "b.asp"} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	source := `<%
id = Request.QueryString("id")
target = IIf(enabled, "a.asp?id=" & id, "b.asp?id=" & id)
Response.Redirect target
%>`
	parsed := core.ParseDocument(filePathURI(page), source, core.Settings{})
	builder := newNavigationGraphBuilder("document", parsed.URI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	builder.addDocument(parsed, parsed.URI)
	if len(builder.edges) != 2 {
		t.Fatalf("IIf redirect edges = %#v, want two", builder.edges)
	}
	wantUsage := map[string]string{
		"a.asp": "a.asp?id={queryString:id}",
		"b.asp": "b.asp?id={queryString:id}",
	}
	for _, edge := range builder.edges {
		targetID, _ := edge["target"].(string)
		label := navigationString(builder.nodeByID[targetID]["label"])
		parameters, _ := edge["parameters"].([]map[string]any)
		found := false
		for _, parameter := range parameters {
			if parameter["name"] != "id" || navigationString(parameter["targetUsage"]) == "" {
				continue
			}
			found = true
			if got := navigationString(parameter["targetUsage"]); got != wantUsage[label] {
				t.Fatalf("IIf %s targetUsage = %q, want %q: %#v", label, got, wantUsage[label], edge)
			}
		}
		if !found {
			t.Fatalf("IIf %s has no id parameter: %#v", label, edge)
		}
	}
}

func TestNavigationVBIIfAlternativeParameterMetadataIsDeterministicWhenReversed(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		parameter := map[string]any{"name": "id", "source": "queryString"}
		a := navigationVBValue{Kind: navigationVBValueLiteral, Text: "a.asp", Parameters: []map[string]any{parameter}}
		b := navigationVBValue{Kind: navigationVBValueLiteral, Text: "b.asp", Parameters: []map[string]any{parameter}}
		alternatives := []navigationVBValue{a, b}
		if reverse {
			alternatives = []navigationVBValue{b, a}
		}
		result := combineNavigationVBValues([]navigationVBValue{{Kind: navigationVBValueLiteral, Alternatives: alternatives}})
		usages := map[string]string{}
		for _, value := range result.finiteCandidates() {
			for _, parameter := range value.Parameters {
				if parameter["name"] == "id" {
					usages[value.Text] = navigationString(parameter["targetUsage"])
				}
			}
		}
		if usages["a.asp"] != "a.asp" || usages["b.asp"] != "b.asp" {
			t.Fatalf("reversed IIf alternative targetUsage = %#v", usages)
		}
		if _, mutated := parameter["targetUsage"]; mutated {
			t.Fatalf("source request parameter was mutated: %#v", parameter)
		}
	}
}

func TestNavigationVBPrimitiveKindsPreserveEqualTextAcrossUnion(t *testing.T) {
	tests := []struct {
		expression string
		text       string
		primitive  navigationPrimitiveKind
	}{
		{expression: `"10"`, text: "10", primitive: navigationPrimitiveString},
		{expression: "10", text: "10", primitive: navigationPrimitiveNumber},
		{expression: "-10", text: "-10", primitive: navigationPrimitiveNumber},
		{expression: "-&H10", text: "-&H10", primitive: navigationPrimitiveNumber},
		{expression: "+10", text: "+10", primitive: navigationPrimitiveNumber},
		{expression: "+&H10", text: "+&H10", primitive: navigationPrimitiveNumber},
		{expression: "True", text: "true", primitive: navigationPrimitiveBoolean},
		{expression: `"true"`, text: "true", primitive: navigationPrimitiveString},
	}
	values := make([]navigationVBValue, 0, len(tests))
	for _, test := range tests {
		value := evaluateNavigationVBExpression(navigationVBSignificantTokens(vbscript.Tokenize(test.expression)), newNavigationVBState())
		if value.Kind != navigationVBValueLiteral || value.Text != test.text || value.Primitive != test.primitive {
			t.Fatalf("evaluateNavigationVBExpression(%q) = %#v, want literal %q with primitive %d", test.expression, value, test.text, test.primitive)
		}
		values = append(values, value)
	}
	merged := navigationMergeVBValueList(append(values, values[0]))
	candidates := merged.finiteCandidates()
	if len(candidates) != len(tests) {
		t.Fatalf("primitive union candidates = %#v, want %d distinct values", candidates, len(tests))
	}
	for _, test := range tests {
		found := false
		for _, candidate := range candidates {
			if candidate.Text == test.text && candidate.Primitive == test.primitive {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("primitive union missing %q with primitive %d: %#v", test.text, test.primitive, candidates)
		}
	}
}

func TestNavigationVBStringifiesPrimitiveValuesLikeVBScript(t *testing.T) {
	for _, testCase := range []struct {
		expression string
		want       string
	}{
		{expression: `"bool-" & True & ".asp"`, want: "bool-True.asp"},
		{expression: `CStr(True)`, want: "True"},
		{expression: `LCase(True)`, want: "true"},
		{expression: `UCase(False)`, want: "FALSE"},
		{expression: `CStr(&H10)`, want: "16"},
		{expression: `"item-" & &O20 & ".asp"`, want: "item-16.asp"},
		{expression: `CStr(-&H10)`, want: "-16"},
		{expression: `CStr(.5)`, want: "0.5"},
		{expression: `CStr(1.0)`, want: "1"},
		{expression: `"item-" & 1e2 & ".asp"`, want: "item-100.asp"},
	} {
		value := evaluateNavigationVBExpression(navigationVBSignificantTokens(vbscript.Tokenize(testCase.expression)), newNavigationVBState())
		if value.Kind != navigationVBValueLiteral || value.Primitive != navigationPrimitiveString || value.Text != testCase.want {
			t.Fatalf("evaluateNavigationVBExpression(%q) = %#v, want string %q", testCase.expression, value, testCase.want)
		}
	}
}

func TestNavigationVBConcatenationProducesStringPrimitive(t *testing.T) {
	value := evaluateNavigationVBExpression(navigationVBSignificantTokens(vbscript.Tokenize(`"10" & 10`)), newNavigationVBState())
	if value.Kind != navigationVBValueLiteral || value.Text != "1010" || value.Primitive != navigationPrimitiveString {
		t.Fatalf("concatenation value = %#v, want literal string primitive %q", value, "1010")
	}
}

func TestNavigationVBRequestParameterMetadataIsOwnedAcrossFormRedirectAndGeneratedHTML(t *testing.T) {
	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	for _, name := range []string{"form.asp", "redirect.asp", "generated.asp"} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	source := `<%
id = Request.QueryString("id")
Response.Redirect "redirect.asp?id=" & id
Response.Write "<a href=""generated.asp?id=" & id & """>Generated</a>"
%>
<form action="<%= "form.asp?id=" & id %>"></form>`

	parsed := core.ParseDocument(filePathURI(page), source, core.Settings{})
	builder := newNavigationGraphBuilder("document", parsed.URI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	builder.addDocument(parsed, parsed.URI)
	wantUsage := map[string]string{
		"form.asp":      "form.asp?id={queryString:id}",
		"redirect.asp":  "redirect.asp?id={queryString:id}",
		"generated.asp": `<a href="generated.asp?id={queryString:id}">Generated</a>`,
	}
	seen := map[string]bool{}
	for _, edge := range builder.edges {
		targetID, _ := edge["target"].(string)
		label := navigationString(builder.nodeByID[targetID]["label"])
		want, ok := wantUsage[label]
		if !ok {
			continue
		}
		parameters, _ := edge["parameters"].([]map[string]any)
		for _, parameter := range parameters {
			if parameter["name"] == "id" && navigationString(parameter["targetUsage"]) != "" {
				if got := navigationString(parameter["targetUsage"]); got != want {
					t.Fatalf("%s targetUsage = %q, want %q: %#v", label, got, want, edge)
				}
				seen[label] = true
			}
		}
	}
	for label := range wantUsage {
		if !seen[label] {
			t.Fatalf("%s edge with id metadata missing: %#v", label, builder.edges)
		}
	}
}

func TestNavigationVBFiniteValueCapKeepsDeterministicUnknownFallback(t *testing.T) {
	values := make([]navigationVBValue, 65)
	for index := range values {
		values[index] = navigationVBValue{Kind: navigationVBValueLiteral, Text: string(rune('a' + index))}
	}
	first := navigationMergeVBValueList(values).finiteCandidates()
	second := navigationMergeVBValueList(values).finiteCandidates()
	if len(first) != navigationVBFiniteValueLimit || len(second) != len(first) {
		t.Fatalf("finite value cap = %d/%d, want %d", len(first), len(second), navigationVBFiniteValueLimit)
	}
	for index := range first {
		if first[index].Kind != second[index].Kind || first[index].Text != second[index].Text {
			t.Fatalf("finite value ordering changed at %d: %#v vs %#v", index, first, second)
		}
	}
	if last := first[len(first)-1]; last.Kind != navigationVBValueUnknown || last.Text != "{unknown}" {
		t.Fatalf("finite value cap fallback = %#v, want unknown", last)
	}
}

func TestNavigationVBConcatenationCapKeepsCompleteSuffixes(t *testing.T) {
	prefixes := make([]navigationVBValue, navigationVBFiniteValueLimit+1)
	for index := range prefixes {
		prefixes[index] = navigationVBValue{Kind: navigationVBValueLiteral, Text: fmt.Sprintf("target-%02d", index)}
	}
	combined := combineNavigationVBValues([]navigationVBValue{
		{Kind: navigationVBValueLiteral, Alternatives: prefixes},
		{Kind: navigationVBValueLiteral, Text: ".asp"},
	})
	values := combined.finiteCandidates()
	if len(values) != navigationVBFiniteValueLimit {
		t.Fatalf("capped concatenation values = %d, want exact cap %d: %#v", len(values), navigationVBFiniteValueLimit, values)
	}
	for _, value := range values[:len(values)-1] {
		if value.Kind != navigationVBValueLiteral || !strings.HasSuffix(value.Text, ".asp") {
			t.Fatalf("capped concatenation retained incomplete value: %#v", value)
		}
	}
	if last := values[len(values)-1]; last.Kind != navigationVBValueUnknown || last.Text != "{unknown}" {
		t.Fatalf("capped concatenation fallback = %#v, want unknown", last)
	}
}

func TestNavigationVBConcatenationPropagatesExistingUnknownFallback(t *testing.T) {
	prefixes := make([]navigationVBValue, navigationVBFiniteValueLimit+1)
	for index := range prefixes {
		prefixes[index] = navigationVBValue{Kind: navigationVBValueLiteral, Text: fmt.Sprintf("target-%02d", index)}
	}
	capped := navigationMergeVBValueList(prefixes)
	combined := combineNavigationVBValues([]navigationVBValue{
		capped,
		{Kind: navigationVBValueLiteral, Text: ".asp"},
	})
	values := combined.finiteCandidates()
	if len(values) != navigationVBFiniteValueLimit {
		t.Fatalf("pre-capped concatenation values = %d, want exact cap %d: %#v", len(values), navigationVBFiniteValueLimit, values)
	}
	for _, value := range values[:len(values)-1] {
		if value.Kind != navigationVBValueLiteral || !strings.HasSuffix(value.Text, ".asp") {
			t.Fatalf("pre-capped concatenation retained incomplete value: %#v", value)
		}
	}
	if last := values[len(values)-1]; last.Kind != navigationVBValueUnknown || last.Text != "{unknown}" {
		t.Fatalf("pre-capped concatenation fallback = %#v, want unknown", last)
	}
}

func TestNavigationVBBranchAssignmentsReplacePreBranchValue(t *testing.T) {
	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	for _, name := range []string{"new.asp", "old.asp", "fallback.asp"} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	source := `<%
target = "fallback.asp"
If enabled Then
  target = "new.asp"
Else
  target = "old.asp"
End If
Response.Redirect target
%>`
	parsed := core.ParseDocument(filePathURI(page), source, core.Settings{})
	builder := newNavigationGraphBuilder("document", parsed.URI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	builder.addDocument(parsed, parsed.URI)
	if navigationPayloadNodeMatching(builder.payload(1), func(node map[string]any) bool { return node["label"] == "fallback.asp" }) {
		t.Fatalf("pre-branch value leaked into finite targets: %#v", builder.payload(1))
	}
}

func TestNavigationVBResolvesFiniteFunctionArguments(t *testing.T) {
	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	for _, name := range []string{"one.asp", "two.asp"} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	source := `<%
Function BuildTarget(name)
  BuildTarget = name & ".asp"
End Function
Response.Redirect BuildTarget("one")
Response.Redirect BuildTarget("two")
%>`
	parsed := core.ParseDocument(filePathURI(page), source, core.Settings{})
	builder := newNavigationGraphBuilder("document", parsed.URI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	builder.addDocument(parsed, parsed.URI)
	for _, name := range []string{"one.asp", "two.asp"} {
		if !navigationPayloadNodeMatching(builder.payload(1), func(node map[string]any) bool { return node["label"] == name }) {
			t.Fatalf("function argument target %s missing: %#v", name, builder.payload(1))
		}
	}
}

func TestNavigationVBUncalledProcedureBodiesDoNotMutateTopLevelState(t *testing.T) {
	const source = `<%
target = "outer.asp"
Function Uncalled()
  Dim target
  target = "inner.asp"
  Response.Redirect "body.asp"
End Function
Sub AlsoUncalled()
  Response.Redirect "sub-body.asp"
End Sub
Response.Redirect target
%>`
	candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
	if len(candidates) != 1 {
		t.Fatalf("uncalled procedure candidates = %#v", candidates)
	}
	if value := candidates[0].Value; value.Kind != navigationVBValueLiteral || value.Text != "outer.asp" {
		t.Fatalf("uncalled procedure value = %#v, want outer.asp", value)
	}
}

func TestNavigationVBCalledFunctionUsesIsolatedLocals(t *testing.T) {
	const source = `<%
target = "outer.asp"
Function BuildTarget()
  Dim target
  target = "inner.asp"
  BuildTarget = target
End Function
Response.Redirect BuildTarget()
%>`
	candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
	if len(candidates) != 1 {
		t.Fatalf("called function candidates = %#v", candidates)
	}
	if value := candidates[0].Value; value.Kind != navigationVBValueLiteral || value.Text != "inner.asp" {
		t.Fatalf("called function value = %#v, want inner.asp", value)
	}
}

func TestNavigationVBCalledSubEmitsReachableRedirectsWithCallerEvidence(t *testing.T) {
	const source = `<%
Sub Navigate(target)
  If enabled Then
    Response.Redirect target
  Else
    Response.Redirect "fallback.asp"
  End If
  Exit Sub
  Response.Redirect "unreachable.asp"
End Sub
Call Navigate("called.asp")
%>`
	candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
	if len(candidates) != 2 {
		t.Fatalf("called Sub candidates = %#v, want two reachable redirects", candidates)
	}
	values := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if !strings.Contains(candidate.Snippet, `Call Navigate("called.asp")`) {
			t.Fatalf("called Sub candidate evidence = %#v, want caller invocation", candidate)
		}
		values = append(values, candidate.Value.Text)
	}
	if !containsString(values, "called.asp") || !containsString(values, "fallback.asp") || containsString(values, "unreachable.asp") {
		t.Fatalf("called Sub values = %#v, want called.asp and fallback.asp only", values)
	}
}

func TestNavigationVBExitSubSkipsUnreachableDirectCallEffects(t *testing.T) {
	const source = `<%
Sub Mutate(ByRef target)
  globalTarget = "unreachable-global.asp"
  target = "unreachable-byref.asp"
  Response.Redirect "unreachable-sink.asp"
End Sub
Sub Navigate(ByRef target)
  Exit Sub
  Call Mutate(target)
  Mutate target
End Sub
globalTarget = "before-global.asp"
value = "before-value.asp"
Call Navigate(value)
Response.Redirect globalTarget
Response.Redirect value
%>`
	candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
	if len(candidates) != 2 {
		t.Fatalf("Exit Sub unreachable direct-call candidates = %#v, want two redirects", candidates)
	}
	values := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		values = append(values, candidate.Value.Text)
		if strings.Contains(candidate.Snippet, "Mutate") || strings.Contains(candidate.Value.Text, "unreachable-") {
			t.Fatalf("Exit Sub retained unreachable direct-call effect: %#v", candidates)
		}
	}
	if !containsString(values, "before-global.asp") || !containsString(values, "before-value.asp") {
		t.Fatalf("Exit Sub direct-call values = %#v, want original global and ByRef values", values)
	}
}

func TestNavigationVBExitFunctionSkipsUnreachableCallEffectsAndKeepsAlternateBranch(t *testing.T) {
	const source = `<%
Sub Mutate(ByRef target)
  globalTarget = "unreachable-global.asp"
  target = "unreachable-byref.asp"
  Response.Redirect "unreachable-sink.asp"
End Sub
Function BuildTarget(ByRef target)
  If enabled Then
    Exit Function
    Call Mutate(target)
    Mutate target
  Else
    target = "alternate-byref.asp"
    BuildTarget = "alternate-return.asp"
  End If
End Function
globalTarget = "before-global.asp"
value = "before-value.asp"
Response.Redirect BuildTarget(value)
Response.Redirect globalTarget
Response.Redirect value
%>`
	candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
	if len(candidates) != 3 {
		t.Fatalf("Exit Function unreachable call candidates = %#v, want three redirects", candidates)
	}
	values := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		for _, value := range candidate.Value.finiteCandidates() {
			values = append(values, value.Text)
			if strings.Contains(value.Text, "unreachable-") {
				t.Fatalf("Exit Function retained unreachable call value: %#v", candidates)
			}
		}
	}
	for _, want := range []string{"alternate-return.asp", "alternate-byref.asp", "before-global.asp", "before-value.asp"} {
		if !containsString(values, want) {
			t.Fatalf("Exit Function alternate-path value %q missing from %#v", want, values)
		}
	}
}

func TestNavigationVBLoopExitSkipsUnreachableCallEffects(t *testing.T) {
	tests := map[string]string{
		"for": `<%
Sub Mutate(ByRef target)
  globalTarget = "unreachable-global.asp"
  target = "unreachable-byref.asp"
  Response.Redirect "unreachable-sink.asp"
End Sub
globalTarget = "before-global.asp"
value = "before-value.asp"
For index = 1 To 2
  Exit For
  Call Mutate(value)
Next
Response.Redirect globalTarget
Response.Redirect value
%>`,
		"do": `<%
Sub Mutate(ByRef target)
  globalTarget = "unreachable-global.asp"
  target = "unreachable-byref.asp"
  Response.Redirect "unreachable-sink.asp"
End Sub
globalTarget = "before-global.asp"
value = "before-value.asp"
Do
  Exit Do
  Mutate value
Loop
Response.Redirect globalTarget
Response.Redirect value
%>`,
		"while": `<%
Sub Mutate(ByRef target)
  globalTarget = "unreachable-global.asp"
  target = "unreachable-byref.asp"
  Response.Redirect "unreachable-sink.asp"
End Sub
globalTarget = "before-global.asp"
value = "before-value.asp"
While enabled
  Exit While
  Call Mutate(value)
Wend
Response.Redirect globalTarget
Response.Redirect value
%>`,
	}
	for name, source := range tests {
		t.Run(name, func(t *testing.T) {
			candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
			if len(candidates) != 2 {
				t.Fatalf("%s loop unreachable call candidates = %#v, want two redirects", name, candidates)
			}
			values := make([]string, 0, len(candidates))
			for _, candidate := range candidates {
				values = append(values, candidate.Value.Text)
				if strings.Contains(candidate.Snippet, "Mutate") || strings.Contains(candidate.Value.Text, "unreachable-") {
					t.Fatalf("%s loop retained unreachable call effect: %#v", name, candidates)
				}
			}
			if !containsString(values, "before-global.asp") || !containsString(values, "before-value.asp") {
				t.Fatalf("%s loop values = %#v, want original global and ByRef values", name, values)
			}
		})
	}
}

func TestNavigationVBImplicitSubInvocationSupportsParameters(t *testing.T) {
	const source = `<%
Sub Navigate(target)
  Response.Redirect target
End Sub
Navigate "implicit.asp"
%>`
	candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
	if len(candidates) != 1 || candidates[0].Value.Text != "implicit.asp" {
		t.Fatalf("implicit Sub candidates = %#v, want implicit.asp", candidates)
	}
	if !strings.Contains(candidates[0].Snippet, `Navigate "implicit.asp"`) {
		t.Fatalf("implicit Sub evidence = %#v, want caller invocation", candidates[0])
	}
}

func TestNavigationVBCalledSubOptionalDefaultShadowsCallerVariable(t *testing.T) {
	const source = `<%
target = "outer.asp"
Sub Navigate(Optional target = "default.asp")
  Response.Redirect target
End Sub
Call Navigate()
%>`
	candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
	if len(candidates) != 1 || candidates[0].Value.Text != "default.asp" {
		t.Fatalf("called Sub optional default = %#v, want default.asp", candidates)
	}
}

func TestNavigationVBCalledSubMergesGlobalAssignmentIntoCaller(t *testing.T) {
	const source = `<%
target = "before.asp"
Sub Mutate()
  target = "after.asp"
End Sub
Call Mutate()
Response.Redirect target
%>`
	candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
	if len(candidates) != 1 || candidates[0].Value.Text != "after.asp" {
		t.Fatalf("called Sub global mutation = %#v, want after.asp", candidates)
	}
}

func TestNavigationVBCalledSubMergesBranchDependentGlobalMutation(t *testing.T) {
	const source = `<%
Sub Mutate()
  If enabled Then
    target = "then.asp"
  Else
    target = "else.asp"
  End If
End Sub
Call Mutate()
Response.Redirect target
%>`
	candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
	if len(candidates) != 1 {
		t.Fatalf("branch-dependent Sub mutation candidates = %#v", candidates)
	}
	values := candidates[0].Value.finiteCandidates()
	for _, want := range []string{"then.asp", "else.asp"} {
		if !navigationVBValueTextsContain(values, want) {
			t.Fatalf("branch-dependent Sub mutation missing %q: %#v", want, values)
		}
	}
}

func TestNavigationVBCalledFunctionMergesGlobalMutationAndReturn(t *testing.T) {
	const source = `<%
Function Mutate()
  target = "after.asp"
  Mutate = "return.asp"
End Function
Response.Redirect Mutate()
Response.Redirect target
%>`
	candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
	if len(candidates) != 2 {
		t.Fatalf("called Function side effects = %#v", candidates)
	}
	values := []string{candidates[0].Value.Text, candidates[1].Value.Text}
	if !containsString(values, "return.asp") || !containsString(values, "after.asp") {
		t.Fatalf("called Function return/global values = %#v, want return.asp and after.asp", values)
	}
}

func TestNavigationVBCalledFunctionCollectsReachableSinks(t *testing.T) {
	const source = `<%
Function Navigate()
  Response.Redirect "inner.asp"
  Navigate = "return.asp"
End Function
Response.Redirect Navigate()
%>`
	candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
	if len(candidates) != 2 {
		t.Fatalf("called Function sinks = %#v, want inner and return redirects", candidates)
	}
	values := []string{candidates[0].Value.Text, candidates[1].Value.Text}
	if !containsString(values, "inner.asp") || !containsString(values, "return.asp") {
		t.Fatalf("called Function sink values = %#v, want inner.asp and return.asp", values)
	}
	for _, candidate := range candidates {
		if !strings.Contains(candidate.Snippet, `Response.Redirect Navigate()`) {
			t.Fatalf("called Function sink evidence = %#v, want caller invocation", candidate)
		}
	}
}

func TestNavigationVBCalledSubMergesByRefMutation(t *testing.T) {
	const source = `<%
value = "before.asp"
Sub Mutate(ByRef target)
  target = "changed.asp"
End Sub
Call Mutate(value)
Response.Redirect value
%>`
	candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
	if len(candidates) != 1 || candidates[0].Value.Text != "changed.asp" {
		t.Fatalf("ByRef mutation = %#v, want changed.asp", candidates)
	}
}

func TestNavigationVBRecursiveFunctionSideEffectsStayBoundedWithUnknownFallback(t *testing.T) {
	const source = `<%
Function Loop()
  target = "depth.asp"
  Loop = Loop()
End Function
Response.Redirect Loop()
Response.Redirect target
%>`
	candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
	if len(candidates) != 2 {
		t.Fatalf("recursive Function side effects = %#v", candidates)
	}
	if candidates[0].Value.Kind != navigationVBValueUnknown || candidates[0].Value.Text != "{unknown}" {
		t.Fatalf("recursive Function return = %#v, want bounded unknown", candidates[0].Value)
	}
	if candidates[1].Value.Kind != navigationVBValueLiteral || candidates[1].Value.Text != "depth.asp" {
		t.Fatalf("recursive Function global side effect = %#v, want depth.asp", candidates[1].Value)
	}
}

func TestNavigationVBCalledSubCollectsWriteAndTransferSinks(t *testing.T) {
	const source = `<%
Sub Render()
  Response.Write "<a href=""written.asp"">Written</a>"
  Server.Transfer "transferred.asp"
End Sub
Call Render()
%>`
	candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
	if len(candidates) != 2 {
		t.Fatalf("called Sub write/transfer candidates = %#v, want two sinks", candidates)
	}
	if candidates[0].Kind != "write" || candidates[0].Value.Text != `<a href="written.asp">Written</a>` {
		t.Fatalf("called Sub write candidate = %#v", candidates[0])
	}
	if candidates[1].Kind != "redirect" || candidates[1].Value.Text != "transferred.asp" {
		t.Fatalf("called Sub transfer candidate = %#v", candidates[1])
	}
}

func TestNavigationVBNestedSubInvocationIsBoundedAndDoesNotDuplicateSinks(t *testing.T) {
	const source = `<%
Sub Outer(target)
  Call Inner(target)
End Sub
Sub Inner(target)
  Response.Redirect target
  Call Outer(target)
End Sub
Call Outer("nested.asp")
%>`
	candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
	if len(candidates) != 1 || candidates[0].Value.Text != "nested.asp" {
		t.Fatalf("nested Sub candidates = %#v, want one nested.asp sink", candidates)
	}
	if !strings.Contains(candidates[0].Snippet, `Call Outer("nested.asp")`) {
		t.Fatalf("nested Sub evidence = %#v, want root caller invocation", candidates[0])
	}
}

func TestNavigationVBCalledSubFromIncludedDocumentUsesRootOwner(t *testing.T) {
	root := t.TempDir()
	pageURI := filePathURI(filepath.Join(root, "default.asp"))
	includeURI := filePathURI(filepath.Join(root, "common.inc"))
	if err := os.WriteFile(filepath.Join(root, "included.asp"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	page := core.ParseDocument(pageURI, `<% Call Navigate("included.asp") %>`, core.Settings{})
	include := core.ParseDocument(includeURI, `<% Sub Navigate(target)
Response.Redirect target
End Sub %>`, core.Settings{})
	owners := map[string][]string{workspacepkg.FileIdentityKeyFromURI(includeURI): {pageURI}}
	builder := newNavigationGraphBuilder("document", pageURI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	builder.prepareVBScriptFunctions([]*core.ParsedDocument{page, include}, owners)
	builder.addDocument(page, pageURI)
	if !navigationPayloadNodeMatching(builder.payload(1), func(node map[string]any) bool { return node["label"] == "included.asp" }) {
		t.Fatalf("included Sub target missing from root owner: %#v", builder.payload(1))
	}
}

func TestNavigationVBDimLocalShadowsOuterVariableOnRead(t *testing.T) {
	const source = `<%
target = "outer.asp"
Function ReadTarget()
  Dim target
  ReadTarget = target
End Function
Response.Redirect ReadTarget()
%>`
	candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
	if len(candidates) != 1 {
		t.Fatalf("Dim local candidates = %#v", candidates)
	}
	if value := candidates[0].Value; value.Kind != navigationVBValueUnknown || value.Text != "{unknown}" {
		t.Fatalf("Dim local value = %#v, want unknown", value)
	}
}

func TestNavigationVBGlobalFunctionCannotSeeCallerLocalVariable(t *testing.T) {
	const source = `<%
target = "global.asp"
Function ReadTarget()
  ReadTarget = target
End Function
Function Outer()
  Dim target
  target = "private.asp"
  Outer = ReadTarget()
End Function
Response.Redirect Outer()
%>`
	candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
	if len(candidates) != 1 {
		t.Fatalf("global function scope candidates = %#v", candidates)
	}
	if value := candidates[0].Value; value.Kind != navigationVBValueLiteral || value.Text != "global.asp" {
		t.Fatalf("global function scope value = %#v, want global.asp", value)
	}
}

func TestNavigationVBGlobalSubDoesNotMutateCallerLocalShadow(t *testing.T) {
	const source = `<%
target = "global-before.asp"
Sub Mutate()
  target = "global-after.asp"
End Sub
Function Outer()
  Dim target
  target = "private.asp"
  Call Mutate()
  Outer = target
End Function
Response.Redirect Outer()
Response.Redirect target
%>`
	candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
	if len(candidates) != 2 {
		t.Fatalf("global Sub/local shadow candidates = %#v", candidates)
	}
	values := []string{candidates[0].Value.Text, candidates[1].Value.Text}
	if !containsString(values, "private.asp") || !containsString(values, "global-after.asp") {
		t.Fatalf("global Sub/local shadow values = %#v, want private.asp and global-after.asp", values)
	}
}

func TestNavigationVBGlobalSubMutationThroughCallerBranchPreservesLocalShadow(t *testing.T) {
	const source = `<%
target = "global-before.asp"
Sub Mutate()
  target = "global-after.asp"
End Sub
Function Outer()
  Dim target
  target = "private.asp"
  If enabled Then
    Call Mutate()
  Else
    target = "else.asp"
  End If
  Outer = target
End Function
Response.Redirect Outer()
Response.Redirect target
%>`
	candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
	if len(candidates) != 2 {
		t.Fatalf("branch global/local shadow candidates = %#v", candidates)
	}
	foundOuter := false
	foundGlobal := false
	for _, candidate := range candidates {
		values := candidate.Value.finiteCandidates()
		if navigationVBValueTextsContain(values, "private.asp") || navigationVBValueTextsContain(values, "else.asp") {
			foundOuter = navigationVBValueTextsContain(values, "private.asp") && navigationVBValueTextsContain(values, "else.asp")
		}
		if navigationVBValueTextsContain(values, "global-after.asp") {
			foundGlobal = true
		}
	}
	if !foundOuter {
		t.Fatalf("branch global/local shadow local values missing: %#v", candidates)
	}
	if !foundGlobal {
		t.Fatalf("branch global/local shadow global value = %#v, want global-after.asp", candidates)
	}
}

func TestNavigationVBCalledFunctionMergesByRefMutation(t *testing.T) {
	const source = `<%
value = "before.asp"
Function Mutate(ByRef target)
  target = "changed.asp"
  Mutate = "return.asp"
End Function
Response.Redirect Mutate(value)
Response.Redirect value
%>`
	candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
	if len(candidates) != 2 {
		t.Fatalf("called Function ByRef candidates = %#v", candidates)
	}
	values := []string{candidates[0].Value.Text, candidates[1].Value.Text}
	if !containsString(values, "return.asp") || !containsString(values, "changed.asp") {
		t.Fatalf("called Function ByRef values = %#v, want return.asp and changed.asp", values)
	}
}

func TestNavigationVBClassOnlyProcedureIsNotGlobalHelper(t *testing.T) {
	const source = `<%
Class Navigator
  Function BuildTarget()
    BuildTarget = "class.asp"
  End Function
End Class
Response.Redirect BuildTarget()
%>`
	candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
	if len(candidates) != 1 {
		t.Fatalf("class-only helper candidates = %#v", candidates)
	}
	if value := candidates[0].Value; value.Kind != navigationVBValueUnknown || value.Text != "{unknown}" {
		t.Fatalf("class-only helper value = %#v, want unknown", value)
	}
}

func TestNavigationVBClassBoundaryAcrossASPRegionsIsNotGlobalHelper(t *testing.T) {
	const source = `<% Class Navigator %>
<% Function BuildTarget()
  BuildTarget = "class.asp"
End Function %>
<% End Class %>
<% Response.Redirect BuildTarget() %>`
	parsed := core.ParseDocument("file:///class-regions.asp", source, core.Settings{})
	if _, ok := navigationVBFunctionDefinitions(parsed)["buildtarget"]; ok {
		t.Fatalf("cross-region class helper was indexed as a global function: %#v", navigationVBFunctionDefinitions(parsed))
	}
}

func TestNavigationVBGlobalProcedureWinsClassNameCollision(t *testing.T) {
	const source = `<%
Function BuildTarget()
  BuildTarget = "global.asp"
End Function
Class Navigator
  Function BuildTarget()
    BuildTarget = "class.asp"
  End Function
End Class
Response.Redirect BuildTarget()
%>`
	candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
	if len(candidates) != 1 {
		t.Fatalf("global/class collision candidates = %#v", candidates)
	}
	if value := candidates[0].Value; value.Kind != navigationVBValueLiteral || value.Text != "global.asp" {
		t.Fatalf("global/class collision value = %#v, want global.asp", value)
	}
}

func TestNavigationVBLoopsPreserveBaseAndBoundedUnknownPaths(t *testing.T) {
	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	for _, name := range []string{"base.asp", "loop.asp"} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tests := map[string]string{
		"for": `<%
target = "base.asp"
For index = 1 To 2
  target = "loop.asp"
Next
Response.Redirect target
%>`,
		"for each": `<%
target = "base.asp"
For Each item In items
  target = "loop.asp"
Next
Response.Redirect target
%>`,
		"while": `<%
target = "base.asp"
While condition
  target = "loop.asp"
Wend
Response.Redirect target
%>`,
		"do": `<%
target = "base.asp"
Do
  target = "loop.asp"
Loop
Response.Redirect target
%>`,
	}
	for name, source := range tests {
		t.Run(name, func(t *testing.T) {
			parsed := core.ParseDocument(filePathURI(page), source, core.Settings{})
			builder := newNavigationGraphBuilder("document", parsed.URI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
			builder.addDocument(parsed, parsed.URI)
			payload := builder.payload(1)
			for _, target := range []string{"base.asp", "loop.asp"} {
				if !navigationPayloadNodeMatching(payload, func(node map[string]any) bool { return node["label"] == target }) {
					t.Fatalf("%s loop target %s missing: %#v", name, target, payload)
				}
			}
			if !navigationPayloadNodeMatching(payload, func(node map[string]any) bool { return node["kind"] == "unknown" }) {
				t.Fatalf("%s loop unknown fallback missing: %#v", name, payload)
			}
		})
	}
}

func TestNavigationVBFunctionLoopsPreserveLocalBaseAndReturnCandidates(t *testing.T) {
	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	for _, name := range []string{"base.asp", "loop.asp"} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	source := `<%
Function BuildTarget()
  Dim target
  target = "base.asp"
  For index = 1 To 2
    target = "loop.asp"
  Next
  BuildTarget = target
End Function
Response.Redirect BuildTarget()
%>`
	parsed := core.ParseDocument(filePathURI(page), source, core.Settings{})
	builder := newNavigationGraphBuilder("document", parsed.URI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	builder.addDocument(parsed, parsed.URI)
	payload := builder.payload(1)
	for _, target := range []string{"base.asp", "loop.asp"} {
		if !navigationPayloadNodeMatching(payload, func(node map[string]any) bool { return node["label"] == target }) {
			t.Fatalf("function loop target %s missing: %#v", target, payload)
		}
	}
	if !navigationPayloadNodeMatching(payload, func(node map[string]any) bool { return node["kind"] == "unknown" }) {
		t.Fatalf("function loop unknown fallback missing: %#v", payload)
	}
}

func TestNavigationVBExitForStopsUnreachableLoopBody(t *testing.T) {
	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	for _, name := range []string{"base.asp", "loop.asp", "unreachable.asp"} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	source := `<%
target = "base.asp"
For index = 1 To 2
  target = "loop.asp"
  Exit For
  target = "unreachable.asp"
Next
Response.Redirect target
%>`
	parsed := core.ParseDocument(filePathURI(page), source, core.Settings{})
	builder := newNavigationGraphBuilder("document", parsed.URI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	builder.addDocument(parsed, parsed.URI)
	payload := builder.payload(1)
	for _, target := range []string{"base.asp", "loop.asp"} {
		if !navigationPayloadNodeMatching(payload, func(node map[string]any) bool { return node["label"] == target }) {
			t.Fatalf("Exit For target %s missing: %#v", target, payload)
		}
	}
	if navigationPayloadNodeMatching(payload, func(node map[string]any) bool { return node["label"] == "unreachable.asp" }) {
		t.Fatalf("Exit For retained unreachable target: %#v", payload)
	}
}

func TestNavigationVBExitForSuppressesUnreachableNavigationSinks(t *testing.T) {
	const source = `<%
target = "base.asp"
For index = 1 To 2
  Exit For
  Response.Redirect "unreachable.asp"
Next
Response.Redirect target
%>`
	candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
	if len(candidates) != 1 {
		t.Fatalf("Exit For navigation sinks = %#v, want only reachable sink", candidates)
	}
	if strings.Contains(candidates[0].Snippet, "unreachable.asp") {
		t.Fatalf("Exit For retained unreachable navigation sink: %#v", candidates)
	}
}

func TestNavigationVBConditionalExitFunctionPreservesElseAndUnknownPath(t *testing.T) {
	const source = `<%
Function BuildTarget()
  If enabled Then
    Exit Function
  Else
    BuildTarget = "else.asp"
  End If
End Function
Response.Redirect BuildTarget()
%>`
	candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
	if len(candidates) != 1 {
		t.Fatalf("conditional Exit Function candidates = %#v", candidates)
	}
	values := candidates[0].Value.finiteCandidates()
	if !navigationVBValueTextsContain(values, "else.asp") || !navigationVBValueTextsContain(values, "{unknown}") {
		t.Fatalf("conditional Exit Function values = %#v, want else.asp and unknown", values)
	}
}

func TestNavigationVBUnconditionalExitFunctionSkipsUnreachableAssignments(t *testing.T) {
	const source = `<%
Function BuildTarget()
  BuildTarget = "before.asp"
  Exit Function
  BuildTarget = "after.asp"
End Function
Response.Redirect BuildTarget()
%>`
	candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
	if len(candidates) != 1 {
		t.Fatalf("unconditional Exit Function candidates = %#v", candidates)
	}
	values := candidates[0].Value.finiteCandidates()
	if len(values) != 1 || values[0].Text != "before.asp" || values[0].Kind != navigationVBValueLiteral {
		t.Fatalf("unconditional Exit Function values = %#v, want exactly concrete before.asp", values)
	}
}

func TestNavigationVBSingleLineConditionalExitFunctionPreservesElse(t *testing.T) {
	const source = `<%
Function BuildTarget()
  If enabled Then Exit Function Else BuildTarget = "else.asp"
End Function
Response.Redirect BuildTarget()
%>`
	candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
	if len(candidates) != 1 {
		t.Fatalf("single-line conditional Exit Function candidates = %#v", candidates)
	}
	values := candidates[0].Value.finiteCandidates()
	if !navigationVBValueTextsContain(values, "else.asp") || !navigationVBValueTextsContain(values, "{unknown}") {
		t.Fatalf("single-line conditional Exit Function values = %#v, want else.asp and unknown", values)
	}
}

func TestNavigationVBConditionalLoopExitsPreserveElsePath(t *testing.T) {
	tests := map[string]string{
		"for": `<%
target = "base.asp"
For index = 1 To 2
  If enabled Then
    Exit For
  Else
    target = "else.asp"
  End If
Next
Response.Redirect target
%>`,
		"do": `<%
target = "base.asp"
Do
  If enabled Then
    Exit Do
  Else
    target = "else.asp"
  End If
Loop
Response.Redirect target
%>`,
		"while": `<%
target = "base.asp"
While enabled
  If enabled Then
    Exit While
  Else
    target = "else.asp"
  End If
Wend
Response.Redirect target
%>`,
	}
	for name, source := range tests {
		t.Run(name, func(t *testing.T) {
			candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
			if len(candidates) != 1 {
				t.Fatalf("conditional Exit %s candidates = %#v", name, candidates)
			}
			values := candidates[0].Value.finiteCandidates()
			if !navigationVBValueTextsContain(values, "base.asp") || !navigationVBValueTextsContain(values, "else.asp") {
				t.Fatalf("conditional Exit %s values = %#v, want base.asp and else.asp", name, values)
			}
		})
	}
}

func TestNavigationVBNestedConditionalExitFunctionPreservesOuterElse(t *testing.T) {
	const source = `<%
Function BuildTarget()
  If outer Then
    If inner Then
      Exit Function
    Else
      BuildTarget = "inner.asp"
    End If
  Else
    BuildTarget = "outer.asp"
  End If
End Function
Response.Redirect BuildTarget()
%>`
	candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
	if len(candidates) != 1 {
		t.Fatalf("nested conditional Exit Function candidates = %#v", candidates)
	}
	values := candidates[0].Value.finiteCandidates()
	for _, target := range []string{"inner.asp", "outer.asp", "{unknown}"} {
		if !navigationVBValueTextsContain(values, target) {
			t.Fatalf("nested conditional Exit Function target %q missing from %#v", target, values)
		}
	}
}

func TestNavigationVBSelectNestedInIfExitUsesInnermostSelectPath(t *testing.T) {
	const source = `<%
Function BuildTarget()
  If outer Then
    Select Case mode
    Case 1
      BuildTarget = "inner.asp"
      Exit Function
      BuildTarget = "unreachable-inner.asp"
    Case Else
      BuildTarget = "inner-else.asp"
    End Select
  Else
    BuildTarget = "outer-else.asp"
  End If
  BuildTarget = "after.asp"
End Function
Response.Redirect BuildTarget()
%>`
	candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
	if len(candidates) != 1 {
		t.Fatalf("Select-in-If Exit Function candidates = %#v", candidates)
	}
	values := candidates[0].Value.finiteCandidates()
	want := []string{"inner.asp", "after.asp"}
	if len(values) != len(want) {
		t.Fatalf("Select-in-If Exit Function values = %#v, want exactly %#v", values, want)
	}
	for _, text := range want {
		if !navigationVBValueTextsContain(values, text) {
			t.Fatalf("Select-in-If Exit Function value %q missing from %#v", text, values)
		}
	}
	for _, text := range []string{"inner-else.asp", "outer-else.asp", "unreachable-inner.asp"} {
		if navigationVBValueTextsContain(values, text) {
			t.Fatalf("Select-in-If Exit Function retained non-final value %q: %#v", text, values)
		}
	}
}

func TestNavigationVBSelectNestedInIfCalledSubExitUsesInnermostSelectPath(t *testing.T) {
	const source = `<%
Sub Navigate()
  If outer Then
    Select Case mode
    Case 1
      Response.Redirect "inner.asp"
      Exit Sub
      Response.Redirect "unreachable-inner.asp"
    Case Else
      Response.Redirect "inner-else.asp"
    End Select
  Else
    Response.Redirect "outer-else.asp"
  End If
  Response.Redirect "after.asp"
End Sub
Call Navigate()
%>`
	candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
	if len(candidates) != 4 {
		t.Fatalf("Select-in-If Exit Sub sinks = %#v, want exactly four", candidates)
	}
	want := []string{"inner.asp", "inner-else.asp", "outer-else.asp", "after.asp"}
	seen := map[string]int{}
	for _, candidate := range candidates {
		seen[candidate.Value.Text]++
		if strings.Contains(candidate.Snippet, "unreachable-inner.asp") {
			t.Fatalf("Select-in-If Exit Sub retained unreachable sink: %#v", candidates)
		}
	}
	for _, text := range want {
		if seen[text] == 0 {
			t.Fatalf("Select-in-If Exit Sub sink %q missing from %#v", text, candidates)
		}
	}
}

func TestNavigationVBFunctionLoopExitsSkipUnreachableAssignments(t *testing.T) {
	tests := map[string]string{
		"for": `<%
Function BuildTarget()
  target = "base.asp"
  For index = 1 To 2
    target = "loop.asp"
    Exit For
    target = "unreachable.asp"
  Next
  BuildTarget = target
End Function
Response.Redirect BuildTarget()
%>`,
		"do": `<%
Function BuildTarget()
  target = "base.asp"
  Do
    target = "loop.asp"
    Exit Do
    target = "unreachable.asp"
  Loop
  BuildTarget = target
End Function
Response.Redirect BuildTarget()
%>`,
		"while": `<%
Function BuildTarget()
  target = "base.asp"
  While enabled
    target = "loop.asp"
    Exit While
    target = "unreachable.asp"
  Wend
  BuildTarget = target
End Function
Response.Redirect BuildTarget()
%>`,
	}
	for name, source := range tests {
		t.Run(name, func(t *testing.T) {
			candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
			if len(candidates) != 1 {
				t.Fatalf("function Exit %s candidates = %#v", name, candidates)
			}
			values := candidates[0].Value.finiteCandidates()
			if navigationVBValueTextsContain(values, "unreachable.asp") {
				t.Fatalf("function Exit %s retained unreachable assignment: %#v", name, values)
			}
		})
	}
}

func TestNavigationVBExitFunctionSkipsNestedUnreachableBranches(t *testing.T) {
	const source = `<%
Function BuildTarget()
  If outer Then
    Exit Function
    If nested Then
      BuildTarget = "unreachable-then.asp"
    Else
      BuildTarget = "unreachable-else.asp"
    End If
  Else
    BuildTarget = "outer.asp"
  End If
End Function
Response.Redirect BuildTarget()
%>`
	candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
	if len(candidates) != 1 {
		t.Fatalf("nested unreachable Exit Function candidates = %#v", candidates)
	}
	values := candidates[0].Value.finiteCandidates()
	if !navigationVBValueTextsContain(values, "outer.asp") || !navigationVBValueTextsContain(values, "{unknown}") {
		t.Fatalf("nested unreachable Exit Function values = %#v, want outer.asp and unknown", values)
	}
	for _, target := range []string{"unreachable-then.asp", "unreachable-else.asp"} {
		if navigationVBValueTextsContain(values, target) {
			t.Fatalf("nested unreachable Exit Function retained %s: %#v", target, values)
		}
	}
}

func navigationVBValueTextsContain(values []navigationVBValue, text string) bool {
	for _, value := range values {
		if value.Text == text {
			return true
		}
	}
	return false
}

func TestNavigationVBFunctionSingleLineIfReturnsFiniteTargets(t *testing.T) {
	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	for _, name := range []string{"new.asp", "old.asp"} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	source := `<%
Function BuildTarget()
  If enabled Then BuildTarget = "new.asp" Else BuildTarget = "old.asp"
End Function
Response.Redirect BuildTarget()
%>`
	parsed := core.ParseDocument(filePathURI(page), source, core.Settings{})
	builder := newNavigationGraphBuilder("document", parsed.URI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	builder.addDocument(parsed, parsed.URI)
	for _, name := range []string{"new.asp", "old.asp"} {
		if !navigationPayloadNodeMatching(builder.payload(1), func(node map[string]any) bool { return node["label"] == name }) {
			t.Fatalf("function single-line If target %s missing: %#v", name, builder.payload(1))
		}
	}
	if len(builder.edges) != 2 {
		t.Fatalf("function single-line If edges = %d, want 2: %#v", len(builder.edges), builder.edges)
	}
}

func TestNavigationVBRecursiveFunctionsReturnUnknown(t *testing.T) {
	tests := map[string]string{
		"direct recursion": `<%
Function Loop()
  Loop = Loop()
End Function
Response.Redirect Loop()
%>`,
		"mutual recursion": `<%
Function First()
  First = Second()
End Function
Function Second()
  Second = First()
End Function
Response.Redirect First()
%>`,
	}
	for name, source := range tests {
		t.Run(name, func(t *testing.T) {
			candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
			if len(candidates) != 1 {
				t.Fatalf("navigation candidates = %#v", candidates)
			}
			if value := candidates[0].Value; value.Kind != navigationVBValueUnknown || value.Text != "{unknown}" {
				t.Fatalf("recursive function value = %#v, want unknown", value)
			}
		})
	}
}

func TestNavigationVBMultilineIfWithoutElsePreservesBasePath(t *testing.T) {
	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	for _, name := range []string{"base.asp", "branch.asp"} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	source := `<%
target = "base.asp"
If enabled Then
  target = "branch.asp"
End If
Response.Redirect target
%>`
	parsed := core.ParseDocument(filePathURI(page), source, core.Settings{})
	builder := newNavigationGraphBuilder("document", parsed.URI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	builder.addDocument(parsed, parsed.URI)
	for _, name := range []string{"base.asp", "branch.asp"} {
		if !navigationPayloadNodeMatching(builder.payload(1), func(node map[string]any) bool { return node["label"] == name }) {
			t.Fatalf("If-without-Else target %s missing: %#v", name, builder.payload(1))
		}
	}
}

func TestNavigationVBSelectCaseWithoutElsePreservesBasePath(t *testing.T) {
	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	for _, name := range []string{"base.asp", "case.asp"} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	source := `<%
target = "base.asp"
Select Case mode
Case 1
  target = "case.asp"
End Select
Response.Redirect target
%>`
	parsed := core.ParseDocument(filePathURI(page), source, core.Settings{})
	builder := newNavigationGraphBuilder("document", parsed.URI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	builder.addDocument(parsed, parsed.URI)
	for _, name := range []string{"base.asp", "case.asp"} {
		if !navigationPayloadNodeMatching(builder.payload(1), func(node map[string]any) bool { return node["label"] == name }) {
			t.Fatalf("Select-without-Case-Else target %s missing: %#v", name, builder.payload(1))
		}
	}
}

func TestNavigationVBSelectCaseExitFunctionPreservesSiblingPaths(t *testing.T) {
	const source = `<%
Function BuildTarget()
  Select Case mode
  Case 1
    BuildTarget = "one.asp"
    Exit Function
    BuildTarget = "unreachable-one.asp"
  Case 2
    BuildTarget = "two.asp"
    Exit Function
  Case Else
    BuildTarget = "else.asp"
  End Select
End Function
Response.Redirect BuildTarget()
%>`
	candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
	if len(candidates) != 1 {
		t.Fatalf("Select Exit Function candidates = %#v", candidates)
	}
	values := candidates[0].Value.finiteCandidates()
	want := []string{"one.asp", "two.asp", "else.asp"}
	if len(values) != len(want) {
		t.Fatalf("Select Exit Function values = %#v, want exactly %#v", values, want)
	}
	for _, text := range want {
		if !navigationVBValueTextsContain(values, text) {
			t.Fatalf("Select Exit Function value %q missing from %#v", text, values)
		}
	}
	if navigationVBValueTextsContain(values, "unreachable-one.asp") || navigationVBValueTextsContain(values, "{unknown}") {
		t.Fatalf("Select Exit Function retained unreachable/unknown value: %#v", values)
	}
}

func TestNavigationVBSelectCaseExitFunctionKeepsTerminatedValueAfterEndSelect(t *testing.T) {
	const source = `<%
Function BuildTarget()
  Select Case mode
  Case 1
    BuildTarget = "one.asp"
    Exit Function
  Case Else
    BuildTarget = "else.asp"
  End Select
  BuildTarget = "after.asp"
End Function
Response.Redirect BuildTarget()
%>`
	candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
	if len(candidates) != 1 {
		t.Fatalf("Select Exit Function after End Select candidates = %#v", candidates)
	}
	values := candidates[0].Value.finiteCandidates()
	want := []string{"one.asp", "after.asp"}
	if len(values) != len(want) {
		t.Fatalf("Select Exit Function after End Select values = %#v, want exactly %#v", values, want)
	}
	for _, text := range want {
		if !navigationVBValueTextsContain(values, text) {
			t.Fatalf("Select Exit Function after End Select value %q missing from %#v", text, values)
		}
	}
}

func TestNavigationVBSelectCaseNestedExitFunctionKeepsTerminatedPaths(t *testing.T) {
	const source = `<%
Function BuildTarget()
  Select Case mode
  Case 1
    If enabled Then
      BuildTarget = "one.asp"
      Exit Function
    Else
      BuildTarget = "one-else.asp"
      Exit Function
    End If
  Case Else
    BuildTarget = "else.asp"
  End Select
  BuildTarget = "after.asp"
End Function
Response.Redirect BuildTarget()
%>`
	candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
	if len(candidates) != 1 {
		t.Fatalf("nested Select Exit Function candidates = %#v", candidates)
	}
	values := candidates[0].Value.finiteCandidates()
	want := []string{"one.asp", "one-else.asp", "after.asp"}
	if len(values) != len(want) {
		t.Fatalf("nested Select Exit Function values = %#v, want exactly %#v", values, want)
	}
	for _, text := range want {
		if !navigationVBValueTextsContain(values, text) {
			t.Fatalf("nested Select Exit Function value %q missing from %#v", text, values)
		}
	}
}

func TestNavigationVBCalledSubSelectExitPreservesSiblingSinks(t *testing.T) {
	const source = `<%
Sub Navigate()
  Select Case mode
  Case 1
    Response.Redirect "one.asp"
    Exit Sub
    Response.Redirect "unreachable-one.asp"
  Case 2
    Response.Redirect "two.asp"
    Exit Sub
  Case Else
    Response.Redirect "else.asp"
  End Select
End Sub
Call Navigate()
%>`
	candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
	if len(candidates) != 3 {
		t.Fatalf("Select Exit Sub sinks = %#v, want exactly three reachable sinks", candidates)
	}
	want := []string{"one.asp", "two.asp", "else.asp"}
	for _, text := range want {
		found := false
		for _, candidate := range candidates {
			if candidate.Value.Text == text {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("Select Exit Sub sink %q missing from %#v", text, candidates)
		}
	}
	for _, candidate := range candidates {
		if strings.Contains(candidate.Snippet, "unreachable-one.asp") {
			t.Fatalf("Select Exit Sub retained unreachable sink: %#v", candidates)
		}
	}
}

func TestNavigationVBCalledSubSelectAllExitSkipsAfterSelectSink(t *testing.T) {
	const source = `<%
Sub Navigate()
  Select Case mode
  Case 1
    Response.Redirect "one.asp"
    Exit Sub
  Case Else
    Response.Redirect "else.asp"
    Exit Sub
  End Select
  Response.Redirect "unreachable-after-select.asp"
End Sub
Call Navigate()
%>`
	candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
	if len(candidates) != 2 {
		t.Fatalf("all-exit Select Sub sinks = %#v, want exactly two", candidates)
	}
	for _, candidate := range candidates {
		if strings.Contains(candidate.Snippet, "unreachable-after-select.asp") {
			t.Fatalf("all-exit Select Sub retained unreachable sink: %#v", candidates)
		}
	}
}

func TestNavigationVBFunctionReturnAssignmentUsesFinalValuePerPath(t *testing.T) {
	const source = `<%
Function BuildTarget()
  BuildTarget = "old.asp"
  BuildTarget = "new.asp"
End Function
Response.Redirect BuildTarget()
%>`
	candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
	if len(candidates) != 1 {
		t.Fatalf("final function assignment candidates = %#v", candidates)
	}
	values := candidates[0].Value.finiteCandidates()
	if len(values) != 1 || values[0].Kind != navigationVBValueLiteral || values[0].Text != "new.asp" {
		t.Fatalf("final function assignment values = %#v, want exactly new.asp", values)
	}
}

func TestNavigationVBFunctionReturnAssignmentBranchUsesFinalValuePerPath(t *testing.T) {
	const source = `<%
Function BuildTarget()
  BuildTarget = "base.asp"
  If enabled Then
    BuildTarget = "then-old.asp"
    BuildTarget = "then-final.asp"
  Else
    BuildTarget = "else-old.asp"
    BuildTarget = "else-final.asp"
  End If
End Function
Response.Redirect BuildTarget()
%>`
	candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
	if len(candidates) != 1 {
		t.Fatalf("branch final function assignment candidates = %#v", candidates)
	}
	values := candidates[0].Value.finiteCandidates()
	want := []string{"then-final.asp", "else-final.asp"}
	if len(values) != len(want) {
		t.Fatalf("branch final function assignment values = %#v, want exactly %#v", values, want)
	}
	for _, text := range want {
		if !navigationVBValueTextsContain(values, text) {
			t.Fatalf("branch final function assignment value %q missing from %#v", text, values)
		}
	}
	for _, text := range []string{"base.asp", "then-old.asp", "else-old.asp"} {
		if navigationVBValueTextsContain(values, text) {
			t.Fatalf("branch final function assignment retained overwritten value %q: %#v", text, values)
		}
	}
}

func TestNavigationVBOptionalDefaultExpressionShadowsOuterVariable(t *testing.T) {
	const source = `<%
target = "outer.asp"
Function BuildTarget(Optional target = "default" & ".asp")
  BuildTarget = target
End Function
Response.Redirect BuildTarget()
%>`
	candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
	if len(candidates) != 1 {
		t.Fatalf("navigation candidates = %#v", candidates)
	}
	if value := candidates[0].Value; value.Kind != navigationVBValueLiteral || value.Text != "default.asp" {
		t.Fatalf("optional default value = %#v, want default.asp", value)
	}
}

func TestNavigationVBOptionalDefaultExpressionDoesNotReadOuterSameName(t *testing.T) {
	const source = `<%
target = "outer.asp"
Function BuildTarget(Optional target = target & ".asp")
  BuildTarget = target
End Function
Response.Redirect BuildTarget()
%>`
	candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
	if len(candidates) != 1 {
		t.Fatalf("same-name optional default candidates = %#v", candidates)
	}
	if value := candidates[0].Value; value.Kind != navigationVBValueUnknown || value.Text != "{unknown}" {
		t.Fatalf("same-name optional default value = %#v, want unknown", value)
	}
}

func TestNavigationVBIncludeExecutionRespectsSourceOrder(t *testing.T) {
	root := t.TempDir()
	pageURI := filePathURI(filepath.Join(root, "default.asp"))
	includeURI := filePathURI(filepath.Join(root, "common.inc"))
	page := core.ParseDocument(pageURI, `<%
target = "before.asp"
Response.Redirect target
%>
<!-- #include file="common.inc" -->
<%
Response.Redirect target
%>`, core.Settings{})
	include := core.ParseDocument(includeURI, `<% target = "after.asp" %>`, core.Settings{})
	owners := map[string][]string{workspacepkg.FileIdentityKeyFromURI(includeURI): {pageURI}}
	builder := newNavigationGraphBuilder("document", pageURI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	builder.prepareVBScriptFunctions([]*core.ParsedDocument{page, include}, owners)
	builder.addDocument(page, pageURI)
	if len(builder.edges) != 2 {
		t.Fatalf("source-order include edges = %#v, want before and after redirects", builder.edges)
	}
	for _, target := range []string{"before.asp", "after.asp"} {
		if !navigationPayloadNodeMatching(builder.payload(2), func(node map[string]any) bool { return node["label"] == target }) {
			t.Fatalf("source-order include target %s missing: %#v", target, builder.payload(2))
		}
	}
}

func TestNavigationVBIncludeExecutionUsesConfiguredIncludePathResolution(t *testing.T) {
	root := t.TempDir()
	pageDir := filepath.Join(root, "pages")
	includeDir := filepath.Join(root, "includes")
	if err := os.MkdirAll(pageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(includeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	pagePath := filepath.Join(pageDir, "default.asp")
	includePath := filepath.Join(includeDir, "common.inc")
	pageSource := `<!-- #include file="common.inc" -->
<% Response.Redirect target %>`
	if err := os.WriteFile(includePath, []byte(`<% target = "include-path.asp" %>`), 0o644); err != nil {
		t.Fatal(err)
	}
	page := core.ParseDocument(filePathURI(pagePath), pageSource, core.Settings{})
	include := core.ParseDocument(filePathURI(includePath), `<% target = "include-path.asp" %>`, core.Settings{})
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = pageDir
	server.workspaceRoots = []workspaceRoot{{Path: pageDir, URI: filePathURI(pageDir)}}
	server.settings.IncludePaths = []string{includeDir}
	owners, resolved := navigationIncludeRelationsWithProgress(context.Background(), server, []*core.ParsedDocument{page, include}, nil)
	builder := newNavigationGraphBuilder("document", page.URI, []workspaceRoot{{Path: pageDir, URI: filePathURI(pageDir)}})
	builder.prepareVBScriptFunctionsWithIncludes([]*core.ParsedDocument{page, include}, owners, resolved)
	builder.addDocument(page, page.URI)
	if !navigationPayloadNodeMatching(builder.payload(2), func(node map[string]any) bool { return node["label"] == "include-path.asp" }) {
		t.Fatalf("configured IncludePaths assignment target missing: %#v", builder.payload(2))
	}
}

func TestNavigationVBIncludeExecutionUsesConfiguredVirtualRootResolution(t *testing.T) {
	root := t.TempDir()
	pageDir := filepath.Join(root, "pages")
	sharedDir := filepath.Join(root, "shared")
	if err := os.MkdirAll(pageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sharedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	pagePath := filepath.Join(pageDir, "default.asp")
	includePath := filepath.Join(sharedDir, "common.inc")
	page := core.ParseDocument(filePathURI(pagePath), `<!-- #include virtual="/shared/common.inc" -->
<% Response.Redirect target %>`, core.Settings{})
	include := core.ParseDocument(filePathURI(includePath), `<% target = "virtual-root.asp" %>`, core.Settings{})
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = pageDir
	server.workspaceRoots = []workspaceRoot{{Path: pageDir, URI: filePathURI(pageDir)}}
	server.settings.VirtualRoots = []string{root}
	owners, resolved := navigationIncludeRelationsWithProgress(context.Background(), server, []*core.ParsedDocument{page, include}, nil)
	builder := newNavigationGraphBuilder("document", page.URI, []workspaceRoot{{Path: pageDir, URI: filePathURI(pageDir)}})
	builder.prepareVBScriptFunctionsWithIncludes([]*core.ParsedDocument{page, include}, owners, resolved)
	builder.addDocument(page, page.URI)
	if !navigationPayloadNodeMatching(builder.payload(2), func(node map[string]any) bool { return node["label"] == "virtual-root.asp" }) {
		t.Fatalf("configured VirtualRoots assignment target missing: %#v", builder.payload(2))
	}
}

func TestNavigationVBIncludeExecutionUsesConfiguredVirtualRootSingularResolution(t *testing.T) {
	root := t.TempDir()
	pageDir := filepath.Join(root, "pages")
	sharedDir := filepath.Join(root, "shared")
	if err := os.MkdirAll(pageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sharedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	pagePath := filepath.Join(pageDir, "default.asp")
	includePath := filepath.Join(sharedDir, "common.inc")
	page := core.ParseDocument(filePathURI(pagePath), `<!-- #include virtual="/shared/common.inc" -->
<% Response.Redirect target %>`, core.Settings{})
	include := core.ParseDocument(filePathURI(includePath), `<% target = "virtual-root-singular.asp" %>`, core.Settings{})
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = pageDir
	server.workspaceRoots = []workspaceRoot{{Path: pageDir, URI: filePathURI(pageDir)}}
	server.settings.VirtualRoot = root
	owners, resolved := navigationIncludeRelationsWithProgress(context.Background(), server, []*core.ParsedDocument{page, include}, nil)
	builder := newNavigationGraphBuilder("document", page.URI, []workspaceRoot{{Path: pageDir, URI: filePathURI(pageDir)}})
	builder.prepareVBScriptFunctionsWithIncludes([]*core.ParsedDocument{page, include}, owners, resolved)
	builder.addDocument(page, page.URI)
	if !navigationPayloadNodeMatching(builder.payload(2), func(node map[string]any) bool { return node["label"] == "virtual-root-singular.asp" }) {
		t.Fatalf("configured VirtualRoot assignment target missing: %#v", builder.payload(2))
	}
}

func TestNavigationVBNestedCyclicAndSiblingIncludesComposeState(t *testing.T) {
	root := t.TempDir()
	pageURI := filePathURI(filepath.Join(root, "default.asp"))
	firstURI := filePathURI(filepath.Join(root, "first.inc"))
	commonURI := filePathURI(filepath.Join(root, "common.inc"))
	secondURI := filePathURI(filepath.Join(root, "second.inc"))
	page := core.ParseDocument(pageURI, `<!-- #include file="first.inc" -->
<!-- #include file="second.inc" -->
<% Response.Redirect target %>`, core.Settings{})
	first := core.ParseDocument(firstURI, `<!-- #include file="common.inc" -->
<!-- #include file="default.asp" -->`, core.Settings{})
	common := core.ParseDocument(commonURI, `<% target = "nested.asp" %>`, core.Settings{})
	second := core.ParseDocument(secondURI, `<% target = "sibling.asp" %>`, core.Settings{})
	owners := map[string][]string{
		workspacepkg.FileIdentityKeyFromURI(firstURI):  {pageURI},
		workspacepkg.FileIdentityKeyFromURI(commonURI): {firstURI},
		workspacepkg.FileIdentityKeyFromURI(secondURI): {pageURI},
	}
	builder := newNavigationGraphBuilder("document", pageURI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	builder.prepareVBScriptFunctions([]*core.ParsedDocument{page, first, common, second}, owners)
	builder.addDocument(page, pageURI)
	if len(builder.edges) != 1 {
		t.Fatalf("nested/cyclic/sibling include edges = %#v, want one redirect", builder.edges)
	}
	if !navigationPayloadNodeMatching(builder.payload(4), func(node map[string]any) bool { return node["label"] == "sibling.asp" }) {
		t.Fatalf("sibling include assignment did not follow nested/cyclic include order: %#v", builder.payload(4))
	}
}

func TestNavigationVBIncludeExecutionStateDoesNotLeakAcrossOwners(t *testing.T) {
	root := t.TempDir()
	firstURI := filePathURI(filepath.Join(root, "first.asp"))
	secondURI := filePathURI(filepath.Join(root, "second.asp"))
	commonURI := filePathURI(filepath.Join(root, "common.inc"))
	first := core.ParseDocument(firstURI, `<% target = "first.asp" %>
<!-- #include file="common.inc" -->
<% Response.Redirect target %>`, core.Settings{})
	second := core.ParseDocument(secondURI, `<% Response.Redirect target %>`, core.Settings{})
	common := core.ParseDocument(commonURI, `<% target = "common.asp" %>`, core.Settings{})
	owners := map[string][]string{workspacepkg.FileIdentityKeyFromURI(commonURI): {firstURI}}
	builder := newNavigationGraphBuilder("workspace", firstURI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	builder.prepareVBScriptFunctions([]*core.ParsedDocument{first, second, common}, owners)
	builder.addDocument(first, firstURI)
	builder.addDocument(second, secondURI)
	if !navigationPayloadNodeMatching(builder.payload(3), func(node map[string]any) bool { return node["label"] == "common.asp" }) {
		t.Fatalf("owner include assignment target missing: %#v", builder.payload(3))
	}
	if len(builder.edges) != 2 {
		t.Fatalf("cross-owner include edges = %#v, want owner target and unknown target", builder.edges)
	}
	if !navigationPayloadNodeMatching(builder.payload(3), func(node map[string]any) bool { return node["kind"] == "unknown" }) {
		t.Fatalf("cross-owner include state leaked instead of preserving unknown second-owner target: %#v", builder.payload(3))
	}
}

func TestNavigationVBTransitiveIncludeFunctionsReachRootOwner(t *testing.T) {
	root := t.TempDir()
	pageURI := filePathURI(filepath.Join(root, "default.asp"))
	middleURI := filePathURI(filepath.Join(root, "middle.inc"))
	commonURI := filePathURI(filepath.Join(root, "common.inc"))
	page := core.ParseDocument(pageURI, `<% Response.Redirect BuildTarget() %>`, core.Settings{})
	middle := core.ParseDocument(middleURI, `<% <!-- middle include --> %>`, core.Settings{})
	common := core.ParseDocument(commonURI, `<% Function BuildTarget()
BuildTarget = "transitive.asp"
End Function %>`, core.Settings{})
	builder := newNavigationGraphBuilder("document", pageURI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	owners := map[string][]string{
		workspacepkg.FileIdentityKeyFromURI(middleURI): {pageURI},
		workspacepkg.FileIdentityKeyFromURI(commonURI): {middleURI},
	}
	builder.prepareVBScriptFunctions([]*core.ParsedDocument{page, middle, common}, owners)
	builder.addDocument(page, pageURI)
	if !navigationPayloadNodeMatching(builder.payload(1), func(node map[string]any) bool { return node["label"] == "transitive.asp" }) {
		t.Fatalf("transitive include function target missing: %#v", builder.payload(1))
	}
}

func TestNavigationVBCyclicIncludeFunctionsTerminateDeterministically(t *testing.T) {
	root := t.TempDir()
	pageURI := filePathURI(filepath.Join(root, "default.asp"))
	firstURI := filePathURI(filepath.Join(root, "first.inc"))
	secondURI := filePathURI(filepath.Join(root, "second.inc"))
	page := core.ParseDocument(pageURI, `<% Response.Redirect BuildTarget() %>`, core.Settings{})
	first := core.ParseDocument(firstURI, `<% Function BuildTarget()
BuildTarget = "cyclic.asp"
End Function %>`, core.Settings{})
	second := core.ParseDocument(secondURI, `<% Function OtherTarget()
OtherTarget = "other.asp"
End Function %>`, core.Settings{})
	owners := map[string][]string{
		workspacepkg.FileIdentityKeyFromURI(firstURI):  {pageURI, secondURI},
		workspacepkg.FileIdentityKeyFromURI(secondURI): {firstURI},
	}
	builder := newNavigationGraphBuilder("document", pageURI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	builder.prepareVBScriptFunctions([]*core.ParsedDocument{page, second, first}, owners)
	builder.addDocument(page, pageURI)
	if !navigationPayloadNodeMatching(builder.payload(1), func(node map[string]any) bool { return node["label"] == "cyclic.asp" }) {
		t.Fatalf("cyclic include function target missing: %#v", builder.payload(1))
	}
	closure := navigationVBFunctionOwnerClosure(firstURI, owners)
	if len(closure) != 3 || closure[0] != firstURI || closure[1] != pageURI || closure[2] != secondURI {
		t.Fatalf("cyclic include owner closure = %#v", closure)
	}
}

func TestNavigationVBUsesFunctionsFromIncludedDocument(t *testing.T) {
	root := t.TempDir()
	pageURI := filePathURI(filepath.Join(root, "default.asp"))
	includeURI := filePathURI(filepath.Join(root, "common.inc"))
	if err := os.WriteFile(filepath.Join(root, "next.asp"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	page := core.ParseDocument(pageURI, `<% Response.Redirect BuildTarget() %>`, core.Settings{})
	include := core.ParseDocument(includeURI, `<% Function BuildTarget()
BuildTarget = "next.asp"
End Function %>`, core.Settings{})
	owners := map[string][]string{workspacepkg.FileIdentityKeyFromURI(includeURI): []string{pageURI}}
	builder := newNavigationGraphBuilder("document", pageURI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	builder.prepareVBScriptFunctions([]*core.ParsedDocument{page, include}, owners)
	builder.addDocument(page, pageURI)
	if !navigationPayloadNodeMatching(builder.payload(1), func(node map[string]any) bool { return node["label"] == "next.asp" }) {
		t.Fatalf("included function target missing: %#v", builder.payload(1))
	}
}

func TestNavigationVBIncludedFragmentSeesOwnerAndSiblingFunctions(t *testing.T) {
	root := t.TempDir()
	pageURI := filePathURI(filepath.Join(root, "default.asp"))
	firstURI := filePathURI(filepath.Join(root, "first.inc"))
	secondURI := filePathURI(filepath.Join(root, "second.inc"))
	for _, name := range []string{"local.asp", "sibling.asp", "root-only.asp"} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	page := core.ParseDocument(pageURI, `<% Function RootTarget()
RootTarget = "root-only.asp"
End Function %>`, core.Settings{})
	first := core.ParseDocument(firstURI, `<% Function RootTarget()
RootTarget = "local.asp"
End Function
Response.Redirect RootTarget()
Response.Redirect SiblingTarget() %>`, core.Settings{})
	second := core.ParseDocument(secondURI, `<% Function SiblingTarget()
SiblingTarget = "sibling.asp"
End Function %>`, core.Settings{})
	owners := map[string][]string{
		workspacepkg.FileIdentityKeyFromURI(firstURI):  {pageURI},
		workspacepkg.FileIdentityKeyFromURI(secondURI): {pageURI},
	}
	builder := newNavigationGraphBuilder("document", pageURI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	builder.prepareVBScriptFunctions([]*core.ParsedDocument{page, second, first}, owners)
	builder.addDocument(first, pageURI)
	payload := builder.payload(3)
	for _, name := range []string{"local.asp", "sibling.asp"} {
		if !navigationPayloadNodeMatching(payload, func(node map[string]any) bool { return node["label"] == name }) {
			t.Fatalf("included fragment target %s missing: %#v", name, payload)
		}
	}
	if navigationPayloadNodeMatching(payload, func(node map[string]any) bool { return node["label"] == "root-only.asp" }) {
		t.Fatalf("included local function did not override owner function: %#v", payload)
	}
}

func TestNavigationVBResolvesFixedPathWithUnknownQuery(t *testing.T) {
	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	target := filepath.Join(root, "next.asp")
	if err := os.WriteFile(target, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	source := `<% id = Request.QueryString("id") : Response.Redirect "next.asp?id=" & id %>`
	parsed := core.ParseDocument(filePathURI(page), source, core.Settings{})
	builder := newNavigationGraphBuilder("document", parsed.URI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	builder.addDocument(parsed, parsed.URI)
	if navigationPayloadNodeMatching(builder.payload(1), func(node map[string]any) bool { return node["kind"] == "unknown" }) {
		t.Fatalf("fixed path with unknown query produced an unknown node: %#v", builder.payload(1))
	}
	if !navigationPayloadNodeMatching(builder.payload(1), func(node map[string]any) bool { return node["uri"] == filePathURI(target) }) {
		t.Fatalf("fixed path target missing: %#v", builder.payload(1))
	}
	if len(builder.edges) != 1 || !navigationPayloadEdgeParameter(builder.edges[0], "id", "queryString") {
		t.Fatalf("fixed path query edge = %#v", builder.edges)
	}
}

func TestNavigationHTMLASPNavigationPreservesExpressionRangeAndUTF16(t *testing.T) {
	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	if err := os.WriteFile(filepath.Join(root, "next.asp"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	source := "🌸<% target = \"next.asp\" %>\n<a href=\"<%= target %>\">Next</a>"
	parsed := core.ParseDocument(filePathURI(page), source, core.Settings{})
	builder := newNavigationGraphBuilder("document", parsed.URI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	builder.addDocument(parsed, parsed.URI)
	if len(builder.edges) != 1 {
		t.Fatalf("interpolated html edges = %#v", builder.edges)
	}
	rangeValue := builder.edges[0]["ranges"].([]lsp.Range)[0]
	if rangeValue.Start.Line != 1 || rangeValue.Start.Character == 0 {
		t.Fatalf("interpolated html UTF-16 range = %#v", rangeValue)
	}
	if rangeValue.Start.Character != 9 {
		t.Fatalf("interpolated html range character = %d, want UTF-16 offset 9", rangeValue.Start.Character)
	}
}

func TestNavigationHTMLMultipleASPInterpolationsMergeDynamicEvidence(t *testing.T) {
	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	source := `<a href="folder/<%= "section" %>/<%= Request.QueryString("id") %>.asp">Next</a>`
	parsed := core.ParseDocument(filePathURI(page), source, core.Settings{})
	builder := newNavigationGraphBuilder("document", parsed.URI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	builder.addDocument(parsed, parsed.URI)
	if len(builder.edges) != 1 {
		t.Fatalf("multiple interpolation edges = %#v", builder.edges)
	}
	edge := builder.edges[0]
	if edge["confidence"] != "possible" {
		t.Fatalf("multiple interpolation confidence = %#v, want possible", edge["confidence"])
	}
	if parameters, _ := edge["parameters"].([]map[string]any); !navigationPayloadEdgeParameter(edge, "id", "queryString") || len(parameters) != 1 {
		t.Fatalf("multiple interpolation parameters = %#v", edge["parameters"])
	}
	if ranges, _ := edge["ranges"].([]lsp.Range); len(ranges) != 2 {
		t.Fatalf("multiple interpolation ranges = %#v, want two expression ranges", edge["ranges"])
	}
	evidence, _ := edge["evidence"].([]map[string]any)
	if len(evidence) != 2 || !strings.Contains(navigationString(evidence[1]["snippet"]), "Request.QueryString") {
		t.Fatalf("multiple interpolation evidence = %#v", evidence)
	}
	if !navigationPayloadNodeMatching(builder.payload(1), func(node map[string]any) bool {
		return node["kind"] == "unknown" && strings.Contains(navigationString(node["label"]), "queryString:id")
	}) {
		t.Fatalf("multiple interpolation unknown target missing: %#v", builder.payload(1))
	}
}

func TestNavigationHTMLInterpolationUsesSourceOrderedIncludeAssignment(t *testing.T) {
	root := t.TempDir()
	pageURI := filePathURI(filepath.Join(root, "default.asp"))
	includeURI := filePathURI(filepath.Join(root, "common.inc"))
	if err := os.WriteFile(filepath.Join(root, "included.asp"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	page := core.ParseDocument(pageURI, `<!-- #include file="common.inc" -->
<a href="<%= target %>">Next</a>`, core.Settings{})
	include := core.ParseDocument(includeURI, `<% target = "included.asp" %>`, core.Settings{})
	owners := map[string][]string{workspacepkg.FileIdentityKeyFromURI(includeURI): {pageURI}}
	builder := newNavigationGraphBuilder("document", pageURI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	builder.prepareVBScriptFunctions([]*core.ParsedDocument{page, include}, owners)
	builder.addDocument(page, pageURI)
	if !navigationPayloadNodeMatching(builder.payload(2), func(node map[string]any) bool { return node["label"] == "included.asp" }) {
		t.Fatalf("include assignment did not reach HTML interpolation: %#v", builder.payload(2))
	}
	if len(builder.edges) != 1 {
		t.Fatalf("include assignment interpolation edges = %#v, want one edge", builder.edges)
	}
	evidence, _ := builder.edges[0]["evidence"].([]map[string]any)
	if len(evidence) != 1 || evidence[0]["uri"] != pageURI {
		t.Fatalf("include assignment interpolation evidence = %#v, want page URI", evidence)
	}
}

func TestNavigationHTMLIncludedInterpolationKeepsExpressionEvidenceURI(t *testing.T) {
	root := t.TempDir()
	pageURI := filePathURI(filepath.Join(root, "default.asp"))
	includeURI := filePathURI(filepath.Join(root, "common.inc"))
	if err := os.WriteFile(filepath.Join(root, "included.asp"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	page := core.ParseDocument(pageURI, `<!-- #include file="common.inc" -->`, core.Settings{})
	include := core.ParseDocument(includeURI, `<% target = "included.asp" %>
<a href="<%= target %>">Next</a>`, core.Settings{})
	owners := map[string][]string{workspacepkg.FileIdentityKeyFromURI(includeURI): {pageURI}}
	builder := newNavigationGraphBuilder("document", pageURI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	builder.prepareVBScriptFunctions([]*core.ParsedDocument{page, include}, owners)
	builder.addDocument(page, pageURI)
	builder.addDocument(include, pageURI)
	if len(builder.edges) != 1 {
		t.Fatalf("included interpolation edges = %#v, want one edge", builder.edges)
	}
	evidence, _ := builder.edges[0]["evidence"].([]map[string]any)
	if len(evidence) != 1 || evidence[0]["uri"] != includeURI {
		t.Fatalf("included interpolation evidence = %#v, want include URI", evidence)
	}
}

func TestNavigationHTMLInterpolationUsesConfiguredIncludePath(t *testing.T) {
	root := t.TempDir()
	pageDir := filepath.Join(root, "pages")
	includeDir := filepath.Join(root, "includes")
	if err := os.MkdirAll(pageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(includeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	pagePath := filepath.Join(pageDir, "default.asp")
	includePath := filepath.Join(includeDir, "common.inc")
	if err := os.WriteFile(filepath.Join(pageDir, "configured.asp"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(includePath, []byte(`<% target = "configured.asp" %>`), 0o644); err != nil {
		t.Fatal(err)
	}
	page := core.ParseDocument(filePathURI(pagePath), `<!-- #include file="common.inc" -->
<a href="<%= target %>">Next</a>`, core.Settings{})
	include := core.ParseDocument(filePathURI(includePath), `<% target = "configured.asp" %>`, core.Settings{})
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = pageDir
	server.workspaceRoots = []workspaceRoot{{Path: pageDir, URI: filePathURI(pageDir)}}
	server.settings.IncludePaths = []string{includeDir}
	owners, resolved := navigationIncludeRelationsWithProgress(context.Background(), server, []*core.ParsedDocument{page, include}, nil)
	builder := newNavigationGraphBuilder("document", page.URI, []workspaceRoot{{Path: pageDir, URI: filePathURI(pageDir)}})
	builder.prepareVBScriptFunctionsWithIncludes([]*core.ParsedDocument{page, include}, owners, resolved)
	builder.addDocument(page, page.URI)
	if !navigationPayloadNodeMatching(builder.payload(2), func(node map[string]any) bool { return node["label"] == "configured.asp" }) {
		t.Fatalf("configured include assignment did not reach HTML interpolation: %#v", builder.payload(2))
	}
}

func TestNavigationHTMLInterpolationUsesSourceOrderedIncludedFunction(t *testing.T) {
	root := t.TempDir()
	pageURI := filePathURI(filepath.Join(root, "default.asp"))
	includeURI := filePathURI(filepath.Join(root, "common.inc"))
	if err := os.WriteFile(filepath.Join(root, "included-function.asp"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	page := core.ParseDocument(pageURI, `<!-- #include file="common.inc" -->
<a href="<%= BuildTarget() %>">Next</a>`, core.Settings{})
	include := core.ParseDocument(includeURI, `<% Function BuildTarget()
BuildTarget = "included-function.asp"
End Function %>`, core.Settings{})
	owners := map[string][]string{workspacepkg.FileIdentityKeyFromURI(includeURI): {pageURI}}
	builder := newNavigationGraphBuilder("document", pageURI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	builder.prepareVBScriptFunctions([]*core.ParsedDocument{page, include}, owners)
	builder.addDocument(page, pageURI)
	if !navigationPayloadNodeMatching(builder.payload(2), func(node map[string]any) bool { return node["label"] == "included-function.asp" }) {
		t.Fatalf("included function did not reach HTML interpolation: %#v", builder.payload(2))
	}
}

func TestNavigationHTMLInterpolationUsesTransitiveIncludedFunction(t *testing.T) {
	root := t.TempDir()
	pageURI := filePathURI(filepath.Join(root, "default.asp"))
	middleURI := filePathURI(filepath.Join(root, "middle.inc"))
	commonURI := filePathURI(filepath.Join(root, "common.inc"))
	if err := os.WriteFile(filepath.Join(root, "transitive-function.asp"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	page := core.ParseDocument(pageURI, `<!-- #include file="middle.inc" -->
<a href="<%= BuildTarget() %>">Next</a>`, core.Settings{})
	middle := core.ParseDocument(middleURI, `<!-- #include file="common.inc" -->`, core.Settings{})
	common := core.ParseDocument(commonURI, `<% Function BuildTarget()
BuildTarget = "transitive-function.asp"
End Function %>`, core.Settings{})
	owners := map[string][]string{
		workspacepkg.FileIdentityKeyFromURI(middleURI): {pageURI},
		workspacepkg.FileIdentityKeyFromURI(commonURI): {middleURI},
	}
	builder := newNavigationGraphBuilder("document", pageURI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	builder.prepareVBScriptFunctions([]*core.ParsedDocument{page, middle, common}, owners)
	builder.addDocument(page, pageURI)
	if !navigationPayloadNodeMatching(builder.payload(3), func(node map[string]any) bool { return node["label"] == "transitive-function.asp" }) {
		t.Fatalf("transitive included function did not reach HTML interpolation: %#v", builder.payload(3))
	}
}

func TestNavigationHTMLMultipleASPInterpolationsPreserveStatefulEffects(t *testing.T) {
	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	if err := os.WriteFile(filepath.Join(root, "stateful.asp"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	source := `<%
Function SetTarget(value)
  target = value
  SetTarget = value
End Function
%>
<a href="<%= SetTarget("stateful.asp") %>?id=<%= target %>">Next</a>`
	parsed := core.ParseDocument(filePathURI(page), source, core.Settings{})
	builder := newNavigationGraphBuilder("document", parsed.URI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	builder.addDocument(parsed, parsed.URI)
	if len(builder.edges) != 1 {
		t.Fatalf("stateful interpolation edges = %#v, want one edge", builder.edges)
	}
	if !navigationPayloadNodeMatching(builder.payload(1), func(node map[string]any) bool { return node["label"] == "stateful.asp" }) {
		t.Fatalf("stateful interpolation target missing: %#v", builder.payload(1))
	}
	if ranges, _ := builder.edges[0]["ranges"].([]lsp.Range); len(ranges) != 2 {
		t.Fatalf("stateful interpolation ranges = %#v, want both expression ranges", builder.edges[0]["ranges"])
	}
}

func TestNavigationHTMLIndependentASPAnchorsDoNotShareVariantCap(t *testing.T) {
	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	var source strings.Builder
	source.WriteString("<%\n")
	for index := 1; index <= 7; index++ {
		fmt.Fprintf(&source, "target%d = IIf(enabled, \"anchor%d-one.asp\", \"anchor%d-two.asp\")\n", index, index, index)
	}
	source.WriteString("%>\n")
	for index := 1; index <= 7; index++ {
		fmt.Fprintf(&source, "<a href=\"<%%= target%d %%>\">Anchor %d</a>\n", index, index)
		for _, suffix := range []string{"one", "two"} {
			name := fmt.Sprintf("anchor%d-%s.asp", index, suffix)
			if err := os.WriteFile(filepath.Join(root, name), nil, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	parsed := core.ParseDocument(filePathURI(page), source.String(), core.Settings{})
	builder := newNavigationGraphBuilder("document", parsed.URI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	builder.addDocument(parsed, parsed.URI)
	if len(builder.edges) != 14 {
		t.Fatalf("independent interpolated anchor edges = %d, want 14: %#v", len(builder.edges), builder.edges)
	}
	for index := 1; index <= 7; index++ {
		for _, suffix := range []string{"one", "two"} {
			label := fmt.Sprintf("anchor%d-%s.asp", index, suffix)
			if !navigationPayloadNodeMatching(builder.payload(1), func(node map[string]any) bool { return node["label"] == label }) {
				t.Fatalf("independent interpolated anchor target %s missing: %#v", label, builder.payload(1))
			}
		}
	}
}

func TestNavigationHTMLInterpolationVariantCapAddsDeterministicUnknownFallback(t *testing.T) {
	values := make([]navigationValue, navigationHTMLInterpolationVariantLimit+1)
	for index := range values {
		values[index] = navigationValue{Kind: navigationValueLiteral, Text: fmt.Sprintf("target-%02d.asp", index)}
	}
	interpolation := navigationHTMLInterpolation{
		marker: "marker",
		region: core.Region{Start: 0, End: 1},
		values: values,
	}
	variants := navigationHTMLInterpolationVariants(`<a href="marker">`, "m", []navigationHTMLInterpolation{interpolation})
	if len(variants) != navigationHTMLInterpolationVariantLimit {
		t.Fatalf("interpolation variant count = %d, want exact cap %d including fallback", len(variants), navigationHTMLInterpolationVariantLimit)
	}
	fallbacks := 0
	concrete := 0
	for _, variant := range variants {
		if strings.Contains(variant.text, "{unknown}") {
			fallbacks++
			if variant.context == nil || variant.context.value.Kind != navigationValueUnknown {
				t.Fatalf("truncated interpolation fallback context = %#v", variant.context)
			}
		} else {
			concrete++
		}
	}
	if fallbacks != 1 {
		t.Fatalf("truncated interpolation fallback count = %d, want 1: %#v", fallbacks, variants)
	}
	if concrete != navigationHTMLInterpolationVariantLimit-1 {
		t.Fatalf("truncated interpolation concrete count = %d, want %d: %#v", concrete, navigationHTMLInterpolationVariantLimit-1, variants)
	}
}

func TestNavigationHTMLInterpolationVariantCapIncludesCartesianFallback(t *testing.T) {
	firstValues := make([]navigationValue, 8)
	for index := range firstValues {
		firstValues[index] = navigationValue{Kind: navigationValueLiteral, Text: fmt.Sprintf("first-%02d", index)}
	}
	secondValues := make([]navigationValue, 9)
	for index := range secondValues {
		secondValues[index] = navigationValue{Kind: navigationValueLiteral, Text: fmt.Sprintf("second-%02d", index)}
	}
	variants := navigationHTMLInterpolationVariants("<a href=\"first/second\">", "source", []navigationHTMLInterpolation{
		{marker: "first", region: core.Region{Start: 0, End: 1}, values: firstValues},
		{marker: "second", region: core.Region{Start: 1, End: 2}, values: secondValues},
	})
	if len(variants) != navigationHTMLInterpolationVariantLimit {
		t.Fatalf("Cartesian interpolation variant count = %d, want exact cap %d", len(variants), navigationHTMLInterpolationVariantLimit)
	}
	fallbacks := 0
	for _, variant := range variants {
		if strings.Contains(variant.text, "{unknown}") {
			fallbacks++
		}
	}
	if fallbacks != 1 {
		t.Fatalf("Cartesian interpolation fallback count = %d, want 1", fallbacks)
	}
}

func TestNavigationHTMLInterpolationElementBudgetKeepsUnknownFallback(t *testing.T) {
	count := navigationHTMLInterpolationElementCountLimit + 4096
	interpolations := make([]navigationHTMLInterpolation, count)
	var template strings.Builder
	template.WriteString(`<a href="`)
	for index := range interpolations {
		marker := navigationHTMLInterpolationMarker(index)
		interpolations[index] = navigationHTMLInterpolation{
			marker: marker,
			region: core.Region{Start: index, End: index + 1},
			values: []navigationValue{{Kind: navigationValueLiteral, Text: fmt.Sprintf("target-%d.asp", index)}},
		}
		template.WriteString(marker)
	}
	template.WriteString(`">Next</a>`)
	elements := navigationHTMLInterpolationElements(template.String(), interpolations)
	if len(elements) != 1 || !elements[0].budgetExceeded {
		t.Fatalf("large interpolation element budget = %#v, want one exceeded element", elements)
	}
	variants, err := navigationHTMLInterpolationVariantsContext(context.Background(), elements[0].text, strings.Repeat("x", count+1), elements[0].interpolations, elements[0].budgetExceeded)
	if err != nil {
		t.Fatalf("large interpolation fallback error = %v", err)
	}
	if len(variants) != 1 {
		t.Fatalf("large interpolation variants = %d, want one conservative fallback", len(variants))
	}
	if !strings.Contains(variants[0].text, "{unknown}") || strings.Contains(variants[0].text, navigationHTMLInterpolationMarkerPrefix) || variants[0].context == nil || variants[0].context.value.Kind != navigationValueUnknown {
		t.Fatalf("large interpolation fallback text = %q, want unknown without raw markers", variants[0].text)
	}
	if variants[0].context == nil || len(variants[0].context.rangeValues) > navigationHTMLInterpolationElementCountLimit {
		t.Fatalf("large interpolation fallback context = %#v, want bounded metadata", variants[0].context)
	}
}

func TestNavigationHTMLInterpolationVariantsCancellationStopsExpansion(t *testing.T) {
	const count = 32
	interpolations := make([]navigationHTMLInterpolation, count)
	var template strings.Builder
	for index := range interpolations {
		marker := navigationHTMLInterpolationMarker(index)
		interpolations[index] = navigationHTMLInterpolation{
			marker: marker,
			region: core.Region{Start: index, End: index + 1},
			values: []navigationValue{{Kind: navigationValueLiteral, Text: fmt.Sprintf("target-%d.asp", index)}},
		}
		template.WriteString(marker)
	}
	ctx := &navigationHTMLInterpolationTestCancelContext{Context: context.Background(), cancelAfter: 12}
	_, err := navigationHTMLInterpolationVariantsContext(ctx, template.String(), strings.Repeat("x", count+1), interpolations, false)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled interpolation expansion error = %v, want context.Canceled", err)
	}
	if ctx.calls < ctx.cancelAfter {
		t.Fatalf("cancelled interpolation expansion checks = %d, want at least %d", ctx.calls, ctx.cancelAfter)
	}
}

func TestNavigationHTMLInterpolationVariantsBelowBudgetAreDeterministic(t *testing.T) {
	interpolations := []navigationHTMLInterpolation{
		{
			marker: navigationHTMLInterpolationMarker(0),
			region: core.Region{Start: 0, End: 1},
			values: []navigationValue{
				{Kind: navigationValueLiteral, Text: "z.asp"},
				{Kind: navigationValueLiteral, Text: "a.asp"},
			},
		},
		{
			marker: navigationHTMLInterpolationMarker(1),
			region: core.Region{Start: 1, End: 3},
			values: []navigationValue{
				{Kind: navigationValueLiteral, Text: "2"},
				{Kind: navigationValueLiteral, Text: "1"},
			},
		},
	}
	template := `<a href="` + interpolations[0].marker + `/` + interpolations[1].marker + `">Next</a>`
	source := strings.Repeat("x", 8)
	first := navigationHTMLInterpolationVariants(template, source, interpolations)
	second := navigationHTMLInterpolationVariants(template, source, interpolations)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("below-budget interpolation variants differ:\nfirst=%#v\nsecond=%#v", first, second)
	}
	if len(first) != 4 || !strings.Contains(first[0].text, "a.asp") || !strings.Contains(first[0].text, "1\x00/ASP_NAV_VALUE") {
		t.Fatalf("below-budget interpolation variants = %#v, want four sorted concrete variants", first)
	}
	if first[0].context == nil || len(first[0].context.rangeValues) != len(interpolations) {
		t.Fatalf("below-budget interpolation context = %#v, want exact ranges", first[0].context)
	}
}

type navigationHTMLInterpolationTestCancelContext struct {
	context.Context
	cancelAfter int
	calls       int
}

func (ctx *navigationHTMLInterpolationTestCancelContext) Err() error {
	ctx.calls++
	if ctx.calls >= ctx.cancelAfter {
		return context.Canceled
	}
	return ctx.Context.Err()
}

func TestNavigationUnknownASPPathRetainsPatternNode(t *testing.T) {
	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	source := `<% target = Request.QueryString("next") %><a href="<%= target %>">Next</a>`
	parsed := core.ParseDocument(filePathURI(page), source, core.Settings{})
	builder := newNavigationGraphBuilder("document", parsed.URI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	builder.addDocument(parsed, parsed.URI)
	if !navigationPayloadNodeMatching(builder.payload(1), func(node map[string]any) bool {
		return node["kind"] == "unknown" && node["label"] == "{queryString:next}"
	}) {
		t.Fatalf("unknown ASP path node missing: %#v", builder.payload(1))
	}
}

func navigationPayloadNodeMatching(payload map[string]any, predicate func(map[string]any) bool) bool {
	nodes, _ := payload["nodes"].([]map[string]any)
	for _, node := range nodes {
		if predicate(node) {
			return true
		}
	}
	return false
}

func navigationPayloadEdgeParameter(edge map[string]any, name, source string) bool {
	parameters, _ := edge["parameters"].([]map[string]any)
	for _, parameter := range parameters {
		if parameter["name"] == name && parameter["source"] == source {
			return true
		}
	}
	return false
}

func TestNavigationPayloadIsDeterministic(t *testing.T) {
	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	source := `<a href="b.asp">B</a><a href="a.asp">A</a>`
	parsed := core.ParseDocument(filePathURI(page), source, core.Settings{})
	build := func() []byte {
		builder := newNavigationGraphBuilder("document", parsed.URI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
		return mustMarshalNavigationPayload(builder, parsed)
	}
	first, second := build(), build()
	if string(first) != string(second) {
		t.Fatalf("navigation payload changed between identical builds:\n%s\n%s", first, second)
	}
}

func TestNavigationRejectsUntrustedRelativeTarget(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "outside.asp"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	page := filepath.Join(root, "default.asp")
	parsed := core.ParseDocument(filePathURI(page), `<a href="../`+filepath.Base(outside)+`/outside.asp">outside</a>`, core.Settings{})
	builder := newNavigationGraphBuilder("document", parsed.URI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	builder.addDocument(parsed, parsed.URI)
	if len(builder.edges) != 0 {
		t.Fatalf("untrusted navigation target produced edges: %#v", builder.edges)
	}
}

func TestNavigationVBControlConditionsEvaluateFunctionByRefEffectsInOrder(t *testing.T) {
	tests := map[string]struct {
		name   string
		source string
		want   []string
	}{
		"if": {
			name: "if",
			source: `<%
value = "before.asp"
Function Mutate(ByRef target)
  target = "condition.asp"
  Mutate = True
End Function
If Mutate(value) Then
  value = "then.asp"
Else
  value = "else.asp"
End If
Response.Redirect value
%>`,
			want: []string{"then.asp", "else.asp"},
		},
		"elseif": {
			name: "elseif",
			source: `<%
value = "before.asp"
Function Mutate(ByRef target)
  target = "condition.asp"
  Mutate = True
End Function
If disabled Then
  value = "then.asp"
ElseIf Mutate(value) Then
  value = "elseif.asp"
Else
  value = "else.asp"
End If
Response.Redirect value
%>`,
			want: []string{"then.asp", "elseif.asp", "else.asp"},
		},
		"while": {
			name: "while",
			source: `<%
value = "before.asp"
Function Mutate(ByRef target)
  target = "condition.asp"
  Mutate = True
End Function
While Mutate(value)
  value = "body.asp"
Wend
Response.Redirect value
%>`,
			want: []string{"condition.asp", "body.asp"},
		},
		"do while": {
			name: "do while",
			source: `<%
value = "before.asp"
Function Mutate(ByRef target)
  target = "condition.asp"
  Mutate = True
End Function
Do While Mutate(value)
  value = "body.asp"
Loop
Response.Redirect value
%>`,
			want: []string{"condition.asp", "body.asp"},
		},
		"do until": {
			name: "do until",
			source: `<%
value = "before.asp"
Function Mutate(ByRef target)
  target = "condition.asp"
  Mutate = True
End Function
Do Until Mutate(value)
  value = "body.asp"
Loop
Response.Redirect value
%>`,
			want: []string{"condition.asp", "body.asp"},
		},
		"select": {
			name: "select",
			source: `<%
	value = "before.asp"
	target = "before-target.asp"
	Function Mutate(ByRef target)
		target = "condition.asp"
		Mutate = 1
	End Function
	Select Case Mutate(value)
	Case 1
	  target = "one.asp"
	Case Mutate(value)
	  target = "two.asp"
	Case Else
	  target = "else.asp"
	End Select
	Response.Redirect value
%>`,
			want: []string{"condition.asp"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidates := extractVBScriptNavigationCandidates(test.source[2:len(test.source)-2], 2, test.source)
			if len(candidates) != 1 {
				t.Fatalf("control condition candidates = %#v", candidates)
			}
			values := candidates[0].Value.finiteCandidates()
			for _, want := range test.want {
				if !navigationVBValueTextsContain(values, want) {
					t.Fatalf("control condition value %q missing from %#v", want, values)
				}
			}
			if navigationVBValueTextsContain(values, "before.asp") {
				t.Fatalf("control condition retained pre-call value: %#v", values)
			}
		})
	}
}

func TestNavigationVBControlConditionGlobalFunctionMutationIsMerged(t *testing.T) {
	const source = `<%
target = "before.asp"
Function Mutate()
  target = "global-condition.asp"
  Mutate = True
End Function
If Mutate() Then
  Response.Redirect target
End If
%>`
	candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
	if len(candidates) != 1 || candidates[0].Value.Text != "global-condition.asp" {
		t.Fatalf("global condition mutation = %#v, want global-condition.asp", candidates)
	}
}

func TestNavigationVBElseIfConditionEffectsStayOnReachablePaths(t *testing.T) {
	const source = `<%
	globalTarget = "before.asp"
	sideEffect = "before-side.asp"
	Function Mutate()
	  sideEffect = "condition-side.asp"
	  Mutate = True
End Function
If enabled Then
  globalTarget = "then.asp"
ElseIf Mutate() Then
  globalTarget = "elseif.asp"
	Else
	  globalTarget = "else.asp"
	End If
	Response.Redirect sideEffect
%>`
	candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
	if len(candidates) != 1 {
		t.Fatalf("ElseIf condition candidates = %#v", candidates)
	}
	values := candidates[0].Value.finiteCandidates()
	for _, want := range []string{"before-side.asp", "condition-side.asp"} {
		if !navigationVBValueTextsContain(values, want) {
			t.Fatalf("ElseIf condition value %q missing from %#v", want, values)
		}
	}
}

func TestNavigationVBCaseConditionEffectsStayOnReachablePaths(t *testing.T) {
	const source = `<%
sideEffect = "before-side.asp"
Function Mutate()
  sideEffect = "case-side.asp"
  Mutate = 1
End Function
Select Case mode
Case 1
  target = "one.asp"
Case Mutate()
  target = "two.asp"
Case Else
  target = "else.asp"
End Select
Response.Redirect sideEffect
%>`
	candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
	if len(candidates) != 1 {
		t.Fatalf("Case condition candidates = %#v", candidates)
	}
	values := candidates[0].Value.finiteCandidates()
	for _, want := range []string{"before-side.asp", "case-side.asp"} {
		if !navigationVBValueTextsContain(values, want) {
			t.Fatalf("Case condition value %q missing from %#v", want, values)
		}
	}
}

func TestNavigationVBControlConditionGlobalMutationVariants(t *testing.T) {
	tests := map[string]string{
		"branch": `<%
target = "before-branch.asp"
Function Mutate()
  target = "after-branch.asp"
  Mutate = True
End Function
If Mutate() Then
  Response.Redirect target
End If
%>`,
		"loop": `<%
target = "before-loop.asp"
Function Mutate()
  target = "after-loop.asp"
  Mutate = True
End Function
While Mutate()
  Exit While
Wend
Response.Redirect target
%>`,
		"select": `<%
target = "before-select.asp"
Function Mutate()
  target = "after-select.asp"
  Mutate = 1
End Function
Select Case Mutate()
Case 1
  Response.Redirect target
Case Else
  Response.Redirect target
End Select
%>`,
	}
	for name, source := range tests {
		t.Run(name, func(t *testing.T) {
			candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
			if len(candidates) == 0 {
				t.Fatalf("global condition candidates = %#v", candidates)
			}
			for _, candidate := range candidates {
				if !navigationVBValueTextsContain(candidate.Value.finiteCandidates(), "after-"+name+".asp") {
					t.Fatalf("global condition target = %#v, want after-%s.asp", candidate.Value, name)
				}
			}
		})
	}
}

func TestNavigationVBBareFunctionEffectsAcrossControlExpressions(t *testing.T) {
	tests := map[string]string{
		"if": `<%
target = "before.asp"
Function Mutate()
  target = target & "x"
  Mutate = True
End Function
If Mutate Then
End If
Response.Redirect target
%>`,
		"elseif": `<%
target = "before.asp"
Function Mutate()
  target = target & "x"
  Mutate = True
End Function
If False Then
ElseIf Mutate Then
End If
Response.Redirect target
%>`,
		"while": `<%
target = "before.asp"
Function Mutate()
  target = target & "x"
  Mutate = True
End Function
While Mutate
Wend
Response.Redirect target
%>`,
		"do while": `<%
target = "before.asp"
Function Mutate()
  target = target & "x"
  Mutate = True
End Function
Do While Mutate
Loop
Response.Redirect target
%>`,
		"do until": `<%
target = "before.asp"
Function Mutate()
  target = target & "x"
  Mutate = True
End Function
Do Until Mutate
Loop
Response.Redirect target
%>`,
		"loop while": `<%
target = "before.asp"
Function Mutate()
  target = target & "x"
  Mutate = True
End Function
Do
Loop While Mutate
Response.Redirect target
%>`,
		"loop until": `<%
target = "before.asp"
Function Mutate()
  target = target & "x"
  Mutate = True
End Function
Do
Loop Until Mutate
Response.Redirect target
%>`,
		"select": `<%
target = "before.asp"
Function Mutate()
  target = target & "x"
  Mutate = 1
End Function
Select Case Mutate
Case Else
End Select
Response.Redirect target
%>`,
		"case": `<%
target = "before.asp"
Function Mutate()
  target = target & "x"
  Mutate = 1
End Function
Select Case 0
Case Mutate
End Select
Response.Redirect target
%>`,
		"case range": `<%
target = "before.asp"
Function Mutate()
  target = target & "x"
  Mutate = 1
End Function
Select Case 0
Case 1 To Mutate
End Select
Response.Redirect target
%>`,
		"for bound": `<%
target = "before.asp"
Function Mutate()
  target = target & "x"
  Mutate = 1
End Function
For index = Mutate To 1
Next
Response.Redirect target
%>`,
		"compound": `<%
target = "before.asp"
Function Mutate()
  target = target & "x"
  Mutate = True
End Function
If Mutate And False Then
End If
Response.Redirect target
%>`,
		"compound case": `<%
target = "before.asp"
Function Mutate()
  target = target & "x"
  Mutate = 1
End Function
Select Case 0
Case Mutate And 0
End Select
Response.Redirect target
%>`,
	}
	for name, source := range tests {
		t.Run(name, func(t *testing.T) {
			candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
			if len(candidates) != 1 {
				t.Fatalf("bare control candidates = %#v", candidates)
			}
			values := candidates[0].Value.finiteCandidates()
			if !navigationVBValueTextsContain(values, "before.aspx") {
				t.Fatalf("bare control value missing mutation: %#v", values)
			}
			if navigationVBValueTextsContain(values, "before.aspxx") {
				t.Fatalf("bare control evaluated function more than once: %#v", values)
			}
		})
	}
}

func TestNavigationVBBareFunctionEffectsDoNotCallVariablesOrMembers(t *testing.T) {
	tests := map[string]string{
		"variable": `<%
target = "before.asp"
Mutate = "variable.asp"
Function Mutate()
  target = target & "x"
  Mutate = True
End Function
If Mutate Then
End If
Response.Redirect target
%>`,
		"member": `<%
target = "before.asp"
Function Mutate()
  target = target & "x"
  Mutate = True
End Function
If object.Mutate Then
End If
Response.Redirect target
%>`,
		"builtin": `<%
target = "before.asp"
If Request Then
End If
Response.Redirect target
%>`,
	}
	for name, source := range tests {
		t.Run(name, func(t *testing.T) {
			candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
			if len(candidates) != 1 {
				t.Fatalf("bare no-call candidates = %#v", candidates)
			}
			values := candidates[0].Value.finiteCandidates()
			if !navigationVBValueTextsContain(values, "before.asp") {
				t.Fatalf("bare no-call mutated target: %#v", values)
			}
			if navigationVBValueTextsContain(values, "before.aspx") {
				t.Fatalf("bare no-call unexpectedly evaluated function: %#v", values)
			}
		})
	}
}

func TestNavigationVBCancellationDiscardsPartialCandidates(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	const source = `<%
Response.Redirect "stale.asp"
%>`
	candidates := extractVBScriptNavigationCandidatesWithContext(ctx, source[2:len(source)-2], 2, source)
	if len(candidates) != 0 {
		t.Fatalf("cancelled extraction published candidates = %#v", candidates)
	}
}

func TestNavigationVBExpressionBudgetBoundsDeepParentheses(t *testing.T) {
	tokens := navigationVBDeepParenthesizedTokens(navigationVBExpressionDepthLimit * 4)
	state := newNavigationVBState()
	value := evaluateNavigationVBExpression(tokens, state)
	if value.Kind != navigationVBValueUnknown || value.Text != "{unknown}" {
		t.Fatalf("deep parenthesized expression = %#v, want deterministic unknown", value)
	}
	if state.expressionBudget == nil || !state.expressionBudget.exhausted {
		t.Fatalf("deep parenthesized expression did not exhaust shared budget: %#v", state.expressionBudget)
	}
	if state.expressionBudget.depth != 0 {
		t.Fatalf("expression depth leaked after bounded evaluation: %d", state.expressionBudget.depth)
	}
	if state.expressionBudget.nodes > navigationVBExpressionNodeLimit || state.expressionBudget.work > navigationVBExpressionWorkLimit {
		t.Fatalf("expression budget exceeded its limits: %#v", state.expressionBudget)
	}
}

func TestNavigationVBConditionEffectsBudgetBoundsDeepParentheses(t *testing.T) {
	state := newNavigationVBState()
	evaluateNavigationVBConditionEffects(navigationVBDeepParenthesizedTokens(navigationVBExpressionDepthLimit*4), state)
	if state.expressionBudget == nil || !state.expressionBudget.exhausted || state.expressionBudget.depth != 0 {
		t.Fatalf("deep condition effects budget state = %#v, want exhausted with no depth leak", state.expressionBudget)
	}
}

func TestNavigationVBExpressionBudgetDepthBoundary(t *testing.T) {
	below := newNavigationVBState()
	value := evaluateNavigationVBExpression(navigationVBDeepParenthesizedTokens(navigationVBExpressionDepthLimit-1), below)
	if value.Kind != navigationVBValueLiteral || value.Text != "target.asp" {
		t.Fatalf("expression at depth limit = %#v, want target.asp", value)
	}
	if below.expressionBudget == nil || below.expressionBudget.exhausted {
		t.Fatalf("expression below depth limit exhausted budget: %#v", below.expressionBudget)
	}

	atLimit := newNavigationVBState()
	value = evaluateNavigationVBExpression(navigationVBDeepParenthesizedTokens(navigationVBExpressionDepthLimit), atLimit)
	if value.Kind != navigationVBValueUnknown || value.Text != "{unknown}" {
		t.Fatalf("expression over depth limit = %#v, want unknown", value)
	}
	if atLimit.expressionBudget == nil || !atLimit.expressionBudget.exhausted {
		t.Fatalf("expression over depth limit did not exhaust budget: %#v", atLimit.expressionBudget)
	}
}

func TestNavigationVBExpressionBudgetCancellationStopsBeforeEvaluation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	state := newNavigationVBState()
	state.cancelContext = ctx
	value := evaluateNavigationVBExpression(navigationVBDeepParenthesizedTokens(navigationVBExpressionDepthLimit*2), state)
	if value.Kind != navigationVBValueUnknown || value.Text != "{unknown}" {
		t.Fatalf("cancelled expression = %#v, want unknown", value)
	}
	if !state.cancelled {
		t.Fatal("cancelled expression did not record cancellation")
	}
	if state.expressionBudget == nil || state.expressionBudget.nodes != 0 || state.expressionBudget.work != 0 {
		t.Fatalf("cancelled expression consumed evaluation budget: %#v", state.expressionBudget)
	}
}

func TestNavigationVBExpressionBudgetUnknownFallbackIsDeterministic(t *testing.T) {
	tokens := navigationVBDeepParenthesizedTokens(navigationVBExpressionDepthLimit * 4)
	firstState := newNavigationVBState()
	secondState := newNavigationVBState()
	first := evaluateNavigationVBExpression(tokens, firstState)
	second := evaluateNavigationVBExpression(tokens, secondState)
	if first.Kind != second.Kind || first.Text != second.Text {
		t.Fatalf("deep expression fallback changed: %#v vs %#v", first, second)
	}
	if first.Kind != navigationVBValueUnknown || first.Text != "{unknown}" {
		t.Fatalf("deep expression fallback = %#v, want unknown", first)
	}
	if firstState.expressionBudget == nil || secondState.expressionBudget == nil || firstState.expressionBudget.nodes != secondState.expressionBudget.nodes || firstState.expressionBudget.work != secondState.expressionBudget.work {
		t.Fatalf("deep expression budget changed: %#v vs %#v", firstState.expressionBudget, secondState.expressionBudget)
	}
}

func TestNavigationVBExpressionBudgetIsSharedAcrossClonedStates(t *testing.T) {
	state := newNavigationVBState()
	clone := cloneNavigationVBState(state)
	if state.expressionBudget == nil || clone.expressionBudget != state.expressionBudget {
		t.Fatalf("cloned state received a separate expression budget: %#v vs %#v", state.expressionBudget, clone.expressionBudget)
	}
	_ = evaluateNavigationVBExpression(navigationVBDeepParenthesizedTokens(navigationVBExpressionDepthLimit*4), clone)
	if !state.expressionBudget.exhausted {
		t.Fatalf("parent state did not observe cloned-state budget exhaustion: %#v", state.expressionBudget)
	}
}

func navigationVBDeepParenthesizedTokens(depth int) []vbscript.Token {
	return vbscript.Tokenize(strings.Repeat("(", depth) + `"target.asp"` + strings.Repeat(")", depth))
}

func TestNavigationVBSingleLineIfDirectSinksStayOnReachableBranches(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   map[string]string
	}{
		{
			name: "redirect and transfer",
			source: `<%
If enabled Then Response.Redirect "then.asp" Else Server.Transfer "else.asp"
%>`,
			want: map[string]string{"redirect:then.asp": "Response.Redirect \"then.asp\"", "redirect:else.asp": "Server.Transfer \"else.asp\""},
		},
		{
			name: "permanent redirect and location header",
			source: `<%
If enabled Then Response.RedirectPermanent "then.asp" Else Response.AddHeader "Location", "else.asp"
%>`,
			want: map[string]string{"redirect:then.asp": "Response.RedirectPermanent \"then.asp\"", "redirect:else.asp": "Response.AddHeader \"Location\", \"else.asp\""},
		},
		{
			name: "generated html",
			source: `<%
If enabled Then Response.Write "<a href=""then.asp"">Then</a>" Else Response.Write "<a href=""else.asp"">Else</a>"
%>`,
			want: map[string]string{"write:<a href=\"then.asp\">Then</a>": "Response.Write \"<a href=\"\"then.asp\"\">Then</a>\"", "write:<a href=\"else.asp\">Else</a>": "Response.Write \"<a href=\"\"else.asp\"\">Else</a>\""},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidates := extractVBScriptNavigationCandidates(test.source[2:len(test.source)-2], 2, test.source)
			if len(candidates) != len(test.want) {
				t.Fatalf("single-line If candidates = %#v, want %d", candidates, len(test.want))
			}
			seen := make(map[string]bool, len(candidates))
			for _, candidate := range candidates {
				values := candidate.Value.finiteCandidates()
				if len(values) != 1 {
					t.Fatalf("single-line If candidate values = %#v", candidate)
				}
				key := candidate.Kind + ":" + values[0].Text
				wantSnippet, ok := test.want[key]
				if !ok {
					t.Fatalf("unexpected single-line If candidate = %#v", candidate)
				}
				if candidate.Snippet != wantSnippet {
					t.Fatalf("single-line If evidence = %q, want %q", candidate.Snippet, wantSnippet)
				}
				if seen[key] {
					t.Fatalf("single-line If duplicated candidate %q: %#v", key, candidates)
				}
				seen[key] = true
			}
		})
	}
}

func TestNavigationVBSingleLineIfColonBranchesTrackAssignmentsAndExit(t *testing.T) {
	const source = `<%
If enabled Then target = "then.asp" : Response.Redirect target : Exit Sub : Response.Redirect "unreachable-then.asp" Else target = "else.asp" : Response.Redirect target
%>`
	candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
	if len(candidates) != 2 {
		t.Fatalf("colon-separated inline If candidates = %#v, want two", candidates)
	}
	seen := map[string]bool{}
	for _, candidate := range candidates {
		values := candidate.Value.finiteCandidates()
		if len(values) != 1 {
			t.Fatalf("colon-separated inline If values = %#v", candidate)
		}
		if candidate.Kind != "redirect" || (values[0].Text != "then.asp" && values[0].Text != "else.asp") {
			t.Fatalf("colon-separated inline If candidate = %#v", candidate)
		}
		seen[values[0].Text] = true
		if strings.Contains(candidate.Snippet, "unreachable-then.asp") {
			t.Fatalf("colon-separated inline If retained sink after Exit: %#v", candidate)
		}
	}
	if !seen["then.asp"] || !seen["else.asp"] {
		t.Fatalf("colon-separated inline If values = %#v", candidates)
	}
}

func TestNavigationVBNestedInlineIfDirectSinksAndFalseSinks(t *testing.T) {
	const source = `<%
If outer Then If inner Then Response.Redirect "inner-then.asp" Else Response.Redirect "inner-else.asp" Else foo = Response.Redirect("false.asp")
%>`
	candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
	if len(candidates) != 2 {
		t.Fatalf("nested inline If candidates = %#v, want two", candidates)
	}
	for _, candidate := range candidates {
		if candidate.Kind != "redirect" || !strings.HasPrefix(candidate.Value.Text, "inner-") {
			t.Fatalf("nested inline If candidate = %#v", candidate)
		}
		if strings.Contains(candidate.Snippet, "false.asp") {
			t.Fatalf("nested inline If retained assignment expression sink: %#v", candidate)
		}
	}
}

func TestNavigationVBInlineElseIfAndElseDirectSinks(t *testing.T) {
	const source = `<%
If first Then
  Response.Redirect "first.asp"
ElseIf second Then Response.Redirect "elseif.asp"
Else Response.Write "<a href=""else.asp"">Else</a>"
End If
%>`
	candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
	if len(candidates) != 3 {
		t.Fatalf("inline ElseIf/Else candidates = %#v, want three", candidates)
	}
	want := map[string]string{
		"redirect:first.asp":                `Response.Redirect "first.asp"`,
		"redirect:elseif.asp":               `Response.Redirect "elseif.asp"`,
		`write:<a href="else.asp">Else</a>`: `Response.Write "<a href=""else.asp"">Else</a>"`,
	}
	for _, candidate := range candidates {
		values := candidate.Value.finiteCandidates()
		if len(values) != 1 {
			t.Fatalf("inline ElseIf/Else values = %#v", candidate)
		}
		key := candidate.Kind + ":" + values[0].Text
		if wantSnippet, ok := want[key]; !ok || candidate.Snippet != wantSnippet {
			t.Fatalf("inline ElseIf/Else candidate evidence = %#v, want %q", candidate, wantSnippet)
		}
	}
}

func TestNavigationVBInlineIfCalledSubAndFunctionSinksAreExactlyOnce(t *testing.T) {
	const source = `<%
Sub NavigateSub(target)
  Response.Redirect target
End Sub
Function NavigateFunction(target)
  Response.Write "<a href=""" & target & """>link</a>"
  NavigateFunction = target
End Function
If enabled Then Call NavigateSub("then.asp") Else Response.Redirect NavigateFunction("else.asp")
%>`
	candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
	if len(candidates) != 3 {
		t.Fatalf("inline If called Sub/Function sinks = %#v, want three", candidates)
	}
	counts := map[string]int{}
	for _, candidate := range candidates {
		for _, value := range candidate.Value.finiteCandidates() {
			counts[candidate.Kind+":"+value.Text]++
		}
	}
	if counts["redirect:then.asp"] != 1 || counts["write:<a href=\"else.asp\">link</a>"] != 1 || counts["redirect:else.asp"] != 1 {
		t.Fatalf("inline If called Sub/Function sink counts = %#v, candidates = %#v", counts, candidates)
	}
}

func TestNavigationVBByRefAndGlobalEffectsReplayRuntimeOrder(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   []string
	}{
		{
			name: "z/a aliases retain assignment order",
			source: `<%
Sub Mutate(ByRef z, ByRef a)
  z = "z.asp"
  a = "a.asp"
End Sub
target = "before.asp"
Call Mutate(target, target)
Response.Redirect target
%>`,
			want: []string{"a.asp"},
		},
		{
			name: "global then ByRef",
			source: `<%
Sub Mutate(ByRef target)
  globalTarget = "global.asp"
  target = "byref.asp"
End Sub
globalTarget = "before.asp"
Call Mutate(globalTarget)
Response.Redirect globalTarget
%>`,
			want: []string{"byref.asp"},
		},
		{
			name: "ByRef then global",
			source: `<%
Sub Mutate(ByRef target)
  target = "byref.asp"
  globalTarget = "global.asp"
End Sub
globalTarget = "before.asp"
Call Mutate(globalTarget)
Response.Redirect globalTarget
%>`,
			want: []string{"global.asp"},
		},
		{
			name: "three aliases",
			source: `<%
Sub Mutate(ByRef z, ByRef a, ByRef m)
  z = "z.asp"
  a = "a.asp"
  m = "m.asp"
End Sub
target = "before.asp"
Call Mutate(target, target, target)
Response.Redirect target
%>`,
			want: []string{"m.asp"},
		},
		{
			name: "nested call",
			source: `<%
Sub Inner(ByRef target)
  target = "inner.asp"
End Sub
Sub Outer(ByRef target)
  target = "outer-before.asp"
  Call Inner(target)
  target = "outer-after.asp"
End Sub
target = "before.asp"
Call Outer(target)
Response.Redirect target
%>`,
			want: []string{"outer-after.asp"},
		},
		{
			name: "branch union",
			source: `<%
Sub Mutate(ByRef target)
  If enabled Then
    target = "then.asp"
  Else
    target = "else.asp"
  End If
End Sub
target = "before.asp"
Call Mutate(target)
Response.Redirect target
%>`,
			want: []string{"then.asp", "else.asp"},
		},
		{
			name: "loop then final write",
			source: `<%
Sub Mutate(ByRef target)
  For index = 1 To 2
    target = "loop.asp"
  Next
  target = "after.asp"
End Sub
target = "before.asp"
Call Mutate(target)
Response.Redirect target
%>`,
			want: []string{"after.asp"},
		},
		{
			name: "bounded recursive call",
			source: `<%
Sub Mutate(ByRef target)
  target = "before-recursion.asp"
  If recurse Then Call Mutate(target)
  target = "after-recursion.asp"
End Sub
target = "before.asp"
Call Mutate(target)
Response.Redirect target
%>`,
			want: []string{"after-recursion.asp"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidates := extractVBScriptNavigationCandidates(test.source[2:len(test.source)-2], 2, test.source)
			if len(candidates) != 1 {
				t.Fatalf("ByRef order candidates = %#v", candidates)
			}
			values := candidates[0].Value.finiteCandidates()
			if len(values) != len(test.want) {
				t.Fatalf("ByRef order values = %#v, want %#v", values, test.want)
			}
			for _, want := range test.want {
				if !navigationVBValueTextsContain(values, want) {
					t.Fatalf("ByRef order value %q missing from %#v", want, values)
				}
			}
		})
	}
}

func TestNavigationVBNestedGlobalWriteDoesNotResolveThroughOuterByRefName(t *testing.T) {
	const source = `<%
Sub Inner(ByRef value)
	  x = "inner-global.asp"
End Sub
Sub Outer(ByRef x)
	  Call Inner(x)
	  x = "outer-byref.asp"
End Sub
target = "before.asp"
x = "x-before.asp"
Call Outer(target)
Response.Redirect target
Response.Redirect x
%>`
	candidates := extractVBScriptNavigationCandidates(source[2:len(source)-2], 2, source)
	if len(candidates) != 2 {
		t.Fatalf("nested global/byref candidates = %#v", candidates)
	}
	if candidates[0].Value.Text != "outer-byref.asp" || candidates[1].Value.Text != "inner-global.asp" {
		t.Fatalf("nested global/byref targets = %#v, want outer-byref.asp and inner-global.asp", candidates)
	}
}

func TestNavigationVBEffectPathMergeIsBoundedAndDeterministic(t *testing.T) {
	paths := make([]navigationVBEffectPath, navigationVBEffectPathLimit+8)
	for index := range paths {
		paths[index] = navigationVBEffectPath{effects: []navigationVBEffect{{target: fmt.Sprintf("target-%d", index), value: navigationVBValue{Kind: navigationVBValueLiteral, Text: fmt.Sprintf("value-%d", index)}}}}
	}
	first, firstTruncated := mergeNavigationVBEffectPaths(paths)
	second, secondTruncated := mergeNavigationVBEffectPaths(paths)
	if len(first) != navigationVBEffectPathLimit || !firstTruncated || len(second) != len(first) || !secondTruncated {
		t.Fatalf("effect path cap = %d/%d, truncated = %t/%t, want %d/true", len(first), len(second), firstTruncated, secondTruncated, navigationVBEffectPathLimit)
	}
	for index := range first {
		if navigationVBEffectPathKey(first[index]) != navigationVBEffectPathKey(second[index]) {
			t.Fatalf("effect path ordering changed at %d", index)
		}
	}
}

func navigationVBTestEffectPathSets33x2() []navigationVBEffectPathSet {
	sets := make([]navigationVBEffectPathSet, 33)
	for branch := range sets {
		sets[branch].paths = make([]navigationVBEffectPath, 2)
		for alternative := range sets[branch].paths {
			sets[branch].paths[alternative] = navigationVBEffectPath{effects: []navigationVBEffect{{
				target: "parameter",
				value:  navigationVBValue{Kind: navigationVBValueLiteral, Text: fmt.Sprintf("target-%02d-%d.asp", branch, alternative)},
				byRef:  true,
			}}}
		}
	}
	return sets
}

func TestNavigationVBEffectPathExact33x2OverflowRetainsMarker(t *testing.T) {
	sets := navigationVBTestEffectPathSets33x2()
	first, firstTruncated := mergeNavigationVBEffectPathSets(sets...)
	second, secondTruncated := mergeNavigationVBEffectPathSets(sets...)
	if len(first) != navigationVBEffectPathLimit || !firstTruncated || !first[len(first)-1].truncated {
		t.Fatalf("33x2 effect paths = %d, truncated = %t, last = %#v; want 64/true/marked", len(first), firstTruncated, first[len(first)-1])
	}
	if len(second) != len(first) || !secondTruncated || !second[len(second)-1].truncated {
		t.Fatalf("repeated 33x2 effect paths = %d, truncated = %t, last = %#v", len(second), secondTruncated, second[len(second)-1])
	}
	for index := range first {
		if navigationVBEffectPathKey(first[index]) != navigationVBEffectPathKey(second[index]) {
			t.Fatalf("33x2 effect path ordering changed at %d", index)
		}
	}
}

func TestNavigationVBBranchFrameOverflowRetainsConservativeAliasValues(t *testing.T) {
	sets := navigationVBTestEffectPathSets33x2()
	frame := navigationVBBranchFrame{effectBranches: make([][]navigationVBEffectPath, 0, len(sets)), effectBranchTruncated: make([]bool, 0, len(sets))}
	for _, set := range sets {
		frame.effectBranches = append(frame.effectBranches, set.paths)
		frame.effectBranchTruncated = append(frame.effectBranchTruncated, set.truncated)
	}
	branchSets := make([]navigationVBEffectPathSet, 0, len(frame.effectBranches))
	for index, paths := range frame.effectBranches {
		branchSets = append(branchSets, navigationVBEffectPathSet{paths: paths, truncated: frame.effectBranchTruncated[index]})
	}
	merged, truncated := mergeNavigationVBEffectPathSets(branchSets...)
	if len(merged) != navigationVBEffectPathLimit || !truncated || !merged[len(merged)-1].truncated {
		t.Fatalf("branch-frame overflow = %d, truncated = %t, last = %#v", len(merged), truncated, merged[len(merged)-1])
	}

	caller := newNavigationVBState()
	caller.variables["target"] = navigationVBValue{Kind: navigationVBValueLiteral, Text: "before.asp"}
	caller.globals["target"] = caller.variables["target"]
	callee := newNavigationVBState()
	callee.localScope = true
	callee.localNames["parameter"] = struct{}{}
	callee.references["parameter"] = "target"
	callee.variables["parameter"] = navigationVBValue{Kind: navigationVBValueLiteral, Text: "target-00-0.asp"}
	callee.effectPaths = merged
	callee.effectPathsTruncated = truncated
	navigationVBMergeCallEffects(caller, callee)
	values := caller.variables["target"].finiteCandidates()
	if !navigationVBValueTextsContain(values, "target-00-0.asp") {
		t.Fatalf("branch-frame valid target missing from %#v", values)
	}
	if !navigationVBValueTextsContain(values, "{unknown}") {
		t.Fatalf("branch-frame unknown fallback missing from %#v", values)
	}
	if !caller.effectPathsTruncated || len(caller.effectPaths) != navigationVBEffectPathLimit || !caller.effectPaths[len(caller.effectPaths)-1].truncated {
		t.Fatalf("branch-frame marker lost after replay: paths=%d truncated=%t last=%#v", len(caller.effectPaths), caller.effectPathsTruncated, caller.effectPaths[len(caller.effectPaths)-1])
	}
}

func TestNavigationVBNestedEffectPathOverflowPropagatesDeterministically(t *testing.T) {
	sets := navigationVBTestEffectPathSets33x2()
	innerPaths, truncated := mergeNavigationVBEffectPathSets(sets...)
	makeCaller := func() *navigationVBState {
		caller := newNavigationVBState()
		caller.variables["target"] = navigationVBValue{Kind: navigationVBValueLiteral, Text: "before.asp"}
		caller.globals["target"] = caller.variables["target"]
		return caller
	}
	makeInner := func() *navigationVBState {
		inner := newNavigationVBState()
		inner.localScope = true
		inner.localNames["inner"] = struct{}{}
		inner.references["inner"] = "parameter"
		inner.effectPaths = cloneNavigationVBEffectPaths(innerPaths)
		inner.effectPathsTruncated = truncated
		return inner
	}
	makeOuter := func() *navigationVBState {
		outer := newNavigationVBState()
		outer.localScope = true
		outer.localNames["parameter"] = struct{}{}
		outer.references["parameter"] = "target"
		return outer
	}

	firstOuter := makeOuter()
	navigationVBMergeCallEffects(firstOuter, makeInner())
	if !firstOuter.effectPathsTruncated || len(firstOuter.effectPaths) != navigationVBEffectPathLimit || !firstOuter.effectPaths[len(firstOuter.effectPaths)-1].truncated {
		t.Fatalf("nested inner overflow marker lost: paths=%d truncated=%t last=%#v", len(firstOuter.effectPaths), firstOuter.effectPathsTruncated, firstOuter.effectPaths[len(firstOuter.effectPaths)-1])
	}
	firstCaller := makeCaller()
	navigationVBMergeCallEffects(firstCaller, firstOuter)
	firstValues := firstCaller.variables["target"].finiteCandidates()
	if !navigationVBValueTextsContain(firstValues, "target-00-0.asp") || !navigationVBValueTextsContain(firstValues, "{unknown}") {
		t.Fatalf("nested overflow values = %#v, want valid target and unknown", firstValues)
	}

	secondOuter := makeOuter()
	navigationVBMergeCallEffects(secondOuter, makeInner())
	secondCaller := makeCaller()
	navigationVBMergeCallEffects(secondCaller, secondOuter)
	secondValues := secondCaller.variables["target"].finiteCandidates()
	if len(firstValues) != len(secondValues) {
		t.Fatalf("nested overflow value count changed: %d vs %d", len(firstValues), len(secondValues))
	}
	for index := range firstValues {
		if firstValues[index].Kind != secondValues[index].Kind || firstValues[index].Text != secondValues[index].Text {
			t.Fatalf("nested overflow value ordering changed at %d: %#v vs %#v", index, firstValues, secondValues)
		}
	}
}

func mustMarshalNavigationPayload(builder *navigationGraphBuilder, parsed *core.ParsedDocument) []byte {
	builder.addDocument(parsed, parsed.URI)
	payload, _ := json.Marshal(builder.payload(1))
	return payload
}
