package htmlservice

import (
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Node represents a parsed HTML element or document node.
type Node struct {
	Tag             string
	Closed          bool
	Start           int
	StartTagEnd     *int
	End             int
	EndTagStart     *int
	Children        []*Node
	Parent          *Node
	Attributes      map[string]*string
	attributeNames  []string
	attrProtoNull   bool
	startByte       int
	startTagEndByte *int
	endByte         int
	endTagStartByte *int
	startTagEnd     int
	endTagStart     int
	startTagEndB    int
	endTagStartB    int
}

func (n *Node) IsSameTag(tag string) bool {
	if n.Tag == "" {
		return tag == ""
	}
	if same, ok := asciiEqualFoldToLower(n.Tag, tag); ok {
		return same
	}
	return utf16Len(n.Tag) == utf16Len(tag) && ecmaLower(n.Tag) == tag
}

func (n *Node) FirstChild() *Node {
	if len(n.Children) == 0 {
		return nil
	}
	return n.Children[0]
}

func (n *Node) LastChild() *Node {
	if len(n.Children) == 0 {
		return nil
	}
	return n.Children[len(n.Children)-1]
}

func (n *Node) AttributeNames() []string {
	if len(n.Attributes) == 0 {
		return []string{}
	}
	if len(n.attributeNames) > 0 {
		return jsObjectKeyOrder(n.attributeNames)
	}
	names := make([]string, 0, len(n.Attributes))
	for name := range n.Attributes {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func jsObjectKeyOrder(names []string) []string {
	type indexedName struct {
		name  string
		index uint64
	}
	indexed := make([]indexedName, 0, len(names))
	ordinary := make([]string, 0, len(names))
	for _, name := range names {
		if index, ok := jsArrayIndexName(name); ok {
			indexed = append(indexed, indexedName{name: name, index: index})
		} else {
			ordinary = append(ordinary, name)
		}
	}
	sort.Slice(indexed, func(i, j int) bool {
		return indexed[i].index < indexed[j].index
	})
	result := make([]string, 0, len(names))
	for _, item := range indexed {
		result = append(result, item.name)
	}
	return append(result, ordinary...)
}

func jsArrayIndexName(name string) (uint64, bool) {
	if name == "" || len(name) > 1 && name[0] == '0' {
		return 0, false
	}
	for i := 0; i < len(name); i++ {
		if name[i] < '0' || name[i] > '9' {
			return 0, false
		}
	}
	index, err := strconv.ParseUint(name, 10, 32)
	if err != nil || index == 1<<32-1 {
		return 0, false
	}
	if strconv.FormatUint(index, 10) != name {
		return 0, false
	}
	return index, true
}

func (n *Node) FindNodeBefore(offset int) *Node {
	idx := sort.Search(len(n.Children), func(i int) bool { return offset <= n.Children[i].Start }) - 1
	if idx >= 0 {
		child := n.Children[idx]
		if offset > child.Start {
			if offset < child.End {
				return child.FindNodeBefore(offset)
			}
			lastChild := child.LastChild()
			if lastChild != nil && lastChild.End == child.End {
				return child.FindNodeBefore(offset)
			}
			return child
		}
	}
	return n
}

func (n *Node) FindNodeAt(offset int) *Node {
	idx := sort.Search(len(n.Children), func(i int) bool { return offset <= n.Children[i].Start }) - 1
	if idx >= 0 {
		child := n.Children[idx]
		if offset > child.Start && offset <= child.End {
			return child.FindNodeAt(offset)
		}
	}
	return n
}

func (n *Node) findNodeBeforeByte(offset int) *Node {
	idx := sort.Search(len(n.Children), func(i int) bool { return offset <= n.Children[i].startByte }) - 1
	if idx >= 0 {
		child := n.Children[idx]
		if offset > child.startByte {
			if offset < child.endByte {
				return child.findNodeBeforeByte(offset)
			}
			lastChild := child.LastChild()
			if lastChild != nil && lastChild.endByte == child.endByte {
				return child.findNodeBeforeByte(offset)
			}
			return child
		}
	}
	return n
}

func (n *Node) findNodeAtByte(offset int) *Node {
	idx := sort.Search(len(n.Children), func(i int) bool { return offset <= n.Children[i].startByte }) - 1
	if idx >= 0 {
		child := n.Children[idx]
		if offset > child.startByte && offset <= child.endByte {
			return child.findNodeAtByte(offset)
		}
	}
	return n
}

// HTMLDocument contains parsed HTML roots and node lookup helpers.
type HTMLDocument struct {
	Roots              []*Node
	root               *Node
	dataAttributeNames []string
	dataAttributeSeen  map[string]bool
}

func (d *HTMLDocument) FindNodeBefore(offset int) *Node {
	if d.root == nil {
		return nil
	}
	return d.root.FindNodeBefore(offset)
}

func (d *HTMLDocument) FindNodeAt(offset int) *Node {
	if d.root == nil {
		return nil
	}
	return d.root.FindNodeAt(offset)
}

func (d *HTMLDocument) findNodeBeforeByte(offset int) *Node {
	if d.root == nil {
		return nil
	}
	return d.root.findNodeBeforeByte(offset)
}

func (d *HTMLDocument) findNodeAtByte(offset int) *Node {
	if d.root == nil {
		return nil
	}
	return d.root.findNodeAtByte(offset)
}

// HTMLParser parses text documents into HTML document trees.
type HTMLParser struct {
	dataManager *HTMLDataManager
}

func NewHTMLParser(dataManager *HTMLDataManager) *HTMLParser {
	return &HTMLParser{dataManager: dataManager}
}

func (p *HTMLParser) ParseDocument(document *TextDocument) *HTMLDocument {
	return p.parse(document.GetText(), p.dataManager.GetVoidElements(document.LanguageID), p.dataManager.getVoidElementSet(document.LanguageID), &document.textIndex)
}

func (p *HTMLParser) Parse(text string, voidElements []string) *HTMLDocument {
	return p.parse(text, voidElements, voidElementSet(voidElements), nil)
}

func (p *HTMLParser) parse(text string, voidElements []string, voidSet map[string]bool, textIndex *utf16Index) *HTMLDocument {
	scanner := newScannerAtByteWithIndex(text, 0, ScannerStateWithinContent, true, textIndex).(*htmlScanner)
	textEnd := scanner.textLengthCU()
	htmlDocument := &Node{Start: 0, End: textEnd, Children: []*Node{}, startByte: 0, endByte: len(text)}
	document := &HTMLDocument{root: htmlDocument}
	curr := htmlDocument
	endTagStart := -1
	endTagStartByte := -1
	endTagName := ""
	pendingAttribute := ""
	token := scanner.Scan()
	for token != TokenTypeEOS {
		switch token {
		case TokenTypeStartTagOpen:
			child := &Node{Start: scanner.GetTokenOffset(), End: textEnd, Children: []*Node{}, Parent: curr, startByte: scanner.GetTokenByteOffset(), endByte: len(text)}
			curr.Children = append(curr.Children, child)
			curr = child
		case TokenTypeStartTag:
			curr.Tag = scanner.GetTokenText()
		case TokenTypeStartTagClose:
			if curr.Parent != nil {
				curr.End = scanner.GetTokenEnd()
				curr.endByte = scanner.GetTokenByteEnd()
				if scanner.GetTokenLength() > 0 {
					curr.startTagEnd = scanner.GetTokenEnd()
					curr.startTagEndB = scanner.GetTokenByteEnd()
					curr.StartTagEnd = &curr.startTagEnd
					curr.startTagEndByte = &curr.startTagEndB
					if curr.Tag != "" && isVoidElement(curr.Tag, voidElements, voidSet, p.dataManager) {
						curr.Closed = true
						curr = curr.Parent
					}
				} else {
					curr = curr.Parent
				}
			}
		case TokenTypeStartTagSelfClose:
			if curr.Parent != nil {
				curr.Closed = true
				curr.End = scanner.GetTokenEnd()
				curr.endByte = scanner.GetTokenByteEnd()
				curr.startTagEnd = scanner.GetTokenEnd()
				curr.startTagEndB = scanner.GetTokenByteEnd()
				curr.StartTagEnd = &curr.startTagEnd
				curr.startTagEndByte = &curr.startTagEndB
				curr = curr.Parent
			}
		case TokenTypeEndTagOpen:
			endTagStart = scanner.GetTokenOffset()
			endTagStartByte = scanner.GetTokenByteOffset()
			endTagName = ""
		case TokenTypeEndTag:
			endTagName = stringsLower(scanner.GetTokenText())
		case TokenTypeEndTagClose:
			node := curr
			for !node.IsSameTag(endTagName) && node.Parent != nil {
				node = node.Parent
			}
			if node.Parent != nil {
				for curr != node {
					curr.End = endTagStart
					curr.endByte = endTagStartByte
					curr.Closed = false
					curr = curr.Parent
				}
				curr.Closed = true
				curr.endTagStart = endTagStart
				curr.endTagStartB = endTagStartByte
				curr.EndTagStart = &curr.endTagStart
				curr.endTagStartByte = &curr.endTagStartB
				curr.End = scanner.GetTokenEnd()
				curr.endByte = scanner.GetTokenByteEnd()
				curr = curr.Parent
			}
		case TokenTypeAttributeName:
			pendingAttribute = scanner.GetTokenText()
			if curr.Attributes == nil {
				curr.Attributes = map[string]*string{}
			}
			if pendingAttribute == "__proto__" && !curr.attrProtoNull {
				curr.attrProtoNull = true
				break
			}
			if _, exists := curr.Attributes[pendingAttribute]; !exists {
				curr.attributeNames = append(curr.attributeNames, pendingAttribute)
			}
			curr.Attributes[pendingAttribute] = nil
			document.addDataAttributeName(pendingAttribute)
		case TokenTypeAttributeValue:
			value := scanner.GetTokenText()
			if curr.Attributes != nil && pendingAttribute != "" {
				if _, exists := curr.Attributes[pendingAttribute]; !exists {
					curr.attributeNames = append(curr.attributeNames, pendingAttribute)
				}
				v := value
				curr.Attributes[pendingAttribute] = &v
				pendingAttribute = ""
			}
		}
		token = scanner.Scan()
	}
	for curr.Parent != nil {
		curr.End = textEnd
		curr.endByte = len(text)
		curr.Closed = false
		curr = curr.Parent
	}
	document.Roots = htmlDocument.Children
	return document
}

func voidElementSet(voidElements []string) map[string]bool {
	set := make(map[string]bool, len(voidElements))
	for _, element := range voidElements {
		set[element] = true
	}
	return set
}

func isVoidElement(tag string, voidElements []string, voidSet map[string]bool, dataManager *HTMLDataManager) bool {
	if voidSet != nil {
		return voidSet[tag]
	}
	return dataManager.IsVoidElement(tag, voidElements)
}

func (d *HTMLDocument) addDataAttributeName(name string) {
	if !strings.HasPrefix(name, "data-") {
		return
	}
	if d.dataAttributeSeen == nil {
		d.dataAttributeSeen = map[string]bool{}
	}
	if d.dataAttributeSeen[name] {
		return
	}
	d.dataAttributeSeen[name] = true
	d.dataAttributeNames = append(d.dataAttributeNames, name)
}

func stringsLower(s string) string {
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if 'A' <= ch && ch <= 'Z' {
			b := []byte(s)
			b[i] = ch + 32
			for j := i + 1; j < len(b); j++ {
				if 'A' <= b[j] && b[j] <= 'Z' {
					b[j] += 32
				}
			}
			return string(b)
		}
	}
	return s
}

func stringsEqualFold(a, b string) bool {
	return stringsLower(a) == stringsLower(b)
}

func asciiEqualFoldToLower(value, lower string) (bool, bool) {
	if len(value) != len(lower) {
		return false, false
	}
	for i := 0; i < len(value); i++ {
		ch := value[i]
		if ch >= utf8.RuneSelf || lower[i] >= utf8.RuneSelf {
			return false, false
		}
		if 'A' <= ch && ch <= 'Z' {
			ch += 32
		}
		if ch != lower[i] {
			return false, true
		}
	}
	return true, true
}

func ecmaLower(value string) string {
	var builder strings.Builder
	changed := false
	for _, r := range value {
		if r == '\u0130' {
			builder.WriteString("i\u0307")
			changed = true
			continue
		}
		lower := strings.ToLower(string(r))
		if lower != string(r) {
			changed = true
		}
		builder.WriteString(lower)
	}
	if !changed {
		return value
	}
	return builder.String()
}
