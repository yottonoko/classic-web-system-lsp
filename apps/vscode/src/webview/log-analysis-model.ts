import { describeCatalogEvent } from "./log-event-catalog";
export type LogLocale = "ja" | "en";
export interface LogEntry {
  line: number;
  raw: string;
  event: string;
  fields: Record<string, unknown>;
  timestamp?: number;
  duration?: number;
  durationField?: string;
  status: string;
  explanation: string;
  known: boolean;
}
export const logInputLimit = 2_000_000;
const checks: Record<string, [string, string]> = {
  prepare: [
    "各検査が共有する構文診断、include文書、外部グローバル名を準備します。キャッシュ復元や関連文書の解析を含む場合があります。",
    "Prepares shared syntax diagnostics, included documents and external global names before individual checks. May include cache restoration or related-document analysis.",
  ],
  parser: [
    "ASP領域や構文の解析結果から、構文上の問題を取り出します。",
    "Collects syntax problems from the ASP parser and region analysis.",
  ],
  declarations: [
    "変数や手続きの宣言を検査し、未対応の宣言構文などを診断します。",
    "Checks declarations, including unsupported declaration syntax.",
  ],
  calls: ["呼び出し構文や引数の使い方を検査します。", "Checks call syntax and argument usage."],
  "vbscript.syntax": [
    "VBScriptの文やブロックの対応など、構文の整合性を検査します。",
    "Checks VBScript statements and block syntax.",
  ],
  "vbscript.deadCode": [
    "実行されないコードや到達できない処理を検査します。",
    "Checks unreachable or dead VBScript code.",
  ],
  "vbscript.unused": [
    "設定に応じて未使用の変数・引数と、Option Explicitがない文書の暗黙グローバル変数を検査します。",
    "Checks unused variables and parameters, and optionally implicit globals in documents without Option Explicit.",
  ],
  "vbscript.naming": [
    "設定された命名規則と宣言名を照合します。",
    "Checks declaration names against configured naming conventions.",
  ],
  includes: [
    "include参照の解決と、参照先に関する問題を検査します。",
    "Resolves include references and checks include-related problems.",
  ],
  "vbscript.types": [
    "型注釈や推論した型を使って、代入・引数などの型の整合性を検査します。",
    "Checks type consistency using annotations and inferred types.",
  ],
  html: [
    "ASPに埋め込まれたHTMLをHTML言語サービスで検査します。",
    "Checks embedded HTML using the HTML language service.",
  ],
  css: [
    "埋め込みCSSをCSS言語サービスで検査します。",
    "Checks embedded CSS using the CSS language service.",
  ],
  javascript: [
    "埋め込みJavaScriptの構文や意味上の問題を検査します。",
    "Checks embedded JavaScript syntax and semantic issues.",
  ],
  diagnostics: [
    "診断処理全体を実行します。配下のcheck処理と時間が重なるため、単純に合計すると二重計上になります。",
    "Runs the diagnostic pipeline. Its time overlaps child checks and must not be added to them as wall time.",
  ],
};
const methods: Record<string, [string, string]> = {
  "textDocument/hover": [
    "カーソル位置の型や説明を返すホバー要求",
    "a hover request for type information and documentation",
  ],
  "textDocument/completion": [
    "入力位置に応じた補完候補の要求",
    "a completion request at the cursor",
  ],
  "textDocument/inlayHint": ["変数型や引数などのインレイヒントの要求", "an inlay hint request"],
  "textDocument/didOpen": [
    "文書を開いたことと本文を伝える通知",
    "a document-open notification containing the text",
  ],
  "textDocument/didChange": [
    "文書の編集内容とバージョンを伝える通知",
    "a document-change notification containing edits and a version",
  ],
  "textDocument/definition": [
    "参照している名前の定義位置を調べる要求",
    "a definition lookup request",
  ],
  "textDocument/references": ["名前が参照されている場所を調べる要求", "a references request"],
  "textDocument/formatting": ["文書の整形結果を求める要求", "a document formatting request"],
  "workspace/executeCommand": [
    "LSPサーバーのコマンドを実行する要求",
    "a server command execution request",
  ],
  "$/cancelRequest": ["実行中の要求の中断を依頼する通知", "a cancellation notification"],
};
const choose = (pair: [string, string], locale: LogLocale) => pair[locale === "ja" ? 0 : 1];
function explain(
  event: string,
  fields: Record<string, unknown>,
  locale: LogLocale,
): [string, boolean] {
  const base = event.replace(/\.(started|completed|cancelled|failed|stale|reuse|hit|miss)$/, "");
  if (base.startsWith("check.") && checks[base.slice(6)])
    return [choose(checks[base.slice(6)], locale), true];
  if (/^lsp\.(request|notification|response)\.(received|completed)$/.test(event)) {
    const method = valueText(fields.method ?? "");
    const description = methods[method]
      ? choose(methods[method], locale)
      : method || (locale === "ja" ? "種類が記録されていない通信" : "an unspecified message");
    const response = event.includes(".response.");
    const identityHelp = event.includes(".notification.")
      ? locale === "ja"
        ? "通知には応答用のrequestIdはありません。"
        : "Notifications do not have a response requestId."
      : locale === "ja"
        ? "requestIdは対応する要求の識別子です。"
        : "requestId identifies the request.";
    return [
      locale === "ja"
        ? `${response ? "クライアントからの応答" : description}を${event.endsWith("received") ? "受信した記録" : "処理し終えた記録"}です。${identityHelp}${response ? "roundTripMsは要求送信から応答まで、deliveryMsは応答受信後の受け渡し時間です。" : "durationMsはこの処理にかかった経過時間で、CPU使用時間そのものではありません。"}`
        : `${event.endsWith("received") ? "Received" : "Finished processing"} ${response ? "a client response" : description}. ${identityHelp} ${response ? "roundTripMs measures the request round trip; deliveryMs measures response delivery." : "durationMs is elapsed processing time, not CPU time."}`,
      true,
    ];
  }
  if (base === "analysis.parse")
    return [
      choose(
        [
          "文書を解析してASP領域や構文木を作ります。countは検出した領域数です。",
          "Parses the document into ASP regions and syntax structures. count is the region count.",
        ],
        locale,
      ),
      true,
    ];
  if (base === "analysisDatabase.diagnostics")
    return [
      choose(
        [
          "保存済みの診断結果を検索します。hitは再利用、missは利用できる結果がなく再解析に進むことを表します。",
          "Looks up saved diagnostics. hit reuses a result; miss proceeds to analysis.",
        ],
        locale,
      ),
      true,
    ];
  const catalog = describeCatalogEvent(event, locale);
  if (catalog) return [catalog, true];
  return [
    choose(
      [
        "このイベント専用の説明は未登録です。処理内容は推測せず、原文と記録されたフィールドを表示しています。",
        "No description is registered for this event. Inspect the original line and fields; its behavior is not inferred.",
      ],
      locale,
    ),
    false,
  ];
}
function object(value: unknown): Record<string, unknown> | undefined {
  return value !== null && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : undefined;
}
function valueText(value: unknown): string {
  return typeof value === "string" ? value : (JSON.stringify(value) ?? "");
}
function trailingMetadata(raw: string): Record<string, unknown> | undefined {
  let start = -1,
    depth = 0,
    quoted = false,
    escaped = false,
    lastStart = -1,
    lastEnd = -1;
  for (let index = 0; index < raw.length; index++) {
    const char = raw[index];
    if (depth === 0) {
      if (char === "{" && (raw[index + 1] === '"' || raw[index + 1] === "}")) {
        start = index;
        depth = 1;
      }
      continue;
    }
    if (escaped) {
      escaped = false;
      continue;
    }
    if (quoted && char === "\\") {
      escaped = true;
      continue;
    }
    if (char === '"') {
      quoted = !quoted;
      continue;
    }
    if (quoted) continue;
    if (char === "{") depth++;
    if (char === "}" && --depth === 0) {
      lastStart = start;
      lastEnd = index + 1;
    }
  }
  if (lastStart >= 0 && !raw.slice(lastEnd).trim()) {
    try {
      return object(JSON.parse(raw.slice(lastStart, lastEnd)));
    } catch {
      /* Keep incomplete records readable. */
    }
  }
  return undefined;
}
function numeric(value: unknown): number | undefined {
  if (typeof value !== "number" && (typeof value !== "string" || !value.trim())) return undefined;
  const result = Number(value);
  return Number.isFinite(result) && result >= 0 && result <= Number.MAX_SAFE_INTEGER
    ? result
    : undefined;
}
/** Parse the server's text/metadata logs and JSON lines without evaluating log content. */
export function parseDebugLog(
  input: string,
  locale: LogLocale,
): { entries: LogEntry[]; truncated: boolean } {
  const lines = input.slice(0, logInputLimit).split(/\r?\n/);
  let truncated = input.length > logInputLimit || lines.length > 10000;
  const entries: LogEntry[] = [];
  for (let index = 0; index < Math.min(lines.length, 10000); index++) {
    if (!lines[index].trim()) continue;
    if (lines[index].length > 20000) truncated = true;
    const raw = lines[index].slice(0, 20000);
    let text = raw;
    let fields: Record<string, unknown> = Object.create(null);
    let record: Record<string, unknown> | undefined;
    try {
      record = object(JSON.parse(raw));
    } catch {
      /* Plain-text logs are the normal server format. */
    }
    if (record) {
      fields = { ...record, ...object(record.metadata) };
      text = valueText(record.message ?? record.event ?? record.category ?? "");
    }
    for (const match of text.matchAll(/([\w.]+)=("(?:\\.|[^"\\])*"|[^\s]+)/g)) {
      const token = match[2].startsWith('"') ? match[2] : match[2].replace(/,$/, "");
      let value: unknown = token;
      try {
        value = JSON.parse(token);
      } catch {
        /* Keep non-JSON field values verbatim. */
      }
      fields[match[1]] = value;
    }
    // Metadata is appended as one JSON object after the readable message.
    fields = { ...fields, ...(record ? object(record.metadata) : trailingMetadata(raw)) };
    if (!fields.uri) {
      const uri = text.match(/:\s+((?:file|vscode-remote):\/\/[^\s,]+)/)?.[1];
      if (uri) fields.uri = uri;
    }
    const scope = text.match(/Formatting conversion (?:started|completed) \(([^)]+)\)/)?.[1];
    if (scope) fields.scope = scope;
    const messageEvent = text.match(/\[asp-lsp\]\s+([\w]+(?:\.[\w]+)+)/)?.[1];
    const legacy = text.match(
      /\[asp-lsp\] (LSP analysis|LSP check|Formatting conversion) (started|completed)/,
    );
    const legacyEvent = legacy
      ? `${legacy[1] === "LSP analysis" ? "analysis.total" : legacy[1] === "LSP check" ? "check.total" : "format.conversion"}.${legacy[2]}`
      : undefined;
    const event =
      legacyEvent ??
      messageEvent ??
      valueText(
        fields.event ??
          fields.step ??
          fields.category ??
          text.match(/\b(?:lsp|check|analysis|database|analysisDatabase)\.[\w.]+/)?.[0] ??
          "unknown",
      );
    const timestampText =
      fields.timestamp ??
      fields.time ??
      raw.match(/\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})/)?.[0];
    const at = typeof timestampText === "string" ? Date.parse(timestampText) : NaN;
    let duration: number | undefined;
    let durationField: string | undefined;
    for (const key of ["durationMs", "roundTripMs", "deliveryMs"]) {
      const value = numeric(fields[key]);
      if (value !== undefined) {
        duration = value;
        durationField = key;
        break;
      }
    }
    if (duration === undefined) {
      const elapsed = text.match(/(?:^|\s)(\d+(?:\.\d+)?)\s*(ns|µs|us|ms|s)(?=[\s,)]|$)/);
      if (elapsed) {
        duration = numeric(
          Number(elapsed[1]) *
            ({ ns: 0.000001, µs: 0.001, us: 0.001, ms: 1, s: 1000 }[elapsed[2]] ?? 1),
        );
        durationField = "elapsed";
      }
    }
    const [explanation, known] = explain(event, fields, locale);
    const suffix = event.split(".").at(-1) ?? "";
    entries.push({
      line: index + 1,
      raw,
      event,
      fields,
      timestamp: Number.isFinite(at) ? at : undefined,
      duration,
      durationField,
      status: valueText(
        fields.status ??
          fields.state ??
          ([
            "started",
            "received",
            "completed",
            "complete",
            "cancelled",
            "failed",
            "error",
            "stale",
            "reuse",
            "hit",
            "miss",
          ].includes(suffix)
            ? suffix
            : duration !== undefined
              ? "completed"
              : "unknown"),
      ),
      explanation,
      known,
    });
  }
  return { entries, truncated };
}
export function logTimingSummary(entries: LogEntry[]) {
  const durations = entries
    .flatMap((entry) => (entry.duration === undefined ? [] : [entry.duration]))
    .sort((a, b) => a - b);
  const timestamps = entries.flatMap((entry) =>
    entry.timestamp === undefined ? [] : [entry.timestamp],
  );
  return {
    timed: durations.length,
    p95: durations.length ? durations[Math.ceil(durations.length * 0.95) - 1] : undefined,
    maximum: durations.at(-1),
    span: timestamps.length > 1 ? Math.max(...timestamps) - Math.min(...timestamps) : undefined,
  };
}
export function logGroupKey(entry: LogEntry): string {
  return (
    entry.event.replace(/\.(started|received|completed|complete|cancelled|failed)$/, "") +
    (entry.fields.method ? ` · ${valueText(entry.fields.method)}` : "")
  );
}
/** Aggregate recorded durations, not CPU time or a deduplicated critical path. */
export function logHeatmap(entries: LogEntry[], bins = 16) {
  const timed = entries.filter(
    (entry) => entry.duration !== undefined && entry.timestamp !== undefined,
  );
  const start = timed.length ? Math.min(...timed.map((entry) => entry.timestamp!)) : 0;
  const end = timed.length ? Math.max(...timed.map((entry) => entry.timestamp!)) : 0;
  const width = Math.max(1, end - start);
  const groups = new Map<string, number[]>();
  for (const entry of timed) {
    const key = logGroupKey(entry);
    const values = groups.get(key) ?? Array<number>(bins).fill(0);
    const bin = Math.min(bins - 1, Math.floor(((entry.timestamp! - start) / width) * bins));
    values[bin] += entry.duration!;
    groups.set(key, values);
  }
  const rows = [...groups]
    .map(([key, values]) => ({ key, values, total: values.reduce((a, b) => a + b, 0) }))
    .sort((a, b) => b.total - a.total)
    .slice(0, 12);
  return { start, end, rows, maximum: Math.max(0, ...rows.flatMap((row) => row.values)) };
}
