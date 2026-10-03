package core

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestTextDocumentUTF16Positions(t *testing.T) {
	doc := NewTextDocument("file:///unicode.asp", "classic-asp", 1, "a😀b\n日本語")
	if got := doc.PositionAt(len("a😀")); got != (lsp.Position{Line: 0, Character: 3}) {
		t.Fatalf("PositionAt emoji = %#v", got)
	}
	if got := doc.OffsetAt(lsp.Position{Line: 1, Character: 2}); got != len("a😀b\n日本") {
		t.Fatalf("OffsetAt Japanese = %d", got)
	}
}

func TestTextDocumentPositionAtMapsOffsetsInsideRuneToRuneStart(t *testing.T) {
	text := "a😀b\n日本"
	doc := NewTextDocument("file:///unicode.asp", "classic-asp", 1, text)
	for offset := len("a") + 1; offset < len("a😀"); offset++ {
		if got := doc.PositionAt(offset); got != (lsp.Position{Line: 0, Character: 1}) {
			t.Fatalf("PositionAt(%d) inside emoji = %#v, want character 1", offset, got)
		}
	}
	previous := lsp.Position{}
	for offset := 0; offset <= len(text); offset++ {
		got := doc.PositionAt(offset)
		if got.Line < previous.Line || got.Line == previous.Line && got.Character < previous.Character {
			t.Fatalf("PositionAt(%d) = %#v moved before PositionAt(%d) = %#v", offset, got, offset-1, previous)
		}
		previous = got
	}
}

func TestTextDocumentOffsetAtClampsToLineContentEnd(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		position lsp.Position
		want     int
	}{
		{
			name:     "LF",
			text:     "alpha\nomega",
			position: lsp.Position{Line: 0, Character: 100},
			want:     len("alpha"),
		},
		{
			name:     "CRLF",
			text:     "head\r\nalpha\r\nomega",
			position: lsp.Position{Line: 1, Character: 100},
			want:     len("head\r\nalpha"),
		},
		{
			name:     "CR",
			text:     "alpha\romega",
			position: lsp.Position{Line: 0, Character: 100},
			want:     len("alpha"),
		},
		{
			name:     "UTF-16 astral",
			text:     "head\nA😀B\nomega",
			position: lsp.Position{Line: 1, Character: 5},
			want:     len("head\nA😀B"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			doc := NewTextDocument("file:///clamp.asp", "classic-asp", 1, test.text)
			if got := doc.OffsetAt(test.position); got != test.want {
				t.Fatalf("OffsetAt(%#v) = %d, want %d", test.position, got, test.want)
			}
		})
	}
}

func TestSourceDocumentSharesRevisionIndex(t *testing.T) {
	parsed := ParseDocument("file:///shared-source.asp", "<% Dim value %>\n<%= value %>\n", Settings{DefaultLanguage: "VBScript"})
	first := SourceDocument(parsed)
	second := SourceDocument(parsed)
	if first == nil || second == nil {
		t.Fatal("SourceDocument returned nil")
	}
	if first != second {
		t.Fatal("SourceDocument built a second line index for the same revision")
	}
	fresh := NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	for offset := 0; offset <= len(parsed.Text); offset++ {
		if got, want := first.PositionAt(offset), fresh.PositionAt(offset); got != want {
			t.Fatalf("shared PositionAt(%d) = %#v, want %#v", offset, got, want)
		}
	}
	coldParsed := ParseDocument("file:///cold-shared-source.asp", "<% Dim value %>\n<%= value %>\n", Settings{DefaultLanguage: "VBScript"})
	var workers sync.WaitGroup
	results := make([]*TextDocument, 8)
	workers.Add(8)
	for index := range results {
		go func() {
			defer workers.Done()
			results[index] = SourceDocument(coldParsed)
		}()
	}
	workers.Wait()
	canonical := results[0]
	if canonical == nil {
		t.Fatal("cold concurrent SourceDocument returned nil")
	}
	for _, got := range results[1:] {
		if got != canonical {
			t.Fatalf("cold concurrent SourceDocument diverged")
		}
	}
	for range 8 {
		if got := SourceDocument(parsed); got != first {
			t.Fatalf("warm SourceDocument diverged")
		}
	}
}

func TestTextDocumentUTF16PositionsAreSafeForConcurrentReaders(t *testing.T) {
	doc := NewTextDocument("file:///unicode-concurrent.asp", "classic-asp", 1, "a😀b\n日本語")
	var workers sync.WaitGroup
	workers.Add(16)
	for range 16 {
		go func() {
			defer workers.Done()
			for range 1000 {
				if got := doc.PositionAt(len("a😀")); got != (lsp.Position{Line: 0, Character: 3}) {
					t.Errorf("PositionAt emoji = %#v", got)
					return
				}
				if got := doc.OffsetAt(lsp.Position{Line: 1, Character: 2}); got != len("a😀b\n日本") {
					t.Errorf("OffsetAt Japanese = %d", got)
					return
				}
			}
		}()
	}
	workers.Wait()
}

func TestTextDocumentRoundTripsUTF16Positions(t *testing.T) {
	for _, text := range []string{"", "a\nb", "a\r\nb", "😀\nvalue"} {
		doc := NewTextDocument("file:///roundtrip.asp", "classic-asp", 1, text)
		for offset := 0; offset <= len(text); offset++ {
			if offset > 0 && offset < len(text) && !isUTF8Boundary(text, offset) {
				continue
			}
			position := doc.PositionAt(offset)
			want := offset
			if offset > 0 && offset < len(text) && text[offset-1] == '\r' && text[offset] == '\n' {
				want--
			}
			if got := doc.OffsetAt(position); got != want {
				t.Fatalf("roundtrip %q offset %d -> %#v -> %d, want %d", text, offset, position, got, want)
			}
		}
	}
}

