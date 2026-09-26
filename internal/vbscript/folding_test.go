package vbscript

import (
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestFoldingRangesForIfBranchesAndLoops(t *testing.T) {
	source := `<%
If ready Then
  Response.Write 1
ElseIf other Then
  Response.Write 2
Else
  Response.Write 3
End If
If inlineReady Then Response.Write inlineReady
Do While ready
  Response.Write 4
Loop
While ready
  Response.Write 5
Wend
For index = 1 To 3
  Response.Write index
Next
For Each item In items
  Response.Write item
Next
%>`
	parsed := core.ParseDocument("file:///tmp/folding.asp", source, core.Settings{})
	ranges := FoldingRanges(parsed)
	for _, expected := range [][2]int{{1, 2}, {3, 4}, {5, 7}, {9, 11}, {12, 14}, {15, 17}, {18, 20}} {
		if !hasFoldingRange(ranges, expected[0], expected[1]) {
			t.Fatalf("missing folding range %v in %#v", expected, ranges)
		}
	}
	if hasFoldingRangeStartingAt(ranges, 8) {
		t.Fatalf("inline If should not produce a folding range: %#v", ranges)
	}
}

func hasFoldingRange(ranges []lsp.FoldingRange, startLine int, endLine int) bool {
	for _, r := range ranges {
		if r.StartLine == startLine && r.EndLine == endLine {
			return true
		}
	}
	return false
}

func hasFoldingRangeStartingAt(ranges []lsp.FoldingRange, startLine int) bool {
	for _, r := range ranges {
		if r.StartLine == startLine {
			return true
		}
	}
	return false
}
