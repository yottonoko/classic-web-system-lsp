package html

import (
	"os"
	"strings"
	"testing"

	"github.com/yottonoko/js-beautify-go/internal/css"
	"github.com/yottonoko/js-beautify-go/internal/javascript"
	"github.com/yottonoko/js-beautify-go/internal/legacycases"
)

func TestLegacyHTMLCases(t *testing.T) {
	if os.Getenv("JS_BEAUTIFY_GO_LEGACY") == "" {
		t.Skip("set JS_BEAUTIFY_GO_LEGACY=1 to run the imported js-beautify html parity corpus")
	}
	cases, err := legacycases.Load("html")
	if err != nil {
		t.Fatal(err)
	}
	failures := 0
	for _, tc := range cases {
		got, err := Beautify(tc.Input, tc.Options, javascript.Beautify, css.Beautify)
		if err != nil {
			t.Fatalf("%s: %v", tc.Name, err)
		}
		if got != tc.Expected {
			failures++
			if failures <= 10 {
				t.Errorf("%s:\ninput:\n%s\nwant:\n%s\ngot:\n%s", tc.Name, htmlVisible(tc.Input), htmlVisible(tc.Expected), htmlVisible(got))
			}
		}
	}
	if failures > 0 {
		t.Fatalf("%d/%d legacy html cases failed", failures, len(cases))
	}
}

func htmlVisible(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "\t", `\t`), "\n", `\n`+"\n")
}
