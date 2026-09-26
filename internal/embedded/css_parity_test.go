package embedded

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestCSSDefinitionMapsCustomPropertyBackToASPSource(t *testing.T) {
	source := `<style>
:root { --theme-color: coral; }
.card { color: var(--theme-color); }
</style>`
	parsed := core.ParseDocument("file:///site/custom-property.asp", source, core.Settings{})
	doc := core.NewTextDocument(parsed.URI, "classic-asp", 0, source)
	usage := strings.LastIndex(source, "--theme-color") + 3

	location := NewCSS().Definition(parsed, doc.PositionAt(usage))
	if location == nil {
		t.Fatal("custom property definition is nil")
	}
	if location.URI != parsed.URI || location.Range != doc.Range(strings.Index(source, "--theme-color"), strings.Index(source, "--theme-color")+len("--theme-color")) {
		t.Fatalf("definition = %#v", location)
	}
}

func TestCSSCodeActionsMapAllServiceResultsBackToASPSource(t *testing.T) {
	source := `<style>
.card { background-colar: coral; }
</style>`
	parsed := core.ParseDocument("file:///site/code-actions.asp", source, core.Settings{})
	css := NewCSS()
	diagnostics := css.Diagnostics(parsed)
	var unknown *lsp.Diagnostic
	for index := range diagnostics {
		if diagnostics[index].Code == "unknownProperties" {
			unknown = &diagnostics[index]
			break
		}
	}
	if unknown == nil {
		t.Fatalf("unknown property diagnostic missing: %#v", diagnostics)
	}

	actions := css.CodeActions(parsed, unknown.Range, diagnostics, []string{"quickfix"})
	if len(actions) < 2 {
		t.Fatalf("code actions = %#v, want multiple service results", actions)
	}
	serialized, err := json.Marshal(actions)
	if err != nil {
		t.Fatal(err)
	}
	text := string(serialized)
	for _, expected := range []string{"Rename to 'background-color'", "Rename to 'background-clip'", `"uri":"file:///site/code-actions.asp"`} {
		if !strings.Contains(text, expected) {
			t.Fatalf("code actions missing %q: %s", expected, text)
		}
	}
	if strings.Contains(text, ".css.virtual") {
		t.Fatalf("code actions leaked virtual URI: %s", text)
	}
	if actions := css.CodeActions(parsed, unknown.Range, diagnostics, []string{"source"}); len(actions) != 0 {
		t.Fatalf("source-only request returned CSS quick fixes: %#v", actions)
	}
}
