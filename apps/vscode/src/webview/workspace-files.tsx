import React, { useMemo, useState } from "react";
import { createRoot } from "react-dom/client";
import { VirtualList } from "./virtual-list";
import styles from "./workspace-files.css?inline";
import type {
  WorkspaceFilesFile,
  WorkspaceFilesPayload,
  WorkspaceFilesPreviewRequest,
} from "../workspace-files-webview";

declare const acquireVsCodeApi: () => {
  postMessage(message: unknown): void;
};

declare global {
  interface Window {
    __ASP_LSP_WORKSPACE_FILES__?: WorkspaceFilesPayload;
  }
}

type TreeRow =
  | { id: string; kind: "root"; depth: number; label: string; detail?: string }
  | { id: string; kind: "folder"; depth: number; label: string; detail?: string }
  | {
      id: string;
      kind: "file";
      depth: number;
      label: string;
      detail?: string;
      file: WorkspaceFilesFile;
    };

type Locale = "en" | "ja";
type TextKey =
  | "action.export"
  | "action.preview"
  | "action.refresh"
  | "empty"
  | "excludeGlobs"
  | "fileCount"
  | "files"
  | "filters"
  | "includeGlobs"
  | "lastModified"
  | "mode.excel"
  | "mode.view"
  | "noSelection"
  | "open"
  | "previewFailed"
  | "respectGitIgnore"
  | "search"
  | "selectedFile"
  | "size"
  | "totalSize"
  | "truncated";

const vscode = acquireVsCodeApi();
const initialPayload = window.__ASP_LSP_WORKSPACE_FILES__;

const messages: Record<Locale, Record<TextKey, string>> = {
  en: {
    "action.export": "Export Excel",
    "action.preview": "Preview",
    "action.refresh": "Refresh",
    empty: "No Classic ASP files match the current filters.",
    excludeGlobs: "Exclude globs",
    fileCount: "{count} files",
    files: "Files",
    filters: "Filters",
    includeGlobs: "Include globs",
    lastModified: "Modified",
    "mode.excel": "Excel analysis files",
    "mode.view": "Project glob files",
    noSelection: "Select a file to inspect it.",
    open: "Open",
    previewFailed: "Preview failed: {error}",
    respectGitIgnore: "Respect .gitignore",
    search: "Search files",
    selectedFile: "Selected file",
    size: "Size",
    totalSize: "Total size",
    truncated: "Truncated: {reason}",
  },
  ja: {
    "action.export": "Excel export",
    "action.preview": "Preview",
    "action.refresh": "更新",
    empty: "現在の filter に一致する Classic ASP file はありません。",
    excludeGlobs: "Exclude globs",
    fileCount: "{count} files",
    files: "ファイル",
    filters: "フィルター",
    includeGlobs: "Include globs",
    lastModified: "更新日時",
    "mode.excel": "Excel 解析 file",
    "mode.view": "Project glob file",
    noSelection: "file を選ぶと詳細を表示します。",
    open: "開く",
    previewFailed: "Preview 失敗: {error}",
    respectGitIgnore: ".gitignore を尊重",
    search: "file を検索",
    selectedFile: "選択 file",
    size: "サイズ",
    totalSize: "合計サイズ",
    truncated: "切り詰め: {reason}",
  },
};

