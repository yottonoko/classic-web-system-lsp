package parser

import (
	"strconv"
	"strings"
)

// TokenType identifies a CSS scanner token kind.
type TokenType int

const (
	TokenIdent TokenType = iota
	TokenAtKeyword
	TokenString
	TokenBadString
	TokenUnquotedString
	TokenHash
	TokenNum
	TokenPercentage
	TokenDimension
	TokenUnicodeRange
	TokenCDO
	TokenCDC
	TokenColon
	TokenSemiColon
	TokenCurlyL
	TokenCurlyR
	TokenParenthesisL
	TokenParenthesisR
	TokenBracketL
	TokenBracketR
	TokenWhitespace
	TokenIncludes
	TokenDashmatch
	TokenSubstringOperator
	TokenPrefixOperator
	TokenSuffixOperator
	TokenDelim
	TokenEMS
	TokenEXS
	TokenLength
	TokenAngle
	TokenTime
	TokenFreq
	TokenExclamation
	TokenResolution
	TokenComma
	TokenCharset
	TokenEscapedJavaScript
	TokenBadEscapedJavaScript
	TokenComment
	TokenSingleLineComment
	TokenEOF
	TokenContainerQueryLength
	TokenCustomToken
)

// Token stores scanner output text, type, and source range.
type Token struct {
	Type   TokenType
	Text   string
	Offset int
	Len    int
}

// MultiLineStream provides rune-based cursor operations for scanners.
type MultiLineStream struct {
	source []rune
	pos    int
}

func NewMultiLineStream(source string) *MultiLineStream {
	return &MultiLineStream{source: []rune(source)}
}

func (s *MultiLineStream) Substring(from int, to ...int) string {
	end := s.pos
	if len(to) > 0 {
		end = to[0]
	}
	if from < 0 {
		from = 0
	}
	if end > len(s.source) {
		end = len(s.source)
	}
	if from > end {
		from = end
	}
	return string(s.source[from:end])
}

func (s *MultiLineStream) EOS() bool {
	return len(s.source) <= s.pos
}

func (s *MultiLineStream) Pos() int {
	return s.pos
}

func (s *MultiLineStream) GoBackTo(pos int) {
	s.pos = pos
}

func (s *MultiLineStream) GoBack(n int) {
	s.pos -= n
}

func (s *MultiLineStream) Advance(n int) {
	s.pos += n
}

func (s *MultiLineStream) NextChar() rune {
	if s.pos >= len(s.source) {
		s.pos++
		return 0
	}
	ch := s.source[s.pos]
	s.pos++
	return ch
}

func (s *MultiLineStream) PeekChar(n ...int) rune {
	offset := 0
	if len(n) > 0 {
		offset = n[0]
	}
	index := s.pos + offset
	if index < 0 || index >= len(s.source) {
		return 0
	}
	return s.source[index]
}

func (s *MultiLineStream) LookbackChar(n int) rune {
	index := s.pos - n
	if index < 0 || index >= len(s.source) {
		return 0
	}
	return s.source[index]
}

func (s *MultiLineStream) AdvanceIfChar(ch rune) bool {
	if s.PeekChar() == ch {
		s.pos++
		return true
	}
	return false
}

func (s *MultiLineStream) AdvanceIfChars(chars []rune) bool {
	if s.pos+len(chars) > len(s.source) {
		return false
	}
	for i, ch := range chars {
		if s.source[s.pos+i] != ch {
			return false
		}
	}
	s.Advance(len(chars))
	return true
}

func (s *MultiLineStream) AdvanceWhileChar(condition func(rune) bool) int {
	posNow := s.pos
	for s.pos < len(s.source) && condition(s.source[s.pos]) {
		s.pos++
	}
	return s.pos - posNow
}

// Scanner tokenizes CSS source text.
type Scanner struct {
	stream           *MultiLineStream
	IgnoreComment    bool
	IgnoreWhitespace bool
	InURL            bool
}

func NewScanner() *Scanner {
	return &Scanner{
		stream:           NewMultiLineStream(""),
		IgnoreComment:    true,
		IgnoreWhitespace: true,
	}
}

func (s *Scanner) SetSource(input string) {
	s.stream = NewMultiLineStream(input)
}

