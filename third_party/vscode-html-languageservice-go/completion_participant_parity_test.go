package htmlservice

import (
	"reflect"
	"sort"
	"testing"
)

type expectedAttributeParticipant struct {
	Tag            string
	Attribute      string
	Value          string
	ReplaceContent string
	Attributes     map[string]*string
}

func TestCompletionParticipantAttributeValueBaselineParity(t *testing.T) {
	assertAttributeParticipant(t, `<|`, nil)
	assertAttributeParticipant(t, `<div |>`, nil)
	assertAttributeParticipant(t, `<div class|>`, nil)
	assertAttributeParticipant(t, `<div>|`, nil)
	assertAttributeParticipant(t, `<div>|</div>`, nil)

	assertAttributeParticipant(t, `<div class="|"></div>`, []expectedAttributeParticipant{{
		Tag: "div", Attribute: "class", Value: "", ReplaceContent: `""`,
	}})
	assertAttributeParticipant(t, `<div class="|f"></div>`, []expectedAttributeParticipant{{
		Tag: "div", Attribute: "class", Value: "", ReplaceContent: `"f"`,
	}})
	assertAttributeParticipant(t, `<div class="foo|"></div>`, []expectedAttributeParticipant{{
		Tag: "div", Attribute: "class", Value: "foo", ReplaceContent: `"foo"`,
	}})
	assertAttributeParticipant(t, `<div class='foo'|></div>`, []expectedAttributeParticipant{{
		Tag: "div", Attribute: "class", Value: "", ReplaceContent: `'foo'`,
	}})
	assertAttributeParticipant(t, `<div class=|'foo'></div>`, []expectedAttributeParticipant{{
		Tag: "div", Attribute: "class", Value: "", ReplaceContent: `'foo'`,
	}})
	assertAttributeParticipant(t, `<div class=|foo></div>`, []expectedAttributeParticipant{{
		Tag: "div", Attribute: "class", Value: "", ReplaceContent: `foo`,
	}})
	assertAttributeParticipant(t, `<div class=foo|></div>`, []expectedAttributeParticipant{{
		Tag: "div", Attribute: "class", Value: "foo", ReplaceContent: `foo`,
	}})
	assertAttributeParticipant(t, `<div class=fo|o></div>`, []expectedAttributeParticipant{{
		Tag: "div", Attribute: "class", Value: "fo", ReplaceContent: `foo`,
	}})
	stylesheet := `"stylesheet"`
	emptyQuoted := `""`
	assertAttributeParticipant(t, `<link rel="stylesheet" href="|">`, []expectedAttributeParticipant{{
		Tag:            "link",
		Attribute:      "href",
		Value:          "",
		ReplaceContent: `""`,
		Attributes: map[string]*string{
			"rel":  &stylesheet,
			"href": &emptyQuoted,
		},
	}})
}

func TestCompletionParticipantContentBaselineParity(t *testing.T) {
	assertContentParticipantCount(t, `<|`, 0)
	assertContentParticipantCount(t, `<div |>`, 0)
	assertContentParticipantCount(t, `<div class|>`, 0)
	assertContentParticipantCount(t, `<div class="|">`, 0)
	assertContentParticipantCount(t, `<div class="foo |">`, 0)
	assertContentParticipantCount(t, `<div>|`, 0)
	assertContentParticipantCount(t, `<div>|</div>`, 0)
	assertContentParticipantCount(t, `<br>|`, 0)
	assertContentParticipantCount(t, `<div class="foo">d|`, 1)
	assertContentParticipantCount(t, `<div class="foo">d|</div>`, 1)
	assertContentParticipantCount(t, `<!-- a| -->`, 0)
	assertContentParticipantCount(t, `<!DOCTYPE a|>`, 0)
}

