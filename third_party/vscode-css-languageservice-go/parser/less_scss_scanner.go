package parser

const (
	LessEllipsis TokenType = TokenCustomToken + iota
)

const (
	ScssVariableName TokenType = TokenCustomToken + iota
	ScssInterpolationFunction
	ScssDefault
	ScssEqualsOperator
	ScssNotEqualsOperator
	ScssGreaterEqualsOperator
	ScssSmallerEqualsOperator
	ScssEllipsis
	ScssModule
)

// LESSScanner tokenizes LESS source text.
type LESSScanner struct {
	*Scanner
}

func NewLESSScanner() *LESSScanner {
	return &LESSScanner{Scanner: NewScanner()}
}

func (s *LESSScanner) Scan() Token {
	if triviaToken := s.triviaLess(); triviaToken != nil {
		return *triviaToken
	}
	offset := s.stream.Pos()
	if s.stream.EOS() {
		return s.FinishToken(offset, TokenEOF)
	}
	return s.scanNextLess(offset)
}

func (s *LESSScanner) scanNextLess(offset int) Token {
	if tokenType, ok := s.escapedJavaScript(); ok {
		return s.FinishToken(offset, tokenType)
	}
	if s.stream.AdvanceIfChars([]rune{'.', '.', '.'}) {
		return s.FinishToken(offset, LessEllipsis)
	}
	return s.Scanner.scanNext(offset)
}

func (s *LESSScanner) triviaLess() *Token {
	for {
		offset := s.stream.Pos()
		if s.whitespace() {
			if !s.IgnoreWhitespace {
				token := s.FinishToken(offset, TokenWhitespace)
				return &token
			}
		} else if s.commentLess() {
			if !s.IgnoreComment {
				token := s.FinishToken(offset, TokenComment)
				return &token
			}
		} else {
			return nil
		}
	}
}

func (s *LESSScanner) commentLess() bool {
	if s.comment() {
		return true
	}
	if !s.InURL && s.stream.AdvanceIfChars([]rune{'/', '/'}) {
		s.stream.AdvanceWhileChar(func(ch rune) bool {
			return ch != '\n' && ch != '\r' && ch != '\f'
		})
		return true
	}
	return false
}

func (s *LESSScanner) escapedJavaScript() (TokenType, bool) {
	if s.stream.PeekChar() == '`' {
		s.stream.Advance(1)
		s.stream.AdvanceWhileChar(func(ch rune) bool { return ch != '`' })
		if s.stream.AdvanceIfChar('`') {
			return TokenEscapedJavaScript, true
		}
		return TokenBadEscapedJavaScript, true
	}
	return 0, false
}

// SCSSScanner tokenizes SCSS source text.
type SCSSScanner struct {
	*Scanner
}

func NewSCSSScanner() *SCSSScanner {
	return &SCSSScanner{Scanner: NewScanner()}
}

func (s *SCSSScanner) Scan() Token {
	if triviaToken := s.triviaSCSS(); triviaToken != nil {
		return *triviaToken
	}
	offset := s.stream.Pos()
	if s.stream.EOS() {
		return s.FinishToken(offset, TokenEOF)
	}
	return s.scanNextSCSS(offset)
}

func (s *SCSSScanner) scanNextSCSS(offset int) Token {
	if s.stream.AdvanceIfChar('$') {
		content := []rune{'$'}
		if s.ident(&content) {
			return s.FinishToken(offset, ScssVariableName, string(content))
		}
		s.stream.GoBackTo(offset)
	}
	if s.stream.AdvanceIfChars([]rune{'#', '{'}) {
		return s.FinishToken(offset, ScssInterpolationFunction)
	}
	if s.stream.AdvanceIfChars([]rune{'=', '='}) {
		return s.FinishToken(offset, ScssEqualsOperator)
	}
	if s.stream.AdvanceIfChars([]rune{'!', '='}) {
		return s.FinishToken(offset, ScssNotEqualsOperator)
	}
	if s.stream.AdvanceIfChar('<') {
		if s.stream.AdvanceIfChar('=') {
			return s.FinishToken(offset, ScssSmallerEqualsOperator)
		}
		return s.FinishToken(offset, TokenDelim)
	}
	if s.stream.AdvanceIfChar('>') {
		if s.stream.AdvanceIfChar('=') {
			return s.FinishToken(offset, ScssGreaterEqualsOperator)
		}
		return s.FinishToken(offset, TokenDelim)
	}
	if s.stream.AdvanceIfChars([]rune{'.', '.', '.'}) {
		return s.FinishToken(offset, ScssEllipsis)
	}
	return s.Scanner.scanNext(offset)
}

func (s *SCSSScanner) triviaSCSS() *Token {
	for {
		offset := s.stream.Pos()
		if s.whitespace() {
			if !s.IgnoreWhitespace {
				token := s.FinishToken(offset, TokenWhitespace)
				return &token
			}
		} else if s.commentSCSS() {
			if !s.IgnoreComment {
				token := s.FinishToken(offset, TokenComment)
				return &token
			}
		} else {
			return nil
		}
	}
}

func (s *SCSSScanner) commentSCSS() bool {
	if s.comment() {
		return true
	}
	if !s.InURL && s.stream.AdvanceIfChars([]rune{'/', '/'}) {
		s.stream.AdvanceWhileChar(func(ch rune) bool {
			return ch != '\n' && ch != '\r' && ch != '\f'
		})
		return true
	}
	return false
}
