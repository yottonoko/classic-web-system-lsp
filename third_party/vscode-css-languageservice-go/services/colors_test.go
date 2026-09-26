package services

import (
	"math"
	"reflect"
	"testing"

	"github.com/yottonoko/vscode-css-languageservice-go/languagefacts"
	"github.com/yottonoko/vscode-css-languageservice-go/lsp"
)

func TestDocumentColors(t *testing.T) {
	t.Run("is color", func(t *testing.T) {
		assertDocumentColors(t, "body { backgroundColor: #ff9977; }",
			lsp.ColorInformation{Color: languagefacts.ColorFrom256RGB(0xff, 0x99, 0x77), Range: offsetRange(24, 31)},
		)
		assertDocumentColors(t, "body { backgroundColor: hsl(0, 0%, 100%); }",
			lsp.ColorInformation{Color: languagefacts.ColorFrom256RGB(255, 255, 255), Range: offsetRange(24, 40)},
		)
		assertDocumentColors(t, ".oo { color: rgb(1,40,1); borderColor: hsl(120, 75%, 85%) }",
			lsp.ColorInformation{Color: languagefacts.ColorFrom256RGB(1, 40, 1), Range: offsetRange(13, 24)},
			lsp.ColorInformation{Color: languagefacts.ColorFromHSL(120, 0.75, 0.85), Range: offsetRange(39, 57)},
		)
		assertDocumentColors(t, "body { backgroundColor: rgba(1, 40, 1, 0.3); }",
			lsp.ColorInformation{Color: languagefacts.ColorFrom256RGB(1, 40, 1, 0.3), Range: offsetRange(24, 43)},
		)
		assertDocumentColors(t, "body { backgroundColor: hwb(194 0% 0% / .5); }",
			lsp.ColorInformation{Color: languagefacts.ColorFromHWB(194, 0, 0, 0.5), Range: offsetRange(24, 43)},
		)
		assertDocumentColors(t, "body { color: lab(46.41 -39.24 33.51); }",
			lsp.ColorInformation{Color: languagefacts.ColorFromLAB(46.41, -39.24, 33.51), Range: offsetRange(14, 37)},
		)
		assertDocumentColors(t, "body { color: lch(46.41 51.60 139.50); }",
			lsp.ColorInformation{Color: languagefacts.ColorFromLCH(46.41, 51.60, 139.50), Range: offsetRange(14, 37)},
		)
		assertDocumentColors(t, "body { color: oklab(.53376 .13032 -.21371); }",
			lsp.ColorInformation{Color: languagefacts.ColorFromOKLAB(.53376, .13032, -.21371), Range: offsetRange(14, 42)},
		)
		assertDocumentColors(t, "body { color: oklch(0.52 0.13 143.39); }",
			lsp.ColorInformation{Color: *languagefacts.ColorFromOKLCH(0.52, 0.13, 143.39), Range: offsetRange(14, 37)},
		)
		assertDocumentColors(t, "#main { color: red }",
			lsp.ColorInformation{Color: languagefacts.ColorFrom256RGB(0xff, 0, 0), Range: offsetRange(15, 18)},
		)
		assertDocumentColors(t, "#main { color: slateblue }",
			lsp.ColorInformation{Color: languagefacts.ColorFrom256RGB(106, 90, 205), Range: offsetRange(15, 24)},
		)
		assertDocumentColorAt(t, "#main { color: #231 }", "#231", colorPtr(languagefacts.ColorFrom256RGB(0x22, 0x33, 0x11)))
		assertDocumentColors(t, "#main { red: 1 }")
		assertDocumentColors(t, "#red { foo: 1 }")
		assertDocumentColorAt(t, "#main { color: #1836f6 }", "1836f6", colorPtr(languagefacts.ColorFrom256RGB(0x18, 0x36, 0xf6)))
		assertDocumentColorAt(t, "#main { color: #0F0E024E }", "0F0E024E", colorPtr(languagefacts.ColorFrom256RGB(0x0f, 0x0e, 0x02, float64(0x4e)/0xff)))
		assertDocumentColorAt(t, "#main { color: rgb(34, 89, 234) }", "rgb", colorPtr(languagefacts.ColorFrom256RGB(34, 89, 234)))
		assertDocumentColorAt(t, "#main { color: rgb(100%, 34%, 10%, 50%) }", "rgb", colorPtr(languagefacts.ColorFrom256RGB(255, 255*0.34, 255*0.1, 0.5)))
		assertDocumentColorAt(t, "#main { color: rgba(+78, 40.6, 99%, 1% ) }", "rgba", colorPtr(languagefacts.ColorFrom256RGB(78, 40.6, 255*0.99, 0.01)))
		assertDocumentColorAt(t, "#main { color: hsl(120deg, 100%, 50%) }", "hsl", colorPtr(languagefacts.ColorFrom256RGB(0, 255, 0)))
		assertDocumentColorAt(t, "#main { color: hsl(180,100%,25%, 0.33) }", "hsl", colorPtr(languagefacts.ColorFrom256RGB(0, 0.5*255, 0.5*255, 0.33)))
		assertDocumentColorAt(t, "#main { color: hsl(30,20%,30%, 0) }", "hsl", colorPtr(languagefacts.ColorFrom256RGB(92, 77, 61, 0)))
		assertDocumentColorAt(t, "#main { color: hsla(38deg,89%,89%, 0) }", "hsla", colorPtr(languagefacts.ColorFrom256RGB(252, 234, 202, 0)))
		assertDocumentColorAt(t, "#main { color: hsl(0.5turn, 100%, 50%) }", "hsl", colorPtr(languagefacts.ColorFrom256RGB(0, 255, 255, 1)))
		assertDocumentColorAt(t, "#main { color: hsl(1.5turn, 100%, 50%) }", "hsl", colorPtr(languagefacts.ColorFrom256RGB(0, 255, 255, 1)))
		assertDocumentColorAt(t, "#main { color: hsl(200grad, 100%, 50%) }", "hsl", colorPtr(languagefacts.ColorFrom256RGB(0, 255, 255, 1)))
		assertDocumentColorAt(t, "#main { color: hsl(3.14159rad, 100%, 50%) }", "hsl", colorPtr(languagefacts.ColorFrom256RGB(0, 255, 255, 1)))
		assertDocumentColorAt(t, "#main { color: hsl(0.13turn, 97%, 32%) }", "hsl", colorPtr(languagefacts.ColorFrom256RGB(161, 126, 2, 1)))
		assertDocumentColorAt(t, "#main { color: hsl(124grad, 71%, 45%) }", "hsl", colorPtr(languagefacts.ColorFrom256RGB(56, 196, 33, 1)))
		assertDocumentColorAt(t, "#main { color: hsl(2.35112rad, 76%, 63%) }", "hsl", colorPtr(languagefacts.ColorFrom256RGB(89, 232, 124, 1)))
		assertDocumentColorAt(t, "#main { color: rgba(0.7) }", "rgba", nil)
		assertDocumentColors(t, "[green] {}")
		assertDocumentColors(t, "[data-color=green] {}")
		assertDocumentColorAt(t, "#main { color: rgb(34 89 234) }", "rgb", colorPtr(languagefacts.ColorFrom256RGB(34, 89, 234)))
		assertDocumentColorAt(t, "#main { color: rgb(34 89 234 / 0.5) }", "rgb", colorPtr(languagefacts.ColorFrom256RGB(34, 89, 234, 0.5)))
		assertDocumentColorAt(t, "#main { color: rgb(34 89 234 / 100%) }", "rgb", colorPtr(languagefacts.ColorFrom256RGB(34, 89, 234)))
		assertDocumentColorAt(t, "#main { color: hsla(240 100% 50% / .05) }", "hsla", colorPtr(languagefacts.ColorFrom256RGB(0, 0, 255, 0.05)))
		assertDocumentColorAt(t, "#main { color: hwb(120 0% 0% / .05) }", "hwb", colorPtr(languagefacts.ColorFrom256RGB(0, 255, 0, 0.05)))
		assertDocumentColorAt(t, "#main { color: hwb(36 33% 35%) }", "hwb", colorPtr(languagefacts.ColorFrom256RGB(166, 133, 84)))
		assertDocumentColorAt(t, "#main { color: lab(90 100 100) }", "lab", colorPtr(languagefacts.ColorFrom256RGB(255, 112, 0)))
		assertDocumentColorAt(t, "#main { color: lab(90% 50 -50) }", "lab", colorPtr(languagefacts.ColorFrom256RGB(255, 195, 255)))
		assertDocumentColorAt(t, "#main { color: lab(46.41 39.24 33.51) }", "lab", colorPtr(languagefacts.ColorFrom256RGB(180, 79, 56)))
		assertDocumentColorAt(t, "#main { color: lab(46.41 -39.24 33.51) }", "lab", colorPtr(languagefacts.ColorFrom256RGB(50, 125, 50)))
		assertDocumentColorAt(t, "#main { color: lch(46.41, 51.60, 139.50) }", "lch", colorPtr(languagefacts.ColorFrom256RGB(50, 125, 50)))
		assertDocumentColorAt(t, "#main { color: oklab(62.8% 56.25% 31.5%) }", "oklab", colorPtr(languagefacts.ColorFrom256RGB(255, 0, 0)))
		assertDocumentColorAt(t, "#main { color: oklab(62.796% 0.22486 0.12585) }", "oklab", colorPtr(languagefacts.ColorFrom256RGB(255, 0, 0)))
		assertDocumentColorAt(t, "#main { color: oklab(.53376 .13032 -.21371) }", "oklab", colorPtr(languagefacts.ColorFrom256RGB(138, 43, 226)))
		assertDocumentColorAt(t, "#main { color: oklch(62.7955% 0.257683 29.2339) }", "oklch", colorPtr(languagefacts.ColorFrom256RGB(255, 0, 0)))
		assertDocumentColorAt(t, "#main { color: oklch(0.70167 0.32249 328.36deg) }", "oklch", colorPtr(languagefacts.ColorFrom256RGB(255, 0, 255)))
		assertDocumentColorAt(t, "#main { color: oklch(.8241 26.5225% 0.84891) }", "oklch", colorPtr(languagefacts.ColorFrom256RGB(255, 168, 193)))
		assertDocumentColorAt(t, "#main { color: oklch(0% 0 none) }", "oklch", colorPtr(languagefacts.ColorFrom256RGB(0, 0, 0)))
	})
}

