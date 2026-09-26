package vbscript

import (
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestCompletionsForResponseMember(t *testing.T) {
	items := CompletionsFor("<%\nResponse.Wr", len("<%\nResponse.Wr"))
	if !hasCompletion(items, "Write") {
		t.Fatalf("expected Response member completion Write, got %#v", items)
	}
	if hasCompletion(items, "Response") {
		t.Fatalf("member completion should not include top-level Response item")
	}
}

func TestCompletionsForTopLevel(t *testing.T) {
	items := CompletionsFor("<%\nResp", len("<%\nResp"))
	if !hasCompletion(items, "Response") {
		t.Fatalf("expected top-level Response completion, got %#v", items)
	}
}

func TestStandaloneCompletionsUseWScriptGlobals(t *testing.T) {
	items := CompletionsForOptions("WScript.", len("WScript."), CompletionOptions{Standalone: true})
	if !hasCompletion(items, "Echo") {
		t.Fatalf("expected WScript member completion Echo, got %#v", items)
	}

	topLevel := CompletionsForOptions("", 0, CompletionOptions{Standalone: true})
	if !hasCompletion(topLevel, "WScript") {
		t.Fatalf("expected standalone top-level WScript completion, got %#v", topLevel)
	}
	if hasCompletion(topLevel, "Response") {
		t.Fatalf("standalone top-level completions should not include ASP Response: %#v", topLevel)
	}
}

func TestSyntaxSnippetCompletionsIncludeClassicVBScriptBlocks(t *testing.T) {
	items := CompletionsFor("<%\n", len("<%\n"))
	for _, label := range []string{
		"If Then",
		"If Then Else",
		"Do Loop",
		"Do While Loop",
		"Do Until Loop",
		"Do Loop While",
		"Do Loop Until",
		"For Next",
		"For Each Next",
		"Select Case",
		"With",
		"Sub",
		"Function",
		"Class",
		"Property Get",
		"Property Let",
		"Property Set",
	} {
		item, ok := completionByLabel(items, label)
		if !ok || item.Kind != lsp.CompletionItemKindSnippet || item.InsertTextFormat != 2 {
			t.Fatalf("snippet %s = %#v ok=%v", label, item, ok)
		}
	}

	disabled := CompletionsForOptions("<%\n", len("<%\n"), CompletionOptions{SyntaxSnippets: false})
	if hasCompletion(disabled, "If Then") {
		t.Fatalf("snippet-disabled completions include If Then: %#v", disabled)
	}
	if !hasCompletion(disabled, "If") || !hasCompletion(disabled, "Response") {
		t.Fatalf("snippet-disabled completions missing keywords/built-ins: %#v", disabled)
	}
}

func TestSyntaxSnippetCompletionsStayAvailableWhileTypingPrefixes(t *testing.T) {
	for _, testCase := range []struct {
		text  string
		label string
	}{
		{"<%\nDo", "Do Loop"},
		{"<%\nSub", "Sub"},
		{"<%\nFunct", "Function"},
	} {
		items := CompletionsFor(testCase.text, len(testCase.text))
		item, ok := completionByLabel(items, testCase.label)
		if !ok || item.Kind != lsp.CompletionItemKindSnippet {
			t.Fatalf("%s completion %s = %#v ok=%v", testCase.text, testCase.label, item, ok)
		}
	}
}

func TestBlockContextCompletionsReturnMatchingClosersAndContinuations(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		text   string
		labels []string
	}{
		{"then", "<%\nIf ready ", []string{"Then"}},
		{"if", "<%\nIf ready Then\ne", []string{"ElseIf", "Else", "End", "End If"}},
		{"select", "<%\nSelect Case value\nc", []string{"Case"}},
		{"loop", "<%\nDo\nlo", []string{"Loop"}},
		{"wend", "<%\nWhile ready\nwe", []string{"Wend"}},
		{"next", "<%\nFor index = 1 To 3\nn", []string{"Next"}},
		{"function", "<%\nFunction Render()\nend f", []string{"End Function"}},
		{"sub", "<%\nSub Render()\nend s", []string{"End Sub"}},
		{"class", "<%\nClass Widget\nend c", []string{"End Class"}},
		{"property", "<%\nClass Widget\nProperty Get Name()\nend p", []string{"End Property"}},
	} {
		items := CompletionsFor(testCase.text, len(testCase.text))
		for _, label := range testCase.labels {
			if !hasCompletion(items, label) {
				t.Fatalf("%s completions missing %s: %#v", testCase.name, label, items)
			}
		}
	}
}

func TestHoverAtResponseWrite(t *testing.T) {
	source := "<%\nResponse.Write CStr(value)\n%>"
	hover := HoverAt(source, strings.Index(source, "Write"))
	if hover == nil {
		t.Fatalf("expected Response.Write hover, got %#v", hover)
	}
	content, _ := hover.Contents.(lsp.MarkupContent)
	if !strings.Contains(content.Value, "Response.Write") {
		t.Fatalf("expected Response.Write hover content, got %#v", hover)
	}
}

func completionByLabel(items []lsp.CompletionItem, label string) (lsp.CompletionItem, bool) {
	for _, item := range items {
		if item.Label == label {
			return item, true
		}
	}
	return lsp.CompletionItem{}, false
}

func hasCompletion(items []lsp.CompletionItem, label string) bool {
	for _, item := range items {
		if item.Label == label {
			return true
		}
	}
	return false
}
