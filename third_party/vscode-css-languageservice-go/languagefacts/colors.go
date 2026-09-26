package languagefacts

import (
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/yottonoko/vscode-css-languageservice-go/lsp"
	"github.com/yottonoko/vscode-css-languageservice-go/parser"
)

var cssColorComponentPattern = regexp.MustCompile(`(?i)none|[-+]?(?:\d+(?:\.\d*)?|\.\d+)(?:%|deg|grad|rad|turn)?`)
var cssColorFunctionNamePattern = regexp.MustCompile(`(?i)^(?:rgba?|hsla?|hwb|lab|lch|oklab|oklch)$`)

var cssNamedColors = map[string]string{
	"aliceblue":            "#f0f8ff",
	"antiquewhite":         "#faebd7",
	"aqua":                 "#00ffff",
	"aquamarine":           "#7fffd4",
	"azure":                "#f0ffff",
	"beige":                "#f5f5dc",
	"bisque":               "#ffe4c4",
	"black":                "#000000",
	"blanchedalmond":       "#ffebcd",
	"blue":                 "#0000ff",
	"blueviolet":           "#8a2be2",
	"brown":                "#a52a2a",
	"burlywood":            "#deb887",
	"cadetblue":            "#5f9ea0",
	"chartreuse":           "#7fff00",
	"chocolate":            "#d2691e",
	"coral":                "#ff7f50",
	"cornflowerblue":       "#6495ed",
	"cornsilk":             "#fff8dc",
	"crimson":              "#dc143c",
	"cyan":                 "#00ffff",
	"darkblue":             "#00008b",
	"darkcyan":             "#008b8b",
	"darkgoldenrod":        "#b8860b",
	"darkgray":             "#a9a9a9",
	"darkgrey":             "#a9a9a9",
	"darkgreen":            "#006400",
	"darkkhaki":            "#bdb76b",
	"darkmagenta":          "#8b008b",
	"darkolivegreen":       "#556b2f",
	"darkorange":           "#ff8c00",
	"darkorchid":           "#9932cc",
	"darkred":              "#8b0000",
	"darksalmon":           "#e9967a",
	"darkseagreen":         "#8fbc8f",
	"darkslateblue":        "#483d8b",
	"darkslategray":        "#2f4f4f",
	"darkslategrey":        "#2f4f4f",
	"darkturquoise":        "#00ced1",
	"darkviolet":           "#9400d3",
	"deeppink":             "#ff1493",
	"deepskyblue":          "#00bfff",
	"dimgray":              "#696969",
	"dimgrey":              "#696969",
	"dodgerblue":           "#1e90ff",
	"firebrick":            "#b22222",
	"floralwhite":          "#fffaf0",
	"forestgreen":          "#228b22",
	"fuchsia":              "#ff00ff",
	"gainsboro":            "#dcdcdc",
	"ghostwhite":           "#f8f8ff",
	"gold":                 "#ffd700",
	"goldenrod":            "#daa520",
	"gray":                 "#808080",
	"grey":                 "#808080",
	"green":                "#008000",
	"greenyellow":          "#adff2f",
	"honeydew":             "#f0fff0",
	"hotpink":              "#ff69b4",
	"indianred":            "#cd5c5c",
	"indigo":               "#4b0082",
	"ivory":                "#fffff0",
	"khaki":                "#f0e68c",
	"lavender":             "#e6e6fa",
	"lavenderblush":        "#fff0f5",
	"lawngreen":            "#7cfc00",
	"lemonchiffon":         "#fffacd",
	"lightblue":            "#add8e6",
	"lightcoral":           "#f08080",
	"lightcyan":            "#e0ffff",
	"lightgoldenrodyellow": "#fafad2",
	"lightgray":            "#d3d3d3",
	"lightgrey":            "#d3d3d3",
	"lightgreen":           "#90ee90",
	"lightpink":            "#ffb6c1",
	"lightsalmon":          "#ffa07a",
	"lightseagreen":        "#20b2aa",
	"lightskyblue":         "#87cefa",
	"lightslategray":       "#778899",
	"lightslategrey":       "#778899",
	"lightsteelblue":       "#b0c4de",
	"lightyellow":          "#ffffe0",
	"lime":                 "#00ff00",
	"limegreen":            "#32cd32",
	"linen":                "#faf0e6",
	"magenta":              "#ff00ff",
	"maroon":               "#800000",
	"mediumaquamarine":     "#66cdaa",
	"mediumblue":           "#0000cd",
	"mediumorchid":         "#ba55d3",
	"mediumpurple":         "#9370d8",
	"mediumseagreen":       "#3cb371",
	"mediumslateblue":      "#7b68ee",
	"mediumspringgreen":    "#00fa9a",
	"mediumturquoise":      "#48d1cc",
	"mediumvioletred":      "#c71585",
	"midnightblue":         "#191970",
	"mintcream":            "#f5fffa",
	"mistyrose":            "#ffe4e1",
	"moccasin":             "#ffe4b5",
	"navajowhite":          "#ffdead",
	"navy":                 "#000080",
	"oldlace":              "#fdf5e6",
	"olive":                "#808000",
	"olivedrab":            "#6b8e23",
	"orange":               "#ffa500",
	"orangered":            "#ff4500",
	"orchid":               "#da70d6",
	"palegoldenrod":        "#eee8aa",
	"palegreen":            "#98fb98",
	"paleturquoise":        "#afeeee",
	"palevioletred":        "#d87093",
	"papayawhip":           "#ffefd5",
	"peachpuff":            "#ffdab9",
	"peru":                 "#cd853f",
	"pink":                 "#ffc0cb",
	"plum":                 "#dda0dd",
	"powderblue":           "#b0e0e6",
	"purple":               "#800080",
	"red":                  "#ff0000",
	"rebeccapurple":        "#663399",
	"rosybrown":            "#bc8f8f",
	"royalblue":            "#4169e1",
	"saddlebrown":          "#8b4513",
	"salmon":               "#fa8072",
	"sandybrown":           "#f4a460",
	"seagreen":             "#2e8b57",
	"seashell":             "#fff5ee",
	"sienna":               "#a0522d",
	"silver":               "#c0c0c0",
	"skyblue":              "#87ceeb",
	"slateblue":            "#6a5acd",
	"slategray":            "#708090",
	"slategrey":            "#708090",
	"snow":                 "#fffafa",
	"springgreen":          "#00ff7f",
	"steelblue":            "#4682b4",
	"tan":                  "#d2b48c",
	"teal":                 "#008080",
	"thistle":              "#d8bfd8",
	"tomato":               "#ff6347",
	"turquoise":            "#40e0d0",
	"violet":               "#ee82ee",
	"wheat":                "#f5deb3",
	"white":                "#ffffff",
	"whitesmoke":           "#f5f5f5",
	"yellow":               "#ffff00",
	"yellowgreen":          "#9acd32",
}

