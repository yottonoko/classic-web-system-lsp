package excel

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/yottonoko/classic-web-system-lsp/internal/graph"
	"github.com/yottonoko/classic-web-system-lsp/internal/lsp"
)

func TestChartPNGUsesPaletteBackedImage(t *testing.T) {
	encoded := chartPNG([][]Cell{{"kind", 3}}, 1, &singlePNGEncoderBufferPool{})
	decoded, err := png.Decode(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if _, ok := decoded.(*image.Paletted); !ok {
		t.Fatalf("chart image type = %T, want *image.Paletted", decoded)
	}
}

func TestCreateAnalysisSheetsSummarizesUsageAndReviewTables(t *testing.T) {
	sheets := CreateAnalysisSheets(testSheetPayload(), LocaleJapanese, AnalysisSheetsOptions{
		GeneratedAt: time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC),
		TargetURI:   "file:///workspace/main.asp",
		Settings: &AnalysisWorkbookSettings{
			ExcelLocale:                             "auto",
			IncludeRelatedIncludeTreesForUnresolved: false,
			ForceRelatedIncludeTreeAnalysis:         false,
			SkipTypeInference:                       false,
			IncludeAnalysisTypeDetails:              true,
		},
	})
	if got := sheetNames(sheets); !reflect.DeepEqual(got, []string{
		"概要",
		"インクルードツリー",
		"インクルードツリー図",
		"分析サマリ",
		"チャート元データ",
		"宣言",
		"ファイル内使用",
		"外部ファイルからの使用",
		"include 先シンボル使用",
		"メンバー使用",
		"暗黙global変数",
		"暗黙global変数代入候補",
		"未使用",
		"未解決",
	}) {
		t.Fatalf("sheet names = %#v", got)
	}
	assertRowsContain(t, sheetRows(t, sheets, "概要"),
		[]Cell{"解析範囲", "ファイル"},
		[]Cell{"ルート", "main.asp"},
		[]Cell{"宣言数", 4},
		[]Cell{"参照数", 2},
		[]Cell{"代入数", 2},
		[]Cell{"呼び出し数", 1},
		[]Cell{"include 数", 1},
		[]Cell{"未解決数", 1},
		[]Cell{"暗黙global変数数", 1},
		[]Cell{"暗黙global変数代入候補数", 1},
		[]Cell{"未使用数", 1},
		[]Cell{"親戚 include tree 解析", "無効"},
		[]Cell{"親戚 include tree 解析の強制", "強制なし"},
		[]Cell{"型推論を skip", "無効"},
		[]Cell{"エディター推論型の詳細", "有効"},
		[]Cell{"Excel 言語", "自動"},
	)
	assertRowsContain(t, sheetRows(t, sheets, "インクルードツリー"),
		[]Cell{"子孫", 1, "main.asp", "util.inc", "includes/util.inc", "あり", "util.inc"},
	)
	assertRowsContain(t, sheetRows(t, sheets, "宣言"),
		[]Cell{"main.asp", "TargetValue", "変数", "", "グローバル", "", "String", "", "", "なし", "", 3, 5, 1, 1, 0, "使用あり"},
		[]Cell{"main.asp", "TargetProc", "関数", "", "グローバル", "", "", "Long", "", "なし", "", 4, 5, 0, 0, 1, "使用あり"},
		[]Cell{"main.asp", "UnusedValue", "変数", "", "グローバル", "", "Variant", "", "", "なし", "", 5, 5, 0, 0, 0, "未使用"},
	)
	assertRowsContain(t, sheetRows(t, sheets, "ファイル内使用"),
		[]Cell{"参照", "読み取り", "main.asp", "main.asp", "main.asp", "TargetValue", "変数", "String", 3, 5, 1},
		[]Cell{"代入", "書き込み", "main.asp", "main.asp", "main.asp", "TargetValue", "変数", "String", 3, 5, 1},
	)
	assertRowsContain(t, sheetRows(t, sheets, "外部ファイルからの使用"),
		[]Cell{"呼び出し", "呼び出し", "consumer.asp", "consumer.asp", "main.asp", "TargetProc", "関数", "Long", 4, 5, 1},
		[]Cell{"代入", "書き込み", "parent.asp", "parent.asp", "main.asp", "MissingValue", "変数", "Variant", 10, 5, 1},
	)
	assertRowsContain(t, sheetRows(t, sheets, "include 先シンボル使用"),
		[]Cell{"参照", "読み取り", "util.inc", "SharedValue", "変数", "String", "main.asp", 2, 5, 1},
	)
	assertRowsContain(t, sheetRows(t, sheets, "メンバー使用"),
		[]Cell{"メンバー", "メンバー", "Customer", "UnknownMember", "Customer.UnknownMember", "main.asp", 18, 5, 1},
	)
	assertRowsContain(t, sheetRows(t, sheets, "暗黙global変数"),
		[]Cell{"main.asp", "MissingValue", "変数", "Variant", "グローバル", 10, 5, 1, 0, 1, 0},
	)
	assertRowsContain(t, sheetRows(t, sheets, "暗黙global変数代入候補"),
		[]Cell{"main.asp", "MissingValue", "parent.asp", "MissingValue", "main.asp", 1, 10, 5, 1},
	)
	assertRowsContain(t, sheetRows(t, sheets, "未使用"),
		[]Cell{"main.asp", "UnusedValue", "変数", "", "グローバル", "Variant", "なし", 5, 5, 0, 0, 0, "未使用"},
	)
	assertRowsContain(t, sheetRows(t, sheets, "未解決"),
		[]Cell{"未解決参照", "読み取り", "未解決Function/Sub", "main.asp", "MissingProc", "main.asp", 9, 5, 1},
	)
	if chart := sheetByName(t, sheets, "チャート元データ"); !chart.Hidden {
		t.Fatal("chart data sheet is visible, want hidden")
	}
	if declarations := sheetByName(t, sheets, "宣言"); declarations.AutoFilterRef != "A1:Q5" {
		t.Fatalf("declaration autoFilterRef = %q, want A1:Q5", declarations.AutoFilterRef)
	}
	summary := sheetByName(t, sheets, "概要")
	if summary.StickyRowsCount != 1 {
		t.Fatalf("summary sticky rows = %d, want 1", summary.StickyRowsCount)
	}
	if header, ok := summary.Data[0][0].(StyledCell); !ok || header.Style != cellPresentationHeader {
		t.Fatalf("summary header = %#v, want styled header", summary.Data[0][0])
	}
	if side, ok := summary.Data[1][3].(StyledCell); !ok || side.Style != cellPresentationSideLabel || side.Value != "表の説明" {
		t.Fatalf("summary side description label = %#v", summary.Data[1][3])
	}
	analysis := sheetByName(t, sheets, "分析サマリ")
	if analysis.StickyRowsCount != 0 {
		t.Fatalf("analysis summary sticky rows = %d, want 0", analysis.StickyRowsCount)
	}
	if title, ok := analysis.Data[0][0].(StyledCell); !ok || title.Style != cellPresentationSection || title.ColumnSpan != 4 {
		t.Fatalf("analysis section title = %#v, want four-column section", analysis.Data[0][0])
	}
	chart := sheetByName(t, sheets, "チャート元データ")
	if chart.StickyRowsCount != 2 {
		t.Fatalf("chart data sticky rows = %d, want 2", chart.StickyRowsCount)
	}
}

