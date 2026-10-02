package lspserver

import (
	"regexp"
	"slices"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

const graphVBArrayIndexRuntimeKey = "lspserver.graph-vb-array-index.runtime.v1"

// graphVBArrayIndex answers graphVBArrayInfo for every declaration of one
// document. graphVBArrayInfo compiles two patterns and scans the whole
// document per declaration, which is quadratic in the size of a large
// include. The index records each ReDim and Dim statement window and the
// words in it that are followed by "(", so a name runs the same patterns only
// on the windows where they can match.
type graphVBArrayIndex struct {
	text   string
	redims graphVBKeywordWindows
	dims   graphVBKeywordWindows
}

type graphVBKeywordWindows struct {
	windows []graphVBKeywordWindow
	// byWord maps a lowercase word followed by "(" to the windows holding it.
	byWord map[string][]int
	// folded lists windows holding U+017F or U+212A, the only non-ASCII
	// characters that (?i) matches against ASCII names, which the word scan
	// does not see.
	folded []int
}

// graphVBKeywordWindow is the text in which a pattern starting at keyword
// can match: one character of left context, the keyword, and everything up
// to the first ")" the pattern can reach.
type graphVBKeywordWindow struct {
	start   int
	keyword int
	end     int
}

func graphVBArrayInfoForDocument(parsed *core.ParsedDocument, name string) (string, []string) {
	if parsed == nil || name == "" {
		return "", nil
	}
	if !graphVBPlainIdentifier(name) {
		return graphVBArrayInfo(parsed.Text, name)
	}
	var index *graphVBArrayIndex
	if value, ok := parsed.LoadRuntimeAnalysis(graphVBArrayIndexRuntimeKey); ok {
		index, _ = value.(*graphVBArrayIndex)
	}
	if index == nil || index.text != parsed.Text {
		index = newGraphVBArrayIndex(parsed.Text)
		parsed.StoreRuntimeAnalysis(graphVBArrayIndexRuntimeKey, index)
	}
	return index.info(name)
}

func newGraphVBArrayIndex(text string) *graphVBArrayIndex {
	index := &graphVBArrayIndex{text: text}
	for position := 0; position < len(text); position++ {
		if position > 0 && graphVBASCIIWordByte(text[position-1]) {
			continue
		}
		switch {
		case graphVBKeywordAt(text, position, "redim"):
			// ReDim [Preserve] name(...): only whitespace and words precede the
			// opening parenthesis, so the first ")" closes the match, and the
			// name is the first word, or the second after Preserve.
			end := graphVBWindowEnd(text, position+len("redim"))
			nameStart := position + len("redim")
			for nameStart < end && graphVBRE2Space(text[nameStart]) {
				nameStart++
			}
			if first, next := graphVBNextWord(text, nameStart, end); strings.EqualFold(first, "preserve") {
				nameStart = next
			}
			index.redims.add(text, position, end, graphVBCallWords(text, nameStart, nameStart+1, end))
		case graphVBKeywordAt(text, position, "dim") && (position+3 == len(text) || !graphVBASCIIWordByte(text[position+3])):
			// Dim ... name(...): the name starts before the end of the line or
			// statement, and its parentheses close at the first ")" after that.
			lineEnd := position + 3
			for lineEnd < len(text) && text[lineEnd] != '\r' && text[lineEnd] != '\n' && text[lineEnd] != ':' {
				lineEnd++
			}
			end := graphVBWindowEnd(text, lineEnd)
			index.dims.add(text, position, end, graphVBCallWords(text, position+3, lineEnd+1, end))
		}
	}
	return index
}

func (index *graphVBArrayIndex) info(name string) (string, []string) {
	if match, ok := index.redims.find(index.text, name, `(?i)\bReDim(?:\s+Preserve)?\s+`+regexp.QuoteMeta(name)+`\s*\(([^)]*)\)`); ok {
		return "dynamic", graphArrayDimensions(match)
	}
	if match, ok := index.dims.find(index.text, name, `(?i)\bDim\b[^\r\n:]*\b`+regexp.QuoteMeta(name)+`\s*\(([^)]*)\)`); ok {
		dimensions := graphArrayDimensions(match)
		if len(dimensions) == 0 {
			return "dynamic", []string{}
		}
		return "fixed", dimensions
	}
	return "", nil
}

func (w *graphVBKeywordWindows) add(text string, keyword, end int, words []string) {
	start := max(keyword-1, 0)
	index := len(w.windows)
	w.windows = append(w.windows, graphVBKeywordWindow{start: start, keyword: keyword, end: end})
	if window := text[start:end]; strings.Contains(window, "\u017F") || strings.Contains(window, "\u212A") {
		w.folded = append(w.folded, index)
	}
	for _, word := range words {
		if w.byWord == nil {
			w.byWord = map[string][]int{}
		}
		if indexes := w.byWord[word]; len(indexes) == 0 || indexes[len(indexes)-1] != index {
			w.byWord[word] = append(indexes, index)
		}
	}
}

// graphVBCallWords returns the lowercase ASCII words that start in
// [from, limit) and are followed by optional whitespace and "(" before end.
func graphVBCallWords(text string, from, limit, end int) []string {
	var words []string
	for position := from; position < limit && position < end; {
		if !graphVBASCIIWordByte(text[position]) || position > 0 && graphVBASCIIWordByte(text[position-1]) {
			position++
			continue
		}
		wordStart := position
		for position < end && graphVBASCIIWordByte(text[position]) {
			position++
		}
		next := position
		for next < end && graphVBRE2Space(text[next]) {
			next++
		}
		if next < end && text[next] == '(' {
			words = append(words, strings.ToLower(text[wordStart:position]))
		}
	}
	return words
}

// graphVBNextWord returns the word at from and the position after the
// whitespace that follows it.
func graphVBNextWord(text string, from, end int) (string, int) {
	position := from
	wordStart := position
	for position < end && graphVBASCIIWordByte(text[position]) {
		position++
	}
	next := position
	for next < end && graphVBRE2Space(text[next]) {
		next++
	}
	return text[wordStart:position], next
}

// find returns the first capture of pattern, which must start with the
// windows' keyword, in text order, as regexp.FindStringSubmatch on the whole
// text would.
func (w *graphVBKeywordWindows) find(text, name, pattern string) (string, bool) {
	candidates := w.byWord[strings.ToLower(name)]
	if len(w.folded) > 0 {
		candidates = append(slices.Clone(candidates), w.folded...)
		slices.Sort(candidates)
		candidates = slices.Compact(candidates)
	}
	if len(candidates) == 0 {
		return "", false
	}
	compiled := regexp.MustCompile(pattern)
	for _, candidate := range candidates {
		window := w.windows[candidate]
		// A match that starts at a later keyword in this window is found again
		// from that keyword's own window, which holds all of it.
		match := compiled.FindStringSubmatchIndex(text[window.start:window.end])
		if len(match) == 4 && match[0] == window.keyword-window.start {
			return text[window.start+match[2] : window.start+match[3]], true
		}
	}
	return "", false
}

func graphVBWindowEnd(text string, from int) int {
	if closing := strings.IndexByte(text[from:], ')'); closing >= 0 {
		return from + closing + 1
	}
	return len(text)
}

func graphVBKeywordAt(text string, position int, keyword string) bool {
	return len(text)-position >= len(keyword) && strings.EqualFold(text[position:position+len(keyword)], keyword)
}

func graphVBPlainIdentifier(name string) bool {
	for index := 0; index < len(name); index++ {
		if !graphVBASCIIWordByte(name[index]) {
			return false
		}
	}
	return name != ""
}

// graphVBASCIIWordByte matches RE2's \b word characters.
func graphVBASCIIWordByte(character byte) bool {
	return character >= '0' && character <= '9' || character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z' || character == '_'
}

// graphVBRE2Space matches RE2's \s.
func graphVBRE2Space(character byte) bool {
	return character == ' ' || character == '\t' || character == '\n' || character == '\f' || character == '\r'
}
