package lspserver

import (
	"fmt"
	"strings"

	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func normalizeLocale(value string) string {
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "ja") {
		return "ja"
	}
	if strings.HasPrefix(lower, "en") {
		return "en"
	}
	return ""
}

func normalizeConfiguredLocale(value string, clientLocale string) string {
	if strings.EqualFold(strings.TrimSpace(value), "auto") {
		return clientLocale
	}
	return normalizeLocale(value)
}

func normalizeExcelLocale(value string) string {
	lower := strings.ToLower(strings.TrimSpace(value))
	if lower == "auto" {
		return "auto"
	}
	return normalizeLocale(lower)
}

func (s *Server) isJapanese() bool {
	return s.settings.Locale == "ja"
}

func (s *Server) workspaceIndexFailureMessage(err error) string {
	cause := "unknown error"
	if err != nil {
		cause = err.Error()
	}
	if s.isJapanese() {
		return "ワークスペース インデックスの作成に失敗しました: " + cause
	}
	return "Workspace index failed: " + cause
}

func (s *Server) missingIncludeMessage(path string) string {
	switch s.settings.Locale {
	case "ja":
		return "include file '" + path + "' を解決できません。"
	case "en":
		return "Include file '" + path + "' could not be resolved."
	default:
		return "Include file '" + path + "' could not be resolved."
	}
}

func (s *Server) localizeDiagnostics(diagnostics []lsp.Diagnostic) []lsp.Diagnostic {
	localized := make([]lsp.Diagnostic, len(diagnostics))
	copy(localized, diagnostics)
	for i := range localized {
		diagnostic := &localized[i]
		ja := s.isJapanese()
		switch diagnostic.Source {
		case "asp-lsp-vbscript":
			name := diagnosticDataText(*diagnostic, "name")
			if name == "" {
				name = firstQuotedDiagnosticText(diagnostic.Message)
			}
			if ja {
				diagnostic.Message = "'" + name + "' は Option Explicit のもとで宣言されていません。"
			} else {
				diagnostic.Message = "'" + name + "' is not declared under Option Explicit."
			}
		case "asp-lsp-vbscript-unused":
			name := diagnosticDataText(*diagnostic, "name")
			if name == "" {
				name = firstQuotedDiagnosticText(diagnostic.Message)
			}
			parameter := diagnosticDataText(*diagnostic, "kind") == "parameter" || strings.HasPrefix(diagnostic.Message, "Parameter ")
			if ja && parameter {
				diagnostic.Message = "パラメーター '" + name + "' は使われていません。"
			} else if ja {
				diagnostic.Message = "'" + name + "' は宣言されていますが使われていません。"
			} else if parameter {
				diagnostic.Message = "Parameter '" + name + "' is never used."
			} else {
				diagnostic.Message = "'" + name + "' is declared but never used."
			}
		case "asp-lsp-vbscript-dead-code":
			if ja {
				diagnostic.Message = "この VBScript code には到達できません。"
			} else {
				diagnostic.Message = "This VBScript code is unreachable."
			}
		case "asp-lsp-vbscript-syntax":
			localizeVBScriptSyntaxDiagnostic(diagnostic, ja)
		case "asp-lsp-vbscript-type":
			localizeVBScriptTypeDiagnostic(diagnostic, ja)
		case "asp-lsp-vbscript-naming":
			localizeVBScriptNamingDiagnostic(diagnostic, ja)
		case "asp-lsp-include":
			localizeIncludeDiagnostic(diagnostic, ja)
		case "asp-lsp-html":
			diagnostic.Message = appendEmbeddedDiagnosticFix(diagnostic.Message, htmlDiagnosticFix(diagnostic.Message), ja)
		case "asp-lsp-css":
			diagnostic.Message = appendEmbeddedDiagnosticFix(diagnostic.Message, cssDiagnosticFix(*diagnostic), ja)
		case "asp-lsp-go":
			if strings.Contains(diagnostic.Message, "%>") {
				if ja {
					diagnostic.Message = "Classic ASP ブロックに閉じ区切り %> がありません。"
				} else {
					diagnostic.Message = "Classic ASP block is missing a closing %> delimiter."
				}
			}
		}
	}
	return localized
}