// HSLA represents a color in hue, saturation, lightness, and alpha form.
type HSLA struct {
	H float64
	S float64
	L float64
	A float64
}

// HWBA represents a color in hue, whiteness, blackness, and alpha form.
type HWBA struct {
	H float64
	W float64
	B float64
	A float64
}

// XYZ represents a CIE XYZ color with alpha.
type XYZ struct {
	X     float64
	Y     float64
	Z     float64
	Alpha float64
}

// LAB represents a CIE Lab or OKLab color with alpha.
type LAB struct {
	L     float64
	A     float64
	B     float64
	Alpha float64
}

// LCH represents a Lab-derived lightness, chroma, hue color.
type LCH struct {
	L     float64
	C     float64
	H     float64
	Alpha float64
}

// IsColorString reports whether text is a CSS color literal or color keyword.
func IsColorString(text string) bool {
	lower := strings.ToLower(text)
	if ColorFromHex(text) != nil {
		return true
	}
	if _, ok := cssNamedColors[lower]; ok {
		return true
	}
	return lower == "currentcolor" || lower == "transparent"
}

// IsColorValue reports whether node represents a color value in the parsed CSS tree.
func IsColorValue(node *parser.Node) bool {
	if node == nil {
		return false
	}
	switch node.Type() {
	case parser.NodeTypeHexColorValue:
		return true
	case parser.NodeTypeFunction:
		name, _, ok := colorFunctionParts(node.GetText())
		return ok && cssColorFunctionNamePattern.MatchString(name)
	case parser.NodeTypeIdentifier:
		if parent := node.GetParent(); parent != nil && parent.Type() != parser.NodeTypeTerm {
			return false
		}
		candidate := strings.ToLower(node.GetText())
		if candidate == "none" {
			return false
		}
		_, ok := cssNamedColors[candidate]
		return ok
	default:
		return false
	}
}

