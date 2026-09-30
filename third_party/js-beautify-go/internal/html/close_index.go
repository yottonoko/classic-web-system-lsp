package html

import (
	"sort"
	"strings"
)

// simpleCloseIndex memoizes the tag scan behind findSimpleCloseSpan for one
// source text. Documents full of unclosed elements (for example table cells or
// list items without end tags) otherwise rescan the rest of the document for
// every open tag, which is quadratic in the document size.
type simpleCloseIndex struct {
	source   string
	built    bool
	starts   []int
	ends     []int
	names    []string
	opens    []bool
	partners []int
	literals map[string]closeLiteralSearch
}

type closeLiteralSearch struct {
	from  int
	index int
}

// literalIndex returns the absolute index of the first case-insensitive
// closeTag occurrence at or after start, reusing the previous answer while it
// still lies ahead of start.
func (x *simpleCloseIndex) literalIndex(start int, closeTag string) int {
	if cached, ok := x.literals[closeTag]; ok && start >= cached.from && (cached.index < 0 || cached.index >= start) {
		return cached.index
	}
	index := indexASCIIFold(x.source[start:], closeTag)
	if index >= 0 {
		index += start
	}
	if x.literals == nil {
		x.literals = map[string]closeLiteralSearch{}
	}
	x.literals[closeTag] = closeLiteralSearch{from: start, index: index}
	return index
}

// build tokenizes the source exactly like the scan loop in findSimpleCloseSpan
// and pairs every open tag with the close tag that loop would stop at.
func (x *simpleCloseIndex) build() {
	x.built = true
	source := x.source
	stacks := map[string][]int{}
	for offset := 0; offset < len(source); {
		nextTag := strings.IndexByte(source[offset:], '<')
		if nextTag < 0 {
			break
		}
		index := offset + nextTag
		tag, next := readHTMLTag(source, index)
		if tag == "" || next <= index {
			offset = index + 1
			continue
		}
		name := lowerASCII(tagName(tag))
		token := len(x.starts)
		open := false
		if name != "" {
			if isCloseTag(tag) {
				if stack := stacks[name]; len(stack) > 0 {
					x.partners[stack[len(stack)-1]] = token
					stacks[name] = stack[:len(stack)-1]
				}
			} else if !isSelfClosing(tag) {
				open = true
				stacks[name] = append(stacks[name], token)
			}
		}
		x.starts = append(x.starts, index)
		x.ends = append(x.ends, next)
		x.names = append(x.names, name)
		x.opens = append(x.opens, open)
		x.partners = append(x.partners, -1)
		offset = next
	}
}

// closeAfterOpen answers the scan for a position directly after an open tag
// named name. known is false when start is not such a position, in which case
// the caller must fall back to scanning.
func (x *simpleCloseIndex) closeAfterOpen(start int, name string) (closeStart int, closeEnd int, found bool, known bool) {
	if !x.built {
		x.build()
	}
	token := sort.SearchInts(x.starts, start) - 1
	if token < 0 || x.ends[token] != start || !x.opens[token] || x.names[token] != name {
		return 0, 0, false, false
	}
	partner := x.partners[token]
	if partner < 0 {
		return 0, 0, false, true
	}
	return x.starts[partner], x.ends[partner], true, true
}

// findSimpleCloseSpan is findSimpleCloseSpan(source[start:], closeTag) for the
// beautifier's own source, backed by the memoized tag index.
func (b *Beautifier) findSimpleCloseSpan(source string, start int, closeTag string) (int, int, bool) {
	name := lowerASCII(tagName(closeTag))
	if name == "" || strings.HasPrefix(name, "{{") {
		return findSimpleCloseSpan(source[start:], closeTag)
	}
	if b.closeIndex == nil || b.closeIndex.source != source {
		b.closeIndex = &simpleCloseIndex{source: source}
	}
	if index := b.closeIndex.literalIndex(start, closeTag); index >= 0 &&
		!hasOpenTagNameBefore(source[start:index], name) &&
		!hasCloseTagNameBefore(source[start:index], name) {
		return index - start, index - start + len(closeTag), true
	}
	closeStart, closeEnd, found, known := b.closeIndex.closeAfterOpen(start, name)
	if !known {
		return findSimpleCloseSpan(source[start:], closeTag)
	}
	if !found {
		return -1, -1, false
	}
	return closeStart - start, closeEnd - start, true
}
