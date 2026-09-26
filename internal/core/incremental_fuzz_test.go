package core

import (
	"math"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestMatchesAFreshParseAfterSeededRandomEdits(t *testing.T) {
	random := newMulberry32(0x5eed_2026)
	text := `<%@ LANGUAGE="VBScript" %>
<html>
<head>
<style>body { color: red; }</style>
<script>const value = "😀";</script>
</head>
<body>
<!-- #include file="common.inc" -->
<% Option Explicit
Dim message
message = "hello"
Response.Write message
%>
	</body>
</html>`
	doc := NewTextDocument("file:///site/incremental-fuzz.asp", "classic-asp", 1, text)
	parsed := ParseDocument(doc.URI, doc.Text, Settings{DefaultLanguage: "VBScript"})

	for step := 0; step < 250; step++ {
		start := randomUTF8Boundary(text, random)
		maxDelete := 8
		if len(text) > 2500 {
			maxDelete = 24
		}
		deleteLength := min(len(text)-start, int(math.Floor(random()*float64(maxDelete))))
		end := start + deleteLength
		for end < len(text) && !utf8.RuneStart(text[end]) {
			end++
		}
		replacement := randomReplacement(random, len(text))
		changeRange := lsp.Range{
			Start: doc.PositionAt(start),
			End:   doc.PositionAt(end),
		}
		text = text[:start] + replacement + text[end:]
		updated := UpdateParsedDocument(parsed, []IncrementalChange{{Range: &changeRange, Text: replacement}}, Settings{DefaultLanguage: "VBScript"})
		parsed = updated.Parsed
		doc.ApplyChange(&changeRange, replacement, step+2)
		fresh := ParseDocument(doc.URI, text, Settings{DefaultLanguage: "VBScript"})
		freshDocument := NewTextDocument(doc.URI, doc.LanguageID, doc.Version, text)
		if doc.Text != text {
			t.Fatalf("document text mismatch at step %d", step)
		}
		for offset := 0; offset <= len(text); offset++ {
			if offset > 0 && offset < len(text) && !isUTF8Boundary(text, offset) {
				continue
			}
			if got, want := doc.PositionAt(offset), freshDocument.PositionAt(offset); got != want {
				t.Fatalf("position index mismatch at step %d offset %d: got %#v, want %#v", step, offset, got, want)
			}
			position := freshDocument.PositionAt(offset)
			if got, want := doc.OffsetAt(position), freshDocument.OffsetAt(position); got != want {
				t.Fatalf("offset index mismatch at step %d position %#v: got %d, want %d", step, position, got, want)
			}
		}
		if parsed == nil || !parsedDocumentsEqual(parsed, fresh) {
			t.Fatalf("incremental fuzz mismatch at step %d", step)
		}
		random = newMulberry32(uint32(0x5eed_2026 + step + len(text)))
	}
}

func randomOffset(length int, random func() float64) int {
	return int(math.Floor(random() * float64(length+1)))
}

func randomUTF8Boundary(text string, random func() float64) int {
	offset := randomOffset(len(text), random)
	for offset < len(text) && !utf8.RuneStart(text[offset]) {
		offset++
	}
	return offset
}

func randomReplacement(random func() float64, currentLength int) string {
	fragments := []string{
		"",
		"x",
		" value",
		"\n",
		"😀",
		"<%",
		"%>",
		`<% Response.Write "fuzz" %>`,
		`<!-- #include file="fuzz.inc" -->`,
		`<script>const fuzz = 1;</script>`,
		`<style>.fuzz { color: blue; }</style>`,
		`style="color: coral;"`,
	}
	if currentLength <= 2500 {
		fragments = append(fragments, `"42"`)
	}
	return fragments[int(math.Floor(random()*float64(len(fragments))))]
}

func newMulberry32(seed uint32) func() float64 {
	value := seed
	return func() float64 {
		value += 0x6d2b79f5
		result := value
		result = (result ^ (result >> 15)) * (result | 1)
		result ^= result + ((result ^ (result >> 7)) * (result | 61))
		return float64((result^(result>>14))>>0) / 4294967296
	}
}

func parsedDocumentsEqual(left *ParsedDocument, right *ParsedDocument) bool {
	if left.URI != right.URI ||
		left.Text != right.Text ||
		left.DefaultLanguage != right.DefaultLanguage ||
		len(left.Regions) != len(right.Regions) ||
		len(left.Includes) != len(right.Includes) ||
		len(left.Errors) != len(right.Errors) {
		return false
	}
	for i := range left.Regions {
		if left.Regions[i] != right.Regions[i] {
			return false
		}
	}
	for i := range left.Includes {
		if left.Includes[i] != right.Includes[i] {
			return false
		}
	}
	for i := range left.Errors {
		if left.Errors[i] != right.Errors[i] {
			return false
		}
	}
	return true
}

func TestIncrementalRangeMapperForSharesRevisionMapper(t *testing.T) {
	parsed := ParseDocument("file:///shared-mapper.asp", "<% Dim firstValue %>\n<%= firstValue %>\n<p>hello</p>\n", Settings{DefaultLanguage: "VBScript"})
	if _, ok := IncrementalRangeMapperFor(parsed); ok {
		t.Fatal("fresh parse should not have a range mapper")
	}
	doc := SourceDocument(parsed)
	end := doc.OffsetAt(lsp.Position{Line: 2, Character: len("<p>hello")})
	changeRange := doc.Range(end, end)
	result := UpdateParsedDocument(parsed, []IncrementalChange{{Range: &changeRange, Text: "!"}}, Settings{DefaultLanguage: "VBScript"})
	if !result.Incremental {
		t.Fatalf("expected incremental update, got %q", result.Reason)
	}
	updated := result.Parsed
	first, ok := IncrementalRangeMapperFor(updated)
	if !ok {
		t.Fatal("incremental revision should have a range mapper")
	}
	second, ok := IncrementalRangeMapperFor(updated)
	if !ok || second != first {
		t.Fatal("range mapper was rebuilt for the same revision")
	}
	coldParsed := ParseDocument("file:///cold-shared-mapper.asp", "<% Dim firstValue %>\n<%= firstValue %>\n<p>hello</p>\n", Settings{DefaultLanguage: "VBScript"})
	coldDocument := SourceDocument(coldParsed)
	coldEnd := coldDocument.OffsetAt(lsp.Position{Line: 2, Character: len("<p>hello")})
	coldRange := coldDocument.Range(coldEnd, coldEnd)
	coldResult := UpdateParsedDocument(coldParsed, []IncrementalChange{{Range: &coldRange, Text: "!"}}, Settings{DefaultLanguage: "VBScript"})
	coldUpdated := coldResult.Parsed
	var workers sync.WaitGroup
	results := make([]*IncrementalRangeMapper, 8)
	workers.Add(8)
	for index := range results {
		go func() {
			defer workers.Done()
			mapper, mapperOK := IncrementalRangeMapperFor(coldUpdated)
			if mapperOK {
				results[index] = mapper
			}
		}()
	}
	workers.Wait()
	coldCanonical := results[0]
	if coldCanonical == nil {
		t.Fatal("cold concurrent range mapper was not created")
	}
	for _, mapper := range results[1:] {
		if mapper != coldCanonical {
			t.Fatalf("cold concurrent range mapper diverged")
		}
	}
	direct, ok := NewIncrementalRangeMapper(updated)
	if !ok {
		t.Fatal("direct mapper build failed")
	}
	for _, offset := range []int{0, 1, len(updated.Text), len(updated.Text) - 1} {
		want, wantOK := direct.Offset(offset)
		if got, gotOK := first.Offset(offset); got != want || gotOK != wantOK {
			t.Fatalf("shared Offset(%d) = (%d, %v), want (%d, %v)", offset, got, gotOK, want, wantOK)
		}
	}
}
