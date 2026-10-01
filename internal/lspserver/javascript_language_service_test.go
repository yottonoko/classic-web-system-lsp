package lspserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestJavaScriptServiceResultCacheDoesNotStoreCancelledResult(t *testing.T) {
	cache := newJavaScriptServiceResultCache()
	ctx, cancel := context.WithCancel(context.Background())
	loads := 0
	if _, err := cache.request(ctx, "request", func() ([]byte, error) {
		loads++
		cancel()
		return []byte("stale"), nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled result error = %v", err)
	}
	raw, err := cache.request(context.Background(), "request", func() ([]byte, error) {
		loads++
		return []byte("fresh"), nil
	})
	if err != nil || string(raw) != "fresh" || loads != 2 {
		t.Fatalf("fresh result = %q, err=%v, loads=%d", raw, err, loads)
	}
}

func TestJavaScriptLanguageServiceMemberCompletionUsesCompilerType(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = t.TempDir()
	uri := "file:///tmp/compiler-member.asp"
	source := `<script>
const value = { compilerMember: 1 };
value.comp
</script>`
	server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 1, source)
	position := positionAtSuffix(source, "value.comp")
	var completion struct {
		Items []struct {
			Label string `json:"label"`
		} `json:"items"`
	}
	if !server.javaScriptLanguageServiceRequest(context.Background(), uri, position, "textDocument/completion", nil, &completion) {
		t.Fatal("real TypeScript language-service request failed")
	}
	encoded, _ := json.Marshal(completion)
	if !strings.Contains(string(encoded), "compilerMember") {
		t.Fatalf("compiler member completion missing: %s", encoded)
	}
}

