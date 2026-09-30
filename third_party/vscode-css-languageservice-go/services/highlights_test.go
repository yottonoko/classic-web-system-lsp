package services

import (
	"reflect"
	"testing"

	"github.com/yottonoko/vscode-css-languageservice-go/lsp"
)

func TestDocumentHighlights(t *testing.T) {
	tests := []struct {
		name            string
		input           string
		marker          string
		expectedMatches int
		expectedWrites  int
		expectedText    string
	}{
		{
			name:            "keyframes animation shorthand",
			input:           "@keyframes id {}; #main { animation: id 4s linear 0s infinite alternate; }",
			marker:          "id",
			expectedMatches: 2,
			expectedWrites:  1,
			expectedText:    "id",
		},
		{
			name:            "keyframes animation-name ignores unrelated values",
			input:           "@keyframes id {}; #main { animation-name: id; foo: id;}",
			marker:          "id",
			expectedMatches: 2,
			expectedWrites:  1,
			expectedText:    "id",
		},
		{
			name:            "root variable used in rule",
			input:           ".a{ background: const(--var1); } :root{ --var1: abc;}",
			marker:          "--var1",
			expectedMatches: 2,
			expectedWrites:  1,
			expectedText:    "--var1",
		},
		{
			name:            "local variable used in other rule",
			input:           ".a{ background: const(--var1); } :b{ --var1: abc;}",
			marker:          "--var1",
			expectedMatches: 2,
			expectedWrites:  1,
			expectedText:    "--var1",
		},
		{
			name:            "property",
			input:           "body { display: inline } #foo { display: inline }",
			marker:          "display",
			expectedMatches: 2,
			expectedWrites:  0,
			expectedText:    "display",
		},
		{
			name:            "value",
			input:           "body { display: inline } #foo { display: inline }",
			marker:          "inline",
			expectedMatches: 2,
			expectedWrites:  0,
			expectedText:    "inline",
		},
		{
			name:            "selector",
			input:           "body { display: inline } #foo { display: inline }",
			marker:          "body",
			expectedMatches: 1,
			expectedWrites:  1,
			expectedText:    "body",
		},
		{
			name:            "comment",
			input:           "/* comment */body { display: inline } ",
			marker:          "comment",
			expectedMatches: 0,
			expectedWrites:  0,
			expectedText:    "comment",
		},
		{
			name:            "whole classname",
			input:           ".foo { }",
			marker:          ".foo",
			expectedMatches: 1,
			expectedWrites:  1,
			expectedText:    ".foo",
		},
		{
			name:            "whole classname not element suffix",
			input:           ".body { } body { }",
			marker:          ".body",
			expectedMatches: 1,
			expectedWrites:  1,
			expectedText:    ".body",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertHighlights(t, tt.input, tt.marker, tt.expectedMatches, tt.expectedWrites, tt.expectedText)
		})
	}
}

func TestCSSNavigationHighlightsPortedNamedCases(t *testing.T) {
	t.Run("mark highlights", func(t *testing.T) {
		assertHighlights(t, "@keyframes id {}; #main { animation: id 4s linear 0s infinite alternate; }", "id", 2, 1, "id")
		assertHighlights(t, "@keyframes id {}; #main { animation-name: id; foo: id;}", "id", 2, 1, "id")
	})
	t.Run("mark occurrences for variable defined in root and used in a rule", func(t *testing.T) {
		assertHighlights(t, ".a{ background: const(--var1); } :root{ --var1: abc;}", "--var1", 2, 1, "--var1")
	})
	t.Run("mark occurrences for variable defined in a rule and used in a different rule", func(t *testing.T) {
		assertHighlights(t, ".a{ background: const(--var1); } :b{ --var1: abc;}", "--var1", 2, 1, "--var1")
	})
	t.Run("mark occurrences for property", func(t *testing.T) {
		assertHighlights(t, "body { display: inline } #foo { display: inline }", "display", 2, 0, "display")
	})
	t.Run("mark occurrences for value", func(t *testing.T) {
		assertHighlights(t, "body { display: inline } #foo { display: inline }", "inline", 2, 0, "inline")
	})
	t.Run("mark occurrences for selector", func(t *testing.T) {
		assertHighlights(t, "body { display: inline } #foo { display: inline }", "body", 1, 1, "body")
	})
	t.Run("mark occurrences for comment", func(t *testing.T) {
		assertHighlights(t, "/* comment */body { display: inline } ", "comment", 0, 0, "comment")
	})
	t.Run("mark occurrences for whole classname instead of only class identifier", func(t *testing.T) {
		assertHighlights(t, ".foo { }", ".foo", 1, 1, ".foo")
		assertHighlights(t, ".body { } body { }", ".body", 1, 1, ".body")
	})
}

