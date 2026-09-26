package htmlservice

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// TokenType identifies the kind of token returned by the scanner.
type TokenType int

const (
	TokenTypeStartCommentTag TokenType = iota
	TokenTypeComment
	TokenTypeEndCommentTag
	TokenTypeStartTagOpen
	TokenTypeStartTagClose
	TokenTypeStartTagSelfClose
	TokenTypeStartTag
	TokenTypeEndTagOpen
	TokenTypeEndTagClose
	TokenTypeEndTag
	TokenTypeDelimiterAssign
	TokenTypeAttributeName
	TokenTypeAttributeValue
	TokenTypeStartDoctypeTag
	TokenTypeDoctype
	TokenTypeEndDoctypeTag
	TokenTypeContent
	TokenTypeWhitespace
	TokenTypeUnknown
	TokenTypeScript
	TokenTypeStyles
	TokenTypeEOS
)

// ScannerState identifies the current state of the HTML scanner.
type ScannerState int

const (
	ScannerStateWithinContent ScannerState = iota
	ScannerStateAfterOpeningStartTag
	ScannerStateAfterOpeningEndTag
	ScannerStateWithinDoctype
	ScannerStateWithinTag
	ScannerStateWithinEndTag
	ScannerStateWithinComment
	ScannerStateWithinScriptContent
	ScannerStateWithinStyleContent
	ScannerStateAfterAttributeName
	ScannerStateBeforeAttributeValue
)

// Scanner tokenizes HTML input and exposes token metadata.
type Scanner interface {
	Scan() TokenType
	GetTokenType() TokenType
	GetTokenOffset() int
	GetTokenLength() int
	GetTokenEnd() int
	GetTokenByteOffset() int
	GetTokenByteLength() int
	GetTokenByteEnd() int
	GetTokenText() string
	GetTokenError() string
	GetScannerState() ScannerState
}

type stream struct {
	source string
	pos    int
}

