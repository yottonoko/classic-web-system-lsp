package services

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/yottonoko/vscode-css-languageservice-go/languagefacts"
	"github.com/yottonoko/vscode-css-languageservice-go/lsp"
)

var cssColorComponentPattern = regexp.MustCompile(`(?i)none|[-+]?(?:\d+(?:\.\d*)?|\.\d+)(?:%|deg|grad|rad|turn)?`)

func FindDocumentColors(document *lsp.TextDocument) []lsp.ColorInformation {
	text := document.Text()
	var result []lsp.ColorInformation
	for i := 0; i < len(text); i++ {
		if text[i] == '#' {
			end := i + 1
			for end < len(text) && isHexDigit(text[end]) {
				end++
			}
			if color := languagefacts.ColorFromHex(text[i:end]); color != nil {
				result = append(result, lsp.ColorInformation{Range: rangeFromOffsets(document, i, end), Color: *color})
				i = end - 1
			}
			continue
		}
		if name, ok := colorFunctionNameAt(text, i); ok {
			open := i + len(name)
			close := matchingParenString(text, open)
			if close == -1 {
				continue
			}
			args := text[open+1 : close]
			if color, ok := colorFromFunction(name, args); ok {
				result = append(result, lsp.ColorInformation{Range: rangeFromOffsets(document, i, close+1), Color: color})
			}
			i = close
		}
	}
	blocks := parseCSSBlocks(text)
	result = append(result, namedColorsInBlocks(document, blocks)...)
	if document.LanguageID == "scss" {
		result = append(result, namedColorsInSCSSValueContexts(document)...)
	}
	return dedupeColorInformation(result)
}

func namedColorsInBlocks(document *lsp.TextDocument, blocks []cssBlock) []lsp.ColorInformation {
	text := document.Text()
	if shouldParallelizeBlockWork(text, blocks) {
		return parallelBlockMap(blocks, parallelWorkerCount(len(blocks)), func(chunk []cssBlock) []lsp.ColorInformation {
			return namedColorsInBlockRange(document, chunk)
		})
	}
	return namedColorsInBlockRange(document, blocks)
}

func namedColorsInBlockRange(document *lsp.TextDocument, blocks []cssBlock) []lsp.ColorInformation {
	text := document.Text()
	var result []lsp.ColorInformation
	for _, block := range blocks {
		for _, declaration := range parseDeclarations(text, block.bodyStart, block.bodyEnd) {
			result = append(result, namedColorsInDeclarationValue(document, declaration)...)
		}
	}
	return result
}

func colorFunctionNameAt(text string, offset int) (string, bool) {
	switch text[offset] {
	case 'r', 'R':
		if hasASCIIPrefixFold(text, offset, "rgba(") {
			return "rgba", true
		}
		if hasASCIIPrefixFold(text, offset, "rgb(") {
			return "rgb", true
		}
	case 'h', 'H':
		if hasASCIIPrefixFold(text, offset, "hsla(") {
			return "hsla", true
		}
		if hasASCIIPrefixFold(text, offset, "hsl(") {
			return "hsl", true
		}
		if hasASCIIPrefixFold(text, offset, "hwb(") {
			return "hwb", true
		}
	case 'o', 'O':
		if hasASCIIPrefixFold(text, offset, "oklch(") {
			return "oklch", true
		}
		if hasASCIIPrefixFold(text, offset, "oklab(") {
			return "oklab", true
		}
	case 'l', 'L':
		if hasASCIIPrefixFold(text, offset, "lch(") {
			return "lch", true
		}
		if hasASCIIPrefixFold(text, offset, "lab(") {
			return "lab", true
		}
	}
	return "", false
}

func hasASCIIPrefixFold(text string, offset int, prefix string) bool {
	if offset < 0 || len(text)-offset < len(prefix) {
		return false
	}
	for i := 0; i < len(prefix); i++ {
		if asciiLower(text[offset+i]) != asciiLower(prefix[i]) {
			return false
		}
	}
	return true
}

func asciiLower(ch byte) byte {
	if ch >= 'A' && ch <= 'Z' {
		return ch + ('a' - 'A')
	}
	return ch
}

func dedupeColorInformation(values []lsp.ColorInformation) []lsp.ColorInformation {
	seen := map[lsp.Range]bool{}
	result := make([]lsp.ColorInformation, 0, len(values))
	for _, value := range values {
		if seen[value.Range] {
			continue
		}
		seen[value.Range] = true
		result = append(result, value)
	}
	return result
}

