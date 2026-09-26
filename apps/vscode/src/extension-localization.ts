import * as vscode from "vscode";
import type { AspFlowchartLocale } from "./flowchart-webview";

export type ExtensionMessageKey =
  | "logAnalysis.title"
  | "server.binaryNotFound"
  | "server.binaryInvalid"
  | "server.startFailed"
  | "server.openOutput"
  | "maintenance.serverUnavailable"
  | "maintenance.commandFailed"
  | "maintenance.reindexRequested"
  | "maintenance.clearCacheCompleted"
  | "maintenance.clearDiskCacheCompleted"
  | "maintenance.clearProcessCacheCompleted"
  | "maintenance.noOp"
  | "status.tooltip"
  | "status.loading.text"
  | "status.loading.tooltip"
  | "status.analyzing.text"
  | "status.analyzing.tooltip"
  | "status.progress.active"
  | "status.progress.cancel"
  | "status.progress.cancelling"
  | "status.progress.analyzingStatusText"
  | "status.progress.diagnostics"
  | "status.progress.diagnosticsInclude"
  | "status.progress.diagnosticsProjectFast"
  | "status.progress.diagnosticsProject"
  | "status.progress.diagnosticsSyntax"
  | "status.progress.documentAnalysis"
  | "status.progress.documentAnalysisCache"
  | "status.progress.documentAnalysisIncremental"
  | "status.progress.documentAnalysisParse"
  | "status.progress.documentAnalysisReady"
  | "status.progress.excel"
  | "status.progress.excelChooseFile"
  | "status.progress.excelFile"
  | "status.progress.excelFileCommit"
  | "status.progress.excelFileRows"
  | "status.progress.excelFileSheet"
  | "status.progress.excelGraph"
  | "status.progress.excelAnalysisContext"
  | "status.progress.excelAnalysisSummary"
  | "status.progress.excelNormalizeGraph"
  | "status.progress.excelSheet"
  | "status.progress.excelSheets"
  | "status.progress.excelWorkbook"
  | "status.progress.flowchart"
  | "status.progress.flowchartBuildPayload"
  | "status.progress.flowchartCanonicalizeSymbols"
  | "status.progress.flowchartCollectIncludes"
  | "status.progress.flowchartHydrateDocument"
  | "status.progress.flowchartIndexDocuments"
  | "status.progress.flowchartLoadDocument"
  | "status.progress.navigationGraph"
  | "status.progress.navigationGraphBuildPayload"
  | "status.progress.navigationGraphCollectDocuments"
  | "status.progress.navigationGraphExtract"
  | "status.progress.navigationGraphResolveIncludes"
  | "status.progress.graphAddStructure"
  | "status.progress.graphAddUsages"
  | "status.progress.graphCanonicalizeSymbols"
  | "status.progress.graphCheckRelatedIncludes"
  | "status.progress.graphCollectIncomingIncludes"
  | "status.progress.graphCollectIncludes"
  | "status.progress.graphCollectRelatedIncludes"
  | "status.progress.graphDocument"
  | "status.progress.graphFilterIncomingIncludes"
  | "status.progress.graphFindIncomingIncludes"
  | "status.progress.graphFinalize"
  | "status.progress.graphFolder"
  | "status.progress.graphIndexDocuments"
  | "status.progress.graphLoadDocuments"
  | "status.progress.graphOpenDocuments"
  | "status.progress.graphPrepareDocuments"
  | "status.progress.graphPrefetchIncludes"
  | "status.progress.graphResolveIncludes"
  | "status.progress.graphReverseIncludeIndex"
  | "status.progress.graphSpillIndexes"
  | "status.progress.graphWorkspaceIndex"
  | "status.progress.graphWorkspace"
  | "status.progress.loadingStatusText"
  | "status.progress.none"
  | "status.progress.placeholder"
  | "status.progress.title"
  | "status.progress.workspaceDiagnostics"
  | "status.progress.workspaceDiagnosticsIndexed"
  | "status.progress.workspaceDiagnosticsOpenDocuments"
  | "status.progress.workspaceIndex"
  | "status.progress.workspaceIndexFailed"
  | "status.progress.workspaceIndexWaitDocuments"
  | "status.progress.workspaceIndexParseFiles"
  | "status.progress.workspaceIndexReadCache"
  | "status.progress.workspaceIndexCatalog"
  | "status.progress.workspaceIndexScanFiles"
  | "status.progress.workspaceIndexScanRoot"
  | "status.progress.workspaceIndexWriteCache"
  | "status.progress.workspaceIndexIncludeGraph"
  | "status.progress.workspaceIndexFinalize"
  | "status.progress.workspacePreviewFiles"
  | "navigationGraph.serverUnavailable"
  | "navigationGraph.noActiveFile"
  | "navigationGraph.noFolder"
  | "navigationGraph.currentTitle"
  | "navigationGraph.folderTitle"
  | "navigationGraph.workspaceTitle"
  | "navigationGraph.documentPanelTitle"
  | "navigationGraph.workspacePanelTitle"
  | "navigationGraph.incomplete"
  | "navigationGraph.openFailed"
  | "navigationGraph.copyFailed"
  | "navigationGraph.updateFailed"
  | "excel.serverUnavailable"
  | "excel.noActiveFile"
  | "excel.currentTitle"
  | "excel.saveLabel"
  | "excel.writeTitle"
  | "excel.exported"
  | "workspaceFiles.serverUnavailable"
  | "workspaceFiles.previewFailed"
  | "workspaceFiles.settingsFailed"
  | "workspaceFiles.exportFailed"
  | "workspaceFiles.openFailed"
  | "workspaceFiles.settingsSaved"
  | "workspaceFiles.viewTitle"
  | "workspaceFiles.viewPanelTitle"
  | "workspaceFiles.workspaceUnavailable"
  | "flowchart.serverUnavailable"
  | "flowchart.noActiveFile"
  | "flowchart.incomplete"
  | "flowchart.openFailed"
  | "flowchart.copyFailed"
  | "flowchart.saveLabel"
  | "flowchart.exported"
  | "flowchart.exportFailed"
  | "flowchart.exportEmpty"
  | "flowchart.copied"
  | "flowchart.currentTitle"
  | "flowchart.documentPanelTitle"
  | "comment.serverUnavailable"
  | "comment.directiveRequiresWholeDocument"
  | "comment.unsafeStructure";

