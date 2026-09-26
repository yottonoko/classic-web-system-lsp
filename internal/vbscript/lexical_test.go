package vbscript

import (
	"reflect"
	"sync"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

func TestVBScriptDocumentTokensShareRevisionAndMatchLegacyStream(t *testing.T) {
	const source = `<div>before</div>
<% Option Explicit
Dim value
value = 1 %>
<span>middle</span>
<% If value Then %><% Then %>
<% End If %>`
	settings := core.Settings{DefaultLanguage: "VBScript"}
	parsed := core.ParseDocument("file:///shared-tokens.asp", source, settings)
	coldParsed := core.ParseDocument("file:///cold-shared-tokens.asp", source, settings)
	var workers sync.WaitGroup
	coldResults := make([][]Token, 8)
	workers.Add(8)
	for index := range coldResults {
		go func() {
			defer workers.Done()
			coldResults[index] = vbscriptDocumentTokens(coldParsed)
		}()
	}
	workers.Wait()
	coldCanonical := coldResults[0]
	if len(coldCanonical) == 0 {
		t.Fatal("cold concurrent token stream was empty")
	}
	for _, got := range coldResults[1:] {
		if len(got) == 0 || &got[0] != &coldCanonical[0] {
			t.Fatal("cold concurrent token stream diverged")
		}
	}
	first := vbscriptDocumentTokens(parsed)
	if len(first) == 0 {
		t.Fatal("expected significant VBScript tokens")
	}
	second := vbscriptDocumentTokens(parsed)
	if &first[0] != &second[0] {
		t.Fatal("cached token stream did not reuse its backing slice")
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("cached token stream diverged")
	}
	if got := vbscriptDocumentTokens(parsed); got == nil {
		t.Fatal("cached token stream became nil")
	}
	legacy := legacyVBScriptDocumentTokens(parsed)
	if !reflect.DeepEqual(first, legacy) {
		t.Fatalf("shared token stream differs from legacy stream: got %#v want %#v", first, legacy)
	}
	fresh := core.ParseDocument(parsed.URI, source, settings)
	if got := vbscriptDocumentTokens(fresh); !reflect.DeepEqual(first, got) {
		t.Fatalf("shared token stream differs from fresh revision: got %#v want %#v", first, got)
	}
	runtimeValue, ok := parsed.LoadRuntimeAnalysis(vbscriptDocumentTokensAnalysisKey)
	if !ok {
		t.Fatal("token stream was not cached for the revision")
	}
	if runtime, ok := runtimeValue.(*vbscriptDocumentTokensRuntime); !ok || runtime == nil || &runtime.tokens[0] != &first[0] {
		t.Fatalf("cached token runtime = %#v, want token backing", runtimeValue)
	}

	document := core.SourceDocument(parsed)
	changeRange := document.Range(0, 0)
	updated := core.UpdateParsedDocument(parsed, []core.IncrementalChange{{Range: &changeRange, Text: "prefix\n"}}, settings)
	updatedTokens := vbscriptDocumentTokens(updated.Parsed)
	if len(updatedTokens) == 0 {
		t.Fatal("updated revision lost significant VBScript tokens")
	}
	if &updatedTokens[0] == &first[0] {
		t.Fatal("updated revision inherited the previous token backing")
	}
	freshUpdated := core.ParseDocument(updated.Parsed.URI, updated.Parsed.Text, settings)
	if got := vbscriptDocumentTokens(freshUpdated); !reflect.DeepEqual(updatedTokens, got) {
		t.Fatalf("updated token stream differs from fresh revision: got %#v want %#v", updatedTokens, got)
	}
}

func legacyVBScriptDocumentTokens(parsed *core.ParsedDocument) []Token {
	if parsed == nil {
		return nil
	}
	tokens := make([]Token, 0)
	for _, region := range parsed.Regions {
		if region.Language != core.LanguageVBScript || region.ContentStart >= region.ContentEnd {
			continue
		}
		if len(tokens) > 0 {
			tokens = append(tokens, Token{Kind: "newline", Start: region.ContentStart, End: region.ContentStart, Text: "\n"})
		}
		tokens = append(tokens, tokenizeWithBase(parsed.Text[region.ContentStart:region.ContentEnd], region.ContentStart)...)
	}
	return significantTokens(tokens)
}

func TestVBScriptSharedTokenRegionsPreserveLexicalBoundaries(t *testing.T) {
	for name, source := range map[string]string{
		"no script":        "<div>😀</div>",
		"empty islands":    "<%%><% %><% ' comment %><% Rem comment %><%value = 1%>",
		"continuations":    "<%value = _\r\n 1\r\nvalue = _ ' invalid\r\n2%>",
		"island boundary":  "<%value = _%><% ' independent comment %><% value = 2 %>",
		"unclosed string":  "<%value = \"unterminated %><% Dim nextValue %>",
		"adjacent if then": "<% If value %><% Then %>😀<% End If %>",
		"separate scripts": "<script runat=\"server\">value = 1</script><script>let value = 2;</script><%value = 3%>",
	} {
		t.Run(name, func(t *testing.T) {
			parsed := core.ParseDocument("file:///token-boundaries.asp", source, core.Settings{DefaultLanguage: "VBScript"})
			runtime := vbscriptDocumentTokensRuntimeFor(parsed)
			want := legacyVBScriptDocumentTokens(parsed)
			if !reflect.DeepEqual(runtime.tokens, want) {
				t.Fatalf("document tokens = %#v, want %#v", runtime.tokens, want)
			}
			index := 0
			for _, region := range parsed.Regions {
				if region.Language != core.LanguageVBScript || region.ContentStart >= region.ContentEnd {
					continue
				}
				got := runtime.regions[index]
				expected := significantTokens(tokenizeWithBase(source[region.ContentStart:region.ContentEnd], region.ContentStart))
				if got.start != region.Start || got.end != region.End || !reflect.DeepEqual(got.tokens, expected) {
					t.Fatalf("region %d = %#v, want independently tokenized %#v", index, got, expected)
				}
				appended := append(got.tokens, Token{Kind: "sentinel"})
				if appended[len(appended)-1].Kind != "sentinel" || !reflect.DeepEqual(runtime.tokens, want) {
					t.Fatal("appending to a region overwrote the shared document tokens")
				}
				index++
			}
		})
	}
}