func TestCreateAnalysisSheetsUsesLocalizedNamesAndNoTruncationText(t *testing.T) {
	payload := testSheetPayload()
	jaSheets := CreateAnalysisSheets(payload, LocaleJapanese, AnalysisSheetsOptions{TargetURI: "file:///workspace/main.asp"})
	enSheets := CreateAnalysisSheets(payload, LocaleEnglish, AnalysisSheetsOptions{TargetURI: "file:///workspace/main.asp"})
	if !contains(sheetNames(jaSheets), "ファイル内使用") || contains(sheetNames(jaSheets), "内部使用") {
		t.Fatalf("Japanese sheet names = %#v", sheetNames(jaSheets))
	}
	if !contains(sheetNames(enSheets), "File-local Usage") {
		t.Fatalf("English sheet names = %#v", sheetNames(enSheets))
	}
	if contains(sheetNames(enSheets), "Implicit Global Assignment Candidates") {
		t.Fatalf("English sheet names contain untruncated implicit global assignment sheet: %#v", sheetNames(enSheets))
	}
	hasTruncatedImplicitAssignmentSheet := false
	for _, name := range sheetNames(enSheets) {
		if len(name) > 31 {
			t.Fatalf("English sheet name %q length = %d, want <= 31", name, len(name))
		}
		if strings.HasPrefix(name, "Implicit Global Assignment") {
			hasTruncatedImplicitAssignmentSheet = true
		}
	}
	if !hasTruncatedImplicitAssignmentSheet {
		t.Fatalf("English sheet names missing truncated implicit global assignment sheet: %#v", sheetNames(enSheets))
	}
	assertRowsContain(t, sheetRows(t, enSheets, "Summary"),
		[]Cell{"Scope", "File"},
		[]Cell{"Root", "main.asp"},
	)

	enSheetsWithSettings := CreateAnalysisSheets(payload, LocaleEnglish, AnalysisSheetsOptions{
		TargetURI: "file:///workspace/main.asp",
		Settings: &AnalysisWorkbookSettings{
			ExcelLocale:                "auto",
			IncludeAnalysisTypeDetails: true,
		},
	})
	assertRowsContain(t, sheetRows(t, enSheetsWithSettings, "Summary"),
		[]Cell{"Excel language", "Auto"},
	)
}

