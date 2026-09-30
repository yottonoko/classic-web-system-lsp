package vbscript

import (
	"sort"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

// SQLString is the content of one VBScript string literal that holds SQL text.
// Start and End are byte offsets in the document and exclude the quotes.
type SQLString struct {
	Start int
	End   int
	// InQuoteAtStart reports that a SQL '...' literal opened by an earlier
	// fragment of the same statement is still open where this fragment starts.
	InQuoteAtStart bool
}

// SQLWordKind classifies a word inside SQL text.
type SQLWordKind int

const (
	SQLWordIdentifier SQLWordKind = iota
	SQLWordKeyword
	SQLWordFunction
)

// SQLWord is one word of SQL text with document byte offsets.
type SQLWord struct {
	Start int
	End   int
	Kind  SQLWordKind
}

// sqlSegment is a statement, or one branch of an inline If, classified as SQL.
type sqlSegment struct {
	tokens  []Token
	strings []SQLString
}

var sqlLeadWords = wordSet(
	"select", "insert", "update", "delete", "exec", "execute", "with", "merge", "create", "alter", "drop", "truncate",
	"from", "where", "and", "or", "order", "group", "having", "inner", "left", "right", "full", "cross", "join", "on",
	"union", "set", "values", "into", "limit",
)

// sqlStatementWords start a complete statement; the rest of sqlLeadWords only
// continue one and need a SQL-named target to count.
var sqlStatementWords = wordSet("select", "insert", "update", "delete", "exec", "execute", "with", "merge", "create", "alter", "drop", "truncate")

var sqlKeywords = wordSet(
	"add", "all", "alter", "and", "any", "as", "asc", "begin", "between", "bigint", "bit", "by", "case", "char", "check",
	"column", "commit", "constraint", "create", "cross", "database", "date", "datetime", "decimal", "declare", "default",
	"delete", "desc", "distinct", "drop", "else", "end", "escape", "except", "exec", "execute", "exists", "fetch", "float",
	"foreign", "from", "full", "function", "group", "having", "identity", "if", "in", "index", "inner", "insert", "int",
	"integer", "intersect", "into", "is", "join", "key", "left", "like", "limit", "merge", "money", "nchar", "next",
	"nolock", "not", "ntext", "null", "numeric", "nvarchar", "offset", "on", "only", "or", "order", "outer", "output",
	"over", "partition", "percent", "primary", "proc", "procedure", "real", "references", "return", "right", "rollback",
	"rows", "select", "set", "smallint", "some", "table", "text", "then", "ties", "tinyint", "top", "tran", "transaction",
	"trigger", "truncate", "union", "unique", "update", "using", "values", "varchar", "view", "when", "where", "while",
	"with",
)

var sqlFunctions = wordSet(
	"abs", "avg", "cast", "charindex", "coalesce", "concat", "convert", "count", "dateadd", "datediff", "datepart", "day",
	"dense_rank", "format", "getdate", "ifnull", "iif", "isdate", "isnull", "isnumeric", "left", "len", "lower", "ltrim",
	"max", "min", "month", "newid", "now", "nullif", "nvl", "nz", "rank", "replace", "right", "round", "row_number",
	"rtrim", "scope_identity", "str", "substring", "sum", "to_char", "to_date", "trim", "upper", "year",
)

// SQLKeywordNames returns the SQL keywords offered by completion, in lower case.
func SQLKeywordNames() []string {
	return sortedWords(sqlKeywords)
}

// SQLFunctionNames returns the SQL functions offered by completion, in lower case.
func SQLFunctionNames() []string {
	return sortedWords(sqlFunctions)
}

func wordSet(words ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(words))
	for _, word := range words {
		set[word] = struct{}{}
	}
	return set
}

func sortedWords(set map[string]struct{}) []string {
	words := make([]string, 0, len(set))
	for word := range set {
		words = append(words, word)
	}
	sort.Strings(words)
	return words
}

// SQLStrings returns the string literals that hold SQL text, in source order.
// Detection is conservative: a literal qualifies only when it is shaped like a
// SQL statement or the statement assigns to or executes SQL.
func SQLStrings(parsed *core.ParsedDocument) []SQLString {
	var result []SQLString
	for _, segment := range sqlSegments(parsed) {
		result = append(result, segment.strings...)
	}
	return result
}

