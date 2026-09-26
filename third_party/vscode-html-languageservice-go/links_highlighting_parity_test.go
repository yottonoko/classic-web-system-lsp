package htmlservice

import (
	"net/url"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

type urlDocumentContext struct {
	workspaceFolder string
}

func (c urlDocumentContext) ResolveReference(ref, base string) (string, bool) {
	if strings.HasPrefix(ref, "/") && c.workspaceFolder != "" {
		return unescapeNonASCIIURIEscapes(joinURLPath(c.workspaceFolder, ref)), true
	}
	resolved, err := url.Parse(base)
	if err != nil {
		return "", false
	}
	refURL, err := url.Parse(ref)
	if err != nil {
		refURL = &url.URL{Path: ref, RawPath: escapeTestURIPathBytes(ref)}
	}
	return unescapeNonASCIIURIEscapes(resolved.ResolveReference(refURL).String()), true
}

func unescapeNonASCIIURIEscapes(value string) string {
	var out strings.Builder
	changed := false
	for i := 0; i < len(value); {
		if value[i] != '%' || i+2 >= len(value) {
			out.WriteByte(value[i])
			i++
			continue
		}
		start := i
		var bytes []byte
		for i+2 < len(value) && value[i] == '%' {
			hi, okHi := hexValue(value[i+1])
			lo, okLo := hexValue(value[i+2])
			if !okHi || !okLo {
				break
			}
			b := hi<<4 | lo
			if b < 0x80 {
				break
			}
			bytes = append(bytes, b)
			i += 3
		}
		if len(bytes) > 0 && utf8.Valid(bytes) {
			out.WriteString(string(bytes))
			changed = true
			continue
		}
		out.WriteByte(value[start])
		i = start + 1
	}
	if !changed {
		return value
	}
	return out.String()
}

func hexValue(ch byte) (byte, bool) {
	switch {
	case '0' <= ch && ch <= '9':
		return ch - '0', true
	case 'a' <= ch && ch <= 'f':
		return ch - 'a' + 10, true
	case 'A' <= ch && ch <= 'F':
		return ch - 'A' + 10, true
	default:
		return 0, false
	}
}

func escapeTestURIPathBytes(value string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(value); i++ {
		ch := value[i]
		if ch < 0x20 || ch >= 0x80 {
			b.WriteByte('%')
			b.WriteByte(hex[ch>>4])
			b.WriteByte(hex[ch&0x0F])
			continue
		}
		b.WriteByte(ch)
	}
	return b.String()
}

type failingBaseDocumentContext struct{}

func (failingBaseDocumentContext) ResolveReference(ref, base string) (string, bool) {
	if ref == "bad" {
		return "", false
	}
	return urlDocumentContext{}.ResolveReference(ref, base)
}

type undefinedDocumentContext struct{}

func (undefinedDocumentContext) ResolveReference(ref, base string) (string, bool) {
	return "", false
}

type emptyDocumentContext struct{}

func (emptyDocumentContext) ResolveReference(ref, base string) (string, bool) {
	return "", true
}

func joinURLPath(base, ref string) string {
	parsed, err := url.Parse(base)
	if err != nil {
		return base + ref
	}
	parsed.Path = pathClean(ref)
	return parsed.String()
}

func pathClean(p string) string {
	for strings.Contains(p, "//") {
		p = strings.ReplaceAll(p, "//", "/")
	}
	return p
}

func linkTargets(modelURL, tokenContent string) []DocumentLink {
	lang := "html"
	if strings.HasSuffix(modelURL, ".hbs") {
		lang = "handlebars"
	}
	doc := NewTextDocument(modelURL, lang, 0, `<a href="`+tokenContent+`">`)
	return GetLanguageService().FindDocumentLinks(doc, urlDocumentContext{})
}

func TestLinkCreationBaselineParity(t *testing.T) {
	cases := []struct {
		model string
		token string
		want  string
	}{
		{"http://model/1.html", "javascript:void;", ""},
		{"http://model/1.html", " \tjavascript:alert(7);", ""},
		{"http://model/1.html", " #relative", "http://model/1.html"},
		{"http://model/1.html", `file:///C:\Alex\src\path\to\file.txt`, `file:///C:\Alex\src\path\to\file.txt`},
		{"http://model/1.html", "http://www.microsoft.com/", "http://www.microsoft.com/"},
		{"http://model/1.html", "http://example.com/%zz", "http://example.com/%zz"},
		{"http://model/1.html", "http://exa mple.com/", "http://exa mple.com/"},
		{"http://model/1.html", "http://[::1", "http://[::1"},
		{"http://model/1.html", "https://www.microsoft.com/", "https://www.microsoft.com/"},
		{"http://model/1.html", "//www.microsoft.com/", "http://www.microsoft.com/"},
		{"file:///C:/doc.html", "file:///C:/foo.html?x=1#frag", "file:///c:/foo.html"},
		{"http://model/x/1.html", "\f//www.microsoft.com/", "http://www.microsoft.com/"},
		{"http://model/x/1.html", "\v//www.microsoft.com/", "http://www.microsoft.com/"},
		{"http://model/x/1.html", "\u00a0//www.microsoft.com/", "http://www.microsoft.com/"},
		{"http://model/x/1.html", "\u0085//www.microsoft.com/", "http://model/x/\u0085//www.microsoft.com/"},
		{"http://model/x/1.html", "\u0085javascript:alert(1)", "http://model/x/\u0085javascript:alert(1)"},
		{"http://model/x/1.html", "a.js", "http://model/x/a.js"},
		{"http://model/x/1.html", "./a2.js", "http://model/x/a2.js"},
		{"http://model/x/1.html", "/b.js", "http://model/b.js"},
		{"http://model/x/y/1.html", "../../c.js", "http://model/c.js"},
		{"file:///C:/Alex/src/path/to/file.html", "javascript:void;", ""},
		{"file:///C:/Alex/src/path/to/file.html", " \tjavascript:alert(7);", ""},
		{"file:///C:/Alex/src/path/to/file.html", " #relative", "file:///C:/Alex/src/path/to/file.html"},
		{"file:///C:/Alex/src/path/to/file.html", `file:///C:\Alex\src\path\to\file.txt`, `file:///C:\Alex\src\path\to\file.txt`},
		{"file:///C:/Alex/src/path/to/file.html", "http://www.microsoft.com/", "http://www.microsoft.com/"},
		{"file:///C:/Alex/src/path/to/file.html", "https://www.microsoft.com/", "https://www.microsoft.com/"},
		{"file:///C:/Alex/src/path/to/file.html", "  //www.microsoft.com/", "http://www.microsoft.com/"},
		{"file:///C:/Alex/src/path/to/file.html", "a.js", "file:///C:/Alex/src/path/to/a.js"},
		{"file:///root/index.html", "file:///root/a b.html?x=1#frag", "file:///root/a b.html"},
		{"file:///C:/Alex/src/path/to/file.html", "/a.js", "file:///a.js"},
		{"https://www.test.com/path/to/file.html", `file:///C:\Alex\src\path\to\file.txt`, `file:///C:\Alex\src\path\to\file.txt`},
		{"https://www.test.com/path/to/file.html", "//www.microsoft.com/", "https://www.microsoft.com/"},
		{"https://www.test.com/path/to/file.html", "%", ""},
		{"file:///c:/Alex/working_dir/18314-link-detection/test.html", "/class/class.js", "file:///class/class.js"},
		{"http://foo/bar.hbs", "/class/class.js", "http://foo/class/class.js"},
		{"http://foo/bar.hbs", "{{asset foo}}/class/class.js", ""},
		{"http://foo/bar.hbs", "{{href-to", ""},
	}
	for _, tc := range cases {
		links := linkTargets(tc.model, tc.token)
		got := ""
		if len(links) > 0 {
			got = links[0].Target
		}
		if got != tc.want {
			t.Fatalf("link %s %q got %q want %q", tc.model, tc.token, got, tc.want)
		}
	}
}

func TestLinkBaseResolveFailureKeepsBaseUndefined(t *testing.T) {
	doc := NewTextDocument("file:///root/index.html", "html", 0, `<base href="bad"><base href="docs/"><img src="foo.png">`)
	links := GetLanguageService().FindDocumentLinks(doc, failingBaseDocumentContext{})
	want := []DocumentLink{{Range: NewRange(NewPosition(0, 46), NewPosition(0, 53)), Target: "file:///root/docs/foo.png"}}
	if !reflect.DeepEqual(links, want) {
		t.Fatalf("base resolve failure links got %#v want %#v", links, want)
	}
}

func TestLinkDetectionBaselineParity(t *testing.T) {
	cases := []struct {
		value string
		want  []DocumentLink
	}{
		{`<img src="foo.png">`, []DocumentLink{{Range: NewRange(NewPosition(0, 10), NewPosition(0, 17)), Target: "file:///test/data/abc/foo.png"}}},
		{`<img src="é.png">`, []DocumentLink{{Range: NewRange(NewPosition(0, 10), NewPosition(0, 15)), Target: "file:///test/data/abc/é.png"}}},
		{`😀<img src="foo.png">`, []DocumentLink{{Range: NewRange(NewPosition(0, 12), NewPosition(0, 19)), Target: "file:///test/data/abc/foo.png"}}},
		{`<a href="http://server/foo.html">`, []DocumentLink{{Range: NewRange(NewPosition(0, 9), NewPosition(0, 31)), Target: "http://server/foo.html"}}},
		{`<img src="">`, []DocumentLink{}},
		{`<LINK HREF="a.html">`, []DocumentLink{{Range: NewRange(NewPosition(0, 12), NewPosition(0, 18)), Target: "file:///test/data/abc/a.html"}}},
		{"<LINK HREF=\"a.html\n>\n", []DocumentLink{}},
		{`<a href=http://www.example.com></a>`, []DocumentLink{{Range: NewRange(NewPosition(0, 8), NewPosition(0, 30)), Target: "http://www.example.com"}}},
		{`<html><base href="docs/"><img src="foo.png"></html>`, []DocumentLink{{Range: NewRange(NewPosition(0, 35), NewPosition(0, 42)), Target: "file:///test/data/abc/docs/foo.png"}}},
		{`<html><base href="http://www.example.com/page.html"><img src="foo.png"></html>`, []DocumentLink{{Range: NewRange(NewPosition(0, 62), NewPosition(0, 69)), Target: "http://www.example.com/foo.png"}}},
		{`<html><base href=".."><img src="foo.png"></html>`, []DocumentLink{{Range: NewRange(NewPosition(0, 32), NewPosition(0, 39)), Target: "file:///test/data/foo.png"}}},
		{`<html><base href="."><img src="foo.png"></html>`, []DocumentLink{{Range: NewRange(NewPosition(0, 31), NewPosition(0, 38)), Target: "file:///test/data/abc/foo.png"}}},
		{`<html><base href="/docs/"><img src="foo.png"></html>`, []DocumentLink{{Range: NewRange(NewPosition(0, 36), NewPosition(0, 43)), Target: "file:///docs/foo.png"}}},
		{`<html><base href=""><base href="docs/"><img src="foo.png"></html>`, []DocumentLink{{Range: NewRange(NewPosition(0, 49), NewPosition(0, 56)), Target: "file:///test/data/abc/foo.png"}}},
		{`<a href="mailto:<%- mail %>@<%- domain %>" > <% - mail %>@<% - domain %> </a>`, []DocumentLink{}},
		{`<link rel="icon" type="image/x-icon" href="data:@file/x-icon;base64,AAABAAIAQEAAAAEAIAAoQgAAJgA">`, []DocumentLink{}},
		{`<blockquote cite="foo.png">`, []DocumentLink{{Range: NewRange(NewPosition(0, 18), NewPosition(0, 25)), Target: "file:///test/data/abc/foo.png"}}},
		{`<style src="styles.css?t=345">`, []DocumentLink{{Range: NewRange(NewPosition(0, 12), NewPosition(0, 28)), Target: "file:///test/data/abc/styles.css"}}},
		{`<a href="https://werkenvoor.be/nl/jobs?f%5B0%5D=activitydomains%3A115&f%5B1%5D=lang%3Anl">link</a>`, []DocumentLink{{Range: NewRange(NewPosition(0, 9), NewPosition(0, 88)), Target: "https://werkenvoor.be/nl/jobs?f%5B0%5D=activitydomains%3A115&f%5B1%5D=lang%3Anl"}}},
		{`<a href="file://///x">`, []DocumentLink{{Range: NewRange(NewPosition(0, 9), NewPosition(0, 20))}}},
		{`<a href="file://[::1">`, []DocumentLink{{Range: NewRange(NewPosition(0, 9), NewPosition(0, 20)), Target: "file://[::1"}}},
		{`<a href="jobs.html?f=bar">link</a>`, []DocumentLink{{Range: NewRange(NewPosition(0, 9), NewPosition(0, 24)), Target: "file:///test/data/abc/jobs.html"}}},
		{`<a href="é.html?x=1#frag">`, []DocumentLink{{Range: NewRange(NewPosition(0, 9), NewPosition(0, 24)), Target: "file:///test/data/abc/é.html"}}},
		{`<body><h1 id="title"></h1><a href="#title"</a></body>`, []DocumentLink{{Range: NewRange(NewPosition(0, 35), NewPosition(0, 41)), Target: "file:///test/data/abc/test.html#1,14"}}},
		{`😀<body><h1 id="title"></h1><a href="#title"</a></body>`, []DocumentLink{{Range: NewRange(NewPosition(0, 37), NewPosition(0, 43)), Target: "file:///test/data/abc/test.html#1,16"}}},
		{`<body><h1 id="title"></h1><a href="file:///test/data/abc/test.html#title"</a></body>`, []DocumentLink{{Range: NewRange(NewPosition(0, 35), NewPosition(0, 72)), Target: "file:///test/data/abc/test.html#1,14"}}},
		{`<a href="#toString"></a>`, []DocumentLink{{Range: NewRange(NewPosition(0, 9), NewPosition(0, 18)), Target: "file:///test/data/abc/test.html#1,NaN"}}},
		{`<body><h1 id="__proto__"></h1><a href="#__proto__"></a></body>`, []DocumentLink{{Range: NewRange(NewPosition(0, 39), NewPosition(0, 49)), Target: "file:///test/data/abc/test.html#1,NaN"}}},
		{`<body><h1 id="title"></h1><a href="#body"</a></body>`, []DocumentLink{{Range: NewRange(NewPosition(0, 35), NewPosition(0, 40)), Target: "file:///test/data/abc/test.html"}}},
	}
	for _, tc := range cases {
		doc := NewTextDocument("file:///test/data/abc/test.html", "html", 0, tc.value)
		got := GetLanguageService().FindDocumentLinks(doc, urlDocumentContext{})
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("links for %q\n got: %#v\nwant: %#v", tc.value, got, tc.want)
		}
	}
}

