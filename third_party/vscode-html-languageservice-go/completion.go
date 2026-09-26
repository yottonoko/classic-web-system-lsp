package htmlservice

import (
	"path"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"
)

func doComplete(dataManager *HTMLDataManager, fs FileSystemProvider, caps *ClientCapabilities, participants []CompletionParticipant, document *TextDocument, position Position, htmlDocument *HTMLDocument, documentContext DocumentContext, options *CompletionConfiguration) CompletionList {
	offset := document.OffsetAt(position)
	text := document.GetText()
	if isRawTextCompletionBlocked(htmlDocument, offset) {
		return CompletionList{}
	}
	tagOpen := activeTagOpenBefore(text, offset)
	if hasInvalidTagCompletionPrefixAt(text, offset, tagOpen) {
		return CompletionList{}
	}
	ctx := completionContextWithTagOpen(text, offset, tagOpen)
	supportsMarkdown := supportsCompletionMarkdown(caps)
	switch ctx.kind {
	case "endtag":
		if options != nil && options.HideEndTagSuggestions != nil && *options.HideEndTagSuggestions {
			return CompletionList{}
		}
		return completeEndTag(dataManager, document, htmlDocument, offset, ctx, options, supportsMarkdown)
	case "tag":
		return completeTag(dataManager, document, offset, position, ctx, options, supportsMarkdown)
	case "attr":
		return completeAttributes(dataManager, document, offset, htmlDocument, ctx, options, supportsMarkdown)
	case "value":
		return completeAttributeValues(dataManager, fs, participants, document, offset, htmlDocument, documentContext, ctx, options, supportsMarkdown)
	case "entity":
		return completeEntities(document, offset, ctx)
	case "content":
		return completeContent(dataManager, participants, document, htmlDocument, offset, options)
	default:
		return CompletionList{}
	}
}

type completionCtx struct {
	kind          string
	tag           string
	attr          string
	rangeStart    int
	rangeEnd      int
	valueStart    int
	valueEnd      int
	rawValueStart int
	rawValueEnd   int
	quote         byte
	inOpenTag     bool
	openTagOnly   bool
	tagOpen       int
}

func completionContext(text string, offset int) completionCtx {
	return completionContextWithTagOpen(text, offset, activeTagOpenBefore(text, offset))
}

func completionContextWithTagOpen(text string, offset int, lt int) completionCtx {
	if offset > len(text) {
		offset = len(text)
	}
	if lt >= 0 {
		outerTagOpen := activeTagOpenBeforeLocal(text, lt)
		if malformedNestedTagCompletionWithOuter(text, lt, outerTagOpen) {
			return completionCtx{}
		}
		if lt+1 < len(text) && text[lt+1] == '!' {
			return completionCtx{}
		}
		if lt+1 < len(text) && text[lt+1] == '/' {
			if lt+2 < offset && !isElementNameStartChar(text[lt+2]) && !isSpace(text[lt+2]) {
				return completionCtx{}
			}
			nameEnd := lt + 2
			for nameEnd < len(text) && isNameChar(text[nameEnd]) {
				nameEnd++
			}
			if nameEnd > lt+2 && offset > nameEnd && strings.TrimSpace(text[nameEnd:offset]) == "" {
				return completionCtx{}
			}
			rangeEnd := scanNameEnd(text, offset)
			if offset > lt+2 && strings.TrimSpace(text[lt+2:offset]) == "" {
				rangeEnd = offset
			}
			return completionCtx{kind: "endtag", rangeStart: lt + 2, rangeEnd: rangeEnd, tagOpen: lt}
		}
		tagStart := lt + 1
		if tagStart < offset && !isElementNameStartChar(text[tagStart]) {
			if strings.TrimSpace(text[tagStart:offset]) == "" {
				return completionCtx{kind: "tag", rangeStart: tagStart, rangeEnd: scanNameEnd(text, offset), inOpenTag: outerTagOpen >= 0, tagOpen: lt}
			}
			nameStart := scanNameStart(text, offset)
			if nameStart > tagStart && strings.TrimSpace(text[tagStart:nameStart]) == "" && nameStart < offset && isElementNameStartChar(text[nameStart]) {
				return completionCtx{kind: "tag", rangeStart: nameStart, rangeEnd: scanNameEnd(text, offset), openTagOnly: true, tagOpen: lt}
			}
			return completionCtx{}
		}
		tagEnd := tagStart
		for tagEnd < len(text) && isNameChar(text[tagEnd]) {
			tagEnd++
		}
		tag := strings.ToLower(text[tagStart:tagEnd])
		if tag == "" {
			if strings.TrimSpace(text[tagStart:offset]) == "" {
				return completionCtx{kind: "tag", rangeStart: tagStart, rangeEnd: offset, inOpenTag: outerTagOpen >= 0, tagOpen: lt}
			}
			return completionCtx{}
		}
		if offset == tagStart && !hasWhitespace(text[tagStart:offset]) {
			return completionCtx{kind: "tag", rangeStart: tagStart, rangeEnd: scanNameEnd(text, offset), tagOpen: lt}
		}
		if offset <= tagEnd && !hasWhitespace(text[tagStart:offset]) {
			return completionCtx{kind: "tag", rangeStart: tagStart, rangeEnd: scanNameEnd(text, offset), openTagOnly: true, tagOpen: lt}
		}
		if tagEnd < offset && !isSpace(text[tagEnd]) {
			return completionCtx{}
		}
		attr := currentAttribute(text, lt+1, offset)
		if attr.ok {
			replaceStart := attr.valueStart
			replaceEnd := attr.valueEnd
			if offset < replaceStart {
				replaceStart = attr.rawValueStart
			} else {
				currentValue := text[attr.valueStart:minInt(offset, attr.valueEnd)]
				if idx := lastJSWhitespaceEnd(currentValue); idx >= 0 {
					replaceStart = attr.valueStart + idx
				}
				if attr.quote != 0 && attr.rawValueEnd == attr.valueEnd {
					replaceEnd = offset
				} else {
					for replaceEnd = offset; replaceEnd < attr.valueEnd; {
						r, size := utf8.DecodeRuneInString(text[replaceEnd:])
						if isJSWhitespaceRune(r) {
							break
						}
						replaceEnd += size
					}
				}
			}
			return completionCtx{
				kind:          "value",
				tag:           tag,
				attr:          attr.name,
				rangeStart:    replaceStart,
				rangeEnd:      replaceEnd,
				valueStart:    attr.valueStart,
				valueEnd:      attr.valueEnd,
				rawValueStart: attr.rawValueStart,
				rawValueEnd:   attr.rawValueEnd,
				quote:         attr.quote,
				tagOpen:       lt,
			}
		}
		if isAfterSelfCloseSlash(text, lt+1, offset) {
			return completionCtx{}
		}
		if containsScannerInvalidJSWhitespace(text[tagEnd:offset]) {
			return completionCtx{}
		}
		attrStart, attrEnd := attributeNameRangeAtCompletion(text, lt+1, offset)
		if attrStart < 0 {
			return completionCtx{}
		}
		return completionCtx{kind: "attr", tag: tag, rangeStart: attrStart, rangeEnd: attrEnd, tagOpen: lt}
	}
	if offset > 0 && text[offset-1] == '<' && !isInsideIgnoredMarkup(text, offset-1) {
		return completionCtx{kind: "tag", rangeStart: offset, rangeEnd: offset, tagOpen: offset - 1}
	}
	if isInsideIgnoredMarkup(text, offset) {
		return completionCtx{}
	}
	return completionCtx{kind: "content"}
}