func TestJavaScriptLanguageServiceSupportsWindowsOpenDocumentURI(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	root := t.TempDir()
	helperPath := filepath.Join(root, "helpers.js")
	if err := os.WriteFile(helperPath, []byte("/** @param {string} name */\nexport function greet(name) {\n  return name.toUpperCase();\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := pathToFileURI(filepath.Join(root, "canonical-open.asp"))
	source := `<script type="module">
import { greet } from "./helpers.js";
const localResult = greet("Ada");
localResult.toUpp
</script>`
	if _, rpcErr := server.handleRequest(context.Background(), "initialize", mustRaw(map[string]any{
		"processId": nil, "rootUri": pathToFileURI(root), "capabilities": map[string]any{},
	})); rpcErr != nil {
		t.Fatal(rpcErr)
	}
	if err := server.handleNotification(context.Background(), "textDocument/didOpen", mustRaw(map[string]any{
		"textDocument": map[string]any{"uri": uri, "languageId": "classic-asp", "version": 1, "text": source},
	})); err != nil {
		t.Fatal(err)
	}
	server.mu.Lock()
	configuredRoot := server.rootPath
	configuredRoots := append([]workspaceRoot(nil), server.workspaceRoots...)
	server.mu.Unlock()
	if !server.workspaceSourcePathAllowed(helperPath) {
		t.Fatalf("JavaScript helper is outside workspace: helper=%q root=%q roots=%#v within=%v symlink=%v", helperPath, configuredRoot, configuredRoots, pathWithinRoot(configuredRoot, helperPath), pathContainsSymlinkWithinRoot(helperPath, configuredRoot))
	}
	if _, err := server.readSourceFileBytes(context.Background(), helperPath, nil); err != nil {
		t.Fatalf("read JavaScript helper: %v", err)
	}
	server.settings.CheckJS = true
	diagnostics := server.diagnostics(canonicalGraphURI(uri))
	position := positionAtSuffix(source, "localResult.toUpp")
	result, rpcErr := server.handleRequest(context.Background(), "textDocument/completion", mustRaw(map[string]any{
		"textDocument": map[string]any{"uri": uri}, "position": position,
	}))
	if rpcErr != nil {
		t.Fatal(rpcErr)
	}
	completion, ok := result.(lsp.CompletionList)
	if !ok {
		t.Fatalf("completion result = %#v", result)
	}
	for _, item := range completion.Items {
		if item.Label == "toUpperCase" {
			return
		}
	}
	t.Fatalf("canonical Windows URI completion missing toUpperCase: %#v; diagnostics: %#v", completion.Items, diagnostics)
}

func TestJavaScriptLanguageServiceLockWaitHonorsCancellation(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.javascriptMu.Lock()
	defer server.javascriptMu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan bool, 1)
	go func() {
		done <- server.javaScriptLanguageServiceRequestWithLockContext(ctx, context.Background(), "file:///tmp/cancelled.asp", lsp.Position{}, "textDocument/diagnostic", nil, &javaScriptDiagnosticReport{})
	}()
	select {
	case ok := <-done:
		if ok {
			t.Fatal("cancelled JavaScript lock wait unexpectedly ran the request")
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled JavaScript lock wait remained queued")
	}
}

func TestJavaScriptColdProjectWalkHonorsCancellation(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = t.TempDir()
	for index := range 128 {
		path := filepath.Join(server.rootPath, fmt.Sprintf("module-%03d.js", index))
		if err := os.WriteFile(path, []byte(fmt.Sprintf("export const value%03d = %d;\n", index, index)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	uri := pathToFileURI(filepath.Join(server.rootPath, "default.asp"))
	source := "<script>\nval\n</script>"
	server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 1, source)
	ctx, cancel := context.WithCancel(context.Background())
	walks := 0
	server.javascriptWorkspaceWalkTestHook = func(string) {
		walks++
		if walks == 5 {
			cancel()
		}
	}
	request, ok := server.prepareJavaScriptRequestContext(ctx, uri, positionAtSuffix(source, "val"))
	if ok || request != nil {
		t.Fatal("cancelled cold JavaScript preparation unexpectedly completed")
	}
	if walks < 5 || walks > 6 {
		t.Fatalf("cancelled workspace walk visited %d entries, want prompt stop", walks)
	}
	if server.javascriptPreparation != nil {
		t.Fatal("cancelled cold JavaScript preparation was published")
	}
}

func TestJavaScriptLanguageServiceCrossFileDefinitionAndRename(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = t.TempDir()
	declarationURI := "file:///tmp/compiler-declaration.asp"
	usageURI := "file:///tmp/compiler-usage.asp"
	declaration := "<script>\nfunction sharedCompilerFunction() { return 1; }\n</script>"
	usage := "<script>\nsharedCompilerFunction();\n</script>"
	server.documents[declarationURI] = core.NewTextDocument(declarationURI, "classic-asp", 1, declaration)
	server.documents[usageURI] = core.NewTextDocument(usageURI, "classic-asp", 1, usage)
	position := positionAtSuffix(usage, "sharedCompilerFunction")
	locations := server.definition(usageURI, position)
	if len(locations) != 1 || locations[0].URI != declarationURI {
		t.Fatalf("cross-file definition = %#v", locations)
	}
	edit := server.rename(usageURI, position, "renamedCompilerFunction")
	encoded, _ := json.Marshal(edit)
	for _, uri := range []string{declarationURI, usageURI} {
		if !strings.Contains(string(encoded), uri) {
			t.Fatalf("cross-file rename missing %s: %s", uri, encoded)
		}
	}
}

func TestJavaScriptDidChangeSynchronouslyInvalidatesChangedOwnerForUnchangedRequester(t *testing.T) {
	tests := []struct {
		name          string
		changedSource string
		verify        func(*testing.T, *Server, string, lsp.Position)
	}{
		{
			name:          "changed export",
			changedSource: "<script>\nconst sharedValue = { afterMember: 1 };\n</script>",
			verify: func(t *testing.T, server *Server, requesterURI string, position lsp.Position) {
				var completion lsp.CompletionList
				if !server.javaScriptLanguageServiceRequest(context.Background(), requesterURI, position, "textDocument/completion", nil, &completion) {
					t.Fatal("completion after cross-owner change failed")
				}
				encoded, _ := json.Marshal(completion)
				if !strings.Contains(string(encoded), "afterMember") || strings.Contains(string(encoded), "beforeMember") {
					t.Fatalf("completion reused stale changed-owner snapshot: %s", encoded)
				}
			},
		},
		{
			name:          "removed JavaScript",
			changedSource: "<div>JavaScript removed</div>",
			verify: func(t *testing.T, server *Server, requesterURI string, position lsp.Position) {
				var diagnostics javaScriptDiagnosticReport
				if !server.javaScriptLanguageServiceRequest(context.Background(), requesterURI, position, "textDocument/diagnostic", nil, &diagnostics) {
					t.Fatal("diagnostics after cross-owner JavaScript removal failed")
				}
				encoded, _ := json.Marshal(diagnostics)
				if !strings.Contains(string(encoded), "Cannot find name 'sharedValue'") {
					t.Fatalf("removed changed-owner JavaScript remained in project: %s", encoded)
				}
			},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			server := New(strings.NewReader(""), io.Discard, io.Discard)
			server.rootPath = t.TempDir()
			server.settings.CheckJS = true
			server.settings.DiagnosticsDebounceMS = 60_000
			ownerURI := pathToFileURI(filepath.Join(server.rootPath, "owner.asp"))
			requesterURI := pathToFileURI(filepath.Join(server.rootPath, "requester.asp"))
			ownerSource := "<script>\nconst sharedValue = { beforeMember: 1 };\n</script>"
			requesterSource := "<script>\nconst observed = sharedValue.beforeMember;\n</script>"
			server.documents[ownerURI] = core.NewTextDocument(ownerURI, "classic-asp", 1, ownerSource)
			server.documents[requesterURI] = core.NewTextDocument(requesterURI, "classic-asp", 1, requesterSource)
			server.markJavaScriptDocumentsChangedLocked()
			position := positionAtSuffix(requesterSource, "sharedValue.")
			var initial lsp.CompletionList
			if !server.javaScriptLanguageServiceRequest(context.Background(), requesterURI, position, "textDocument/completion", nil, &initial) {
				t.Fatal("initial cross-owner completion failed")
			}
			initialJSON, _ := json.Marshal(initial)
			if !strings.Contains(string(initialJSON), "beforeMember") {
				t.Fatalf("initial cross-owner completion missing beforeMember: %s", initialJSON)
			}

			started, release := blockFirstDocumentOpenSnapshot(t, server)
			if err := server.handleNotification(context.Background(), "textDocument/didChange", mustRaw(map[string]any{
				"textDocument":   map[string]any{"uri": ownerURI, "version": 2},
				"contentChanges": []map[string]any{{"text": testCase.changedSource}},
			})); err != nil {
				t.Fatal(err)
			}
			waitForDocumentOpenSnapshotStart(t, started)
			testCase.verify(t, server, requesterURI, position)
			release()
			waitForDocumentOpenAnalysisWorkers(t, server)
			server.cancelScheduledDiagnostics(ownerURI)
		})
	}
}

func TestJavaScriptDidChangeUsesLanguageImpactWithoutDoubleArtifactInvalidation(t *testing.T) {
	tests := []struct {
		name            string
		oldText         string
		newText         string
		generationDelta uint64
	}{
		{name: "html", oldText: "beforeHtml", newText: "after HTML is wider", generationDelta: 0},
		{name: "css", oldText: "red", newText: "lavender", generationDelta: 0},
		{name: "vbscript", oldText: "beforeVB", newText: "afterVBIsWider", generationDelta: 0},
		{name: "javascript", oldText: "beforeMember", newText: "after-Member", generationDelta: 1},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			server := New(strings.NewReader(""), io.Discard, io.Discard)
			server.rootPath = t.TempDir()
			server.settings.DiagnosticsDebounceMS = 60_000
			uri := pathToFileURI(filepath.Join(server.rootPath, "mixed.asp"))
			source := `<div>beforeHtml</div>
<style>.card { color: red; }</style>
<% Dim beforeVB %>
<script>const value = { beforeMember: 1 }; value.beforeMember;</script>`
			document := core.NewTextDocument(uri, "classic-asp", 1, source)
			server.documents[uri] = document
			server.markJavaScriptDocumentsChangedLocked()
			position := positionAtSuffix(source, "value.beforeMember")
			var initial lsp.CompletionList
			if !server.javaScriptLanguageServiceRequest(context.Background(), uri, position, "textDocument/completion", nil, &initial) {
				t.Fatal("initial JavaScript preparation failed")
			}
			initialGeneration := server.javascriptDocumentGeneration
			initialProjectGeneration := server.javascriptProject.State().Generation
			offset := strings.Index(source, testCase.oldText)
			if offset < 0 {
				t.Fatalf("source missing %q", testCase.oldText)
			}
			started, release := blockFirstDocumentOpenSnapshot(t, server)
			if err := server.handleNotification(context.Background(), "textDocument/didChange", mustRaw(map[string]any{
				"textDocument": map[string]any{"uri": uri, "version": 2},
				"contentChanges": []map[string]any{{
					"range": document.Range(offset, offset+len(testCase.oldText)),
					"text":  testCase.newText,
				}},
			})); err != nil {
				t.Fatal(err)
			}
			waitForDocumentOpenSnapshotStart(t, started)
			if got, want := server.javascriptDocumentGeneration, initialGeneration+testCase.generationDelta; got != want {
				t.Fatalf("synchronous JavaScript generation = %d, want %d", got, want)
			}
			release()
			waitForDocumentOpenAnalysisWorkers(t, server)
			server.cancelScheduledDiagnostics(uri)
			if got, want := server.javascriptDocumentGeneration, initialGeneration+testCase.generationDelta; got != want {
				t.Fatalf("artifact publish advanced JavaScript generation again: got %d, want %d", got, want)
			}
			if testCase.generationDelta == 0 {
				current := server.documentByURI(uri)
				var completion lsp.CompletionList
				if current == nil || !server.javaScriptLanguageServiceRequest(context.Background(), uri, positionAtSuffix(current.Text, "value."), "textDocument/completion", nil, &completion) {
					t.Fatal("JavaScript request after unrelated language edit failed")
				}
				encoded, _ := json.Marshal(completion)
				if !strings.Contains(string(encoded), "beforeMember") {
					t.Fatalf("completion after unrelated language edit = %s", encoded)
				}
				if got := server.javascriptDocumentGeneration; got != initialGeneration {
					t.Fatalf("source-map refresh advanced JavaScript document generation: got %d, want %d", got, initialGeneration)
				}
				if got := server.javascriptProject.State().Generation; got != initialProjectGeneration {
					t.Fatalf("source-map-only refresh advanced TypeScript project generation: got %d, want %d", got, initialProjectGeneration)
				}
			}
		})
	}
}

func TestJavaScriptLanguageServiceRichHoverAndSemanticDiagnostic(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = t.TempDir()
	server.settings.CheckJS = true
	uri := "file:///tmp/compiler-hover-diagnostic.asp"
	source := `<script>
/** Returns the compiler-backed greeting.
 * @param {string} name
 */
function greeting(name) { return name.toUpperCase(); }
greeting(42);
</script>`
	server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 1, source)
	hover := server.hover(uri, positionAtSuffix(source, "function greet"))
	hoverJSON, _ := json.Marshal(hover)
	if !strings.Contains(string(hoverJSON), "compiler-backed greeting") || !strings.Contains(string(hoverJSON), "function greeting") {
		t.Fatalf("rich compiler hover missing signature/docs: %s", hoverJSON)
	}
	diagnostics := server.diagnosticsForParsed(context.Background(), core.ParseDocument(uri, source, core.Settings{DefaultLanguage: "VBScript"}))
	diagnosticJSON, _ := json.Marshal(diagnostics)
	if !strings.Contains(string(diagnosticJSON), "not assignable") {
		t.Fatalf("semantic type error missing: %s", diagnosticJSON)
	}
}

