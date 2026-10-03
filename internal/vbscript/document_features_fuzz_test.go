package vbscript

import (
	"os"
	"path/filepath"
	"testing"
	"unicode/utf8"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func FuzzDocumentFeaturesKeepRangesInsideDocument(f *testing.F) {
	samples, _ := filepath.Glob("../../samples/classic-asp-dashboard/*.asp")
	includes, _ := filepath.Glob("../../samples/classic-asp-dashboard/includes/*.inc")
	for _, path := range append(samples, includes...) {
		if data, err := os.ReadFile(path); err == nil && len(data) <= 6000 {
			f.Add(string(data))
		}
	}
	for _, seed := range []string{
		"<%😀0",
		"<% Dim a, b\na = 1 : b = a %>",
		"<%\nClass Foo\nPublic Property Get Bar()\nBar = 1\nEnd Property\nEnd Class\nSet x = New Foo\nx.Bar\n%>",
		"<script runat=\"server\" language=\"VBScript\">\nFunction F(ByRef a, ByVal b)\nF = a & b\nEnd Function\n</script>",
		"<%= Request(\"id\") %><% sql = \"SELECT * FROM t WHERE id=\" & Request(\"id\") : conn.Execute sql %>",
		"<% If a Then _\n b = 1 _\n Else c = 2 %>",
		"<% With obj\n.Name = \"x\"\nEnd With\nReDim Preserve arr(10)\nFor Each i In arr\nNext %>",
		"<!-- #include file=\"a.inc\" --><% ''' <summary>x</summary>\nSub S(p)\nEnd Sub %>",
		"<% Select Case x\nCase 1, 2\nCase Else\nEnd Select\nDo While x\nLoop\nWhile y\nWend %>",
		"<% Rem comment\n' 😀 \"unterminated\n x = \"a\"\"b\" 日本 %>",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		if len(text) > 4096 || !utf8.ValidString(text) {
			return
		}
		parsed := core.ParseDocument("file:///fuzz/page.asp", text, core.Settings{DefaultLanguage: "VBScript"})
		doc := core.NewTextDocument(parsed.URI, "classic-asp", 1, text)
		end := doc.PositionAt(len(text))
		checkRange := func(kind string, r lsp.Range) {
			t.Helper()
			if r.Start.Line < 0 || r.Start.Character < 0 || compareSelectionPositions(r.Start, r.End) > 0 || compareSelectionPositions(r.End, end) > 0 {
				t.Fatalf("%s range %#v is inverted or outside the document ending at %#v", kind, r, end)
			}
		}
		for _, diagnostic := range SyntaxDiagnostics(parsed, SyntaxOptions{IfSyntaxDiagnostics: "strict"}) {
			checkRange("syntax diagnostic", diagnostic.Range)
		}
		for _, diagnostic := range DeadCodeDiagnostics(parsed) {
			checkRange("dead code diagnostic", diagnostic.Range)
		}
		for _, diagnostic := range SQLInjectionDiagnostics(parsed, lsp.DiagnosticSeverity(2)) {
			checkRange("SQL injection diagnostic", diagnostic.Range)
		}
		FoldingRanges(parsed)
		DocumentSymbols(parsed)
		SemanticTokens(parsed)
		Signatures(parsed)
		BuildSymbolIndex(parsed)
		step := max(1, len(text)/64)
		for offset := 0; offset <= len(text); offset += step {
			if offset < len(text) && !utf8.RuneStart(text[offset]) {
				continue
			}
			position := doc.PositionAt(offset)
			for _, location := range Definition(parsed, position) {
				checkRange("definition", location.Range)
			}
			for _, location := range References(parsed, position, true) {
				checkRange("reference", location.Range)
			}
			for _, highlight := range Highlights(parsed, position) {
				checkRange("highlight", highlight.Range)
			}
			if r := RenameRange(parsed, position); r != nil {
				checkRange("rename", *r)
			}
			for selection := SelectionRange(parsed, position); selection != nil; selection = selection.Parent {
				checkRange("selection", selection.Range)
			}
			SignatureHelp(parsed, position)
			HoverAt(text, offset)
			CompletionsFor(text, offset)
		}
		for _, edit := range core.FormatDocument(parsed, core.FormattingOptions{TabSize: 4, InsertSpaces: true}) {
			checkRange("format edit", edit.Range)
		}
	})
}
