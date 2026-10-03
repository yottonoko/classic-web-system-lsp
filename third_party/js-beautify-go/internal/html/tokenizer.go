package html

import (
	"regexp"
	"strings"
)

const (
	tokenTagOpen          = "TK_TAG_OPEN"
	tokenTagClose         = "TK_TAG_CLOSE"
	tokenControlFlowOpen  = "TK_CONTROL_FLOW_OPEN"
	tokenControlFlowClose = "TK_CONTROL_FLOW_CLOSE"
	tokenAttribute        = "TK_ATTRIBUTE"
	tokenEquals           = "TK_EQUALS"
	tokenValue            = "TK_VALUE"
	tokenComment          = "TK_COMMENT"
	tokenText             = "TK_TEXT"
	tokenUnknown          = "TK_UNKNOWN"
	tokenStart            = "TK_START"
	tokenEOF              = "TK_EOF"
	tokenContent          = "TK_CONTENT"
)

var (
	directivesBlockPattern     = regexp.MustCompile(`<!--` + ` beautify( \w+[:]\w+)+ ` + `-->`)
	directivePattern           = regexp.MustCompile(` (\w+)[:](\w+)`)
	directivesEndIgnorePattern = newRegexpMatcher(`<!--[` + jsSpaceClass + `]beautify[` + jsSpaceClass + `]ignore:end[` + jsSpaceClass + `]-->`)
)

// htmlToken mirrors js-beautify's Token with only the fields the HTML
// beautifier uses.
type htmlToken struct {
	Type             string
	Text             string
	Newlines         int
	WhitespaceBefore string
	Parent           *htmlToken
	Next             *htmlToken
	Previous         *htmlToken
	Opened           *htmlToken
	Closed           *htmlToken
	Directives       map[string]string
}

// tokenStream mirrors js-beautify's TokenStream.
type tokenStream struct {
	tokens   []*htmlToken
	position int
}

func (s *tokenStream) next() *htmlToken {
	if s.position >= len(s.tokens) {
		return nil
	}
	token := s.tokens[s.position]
	s.position++
	return token
}

func (s *tokenStream) peek(index int) *htmlToken {
	index += s.position
	if index < 0 || index >= len(s.tokens) {
		return nil
	}
	return s.tokens[index]
}

func htmlDirectives(text string) map[string]string {
	if !directivesBlockPattern.MatchString(text) {
		return nil
	}
	directives := map[string]string{}
	for _, match := range directivePattern.FindAllStringSubmatch(text, -1) {
		directives[match[1]] = match[2]
	}
	return directives
}

type tokenizerPatterns struct {
	word                          templatablePattern
	wordControlFlowCloseExcluded  templatablePattern
	singleQuote                   templatablePattern
	doubleQuote                   templatablePattern
	attribute                     templatablePattern
	elementName                   templatablePattern
	angularControlFlowStart       pattern
	handlebarsComment             pattern
	handlebars                    pattern
	handlebarsOpen                pattern
	handlebarsRawClose            pattern
	comment                       pattern
	cdata                         pattern
	conditionalComment            pattern
	processing                    pattern
	unformattedContentDelimiter   pattern
	hasUnformattedContentDelimter bool
}

// tokenizer ports js-beautify's HTML tokenizer and its core base class.
type tokenizer struct {
	input      *inputScanner
	options    *Options
	whitespace whitespacePattern
	patterns   tokenizerPatterns
	// slab allocates tokens in chunks; tokens are linked by pointer, so a
	// chunk is never grown in place.
	slab []htmlToken
}