func TestLESSDocumentColors(t *testing.T) {
	assertDocumentColorsForLanguage(t, "less", "@foo: #ff9977;",
		lsp.ColorInformation{Color: languagefacts.ColorFrom256RGB(0xff, 0x99, 0x77), Range: offsetRange(6, 13)},
	)
	assertDocumentColorsForLanguage(t, "less", "body { @foo: hsl(0, 0%, 100%); }",
		lsp.ColorInformation{Color: languagefacts.ColorFrom256RGB(255, 255, 255), Range: offsetRange(13, 29)},
	)
	assertDocumentColorsForLanguage(t, "less", "body { @foo: hsl(0, 1%, 100%); }",
		lsp.ColorInformation{Color: languagefacts.ColorFrom256RGB(255, 255, 255), Range: offsetRange(13, 29)},
	)
}

func TestCSSNavigationColorsPortedNamedCases(t *testing.T) {
	t.Run("color symbols", func(t *testing.T) {
		assertDocumentColors(t, "body { backgroundColor: #ff9977; }",
			lsp.ColorInformation{Color: languagefacts.ColorFrom256RGB(0xff, 0x99, 0x77), Range: offsetRange(24, 31)},
		)
		assertDocumentColors(t, "body { backgroundColor: hsl(0, 0%, 100%); }",
			lsp.ColorInformation{Color: languagefacts.ColorFrom256RGB(255, 255, 255), Range: offsetRange(24, 40)},
		)
		assertDocumentColors(t, "body { backgroundColor: hsl(0, 1%, 100%); }",
			lsp.ColorInformation{Color: languagefacts.ColorFrom256RGB(255, 255, 255), Range: offsetRange(24, 40)},
		)
		assertDocumentColors(t, ".oo { color: rgb(1,40,1); borderColor: hsl(120, 75%, 85%) }",
			lsp.ColorInformation{Color: languagefacts.ColorFrom256RGB(1, 40, 1), Range: offsetRange(13, 24)},
			lsp.ColorInformation{Color: languagefacts.ColorFromHSL(120, 0.75, 0.85), Range: offsetRange(39, 57)},
		)
		assertDocumentColors(t, "body { backgroundColor: rgba(1, 40, 1, 0.3); }",
			lsp.ColorInformation{Color: languagefacts.ColorFrom256RGB(1, 40, 1, 0.3), Range: offsetRange(24, 43)},
		)
		assertDocumentColors(t, "body { backgroundColor: hwb(194 0% 0% / .5); }",
			lsp.ColorInformation{Color: languagefacts.ColorFromHWB(194, 0, 0, 0.5), Range: offsetRange(24, 43)},
		)
	})
	t.Run("color presentations", func(t *testing.T) {
		assertColorPresentations(t,
			languagefacts.ColorFrom256RGB(255, 0, 0),
			[]string{
				"rgb(255, 0, 0)",
				"#ff0000",
				"hsl(0, 100%, 50%)",
				"hwb(0 0% 0%)",
				"lab(53.23% 80.11 67.22)",
				"lch(53.23% 104.58 40)",
				"oklab(62.793% 0.22489 0.1258)",
				"oklch(62.793% 0.25768 29.223)",
			},
		)
		assertColorPresentations(t,
			languagefacts.ColorFrom256RGB(77, 33, 111, 0.5),
			[]string{
				"rgba(77, 33, 111, 0.5)",
				"#4d216f80",
				"hsla(274, 54%, 28%, 0.5)",
				"hwb(274 13% 56% / 0.5)",
				"lab(23.04% 35.9 -36.96 / 0.5)",
				"lch(23.04% 51.53 314.16 / 0.5)",
				"oklab(35.231% 0.0782 -0.10478 / 0.5)",
				"oklch(35.231% 0.13074 306.734 / 0.5)",
			},
		)
	})
}

