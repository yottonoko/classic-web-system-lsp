package services

import (
	"fmt"
	"strconv"
	"strings"
)

// MarkedString represents a language-tagged markdown code block.
type MarkedString struct {
	Language string
	Value    string
}

// Specificity stores CSS selector specificity components.
type Specificity struct {
	ID   int
	Attr int
	Tag  int
}

// ElementAttribute is a printable selector-derived HTML attribute.
type ElementAttribute struct {
	Name  string
	Value *string
}

// Element is the selector printer's lightweight HTML tree representation.
type Element struct {
	Parent     *Element
	Children   []*Element
	Attributes []ElementAttribute
	root       bool
}

type selectorComponent struct {
	combinator string
	compound   string
}

type compoundElement struct {
	tag     string
	id      string
	classes []string
	attrs   []string
	pseudos []string
}

// SelectorToElement converts a CSS selector string into a printable element tree.
func SelectorToElement(selector string) *Element {
	root := newRootElement()
	builder := selectorElementBuilder{element: root}
	builder.processSelector(selector)
	return root
}

// SelectorToElementInDocument converts the selector at selectorOffset, including SCSS nesting context.
func SelectorToElementInDocument(text string, selectorOffset int, languageID string) *Element {
	target, ok := selectorPrintingBlockAtOffset(text, selectorOffset)
	if !ok {
		return SelectorToElement(text)
	}
	root := newRootElement()
	builder := selectorElementBuilder{element: root}
	for _, parentSelector := range selectorPrintingParentSelectors(text, target, languageID) {
		builder.processSelector(parentSelector)
	}
	builder.processSelector(selectorPartAtOffset(text, target.headStart, target.start, selectorOffset))
	return root
}

// ToElement converts a simple selector compound into a printable element.
func ToElement(compound string, parentElement ...*Element) *Element {
	var parent *Element
	if len(parentElement) > 0 {
		parent = parentElement[0]
	}
	return compoundToElement(compound, parent)
}

func newRootElement() *Element {
	return &Element{root: true}
}

func newLabelElement(label string) *Element {
	element := &Element{}
	element.AddAttr("name", stringPtr(label))
	return element
}

// FindAttribute returns the first attribute value for name.
func (e *Element) FindAttribute(name string) (string, bool) {
	if e == nil {
		return "", false
	}
	for _, attribute := range e.Attributes {
		if attribute.Name == name {
			if attribute.Value == nil {
				return "", false
			}
			return *attribute.Value, true
		}
	}
	return "", false
}

// AddChild appends child and updates its parent pointer.
func (e *Element) AddChild(child *Element) {
	if e == nil || child == nil {
		return
	}
	child.Parent = e
	e.Children = append(e.Children, child)
}

// AddAttr appends an attribute or merges repeated attribute names.
func (e *Element) AddAttr(name string, value *string) {
	if e == nil {
		return
	}
	for i := range e.Attributes {
		if e.Attributes[i].Name == name {
			if value == nil {
				return
			}
			if e.Attributes[i].Value == nil {
				copied := *value
				e.Attributes[i].Value = &copied
				return
			}
			*e.Attributes[i].Value += " " + *value
			return
		}
	}
	e.Attributes = append(e.Attributes, ElementAttribute{Name: name, Value: cloneStringPtr(value)})
}

// Append appends text to the last attribute value.
func (e *Element) Append(text string) {
	if e == nil || len(e.Attributes) == 0 {
		return
	}
	last := &e.Attributes[len(e.Attributes)-1]
	if last.Value == nil {
		last.Value = stringPtr(text)
		return
	}
	*last.Value += text
}

// Prepend prepends text to the first attribute value.
func (e *Element) Prepend(text string) {
	if e == nil || len(e.Attributes) == 0 {
		return
	}
	first := &e.Attributes[0]
	if first.Value == nil {
		first.Value = stringPtr(text)
		return
	}
	*first.Value = text + *first.Value
}

// FindRoot returns the topmost non-root element for this element branch.
func (e *Element) FindRoot() *Element {
	current := e
	for current != nil && current.Parent != nil && !current.Parent.root {
		current = current.Parent
	}
	return current
}