func TestJavaScriptLanguageServiceCallHierarchyUsesCompilerGraph(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = t.TempDir()
	uri := "file:///tmp/compiler-call-hierarchy.asp"
	source := `<script>
function callee() { return 1; }
function caller() { return callee(); }
</script>`
	server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 1, source)
	items := server.prepareCallHierarchy(uri, positionAtSuffix(source, "callee"))
	if len(items) != 1 {
		t.Fatalf("prepare call hierarchy = %#v", items)
	}
	incoming := server.incomingCalls(items[0])
	if len(incoming) != 1 || incoming[0].From.Name != "caller" {
		t.Fatalf("incoming calls = %#v", incoming)
	}
}

func TestJavaScriptCompletionResolveUsesCompilerDocumentation(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = t.TempDir()
	uri := "file:///tmp/compiler-resolve.asp"
	source := `<script>
class CompilerValue {
  /** Compiler property docs. */
  documentedMember = 1;
}
const value = new CompilerValue();
value.doc
</script>`
	server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 1, source)
	var completions lsp.CompletionList
	position := positionAtSuffix(source, "value.doc")
	if !server.javaScriptLanguageServiceRequest(context.Background(), uri, position, "textDocument/completion", nil, &completions) {
		t.Fatal("completion request failed")
	}
	for _, item := range completions.Items {
		if item.Label != "documentedMember" {
			continue
		}
		resolved := server.resolveCompletionItem(item)
		encoded, _ := json.Marshal(resolved)
		if !strings.Contains(string(encoded), "Compiler property docs") || !strings.Contains(string(encoded), "documentedMember") {
			t.Fatalf("resolved compiler completion missing details: %s", encoded)
		}
		return
	}
	t.Fatal("documentedMember completion missing")
}

