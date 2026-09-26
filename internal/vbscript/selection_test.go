package vbscript

import (
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestSelectionRangeBuildsTokenStatementBlockRegionDocumentChain(t *testing.T) {
	source := `<div></div>
<%
Function BuildName(value)
  If value <> "" Then
    BuildName = value
  End If
End Function
%>`
	parsed := core.ParseDocument("file:///selection.asp", source, core.Settings{})
	doc := core.NewTextDocument(parsed.URI, "classic-asp", 0, source)
	selection := SelectionRange(parsed, doc.PositionAt(strings.Index(source, "BuildName =")+2))
	if selection == nil {
		t.Fatal("selection range is nil")
	}
	count := 0
	for current := selection; current != nil; current = current.Parent {
		count++
		if current.Parent != nil && !selectionRangeContains(current.Parent.Range, current.Range) {
			t.Fatalf("parent %#v does not contain child %#v", current.Parent.Range, current.Range)
		}
	}
	if count < 5 {
		t.Fatalf("selection chain length = %d, want token, statement, block, region, document", count)
	}
}

func selectionRangeContains(outer, inner lsp.Range) bool {
	return compareSelectionPositions(outer.Start, inner.Start) <= 0 && compareSelectionPositions(outer.End, inner.End) >= 0
}

func compareSelectionPositions(left, right lsp.Position) int {
	if left.Line != right.Line {
		return left.Line - right.Line
	}
	return left.Character - right.Character
}
