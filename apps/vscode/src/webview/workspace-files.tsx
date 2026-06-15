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

type Summary = {
  aspFiles: number;
  asaFiles: number;
  folders: number;
  incFiles: number;
  latestModifiedMs: number;
};

type Locale = "en" | "ja";
type TextKey =
  | "action.export"
  | "action.preview"
  | "action.refresh"
  | "analysisOverview"
  | "currentFilters"
  | "empty"
  | "excludeGlobs"
  | "fileCount"
  | "files"
  | "filters"
  | "folder"
  | "foldersScanned"
  | "fullPath"
  | "includeGlobs"
  | "lastModified"
  | "lastScanned"
  | "mode.excel"
  | "mode.view"
  | "name"
  | "noSelection"
  | "none"
  | "open"
  | "previewFailed"
  | "projectRoot"
  | "relativePath"
  | "respectGitIgnore"
  | "search"
  | "selectedFile"
  | "size"
  | "totalSize"
  | "truncated"
  | "type"
  | "workspace";

const vscode = acquireVsCodeApi();
const initialPayload = window.__ASP_LSP_WORKSPACE_FILES__;

const messages: Record<Locale, Record<TextKey, string>> = {
  en: {
    "action.export": "Export Excel",
    "action.preview": "Preview",
    "action.refresh": "Refresh",
    analysisOverview: "Analysis overview",
    currentFilters: "Current filters",
    empty: "No Classic ASP files match the current filters.",
    excludeGlobs: "Exclude globs",
    fileCount: "{count} files",
    files: "Files",
    filters: "Filters",
    folder: "Folder",
    foldersScanned: "Folders scanned",
    fullPath: "Full path",
    includeGlobs: "Include globs",
    lastModified: "Modified",
    lastScanned: "Last scanned",
    "mode.excel": "Analysis files",
    "mode.view": "Project glob files",
    name: "Name",
    noSelection: "Select a file to inspect it.",
    none: "None",
    open: "Open",
    previewFailed: "Preview failed: {error}",
    projectRoot: "Project root",
    relativePath: "Relative path",
    respectGitIgnore: "Respect .gitignore",
    search: "Search files",
    selectedFile: "Selected file",
    size: "Size",
    totalSize: "Total size",
    truncated: "Truncated: {reason}",
    type: "Type",
    workspace: "Workspace",
  },
  ja: {
    "action.export": "Excel export",
    "action.preview": "Preview",
    "action.refresh": "更新",
    analysisOverview: "解析 overview",
    currentFilters: "現在の filter",
    empty: "現在の filter に一致する Classic ASP file はありません。",
    excludeGlobs: "Exclude globs",
    fileCount: "{count} files",
    files: "Files",
    filters: "Filters",
    folder: "Folder",
    foldersScanned: "Folders scanned",
    fullPath: "Full path",
    includeGlobs: "Include globs",
    lastModified: "Modified",
    lastScanned: "Last scanned",
    "mode.excel": "Analysis Files",
    "mode.view": "Project Glob Files",
    name: "Name",
    noSelection: "file を選ぶと詳細を表示します。",
    none: "None",
    open: "開く",
    previewFailed: "Preview 失敗: {error}",
    projectRoot: "Project Root",
    relativePath: "Relative Path",
    respectGitIgnore: ".gitignore を尊重",
    search: "Search files...",
    selectedFile: "Selected File",
    size: "Size",
    totalSize: "Total Size",
    truncated: "切り詰め: {reason}",
    type: "Type",
    workspace: "Workspace",
  },
};