// SQLStringAt returns the SQL string literal whose content contains offset.
func SQLStringAt(parsed *core.ParsedDocument, offset int) (SQLString, bool) {
	for _, candidate := range SQLStrings(parsed) {
		if offset >= candidate.Start && offset <= candidate.End {
			return candidate, true
		}
	}
	return SQLString{}, false
}

// SQLWords returns the keywords, functions, and identifiers of a SQL string.
// Quoted SQL literals and comments are skipped.
func SQLWords(text string, fragment SQLString) []SQLWord {
	words, _ := scanSQLWords(text, fragment.Start, fragment.End, fragment.InQuoteAtStart)
	return words
}

func sqlSegments(parsed *core.ParsedDocument) []sqlSegment {
	if parsed == nil {
		return nil
	}
	var segments []sqlSegment
	for _, statement := range vbStatements(parsed) {
		if !statementHasString(statement.Tokens) {
			continue
		}
		for _, tokens := range splitInlineBranches(statement.Tokens) {
			fragments := classifySQLSegment(parsed.Text, tokens)
			if len(fragments) == 0 {
				continue
			}
			segments = append(segments, sqlSegment{tokens: tokens, strings: fragments})
		}
	}
	return segments
}

func statementHasString(tokens []Token) bool {
	for _, token := range tokens {
		if token.Kind == "string" {
			return true
		}
	}
	return false
}

// splitInlineBranches separates "If cond Then a Else b" into its condition and
// branches so a Request value in the condition is not tied to SQL in a branch.
func splitInlineBranches(tokens []Token) [][]Token {
	var segments [][]Token
	start := 0
	for index, token := range tokens {
		if token.Kind != "keyword" {
			continue
		}
		switch strings.ToLower(token.Text) {
		case "then", "else", "elseif":
			if start < index {
				segments = append(segments, tokens[start:index])
			}
			start = index + 1
		}
	}
	if start < len(tokens) {
		segments = append(segments, tokens[start:])
	}
	return segments
}

func classifySQLSegment(text string, tokens []Token) []SQLString {
	var literals []Token
	for _, token := range tokens {
		if token.Kind == "string" {
			literals = append(literals, token)
		}
	}
	if len(literals) == 0 {
		return nil
	}
	contents := make([]string, len(literals))
	for index, literal := range literals {
		start, end := stringContentBounds(text, literal)
		contents[index] = text[start:end]
	}
	if !sqlShapedLiterals(contents) || writesResponse(tokens) {
		target, equal := assignmentTarget(tokens)
		named := isSQLName(target)
		leads := false
		for _, content := range contents {
			if _, ok := sqlLeadWords[firstSQLWord(content)]; ok {
				leads = true
				break
			}
		}
		_, starts := sqlStatementWords[firstSQLWord(contents[0])]
		appends := named && equal >= 0 && equal+2 < len(tokens) &&
			strings.EqualFold(tokens[equal+1].Text, target) && tokens[equal+2].Text == "&"
		if !(named && leads) && !(starts && executesSQL(tokens)) && !appends {
			return nil
		}
	}
	fragments := make([]SQLString, 0, len(literals))
	inQuote := false
	for _, literal := range literals {
		start, end := stringContentBounds(text, literal)
		fragments = append(fragments, SQLString{Start: start, End: end, InQuoteAtStart: inQuote})
		_, inQuote = scanSQLWords(text, start, end, inQuote)
	}
	return fragments
}

// stringContentBounds returns the content of a string token without its quotes.
// An unterminated literal ends at the end of its first line.
func stringContentBounds(text string, token Token) (int, int) {
	start := token.Start + 1
	end := token.End
	if start > end {
		return token.End, token.End
	}
	if newline := strings.IndexAny(text[start:end], "\r\n"); newline >= 0 {
		return start, start + newline
	}
	if end-token.Start >= 2 && text[end-1] == '"' {
		end--
	}
	return start, end
}

func firstSQLWord(content string) string {
	start := 0
	for start < len(content) && (content[start] == ' ' || content[start] == '\t' || content[start] == '(') {
		start++
	}
	end := start
	for end < len(content) && isSQLWordByte(content[end]) {
		end++
	}
	// "from.asp" and "on=1" start with a SQL word but are not SQL.
	if end < len(content) && content[end] != ' ' && content[end] != '\t' && content[end] != '(' {
		return ""
	}
	return strings.ToLower(content[start:end])
}