// GetColorValue resolves node to a concrete color when all color components are static.
func GetColorValue(node *parser.Node) *lsp.Color {
	if node == nil {
		return nil
	}
	switch node.Type() {
	case parser.NodeTypeHexColorValue:
		return ColorFromHex(node.GetText())
	case parser.NodeTypeFunction:
		name, args, ok := colorFunctionParts(node.GetText())
		if !ok || !cssColorFunctionNamePattern.MatchString(name) || strings.Contains(args, "$") {
			return nil
		}
		color, ok := colorFromFunction(name, args)
		if !ok {
			return nil
		}
		return &color
	case parser.NodeTypeIdentifier:
		if parent := node.GetParent(); parent != nil && parent.Type() != parser.NodeTypeTerm {
			return nil
		}
		color, ok := ColorFromName(node.GetText())
		if !ok {
			return nil
		}
		return color
	default:
		return nil
	}
}

// ColorFromName resolves a named CSS color from the upstream color table.
func ColorFromName(name string) (*lsp.Color, bool) {
	hex, ok := cssNamedColors[strings.ToLower(name)]
	if !ok {
		return nil, false
	}
	color := ColorFromHex(hex)
	return color, color != nil
}

func colorFunctionParts(text string) (string, string, bool) {
	open := strings.IndexRune(text, '(')
	if open <= 0 || !strings.HasSuffix(strings.TrimSpace(text), ")") {
		return "", "", false
	}
	close := strings.LastIndex(text, ")")
	if close < open {
		return "", "", false
	}
	return strings.ToLower(strings.TrimSpace(text[:open])), text[open+1 : close], true
}

func colorFromFunction(name, args string) (lsp.Color, bool) {
	values := parseCSSColorComponents(args)
	switch name {
	case "rgb", "rgba":
		if len(values) < 3 {
			return lsp.Color{}, false
		}
		alpha := 1.0
		if len(values) >= 4 {
			alpha = alphaComponent(values[3])
		}
		return ColorFrom256RGB(rgbComponent(values[0]), rgbComponent(values[1]), rgbComponent(values[2]), alpha), true
	case "hsl", "hsla":
		if len(values) < 3 {
			return lsp.Color{}, false
		}
		alpha := 1.0
		if len(values) >= 4 {
			alpha = alphaComponent(values[3])
		}
		return ColorFromHSL(angleComponent(values[0]), percentComponent(values[1]), percentComponent(values[2]), alpha), true
	case "hwb":
		if len(values) < 3 {
			return lsp.Color{}, false
		}
		alpha := 1.0
		if len(values) >= 4 {
			alpha = alphaComponent(values[3])
		}
		return ColorFromHWB(angleComponent(values[0]), percentComponent(values[1]), percentComponent(values[2]), alpha), true
	case "lab":
		if len(values) < 3 {
			return lsp.Color{}, false
		}
		alpha := 1.0
		if len(values) >= 4 {
			alpha = alphaComponent(values[3])
		}
		return ColorFromLAB(labLightnessComponent(values[0]), values[1].value, values[2].value, alpha), true
	case "lch":
		if len(values) < 3 {
			return lsp.Color{}, false
		}
		alpha := 1.0
		if len(values) >= 4 {
			alpha = alphaComponent(values[3])
		}
		return ColorFromLCH(labLightnessComponent(values[0]), values[1].value, angleComponent(values[2]), alpha), true
	case "oklab":
		if len(values) < 3 {
			return lsp.Color{}, false
		}
		alpha := 1.0
		if len(values) >= 4 {
			alpha = alphaComponent(values[3])
		}
		return ColorFromOKLAB(okLightnessComponent(values[0]), okABComponent(values[1]), okABComponent(values[2]), alpha), true
	case "oklch":
		if len(values) < 3 {
			return lsp.Color{}, false
		}
		alpha := 1.0
		if len(values) >= 4 {
			alpha = alphaComponent(values[3])
		}
		color := ColorFromOKLCH(okLightnessComponent(values[0]), okChromaComponent(values[1]), angleComponent(values[2]), alpha)
		if color == nil {
			return lsp.Color{}, false
		}
		return *color, true
	default:
		return lsp.Color{}, false
	}
}

