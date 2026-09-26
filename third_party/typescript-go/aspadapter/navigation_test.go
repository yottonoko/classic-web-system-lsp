package aspadapter

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAnalyzeJavaScriptNavigationResolvesFiniteAliasesAndTemplates(t *testing.T) {
	source := `const prefix = "next";
let suffix = ".asp";
const target = prefix + suffix;
const choice = true ? target : "fallback.asp";
location.href = ` + "`/app/${choice}`" + `;
window.open("detail.asp", "content");
`
	result, err := AnalyzeJavaScriptNavigation(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 2 {
		t.Fatalf("sinks = %#v", result.Sinks)
	}
	location := result.Sinks[0]
	if location.Kind != "javascriptLocation" || len(location.Expression.Values) != 2 {
		t.Fatalf("location = %#v", location)
	}
	if got := location.Expression.Values[0].Text; got != "/app/next.asp" {
		t.Fatalf("first finite value = %q", got)
	}
	if got := location.Expression.Values[1].Text; got != "/app/fallback.asp" {
		t.Fatalf("second finite value = %q", got)
	}
	if location.Expression.Values[0].Kind != NavigationValueTemplate {
		t.Fatalf("template kind = %#v", location.Expression.Values[0])
	}
	if location.Expression.Range.ByteStart != strings.Index(source, "`/app") || location.Expression.Range.ByteEnd != strings.Index(source, "`;")+1 {
		t.Fatalf("expression range = %#v", location.Expression.Range)
	}
	if result.Sinks[1].TargetFrame != "content" {
		t.Fatalf("window target frame = %q", result.Sinks[1].TargetFrame)
	}
}

func TestAnalyzeJavaScriptNavigationIgnoresCommentsAndStrings(t *testing.T) {
	source := `// location.href = "comment.asp";
const text = "window.open('string.asp')";
/* history.pushState({}, "", "comment.asp") */
location.assign("real.asp");
`
	result, err := AnalyzeJavaScriptNavigation(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 || result.Sinks[0].Expression.Values[0].Text != "real.asp" {
		t.Fatalf("comment/string filtering = %#v", result.Sinks)
	}
}

func TestAnalyzeJavaScriptNavigationResolvesFunctionControlDependencies(t *testing.T) {
	const marker = "\x00ASP_NAV_DEP_0123456789abcdef0123456789abcdef_1\x00"
	for _, testCase := range []struct {
		name       string
		source     string
		wantMarker bool
	}{
		{
			name:       "direct call",
			source:     `function go(value) { if (value) location.href = "next.asp"; } go("` + marker + `");`,
			wantMarker: true,
		},
		{
			name:       "multiple calls",
			source:     `function go(value) { value && (location.href = "next.asp"); } go(false); go("` + marker + `");`,
			wantMarker: true,
		},
		{
			name:       "nested call",
			source:     `function go(value) { if (value) location.href = "next.asp"; } function wrap(value) { go(value); } wrap("` + marker + `");`,
			wantMarker: true,
		},
		{
			name:       "uncalled function",
			source:     `function go(value) { if (value) location.href = "next.asp"; }`,
			wantMarker: false,
		},
		{
			name:       "called switch",
			source:     `function go(value) { switch (value) { case "yes": location.href = "next.asp"; break; } } go("` + marker + `");`,
			wantMarker: true,
		},
		{
			name:       "called loop",
			source:     `function go(value) { while (value) { location.href = "next.asp"; break; } } go("` + marker + `");`,
			wantMarker: true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			result, err := AnalyzeJavaScriptNavigation(context.Background(), testCase.source)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Sinks) != 1 || len(result.Sinks[0].Expression.Values) == 0 {
				t.Fatalf("sinks = %#v, want one sink with values", result.Sinks)
			}
			dependencies := result.Sinks[0].Expression.Values[0].Dependencies
			if got := slices.Contains(dependencies, marker); got != testCase.wantMarker {
				t.Fatalf("dependencies = %#v, marker present = %v, want %v", dependencies, got, testCase.wantMarker)
			}
			for _, dependency := range dependencies {
				if strings.Contains(dependency, "asp-navigation-parameter:") {
					t.Fatalf("internal parameter dependency leaked: %#v", dependencies)
				}
			}
		})
	}
}

