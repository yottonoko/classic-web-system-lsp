package core

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

var hexColorPattern = regexp.MustCompile(`#(?:[0-9a-fA-F]{6}|[0-9a-fA-F]{3})\b`)

func DocumentColors(parsed *ParsedDocument) []lsp.ColorInformation {
	var cached []lsp.ColorInformation
	if parsed.LoadAnalysis("core.document-colors.v1", &cached) {
		return cached
	}
	source := NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	var colors []lsp.ColorInformation
	for _, region := range parsed.Regions {
		if region.Language != LanguageCSS {
			continue
		}
		text := parsed.Text[region.ContentStart:region.ContentEnd]
		for _, match := range hexColorPattern.FindAllStringIndex(text, -1) {
			start := region.ContentStart + match[0]
			end := region.ContentStart + match[1]
			color, ok := parseHexColor(parsed.Text[start:end])
			if !ok {
				continue
			}
			colors = append(colors, lsp.ColorInformation{
				Range: source.Range(start, end),
				Color: color,
			})
		}
	}
	parsed.StoreAnalysis("core.document-colors.v1", colors)
	return colors
}

func ColorPresentations(color lsp.Color, r lsp.Range) []lsp.ColorPresentation {
	hex := formatHexColor(color)
	rgb := formatRGBColor(color)
	return []lsp.ColorPresentation{
		{Label: hex, TextEdit: &lsp.TextEdit{Range: r, NewText: hex}},
		{Label: rgb, TextEdit: &lsp.TextEdit{Range: r, NewText: rgb}},
	}
}

func parseHexColor(value string) (lsp.Color, bool) {
	hex := strings.TrimPrefix(value, "#")
	if len(hex) == 3 {
		hex = string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]})
	}
	if len(hex) != 6 {
		return lsp.Color{}, false
	}
	red, ok := parseHexByte(hex[0:2])
	if !ok {
		return lsp.Color{}, false
	}
	green, ok := parseHexByte(hex[2:4])
	if !ok {
		return lsp.Color{}, false
	}
	blue, ok := parseHexByte(hex[4:6])
	if !ok {
		return lsp.Color{}, false
	}
	return lsp.Color{Red: float64(red) / 255, Green: float64(green) / 255, Blue: float64(blue) / 255, Alpha: 1}, true
}

func parseHexByte(value string) (uint64, bool) {
	parsed, err := strconv.ParseUint(value, 16, 8)
	return parsed, err == nil
}

func formatHexColor(color lsp.Color) string {
	red := colorByte(color.Red)
	green := colorByte(color.Green)
	blue := colorByte(color.Blue)
	return fmt.Sprintf("#%02x%02x%02x", red, green, blue)
}

func formatRGBColor(color lsp.Color) string {
	red := colorByte(color.Red)
	green := colorByte(color.Green)
	blue := colorByte(color.Blue)
	return fmt.Sprintf("rgb(%d, %d, %d)", red, green, blue)
}

func colorByte(value float64) int {
	if value < 0 {
		value = 0
	}
	if value > 1 {
		value = 1
	}
	return int(math.Round(value * 255))
}