// sqlShapedLiterals reports literals that read as a SQL statement on their own,
// without help from the surrounding VBScript.
func sqlShapedLiterals(contents []string) bool {
	first := strings.ToLower(strings.TrimLeft(contents[0], " \t("))
	all := " " + strings.ToLower(strings.Join(contents, " ")) + " "
	word, rest := splitSQLLeadWord(first)
	second, _ := splitSQLLeadWord(rest)
	switch word {
	case "select":
		return containsSQLWord(all, "from")
	case "insert":
		return second == "into"
	case "delete":
		return second == "from"
	case "update":
		return containsSQLWord(all, "set") && strings.Contains(all, "=")
	case "create", "alter", "drop":
		switch second {
		case "table", "view", "index", "procedure", "proc", "function", "database", "trigger":
			return true
		}
	case "truncate":
		return second == "table"
	}
	return false
}

func splitSQLLeadWord(content string) (string, string) {
	content = strings.TrimLeft(content, " \t")
	end := 0
	for end < len(content) && isSQLWordByte(content[end]) {
		end++
	}
	return content[:end], content[end:]
}

func containsSQLWord(lower string, word string) bool {
	for offset := 0; offset < len(lower); {
		index := strings.Index(lower[offset:], word)
		if index < 0 {
			return false
		}
		start := offset + index
		end := start + len(word)
		if (start == 0 || !isSQLWordByte(lower[start-1])) && (end >= len(lower) || !isSQLWordByte(lower[end])) {
			return true
		}
		offset = end
	}
	return false
}

// assignmentTarget returns the last name of "[Set] a.b = ..." and the index of
// its "=" token, or ("", -1) when the segment is not an assignment.
func assignmentTarget(tokens []Token) (string, int) {
	index := 0
	if index < len(tokens) && tokens[index].Kind == "keyword" {
		switch strings.ToLower(tokens[index].Text) {
		case "set", "const", "let":
			index++
		default:
			return "", -1
		}
	}
	target := ""
	for ; index < len(tokens); index++ {
		token := tokens[index]
		switch {
		case token.Kind == "identifier":
			target = token.Text
		case token.Text == ".":
		case token.Text == "=" && target != "":
			return target, index
		default:
			return "", -1
		}
	}
	return "", -1
}

func isSQLName(name string) bool {
	lower := strings.ToLower(name)
	if strings.Contains(lower, "querystring") {
		return false
	}
	return strings.Contains(lower, "sql") || strings.Contains(lower, "query") || strings.Contains(lower, "qry") || lower == "commandtext"
}

// writesResponse reports "Response.Write ...", whose text is page output even
// when it happens to read like SQL ("Select an item from the list").
func writesResponse(tokens []Token) bool {
	return len(tokens) >= 3 && strings.EqualFold(tokens[0].Text, "Response") && tokens[1].Text == "." && strings.EqualFold(tokens[2].Text, "Write")
}

// executesSQL reports a ".Execute" or ".Open" member call ahead of the first
// string literal, as in conn.Execute("...") or rs.Open "...", conn.
func executesSQL(tokens []Token) bool {
	for index := 0; index+1 < len(tokens); index++ {
		if tokens[index].Kind == "string" {
			return false
		}
		if tokens[index].Text != "." {
			continue
		}
		switch strings.ToLower(tokens[index+1].Text) {
		case "execute", "open":
			return true
		}
	}
	return false
}

func isSQLWordByte(character byte) bool {
	return character == '_' || character >= '0' && character <= '9' || character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z'
}