func TestAnalyzeJavaScriptNavigationTracksEscapedRegexDependencies(t *testing.T) {
	const marker = "\x00ASP_NAV_DEP_0123456789abcdef0123456789abcdef_1\x00"
	const escapedMarker = `\x00ASP_NAV_DEP_0123456789abcdef0123456789abcdef_1\x00`
	source := `if (/` + escapedMarker + `/.test(input)) location.href = "next.asp";
function go(value) { if (/` + escapedMarker + `/.test(value)) location.assign("inside.asp"); }
go(input);`
	result, err := AnalyzeJavaScriptNavigation(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 2 {
		t.Fatalf("sinks = %#v, want two", result.Sinks)
	}
	for index, sink := range result.Sinks {
		if len(sink.Expression.Values) == 0 || !slices.Contains(sink.Expression.Values[0].Dependencies, marker) {
			t.Fatalf("sink[%d] dependencies = %#v, want %q", index, sink.Expression.Values, marker)
		}
	}
}

func TestAnalyzeJavaScriptNavigationExpressionRangeSkipsComments(t *testing.T) {
	source := "location.href = /* keep syntax out of the value */ \"real.asp\";"
	result, err := AnalyzeJavaScriptNavigation(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 {
		t.Fatalf("sinks = %#v", result.Sinks)
	}
	rangeValue := result.Sinks[0].Expression.Range
	start := strings.Index(source, `"real.asp"`)
	if rangeValue.ByteStart != start || rangeValue.ByteEnd != start+len(`"real.asp"`) {
		t.Fatalf("comment-adjusted expression range = %#v", rangeValue)
	}
}

func TestAnalyzeJavaScriptNavigationAcceptsTypeScriptAssertions(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `const target = "/typed.asp" as const;
location.href = target;
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 || len(result.Sinks[0].Expression.Values) != 1 || result.Sinks[0].Expression.Values[0].Text != "/typed.asp" {
		t.Fatalf("TypeScript assertion result = %#v", result.Sinks)
	}
}

func TestAnalyzeJavaScriptNavigationDoesNotLeakFunctionBindings(t *testing.T) {
	source := `const target = "outer.asp";
function helper() { const target = "inner.asp"; location.href = target; }
location.href = target;
`
	result, err := AnalyzeJavaScriptNavigation(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 2 || result.Sinks[0].Expression.Values[0].Text != "inner.asp" || result.Sinks[1].Expression.Values[0].Text != "outer.asp" {
		t.Fatalf("function scopes = %#v", result.Sinks)
	}
}

func TestAnalyzeJavaScriptNavigationEvaluatesForwardFunctionReturns(t *testing.T) {
	source := `location.href = buildPath("next");
function buildPath(prefix) {
  const suffix = ".asp";
  return prefix + suffix;
}
`
	result, err := AnalyzeJavaScriptNavigation(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 || len(result.Sinks[0].Expression.Values) != 1 || result.Sinks[0].Expression.Values[0].Text != "next.asp" {
		t.Fatalf("forward function return = %#v", result.Sinks)
	}
}

func TestAnalyzeJavaScriptNavigationKeepsNestedFunctionDeclarationsLexical(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
function target() { return "top.asp"; }
function outer() {
  function target() { return "nested.asp"; }
  return target();
}
location.href = outer();
location.assign(target());
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 2 {
		t.Fatalf("lexical function sinks = %#v", result.Sinks)
	}
	if got := result.Sinks[0].Expression.Values; len(got) != 1 || got[0].Text != "nested.asp" {
		t.Fatalf("nested function lookup = %#v", got)
	}
	if got := result.Sinks[1].Expression.Values; len(got) != 1 || got[0].Text != "top.asp" {
		t.Fatalf("top-level function lookup = %#v", got)
	}
}

func TestAnalyzeJavaScriptNavigationIgnoresUncalledNestedFunctionForGlobalLookup(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
function target() { return "top.asp"; }
function unused() {
  function target() { return "nested.asp"; }
}
location.href = target();
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 || len(result.Sinks[0].Expression.Values) != 1 || result.Sinks[0].Expression.Values[0].Text != "top.asp" {
		t.Fatalf("uncalled nested lookup = %#v", result.Sinks)
	}
}

func TestAnalyzeJavaScriptNavigationUsesLocalFunctionScopeInsideVisitedFunction(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
function target() { return "top.asp"; }
function outer() {
  function target() { return "nested.asp"; }
  location.href = target();
}
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 || len(result.Sinks[0].Expression.Values) != 1 || result.Sinks[0].Expression.Values[0].Text != "nested.asp" {
		t.Fatalf("visited function scope = %#v", result.Sinks)
	}
}

func TestAnalyzeJavaScriptNavigationFunctionParameterShadowsGlobalFunction(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
function target() { return "top.asp"; }
function outer(target) { return target(); }
location.href = outer("not-a-function");
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 || len(result.Sinks[0].Expression.Values) != 1 || result.Sinks[0].Expression.Values[0].Kind != NavigationValueUnknown {
		t.Fatalf("parameter shadowing = %#v", result.Sinks)
	}
}

func TestAnalyzeJavaScriptNavigationEvaluatesFunctionExpressionsAndArrows(t *testing.T) {
	source := `const makePath = function(prefix) {
  const root = "/app/";
  return root + prefix;
};
const makeDetail = (name) => ` + "`" + `${name}.asp` + "`" + `;
location.href = makePath("next");
window.open(makeDetail("detail"), "content");
`
	result, err := AnalyzeJavaScriptNavigation(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 2 {
		t.Fatalf("function expressions and arrows = %#v", result.Sinks)
	}
	if got := result.Sinks[0].Expression.Values[0].Text; got != "/app/next" {
		t.Fatalf("function expression result = %q", got)
	}
	if got := result.Sinks[1].Expression.Values[0].Text; got != "detail.asp" || result.Sinks[1].TargetFrame != "content" {
		t.Fatalf("arrow result = %#v", result.Sinks[1])
	}
}

func TestAnalyzeJavaScriptNavigationEvaluatesConditionalFunctionReturns(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
function choose(flag) {
  return flag ? "/one.asp" : "/two.asp";
}
location.href = choose(unknownFlag);
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 || len(result.Sinks[0].Expression.Values) != 2 {
		t.Fatalf("conditional function return = %#v", result.Sinks)
	}
	if got := []string{result.Sinks[0].Expression.Values[0].Text, result.Sinks[0].Expression.Values[1].Text}; got[0] != "/one.asp" || got[1] != "/two.asp" {
		t.Fatalf("conditional function values = %#v", got)
	}
}

func TestAnalyzeJavaScriptNavigationMergesBranchAssignments(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
let target = "base.asp";
if (unknownFlag) {
  target = "one.asp";
} else {
  target = "two.asp";
}
location.href = target;
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 {
		t.Fatalf("branch assignment sinks = %#v", result.Sinks)
	}
	values := result.Sinks[0].Expression.Values
	if len(values) != 2 || values[0].Text != "one.asp" || values[1].Text != "two.asp" {
		t.Fatalf("branch assignment values = %#v", values)
	}
}

func TestAnalyzeJavaScriptNavigationKeepsMissingElseBaseState(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
let target = "base.asp";
if (unknownFlag) target = "one.asp";
location.href = target;
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 {
		t.Fatalf("missing else sinks = %#v", result.Sinks)
	}
	values := result.Sinks[0].Expression.Values
	if len(values) != 2 || values[0].Text != "one.asp" || values[1].Text != "base.asp" {
		t.Fatalf("missing else values = %#v", values)
	}
}

func TestAnalyzeJavaScriptNavigationSnapshotsAliasesBeforeDependencyMutation(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
let suffix = ".asp";
const target = "/app" + suffix;
if (unknownFlag) suffix = ".html";
location.href = target;
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 {
		t.Fatalf("branch alias sinks = %#v", result.Sinks)
	}
	values := result.Sinks[0].Expression.Values
	if len(values) != 1 || values[0].Text != "/app.asp" {
		t.Fatalf("branch alias values = %#v", values)
	}
}

func TestAnalyzeJavaScriptNavigationMergesSwitchAssignments(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
let target = "base.asp";
switch (unknownFlag) {
case 1:
  target = "one.asp";
  break;
case 2:
  target = "two.asp";
  break;
default:
  target = "fallback.asp";
}
location.href = target;
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 {
		t.Fatalf("switch assignment sinks = %#v", result.Sinks)
	}
	values := result.Sinks[0].Expression.Values
	if len(values) != 3 || values[0].Text != "one.asp" || values[1].Text != "two.asp" || values[2].Text != "fallback.asp" {
		t.Fatalf("switch assignment values = %#v", values)
	}
}

func TestAnalyzeJavaScriptNavigationModelsSwitchFallthrough(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
let target = "base";
switch (unknownFlag) {
case 1:
  target += "-one";
case 2:
  target += "-two";
  break;
default:
  target = "fallback";
}
location.href = target;
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 {
		t.Fatalf("fallthrough sinks = %#v", result.Sinks)
	}
	values := result.Sinks[0].Expression.Values
	if len(values) != 3 || values[0].Text != "base-one-two" || values[1].Text != "base-two" || values[2].Text != "fallback" {
		t.Fatalf("fallthrough values = %#v", values)
	}
}

func TestAnalyzeJavaScriptNavigationSnapshotsInitializerValues(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
let source = "old.asp";
const target = source;
source = "new.asp";
location.href = target;
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 {
		t.Fatalf("initializer snapshot sinks = %#v", result.Sinks)
	}
	if values := result.Sinks[0].Expression.Values; len(values) != 1 || values[0].Text != "old.asp" {
		t.Fatalf("initializer snapshot values = %#v", values)
	}
}

func TestAnalyzeJavaScriptNavigationSnapshotsFunctionInitializerValues(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
function buildPath() {
  let source = "old.asp";
  let target = source;
  source = "new.asp";
  return target;
}
location.href = buildPath();
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 {
		t.Fatalf("function initializer snapshot sinks = %#v", result.Sinks)
	}
	if values := result.Sinks[0].Expression.Values; len(values) != 1 || values[0].Text != "old.asp" {
		t.Fatalf("function initializer snapshot values = %#v", values)
	}
}

func TestAnalyzeJavaScriptNavigationAppliesSwitchValueTDZ(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
const target = "global.asp";
switch (unknownFlag) {
case 1:
  location.href = target;
  break;
case 2:
  const target = "local.asp";
  location.assign(target);
  break;
}
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 2 {
		t.Fatalf("switch value TDZ sinks = %#v", result.Sinks)
	}
	if values := result.Sinks[0].Expression.Values; len(values) != 1 || values[0].Kind != NavigationValueUnknown {
		t.Fatalf("switch value pre-declaration lookup = %#v", values)
	}
	if values := result.Sinks[1].Expression.Values; len(values) != 1 || values[0].Text != "local.asp" {
		t.Fatalf("switch value post-declaration lookup = %#v", values)
	}
}

func TestAnalyzeJavaScriptNavigationSkipsFallthroughCaseExpressions(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
let marker = "base";
switch (unknownFlag) {
case 1:
  marker = "one";
case marker = "two":
  location.href = marker;
  break;
default:
  location.assign(marker);
  break;
}
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 2 {
		t.Fatalf("switch case expression sinks = %#v", result.Sinks)
	}
	values := result.Sinks[0].Expression.Values
	if len(values) != 2 || values[0].Text != "one" || values[1].Text != "two" {
		t.Fatalf("fallthrough case expression values = %#v", values)
	}
	if values := result.Sinks[1].Expression.Values; len(values) != 1 || values[0].Text != "two" {
		t.Fatalf("default case expression values = %#v", values)
	}
}

func TestAnalyzeJavaScriptNavigationSkipsFallthroughCaseExpressionsInFunctions(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
function buildPath() {
  let marker = "base";
  switch (unknownFlag) {
  case 1:
    marker = "one";
  case marker = "two":
    return marker;
  default:
    return marker;
  }
}
location.href = buildPath();
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 {
		t.Fatalf("function case expression sinks = %#v", result.Sinks)
	}
	values := result.Sinks[0].Expression.Values
	if len(values) != 2 || values[0].Text != "one" || values[1].Text != "two" {
		t.Fatalf("function fallthrough case expression values = %#v", values)
	}
}

func TestAnalyzeJavaScriptNavigationUsesGlobalBindingsForCallees(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
let shared = "global.asp";
function readShared() { return shared; }
function caller() {
  let shared = "private.asp";
  return readShared();
}
location.href = caller();
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 {
		t.Fatalf("lexical global read sinks = %#v", result.Sinks)
	}
	if values := result.Sinks[0].Expression.Values; len(values) != 1 || values[0].Text != "global.asp" {
		t.Fatalf("lexical global read values = %#v", values)
	}
}

func TestAnalyzeJavaScriptNavigationUsesGlobalStateAtCallTime(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
let shared = "before.asp";
function readShared() { return shared; }
shared = "after.asp";
location.href = readShared();
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 {
		t.Fatalf("global call-time sinks = %#v", result.Sinks)
	}
	if values := result.Sinks[0].Expression.Values; len(values) != 1 || values[0].Text != "after.asp" {
		t.Fatalf("global call-time values = %#v", values)
	}
}

func TestAnalyzeJavaScriptNavigationCapturesNestedFunctionEnvironment(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
function outer() {
  const captured = "closure.asp";
  function inner() { return captured; }
  return inner();
}
function outerArrow() {
  const captured = "arrow-closure.asp";
  const inner = () => captured;
  return inner();
}
location.href = outer();
location.assign(outerArrow());
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 2 {
		t.Fatalf("closure sinks = %#v", result.Sinks)
	}
	if values := result.Sinks[0].Expression.Values; len(values) != 1 || values[0].Text != "closure.asp" {
		t.Fatalf("closure values = %#v", values)
	}
	if values := result.Sinks[1].Expression.Values; len(values) != 1 || values[0].Text != "arrow-closure.asp" {
		t.Fatalf("arrow closure values = %#v", values)
	}
}

func TestAnalyzeJavaScriptNavigationCapturesDefinitionBlockBeforeOuterFunction(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
function outer() {
  function target() { return "outer.asp"; }
  {
    function target() { return "block.asp"; }
    const call = () => target();
    return call();
  }
}
location.href = outer();
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 {
		t.Fatalf("definition block closure sinks = %#v", result.Sinks)
	}
	if values := result.Sinks[0].Expression.Values; len(values) != 1 || values[0].Text != "block.asp" {
		t.Fatalf("definition block closure values = %#v", values)
	}
}

func TestAnalyzeJavaScriptNavigationKeepsNestedFunctionVarLocal(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
var target = () => "global.asp";
function outer() {
  var target = () => "outer.asp";
  function inner() {
    var target = () => "inner.asp";
    return target();
  }
  return inner();
}
location.href = outer();
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 {
		t.Fatalf("nested function var sinks = %#v", result.Sinks)
	}
	if values := result.Sinks[0].Expression.Values; len(values) != 1 || values[0].Text != "inner.asp" {
		t.Fatalf("nested function var values = %#v", values)
	}
}

func TestAnalyzeJavaScriptNavigationResolvesNestedForwardCallable(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
function outer() {
  const first = () => second();
  const second = () => "second.asp";
  return first();
}
location.href = outer();
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 {
		t.Fatalf("nested forward callable sinks = %#v", result.Sinks)
	}
	if values := result.Sinks[0].Expression.Values; len(values) != 1 || values[0].Text != "second.asp" {
		t.Fatalf("nested forward callable values = %#v", values)
	}
}

func TestAnalyzeJavaScriptNavigationCapturesCurrentOuterValue(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
function outer() {
  let captured = "before.asp";
  const inner = () => captured;
  captured = "after.asp";
  return inner();
}
location.href = outer();
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 {
		t.Fatalf("current outer value sinks = %#v", result.Sinks)
	}
	if values := result.Sinks[0].Expression.Values; len(values) != 1 || values[0].Text != "after.asp" {
		t.Fatalf("current outer value values = %#v", values)
	}
}

func TestAnalyzeJavaScriptNavigationResolvesAssignedNestedForwardCallable(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
function outer() {
  let first;
  first = () => second();
  let second = () => "assigned-second.asp";
  return first();
}
location.href = outer();
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 {
		t.Fatalf("assigned nested forward callable sinks = %#v", result.Sinks)
	}
	if values := result.Sinks[0].Expression.Values; len(values) != 1 || values[0].Text != "assigned-second.asp" {
		t.Fatalf("assigned nested forward callable values = %#v", values)
	}
}

func TestAnalyzeJavaScriptNavigationDoesNotCaptureGlobalThroughTopLevelBlock(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
function target() { return "global.asp"; }
{
  const first = () => target();
  location.href = first();
}
location.assign(target());
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 2 {
		t.Fatalf("top-level global capture sinks = %#v", result.Sinks)
	}
	for index, sink := range result.Sinks {
		if values := sink.Expression.Values; len(values) != 1 || values[0].Text != "global.asp" {
			t.Fatalf("top-level global capture value %d = %#v", index, values)
		}
	}
}

func TestAnalyzeJavaScriptNavigationPropagatesGlobalCallEffects(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
let target = "before.asp";
function update() { target = "after.asp"; }
update();
location.href = target;
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 {
		t.Fatalf("global call effects sinks = %#v", result.Sinks)
	}
	if values := result.Sinks[0].Expression.Values; len(values) != 1 || values[0].Text != "after.asp" {
		t.Fatalf("global call effects values = %#v", values)
	}
}

func TestAnalyzeJavaScriptNavigationPropagatesValueUsedCallEffectsInOrder(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
let target = "before.asp";
function update() {
  target = "after.asp";
  return "result.asp";
}

location.href = update();
location.assign(target);
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 2 {
		t.Fatalf("value-used call effects sinks = %#v", result.Sinks)
	}
	if values := result.Sinks[0].Expression.Values; len(values) != 1 || values[0].Text != "result.asp" {
		t.Fatalf("value-used call result = %#v", values)
	}
	if values := result.Sinks[1].Expression.Values; len(values) != 1 || values[0].Text != "after.asp" {
		t.Fatalf("value-used call state = %#v", values)
	}
}

func TestAnalyzeJavaScriptNavigationEvaluatesSideEffectingCallOnceAsSinkValue(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
let path = "a";
function appendPath() {
  path += "b";
  return path + ".asp";
}
location.href = appendPath();
location.assign(path + ".asp");
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 2 {
		t.Fatalf("sinks = %#v", result.Sinks)
	}
	if values := result.Sinks[0].Expression.Values; len(values) != 1 || values[0].Text != "ab.asp" {
		t.Fatalf("side-effecting sink value = %#v", values)
	}
	if values := result.Sinks[1].Expression.Values; len(values) != 1 || values[0].Text != "ab.asp" {
		t.Fatalf("side-effecting sink state = %#v", values)
	}
}

func TestAnalyzeJavaScriptNavigationPreservesSideEffectingCallSourceOrder(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
let order = "";
function first() {
  order += "1";
  return "first";
}
function second() {
  order += "2";
  return "second";
}
location.href = first() + second();
location.assign(order);
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 2 {
		t.Fatalf("sinks = %#v", result.Sinks)
	}
	if values := result.Sinks[0].Expression.Values; len(values) != 1 || values[0].Text != "firstsecond" {
		t.Fatalf("source-order value = %#v", values)
	}
	if values := result.Sinks[1].Expression.Values; len(values) != 1 || values[0].Text != "12" {
		t.Fatalf("source-order state = %#v", values)
	}
}

func TestAnalyzeJavaScriptNavigationEvaluatesSideEffectingCallOnceAsAssignment(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
let path = "a";
function appendPath() {
  path += "b";
  return path;
}
let target;
target = appendPath();
location.href = target + ".asp";
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 {
		t.Fatalf("sinks = %#v", result.Sinks)
	}
	if values := result.Sinks[0].Expression.Values; len(values) != 1 || values[0].Text != "ab.asp" {
		t.Fatalf("side-effecting assignment = %#v", values)
	}
}

func TestAnalyzeJavaScriptNavigationDoesNotCountStaticFunctionSinkEffects(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
let path = "a";
function appendPath() {
  path += "b";
  return path + ".asp";
}
function navigate() {
  location.href = appendPath();
}
navigate();
location.assign(path + ".asp");
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 2 {
		t.Fatalf("sinks = %#v", result.Sinks)
	}
	if values := result.Sinks[0].Expression.Values; len(values) != 1 || values[0].Text != "ab.asp" {
		t.Fatalf("function sink value = %#v", values)
	}
	if values := result.Sinks[1].Expression.Values; len(values) != 1 || values[0].Text != "ab.asp" {
		t.Fatalf("function sink state = %#v", values)
	}
}

func TestAnalyzeJavaScriptNavigationEvaluatesSideEffectingCallOnceInCondition(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
let path = "a";
function appendPath() {
  path += "b";
  return true;
}
if (appendPath()) {
  location.href = path + ".asp";
}
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 {
		t.Fatalf("sinks = %#v", result.Sinks)
	}
	if values := result.Sinks[0].Expression.Values; len(values) != 1 || values[0].Text != "ab.asp" {
		t.Fatalf("side-effecting condition = %#v", values)
	}
}

func TestAnalyzeJavaScriptNavigationEvaluatesSideEffectingCallOnceInFunctionCondition(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
let path = "a";
function appendPath() {
  path += "b";
  return true;
}
function buildPath() {
  if (appendPath()) {
    return path + ".asp";
  }
  return "fallback.asp";
}
location.href = buildPath();
location.assign(path + ".asp");
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 2 {
		t.Fatalf("sinks = %#v", result.Sinks)
	}
	if values := result.Sinks[0].Expression.Values; len(values) != 2 || values[0].Text != "ab.asp" || values[1].Text != "fallback.asp" {
		t.Fatalf("function condition value = %#v", values)
	}
	if values := result.Sinks[1].Expression.Values; len(values) != 1 || values[0].Text != "ab.asp" {
		t.Fatalf("function condition state = %#v", values)
	}
}

func TestAnalyzeJavaScriptNavigationEvaluatesFormActionCallOnce(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
let path = "a";
function appendPath() {
  path += "b";
  return path + ".asp";
}
form.action = appendPath();
form.submit();
location.assign(path + ".asp");
`)
	if err != nil {
		t.Fatal(err)
	}
	sink := navigationFormSubmitSink(t, result)
	if values := sink.Expression.Values; len(values) != 1 || values[0].Text != "ab.asp" {
		t.Fatalf("form action value = %#v", values)
	}
	if values := result.Sinks[len(result.Sinks)-1].Expression.Values; len(values) != 1 || values[0].Text != "ab.asp" {
		t.Fatalf("form action state = %#v", values)
	}
}

type cancelAfterChecksContext struct {
	checks atomic.Int64
	limit  int64
	done   chan struct{}
	once   sync.Once
}

func newCancelAfterChecksContext(limit int64) *cancelAfterChecksContext {
	return &cancelAfterChecksContext{limit: limit, done: make(chan struct{})}
}

func (c *cancelAfterChecksContext) Deadline() (time.Time, bool) {
	return time.Time{}, false
}

func (c *cancelAfterChecksContext) Done() <-chan struct{} {
	return c.done
}

func (c *cancelAfterChecksContext) Err() error {
	if c.checks.Add(1) <= c.limit {
		return nil
	}
	c.once.Do(func() { close(c.done) })
	return context.Canceled
}

func (c *cancelAfterChecksContext) Value(any) any {
	return nil
}

func TestAnalyzeJavaScriptNavigationCancelsDuringLargeInvokedFunction(t *testing.T) {
	var source strings.Builder
	source.WriteString(`
function buildPath() {
  let target = "large";
`)
	for index := 0; index < 20000; index++ {
		source.WriteString("  unknownFlag;\n")
	}
	source.WriteString(`
  return target;
}
location.href = buildPath();
`)
	ctx := newCancelAfterChecksContext(100000)
	result, err := AnalyzeJavaScriptNavigation(ctx, source.String())
	if !errors.Is(err, context.Canceled) || !result.Cancelled || len(result.Sinks) != 0 {
		t.Fatalf("large function cancellation = %#v, err = %v, checks = %d", result, err, ctx.checks.Load())
	}
}

func TestAnalyzeJavaScriptNavigationPropagatesFormCallEffects(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
function update() {
  form.action = "after.asp";
  form.method = "POST";
}
update();
form.submit();
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 2 {
		t.Fatalf("form call effects sinks = %#v", result.Sinks)
	}
	sink := result.Sinks[len(result.Sinks)-1]
	if sink.Kind != "javascriptFormSubmit" || sink.Method != "POST" || len(sink.Expression.Values) != 1 || sink.Expression.Values[0].Text != "after.asp" {
		t.Fatalf("form call effects sink = %#v", sink)
	}
}

func TestAnalyzeJavaScriptNavigationMergesBranchCallEffects(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
let target = "base.asp";
function update(flag) {
  if (flag) target = "one.asp";
  else target = "two.asp";
}
update(unknownFlag);
location.href = target;
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 {
		t.Fatalf("branch call effects sinks = %#v", result.Sinks)
	}
	values := result.Sinks[0].Expression.Values
	if len(values) != 2 || values[0].Text != "one.asp" || values[1].Text != "two.asp" {
		t.Fatalf("branch call effects values = %#v", values)
	}
}

func TestAnalyzeJavaScriptNavigationMergesAbruptBranchCallEffects(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
let target = "base.asp";
function update(flag) {
  if (flag) {
    target = "returned.asp";
    return;
  }
  target = "normal.asp";
}
update(unknownFlag);
location.href = target;
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 {
		t.Fatalf("abrupt branch call effects sinks = %#v", result.Sinks)
	}
	values := result.Sinks[0].Expression.Values
	if len(values) != 2 || values[0].Text != "returned.asp" || values[1].Text != "normal.asp" {
		t.Fatalf("abrupt branch call effects values = %#v", values)
	}
}

func TestAnalyzeJavaScriptNavigationMergesBranchFormCallEffects(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
function update(flag) {
  if (flag) form.method = "POST";
  else form.method = "GET";
}
update(unknownFlag);
form.submit();
`)
	if err != nil {
		t.Fatal(err)
	}
	sink := navigationFormSubmitSink(t, result)
	if sink.Method != "{unknown}" {
		t.Fatalf("branch form method = %#v", sink)
	}
}

func TestAnalyzeJavaScriptNavigationIsolatesCalleeLocalsFromCallEffects(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
let target = "global.asp";
function update() {
  let target = "local.asp";
  target = "changed-local.asp";
}
update();
location.href = target;
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 || len(result.Sinks[0].Expression.Values) != 1 || result.Sinks[0].Expression.Values[0].Text != "global.asp" {
		t.Fatalf("callee local isolation = %#v", result.Sinks)
	}
}

func TestAnalyzeJavaScriptNavigationKeepsGlobalEffectsThroughCallerShadow(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
let target = "global-before.asp";
function update() { target = "global-after.asp"; }
function caller() {
  let target = "caller.asp";
  update();
  return target;
}
location.href = caller();
location.assign(target);
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 2 {
		t.Fatalf("caller shadow effects sinks = %#v", result.Sinks)
	}
	if values := result.Sinks[0].Expression.Values; len(values) != 1 || values[0].Text != "caller.asp" {
		t.Fatalf("caller shadow return = %#v", values)
	}
	if values := result.Sinks[1].Expression.Values; len(values) != 1 || values[0].Text != "global-after.asp" {
		t.Fatalf("caller shadow global = %#v", values)
	}
}

func TestAnalyzeJavaScriptNavigationKeepsGlobalEffectsAfterBlockShadow(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
let target = "global-before.asp";
function update() { target = "global-after.asp"; }
{
  let target = "block.asp";
  update();
}
location.href = target;
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 || len(result.Sinks[0].Expression.Values) != 1 || result.Sinks[0].Expression.Values[0].Text != "global-after.asp" {
		t.Fatalf("block shadow global effect = %#v", result.Sinks)
	}
}

func TestAnalyzeJavaScriptNavigationPropagatesCapturedLexicalEffects(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
function outer() {
  let target = "outer-before.asp";
  function update() { target = "outer-after.asp"; }
  update();
  return target;
}
location.href = outer();
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 || len(result.Sinks[0].Expression.Values) != 1 || result.Sinks[0].Expression.Values[0].Text != "outer-after.asp" {
		t.Fatalf("captured lexical effect = %#v", result.Sinks)
	}
}

func TestAnalyzeJavaScriptNavigationUsesUnknownForRecursiveCallEffects(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
let target = "global.asp";
function update() {
  target = "changed.asp";
  update();
}
update();
location.href = target;
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 {
		t.Fatalf("recursive call effects sinks = %#v", result.Sinks)
	}
	values := result.Sinks[0].Expression.Values
	if len(values) != 1 || values[0].Kind != NavigationValueUnknown {
		t.Fatalf("recursive call effects values = %#v", values)
	}
}

func TestAnalyzeJavaScriptNavigationEvaluatesCasesBeforeEarlierDefault(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
let marker = "base";
switch (unknownFlag) {
default:
  location.href = marker;
  break;
case marker = "later":
  location.assign(marker);
  break;
}
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 2 {
		t.Fatalf("default ordering sinks = %#v", result.Sinks)
	}
	if values := result.Sinks[0].Expression.Values; len(values) != 1 || values[0].Text != "later" {
		t.Fatalf("earlier default values = %#v", values)
	}
	if values := result.Sinks[1].Expression.Values; len(values) != 1 || values[0].Text != "later" {
		t.Fatalf("later case values = %#v", values)
	}
}

func TestAnalyzeJavaScriptNavigationEvaluatesCasesBeforeEarlierDefaultInFunctions(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
function buildPath() {
  let marker = "base";
  switch (unknownFlag) {
  default:
    return marker;
  case marker = "later":
    return marker;
  }
}
location.href = buildPath();
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 {
		t.Fatalf("function default ordering sinks = %#v", result.Sinks)
	}
	values := result.Sinks[0].Expression.Values
	if len(values) != 1 || values[0].Text != "later" {
		t.Fatalf("function default ordering values = %#v", values)
	}
}

func TestAnalyzeJavaScriptNavigationUsesUnknownForLoopAssignments(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
let target = "base.asp";
while (unknownFlag) target = "loop.asp";
location.href = target;
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 {
		t.Fatalf("loop assignment sinks = %#v", result.Sinks)
	}
	values := result.Sinks[0].Expression.Values
	if len(values) != 3 || values[0].Text != "base.asp" || values[1].Text != "loop.asp" || values[2].Kind != NavigationValueUnknown {
		t.Fatalf("loop assignment values = %#v", values)
	}
}

func TestAnalyzeJavaScriptNavigationEvaluatesSequentialFunctionAssignments(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
function buildPath() {
  let target = "first.asp";
  target = "second.asp";
  return target;
}
location.href = buildPath();
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 || len(result.Sinks[0].Expression.Values) != 1 || result.Sinks[0].Expression.Values[0].Text != "second.asp" {
		t.Fatalf("sequential function assignments = %#v", result.Sinks)
	}
}

func TestAnalyzeJavaScriptNavigationMergesFunctionBranchAssignments(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
function buildPath() {
  let target = "base.asp";
  if (unknownFlag) target = "branch.asp";
  return target;
}
location.href = buildPath();
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 {
		t.Fatalf("function branch sinks = %#v", result.Sinks)
	}
	values := result.Sinks[0].Expression.Values
	if len(values) != 2 || values[0].Text != "branch.asp" || values[1].Text != "base.asp" {
		t.Fatalf("function branch values = %#v", values)
	}
}

func TestAnalyzeJavaScriptNavigationUsesUnknownForFunctionLoopAssignments(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
function buildPath() {
  let target = "base.asp";
  while (unknownFlag) target = "loop.asp";
  return target;
}
location.href = buildPath();
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 {
		t.Fatalf("function loop sinks = %#v", result.Sinks)
	}
	values := result.Sinks[0].Expression.Values
	if len(values) != 3 || values[0].Text != "base.asp" || values[1].Text != "loop.asp" || values[2].Kind != NavigationValueUnknown {
		t.Fatalf("function loop values = %#v", values)
	}
}

func TestAnalyzeJavaScriptNavigationModelsFunctionSwitchFallthroughReturns(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
function buildPath() {
  let target = "base";
  switch (unknownFlag) {
  case 1:
    target += "-one";
  case 2:
    target += "-two";
    return target;
  default:
    return "fallback";
  }
}
location.href = buildPath();
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 {
		t.Fatalf("function fallthrough sinks = %#v", result.Sinks)
	}
	values := result.Sinks[0].Expression.Values
	if len(values) != 3 || values[0].Text != "base-one-two" || values[1].Text != "base-two" || values[2].Text != "fallback" {
		t.Fatalf("function fallthrough values = %#v", values)
	}
}

func TestAnalyzeJavaScriptNavigationUsesUnknownForRecursiveFunctions(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
function loop() { return loop(); }
function first() { return second(); }
function second() { return first(); }
location.href = loop();
location.assign(first());
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 2 {
		t.Fatalf("recursive function sinks = %#v", result.Sinks)
	}
	for _, sink := range result.Sinks {
		if len(sink.Expression.Values) != 1 || sink.Expression.Values[0].Kind != NavigationValueUnknown {
			t.Fatalf("recursive function value = %#v", sink.Expression.Values)
		}
	}
}

func TestAnalyzeJavaScriptNavigationBoundsFunctionCallDepth(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
function level0() { return level1(); }
function level1() { return level2(); }
function level2() { return level3(); }
function level3() { return level4(); }
function level4() { return level5(); }
function level5() { return level6(); }
function level6() { return level7(); }
function level7() { return level8(); }
function level8() { return "deep.asp"; }
location.href = level0();
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 || len(result.Sinks[0].Expression.Values) != 1 || result.Sinks[0].Expression.Values[0].Kind != NavigationValueUnknown {
		t.Fatalf("depth guard result = %#v", result.Sinks)
	}
}

func TestAnalyzeJavaScriptNavigationUsesJavaScriptNumericCoercion(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `location.href = 1 + 2;`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 || result.Sinks[0].Expression.Values[0].Text != "3" {
		t.Fatalf("numeric expression = %#v", result.Sinks)
	}
}

func TestAnalyzeJavaScriptNavigationDeduplicatesCandidates(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `location.href = true ? "same.asp" : "same.asp";`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 || len(result.Sinks[0].Expression.Values) != 1 || result.Sinks[0].Expression.Values[0].Text != "same.asp" {
		t.Fatalf("deduplicated values = %#v", result.Sinks)
	}
}

func TestAnalyzeJavaScriptNavigationUsesUTF16RangesAndUnknownFallback(t *testing.T) {
	source := "const target = value;\n😀 location.href = target;\n"
	result, err := AnalyzeJavaScriptNavigation(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 {
		t.Fatalf("sinks = %#v", result.Sinks)
	}
	sink := result.Sinks[0]
	if sink.Expression.Values[0].Kind != NavigationValueUnknown || sink.Expression.Values[0].Text != "{unknown}" {
		t.Fatalf("unknown value = %#v", sink.Expression.Values)
	}
	if sink.Range.Start != strings.Index(source, "😀")+3 {
		t.Fatalf("sink UTF-16 start = %d", sink.Range.Start)
	}
}

func TestAnalyzeJavaScriptNavigationCapsFiniteCandidates(t *testing.T) {
	branches := make([]string, 0, navigationValueLimit+4)
	for index := 0; index < navigationValueLimit+4; index++ {
		branches = append(branches, `"value`+string(rune('a'+index))+`.asp"`)
	}
	expression := branches[len(branches)-1]
	for index := len(branches) - 2; index >= 0; index-- {
		expression = "true ? " + branches[index] + " : " + expression
	}
	source := "location.href = " + expression + ";"
	result, err := AnalyzeJavaScriptNavigation(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 || len(result.Sinks[0].Expression.Values) != navigationValueLimit {
		t.Fatalf("capped values = %#v", result.Sinks)
	}
	last := result.Sinks[0].Expression.Values[len(result.Sinks[0].Expression.Values)-1]
	if last.Kind != NavigationValueUnknown {
		t.Fatalf("capped fallback = %#v", last)
	}
}

func TestAnalyzeJavaScriptNavigationKeepsUnknownAfterTruncatedCandidateSlot(t *testing.T) {
	expression := `unknownFlag ? external() : "finite-00.asp"`
	for index := 1; index < navigationValueLimit; index++ {
		expression = fmt.Sprintf("unknownFlag ? (%s) : %q", expression, fmt.Sprintf("finite-%02d.asp", index))
	}
	result, err := AnalyzeJavaScriptNavigation(context.Background(), "location.href = "+expression+";")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 {
		t.Fatalf("truncated-slot sinks = %#v", result.Sinks)
	}
	values := result.Sinks[0].Expression.Values
	if len(values) != navigationValueLimit {
		t.Fatalf("truncated-slot values = %d, want %d: %#v", len(values), navigationValueLimit, values)
	}
	for index := 0; index < navigationValueLimit-1; index++ {
		want := fmt.Sprintf("finite-%02d.asp", index)
		if values[index].Text != want || values[index].Kind != NavigationValueLiteral {
			t.Fatalf("truncated-slot value %d = %#v, want literal %q", index, values[index], want)
		}
	}
	if last := values[len(values)-1]; last.Text != "{unknown}" || last.Kind != NavigationValueUnknown {
		t.Fatalf("truncated-slot fallback = %#v", last)
	}
}

func TestAnalyzeJavaScriptNavigationCapsExactlyThirtyTwoFiniteValuesOnOverflow(t *testing.T) {
	branches := make([]string, navigationValueLimit+1)
	for index := range branches {
		branches[index] = fmt.Sprintf("finite-%02d.asp", index)
	}
	expression := fmt.Sprintf("%q", branches[len(branches)-1])
	for index := len(branches) - 2; index >= 0; index-- {
		expression = fmt.Sprintf("true ? %q : (%s)", branches[index], expression)
	}
	result, err := AnalyzeJavaScriptNavigation(context.Background(), "location.href = "+expression+";")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 {
		t.Fatalf("exact-overflow sinks = %#v", result.Sinks)
	}
	values := result.Sinks[0].Expression.Values
	if len(values) != navigationValueLimit {
		t.Fatalf("exact-overflow values = %d, want %d: %#v", len(values), navigationValueLimit, values)
	}
	for index := 0; index < navigationValueLimit-1; index++ {
		want := fmt.Sprintf("finite-%02d.asp", index)
		if values[index].Text != want || values[index].Kind != NavigationValueLiteral {
			t.Fatalf("exact-overflow value %d = %#v, want literal %q", index, values[index], want)
		}
	}
	if last := values[len(values)-1]; last.Text != "{unknown}" || last.Kind != NavigationValueUnknown {
		t.Fatalf("exact-overflow fallback = %#v", last)
	}
}

func TestAnalyzeJavaScriptNavigationCandidateOrderIsDeterministic(t *testing.T) {
	expression := `unknownFlag ? external() : "{unknown}"`
	for index := 1; index < navigationValueLimit; index++ {
		expression = fmt.Sprintf("unknownFlag ? (%s) : %q", expression, fmt.Sprintf("finite-%02d.asp", index))
	}
	source := "location.href = " + expression + ";"
	var want []NavigationValue
	for run := 0; run < 8; run++ {
		result, err := AnalyzeJavaScriptNavigation(context.Background(), source)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Sinks) != 1 {
			t.Fatalf("deterministic sinks = %#v", result.Sinks)
		}
		values := result.Sinks[0].Expression.Values
		if want == nil {
			want = append([]NavigationValue(nil), values...)
			continue
		}
		if !reflect.DeepEqual(values, want) {
			t.Fatalf("candidate order changed on run %d: got %#v, want %#v", run, values, want)
		}
	}
}

func TestAnalyzeJavaScriptNavigationEvaluatesUnsupportedCallArgumentsOnceInOrder(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
let target = "base";
function mutate() {
  target += "x";
  return "argument.asp";
}
function consume(value) { return value; }
location.href = external(consume(mutate()));
location.assign(target + ".asp");
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 2 {
		t.Fatalf("unsupported call sinks = %#v", result.Sinks)
	}
	if values := result.Sinks[0].Expression.Values; len(values) != 1 || values[0].Kind != NavigationValueUnknown {
		t.Fatalf("unsupported call value = %#v", values)
	}
	if values := result.Sinks[1].Expression.Values; len(values) != 1 || values[0].Text != "basex.asp" {
		t.Fatalf("unsupported call argument effects = %#v", values)
	}
}

func TestAnalyzeJavaScriptNavigationEvaluatesComputedCalleeBeforeArguments(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
let order = "";
function markCallee() {
  order += "callee";
  return "method";
}
function markArgument() {
  order += "arg";
  return "argument";
}
const receiver = {};
location.href = (receiver[(markCallee(), "method")])(markArgument());
location.assign(order);
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 2 {
		t.Fatalf("computed callee sinks = %#v", result.Sinks)
	}
	if values := result.Sinks[0].Expression.Values; len(values) != 1 || values[0].Kind != NavigationValueUnknown {
		t.Fatalf("computed callee value = %#v", values)
	}
	if values := result.Sinks[1].Expression.Values; len(values) != 1 || values[0].Text != "calleearg" {
		t.Fatalf("computed callee order = %#v", values)
	}
}

func TestAnalyzeJavaScriptNavigationPreservesSupportedCallLookupAfterCalleeEvaluation(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
let order = "";
function target(value) {
  return order;
}
function markArgument() {
  order += "arg";
  return "argument";
}
location.href = (target)(markArgument());
location.assign(order);
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 2 {
		t.Fatalf("supported call sinks = %#v", result.Sinks)
	}
	for index, sink := range result.Sinks {
		if values := sink.Expression.Values; len(values) != 1 || values[0].Text != "arg" {
			t.Fatalf("supported call value %d = %#v", index, values)
		}
	}
}

func TestAnalyzeJavaScriptNavigationSnapshotsCalleeBeforeArgumentMutation(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
function target() { return "before"; }
location.href = target((target = () => "after", "argument"));
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 {
		t.Fatalf("callee snapshot sinks = %#v", result.Sinks)
	}
	if values := result.Sinks[0].Expression.Values; len(values) != 1 || values[0].Text != "before" {
		t.Fatalf("callee snapshot value = %#v", values)
	}
}

func TestAnalyzeJavaScriptNavigationEvaluatesNewCalleeBeforeArguments(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
let order = "";
function markCallee() {
  order += "callee";
  return function Constructor() {};
}
function markArgument() {
  order += "arg";
  return "argument";
}
location.href = new (markCallee())(markArgument());
location.assign(order);
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 2 {
		t.Fatalf("new expression sinks = %#v", result.Sinks)
	}
	if values := result.Sinks[0].Expression.Values; len(values) != 1 || values[0].Kind != NavigationValueUnknown {
		t.Fatalf("new expression value = %#v", values)
	}
	if values := result.Sinks[1].Expression.Values; len(values) != 1 || values[0].Text != "calleearg" {
		t.Fatalf("new expression order = %#v", values)
	}
}

func TestAnalyzeJavaScriptNavigationEvaluatesUnsupportedNewCalleeExpressions(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
let order = "";
function markCallee() {
  order += "callee";
  return "constructor";
}
function markArgument() {
  order += "arg";
  return "argument";
}
const receiver = {};
location.href = new (receiver[(markCallee(), "constructor")])(markArgument());
location.assign(order);
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 2 {
		t.Fatalf("unsupported new expression sinks = %#v", result.Sinks)
	}
	if values := result.Sinks[0].Expression.Values; len(values) != 1 || values[0].Kind != NavigationValueUnknown {
		t.Fatalf("unsupported new expression value = %#v", values)
	}
	if values := result.Sinks[1].Expression.Values; len(values) != 1 || values[0].Text != "calleearg" {
		t.Fatalf("unsupported new expression order = %#v", values)
	}
}

func TestAnalyzeJavaScriptNavigationKeepsSimpleNewExpressionUnknown(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `location.href = new URL("/next.asp");`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 {
		t.Fatalf("simple new expression sinks = %#v", result.Sinks)
	}
	if values := result.Sinks[0].Expression.Values; len(values) != 1 || values[0].Kind != NavigationValueUnknown {
		t.Fatalf("simple new expression value = %#v", values)
	}
}

func TestAnalyzeJavaScriptNavigationCancelsDuringCallEvaluation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := AnalyzeJavaScriptNavigation(ctx, `
const receiver = {};
location.href = receiver[unknownKey](unknownArgument);
`)
	if !errors.Is(err, context.Canceled) || !result.Cancelled || len(result.Sinks) != 0 {
		t.Fatalf("cancelled call evaluation = %#v, err = %v", result, err)
	}
}

func TestAnalyzeJavaScriptNavigationCancelsDuringNewExpressionEvaluation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := AnalyzeJavaScriptNavigation(ctx, `
function markCallee() { return function Constructor() {}; }
function markArgument() { return "argument"; }
location.href = new (markCallee())(markArgument());
`)
	if !errors.Is(err, context.Canceled) || !result.Cancelled || len(result.Sinks) != 0 {
		t.Fatalf("cancelled new expression evaluation = %#v, err = %v", result, err)
	}
}

func TestAnalyzeJavaScriptNavigationKeepsMutuallyExclusiveExpressionAssignmentsSeparate(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
let target = "";
function append(flag) {
  return flag ? (target += "a") : (target += "b");
}
location.href = append(unknownFlag);
location.assign(target);
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 2 {
		t.Fatalf("exclusive branch sinks = %#v", result.Sinks)
	}
	for index, sink := range result.Sinks {
		values := sink.Expression.Values
		if len(values) != 2 || values[0].Text != "a" || values[1].Text != "b" {
			t.Fatalf("exclusive branch values %d = %#v all=%#v", index, values, result.Sinks)
		}
	}
}

func TestAnalyzeJavaScriptNavigationMergesTopLevelConditionalAssignmentEffects(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
let target = "";
location.href = unknownFlag ? (target += "a") : (target += "b");
location.assign(target);
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 2 {
		t.Fatalf("conditional assignment sinks = %#v", result.Sinks)
	}
	for index, sink := range result.Sinks {
		values := sink.Expression.Values
		if len(values) != 2 || values[0].Text != "a" || values[1].Text != "b" {
			t.Fatalf("conditional assignment values %d = %#v", index, values)
		}
	}
}

func TestAnalyzeJavaScriptNavigationShortCircuitEffectsArePathSensitive(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
let target = "";
function mutate() {
  target += "m";
  return "m";
}
false && mutate();
location.href = target;
true || mutate();
location.replace(target);
unknownFlag && mutate();
location.assign(target);
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 3 {
		t.Fatalf("short-circuit sinks = %#v", result.Sinks)
	}
	for index := 0; index < 2; index++ {
		if values := result.Sinks[index].Expression.Values; len(values) != 1 || values[0].Text != "" {
			t.Fatalf("short-circuit skipped value %d = %#v", index, values)
		}
	}
	if values := result.Sinks[2].Expression.Values; len(values) != 2 || values[0].Text != "" || values[1].Text != "m" {
		t.Fatalf("short-circuit uncertain values = %#v all=%#v", values, result.Sinks)
	}
}

func TestAnalyzeJavaScriptNavigationNullishEffectsArePathSensitive(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
let target = "";
function mutate() {
  target += "m";
  return "m";
}
null ?? mutate();
location.href = target;
"left" ?? mutate();
location.replace(target);
unknownFlag ?? mutate();
location.assign(target);
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 3 {
		t.Fatalf("nullish sinks = %#v", result.Sinks)
	}
	for index := 0; index < 2; index++ {
		if values := result.Sinks[index].Expression.Values; len(values) != 1 || values[0].Text != "m" {
			t.Fatalf("nullish value %d = %#v", index, values)
		}
	}
	if values := result.Sinks[2].Expression.Values; len(values) != 2 || values[0].Text != "m" || values[1].Text != "mm" {
		t.Fatalf("nullish uncertain values = %#v", values)
	}
}

func TestAnalyzeJavaScriptNavigationKeepsLiteralUnknownTextDistinctFromUnknownSentinel(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `location.href = unknownFlag ? "{unknown}" : external();`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 {
		t.Fatalf("literal unknown sinks = %#v", result.Sinks)
	}
	values := result.Sinks[0].Expression.Values
	if len(values) != 2 || values[0].Text != "{unknown}" || values[0].Kind != NavigationValueLiteral || values[1].Text != "{unknown}" || values[1].Kind != NavigationValueUnknown {
		t.Fatalf("literal and sentinel values = %#v", values)
	}
}

func TestAnalyzeJavaScriptNavigationHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := AnalyzeJavaScriptNavigation(ctx, `location.href = "cancel.asp";`)
	if !errors.Is(err, context.Canceled) || !result.Cancelled || len(result.Sinks) != 0 {
		t.Fatalf("cancelled result = %#v, err = %v", result, err)
	}
}

func TestAnalyzeJavaScriptNavigationCancelsDuringUTF16Preprocessing(t *testing.T) {
	ctx := newCancelAfterChecksContext(2)
	source := strings.Repeat("x", navigationPreprocessingCheckInterval*4)
	start := time.Now()
	result, err := AnalyzeJavaScriptNavigation(ctx, source)
	if !errors.Is(err, context.Canceled) || !result.Cancelled || len(result.Sinks) != 0 {
		t.Fatalf("preprocessing cancellation = %#v, err = %v, checks = %d", result, err, ctx.checks.Load())
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("preprocessing cancellation took %s", elapsed)
	}
}

func TestAnalyzeJavaScriptNavigationBoundsLargePreprocessingInput(t *testing.T) {
	source := strings.Repeat("x", navigationSourceByteLimit+1)
	start := time.Now()
	result, err := AnalyzeJavaScriptNavigation(context.Background(), source)
	if !errors.Is(err, errNavigationSourceByteLimit) || result.Cancelled || len(result.Sinks) != 0 {
		t.Fatalf("large preprocessing input = %#v, err = %v", result, err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("large preprocessing input took %s", elapsed)
	}
}

func TestAnalyzeJavaScriptNavigationCancelsDuringParsing(t *testing.T) {
	ctx := newCancelAfterChecksContext(25)
	source := strings.Repeat("identifier;\n", 100_000)
	start := time.Now()
	result, err := AnalyzeJavaScriptNavigation(ctx, source)
	if !errors.Is(err, context.Canceled) || !result.Cancelled || len(result.Sinks) != 0 {
		t.Fatalf("parse cancellation = %#v, err = %v, checks = %d", result, err, ctx.checks.Load())
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("parse cancellation took %s", elapsed)
	}
}

func TestBuildNavigationUTF16OffsetsPreservesAstralRanges(t *testing.T) {
	offsets, err := buildNavigationUTF16Offsets(context.Background(), "😀x")
	if err != nil {
		t.Fatal(err)
	}
	want := []int{0, 0, 0, 0, 2, 3}
	if !reflect.DeepEqual(offsets, want) {
		t.Fatalf("UTF-16 offsets = %#v, want %#v", offsets, want)
	}
}

func TestAnalyzeJavaScriptNavigationSkipsUnreachableSinksAfterReturn(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
function buildPath() {
  return "returned.asp";
  location.href = "unreachable.asp";
}
location.href = buildPath();
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 || len(result.Sinks[0].Expression.Values) != 1 || result.Sinks[0].Expression.Values[0].Text != "returned.asp" {
		t.Fatalf("return reachability = %#v", result.Sinks)
	}
}

func TestAnalyzeJavaScriptNavigationKeepsReachableIfBranchSinks(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
function inspect(flag) {
  if (flag) {
    return "one.asp";
    location.href = "unreachable.asp";
  }
  location.href = "reachable.asp";
  return "two.asp";
}
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 || len(result.Sinks[0].Expression.Values) != 1 || result.Sinks[0].Expression.Values[0].Text != "reachable.asp" {
		t.Fatalf("branch reachability = %#v", result.Sinks)
	}
}

func TestAnalyzeJavaScriptNavigationSkipsUnreachableSinksAfterLoopControl(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
while (unknownFlag) {
  continue;
  location.href = "unreachable-continue.asp";
}
for (; unknownFlag;) {
  break;
  location.href = "unreachable-break.asp";
}
location.href = "reachable.asp";
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 || len(result.Sinks[0].Expression.Values) != 1 || result.Sinks[0].Expression.Values[0].Text != "reachable.asp" {
		t.Fatalf("loop reachability = %#v", result.Sinks)
	}
}

func TestAnalyzeJavaScriptNavigationSkipsUnreachableSwitchSinks(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
function inspect(flag) {
  switch (flag) {
  case 1:
    return "one.asp";
    location.href = "unreachable-return.asp";
  default:
    location.href = "reachable-case.asp";
    break;
  }
  location.href = "reachable-after-switch.asp";
}
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 2 {
		t.Fatalf("switch reachability sinks = %#v", result.Sinks)
	}
	if result.Sinks[0].Expression.Values[0].Text != "reachable-case.asp" || result.Sinks[1].Expression.Values[0].Text != "reachable-after-switch.asp" {
		t.Fatalf("switch reachability values = %#v", result.Sinks)
	}
}

func TestAnalyzeJavaScriptNavigationHonorsBlockFunctionShadowing(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
function target() { return "global.asp"; }
{
  const target = () => "block.asp";
  location.href = target();
}
location.href = target();
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 2 {
		t.Fatalf("block shadowing sinks = %#v", result.Sinks)
	}
	if result.Sinks[0].Expression.Values[0].Text != "block.asp" || result.Sinks[1].Expression.Values[0].Text != "global.asp" {
		t.Fatalf("block shadowing values = %#v", result.Sinks)
	}
}

func TestAnalyzeJavaScriptNavigationKeepsVarFunctionScopeOutsideBlock(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
function target() { return "global.asp"; }
{
  var target = () => "var.asp";
  location.href = target();
}
location.href = target();
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 2 {
		t.Fatalf("var scope sinks = %#v", result.Sinks)
	}
	if result.Sinks[0].Expression.Values[0].Text != "var.asp" || result.Sinks[1].Expression.Values[0].Text != "var.asp" {
		t.Fatalf("var scope values = %#v", result.Sinks)
	}
}

func TestAnalyzeJavaScriptNavigationRestoresNestedBlockFunctionScopes(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
function target() { return "global.asp"; }
location.href = target();
{
  {
    const target = () => "nested.asp";
    location.href = target();
  }
  location.href = target();
}
location.href = target();
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 4 {
		t.Fatalf("nested block scopes = %#v", result.Sinks)
	}
	expected := []string{"global.asp", "nested.asp", "global.asp", "global.asp"}
	for index, want := range expected {
		values := result.Sinks[index].Expression.Values
		if len(values) != 1 || values[0].Text != want {
			t.Fatalf("nested block value %d = %#v, want %q", index, values, want)
		}
	}
}

func TestAnalyzeJavaScriptNavigationTreatsBlockClassAsShadowing(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
function target() { return "global.asp"; }
{
  class target {}
  location.href = target();
}
location.href = target();
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 2 {
		t.Fatalf("class shadowing sinks = %#v", result.Sinks)
	}
	if len(result.Sinks[0].Expression.Values) != 1 || result.Sinks[0].Expression.Values[0].Kind != NavigationValueUnknown {
		t.Fatalf("class shadowing inner value = %#v", result.Sinks[0].Expression.Values)
	}
	if len(result.Sinks[1].Expression.Values) != 1 || result.Sinks[1].Expression.Values[0].Text != "global.asp" {
		t.Fatalf("class shadowing outer value = %#v", result.Sinks[1].Expression.Values)
	}
}

func TestAnalyzeJavaScriptNavigationTreatsBlockFunctionAsLexical(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
function target() { return "global.asp"; }
{
  function target() { return "block.asp"; }
  location.href = target();
}
location.href = target();
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 2 || result.Sinks[0].Expression.Values[0].Text != "block.asp" || result.Sinks[1].Expression.Values[0].Text != "global.asp" {
		t.Fatalf("block function values = %#v", result.Sinks)
	}
}

func TestAnalyzeJavaScriptNavigationDoesNotLeakNamedFunctionExpression(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
function target() { return "global.asp"; }
const factory = function target() { return "named-expression.asp"; };
location.href = target();
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 || len(result.Sinks[0].Expression.Values) != 1 || result.Sinks[0].Expression.Values[0].Text != "global.asp" {
		t.Fatalf("named function expression lookup = %#v", result.Sinks)
	}
}

func TestAnalyzeJavaScriptNavigationEvaluatesFunctionLocalArrowScope(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
function buildPath() {
  const target = () => "local.asp";
  return target();
}
location.href = buildPath();
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 || len(result.Sinks[0].Expression.Values) != 1 || result.Sinks[0].Expression.Values[0].Text != "local.asp" {
		t.Fatalf("function local arrow scope = %#v", result.Sinks)
	}
}

func TestAnalyzeJavaScriptNavigationKeepsForLetScopeLocal(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
function target() { return "global.asp"; }
for (let target = () => "loop.asp"; unknownFlag; ) {
  location.href = target();
  break;
}
location.href = target();
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 2 || result.Sinks[0].Expression.Values[0].Text != "loop.asp" || result.Sinks[1].Expression.Values[0].Text != "global.asp" {
		t.Fatalf("for let scope values = %#v", result.Sinks)
	}
}

func TestAnalyzeJavaScriptNavigationDoesNotLeakBlockValueBindings(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
{
  const target = "block.asp";
}
location.href = target;
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 || len(result.Sinks[0].Expression.Values) != 1 || result.Sinks[0].Expression.Values[0].Kind != NavigationValueUnknown {
		t.Fatalf("block value leakage = %#v", result.Sinks)
	}
}

func TestAnalyzeJavaScriptNavigationKeepsSinksBeforeAbruptSwitchFlow(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
function inspect(flag) {
  while (unknownFlag) {
    switch (flag) {
    case 1:
      location.href = "before-return.asp";
      return;
    case 2:
      location.href = "before-break.asp";
      break;
    default:
      location.href = "before-continue.asp";
      continue;
    }
    location.href = "after-switch.asp";
    break;
  }
}
`)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"before-return.asp":   false,
		"before-break.asp":    false,
		"before-continue.asp": false,
		"after-switch.asp":    false,
	}
	for _, sink := range result.Sinks {
		for _, value := range sink.Expression.Values {
			if _, ok := want[value.Text]; ok {
				want[value.Text] = true
			}
		}
	}
	for value, found := range want {
		if !found {
			t.Fatalf("abrupt switch sink %q missing from %#v", value, result.Sinks)
		}
	}
}

