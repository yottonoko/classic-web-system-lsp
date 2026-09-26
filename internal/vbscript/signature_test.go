package vbscript

import (
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

func TestBuildSignaturesKeepsFirstDuplicateProcedure(t *testing.T) {
	source := `<%
Function Shared(firstName)
End Function
Sub Shared(secondName)
End Sub
%>`
	parsed := core.ParseDocument("file:///signature-duplicate.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	signature, ok := BuildSignatures(parsed)["shared"]
	if !ok {
		t.Fatal("duplicate procedure signature is missing")
	}
	if !strings.EqualFold(signature.Kind, "function") || signature.Label != "Shared(ByRef firstName)" {
		t.Fatalf("duplicate procedure signature = %#v, want first Function Shared(ByRef firstName)", signature)
	}
}

func TestBuildSignaturesIgnoresLegacyAnalysisMap(t *testing.T) {
	source := `<%
Function Shared(firstName)
End Function
Sub Shared(secondName)
End Sub
%>`
	parsed := core.ParseDocument("file:///signature-legacy-analysis.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	legacy := map[string]Signature{
		"shared": {Name: "Shared", Kind: "sub", Label: "Shared(ByRef secondName)"},
	}
	parsed.StoreAnalysis("vbscript.signatures-by-name.v1", legacy)
	parsed.StoreRuntimeAnalysis("vbscript.signatures-by-name.runtime.v1", signatureMapRuntime{values: legacy})

	signature, ok := BuildSignatures(parsed)["shared"]
	if !ok {
		t.Fatal("rebuilt duplicate procedure signature is missing")
	}
	if !strings.EqualFold(signature.Kind, "function") || signature.Label != "Shared(ByRef firstName)" {
		t.Fatalf("rebuilt duplicate procedure signature = %#v, want first Function Shared(ByRef firstName)", signature)
	}
	var current map[string]Signature
	if !parsed.LoadAnalysis("vbscript.signatures-by-name.v2", &current) {
		t.Fatal("rebuilt signature map was not stored under the current analysis key")
	}
	if current["shared"].Label != "Shared(ByRef firstName)" {
		t.Fatalf("current signature map = %#v, want first duplicate", current)
	}
}

func TestProcedureHeaderAtRecoversUnterminatedParameterLists(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		kind      string
		accessor  string
		params    string
		complete  bool
		endPrefix string
	}{
		{
			name:      "function with default expression",
			text:      `Function Broken(Optional ByVal value = "a:b)", ByRef second`,
			kind:      "function",
			params:    `Optional ByVal value = "a:b)", ByRef second`,
			complete:  false,
			endPrefix: `Function Broken(Optional ByVal value = "a:b)", ByRef second`,
		},
		{
			name:      "function with doubled quote",
			text:      `Function Escaped(Optional ByVal value = "a""b", ByRef second`,
			kind:      "function",
			params:    `Optional ByVal value = "a""b", ByRef second`,
			complete:  false,
			endPrefix: `Function Escaped(Optional ByVal value = "a""b", ByRef second`,
		},
		{
			name:      "property setter after colon",
			text:      `Public Property Let Item(ByRef value: Dim following`,
			kind:      "property",
			accessor:  "let",
			params:    "ByRef value",
			complete:  false,
			endPrefix: "Public Property Let Item(ByRef value",
		},
		{
			name:      "completed string containing delimiters",
			text:      `Function Complete(Optional ByVal value = "a:b)")`,
			kind:      "function",
			params:    `Optional ByVal value = "a:b)"`,
			complete:  true,
			endPrefix: `Function Complete(Optional ByVal value = "a:b)")`,
		},
		{
			name:      "nested default expression",
			text:      `Function Nested(Optional ByVal value = Lookup(1, 2), ByRef second)`,
			kind:      "function",
			params:    `Optional ByVal value = Lookup(1, 2), ByRef second`,
			complete:  true,
			endPrefix: `Function Nested(Optional ByVal value = Lookup(1, 2), ByRef second)`,
		},
		{
			name:      "date default expression",
			text:      `Function DateDefault(Optional ByVal value = #12:30:00#, ByRef second)`,
			kind:      "function",
			params:    `Optional ByVal value = #12:30:00#, ByRef second`,
			complete:  true,
			endPrefix: `Function DateDefault(Optional ByVal value = #12:30:00#, ByRef second)`,
		},
		{
			name:      "comment stops recovery",
			text:      `Sub Commented(ByVal value ' Function Fake(ByVal ignored)`,
			kind:      "sub",
			params:    "ByVal value ",
			complete:  false,
			endPrefix: "Sub Commented(ByVal value ",
		},
		{
			name:      "legacy no parentheses",
			text:      "Function Legacy",
			kind:      "function",
			params:    "",
			complete:  false,
			endPrefix: "Function Legacy",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			header, ok := ProcedureHeaderAt(test.text)
			if !ok {
				t.Fatalf("ProcedureHeaderAt(%q) did not match", test.text)
			}
			if header.Kind != test.kind || header.Accessor != test.accessor {
				t.Fatalf("header kind/accessor = %q/%q, want %q/%q", header.Kind, header.Accessor, test.kind, test.accessor)
			}
			if !header.HasParameterList {
				if test.params != "" {
					t.Fatalf("header omitted parameter list for %q", test.text)
				}
			} else if got := test.text[header.ParamsStart:header.ParamsEnd]; got != test.params {
				t.Fatalf("parameter text = %q, want %q", got, test.params)
			}
			if header.ParameterListComplete != test.complete {
				t.Fatalf("parameter list complete = %v, want %v", header.ParameterListComplete, test.complete)
			}
			if got := test.text[header.NameStart:header.NameEnd]; got == "" {
				t.Fatal("header name is empty")
			}
			if test.endPrefix != "" && test.text[:header.End] != test.endPrefix {
				t.Fatalf("header text = %q, want %q", test.text[:header.End], test.endPrefix)
			}
		})
	}
}

