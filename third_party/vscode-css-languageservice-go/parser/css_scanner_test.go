package parser

import "testing"

func assertSingleToken(t *testing.T, scan *Scanner, source string, length, offset int, text string, tokenTypes ...TokenType) {
	t.Helper()
	scan.SetSource(source)
	token := scan.Scan()
	if token.Len != length {
		t.Fatalf("%q len = %d, want %d", source, token.Len, length)
	}
	if token.Offset != offset {
		t.Fatalf("%q offset = %d, want %d", source, token.Offset, offset)
	}
	if token.Text != text {
		t.Fatalf("%q text = %q, want %q", source, token.Text, text)
	}
	if token.Type != tokenTypes[0] {
		t.Fatalf("%q type = %d, want %d", source, token.Type, tokenTypes[0])
	}
	for i := 1; i < len(tokenTypes); i++ {
		if got := scan.Scan().Type; got != tokenTypes[i] {
			t.Fatalf("%q type[%d] = %d, want %d", source, i, got, tokenTypes[i])
		}
	}
	if got := scan.Scan().Type; got != TokenEOF {
		t.Fatalf("%q final type = %d, want EOF", source, got)
	}
}

func TestCSSScannerWhitespace(t *testing.T) {
	t.Run("Whitespace", func(t *testing.T) {
		scanner := NewScanner()
		assertSingleToken(t, scanner, " @", 1, 1, "@", TokenDelim)
		assertSingleToken(t, scanner, " /* comment*/ \n/*comment*/@", 1, 26, "@", TokenDelim)

		scanner = NewScanner()
		scanner.IgnoreWhitespace = false
		assertSingleToken(t, scanner, " @", 1, 0, " ", TokenWhitespace, TokenDelim)
		assertSingleToken(t, scanner, "/*comment*/ @", 1, 11, " ", TokenWhitespace, TokenDelim)

		scanner = NewScanner()
		scanner.IgnoreComment = false
		assertSingleToken(t, scanner, " /*comment*/@", 11, 1, "/*comment*/", TokenComment, TokenDelim)
		assertSingleToken(t, scanner, "/*comment*/ @", 11, 0, "/*comment*/", TokenComment, TokenDelim)
	})
}

func TestCSSScannerIdent(t *testing.T) {
	t.Run("Token Ident", func(t *testing.T) {
		scanner := NewScanner()
		assertSingleToken(t, scanner, "\u060frf", 3, 0, "\u060frf", TokenIdent)
		assertSingleToken(t, scanner, "über", 4, 0, "über", TokenIdent)
		assertSingleToken(t, scanner, "-bo", 3, 0, "-bo", TokenIdent)
		assertSingleToken(t, scanner, "_bo", 3, 0, "_bo", TokenIdent)
		assertSingleToken(t, scanner, "boo", 3, 0, "boo", TokenIdent)
		assertSingleToken(t, scanner, "Boo", 3, 0, "Boo", TokenIdent)
		assertSingleToken(t, scanner, "red--", 5, 0, "red--", TokenIdent)
		assertSingleToken(t, scanner, "red-->", 5, 0, "red--", TokenIdent, TokenDelim)
		assertSingleToken(t, scanner, "--red", 5, 0, "--red", TokenIdent)
		assertSingleToken(t, scanner, "--100", 5, 0, "--100", TokenIdent)
		assertSingleToken(t, scanner, "---red", 6, 0, "---red", TokenIdent)
		assertSingleToken(t, scanner, "---", 3, 0, "---", TokenIdent)
		assertSingleToken(t, scanner, `a\.b`, 4, 0, `a.b`, TokenIdent)
		assertSingleToken(t, scanner, `\E9motion`, 9, 0, "émotion", TokenIdent)
		assertSingleToken(t, scanner, `\E9 dition`, 10, 0, "édition", TokenIdent)
		assertSingleToken(t, scanner, `\0000E9dition`, 13, 0, "édition", TokenIdent)
		assertSingleToken(t, scanner, `S\0000e9f`, 9, 0, "Séf", TokenIdent)
	})
}

func TestCSSScannerURL(t *testing.T) {
	t.Run("Token Url", func(t *testing.T) {
		scanner := NewScanner()
		assertURLArgument := func(source, text string, tokenType TokenType) {
			t.Helper()
			scanner.SetSource(source)
			token := scanner.ScanUnquotedString()
			if token == nil {
				t.Fatalf("%q did not produce URL token", source)
			}
			if token.Len != len([]rune(text)) || token.Offset != 0 || token.Text != text || token.Type != tokenType {
				t.Fatalf("%q token = %+v, want len=%d offset=0 text=%q type=%d", source, *token, len([]rune(text)), text, tokenType)
			}
		}
		assertURLArgument("http://msft.com", "http://msft.com", TokenUnquotedString)
		assertURLArgument("http://msft.com'", "http://msft.com", TokenUnquotedString)
	})
}

