// Package unpackers contains safe Go decoders for supported packed JavaScript forms.
package unpackers

import (
	"regexp"
	"strconv"
	"strings"
)

// JavascriptObfuscator expands simple javascript-obfuscator string arrays.
type JavascriptObfuscator struct{}

var jsObfuscatorStartRE = regexp.MustCompile(`^var _0x[a-f0-9]+ ?= ?\[`)
var jsObfuscatorArrayRE = regexp.MustCompile(`(?s)var (_0x[a-f\d]+) ?= ?\[(.*?)\];`)

// Detect reports whether source starts with a supported obfuscator array.
func (JavascriptObfuscator) Detect(source string) bool {
	return jsObfuscatorStartRE.MatchString(source)
}

// Unpack replaces supported string-array lookups with string literals.
func (j JavascriptObfuscator) Unpack(source string) string {
	if !j.Detect(source) {
		return source
	}
	matches := jsObfuscatorArrayRE.FindStringSubmatchIndex(source)
	if matches == nil {
		return source
	}
	fullEnd := matches[1]
	varName := source[matches[2]:matches[3]]
	items := smartSplit(source[matches[4]:matches[5]])
	result := source[fullEnd:]
	for index, item := range items {
		re := regexp.MustCompile(regexp.QuoteMeta(varName) + `\[` + strconv.Itoa(index) + `\]`)
		result = re.ReplaceAllString(result, fixQuotes(unescapeHex(item)))
	}
	return result
}

func smartSplit(source string) []string {
	var stringsOut []string
	pos := 0
	for pos < len(source) {
		if source[pos] == '"' {
			word := ""
			pos++
			for pos < len(source) {
				if source[pos] == '"' {
					break
				}
				if source[pos] == '\\' {
					word += `\`
					pos++
					if pos >= len(source) {
						break
					}
				}
				word += source[pos : pos+1]
				pos++
			}
			stringsOut = append(stringsOut, `"`+word+`"`)
		}
		pos++
	}
	return stringsOut
}

func fixQuotes(source string) string {
	if len(source) >= 2 && source[0] == '"' && source[len(source)-1] == '"' {
		source = source[1 : len(source)-1]
		source = "'" + strings.ReplaceAll(source, "'", `\'`) + "'"
	}
	return source
}

func unescapeHex(source string) string {
	for i := 32; i < 128; i++ {
		hex := strconv.FormatInt(int64(i), 16)
		re := regexp.MustCompile(`(?i)\\x` + hex)
		source = re.ReplaceAllString(source, string(rune(i)))
	}
	source = strings.ReplaceAll(source, `\x09`, "\t")
	return source
}
