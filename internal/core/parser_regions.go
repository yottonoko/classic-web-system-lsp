package core

import (
	"regexp"
	"sort"
	"strings"
	"sync"
	"unsafe"

	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func indexASPOpen(text string, cursor int) int {
	return indexASPOpenWithFeatures(text, cursor, aspOpenScanFeatures{
		hasHTMLComments:    true,
		hasRawTextElements: true,
		hasScriptElements:  true,
		hasClientComments:  true,
	})
}

type aspOpenScanFeatures struct {
	hasHTMLComments    bool
	hasRawTextElements bool
	hasScriptElements  bool
	hasClientComments  bool
	// quotes carries line quote state between monotonically increasing opens
	// so long single-line documents are not rescanned from the line start.
	quotes *lineQuoteScanner
}

func newASPOpenScanFeatures(text string) aspOpenScanFeatures {
	features := aspOpenScanFeatures{
		hasHTMLComments:   strings.Contains(text, "<!--"),
		hasClientComments: strings.Contains(text, "//") || strings.Contains(text, "/*"),
	}
	for offset := 0; offset < len(text); offset++ {
		if text[offset] != '<' {
			continue
		}
		rest := text[offset:]
		if !features.hasScriptElements && hasTagOpenFold(rest, "<script") {
			features.hasScriptElements = true
		}
		if !features.hasRawTextElements && hasAnyTagOpenFold(rest, rawTextElementOpens) {
			features.hasRawTextElements = true
		}
		if features.hasScriptElements && features.hasRawTextElements {
			break
		}
	}
	return features
}

// rawTextElementOpens are `<`-prefixed tag names detected case-insensitively.
var rawTextElementOpens = []string{"<textarea", "<title", "<xmp", "<iframe", "<noembed", "<noframes", "<plaintext"}

// hasTagOpenFold reports whether text starts with the lowercase needle,
// folding ASCII uppercase to lowercase without allocating.
func hasTagOpenFold(text, needle string) bool {
	if len(text) < len(needle) {
		return false
	}
	return asciiEqualFold(text[:len(needle)], needle)
}

func hasAnyTagOpenFold(text string, needles []string) bool {
	for _, needle := range needles {
		if hasTagOpenFold(text, needle) {
			return true
		}
	}
	return false
}

func indexASPOpenWithFeatures(text string, cursor int, features aspOpenScanFeatures) int {
	scanStart := cursor
	for cursor < len(text) {
		open := strings.Index(text[cursor:], "<%")
		if open == -1 {
			return -1
		}
		open += cursor
		javascriptLiteralOutput := aspOutputExpressionInJavaScriptLiteral(text, open)
		if (!features.hasHTMLComments && !features.hasRawTextElements || !aspOpenLooksLikeHTMLFalseRegion(text, open) || javascriptLiteralOutput) &&
			(!aspOpenLooksLikeQuotedLiteralWithScanner(text, open, features.quotes) || javascriptLiteralOutput) &&
			(!features.hasScriptElements || !aspOpenLooksLikeJSTemplateLiteral(text, open) || javascriptLiteralOutput) &&
			(!features.hasClientComments || !aspOpenLooksLikeClientCommentSince(text, open, scanStart)) {
			return open
		}
		cursor = open + 2
	}
	return -1
}

func aspOpenIsOutputExpression(text string, open int) bool {
	return open >= 0 && open+2 < len(text) && text[open+2] == '='
}

func aspOutputExpressionInJavaScriptLiteral(text string, open int) bool {
	if !aspOpenIsOutputExpression(text, open) {
		return false
	}
	candidate, ok := htmlASPOpenCandidate(text, open)
	return ok && candidate.context == htmlScanScript && (candidate.quotedLiteral || candidate.jsTemplate)
}

func aspCloseDelimiter(text string, open int) int {
	if candidate, ok := htmlASPOpenCandidate(text, open); ok && candidate.valid {
		return candidate.close
	}
	if candidate, ok := htmlASPOpenCandidate(text, open); ok && candidate.falseRegion {
		// Callers normally filter false candidates before asking for a close. Keep
		// the standalone helper's historical delimiter behavior for direct users.
		return aspCloseDelimiterFallback(text, open, candidate.inAttribute)
	}
	return aspCloseDelimiterFallback(text, open, aspOpenInsideHTMLAttribute(text, open))
}

func aspCloseDelimiterFallback(text string, open int, inAttribute bool) int {
	contentStart := open + 2
	if !inAttribute {
		closeRel := strings.Index(text[contentStart:], "%>")
		if closeRel >= 0 {
			return contentStart + closeRel
		}
		return -1
	}
	var quote byte
	for i := contentStart; i+1 < len(text); i++ {
		ch := text[i]
		if quote != 0 {
			if ch == quote && !isEscaped(text, i) {
				quote = 0
			}
			continue
		}
		if ch == '"' || ch == '\'' {
			quote = ch
			continue
		}
		if ch == '%' && text[i+1] == '>' {
			return i
		}
	}
	return -1
}

func aspOpenInsideHTMLAttribute(text string, open int) bool {
	candidate, ok := htmlASPOpenCandidate(text, open)
	if ok {
		return candidate.inAttribute
	}
	tagStart := htmlOpeningTagStartBefore(text, open)
	if tagStart < 0 {
		return false
	}
	var quote byte
	for cursor := tagStart + 1; cursor < open; cursor++ {
		ch := text[cursor]
		if quote != 0 {
			if ch == quote && !isEscaped(text, cursor) {
				quote = 0
			}
			continue
		}
		if ch == '"' || ch == '\'' {
			quote = ch
		}
	}
	return quote != 0
}

// htmlOpeningTagStartBefore returns the last opening-tag start whose closing
// delimiter has not been seen before offset.  Unlike a line-local search this
// preserves HTML attribute state across line breaks and ignores delimiters
// inside quoted attribute values.
func htmlOpeningTagStartBefore(text string, offset int) int {
	if candidate, ok := htmlASPOpenCandidate(text, offset); ok {
		return candidate.tagStart
	}
	index := htmlTagScanIndexFor(text)
	position := sort.Search(len(index.tags), func(i int) bool {
		return index.tags[i].start > offset
	}) - 1
	if position >= 0 && offset < index.tags[position].end {
		return index.tags[position].start
	}
	return -1
}

type htmlTagScanCacheKey struct {
	data uintptr
	len  int
}

type htmlTagScanTag struct {
	start int
	end   int
}

type htmlTagScanCandidate struct {
	open          int
	close         int
	tagStart      int
	context       htmlScanMode
	inAttribute   bool
	falseRegion   bool
	quotedLiteral bool
	jsTemplate    bool
	clientComment bool
	valid         bool
}

type htmlTagScanIndex struct {
	text       string
	tags       []htmlTagScanTag
	candidates []htmlTagScanCandidate
}

type htmlTagScanCacheEntry struct {
	key   htmlTagScanCacheKey
	index *htmlTagScanIndex
}

const maxHTMLTagScanCacheEntries = 8

var htmlTagScanCache struct {
	sync.Mutex
	entries []htmlTagScanCacheEntry
}

// htmlTagScanIndexFor builds the HTML/embedded lexical index once per source.
// Candidate lookups then share the same comment, raw-text, tag, and attribute
// state instead of rescanning a prefix for each ASP opener.
func htmlTagScanIndexFor(text string) *htmlTagScanIndex {
	key := htmlTagScanCacheKey{len: len(text)}
	if len(text) > 0 {
		key.data = uintptr(unsafe.Pointer(unsafe.StringData(text)))
	}
	htmlTagScanCache.Lock()
	for index := range htmlTagScanCache.entries {
		entry := &htmlTagScanCache.entries[index]
		if entry.key == key {
			result := entry.index
			htmlTagScanCache.Unlock()
			return result
		}
	}
	htmlTagScanCache.Unlock()

	// Build outside the cache lock so an expensive source scan does not block
	// unrelated documents. A second lookup below deduplicates concurrent builds
	// for the same source.
	built := buildHTMLTagScanIndex(text)
	htmlTagScanCache.Lock()
	for index := range htmlTagScanCache.entries {
		entry := &htmlTagScanCache.entries[index]
		if entry.key == key {
			result := entry.index
			htmlTagScanCache.Unlock()
			return result
		}
	}
	if len(htmlTagScanCache.entries) >= maxHTMLTagScanCacheEntries {
		htmlTagScanCache.entries[0] = htmlTagScanCacheEntry{}
		entry := &htmlTagScanCache.entries[0]
		entry.key = key
		entry.index = built
		result := entry.index
		htmlTagScanCache.Unlock()
		return result
	} else {
		htmlTagScanCache.entries = append(htmlTagScanCache.entries, htmlTagScanCacheEntry{})
	}
	entry := &htmlTagScanCache.entries[len(htmlTagScanCache.entries)-1]
	entry.key = key
	entry.index = built
	result := entry.index
	htmlTagScanCache.Unlock()
	return result
}

func htmlASPOpenCandidate(text string, open int) (htmlTagScanCandidate, bool) {
	index := htmlTagScanIndexFor(text)
	position := sort.Search(len(index.candidates), func(i int) bool {
		return index.candidates[i].open >= open
	})
	if position < len(index.candidates) && index.candidates[position].open == open {
		return index.candidates[position], true
	}
	return htmlTagScanCandidate{}, false
}

func buildHTMLTagScanIndex(text string) *htmlTagScanIndex {
	scanner := htmlTagScanner{
		text:       text,
		index:      &htmlTagScanIndex{text: text},
		mode:       htmlScanData,
		commentEnd: -1,
	}
	scanner.scan()
	return scanner.index
}

type htmlScanMode uint8

const (
	htmlScanData htmlScanMode = iota
	htmlScanTag
	htmlScanComment
	htmlScanRawText
	htmlScanPlaintext
	htmlScanScript
	htmlScanStyle
)

const (
	jsScanNormal = iota
	jsScanSingleQuote
	jsScanDoubleQuote
	jsScanLineComment
	jsScanBlockComment
	jsScanTemplate
	jsScanTemplateExpression
	jsScanRegex
	jsScanRegexClass
)

const (
	cssScanNormal = iota
	cssScanSingleQuote
	cssScanDoubleQuote
	cssScanComment
)

type htmlJavaScriptScanState struct {
	stack         []int
	braceDepth    []int
	canStartRegex bool
}

type htmlCSSScanState struct {
	state int
}

type htmlTagScanner struct {
	text         string
	index        *htmlTagScanIndex
	mode         htmlScanMode
	cursor       int
	commentEnd   int
	commentQuote byte
	commentLine  bool
	commentBlock bool
	tagStart     int
	tagName      string
	tagQuote     byte
	rawTag       string
	javascript   htmlJavaScriptScanState
	css          htmlCSSScanState
}

func (scanner *htmlTagScanner) scan() {
	for scanner.cursor < len(scanner.text) {
		switch scanner.mode {
		case htmlScanData:
			scanner.scanData()
		case htmlScanTag:
			scanner.scanTag()
		case htmlScanComment:
			scanner.scanComment()
		case htmlScanRawText, htmlScanPlaintext:
			scanner.scanRawText()
		case htmlScanScript:
			scanner.scanScript()
		case htmlScanStyle:
			scanner.scanStyle()
		}
	}
	if scanner.mode == htmlScanTag {
		scanner.index.tags = append(scanner.index.tags, htmlTagScanTag{start: scanner.tagStart, end: len(scanner.text)})
	}
}

func (scanner *htmlTagScanner) scanData() {
	if scanner.text[scanner.cursor] != '<' {
		scanner.cursor++
		return
	}
	if strings.HasPrefix(scanner.text[scanner.cursor:], "<!--") {
		scanner.mode = htmlScanComment
		scanner.cursor += len("<!--")
		scanner.commentQuote = 0
		scanner.commentLine = false
		scanner.commentBlock = false
		scanner.commentEnd = htmlCommentEndAt(scanner.text, scanner.cursor)
		return
	}
	if strings.HasPrefix(scanner.text[scanner.cursor:], "<%") {
		scanner.scanASPOpen(false)
		return
	}
	if scanner.cursor+1 < len(scanner.text) && isHTMLTagNameStartByte(scanner.text[scanner.cursor+1]) {
		scanner.tagStart = scanner.cursor
		scanner.cursor += 1
		nameStart := scanner.cursor
		for scanner.cursor < len(scanner.text) && isHTMLTagNameByte(scanner.text[scanner.cursor]) {
			scanner.cursor++
		}
		scanner.tagName = scanner.text[nameStart:scanner.cursor]
		scanner.tagQuote = 0
		scanner.mode = htmlScanTag
		return
	}
	scanner.cursor++
}

func (scanner *htmlTagScanner) scanTag() {
	if strings.HasPrefix(scanner.text[scanner.cursor:], "<%") {
		scanner.scanASPOpen(true)
		return
	}
	if scanner.tagQuote != 0 {
		if scanner.text[scanner.cursor] == scanner.tagQuote && !isEscaped(scanner.text, scanner.cursor) {
			scanner.tagQuote = 0
		}
		scanner.cursor++
		return
	}
	switch scanner.text[scanner.cursor] {
	case '"', '\'':
		scanner.tagQuote = scanner.text[scanner.cursor]
		scanner.cursor++
	case '>':
		tagEnd := scanner.cursor + 1
		scanner.index.tags = append(scanner.index.tags, htmlTagScanTag{start: scanner.tagStart, end: tagEnd})
		scanner.cursor = tagEnd
		scanner.enterTagContent()
	default:
		scanner.cursor++
	}
}

func (scanner *htmlTagScanner) enterTagContent() {
	switch strings.ToLower(scanner.tagName) {
	case "plaintext":
		scanner.mode = htmlScanPlaintext
	case "script":
		scanner.mode = htmlScanScript
		scanner.javascript = htmlJavaScriptScanState{
			stack:         []int{jsScanNormal},
			canStartRegex: true,
		}
	case "style":
		scanner.mode = htmlScanStyle
		scanner.css = htmlCSSScanState{state: cssScanNormal}
	default:
		if isHTMLFalseRawTextTag(scanner.tagName) {
			scanner.mode = htmlScanRawText
			scanner.rawTag = strings.ToLower(scanner.tagName)
		} else {
			scanner.mode = htmlScanData
		}
	}
}

func (scanner *htmlTagScanner) scanComment() {
	if scanner.commentEnd >= 0 && scanner.cursor >= scanner.commentEnd {
		scanner.cursor = scanner.commentEnd + len("-->")
		scanner.mode = htmlScanData
		scanner.commentEnd = -1
		scanner.commentQuote = 0
		scanner.commentLine = false
		scanner.commentBlock = false
		return
	}
	if strings.HasPrefix(scanner.text[scanner.cursor:], "<%") {
		scanner.scanASPOpen(false)
		return
	}
	end := scanner.cursor + 1
	if end < len(scanner.text) && !strings.HasPrefix(scanner.text[end:], "<%") {
		end++
	}
	scanner.advanceHTMLCommentState(scanner.cursor, end, false)
}

func (scanner *htmlTagScanner) scanRawText() {
	if scanner.mode == htmlScanRawText && htmlClosingTagEndAt(scanner.text, scanner.cursor, scanner.rawTag, len(scanner.text)) >= 0 {
		end := htmlClosingTagEndAt(scanner.text, scanner.cursor, scanner.rawTag, len(scanner.text))
		scanner.cursor = end
		scanner.mode = htmlScanData
		scanner.rawTag = ""
		return
	}
	if strings.HasPrefix(scanner.text[scanner.cursor:], "<%") {
		scanner.appendCandidate(htmlTagScanCandidate{
			open:        scanner.cursor,
			close:       -1,
			context:     scanner.mode,
			falseRegion: true,
			valid:       false,
		})
		scanner.cursor += 2
		return
	}
	scanner.cursor++
}

func (scanner *htmlTagScanner) scanScript() {
	if closeEnd := htmlClosingTagEndAt(scanner.text, scanner.cursor, "script", len(scanner.text)); closeEnd >= 0 {
		scanner.cursor = closeEnd
		scanner.mode = htmlScanData
		return
	}
	if strings.HasPrefix(scanner.text[scanner.cursor:], "<%") {
		state := scanner.javascript.stack[len(scanner.javascript.stack)-1]
		quotedLiteral := false
		if state == jsScanSingleQuote || state == jsScanDoubleQuote {
			quote := byte('\'')
			if state == jsScanDoubleQuote {
				quote = '"'
			}
			quotedLiteral = aspOpenQuotedLiteralAt(scanner.text, scanner.cursor, quote)
		}
		falseRegion := state == jsScanLineComment || state == jsScanBlockComment || state == jsScanTemplate || quotedLiteral
		scanner.scanASPOpenWithFlags(false, falseRegion, state == jsScanTemplate, state == jsScanLineComment || state == jsScanBlockComment, quotedLiteral)
		return
	}
	scanner.advanceJavaScript()
}

func (scanner *htmlTagScanner) scanStyle() {
	if closeEnd := htmlClosingTagEndAt(scanner.text, scanner.cursor, "style", len(scanner.text)); closeEnd >= 0 {
		scanner.cursor = closeEnd
		scanner.mode = htmlScanData
		return
	}
	if strings.HasPrefix(scanner.text[scanner.cursor:], "<%") {
		state := scanner.css.state
		quotedLiteral := false
		if state == cssScanSingleQuote || state == cssScanDoubleQuote {
			quote := byte('\'')
			if state == cssScanDoubleQuote {
				quote = '"'
			}
			quotedLiteral = aspOpenQuotedLiteralAt(scanner.text, scanner.cursor, quote)
		}
		falseRegion := state == cssScanComment || quotedLiteral
		scanner.scanASPOpenWithFlags(false, falseRegion, false, state == cssScanComment, quotedLiteral)
		return
	}
	scanner.advanceCSS()
}

func (scanner *htmlTagScanner) scanASPOpen(inAttribute bool) {
	scanner.scanASPOpenWithFlags(inAttribute, false, false, false, false)
}

func (scanner *htmlTagScanner) scanASPOpenWithFlags(inAttribute, falseRegion, jsTemplate, clientComment, quotedLiteral bool) {
	open := scanner.cursor
	if scanner.mode == htmlScanComment {
		close, commentEnd, valid := scanHTMLCommentASPClose(scanner.text, open, scanner.commentEnd)
		quotedLiteral := false
		if scanner.commentQuote != 0 {
			quotedLiteral = aspOpenQuotedLiteralAt(scanner.text, open, scanner.commentQuote)
		}
		candidate := htmlTagScanCandidate{
			open:          open,
			close:         close,
			context:       htmlScanComment,
			falseRegion:   !valid,
			quotedLiteral: quotedLiteral,
			clientComment: scanner.commentLine || scanner.commentBlock,
			valid:         valid,
		}
		scanner.appendCandidate(candidate)
		if valid {
			scanner.advanceHTMLCommentState(open+2, close+2, true)
			// Lexical state cannot cross an ASP island boundary into HTML comment
			// prose. Client comments in the prose are tracked separately below.
			scanner.commentQuote = 0
			scanner.commentLine = false
			scanner.commentBlock = false
			scanner.cursor = close + 2
		} else {
			scanner.cursor = commentEnd
			scanner.mode = htmlScanData
			scanner.commentEnd = -1
			scanner.commentQuote = 0
			scanner.commentLine = false
			scanner.commentBlock = false
		}
		return
	}
	candidate := htmlTagScanCandidate{
		open:          open,
		close:         -1,
		tagStart:      scanner.tagStart,
		context:       scanner.mode,
		inAttribute:   inAttribute && scanner.tagQuote != 0,
		falseRegion:   falseRegion,
		quotedLiteral: quotedLiteral,
		jsTemplate:    jsTemplate,
		clientComment: clientComment,
		valid:         !falseRegion,
	}
	if candidate.valid {
		candidate.close = aspCloseDelimiterFallback(scanner.text, open, candidate.inAttribute)
	}
	scanner.appendCandidate(candidate)
	if !candidate.valid {
		scanner.cursor += 2
		return
	}
	if candidate.close < 0 {
		scanner.cursor = len(scanner.text)
		return
	}
	scanner.cursor = candidate.close + 2
}

func (scanner *htmlTagScanner) appendCandidate(candidate htmlTagScanCandidate) {
	scanner.index.candidates = append(scanner.index.candidates, candidate)
}

func (scanner *htmlTagScanner) advanceHTMLCommentState(start, end int, trackQuotes bool) {
	for scanner.cursor = start; scanner.cursor < end; {
		ch := scanner.text[scanner.cursor]
		if scanner.commentLine {
			if ch == '\r' || ch == '\n' {
				scanner.commentLine = false
				scanner.commentQuote = 0
			}
			scanner.cursor++
			continue
		}
		if trackQuotes && scanner.commentQuote != 0 {
			if ch == '\\' {
				if scanner.commentByteAvailable(scanner.cursor + 1) {
					scanner.cursor += 2
				} else {
					scanner.cursor++
				}
				continue
			}
			if ch == scanner.commentQuote {
				if scanner.commentQuote == '"' && scanner.commentByteAvailable(scanner.cursor+1) && scanner.text[scanner.cursor+1] == '"' {
					scanner.cursor += 2
					continue
				}
				scanner.commentQuote = 0
			}
			if ch == '\r' || ch == '\n' {
				scanner.commentQuote = 0
			}
			scanner.cursor++
			continue
		}
		if scanner.commentBlock {
			if ch == '*' && scanner.commentByteAvailable(scanner.cursor+1) && scanner.text[scanner.cursor+1] == '/' {
				scanner.commentBlock = false
				scanner.cursor += 2
				continue
			}
			scanner.cursor++
			continue
		}
		if ch == '/' && scanner.commentByteAvailable(scanner.cursor+1) {
			switch scanner.text[scanner.cursor+1] {
			case '/':
				scanner.commentLine = true
				scanner.cursor += 2
				continue
			case '*':
				scanner.commentBlock = true
				scanner.cursor += 2
				continue
			}
		}
		switch ch {
		case '"', '`':
			if trackQuotes {
				scanner.commentQuote = ch
			}
		case '\r', '\n':
			scanner.commentQuote = 0
		}
		scanner.cursor++
	}
}

func (scanner *htmlTagScanner) commentByteAvailable(offset int) bool {
	return offset < len(scanner.text) && (scanner.commentEnd < 0 || offset < scanner.commentEnd)
}

func scanHTMLCommentASPClose(text string, open, commentBoundary int) (close, commentEnd int, valid bool) {
	limit := len(text)
	if commentBoundary >= 0 && commentBoundary < limit {
		limit = commentBoundary
	}
	var quote byte
	for cursor := open + 2; cursor < limit; cursor++ {
		if quote != 0 {
			if text[cursor] == '\\' {
				cursor++
				continue
			}
			if text[cursor] == quote {
				if quote == '"' && cursor+1 < len(text) && text[cursor+1] == '"' {
					cursor++
					continue
				}
				quote = 0
			}
			continue
		}
		switch text[cursor] {
		case '"', '`':
			quote = text[cursor]
			continue
		}
		if cursor+len("-->") <= limit && strings.HasPrefix(text[cursor:], "-->") {
			return -1, cursor + len("-->"), false
		}
		if cursor+len("%>") <= limit && strings.HasPrefix(text[cursor:], "%>") {
			if commentBoundary >= 0 {
				return cursor, 0, true
			}
			return -1, len(text), false
		}
	}
	if commentBoundary >= 0 {
		return -1, commentBoundary + len("-->"), false
	}
	return -1, len(text), false
}

func htmlCommentEndAt(text string, start int) int {
	for cursor := start; cursor < len(text); cursor++ {
		if strings.HasPrefix(text[cursor:], "-->") {
			return cursor
		}
		if !strings.HasPrefix(text[cursor:], "<%") {
			continue
		}
		// Only an ASP island gets lexical quote handling here. HTML comment
		// prose is opaque text, so an ordinary quote cannot hide its terminator.
		close, commentEnd, valid := scanHTMLCommentASPCloseToEOF(text, cursor)
		if commentEnd >= 0 {
			return commentEnd
		}
		if !valid {
			return -1
		}
		cursor = close + 1
	}
	return -1
}

func scanHTMLCommentASPCloseToEOF(text string, open int) (close, commentEnd int, valid bool) {
	var quote byte
	for cursor := open + 2; cursor < len(text); cursor++ {
		if quote != 0 {
			if text[cursor] == '\\' {
				cursor++
				continue
			}
			if text[cursor] == quote {
				if quote == '"' && cursor+1 < len(text) && text[cursor+1] == '"' {
					cursor++
					continue
				}
				quote = 0
			}
			continue
		}
		switch text[cursor] {
		case '"', '`':
			quote = text[cursor]
			continue
		}
		if strings.HasPrefix(text[cursor:], "-->") {
			return -1, cursor, false
		}
		if strings.HasPrefix(text[cursor:], "%>") {
			return cursor, -1, true
		}
	}
	return -1, -1, false
}

func (scanner *htmlTagScanner) advanceJavaScript() {
	state := scanner.javascript.stack[len(scanner.javascript.stack)-1]
	ch := scanner.text[scanner.cursor]
	switch state {
	case jsScanLineComment:
		if ch == '\r' || ch == '\n' {
			scanner.javascript.stack = scanner.javascript.stack[:len(scanner.javascript.stack)-1]
		}
		scanner.cursor++
		return
	case jsScanBlockComment:
		if ch == '*' && scanner.cursor+1 < len(scanner.text) && scanner.text[scanner.cursor+1] == '/' {
			scanner.javascript.stack = scanner.javascript.stack[:len(scanner.javascript.stack)-1]
			scanner.cursor += 2
			return
		}
		scanner.cursor++
		return
	case jsScanSingleQuote, jsScanDoubleQuote:
		if ch == '\\' {
			scanner.cursor += 1
			if scanner.cursor < len(scanner.text) {
				scanner.cursor++
			}
			return
		}
		quote := byte('\'')
		if state == jsScanDoubleQuote {
			quote = '"'
		}
		if ch == quote || ch == '\r' || ch == '\n' {
			scanner.javascript.stack = scanner.javascript.stack[:len(scanner.javascript.stack)-1]
		}
		scanner.cursor++
		return
	case jsScanTemplate:
		if ch == '\\' {
			scanner.cursor += 1
			if scanner.cursor < len(scanner.text) {
				scanner.cursor++
			}
			return
		}
		if ch == '`' {
			scanner.javascript.stack = scanner.javascript.stack[:len(scanner.javascript.stack)-1]
			scanner.cursor++
			return
		}
		if ch == '$' && scanner.cursor+1 < len(scanner.text) && scanner.text[scanner.cursor+1] == '{' {
			scanner.javascript.stack = append(scanner.javascript.stack, jsScanTemplateExpression)
			scanner.javascript.braceDepth = append(scanner.javascript.braceDepth, 1)
			scanner.cursor += 2
			return
		}
		scanner.cursor++
		return
	case jsScanRegex:
		if ch == '\\' {
			scanner.cursor += 1
			if scanner.cursor < len(scanner.text) {
				scanner.cursor++
			}
			return
		}
		if ch == '[' {
			scanner.javascript.stack[len(scanner.javascript.stack)-1] = jsScanRegexClass
		} else if ch == '/' {
			scanner.javascript.stack = scanner.javascript.stack[:len(scanner.javascript.stack)-1]
			scanner.javascript.canStartRegex = false
		}
		scanner.cursor++
		return
	case jsScanRegexClass:
		if ch == '\\' {
			scanner.cursor += 1
			if scanner.cursor < len(scanner.text) {
				scanner.cursor++
			}
			return
		}
		if ch == ']' {
			scanner.javascript.stack[len(scanner.javascript.stack)-1] = jsScanRegex
		}
		scanner.cursor++
		return
	}

	if ch == '\\' {
		scanner.cursor++
		return
	}
	if ch == '\'' {
		scanner.javascript.stack = append(scanner.javascript.stack, jsScanSingleQuote)
		scanner.cursor++
		return
	}
	if ch == '"' {
		scanner.javascript.stack = append(scanner.javascript.stack, jsScanDoubleQuote)
		scanner.cursor++
		return
	}
	if ch == '`' {
		scanner.javascript.stack = append(scanner.javascript.stack, jsScanTemplate)
		scanner.cursor++
		return
	}
	if ch == '/' && scanner.cursor+1 < len(scanner.text) {
		switch scanner.text[scanner.cursor+1] {
		case '/':
			scanner.javascript.stack = append(scanner.javascript.stack, jsScanLineComment)
			scanner.cursor += 2
			return
		case '*':
			scanner.javascript.stack = append(scanner.javascript.stack, jsScanBlockComment)
			scanner.cursor += 2
			return
		}
		if scanner.javascript.canStartRegex {
			scanner.javascript.stack = append(scanner.javascript.stack, jsScanRegex)
			scanner.cursor++
			return
		}
	}
	if isJavaScriptIdentifierByte(ch) {
		start := scanner.cursor
		for scanner.cursor+1 < len(scanner.text) && isJavaScriptIdentifierByte(scanner.text[scanner.cursor+1]) {
			scanner.cursor++
		}
		scanner.javascript.canStartRegex = javascriptRegexCanFollowKeyword(scanner.text[start : scanner.cursor+1])
		scanner.cursor++
		return
	}
	if state == jsScanTemplateExpression {
		switch ch {
		case '{':
			scanner.javascript.braceDepth[len(scanner.javascript.braceDepth)-1]++
		case '}':
			scanner.javascript.braceDepth[len(scanner.javascript.braceDepth)-1]--
			if scanner.javascript.braceDepth[len(scanner.javascript.braceDepth)-1] == 0 {
				scanner.javascript.braceDepth = scanner.javascript.braceDepth[:len(scanner.javascript.braceDepth)-1]
				scanner.javascript.stack = scanner.javascript.stack[:len(scanner.javascript.stack)-1]
			}
		}
	}
	switch ch {
	case ')', ']', '}':
		scanner.javascript.canStartRegex = false
	default:
		scanner.javascript.canStartRegex = true
	}
	scanner.cursor++
}

func (scanner *htmlTagScanner) advanceCSS() {
	ch := scanner.text[scanner.cursor]
	switch scanner.css.state {
	case cssScanComment:
		if ch == '*' && scanner.cursor+1 < len(scanner.text) && scanner.text[scanner.cursor+1] == '/' {
			scanner.css.state = cssScanNormal
			scanner.cursor += 2
			return
		}
		scanner.cursor++
		return
	case cssScanSingleQuote, cssScanDoubleQuote:
		if ch == '\\' {
			scanner.cursor += 1
			if scanner.cursor < len(scanner.text) {
				scanner.cursor++
			}
			return
		}
		quote := byte('\'')
		if scanner.css.state == cssScanDoubleQuote {
			quote = '"'
		}
		if ch == quote || ch == '\r' || ch == '\n' {
			scanner.css.state = cssScanNormal
		}
		scanner.cursor++
		return
	}
	if ch == '/' && scanner.cursor+1 < len(scanner.text) && scanner.text[scanner.cursor+1] == '*' {
		scanner.css.state = cssScanComment
		scanner.cursor += 2
		return
	}
	if ch == '\'' {
		scanner.css.state = cssScanSingleQuote
	} else if ch == '"' {
		scanner.css.state = cssScanDoubleQuote
	}
	scanner.cursor++
}

func isHTMLTagNameStartByte(ch byte) bool {
	return ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z'
}

func aspOpenLooksLikeClientCommentSince(text string, open int, scanStart int) bool {
	if candidate, ok := htmlASPOpenCandidate(text, open); ok {
		if candidate.context == htmlScanComment || candidate.context == htmlScanScript || candidate.context == htmlScanStyle {
			return candidate.clientComment
		}
		if candidate.clientComment {
			return true
		}
	}
	if aspOpenLooksLikeLineComment(text, open) {
		return true
	}
	if scanStart < 0 {
		scanStart = 0
	}
	if scanStart > open {
		scanStart = open
	}
	return activeClientBlockComment(text, scanStart, open)
}

func activeClientBlockComment(text string, start, end int) bool {
	blockComment := false
	var quote byte
	for cursor := start; cursor < end; cursor++ {
		if quote != 0 {
			if text[cursor] == quote && !isEscaped(text, cursor) {
				quote = 0
			}
			continue
		}
		if blockComment {
			if cursor+1 < end && text[cursor] == '*' && text[cursor+1] == '/' {
				blockComment = false
				cursor++
			}
			continue
		}
		if cursor+1 < end && text[cursor] == '/' && text[cursor+1] == '/' {
			newline := strings.IndexAny(text[cursor+2:end], "\r\n")
			if newline < 0 {
				return false
			}
			cursor += newline
			continue
		}
		if cursor+1 < end && text[cursor] == '/' && text[cursor+1] == '*' {
			blockComment = true
			cursor++
			continue
		}
		if text[cursor] == '"' || text[cursor] == '\'' || text[cursor] == '`' {
			quote = text[cursor]
		}
	}
	return blockComment
}

func aspOpenLooksLikeLineComment(text string, open int) bool {
	lineStart := strings.LastIndexAny(text[:open], "\r\n") + 1
	var quote byte
	for i := lineStart; i < open; i++ {
		ch := text[i]
		if quote != 0 {
			if ch == quote && !isEscaped(text, i) {
				quote = 0
			}
			continue
		}
		if ch == '"' || ch == '\'' || ch == '`' {
			quote = ch
			continue
		}
		if ch == '/' && i+1 < open && text[i+1] == '/' {
			return true
		}
	}
	return false
}

func aspOpenLooksLikeQuotedLiteral(text string, open int) bool {
	return aspOpenLooksLikeQuotedLiteralWithScanner(text, open, nil)
}

func aspOpenLooksLikeQuotedLiteralWithScanner(text string, open int, quotes *lineQuoteScanner) bool {
	if candidate, ok := htmlASPOpenCandidate(text, open); ok {
		if candidate.context == htmlScanComment || candidate.context == htmlScanScript || candidate.context == htmlScanStyle {
			return candidate.quotedLiteral
		}
	}
	var quote byte
	var ok bool
	if quotes != nil && quotes.text == text {
		quote, ok = quotes.activeQuote(open)
	} else {
		quote, ok = activeQuoteOnLine(text, open)
	}
	if !ok {
		return false
	}
	close := matchingQuoteOnLine(text, open+2, quote)
	if close < 0 {
		return false
	}
	aspClose := strings.Index(text[open+2:], "%>")
	if aspClose < 0 {
		return true
	}
	aspCloseOffset := open + 2 + aspClose
	if close < aspCloseOffset {
		return strings.Contains(text[close:aspCloseOffset], "<%")
	}
	return false
}

func activeQuoteOnLine(text string, offset int) (byte, bool) {
	lineStart := strings.LastIndexAny(text[:offset], "\r\n") + 1
	var quote byte
	for i := lineStart; i < offset; i++ {
		ch := text[i]
		if quote != 0 {
			if ch == quote && !isEscaped(text, i) {
				quote = 0
			}
			continue
		}
		if ch == '"' || ch == '\'' || ch == '`' {
			quote = ch
		}
	}
	if quote == 0 {
		return 0, false
	}
	return quote, true
}

// lineQuoteScanner computes activeQuoteOnLine incrementally. Queries at
// non-decreasing offsets on the same line resume from the previous offset;
// any other query restarts from the line start.
type lineQuoteScanner struct {
	text    string
	scanned int
	quote   byte
}

func (s *lineQuoteScanner) activeQuote(offset int) (byte, bool) {
	if offset < s.scanned {
		s.scanned = 0
		s.quote = 0
	}
	if lineBreak := strings.LastIndexAny(s.text[s.scanned:offset], "\r\n"); lineBreak >= 0 || s.scanned == 0 {
		s.scanned = strings.LastIndexAny(s.text[:offset], "\r\n") + 1
		s.quote = 0
	}
	quote := s.quote
	for i := s.scanned; i < offset; i++ {
		ch := s.text[i]
		if quote != 0 {
			if ch == quote && !isEscaped(s.text, i) {
				quote = 0
			}
			continue
		}
		if ch == '"' || ch == '\'' || ch == '`' {
			quote = ch
		}
	}
	s.scanned = offset
	s.quote = quote
	return quote, quote != 0
}

func matchingQuoteOnLine(text string, offset int, quote byte) int {
	for i := offset; i < len(text); i++ {
		switch text[i] {
		case '\r', '\n':
			return -1
		default:
			if text[i] == quote && !isEscaped(text, i) {
				return i
			}
		}
	}
	return -1
}

func aspOpenQuotedLiteralAt(text string, open int, quote byte) bool {
	close := matchingQuoteOnLine(text, open+2, quote)
	if close < 0 {
		return true
	}
	aspClose := strings.Index(text[open+2:], "%>")
	if aspClose < 0 {
		return true
	}
	aspCloseOffset := open + 2 + aspClose
	if close < aspCloseOffset {
		return strings.Contains(text[close:aspCloseOffset], "<%")
	}
	return false
}

func isEscaped(text string, offset int) bool {
	backslashes := 0
	for i := offset - 1; i >= 0 && text[i] == '\\'; i-- {
		backslashes++
	}
	return backslashes%2 == 1
}

func aspOpenLooksLikeHTMLFalseRegion(text string, open int) bool {
	if candidate, ok := htmlASPOpenCandidate(text, open); ok {
		return candidate.falseRegion
	}
	return htmlFalseRawTextAt(text, open)
}

func htmlFalseRawTextAt(text string, limit int) bool {
	if candidate, ok := htmlASPOpenCandidate(text, limit); ok {
		return candidate.falseRegion
	}
	return false
}

func isHTMLTagNameByte(ch byte) bool {
	return isHTMLTagNameStartByte(ch) || ch >= '0' && ch <= '9' || ch == '-' || ch == ':' || ch == '_'
}

func isHTMLFalseRawTextTag(tag string) bool {
	switch strings.ToLower(tag) {
	case "textarea", "title", "xmp", "iframe", "noembed", "noframes":
		return true
	default:
		return false
	}
}

func htmlClosingTagEndAt(text string, start int, tag string, limit int) int {
	prefix := "</" + tag
	if start < 0 || start+len(prefix) > limit || !strings.EqualFold(text[start:start+len(prefix)], prefix) {
		return -1
	}
	cursor := start + len(prefix)
	for cursor < limit && isHTMLWhitespace(text[cursor]) {
		cursor++
	}
	if cursor >= limit || text[cursor] != '>' {
		return -1
	}
	return cursor + 1
}

func aspOpenLooksLikeJSTemplateLiteral(text string, open int) bool {
	if candidate, ok := htmlASPOpenCandidate(text, open); ok {
		return candidate.context == htmlScanScript && candidate.jsTemplate
	}
	opens := scriptOpenPattern.FindAllStringIndex(text[:open], -1)
	if len(opens) == 0 {
		return false
	}
	lastOpen := opens[len(opens)-1]
	closes := scriptClosePattern.FindAllStringIndex(text[lastOpen[1]:open], -1)
	if len(closes) > 0 {
		return false
	}
	active, _ := javascriptTemplateRawAt(text[lastOpen[1]:open])
	return active
}

func javascriptTemplateRawAt(text string) (bool, int) {
	const (
		jsNormal = iota
		jsSingleQuote
		jsDoubleQuote
		jsLineComment
		jsBlockComment
		jsTemplateRaw
		jsTemplateExpression
	)
	stack := []int{jsNormal}
	braceDepth := []int(nil)
	templateStarts := []int(nil)
	for index := 0; index < len(text); index++ {
		state := stack[len(stack)-1]
		ch := text[index]
		if state == jsLineComment {
			if ch == '\r' || ch == '\n' {
				stack = stack[:len(stack)-1]
			}
			continue
		}
		if state == jsBlockComment {
			if ch == '*' && index+1 < len(text) && text[index+1] == '/' {
				stack = stack[:len(stack)-1]
				index++
			}
			continue
		}
		if state == jsSingleQuote || state == jsDoubleQuote {
			if ch == '\\' {
				index++
				continue
			}
			quote := byte('\'')
			if state == jsDoubleQuote {
				quote = '"'
			}
			if ch == quote || ch == '\r' || ch == '\n' {
				stack = stack[:len(stack)-1]
			}
			continue
		}
		if state == jsTemplateRaw {
			if ch == '\\' {
				index++
				continue
			}
			if ch == '`' {
				stack = stack[:len(stack)-1]
				templateStarts = templateStarts[:len(templateStarts)-1]
				continue
			}
			if ch == '$' && index+1 < len(text) && text[index+1] == '{' {
				stack = append(stack, jsTemplateExpression)
				braceDepth = append(braceDepth, 1)
				index++
			}
			continue
		}

		if ch == '\\' {
			index++
			continue
		}
		if ch == '\'' {
			stack = append(stack, jsSingleQuote)
			continue
		}
		if ch == '"' {
			stack = append(stack, jsDoubleQuote)
			continue
		}
		if ch == '`' {
			stack = append(stack, jsTemplateRaw)
			templateStarts = append(templateStarts, index)
			continue
		}
		if ch == '/' && index+1 < len(text) {
			switch text[index+1] {
			case '/':
				stack = append(stack, jsLineComment)
				index++
				continue
			case '*':
				stack = append(stack, jsBlockComment)
				index++
				continue
			}
		}
		if state == jsTemplateExpression {
			switch ch {
			case '{':
				braceDepth[len(braceDepth)-1]++
			case '}':
				braceDepth[len(braceDepth)-1]--
				if braceDepth[len(braceDepth)-1] == 0 {
					braceDepth = braceDepth[:len(braceDepth)-1]
					stack = stack[:len(stack)-1]
				}
			}
		}
	}
	if len(stack) == 0 || stack[len(stack)-1] != jsTemplateRaw || len(templateStarts) == 0 {
		return false, -1
	}
	return true, templateStarts[len(templateStarts)-1]
}

func RegionAt(parsed *ParsedDocument, offset int) *Region {
	if parsed == nil || offset < 0 {
		return nil
	}
	var best *Region
	bestPriority := -1
	bestSpan := int(^uint(0) >> 1)
	for i := range parsed.Regions {
		region := &parsed.Regions[i]
		atUnclosedStyleAttributeEnd := region.Kind == RegionStyleAttribute && region.End == region.ContentEnd && offset == region.End
		if offset < region.Start || offset >= region.End && !atUnclosedStyleAttributeEnd {
			continue
		}
		priority := regionPriority(*region, offset)
		if priority < 0 {
			continue
		}
		span := region.End - region.Start
		if offset >= region.ContentStart && offset < region.ContentEnd {
			span = region.ContentEnd - region.ContentStart
		}
		if priority > bestPriority || priority == bestPriority && span < bestSpan {
			best = region
			bestPriority = priority
			bestSpan = span
		}
	}
	return best
}

func regionPriority(region Region, offset int) int {
	switch region.Kind {
	case RegionASPBlock, RegionASPExpression, RegionASPDirective:
		return 3
	case RegionStyleAttribute:
		if offset < region.ContentStart || offset > region.ContentEnd {
			return -1
		}
		return 2
	case RegionStyle, RegionClientScript, RegionServerScript:
		if offset < region.ContentStart || offset >= region.ContentEnd {
			return -1
		}
		return 2
	default:
		return 1
	}
}

func Diagnostics(parsed *ParsedDocument) []lsp.Diagnostic {
	source := SourceDocument(parsed)
	diagnostics := make([]lsp.Diagnostic, 0, len(parsed.Errors))
	for _, err := range parsed.Errors {
		diagnostics = append(diagnostics, lsp.Diagnostic{
			Range:    source.Range(err.Start, err.End),
			Severity: lsp.DiagnosticSeverityError,
			Source:   "asp-lsp-go",
			Message:  err.Message,
		})
	}
	return diagnostics
}

func normalizeServerLanguage(value string) EmbeddedLanguage {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "jscript", "javascript":
		return LanguageJScript
	default:
		return LanguageVBScript
	}
}