func TestCSSScannerAtKeyword(t *testing.T) {
	t.Run("Token AtKeyword", func(t *testing.T) {
		scanner := NewScanner()
		assertSingleToken(t, scanner, "@import", 7, 0, "@import", TokenAtKeyword)
		assertSingleToken(t, scanner, "@importttt", 10, 0, "@importttt", TokenAtKeyword)
		assertSingleToken(t, scanner, "@imp", 4, 0, "@imp", TokenAtKeyword)
		assertSingleToken(t, scanner, "@5", 2, 0, "@5", TokenAtKeyword)
		assertSingleToken(t, scanner, "@media", 6, 0, "@media", TokenAtKeyword)
		assertSingleToken(t, scanner, "@page", 5, 0, "@page", TokenAtKeyword)
		assertSingleToken(t, scanner, "@charset", 8, 0, "@charset", TokenCharset)
		assertSingleToken(t, scanner, "@-mport", 7, 0, "@-mport", TokenAtKeyword)
		assertSingleToken(t, scanner, "@ðmport", 7, 0, "@ðmport", TokenAtKeyword)
		assertSingleToken(t, scanner, "@apply", 6, 0, "@apply", TokenAtKeyword)
		assertSingleToken(t, scanner, "@", 1, 0, "@", TokenDelim)
	})
}

func TestCSSScannerNumberDelimHashDimensionStringAndOperators(t *testing.T) {
	scanner := NewScanner()
	t.Run("Token Number", func(t *testing.T) {
		assertSingleToken(t, scanner, "1234", 4, 0, "1234", TokenNum)
		assertSingleToken(t, scanner, "1.34", 4, 0, "1.34", TokenNum)
		assertSingleToken(t, scanner, ".234", 4, 0, ".234", TokenNum)
		assertSingleToken(t, scanner, ".234.", 4, 0, ".234", TokenNum, TokenDelim)
		assertSingleToken(t, scanner, "..234", 1, 0, ".", TokenDelim, TokenNum)
	})

	t.Run("Token Delim", func(t *testing.T) {
		assertSingleToken(t, scanner, "@", 1, 0, "@", TokenDelim)
		assertSingleToken(t, scanner, "+", 1, 0, "+", TokenDelim)
		assertSingleToken(t, scanner, ">", 1, 0, ">", TokenDelim)
		assertSingleToken(t, scanner, "#", 1, 0, "#", TokenDelim)
		assertSingleToken(t, scanner, "'", 1, 0, "'", TokenBadString)
		assertSingleToken(t, scanner, `"`, 1, 0, `"`, TokenBadString)
	})

	t.Run("Token Hash", func(t *testing.T) {
		assertSingleToken(t, scanner, "#import", 7, 0, "#import", TokenHash)
		assertSingleToken(t, scanner, "#-mport", 7, 0, "#-mport", TokenHash)
		assertSingleToken(t, scanner, "#123", 4, 0, "#123", TokenHash)
	})

	t.Run("Token Dimension/Percentage", func(t *testing.T) {
		assertSingleToken(t, scanner, "3em", 3, 0, "3em", TokenEMS)
		assertSingleToken(t, scanner, "4.423ex", 7, 0, "4.423ex", TokenEXS)
		assertSingleToken(t, scanner, "3423px", 6, 0, "3423px", TokenLength)
		assertSingleToken(t, scanner, "4.423cm", 7, 0, "4.423cm", TokenLength)
		assertSingleToken(t, scanner, "4.423mm", 7, 0, "4.423mm", TokenLength)
		assertSingleToken(t, scanner, "4.423in", 7, 0, "4.423in", TokenLength)
		assertSingleToken(t, scanner, "4.423pt", 7, 0, "4.423pt", TokenLength)
		assertSingleToken(t, scanner, "4.423pc", 7, 0, "4.423pc", TokenLength)
		assertSingleToken(t, scanner, "4.423deg", 8, 0, "4.423deg", TokenAngle)
		assertSingleToken(t, scanner, "4.423rad", 8, 0, "4.423rad", TokenAngle)
		assertSingleToken(t, scanner, "4.423grad", 9, 0, "4.423grad", TokenAngle)
		assertSingleToken(t, scanner, "4.423ms", 7, 0, "4.423ms", TokenTime)
		assertSingleToken(t, scanner, "4.423s", 6, 0, "4.423s", TokenTime)
		assertSingleToken(t, scanner, "4.423hz", 7, 0, "4.423hz", TokenFreq)
		assertSingleToken(t, scanner, ".423khz", 7, 0, ".423khz", TokenFreq)
		assertSingleToken(t, scanner, "3.423%", 6, 0, "3.423%", TokenPercentage)
		assertSingleToken(t, scanner, ".423%", 5, 0, ".423%", TokenPercentage)
		assertSingleToken(t, scanner, ".423ft", 6, 0, ".423ft", TokenDimension)
		assertSingleToken(t, scanner, "200dpi", 6, 0, "200dpi", TokenResolution)
		assertSingleToken(t, scanner, "123dpcm", 7, 0, "123dpcm", TokenResolution)
	})

	t.Run("Token String", func(t *testing.T) {
		assertSingleToken(t, scanner, "'farboo'", 8, 0, "'farboo'", TokenString)
		assertSingleToken(t, scanner, `"farboo"`, 8, 0, `"farboo"`, TokenString)
		assertSingleToken(t, scanner, "\"farboð\"", 8, 0, "\"farboð\"", TokenString)
		assertSingleToken(t, scanner, `"far\"oo"`, 9, 0, `"far"oo"`, TokenString)
		assertSingleToken(t, scanner, "\"fa\\\noo\"", 8, 0, "\"fa\noo\"", TokenString)
		assertSingleToken(t, scanner, "\"fa\\\roo\"", 8, 0, "\"fa\roo\"", TokenString)
		assertSingleToken(t, scanner, "\"fa\\\foo\"", 8, 0, "\"fa\foo\"", TokenString)
		assertSingleToken(t, scanner, "'farboo\"", 8, 0, "'farboo\"", TokenBadString)
		assertSingleToken(t, scanner, "'farboo", 7, 0, "'farboo", TokenBadString)
	})

	t.Run("Token CDO", func(t *testing.T) {
		assertSingleToken(t, scanner, "<!--", 4, 0, "<!--", TokenCDO)
		assertSingleToken(t, scanner, "<!-\n-", 1, 0, "<", TokenDelim, TokenExclamation, TokenDelim, TokenDelim)
	})
	t.Run("Token CDC", func(t *testing.T) {
		assertSingleToken(t, scanner, "-->", 3, 0, "-->", TokenCDC)
		assertSingleToken(t, scanner, "--y>", 3, 0, "--y", TokenIdent, TokenDelim)
		assertSingleToken(t, scanner, "--<", 2, 0, "--", TokenIdent, TokenDelim)
	})

	t.Run("Token singletokens ;:{}[]()", func(t *testing.T) {
		assertSingleToken(t, scanner, ":  ", 1, 0, ":", TokenColon)
		assertSingleToken(t, scanner, ";  ", 1, 0, ";", TokenSemiColon)
		assertSingleToken(t, scanner, "{  ", 1, 0, "{", TokenCurlyL)
		assertSingleToken(t, scanner, "}  ", 1, 0, "}", TokenCurlyR)
		assertSingleToken(t, scanner, "[  ", 1, 0, "[", TokenBracketL)
		assertSingleToken(t, scanner, "]  ", 1, 0, "]", TokenBracketR)
		assertSingleToken(t, scanner, "(  ", 1, 0, "(", TokenParenthesisL)
		assertSingleToken(t, scanner, ")  ", 1, 0, ")", TokenParenthesisR)
	})

	t.Run("Token dashmatch & includes", func(t *testing.T) {
		assertSingleToken(t, scanner, "~=", 2, 0, "~=", TokenIncludes)
		assertSingleToken(t, scanner, "~", 1, 0, "~", TokenDelim)
		assertSingleToken(t, scanner, "|=", 2, 0, "|=", TokenDashmatch)
		assertSingleToken(t, scanner, "|", 1, 0, "|", TokenDelim)
		assertSingleToken(t, scanner, "^=", 2, 0, "^=", TokenPrefixOperator)
		assertSingleToken(t, scanner, "$=", 2, 0, "$=", TokenSuffixOperator)
		assertSingleToken(t, scanner, "*=", 2, 0, "*=", TokenSubstringOperator)
	})
}

