package lspserver

import (
	"context"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
)

func (s *Server) vbscriptSignatureHover(parsed *core.ParsedDocument, offset int) *lsp.Hover {
	return s.vbscriptSignatureHoverContext(context.Background(), parsed, offset)
}

func (s *Server) vbscriptSignatureHoverContext(ctx context.Context, parsed *core.ParsedDocument, offset int) *lsp.Hover {
	if ctx == nil {
		ctx = context.Background()
	}
	if parsed == nil || ctx.Err() != nil {
		return nil
	}
	word := vbscript.WordAt(parsed.Text, offset)
	if word == "" {
		return nil
	}
	if hover := vbscriptPropertySignatureHoverWithLocale(parsed, offset, s.settings.Locale); hover != nil {
		return hover
	}
	lower := strings.ToLower(word)
	if !vbscriptQualifiedMemberAtOffset(parsed.Text, offset) {
		if declaration, ok := vbscriptClassMemberDeclarationAtOffset(parsed, word, offset); ok && declaration.Kind == "method" {
			if signature, ok := vbscriptSignatureForMemberDeclaration(parsed, declaration); ok {
				annotations := graphAnalysisTypes(parsed)
				missingTypeMetadata := positionInRangeOffset(offset, signature.NameRange, parsed) &&
					vbscriptSignatureMissingTypeMetadata(parsed, signature, &annotations)
				signature = annotatedVBScriptSignature(parsed, signature)
				return vbscriptSignatureHover(signature, vbscriptXMLDocForSignature(parsed, signature), missingTypeMetadata, s.settings.Locale)
			}
		}
	}
	if signature, ok := vbscriptSignatureAtOffset(parsed, offset, word); ok {
		annotations := graphAnalysisTypes(parsed)
		missingTypeMetadata := positionInRangeOffset(offset, signature.NameRange, parsed) &&
			vbscriptSignatureMissingTypeMetadata(parsed, signature, &annotations)
		signature = annotatedVBScriptSignature(parsed, signature)
		return vbscriptSignatureHover(signature, vbscriptXMLDocForSignature(parsed, signature), missingTypeMetadata, s.settings.Locale)
	}
	included, complete := s.includedDocumentsContextResult(ctx, parsed)
	if !complete || ctx.Err() != nil {
		return nil
	}
	for _, included := range included {
		if ctx.Err() != nil {
			return nil
		}
		if signature, ok := vbscript.BuildSignatures(included)[lower]; ok {
			signature = annotatedVBScriptSignature(included, signature)
			return s.appendVBScriptDefinedInHover(vbscriptSignatureHover(signature, vbscriptXMLDocForSignature(included, signature), false, s.settings.Locale), included.URI)
		}
	}
	return nil
}

func annotatedVBScriptSignature(parsed *core.ParsedDocument, signature vbscript.Signature) vbscript.Signature {
	if parsed == nil || signature.Label == "" {
		return signature
	}
	analysis := graphAnalysisTypes(parsed)
	typeName := graphReturnTypeForSignature(parsed, signature, &analysis)
	if typeName == "" || strings.Contains(strings.ToLower(signature.Label), " as ") {
		return signature
	}
	signature.Label += " As " + typeName
	return signature
}

func vbscriptSignatureAtOffset(parsed *core.ParsedDocument, offset int, word string) (vbscript.Signature, bool) {
	for _, signature := range vbscript.Signatures(parsed) {
		if !strings.EqualFold(signature.Name, word) {
			continue
		}
		if positionInRangeOffset(offset, signature.NameRange, parsed) {
			return signature, true
		}
	}
	if !vbscriptQualifiedMemberAtOffset(parsed.Text, offset) {
		if declaration, ok := vbscriptClassMemberDeclarationAtOffset(parsed, word, offset); ok && declaration.Kind == "method" {
			if signature, ok := vbscriptSignatureForMemberDeclaration(parsed, declaration); ok {
				return signature, true
			}
		}
	}
	return vbscriptRootSignatureByName(parsed, word)
}

func vbscriptRootSignatureByName(parsed *core.ParsedDocument, name string) (vbscript.Signature, bool) {
	if parsed == nil || name == "" {
		return vbscript.Signature{}, false
	}
	classOwners := vbClassMemberLineOwners(parsed)
	for _, signature := range vbscript.Signatures(parsed) {
		if strings.EqualFold(signature.Name, name) && classOwners[signature.NameRange.Start.Line] == "" {
			return signature, true
		}
	}
	return vbscript.Signature{}, false
}

