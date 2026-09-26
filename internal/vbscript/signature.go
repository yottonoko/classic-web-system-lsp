package vbscript

import (
	"sort"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

type Parameter struct {
	Name     string
	Mode     string
	Optional bool
}

type Signature struct {
	Name       string
	Kind       string
	Label      string
	Range      lsp.Range
	NameRange  lsp.Range
	Parameters []Parameter
}

type signatureMapRuntime struct {
	values              map[string]Signature
	ownsReferencedValue bool
}

func (runtime signatureMapRuntime) EstimateBytes() int64 {
	bytes := int64(128 + len(runtime.values)*128)
	for key, signature := range runtime.values {
		bytes += int64(len(key)) * 2
		if runtime.ownsReferencedValue {
			bytes += estimateSignatureReferencedBytes(signature)
		}
	}
	return bytes
}

type signatureListRuntime []Signature

func (runtime signatureListRuntime) EstimateBytes() int64 {
	bytes := int64(64 + cap(runtime)*128)
	for _, signature := range runtime {
		bytes += estimateSignatureReferencedBytes(signature)
	}
	return bytes
}

func estimateSignatureReferencedBytes(signature Signature) int64 {
	bytes := int64(len(signature.Name)+len(signature.Kind)+len(signature.Label)) * 2
	bytes += int64(cap(signature.Parameters)) * 64
	for _, parameter := range signature.Parameters {
		bytes += int64(len(parameter.Name)+len(parameter.Mode)) * 2
	}
	return bytes
}

// ProcedureHeader contains the source offsets for one Sub, Function, or
// Property accessor declaration. Offsets are byte offsets into the scanned
// statement; callers map them through their TextDocument to obtain LSP
// positions.
type ProcedureHeader struct {
	Kind                  string
	Accessor              string
	Visibility            string
	KeywordStart          int
	AccessorStart         int
	AccessorEnd           int
	NameStart             int
	NameEnd               int
	ParamsStart           int
	ParamsEnd             int
	End                   int
	HasParameterList      bool
	ParameterListComplete bool
}

// ProcedureHeaderAt scans one VBScript logical statement for a procedure
// declaration. A parameter list may be incomplete while a document is being
// edited; in that case the scan ends at a comment, colon, non-continuation
// newline, or the end of the statement and retains the materialized parameter
// prefix.
func ProcedureHeaderAt(text string) (ProcedureHeader, bool) {
	header := ProcedureHeader{ParamsStart: -1, ParamsEnd: -1}
	cursor := skipProcedureHeaderWhitespace(text, 0)
	if cursor >= len(text) || text[cursor] == '\'' {
		return ProcedureHeader{}, false
	}
	header.KeywordStart = cursor
	visibilityStart := cursor
	visibilityEnd := cursor
	visibilityCount := 0
	hasDefaultVisibility := false
	for {
		wordStart, wordEnd := procedureHeaderIdentifier(text, cursor)
		if wordStart < 0 {
			break
		}
		word := strings.ToLower(text[wordStart:wordEnd])
		if word != "public" && word != "private" && word != "default" {
			break
		}
		visibilityCount++
		hasDefaultVisibility = hasDefaultVisibility || word == "default"
		visibilityEnd = wordEnd
		cursor = skipProcedureHeaderWhitespace(text, wordEnd)
		if cursor >= len(text) {
			return ProcedureHeader{}, false
		}
	}
	wordStart, wordEnd := procedureHeaderIdentifier(text, cursor)
	if wordStart < 0 {
		return ProcedureHeader{}, false
	}
	header.KeywordStart = wordStart
	keyword := strings.ToLower(text[wordStart:wordEnd])
	switch keyword {
	case "sub", "function":
		if hasDefaultVisibility || visibilityCount > 1 {
			return ProcedureHeader{}, false
		}
		header.Kind = keyword
	case "property":
		if visibilityCount > 0 {
			header.Visibility = strings.TrimSpace(text[visibilityStart:visibilityEnd])
		}
		cursor = skipProcedureHeaderWhitespace(text, wordEnd)
		accessorStart, accessorEnd := procedureHeaderIdentifier(text, cursor)
		if accessorStart < 0 {
			return ProcedureHeader{}, false
		}
		accessor := strings.ToLower(text[accessorStart:accessorEnd])
		if accessor != "get" && accessor != "let" && accessor != "set" {
			return ProcedureHeader{}, false
		}
		header.Kind = "property"
		header.Accessor = accessor
		header.AccessorStart = accessorStart
		header.AccessorEnd = accessorEnd
		wordEnd = accessorEnd
	default:
		return ProcedureHeader{}, false
	}
	if header.Visibility == "" && visibilityCount > 0 {
		header.Visibility = strings.TrimSpace(text[visibilityStart:visibilityEnd])
	}
	cursor = skipProcedureHeaderWhitespace(text, wordEnd)
	nameStart, nameEnd := procedureHeaderIdentifier(text, cursor)
	if nameStart < 0 {
		return ProcedureHeader{}, false
	}
	header.NameStart = nameStart
	header.NameEnd = nameEnd
	cursor = skipProcedureHeaderWhitespace(text, nameEnd)
	header.End = cursor
	if cursor >= len(text) || text[cursor] != '(' {
		return header, true
	}
	header.HasParameterList = true
	header.ParamsStart = cursor + 1
	header.ParamsEnd, header.ParameterListComplete = scanProcedureParameters(text, header.ParamsStart)
	if header.ParameterListComplete {
		header.End = header.ParamsEnd + 1
	} else {
		header.End = header.ParamsEnd
	}
	return header, true
}

// ProcedureHeaderAtLogical scans a declaration beginning at start and returns
// the end of its explicit-continuation logical line. The returned header
// offsets are absolute offsets into text. A declaration without an explicit
// continuation remains bounded to its physical line.
func ProcedureHeaderAtLogical(text string, start int) (ProcedureHeader, int, bool) {
	if start < 0 || start >= len(text) {
		return ProcedureHeader{}, start, false
	}
	firstEnd := procedureHeaderLineEnd(text, start)
	// Most VBScript lines are declarations or executable statements. Avoid
	// running the full recovery scanner for lines that cannot begin a
	// procedure; callers use this helper while walking every physical line.
	candidate := skipProcedureHeaderWhitespace(text, start)
	if candidate >= firstEnd || text[candidate] == '\'' {
		return ProcedureHeader{}, firstEnd, false
	}
	wordStart, wordEnd := procedureHeaderIdentifier(text, candidate)
	if wordStart < 0 || !procedureHeaderLeadingKeyword(text[wordStart:wordEnd]) {
		return ProcedureHeader{}, firstEnd, false
	}
	first, ok := ProcedureHeaderAt(text[start:firstEnd])
	if !ok || !first.HasParameterList || first.ParameterListComplete || !procedureHeaderLineContinues(text, start, firstEnd) {
		if ok {
			shiftProcedureHeaderOffsets(&first, start)
		}
		return first, firstEnd, ok
	}

	logicalEnd := firstEnd
	lineStart := procedureHeaderNextLineStart(text, firstEnd)
	for lineStart < len(text) {
		lineEnd := procedureHeaderLineEnd(text, lineStart)
		if procedureHeaderContinuationBoundary(text, lineStart, lineEnd) {
			break
		}
		logicalEnd = lineEnd
		if !procedureHeaderLineContinues(text, lineStart, lineEnd) {
			break
		}
		lineStart = procedureHeaderNextLineStart(text, lineEnd)
	}
	header, ok := ProcedureHeaderAt(text[start:logicalEnd])
	if !ok {
		return ProcedureHeader{}, logicalEnd, false
	}
	shiftProcedureHeaderOffsets(&header, start)
	return header, logicalEnd, true
}

func shiftProcedureHeaderOffsets(header *ProcedureHeader, offset int) {
	if header == nil {
		return
	}
	header.KeywordStart += offset
	header.AccessorStart += offset
	header.AccessorEnd += offset
	header.NameStart += offset
	header.NameEnd += offset
	if header.ParamsStart >= 0 {
		header.ParamsStart += offset
	}
	if header.ParamsEnd >= 0 {
		header.ParamsEnd += offset
	}
	header.End += offset
}

func procedureHeaderLeadingKeyword(word string) bool {
	switch len(word) {
	case 3:
		return strings.EqualFold(word, "sub")
	case 6:
		return strings.EqualFold(word, "public")
	case 7:
		return strings.EqualFold(word, "default") || strings.EqualFold(word, "private")
	case 8:
		return strings.EqualFold(word, "function") || strings.EqualFold(word, "property")
	default:
		return false
	}
}

func procedureHeaderLineEnd(text string, start int) int {
	for end := start; end < len(text); end++ {
		if text[end] == '\r' || text[end] == '\n' {
			return end
		}
	}
	return len(text)
}

func procedureHeaderNextLineStart(text string, lineEnd int) int {
	if lineEnd >= len(text) {
		return lineEnd
	}
	lineStart := lineEnd + 1
	if text[lineEnd] == '\r' && lineStart < len(text) && text[lineStart] == '\n' {
		lineStart++
	}
	return lineStart
}

func procedureHeaderLineContinues(text string, lineStart, lineEnd int) bool {
	for cursor := lineStart; cursor < lineEnd; cursor++ {
		switch text[cursor] {
		case '\'':
			return false
		case '"':
			cursor++
			for cursor < lineEnd {
				if text[cursor] != '"' {
					cursor++
					continue
				}
				if cursor+1 < lineEnd && text[cursor+1] == '"' {
					cursor += 2
					continue
				}
				break
			}
			if cursor >= lineEnd {
				return false
			}
		}
	}
	marker := lineEnd
	for marker > lineStart && (text[marker-1] == ' ' || text[marker-1] == '\t') {
		marker--
	}
	if marker <= lineStart || text[marker-1] != '_' {
		return false
	}
	marker--
	return marker == lineStart || !isIdent(text[marker-1])
}

func procedureHeaderContinuationBoundary(text string, lineStart, lineEnd int) bool {
	cursor := skipProcedureHeaderWhitespace(text, lineStart)
	if cursor >= lineEnd || text[cursor] == '\'' {
		return true
	}
	wordStart, wordEnd := procedureHeaderIdentifier(text, cursor)
	if wordStart < 0 || wordEnd > lineEnd {
		return false
	}
	switch strings.ToLower(text[wordStart:wordEnd]) {
	case "class", "end", "function", "property", "rem", "sub":
		return true
	case "default", "private", "public":
		_, ok := ProcedureHeaderAt(text[lineStart:lineEnd])
		return ok
	default:
		return false
	}
}

func skipProcedureHeaderWhitespace(text string, offset int) int {
	for offset < len(text) && (text[offset] == ' ' || text[offset] == '\t') {
		offset++
	}
	return offset
}

func procedureHeaderIdentifier(text string, offset int) (int, int) {
	if offset < 0 || offset >= len(text) || !isIdentifierStart(text[offset]) {
		return -1, -1
	}
	end := offset + 1
	for end < len(text) && isIdent(text[end]) {
		end++
	}
	return offset, end
}

func scanProcedureParameters(text string, offset int) (int, bool) {
	depth := 1
	for cursor := offset; cursor < len(text); cursor++ {
		switch text[cursor] {
		case '"':
			cursor = skipProcedureHeaderString(text, cursor) - 1
		case '\'', ':':
			return cursor, false
		case '\r', '\n':
			if !procedureHeaderContinuationAt(text, cursor) {
				return cursor, false
			}
			cursor = procedureHeaderNextLineStart(text, cursor) - 1
		case '#':
			if end, ok := skipDateLiteral(text, cursor); ok {
				cursor = end - 1
			}
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return cursor, true
			}
		}
	}
	return len(text), false
}