func TestLinkDocumentContextFalsyResolutionMatchesForkSource(t *testing.T) {
	doc := NewTextDocument("file:///root/index.html", "html", 0, `<img src="foo.png">`)
	if links := GetLanguageService().FindDocumentLinks(doc, undefinedDocumentContext{}); len(links) != 0 {
		t.Fatalf("undefined document context links got %#v want none", links)
	}
	if links := GetLanguageService().FindDocumentLinks(doc, emptyDocumentContext{}); len(links) != 0 {
		t.Fatalf("empty document context links got %#v want none", links)
	}
}

func TestLinkNilDocumentContextRelativeQueryFragmentMatchesForkSource(t *testing.T) {
	doc := NewTextDocument("file:///test/data/abc/test.html", "html", 0, `<a href="a?b"><a href="a#b"><a href="?b"><a href="#b"><a href="foo#"><a href="foo?"><a href="./foo#"><a href="./foo?"><a href="foo/#"><a href="foo/?">`)
	links := GetLanguageService().FindDocumentLinks(doc, nil)
	var got []string
	for _, link := range links {
		got = append(got, link.Target)
	}
	want := []string{"file:///a", "file:///a", "file:///", "file:///test/data/abc/test.html", "foo#", "foo?", "./foo#", "./foo?", "foo/#", "foo/?"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("nil context relative query/fragment targets got %#v want %#v", got, want)
	}
}

