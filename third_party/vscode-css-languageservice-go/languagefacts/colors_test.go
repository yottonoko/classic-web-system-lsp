package languagefacts

import (
	"math"
	"testing"

	"github.com/yottonoko/vscode-css-languageservice-go/lsp"
	"github.com/yottonoko/vscode-css-languageservice-go/parser"
)

func TestHexDigit(t *testing.T) {
	t.Run("hexDigit", func(t *testing.T) {
		input1 := "0123456789ABCDEF"
		input2 := "0123456789abcdef"
		for i, ch := range input1 {
			if got := HexDigit(ch); got != i {
				t.Fatalf("HexDigit(%q) = %d, want %d", ch, got, i)
			}
			if got := HexDigit([]rune(input2)[i]); got != i {
				t.Fatalf("HexDigit(%q) = %d, want %d", []rune(input2)[i], got, i)
			}
		}
	})
}

func TestColorFromHex(t *testing.T) {
	t.Run("colorFromHex", func(t *testing.T) {
		tests := []struct {
			name     string
			input    string
			expected *lsp.Color
		}{
			{name: "#000", input: "#000", expected: ptr(ColorFrom256RGB(0, 0, 0))},
			{name: "#fff", input: "#fff", expected: ptr(ColorFrom256RGB(255, 255, 255))},
			{name: "#15a", input: "#15a", expected: ptr(ColorFrom256RGB(0x11, 0x55, 0xaa))},
			{name: "#09f", input: "#09f", expected: ptr(ColorFrom256RGB(0x00, 0x99, 0xff))},
			{name: "#ABC", input: "#ABC", expected: ptr(ColorFrom256RGB(0xaa, 0xbb, 0xcc))},
			{name: "#DEF", input: "#DEF", expected: ptr(ColorFrom256RGB(0xdd, 0xee, 0xff))},
			{name: "#96af", input: "#96af", expected: ptr(ColorFrom256RGB(0x99, 0x66, 0xaa, 1))},
			{name: "#90AF", input: "#90AF", expected: ptr(ColorFrom256RGB(0x99, 0x00, 0xaa, 1))},
			{name: "#96a3", input: "#96a3", expected: ptr(ColorFrom256RGB(0x99, 0x66, 0xaa, float64(0x33)/255))},
			{name: "#132435", input: "#132435", expected: ptr(ColorFrom256RGB(0x13, 0x24, 0x35))},
			{name: "#cafebabe", input: "#cafebabe", expected: ptr(ColorFrom256RGB(0xca, 0xfe, 0xba, float64(0xbe)/255))},
			{name: "without hash", input: "123"},
			{name: "#12Y", input: "#12Y", expected: ptr(ColorFrom256RGB(0x11, 0x22, 0x00))},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assertColor(t, ColorFromHex(tt.input), tt.expected, tt.input)
			})
		}
	})
}

func TestColorValueNodes(t *testing.T) {
	t.Run("isColorString", func(t *testing.T) {
		for _, input := range []string{"#fff", "red", "slateblue", "transparent", "currentColor"} {
			if !IsColorString(input) {
				t.Fatalf("IsColorString(%q) = false", input)
			}
		}
		if IsColorString("not-a-color") {
			t.Fatalf("IsColorString(non-color) = true")
		}
	})

	assertColorNode(t, "#main { color: #231 }", "#231", true, ptr(ColorFrom256RGB(0x22, 0x33, 0x11)))
	assertColorNode(t, "#main { color: rgb(34, 89, 234) }", "rgb", true, ptr(ColorFrom256RGB(34, 89, 234)))
	assertColorNode(t, "#main { color: hsl(120deg, 100%, 50%) }", "hsl", true, ptr(ColorFrom256RGB(0, 255, 0)))
	assertColorNode(t, "#main { color: slateblue }", "slateblue", true, ptr(ColorFrom256RGB(106, 90, 205)))
	assertColorNode(t, "#main { color: none }", "none", false, nil)
	assertColorNode(t, "#main { color: rgba(0.7) }", "rgba", true, nil)
	assertColorNodeForLanguage(t, "scss", "#main { color: rgba($input-border, 0.7) }", "rgba", true, nil)
}