// scanSQLWords lexes SQL text in text[start:end]. inQuote carries an open SQL
// '...' literal across the VBScript fragments of one statement.
func scanSQLWords(text string, start, end int, inQuote bool) ([]SQLWord, bool) {
	var words []SQLWord
	for offset := start; offset < end; {
		character := text[offset]
		if inQuote {
			if character == '\'' {
				inQuote = false
			}
			offset++
			continue
		}
		switch {
		case character == '\'':
			inQuote = true
			offset++
		case character == '-' && offset+1 < end && text[offset+1] == '-':
			return words, false
		case character == '/' && offset+1 < end && text[offset+1] == '*':
			close := strings.Index(text[offset+2:end], "*/")
			if close < 0 {
				return words, false
			}
			offset += close + 4
		case character == '[':
			close := strings.IndexByte(text[offset:end], ']')
			if close < 0 {
				return words, false
			}
			if close > 1 {
				words = append(words, SQLWord{Start: offset + 1, End: offset + close, Kind: SQLWordIdentifier})
			}
			offset += close + 1
		case character == '@' || character == '#' || character == ':':
			// Parameters, temp tables, and bind variables are not plain names.
			offset++
			for offset < end && isSQLWordByte(text[offset]) {
				offset++
			}
		case character >= '0' && character <= '9':
			for offset < end && (isSQLWordByte(text[offset]) || text[offset] == '.') {
				offset++
			}
		case isSQLWordByte(character):
			wordStart := offset
			for offset < end && isSQLWordByte(text[offset]) {
				offset++
			}
			lower := strings.ToLower(text[wordStart:offset])
			kind := SQLWordIdentifier
			if _, ok := sqlFunctions[lower]; ok && sqlCallFollows(text, offset, end) {
				kind = SQLWordFunction
			} else if _, ok := sqlKeywords[lower]; ok {
				kind = SQLWordKeyword
			}
			words = append(words, SQLWord{Start: wordStart, End: offset, Kind: kind})
		default:
			offset++
		}
	}
	return words, inQuote
}

func sqlCallFollows(text string, offset, end int) bool {
	for offset < end && (text[offset] == ' ' || text[offset] == '\t') {
		offset++
	}
	return offset < end && text[offset] == '('
}

func sqlSemanticTokens(doc *core.TextDocument, parsed *core.ParsedDocument) []semanticToken {
	var tokens []semanticToken
	for _, fragment := range SQLStrings(parsed) {
		for _, word := range SQLWords(parsed.Text, fragment) {
			switch word.Kind {
			case SQLWordKeyword:
				tokens = append(tokens, semanticTokenForRange(doc, word.Start, word.End, semanticKeyword, 0))
			case SQLWordFunction:
				tokens = append(tokens, semanticTokenForRange(doc, word.Start, word.End, semanticFunction, semanticLibrary))
			}
		}
	}
	return tokens
}

// sqlTransparentCalls pass a value through without making it safe for SQL.
// An empty name is a grouping parenthesis.
var sqlTransparentCalls = wordSet("", "trim", "ltrim", "rtrim", "lcase", "ucase", "cstr", "left", "right", "mid", "execute", "open")

// SQLInjectionDiagnostics reports Request values concatenated into SQL text.
// A value wrapped in a conversion, Replace, or any user function is treated as
// sanitized, as is a variable that is validated or reassigned anywhere.
func SQLInjectionDiagnostics(parsed *core.ParsedDocument, severity lsp.DiagnosticSeverity) []lsp.Diagnostic {
	segments := sqlSegments(parsed)
	if len(segments) == 0 {
		return nil
	}
	tainted := taintedRequestVariables(parsed)
	doc := core.SourceDocument(parsed)
	var diagnostics []lsp.Diagnostic
	for _, segment := range segments {
		first := 0
		if _, equal := assignmentTarget(segment.tokens); equal >= 0 {
			first = equal + 1
		}
		for _, source := range requestSources(segment.tokens, first, tainted) {
			if !source.concatenated {
				continue
			}
			start := segment.tokens[source.start].Start
			end := segment.tokens[source.end].End
			name := parsed.Text[start:end]
			diagnostics = append(diagnostics, lsp.Diagnostic{
				Range:    doc.Range(start, end),
				Severity: severity,
				Source:   "asp-lsp-vbscript-sql",
				Code:     "sqlInjection",
				Message:  "Request value '" + name + "' is concatenated into SQL text. Use ADODB.Command parameters instead.",
				Data:     map[string]any{"name": name},
			})
		}
	}
	return diagnostics
}

type requestSource struct {
	start        int
	end          int
	concatenated bool
}

type sqlCallFrame struct {
	name      string
	nameIndex int
}

