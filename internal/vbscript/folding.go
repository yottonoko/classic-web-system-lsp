package vbscript

import (
	"sort"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

type foldLine struct {
	Line           int
	StartCharacter int
	EndCharacter   int
	Text           string
}

type foldStart struct {
	Line           int
	StartCharacter int
}

func FoldingRanges(parsed *core.ParsedDocument) []lsp.FoldingRange {
	if parsed == nil {
		return nil
	}
	var cached []lsp.FoldingRange
	if parsed.LoadAnalysis("vbscript.folding-ranges.v1", &cached) {
		return cached
	}
	doc := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	var ranges []lsp.FoldingRange
	for _, region := range parsed.Regions {
		if region.Language != core.LanguageVBScript {
			continue
		}
		ranges = append(ranges, foldingRangesForRegion(doc, parsed.Text, region)...)
	}
	sort.SliceStable(ranges, func(i, j int) bool {
		if ranges[i].StartLine != ranges[j].StartLine {
			return ranges[i].StartLine < ranges[j].StartLine
		}
		return ranges[i].EndLine < ranges[j].EndLine
	})
	parsed.StoreAnalysis("vbscript.folding-ranges.v1", ranges)
	return ranges
}

func foldingRangesForRegion(doc *core.TextDocument, text string, region core.Region) []lsp.FoldingRange {
	lines := foldLinesForRegion(doc, text, region)
	var ranges []lsp.FoldingRange
	var ifStack []foldStart
	var doStack []foldStart
	var whileStack []foldStart
	var forStack []foldStart

	for index, line := range lines {
		switch {
		case isBlockIfStart(line.Text):
			ifStack = append(ifStack, foldStart{Line: line.Line, StartCharacter: line.StartCharacter})
		case isElseIf(line.Text) || isElse(line.Text):
			if len(ifStack) == 0 {
				continue
			}
			start := ifStack[len(ifStack)-1]
			endLine := line.Line - 1
			endCharacter := line.EndCharacter
			if index > 0 {
				endLine = lines[index-1].Line
				endCharacter = lines[index-1].EndCharacter
			}
			ranges = appendFoldRange(ranges, start, endLine, endCharacter)
			ifStack[len(ifStack)-1] = foldStart{Line: line.Line, StartCharacter: line.StartCharacter}
		case isEndIf(line.Text):
			if len(ifStack) == 0 {
				continue
			}
			start := ifStack[len(ifStack)-1]
			ifStack = ifStack[:len(ifStack)-1]
			ranges = appendFoldRange(ranges, start, line.Line, line.EndCharacter)
		case isDoStart(line.Text):
			doStack = append(doStack, foldStart{Line: line.Line, StartCharacter: line.StartCharacter})
		case isLoopEnd(line.Text):
			if len(doStack) == 0 {
				continue
			}
			start := doStack[len(doStack)-1]
			doStack = doStack[:len(doStack)-1]
			ranges = appendFoldRange(ranges, start, line.Line, line.EndCharacter)
		case isWhileStart(line.Text):
			whileStack = append(whileStack, foldStart{Line: line.Line, StartCharacter: line.StartCharacter})
		case isWendEnd(line.Text):
			if len(whileStack) == 0 {
				continue
			}
			start := whileStack[len(whileStack)-1]
			whileStack = whileStack[:len(whileStack)-1]
			ranges = appendFoldRange(ranges, start, line.Line, line.EndCharacter)
		case isForStart(line.Text):
			forStack = append(forStack, foldStart{Line: line.Line, StartCharacter: line.StartCharacter})
		case isNextEnd(line.Text):
			if len(forStack) == 0 {
				continue
			}
			start := forStack[len(forStack)-1]
			forStack = forStack[:len(forStack)-1]
			ranges = appendFoldRange(ranges, start, line.Line, line.EndCharacter)
		}
	}
	return ranges
}

func foldLinesForRegion(doc *core.TextDocument, text string, region core.Region) []foldLine {
	var lines []foldLine
	for offset := region.ContentStart; offset < region.ContentEnd; {
		lineStart := offset
		lineEnd := offset
		for lineEnd < region.ContentEnd && text[lineEnd] != '\n' && text[lineEnd] != '\r' {
			lineEnd++
		}
		next := lineEnd
		if next < region.ContentEnd && text[next] == '\r' {
			next++
			if next < region.ContentEnd && text[next] == '\n' {
				next++
			}
		} else if next < region.ContentEnd && text[next] == '\n' {
			next++
		}
		lineText := text[lineStart:lineEnd]
		trimmed := strings.TrimSpace(lineText)
		if trimmed != "" {
			leading := len(lineText) - len(strings.TrimLeft(lineText, " \t"))
			start := doc.PositionAt(lineStart + leading)
			end := doc.PositionAt(lineEnd)
			lines = append(lines, foldLine{
				Line:           start.Line,
				StartCharacter: start.Character,
				EndCharacter:   end.Character,
				Text:           trimmed,
			})
		}
		offset = next
	}
	return lines
}

func appendFoldRange(ranges []lsp.FoldingRange, start foldStart, endLine int, endCharacter int) []lsp.FoldingRange {
	if endLine <= start.Line {
		return ranges
	}
	return append(ranges, lsp.FoldingRange{
		StartLine:      start.Line,
		StartCharacter: start.StartCharacter,
		EndLine:        endLine,
		EndCharacter:   endCharacter,
	})
}

func isBlockIfStart(text string) bool {
	lower := strings.ToLower(text)
	return strings.HasPrefix(lower, "if ") && strings.HasSuffix(lower, " then")
}

func isElseIf(text string) bool {
	return strings.HasPrefix(strings.ToLower(text), "elseif ")
}

func isElse(text string) bool {
	return strings.EqualFold(text, "else")
}

func isEndIf(text string) bool {
	return strings.EqualFold(text, "end if")
}

func isDoStart(text string) bool {
	lower := strings.ToLower(text)
	return lower == "do" || strings.HasPrefix(lower, "do ")
}

func isLoopEnd(text string) bool {
	lower := strings.ToLower(text)
	return lower == "loop" || strings.HasPrefix(lower, "loop ")
}

func isWhileStart(text string) bool {
	return strings.HasPrefix(strings.ToLower(text), "while ")
}

func isWendEnd(text string) bool {
	return strings.EqualFold(text, "wend")
}

func isForStart(text string) bool {
	return strings.HasPrefix(strings.ToLower(text), "for ")
}

func isNextEnd(text string) bool {
	lower := strings.ToLower(text)
	return lower == "next" || strings.HasPrefix(lower, "next ")
}