func TestCreateAnalysisSheetsChecksUnusedDeclarationKinds(t *testing.T) {
	sheets := CreateAnalysisSheets(unusedDeclarationKindsPayload(), LocaleJapanese, AnalysisSheetsOptions{
		TargetURI: "file:///workspace/coverage.asp",
	})
	unusedRows := sheetRows(t, sheets, "未使用")
	assertRowsContain(t, unusedRows,
		[]Cell{"coverage.asp", "UnusedFunction", "関数", "", "グローバル", "", "なし", 2, 5, 0, 0, 0, "未使用"},
		[]Cell{"coverage.asp", "UnusedLocalValue", "変数", "", "ローカル", "Variant", "なし", 4, 5, 0, 0, 0, "未使用"},
		[]Cell{"coverage.asp", "UnusedParameter", "パラメーター", "", "ローカル", "Variant", "なし", 10, 5, 0, 0, 0, "未使用"},
		[]Cell{"coverage.asp", "UnusedConst", "定数", "", "グローバル", "Variant", "なし", 6, 5, 0, 0, 0, "未使用"},
		[]Cell{"coverage.asp", "UnusedLocalConst", "定数", "", "ローカル", "Variant", "なし", 8, 5, 0, 0, 0, "未使用"},
	)
	flat := flattenRows(unusedRows)
	for _, unexpected := range []string{"UsedFunction", "UsedLocalValue", "UsedConst", "UsedLocalConst", "UsedParameter"} {
		if strings.Contains(flat, unexpected) {
			t.Fatalf("unused sheet unexpectedly contains %q: %s", unexpected, flat)
		}
	}
	if contains(sheetNames(sheets), "被参照") {
		t.Fatalf("sheet names unexpectedly contain legacy referenced sheet: %#v", sheetNames(sheets))
	}
}

func TestCreateAnalysisSheetsReportsProgress(t *testing.T) {
	events := []AnalysisProgressEvent{}
	sheets := CreateAnalysisSheets(testSheetPayload(), LocaleJapanese, AnalysisSheetsOptions{
		TargetURI: "file:///workspace/main.asp",
		Progress: func(event AnalysisProgressEvent) {
			events = append(events, event)
		},
	})
	if !contains(sheetNames(sheets), "概要") {
		t.Fatalf("sheet names missing summary: %#v", sheetNames(sheets))
	}
	labels := make([]string, 0, len(events))
	for _, event := range events {
		labels = append(labels, event.Label)
	}
	for _, label := range []string{"excel.normalizeGraph", "excel.analysisContext", "excel.analysisSummary", "excel.sheet", "excel.sheets"} {
		if !contains(labels, label) {
			t.Fatalf("progress labels missing %q in %#v", label, labels)
		}
	}
	if len(events) < 3 || events[0].Label != "excel.normalizeGraph" || events[0].Current != 0 || events[0].Total != 1 ||
		events[1].Label != "excel.normalizeGraph" || events[1].Current != 1 || events[1].Total != 1 ||
		events[2].Label != "excel.analysisContext" || events[2].Current != 1 || events[2].Total != 1 {
		t.Fatalf("analysis context progress = %#v, want normalization 0/1 then 1/1 and context 1/1", events[:min(len(events), 3)])
	}
	foundSummarySheet := false
	sheetCurrent := 0
	for _, event := range events {
		if event.Label == "excel.sheet" && event.Detail == "概要" {
			foundSummarySheet = true
			if event.Total != 14 || len(event.ActiveItems) != 1 || event.ActiveItems[0] != "概要" {
				t.Fatalf("summary sheet progress = %#v, want sheet detail and active item", event)
			}
		}
		if event.Label == "excel.sheet" {
			sheetCurrent++
			if event.Current != sheetCurrent || event.Total != 14 || event.Detail == "" || len(event.ActiveItems) != 1 {
				t.Fatalf("sheet progress = %#v, want completed sheet %d/14 with active detail", event, sheetCurrent)
			}
		}
	}
	if !foundSummarySheet {
		t.Fatalf("progress events missing summary sheet detail: %#v", events)
	}
	if sheetCurrent != 14 {
		t.Fatalf("sheet progress count = %d, want 14: %#v", sheetCurrent, events)
	}
	for _, event := range events {
		if event.Label == "excel.analysisSummary" && (event.Current != 1 || event.Total != 1 || event.Detail == "") {
			t.Fatalf("analysis summary progress = %#v, want completed 1/1 with detail", event)
		}
	}
	last := events[len(events)-1]
	if last.Label != "excel.sheets" || last.Current != last.Total || len(last.ActiveItems) != 0 {
		t.Fatalf("last progress event = %#v, want complete excel.sheets", last)
	}
}

