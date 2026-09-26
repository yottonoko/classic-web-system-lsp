package lspserver

import (
	"fmt"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

type vbscriptParsedTypeAnnotation struct {
	kind     string
	name     string
	typeText string
	typeExpr vbscriptType
	err      error
}

func parseVBScriptTypeAnnotationText(annotation string) (vbscriptParsedTypeAnnotation, bool) {
	annotation = strings.TrimSpace(annotation)
	if !strings.HasPrefix(annotation, "@") {
		return vbscriptParsedTypeAnnotation{}, false
	}
	space := strings.IndexAny(annotation, " \t")
	keyword := annotation
	rest := ""
	if space >= 0 {
		keyword = annotation[:space]
		rest = strings.TrimSpace(annotation[space:])
	}
	keyword = strings.ToLower(keyword)
	if keyword != "@type" && keyword != "@param" && keyword != "@member" && keyword != "@returns" {
		return vbscriptParsedTypeAnnotation{}, false
	}
	parsed := vbscriptParsedTypeAnnotation{kind: strings.TrimPrefix(keyword, "@")}
	switch keyword {
	case "@type", "@param", "@member":
		name, typeText, ok := splitVBScriptAnnotationNameAndType(rest)
		if !ok {
			parsed.err = fmt.Errorf("%s annotation requires a name, As, and a type", keyword)
			return parsed, true
		}
		parsed.name = name
		parsed.typeText = typeText
		if !validVBScriptAnnotationName(parsed.kind, name) {
			parsed.err = fmt.Errorf("%s annotation has an invalid name", keyword)
			return parsed, true
		}
	case "@returns":
		if rest == "" {
			parsed.err = fmt.Errorf("%s annotation requires a type", keyword)
			return parsed, true
		}
		if typeExpr, err := parseVBScriptType(rest); err == nil {
			parsed.typeText = rest
			parsed.typeExpr = typeExpr
			return parsed, true
		}
		name, typeText, ok := splitVBScriptReturnNameAndType(rest)
		if !ok {
			parsed.err = fmt.Errorf("%s annotation requires a type or a name and type", keyword)
			return parsed, true
		}
		if !isVBScriptBareAnnotationName(name) {
			parsed.err = fmt.Errorf("%s annotation has an invalid name", keyword)
			return parsed, true
		}
		parsed.name = name
		parsed.typeText = typeText
	}
	if parsed.typeExpr, parsed.err = parseVBScriptType(parsed.typeText); parsed.err != nil {
		return parsed, true
	}
	return parsed, true
}

func splitVBScriptAnnotationNameAndType(rest string) (string, string, bool) {
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return "", "", false
	}
	nameEnd := strings.IndexAny(rest, " \t")
	if nameEnd <= 0 {
		return "", "", false
	}
	name := rest[:nameEnd]
	remainder := strings.TrimSpace(rest[nameEnd:])
	if len(remainder) < 2 || !strings.EqualFold(remainder[:2], "as") {
		return "", "", false
	}
	if len(remainder) > 2 && !isVBWhitespace(remainder[2]) {
		return "", "", false
	}
	typeText := strings.TrimSpace(remainder[2:])
	if !isVBScriptAnnotationName(name) || typeText == "" {
		return "", "", false
	}
	return name, typeText, true
}

func splitVBScriptReturnNameAndType(rest string) (string, string, bool) {
	space := strings.IndexAny(rest, " \t")
	if space <= 0 {
		return "", "", false
	}
	name := strings.TrimSpace(rest[:space])
	typeText := strings.TrimSpace(rest[space:])
	if !isVBScriptAnnotationName(name) || typeText == "" {
		return "", "", false
	}
	return name, typeText, true
}

func isVBScriptAnnotationName(name string) bool {
	if name == "" {
		return false
	}
	for index, char := range name {
		if index == 0 {
			if !isVBScriptTypeNameStart(char) {
				return false
			}
			continue
		}
		if !isVBScriptTypeNamePart(char) {
			return false
		}
	}
	return true
}

func validVBScriptAnnotationName(kind, name string) bool {
	switch kind {
	case "type", "returns":
		return isVBScriptBareAnnotationName(name)
	case "member":
		return isVBScriptQualifiedAnnotationName(name)
	case "param":
		return isVBScriptParamAnnotationName(name)
	default:
		return false
	}
}

func isVBScriptBareAnnotationName(name string) bool {
	if name == "" {
		return false
	}
	for index, char := range name {
		if index == 0 {
			if !isVBScriptTypeNameStart(char) {
				return false
			}
			continue
		}
		if !((char >= 'A' && char <= 'Z') || (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '_') {
			return false
		}
	}
	return true
}

func isVBScriptQualifiedAnnotationName(name string) bool {
	parts := strings.Split(name, ".")
	if len(parts) != 2 {
		return false
	}
	for _, part := range parts {
		if !isVBScriptBareAnnotationName(part) {
			return false
		}
	}
	return true
}

func isVBScriptParamAnnotationName(name string) bool {
	parts := strings.Split(name, ".")
	if len(parts) < 1 || len(parts) > 2 {
		return false
	}
	for _, part := range parts {
		if !isVBScriptBareAnnotationName(part) {
			return false
		}
	}
	return true
}

func vbscriptTypeAnnotationDiagnostics(parsed *core.ParsedDocument) []lsp.Diagnostic {
	if parsed == nil {
		return nil
	}
	doc := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	var diagnostics []lsp.Diagnostic
	for _, region := range parsed.Regions {
		if region.Language != core.LanguageVBScript {
			continue
		}
		for lineStart := region.ContentStart; lineStart < region.ContentEnd; {
			lineEnd := lineStart
			for lineEnd < region.ContentEnd && parsed.Text[lineEnd] != '\n' && parsed.Text[lineEnd] != '\r' {
				lineEnd++
			}
			line := parsed.Text[lineStart:lineEnd]
			comment := vbscriptAnnotationCommentOffset(line)
			if comment >= 0 {
				annotationText := strings.TrimSpace(line[comment+1:])
				if annotation, ok := parseVBScriptTypeAnnotationText(annotationText); ok {
					if annotation.err != nil {
						start := lineStart + comment
						diagnostics = append(diagnostics, lsp.Diagnostic{
							Range:    doc.Range(start, lineEnd),
							Severity: lsp.DiagnosticSeverityWarning,
							Code:     "malformedTypeAnnotation",
							Source:   vbscriptTypeDiagnosticSource,
							Message:  "malformedTypeAnnotation",
							Data: map[string]any{
								"annotation": annotationText,
								"kind":       annotation.kind,
							},
						})
					}
				}
			}
			if lineEnd >= region.ContentEnd {
				break
			}
			lineStart = lineEnd + 1
			if parsed.Text[lineEnd] == '\r' && lineStart < region.ContentEnd && parsed.Text[lineStart] == '\n' {
				lineStart++
			}
		}
	}
	return dedupeDiagnostics(diagnostics)
}

func vbscriptAnnotationCommentOffset(line string) int {
	inString := false
	for index := 0; index < len(line); index++ {
		switch line[index] {
		case '"':
			if inString && index+1 < len(line) && line[index+1] == '"' {
				index++
				continue
			}
			inString = !inString
		case '\'':
			if !inString {
				return index
			}
		}
	}
	return -1
}
