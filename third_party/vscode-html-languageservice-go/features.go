package htmlservice

import (
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	parallelSymbolChildrenThreshold = 512
	parallelSelectionThreshold      = 4096
	parallelLinkCandidateThreshold  = 4096
)

func FindDocumentHighlights(document *TextDocument, position Position, htmlDocument *HTMLDocument) []DocumentHighlight {
	offset := document.OffsetAt(position)
	node := htmlDocument.findNodeAtByte(offset)
	if node == nil || node.Tag == "" {
		return []DocumentHighlight{}
	}
	startRange := getTagNameRange(TokenTypeStartTag, document, node.startByte)
	var endRange *Range
	if node.endTagStartByte != nil {
		endRange = getTagNameRange(TokenTypeEndTag, document, *node.endTagStartByte)
	}
	if (startRange != nil && rangeCovers(*startRange, position)) || (endRange != nil && rangeCovers(*endRange, position)) {
		var result []DocumentHighlight
		if startRange != nil {
			result = append(result, DocumentHighlight{Kind: DocumentHighlightKindRead, Range: *startRange})
		}
		if endRange != nil {
			result = append(result, DocumentHighlight{Kind: DocumentHighlightKindRead, Range: *endRange})
		}
		return result
	}
	return []DocumentHighlight{}
}

func positionBeforeOrEqual(a, b Position) bool {
	return a.Line < b.Line || (a.Line == b.Line && a.Character <= b.Character)
}

func rangeCovers(r Range, p Position) bool {
	return positionBeforeOrEqual(r.Start, p) && positionBeforeOrEqual(p, r.End)
}

func getTagNameRange(tokenType TokenType, document *TextDocument, startOffset int) *Range {
	scanner := newScannerAtByte(document.GetText(), startOffset, ScannerStateWithinContent, false)
	token := scanner.Scan()
	for token != TokenTypeEOS && token != tokenType {
		token = scanner.Scan()
	}
	if token == TokenTypeEOS {
		return nil
	}
	r := NewRange(document.PositionAt(scanner.GetTokenByteOffset()), document.PositionAt(scanner.GetTokenByteEnd()))
	return &r
}

func FindDocumentSymbols(document *TextDocument, htmlDocument *HTMLDocument) []SymbolInformation {
	symbols := []SymbolInformation{}
	var walk func(symbol DocumentSymbol, parent *DocumentSymbol)
	walk = func(symbol DocumentSymbol, parent *DocumentSymbol) {
		container := ""
		if parent != nil {
			container = parent.Name
		}
		symbols = append(symbols, SymbolInformation{
			Name:          symbol.Name,
			Kind:          symbol.Kind,
			Location:      Location{URI: document.URI, Range: symbol.Range},
			ContainerName: container,
		})
		parentCopy := symbol
		for _, child := range symbol.Children {
			walk(child, &parentCopy)
		}
	}
	for _, symbol := range FindDocumentSymbols2(document, htmlDocument) {
		walk(symbol, nil)
	}
	return symbols
}

func FindDocumentSymbols2(document *TextDocument, htmlDocument *HTMLDocument) []DocumentSymbol {
	if len(htmlDocument.Roots) < parallelSymbolChildrenThreshold {
		symbols := make([]DocumentSymbol, 0, len(htmlDocument.Roots))
		for _, node := range htmlDocument.Roots {
			symbols = append(symbols, fileSymbol(document, node))
		}
		return symbols
	}
	return parallelMapOrdered(htmlDocument.Roots, parallelSymbolChildrenThreshold, func(_ int, node *Node) DocumentSymbol {
		return fileSymbol(document, node)
	})
}

func fileSymbol(document *TextDocument, node *Node) DocumentSymbol {
	r := NewRange(document.PositionAt(node.startByte), document.PositionAt(node.endByte))
	symbol := DocumentSymbol{Name: nodeToName(node), Kind: SymbolKindField, Range: r, SelectionRange: r}
	if len(node.Children) > 0 {
		if len(node.Children) < parallelSymbolChildrenThreshold {
			symbol.Children = make([]DocumentSymbol, 0, len(node.Children))
			for _, child := range node.Children {
				symbol.Children = append(symbol.Children, fileSymbol(document, child))
			}
			return symbol
		}
		symbol.Children = parallelMapOrdered(node.Children, parallelSymbolChildrenThreshold, func(_ int, child *Node) DocumentSymbol {
			return fileSymbol(document, child)
		})
	}
	return symbol
}

