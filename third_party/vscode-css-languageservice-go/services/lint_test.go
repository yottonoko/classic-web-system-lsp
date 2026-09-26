package services

import (
	"reflect"
	"testing"

	"github.com/yottonoko/vscode-css-languageservice-go/languagefacts"
	"github.com/yottonoko/vscode-css-languageservice-go/lsp"
)

type lintCase struct {
	name     string
	input    string
	language string
	settings map[string]any
	expected []string
}

func TestLintEntriesBasicRules(t *testing.T) {
	t.Run("universal selector, empty rule", func(t *testing.T) {
		assertLintCases(t, []lintCase{
			{name: "universal selector", input: "* { color: red }", expected: []string{"universalSelector"}},
			{name: "universal selector in list first", input: "*, div { color: red }", expected: []string{"universalSelector"}},
			{name: "universal selector in list last", input: "div, * { color: red }", expected: []string{"universalSelector"}},
			{name: "universal selector child", input: "div > * { color: red }", expected: []string{"universalSelector"}},
			{name: "universal selector adjacent", input: "div + * { color: red }", expected: []string{"universalSelector"}},
		})
	})
	t.Run("properies ignored due to inline ", func(t *testing.T) {
		assertLintCases(t, []lintCase{
			{name: "inline float right", input: "selector { display: inline; float: right; }", expected: []string{"float"}},
			{name: "inline float none", input: "selector { display: inline; float: none; }", expected: []string{"float"}},
			{name: "inline-block float right", input: "selector { display: inline-block; float: right; }", expected: []string{"propertyIgnoredDueToDisplay", "float"}},
			{name: "inline-block float none", input: "selector { display: inline-block; float: none; }", expected: []string{"float"}},
			{name: "block vertical-align", input: "selector { display: block; vertical-align: center; }", expected: []string{"propertyIgnoredDueToDisplay"}},
			{name: "inline-block float none important", input: "selector { display: inline-block; float: none !important; }", expected: []string{"float", "important"}},
		})
	})
	assertLintCases(t, []lintCase{
		{name: "universal selector", input: "* { color: red }", expected: []string{"universalSelector"}},
		{name: "universal selector in list first", input: "*, div { color: red }", expected: []string{"universalSelector"}},
		{name: "universal selector in list last", input: "div, * { color: red }", expected: []string{"universalSelector"}},
		{name: "universal selector child", input: "div > * { color: red }", expected: []string{"universalSelector"}},
		{name: "universal selector adjacent", input: "div + * { color: red }", expected: []string{"universalSelector"}},
		{name: "empty ruleset", input: "selector {}", expected: []string{"emptyRules"}},
		{name: "inline float right", input: "selector { display: inline; float: right; }", expected: []string{"float"}},
		{name: "inline float none", input: "selector { display: inline; float: none; }", expected: []string{"float"}},
		{name: "inline-block float right", input: "selector { display: inline-block; float: right; }", expected: []string{"propertyIgnoredDueToDisplay", "float"}},
		{name: "inline-block float none", input: "selector { display: inline-block; float: none; }", expected: []string{"float"}},
		{name: "block vertical-align", input: "selector { display: block; vertical-align: center; }", expected: []string{"propertyIgnoredDueToDisplay"}},
		{name: "inline-block float none important", input: "selector { display: inline-block; float: none !important; }", expected: []string{"float", "important"}},
		{name: "avoid important", input: "selector { display: inline !important; }", expected: []string{"important"}},
		{name: "avoid float", input: "selector { float: right; }", expected: []string{"float"}},
		{name: "avoid id selectors", input: "#selector { display: inline; }", expected: []string{"idSelector"}},
	})
}