func malformedNestedTagCompletion(text string, lt int) bool {
	return malformedNestedTagCompletionWithOuter(text, lt, activeTagOpenBefore(text, lt))
}

func malformedNestedTagCompletionWithOuter(text string, lt int, outerTagOpen int) bool {
	if outerTagOpen < 0 {
		return false
	}
	if lt+1 < len(text) && text[lt+1] == '/' {
		return true
	}
	return outerTagOpen+1 < len(text) && text[outerTagOpen+1] == '<'
}

func hasInvalidTagCompletionPrefix(text string, offset int) bool {
	return hasInvalidTagCompletionPrefixAt(text, offset, activeTagOpenBefore(text, offset))
}

func hasInvalidTagCompletionPrefixAt(text string, offset int, lt int) bool {
	if lt < 0 || lt+1 >= offset {
		return false
	}
	next := text[lt+1]
	if next == '/' {
		if lt+2 >= offset {
			return false
		}
		ch := text[lt+2]
		return !isElementNameStartChar(ch) && !isSpace(ch)
	}
	return !isElementNameStartChar(next) && !isSpace(next) && next != '!'
}

func entityContext(text string, offset int) (completionCtx, bool) {
	k := offset - 1
	for k >= 0 && isLetterOrDigit(text[k]) {
		k--
	}
	if k < 0 || text[k] != '&' {
		return completionCtx{}, false
	}
	return completionCtx{kind: "entity", rangeStart: k, rangeEnd: offset}, true
}

type attributeContext struct {
	name          string
	valueStart    int
	valueEnd      int
	rawValueStart int
	rawValueEnd   int
	quote         byte
	ok            bool
}

func currentAttribute(text string, tagContentStart, offset int) attributeContext {
	i := tagContentStart
	for i < offset && isNameChar(text[i]) {
		i++
	}
	for i < offset {
		for i < offset && isSpace(text[i]) {
			i++
		}
		if i < offset {
			r, _ := utf8.DecodeRuneInString(text[i:])
			if isJSWhitespaceRune(r) && !isSpace(text[i]) {
				return attributeContext{}
			}
		}
		nameStart := i
		for i < offset {
			size, ok := attributeNameCharAt(text, i)
			if !ok {
				break
			}
			i += size
		}
		if nameStart == i {
			i++
			continue
		}
		name := strings.ToLower(text[nameStart:i])
		for i < offset && isSpace(text[i]) {
			i++
		}
		if i < len(text) && text[i] == '=' {
			i++
			valueWhitespaceStart := i
			for i < offset && isSpace(text[i]) {
				i++
			}
			if i == offset && i > valueWhitespaceStart {
				return attributeContext{name: name, valueStart: i, valueEnd: i, rawValueStart: i, rawValueEnd: i, ok: true}
			}
			rawStart := i
			quote := byte(0)
			if i < len(text) && (text[i] == '"' || text[i] == '\'') {
				quote = text[i]
				i++
			}
			valueStart := i
			j := i
			for j < len(text) {
				if quote != 0 {
					if text[j] == quote {
						break
					}
					j++
					continue
				}
				r, size := utf8.DecodeRuneInString(text[j:])
				if isUnquotedAttributeValueStop(text, j, r) {
					break
				}
				j += size
			}
			valueEnd := j
			rawEnd := j
			if quote != 0 && rawEnd < len(text) && text[rawEnd] == quote {
				rawEnd++
			}
			if offset >= rawStart && offset <= rawEnd {
				return attributeContext{name: name, valueStart: valueStart, valueEnd: valueEnd, rawValueStart: rawStart, rawValueEnd: rawEnd, quote: quote, ok: true}
			}
			i = rawEnd
			continue
		}
	}
	return attributeContext{}
}

func activeTagOpenBefore(text string, offset int) int {
	if tagOpen, ok := activeTagOpenBeforeFast(text, offset); ok {
		return tagOpen
	}
	return activeTagOpenBeforeSlow(text, offset)
}

func activeTagOpenBeforeFast(text string, offset int) (int, bool) {
	if offset > len(text) {
		offset = len(text)
	}
	tagOpen := -1
	for i := offset - 1; i >= 0; i-- {
		if text[i] == '<' {
			tagOpen = i
			break
		}
	}
	if tagOpen < 0 || tagOpen+1 >= len(text) {
		return -1, true
	}
	next := text[tagOpen+1]
	if next != '/' && !isElementNameStartChar(next) {
		return -1, next == '!'
	}
	if isInsideIgnoredMarkupNear(text, tagOpen) || hasUnquotedTagCloseBefore(text, tagOpen+1, offset) {
		return -1, true
	}
	return tagOpen, true
}

func hasUnquotedTagCloseBefore(text string, start, end int) bool {
	quote := byte(0)
	for i := start; i < end; i++ {
		ch := text[i]
		if quote != 0 {
			if ch == quote {
				quote = 0
			}
			continue
		}
		switch ch {
		case '"', '\'':
			quote = ch
		case '>':
			return true
		}
	}
	return false
}

func activeTagOpenBeforeSlow(text string, offset int) int {
	inTag := false
	quote := byte(0)
	tagOpen := -1
	for i := 0; i < offset; i++ {
		ch := text[i]
		if !inTag {
			if ch == '<' && i+1 < len(text) && text[i+1] == '!' {
				if strings.HasPrefix(text[i:], "<!--") {
					end := strings.Index(text[i+4:], "-->")
					if end < 0 || i+4+end+3 > offset {
						return -1
					}
					i += 4 + end + 2
					continue
				}
				end := strings.IndexByte(text[i+2:], '>')
				if end < 0 || i+2+end >= offset {
					return -1
				}
				i += 2 + end
				continue
			}
			if ch == '<' {
				inTag = true
				tagOpen = i
			}
			continue
		}
		if quote != 0 {
			if ch == quote {
				quote = 0
			}
			continue
		}
		switch ch {
		case '"', '\'':
			quote = ch
		case '>':
			inTag = false
			tagOpen = -1
		case '<':
			tagOpen = i
		}
	}
	if inTag {
		return tagOpen
	}
	return -1
}