func nodeToName(node *Node) string {
	var builder strings.Builder
	builder.WriteString(node.Tag)
	if node.Attributes != nil {
		if id := node.Attributes["id"]; id != nil {
			builder.WriteByte('#')
			builder.WriteString(stripQuotesOnly(*id))
		}
		if classes := node.Attributes["class"]; classes != nil {
			for _, className := range splitJSWhitespaceRuns(stripQuotesOnly(*classes)) {
				builder.WriteByte('.')
				builder.WriteString(className)
			}
		}
	}
	name := builder.String()
	if name == "" {
		return "?"
	}
	return name
}

func stripQuotesOnly(value string) string {
	if !strings.ContainsAny(value, `"'`) {
		return value
	}
	var builder strings.Builder
	builder.Grow(len(value))
	for i := 0; i < len(value); i++ {
		if value[i] != '"' && value[i] != '\'' {
			builder.WriteByte(value[i])
		}
	}
	return builder.String()
}

func DoRename(document *TextDocument, position Position, newName string, htmlDocument *HTMLDocument) *WorkspaceEdit {
	offset := document.OffsetAt(position)
	node := htmlDocument.findNodeAtByte(offset)
	if node == nil || node.Tag == "" || !isWithinTagRange(node, offset, node.Tag) {
		return nil
	}
	edits := make([]TextEdit, 0, 2)
	edits = append(edits, NewTextEdit(document.rangeAtByteOffsets(node.startByte+1, node.startByte+1+len(node.Tag)), newName))
	if node.endTagStartByte != nil {
		edits = append(edits, NewTextEdit(document.rangeAtByteOffsets(*node.endTagStartByte+2, *node.endTagStartByte+2+len(node.Tag)), newName))
	}
	return &WorkspaceEdit{Changes: map[DocumentUri][]TextEdit{document.URI: edits}}
}

func isWithinTagRange(node *Node, offset int, tag string) bool {
	if node.endTagStartByte != nil && *node.endTagStartByte+2 <= offset && offset <= *node.endTagStartByte+2+len(tag) {
		return true
	}
	return node.startByte+1 <= offset && offset <= node.startByte+1+len(tag)
}

func FindLinkedEditingRanges(document *TextDocument, position Position, htmlDocument *HTMLDocument) []Range {
	offset := document.OffsetAt(position)
	node := htmlDocument.findNodeAtByte(offset)
	if node == nil || node.endTagStartByte == nil {
		return nil
	}
	tagLength := len(node.Tag)
	if (node.startByte+1 <= offset && offset <= node.startByte+1+tagLength) ||
		(*node.endTagStartByte+2 <= offset && offset <= *node.endTagStartByte+2+tagLength) {
		return []Range{
			NewRange(document.PositionAt(node.startByte+1), document.PositionAt(node.startByte+1+tagLength)),
			NewRange(document.PositionAt(*node.endTagStartByte+2), document.PositionAt(*node.endTagStartByte+2+tagLength)),
		}
	}
	return nil
}

func FindMatchingTagPosition(document *TextDocument, position Position, htmlDocument *HTMLDocument) *Position {
	offset := document.OffsetAt(position)
	node := htmlDocument.findNodeAtByte(offset)
	if node == nil || node.Tag == "" || node.endTagStartByte == nil {
		return nil
	}
	if node.startByte+1 <= offset && offset <= node.startByte+1+len(node.Tag) {
		p := document.PositionAt((offset - 1 - node.startByte) + *node.endTagStartByte + 2)
		return &p
	}
	if *node.endTagStartByte+2 <= offset && offset <= *node.endTagStartByte+2+len(node.Tag) {
		p := document.PositionAt((offset - 2 - *node.endTagStartByte) + node.startByte + 1)
		return &p
	}
	return nil
}

