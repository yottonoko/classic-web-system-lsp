package lspserver

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

type inlineStyleTarget struct {
	TagStart        int
	TagEnd          int
	StyleAttrStart  int
	StyleAttrEnd    int
	StyleValueStart int
	StyleValueEnd   int
}

var (
	inlineStyleAttrPattern  = regexp.MustCompile(`(?is)\sstyle\s*=\s*("([^"]*)"|'([^']*)')`)
	inlineClassAttrPattern  = regexp.MustCompile(`(?is)\sclass\s*=\s*("([^"]*)"|'([^']*)')`)
	inlineIDAttrPattern     = regexp.MustCompile(`(?is)\sid\s*=\s*("([^"]*)"|'([^']*)')`)
	inlineStyleElementRegex = regexp.MustCompile(`(?is)<style\b[^>]*>(.*?)</style\s*>`)
)

func (s *Server) inlineStyleExtractionCodeActions(params codeActionParams, offset int) []lsp.CodeAction {
	doc, _ := s.parsed(params.TextDocument.URI)
	if doc == nil {
		return nil
	}
	target, ok := inlineStyleTargetAt(doc.Text, offset)
	if !ok {
		return nil
	}
	className := nextInlineStyleName(doc.Text)
	idName, hasID := inlineStyleAttributeValue(doc.Text[target.TagStart:target.TagEnd+1], inlineIDAttrPattern)
	if idName == "" {
		idName = nextInlineStyleName(doc.Text)
	}
	styleValue := doc.Text[target.StyleValueStart:target.StyleValueEnd]
	fullRange := doc.Range(0, len(doc.Text))
	return []lsp.CodeAction{
		{
			Title: "Extract inline style to class",
			Kind:  "refactor.extract",
			Edit: &lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{
				params.TextDocument.URI: {{
					Range:   fullRange,
					NewText: extractInlineStyle(doc.Text, target, "."+className, styleValue, s.settings.StyleExtractionInsertionMode, inlineStyleClassTagEdit(className)),
				}},
			}},
		},
		{
			Title: "Extract inline style to ID",
			Kind:  "refactor.extract",
			Edit: &lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{
				params.TextDocument.URI: {{
					Range: fullRange,
					NewText: extractInlineStyle(doc.Text, target, "#"+idName, styleValue, s.settings.StyleExtractionInsertionMode, func(tag string) string {
						if hasID {
							return removeInlineStyleAttribute(tag)
						}
						return insertHTMLAttribute(removeInlineStyleAttribute(tag), `id="`+idName+`"`)
					}),
				}},
			}},
		},
	}
}

func inlineStyleTargetAt(text string, offset int) (inlineStyleTarget, bool) {
	if offset < 0 {
		offset = 0
	}
	if offset > len(text) {
		offset = len(text)
	}
	tagStart := strings.LastIndex(text[:offset], "<")
	if tagStart < 0 || tagStart+1 >= len(text) {
		return inlineStyleTarget{}, false
	}
	switch text[tagStart+1] {
	case '/', '!', '?':
		return inlineStyleTarget{}, false
	}
	closeRel := strings.IndexByte(text[tagStart:], '>')
	if closeRel < 0 {
		return inlineStyleTarget{}, false
	}
	tagEnd := tagStart + closeRel
	if offset > tagEnd {
		return inlineStyleTarget{}, false
	}
	tag := text[tagStart : tagEnd+1]
	match := inlineStyleAttrPattern.FindStringSubmatchIndex(tag)
	if match == nil {
		return inlineStyleTarget{}, false
	}
	valueStart, valueEnd := -1, -1
	if match[4] >= 0 {
		valueStart, valueEnd = match[4], match[5]
	} else if match[6] >= 0 {
		valueStart, valueEnd = match[6], match[7]
	}
	if valueStart < 0 || valueStart == valueEnd {
		return inlineStyleTarget{}, false
	}
	value := tag[valueStart:valueEnd]
	if strings.Contains(value, "<%") || strings.Contains(value, "%>") {
		return inlineStyleTarget{}, false
	}
	return inlineStyleTarget{
		TagStart:        tagStart,
		TagEnd:          tagEnd,
		StyleAttrStart:  tagStart + match[0],
		StyleAttrEnd:    tagStart + match[1],
		StyleValueStart: tagStart + valueStart,
		StyleValueEnd:   tagStart + valueEnd,
	}, true
}

