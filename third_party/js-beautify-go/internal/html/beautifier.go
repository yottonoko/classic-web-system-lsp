// Package html implements the HTML formatter.
//
// It is a direct port of js-beautify's src/html beautifier and tokenizer
// (version 1.15.4, the copy vendored by vscode-html-languageservice).
package html

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/yottonoko/js-beautify-go/internal/core"
)

// LangBeautifier formats an embedded language block with inherited options.
type LangBeautifier func(string, map[string]any) (string, error)

var (
	baseIndentPattern         = regexp.MustCompile(`^[\t ]*`)
	handlebarsTagCheckPattern = regexp.MustCompile(`^\{\{~?(?:[\^]|#\*?)?([^` + jsSpaceClass + `}]+)`)
	scriptBeautifierType      = regexp.MustCompile(`module|((text|application|dojo)/(x-)?(javascript|ecmascript|jscript|livescript|(ld\+)?json|method|aspect))`)
	htmlBeautifierType        = regexp.MustCompile(`(text|application|dojo)/(x-)?(html)`)
	trailingBlankLinePattern  = regexp.MustCompile(`\n[ \t]*$`)
	wrappedScriptStartPattern = regexp.MustCompile(`^(<!--|<!\[CDATA\[)`)
	wrappedScriptPattern      = regexp.MustCompile(`^(<!--[^\n]*|<!\[CDATA\[)(\n?)([ \t\n]*)([\s\S]*)(-->|]]>)$`)
	trailingIndentPattern     = regexp.MustCompile(`[ \t]+$`)
)

// Beautifier formats HTML source using normalized options.
type Beautifier struct {
	sourceText string
	options    *Options
	js         LangBeautifier
	css        LangBeautifier
	tagStack   *tagStack

	isWrapAttributesForce                bool
	isWrapAttributesForceExpandMultiline bool
	isWrapAttributesForceAligned         bool
	isWrapAttributesAlignedMultiple      bool
	isWrapAttributesPreserve             bool
	isWrapAttributesPreserveAligned      bool
}

// Beautify formats HTML source and delegates embedded JavaScript and CSS.
func Beautify(source string, options map[string]any, js LangBeautifier, css LangBeautifier) (string, error) {
	opts, err := NewOptions(options)
	if err != nil {
		return "", err
	}
	return NewBeautifier(source, opts, js, css).Beautify()
}

// NewBeautifier creates an HTML beautifier for source, options, and delegates.
func NewBeautifier(source string, options *Options, js LangBeautifier, css LangBeautifier) *Beautifier {
	wrap := options.WrapAttributes
	return &Beautifier{
		sourceText:                           source,
		options:                              options,
		js:                                   js,
		css:                                  css,
		isWrapAttributesForce:                strings.HasPrefix(wrap, "force"),
		isWrapAttributesForceExpandMultiline: wrap == "force-expand-multiline",
		isWrapAttributesForceAligned:         wrap == "force-aligned",
		isWrapAttributesAlignedMultiple:      wrap == "aligned-multiple",
		isWrapAttributesPreserve:             strings.HasPrefix(wrap, "preserve"),
		isWrapAttributesPreserveAligned:      wrap == "preserve-aligned",
	}
}

// printer handles output and printing helpers for the beautifier.
type printer struct {
	indentLevel         int
	alignmentSize       int
	maxPreserveNewlines int
	preserveNewlines    bool
	output              *core.Output
}

func newPrinter(options *Options, baseIndentString string) *printer {
	return &printer{
		maxPreserveNewlines: options.MaxPreserveNewlines,
		preserveNewlines:    options.PreserveNewlines,
		output:              core.NewOutput(core.OutputOptionsFromBase(options.BaseOptions), baseIndentString),
	}
}

func (p *printer) setSpaceBeforeToken(value, nonBreaking bool) {
	p.output.SpaceBeforeToken = value
	p.output.NonBreakingSpace = nonBreaking
}

func (p *printer) setWrapPoint() {
	p.output.SetIndent(p.indentLevel, p.alignmentSize)
	p.output.SetWrapPoint()
}