func activeTagOpenBeforeLocal(text string, offset int) int {
	quote := byte(0)
	for i := offset - 1; i >= 0; i-- {
		ch := text[i]
		if quote != 0 {
			if ch == quote {
				quote = 0
			}
			continue
		}
		switch ch {
		case '"', '\'':
			quote = ch
		case '>':
			return -1
		case '<':
			if i+1 < len(text) && text[i+1] == '!' {
				return -1
			}
			return i
		}
	}
	return -1
}

func isInsideIgnoredMarkupNear(text string, offset int) bool {
	for i := offset - 1; i >= 0; i-- {
		if text[i] == '>' {
			return false
		}
		if text[i] != '<' || i+1 >= offset {
			continue
		}
		if strings.HasPrefix(text[i:offset], "<!--") {
			return true
		}
		if text[i+1] == '!' {
			return true
		}
	}
	return false
}

func isInsideIgnoredMarkup(text string, offset int) bool {
	if offset < 0 {
		return false
	}
	prefix := text[:offset]
	if start := strings.LastIndex(prefix, "<!--"); start >= 0 {
		return !strings.Contains(text[start+4:offset], "-->")
	}
	if start := strings.LastIndex(prefix, "<!"); start >= 0 {
		return !strings.Contains(text[start+2:offset], ">")
	}
	return false
}

func attributeNameRangeAtCompletion(text string, tagContentStart, offset int) (int, int) {
	if offset > 0 && isSpace(text[offset-1]) {
		return offset, offset
	}
	start := scanNameStart(text, offset)
	if start <= tagContentStart {
		return start, scanNameEnd(text, offset)
	}
	if start > 0 && !isSpace(text[start-1]) {
		return -1, -1
	}
	return start, scanNameEnd(text, offset)
}

func activeTagCloseAfter(text string, tagOpen, offset int) int {
	quote := byte(0)
	for i := tagOpen + 1; i < len(text); i++ {
		ch := text[i]
		if quote != 0 {
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '"' || ch == '\'' {
			quote = ch
			continue
		}
		if ch == '>' && i >= offset {
			return i
		}
	}
	return len(text)
}

func containsScannerInvalidJSWhitespace(text string) bool {
	for i := 0; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])
		if isJSWhitespaceRune(r) && !isSpace(text[i]) {
			return true
		}
		i += size
	}
	return false
}

func isRawTextCompletionBlocked(htmlDocument *HTMLDocument, offset int) bool {
	if htmlDocument == nil {
		return false
	}
	for node := htmlDocument.findNodeBeforeByte(offset); node != nil; node = node.Parent {
		if node.startTagEndByte == nil || offset <= *node.startTagEndByte {
			continue
		}
		if node.endTagStartByte != nil && offset > *node.endTagStartByte {
			continue
		}
		switch strings.ToLower(node.Tag) {
		case "style":
			return true
		case "script":
			if node.Attributes == nil {
				return true
			}
			if typ := nodeAttributeValueCaseInsensitive(node, "type"); typ != nil && htmlScriptTypeValues[stringsTrimQuotes(*typ)] {
				return false
			}
			return true
		}
	}
	return false
}

func nodeAttributeValueCaseInsensitive(node *Node, name string) *string {
	if node == nil || node.Attributes == nil {
		return nil
	}
	for attrName, value := range node.Attributes {
		if strings.EqualFold(attrName, name) {
			return value
		}
	}
	return nil
}

func completeTag(dataManager *HTMLDataManager, document *TextDocument, offset int, position Position, ctx completionCtx, options *CompletionConfiguration, supportsMarkdown bool) CompletionList {
	var items []CompletionItem
	if position.Line == 0 && offset == ctx.rangeStart {
		item := completionWithEdit("!DOCTYPE", CompletionItemKindProperty, document, ctx.rangeStart, ctx.rangeEnd, "!DOCTYPE html>")
		item.Documentation = "A preamble for an HTML document."
		item.InsertTextFormat = InsertTextFormatPlainText
		items = append(items, item)
	}
	for _, provider := range completionDataProviders(dataManager, document.LanguageID, options) {
		for _, tag := range provider.ProvideTags() {
			name := tag.Name
			item := completionWithEdit(name, CompletionItemKindProperty, document, ctx.rangeStart, ctx.rangeEnd, name)
			item.InsertTextFormat = InsertTextFormatPlainText
			if doc := GenerateDocumentation(tag, HoverSettings{}, supportsMarkdown); doc != nil {
				item.Documentation = *doc
			}
			items = append(items, item)
		}
	}
	if !ctx.openTagOnly && (options == nil || options.HideEndTagSuggestions == nil || !*options.HideEndTagSuggestions) {
		if closeItem := closeTagCompletionItem(dataManager, document, offset, ctx, options); closeItem != nil {
			items = append(items, *closeItem)
		}
	}
	return CompletionList{Items: items}
}

func closeTagCompletionItem(dataManager *HTMLDataManager, document *TextDocument, offset int, ctx completionCtx, options *CompletionConfiguration) *CompletionItem {
	text := document.GetText()
	voidElements := dataManager.GetVoidElements(document.LanguageID)
	stack := openTagStackEntriesBefore(text, ctx.rangeStart-1, voidElements)
	if ctx.inOpenTag && len(stack) > 0 {
		stack = stack[:len(stack)-1]
	}
	if len(stack) == 0 {
		return nil
	}
	entry := stack[len(stack)-1]
	tag := entry.tag
	label := "/" + tag
	insert := "/" + tag
	if !hasFollowingEndTagClose(text, ctx.rangeEnd) {
		insert += ">"
	}
	editStart := ctx.rangeStart
	filterText := "/" + tag
	if lineStart, insertPrefix, filterPrefix, ok := indentedCloseTagLineStart(text, entry.start, ctx.rangeStart-1); ok {
		editStart = lineStart
		insert = insertPrefix + tag
		if !hasFollowingEndTagClose(text, ctx.rangeEnd) {
			insert += ">"
		}
		filterText = filterPrefix + tag
	}
	item := completionWithEdit(label, CompletionItemKindProperty, document, editStart, ctx.rangeEnd, insert)
	item.InsertTextFormat = InsertTextFormatPlainText
	item.FilterText = filterText
	return &item
}

func closeTagFilterText(text string, rangeStart int, openTagStart int, tag string) string {
	openBracket := rangeStart - 1
	if rangeStart >= 2 && text[rangeStart-2] == '<' && text[rangeStart-1] == '/' {
		openBracket = rangeStart - 2
	}
	if _, _, filterPrefix, ok := indentedCloseTagLineStart(text, openTagStart, openBracket); ok {
		return filterPrefix + tag
	}
	return "/" + tag
}

