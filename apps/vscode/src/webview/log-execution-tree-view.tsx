import { createMemo, createSignal } from "solid-js";
import type { JSX } from "@solidjs/web";
import type { LogEntry, LogLocale } from "./log-analysis-model";
import { buildLogExecutionTree } from "./log-execution-tree";
import { LogHint } from "./log-hint";
import { LogDetails } from "./log-details";

/** Render lifecycle events in time order with explicit and inferred relationships. */
export function LogExecutionTree(props: {
  entries: LogEntry[];
  locale: LogLocale;
  onSelect: (line: number) => void;
}): JSX.Element {
  const graph = createMemo(() => buildLogExecutionTree(props.entries));
  const t = (ja: string, en: string) => (props.locale === "ja" ? ja : en);
  const [expanded, setExpanded] = createSignal(false);
  const [detail, setDetail] = createSignal<LogEntry>();
  let dialog!: HTMLDialogElement;
  let expandButton!: HTMLButtonElement;
  const openDetails = (entry: LogEntry) => {
    setDetail(entry);
    props.onSelect(entry.line);
    dialog.showModal();
    const content = dialog.querySelector(".log-details");
    if (content) {
      content.scrollTop = 0;
      content.scrollLeft = 0;
    }
  };
  const help = (entry: LogEntry) => (
    <button class="node-details-button" onClick={() => openDetails(entry)}>
      {t("詳細", "Details")}
    </button>
  );
  const node = (entry: LogEntry, phase: string, x: number, y: number, note: string) => (
    <article
      class="execution-node"
      onDblClick={() => openDetails(entry)}
      style={{ left: `${x}px`, top: `${y}px` }}
    >
      <div class="execution-node-heading">
        <strong>{phase}</strong>
        {help(entry)}
      </div>
      <button class="execution-select" onClick={() => props.onSelect(entry.line)} title={entry.raw}>
        {entry.event}
        <br />
        {String(entry.fields.method ?? entry.fields.uri ?? "")}
      </button>
      <small>
        {note || (entry.duration === undefined ? `#${entry.line}` : `${entry.duration} ms`)}
      </small>
    </article>
  );
  return (
    <section
      class={expanded() ? "panel execution-panel execution-expanded" : "panel execution-panel"}
      onKeyDown={(event) => {
        if (event.key === "Escape" && !dialog.open && expanded()) {
          setExpanded(false);
          expandButton.focus();
        }
      }}
    >
      <div class="execution-toolbar">
        <button
          ref={(element) => {
            expandButton = element;
          }}
          class="expand-graph"
          aria-pressed={expanded() ? "true" : "false"}
          onClick={() => setExpanded(!expanded())}
        >
          {expanded() ? t("拡大を終了", "Exit expanded view") : t("グラフを拡大", "Expand graph")}
        </button>
      </div>
      <dialog
        class="log-detail-dialog"
        ref={(element) => {
          dialog = element;
        }}
        aria-label={t("処理の詳細", "Operation details")}
      >
        <form method="dialog">
          <strong>{t("処理の詳細", "Operation details")}</strong>
          <button>{t("閉じる", "Close")}</button>
        </form>
        {detail() && <LogDetails entry={detail()!} locale={props.locale} />}
      </dialog>
      <h2>{t("処理の流れ · 時系列イベントDAG", "Processing flow · chronological event DAG")}</h2>
      <LogHint label={t("ヒント", "Hint")}>
        <p>
          {t(
            "時間は上から下へ進みます。間隔は見やすさのため均等で、経過時間に比例しません。青い縦線は同じ処理の開始と終了、紫の実線はIDで明示された親子関係です。点線は所要時間から推定した開始、または同じURIと時間範囲から推定した親子関係です。並行する処理は横に配置します。時刻のない記録は前後のログに沿って配置します。配置位置から時刻や所要時間は算出しません。関係が曖昧な場合は接続しません。",
            "Time advances downward. Spacing is uniform for readability, not proportional to elapsed time. Blue vertical lines connect an operation's start and finish; solid purple lines connect parents and children explicitly correlated by ID. Dashed lines indicate estimated starts or parent relationships inferred from the same URI and time containment. Concurrent operations occupy separate columns. Untimed records follow neighboring logs without assigning measured timestamps or durations; ambiguous relationships remain disconnected.",
          )}
        </p>
      </LogHint>
      <div class="execution-viewport" tabindex={0} aria-label={t("イベントDAG", "Event DAG")}>
        <div
          class="execution-canvas"
          style={{
            width: `${Math.max(360, 100 + graph().lanes * 260)}px`,
            height: `${graph().height}px`,
          }}
        >
          <svg class="execution-edges" width="100%" height="100%" aria-hidden="true">
            {graph().spans.map((span) => (
              <>
                {span.bottom > span.top && (
                  <path
                    class="execution-life"
                    stroke-dasharray={span.estimated ? "6 4" : undefined}
                    d={`M ${210 + span.lane * 260} ${span.top + 90} V ${span.bottom}`}
                  />
                )}
                {span.parent !== undefined &&
                  (() => {
                    const parent = graph().spans.find((item) => item.id === span.parent);
                    return parent ? (
                      <path
                        class="execution-parent"
                        stroke-dasharray={span.inferredParent ? "6 4" : undefined}
                        d={`M ${330 + parent.lane * 260} ${parent.top + 45} H ${350 + parent.lane * 260} V ${span.top + 12} H ${90 + span.lane * 260}`}
                      />
                    ) : null;
                  })()}
              </>
            ))}
          </svg>
          {graph().ticks.map(({ time, top }) => (
            <time
              class="execution-time"
              style={{ top: `${top}px` }}
              title={new Date(time).toISOString()}
            >
              +{(time - graph().ticks[0].time).toFixed(1)} ms
            </time>
          ))}
          {graph().spans.map((span) => (
            <>
              {node(
                span.start ?? span.end!,
                span.start
                  ? t("開始", "Start")
                  : span.estimated && !span.orderedWithoutTime
                    ? t("開始（推定）", "Start (estimated)")
                    : t("イベント", "Event"),
                100 + span.lane * 260,
                span.top,
                span.orderedWithoutTime
                  ? t("時刻なし・前後のログ順", "Untimed · neighboring log order")
                  : span.ambiguous
                    ? t("対応不明", "Ambiguous correlation")
                    : span.parent !== undefined
                      ? `${t("親", "Parent")} #${span.parent}${span.inferredParent ? t("（推定）", " (inferred)") : ""}`
                      : span.finish === undefined
                        ? t("終了未記録", "Finish not recorded")
                        : "",
              )}
              {span.end &&
                span.bottom > span.top &&
                node(span.end, t("終了", "Finish"), 100 + span.lane * 260, span.bottom, "")}
            </>
          ))}
        </div>
      </div>
      {graph().omitted > 0 && (
        <p>
          {t(
            "表示上限100処理。検索で対象を絞り込んでください。省略:",
            "Showing up to 100 operations. Narrow the search. Omitted:",
          )}{" "}
          {graph().omitted}
        </p>
      )}
    </section>
  );
}
