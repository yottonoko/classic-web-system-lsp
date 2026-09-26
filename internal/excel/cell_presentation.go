package excel

import "fmt"

type cellPresentation string

const (
	cellPresentationHeader      cellPresentation = "header"
	cellPresentationSection     cellPresentation = "section"
	cellPresentationSideLabel   cellPresentation = "side-label"
	cellPresentationSideValue   cellPresentation = "side-value"
	cellPresentationToneDanger  cellPresentation = "tone-danger"
	cellPresentationToneGood    cellPresentation = "tone-good"
	cellPresentationToneInfo    cellPresentation = "tone-info"
	cellPresentationToneNeutral cellPresentation = "tone-neutral"
	cellPresentationToneWarning cellPresentation = "tone-warning"
	cellPresentationPercent     cellPresentation = "percent"
)

// StyledCell carries the cell-level presentation used by the legacy
// TypeScript workbook without changing the primitive Cell API.
type StyledCell struct {
	Value      Cell
	Style      cellPresentation
	ColumnSpan int
}

func presentedCellValue(cell Cell) Cell {
	if styled, ok := cell.(StyledCell); ok {
		return styled.Value
	}
	return cell
}

func headerCells(values []Cell) []Cell {
	result := make([]Cell, len(values))
	for index, value := range values {
		result[index] = StyledCell{Value: presentedCellValue(value), Style: cellPresentationHeader}
	}
	return result
}

func describedTableRows(locale Locale, descriptionKey string, headers []Cell, body [][]Cell) [][]Cell {
	tableRows := make([][]Cell, 0, len(body)+1)
	tableRows = append(tableRows, headerCells(headers))
	tableRows = append(tableRows, body...)
	sideRows := tableSideDescriptionRows(locale, descriptionKey, headers)
	rowCount := max(len(tableRows), len(sideRows)+1)
	result := make([][]Cell, rowCount)
	for index := 0; index < rowCount; index++ {
		if index < len(tableRows) {
			result[index] = append([]Cell(nil), tableRows[index]...)
		}
		if index == 0 || index-1 >= len(sideRows) {
			continue
		}
		for len(result[index]) < len(headers) {
			result[index] = append(result[index], nil)
		}
		result[index] = append(result[index], nil)
		result[index] = append(result[index], sideRows[index-1]...)
	}
	return result
}

func describedSectionRows(title string, locale Locale, descriptionKey string, headers []Cell, body [][]Cell) [][]Cell {
	section := make([]Cell, len(headers))
	if len(headers) > 0 {
		section[0] = StyledCell{Value: title, Style: cellPresentationSection, ColumnSpan: len(headers)}
	}
	return append([][]Cell{section}, describedTableRows(locale, descriptionKey, headers, body)...)
}

func tableSideDescriptionRows(locale Locale, descriptionKey string, headers []Cell) [][]Cell {
	description := analysisTableDescription(locale, descriptionKey)
	rows := [][]Cell{sideDescriptionRow(text(locale, "tableDescription"), description)}
	for _, header := range headers {
		name := fmt.Sprint(presentedCellValue(header))
		rows = append(rows, sideDescriptionRow(name, headerDescription(locale, name)))
	}
	return rows
}

func sideDescriptionRow(label, description string) []Cell {
	return []Cell{
		StyledCell{Value: label, Style: cellPresentationSideLabel},
		StyledCell{Value: description, Style: cellPresentationSideValue},
	}
}

func toneCell(value Cell, tone cellPresentation) Cell {
	return StyledCell{Value: value, Style: tone}
}

func percentCell(value float64) Cell {
	return StyledCell{Value: value, Style: cellPresentationPercent}
}

func analysisTableDescription(locale Locale, key string) string {
	if locale == LocaleEnglish {
		if value, ok := enTableDescriptions[key]; ok {
			return value
		}
		return key
	}
	if value, ok := jaTableDescriptions[key]; ok {
		return value
	}
	return key
}

func headerDescription(locale Locale, header string) string {
	for key, descriptions := range headerDescriptions {
		if text(locale, key) == header {
			if locale == LocaleEnglish {
				return descriptions.en
			}
			return descriptions.ja
		}
	}
	if locale == LocaleEnglish {
		return "Value for " + header + "."
	}
	return header + " の値です。"
}

type localizedDescription struct {
	en string
	ja string
}