func (p *printer) addRawToken(token *htmlToken) {
	p.output.AddRawToken(&core.Token{Text: token.Text, Newlines: token.Newlines, WhitespaceBefore: token.WhitespaceBefore})
}

func (p *printer) printPreservedNewlines(token *htmlToken) bool {
	newlines := 0
	if token.Type != tokenText && token.Previous.Type != tokenText && token.Newlines > 0 {
		newlines = 1
	}
	if p.preserveNewlines {
		newlines = min(token.Newlines, p.maxPreserveNewlines+1)
	}
	for n := 0; n < newlines; n++ {
		p.printNewline(n > 0)
	}
	return newlines != 0
}

func (p *printer) traverseWhitespace(token *htmlToken) bool {
	if token.WhitespaceBefore != "" || token.Newlines > 0 {
		if !p.printPreservedNewlines(token) {
			p.output.SpaceBeforeToken = true
		}
		return true
	}
	return false
}

func (p *printer) printNewline(force bool) {
	p.output.AddNewLine(force)
}

func (p *printer) printToken(token *htmlToken) {
	if token.Text != "" {
		p.output.SetIndent(p.indentLevel, p.alignmentSize)
		p.output.AddToken(token.Text)
	}
}

func (p *printer) indent() {
	p.indentLevel++
}

func (p *printer) deindent() {
	if p.indentLevel > 0 {
		p.indentLevel--
		p.output.SetIndent(p.indentLevel, p.alignmentSize)
	}
}

func (p *printer) fullIndent(level int) string {
	level += p.indentLevel
	if level < 1 {
		return ""
	}
	return p.output.GetIndentString(level, 0)
}

// parserToken covers both TagOpenParserToken and the plain parser tokens
// js-beautify creates for other token types; unused fields stay zero.
type parserToken struct {
	parent               *parserToken
	text                 string
	tokenType            string
	tagName              string
	isInlineElement      bool
	isUnformatted        bool
	isContentUnformatted bool
	isEmptyElement       bool
	isStartTag           bool
	isEndTag             bool
	indentContent        bool
	multilineContent     bool
	customBeautifierName string
	startTagToken        *parserToken
	attrCount            int
	hasWrappedAttrs      bool
	alignmentSize        int
	tagComplete          bool
	tagStartChar         byte
	tagCheck             string
}

func newTagOpenParserToken(options *Options, parent *parserToken, raw *htmlToken) *parserToken {
	token := &parserToken{parent: parent, tokenType: tokenTagOpen}
	if raw == nil {
		token.tagComplete = true
		return token
	}
	token.tagStartChar = raw.Text[0]
	token.text = raw.Text
	if token.tagStartChar == '<' {
		// Like /^<([^\s>]*)/.
		end := 1
		for end < len(raw.Text) {
			r, size := utf8.DecodeRuneInString(raw.Text[end:])
			if r == '>' || isJSSpace(r) {
				break
			}
			end += size
		}
		token.tagCheck = raw.Text[1:end]
	} else {
		if match := handlebarsTagCheckPattern.FindStringSubmatch(raw.Text); match != nil {
			token.tagCheck = match[1]
		}
		if (strings.HasPrefix(raw.Text, "{{#>") || strings.HasPrefix(raw.Text, "{{~#>")) && strings.HasPrefix(token.tagCheck, ">") {
			if token.tagCheck == ">" && raw.Next != nil {
				token.tagCheck = strings.Split(raw.Next.Text, " ")[0]
			} else {
				token.tagCheck = strings.Split(raw.Text, ">")[1]
			}
		}
	}
	token.tagCheck = strings.ToLower(token.tagCheck)
	if raw.Type == tokenComment {
		token.tagComplete = true
	}
	token.isStartTag = !strings.HasPrefix(token.tagCheck, "/")
	token.tagName = token.tagCheck
	if !token.isStartTag {
		token.tagName = token.tagCheck[1:]
	}
	token.isEndTag = !token.isStartTag || (raw.Closed != nil && raw.Closed.Text == "/>")
	// Handlebars tags start after "{{" or "{{~".
	handlebarStarts := 2
	if token.tagStartChar == '{' && len(token.text) >= 3 && token.text[2] == '~' {
		handlebarStarts = 3
	}
	// Handlebars tags that do not start with # or ^ are single tags, as are
	// all of them when handlebars indentation is off.
	if token.tagStartChar == '{' && (!options.IndentHandlebars || len(token.text) < 3 ||
		(handlebarStarts < len(token.text) && token.text[handlebarStarts] != '#' && token.text[handlebarStarts] != '^')) {
		token.isEndTag = true
	}
	return token
}

