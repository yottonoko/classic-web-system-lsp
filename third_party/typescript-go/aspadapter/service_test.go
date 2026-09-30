package aspadapter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestProjectReturnsHierarchicalDocumentSymbols(t *testing.T) {
	project := NewProject("/project")
	if _, err := project.UpdateFiles(map[string]string{
		"/project/symbols.js": "class Widget { render() {} }\n",
	}); err != nil {
		t.Fatal(err)
	}
	raw, err := project.Request(context.Background(), "textDocument/documentSymbol", []byte(`{"textDocument":{"uri":"file:///project/symbols.js"}}`))
	if err != nil {
		t.Fatal(err)
	}
	var symbols []struct {
		Name  string `json:"name"`
		Range struct {
			End struct {
				Character int `json:"character"`
			} `json:"end"`
		} `json:"range"`
		SelectionRange struct {
			End struct {
				Character int `json:"character"`
			} `json:"end"`
		} `json:"selectionRange"`
		Children []struct {
			Name string `json:"name"`
		} `json:"children"`
	}
	if err := json.Unmarshal(raw, &symbols); err != nil {
		t.Fatalf("document symbols are not hierarchical: %v: %s", err, raw)
	}
	if len(symbols) != 1 || symbols[0].Name != "Widget" || symbols[0].Range.End.Character == 0 || symbols[0].SelectionRange.End.Character == 0 || len(symbols[0].Children) != 1 || symbols[0].Children[0].Name != "render" {
		t.Fatalf("hierarchical document symbols = %s", raw)
	}
}

func TestProjectServesConcurrentDiagnosticsFromOneGeneration(t *testing.T) {
	project := NewProject("/project")
	files := make(map[string]string, 8)
	for index := range 8 {
		files[fmt.Sprintf("/project/file-%d.js", index)] = "const value = missingName;\n"
	}
	if _, err := project.Update(files, ProjectOptions{CompilerOptions: map[string]any{"checkJs": true}}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errors := make(chan error, len(files))
	for index := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			params := []byte(fmt.Sprintf(`{"textDocument":{"uri":"file:///project/file-%d.js"}}`, index))
			raw, err := project.Request(context.Background(), "textDocument/diagnostic", params)
			if err != nil {
				errors <- err
				return
			}
			if !bytes.Contains(raw, []byte("missingName")) {
				errors <- fmt.Errorf("diagnostic response %d did not contain missingName: %s", index, raw)
			}
		}()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
}

func TestProjectRequestLockWaitHonorsCancellation(t *testing.T) {
	project := NewProject("/project")
	project.mu.Lock()
	defer project.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := project.Request(ctx, "textDocument/diagnostic", []byte(`{"textDocument":{"uri":"file:///project/file.js"}}`)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancelled project request error = %v", err)
	}
}

