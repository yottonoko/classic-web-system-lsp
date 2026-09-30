package lspserver

import (
	"fmt"
	"testing"
)

func TestVBProcedureScopeIndexMatchesLinearLookup(t *testing.T) {
	scope := func(name string, start, end int) vbProcedureScope {
		return vbProcedureScope{Name: name, StartOffset: start, EndOffset: end}
	}
	cases := [][]vbProcedureScope{
		nil,
		{scope("a", 10, 20), scope("b", 30, 40), scope("zero", 0, 0), scope("c", 41, 50)},
		{scope("late", 60, 70), scope("early", 5, 15)},
		{scope("outer", 0, 100), scope("inner", 20, 30)},
		{scope("left", 10, 20), scope("right", 20, 30)},
		{scope("inverted", 30, 10), scope("ok", 40, 45)},
	}
	for index, scopes := range cases {
		lookup := newVBProcedureScopeIndex(scopes)
		for offset := -1; offset <= 110; offset++ {
			want := vbProcedureScopeAtOffset(scopes, offset)
			if got := lookup.at(offset); got != want {
				t.Fatalf("%s offset %d: got %q, want %q", fmt.Sprint("case ", index), offset, got, want)
			}
		}
	}
}
