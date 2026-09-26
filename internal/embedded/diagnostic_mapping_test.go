package embedded

import (
	"strings"
	"testing"

	csslsp "github.com/yottonoko/vscode-css-languageservice-go/lsp"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestRemapCSSRangeRejectsRangesAcrossMaskedASPHoles(t *testing.T) {
	source := `<style>.card { color: red; <% If enabled Then %> background: blue; }</style>`
	parsed := core.ParseDocument("file:///css-range.asp", source, core.Settings{})
	virtual := core.BuildVirtualDocument(parsed, core.LanguageCSS)
	doc := core.NewTextDocument(parsed.URI, "classic-asp", 0, source)
	virtualDoc := core.NewTextDocument(virtual.URI, "css", 0, virtual.Text)
	start := strings.Index(virtual.Text, "color")
	end := strings.Index(virtual.Text, "background") + len("background")
	rangeInVirtual := csslsp.Range{
		Start: csslsp.Position(virtualDoc.PositionAt(start)),
		End:   csslsp.Position(virtualDoc.PositionAt(end)),
	}
	if mapped, ok := remapCSSRange(virtual, doc, rangeInVirtual); ok {
		t.Fatalf("cross-hole CSS range mapped to %#v", mapped)
	}

	backgroundStart := strings.Index(virtual.Text, "background")
	backgroundEnd := backgroundStart + len("background")
	mapped, ok := remapCSSRange(virtual, doc, csslsp.Range{
		Start: csslsp.Position(virtualDoc.PositionAt(backgroundStart)),
		End:   csslsp.Position(virtualDoc.PositionAt(backgroundEnd)),
	})
	want := lsp.Range{Start: doc.PositionAt(strings.Index(source, "background")), End: doc.PositionAt(strings.Index(source, "background") + len("background"))}
	if !ok || mapped != want {
		t.Fatalf("CSS range after hole = %#v, ok=%t, want %#v", mapped, ok, want)
	}
}