func (s *Scanner) FinishToken(offset int, typ TokenType, text ...string) Token {
	tokenText := s.stream.Substring(offset)
	if len(text) > 0 {
		tokenText = text[0]
	}
	return Token{Offset: offset, Len: s.stream.Pos() - offset, Type: typ, Text: tokenText}
}

func (s *Scanner) Substring(offset, length int) string {
	return s.stream.Substring(offset, offset+length)
}

func (s *Scanner) Pos() int {
	return s.stream.Pos()
}

func (s *Scanner) GoBackTo(pos int) {
	s.stream.GoBackTo(pos)
}

func (s *Scanner) ScanUnquotedString() *Token {
	offset := s.stream.Pos()
	var content []rune
	if s.unquotedString(&content) {
		token := s.FinishToken(offset, TokenUnquotedString, string(content))
		return &token
	}
	return nil
}

func (s *Scanner) Scan() Token {
	if triviaToken := s.trivia(); triviaToken != nil {
		return *triviaToken
	}
	offset := s.stream.Pos()
	if s.stream.EOS() {
		return s.FinishToken(offset, TokenEOF)
	}
	return s.scanNext(offset)
}

func (s *Scanner) TryScanUnicode() *Token {
	offset := s.stream.Pos()
	if !s.stream.EOS() && s.unicodeRange() {
		token := s.FinishToken(offset, TokenUnicodeRange)
		return &token
	}
	s.stream.GoBackTo(offset)
	return nil
}

func (s *Scanner) scanNext(offset int) Token {
	if s.stream.AdvanceIfChars([]rune{'<', '!', '-', '-'}) {
		return s.FinishToken(offset, TokenCDO)
	}
	if s.stream.AdvanceIfChars([]rune{'-', '-', '>'}) {
		return s.FinishToken(offset, TokenCDC)
	}

	var content []rune
	if s.ident(&content) {
		return s.FinishToken(offset, TokenIdent, string(content))
	}

	if s.stream.AdvanceIfChar('@') {
		content = []rune{'@'}
		if s.name(&content) {
			keywordText := string(content)
			if keywordText == "@charset" {
				return s.FinishToken(offset, TokenCharset, keywordText)
			}
			return s.FinishToken(offset, TokenAtKeyword, keywordText)
		}
		return s.FinishToken(offset, TokenDelim)
	}

	if s.stream.AdvanceIfChar('#') {
		content = []rune{'#'}
		if s.name(&content) {
			return s.FinishToken(offset, TokenHash, string(content))
		}
		return s.FinishToken(offset, TokenDelim)
	}

	if s.stream.AdvanceIfChar('!') {
		return s.FinishToken(offset, TokenExclamation)
	}

	if s.number() {
		pos := s.stream.Pos()
		content = []rune(s.stream.Substring(offset, pos))
		if s.stream.AdvanceIfChar('%') {
			return s.FinishToken(offset, TokenPercentage)
		}
		if s.ident(&content) {
			dim := strings.ToLower(s.stream.Substring(pos))
			if typ, ok := staticUnitTable[dim]; ok {
				return s.FinishToken(offset, typ, string(content))
			}
			return s.FinishToken(offset, TokenDimension, string(content))
		}
		return s.FinishToken(offset, TokenNum)
	}

	content = nil
	if tokenType, ok := s.stringToken(&content); ok {
		return s.FinishToken(offset, tokenType, string(content))
	}

	if tokenType, ok := staticTokenTable[s.stream.PeekChar()]; ok {
		s.stream.Advance(1)
		return s.FinishToken(offset, tokenType)
	}

	if s.stream.PeekChar(0) == '~' && s.stream.PeekChar(1) == '=' {
		s.stream.Advance(2)
		return s.FinishToken(offset, TokenIncludes)
	}
	if s.stream.PeekChar(0) == '|' && s.stream.PeekChar(1) == '=' {
		s.stream.Advance(2)
		return s.FinishToken(offset, TokenDashmatch)
	}
	if s.stream.PeekChar(0) == '*' && s.stream.PeekChar(1) == '=' {
		s.stream.Advance(2)
		return s.FinishToken(offset, TokenSubstringOperator)
	}
	if s.stream.PeekChar(0) == '^' && s.stream.PeekChar(1) == '=' {
		s.stream.Advance(2)
		return s.FinishToken(offset, TokenPrefixOperator)
	}
	if s.stream.PeekChar(0) == '$' && s.stream.PeekChar(1) == '=' {
		s.stream.Advance(2)
		return s.FinishToken(offset, TokenSuffixOperator)
	}

	s.stream.NextChar()
	return s.FinishToken(offset, TokenDelim)
}

