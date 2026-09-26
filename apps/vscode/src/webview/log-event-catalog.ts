export type LogDescription = readonly [string, string];
// Keys correspond to event families emitted by internal/lspserver. More specific
// families take precedence; lifecycle suffixes are explained separately.
const families: Record<string, LogDescription> = {
  javascriptProjectIdentity: [
    "JavaScriptプロジェクトの識別情報を保存します。write.failedは保存失敗で、原文に原因が記録されます。",
    "Persists JavaScript project identity. write.failed indicates persistence failure; inspect the original error.",
  ],
  "workspace.index": [
    "ワークスペース索引の構築処理です。failedは構築に失敗したことを示します。",
    "Builds the workspace index; failed indicates that indexing failed.",
  ],
  "diagnostics.initialSyntax": [
    "文書を開いた直後の構文診断を先に公開します。重いプロジェクト解析の完了を待たず、最初の問題表示を返す段階です。",
    "Publishes initial syntax diagnostics before heavier project analysis completes.",
  ],
  "diagnostics.syntax": [
    "構文検査の結果をエディターに公開する段階です。",
    "Publishes syntax-check results to the editor.",
  ],
  "diagnostics.fast": [
    "文書単体で得られる軽量な診断を公開する段階です。",
    "Publishes lightweight diagnostics available from the current document.",
  ],
  "diagnostics.include": [
    "include先の情報を反映した診断の段階です。文書更新で古くなった結果は採用しません。",
    "Handles include-aware diagnostics and rejects results invalidated by document changes.",
  ],
  "diagnostics.projectFast": [
    "プロジェクト情報を使う早期診断を公開します。",
    "Publishes early project-aware diagnostics.",
  ],
  "diagnostics.project": [
    "プロジェクト全体の文脈を反映した診断を公開します。",
    "Publishes diagnostics using the full project context.",
  ],
  "diagnostics.final": [
    "診断の最終結果を公開します。世代が変わった場合は古い結果を破棄します。",
    "Publishes final diagnostics, discarding results from an outdated generation.",
  ],
  "diagnostics.documentOpen": [
    "文書を開いた後のバックグラウンド解析を扱います。失敗時は原文のエラーを確認してください。",
    "Handles background analysis after opening a document; inspect the original error on failure.",
  ],
  "diagnostics.step": [
    "個別の検査工程の計測です。stepが検査名、durationMsが経過時間、countがその工程の結果数です。",
    "Measures one checking stage: step names the stage, durationMs is elapsed time, and count is its result count.",
  ],
  diagnostics: [
    "文書の診断処理を開始、再利用、または公開します。uriが対象文書を示します。",
    "Starts, reuses, or publishes document diagnostics. uri identifies the document.",
  ],
  "documentChange.artifacts": [
    "変更後の文書解析成果物が準備できたことを記録します。",
    "Records readiness of analysis artifacts for a changed document.",
  ],
  "documentChange.scheduleDiagnostics.postScheduleProjectUpdate": [
    "診断ジョブの登録後に出力する経路マーカーです。名前にProjectUpdateが含まれますが、この行だけでプロジェクト更新の実行や完了は確認できません。",
    "A path marker emitted after registering a diagnostic job. Despite its ProjectUpdate name, this line alone does not establish execution or completion of a project update.",
  ],
  "documentChange.scheduleDiagnostics": [
    "編集後の診断を予約します。実際の検査完了を表すイベントではありません。",
    "Schedules diagnostics after an edit; this does not mean checking has completed.",
  ],
  "document.open": [
    "クライアントから文書を開く通知を受け、解析対象として登録します。",
    "Registers a document after the client opens it.",
  ],
  "document.save": [
    "文書の保存通知を受け、必要な更新を行います。",
    "Handles a document-save notification and required updates.",
  ],
  projectUpdate: [
    "文書変更をプロジェクトの解析状態に反映する処理を予約します。",
    "Schedules propagation of document changes into project analysis state.",
  ],
  "invalidation.jsProject": [
    "JavaScriptプロジェクトの解析キャッシュを無効化します。次の要求で必要な状態を作り直します。",
    "Invalidates JavaScript project analysis for rebuilding on subsequent requests.",
  ],
  "invalidation.includePublicBoundary": [
    "includeの公開宣言境界が変化し、依存する解析結果を無効化します。",
    "Invalidates dependent analysis after an include's public declaration boundary changes.",
  ],
  "invalidation.includeResolution": [
    "設定変更などでincludeの解決条件が変わったため、参照先のキャッシュを無効化します。",
    "Invalidates include resolution after its configuration or inputs change.",
  ],
  "include.publicBoundary": [
    "includeが公開する宣言の境界情報を再利用します。",
    "Reuses an include's public declaration boundary information.",
  ],
  workspaceFolders: [
    "ワークスペースフォルダーの構成変更を反映します。",
    "Applies changes to workspace folders.",
  ],
  "workspace.configuration": [
    "クライアントからワークスペース設定を取得します。失敗時は取得エラーが原文に記録されます。",
    "Retrieves workspace configuration from the client; failures include the original error.",
  ],
  configuration: [
    "サーバー設定の変更を比較します。unchangedは設定が同じ、changedは更新が必要であることを示します。",
    "Compares server settings: unchanged keeps current state; changed applies updated settings.",
  ],
  "legacyDiskCache.cleanup": [
    "旧形式のディスクキャッシュを整理します。成功時の数字は削除した項目数です。failedは削除に失敗した記録です。",
    "Cleans legacy disk caches; the number reports removed entries on success; failed records a cleanup error.",
  ],
  "semanticTokens.full": [
    "文書の意味に応じた色分けデータを生成します。cacheHitは保存結果、inflightReuseは実行中の同じ要求、incrementalReuseは差分結果の再利用です。",
    "Builds semantic highlighting. cacheHit reuses a stored result, inflightReuse joins ongoing work, and incrementalReuse reuses incremental data.",
  ],
  "javascript.diagnostics.prewarm": [
    "後続要求を速くするため、JavaScript診断の解析状態を事前に準備します。",
    "Prepares JavaScript diagnostic state for faster subsequent requests.",
  ],
  "javascript.diagnostics.worker": [
    "JavaScript診断をワーカー経路で実行します。payloadBytesは処理に渡すデータ量です。",
    "Runs JavaScript diagnostics through the worker path; payloadBytes is the input size.",
  ],
  "javascriptSemantic.worker": [
    "JavaScriptの意味解析をワーカー経路に渡すことを記録します。",
    "Records dispatch of JavaScript semantic analysis to the worker path.",
  ],
  "javascript.languageService": [
    "JavaScript言語サービスを作成・再利用・再構築します。generationは解析世代、buildsは構築回数です。errorの場合は原文に原因が記録されます。",
    "Creates, reuses, or rebuilds the JavaScript language service. generation tracks its revision and builds counts rebuilds; error carries the failure details.",
  ],
  "javascript.openProjectFiles": [
    "JavaScriptプロジェクトに含める文書を収集します。収集結果を再利用できる場合は読み直しを省きます。",
    "Collects documents for the JavaScript project, reusing a collection when possible.",
  ],
  "js.snapshot.changeRange": [
    "JavaScript文書の差分範囲を再利用できるか確認します。hitは差分利用、missは利用可能な範囲がない状態です。",
    "Checks whether a JavaScript snapshot change range is reusable: hit uses it, miss lacks a usable range.",
  ],
  "vbProject.context.refresh": [
    "VBScriptのプロジェクト文脈を更新します。reasonは更新のきっかけです。",
    "Refreshes VBScript project context; reason identifies the trigger.",
  ],
  "vbProject.summaryGraph": [
    "includeや公開宣言をまとめたVBScriptの概要グラフを収集・構築・再利用します。",
    "Collects, builds, or reuses a VBScript summary graph of includes and public declarations.",
  ],
  sourceSnapshot: [
    "ソースファイルの不変スナップショットを扱います。hitはメモリ再利用、readは読み込み、invalidateは変更に伴う無効化です。",
    "Handles immutable source snapshots: hit reuses memory, read loads a file, and invalidate drops outdated state.",
  ],
  referenceCache: [
    "参照検索の結果を扱います。symbolは対象名、referencesは参照数です。inflight.joinは実行中の同じ検索を共有し、partialは途中までの結果、promotedは変更のない結果の世代更新です。",
    "Handles reference-query results. symbol names the target and references counts matches. inflight.join shares an ongoing query, partial retains partial results, and promoted carries unchanged results to a new generation.",
  ],
  "vb.references.workspace.shadowed": [
    "同じ名前のローカル宣言などに隠れる文書を参照検索から除外します。",
    "Excludes documents where local declarations shadow the searched name.",
  ],
  "vb.references.workspace.candidates": [
    "ワークスペース内で対象名を参照している可能性のある文書を集めます。",
    "Collects workspace documents that may reference the requested name.",
  ],
  "vb.references.workspace.reuse": [
    "ワークスペース参照検索の既存結果を再利用します。",
    "Reuses existing workspace reference results.",
  ],
  "vb.references.worker": [
    "参照検索ワーカーの結果を確認します。staleは文書更新などで結果が古くなった状態です。",
    "Checks reference-worker results; stale means changes invalidated them.",
  ],
  "vb.references.reachability": [
    "includeの到達関係を確認し、対象に到達できない文書の検索を省きます。",
    "Skips reference scans for documents that cannot reach the target through includes.",
  ],
  "vb.references.batch.documentDelta": [
    "文書差分から参照検索の結果を再利用し、全面的な再検索を避けます。",
    "Reuses reference results from a document delta instead of rescanning everything.",
  ],
  "vb.references.batch": [
    "複数の名前の参照をまとめて処理します。warmedは準備済み件数、cacheHitsは再利用件数、dbRestoredはDB復元件数、examinedSegmentsは調べた区間数です。",
    "Processes reference queries in batches. warmed counts prepared results, cacheHits counts reuses, dbRestored counts database restores, and examinedSegments counts scanned segments.",
  ],
  "format.embedded": [
    "埋め込みHTML・CSS・JavaScriptなどの整形を行い、元文書の位置に編集結果を戻します。",
    "Formats embedded content and maps edits back to the source document.",
  ],
  "format.conversion": [
    "整形変換の開始または終了です。scopeは対象範囲、editsは生成された編集数です。",
    "Starts or finishes formatting conversion; scope identifies the range and edits counts produced edits.",
  ],
  "analysis.total": [
    "LSPの解析処理全体の開始または終了です。配下の工程を含むため、各工程との単純合計は実時間になりません。",
    "Starts or finishes overall LSP analysis, including nested stages; summing them does not give wall time.",
  ],
  "check.total": [
    "LSPの検査全体の開始または終了です。diagnosticsは最終的な診断数です。",
    "Starts or finishes the overall LSP check; diagnostics is the final diagnostic count.",
  ],
  workspaceDiagnostics: [
    "ワークスペース診断でプロセス内のキャッシュを確認します。documentsは対象文書数、missesは再解析が必要な文書数です。",
    "Checks the workspace diagnostic process cache. documents counts inputs and misses counts documents requiring analysis.",
  ],
  "vbscript.worker": [
    "VBScript文書群の解析をワーカーに渡します。dispatchは投入、startedは開始、終了状態は実行結果を示します。",
    "Dispatches VBScript documents to a worker; dispatch, started, and the terminal state track its lifecycle.",
  ],
  "check.workspace.vbscript.diagnostics.worker": [
    "ワークスペースのVBScript診断をワーカー経路で実行します。documentsは対象文書数です。",
    "Runs workspace VBScript diagnostics through the worker path; documents is the input count.",
  ],
  "worker.payload.bytes": [
    "ワーカーに渡すデータ量を記録します。時間ではなくバイト数です。",
    "Records worker input size in bytes, not elapsed time.",
  ],
  "asp.graph.bulk": [
    "複数文書の解析グラフを一括構築します。spill.writeは中間データの退避、completeは構築完了を示します。",
    "Builds an analysis graph in bulk. spill.write stores intermediate data; complete marks completion.",
  ],
  "graphVbIndex.workerSymbolExtraction": [
    "グラフに必要なVBScriptの宣言・シンボルを文書群から抽出します。",
    "Extracts VBScript declarations and symbols needed by the graph.",
  ],
  "graphVbIndex.extendTypeHints": [
    "抽出済みのVBScriptグラフに型情報を補います。",
    "Adds type information to the extracted VBScript graph.",
  ],
  "includeDiagnostics.directIncludes": [
    "直接参照しているinclude先を診断対象として確認します。",
    "Checks directly included documents for include diagnostics.",
  ],
  "includeDiagnostics.cycleGraph": [
    "include関係の循環を調べるためのグラフを検査します。",
    "Checks the include graph for cycles.",
  ],
  "includeDiagnostics.reuse": [
    "include診断の既存結果を再利用します。",
    "Reuses existing include diagnostics.",
  ],
  "analysis.parse.skeleton": [
    "編集後の構造を軽量に解析し、後続処理の骨格を作ります。",
    "Builds a lightweight structural skeleton after an edit.",
  ],
  "analysis.parse.impact": [
    "編集の影響範囲を判定します。mode=fullは全体解析、reasonは差分解析を選べなかった理由です。",
    "Classifies edit impact. mode=full selects full parsing; reason explains why incremental parsing was not used.",
  ],
  "analysis.parse.incremental": [
    "変更範囲に応じて構文解析結果を差分更新します。",
    "Updates parsing results incrementally for the changed range.",
  ],
  "analysis.vbscript.reuse": [
    "変更の影響を受けないVBScript解析結果を再利用します。",
    "Reuses VBScript analysis unaffected by the edit.",
  ],
  "check.javascriptSyntax.reuse": [
    "JavaScriptの構文診断を再利用します。",
    "Reuses JavaScript syntax diagnostics.",
  ],
  "check.javascriptDiagnostics.reuse": [
    "JavaScriptの意味診断を再利用します。",
    "Reuses JavaScript semantic diagnostics.",
  ],
  "check.vbscript.diagnostics.reuse": [
    "VBScript診断結果を再利用します。",
    "Reuses VBScript diagnostic results.",
  ],
  "htmlDiagnostics.reuse": ["HTML診断結果を再利用します。", "Reuses HTML diagnostic results."],
  "cssDiagnostics.reuse": ["CSS診断結果を再利用します。", "Reuses CSS diagnostic results."],
  workspaceArtifact: [
    "文書の解析成果物をワークスペース索引へ適用、または保存します。",
    "Applies or stores document analysis artifacts in the workspace index.",
  ],
  workspaceManifest: [
    "ワークスペース文書の保存用マニフェストを更新します。",
    "Updates the persisted workspace document manifest.",
  ],
  workspaceIncludeGraph: [
    "ワークスペース全体のinclude依存関係を保存します。",
    "Persists workspace include dependencies.",
  ],
  workspaceVBAutoIncludeCatalog: [
    "VBScriptの自動include候補となる公開宣言のカタログを保存・削除します。",
    "Stores or deletes the public-declaration catalog used for automatic VBScript includes.",
  ],
  workspaceIndex: [
    "ワークスペースの文書・宣言索引を構築または保存します。startedからcompleteまでが処理範囲で、transactionやflushは保存工程です。",
    "Builds or persists the workspace document/symbol index. started to complete spans the operation; transaction and flush are persistence stages.",
  ],
  "memory.pressure": [
    "解析キャッシュのメモリ使用量と圧迫状態を報告します。原文の数値はキャッシュ管理の判断材料で、処理時間ではありません。",
    "Reports analysis-cache memory usage and pressure. Its values describe memory management, not processing time.",
  ],
  "visual.refresh": [
    "表示更新をクライアントへ要求します。reasonは更新のきっかけです。",
    "Requests a client visual refresh; reason identifies its trigger.",
  ],
  "server.warning": [
    "サーバーが警告を記録しています。個別の原因は原文を確認してください。",
    "The server recorded a warning; inspect the original message for its cause.",
  ],
  "debugLogFile.queue": [
    "ログ書き込み待ちキューの混雑によって省略したログ件数を報告します。traceとotherは省略した種類ごとの件数です。",
    "Reports dropped log records due to writer-queue pressure; trace and other count the dropped categories.",
  ],
  "debugLogFile.write": [
    "デバッグログファイルへの書き込みです。failedの場合はファイルへの記録に失敗しています。",
    "Writes a debug log file; failed means the record could not be persisted.",
  ],
};
const databaseParts: Record<string, LogDescription> = {
  workspaceIndex: ["文書と宣言のワークスペース索引", "the workspace document and symbol index"],
  javascriptProjectIdentity: ["JavaScriptプロジェクトの識別情報", "JavaScript project identity"],
  workspaceLegacyUndefinedGlobals: [
    "従来互換の未定義グローバル名カタログ",
    "the legacy undefined-global catalog",
  ],
  workspaceVBAutoIncludeCatalog: [
    "VBScript自動include候補カタログ",
    "the VBScript automatic-include catalog",
  ],
  referenceQueries: ["名前ごとの参照検索結果", "reference-query results"],
  referenceCountSummaries: ["参照数の概要", "reference-count summaries"],
  referenceDocuments: ["参照検索対象の文書情報", "reference-document information"],
  fileAnalysis: ["文書の解析成果物", "file analysis artifacts"],
  diagnostics: ["診断結果", "diagnostic results"],
  workspaceIncludeGraph: ["include依存グラフ", "the include dependency graph"],
  graphPayload: ["表示・出力用の解析グラフ", "the display/export graph"],
  fileBundle: ["文書の解析データ一式", "a file analysis bundle"],
  referenceSchema: ["参照索引の保存形式", "the reference-index storage schema"],
};
const operations: Record<string, LogDescription> = {
  hit: ["有効な保存結果が見つかり再利用します。", "Found a valid saved result and reuses it."],
  miss: [
    "再利用できる保存結果が見つかりませんでした。後続処理は呼び出し元の経路によります。",
    "No reusable saved result was found; the next action depends on the caller.",
  ],
  stale: [
    "保存結果が現在の文書・設定と一致せず、採用しません。",
    "The saved result does not match current inputs and is rejected.",
  ],
  write: [
    "解析結果を保存します。bytesは保存データ量です。",
    "Persists results; bytes is the stored data size.",
  ],
  restore: ["保存済みの状態をメモリに復元します。", "Restores saved state into memory."],
  read: [
    "保存結果を読み出します。hitsとmissesは利用可否の件数です。",
    "Reads saved results; hits and misses count usable and missing entries.",
  ],
  queued: [
    "保存処理をキューに積みます。この時点で保存完了とは限りません。",
    "Queues persistence; this does not mean the write has completed.",
  ],
  publish: [
    "構築結果を現在の解析状態として公開します。",
    "Publishes a built result as the current analysis state.",
  ],
  reset: [
    "保存形式の不一致などに対応して索引をリセットします。",
    "Resets an index, for example after a storage-schema mismatch.",
  ],
};
const catalogPrefixes = Object.keys(families).sort((a, b) => b.length - a.length);

