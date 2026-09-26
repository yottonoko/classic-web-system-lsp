package lspserver

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestParseVBScriptTypeSupportsLiteralUnionsAndTemplates(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{name: "string literal", text: `"text"`, want: `"text"`},
		{name: "number literal", text: "404", want: "404"},
		{name: "boolean literal", text: "True", want: "True"},
		{name: "union", text: `"text" | 404 | False`, want: `"text" | 404 | False`},
		{name: "template", text: "`page-${String}.asp`", want: "`page-${String}.asp`"},
		{name: "template union", text: "`page-${String}.asp` | \"index\"", want: "`page-${String}.asp` | \"index\""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parsed, err := parseVBScriptType(test.text)
			if err != nil {
				t.Fatalf("parseVBScriptType(%q) error = %v", test.text, err)
			}
			if got := parsed.String(); got != test.want {
				t.Fatalf("parseVBScriptType(%q) = %q, want %q", test.text, got, test.want)
			}
		})
	}
}

func TestParseVBScriptTypeRejectsMalformedExpressions(t *testing.T) {
	for _, text := range []string{
		`"unterminated`,
		"`page-${String.asp`",
		"String |",
		"| String",
		"404 nope",
	} {
		if _, err := parseVBScriptType(text); err == nil {
			t.Errorf("parseVBScriptType(%q) succeeded for malformed expression", text)
		}
	}
}

func TestVBScriptLiteralUnionWideningIsBoundedAndDeterministic(t *testing.T) {
	values := make([]string, vbscriptLiteralUnionLimit+1)
	for index := range values {
		values[index] = `"value` + intString(index) + `"`
	}
	got := vbscriptTypeUnionFromStrings(values...)
	if got.String() != "String" {
		t.Fatalf("literal union with %d values = %q, want String", len(values), got)
	}

	first := vbscriptTypeUnionFromStrings(`"b"`, `"a"`, `"b"`)
	second := vbscriptTypeUnionFromStrings(`"b"`, `"a"`, `"b"`)
	if first.String() != `"b" | "a"` || first.String() != second.String() {
		t.Fatalf("literal union ordering is not deterministic: first=%q second=%q", first, second)
	}
}

func TestVBScriptCustomObjectUnionIdentityIsCaseInsensitive(t *testing.T) {
	parsed, err := parseVBScriptType("Foo | foo | Bar | BAR")
	if err != nil {
		t.Fatalf("parseVBScriptType custom object union error = %v", err)
	}
	if parsed.String() != "Foo | Bar" {
		t.Fatalf("custom object union display = %q, want first spelling per object", parsed.String())
	}
	if parsed.kind != vbscriptTypeUnion || len(parsed.parts) != 2 {
		t.Fatalf("custom object union = %#v, want two semantic arms", parsed)
	}
	if parsed.parts[0].name != "Foo" || parsed.parts[1].name != "Bar" {
		t.Fatalf("custom object union retained unexpected spellings: %#v", parsed.parts)
	}

	reversed, err := parseVBScriptType("foo | FOO")
	if err != nil {
		t.Fatalf("parseVBScriptType reversed custom object union error = %v", err)
	}
	if reversed.String() != "foo" {
		t.Fatalf("reversed custom object union display = %q, want first spelling foo", reversed.String())
	}

	for _, equivalent := range [][2]string{{"Foo", "foo"}, {"Foo.Bar", "foo.bar"}, {"Foo-Bar", "foo-bar"}} {
		left, leftErr := parseVBScriptType(equivalent[0])
		right, rightErr := parseVBScriptType(equivalent[1])
		if leftErr != nil || rightErr != nil {
			t.Fatalf("equivalent custom object types failed to parse: left=%v right=%v", leftErr, rightErr)
		}
		if left.identity() != right.identity() {
			t.Errorf("object identities differ for %q and %q: %q != %q", equivalent[0], equivalent[1], left.identity(), right.identity())
		}
	}

	distinct, err := parseVBScriptType("Foo | Foo.Bar | Foo-Bar | String")
	if err != nil {
		t.Fatalf("parseVBScriptType distinct custom object union error = %v", err)
	}
	if distinct.kind != vbscriptTypeUnion || len(distinct.parts) != 4 {
		t.Fatalf("distinct custom object union = %#v, want four semantic arms", distinct)
	}
}

func TestVBScriptCustomObjectUnionIdentityPreservesInferenceAndAssignability(t *testing.T) {
	upper, err := parseVBScriptType("Customer")
	if err != nil {
		t.Fatalf("parseVBScriptType upper object error = %v", err)
	}
	lower, err := parseVBScriptType("customer")
	if err != nil {
		t.Fatalf("parseVBScriptType lower object error = %v", err)
	}

	inferred := mergeVBScriptMutableTypes(upper, lower)
	if inferred.String() != "Customer" {
		t.Fatalf("repeated case-variant inference = %q, want first spelling Customer", inferred.String())
	}
	if !vbscriptTypeAssignable(upper, lower) || !vbscriptTypeAssignable(lower, upper) {
		t.Fatalf("case-variant object types are not mutually assignable: upper=%#v lower=%#v", upper, lower)
	}

	declared, err := parseVBScriptType("Customer | customer | Order")
	if err != nil {
		t.Fatalf("parseVBScriptType declared object union error = %v", err)
	}
	annotation, ok := parseVBScriptTypeAnnotationText("@type item As Customer | customer | Order")
	if !ok || annotation.err != nil || annotation.typeExpr.String() != "Customer | Order" {
		t.Fatalf("declared object annotation = %#v, ok=%t, want first-spelling union Customer | Order", annotation, ok)
	}
	if !vbscriptTypeAssignable(declared, lower) {
		t.Fatalf("declared object union does not accept case-variant inferred object: declared=%#v inferred=%#v", declared, lower)
	}
	distinct, distinctErr := parseVBScriptType("Account")
	if distinctErr != nil {
		t.Fatalf("parseVBScriptType distinct object error = %v", distinctErr)
	}
	if vbscriptTypeAssignable(declared, distinct) {
		t.Fatal("declared object union accepted a distinct object type")
	}
}

func TestVBScriptCustomObjectDuplicatesDoNotConsumeLiteralUnionBudget(t *testing.T) {
	values := make([]string, 0, vbscriptLiteralUnionLimit+2)
	for index := 0; index < vbscriptLiteralUnionLimit; index++ {
		values = append(values, `"value`+intString(index)+`"`)
	}
	values = append(values, "Record", "record")

	merged := vbscriptTypeUnionFromStrings(values...)
	if merged.kind != vbscriptTypeUnion {
		t.Fatalf("mixed literal/object union = %#v, want a union at the literal budget boundary", merged)
	}
	if len(merged.parts) != vbscriptLiteralUnionLimit+1 {
		t.Fatalf("mixed literal/object union has %d arms, want %d after deduplicating Record/record", len(merged.parts), vbscriptLiteralUnionLimit+1)
	}
	if merged.String() == "String" {
		t.Fatal("case-variant object duplicate widened a literal union at the exact budget boundary")
	}
	if got := merged.parts[len(merged.parts)-1].String(); got != "Record" {
		t.Fatalf("mixed union object display = %q, want first spelling Record", got)
	}
}