export type ExtensionMessageArgs = Record<string, string>;

const extensionMessages: Record<"en" | "ja", Record<ExtensionMessageKey, string>> = {
  en: {
    "server.binaryNotFound":
      "The Classic ASP Language Server binary was not found at {path}. Build it with `{command}`, or install the packaged VSIX.",
    "server.binaryInvalid":
      "The Classic ASP Language Server path is not an executable file: {path}. Build it with `{command}`, or install the packaged VSIX.",
    "server.startFailed":
      "The Classic ASP Language Server could not start from {path}: {error}. Check the Classic ASP output channel for details.",
    "server.openOutput": "Open Output",
    "maintenance.serverUnavailable":
      "Start the Classic ASP Language Server before running this maintenance command.",
    "maintenance.commandFailed": "Classic ASP maintenance command failed: {error}",
    "maintenance.reindexRequested": "Classic ASP workspace reindex requested.",
    "maintenance.clearCacheCompleted": "Classic ASP process and disk caches were cleared.",
    "maintenance.clearDiskCacheCompleted": "Classic ASP disk cache was cleared.",
    "maintenance.clearProcessCacheCompleted": "Classic ASP process cache was cleared.",
    "maintenance.noOp": "Classic ASP maintenance command completed without making changes.",
    "status.tooltip": "Classic ASP Language Server",
    "status.loading.text": "ASP Loading",
    "status.loading.tooltip": "Classic ASP Language Server is loading workspace data.",
    "status.analyzing.text": "ASP Analyzing",
    "status.analyzing.tooltip": "Classic ASP Language Server is analyzing ASP files.",
    "status.progress.active": "Active",
    "status.progress.cancel": "Cancel",
    "status.progress.cancelling": "Cancelling",
    "status.progress.analyzingStatusText": "ASP {task}",
    "status.progress.diagnostics": "Document diagnostics",
    "status.progress.diagnosticsInclude": "Checking include diagnostics",
    "status.progress.diagnosticsProjectFast": "Checking fast project diagnostics",
    "status.progress.diagnosticsProject": "Checking project diagnostics",
    "status.progress.diagnosticsSyntax": "Checking syntax diagnostics",
    "status.progress.documentAnalysis": "Document analysis",
    "status.progress.documentAnalysisCache": "Updating document analysis cache",
    "status.progress.documentAnalysisIncremental": "Applying incremental document analysis",
    "status.progress.documentAnalysisParse": "Parsing document",
    "status.progress.documentAnalysisReady": "Document analysis ready",
    "status.progress.excel": "Creating Excel workbook",
    "status.progress.excelChooseFile": "Choosing Excel output path",
    "status.progress.excelFile": "Writing Excel file",
    "status.progress.excelFileCommit": "Finalizing Excel file",
    "status.progress.excelFileRows": "Writing Excel rows",
    "status.progress.excelFileSheet": "Writing Excel sheet",
    "status.progress.excelGraph": "Collecting Excel analysis graph",
    "status.progress.excelAnalysisContext": "Classifying Excel graph data",
    "status.progress.excelAnalysisSummary": "Building Excel analysis summary",
    "status.progress.excelNormalizeGraph": "Normalizing Excel graph payload",
    "status.progress.excelSheet": "Building Excel sheet",
    "status.progress.excelSheets": "Building Excel sheets",
    "status.progress.excelWorkbook": "Generating Excel workbook",
    "status.progress.flowchart": "Generating flowchart",
    "status.progress.flowchartBuildPayload": "Building flowchart payload",
    "status.progress.flowchartCanonicalizeSymbols": "Canonicalizing flowchart symbols",
    "status.progress.flowchartCollectIncludes": "Collecting flowchart include tree",
    "status.progress.flowchartHydrateDocument": "Hydrating flowchart VBScript",
    "status.progress.flowchartIndexDocuments": "Indexing flowchart documents",
    "status.progress.flowchartLoadDocument": "Loading flowchart document",
    "status.progress.navigationGraph": "Generating navigation graph",
    "status.progress.navigationGraphBuildPayload": "Building navigation graph payload",
    "status.progress.navigationGraphCollectDocuments": "Collecting navigation graph documents",
    "status.progress.navigationGraphExtract": "Extracting navigation transitions",
    "status.progress.navigationGraphResolveIncludes": "Resolving navigation include owners",
    "status.progress.graphAddStructure": "Adding graph structure",
    "status.progress.graphAddUsages": "Adding graph usages",
    "status.progress.graphCanonicalizeSymbols": "Canonicalizing graph symbols",
    "status.progress.graphCheckRelatedIncludes": "Checking related include analysis need",
    "status.progress.graphCollectIncomingIncludes": "Collecting incoming include files",
    "status.progress.graphCollectIncludes": "Collecting include tree",
    "status.progress.graphCollectRelatedIncludes": "Collecting related include trees",
    "status.progress.graphDocument": "Generating current file graph",
    "status.progress.graphFilterIncomingIncludes": "Filtering incoming include files",
    "status.progress.graphFindIncomingIncludes": "Checking open incoming include files",
    "status.progress.graphFinalize": "Finalizing graph",
    "status.progress.graphFolder": "Generating folder graph",
    "status.progress.graphIndexDocuments": "Indexing graph documents",
    "status.progress.graphLoadDocuments": "Loading graph documents",
    "status.progress.graphOpenDocuments": "Loading open graph documents",
    "status.progress.graphPrepareDocuments": "Preparing graph documents",
    "status.progress.graphPrefetchIncludes": "Prefetching include targets",
    "status.progress.graphResolveIncludes": "Resolving include paths",
    "status.progress.graphReverseIncludeIndex": "Reading reverse include index",
    "status.progress.graphSpillIndexes": "Writing graph index spill files",
    "status.progress.graphWorkspace": "Generating workspace graph",
    "status.progress.graphWorkspaceIndex": "Loading graph workspace index",
    "status.progress.loadingStatusText": "ASP Loading: {task}",
    "status.progress.none": "No active Classic ASP tasks.",
    "status.progress.placeholder": "Current Classic ASP tasks",
    "status.progress.title": "Classic ASP Progress",
    "status.progress.workspaceDiagnostics": "Workspace diagnostics",
    "status.progress.workspaceDiagnosticsIndexed": "Checking indexed workspace diagnostics",
    "status.progress.workspaceDiagnosticsOpenDocuments": "Checking open document diagnostics",
    "status.progress.workspaceIndex": "Loading workspace index",
    "status.progress.workspaceIndexFailed": "Workspace index failed",
    "status.progress.workspaceIndexWaitDocuments": "Waiting for open-document analysis",
    "status.progress.workspaceIndexParseFiles": "Parsing workspace files",
    "status.progress.workspaceIndexReadCache": "Reading workspace cache",
    "status.progress.workspaceIndexCatalog": "Building auto-include catalog",
    "status.progress.workspaceIndexScanFiles": "Reading workspace files",
    "status.progress.workspaceIndexScanRoot": "Scanning workspace root",
    "status.progress.workspaceIndexWriteCache": "Writing workspace index to analysis database",
    "status.progress.workspaceIndexIncludeGraph": "Restoring workspace include graph",
    "status.progress.workspaceIndexFinalize": "Finalizing workspace index",
    "status.progress.workspacePreviewFiles": "Previewing workspace files",
    "navigationGraph.serverUnavailable":
      "Start the Classic ASP Language Server before building a navigation graph.",
    "navigationGraph.noActiveFile":
      "Open a Classic ASP file before building the current file navigation graph.",
    "navigationGraph.noFolder": "Select a folder before building the folder navigation graph.",
    "navigationGraph.currentTitle": "Classic ASP: Current File Navigation Graph",
    "navigationGraph.folderTitle": "Classic ASP: Folder Navigation Graph",
    "navigationGraph.workspaceTitle": "Classic ASP: Project Navigation Graph",
    "navigationGraph.documentPanelTitle": "Classic ASP Navigation Graph: {name}",
    "navigationGraph.workspacePanelTitle": "Classic ASP Navigation Graph: Project",
    "navigationGraph.incomplete": "The Classic ASP navigation graph response was incomplete.",
    "navigationGraph.openFailed": "Failed to open Classic ASP navigation graph: {error}",
    "navigationGraph.copyFailed": "Failed to copy Classic ASP navigation graph text: {error}",
    "navigationGraph.updateFailed": "Failed to update Classic ASP navigation graph: {error}",
    "excel.serverUnavailable": "Start the Classic ASP Language Server before exporting analysis.",
    "excel.noActiveFile": "Open a Classic ASP file before exporting analysis.",
    "excel.currentTitle": "Classic ASP: Export Current File Analysis",
    "excel.saveLabel": "Export",
    "excel.writeTitle": "Creating Classic ASP analysis workbook",
    "excel.exported": "Classic ASP analysis exported to {file}.",
    "workspaceFiles.serverUnavailable":
      "Start the Classic ASP Language Server before previewing workspace files.",
    "workspaceFiles.previewFailed": "Failed to preview Classic ASP workspace files: {error}",
    "workspaceFiles.settingsFailed": "Failed to save Classic ASP workspace file settings: {error}",
    "workspaceFiles.exportFailed": "Failed to export Classic ASP workspace analysis: {error}",
    "workspaceFiles.openFailed": "Failed to open Classic ASP workspace file: {error}",
    "workspaceFiles.settingsSaved": "Classic ASP workspace glob settings saved.",
    "workspaceFiles.viewTitle": "Classic ASP: Project glob files",
    "workspaceFiles.viewPanelTitle": "Classic ASP Files: Project glob",
    "logAnalysis.title": "Classic ASP: Debug Log Analysis",
    "workspaceFiles.workspaceUnavailable":
      "Open a workspace before saving Classic ASP workspace glob settings.",
    "flowchart.serverUnavailable":
      "Start the Classic ASP Language Server before building a flowchart.",
    "flowchart.noActiveFile": "Open a Classic ASP file before building the current file flowchart.",
    "flowchart.incomplete": "The Classic ASP flowchart became stale before it finished building.",
    "flowchart.openFailed": "Failed to open Classic ASP flowchart: {error}",
    "flowchart.copyFailed": "Failed to copy the Classic ASP flowchart: {error}",
    "flowchart.saveLabel": "Export",
    "flowchart.exported": "Exported flowchart to {file}.",
    "flowchart.exportFailed": "Failed to export flowchart: {error}",
    "flowchart.exportEmpty": "Flowchart content is empty.",
    "flowchart.copied": "Copied Mermaid flowchart.",
    "flowchart.currentTitle": "Classic ASP: Current File Flowchart",
    "flowchart.documentPanelTitle": "Classic ASP Flowchart: {name}",
    "comment.serverUnavailable": "Start the Classic ASP Language Server before toggling comments.",
    "comment.directiveRequiresWholeDocument":
      "Select the entire document to toggle an ASP language directive safely.",
    "comment.unsafeStructure":
      "Classic ASP comments were not changed because the selected structure is unsafe to transform.",
  },
  ja: {
    "server.binaryNotFound":
      "Classic ASP Language Server の実行ファイルが {path} にありません。`{command}` でビルドするか、配布済み VSIX をインストールしてください。",
    "server.binaryInvalid":
      "Classic ASP Language Server の path {path} は実行可能なファイルではありません。`{command}` でビルドするか、配布済み VSIX をインストールしてください。",
    "server.startFailed":
      "{path} の Classic ASP Language Server を起動できませんでした: {error}。Classic ASP の出力チャネルで詳細を確認してください。",
    "server.openOutput": "出力を開く",
    "maintenance.serverUnavailable":
      "保守コマンドを実行する前に Classic ASP Language Server を起動してください。",
    "maintenance.commandFailed": "Classic ASP 保守コマンドに失敗しました: {error}",
    "maintenance.reindexRequested": "Classic ASP ワークスペースの再インデックスを要求しました。",
    "maintenance.clearCacheCompleted":
      "Classic ASP のプロセスキャッシュとディスクキャッシュを削除しました。",
    "maintenance.clearDiskCacheCompleted": "Classic ASP のディスクキャッシュを削除しました。",
    "maintenance.clearProcessCacheCompleted": "Classic ASP のプロセスキャッシュを削除しました。",
    "maintenance.noOp": "Classic ASP の保守コマンドは変更なしで完了しました。",
    "status.tooltip": "Classic ASP Language Server",
    "status.loading.text": "ASP 読み込み中",
    "status.loading.tooltip": "Classic ASP Language Server が workspace data を読み込み中です。",
    "status.analyzing.text": "ASP 解析中",
    "status.analyzing.tooltip": "Classic ASP Language Server が ASP file を解析中です。",
    "status.progress.active": "実行中",
    "status.progress.cancel": "キャンセル",
    "status.progress.cancelling": "キャンセル中",
    "status.progress.analyzingStatusText": "ASP {task}",
    "status.progress.diagnostics": "document diagnostics",
    "status.progress.diagnosticsInclude": "include diagnostics 確認中",
    "status.progress.diagnosticsProjectFast": "fast project diagnostics 確認中",
    "status.progress.diagnosticsProject": "project diagnostics 確認中",
    "status.progress.diagnosticsSyntax": "syntax diagnostics 確認中",
    "status.progress.documentAnalysis": "document 解析",
    "status.progress.documentAnalysisCache": "document 解析 cache 更新中",
    "status.progress.documentAnalysisIncremental": "document 差分解析中",
    "status.progress.documentAnalysisParse": "document parse 中",
    "status.progress.documentAnalysisReady": "document 解析完了",
    "status.progress.excel": "Excel 作成中",
    "status.progress.excelChooseFile": "Excel 出力先選択中",
    "status.progress.excelFile": "Excel ファイルを書き込み中",
    "status.progress.excelFileCommit": "Excel ファイルを仕上げ中",
    "status.progress.excelFileRows": "Excel 行を書き込み中",
    "status.progress.excelFileSheet": "Excel シートを書き込み中",
    "status.progress.excelGraph": "Excel 解析グラフを取得中",
    "status.progress.excelAnalysisContext": "Excel グラフデータを分類中",
    "status.progress.excelAnalysisSummary": "Excel 解析サマリーを作成中",
    "status.progress.excelNormalizeGraph": "Excel グラフ payload を正規化中",
    "status.progress.excelSheet": "Excel シートを作成中",
    "status.progress.excelSheets": "Excel シートを作成中",
    "status.progress.excelWorkbook": "Excel ブックを生成中",
    "status.progress.flowchart": "フローチャートを生成中",
    "status.progress.flowchartBuildPayload": "フローチャート payload を作成中",
    "status.progress.flowchartCanonicalizeSymbols": "フローチャート symbol を正規化中",
    "status.progress.flowchartCollectIncludes": "フローチャート include ツリーを収集中",
    "status.progress.flowchartHydrateDocument": "フローチャート用 VBScript を復元中",
    "status.progress.flowchartIndexDocuments": "フローチャート用ドキュメントをインデックス中",
    "status.progress.flowchartLoadDocument": "フローチャート用ドキュメントを読み込み中",
    "status.progress.navigationGraph": "画面遷移グラフを生成中",
    "status.progress.navigationGraphBuildPayload": "画面遷移グラフ payload を作成中",
    "status.progress.navigationGraphCollectDocuments": "画面遷移グラフ用ドキュメントを収集中",
    "status.progress.navigationGraphExtract": "画面遷移を抽出中",
    "status.progress.navigationGraphResolveIncludes": "画面遷移 include 元を解決中",
    "status.progress.graphAddStructure": "グラフ構造を追加中",
    "status.progress.graphAddUsages": "グラフ使用箇所を追加中",
    "status.progress.graphCanonicalizeSymbols": "グラフ symbol を正規化中",
    "status.progress.graphCheckRelatedIncludes": "関連 include 解析の必要性を確認中",
    "status.progress.graphCollectIncomingIncludes": "取り込み元 include ファイルを収集中",
    "status.progress.graphCollectIncludes": "include ツリーを収集中",
    "status.progress.graphCollectRelatedIncludes": "関連 include ツリーを収集中",
    "status.progress.graphDocument": "現在のファイルのグラフを生成中",
    "status.progress.graphFilterIncomingIncludes": "取り込み元 include ファイルを絞り込み中",
    "status.progress.graphFindIncomingIncludes": "開いている取り込み元 include ファイルを確認中",
    "status.progress.graphFinalize": "グラフを仕上げ中",
    "status.progress.graphFolder": "フォルダーグラフを生成中",
    "status.progress.graphIndexDocuments": "グラフ用ドキュメントをインデックス中",
    "status.progress.graphLoadDocuments": "グラフ用ドキュメントを読み込み中",
    "status.progress.graphOpenDocuments": "開いているドキュメントのグラフを読み込み中",
    "status.progress.graphPrepareDocuments": "グラフ用ドキュメントを準備中",
    "status.progress.graphPrefetchIncludes": "include 先を先読み中",
    "status.progress.graphResolveIncludes": "include パスを解決中",
    "status.progress.graphReverseIncludeIndex": "逆 include インデックスを読み込み中",
    "status.progress.graphSpillIndexes": "グラフ用インデックスをディスクへ書き込み中",
    "status.progress.graphWorkspace": "ワークスペースグラフを生成中",
    "status.progress.graphWorkspaceIndex": "グラフ用ワークスペースインデックスを読み込み中",
    "status.progress.loadingStatusText": "ASP 読み込み中: {task}",
    "status.progress.none": "実行中の Classic ASP タスクはありません。",
    "status.progress.placeholder": "現在の Classic ASP タスク",
    "status.progress.title": "Classic ASP 進行状況",
    "status.progress.workspaceDiagnostics": "ワークスペース診断中",
    "status.progress.workspaceDiagnosticsIndexed": "インデックス済みワークスペース診断を確認中",
    "status.progress.workspaceDiagnosticsOpenDocuments": "開いているドキュメントの診断を確認中",
    "status.progress.workspaceIndex": "ワークスペースインデックスを読み込み中",
    "status.progress.workspaceIndexFailed": "ワークスペースインデックスに失敗",
    "status.progress.workspaceIndexWaitDocuments": "開いている文書の解析完了待ち",
    "status.progress.workspaceIndexParseFiles": "ワークスペースを構文解析中（ファイル数）",
    "status.progress.workspaceIndexReadCache": "ワークスペースキャッシュを読み込み中",
    "status.progress.workspaceIndexCatalog": "自動 include の索引を構築中",
    "status.progress.workspaceIndexScanFiles": "ワークスペースファイルを読み込み中（ファイル数）",
    "status.progress.workspaceIndexScanRoot": "対象ファイルを探索中（発見数）",
    "status.progress.workspaceIndexWriteCache":
      "ワークスペースインデックスを解析データベースへ書き込み中",
    "status.progress.workspaceIndexIncludeGraph": "ワークスペース include グラフを復元中",
    "status.progress.workspaceIndexFinalize": "ワークスペースインデックスを仕上げ中",
    "status.progress.workspacePreviewFiles": "ワークスペースファイルをプレビュー中",
    "navigationGraph.serverUnavailable":
      "画面遷移グラフを作成する前に Classic ASP Language Server を起動してください。",
    "navigationGraph.noActiveFile":
      "現在のファイルの画面遷移グラフを作成する前に Classic ASP ファイルを開いてください。",
    "navigationGraph.noFolder":
      "フォルダー画面遷移グラフを作成する前にフォルダーを選択してください。",
    "navigationGraph.currentTitle": "Classic ASP: 現在のファイル画面遷移グラフ",
    "navigationGraph.folderTitle": "Classic ASP: フォルダー画面遷移グラフ",
    "navigationGraph.workspaceTitle": "Classic ASP: プロジェクト画面遷移グラフ",
    "navigationGraph.documentPanelTitle": "Classic ASP 画面遷移グラフ: {name}",
    "navigationGraph.workspacePanelTitle": "Classic ASP 画面遷移グラフ: プロジェクト",
    "navigationGraph.incomplete": "Classic ASP 画面遷移グラフの応答が不完全でした。",
    "navigationGraph.openFailed": "Classic ASP 画面遷移グラフを開けませんでした: {error}",
    "navigationGraph.copyFailed":
      "Classic ASP 画面遷移グラフのテキストをコピーできませんでした: {error}",
    "navigationGraph.updateFailed": "Classic ASP 画面遷移グラフを更新できませんでした: {error}",
    "excel.serverUnavailable":
      "解析を出力する前に Classic ASP Language Server を起動してください。",
    "excel.noActiveFile": "解析を出力する前に Classic ASP ファイルを開いてください。",
    "excel.currentTitle": "Classic ASP: 現在のファイル解析を Excel 出力",
    "excel.saveLabel": "出力",
    "excel.writeTitle": "Classic ASP 解析ブックを作成中",
    "excel.exported": "Classic ASP 解析を {file} に出力しました。",
    "workspaceFiles.serverUnavailable":
      "ワークスペースファイルをプレビューする前に Classic ASP Language Server を起動してください。",
    "workspaceFiles.previewFailed":
      "Classic ASP ワークスペースファイルのプレビューに失敗しました: {error}",
    "workspaceFiles.settingsFailed":
      "Classic ASP ワークスペースファイル設定の保存に失敗しました: {error}",
    "workspaceFiles.exportFailed": "Classic ASP ワークスペース解析の出力に失敗しました: {error}",
    "workspaceFiles.openFailed": "Classic ASP ワークスペースファイルを開けませんでした: {error}",
    "workspaceFiles.settingsSaved": "Classic ASP の workspace glob 設定を保存しました。",
    "workspaceFiles.viewTitle": "Classic ASP: プロジェクト glob ファイル",
    "workspaceFiles.viewPanelTitle": "Classic ASP ファイル: プロジェクト glob",
    "logAnalysis.title": "Classic ASP: デバッグログ解析",
    "workspaceFiles.workspaceUnavailable":
      "Classic ASP の workspace glob 設定を保存する前にワークスペースを開いてください。",
    "flowchart.serverUnavailable":
      "フローチャートを作成する前に Classic ASP Language Server を起動してください。",
    "flowchart.noActiveFile":
      "現在のファイルのフローチャートを作成する前に Classic ASP ファイルを開いてください。",
    "flowchart.incomplete":
      "Classic ASP フローチャートの作成中にドキュメントが更新されたため、表示を中止しました。",
    "flowchart.openFailed": "Classic ASP フローチャートを開けませんでした: {error}",
    "flowchart.copyFailed": "Classic ASP フローチャートのコピーに失敗しました: {error}",
    "flowchart.saveLabel": "出力",
    "flowchart.exported": "フローチャートを {file} に出力しました。",
    "flowchart.exportFailed": "フローチャートの出力に失敗しました: {error}",
    "flowchart.exportEmpty": "フローチャートの内容が空です。",
    "flowchart.copied": "Mermaid フローチャートをコピーしました。",
    "flowchart.currentTitle": "Classic ASP: 現在のファイルフローチャート",
    "flowchart.documentPanelTitle": "Classic ASP フローチャート: {name}",
    "comment.serverUnavailable":
      "コメントを切り替える前に Classic ASP Language Server を起動してください。",
    "comment.directiveRequiresWholeDocument":
      "ASP の言語 directive を安全に切り替えるにはドキュメント全体を選択してください。",
    "comment.unsafeStructure":
      "選択された構造を安全に変換できないため、Classic ASP のコメントは変更しませんでした。",
  },
};

export function extensionLocalizer(): (
  key: ExtensionMessageKey,
  args?: ExtensionMessageArgs,
) => string {
  return extensionLocalizerForLocale(extensionLocale());
}

export function extensionLocalizerForLocale(
  locale: AspFlowchartLocale,
): (key: ExtensionMessageKey, args?: ExtensionMessageArgs) => string {
  return (key, args) => {
    let message = extensionMessages[locale][key] ?? extensionMessages.en[key];
    for (const [name, value] of Object.entries(args ?? {})) {
      message = message.replaceAll(`{${name}}`, value);
    }
    return message;
  };
}

export function extensionLocale(): AspFlowchartLocale {
  const configLocale = vscode.workspace.getConfiguration("aspLsp").get<string>("locale") ?? "auto";
  return localeFromSetting(configLocale);
}

export function localeFromSetting(value: unknown): AspFlowchartLocale {
  return value === "ja" || (value !== "en" && vscode.env.language.startsWith("ja")) ? "ja" : "en";
}
