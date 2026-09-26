package javascript

import (
	"os"
	"strings"
	"testing"

	"github.com/yottonoko/js-beautify-go/internal/legacycases"
)

func TestLegacyJavaScriptCases(t *testing.T) {
	if os.Getenv("JS_BEAUTIFY_GO_LEGACY") == "" {
		t.Skip("set JS_BEAUTIFY_GO_LEGACY=1 to run the imported js-beautify javascript parity corpus")
	}
	cases, err := legacycases.Load("javascript")
	if err != nil {
		t.Fatal(err)
	}
	failures := 0
	for _, tc := range cases {
		got, err := Beautify(tc.Input, tc.Options)
		if err != nil {
			t.Fatalf("%s: %v", tc.Name, err)
		}
		if got != tc.Expected {
			failures++
			if failures <= 10 {
				t.Errorf("%s:\ninput:\n%s\nwant:\n%s\ngot:\n%s", tc.Name, visible(tc.Input), visible(tc.Expected), visible(got))
			}
		}
	}
	if failures > 0 {
		t.Fatalf("%d/%d legacy javascript cases failed", failures, len(cases))
	}
}

func visible(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "\t", `\t`), "\n", `\n`+"\n")
}