func TestProjectRoutesJavaScriptLanguageFeaturesAcrossModuleFiles(t *testing.T) {
	project := NewProject("/project")
	models := `export class BaseWidget {
  render(value) { return value; }
}
export class DashboardWidget extends BaseWidget {
  render(value) { return super.render(value); }
}
export function formatWidget(widget, prefix) { return prefix + widget.render(prefix); }
`
	main := `import * as models from "./models.js";
const widget = new models.DashboardWidget();
const output = models.formatWidget(widget, "x");
widget.render(output);
`
	if _, err := project.UpdateFiles(map[string]string{
		"/project/models.js": models,
		"/project/main.js":   main,
	}); err != nil {
		t.Fatal(err)
	}
	position := func(text string, offset int) map[string]int {
		line := strings.Count(text[:offset], "\n")
		lineStart := strings.LastIndex(text[:offset], "\n") + 1
		return map[string]int{"line": line, "character": offset - lineStart}
	}
	request := func(method, uri string, positionValue map[string]int, extra map[string]any) string {
		t.Helper()
		params := map[string]any{"textDocument": map[string]any{"uri": uri}}
		if positionValue != nil {
			params["position"] = positionValue
		}
		for key, value := range extra {
			params[key] = value
		}
		body, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := project.Request(context.Background(), method, body)
		if err != nil {
			t.Fatalf("%s failed: %v", method, err)
		}
		return string(raw)
	}
	mainURI := "file:///project/main.js"
	modelsURI := "file:///project/models.js"
	dashboardPosition := position(main, strings.Index(main, "DashboardWidget"))
	formatCallPosition := position(main, strings.LastIndex(main, "formatWidget")+len("formatWidget("))
	widgetUsePosition := position(main, strings.LastIndex(main, "widget.render"))
	renderMemberPosition := position(main, strings.LastIndex(main, "widget.render")+len("widget."))
	baseRenderPosition := position(models, strings.Index(models, "render"))

	checks := []struct {
		method   string
		uri      string
		position map[string]int
		extra    map[string]any
		contains []string
	}{
		{"textDocument/completion", mainURI, renderMemberPosition, nil, []string{"render"}},
		{"textDocument/hover", mainURI, dashboardPosition, nil, []string{"DashboardWidget"}},
		{"textDocument/signatureHelp", mainURI, formatCallPosition, nil, []string{"formatWidget", "prefix"}},
		{"textDocument/definition", mainURI, dashboardPosition, nil, []string{"models.js"}},
		{"textDocument/typeDefinition", mainURI, widgetUsePosition, nil, []string{"models.js"}},
		{"textDocument/implementation", modelsURI, baseRenderPosition, nil, []string{"models.js"}},
		{"textDocument/references", mainURI, dashboardPosition, map[string]any{"context": map[string]any{"includeDeclaration": true}}, []string{"models.js", "main.js"}},
		{"textDocument/prepareRename", mainURI, dashboardPosition, nil, []string{"start", "end"}},
		{"textDocument/rename", mainURI, dashboardPosition, map[string]any{"newName": "RenamedWidget"}, []string{"models.js", "main.js", "RenamedWidget"}},
		{"textDocument/documentSymbol", mainURI, nil, nil, []string{"widget", "output"}},
		{"textDocument/documentHighlight", mainURI, widgetUsePosition, nil, []string{"range"}},
	}
	for _, check := range checks {
		t.Run(strings.TrimPrefix(check.method, "textDocument/"), func(t *testing.T) {
			result := request(check.method, check.uri, check.position, check.extra)
			for _, expected := range check.contains {
				if !strings.Contains(result, expected) {
					t.Fatalf("%s missing %q: %s", check.method, expected, result)
				}
			}
		})
	}
}