func newTokenizer(source string, options *Options) *tokenizer {
	input := newInputScanner(source)
	t := &tokenizer{input: input, options: options, whitespace: whitespacePattern{input: input}}
	templatable := newTemplatablePattern(input).readOptions(options.Templating)
	plain := newPattern(input)
	t.patterns = tokenizerPatterns{
		word:                         templatable.untilPattern(newByteSetMatcher("\n\r\t <")),
		wordControlFlowCloseExcluded: templatable.untilPattern(newByteSetMatcher("\n\r\t <}")),
		singleQuote:                  templatable.untilAfterPattern(literalMatcher("'")),
		doubleQuote:                  templatable.untilAfterPattern(literalMatcher(`"`)),
		attribute:                    templatable.untilPattern(newUnionMatcher(newByteSetMatcher("\n\r\t =>"), literalMatcher("/>"))),
		elementName:                  templatable.untilPattern(newByteSetMatcher("\n\r\t >/")),
		angularControlFlowStart:      plain.matchingPattern(newRegexpMatcher(`@[a-zA-Z]+[^({]*[({]`)),
		handlebarsComment:            plain.startingWith(literalMatcher("{{!--")).untilAfterPattern(literalMatcher("--}}")),
		handlebars:                   plain.startingWith(literalMatcher("{{")).untilAfterPattern(literalMatcher("}}")),
		handlebarsOpen:               plain.untilPattern(newByteSetMatcher("\n\r\t }")),
		handlebarsRawClose:           plain.untilPattern(literalMatcher("}}")),
		comment:                      plain.startingWith(literalMatcher("<!--")).untilAfterPattern(literalMatcher("-->")),
		cdata:                        plain.startingWith(literalMatcher("<![CDATA[")).untilAfterPattern(literalMatcher("]]>")),
		conditionalComment:           plain.startingWith(literalMatcher("<![")).untilAfterPattern(literalMatcher("]>")),
		processing:                   plain.startingWith(literalMatcher("<?")).untilAfterPattern(literalMatcher("?>")),
	}
	if options.IndentHandlebars {
		t.patterns.word = t.patterns.word.exclude("handlebars")
		t.patterns.wordControlFlowCloseExcluded = t.patterns.wordControlFlowCloseExcluded.exclude("handlebars")
	}
	if options.UnformattedContentDelimiter != "" {
		literal := literalMatcher(options.UnformattedContentDelimiter)
		t.patterns.unformattedContentDelimiter = plain.matchingPattern(literal).untilAfterPattern(literal)
		t.patterns.hasUnformattedContentDelimter = true
	}
	return t
}

func (t *tokenizer) tokenize() *tokenStream {
	t.input.restart()
	tokens := &tokenStream{tokens: make([]*htmlToken, 0, len(t.input.input)/8+16)}
	previous := &htmlToken{Type: tokenStart}
	var openToken *htmlToken
	var openStack []*htmlToken
	for previous.Type != tokenEOF {
		current := t.nextToken(previous, openToken)
		current.Parent = openToken
		if t.isOpening(current) {
			openStack = append(openStack, openToken)
			openToken = current
		} else if openToken != nil && t.isClosing(current, openToken) {
			current.Opened = openToken
			openToken.Closed = current
			openToken = openStack[len(openStack)-1]
			openStack = openStack[:len(openStack)-1]
			current.Parent = openToken
		}
		current.Previous = previous
		previous.Next = current
		tokens.tokens = append(tokens.tokens, current)
		previous = current
	}
	return tokens
}

func (t *tokenizer) createToken(tokenType, text string) *htmlToken {
	if len(t.slab) == cap(t.slab) {
		t.slab = make([]htmlToken, 0, 512)
	}
	t.slab = append(t.slab, htmlToken{Type: tokenType, Text: text, Newlines: t.whitespace.newlineCount, WhitespaceBefore: t.whitespace.whitespaceBeforeToken})
	return &t.slab[len(t.slab)-1]
}

func (t *tokenizer) isOpening(current *htmlToken) bool {
	return current.Type == tokenTagOpen || current.Type == tokenControlFlowOpen
}

func (t *tokenizer) isClosing(current, openToken *htmlToken) bool {
	if current.Type == tokenTagClose && openToken != nil {
		if (current.Text == ">" || current.Text == "/>") && strings.HasPrefix(openToken.Text, "<") {
			return true
		}
		if current.Text == "}}" && strings.HasPrefix(openToken.Text, "{{") {
			return true
		}
	}
	return current.Type == tokenControlFlowClose && current.Text == "}" && strings.HasSuffix(openToken.Text, "{")
}