func GetFoldingRanges(dataManager *HTMLDataManager, document *TextDocument, rangeLimit int) []FoldingRange {
	scanner := CreateScanner(document.GetText())
	token := scanner.Scan()
	ranges := []FoldingRange{}
	var stack []struct {
		startLine int
		tagName   string
	}
	lastTagName := ""
	prevStart := -1
	var voidElementSet map[string]bool
	addRange := func(r FoldingRange) {
		ranges = append(ranges, r)
		prevStart = r.StartLine
	}
	for token != TokenTypeEOS {
		switch token {
		case TokenTypeStartTag:
			tagName := scanner.GetTokenText()
			startLine := document.lineAtByteOffset(scanner.GetTokenByteOffset())
			stack = append(stack, struct {
				startLine int
				tagName   string
			}{startLine, tagName})
			lastTagName = tagName
		case TokenTypeEndTag:
			lastTagName = scanner.GetTokenText()
		case TokenTypeStartTagClose:
			if lastTagName == "" {
				break
			}
			if voidElementSet == nil {
				voidElementSet = dataManager.getVoidElementSet(document.LanguageID)
			}
			if !voidElementSet[lastTagName] {
				break
			}
			fallthrough
		case TokenTypeEndTagClose, TokenTypeStartTagSelfClose:
			i := len(stack) - 1
			for i >= 0 && stack[i].tagName != lastTagName {
				i--
			}
			if i >= 0 {
				stackElement := stack[i]
				stack = stack[:i]
				line := document.lineAtByteOffset(scanner.GetTokenByteOffset())
				startLine := stackElement.startLine
				endLine := line - 1
				if endLine > startLine && prevStart != startLine {
					addRange(FoldingRange{StartLine: startLine, EndLine: endLine})
				}
			}
		case TokenTypeComment:
			startLine := document.lineAtByteOffset(scanner.GetTokenByteOffset())
			text := scanner.GetTokenText()
			if isFoldingRegionStart(text) {
				stack = append(stack, struct {
					startLine int
					tagName   string
				}{startLine, ""})
			} else if foldingRegionEndRE.MatchString(text) {
				i := len(stack) - 1
				for i >= 0 && stack[i].tagName != "" {
					i--
				}
				if i >= 0 {
					stackElement := stack[i]
					stack = stack[:i]
					if startLine > stackElement.startLine && prevStart != stackElement.startLine {
						addRange(FoldingRange{StartLine: stackElement.startLine, EndLine: startLine, Kind: FoldingRangeKindRegion})
					}
				}
			} else {
				endLine := document.lineAtByteOffset(scanner.GetTokenByteOffset() + scanner.GetTokenByteLength())
				if startLine < endLine {
					addRange(FoldingRange{StartLine: startLine, EndLine: endLine, Kind: FoldingRangeKindComment})
				}
			}
		}
		token = scanner.Scan()
	}
	if rangeLimit != 0 && len(ranges) > rangeLimit {
		return limitFoldingRanges(ranges, rangeLimit)
	}
	return ranges
}

var (
	foldingRegionStartRE = regexp.MustCompile(`^#region\b`)
	foldingRegionEndRE   = regexp.MustCompile(`endregion\b`)
)

func isFoldingRegionStart(text string) bool {
	return foldingRegionStartRE.MatchString(trimLeftJSWhitespace(text))
}

func limitFoldingRanges(ranges []FoldingRange, rangeLimit int) []FoldingRange {
	sort.Slice(ranges, func(i, j int) bool {
		if ranges[i].StartLine == ranges[j].StartLine {
			return ranges[i].EndLine < ranges[j].EndLine
		}
		return ranges[i].StartLine < ranges[j].StartLine
	})
	var top *FoldingRange
	var previous []*FoldingRange
	nestingLevels := make([]int, len(ranges))
	for i := range nestingLevels {
		nestingLevels[i] = -1
	}
	nestingLevelCounts := []int{}
	setNestingLevel := func(index, level int) {
		nestingLevels[index] = level
		if level < 30 {
			for len(nestingLevelCounts) <= level {
				nestingLevelCounts = append(nestingLevelCounts, 0)
			}
			nestingLevelCounts[level]++
		}
	}
	for i := range ranges {
		entry := &ranges[i]
		if top == nil {
			top = entry
			setNestingLevel(i, 0)
			continue
		}
		if entry.StartLine > top.StartLine {
			if entry.EndLine <= top.EndLine {
				previous = append(previous, top)
				top = entry
				setNestingLevel(i, len(previous))
			} else if entry.StartLine > top.EndLine {
				for {
					if len(previous) == 0 {
						top = nil
					} else {
						top = previous[len(previous)-1]
						previous = previous[:len(previous)-1]
					}
					if top == nil || entry.StartLine <= top.EndLine {
						break
					}
				}
				if top != nil {
					previous = append(previous, top)
				}
				top = entry
				setNestingLevel(i, len(previous))
			}
		}
	}
	entries := 0
	maxLevel := 0
	for i, count := range nestingLevelCounts {
		if count == 0 {
			continue
		}
		if count+entries > rangeLimit {
			maxLevel = i
			break
		}
		entries += count
	}
	var result []FoldingRange
	for i, r := range ranges {
		level := nestingLevels[i]
		if level >= 0 {
			if level < maxLevel || (level == maxLevel && entries < rangeLimit) {
				result = append(result, r)
				if level == maxLevel {
					entries++
				}
			}
		}
	}
	return result
}