func TestProjectUsesRealMemberAndCrossFileCompletions(t *testing.T) {
	project := NewProject("/project")
	files := map[string]string{
		"/project/one.js": "const sharedValue = { alphaMember: 1 };\n",
		"/project/two.js": "sharedValue.alp\n",
	}
	if _, err := project.UpdateFiles(files); err != nil {
		t.Fatal(err)
	}
	raw, err := project.Request(context.Background(), "textDocument/completion", []byte(`{"textDocument":{"uri":"file:///project/two.js"},"position":{"line":0,"character":15}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "alphaMember") {
		t.Fatalf("member completion missing compiler-inferred property: %s", raw)
	}
}

func TestProjectCompletesLocalPrefix(t *testing.T) {
	project := NewProject("/project")
	if _, err := project.UpdateFiles(map[string]string{"/project/one.js": "\nconst alphaValue = 1;\nalph\n"}); err != nil {
		t.Fatal(err)
	}
	raw, err := project.Request(context.Background(), "textDocument/completion", []byte(`{"textDocument":{"uri":"file:///project/one.js"},"position":{"line":2,"character":4}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "alphaValue") {
		t.Fatalf("local completion missing: %s", raw)
	}
}

func TestProjectSupportsUNCPaths(t *testing.T) {
	project := NewProject(`\\server\share\project`)
	if _, err := project.UpdateFiles(map[string]string{
		`\\server\share\project\main.js`: "const networkValue = 1;\nnetworkVal\n",
	}); err != nil {
		t.Fatal(err)
	}
	raw, err := project.Request(context.Background(), "textDocument/completion", []byte(`{"textDocument":{"uri":"file://server/share/project/main.js"},"position":{"line":1,"character":10}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "networkValue") {
		t.Fatalf("UNC project completion missing: %s", raw)
	}
}

func TestProjectNormalizesMixedWindowsPathFamiliesBeforeRebuild(t *testing.T) {
	project := NewProject("/project")
	project.root = "C:/project"
	project.files = map[string]string{
		"C:/project/main.js":     "const mainValue = 1;\n",
		"/__asp_lsp/embedded.js": "mainValue;\n",
	}
	if _, err := project.rebuildLocked("", false); err != nil {
		t.Fatal(err)
	}
	if _, ok := project.files["c:/project/__asp_lsp/embedded.js"]; !ok {
		t.Fatalf("normalized project files = %#v", project.files)
	}
}

func TestNormalizeProjectPathForRootKeepsOneWindowsPathFamily(t *testing.T) {
	tests := []struct {
		name string
		root string
		path string
		want string
	}{
		{name: "posix path under drive root", root: "C:/project", path: "/__asp_lsp/embedded.js", want: "c:/project/__asp_lsp/embedded.js"},
		{name: "UNC path under drive root", root: "C:/project", path: "//server/share/embedded.js", want: "c:/project/server/share/embedded.js"},
		{name: "drive path under UNC root", root: "//server/share/project", path: "C:/embedded.js", want: "//server/share/project/embedded.js"},
		{name: "extended drive path", root: "C:/project", path: "//?/C:/project/main.js", want: "c:/project/main.js"},
		{name: "POSIX project", root: "/project", path: "/project/main.js", want: "/project/main.js"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := normalizeProjectPathForRoot(test.root, test.path); got != test.want {
				t.Fatalf("normalizeProjectPathForRoot(%q, %q) = %q, want %q", test.root, test.path, got, test.want)
			}
		})
	}
}

func TestNormalizeProjectInputPathMakesDriveRelativePathAbsolute(t *testing.T) {
	got, err := normalizeProjectInputPath("C:project/main.js")
	if err != nil {
		t.Fatal(err)
	}
	if got == "C:project/main.js" || classifyProjectPath(got) == projectPathUnknown {
		t.Fatalf("normalized drive-relative path = %q", got)
	}
}

func TestProjectUpdateNormalizesMixedWindowsPathFamiliesOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows path semantics are required")
	}
	project := NewProject(`C:\project`)
	if _, err := project.UpdateFiles(map[string]string{
		`C:\project\main.js`:         "const mainValue = 1;\n",
		`\\server\share\embedded.js`: "mainValue;\n",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestProjectResolvesRelativeModuleFromWindowsDrivePaths(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows path semantics are required")
	}
	root := filepath.ToSlash(t.TempDir())
	root = strings.ToLower(root[:1]) + root[1:]
	mainPath := root + "/main.js"
	helperPath := root + "/helper.js"
	mainSource := `import { value } from "./helper.js"; value.toUpp`
	project := NewProject(root)
	if _, err := project.UpdateFiles(map[string]string{
		mainPath:   mainSource,
		helperPath: `export const value = "text";`,
	}); err != nil {
		t.Fatal(err)
	}
	uri := "file:///" + mainPath
	params := fmt.Sprintf(`{"textDocument":{"uri":%q},"position":{"line":0,"character":%d}}`, uri, len(mainSource))
	raw, err := project.Request(context.Background(), "textDocument/completion", []byte(params))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "toUpperCase") {
		t.Fatalf("relative module completion missing: %s", raw)
	}
}

func TestProjectSupportsUppercaseWindowsDrivePaths(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows drive paths are required")
	}
	root := filepath.ToSlash(t.TempDir())
	root = strings.ToUpper(root[:1]) + root[1:]
	path := root + "/main.js"
	project := NewProject(root)
	if _, err := project.UpdateFiles(map[string]string{path: "const driveValue = 1;\ndriveVal\n"}); err != nil {
		t.Fatal(err)
	}
	uri := "file:///" + path
	raw, err := project.Request(context.Background(), "textDocument/completion", []byte(fmt.Sprintf(`{"textDocument":{"uri":%q},"position":{"line":1,"character":8}}`, uri)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "driveValue") {
		t.Fatalf("uppercase drive project completion missing: %s", raw)
	}
}

func TestProjectSupportsWindowsDriveRelativePaths(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows drive-relative paths are required")
	}
	driveRelativePath := strings.ToLower(filepath.VolumeName(t.TempDir())) + "drive-relative.js"
	absolutePath, err := filepath.Abs(driveRelativePath)
	if err != nil {
		t.Fatal(err)
	}
	project := NewProject(strings.ToLower(filepath.VolumeName(absolutePath)) + ".")
	if _, err := project.UpdateFiles(map[string]string{driveRelativePath: "const relativeValue = 1;\nrelativeVal\n"}); err != nil {
		t.Fatal(err)
	}
	uriPath := filepath.ToSlash(absolutePath)
	uriPath = strings.ToLower(uriPath[:1]) + uriPath[1:]
	raw, err := project.Request(context.Background(), "textDocument/completion", []byte(fmt.Sprintf(`{"textDocument":{"uri":%q},"position":{"line":1,"character":11}}`, "file:///"+uriPath)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "relativeValue") {
		t.Fatalf("drive-relative project completion missing: %s", raw)
	}
}

func TestProjectCompletionIncludesBrowserDocumentGlobal(t *testing.T) {
	project := NewProject("/tmp")
	path := "/tmp/compiler-types-empty.asp.__asp_client.js"
	if _, err := project.UpdateFiles(map[string]string{path: "\n$\n"}); err != nil {
		t.Fatal(err)
	}
	if _, err := project.UpdateFiles(map[string]string{path: "\ndocu\n"}); err != nil {
		t.Fatal(err)
	}
	raw, err := project.Request(context.Background(), "textDocument/completion", []byte(`{"textDocument":{"uri":"file:///tmp/compiler-types-empty.asp.__asp_client.js"},"position":{"line":1,"character":4}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"label":"document"`) {
		t.Fatalf("browser document completion missing: %s", raw)
	}
}

func TestProjectStateReportsActualReuseAndRebuild(t *testing.T) {
	project := NewProject("/project")
	files := map[string]string{"/project/one.js": "const value = 1;\n"}
	first, err := project.UpdateFiles(files)
	if err != nil {
		t.Fatal(err)
	}
	second, err := project.UpdateFiles(files)
	if err != nil {
		t.Fatal(err)
	}
	third, err := project.UpdateFiles(map[string]string{"/project/one.js": "const value = 2;\n"})
	if err != nil {
		t.Fatal(err)
	}
	if !first.Rebuilt || first.Incremental || second.Rebuilt || !third.Rebuilt || !third.Incremental || first.Builds != 1 || second.Builds != 1 || third.Builds != 2 {
		t.Fatalf("project lifecycle states = first %#v, second %#v, third %#v", first, second, third)
	}
	fourth, err := project.Update(map[string]string{"/project/one.js": "const value = 2;\n"}, ProjectOptions{Types: []string{}, TypesConfigured: true})
	if err != nil {
		t.Fatal(err)
	}
	if !fourth.Rebuilt || fourth.Incremental || fourth.Builds != 3 {
		t.Fatalf("compiler option change did not rebuild project: %#v", fourth)
	}
}

func TestProjectUpdateDeltaPreservesUnrelatedFilesAndUsesIncrementalProgram(t *testing.T) {
	project := NewProject("/project")
	if _, err := project.UpdateFiles(map[string]string{
		"/project/changed.js":   "const changed = { beforeMember: 1 };\nchanged.before\n",
		"/project/unrelated.js": "const unrelated = 1;\n",
	}); err != nil {
		t.Fatal(err)
	}
	state, err := project.UpdateDelta(map[string]string{
		"/project/changed.js": "const changed = { afterMember: 1 };\nchanged.after\n",
	}, nil, ProjectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !state.Rebuilt || !state.Incremental || state.Builds != 2 {
		t.Fatalf("delta project state = %#v", state)
	}
	if got := project.files["/project/unrelated.js"]; got != "const unrelated = 1;\n" {
		t.Fatalf("unrelated project file = %q", got)
	}
	raw, err := project.Request(context.Background(), "textDocument/completion", []byte(`{"textDocument":{"uri":"file:///project/changed.js"},"position":{"line":1,"character":13}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "afterMember") {
		t.Fatalf("delta completion did not observe changed file: %s", raw)
	}
}

func TestProjectRemainsUsableAfterCancelledDiagnostics(t *testing.T) {
	project := NewProject("/project")
	if _, err := project.UpdateFiles(map[string]string{
		"/project/cancel.js": "const value = { member: 1 };\nvalue.mem\n",
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _ = project.Request(ctx, "textDocument/semanticDiagnostic", []byte(`{"textDocument":{"uri":"file:///project/cancel.js"}}`))

	raw, err := project.Request(context.Background(), "textDocument/completion", []byte(`{"textDocument":{"uri":"file:///project/cancel.js"},"position":{"line":1,"character":9}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "member") {
		t.Fatalf("completion after cancelled diagnostics = %s", raw)
	}
}

func TestProjectUpdateDeltaRebuildsWhenProjectRootsChange(t *testing.T) {
	project := NewProject("/project")
	if _, err := project.UpdateFiles(map[string]string{
		"/project/existing.js": "export const existing = 1;\n",
	}); err != nil {
		t.Fatal(err)
	}
	added, err := project.UpdateDelta(map[string]string{
		"/project/added.js": "export const added = 2;\n",
	}, nil, ProjectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !added.Rebuilt || added.Incremental {
		t.Fatalf("added-root project state = %#v", added)
	}
	removed, err := project.UpdateDelta(nil, []string{"/project/added.js"}, ProjectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !removed.Rebuilt || removed.Incremental {
		t.Fatalf("removed-root project state = %#v", removed)
	}
	if _, ok := project.files["/project/added.js"]; ok {
		t.Fatal("removed project root is still present")
	}
}

func TestProjectFallsBackToFullRebuildWhenImportsChange(t *testing.T) {
	project := NewProject("/project")
	if _, err := project.UpdateFiles(map[string]string{
		"/project/helper.js": "export const value = 1;\n",
		"/project/main.js":   "const local = 1;\n",
	}); err != nil {
		t.Fatal(err)
	}
	state, err := project.UpdateFiles(map[string]string{
		"/project/helper.js": "export const value = 1;\n",
		"/project/main.js":   "import { value } from './helper.js';\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if state.Incremental {
		t.Fatalf("import structure change unexpectedly reused the compiler program: %#v", state)
	}
}

func TestProjectIncrementalUpdateRefreshesCrossFileTypes(t *testing.T) {
	project := NewProject("/project")
	files := map[string]string{
		"/project/helper.js": "const shared = { alphaMember: 1 };\n",
		"/project/main.js":   "shared.bet\n",
	}
	if _, err := project.UpdateFiles(files); err != nil {
		t.Fatal(err)
	}
	files["/project/helper.js"] = "const shared = { betaMember: 1 };\n"
	state, err := project.UpdateFiles(files)
	if err != nil {
		t.Fatal(err)
	}
	if !state.Incremental {
		t.Fatalf("single-file type change did not reuse the compiler program: %#v", state)
	}
	raw, err := project.Request(context.Background(), "textDocument/completion", []byte(`{"textDocument":{"uri":"file:///project/main.js"},"position":{"line":0,"character":10}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "betaMember") || strings.Contains(string(raw), "alphaMember") {
		t.Fatalf("cross-file completion remained stale after incremental update: %s", raw)
	}
}

func BenchmarkProjectIncrementalUpdate(b *testing.B) {
	project := NewProject("/project")
	files := map[string]string{
		"/project/helper.js": "const shared = { value: 1 };\n",
		"/project/main.js":   "shared.value;\n",
	}
	if _, err := project.UpdateFiles(files); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		files["/project/main.js"] = fmt.Sprintf("shared.value; // %d\n", i)
		state, err := project.UpdateFiles(files)
		if err != nil {
			b.Fatal(err)
		}
		if !state.Incremental {
			b.Fatalf("update %d was not incremental: %#v", i, state)
		}
	}
}

func TestProjectDiagnosticsReportJavaScriptSyntaxErrors(t *testing.T) {
	project := NewProject("/project")
	if _, err := project.UpdateFiles(map[string]string{"/project/one.js": "function broken( {\n"}); err != nil {
		t.Fatal(err)
	}
	raw, err := project.Request(context.Background(), "textDocument/diagnostic", []byte(`{"textDocument":{"uri":"file:///project/one.js"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"items":[{`) {
		t.Fatalf("compiler syntax diagnostic missing: %s", raw)
	}
	semantic, err := project.Request(context.Background(), "textDocument/semanticDiagnostic", []byte(`{"textDocument":{"uri":"file:///project/one.js"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(semantic), `"kind":"full"`) {
		t.Fatalf("semantic diagnostic response missing full report: %s", semantic)
	}
	typoProject := NewProject("/project")
	if _, err := typoProject.Update(map[string]string{"/project/typo.js": "const value = { compilerProperty: 1 };\nvalue.compilerPropert;\n"}, ProjectOptions{CompilerOptions: map[string]any{"checkJs": true}}); err != nil {
		t.Fatal(err)
	}
	typoDiagnostics, err := typoProject.Request(context.Background(), "textDocument/semanticDiagnostic", []byte(`{"textDocument":{"uri":"file:///project/typo.js"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(typoDiagnostics), "Did you mean") || !strings.Contains(string(typoDiagnostics), "compilerProperty") {
		t.Fatalf("spelling suggestion changed: %s", typoDiagnostics)
	}
	privateProject := NewProject("/project")
	if _, err := privateProject.Update(map[string]string{"/project/private.ts": "class Value { private compilerProperty = 1; }\nconst value = new Value();\nvalue.compilerPropert;\n"}, ProjectOptions{}); err != nil {
		t.Fatal(err)
	}
	privateDiagnostics, err := privateProject.Request(context.Background(), "textDocument/semanticDiagnostic", []byte(`{"textDocument":{"uri":"file:///project/private.ts"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(privateDiagnostics), "Did you mean") {
		t.Fatalf("inaccessible spelling suggestion changed: %s", privateDiagnostics)
	}
}

func TestProjectCachesDiagnosticResponseBytesAndInvalidatesOnUpdate(t *testing.T) {
	project := NewProject("/project")
	if _, err := project.UpdateFiles(map[string]string{"/project/cache.js": "const value = 1;\nvalue;\n"}); err != nil {
		t.Fatal(err)
	}
	params := []byte(`{"textDocument":{"uri":"file:///project/cache.js"}}`)
	first, err := project.Request(context.Background(), "textDocument/semanticDiagnostic", params)
	if err != nil {
		t.Fatal(err)
	}
	original := append([]byte(nil), first...)
	if len(project.diagnosticResponses) != 1 {
		t.Fatalf("diagnostic cache after miss = %d", len(project.diagnosticResponses))
	}
	first[0] ^= 1
	second, err := project.Request(context.Background(), "textDocument/semanticDiagnostic", params)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(second, original) {
		t.Fatalf("diagnostic cache hit changed response bytes: got %s want %s", second, original)
	}
	if _, err := project.UpdateFiles(map[string]string{"/project/cache.js": "const value = 2;\nvalue;\n"}); err != nil {
		t.Fatal(err)
	}
	if len(project.diagnosticResponses) != 0 {
		t.Fatalf("diagnostic cache was not invalidated: %d", len(project.diagnosticResponses))
	}
}

// lateCancelContext reports cancellation only after Err has been polled a
// fixed number of times, so a request is canceled while the checker is running.
type lateCancelContext struct {
	context.Context
	remaining *atomic.Int64
}

func (c lateCancelContext) Err() error {
	if c.remaining.Add(-1) >= 0 {
		return nil
	}
	return context.Canceled
}

func TestProjectServesRequestsAfterCanceledCheck(t *testing.T) {
	project := NewProject("/project")
	var source strings.Builder
	for index := range 400 {
		fmt.Fprintf(&source, "function f%d(value) { return missingName%d + value; }\n", index, index)
	}
	if _, err := project.Update(map[string]string{"/project/file.js": source.String()}, ProjectOptions{CompilerOptions: map[string]any{"checkJs": true}}); err != nil {
		t.Fatal(err)
	}
	params := []byte(`{"textDocument":{"uri":"file:///project/file.js"}}`)
	for _, polls := range []int64{3, 6, 12, 40} {
		remaining := &atomic.Int64{}
		remaining.Store(polls)
		_, _ = project.Request(lateCancelContext{Context: context.Background(), remaining: remaining}, "textDocument/semanticDiagnostic", params)
	}
	raw, err := project.Request(context.Background(), "textDocument/semanticDiagnostic", params)
	if err != nil {
		t.Fatalf("request after canceled checks failed: %v", err)
	}
	if !bytes.Contains(raw, []byte("missingName399")) {
		t.Fatalf("diagnostics after canceled checks are incomplete: %.200s", raw)
	}
}

func TestProjectRequestForMissingFileReturnsError(t *testing.T) {
	project := NewProject("/project")
	if _, err := project.UpdateFiles(map[string]string{"/project/present.js": "const value = 1;\n"}); err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"textDocument/foldingRange", "textDocument/documentSymbol", "textDocument/semanticTokens/full", "textDocument/semanticDiagnostic"} {
		if _, err := project.Request(context.Background(), method, []byte(`{"textDocument":{"uri":"file:///project/missing.js"}}`)); err == nil {
			t.Fatalf("%s for a missing file returned no error", method)
		}
	}
	if _, err := project.Request(context.Background(), "textDocument/foldingRange", []byte(`{"textDocument":{"uri":"file:///project/present.js"}}`)); err != nil {
		t.Fatalf("request after missing-file errors failed: %v", err)
	}
}
