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
	// The corpus comes from js-beautify 2.0, which formats type="importmap"
	// as JavaScript. This port follows 1.15.4, the version vendored by
	// vscode-html-languageservice, which leaves importmap content as is.
	skipped := map[string]bool{"Tests for script and style types (issue 453, 821)/022": true}
	failures := 0
	for _, tc := range cases {
		if skipped[tc.Name] {
			continue
		}
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