func vbscriptPropertySignatureHoverWithLocale(parsed *core.ParsedDocument, offset int, locale string) *lsp.Hover {
	if parsed == nil {
		return nil
	}
	doc := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	for _, region := range parsed.Regions {
		if region.Language != core.LanguageVBScript || offset < region.ContentStart || offset > region.ContentEnd {
			continue
		}
		for lineStart := region.ContentStart; lineStart < region.ContentEnd; {
			lineEnd := vbPhysicalLineEnd(parsed.Text, lineStart, region.ContentEnd)
			header, logicalEnd, headerOK := vbscript.ProcedureHeaderAtLogical(parsed.Text, lineStart)
			if headerOK {
				if logicalEnd <= region.ContentEnd {
					lineEnd = logicalEnd
				} else {
					headerOK = false
				}
			}
			if headerOK && header.Kind == "property" {
				nameStart := header.NameStart
				nameEnd := header.NameEnd
				if offset >= nameStart && offset <= nameEnd {
					visibility := "Public"
					if header.Visibility != "" {
						visibility = header.Visibility
					}
					accessor := parsed.Text[header.AccessorStart:header.AccessorEnd]
					name := parsed.Text[header.NameStart:header.NameEnd]
					parameters := ""
					if header.HasParameterList {
						parameters = vbProcedureParameterDisplay(parsed.Text[header.ParamsStart:header.ParamsEnd])
					}
					signature, ok := vbPropertyAccessorSignatureAt(parsed, doc.PositionAt(nameStart))
					if !ok || !lspRangeEqual(signature.NameRange, doc.Range(nameStart, nameEnd)) {
						signature = vbscript.Signature{
							Name: name, Kind: "property",
							Label: visibility + " Property " + accessor + " " + name + "(" + parameters + ")",
							Range: doc.Range(lineStart, lineEnd), NameRange: doc.Range(nameStart, nameEnd),
							Parameters: vbPropertyParameters(parameters),
						}
					}
					analysis := graphAnalysisTypes(parsed)
					owner := vbClassMemberLineOwners(parsed)[doc.PositionAt(lineStart).Line]
					typeName := graphReturnTypeForDeclaration(vbUsageDeclaration{Name: name, MemberOf: owner, Kind: "property"}, &analysis)
					if typeName == "" {
						typeName = inferVBScriptPropertyType(parsed.Text, lineEnd, region.ContentEnd, name)
					}
					value := "```vbscript\n" + visibility + " Property " + accessor + " " + name + "(" + parameters + ")"
					if typeName != "" {
						value += " As " + typeName
					}
					value += "\n```"
					if documentation := vbscriptXMLDocForSignature(parsed, signature); documentation.hasContent() {
						value += "\n\n" + documentation.markdown(signature, locale)
					}
					return &lsp.Hover{Contents: lsp.MarkupContent{Kind: "markdown", Value: value}, Range: ptrRange(doc.Range(nameStart, nameEnd))}
				}
			}
			if lineEnd >= region.ContentEnd {
				break
			}
			lineStart = vbNextLineStart(parsed.Text, lineEnd, region.ContentEnd)
		}
	}
	return nil
}

func inferVBScriptPropertyType(text string, bodyStart int, regionEnd int, name string) string {
	lowerName := strings.ToLower(name)
	for lineStart := bodyStart; lineStart < regionEnd; {
		lineEnd := lineStart
		for lineEnd < regionEnd && text[lineEnd] != '\n' && text[lineEnd] != '\r' {
			lineEnd++
		}
		line := strings.TrimSpace(text[lineStart:lineEnd])
		lower := strings.ToLower(line)
		if lower == "end property" {
			return ""
		}
		if strings.HasPrefix(lower, lowerName) {
			rest := strings.TrimSpace(line[len(name):])
			if strings.HasPrefix(rest, "=") {
				return inferVBScriptLiteralType(strings.TrimSpace(rest[1:]))
			}
		}
		if lineEnd >= regionEnd {
			break
		}
		lineStart = lineEnd + 1
		if text[lineEnd] == '\r' && lineStart < regionEnd && text[lineStart] == '\n' {
			lineStart++
		}
	}
	return ""
}

func inferVBScriptLiteralType(value string) string {
	lower := strings.ToLower(strings.TrimSpace(value))
	switch {
	case lower == "true" || lower == "false":
		return "Boolean"
	case strings.HasPrefix(value, `"`):
		return "String"
	case lower == "nothing":
		return "Object"
	case lower != "" && (lower[0] >= '0' && lower[0] <= '9' || lower[0] == '-'):
		return "Number"
	default:
		return ""
	}
}

func ptrRange(value lsp.Range) *lsp.Range {
	return &value
}

func vbscriptSignatureHover(signature vbscript.Signature, doc vbscriptXMLDoc, missingTypeMetadata bool, locale string) *lsp.Hover {
	prefix := "Function"
	if strings.EqualFold(signature.Kind, "sub") {
		prefix = "Sub"
	}
	value := "```vbscript\n" + prefix + " " + signature.Label + "\n```"
	if doc.hasContent() {
		value += "\n\n" + doc.markdown(signature, locale)
		if missingTypeMetadata {
			value += "\n\n_" + vbscriptXMLDocumentationTypeNoteForLocale(locale) + "_"
		}
	}
	return &lsp.Hover{
		Contents: lsp.MarkupContent{
			Kind:  "markdown",
			Value: value,
		},
	}
}

func positionInRangeOffset(offset int, r lsp.Range, parsed *core.ParsedDocument) bool {
	doc := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	start := doc.OffsetAt(r.Start)
	end := doc.OffsetAt(r.End)
	return offset >= start && offset <= end
}
