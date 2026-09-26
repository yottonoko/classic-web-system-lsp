// Package core contains shared option, scanner, token, and output primitives.
package core

// Token is shared by tokenizers and printers.
type Token struct {
	// Type identifies the token category.
	Type string
	// Text is the token source text.
	Text string
	// CommentsBefore contains comments attached before this token.
	CommentsBefore *TokenStream
	// Newlines records newlines before this token.
	Newlines int
	// WhitespaceBefore records whitespace before this token.
	WhitespaceBefore string
	// Parent links this token to its parent token.
	Parent *Token
	// Next links this token to the next token.
	Next *Token
	// Previous links this token to the previous token.
	Previous *Token
	// Opened links a closing token to its opening token.
	Opened *Token
	// Closed links an opening token to its closing token.
	Closed *Token
	// Directives stores parsed beautify directives.
	Directives map[string]string
	// TagName stores an HTML tag name.
	TagName string
	// Attributes stores parsed HTML attributes.
	Attributes []string
}

// NewToken creates a token with source text and leading trivia metadata.
func NewToken(tokenType, text string, newlines int, whitespaceBefore string) *Token {
	return &Token{
		Type:             tokenType,
		Text:             text,
		Newlines:         newlines,
		WhitespaceBefore: whitespaceBefore,
	}
}

// TokenStream is a small forward-only token stream.
type TokenStream struct {
	tokens   []*Token
	position int
	parent   *Token
}

// NewTokenStream returns an empty token stream with an optional parent token.
func NewTokenStream(parent *Token) *TokenStream {
	return &TokenStream{parent: parent}
}

// Restart moves the stream back to the first token.
func (s *TokenStream) Restart() {
	s.position = 0
}

// IsEmpty reports whether the stream contains no tokens.
func (s *TokenStream) IsEmpty() bool {
	return len(s.tokens) == 0
}

// HasNext reports whether Next can return a token.
func (s *TokenStream) HasNext() bool {
	return s.position < len(s.tokens)
}

// Next returns the next token and advances the stream.
func (s *TokenStream) Next() *Token {
	if !s.HasNext() {
		return nil
	}
	value := s.tokens[s.position]
	s.position++
	return value
}

// Peek returns a token at the current position plus index without advancing.
func (s *TokenStream) Peek(index ...int) *Token {
	offset := 0
	if len(index) > 0 {
		offset = index[0]
	}
	pos := s.position + offset
	if pos < 0 || pos >= len(s.tokens) {
		return nil
	}
	return s.tokens[pos]
}

// Add appends token and assigns the stream parent when present.
func (s *TokenStream) Add(token *Token) {
	if s.parent != nil {
		token.Parent = s.parent
	}
	s.tokens = append(s.tokens, token)
}