func procedureHeaderContinuationAt(text string, newline int) bool {
	if newline <= 0 || (text[newline] != '\r' && text[newline] != '\n') {
		return false
	}
	lineStart := newline
	for lineStart > 0 && text[lineStart-1] != '\r' && text[lineStart-1] != '\n' {
		lineStart--
	}
	return procedureHeaderLineContinues(text, lineStart, newline)
}

func skipProcedureHeaderString(text string, offset int) int {
	for cursor := offset + 1; cursor < len(text); cursor++ {
		if text[cursor] != '"' {
			continue
		}
		if cursor+1 < len(text) && text[cursor+1] == '"' {
			cursor++
			continue
		}
		return cursor + 1
	}
	return len(text)
}

const (
	signatureByNameAnalysisKey        = "vbscript.signatures-by-name.v2"
	signatureByNameRuntimeAnalysisKey = "vbscript.signatures-by-name.runtime.v2"
)

func BuildSignatures(parsed *core.ParsedDocument) map[string]Signature {
	if cached, ok := parsed.LoadRuntimeAnalysis(signatureByNameRuntimeAnalysisKey); ok {
		if signatures, valid := cached.(signatureMapRuntime); valid {
			return signatures.values
		}
	}
	var cached map[string]Signature
	if parsed.LoadAnalysis(signatureByNameAnalysisKey, &cached) {
		parsed.StoreRuntimeAnalysis(signatureByNameRuntimeAnalysisKey, signatureMapRuntime{values: cached, ownsReferencedValue: true})
		return cached
	}
	signatures := map[string]Signature{}
	for _, signature := range Signatures(parsed) {
		key := strings.ToLower(signature.Name)
		if _, exists := signatures[key]; !exists {
			signatures[key] = signature
		}
	}
	parsed.StoreAnalysis(signatureByNameAnalysisKey, signatures)
	parsed.StoreRuntimeAnalysis(signatureByNameRuntimeAnalysisKey, signatureMapRuntime{values: signatures})
	return signatures
}

