package parser

import "testing"

func TestLESSScannerEscapedJavaScript(t *testing.T) {
	t.Run("Test Escaped JavaScript", func(t *testing.T) {
		assertLESSSingleToken(t, "`", 1, 0, "`", TokenBadEscapedJavaScript)
		assertLESSSingleToken(t, "`a", 2, 0, "`a", TokenBadEscapedJavaScript)
		assertLESSSingleToken(t, "`let a = \"ssss\"`", 16, 0, "`let a = \"ssss\"`", TokenEscapedJavaScript)
		assertLESSSingleToken(t, "`let a = \"ss\ns\"`", 16, 0, "`let a = \"ss\ns\"`", TokenEscapedJavaScript)
	})
}

func TestLESSScannerSingleLineComment(t *testing.T) {
	t.Run("Test Token SingleLineComment", func(t *testing.T) {
		assertLESSSingleToken(t, "//", 0, 2, "", TokenEOF)
		assertLESSSingleToken(t, "//this is a comment test", 0, 24, "", TokenEOF)
		assertLESSSingleToken(t, "// this is a comment test", 0, 25, "", TokenEOF)
		assertLESSSingleToken(t, "// this is a\na", 1, 13, "a", TokenIdent)
		assertLESSSingleToken(t, "// this is a\n// more\n   \n/* comment */a", 1, 38, "a", TokenIdent)
	})
}

func assertLESSSingleToken(t *testing.T, source string, length, offset int, text string, typ TokenType) {
	t.Helper()
	scan := NewLESSScanner()
	scan.SetSource(source)
	token := scan.Scan()
	if token.Len != length || token.Offset != offset || token.Text != text || token.Type != typ {
		t.Fatalf("%q token = %+v, want len=%d offset=%d text=%q type=%d", source, token, length, offset, text, typ)
	}
}