func TestSignaturesRecoverUnterminatedParameterList(t *testing.T) {
	source := `<%
Function Broken(Optional ByVal value = "fallback", ByRef second
  Broken = value
End Function
Class Widget
  Public Function Method(ByVal item
    Response.Write item
  End Function
End Class
%>`
	parsed := core.ParseDocument("file:///signature-incomplete.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	signatures := Signatures(parsed)
	if len(signatures) != 2 {
		t.Fatalf("signatures = %#v, want root and class Function declarations", signatures)
	}
	if signatures[0].Label != `Broken(ByVal value, ByRef second)` {
		t.Fatalf("incomplete root signature label = %q", signatures[0].Label)
	}
	if signatures[1].Label != "Method(ByVal item)" {
		t.Fatalf("incomplete class signature label = %q", signatures[1].Label)
	}
}

func TestProcedureHeaderAtLogicalRecoversExplicitContinuations(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		kind     string
		accessor string
		params   []Parameter
	}{
		{
			name: "function with CRLF and nested default",
			text: "Function Multi( _\r\n  Optional ByVal first = Lookup(\"a,b)\"), _\r\n  ByRef second _\r\n)",
			kind: "function",
			params: []Parameter{
				{Name: "first", Mode: "ByVal", Optional: true},
				{Name: "second", Mode: "ByRef"},
			},
		},
		{
			name:     "property setter with LF",
			text:     "Public Property Let Item( _\n  ByVal key, Optional ByRef value _\n)",
			kind:     "property",
			accessor: "let",
			params: []Parameter{
				{Name: "key", Mode: "ByVal"},
				{Name: "value", Mode: "ByRef", Optional: true},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			header, logicalEnd, ok := ProcedureHeaderAtLogical(test.text, 0)
			if !ok || logicalEnd != len(test.text) {
				t.Fatalf("ProcedureHeaderAtLogical = %#v, end %d, want match ending at %d", header, logicalEnd, len(test.text))
			}
			if header.Kind != test.kind || header.Accessor != test.accessor || !header.ParameterListComplete {
				t.Fatalf("header kind/accessor/complete = %q/%q/%v, want %q/%q/true", header.Kind, header.Accessor, header.ParameterListComplete, test.kind, test.accessor)
			}
			if header.NameStart < 0 || header.NameEnd <= header.NameStart || test.text[header.NameStart:header.NameEnd] == "" {
				t.Fatalf("header name range = %d:%d", header.NameStart, header.NameEnd)
			}
			got := parseParameters(test.text[header.ParamsStart:header.ParamsEnd])
			if len(got) != len(test.params) {
				t.Fatalf("parameters = %#v, want %#v", got, test.params)
			}
			for index := range test.params {
				if got[index] != test.params[index] {
					t.Fatalf("parameter %d = %#v, want %#v", index, got[index], test.params[index])
				}
			}
			if test.text[header.End-1] != ')' {
				t.Fatalf("header end = %d, source prefix = %q, want closing parenthesis", header.End, test.text[:header.End])
			}
		})
	}
}

func TestProcedureHeaderAtLogicalKeepsIncompleteHeadersPhysicalLineBounded(t *testing.T) {
	text := "Function Single(ByVal first\n  ByRef second)\nSub Next"
	header, logicalEnd, ok := ProcedureHeaderAtLogical(text, 0)
	if !ok || header.ParameterListComplete {
		t.Fatalf("header = %#v, end %d, want incomplete first line", header, logicalEnd)
	}
	firstLineEnd := strings.IndexByte(text, '\n')
	if logicalEnd != firstLineEnd || text[header.ParamsStart:header.ParamsEnd] != "ByVal first" {
		t.Fatalf("header end/parameters = %d/%q, want %d/ByVal first", logicalEnd, text[header.ParamsStart:header.ParamsEnd], firstLineEnd)
	}
}

func TestProcedureHeaderAtLogicalDoesNotContinueComments(t *testing.T) {
	text := "Function Commented(ByVal first ' continuation marker is only a comment _\nSub Next"
	header, logicalEnd, ok := ProcedureHeaderAtLogical(text, 0)
	if !ok || header.ParameterListComplete {
		t.Fatalf("header = %#v, end %d, want incomplete commented header", header, logicalEnd)
	}
	firstLineEnd := strings.IndexByte(text, '\n')
	if logicalEnd != firstLineEnd || text[header.ParamsStart:header.ParamsEnd] != "ByVal first " {
		t.Fatalf("header end/parameters = %d/%q, want comment-bounded first line", logicalEnd, text[header.ParamsStart:header.ParamsEnd])
	}
}

