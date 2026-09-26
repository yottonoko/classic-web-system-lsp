package lspserver

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

func TestCSSRegionCompletionPreservesCrossRegionCustomProperties(t *testing.T) {
	const uri = "file:///site/cross-region-custom-property.asp"
	source := `<style>:root { --brand-color: red; }</style>
<style>.card { color: var(--br) }</style>`
	server := New(nil, io.Discard, io.Discard)
	document := core.NewTextDocument(uri, "classic-asp", 1, source)
	server.documents[uri] = document
	parsed := server.parseTextDocument(document, server.settings.DefaultLanguage)
	position := document.PositionAt(strings.LastIndex(source, "--br") + len("--br"))
	full := server.css.Complete(context.Background(), parsed, position)
	local := server.css.CompleteDocument(context.Background(), parsed, document, position)
	if hasCompletionLabel(full.Items, "--brand-color") && !hasCompletionLabel(local.Items, "--brand-color") {
		t.Fatal("region-local CSS completion lost a custom property declared in another style block")
	}
}

func TestCSSRegionCompletionPreservesCrossRegionKeyframes(t *testing.T) {
	const uri = "file:///site/cross-region-keyframes.asp"
	source := `<style>@keyframes shimmer { from { opacity: 0; } to { opacity: 1; } }</style>
<style>.card { animation-name: shi }</style>`
	server := New(nil, io.Discard, io.Discard)
	document := core.NewTextDocument(uri, "classic-asp", 1, source)
	server.documents[uri] = document
	parsed := server.parseTextDocument(document, server.settings.DefaultLanguage)
	position := document.PositionAt(strings.LastIndex(source, "shi") + len("shi"))
	local := server.css.CompleteDocument(context.Background(), parsed, document, position)
	if !hasCompletionLabel(local.Items, "shimmer") {
		t.Fatal("region-local CSS completion lost keyframes declared in another style block")
	}
}
