package lspserver

import (
	"reflect"
	"regexp"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
)

var graphMemberChainOracle = regexp.MustCompile(`\b[A-Za-z_][A-Za-z0-9_]*(?:\s*\.\s*[A-Za-z_][A-Za-z0-9_]*)+`)

func TestGraphMemberChainMatchesRegexpOracle(t *testing.T) {
	for _, text := range []string{
		"", "a", "a.b", "a . b", "a.\r\n\tb.c", "a.\vb", "1a.b", "_x.y_1.z", "a..b", "a.b.", "a.1", "a.b .1",
		"x9.y é.z obj.Item(1).Name", "Response.Write rs.Fields.Item", "'c.d\nRem e.f", "a\f.\fb", "ab.cd9e._f",
	} {
		if got, want := graphMemberChainMatches(text), graphMemberChainOracle.FindAllStringIndex(text, -1); !reflect.DeepEqual(got, want) {
			t.Fatalf("graphMemberChainMatches(%q) = %v, want %v", text, got, want)
		}
	}
}

func FuzzGraphMemberChainMatchesRegexpOracle(f *testing.F) {
	for _, seed := range []string{"a.b", "x . y.z", "1a.b", "a.\vb", "é.a.b"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		if got, want := graphMemberChainMatches(text), graphMemberChainOracle.FindAllStringIndex(text, -1); !reflect.DeepEqual(got, want) {
			t.Fatalf("graphMemberChainMatches(%q) = %v, want %v", text, got, want)
		}
	})
}

func TestVBRegionsMayDeclareProceduresMatchesDocumentCST(t *testing.T) {
	for _, test := range []struct {
		source string
		want   bool
	}{
		{source: "<% Dim value : value = 1 %><p>Function in HTML</p>", want: false},
		{source: "<%= Helper() %>", want: false},
		{source: "<%\nPUBLIC FUNCTION Helper()\nEND FUNCTION\n%>", want: true},
		{source: "<%\nprivate sub Run : end sub\n%>", want: true},
		{source: "<%\nClass C\nProperty Get Name : End Property\nEnd Class\n%>", want: true},
	} {
		parsed := core.ParseDocument("file:///procedures.asp", test.source, core.Settings{DefaultLanguage: "VBScript"})
		if got := vbRegionsMayDeclareProcedures(parsed); got != test.want {
			t.Fatalf("vbRegionsMayDeclareProcedures(%q) = %v, want %v", test.source, got, test.want)
		}
		if test.want {
			continue
		}
		var hasProcedure func(*vbscript.CSTNode) bool
		hasProcedure = func(node *vbscript.CSTNode) bool {
			if node.Kind == "Procedure" || node.Kind == "Property" {
				return true
			}
			for _, child := range node.Children {
				if hasProcedure(child) {
					return true
				}
			}
			return false
		}
		if hasProcedure(vbscript.ParseDocumentCST(parsed)) {
			t.Fatalf("document CST for %q has procedures that the precheck skipped", test.source)
		}
	}
}