func TestProcedureHeaderAtLogicalDoesNotJoinAfterCompletedHeader(t *testing.T) {
	text := "Function Complete(ByVal value) _\nSub Next"
	header, logicalEnd, ok := ProcedureHeaderAtLogical(text, 0)
	if !ok || !header.ParameterListComplete {
		t.Fatalf("header = %#v, end %d, want completed physical header", header, logicalEnd)
	}
	if logicalEnd != strings.IndexByte(text, '\n') {
		t.Fatalf("logical end = %d, want completed header's physical line", logicalEnd)
	}
}

func TestProcedureHeaderAtLogicalDoesNotConsumeFollowingProcedureEnd(t *testing.T) {
	text := "Function Broken( _\n  ByRef value _\nEnd Function\nSub Next"
	header, logicalEnd, ok := ProcedureHeaderAtLogical(text, 0)
	if !ok || header.ParameterListComplete {
		t.Fatalf("header = %#v, end %d, want incomplete parameter prefix", header, logicalEnd)
	}
	wantEnd := strings.Index(text, "End Function") - 1
	if logicalEnd != wantEnd || text[header.ParamsStart:header.ParamsEnd] != " _\n  ByRef value _" {
		t.Fatalf("logical header end/parameters = %d/%q, want %d/prefix before End Function", logicalEnd, text[header.ParamsStart:header.ParamsEnd], wantEnd)
	}
}

func TestProcedureHeaderAtLogicalStopsBeforeModifierLedProcedure(t *testing.T) {
	tests := []struct {
		name     string
		next     string
		kind     string
		accessor string
	}{
		{name: "public sub", next: "Public Sub Next(ByVal value)", kind: "sub"},
		{name: "private function", next: "Private Function Next(ByVal value)", kind: "function"},
		{name: "default property", next: "Default Property Get Next(ByVal value)", kind: "property", accessor: "get"},
		{name: "public default property", next: "Public Default Property Get Next(ByVal value)", kind: "property", accessor: "get"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			text := "Function Broken( _\n" + test.next + "\nEnd Function"
			header, logicalEnd, ok := ProcedureHeaderAtLogical(text, 0)
			if !ok || header.ParameterListComplete {
				t.Fatalf("broken header = %#v, end %d, want incomplete header", header, logicalEnd)
			}
			firstLineEnd := strings.IndexByte(text, '\n')
			if logicalEnd != firstLineEnd {
				t.Fatalf("broken logical end = %d, want %d before %q", logicalEnd, firstLineEnd, test.next)
			}

			nextStart := firstLineEnd + 1
			nextHeader, _, nextOK := ProcedureHeaderAtLogical(text, nextStart)
			if !nextOK || nextHeader.Kind != test.kind || nextHeader.Accessor != test.accessor {
				t.Fatalf("next header = %#v, want %s/%s", nextHeader, test.kind, test.accessor)
			}
			if got := text[nextHeader.NameStart:nextHeader.NameEnd]; got != "Next" {
				t.Fatalf("next header name = %q, want Next", got)
			}
		})
	}
}

func TestSignaturesKeepModifierLedProcedureAfterIncompleteContinuation(t *testing.T) {
	source := `<%
Function Broken( _
Public Sub Next(ByVal value)
End Sub
%>`
	parsed := core.ParseDocument("file:///signature-modifier-boundary.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	signatures := Signatures(parsed)
	if len(signatures) != 2 {
		t.Fatalf("signatures = %#v, want Broken and Next", signatures)
	}
	if signatures[1].Name != "Next" || signatures[1].Kind != "sub" || signatures[1].Label != "Next(ByVal value)" {
		t.Fatalf("next signature = %#v, want Public Sub Next(ByVal value)", signatures[1])
	}
}

func TestProcedureHeaderAtLogicalMapsAbsoluteOffsets(t *testing.T) {
	text := "前置 = 1\r\n  Function Mapped( _\r\n    ByVal value _\r\n  )\r\n"
	start := strings.Index(text, "Function Mapped")
	header, logicalEnd, ok := ProcedureHeaderAtLogical(text, start)
	if !ok || !header.ParameterListComplete {
		t.Fatalf("header = %#v, end %d, want complete mapped header", header, logicalEnd)
	}
	if got := text[header.NameStart:header.NameEnd]; got != "Mapped" {
		t.Fatalf("mapped name = %q, want Mapped", got)
	}
	if got := text[header.ParamsStart:header.ParamsEnd]; !strings.Contains(got, "ByVal value") {
		t.Fatalf("mapped parameter text = %q, want ByVal value", got)
	}
	if logicalEnd != len(text)-2 || text[header.End-1] != ')' {
		t.Fatalf("mapped header end/logical end = %d/%d, want closing parenthesis before final newline", header.End, logicalEnd)
	}
}
