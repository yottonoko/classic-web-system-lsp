package htmlservice

import (
	"reflect"
	"testing"
)

type parserNodeJSON struct {
	Tag         string
	Start       int
	End         int
	EndTagStart *int
	Closed      bool
	Children    []parserNodeJSON
}

type parserNodeAttributesJSON struct {
	Tag        string
	Attributes map[string]*string
	Children   []parserNodeAttributesJSON
}

func parseForParity(input string) *HTMLDocument {
	manager := NewHTMLDataManager(LanguageServiceOptions{})
	return NewHTMLParser(manager).Parse(input, manager.GetVoidElements("html"))
}

func nodeJSON(node *Node) parserNodeJSON {
	result := parserNodeJSON{Tag: node.Tag, Start: node.Start, End: node.End, EndTagStart: node.EndTagStart, Closed: node.Closed}
	for _, child := range node.Children {
		result.Children = append(result.Children, nodeJSON(child))
	}
	return result
}

func nodeAttributesJSON(node *Node) parserNodeAttributesJSON {
	result := parserNodeAttributesJSON{
		Tag:        node.Tag,
		Attributes: node.Attributes,
		Children:   []parserNodeAttributesJSON{},
	}
	for _, child := range node.Children {
		result.Children = append(result.Children, nodeAttributesJSON(child))
	}
	return result
}

func intPtr(v int) *int { return &v }
func stringPtr(v string) *string {
	return &v
}

func assertParserDocument(t *testing.T, input string, expected []parserNodeJSON) {
	t.Helper()
	doc := parseForParity(input)
	var actual []parserNodeJSON
	for _, root := range doc.Roots {
		actual = append(actual, nodeJSON(root))
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("parse %q\n got: %#v\nwant: %#v", input, actual, expected)
	}
}

func assertParserAttributes(t *testing.T, input string, expected []parserNodeAttributesJSON) {
	t.Helper()
	doc := parseForParity(input)
	actual := []parserNodeAttributesJSON{}
	for _, root := range doc.Roots {
		actual = append(actual, nodeAttributesJSON(root))
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("parse attrs %q\n got: %#v\nwant: %#v", input, actual, expected)
	}
}

func TestParserBaselineTreeParity(t *testing.T) {
	assertParserDocument(t, `<html></html>`, []parserNodeJSON{{Tag: "html", Start: 0, End: 13, EndTagStart: intPtr(6), Closed: true}})
	assertParserDocument(t, `<html><body></body></html>`, []parserNodeJSON{{Tag: "html", Start: 0, End: 26, EndTagStart: intPtr(19), Closed: true, Children: []parserNodeJSON{{Tag: "body", Start: 6, End: 19, EndTagStart: intPtr(12), Closed: true}}}})
	assertParserDocument(t, `<html><head></head><body></body></html>`, []parserNodeJSON{{Tag: "html", Start: 0, End: 39, EndTagStart: intPtr(32), Closed: true, Children: []parserNodeJSON{{Tag: "head", Start: 6, End: 19, EndTagStart: intPtr(12), Closed: true}, {Tag: "body", Start: 19, End: 32, EndTagStart: intPtr(25), Closed: true}}}})
	assertParserDocument(t, `<br/>`, []parserNodeJSON{{Tag: "br", Start: 0, End: 5, Closed: true}})
	assertParserDocument(t, `<div><br/><span></span></div>`, []parserNodeJSON{{Tag: "div", Start: 0, End: 29, EndTagStart: intPtr(23), Closed: true, Children: []parserNodeJSON{{Tag: "br", Start: 5, End: 10, Closed: true}, {Tag: "span", Start: 10, End: 23, EndTagStart: intPtr(16), Closed: true}}}})
	assertParserDocument(t, `<meta>`, []parserNodeJSON{{Tag: "meta", Start: 0, End: 6, Closed: true}})
	assertParserDocument(t, `<div><input type="button"><span><br><br></span></div>`, []parserNodeJSON{{Tag: "div", Start: 0, End: 53, EndTagStart: intPtr(47), Closed: true, Children: []parserNodeJSON{{Tag: "input", Start: 5, End: 26, Closed: true}, {Tag: "span", Start: 26, End: 47, EndTagStart: intPtr(40), Closed: true, Children: []parserNodeJSON{{Tag: "br", Start: 32, End: 36, Closed: true}, {Tag: "br", Start: 36, End: 40, Closed: true}}}}}})
}