func Signatures(parsed *core.ParsedDocument) []Signature {
	if cached, ok := parsed.LoadRuntimeAnalysis("vbscript.signatures.runtime.v1"); ok {
		if signatures, valid := cached.(signatureListRuntime); valid {
			return []Signature(signatures)
		}
	}
	var cached []Signature
	if parsed.LoadAnalysis("vbscript.signatures.v1", &cached) {
		parsed.StoreRuntimeAnalysis("vbscript.signatures.runtime.v1", signatureListRuntime(cached))
		return cached
	}
	source := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	var signatures []Signature
	for _, region := range parsed.Regions {
		if region.Language != core.LanguageVBScript {
			continue
		}
		text := parsed.Text[region.ContentStart:region.ContentEnd]
		for lineStart := 0; lineStart < len(text); {
			header, lineEnd, ok := ProcedureHeaderAtLogical(text, lineStart)
			if !ok || (header.Kind != "sub" && header.Kind != "function") {
				if lineEnd >= len(text) {
					break
				}
				lineStart = procedureHeaderNextLineStart(text, lineEnd)
				continue
			}
			name := text[header.NameStart:header.NameEnd]
			paramsText := ""
			if header.HasParameterList {
				paramsText = text[header.ParamsStart:header.ParamsEnd]
			}
			params := parseParameters(paramsText)
			label := signatureLabel(name, params)
			start := region.ContentStart + header.KeywordStart
			nameStart := region.ContentStart + header.NameStart
			nameEnd := region.ContentStart + header.NameEnd
			end := region.ContentStart + header.End
			signatures = append(signatures, Signature{
				Name:       name,
				Kind:       header.Kind,
				Label:      label,
				Range:      source.Range(start, end),
				NameRange:  source.Range(nameStart, nameEnd),
				Parameters: params,
			})
			if lineEnd >= len(text) {
				break
			}
			lineStart = procedureHeaderNextLineStart(text, lineEnd)
		}
	}
	sort.Slice(signatures, func(i, j int) bool {
		if signatures[i].Range.Start.Line != signatures[j].Range.Start.Line {
			return signatures[i].Range.Start.Line < signatures[j].Range.Start.Line
		}
		return signatures[i].Range.Start.Character < signatures[j].Range.Start.Character
	})
	parsed.StoreAnalysis("vbscript.signatures.v1", signatures)
	parsed.StoreRuntimeAnalysis("vbscript.signatures.runtime.v1", signatureListRuntime(signatures))
	return signatures
}