func aspDirectiveLanguageWithFeatures(text string, features aspOpenScanFeatures) (string, bool) {
	cursor := 0
	for cursor < len(text) {
		open := indexASPOpenWithFeatures(text, cursor, features)
		if open == -1 {
			return "", false
		}
		contentStart := open + 2
		closeRel := strings.Index(text[contentStart:], "%>")
		if closeRel == -1 {
			return "", false
		}
		close := contentStart + closeRel
		cursor = close + 2
		if contentStart >= len(text) || text[contentStart] != '@' {
			continue
		}
		raw := strings.TrimSpace(text[contentStart+1 : close])
		attributes := parseAttributeStrings(aspDirectiveAttributeText(raw))
		if value, ok := attributes["language"]; ok {
			return value, true
		}
	}
	return "", false
}

func aspDirectiveAttributeText(raw string) string {
	return strings.TrimSpace(raw)
}

var attrPattern = regexp.MustCompile(`([A-Za-z_:][-A-Za-z0-9_:.]*)\s*(?:=\s*("([^"]*)"|'([^']*)'|([^\s>]+)))?`)

var scriptOpenPattern = regexp.MustCompile(`(?is)<script\b[^>]*>`)
var scriptClosePattern = regexp.MustCompile(`(?is)</script\s*>`)