func extractInlineStyle(text string, target inlineStyleTarget, selector string, styleValue string, insertionMode string, editTag func(string) string) string {
	rule := inlineStyleRule(selector, styleValue)
	tag := text[target.TagStart : target.TagEnd+1]
	newTag := editTag(tag)
	if insertionMode == "reuseExistingStyleTag" {
		if insertion, ok := nearestStyleElementInsertion(text, target.TagStart); ok {
			withTag := text[:target.TagStart] + newTag + text[target.TagEnd+1:]
			if insertion > target.TagEnd {
				insertion += len(newTag) - (target.TagEnd + 1 - target.TagStart)
			}
			return withTag[:insertion] + rule + "\n" + withTag[insertion:]
		}
	}
	styleBlock := "<style>\n" + rule + "\n</style>\n"
	return text[:target.TagStart] + styleBlock + newTag + text[target.TagEnd+1:]
}

func inlineStyleClassTagEdit(className string) func(string) string {
	return func(tag string) string {
		tag = removeInlineStyleAttribute(tag)
		match := inlineClassAttrPattern.FindStringSubmatchIndex(tag)
		if match == nil {
			return insertHTMLAttribute(tag, `class="`+className+`"`)
		}
		valueStart, valueEnd := quotedAttributeValueSpan(match)
		if valueStart < 0 {
			return tag
		}
		value := strings.TrimSpace(tag[valueStart:valueEnd])
		if value != "" {
			value += " "
		}
		value += className
		return tag[:valueStart] + value + tag[valueEnd:]
	}
}

func removeInlineStyleAttribute(tag string) string {
	match := inlineStyleAttrPattern.FindStringIndex(tag)
	if match == nil {
		return tag
	}
	return tag[:match[0]] + tag[match[1]:]
}

func insertHTMLAttribute(tag string, attr string) string {
	insert := strings.LastIndexByte(tag, '>')
	if insert < 0 {
		return tag
	}
	if insert > 0 && tag[insert-1] == '/' {
		insert--
	}
	return tag[:insert] + " " + attr + tag[insert:]
}

func inlineStyleAttributeValue(tag string, pattern *regexp.Regexp) (string, bool) {
	match := pattern.FindStringSubmatchIndex(tag)
	if match == nil {
		return "", false
	}
	start, end := quotedAttributeValueSpan(match)
	if start < 0 {
		return "", false
	}
	return tag[start:end], true
}

func quotedAttributeValueSpan(match []int) (int, int) {
	if len(match) >= 6 && match[4] >= 0 {
		return match[4], match[5]
	}
	if len(match) >= 8 && match[6] >= 0 {
		return match[6], match[7]
	}
	return -1, -1
}

func nextInlineStyleName(text string) string {
	for i := 1; ; i++ {
		name := "style-" + strconv.Itoa(i)
		if !strings.Contains(text, name) {
			return name
		}
	}
}

func inlineStyleRule(selector string, styleValue string) string {
	lines := []string{"  " + selector + " {"}
	for _, declaration := range splitInlineStyleDeclarations(styleValue) {
		property, value, ok := strings.Cut(declaration, ":")
		if !ok {
			continue
		}
		property = strings.TrimSpace(property)
		value = strings.TrimSpace(value)
		if property == "" || value == "" {
			continue
		}
		lines = append(lines, "    "+property+": "+value+";")
	}
	lines = append(lines, "  }")
	return strings.Join(lines, "\n")
}

func splitInlineStyleDeclarations(value string) []string {
	var declarations []string
	start := 0
	depth := 0
	quote := byte(0)
	for i := 0; i < len(value); i++ {
		ch := value[i]
		if quote != 0 {
			if ch == quote && (i == 0 || value[i-1] != '\\') {
				quote = 0
			}
			continue
		}
		switch ch {
		case '"', '\'':
			quote = ch
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ';':
			if depth == 0 {
				if part := strings.TrimSpace(value[start:i]); part != "" {
					declarations = append(declarations, part)
				}
				start = i + 1
			}
		}
	}
	if part := strings.TrimSpace(value[start:]); part != "" {
		declarations = append(declarations, part)
	}
	return declarations
}

func nearestStyleElementInsertion(text string, target int) (int, bool) {
	matches := inlineStyleElementRegex.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return 0, false
	}
	best := matches[0]
	bestDistance := absInt(best[3] - target)
	for _, match := range matches[1:] {
		distance := absInt(match[3] - target)
		if distance < bestDistance {
			best = match
			bestDistance = distance
		}
	}
	return best[3], true
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