type tagFrame struct {
	parent      *tagFrame
	tag         string
	indentLevel int
	parserToken *parserToken
}

type tagStack struct {
	printer *printer
	current *tagFrame
}

func (s *tagStack) parserToken() *parserToken {
	if s.current == nil {
		return nil
	}
	return s.current.parserToken
}

func (s *tagStack) recordTag(token *parserToken) {
	s.current = &tagFrame{parent: s.current, tag: token.tagName, indentLevel: s.printer.indentLevel, parserToken: token}
}

func (s *tagStack) tryPopFrame(frame *tagFrame) *parserToken {
	if frame == nil {
		return nil
	}
	s.printer.indentLevel = frame.indentLevel
	s.current = frame.parent
	return frame.parserToken
}

func (s *tagStack) frame(tags []string, stop []string) *tagFrame {
	for frame := s.current; frame != nil; frame = frame.parent {
		if containsString(tags, frame.tag) {
			return frame
		}
		if containsString(stop, frame.tag) {
			return nil
		}
	}
	return nil
}

func (s *tagStack) tryPop(tag string, stop ...string) *parserToken {
	return s.tryPopFrame(s.frame([]string{tag}, stop))
}

func (s *tagStack) indentToTag(tags []string) {
	if frame := s.frame(tags, nil); frame != nil {
		s.printer.indentLevel = frame.indentLevel
	}
}

// Beautify formats the source text.
func (b *Beautifier) Beautify() (string, error) {
	if b.options.Disabled {
		return b.sourceText, nil
	}
	source := b.sourceText
	eol := b.options.EOL
	if eol == "auto" {
		eol = "\n"
		if index := strings.IndexAny(source, "\r\n"); index >= 0 {
			eol = source[index : index+1]
			if strings.HasPrefix(source[index:], "\r\n") {
				eol = "\r\n"
			}
		}
	}
	// Like upstream, normalize all line breaks before tokenizing.
	source = strings.ReplaceAll(source, "\r\n", "\n")
	source = strings.ReplaceAll(source, "\r", "\n")
	baseIndentString := baseIndentPattern.FindString(source)

	// Only the type of the previous parser token is ever read.
	lastTokenType := ""
	lastTagToken := newTagOpenParserToken(b.options, nil, nil)
	p := newPrinter(b.options, baseIndentString)
	tokens := newTokenizer(source, b.options).tokenize()
	b.tagStack = &tagStack{printer: p}

	for raw := tokens.next(); raw.Type != tokenEOF; raw = tokens.next() {
		switch {
		case raw.Type == tokenTagOpen || raw.Type == tokenComment:
			lastTagToken = b.handleTagOpen(p, raw, lastTagToken, lastTokenType, tokens)
			lastTokenType = lastTagToken.tokenType
		case raw.Type == tokenAttribute || raw.Type == tokenEquals || raw.Type == tokenValue ||
			(raw.Type == tokenText && !lastTagToken.tagComplete):
			b.handleInsideTag(p, raw, lastTagToken, lastTokenType)
			lastTokenType = raw.Type
		case raw.Type == tokenTagClose:
			b.handleTagClose(p, raw, lastTagToken)
			lastTokenType = raw.Type
		case raw.Type == tokenText:
			if err := b.handleText(p, raw, lastTagToken); err != nil {
				return "", err
			}
			lastTokenType = tokenContent
		case raw.Type == tokenControlFlowOpen:
			b.handleControlFlowOpen(p, raw)
			lastTokenType = raw.Type
		case raw.Type == tokenControlFlowClose:
			b.handleControlFlowClose(p, raw)
			lastTokenType = raw.Type
		default:
			p.addRawToken(raw)
		}
	}
	return p.output.GetCode(eol), nil
}

