package vbscript

import (
	"strconv"
	"strings"
	"testing"
	"unsafe"
)

func TestCSTEstimateBytesDeduplicatesTokenAliases(t *testing.T) {
	tokens := []Token{{
		Kind: strings.Clone("identifier"),
		Text: strings.Clone("shared-value"),
	}}
	nameToken := &tokens[0]

	base := &CSTNode{
		Kind:      "Statement",
		NameToken: nameToken,
		Tokens:    tokens,
		Statement: &CSTStatement{Kind: CSTStatementExecutable, Role: CSTStatementRoleExecutable, Tokens: tokens},
	}
	aliases := &CSTNode{
		Kind:      "Statement",
		NameToken: nameToken,
		Tokens:    tokens,
		Statement: &CSTStatement{
			Kind:   CSTStatementExecutable,
			Role:   CSTStatementRoleExecutable,
			Tokens: tokens,
			Parts: CSTStatementParts{
				Condition:     tokens,
				Selector:      tokens,
				CaseValues:    tokens,
				LoopCondition: tokens,
				InlineThen:    tokens,
				InlineElse:    tokens,
				Callee:        tokens,
			},
		},
	}

	if got, want := aliases.EstimateBytes(), base.EstimateBytes(); got != want {
		t.Fatalf("aliased token fields estimate = %d, base estimate = %d; aliases added retained storage", got, want)
	}
	if got := aliases.EstimateBytes(); got <= 0 {
		t.Fatalf("aliased CST estimate = %d, want nonzero", got)
	}
}

func TestCSTEstimateBytesChargesDistinctTokenBackingAndStrings(t *testing.T) {
	sharedKind := strings.Clone("identifier")
	sharedText := strings.Clone("shared-value")
	originalTokens := []Token{{Kind: sharedKind, Text: sharedText}}
	clonedTokens := append([]Token(nil), originalTokens...)
	if unsafe.SliceData(originalTokens) == unsafe.SliceData(clonedTokens) {
		t.Fatal("test token slices unexpectedly share backing")
	}

	rootWithAlias := &CSTNode{
		Tokens: originalTokens,
		Children: []*CSTNode{{
			Tokens: originalTokens,
		}},
	}
	rootWithClonedBacking := &CSTNode{
		Tokens: originalTokens,
		Children: []*CSTNode{{
			Tokens: clonedTokens,
		}},
	}

	tokenSize := int64(unsafe.Sizeof(Token{}))
	wantBackingGrowth := int64(cap(clonedTokens)) * tokenSize
	if got := rootWithClonedBacking.EstimateBytes() - rootWithAlias.EstimateBytes(); got != wantBackingGrowth {
		t.Fatalf("distinct token slice backing growth = %d, want %d", got, wantBackingGrowth)
	}

	clonedStrings := []Token{{Kind: strings.Clone(sharedKind), Text: strings.Clone(sharedText)}}
	if unsafe.StringData(clonedStrings[0].Kind) == unsafe.StringData(sharedKind) || unsafe.StringData(clonedStrings[0].Text) == unsafe.StringData(sharedText) {
		t.Fatal("test token strings unexpectedly share backing")
	}
	rootWithClonedStrings := &CSTNode{
		Tokens: originalTokens,
		Children: []*CSTNode{{
			Tokens: clonedStrings,
		}},
	}
	wantClonedStorage := wantBackingGrowth + int64(len(sharedKind)+len(sharedText))*2
	if got := rootWithClonedStrings.EstimateBytes() - rootWithAlias.EstimateBytes(); got != wantClonedStorage {
		t.Fatalf("distinct token slice and string growth = %d, want %d", got, wantClonedStorage)
	}
}

func TestCSTEstimateBytesChargesUnionOfOverlappingTokenStringRanges(t *testing.T) {
	backing := strings.Repeat("x", 32)
	first := backing[2:12]
	second := backing[7:19]
	firstEnd := uintptr(unsafe.Pointer(unsafe.StringData(first))) + uintptr(len(first))
	secondStart := uintptr(unsafe.Pointer(unsafe.StringData(second)))
	if firstEnd <= secondStart {
		t.Fatal("test token strings unexpectedly do not overlap")
	}

	got := estimateCSTTokenStringBytes(Token{Kind: first, Text: second})
	want := int64(19-2) * 2
	if got != want {
		t.Fatalf("overlapping token string storage = %d, want union size %d", got, want)
	}
}