func assertHighlightsForTest(t *testing.T, value string, expected []int, name string) {
	t.Helper()
	offset := stringsIndex(value, "|")
	value = stringsReplace(value, "|", "")
	doc := NewTextDocument("test://test/test.html", "html", 0, value)
	ls := GetLanguageService()
	htmlDoc := ls.ParseHTMLDocument(doc)
	highlights := ls.FindDocumentHighlights(doc, doc.PositionAt(offset), htmlDoc)
	if len(highlights) != len(expected) {
		t.Fatalf("highlight len for %q got %#v want %#v", value, highlights, expected)
	}
	for i, h := range highlights {
		start := doc.OffsetAt(h.Range.Start)
		end := doc.OffsetAt(h.Range.End)
		if start != expected[i] || strings.ToLower(doc.GetText()[start:end]) != name {
			t.Fatalf("highlight %q got start %d text %q want %d %q", value, start, doc.GetText()[start:end], expected[i], name)
		}
	}
}

func TestHighlightingBaselineParity(t *testing.T) {
	doc := NewTextDocument("test://test/test.html", "html", 0, "<html></html>")
	ls := GetLanguageService()
	htmlDoc := ls.ParseHTMLDocument(doc)
	if highlights := ls.FindDocumentHighlights(doc, doc.PositionAt(6), htmlDoc); highlights == nil || len(highlights) != 0 {
		t.Fatalf("expected empty highlight slice, got %#v", highlights)
	}
	assertHighlightsForTest(t, "|<html></html>", nil, "")
	assertHighlightsForTest(t, "<|html></html>", []int{1, 8}, "html")
	assertHighlightsForTest(t, "<h|tml></html>", []int{1, 8}, "html")
	assertHighlightsForTest(t, "<htm|l></html>", []int{1, 8}, "html")
	assertHighlightsForTest(t, "<html|></html>", []int{1, 8}, "html")
	assertHighlightsForTest(t, "<html>|</html>", nil, "")
	assertHighlightsForTest(t, "<html><|/html>", nil, "")
	assertHighlightsForTest(t, "<html></|html>", []int{1, 8}, "html")
	assertHighlightsForTest(t, "<html></h|tml>", []int{1, 8}, "html")
	assertHighlightsForTest(t, "<html></ht|ml>", []int{1, 8}, "html")
	assertHighlightsForTest(t, "<html></htm|l>", []int{1, 8}, "html")
	assertHighlightsForTest(t, "<html></html|>", []int{1, 8}, "html")
	assertHighlightsForTest(t, "<html></html>|", nil, "")
	assertHighlightsForTest(t, "<html>|<div></div></html>", nil, "")
	assertHighlightsForTest(t, "<html><|div></div></html>", []int{7, 13}, "div")
	assertHighlightsForTest(t, "<html><div>|</div></html>", nil, "")
	assertHighlightsForTest(t, "<html><div></di|v></html>", []int{7, 13}, "div")
	assertHighlightsForTest(t, "<html><div><div></div></di|v></html>", []int{7, 24}, "div")
	assertHighlightsForTest(t, "<html><div><div></div|></div></html>", []int{12, 18}, "div")
	assertHighlightsForTest(t, "<html><div><div|></div></div></html>", []int{12, 18}, "div")
	assertHighlightsForTest(t, "<html><div><div></div></div></h|tml>", []int{1, 30}, "html")
	assertHighlightsForTest(t, "<html><di|v></div><div></div></html>", []int{7, 13}, "div")
	assertHighlightsForTest(t, "<html><div></div><div></d|iv></html>", []int{18, 24}, "div")
	assertHighlightsForTest(t, "<html><|div/></html>", []int{7}, "div")
	assertHighlightsForTest(t, "<html><|br></html>", []int{7}, "br")
	assertHighlightsForTest(t, "<html><div><d|iv/></div></html>", []int{12}, "div")
	assertHighlightsForTest(t, "<HTML><diV><Div></dIV></dI|v></html>", []int{7, 24}, "div")
	assertHighlightsForTest(t, "<HTML><diV|><Div></dIV></dIv></html>", []int{7, 24}, "div")
	assertHighlightsForTest(t, "<div><ol><li></li></ol></p></|div>", []int{1, 29}, "div")
}