func GetSelectionRanges(parser *HTMLParser, document *TextDocument, positions []Position) []SelectionRange {
	htmlDocument := parser.ParseDocument(document)
	if len(positions) < parallelSelectionThreshold {
		result := make([]SelectionRange, 0, len(positions))
		for _, position := range positions {
			result = append(result, getSelectionRange(document, position, htmlDocument))
		}
		return result
	}
	return parallelMapOrdered(positions, parallelSelectionThreshold, func(_ int, position Position) SelectionRange {
		return getSelectionRange(document, position, htmlDocument)
	})
}

func getSelectionRange(document *TextDocument, position Position, htmlDocument *HTMLDocument) SelectionRange {
	ranges := getApplicableRanges(document, position, htmlDocument)
	var current *SelectionRange
	prevSet := false
	var prev [2]int
	for i := len(ranges) - 1; i >= 0; i-- {
		r := ranges[i]
		if !prevSet || r != prev {
			next := &SelectionRange{Range: NewRange(document.PositionAt(r[0]), document.PositionAt(r[1]))}
			if current != nil {
				next.Parent = current
			}
			current = next
		}
		prev = r
		prevSet = true
	}
	if current == nil {
		return SelectionRange{Range: NewRange(position, position)}
	}
	return *current
}

func getApplicableRanges(document *TextDocument, position Position, htmlDoc *HTMLDocument) [][2]int {
	currOffset := document.OffsetAt(position)
	currNode := htmlDoc.findNodeAtByte(currOffset)
	if currNode == nil {
		return nil
	}
	result := getAllParentTagRanges(currNode)
	if currNode.startTagEndByte != nil && currNode.endTagStartByte == nil {
		if *currNode.startTagEndByte != currNode.endByte {
			return [][2]int{{currNode.startByte, currNode.endByte}}
		}
		text := document.GetText()
		closeText := text[*currNode.startTagEndByte-2 : *currNode.startTagEndByte]
		if closeText == "/>" {
			result = prependRange(result, [2]int{currNode.startByte + 1, *currNode.startTagEndByte - 2})
		} else {
			result = prependRange(result, [2]int{currNode.startByte + 1, *currNode.startTagEndByte - 1})
		}
		return prependRanges(result, getAttributeLevelRanges(document, currNode, currOffset))
	}
	if currNode.startTagEndByte == nil || currNode.endTagStartByte == nil {
		return result
	}
	result = prependRange(result, [2]int{currNode.startByte, currNode.endByte})
	if currNode.startByte < currOffset && currOffset < *currNode.startTagEndByte {
		result = prependRange(result, [2]int{currNode.startByte + 1, *currNode.startTagEndByte - 1})
		return prependRanges(result, getAttributeLevelRanges(document, currNode, currOffset))
	}
	if *currNode.startTagEndByte <= currOffset && currOffset <= *currNode.endTagStartByte {
		return prependRange(result, [2]int{*currNode.startTagEndByte, *currNode.endTagStartByte})
	}
	if currOffset >= *currNode.endTagStartByte+2 {
		return prependRange(result, [2]int{*currNode.endTagStartByte + 2, currNode.endByte - 1})
	}
	return result
}

func prependRange(ranges [][2]int, prefix [2]int) [][2]int {
	result := make([][2]int, 0, len(ranges)+1)
	result = append(result, prefix)
	return append(result, ranges...)
}

func prependRanges(ranges, prefix [][2]int) [][2]int {
	if len(prefix) == 0 {
		return ranges
	}
	result := make([][2]int, 0, len(prefix)+len(ranges))
	result = append(result, prefix...)
	return append(result, ranges...)
}

