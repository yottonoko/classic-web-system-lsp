package services

import (
	"reflect"
	"sort"
	"testing"

	"github.com/yottonoko/vscode-css-languageservice-go/lsp"
)

func TestFoldingRanges(t *testing.T) {
	tests := []foldingTest{
		{name: "Fold single rule", lines: []string{".foo {", "  color: red;", "}"}, expected: []lsp.FoldingRange{FoldingRange(0, 1)}},
		{name: "No fold for single line", lines: []string{".foo { color: red; }"}},
		{name: "Fold multiple rules", lines: []string{".foo {", "  color: red;", "  opacity: 1;", "}"}, expected: []lsp.FoldingRange{FoldingRange(0, 2)}},
		{name: "Fold with no indentation", lines: []string{".foo{", "color: red;", "}"}, expected: []lsp.FoldingRange{FoldingRange(0, 1)}},
		{name: "Fold with opening curly brace on new line", lines: []string{".foo", "{", "color: red;", "}"}, expected: []lsp.FoldingRange{FoldingRange(1, 2)}},
		{name: "Fold with closing curly brace on same line", lines: []string{".foo", "{", "color: red; }"}, expected: []lsp.FoldingRange{FoldingRange(1, 2)}},
		{name: "Without closing curly brace", lines: []string{".foo {", "color: red;"}},
		{name: "Without closing curly brace creates correct folding ranges", lines: []string{".foo {", "color: red;", ".bar {", "color: blue;", "}"}, expected: []lsp.FoldingRange{FoldingRange(2, 3)}},
		{name: "Without closing curly brace in nested rules creates correct folding ranges", lines: []string{".foo {", "  .bar {", "  .baz {", "    color: blue;", "  }", "}"}, expected: []lsp.FoldingRange{FoldingRange(1, 4), FoldingRange(2, 3)}},
		{name: "Without opening curly brace should not throw error", lines: []string{".foo", "  color: blue;", "}}"}},
		{name: "Comment - single star", lines: []string{"/*", ".foo {", "  color: red;", "}", "*/"}, expected: []lsp.FoldingRange{FoldingRange(0, 4, "comment")}},
		{name: "Comment - double star", lines: []string{"/**", ".foo {", "  color: red;", "}", "*/"}, expected: []lsp.FoldingRange{FoldingRange(0, 4, "comment")}},
		{name: "Comment - wrong indentation and no newline", lines: []string{"/**", ".foo{", "color: red;", "} */"}, expected: []lsp.FoldingRange{FoldingRange(0, 3, "comment")}},
		{name: "Comment - Single line", lines: []string{"./* .foo { color: red; } */"}},
		{name: "Postcss nested", lines: []string{".foo {", "& .bar {", "  color: red;", "}", "}"}, expected: []lsp.FoldingRange{FoldingRange(0, 3), FoldingRange(1, 2)}},
		{name: "Media query", lines: []string{"@media screen {", ".foo {", "color: red;", "}", "}"}, expected: []lsp.FoldingRange{FoldingRange(0, 3), FoldingRange(1, 2)}},
		{name: "Simple region with comment", lines: []string{"/* #region */", "& .bar {", "  color: red;", "}", "/* #endregion */"}, expected: []lsp.FoldingRange{FoldingRange(0, 4, "region"), FoldingRange(1, 2)}},
		{name: "Simple region with padded comment", lines: []string{"/*  #region   */", "& .bar {", "  color: red;", "}", "/*   #endregion   */"}, expected: []lsp.FoldingRange{FoldingRange(0, 4, "region"), FoldingRange(1, 2)}},
		{name: "Simple region without spaces", lines: []string{"/*#region*/", "& .bar {", "  color: red;", "}", "/*#endregion*/"}, expected: []lsp.FoldingRange{FoldingRange(0, 4, "region"), FoldingRange(1, 2)}},
		{name: "Simple region with description", lines: []string{"/* #region Header page */", ".bar {", "  color: red;", "}", "/* #endregion */"}, expected: []lsp.FoldingRange{FoldingRange(0, 4, "region"), FoldingRange(1, 2)}},
		{name: "Max ranges", lines: []string{"/* #region Header page */", ".bar {", "  color: red;", "}", "/* #endregion */"}, expected: []lsp.FoldingRange{FoldingRange(0, 4, "region")}, rangeLimit: 1},
		{name: "region intersecting with declaration", lines: []string{"/* #region */", ".bar {", "  color: red;", "/* #endregion */", "  display: block;", "}"}, expected: []lsp.FoldingRange{FoldingRange(0, 3, "region")}},
		{name: "declaration intersecting with region", lines: []string{".bar {", "/* #region */", "  color: red;", "}", "/* #endregion */"}, expected: []lsp.FoldingRange{FoldingRange(0, 2)}},
		{name: "incomplete region marker", lines: []string{"/* #endregion */"}},
		{name: "SCSS Mixin", languageIDs: []string{"scss"}, lines: []string{"@mixin clearfix($width) {", "  @if !$width {", "    // if width is not passed, or empty do this", "  } @else {", "    display: inline-block;", "    width: $width;", "  }", "}"}, expected: []lsp.FoldingRange{FoldingRange(0, 6), FoldingRange(1, 2), FoldingRange(3, 5)}},
		{name: "SCSS Interolation", languageIDs: []string{"scss"}, lines: []string{".orbit-#{$d}-prev {", "  foo-#{$d}-bar: 1;", "  #{$d}-bar-#{$d}: 2;", "}"}, expected: []lsp.FoldingRange{FoldingRange(0, 2)}},
		{name: "SCSS While", languageIDs: []string{"scss"}, lines: []string{"@while $i > 0 {", "  .item-#{$i} { width: 2em * $i; }", "  $i: $i - 2;", "}"}, expected: []lsp.FoldingRange{FoldingRange(0, 2)}},
		{name: "SCSS Nested media query", languageIDs: []string{"scss"}, lines: []string{"@mixin desktop {", "  $desktop-width: 1024px;", "  @media(min-width: #{$desktop-width}) {", "    width: 500px;", "  }", "}"}, expected: []lsp.FoldingRange{FoldingRange(0, 4), FoldingRange(2, 3)}},
		{name: "SCSS/LESS simple region with comment", languageIDs: []string{"scss", "less"}, lines: []string{"/* #region */", "& .bar {", "  color: red;", "}", "/* #endregion */"}, expected: []lsp.FoldingRange{FoldingRange(0, 4, "region"), FoldingRange(1, 2)}},
		{name: "SCSS/LESS simple region with padded comment", languageIDs: []string{"scss", "less"}, lines: []string{"/*  #region  */", "& .bar {", "  color: red;", "}", "/*   #endregion   */"}, expected: []lsp.FoldingRange{FoldingRange(0, 4, "region"), FoldingRange(1, 2)}},
		{name: "Region with SCSS single line comment", languageIDs: []string{"scss", "less"}, lines: []string{"// #region", "& .bar {", "  color: red;", "}", "// #endregion"}, expected: []lsp.FoldingRange{FoldingRange(0, 4, "region"), FoldingRange(1, 2)}},
		{name: "Region with SCSS single line padded comment", languageIDs: []string{"scss", "less"}, lines: []string{"//   #region  ", "& .bar {", "  color: red;", "}", "//   #endregion"}, expected: []lsp.FoldingRange{FoldingRange(0, 4, "region"), FoldingRange(1, 2)}},
		{name: "Region with both simple comments and region comments", languageIDs: []string{"scss", "less"}, lines: []string{"// #region", "/*", "comments", "*/", "& .bar {", "  color: red;", "}", "// #endregion"}, expected: []lsp.FoldingRange{FoldingRange(0, 7, "region"), FoldingRange(1, 3, "comment"), FoldingRange(4, 5)}},
	}
	for _, tt := range tests {
		languageIDs := tt.languageIDs
		if len(languageIDs) == 0 {
			languageIDs = []string{"css"}
		}
		for _, languageID := range languageIDs {
			t.Run(tt.name+"/"+languageID, func(t *testing.T) {
				assertRanges(t, tt.lines, tt.expected, languageID, tt.rangeLimit)
			})
		}
	}
}

type foldingTest struct {
	name        string
	lines       []string
	expected    []lsp.FoldingRange
	languageIDs []string
	rangeLimit  int
}

func assertRanges(t *testing.T, lines []string, expected []lsp.FoldingRange, languageID string, rangeLimit int) {
	t.Helper()
	document := lsp.NewTextDocument(lsp.DocumentURI("test://foo/bar."+languageID), languageID, 1, joinLines(lines))
	actual := GetFoldingRanges(document, rangeLimit)
	sort.SliceStable(actual, func(i, j int) bool { return actual[i].StartLine < actual[j].StartLine })
	if expected == nil {
		expected = []lsp.FoldingRange{}
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("%s\nactual: %#v\nwant:   %#v", joinLines(lines), actual, expected)
	}
}

func joinLines(lines []string) string {
	result := ""
	for i, line := range lines {
		if i > 0 {
			result += "\n"
		}
		result += line
	}
	return result
}