func TestCSTEstimateBytesDeduplicatesExactTokenStringAliases(t *testing.T) {
	shared := strings.Repeat("alias", 4)
	got := estimateCSTTokenStringBytes(Token{Kind: shared, Text: shared})
	want := int64(len(shared)) * 2
	if got != want {
		t.Fatalf("exact token string alias storage = %d, want %d", got, want)
	}
}

func TestCSTEstimateBytesChargesAdjacentTokenStringRanges(t *testing.T) {
	backing := strings.Repeat("x", 32)
	first := backing[:9]
	second := backing[9:21]
	firstEnd := uintptr(unsafe.Pointer(unsafe.StringData(first))) + uintptr(len(first))
	secondStart := uintptr(unsafe.Pointer(unsafe.StringData(second)))
	if firstEnd != secondStart {
		t.Fatal("test token strings unexpectedly are not adjacent")
	}

	got := estimateCSTTokenStringBytes(Token{Kind: first, Text: second})
	want := int64(len(first)+len(second)) * 2
	if got != want {
		t.Fatalf("adjacent token string storage = %d, want %d", got, want)
	}
}

func TestCSTEstimateBytesChargesEqualClonedTokenStringsSeparately(t *testing.T) {
	original := strings.Clone("distinct-string-backing")
	clone := strings.Clone(original)
	if unsafe.StringData(original) == unsafe.StringData(clone) {
		t.Fatal("test token strings unexpectedly share backing")
	}

	got := estimateCSTTokenStringBytes(Token{Kind: original, Text: clone})
	want := int64(len(original)+len(clone)) * 2
	if got != want {
		t.Fatalf("equal cloned token string storage = %d, want %d", got, want)
	}
}

func estimateCSTTokenStringBytes(token Token) int64 {
	tokens := []Token{token}
	node := &CSTNode{Tokens: tokens}
	withStrings := node.EstimateBytes()
	tokens[0].Kind = ""
	tokens[0].Text = ""
	return withStrings - node.EstimateBytes()
}

func TestCSTEstimateBytesParsedTreeScalesWithInput(t *testing.T) {
	small := ParseCST("value = 1")
	large := ParseCST(strings.Repeat("value = 1\n", 64))
	if small.EstimateBytes() <= 0 || large.EstimateBytes() <= 0 {
		t.Fatalf("parsed CST estimates = small(%d), large(%d); want nonzero", small.EstimateBytes(), large.EstimateBytes())
	}
	if large.EstimateBytes() <= small.EstimateBytes() {
		t.Fatalf("large parsed CST estimate = %d, small estimate = %d; want growth", large.EstimateBytes(), small.EstimateBytes())
	}
}

func TestCSTEstimateBytes5000DeclarationsAllocationBudget(t *testing.T) {
	node := cstEstimateNodeWithDeclarations(5_000)
	var estimate int64
	allocations := testing.AllocsPerRun(5, func() {
		estimate = node.EstimateBytes()
	})
	if estimate == 0 {
		t.Fatal("CST estimate = 0, want nonzero")
	}
	if allocations > 1_000 {
		t.Fatalf("CST estimate allocations = %.0f, want at most 1000", allocations)
	}
}

func cstEstimateNodeWithDeclarations(declarations int) *CSTNode {
	var source strings.Builder
	source.Grow(declarations * 16)
	for index := 0; index < declarations; index++ {
		source.WriteString("Dim value")
		source.WriteString(strconv.Itoa(index))
		source.WriteByte('\n')
	}
	return ParseCST(source.String())
}

func BenchmarkCSTEstimateBytes5000Declarations(b *testing.B) {
	node := cstEstimateNodeWithDeclarations(5_000)
	b.ReportAllocs()
	b.ResetTimer()
	var estimate int64
	for range b.N {
		estimate = node.EstimateBytes()
	}
	if estimate == 0 {
		b.Fatal("CST estimate = 0, want nonzero")
	}
}
