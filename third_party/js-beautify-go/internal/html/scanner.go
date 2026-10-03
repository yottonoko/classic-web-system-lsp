package html

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// jsSpaceClass is the body of a character class matching JavaScript's \s.
const jsSpaceClass = `\t\n\x{0B}\f\r \x{A0}\x{1680}\x{2000}-\x{200A}\x{2028}\x{2029}\x{202F}\x{205F}\x{3000}\x{FEFF}`

// matcher is a pattern the scanner can match at a position or search for.
// It stands in for the JavaScript RegExp objects js-beautify uses with the
// sticky flag and lastIndex. Most patterns are simple literals or byte sets,
// which are matched directly instead of through package regexp.
type matcher interface {
	// matchAt returns the match length at pos, or -1 when there is no match.
	matchAt(input string, pos int) int
	// find returns the first match at or after pos, or -1, -1.
	find(input string, pos int) (int, int)
	// firstBytes lists the bytes a match can start with, or "" when unknown.
	firstBytes() string
}

type literalMatcher string

func (m literalMatcher) matchAt(input string, pos int) int {
	if strings.HasPrefix(input[pos:], string(m)) {
		return len(m)
	}
	return -1
}

func (m literalMatcher) find(input string, pos int) (int, int) {
	index := strings.Index(input[pos:], string(m))
	if index < 0 {
		return -1, -1
	}
	return pos + index, pos + index + len(m)
}

func (m literalMatcher) firstBytes() string {
	return string(m[:1])
}

// byteSet is a precomputed set of bytes for scanning.
type byteSet [256]bool

func newByteSet(chars string) *byteSet {
	var set byteSet
	for i := 0; i < len(chars); i++ {
		set[chars[i]] = true
	}
	return &set
}

func (set *byteSet) index(input string, pos int) int {
	for i := pos; i < len(input); i++ {
		if set[input[i]] {
			return i
		}
	}
	return -1
}

// byteSetMatcher matches one byte from an ASCII set.
type byteSetMatcher struct {
	chars string
	set   *byteSet
}

func newByteSetMatcher(chars string) byteSetMatcher {
	return byteSetMatcher{chars: chars, set: newByteSet(chars)}
}

func (m byteSetMatcher) matchAt(input string, pos int) int {
	if pos < len(input) && m.set[input[pos]] {
		return 1
	}
	return -1
}

func (m byteSetMatcher) find(input string, pos int) (int, int) {
	index := m.set.index(input, pos)
	if index < 0 {
		return -1, -1
	}
	return index, index + 1
}

func (m byteSetMatcher) firstBytes() string {
	return m.chars
}

// unionMatcher ports (?:a|b|...) for options with known first bytes. Like a
// leftmost-first regexp, it returns the earliest match and, at one position,
// the first option that matches.
type unionMatcher struct {
	options  []matcher
	triggers string
	set      *byteSet
}

func newUnionMatcher(options ...matcher) unionMatcher {
	var triggers strings.Builder
	for _, option := range options {
		for _, b := range []byte(option.firstBytes()) {
			if strings.IndexByte(triggers.String(), b) < 0 {
				triggers.WriteByte(b)
			}
		}
	}
	return unionMatcher{options: options, triggers: triggers.String(), set: newByteSet(triggers.String())}
}

func (m unionMatcher) matchAt(input string, pos int) int {
	for _, option := range m.options {
		if length := option.matchAt(input, pos); length >= 0 {
			return length
		}
	}
	return -1
}

func (m unionMatcher) find(input string, pos int) (int, int) {
	for from := pos; from < len(input); {
		at := m.set.index(input, from)
		if at < 0 {
			break
		}
		if length := m.matchAt(input, at); length >= 0 {
			return at, at + length
		}
		from = at + 1
	}
	return -1, -1
}

func (m unionMatcher) firstBytes() string {
	return m.triggers
}