func TestVBScriptTemplateIdentityUsesDecodedSegmentsAndChildTypes(t *testing.T) {
	equivalent := []string{"`outer-\\}`", "`outer-}`"}
	parsed := make([]vbscriptType, 0, len(equivalent))
	for _, text := range equivalent {
		value, err := parseVBScriptType(text)
		if err != nil {
			t.Fatalf("parseVBScriptType(%q) error = %v", text, err)
		}
		parsed = append(parsed, value)
	}
	if parsed[0].identity() != parsed[1].identity() {
		t.Fatalf("escaped/unescaped template identities differ: %q != %q", parsed[0].identity(), parsed[1].identity())
	}
	merged := makeVBScriptTypeUnion(parsed...)
	if merged.kind != vbscriptTypeTemplate || merged.String() != equivalent[0] {
		t.Fatalf("escaped/unescaped template union = %#v, want first spelling %q", merged, equivalent[0])
	}

	nestedEquivalent := []string{
		"`outer-${`inner-\\}`}`",
		"`outer-${`inner-}`}`",
	}
	nestedParsed := make([]vbscriptType, 0, len(nestedEquivalent))
	for _, text := range nestedEquivalent {
		value, err := parseVBScriptType(text)
		if err != nil {
			t.Fatalf("parseVBScriptType(%q) error = %v", text, err)
		}
		nestedParsed = append(nestedParsed, value)
	}
	if nestedParsed[0].identity() != nestedParsed[1].identity() {
		t.Fatalf("nested escaped/unescaped template identities differ: %q != %q", nestedParsed[0].identity(), nestedParsed[1].identity())
	}
	nestedUnion := makeVBScriptTypeUnion(nestedParsed...)
	if nestedUnion.kind != vbscriptTypeTemplate || nestedUnion.String() != nestedEquivalent[0] {
		t.Fatalf("nested escaped/unescaped template union = %#v, want first spelling %q", nestedUnion, nestedEquivalent[0])
	}

	nestedTypeUnion := []string{
		"`outer-${`inner-${Customer | customer}`}`",
		"`outer-${`inner-${customer}`}`",
	}
	nestedTypeParsed := make([]vbscriptType, 0, len(nestedTypeUnion))
	for _, text := range nestedTypeUnion {
		value, err := parseVBScriptType(text)
		if err != nil {
			t.Fatalf("parseVBScriptType(%q) error = %v", text, err)
		}
		nestedTypeParsed = append(nestedTypeParsed, value)
	}
	if nestedTypeParsed[0].identity() != nestedTypeParsed[1].identity() {
		t.Fatalf("nested equivalent union-template identities differ: %q != %q", nestedTypeParsed[0].identity(), nestedTypeParsed[1].identity())
	}
	nestedTypeDisplay := "`outer-${`inner-${Customer}`}`"
	if merged := makeVBScriptTypeUnion(nestedTypeParsed...); merged.kind != vbscriptTypeTemplate || merged.String() != nestedTypeDisplay {
		t.Fatalf("nested equivalent union-template merge = %#v, want first semantic spelling %q", merged, nestedTypeDisplay)
	}

	orderedNested := [][2]string{
		{"`outer-${\"ready\" | \"waiting\"}`", "`outer-${\"waiting\" | \"ready\"}`"},
		{"`outer-${Customer | Order}`", "`outer-${order | customer}`"},
	}
	for _, texts := range orderedNested {
		left, leftErr := parseVBScriptType(texts[0])
		right, rightErr := parseVBScriptType(texts[1])
		if leftErr != nil || rightErr != nil {
			t.Fatalf("reordered nested union parse failed: left=%v right=%v", leftErr, rightErr)
		}
		if left.identity() != right.identity() {
			t.Errorf("reordered nested union identities differ for %q and %q: %q != %q", texts[0], texts[1], left.identity(), right.identity())
		}
		merged := makeVBScriptTypeUnion(left, right)
		if merged.kind != vbscriptTypeTemplate || merged.String() != left.String() {
			t.Errorf("reordered nested union merge = %#v, want first display %q", merged, left.String())
		}
	}

	distinct := [][2]string{
		{"`ab`", "`a${String}b`"},
		{"`outer-\\}`", "`outer-\\\\}`"},
		{"`outer-${\"ready\" | \"waiting\"}`", "`outer-${\"ready\" | \"missing\"}`"},
		{"`outer-${Customer | Order}`", "`outer-${Customer | Account}`"},
		{"`outer-${\"a\" | \"b|string:c\"}`", "`outer-${\"a\" | \"b\" | \"string:c\"}`"},
	}
	for _, texts := range distinct {
		left, leftErr := parseVBScriptType(texts[0])
		right, rightErr := parseVBScriptType(texts[1])
		if leftErr != nil || rightErr != nil {
			t.Fatalf("distinct template parse failed: left=%v right=%v", leftErr, rightErr)
		}
		if left.identity() == right.identity() {
			t.Errorf("distinct template identities collapsed for %q and %q: %q", texts[0], texts[1], left.identity())
		}
		merged := makeVBScriptTypeUnion(left, right)
		if merged.kind != vbscriptTypeUnion || len(merged.parts) != 2 {
			t.Errorf("distinct template union = %#v, want two arms", merged)
		}
	}
}

func TestVBScriptMutableLiteralInferencePreservesUnknownAlternative(t *testing.T) {
	known := inferVBScriptValueLiteralType(`"known"`)
	unknown := inferVBScriptValueLiteralType("ResolveAtRuntime()")
	if !unknown.isUnknown() {
		t.Fatalf("dynamic expression inferred as %#v, want unknown", unknown)
	}
	for _, values := range [][2]vbscriptType{{known, unknown}, {unknown, known}} {
		merged := mergeVBScriptMutableTypes(values[0], values[1])
		if !merged.isUnknown() || merged.String() != "Variant" {
			t.Fatalf("merged literal/dynamic values = %#v, want Variant unknown", merged)
		}
	}
}

func TestVBScriptTypeCompatibilityUnderstandsLiteralBasesAndTemplates(t *testing.T) {
	tests := []struct {
		expected string
		actual   string
		want     bool
	}{
		{expected: "String", actual: `"text"`, want: true},
		{expected: `"text"`, actual: `"text"`, want: true},
		{expected: `"text"`, actual: `"other"`, want: false},
		{expected: "Number", actual: "404", want: true},
		{expected: "Boolean", actual: "False", want: true},
		{expected: "`page-${String}.asp`", actual: `"page-home.asp"`, want: true},
		{expected: "`page-${String}.asp`", actual: `"other.asp"`, want: false},
		{expected: "`page-${String}.asp`", actual: "String", want: false},
		{expected: "String", actual: "`page-${String}.asp`", want: true},
	}
	for _, test := range tests {
		if got := vbscriptTypeExpressionsCompatible(test.expected, test.actual); got != test.want {
			t.Errorf("vbscriptTypeExpressionsCompatible(%q, %q) = %t, want %t", test.expected, test.actual, got, test.want)
		}
	}
}