func parseAttributeStrings(text string) map[string]string {
	attributes := make(map[string]string)
	for _, match := range attrPattern.FindAllStringSubmatchIndex(text, -1) {
		if len(match) < 12 || match[2] < 0 {
			continue
		}
		valueStart, valueEnd := firstAttributeValueSpan(match)
		if valueStart < 0 {
			continue
		}
		name := text[match[2]:match[3]]
		value := text[valueStart:valueEnd]
		attributes[name] = value
		attributes[strings.ToLower(name)] = value
	}
	return attributes
}

func firstAttributeValueSpan(match []int) (int, int) {
	for i := 6; i+1 < len(match); i += 2 {
		if match[i] >= 0 {
			return match[i], match[i+1]
		}
	}
	return -1, -1
}

func extractIncludes(source *TextDocument, text string) []Include {
	includes := make([]Include, 0)
	for cursor := 0; cursor < len(text); {
		start, token := nextIncludeScanToken(text, cursor)
		if start < 0 {
			break
		}
		if includeTokenInsideHTMLTag(text, start) {
			cursor = start + 1
			continue
		}
		switch token {
		case "asp":
			close := aspCloseDelimiter(text, start)
			if close < 0 {
				return includes
			}
			cursor = close + 2
		case "comment":
			end := strings.Index(text[start+4:], "-->")
			if end < 0 {
				return includes
			}
			end += start + 4 + 3
			if include, ok := parseIncludeComment(source, text, start, end); ok {
				includes = append(includes, include)
			}
			cursor = end
		case "script", "style":
			end := includeOpeningTagEnd(text, start)
			if end < 0 {
				return includes
			}
			if includeOpeningTagIsSelfClosing(text, start, end) {
				cursor = end
				continue
			}
			cursor = includeEmbeddedElementEnd(text, token, end)
		default:
			cursor = start + 1
		}
	}
	return includes
}

