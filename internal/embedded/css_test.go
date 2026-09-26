package embedded

import (
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	csslsp "github.com/yottonoko/vscode-css-languageservice-go/lsp"
)

func TestCSSCrossRegionCompletionIndexSkipsCommentsAndStrings(t *testing.T) {
	source := `<style>
/* --commented: red; @keyframes commented */
.card { content: "--quoted: value @keyframes quoted"; }
:root { --theme-color: coral; }
@keyframes pulse { from { opacity: 0; } }
</style>`
	parsed := core.ParseDocument("file:///site/css-index.asp", source, core.Settings{})
	index := cssCrossRegionCompletions(parsed)
	if !containsCSSCompletionName(index.customProperties, "--theme-color") || containsCSSCompletionName(index.customProperties, "--commented") || containsCSSCompletionName(index.customProperties, "--quoted") {
		t.Fatalf("custom property index = %#v", index.customProperties)
	}
	if !containsCSSCompletionName(index.keyframes, "pulse") || containsCSSCompletionName(index.keyframes, "commented") || containsCSSCompletionName(index.keyframes, "quoted") {
		t.Fatalf("keyframe index = %#v", index.keyframes)
	}
}

func containsCSSCompletionName(names []string, want string) bool {
	return strings.Contains("\x00"+strings.Join(names, "\x00")+"\x00", "\x00"+want+"\x00")
}

func TestCSSEmptyValueDiagnosticsMatchVSCodeCSS(t *testing.T) {
	diagnostics := cssEmptyValueDiagnostics(".x { color: }", nil)
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	diagnostic := diagnostics[0]
	if diagnostic.Code != "css-propertyvalueexpected" {
		t.Fatalf("code = %#v", diagnostic.Code)
	}
	if diagnostic.Message != "property value expected" {
		t.Fatalf("message = %q", diagnostic.Message)
	}
	if diagnostic.Range.Start.Line != 0 || diagnostic.Range.Start.Character != 12 ||
		diagnostic.Range.End.Line != 0 || diagnostic.Range.End.Character != 13 {
		t.Fatalf("range = %#v", diagnostic.Range)
	}
}

func TestCSSEmptyValueDiagnosticsSkipsExistingRange(t *testing.T) {
	existing := []csslsp.Diagnostic{{
		Range: csslsp.Range{
			Start: csslsp.Position{Line: 0, Character: 12},
			End:   csslsp.Position{Line: 0, Character: 13},
		},
	}}
	diagnostics := cssEmptyValueDiagnostics(".x { color: }", existing)
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestCSSEmptyValueDiagnosticsUsesUTF16Positions(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		character int
	}{
		{name: "BMP", text: ".é { color: }", character: 12},
		{name: "non-BMP", text: ".😀 { color: }", character: 13},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			diagnostics := cssEmptyValueDiagnostics(test.text, nil)
			if len(diagnostics) != 1 {
				t.Fatalf("diagnostics = %#v", diagnostics)
			}
			got := diagnostics[0].Range
			want := csslsp.Range{
				Start: csslsp.Position{Line: 0, Character: test.character},
				End:   csslsp.Position{Line: 0, Character: test.character + 1},
			}
			if got != want {
				t.Fatalf("range = %#v, want %#v", got, want)
			}
		})
	}
}

func TestCSSEmptyValueDiagnosticsIgnoresBracesInStringsAndComments(t *testing.T) {
	text := `.x { content: "}"; /* } :; */ color: }`
	diagnostics := cssEmptyValueDiagnostics(text, nil)
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	got := diagnostics[0].Range
	want := csslsp.Range{
		Start: csslsp.Position{Line: 0, Character: 37},
		End:   csslsp.Position{Line: 0, Character: 38},
	}
	if got != want {
		t.Fatalf("range = %#v, want %#v", got, want)
	}
}
