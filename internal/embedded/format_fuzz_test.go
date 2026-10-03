package embedded

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
)

func FuzzFormatDocumentPreservesServerCode(f *testing.F) {
	samples, _ := filepath.Glob("../../samples/classic-asp-dashboard/*.asp")
	includes, _ := filepath.Glob("../../samples/classic-asp-dashboard/includes/*.inc")
	for _, path := range append(samples, includes...) {
		if data, err := os.ReadFile(path); err == nil && len(data) <= 6000 {
			f.Add(string(data))
		}
	}
	for _, seed := range []string{
		"<script>var a = '<%= x %>';</script><style>.a { color: <%= c %>; }</style>",
		"<script runat=\"server\" language=\"VBScript\">\nSub S()\nEnd Sub\n</script>",
		"< %0",
		"< %<%",
		"<p>a < %b</p>",
		"<sCript>\"<%",
		"<stYle><%%>0<\"%0",
		"<stYle><%\"00",
		"\"<A\"<%\"",
		"<sCript>'<%%></sCript>'0",
		"< A000\"0<%000000\"00000000000000000000%>",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		if len(text) > 3000 || !utf8.ValidString(text) {
			return
		}
		before := formatFuzzServerCode(text)
		for _, html := range []bool{true, false} {
			options := core.FormattingOptions{TabSize: 2, InsertSpaces: true, FormatCSS: FormatCSS, FormatJavaScript: FormatJavaScript}
			if html {
				options.FormatHTML = FormatHTML
			}
			parsed := core.ParseDocument("file:///fuzz/page.asp", text, core.Settings{DefaultLanguage: "VBScript"})
			for _, edit := range core.FormatDocument(parsed, options) {
				if after := formatFuzzServerCode(edit.NewText); after != before {
					t.Fatalf("formatting (HTML formatter %v) changed server code\ninput:  %q\noutput: %q\nbefore: %q\nafter:  %q", html, text, edit.NewText, before, after)
				}
			}
		}
	})
}

// formatFuzzServerCode lists server regions with their code apart from
// whitespace and letter case, which the VBScript formatter may change.
func formatFuzzServerCode(text string) string {
	parsed := core.ParseDocument("file:///fuzz/page.asp", text, core.Settings{DefaultLanguage: "VBScript"})
	var out strings.Builder
	for _, region := range parsed.Regions {
		switch region.Kind {
		case core.RegionASPBlock, core.RegionASPExpression, core.RegionASPDirective, core.RegionServerScript:
			out.WriteString(string(region.Kind))
			for _, r := range text[region.ContentStart:region.ContentEnd] {
				if !unicode.IsSpace(r) {
					out.WriteRune(unicode.ToLower(r))
				}
			}
			out.WriteByte('|')
		}
	}
	return out.String()
}