var headerDescriptions = map[string]localizedDescription{
	"scope":                {"Analysis scope used for this workbook.", "この workbook で使った解析範囲です。"},
	"value":                {"Value for the metric or setting named in the first column.", "1列目の項目や設定に対応する値です。"},
	"setting":              {"Configuration setting or analysis option name.", "設定または解析 option の名前です。"},
	"direction":            {"Relationship direction from the exported target file.", "出力対象ファイルから見た include 関係の方向です。"},
	"depth":                {"Include graph distance for this relationship.", "この関係の include graph 上の距離です。"},
	"metric":               {"Metric, issue, or grouping name.", "指標、確認項目、分類の名前です。"},
	"total":                {"Total number of matching items.", "条件に一致した項目の合計数です。"},
	"usedCount":            {"Number of declarations that have detected usages.", "使用が検出された宣言の数です。"},
	"unusedRate":           {"Share of declarations that have no detected usages.", "使用が検出されなかった宣言の比率です。"},
	"bar":                  {"Compact visual bar for comparing counts.", "件数を比較するための簡易棒グラフです。"},
	"file":                 {"File that owns the row's item.", "この行の項目を持つファイルです。"},
	"exists":               {"Whether the include target exists on disk.", "include 先が disk 上に存在するかです。"},
	"rootFile":             {"Whether the file is the exported target file.", "出力対象ファイルかどうかです。"},
	"includesOut":          {"Number of outgoing include links from the file.", "このファイルから出ている include 数です。"},
	"includedBy":           {"Number of files that include this file.", "このファイルを include しているファイル数です。"},
	"declarationCount":     {"Number of declarations found for the file.", "このファイルで見つかった宣言数です。"},
	"sourceFile":           {"File that contains the include directive.", "include directive が書かれているファイルです。"},
	"includePath":          {"Path text written in the include directive.", "include directive に書かれた path 文字列です。"},
	"includeMode":          {"Include mode, such as file or virtual.", "file や virtual などの include mode です。"},
	"resolvedTarget":       {"Resolved include target file.", "解決された include 先ファイルです。"},
	"actualPath":           {"Filesystem path found during include resolution.", "include 解決で見つかった実 filesystem path です。"},
	"pathCaseMatches":      {"Whether the include path casing matches the actual path.", "include path の大文字小文字が実 path と一致するかです。"},
	"line":                 {"One-based line number for the range.", "範囲の1始まりの行番号です。"},
	"column":               {"One-based column number for the range.", "範囲の1始まりの列番号です。"},
	"name":                 {"Declaration or item name.", "宣言や項目の名前です。"},
	"kind":                 {"Declaration or graph item kind.", "宣言や graph 項目の種別です。"},
	"memberOf":             {"Owning class or object, when the item is a member.", "member の場合の所有 class や object です。"},
	"bindingScope":         {"Scope where the declaration is bound.", "宣言が束縛される scope です。"},
	"procedureKind":        {"Procedure form, such as Function, Sub, or Property.", "Function、Sub、Property などの procedure 形式です。"},
	"inferredType":         {"Inferred or declared type. Unknown type-capable items use Variant.", "宣言または推論された型です。不明な型付き項目は Variant です。"},
	"returnType":           {"Inferred or declared return type. Unknown Function/Property returns use Variant.", "宣言または推論された戻り値の型です。不明な Function/Property は Variant です。"},
	"parameters":           {"Procedure parameters with inferred types; unknown parameter types use Variant.", "procedure の引数と推論型です。不明な引数型は Variant です。"},
	"implicit":             {"Whether the declaration was inferred from implicit VBScript usage.", "VBScript の暗黙使用から推定された宣言かどうかです。"},
	"array":                {"Array kind and dimensions, when detected.", "検出できた配列種別と次元です。"},
	"referenceCount":       {"Detected read/reference usage count.", "読み取り/参照として検出された使用数です。"},
	"assignmentCount":      {"Detected assignment/write usage count.", "代入/書き込みとして検出された使用数です。"},
	"callCount":            {"Detected call usage count.", "呼び出しとして検出された使用数です。"},
	"status":               {"Review status for the row.", "この行の確認状態です。"},
	"usageKind":            {"Usage link category.", "使用 link の分類です。"},
	"role":                 {"Usage role, such as read, write, or call.", "read、write、call などの使用 role です。"},
	"usageFile":            {"File where the usage occurs.", "使用箇所があるファイルです。"},
	"usageOwner":           {"Graph node that owns the usage.", "使用箇所を所有する graph node です。"},
	"declarationFile":      {"File where the referenced declaration is defined.", "参照先宣言が定義されているファイルです。"},
	"declarationName":      {"Referenced declaration name.", "参照先宣言の名前です。"},
	"declarationKind":      {"Referenced declaration kind.", "参照先宣言の種別です。"},
	"includeFile":          {"Included file that owns the referenced symbol.", "参照先 symbol を持つ include ファイルです。"},
	"includedSymbol":       {"Symbol declared in an included file.", "include 先ファイルで宣言された symbol です。"},
	"includedKind":         {"Kind of the included symbol.", "include 先 symbol の種別です。"},
	"usedFromFile":         {"File that uses the included symbol.", "include 先 symbol を使っているファイルです。"},
	"receiver":             {"Receiver expression before the member access.", "member access の receiver 式です。"},
	"memberName":           {"Member name after the receiver.", "receiver の後ろの member 名です。"},
	"expression":           {"Full member expression text.", "member 式全体です。"},
	"implicitGlobalFile":   {"File that owns the implicit global variable.", "暗黙 global 変数を持つファイルです。"},
	"implicitGlobalName":   {"Implicit global variable name.", "暗黙 global 変数の名前です。"},
	"assignmentFile":       {"File containing a possible assignment.", "代入候補があるファイルです。"},
	"assignmentTarget":     {"Assignment target name.", "代入対象の名前です。"},
	"assignmentTargetFile": {"File that owns the assignment target.", "代入対象を持つファイルです。"},
	"includeDepth":         {"Distance through include parents from the target file.", "対象ファイルから include 元へたどった距離です。"},
	"count":                {"Count represented by this row.", "この行が表す件数です。"},
	"risk":                 {"Review risk or issue category.", "確認リスクや問題分類です。"},
	"action":               {"Suggested review action.", "推奨される確認作業です。"},
}