func TestLESSNavigationColorsPortedNamedCases(t *testing.T) {
	t.Run("color symbols", func(t *testing.T) {
		assertDocumentColorsForLanguage(t, "less", "@foo: #ff9977;",
			lsp.ColorInformation{Color: languagefacts.ColorFrom256RGB(0xff, 0x99, 0x77), Range: offsetRange(6, 13)},
		)
		assertDocumentColorsForLanguage(t, "less", "body { @foo: hsl(0, 0%, 100%); }",
			lsp.ColorInformation{Color: languagefacts.ColorFrom256RGB(255, 255, 255), Range: offsetRange(13, 29)},
		)
		assertDocumentColorsForLanguage(t, "less", "body { @foo: hsl(0, 1%, 100%); }",
			lsp.ColorInformation{Color: languagefacts.ColorFrom256RGB(255, 255, 255), Range: offsetRange(13, 29)},
		)
	})
}

func TestSCSSNavigationColorsPortedNamedCases(t *testing.T) {
	t.Run("color symbols", func(t *testing.T) {
		assertDocumentColorsForLanguage(t, "scss", "$colors: (blue: $blue,indigo: $indigo)")
		assertDocumentColorsForLanguage(t, "scss", "#main { color: foo(red) }",
			lsp.ColorInformation{Color: languagefacts.ColorFrom256RGB(0xff, 0, 0), Range: offsetRange(19, 22)},
		)
		assertDocumentColorsForLanguage(t, "scss", "#main { color: red() }")
		assertDocumentColorsForLanguage(t, "scss", "#main { red { nested: 1px } }")
		assertDocumentColorsForLanguage(t, "scss", "#main { @include red; }")
		assertDocumentColorAtForLanguage(t, "scss", "#main { @include foo($f: red); }", "red", colorPtr(languagefacts.ColorFrom256RGB(0xff, 0, 0)))
		assertDocumentColorAtForLanguage(t, "scss", "@function red($p) { @return 1px; }", "red", nil)
		assertDocumentColorAtForLanguage(t, "scss", "@function foo($p) { @return red; }", "red", colorPtr(languagefacts.ColorFrom256RGB(0xff, 0, 0)))
		assertDocumentColorAtForLanguage(t, "scss", "@function foo($r: red) { @return $r; }", "red", colorPtr(languagefacts.ColorFrom256RGB(0xff, 0, 0)))
		assertDocumentColorAtForLanguage(t, "scss", "#main { color: rgba($input-border, 0.7) }", "rgba", nil)
		assertDocumentColorAtForLanguage(t, "scss", "#main { color: rgba($input-border, 1, 1, 0.7) }", "rgba", nil)
	})
}