// prefixThenRuneMatcher matches prefix followed by one character that is not
// excluded, like /<%[^%]/.
type prefixThenRuneMatcher struct {
	prefix   string
	excluded func(rune) bool
}

func (m prefixThenRuneMatcher) matchAt(input string, pos int) int {
	if !strings.HasPrefix(input[pos:], m.prefix) || pos+len(m.prefix) >= len(input) {
		return -1
	}
	r, size := utf8.DecodeRuneInString(input[pos+len(m.prefix):])
	if m.excluded(r) {
		return -1
	}
	return len(m.prefix) + size
}

func (m prefixThenRuneMatcher) find(input string, pos int) (int, int) {
	return findByMatchAt(m, input, pos)
}

func (m prefixThenRuneMatcher) firstBytes() string {
	return m.prefix[:1]
}

// runeThenSuffixMatcher matches one character that is not excluded followed
// by suffix, like /[^%]%>/.
type runeThenSuffixMatcher struct {
	suffix   string
	excluded func(rune) bool
}

func (m runeThenSuffixMatcher) matchAt(input string, pos int) int {
	if pos >= len(input) {
		return -1
	}
	r, size := utf8.DecodeRuneInString(input[pos:])
	if m.excluded(r) || !strings.HasPrefix(input[pos+size:], m.suffix) {
		return -1
	}
	return size + len(m.suffix)
}

func (m runeThenSuffixMatcher) find(input string, pos int) (int, int) {
	for from := pos; ; {
		index := strings.Index(input[from:], m.suffix)
		if index < 0 {
			return -1, -1
		}
		at := from + index
		if at > pos {
			r, size := utf8.DecodeLastRuneInString(input[pos:at])
			if !m.excluded(r) {
				return at - size, at + len(m.suffix)
			}
		}
		from = at + 1
	}
}

func (m runeThenSuffixMatcher) firstBytes() string {
	return ""
}

// closeTagMatcher ports new RegExp('</' + tag_name + '[\n\r\t ]*?>', 'ig').
type closeTagMatcher string

func (m closeTagMatcher) matchAt(input string, pos int) int {
	name := string(m)
	if !strings.HasPrefix(input[pos:], "</") || len(input)-pos-2 < len(name) || !strings.EqualFold(input[pos+2:pos+2+len(name)], name) {
		return -1
	}
	end := pos + 2 + len(name)
	for end < len(input) && (input[end] == '\n' || input[end] == '\r' || input[end] == '\t' || input[end] == ' ') {
		end++
	}
	if end >= len(input) || input[end] != '>' {
		return -1
	}
	return end + 1 - pos
}

func (m closeTagMatcher) find(input string, pos int) (int, int) {
	return findByMatchAt(m, input, pos)
}

func (m closeTagMatcher) firstBytes() string {
	return "<"
}

func findByMatchAt(m matcher, input string, pos int) (int, int) {
	first := m.firstBytes()
	for from := pos; from < len(input); {
		index := strings.IndexAny(input[from:], first)
		if index < 0 {
			break
		}
		at := from + index
		if length := m.matchAt(input, at); length >= 0 {
			return at, at + length
		}
		from = at + 1
	}
	return -1, -1
}

type regexpMatcher struct {
	anchored *regexp.Regexp
	search   *regexp.Regexp
}

// newRegexpMatcher compiles a Go regular expression with the leftmost-first
// semantics JavaScript also uses, for the rarely used patterns that are not
// literals or byte sets.
func newRegexpMatcher(pattern string) *regexpMatcher {
	return &regexpMatcher{
		anchored: regexp.MustCompile(`^(?:` + pattern + `)`),
		search:   regexp.MustCompile(pattern),
	}
}

func (m *regexpMatcher) matchAt(input string, pos int) int {
	location := m.anchored.FindStringIndex(input[pos:])
	if location == nil {
		return -1
	}
	return location[1]
}