func (s *stream) eos() bool { return len(s.source) <= s.pos }
func (s *stream) goBackTo(pos int) {
	s.pos = pos
}
func (s *stream) goBack(n int) {
	s.pos -= n
	if s.pos < 0 {
		s.pos = 0
	}
}
func (s *stream) advance(n int) {
	s.pos += n
	if s.pos > len(s.source) {
		s.pos = len(s.source)
	}
}
func (s *stream) advanceRune() {
	if s.eos() {
		return
	}
	_, size := utf8.DecodeRuneInString(s.source[s.pos:])
	if size <= 0 {
		size = 1
	}
	s.advance(size)
}
func (s *stream) goToEnd() { s.pos = len(s.source) }
func (s *stream) peekChar(n ...int) byte {
	offset := 0
	if len(n) > 0 {
		offset = n[0]
	}
	idx := s.pos + offset
	if idx < 0 || idx >= len(s.source) {
		return 0
	}
	return s.source[idx]
}
func (s *stream) advanceIfChar(ch byte) bool {
	if s.pos < len(s.source) && s.source[s.pos] == ch {
		s.pos++
		return true
	}
	return false
}
func (s *stream) advanceIfChars(chars []byte) bool {
	if s.pos+len(chars) > len(s.source) {
		return false
	}
	for i, ch := range chars {
		if s.source[s.pos+i] != ch {
			return false
		}
	}
	s.pos += len(chars)
	return true
}
func (s *stream) advanceIfRegexp(re *regexp.Regexp) string {
	loc := re.FindStringIndex(s.source[s.pos:])
	if loc == nil {
		return ""
	}
	s.pos += loc[1]
	return s.source[s.pos-loc[1]+loc[0] : s.pos]
}
func (s *stream) advanceElementName() string {
	if s.eos() || !isElementNameStartByte(s.source[s.pos]) {
		return ""
	}
	start := s.pos
	s.pos++
	hasUpper := false
	if 'A' <= s.source[start] && s.source[start] <= 'Z' {
		hasUpper = true
	}
	for s.pos < len(s.source) && isElementNameByte(s.source[s.pos]) {
		if 'A' <= s.source[s.pos] && s.source[s.pos] <= 'Z' {
			hasUpper = true
		}
		s.pos++
	}
	value := s.source[start:s.pos]
	if hasUpper {
		return strings.ToLower(value)
	}
	return value
}
func (s *stream) advanceAttributeName() string {
	start := s.pos
	hasUpper := false
	for s.pos < len(s.source) {
		ch := s.source[s.pos]
		if ch < utf8.RuneSelf {
			if isExcludedAttributeNameByte(ch) {
				break
			}
			if 'A' <= ch && ch <= 'Z' {
				hasUpper = true
			}
			s.pos++
			continue
		}
		r, size := utf8.DecodeRuneInString(s.source[s.pos:])
		if isJSWhitespaceRune(r) {
			break
		}
		s.pos += size
	}
	if s.pos == start {
		return ""
	}
	value := s.source[start:s.pos]
	if hasUpper {
		return strings.ToLower(value)
	}
	return value
}
func (s *stream) advanceUnquotedAttributeValue() string {
	start := s.pos
	for s.pos < len(s.source) {
		ch := s.source[s.pos]
		if ch < utf8.RuneSelf {
			if isExcludedUnquotedValueByte(ch) {
				break
			}
			s.pos++
			continue
		}
		r, size := utf8.DecodeRuneInString(s.source[s.pos:])
		if isJSWhitespaceRune(r) {
			break
		}
		s.pos += size
	}
	return s.source[start:s.pos]
}
func (s *stream) advanceIfDoctype() bool {
	const doctype = "!doctype"
	if !asciiHasPrefixFold(s.source[s.pos:], doctype) {
		return false
	}
	s.pos += len(doctype)
	return true
}
func (s *stream) advanceUntilRegexp(re *regexp.Regexp) string {
	loc := re.FindStringIndex(s.source[s.pos:])
	if loc == nil {
		s.goToEnd()
		return ""
	}
	s.pos += loc[0]
	return s.source[s.pos : s.pos+loc[1]-loc[0]]
}
func (s *stream) advanceUntilChar(ch byte) bool {
	for s.pos < len(s.source) {
		if s.source[s.pos] == ch {
			return true
		}
		s.pos++
	}
	return false
}
func (s *stream) advanceUntilChars(chars []byte) bool {
	for s.pos+len(chars) <= len(s.source) {
		ok := true
		for i, ch := range chars {
			if s.source[s.pos+i] != ch {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
		s.pos++
	}
	s.goToEnd()
	return false
}
func (s *stream) advanceUntilStyleClose() {
	for s.pos < len(s.source) {
		idx := strings.IndexByte(s.source[s.pos:], '<')
		if idx < 0 {
			s.goToEnd()
			return
		}
		s.pos += idx
		if asciiHasPrefixFold(s.source[s.pos:], "</style") {
			return
		}
		s.pos++
	}
}
func (s *stream) skipWhitespace() bool {
	start := s.pos
	for s.pos < len(s.source) {
		switch s.source[s.pos] {
		case ' ', '\t', '\n', '\f', '\r':
			s.pos++
		default:
			return s.pos > start
		}
	}
	return s.pos > start
}

type htmlScanner struct {
	stream               *stream
	textIndex            *utf16Index
	state                ScannerState
	tokenOffset          int
	tokenByteEnd         int
	tokenOffsetCU        int
	tokenEndCU           int
	tokenOffsetCUValid   bool
	tokenEndCUValid      bool
	tokenType            TokenType
	tokenError           string
	tokenTextOverride    string
	hasTokenTextOverride bool
	hasSpaceAfterTag     bool
	lastTag              string
	lastAttributeName    string
	lastTypeValue        string
	emitPseudoCloseTags  bool
	pending              *pendingScannerToken
	publicOffsetOverride *int
	publicEndOverride    *int
}

type pendingScannerToken struct {
	tokenType    TokenType
	byteOffset   int
	byteEnd      int
	offsetCU     int
	endCU        int
	textOverride string
	tokenError   string
	state        ScannerState
}

var (
	jsWhitespaceRE       = `\s\x{000B}\x{00A0}\x{1680}\x{2000}-\x{200A}\x{2028}\x{2029}\x{202F}\x{205F}\x{3000}\x{FEFF}`
	scriptNameRE         = `[sS][cC][rR][iI][pP][tT]`
	styleNameRE          = `[sS][tT][yY][lL][eE]`
	scriptContentRE      = regexp.MustCompile(`<!--|-->|</?` + scriptNameRE + `[` + jsWhitespaceRE + `]*/?>?`)
	htmlScriptTypeValues = map[string]bool{
		"text/x-handlebars-template": true,
		"text/html":                  true,
	}
)

func CreateScanner(input string, initialOffset ...int) Scanner {
	offset := 0
	state := ScannerStateWithinContent
	emitPseudoCloseTags := false
	if len(initialOffset) > 0 {
		offset = initialOffset[0]
	}
	if len(initialOffset) > 1 {
		state = ScannerState(initialOffset[1])
	}
	if len(initialOffset) > 2 {
		emitPseudoCloseTags = initialOffset[2] != 0
	}
	return newScannerAtUTF16(input, offset, state, emitPseudoCloseTags)
}

func NewScanner(input string, initialOffset int, initialState ScannerState, emitPseudoCloseTags bool) Scanner {
	return newScannerAtUTF16(input, initialOffset, initialState, emitPseudoCloseTags)
}

func newScannerAtUTF16(input string, initialOffset int, initialState ScannerState, emitPseudoCloseTags bool) Scanner {
	var textIndex *utf16Index
	if initialOffset != 0 {
		index := newUTF16Index(input)
		textIndex = &index
	}
	info := utf16OffsetInfoForPosition(input, initialOffset)
	prefixByteEnd, prefixText, hasPrefixText := scannerInitialPrefix(input, initialOffset)
	scanner := &htmlScanner{
		stream:               &stream{source: input, pos: info.byteOffset},
		textIndex:            textIndex,
		state:                initialState,
		tokenByteEnd:         prefixByteEnd,
		tokenEndCU:           initialOffset,
		tokenOffsetCUValid:   true,
		tokenEndCUValid:      true,
		tokenType:            TokenTypeUnknown,
		tokenTextOverride:    prefixText,
		hasTokenTextOverride: hasPrefixText,
		emitPseudoCloseTags:  emitPseudoCloseTags,
	}
	if initialOffset < 0 {
		if initialState == ScannerStateWithinContent && len(input) > 0 && input[0] == '<' {
			scanner.pending = &pendingScannerToken{
				tokenType:    TokenTypeContent,
				byteOffset:   0,
				byteEnd:      0,
				offsetCU:     initialOffset,
				endCU:        0,
				textOverride: "",
				state:        ScannerStateWithinContent,
			}
		} else {
			scanner.publicOffsetOverride = scannerIntPtr(initialOffset)
		}
	} else if textIndex != nil && initialOffset > textIndex.len() {
		scanner.publicOffsetOverride = scannerIntPtr(initialOffset)
		scanner.publicEndOverride = scannerIntPtr(initialOffset)
	}
	if info.inside {
		scanner.queueInitialSurrogateToken(info, initialState)
	}
	return scanner
}

func scannerInitialPrefix(input string, initialOffset int) (int, string, bool) {
	if initialOffset <= 0 {
		return 0, "", false
	}
	units := 0
	for offset, r := range input {
		width := 1
		_, size := utf8.DecodeRuneInString(input[offset:])
		if r > 0xFFFF {
			width = 2
		}
		if units+width > initialOffset {
			if r > 0xFFFF && initialOffset == units+1 {
				high, _ := utf16EncodeRune(r)
				return offset + size, input[:offset] + utf16CodeUnitString(high), true
			}
			return offset, "", false
		}
		units += width
		if units >= initialOffset {
			return offset + size, "", false
		}
	}
	return len(input), "", false
}

func newScannerAtByte(input string, initialOffset int, initialState ScannerState, emitPseudoCloseTags bool) Scanner {
	return newScannerAtByteWithIndex(input, initialOffset, initialState, emitPseudoCloseTags, nil)
}

func newScannerAtByteWithIndex(input string, initialOffset int, initialState ScannerState, emitPseudoCloseTags bool, textIndex *utf16Index) Scanner {
	if initialOffset < 0 {
		initialOffset = 0
	}
	if initialOffset > len(input) {
		initialOffset = len(input)
	}
	return &htmlScanner{stream: &stream{source: input, pos: initialOffset}, textIndex: textIndex, state: initialState, emitPseudoCloseTags: emitPseudoCloseTags}
}

func (s *htmlScanner) nextElementName() string {
	return s.stream.advanceElementName()
}

func (s *htmlScanner) nextAttributeName() string {
	return s.stream.advanceAttributeName()
}

func (s *htmlScanner) finish(offset int, typ TokenType, err ...string) TokenType {
	s.tokenType = typ
	s.tokenOffset = offset
	s.tokenByteEnd = s.stream.pos
	s.tokenOffsetCUValid = false
	s.tokenEndCUValid = false
	if s.publicOffsetOverride != nil {
		s.tokenOffsetCU = *s.publicOffsetOverride
		s.tokenOffsetCUValid = true
		s.publicOffsetOverride = nil
	}
	if s.publicEndOverride != nil {
		s.tokenEndCU = *s.publicEndOverride
		s.tokenEndCUValid = true
		s.publicEndOverride = nil
	}
	s.tokenError = ""
	s.tokenTextOverride = ""
	s.hasTokenTextOverride = false
	if len(err) > 0 {
		s.tokenError = err[0]
	}
	return typ
}

func (s *htmlScanner) finishWithPublic(offsetByte, endByte, offsetCU, endCU int, typ TokenType, textOverride string, err ...string) TokenType {
	s.tokenType = typ
	s.tokenOffset = offsetByte
	s.tokenByteEnd = endByte
	s.tokenOffsetCU = offsetCU
	s.tokenEndCU = endCU
	s.tokenOffsetCUValid = true
	s.tokenEndCUValid = true
	s.tokenError = ""
	s.tokenTextOverride = textOverride
	s.hasTokenTextOverride = true
	if len(err) > 0 {
		s.tokenError = err[0]
	}
	return typ
}

func (s *htmlScanner) finishPending(p pendingScannerToken) TokenType {
	s.state = p.state
	return s.finishWithPublic(p.byteOffset, p.byteEnd, p.offsetCU, p.endCU, p.tokenType, p.textOverride, p.tokenError)
}

func (s *htmlScanner) Scan() TokenType {
	if s.pending != nil {
		pending := *s.pending
		s.pending = nil
		return s.finishPending(pending)
	}
	offset := s.stream.pos
	token := s.internalScan()
	if token != TokenTypeEOS && offset == s.stream.pos && !(s.emitPseudoCloseTags && (token == TokenTypeStartTagClose || token == TokenTypeEndTagClose)) {
		return s.advanceOneUTF16Unit(offset, TokenTypeUnknown, s.state, "")
	}
	return token
}

func (s *htmlScanner) internalScan() TokenType {
	offset := s.stream.pos
	if s.stream.eos() {
		return s.finish(offset, TokenTypeEOS)
	}
	errorMessage := ""
	switch s.state {
	case ScannerStateWithinComment:
		if s.stream.advanceIfChars([]byte{'-', '-', '>'}) {
			s.state = ScannerStateWithinContent
			return s.finish(offset, TokenTypeEndCommentTag)
		}
		s.stream.advanceUntilChars([]byte{'-', '-', '>'})
		return s.finish(offset, TokenTypeComment)
	case ScannerStateWithinDoctype:
		if s.stream.advanceIfChar('>') {
			s.state = ScannerStateWithinContent
			return s.finish(offset, TokenTypeEndDoctypeTag)
		}
		s.stream.advanceUntilChar('>')
		return s.finish(offset, TokenTypeDoctype)
	case ScannerStateWithinContent:
		if s.stream.advanceIfChar('<') {
			if !s.stream.eos() && s.stream.peekChar() == '!' {
				if s.stream.advanceIfChars([]byte{'!', '-', '-'}) {
					s.state = ScannerStateWithinComment
					return s.finish(offset, TokenTypeStartCommentTag)
				}
				if s.stream.advanceIfDoctype() {
					s.state = ScannerStateWithinDoctype
					return s.finish(offset, TokenTypeStartDoctypeTag)
				}
			}
			if s.stream.advanceIfChar('/') {
				s.state = ScannerStateAfterOpeningEndTag
				return s.finish(offset, TokenTypeEndTagOpen)
			}
			s.state = ScannerStateAfterOpeningStartTag
			return s.finish(offset, TokenTypeStartTagOpen)
		}
		s.stream.advanceUntilChar('<')
		return s.finish(offset, TokenTypeContent)
	case ScannerStateAfterOpeningEndTag:
		tagName := s.nextElementName()
		if tagName != "" {
			s.state = ScannerStateWithinEndTag
			return s.finish(offset, TokenTypeEndTag)
		}
		if s.stream.skipWhitespace() {
			return s.finish(offset, TokenTypeWhitespace, "Tag name must directly follow the open bracket.")
		}
		s.state = ScannerStateWithinEndTag
		s.stream.advanceUntilChar('>')
		if offset < s.stream.pos {
			return s.finish(offset, TokenTypeUnknown, "End tag name expected.")
		}
		return s.internalScan()
	case ScannerStateWithinEndTag:
		if s.stream.skipWhitespace() {
			return s.finish(offset, TokenTypeWhitespace)
		}
		if s.stream.advanceIfChar('>') {
			s.state = ScannerStateWithinContent
			return s.finish(offset, TokenTypeEndTagClose)
		}
		if s.emitPseudoCloseTags && s.stream.peekChar() == '<' {
			s.state = ScannerStateWithinContent
			return s.finish(offset, TokenTypeEndTagClose, "Closing bracket missing.")
		}
		errorMessage = "Closing bracket expected."
	case ScannerStateAfterOpeningStartTag:
		s.lastTag = s.nextElementName()
		s.lastTypeValue = ""
		s.lastAttributeName = ""
		if s.lastTag != "" {
			s.hasSpaceAfterTag = false
			s.state = ScannerStateWithinTag
			return s.finish(offset, TokenTypeStartTag)
		}
		if s.stream.skipWhitespace() {
			return s.finish(offset, TokenTypeWhitespace, "Tag name must directly follow the open bracket.")
		}
		s.state = ScannerStateWithinTag
		s.stream.advanceUntilChar('>')
		if offset < s.stream.pos {
			return s.finish(offset, TokenTypeUnknown, "Start tag name expected.")
		}
		return s.internalScan()
	case ScannerStateWithinTag:
		if s.stream.skipWhitespace() {
			s.hasSpaceAfterTag = true
			return s.finish(offset, TokenTypeWhitespace)
		}
		if s.emitPseudoCloseTags && s.stream.peekChar() == '<' {
			s.state = ScannerStateWithinContent
			return s.finish(offset, TokenTypeStartTagClose, "Closing bracket missing.")
		}
		if s.hasSpaceAfterTag {
			s.lastAttributeName = s.nextAttributeName()
			if s.lastAttributeName != "" {
				s.state = ScannerStateAfterAttributeName
				s.hasSpaceAfterTag = false
				return s.finish(offset, TokenTypeAttributeName)
			}
		}
		if s.stream.advanceIfChars([]byte{'/', '>'}) {
			s.state = ScannerStateWithinContent
			return s.finish(offset, TokenTypeStartTagSelfClose)
		}
		if s.stream.advanceIfChar('>') {
			switch s.lastTag {
			case "script":
				if s.lastTypeValue != "" && htmlScriptTypeValues[s.lastTypeValue] {
					s.state = ScannerStateWithinContent
				} else {
					s.state = ScannerStateWithinScriptContent
				}
			case "style":
				s.state = ScannerStateWithinStyleContent
			default:
				s.state = ScannerStateWithinContent
			}
			return s.finish(offset, TokenTypeStartTagClose)
		}
		return s.advanceOneUTF16Unit(offset, TokenTypeUnknown, ScannerStateWithinTag, "Unexpected character in tag.")
	case ScannerStateAfterAttributeName:
		if s.stream.skipWhitespace() {
			s.hasSpaceAfterTag = true
			return s.finish(offset, TokenTypeWhitespace)
		}
		if s.stream.advanceIfChar('=') {
			s.state = ScannerStateBeforeAttributeValue
			return s.finish(offset, TokenTypeDelimiterAssign)
		}
		s.state = ScannerStateWithinTag
		return s.internalScan()
	case ScannerStateBeforeAttributeValue:
		if s.stream.skipWhitespace() {
			return s.finish(offset, TokenTypeWhitespace)
		}
		attributeValue := s.stream.advanceUnquotedAttributeValue()
		if attributeValue != "" {
			if s.stream.peekChar() == '>' && s.stream.peekChar(-1) == '/' {
				s.stream.goBack(1)
				attributeValue = attributeValue[:len(attributeValue)-1]
			}
			if s.lastAttributeName == "type" {
				s.lastTypeValue = attributeValue
			}
			if attributeValue != "" {
				s.state = ScannerStateWithinTag
				s.hasSpaceAfterTag = false
				return s.finish(offset, TokenTypeAttributeValue)
			}
		}
		ch := s.stream.peekChar()
		if ch == '\'' || ch == '"' {
			s.stream.advance(1)
			if s.stream.advanceUntilChar(ch) {
				s.stream.advance(1)
			}
			if s.lastAttributeName == "type" {
				end := s.stream.pos - 1
				if end < offset+1 {
					end = s.stream.pos
				}
				if end >= offset+1 {
					s.lastTypeValue = s.stream.source[offset+1 : end]
				}
			}
			s.state = ScannerStateWithinTag
			s.hasSpaceAfterTag = false
			return s.finish(offset, TokenTypeAttributeValue)
		}
		s.state = ScannerStateWithinTag
		s.hasSpaceAfterTag = false
		return s.internalScan()
	case ScannerStateWithinScriptContent:
		scriptState := 1
		for !s.stream.eos() {
			match := s.stream.advanceIfRegexp(scriptContentRE)
			if match == "" {
				s.stream.goToEnd()
				return s.finish(offset, TokenTypeScript)
			}
			lower := strings.ToLower(match)
			if lower == "<!--" {
				if scriptState == 1 {
					scriptState = 2
				}
			} else if lower == "-->" {
				scriptState = 1
			} else if len(match) > 1 && match[1] != '/' {
				if scriptState == 2 {
					scriptState = 3
				}
			} else if scriptState == 3 {
				scriptState = 2
			} else {
				s.stream.goBack(len(match))
				break
			}
		}
		s.state = ScannerStateWithinContent
		if offset < s.stream.pos {
			return s.finish(offset, TokenTypeScript)
		}
		return s.internalScan()
	case ScannerStateWithinStyleContent:
		s.stream.advanceUntilStyleClose()
		s.state = ScannerStateWithinContent
		if offset < s.stream.pos {
			return s.finish(offset, TokenTypeStyles)
		}
		return s.internalScan()
	}
	return s.advanceOneUTF16Unit(offset, TokenTypeUnknown, ScannerStateWithinContent, errorMessage)
}

func (s *htmlScanner) GetTokenType() TokenType { return s.tokenType }
func (s *htmlScanner) GetTokenOffset() int {
	if s.tokenOffsetCUValid {
		return s.tokenOffsetCU
	}
	return s.codeUnitsAt(s.tokenOffset)
}
func (s *htmlScanner) GetTokenLength() int {
	return s.GetTokenEnd() - s.GetTokenOffset()
}
func (s *htmlScanner) GetTokenEnd() int {
	if s.tokenEndCUValid {
		return s.tokenEndCU
	}
	return s.codeUnitsAt(s.tokenByteEnd)
}
func (s *htmlScanner) GetTokenByteOffset() int { return s.tokenOffset }
func (s *htmlScanner) GetTokenByteLength() int { return s.tokenByteEnd - s.tokenOffset }
func (s *htmlScanner) GetTokenByteEnd() int    { return s.tokenByteEnd }
func (s *htmlScanner) GetTokenText() string {
	if s.hasTokenTextOverride {
		return s.tokenTextOverride
	}
	return s.stream.source[s.tokenOffset:s.tokenByteEnd]
}
func (s *htmlScanner) GetTokenError() string         { return s.tokenError }
func (s *htmlScanner) GetScannerState() ScannerState { return s.state }

func (s *htmlScanner) codeUnitsAt(offset int) int {
	if s.textIndex == nil {
		index := newUTF16Index(s.stream.source)
		s.textIndex = &index
	}
	return s.textIndex.codeUnitsAt(offset)
}

func (s *htmlScanner) textLengthCU() int {
	if s.textIndex == nil {
		index := newUTF16Index(s.stream.source)
		s.textIndex = &index
	}
	return s.textIndex.len()
}

func (s *htmlScanner) advanceOneUTF16Unit(offset int, typ TokenType, nextState ScannerState, err string) TokenType {
	if offset >= len(s.stream.source) {
		s.state = nextState
		return s.finish(offset, typ, err)
	}
	r, size := utf8.DecodeRuneInString(s.stream.source[offset:])
	if r > 0xFFFF {
		high, low := utf16EncodeRune(r)
		offsetCU := s.codeUnitsAt(offset)
		endByte := offset + size
		s.state = nextState
		if nextState == ScannerStateWithinContent {
			pendingEndByte := endByte
			pendingEndCU := offsetCU + 2
			pendingText := utf16CodeUnitString(low)
			for pendingEndByte < len(s.stream.source) && s.stream.source[pendingEndByte] != '<' {
				nextRune, nextSize := utf8.DecodeRuneInString(s.stream.source[pendingEndByte:])
				pendingText += s.stream.source[pendingEndByte : pendingEndByte+nextSize]
				pendingEndByte += nextSize
				if nextRune > 0xFFFF {
					pendingEndCU += 2
				} else {
					pendingEndCU++
				}
			}
			s.stream.goBackTo(pendingEndByte)
			s.pending = &pendingScannerToken{
				tokenType:    TokenTypeContent,
				byteOffset:   endByte,
				byteEnd:      pendingEndByte,
				offsetCU:     offsetCU + 1,
				endCU:        pendingEndCU,
				textOverride: pendingText,
				state:        ScannerStateWithinContent,
			}
		} else {
			s.stream.goBackTo(endByte)
			s.pending = &pendingScannerToken{
				tokenType:    typ,
				byteOffset:   endByte,
				byteEnd:      endByte,
				offsetCU:     offsetCU + 1,
				endCU:        offsetCU + 2,
				textOverride: utf16CodeUnitString(low),
				tokenError:   err,
				state:        nextState,
			}
		}
		return s.finishWithPublic(offset, endByte, offsetCU, offsetCU+1, typ, utf16CodeUnitString(high), err)
	}
	s.stream.advanceRune()
	s.state = nextState
	return s.finish(offset, typ, err)
}

func (s *htmlScanner) queueInitialSurrogateToken(info utf16OffsetInfo, initialState ScannerState) {
	tokenType := TokenTypeUnknown
	err := ""
	endByte := info.byteOffset
	endCU := info.unitStart + 1
	text := utf16CodeUnitString(info.unit)
	state := initialState
	if initialState == ScannerStateWithinContent {
		tokenType = TokenTypeContent
		for endByte < len(s.stream.source) && s.stream.source[endByte] != '<' {
			r, size := utf8.DecodeRuneInString(s.stream.source[endByte:])
			text += s.stream.source[endByte : endByte+size]
			endByte += size
			if r > 0xFFFF {
				endCU += 2
			} else {
				endCU++
			}
		}
		state = ScannerStateWithinContent
	} else if initialState == ScannerStateAfterOpeningStartTag {
		tokenType, endByte, endCU, text, err, state = initialSurrogateTagToken(s.stream.source, info, TokenTypeUnknown, "Start tag name expected.", ScannerStateWithinTag)
	} else if initialState == ScannerStateAfterAttributeName {
		err = "Unexpected character in tag."
		state = ScannerStateWithinTag
	} else if initialState == ScannerStateBeforeAttributeValue {
		tokenType, endByte, endCU, text, err, state = initialSurrogateBeforeAttributeValueToken(s.stream.source, info)
	} else if initialState == ScannerStateWithinTag {
		err = "Unexpected character in tag."
	} else if initialState == ScannerStateWithinEndTag {
		err = "Closing bracket expected."
		state = ScannerStateWithinContent
	} else if initialState == ScannerStateAfterOpeningEndTag {
		tokenType, endByte, endCU, text, err, state = initialSurrogateTagToken(s.stream.source, info, TokenTypeUnknown, "End tag name expected.", ScannerStateWithinEndTag)
	} else if initialState == ScannerStateWithinComment {
		tokenType, endByte, endCU, text, err, state = initialSurrogateRawToken(s.stream.source, info, initialState)
	} else if initialState == ScannerStateWithinDoctype {
		tokenType, endByte, endCU, text, err, state = initialSurrogateRawToken(s.stream.source, info, initialState)
	} else if initialState == ScannerStateWithinScriptContent {
		tokenType, endByte, endCU, text, err, state = initialSurrogateRawToken(s.stream.source, info, initialState)
	} else if initialState == ScannerStateWithinStyleContent {
		tokenType, endByte, endCU, text, err, state = initialSurrogateRawToken(s.stream.source, info, initialState)
	}
	s.stream.goBackTo(endByte)
	s.pending = &pendingScannerToken{
		tokenType:    tokenType,
		byteOffset:   info.byteOffset,
		byteEnd:      endByte,
		offsetCU:     info.unitStart,
		endCU:        endCU,
		textOverride: text,
		tokenError:   err,
		state:        state,
	}
}

func initialSurrogateBeforeAttributeValueToken(source string, info utf16OffsetInfo) (TokenType, int, int, string, string, ScannerState) {
	if info.byteOffset >= len(source) {
		return TokenTypeAttributeValue, info.byteOffset, info.unitStart + 1, utf16CodeUnitString(info.unit), "", ScannerStateWithinTag
	}
	if source[info.byteOffset] == '/' && info.byteOffset+1 < len(source) && source[info.byteOffset+1] == '>' {
		return TokenTypeAttributeValue, info.byteOffset, info.unitStart + 1, utf16CodeUnitString(info.unit), "", ScannerStateWithinTag
	}
	r, _ := utf8.DecodeRuneInString(source[info.byteOffset:])
	if isJSWhitespaceRune(r) || strings.ContainsRune("\"'`=<>", r) {
		return TokenTypeAttributeValue, info.byteOffset, info.unitStart + 1, utf16CodeUnitString(info.unit), "", ScannerStateWithinTag
	}
	return initialSurrogateReplayToken(source, info, ScannerStateBeforeAttributeValue)
}

func initialSurrogateRawToken(source string, info utf16OffsetInfo, initialState ScannerState) (TokenType, int, int, string, string, ScannerState) {
	if initialRawDelimiterAt(source, info.byteOffset, initialState) {
		token := TokenTypeUnknown
		state := initialState
		switch initialState {
		case ScannerStateWithinComment:
			token = TokenTypeComment
		case ScannerStateWithinDoctype:
			token = TokenTypeDoctype
		case ScannerStateWithinScriptContent:
			token = TokenTypeScript
			state = ScannerStateWithinContent
		case ScannerStateWithinStyleContent:
			token = TokenTypeStyles
			state = ScannerStateWithinContent
		}
		return token, info.byteOffset, info.unitStart + 1, utf16CodeUnitString(info.unit), "", state
	}
	return initialSurrogateReplayToken(source, info, initialState)
}

func initialSurrogateReplayToken(source string, info utf16OffsetInfo, initialState ScannerState) (TokenType, int, int, string, string, ScannerState) {
	scanner, _ := newScannerAtByte(source, info.byteOffset, initialState, false).(*htmlScanner)
	token := scanner.Scan()
	text := utf16CodeUnitString(info.unit) + scanner.GetTokenText()
	endCU := info.unitStart + 1 + scanner.GetTokenLength()
	return token, scanner.GetTokenByteEnd(), endCU, text, scanner.GetTokenError(), scanner.GetScannerState()
}

func initialSurrogateTagToken(source string, info utf16OffsetInfo, token TokenType, err string, state ScannerState) (TokenType, int, int, string, string, ScannerState) {
	endByte := info.byteOffset
	endCU := info.unitStart + 1
	text := utf16CodeUnitString(info.unit)
	for endByte < len(source) && source[endByte] != '>' {
		r, size := utf8.DecodeRuneInString(source[endByte:])
		text += source[endByte : endByte+size]
		endByte += size
		if r > 0xFFFF {
			endCU += 2
		} else {
			endCU++
		}
	}
	return token, endByte, endCU, text, err, state
}

func initialRawDelimiterAt(source string, offset int, state ScannerState) bool {
	if offset >= len(source) {
		return false
	}
	rest := source[offset:]
	switch state {
	case ScannerStateWithinComment:
		return strings.HasPrefix(rest, "-->")
	case ScannerStateWithinDoctype:
		return rest[0] == '>'
	case ScannerStateWithinScriptContent:
		return asciiHasPrefixFold(rest, "</script")
	case ScannerStateWithinStyleContent:
		return asciiHasPrefixFold(rest, "</style")
	default:
		return false
	}
}

func asciiHasPrefixFold(value, prefix string) bool {
	return len(value) >= len(prefix) && strings.EqualFold(value[:len(prefix)], prefix)
}

func utf16CodeUnitString(unit uint16) string {
	if unit < 0x80 {
		return string([]byte{byte(unit)})
	}
	if unit < 0x800 {
		return string([]byte{byte(0xC0 | unit>>6), byte(0x80 | unit&0x3F)})
	}
	return string([]byte{byte(0xE0 | unit>>12), byte(0x80 | ((unit >> 6) & 0x3F)), byte(0x80 | (unit & 0x3F))})
}

func scannerIntPtr(v int) *int { return &v }

func isElementNameStartByte(ch byte) bool {
	return ch == '_' || ch == ':' || 'A' <= ch && ch <= 'Z' || 'a' <= ch && ch <= 'z'
}

func isElementNameByte(ch byte) bool {
	return isElementNameStartByte(ch) || '0' <= ch && ch <= '9' || ch == '-' || ch == '.'
}

func isExcludedAttributeNameByte(ch byte) bool {
	if ch <= 0x0f || 0x7f <= ch && ch <= 0x9f {
		return true
	}
	switch ch {
	case ' ', '\t', '\n', '\v', '\f', '\r', '"', '\'', '>', '/', '=':
		return true
	default:
		return false
	}
}

func isExcludedUnquotedValueByte(ch byte) bool {
	switch ch {
	case ' ', '\t', '\n', '\v', '\f', '\r', '"', '\'', '`', '=', '<', '>':
		return true
	default:
		return false
	}
}