func TestAnalyzeJavaScriptNavigationEnforcesBlockBindingTDZ(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
function target() { return "global.asp"; }
{
  location.href = target();
  const target = () => "local.asp";
  location.href = target();
}
location.href = target();
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 3 {
		t.Fatalf("block TDZ sinks = %#v", result.Sinks)
	}
	if values := result.Sinks[0].Expression.Values; len(values) != 1 || values[0].Kind != NavigationValueUnknown {
		t.Fatalf("pre-declaration lookup = %#v", values)
	}
	if values := result.Sinks[1].Expression.Values; len(values) != 1 || values[0].Text != "local.asp" {
		t.Fatalf("post-declaration lookup = %#v", values)
	}
	if values := result.Sinks[2].Expression.Values; len(values) != 1 || values[0].Text != "global.asp" {
		t.Fatalf("post-block lookup = %#v", values)
	}
}

func TestAnalyzeJavaScriptNavigationHoistsBlockFunctionDeclarations(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
function target() { return "global.asp"; }
{
  location.href = target();
  function target() { return "hoisted.asp"; }
  location.href = target();
}
location.href = target();
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 3 {
		t.Fatalf("hoisted function sinks = %#v", result.Sinks)
	}
	for index, want := range []string{"hoisted.asp", "hoisted.asp", "global.asp"} {
		values := result.Sinks[index].Expression.Values
		if len(values) != 1 || values[0].Text != want {
			t.Fatalf("hoisted function value %d = %#v, want %q", index, values, want)
		}
	}
}