function App(): React.ReactElement {
  const [payload, setPayload] = useState<WorkspaceFilesPayload>(initialPayload ?? emptyPayload());
  const locale: Locale = payload.locale === "ja" ? "ja" : "en";
  const [includeGlobs, setIncludeGlobs] = useState(globText(payload.includeGlobs));
  const [excludeGlobs, setExcludeGlobs] = useState(globText(payload.excludeGlobs));
  const [respectGitIgnore, setRespectGitIgnore] = useState(payload.respectGitIgnore);
  const [search, setSearch] = useState("");
  const [selectedUri, setSelectedUri] = useState<string | undefined>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | undefined>();
  const allFiles = useMemo(() => payload.roots.flatMap((root) => root.files), [payload]);
  const rows = useMemo(() => treeRows(payload, search), [payload, search]);
  const summary = useMemo(() => summarizePayload(payload), [payload]);
  const selectedFile =
    allFiles.find((file) => file.uri === selectedUri) ?? allFiles[0] ?? undefined;
  const fileUris = allFiles.map((file) => file.uri);
  const includeList = globLines(includeGlobs);
  const excludeList = globLines(excludeGlobs);
  const text = (key: TextKey, params: Record<string, string | number> = {}): string => {
    let message = messages[locale][key] ?? messages.en[key];
    for (const [name, value] of Object.entries(params)) {
      message = message.replaceAll(`{${name}}`, String(value));
    }
    return message;
  };
  const rootLabel =
    payload.roots.map((root) => root.displayPath ?? root.name).join(", ") || text("workspace");
  const request = (): WorkspaceFilesPreviewRequest => ({
    includeGlobs: includeList,
    excludeGlobs: excludeList,
    respectGitIgnore,
  });

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
    <div
      className="workspace-files-app"
      data-asp-lsp-theme={payload.settings?.theme === "light" ? "light" : "dark"}
    >
      <style>{styles}</style>
      <header className="workspace-files-toolbar">
        <div className="toolbar-title">
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
      <section className="filter-strip" aria-label={text("filters")}>
        <label className="glob-field">
          <span>{text("includeGlobs")}</span>
          <textarea
            value={includeGlobs}
            onChange={(event) => setIncludeGlobs(event.currentTarget.value)}
            spellCheck={false}
          />
        </label>
        <label className="glob-field">
          <span>{text("excludeGlobs")}</span>
          <textarea
            value={excludeGlobs}
            onChange={(event) => setExcludeGlobs(event.currentTarget.value)}
            spellCheck={false}
          />
        </label>
        <div className="filter-actions">
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
        </div>
      </section>
      {payload.truncated ? (
        <div className="notice">{text("truncated", { reason: payload.truncated.reason })}</div>
      ) : null}
      {error ? <div className="notice danger">{error}</div> : null}
      <main className="workspace-files-main">
        <section className="tree-pane" aria-label={text("files")}>
          <div className="tree-pane-heading">
            <div>
              <h2>{text("projectRoot")}</h2>
              <p>{rootLabel}</p>
            </div>
            <span>{text("fileCount", { count: payload.stats.files })}</span>
          </div>
          <div className="tree-table-header" aria-hidden="true">
            <span>{text("name")}</span>
            <span>{text("type")}</span>
            <span>{text("size")}</span>
            <span>{text("lastModified")}</span>
          </div>
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
                  locale={locale}
                  row={row}
                  selected={row.kind === "file" && row.file.uri === selectedFile?.uri}
                  text={text}
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
          <section className="panel-section overview-section">
            <h2>{text("analysisOverview")}</h2>
            <div className="overview-grid">
              <MetricCard
                detail={`ASP: ${summary.aspFiles} · INC: ${summary.incFiles} · ASA: ${summary.asaFiles}`}
                label={text("files")}
                value={formatNumber(payload.stats.files, locale)}
              />
              <MetricCard
                label={text("foldersScanned")}
                value={formatNumber(summary.folders, locale)}
              />
              <MetricCard label={text("totalSize")} value={formatBytes(payload.stats.totalBytes)} />
              <MetricCard
                detail={
                  summary.latestModifiedMs > 0 ? formatDate(summary.latestModifiedMs, locale) : ""
                }
                label={text("lastScanned")}
                value={
                  summary.latestModifiedMs > 0
                    ? formatDateShort(summary.latestModifiedMs, locale)
                    : "-"
                }
              />
            </div>
          </section>
          <section className="panel-section">
            <h2>{text("currentFilters")}</h2>
            <GlobChips label={text("includeGlobs")} none={text("none")} values={includeList} />
            <GlobChips
              label={text("excludeGlobs")}
              none={text("none")}
              tone="danger"
              values={excludeList}
            />
          </section>
          <section className="panel-section selected-section">
            <h2>{text("selectedFile")}</h2>
            {selectedFile ? (
              <dl className="details-list">
                <dt>{text("type")}</dt>
                <dd>{fileType(selectedFile)}</dd>
                <dt>{text("size")}</dt>
                <dd>{formatBytes(selectedFile.size)}</dd>
                <dt>{text("lastModified")}</dt>
                <dd>{formatDate(selectedFile.mtimeMs, locale)}</dd>
                <dt>{text("relativePath")}</dt>
                <dd>{selectedFile.displayPath ?? selectedFile.relativePath}</dd>
                <dt>{text("fullPath")}</dt>
                <dd>{selectedFile.fileName}</dd>
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
          {payload.mode === "excel" ? (
            <section className="panel-section export-section">
              <button
                type="button"
                className="primary-button export-button"
                onClick={exportExcel}
                disabled={busy || fileUris.length === 0}
              >
                {text("action.export")}
              </button>
            </section>
          ) : null}
        </aside>
      </main>
    </div>
  );
}

function MetricCard({
  detail,
  label,
  value,
}: {
  detail?: string;
  label: string;
  value: string;
}): React.ReactElement {
  return (
    <div className="metric-card">
      <span>{label}</span>
      <strong>{value}</strong>
      {detail ? <small>{detail}</small> : null}
    </div>
  );
}

function GlobChips({
  label,
  none,
  tone = "default",
  values,
}: {
  label: string;
  none: string;
  tone?: "danger" | "default";
  values: string[];
}): React.ReactElement {
  return (
    <div className="filter-chip-row">
      <span>{label}:</span>
      <div>
        {values.length === 0 ? (
          <em>{none}</em>
        ) : (
          values.map((value) => (
            <code className={`filter-chip ${tone}`} key={value}>
              {value}
            </code>
          ))
        )}
      </div>
    </div>
  );
}

function TreeRowView({
  locale,
  row,
  selected,
  text,
  onSelect,
}: {
  locale: Locale;
  row: TreeRow;
  selected: boolean;
  text(key: TextKey, params?: Record<string, string | number>): string;
  onSelect(): void;
}): React.ReactElement {
  const className = `tree-row ${row.kind}${selected ? " selected" : ""}`;
  return (
    <button type="button" className={className} onClick={onSelect} title={row.detail}>
      <span className="tree-name" style={{ paddingLeft: `${10 + row.depth * 18}px` }}>
        <span className="tree-icon" aria-hidden="true">
          {row.kind === "file" ? fileType(row.file) : row.kind === "folder" ? "/" : "WS"}
        </span>
        <span className="tree-label">{row.label}</span>
      </span>
      <span className="tree-type">
        {row.kind === "file"
          ? fileType(row.file)
          : row.kind === "folder"
            ? text("folder")
            : text("workspace")}
      </span>
      <span className="tree-size">{row.kind === "file" ? formatBytes(row.file.size) : "-"}</span>
      <span className="tree-modified">
        {row.kind === "file" ? formatDateShort(row.file.mtimeMs, locale) : "-"}
      </span>
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

function summarizePayload(payload: WorkspaceFilesPayload): Summary {
  const folders = new Set<string>();
  let aspFiles = 0;
  let asaFiles = 0;
  let incFiles = 0;
  let latestModifiedMs = 0;
  for (const root of payload.roots) {
    for (const file of root.files) {
      const type = fileType(file);
      if (type === "ASP") {
        aspFiles += 1;
      } else if (type === "ASA") {
        asaFiles += 1;
      } else if (type === "INC") {
        incFiles += 1;
      }
      latestModifiedMs = Math.max(latestModifiedMs, file.mtimeMs);
      const parts = file.relativePath.split("/");
      for (let index = 0; index < parts.length - 1; index += 1) {
        folders.add(`${root.uri}:${parts.slice(0, index + 1).join("/")}`);
      }
    }
  }
  return { asaFiles, aspFiles, folders: folders.size, incFiles, latestModifiedMs };
}

function globText(globs: string[]): string {
  return globs.join(", ");
}

function globLines(value: string): string[] {
  const globs: string[] = [];
  let current = "";
  let braceDepth = 0;
  const flush = (): void => {
    const glob = current.trim();
    if (glob.length > 0) {
      globs.push(glob);
    }
    current = "";
  };
  for (const char of value) {
    if (char === "{") {
      braceDepth += 1;
      current += char;
    } else if (char === "}") {
      braceDepth = Math.max(0, braceDepth - 1);
      current += char;
    } else if ((char === "," && braceDepth === 0) || char === "\n" || char === "\r") {
      flush();
    } else {
      current += char;
    }
  }
  flush();
  return globs;
}

function fileType(file: WorkspaceFilesFile): string {
  const extension = file.relativePath.split(".").at(-1)?.toUpperCase();
  return extension && extension !== file.relativePath.toUpperCase() ? extension : "ASP";
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

function formatDate(value: number, locale: Locale): string {
  return new Date(value).toLocaleString(locale);
}

function formatDateShort(value: number, locale: Locale): string {
  return new Date(value).toLocaleDateString(locale, {
    day: "2-digit",
    month: "2-digit",
    year: "numeric",
  });
}

function formatNumber(value: number, locale: Locale): string {
  return new Intl.NumberFormat(locale).format(value);
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