func appendEmbeddedDiagnosticFix(message, english string, ja bool) string {
	if english == "" || strings.Contains(message, "Fix:") || strings.Contains(message, "修正方法:") {
		return message
	}
	if ja {
		return message + " 修正方法: " + embeddedDiagnosticFixJapanese(english)
	}
	return message + " Fix: " + english
}

func embeddedDiagnosticFixJapanese(english string) string {
	switch english {
	case "Ensure the tag name or attribute name contains only valid HTML characters, and keep VBScript output inside a valid tag name or quoted attribute value.":
		return "タグ名・属性名の不要な文字を削除し、VBScript の出力は有効なタグ名か引用符内の属性値にしてください。"
	case "Put a tag name immediately after '<'; do not place a VBScript output block where the tag name starts.":
		return "「<」の直後にタグ名を置き、タグ名の位置へ VBScript の出力を置かないでください。"
	case "Add '>' to the end of the tag, or '/>' for a self-closing tag, after any VBScript output.":
		return "タグ末尾に「>」または自己終了タグなら「/>」を追加し、VBScript の出力後もタグを閉じてください。"
	case "Add at least one CSS declaration inside the rule, or remove the empty rule set.":
		return "ルール内に CSS 宣言を1つ以上追加するか、空のルールセットごと削除してください。"
	case "Add a value after ':', or remove the property declaration when no value is needed.":
		return "「:」の後に値を追加するか、値が不要ならプロパティ宣言ごと削除してください。"
	case "Rename the property to a valid CSS property, and check any vendor prefix.":
		return "プロパティ名を有効な CSS 名へ直し、ベンダー接頭辞も確認してください。"
	case "Rename the at-rule to a valid CSS at-rule, or remove the unnecessary at-rule.":
		return "at-rule 名を有効な CSS 名へ直すか、不要な at-rule を削除してください。"
	case "Add '{' after the selector or at-rule header.":
		return "セレクターまたは at-rule の後に「{」を追加してください。"
	case "Add the missing '}' to close the CSS block.":
		return "CSS ブロックを閉じる不足した「}」を追加してください。"
	case "Add ':' between the CSS property name and its value.":
		return "CSS プロパティ名と値の間に「:」を追加してください。"
	case "Add ';' between declarations or at the end of the declaration.":
		return "宣言の間、または宣言末尾に「;」を追加してください。"
	case "Replace the selector with a valid CSS selector.":
		return "有効な CSS セレクターへ置き換えてください。"
	case "Define both 'src' and 'font-family' in the @font-face rule.":
		return "@font-face に「src」と「font-family」の両方を定義してください。"
	case "Check that VBScript output does not break the CSS selector, property name, or property value.":
		return "VBScript の出力が CSS のセレクター・プロパティ名・値を壊していないか確認してください。"
	default:
		return "VBScript の出力を含め、該当箇所が有効な CSS 構文になるように直してください。"
	}
}

func htmlDiagnosticFix(message string) string {
	switch message {
	case "Unexpected character in tag.", "Unexpected character in tag":
		return "Ensure the tag name or attribute name contains only valid HTML characters, and keep VBScript output inside a valid tag name or quoted attribute value."
	case "Tag name must directly follow the open bracket.":
		return "Put a tag name immediately after '<'; do not place a VBScript output block where the tag name starts."
	case "Closing bracket expected.", "Closing bracket missing.":
		return "Add '>' to the end of the tag, or '/>' for a self-closing tag, after any VBScript output."
	default:
		return "Ensure the surrounding HTML tag is complete and that VBScript output produces valid HTML."
	}
}