func TestCreateAnalysisSheetsSkipsFlowchartExceptionNodes(t *testing.T) {
	payload := testSheetPayload()
	payload.Nodes = append(payload.Nodes, graph.Node{
		ID:     "flow:on-error",
		Kind:   "exceptionHandling",
		Label:  "On Error Resume Next",
		URI:    "file:///workspace/main.asp",
		Range:  testRange(12, 4),
		Origin: "source",
	})
	sheets := CreateAnalysisSheets(payload, LocaleJapanese, AnalysisSheetsOptions{TargetURI: "file:///workspace/main.asp"})
	flattened := []string{}
	for _, sheet := range sheets {
		for _, row := range sheet.Data {
			for _, cell := range row {
				flattened = append(flattened, stringifyCell(cell))
			}
		}
	}
	text := strings.Join(flattened, "\n")
	if strings.Contains(text, "On Error Resume Next") || strings.Contains(text, "exceptionHandling") {
		t.Fatalf("analysis sheets contain flowchart exception node: %s", text)
	}
}

func TestCreateAnalysisSheetsSummarizesWorkspaceFileList(t *testing.T) {
	payload := testSheetPayload()
	payload.Scope = "workspace"
	payload.RootURI = ""
	sheets := CreateAnalysisSheets(payload, LocaleJapanese, AnalysisSheetsOptions{
		AnalysisFileCount: 7,
		IncludeGlobs:      []string{"**/*.asp", "**/*.inc"},
		ExcludeGlobs:      []string{"legacy/**"},
	})
	assertRowsContain(t, sheetRows(t, sheets, "概要"),
		[]Cell{"解析範囲", "ワークスペース"},
		[]Cell{"ルート", "ワークスペース"},
		[]Cell{"解析 file 数", 7},
		[]Cell{"一時 include glob", "**/*.asp\n**/*.inc"},
		[]Cell{"一時 exclude glob", "legacy/**"},
	)
	assertRowsContain(t, sheetRows(t, sheets, "宣言"),
		[]Cell{"util.inc", "SharedValue", "変数", "", "グローバル", "", "String", "", "", "なし", "", 2, 5, 1, 0, 0, "使用あり"},
		[]Cell{"main.asp", "TargetValue", "変数", "", "グローバル", "", "String", "", "", "なし", "", 3, 5, 1, 1, 0, "使用あり"},
	)
}

func TestCreateAnalysisSheetsIncludeTreeReportsDepthAndRelatedDirections(t *testing.T) {
	mainURI := "file:///workspace/main.asp"
	childURI := "file:///workspace/child.inc"
	grandchildURI := "file:///workspace/grandchild.inc"
	parentURI := "file:///workspace/parent.asp"
	siblingURI := "file:///workspace/sibling.inc"
	payload := graph.Payload{Scope: "document", URI: mainURI, RootURI: mainURI}
	for _, file := range []struct {
		uri  string
		name string
	}{
		{mainURI, "main.asp"},
		{childURI, "child.inc"},
		{grandchildURI, "grandchild.inc"},
		{parentURI, "parent.asp"},
		{siblingURI, "sibling.inc"},
	} {
		payload.Nodes = append(payload.Nodes, graph.Node{ID: file.uri, URI: file.uri, FileName: file.name, Label: file.name, Kind: "file"})
	}
	addInclude := func(id, source, target, mode string, line, column int) {
		payload.AddEdge(graph.Edge{
			ID: id, Source: source, Target: target, Kind: "include", Label: target,
			Ranges:  []lsp.Location{{URI: source, Range: *testRange(line, column)}},
			Include: &graph.IncludeInfo{Path: target, Mode: mode, Exists: true, ResolvedURI: target, PathCaseMatches: true},
		})
	}
	addInclude("main-child", mainURI, childURI, "file", 1, 2)
	addInclude("child-grandchild", childURI, grandchildURI, "virtual", 2, 3)
	addInclude("parent-main", parentURI, mainURI, "file", 3, 4)
	addInclude("parent-sibling", parentURI, siblingURI, "file", 4, 5)

	sheets := CreateAnalysisSheets(payload, LocaleJapanese, AnalysisSheetsOptions{TargetURI: mainURI})
	rows := sheetRows(t, sheets, "インクルードツリー")
	assertRowsContain(t, rows,
		[]Cell{"祖先", 1, "parent.asp", "main.asp", mainURI, "file", "あり", "main.asp", 4, 5},
		[]Cell{"子孫", 1, "main.asp", "child.inc", childURI, "file", "あり", "child.inc", 2, 3},
		[]Cell{"子孫", 2, "child.inc", "grandchild.inc", grandchildURI, "virtual", "あり", "grandchild.inc", 3, 4},
		[]Cell{"親戚", 2, "parent.asp", "sibling.inc", siblingURI, "file", "あり", "sibling.inc", 5, 6},
	)
	diagram := sheetByName(t, sheets, "インクルードツリー図")
	nodes := map[string]struct{}{}
	connectors := 0
	for _, shape := range diagram.Shapes {
		switch shape.Type {
		case "roundRect":
			if _, duplicate := nodes[shape.Text]; duplicate {
				t.Fatalf("duplicate diagram node %q: %#v", shape.Text, diagram.Shapes)
			}
			nodes[shape.Text] = struct{}{}
		case "straightConnector1":
			connectors++
		}
	}
	if len(nodes) != 5 || connectors != 4 {
		t.Fatalf("diagram nodes/connectors = %d/%d, want 5/4: %#v", len(nodes), connectors, diagram.Shapes)
	}
}

