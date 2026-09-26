package cssls

import (
	"context"
	"testing"

	"github.com/yottonoko/vscode-css-languageservice-go/lsp"
)

func TestLanguageServiceUnicodeRangesUseUTF16Positions(t *testing.T) {
	service := GetCSSLanguageService()
	document := lsp.NewTextDocument("test://test/unicode.css", "css", 0, ".💜 { color: #ff00aa; }\n.é { displai: inline; }")
	stylesheet := service.ParseStylesheet(document)

	colors := service.FindDocumentColors(document, stylesheet)
	if len(colors) != 1 || document.GetText(&colors[0].Range) != "#ff00aa" {
		t.Fatalf("colors = %#v", colors)
	}

	diagnostics := service.DoValidation(document, stylesheet, nil)
	if len(diagnostics) != 1 || document.GetText(&diagnostics[0].Range) != "displai" {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}

	actions := service.DoCodeActions2(document, lsp.Range{}, lsp.CodeActionContext{Diagnostics: diagnostics}, stylesheet)
	if len(actions) == 0 || actions[0].Edit == nil || len(actions[0].Edit.DocumentChanges) == 0 {
		t.Fatalf("code actions = %#v", actions)
	}
	if got := lsp.ApplyEdits(document, actions[0].Edit.DocumentChanges[0].Edits); got != ".💜 { color: #ff00aa; }\n.é { display: inline; }" {
		t.Fatalf("edited unicode document = %q", got)
	}
}

func TestLanguageServiceUnicodeCompletionPosition(t *testing.T) {
	service := GetCSSLanguageService()
	document := lsp.NewTextDocument("test://test/unicode.css", "css", 0, ".💜 { dis }")
	stylesheet := service.ParseStylesheet(document)

	list, err := service.DoComplete(context.Background(), document, lsp.Position{Line: 0, Character: 9}, stylesheet, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range list.Items {
		if item.Label == "display" {
			return
		}
	}
	t.Fatalf("completion did not include display: %#v", list.Items)
}