func completeAttributes(dataManager *HTMLDataManager, document *TextDocument, offset int, htmlDocument *HTMLDocument, ctx completionCtx, options *CompletionConfiguration, supportsMarkdown bool) CompletionList {
	if ctx.tag == "" {
		node := htmlDocument.findNodeAtByte(offset)
		if node != nil {
			ctx.tag = node.Tag
		}
	}
	existing := existingAttributesAt(document.GetText(), offset, ctx.tagOpen)
	currentName := document.GetText()[ctx.rangeStart:ctx.rangeEnd]
	editRange := NewRange(document.PositionAt(ctx.rangeStart), document.PositionAt(ctx.rangeEnd))
	hasExistingValue := attributeNameHasExistingValue(document.GetText(), ctx.rangeEnd)
	var items []CompletionItem
	seen := map[string]bool{}
	for _, provider := range completionDataProviders(dataManager, document.LanguageID, options) {
		if provider.GetID() == "html5" {
			for _, tmpl := range cachedAttributeCompletionTemplates(provider, ctx.tag, options, supportsMarkdown, hasExistingValue) {
				name := tmpl.Label
				if seen[name] || (existing[name] && name != currentName) {
					continue
				}
				seen[name] = true
				item := tmpl.itemWithRange(editRange)
				items = append(items, item)
			}
			continue
		}
		for _, attr := range provider.ProvideAttributes(ctx.tag) {
			name := attr.Name
			if seen[name] || (existing[name] && name != currentName) {
				continue
			}
			seen[name] = true
			newText := attributeInsertText(name, attr.ValueSet, options)
			if hasExistingValue {
				newText = name
			}
			kind := CompletionItemKindValue
			if attr.ValueSet == "handler" {
				kind = CompletionItemKindFunction
			}
			item := completionWithRange(name, kind, editRange, newText)
			item.InsertTextFormat = InsertTextFormatSnippet
			if newText != name && (attr.ValueSet != "" || name == "style") && attr.ValueSet != "v" {
				item.Command = &Command{Title: "Suggest", Command: "editor.action.triggerSuggest"}
			}
			if doc := GenerateDocumentation(attr, HoverSettings{}, supportsMarkdown); doc != nil {
				item.Documentation = *doc
			}
			items = append(items, item)
		}
	}
	if !seen["data-"] {
		item := completionWithRange("data-", CompletionItemKindValue, editRange, `data-$1="$2"`)
		item.InsertTextFormat = InsertTextFormatSnippet
		items = append(items, item)
		seen["data-"] = true
	}
	for _, name := range documentDataAttributes(htmlDocument) {
		if seen[name] || (existing[name] && name != currentName) {
			continue
		}
		seen[name] = true
		newText := name + `="$1"`
		item := completionWithRange(name, CompletionItemKindValue, editRange, newText)
		item.InsertTextFormat = InsertTextFormatSnippet
		items = append(items, item)
	}
	return CompletionList{Items: items}
}

type attributeCompletionTemplate struct {
	Label            string
	Kind             CompletionItemKind
	NewText          string
	InsertTextFormat InsertTextFormat
	Command          *Command
	Documentation    any
}

func (t attributeCompletionTemplate) itemWithRange(editRange Range) CompletionItem {
	item := completionWithRange(t.Label, t.Kind, editRange, t.NewText)
	item.InsertTextFormat = t.InsertTextFormat
	item.Command = t.Command
	item.Documentation = t.Documentation
	return item
}

var attributeCompletionTemplateCache sync.Map

func cachedAttributeCompletionTemplates(provider HTMLDataProvider, tag string, options *CompletionConfiguration, supportsMarkdown, hasExistingValue bool) []attributeCompletionTemplate {
	key := attributeCompletionTemplateCacheKey(provider.GetID(), tag, options, supportsMarkdown, hasExistingValue)
	if cached, ok := attributeCompletionTemplateCache.Load(key); ok {
		return cached.([]attributeCompletionTemplate)
	}
	attrs := provider.ProvideAttributes(tag)
	templates := make([]attributeCompletionTemplate, 0, len(attrs))
	for _, attr := range attrs {
		name := attr.Name
		newText := attributeInsertText(name, attr.ValueSet, options)
		if hasExistingValue {
			newText = name
		}
		kind := CompletionItemKindValue
		if attr.ValueSet == "handler" {
			kind = CompletionItemKindFunction
		}
		tmpl := attributeCompletionTemplate{
			Label:            name,
			Kind:             kind,
			NewText:          newText,
			InsertTextFormat: InsertTextFormatSnippet,
		}
		if newText != name && (attr.ValueSet != "" || name == "style") && attr.ValueSet != "v" {
			tmpl.Command = &Command{Title: "Suggest", Command: "editor.action.triggerSuggest"}
		}
		if doc := GenerateDocumentation(attr, HoverSettings{}, supportsMarkdown); doc != nil {
			tmpl.Documentation = *doc
		}
		templates = append(templates, tmpl)
	}
	attributeCompletionTemplateCache.Store(key, templates)
	return templates
}

func attributeCompletionTemplateCacheKey(providerID, tag string, options *CompletionConfiguration, supportsMarkdown, hasExistingValue bool) string {
	defaultValue := ""
	if options != nil {
		defaultValue = options.AttributeDefaultValue
	}
	return providerID + "\x00" + tag + "\x00" + defaultValue + "\x00" + boolCacheKey(supportsMarkdown) + boolCacheKey(hasExistingValue)
}

func boolCacheKey(value bool) string {
	if value {
		return "1"
	}
	return "0"
}

func completionDataProviders(dataManager *HTMLDataManager, languageID string, options *CompletionConfiguration) []HTMLDataProvider {
	providers := dataManager.GetDataProviders()
	filtered := providers[:0]
	for _, provider := range providers {
		id := provider.GetID()
		if !provider.IsApplicable(languageID) {
			continue
		}
		if options != nil {
			if options.HTML5 != nil && !*options.HTML5 && id == "html5" {
				continue
			}
			if options.Provider != nil {
				if enabled, ok := options.Provider[id]; ok && !enabled {
					continue
				}
			}
		}
		filtered = append(filtered, provider)
	}
	return filtered
}

func attributeNameHasExistingValue(text string, offset int) bool {
	for offset < len(text) && isSpace(text[offset]) {
		offset++
	}
	return offset < len(text) && text[offset] == '='
}

func attributeInsertText(name, valueSet string, options *CompletionConfiguration) string {
	if name == "data-" {
		return `data-$1="$2"`
	}
	if valueSet == "v" {
		return name
	}
	quote := `"`
	if options != nil {
		switch options.AttributeDefaultValue {
		case "singlequotes":
			quote = `'`
		case "empty":
			return name + "=$1"
		}
	}
	return name + "=" + quote + "$1" + quote
}

func documentDataAttributes(htmlDocument *HTMLDocument) []string {
	if htmlDocument == nil || len(htmlDocument.dataAttributeNames) == 0 {
		return nil
	}
	return htmlDocument.dataAttributeNames
}