func TestAnalyzeJavaScriptNavigationEnforcesTopLevelLexicalTDZ(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
location.href = target();
const target = () => "after-const.asp";
location.href = target();
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 2 {
		t.Fatalf("top-level const TDZ sinks = %#v", result.Sinks)
	}
	if values := result.Sinks[0].Expression.Values; len(values) != 1 || values[0].Kind != NavigationValueUnknown {
		t.Fatalf("top-level pre-declaration lookup = %#v", values)
	}
	if values := result.Sinks[1].Expression.Values; len(values) != 1 || values[0].Text != "after-const.asp" {
		t.Fatalf("top-level post-declaration lookup = %#v", values)
	}
}

func TestAnalyzeJavaScriptNavigationEnforcesTopLevelLetTDZ(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
location.href = target();
let target = () => "after-let.asp";
location.href = target();
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 2 {
		t.Fatalf("top-level let TDZ sinks = %#v", result.Sinks)
	}
	if values := result.Sinks[0].Expression.Values; len(values) != 1 || values[0].Kind != NavigationValueUnknown {
		t.Fatalf("top-level let pre-declaration lookup = %#v", values)
	}
	if values := result.Sinks[1].Expression.Values; len(values) != 1 || values[0].Text != "after-let.asp" {
		t.Fatalf("top-level let post-declaration lookup = %#v", values)
	}
}