// nextIncludeScanToken finds the next scanner boundary used by the TypeScript
// include extractor.  Looking only at these boundaries lets us skip complete
// ASP/script/style islands instead of matching comments inside their contents.
func nextIncludeScanToken(text string, cursor int) (int, string) {
	for index := cursor; index < len(text); index++ {
		if text[index] != '<' {
			continue
		}
		if strings.HasPrefix(text[index:], "<!--") {
			return index, "comment"
		}
		if strings.HasPrefix(text[index:], "<%") {
			return index, "asp"
		}
		for _, tag := range []string{"script", "style"} {
			if len(text)-index < len(tag)+2 || !asciiEqualFoldBytes(text[index+1:index+1+len(tag)], tag) {
				continue
			}
			next := text[index+1+len(tag)]
			if next == '/' || isHTMLWhitespace(next) || next == '>' {
				return index, tag
			}
		}
	}
	return -1, ""
}

func includeTokenInsideHTMLTag(text string, index int) bool {
	if index <= 0 {
		return false
	}
	lastOpen := strings.LastIndex(text[:index], "<")
	lastClose := strings.LastIndex(text[:index], ">")
	if lastOpen < 0 || lastClose >= lastOpen {
		return false
	}
	return !strings.HasPrefix(text[lastOpen:], "<!--") && !strings.HasPrefix(text[lastOpen:], "<%")
}

