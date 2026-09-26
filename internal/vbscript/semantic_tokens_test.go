package vbscript

import (
	"fmt"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

func TestClassScopeIndexMatchesPreviousLookup(t *testing.T) {
	text := "<%\nſſſ Class Sample\n  Dim value\n  note = \"end class and class nested \"\nEnd Class ' class trailing\nDim outside\n' CLASS comment\nクラス = 1\n%>"
	index := newClassScopeIndex(text)
	for offset := 0; offset <= len(text); offset++ {
		if got, want := index.contains(offset), previousInClassScopeAt(text, offset); got != want {
			t.Fatalf("class scope mismatch at byte offset %d: got %t, want %t", offset, got, want)
		}
	}
}

func TestSemanticTokensClassScope(t *testing.T) {
	const source = `<%
Dim globalValue
Class Sample
  Private Dim privateValue
  Public Dim publicValue

  Public Sub Update()
    privateValue = publicValue
  End Sub
End Class
globalValue = 1
%>`
	parsed := core.ParseDocument("file:///tmp/class-scope.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	tokens := decodeSemantic(SemanticTokens(parsed).Data)

	assertSemanticTokenAtText(t, tokens, source, "globalValue", 0, semanticVariable, 0)
	assertSemanticTokenAtText(t, tokens, source, "privateValue", 0, semanticProperty, semanticPrivate)
	assertSemanticTokenAtText(t, tokens, source, "publicValue", 0, semanticProperty, semanticPublic)
	assertSemanticTokenAtText(t, tokens, source, "Update", 0, semanticMethod, semanticPublic)
	assertSemanticTokenAtText(t, tokens, source, "globalValue", 1, semanticVariable, 0)
}

func TestSemanticTokensKeepQualifiedMembersSeparateFromGlobals(t *testing.T) {
	const source = `<%
Function ABC()
End Function
Dim A
A.ABC()
value = A.ABC
ABC()
%>`
	parsed := core.ParseDocument("file:///tmp/qualified-members.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	tokens := decodeSemantic(SemanticTokens(parsed).Data)

	assertSemanticTokenAtText(t, tokens, source, "ABC", 0, semanticFunction, 0)
	assertSemanticTokenAtText(t, tokens, source, "ABC", 1, semanticMethod, 0)
	assertSemanticTokenAtText(t, tokens, source, "ABC", 2, semanticProperty, 0)
	assertSemanticTokenAtText(t, tokens, source, "ABC", 3, semanticFunction, 0)
}

func TestSemanticTokensPreferQualifiedMembersOverParameterNames(t *testing.T) {
	const source = `<%
Sub UseValue(ABC)
  A.ABC()
  value = A.ABC
End Sub
%>`
	parsed := core.ParseDocument("file:///tmp/qualified-parameter-collision.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	tokens := decodeSemantic(SemanticTokens(parsed).Data)

	assertSemanticTokenAtText(t, tokens, source, "ABC", 0, semanticParameter, semanticByref)
	assertSemanticTokenAtText(t, tokens, source, "ABC", 1, semanticMethod, 0)
	assertSemanticTokenAtText(t, tokens, source, "ABC", 2, semanticProperty, 0)
}

func BenchmarkSemanticTokensClassScope(b *testing.B) {
	var source strings.Builder
	source.WriteString("<%\nClass LargeClass\n")
	for i := 0; i < 500; i++ {
		fmt.Fprintf(&source, "  Private Dim value%d\n  value%d = value%d + 1\n", i, i, i)
	}
	source.WriteString("End Class\n%>")
	parsed := core.ParseDocument("file:///tmp/semantic-tokens-class-scope.asp", source.String(), core.Settings{DefaultLanguage: "VBScript"})

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		semanticTokens(parsed, nil, nil, true)
	}
}

func previousInClassScopeAt(text string, start int) bool {
	prefix := strings.ToLower(text[:start])
	lastClass := strings.LastIndex(prefix, "class ")
	if lastClass < 0 {
		return false
	}
	lastEndClass := strings.LastIndex(prefix, "end class")
	return lastEndClass < lastClass
}

func assertSemanticTokenAtText(t *testing.T, tokens []semanticToken, source, name string, occurrence, tokenType, modifiers int) {
	t.Helper()
	offset := -1
	remaining := source
	base := 0
	for i := 0; i <= occurrence; i++ {
		found := strings.Index(remaining, name)
		if found < 0 {
			t.Fatalf("identifier %q occurrence %d not found", name, occurrence)
		}
		offset = base + found
		base = offset + len(name)
		remaining = source[base:]
	}
	doc := core.NewTextDocument("file:///tmp/class-scope.asp", "classic-asp", 0, source)
	position := doc.PositionAt(offset)
	if !hasSemanticToken(tokens, position.Line, position.Character, tokenType, modifiers) {
		t.Fatalf("semantic token for %q occurrence %d missing at %#v: %#v", name, occurrence, position, tokens)
	}
}