func completeAttributeValues(dataManager *HTMLDataManager, fs FileSystemProvider, participants []CompletionParticipant, document *TextDocument, offset int, htmlDocument *HTMLDocument, documentContext DocumentContext, ctx completionCtx, options *CompletionConfiguration, supportsMarkdown bool) CompletionList {
	for _, participant := range participants {
		if participant, ok := participant.(HTMLAttributeValueCompletionParticipant); ok && participant != nil {
			valueEnd := minInt(offset, ctx.valueEnd)
			value := ""
			if valueEnd >= ctx.valueStart && !(ctx.quote != 0 && offset >= ctx.rawValueEnd) {
				value = document.GetText()[ctx.valueStart:valueEnd]
			}
			rangeStart := ctx.rawValueStart
			rangeEnd := ctx.rawValueEnd
			if rangeStart == 0 && rangeEnd == 0 {
				rangeStart = ctx.rangeStart
				rangeEnd = ctx.rangeEnd
			}
			participant.OnHTMLAttributeValue(HtmlAttributeValueContext{
				Document:   document,
				Position:   document.PositionAt(offset),
				Tag:        ctx.tag,
				Attribute:  ctx.attr,
				Value:      value,
				Range:      NewRange(document.PositionAt(rangeStart), document.PositionAt(rangeEnd)),
				Attributes: attributeValueMap(document.GetText(), offset, false),
			})
		}
	}
	var pathItems []CompletionItem
	pathIncomplete := false
	if dataManager.IsPathAttribute(ctx.tag, ctx.attr) && fs != nil {
		if readDirectory, ok := fs.(FileSystemReadDirectoryProvider); ok {
			pathItems, pathIncomplete = completePathValues(readDirectory, document, documentContext, offset, ctx)
		}
	}
	items := append([]CompletionItem(nil), pathItems...)
	for _, provider := range completionDataProviders(dataManager, document.LanguageID, options) {
		for _, value := range provider.ProvideValues(ctx.tag, ctx.attr) {
			insert := value.Name
			if ctx.quote == 0 {
				insert = `"` + insert + `"`
			}
			item := completionWithEdit(value.Name, CompletionItemKindUnit, document, ctx.rangeStart, ctx.rangeEnd, insert)
			item.FilterText = insert
			item.InsertTextFormat = InsertTextFormatPlainText
			if doc := GenerateDocumentation(value, HoverSettings{}, supportsMarkdown); doc != nil {
				item.Documentation = *doc
			}
			items = append(items, item)
		}
	}
	if entity, ok := entityContext(document.GetText(), offset); ok {
		items = append(items, completeEntities(document, offset, entity).Items...)
	}
	return CompletionList{IsIncomplete: pathIncomplete, Items: items}
}

func completeEndTag(dataManager *HTMLDataManager, document *TextDocument, htmlDocument *HTMLDocument, offset int, ctx completionCtx, options *CompletionConfiguration, supportsMarkdown bool) CompletionList {
	node := htmlDocument.findNodeBeforeByte(offset)
	for node != nil && (node.Tag == "" || node.endTagStartByte != nil || isVoidNode(node)) {
		node = node.Parent
	}
	if node == nil || node.Tag == "" {
		return completeFallbackEndTags(dataManager, document, ctx, options, supportsMarkdown)
	}
	label := "/" + node.Tag
	start, insert := endTagEdit(document.GetText(), ctx, node.Tag, node.startByte)
	if start == ctx.rangeStart && ctx.rangeStart > 0 && document.GetText()[ctx.rangeStart-1] == '/' {
		start--
		insert = "/" + insert
	}
	item := completionWithEdit(label, CompletionItemKindProperty, document, start, ctx.rangeEnd, insert)
	item.InsertTextFormat = InsertTextFormatPlainText
	item.FilterText = closeTagFilterText(document.GetText(), ctx.rangeStart, node.startByte, node.Tag)
	return CompletionList{Items: []CompletionItem{item}}
}

func completeFallbackEndTags(dataManager *HTMLDataManager, document *TextDocument, ctx completionCtx, options *CompletionConfiguration, supportsMarkdown bool) CompletionList {
	closeTag := ""
	if !hasFollowingEndTagClose(document.GetText(), ctx.rangeEnd) {
		closeTag = ">"
	}
	var items []CompletionItem
	for _, provider := range completionDataProviders(dataManager, document.LanguageID, options) {
		for _, tag := range provider.ProvideTags() {
			label := "/" + tag.Name
			start := ctx.rangeStart
			insert := label + closeTag
			if start > 0 && document.GetText()[start-1] == '/' {
				start--
			}
			item := completionWithEdit(label, CompletionItemKindProperty, document, start, ctx.rangeEnd, insert)
			item.FilterText = label + closeTag
			item.InsertTextFormat = InsertTextFormatPlainText
			if doc := GenerateDocumentation(tag, HoverSettings{}, supportsMarkdown); doc != nil {
				item.Documentation = *doc
			}
			items = append(items, item)
		}
	}
	return CompletionList{Items: items}
}

func endTagEdit(text string, ctx completionCtx, tag string, openTagStart int) (int, string) {
	insert := tag
	if !hasFollowingEndTagClose(text, ctx.rangeEnd) {
		insert += ">"
	}
	if lineStart, insertPrefix, _, ok := indentedCloseTagLineStart(text, openTagStart, ctx.rangeStart-2); ok {
		return lineStart, insertPrefix + insert
	}
	return ctx.rangeStart, insert
}

func hasFollowingEndTagClose(text string, offset int) bool {
	for offset < len(text) && isSpace(text[offset]) {
		offset++
	}
	return offset < len(text) && text[offset] == '>'
}

func indentedCloseTagLineStart(text string, openTagStart, closeTagOpen int) (int, string, string, bool) {
	startIndent, ok := lineIndentBeforeOffset(text, openTagStart)
	if !ok {
		return 0, "", "", false
	}
	endIndent, ok := lineIndentBeforeOffset(text, closeTagOpen)
	if !ok || startIndent == endIndent {
		return 0, "", "", false
	}
	return closeTagOpen - len(endIndent), startIndent + "</", endIndent + "</", true
}

func lineIndentBeforeOffset(text string, offset int) (string, bool) {
	if offset > len(text) {
		offset = len(text)
	}
	start := offset
	for start > 0 {
		ch := text[start-1]
		if ch == '\n' || ch == '\r' {
			return text[start:offset], true
		}
		if !isSpace(ch) {
			return "", false
		}
		start--
	}
	return text[:offset], true
}

func lineIndentAt(text string, offset int) string {
	if offset > len(text) {
		offset = len(text)
	}
	lineStart := lineStartBefore(text, offset)
	i := lineStart
	for i < len(text) && (text[i] == ' ' || text[i] == '\t') {
		i++
	}
	return text[lineStart:i]
}

func lineStartBefore(text string, offset int) int {
	if offset > len(text) {
		offset = len(text)
	}
	for i := offset - 1; i >= 0; i-- {
		if text[i] == '\n' || text[i] == '\r' {
			return i + 1
		}
	}
	return 0
}

