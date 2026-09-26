package excel

var jaAnalysisText = map[string]string{
	"summary":                   "概要",
	"includeTree":               "インクルードツリー",
	"includeTreeDiagram":        "インクルードツリー図",
	"analysisSummary":           "分析サマリ",
	"chartData":                 "チャート元データ",
	"declarationsSheet":         "宣言",
	"fileLocalUsage":            "ファイル内使用",
	"externalUsage":             "外部ファイルからの使用",
	"includedUsage":             "include 先シンボル使用",
	"memberUsage":               "メンバー使用",
	"implicitGlobalsSheet":      "暗黙global変数",
	"implicitAssignments":       "暗黙global変数代入候補",
	"unusedSheet":               "未使用",
	"unresolvedSheet":           "未解決",
	"name":                      "名前",
	"value":                     "値",
	"tableDescription":          "表の説明",
	"scope":                     "解析範囲",
	"root":                      "ルート",
	"generatedAt":               "生成時刻",
	"declarations":              "宣言数",
	"references":                "参照数",
	"assignments":               "代入数",
	"calls":                     "呼び出し数",
	"includes":                  "include 数",
	"unresolved":                "未解決数",
	"implicitGlobals":           "暗黙global変数数",
	"implicitGlobalAssignments": "暗黙global変数代入候補数",
	"unused":                    "未使用数",
	"workspace":                 "ワークスペース",
	"document":                  "ファイル",
	"folder":                    "フォルダー",
	"analysisFileCount":         "解析 file 数",
	"includeGlobs":              "一時 include glob",
	"excludeGlobs":              "一時 exclude glob",
	"includeRelatedIncludeTreesForUnresolved": "親戚 include tree 解析",
	"forceRelatedIncludeTreeAnalysis":         "親戚 include tree 解析の強制",
	"skipTypeInference":                       "型推論を skip",
	"includeAnalysisTypeDetails":              "エディター推論型の詳細",
	"excelLocale":                             "Excel 言語",
	"enabled":                                 "有効",
	"disabled":                                "無効",
	"forced":                                  "強制あり",
	"notForced":                               "強制なし",
	"auto":                                    "自動",
	"direction":                               "方向",
	"depth":                                   "深さ",
	"sourceFile":                              "参照元ファイル",
	"includeFile":                             "include ファイル",
	"includePath":                             "include path",
	"includeMode":                             "mode",
	"exists":                                  "存在",
	"resolvedTarget":                          "解決先",
	"descendant":                              "子孫",
	"ancestor":                                "祖先",
	"relative":                                "親戚",
	"yes":                                     "あり",
	"no":                                      "なし",
	"item":                                    "項目",
	"count":                                   "件数",
	"status":                                  "状態",
	"needsReview":                             "要確認",
	"ok":                                      "なし",
	"unusedDeclarations":                      "未使用の宣言",
	"file":                                    "ファイル",
	"kind":                                    "種別",
	"scopeColumn":                             "scope",
	"inferredType":                            "推論型",
	"line":                                    "行",
	"column":                                  "列",
	"usageKind":                               "使用種別",
	"role":                                    "role",
	"source":                                  "使用元",
	"target":                                  "対象",
	"usageFile":                               "使用ファイル",
	"usageOwner":                              "使用元ノード",
	"declarationFile":                         "宣言ファイル",
	"declarationName":                         "宣言名",
	"declarationKind":                         "宣言種別",
	"memberOf":                                "所属",
	"bindingScope":                            "scope",
	"procedureKind":                           "procedure kind",
	"returnType":                              "戻り値の型",
	"parameters":                              "引数",
	"implicit":                                "暗黙宣言",
	"array":                                   "配列",
	"receiver":                                "receiver",
	"memberName":                              "member",
	"expression":                              "式",
	"implicitGlobalFile":                      "暗黙global変数ファイル",
	"implicitGlobalName":                      "暗黙global変数",
	"assignmentFile":                          "代入ファイル",
	"assignmentTarget":                        "代入対象",
	"assignmentTargetFile":                    "代入対象ファイル",
	"includeDepth":                            "include 元距離",
	"usedFromFile":                            "使用元ファイル",
	"variable":                                "変数",
	"function":                                "関数",
	"sub":                                     "Sub",
	"constant":                                "定数",
	"class":                                   "クラス",
	"parameter":                               "パラメーター",
	"global":                                  "グローバル",
	"local":                                   "ローカル",
	"used":                                    "使用あり",
	"unusedStatus":                            "未使用",
	"read":                                    "読み取り",
	"write":                                   "書き込み",
	"call":                                    "呼び出し",
	"member":                                  "メンバー",
	"unresolvedFunction":                      "未解決Function/Sub",
	"unresolvedReference":                     "未解決参照",
	"metric":                                  "指標",
	"action":                                  "対応",
	"reviewPriority":                          "確認優先",
	"externalReferenceSummary":                "使用サマリ",
	"includeUsageSummary":                     "include 先の使用",
	"topReferencedDeclarations":               "よく使われている宣言",
	"analysisCharts":                          "グラフ",
	"unusedByKind":                            "種別ごとの未使用",
	"unreferencedDeclarations":                "未使用の宣言",
	"externalUsageCount":                      "他ファイルからの使用数",
	"includedUsageCount":                      "include 先シンボル使用数",
	"issueSummary":                            "確認項目",
	"present":                                 "あり",
	"none":                                    "なし",
	"total":                                   "合計",
	"usedCount":                               "使用あり",
	"unusedCount":                             "未使用",
	"usageCount":                              "使用数",
	"referenceCount":                          "参照数",
	"assignmentCount":                         "代入数",
	"callCount":                               "呼び出し数",
	"unusedRate":                              "未使用率",
	"bar":                                     "棒グラフ",
	"reviewUnusedAction":                      "未使用 sheet で削除可否を確認",
	"reviewUnresolvedAction":                  "未解決 sheet で名前解決を確認",
	"reviewImplicitGlobalsAction":             "暗黙global変数 sheet で宣言漏れか確認",
	"reviewImplicitGlobalAssignmentsAction":   "暗黙global変数代入候補 sheet で include 元からの代入を確認",
	"reviewExternalUsagesAction":              "外部ファイルからの使用 sheet で利用元を確認",
	"reviewMissingExternalUsagesAction":       "対象ファイルは他ファイルから使われていない可能性あり",
	"reviewIncludedUsagesAction":              "include 先シンボル使用 sheet で include 依存を確認",
	"reviewMissingIncludedUsagesAction":       "include 先シンボルの使用なし",
}