var enTableDescriptions = map[string]string{
	"summary":                   "High-level counts and generation metadata for the exported target file analysis.",
	"includeTree":               "Include relationships that belong to the target file's descendants, ancestors, and related ancestor descendant trees.",
	"analysisSettings":          "Analysis settings used while generating this workbook.",
	"reviewPriority":            "Review-oriented summary of risks that usually need manual confirmation.",
	"externalReferenceSummary":  "Declaration counts grouped by kind, with how many are used or unused.",
	"includeUsageSummary":       "Usage counts for symbols declared in included files and used by the target file.",
	"topReferencedDeclarations": "Most-used declarations in the target file, ordered by usage count.",
	"unusedByKind":              "Unused declaration totals grouped by declaration kind.",
	"chartData":                 "Source data used by the workbook charts.",
	"declarations":              "Declarations that belong to the exported target file only.",
	"internalUsages":            "Usages inside the exported target file that point to target-file declarations.",
	"externalFileUsages":        "Usages from other files that point to declarations in the exported target file.",
	"includedSymbolUsages":      "Usages in the target file that point to declarations from included files.",
	"memberUsages":              "Member expression usages found in the target file.",
	"implicitGlobals":           "Implicit global variables inferred for the target file.",
	"implicitGlobalAssignments": "Possible assignments from include-related context into inferred implicit globals.",
	"unused":                    "Target-file declarations that have no detected usages.",
	"unresolved":                "Unresolved references, calls, and assignments found in the target file.",
}

var jaTableDescriptions = map[string]string{
	"summary":                   "出力対象ファイルの解析件数と生成情報の概要です。",
	"includeTree":               "対象ファイルの子孫、祖先、祖先から伸びる親戚 include tree に属する include 関係です。",
	"analysisSettings":          "この workbook を生成したときに使った解析設定です。",
	"reviewPriority":            "手作業で確認した方がよいリスク項目をまとめた表です。",
	"externalReferenceSummary":  "宣言種別ごとの総数、使用あり、未使用の内訳です。",
	"includeUsageSummary":       "対象ファイルが include 先の宣言をどの種類で使っているかの集計です。",
	"topReferencedDeclarations": "対象ファイル内でよく使われている宣言を使用数順に並べた表です。",
	"unusedByKind":              "未使用宣言を宣言種別ごとに集計した表です。",
	"chartData":                 "ワークブック内のグラフに使う元データです。",
	"declarations":              "出力対象ファイル自身にある宣言だけを並べた表です。",
	"internalUsages":            "出力対象ファイル内から同じファイル内の宣言へ向く使用箇所です。",
	"externalFileUsages":        "他ファイルから出力対象ファイル内の宣言へ向く使用箇所です。",
	"includedSymbolUsages":      "対象ファイルから include 先の宣言へ向く使用箇所です。",
	"memberUsages":              "対象ファイル内で見つかったメンバー式の使用箇所です。",
	"implicitGlobals":           "対象ファイルで推定された暗黙 global 変数です。",
	"implicitGlobalAssignments": "include 関係から暗黙 global へ代入している可能性がある箇所です。",
	"unused":                    "使用が検出されなかった対象ファイル内の宣言です。",
	"unresolved":                "対象ファイル内で名前解決できなかった参照、呼び出し、代入です。",
}