// RemoveChild detaches child from this element.
func (e *Element) RemoveChild(child *Element) bool {
	if e == nil || child == nil {
		return false
	}
	for i, existing := range e.Children {
		if existing == child {
			e.Children = append(e.Children[:i], e.Children[i+1:]...)
			child.Parent = nil
			return true
		}
	}
	return false
}

// Clone copies the element and optionally its descendants.
func (e *Element) Clone(cloneChildren bool) *Element {
	if e == nil {
		return nil
	}
	clone := &Element{root: e.root}
	for _, attribute := range e.Attributes {
		clone.Attributes = append(clone.Attributes, ElementAttribute{Name: attribute.Name, Value: cloneStringPtr(attribute.Value)})
	}
	if cloneChildren {
		for _, child := range e.Children {
			clone.AddChild(child.Clone(true))
		}
	}
	return clone
}

// CloneWithParent clones this element and its non-root ancestors.
func (e *Element) CloneWithParent() *Element {
	clone := e.Clone(false)
	if e != nil && e.Parent != nil && !e.Parent.root {
		parentClone := e.Parent.CloneWithParent()
		parentClone.AddChild(clone)
	}
	return clone
}

func SelectorToMarkedStrings(selector string) []MarkedString {
	selector = firstSelectorGroup(selector)
	return selectorToMarkedStrings(selector)
}

func SelectorToMarkedStringsAt(selector string, offset int) []MarkedString {
	selector = selectorPartAtOffset(selector, 0, len(selector), offset)
	return selectorToMarkedStrings(selector)
}

func selectorToMarkedStrings(selector string) []MarkedString {
	return []MarkedString{
		{Language: "html", Value: selectorHTML(selector)},
		{Value: selectorSpecificityMarkedString(CalculateSpecificity(selector))},
	}
}

func CalculateSpecificity(selector string) Specificity {
	var result Specificity
	for _, component := range parseSelectorComponents(selector) {
		result.add(compoundSpecificity(component.compound))
	}
	return result
}

func selectorSpecificityMarkedString(s Specificity) string {
	return fmt.Sprintf("[Selector Specificity](https://developer.mozilla.org/docs/Web/CSS/Specificity): (%d, %d, %d)", s.ID, s.Attr, s.Tag)
}

func selectorHTML(selector string) string {
	components := parseSelectorComponents(selector)
	if len(components) == 0 {
		return "<element>"
	}
	var lines []string
	depth := 0
	for i, component := range components {
		html := compoundToHTML(component.compound)
		switch {
		case i == 0:
			lines = append(lines, html)
		case component.combinator == ">":
			depth++
			lines = append(lines, strings.Repeat("  ", depth)+html)
		case component.combinator == "+":
			lines = append(lines, strings.Repeat("  ", depth)+html)
		case component.combinator == "~":
			lines = append(lines, strings.Repeat("  ", depth)+"⋮")
			lines = append(lines, strings.Repeat("  ", depth)+html)
		default:
			depth++
			lines = append(lines, strings.Repeat("  ", depth)+"…")
			depth++
			lines = append(lines, strings.Repeat("  ", depth)+html)
		}
	}
	return strings.Join(lines, "\n")
}

func parseSelectorComponents(selector string) []selectorComponent {
	selector = strings.TrimSpace(firstSelectorGroup(selector))
	var components []selectorComponent
	combinator := ""
	for i := 0; i < len(selector); {
		hadSpace := false
		for i < len(selector) && isCSSSpace(selector[i]) {
			hadSpace = true
			i++
		}
		if hadSpace && len(components) > 0 && combinator == "" {
			combinator = " "
		}
		if i >= len(selector) {
			break
		}
		if selector[i] == '>' || selector[i] == '+' || selector[i] == '~' {
			combinator = string(selector[i])
			i++
			continue
		}
		start := i
		parenDepth := 0
		bracketDepth := 0
		for i < len(selector) {
			if selector[i] == '\\' {
				i = advanceCSSEscape(selector, i)
				continue
			}
			next := skipCSSIgnored(selector, i)
			if next != i {
				i = next + 1
				continue
			}
			switch selector[i] {
			case '(':
				parenDepth++
			case ')':
				if parenDepth > 0 {
					parenDepth--
				}
			case '[':
				bracketDepth++
			case ']':
				if bracketDepth > 0 {
					bracketDepth--
				}
			case '>', '+', '~':
				if parenDepth == 0 && bracketDepth == 0 {
					goto compoundDone
				}
			default:
				if isCSSSpace(selector[i]) && parenDepth == 0 && bracketDepth == 0 {
					goto compoundDone
				}
			}
			i++
		}
	compoundDone:
		compound := strings.TrimSpace(selector[start:i])
		if compound != "" {
			components = append(components, selectorComponent{combinator: combinator, compound: compound})
		}
		combinator = ""
	}
	return components
}

