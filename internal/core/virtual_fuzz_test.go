package core

import (
	"testing"
	"unicode/utf8"
)

func FuzzVirtualDocumentsMapSourceText(f *testing.F) {
	for _, seed := range []string{
		"<html><head><style>a { color: <%= c %>; }</style><script>var x = '<%= v %>'; 😀</script></head><body style=\"color:red\"><% Dim a %><p>日本</p></body></html>",
		"<%@ Language=\"JScript\" %><% var a = 1; %><script runat=\"server\" language=\"JScript\">function f(){}</script>",
		"<script runat=\"server\">Sub S()\nEnd Sub</script><script>if (a < b) {}</script>",
		"<div onclick=\"go(<%= id %>)\">x</div><style>\n.a{}\n</style>",
		"<%😀0",
		"<style style=\"color: red\">a { color: blue }</style>",
	} {
		f.Add(seed)
	}
	languages := []EmbeddedLanguage{LanguageHTML, LanguageCSS, LanguageJavaScript, LanguageVBScript, LanguageJScript}
	f.Fuzz(func(t *testing.T, text string) {
		if len(text) > 4096 || !utf8.ValidString(text) {
			return
		}
		parsed := ParseDocument("file:///fuzz/page.asp", text, Settings{DefaultLanguage: "VBScript"})
		for index, region := range parsed.Regions {
			if region.Start < 0 || region.Start > region.ContentStart || region.ContentStart > region.ContentEnd || region.ContentEnd > region.End || region.End > len(text) {
				t.Fatalf("region %d %#v has invalid bounds for length %d", index, region, len(text))
			}
			for _, offset := range []int{region.Start, region.ContentStart, region.ContentEnd, region.End} {
				if offset < len(text) && !utf8.RuneStart(text[offset]) {
					t.Fatalf("region %d %#v splits a rune at %d", index, region, offset)
				}
			}
		}
		// ASP does not allow server code nested in a server script of the same
		// language, so those sources are not required to map in source order.
		nestedSameLanguage := map[EmbeddedLanguage]bool{}
		for _, owner := range parsed.Regions {
			for _, nested := range parsed.Regions {
				if nested != owner && nested.Language == owner.Language && nested.Start >= owner.ContentStart && nested.End <= owner.ContentEnd {
					nestedSameLanguage[owner.Language] = true
				}
			}
		}
		check := func(name string, virtual VirtualDocument) {
			if nestedSameLanguage[EmbeddedLanguage(virtual.LanguageID)] {
				return
			}
			previousVirtual, previousSource := 0, 0
			for index, segment := range virtual.Segments {
				if segment.VirtualStart < previousVirtual || segment.SourceStart < previousSource ||
					segment.VirtualEnd < segment.VirtualStart || segment.VirtualEnd > len(virtual.Text) ||
					segment.SourceEnd > len(text) || segment.VirtualEnd-segment.VirtualStart != segment.SourceEnd-segment.SourceStart {
					t.Fatalf("%s segment %d %#v is out of order or bounds (virtual %d, source %d)", name, index, segment, len(virtual.Text), len(text))
				}
				if virtual.Text[segment.VirtualStart:segment.VirtualEnd] != text[segment.SourceStart:segment.SourceEnd] {
					t.Fatalf("%s segment %d %#v maps %q to %q", name, index, segment, virtual.Text[segment.VirtualStart:segment.VirtualEnd], text[segment.SourceStart:segment.SourceEnd])
				}
				previousVirtual, previousSource = segment.VirtualEnd, segment.SourceEnd
			}
			if !utf8.ValidString(virtual.Text) {
				t.Fatalf("%s virtual text is not valid UTF-8: %q", name, virtual.Text)
			}
		}
		for _, language := range languages {
			check(string(language), BuildVirtualDocument(parsed, language))
		}
		for _, region := range parsed.Regions {
			if region.Language == LanguageCSS || region.Language == LanguageJavaScript || region.Language == LanguageJScript {
				check("region "+string(region.Kind), BuildEmbeddedRegionVirtualDocument(parsed, region))
			}
		}
	})
}