// requestSources finds unsanitized Request values and tainted variables in
// tokens[first:]. start and end are token indexes of the value expression.
func requestSources(tokens []Token, first int, tainted map[string]struct{}) []requestSource {
	closes := matchingCloseIndexes(tokens)
	var sources []requestSource
	var stack []sqlCallFrame
	for index := 0; index < len(tokens); index++ {
		token := tokens[index]
		switch token.Text {
		case "(":
			frame := sqlCallFrame{nameIndex: index}
			if index > 0 && (tokens[index-1].Kind == "identifier" || tokens[index-1].Kind == "keyword") {
				frame.name = strings.ToLower(tokens[index-1].Text)
				frame.nameIndex = callStartIndex(tokens, index-1)
			}
			stack = append(stack, frame)
			continue
		case ")":
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			continue
		}
		if index < first || token.Kind != "identifier" || index > 0 && tokens[index-1].Text == "." {
			continue
		}
		end := -1
		if strings.EqualFold(token.Text, "Request") {
			end = requestExpressionEnd(tokens, index, closes)
		} else if _, ok := tainted[strings.ToLower(token.Text)]; ok && (index+1 >= len(tokens) || tokens[index+1].Text != "(" && tokens[index+1].Text != ".") {
			end = index
		}
		if end < 0 {
			continue
		}
		safe := false
		for _, frame := range stack {
			if _, transparent := sqlTransparentCalls[frame.name]; !transparent {
				safe = true
				break
			}
		}
		if !safe {
			source := requestSource{start: index, end: end, concatenated: isConcatenated(tokens, index, end)}
			for depth := len(stack) - 1; depth >= 0 && !source.concatenated; depth-- {
				frame := stack[depth]
				if close, ok := closes[frameOpenIndex(tokens, frame)]; ok && frame.name != "execute" && frame.name != "open" {
					source.concatenated = isConcatenated(tokens, frame.nameIndex, close)
				}
			}
			sources = append(sources, source)
		}
		index = end
	}
	return sources
}

func frameOpenIndex(tokens []Token, frame sqlCallFrame) int {
	for index := frame.nameIndex; index < len(tokens); index++ {
		if tokens[index].Text == "(" {
			return index
		}
	}
	return -1
}

// callStartIndex walks back over "owner.member" so a member call's span starts
// at its owner.
func callStartIndex(tokens []Token, nameIndex int) int {
	for nameIndex >= 2 && tokens[nameIndex-1].Text == "." && tokens[nameIndex-2].Kind == "identifier" {
		nameIndex -= 2
	}
	return nameIndex
}

func isConcatenated(tokens []Token, start, end int) bool {
	isOperator := func(index int) bool {
		return index >= 0 && index < len(tokens) && (tokens[index].Text == "&" || tokens[index].Text == "+")
	}
	return isOperator(start-1) || isOperator(end+1)
}

// requestExpressionEnd returns the last token of Request("x"),
// Request.Form("x"), or Request.QueryString("x").Item, or -1 for a bare
// Request reference.
func requestExpressionEnd(tokens []Token, index int, closes map[int]int) int {
	end := -1
	cursor := index + 1
	for cursor < len(tokens) {
		switch {
		case tokens[cursor].Text == "(":
			close, ok := closes[cursor]
			if !ok {
				return -1
			}
			end = close
			cursor = close + 1
		case tokens[cursor].Text == "." && cursor+1 < len(tokens) && tokens[cursor+1].Kind == "identifier":
			end = cursor + 1
			cursor += 2
		default:
			return end
		}
	}
	return end
}

func matchingCloseIndexes(tokens []Token) map[int]int {
	closes := map[int]int{}
	var stack []int
	for index, token := range tokens {
		switch token.Text {
		case "(":
			stack = append(stack, index)
		case ")":
			if len(stack) > 0 {
				closes[stack[len(stack)-1]] = index
				stack = stack[:len(stack)-1]
			}
		}
	}
	return closes
}