func (b *Beautifier) handleControlFlowOpen(p *printer, raw *htmlToken) {
	p.setSpaceBeforeToken(raw.Newlines > 0 || raw.WhitespaceBefore != "", true)
	if raw.Newlines > 0 {
		p.printPreservedNewlines(raw)
	} else {
		p.setSpaceBeforeToken(raw.Newlines > 0 || raw.WhitespaceBefore != "", true)
	}
	p.printToken(raw)
	p.indent()
}

func (b *Beautifier) handleControlFlowClose(p *printer, raw *htmlToken) {
	p.deindent()
	if raw.Newlines > 0 {
		p.printPreservedNewlines(raw)
	} else {
		p.setSpaceBeforeToken(raw.Newlines > 0 || raw.WhitespaceBefore != "", true)
	}
	p.printToken(raw)
}

func (b *Beautifier) handleTagClose(p *printer, raw *htmlToken, lastTagToken *parserToken) {
	p.alignmentSize = 0
	lastTagToken.tagComplete = true
	p.setSpaceBeforeToken(raw.Newlines > 0 || raw.WhitespaceBefore != "", true)
	if lastTagToken.isUnformatted {
		p.addRawToken(raw)
	} else {
		if lastTagToken.tagStartChar == '<' {
			// Space before "/>", no space before ">".
			p.setSpaceBeforeToken(strings.HasPrefix(raw.Text, "/"), true)
			if b.isWrapAttributesForceExpandMultiline && lastTagToken.hasWrappedAttrs {
				p.printNewline(false)
			}
		}
		p.printToken(raw)
	}
	if lastTagToken.indentContent && !(lastTagToken.isUnformatted || lastTagToken.isContentUnformatted) {
		p.indent()
		// Only indent once per opened tag.
		lastTagToken.indentContent = false
	}
	if !lastTagToken.isInlineElement && !(lastTagToken.isUnformatted || lastTagToken.isContentUnformatted) {
		p.setWrapPoint()
	}
}

func (b *Beautifier) handleInsideTag(p *printer, raw *htmlToken, lastTagToken *parserToken, lastTokenType string) {
	wrapped := lastTagToken.hasWrappedAttrs
	p.setSpaceBeforeToken(raw.Newlines > 0 || raw.WhitespaceBefore != "", true)
	switch {
	case lastTagToken.isUnformatted:
		p.addRawToken(raw)
	case lastTagToken.tagStartChar == '{' && raw.Type == tokenText:
		// Inside handlebars, allow newlines or a single space between the
		// opening and the contents.
		if p.printPreservedNewlines(raw) {
			raw.Newlines = 0
			p.addRawToken(raw)
		} else {
			p.printToken(raw)
		}
	default:
		switch {
		case raw.Type == tokenAttribute:
			p.setSpaceBeforeToken(true, false)
		case raw.Type == tokenEquals:
			p.setSpaceBeforeToken(false, false)
		case raw.Type == tokenValue && raw.Previous.Type == tokenEquals:
			p.setSpaceBeforeToken(false, false)
		}
		if raw.Type == tokenAttribute && lastTagToken.tagStartChar == '<' {
			if b.isWrapAttributesPreserve || b.isWrapAttributesPreserveAligned {
				p.traverseWhitespace(raw)
				wrapped = wrapped || raw.Newlines != 0
			}
			// With force wrapping and at least wrap_attributes_min_attrs
			// attributes, wrap the second and later attributes, and the first
			// one only for force-expand-multiline.
			if b.isWrapAttributesForce && lastTagToken.attrCount >= b.options.WrapAttributesMinAttrs &&
				(lastTokenType != tokenTagOpen || b.isWrapAttributesForceExpandMultiline) {
				p.printNewline(false)
				wrapped = true
			}
		}
		p.printToken(raw)
		wrapped = wrapped || p.output.PreviousTokenWrapped
		lastTagToken.hasWrappedAttrs = wrapped
	}
}

func (b *Beautifier) handleText(p *printer, raw *htmlToken, lastTagToken *parserToken) error {
	switch {
	case lastTagToken.customBeautifierName != "":
		return b.printCustomBeautifierText(p, raw, lastTagToken)
	case lastTagToken.isUnformatted || lastTagToken.isContentUnformatted:
		p.addRawToken(raw)
	default:
		p.traverseWhitespace(raw)
		p.printToken(raw)
	}
	return nil
}