func SignatureHelp(parsed *core.ParsedDocument, position lsp.Position) *lsp.SignatureHelp {
	return signatureHelp(parsed, position, false, true)
}

func StandaloneSignatureHelp(parsed *core.ParsedDocument, position lsp.Position) *lsp.SignatureHelp {
	return signatureHelp(parsed, position, true, true)
}

// BuiltinSignatureHelp はユーザー定義 procedure を参照せず、組み込み関数の signature help だけを返す。
// server 側で root と class の scope を解決した後に使う。
func BuiltinSignatureHelp(parsed *core.ParsedDocument, position lsp.Position) *lsp.SignatureHelp {
	return signatureHelp(parsed, position, false, false)
}

// StandaloneBuiltinSignatureHelp は standalone VBScript 用の BuiltinSignatureHelp で、WScript 専用 signature も含む。
func StandaloneBuiltinSignatureHelp(parsed *core.ParsedDocument, position lsp.Position) *lsp.SignatureHelp {
	return signatureHelp(parsed, position, true, false)
}

func signatureHelp(parsed *core.ParsedDocument, position lsp.Position, standalone, includeUserDefined bool) *lsp.SignatureHelp {
	doc := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	offset := doc.OffsetAt(position)
	open := callOpenParenBefore(parsed.Text, offset)
	if open < 0 {
		return nil
	}
	nameStart, nameEnd := identifierBefore(parsed.Text, open)
	if nameStart < 0 {
		return nil
	}
	name := parsed.Text[nameStart:nameEnd]
	key := strings.ToLower(name)
	qualified := false
	if ownerStart, ownerEnd := memberOwnerBeforeOffset(parsed.Text, nameStart); ownerStart >= 0 {
		key = strings.ToLower(parsed.Text[ownerStart:ownerEnd] + "." + name)
		qualified = true
	}
	var signature Signature
	var ok bool
	if includeUserDefined && !qualified {
		signature, ok = BuildSignatures(parsed)[strings.ToLower(name)]
	}
	if !ok {
		signature, ok = builtinSignature(key)
	}
	if !ok && standalone {
		signature, ok = standaloneBuiltinSignature(key)
	}
	if !ok {
		return nil
	}
	return &lsp.SignatureHelp{
		Signatures: []lsp.SignatureInformation{{
			Label:      signature.Label,
			Parameters: parameterInformation(signature.Parameters),
		}},
		ActiveSignature: 0,
		ActiveParameter: activeParameter(parsed.Text, open+1, offset),
	}
}

func standaloneBuiltinSignature(key string) (Signature, bool) {
	switch key {
	case "wscript.echo":
		return Signature{Name: "WScript.Echo", Label: "WScript.Echo(value)", Parameters: []Parameter{{Name: "value", Mode: "ByRef"}}}, true
	case "wscript.quit":
		return Signature{Name: "WScript.Quit", Label: "WScript.Quit(errorCode)", Parameters: []Parameter{{Name: "errorCode", Mode: "ByRef"}}}, true
	case "wscript.sleep":
		return Signature{Name: "WScript.Sleep", Label: "WScript.Sleep(milliseconds)", Parameters: []Parameter{{Name: "milliseconds", Mode: "ByRef"}}}, true
	case "wscript.createobject":
		return Signature{Name: "WScript.CreateObject", Label: "WScript.CreateObject(progId, prefix)", Parameters: []Parameter{{Name: "progId", Mode: "ByRef"}, {Name: "prefix", Mode: "ByRef"}}}, true
	case "wscript.getobject":
		return Signature{Name: "WScript.GetObject", Label: "WScript.GetObject(pathname, progId, prefix)", Parameters: []Parameter{{Name: "pathname", Mode: "ByRef"}, {Name: "progId", Mode: "ByRef"}, {Name: "prefix", Mode: "ByRef"}}}, true
	default:
		return Signature{}, false
	}
}