func completeContent(dataManager *HTMLDataManager, participants []CompletionParticipant, document *TextDocument, htmlDocument *HTMLDocument, offset int, options *CompletionConfiguration) CompletionList {
	var items []CompletionItem
	node := htmlDocument.findNodeBeforeByte(offset)
	if node != nil && node.Tag != "" && node.startTagEndByte != nil && offset == *node.startTagEndByte {
		if options != nil && options.HideAutoCompleteProposals != nil && *options.HideAutoCompleteProposals {
			return CompletionList{}
		}
		if !isVoidNode(node) || isProviderFilteredVoidNode(dataManager, document, node, options) {
			label := "</" + node.Tag + ">"
			edit := completionWithEdit(label, CompletionItemKindProperty, document, offset, offset, "$0"+label)
			edit.FilterText = label
			edit.InsertTextFormat = InsertTextFormatSnippet
			items = append(items, edit)
		}
		return CompletionList{Items: items}
	}
	for _, participant := range participants {
		if participant, ok := participant.(HTMLContentCompletionParticipant); ok && participant != nil {
			participant.OnHTMLContent(HtmlContentContext{Document: document, Position: document.PositionAt(offset)})
		}
	}
	if entity, ok := entityContext(document.GetText(), offset); ok {
		items = append(items, completeEntities(document, offset, entity).Items...)
	}
	return CompletionList{Items: items}
}

func isVoidNode(node *Node) bool {
	return node.startTagEndByte != nil && node.endTagStartByte == nil && node.Closed
}

func isProviderFilteredVoidNode(dataManager *HTMLDataManager, document *TextDocument, node *Node, options *CompletionConfiguration) bool {
	if node.startTagEndByte == nil || *node.startTagEndByte >= 2 && document.GetText()[*node.startTagEndByte-2:*node.startTagEndByte] == "/>" {
		return false
	}
	voidElements := voidElementsForProviders(completionDataProviders(dataManager, document.LanguageID, options), document.LanguageID)
	return !dataManager.IsVoidElement(node.Tag, voidElements)
}

func completeEntities(document *TextDocument, offset int, ctx completionCtx) CompletionList {
	items := make([]CompletionItem, 0, len(htmlEntityCompletionNames))
	for _, entity := range htmlEntityCompletionNames {
		label := "&" + entity
		item := completionWithEdit(label, CompletionItemKindKeyword, document, ctx.rangeStart, ctx.rangeEnd, label)
		item.InsertTextFormat = InsertTextFormatPlainText
		if description, ok := htmlEntityCompletionDescription(strings.TrimSuffix(entity, ";")); ok {
			item.Documentation = description
		}
		items = append(items, item)
	}
	return CompletionList{Items: items}
}

func htmlEntityCompletionDescription(name string) (string, bool) {
	value, ok := htmlEntityValue(name)
	if !ok {
		return "", false
	}
	return "Character entity representing '" + value + "'", true
}

func DoQuoteComplete(document *TextDocument, position Position, htmlDocument *HTMLDocument, options *CompletionConfiguration) *string {
	offset := document.OffsetAt(position)
	text := document.GetText()
	if offset == 0 || offset > len(text) || text[offset-1] != '=' {
		return nil
	}
	if offset < len(text) && (text[offset] == '"' || text[offset] == '\'' || text[offset] == '=') {
		return nil
	}
	if options != nil && options.AttributeDefaultValue == "empty" {
		return nil
	}
	quote := `"`
	if options != nil && options.AttributeDefaultValue == "singlequotes" {
		quote = `'`
	}
	node := htmlDocument.findNodeBeforeByte(offset)
	if node == nil || node.Attributes == nil || node.startByte >= offset || (node.endTagStartByte != nil && *node.endTagStartByte <= offset) {
		return nil
	}
	scanner := newScannerAtByte(text, node.startByte, ScannerStateWithinContent, false)
	for token := scanner.Scan(); token != TokenTypeEOS && scanner.GetTokenByteEnd() <= offset; token = scanner.Scan() {
		if token == TokenTypeAttributeName && scanner.GetTokenByteEnd() == offset-1 {
			token = scanner.Scan()
			if token != TokenTypeDelimiterAssign {
				return nil
			}
			token = scanner.Scan()
			if token == TokenTypeUnknown || token == TokenTypeAttributeValue {
				return nil
			}
			result := quote + "$1" + quote
			return &result
		}
	}
	return nil
}

func DoTagComplete(document *TextDocument, position Position, htmlDocument *HTMLDocument) *string {
	offset := document.OffsetAt(position)
	text := document.GetText()
	if offset >= 2 && text[offset-2:offset] == "</" {
		node := htmlDocument.findNodeBeforeByte(offset)
		for node != nil && node.Closed && !(node.endTagStartByte != nil && *node.endTagStartByte > offset) {
			node = node.Parent
		}
		if node != nil && node.Tag != "" {
			scanner := newScannerAtByte(text, node.startByte, ScannerStateWithinContent, false)
			for token := scanner.Scan(); token != TokenTypeEOS && scanner.GetTokenByteEnd() <= offset; token = scanner.Scan() {
				if token == TokenTypeEndTagOpen && scanner.GetTokenByteEnd() == offset {
					tag := node.Tag
					if offset >= len(text) || text[offset] != '>' {
						tag += ">"
					}
					return &tag
				}
			}
		}
		return nil
	}
	node := htmlDocument.findNodeBeforeByte(offset)
	if node == nil || node.Tag == "" || node.endTagStartByte != nil || node.startTagEndByte == nil || offset != *node.startTagEndByte || isVoidNode(node) {
		return nil
	}
	scanner := newScannerAtByte(text, node.startByte, ScannerStateWithinContent, false)
	for token := scanner.Scan(); token != TokenTypeEOS && scanner.GetTokenByteEnd() <= offset; token = scanner.Scan() {
		if token == TokenTypeStartTagClose && scanner.GetTokenByteEnd() == offset {
			result := "$0</" + node.Tag + ">"
			return &result
		}
	}
	return nil
}

func completionWithEdit(label string, kind CompletionItemKind, document *TextDocument, start, end int, newText string) CompletionItem {
	return completionWithRange(label, kind, NewRange(document.PositionAt(start), document.PositionAt(end)), newText)
}

func completionWithRange(label string, kind CompletionItemKind, r Range, newText string) CompletionItem {
	edit := NewTextEdit(r, newText)
	return CompletionItem{Label: label, Kind: kind, TextEdit: &edit}
}

func existingAttributes(text string, offset int) map[string]bool {
	return existingAttributesAt(text, offset, activeTagOpenBefore(text, offset))
}