func TestAnalyzeJavaScriptNavigationEnforcesTopLevelClassShadowing(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
location.href = target();
class target {}
location.href = target();
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 2 {
		t.Fatalf("top-level class sinks = %#v", result.Sinks)
	}
	for index, sink := range result.Sinks {
		if len(sink.Expression.Values) != 1 || sink.Expression.Values[0].Kind != NavigationValueUnknown {
			t.Fatalf("top-level class lookup %d = %#v", index, sink.Expression.Values)
		}
	}
}

func TestAnalyzeJavaScriptNavigationPreservesTopLevelVarAndFunctionHoisting(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
location.href = target();
var target = () => "var-target.asp";
location.href = target();
location.assign(hoisted());
function hoisted() { return "function-target.asp"; }
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 3 {
		t.Fatalf("top-level var/function sinks = %#v", result.Sinks)
	}
	if values := result.Sinks[0].Expression.Values; len(values) != 1 || values[0].Kind != NavigationValueUnknown {
		t.Fatalf("top-level pre-declaration var lookup = %#v", values)
	}
	for index, want := range []string{"var-target.asp", "function-target.asp"} {
		values := result.Sinks[index+1].Expression.Values
		if len(values) != 1 || values[0].Text != want {
			t.Fatalf("top-level hoisting value %d = %#v, want %q", index+1, values, want)
		}
	}
}

