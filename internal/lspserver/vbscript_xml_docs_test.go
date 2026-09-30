package lspserver

import (
	"reflect"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

func TestCommentLinesBeforeLineMatchesFullSplit(t *testing.T) {
	sources := []string{
		"<%\n''' <summary>Adds.</summary>\n''' <param name=\"a\">A</param>\nFunction Add(a)\nEnd Function\n%>",
		"<%\r\n' plain one\r\n  ' plain two\r\nSub S()\r\nEnd Sub\r\n%>",
		"<%\nDim x\n' orphan\n\nSub S()\nEnd Sub %>",
		"''' <summary>First line</summary>\nSub S()\nEnd Sub",
		"<%\n' tail\n' comment",
	}
	for _, source := range sources {
		parsed := core.ParseDocument("file:///docs.asp", source, core.Settings{DefaultLanguage: "VBScript"})
		lines := strings.Split(source, "\n")
		for line := 0; line <= len(lines)+1; line++ {
			want := referenceXMLDocBeforeLine(lines, line)
			if got := vbscriptXMLDocBeforeLine(parsed, line); !reflect.DeepEqual(got, want) {
				t.Fatalf("source %q line %d: got %#v, want %#v", source, line, got, want)
			}
		}
	}
}

func referenceXMLDocBeforeLine(lines []string, line int) vbscriptXMLDoc {
	if block := xmlDocBlockBeforeLine(lines, line); len(block) > 0 {
		return parseVBScriptXMLDoc(strings.Join(block, "\n"))
	}
	if plain := plainDocBlockBeforeLine(lines, line); len(plain) > 0 {
		return parseVBScriptPlainDoc(plain)
	}
	return vbscriptXMLDoc{Params: map[string]string{}}
}
