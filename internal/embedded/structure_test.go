package embedded

import (
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestEmbeddedCSSSelectionRangeRemapsAcrossNestedASP(t *testing.T) {
	source := `<style>
.panel { <% If ok Then %>color: red;<% End If %> }
</style>`
	parsed := core.ParseDocument("file:///selection.asp", source, core.Settings{DefaultLanguage: string(core.LanguageVBScript)})
	doc := core.NewTextDocument(parsed.URI, "classic-asp", 0, source)
	_ = NewHTML().Diagnostics(parsed)
	selection := NewCSS().SelectionRange(parsed, doc.PositionAt(strings.Index(source, "color")))
	if selection == nil || selection.Range.Start.Line != 1 {
		t.Fatalf("selection range = %#v", selection)
	}
	assertSelectionParentsContainChildren(t, *selection)
}

func TestEmbeddedCSSRenameUsesLanguageServiceAndRemapsAllReferences(t *testing.T) {
	source := `<style>:root{--accent:red}.card{color:var(--accent)}</style>`
	parsed := core.ParseDocument("file:///rename.asp", source, core.Settings{DefaultLanguage: string(core.LanguageVBScript)})
	doc := core.NewTextDocument(parsed.URI, "classic-asp", 0, source)
	position := doc.PositionAt(strings.Index(source, "--accent") + 2)
	css := NewCSS()
	if prepared := css.PrepareRename(parsed, position); prepared == nil {
		t.Fatal("prepare rename returned nil")
	}
	edit := css.Rename(parsed, position, "--theme")
	if edit == nil || len(edit.Changes[parsed.URI]) != 2 {
		t.Fatalf("rename edit = %#v", edit)
	}
}

func TestEmbeddedHTMLServicesRemapNestedTagStructures(t *testing.T) {
	source := `<main>
  <section><span>value</span></section>
</main>`
	parsed := core.ParseDocument("file:///structure.asp", source, core.Settings{DefaultLanguage: string(core.LanguageVBScript)})
	doc := core.NewTextDocument(parsed.URI, "classic-asp", 0, source)
	html := NewHTML()
	if diagnostics := html.Diagnostics(parsed); len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	position := doc.PositionAt(strings.Index(source, "span") + 1)
	if highlights := html.Highlights(parsed, position); len(highlights) != 2 {
		t.Fatalf("highlights = %#v", highlights)
	}
	if ranges := html.LinkedEditingRanges(parsed, position); len(ranges) != 2 {
		t.Fatalf("linked ranges = %#v", ranges)
	}
	if symbols := html.DocumentSymbols(parsed); len(symbols) != 1 || len(symbols[0].Children) != 1 {
		t.Fatalf("symbols = %#v", symbols)
	}
}

func TestEmbeddedHTMLDiagnosticsRemapScannerErrors(t *testing.T) {
	source := `</a😀>`
	parsed := core.ParseDocument("file:///diagnostics.asp", source, core.Settings{DefaultLanguage: string(core.LanguageVBScript)})
	diagnostics := NewHTML().Diagnostics(parsed)
	if len(diagnostics) == 0 || diagnostics[0].Source != "asp-lsp-html" {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestEmbeddedHTMLDiagnosticsMapUTF16ScannerOffsetsToSourceBytes(t *testing.T) {
	source := `😀</a😀>`
	parsed := core.ParseDocument("file:///diagnostics-unicode.asp", source, core.Settings{DefaultLanguage: string(core.LanguageVBScript)})
	diagnostics := NewHTML().Diagnostics(parsed)
	if len(diagnostics) != 1 || diagnostics[0].Message != "Closing bracket expected." {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	doc := core.NewTextDocument(parsed.URI, "classic-asp", 0, source)
	malformedCharacter := strings.LastIndex(source, "😀")
	want := doc.Range(malformedCharacter, malformedCharacter+len("😀"))
	if diagnostics[0].Range != want {
		t.Fatalf("diagnostic range = %#v, want %#v", diagnostics[0].Range, want)
	}
}

func TestEmbeddedHTMLDiagnosticsMapUTF16ScannerOffsetsAfterASPHole(t *testing.T) {
	source := `😀</a<%= name %>😀>`
	parsed := core.ParseDocument("file:///diagnostics-unicode-asp-hole.asp", source, core.Settings{DefaultLanguage: string(core.LanguageVBScript)})
	diagnostics := NewHTML().Diagnostics(parsed)
	if len(diagnostics) != 1 || diagnostics[0].Message != "Closing bracket expected." {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	doc := core.NewTextDocument(parsed.URI, "classic-asp", 0, source)
	malformedCharacter := strings.LastIndex(source, "😀")
	want := doc.Range(malformedCharacter, malformedCharacter+len("😀"))
	if diagnostics[0].Range != want {
		t.Fatalf("diagnostic range = %#v, want %#v", diagnostics[0].Range, want)
	}
}

func TestEmbeddedHTMLDiagnosticsSkipRangesCrossingMaskedASPHoles(t *testing.T) {
	source := `</<% dynamicTagName %>>`
	parsed := core.ParseDocument("file:///diagnostics-asp-hole.asp", source, core.Settings{DefaultLanguage: string(core.LanguageVBScript)})
	if diagnostics := NewHTML().Diagnostics(parsed); len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v, want no diagnostic mapped from the masked ASP tag name", diagnostics)
	}
}

func TestEmbeddedCSSDiagnosticsSkipSyntheticInlineStyleWrapper(t *testing.T) {
	source := `<div style=""></div><style>.empty {}</style>`
	parsed := core.ParseDocument("file:///diagnostics-inline-style.asp", source, core.Settings{})
	diagnostics := NewCSS().Diagnostics(parsed)
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics = %#v, want only the real CSS empty-ruleset diagnostic", diagnostics)
	}
	diagnostic := diagnostics[0]
	if diagnostic.Code != "emptyRules" || diagnostic.Message != "Do not use empty rulesets" {
		t.Fatalf("diagnostic = %#v", diagnostic)
	}
	doc := core.NewTextDocument(parsed.URI, "classic-asp", 0, source)
	styleStart := strings.Index(source, ".empty")
	if diagnostic.Range != doc.Range(styleStart, strings.Index(source, "</style>")) {
		t.Fatalf("diagnostic range = %#v, want the CSS rule range", diagnostic.Range)
	}
}

func assertSelectionParentsContainChildren(t *testing.T, selection lsp.SelectionRange) {
	t.Helper()
	for selection.Parent != nil {
		parent := selection.Parent.Range
		child := selection.Range
		if compareTestPositions(parent.Start, child.Start) > 0 || compareTestPositions(parent.End, child.End) < 0 {
			t.Fatalf("parent %#v does not contain child %#v", parent, child)
		}
		selection = *selection.Parent
	}
}

func compareTestPositions(left, right lsp.Position) int {
	if left.Line != right.Line {
		return left.Line - right.Line
	}
	return left.Character - right.Character
}

func TestJavaScriptFormatterKeepsFunctionAndControlBraceSettingsIndependent(t *testing.T) {
	const source = "function f() {\nif(x) {\nreturn 1;\n}\n}"
	functionsOnly, err := FormatJavaScript(source, core.FormattingOptions{
		InsertSpaces:                    true,
		TabSize:                         2,
		JavaScriptBraceFunctionsNewLine: lsp.BoolPtr(true),
		JavaScriptBraceControlNewLine:   lsp.BoolPtr(false),
	}, core.LanguageJavaScript)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(functionsOnly, "function f()\n{") || !strings.Contains(functionsOnly, "if (x) {") {
		t.Fatalf("functions-only braces = %q", functionsOnly)
	}
	controlOnly, err := FormatJavaScript(source, core.FormattingOptions{
		InsertSpaces:                    true,
		TabSize:                         2,
		JavaScriptBraceFunctionsNewLine: lsp.BoolPtr(false),
		JavaScriptBraceControlNewLine:   lsp.BoolPtr(true),
	}, core.LanguageJavaScript)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(controlOnly, "function f() {") || !strings.Contains(controlOnly, "if (x)\n  {") {
		t.Fatalf("control-only braces = %q", controlOnly)
	}
}