func TestJavaScriptPrepareRenameAndInlayHintsUseCompilerRanges(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = t.TempDir()
	uri := "file:///tmp/compiler-rename-inlay.asp"
	source := `<script>
function combine(first, second) { return first + second; }
const total = combine(1, 2);
</script>`
	doc := core.NewTextDocument(uri, "classic-asp", 1, source)
	server.documents[uri] = doc
	position := positionAtSuffix(source, "total")
	renameRange := server.renameRange(uri, position)
	if renameRange == nil || doc.Text[doc.OffsetAt(renameRange.Start):doc.OffsetAt(renameRange.End)] != "total" {
		t.Fatalf("prepare rename range = %#v", renameRange)
	}
	hints := server.inlayHints(uri, doc.Range(0, len(source)))
	encoded, _ := json.Marshal(hints)
	if !strings.Contains(string(encoded), "first") || !strings.Contains(string(encoded), "second") {
		t.Fatalf("compiler inlay hints missing parameter names: %s", encoded)
	}
}

func TestJavaScriptSemanticTokensMapCompilerPropertyToASPSource(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = t.TempDir()
	uri := "file:///tmp/compiler-semantic.asp"
	source := "<div>prefix</div>\n<script>\nconst value = { compilerProperty: 1 };\nvalue.compilerProperty;\n</script>"
	server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 1, source)
	tokens := server.semanticTokens(uri)
	for _, token := range decodeSemanticTokenData(tokens.Data) {
		if token.TokenType != 6 {
			continue
		}
		line := strings.Split(source, "\n")[token.Line]
		if token.Character+token.Length <= len(line) && line[token.Character:token.Character+token.Length] == "compilerProperty" {
			return
		}
	}
	t.Fatalf("compiler property semantic token missing: %#v", tokens.Data)
}

func TestJavaScriptCodeActionUsesCompilerDiagnosticCode(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = t.TempDir()
	server.settings.CheckJS = true
	server.settings.JavaScriptAutoImports = true
	helper := filepath.Join(server.rootPath, "helpers.js")
	if err := os.WriteFile(helper, []byte("export function helperThing() { return 1; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := "file:///tmp/compiler-code-action.asp"
	source := "<script type=\"module\">\nhelperThing();\n</script>"
	doc := core.NewTextDocument(uri, "classic-asp", 1, source)
	server.documents[uri] = doc
	parsed := core.ParseDocument(uri, source, core.Settings{DefaultLanguage: "VBScript"})
	diagnostics := server.diagnosticsForParsed(context.Background(), parsed)
	var target lsp.Diagnostic
	found := false
	for _, diagnostic := range diagnostics {
		if strings.Contains(diagnostic.Message, "helperThing") {
			target, found = diagnostic, true
			break
		}
	}
	if !found {
		t.Fatalf("compiler typo diagnostic missing: %#v", diagnostics)
	}
	params := codeActionParams{TextDocument: lsp.TextDocumentIdentifier{URI: uri}, Range: target.Range}
	params.Context.Diagnostics = []lsp.Diagnostic{target}
	params.Context.Only = []string{"quickfix"}
	actions := server.codeActions(context.Background(), params)
	encoded, _ := json.Marshal(actions)
	if !strings.Contains(string(encoded), uri) || !strings.Contains(string(encoded), "helpers") || !strings.Contains(string(encoded), "import") {
		t.Fatalf("compiler import quickfix was not mapped to ASP source: %s", encoded)
	}
}

func TestJavaScriptCompilerTypesEmptyKeepsBrowserDocumentCompletionAfterChange(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = t.TempDir()
	writeAmbientTypes(t, server.rootPath, "jquery", `declare const $: { ready(callback: () => void): void };`)
	server.settings.JavaScriptCompilerOptionTypes = []string{}
	uri := pathToFileURI(filepath.Join(server.rootPath, "compiler-types-empty.asp"))
	server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 1, "<script>\n$\n</script>")
	server.completion(context.Background(), uri, lsp.Position{Line: 1, Character: 1}, nil)
	server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 2, "<script>\ndocu\n</script>")
	position := lsp.Position{Line: 1, Character: 4}
	doc, parsed := server.parsed(uri)
	region := core.RegionAt(parsed, doc.OffsetAt(position))
	if region == nil || region.Language != core.LanguageJavaScript || isJavaScriptMemberCompletion(doc, position) {
		t.Fatalf("unexpected completion context: region=%#v member=%v", region, isJavaScriptMemberCompletion(doc, position))
	}
	completions := server.completion(context.Background(), uri, position, nil)
	for _, item := range completions.Items {
		if item.Label == "document" {
			return
		}
	}
	t.Fatalf("browser document completion missing after project rebuild: %#v", completions.Items)
}