func TestSCSSDocumentHighlights(t *testing.T) {
	assertHighlightsWithLanguage(t, "scss", "$var1: 1; $var2: $var1;", "$var1", 2, 1, "$var1")
	assertHighlightsWithLanguage(t, "scss", "@mixin r1 { color: red; } .foo { @include r1; }", "r1", 2, 1, "r1")
	assertHighlightsWithLanguage(t, "scss", "@function r1($p1) { @return $p1; } .foo { width: r1(1); }", "r1", 2, 1, "r1")
	assertHighlightsWithLanguage(t, "scss", "@function r1($p1) { @return $p1; }", "$p1", 2, 1, "$p1")
}

func TestSCSSNavigationHighlightsPortedNamedCases(t *testing.T) {
	t.Run("mark highlights", func(t *testing.T) {
		tests := []struct {
			input           string
			marker          string
			expectedMatches int
			expectedWrites  int
			expectedText    string
		}{
			{"$var1: 1; $var2: /**/$var1;", "$var1", 2, 1, "$var1"},
			{"$var1: 1; ls { $var2: /**/$var1; }", "/**/", 2, 1, "$var1"},
			{"r1 { $var1: 1; p1: $var1;} r2,r3 { $var1: 1; p1: /**/$var1 + $var1;}", "/**/", 3, 1, "$var1"},
			{".r1 { r1: 1em; } r2 { r1: 2em; @extend /**/.r1;}", "/**/", 2, 1, ".r1"},
			{"/**/%r1 { r1: 1em; } r2 { r1: 2em; @extend %r1;}", "/**/", 2, 1, "%r1"},
			{"@mixin r1 { r1: $p1; } r2 { r2: 2em; @include /**/r1; }", "/**/", 2, 1, "r1"},
			{"@mixin r1($p1) { r1: $p1; } r2 { r2: 2em; @include /**/r1(2px); }", "/**/", 2, 1, "r1"},
			{"$p1: 1; @mixin r1($p1: $p1) { r1: $p1; } r2 { r2: 2em; @include /**/r1; }", "/**/", 2, 1, "r1"},
			{"/**/$p1: 1; @mixin r1($p1: $p1) { r1: $p1; }", "/**/", 2, 1, "$p1"},
			{"$p1 : 1; @mixin r1($p1) { r1: /**/$p1; }", "/**/", 2, 1, "$p1"},
			{"/**/$p1 : 1; @mixin r1($p1) { r1: $p1; }", "/**/", 1, 1, "$p1"},
			{"$p1 : 1; @mixin r1(/**/$p1) { r1: $p1; }", "/**/", 2, 1, "$p1"},
			{"$p1 : 1; @function r1($p1, $p2: /**/$p1) { @return $p1 + $p1 + $p2; }", "/**/", 2, 1, "$p1"},
			{"$p1 : 1; @function r1($p1, /**/$p2: $p1) { @return $p1 + $p2 + $p2; }", "/**/", 3, 1, "$p2"},
			{"@function r1($p1, $p2) { @return $p1 + $p2; } @function r2() { @return /**/r1(1, 2); }", "/**/", 2, 1, "r1"},
			{"@function /**/r1($p1, $p2) { @return $p1 + $p2; } @function r2() { @return r1(1, 2); } ls { x: r2(); }", "/**/", 2, 1, "r1"},
			{"@function r1($p1, $p2) { @return $p1 + $p2; } @function r2() { @return r1(/**/$p1 : 1, $p2 : 2); } ls { x: r2(); }", "/**/", 3, 1, "$p1"},
			{"@mixin /*here*/foo { display: inline } foo { @include foo; }", "/*here*/", 2, 1, "foo"},
			{"@mixin foo { display: inline } foo { @include /*here*/foo; }", "/*here*/", 2, 1, "foo"},
			{"@mixin foo { display: inline } /*here*/foo { @include foo; }", "/*here*/", 1, 1, "foo"},
			{"@function /*here*/foo($i) { @return $i*$i; } #foo { width: foo(2); }", "/*here*/", 2, 1, "foo"},
			{"@function foo($i) { @return $i*$i; } #foo { width: /*here*/foo(2); }", "/*here*/", 2, 1, "foo"},
			{".text { @include mixins.responsive using ($multiplier) { font-size: /*here*/$multiplier * 10px; } }", "/*here*/$", 2, 1, "$multiplier"},
		}
		for _, tt := range tests {
			assertHighlightsWithLanguage(t, "scss", tt.input, tt.marker, tt.expectedMatches, tt.expectedWrites, tt.expectedText)
		}
	})
}

