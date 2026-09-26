import { LogDetails } from "./log-details";
import { LogHint as Hint } from "./log-hint";
import { LogExecutionTree } from "./log-execution-tree-view";
import { createMemo, createSignal } from "solid-js";
import { render, type JSX } from "@solidjs/web";
import { WebviewErrorBoundary } from "./webview-error-boundary";
import {
  logHeatmap,
  logGroupKey,
  logTimingSummary,
  parseDebugLog,
  type LogLocale,
} from "./log-analysis-model";
import { logStatusDescription } from "./log-event-catalog";
import styles from "./log-analysis.css?inline";

const sample = `2026-09-08T01:00:00.000Z DEBUG lsp.request.received [asp-lsp] lsp.request.received method=textDocument/hover requestId=7
2026-09-08T01:00:00.030Z DEBUG diagnostics.step [asp-lsp] check.parser in 3.0 ms count=0 uri=file:///demo/index.asp
2026-09-08T01:00:00.140Z DEBUG diagnostics.step [asp-lsp] check.vbscript.types in 105.0 ms count=2 uri=file:///demo/index.asp
2026-09-08T01:00:00.180Z DEBUG lsp.request.completed [asp-lsp] lsp.request.completed durationMs=180 method=textDocument/hover requestId=7 status=ok
2026-09-08T01:00:00.310Z DEBUG diagnostics.step [asp-lsp] check.vbscript.types in 120.0 ms count=1 uri=file:///demo/index.asp
2026-09-08T01:00:00.450Z DEBUG diagnostics.step [asp-lsp] check.vbscript.types in 90.0 ms count=1 uri=file:///demo/index.asp
2026-09-08T01:00:00.500Z DEBUG diagnostics.step [asp-lsp] check.css.cancelled in 5.0 ms count=0 uri=file:///demo/index.asp`;