func TestColorPresentations(t *testing.T) {
	assertColorPresentations(t,
		languagefacts.ColorFrom256RGB(255, 0, 0),
		[]string{
			"rgb(255, 0, 0)",
			"#ff0000",
			"hsl(0, 100%, 50%)",
			"hwb(0 0% 0%)",
			"lab(53.23% 80.11 67.22)",
			"lch(53.23% 104.58 40)",
			"oklab(62.793% 0.22489 0.1258)",
			"oklch(62.793% 0.25768 29.223)",
		},
	)
	assertColorPresentations(t,
		languagefacts.ColorFrom256RGB(77, 33, 111, 0.5),
		[]string{
			"rgba(77, 33, 111, 0.5)",
			"#4d216f80",
			"hsla(274, 54%, 28%, 0.5)",
			"hwb(274 13% 56% / 0.5)",
			"lab(23.04% 35.9 -36.96 / 0.5)",
			"lch(23.04% 51.53 314.16 / 0.5)",
			"oklab(35.231% 0.0782 -0.10478 / 0.5)",
			"oklch(35.231% 0.13074 306.734 / 0.5)",
		},
	)
}

func assertDocumentColors(t *testing.T, input string, expected ...lsp.ColorInformation) {
	t.Helper()
	assertDocumentColorsForLanguage(t, "css", input, expected...)
}