func includeOpeningTagEnd(text string, start int) int {
	var quote byte
	for index := start; index < len(text); index++ {
		ch := text[index]
		if quote != 0 {
			if ch == quote && !isEscaped(text, index) {
				quote = 0
			}
			continue
		}
		if ch == '"' || ch == '\'' {
			quote = ch
			continue
		}
		if ch == '>' {
			return index + 1
		}
	}
	return -1
}

func includeOpeningTagIsSelfClosing(text string, start, end int) bool {
	for index := end - 2; index > start && isHTMLWhitespace(text[index]); index-- {
		// Skip whitespace before the final tag delimiter.
	}
	return end >= 2 && text[end-2] == '/'
}

func includeEmbeddedElementEnd(text, tag string, contentStart int) int {
	pattern := regexp.MustCompile(`(?is)</` + tag + `\s*>`)
	close := pattern.FindStringIndex(text[contentStart:])
	if close == nil {
		return len(text)
	}
	return contentStart + close[1]
}

func parseIncludeComment(source *TextDocument, text string, start, end int) (Include, bool) {
	contentStart := start + 4
	contentEnd := end - 3
	if contentEnd < contentStart {
		return Include{}, false
	}
	body := text[contentStart:contentEnd]
	leading := len(body) - len(strings.TrimLeft(body, " \t\r\n"))
	bodyStart := contentStart + leading
	body = strings.TrimSpace(body)
	if len(body) < len("#include") || !strings.EqualFold(body[:len("#include")], "#include") ||
		len(body) > len("#include") && !isHTMLWhitespace(body[len("#include")]) {
		return Include{}, false
	}
	attributeStart := bodyStart + len("#include")
	attributeText := text[attributeStart:contentEnd]
	mode, path := "", ""
	pathStart := -1
	for _, attribute := range parseIncludeAttributes(attributeText) {
		if attribute.Path == "" {
			continue
		}
		// TypeScript gives a virtual include precedence when both attributes are
		// present in one comment, matching IIS include resolution.
		if attribute.Mode == "virtual" || mode == "" {
			mode, path = attribute.Mode, attribute.Path
			pathStart = attribute.ValueStart
		}
	}
	if path == "" || pathStart < 0 {
		return Include{}, false
	}
	pathStart += attributeStart
	return Include{Path: path, Mode: mode, Range: source.Range(pathStart, pathStart+len(path))}, true
}

