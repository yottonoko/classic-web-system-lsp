package legacycases

import (
	"strings"
	"testing"
)

func TestDecodeLegacyStringExpression(t *testing.T) {
	got := decodeLegacyStringExpression(`var ' + unicode_char(3232) + '_' + unicode_char(3232) + ' = "hi";`)
	want := `var ಠ_ಠ = "hi";`
	if got != want {
		t.Fatalf("decoded expression mismatch:\nwant %q\ngot  %q", want, got)
	}
}

func TestDecodeLegacyStringEscapes(t *testing.T) {
	got := decodeLegacyStringExpression(`a\\b\nc\'d`)
	want := "a\\b\nc'd"
	if got != want {
		t.Fatalf("decoded escape mismatch:\nwant %q\ngot  %q", want, got)
	}
}

func TestDecodeLegacyStringVariables(t *testing.T) {
	got := decodeLegacyStringExpression(`' + wrap_input_1 + '`)
	if !strings.Contains(got, `foo.bar().baz().cucumber`) {
		t.Fatalf("legacy variable was not expanded:\n%s", got)
	}
	if strings.Contains(got, "wrap_input_1") {
		t.Fatalf("legacy variable name leaked into decoded input:\n%s", got)
	}
}