func assertDocumentColorAt(t *testing.T, input, selection string, expected *lsp.Color) {
	t.Helper()
	assertDocumentColorAtForLanguage(t, "css", input, selection, expected)
}

func assertDocumentColorAtForLanguage(t *testing.T, languageID, input, selection string, expected *lsp.Color) {
	t.Helper()
	document := lsp.NewTextDocument(lsp.DocumentURI("test://test/test."+languageID), languageID, 0, input)
	offset := stringsIndex(input, selection)
	actualColors := FindDocumentColors(document)
	for _, actual := range actualColors {
		start := document.OffsetAt(actual.Range.Start)
		end := document.OffsetAt(actual.Range.End)
		if offset < start || offset > end {
			continue
		}
		if expected == nil {
			t.Fatalf("%s\nunexpected color at %q: %#v", input, selection, actual.Color)
		}
		if !colorsApproximatelyEqual(actual.Color, *expected) {
			t.Fatalf("%s\ncolor at %q = %#v, want %#v", input, selection, actual.Color, *expected)
		}
		return
	}
	if expected != nil {
		t.Fatalf("%s\nno color at %q in %#v", input, selection, actualColors)
	}
}

func assertDocumentColorsForLanguage(t *testing.T, languageID string, input string, expected ...lsp.ColorInformation) {
	t.Helper()
	document := lsp.NewTextDocument(lsp.DocumentURI("test://test/test."+languageID), languageID, 0, input)
	actual := FindDocumentColors(document)
	if len(actual) == 0 && len(expected) == 0 {
		return
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("%s\nactual: %#v\nwant:   %#v", input, actual, expected)
	}
}

func colorPtr(color lsp.Color) *lsp.Color {
	return &color
}

func colorsApproximatelyEqual(actual, expected lsp.Color) bool {
	rDiff := math.Abs((actual.Red - expected.Red) * 255)
	gDiff := math.Abs((actual.Green - expected.Green) * 255)
	bDiff := math.Abs((actual.Blue - expected.Blue) * 255)
	aDiff := math.Abs((actual.Alpha - expected.Alpha) * 100)
	return rDiff < 1 && gDiff < 1 && bDiff < 1 && aDiff < 1
}

func assertColorPresentations(t *testing.T, color lsp.Color, expected []string) {
	t.Helper()
	r := offsetRange(1, 2)
	actual := GetColorPresentations(color, r)
	labels := make([]string, len(actual))
	edits := make([]*lsp.TextEdit, len(actual))
	for i, item := range actual {
		labels[i] = item.Label
		edits[i] = item.TextEdit
	}
	if !reflect.DeepEqual(labels, expected) {
		t.Fatalf("labels = %#v, want %#v", labels, expected)
	}
	for i, edit := range edits {
		if edit == nil || edit.Range != r || edit.NewText != expected[i] {
			t.Fatalf("edit[%d] = %#v", i, edit)
		}
	}
}