func TestTextDocumentRangeFromOffsets(t *testing.T) {
	text := "😀\nvalue"
	doc := NewTextDocument("file:///range.asp", "classic-asp", 1, text)
	r := doc.Range(len("😀\n"), len(text))
	if r.Start != (lsp.Position{Line: 1, Character: 0}) || r.End != (lsp.Position{Line: 1, Character: len("value")}) {
		t.Fatalf("range = %#v", r)
	}
}

func TestTextDocumentApplyRangeChange(t *testing.T) {
	doc := NewTextDocument("file:///change.asp", "classic-asp", 1, "Hello <%= name %>")
	doc.ApplyChange(&lsp.Range{
		Start: lsp.Position{Line: 0, Character: 10},
		End:   lsp.Position{Line: 0, Character: 14},
	}, "customer", 2)
	if doc.Text != "Hello <%= customer %>" {
		t.Fatalf("changed text = %q", doc.Text)
	}
	if doc.Version != 2 {
		t.Fatalf("version = %d", doc.Version)
	}
}

func TestTextDocumentApplyRangeChangeUsesUTF16Offsets(t *testing.T) {
	doc := NewTextDocument("file:///change.asp", "classic-asp", 1, "a😀b\n日本語")
	doc.ApplyChange(&lsp.Range{
		Start: lsp.Position{Line: 0, Character: 3},
		End:   lsp.Position{Line: 0, Character: 4},
	}, "B", 2)
	if doc.Text != "a😀B\n日本語" || doc.Version != 2 {
		t.Fatalf("document = %q version %d", doc.Text, doc.Version)
	}
	doc.ApplyChange(&lsp.Range{
		Start: lsp.Position{Line: 1, Character: 1},
		End:   lsp.Position{Line: 1, Character: 2},
	}, "本", 3)
	if doc.Text != "a😀B\n日本語" || doc.Version != 3 {
		t.Fatalf("document = %q version %d", doc.Text, doc.Version)
	}
}

func TestTextDocumentIncrementalLinePositionsMatchFreshDocument(t *testing.T) {
	tests := []struct {
		name        string
		initial     string
		start       int
		end         int
		replacement string
	}{
		{
			name:        "LF",
			initial:     "alpha\nbeta",
			start:       len("alpha\n"),
			end:         len("alpha\n"),
			replacement: "x",
		},
		{
			name:        "create CRLF across rescan boundary",
			initial:     "alpha\rbeta",
			start:       len("alpha\r"),
			end:         len("alpha\r"),
			replacement: "\n",
		},
		{
			name:        "break CRLF",
			initial:     "alpha\r\nbeta",
			start:       len("alpha\r"),
			end:         len("alpha\r\n"),
			replacement: "",
		},
		{
			name:        "join CR and LF across rescan boundary",
			initial:     "alpha\rx\nbeta",
			start:       len("alpha\r"),
			end:         len("alpha\rx"),
			replacement: "",
		},
		{
			name:        "CR",
			initial:     "alpha\rbeta",
			start:       len("alpha\r"),
			end:         len("alpha\r"),
			replacement: "x",
		},
		{
			name:        "UTF-16 astral before CRLF boundary",
			initial:     "😀\rbeta",
			start:       len("😀\r"),
			end:         len("😀\r"),
			replacement: "\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			doc := NewTextDocument("file:///incremental.asp", "classic-asp", 1, test.initial)
			doc.applyByteChange(test.start, test.end, test.replacement, 2)

			wantText := test.initial[:test.start] + test.replacement + test.initial[test.end:]
			if doc.Text != wantText {
				t.Fatalf("text = %q, want %q", doc.Text, wantText)
			}
			fresh := NewTextDocument(doc.URI, doc.LanguageID, doc.Version, wantText)
			for offset := 0; offset <= len(wantText); offset++ {
				if offset > 0 && offset < len(wantText) && !isUTF8Boundary(wantText, offset) {
					continue
				}
				if got, want := doc.PositionAt(offset), fresh.PositionAt(offset); got != want {
					t.Fatalf("PositionAt(%d) = %#v, want %#v", offset, got, want)
				}
			}
		})
	}
}

func isUTF8Boundary(text string, offset int) bool {
	return offset == len(text) || (text[offset]&0b1100_0000) != 0b1000_0000
}

func TestLineIndexPreallocationPreservesMixedNewlinesAndUTF16(t *testing.T) {
	for _, text := range []string{"", "plain", "\r\n", "\r\r\n\n", "a😀\r日本\ntext\r\n", strings.Repeat("a😀\r日本\ntext\r\n", 5000)} {
		starts, ascii := lineStartsAndASCII(text)
		wantStarts, wantASCII, err := lineStartsAndASCIIContext(context.Background(), text)
		if err != nil || !slices.Equal(starts, wantStarts) || !slices.Equal(ascii, wantASCII) {
			t.Fatalf("line indexes differ for %d bytes: %v", len(text), err)
		}
		if cap(starts) != len(starts) || cap(ascii) != len(ascii) {
			t.Fatalf("line indexes retain spare storage: starts %d/%d, ascii %d/%d", len(starts), cap(starts), len(ascii), cap(ascii))
		}
	}
	source := strings.Repeat("Response.Write value\r\n", 5000)
	if allocations := testing.AllocsPerRun(10, func() { lineStartsAndASCII(source) }); allocations > 2 {
		t.Fatalf("line index allocations = %.0f, want at most 2", allocations)
	}
}