func TestAnalyzeJavaScriptNavigationUsesSharedSwitchLexicalTDZ(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
function target() { return "global.asp"; }
function inspect(flag) {
  switch (flag) {
  case 1:
    location.href = target();
    break;
  case 2:
    const target = () => "case.asp";
    location.href = target();
    break;
  }
}
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 2 {
		t.Fatalf("switch lexical sinks = %#v", result.Sinks)
	}
	if values := result.Sinks[0].Expression.Values; len(values) != 1 || values[0].Kind != NavigationValueUnknown {
		t.Fatalf("earlier case TDZ lookup = %#v", values)
	}
	if values := result.Sinks[1].Expression.Values; len(values) != 1 || values[0].Text != "case.asp" {
		t.Fatalf("later case lookup = %#v", values)
	}
}

func TestAnalyzeJavaScriptNavigationActivatesSwitchLexicalsOnFallthrough(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
function inspect(flag) {
  switch (flag) {
  case 1:
    const target = () => "fallthrough.asp";
  case 2:
    location.href = target();
    break;
  }
}
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 {
		t.Fatalf("switch fallthrough lexical sinks = %#v", result.Sinks)
	}
	values := result.Sinks[0].Expression.Values
	if len(values) != 2 || values[0].Text != "fallthrough.asp" || values[1].Kind != NavigationValueUnknown {
		t.Fatalf("switch fallthrough lexical values = %#v", values)
	}
}

func TestAnalyzeJavaScriptNavigationActivatesSwitchVarInitializersInSourceOrder(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
function target() { return "global.asp"; }
function inspect(flag) {
  switch (flag) {
  case 1:
    location.href = target();
    break;
  case 2:
    var target = () => "switch-var.asp";
    location.href = target();
    break;
  }
}
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 2 {
		t.Fatalf("switch var sinks = %#v", result.Sinks)
	}
	if values := result.Sinks[0].Expression.Values; len(values) != 1 || values[0].Kind != NavigationValueUnknown {
		t.Fatalf("pre-assignment var lookup = %#v", values)
	}
	if values := result.Sinks[1].Expression.Values; len(values) != 1 || values[0].Text != "switch-var.asp" {
		t.Fatalf("post-assignment var lookup = %#v", values)
	}
}

func TestAnalyzeJavaScriptNavigationActivatesFunctionVarInitializersInSourceOrder(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
function buildPath() {
  location.href = target();
  var target = () => "function-var.asp";
  location.assign(target());
}
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 2 {
		t.Fatalf("function var sinks = %#v", result.Sinks)
	}
	if values := result.Sinks[0].Expression.Values; len(values) != 1 || values[0].Kind != NavigationValueUnknown {
		t.Fatalf("function pre-assignment var lookup = %#v", values)
	}
	if values := result.Sinks[1].Expression.Values; len(values) != 1 || values[0].Text != "function-var.asp" {
		t.Fatalf("function post-assignment var lookup = %#v", values)
	}
}

func TestAnalyzeJavaScriptNavigationPreservesFunctionDeclarationThroughUninitializedVar(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
location.href = target();
var target;
location.assign(target());
function target() { return "hoisted-function.asp"; }
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 2 {
		t.Fatalf("uninitialized var sinks = %#v", result.Sinks)
	}
	for index, sink := range result.Sinks {
		if values := sink.Expression.Values; len(values) != 1 || values[0].Text != "hoisted-function.asp" {
			t.Fatalf("uninitialized var value %d = %#v", index, values)
		}
	}
}

func TestAnalyzeJavaScriptNavigationKeepsSwitchFunctionDeclarationsScoped(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
function inspect(flag) {
  switch (flag) {
  case 1:
    location.href = target();
    break;
  case 2:
    function target() { return "switch-function.asp"; }
    location.href = target();
    break;
  }
}
function target() { return "global.asp"; }
location.href = target();
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 3 {
		t.Fatalf("switch function sinks = %#v", result.Sinks)
	}
	if values := result.Sinks[0].Expression.Values; len(values) != 1 || values[0].Text != "switch-function.asp" {
		t.Fatalf("switch function earlier case lookup = %#v", values)
	}
	if values := result.Sinks[1].Expression.Values; len(values) != 1 || values[0].Text != "switch-function.asp" {
		t.Fatalf("switch function later case lookup = %#v", values)
	}
	if values := result.Sinks[2].Expression.Values; len(values) != 1 || values[0].Text != "global.asp" {
		t.Fatalf("switch function outer lookup = %#v", values)
	}
}

func TestAnalyzeJavaScriptNavigationMergesSwitchVarCallableAssignments(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
switch (unknownFlag) {
case 1:
  var target = () => "one.asp";
  break;
default:
  var target = () => "two.asp";
  break;
}
location.href = target();
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 {
		t.Fatalf("switch callable merge sinks = %#v", result.Sinks)
	}
	values := result.Sinks[0].Expression.Values
	if len(values) != 1 || values[0].Kind != NavigationValueUnknown {
		t.Fatalf("switch callable merge values = %#v", values)
	}
}

func TestAnalyzeJavaScriptNavigationPreservesFunctionBlockAssignments(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
function buildPath() {
  let target = "base.asp";
  if (unknownFlag) {
    target = "branch.asp";
  }
  return target;
}
location.href = buildPath();
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 1 {
		t.Fatalf("braced function assignment sinks = %#v", result.Sinks)
	}
	values := result.Sinks[0].Expression.Values
	if len(values) != 2 || values[0].Text != "branch.asp" || values[1].Text != "base.asp" {
		t.Fatalf("braced function assignment values = %#v", values)
	}
}