func cssDiagnosticFix(diagnostic lsp.Diagnostic) string {
	switch fmt.Sprint(diagnostic.Code) {
	case "emptyRules":
		return "Add at least one CSS declaration inside the rule, or remove the empty rule set."
	case "css-propertyvalueexpected":
		return "Add a value after ':', or remove the property declaration when no value is needed."
	case "unknownProperties", "unknownVendorSpecificProperties":
		return "Rename the property to a valid CSS property, and check any vendor prefix."
	case "unknownAtRules":
		return "Rename the at-rule to a valid CSS at-rule, or remove the unnecessary at-rule."
	case "css-lcurlyexpected":
		return "Add '{' after the selector or at-rule header."
	case "css-rcurlyexpected":
		return "Add the missing '}' to close the CSS block."
	case "css-colonexpected":
		return "Add ':' between the CSS property name and its value."
	case "css-semicolonexpected":
		return "Add ';' between declarations or at the end of the declaration."
	case "css-selectorexpected", "css-ruleorselectorexpected":
		return "Replace the selector with a valid CSS selector."
	case "fontFaceProperties":
		return "Define both 'src' and 'font-family' in the @font-face rule."
	default:
		return "Check that VBScript output does not break the CSS selector, property name, or property value."
	}
}

func localizeVBScriptSyntaxDiagnostic(diagnostic *lsp.Diagnostic, ja bool) {
	code := fmt.Sprint(diagnostic.Code)
	switch code {
	case "initializedDeclaration", "typedDeclaration":
		keyword := titleCaseDiagnosticKeyword(diagnosticDataText(*diagnostic, "declarationKind"))
		if keyword == "" {
			keyword = "Dim"
		}
		if code == "initializedDeclaration" && ja {
			diagnostic.Message = "VBScript の " + keyword + " 宣言には初期値を含められません。"
		} else if code == "initializedDeclaration" {
			diagnostic.Message = "VBScript " + keyword + " declarations cannot include initializers."
		} else if ja {
			diagnostic.Message = "VBScript の " + keyword + " 宣言には As 型指定を含められません。"
		} else {
			diagnostic.Message = "VBScript " + keyword + " declarations cannot include As types."
		}
	case "callStatementRequiresParentheses", "expressionCallRequiresParentheses", "statementCallDisallowsParenthesizedArguments":
		name := diagnosticDataText(*diagnostic, "name")
		if name == "" {
			name = firstQuotedDiagnosticText(diagnostic.Message)
		}
		if ja {
			diagnostic.Message = "'" + name + "' の VBScript 呼び出し構文が無効です。"
		} else {
			diagnostic.Message = "VBScript call syntax is invalid for '" + name + "'."
		}
	case "missingThen":
		diagnostic.Message = chooseDiagnosticMessage(ja, "VBScript If statements must include Then.", "VBScript の If statement には Then が必要です。")
	case "missingIfCondition":
		diagnostic.Message = chooseDiagnosticMessage(ja, "VBScript If statements must include a condition.", "VBScript の If statement には条件式が必要です。")
	case "invalidIfCondition":
		diagnostic.Message = chooseDiagnosticMessage(ja, "VBScript If condition syntax is invalid.", "VBScript の If 条件式の構文が無効です。")
	case "missingEndIf":
		diagnostic.Message = chooseDiagnosticMessage(ja, "VBScript multiline If block is missing End If.", "VBScript の multiline If block に End If がありません。")
	case "invalidOnErrorStatement":
		diagnostic.Message = chooseDiagnosticMessage(ja,
			"VBScript On Error statements must be 'On Error Resume Next' or 'On Error GoTo 0'.",
			"VBScript の On Error statement は 'On Error Resume Next' または 'On Error GoTo 0' にしてください。")
	default:
		if !strings.HasPrefix(code, "missing") {
			return
		}
		kind := diagnosticDataText(*diagnostic, "kind")
		terminator := diagnosticDataText(*diagnostic, "terminator")
		if kind == "" || terminator == "" {
			kind, terminator = syntaxBlockDiagnosticParts(code)
		}
		if kind == "" || terminator == "" {
			return
		}
		if ja {
			diagnostic.Message = "VBScript の " + kind + " block に " + terminator + " がありません。"
		} else {
			diagnostic.Message = "VBScript " + kind + " block is missing " + terminator + "."
		}
	}
}