var enAnalysisText = map[string]string{
	"summary":                   "Summary",
	"includeTree":               "Include Tree",
	"includeTreeDiagram":        "Include Tree Diagram",
	"analysisSummary":           "Analysis Summary",
	"chartData":                 "Chart Data",
	"declarationsSheet":         "Declarations",
	"fileLocalUsage":            "File-local Usage",
	"externalUsage":             "External File Usage",
	"includedUsage":             "Included Symbol Usage",
	"memberUsage":               "Member Usage",
	"implicitGlobalsSheet":      "Implicit Globals",
	"implicitAssignments":       "Implicit Global Assignment",
	"unusedSheet":               "Unused",
	"unresolvedSheet":           "Unresolved",
	"name":                      "Name",
	"value":                     "Value",
	"tableDescription":          "Table description",
	"scope":                     "Scope",
	"root":                      "Root",
	"generatedAt":               "Generated At",
	"declarations":              "Declarations",
	"references":                "References",
	"assignments":               "Assignments",
	"calls":                     "Calls",
	"includes":                  "Includes",
	"unresolved":                "Unresolved",
	"implicitGlobals":           "Implicit Globals",
	"implicitGlobalAssignments": "Implicit Global Assignments",
	"unused":                    "Unused",
	"workspace":                 "Workspace",
	"document":                  "File",
	"folder":                    "Folder",
	"analysisFileCount":         "Analysis file count",
	"includeGlobs":              "Temporary include globs",
	"excludeGlobs":              "Temporary exclude globs",
	"includeRelatedIncludeTreesForUnresolved": "Related include tree analysis",
	"forceRelatedIncludeTreeAnalysis":         "Force related include tree analysis",
	"skipTypeInference":                       "Skip type inference",
	"includeAnalysisTypeDetails":              "Editor inference type details",
	"excelLocale":                             "Excel language",
	"enabled":                                 "Enabled",
	"disabled":                                "Disabled",
	"forced":                                  "Forced",
	"notForced":                               "Not forced",
	"auto":                                    "Auto",
	"direction":                               "Direction",
	"depth":                                   "Depth",
	"sourceFile":                              "Source File",
	"includeFile":                             "Include File",
	"includePath":                             "Include Path",
	"includeMode":                             "Mode",
	"exists":                                  "Exists",
	"resolvedTarget":                          "Resolved Target",
	"descendant":                              "Descendant",
	"ancestor":                                "Ancestor",
	"relative":                                "Relative",
	"yes":                                     "Yes",
	"no":                                      "No",
	"item":                                    "Item",
	"status":                                  "Status",
	"needsReview":                             "Needs review",
	"ok":                                      "None",
	"unusedDeclarations":                      "Unused declarations",
	"file":                                    "File",
	"kind":                                    "Kind",
	"scopeColumn":                             "Scope",
	"inferredType":                            "Inferred Type",
	"line":                                    "Line",
	"column":                                  "Column",
	"usageKind":                               "Usage Kind",
	"role":                                    "Role",
	"source":                                  "Source",
	"target":                                  "Target",
	"usageFile":                               "Usage File",
	"usageOwner":                              "Usage owner",
	"declarationFile":                         "Declaration File",
	"declarationName":                         "Declaration",
	"declarationKind":                         "Declaration kind",
	"memberOf":                                "Member of",
	"bindingScope":                            "Binding scope",
	"procedureKind":                           "Procedure kind",
	"returnType":                              "Return type",
	"parameters":                              "Parameters",
	"implicit":                                "Implicit",
	"array":                                   "Array",
	"receiver":                                "Receiver",
	"memberName":                              "Member",
	"expression":                              "Expression",
	"implicitGlobalFile":                      "Implicit Global File",
	"implicitGlobalName":                      "Implicit Global",
	"assignmentFile":                          "Assignment File",
	"assignmentTarget":                        "Assignment Target",
	"assignmentTargetFile":                    "Assignment target file",
	"includeDepth":                            "Include depth",
	"count":                                   "Count",
	"usedFromFile":                            "Used From File",
	"variable":                                "Variable",
	"function":                                "Function",
	"sub":                                     "Sub",
	"constant":                                "Constant",
	"class":                                   "Class",
	"parameter":                               "Parameter",
	"global":                                  "Global",
	"local":                                   "Local",
	"used":                                    "Used",
	"unusedStatus":                            "Unused",
	"read":                                    "Read",
	"write":                                   "Write",
	"call":                                    "Call",
	"member":                                  "Member",
	"unresolvedFunction":                      "Unresolved Function/Sub",
	"unresolvedReference":                     "Unresolved reference",
	"metric":                                  "Metric",
	"action":                                  "Action",
	"reviewPriority":                          "Review priority",
	"externalReferenceSummary":                "Usage summary",
	"includeUsageSummary":                     "Included file usage",
	"topReferencedDeclarations":               "Top referenced declarations",
	"analysisCharts":                          "Charts",
	"unusedByKind":                            "Unused by kind",
	"unreferencedDeclarations":                "Unused declarations",
	"externalUsageCount":                      "External usages",
	"includedUsageCount":                      "Included symbol usages",
	"issueSummary":                            "Issue summary",
	"present":                                 "Present",
	"none":                                    "None",
	"total":                                   "Total",
	"usedCount":                               "Used",
	"unusedCount":                             "Unused",
	"usageCount":                              "Usage count",
	"referenceCount":                          "References",
	"assignmentCount":                         "Assignments",
	"callCount":                               "Calls",
	"unusedRate":                              "Unused rate",
	"bar":                                     "Bar",
	"reviewUnusedAction":                      "Review the Unused sheet before removing declarations.",
	"reviewUnresolvedAction":                  "Review the Unresolved sheet and fix name resolution.",
	"reviewImplicitGlobalsAction":             "Review the Implicit Globals sheet for missing declarations.",
	"reviewImplicitGlobalAssignmentsAction":   "Review implicit global assignment candidates.",
	"reviewExternalUsagesAction":              "Review the External File Usage sheet for other-file callers.",
	"reviewMissingExternalUsagesAction":       "No other-file usages were found for the target file.",
	"reviewIncludedUsagesAction":              "Review the Included Symbol Usage sheet for include dependencies.",
	"reviewMissingIncludedUsagesAction":       "No included-file symbol usages were found.",
}