type cssColorComponent struct {
	value float64
	unit  string
	none  bool
}

func parseCSSColorComponents(text string) []cssColorComponent {
	matches := cssColorComponentPattern.FindAllString(text, -1)
	values := make([]cssColorComponent, 0, len(matches))
	for _, match := range matches {
		lower := strings.ToLower(match)
		if lower == "none" {
			values = append(values, cssColorComponent{none: true})
			continue
		}
		unit := ""
		for _, candidate := range []string{"turn", "grad", "rad", "deg", "%"} {
			if strings.HasSuffix(lower, candidate) {
				unit = candidate
				match = match[:len(match)-len(candidate)]
				break
			}
		}
		value, err := strconv.ParseFloat(match, 64)
		if err == nil {
			values = append(values, cssColorComponent{value: value, unit: unit})
		}
	}
	return values
}

func rgbComponent(component cssColorComponent) float64 {
	if component.unit == "%" {
		return component.value * 255 / 100
	}
	return component.value
}

func percentComponent(component cssColorComponent) float64 {
	return component.value / 100
}

func alphaComponent(component cssColorComponent) float64 {
	if component.unit == "%" {
		return component.value / 100
	}
	return component.value
}

func angleComponent(component cssColorComponent) float64 {
	if component.none {
		return 0
	}
	switch component.unit {
	case "turn":
		return component.value * 360
	case "grad":
		return component.value * 0.9
	case "rad":
		return component.value * 180 / math.Pi
	default:
		return component.value
	}
}

func labLightnessComponent(component cssColorComponent) float64 {
	return component.value
}

func okLightnessComponent(component cssColorComponent) float64 {
	if component.unit == "%" {
		return component.value / 100
	}
	return component.value
}

func okABComponent(component cssColorComponent) float64 {
	if component.unit == "%" {
		return component.value * 0.004
	}
	return component.value
}

func okChromaComponent(component cssColorComponent) float64 {
	if component.unit == "%" {
		return component.value * 0.004
	}
	return component.value
}

func HexDigit(charCode rune) int {
	if charCode < '0' {
		return 0
	}
	if charCode <= '9' {
		return int(charCode - '0')
	}
	if charCode < 'a' {
		charCode += 'a' - 'A'
	}
	if charCode >= 'a' && charCode <= 'f' {
		return int(charCode-'a') + 10
	}
	return 0
}

func ColorFromHex(text string) *lsp.Color {
	runes := []rune(text)
	if len(runes) == 0 || runes[0] != '#' {
		return nil
	}
	switch len(runes) {
	case 4:
		return &lsp.Color{
			Red:   float64(HexDigit(runes[1])*0x11) / 255,
			Green: float64(HexDigit(runes[2])*0x11) / 255,
			Blue:  float64(HexDigit(runes[3])*0x11) / 255,
			Alpha: 1,
		}
	case 5:
		return &lsp.Color{
			Red:   float64(HexDigit(runes[1])*0x11) / 255,
			Green: float64(HexDigit(runes[2])*0x11) / 255,
			Blue:  float64(HexDigit(runes[3])*0x11) / 255,
			Alpha: float64(HexDigit(runes[4])*0x11) / 255,
		}
	case 7:
		return &lsp.Color{
			Red:   float64(HexDigit(runes[1])*0x10+HexDigit(runes[2])) / 255,
			Green: float64(HexDigit(runes[3])*0x10+HexDigit(runes[4])) / 255,
			Blue:  float64(HexDigit(runes[5])*0x10+HexDigit(runes[6])) / 255,
			Alpha: 1,
		}
	case 9:
		return &lsp.Color{
			Red:   float64(HexDigit(runes[1])*0x10+HexDigit(runes[2])) / 255,
			Green: float64(HexDigit(runes[3])*0x10+HexDigit(runes[4])) / 255,
			Blue:  float64(HexDigit(runes[5])*0x10+HexDigit(runes[6])) / 255,
			Alpha: float64(HexDigit(runes[7])*0x10+HexDigit(runes[8])) / 255,
		}
	default:
		return nil
	}
}