type selectorElementBuilder struct {
	element    *Element
	prevSimple bool
}

func (b *selectorElementBuilder) processSelector(selector string) {
	if b.element == nil {
		b.element = newRootElement()
	}
	parentElement := (*Element)(nil)
	if !b.element.root && selectorHasParentReference(selector) {
		current := b.element.FindRoot()
		if current != nil && current.Parent != nil && current.Parent.root {
			parentElement = b.element
			b.element = current.Parent
			b.element.RemoveChild(current)
			b.prevSimple = false
		}
	}
	for index, component := range parseSelectorComponents(selector) {
		if index > 0 {
			switch component.combinator {
			case "+":
				if b.element.Parent != nil {
					b.element = b.element.Parent
				}
			case "~":
				if b.element.Parent != nil {
					b.element = b.element.Parent
				}
				b.element.AddChild(newLabelElement("⋮"))
			case ">":
			default:
				label := newLabelElement("…")
				b.element.AddChild(label)
				b.element = label
			}
		} else if b.prevSimple {
			label := newLabelElement("…")
			b.element.AddChild(label)
			b.element = label
		}
		element := compoundToElement(component.compound, parentElement)
		root := element.FindRoot()
		b.element.AddChild(root)
		b.element = element
		b.prevSimple = true
	}
}

func selectorHasParentReference(selector string) bool {
	for _, component := range parseSelectorComponents(selector) {
		if strings.Contains(component.compound, "&") {
			return true
		}
	}
	return false
}

func compoundToElement(compound string, parentElement *Element) *Element {
	if parentElement != nil && strings.Contains(compound, "&") {
		return parentReferencedElement(compound, parentElement)
	}
	result := &Element{}
	addCompoundFragment(result, compound)
	return result
}

func parentReferencedElement(compound string, parentElement *Element) *Element {
	segments := strings.Split(compound, "&")
	if len(segments) == 1 {
		result := &Element{}
		result.AddAttr("name", stringPtr(compound))
		return result
	}
	result := parentElement.CloneWithParent()
	if segments[0] != "" {
		if root := result.FindRoot(); root != nil {
			root.Prepend(cssUnescape(segments[0]))
		}
	}
	for i := 1; i < len(segments); i++ {
		if i > 1 {
			clone := parentElement.CloneWithParent()
			if root := clone.FindRoot(); root != nil {
				result.AddChild(root)
			}
			result = clone
		}
		suffix, rest := parentReferenceSuffix(segments[i])
		if suffix != "" {
			result.Append(cssUnescape(suffix))
		}
		if rest != "" {
			addCompoundFragment(result, rest)
		}
	}
	return result
}

func parentReferenceSuffix(segment string) (string, string) {
	end := 0
	for end < len(segment) {
		if segment[end] == '\\' {
			end = advanceCSSEscape(segment, end)
			continue
		}
		if !isSelectorIdentifierByte(segment[end]) {
			break
		}
		end++
	}
	return segment[:end], segment[end:]
}

