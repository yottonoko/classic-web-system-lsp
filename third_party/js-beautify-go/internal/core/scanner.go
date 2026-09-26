// Package core contains shared option, scanner, token, and output primitives.
package core

import (
	"regexp"
	"strings"
)

// InputScanner is a small forward scanner modeled after js-beautify's scanner.
type InputScanner struct {
	input    string
	position int
}

// NewInputScanner returns a scanner positioned at the start of input.
func NewInputScanner(input string) *InputScanner {
	return &InputScanner{input: input}
}

// Restart moves the scanner back to the start of input.
func (s *InputScanner) Restart() {
	s.position = 0
}

// Back moves the scanner one byte backward when possible.
func (s *InputScanner) Back() {
	if s.position > 0 {
		s.position--
	}
}

// HasNext reports whether the scanner can read another byte.
func (s *InputScanner) HasNext() bool {
	return s.position < len(s.input)
}

// Next returns the next byte-sized string and advances the scanner.
func (s *InputScanner) Next() string {
	if !s.HasNext() {
		return ""
	}
	value := s.input[s.position : s.position+1]
	s.position++
	return value
}

// Peek returns the byte-sized string at the current position plus index.
func (s *InputScanner) Peek(index ...int) string {
	offset := 0
	if len(index) > 0 {
		offset = index[0]
	}
	pos := s.position + offset
	if pos < 0 || pos >= len(s.input) {
		return ""
	}
	return s.input[pos : pos+1]
}

// Position returns the current byte offset.
func (s *InputScanner) Position() int {
	return s.position
}

// SetPosition clamps and sets the current byte offset.
func (s *InputScanner) SetPosition(pos int) {
	if pos < 0 {
		pos = 0
	}
	if pos > len(s.input) {
		pos = len(s.input)
	}
	s.position = pos
}

// Test reports whether pattern matches at the current position plus index.
func (s *InputScanner) Test(pattern *regexp.Regexp, index ...int) bool {
	offset := 0
	if len(index) > 0 {
		offset = index[0]
	}
	pos := s.position + offset
	if pos < 0 || pos >= len(s.input) {
		return false
	}
	return matchAt(pattern, s.input, pos) != nil
}

// TestChar reports whether a single peeked byte matches pattern.
func (s *InputScanner) TestChar(pattern *regexp.Regexp, index int) bool {
	value := s.Peek(index)
	return value != "" && pattern.MatchString(value)
}

// Match consumes and returns pattern submatches at the current position.
func (s *InputScanner) Match(pattern *regexp.Regexp) []string {
	match := matchAt(pattern, s.input, s.position)
	if match == nil {
		return nil
	}
	s.position += len(match[0])
	return match
}

// Read consumes an optional starting pattern and optional content until a pattern.
func (s *InputScanner) Read(startingPattern *regexp.Regexp, rest ...any) string {
	value := ""
	matched := false
	if startingPattern != nil {
		match := s.Match(startingPattern)
		if match != nil {
			value += match[0]
			matched = true
		}
	} else {
		matched = true
	}
	if len(rest) >= 1 && rest[0] != nil && matched {
		untilPattern := rest[0].(*regexp.Regexp)
		untilAfter := false
		if len(rest) >= 2 {
			untilAfter, _ = rest[1].(bool)
		}
		value += s.ReadUntil(untilPattern, untilAfter)
	}
	return value
}

// ReadUntil consumes text until pattern, optionally including the matched text.
func (s *InputScanner) ReadUntil(pattern *regexp.Regexp, untilAfter ...bool) string {
	after := len(untilAfter) > 0 && untilAfter[0]
	matchIndex := len(s.input)
	matchLen := 0
	if loc := pattern.FindStringIndex(s.input[s.position:]); loc != nil {
		matchIndex = s.position + loc[0]
		matchLen = loc[1] - loc[0]
		if after {
			matchIndex += matchLen
		}
	}
	_ = matchLen
	value := s.input[s.position:matchIndex]
	s.position = matchIndex
	return value
}

// ReadUntilAfter consumes text through the first match for pattern.
func (s *InputScanner) ReadUntilAfter(pattern *regexp.Regexp) string {
	return s.ReadUntil(pattern, true)
}

// PeekUntilAfter returns text through pattern without advancing the scanner.
func (s *InputScanner) PeekUntilAfter(pattern *regexp.Regexp) string {
	start := s.position
	value := s.ReadUntilAfter(pattern)
	s.position = start
	return value
}

// LookBack reports whether testVal appears immediately before the current byte.
func (s *InputScanner) LookBack(testVal string) bool {
	start := s.position - 1
	return start >= len(testVal) &&
		strings.ToLower(s.input[start-len(testVal):start]) == testVal
}

func matchAt(pattern *regexp.Regexp, input string, index int) []string {
	if index < 0 || index > len(input) {
		return nil
	}
	loc := pattern.FindStringIndex(input[index:])
	if loc == nil || loc[0] != 0 {
		return nil
	}
	return pattern.FindStringSubmatch(input[index : index+loc[1]])
}