// taintedRequestVariables returns the variables whose every assignment is an
// unsanitized Request value and that are never checked with IsNumeric or IsDate.
func taintedRequestVariables(parsed *core.ParsedDocument) map[string]struct{} {
	taintedCount := map[string]int{}
	excluded := map[string]struct{}{}
	for _, statement := range vbStatements(parsed) {
		for _, tokens := range splitInlineBranches(statement.Tokens) {
			for index := 0; index+3 < len(tokens); index++ {
				if tokens[index+1].Text != "(" || tokens[index+2].Kind != "identifier" || tokens[index+3].Text != ")" {
					continue
				}
				switch strings.ToLower(tokens[index].Text) {
				case "isnumeric", "isdate":
					excluded[strings.ToLower(tokens[index+2].Text)] = struct{}{}
				}
			}
			target, equal := assignmentTarget(tokens)
			simple := equal == 1 && tokens[0].Kind == "identifier" || equal == 2 && tokens[0].Kind == "keyword"
			if !simple {
				continue
			}
			name := strings.ToLower(target)
			value := tokens[equal+1:]
			sources := requestSources(value, 0, nil)
			// Only "name = Request(...)" with nothing appended counts; any
			// other assignment makes the variable's content unknown.
			if len(sources) == 1 && wholeExpression(value, sources[0]) {
				taintedCount[name]++
			} else {
				excluded[name] = struct{}{}
			}
		}
	}
	tainted := map[string]struct{}{}
	for name := range taintedCount {
		if _, skip := excluded[name]; !skip {
			tainted[name] = struct{}{}
		}
	}
	return tainted
}

// wholeExpression reports that the value is exactly the source, optionally
// wrapped in transparent calls such as Trim(...).
func wholeExpression(value []Token, source requestSource) bool {
	start, end := source.start, source.end
	for start > 0 && end+1 < len(value) && value[start-1].Text == "(" && value[end+1].Text == ")" {
		start--
		end++
		if start > 0 && (value[start-1].Kind == "identifier" || value[start-1].Kind == "keyword") {
			start = callStartIndex(value, start-1)
		}
	}
	return start == 0 && end == len(value)-1
}

// StringLiteralContentAt returns the content bounds of the VBScript string
// literal that contains offset, excluding its quotes.
func StringLiteralContentAt(parsed *core.ParsedDocument, offset int) (start int, end int, ok bool) {
	if parsed == nil {
		return 0, 0, false
	}
	tokens := vbscriptDocumentTokens(parsed)
	index := sort.Search(len(tokens), func(index int) bool { return tokens[index].End >= offset })
	for ; index < len(tokens) && tokens[index].Start < offset; index++ {
		if tokens[index].Kind != "string" {
			continue
		}
		start, end = stringContentBounds(parsed.Text, tokens[index])
		if offset >= start && offset <= end {
			return start, end, true
		}
	}
	return 0, 0, false
}

// SQLInsideQuoteAt reports that offset lies inside a SQL '...' literal of the
// fragment, where SQL names and keywords are plain data.
func SQLInsideQuoteAt(text string, fragment SQLString, offset int) bool {
	_, inQuote := scanSQLWords(text, fragment.Start, min(max(offset, fragment.Start), fragment.End), fragment.InQuoteAtStart)
	return inQuote
}

// SQLNames are the table and column names used by the SQL strings of a document.
type SQLNames struct {
	Tables  []string
	Columns []string
}

var sqlTableContextWords = wordSet("from", "join", "into", "update", "table")

// IsSQLTableContextWord reports a keyword that is followed by a table name.
func IsSQLTableContextWord(word string) bool {
	_, ok := sqlTableContextWords[strings.ToLower(word)]
	return ok
}

// CollectSQLNames harvests the identifiers of the SQL strings in a document.
// A name that follows FROM, JOIN, INTO, UPDATE, or TABLE is a table; any other
// identifier is treated as a column.
func CollectSQLNames(parsed *core.ParsedDocument) SQLNames {
	var names SQLNames
	if parsed == nil {
		return names
	}
	tables := map[string]struct{}{}
	columns := map[string]struct{}{}
	for _, segment := range sqlSegments(parsed) {
		previous := ""
		for _, fragment := range segment.strings {
			for _, word := range SQLWords(parsed.Text, fragment) {
				text := parsed.Text[word.Start:word.End]
				lower := strings.ToLower(text)
				if word.Kind != SQLWordIdentifier {
					previous = lower
					continue
				}
				if _, table := sqlTableContextWords[previous]; table {
					if _, seen := tables[lower]; !seen {
						tables[lower] = struct{}{}
						names.Tables = append(names.Tables, text)
					}
				} else if _, seen := columns[lower]; !seen {
					columns[lower] = struct{}{}
					names.Columns = append(names.Columns, text)
				}
				previous = ""
			}
		}
	}
	filtered := names.Columns[:0]
	for _, column := range names.Columns {
		if _, table := tables[strings.ToLower(column)]; !table {
			filtered = append(filtered, column)
		}
	}
	names.Columns = filtered
	return names
}
