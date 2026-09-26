package tsgoadapter

import (
	"context"
	"strings"
	"testing"
)

func TestCheckVendoredTypeScriptGo(t *testing.T) {
	status := Check("../..")
	if !status.Available {
		t.Fatalf("typescript-go vendor unavailable: %#v", status)
	}
	if PinnedCommit != "79fe60a804acfa49d81e67c499bd7509d70aa8ce" {
		t.Fatalf("unexpected pinned commit %s", PinnedCommit)
	}
}

func TestClassifyJavaScriptLiteralContextsPreservesUnsortedOffsets(t *testing.T) {
	source := `const single = 'a'; const raw = 0; const double = "b";`
	offsets := []int{strings.Index(source, `"b"`) + 1, strings.Index(source, "raw = 0") + len("raw = "), strings.Index(source, "'a'") + 1}
	got, err := ClassifyJavaScriptLiteralContextsContext(context.Background(), source, offsets)
	if err != nil {
		t.Fatal(err)
	}
	want := []JavaScriptLiteralContext{JavaScriptLiteralDoubleQuoted, JavaScriptLiteralRaw, JavaScriptLiteralSingleQuoted}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("literal context[%d] = %d, want %d", index, got[index], want[index])
		}
	}
}

func TestClassifyJavaScriptLiteralContextsHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ClassifyJavaScriptLiteralContextsContext(ctx, `const value = "x";`, []int{15}); err != context.Canceled {
		t.Fatalf("canceled literal classification error = %v, want %v", err, context.Canceled)
	}
}

func TestAnalyzeJavaScriptNavigationTracksControlDependenciesButNotCommaOperands(t *testing.T) {
	const marker = "\x00ASP_NAV_DEP_0123456789abcdef0123456789abcdef_0\x00"
	source := `const flag = "\x00ASP_NAV_DEP_0123456789abcdef0123456789abcdef_0\x00";
const target = flag ? "new.asp" : "old.asp";
location.href = target;
window.open((flag, "known.asp"));
function choose(value) { return value ? "new.asp" : "old.asp"; }
location.assign(choose(flag));
let assigned = flag;
assigned &&= "new.asp";
location.replace(assigned);
let branched;
if (flag) { branched = "new.asp"; } else { branched = "old.asp"; }
window.open(branched);
let switched;
switch (flag) { case "x": switched = "new.asp"; break; default: switched = "old.asp"; }
window.open(switched);`
	analysis, err := AnalyzeJavaScriptNavigation(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	if len(analysis.Sinks) != 6 {
		t.Fatalf("navigation sinks = %#v, want six", analysis.Sinks)
	}
	for _, value := range analysis.Sinks[0].Expression.Values {
		if len(value.Dependencies) != 1 || value.Dependencies[0] != marker {
			t.Fatalf("conditional value dependencies = %#v, want %q", value.Dependencies, marker)
		}
	}
	for _, value := range analysis.Sinks[1].Expression.Values {
		if len(value.Dependencies) != 0 {
			t.Fatalf("comma value dependencies = %#v, want none", value.Dependencies)
		}
	}
	for sinkIndex, sink := range analysis.Sinks[2:] {
		for _, value := range sink.Expression.Values {
			if len(value.Dependencies) != 1 || value.Dependencies[0] != marker {
				t.Fatalf("control sink[%d] value dependencies = %#v, want %q", sinkIndex+2, value.Dependencies, marker)
			}
		}
	}
}

func TestAnalyzeJavaScriptNavigationTracksSwitchDependency(t *testing.T) {
	const marker = "\x00ASP_NAV_DEP_0123456789abcdef0123456789abcdef_0\x00"
	source := `const flag = "\x00ASP_NAV_DEP_0123456789abcdef0123456789abcdef_0\x00";
let target;
switch (flag) { case "x": target = "new.asp"; break; default: target = "old.asp"; }
window.open(target);`
	analysis, err := AnalyzeJavaScriptNavigation(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	if len(analysis.Sinks) != 1 {
		t.Fatalf("navigation sinks = %#v, want one", analysis.Sinks)
	}
	for _, value := range analysis.Sinks[0].Expression.Values {
		if len(value.Dependencies) != 1 || value.Dependencies[0] != marker {
			t.Fatalf("switch value dependencies = %#v, want %q; analysis=%#v", value.Dependencies, marker, analysis)
		}
	}
}

func TestAnalyzeJavaScriptUsesVendoredTypeScriptGoScanner(t *testing.T) {
	analysis := AnalyzeJavaScript("const value = formatName(customer.name)")
	if analysis.TokenCount == 0 {
		t.Fatalf("expected tokens, got %#v", analysis)
	}
	if !hasIdentifier(analysis.Identifiers, "formatName") {
		t.Fatalf("expected formatName identifier, got %#v", analysis.Identifiers)
	}
}

func TestClassifyJavaScriptLiteralContextsUsesParserLexicalGoals(t *testing.T) {
	source := `if (enabled) /"0"/.test(value); const raw = 0; const single = '0'; const double = "0"; const template = ` + "`0`" + `;`
	offsets := []int{
		strings.Index(source, `/"0"/`) + 2,
		strings.Index(source, "raw = 0") + len("raw = "),
		strings.Index(source, "'0'") + 1,
		strings.Index(source, `double = "0"`) + len(`double = "`),
		strings.Index(source, "`0`") + 1,
	}
	got := ClassifyJavaScriptLiteralContexts(source, offsets)
	want := []JavaScriptLiteralContext{JavaScriptLiteralRegex, JavaScriptLiteralRaw, JavaScriptLiteralSingleQuoted, JavaScriptLiteralDoubleQuoted, JavaScriptLiteralTemplate}
	if len(got) != len(want) {
		t.Fatalf("literal contexts = %#v, want %#v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("literal context[%d] = %d, want %d: source=%q offset=%d", index, got[index], want[index], source, offsets[index])
		}
	}
}

func hasIdentifier(identifiers []Identifier, name string) bool {
	for _, identifier := range identifiers {
		if identifier.Text == name {
			return true
		}
	}
	return false
}