func TestVBScriptTemplateBooleanLiteralMatchingPreservesIdentity(t *testing.T) {
	tests := []struct {
		name     string
		expected string
		actual   string
		want     bool
	}{
		{name: "True accepts its canonical spelling", expected: "`flag-${True}`", actual: `"flag-True"`, want: true},
		{name: "True rejects False", expected: "`flag-${True}`", actual: `"flag-False"`, want: false},
		{name: "False accepts its canonical spelling", expected: "`flag-${False}`", actual: `"flag-False"`, want: true},
		{name: "False rejects True", expected: "`flag-${False}`", actual: `"flag-True"`, want: false},
		{name: "lowercase True annotation normalizes to VB spelling", expected: "`flag-${true}`", actual: `"flag-True"`, want: true},
		{name: "normalized True rejects lowercase output", expected: "`flag-${TRUE}`", actual: `"flag-true"`, want: false},
		{name: "normalized True rejects uppercase output", expected: "`flag-${TRUE}`", actual: `"flag-TRUE"`, want: false},
		{name: "Boolean accepts True", expected: "`flag-${Boolean}`", actual: `"flag-True"`, want: true},
		{name: "Boolean accepts False", expected: "`flag-${Boolean}`", actual: `"flag-False"`, want: true},
		{name: "Boolean remains case insensitive", expected: "`flag-${Boolean}`", actual: `"flag-false"`, want: true},
		{name: "boolean union accepts True", expected: "`flag-${True | False}`", actual: `"flag-True"`, want: true},
		{name: "boolean union accepts False", expected: "`flag-${True | False}`", actual: `"flag-False"`, want: true},
		{name: "boolean union rejects lowercase output", expected: "`flag-${True | False}`", actual: `"flag-false"`, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := vbscriptTypeExpressionsCompatible(test.expected, test.actual); got != test.want {
				t.Errorf("vbscriptTypeExpressionsCompatible(%q, %q) = %t, want %t", test.expected, test.actual, got, test.want)
			}
		})
	}

	if !vbscriptTypeExpressionsCompatible("String", "`flag-${True}`") {
		t.Fatal("String should accept a template containing an exact boolean literal")
	}
	if vbscriptTypeExpressionsCompatible("`flag-${True}`", "String") {
		t.Fatal("an exact boolean template should not accept an unbounded String")
	}
}

func TestVBScriptNumericLiteralIdentityKeepsLargeValuesDistinct(t *testing.T) {
	left, leftErr := parseVBScriptType("9007199254740992")
	right, rightErr := parseVBScriptType("9007199254740993")
	if leftErr != nil || rightErr != nil {
		t.Fatalf("large numeric literals failed to parse: left=%v right=%v", leftErr, rightErr)
	}
	if left.identity() == right.identity() {
		t.Fatalf("large numeric literal identities collapsed: left=%q right=%q", left.identity(), right.identity())
	}
	if vbscriptTypeAssignable(left, right) || vbscriptTypeAssignable(right, left) {
		t.Fatalf("distinct large numeric literals were assignable: left=%#v right=%#v", left, right)
	}
	union := makeVBScriptTypeUnion(left, right)
	if union.kind != vbscriptTypeUnion || len(union.parts) != 2 {
		t.Fatalf("large numeric literal union = %#v, want two arms", union)
	}

	for _, equivalent := range [][2]string{{"1000", "1e3"}, {"0.5", "5e-1"}, {"&HFF", "&O377"}, {"&077", "&O77"}} {
		if !vbscriptNumericLiteralsEqual(equivalent[0], equivalent[1]) {
			t.Errorf("equivalent numeric literals %q and %q were not equal", equivalent[0], equivalent[1])
		}
	}
	if !vbscriptNumericLiteralsEqual("10e999999999999999999999", "1e1000000000000000000000") {
		t.Fatal("scientific literals with arbitrary-size exponents were not canonicalized exactly")
	}
	if got := vbscriptNumericLiteralIdentity("1.0000"); got != "1" {
		t.Fatalf("ordinary decimal canonical identity = %q, want 1", got)
	}
}

func TestVBScriptNumericLiteralIdentityBoundsExtremeExponents(t *testing.T) {
	for _, test := range []struct {
		name     string
		literal  string
		identity string
	}{
		{name: "large positive exponent", literal: "1e1000000000", identity: "symbolic:1e1000000000"},
		{name: "large negative exponent", literal: "1e-1000000000", identity: "symbolic:1e-1000000000"},
		{name: "int64 maximum exponent", literal: "1e9223372036854775807", identity: "symbolic:1e9223372036854775807"},
		{name: "int64 minimum exponent", literal: "1e-9223372036854775808", identity: "symbolic:1e-9223372036854775808"},
	} {
		t.Run(test.name, func(t *testing.T) {
			value, ok := parseVBScriptNumericLiteral(test.literal)
			if !ok {
				t.Fatalf("parseVBScriptNumericLiteral(%q) was rejected", test.literal)
			}
			if got := value.identity(); got != test.identity {
				t.Fatalf("identity(%q) = %q, want %q", test.literal, got, test.identity)
			}
		})
	}

	for _, equivalent := range [][2]string{
		{"10e999999999999999999999", "1e1000000000000000000000"},
		{"10e-9223372036854775808", "1e-9223372036854775807"},
		{"1e4096", "1" + strings.Repeat("0", 4096)},
	} {
		if !vbscriptNumericLiteralsEqual(equivalent[0], equivalent[1]) {
			t.Errorf("equivalent extreme literals %q and %q were not equal", equivalent[0], equivalent[1])
		}
	}
	for _, distinct := range [][2]string{
		{"1e1000000000", "1e1000000001"},
		{"1e-9223372036854775808", "1e-9223372036854775809"},
	} {
		if vbscriptNumericLiteralsEqual(distinct[0], distinct[1]) {
			t.Errorf("distinct extreme literals %q and %q were equal", distinct[0], distinct[1])
		}
	}
}

func TestVBScriptNumericLiteralIdentityRejectsOutOfPolicyExponentSpelling(t *testing.T) {
	literal := "1e" + strings.Repeat("9", vbscriptNumericExponentDigitLimit+1)
	if _, ok := parseVBScriptNumericLiteral(literal); ok {
		t.Fatalf("out-of-policy exponent spelling was accepted: %q", literal)
	}
	if isVBScriptNumberTypeLiteral(literal) {
		t.Fatalf("out-of-policy exponent spelling was classified as a number: %q", literal)
	}
}

func TestVBScriptNumericLiteralExtremeExponentAllocationsStayBounded(t *testing.T) {
	const literal = "1e1000000000"
	allocations := testing.AllocsPerRun(100, func() {
		value, ok := parseVBScriptNumericLiteral(literal)
		if !ok || value.identity() != "symbolic:1e1000000000" {
			t.Fatalf("extreme exponent parse failed: value=%#v ok=%t", value, ok)
		}
	})
	if allocations > 64 {
		t.Fatalf("extreme exponent required %f allocations per run, want <= 64", allocations)
	}
}