func TestLintEntriesZeroUnitsAndDuplicates(t *testing.T) {
	t.Run("zero with unit", func(t *testing.T) {
		assertLintCases(t, []lintCase{
			{name: "zero px", input: "selector { width: 0px }", expected: []string{"zeroUnits"}},
			{name: "zero mixed-case px", input: "selector { width: 0Px }", expected: []string{"zeroUnits"}},
			{name: "zero em", input: "selector { line-height: 0EM }", expected: []string{"zeroUnits"}},
			{name: "zero pc", input: "selector { line-height: 0pc }", expected: []string{"zeroUnits"}},
			{name: "zero in shorthand", input: "selector { outline: black 0em solid; }", expected: []string{"zeroUnits"}},
			{name: "zero in grid template", input: "selector { grid-template-columns: 40px 50px auto 0px 40px; }", expected: []string{"zeroUnits"}},
			{name: "zero percent allowed", input: "selector { min-height: 0% }"},
			{name: "zero in calc allowed", input: "selector { top: calc(0px - 10vw); }"},
		})
	})
	assertLintCases(t, []lintCase{
		{name: "zero px", input: "selector { width: 0px }", expected: []string{"zeroUnits"}},
		{name: "zero mixed-case px", input: "selector { width: 0Px }", expected: []string{"zeroUnits"}},
		{name: "zero em", input: "selector { line-height: 0EM }", expected: []string{"zeroUnits"}},
		{name: "zero pc", input: "selector { line-height: 0pc }", expected: []string{"zeroUnits"}},
		{name: "zero in shorthand", input: "selector { outline: black 0em solid; }", expected: []string{"zeroUnits"}},
		{name: "zero in grid template", input: "selector { grid-template-columns: 40px 50px auto 0px 40px; }", expected: []string{"zeroUnits"}},
		{name: "zero percent allowed", input: "selector { min-height: 0% }"},
		{name: "zero in calc allowed", input: "selector { top: calc(0px - 10vw); }"},
		{name: "duplicate declarations", input: "selector { color: red; color: blue }", expected: []string{"duplicateProperties", "duplicateProperties"}},
		{name: "duplicate vendor value ignored", input: "selector { color: -o-red; color: red }"},
	})
}

func TestLintEntriesUnknownPropertiesAndValidProperties(t *testing.T) {
	t.Run("unknown properties", func(t *testing.T) {
		assertLintCases(t, []lintCase{
			{name: "unknown ms vendor property", input: `selector { -ms-property: "rest is missing" }`, expected: []string{"unknownVendorSpecificProperties"}},
			{name: "unknown moz vendor property", input: `selector { -moz-box-shadow: "rest is missing" }`, expected: []string{"unknownVendorSpecificProperties", "vendorPrefix"}},
			{name: "known standard property", input: `selector { box-shadow: none }`},
			{name: "unknown standard property", input: `selector { box-property: "rest is missing" }`, expected: []string{"unknownProperties"}},
			{name: "export block ignored", input: `:export { prop: "some" }`},
			{name: "custom valid properties", input: `selector { foo: "some"; bar: 0px }`, settings: map[string]any{"validProperties": []string{"foo", "bar"}}},
			{name: "custom valid properties ignore nil", input: `selector { foo: "some"; }`, settings: map[string]any{"validProperties": []any{"foo", nil}}},
			{name: "custom valid properties still reject others", input: `selector { bar: "some"; }`, settings: map[string]any{"validProperties": []string{"foo"}}, expected: []string{"unknownProperties"}},
		})
	})
	assertLintCases(t, []lintCase{
		{name: "unknown ms vendor property", input: `selector { -ms-property: "rest is missing" }`, expected: []string{"unknownVendorSpecificProperties"}},
		{name: "unknown moz vendor property", input: `selector { -moz-box-shadow: "rest is missing" }`, expected: []string{"unknownVendorSpecificProperties", "vendorPrefix"}},
		{name: "known standard property", input: `selector { box-shadow: none }`},
		{name: "unknown standard property", input: `selector { box-property: "rest is missing" }`, expected: []string{"unknownProperties"}},
		{name: "export block ignored", input: `:export { prop: "some" }`},
		{name: "custom valid properties", input: `selector { foo: "some"; bar: 0px }`, settings: map[string]any{"validProperties": []string{"foo", "bar"}}},
		{name: "custom valid properties ignore nil", input: `selector { foo: "some"; }`, settings: map[string]any{"validProperties": []any{"foo", nil}}},
		{name: "custom valid properties still reject others", input: `selector { bar: "some"; }`, settings: map[string]any{"validProperties": []string{"foo"}}, expected: []string{"unknownProperties"}},
	})

	entries := lintEntryRules(t, `selector { Box-Property: "rest is missing" }`, nil)
	if len(entries) != 1 || entries[0].Message != "Unknown property: 'Box-Property'" {
		t.Fatalf("unknown property message = %#v", entries)
	}
}