func TestHSLAndHWB(t *testing.T) {
	hslTests := []struct {
		name     string
		color    lsp.Color
		expected HSLA
	}{
		{name: "transparent black", color: ColorFrom256RGB(0, 0, 0, 0), expected: HSLA{H: 0, S: 0, L: 0, A: 0}},
		{name: "black", color: ColorFrom256RGB(0, 0, 0, 1), expected: HSLA{H: 0, S: 0, L: 0, A: 1}},
		{name: "white", color: ColorFrom256RGB(255, 255, 255, 1), expected: HSLA{H: 0, S: 0, L: 1, A: 1}},
		{name: "red", color: ColorFrom256RGB(255, 0, 0, 1), expected: HSLA{H: 0, S: 1, L: 0.5, A: 1}},
		{name: "lime", color: ColorFrom256RGB(0, 255, 0, 1), expected: HSLA{H: 120, S: 1, L: 0.5, A: 1}},
		{name: "blue", color: ColorFrom256RGB(0, 0, 255, 1), expected: HSLA{H: 240, S: 1, L: 0.5, A: 1}},
		{name: "yellow", color: ColorFrom256RGB(255, 255, 0, 1), expected: HSLA{H: 60, S: 1, L: 0.5, A: 1}},
		{name: "cyan", color: ColorFrom256RGB(0, 255, 255, 1), expected: HSLA{H: 180, S: 1, L: 0.5, A: 1}},
		{name: "magenta", color: ColorFrom256RGB(255, 0, 255, 1), expected: HSLA{H: 300, S: 1, L: 0.5, A: 1}},
		{name: "silver", color: ColorFrom256RGB(192, 192, 192, 1), expected: HSLA{H: 0, S: 0, L: 0.753, A: 1}},
		{name: "gray", color: ColorFrom256RGB(128, 128, 128, 1), expected: HSLA{H: 0, S: 0, L: 0.502, A: 1}},
		{name: "maroon", color: ColorFrom256RGB(128, 0, 0, 1), expected: HSLA{H: 0, S: 1, L: 0.251, A: 1}},
		{name: "olive", color: ColorFrom256RGB(128, 128, 0, 1), expected: HSLA{H: 60, S: 1, L: 0.251, A: 1}},
		{name: "green", color: ColorFrom256RGB(0, 128, 0, 1), expected: HSLA{H: 120, S: 1, L: 0.251, A: 1}},
		{name: "purple", color: ColorFrom256RGB(128, 0, 128, 1), expected: HSLA{H: 300, S: 1, L: 0.251, A: 1}},
		{name: "teal", color: ColorFrom256RGB(0, 128, 128, 1), expected: HSLA{H: 180, S: 1, L: 0.251, A: 1}},
		{name: "navy", color: ColorFrom256RGB(0, 0, 128, 1), expected: HSLA{H: 240, S: 1, L: 0.251, A: 1}},
		{name: "random blue", color: ColorFrom256RGB(0, 195, 255), expected: HSLA{H: 194, S: 1, L: 0.5, A: 1}},
		{name: "random gray blue", color: ColorFrom256RGB(40, 50, 60), expected: HSLA{H: 210, S: 0.2, L: 0.2, A: 1}},
		{name: "random green", color: ColorFrom256RGB(40, 255, 60), expected: HSLA{H: 126, S: 1.0, L: 0.58, A: 1}},
		{name: "random orange", color: ColorFrom256RGB(231, 135, 19), expected: HSLA{H: 33, S: 0.85, L: 0.49, A: 1}},
	}
	t.Run("hslFromColor", func(t *testing.T) {
		for _, tt := range hslTests {
			t.Run(tt.name, func(t *testing.T) {
				assertHSL(t, HSLFromColor(tt.color), tt.expected)
			})
		}
	})

	hwbTests := []struct {
		name     string
		color    lsp.Color
		expected HWBA
	}{
		{name: "transparent black", color: ColorFrom256RGB(0, 0, 0, 0), expected: HWBA{H: 0, W: 0, B: 1, A: 0}},
		{name: "black", color: ColorFrom256RGB(0, 0, 0, 1), expected: HWBA{H: 0, W: 0, B: 1, A: 1}},
		{name: "white", color: ColorFrom256RGB(255, 255, 255, 1), expected: HWBA{H: 0, W: 1, B: 0, A: 1}},
		{name: "red", color: ColorFrom256RGB(255, 0, 0, 1), expected: HWBA{H: 0, W: 0, B: 0, A: 1}},
		{name: "lime", color: ColorFrom256RGB(0, 255, 0, 1), expected: HWBA{H: 120, W: 0, B: 0, A: 1}},
		{name: "blue", color: ColorFrom256RGB(0, 0, 255, 1), expected: HWBA{H: 240, W: 0, B: 0, A: 1}},
		{name: "yellow", color: ColorFrom256RGB(255, 255, 0, 1), expected: HWBA{H: 60, W: 0, B: 0, A: 1}},
		{name: "cyan", color: ColorFrom256RGB(0, 255, 255, 1), expected: HWBA{H: 180, W: 0, B: 0, A: 1}},
		{name: "magenta", color: ColorFrom256RGB(255, 0, 255, 1), expected: HWBA{H: 300, W: 0, B: 0, A: 1}},
		{name: "silver", color: ColorFrom256RGB(192, 192, 192, 1), expected: HWBA{H: 0, W: 0.752, B: 0.247, A: 1}},
		{name: "gray", color: ColorFrom256RGB(128, 128, 128, 1), expected: HWBA{H: 0, W: 0.5, B: 0.5, A: 1}},
		{name: "maroon", color: ColorFrom256RGB(128, 0, 0, 1), expected: HWBA{H: 0, W: 0, B: 0.5, A: 1}},
		{name: "olive", color: ColorFrom256RGB(128, 128, 0, 1), expected: HWBA{H: 60, W: 0, B: 0.5, A: 1}},
		{name: "green", color: ColorFrom256RGB(0, 128, 0, 1), expected: HWBA{H: 120, W: 0, B: 0.5, A: 1}},
		{name: "purple", color: ColorFrom256RGB(128, 0, 128, 1), expected: HWBA{H: 300, W: 0, B: 0.5, A: 1}},
		{name: "teal", color: ColorFrom256RGB(0, 128, 128, 1), expected: HWBA{H: 180, W: 0, B: 0.5, A: 1}},
		{name: "navy", color: ColorFrom256RGB(0, 0, 128, 1), expected: HWBA{H: 240, W: 0, B: 0.5, A: 1}},
		{name: "random blue", color: ColorFrom256RGB(0, 195, 255), expected: HWBA{H: 194, W: 0, B: 0, A: 1}},
		{name: "random gray blue", color: ColorFrom256RGB(40, 50, 60), expected: HWBA{H: 210, W: 0.16, B: 0.76, A: 1}},
		{name: "random green", color: ColorFrom256RGB(40, 255, 60), expected: HWBA{H: 126, W: 0.16, B: 0, A: 1}},
		{name: "random orange", color: ColorFrom256RGB(231, 135, 19), expected: HWBA{H: 33, W: 0.07, B: 0.09, A: 1}},
	}
	t.Run("hwbFromColor", func(t *testing.T) {
		for _, tt := range hwbTests {
			t.Run(tt.name, func(t *testing.T) {
				assertHWB(t, HWBFromColor(tt.color), tt.expected)
			})
		}
	})

	hwbToColorTests := []struct {
		name     string
		actual   lsp.Color
		expected lsp.Color
	}{
		{name: "gray", actual: ColorFromHWB(0, 0.5, 0.5), expected: ColorFrom256RGB(128, 128, 128)},
		{name: "random red", actual: ColorFromHWB(350, 0.09, 0.5), expected: ColorFrom256RGB(128, 23, 41)},
		{name: "random green", actual: ColorFromHWB(118, 0.02, 0.01), expected: ColorFrom256RGB(13, 253, 5)},
		{name: "pale green", actual: ColorFromHWB(120, 0.92, 0.01), expected: ColorFrom256RGB(235, 252, 235)},
	}
	t.Run("hwbToColor", func(t *testing.T) {
		for _, tt := range hwbToColorTests {
			t.Run(tt.name, func(t *testing.T) {
				assertColor(t, ptr(tt.actual), ptr(tt.expected), tt.name)
			})
		}
	})

	hslToColorTests := []struct {
		name     string
		actual   lsp.Color
		expected lsp.Color
	}{
		{name: "gray", actual: ColorFromHSL(0, 0, 0.5), expected: ColorFrom256RGB(128, 128, 128)},
		{name: "random red", actual: ColorFromHSL(350, 0.7, 0.3), expected: ColorFrom256RGB(130, 23, 41)},
		{name: "green", actual: ColorFromHSL(118, 0.98, 0.5), expected: ColorFrom256RGB(11, 252, 3)},
		{name: "pale green", actual: ColorFromHSL(120, 0.83, 0.95), expected: ColorFrom256RGB(232, 253, 232)},
	}
	t.Run("hslToColor", func(t *testing.T) {
		for _, tt := range hslToColorTests {
			t.Run(tt.name, func(t *testing.T) {
				assertColor(t, ptr(tt.actual), ptr(tt.expected), tt.name)
			})
		}
	})
}