func WorkspaceSymbols(parsed *core.ParsedDocument, query string) []lsp.SymbolInformation {
	index := BuildSymbolIndex(parsed)
	query = strings.ToLower(strings.TrimSpace(query))
	symbols := make([]lsp.SymbolInformation, 0, len(index.Declarations))
	for _, symbol := range index.Declarations {
		if query != "" && !strings.Contains(strings.ToLower(symbol.Name), query) {
			continue
		}
		symbols = append(symbols, lsp.SymbolInformation{
			Name:     symbol.Name,
			Kind:     symbolKind(symbol.Kind),
			Location: lsp.Location{URI: parsed.URI, Range: symbol.Range},
		})
	}
	return symbols
}

func SignatureAt(parsed *core.ParsedDocument, position lsp.Position) (Signature, bool) {
	doc := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	word := WordAt(parsed.Text, doc.OffsetAt(position))
	if word == "" {
		return Signature{}, false
	}
	signature, ok := BuildSignatures(parsed)[strings.ToLower(word)]
	return signature, ok
}

func EnclosingSignature(parsed *core.ParsedDocument, position lsp.Position) (Signature, bool) {
	signatures := Signatures(parsed)
	var current Signature
	found := false
	for _, signature := range signatures {
		if signature.Range.Start.Line > position.Line {
			break
		}
		current = signature
		found = true
	}
	return current, found
}

func CallRanges(parsed *core.ParsedDocument, name string) []lsp.Range {
	postings := BuildReferenceShard(parsed).PostingsFor(name)
	ranges := make([]lsp.Range, 0, len(postings))
	for _, posting := range postings {
		if posting.HasRole(ReferenceRoleCall) {
			ranges = append(ranges, posting.Range)
		}
	}
	return ranges
}

// UnqualifiedCallRanges returns calls that are not qualified member accesses.
func UnqualifiedCallRanges(parsed *core.ParsedDocument, name string) []lsp.Range {
	shard := BuildReferenceShard(parsed)
	postings := shard.PostingsFor(name)
	globalResolutions := shard.GlobalResolutionsFor(name)
	ranges := make([]lsp.Range, 0, len(postings))
	for index, posting := range postings {
		if globalResolutions[index] && posting.HasRole(ReferenceRoleCall) {
			ranges = append(ranges, posting.Range)
		}
	}
	return ranges
}

// InlayHintOptions controls optional VBScript inlay hint categories.
type InlayHintOptions struct {
	ImplicitByRef  bool
	ParameterNames bool
}

func InlayHints(parsed *core.ParsedDocument, r lsp.Range) []lsp.InlayHint {
	return InlayHintsWithOptions(parsed, r, InlayHintOptions{ParameterNames: true})
}

// InlayHintsWithOptions returns VBScript inlay hints for the requested source range.
func InlayHintsWithOptions(parsed *core.ParsedDocument, r lsp.Range, options InlayHintOptions) []lsp.InlayHint {
	doc := core.NewTextDocument(parsed.URI, "classic-asp", 0, parsed.Text)
	startOffset := doc.OffsetAt(r.Start)
	endOffset := doc.OffsetAt(r.End)
	signatures := BuildSignatures(parsed)
	var hints []lsp.InlayHint
	if options.ImplicitByRef {
		hints = append(hints, implicitByRefInlayHints(parsed, doc, signatures, startOffset, endOffset)...)
	}
	if !options.ParameterNames {
		return hints
	}
	for _, region := range parsed.Regions {
		if region.Language != core.LanguageVBScript {
			continue
		}
		text := parsed.Text[region.ContentStart:region.ContentEnd]
		for _, span := range identifierSpans(text) {
			nameStart := region.ContentStart + span.Start
			nameEnd := region.ContentStart + span.End
			if ownerStart, _ := memberOwnerBeforeOffset(parsed.Text, nameStart); ownerStart >= 0 {
				continue
			}
			signature, ok := signatures[strings.ToLower(parsed.Text[nameStart:nameEnd])]
			if !ok || isSignatureDeclaration(signature, doc.PositionAt(nameStart)) {
				continue
			}
			open := nextNonSpace(parsed.Text, nameEnd)
			if open < 0 || parsed.Text[open] != '(' {
				continue
			}
			argStarts := argumentStarts(parsed.Text, open+1)
			for i, argStart := range argStarts {
				if i >= len(signature.Parameters) || argStart < startOffset || argStart > endOffset {
					continue
				}
				hints = append(hints, lsp.InlayHint{
					Position:     doc.PositionAt(argStart),
					Label:        signature.Parameters[i].Name + ":",
					Kind:         2,
					PaddingRight: lsp.BoolPtr(true),
				})
			}
		}
	}
	return hints
}

func implicitByRefInlayHints(parsed *core.ParsedDocument, doc *core.TextDocument, signatures map[string]Signature, startOffset, endOffset int) []lsp.InlayHint {
	hints := []lsp.InlayHint{}
	for _, signature := range signatures {
		declarationEnd := doc.OffsetAt(signature.Range.End)
		cursor := doc.OffsetAt(signature.NameRange.End)
		for _, parameter := range signature.Parameters {
			offset := findParameterNameOffset(parsed.Text, parameter.Name, cursor, declarationEnd)
			if offset < 0 {
				continue
			}
			if strings.EqualFold(parameter.Mode, "ByRef") && !hasExplicitByRef(parsed.Text, cursor, offset) && offset >= startOffset && offset <= endOffset {
				hints = append(hints, lsp.InlayHint{
					Position: doc.PositionAt(offset),
					Label:    "ByRef ",
					Kind:     1,
				})
			}
			cursor = offset + len(parameter.Name)
		}
	}
	return hints
}