func (b *Beautifier) printCustomBeautifierText(p *printer, raw *htmlToken, lastTagToken *parserToken) error {
	if raw.Text == "" {
		return nil
	}
	text := raw.Text
	var beautifier LangBeautifier
	switch lastTagToken.customBeautifierName {
	case "javascript":
		beautifier = b.js
	case "css":
		beautifier = b.css
	case "html":
		beautifier = func(source string, options map[string]any) (string, error) {
			return Beautify(source, options, b.js, b.css)
		}
	}
	scriptIndentLevel := 1
	switch b.options.IndentScripts {
	case "keep":
		scriptIndentLevel = 0
	case "separate":
		scriptIndentLevel = -p.indentLevel
	}
	indentation := p.fullIndent(scriptIndentLevel)
	// Strip one trailing empty line; it is added back before the closing tag.
	text = removeFirstMatch(trailingBlankLinePattern, text)
	pre, post := "", ""
	if lastTagToken.customBeautifierName != "html" && strings.HasPrefix(text, "<") && wrappedScriptStartPattern.MatchString(text) {
		matched := wrappedScriptPattern.FindStringSubmatch(text)
		// A comment or CDATA wrapper that does not finish is printed raw.
		if matched == nil {
			p.addRawToken(raw)
			return nil
		}
		pre = indentation + matched[1] + "\n"
		text = matched[4]
		if matched[5] != "" {
			post = indentation + matched[5]
		}
		text = removeFirstMatch(trailingBlankLinePattern, text)
		if matched[2] != "" || strings.Contains(matched[3], "\n") {
			// Indentation on the first line of the wrapped text is the basis
			// for indenting when there is no beautifier.
			if indent := trailingIndentPattern.FindString(matched[3]); indent != "" {
				raw.WhitespaceBefore = indent
			}
		}
	}
	if text != "" {
		if beautifier != nil {
			childOptions := make(map[string]any, len(b.options.RawOptions)+1)
			for key, value := range b.options.RawOptions {
				childOptions[key] = value
			}
			childOptions["eol"] = "\n"
			formatted, err := beautifier(indentation+text, childOptions)
			if err != nil {
				return err
			}
			text = formatted
		} else {
			if white := raw.WhitespaceBefore; white != "" {
				text = regexp.MustCompile("\n("+regexp.QuoteMeta(white)+")?").ReplaceAllString(text, "\n")
			}
			text = indentation + strings.ReplaceAll(text, "\n", "\n"+indentation)
		}
	}
	if pre != "" {
		if text == "" {
			text = pre + post
		} else {
			text = pre + text + "\n" + post
		}
	}
	p.printNewline(false)
	if text != "" {
		raw.Text = text
		raw.WhitespaceBefore = ""
		raw.Newlines = 0
		p.addRawToken(raw)
		p.printNewline(true)
	}
	return nil
}

func removeFirstMatch(pattern *regexp.Regexp, text string) string {
	location := pattern.FindStringIndex(text)
	if location == nil {
		return text
	}
	return text[:location[0]] + text[location[1]:]
}

func (b *Beautifier) handleTagOpen(p *printer, raw *htmlToken, lastTagToken *parserToken, lastTokenType string, tokens *tokenStream) *parserToken {
	token := b.tagOpenToken(raw)
	if (lastTagToken.isUnformatted || lastTagToken.isContentUnformatted) && !lastTagToken.isEmptyElement &&
		raw.Type == tokenTagOpen && !token.isStartTag {
		// End tags of unformatted or content_unformatted elements are printed
		// raw to keep any newlines inside them exactly the same.
		p.addRawToken(raw)
		token.startTagToken = b.tagStack.tryPop(token.tagName)
	} else {
		p.traverseWhitespace(raw)
		b.setTagPosition(p, raw, token, lastTagToken, lastTokenType)
		if !token.isInlineElement {
			p.setWrapPoint()
		}
		p.printToken(raw)
	}
	if token.isStartTag && b.isWrapAttributesForce {
		for index := 0; ; index++ {
			peek := tokens.peek(index)
			if peek == nil {
				break
			}
			if peek.Type == tokenAttribute {
				token.attrCount++
			}
			if peek.Type == tokenEOF || peek.Type == tokenTagClose {
				break
			}
		}
	}
	if b.isWrapAttributesForceAligned || b.isWrapAttributesAlignedMultiple || b.isWrapAttributesPreserveAligned {
		token.alignmentSize = utf16Length(raw.Text) + 1
	}
	if !token.tagComplete && !token.isUnformatted {
		p.alignmentSize = token.alignmentSize
	}
	return token
}

