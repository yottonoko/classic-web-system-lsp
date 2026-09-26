package workspace

import (
	"bytes"
	"io"
	"unicode/utf8"

	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/transform"
)

func DecodeLegacy(content []byte, encoding string) (string, error) {
	switch encoding {
	case "", "auto":
		if utf8.Valid(content) {
			return string(content), nil
		}
		return decodeShiftJIS(content)
	case "utf8":
		return string(content), nil
	case "shift_jis", "cp932":
		return decodeShiftJIS(content)
	default:
		return string(content), nil
	}
}

func decodeShiftJIS(content []byte) (string, error) {
	reader := transform.NewReader(bytes.NewReader(content), japanese.ShiftJIS.NewDecoder())
	decoded, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}
