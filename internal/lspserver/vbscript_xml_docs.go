package lspserver

import (
	"html"
	"regexp"
	"slices"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
)

type vbscriptXMLDoc struct {
	Summary string
	Returns string
	Params  map[string]string
	Plain   []string
}

const vbscriptXMLDocumentationTypeNote = "XML documentation is descriptive only. Use `' @type`, `' @param ... As ...`, or `' @returns ...` annotations for VBScript type metadata."

func vbscriptXMLDocumentationTypeNoteForLocale(locale string) string {
	if locale == "ja" {
		return "XML ドキュメントコメントは説明用です。VBScript の型メタデータには `' @type`、`' @param ... As ...`、`' @returns ...` 注釈を使ってください。"
	}
	return vbscriptXMLDocumentationTypeNote
}

var (
	xmlParamPattern = regexp.MustCompile(`(?is)<param\b[^>]*\bname\s*=\s*["']([^"']+)["'][^>]*>(.*?)</param>`)
	xmlTagStripper  = regexp.MustCompile(`(?is)<[^>]+>`)
)

func vbscriptXMLDocBeforeLine(parsed *core.ParsedDocument, line int) vbscriptXMLDoc {
	lines := commentLinesBeforeLine(parsed, line)
	line = len(lines)
	block := xmlDocBlockBeforeLine(lines, line)
	if len(block) > 0 {
		return parseVBScriptXMLDoc(strings.Join(block, "\n"))
	}
	if plain := plainDocBlockBeforeLine(lines, line); len(plain) > 0 {
		return parseVBScriptPlainDoc(plain)
	}
	return vbscriptXMLDoc{Params: map[string]string{}}
}

func vbscriptXMLDocForSignature(parsed *core.ParsedDocument, signature vbscript.Signature) vbscriptXMLDoc {
	if parsed == nil {
		return vbscriptXMLDoc{Params: map[string]string{}}
	}
	return vbscriptXMLDocBeforeLine(parsed, signature.Range.Start.Line)
}

// commentLinesBeforeLine returns the contiguous comment lines directly above
// line. Doc blocks never extend past a non-comment line, so this avoids
// splitting the whole document for every signature lookup.
func commentLinesBeforeLine(parsed *core.ParsedDocument, line int) []string {
	if line <= 0 {
		return nil
	}
	text := parsed.Text
	end := core.SourceDocument(parsed).OffsetAt(lsp.Position{Line: line})
	var lines []string
	for end > 0 {
		lineEnd := end
		if text[lineEnd-1] == '\n' {
			lineEnd--
			if lineEnd > 0 && text[lineEnd-1] == '\r' {
				lineEnd--
			}
		} else if text[lineEnd-1] == '\r' {
			lineEnd--
		}
		if lineEnd == end && end < len(text) {
			break
		}
		lineStart := strings.LastIndexAny(text[:lineEnd], "\r\n") + 1
		content := text[lineStart:lineEnd]
		if !strings.HasPrefix(strings.TrimSpace(content), "'") {
			break
		}
		lines = append(lines, content)
		end = lineStart
	}
	slices.Reverse(lines)
	return lines
}

func xmlDocBlockBeforeLine(lines []string, line int) []string {
	if line > len(lines) {
		line = len(lines)
	}
	block := []string{}
	for i := line - 1; i >= 0; i-- {
		trimmed := strings.TrimSpace(strings.TrimRight(lines[i], "\r"))
		if !strings.HasPrefix(trimmed, "'''") {
			break
		}
		content := strings.TrimSpace(strings.TrimPrefix(trimmed, "'''"))
		block = append([]string{content}, block...)
	}
	return block
}

func plainDocBlockBeforeLine(lines []string, line int) []string {
	if line > len(lines) {
		line = len(lines)
	}
	block := []string{}
	for i := line - 1; i >= 0; i-- {
		trimmed := strings.TrimSpace(strings.TrimRight(lines[i], "\r"))
		if strings.HasPrefix(trimmed, "'''") || !strings.HasPrefix(trimmed, "'") {
			break
		}
		content := strings.TrimSpace(strings.TrimPrefix(trimmed, "'"))
		if strings.HasPrefix(content, "@") {
			break
		}
		block = append([]string{content}, block...)
	}
	return block
}