func TestCompletionParticipantRunsBeforePathReadDirectoryMatchesForkSource(t *testing.T) {
	events := []string{}
	ls := GetLanguageService(LanguageServiceOptions{FileSystemProvider: eventFS{events: &events}})
	doc, pos := markedDocument(`<script src="|">`)
	participant := &capturingParticipant{document: doc, position: pos, events: &events}
	ls.SetCompletionParticipants([]CompletionParticipant{participant})
	htmlDoc := ls.ParseHTMLDocument(doc)
	list := ls.DoComplete2(doc, pos, htmlDoc, identityContext{}, nil)
	if len(list.Items) == 0 {
		t.Fatalf("expected path items")
	}
	if !reflect.DeepEqual(events, []string{"participant", "fs"}) {
		t.Fatalf("events got %#v want participant then fs", events)
	}
}

func assertAttributeParticipant(t *testing.T, marked string, expected []expectedAttributeParticipant) {
	t.Helper()
	ls := GetLanguageService()
	doc, pos := markedDocument(marked)
	participant := &capturingParticipant{document: doc, position: pos}
	ls.SetCompletionParticipants([]CompletionParticipant{participant})
	htmlDoc := ls.ParseHTMLDocument(doc)
	_ = ls.DoComplete(doc, pos, htmlDoc, nil)
	actual := participant.attributes
	sort.Slice(actual, func(i, j int) bool {
		a := actual[i]
		b := actual[j]
		return a.Tag+a.Attribute+a.Value < b.Tag+b.Attribute+b.Value
	})
	sort.Slice(expected, func(i, j int) bool {
		a := expected[i]
		b := expected[j]
		return a.Tag+a.Attribute+a.Value < b.Tag+b.Attribute+b.Value
	})
	for i := range expected {
		if expected[i].Attributes == nil && i < len(actual) {
			actual[i].Attributes = nil
		}
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("%s participant attrs: got %#v want %#v", marked, actual, expected)
	}
	if participant.badDocument || participant.badPosition {
		t.Fatalf("participant context document/position mismatch for %s", marked)
	}
}

func assertContentParticipantCount(t *testing.T, marked string, expected int) {
	t.Helper()
	ls := GetLanguageService()
	doc, pos := markedDocument(marked)
	participant := &capturingParticipant{document: doc, position: pos}
	ls.SetCompletionParticipants([]CompletionParticipant{participant})
	htmlDoc := ls.ParseHTMLDocument(doc)
	_ = ls.DoComplete(doc, pos, htmlDoc, nil)
	if participant.contentCount != expected {
		t.Fatalf("%s content callback count: got %d want %d", marked, participant.contentCount, expected)
	}
	if participant.badDocument || participant.badPosition {
		t.Fatalf("participant context document/position mismatch for %s", marked)
	}
}

func markedDocument(marked string) (*TextDocument, Position) {
	offset := stringsIndex(marked, "|")
	text := marked[:offset] + marked[offset+1:]
	doc := NewTextDocument("test://test/test.html", "html", 0, text)
	return doc, doc.PositionAt(offset)
}

type capturingParticipant struct {
	document     *TextDocument
	position     Position
	attributes   []expectedAttributeParticipant
	contentCount int
	events       *[]string
	badDocument  bool
	badPosition  bool
}

func (p *capturingParticipant) OnHTMLAttributeValue(context HtmlAttributeValueContext) {
	if p.events != nil {
		*p.events = append(*p.events, "participant")
	}
	if context.Document != p.document {
		p.badDocument = true
	}
	if context.Position != p.position {
		p.badPosition = true
	}
	p.attributes = append(p.attributes, expectedAttributeParticipant{
		Tag:            context.Tag,
		Attribute:      context.Attribute,
		Value:          context.Value,
		ReplaceContent: context.Document.GetText(context.Range),
		Attributes:     context.Attributes,
	})
}

func (p *capturingParticipant) OnHTMLContent(context HtmlContentContext) {
	if context.Document != p.document {
		p.badDocument = true
	}
	if context.Position != p.position {
		p.badPosition = true
	}
	p.contentCount++
}
