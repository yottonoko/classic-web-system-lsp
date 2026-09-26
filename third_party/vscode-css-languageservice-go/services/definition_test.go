package services

import (
	"testing"

	"github.com/yottonoko/vscode-css-languageservice-go/lsp"
)

func TestDefinitionForCSSVariable(t *testing.T) {
	document, position := definitionDocument(":root{ --var1: abc;} .a{ color: var(|--var1); }")
	location := Definition(document, position)
	if location == nil {
		t.Fatal("definition is nil")
	}
	if location.URI != document.URI || document.GetText(&location.Range) != "--var1" || location.Range.Start.Character != 7 {
		t.Fatalf("definition = %#v", location)
	}
}

func TestDefinitionForKeyframes(t *testing.T) {
	document, position := definitionDocument("@keyframes id {}; #main { animation: |id 4s linear; }")
	location := Definition(document, position)
	if location == nil {
		t.Fatal("definition is nil")
	}
	if location.URI != document.URI || document.GetText(&location.Range) != "id" || location.Range.Start.Character != 11 {
		t.Fatalf("definition = %#v", location)
	}
}

func TestDefinitionForSCSSSymbols(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantText   string
		wantOffset int
	}{
		{
			name:       "variable",
			input:      "$var1: 1; $var2: |$var1;",
			wantText:   "$var1",
			wantOffset: 0,
		},
		{
			name:       "mixin",
			input:      "@mixin r1 { color: red; } .foo { @include |r1; }",
			wantText:   "r1",
			wantOffset: 7,
		},
		{
			name:       "function",
			input:      "@function r1($p1) { @return $p1; } .foo { width: |r1(1); }",
			wantText:   "r1",
			wantOffset: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			document, position := definitionDocumentForLanguage("scss", tt.input)
			location := Definition(document, position)
			if location == nil {
				t.Fatal("definition is nil")
			}
			if location.URI != document.URI || document.GetText(&location.Range) != tt.wantText || document.OffsetAt(location.Range.Start) != tt.wantOffset {
				t.Fatalf("definition = %#v", location)
			}
		})
	}
}

func definitionDocument(markedInput string) (*lsp.TextDocument, lsp.Position) {
	return definitionDocumentForLanguage("css", markedInput)
}

func definitionDocumentForLanguage(languageID string, markedInput string) (*lsp.TextDocument, lsp.Position) {
	offset := stringsIndex(markedInput, "|")
	input := markedInput[:offset] + markedInput[offset+1:]
	document := lsp.NewTextDocument(lsp.DocumentURI("test://test/test."+languageID), languageID, 0, input)
	return document, document.PositionAt(offset)
}
