package lspserver

import (
	"math/rand/v2"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

func TestVBFoldNameIndexMatchesEqualFoldScan(t *testing.T) {
	declarations := []vbUsageDeclaration{
		{Name: "items"}, {Name: "Kount"}, {Name: "ITEMS"}, {Name: "Kount"}, {Name: "ſtatus"}, {Name: "顧客"}, {Name: "count"}, {Name: "Items"},
	}
	parsed := &core.ParsedDocument{URI: "file:///names.asp"}
	const key = "test.fold-names"
	for _, name := range []string{"items", "kount", "KOUNT", "status", "Kount", "ſtatus", "顧客", "missing", ""} {
		var want []int
		for index, declaration := range declarations {
			if strings.EqualFold(declaration.Name, name) {
				want = append(want, index)
			}
		}
		candidates := slices.Collect(vbFoldNameIndexFor(parsed, key, declarations, vbUsageDeclarationName).candidates(name))
		if !slices.IsSorted(candidates) {
			t.Fatalf("candidates for %q are not ascending: %v", name, candidates)
		}
		var got []int
		for _, index := range candidates {
			if strings.EqualFold(declarations[index].Name, name) {
				got = append(got, index)
			}
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("indexes for %q = %v, want %v", name, got, want)
		}
	}
	if got := slices.Collect(vbFoldNameIndexFor(parsed, key, declarations[:3], vbUsageDeclarationName).candidates("items")); !reflect.DeepEqual(got, []int{0, 2}) {
		t.Fatalf("a shorter slice reused a stale index: %v", got)
	}
}

func TestVBScriptNameBindingUsesIndexedLookups(t *testing.T) {
	source := `<%
Class Cart
  Private items
  Public Function Count()
    Count = items
  End Function
End Class
Function Build(value)
  Dim total
  total = value
  later = 1
  Build = total
End Function
helper = 2
%>`
	parsed := core.ParseDocument("file:///binding.asp", source, core.Settings{})
	scopeOf := func(marker string) string {
		return vbscriptScopeAtOffset(parsed, strings.Index(source, marker))
	}
	buildScope := scopeOf("total = value")
	countScope := scopeOf("Count = items")
	cases := []struct {
		name, scope string
		offset      int
		want        bool
	}{
		{"TOTAL", buildScope, len(source), true},
		{"Value", buildScope, len(source), true},
		{"LATER", "", len(source), true},
		{"later", "", strings.Index(source, "later") - 1, false},
		{"helper", buildScope, len(source), false},
		{"Items", countScope, len(source), true},
		{"missing", buildScope, len(source), false},
	}
	for _, test := range cases {
		if got := vbscriptNameBoundInScopeAtOffsetUncached(parsed, test.name, test.scope, test.offset); got != test.want {
			t.Fatalf("bound(%q, %q, %d) = %v, want %v", test.name, test.scope, test.offset, got, test.want)
		}
	}
	if got := vbscriptClassScopeForProcedure(parsed, countScope); !strings.EqualFold(got, "Cart") {
		t.Fatalf("class scope for %q = %q, want Cart", countScope, got)
	}
}

func TestFoldKeyMatchesEqualFold(t *testing.T) {
	values := []string{
		"", "a", "A", "k", "K", "K", "s", "S", "ſ", "ß", "ẞ", "Σ", "σ", "ς", "file:///Shared.inc", "FILE:///shared.INC",
		"file:///ſhared.inc", "顧客", "\xff", "\xfe", "\xffa", "İ", "i", "ı", "ǅ", "ǆ", "Ǆ", "ω", "Ω", "Ω",
	}
	for _, left := range values {
		for _, right := range values {
			if got, want := foldKey(left) == foldKey(right), strings.EqualFold(left, right); got != want {
				t.Fatalf("foldKey(%q) == foldKey(%q) is %v, EqualFold is %v", left, right, got, want)
			}
		}
	}
}

func TestFirstAssignmentValueAfterInDocumentMatchesLineScan(t *testing.T) {
	fragments := []string{
		"Dim a\n", "a = 1\n", "Set a = Nothing\r\n", "Let b = \"x: y\"\n", "b(1) = 2\n", "a.b = 3\n", "c = 4: a = 5\n",
		"If a = 1 Then b = 2\n", "' a = 9\n", "x = (a: b)\n", "Dim c: c = Request(\"c\")\n", "A = \"upper\"\n", "\n", "\r\n",
		"For k = 0 To 3\n", "set = 1\n", "Function F(a, b)\n", "  F = a\n", "ſ = 1\n", "a=\n",
	}
	random := rand.New(rand.NewPCG(3, 4))
	for range 300 {
		var text strings.Builder
		for range 1 + random.IntN(12) {
			text.WriteString(fragments[random.IntN(len(fragments))])
		}
		parsed := &core.ParsedDocument{URI: "file:///assign.asp", Text: text.String()}
		for offset := 0; offset <= len(parsed.Text); offset++ {
			for _, name := range []string{"a", "A", "b", "c", "k", "F", "set", "ſ", "missing"} {
				wantValue, wantOK := firstAssignmentValueAfter(parsed.Text, name, offset)
				gotValue, gotOK := firstAssignmentValueAfterInDocument(parsed, name, offset)
				if gotValue != wantValue || gotOK != wantOK {
					t.Fatalf("value of %q after %d in %q = %q, %v; want %q, %v", name, offset, parsed.Text, gotValue, gotOK, wantValue, wantOK)
				}
			}
		}
	}
}