func TestCustomDataDiagnostics(t *testing.T) {
	manager := languagefacts.NewDataManager(languagefacts.DataManagerOptions{
		CustomDataProviders: []languagefacts.CSSDataProvider{languagefacts.NewCSSDataProvider(languagefacts.CSSDataV1{
			Version: 1.1,
			Properties: []languagefacts.PropertyData{
				{Name: "foo"},
				{Name: "_foo"},
			},
			AtDirectives: []languagefacts.AtDirectiveData{{Name: "@foo"}},
		})},
	})

	t.Run("No unknown properties", func(t *testing.T) {
		for _, input := range []string{".foo { foo: 1; _foo: 1 }", ".foo { FOO: 1; }"} {
			document := lintDocument("css", input)
			if got := diagnosticCodes(Validate(document, manager, nil)); !reflect.DeepEqual(got, []string{}) {
				t.Fatalf("%s diagnostics = %#v", input, got)
			}
		}
	})

	t.Run("No unknown at-directives", func(t *testing.T) {
		document := lintDocument("css", `@foo 'bar';`)
		if got := diagnosticCodes(Validate(document, manager, nil)); !reflect.DeepEqual(got, []string{}) {
			t.Fatalf("diagnostics = %#v", got)
		}

		document = lintDocument("css", `@bar 'bar';`)
		if got := diagnosticCodes(Validate(document, manager, nil)); !reflect.DeepEqual(got, []string{"unknownAtRules"}) {
			t.Fatalf("unknown at-directive diagnostics = %#v", got)
		}
	})
}