func namedColorsInDeclarationValue(document *lsp.TextDocument, declaration cssDeclaration) []lsp.ColorInformation {
	text := document.Text()
	start := declaration.valueStart
	end := min(len(text), declaration.valueStart+len(declaration.value))
	var result []lsp.ColorInformation
	for i := start; i < end; i++ {
		next := skipCSSIgnored(text, i)
		if next != i {
			i = next
			continue
		}
		if !isSelectorIdentifierByte(text[i]) {
			continue
		}
		tokenStart := i
		for i < end && isSelectorIdentifierByte(text[i]) {
			i++
		}
		token := text[tokenStart:i]
		color, ok := namedColorValue(token)
		if !ok {
			i--
			continue
		}
		j := i
		for j < end && isCSSSpace(text[j]) {
			j++
		}
		if j < end && text[j] == '(' {
			i--
			continue
		}
		result = append(result, lsp.ColorInformation{Range: rangeFromOffsets(document, tokenStart, i), Color: color})
		i--
	}
	return result
}

func namedColorValue(name string) (lsp.Color, bool) {
	color, ok := languagefacts.ColorFromName(name)
	if !ok {
		return lsp.Color{}, false
	}
	return *color, true
}

func namedColorsInSCSSValueContexts(document *lsp.TextDocument) []lsp.ColorInformation {
	text := document.Text()
	var result []lsp.ColorInformation
	for i := 0; i < len(text); i++ {
		next := skipCSSIgnored(text, i)
		if next != i {
			i = next
			continue
		}
		if !isSelectorIdentifierByte(text[i]) {
			continue
		}
		tokenStart := i
		for i < len(text) && isSelectorIdentifierByte(text[i]) {
			i++
		}
		token := text[tokenStart:i]
		color, ok := namedColorValue(token)
		if !ok || !scssNamedColorIsValue(text, tokenStart, i) {
			i--
			continue
		}
		result = append(result, lsp.ColorInformation{Range: rangeFromOffsets(document, tokenStart, i), Color: color})
		i--
	}
	return result
}

func scssNamedColorIsValue(text string, start, end int) bool {
	next := nextNonSpace(text, end)
	if next < len(text) && (text[next] == '(' || text[next] == '{') {
		return false
	}
	segmentStart := lastStatementBoundary(text, start)
	segment := strings.ToLower(text[segmentStart:start])
	prev := previousNonSpace(text, start-1)
	if strings.Contains(segment, "@return") {
		return true
	}
	if prev == ':' && (strings.Contains(segment, "@include") || strings.Contains(segment, "@function")) {
		return true
	}
	return false
}

func nextNonSpace(text string, index int) int {
	for index < len(text) && isCSSSpace(text[index]) {
		index++
	}
	return index
}

func GetColorPresentations(color lsp.Color, r lsp.Range) []lsp.ColorPresentation {
	red256 := int(math.Round(color.Red * 255))
	green256 := int(math.Round(color.Green * 255))
	blue256 := int(math.Round(color.Blue * 255))
	alpha := color.Alpha
	if alpha == 0 {
		alpha = 1
	}

	var labels []string
	if alpha == 1 {
		labels = append(labels, fmt.Sprintf("rgb(%d, %d, %d)", red256, green256, blue256))
		labels = append(labels, fmt.Sprintf("#%02x%02x%02x", red256, green256, blue256))
	} else {
		labels = append(labels, fmt.Sprintf("rgba(%d, %d, %d, %s)", red256, green256, blue256, formatNumber(alpha)))
		labels = append(labels, fmt.Sprintf("#%02x%02x%02x%02x", red256, green256, blue256, int(math.Round(alpha*255))))
	}

	hsl := languagefacts.HSLFromColor(color)
	if alpha == 1 {
		labels = append(labels, fmt.Sprintf("hsl(%s, %d%%, %d%%)", formatNumber(hsl.H), int(math.Round(hsl.S*100)), int(math.Round(hsl.L*100))))
	} else {
		labels = append(labels, fmt.Sprintf("hsla(%s, %d%%, %d%%, %s)", formatNumber(hsl.H), int(math.Round(hsl.S*100)), int(math.Round(hsl.L*100)), formatNumber(alpha)))
	}

	hwb := languagefacts.HWBFromColor(color)
	if alpha == 1 {
		labels = append(labels, fmt.Sprintf("hwb(%s %d%% %d%%)", formatNumber(hwb.H), int(math.Round(hwb.W*100)), int(math.Round(hwb.B*100))))
	} else {
		labels = append(labels, fmt.Sprintf("hwb(%s %d%% %d%% / %s)", formatNumber(hwb.H), int(math.Round(hwb.W*100)), int(math.Round(hwb.B*100)), formatNumber(alpha)))
	}

	lab := languagefacts.LABFromColor(color)
	lch := languagefacts.LCHFromColor(color)
	oklab := languagefacts.OKLABFromColor(color)
	oklch := languagefacts.OKLCHFromColor(color)
	labels = append(labels, colorFunctionLabel("lab", []string{formatNumber(lab.L) + "%", formatNumber(lab.A), formatNumber(lab.B)}, alpha))
	labels = append(labels, colorFunctionLabel("lch", []string{formatNumber(lch.L) + "%", formatNumber(lch.C), formatNumber(lch.H)}, alpha))
	labels = append(labels, colorFunctionLabel("oklab", []string{formatNumber(oklab.L) + "%", formatNumber(oklab.A), formatNumber(oklab.B)}, alpha))
	labels = append(labels, colorFunctionLabel("oklch", []string{formatNumber(oklch.L) + "%", formatNumber(oklch.C), formatNumber(oklch.H)}, alpha))

	result := make([]lsp.ColorPresentation, len(labels))
	for i, label := range labels {
		edit := lsp.Replace(r, label)
		result[i] = lsp.ColorPresentation{Label: label, TextEdit: &edit}
	}
	return result
}

