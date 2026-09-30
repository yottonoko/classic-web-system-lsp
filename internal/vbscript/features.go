package vbscript

import (
	"regexp"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/core"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

var declarations = regexp.MustCompile(`(?im)^\s*(Public\s+|Private\s+)?(Sub|Function|Class)\s+([A-Za-z_][A-Za-z0-9_]*)`)

type CompletionOptions struct {
	SyntaxSnippets bool
	SyntaxKeywords bool
	Standalone     bool
}

func Completions() []lsp.CompletionItem {
	return CompletionsFor("", 0)
}

func CompletionsFor(text string, offset int) []lsp.CompletionItem {
	return CompletionsForOptions(text, offset, CompletionOptions{SyntaxSnippets: true, SyntaxKeywords: true})
}

func CompletionsForOptions(text string, offset int, options CompletionOptions) []lsp.CompletionItem {
	if options.SyntaxKeywords {
		if items := blockContextCompletions(text, offset, options.SyntaxSnippets); len(items) > 0 {
			return items
		}
	}
	if items := onErrorCompletions(text, offset); len(items) > 0 {
		return items
	}
	if owner := memberOwnerBefore(text, offset); owner != "" {
		return objectMembers(owner, options.Standalone)
	}
	keywords := []string{"Dim", "Set", "If", "Then", "Else", "ElseIf", "End If", "For", "Each", "Next", "Do", "Loop", "Function", "Sub", "Class"}
	if options.Standalone {
		keywords = append(keywords, "WScript", "Err", "CStr")
	} else {
		keywords = append(keywords, "Response", "Request", "Server", "Session", "Application")
	}
	items := make([]lsp.CompletionItem, 0, len(keywords)+len(builtinFunctionCatalog)+len(builtinConstantCatalog))
	if options.SyntaxSnippets {
		items = append(items, syntaxSnippetCompletions()...)
	}
	for _, keyword := range keywords {
		kind := lsp.CompletionItemKindKeyword
		if strings.Contains(keyword, ".") || keyword == "Response" || keyword == "Request" || keyword == "Server" || keyword == "Session" || keyword == "Application" || keyword == "WScript" {
			kind = lsp.CompletionItemKindVariable
		}
		items = append(items, lsp.CompletionItem{Label: keyword, Kind: kind, Detail: "VBScript"})
	}
	items = append(items, builtinCompletionItems()...)
	return items
}

func StandaloneHoverAt(text string, offset int) *lsp.Hover {
	word := WordAt(text, offset)
	if word == "" {
		return nil
	}
	start := offset
	if start < 0 {
		start = 0
	}
	if start > len(text) {
		start = len(text)
	}
	for start > 0 && isIdent(text[start-1]) {
		start--
	}
	if ownerStart, ownerEnd := memberOwnerBeforeOffset(text, start); ownerStart >= 0 {
		if isClassicASPGlobalObject(text[ownerStart:ownerEnd]) {
			return nil
		}
		if hover := standaloneBuiltinHover(strings.ToLower(text[ownerStart:ownerEnd] + "." + word)); hover != nil {
			return hover
		}
	}
	if hover := standaloneBuiltinHover(strings.ToLower(word)); hover != nil {
		return hover
	}
	if isClassicASPGlobalObject(word) {
		return nil
	}
	return HoverAt(text, offset)
}

func isClassicASPGlobalObject(name string) bool {
	switch strings.ToLower(name) {
	case "application", "session", "response", "request", "server":
		return true
	default:
		return false
	}
}

func standaloneBuiltinHover(key string) *lsp.Hover {
	var value string
	switch key {
	case "wscript":
		value = "Dim WScript As WScript\n\nWindows Script Host automation object."
	case "wscript.echo":
		value = "Sub WScript.Echo(value)\n\nWrites text to the host output stream."
	case "wscript.quit":
		value = "Sub WScript.Quit(errorCode)\n\nTerminates the script process."
	case "wscript.sleep":
		value = "Sub WScript.Sleep(milliseconds)\n\nPauses script execution."
	case "wscript.createobject":
		value = "Function WScript.CreateObject(progId, prefix) As Object\n\nCreates a COM automation object."
	case "wscript.getobject":
		value = "Function WScript.GetObject(pathname, progId, prefix) As Object\n\nGets an existing COM automation object."
	case "wscript.scriptfullname":
		value = "property WScript.ScriptFullName As String\n\nReturns the full path of the running script."
	case "wscript.scriptname":
		value = "property WScript.ScriptName As String\n\nReturns the running script file name."
	default:
		return nil
	}
	return vbscriptSignatureHover(value)
}

func vbscriptSignatureHover(value string) *lsp.Hover {
	signature, documentation, ok := strings.Cut(value, "\n\n")
	markdown := fencedVBScriptSignature(signature)
	if ok && strings.TrimSpace(documentation) != "" {
		markdown += "\n\n" + documentation
	}
	return &lsp.Hover{Contents: lsp.MarkupContent{Kind: "markdown", Value: markdown}}
}

func fencedVBScriptSignature(signature string) string {
	signature = strings.TrimSpace(signature)
	if signature == "" {
		return ""
	}
	return "```vbscript\n" + signature + "\n```"
}

func syntaxSnippetCompletions() []lsp.CompletionItem {
	snippets := []struct {
		label string
		text  string
	}{
		{"If Then", "If ${1:condition} Then\n\t${0}\nEnd If"},
		{"If Then Else", "If ${1:condition} Then\n\t${2}\nElse\n\t${0}\nEnd If"},
		{"Do Loop", "Do\n\t${0}\nLoop"},
		{"Do While Loop", "Do While ${1:condition}\n\t${0}\nLoop"},
		{"Do Until Loop", "Do Until ${1:condition}\n\t${0}\nLoop"},
		{"Do Loop While", "Do\n\t${0}\nLoop While ${1:condition}"},
		{"Do Loop Until", "Do\n\t${0}\nLoop Until ${1:condition}"},
		{"For Next", "For ${1:index} = ${2:start} To ${3:end}\n\t${0}\nNext"},
		{"For Each Next", "For Each ${1:item} In ${2:items}\n\t${0}\nNext"},
		{"Select Case", "Select Case ${1:expression}\n\tCase ${2:value}\n\t\t${0}\nEnd Select"},
		{"With", "With ${1:object}\n\t${0}\nEnd With"},
		{"Sub", "Sub ${1:Name}(${2})\n\t${0}\nEnd Sub"},
		{"Function", "Function ${1:Name}(${2})\n\t${0}\nEnd Function"},
		{"Class", "Class ${1:Name}\n\t${0}\nEnd Class"},
		{"Property Get", "Property Get ${1:Name}()\n\t${0}\nEnd Property"},
		{"Property Let", "Property Let ${1:Name}(ByVal value)\n\t${0}\nEnd Property"},
		{"Property Set", "Property Set ${1:Name}(ByRef value)\n\t${0}\nEnd Property"},
	}
	items := make([]lsp.CompletionItem, 0, len(snippets))
	for _, snippet := range snippets {
		items = append(items, lsp.CompletionItem{
			Label:            snippet.label,
			Kind:             lsp.CompletionItemKindSnippet,
			Detail:           "VBScript snippet",
			InsertText:       snippet.text,
			InsertTextFormat: 2,
		})
	}
	return items
}

func objectMembers(owner string, standalone bool) []lsp.CompletionItem {
	var labels []string
	switch strings.ToLower(owner) {
	case "application":
		if standalone {
			return nil
		}
		labels = []string{"Contents", "StaticObjects", "Contents.Remove", "Contents.RemoveAll", "Lock", "Unlock"}
	case "session":
		if standalone {
			return nil
		}
		labels = []string{"Contents", "StaticObjects", "CodePage", "LCID", "SessionID", "Timeout", "Abandon", "Contents.Remove", "Contents.RemoveAll"}
	case "response":
		if standalone {
			return nil
		}
		labels = []string{"Cookies", "Buffer", "CacheControl", "Charset", "ContentType", "Expires", "ExpiresAbsolute", "IsClientConnected", "Pics", "Status", "AddHeader", "AppendToLog", "BinaryWrite", "Clear", "End", "Flush", "Redirect", "Write"}
	case "request":
		if standalone {
			return nil
		}
		labels = []string{"QueryString", "Form", "Cookies", "ServerVariables", "ClientCertificate", "TotalBytes", "BinaryRead"}
	case "server":
		if standalone {
			return nil
		}
		labels = []string{"ScriptTimeout", "CreateObject", "Execute", "GetLastError", "HTMLEncode", "MapPath", "Transfer", "URLEncode"}
	case "err":
		labels = []string{"Number", "Description", "Source", "HelpFile", "HelpContext", "Clear", "Raise"}
	case "wscript":
		if !standalone {
			return nil
		}
		labels = []string{"Arguments", "FullName", "Name", "Path", "ScriptFullName", "ScriptName", "StdErr", "StdIn", "StdOut", "Version", "ConnectObject", "CreateObject", "DisconnectObject", "Echo", "GetObject", "Quit", "Sleep"}
	default:
		return nil
	}
	items := make([]lsp.CompletionItem, 0, len(labels))
	for _, label := range labels {
		kind := lsp.CompletionItemKindMethod
		if isBuiltinObjectProperty(label) {
			kind = lsp.CompletionItemKindProperty
		}
		items = append(items, lsp.CompletionItem{Label: label, Kind: kind, Detail: owner})
	}
	return items
}

func isBuiltinObjectProperty(label string) bool {
	switch label {
	case "Contents", "StaticObjects", "CodePage", "LCID", "SessionID", "Timeout",
		"Cookies", "Buffer", "CacheControl", "Charset", "ContentType", "Expires", "ExpiresAbsolute", "IsClientConnected", "Pics", "Status",
		"QueryString", "Form", "ServerVariables", "ClientCertificate", "TotalBytes", "ScriptTimeout",
		"Number", "Description", "Source", "HelpFile", "HelpContext",
		"Arguments", "FullName", "Name", "Path", "ScriptFullName", "ScriptName", "StdErr", "StdIn", "StdOut", "Version":
		return true
	default:
		return false
	}
}

func blockContextCompletions(text string, offset int, snippetsEnabled bool) []lsp.CompletionItem {
	lineStart, prefix := completionLinePrefix(text, offset)
	trimmed := strings.TrimSpace(prefix)
	lower := strings.ToLower(trimmed)
	switch {
	case lower == "":
		return nil
	case strings.HasPrefix("then", lower) && lineLooksLikeUnclosedIf(text[lineStart:offset]):
		return []lsp.CompletionItem{keywordCompletion("Then", text, lineStart+strings.LastIndex(strings.ToLower(prefix), lower), offset)}
	case strings.HasPrefix("case", lower) && hasOpenBlock(text[:lineStart], "select"):
		return []lsp.CompletionItem{keywordCompletion("Case", text, lineStart+strings.LastIndex(strings.ToLower(prefix), lower), offset)}
	case strings.HasPrefix("loop", lower) && hasOpenBlock(text[:lineStart], "do"):
		return []lsp.CompletionItem{blockCloseCompletion("Loop", text, lineStart, offset, snippetsEnabled)}
	case strings.HasPrefix("wend", lower) && hasOpenBlock(text[:lineStart], "while"):
		return []lsp.CompletionItem{blockCloseCompletion("Wend", text, lineStart, offset, snippetsEnabled)}
	case strings.HasPrefix("next", lower) && hasOpenBlock(text[:lineStart], "for"):
		return []lsp.CompletionItem{blockCloseCompletion("Next", text, lineStart, offset, snippetsEnabled)}
	case isEndBlockCompletionPrefix(lower, text[:lineStart]):
		return endBlockCompletions(text, lineStart, offset, lower, snippetsEnabled)
	}
	return nil
}

func isEndBlockCompletionPrefix(lower string, beforeLine string) bool {
	if strings.HasPrefix(lower, "end") || strings.HasPrefix("end", lower) || strings.HasPrefix("elseif", lower) || strings.HasPrefix("else", lower) {
		return len(openEndBlocks(beforeLine)) > 0
	}
	for _, label := range openEndBlocks(beforeLine) {
		suffix := strings.ToLower(strings.TrimPrefix(label, "End "))
		if strings.HasPrefix(suffix, lower) {
			return true
		}
	}
	return false
}

func completionLinePrefix(text string, offset int) (int, string) {
	if offset < 0 {
		offset = 0
	}
	if offset > len(text) {
		offset = len(text)
	}
	lineStart := 0
	if newline := strings.LastIndexAny(text[:offset], "\n\r"); newline >= 0 {
		lineStart = newline + 1
	}
	return lineStart, text[lineStart:offset]
}

func lineLooksLikeUnclosedIf(line string) bool {
	lower := strings.ToLower(strings.TrimSpace(line))
	return (strings.HasPrefix(lower, "if ") || strings.HasPrefix(lower, "elseif ")) && !strings.Contains(lower, " then")
}

func hasOpenBlock(text string, kind string) bool {
	depth := 0
	for _, rawLine := range strings.Split(text, "\n") {
		line := strings.ToLower(strings.TrimSpace(strings.TrimRight(rawLine, "\r")))
		if line == "" || strings.HasPrefix(line, "'") || strings.HasPrefix(line, "rem ") {
			continue
		}
		switch kind {
		case "select":
			if strings.HasPrefix(line, "select case") {
				depth++
			} else if strings.HasPrefix(line, "end select") && depth > 0 {
				depth--
			}
		case "do":
			if strings.HasPrefix(line, "do") {
				depth++
			} else if strings.HasPrefix(line, "loop") && depth > 0 {
				depth--
			}
		case "while":
			if strings.HasPrefix(line, "while ") {
				depth++
			} else if strings.HasPrefix(line, "wend") && depth > 0 {
				depth--
			}
		case "for":
			if strings.HasPrefix(line, "for ") {
				depth++
			} else if strings.HasPrefix(line, "next") && depth > 0 {
				depth--
			}
		}
	}
	return depth > 0
}

func endBlockCompletions(text string, lineStart int, offset int, lowerPrefix string, snippetsEnabled bool) []lsp.CompletionItem {
	open := openEndBlocks(text[:lineStart])
	if len(open) == 0 {
		return nil
	}
	typedAfterEnd := ""
	if strings.HasPrefix(lowerPrefix, "end") {
		typedAfterEnd = strings.TrimSpace(strings.TrimPrefix(lowerPrefix, "end"))
	}
	items := make([]lsp.CompletionItem, 0, len(open)+1)
	if open[0] == "End If" && strings.HasPrefix("elseif", lowerPrefix) {
		items = append(items, keywordCompletion("ElseIf", text, lineStart, offset))
	}
	if open[0] == "End If" && strings.HasPrefix("else", lowerPrefix) {
		items = append(items, keywordCompletion("Else", text, lineStart, offset))
	}
	if typedAfterEnd == "" && strings.HasPrefix("end", lowerPrefix) {
		items = append(items, keywordCompletion("End", text, lineStart, offset))
	}
	for _, label := range open {
		suffix := strings.ToLower(strings.TrimPrefix(label, "End "))
		if typedAfterEnd != "" && !strings.HasPrefix(suffix, typedAfterEnd) {
			continue
		}
		items = append(items, blockCloseCompletion(label, text, lineStart, offset, snippetsEnabled))
	}
	return items
}

func openEndBlocks(text string) []string {
	stack := []string{}
	for _, rawLine := range strings.Split(text, "\n") {
		line := strings.ToLower(strings.TrimSpace(strings.TrimRight(rawLine, "\r")))
		if line == "" || strings.HasPrefix(line, "'") || strings.HasPrefix(line, "rem ") {
			continue
		}
		switch {
		case strings.HasPrefix(line, "end if"), strings.HasPrefix(line, "end function"), strings.HasPrefix(line, "end sub"),
			strings.HasPrefix(line, "end class"), strings.HasPrefix(line, "end property"):
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case strings.HasPrefix(line, "if ") && strings.Contains(line, " then"):
			stack = append(stack, "End If")
		case strings.HasPrefix(line, "function "):
			stack = append(stack, "End Function")
		case strings.HasPrefix(line, "sub "):
			stack = append(stack, "End Sub")
		case strings.HasPrefix(line, "class "):
			stack = append(stack, "End Class")
		case strings.HasPrefix(line, "public function "), strings.HasPrefix(line, "private function "):
			stack = append(stack, "End Function")
		case strings.HasPrefix(line, "public sub "), strings.HasPrefix(line, "private sub "):
			stack = append(stack, "End Sub")
		case strings.HasPrefix(line, "property "), strings.HasPrefix(line, "public property "), strings.HasPrefix(line, "private property "):
			stack = append(stack, "End Property")
		}
	}
	for i, j := 0, len(stack)-1; i < j; i, j = i+1, j-1 {
		stack[i], stack[j] = stack[j], stack[i]
	}
	return stack
}

func keywordCompletion(label string, text string, start int, end int) lsp.CompletionItem {
	return lsp.CompletionItem{
		Label:    label,
		Kind:     lsp.CompletionItemKindKeyword,
		Detail:   "VBScript syntax keyword",
		TextEdit: &lsp.TextEdit{Range: lineRange(text, start, end), NewText: label},
	}
}

func blockCloseCompletion(label string, text string, start int, end int, snippetsEnabled bool) lsp.CompletionItem {
	kind := lsp.CompletionItemKindKeyword
	detail := "VBScript syntax keyword"
	if snippetsEnabled {
		kind = lsp.CompletionItemKindSnippet
		detail = "VBScript syntax snippet"
	}
	filterText := strings.TrimPrefix(label, "End ")
	return lsp.CompletionItem{
		Label:      label,
		Kind:       kind,
		Detail:     detail,
		FilterText: filterText,
		TextEdit:   &lsp.TextEdit{Range: lineRange(text, start, end), NewText: label},
	}
}

func memberOwnerBefore(text string, offset int) string {
	if offset < 0 {
		offset = 0
	}
	if offset > len(text) {
		offset = len(text)
	}
	i := offset
	for i > 0 && isIdent(text[i-1]) {
		i--
	}
	if i == 0 || text[i-1] != '.' {
		return ""
	}
	i--
	end := i
	for i > 0 && isIdent(text[i-1]) {
		i--
	}
	if i == end {
		return ""
	}
	return text[i:end]
}

func onErrorCompletions(text string, offset int) []lsp.CompletionItem {
	if offset < 0 {
		offset = 0
	}
	if offset > len(text) {
		offset = len(text)
	}
	lineStart := 0
	if newline := strings.LastIndexAny(text[:offset], "\n\r"); newline >= 0 {
		lineStart = newline + 1
	}
	indentEnd := lineStart
	for indentEnd < offset && (text[indentEnd] == ' ' || text[indentEnd] == '\t') {
		indentEnd++
	}
	prefix := text[indentEnd:offset]
	lower := strings.ToLower(prefix)
	if lower == "" {
		return nil
	}
	var labels []string
	switch {
	case strings.HasPrefix("error", lower) || strings.HasPrefix("on error", lower):
		labels = []string{"On Error Resume Next", "On Error GoTo 0"}
	case strings.HasPrefix("error resume next", lower) || strings.HasPrefix("on error resume next", lower):
		labels = []string{"On Error Resume Next"}
	case strings.HasPrefix("error goto 0", lower) || strings.HasPrefix("on error goto 0", lower):
		labels = []string{"On Error GoTo 0"}
	default:
		return nil
	}
	r := lineRange(text, indentEnd, offset)
	items := make([]lsp.CompletionItem, 0, len(labels))
	for _, label := range labels {
		filterText := strings.TrimPrefix(label, "On ")
		items = append(items, lsp.CompletionItem{
			Label:      label,
			Kind:       lsp.CompletionItemKindKeyword,
			Detail:     "VBScript error handling",
			InsertText: label,
			TextEdit:   &lsp.TextEdit{Range: r, NewText: label},
			FilterText: filterText,
		})
	}
	return items
}

func lineRange(text string, start, end int) lsp.Range {
	doc := core.NewTextDocument("", "classic-asp", 0, text)
	return doc.Range(start, end)
}

func HoverAt(text string, offset int) *lsp.Hover {
	word := WordAt(text, offset)
	if word == "" {
		return nil
	}
	start := offset
	if start < 0 {
		start = 0
	}
	if start > len(text) {
		start = len(text)
	}
	for start > 0 && isIdent(text[start-1]) {
		start--
	}
	if ownerStart, ownerEnd := memberOwnerBeforeOffset(text, start); ownerStart >= 0 {
		if hover := builtinHover(strings.ToLower(text[ownerStart:ownerEnd] + "." + word)); hover != nil {
			return hover
		}
	}
	return Hover(word)
}

func Hover(word string) *lsp.Hover {
	if hover := builtinHover(strings.ToLower(word)); hover != nil {
		return hover
	}
	switch strings.ToLower(word) {
	case "response":
		return vbscriptSignatureHover("Dim Response As Response\n\nClassic ASP response object.")
	case "request":
		return vbscriptSignatureHover("Dim Request As Request\n\nClassic ASP request object.")
	case "server":
		return vbscriptSignatureHover("Dim Server As Server\n\nClassic ASP server utility object.")
	case "session":
		return vbscriptSignatureHover("Dim Session As Session\n\nClassic ASP session object.")
	case "application":
		return vbscriptSignatureHover("Dim Application As Application\n\nClassic ASP application object.")
	case "application_onstart":
		return vbscriptSignatureHover("Sub Application_OnStart()")
	default:
		return nil
	}
}

func builtinHover(key string) *lsp.Hover {
	if spec, ok := builtinFunctionSpecForKey(key); ok {
		return vbscriptSignatureHover(builtinFunctionHoverText(spec))
	}
	if spec, ok := builtinConstantSpecForKey(key); ok {
		return vbscriptSignatureHover(builtinConstantHoverText(spec))
	}
	var value string
	switch key {
	case "ascb":
		value = "Function AscB(string) As Number\n\nReturns the first byte code for a string."
	case "ascw":
		value = "Function AscW(string) As Number\n\nReturns the Unicode code point for the first character."
	case "chrb":
		value = "Function ChrB(charCode) As String\n\nReturns a one-byte character for the supplied code."
	case "chrw":
		value = "Function ChrW(charCode) As String\n\nReturns a Unicode character for the supplied code."
	case "cstr":
		value = "Function CStr(value) As String\n\nConverts a value to String."
	case "cbool":
		value = "Function CBool(value) As Boolean\n\nConverts a value to Boolean."
	case "ccur":
		value = "Function CCur(value) As Currency\n\nConverts a value to Currency."
	case "lcase":
		value = "Function LCase(string) As String\n\nConverts a string to lowercase."
	case "ucase":
		value = "Function UCase(string) As String\n\nConverts a string to uppercase."
	case "trim":
		value = "Function Trim(string) As String\n\nRemoves leading and trailing spaces from a string."
	case "ltrim":
		value = "Function LTrim(string) As String\n\nRemoves leading spaces from a string."
	case "rtrim":
		value = "Function RTrim(string) As String\n\nRemoves trailing spaces from a string."
	case "len":
		value = "Function Len(value) As Number\n\nReturns the number of characters in a string."
	case "date":
		value = "Function Date() As Date\n\nReturns the current system date."
	case "now":
		value = "Function Now() As Date\n\nReturns the current system date and time."
	case "time":
		value = "Function Time() As Date\n\nReturns the current system time."
	case "dateadd":
		value = "Function DateAdd(interval, number, date) As Date\n\nAdds a time interval to a date."
	case "datediff":
		value = "Function DateDiff(interval, date1, date2, firstDayOfWeek, firstWeekOfYear) As Number\n\nReturns the number of intervals between two dates."
	case "datepart":
		value = "Function DatePart(interval, date, firstDayOfWeek, firstWeekOfYear) As Number\n\nReturns the requested interval part of a date expression."
	case "day":
		value = "Function Day(date) As Number\n\nReturns the day of the month."
	case "month":
		value = "Function Month(date) As Number\n\nReturns the month of the year."
	case "year":
		value = "Function Year(date) As Number\n\nReturns the year."
	case "getobject":
		value = "Function GetObject(pathname, class) As Object\n\nReturns an automation object from a file or class."
	case "inputbox":
		value = "Function InputBox(prompt, title, default, xpos, ypos, helpfile, context) As String\n\nDisplays a prompt and returns user input."
	case "instrb":
		value = "Function InStrB(start, string1, string2, compare) As Number\n\nReturns the byte position of one string within another."
	case "join":
		value = "Function Join(list, delimiter) As String\n\nJoins array elements into a string."
	case "leftb":
		value = "Function LeftB(string, length) As String\n\nReturns bytes from the left side of a string."
	case "lenb":
		value = "Function LenB(value) As Number\n\nReturns the byte length of a value."
	case "midb":
		value = "Function MidB(string, start, length) As String\n\nReturns bytes from the middle of a string."
	case "msgbox":
		value = "Function MsgBox(prompt, buttons, title, helpfile, context) As Number\n\nDisplays a message box and returns the selected button."
	case "replace":
		value = "Function Replace(expression, find, replaceWith, start, count, compare) As String\n\nReplaces matching substrings in a string."
	case "rightb":
		value = "Function RightB(string, length) As String\n\nReturns bytes from the right side of a string."
	case "split":
		value = "Function Split(expression, delimiter, count, compare) As Array\n\nSplits a string into an array."
	case "response.write":
		value = "Response.Write value\n\nWrites output to the HTTP response."
	case "response.buffer":
		value = "property Response.Buffer As Boolean\n\nControls whether ASP buffers page output."
	case "server.getlasterror":
		value = "Function Server.GetLastError() As ASPError\n\nReturns details for the last ASP error."
	case "randomize":
		value = "Function Randomize(number) As Variant\n\nInitializes the random-number generator."
	case "array":
		value = "Function Array(values) As Array\n\nCreates a Variant array."
	case "ubound":
		value = "Function UBound(array, dimension) As Number\n\nReturns the largest available subscript for an array dimension."
	case "adinteger":
		value = "Const adInteger As Number\n\nADO integer data type constant."
	case "err":
		value = "Const Err As ErrObject\n\nProvides access to the current VBScript error object."
	case "err.number":
		value = "property ErrObject.Number As Number\n\nReturns the current error number."
	case "err.description":
		value = "property ErrObject.Description As String\n\nReturns the current error description."
	case "err.source":
		value = "property ErrObject.Source As String\n\nReturns the object or application that generated the error."
	case "err.clear":
		value = "Sub ErrObject.Clear()\n\nClears the current error state."
	case "err.raise":
		value = "Sub ErrObject.Raise(number, source, description, helpfile, helpcontext)\n\nRaises a runtime error."
	default:
		return nil
	}
	return vbscriptSignatureHover(value)
}

func builtinCompletionItems() []lsp.CompletionItem {
	items := make([]lsp.CompletionItem, 0, len(builtinFunctionCatalog)+len(builtinConstantCatalog))
	for _, spec := range builtinFunctionCatalog {
		items = append(items, lsp.CompletionItem{
			Label:  spec.Label,
			Kind:   lsp.CompletionItemKindFunction,
			Detail: "Function " + spec.Signature + " As " + spec.ReturnType,
		})
	}
	for _, spec := range builtinConstantCatalog {
		items = append(items, lsp.CompletionItem{
			Label:  spec.Label,
			Kind:   lsp.CompletionItemKindValue,
			Detail: "Const " + spec.Type,
		})
	}
	return items
}

func DocumentSymbols(parsed *core.ParsedDocument) []lsp.DocumentSymbol {
	var cached []lsp.DocumentSymbol
	if parsed.LoadAnalysis("vbscript.document-symbols.v1", &cached) {
		return cached
	}
	source := core.SourceDocument(parsed)
	var symbols []lsp.DocumentSymbol
	for _, match := range declarations.FindAllStringSubmatchIndex(parsed.Text, -1) {
		nameStart, nameEnd := match[6], match[7]
		kindStart, kindEnd := match[4], match[5]
		kindText := strings.ToLower(parsed.Text[kindStart:kindEnd])
		kind := 12
		if kindText == "class" {
			kind = 5
		}
		r := source.Range(match[0], match[1])
		sel := source.Range(nameStart, nameEnd)
		symbols = append(symbols, lsp.DocumentSymbol{
			Name:           parsed.Text[nameStart:nameEnd],
			Kind:           kind,
			Range:          r,
			SelectionRange: sel,
		})
	}
	parsed.StoreAnalysis("vbscript.document-symbols.v1", symbols)
	return symbols
}

func TypeHierarchyItems(parsed *core.ParsedDocument, position lsp.Position) []lsp.TypeHierarchyItem {
	source := core.SourceDocument(parsed)
	offset := source.OffsetAt(position)
	word := strings.ToLower(WordAt(parsed.Text, offset))
	if word == "" {
		return nil
	}
	var items []lsp.TypeHierarchyItem
	for _, match := range declarations.FindAllStringSubmatchIndex(parsed.Text, -1) {
		kindText := strings.ToLower(parsed.Text[match[4]:match[5]])
		if kindText != "class" {
			continue
		}
		nameStart, nameEnd := match[6], match[7]
		name := parsed.Text[nameStart:nameEnd]
		if strings.ToLower(name) != word {
			continue
		}
		items = append(items, lsp.TypeHierarchyItem{
			Name:           name,
			Kind:           5,
			URI:            parsed.URI,
			Range:          source.Range(match[0], match[1]),
			SelectionRange: source.Range(nameStart, nameEnd),
			Data:           map[string]any{"uri": parsed.URI, "name": name},
		})
	}
	return items
}

func WordAt(text string, offset int) string {
	if offset < 0 {
		offset = 0
	}
	if offset > len(text) {
		offset = len(text)
	}
	start := offset
	for start > 0 && isIdent(text[start-1]) {
		start--
	}
	end := offset
	for end < len(text) && isIdent(text[end]) {
		end++
	}
	return text[start:end]
}

func isIdent(b byte) bool {
	return b == '_' || b >= '0' && b <= '9' || b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z'
}
