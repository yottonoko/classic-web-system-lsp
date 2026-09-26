package services

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/yottonoko/vscode-css-languageservice-go/lsp"
)

func TestParallelBlockMapPreservesOrder(t *testing.T) {
	blocks := make([]cssBlock, 20)
	for i := range blocks {
		blocks[i] = cssBlock{start: i}
	}
	got := parallelBlockMap(blocks, 4, func(chunk []cssBlock) []int {
		values := make([]int, len(chunk))
		for i, block := range chunk {
			values[i] = block.start
		}
		return values
	})
	for i, value := range got {
		if value != i {
			t.Fatalf("parallelBlockMap order[%d] = %d, want %d", i, value, i)
		}
	}
}

func TestParallelThresholds(t *testing.T) {
	if shouldParallelize(".a { color: red; }", nil) {
		t.Fatal("small input should not use parallel path")
	}
	blocks := make([]cssBlock, parallelBlockThreshold)
	if !shouldParallelize("", blocks) {
		t.Fatal("block threshold input should use parallel path")
	}
	if shouldParallelizeBlockWork("", blocks) {
		t.Fatal("block work should use the larger work threshold")
	}
	if !shouldParallelize(strings.Repeat("a", parallelTextThreshold), nil) {
		t.Fatal("text threshold input should use parallel path")
	}
}

func TestParallelCollectorsMatchSequentialCollectors(t *testing.T) {
	text := parallelCollectorCSS(parallelBlockWorkBlockThreshold)
	document := lsp.NewTextDocument("file:///parallel.scss", "scss", 0, text)
	blocks := parseCSSBlocks(text)
	if !shouldParallelizeBlockWork(text, blocks) {
		t.Fatalf("test fixture did not cross parallel threshold: text=%d blocks=%d", len(text), len(blocks))
	}

	if got, want := documentSymbolEntriesForBlocks(text, "scss", blocks), documentSymbolEntriesForBlockRange(text, "scss", blocks); !reflect.DeepEqual(got, want) {
		t.Fatalf("parallel symbol entries differ from sequential entries")
	}
	if got, want := highlightTokensForBlocks(text, "scss", blocks), highlightTokensForBlockRange(text, "scss", blocks); !reflect.DeepEqual(got, want) {
		t.Fatalf("parallel highlight tokens differ from sequential tokens")
	}
	if got, want := namedColorsInBlocks(document, blocks), namedColorsInBlockRange(document, blocks); !reflect.DeepEqual(got, want) {
		t.Fatalf("parallel named colors differ from sequential colors")
	}

	variableBlocks := completionCSSBlocks(text, len(text))
	if got, want := collectCSSVariables(text), dedupeCSSVariables(collectCSSVariablesForBlocks(text, variableBlocks)); !reflect.DeepEqual(got, want) {
		t.Fatalf("parallel CSS variables differ from sequential variables")
	}

	builder := completionBuilder{document: document, context: completionContext{offset: len(text)}}
	if got, want := builder.collectReusedValues("color"), dedupeReusedValues(builder.collectReusedValuesForBlocks(text, "color", blocks)); !reflect.DeepEqual(got, want) {
		t.Fatalf("parallel reused values differ from sequential values")
	}
}

func parallelCollectorCSS(rules int) string {
	var b strings.Builder
	b.WriteString(":root { --accent: red; }\n")
	b.WriteString("$accent: red;\n")
	b.WriteString("@mixin tone($color: blue) { color: $color; }\n")
	for i := 0; i < rules; i++ {
		fmt.Fprintf(&b, ".card-%d {\n", i)
		fmt.Fprintf(&b, "  color: red;\n")
		fmt.Fprintf(&b, "  background: var(--accent);\n")
		fmt.Fprintf(&b, "  border-color: blue;\n")
		fmt.Fprintf(&b, "  --local-%d: green;\n", i)
		fmt.Fprintf(&b, "  @include tone($color: red);\n")
		b.WriteString("}\n")
	}
	return b.String()
}