func TestLintEntriesBoxModel(t *testing.T) {
	t.Run("box model", func(t *testing.T) {
		assertLintCases(t, []lintCase{
			{name: "border zero", input: `.mybox { height: 100px; border: 0; }`},
			{name: "border one px height", input: `.mybox { height: 100px; border: 1px; }`, expected: []string{"boxModel", "boxModel"}},
			{name: "padding bottom one px height", input: `.mybox { height: 100px; padding: 0 0 1px; }`, expected: []string{"boxModel", "boxModel"}},
			{name: "box sizing suppresses", input: `.mybox { height: 100px; border: 1px; box-sizing: border-box; }`},
		})
	})
	assertLintCases(t, []lintCase{
		{name: "border initial", input: `.mybox { height: 100px; border: initial; }`},
		{name: "border unset", input: `.mybox { height: 100px; border: unset; }`},
		{name: "border none", input: `.mybox { height: 100px; border: none; }`},
		{name: "border hidden", input: `.mybox { height: 100px; border: hidden; }`},
		{name: "border zero", input: `.mybox { height: 100px; border: 0; }`},
		{name: "border zero solid", input: `.mybox { height: 100px; border: 0 solid; }`},
		{name: "border one px none", input: `.mybox { height: 100px; border: 1px none; }`},
		{name: "border zero color", input: `.mybox { height: 100px; border: 0 solid #ccc; }`},
		{name: "border before height initial", input: `.mybox { border: initial; height: 100px; }`},
		{name: "border before height zero", input: `.mybox { border: 0; height: 100px; }`},
		{name: "border one px height", input: `.mybox { height: 100px; border: 1px; }`, expected: []string{"boxModel", "boxModel"}},
		{name: "border one px solid height", input: `.mybox { height: 100px; border: 1px solid; }`, expected: []string{"boxModel", "boxModel"}},
		{name: "border one px width", input: `.mybox { width: 100px; border: 1px; }`, expected: []string{"boxModel", "boxModel"}},
		{name: "border before height one px", input: `.mybox { border: 1px; height: 100px; }`, expected: []string{"boxModel", "boxModel"}},
		{name: "border before height one px solid", input: `.mybox { border: 1px solid; height: 100px; }`, expected: []string{"boxModel", "boxModel"}},
		{name: "border top initial", input: `.mybox { height: 100px; border-top: initial; }`},
		{name: "border top none", input: `.mybox { height: 100px; border-top: none; }`},
		{name: "border top zero", input: `.mybox { height: 100px; border-top: 0; }`},
		{name: "border top zero solid", input: `.mybox { height: 100px; border-top: 0 solid; }`},
		{name: "border top does not affect width", input: `.mybox { width: 100px; border-top: 1px; }`},
		{name: "border top solid does not affect width", input: `.mybox { width: 100px; border-top: 1px solid; }`},
		{name: "border top one px height", input: `.mybox { height: 100px; border-top: 1px; }`, expected: []string{"boxModel", "boxModel"}},
		{name: "border top one px solid height", input: `.mybox { height: 100px; border-top: 1px solid; }`, expected: []string{"boxModel", "boxModel"}},
		{name: "border width zero", input: `.mybox { height: 100px; border-width: 0; }`},
		{name: "border width two zeroes", input: `.mybox { height: 100px; border-width: 0 0; }`},
		{name: "border width three zeroes", input: `.mybox { height: 100px; border-width: 0 0 0; }`},
		{name: "border width four zeroes", input: `.mybox { height: 100px; border-width: 0 0 0 0; }`},
		{name: "border width vertical zero", input: `.mybox { height: 100px; border-width: 0 1px; }`},
		{name: "border width vertical zero four values", input: `.mybox { height: 100px; border-width: 0 1px 0 1px; }`},
		{name: "border width horizontal zero", input: `.mybox { width: 100px; border-width: 1px 0; }`},
		{name: "border width horizontal zero four values", input: `.mybox { width: 100px; border-width: 1px 0 1px 0; }`},
		{name: "border width one px height", input: `.mybox { height: 100px; border-width: 1px; }`, expected: []string{"boxModel", "boxModel"}},
		{name: "border width bottom one px height", input: `.mybox { height: 100px; border-width: 0 0 1px; }`, expected: []string{"boxModel", "boxModel"}},
		{name: "border width right one px width", input: `.mybox { width: 100px; border-width: 0 1px; }`, expected: []string{"boxModel", "boxModel"}},
		{name: "border width left one px width", input: `.mybox { width: 100px; border-width: 0 0 0 1px; }`, expected: []string{"boxModel", "boxModel"}},
		{name: "border style unset", input: `.mybox { height: 100px; border-style: unset; }`},
		{name: "border style initial", input: `.mybox { height: 100px; border-style: initial; }`},
		{name: "border style none", input: `.mybox { height: 100px; border-style: none; }`},
		{name: "border style hidden", input: `.mybox { height: 100px; border-style: hidden; }`},
		{name: "border style solid", input: `.mybox { height: 100px; border-style: solid; }`, expected: []string{"boxModel", "boxModel"}},
		{name: "border style dashed", input: `.mybox { height: 100px; border-style: dashed; }`, expected: []string{"boxModel", "boxModel"}},
		{name: "border top width zero", input: `.mybox { height: 100px; border-top-width: 0; }`},
		{name: "border top width does not affect width", input: `.mybox { width: 100px; border-top-width: 1px; }`},
		{name: "border top width one px height", input: `.mybox { height: 100px; border-top-width: 1px; }`, expected: []string{"boxModel", "boxModel"}},
		{name: "border top style unset", input: `.mybox { height: 100px; border-top-style: unset; }`},
		{name: "border top style does not affect width", input: `.mybox { width: 100px; border-top-style: solid; }`},
		{name: "border top style solid height", input: `.mybox { height: 100px; border-top-style: solid; }`, expected: []string{"boxModel", "boxModel"}},
		{name: "padding initial", input: `.mybox { height: 100px; padding: initial; }`},
		{name: "padding unset", input: `.mybox { height: 100px; padding: unset; }`},
		{name: "padding zero", input: `.mybox { height: 100px; padding: 0; }`},
		{name: "padding two zeroes", input: `.mybox { height: 100px; padding: 0 0; }`},
		{name: "padding three zeroes", input: `.mybox { height: 100px; padding: 0 0 0; }`},
		{name: "padding four zeroes", input: `.mybox { height: 100px; padding: 0 0 0 0; }`},
		{name: "padding vertical zero", input: `.mybox { height: 100px; padding: 0 1px; }`},
		{name: "padding vertical zero four values", input: `.mybox { height: 100px; padding: 0 1px 0 1px; }`},
		{name: "padding horizontal zero", input: `.mybox { width: 100px; padding: 1px 0; }`},
		{name: "padding horizontal zero three values", input: `.mybox { width: 100px; padding: 1px 0 1px; }`},
		{name: "padding one px height", input: `.mybox { height: 100px; padding: 1px; }`, expected: []string{"boxModel", "boxModel"}},
		{name: "padding top bottom one px height", input: `.mybox { height: 100px; padding: 1px 0; }`, expected: []string{"boxModel", "boxModel"}},
		{name: "padding bottom one px height", input: `.mybox { height: 100px; padding: 0 0 1px; }`, expected: []string{"boxModel", "boxModel"}},
		{name: "box sizing suppresses", input: `.mybox { height: 100px; border: 1px; box-sizing: border-box; }`},
		{name: "border overridden by sides", input: `.mybox { height: 100px; border: 1px; border-top: 0; border-bottom: 0; }`},
		{name: "incomplete padding", input: `.mybox { padding:; }`},
		{name: "incomplete border", input: `.mybox { border: `},
		{name: "incomplete border with padding", input: `.mybox { height: 100px; padding: 1px; border: }`, expected: []string{"boxModel", "boxModel"}},
	})
}