func TestVBScriptTypeDefinitionUsesVisibleLocalAndGlobalDeclarations(t *testing.T) {
	source := `<%
Class GlobalType
End Class
Class LocalType
End Class
' @type value As GlobalType
Dim value
Sub Render()
  Dim value
  Set value = New LocalType
  value
End Sub
value
%>`
	parsed := core.ParseDocument("file:///tmp/scope-aware-type-definition.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	document := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	localLocations := server.vbscriptTypeDefinition(parsed, document.PositionAt(strings.LastIndex(source[:strings.Index(source, "End Sub")], "value")))
	if len(localLocations) != 1 || localLocations[0].Range.Start.Line != 3 {
		t.Fatalf("local typeDefinition locations = %#v, want LocalType declaration on line 3", localLocations)
	}
	globalOffset := strings.LastIndex(source, "value")
	globalLocations := server.vbscriptTypeDefinition(parsed, document.PositionAt(globalOffset))
	if len(globalLocations) != 1 || globalLocations[0].Range.Start.Line != 1 {
		t.Fatalf("global typeDefinition locations = %#v, want GlobalType declaration on line 1", globalLocations)
	}
}

func TestVBScriptTypeDefinitionRespectsPropertyFunctionAndClassScopes(t *testing.T) {
	source := `<%
Class GlobalType
End Class
Class PropertyType
End Class
Class SubType
End Class
Class FunctionType
End Class
Class FieldType
End Class
Class Holder
' @member Holder.field As FieldType
  Public field
  Property Get Value()
    Dim propertyLocal
    Set propertyLocal = New PropertyType
    propertyLocal
    field
  End Property
  Sub Run()
    Dim subLocal
    Set subLocal = New SubType
    subLocal
  End Sub
  Function Build()
    Dim functionLocal
    Set functionLocal = New FunctionType
    functionLocal
  End Function
End Class
' @type globalValue As GlobalType
Dim globalValue
globalValue
%>`
	parsed := core.ParseDocument("file:///tmp/lexical-type-definition.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	document := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	lineOf := func(text string) int { return document.PositionAt(strings.Index(source, text)).Line }
	tests := []struct {
		name      string
		reference string
		classText string
	}{
		{name: "property local", reference: "    propertyLocal\n", classText: "Class PropertyType"},
		{name: "property class field", reference: "    field\n", classText: "Class FieldType"},
		{name: "sub local", reference: "    subLocal\n", classText: "Class SubType"},
		{name: "function local", reference: "    functionLocal\n", classText: "Class FunctionType"},
		{name: "global", reference: "globalValue\n", classText: "Class GlobalType"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			referenceStart := strings.LastIndex(source, test.reference)
			if referenceStart < 0 {
				t.Fatalf("reference %q missing", test.reference)
			}
			offset := referenceStart + len(test.reference) - len("\n") - 1
			locations := server.vbscriptTypeDefinition(parsed, document.PositionAt(offset))
			if len(locations) != 1 || locations[0].Range.Start.Line != lineOf(test.classText) {
				t.Fatalf("%s typeDefinition locations = %#v, want %s line %d", test.name, locations, test.classText, lineOf(test.classText))
			}
		})
	}
}

func TestVBScriptTypeDefinitionReturnsUnionClassArmsFromResolvedIncludes(t *testing.T) {
	root := t.TempDir()
	includePath := filepath.Join(root, "types.inc")
	ownerPath := filepath.Join(root, "default.asp")
	includeSource := `<%
Class IncludedA
End Class
Class IncludedB
End Class
%>`
	if err := os.WriteFile(includePath, []byte(includeSource), 0o600); err != nil {
		t.Fatal(err)
	}
	ownerSource := `<!-- #include file="types.inc" -->
<%
' @type item As IncludedA | IncludedB
Dim item
item
%>`
	parsed := core.ParseDocument(filePathURI(ownerPath), ownerSource, core.Settings{DefaultLanguage: "VBScript"})
	server := New(strings.NewReader(""), io.Discard, io.Discard)
	server.rootPath = root
	server.rootURI = filePathURI(root)
	document := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	locations := server.vbscriptTypeDefinition(parsed, document.PositionAt(strings.LastIndex(ownerSource, "item")))
	if len(locations) != 2 {
		t.Fatalf("include-backed union typeDefinition locations = %#v, want two class definitions", locations)
	}
	for _, location := range locations {
		if !strings.EqualFold(location.URI, filePathURI(includePath)) {
			t.Fatalf("union typeDefinition escaped include source: %#v", locations)
		}
	}
	if locations[0].Range.Start.Line != 1 || locations[1].Range.Start.Line != 3 {
		t.Fatalf("include-backed union ranges = %#v, want class lines 1 and 3", locations)
	}
}

func TestVBScriptDoubledQuoteStringLiteralDecodesAndAssigns(t *testing.T) {
	const sourceLiteral = `"a""b"`
	parsed, err := parseVBScriptType(sourceLiteral)
	if err != nil {
		t.Fatalf("parseVBScriptType(%q) error = %v", sourceLiteral, err)
	}
	if parsed.kind != vbscriptTypeStringLiteral || parsed.value != `a"b` {
		t.Fatalf("parsed doubled-quote literal = %#v, want string literal value %q", parsed, `a"b`)
	}
	if got := parsed.String(); got != sourceLiteral {
		t.Fatalf("doubled-quote literal spelling = %q, want %q", got, sourceLiteral)
	}

	inferred := inferVBScriptValueLiteralType(sourceLiteral)
	if inferred.kind != vbscriptTypeStringLiteral || inferred.value != `a"b` {
		t.Fatalf("inferred doubled-quote literal = %#v, want string literal value %q", inferred, `a"b`)
	}
	if !vbscriptTypeAssignable(parsed, inferred) {
		t.Fatalf("doubled-quote annotation is not compatible with equivalent value: expected=%#v actual=%#v", parsed, inferred)
	}
	if !vbscriptTypeExpressionsCompatible(sourceLiteral, sourceLiteral) {
		t.Fatalf("doubled-quote type expression is not self-compatible")
	}

	program := `<%
' @type value As "a""b"
Dim value
value = "a""b" ' apostrophe comment after literal
%>`
	document := core.ParseDocument("file:///tmp/doubled-quote-type.asp", program, core.Settings{DefaultLanguage: "VBScript"})
	server := &Server{settings: serverSettings{VBScriptTypeChecking: "strict"}}
	if diagnostics := server.vbscriptTypeDiagnostics(document); len(diagnostics) != 0 {
		t.Fatalf("equivalent doubled-quote assignment produced diagnostics: %#v", diagnostics)
	}

	apostropheProgram := `<%
' @type value As "a'b"
Dim value
value = "a'b" ' comment outside the string
%>`
	apostropheDocument := core.ParseDocument("file:///tmp/apostrophe-literal-type.asp", apostropheProgram, core.Settings{DefaultLanguage: "VBScript"})
	if diagnostics := server.vbscriptTypeDiagnostics(apostropheDocument); len(diagnostics) != 0 {
		t.Fatalf("apostrophe inside a string was treated as a comment: %#v", diagnostics)
	}
}

func TestVBScriptSourceStringLiteralKeepsBackslashesAndAnnotationEscapesRoundTrip(t *testing.T) {
	const sourceLiteral = `"C:\資料\表示\"`
	const annotationLiteral = `"C:\\資料\\表示\\"`

	inferred := inferVBScriptValueLiteralType(sourceLiteral)
	if inferred.kind != vbscriptTypeStringLiteral || inferred.value != `C:\資料\表示\` {
		t.Fatalf("source path literal = %#v, want literal value %q", inferred, `C:\資料\表示\`)
	}
	if got, want := inferred.String(), annotationLiteral; got != want {
		t.Fatalf("source path display = %q, want annotation-safe spelling %q", got, want)
	}
	annotation, err := parseVBScriptType(annotationLiteral)
	if err != nil {
		t.Fatalf("annotation path literal failed to parse: %v", err)
	}
	if annotation.value != inferred.value {
		t.Fatalf("annotation path value = %q, want %q", annotation.value, inferred.value)
	}
	if !vbscriptTypeAssignable(annotation, inferred) {
		t.Fatalf("equivalent path literals were not assignable: annotation=%#v source=%#v", annotation, inferred)
	}
	if !vbscriptTypeExpressionsCompatible(annotationLiteral, inferred.String()) {
		t.Fatalf("escaped annotation and source display were not compatible")
	}

	const doubledQuoteSource = `"C:\資料\a""b"`
	doubledQuote := inferVBScriptValueLiteralType(doubledQuoteSource)
	if doubledQuote.value != `C:\資料\a"b` {
		t.Fatalf("source doubled-quote path = %q, want %q", doubledQuote.value, `C:\資料\a"b`)
	}
	if reparsed, err := parseVBScriptType(doubledQuote.String()); err != nil || reparsed.value != doubledQuote.value {
		t.Fatalf("doubled-quote source display did not round-trip: parsed=%#v err=%v", reparsed, err)
	}
}