func utf16Length(value string) int {
	length := 0
	for _, r := range value {
		length++
		if r > 0xFFFF {
			length++
		}
	}
	return length
}

func (b *Beautifier) tagOpenToken(raw *htmlToken) *parserToken {
	token := newTagOpenParserToken(b.options, b.tagStack.parserToken(), raw)
	token.alignmentSize = b.options.WrapAttributesIndentSize
	token.isEndTag = token.isEndTag || containsString(b.options.VoidElements, token.tagCheck)
	token.isEmptyElement = token.tagComplete || (token.isStartTag && token.isEndTag)
	token.isUnformatted = !token.tagComplete && containsString(b.options.Unformatted, token.tagCheck)
	token.isContentUnformatted = !token.isEmptyElement && containsString(b.options.ContentUnformatted, token.tagCheck)
	token.isInlineElement = containsString(b.options.Inline, token.tagName) ||
		(b.options.InlineCustomElements && strings.Contains(token.tagName, "-")) || token.tagStartChar == '{'
	return token
}

func customBeautifierName(tagCheck string, raw *htmlToken) string {
	if raw.Closed == nil {
		return ""
	}
	typeAttribute := ""
	switch tagCheck {
	case "script":
		typeAttribute = "text/javascript"
	case "style":
		typeAttribute = "text/css"
	}
	if value := typeAttributeValue(raw); value != "" {
		typeAttribute = value
	}
	switch {
	case strings.Contains(typeAttribute, "text/css"):
		return "css"
	case scriptBeautifierType.MatchString(typeAttribute):
		return "javascript"
	case htmlBeautifierType.MatchString(typeAttribute):
		return "html"
	case strings.Contains(typeAttribute, "test/null"):
		// Test-only type for checking the beautifier with no delegate.
		return "null"
	}
	return ""
}

func typeAttributeValue(start *htmlToken) string {
	for raw := start.Next; raw != nil && raw.Type != tokenEOF && start.Closed != raw; raw = raw.Next {
		if raw.Type == tokenAttribute && raw.Text == "type" {
			if raw.Next != nil && raw.Next.Type == tokenEquals && raw.Next.Next != nil && raw.Next.Next.Type == tokenValue {
				return raw.Next.Next.Text
			}
			return ""
		}
	}
	return ""
}

