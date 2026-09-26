// Package unpackers contains safe Go decoders for supported packed JavaScript forms.
package unpackers

import (
	"net/url"
	"strings"
)

// URLencoded detects bookmarklet-style percent-encoded JavaScript.
type URLencoded struct{}

// Detect reports whether source looks like percent-encoded JavaScript.
func (URLencoded) Detect(source string) bool {
	if strings.Contains(source, " ") {
		return false
	}
	if strings.Contains(source, "%2") {
		return true
	}
	return strings.Count(source, "%") > 3
}

// Unpack decodes percent-encoded JavaScript when the source is URL-encoded.
func (u URLencoded) Unpack(source string) string {
	if !u.Detect(source) {
		return source
	}
	if strings.Contains(source, "%2B") || strings.Contains(source, "%2b") {
		source = strings.ReplaceAll(source, "+", "%20")
	}
	result, err := url.QueryUnescape(source)
	if err != nil {
		return source
	}
	return result
}