func getAllParentTagRanges(initial *Node) [][2]int {
	var result [][2]int
	curr := initial
	for curr != nil && curr.Parent != nil {
		curr = curr.Parent
		result = append(result, getNodeRanges(curr)...)
	}
	return result
}

func getNodeRanges(n *Node) [][2]int {
	if n.startTagEndByte != nil && n.endTagStartByte != nil && *n.startTagEndByte < *n.endTagStartByte {
		return [][2]int{{*n.startTagEndByte, *n.endTagStartByte}, {n.startByte, n.endByte}}
	}
	return [][2]int{{n.startByte, n.endByte}}
}

func getAttributeLevelRanges(document *TextDocument, currNode *Node, currOffset int) [][2]int {
	currNodeText := document.GetText()[currNode.startByte:currNode.endByte]
	relativeOffset := currOffset - currNode.startByte
	scanner := CreateScanner(currNodeText)
	token := scanner.Scan()
	var result [][2]int
	isInsideAttribute := false
	attrStart := -1
	for token != TokenTypeEOS {
		switch token {
		case TokenTypeAttributeName:
			if relativeOffset < scanner.GetTokenByteOffset() {
				isInsideAttribute = false
				break
			}
			if relativeOffset <= scanner.GetTokenByteEnd() {
				result = append([][2]int{{scanner.GetTokenByteOffset(), scanner.GetTokenByteEnd()}}, result...)
			}
			isInsideAttribute = true
			attrStart = scanner.GetTokenByteOffset()
		case TokenTypeAttributeValue:
			if !isInsideAttribute {
				break
			}
			valueText := scanner.GetTokenText()
			if relativeOffset < scanner.GetTokenByteOffset() {
				result = append(result, [2]int{attrStart, scanner.GetTokenByteEnd()})
				break
			}
			if relativeOffset >= scanner.GetTokenByteOffset() && relativeOffset <= scanner.GetTokenByteEnd() {
				result = append([][2]int{{scanner.GetTokenByteOffset(), scanner.GetTokenByteEnd()}}, result...)
				if len(valueText) >= 2 && ((valueText[0] == '"' && valueText[len(valueText)-1] == '"') || (valueText[0] == '\'' && valueText[len(valueText)-1] == '\'')) {
					if relativeOffset >= scanner.GetTokenByteOffset()+1 && relativeOffset <= scanner.GetTokenByteEnd()-1 {
						result = append([][2]int{{scanner.GetTokenByteOffset() + 1, scanner.GetTokenByteEnd() - 1}}, result...)
					}
				}
				result = append(result, [2]int{attrStart, scanner.GetTokenByteEnd()})
			}
		}
		token = scanner.Scan()
	}
	for i := range result {
		result[i][0] += currNode.startByte
		result[i][1] += currNode.startByte
	}
	return result
}

func FindDocumentLinks(dataManager *HTMLDataManager, document *TextDocument, documentContext DocumentContext) []DocumentLink {
	candidates := []documentLinkCandidate{}
	scanner := CreateScanner(document.GetText())
	token := scanner.Scan()
	lastAttributeName := ""
	lastTagName := ""
	afterBase := false
	base := ""
	baseDefined := false
	idLocations := map[string]int{}
	for token != TokenTypeEOS {
		switch token {
		case TokenTypeStartTag:
			lastTagName = strings.ToLower(scanner.GetTokenText())
			if !baseDefined || base == "" {
				afterBase = lastTagName == "base"
			}
		case TokenTypeAttributeName:
			lastAttributeName = strings.ToLower(scanner.GetTokenText())
		case TokenTypeAttributeValue:
			if lastTagName != "" && lastAttributeName != "" && dataManager.IsPathAttribute(lastTagName, lastAttributeName) {
				attributeValue := scanner.GetTokenText()
				if !afterBase {
					if candidate := createDocumentLinkCandidate(document, documentContext, attributeValue, scanner.GetTokenByteOffset(), scanner.GetTokenByteEnd(), base); candidate != nil {
						candidates = append(candidates, *candidate)
					}
				}
				if afterBase && !baseDefined {
					rawBase := normalizeRef(attributeValue)
					if rawBase != "" && documentContext != nil {
						if resolved, ok := documentContext.ResolveReference(rawBase, document.URI); ok {
							base = resolved
							baseDefined = true
						}
					} else {
						base = rawBase
						baseDefined = true
					}
				}
				afterBase = false
				lastAttributeName = ""
			} else if lastAttributeName == "id" {
				idLocations[normalizeRef(scanner.GetTokenText())] = scanner.GetTokenByteOffset()
			}
		}
		token = scanner.Scan()
	}
	links := finishDocumentLinkCandidates(document, candidates)
	localWithHash := document.URI + "#"
	for i := range links {
		if strings.HasPrefix(links[i].Target, localWithHash) {
			target := strings.TrimPrefix(links[i].Target, localWithHash)
			if target == "__proto__" {
				links[i].Target = localWithHash + "1,NaN"
			} else if offset, ok := idLocations[target]; ok {
				pos := document.PositionAt(offset)
				links[i].Target = localWithHash + intToString(pos.Line+1) + "," + intToString(pos.Character+1)
			} else if isJSObjectPrototypeName(target) {
				links[i].Target = localWithHash + "1,NaN"
			} else {
				links[i].Target = document.URI
			}
		}
	}
	return links
}

