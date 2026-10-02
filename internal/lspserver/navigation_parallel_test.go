package lspserver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

func TestNavigationPrecomputedProgramsMatchSequentialExecution(t *testing.T) {
	root := t.TempDir()
	shared := core.ParseDocument(filePathURI(filepath.Join(root, "shared.inc")), `<%
Function NextPage(value)
  NextPage = value & ".asp"
End Function
If Request("stop") = "1" Then Response.Redirect NextPage(sharedTarget)
%>
<a href="<%= sharedTarget %>">shared</a>
<button onclick="location.href='help.asp'">help</button>
`, core.Settings{})
	sharedKey := workspacepkg.FileIdentityKeyFromURI(shared.URI)
	documents := []*core.ParsedDocument{shared}
	relations := map[string][]navigationVBIncludeRelation{}
	owners := map[string][]string{}
	for index := range 12 {
		source := fmt.Sprintf(`<%% sharedTarget = "page%d.asp" %%>
<!-- #include file="shared.inc" -->
<%% If Request("next") = "1" Then Response.Redirect "page%d.asp" %%>
<form action="page%d.asp" method="post"><input type="hidden" name="id" value="<%%= sharedTarget %%>"></form>
`, (index+1)%12, (index+2)%12, (index+3)%12)
		page := core.ParseDocument(filePathURI(filepath.Join(root, fmt.Sprintf("page%d.asp", index))), source, core.Settings{})
		documents = append(documents, page)
		relations[workspacepkg.FileIdentityKeyFromURI(page.URI)] = []navigationVBIncludeRelation{{
			ParentURI: page.URI, ChildURI: shared.URI, ChildKey: sharedKey, Offset: strings.Index(source, "<!--"),
		}}
		owners[sharedKey] = append(owners[sharedKey], page.URI)
	}
	build := func(precompute bool) (map[string]any, *navigationGraphBuilder) {
		t.Helper()
		ctx := context.Background()
		builder := newNavigationGraphBuilder("workspace", "", []workspaceRoot{{URI: filePathURI(root), Path: root}})
		builder.cancelContext = ctx
		builder.javascriptCandidates = typeScriptGoNavigationCandidates{ctx: ctx}
		if err := builder.prepareVBScriptFunctionsWithIncludesContext(ctx, documents, owners, relations); err != nil {
			t.Fatal(err)
		}
		if precompute {
			builder.precomputeVBScriptNavigationPrograms(func(count int, fn func(int)) {
				var wait sync.WaitGroup
				for index := range count {
					wait.Go(func() { fn(index) })
				}
				wait.Wait()
			})
			if len(builder.vbProgramRuns) == 0 {
				t.Fatal("no programs were precomputed")
			}
		}
		for _, parsed := range documents {
			documentOwners := []string{parsed.URI}
			if parsed == shared {
				documentOwners = owners[sharedKey]
			}
			for _, ownerURI := range documentOwners {
				builder.addDocument(parsed, ownerURI)
				if builder.navigationError != nil {
					t.Fatal(builder.navigationError)
				}
			}
		}
		return builder.payload(len(documents)), builder
	}
	sequential, _ := build(false)
	parallel, builder := build(true)
	if len(builder.vbProgramRuns) != 0 {
		t.Fatalf("%d precomputed programs were never emitted", len(builder.vbProgramRuns))
	}
	if _, rendered := builder.vbHTMLProgramsRendered[sharedKey]; !rendered {
		t.Fatal("the shared include's programs were not marked rendered")
	}
	sequentialJSON, _ := json.Marshal(sequential)
	parallelJSON, _ := json.Marshal(parallel)
	if string(sequentialJSON) != string(parallelJSON) {
		t.Fatalf("parallel navigation payload differs:\nsequential %s\nparallel   %s", sequentialJSON, parallelJSON)
	}
	edges, _ := parallel["edges"].([]map[string]any)
	if len(edges) < 36 {
		t.Fatalf("navigation edges = %d, want redirects, forms, and shared links for every page", len(edges))
	}
}

func TestNavigationVBPreparedStatementsAreSharedAndEquivalent(t *testing.T) {
	content := `
Class Helper
  Function Inside()
  End Function
End Class
Public Function BuildTarget(name, ByVal suffix)
  BuildTarget = name & suffix
End Function
Sub Go()
  Response.Redirect BuildTarget("a", ".asp")
End Sub
Go
`
	var cache navigationVBPreparedCache
	first := cache.statements(content)
	second := cache.statements(content)
	if len(first) == 0 || &first[0] != &second[0] {
		t.Fatal("prepared statements were not shared for the same content")
	}
	uncached := (*navigationVBPreparedCache)(nil).statements(content)
	if !reflect.DeepEqual(first, uncached) {
		t.Fatal("cached and uncached prepared statements differ")
	}
	parsed := core.ParseDocument("file:///prepared.asp", "<%"+content+"%>", core.Settings{})
	functions := navigationVBFunctionDefinitionsPrepared(parsed, &cache)
	if _, ok := functions["inside"]; ok {
		t.Fatalf("class member was indexed as a global function: %#v", functions)
	}
	build, ok := functions["buildtarget"]
	if !ok || !build.IsFunction || len(build.Parameters) != 2 || len(build.Body) != 1 {
		t.Fatalf("BuildTarget = %#v, want a function with two parameters and one body statement", build)
	}
	if goSub, ok := functions["go"]; !ok || goSub.IsFunction || len(goSub.Body) != 1 {
		t.Fatalf("Go = %#v, want a sub with one body statement", goSub)
	}
	candidates := extractVBScriptNavigationCandidates(content, 0, content)
	state := newNavigationVBState()
	state.prepared = &cache
	shared := extractVBScriptNavigationCandidatesWithState(content, 0, content, state)
	if !reflect.DeepEqual(candidates, shared) || len(shared) != 1 || shared[0].Value.Text != "a.asp" {
		t.Fatalf("prepared candidates = %#v, want %#v with target a.asp", shared, candidates)
	}
}

func TestNavigationTargetFilesReuseChecksWithinBuild(t *testing.T) {
	root := t.TempDir()
	owner := filepath.Join(root, "owner.asp")
	target := filepath.Join(root, "target.asp")
	for _, path := range []string{owner, target} {
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	roots := []workspaceRoot{{URI: filePathURI(root), Path: root}}
	trusted, complete := trustedPathRootsContext(context.Background(), []string{root})
	if !complete {
		t.Fatal("trusted roots were incomplete")
	}
	var files navigationTargetFiles
	resolve := func(files *navigationTargetFiles) bool {
		t.Helper()
		value, ok := navigationTargetWithPreparedRoots(context.Background(), filePathURI(owner), "target.asp?id=1", "htmlLink", roots, trusted, true, files, false)
		if !ok {
			t.Fatal("target did not resolve")
		}
		return value["exists"] == true
	}
	if !resolve(&files) {
		t.Fatal("existing target was reported missing")
	}
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if !resolve(&files) {
		t.Fatal("a build re-checked a target it had already checked")
	}
	if resolve(nil) {
		t.Fatal("a removed target was reported to exist without a build cache")
	}
	if _, ok := navigationTargetWithPreparedRoots(context.Background(), filePathURI(owner), "../outside.asp", "htmlLink", roots, trusted, true, &files, false); ok {
		t.Fatal("a target outside the workspace resolved")
	}
}
