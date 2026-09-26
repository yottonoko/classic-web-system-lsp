package htmlservice

import (
	"strconv"
	"strings"
	"unicode/utf16"
)

func DoHover(dataManager *HTMLDataManager, caps *ClientCapabilities, document *TextDocument, position Position, htmlDocument *HTMLDocument, options *HoverSettings) *Hover {
	settings := HoverSettings{}
	if options != nil {
		settings = *options
	}
	supports := supportsHoverMarkdown(caps)
	offset := document.OffsetAt(position)
	node := htmlDocument.findNodeAtByte(offset)
	if node == nil || node.Tag == "" {
		return nil
	}
	if hover := tagNameHover(dataManager, document, node, offset, settings, supports); hover != nil {
		return hover
	}
	scanner := CreateScanner(document.GetText())
	var lastTag string
	var lastAttribute string
	for token := scanner.Scan(); token != TokenTypeEOS; token = scanner.Scan() {
		coversToken := scanner.GetTokenByteOffset() <= offset && offset <= scanner.GetTokenByteEnd()
		switch token {
		case TokenTypeStartTag, TokenTypeEndTag:
			if token == TokenTypeStartTag {
				lastTag = scanner.GetTokenText()
			}
			if !coversToken {
				continue
			}
			if token == TokenTypeEndTag && (node.endTagStartByte == nil || scanner.GetTokenByteOffset() != *node.endTagStartByte+2) {
				return nil
			}
			name := strings.ToLower(scanner.GetTokenText())
			if token == TokenTypeEndTag {
				name = strings.ToLower(node.Tag)
			}
			if tag, ok := findTagData(dataManager, document.LanguageID, name); ok {
				if doc := GenerateDocumentation(tag, settings, supports); doc != nil {
					r := NewRange(document.PositionAt(scanner.GetTokenByteOffset()), document.PositionAt(scanner.GetTokenByteEnd()))
					return &Hover{Contents: *doc, Range: &r}
				}
				kind := MarkupKindPlainText
				if supports {
					kind = MarkupKindMarkdown
				}
				r := NewRange(document.PositionAt(scanner.GetTokenByteOffset()), document.PositionAt(scanner.GetTokenByteEnd()))
				return &Hover{Contents: MarkupContent{Kind: kind}, Range: &r}
			}
			return nil
		case TokenTypeAttributeName:
			lastAttribute = scanner.GetTokenText()
			if !coversToken {
				continue
			}
			node := htmlDocument.findNodeAtByte(offset)
			tagName := lastTag
			if node != nil && node.Tag != "" {
				tagName = node.Tag
			}
			attrName := scanner.GetTokenText()
			for _, provider := range dataManager.GetDataProviders() {
				if !provider.IsApplicable(document.LanguageID) {
					continue
				}
				for _, attr := range provider.ProvideAttributes(tagName) {
					if attr.Name == attrName && descriptionTruthy(attr.Description) {
						if doc := GenerateDocumentation(attr, settings, supports); doc != nil {
							r := NewRange(document.PositionAt(scanner.GetTokenByteOffset()), document.PositionAt(scanner.GetTokenByteEnd()))
							return &Hover{Contents: *doc, Range: &r}
						}
					}
				}
			}
			return nil
		case TokenTypeAttributeValue:
			if !coversToken {
				continue
			}
			if hover := entityHover(document, offset); hover != nil {
				return hover
			}
			node := htmlDocument.findNodeAtByte(offset)
			tagName := lastTag
			if node != nil && node.Tag != "" {
				tagName = node.Tag
			}
			valueName := stringsTrimHoverQuotes(scanner.GetTokenText())
			for _, provider := range dataManager.GetDataProviders() {
				if !provider.IsApplicable(document.LanguageID) {
					continue
				}
				for _, value := range provider.ProvideValues(tagName, lastAttribute) {
					if value.Name == valueName && descriptionTruthy(value.Description) {
						if doc := GenerateDocumentation(value, settings, supports); doc != nil {
							r := NewRange(document.PositionAt(scanner.GetTokenByteOffset()), document.PositionAt(scanner.GetTokenByteEnd()))
							return &Hover{Contents: *doc, Range: &r}
						}
					}
				}
			}
			return nil
		}
	}
	return entityHover(document, offset)
}