type includeAttribute struct {
	Mode       string
	Path       string
	ValueStart int
}

func parseIncludeAttributes(text string) []includeAttribute {
	var attributes []includeAttribute
	for cursor := 0; cursor < len(text); {
		for cursor < len(text) && isHTMLWhitespace(text[cursor]) {
			cursor++
		}
		if cursor >= len(text) {
			break
		}
		nameStart := cursor
		if !isIncludeAttributeNameStart(text[cursor]) {
			cursor++
			continue
		}
		cursor++
		for cursor < len(text) && isIncludeAttributeNameChar(text[cursor]) {
			cursor++
		}
		name := strings.ToLower(text[nameStart:cursor])
		for cursor < len(text) && isHTMLWhitespace(text[cursor]) {
			cursor++
		}
		if cursor >= len(text) || text[cursor] != '=' {
			continue
		}
		cursor++
		for cursor < len(text) && isHTMLWhitespace(text[cursor]) {
			cursor++
		}
		if cursor >= len(text) {
			break
		}
		valueStart := cursor
		valueEnd := cursor
		if text[cursor] == '"' || text[cursor] == '\'' {
			quote := text[cursor]
			valueStart++
			valueEnd = valueStart
			for valueEnd < len(text) && text[valueEnd] != quote {
				valueEnd++
			}
			cursor = valueEnd
			if cursor < len(text) {
				cursor++
			}
		} else {
			for valueEnd < len(text) && !isHTMLWhitespace(text[valueEnd]) && text[valueEnd] != '>' {
				valueEnd++
			}
			cursor = valueEnd
		}
		if (name == "file" || name == "virtual") && valueStart < valueEnd {
			attributes = append(attributes, includeAttribute{Mode: name, Path: text[valueStart:valueEnd], ValueStart: valueStart})
		}
	}
	return attributes
}

