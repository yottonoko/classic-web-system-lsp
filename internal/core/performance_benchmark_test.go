package core

import (
	"strconv"
	"strings"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

var performanceBenchmarkSink any
var performanceBenchmarkIntSink int
var performanceBenchmarkPositionSink lsp.Position

func BenchmarkClassicASPParserFullParse(b *testing.B) {
	for _, blocks := range []int{250, 500, 1000, 2000} {
		b.Run("blocks="+strconv.Itoa(blocks), func(b *testing.B) {
			source := benchmarkClassicASPDocument(blocks)
			b.ReportAllocs()
			for b.Loop() {
				performanceBenchmarkSink = ParseDocument("file:///bench/default.asp", source, Settings{DefaultLanguage: "VBScript"})
			}
		})
	}
}

func BenchmarkClassicASPParserRawTextAndCommentScaling(b *testing.B) {
	for _, blocks := range []int{250, 500, 1000, 2000} {
		b.Run("blocks="+strconv.Itoa(blocks), func(b *testing.B) {
			source := benchmarkClassicASPRawTextAndCommentDocument(blocks)
			b.ReportAllocs()
			for b.Loop() {
				performanceBenchmarkSink = ParseDocument("file:///bench/raw-text.asp", source, Settings{DefaultLanguage: "VBScript"})
			}
		})
	}
}

func BenchmarkClassicASPParserSingleHTMLCommentScaling(b *testing.B) {
	for _, blocks := range []int{250, 500, 1000, 2000} {
		b.Run("blocks="+strconv.Itoa(blocks), func(b *testing.B) {
			source := benchmarkClassicASPSingleHTMLCommentDocument(blocks)
			b.ReportAllocs()
			for b.Loop() {
				// Clone the source so each iteration exercises one fresh indexed
				// scan instead of the bounded source cache's warm path.
				freshSource := strings.Clone(source)
				performanceBenchmarkSink = ParseDocument("file:///bench/comment.asp", freshSource, Settings{DefaultLanguage: "VBScript"})
			}
		})
	}
}

func BenchmarkClassicASPParserIncrementalEditEquivalent(b *testing.B) {
	benchmarkUpdateParsedDocument(b, 0)
}

func BenchmarkUpdateParsedDocumentIncludes(b *testing.B) {
	for _, includeCount := range []int{0, 10, 100, 1000} {
		b.Run("includes="+strconv.Itoa(includeCount), func(b *testing.B) {
			benchmarkUpdateParsedDocument(b, includeCount)
		})
	}
}

func benchmarkUpdateParsedDocument(b *testing.B, includeCount int) {
	b.Helper()
	const uri = "file:///bench/incremental.asp"
	source := benchmarkIncrementalIncludeDocument(includeCount)
	previous := ParseDocument(uri, source, Settings{DefaultLanguage: "VBScript"})
	if len(previous.Includes) != includeCount {
		b.Fatalf("parsed includes = %d, want %d", len(previous.Includes), includeCount)
	}
	start := strings.LastIndex(source, "benchmarkValue")
	if start < 0 {
		b.Fatal("benchmark source missing benchmarkValue")
	}
	document := NewTextDocument(uri, "classic-asp", 1, source)
	changeRange := document.Range(start, start+len("benchmarkValue"))
	change := IncrementalChange{
		Range:        &changeRange,
		Text:         "updatedValue",
		ByteStart:    start,
		ByteEnd:      start + len("benchmarkValue"),
		HasByteRange: true,
	}
	if probe := UpdateParsedDocument(previous, []IncrementalChange{change}, Settings{DefaultLanguage: "VBScript"}); !probe.Incremental {
		b.Fatalf("benchmark edit fell back: %s", probe.Reason)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		performanceBenchmarkSink = UpdateParsedDocument(previous, []IncrementalChange{change}, Settings{DefaultLanguage: "VBScript"})
	}
}

func BenchmarkClassicASPFormatterVBScriptBlock(b *testing.B) {
	source := "<%\n" + benchmarkVBScriptBlock(2000) + "\n%>"
	parsed := ParseDocument("file:///bench/format.asp", source, Settings{DefaultLanguage: "VBScript"})
	options := FormattingOptions{TabSize: 2, InsertSpaces: true}
	b.ReportAllocs()
	for b.Loop() {
		performanceBenchmarkSink = FormatDocument(parsed, options)
	}
}

func BenchmarkClassicASPVirtualDocumentVBScript(b *testing.B) {
	source := benchmarkClassicASPDocument(1000)
	parsed := ParseDocument("file:///bench/default.asp", source, Settings{DefaultLanguage: "VBScript"})
	b.ReportAllocs()
	for b.Loop() {
		performanceBenchmarkSink = BuildVirtualDocument(parsed, LanguageVBScript)
	}
}

func BenchmarkClassicASPPositionAt200KBFixture(b *testing.B) {
	fixture := benchmarkPositionFixture(4000)
	doc := NewTextDocument("file:///bench/position.asp", "classic-asp", 1, fixture)
	offsets := benchmarkPositionOffsets(len(fixture), 512)
	b.ReportAllocs()
	for b.Loop() {
		var position lsp.Position
		for _, offset := range offsets {
			position = doc.PositionAt(offset)
		}
		performanceBenchmarkPositionSink = position
	}
}

func BenchmarkClassicASPOffsetAt200KBFixture(b *testing.B) {
	fixture := benchmarkPositionFixture(4000)
	doc := NewTextDocument("file:///bench/position.asp", "classic-asp", 1, fixture)
	offsets := benchmarkPositionOffsets(len(fixture), 512)
	positions := make([]lsp.Position, 0, len(offsets))
	for _, offset := range offsets {
		positions = append(positions, doc.PositionAt(offset))
	}
	b.ReportAllocs()
	for b.Loop() {
		var offset int
		for _, position := range positions {
			offset += doc.OffsetAt(position)
		}
		performanceBenchmarkIntSink = offset
	}
}

func BenchmarkTextDocumentApplyChange200KBFixture(b *testing.B) {
	fixture := benchmarkPositionFixture(4000)
	document := NewTextDocument("file:///bench/change.asp", "classic-asp", 1, fixture)
	offset := len(fixture) / 2
	for offset < len(fixture) && fixture[offset] != 'x' {
		offset++
	}
	if offset >= len(fixture) {
		b.Fatal("benchmark replacement position missing")
	}
	changeRange := document.Range(offset, offset+1)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; b.Loop(); index++ {
		replacement := "x"
		if index%2 == 0 {
			replacement = "y"
		}
		document.ApplyChange(&changeRange, replacement, index+2)
	}
}

func BenchmarkVirtualDocumentPositionMappingLargeFixture(b *testing.B) {
	source := benchmarkClassicASPDocument(4000)
	parsed := ParseDocument("file:///bench/mapping.asp", source, Settings{DefaultLanguage: "VBScript"})
	virtual := BuildVirtualDocument(parsed, LanguageVBScript)
	document := NewTextDocument(parsed.URI, "classic-asp", 1, source)
	position := document.PositionAt(strings.LastIndex(source, "Response.Write"))
	if _, ok := virtual.ToVirtualPosition(position, document); !ok {
		b.Fatal("benchmark position did not map")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		performanceBenchmarkPositionSink, _ = virtual.ToVirtualPosition(position, document)
	}
}

func benchmarkClassicASPDocument(blocks int) string {
	var b strings.Builder
	for i := range blocks {
		b.WriteString(`<section data-index="`)
		b.WriteString(strconv.Itoa(i))
		b.WriteString(`">`)
		b.WriteByte('\n')
		b.WriteString("<%\n")
		b.WriteString("Dim value")
		b.WriteString(strconv.Itoa(i))
		b.WriteByte('\n')
		b.WriteString("value")
		b.WriteString(strconv.Itoa(i))
		b.WriteString(" = ")
		b.WriteString(strconv.Itoa(i))
		b.WriteByte('\n')
		b.WriteString("If value")
		b.WriteString(strconv.Itoa(i))
		b.WriteString(" > 0 Then\n")
		b.WriteString("  Response.Write value")
		b.WriteString(strconv.Itoa(i))
		b.WriteByte('\n')
		b.WriteString("End If\n")
		b.WriteString("%>\n")
		b.WriteString("</section>")
		if i+1 < blocks {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func benchmarkClassicASPRawTextAndCommentDocument(blocks int) string {
	var b strings.Builder
	for i := range blocks {
		b.WriteString(`<textarea data-index="`)
		b.WriteString(strconv.Itoa(i))
		b.WriteString(`">fake <% no close</textarea>`)
		b.WriteByte('\n')
		b.WriteString(`<!-- fake <% no close -->`)
		b.WriteByte('\n')
		b.WriteString(`<section><% Response.Write `)
		b.WriteString(strconv.Itoa(i))
		b.WriteString(` %></section>`)
		if i+1 < blocks {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func benchmarkClassicASPSingleHTMLCommentDocument(blocks int) string {
	var b strings.Builder
	b.WriteString("<!--")
	for i := range blocks {
		b.WriteString("<% Response.Write ")
		b.WriteString(strconv.Itoa(i))
		b.WriteString(" %>")
	}
	b.WriteString("-->")
	return b.String()
}

func benchmarkIncrementalIncludeDocument(includeCount int) string {
	var b strings.Builder
	for index := range includeCount {
		b.WriteString(`<!-- #include file="partials/part-`)
		b.WriteString(strconv.Itoa(index))
		b.WriteString(`.inc" -->`)
		b.WriteByte('\n')
	}
	b.WriteString(`<%
Dim benchmarkValue
benchmarkValue = 1
Response.Write benchmarkValue
%>`)
	return b.String()
}

func benchmarkPositionFixture(lines int) string {
	var b strings.Builder
	for i := range lines {
		b.WriteString("line ")
		padded := strconv.Itoa(i)
		for range 4 - len(padded) {
			b.WriteByte('0')
		}
		b.WriteString(padded)
		b.WriteByte(' ')
		b.WriteString(strings.Repeat("x", 38))
		if i+1 < lines {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func benchmarkPositionOffsets(length int, count int) []int {
	offsets := make([]int, 0, count)
	for i := range count {
		offsets = append(offsets, length*i/count)
	}
	return offsets
}

func benchmarkVBScriptBlock(lines int) string {
	var b strings.Builder
	for i := range lines {
		b.WriteString("If value")
		b.WriteString(strconv.Itoa(i))
		b.WriteString(">0 Then\n")
		b.WriteString("Response.Write value")
		b.WriteString(strconv.Itoa(i))
		b.WriteByte('\n')
		b.WriteString("End If")
		if i+1 < lines {
			b.WriteByte('\n')
		}
	}
	return b.String()
}
