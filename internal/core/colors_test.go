package core

import "testing"

func TestDocumentColorsMapsStyleAttributeRange(t *testing.T) {
	source := `<div style="color: #ff0000"></div>`
	parsed := ParseDocument("file:///tmp/color.asp", source, Settings{DefaultLanguage: "VBScript"})
	colors := DocumentColors(parsed)
	if len(colors) != 1 {
		t.Fatalf("expected one color, got %#v", colors)
	}
	if colors[0].Range.Start.Line != 0 || colors[0].Range.Start.Character != len(`<div style="color: `) {
		t.Fatalf("unexpected source range: %#v", colors[0].Range)
	}
	if colors[0].Color.Red != 1 || colors[0].Color.Green != 0 || colors[0].Color.Blue != 0 || colors[0].Color.Alpha != 1 {
		t.Fatalf("unexpected color: %#v", colors[0].Color)
	}
}

func TestColorPresentationsFormatsHex(t *testing.T) {
	colors := DocumentColors(ParseDocument("file:///tmp/color.asp", `<style>.x{color:#0f0}</style>`, Settings{}))
	presentations := ColorPresentations(colors[0].Color, colors[0].Range)
	if len(presentations) != 2 || presentations[0].Label != "#00ff00" || presentations[1].Label != "rgb(0, 255, 0)" {
		t.Fatalf("unexpected presentations: %#v", presentations)
	}
	if presentations[0].TextEdit == nil || presentations[0].TextEdit.Range != colors[0].Range {
		t.Fatalf("unexpected presentation edit: %#v", presentations[0].TextEdit)
	}
}