func TestLabConversions(t *testing.T) {
	t.Run("xyzFromLAB", func(t *testing.T) {
		tests := []struct {
			name     string
			input    LAB
			expected XYZ
		}{
			{name: "green", input: LAB{L: 46.41, A: -39.24, B: 33.51}, expected: XYZ{X: 9.22, Y: 15.58, Z: 5.54, Alpha: 1}},
			{name: "bright green", input: LAB{L: 50, A: -50, B: 50}, expected: XYZ{X: 9.8, Y: 18.42, Z: 3.53, Alpha: 1}},
			{name: "light magenta", input: LAB{L: 90, A: 50, B: -50}, expected: XYZ{X: 99.03, Y: 76.3, Z: 171.63, Alpha: 1}},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assertXYZ(t, XYZFromLAB(tt.input), tt.expected)
			})
		}
	})

	t.Run("xyzFromOKLAB", func(t *testing.T) {
		tests := []struct {
			name     string
			input    LAB
			expected XYZ
		}{
			{name: "green", input: LAB{L: 0.52, A: -0.11, B: 0.08}, expected: XYZ{X: 8.8, Y: 15.19, Z: 5.24, Alpha: 1}},
			{name: "bright green", input: LAB{L: 0.55, A: -0.14, B: 0.11}, expected: XYZ{X: 9.49, Y: 18.2, Z: 3.42, Alpha: 1}},
			{name: "light magenta", input: LAB{L: 0.86, A: 0.08, B: -0.01}, expected: XYZ{X: 70.1, Y: 61.33, Z: 71.52, Alpha: 1}},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assertXYZ(t, XYZFromOKLAB(tt.input), tt.expected)
			})
		}
	})

	t.Run("xyzToRGB", func(t *testing.T) {
		tests := []struct {
			name     string
			input    XYZ
			expected lsp.Color
		}{
			{name: "green", input: XYZ{X: 9.22, Y: 15.58, Z: 5.54, Alpha: 1}, expected: lsp.Color{Red: 50, Green: 125, Blue: 50, Alpha: 1}},
			{name: "bright green", input: XYZ{X: 9.8, Y: 18.42, Z: 3.53, Alpha: 1}, expected: lsp.Color{Red: 35, Green: 137, Blue: 16, Alpha: 1}},
			{name: "light magenta", input: XYZ{X: 99, Y: 76, Z: 71, Alpha: 1}, expected: lsp.Color{Red: 255, Green: 187, Blue: 211, Alpha: 1}},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assertColor(t, ptr(XYZToRGB(tt.input)), ptr(tt.expected), tt.name)
			})
		}
	})

	t.Run("XYZtoOKLAB", func(t *testing.T) {
		tests := []struct {
			name     string
			input    XYZ
			expected LAB
		}{
			{name: "green", input: XYZ{X: 8.8, Y: 15.19, Z: 5.24, Alpha: 1}, expected: LAB{L: 0.52, A: -0.11009, B: 0.07998, Alpha: 1}},
			{name: "bright green", input: XYZ{X: 9.49, Y: 18.2, Z: 3.42, Alpha: 1}, expected: LAB{L: 0.54998, A: -0.14, B: 0.10994, Alpha: 1}},
			{name: "light magenta", input: XYZ{X: 70.1, Y: 61.33, Z: 71.52, Alpha: 1}, expected: LAB{L: 0.86001, A: 0.07999, B: -0.01011, Alpha: 1}},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assertLAB(t, XYZToOKLAB(tt.input), tt.expected)
			})
		}
	})

	t.Run("LABToRGB", func(t *testing.T) {
		assertColor(t, ptr(ColorFromLAB(46.41, -39.24, 33.51)), ptr(ColorFrom256RGB(50, 125, 50)), "lab")
	})
	t.Run("OKLABToRGB", func(t *testing.T) {
		assertColor(t, ptr(ColorFromOKLAB(0.86, 0.08, -0.01)), ptr(ColorFrom256RGB(251.88, 187.65, 213.63)), "oklab")
	})
	t.Run("labFromLCH", func(t *testing.T) {
		assertLAB(t, LABFromLCH(46.41, 51.60, 139.50), LAB{L: 46.41, A: -39.24, B: 33.51, Alpha: 1})
	})
	t.Run("LCHtoRGB", func(t *testing.T) {
		assertColor(t, ptr(ColorFromLCH(46.41, 51.60, 139.50)), ptr(ColorFrom256RGB(50, 125, 50)), "lch")
	})
	t.Run("OKLCHtoRGB", func(t *testing.T) {
		assertColor(t, ColorFromOKLCH(0.52, 0.13, 143.39), ptr(ColorFrom256RGB(50.32, 123.2, 50.17)), "oklch")
	})
	t.Run("labFromColor", func(t *testing.T) {
		assertLAB(t, LABFromColor(ColorFrom256RGB(50, 125, 50)), LAB{L: 46.41, A: -39.24, B: 33.51, Alpha: 1})
	})
	t.Run("oklabFromColor", func(t *testing.T) {
		assertLAB(t, OKLABFromColor(ColorFrom256RGB(50, 125, 50)), LAB{L: 52.488, A: -0.10665, B: 0.07916, Alpha: 1})
	})
	t.Run("RGBToXYZ", func(t *testing.T) {
		assertXYZ(t, RGBToXYZ(ColorFrom256RGB(50, 125, 50)), XYZ{X: 9.22, Y: 15.58, Z: 5.54, Alpha: 1})
	})
	t.Run("RGBToLCH", func(t *testing.T) {
		assertLCH(t, LCHFromColor(ColorFrom256RGB(50, 125, 50)), LCH{L: 46.41, C: 51.60, H: 139.50})
	})
}

