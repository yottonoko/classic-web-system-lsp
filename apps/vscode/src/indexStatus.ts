export interface IndexProgressParams {
  state: "begin" | "progress" | "end";
  done?: number;
  total?: number;
}

export interface IndexStatusDisplay {
  visible: boolean;
  text: string;
  tooltip: string;
}

export function formatIndexProgressStatus(
  payload: IndexProgressParams,
  language: string,
): IndexStatusDisplay {
  if (payload.state === "end") {
    return { visible: false, text: "", tooltip: "" };
  }

  const isJa = language.toLowerCase().startsWith("ja");
  const total =
    typeof payload.total === "number" && Number.isFinite(payload.total) && payload.total > 0
      ? Math.floor(payload.total)
      : 0;
  const done =
    typeof payload.done === "number" && Number.isFinite(payload.done) && payload.done > 0
      ? Math.min(Math.floor(payload.done), total)
      : 0;

  let text: string;
  if (total > 0) {
    const pct = Math.floor((done * 100) / total);
    text = isJa
      ? `$(sync~spin) Classic ASP: インデックス構築中 (${done}/${total}, ${pct}%)`
      : `$(sync~spin) Classic ASP: Indexing (${done}/${total}, ${pct}%)`;
  } else {
    text = isJa
      ? "$(sync~spin) Classic ASP: インデックス構築中..."
      : "$(sync~spin) Classic ASP: Indexing...";
  }

  const tooltip = isJa
    ? "Classic ASP ワークスペースのクロスファイル参照インデックスを構築しています"
    : "Building Classic ASP workspace cross-file reference index";

  return { visible: true, text, tooltip };
}