func addCompoundFragment(element *Element, compound string) {
	for i := 0; i < len(compound); {
		switch compound[i] {
		case '*':
			element.AddAttr("name", stringPtr("element"))
			i++
		case '%':
			name, next := readIdentifier(compound, i+1)
			if name != "" {
				element.AddAttr("name", stringPtr("%"+name))
			}
			i = next
		case '.':
			name, next := readIdentifier(compound, i+1)
			if name != "" {
				element.AddAttr("class", stringPtr(name))
			}
			i = next
		case '#':
			name, next := readIdentifier(compound, i+1)
			if name != "" {
				element.AddAttr("id", stringPtr(name))
			}
			i = next
		case '[':
			end := strings.IndexByte(compound[i:], ']')
			if end == -1 {
				i = len(compound)
				continue
			}
			name, value := attributeElement(compound[i+1 : i+end])
			if name != "" {
				element.AddAttr(name, value)
			}
			i += end + 1
		case ':':
			colons := 1
			if i+1 < len(compound) && compound[i+1] == ':' {
				colons = 2
			}
			name, next := readIdentifier(compound, i+colons)
			if name != "" {
				prefix := ":"
				if colons == 2 {
					prefix = "::"
				}
				element.AddAttr(prefix+name, stringPtr(""))
			}
			if next < len(compound) && compound[next] == '(' {
				if close := matchingParenString(compound, next); close != -1 {
					next = close + 1
				}
			}
			i = next
		default:
			if compound[i] == '\\' || isIdentStart(compound[i]) {
				name, next := readIdentifier(compound, i)
				if name != "" {
					element.AddAttr("name", stringPtr(name))
				}
				i = next
			} else {
				i++
			}
		}
	}
}

func attributeElement(attr string) (string, *string) {
	attr = strings.TrimSpace(attr)
	for _, op := range []string{"~=", "|=", "^=", "$=", "*=", "="} {
		if index := strings.Index(attr, op); index != -1 {
			name := cssUnescape(strings.TrimSpace(attr[:index]))
			rawValue := cssUnescape(quotesRemove(strings.TrimSpace(attr[index+len(op):])))
			switch op {
			case "|=":
				rawValue += "-…"
			case "^=":
				rawValue += "…"
			case "$=":
				rawValue = "…" + rawValue
			case "~=":
				rawValue = " … " + rawValue + " … "
			case "*=":
				rawValue = "…" + rawValue + "…"
			}
			return name, stringPtr(rawValue)
		}
	}
	return cssUnescape(attr), nil
}

func selectorPrintingBlockAtOffset(text string, selectorOffset int) (cssBlock, bool) {
	for _, block := range parseCSSBlocks(text) {
		if block.headStart <= selectorOffset && selectorOffset < block.start {
			return block, true
		}
	}
	return cssBlock{}, false
}

func selectorPrintingParentSelectors(text string, target cssBlock, languageID string) []string {
	var parents []string
	for _, block := range parseCSSBlocks(text) {
		if block.start == target.start && block.end == target.end {
			continue
		}
		if block.bodyStart <= target.headStart && target.end <= block.bodyEnd {
			head := strings.TrimSpace(block.head)
			lower := strings.ToLower(head)
			if languageID == "scss" && strings.HasPrefix(lower, "@mixin") {
				parents = nil
				continue
			}
			if strings.HasPrefix(lower, "@at-root") {
				parents = nil
				continue
			}
			if strings.HasPrefix(lower, "@") {
				continue
			}
			parents = append(parents, selectorPartAtOffset(text, block.headStart, block.start, block.headStart))
		}
	}
	return parents
}

func selectorPartAtOffset(text string, start, end, offset int) string {
	parts := splitSelectorParts(text, start, end)
	for _, part := range parts {
		if part.start <= offset && offset <= part.end {
			return text[part.start:part.end]
		}
	}
	if len(parts) > 0 {
		return text[parts[0].start:parts[0].end]
	}
	return strings.TrimSpace(text[start:end])
}

func compoundToHTML(compound string) string {
	element := parseCompound(compound)
	tag := element.tag
	if tag == "" || tag == "*" {
		tag = "element"
	}
	var attrs []string
	if element.id != "" {
		attrs = append(attrs, `id="`+element.id+`"`)
	}
	if len(element.classes) > 0 {
		attrs = append(attrs, `class="`+strings.Join(element.classes, " ")+`"`)
	}
	attrs = append(attrs, element.attrs...)
	attrs = append(attrs, element.pseudos...)
	if len(attrs) == 0 {
		return "<" + tag + ">"
	}
	return "<" + tag + " " + strings.Join(attrs, " ") + ">"
}

