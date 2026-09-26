package vbscript

import "testing"

func TestSmallestContainingNodeSelectsNestedSpan(t *testing.T) {
	outer := &CSTNode{Kind: "Procedure", Start: 0, End: 100}
	middle := &CSTNode{Kind: "Procedure", Start: 10, End: 90}
	inner := &CSTNode{Kind: "Procedure", Start: 20, End: 30}
	lookup := BuildNodeLookup([]*CSTNode{outer, middle, inner})

	if got := SmallestContainingNode(lookup, 25); got != inner {
		t.Fatalf("offset 25 = %#v", got)
	}
	if got := SmallestContainingNode(lookup, 50); got != middle {
		t.Fatalf("offset 50 = %#v", got)
	}
	if got := SmallestContainingNode(lookup, 5); got != outer {
		t.Fatalf("offset 5 = %#v", got)
	}
	if got := SmallestContainingNode(lookup, 101); got != nil {
		t.Fatalf("offset 101 = %#v", got)
	}
}

func TestSmallestContainingNodeTieBreaksByPreOrder(t *testing.T) {
	first := &CSTNode{Kind: "Procedure", Start: 0, End: 10}
	second := &CSTNode{Kind: "Property", Start: 0, End: 10}
	lookup := BuildNodeLookup([]*CSTNode{first, second})

	if got := SmallestContainingNode(lookup, 5); got != first {
		t.Fatalf("expected first same-span node, got %#v", got)
	}
}