func assertColor(t *testing.T, actual, expected *lsp.Color, message string) {
	t.Helper()
	if actual == nil || expected == nil {
		if actual != expected {
			t.Fatalf("%s = %#v, want %#v", message, actual, expected)
		}
		return
	}
	if math.Abs((actual.Red-expected.Red)*255) < 1 &&
		math.Abs((actual.Green-expected.Green)*255) < 1 &&
		math.Abs((actual.Blue-expected.Blue)*255) < 1 &&
		math.Abs((actual.Alpha-expected.Alpha)*100) < 1 {
		return
	}
	t.Fatalf("%s = %#v, want %#v", message, *actual, *expected)
}

func assertColorNode(t *testing.T, input, marker string, wantColorValue bool, want *lsp.Color) {
	t.Helper()
	assertColorNodeForLanguage(t, "css", input, marker, wantColorValue, want)
}

func assertColorNodeForLanguage(t *testing.T, languageID, input, marker string, wantColorValue bool, want *lsp.Color) {
	t.Helper()
	offset := stringsIndex(input, marker)
	var p *parser.Parser
	switch languageID {
	case "scss":
		p = parser.NewSCSSParser()
	case "less":
		p = parser.NewLESSParser()
	default:
		p = parser.NewParser()
	}
	root := p.ParseStylesheet(input)
	node := colorNodeAt(root, offset)
	if node == nil {
		t.Fatalf("no color node at %q in %q", marker, input)
	}
	if got := IsColorValue(node); got != wantColorValue {
		t.Fatalf("IsColorValue(%s) = %v, want %v", node.GetText(), got, wantColorValue)
	}
	assertColor(t, GetColorValue(node), want, node.GetText())
}