func tagNameHover(dataManager *HTMLDataManager, document *TextDocument, node *Node, offset int, settings HoverSettings, supports bool) *Hover {
	start := -1
	end := -1
	if node.startByte+1 <= offset && offset <= node.startByte+1+len(node.Tag) {
		start = node.startByte + 1
		end = node.startByte + 1 + len(node.Tag)
	} else if node.endTagStartByte != nil && *node.endTagStartByte+2 <= offset && offset <= *node.endTagStartByte+2+len(node.Tag) {
		start = *node.endTagStartByte + 2
		end = *node.endTagStartByte + 2 + len(node.Tag)
	}
	if start < 0 {
		return nil
	}
	if tag, ok := findTagData(dataManager, document.LanguageID, node.Tag); ok {
		r := NewRange(document.PositionAt(start), document.PositionAt(end))
		if doc := GenerateDocumentation(tag, settings, supports); doc != nil {
			return &Hover{Contents: *doc, Range: &r}
		}
		kind := MarkupKindPlainText
		if supports {
			kind = MarkupKindMarkdown
		}
		return &Hover{Contents: MarkupContent{Kind: kind}, Range: &r}
	}
	return nil
}

func findTagData(dataManager *HTMLDataManager, languageID, name string) (TagData, bool) {
	lowerName := strings.ToLower(name)
	for _, provider := range dataManager.GetDataProviders() {
		if !provider.IsApplicable(languageID) {
			continue
		}
		if static, ok := provider.(*staticHTMLDataProvider); ok {
			tag, ok := static.tagMap[lowerName]
			if ok {
				return tag, true
			}
			continue
		}
		for _, tag := range provider.ProvideTags() {
			if strings.EqualFold(tag.Name, name) {
				return tag, true
			}
		}
	}
	return TagData{}, false
}

func stringsTrimHoverQuotes(value string) string {
	if len(value) > 0 && (value[0] == '"' || value[0] == '\'') {
		value = value[1:]
	}
	if len(value) > 0 {
		last := value[len(value)-1]
		if last == '"' || last == '\'' {
			value = value[:len(value)-1]
		}
	}
	return value
}

func descriptionTruthy(description any) bool {
	if description == nil {
		return false
	}
	if text, ok := description.(string); ok {
		return text != ""
	}
	return true
}

func entityHover(document *TextDocument, offset int) *Hover {
	text := document.GetText()
	if offset <= 0 || offset > len(text) {
		return nil
	}
	amp := strings.LastIndex(text[:offset], "&")
	if amp < 0 || offset == amp {
		return nil
	}
	end := amp + 1
	for end < len(text) && isEntityNameChar(text[end]) {
		end++
	}
	hasSemicolon := end < len(text) && text[end] == ';'
	rangeEnd := end
	if hasSemicolon {
		if offset > end {
			return nil
		}
		rangeEnd++
	}
	if offset > rangeEnd {
		return nil
	}
	entityName := strings.TrimSuffix(text[amp+1:rangeEnd], ";")
	description, ok := htmlEntityDescription(entityName)
	if !ok {
		return nil
	}
	r := NewRange(document.PositionAt(amp+1), document.PositionAt(rangeEnd))
	return &Hover{Contents: description, Range: &r}
}

func isEntityNameChar(ch byte) bool {
	return ('a' <= ch && ch <= 'z') || ('A' <= ch && ch <= 'Z') || ('0' <= ch && ch <= '9') || ch == '#'
}

func htmlEntityDescription(name string) (string, bool) {
	value, ok := htmlEntityValue(name)
	if !ok {
		return "", false
	}
	return "Character entity representing '" + value + "', unicode equivalent '" + unicodeCodePoint(value) + "'", true
}

func htmlEntityValue(name string) (string, bool) {
	value, ok := htmlEntities[name+";"]
	if !ok {
		value, ok = htmlEntities[name]
	}
	return value, ok && value != ""
}

func unicodeCodePoint(value string) string {
	codeUnit := 0
	if runes := []rune(value); len(runes) > 0 {
		codeUnit = int(utf16.Encode(runes[:1])[0])
	}
	code := strings.ToUpper(strconv.FormatInt(int64(codeUnit), 16))
	for len(code) < 4 {
		code = "0" + code
	}
	return "U+" + code
}

func supportsHoverMarkdown(caps *ClientCapabilities) bool {
	if caps == nil {
		return true
	}
	if caps.TextDocument != nil && caps.TextDocument.Hover != nil {
		for _, kind := range caps.TextDocument.Hover.ContentFormat {
			if kind == MarkupKindMarkdown {
				return true
			}
		}
	}
	return false
}

func supportsCompletionMarkdown(caps *ClientCapabilities) bool {
	if caps == nil {
		return true
	}
	if caps.TextDocument != nil && caps.TextDocument.Completion != nil && caps.TextDocument.Completion.CompletionItem != nil {
		for _, kind := range caps.TextDocument.Completion.CompletionItem.DocumentationFormat {
			if kind == MarkupKindMarkdown {
				return true
			}
		}
	}
	return false
}
