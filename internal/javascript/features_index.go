package javascript

import (
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/tsgoadapter"
)

func BuildIndex(parsed *core.ParsedDocument) Index {
	source := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	index := Index{Declarations: map[string]Symbol{}, Occurrences: map[string][]Occurrence{}}
	for _, language := range []core.EmbeddedLanguage{core.LanguageJavaScript, core.LanguageJScript} {
		virtual := core.BuildVirtualDocument(parsed, language)
		if virtual.Text == "" {
			continue
		}
		analysis := tsgoadapter.AnalyzeJavaScript(virtual.Text)
		for i, identifier := range analysis.Identifiers {
			start, ok := virtual.ToSourceOffset(identifier.Start)
			if !ok {
				continue
			}
			end, ok := virtual.ToSourceOffset(identifier.End)
			if !ok {
				continue
			}
			occurrence := Occurrence{Name: identifier.Text, Range: source.Range(start, end)}
			lower := strings.ToLower(identifier.Text)
			index.Occurrences[lower] = append(index.Occurrences[lower], occurrence)
			if kind := declarationKind(virtual.Text, analysis.Identifiers, i); kind != "" {
				if _, exists := index.Declarations[lower]; !exists {
					index.Declarations[lower] = Symbol{Name: identifier.Text, Kind: kind, Range: occurrence.Range}
				}
			}
		}
	}
	return index
}

func identifierAt(parsed *core.ParsedDocument, position lsp.Position) (string, bool) {
	_, occurrence, ok := occurrenceAt(parsed, position)
	if !ok {
		return "", false
	}
	return occurrence.Name, true
}

func occurrenceAt(parsed *core.ParsedDocument, position lsp.Position) (core.VirtualDocument, Occurrence, bool) {
	virtual, ok := virtualForPosition(parsed, position)
	if !ok {
		return core.VirtualDocument{}, Occurrence{}, false
	}
	sourceDoc := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	virtualPosition, ok := virtual.ToVirtualPosition(position, sourceDoc)
	if !ok {
		return core.VirtualDocument{}, Occurrence{}, false
	}
	virtualDoc := core.NewTextDocument(virtual.URI, virtual.LanguageID, 0, virtual.Text)
	virtualOffset := virtualDoc.OffsetAt(virtualPosition)
	analysis := tsgoadapter.AnalyzeJavaScript(virtual.Text)
	for _, identifier := range analysis.Identifiers {
		if virtualOffset < identifier.Start || virtualOffset > identifier.End {
			continue
		}
		start, ok := virtual.ToSourceOffset(identifier.Start)
		if !ok {
			continue
		}
		end, ok := virtual.ToSourceOffset(identifier.End)
		if !ok {
			continue
		}
		return virtual, Occurrence{Name: identifier.Text, Range: sourceDoc.Range(start, end)}, true
	}
	return core.VirtualDocument{}, Occurrence{}, false
}

func virtualForPosition(parsed *core.ParsedDocument, position lsp.Position) (core.VirtualDocument, bool) {
	sourceDoc := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	offset := sourceDoc.OffsetAt(position)
	region := core.RegionAt(parsed, offset)
	if region == nil {
		return core.VirtualDocument{}, false
	}
	switch region.Language {
	case core.LanguageJavaScript:
		return core.BuildVirtualDocument(parsed, core.LanguageJavaScript), true
	case core.LanguageJScript:
		return core.BuildVirtualDocument(parsed, core.LanguageJScript), true
	default:
		return core.VirtualDocument{}, false
	}
}

func declarationKind(text string, identifiers []tsgoadapter.Identifier, index int) string {
	previous := previousWord(text, identifiers[index].Start)
	switch strings.ToLower(previous) {
	case "function":
		return "function"
	case "const", "let", "var":
		return "variable"
	case "class":
		return "class"
	default:
		return ""
	}
}

func previousWord(text string, offset int) string {
	i := offset
	for i > 0 && isSpace(text[i-1]) {
		i--
	}
	end := i
	for i > 0 && isIdent(text[i-1]) {
		i--
	}
	return text[i:end]
}

func signatureLabel(parsed *core.ParsedDocument, name string) string {
	for _, language := range []core.EmbeddedLanguage{core.LanguageJavaScript, core.LanguageJScript} {
		virtual := core.BuildVirtualDocument(parsed, language)
		for _, match := range functionPattern.FindAllStringSubmatch(virtual.Text, -1) {
			if match[1] == name {
				return name + "(" + strings.TrimSpace(match[2]) + ")"
			}
		}
	}
	return name
}

func parameterInformation(label string) []lsp.ParameterInformation {
	start := strings.Index(label, "(")
	end := strings.LastIndex(label, ")")
	if start < 0 || end <= start+1 {
		return nil
	}
	parts := strings.Split(label[start+1:end], ",")
	params := make([]lsp.ParameterInformation, 0, len(parts))
	for _, part := range parts {
		name := strings.TrimSpace(part)
		if name != "" {
			params = append(params, lsp.ParameterInformation{Label: name})
		}
	}
	return params
}

func callOpenParenBefore(text string, offset int) int {
	if offset > len(text) {
		offset = len(text)
	}
	depth := 0
	inString := byte(0)
	for i := offset - 1; i >= 0; i-- {
		ch := text[i]
		if inString != 0 {
			if ch == inString && (i == 0 || text[i-1] != '\\') {
				inString = 0
			}
			continue
		}
		switch ch {
		case '"', '\'', '`':
			inString = ch
		case ')':
			depth++
		case '(':
			if depth == 0 {
				return i
			}
			depth--
		case '\n', '\r':
			if depth == 0 {
				return -1
			}
		}
	}
	return -1
}

func identifierBefore(text string, offset int) string {
	i := offset
	for i > 0 && isSpace(text[i-1]) {
		i--
	}
	end := i
	for i > 0 && isIdent(text[i-1]) {
		i--
	}
	if i == end {
		return ""
	}
	return text[i:end]
}

func isValidIdentifier(value string) bool {
	if value == "" {
		return false
	}
	if !(value[0] == '$' || value[0] == '_' || value[0] >= 'A' && value[0] <= 'Z' || value[0] >= 'a' && value[0] <= 'z') {
		return false
	}
	for i := 1; i < len(value); i++ {
		if !isIdent(value[i]) {
			return false
		}
	}
	return true
}

func isIdent(b byte) bool {
	return b == '$' || b == '_' || b >= '0' && b <= '9' || b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z'
}

func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\r' || b == '\n'
}

func symbolKind(kind string) int {
	switch kind {
	case "class":
		return 5
	case "variable":
		return 13
	default:
		return 12
	}
}