func colorNodeAt(root *parser.Node, offset int) *parser.Node {
	path := parser.GetNodePath(root, offset)
	var candidate *parser.Node
	for i := len(path) - 1; i >= 0; i-- {
		switch path[i].Type() {
		case parser.NodeTypeFunction:
			return path[i]
		case parser.NodeTypeHexColorValue, parser.NodeTypeIdentifier:
			if candidate == nil {
				candidate = path[i]
			}
		}
	}
	return candidate
}

func stringsIndex(text, needle string) int {
	for i := 0; i+len(needle) <= len(text); i++ {
		if text[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

func assertHSL(t *testing.T, actual, expected HSLA) {
	t.Helper()
	if math.Abs(actual.H-expected.H) < 1 &&
		math.Abs((actual.S-expected.S)*100) < 1 &&
		math.Abs((actual.L-expected.L)*100) < 1 &&
		math.Abs((actual.A-expected.A)*100) < 1 {
		return
	}
	t.Fatalf("HSL = %#v, want %#v", actual, expected)
}

func assertHWB(t *testing.T, actual, expected HWBA) {
	t.Helper()
	if math.Abs(actual.H-expected.H) < 1 &&
		math.Abs((actual.W-expected.W)*100) < 1 &&
		math.Abs((actual.B-expected.B)*100) < 1 &&
		math.Abs((actual.A-expected.A)*100) < 1 {
		return
	}
	t.Fatalf("HWB = %#v, want %#v", actual, expected)
}

func assertXYZ(t *testing.T, actual, expected XYZ) {
	t.Helper()
	if math.Abs(actual.X-expected.X) < 1 &&
		math.Abs(actual.Y-expected.Y) < 1 &&
		math.Abs(actual.Z-expected.Z) < 1 &&
		math.Abs((actual.Alpha-expected.Alpha)*100) < 1 {
		return
	}
	t.Fatalf("XYZ = %#v, want %#v", actual, expected)
}

func assertLAB(t *testing.T, actual, expected LAB) {
	t.Helper()
	if math.Abs(actual.L-expected.L) < 1 &&
		math.Abs(actual.A-expected.A) < 1 &&
		math.Abs(actual.B-expected.B) < 1 &&
		math.Abs((actual.Alpha-expected.Alpha)*100) < 1 {
		return
	}
	t.Fatalf("LAB = %#v, want %#v", actual, expected)
}

func assertLCH(t *testing.T, actual, expected LCH) {
	t.Helper()
	if math.Abs(actual.L-expected.L) < 1 &&
		math.Abs(actual.C-expected.C) < 1 &&
		math.Abs(actual.H-expected.H) < 1 {
		return
	}
	t.Fatalf("LCH = %#v, want %#v", actual, expected)
}

func ptr[T any](value T) *T {
	return &value
}