func TestLintEntriesIEHacks(t *testing.T) {
	t.Run("IE hacks", func(t *testing.T) {
		// Upstream keeps this test name with all assertions disabled because IE star hacks are incompatible with CSS nesting.
	})
}

func TestLintEntriesVendorPrefixes(t *testing.T) {
	t.Run("vendor specific prefixes", func(t *testing.T) {
		assertLintCases(t, []lintCase{
			{name: "missing standard and sibling vendor prefixes", input: "selector { -moz-animation: none }", expected: []string{"compatibleVendorPrefixes", "vendorPrefix"}},
			{name: "missing sibling vendor prefixes", input: "selector { -moz-transform: none; transform: none }", expected: []string{"compatibleVendorPrefixes"}},
			{name: "standard transform only", input: "selector { transform: none; }"},
			{name: "all transform prefixes", input: "selector { -moz-transform: none; transform: none; -o-transform: none; -webkit-transform: none; -ms-transform: none; }"},
			{name: "custom property ignored", input: "selector { --transform: none; }"},
			{name: "webkit appearance needs standard", input: "selector { -webkit-appearance: none }", expected: []string{"vendorPrefix"}},
		})
	})
}

func TestLintEntriesVendorPseudoElementPrefixSuppression(t *testing.T) {
	t.Run("ignore missing standard properties in contexts with vendor specific pseudo-element", func(t *testing.T) {
		assertLintCases(t, []lintCase{
			{name: "matching vendor pseudo-element suppresses standard property", input: `input[type="range"]::-webkit-slider-thumb { -webkit-appearance: none; }`},
			{name: "nested matching vendor pseudo-element suppresses standard property", input: `input[type="range"]::-webkit-slider-thumb { color: black; & selector { -webkit-appearance: none; } }`},
			{name: "mixed vendor pseudo-element still reports missing standard", input: `input[type="range"]::-webkit-slider-thumb { -webkit-appearance: none; -moz-appearance: none; }`, expected: []string{"vendorPrefix"}},
		})
	})
}

func TestLintEntriesFontFaceRequiredProperties(t *testing.T) {
	t.Run("font-face required properties", func(t *testing.T) {
		assertLintCases(t, []lintCase{
			{name: "empty font face", input: "@font-face { }", expected: []string{"fontFaceProperties"}},
			{name: "font face src only", input: "@font-face { src: url(test.ttf) }", expected: []string{"fontFaceProperties"}},
			{name: "font face family only", input: "@font-face { font-family: 'name' }", expected: []string{"fontFaceProperties"}},
			{name: "font face complete", input: "@font-face { src: url(test.ttf); font-family: 'name' }"},
			{name: "scss font face interpolated property", language: "scss", input: "@font-face { font-#{family}: foo }"},
			{name: "scss font face nested property", language: "scss", input: "@font-face { font: {family: foo } }"},
			{name: "scss font face control block", language: "scss", input: "@font-face { @if true { } }"},
		})
	})
}

func TestLintEntriesKeyframes(t *testing.T) {
	t.Run("keyframes", func(t *testing.T) {
		assertLintCases(t, []lintCase{
			{name: "standard keyframes", input: "@keyframes foo { }"},
			{name: "standard plus one vendor keyframes", input: "@keyframes foo { } @-moz-keyframes foo { }", expected: []string{"compatibleVendorPrefixes"}},
			{name: "vendor keyframes without standard", input: "@-moz-keyframes foo { }", expected: []string{"vendorPrefix", "compatibleVendorPrefixes"}},
		})
	})
}