func TestCreateAnalysisSheetsDeduplicatesIdenticalIncludedUsages(t *testing.T) {
	payload := testSheetPayload()
	duplicate := graph.Edge{
		ID: "ref-shared-duplicate", Source: "file:/workspace/main.asp", Target: "vb:shared",
		Kind: "references", Role: "read", Count: 1,
	}
	payload.Links = append(payload.Links, duplicate)

	rows := sheetRows(t, CreateAnalysisSheets(payload, LocaleJapanese, AnalysisSheetsOptions{
		TargetURI: "file:///workspace/main.asp",
	}), "include 先シンボル使用")
	matching := 0
	for _, row := range rows {
		if len(row) >= 4 && row[2] == "util.inc" && row[3] == "SharedValue" {
			matching++
		}
	}
	if matching != 1 {
		t.Fatalf("identical included usage rows = %d, want 1; rows=%#v", matching, rows)
	}
	context := newAnalysisContext(payload, AnalysisSheetsOptions{TargetURI: "file:///workspace/main.asp"})
	if got := context.usageCounts["vb:shared"].references; got != 1 {
		t.Fatalf("deduplicated reference count = %d, want 1", got)
	}
}

func TestCreateAnalysisSheetsDeduplicatesDisplayedExternalUsagesWithDifferentGraphIDs(t *testing.T) {
	payload := testSheetPayload()
	consumerURI := "file:///workspace/consumer.asp"
	mainURI := "file:///workspace/main.asp"
	payload.Nodes = append(payload.Nodes,
		graph.Node{ID: "file:/workspace/consumer-alias.asp", Kind: "file", Label: "consumer.asp", URI: consumerURI, FileName: "consumer.asp"},
		graph.Node{ID: "vb:target-proc-alias", Kind: "vbDeclaration", Label: "TargetProc", URI: mainURI, Range: testRange(3, 4), DeclarationKind: "function", BindingScope: "global", TypeName: "Long", Origin: "source"},
	)
	payload.AddEdge(graph.Edge{
		ID: "call-target-proc-alias", Source: "file:/workspace/consumer-alias.asp", Target: "vb:target-proc-alias",
		Kind: "calls", Role: "function", Count: 1,
	})

	rows := sheetRows(t, CreateAnalysisSheets(payload, LocaleJapanese, AnalysisSheetsOptions{
		TargetURI: mainURI,
	}), "外部ファイルからの使用")
	matching := 0
	for _, row := range rows {
		if len(row) >= 6 && row[2] == "consumer.asp" && row[5] == "TargetProc" {
			matching++
		}
	}
	if matching != 1 {
		t.Fatalf("display-identical external usage rows = %d, want 1; rows=%#v", matching, rows)
	}
	context := newAnalysisContext(payload, AnalysisSheetsOptions{TargetURI: mainURI})
	if got := context.usageCounts["vb:target-proc"].calls; got != 1 {
		t.Fatalf("deduplicated external call count = %d, want 1", got)
	}
}

func TestAnalysisContextKeepsSameBasenameUsagesFromDistinctURIs(t *testing.T) {
	const targetURI = "file:///workspace/target.inc"
	payload := graph.Payload{Scope: "workspace", RootURI: "file:///workspace"}
	payload.Nodes = []graph.Node{
		{ID: "file:a", URI: "file:///workspace/a/default.asp", FileName: "default.asp", Kind: "file"},
		{ID: "file:b", URI: "file:///workspace/b/default.asp", FileName: "default.asp", Kind: "file"},
		{ID: "file:target", URI: targetURI, FileName: "target.inc", Kind: "file"},
		{ID: "source:a", URI: "file:///workspace/a/default.asp", Label: "UseValue", Kind: "vbReference"},
		{ID: "source:b", URI: "file:///workspace/b/default.asp", Label: "UseValue", Kind: "vbReference"},
		{ID: "target", URI: targetURI, Label: "SharedValue", Kind: "vbDeclaration", DeclarationKind: "variable"},
	}
	for _, source := range []string{"source:a", "source:b"} {
		payload.AddEdge(graph.Edge{Source: source, Target: "target", Kind: "references", Role: "read", Count: 1})
	}

	context := newAnalysisContext(payload, AnalysisSheetsOptions{})
	if got := len(context.internalUsages); got != 2 {
		t.Fatalf("same-basename usage rows = %d, want 2: %#v", got, context.internalUsages)
	}
	if got := context.usageCounts["target"].references; got != 2 {
		t.Fatalf("same-basename reference count = %d, want 2", got)
	}
}

