package javascript

import (
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestJavaScriptNavigationAndRename(t *testing.T) {
	const uri = "file:///tmp/script.asp"
	source := `<script>
function formatName(value) {
  return value;
}
const result = formatName(customer.name);
</script>`
	parsed := core.ParseDocument(uri, source, core.Settings{})
	position := positionOf(t, source, "formatName(customer", 2)

	definition := Definition(parsed, position)
	if len(definition) != 1 || definition[0].Range.Start.Line != 1 {
		t.Fatalf("expected JS definition on function line, got %#v", definition)
	}

	references := References(parsed, position, true)
	if len(references) != 2 {
		t.Fatalf("expected declaration and call references, got %#v", references)
	}

	rename := RenameEdit(parsed, position, "renderName")
	changes := rename["changes"].(map[string]any)
	edits := changes[uri].([]lsp.TextEdit)
	if len(edits) != 2 {
		t.Fatalf("expected two JS rename edits, got %#v", edits)
	}

	hover := Hover(parsed, position)
	if hover == nil {
		t.Fatalf("expected hover")
	}

	documentSymbols := DocumentSymbols(parsed)
	if len(documentSymbols) == 0 {
		t.Fatalf("expected JS document symbols")
	}
	workspaceSymbols := WorkspaceSymbols(parsed, "format")
	if len(workspaceSymbols) != 1 || workspaceSymbols[0].Name != "formatName" {
		t.Fatalf("expected JS workspace symbol, got %#v", workspaceSymbols)
	}
}

func TestSemanticDiagnosticsSkipASPHoles(t *testing.T) {
	const uri = "file:///tmp/script.asp"
	source := `<div>before</div>
<script>
const fromAsp = <%= Request("id") %> + <% Response.Write ServerSideNumber %>;
const object = { key: "<%= ServerKey %>", more: <% implicitValue = 1 : Response.Write implicitValue %> };
const fake = "<% not an island";
missingAfterIslands.toFixed();
const after = maybeMissingAgain(<%= AfterArg %>);
</script>`
	parsed := core.ParseDocument(uri, source, core.Settings{})
	diagnostics := SemanticDiagnostics(parsed)
	virtual := core.BuildVirtualDocument(parsed, core.LanguageJavaScript)
	var found bool
	for _, diagnostic := range diagnostics {
		if diagnostic.Source == "asp-lsp-typescript" && diagnostic.Message == "Cannot find name 'missingAfterIslands'." {
			found = true
			if diagnostic.Range.Start.Line != 5 || diagnostic.Range.Start.Character != 0 {
				t.Fatalf("unexpected diagnostic range = %#v", diagnostic.Range)
			}
		}
		if strings.Contains(diagnostic.Message, "AfterArg") {
			t.Fatalf("unexpected ASP island diagnostic = %#v", diagnostic)
		}
	}
	if !found {
		t.Fatalf("diagnostics = %#v\nvirtual = %q\nregions = %#v", diagnostics, virtual.Text, parsed.Regions)
	}
}

func TestJavaScriptCallHierarchy(t *testing.T) {
	const uri = "file:///tmp/calls.asp"
	source := `<script>
function renderCard(value) {
  return value;
}
function boot() {
  return renderCard("ready");
}
</script>`
	parsed := core.ParseDocument(uri, source, core.Settings{})
	position := positionOf(t, source, "renderCard(\"ready", 2)

	items := PrepareCallHierarchy(parsed, position)
	if len(items) != 1 || items[0].Name != "renderCard" {
		t.Fatalf("expected renderCard hierarchy item, got %#v", items)
	}

	incoming := IncomingCalls(parsed, "renderCard")
	if len(incoming) != 1 || incoming[0].From.Name != "boot" {
		t.Fatalf("expected incoming call from boot, got %#v", incoming)
	}

	outgoing := OutgoingCalls(parsed, "boot")
	if len(outgoing) != 1 || outgoing[0].To.Name != "renderCard" {
		t.Fatalf("expected outgoing call to renderCard, got %#v", outgoing)
	}
}

func TestJavaScriptInlayHints(t *testing.T) {
	const uri = "file:///tmp/hints.asp"
	source := `<script>
function add(first, second) {
  return first + second;
}
const result = add(1, 2);
</script>`
	parsed := core.ParseDocument(uri, source, core.Settings{})
	doc := core.NewTextDocument(uri, "classic-asp", 0, source)
	hints := InlayHints(parsed, lsp.Range{Start: doc.PositionAt(0), End: doc.PositionAt(len(source))})
	if len(hints) != 2 || hints[0].Label != "first:" || hints[1].Label != "second:" {
		t.Fatalf("expected JavaScript parameter hints, got %#v", hints)
	}
}

func positionOf(t *testing.T, source, needle string, delta int) lsp.Position {
	t.Helper()
	index := -1
	for i := 0; i+len(needle) <= len(source); i++ {
		if source[i:i+len(needle)] == needle {
			index = i + delta
			break
		}
	}
	if index < 0 {
		t.Fatalf("needle %q not found", needle)
	}
	doc := core.NewTextDocument("file:///tmp/test.asp", "classic-asp", 0, source)
	return doc.PositionAt(index)
}