func TestAnalyzeJavaScriptNavigationRestoresOnlyNestedBlockDeclarations(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `
function buildPath() {
  let target = "outer.asp";
  {
    target = "assigned.asp";
  }
  {
    const target = "inner.asp";
    location.href = target;
  }
  return target;
}
location.href = buildPath();
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 2 {
		t.Fatalf("nested block state sinks = %#v", result.Sinks)
	}
	if values := result.Sinks[0].Expression.Values; len(values) != 1 || values[0].Text != "inner.asp" {
		t.Fatalf("nested block assignment value = %#v", values)
	}
	if values := result.Sinks[1].Expression.Values; len(values) != 1 || values[0].Text != "assigned.asp" {
		t.Fatalf("nested block return value = %#v", values)
	}
}

func navigationFormSubmitSink(t *testing.T, result NavigationAnalysis) NavigationSink {
	t.Helper()
	for _, sink := range result.Sinks {
		if sink.Kind == "javascriptFormSubmit" {
			return sink
		}
	}
	t.Fatalf("form submit sink missing from %#v", result.Sinks)
	return NavigationSink{}
}

func TestAnalyzeJavaScriptNavigationTracksFormSubmitMethods(t *testing.T) {
	tests := []struct {
		name   string
		source string
		method string
	}{
		{name: "default GET", source: `form.action = "default.asp"; form.submit();`, method: "GET"},
		{name: "explicit GET", source: `form.action = "get.asp"; form.method = "get"; form.submit();`, method: "GET"},
		{name: "explicit POST", source: `form.action = "post.asp"; form.method = "POST"; form.submit();`, method: "POST"},
		{name: "branch unknown", source: `form.action = "branch.asp"; if (unknownFlag) form.method = "GET"; else form.method = "POST"; form.submit();`, method: "{unknown}"},
		{name: "dynamic unknown", source: `form.action = "dynamic.asp"; form.method = unknownMethod; form.submit();`, method: "{unknown}"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := AnalyzeJavaScriptNavigation(context.Background(), test.source)
			if err != nil {
				t.Fatal(err)
			}
			sink := navigationFormSubmitSink(t, result)
			if sink.Method != test.method {
				t.Fatalf("form method = %q, want %q; sinks = %#v", sink.Method, test.method, result.Sinks)
			}
		})
	}
}

func TestAnalyzeJavaScriptNavigationBoundsLargeSwitchWorkAndKeepsUnknownSinks(t *testing.T) {
	const cases = 4000
	var source strings.Builder
	source.WriteString("switch (unknownFlag) {\n")
	for index := 0; index < cases; index++ {
		fmt.Fprintf(&source, "case %d:\n  location.href = %q;\n  break;\n", index, fmt.Sprintf("case-%04d.asp", index))
	}
	source.WriteString("}\nlocation.assign(unknownTarget);\n")

	start := time.Now()
	result, err := AnalyzeJavaScriptNavigation(context.Background(), source.String())
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("large switch evaluation took %s, budget=%d", elapsed, NavigationEvaluationWorkLimit)
	}
	if len(result.Sinks) != cases+1 {
		t.Fatalf("large switch sinks = %d, want %d", len(result.Sinks), cases+1)
	}
	unknown := 0
	for _, sink := range result.Sinks {
		for _, value := range sink.Expression.Values {
			if value.Kind == NavigationValueUnknown {
				unknown++
				break
			}
		}
	}
	if unknown == 0 {
		t.Fatalf("large switch did not produce an unknown fallback: first=%#v", result.Sinks[0])
	}
}

func TestAnalyzeJavaScriptNavigationBoundsManyDistinctFallbackSinks(t *testing.T) {
	const cases = 8000
	var source strings.Builder
	source.WriteString("switch (unknownFlag) {\n")
	for index := 0; index < cases; index++ {
		fmt.Fprintf(&source, "case %d:\n  location.href = %q;\n  break;\n", index, fmt.Sprintf("many-distinct-%04d.asp", index))
	}
	source.WriteString("}\n")

	start := time.Now()
	result, err := AnalyzeJavaScriptNavigation(context.Background(), source.String())
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("many-distinct fallback took %s", elapsed)
	}
	if len(result.Sinks) != cases {
		t.Fatalf("many-distinct fallback sinks = %d, want %d", len(result.Sinks), cases)
	}
	occurrences := make(map[SourceRange]struct{}, len(result.Sinks))
	for _, sink := range result.Sinks {
		if _, duplicate := occurrences[sink.occurrence]; duplicate {
			t.Fatalf("duplicate sink occurrence = %#v", sink)
		}
		occurrences[sink.occurrence] = struct{}{}
	}
}

func TestAnalyzeJavaScriptNavigationFallbackKeepsResolvedSinksExact(t *testing.T) {
	const cases = 4000
	var source strings.Builder
	source.WriteString(`location.href = "early.asp";
switch (unknownFlag) {
`)
	for index := 0; index < cases; index++ {
		fmt.Fprintf(&source, "case %d:\n  location.href = %q;\n  break;\n", index, fmt.Sprintf("fallback-%04d.asp", index))
	}
	source.WriteString(`}
location.assign("late.asp");
`)

	result, err := AnalyzeJavaScriptNavigation(context.Background(), source.String())
	if err != nil {
		t.Fatal(err)
	}
	var early, late *NavigationSink
	for index := range result.Sinks {
		switch result.Sinks[index].Expression.Text {
		case `"early.asp"`:
			early = &result.Sinks[index]
		case `"late.asp"`:
			late = &result.Sinks[index]
		}
	}
	if early == nil || len(early.Expression.Values) != 1 || early.Expression.Values[0].Text != "early.asp" || early.Expression.Values[0].Kind != NavigationValueLiteral {
		t.Fatalf("early exact sink = %#v", early)
	}
	if late == nil || len(late.Expression.Values) != 1 || late.Expression.Values[0].Kind != NavigationValueUnknown {
		t.Fatalf("late fallback sink = %#v", late)
	}
}

func TestAnalyzeJavaScriptNavigationFallbackDistinguishesSameStatementSinks(t *testing.T) {
	const noiseTypes = 40_000
	var source strings.Builder
	source.WriteString(`location.assign("early-same-statement.asp"), noise<`)
	for index := 0; index < noiseTypes; index++ {
		if index > 0 {
			source.WriteString("|")
		}
		source.WriteString(`"noise"`)
	}
	source.WriteString(`>(), location.assign("late-same-statement.asp");`)

	result, err := AnalyzeJavaScriptNavigation(context.Background(), source.String())
	if err != nil {
		t.Fatal(err)
	}
	var early, late *NavigationSink
	locationSinks := 0
	for index := range result.Sinks {
		if result.Sinks[index].Kind != "javascriptLocation" {
			continue
		}
		locationSinks++
		switch result.Sinks[index].Expression.Text {
		case `"early-same-statement.asp"`:
			early = &result.Sinks[index]
		case `"late-same-statement.asp"`:
			late = &result.Sinks[index]
		}
	}
	if locationSinks != 2 {
		t.Fatalf("same-statement location sinks = %d, sinks = %#v", locationSinks, result.Sinks)
	}
	if early == nil || len(early.Expression.Values) != 1 || early.Expression.Values[0].Text != "early-same-statement.asp" || early.Expression.Values[0].Kind != NavigationValueLiteral {
		t.Fatalf("same-statement early sink = %#v", early)
	}
	if late == nil || len(late.Expression.Values) != 1 || late.Expression.Values[0].Kind != NavigationValueUnknown {
		t.Fatalf("same-statement late sink = %#v", late)
	}
	repeated, err := AnalyzeJavaScriptNavigation(context.Background(), source.String())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(repeated, result) {
		t.Fatal("same-statement fallback changed between runs")
	}
}

func TestNavigationResolvedOccurrenceIndexHonorsCancellation(t *testing.T) {
	ctx := newCancelAfterChecksContext(1)
	analyzer := &navigationAnalyzer{
		ctx: ctx,
	}
	analyzer.sinks = make([]NavigationSink, int(navigationEvaluationWorkCheckInterval)*2)
	if resolved := analyzer.indexResolvedNavigationSinks(); resolved != nil {
		t.Fatalf("cancelled resolved occurrence index = %d entries", len(resolved))
	}
	if !errors.Is(analyzer.cancelErr, context.Canceled) {
		t.Fatalf("resolved occurrence index cancellation = %v", analyzer.cancelErr)
	}
}

func TestNavigationSinkOrderingHonorsCancellation(t *testing.T) {
	const count = 1024
	analyzer := &navigationAnalyzer{
		ctx:   newCancelAfterChecksContext(1),
		sinks: make([]NavigationSink, count),
	}
	for index := range analyzer.sinks {
		start := count - index
		analyzer.sinks[index].Range = SourceRange{Start: start, End: start}
		analyzer.sinks[index].occurrence = SourceRange{ByteStart: start, ByteEnd: start + 1}
	}
	if err := analyzer.sortNavigationSinks(); !errors.Is(err, context.Canceled) {
		t.Fatalf("sink ordering cancellation = %v", err)
	}
	if !errors.Is(analyzer.cancelErr, context.Canceled) {
		t.Fatalf("sink ordering analyzer cancellation = %v", analyzer.cancelErr)
	}
}

func TestNavigationSinkOrderingBoundsMaximumList(t *testing.T) {
	const count = int(navigationSinkCountLimit)
	analyzer := &navigationAnalyzer{
		ctx:   context.Background(),
		sinks: make([]NavigationSink, count),
	}
	for index := range analyzer.sinks {
		start := count - index
		analyzer.sinks[index].Range = SourceRange{Start: start, End: start}
		analyzer.sinks[index].occurrence = SourceRange{ByteStart: start, ByteEnd: start + 1}
	}
	if err := analyzer.sortNavigationSinks(); err != nil {
		t.Fatal(err)
	}
	for index, sink := range analyzer.sinks {
		if sink.Range.Start != index+1 {
			t.Fatalf("maximum sink ordering at %d = %d", index, sink.Range.Start)
		}
	}
}

func TestAnalyzeJavaScriptNavigationBelowBudgetPreservesExactSourceOrder(t *testing.T) {
	result, err := AnalyzeJavaScriptNavigation(context.Background(), `location.assign("first.asp");
location.href = "second.asp";
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 2 {
		t.Fatalf("below-budget sinks = %#v", result.Sinks)
	}
	for index, want := range []string{"first.asp", "second.asp"} {
		sink := result.Sinks[index]
		if len(sink.Expression.Values) != 1 || sink.Expression.Values[0].Text != want || sink.Expression.Values[0].Kind != NavigationValueLiteral {
			t.Fatalf("below-budget sink %d = %#v, want %q", index, sink, want)
		}
	}
}

func TestAnalyzeJavaScriptNavigationFallbackDoesNotDuplicateFormSubmitMethods(t *testing.T) {
	const cases = 4000
	var source strings.Builder
	source.WriteString(`form.action = "before.asp";
form.method = "POST";
form.submit();
switch (unknownFlag) {
`)
	for index := 0; index < cases; index++ {
		fmt.Fprintf(&source, "case %d:\n  location.href = %q;\n  break;\n", index, fmt.Sprintf("form-fallback-%04d.asp", index))
	}
	source.WriteString(`}
form.action = "after.asp";
form.method = "GET";
form.submit();
`)

	result, err := AnalyzeJavaScriptNavigation(context.Background(), source.String())
	if err != nil {
		t.Fatal(err)
	}
	var submits []NavigationSink
	for _, sink := range result.Sinks {
		if sink.Kind == "javascriptFormSubmit" {
			submits = append(submits, sink)
		}
	}
	if len(submits) != 2 {
		t.Fatalf("form submit sinks = %#v", submits)
	}
	if submits[0].Method != "POST" || len(submits[0].Expression.Values) != 1 || submits[0].Expression.Values[0].Text != "before.asp" {
		t.Fatalf("resolved form submit = %#v", submits[0])
	}
	if submits[1].Method != "{unknown}" || len(submits[1].Expression.Values) != 1 || submits[1].Expression.Values[0].Kind != NavigationValueUnknown {
		t.Fatalf("fallback form submit = %#v", submits[1])
	}
}