function App(): JSX.Element {
  const [locale, setLocale] = createSignal<LogLocale>(
    document.documentElement.lang === "ja" ? "ja" : "en",
  );
  const t = (ja: string, en: string) => (locale() === "ja" ? ja : en);
  const [screen, setScreen] = createSignal<"tree" | "analysis">("tree");
  const [input, setInput] = createSignal("");
  const [submitted, setSubmitted] = createSignal("");
  const [query, setQuery] = createSignal("");
  const [status, setStatus] = createSignal("all");
  const [group, setGroup] = createSignal("");
  const [page, setPage] = createSignal(0);
  const [selected, setSelected] = createSignal<number>();
  const [view, setView] = createSignal<"duration" | "timeline">("duration");
  const parsed = createMemo(() => parseDebugLog(submitted(), locale()));
  const entries = createMemo(() => parsed().entries);
  const visible = createMemo(() =>
    entries().filter(
      (entry) =>
        (status() === "all" ||
          (status() === "unknown" ? !entry.known : ["failed", "error"].includes(entry.status))) &&
        (!group() || logGroupKey(entry) === group()) &&
        (!query() ||
          (entry.raw + " " + entry.explanation).toLowerCase().includes(query().toLowerCase())),
    ),
  );
  const summary = createMemo(() => logTimingSummary(visible()));
  const slow = createMemo(() =>
    visible()
      .filter((entry) => entry.duration !== undefined)
      .sort((a, b) => b.duration! - a.duration!),
  );
  const heatmap = createMemo(() => logHeatmap(visible()));
  const detail = createMemo(() => entries().find((entry) => entry.line === selected()));
  const pageCount = createMemo(() => Math.max(1, Math.ceil(visible().length / 100)));
  const rows = createMemo(() =>
    visible().slice(
      Math.min(page(), pageCount() - 1) * 100,
      (Math.min(page(), pageCount() - 1) + 1) * 100,
    ),
  );
  const clock = (value: number) =>
    new Date(value).toLocaleTimeString(locale(), {
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
      fractionalSecondDigits: 3,
    });
  const ms = (value: number | undefined) =>
    value === undefined
      ? "—"
      : `${value.toLocaleString(locale(), { maximumFractionDigits: 3 })} ms`;
  const durationHelp = () =>
    t(
      "記録された経過時間です。CPU時間ではありません。要求全体・子工程・並列処理は重複するため合計して実時間を求めることはできません。長い処理はボトルネック候補で、原因の確定には同じURIや要求の前後も確認してください。",
      "Recorded elapsed time, not CPU time. Requests, child stages, and parallel work overlap; their sum is not wall time. Long operations are bottleneck candidates; inspect nearby events for the same URI/request before attributing a cause.",
    );
  const analyze = (text: string) => {
    setSubmitted(text);
    setQuery("");
    setStatus("all");
    setGroup("");
    setPage(0);
    setSelected(undefined);
  };
  const timeline = createMemo(() => {
    const timed = slow().filter((entry) => entry.timestamp !== undefined);
    const start = timed.length
      ? Math.min(...timed.map((entry) => entry.timestamp! - entry.duration!))
      : 0;
    const end = timed.length ? Math.max(...timed.map((entry) => entry.timestamp!)) : 1;
    return {
      start,
      span: Math.max(1, end - start),
      rows: timed.sort((a, b) => a.timestamp! - b.timestamp!).slice(0, 40),
    };
  });
  return (
    <>
      <style>{styles}</style>
      <main class="log-app">
        <header class="log-header">
          <div>
            <span class="eyebrow">CLASSIC ASP / DEBUG</span>
            <h1>{t("ログ解析", "Log analysis")}</h1>
          </div>
          <label>
            {t("説明言語", "Explanation language")}
            <select
              aria-label={t("説明言語", "Explanation language")}
              value={locale()}
              onChange={(event) => setLocale(event.currentTarget.value as LogLocale)}
            >
              <option value="ja">日本語</option>
              <option value="en">English</option>
            </select>
          </label>
        </header>
        <section class="input-section">
          <label for="log-input">{t("ログを貼り付け", "Paste logs")}</label>
          <textarea
            id="log-input"
            rows={4}
            value={input()}
            spellcheck={false}
            onInput={(event) => setInput(event.currentTarget.value)}
            placeholder={t(
              "[asp-lsp] で始まる出力、デバッグログファイル、JSON Linesに対応",
              "Paste [asp-lsp] output, debug log files, or JSON Lines",
            )}
          />
          <div class="actions">
            <button class="primary" onClick={() => analyze(input())}>
              {t("解析", "Analyze")}
            </button>
            <button
              onClick={() => {
                setInput(sample);
                analyze(sample);
              }}
            >
              {t("サンプル", "Sample")}
            </button>
            <button
              onClick={() => {
                setInput("");
                analyze("");
              }}
            >
              {t("クリア", "Clear")}
            </button>
            <span class="muted">
              {t(
                "このページ内で解析・最大200万文字 / 10,000行",
                "Local analysis · up to 2 million characters / 10,000 lines",
              )}
            </span>
            <Hint label={t("ヒント", "Hint")}>
              {t(
                "データはサーバーへ送信せず、ファイルにも保存しません。ログの取得には既存のdebugOutput/debugLogFile設定を使用します。イベント未登録や壊れた行も消さずに表示します。",
                "Data stays in this page without transmission or file storage. Use existing debugOutput/debugLogFile settings to collect logs. Unregistered events and malformed lines remain visible.",
              )}
            </Hint>
          </div>
        </section>
        {parsed().truncated && (
          <p role="alert">
            {t(
              "解析上限を超えた部分は省略しました（1行は最大20,000文字）。ログを分割してください。",
              "Input beyond the analysis limits was omitted (20,000 characters per line). Split the log into smaller parts.",
            )}
          </p>
        )}
        {entries().length > 0 ? (
          <>
            <section class="metrics" aria-label={t("概要", "Summary")}>
              <article>
                <span>{t("表示行", "Visible lines")}</span>
                <strong>{visible().length.toLocaleString(locale())}</strong>
                <small>
                  {t("時間あり", "With timing")} {summary().timed}
                </small>
              </article>
              <article>
                <span>{t("最長の処理・候補", "Longest operation · candidate")}</span>
                <strong>{ms(summary().maximum)}</strong>
                <small title={slow()[0] ? logGroupKey(slow()[0]) : undefined}>
                  {slow()[0] ? logGroupKey(slow()[0]) : "—"}
                </small>
                <Hint label={t("ヒント", "Hint")}>{durationHelp()}</Hint>
              </article>
              <article>
                <span>
                  P95{" "}
                  <Hint label={t("ヒント", "Hint")}>
                    {t(
                      "記録された所要時間を短い順に並べた95パーセンタイルです。異なる種類の工程を含むため、特定APIのSLAを表す値ではありません。",
                      "The 95th percentile of recorded durations. Mixed operation types mean this is not a particular API's SLA.",
                    )}
                  </Hint>
                </span>
                <strong>{ms(summary().p95)}</strong>
                <small>{t("記録された工程の分布", "Recorded operation distribution")}</small>
              </article>
              <article>
                <span>{t("観測区間", "Observed span")}</span>
                <strong>{ms(summary().span)}</strong>
                <small>{t("最初〜最後の記録時刻", "First to last timestamp")}</small>
              </article>
            </section>
            <div class="filters">
              <input
                aria-label={t("ログ検索", "Search logs")}
                placeholder={t("イベント・URI・説明で検索", "Search event, URI, description")}
                value={query()}
                onInput={(event) => {
                  setQuery(event.currentTarget.value);
                  setPage(0);
                }}
              />
              <select
                aria-label={t("状態フィルター", "Status filter")}
                value={status()}
                onChange={(event) => {
                  setStatus(event.currentTarget.value);
                  setPage(0);
                }}
              >
                <option value="all">{t("すべて", "All")}</option>
                <option value="errors">{t("エラー", "Errors")}</option>
                <option value="unknown">{t("説明未登録", "Unregistered")}</option>
              </select>
              {group() && (
                <button onClick={() => setGroup("")}>
                  {t("工程絞り込み解除", "Clear operation filter")}
                </button>
              )}
            </div>
            <nav class="actions" aria-label={t("グラフ画面", "Graph screen")}>
              <button
                aria-pressed={screen() === "tree" ? "true" : "false"}
                class={screen() === "tree" ? "primary" : ""}
                onClick={() => setScreen("tree")}
              >
                {t("処理の流れ", "Processing flow")}
              </button>
              <button
                aria-pressed={screen() === "analysis" ? "true" : "false"}
                class={screen() === "analysis" ? "primary" : ""}
                onClick={() => setScreen("analysis")}
              >
                {t("時間分析", "Timing analysis")}
              </button>
            </nav>
            {screen() === "tree" && (
              <LogExecutionTree entries={visible()} locale={locale()} onSelect={setSelected} />
            )}
            <section class="chart-grid" hidden={screen() !== "analysis"}>
              <article class="panel">
                <div class="panel-title">
                  <h2>{t("時間の長い処理", "Longest operations")}</h2>
                  <Hint label={t("ヒント", "Hint")}>
                    {durationHelp()}
                    {t(
                      " 棒を選ぶと元の行を確認できます。タイムラインは記録時刻を終了時刻とし、所要時間を引いて開始を推定します。ログ出力待ちの遅延は補正しません。",
                      " Select a bar to inspect its line. The timeline estimates a start by subtracting duration from the logged completion timestamp; logging delays are not corrected.",
                    )}
                  </Hint>
                  <select
                    aria-label={t("グラフ表示", "Chart view")}
                    value={view()}
                    onChange={(event) =>
                      setView(event.currentTarget.value as "duration" | "timeline")
                    }
                  >
                    <option value="duration">{t("所要時間", "Duration")}</option>
                    <option value="timeline">{t("推定タイムライン", "Estimated timeline")}</option>
                  </select>
                </div>
                <div class="bars">
                  {(view() === "duration" ? slow().slice(0, 20) : timeline().rows).map((entry) => (
                    <button
                      class="bar-row"
                      onClick={() => setSelected(entry.line)}
                      title={`#${entry.line} ${entry.event} ${ms(entry.duration)}`}
                    >
                      <span class="bar-label">
                        #{entry.line} {entry.event}
                      </span>
                      <span class="bar-track">
                        <span
                          class="bar-fill"
                          style={{
                            width: `${Math.max(0.3, (100 * entry.duration!) / (view() === "duration" ? summary().maximum || 1 : timeline().span))}%`,
                            "margin-left":
                              view() === "timeline"
                                ? `${(100 * (entry.timestamp! - entry.duration! - timeline().start)) / timeline().span}%`
                                : "0",
                          }}
                        />
                      </span>
                      <span>{ms(entry.duration)}</span>
                    </button>
                  ))}
                </div>
                {slow().length === 0 && (
                  <p class="muted">
                    {t("所要時間が記録されていません。", "No durations were recorded.")}
                  </p>
                )}
                {view() === "timeline" && timeline().rows.length === 0 && slow().length > 0 && (
                  <p class="muted">
                    {t(
                      "日時と所要時間の両方が必要です。",
                      "Both timestamps and durations are required.",
                    )}
                  </p>
                )}
              </article>
              <article class="panel">
                <div class="panel-title">
                  <h2>{t("時間帯 × 工程", "Time × operation")}</h2>
                  <Hint label={t("ヒント", "Hint")}>
                    {t(
                      "記録時刻を16区間に分け、工程ごとに所要時間を集計します。濃いセルほど記録時間の合計が大きい区間です。上位12工程を表示します。並列処理・親子工程の時間は重複し、CPU使用率を表す図ではありません。セルの数値を確認し、選択すると工程で絞り込めます。",
                      "Splits logged timestamps into 16 buckets and sums recorded durations per operation. Darker cells have larger sums. Shows the top 12 operations. Parallel and nested time overlaps; this is not CPU utilization. Inspect cell values or select a cell to filter its operation.",
                    )}
                  </Hint>
                </div>
                {heatmap().rows.length ? (
                  <>
                    <div class="heat-legend">
                      <span>{clock(heatmap().start)}</span>
                      <span>{t("淡:短い / 濃:長い", "Light: shorter / dark: longer")}</span>
                      <span>{clock(heatmap().end)}</span>
                    </div>
                    <div class="heat-scroll">
                      {heatmap().rows.map((row) => (
                        <div class="heat-row">
                          <span title={row.key}>{row.key}</span>
                          <div>
                            {row.values.map((value, index) => (
                              <button
                                aria-label={`${row.key} ${index + 1}: ${ms(value)}`}
                                title={`${row.key} ${index + 1}: ${ms(value)}`}
                                style={{
                                  "background-color": `color-mix(in srgb, var(--heat) ${value ? 20 + (80 * value) / (heatmap().maximum || 1) : 0}%, var(--surface))`,
                                }}
                                onClick={() => {
                                  setGroup(row.key);
                                  setPage(0);
                                }}
                              >
                                {value > 0 ? (
                                  <span>{value < 1 ? "<1" : Math.round(value)}</span>
                                ) : (
                                  ""
                                )}
                              </button>
                            ))}
                          </div>
                        </div>
                      ))}
                    </div>
                  </>
                ) : (
                  <p class="muted">
                    {t(
                      "日時と所要時間を含む行を貼り付けてください。",
                      "Paste records containing timestamps and durations.",
                    )}
                  </p>
                )}
              </article>
            </section>
            <section class="panel detail" aria-label={t("選択行の詳細", "Selected line")}>
              {detail() ? (
                <LogDetails entry={detail()!} locale={locale()} />
              ) : (
                <p class="muted">
                  {t(
                    "グラフまたは行を選択すると、原文とフィールドを確認できます。",
                    "Select a chart bar or row to inspect the original record and fields.",
                  )}
                </p>
              )}
            </section>
            <section class="panel">
              <div class="panel-title">
                <h2>{t("行ごとの説明", "Line explanations")}</h2>
                <span>
                  {Math.min(page(), pageCount() - 1) + 1} / {pageCount()}
                </span>
                <button disabled={page() <= 0} onClick={() => setPage(page() - 1)}>
                  {t("前へ", "Previous")}
                </button>
                <button disabled={page() >= pageCount() - 1} onClick={() => setPage(page() + 1)}>
                  {t("次へ", "Next")}
                </button>
              </div>
              <div class="log-rows">
                {rows().map((entry) => (
                  <article class={selected() === entry.line ? "log-row selected" : "log-row"}>
                    <div class="row-main">
                      <button
                        class="line-number row-select"
                        onClick={() => setSelected(entry.line)}
                      >
                        #{entry.line}
                      </button>
                      <span class="row-copy">
                        <span class="row-type">
                          <button class="row-select" onClick={() => setSelected(entry.line)}>
                            <strong>{entry.event}</strong>
                          </button>
                          <Hint label={t("ヒント", "Hint")}>
                            <p>{entry.explanation}</p>
                            <p>{logStatusDescription(entry.status, locale())}</p>
                          </Hint>
                        </span>
                        <button
                          class="row-select row-explanation"
                          onClick={() => setSelected(entry.line)}
                        >
                          {entry.explanation.split(locale() === "ja" ? "。" : ". ")[0]}
                        </button>
                      </span>
                      <button class="row-time row-select" onClick={() => setSelected(entry.line)}>
                        {ms(entry.duration)}
                        <small>{entry.status}</small>
                      </button>
                    </div>
                  </article>
                ))}
              </div>
              {!visible().length && (
                <p>{t("条件に一致する行がありません。", "No matching records.")}</p>
              )}
            </section>
          </>
        ) : (
          <section class="empty">
            <h2>
              {t(
                "処理の流れと、時間がかかる場所を確認",
                "Understand the flow and locate slow operations",
              )}
            </h2>
            <p>
              {t(
                "ログを貼り付けて解析してください。サンプルでも表示を試せます。",
                "Paste a log and select Analyze, or explore the sample.",
              )}
            </p>
          </section>
        )}
      </main>
    </>
  );
}
render(
  () => (
    <WebviewErrorBoundary title="Log analysis">
      <App />
    </WebviewErrorBoundary>
  ),
  document.getElementById("root") ?? document.body,
);