func TestSCSSLintEntries(t *testing.T) {
	t.Run("ID selectors", func(t *testing.T) {
		assertLintCases(t, []lintCase{
			{name: "id selector", language: "scss", input: "#id { color: red; }", expected: []string{"idSelector"}},
			{name: "element id selector", language: "scss", input: "element#id { color: red; }", expected: []string{"idSelector"}},
			{name: "interpolated id selector suffix", language: "scss", input: "#id__#{foo} { color: red; }", expected: []string{"idSelector"}},
		})
	})
	t.Run("Interpolation selectors", func(t *testing.T) {
		assertLintCases(t, []lintCase{
			{name: "interpolation selector", language: "scss", input: "#{foo} { color: red; }"},
			{name: "interpolation selector suffix", language: "scss", input: "#{foo}__cont { color: red; }"},
			{name: "interpolation selector class", language: "scss", input: "#{foo}.class { color: red; }"},
		})
	})
	t.Run("font-face required properties", func(t *testing.T) {
		assertLintCases(t, []lintCase{
			{name: "scss font face empty", language: "scss", input: "@font-face { }", expected: []string{"fontFaceProperties"}},
			{name: "scss font face src only", language: "scss", input: "@font-face { src: url(test.ttf) }", expected: []string{"fontFaceProperties"}},
			{name: "scss font face family only", language: "scss", input: "@font-face { font-family: 'name' }", expected: []string{"fontFaceProperties"}},
			{name: "scss font face interpolated property", language: "scss", input: "@font-face { font-#{family}: foo }"},
			{name: "scss font face nested property", language: "scss", input: "@font-face { font: {family: foo } }"},
			{name: "scss font face control block", language: "scss", input: "@font-face { @if true { } }"},
		})
	})
	t.Run("unknown properties", func(t *testing.T) {
		assertLintCases(t, []lintCase{
			{name: "unknown ms vendor property", language: "scss", input: `selector { -ms-property: "rest is missing" }`, expected: []string{"unknownVendorSpecificProperties"}},
			{name: "unknown moz vendor property", language: "scss", input: `selector { -moz-box-shadow: "rest is missing" }`, expected: []string{"unknownVendorSpecificProperties", "vendorPrefix"}},
			{name: "known standard property", language: "scss", input: "selector { box-shadow: none }"},
			{name: "interpolated vendor property", language: "scss", input: "selector { -moz-#{box}-shadow: none }"},
			{name: "nested property", language: "scss", input: "selector { outer: { nested : blue }"},
			{name: "export block ignored", language: "scss", input: `:export { prop: "some" }`},
		})
	})
	t.Run("vendor specific prefixes", func(t *testing.T) {
		assertLintCases(t, []lintCase{
			{name: "missing standard and sibling vendor prefixes", language: "scss", input: "selector { -moz-animation: none }", expected: []string{"compatibleVendorPrefixes", "vendorPrefix"}},
			{name: "missing sibling vendor prefixes", language: "scss", input: "selector { -moz-transform: none; transform: none }", expected: []string{"compatibleVendorPrefixes"}},
			{name: "all transform prefixes", language: "scss", input: "selector { -moz-transform: none; transform: none; -o-transform: none; -webkit-transform: none; -ms-transform: none; }"},
		})
	})
	assertLintCases(t, []lintCase{
		{name: "empty nested ruleset", language: "scss", input: "selector { color: red; nested {} }", expected: []string{"emptyRules"}},
		{name: "id selector", language: "scss", input: "#id { color: red; }", expected: []string{"idSelector"}},
		{name: "element id selector", language: "scss", input: "element#id { color: red; }", expected: []string{"idSelector"}},
		{name: "interpolated id selector suffix", language: "scss", input: "#id__#{foo} { color: red; }", expected: []string{"idSelector"}},
		{name: "interpolation selector", language: "scss", input: "#{foo} { color: red; }"},
		{name: "interpolation selector suffix", language: "scss", input: "#{foo}__cont { color: red; }"},
		{name: "interpolation selector class", language: "scss", input: "#{foo}.class { color: red; }"},
		{name: "unknown ms vendor property", language: "scss", input: `selector { -ms-property: "rest is missing" }`, expected: []string{"unknownVendorSpecificProperties"}},
		{name: "unknown moz vendor property", language: "scss", input: `selector { -moz-box-shadow: "rest is missing" }`, expected: []string{"unknownVendorSpecificProperties", "vendorPrefix"}},
		{name: "known standard property", language: "scss", input: "selector { box-shadow: none }"},
		{name: "interpolated vendor property", language: "scss", input: "selector { -moz-#{box}-shadow: none }"},
		{name: "nested property", language: "scss", input: "selector { outer: { nested : blue }"},
		{name: "export block ignored", language: "scss", input: `:export { prop: "some" }`},
		{name: "missing standard and sibling vendor prefixes", language: "scss", input: "selector { -moz-animation: none }", expected: []string{"compatibleVendorPrefixes", "vendorPrefix"}},
		{name: "missing sibling vendor prefixes", language: "scss", input: "selector { -moz-transform: none; transform: none }", expected: []string{"compatibleVendorPrefixes"}},
		{name: "all transform prefixes", language: "scss", input: "selector { -moz-transform: none; transform: none; -o-transform: none; -webkit-transform: none; -ms-transform: none; }"},
	})
}