func TestAnalyzeJavaScriptNavigationFallbackHonorsCancellation(t *testing.T) {
	const cases = 4000
	var source strings.Builder
	source.WriteString("switch (unknownFlag) {\n")
	for index := 0; index < cases; index++ {
		fmt.Fprintf(&source, "case %d:\n  location.href = %q;\n  break;\n", index, fmt.Sprintf("cancel-fallback-%04d.asp", index))
	}
	source.WriteString("}\n")
	ctx := newCancelAfterChecksContext(int64(NavigationEvaluationWorkLimit/navigationEvaluationWorkCheckInterval) + 64)
	start := time.Now()
	result, err := AnalyzeJavaScriptNavigation(ctx, source.String())
	if !errors.Is(err, context.Canceled) || !result.Cancelled {
		t.Fatalf("fallback cancellation = %#v, err = %v, checks = %d", result, err, ctx.checks.Load())
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("fallback cancellation took %s", elapsed)
	}
}

func TestAnalyzeJavaScriptNavigationFallbackBoundsDeepMemberChains(t *testing.T) {
	const cases = 4000
	const depth = 5000
	var source strings.Builder
	source.WriteString("location.href = \"early-deep.asp\";\n")
	source.WriteString("switch (unknownFlag) {\n")
	for index := 0; index < cases; index++ {
		fmt.Fprintf(&source, "case %d:\n  location.href = %q;\n  break;\n", index, fmt.Sprintf("deep-fallback-%04d.asp", index))
	}
	source.WriteString("}\nlocation.href = root")
	for index := 0; index < depth; index++ {
		fmt.Fprintf(&source, ".member%d", index)
	}
	source.WriteString(";\n")

	start := time.Now()
	result, err := AnalyzeJavaScriptNavigation(context.Background(), source.String())
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("deep fallback took %s", elapsed)
	}
	for _, sink := range result.Sinks {
		if sink.Expression.Text == `"early-deep.asp"` {
			if len(sink.Expression.Values) != 1 || sink.Expression.Values[0].Text != "early-deep.asp" || sink.Expression.Values[0].Kind != NavigationValueLiteral {
				t.Fatalf("early deep sink = %#v", sink)
			}
			return
		}
	}
	t.Fatalf("early deep sink missing from %d sinks", len(result.Sinks))
}

func TestAnalyzeJavaScriptNavigationLargeSwitchFallbackIsDeterministic(t *testing.T) {
	const cases = 3000
	var source strings.Builder
	source.WriteString("switch (unknownFlag) {\n")
	for index := 0; index < cases; index++ {
		fmt.Fprintf(&source, "case %d:\n  location.href = %q;\n  break;\n", index, fmt.Sprintf("deterministic-%04d.asp", index))
	}
	source.WriteString("}\n")

	var want NavigationAnalysis
	for run := 0; run < 3; run++ {
		result, err := AnalyzeJavaScriptNavigation(context.Background(), source.String())
		if err != nil {
			t.Fatal(err)
		}
		if run == 0 {
			want = result
			continue
		}
		if !reflect.DeepEqual(result, want) {
			t.Fatalf("large switch fallback changed on run %d", run)
		}
	}
}

func buildLargeLiveBindingNavigationSource(bindings, cases int) string {
	var source strings.Builder
	for index := 0; index < bindings; index++ {
		fmt.Fprintf(&source, "let live%d = %q;\n", index, fmt.Sprintf("base-%04d.asp", index))
	}
	source.WriteString("function emit(flag) {\n  if (flag) {\n")
	for index := 0; index < bindings; index++ {
		fmt.Fprintf(&source, "    live%d = %q;\n", index, fmt.Sprintf("then-%04d.asp", index))
	}
	source.WriteString("  } else {\n")
	for index := 0; index < bindings; index++ {
		fmt.Fprintf(&source, "    live%d = %q;\n", index, fmt.Sprintf("else-%04d.asp", index))
	}
	source.WriteString("  }\n  return live0;\n}\n")
	source.WriteString("switch (unknownFlag) {\n")
	for index := 0; index < cases; index++ {
		fmt.Fprintf(&source, "case %d:\n  location.href = emit(unknownFlag);\n  break;\n", index)
	}
	source.WriteString("}\nlocation.assign(unknownTarget);\n")
	return source.String()
}

func TestAnalyzeJavaScriptNavigationBoundsLiveBindingsAcrossSwitchCalls(t *testing.T) {
	const bindings = 2200
	const cases = 220
	source := buildLargeLiveBindingNavigationSource(bindings, cases)
	start := time.Now()
	result, err := AnalyzeJavaScriptNavigation(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 8*time.Second {
		t.Fatalf("large live-binding switch evaluation took %s, budget=%d", elapsed, NavigationEvaluationWorkLimit)
	}
	if len(result.Sinks) != cases+1 {
		t.Fatalf("large live-binding sinks = %d, want %d", len(result.Sinks), cases+1)
	}
	unknown := 0
	for _, sink := range result.Sinks {
		for _, value := range sink.Expression.Values {
			if value.Kind == NavigationValueUnknown {
				unknown++
				break
			}
		}
	}
	if unknown == 0 {
		t.Fatalf("large live-binding switch did not retain an unknown fallback")
	}
}

func TestAnalyzeJavaScriptNavigationLargeLiveBindingFallbackIsDeterministic(t *testing.T) {
	source := buildLargeLiveBindingNavigationSource(1800, 180)
	var want NavigationAnalysis
	for run := 0; run < 2; run++ {
		result, err := AnalyzeJavaScriptNavigation(context.Background(), source)
		if err != nil {
			t.Fatal(err)
		}
		if run == 0 {
			want = result
			continue
		}
		if !reflect.DeepEqual(result, want) {
			t.Fatalf("large live-binding fallback changed on run %d", run)
		}
	}
}

func TestNavigationEvaluationWorkBudgetBoundary(t *testing.T) {
	analyzer := &navigationAnalyzer{
		ctx:  context.Background(),
		work: &navigationWorkBudget{limit: NavigationEvaluationWorkLimit},
	}
	analyzer.work.owner = analyzer
	if !analyzer.consumeNavigationWork(NavigationEvaluationWorkLimit - 1) {
		t.Fatal("budget exhausted before the boundary")
	}
	if analyzer.consumeNavigationWork(1) {
		t.Fatal("budget accepted work at the boundary")
	}
	if !analyzer.navigationWorkExhausted() || analyzer.work.used != NavigationEvaluationWorkLimit {
		t.Fatalf("budget boundary state = %#v", analyzer.work)
	}
	if analyzer.consumeNavigationWork(1) {
		t.Fatal("exhausted budget accepted additional work")
	}
}

func newNavigationWorkTestAnalyzer(ctx context.Context) *navigationAnalyzer {
	analyzer := &navigationAnalyzer{
		ctx:  ctx,
		work: &navigationWorkBudget{limit: NavigationEvaluationWorkLimit},
	}
	analyzer.work.owner = analyzer
	return analyzer
}

func largeNavigationValueMap(count int) map[string]navigationFiniteSet {
	values := make(map[string]navigationFiniteSet, count)
	for index := 0; index < count; index++ {
		values[fmt.Sprintf("live%d", index)] = literalNavigationSet(fmt.Sprintf("value%d.asp", index))
	}
	return values
}

func TestNavigationEvaluationCancelsDuringLargeStateClone(t *testing.T) {
	analyzer := newNavigationWorkTestAnalyzer(newCancelAfterChecksContext(1))
	cloned := cloneNavigationValues(largeNavigationValueMap(5000), analyzer.work)
	if !errors.Is(analyzer.cancelErr, context.Canceled) {
		t.Fatalf("large state clone cancellation = %v", analyzer.cancelErr)
	}
	if len(cloned) != 0 {
		t.Fatalf("large state clone returned partial state: %d entries", len(cloned))
	}
}

func TestNavigationEvaluationCancelsDuringLargeStateMerge(t *testing.T) {
	analyzer := newNavigationWorkTestAnalyzer(newCancelAfterChecksContext(1))
	values := largeNavigationValueMap(5000)
	first := navigationState{values: values, work: analyzer.work}
	second := navigationState{values: values, work: analyzer.work}
	merged := analyzer.mergeNavigationStates(first, second)
	if !errors.Is(analyzer.cancelErr, context.Canceled) {
		t.Fatalf("large state merge cancellation = %v", analyzer.cancelErr)
	}
	if merged.work != analyzer.work {
		t.Fatalf("large state merge lost shared budget")
	}
}

func TestAnalyzeJavaScriptNavigationNormalLiveBindingPerformance(t *testing.T) {
	const bindings = 24
	const cases = 8
	var source strings.Builder
	for index := 0; index < bindings; index++ {
		fmt.Fprintf(&source, "let live%d = %q;\n", index, fmt.Sprintf("normal-%04d.asp", index))
	}
	source.WriteString("function read() { return live0; }\nswitch (unknownFlag) {\n")
	for index := 0; index < cases; index++ {
		fmt.Fprintf(&source, "case %d:\n  location.href = read();\n  break;\n", index)
	}
	source.WriteString("}\n")
	start := time.Now()
	result, err := AnalyzeJavaScriptNavigation(context.Background(), source.String())
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("normal live-binding evaluation took %s", elapsed)
	}
	if len(result.Sinks) != cases {
		t.Fatalf("normal live-binding sinks = %d, want %d", len(result.Sinks), cases)
	}
	for _, sink := range result.Sinks {
		if len(sink.Expression.Values) != 1 || sink.Expression.Values[0].Text != "normal-0000.asp" || sink.Expression.Values[0].Kind == NavigationValueUnknown {
			t.Fatalf("normal live-binding sink = %#v", sink)
		}
	}
}

func TestAnalyzeJavaScriptNavigationAttributesCalledSinksToCaller(t *testing.T) {
	source := `function navigate(target) { forward(target); } function forward(target) { location.href = target; }
;
var target = "first-result.asp";
;
/* 😀 */ navigate(target); navigate("other.asp");`
	result, err := AnalyzeJavaScriptNavigation(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 2 {
		t.Fatalf("sinks = %#v", result.Sinks)
	}
	for i, want := range []string{"first-result.asp", "other.asp"} {
		sink := result.Sinks[i]
		if len(sink.Expression.Values) != 1 || sink.Expression.Values[0].Text != want {
			t.Fatalf("sink[%d] = %#v", i, sink)
		}
		if !strings.Contains(sink.Snippet, "navigate(") || !strings.Contains(source[sink.Expression.Range.ByteStart:sink.Expression.Range.ByteEnd], "navigate(") {
			t.Fatalf("caller source = %#v", sink)
		}
	}
}

func TestAnalyzeJavaScriptNavigationKeepsUnexecutedSinkFallback(t *testing.T) {
	source := `function go(target) { location.assign("first.asp"); try { location.href = target; } catch (error) {} } go("next.asp");`
	result, err := AnalyzeJavaScriptNavigation(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 2 {
		t.Fatalf("sinks = %#v, want resolved caller and conservative try fallback", result.Sinks)
	}
}

func TestNavigationImmediatelyInvokedScopesPreserveCallerAndLocalBindings(t *testing.T) {
	source := `var target = "global.asp"; function go(value) { location.href = value; }
(function () { var target = "local.asp"; go(target); })();
(function () { go(target); })();
(function () { var target = "direct.asp"; location.assign(target); })();`
	result, err := AnalyzeJavaScriptNavigation(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sinks) != 3 {
		t.Fatalf("sinks = %#v", result.Sinks)
	}
	for i, want := range []string{"local.asp", "global.asp", "direct.asp"} {
		if len(result.Sinks[i].Expression.Values) != 1 || result.Sinks[i].Expression.Values[0].Text != want {
			t.Fatalf("sink[%d] = %#v", i, result.Sinks[i])
		}
		text := source[result.Sinks[i].Expression.Range.ByteStart:result.Sinks[i].Expression.Range.ByteEnd]
		if strings.Contains(text, "function") {
			t.Fatalf("wrapper source leaked: %q", text)
		}
	}
}