func isIncludeAttributeNameStart(ch byte) bool {
	return ch == '_' || ch == ':' || ch >= 'A' && ch <= 'Z' || ch >= 'a' && ch <= 'z'
}

func isIncludeAttributeNameChar(ch byte) bool {
	return isIncludeAttributeNameStart(ch) || ch >= '0' && ch <= '9' || ch == '-' || ch == '.'
}

func scanHTMLScriptAndStyle(text string, base []Region, defaultLanguage EmbeddedLanguage) []Region {
	if !containsASCIIFold(text, "style") && !containsASCIIFold(text, "<script") {
		return nil
	}
	masked := []byte(text)
	for _, region := range base {
		if region.Kind != RegionHTML {
			for i := region.Start; i < region.End && i < len(masked); i++ {
				if masked[i] != '\r' && masked[i] != '\n' {
					masked[i] = ' '
				}
			}
		}
	}
	lowerASCIIBytes(masked)
	html := string(masked)
	var regions []Region
	regions = append(regions, tagContentRegions(html, "style", RegionStyle, LanguageCSS)...)
	regions = append(regions, scriptContentRegions(html, defaultLanguage)...)
	regions = append(regions, styleAttributeRegions(html)...)
	return regions
}

func lowerASCIIBytes(text []byte) {
	for i, ch := range text {
		if 'A' <= ch && ch <= 'Z' {
			text[i] = ch + ('a' - 'A')
		}
	}
}

// ContainsASCIIFold reports whether text contains needle, folding ASCII
// uppercase to lowercase without allocating.
func ContainsASCIIFold(text, needle string) bool {
	return indexASCIIFold(text, needle) >= 0
}

func containsASCIIFold(text string, needle string) bool {
	return indexASCIIFold(text, needle) >= 0
}

func asciiEqualFoldBytes(left string, right string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := 0; i < len(left); i++ {
		l, r := left[i], right[i]
		if 'A' <= l && l <= 'Z' {
			l += 'a' - 'A'
		}
		if 'A' <= r && r <= 'Z' {
			r += 'a' - 'A'
		}
		if l != r {
			return false
		}
	}
	return true
}

func scriptContentRegions(html string, defaultLanguage EmbeddedLanguage) []Region {
	var regions []Region
	offset := 0
	for offset < len(html) {
		start, contentStart, ok := nextHTMLContentElement(html, "script", offset)
		if !ok {
			break
		}
		kind := RegionClientScript
		language := LanguageJavaScript
		attrs := parseAttributeStrings(html[start:contentStart])
		if strings.EqualFold(attrs["runat"], "server") {
			kind = RegionServerScript
			if languageAttr := serverScriptLanguageAttribute(attrs); languageAttr != "" {
				language = normalizeServerLanguage(languageAttr)
			} else {
				language = defaultLanguage
			}
		}
		contentEnd, end, ok := htmlContentElementEnd(html, "script", contentStart)
		if !ok {
			regions = append(regions, Region{
				Kind:         kind,
				Language:     language,
				Start:        start,
				End:          len(html),
				ContentStart: contentStart,
				ContentEnd:   len(html),
			})
			break
		}
		regions = append(regions, Region{Kind: kind, Language: language, Start: start, End: end, ContentStart: contentStart, ContentEnd: contentEnd})
		offset = end
	}
	return regions
}

func serverScriptLanguageAttribute(attrs map[string]string) string {
	if language := strings.TrimSpace(attrs["language"]); language != "" {
		return language
	}
	if scriptType := strings.TrimSpace(attrs["type"]); scriptType != "" {
		return scriptType
	}
	return ""
}

func tagContentRegions(html, tag string, kind RegionKind, language EmbeddedLanguage) []Region {
	var regions []Region
	offset := 0
	for offset < len(html) {
		start, contentStart, ok := nextHTMLContentElement(html, tag, offset)
		if !ok {
			break
		}
		contentEnd, end, ok := htmlContentElementEnd(html, tag, contentStart)
		if !ok {
			regions = append(regions, Region{
				Kind:         kind,
				Language:     language,
				Start:        start,
				End:          len(html),
				ContentStart: contentStart,
				ContentEnd:   len(html),
			})
			break
		}
		regions = append(regions, Region{Kind: kind, Language: language, Start: start, End: end, ContentStart: contentStart, ContentEnd: contentEnd})
		offset = end
	}
	return regions
}