func (s *Scanner) trivia() *Token {
	for {
		offset := s.stream.Pos()
		if s.whitespace() {
			if !s.IgnoreWhitespace {
				token := s.FinishToken(offset, TokenWhitespace)
				return &token
			}
		} else if s.comment() {
			if !s.IgnoreComment {
				token := s.FinishToken(offset, TokenComment)
				return &token
			}
		} else {
			return nil
		}
	}
}

func (s *Scanner) comment() bool {
	if s.stream.AdvanceIfChars([]rune{'/', '*'}) {
		success := false
		hot := false
		s.stream.AdvanceWhileChar(func(ch rune) bool {
			if hot && ch == '/' {
				success = true
				return false
			}
			hot = ch == '*'
			return true
		})
		if success {
			s.stream.Advance(1)
		}
		return true
	}
	return false
}

func (s *Scanner) number() bool {
	npeek := 0
	hasDot := false
	peekFirst := s.stream.PeekChar()
	if peekFirst == '+' || peekFirst == '-' {
		npeek++
	}
	if s.stream.PeekChar(npeek) == '.' {
		npeek++
		hasDot = true
	}
	ch := s.stream.PeekChar(npeek)
	if isDigit(ch) {
		s.stream.Advance(npeek + 1)
		s.stream.AdvanceWhileChar(func(ch rune) bool {
			return isDigit(ch) || (!hasDot && ch == '.')
		})
		return true
	}
	return false
}

func (s *Scanner) newline(result *[]rune) bool {
	ch := s.stream.PeekChar()
	switch ch {
	case '\r', '\f', '\n':
		s.stream.Advance(1)
		*result = append(*result, ch)
		if ch == '\r' && s.stream.AdvanceIfChar('\n') {
			*result = append(*result, '\n')
		}
		return true
	}
	return false
}

func (s *Scanner) escape(result *[]rune, includeNewLines ...bool) bool {
	include := len(includeNewLines) > 0 && includeNewLines[0]
	ch := s.stream.PeekChar()
	if ch != '\\' {
		return false
	}
	s.stream.Advance(1)
	ch = s.stream.PeekChar()
	hexNumCount := 0
	for hexNumCount < 6 && isHexDigit(ch) {
		s.stream.Advance(1)
		ch = s.stream.PeekChar()
		hexNumCount++
	}
	if hexNumCount > 0 {
		hexText := s.stream.Substring(s.stream.Pos()-hexNumCount, s.stream.Pos())
		if hexVal, err := strconv.ParseInt(hexText, 16, 32); err == nil && hexVal != 0 {
			*result = append(*result, rune(hexVal))
		}
		if ch == ' ' || ch == '\t' {
			s.stream.Advance(1)
		} else {
			empty := []rune{}
			s.newline(&empty)
		}
		return true
	}
	if ch != '\r' && ch != '\f' && ch != '\n' {
		s.stream.Advance(1)
		*result = append(*result, ch)
		return true
	}
	if include {
		return s.newline(result)
	}
	return false
}

func (s *Scanner) stringChar(closeQuote rune, result *[]rune) bool {
	ch := s.stream.PeekChar()
	if ch != 0 && ch != closeQuote && ch != '\\' && ch != '\r' && ch != '\f' && ch != '\n' {
		s.stream.Advance(1)
		*result = append(*result, ch)
		return true
	}
	return false
}

func (s *Scanner) stringToken(result *[]rune) (TokenType, bool) {
	if s.stream.PeekChar() == '\'' || s.stream.PeekChar() == '"' {
		closeQuote := s.stream.NextChar()
		*result = append(*result, closeQuote)
		for s.stringChar(closeQuote, result) || s.escape(result, true) {
		}
		if s.stream.PeekChar() == closeQuote {
			s.stream.NextChar()
			*result = append(*result, closeQuote)
			return TokenString, true
		}
		return TokenBadString, true
	}
	return 0, false
}