func (t *tokenizer) nextToken(previous, openToken *htmlToken) *htmlToken {
	t.whitespace.read()
	c := t.input.peek(0)
	if c < 0 {
		return t.createToken(tokenEOF, "")
	}
	if token := t.readOpenHandlebars(c, openToken); token != nil {
		return token
	}
	if token := t.readAttribute(c, previous, openToken); token != nil {
		return token
	}
	if token := t.readClose(c, openToken); token != nil {
		return token
	}
	if token := t.readScriptAndStyle(c, previous); token != nil {
		return token
	}
	if token := t.readControlFlows(c, openToken); token != nil {
		return token
	}
	if token := t.readRawContent(previous, openToken); token != nil {
		return token
	}
	if token := t.readContentWord(c, openToken); token != nil {
		return token
	}
	if token := t.readCommentOrCDATA(c); token != nil {
		return token
	}
	if token := t.readProcessing(c); token != nil {
		return token
	}
	if token := t.readOpen(c, openToken); token != nil {
		return token
	}
	return t.createToken(tokenUnknown, t.input.next())
}

func (t *tokenizer) readCommentOrCDATA(c int) *htmlToken {
	if c != '<' || t.input.peek(1) != '!' {
		return nil
	}
	var directives map[string]string
	value := t.patterns.comment.read()
	if value != "" {
		directives = htmlDirectives(value)
		if directives != nil && directives["ignore"] == "start" {
			value += t.input.readUntilAfter(directivesEndIgnorePattern)
		}
	} else {
		value = t.patterns.cdata.read()
	}
	if value == "" {
		return nil
	}
	token := t.createToken(tokenComment, value)
	token.Directives = directives
	return token
}

func (t *tokenizer) readProcessing(c int) *htmlToken {
	if c != '<' {
		return nil
	}
	value := ""
	if peek1 := t.input.peek(1); peek1 == '!' || peek1 == '?' {
		value = t.patterns.conditionalComment.read()
		if value == "" {
			value = t.patterns.processing.read()
		}
	}
	if value == "" {
		return nil
	}
	return t.createToken(tokenComment, value)
}

func (t *tokenizer) readOpen(c int, openToken *htmlToken) *htmlToken {
	if (openToken != nil && openToken.Type != tokenControlFlowOpen) || c != '<' {
		return nil
	}
	start := t.input.position
	t.input.next()
	if t.input.peek(0) == '/' {
		t.input.next()
	}
	t.patterns.elementName.read()
	return t.createToken(tokenTagOpen, t.input.input[start:t.input.position])
}

func (t *tokenizer) readOpenHandlebars(c int, openToken *htmlToken) *htmlToken {
	if openToken != nil && openToken.Type != tokenControlFlowOpen {
		return nil
	}
	if !(containsString(t.options.Templating, "angular") || t.options.IndentHandlebars) || c != '{' || t.input.peek(1) != '{' {
		return nil
	}
	if t.options.IndentHandlebars && t.input.peek(2) == '!' {
		value := t.patterns.handlebarsComment.read()
		if value == "" {
			value = t.patterns.handlebars.read()
		}
		return t.createToken(tokenComment, value)
	}
	return t.createToken(tokenTagOpen, t.patterns.handlebarsOpen.read())
}

func (t *tokenizer) readControlFlows(c int, openToken *htmlToken) *htmlToken {
	if !containsString(t.options.Templating, "angular") {
		return nil
	}
	if c == '@' {
		value := t.patterns.angularControlFlowStart.read()
		if value == "" {
			return nil
		}
		opening := 0
		if strings.HasSuffix(value, "(") {
			opening = 1
		}
		closing := 0
		for !(strings.HasSuffix(value, "{") && opening == closing) {
			next := t.input.next()
			if next == "" {
				break
			}
			if next == "(" {
				opening++
			} else if next == ")" {
				closing++
			}
			value += next
		}
		return t.createToken(tokenControlFlowOpen, value)
	}
	if c == '}' && openToken != nil && openToken.Type == tokenControlFlowOpen {
		return t.createToken(tokenControlFlowClose, t.input.next())
	}
	return nil
}