func existingAttributesAt(text string, offset int, lt int) map[string]bool {
	end := len(text)
	if lt >= 0 {
		end = activeTagCloseAfter(text, lt, offset)
	}
	result := map[string]bool{}
	if lt < 0 {
		return result
	}
	i := lt + 1
	if i < end && text[i] == '/' {
		i++
	}
	for i < end && isNameChar(text[i]) {
		i++
	}
	for i < end {
		for i < end && isSpace(text[i]) {
			i++
		}
		if i >= end || text[i] == '>' || text[i] == '/' {
			break
		}
		nameStart := i
		for i < end {
			size, ok := attributeNameCharAt(text, i)
			if !ok {
				break
			}
			i += size
		}
		if nameStart == i {
			i++
			continue
		}
		result[text[nameStart:i]] = true
		for i < end && isSpace(text[i]) {
			i++
		}
		if i >= end || text[i] != '=' {
			continue
		}
		i++
		for i < end && isSpace(text[i]) {
			i++
		}
		if i >= end {
			break
		}
		if text[i] == '"' || text[i] == '\'' {
			quote := text[i]
			i++
			for i < end && text[i] != quote {
				i++
			}
			if i < end {
				i++
			}
			continue
		}
		for i < end {
			r, size := utf8.DecodeRuneInString(text[i:])
			if isUnquotedAttributeValueStop(text, i, r) {
				break
			}
			i += size
		}
	}
	return result
}

func attributeValueMap(text string, offset int, trimQuotes bool) map[string]*string {
	lt := activeTagOpenBefore(text, offset)
	end := len(text)
	if lt >= 0 {
		end = activeTagCloseAfter(text, lt, offset)
	}
	result := map[string]*string{}
	if lt < 0 {
		return result
	}
	scanner := CreateScanner(text[lt:end])
	var lastAttribute string
	for token := scanner.Scan(); token != TokenTypeEOS; token = scanner.Scan() {
		switch token {
		case TokenTypeAttributeName:
			lastAttribute = scanner.GetTokenText()
			result[lastAttribute] = nil
		case TokenTypeAttributeValue:
			if lastAttribute == "" {
				continue
			}
			value := scanner.GetTokenText()
			if trimQuotes {
				value = stringsTrimQuotes(value)
			}
			copied := value
			result[lastAttribute] = &copied
			lastAttribute = ""
		case TokenTypeWhitespace, TokenTypeDelimiterAssign:
		default:
			if token != TokenTypeStartTagOpen && token != TokenTypeStartTag {
				lastAttribute = ""
			}
		}
	}
	return result
}

func completePathValues(fs FileSystemReadDirectoryProvider, document *TextDocument, documentContext DocumentContext, offset int, ctx completionCtx) ([]CompletionItem, bool) {
	text := document.GetText()
	valueRangeStart := ctx.rawValueStart
	valueRangeEnd := ctx.rawValueEnd
	if valueRangeStart == 0 && valueRangeEnd == 0 {
		valueRangeStart = ctx.valueStart
		valueRangeEnd = ctx.valueEnd
	}
	if valueRangeStart < 0 || valueRangeEnd > len(text) || valueRangeStart > valueRangeEnd {
		return nil, false
	}
	fullValue := stripPathQuotes(text[valueRangeStart:valueRangeEnd])
	if isRemotePath(fullValue) {
		return nil, false
	}
	valueBeforeCursor := ""
	valueBeforeCursorEnd := minInt(offset, ctx.valueEnd)
	if valueBeforeCursorEnd >= ctx.valueStart {
		valueBeforeCursor = text[ctx.valueStart:valueBeforeCursorEnd]
	}
	if isRemotePath(valueBeforeCursor) {
		return nil, false
	}
	if fullValue == "." || fullValue == ".." {
		return nil, true
	}
	if documentContext == nil {
		panic("documentContext is nil")
	}
	valueBeforeLastSlash := ""
	if slash := strings.LastIndex(valueBeforeCursor, "/"); slash >= 0 {
		valueBeforeLastSlash = valueBeforeCursor[:slash+1]
	}
	parentRef := valueBeforeLastSlash
	if parentRef == "" {
		parentRef = "."
	}
	baseURI, ok := documentContext.ResolveReference(parentRef, document.URI)
	if !ok || baseURI == "" {
		return nil, false
	}
	entries, err := fs.ReadDirectory(baseURI)
	if err != nil {
		return nil, false
	}
	hasPathQuotes := valueRangeStart < len(text) && (text[valueRangeStart] == '"' || text[valueRangeStart] == '\'')
	replaceStart, replaceEnd := pathReplaceOffsets(valueBeforeCursor, fullValue, valueRangeStart, valueRangeEnd, hasPathQuotes)
	extensionFilter := pathExtensionFilter(ctx.tag, ctx.attr, attributeValueMap(text, offset, true))
	var items []CompletionItem
	for _, entry := range entries {
		name, _ := entry[0].(string)
		if strings.HasPrefix(name, ".") {
			continue
		}
		fileType := completionEntryFileType(entry[1])
		label := name
		insert := name
		kind := CompletionItemKindFile
		if fileType == FileTypeDirectory {
			label += "/"
			insert += "/"
			kind = CompletionItemKindFolder
		}
		item := completionWithEdit(label, kind, document, replaceStart, replaceEnd, insert)
		if fileType == FileTypeDirectory {
			item.Command = &Command{Title: "Suggest", Command: "editor.action.triggerSuggest"}
		} else if extensionFilter != nil {
			if extensionFilter.matches(name) {
				item.SortText = "0_" + name
			} else if extensionFilter.exclusive {
				continue
			} else {
				item.SortText = "1_" + name
			}
		}
		items = append(items, item)
	}
	return items, false
}

func completionEntryFileType(value any) FileType {
	switch v := value.(type) {
	case FileType:
		return v
	case int:
		return FileType(v)
	case int8:
		return FileType(v)
	case int16:
		return FileType(v)
	case int32:
		return FileType(v)
	case int64:
		return FileType(v)
	case uint:
		return FileType(v)
	case uint8:
		return FileType(v)
	case uint16:
		return FileType(v)
	case uint32:
		return FileType(v)
	case uint64:
		return FileType(v)
	case float64:
		return FileType(v)
	default:
		return 0
	}
}

func stripPathQuotes(value string) string {
	if len(value) == 0 || (value[0] != '"' && value[0] != '\'') {
		return value
	}
	if len(value) <= 2 {
		return ""
	}
	return value[1 : len(value)-1]
}

func pathReplaceOffsets(valueBeforeCursor, fullValue string, valueRangeStart, valueRangeEnd int, hasPathQuotes bool) (int, int) {
	lastSlash := strings.LastIndex(valueBeforeCursor, "/")
	if lastSlash == -1 {
		return valueRangeStart + 1, valueRangeEnd - 1
	}
	valueAfterLastSlash := ""
	if lastSlash+1 < len(fullValue) {
		valueAfterLastSlash = fullValue[lastSlash+1:]
	}
	valueEnd := valueRangeEnd - 1
	start := valueEnd - len(valueAfterLastSlash)
	end := valueEnd
	if whitespace := strings.Index(valueAfterLastSlash, " "); whitespace >= 0 {
		end = start + whitespace
	}
	return start, end
}