func (s *Scanner) unquotedChar(result *[]rune) bool {
	ch := s.stream.PeekChar()
	if ch != 0 && ch != '\\' && ch != '\'' && ch != '"' && ch != '(' && ch != ')' && ch != ' ' && ch != '\t' && ch != '\n' && ch != '\f' && ch != '\r' {
		s.stream.Advance(1)
		*result = append(*result, ch)
		return true
	}
	return false
}

func (s *Scanner) unquotedString(result *[]rune) bool {
	hasContent := false
	for s.unquotedChar(result) || s.escape(result) {
		hasContent = true
	}
	return hasContent
}

func (s *Scanner) whitespace() bool {
	n := s.stream.AdvanceWhileChar(func(ch rune) bool {
		return ch == ' ' || ch == '\t' || ch == '\n' || ch == '\f' || ch == '\r'
	})
	return n > 0
}

func (s *Scanner) name(result *[]rune) bool {
	matched := false
	for s.identChar(result) || s.escape(result) {
		matched = true
	}
	return matched
}

func (s *Scanner) ident(result *[]rune) bool {
	pos := s.stream.Pos()
	hasMinus := s.minus(result)
	if hasMinus {
		if s.minus(result) || s.identFirstChar(result) || s.escape(result) {
			for s.identChar(result) || s.escape(result) {
			}
			return true
		}
	} else if s.identFirstChar(result) || s.escape(result) {
		for s.identChar(result) || s.escape(result) {
		}
		return true
	}
	s.stream.GoBackTo(pos)
	return false
}

func (s *Scanner) identFirstChar(result *[]rune) bool {
	ch := s.stream.PeekChar()
	if ch == '_' || (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= 0x80 && ch <= 0xFFFF) {
		s.stream.Advance(1)
		*result = append(*result, ch)
		return true
	}
	return false
}

func (s *Scanner) minus(result *[]rune) bool {
	ch := s.stream.PeekChar()
	if ch == '-' {
		s.stream.Advance(1)
		*result = append(*result, ch)
		return true
	}
	return false
}

func (s *Scanner) identChar(result *[]rune) bool {
	ch := s.stream.PeekChar()
	if ch == '_' || ch == '-' || (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || isDigit(ch) || (ch >= 0x80 && ch <= 0xFFFF) {
		s.stream.Advance(1)
		*result = append(*result, ch)
		return true
	}
	return false
}

func (s *Scanner) unicodeRange() bool {
	if s.stream.AdvanceIfChar('+') {
		codePoints := s.stream.AdvanceWhileChar(isHexDigit) + s.stream.AdvanceWhileChar(func(ch rune) bool { return ch == '?' })
		if codePoints >= 1 && codePoints <= 6 {
			if s.stream.AdvanceIfChar('-') {
				digits := s.stream.AdvanceWhileChar(isHexDigit)
				return digits >= 1 && digits <= 6
			}
			return true
		}
	}
	return false
}

var staticTokenTable = map[rune]TokenType{
	';': TokenSemiColon,
	':': TokenColon,
	'{': TokenCurlyL,
	'}': TokenCurlyR,
	']': TokenBracketR,
	'[': TokenBracketL,
	'(': TokenParenthesisL,
	')': TokenParenthesisR,
	',': TokenComma,
}

var staticUnitTable = map[string]TokenType{
	"em":    TokenEMS,
	"ex":    TokenEXS,
	"px":    TokenLength,
	"cm":    TokenLength,
	"mm":    TokenLength,
	"in":    TokenLength,
	"pt":    TokenLength,
	"pc":    TokenLength,
	"deg":   TokenAngle,
	"rad":   TokenAngle,
	"grad":  TokenAngle,
	"ms":    TokenTime,
	"s":     TokenTime,
	"hz":    TokenFreq,
	"khz":   TokenFreq,
	"%":     TokenPercentage,
	"fr":    TokenPercentage,
	"dpi":   TokenResolution,
	"dpcm":  TokenResolution,
	"cqw":   TokenContainerQueryLength,
	"cqh":   TokenContainerQueryLength,
	"cqi":   TokenContainerQueryLength,
	"cqb":   TokenContainerQueryLength,
	"cqmin": TokenContainerQueryLength,
	"cqmax": TokenContainerQueryLength,
}

func isDigit(ch rune) bool {
	return ch >= '0' && ch <= '9'
}

func isHexDigit(ch rune) bool {
	return isDigit(ch) || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')
}