func (t *tokenizer) readClose(c int, openToken *htmlToken) *htmlToken {
	if openToken == nil || openToken.Type != tokenTagOpen {
		return nil
	}
	if strings.HasPrefix(openToken.Text, "<") && (c == '>' || (c == '/' && t.input.peek(1) == '>')) {
		value := t.input.next()
		if c == '/' {
			value += t.input.next()
		}
		return t.createToken(tokenTagClose, value)
	}
	if strings.HasPrefix(openToken.Text, "{") && c == '}' && t.input.peek(1) == '}' {
		t.input.next()
		t.input.next()
		return t.createToken(tokenTagClose, "}}")
	}
	return nil
}

func (t *tokenizer) readAttribute(c int, previous, openToken *htmlToken) *htmlToken {
	if openToken == nil || !strings.HasPrefix(openToken.Text, "<") {
		return nil
	}
	switch c {
	case '=':
		return t.createToken(tokenEquals, t.input.next())
	case '"', '\'':
		start := t.input.position
		t.input.next()
		if c == '"' {
			t.patterns.doubleQuote.read()
		} else {
			t.patterns.singleQuote.read()
		}
		return t.createToken(tokenValue, t.input.input[start:t.input.position])
	}
	value := t.patterns.attribute.read()
	if value == "" {
		return nil
	}
	if previous.Type == tokenEquals {
		return t.createToken(tokenValue, value)
	}
	return t.createToken(tokenAttribute, value)
}

func (t *tokenizer) isContentUnformatted(tagName string) bool {
	return !containsString(t.options.VoidElements, tagName) &&
		(containsString(t.options.ContentUnformatted, tagName) || containsString(t.options.Unformatted, tagName))
}

func openedElementName(previous *htmlToken) (string, bool) {
	if previous.Type != tokenTagClose || previous.Opened == nil || !strings.HasPrefix(previous.Opened.Text, "<") || strings.HasPrefix(previous.Text, "/") {
		return "", false
	}
	return strings.ToLower(previous.Opened.Text[1:]), true
}

func (t *tokenizer) readRawContent(previous, openToken *htmlToken) *htmlToken {
	value := ""
	if openToken != nil && strings.HasPrefix(openToken.Text, "{") {
		value = t.patterns.handlebarsRawClose.read()
	} else if tagName, ok := openedElementName(previous); ok && t.isContentUnformatted(tagName) {
		value = t.input.readUntil(closeTagMatcher(tagName), false)
	}
	if value == "" {
		return nil
	}
	return t.createToken(tokenText, value)
}

func (t *tokenizer) readScriptAndStyle(c int, previous *htmlToken) *htmlToken {
	tagName, ok := openedElementName(previous)
	if !ok || (tagName != "script" && tagName != "style") {
		return nil
	}
	if token := t.readCommentOrCDATA(c); token != nil {
		token.Type = tokenText
		return token
	}
	if value := t.input.readUntil(closeTagMatcher(tagName), false); value != "" {
		return t.createToken(tokenText, value)
	}
	return nil
}

func (t *tokenizer) readContentWord(c int, openToken *htmlToken) *htmlToken {
	value := ""
	if t.patterns.hasUnformattedContentDelimter && c == int(t.options.UnformattedContentDelimiter[0]) {
		value = t.patterns.unformattedContentDelimiter.read()
	}
	if value == "" {
		if openToken != nil && openToken.Type == tokenControlFlowOpen {
			value = t.patterns.wordControlFlowCloseExcluded.read()
		} else {
			value = t.patterns.word.read()
		}
	}
	if value == "" {
		return nil
	}
	return t.createToken(tokenText, value)
}