func parseVBScriptXMLDoc(source string) vbscriptXMLDoc {
	doc := vbscriptXMLDoc{Params: map[string]string{}}
	if summary, ok := xmlDocElementBody(source, "summary"); ok {
		doc.Summary = cleanXMLDocText(summary)
	}
	if returns, ok := xmlDocElementBody(source, "returns"); ok {
		doc.Returns = cleanXMLDocText(returns)
	}
	for _, match := range xmlParamPattern.FindAllStringSubmatch(source, -1) {
		if len(match) < 3 {
			continue
		}
		name := strings.TrimSpace(match[1])
		if name == "" {
			continue
		}
		doc.Params[strings.ToLower(name)] = cleanXMLDocText(match[2])
	}
	return doc
}

func xmlDocElementBody(source string, tag string) (string, bool) {
	lowerSource := strings.ToLower(source)
	lowerTag := strings.ToLower(tag)
	openStart := xmlDocFindOpeningTag(lowerSource, lowerTag, 0)
	if openStart < 0 {
		return "", false
	}
	openEnd := strings.IndexByte(source[openStart:], '>')
	if openEnd < 0 {
		return "", false
	}
	bodyStart := openStart + openEnd + 1
	cursor := bodyStart
	depth := 1
	for cursor < len(source) {
		nextOpen := xmlDocFindOpeningTag(lowerSource, lowerTag, cursor)
		nextClose := strings.Index(lowerSource[cursor:], "</"+lowerTag)
		if nextClose >= 0 {
			nextClose += cursor
		}
		if nextClose < 0 && nextOpen < 0 {
			break
		}
		if nextOpen >= 0 && (nextClose < 0 || nextOpen < nextClose) {
			depth++
			end := strings.IndexByte(source[nextOpen:], '>')
			if end < 0 {
				break
			}
			cursor = nextOpen + end + 1
			continue
		}
		depth--
		if depth == 0 {
			return source[bodyStart:nextClose], true
		}
		end := strings.IndexByte(source[nextClose:], '>')
		if end < 0 {
			break
		}
		cursor = nextClose + end + 1
	}
	return source[bodyStart:], true
}

func xmlDocFindOpeningTag(lowerSource string, lowerTag string, start int) int {
	needle := "<" + lowerTag
	cursor := start
	for cursor < len(lowerSource) {
		index := strings.Index(lowerSource[cursor:], needle)
		if index < 0 {
			return -1
		}
		index += cursor
		after := index + len(needle)
		if after >= len(lowerSource) || lowerSource[after] == '>' || lowerSource[after] == '/' || isVBWhitespace(lowerSource[after]) {
			return index
		}
		cursor = after
	}
	return -1
}

func parseVBScriptPlainDoc(lines []string) vbscriptXMLDoc {
	doc := vbscriptXMLDoc{Params: map[string]string{}, Plain: make([]string, 0, len(lines))}
	for _, line := range lines {
		if line == "" {
			continue
		}
		doc.Plain = append(doc.Plain, escapePlainDocMarkdown(line))
	}
	return doc
}

func cleanXMLDocText(value string) string {
	value = xmlTagStripper.ReplaceAllString(value, "")
	value = html.UnescapeString(value)
	return strings.Join(strings.Fields(value), " ")
}