type pathExtensionPriority struct {
	extensions map[string]bool
	exclusive  bool
}

func (p *pathExtensionPriority) matches(name string) bool {
	return p.extensions[strings.ToLower(path.Ext(name))]
}

func pathExtensionFilter(tag, attr string, attributes map[string]*string) *pathExtensionPriority {
	tag = strings.ToLower(tag)
	attr = strings.ToLower(attr)
	if tag == "link" && attr == "href" {
		rel := normalizedAttributeValue(attributes["rel"])
		switch rel {
		case "stylesheet":
			return newPathExtensionPriority(false, ".css", ".scss", ".sass", ".less")
		case "icon", "apple-touch-icon":
			return newPathExtensionPriority(false, ".ico", ".png", ".svg", ".jpg", ".jpeg", ".gif", ".webp")
		}
	}
	if tag == "script" && attr == "src" {
		return newPathExtensionPriority(false, ".js", ".mjs", ".cjs", ".ts", ".tsx", ".jsx")
	}
	if tag == "img" && attr == "src" {
		return newPathExtensionPriority(false, ".png", ".jpg", ".jpeg", ".gif", ".svg", ".webp", ".bmp", ".ico")
	}
	if tag == "video" && attr == "src" {
		return newPathExtensionPriority(false, ".mp4", ".webm", ".ogg", ".mov", ".avi")
	}
	if tag == "audio" && attr == "src" {
		return newPathExtensionPriority(false, ".mp3", ".wav", ".ogg", ".m4a", ".aac", ".flac")
	}
	return nil
}

func newPathExtensionPriority(exclusive bool, extensions ...string) *pathExtensionPriority {
	result := &pathExtensionPriority{extensions: map[string]bool{}, exclusive: exclusive}
	for _, extension := range extensions {
		result.extensions[extension] = true
	}
	return result
}

func normalizedAttributeValue(value *string) string {
	if value == nil {
		return ""
	}
	return stringsTrimQuotes(*value)
}

func isRemotePath(value string) bool {
	return strings.HasPrefix(value, "http") ||
		strings.HasPrefix(value, "//")
}

func scanNameStart(text string, offset int) int {
	i := offset
	for i > 0 {
		r, size := utf8.DecodeLastRuneInString(text[:i])
		if r == utf8.RuneError && size == 0 || !isAttributeNameRune(r) {
			break
		}
		i -= size
	}
	return i
}

func scanNameEnd(text string, offset int) int {
	i := offset
	for i < len(text) {
		size, ok := attributeNameCharAt(text, i)
		if !ok {
			break
		}
		i += size
	}
	return i
}

func isNameChar(ch byte) bool {
	return ('a' <= ch && ch <= 'z') || ('A' <= ch && ch <= 'Z') || ('0' <= ch && ch <= '9') || ch == '_' || ch == ':' || ch == '-' || ch == '.'
}

func isElementNameStartChar(ch byte) bool {
	return isLetterOrDigit(ch) || ch == '_' || ch == ':'
}

func isLetterOrDigit(ch byte) bool {
	return ('a' <= ch && ch <= 'z') || ('A' <= ch && ch <= 'Z') || ('0' <= ch && ch <= '9')
}

func attributeNameCharAt(text string, offset int) (int, bool) {
	r, size := utf8.DecodeRuneInString(text[offset:])
	if r == utf8.RuneError && size == 0 {
		return 0, false
	}
	return size, isAttributeNameRune(r)
}

func isAttributeNameRune(r rune) bool {
	return !isJSWhitespaceRune(r) && r != '"' && r != '\'' && r != '>' && r != '<' && r != '=' && r != '/' && r != 0 && (r > 0x0f || r < 0) && r != 0x7f && (r < 0x80 || r > 0x9f)
}

func isUnquotedAttributeValueStop(text string, offset int, r rune) bool {
	if isJSWhitespaceRune(r) {
		return true
	}
	switch r {
	case '"', '\'', '`', '=', '<', '>':
		return true
	case '/':
		return offset+1 < len(text) && text[offset+1] == '>'
	default:
		return false
	}
}

func isSpace(ch byte) bool {
	return ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' || ch == '\f'
}

func hasWhitespace(s string) bool {
	for i := 0; i < len(s); i++ {
		if isSpace(s[i]) {
			return true
		}
	}
	return false
}

func lastJSWhitespaceEnd(s string) int {
	last := -1
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if isJSWhitespaceRune(r) {
			last = i + size
		}
		i += size
	}
	return last
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func isAfterSelfCloseSlash(text string, start, offset int) bool {
	var quote byte
	for i := start; i < offset; i++ {
		if quote != 0 {
			if text[i] == quote {
				quote = 0
			}
			continue
		}
		if text[i] == '"' || text[i] == '\'' {
			quote = text[i]
		}
	}
	if quote != 0 {
		return false
	}
	return offset > start && text[offset-1] == '/'
}

type tagStackEntry struct {
	tag   string
	start int
}

func openTagStackBefore(text string, limit int) []string {
	entries := openTagStackEntriesBefore(text, limit, defaultVoidTagNames())
	stack := make([]string, len(entries))
	for i, entry := range entries {
		stack[i] = entry.tag
	}
	return stack
}

func defaultVoidTagNames() []string {
	return []string{"area", "base", "br", "col", "embed", "hr", "img", "input", "link", "meta", "param", "source", "track", "wbr"}
}

func openTagStackEntriesBefore(text string, limit int, voidElements []string) []tagStackEntry {
	scanner := CreateScanner(text)
	var stack []tagStackEntry
	var lastStartTag tagStackEntry
	for token := scanner.Scan(); token != TokenTypeEOS; token = scanner.Scan() {
		if scanner.GetTokenByteOffset() >= limit {
			break
		}
		switch token {
		case TokenTypeStartTag:
			tag := scanner.GetTokenText()
			start := scanner.GetTokenByteOffset()
			if start > 0 && text[start-1] == '<' {
				start--
			}
			lastStartTag = tagStackEntry{tag: tag, start: start}
			if !isVoidTagName(tag, voidElements) {
				stack = append(stack, lastStartTag)
			}
		case TokenTypeStartTagSelfClose:
			if lastStartTag.tag != "" && len(stack) > 0 && strings.EqualFold(stack[len(stack)-1].tag, lastStartTag.tag) {
				stack = stack[:len(stack)-1]
			}
			lastStartTag = tagStackEntry{}
		case TokenTypeStartTagClose:
			lastStartTag = tagStackEntry{}
		case TokenTypeEndTag:
			name := scanner.GetTokenText()
			for i := len(stack) - 1; i >= 0; i-- {
				if strings.EqualFold(stack[i].tag, name) {
					stack = stack[:i]
					break
				}
			}
		}
	}
	return stack
}

func isVoidTagName(name string, voidElements []string) bool {
	name = strings.ToLower(name)
	i := sort.SearchStrings(voidElements, name)
	return i < len(voidElements) && voidElements[i] == name
}