func TestVBScriptNumericTemplateArmsMatchEquivalentSpellings(t *testing.T) {
	tests := []struct {
		name     string
		expected string
		actual   string
		want     bool
	}{
		{name: "scientific to decimal", expected: "`value-${1e3}`", actual: `"value-1000"`, want: true},
		{name: "decimal to scientific", expected: "`value-${1000}`", actual: `"value-1e3"`, want: true},
		{name: "decimal fraction", expected: "`value-${1.00}`", actual: `"value-1e0"`, want: true},
		{name: "hex to decimal", expected: "`value-${&HFF}`", actual: `"value-255"`, want: true},
		{name: "octal to decimal", expected: "`value-${&O377}`", actual: `"value-255"`, want: true},
		{name: "legacy octal to decimal", expected: "`value-${&077}`", actual: `"value-63"`, want: true},
		{name: "hex to octal", expected: "`value-${&HFF}`", actual: `"value-&O377"`, want: true},
		{name: "non-equivalent decimal", expected: "`value-${1e3}`", actual: `"value-1001"`, want: false},
		{name: "non-equivalent radix", expected: "`value-${&HFF}`", actual: `"value-256"`, want: false},
		{name: "extreme symbolic identity", expected: "`value-${1e1000000000}`", actual: `"value-1e1000000000"`, want: true},
		{name: "extreme symbolic mismatch", expected: "`value-${1e1000000000}`", actual: `"value-1e1000000001"`, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := vbscriptTypeExpressionsCompatible(test.expected, test.actual); got != test.want {
				t.Fatalf("vbscriptTypeExpressionsCompatible(%q, %q) = %t, want %t", test.expected, test.actual, got, test.want)
			}
		})
	}
	large := "1" + strings.Repeat("0", vbscriptNumericMaterializationDigitLimit+1)
	if !vbscriptTypeExpressionsCompatible("`value-${"+large+"}`", `"value-`+large+`"`) {
		t.Fatalf("large symbolic decimal template arm did not match its equivalent value")
	}
}

func TestVBScriptNestedTemplateMatchingIsStructural(t *testing.T) {
	const nested = "`outer-${`inner-${Number}`}`"
	for _, test := range []struct {
		name   string
		actual string
		want   bool
	}{
		{name: "numeric interpolation", actual: `"outer-inner-1000"`, want: true},
		{name: "equivalent numeric spelling", actual: `"outer-inner-1e3"`, want: true},
		{name: "wrong nested prefix", actual: `"outer-other-1000"`, want: false},
		{name: "non-numeric nested value", actual: `"outer-inner-value"`, want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := vbscriptTypeExpressionsCompatible(nested, test.actual); got != test.want {
				t.Fatalf("vbscriptTypeExpressionsCompatible(%q, %q) = %t, want %t", nested, test.actual, got, test.want)
			}
		})
	}
	for _, test := range []struct {
		name     string
		expected string
		actual   string
		want     bool
	}{
		{name: "nested exact numeric with suffix", expected: "`outer-${`inner-${1e3}`}-suffix`", actual: `"outer-inner-1000-suffix"`, want: true},
		{name: "nested exact numeric with suffix mismatch", expected: "`outer-${`inner-${1e3}`}-suffix`", actual: `"outer-inner-1001-suffix"`, want: false},
		{name: "nested numeric before another expression", expected: "`outer-${`inner-${Number}`}-${True}`", actual: `"outer-inner-1000-True"`, want: true},
		{name: "nested numeric before another expression mismatch", expected: "`outer-${`inner-${Number}`}-${True}`", actual: `"outer-inner-1000-False"`, want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := vbscriptTypeExpressionsCompatible(test.expected, test.actual); got != test.want {
				t.Fatalf("vbscriptTypeExpressionsCompatible(%q, %q) = %t, want %t", test.expected, test.actual, got, test.want)
			}
		})
	}

	const nestedLiteralBrace = "`outer-${`inner-}`}`"
	parsed, err := parseVBScriptType(nestedLiteralBrace)
	if err != nil {
		t.Fatalf("nested template with a literal closing brace failed to parse: %v", err)
	}
	if got := parsed.String(); got != nestedLiteralBrace {
		t.Fatalf("nested template with a literal closing brace = %q, want %q", got, nestedLiteralBrace)
	}
	if !vbscriptTypeExpressionsCompatible(nestedLiteralBrace, `"outer-inner-}"`) {
		t.Fatalf("literal closing brace inside nested template did not match")
	}
}

func TestVBScriptNestedTemplateEscapesAndMalformedExpressions(t *testing.T) {
	const escaped = "`literal-\\`-\\}-\\${not-an-expression`"
	parsed, err := parseVBScriptType(escaped)
	if err != nil {
		t.Fatalf("escaped nested-template delimiters failed to parse: %v", err)
	}
	if got := parsed.String(); got != escaped {
		t.Fatalf("escaped nested-template delimiters = %q, want %q", got, escaped)
	}
	const nestedEscapedBrace = "`outer-${`inner-\\}`}`"
	nestedParsed, err := parseVBScriptType(nestedEscapedBrace)
	if err != nil {
		t.Fatalf("escaped brace inside nested template failed to parse: %v", err)
	}
	if got := nestedParsed.String(); got != nestedEscapedBrace {
		t.Fatalf("escaped brace inside nested template = %q, want %q", got, nestedEscapedBrace)
	}
	const tick = "`"
	nestedEscapedBacktick := tick + "outer-${" + tick + "inner-\\" + tick + tick + "}" + tick
	nestedBacktickParsed, err := parseVBScriptType(nestedEscapedBacktick)
	if err != nil {
		t.Fatalf("escaped backtick inside nested template failed to parse: %v", err)
	}
	if got := nestedBacktickParsed.String(); got != nestedEscapedBacktick {
		t.Fatalf("escaped backtick inside nested template = %q, want %q", got, nestedEscapedBacktick)
	}

	for _, text := range []string{
		"`outer-${`inner-${Number}`}\"",
	} {
		if _, err := parseVBScriptType(text); err == nil {
			t.Errorf("parseVBScriptType(%q) succeeded for malformed nested template", text)
		}
	}
}