func localizeVBScriptTypeDiagnostic(diagnostic *lsp.Diagnostic, ja bool) {
	code := fmt.Sprint(diagnostic.Code)
	name := diagnosticDataText(*diagnostic, "name")
	if name == "" {
		name = diagnosticDataText(*diagnostic, "member")
	}
	typeName := diagnosticDataText(*diagnostic, "type")
	if typeName == "" {
		typeName = diagnosticDataText(*diagnostic, "typeName")
	}
	switch code {
	case "setScalar":
		if ja {
			diagnostic.Message = "Set はオブジェクト参照を代入しますが、'" + name + "' は " + typeName + " を受け取っています。"
		} else {
			diagnostic.Message = "Set assigns an object reference, but '" + name + "' receives " + typeName + "."
		}
	case "objectNeedsSet":
		diagnostic.Message = chooseDiagnosticMessage(ja, "Object assignment to '"+name+"' should use Set.", "'"+name+"' へのオブジェクト代入には Set が必要です。")
	case "typeMismatch":
		expected := diagnosticDataText(*diagnostic, "expected")
		actual := diagnosticDataText(*diagnostic, "actual")
		if ja {
			diagnostic.Message = "型が一致しません: '" + name + "' は " + expected + " ですが、" + actual + " が代入されています。"
		} else {
			diagnostic.Message = "Type mismatch: '" + name + "' is " + expected + ", but assigned " + actual + "."
		}
	case "malformedTypeAnnotation":
		annotation := diagnosticDataText(*diagnostic, "annotation")
		diagnostic.Message = chooseDiagnosticMessage(ja,
			"Malformed VBScript type annotation: '"+annotation+"'.",
			"VBScript の型注釈が不正です: '"+annotation+"'。")
	case "argumentCount", "argumentCountMismatch":
		expected := diagnosticDataText(*diagnostic, "expected")
		if minimum := diagnosticDataText(*diagnostic, "expectedMin"); minimum != "" {
			if maximum := diagnosticDataText(*diagnostic, "expectedMax"); maximum != "" {
				expected = minimum
				if minimum != maximum {
					expected += "-" + maximum
				}
			}
		}
		actual := diagnosticDataText(*diagnostic, "actual")
		if ja {
			diagnostic.Message = "'" + name + "' の引数の数が一致しません: 期待値 " + expected + "、実際 " + actual + "。"
		} else {
			diagnostic.Message = "Argument count mismatch for '" + name + "': expected " + expected + ", got " + actual + "."
		}
	case "unknownCall":
		diagnostic.Message = chooseDiagnosticMessage(ja, "Call target '"+name+"' is not known.", "呼び出し先 '"+name+"' は不明です。")
	case "missingMember":
		member := diagnosticDataText(*diagnostic, "member")
		if ja {
			diagnostic.Message = "型 '" + typeName + "' にメンバー '" + member + "' はありません。"
		} else {
			diagnostic.Message = "Type '" + typeName + "' has no member '" + member + "'."
		}
	}
}

func localizeVBScriptNamingDiagnostic(diagnostic *lsp.Diagnostic, ja bool) {
	if fmt.Sprint(diagnostic.Code) != "identifierCase" {
		return
	}
	name := diagnosticDataText(*diagnostic, "name")
	expected := diagnosticDataText(*diagnostic, "expectedName")
	style := diagnosticDataText(*diagnostic, "style")
	if ja {
		diagnostic.Message = "識別子 '" + name + "' は " + style + " casing の '" + expected + "' にしてください。"
	} else {
		diagnostic.Message = "Identifier '" + name + "' should be '" + expected + "' for " + style + " casing."
	}
}