func findParameterNameOffset(text, name string, start, end int) int {
	if start < 0 {
		start = 0
	}
	if end > len(text) {
		end = len(text)
	}
	if start >= end {
		return -1
	}
	for _, span := range identifierSpans(text[start:end]) {
		if strings.EqualFold(text[start+span.Start:start+span.End], name) {
			return start + span.Start
		}
	}
	return -1
}

func hasExplicitByRef(text string, start, end int) bool {
	if start < 0 {
		start = 0
	}
	if end > len(text) {
		end = len(text)
	}
	if start >= end {
		return false
	}
	return strings.Contains(strings.ToLower(text[start:end]), "byref")
}

func parseParameters(paramsText string) []Parameter {
	if strings.TrimSpace(paramsText) == "" {
		return nil
	}
	parts := splitParameterParts(paramsText)
	params := make([]Parameter, 0, len(parts))
	for _, part := range parts {
		mode := "ByRef"
		optional := false
		name := ""
		partTokens := Tokenize(part)
		for index, token := range partTokens {
			if isProcedureContinuationToken(partTokens, index) {
				continue
			}
			if token.Kind != "identifier" && token.Kind != "keyword" {
				continue
			}
			switch strings.ToLower(token.Text) {
			case "optional":
				optional = true
			case "byval":
				mode = "ByVal"
			case "byref":
				mode = "ByRef"
			case "paramarray":
				mode = "ByRef"
				optional = true
			default:
				if name == "" {
					name = strings.TrimSuffix(token.Text, "()")
				}
			}
		}
		if isValidIdentifier(name) {
			params = append(params, Parameter{Name: name, Mode: mode, Optional: optional})
		}
	}
	return params
}

func isProcedureContinuationToken(tokens []Token, index int) bool {
	if index < 0 || index >= len(tokens) || tokens[index].Text != "_" {
		return false
	}
	next := index + 1
	for next < len(tokens) && tokens[next].Kind == "whitespace" {
		next++
	}
	return next < len(tokens) && tokens[next].Kind == "newline"
}

func splitParameterParts(text string) []string {
	parts := make([]string, 0, 4)
	start := 0
	depth := 0
	for cursor := 0; cursor < len(text); cursor++ {
		switch text[cursor] {
		case '"':
			cursor = skipProcedureHeaderString(text, cursor) - 1
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				parts = append(parts, text[start:cursor])
				start = cursor + 1
			}
		}
	}
	return append(parts, text[start:])
}

func signatureLabel(name string, params []Parameter) string {
	parts := make([]string, 0, len(params))
	for _, param := range params {
		parts = append(parts, param.Mode+" "+param.Name)
	}
	return name + "(" + strings.Join(parts, ", ") + ")"
}

func parameterInformation(params []Parameter) []lsp.ParameterInformation {
	info := make([]lsp.ParameterInformation, 0, len(params))
	for _, param := range params {
		info = append(info, lsp.ParameterInformation{Label: param.Mode + " " + param.Name})
	}
	return info
}

