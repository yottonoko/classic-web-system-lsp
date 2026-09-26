package lspserver

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	workspacepkg "github.com/yottonoko/classic-web-system-lsp/internal/workspace"
)

func TestLegacyUndefinedGlobalsParticipateInLanguageFeatures(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	server.settings.VBScriptAssumeUndefinedGlobals = true
	server.settings.CodeLensReferences = true
	server.settings.CodeLensReferenceGlobals = true
	uri := filePathURI(filepath.Join(t.TempDir(), "legacy-features.asp"))
	text := `<% Option Explicit
LegacyValue = LegacyFunction()
Response.Write LegacyValue
Dim item
Set item = New LegacyClass

%>`
	document := core.NewTextDocument(uri, "classic-asp", 1, text)
	server.documents[uri] = document
	server.workspace[uri] = document
	parsed := server.parseTextDocument(document, "VBScript")
	if _, ok := server.workspaceLegacyUndefinedGlobals(context.Background()); !ok {
		t.Fatal("legacy undefined global catalog was not built")
	}
	position := document.PositionAt(strings.Index(text, "LegacyValue"))
	if _, ok := server.WorkspaceLegacyUndefinedGlobalAt(context.Background(), uri, position); !ok {
		t.Fatal("legacy undefined global was not found at its source position")
	}

	completion := server.completion(context.Background(), uri, lsp.Position{Line: 6, Character: 0}, nil)
	for _, name := range []string{"LegacyValue", "LegacyFunction", "LegacyClass"} {
		if !completionHasLabel(completion.Items, name) {
			t.Fatalf("completion missing %q: %#v", name, completion.Items)
		}
	}

	hover, ok := server.hover(uri, position).(*lsp.Hover)
	if !ok || hover == nil {
		t.Fatalf("hover = %#v", hover)
	}
	contents, contentsOK := hover.Contents.(lsp.MarkupContent)
	if !contentsOK || !strings.Contains(contents.Value, "legacy global") || !strings.Contains(contents.Value, "Observed references: 2") {
		t.Fatalf("hover = %#v", hover)
	}
	definition := server.definition(uri, position)
	if len(definition) != 1 || !workspacepkg.SameFileIdentityURI(definition[0].URI, uri) || definition[0].Range.Start != position {
		t.Fatalf("definition = %#v", definition)
	}
	references := server.referencesContext(context.Background(), uri, position, true)
	if len(references) != 2 {
		t.Fatalf("references = %#v, want 2", references)
	}
	if renameRange := server.renameRange(uri, position); renameRange == nil || renameRange.Start != position {
		t.Fatalf("rename range = %#v", renameRange)
	}
	rename := server.rename(uri, position, "RenamedLegacy")
	changes, ok := rename["changes"].(map[string][]lsp.TextEdit)
	edits := changes[uri]
	if len(edits) == 0 {
		for changedURI, candidateEdits := range changes {
			if workspacepkg.SameFileIdentityURI(changedURI, uri) {
				edits = candidateEdits
				break
			}
		}
	}
	if !ok || len(edits) != 2 {
		t.Fatalf("rename changes = %#v", rename)
	}

	if !documentSymbolsContain(server.documentSymbols(uri), "LegacyValue") {
		t.Fatalf("document symbols missing legacy global: %#v", server.documentSymbols(uri))
	}
	if !workspaceSymbolsContain(server.workspaceSymbols(context.Background(), "LegacyValue"), "LegacyValue") {
		t.Fatalf("workspace symbols missing legacy global: %#v", server.workspaceSymbols(context.Background(), "LegacyValue"))
	}
	if codeLensesContainName(server.codeLens(uri), "LegacyValue") {
		t.Fatalf("CodeLens must not target a legacy global usage: %#v", server.codeLens(uri))
	}
	payload := server.buildDocumentSetGraph("workspace", uri, []*core.ParsedDocument{parsed}, true, false)
	graphHasLegacyValue := false
	for _, node := range payload.Nodes {
		if strings.EqualFold(node.Label, "LegacyValue") {
			graphHasLegacyValue = true
			break
		}
	}
	if !graphHasLegacyValue {
		t.Fatalf("graph missing legacy global: %#v", payload.Nodes)
	}

	for _, diagnostic := range server.diagnosticsForParsed(context.Background(), parsed) {
		if diagnostic.Source == "asp-lsp-vbscript" && strings.Contains(diagnostic.Message, "LegacyValue") {
			t.Fatalf("enabled compatibility setting left undeclared diagnostic: %#v", diagnostic)
		}
	}
}

func TestLegacyUndefinedGlobalCompletionDoesNotJoinBackgroundCatalogBuild(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	server.settings.VBScriptAssumeUndefinedGlobals = true
	server.legacyUndefinedGlobalBuilds = map[legacyUndefinedGlobalBuildKey]*legacyUndefinedGlobalBuild{}
	buildKey := legacyUndefinedGlobalBuildKey{
		generation:          server.graphGeneration,
		settingsFingerprint: server.legacyUndefinedGlobalSettingsFingerprint(nil),
	}
	server.legacyUndefinedGlobalBuilds[buildKey] = &legacyUndefinedGlobalBuild{done: make(chan struct{})}
	completed := make(chan struct{})
	go func() {
		_ = server.legacyUndefinedGlobalCompletions(context.Background())
		close(completed)
	}()
	select {
	case <-completed:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("completion joined a background legacy-global catalog build")
	}
	close(server.legacyUndefinedGlobalBuilds[buildKey].done)
}

func TestLegacyUndefinedGlobalsDefaultOffKeepsOptionExplicitDiagnostic(t *testing.T) {
	server := New(nil, io.Discard, io.Discard)
	uri := "file:///workspace/legacy-disabled.asp"
	text := "<% Option Explicit\nResponse.Write LegacyValue\n%>"
	document := core.NewTextDocument(uri, "classic-asp", 1, text)
	server.documents[uri] = document
	server.workspace[uri] = document
	parsed := server.parseTextDocument(document, "VBScript")

	diagnostics := server.diagnosticsForParsed(context.Background(), parsed)
	for _, diagnostic := range diagnostics {
		if diagnostic.Source == "asp-lsp-vbscript" && strings.Contains(diagnostic.Message, "LegacyValue") {
			return
		}
	}
	t.Fatalf("default-off setting suppressed undeclared diagnostic: %#v", diagnostics)
}

func completionHasLabel(items []lsp.CompletionItem, name string) bool {
	for _, item := range items {
		if strings.EqualFold(item.Label, name) {
			return true
		}
	}
	return false
}

func documentSymbolsContain(symbols []lsp.DocumentSymbol, name string) bool {
	for _, symbol := range symbols {
		if strings.EqualFold(symbol.Name, name) {
			return true
		}
	}
	return false
}

func workspaceSymbolsContain(symbols []lsp.SymbolInformation, name string) bool {
	for _, symbol := range symbols {
		if strings.EqualFold(symbol.Name, name) {
			return true
		}
	}
	return false
}

func codeLensesContainName(lenses []lsp.CodeLens, name string) bool {
	for _, lens := range lenses {
		data, ok := lens.Data.(map[string]any)
		label, labelOK := data["name"].(string)
		if ok && labelOK && strings.EqualFold(label, name) {
			return true
		}
	}
	return false
}
