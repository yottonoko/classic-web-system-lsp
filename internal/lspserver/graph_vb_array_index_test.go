package lspserver

import (
	"math/rand/v2"
	"reflect"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

func TestGraphVBArrayIndexMatchesWholeTextPatterns(t *testing.T) {
	fragments := []string{
		"Dim items(3)\n", "Dim a, b(2, 3), c\n", "dim Values()\n", "ReDim Preserve values(10)\n", "REDIM items(n + 1)\n",
		"Dim x: Dim y(4)\n", "Dim z ' items(9) 顧客\n", "xDim q(1)\n", "Dim spaced\n  (5)\n", "ReDim\n Preserve\n spaced (7)\n",
		"Dim Kount(2)\n", "ReDim Preſserve count(3)\n", "Call items(1)\n", "Dim items(1)(2)\n", "Dim open(\n",
		"Dim a(1), a(2)\n", "Response.Write \"Dim items(5)\"\n", "Dim [bracketed name](2)\n", "' ReDim items(8)\n", "Dim count\n",
		"ReDim  Preserve\titems (2)\n", "Dim items (3) : ReDim items(4)\n", "ReDim items\n", "Dim a(1) ' c(2)\n", "\tDim\titems\t(5)\n",
		"ReDim Preserve Preserve(1)\n", "Dim x\r\n(9)\r\n", "ReDim count(\n",
	}
	names := []string{"items", "a", "b", "c", "Values", "values", "x", "y", "z", "q", "spaced", "count", "Count", "kount", "open", "bracketed name", "[bracketed name]", "missing", "Preserve", "preserve"}
	random := rand.New(rand.NewPCG(1, 2))
	for round := range 1000 {
		var text strings.Builder
		for range 1 + random.IntN(8) {
			text.WriteString(fragments[random.IntN(len(fragments))])
		}
		parsed := &core.ParsedDocument{URI: "file:///array.asp", Text: text.String()}
		for _, name := range names {
			wantKind, wantDimensions := graphVBArrayInfo(parsed.Text, name)
			gotKind, gotDimensions := graphVBArrayInfoForDocument(parsed, name)
			if gotKind != wantKind || !reflect.DeepEqual(gotDimensions, wantDimensions) {
				t.Fatalf("round %d name %q in %q = (%q, %#v), want (%q, %#v)", round, name, parsed.Text, gotKind, gotDimensions, wantKind, wantDimensions)
			}
		}
	}
}

func TestGraphVBArrayIndexRunsPatternsOnlyOnCandidateWindows(t *testing.T) {
	var text strings.Builder
	for index := range 2000 {
		text.WriteString("Dim value")
		text.WriteString(strings.Repeat("x", index%7))
		text.WriteString(" ' 説明\n")
	}
	text.WriteString("Dim target(4)\n")
	index := newGraphVBArrayIndex(text.String())
	if len(index.dims.windows) != 2001 || len(index.dims.folded) != 0 {
		t.Fatalf("dim windows = %d folded = %d, want 2001 and none", len(index.dims.windows), len(index.dims.folded))
	}
	if got := index.dims.byWord["target"]; len(got) != 1 {
		t.Fatalf("target windows = %v, want one", got)
	}
	if len(index.dims.byWord["valuex"]) != 0 {
		t.Fatal("a word without parentheses was indexed")
	}
	if kind, dimensions := index.info("Target"); kind != "fixed" || !reflect.DeepEqual(dimensions, []string{"4"}) {
		t.Fatalf("Target = (%q, %#v), want fixed [4]", kind, dimensions)
	}
}
