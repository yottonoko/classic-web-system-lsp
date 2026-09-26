package lspserver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

func TestNavigationIncludeExecutionBoundsSharedFanoutAndCancellation(t *testing.T) {
	const depth = 14
	documents := make(map[string]*core.ParsedDocument, depth+1)
	relations := make(map[string][]navigationVBIncludeRelation, depth+1)
	for index := depth; index >= 0; index-- {
		uri := fmt.Sprintf("file:///tmp/navigation-fanout-%d.asp", index)
		text := fmt.Sprintf("<%% value = %d %%>", index)
		parsed := core.ParseDocument(uri, text, core.Settings{})
		documents[workspacepkg.FileIdentityKeyFromURI(uri)] = parsed
		if index == 0 {
			continue
		}
		childURI := fmt.Sprintf("file:///tmp/navigation-fanout-%d.asp", index-1)
		parentKey := workspacepkg.FileIdentityKeyFromURI(uri)
		childKey := workspacepkg.FileIdentityKeyFromURI(childURI)
		relations[parentKey] = []navigationVBIncludeRelation{
			{ParentURI: uri, ChildURI: childURI, ChildKey: childKey, Offset: 0, Index: 0},
			{ParentURI: uri, ChildURI: childURI, ChildKey: childKey, Offset: 0, Index: 1},
		}
	}
	root := documents[workspacepkg.FileIdentityKeyFromURI("file:///tmp/navigation-fanout-14.asp")]
	program, err := navigationVBExecutionProgramContext(context.Background(), root, root.URI, documents, relations)
	if err != nil {
		t.Fatalf("fanout execution preparation failed: %v", err)
	}
	if len(program) > includeExpansionUnitBudget+1 {
		t.Fatalf("fanout navigation materialized %d units, budget is %d", len(program), includeExpansionUnitBudget)
	}
	truncated := false
	for _, unit := range program {
		truncated = truncated || unit.truncated
	}
	if !truncated {
		t.Fatalf("fanout navigation did not publish explicit truncation marker")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	program, err = navigationVBExecutionProgramContext(cancelled, root, root.URI, documents, relations)
	if err == nil || len(program) != 0 {
		t.Fatalf("cancelled navigation preparation = (%d units, %v), want no publication", len(program), err)
	}
}

func TestNavigationIncludeTruncationDiscardsPrefixAcrossVBHTMLAndJavaScript(t *testing.T) {
	const depth = 14
	build := func(suffix string) map[string]any {
		documents := make([]*core.ParsedDocument, 0, depth+1)
		byKey := make(map[string]*core.ParsedDocument, depth+1)
		relations := make(map[string][]navigationVBIncludeRelation, depth+1)
		for level := 0; level <= depth; level++ {
			uri := fmt.Sprintf("file:///tmp/navigation-prefix-%d.asp", level)
			target := fmt.Sprintf("level-%d-%s.asp", level, suffix)
			text := fmt.Sprintf(`<%% Response.Redirect "%s" %%>
<a href="%s">level</a>
<script>location.href = %q</script>`, target, target, target)
			if level > 0 {
				childURI := fmt.Sprintf("file:///tmp/navigation-prefix-%d.asp", level-1)
				text += fmt.Sprintf(`
<!-- #include file="%s" -->
<!-- #include file="%s" -->`, filepath.Base(fileURIPath(childURI)), filepath.Base(fileURIPath(childURI)))
			}
			parsed := core.ParseDocument(uri, text, core.Settings{})
			documents = append(documents, parsed)
			byKey[workspacepkg.FileIdentityKeyFromURI(uri)] = parsed
			if level > 0 {
				childURI := fmt.Sprintf("file:///tmp/navigation-prefix-%d.asp", level-1)
				parentKey := workspacepkg.FileIdentityKeyFromURI(uri)
				childKey := workspacepkg.FileIdentityKeyFromURI(childURI)
				relations[parentKey] = []navigationVBIncludeRelation{
					{ParentURI: uri, ChildURI: childURI, ChildKey: childKey, Offset: len(text), Index: 0},
					{ParentURI: uri, ChildURI: childURI, ChildKey: childKey, Offset: len(text), Index: 1},
				}
			}
		}
		root := byKey[workspacepkg.FileIdentityKeyFromURI(fmt.Sprintf("file:///tmp/navigation-prefix-%d.asp", depth))]
		builder := newNavigationGraphBuilder("document", root.URI, nil)
		if err := builder.prepareVBScriptFunctionsWithIncludesContext(context.Background(), documents, nil, relations); err != nil {
			t.Fatalf("prepare adversarial include graph = %v", err)
		}
		if !builder.vbIncludeExpansionTruncated {
			t.Fatal("adversarial double fanout did not mark include expansion truncated")
		}
		builder.addDocument(root, root.URI)
		payload := builder.payload(len(documents))
		if truncated, ok := payload["includeExpansionTruncated"].(bool); !ok || !truncated {
			t.Fatalf("truncation marker = %#v, want true", payload["includeExpansionTruncated"])
		}
		if edges, ok := payload["edges"].([]map[string]any); !ok || len(edges) != 0 {
			t.Fatalf("truncated graph edges = %#v, want none", payload["edges"])
		}
		nodes, ok := payload["nodes"].([]map[string]any)
		if !ok || len(nodes) != 1 {
			t.Fatalf("truncated graph nodes = %#v, want only root metadata", payload["nodes"])
		}
		nodeURI, _ := nodes[0]["uri"].(string)
		if !workspacepkg.SameFileIdentityURI(nodeURI, root.URI) || nodes[0]["isRoot"] != true {
			t.Fatalf("truncated graph nodes = %#v, want only root metadata", payload["nodes"])
		}
		return payload
	}

	first, err := json.Marshal(build("first"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := json.Marshal(build("second"))
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("truncated payload depends on arbitrary prefix:\n%s\n%s", first, second)
	}
}

func TestNavigationIncludeBelowBudgetPreservesConcreteTargets(t *testing.T) {
	rootURI := "file:///tmp/navigation-below-budget-root.asp"
	childURI := "file:///tmp/navigation-below-budget-child.inc"
	root := core.ParseDocument(rootURI, `<% Response.Redirect "root-target.asp" %>
<!-- #include file="navigation-below-budget-child.inc" -->`, core.Settings{})
	child := core.ParseDocument(childURI, `<a href="child-target.asp">child</a>
<script>location.href = "script-target.asp"</script>`, core.Settings{})
	relations := map[string][]navigationVBIncludeRelation{
		workspacepkg.FileIdentityKeyFromURI(rootURI): {
			{ParentURI: rootURI, ChildURI: childURI, ChildKey: workspacepkg.FileIdentityKeyFromURI(childURI), Offset: strings.Index(root.Text, "<!--"), Index: 0},
		},
	}
	builder := newNavigationGraphBuilder("document", rootURI, nil)
	if err := builder.prepareVBScriptFunctionsWithIncludesContext(context.Background(), []*core.ParsedDocument{root, child}, nil, relations); err != nil {
		t.Fatalf("prepare below-budget include graph = %v", err)
	}
	if builder.vbIncludeExpansionTruncated {
		t.Fatal("below-budget include graph unexpectedly marked truncated")
	}
	builder.addDocument(root, rootURI)
	payload := builder.payload(2)
	if _, truncated := payload["includeExpansionTruncated"]; truncated {
		t.Fatalf("below-budget payload has truncation marker: %#v", payload)
	}
	if edges, ok := payload["edges"].([]map[string]any); !ok || len(edges) < 3 {
		t.Fatalf("below-budget concrete edges = %#v, want VB, HTML, and JavaScript targets", payload["edges"])
	}
}

func TestNavigationHTMLRepeatedIncludePreservesOccurrenceStateAndEvidence(t *testing.T) {
	root := t.TempDir()
	pageURI := filePathURI(filepath.Join(root, "first.asp"))
	includeURI := filePathURI(filepath.Join(root, "shared.inc"))
	for _, target := range []string{"first.asp", "second.asp"} {
		if err := os.WriteFile(filepath.Join(root, target), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	page := core.ParseDocument(pageURI, `<%
target = "first.asp"
%>
<!-- #include file="shared.inc" -->
<%
target = "second.asp"
%>
<!-- #include file="shared.inc" -->`, core.Settings{})
	include := core.ParseDocument(includeURI, `<a href="<%= target %>">Shared</a>`, core.Settings{})
	owners := map[string][]string{workspacepkg.FileIdentityKeyFromURI(includeURI): {pageURI}}

	build := func() *navigationGraphBuilder {
		builder := newNavigationGraphBuilder("document", pageURI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
		builder.prepareVBScriptFunctions([]*core.ParsedDocument{page, include}, owners)
		builder.addDocument(page, pageURI)
		if builder.navigationError != nil {
			t.Fatalf("repeated include navigation error = %v", builder.navigationError)
		}
		return builder
	}
	builder := build()
	if len(builder.edges) != 2 {
		t.Fatalf("repeated include edges = %#v, want one edge per target occurrence", builder.edges)
	}
	wantRanges := make([]lsp.Range, 0, 2)
	for _, target := range []string{"first.asp", "second.asp"} {
		var edge map[string]any
		for _, candidate := range builder.edges {
			if navigationHTMLTestEdgeTarget(builder, candidate) == target {
				edge = candidate
				break
			}
		}
		if edge == nil {
			t.Fatalf("repeated include target %q missing: %#v", target, builder.edges)
		}
		if edge["count"] != 1 {
			t.Fatalf("repeated include target %q count = %#v, want one occurrence", target, edge["count"])
		}
		ranges, _ := edge["ranges"].([]lsp.Range)
		if len(ranges) != 1 {
			t.Fatalf("repeated include target %q ranges = %#v, want one source range", target, edge["ranges"])
		}
		wantRanges = append(wantRanges, ranges[0])
		evidence, _ := edge["evidence"].([]map[string]any)
		if len(evidence) != 1 || evidence[0]["uri"] != includeURI {
			t.Fatalf("repeated include target %q evidence = %#v, want shared.inc evidence", target, edge["evidence"])
		}
	}
	if wantRanges[0] != wantRanges[1] || wantRanges[0] == (lsp.Range{}) {
		t.Fatalf("repeated include expression ranges = %#v, want the same non-zero source range", wantRanges)
	}

	firstPayload, err := json.Marshal(builder.payload(2))
	if err != nil {
		t.Fatal(err)
	}
	secondBuilder := build()
	secondPayload, err := json.Marshal(secondBuilder.payload(2))
	if err != nil {
		t.Fatal(err)
	}
	if string(firstPayload) != string(secondPayload) {
		t.Fatalf("repeated include payload is nondeterministic:\n%s\n%s", firstPayload, secondPayload)
	}
}

func TestNavigationHTMLRepeatedIncludeAcrossFirstAndSecondOwnersIsDeterministic(t *testing.T) {
	root := t.TempDir()
	firstURI := filePathURI(filepath.Join(root, "first.asp"))
	secondURI := filePathURI(filepath.Join(root, "second.asp"))
	includeURI := filePathURI(filepath.Join(root, "shared.inc"))
	for _, target := range []string{"first-target.asp", "second-target.asp"} {
		if err := os.WriteFile(filepath.Join(root, target), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	first := core.ParseDocument(firstURI, `<% target = "first-target.asp" %>
<!-- #include file="shared.inc" -->`, core.Settings{})
	second := core.ParseDocument(secondURI, `<% target = "second-target.asp" %>
<!-- #include file="shared.inc" -->`, core.Settings{})
	include := core.ParseDocument(includeURI, `<a href="<%= target %>">Shared</a>`, core.Settings{})
	documents := []*core.ParsedDocument{first, second, include}
	owners := map[string][]string{workspacepkg.FileIdentityKeyFromURI(includeURI): {firstURI, secondURI}}

	build := func() *navigationGraphBuilder {
		builder := newNavigationGraphBuilder("workspace", firstURI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
		builder.prepareVBScriptFunctions(documents, owners)
		builder.addDocument(first, firstURI)
		builder.addDocument(second, secondURI)
		if builder.navigationError != nil {
			t.Fatalf("cross-owner repeated include navigation error = %v", builder.navigationError)
		}
		return builder
	}
	builder := build()
	if len(builder.edges) != 2 {
		t.Fatalf("cross-owner repeated include edges = %#v, want one edge per owner occurrence", builder.edges)
	}
	for _, target := range []string{"first-target.asp", "second-target.asp"} {
		found := false
		for _, edge := range builder.edges {
			if navigationHTMLTestEdgeTarget(builder, edge) == target && edge["count"] == 1 {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("cross-owner repeated include target %q missing: %#v", target, builder.edges)
		}
	}
	firstPayload, err := json.Marshal(builder.payload(3))
	if err != nil {
		t.Fatal(err)
	}
	secondPayload, err := json.Marshal(build().payload(3))
	if err != nil {
		t.Fatal(err)
	}
	if string(firstPayload) != string(secondPayload) {
		t.Fatalf("cross-owner repeated include payload is nondeterministic:\n%s\n%s", firstPayload, secondPayload)
	}
}