func parseCompound(compound string) compoundElement {
	var element compoundElement
	for i := 0; i < len(compound); {
		switch compound[i] {
		case '*':
			if element.tag == "" {
				element.tag = "element"
			}
			i++
		case '.':
			name, next := readIdentifier(compound, i+1)
			if name != "" {
				element.classes = append(element.classes, name)
			}
			i = next
		case '#':
			name, next := readIdentifier(compound, i+1)
			element.id = name
			i = next
		case '[':
			end := strings.IndexByte(compound[i:], ']')
			if end == -1 {
				i = len(compound)
				continue
			}
			attr := strings.TrimSpace(compound[i+1 : i+end])
			if attr != "" {
				element.attrs = append(element.attrs, attributeHTML(attr))
			}
			i += end + 1
		case ':':
			colons := 1
			if i+1 < len(compound) && compound[i+1] == ':' {
				colons = 2
			}
			name, next := readIdentifier(compound, i+colons)
			if name != "" {
				prefix := ":"
				if colons == 2 {
					prefix = "::"
				}
				element.pseudos = append(element.pseudos, prefix+name)
			}
			if next < len(compound) && compound[next] == '(' {
				if close := matchingParenString(compound, next); close != -1 {
					next = close + 1
				}
			}
			i = next
		default:
			if isIdentStart(compound[i]) {
				name, next := readIdentifier(compound, i)
				if element.tag == "" {
					element.tag = name
				}
				i = next
			} else {
				i++
			}
		}
	}
	return element
}

func attributeHTML(attr string) string {
	for _, op := range []string{"~=", "|=", "^=", "$=", "*=", "="} {
		if index := strings.Index(attr, op); index != -1 {
			name := strings.TrimSpace(attr[:index])
			value := strings.Trim(strings.TrimSpace(attr[index+len(op):]), `"'`)
			return name + `="` + value + `"`
		}
	}
	return attr
}

func compoundSpecificity(compound string) Specificity {
	var result Specificity
	for i := 0; i < len(compound); {
		switch compound[i] {
		case '*':
			i++
		case '#':
			_, next := readIdentifier(compound, i+1)
			result.ID++
			i = next
		case '.':
			_, next := readIdentifier(compound, i+1)
			result.Attr++
			i = next
		case '[':
			result.Attr++
			if end := strings.IndexByte(compound[i:], ']'); end != -1 {
				i += end + 1
			} else {
				i = len(compound)
			}
		case ':':
			colons := 1
			if i+1 < len(compound) && compound[i+1] == ':' {
				colons = 2
			}
			name, next := readIdentifier(compound, i+colons)
			name = strings.ToLower(name)
			hasArgs := next < len(compound) && compound[next] == '('
			args := ""
			if hasArgs {
				if close := matchingParenString(compound, next); close != -1 {
					args = compound[next+1 : close]
					next = close + 1
				}
			}
			switch {
			case colons == 2 || name == "before" || name == "after" || name == "first-line" || name == "first-letter":
				result.Tag++
				if name == "slotted" && args != "" {
					result.add(mostSpecificSelector(args))
				}
			case name == "where":
			case name == "not" || name == "is" || name == "has":
				result.add(mostSpecificSelector(args))
			case name == "nth-child" || name == "nth-last-child":
				result.Attr++
				if selectorList := nthChildSelectorList(args); selectorList != "" {
					result.add(mostSpecificSelector(selectorList))
				}
			case name == "host" || name == "host-context":
				result.Attr++
				if args != "" {
					result.add(mostSpecificSelector(args))
				}
			default:
				result.Attr++
			}
			i = next
		default:
			if isIdentStart(compound[i]) {
				_, next := readIdentifier(compound, i)
				result.Tag++
				i = next
			} else {
				i++
			}
		}
	}
	return result
}

func mostSpecificSelector(selectorList string) Specificity {
	var best Specificity
	for _, selector := range splitSelectorList(selectorList) {
		score := CalculateSpecificity(selector)
		if compareSpecificity(score, best) > 0 {
			best = score
		}
	}
	return best
}

func nthChildSelectorList(args string) string {
	lower := strings.ToLower(args)
	parenDepth := 0
	bracketDepth := 0
	for i := 0; i < len(lower); i++ {
		next := skipCSSIgnored(lower, i)
		if next != i {
			i = next
			continue
		}
		switch lower[i] {
		case '(':
			parenDepth++
		case ')':
			if parenDepth > 0 {
				parenDepth--
			}
		case '[':
			bracketDepth++
		case ']':
			if bracketDepth > 0 {
				bracketDepth--
			}
		case 'o':
			if parenDepth == 0 && bracketDepth == 0 && i+1 < len(lower) && lower[i+1] == 'f' && selectorKeywordBoundary(lower, i-1) && selectorKeywordBoundary(lower, i+2) {
				return strings.TrimSpace(args[i+2:])
			}
		}
	}
	return ""
}