func TestJavaScriptMemberCompletionExcludesServerGlobalExtras(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = t.TempDir()
	server.settings.JavaScriptAutoImports = true
	server.settings.JavaScriptCompilerOptionTypes = []string{"jquery"}
	if err := os.WriteFile(filepath.Join(server.rootPath, "helpers.js"), []byte("export function helperThing() { return 1; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := pathToFileURI(filepath.Join(server.rootPath, "member-completion.asp"))
	source := "<script type=\"module\">\nconst obj = { ownMember: 1 };\nobj.\n</script>"
	server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 1, source)
	completions := server.completion(context.Background(), uri, lsp.Position{Line: 2, Character: 4}, nil)
	labels := map[string]struct{}{}
	for _, item := range completions.Items {
		labels[item.Label] = struct{}{}
	}
	if _, ok := labels["ownMember"]; !ok {
		t.Fatalf("JavaScript member completion missing ownMember: %#v", completions.Items)
	}
	for _, forbidden := range []string{"$", "document", "helperThing"} {
		if _, ok := labels[forbidden]; ok {
			t.Fatalf("JavaScript member completion leaked %s: %#v", forbidden, completions.Items)
		}
	}
}

func TestJavaScriptProjectPreparationReusesSnapshotAndAppliesWatchedFilesWithoutWalking(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = t.TempDir()
	uri := pathToFileURI(filepath.Join(server.rootPath, "prepared.asp"))
	source := "<script>\nconst preparedValue = 1;\npreparedValue;\n</script>"
	server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 1, source)
	position := positionAtSuffix(source, "preparedValue;")

	first, ok := server.prepareJavaScriptRequest(uri, position)
	if !ok || first.state.Builds != 1 || server.javascriptPreparation == nil {
		t.Fatalf("initial preparation = %#v, cached=%v", first, server.javascriptPreparation != nil)
	}
	preparation := server.javascriptPreparation
	second, ok := server.prepareJavaScriptRequest(uri, position)
	if !ok || server.javascriptPreparation != preparation || second.state.Builds != first.state.Builds || second.state.Rebuilt {
		t.Fatalf("warm preparation rebuilt: first=%#v second=%#v cacheReused=%v", first.state, second.state, server.javascriptPreparation == preparation)
	}

	walks, upserts, deletes := 0, 0, 0
	server.mu.Lock()
	server.javascriptWorkspaceWalkTestHook = func(string) { walks++ }
	server.javascriptProjectDeltaTestHook = func(upserted, deleted int) {
		upserts += upserted
		deletes += deleted
	}
	server.mu.Unlock()
	helperPath := filepath.Join(server.rootPath, "helper.js")
	helperProjectPath := javaScriptProjectPath(helperPath)
	if err := os.WriteFile(helperPath, []byte("export const helperValue = 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := server.didChangeWatchedFiles(didChangeWatchedFilesParams{Changes: []fileEvent{{URI: pathToFileURI(helperPath), Type: fileChangeCreated}}}); err != nil {
		t.Fatal(err)
	}
	if server.javascriptPreparation != preparation {
		t.Fatal("watched JavaScript file dropped the preparation instead of marking the file")
	}
	if _, ok := server.prepareJavaScriptRequest(uri, position); !ok {
		t.Fatal("preparation after a created JavaScript file failed")
	}
	if walks != 0 || upserts != 1 || deletes != 0 || server.javascriptPreparation.workspaceFiles[helperProjectPath] == "" || server.javascriptPreparation.autoImportExports[helperProjectPath] == nil {
		t.Fatalf("created file: walks=%d upserts=%d deletes=%d workspaceFiles=%v", walks, upserts, deletes, server.javascriptPreparation.workspaceFiles)
	}

	if err := os.Remove(helperPath); err != nil {
		t.Fatal(err)
	}
	if err := server.didChangeWatchedFiles(didChangeWatchedFilesParams{Changes: []fileEvent{{URI: pathToFileURI(helperPath), Type: fileChangeDeleted}}}); err != nil {
		t.Fatal(err)
	}
	if _, ok := server.prepareJavaScriptRequest(uri, position); !ok {
		t.Fatal("preparation after a deleted JavaScript file failed")
	}
	if _, kept := server.javascriptPreparation.workspaceFiles[helperProjectPath]; walks != 0 || deletes != 1 || kept || server.javascriptPreparation.autoImportExports[helperProjectPath] != nil {
		t.Fatalf("deleted file: walks=%d upserts=%d deletes=%d kept=%v", walks, upserts, deletes, kept)
	}
	if len(server.javascriptPreparation.dirtyWorkspaceFiles) != 0 {
		t.Fatalf("applied workspace files stayed dirty: %v", server.javascriptPreparation.dirtyWorkspaceFiles)
	}
}

func TestJavaScriptProjectPreparationReusesUnchangedMappingsAndTracksLanguageSwitches(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = t.TempDir()
	firstURI := pathToFileURI(filepath.Join(server.rootPath, "first.asp"))
	secondURI := pathToFileURI(filepath.Join(server.rootPath, "second.asp"))
	firstSource := `<script>
const emojiValue = "😀";
emojiValue;
</script>`
	secondSource := `<script>
const clientValue = 1;
clientValue;
</script>`
	firstDocument := core.NewTextDocument(firstURI, "classic-asp", 1, firstSource)
	secondDocument := core.NewTextDocument(secondURI, "classic-asp", 1, secondSource)
	server.mu.Lock()
	server.rememberOpenDocumentLocked(firstURI, firstDocument)
	server.rememberOpenDocumentLocked(secondURI, secondDocument)
	server.mu.Unlock()

	firstPosition := firstDocument.PositionAt(strings.Index(firstSource, "emojiValue;") + len("emojiValue"))
	firstRequest, ok := server.prepareJavaScriptRequest(firstURI, firstPosition)
	if !ok {
		t.Fatal("initial JavaScript preparation failed")
	}
	unchangedPath := javaScriptVirtualPath(secondURI, core.LanguageJavaScript)
	unchangedURI := (&javaScriptFileURI{path: unchangedPath}).String()
	clientURI := (&javaScriptFileURI{path: javaScriptVirtualPath(firstURI, core.LanguageJavaScript)}).String()
	oldUnchanged := firstRequest.files[unchangedURI]
	oldClient := firstRequest.files[clientURI]
	if oldUnchanged == nil || oldClient == nil {
		t.Fatalf("initial mappings = %#v, unchangedURI=%q, clientURI=%q, unchanged=%v, client=%v", firstRequest.files, unchangedURI, clientURI, oldUnchanged != nil, oldClient != nil)
	}
	virtualPosition := firstRequest.virtualPosition(firstPosition)
	if sourcePosition, mapped := firstRequest.active.virtual.ToSourcePosition(virtualPosition, firstDocument); !mapped || sourcePosition != firstPosition {
		t.Fatalf("UTF-16 mapping round trip = %#v, mapped=%v, want %#v", sourcePosition, mapped, firstPosition)
	}

	updatedFirstSource := `<script>
const emojiValue = "😀";
emojiValue + 1;
</script>`
	server.mu.Lock()
	firstDocument.Update(2, updatedFirstSource)
	server.deleteParsedCacheForURILocked(firstURI)
	server.markJavaScriptDocumentsChangedLocked()
	server.mu.Unlock()
	updatedFirstPosition := firstDocument.PositionAt(strings.Index(updatedFirstSource, "emojiValue +") + len("emojiValue"))
	secondRequest, ok := server.prepareJavaScriptRequest(firstURI, updatedFirstPosition)
	if !ok {
		t.Fatal("single-document JavaScript preparation failed")
	}
	if secondRequest.files[unchangedURI] != oldUnchanged {
		t.Fatal("unchanged document virtual mapping was rebuilt")
	}
	if secondRequest.files[clientURI] == oldClient {
		t.Fatal("changed document virtual mapping was reused")
	}

	updatedSecondSource := `<script runat="server" language="JScript">
var serverValue = 1;
serverValue;
</script>`
	server.mu.Lock()
	secondDocument.Update(2, updatedSecondSource)
	server.deleteParsedCacheForURILocked(secondURI)
	server.markJavaScriptDocumentsChangedLocked()
	server.mu.Unlock()
	serverValuePosition := secondDocument.PositionAt(strings.Index(updatedSecondSource, "serverValue;") + len("serverValue"))
	thirdRequest, ok := server.prepareJavaScriptRequest(secondURI, serverValuePosition)
	if !ok {
		t.Fatal("JScript preparation after language switch failed")
	}
	jscriptURI := (&javaScriptFileURI{path: javaScriptVirtualPath(secondURI, core.LanguageJScript)}).String()
	if thirdRequest.active.virtual.LanguageID != string(core.LanguageJScript) || thirdRequest.files[jscriptURI] == nil {
		t.Fatalf("JScript mapping after switch = %#v", thirdRequest.active)
	}
	secondClientURI := (&javaScriptFileURI{path: javaScriptVirtualPath(secondURI, core.LanguageJavaScript)}).String()
	if _, exists := thirdRequest.files[secondClientURI]; exists {
		t.Fatal("stale JavaScript mapping survived JScript switch")
	}

	server.mu.Lock()
	server.deleteOpenDocumentLocked(secondURI)
	server.mu.Unlock()
	fourthRequest, ok := server.prepareJavaScriptRequest(firstURI, updatedFirstPosition)
	if !ok {
		t.Fatal("preparation after document deletion failed")
	}
	if _, exists := fourthRequest.files[jscriptURI]; exists {
		t.Fatal("deleted document mapping survived preparation rebuild")
	}
}

func TestJavaScriptProjectPreparationDeltaDoesNotRescanWorkspaceOrUnrelatedOwners(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = t.TempDir()
	helperPath := filepath.Join(server.rootPath, "helper.js")
	if err := os.WriteFile(helperPath, []byte("const helperValue = 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	changedURI := pathToFileURI(filepath.Join(server.rootPath, "changed.asp"))
	changedSource := "<script>\nconst changedValue = { beforeMember: 1 };\nchangedValue.before\n</script>"
	changedDocument := core.NewTextDocument(changedURI, "classic-asp", 1, changedSource)
	server.documents[changedURI] = changedDocument
	const unrelatedCount = 128
	for index := range unrelatedCount {
		uri := pathToFileURI(filepath.Join(server.rootPath, fmt.Sprintf("unrelated-%03d.asp", index)))
		source := fmt.Sprintf("<script>\nconst unrelatedValue%03d = %d;\n</script>", index, index)
		server.documents[uri] = core.NewTextDocument(uri, "classic-asp", 1, source)
	}
	position := positionAtSuffix(changedSource, "changedValue.before")
	first, ok := server.prepareJavaScriptRequest(changedURI, position)
	if !ok {
		t.Fatal("initial JavaScript preparation failed")
	}
	preparation := server.javascriptPreparation
	unrelatedURI := pathToFileURI(filepath.Join(server.rootPath, "unrelated-127.asp"))
	unrelatedVirtualURI := (&javaScriptFileURI{path: javaScriptVirtualPath(unrelatedURI, core.LanguageJavaScript)}).String()
	unrelatedMapping := first.files[unrelatedVirtualURI]
	if unrelatedMapping == nil {
		t.Fatal("initial unrelated mapping missing")
	}

	walks := 0
	reads := 0
	documentInventories := 0
	deltaUpserts := 0
	deltaDeletes := 0
	parsedURIs := []string{}
	server.javascriptWorkspaceWalkTestHook = func(string) { walks++ }
	server.javascriptDocumentsTestHook = func(int) { documentInventories++ }
	server.javascriptProjectDeltaTestHook = func(upserts, deletes int) {
		deltaUpserts += upserts
		deltaDeletes += deletes
	}
	server.workspaceFileReadTestHook = func(string) { reads++ }
	server.documentParseTestHook = func(uri string) { parsedURIs = append(parsedURIs, uri) }
	updatedSource := "<script>\nconst changedValue = { afterMember: 1 };\nchangedValue.after\n</script>"
	server.mu.Lock()
	changedDocument.Update(2, updatedSource)
	server.deleteParsedCacheForURILocked(changedURI)
	server.markJavaScriptDocumentChangedLocked(changedURI)
	server.mu.Unlock()
	updatedPosition := positionAtSuffix(updatedSource, "changedValue.after")
	second, ok := server.prepareJavaScriptRequest(changedURI, updatedPosition)
	if !ok {
		t.Fatal("delta JavaScript preparation failed")
	}
	if server.javascriptPreparation != preparation {
		t.Fatal("ordinary JavaScript edit replaced the project preparation")
	}
	if !second.state.Incremental {
		t.Fatalf("ordinary JavaScript edit did not reuse UpdateProgram: %#v", second.state)
	}
	if walks != 0 || reads != 0 || documentInventories != 0 {
		t.Fatalf("delta preparation touched workspace inventory: walks=%d reads=%d document inventories=%d", walks, reads, documentInventories)
	}
	if deltaUpserts != 1 || deltaDeletes != 0 {
		t.Fatalf("project delta = %d upserts/%d deletes, want one changed virtual file", deltaUpserts, deltaDeletes)
	}
	if len(parsedURIs) != 1 || parsedURIs[0] != changedURI {
		t.Fatalf("delta preparation parsed URIs = %#v, want only %q", parsedURIs, changedURI)
	}
	if second.files[unrelatedVirtualURI] != unrelatedMapping {
		t.Fatal("delta preparation rebuilt an unrelated owner mapping")
	}
	var completion struct {
		Items []struct {
			Label string `json:"label"`
		} `json:"items"`
	}
	if !server.javaScriptLanguageServiceRequest(context.Background(), changedURI, updatedPosition, "textDocument/completion", nil, &completion) {
		t.Fatal("completion after delta preparation failed")
	}
	encoded, _ := json.Marshal(completion)
	if !strings.Contains(string(encoded), "afterMember") {
		t.Fatalf("completion did not observe delta update: %s", encoded)
	}
}

func TestJavaScriptProjectPreparationRefreshesOnlyRequestedOwnerAndSkipsUnchangedVirtualText(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = t.TempDir()
	uri := pathToFileURI(filepath.Join(server.rootPath, "active.asp"))
	unrelatedURI := pathToFileURI(filepath.Join(server.rootPath, "unrelated.asp"))
	source := "<script type=\"text/javascript\">\nconst activeValue = { activeMember: 1 };\nactiveValue.activeM\n</script>"
	unrelatedSource := "<script>\nconst unrelatedValue = 1;\n</script>"
	document := core.NewTextDocument(uri, "classic-asp", 1, source)
	unrelatedDocument := core.NewTextDocument(unrelatedURI, "classic-asp", 1, unrelatedSource)
	server.documents[uri] = document
	server.documents[unrelatedURI] = unrelatedDocument
	position := positionAtSuffix(source, "activeValue.activeM")
	first, ok := server.prepareJavaScriptRequest(uri, position)
	if !ok {
		t.Fatal("initial JavaScript preparation failed")
	}
	initialGeneration := first.state.Generation

	updatedSource := "<script>\nconst activeValue = { activeMember: 1 };\nactiveValue.activeM\n</script>"
	updatedUnrelatedSource := "<script>\nconst changedUnrelatedValue = 1;\n</script>"
	server.mu.Lock()
	document.Update(2, updatedSource)
	unrelatedDocument.Update(2, updatedUnrelatedSource)
	server.deleteParsedCacheForURILocked(uri)
	server.deleteParsedCacheForURILocked(unrelatedURI)
	server.markJavaScriptDocumentChangedLocked(uri)
	server.markJavaScriptDocumentChangedLocked(unrelatedURI)
	server.mu.Unlock()

	deltaCalls := 0
	deltaUpserts := -1
	deltaDeletes := -1
	parsedURIs := []string{}
	server.javascriptProjectDeltaTestHook = func(upserts, deletes int) {
		deltaCalls++
		deltaUpserts = upserts
		deltaDeletes = deletes
	}
	server.documentParseTestHook = func(parsedURI string) { parsedURIs = append(parsedURIs, parsedURI) }
	updatedPosition := positionAtSuffix(updatedSource, "activeValue.activeM")
	second, ok := server.prepareJavaScriptRequest(uri, updatedPosition)
	if !ok {
		t.Fatal("mapping-only JavaScript preparation failed")
	}
	if deltaCalls != 1 || deltaUpserts != 0 || deltaDeletes != 0 {
		t.Fatalf("mapping-only project delta = calls:%d upserts:%d deletes:%d, want one observed zero-file delta", deltaCalls, deltaUpserts, deltaDeletes)
	}
	if second.state.Generation != initialGeneration {
		t.Fatalf("mapping-only edit advanced project generation from %d to %d", initialGeneration, second.state.Generation)
	}
	if len(parsedURIs) != 1 || parsedURIs[0] != uri {
		t.Fatalf("mapping-only request parsed URIs = %#v, want only %q", parsedURIs, uri)
	}
	if _, dirty := server.javascriptPreparation.dirtyOwners[uri]; dirty {
		t.Fatal("requested owner remained dirty after mapping refresh")
	}
	if _, dirty := server.javascriptPreparation.dirtyOwners[unrelatedURI]; !dirty {
		t.Fatal("interactive request consumed unrelated dirty owner")
	}
	active := second.active
	if active == nil || active.source.Text != document.Text || active.source.Version != document.Version {
		t.Fatal("mapping-only request did not publish the current source mapping")
	}
	var completion struct {
		Items []struct {
			Label string `json:"label"`
		} `json:"items"`
	}
	if !server.javaScriptLanguageServiceRequest(context.Background(), uri, updatedPosition, "textDocument/completion", nil, &completion) {
		t.Fatal("completion after mapping-only refresh failed")
	}
	encoded, _ := json.Marshal(completion)
	if !strings.Contains(string(encoded), "activeMember") {
		t.Fatalf("completion did not use refreshed mapping: %s", encoded)
	}
}

func TestJavaScriptWorkspaceIndexProjectAndAutoImportShareOnePhysicalRead(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = t.TempDir()
	helperPath := filepath.Join(server.rootPath, "helper.js")
	if err := os.WriteFile(helperPath, []byte("export const sharedHelper = 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := pathToFileURI(filepath.Join(server.rootPath, "default.asp"))
	source := "<script>\nsharedH\n</script>"
	document := core.NewTextDocument(uri, "classic-asp", 1, source)
	server.documents[uri] = document
	physicalReads := 0
	server.workspaceFileReadTestHook = func(path string) {
		if filepath.Clean(path) == filepath.Clean(helperPath) {
			physicalReads++
		}
	}
	if _, err := server.readWorkspaceTextFile(helperPath); err != nil {
		t.Fatal(err)
	}
	position := positionAtSuffix(source, "sharedH")
	if _, ok := server.prepareJavaScriptRequest(uri, position); !ok {
		t.Fatal("JavaScript project preparation failed")
	}
	parsed := server.parseTextDocument(document, server.settings.DefaultLanguage)
	if candidates := server.javascriptAutoImportCandidates(context.Background(), uri, "sharedH"); len(candidates) != 1 || candidates[0].Name != "sharedHelper" {
		t.Fatalf("auto-import candidates = %#v", candidates)
	}
	if items := server.javascriptAutoImportCompletions(context.Background(), parsed, document, position, document.OffsetAt(position)); !hasCompletionLabel(items, "sharedHelper") {
		t.Fatalf("auto-import completion missing sharedHelper: %#v", items)
	}
	if physicalReads != 1 {
		t.Fatalf("workspace index, project, and auto-import performed %d physical reads, want one", physicalReads)
	}

	server.invalidateSourceSnapshot(helperPath)
	server.mu.Lock()
	server.resetJavaScriptProjectLocked()
	server.mu.Unlock()
	if _, ok := server.prepareJavaScriptRequest(uri, position); !ok {
		t.Fatal("JavaScript project preparation after invalidation failed")
	}
	if physicalReads != 2 {
		t.Fatalf("watcher-style invalidation physical reads = %d, want exactly one new read", physicalReads)
	}
}

func TestJavaScriptFileURIUsesCanonicalUNCForm(t *testing.T) {
	uri := (&javaScriptFileURI{path: "//server/share/site/default.asp.__asp_client.js"}).String()
	if uri != "file://server/share/site/default.asp.__asp_client.js" {
		t.Fatalf("UNC JavaScript URI = %q", uri)
	}
}

func TestJavaScriptProjectConfigCacheInvalidatesExternalChanges(t *testing.T) {
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	root := t.TempDir()
	sourceDirectory := filepath.Join(root, "src")
	if err := os.MkdirAll(sourceDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	sourceURI := pathToFileURI(filepath.Join(sourceDirectory, "page.asp"))
	configPath := filepath.Join(root, "jsconfig.json")
	if err := os.WriteFile(configPath, []byte(`{"compilerOptions":{"types":["jquery"],"target":"ES5"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	first := server.cachedJavaScriptProjectConfig(root, sourceURI)
	if first.Options["target"] != "ES5" || len(first.Types) != 1 || first.Types[0] != "jquery" {
		t.Fatalf("initial config = %#v", first)
	}
	if len(server.javascriptProjectConfigCache) != 1 {
		t.Fatalf("config cache entries = %d", len(server.javascriptProjectConfigCache))
	}
	if err := os.WriteFile(configPath, []byte(`{"compilerOptions":{"types":[],"target":"ESNext"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	updated := server.cachedJavaScriptProjectConfig(root, sourceURI)
	if updated.Options["target"] != "ESNext" || updated.Types == nil || len(updated.Types) != 0 {
		t.Fatalf("updated config = %#v", updated)
	}
	if err := os.Remove(configPath); err != nil {
		t.Fatal(err)
	}
	missing := server.cachedJavaScriptProjectConfig(root, sourceURI)
	if missing.Options != nil || missing.Types != nil {
		t.Fatalf("deleted config retained stale values = %#v", missing)
	}
	if err := os.WriteFile(configPath, []byte(`{"compilerOptions":{"types":["node"]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	recreated := server.cachedJavaScriptProjectConfig(root, sourceURI)
	if len(recreated.Types) != 1 || recreated.Types[0] != "node" {
		t.Fatalf("recreated config = %#v", recreated)
	}
}

func positionAtSuffix(text, suffix string) (position lsp.Position) {
	offset := strings.Index(text, suffix) + len(suffix)
	position.Line = strings.Count(text[:offset], "\n")
	lineStart := strings.LastIndex(text[:offset], "\n") + 1
	position.Character = offset - lineStart
	return position
}

func TestJavaScriptWorkspaceFileMembershipMatchesWalkRules(t *testing.T) {
	root := t.TempDir()
	for _, directory := range []string{"js", "build", "nested", filepath.Join("node_modules", "lib")} {
		if err := os.MkdirAll(filepath.Join(root, directory), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "nested", "jsconfig.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	filter := javaScriptWorkspaceFileFilter{root: root, workspaceFilter: javascriptProjectDiscoverySettings{}.filterForRoot(root)}
	for _, test := range []struct {
		path              string
		included, decided bool
	}{
		{filepath.Join(root, "js", "app.js"), true, true},
		{filepath.Join(root, "app.ts"), true, true},
		{filepath.Join(root, "js", "page.asp"), false, true},
		{filepath.Join(root, "build", "bundle.js"), false, true},
		{filepath.Join(root, "node_modules", "lib", "index.js"), false, true},
		{filepath.Join(filepath.Dir(root), "outside.js"), false, true},
		// A nested project is kept only when it holds the requesting document.
		{filepath.Join(root, "nested", "module.js"), false, false},
	} {
		included, decided := filter.membership(test.path)
		if included != test.included || decided != test.decided {
			t.Errorf("membership(%s) = %v, %v; want %v, %v", test.path, included, decided, test.included, test.decided)
		}
	}
}