func (m *regexpMatcher) find(input string, pos int) (int, int) {
	location := m.search.FindStringIndex(input[pos:])
	if location == nil {
		return -1, -1
	}
	return pos + location[0], pos + location[1]
}

func (m *regexpMatcher) firstBytes() string {
	return ""
}

// smartyStartMatcher implements /{(?=[^}{\s\n])/, which needs a lookahead.
type smartyStartMatcher struct{}

func (smartyStartMatcher) matchAt(input string, pos int) int {
	if pos+1 >= len(input) || input[pos] != '{' {
		return -1
	}
	next, _ := utf8.DecodeRuneInString(input[pos+1:])
	if next == '}' || next == '{' || isJSSpace(next) {
		return -1
	}
	return 1
}

func (m smartyStartMatcher) find(input string, pos int) (int, int) {
	return findByMatchAt(m, input, pos)
}

func (smartyStartMatcher) firstBytes() string {
	return "{"
}

func isJSSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xA0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF:
		return true
	}
	return r >= 0x2000 && r <= 0x200A
}

// inputScanner mirrors js-beautify's InputScanner over byte offsets.
type inputScanner struct {
	input    string
	position int
}

func newInputScanner(input string) *inputScanner {
	return &inputScanner{input: input}
}

func (s *inputScanner) restart() {
	s.position = 0
}

func (s *inputScanner) hasNext() bool {
	return s.position < len(s.input)
}

// next returns the next character. Unlike JavaScript, which steps over UTF-16
// code units, it returns whole runes so tokens never split a character.
func (s *inputScanner) next() string {
	if !s.hasNext() {
		return ""
	}
	_, size := utf8.DecodeRuneInString(s.input[s.position:])
	value := s.input[s.position : s.position+size]
	s.position += size
	return value
}

// peek returns the byte at the current position plus index, or -1.
func (s *inputScanner) peek(index int) int {
	index += s.position
	if index < 0 || index >= len(s.input) {
		return -1
	}
	return int(s.input[index])
}

// match consumes a match of pattern at the current position.
func (s *inputScanner) match(pattern matcher) (string, bool) {
	length := pattern.matchAt(s.input, s.position)
	if length < 0 {
		return "", false
	}
	value := s.input[s.position : s.position+length]
	s.position += length
	return value, true
}

// read advances like InputScanner.read; reads are contiguous, so callers
// slice the input instead of concatenating the pieces.
func (s *inputScanner) read(starting, until matcher, untilAfter bool) string {
	start := s.position
	matched := false
	if starting != nil {
		_, matched = s.match(starting)
	}
	if until != nil && (matched || starting == nil) {
		s.readUntil(until, untilAfter)
	}
	return s.input[start:s.position]
}

func (s *inputScanner) readUntil(pattern matcher, untilAfter bool) string {
	matchIndex := len(s.input)
	if start, end := pattern.find(s.input, s.position); start >= 0 {
		matchIndex = start
		if untilAfter {
			matchIndex = end
		}
	}
	value := s.input[s.position:matchIndex]
	s.position = matchIndex
	return value
}

func (s *inputScanner) readUntilAfter(pattern matcher) string {
	return s.readUntil(pattern, true)
}

// pattern mirrors js-beautify's Pattern builder. Builder methods return copies.
type pattern struct {
	input      *inputScanner
	starting   matcher
	matching   matcher
	until      matcher
	untilAfter bool
}

func newPattern(input *inputScanner) pattern {
	return pattern{input: input}
}

func (p pattern) read() string {
	start := p.input.position
	p.input.read(p.starting, nil, false)
	if p.starting == nil || p.input.position > start {
		p.input.read(p.matching, p.until, p.untilAfter)
	}
	return p.input.input[start:p.input.position]
}

func (p pattern) untilAfterPattern(m matcher) pattern {
	p.untilAfter = true
	p.until = m
	return p
}

func (p pattern) untilPattern(m matcher) pattern {
	p.untilAfter = false
	p.until = m
	return p
}