func TestHeaderDescriptionsMatchLegacyTypeScriptWorkbook(t *testing.T) {
	if got := headerDescription(LocaleJapanese, text(LocaleJapanese, "sourceFile")); got != "include directive が書かれているファイルです。" {
		t.Fatalf("Japanese source file description = %q", got)
	}
	if got := headerDescription(LocaleEnglish, text(LocaleEnglish, "sourceFile")); got != "File that contains the include directive." {
		t.Fatalf("English source file description = %q", got)
	}
}

func TestAnalysisSummaryKeepsLegacyChartSectionAndDynamicAnchors(t *testing.T) {
	context := newAnalysisContext(testSheetPayload(), AnalysisSheetsOptions{TargetURI: "file:///workspace/main.asp"})
	summaryRows := analysisSummaryRows(context, LocaleJapanese)
	sheet := analysisSummarySheet(context, LocaleJapanese)
	if got, want := len(sheet.Data), len(summaryRows)+36; got != want {
		t.Fatalf("analysis summary rows = %d, want %d", got, want)
	}
	chartTitle, ok := sheet.Data[len(summaryRows)+1][0].(StyledCell)
	if !ok || chartTitle.Value != "グラフ" || chartTitle.Style != cellPresentationSection {
		t.Fatalf("chart section title = %#v", sheet.Data[len(summaryRows)+1])
	}
	wantFirst := fmt.Sprintf("J%d", len(summaryRows)+3)
	wantSecond := fmt.Sprintf("J%d", len(summaryRows)+20)
	if len(sheet.Images) != 2 || sheet.Images[0].Cell != wantFirst || sheet.Images[1].Cell != wantSecond {
		t.Fatalf("chart anchors = %#v, want %s and %s", sheet.Images, wantFirst, wantSecond)
	}
}

func TestCreateAnalysisSheetsIncludeTreeDiagramStopsAtCyclesDeterministically(t *testing.T) {
	const aURI = "file:///workspace/a.asp"
	const bURI = "file:///workspace/b.inc"
	payload := graph.Payload{
		Scope: "document", URI: aURI, RootURI: aURI,
		Nodes: []graph.Node{
			{ID: "file:a", URI: aURI, FileName: "a.asp", Label: "a.asp", Kind: "file"},
			{ID: "file:b", URI: bURI, FileName: "b.inc", Label: "b.inc", Kind: "file"},
		},
	}
	payload.AddEdge(graph.Edge{ID: "a-b", Source: "file:a", Target: "file:b", Kind: "include"})
	payload.AddEdge(graph.Edge{ID: "b-a", Source: "file:b", Target: "file:a", Kind: "include"})
	build := func() AnalysisSheet {
		return sheetByName(t, CreateAnalysisSheets(payload, LocaleJapanese, AnalysisSheetsOptions{TargetURI: aURI}), "インクルードツリー図")
	}
	first, second := build(), build()
	if !reflect.DeepEqual(first.Shapes, second.Shapes) {
		t.Fatalf("cyclic diagram is not deterministic:\nfirst=%#v\nsecond=%#v", first.Shapes, second.Shapes)
	}
	nodes, connectors := 0, 0
	for _, shape := range first.Shapes {
		if shape.Type == "roundRect" {
			nodes++
		} else if shape.Type == "straightConnector1" {
			connectors++
		}
	}
	if nodes != 2 || connectors != 2 {
		t.Fatalf("cyclic diagram nodes/connectors = %d/%d, want 2/2: %#v", nodes, connectors, first.Shapes)
	}
}

func TestCreateAnalysisSheetsKeepsDistinctIncludedUsageLocations(t *testing.T) {
	payload := testSheetPayload()
	payload.Links = append(payload.Links, graph.Edge{
		ID: "ref-shared-distinct", Source: "file:/workspace/main.asp", Target: "vb:shared",
		Kind: "references", Role: "read", Count: 1,
		Ranges: []lsp.Location{{URI: "file:///workspace/main.asp", Range: *testRange(20, 2)}},
	})

	rows := sheetRows(t, CreateAnalysisSheets(payload, LocaleJapanese, AnalysisSheetsOptions{
		TargetURI: "file:///workspace/main.asp",
	}), "include 先シンボル使用")
	matching := 0
	for _, row := range rows {
		if len(row) >= 4 && row[2] == "util.inc" && row[3] == "SharedValue" {
			matching++
		}
	}
	if matching != 2 {
		t.Fatalf("distinct included usage rows = %d, want 2; rows=%#v", matching, rows)
	}
}

