import React, { useEffect, useMemo, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import { VirtualList } from "./virtual-list";
import styles from "./workspace-files.css?inline";
import { cn } from "../lib/utils";
import type {
  WorkspaceFilesFile,
  WorkspaceFilesGlobStat,
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

type GlobInputItem = {
  id: string;
  value: string;
};

type GlobKind = "exclude" | "include";

type Locale = "en" | "ja";
type TextKey =
  | "action.addGlob"
  | "action.export"
  | "action.removeGlob"
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
  | "globPending"
  | "includeGlobs"
  | "lastModified"
  | "lastScanned"
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
  | "title"
  | "totalSize"
  | "truncated"
  | "type"
  | "workspace";

const vscode = acquireVsCodeApi();
const initialPayload = window.__ASP_LSP_WORKSPACE_FILES__;
let nextGlobItemId = 0;

const messages: Record<Locale, Record<TextKey, string>> = {
  en: {
    "action.addGlob": "Add glob",
    "action.export": "Export Excel",
    "action.removeGlob": "Remove glob",
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
    globPending: "Preview required",
    includeGlobs: "Include globs",
    lastModified: "Modified",
    lastScanned: "Last scanned",
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
    title: "Analysis files",
    totalSize: "Total size",
    truncated: "Truncated: {reason}",
    type: "Type",
    workspace: "Workspace",
  },
  ja: {
    "action.addGlob": "glob 追加",
    "action.export": "Excel export",
    "action.removeGlob": "glob 削除",
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
    globPending: "Preview 待ち",
    includeGlobs: "Include globs",
    lastModified: "Modified",
    lastScanned: "Last scanned",
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
    title: "Analysis Files",
    totalSize: "Total Size",
    truncated: "切り詰め: {reason}",
    type: "Type",
    workspace: "Workspace",
  },
};

function messageText(
  locale: Locale,
  key: TextKey,
  params: Record<string, string | number> = {},
): string {
  let message = messages[locale][key] ?? messages.en[key];
  for (const [name, value] of Object.entries(params)) {
    message = message.replaceAll(`{${name}}`, String(value));
  }
  return message;
}

function createRequestId(): string {
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
}

function previewRequestSignature(request: WorkspaceFilesPreviewRequest): string {
  return JSON.stringify({
    includeGlobs: request.includeGlobs,
    excludeGlobs: request.excludeGlobs,
    respectGitIgnore: request.respectGitIgnore,
  });
}

function App(): React.ReactElement {
  const [payload, setPayload] = useState<WorkspaceFilesPayload>(initialPayload ?? emptyPayload());
  const locale: Locale = payload.locale === "ja" ? "ja" : "en";
  const [includeGlobItems, setIncludeGlobItems] = useState(() =>
    globItems(payload.includeGlobs, "include"),
  );
  const [excludeGlobItems, setExcludeGlobItems] = useState(() =>
    globItems(payload.excludeGlobs, "exclude"),
  );
  const [respectGitIgnore, setRespectGitIgnore] = useState(payload.respectGitIgnore);
  const [search, setSearch] = useState("");
  const [collapsedTreeIds, setCollapsedTreeIds] = useState<ReadonlySet<string>>(() => new Set());
  const [selectedUri, setSelectedUri] = useState<string | undefined>();
  const [previewBusy, setPreviewBusy] = useState(false);
  const [exportBusy, setExportBusy] = useState(false);
  const [error, setError] = useState<string | undefined>();
  const activePreviewRequestIdRef = useRef<string | undefined>(undefined);
  const lastPreviewSignatureRef = useRef<string | undefined>(undefined);
  const allFiles = useMemo(() => payload.roots.flatMap((root) => root.files), [payload]);
  const rows = useMemo(
    () => visibleTreeRows(treeRows(payload), collapsedTreeIds),
    [payload, collapsedTreeIds],
  );
  const summary = useMemo(() => summarizePayload(payload), [payload]);
  const selectedFile =
    allFiles.find((file) => file.uri === selectedUri) ?? allFiles[0] ?? undefined;
  const includeList = useMemo(() => globValues(includeGlobItems), [includeGlobItems]);
  const excludeList = useMemo(() => globValues(excludeGlobItems), [excludeGlobItems]);
  const previewRequest = useMemo<WorkspaceFilesPreviewRequest>(
    () => ({
      includeGlobs: includeList,
      excludeGlobs: excludeList,
      respectGitIgnore,
    }),
    [excludeList, includeList, respectGitIgnore],
  );
  const previewSignature = useMemo(() => previewRequestSignature(previewRequest), [previewRequest]);
  const busy = previewBusy || exportBusy;
  const text = (key: TextKey, params: Record<string, string | number> = {}): string =>
    messageText(locale, key, params);
  const rootLabel =
    payload.roots.map((root) => root.displayPath ?? root.name).join(", ") || text("workspace");

  useEffect(() => {
    if (lastPreviewSignatureRef.current === undefined) {
      lastPreviewSignatureRef.current = previewSignature;
      return;
    }
    if (previewSignature === lastPreviewSignatureRef.current) {
      return;
    }
    if (activePreviewRequestIdRef.current !== undefined) {
      activePreviewRequestIdRef.current = undefined;
      setPreviewBusy(false);
    }
    const requestId = createRequestId();
    let listener: ((event: MessageEvent) => void) | undefined;
    const timeout = window.setTimeout(() => {
      activePreviewRequestIdRef.current = requestId;
      setPreviewBusy(true);
      setError(undefined);
      listener = (event: MessageEvent): void => {
        const message = event.data as {
          type?: string;
          requestId?: string;
          payload?: WorkspaceFilesPayload;
          error?: string;
        };
        if (message.type !== "previewResult" || message.requestId !== requestId) {
          return;
        }
        if (listener) {
          window.removeEventListener("message", listener);
        }
        if (activePreviewRequestIdRef.current !== requestId) {
          return;
        }
        activePreviewRequestIdRef.current = undefined;
        setPreviewBusy(false);
        if (message.payload) {
          lastPreviewSignatureRef.current = previewRequestSignature({
            includeGlobs: message.payload.includeGlobs,
            excludeGlobs: message.payload.excludeGlobs,
            respectGitIgnore: message.payload.respectGitIgnore,
          });
          setPayload(message.payload);
          setIncludeGlobItems(globItems(message.payload.includeGlobs, "include"));
          setExcludeGlobItems(globItems(message.payload.excludeGlobs, "exclude"));
          setRespectGitIgnore(message.payload.respectGitIgnore);
          setSelectedUri(undefined);
          setCollapsedTreeIds(new Set());
        } else {
          setError(messageText(locale, "previewFailed", { error: message.error ?? "unknown" }));
        }
      };
      window.addEventListener("message", listener);
      vscode.postMessage({ type: "preview", requestId, ...previewRequest });
    }, 500);
    return () => {
      window.clearTimeout(timeout);
      if (listener) {
        window.removeEventListener("message", listener);
      }
      if (activePreviewRequestIdRef.current === requestId) {
        activePreviewRequestIdRef.current = undefined;
        setPreviewBusy(false);
      }
    };
  }, [locale, previewRequest, previewSignature]);

  const toggleTreeRow = (id: string): void => {
    setCollapsedTreeIds((ids) => {
      const next = new Set(ids);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
  };

  const updateGlobItem = (kind: GlobKind, id: string, value: string): void => {
    const update = (item: GlobInputItem): GlobInputItem =>
      item.id === id ? { ...item, value } : item;
    if (kind === "include") {
      setIncludeGlobItems((items) => items.map(update));
    } else {
      setExcludeGlobItems((items) => items.map(update));
    }
  };

  const addGlobItem = (kind: GlobKind): void => {
    const item = createGlobItem(kind, "");
    if (kind === "include") {
      setIncludeGlobItems((items) => [...items, item]);
    } else {
      setExcludeGlobItems((items) => [...items, item]);
    }
  };

  const removeGlobItem = (kind: GlobKind, id: string): void => {
    const remove = (items: GlobInputItem[]): GlobInputItem[] => {
      const next = items.filter((item) => item.id !== id);
      return next.length > 0 ? next : [createGlobItem(kind, "")];
    };
    if (kind === "include") {
      setIncludeGlobItems(remove);
    } else {
      setExcludeGlobItems(remove);
    }
  };

  function exportSelectedExcel(): void {
    if (!selectedFile) {
      return;
    }
    setExportBusy(true);
    const listener = (event: MessageEvent): void => {
      const message = event.data as { type?: string; ok?: boolean; error?: string };
      if (message.type !== "exportResult") {
        return;
      }
      window.removeEventListener("message", listener);
      setExportBusy(false);
      if (message.ok === false) {
        setError(message.error ?? "Export failed.");
      }
    };
    window.addEventListener("message", listener);
    vscode.postMessage({
      type: "exportSelectedExcel",
      selectedUri: selectedFile.uri,
      ...previewRequest,
    });
  }

  return (
    <div
      className="workspace-files-app"
      data-asp-lsp-theme={payload.settings?.theme === "light" ? "light" : "dark"}
    >
      <style>{styles}</style>
      <header className="workspace-files-toolbar">
        <div className="toolbar-title">
          <h1>{text("title")}</h1>
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
        </div>
      </header>
      <section className="filter-strip" aria-label={text("filters")}>
        <GlobEditor
          items={includeGlobItems}
          kind="include"
          label={text("includeGlobs")}
          stats={payload.globStats?.include}
          text={text}
          onAdd={() => addGlobItem("include")}
          onChange={(id, value) => updateGlobItem("include", id, value)}
          onRemove={(id) => removeGlobItem("include", id)}
        />
        <GlobEditor
          items={excludeGlobItems}
          kind="exclude"
          label={text("excludeGlobs")}
          stats={payload.globStats?.exclude}
          text={text}
          onAdd={() => addGlobItem("exclude")}
          onChange={(id, value) => updateGlobItem("exclude", id, value)}
          onRemove={(id) => removeGlobItem("exclude", id)}
        />
        <div className="filter-actions">
          <label className="checkbox-row">
            <input
              type="checkbox"
              checked={respectGitIgnore}
              onChange={(event) => setRespectGitIgnore(event.currentTarget.checked)}
            />
            <span>{text("respectGitIgnore")}</span>
          </label>
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
                  search={search}
                  selected={row.kind === "file" && row.file.uri === selectedFile?.uri}
                  text={text}
                  collapsed={isCollapsibleTreeRow(row) && collapsedTreeIds.has(row.id)}
                  onSelect={() => {
                    if (row.kind === "file") {
                      setSelectedUri(row.file.uri);
                    } else {
                      toggleTreeRow(row.id);
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
            <GlobChips
              label={text("includeGlobs")}
              none={text("none")}
              stats={payload.globStats?.include}
              text={text}
              values={includeList}
            />
            <GlobChips
              label={text("excludeGlobs")}
              none={text("none")}
              stats={payload.globStats?.exclude}
              text={text}
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
              <div className="selected-actions">
                <button
                  type="button"
                  onClick={() => vscode.postMessage({ type: "openFile", uri: selectedFile.uri })}
                >
                  {text("open")}
                </button>
                <button
                  type="button"
                  className="primary-button"
                  onClick={exportSelectedExcel}
                  disabled={busy}
                >
                  {text("action.export")}
                </button>
              </div>
            ) : null}
          </section>
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

function GlobEditor({
  items,
  kind,
  label,
  stats,
  text,
  onAdd,
  onChange,
  onRemove,
}: {
  items: GlobInputItem[];
  kind: GlobKind;
  label: string;
  stats: WorkspaceFilesGlobStat[] | undefined;
  text(key: TextKey, params?: Record<string, string | number>): string;
  onAdd(): void;
  onChange(id: string, value: string): void;
  onRemove(id: string): void;
}): React.ReactElement {
  return (
    <section className="glob-editor">
      <div className="glob-editor-heading">
        <span>{label}</span>
        <button type="button" onClick={onAdd}>
          {text("action.addGlob")}
        </button>
      </div>
      <div className="glob-editor-list">
        {items.map((item, index) => {
          const count = globStatCount(stats, index, item.value);
          return (
            <div className={cn("glob-row", kind)} key={item.id}>
              <input
                aria-label={`${label} ${index + 1}`}
                value={item.value}
                onChange={(event) => onChange(item.id, event.currentTarget.value)}
                spellCheck={false}
              />
              <span className="glob-count">{globCountText(count, text)}</span>
              <button
                type="button"
                className="icon-button"
                title={text("action.removeGlob")}
                onClick={() => onRemove(item.id)}
              >
                x
              </button>
            </div>
          );
        })}
      </div>
    </section>
  );
}

function GlobChips({
  label,
  none,
  stats,
  text,
  tone = "default",
  values,
}: {
  label: string;
  none: string;
  stats: WorkspaceFilesGlobStat[] | undefined;
  text(key: TextKey, params?: Record<string, string | number>): string;
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
          values.map((value, index) => (
            <code className={cn("filter-chip", tone)} key={`${value}:${index}`}>
              {value}
              <span>{globCountText(globStatCount(stats, index, value), text)}</span>
            </code>
          ))
        )}
      </div>
    </div>
  );
}

function TreeRowView({
  collapsed,
  locale,
  row,
  search,
  selected,
  text,
  onSelect,
}: {
  collapsed: boolean;
  locale: Locale;
  row: TreeRow;
  search: string;
  selected: boolean;
  text(key: TextKey, params?: Record<string, string | number>): string;
  onSelect(): void;
}): React.ReactElement {
  const className = cn("tree-row", row.kind, selected && "selected");
  const collapsible = isCollapsibleTreeRow(row);
  return (
    <button
      type="button"
      aria-expanded={collapsible ? !collapsed : undefined}
      className={className}
      onClick={onSelect}
      title={row.detail ?? row.label}
    >
      <span className="tree-name" style={{ paddingLeft: `${10 + row.depth * 18}px` }}>
        <span className="tree-disclosure" aria-hidden="true">
          {collapsible ? (collapsed ? "+" : "-") : ""}
        </span>
        <span className="tree-icon" aria-hidden="true">
          {row.kind === "file" ? fileType(row.file) : row.kind === "folder" ? "/" : "WS"}
        </span>
        <span className="tree-label">
          <HighlightedText query={search} text={row.label} />
        </span>
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

function HighlightedText({ query, text }: { query: string; text: string }): React.ReactElement {
  const normalizedQuery = query.trim().toLowerCase();
  if (!normalizedQuery) {
    return <>{text}</>;
  }

  const ranges = highlightRanges(text, normalizedQuery);
  if (ranges.length === 0) {
    return <>{text}</>;
  }

  const parts: React.ReactNode[] = [];
  let cursor = 0;
  for (const [start, end] of ranges) {
    if (cursor < start) {
      parts.push(text.slice(cursor, start));
    }
    parts.push(
      <mark className="tree-match" key={`${start}:${end}`}>
        {text.slice(start, end)}
      </mark>,
    );
    cursor = end;
  }
  if (cursor < text.length) {
    parts.push(text.slice(cursor));
  }
  return <>{parts}</>;
}

function highlightRanges(text: string, normalizedQuery: string): Array<[number, number]> {
  const normalizedText = text.toLowerCase();
  const ranges: Array<[number, number]> = [];
  let cursor = 0;
  while (cursor < normalizedText.length) {
    const start = normalizedText.indexOf(normalizedQuery, cursor);
    if (start < 0) {
      break;
    }
    const end = start + normalizedQuery.length;
    ranges.push([start, end]);
    cursor = end;
  }
  return ranges;
}

function isCollapsibleTreeRow(row: TreeRow): boolean {
  return row.kind === "folder" || row.kind === "root";
}

function visibleTreeRows(rows: TreeRow[], collapsedIds: ReadonlySet<string>): TreeRow[] {
  const visibleRows: TreeRow[] = [];
  let collapsedDepth: number | undefined;
  for (const row of rows) {
    if (collapsedDepth !== undefined) {
      if (row.depth > collapsedDepth) {
        continue;
      }
      collapsedDepth = undefined;
    }
    visibleRows.push(row);
    if (isCollapsibleTreeRow(row) && collapsedIds.has(row.id)) {
      collapsedDepth = row.depth;
    }
  }
  return visibleRows;
}

function treeRows(payload: WorkspaceFilesPayload): TreeRow[] {
  const rows: TreeRow[] = [];
  for (const root of payload.roots) {
    if (root.files.length === 0) {
      continue;
    }
    rows.push({
      id: `root:${root.uri}`,
      kind: "root",
      depth: 0,
      label: root.displayPath ?? root.name,
      detail: `${root.files.length}`,
    });
    const folderIds = new Set<string>();
    for (const file of [...root.files].sort((left, right) =>
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

function globItems(globs: string[], kind: GlobKind): GlobInputItem[] {
  const items = globs.map((glob) => createGlobItem(kind, glob));
  return items.length > 0 ? items : [createGlobItem(kind, "")];
}

function createGlobItem(kind: GlobKind, value: string): GlobInputItem {
  nextGlobItemId += 1;
  return { id: `${kind}:${nextGlobItemId}`, value };
}

function globValues(items: GlobInputItem[]): string[] {
  return items.map((item) => item.value.trim()).filter((value) => value.length > 0);
}

function globStatCount(
  stats: WorkspaceFilesGlobStat[] | undefined,
  index: number,
  value: string,
): number | undefined {
  const glob = value.trim();
  if (glob.length === 0) {
    return 0;
  }
  const stat = stats?.[index];
  return stat?.glob === glob ? stat.files : undefined;
}

function globCountText(
  count: number | undefined,
  text: (key: TextKey, params?: Record<string, string | number>) => string,
): string {
  return count === undefined ? text("globPending") : text("fileCount", { count });
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
    includeGlobs: ["**/*.{asp,asa,inc}"],
    excludeGlobs: [],
    globStats: {
      include: [{ glob: "**/*.{asp,asa,inc}", files: 0 }],
      exclude: [],
    },
    respectGitIgnore: false,
    roots: [],
    stats: { files: 0, totalBytes: 0 },
  };
}

createRoot(document.getElementById("root") ?? document.body).render(<App />);
