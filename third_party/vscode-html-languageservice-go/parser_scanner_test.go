package htmlservice

import (
	"reflect"
	"testing"
)

func collectTokens(input string) []TokenType {
	scanner := CreateScanner(input)
	var tokens []TokenType
	for {
		token := scanner.Scan()
		tokens = append(tokens, token)
		if token == TokenTypeEOS {
			return tokens
		}
	}
}

func TestScannerBasicTagsAndEmbeddedContent(t *testing.T) {
	got := collectTokens(`<div class="foo"><script>if (a < b) {}</script><style>h1 { color: red; }</style></div>`)
	want := []TokenType{
		TokenTypeStartTagOpen, TokenTypeStartTag, TokenTypeWhitespace, TokenTypeAttributeName, TokenTypeDelimiterAssign, TokenTypeAttributeValue, TokenTypeStartTagClose,
		TokenTypeStartTagOpen, TokenTypeStartTag, TokenTypeStartTagClose,
		TokenTypeScript,
		TokenTypeEndTagOpen, TokenTypeEndTag, TokenTypeEndTagClose,
		TokenTypeStartTagOpen, TokenTypeStartTag, TokenTypeStartTagClose,
		TokenTypeStyles,
		TokenTypeEndTagOpen, TokenTypeEndTag, TokenTypeEndTagClose,
		TokenTypeEndTagOpen, TokenTypeEndTag, TokenTypeEndTagClose,
		TokenTypeEOS,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tokens mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestScannerCommentsDoctypeAttributesAndErrors(t *testing.T) {
	scanner := CreateScanner(`<!DOCTYPE html><!-- hi --><input disabled type=color/>`)
	var texts []string
	var errors []string
	for {
		token := scanner.Scan()
		texts = append(texts, scanner.GetTokenText())
		if scanner.GetTokenError() != "" {
			errors = append(errors, scanner.GetTokenError())
		}
		if token == TokenTypeEOS {
			break
		}
	}
	if len(errors) != 0 {
		t.Fatalf("unexpected scanner errors: %#v", errors)
	}
	for _, expected := range []string{"<!DOCTYPE", " html", ">", "<!--", " hi ", "-->", "<", "input", "disabled", "type", "color", "/>"} {
		if !containsString(texts, expected) {
			t.Fatalf("missing token text %q in %#v", expected, texts)
		}
	}
}

func TestParserMatchesOriginalTreeCases(t *testing.T) {
	ls := GetLanguageService()
	doc := NewTextDocument("test://test.html", "html", 0, `<html><head></head><body><input type="button"><span><br><br></span></body></html>`)
	htmlDoc := ls.ParseHTMLDocument(doc)
	if len(htmlDoc.Roots) != 1 || htmlDoc.Roots[0].Tag != "html" {
		t.Fatalf("expected html root: %#v", htmlDoc.Roots)
	}
	body := htmlDoc.Roots[0].Children[1]
	if body.Tag != "body" || !body.Closed {
		t.Fatalf("expected closed body: %#v", body)
	}
	input := body.Children[0]
	if input.Tag != "input" || !input.Closed || input.Attributes["type"] == nil || *input.Attributes["type"] != `"button"` {
		t.Fatalf("expected void input with type attribute: %#v", input)
	}
	span := body.Children[1]
	if span.Tag != "span" || len(span.Children) != 2 || span.Children[0].Tag != "br" {
		t.Fatalf("expected span with two br children: %#v", span)
	}
}

func TestNodeAttributeNames(t *testing.T) {
	emptyDoc := parseForParity("")
	if emptyDoc.Roots == nil || len(emptyDoc.Roots) != 0 {
		t.Fatalf("empty document roots got %#v want empty slice", emptyDoc.Roots)
	}
	doc := parseForParity(`<div id="x" class="y" id="z"></div>`)
	if len(doc.Roots) != 1 {
		t.Fatalf("unexpected roots: %#v", doc.Roots)
	}
	if doc.Roots[0].Children == nil || len(doc.Roots[0].Children) != 0 {
		t.Fatalf("empty child slice got %#v want empty slice", doc.Roots[0].Children)
	}
	got := doc.Roots[0].AttributeNames()
	want := []string{"id", "class"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("attribute names got %#v want %#v", got, want)
	}
	if id := doc.Roots[0].Attributes["id"]; id == nil || *id != `"z"` {
		t.Fatalf("duplicate attribute value got %#v", id)
	}

	doc = parseForParity(`<div 2=a 1=b x=c></div>`)
	got = doc.Roots[0].AttributeNames()
	want = []string{"1", "2", "x"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("integer-like attribute names got %#v want %#v", got, want)
	}

	doc = parseForParity(`<div></div>`)
	got = doc.Roots[0].AttributeNames()
	if got == nil || len(got) != 0 {
		t.Fatalf("empty attribute names got %#v want empty slice", got)
	}

	doc = parseForParity(`<div __proto__ a=b></div>`)
	got = doc.Roots[0].AttributeNames()
	want = []string{"a"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("__proto__ attribute names got %#v want %#v", got, want)
	}
	if _, ok := doc.Roots[0].Attributes["__proto__"]; ok {
		t.Fatalf("__proto__ should not be an own attribute: %#v", doc.Roots[0].Attributes)
	}
	if a := doc.Roots[0].Attributes["a"]; a == nil || *a != "b" {
		t.Fatalf("attribute after __proto__ got %#v", a)
	}

	doc = parseForParity(`<div __proto__></div>`)
	got = doc.Roots[0].AttributeNames()
	if got == nil || len(got) != 0 {
		t.Fatalf("valueless __proto__ attribute names got %#v want empty slice", got)
	}
	if doc.Roots[0].Attributes == nil || len(doc.Roots[0].Attributes) != 0 {
		t.Fatalf("valueless __proto__ attributes got %#v want empty map", doc.Roots[0].Attributes)
	}

	doc = parseForParity(`<div __proto__=x></div>`)
	got = doc.Roots[0].AttributeNames()
	want = []string{"__proto__"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("valued __proto__ attribute names got %#v want %#v", got, want)
	}
	if proto := doc.Roots[0].Attributes["__proto__"]; proto == nil || *proto != "x" {
		t.Fatalf("valued __proto__ attribute got %#v want x", proto)
	}

	doc = parseForParity(`<div __proto__="x" a=b></div>`)
	got = doc.Roots[0].AttributeNames()
	want = []string{"__proto__", "a"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("quoted __proto__ attribute names got %#v want %#v", got, want)
	}
	if proto := doc.Roots[0].Attributes["__proto__"]; proto == nil || *proto != `"x"` {
		t.Fatalf("quoted __proto__ attribute got %#v want quoted x", proto)
	}
	if a := doc.Roots[0].Attributes["a"]; a == nil || *a != "b" {
		t.Fatalf("attribute after quoted __proto__ got %#v", a)
	}

	doc = parseForParity(`<div __proto__ __proto__></div>`)
	got = doc.Roots[0].AttributeNames()
	want = []string{"__proto__"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("duplicate valueless __proto__ attribute names got %#v want %#v", got, want)
	}
	if proto, ok := doc.Roots[0].Attributes["__proto__"]; !ok || proto != nil {
		t.Fatalf("duplicate valueless __proto__ attribute got %#v ok=%v want own nil", proto, ok)
	}

	doc = parseForParity(`<div __proto__=x __proto__=y></div>`)
	got = doc.Roots[0].AttributeNames()
	want = []string{"__proto__"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("duplicate valued __proto__ attribute names got %#v want %#v", got, want)
	}
	if proto := doc.Roots[0].Attributes["__proto__"]; proto == nil || *proto != "y" {
		t.Fatalf("duplicate valued __proto__ attribute got %#v want y", proto)
	}
}

func TestNodeIsSameTagUnicodeLowercaseMatchesForkSource(t *testing.T) {
	if !(&Node{Tag: "Ä"}).IsSameTag("ä") {
		t.Fatalf("Ä should match ä")
	}
	if !(&Node{Tag: "Σ"}).IsSameTag("σ") {
		t.Fatalf("Σ should match σ")
	}
	if (&Node{Tag: "Σ"}).IsSameTag("ς") {
		t.Fatalf("Σ should not match final sigma")
	}
	if (&Node{Tag: "İ"}).IsSameTag("i") {
		t.Fatalf("İ should not match i")
	}
}

func TestParserMissingTagsAndFindNodeBefore(t *testing.T) {
	parser := NewHTMLParser(NewHTMLDataManager(LanguageServiceOptions{}))
	htmlDoc := parser.Parse(`<h1><div><span></h1>`, parser.dataManager.GetVoidElements("html"))
	if len(htmlDoc.Roots) != 1 {
		t.Fatalf("expected one root")
	}
	h1 := htmlDoc.Roots[0]
	if h1.Tag != "h1" || !h1.Closed || h1.Children[0].Closed {
		t.Fatalf("unexpected missing tag recovery: %#v", h1)
	}
	str := `<div><input type="button"><span><br><hr></span></div>`
	htmlDoc = parser.Parse(str, parser.dataManager.GetVoidElements("html"))
	cases := map[int]string{1: "div", 6: "input", 27: "span", 33: "br", 37: "hr", 53: "div"}
	for offset, tag := range cases {
		if got := htmlDoc.FindNodeBefore(offset).Tag; got != tag {
			t.Fatalf("offset %d: got %q want %q", offset, got, tag)
		}
	}
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