func TestLESSDocumentHighlights(t *testing.T) {
	assertHighlightsWithLanguage(t, "less", "@var1: 1; @var2: /**/@var1;", "/**/", 2, 1, "@var1")
	assertHighlightsWithLanguage(t, "less", "@var1: 1; ls { @var2: /**/@var1; }", "/**/", 2, 1, "@var1")
	assertHighlightsWithLanguage(t, "less", ".r1(@p1) { r1: @p1; } r2 { r1: 2em; /**/.r1(2px); }", "/**/", 2, 1, ".r1")
	assertHighlightsWithLanguage(t, "less", "/**/.r1(@p1) { r1: @p1; } r2 { r1: 2em; .r1(2px); }", "/**/", 2, 1, ".r1")
}

func TestLESSNavigationHighlightsPortedNamedCases(t *testing.T) {
	t.Run("mark highlights", func(t *testing.T) {
		assertHighlightsWithLanguage(t, "less", "@var1: 1; @var2: /**/@var1;", "/**/", 2, 1, "@var1")
		assertHighlightsWithLanguage(t, "less", "@var1: 1; ls { @var2: /**/@var1; }", "/**/", 2, 1, "@var1")
		assertHighlightsWithLanguage(t, "less", "r1 { @var1: 1; p1: @var1;} r2,r3 { @var1: 1; p1: /**/@var1 + @var1;}", "/**/", 3, 1, "@var1")
		assertHighlightsWithLanguage(t, "less", ".r1 { r1: 1em; } r2 { r1: 2em; /**/.r1;}", "/**/", 2, 1, ".r1")
		assertHighlightsWithLanguage(t, "less", ".r1(@p1) { r1: @p1; } r2 { r1: 2em; /**/.r1(2px); }", "/**/", 2, 1, ".r1")
		assertHighlightsWithLanguage(t, "less", "/**/.r1(@p1) { r1: @p1; } r2 { r1: 2em; .r1(2px); }", "/**/", 2, 1, ".r1")
		assertHighlightsWithLanguage(t, "less", "@p1 : 1; .r1(@p1) { r1: /**/@p1; }", "/**/", 2, 1, "@p1")
		assertHighlightsWithLanguage(t, "less", "/**/@p1 : 1; .r1(@p1) { r1: @p1; }", "/**/", 1, 1, "@p1")
		assertHighlightsWithLanguage(t, "less", "@p1 : 1; .r1(/**/@p1) { r1: @p1; }", "/**/", 2, 1, "@p1")
	})
}

