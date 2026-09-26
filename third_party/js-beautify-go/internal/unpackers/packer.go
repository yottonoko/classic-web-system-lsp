// Package unpackers contains safe Go decoders for supported packed JavaScript forms.
package unpackers

import (
	"regexp"
	"strconv"
	"strings"
)

// Packer decodes Dean Edwards P_A_C_K_E_R payloads without executing them.
type Packer struct{}

var packerChunkRE = regexp.MustCompile(`(?s)eval\(\(?function\(.*?(,0,\{\}\)\)|split\('\|'\)\)\))($|\n)`)
var packerPayloadRE = regexp.MustCompile(`(?s)eval\(\(?function\(p,a,c,k,e,r\)\{.*?\}\('((?:\\'|[^'])*)',(\d+),(\d+),'((?:\\'|[^'])*)'\.split\('\|'\),0,\{\}\)\)?\)`)

// Detect reports whether source contains a supported P_A_C_K_E_R payload.
func (Packer) Detect(source string) bool {
	return len(Packer{}.Chunks(source)) > 0
}

// Chunks returns packed eval chunks found in source.
func (Packer) Chunks(source string) []string {
	return packerChunkRE.FindAllString(source, -1)
}

// Unpack replaces supported packed chunks with decoded source.
func (p Packer) Unpack(source string) string {
	for _, chunk := range p.Chunks(source) {
		trimmed := strings.TrimSuffix(chunk, "\n")
		source = strings.ReplaceAll(source, trimmed, p.UnpackChunk(trimmed))
	}
	return source
}

// UnpackChunk decodes one supported P_A_C_K_E_R chunk.
func (Packer) UnpackChunk(source string) string {
	matches := packerPayloadRE.FindStringSubmatch(source)
	if matches == nil {
		return source
	}
	payload := strings.ReplaceAll(matches[1], `\'`, `'`)
	count, err := strconv.Atoi(matches[3])
	if err != nil {
		return source
	}
	keywords := strings.Split(matches[4], "|")
	for i := count - 1; i >= 0; i-- {
		if i >= len(keywords) || keywords[i] == "" {
			continue
		}
		word := encodeBase62(i)
		re := regexp.MustCompile(`\b` + regexp.QuoteMeta(word) + `\b`)
		payload = re.ReplaceAllString(payload, keywords[i])
	}
	return payload
}

func encodeBase62(value int) string {
	const chars = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	if value < 62 {
		return chars[value : value+1]
	}
	high := value / 62
	low := value % 62
	return encodeBase62(high) + chars[low:low+1]
}