func TestCSSScannerCommentsWhitespacesAndSequences(t *testing.T) {
	scanner := NewScanner()
	t.Run("Comments", func(t *testing.T) {
		assertSingleToken(t, scanner, "/*      */", 0, 10, "", TokenEOF)
		assertSingleToken(t, scanner, "/*      abcd*/", 0, 14, "", TokenEOF)
		assertSingleToken(t, scanner, "/*abcd  */", 0, 10, "", TokenEOF)
		assertSingleToken(t, scanner, "/* ab- .-cd  */", 0, 15, "", TokenEOF)
	})
	t.Run("Whitespaces", func(t *testing.T) {
		assertSingleToken(t, scanner, " ", 0, 1, "", TokenEOF)
		assertSingleToken(t, scanner, "      ", 0, 6, "", TokenEOF)
	})

	t.Run("Token Sequence", func(t *testing.T) {
		assertTokenSequence(t, scanner, "5 5 5 5", TokenNum, TokenNum, TokenNum, TokenNum)
		assertTokenSequence(t, scanner, "/* 5 4 */-->", TokenCDC)
		assertTokenSequence(t, scanner, "/* 5 4 */ -->", TokenCDC)
		assertTokenSequence(t, scanner, `/* "adaasd" */ -->`, TokenCDC)
		assertTokenSequence(t, scanner, "/* <!-- */ -->", TokenCDC)
		assertTokenSequence(t, scanner, "red-->", TokenIdent, TokenDelim)
		assertTokenSequence(t, scanner, "@ import", TokenDelim, TokenIdent)
	})
}

func assertTokenSequence(t *testing.T, scan *Scanner, source string, tokens ...TokenType) {
	t.Helper()
	scan.SetSource(source)
	token := scan.Scan()
	for i, want := range tokens {
		if token.Type != want {
			t.Fatalf("%q token[%d] = %d, want %d", source, i, token.Type, want)
		}
		token = scan.Scan()
	}
}
