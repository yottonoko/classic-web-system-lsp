package vbscript

import (
	"fmt"
	"strings"
	"testing"
	"unsafe"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

func TestVBScriptTokenRuntimeAccountsRetainedCapacity(t *testing.T) {
	parsed := core.ParseDocument("file:///token-capacity.asp", "<%"+strings.Repeat(" value ", 1000)+"%>", core.Settings{DefaultLanguage: "VBScript"})
	runtime := vbscriptDocumentTokensRuntimeFor(parsed)
	backing := make(map[*Token]struct{})
	collect := func(tokens []Token) {
		for index := range cap(tokens) {
			backing[&tokens[:cap(tokens)][index]] = struct{}{}
		}
	}
	collect(runtime.tokens)
	for _, region := range runtime.regions {
		collect(region.tokens)
	}
	minimum := int64(unsafe.Sizeof(*runtime)) + int64(len(backing))*int64(unsafe.Sizeof(Token{})) + int64(cap(runtime.regions))*int64(unsafe.Sizeof(vbscriptDocumentTokenRegion{}))
	var reported int64
	for _, owner := range runtime.RuntimeAnalysisMemoryOwnerSet() {
		reported += owner.Bytes
	}
	if reported < minimum {
		t.Fatalf("reported %d bytes, but the token and region arrays alone retain at least %d", reported, minimum)
	}
}

func TestVBStatementsAccountSharedAndMergedTokenStorage(t *testing.T) {
	const source = "<% Dim value : value = 1 %><% If value %><% Then %>😀<% End If %>"
	parsed := core.ParseDocument("file:///statement-storage.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	statements := vbStatements(parsed)
	value, ok := parsed.LoadRuntimeAnalysis(vbStatementsAnalysisKey)
	if !ok {
		t.Fatal("statement runtime was not retained")
	}
	runtime := value.(*vbStatementsRuntime)
	backing := make(map[*Token]struct{})
	collect := func(tokens []Token) {
		for index := range cap(tokens) {
			backing[&tokens[:cap(tokens)][index]] = struct{}{}
		}
	}
	collect(runtime.tokens.tokens)
	for _, statement := range statements {
		collect(statement.Tokens)
	}
	want := int64(unsafe.Sizeof(*runtime)) + int64(unsafe.Sizeof(*runtime.tokens)) +
		int64(cap(statements))*int64(unsafe.Sizeof(vbStatement{})) +
		int64(cap(runtime.tokens.regions))*int64(unsafe.Sizeof(vbscriptDocumentTokenRegion{})) +
		int64(len(backing))*int64(unsafe.Sizeof(Token{}))
	// ParsedDocument also accounts for each provider's 24-byte cache entry.
	want += 2 * 24
	var got int64
	for _, owner := range parsed.RuntimeAnalysisMemoryOwners() {
		if owner.Identity == runtime || owner.Identity == runtime.tokens {
			got += owner.Bytes
		}
	}
	if got != want {
		t.Fatalf("retained statement storage = %d, want %d with shared arrays charged once", got, want)
	}
	if len(statements) != 4 || len(statements[2].Tokens) != 3 || statements[2].Tokens[2].Text != "Then" {
		t.Fatalf("adjacent If/Then islands did not merge: %#v", statements)
	}
	before := append([]Token(nil), runtime.tokens.tokens...)
	for _, statement := range statements {
		_ = append(statement.Tokens, Token{Kind: "sentinel"})
	}
	for index, token := range runtime.tokens.tokens {
		if token != before[index] {
			t.Fatal("appending to a statement overwrote the shared tokens")
		}
	}
}

func BenchmarkVBScriptColdDiagnostics(b *testing.B) {
	for _, islands := range []bool{false, true} {
		b.Run(fmt.Sprintf("islands=%t", islands), func(b *testing.B) {
			var source strings.Builder
			if !islands {
				source.WriteString("<%\n")
			}
			for index := range 1000 {
				if islands {
					source.WriteString("<% ")
				}
				fmt.Fprintf(&source, "Dim value%d : value%d = %d ' comment\n", index, index, index)
				if islands {
					source.WriteString("%><span>value</span>\n")
				}
			}
			if !islands {
				source.WriteString("%>")
			}
			text := source.String()
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				parsed := core.ParseDocument("file:///cold-diagnostics.asp", text, core.Settings{DefaultLanguage: "VBScript"})
				if diagnostics := SyntaxDiagnostics(parsed, SyntaxOptions{}); len(diagnostics) != 0 {
					b.Fatalf("unexpected diagnostics: %#v", diagnostics)
				}
			}
		})
	}
}