func localizeIncludeDiagnostic(diagnostic *lsp.Diagnostic, ja bool) {
	switch fmt.Sprint(diagnostic.Code) {
	case "include.missing":
		path := diagnosticDataText(*diagnostic, "path")
		if path == "" {
			path = firstQuotedDiagnosticText(diagnostic.Message)
		}
		diagnostic.Message = chooseDiagnosticMessage(ja, "Include file '"+path+"' could not be resolved.", "include file '"+path+"' を解決できません。")
	case "include.pathCaseMismatch":
		path := diagnosticDataText(*diagnostic, "path")
		actual := diagnosticDataText(*diagnostic, "actualPath")
		if ja {
			diagnostic.Message = "include path '" + path + "' は file system 上の大文字小文字 '" + actual + "' と一致していません。"
		} else {
			diagnostic.Message = "Include path '" + path + "' differs from the file system casing '" + actual + "'."
		}
	case "include.currentDocument":
		diagnostic.Message = chooseDiagnosticMessage(ja, "Include file references the current document.", "include file が現在のドキュメントを参照しています。")
	case "include.cycle":
		cycle := diagnosticDataText(*diagnostic, "cycle")
		diagnostic.Message = chooseDiagnosticMessage(ja, "Include cycle detected: "+cycle+".", "include の循環を検出しました: "+cycle+"。")
	}
}

func diagnosticDataText(diagnostic lsp.Diagnostic, key string) string {
	data, ok := diagnostic.Data.(map[string]any)
	if !ok || data[key] == nil {
		return ""
	}
	return fmt.Sprint(data[key])
}

func firstQuotedDiagnosticText(message string) string {
	start := strings.IndexByte(message, '\'')
	if start < 0 {
		return ""
	}
	end := strings.IndexByte(message[start+1:], '\'')
	if end < 0 {
		return ""
	}
	return message[start+1 : start+1+end]
}

func titleCaseDiagnosticKeyword(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	return strings.ToUpper(value[:1]) + value[1:]
}

func chooseDiagnosticMessage(ja bool, english, japanese string) string {
	if ja {
		return japanese
	}
	return english
}

func syntaxBlockDiagnosticParts(code string) (string, string) {
	switch code {
	case "missingEndClass":
		return "Class", "End Class"
	case "missingEndFunction":
		return "Function", "End Function"
	case "missingEndSub":
		return "Sub", "End Sub"
	case "missingEndProperty":
		return "Property", "End Property"
	case "missingEndSelect":
		return "Select Case", "End Select"
	case "missingEndWith":
		return "With", "End With"
	case "missingLoop":
		return "Do", "Loop"
	case "missingWend":
		return "While", "Wend"
	case "missingNext":
		return "For", "Next"
	default:
		return "", ""
	}
}

func (s *Server) declareDimTitle(name string) string {
	if s.isJapanese() {
		return name + " を Dim で宣言"
	}
	return "Declare " + name + " with Dim"
}

func (s *Server) createMissingIncludeTitle(path string) string {
	if s.isJapanese() {
		return "不足している include " + path + " を作成"
	}
	return "Create missing include " + path
}

func (s *Server) autoIncludeCompletionDetail(path string) string {
	if s.isJapanese() {
		return path + " から自動 include"
	}
	return "Auto include from " + path
}

func (s *Server) autoIncludeCodeActionTitle(name, path string) string {
	if s.isJapanese() {
		return name + " のために " + path + " を include"
	}
	return "Include " + path + " for " + name
}

func (s *Server) dimQuickFixTitle(title string) string {
	if !s.isJapanese() {
		return title
	}
	switch title {
	case "Split initialized Dim declaration":
		return "初期化つき Dim 宣言を分割"
	case "Split Dim declarations":
		return "Dim 宣言を分割"
	default:
		return title
	}
}

func embeddedCompletionDetail(kind string, locale string) string {
	if locale == "ja" {
		if kind == "html" {
			return "HTML 補完"
		}
		return "CSS 補完"
	}
	if kind == "html" {
		return "HTML completion"
	}
	return "CSS completion"
}

func embeddedCompletionDocumentation(kind string, locale string) string {
	if locale == "ja" {
		if kind == "html" {
			return "vscode-html-languageservice による補完です。"
		}
		return "vscode-css-languageservice による補完です。"
	}
	if kind == "html" {
		return "Completion provided by vscode-html-languageservice."
	}
	return "Completion provided by vscode-css-languageservice."
}

func (s *Server) unknownCommandMessage(command string) string {
	if s.isJapanese() {
		return "不明なコマンド: " + command
	}
	return "Unknown command: " + command
}