func TestParserOffsetsUseUTF16CodeUnits(t *testing.T) {
	doc := parseForParity(`😀<div></div>`)
	if len(doc.Roots) != 1 {
		t.Fatalf("root count got %d want 1", len(doc.Roots))
	}
	root := doc.Roots[0]
	if root.Start != 2 || root.StartTagEnd == nil || *root.StartTagEnd != 7 || root.EndTagStart == nil || *root.EndTagStart != 7 || root.End != 13 {
		t.Fatalf("UTF-16 node offsets got start=%d startTagEnd=%v endTagStart=%v end=%d", root.Start, root.StartTagEnd, root.EndTagStart, root.End)
	}
	if got := doc.FindNodeBefore(3).Tag; got != "div" {
		t.Fatalf("FindNodeBefore with UTF-16 offset got %q want div", got)
	}
}

func TestParserBaselineRecoveryParity(t *testing.T) {
	assertParserDocument(t, `</meta>`, nil)
	assertParserDocument(t, `<div></div></div>`, []parserNodeJSON{{Tag: "div", Start: 0, End: 11, EndTagStart: intPtr(5), Closed: true}})
	assertParserDocument(t, `<div><div></div>`, []parserNodeJSON{{Tag: "div", Start: 0, End: 16, Closed: false, Children: []parserNodeJSON{{Tag: "div", Start: 5, End: 16, EndTagStart: intPtr(10), Closed: true}}}})
	assertParserDocument(t, `<title><div></title>`, []parserNodeJSON{{Tag: "title", Start: 0, End: 20, EndTagStart: intPtr(12), Closed: true, Children: []parserNodeJSON{{Tag: "div", Start: 7, End: 12, Closed: false}}}})
	assertParserDocument(t, `<h1><div><span></h1>`, []parserNodeJSON{{Tag: "h1", Start: 0, End: 20, EndTagStart: intPtr(15), Closed: true, Children: []parserNodeJSON{{Tag: "div", Start: 4, End: 15, Closed: false, Children: []parserNodeJSON{{Tag: "span", Start: 9, End: 15, Closed: false}}}}}})
	assertParserDocument(t, `<div><div</div>`, []parserNodeJSON{{Tag: "div", Start: 0, End: 15, EndTagStart: intPtr(9), Closed: true, Children: []parserNodeJSON{{Tag: "div", Start: 5, End: 9, Closed: false}}}})
	assertParserDocument(t, "<div><div\n</div>", []parserNodeJSON{{Tag: "div", Start: 0, End: 16, EndTagStart: intPtr(10), Closed: true, Children: []parserNodeJSON{{Tag: "div", Start: 5, End: 10, Closed: false}}}})
	assertParserDocument(t, `<div><div></div</div>`, []parserNodeJSON{{Tag: "div", Start: 0, End: 21, EndTagStart: intPtr(15), Closed: true, Children: []parserNodeJSON{{Tag: "div", Start: 5, End: 15, EndTagStart: intPtr(10), Closed: true}}}})
}

func TestParserBaselineFindNodeBeforeParity(t *testing.T) {
	str := `<div><input type="button"><span><br><hr></span></div>`
	doc := parseForParity(str)
	cases := []struct {
		offset int
		tag    string
	}{
		{0, ""}, {1, "div"}, {5, "div"}, {6, "input"}, {25, "input"}, {26, "input"},
		{27, "span"}, {32, "span"}, {33, "br"}, {36, "br"}, {37, "hr"}, {40, "hr"},
		{41, "hr"}, {42, "hr"}, {47, "span"}, {48, "span"}, {52, "span"}, {53, "div"},
	}
	for _, tc := range cases {
		node := doc.FindNodeBefore(tc.offset)
		tag := ""
		if node != nil {
			tag = node.Tag
		}
		if tag != tc.tag {
			t.Fatalf("offset %d: got %q want %q", tc.offset, tag, tc.tag)
		}
	}
	doc = parseForParity(`<div><span><br></div>`)
	for _, tc := range []struct {
		offset int
		tag    string
	}{{15, "br"}, {18, "br"}, {21, "div"}} {
		if got := doc.FindNodeBefore(tc.offset).Tag; got != tc.tag {
			t.Fatalf("incomplete offset %d: got %q want %q", tc.offset, got, tc.tag)
		}
	}
}

func TestParserBaselineAttributesParity(t *testing.T) {
	assertParserAttributes(t, `<div class="these are my-classes" id="test"><span aria-describedby="test"></span></div>`, []parserNodeAttributesJSON{{
		Tag: "div",
		Attributes: map[string]*string{
			"class": stringPtr(`"these are my-classes"`),
			"id":    stringPtr(`"test"`),
		},
		Children: []parserNodeAttributesJSON{{
			Tag: "span",
			Attributes: map[string]*string{
				"aria-describedby": stringPtr(`"test"`),
			},
			Children: []parserNodeAttributesJSON{},
		}},
	}})
	assertParserAttributes(t, `<div checked id="test"></div>`, []parserNodeAttributesJSON{{
		Tag: "div",
		Attributes: map[string]*string{
			"checked": nil,
			"id":      stringPtr(`"test"`),
		},
		Children: []parserNodeAttributesJSON{},
	}})
}
