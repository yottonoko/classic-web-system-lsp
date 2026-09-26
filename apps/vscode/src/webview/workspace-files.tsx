import { createSignal, createMemo, createEffect, untrack, type Accessor } from "solid-js";
import { render, type JSX } from "@solidjs/web";
import { webviewStyle } from "./webview-dom-types";
import { ImeSafeInput } from "./ime-safe-input";
import { VirtualList } from "./virtual-list";
import { WebviewErrorBoundary } from "./webview-error-boundary";
import styles from "./workspace-files.css?inline";
import type {
  WorkspaceFilesPayload,
  WorkspaceFilesPreviewRequest,
} from "../workspace-files-webview";
import { MetricCard, GlobChips, GlobEditor, TreeRowView } from "./workspace-files-components";
import {
  createGlobItem,
  excludePatternForTreeRow,
  emptyPayload,
  fileType,
  formatBytes,
  formatDate,
  formatDateShort,
  formatNumber,
  globItems,
  globValues,
  isCollapsibleTreeRow,
  summarizePayload,
  treeRows,
  visibleTreeRows,
} from "./workspace-files-model";
import type {
  GlobInputItem,
  GlobKind,
  Locale,
  TextKey,
  TreeContextMenu,
  TreeRow,
} from "./workspace-files-types";
declare const acquireVsCodeApi: () => {
  postMessage(message: unknown): void;
};
declare global {
  interface Window {
    __ASP_LSP_WORKSPACE_FILES__?: WorkspaceFilesPayload;
  }
}
const vscode = acquireVsCodeApi();
const initialPayload = window.__ASP_LSP_WORKSPACE_FILES__;
const initialWorkspaceFilesPayload = initialPayload ?? emptyPayload();
function detectedWorkspaceTheme(): "light" | "dark" {
  const classList = document.body.classList;
  return classList.contains("vscode-light") || classList.contains("vscode-high-contrast-light")
    ? "light"
    : "dark";
}
function createResolvedWorkspaceTheme(
  setting: Accessor<"auto" | "light" | "dark" | undefined>,
): Accessor<"light" | "dark"> {
  const [theme, setTheme] = createSignal<"light" | "dark">(untrack(() => detectedWorkspaceTheme()));
  createEffect(setting, (setting) => {
    if (setting === "light" || setting === "dark") {
      setTheme(setting);
      return undefined;
    }
    const observer = new MutationObserver(() => setTheme(detectedWorkspaceTheme()));
    const options: MutationObserverInit = { attributes: true, attributeFilter: ["class", "style"] };
    observer.observe(document.body, options);
    observer.observe(document.documentElement, options);
    setTheme(detectedWorkspaceTheme());
    return () => observer.disconnect();
  });
  return createMemo(() => {
    const value = setting();
    return value === "light" || value === "dark" ? value : theme();
  });
}
const messages: Record<Locale, Record<TextKey, string>> = {
  en: {
    "action.addGlob": "Add glob",
    "action.excludePattern": "Add exclude glob: {pattern}",
    "action.export": "Export Excel",
    "action.removeGlob": "Remove glob",
    "action.saveSettings": "Save settings",
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
    settingsSaveFailed: "Save failed: {error}",
    settingsSaved: "Settings saved",
    showUnmatched: "Show unmatched files",
    size: "Size",
    savingSettings: "Saving...",
    title: "Analysis files",
    totalSize: "Total size",
    type: "Type",
    workspace: "Workspace",
  },
  ja: {
    "action.addGlob": "glob を追加",
    "action.excludePattern": "除外 glob に追加: {pattern}",
    "action.export": "Excel 出力",
    "action.removeGlob": "glob を削除",
    "action.saveSettings": "設定に保存",
    analysisOverview: "解析概要",
    currentFilters: "現在のフィルター",
    empty: "現在のフィルターに一致する Classic ASP ファイルはありません。",
    excludeGlobs: "除外 glob",
    fileCount: "{count} 件",
    files: "ファイル",
    filters: "フィルター",
    folder: "フォルダー",
    foldersScanned: "スキャンしたフォルダー",
    fullPath: "フルパス",
    globPending: "プレビュー待ち",
    includeGlobs: "対象 glob",
    lastModified: "更新日時",
    lastScanned: "最終スキャン",
    name: "名前",
    noSelection: "ファイルを選ぶと詳細を表示します。",
    none: "なし",
    open: "開く",
    previewFailed: "プレビューに失敗しました: {error}",
    projectRoot: "プロジェクトルート",
    relativePath: "相対パス",
    respectGitIgnore: ".gitignore を尊重",
    search: "ファイルを検索...",
    selectedFile: "選択中のファイル",
    settingsSaveFailed: "保存に失敗しました: {error}",
    settingsSaved: "設定を保存しました",
    showUnmatched: "対象外ファイル/フォルダーも表示",
    size: "サイズ",
    savingSettings: "保存中...",
    title: "解析ファイル",
    totalSize: "合計サイズ",
    type: "種類",
    workspace: "ワークスペース",
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
    showUnmatched: request.showUnmatched,
  });
}
function settingsRequestSignature(request: WorkspaceFilesPreviewRequest): string {
  return JSON.stringify({
    includeGlobs: request.includeGlobs,
    excludeGlobs: request.excludeGlobs,
    respectGitIgnore: request.respectGitIgnore,
  });
}
function previewRequestFromPayload(payload: WorkspaceFilesPayload): WorkspaceFilesPreviewRequest {
  return {
    includeGlobs: payload.includeGlobs,
    excludeGlobs: payload.excludeGlobs,
    respectGitIgnore: payload.respectGitIgnore,
    showUnmatched: payload.showUnmatched,
  };
}
function App(): JSX.Element {
  const [payload, setPayload] = createSignal<WorkspaceFilesPayload>(initialWorkspaceFilesPayload);
  const locale = createMemo<Locale>(() => (payload().locale === "ja" ? "ja" : "en"));
  const theme = createResolvedWorkspaceTheme(() => payload().settings?.theme);
  const [includeGlobItems, setIncludeGlobItems] = createSignal(
    untrack(() => globItems(payload().includeGlobs, "include")),
  );
  const [excludeGlobItems, setExcludeGlobItems] = createSignal(
    untrack(() => globItems(payload().excludeGlobs, "exclude")),
  );
  const [respectGitIgnore, setRespectGitIgnore] = createSignal(payload().respectGitIgnore);
  const [showUnmatched, setShowUnmatched] = createSignal(payload().showUnmatched !== false);
  const [search, setSearch] = createSignal("");
  const [collapsedTreeIds, setCollapsedTreeIds] = createSignal<ReadonlySet<string>>(
    untrack(() => new Set<string>()),
  );
  const [selectedUri, setSelectedUri] = createSignal<string | undefined>();
  const [contextMenu, setContextMenu] = createSignal<TreeContextMenu | undefined>();
  const [committedPreviewRequest, setCommittedPreviewRequest] =
    createSignal<WorkspaceFilesPreviewRequest>(untrack(() => previewRequestFromPayload(payload())));
  const [savedSettingsSignature, setSavedSettingsSignature] = createSignal(
    untrack(() => settingsRequestSignature(previewRequestFromPayload(payload()))),
  );
  const [previewBusy, setPreviewBusy] = createSignal(false);
  const [exportBusy, setExportBusy] = createSignal(false);
  const [saveBusy, setSaveBusy] = createSignal(false);
  const [error, setError] = createSignal<string | undefined>();
  const [status, setStatus] = createSignal<string | undefined>();
  const activePreviewRequestIdRef = { current: undefined } as {
    current: (string | undefined) | undefined;
  };
  const activeSaveRequestIdRef = { current: undefined } as {
    current: (string | undefined) | undefined;
  };
  const draftPreviewSignatureRef = { current: undefined } as {
    current: (string | undefined) | undefined;
  };
  const lastPreviewSignatureRef = { current: undefined } as {
    current: (string | undefined) | undefined;
  };
  const allFiles = createMemo(() => payload().roots.flatMap((root) => root.files));
  const rows = createMemo(() => visibleTreeRows(treeRows(payload()), collapsedTreeIds()));
  const summary = createMemo(() => summarizePayload(payload()));
  const selectedFile = createMemo(
    () => allFiles().find((file) => file.uri === selectedUri()) ?? allFiles()[0] ?? undefined,
  );
  const includeList = createMemo(() => globValues(includeGlobItems()));
  const excludeList = createMemo(() => globValues(excludeGlobItems()));
  const previewRequest = createMemo<WorkspaceFilesPreviewRequest>(() => ({
    includeGlobs: includeList(),
    excludeGlobs: excludeList(),
    respectGitIgnore: respectGitIgnore(),
    showUnmatched: showUnmatched(),
  }));
  const draftPreviewSignature = createMemo(() => previewRequestSignature(previewRequest()));
  const committedPreviewSignature = createMemo(() =>
    previewRequestSignature(committedPreviewRequest()),
  );
  const draftSettingsSignature = createMemo(() => settingsRequestSignature(previewRequest()));
  const previewPending = createMemo(() => draftPreviewSignature() !== committedPreviewSignature());
  const settingsDirty = createMemo(() => draftSettingsSignature() !== savedSettingsSignature());
  const busy = createMemo(() => previewBusy() || exportBusy() || saveBusy());
  const text = (key: TextKey, params: Record<string, string | number> = {}): string =>
    messageText(locale(), key, params);
  const rootLabel = createMemo(
    () =>
      payload()
        .roots.map((root) => root.displayPath ?? root.name)
        .join(", ") || text("workspace"),
  );
  createEffect(
    () => [draftPreviewSignature()],
    () => {
      draftPreviewSignatureRef.current = draftPreviewSignature();
    },
  );
  createEffect(
    () => [committedPreviewRequest(), committedPreviewSignature(), locale()],
    () => {
      if (lastPreviewSignatureRef.current === undefined) {
        lastPreviewSignatureRef.current = committedPreviewSignature();
        return;
      }
      if (committedPreviewSignature() === lastPreviewSignatureRef.current) {
        return;
      }
      if (activePreviewRequestIdRef.current !== undefined) {
        activePreviewRequestIdRef.current = undefined;
        setPreviewBusy(false);
      }
      const requestId = createRequestId();
      const requestSignature = committedPreviewSignature();
      let listener: ((event: MessageEvent) => void) | undefined;
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
          const nextRequest = previewRequestFromPayload(message.payload);
          const nextSignature = previewRequestSignature(nextRequest);
          lastPreviewSignatureRef.current = nextSignature;
          setPayload(message.payload);
          setCommittedPreviewRequest(nextRequest);
          setSelectedUri(undefined);
          setCollapsedTreeIds(new Set<string>());
          if (draftPreviewSignatureRef.current === requestSignature) {
            setIncludeGlobItems(globItems(message.payload.includeGlobs, "include"));
            setExcludeGlobItems(globItems(message.payload.excludeGlobs, "exclude"));
            setRespectGitIgnore(message.payload.respectGitIgnore);
            setShowUnmatched(message.payload.showUnmatched !== false);
          }
        } else {
          setError(messageText(locale(), "previewFailed", { error: message.error ?? "unknown" }));
        }
      };
      window.addEventListener("message", listener);
      vscode.postMessage({ type: "preview", requestId, ...committedPreviewRequest() });
      return () => {
        if (listener) {
          window.removeEventListener("message", listener);
        }
        if (activePreviewRequestIdRef.current === requestId) {
          activePreviewRequestIdRef.current = undefined;
          setPreviewBusy(false);
        }
      };
    },
  );
  createEffect(
    () => [contextMenu()],
    () => {
      if (!contextMenu()) {
        return;
      }
      const close = (): void => setContextMenu(undefined);
      const onKeyDown = (event: KeyboardEvent): void => {
        if (event.key === "Escape") {
          close();
        }
      };
      window.addEventListener("mousedown", close);
      window.addEventListener("blur", close);
      window.addEventListener("scroll", close, true);
      window.addEventListener("keydown", onKeyDown);
      return () => {
        window.removeEventListener("mousedown", close);
        window.removeEventListener("blur", close);
        window.removeEventListener("scroll", close, true);
        window.removeEventListener("keydown", onKeyDown);
      };
    },
  );
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
  const requestFromGlobItems = (
    includeItems: GlobInputItem[],
    excludeItems: GlobInputItem[],
    overrides: Partial<
      Pick<WorkspaceFilesPreviewRequest, "respectGitIgnore" | "showUnmatched">
    > = {},
  ): WorkspaceFilesPreviewRequest => ({
    includeGlobs: globValues(includeItems),
    excludeGlobs: globValues(excludeItems),
    respectGitIgnore: overrides.respectGitIgnore ?? respectGitIgnore(),
    showUnmatched: overrides.showUnmatched ?? showUnmatched(),
  });
  const commitPreviewRequest = (request: WorkspaceFilesPreviewRequest): void => {
    if (previewRequestSignature(request) === committedPreviewSignature()) {
      return;
    }
    setStatus(undefined);
    setCommittedPreviewRequest(request);
  };
  const updateGlobItem = (kind: GlobKind, id: string, value: string): void => {
    setStatus(undefined);
    const update = (item: GlobInputItem): GlobInputItem =>
      item.id === id ? { ...item, value } : item;
    if (kind === "include") {
      setIncludeGlobItems((items) => items.map(update));
    } else {
      setExcludeGlobItems((items) => items.map(update));
    }
  };
  const addGlobItem = (kind: GlobKind): void => {
    setStatus(undefined);
    const item = createGlobItem(kind, "");
    if (kind === "include") {
      setIncludeGlobItems((items) => [...items, item]);
    } else {
      setExcludeGlobItems((items) => [...items, item]);
    }
  };
  const commitGlobItem = (kind: GlobKind, id: string, value: string): void => {
    const update = (item: GlobInputItem): GlobInputItem =>
      item.id === id ? { ...item, value } : item;
    const nextIncludeItems =
      kind === "include" ? includeGlobItems().map(update) : includeGlobItems();
    const nextExcludeItems =
      kind === "exclude" ? excludeGlobItems().map(update) : excludeGlobItems();
    if (kind === "include") {
      setIncludeGlobItems(nextIncludeItems);
    } else {
      setExcludeGlobItems(nextExcludeItems);
    }
    commitPreviewRequest(requestFromGlobItems(nextIncludeItems, nextExcludeItems));
  };
  const removeGlobItem = (kind: GlobKind, id: string): void => {
    const remove = (items: GlobInputItem[]): GlobInputItem[] => {
      const next = items.filter((item) => item.id !== id);
      return next.length > 0 ? next : [createGlobItem(kind, "")];
    };
    const nextIncludeItems = kind === "include" ? remove(includeGlobItems()) : includeGlobItems();
    const nextExcludeItems = kind === "exclude" ? remove(excludeGlobItems()) : excludeGlobItems();
    if (kind === "include") {
      setIncludeGlobItems(nextIncludeItems);
    } else {
      setExcludeGlobItems(nextExcludeItems);
    }
    commitPreviewRequest(requestFromGlobItems(nextIncludeItems, nextExcludeItems));
  };
  const addExcludeGlobPattern = (pattern: string): void => {
    let nextExcludeItems = excludeGlobItems();
    if (!excludeGlobItems().some((item) => item.value.trim() === pattern)) {
      const emptyIndex = excludeGlobItems().findIndex((item) => item.value.trim().length === 0);
      nextExcludeItems =
        emptyIndex >= 0
          ? excludeGlobItems().map((item, index) =>
              index === emptyIndex ? { ...item, value: pattern } : item,
            )
          : [...excludeGlobItems(), createGlobItem("exclude", pattern)];
      setExcludeGlobItems(nextExcludeItems);
    }
    commitPreviewRequest(requestFromGlobItems(includeGlobItems(), nextExcludeItems));
    setContextMenu(undefined);
  };
  const openTreeContextMenu = (row: TreeRow, event: MouseEvent): void => {
    const pattern = excludePatternForTreeRow(row);
    if (!pattern) {
      return;
    }
    event.preventDefault();
    event.stopPropagation();
    setContextMenu({ x: event.clientX, y: event.clientY, pattern });
  };
  function exportSelectedExcel(): void {
    if (!selectedFile()) {
      return;
    }
    setExportBusy(true);
    const listener = (event: MessageEvent): void => {
      const message = event.data as {
        type?: string;
        ok?: boolean;
        error?: string;
      };
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
      selectedUri: selectedFile().uri,
      ...committedPreviewRequest(),
    });
  }
  function saveWorkspaceSettings(): void {
    if (!settingsDirty() || saveBusy()) {
      return;
    }
    const requestId = createRequestId();
    const request = previewRequest();
    const requestSignature = settingsRequestSignature(request);
    activeSaveRequestIdRef.current = requestId;
    setSaveBusy(true);
    setStatus(undefined);
    setError(undefined);
    const listener = (event: MessageEvent): void => {
      const message = event.data as {
        type?: string;
        requestId?: string;
        ok?: boolean;
        error?: string;
      };
      if (message.type !== "saveSettingsResult" || message.requestId !== requestId) {
        return;
      }
      window.removeEventListener("message", listener);
      if (activeSaveRequestIdRef.current !== requestId) {
        return;
      }
      activeSaveRequestIdRef.current = undefined;
      setSaveBusy(false);
      if (message.ok === false) {
        setError(
          messageText(locale(), "settingsSaveFailed", { error: message.error ?? "unknown" }),
        );
      } else {
        setSavedSettingsSignature(requestSignature);
        setStatus(text("settingsSaved"));
      }
    };
    window.addEventListener("message", listener);
    vscode.postMessage({
      type: "saveSettings",
      requestId,
      includeGlobs: request.includeGlobs,
      excludeGlobs: request.excludeGlobs,
      respectGitIgnore: request.respectGitIgnore,
    });
  }
  return (
    <div
      class="workspace-files-app"
      data-asp-lsp-theme={theme()}
      data-asp-lsp-theme-setting={initialWorkspaceFilesPayload.settings?.theme ?? "auto"}
    >
      <style>{styles}</style>
      <header class="workspace-files-toolbar">
        <div class="toolbar-title">
          <h1>{text("title")}</h1>
          <p>
            {text("fileCount", { count: payload().stats.files })} · {text("totalSize")}{" "}
            {formatBytes(payload().stats.totalBytes)}
          </p>
        </div>
        <div class="toolbar-actions">
          <ImeSafeInput
            aria-label={text("search")}
            class="search-input"
            placeholder={text("search")}
            value={search()}
            onValueChange={setSearch}
          />
        </div>
      </header>
      <section class="filter-strip" aria-label={text("filters")}>
        <GlobEditor
          items={includeGlobItems()}
          kind="include"
          label={text("includeGlobs")}
          stats={payload().globStats?.include}
          text={text}
          onAdd={() => addGlobItem("include")}
          onChange={(id, value) => updateGlobItem("include", id, value)}
          onCommit={(id, value) => commitGlobItem("include", id, value)}
          onRemove={(id) => removeGlobItem("include", id)}
        />
        <GlobEditor
          items={excludeGlobItems()}
          kind="exclude"
          label={text("excludeGlobs")}
          stats={payload().globStats?.exclude}
          text={text}
          onAdd={() => addGlobItem("exclude")}
          onChange={(id, value) => updateGlobItem("exclude", id, value)}
          onCommit={(id, value) => commitGlobItem("exclude", id, value)}
          onRemove={(id) => removeGlobItem("exclude", id)}
        />
        <div class="filter-actions">
          <label class="checkbox-row">
            <input
              type="checkbox"
              checked={respectGitIgnore()}
              onInput={(event) => {
                const nextRespectGitIgnore = event.currentTarget.checked;
                setRespectGitIgnore(nextRespectGitIgnore);
                commitPreviewRequest(
                  requestFromGlobItems(includeGlobItems(), excludeGlobItems(), {
                    respectGitIgnore: nextRespectGitIgnore,
                  }),
                );
              }}
            />
            <span>{text("respectGitIgnore")}</span>
          </label>
          <label class="checkbox-row">
            <input
              type="checkbox"
              checked={showUnmatched()}
              onInput={(event) => {
                const nextShowUnmatched = event.currentTarget.checked;
                setShowUnmatched(nextShowUnmatched);
                commitPreviewRequest(
                  requestFromGlobItems(includeGlobItems(), excludeGlobItems(), {
                    showUnmatched: nextShowUnmatched,
                  }),
                );
              }}
            />
            <span>{text("showUnmatched")}</span>
          </label>
          <div class="filter-status" aria-live="polite">
            {previewPending() ? text("globPending") : (status() ?? "")}
          </div>
          <button
            type="button"
            class="primary-button"
            disabled={!settingsDirty() || saveBusy()}
            onClick={saveWorkspaceSettings}
          >
            {saveBusy() ? text("savingSettings") : text("action.saveSettings")}
          </button>
        </div>
      </section>
      {error() ? <div class="notice danger">{error()}</div> : null}
      <main class="workspace-files-main">
        <section class="tree-pane" aria-label={text("files")}>
          <div class="tree-pane-heading">
            <div>
              <h2>{text("projectRoot")}</h2>
              <p>{rootLabel()}</p>
            </div>
            <span>{text("fileCount", { count: payload().stats.files })}</span>
          </div>
          <div class="tree-table-header" aria-hidden="true">
            <span>{text("name")}</span>
            <span>{text("type")}</span>
            <span>{text("size")}</span>
            <span>{text("lastModified")}</span>
          </div>
          {rows().length === 0 ? (
            <div class="empty-state">{text("empty")}</div>
          ) : (
            <VirtualList
              className="tree-list"
              estimateSize={34}
              gap={0}
              getKey={(row) => row.id}
              items={rows()}
              maxHeight="100%"
              renderItem={(row) => (
                <TreeRowView
                  locale={locale()}
                  row={row}
                  search={search()}
                  selected={row.kind === "file" && row.file.uri === selectedFile()?.uri}
                  text={text}
                  collapsed={isCollapsibleTreeRow(row) && collapsedTreeIds().has(row.id)}
                  onSelect={() => {
                    if (row.kind === "file") {
                      setSelectedUri(row.file.uri);
                    } else {
                      toggleTreeRow(row.id);
                    }
                  }}
                  onContextMenu={(event) => openTreeContextMenu(row, event)}
                />
              )}
              threshold={80}
            />
          )}
        </section>
        <aside class="side-pane">
          <section class="panel-section overview-section">
            <h2>{text("analysisOverview")}</h2>
            <div class="overview-grid">
              <MetricCard
                detail={`ASP: ${summary().aspFiles} · INC: ${summary().incFiles} · ASA: ${summary().asaFiles}`}
                label={text("files")}
                value={formatNumber(payload().stats.files, locale())}
              />
              <MetricCard
                label={text("foldersScanned")}
                value={formatNumber(summary().folders, locale())}
              />
              <MetricCard
                label={text("totalSize")}
                value={formatBytes(payload().stats.totalBytes)}
              />
              <MetricCard
                detail={
                  summary().latestModifiedMs > 0
                    ? formatDate(summary().latestModifiedMs, locale())
                    : ""
                }
                label={text("lastScanned")}
                value={
                  summary().latestModifiedMs > 0
                    ? formatDateShort(summary().latestModifiedMs, locale())
                    : "-"
                }
              />
            </div>
          </section>
          <section class="panel-section">
            <h2>{text("currentFilters")}</h2>
            <GlobChips
              label={text("includeGlobs")}
              none={text("none")}
              stats={payload().globStats?.include}
              text={text}
              values={includeList()}
            />
            <GlobChips
              label={text("excludeGlobs")}
              none={text("none")}
              stats={payload().globStats?.exclude}
              text={text}
              tone="danger"
              values={excludeList()}
            />
          </section>
          <section class="panel-section selected-section">
            <h2>{text("selectedFile")}</h2>
            {selectedFile() ? (
              <dl class="details-list">
                <dt>{text("type")}</dt>
                <dd>{fileType(selectedFile())}</dd>
                <dt>{text("size")}</dt>
                <dd>{formatBytes(selectedFile().size)}</dd>
                <dt>{text("lastModified")}</dt>
                <dd>{formatDate(selectedFile().mtimeMs, locale())}</dd>
                <dt>{text("relativePath")}</dt>
                <dd>{selectedFile().displayPath ?? selectedFile().relativePath}</dd>
                <dt>{text("fullPath")}</dt>
                <dd>{selectedFile().fileName}</dd>
              </dl>
            ) : (
              <p class="muted">{text("noSelection")}</p>
            )}
            {selectedFile() ? (
              <div class="selected-actions">
                <button
                  type="button"
                  onClick={() => vscode.postMessage({ type: "openFile", uri: selectedFile().uri })}
                >
                  {text("open")}
                </button>
                <button
                  type="button"
                  class="primary-button"
                  onClick={exportSelectedExcel}
                  disabled={busy()}
                >
                  {text("action.export")}
                </button>
              </div>
            ) : null}
          </section>
        </aside>
      </main>
      {contextMenu() ? (
        <div
          class="context-menu fixed z-50 min-w-[220px] rounded-md border border-[var(--asp-lsp-border)] bg-[var(--asp-lsp-panel)] p-1 shadow-[0_14px_30px_rgb(0_0_0_/_35%)]"
          style={webviewStyle({ left: contextMenu()!.x, top: contextMenu()!.y })}
          onMouseDown={(event) => event.stopPropagation()}
          role="menu"
        >
          <button
            type="button"
            class="w-full justify-start border-0 bg-transparent px-2 py-1 text-left text-xs text-[var(--asp-lsp-text-strong)] hover:bg-[var(--vscode-list-hoverBackground)]"
            onClick={() => addExcludeGlobPattern(contextMenu()!.pattern)}
            role="menuitem"
          >
            {text("action.excludePattern", { pattern: contextMenu()!.pattern })}
          </button>
        </div>
      ) : null}
    </div>
  );
}
const workspaceFilesErrorLocale = initialWorkspaceFilesPayload.locale === "ja" ? "ja" : "en";
render(
  () => (
    <WebviewErrorBoundary
      title={
        workspaceFilesErrorLocale === "ja"
          ? "ワークスペースファイルの表示に失敗しました"
          : "Workspace files failed to render"
      }
    >
      <App />
    </WebviewErrorBoundary>
  ),
  document.getElementById("root") ?? document.body,
);