func isJSObjectPrototypeName(name string) bool {
	switch name {
	case "__defineGetter__", "__defineSetter__", "__lookupGetter__", "__lookupSetter__",
		"constructor", "hasOwnProperty", "isPrototypeOf", "propertyIsEnumerable",
		"toLocaleString", "toString", "valueOf", "__proto__":
		return true
	default:
		return false
	}
}

func normalizeRef(ref string) string {
	return stringsTrimQuotes(ref)
}

var validRefPattern = regexp.MustCompile(`\b(w[\w\d+.-]*://)?[^\s()<>]+(?:\([\w\d]+\)|([^[:punct:]\s]|/?))`)
var (
	javascriptSchemePattern = regexp.MustCompile(`(?i)^\s*javascript:`)
	uriSchemePattern        = regexp.MustCompile(`^(\w[\w\d+.-]*):`)
	uriProtocolPattern      = regexp.MustCompile(`^\w[\w\d+.-]*:`)
)

func validateRef(ref, languageID string) bool {
	if ref == "" {
		return false
	}
	if languageID == "handlebars" && (strings.Contains(ref, "{{") || strings.Contains(ref, "}}")) {
		return false
	}
	return validRefPattern.MatchString(ref)
}

type documentLinkCandidate struct {
	startOffset  int
	endOffset    int
	workspaceURL string
}

type documentLinkResult struct {
	link DocumentLink
	ok   bool
}

func createDocumentLinkCandidate(document *TextDocument, documentContext DocumentContext, attributeValue string, startOffset, endOffset int, base string) *documentLinkCandidate {
	tokenContent := normalizeRef(attributeValue)
	if !validateRef(tokenContent, document.LanguageID) {
		return nil
	}
	if len(tokenContent) < len(attributeValue) {
		startOffset++
		endOffset--
	}
	workspaceURL, ok := getWorkspaceURL(document.URI, tokenContent, documentContext, base)
	if !ok {
		return nil
	}
	return &documentLinkCandidate{startOffset: startOffset, endOffset: endOffset, workspaceURL: workspaceURL}
}

func finishDocumentLinkCandidates(document *TextDocument, candidates []documentLinkCandidate) []DocumentLink {
	if len(candidates) < parallelLinkCandidateThreshold {
		links := make([]DocumentLink, 0, len(candidates))
		for _, candidate := range candidates {
			if result := finishDocumentLinkCandidate(document, candidate); result.ok {
				links = append(links, result.link)
			}
		}
		return links
	}
	results := parallelMapOrdered(candidates, parallelLinkCandidateThreshold, func(_ int, candidate documentLinkCandidate) documentLinkResult {
		return finishDocumentLinkCandidate(document, candidate)
	})
	links := make([]DocumentLink, 0, len(results))
	for _, result := range results {
		if result.ok {
			links = append(links, result.link)
		}
	}
	return links
}

func finishDocumentLinkCandidate(document *TextDocument, candidate documentLinkCandidate) documentLinkResult {
	target, ok := validateAndCleanURI(candidate.workspaceURL, document)
	if !ok {
		return documentLinkResult{}
	}
	return documentLinkResult{
		link: DocumentLink{Range: document.rangeAtByteOffsets(candidate.startOffset, candidate.endOffset), Target: target},
		ok:   true,
	}
}