func TestLESSLintEntries(t *testing.T) {
	t.Run("unknown properties", func(t *testing.T) {
		assertLintCases(t, []lintCase{
			{name: "property merge append", language: "less", input: "selector { box-shadow+: 0 0 20px black; }"},
			{name: "property merge space append", language: "less", input: "selector { transform+_: rotate(15deg); }"},
		})
	})
}

func TestValidateFiltersIgnoredRulesAndReturnsDiagnostics(t *testing.T) {
	document := lintDocument("css", "selector {} #selector { float: right }")
	manager := languagefacts.NewDataManager(languagefacts.DataManagerOptions{})

	diagnostics := Validate(document, manager, nil)
	if gotRules := diagnosticCodes(diagnostics); !reflect.DeepEqual(gotRules, []string{"emptyRules"}) {
		t.Fatalf("default diagnostics = %#v", gotRules)
	}
	if diagnostics[0].Source != "css" || diagnostics[0].Severity != lsp.DiagnosticSeverityWarning {
		t.Fatalf("diagnostic metadata = %#v", diagnostics[0])
	}

	diagnostics = Validate(document, manager, map[string]any{"idSelector": "warning", "float": "error"})
	if gotRules := diagnosticCodes(diagnostics); !reflect.DeepEqual(gotRules, []string{"emptyRules", "idSelector", "float"}) {
		t.Fatalf("configured diagnostics = %#v", gotRules)
	}
	if diagnostics[2].Severity != lsp.DiagnosticSeverityError {
		t.Fatalf("float severity = %#v", diagnostics[2].Severity)
	}
}

func assertLintCases(t *testing.T, cases []lintCase) {
	t.Helper()
	for _, tt := range cases {
		languages := []string{tt.language}
		if tt.language == "" {
			languages = []string{"css", "less", "scss"}
		}
		for _, language := range languages {
			t.Run(tt.name+"/"+language, func(t *testing.T) {
				assertLintRulesForLanguage(t, language, tt.input, tt.settings, tt.expected)
			})
		}
	}
}

func assertLintRulesForLanguage(t *testing.T, language, input string, settings map[string]any, expected []string) {
	t.Helper()
	entries := lintEntryRulesForLanguage(t, language, input, settings)
	actual := make([]string, len(entries))
	for i, entry := range entries {
		actual[i] = entry.Rule.ID
	}
	if expected == nil {
		expected = []string{}
	}
	if !sameStringMultiset(actual, expected) {
		t.Fatalf("%s\nactual: %#v\nwant:   %#v", input, actual, expected)
	}
}

func lintEntryRules(t *testing.T, input string, settings map[string]any) []LintEntry {
	t.Helper()
	return lintEntryRulesForLanguage(t, "css", input, settings)
}

func lintEntryRulesForLanguage(t *testing.T, language, input string, settings map[string]any) []LintEntry {
	t.Helper()
	document := lintDocument(language, input)
	manager := languagefacts.NewDataManager(languagefacts.DataManagerOptions{})
	return LintEntries(document, manager, NewLintConfiguration(settings), LevelError|LevelWarning|LevelIgnore)
}

func diagnosticCodes(diagnostics []lsp.Diagnostic) []string {
	codes := make([]string, len(diagnostics))
	for i, diagnostic := range diagnostics {
		codes[i] = diagnostic.Code.(string)
	}
	return codes
}

func lintDocument(languageID, input string) *lsp.TextDocument {
	return lsp.NewTextDocument(lsp.DocumentURI("test://test/test."+languageID), languageID, 0, input)
}

func sameStringMultiset(actual, expected []string) bool {
	if len(actual) != len(expected) {
		return false
	}
	counts := map[string]int{}
	for _, value := range actual {
		counts[value]++
	}
	for _, value := range expected {
		counts[value]--
	}
	for _, count := range counts {
		if count != 0 {
			return false
		}
	}
	return true
}