function App(): React.ReactElement {
  const [payload, setPayload] = useState<WorkspaceFilesPayload>(initialPayload ?? emptyPayload());
  const locale: Locale = payload.locale === "ja" ? "ja" : "en";
  const [includeGlobs, setIncludeGlobs] = useState(payload.includeGlobs.join("\n"));
  const [excludeGlobs, setExcludeGlobs] = useState(payload.excludeGlobs.join("\n"));
  const [respectGitIgnore, setRespectGitIgnore] = useState(payload.respectGitIgnore);
  const [search, setSearch] = useState("");
  const [selectedUri, setSelectedUri] = useState<string | undefined>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | undefined>();
  const allFiles = useMemo(() => payload.roots.flatMap((root) => root.files), [payload]);
  const rows = useMemo(() => treeRows(payload, search), [payload, search]);
  const selectedFile =
    allFiles.find((file) => file.uri === selectedUri) ?? allFiles[0] ?? undefined;
  const fileUris = allFiles.map((file) => file.uri);
  const request = (): WorkspaceFilesPreviewRequest => ({
    includeGlobs: globLines(includeGlobs),
    excludeGlobs: globLines(excludeGlobs),
    respectGitIgnore,
  });
  const text = (key: TextKey, params: Record<string, string | number> = {}): string => {
    let message = messages[locale][key] ?? messages.en[key];
    for (const [name, value] of Object.entries(params)) {
      message = message.replaceAll(`{${name}}`, String(value));
    }
    return message;
  };

  function preview(): void {
    const requestId = `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
    setBusy(true);
    setError(undefined);
    const listener = (event: MessageEvent): void => {
      const message = event.data as {
        type?: string;
        requestId?: string;
        payload?: WorkspaceFilesPayload;
        error?: string;
      };
      if (message.type !== "previewResult" || message.requestId !== requestId) {
        return;
      }
      window.removeEventListener("message", listener);
      setBusy(false);
      if (message.payload) {
        setPayload(message.payload);
        setSelectedUri(undefined);
      } else {
        setError(text("previewFailed", { error: message.error ?? "unknown" }));
      }
    };
    window.addEventListener("message", listener);
    vscode.postMessage({ type: "preview", requestId, ...request() });
  }

  function exportExcel(): void {
    if (payload.mode !== "excel" || fileUris.length === 0) {
      return;
    }
    setBusy(true);
    const listener = (event: MessageEvent): void => {
      const message = event.data as { type?: string; ok?: boolean; error?: string };
      if (message.type !== "exportResult") {
        return;
      }
      window.removeEventListener("message", listener);
      setBusy(false);
      if (message.ok === false) {
        setError(message.error ?? "Export failed.");
      }
    };
    window.addEventListener("message", listener);
    vscode.postMessage({ type: "exportExcel", fileUris, ...request() });
  }

  return (
    <div className="workspace-files-app">
      <style>{styles}</style>
      <header className="workspace-files-toolbar">
        <div>
          <h1>{text(payload.mode === "excel" ? "mode.excel" : "mode.view")}</h1>
          <p>
            {text("fileCount", { count: payload.stats.files })} · {text("totalSize")}{" "}
            {formatBytes(payload.stats.totalBytes)}
          </p>
        </div>
        <div className="toolbar-actions">
          <input
            aria-label={text("search")}
            className="search-input"
            placeholder={text("search")}
            value={search}
            onChange={(event) => setSearch(event.currentTarget.value)}
          />
          <button type="button" onClick={preview} disabled={busy}>
            {text("action.refresh")}
          </button>
          {payload.mode === "excel" ? (
            <button
              type="button"
              className="primary-button"
              onClick={exportExcel}
              disabled={busy || fileUris.length === 0}
            >
              {text("action.export")}
            </button>
          ) : null}
        </div>
      </header>
      {payload.truncated ? (
        <div className="notice">{text("truncated", { reason: payload.truncated.reason })}</div>
      ) : null}
      {error ? <div className="notice danger">{error}</div> : null}
      <main className="workspace-files-main">
        <section className="tree-pane" aria-label={text("files")}>
          {rows.length === 0 ? (
            <div className="empty-state">{text("empty")}</div>
          ) : (
            <VirtualList
              className="tree-list"
              estimateSize={34}
              gap={0}
              getKey={(row) => row.id}
              items={rows}
              maxHeight={720}
              renderItem={(row) => (
                <TreeRowView
                  row={row}
                  selected={row.kind === "file" && row.file.uri === selectedFile?.uri}
                  onSelect={() => {
                    if (row.kind === "file") {
                      setSelectedUri(row.file.uri);
                    }
                  }}
                />
              )}
              threshold={80}
            />
          )}
        </section>
        <aside className="side-pane">
          <section className="panel-section">
            <h2>{text("filters")}</h2>
            <label>
              <span>{text("includeGlobs")}</span>
              <textarea
                value={includeGlobs}
                onChange={(event) => setIncludeGlobs(event.currentTarget.value)}
                spellCheck={false}
              />
            </label>
            <label>
              <span>{text("excludeGlobs")}</span>
              <textarea
                value={excludeGlobs}
                onChange={(event) => setExcludeGlobs(event.currentTarget.value)}
                spellCheck={false}
              />
            </label>
            <label className="checkbox-row">
              <input
                type="checkbox"
                checked={respectGitIgnore}
                onChange={(event) => setRespectGitIgnore(event.currentTarget.checked)}
              />
              <span>{text("respectGitIgnore")}</span>
            </label>
            <button type="button" onClick={preview} disabled={busy}>
              {text("action.preview")}
            </button>
          </section>
          <section className="panel-section">
            <h2>{text("selectedFile")}</h2>
            {selectedFile ? (
              <dl className="details-list">
                <dt>{text("files")}</dt>
                <dd>{selectedFile.displayPath ?? selectedFile.relativePath}</dd>
                <dt>{text("size")}</dt>
                <dd>{formatBytes(selectedFile.size)}</dd>
                <dt>{text("lastModified")}</dt>
                <dd>{new Date(selectedFile.mtimeMs).toLocaleString(locale)}</dd>
              </dl>
            ) : (
              <p className="muted">{text("noSelection")}</p>
            )}
            {selectedFile ? (
              <button
                type="button"
                onClick={() => vscode.postMessage({ type: "openFile", uri: selectedFile.uri })}
              >
                {text("open")}
              </button>
            ) : null}
          </section>
        </aside>
      </main>
    </div>
  );
}

function TreeRowView({
  row,
  selected,
  onSelect,
}: {
  row: TreeRow;
  selected: boolean;
  onSelect(): void;
}): React.ReactElement {
  const className = `tree-row ${row.kind}${selected ? " selected" : ""}`;
  return (
    <button
      type="button"
      className={className}
      style={{ paddingLeft: `${12 + row.depth * 18}px` }}
      onClick={onSelect}
      title={row.detail}
    >
      <span className="tree-icon" aria-hidden="true">
        {row.kind === "file" ? "ASP" : row.kind === "folder" ? "/" : "WS"}
      </span>
      <span className="tree-label">{row.label}</span>
      {row.detail ? <span className="tree-detail">{row.detail}</span> : null}
    </button>
  );
}

function treeRows(payload: WorkspaceFilesPayload, search: string): TreeRow[] {
  const query = search.trim().toLowerCase();
  const rows: TreeRow[] = [];
  for (const root of payload.roots) {
    const files = root.files.filter(
      (file) =>
        !query ||
        file.relativePath.toLowerCase().includes(query) ||
        file.displayPath?.toLowerCase().includes(query),
    );
    if (files.length === 0) {
      continue;
    }
    rows.push({
      id: `root:${root.uri}`,
      kind: "root",
      depth: 0,
      label: root.displayPath ?? root.name,
      detail: `${files.length}`,
    });
    const folderIds = new Set<string>();
    for (const file of files.sort((left, right) =>
      left.relativePath.localeCompare(right.relativePath),
    )) {
      const parts = file.relativePath.split("/");
      for (let index = 0; index < parts.length - 1; index += 1) {
        const folderPath = parts.slice(0, index + 1).join("/");
        const id = `folder:${root.uri}:${folderPath}`;
        if (!folderIds.has(id)) {
          folderIds.add(id);
          rows.push({
            id,
            kind: "folder",
            depth: index + 1,
            label: parts[index],
            detail: folderPath,
          });
        }
      }
      rows.push({
        id: `file:${file.uri}`,
        kind: "file",
        depth: parts.length,
        label: parts.at(-1) ?? file.relativePath,
        detail: file.relativePath,
        file,
      });
    }
  }
  return rows;
}

function globLines(value: string): string[] {
  return value
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter((line) => line.length > 0);
}

function formatBytes(value: number): string {
  if (value < 1024) {
    return `${value} B`;
  }
  const units = ["KB", "MB", "GB"];
  let amount = value / 1024;
  for (const unit of units) {
    if (amount < 1024 || unit === units.at(-1)) {
      return `${amount.toFixed(amount >= 100 ? 0 : amount >= 10 ? 1 : 2)} ${unit}`;
    }
    amount /= 1024;
  }
  return `${value} B`;
}

function emptyPayload(): WorkspaceFilesPayload {
  return {
    mode: "view",
    includeGlobs: ["**/*.{asp,asa,inc}"],
    excludeGlobs: [],
    respectGitIgnore: false,
    roots: [],
    stats: { files: 0, totalBytes: 0 },
  };
}

createRoot(document.getElementById("root") ?? document.body).render(<App />);
