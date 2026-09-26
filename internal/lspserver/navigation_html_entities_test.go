package lspserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestNavigationHTMLStandardEntitiesPreserveEncodedSourceEvidence(t *testing.T) {
	root := t.TempDir()
	page := filepath.Join(root, "default.asp")
	targets := map[string]string{
		"decimal.asp":         "decimal&#46;asp",
		"amp&target.asp":      "amp&#38;target.asp",
		"hex.asp":             "hex&#x2e;asp",
		"named©.asp":          "named&copy;.asp",
		"malformed&bogus.asp": "malformed&bogus.asp",
		"repeat&target.asp":   "repeat&amp;target.asp",
	}
	for target := range targets {
		if err := os.WriteFile(filepath.Join(root, target), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	source := "🌸 <a href=\"decimal&#46;asp\">decimal</a>\n" +
		"<a href=\"amp&#38;target.asp\">amp</a>\n" +
		"<a href=\"hex&#x2e;asp\">hex</a>\n" +
		"<a href=\"named&copy;.asp\">named</a>\n" +
		"<a href=\"malformed&bogus.asp\">malformed</a>\n" +
		"<a href=\"repeat&amp;target.asp\">one</a><a href=\"repeat&amp;target.asp\">two</a>"
	parsed := core.ParseDocument(filePathURI(page), source, core.Settings{})
	builder := newNavigationGraphBuilder("document", parsed.URI, []workspaceRoot{{Path: root, URI: filePathURI(root)}})
	builder.addDocument(parsed, parsed.URI)
	if builder.navigationError != nil {
		t.Fatalf("HTML entity navigation error = %v", builder.navigationError)
	}
	seen := make(map[string]map[string]any, len(builder.edges))
	for _, edge := range builder.edges {
		seen[navigationHTMLTestEdgeTarget(builder, edge)] = edge
	}
	document := core.NewTextDocument(parsed.URI, "classic-asp", 0, source)
	for target, raw := range targets {
		edge := seen[target]
		if edge == nil {
			t.Fatalf("HTML entity target %q missing: %#v", target, builder.edges)
		}
		starts := []int{strings.Index(source, raw)}
		if target == "repeat&target.asp" {
			starts = []int{strings.Index(source, raw), strings.LastIndex(source, raw)}
		}
		ranges, _ := edge["ranges"].([]lsp.Range)
		if len(ranges) != len(starts) {
			t.Fatalf("HTML entity target %q ranges = %#v, want %d", target, ranges, len(starts))
		}
		for index, start := range starts {
			if start < 0 {
				t.Fatalf("raw HTML entity source %q missing for target %q", raw, target)
			}
			want := document.Range(start, start+len(raw))
			if ranges[index] != want {
				t.Fatalf("HTML entity target %q range[%d] = %#v, want raw encoded range %#v", target, index, ranges[index], want)
			}
		}
	}
	if got := decodeHTMLAttributeValue("&amp;#46;"); got != "&#46;" {
		t.Fatalf("HTML entity decoder double-decoded = %q, want &#46;", got)
	}
}