func callOpenParenBefore(text string, offset int) int {
	if offset > len(text) {
		offset = len(text)
	}
	depth := 0
	for i := offset - 1; i >= 0; i-- {
		switch text[i] {
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

func identifierBefore(text string, offset int) (int, int) {
	i := offset
	for i > 0 && (text[i-1] == ' ' || text[i-1] == '\t') {
		i--
	}
	end := i
	for i > 0 && isIdent(text[i-1]) {
		i--
	}
	if i == end {
		return -1, -1
	}
	return i, end
}

func memberOwnerBeforeOffset(text string, offset int) (int, int) {
	i := offset
	for i > 0 && (text[i-1] == ' ' || text[i-1] == '\t') {
		i--
	}
	if i == 0 || text[i-1] != '.' {
		return -1, -1
	}
	return identifierBefore(text, i-1)
}

func activeParameter(text string, start, offset int) int {
	if offset > len(text) {
		offset = len(text)
	}
	active := 0
	depth := 0
	inString := false
	for i := start; i < offset; i++ {
		switch text[i] {
		case '"':
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

func builtinSignature(key string) (Signature, bool) {
	if spec, ok := builtinFunctionSpecForKey(key); ok {
		return Signature{Name: spec.Label, Label: spec.Signature, Parameters: builtinSignatureParameters(spec.Signature)}, true
	}
	switch key {
	case "array":
		return Signature{Name: "Array", Label: "Array(values)", Parameters: []Parameter{{Name: "values", Mode: "ByRef"}}}, true
	case "ascb":
		return Signature{Name: "AscB", Label: "AscB(string)", Parameters: []Parameter{{Name: "string", Mode: "ByRef"}}}, true
	case "ascw":
		return Signature{Name: "AscW", Label: "AscW(string)", Parameters: []Parameter{{Name: "string", Mode: "ByRef"}}}, true
	case "chrb":
		return Signature{Name: "ChrB", Label: "ChrB(charCode)", Parameters: []Parameter{{Name: "charCode", Mode: "ByRef"}}}, true
	case "chrw":
		return Signature{Name: "ChrW", Label: "ChrW(charCode)", Parameters: []Parameter{{Name: "charCode", Mode: "ByRef"}}}, true
	case "cstr":
		return Signature{Name: "CStr", Label: "CStr(value)", Parameters: []Parameter{{Name: "value", Mode: "ByRef"}}}, true
	case "cbool":
		return Signature{Name: "CBool", Label: "CBool(value)", Parameters: []Parameter{{Name: "value", Mode: "ByRef"}}}, true
	case "date":
		return Signature{Name: "Date", Label: "Date()", Parameters: nil}, true
	case "dateadd":
		return Signature{Name: "DateAdd", Label: "DateAdd(interval, number, date)", Parameters: []Parameter{{Name: "interval", Mode: "ByRef"}, {Name: "number", Mode: "ByRef"}, {Name: "date", Mode: "ByRef"}}}, true
	case "datediff":
		return Signature{Name: "DateDiff", Label: "DateDiff(interval, date1, date2, firstDayOfWeek, firstWeekOfYear)", Parameters: []Parameter{{Name: "interval", Mode: "ByRef"}, {Name: "date1", Mode: "ByRef"}, {Name: "date2", Mode: "ByRef"}, {Name: "firstDayOfWeek", Mode: "ByRef"}, {Name: "firstWeekOfYear", Mode: "ByRef"}}}, true
	case "datepart":
		return Signature{Name: "DatePart", Label: "DatePart(interval, date, firstDayOfWeek, firstWeekOfYear)", Parameters: []Parameter{{Name: "interval", Mode: "ByRef"}, {Name: "date", Mode: "ByRef"}, {Name: "firstDayOfWeek", Mode: "ByRef"}, {Name: "firstWeekOfYear", Mode: "ByRef"}}}, true
	case "day":
		return Signature{Name: "Day", Label: "Day(date)", Parameters: []Parameter{{Name: "date", Mode: "ByRef"}}}, true
	case "getobject":
		return Signature{Name: "GetObject", Label: "GetObject(pathname, class)", Parameters: []Parameter{{Name: "pathname", Mode: "ByRef"}, {Name: "class", Mode: "ByRef"}}}, true
	case "inputbox":
		return Signature{Name: "InputBox", Label: "InputBox(prompt, title, default, xpos, ypos, helpfile, context)", Parameters: []Parameter{{Name: "prompt", Mode: "ByRef"}, {Name: "title", Mode: "ByRef"}, {Name: "default", Mode: "ByRef"}, {Name: "xpos", Mode: "ByRef"}, {Name: "ypos", Mode: "ByRef"}, {Name: "helpfile", Mode: "ByRef"}, {Name: "context", Mode: "ByRef"}}}, true
	case "instrb":
		return Signature{Name: "InStrB", Label: "InStrB(start, string1, string2, compare)", Parameters: []Parameter{{Name: "start", Mode: "ByRef"}, {Name: "string1", Mode: "ByRef"}, {Name: "string2", Mode: "ByRef"}, {Name: "compare", Mode: "ByRef"}}}, true
	case "join":
		return Signature{Name: "Join", Label: "Join(list, delimiter)", Parameters: []Parameter{{Name: "list", Mode: "ByRef"}, {Name: "delimiter", Mode: "ByRef"}}}, true
	case "lcase":
		return Signature{Name: "LCase", Label: "LCase(string)", Parameters: []Parameter{{Name: "string", Mode: "ByRef"}}}, true
	case "leftb":
		return Signature{Name: "LeftB", Label: "LeftB(string, length)", Parameters: []Parameter{{Name: "string", Mode: "ByRef"}, {Name: "length", Mode: "ByRef"}}}, true
	case "len":
		return Signature{Name: "Len", Label: "Len(value)", Parameters: []Parameter{{Name: "value", Mode: "ByRef"}}}, true
	case "lenb":
		return Signature{Name: "LenB", Label: "LenB(value)", Parameters: []Parameter{{Name: "value", Mode: "ByRef"}}}, true
	case "midb":
		return Signature{Name: "MidB", Label: "MidB(string, start, length)", Parameters: []Parameter{{Name: "string", Mode: "ByRef"}, {Name: "start", Mode: "ByRef"}, {Name: "length", Mode: "ByRef"}}}, true
	case "msgbox":
		return Signature{Name: "MsgBox", Label: "MsgBox(prompt, buttons, title, helpfile, context)", Parameters: []Parameter{{Name: "prompt", Mode: "ByRef"}, {Name: "buttons", Mode: "ByRef"}, {Name: "title", Mode: "ByRef"}, {Name: "helpfile", Mode: "ByRef"}, {Name: "context", Mode: "ByRef"}}}, true
	case "now":
		return Signature{Name: "Now", Label: "Now()", Parameters: nil}, true
	case "replace":
		return Signature{Name: "Replace", Label: "Replace(expression, find, replaceWith, start, count, compare)", Parameters: []Parameter{{Name: "expression", Mode: "ByRef"}, {Name: "find", Mode: "ByRef"}, {Name: "replaceWith", Mode: "ByRef"}, {Name: "start", Mode: "ByRef"}, {Name: "count", Mode: "ByRef"}, {Name: "compare", Mode: "ByRef"}}}, true
	case "rightb":
		return Signature{Name: "RightB", Label: "RightB(string, length)", Parameters: []Parameter{{Name: "string", Mode: "ByRef"}, {Name: "length", Mode: "ByRef"}}}, true
	case "trim":
		return Signature{Name: "Trim", Label: "Trim(string)", Parameters: []Parameter{{Name: "string", Mode: "ByRef"}}}, true
	case "ltrim":
		return Signature{Name: "LTrim", Label: "LTrim(string)", Parameters: []Parameter{{Name: "string", Mode: "ByRef"}}}, true
	case "rtrim":
		return Signature{Name: "RTrim", Label: "RTrim(string)", Parameters: []Parameter{{Name: "string", Mode: "ByRef"}}}, true
	case "split":
		return Signature{Name: "Split", Label: "Split(expression, delimiter, count, compare)", Parameters: []Parameter{{Name: "expression", Mode: "ByRef"}, {Name: "delimiter", Mode: "ByRef"}, {Name: "count", Mode: "ByRef"}, {Name: "compare", Mode: "ByRef"}}}, true
	case "time":
		return Signature{Name: "Time", Label: "Time()", Parameters: nil}, true
	case "ubound":
		return Signature{Name: "UBound", Label: "UBound(array, dimension)", Parameters: []Parameter{{Name: "array", Mode: "ByRef"}, {Name: "dimension", Mode: "ByRef"}}}, true
	case "ucase":
		return Signature{Name: "UCase", Label: "UCase(string)", Parameters: []Parameter{{Name: "string", Mode: "ByRef"}}}, true
	case "month":
		return Signature{Name: "Month", Label: "Month(date)", Parameters: []Parameter{{Name: "date", Mode: "ByRef"}}}, true
	case "randomize":
		return Signature{Name: "Randomize", Label: "Randomize(number)", Parameters: []Parameter{{Name: "number", Mode: "ByRef"}}}, true
	case "year":
		return Signature{Name: "Year", Label: "Year(date)", Parameters: []Parameter{{Name: "date", Mode: "ByRef"}}}, true
	case "err.raise":
		return Signature{Name: "Err.Raise", Label: "Err.Raise(number, source, description, helpfile, helpcontext)", Parameters: []Parameter{{Name: "number", Mode: "ByRef"}, {Name: "source", Mode: "ByRef"}, {Name: "description", Mode: "ByRef"}, {Name: "helpfile", Mode: "ByRef"}, {Name: "helpcontext", Mode: "ByRef"}}}, true
	case "response.write":
		return Signature{Name: "Response.Write", Label: "Response.Write(value)", Parameters: []Parameter{{Name: "value", Mode: "ByRef"}}}, true
	default:
		return Signature{}, false
	}
}

func builtinSignatureParameters(signature string) []Parameter {
	start := strings.IndexByte(signature, '(')
	end := strings.LastIndexByte(signature, ')')
	if start < 0 || end < start {
		return nil
	}
	raw := strings.TrimSpace(signature[start+1 : end])
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	parameters := make([]Parameter, 0, len(parts))
	for _, part := range parts {
		name := strings.TrimSpace(part)
		if name == "" {
			continue
		}
		parameters = append(parameters, Parameter{Name: name, Mode: "ByRef"})
	}
	return parameters
}

func nextNonSpace(text string, offset int) int {
	for offset < len(text) {
		switch text[offset] {
		case ' ', '\t', '\r', '\n':
			offset++
		default:
			return offset
		}
	}
	return -1
}

func isSignatureDeclaration(signature Signature, position lsp.Position) bool {
	return positionInRange(position, signature.Range)
}

func positionInRange(position lsp.Position, r lsp.Range) bool {
	if position.Line < r.Start.Line || position.Line > r.End.Line {
		return false
	}
	if position.Line == r.Start.Line && position.Character < r.Start.Character {
		return false
	}
	if position.Line == r.End.Line && position.Character > r.End.Character {
		return false
	}
	return true
}

func argumentStarts(text string, offset int) []int {
	var starts []int
	depth := 0
	inString := false
	expectArgument := true
	for i := offset; i < len(text); i++ {
		if expectArgument && !inString && depth == 0 && text[i] != ' ' && text[i] != '\t' && text[i] != '\r' && text[i] != '\n' {
			starts = append(starts, i)
			expectArgument = false
		}
		switch text[i] {
		case '"':
			inString = !inString
		case '(':
			if !inString {
				depth++
			}
		case ')':
			if !inString {
				if depth == 0 {
					return starts
				}
				depth--
			}
		case ',':
			if !inString && depth == 0 {
				expectArgument = true
			}
		}
	}
	return starts
}

func symbolKind(kind string) int {
	switch kind {
	case "class":
		return 5
	case "const":
		return 14
	case "variable":
		return 13
	default:
		return 12
	}
}