func ColorFrom256RGB(red, green, blue float64, alpha ...float64) lsp.Color {
	a := 1.0
	if len(alpha) > 0 {
		a = alpha[0]
	}
	return lsp.Color{Red: red / 255, Green: green / 255, Blue: blue / 255, Alpha: a}
}

func ColorFromHSL(hue, sat, light float64, alpha ...float64) lsp.Color {
	a := 1.0
	if len(alpha) > 0 {
		a = alpha[0]
	}
	hue = hue / 60
	if sat == 0 {
		return lsp.Color{Red: light, Green: light, Blue: light, Alpha: a}
	}
	hueToRGB := func(t1, t2, hue float64) float64 {
		for hue < 0 {
			hue += 6
		}
		for hue >= 6 {
			hue -= 6
		}
		if hue < 1 {
			return (t2-t1)*hue + t1
		}
		if hue < 3 {
			return t2
		}
		if hue < 4 {
			return (t2-t1)*(4-hue) + t1
		}
		return t1
	}
	t2 := light + sat - light*sat
	if light <= 0.5 {
		t2 = light * (sat + 1)
	}
	t1 := light*2 - t2
	return lsp.Color{
		Red:   hueToRGB(t1, t2, hue+2),
		Green: hueToRGB(t1, t2, hue),
		Blue:  hueToRGB(t1, t2, hue-2),
		Alpha: a,
	}
}

func HSLFromColor(rgba lsp.Color) HSLA {
	r, g, b, a := rgba.Red, rgba.Green, rgba.Blue, rgba.Alpha
	maxValue := math.Max(r, math.Max(g, b))
	minValue := math.Min(r, math.Min(g, b))
	h := 0.0
	s := 0.0
	l := (minValue + maxValue) / 2
	chroma := maxValue - minValue
	if chroma > 0 {
		if l <= 0.5 {
			s = chroma / (2 * l)
		} else {
			s = chroma / (2 - 2*l)
		}
		s = math.Min(s, 1)
		switch maxValue {
		case r:
			h = (g - b) / chroma
			if g < b {
				h += 6
			}
		case g:
			h = (b-r)/chroma + 2
		case b:
			h = (r-g)/chroma + 4
		}
		h = math.Round(h * 60)
	}
	return HSLA{H: h, S: s, L: l, A: a}
}

func ColorFromHWB(hue, white, black float64, alpha ...float64) lsp.Color {
	a := 1.0
	if len(alpha) > 0 {
		a = alpha[0]
	}
	if white+black >= 1 {
		gray := white / (white + black)
		return lsp.Color{Red: gray, Green: gray, Blue: gray, Alpha: a}
	}
	rgb := ColorFromHSL(hue, 1, 0.5, a)
	return lsp.Color{
		Red:   rgb.Red*(1-white-black) + white,
		Green: rgb.Green*(1-white-black) + white,
		Blue:  rgb.Blue*(1-white-black) + white,
		Alpha: a,
	}
}

func HWBFromColor(rgba lsp.Color) HWBA {
	hsl := HSLFromColor(rgba)
	white := math.Min(rgba.Red, math.Min(rgba.Green, rgba.Blue))
	black := 1 - math.Max(rgba.Red, math.Max(rgba.Green, rgba.Blue))
	return HWBA{H: hsl.H, W: white, B: black, A: hsl.A}
}