func (p pattern) startingWith(m matcher) pattern {
	p.starting = m
	return p
}

func (p pattern) matchingPattern(m matcher) pattern {
	p.matching = m
	return p
}

// whitespacePattern mirrors js-beautify's WhitespacePattern.
type whitespacePattern struct {
	input                 *inputScanner
	newlineCount          int
	whitespaceBeforeToken string
}

func (p *whitespacePattern) read() string {
	p.newlineCount = 0
	p.whitespaceBeforeToken = ""
	start := p.input.position
	for p.input.position < len(p.input.input) {
		switch p.input.input[p.input.position] {
		case '\t', ' ', '\n', '\r':
			p.input.position++
			continue
		}
		break
	}
	value := p.input.input[start:p.input.position]
	if value == " " {
		p.whitespaceBeforeToken = " "
	} else if value != "" {
		// Like WhitespacePattern.__split on \r\n, \r, or \n: count the line
		// breaks and keep the text after the last one.
		start := 0
		for index := 0; index < len(value); index++ {
			switch value[index] {
			case '\r':
				p.newlineCount++
				if index+1 < len(value) && value[index+1] == '\n' {
					index++
				}
				start = index + 1
			case '\n':
				p.newlineCount++
				start = index + 1
			}
		}
		p.whitespaceBeforeToken = value[start:]
	}
	return value
}

var templateLanguages = []string{"django", "erb", "handlebars", "php", "smarty", "angular"}

type templateFlags struct {
	django, erb, handlebars, php, smarty, angular bool
}

func (f *templateFlags) set(language string, value bool) {
	switch language {
	case "django":
		f.django = value
	case "erb":
		f.erb = value
	case "handlebars":
		f.handlebars = value
	case "php":
		f.php = value
	case "smarty":
		f.smarty = value
	case "angular":
		f.angular = value
	}
}

type templatePatterns struct {
	handlebarsComment, handlebarsUnescaped, handlebars, php, erb, django, djangoValue, djangoComment, smarty, smartyComment, smartyLiteral pattern
}

// templatablePattern mirrors js-beautify's TemplatablePattern.
type templatablePattern struct {
	pattern
	templatePattern matcher
	disabled        templateFlags
	excluded        templateFlags
	patterns        *templatePatterns
}

func isPercent(r rune) bool {
	return r == '%'
}

var (
	// phpStart ports /<\?(?:[= ]|php)/.
	phpStart = newUnionMatcher(literalMatcher("<?="), literalMatcher("<? "), literalMatcher("<?php"))
	// erbStart ports /<%[^%]/ and erbEnd ports /[^%]%>/.
	erbStart = prefixThenRuneMatcher{prefix: "<%", excluded: isPercent}
	erbEnd   = runeThenSuffixMatcher{suffix: "%>", excluded: isPercent}
	// smartyEnd ports /[^\s\n]}/.
	smartyEnd = runeThenSuffixMatcher{suffix: "}", excluded: isJSSpace}
)

func newTemplatablePattern(input *inputScanner) templatablePattern {
	base := newPattern(input)
	return templatablePattern{
		pattern: base,
		patterns: &templatePatterns{
			handlebarsComment:   base.startingWith(literalMatcher("{{!--")).untilAfterPattern(literalMatcher("--}}")),
			handlebarsUnescaped: base.startingWith(literalMatcher("{{{")).untilAfterPattern(literalMatcher("}}}")),
			handlebars:          base.startingWith(literalMatcher("{{")).untilAfterPattern(literalMatcher("}}")),
			php:                 base.startingWith(phpStart).untilAfterPattern(literalMatcher("?>")),
			erb:                 base.startingWith(erbStart).untilAfterPattern(erbEnd),
			django:              base.startingWith(literalMatcher("{%")).untilAfterPattern(literalMatcher("%}")),
			djangoValue:         base.startingWith(literalMatcher("{{")).untilAfterPattern(literalMatcher("}}")),
			djangoComment:       base.startingWith(literalMatcher("{#")).untilAfterPattern(literalMatcher("#}")),
			smarty:              base.startingWith(smartyStartMatcher{}).untilAfterPattern(smartyEnd),
			smartyComment:       base.startingWith(literalMatcher("{*")).untilAfterPattern(literalMatcher("*}")),
			smartyLiteral:       base.startingWith(literalMatcher("{literal}")).untilAfterPattern(literalMatcher("{/literal}")),
		},
	}
}