func escapePlainDocMarkdown(value string) string {
	value = html.EscapeString(value)
	value = strings.ReplaceAll(value, `\`, `\\`)
	for _, marker := range []string{"*", "_", "`", "[", "]", "(", ")", "#", "+", "-", ".", "!"} {
		value = strings.ReplaceAll(value, marker, `\`+marker)
	}
	return value
}

func (d vbscriptXMLDoc) hasContent() bool {
	return d.Summary != "" || d.Returns != "" || len(d.Params) > 0 || len(d.Plain) > 0
}

func (d vbscriptXMLDoc) markdown(signature vbscript.Signature, locale string) string {
	if len(d.Plain) > 0 {
		return strings.Join(d.Plain, "  \n")
	}
	parts := []string{}
	if d.Summary != "" {
		parts = append(parts, d.Summary)
	}
	paramLines := []string{}
	for _, param := range signature.Parameters {
		if description := d.Params[strings.ToLower(param.Name)]; description != "" {
			paramLines = append(paramLines, "- `"+param.Name+"`: "+description)
		}
	}
	if len(paramLines) > 0 {
		parts = append(parts, "**"+vbscriptXMLDocHeading(locale, "parameters")+"**\n"+strings.Join(paramLines, "\n"))
	}
	if d.Returns != "" {
		parts = append(parts, "**"+vbscriptXMLDocHeading(locale, "returns")+"**\n\n"+d.Returns)
	}
	return strings.Join(parts, "\n\n")
}

func vbscriptXMLDocHeading(locale string, key string) string {
	if locale == "ja" {
		switch key {
		case "parameters":
			return "パラメーター"
		case "returns":
			return "戻り値"
		}
	}
	switch key {
	case "parameters":
		return "Parameters"
	case "returns":
		return "Returns"
	default:
		return key
	}
}

func vbscriptXMLDocCompletions(parsed *core.ParsedDocument, text string, offset int, prefix string) ([]lsp.CompletionItem, bool) {
	trimmed := strings.TrimLeft(prefix, " \t")
	lower := strings.ToLower(trimmed)
	switch {
	case xmlAttributeValueOpen(lower, "name"):
		return vbscriptXMLParamNameCompletions(parsed, text, offset), true
	case xmlAttributeValueOpen(lower, "cref"):
		return vbscriptXMLCrefCompletions(parsed), true
	case xmlClosingTagContext(lower):
		return vbscriptXMLTagCompletions(), true
	case xmlAttributeContext(lower):
		return []lsp.CompletionItem{{Label: "cref", Kind: lsp.CompletionItemKindProperty}}, true
	case xmlTagContext(lower):
		return vbscriptXMLTagCompletions(), true
	default:
		return nil, true
	}
}

func xmlAttributeValueOpen(prefix string, attr string) bool {
	attrStart := strings.LastIndex(prefix, attr+"=")
	if attrStart < 0 {
		return false
	}
	value := prefix[attrStart+len(attr)+1:]
	return strings.HasPrefix(value, `"`) && !strings.Contains(value[1:], `"`) ||
		strings.HasPrefix(value, `'`) && !strings.Contains(value[1:], `'`)
}

func xmlClosingTagContext(prefix string) bool {
	last := strings.LastIndex(prefix, "</")
	if last < 0 {
		return false
	}
	return !strings.Contains(prefix[last:], ">")
}

func xmlTagContext(prefix string) bool {
	last := strings.LastIndex(prefix, "<")
	if last < 0 {
		return false
	}
	fragment := prefix[last:]
	return !strings.Contains(fragment, ">") && !strings.HasPrefix(fragment, "</")
}

func xmlAttributeContext(prefix string) bool {
	last := strings.LastIndex(prefix, "<")
	if last < 0 {
		return false
	}
	fragment := prefix[last:]
	return !strings.Contains(fragment, ">") && strings.Contains(fragment, " ") && !strings.Contains(fragment, "=")
}

func vbscriptXMLTagCompletions() []lsp.CompletionItem {
	labels := []string{"summary", "param", "returns", "see"}
	items := make([]lsp.CompletionItem, 0, len(labels))
	for _, label := range labels {
		items = append(items, lsp.CompletionItem{Label: label, Kind: lsp.CompletionItemKindKeyword})
	}
	return items
}

func vbscriptXMLParamNameCompletions(parsed *core.ParsedDocument, text string, offset int) []lsp.CompletionItem {
	signature, ok := nextVBScriptSignature(parsed, text, offset)
	if !ok {
		return nil
	}
	items := make([]lsp.CompletionItem, 0, len(signature.Parameters))
	for _, param := range signature.Parameters {
		items = append(items, lsp.CompletionItem{Label: param.Name, Kind: lsp.CompletionItemKindValue})
	}
	return items
}

func vbscriptXMLCrefCompletions(parsed *core.ParsedDocument) []lsp.CompletionItem {
	signatures := vbscript.Signatures(parsed)
	items := make([]lsp.CompletionItem, 0, len(signatures))
	for _, signature := range signatures {
		items = append(items, lsp.CompletionItem{Label: signature.Name, Kind: vbscriptCompletionKind(signature.Kind)})
	}
	return items
}

func nextVBScriptSignature(parsed *core.ParsedDocument, text string, offset int) (vbscript.Signature, bool) {
	doc := core.NewTextDocument(parsed.URI, "classic-asp", 0, text)
	for _, signature := range vbscript.Signatures(parsed) {
		if doc.OffsetAt(signature.Range.Start) >= offset {
			return signature, true
		}
	}
	return vbscript.Signature{}, false
}

func enrichVBScriptSignatureHelp(parsed *core.ParsedDocument, help *lsp.SignatureHelp, locale string) *lsp.SignatureHelp {
	if help == nil || len(help.Signatures) == 0 {
		return help
	}
	name := signatureNameFromLabel(help.Signatures[0].Label)
	if name == "" {
		return help
	}
	signature, ok := vbscript.BuildSignatures(parsed)[strings.ToLower(name)]
	if !ok {
		return help
	}
	enrichVBScriptSignatureInformation(&help.Signatures[0], signature, locale, parsed)
	return help
}

func enrichVBScriptSignatureInformation(info *lsp.SignatureInformation, signature vbscript.Signature, locale string, parsed *core.ParsedDocument) {
	if info == nil || parsed == nil {
		return
	}
	doc := vbscriptXMLDocForSignature(parsed, signature)
	if !doc.hasContent() {
		return
	}
	info.Documentation = doc.markdown(signature, locale)
	for index, parameter := range info.Parameters {
		name := parameterNameFromLabel(parameter.Label)
		if description := doc.Params[strings.ToLower(name)]; description != "" {
			info.Parameters[index].Documentation = description
		}
	}
}

func vbscriptParameterDeclarationHover(parsed *core.ParsedDocument, offset int, locale string) *lsp.Hover {
	return vbscriptParameterHoverAtOffset(parsed, offset, locale)
}

// vbscriptParameterHoverAtOffset resolves both parameter declarations and
// references against the enclosing CST procedure scope. Parameter names are
// local to a procedure, so this lookup must run before included/global and
// built-in fallbacks can claim the same spelling.
func vbscriptParameterHoverAtOffset(parsed *core.ParsedDocument, offset int, locale string) *lsp.Hover {
	if parsed == nil {
		return nil
	}
	region := core.RegionAt(parsed, offset)
	if region == nil || region.Language != core.LanguageVBScript {
		return nil
	}
	word := vbscript.WordAt(parsed.Text, offset)
	if word == "" {
		return nil
	}
	scope := vbscriptScopeAtOffset(parsed, offset)
	if scope == "" {
		return nil
	}
	document := core.SourceDocument(parsed)
	declaration := vbUsageDeclaration{
		Name:     word,
		Kind:     "parameter",
		Line:     document.PositionAt(offset).Line,
		Local:    true,
		Scope:    scope,
		MemberOf: vbProcedureScopeOwner(scope),
	}
	start, end := vbscriptIdentifierOffsetsAtOffset(parsed.Text, offset)
	if start >= 0 && end > start {
		declaration.Range = document.Range(start, end)
		declaration.Start = start
		declaration.End = end
	}
	signature, ok := vbscriptSignatureForUsageDeclaration(parsed, declaration)
	if !ok {
		return nil
	}
	if positionInRangeOffset(offset, signature.NameRange, parsed) {
		return nil
	}
	if _, ok := signatureParameterByName(signature, word); !ok {
		return nil
	}
	if vbscriptLocalParameterShadowedAtOffset(parsed, declaration, offset) {
		return nil
	}
	return vbscriptParameterHoverForDeclaration(parsed, declaration, offset, locale)
}

func vbscriptLocalParameterShadowedAtOffset(parsed *core.ParsedDocument, parameter vbUsageDeclaration, offset int) bool {
	for _, declaration := range variableInlayDeclarations(parsed, true, nil) {
		if !declaration.Local || declaration.Start > offset ||
			!strings.EqualFold(declaration.Scope, parameter.Scope) ||
			!strings.EqualFold(declaration.Name, parameter.Name) ||
			!vbscriptUsageDeclarationsShareProcedureScope(parsed, parameter, declaration) {
			continue
		}
		return true
	}
	return false
}

func vbscriptUsageDeclarationsShareProcedureScope(parsed *core.ParsedDocument, left, right vbUsageDeclaration) bool {
	leftScope, leftOK := vbProcedureScopeForUsageDeclaration(parsed, left)
	rightScope, rightOK := vbProcedureScopeForUsageDeclaration(parsed, right)
	if leftOK != rightOK {
		return false
	}
	if leftOK {
		return leftScope.NameStart == rightScope.NameStart
	}
	return true
}

func vbscriptParameterHoverForDeclaration(parsed *core.ParsedDocument, declaration vbUsageDeclaration, offset int, locale string) *lsp.Hover {
	signature, ok := vbscriptSignatureForUsageDeclaration(parsed, declaration)
	if !ok {
		return nil
	}
	parameter, ok := signatureParameterByName(signature, declaration.Name)
	if !ok {
		return nil
	}
	documentation := ""
	doc := vbscriptXMLDocForSignature(parsed, signature)
	if description := doc.Params[strings.ToLower(parameter.Name)]; description != "" {
		documentation = description
	}
	typeName := graphParameterTypeForDeclaration(parsed, declaration, signature)
	if typeName == "" {
		typeName = inferVBDeclarationType(parsed, declaration)
	}
	if typeName == "" {
		typeName = "Variant"
	}
	document := core.SourceDocument(parsed)
	start, end := vbscriptIdentifierOffsetsAtOffset(parsed.Text, offset)
	hover := &lsp.Hover{
		Contents: lsp.MarkupContent{Kind: "markdown", Value: markdownVBScriptSignature(parameter.Mode+" "+parameter.Name+" As "+typeName, documentation)},
	}
	if start >= 0 && end > start {
		rangeValue := document.Range(start, end)
		hover.Range = &rangeValue
	}
	return hover
}

func vbscriptIdentifierOffsetsAtOffset(text string, offset int) (int, int) {
	if offset < 0 {
		return -1, -1
	}
	if offset > len(text) {
		offset = len(text)
	}
	start := offset
	if start == len(text) || start < len(text) && !isVBIdentifier(text[start]) {
		start--
	}
	if start < 0 || !isVBIdentifier(text[start]) {
		return -1, -1
	}
	for start > 0 && isVBIdentifier(text[start-1]) {
		start--
	}
	end := start
	for end < len(text) && isVBIdentifier(text[end]) {
		end++
	}
	return start, end
}

func signatureParameterByName(signature vbscript.Signature, name string) (vbscript.Parameter, bool) {
	for _, parameter := range signature.Parameters {
		if strings.EqualFold(parameter.Name, name) {
			return parameter, true
		}
	}
	return vbscript.Parameter{}, false
}

func vbscriptNoParenSignatureHelp(parsed *core.ParsedDocument, position lsp.Position, locale string) *lsp.SignatureHelp {
	doc := core.SourceDocument(parsed)
	offset := doc.OffsetAt(position)
	lineStart := offset
	for lineStart > 0 && parsed.Text[lineStart-1] != '\n' && parsed.Text[lineStart-1] != '\r' {
		lineStart--
	}
	lineEnd := offset
	for lineEnd < len(parsed.Text) && parsed.Text[lineEnd] != '\n' && parsed.Text[lineEnd] != '\r' {
		lineEnd++
	}
	statementStart := vbscriptStatementStart(parsed.Text, lineStart, offset)
	line := parsed.Text[statementStart:lineEnd]
	relativeOffset := offset - statementStart
	if relativeOffset < 0 || relativeOffset > len(line) {
		return nil
	}
	name, nameStart, argsStart, ok := vbscriptNoParenCallDetails(line, relativeOffset)
	if !ok {
		return nil
	}
	signature, ok := vbscriptSignatureForNoParenCall(parsed, statementStart+nameStart, name)
	if !ok || len(signature.Parameters) == 0 {
		return nil
	}
	label := signature.Label
	if strings.EqualFold(signature.Kind, "sub") {
		label = "Sub " + label
	} else if strings.EqualFold(signature.Kind, "function") {
		label = "Function " + label
	}
	info := lsp.SignatureInformation{
		Label:      label,
		Parameters: vbscriptNoParenParameterInformation(signature.Parameters),
	}
	enrichVBScriptSignatureInformation(&info, signature, locale, parsed)
	return &lsp.SignatureHelp{
		Signatures:      []lsp.SignatureInformation{info},
		ActiveSignature: 0,
		ActiveParameter: vbscriptNoParenActiveParameter(line, argsStart, relativeOffset),
	}
}

func vbscriptStatementStart(text string, lineStart, offset int) int {
	if lineStart < 0 {
		lineStart = 0
	}
	if offset > len(text) {
		offset = len(text)
	}
	statementStart := lineStart
	inString := false
	for index := lineStart; index < offset; index++ {
		switch text[index] {
		case '"':
			if inString && index+1 < offset && text[index+1] == '"' {
				index++
				continue
			}
			inString = !inString
		case ':':
			if !inString {
				statementStart = index + 1
			}
		}
	}
	return statementStart
}

func vbscriptNoParenCallDetails(line string, offset int) (string, int, int, bool) {
	prefix := strings.TrimLeft(line[:offset], " \t")
	leading := len(line[:offset]) - len(prefix)
	if strings.HasPrefix(strings.ToLower(prefix), "call ") {
		leading += len(prefix) - len(strings.TrimLeft(prefix[len("call "):], " \t"))
		prefix = strings.TrimLeft(prefix[len("call "):], " \t")
	}
	nameEnd := 0
	for nameEnd < len(prefix) && isVBIdentifierByte(prefix[nameEnd]) {
		nameEnd++
	}
	if nameEnd == 0 {
		return "", 0, 0, false
	}
	name := prefix[:nameEnd]
	cursor := leading + nameEnd
	for cursor < len(line) && (line[cursor] == ' ' || line[cursor] == '\t') {
		cursor++
	}
	if cursor >= offset || cursor < len(line) && line[cursor] == '(' {
		return "", 0, 0, false
	}
	return name, leading, cursor, true
}

func vbscriptSignatureForNoParenCall(parsed *core.ParsedDocument, nameStart int, name string) (vbscript.Signature, bool) {
	if parsed == nil || name == "" {
		return vbscript.Signature{}, false
	}
	position := core.SourceDocument(parsed).PositionAt(nameStart)
	if declaration, ok := vbscriptClassMemberDeclarationAtOffset(parsed, name, nameStart); ok && declaration.Kind == "method" {
		if signature, ok := vbscriptSignatureForMemberDeclaration(parsed, declaration); ok {
			return signature, true
		}
	}
	if vbLocalDeclarationShadowsNameAt(parsed, name, position) {
		return vbscript.Signature{}, false
	}
	return vbscriptRootSignatureByName(parsed, name)
}

func vbscriptNoParenParameterInformation(params []vbscript.Parameter) []lsp.ParameterInformation {
	info := make([]lsp.ParameterInformation, 0, len(params))
	for _, param := range params {
		info = append(info, lsp.ParameterInformation{Label: param.Mode + " " + param.Name})
	}
	return info
}

func vbscriptNoParenActiveParameter(line string, argsStart int, offset int) int {
	if offset < argsStart {
		return 0
	}
	active := 0
	inString := false
	depth := 0
	for i := argsStart; i < offset && i < len(line); i++ {
		switch line[i] {
		case '"':
			if inString && i+1 < offset && i+1 < len(line) && line[i+1] == '"' {
				i++
				continue
			}
			inString = !inString
		case '(':
			if !inString {
				depth++
			}
		case ')':
			if !inString && depth > 0 {
				depth--
			}
		case ',':
			if !inString && depth == 0 {
				active++
			}
		}
	}
	return active
}

func vbscriptDeclarationMissingTypeMetadata(declaration vbUsageDeclaration, annotations *vbGraphAnalysisTypes) bool {
	if annotations == nil {
		return false
	}
	lowerName := strings.ToLower(declaration.Name)
	switch declaration.Kind {
	case "variable", "constant", "field":
		return strings.TrimSpace(annotations.Types[lowerName]) == ""
	case "parameter":
		owner := strings.ToLower(vbProcedureScopeName(declaration.Scope))
		return strings.TrimSpace(annotations.Params[owner][lowerName]) == "" &&
			strings.TrimSpace(annotations.Params[""][lowerName]) == ""
	case "function", "property", "method":
		return graphReturnTypeForDeclaration(declaration, annotations) == ""
	default:
		return false
	}
}

func vbscriptSignatureMissingTypeMetadata(parsed *core.ParsedDocument, signature vbscript.Signature, annotations *vbGraphAnalysisTypes) bool {
	if annotations == nil {
		return false
	}
	owner := strings.ToLower(signature.Name)
	scopedParams := annotations.ScopedParams[graphSignatureKey(graphSignatureOwner(parsed, signature), signature.Name, graphSignatureAccessor(parsed, signature))]
	for _, parameter := range signature.Parameters {
		lowerParam := strings.ToLower(parameter.Name)
		if strings.TrimSpace(scopedParams[lowerParam]) == "" &&
			strings.TrimSpace(annotations.Params[owner][lowerParam]) == "" &&
			strings.TrimSpace(annotations.Params[""][lowerParam]) == "" {
			return true
		}
	}
	if strings.EqualFold(signature.Kind, "function") && graphReturnTypeForSignature(parsed, signature, annotations) == "" {
		return true
	}
	return false
}

func vbscriptVariableXMLDocBeforeLine(parsed *core.ParsedDocument, declaration vbUsageDeclaration, declarations []vbUsageDeclaration) vbscriptXMLDoc {
	if !vbscriptVariableDocTargetIsUnambiguous(declaration, declarations) {
		return vbscriptXMLDoc{Params: map[string]string{}}
	}
	if plain := inlinePlainDocForDeclaration(parsed.Text, declaration); plain != "" {
		return parseVBScriptPlainDoc([]string{plain})
	}
	return vbscriptXMLDocBeforeLine(parsed, declaration.Line)
}

func inlinePlainDocForDeclaration(text string, declaration vbUsageDeclaration) string {
	lineEnd := declaration.End
	for lineEnd < len(text) && text[lineEnd] != '\n' && text[lineEnd] != '\r' {
		lineEnd++
	}
	if declaration.End >= lineEnd {
		return ""
	}
	commentStart := vbInlineApostropheCommentStart(text[declaration.End:lineEnd])
	if commentStart < 0 {
		return ""
	}
	comment := strings.TrimSpace(text[declaration.End+commentStart+1 : lineEnd])
	if comment == "" || strings.HasPrefix(comment, "@") {
		return ""
	}
	return comment
}

func vbInlineApostropheCommentStart(text string) int {
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '"':
			i = skipVBString(text, i) - 1
		case '\'':
			return i
		}
	}
	return -1
}

func vbscriptVariableDocTargetIsUnambiguous(target vbUsageDeclaration, declarations []vbUsageDeclaration) bool {
	if target.Implicit {
		return false
	}
	if target.Kind != "variable" && target.Kind != "constant" && target.Kind != "field" {
		return true
	}
	count := 0
	for _, declaration := range declarations {
		if declaration.Implicit || declaration.Line != target.Line || declaration.Scope != target.Scope || declaration.Local != target.Local {
			continue
		}
		if declaration.Kind == "variable" || declaration.Kind == "constant" || declaration.Kind == "field" {
			count++
		}
	}
	return count == 1
}

func signatureNameFromLabel(label string) string {
	if index := strings.Index(label, "("); index >= 0 {
		return strings.TrimSpace(label[:index])
	}
	return strings.TrimSpace(label)
}

func parameterNameFromLabel(label any) string {
	text, ok := label.(string)
	if !ok {
		return ""
	}
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}