func XYZFromLAB(lab LAB) XYZ {
	alpha := lab.Alpha
	if alpha == 0 {
		alpha = 1
	}
	y := (lab.L + 16) / 116
	x := lab.A/500 + y
	z := y - lab.B/200
	convert := func(v float64) float64 {
		pow := v * v * v
		if pow > 0.008856 {
			return pow
		}
		return (v - 16.0/116.0) / 7.787
	}
	return XYZ{X: convert(x) * 95.047, Y: convert(y) * 100, Z: convert(z) * 108.883, Alpha: alpha}
}

func XYZFromOKLAB(lab LAB) XYZ {
	alpha := lab.Alpha
	if alpha == 0 {
		alpha = 1
	}
	l := lab.L + 0.3963377774*lab.A + 0.2158037573*lab.B
	m := lab.L - 0.1055613458*lab.A - 0.0638541728*lab.B
	s := lab.L - 0.0894841775*lab.A - 1.291485548*lab.B
	l3, m3, s3 := l*l*l, m*m*m, s*s*s
	return XYZ{
		X:     (1.2270138511*l3 - 0.5577999807*m3 + 0.281256149*s3) * 100,
		Y:     (-0.0405801784*l3 + 1.1122568696*m3 - 0.0716766787*s3) * 100,
		Z:     (-0.0763812845*l3 - 0.4214819784*m3 + 1.5861632204*s3) * 100,
		Alpha: alpha,
	}
}

func XYZToRGB(xyz XYZ) lsp.Color {
	x, y, z := xyz.X/100, xyz.Y/100, xyz.Z/100
	r := 3.2406254773200533*x - 1.5372079722103187*y - 0.4986285986982479*z
	g := -0.9689307147293197*x + 1.8757560608852415*y + 0.041517523842953964*z
	b := 0.055710120445510616*x + -0.2040210505984867*y + 1.0569959422543882*z
	compand := func(c float64) float64 {
		if c <= 0.0031308 {
			return 12.92 * c
		}
		return math.Min(1.055*math.Pow(c, 1/2.4)-0.055, 1)
	}
	return lsp.Color{Red: math.Round(compand(r) * 255), Green: math.Round(compand(g) * 255), Blue: math.Round(compand(b) * 255), Alpha: xyz.Alpha}
}

func RGBToXYZ(rgba lsp.Color) XYZ {
	r, g, b := rgba.Red, rgba.Green, rgba.Blue
	if r > 0.04045 {
		r = math.Pow((r+0.055)/1.055, 2.4)
	} else {
		r = r / 12.92
	}
	if g > 0.04045 {
		g = math.Pow((g+0.055)/1.055, 2.4)
	} else {
		g = g / 12.92
	}
	if b > 0.04045 {
		b = math.Pow((b+0.055)/1.055, 2.4)
	} else {
		b = b / 12.92
	}
	r, g, b = r*100, g*100, b*100
	return XYZ{
		X:     r*0.4124 + g*0.3576 + b*0.1805,
		Y:     r*0.2126 + g*0.7152 + b*0.0722,
		Z:     r*0.0193 + g*0.1192 + b*0.9505,
		Alpha: rgba.Alpha,
	}
}

func XYZToLAB(xyz XYZ, doRound ...bool) LAB {
	round := true
	if len(doRound) > 0 {
		round = doRound[0]
	}
	x, y, z := xyz.X/95.047, xyz.Y/100, xyz.Z/108.883
	convert := func(v float64) float64 {
		if v > 0.008856 {
			return math.Pow(v, 1.0/3.0)
		}
		return 7.787*v + 16.0/116.0
	}
	x, y, z = convert(x), convert(y), convert(z)
	l := 116*y - 16
	a := 500 * (x - y)
	b := 200 * (y - z)
	if round {
		l, a, b = roundN(l, 2), roundN(a, 2), roundN(b, 2)
	}
	return LAB{L: l, A: a, B: b, Alpha: xyz.Alpha}
}

