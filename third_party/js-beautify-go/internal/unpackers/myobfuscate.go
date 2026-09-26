// Package unpackers contains safe Go decoders for supported packed JavaScript forms.
package unpackers

import (
	"encoding/base64"
	"net/url"
	"regexp"
	"strings"
)

// MyObfuscate decodes supported myobfuscate.com payloads without eval.
type MyObfuscate struct{}

const myObfuscateWarning = `//
// Unpacker warning: be careful when using myobfuscate.com for your projects:
// scripts obfuscated by the free online version call back home.
//

`

const myObfuscateSignature = `["\x41\x42\x43\x44\x45\x46\x47\x48\x49\x4A\x4B\x4C\x4D\x4E\x4F\x50\x51\x52\x53\x54\x55\x56\x57\x58\x59\x5A\x61\x62\x63\x64\x65\x66\x67\x68\x69\x6A\x6B\x6C\x6D\x6E\x6F\x70\x71\x72\x73\x74\x75\x76\x77\x78\x79\x7A\x30\x31\x32\x33\x34\x35\x36\x37\x38\x39\x2B\x2F\x3D","","\x63\x68\x61\x72\x41\x74","\x69\x6E\x64\x65\x78\x4F\x66","\x66\x72\x6F\x6D\x43\x68\x61\x72\x43\x6F\x64\x65","\x6C\x65\x6E\x67\x74\x68"]`

var myObfuscateVarRE = regexp.MustCompile(`^var _?[0O1lI]{3}=('|\[).*\)\)\);`)
var myObfuscateFnRE = regexp.MustCompile(`^function _?[0O1lI]{3}\(_`)
var myObfuscateEvalRE = regexp.MustCompile(`eval\(`)
var myObfuscateEscapeRE = regexp.MustCompile(`var _escape\s*=\s*'([^']*)'`)
var myObfuscateEvalVarRE = regexp.MustCompile(`eval\(\w+\(\w+\((\w+)\)\)\);`)

// Detect reports whether source matches a supported MyObfuscate signature.
func (MyObfuscate) Detect(source string) bool {
	if strings.Contains(source, myObfuscateSignature) {
		return true
	}
	return myObfuscateVarRE.MatchString(source) ||
		(myObfuscateFnRE.MatchString(source) && myObfuscateEvalRE.MatchString(source))
}

// Unpack decodes a supported MyObfuscate payload and prepends a warning.
func (m MyObfuscate) Unpack(source string) string {
	if !m.Detect(source) {
		return source
	}
	if filtered, ok := filterMyObfuscate(source); ok {
		source = filtered
	}
	match := myObfuscateEscapeRE.FindStringSubmatch(source)
	if match == nil {
		return source
	}
	unescaped, err := url.QueryUnescape(match[1])
	if err != nil {
		return source
	}
	unescaped = strings.TrimPrefix(unescaped, "<script>")
	unescaped = strings.TrimSuffix(unescaped, "</script>")
	return myObfuscateWarning + unescaped
}

func filterMyObfuscate(source string) (string, bool) {
	match := myObfuscateEvalVarRE.FindStringSubmatch(source)
	if match == nil {
		return source, false
	}
	varName := regexp.QuoteMeta(match[1])
	payloadRE := regexp.MustCompile(`(?s)var\s+` + varName + `\s*=\s*'(.*?)';`)
	payloadMatch := payloadRE.FindStringSubmatch(source)
	if payloadMatch == nil {
		return source, false
	}
	reversed := reverseASCII(payloadMatch[1])
	decoded, err := base64.StdEncoding.DecodeString(reversed)
	if err != nil {
		return source, false
	}
	unquoted, err := url.QueryUnescape(string(decoded))
	if err != nil {
		return source, false
	}
	return unquoted, true
}

func reverseASCII(value string) string {
	bytes := []byte(value)
	for left, right := 0, len(bytes)-1; left < right; left, right = left+1, right-1 {
		bytes[left], bytes[right] = bytes[right], bytes[left]
	}
	return string(bytes)
}