func (p templatablePattern) update() templatablePattern {
	var items []matcher
	if !p.disabled.php {
		items = append(items, p.patterns.php.starting)
	}
	if !p.disabled.handlebars {
		items = append(items, p.patterns.handlebars.starting)
	}
	if !p.disabled.angular {
		items = append(items, p.patterns.handlebars.starting)
	}
	if !p.disabled.erb {
		items = append(items, p.patterns.erb.starting)
	}
	if !p.disabled.django {
		items = append(items, p.patterns.django.starting, p.patterns.djangoValue.starting, p.patterns.djangoComment.starting)
	}
	if !p.disabled.smarty {
		items = append(items, p.patterns.smarty.starting)
	}
	if p.until != nil {
		items = append(items, p.until)
	}
	p.templatePattern = newUnionMatcher(items...)
	return p
}

func (p templatablePattern) readOptions(templating []string) templatablePattern {
	for _, language := range templateLanguages {
		p.disabled.set(language, !containsString(templating, language))
	}
	return p.update()
}

func (p templatablePattern) exclude(language string) templatablePattern {
	p.excluded.set(language, true)
	return p.update()
}

func (p templatablePattern) untilPattern(m matcher) templatablePattern {
	p.pattern = p.pattern.untilPattern(m)
	return p.update()
}

func (p templatablePattern) untilAfterPattern(m matcher) templatablePattern {
	p.pattern = p.pattern.untilAfterPattern(m)
	return p.update()
}

func (p templatablePattern) read() string {
	start := p.input.position
	if p.matching != nil {
		p.input.read(p.starting, nil, false)
	} else {
		p.input.read(p.starting, p.templatePattern, false)
	}
	for p.readTemplate() != "" {
		if p.matching != nil {
			p.input.read(p.matching, nil, false)
		} else {
			p.input.readUntil(p.templatePattern, false)
		}
	}
	if p.untilAfter {
		p.input.readUntilAfter(p.until)
	}
	return p.input.input[start:p.input.position]
}

func (p templatablePattern) readTemplate() string {
	result := ""
	switch p.input.peek(0) {
	case '<':
		peek1 := p.input.peek(1)
		if !p.disabled.php && !p.excluded.php && peek1 == '?' {
			result = p.patterns.php.read()
		}
		if result == "" && !p.disabled.erb && !p.excluded.erb && peek1 == '%' {
			result = p.patterns.erb.read()
		}
	case '{':
		if !p.disabled.handlebars && !p.excluded.handlebars {
			result = p.patterns.handlebarsComment.read()
			if result == "" {
				result = p.patterns.handlebarsUnescaped.read()
			}
			if result == "" {
				result = p.patterns.handlebars.read()
			}
		}
		if !p.disabled.django {
			if result == "" && !p.excluded.django && !p.excluded.handlebars {
				result = p.patterns.djangoValue.read()
			}
			if !p.excluded.django {
				if result == "" {
					result = p.patterns.djangoComment.read()
				}
				if result == "" {
					result = p.patterns.django.read()
				}
			}
		}
		if !p.disabled.smarty && p.disabled.django && p.disabled.handlebars {
			if result == "" {
				result = p.patterns.smartyComment.read()
			}
			if result == "" {
				result = p.patterns.smartyLiteral.read()
			}
			if result == "" {
				result = p.patterns.smarty.read()
			}
		}
	}
	return result
}

func containsString(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}