func XYZToOKLAB(xyz XYZ, doRound ...bool) LAB {
	round := true
	if len(doRound) > 0 {
		round = doRound[0]
	}
	x, y, z := xyz.X/100, xyz.Y/100, xyz.Z/100
	l := 0.8189330101*x + 0.3618667424*y - 0.1288597137*z
	m := 0.0329845436*x + 0.9293118715*y + 0.0361456387*z
	s := 0.0482003018*x + 0.2643662691*y + 0.633851707*z
	l_, m_, s_ := math.Cbrt(l), math.Cbrt(m), math.Cbrt(s)
	L := 0.2104542553*l_ + 0.793617785*m_ - 0.0040720468*s_
	A := 1.9779984951*l_ - 2.428592205*m_ + 0.4505937099*s_
	B := 0.0259040371*l_ + 0.7827717662*m_ - 0.808675766*s_
	if round {
		L, A, B = roundN(L, 5), roundN(A, 5), roundN(B, 5)
	}
	return LAB{L: L, A: A, B: B, Alpha: xyz.Alpha}
}

func LABFromColor(rgba lsp.Color, doRound ...bool) LAB {
	return XYZToLAB(RGBToXYZ(rgba), doRound...)
}

func OKLABFromColor(rgba lsp.Color, doRound ...bool) LAB {
	lab := XYZToOKLAB(RGBToXYZ(rgba), doRound...)
	lab.L *= 100
	return lab
}

func LCHFromColor(rgba lsp.Color) LCH {
	lch := labToLCH(LABFromColor(rgba, false))
	return LCH{L: roundN(lch.L, 2), C: roundN(lch.C, 2), H: roundN(lch.H, 2), Alpha: lch.Alpha}
}

func OKLCHFromColor(rgba lsp.Color) LCH {
	lch := labToLCH(OKLABFromColor(rgba, false))
	return LCH{L: roundN(lch.L, 3), C: roundN(lch.C, 5), H: roundN(lch.H, 3), Alpha: lch.Alpha}
}

func ColorFromLAB(l, a, b float64, alpha ...float64) lsp.Color {
	al := 1.0
	if len(alpha) > 0 {
		al = alpha[0]
	}
	return labToColor(LAB{L: l, A: a, B: b, Alpha: al}, XYZFromLAB)
}

func ColorFromOKLAB(l, a, b float64, alpha ...float64) lsp.Color {
	al := 1.0
	if len(alpha) > 0 {
		al = alpha[0]
	}
	return labToColor(LAB{L: l, A: a, B: b, Alpha: al}, XYZFromOKLAB)
}

func LABFromLCH(l, c, h float64, alpha ...float64) LAB {
	al := 1.0
	if len(alpha) > 0 {
		al = alpha[0]
	}
	return LAB{
		L:     l,
		A:     c * math.Cos(h*math.Pi/180),
		B:     c * math.Sin(h*math.Pi/180),
		Alpha: al,
	}
}

func ColorFromLCH(l, c, h float64, alpha ...float64) lsp.Color {
	lab := LABFromLCH(l, c, h, alpha...)
	return ColorFromLAB(lab.L, lab.A, lab.B, lab.Alpha)
}

func ColorFromOKLCH(l, c, h float64, alpha ...float64) *lsp.Color {
	lab := LABFromLCH(l, c, h, alpha...)
	color := ColorFromOKLAB(lab.L, lab.A, lab.B, lab.Alpha)
	return &color
}

func labToColor(lab LAB, xyzConverter func(LAB) XYZ) lsp.Color {
	rgb := XYZToRGB(xyzConverter(lab))
	return lsp.Color{
		Red:   clamp01(rgb.Red / 255),
		Green: clamp01(rgb.Green / 255),
		Blue:  clamp01(rgb.Blue / 255),
		Alpha: lab.Alpha,
	}
}

func labToLCH(lab LAB) LCH {
	c := math.Sqrt(math.Pow(lab.A, 2) + math.Pow(lab.B, 2))
	h := math.Atan2(lab.B, lab.A) * 180 / math.Pi
	for h < 0 {
		h += 360
	}
	return LCH{L: lab.L, C: c, H: h, Alpha: lab.Alpha}
}

func roundN(value float64, places int) float64 {
	pow := math.Pow(10, float64(places))
	return math.Round((value+math.SmallestNonzeroFloat64)*pow) / pow
}

func clamp01(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}