func testSheetPayload() graph.Payload {
	mainURI := "file:///workspace/main.asp"
	utilURI := "file:///workspace/includes/util.inc"
	consumerURI := "file:///workspace/consumer.asp"
	parentURI := "file:///workspace/parent.asp"
	payload := graph.Payload{
		Scope:   "document",
		RootURI: mainURI,
		URI:     mainURI,
		Nodes: []graph.Node{
			{ID: "file:/workspace/main.asp", Kind: "file", Label: "main.asp", URI: mainURI, FileName: "main.asp", IsRoot: true},
			{ID: "file:/workspace/includes/util.inc", Kind: "file", Label: "util.inc", URI: utilURI, FileName: "util.inc"},
			{ID: "file:/workspace/consumer.asp", Kind: "file", Label: "consumer.asp", URI: consumerURI, FileName: "consumer.asp"},
			{ID: "file:/workspace/parent.asp", Kind: "file", Label: "parent.asp", URI: parentURI, FileName: "parent.asp"},
			{ID: "vb:shared", Kind: "vbDeclaration", Label: "SharedValue", URI: utilURI, Range: testRange(1, 4), DeclarationKind: "variable", BindingScope: "global", TypeName: "String", Origin: "source"},
			{ID: "vb:target-value", Kind: "vbDeclaration", Label: "TargetValue", URI: mainURI, Range: testRange(2, 4), DeclarationKind: "variable", BindingScope: "global", TypeName: "String", Origin: "source"},
			{ID: "vb:target-proc", Kind: "vbDeclaration", Label: "TargetProc", URI: mainURI, Range: testRange(3, 4), DeclarationKind: "function", BindingScope: "global", TypeName: "Long", Origin: "source"},
			{ID: "vb:unused", Kind: "vbDeclaration", Label: "UnusedValue", URI: mainURI, Range: testRange(4, 4), DeclarationKind: "variable", BindingScope: "global", Origin: "source"},
			{ID: "vb:missing-value", Kind: "vbDeclaration", Label: "MissingValue", URI: mainURI, Range: testRange(9, 4), DeclarationKind: "variable", BindingScope: "global", Implicit: true, Origin: "source"},
			{ID: "unresolved:missingproc", Kind: "vbUnresolved", Label: "MissingProc", URI: mainURI, Range: testRange(8, 4), Group: "unresolvedFunction"},
			{ID: "member:customer.unknownmember", Kind: "vbMemberReference", Label: "UnknownMember", URI: mainURI, Range: testRange(17, 4), Role: "member", ReceiverName: "Customer", MemberName: "UnknownMember", FullPath: "Customer.UnknownMember"},
		},
	}
	payload.AddEdge(graph.Edge{ID: "include-util", Source: "file:/workspace/main.asp", Target: "file:/workspace/includes/util.inc", Kind: "include", Label: "includes/util.inc", Count: 1, Include: &graph.IncludeInfo{Path: "includes/util.inc", Exists: true, ResolvedURI: utilURI}})
	payload.AddEdge(graph.Edge{ID: "include-parent-main", Source: "file:/workspace/parent.asp", Target: "file:/workspace/main.asp", Kind: "include", Label: "main.asp", Count: 1, Include: &graph.IncludeInfo{Path: "main.asp", Exists: true, ResolvedURI: mainURI}})
	payload.AddEdge(graph.Edge{ID: "ref-shared", Source: "file:/workspace/main.asp", Target: "vb:shared", Kind: "references", Role: "read", Count: 1})
	payload.AddEdge(graph.Edge{ID: "ref-target-value", Source: "file:/workspace/main.asp", Target: "vb:target-value", Kind: "references", Role: "read", Count: 1})
	payload.AddEdge(graph.Edge{ID: "assign-target-value", Source: "file:/workspace/main.asp", Target: "vb:target-value", Kind: "assignments", Role: "write", Count: 1})
	payload.AddEdge(graph.Edge{ID: "call-target-proc", Source: "file:/workspace/consumer.asp", Target: "vb:target-proc", Kind: "calls", Role: "function", Count: 1})
	payload.AddEdge(graph.Edge{ID: "assign-missing-value", Source: "file:/workspace/parent.asp", Target: "vb:missing-value", Kind: "assignments", Role: "write", Count: 1})
	payload.AddEdge(graph.Edge{ID: "unresolved-missing-proc", Source: "file:/workspace/main.asp", Target: "unresolved:missingproc", Kind: "unresolvedReference", Role: "read", Count: 1, Ranges: []lsp.Location{{URI: mainURI, Range: *testRange(8, 4)}}})
	return payload
}