func TestPrepareRenameAndRename(t *testing.T) {
	document := highlightDocument("body { display: inline } #foo { display: inline }")
	position := document.PositionAt(stringsIndex(document.Text(), "display") + len("display"))

	r := PrepareRename(document, position)
	if r == nil || document.GetText(r) != "display" {
		t.Fatalf("prepare rename = %#v", r)
	}
	edit := Rename(document, position, "visibility")
	changes := edit.Changes[document.URI]
	if got := lsp.ApplyEdits(document, changes); got != "body { visibility: inline } #foo { visibility: inline }" {
		t.Fatalf("renamed = %q", got)
	}
}

func TestSCSSPrepareRenameAndRename(t *testing.T) {
	document := lsp.NewTextDocument("test://test/test.scss", "scss", 0, "$var1: 1; $var2: $var1;")
	position := document.PositionAt(stringsIndex(document.Text(), "$var1") + len("$var1"))

	r := PrepareRename(document, position)
	if r == nil || document.GetText(r) != "$var1" {
		t.Fatalf("prepare rename = %#v", r)
	}
	edit := Rename(document, position, "$renamed")
	changes := edit.Changes[document.URI]
	if got := lsp.ApplyEdits(document, changes); got != "$renamed: 1; $var2: $renamed;" {
		t.Fatalf("renamed = %q", got)
	}
}

func assertHighlights(t *testing.T, input, marker string, expectedMatches, expectedWrites int, expectedText string) {
	t.Helper()
	assertHighlightsWithLanguage(t, "css", input, marker, expectedMatches, expectedWrites, expectedText)
}

func assertHighlightsWithLanguage(t *testing.T, languageID, input, marker string, expectedMatches, expectedWrites int, expectedText string) {
	t.Helper()
	document := lsp.NewTextDocument(lsp.DocumentURI("test://test/test."+languageID), languageID, 0, input)
	position := document.PositionAt(stringsIndex(input, marker) + len(marker))
	highlights := FindDocumentHighlights(document, position)
	if len(highlights) != expectedMatches {
		t.Fatalf("%s\nhighlight count = %d, want %d: %#v", input, len(highlights), expectedMatches, highlights)
	}
	writes := 0
	for _, highlight := range highlights {
		if highlight.Kind == lsp.DocumentHighlightKindWrite {
			writes++
		}
		if text := document.GetText(&highlight.Range); text != expectedText {
			t.Fatalf("highlight text = %q, want %q", text, expectedText)
		}
	}
	if writes != expectedWrites {
		t.Fatalf("%s\nwrites = %d, want %d: %#v", input, writes, expectedWrites, highlights)
	}
}

func TestHighlightRangesAreStable(t *testing.T) {
	document := highlightDocument("body { display: inline } #foo { display: inline }")
	position := document.PositionAt(stringsIndex(document.Text(), "inline") + len("inline"))
	actual := FindDocumentHighlights(document, position)
	expected := []lsp.DocumentHighlight{
		{Range: offsetRange(16, 22), Kind: lsp.DocumentHighlightKindRead},
		{Range: offsetRange(41, 47), Kind: lsp.DocumentHighlightKindRead},
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("actual: %#v\nwant:   %#v", actual, expected)
	}
}

func highlightDocument(input string) *lsp.TextDocument {
	return lsp.NewTextDocument("test://test/test.css", "css", 0, input)
}

func TestHighlightTokenCacheStaysBounded(t *testing.T) {
	for i := 0; i < highlightTokenCacheLimit*4; i++ {
		document := lsp.NewTextDocument("file:///cache.css", "css", i, ".a { color: red; }")
		if len(highlightTokensForDocument(document)) == 0 {
			t.Fatal("expected highlight tokens")
		}
	}
	highlightTokenCache.Lock()
	defer highlightTokenCache.Unlock()
	if len(highlightTokenCache.entries) > highlightTokenCacheLimit || len(highlightTokenCache.order) != len(highlightTokenCache.entries) {
		t.Fatalf("cache holds %d entries and %d order slots, want at most %d", len(highlightTokenCache.entries), len(highlightTokenCache.order), highlightTokenCacheLimit)
	}
}
