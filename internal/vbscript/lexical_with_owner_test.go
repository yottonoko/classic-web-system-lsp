package vbscript

import (
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

func TestWithOwnerIndexMatchesLinearScan(t *testing.T) {
	source := "<%\nWith a\n  .b = 1\n  With .c.d\n    .e = .f\n  End With\n  .g = 2 : With x : .y = 1 : End With\n  .h\nEnd With\n.z = 3\nWith\nEnd With\nWith q.r\n.s\n%>"
	parsed := core.ParseDocument("file:///with.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	tokens := vbscriptDocumentTokens(parsed)
	index := newWithOwnerIndex(parsed.Text, tokens)
	if name, _, _ := index.at(strings.Index(source, ".e")); name != "a.c.d" {
		t.Fatalf("owner at .e = %q, want a.c.d", name)
	}
	for offset := 0; offset <= len(source); offset++ {
		wantName, wantStart, wantEnd := linearWithOwnerAt(parsed.Text, offset, tokens)
		gotName, gotStart, gotEnd := index.at(offset)
		if gotName != wantName || gotStart != wantStart || gotEnd != wantEnd {
			t.Fatalf("offset %d: got (%q, %d, %d), want (%q, %d, %d)", offset, gotName, gotStart, gotEnd, wantName, wantStart, wantEnd)
		}
	}
}

// linearWithOwnerAt is the original full rescan used as the index oracle.
func linearWithOwnerAt(text string, offset int, tokens []Token) (string, int, int) {
	stack := make([]lexicalWithTarget, 0, 2)
	for index := 0; index < len(tokens); {
		if tokens[index].Kind == "newline" || tokens[index].Text == ":" {
			index++
			continue
		}
		if tokens[index].Start >= offset {
			break
		}
		end := cstStatementEndIndex(tokens, index)
		if end <= index {
			index++
			continue
		}
		statement := tokens[index:end]
		first := strings.ToLower(statement[0].Text)
		switch {
		case first == "with":
			if target := withTargetFromTokens(text, statement[1:], stack); target.name != "" {
				stack = append(stack, target)
			}
		case first == "end" && len(statement) > 1 && strings.EqualFold(statement[1].Text, "with"):
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
		index = end
	}
	if len(stack) == 0 {
		return "", -1, -1
	}
	target := stack[len(stack)-1]
	return target.name, target.start, target.end
}