func unusedDeclarationKindsPayload() graph.Payload {
	uri := "file:///workspace/coverage.asp"
	payload := graph.Payload{
		Scope:   "document",
		RootURI: uri,
		Nodes: []graph.Node{
			{ID: "file:/workspace/coverage.asp", Kind: "file", Label: "coverage.asp", URI: uri, FileName: "coverage.asp", IsRoot: true},
			{ID: "vb:used-function", Kind: "vbDeclaration", Label: "UsedFunction", URI: uri, Range: testRange(0, 4), DeclarationKind: "function", BindingScope: "global", Origin: "source"},
			{ID: "vb:unused-function", Kind: "vbDeclaration", Label: "UnusedFunction", URI: uri, Range: testRange(1, 4), DeclarationKind: "function", BindingScope: "global", Origin: "source"},
			{ID: "vb:used-local", Kind: "vbDeclaration", Label: "UsedLocalValue", URI: uri, Range: testRange(2, 4), DeclarationKind: "variable", BindingScope: "local", Origin: "source"},
			{ID: "vb:unused-local", Kind: "vbDeclaration", Label: "UnusedLocalValue", URI: uri, Range: testRange(3, 4), DeclarationKind: "variable", BindingScope: "local", Origin: "source"},
			{ID: "vb:used-const", Kind: "vbDeclaration", Label: "UsedConst", URI: uri, Range: testRange(4, 4), DeclarationKind: "constant", BindingScope: "global", Origin: "source"},
			{ID: "vb:unused-const", Kind: "vbDeclaration", Label: "UnusedConst", URI: uri, Range: testRange(5, 4), DeclarationKind: "constant", BindingScope: "global", Origin: "source"},
			{ID: "vb:used-local-const", Kind: "vbDeclaration", Label: "UsedLocalConst", URI: uri, Range: testRange(6, 4), DeclarationKind: "constant", BindingScope: "local", Origin: "source"},
			{ID: "vb:unused-local-const", Kind: "vbDeclaration", Label: "UnusedLocalConst", URI: uri, Range: testRange(7, 4), DeclarationKind: "constant", BindingScope: "local", Origin: "source"},
			{ID: "vb:used-parameter", Kind: "vbDeclaration", Label: "UsedParameter", URI: uri, Range: testRange(8, 4), DeclarationKind: "parameter", BindingScope: "local", Origin: "source"},
			{ID: "vb:unused-parameter", Kind: "vbDeclaration", Label: "UnusedParameter", URI: uri, Range: testRange(9, 4), DeclarationKind: "parameter", BindingScope: "local", Origin: "source"},
		},
	}
	payload.AddEdge(graph.Edge{ID: "call-used-function", Source: "file:/workspace/coverage.asp", Target: "vb:used-function", Kind: "calls", Role: "function", Count: 1})
	payload.AddEdge(graph.Edge{ID: "assign-used-local", Source: "file:/workspace/coverage.asp", Target: "vb:used-local", Kind: "assignments", Role: "write", Count: 1})
	payload.AddEdge(graph.Edge{ID: "ref-used-const", Source: "file:/workspace/coverage.asp", Target: "vb:used-const", Kind: "references", Role: "read", Count: 1})
	payload.AddEdge(graph.Edge{ID: "ref-used-local-const", Source: "file:/workspace/coverage.asp", Target: "vb:used-local-const", Kind: "references", Role: "read", Count: 1})
	payload.AddEdge(graph.Edge{ID: "ref-used-parameter", Source: "file:/workspace/coverage.asp", Target: "vb:used-parameter", Kind: "references", Role: "read", Count: 1})
	return payload
}

func testRange(line int, character int) *lsp.Range {
	return &lsp.Range{
		Start: lsp.Position{Line: line, Character: character},
		End:   lsp.Position{Line: line, Character: character + 1},
	}
}

func sheetNames(sheets []AnalysisSheet) []string {
	names := make([]string, 0, len(sheets))
	for _, sheet := range sheets {
		names = append(names, sheet.Sheet)
	}
	return names
}

func sheetByName(t *testing.T, sheets []AnalysisSheet, name string) AnalysisSheet {
	t.Helper()
	for _, sheet := range sheets {
		if sheet.Sheet == name {
			return sheet
		}
	}
	t.Fatalf("missing sheet %q in %#v", name, sheetNames(sheets))
	return AnalysisSheet{}
}

func sheetRows(t *testing.T, sheets []AnalysisSheet, name string) [][]Cell {
	t.Helper()
	return sheetByName(t, sheets, name).Data
}

func assertRowsContain(t *testing.T, rows [][]Cell, expectedRows ...[]Cell) {
	t.Helper()
	for _, expected := range expectedRows {
		found := false
		for _, row := range rows {
			if len(row) < len(expected) {
				continue
			}
			matches := true
			for index := range expected {
				if !reflect.DeepEqual(presentedCellValue(row[index]), expected[index]) {
					matches = false
					break
				}
			}
			if matches {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("rows missing %#v in %#v", expected, rows)
		}
	}
}

func flattenRows(rows [][]Cell) string {
	values := []string{}
	for _, row := range rows {
		for _, cell := range row {
			values = append(values, stringifyCell(cell))
		}
	}
	return strings.Join(values, "\n")
}

func contains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func stringifyCell(cell Cell) string {
	if cell == nil {
		return ""
	}
	return strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(fmt.Sprint(presentedCellValue(cell)), "\n", " "), "\t", " "))
}