export function describeCatalogEvent(event: string, locale: "ja" | "en"): string | undefined {
  const language = locale === "ja" ? 0 : 1;
  if (event.startsWith("database.")) {
    const [, component, operation, outcome] = event.split(".");
    if (outcome === "failed" && databaseParts[component])
      return language === 0
        ? `${databaseParts[component][0]}の${operation}処理に失敗しました。保存成功を示す記録ではありません。原因は原文を確認してください。`
        : `${operation} failed for ${databaseParts[component][1]}. This does not record a successful write; inspect the original error.`;
    if (databaseParts[component] && operations[operation])
      return `${databaseParts[component][language]}: ${operations[operation][language]}`;
  }
  if (event.startsWith("analysisDatabase.")) {
    const component = event.split(".")[1];
    const subject =
      databaseParts[component]?.[language] ??
      (language === 0 ? "解析データベース" : "the analysis database");
    return language === 0
      ? `${subject}の処理で問題が発生しました。末尾の操作名と原文のエラーを確認してください。openは接続、flushは書き出し、closeは終了です。`
      : `A problem occurred while handling ${subject}. Inspect the operation and original error. open connects, flush writes pending data, and close shuts down storage.`;
  }
  const prefix = catalogPrefixes.find((key) => event === key || event.startsWith(key + "."));
  if (!prefix) return undefined;
  const context = families[prefix][language];
  const suffix = event.split(".").at(-1)!;
  if (["failed", "error", "stale", "cancelled"].includes(suffix))
    return `${logStatusDescription(suffix, locale)} ${language === 0 ? "対象工程の役割: " : "Operation purpose: "}${context}`;
  return context;
}
export function logStatusDescription(status: string, locale: "ja" | "en"): string {
  const descriptions: Record<string, LogDescription> = {
    started: [
      "処理を開始しました。完了したことは意味しません。",
      "Processing started; completion is not implied.",
    ],
    received: [
      "通信を受信しました。処理完了を意味しません。クライアント応答には往復時間が記録される場合があります。",
      "A message was received, not necessarily processed. Client responses may already include round-trip time.",
    ],
    completed: [
      "処理が終了しました。所要時間があれば時間グラフに表示します。",
      "Processing ended; recorded durations appear in the timing charts.",
    ],
    complete: ["処理が完了しました。", "Processing completed."],
    ok: ["エラーなしで処理を終えました。", "Processing finished without a reported error."],
    error: [
      "エラーとして終了しました。codeと原文で原因を確認してください。",
      "Processing ended with an error; inspect code and the original line.",
    ],
    failed: [
      "処理に失敗しました。原文に記録された原因を確認してください。",
      "Processing failed; inspect the cause in the original line.",
    ],
    cancelled: [
      "新しい要求や編集などによって処理が中断されました。必ずしも障害ではありません。",
      "Processing was cancelled, possibly by a newer request or edit; this is not necessarily a fault.",
    ],
    stale: [
      "古い世代の結果になり、現在の結果として採用しません。",
      "The result became stale and is not used as the current result.",
    ],
    unknown: [
      "この行から処理状態を確定できません。",
      "This line does not establish the processing state.",
    ],
    unmatched: [
      "対応する待機中の要求が見つからない応答です。",
      "The response did not match a pending request.",
    ],
    reuse: ["既存の解析結果を再利用しています。", "Existing analysis results are reused."],
    hit: operations.hit,
    miss: operations.miss,
  };
  return (
    descriptions[status]?.[locale === "ja" ? 0 : 1] ??
    (locale === "ja" ? `記録された状態: ${status}` : `Recorded state: ${status}`)
  );
}
export function logFieldDescription(field: string, locale: "ja" | "en"): string {
  const fields: Record<string, LogDescription> = {
    spanId: [
      "処理1回ごとの識別子。並行して同じ処理が動いても開始と終了を正確に対応付けます。",
      "Unique operation identifier that pairs its start and end even when identical operations overlap.",
    ],
    parentSpanId: [
      "この処理を開始した親処理のspanId。時刻やURIからの推定より優先して親子を接続します。",
      "The initiating parent's spanId. Explicit correlation takes precedence over time or URI inference.",
    ],
    traceId: [
      "起点の処理から派生した一連の処理の識別子。非同期処理でも同じ値を引き継ぎます。",
      "Identifier shared by an originating operation and its descendants, including asynchronous work.",
    ],
    parentRequestId: [
      "親要求の識別子。親子関係を明示する追加ログ項目です。",
      "Parent request identifier when explicitly recorded.",
    ],
    receivedAt: [
      "サーバーが通信を受信した時刻。ログ書き出し時刻とは異なる場合があります。",
      "Server message reception time, which can differ from log emission time.",
    ],
    matched: [
      "応答と待機中のクライアント要求を対応付けられたか。",
      "Whether the response matched a pending client request.",
    ],
    scope: [
      "整形の対象範囲（文書全体または選択範囲）。",
      "Formatting scope: document or selection.",
    ],
    payload: [
      "ワーカーへ渡すデータのバイト数。時間ではありません。",
      "Worker input size in bytes, not time.",
    ],
    reason: [
      "再利用・無効化・処理経路の選択などの理由。",
      "Reason for reuse, invalidation, or selecting a processing path.",
    ],
    durationMs: [
      "この処理の経過時間（ミリ秒）。子工程や並列処理と重複する場合があります。",
      "Elapsed milliseconds for this operation; nested or parallel operations may overlap.",
    ],
    roundTripMs: [
      "サーバーが要求を送ってから応答を受けるまでの時間（ミリ秒）。",
      "Milliseconds from sending a request to receiving its response.",
    ],
    deliveryMs: [
      "応答を受信してから待機している処理へ届けるまでの時間（ミリ秒）。",
      "Milliseconds spent delivering a received response to its waiting operation.",
    ],
    count: [
      "工程固有の件数。checkでは診断数、analysis.parseでは領域数、診断DBのhitでは復元した診断数です。開始時や中断時の0は、問題なしを保証しません。",
      "Stage-specific count: diagnostics for check, regions for analysis.parse, restored diagnostics for a diagnostic database hit. Zero at start or interruption does not guarantee a clean result.",
    ],
    uri: ["処理対象文書のURI。", "URI of the document being processed."],
    requestId: [
      "要求と応答を対応付ける識別子。異なるセッションで同じ値が使われることがあります。",
      "Identifier matching requests and responses; different sessions may reuse it.",
    ],
    version: ["クライアントが管理する文書バージョン。", "Document version tracked by the client."],
    generation: [
      "解析状態の世代。古い世代の結果は破棄される場合があります。",
      "Analysis generation; results from older generations may be discarded.",
    ],
    method: ["LSPの要求・通知の名前。", "Name of the LSP request or notification."],
    paramsBytes: ["受信したパラメーターのバイト数。", "Byte size of received parameters."],
    resultBytes: ["応答結果のバイト数。", "Byte size of the response result."],
    payloadBytes: ["処理に渡す入力データのバイト数。", "Byte size of the processing input."],
    documents: ["対象となる文書の数。", "Number of documents involved."],
    status: ["処理の結果や状態。", "Processing result or status."],
    step: ["計測対象の検査工程名。", "Name of the measured checking stage."],
    position: [
      "LSP位置。行とUTF-16列は0から数えます。",
      "LSP position: zero-based line and UTF-16 column.",
    ],
    code: [
      "LSP/JSON-RPCエラーコード。-32800は要求のキャンセルです。",
      "LSP/JSON-RPC error code; -32800 means request cancelled.",
    ],
    elapsed: [
      "テキストログに記録された経過時間をミリ秒へ換算した値。",
      "Elapsed time from the text log, converted to milliseconds.",
    ],
  };
  return (
    fields[field]?.[locale === "ja" ? 0 : 1] ??
    (locale === "ja"
      ? `${field}に記録された補助情報です。意味の確定が必要な場合はイベントの説明と原文を確認してください。`
      : `Additional information recorded as ${field}. Consult the event description and original record for context.`)
  );
}
