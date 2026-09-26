package lspserver

import (
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
	"github.com/yottonoko/classic-web-system-lsp/internal/vbscript"
)

func (s *Server) vbscriptBuiltInCompletionItems(standalone bool) []lsp.CompletionItem {
	items := []lsp.CompletionItem{
		s.enrichVBScriptBuiltInCompletionItem(lsp.CompletionItem{
			Label:  "DatePart",
			Kind:   lsp.CompletionItemKindFunction,
			Detail: "VBScript",
		}),
	}
	if standalone {
		return items
	}
	return append(items,
		s.enrichVBScriptBuiltInCompletionItem(lsp.CompletionItem{
			Label:  "Application_OnStart",
			Kind:   lsp.CompletionItemKindFunction,
			Detail: "Classic ASP Global.asa event",
		}),
		s.enrichVBScriptBuiltInCompletionItem(lsp.CompletionItem{
			Label:  "RegExp",
			Kind:   lsp.CompletionItemKindClass,
			Detail: "VBScript RegExp object",
		}),
	)
}

func (s *Server) enrichVBScriptBuiltInCompletionItem(item lsp.CompletionItem) lsp.CompletionItem {
	switch {
	case strings.EqualFold(item.Label, "DatePart"):
		item.Documentation = lsp.MarkupContent{Kind: "markdown", Value: s.datePartDocumentation()}
	case strings.EqualFold(item.Label, "RegExp"):
		if s.isJapanese() {
			item.Documentation = lsp.MarkupContent{Kind: "markdown", Value: "Pattern を使って文字列を検索する VBScript RegExp object です。"}
		} else {
			item.Documentation = lsp.MarkupContent{Kind: "markdown", Value: "VBScript RegExp object for searching strings with a pattern."}
		}
	case strings.EqualFold(item.Label, "Buffer") && strings.EqualFold(item.Detail, "Response"):
		item.Documentation = lsp.MarkupContent{Kind: "markdown", Value: s.responseBufferDocumentation()}
	case strings.EqualFold(item.Label, "Execute") && strings.EqualFold(item.Detail, "Server"):
		item.Documentation = lsp.MarkupContent{Kind: "markdown", Value: s.serverExecuteDocumentation()}
	case strings.EqualFold(item.Label, "adInteger"):
		item.Documentation = lsp.MarkupContent{Kind: "markdown", Value: s.adIntegerDocumentation()}
	default:
		if signature, returnType, summary, ok := vbscript.BuiltinFunctionDocumentation(item.Label); ok {
			item.Documentation = lsp.MarkupContent{
				Kind:  "markdown",
				Value: markdownVBScriptSignature("Function "+signature+" As "+returnType, summary),
			}
		} else if typeName, summary, ok := vbscript.BuiltinConstantDocumentation(item.Label); ok {
			item.Documentation = lsp.MarkupContent{
				Kind:  "markdown",
				Value: markdownVBScriptSignature("Const "+item.Label+" As "+typeName, summary),
			}
		}
	}
	return item
}

func (s *Server) enrichVBScriptBuiltInHover(text string, offset int, hover *lsp.Hover) *lsp.Hover {
	if hover == nil {
		return hover
	}
	switch strings.ToLower(vbscript.WordAt(text, offset)) {
	case "datepart":
		hover.Contents = lsp.MarkupContent{Kind: "markdown", Value: markdownVBScriptSignature("Function DatePart(interval, date, firstDayOfWeek, firstWeekOfYear) As Number", s.datePartDocumentation())}
	case "buffer":
		if strings.Contains(markupText(hover.Contents), "Response.Buffer") {
			hover.Contents = lsp.MarkupContent{Kind: "markdown", Value: markdownVBScriptSignature("property Response.Buffer As Boolean", s.responseBufferDocumentation())}
		}
	case "adinteger":
		hover.Contents = lsp.MarkupContent{Kind: "markdown", Value: markdownVBScriptSignature("Const adInteger As Number", s.adIntegerDocumentation())}
	}
	return hover
}

func markdownVBScriptSignature(signature, documentation string) string {
	signature = strings.TrimSpace(signature)
	markdown := "```vbscript\n" + signature + "\n```"
	if strings.TrimSpace(documentation) != "" {
		markdown += "\n\n" + documentation
	}
	return markdown
}

func (s *Server) enrichVBScriptBuiltInSignatureHelp(help *lsp.SignatureHelp) *lsp.SignatureHelp {
	if help == nil || len(help.Signatures) == 0 || !strings.HasPrefix(help.Signatures[0].Label, "DatePart(") {
		return help
	}
	help.Signatures[0].Documentation = s.datePartDocumentation()
	for index, parameter := range help.Signatures[0].Parameters {
		name := parameterNameFromLabel(parameter.Label)
		if doc := s.datePartParameterDocumentation(name); doc != "" {
			help.Signatures[0].Parameters[index].Documentation = doc
		}
	}
	return help
}

func (s *Server) datePartDocumentation() string {
	if s.isJapanese() {
		return "date expression から指定した interval part を返します。\n\n**パラメーター**\n\n- `interval`: 返す interval code。\n- `date`: 評価する date expression。\n- `firstDayOfWeek`: 週の最初の曜日。\n- `firstWeekOfYear`: 年の最初の週。\n\n**戻り値**\n\ndate の指定部分。"
	}
	return "Returns the requested interval part of a date expression.\n\n**Parameters**\n\n- `interval`: Interval code to return.\n- `date`: Date expression to evaluate.\n- `firstDayOfWeek`: First day of the week.\n- `firstWeekOfYear`: First week of the year.\n\n**Returns**\n\nRequested part of the date."
}

func (s *Server) datePartParameterDocumentation(name string) string {
	if s.isJapanese() {
		switch strings.ToLower(name) {
		case "interval":
			return "返す interval code。"
		case "date":
			return "評価する date expression。"
		case "firstdayofweek":
			return "週の最初の曜日。"
		case "firstweekofyear":
			return "年の最初の週。"
		}
		return ""
	}
	switch strings.ToLower(name) {
	case "interval":
		return "Interval code to return."
	case "date":
		return "Date expression to evaluate."
	case "firstdayofweek":
		return "First day of the week."
	case "firstweekofyear":
		return "First week of the year."
	default:
		return ""
	}
}

func (s *Server) responseBufferDocumentation() string {
	if s.isJapanese() {
		return "ASP の page output を buffer してから client へ送るかを制御します。\n\n**値**\n\nhtml tag より前、または response output より前に設定します。"
	}
	return "Controls whether ASP buffers page output before sending it to the client.\n\n**Values**\n\nSet this before any HTML tag or response output is sent."
}

func (s *Server) serverExecuteDocumentation() string {
	if s.isJapanese() {
		return "別の ASP page を実行し、完了後に現在の page へ戻ります。"
	}
	return "Runs another ASP page and returns to the current page after it finishes."
}

func (s *Server) adIntegerDocumentation() string {
	if s.isJapanese() {
		return "ADO data type constant for a 32-bit signed integer value.\n\n**値**\n\n32-bit signed integer value。"
	}
	return "ADO data type constant for a 32-bit signed integer value."
}

func markupText(contents any) string {
	switch value := contents.(type) {
	case lsp.MarkupContent:
		return value.Value
	case string:
		return value
	default:
		return ""
	}
}
