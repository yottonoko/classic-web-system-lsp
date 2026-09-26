package lspserver

import (
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestOrganizeJavaScriptImportsPreservesSeparatedContent(t *testing.T) {
	const uri = "file:///organize-imports.asp"
	const sourceText = `<script type="module">
import { z } from "z";
import { a } from "a";
const retained = z + a;
// retained comment
import { d } from "d";
<% Response.Write "retained ASP" %>
import { c } from "c";
import { b } from "b";
</script>`
	const want = `<script type="module">
import { a } from "a";
import { z } from "z";
const retained = z + a;
// retained comment
import { d } from "d";
<% Response.Write "retained ASP" %>
import { b } from "b";
import { c } from "c";
</script>`

	parsed := core.ParseDocument(uri, sourceText, core.Settings{})
	region := javaScriptRegionForOrganizeImportTest(t, parsed)
	source := core.NewTextDocument(uri, "classic-asp", 0, sourceText)
	edits, ok := organizeImportEdits(source, sourceText, region, parsed.Regions)
	if !ok {
		t.Fatal("organizeImportEdits returned no edits")
	}
	if len(edits) != 3 {
		t.Fatalf("organizeImportEdits returned %d edits, want 3 independent import blocks", len(edits))
	}

	if got := applyOrganizeImportEdits(t, source, edits); got != want {
		t.Fatalf("organized source mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestOrganizeJavaScriptImportsSortsAdjacentImports(t *testing.T) {
	const uri = "file:///organize-adjacent-imports.asp"
	const sourceText = "<script type=\"module\">\r\n" +
		"import { z } from \"z\";\r\n" +
		"import { a } from \"a\";\r\n" +
		"</script>"
	const want = "<script type=\"module\">\r\n" +
		"import { a } from \"a\";\r\n" +
		"import { z } from \"z\";\r\n" +
		"</script>"

	parsed := core.ParseDocument(uri, sourceText, core.Settings{})
	region := javaScriptRegionForOrganizeImportTest(t, parsed)
	source := core.NewTextDocument(uri, "classic-asp", 0, sourceText)
	edits, ok := organizeImportEdits(source, sourceText, region, parsed.Regions)
	if !ok {
		t.Fatal("organizeImportEdits returned no edits")
	}
	if len(edits) != 1 {
		t.Fatalf("organizeImportEdits returned %d edits, want 1 contiguous import block", len(edits))
	}

	if got := applyOrganizeImportEdits(t, source, edits); got != want {
		t.Fatalf("organized source mismatch:\n--- got ---\n%q\n--- want ---\n%q", got, want)
	}
}

func javaScriptRegionForOrganizeImportTest(t *testing.T, parsed *core.ParsedDocument) core.Region {
	t.Helper()
	for _, region := range parsed.Regions {
		if region.Kind == core.RegionClientScript && region.Language == core.LanguageJavaScript {
			return region
		}
	}
	t.Fatal("JavaScript client script region not found")
	return core.Region{}
}

func applyOrganizeImportEdits(t *testing.T, source *core.TextDocument, edits []lsp.TextEdit) string {
	t.Helper()
	got := source.Text
	for i := len(edits) - 1; i >= 0; i-- {
		start := source.OffsetAt(edits[i].Range.Start)
		end := source.OffsetAt(edits[i].Range.End)
		if start < 0 || end < start || end > len(source.Text) {
			t.Fatalf("invalid edit range %#v for source length %d", edits[i].Range, len(source.Text))
		}
		got = got[:start] + edits[i].NewText + got[end:]
	}
	return got
}