func selectorKeywordBoundary(text string, index int) bool {
	if index < 0 || index >= len(text) {
		return true
	}
	return !isSelectorIdentifierByte(text[index])
}

func compareSpecificity(a, b Specificity) int {
	if a.ID != b.ID {
		return a.ID - b.ID
	}
	if a.Attr != b.Attr {
		return a.Attr - b.Attr
	}
	return a.Tag - b.Tag
}

func splitSelectorList(selector string) []string {
	var result []string
	start := 0
	parenDepth := 0
	bracketDepth := 0
	for i := 0; i < len(selector); i++ {
		if selector[i] == '\\' {
			i = advanceCSSEscape(selector, i) - 1
			continue
		}
		next := skipCSSIgnored(selector, i)
		if next != i {
			i = next
			continue
		}
		switch selector[i] {
		case '(':
			parenDepth++
		case ')':
			if parenDepth > 0 {
				parenDepth--
			}
		case '[':
			bracketDepth++
		case ']':
			if bracketDepth > 0 {
				bracketDepth--
			}
		case ',':
			if parenDepth == 0 && bracketDepth == 0 {
				if part := strings.TrimSpace(selector[start:i]); part != "" {
					result = append(result, part)
				}
				start = i + 1
			}
		}
	}
	if part := strings.TrimSpace(selector[start:]); part != "" {
		result = append(result, part)
	}
	return result
}

func firstSelectorGroup(selector string) string {
	parts := splitSelectorList(selector)
	if len(parts) == 0 {
		return strings.TrimSpace(selector)
	}
	return parts[0]
}

func readIdentifier(text string, start int) (string, int) {
	i := start
	var builder strings.Builder
	for i < len(text) {
		if text[i] == '\\' {
			decoded, next, ok := readCSSEscape(text, i)
			if !ok {
				break
			}
			builder.WriteRune(decoded)
			i = next
			continue
		}
		if !isSelectorIdentifierByte(text[i]) {
			break
		}
		builder.WriteByte(text[i])
		i++
	}
	if builder.Len() == 0 {
		return "", start
	}
	return builder.String(), i
}

func matchingParenString(text string, open int) int {
	depth := 0
	for i := open; i < len(text); i++ {
		if text[i] == '\\' {
			i = advanceCSSEscape(text, i) - 1
			continue
		}
		next := skipCSSIgnored(text, i)
		if next != i {
			i = next
			continue
		}
		switch text[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func (s *Specificity) add(other Specificity) {
	s.ID += other.ID
	s.Attr += other.Attr
	s.Tag += other.Tag
}

func stringPtr(value string) *string {
	return &value
}

func cloneStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	return stringPtr(*value)
}

func quotesRemove(value string) string {
	if len(value) >= 2 {
		first := value[0]
		last := value[len(value)-1]
		if (first == '\'' || first == '"') && first == last {
			return value[1 : len(value)-1]
		}
	}
	return value
}

func cssUnescape(content string) string {
	var builder strings.Builder
	for i := 0; i < len(content); {
		if content[i] == '\\' {
			decoded, next, ok := readCSSEscape(content, i)
			if ok {
				builder.WriteRune(decoded)
				i = next
				continue
			}
		}
		builder.WriteByte(content[i])
		i++
	}
	return builder.String()
}

func advanceCSSEscape(text string, start int) int {
	_, next, ok := readCSSEscape(text, start)
	if !ok {
		return start + 1
	}
	return next
}

func readCSSEscape(text string, start int) (rune, int, bool) {
	if start >= len(text) || text[start] != '\\' {
		return 0, start, false
	}
	if start+1 >= len(text) {
		return '\\', start + 1, true
	}
	i := start + 1
	hexStart := i
	for i < len(text) && i-hexStart < 6 && isHexDigit(text[i]) {
		i++
	}
	if i > hexStart {
		value, err := strconv.ParseInt(text[hexStart:i], 16, 32)
		if err != nil {
			return 0, start, false
		}
		if i < len(text) && isCSSSpace(text[i]) {
			i++
		}
		return rune(value), i, true
	}
	return rune(text[i]), i + 1, true
}