func text(locale Locale, key string) string {
	if locale == LocaleEnglish {
		if value, ok := enAnalysisText[key]; ok {
			return value
		}
	}
	if value, ok := jaAnalysisText[key]; ok {
		return value
	}
	return key
}

func scopeLabel(scope string, locale Locale) string {
	switch scope {
	case "workspace":
		return text(locale, "workspace")
	case "folder":
		return text(locale, "folder")
	default:
		return text(locale, "document")
	}
}

func enabledText(enabled bool, locale Locale) string {
	if enabled {
		return text(locale, "enabled")
	}
	return text(locale, "disabled")
}

func forcedText(forced bool, locale Locale) string {
	if forced {
		return text(locale, "forced")
	}
	return text(locale, "notForced")
}

func excelLocaleText(value string, locale Locale) string {
	if value == "" || value == "auto" {
		return text(locale, "auto")
	}
	return value
}

func reviewStatus(count int, locale Locale) string {
	if count > 0 {
		return text(locale, "needsReview")
	}
	return text(locale, "ok")
}

func declarationKindLabel(kind string, locale Locale) string {
	switch kind {
	case "variable":
		return text(locale, "variable")
	case "function":
		return text(locale, "function")
	case "sub":
		return text(locale, "sub")
	case "constant":
		return text(locale, "constant")
	case "class":
		return text(locale, "class")
	case "parameter":
		return text(locale, "parameter")
	default:
		if kind == "" {
			return ""
		}
		return kind
	}
}