func nextHTMLContentElement(html, wantedTag string, cursor int) (int, int, bool) {
	for cursor < len(html) {
		relative := strings.IndexByte(html[cursor:], '<')
		if relative < 0 {
			return 0, 0, false
		}
		start := cursor + relative
		if strings.HasPrefix(html[start:], "<!--") {
			commentEnd := strings.Index(html[start+4:], "-->")
			if commentEnd < 0 {
				return 0, 0, false
			}
			cursor = start + 4 + commentEnd + len("-->")
			continue
		}
		if start+1 >= len(html) {
			return 0, 0, false
		}
		if html[start+1] == '!' || html[start+1] == '?' {
			tagEnd, closed := htmlOpeningTagEnd(html, start+2)
			if !closed {
				return 0, 0, false
			}
			cursor = tagEnd
			continue
		}
		nameStart := start + 1
		if html[nameStart] == '/' {
			cursor = nameStart + 1
			continue
		}
		if !isHTMLTagNameStart(html[nameStart]) {
			cursor = nameStart
			continue
		}
		nameEnd := nameStart + 1
		for nameEnd < len(html) && isHTMLTagNameChar(html[nameEnd]) {
			nameEnd++
		}
		tagEnd, closed := htmlOpeningTagEnd(html, nameEnd)
		if !closed {
			return 0, 0, false
		}
		name := html[nameStart:nameEnd]
		if asciiEqualFold(name, wantedTag) {
			return start, tagEnd, true
		}
		if isHTMLRawTextTag(name) {
			if name == "plaintext" {
				return 0, 0, false
			}
			cursor = htmlRawTextElementEnd(html, name, tagEnd)
			continue
		}
		cursor = tagEnd
	}
	return 0, 0, false
}

func htmlContentElementEnd(html, tag string, contentStart int) (int, int, bool) {
	closePrefix := "</" + tag
	for cursor := contentStart; cursor < len(html); {
		relative := indexASCIIFold(html[cursor:], closePrefix)
		if relative < 0 {
			return 0, 0, false
		}
		closeStart := cursor + relative
		afterName := closeStart + len(closePrefix)
		closeEnd := afterName
		for closeEnd < len(html) && isHTMLWhitespace(html[closeEnd]) {
			closeEnd++
		}
		if closeEnd < len(html) && html[closeEnd] == '>' {
			return closeStart, closeEnd + 1, true
		}
		cursor = afterName
	}
	return 0, 0, false
}

func isHTMLRawTextTag(tag string) bool {
	switch strings.ToLower(tag) {
	case "plaintext", "script", "style", "textarea", "title", "xmp", "iframe", "noembed", "noframes":
		return true
	default:
		return false
	}
}

func styleAttributeRegions(html string) []Region {
	if !strings.Contains(html, "style") {
		return nil
	}
	var regions []Region
	for cursor := 0; cursor < len(html); {
		openOffset := strings.IndexByte(html[cursor:], '<')
		if openOffset < 0 {
			break
		}
		open := cursor + openOffset
		if strings.HasPrefix(html[open:], "<!--") {
			commentEnd := strings.Index(html[open+4:], "-->")
			if commentEnd < 0 {
				break
			}
			cursor = open + 4 + commentEnd + len("-->")
			continue
		}
		if open+1 >= len(html) {
			break
		}

		switch html[open+1] {
		case '!', '?', '/':
			tagEnd, _ := htmlOpeningTagEnd(html, open+2)
			cursor = tagEnd
			continue
		}
		tagNameStart := open + 1
		if !isHTMLTagNameStart(html[tagNameStart]) {
			cursor = tagNameStart
			continue
		}

		tagNameEnd := tagNameStart + 1
		for tagNameEnd < len(html) && isHTMLTagNameChar(html[tagNameEnd]) {
			tagNameEnd++
		}
		tagEnd, closed := htmlOpeningTagEnd(html, tagNameEnd)
		attributesEnd := tagEnd
		if closed {
			attributesEnd--
		}
		regions = append(regions, styleAttributesInOpeningTag(html, tagNameEnd, attributesEnd)...)

		tagName := html[tagNameStart:tagNameEnd]
		if closed {
			switch tagName {
			case "plaintext":
				cursor = len(html)
				continue
			case "script", "style", "textarea", "title", "xmp", "iframe", "noembed", "noframes":
				cursor = htmlRawTextElementEnd(html, tagName, tagEnd)
				continue
			}
		}
		cursor = tagEnd
	}
	return regions
}

func styleAttributesInOpeningTag(html string, start, end int) []Region {
	var regions []Region
	for cursor := start; cursor < end; {
		for cursor < end && (isHTMLWhitespace(html[cursor]) || html[cursor] == '/') {
			cursor++
		}
		if cursor >= end {
			break
		}

		nameStart := cursor
		for cursor < end && isHTMLAttributeNameChar(html[cursor]) {
			cursor++
		}
		if cursor == nameStart {
			cursor++
			continue
		}
		nameEnd := cursor
		for cursor < end && isHTMLWhitespace(html[cursor]) {
			cursor++
		}
		if cursor >= end || html[cursor] != '=' {
			continue
		}
		cursor++
		for cursor < end && isHTMLWhitespace(html[cursor]) {
			cursor++
		}

		contentStart := cursor
		contentEnd := cursor
		valueEnd := cursor
		if cursor < end && (html[cursor] == '"' || html[cursor] == '\'') {
			quote := html[cursor]
			contentStart = cursor + 1
			valueEnd = contentStart
			for valueEnd < end && html[valueEnd] != quote {
				valueEnd++
			}
			contentEnd = valueEnd
			if valueEnd < end {
				valueEnd++
			}
		} else {
			for valueEnd < end && !isHTMLWhitespace(html[valueEnd]) && html[valueEnd] != '>' {
				valueEnd++
			}
			contentEnd = valueEnd
		}
		if html[nameStart:nameEnd] == "style" {
			regions = append(regions, Region{
				Kind:         RegionStyleAttribute,
				Language:     LanguageCSS,
				Start:        nameStart,
				End:          valueEnd,
				ContentStart: contentStart,
				ContentEnd:   contentEnd,
			})
		}
		cursor = valueEnd
	}
	return regions
}

func htmlOpeningTagEnd(html string, start int) (int, bool) {
	var quote byte
	for cursor := start; cursor < len(html); cursor++ {
		ch := html[cursor]
		if quote != 0 {
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '"' || ch == '\'' {
			quote = ch
			continue
		}
		if ch == '>' {
			return cursor + 1, true
		}
	}
	return len(html), false
}

func htmlRawTextElementEnd(html, tagName string, contentStart int) int {
	closePrefix := "</" + tagName
	for cursor := contentStart; cursor < len(html); {
		closeOffset := indexASCIIFold(html[cursor:], closePrefix)
		if closeOffset < 0 {
			return len(html)
		}
		afterName := cursor + closeOffset + len(closePrefix)
		closeEnd := afterName
		for closeEnd < len(html) && isHTMLWhitespace(html[closeEnd]) {
			closeEnd++
		}
		if closeEnd < len(html) && html[closeEnd] == '>' {
			return closeEnd + 1
		}
		cursor = afterName
	}
	return len(html)
}

func indexASCIIFold(text, needle string) int {
	if needle == "" {
		return 0
	}
	for index := 0; index+len(needle) <= len(text); index++ {
		if asciiEqualFold(text[index:index+len(needle)], needle) {
			return index
		}
	}
	return -1
}

func isHTMLTagNameStart(ch byte) bool {
	return ch >= 'a' && ch <= 'z'
}

func isHTMLTagNameChar(ch byte) bool {
	return isHTMLTagNameStart(ch) || ch >= '0' && ch <= '9' || ch == '-' || ch == ':' || ch == '_'
}

func isHTMLAttributeNameChar(ch byte) bool {
	return !isHTMLWhitespace(ch) && ch != '"' && ch != '\'' && ch != '>' && ch != '/' && ch != '=' && ch != '<' && ch != '`'
}

func isHTMLWhitespace(ch byte) bool {
	return ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' || ch == '\f'
}

func mergeRegions(regions []Region) []Region {
	if len(regions) == 0 {
		return regions
	}
	// Keep nested script/style regions after ASP holes, because callers choose the
	// most specific region by content range when needed.
	return regions
}
