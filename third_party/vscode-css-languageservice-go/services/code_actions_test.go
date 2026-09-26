package services

import (
	"reflect"
	"testing"

	"github.com/yottonoko/vscode-css-languageservice-go/languagefacts"
	"github.com/yottonoko/vscode-css-languageservice-go/lsp"
)

func TestCodeActionsForUnknownProperties(t *testing.T) {
	t.Run("Unknown Properties", func(t *testing.T) {
		assertCodeActions(t, "body { /*here*/displai: inline }", "/*here*/", []codeActionExpectation{
			{Title: "Rename to 'display'", Content: "body { /*here*/display: inline }"},
		})
		assertCodeActions(t, "body { /*here*/background-colar: red }", "/*here*/", []codeActionExpectation{
			{Title: "Rename to 'background-color'", Content: "body { /*here*/background-color: red }"},
			{Title: "Rename to 'background-clip'", Content: "body { /*here*/background-clip: red }"},
			{Title: "Rename to 'background-image'", Content: "body { /*here*/background-image: red }"},
		})
	})

	t.Run("diagnostic range must match a declaration property", func(t *testing.T) {
		input := "body { displai: inline }"
		document := lsp.NewTextDocument("test://test/test.css", "css", 0, input)
		manager := languagefacts.NewDataManager(languagefacts.DataManagerOptions{})
		start := stringsIndex(input, "inline")
		diagnostic := lsp.Diagnostic{
			Range:   lsp.Range{Start: document.PositionAt(start), End: document.PositionAt(start + len("inline"))},
			Code:    RuleUnknownProperty.ID,
			Message: "Unknown property",
		}
		actions := CodeActions(document, diagnostic.Range, lsp.CodeActionContext{Diagnostics: []lsp.Diagnostic{diagnostic}}, manager)
		if len(actions) != 0 {
			t.Fatalf("actions for non-property diagnostic = %#v", actions)
		}
	})

	t.Run("context only filters quick fixes", func(t *testing.T) {
		input := "body { displai: inline }"
		document := lsp.NewTextDocument("test://test/test.css", "css", 0, input)
		manager := languagefacts.NewDataManager(languagefacts.DataManagerOptions{})
		diagnostics := Validate(document, manager, nil)
		actions := CodeActions(document, diagnostics[0].Range, lsp.CodeActionContext{
			Diagnostics: diagnostics,
			Only:        []lsp.CodeActionKind{"source"},
		}, manager)
		if len(actions) != 0 {
			t.Fatalf("source-only code actions = %#v", actions)
		}
	})
}

type codeActionExpectation struct {
	Title   string
	Content string
}

func assertCodeActions(t *testing.T, input, tokenBefore string, expected []codeActionExpectation) {
	t.Helper()
	document := lsp.NewTextDocument("test://test/test.css", "css", 0, input)
	manager := languagefacts.NewDataManager(languagefacts.DataManagerOptions{})
	diagnostics := Validate(document, manager, nil)
	offset := stringsIndex(input, tokenBefore)
	r := lsp.Range{Start: document.PositionAt(offset), End: document.PositionAt(offset + len(tokenBefore))}
	actions := CodeActions(document, r, lsp.CodeActionContext{Diagnostics: diagnostics}, manager)

	labels := make([]string, len(actions))
	for i, action := range actions {
		labels[i] = action.Title
	}
	for _, want := range expected {
		index := indexOf(labels, want.Title)
		if index == -1 {
			t.Fatalf("quick fix %q not found in %#v", want.Title, labels)
		}
		action := actions[index]
		if action.Kind != lsp.CodeActionKindQuickFix || action.Edit == nil {
			t.Fatalf("action = %#v", action)
		}
		if len(action.Edit.DocumentChanges) != 1 {
			t.Fatalf("%s documentChanges = %#v", want.Title, action.Edit.DocumentChanges)
		}
		change := action.Edit.DocumentChanges[0]
		if change.TextDocument.URI != document.URI || change.TextDocument.Version != document.Version {
			t.Fatalf("%s textDocument = %#v", want.Title, change.TextDocument)
		}
		edits := change.Edits
		if got := lsp.ApplyEdits(document, edits); got != want.Content {
			t.Fatalf("%s content = %q, want %q", want.Title, got, want.Content)
		}
		if !reflect.DeepEqual(action.Diagnostics, diagnostics[:1]) {
			t.Fatalf("diagnostics = %#v", action.Diagnostics)
		}
	}
}

func indexOf(values []string, target string) int {
	for i, value := range values {
		if value == target {
			return i
		}
	}
	return -1
}