func TestVBScriptTemplateEscapesDecodeForCompatibilityAndRoundTrip(t *testing.T) {
	const tick = "`"
	quoted := func(value string) string {
		return `"` + value + `"`
	}
	outerBacktick := tick + "outer-\\" + tick + tick
	nestedBacktick := tick + "outer-${" + tick + "inner-\\" + tick + tick + "}" + tick
	nestedInterpolation := tick + "outer-${" + tick + "inner-\\${value" + tick + "}" + tick
	nestedBackslash := tick + "outer-${" + tick + "inner-\\\\" + tick + "}" + tick
	tests := []struct {
		name     string
		expected string
		actual   string
	}{
		{name: "outer closing brace", expected: "`outer-\\}`", actual: quoted("outer-}")},
		{name: "outer backtick", expected: outerBacktick, actual: quoted("outer-" + tick)},
		{name: "outer interpolation marker", expected: "`outer-\\${value`", actual: quoted("outer-${value")},
		{name: "outer backslash", expected: "`outer-\\\\`", actual: quoted("outer-\\")},
		{name: "nested closing brace", expected: "`outer-${`inner-\\}`}`", actual: quoted("outer-inner-}")},
		{name: "nested backtick", expected: nestedBacktick, actual: quoted("outer-inner-" + tick)},
		{name: "nested interpolation marker", expected: nestedInterpolation, actual: quoted("outer-inner-${value")},
		{name: "nested backslash", expected: nestedBackslash, actual: quoted("outer-inner-\\")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parsed, err := parseVBScriptType(test.expected)
			if err != nil {
				t.Fatalf("parseVBScriptType(%q) error = %v", test.expected, err)
			}
			if got := parsed.String(); got != test.expected {
				t.Fatalf("template display = %q, want raw spelling %q", got, test.expected)
			}
			if !vbscriptTypeExpressionsCompatible(test.expected, test.actual) {
				t.Fatalf("decoded template %q did not match source literal %q", test.expected, test.actual)
			}
		})
	}
	expected, err := parseVBScriptType("`outer-\\}`")
	if err != nil {
		t.Fatalf("escaped closing brace template failed to parse: %v", err)
	}
	actual, err := parseVBScriptSourceStringType(quoted(`outer-\}`))
	if err != nil {
		t.Fatalf("raw escaped closing brace source literal failed to parse: %v", err)
	}
	if vbscriptTypeAssignable(expected, actual) {
		t.Fatalf("escaped closing brace template matched a source literal retaining its backslash")
	}

	for _, text := range []string{
		"`invalid-\\q`",
		"`invalid-\\$`",
		"`invalid-\\",
	} {
		if _, err := parseVBScriptType(text); err == nil {
			t.Errorf("parseVBScriptType(%q) succeeded for malformed template escape", text)
		}
	}
}

func TestVBScriptTemplateEscapesStrictDiagnostics(t *testing.T) {
	const tick = "`"
	quoted := func(value string) string {
		return `"` + value + `"`
	}
	annotations := []struct {
		name     string
		expected string
		actual   string
	}{
		{name: "brace", expected: "`outer-\\}`", actual: quoted("outer-}")},
		{name: "backtick", expected: tick + "outer-\\" + tick + tick, actual: quoted("outer-" + tick)},
		{name: "interpolation", expected: "`outer-\\${value`", actual: quoted("outer-${value")},
		{name: "backslash", expected: "`outer-\\\\`", actual: quoted("outer-\\")},
		{name: "nested", expected: "`outer-${`inner-\\}`}`", actual: quoted("outer-inner-}")},
		{name: "nested_backtick", expected: tick + "outer-${" + tick + "inner-\\" + tick + tick + "}" + tick, actual: quoted("outer-inner-" + tick)},
		{name: "nested_interpolation", expected: tick + "outer-${" + tick + "inner-\\${value" + tick + "}" + tick, actual: quoted("outer-inner-${value")},
		{name: "nested_backslash", expected: tick + "outer-${" + tick + "inner-\\\\" + tick + "}" + tick, actual: quoted("outer-inner-\\")},
	}
	var source strings.Builder
	source.WriteString("<%\n")
	for _, annotation := range annotations {
		source.WriteString("' @type ")
		source.WriteString(annotation.name)
		source.WriteString(" As ")
		source.WriteString(annotation.expected)
		source.WriteString("\nDim ")
		source.WriteString(annotation.name)
		source.WriteString("\n")
		source.WriteString(annotation.name)
		source.WriteString(" = ")
		source.WriteString(annotation.actual)
		source.WriteString("\n")
	}
	source.WriteString("%>")
	parsed := core.ParseDocument("file:///tmp/template-escape-types.asp", source.String(), core.Settings{DefaultLanguage: "VBScript"})
	strict := &Server{settings: serverSettings{VBScriptTypeChecking: "strict"}}
	if diagnostics := strict.vbscriptTypeDiagnostics(parsed); len(diagnostics) != 0 {
		t.Fatalf("valid escaped template assignments produced diagnostics: %#v", diagnostics)
	}

	malformed := core.ParseDocument("file:///tmp/malformed-template-escape-type.asp", `<%
' @type value As `+"`invalid-\\q`"+`
Dim value
%>`, core.Settings{DefaultLanguage: "VBScript"})
	diagnostics := strict.vbscriptTypeDiagnostics(malformed)
	if len(diagnostics) == 0 || diagnostics[0].Code != "malformedTypeAnnotation" {
		t.Fatalf("malformed escaped template diagnostics = %#v, want malformedTypeAnnotation", diagnostics)
	}
}

func TestVBScriptNestedTemplateStrictDiagnostics(t *testing.T) {
	const nested = "`outer-${`inner-${Number}`}`"
	valid := core.ParseDocument("file:///tmp/nested-template-type.asp", `<%
' @type value As `+nested+`
Dim value
value = "outer-inner-1000"
%>`, core.Settings{DefaultLanguage: "VBScript"})
	strict := &Server{settings: serverSettings{VBScriptTypeChecking: "strict"}}
	if diagnostics := strict.vbscriptTypeDiagnostics(valid); len(diagnostics) != 0 {
		t.Fatalf("valid nested template assignment produced diagnostics: %#v", diagnostics)
	}

	const malformed = "`outer-${`inner-${Number}`}"
	invalid := core.ParseDocument("file:///tmp/malformed-nested-template-type.asp", `<%
' @type value As `+malformed+`
Dim value
%>`, core.Settings{DefaultLanguage: "VBScript"})
	diagnostics := strict.vbscriptTypeDiagnostics(invalid)
	if len(diagnostics) == 0 || diagnostics[0].Code != "malformedTypeAnnotation" {
		t.Fatalf("malformed nested template diagnostics = %#v, want malformedTypeAnnotation", diagnostics)
	}
}

