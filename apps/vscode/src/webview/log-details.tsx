import type { JSX } from "@solidjs/web";
import type { LogEntry, LogLocale } from "./log-analysis-model";
import { logFieldDescription, logStatusDescription } from "./log-event-catalog";

/** Shared structured explanation for selected rows and event dialogs. */
export function LogDetails(props: { entry: LogEntry; locale: LogLocale }): JSX.Element {
  const t = (ja: string, en: string) => (props.locale === "ja" ? ja : en);
  const valueText = (value: unknown) =>
    typeof value === "object" ? JSON.stringify(value, null, 2) : String(value);
  return (
    <div class="log-details">
      <header class="log-details-heading">
        <span class="detail-eyebrow">
          {t("処理の詳細", "OPERATION DETAILS")} · #{props.entry.line}
        </span>
        <h3>{props.entry.event}</h3>
        <p>{props.entry.explanation}</p>
      </header>
      <div class="detail-facts">
        <article>
          <span>{t("所要時間", "Elapsed time")}</span>
          <strong>
            {props.entry.duration === undefined
              ? t("記録なし", "Not recorded")
              : `${props.entry.duration.toLocaleString(props.locale, { maximumFractionDigits: 3 })} ms`}
          </strong>
          <small>{t("経過時間（CPU時間ではありません）", "Elapsed time, not CPU time")}</small>
        </article>
        <article>
          <span>{t("記録時刻", "Recorded time")}</span>
          <strong>
            {props.entry.timestamp === undefined
              ? t("時刻なし", "Untimed")
              : new Date(props.entry.timestamp).toLocaleTimeString(props.locale, {
                  hour: "2-digit",
                  minute: "2-digit",
                  second: "2-digit",
                  fractionalSecondDigits: 3,
                })}
          </strong>
          <small>
            {props.entry.timestamp === undefined
              ? t(
                  "グラフ位置は前後のログを参考に配置",
                  "Graph position follows neighboring records",
                )
              : new Date(props.entry.timestamp).toISOString()}
          </small>
        </article>
        <article>
          <span>{t("状態", "Status")}</span>
          <strong>{props.entry.status || "—"}</strong>
          <small>{logStatusDescription(props.entry.status, props.locale)}</small>
        </article>
      </div>
      <section class="detail-section">
        <h4>{t("記録された項目", "Recorded fields")}</h4>
        <div class="detail-field-grid">
          {Object.entries(props.entry.fields).map(([key, value]) => (
            <article class="detail-field">
              <strong>{key}</strong>
              <pre>{valueText(value)}</pre>
              <p>{logFieldDescription(key, props.locale)}</p>
            </article>
          ))}
        </div>
        {!Object.keys(props.entry.fields).length && (
          <p>{t("追加項目はありません。", "No additional fields were recorded.")}</p>
        )}
      </section>
      <section class="detail-section">
        <h4>{t("元のログ", "Original log")}</h4>
        <pre class="detail-raw">{props.entry.raw}</pre>
      </section>
    </div>
  );
}