func colorFromFunction(name, args string) (lsp.Color, bool) {
	if strings.Contains(args, "$") {
		return lsp.Color{}, false
	}
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
		return languagefacts.ColorFrom256RGB(rgbComponent(values[0]), rgbComponent(values[1]), rgbComponent(values[2]), alpha), true
	case "hsl", "hsla":
		if len(values) < 3 {
			return lsp.Color{}, false
		}
		alpha := 1.0
		if len(values) >= 4 {
			alpha = alphaComponent(values[3])
		}
		return languagefacts.ColorFromHSL(angleComponent(values[0]), percentComponent(values[1]), percentComponent(values[2]), alpha), true
	case "hwb":
		if len(values) < 3 {
			return lsp.Color{}, false
		}
		alpha := 1.0
		if len(values) >= 4 {
			alpha = alphaComponent(values[3])
		}
		return languagefacts.ColorFromHWB(angleComponent(values[0]), percentComponent(values[1]), percentComponent(values[2]), alpha), true
	case "lab":
		if len(values) < 3 {
			return lsp.Color{}, false
		}
		alpha := 1.0
		if len(values) >= 4 {
			alpha = alphaComponent(values[3])
		}
		return languagefacts.ColorFromLAB(labLightnessComponent(values[0]), values[1].value, values[2].value, alpha), true
	case "lch":
		if len(values) < 3 {
			return lsp.Color{}, false
		}
		alpha := 1.0
		if len(values) >= 4 {
			alpha = alphaComponent(values[3])
		}
		return languagefacts.ColorFromLCH(labLightnessComponent(values[0]), values[1].value, angleComponent(values[2]), alpha), true
	case "oklab":
		if len(values) < 3 {
			return lsp.Color{}, false
		}
		alpha := 1.0
		if len(values) >= 4 {
			alpha = alphaComponent(values[3])
		}
		return languagefacts.ColorFromOKLAB(okLightnessComponent(values[0]), okABComponent(values[1]), okABComponent(values[2]), alpha), true
	case "oklch":
		if len(values) < 3 {
			return lsp.Color{}, false
		}
		alpha := 1.0
		if len(values) >= 4 {
			alpha = alphaComponent(values[3])
		}
		color := languagefacts.ColorFromOKLCH(okLightnessComponent(values[0]), okChromaComponent(values[1]), angleComponent(values[2]), alpha)
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

func colorFunctionLabel(name string, args []string, alpha float64) string {
	label := name + "(" + strings.Join(args, " ")
	if alpha != 1 {
		label += " / " + formatNumber(alpha)
	}
	return label + ")"
}

func formatNumber(value float64) string {
	if math.Abs(value) < 0.0000001 {
		value = 0
	}
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func isHexDigit(b byte) bool {
	return b >= '0' && b <= '9' || b >= 'a' && b <= 'f' || b >= 'A' && b <= 'F'
}