func TestVBScriptStrictDiagnosticsAcceptEquivalentPathAndNumericLiterals(t *testing.T) {
	const source = `<%
' @type path As "C:\\資料\\表示\\"
Dim path
path = "C:\資料\表示\"
' @type count As 1e3
Dim count
count = 1000
%>`
	parsed := core.ParseDocument("file:///tmp/equivalent-literal-types.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	server := &Server{settings: serverSettings{VBScriptTypeChecking: "strict"}}
	if diagnostics := server.vbscriptTypeDiagnostics(parsed); len(diagnostics) != 0 {
		t.Fatalf("equivalent path and numeric assignments produced diagnostics: %#v", diagnostics)
	}
}

func TestVBScriptTypeParserBudgetsHaveExactBoundaries(t *testing.T) {
	longName := "T" + strings.Repeat("x", vbscriptTypeParseInputLengthLimit-1)
	if parsed, err := parseVBScriptType(longName); err != nil || parsed.String() != longName {
		t.Fatalf("input at the length boundary failed: parsed=%#v err=%v", parsed, err)
	}
	assertVBScriptTypeParseBudgetError(t, longName+"x", "input length")

	union := make([]string, vbscriptTypeParseUnionArmLimit)
	for index := range union {
		union[index] = "Type" + intString(index)
	}
	if parsed, err := parseVBScriptType(strings.Join(union, " | ")); err != nil || parsed.kind != vbscriptTypeUnion || len(parsed.parts) != vbscriptTypeParseUnionArmLimit {
		t.Fatalf("union at the arm boundary failed: parsed=%#v err=%v", parsed, err)
	}
	assertVBScriptTypeParseBudgetError(t, strings.Join(append(union, "Overflow"), " | "), "union arms")

	buildNestedTemplate := func(depth int) string {
		text := "String"
		for index := 0; index < depth; index++ {
			text = "`value-${" + text + "}`"
		}
		return text
	}
	if parsed, err := parseVBScriptType(buildNestedTemplate(vbscriptTypeParseNestingLimit - 1)); err != nil || parsed.kind != vbscriptTypeTemplate {
		t.Fatalf("nested template at the depth boundary failed: parsed=%#v err=%v", parsed, err)
	}
	assertVBScriptTypeParseBudgetError(t, buildNestedTemplate(vbscriptTypeParseNestingLimit), "nesting depth")

	buildTemplateParts := func(interpolations int) string {
		return "`" + strings.Repeat("${String}", interpolations) + "`"
	}
	if _, err := parseVBScriptType(buildTemplateParts((vbscriptTypeParseTemplatePartLimit - 1) / 2)); err != nil {
		t.Fatalf("template parts below the boundary failed: %v", err)
	}
	assertVBScriptTypeParseBudgetError(t, buildTemplateParts(vbscriptTypeParseTemplatePartLimit/2), "template parts")

	buildNodeHeavyType := func() string {
		inner := make([]string, vbscriptTypeParseUnionArmLimit)
		for index := range inner {
			inner[index] = "Inner" + intString(index)
		}
		innerUnion := strings.Join(inner, " | ")
		outer := make([]string, vbscriptTypeParseNodeLimit/vbscriptTypeParseUnionArmLimit+1)
		for index := range outer {
			outer[index] = "`x-${" + innerUnion + "}`"
		}
		return strings.Join(outer, " | ")
	}
	assertVBScriptTypeParseBudgetError(t, buildNodeHeavyType(), "parsed nodes")
}

func TestVBScriptTypeParserBudgetFailuresWidenAndDoNotPartiallyInfer(t *testing.T) {
	longName := "T" + strings.Repeat("x", vbscriptTypeParseInputLengthLimit)
	parsed, err := parseVBScriptType(longName)
	if err == nil || !parsed.isUnknown() {
		t.Fatalf("overlong expression returned an exact type: parsed=%#v err=%v", parsed, err)
	}
	if inferred := vbscriptTypeUnionFromStrings(longName); !inferred.isUnknown() || inferred.String() != "Variant" {
		t.Fatalf("overlong expression did not widen to Variant: %#v", inferred)
	}

	source := `<%
' @type value As ` + longName + `
Dim value
%>`
	parsedDocument := core.ParseDocument("file:///tmp/overlong-type-annotation.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	server := &Server{settings: serverSettings{VBScriptTypeChecking: "strict", Locale: "ja"}}
	diagnostics := server.vbscriptTypeDiagnostics(parsedDocument)
	if len(diagnostics) == 0 || diagnostics[0].Code != "malformedTypeAnnotation" {
		t.Fatalf("overlong strict annotation diagnostics = %#v, want malformedTypeAnnotation", diagnostics)
	}
	localized := server.localizeDiagnostics(diagnostics)
	if !strings.Contains(localized[0].Message, "VBScript の型注釈が不正です") {
		t.Fatalf("overlong annotation was not localized: %#v", localized)
	}
}

func TestVBScriptTypeParserLongBudgetFailureAllocationsStayBounded(t *testing.T) {
	longName := "T" + strings.Repeat("x", vbscriptTypeParseInputLengthLimit+1)
	allocations := testing.AllocsPerRun(100, func() {
		parsed, err := parseVBScriptType(longName)
		if err == nil || !parsed.isUnknown() {
			t.Fatalf("overlong parse unexpectedly succeeded: parsed=%#v err=%v", parsed, err)
		}
	})
	if allocations > 8 {
		t.Fatalf("overlong parse required %f allocations per run, want <= 8", allocations)
	}
}

func assertVBScriptTypeParseBudgetError(t *testing.T, text, code string) {
	t.Helper()
	parsed, err := parseVBScriptType(text)
	if err == nil || !parsed.isUnknown() {
		t.Fatalf("parse(%q) = parsed=%#v err=%v, want a budget failure", text, parsed, err)
	}
	var budgetError *vbscriptTypeParseError
	if !errors.As(err, &budgetError) || budgetError.code != code {
		t.Fatalf("parse(%q) error = %T %v, want budget code %q", text, err, err, code)
	}
}

func TestVBScriptMalformedTypeAnnotationsOnlyDiagnoseInStrictMode(t *testing.T) {
	parsed := core.ParseDocument("file:///tmp/malformed-type.asp", `<%
' @type value As String |
Dim value
%>`, core.Settings{DefaultLanguage: "VBScript"})
	permissive := &Server{settings: serverSettings{VBScriptTypeChecking: "basic"}}
	if diagnostics := permissive.vbscriptTypeDiagnostics(parsed); len(diagnostics) != 0 {
		t.Fatalf("permissive type diagnostics = %#v, want none", diagnostics)
	}
	strict := &Server{settings: serverSettings{VBScriptTypeChecking: "strict"}}
	diagnostics := strict.vbscriptTypeDiagnostics(parsed)
	if !strings.Contains(mustJSONText(t, diagnostics), "malformedTypeAnnotation") {
		t.Fatalf("strict diagnostics missing malformed annotation: %#v", diagnostics)
	}
}

func TestVBScriptInvalidAnnotationNamesLocalizeAsMalformedTypeAnnotations(t *testing.T) {
	parsed := core.ParseDocument("file:///tmp/invalid-annotation-names.asp", `<%
' @type value-name As String
Dim value
' @member Customer As String
%>`, core.Settings{DefaultLanguage: "VBScript"})
	server := &Server{settings: serverSettings{VBScriptTypeChecking: "strict", Locale: "ja"}}
	diagnostics := server.vbscriptTypeDiagnostics(parsed)
	if len(diagnostics) != 2 {
		t.Fatalf("invalid annotation diagnostics = %#v, want 2", diagnostics)
	}
	localized := server.localizeDiagnostics(diagnostics)
	for _, diagnostic := range localized {
		if diagnostic.Code != "malformedTypeAnnotation" || !strings.Contains(diagnostic.Message, "VBScript の型注釈が不正です") {
			t.Fatalf("invalid annotation diagnostic was not localized: %#v", diagnostic)
		}
	}
}

func TestVBScriptTypeAnnotationDiagnosticsIgnoreApostrophesInsideStrings(t *testing.T) {
	parsed := core.ParseDocument("file:///tmp/string-type-text.asp", `<%
text = "' @type value As String |"
%>`, core.Settings{DefaultLanguage: "VBScript"})
	server := &Server{settings: serverSettings{VBScriptTypeChecking: "strict"}}
	if diagnostics := server.vbscriptTypeDiagnostics(parsed); len(diagnostics) != 0 {
		t.Fatalf("string content produced type annotation diagnostics: %#v", diagnostics)
	}
}

func TestParseVBScriptTypeAnnotationSupportsReturnAndMemberForms(t *testing.T) {
	for _, text := range []string{
		`@type route As "home" | 404`,
		`@param Render.path As ` + "`page-${String}.asp`",
		`@member Page.Path As String`,
		`@returns RenderResult String | Number`,
		`@returns "home" | "login"`,
	} {
		annotation, ok := parseVBScriptTypeAnnotationText(text)
		if !ok || annotation.err != nil {
			t.Fatalf("parseVBScriptTypeAnnotationText(%q) = %#v, ok=%t", text, annotation, ok)
		}
		if annotation.typeExpr.String() == "Variant" {
			t.Fatalf("parseVBScriptTypeAnnotationText(%q) lost its type", text)
		}
	}
}

func TestParseVBScriptTypeAnnotationValidatesNamesByKind(t *testing.T) {
	valid := []string{
		`@type value As String`,
		`@param Render.path As String`,
		`@param path As String`,
		`@member Page.Path As String`,
		`@returns Render String`,
	}
	for _, text := range valid {
		annotation, ok := parseVBScriptTypeAnnotationText(text)
		if !ok || annotation.err != nil {
			t.Errorf("valid annotation %q = %#v, ok=%t", text, annotation, ok)
		}
	}
	invalid := []string{
		`@type value-name As String`,
		`@type Value.Name As String`,
		`@param Render.path-name As String`,
		`@param Render.Path.Name As String`,
		`@member Page As String`,
		`@member Page.Path.Name As String`,
		`@returns Render.Name String`,
	}
	for _, text := range invalid {
		annotation, ok := parseVBScriptTypeAnnotationText(text)
		if !ok || annotation.err == nil {
			t.Errorf("invalid annotation %q = %#v, ok=%t; want an error", text, annotation, ok)
		}
	}
}

func TestVBScriptMutableLiteralInferencePreservesSingleValuesAndFiniteUnions(t *testing.T) {
	source := `<%
x = "next.asp"
n = 42
flag = True
route = "home"
route = "login"
%>`
	parsed := core.ParseDocument("file:///tmp/literal-inference.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	want := map[string]string{
		"x":     `"next.asp"`,
		"n":     "42",
		"flag":  "True",
		"route": `"home" | "login"`,
	}
	for _, declaration := range variableInlayDeclarations(parsed, true, nil) {
		expected, ok := want[strings.ToLower(declaration.Name)]
		if !ok {
			continue
		}
		if got := inferVBDeclarationType(parsed, declaration); got != expected {
			t.Errorf("%s inferred type = %q, want %q", declaration.Name, got, expected)
		}
	}
	hints := vbscriptVariableTypeInlayHints(parsed, lsp.Range{
		Start: lsp.Position{Line: 0, Character: 0},
		End:   core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text).PositionAt(len(parsed.Text)),
	}, vbscriptVariableTypeInlayOptions{VariableTypes: true})
	serialized := mustJSONForTest(t, hints)
	for _, expected := range []string{`As \"next.asp\"`, "As 42", "As True", `As \"home\" | \"login\"`} {
		if !strings.Contains(serialized, expected) {
			t.Errorf("literal inlay hints missing %q: %s", expected, serialized)
		}
	}
}

func TestVBScriptAnnotationInferencePreservesTemplateAndLiteralUnionSpelling(t *testing.T) {
	source := `<%
' @type path As ` + "`page-${String}.asp`" + `
Dim path
' @type status As "ready" | 200 | False
Dim status
%>`
	parsed := core.ParseDocument("file:///tmp/literal-annotation.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	for _, declaration := range variableInlayDeclarations(parsed, true, nil) {
		var want string
		switch strings.ToLower(declaration.Name) {
		case "path":
			want = "`page-${String}.asp`"
		case "status":
			want = `"ready" | 200 | False`
		default:
			continue
		}
		if got := inferVBDeclarationType(parsed, declaration); got != want {
			t.Errorf("%s annotation type = %q, want %q", declaration.Name, got, want)
		}
	}
}

func TestVBScriptAnnotationConsumersPreserveReturnAndVariableTypeExpressions(t *testing.T) {
	source := `<%
' @returns String | Number
Function BareReturn()
  BareReturn = 1
End Function
' @returns NamedReturn ` + "`page-${String}.asp`" + ` | "index"
Function NamedReturn()
  NamedReturn = "index"
End Function
' @returns String |
Function MalformedReturn()
  MalformedReturn = "ignored"
End Function
' @type path As ` + "`page-${String}.asp`" + `
Dim path
' @type mode As "home" | "login"
Dim mode
%>`
	parsed := core.ParseDocument("file:///tmp/annotation-consumers.asp", source, core.Settings{DefaultLanguage: "VBScript"})
	analysis := graphAnalysisTypes(parsed)
	if got := analysis.Returns["barereturn"]; got != "String | Number" {
		t.Fatalf("bare @returns type = %q, want full union", got)
	}
	if got := analysis.Returns["namedreturn"]; got != "`page-${String}.asp` | \"index\"" {
		t.Fatalf("named @returns type = %q, want full template union", got)
	}
	if _, ok := analysis.Returns["string"]; ok {
		t.Fatalf("bare @returns union was misclassified as a function named String: %#v", analysis.Returns)
	}
	if _, ok := analysis.Returns["malformedreturn"]; ok {
		t.Fatalf("malformed @returns annotation was accepted: %#v", analysis.Returns)
	}

	functionHints := vbscriptFunctionReturnTypeInlayHints(parsed, fullDocumentRange(source), true)
	functionHintText := mustJSONForTest(t, functionHints)
	for _, expected := range []string{"As String | Number", "As `page-${String}.asp` | \\\"index\\\""} {
		if !strings.Contains(functionHintText, expected) {
			t.Fatalf("function return inlay hints missing %q: %s", expected, functionHintText)
		}
	}
	if strings.Contains(functionHintText, "String |\"") {
		t.Fatalf("malformed return annotation leaked into inlay hints: %s", functionHintText)
	}

	server := &Server{}
	variableHints := vbscriptVariableTypeInlayHints(parsed, fullDocumentRange(source), vbscriptVariableTypeInlayOptions{VariableTypes: true})
	variableHintText := mustJSONForTest(t, variableHints)
	for _, expected := range []string{"As `page-${String}.asp`", `As \"home\" | \"login\"`} {
		if !strings.Contains(variableHintText, expected) {
			t.Fatalf("variable inlay hints missing %q: %s", expected, variableHintText)
		}
	}
	for _, test := range []struct {
		name string
		want string
	}{
		{name: "path", want: "`page-${String}.asp`"},
		{name: "mode", want: `\"home\" | \"login\"`},
	} {
		hover := server.vbscriptVariableHover(parsed, strings.Index(source, test.name), "", true, "en")
		if hover == nil || !strings.Contains(mustJSONForTest(t, hover), test.want) {
			t.Fatalf("%s hover did not preserve %q: %#v", test.name, test.want, hover)
		}
	}
	functionHover := server.vbscriptSignatureHover(parsed, strings.Index(source, "BareReturn"))
	if functionHover == nil || !strings.Contains(mustJSONForTest(t, functionHover), "Function BareReturn() As String | Number") {
		t.Fatalf("function hover did not preserve bare return union: %#v", functionHover)
	}
	namedFunctionHover := server.vbscriptSignatureHover(parsed, strings.Index(source, "NamedReturn"))
	if namedFunctionHover == nil || !strings.Contains(mustJSONForTest(t, namedFunctionHover), "Function NamedReturn() As `page-${String}.asp` | \\\"index\\\"") {
		t.Fatalf("function hover did not preserve named return template union: %#v", namedFunctionHover)
	}
}