func getWorkspaceURL(documentURI, tokenContent string, documentContext DocumentContext, base string) (string, bool) {
	if javascriptSchemePattern.MatchString(tokenContent) || strings.ContainsAny(tokenContent, "\n\r") {
		return "", false
	}
	tokenContent = trimLeftJSWhitespace(tokenContent)
	if match := uriSchemePattern.FindStringSubmatch(tokenContent); len(match) > 0 {
		schema := strings.ToLower(match[1])
		if schema == "http" || schema == "https" || schema == "file" {
			return tokenContent, true
		}
		return "", false
	}
	if strings.HasPrefix(tokenContent, "#") {
		return documentURI + tokenContent, true
	}
	if strings.HasPrefix(tokenContent, "//") {
		scheme := "http"
		if strings.HasPrefix(documentURI, "https://") {
			scheme = "https"
		}
		return scheme + ":" + trimLeftJSWhitespace(tokenContent), true
	}
	if documentContext != nil {
		if base == "" {
			base = documentURI
		}
		if resolved, ok := documentContext.ResolveReference(tokenContent, base); ok {
			if resolved == "" {
				return "", false
			}
			return resolved, true
		}
		return "", false
	}
	return tokenContent, true
}

func validateAndCleanURI(uriStr string, document *TextDocument) (string, bool) {
	if !startsWithIgnoreCase(uriStr, "file:") {
		if document != nil && !hasURIProtocol(uriStr) && hasNonEmptyQueryOrFragment(uriStr) {
			return cleanRelativeURIWithQueryOrFragment(uriStr, document.URI), true
		}
		return uriStr, true
	}
	if strings.HasPrefix(uriStr, "file://///") {
		return "", true
	}
	parsed, err := url.Parse(uriStr)
	if err != nil {
		return uriStr, true
	}
	cleaned := false
	if parsed.Scheme == "file" {
		if parsed.RawQuery != "" {
			uriStr = stripURIQuery(uriStr)
			cleaned = true
		}
		if parsed.Fragment != "" && !isLocalDocumentFragment(uriStr, document.URI) {
			uriStr = stripURIFragment(uriStr)
			cleaned = true
		}
	}
	if cleaned {
		uriStr = normalizeFileDriveLetter(uriStr)
	}
	return uriStr, true
}

func hasURIProtocol(uriStr string) bool {
	return uriProtocolPattern.MatchString(uriStr)
}

func hasNonEmptyQueryOrFragment(uriStr string) bool {
	query := strings.Index(uriStr, "?")
	fragment := strings.Index(uriStr, "#")
	if query >= 0 {
		queryEnd := len(uriStr)
		if fragment > query {
			queryEnd = fragment
		}
		if queryEnd > query+1 {
			return true
		}
	}
	return fragment >= 0 && len(uriStr) > fragment+1
}

func cleanRelativeURIWithQueryOrFragment(uriStr, documentURI string) string {
	if strings.HasPrefix(uriStr, "#") {
		return documentURI
	}
	cleaned := stripURIFragment(stripURIQuery(uriStr))
	if cleaned == "" {
		return "file:///"
	}
	return "file:///" + strings.TrimLeft(cleaned, "/")
}

func normalizeFileDriveLetter(uriStr string) string {
	if len(uriStr) > len("file:///C:/") && strings.HasPrefix(uriStr, "file:///") && uriStr[9] == ':' && uriStr[10] == '/' {
		ch := uriStr[8]
		if 'A' <= ch && ch <= 'Z' {
			return uriStr[:8] + string(ch+'a'-'A') + uriStr[9:]
		}
	}
	return uriStr
}

func stripURIQuery(uriStr string) string {
	query := strings.Index(uriStr, "?")
	if query < 0 {
		return uriStr
	}
	if fragment := strings.Index(uriStr[query+1:], "#"); fragment >= 0 {
		return uriStr[:query] + uriStr[query+1+fragment:]
	}
	return uriStr[:query]
}

func stripURIFragment(uriStr string) string {
	if fragment := strings.Index(uriStr, "#"); fragment >= 0 {
		return uriStr[:fragment]
	}
	return uriStr
}

func isLocalDocumentFragment(uriStr, documentURI string) bool {
	return strings.HasPrefix(uriStr, documentURI) && len(uriStr) > len(documentURI) && uriStr[len(documentURI)] == '#'
}

func intToString(v int) string {
	return strconv.Itoa(v)
}