func (b *Beautifier) setTagPosition(p *printer, raw *htmlToken, token, lastTagToken *parserToken, lastTokenType string) {
	if !token.isEmptyElement {
		if token.isEndTag {
			// Close this element and any open descendants.
			token.startTagToken = b.tagStack.tryPop(token.tagName)
		} else {
			// Close an element with an optional end tag that this start tag ends.
			if b.doOptionalEndElement(token) && !token.isInlineElement {
				p.printNewline(false)
			}
			b.tagStack.recordTag(token)
			if (token.tagName == "script" || token.tagName == "style") && !(token.isUnformatted || token.isContentUnformatted) {
				token.customBeautifierName = customBeautifierName(token.tagCheck, raw)
			}
		}
	}
	if containsString(b.options.ExtraLiners, token.tagCheck) {
		p.printNewline(false)
		if !p.output.JustAddedBlankline() {
			p.printNewline(true)
		}
	}
	switch {
	case token.isEmptyElement:
		// An {{else}} resets the indent to its if, unless, or each block.
		if token.tagStartChar == '{' && token.tagCheck == "else" {
			b.tagStack.indentToTag([]string{"if", "unless", "each"})
			token.indentContent = true
			// No newline when the opening {{#if}} is on the current line.
			if !p.output.CurrentLine.HasMatch("{{#if") {
				p.printNewline(false)
			}
		}
		if token.tagName == "!--" && lastTokenType == tokenTagClose && lastTagToken.isEndTag && !strings.Contains(token.text, "\n") {
			// Leave comments on the same line after an end tag.
		} else {
			if !(token.isInlineElement || token.isUnformatted) {
				p.printNewline(false)
			}
			b.calculateParentMultiline(p, token)
		}
	case token.isEndTag:
		doEndExpand := token.startTagToken != nil && token.startTagToken.multilineContent
		doEndExpand = doEndExpand || (!token.isInlineElement &&
			!(lastTagToken.isInlineElement || lastTagToken.isUnformatted) &&
			!(lastTokenType == tokenTagClose && token.startTagToken == lastTagToken) &&
			lastTokenType != tokenContent)
		if token.isContentUnformatted || token.isUnformatted {
			doEndExpand = false
		}
		if doEndExpand {
			p.printNewline(false)
		}
	default:
		token.indentContent = token.customBeautifierName == ""
		if token.tagStartChar == '<' {
			switch token.tagName {
			case "html":
				token.indentContent = b.options.IndentInnerHTML
			case "head":
				token.indentContent = b.options.IndentHeadInnerHTML
			case "body":
				token.indentContent = b.options.IndentBodyInnerHTML
			}
		}
		if !(token.isInlineElement || token.isUnformatted) && (lastTokenType != tokenContent || token.isContentUnformatted) {
			p.printNewline(false)
		}
		b.calculateParentMultiline(p, token)
	}
}

func (b *Beautifier) calculateParentMultiline(p *printer, token *parserToken) {
	if token.parent != nil && p.output.JustAddedNewline() &&
		!((token.isInlineElement || token.isUnformatted) && token.parent.isInlineElement) {
		token.parent.multilineContent = true
	}
}

// pClosers start elements that end an open <p>.
var pClosers = []string{"address", "article", "aside", "blockquote", "details", "div", "dl", "fieldset", "figcaption", "figure", "footer", "form", "h1", "h2", "h3", "h4", "h5", "h6", "header", "hr", "main", "menu", "nav", "ol", "p", "pre", "section", "table", "ul"}
var pParentExcludes = []string{"a", "audio", "del", "ins", "map", "noscript", "video"}

// doOptionalEndElement closes elements whose end tag may be omitted before
// this start tag (https://www.w3.org/TR/html5/syntax.html#optional-tags).
func (b *Beautifier) doOptionalEndElement(token *parserToken) bool {
	if token.isEmptyElement || !token.isStartTag || token.parent == nil {
		return false
	}
	var result *parserToken
	try := func(tag string, stop ...string) {
		if result == nil {
			result = b.tagStack.tryPop(tag, stop...)
		}
	}
	switch name := token.tagName; {
	case name == "body":
		try("head")
	case name == "li":
		try("li", "ol", "ul", "menu")
	case name == "dd" || name == "dt":
		try("dt", "dl")
		try("dd", "dl")
	case token.parent.tagName == "p" && containsString(pClosers, name):
		if pParent := token.parent.parent; pParent == nil || !containsString(pParentExcludes, pParent.tagName) {
			try("p")
		}
	case name == "rp" || name == "rt":
		try("rt", "ruby", "rtc")
		try("rp", "ruby", "rtc")
	case name == "optgroup":
		try("optgroup", "select")
	case name == "option":
		try("option", "select", "datalist", "optgroup")
	case name == "colgroup":
		try("caption", "table")
	case name == "thead":
		try("caption", "table")
		try("colgroup", "table")
	case name == "tbody" || name == "tfoot":
		try("caption", "table")
		try("colgroup", "table")
		try("thead", "table")
		try("tbody", "table")
	case name == "tr":
		try("caption", "table")
		try("colgroup", "table")
		try("tr", "table", "thead", "tbody", "tfoot")
	case name == "th" || name == "td":
		try("td", "table", "thead", "tbody", "tfoot", "tr")
		try("th", "table", "thead", "tbody", "tfoot", "tr")
	}
	token.parent = b.tagStack.parserToken()
	return result != nil
}