func scopeText(scope string, locale Locale) string {
	switch scope {
	case "global":
		return text(locale, "global")
	case "local":
		return text(locale, "local")
	default:
		return scope
	}
}

func usedStatus(counts usageCounts, locale Locale) string {
	if counts.references+counts.assignments+counts.calls > 0 {
		return text(locale, "used")
	}
	return text(locale, "unusedStatus")
}

func usageKindLabel(kind string, locale Locale) string {
	if locale == LocaleEnglish {
		switch kind {
		case "references":
			return "Reference"
		case "assignments":
			return "Assignment"
		case "calls":
			return "Call"
		}
	}
	switch kind {
	case "references":
		return "参照"
	case "assignments":
		return "代入"
	case "calls":
		return "呼び出し"
	case "unresolvedReference":
		return text(locale, "unresolvedReference")
	default:
		return kind
	}
}

func roleLabel(role string, locale Locale) string {
	switch role {
	case "read":
		return text(locale, "read")
	case "write":
		return text(locale, "write")
	case "function", "call":
		return text(locale, "call")
	case "member":
		return text(locale, "member")
	default:
		return role
	}
}

func unresolvedKindLabel(group string, locale Locale) string {
	if group == "unresolvedFunction" {
		return text(locale, "unresolvedFunction")
	}
	if group == "unresolvedReference" {
		return text(locale, "unresolvedReference")
	}
	return group
}
