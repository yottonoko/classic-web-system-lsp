import React, { useEffect, useMemo, useState } from "react";
import { createRoot } from "react-dom/client";
import styles from "./settings.css?inline";
import { cn } from "../lib/utils";
import type {
  AspSettingsTarget,
  AspSettingsWebviewPayload,
} from "../settings-webview";
import type {
  AspSettingsLocale,
  SettingsMetadata,
  SettingsTargetScope,
  SettingsValueState,
} from "../settings-metadata";

declare const acquireVsCodeApi: () => {
  postMessage(message: unknown): void;
};

declare global {
  interface Window {
    __ASP_LSP_SETTINGS__?: AspSettingsWebviewPayload;
  }
}

type DraftEntry = { kind: "reset" } | { kind: "value"; value: unknown };
type Drafts = Record<string, DraftEntry | undefined>;
type JsonErrors = Record<string, string | undefined>;
type SettingsUpdate = { key: string; reset?: boolean; value?: unknown };
type HostMessage =
  | { error?: string; payload?: AspSettingsWebviewPayload; requestId?: string; type: "saveResult" }
  | {
      error?: string;
      payload?: AspSettingsWebviewPayload;
      requestId?: string;
      type: "settingsPayload";
    };

const vscode = acquireVsCodeApi();
const initialPayload = window.__ASP_LSP_SETTINGS__;
const identifierCaseKinds = [
  "variable",
  "parameter",
  "class",
  "function",
  "sub",
  "constant",
  "field",
  "property",
  "method",
];
const identifierCaseValues = [
  "PascalCase",
  "UPPERCASE",
  "camelCase",
  "lowercase",
  "snake_case",
  "UPPER_SNAKE",
  "ignore",
];

const messages: Record<AspSettingsLocale, Record<string, string>> = {
  en: {
    allCategories: "All",
    arrayEmpty: "No entries",
    cancelReset: "Cancel reset",
    defaultValue: "Default",
    dirty: "{count} unsaved",
    effective: "Effective",
    filterPlaceholder: "Search settings",
    folder: "Folder",
    global: "Global",
    inheritedFrom: "Inherited from {source}",
    jsonInvalid: "Invalid JSON: {error}",
    noMatches: "No settings match the current search.",
    reset: "Reset in this scope",
    resetPending: "Reset pending",
    save: "Save changes",
    saved: "Saved",
    saving: "Saving...",
    scope: "Scope",
    selectFolder: "Select folder",
    target: "Target value",
    title: "Classic ASP settings",
    workspace: "Workspace",
    workspaceFolder: "Folder",
    workspaceFolderUnavailable: "Open a workspace folder to use folder settings.",
    workspaceUnavailable: "Open a workspace to use workspace settings.",
  },
  ja: {
    allCategories: "すべて",
    arrayEmpty: "項目なし",
    cancelReset: "reset を取り消す",
    defaultValue: "既定値",
    dirty: "未保存 {count} 件",
    effective: "有効値",
    filterPlaceholder: "設定を検索",
    folder: "フォルダー",
    global: "グローバル",
    inheritedFrom: "{source} から継承",
    jsonInvalid: "JSON が不正です: {error}",
    noMatches: "現在の検索に一致する設定はありません。",
    reset: "この scope の値を削除",
    resetPending: "reset 待ち",
    save: "変更を保存",
    saved: "保存済み",
    saving: "保存中...",
    scope: "scope",
    selectFolder: "folder を選択",
    target: "保存対象の値",
    title: "Classic ASP 設定",
    workspace: "ワークスペース",
    workspaceFolder: "フォルダー",
    workspaceFolderUnavailable: "folder 設定を使うには workspace folder を開いてください。",
    workspaceUnavailable: "workspace 設定を使うには workspace を開いてください。",
  },
};

function App(): React.ReactElement {
  const [payload, setPayload] = useState<AspSettingsWebviewPayload>(
    initialPayload ?? emptyPayload(),
  );
  const locale = payload.locale === "ja" ? "ja" : "en";
  const [activeCategory, setActiveCategory] = useState("all");
  const [search, setSearch] = useState("");
  const [drafts, setDrafts] = useState<Drafts>({});
  const [jsonErrors, setJsonErrors] = useState<JsonErrors>({});
  const [status, setStatus] = useState<string | undefined>();
  const [saving, setSaving] = useState(false);
  const settingsByKey = useMemo(
    () => new Map(payload.settings.map((setting) => [setting.key, setting])),
    [payload.settings],
  );
  const dirtyKeys = useMemo(
    () =>
      payload.settings
        .filter((setting) => draftIsDirty(setting, drafts[setting.key], payload.values[setting.key]))
        .map((setting) => setting.key),
    [drafts, payload.settings, payload.values],
  );
  const filteredSettings = useMemo(() => {
    const normalizedSearch = search.trim().toLowerCase();
    return payload.settings.filter((setting) => {
      const categoryMatches = activeCategory === "all" || setting.category === activeCategory;
      const searchMatches =
        !normalizedSearch ||
        `${setting.key} ${setting.title} ${setting.description}`.toLowerCase().includes(
          normalizedSearch,
        );
      return categoryMatches && searchMatches;
    });
  }, [activeCategory, payload.settings, search]);
  const selectedSetting = filteredSettings[0] ?? payload.settings[0];
  const hasJsonErrors = Object.values(jsonErrors).some(Boolean);
  const target = payload.selectedScope;

  useEffect(() => {
    const listener = (event: MessageEvent<HostMessage>) => {
      const message = event.data;
      if (message.type === "settingsPayload" && message.payload) {
        setPayload(message.payload);
        setStatus(undefined);
      } else if (message.type === "saveResult") {
        setSaving(false);
        if (message.payload) {
          setPayload(message.payload);
          setDrafts({});
          setJsonErrors({});
          setStatus(text(locale, "saved"));
        } else {
          setStatus(message.error);
        }
      }
    };
    window.addEventListener("message", listener);
    return () => window.removeEventListener("message", listener);
  }, [locale]);

  function updateDraft(key: string, value: unknown): void {
    setDrafts((current) => ({ ...current, [key]: { kind: "value", value } }));
    setJsonErrors((current) => ({ ...current, [key]: undefined }));
  }

  function updateJsonError(key: string, error: string | undefined): void {
    setJsonErrors((current) => ({ ...current, [key]: error }));
  }

  function resetDraft(key: string): void {
    setDrafts((current) => ({ ...current, [key]: { kind: "reset" } }));
    setJsonErrors((current) => ({ ...current, [key]: undefined }));
  }

  function clearDraft(key: string): void {
    setDrafts((current) => ({ ...current, [key]: undefined }));
    setJsonErrors((current) => ({ ...current, [key]: undefined }));
  }

  function reload(nextTarget: AspSettingsTarget): void {
    setPayload((current) => ({ ...current, selectedScope: nextTarget }));
    setDrafts({});
    setJsonErrors({});
    setStatus(undefined);
    vscode.postMessage({
      type: "reloadSettings",
      requestId: createRequestId(),
      target: nextTarget,
    });
  }

  function save(): void {
    if (dirtyKeys.length === 0 || hasJsonErrors || saving) {
      return;
    }
    const updates: SettingsUpdate[] = dirtyKeys.map((key) => {
      const draft = drafts[key];
      return draft?.kind === "reset" ? { key, reset: true } : { key, value: draft?.value };
    });
    setSaving(true);
    setStatus(undefined);
    vscode.postMessage({
      type: "saveSettings",
      requestId: createRequestId(),
      target,
      updates,
    });
  }

  return (
    <main
      className="asp-lsp-settings-shell flex h-full min-h-0 flex-col bg-[#101419] text-[#d9e0ea]"
      data-asp-lsp-theme={themeForPayload(payload)}
    >
      <style>{styles}</style>
      <header className="flex min-h-[72px] items-center justify-between border-b border-[#263140] bg-[#151b23] px-5">
        <div>
          <h1 className="text-lg font-semibold text-[#f1f5f9]">{text(locale, "title")}</h1>
          <div className="mt-1 flex flex-wrap items-center gap-2 text-xs text-[#9fb0c5]">
            <span>{payload.settings.length} settings</span>
            <span>{scopeLabel(locale, target.scope)}</span>
            {dirtyKeys.length > 0 ? (
              <span className="rounded border border-[#334255] px-2 py-0.5 text-[#f6c177]">
                {formatText(locale, "dirty", { count: dirtyKeys.length })}
              </span>
            ) : null}
          </div>
        </div>
        <div className="flex items-center gap-3">
          {status ? <span className="max-w-[320px] truncate text-xs text-[#9fb0c5]">{status}</span> : null}
          <button
            className="rounded border border-[#7dd3fc] bg-[#17324a] px-3 py-2 text-sm font-medium text-[#f1f5f9] disabled:cursor-not-allowed disabled:opacity-50"
            disabled={dirtyKeys.length === 0 || hasJsonErrors || saving}
            onClick={save}
            type="button"
          >
            {saving ? text(locale, "saving") : text(locale, "save")}
          </button>
        </div>
      </header>
      <section className="grid min-h-0 flex-1 grid-cols-[260px_minmax(420px,1fr)_minmax(340px,420px)] overflow-hidden max-[1050px]:grid-cols-[220px_minmax(360px,1fr)]">
        <Sidebar
          activeCategory={activeCategory}
          dirtyKeys={dirtyKeys}
          locale={locale}
          onCategoryChange={setActiveCategory}
          payload={payload}
          search={search}
          setSearch={setSearch}
        />
        <div className="settings-scrollbar min-h-0 overflow-auto border-r border-[#263140]">
          <ScopePicker locale={locale} onChange={reload} payload={payload} target={target} />
          <div className="space-y-3 p-4">
            {filteredSettings.length === 0 ? (
              <div className="rounded border border-[#263140] bg-[#101820] p-5 text-sm text-[#9fb0c5]">
                {text(locale, "noMatches")}
              </div>
            ) : (
              filteredSettings.map((setting) => (
                <SettingRow
                  clearDraft={clearDraft}
                  draft={drafts[setting.key]}
                  jsonError={jsonErrors[setting.key]}
                  key={setting.key}
                  locale={locale}
                  metadata={setting}
                  onJsonError={updateJsonError}
                  onReset={resetDraft}
                  onUpdate={updateDraft}
                  state={payload.values[setting.key]}
                />
              ))
            )}
          </div>
        </div>
        <PreviewPanel
          drafts={drafts}
          locale={locale}
          selectedSetting={selectedSetting}
          settingsByKey={settingsByKey}
          values={payload.values}
        />
      </section>
    </main>
  );
}

function Sidebar(props: {
  activeCategory: string;
  dirtyKeys: string[];
  locale: AspSettingsLocale;
  onCategoryChange(category: string): void;
  payload: AspSettingsWebviewPayload;
  search: string;
  setSearch(value: string): void;
}): React.ReactElement {
  const dirtyCategories = new Map<string, number>();
  for (const key of props.dirtyKeys) {
    const setting = props.payload.settings.find((candidate) => candidate.key === key);
    if (setting) {
      dirtyCategories.set(setting.category, (dirtyCategories.get(setting.category) ?? 0) + 1);
    }
  }
  return (
    <aside className="flex min-h-0 flex-col border-r border-[#263140] bg-[#151b23]">
      <div className="border-b border-[#263140] p-3">
        <input
          className="w-full rounded border border-[#263140] bg-[#0c1117] px-3 py-2 text-sm text-[#d9e0ea] outline-none focus:border-[#7dd3fc]"
          onChange={(event) => props.setSearch(event.currentTarget.value)}
          placeholder={text(props.locale, "filterPlaceholder")}
          type="search"
          value={props.search}
        />
      </div>
      <nav className="settings-scrollbar min-h-0 flex-1 space-y-1 overflow-auto p-2">
        <CategoryButton
          active={props.activeCategory === "all"}
          dirtyCount={props.dirtyKeys.length}
          label={text(props.locale, "allCategories")}
          onClick={() => props.onCategoryChange("all")}
          total={props.payload.settings.length}
        />
        {props.payload.categories.map((category) => (
          <CategoryButton
            active={props.activeCategory === category.id}
            dirtyCount={dirtyCategories.get(category.id) ?? 0}
            key={category.id}
            label={category.label}
            onClick={() => props.onCategoryChange(category.id)}
            total={category.total}
          />
        ))}
      </nav>
    </aside>
  );
}

function CategoryButton(props: {
  active: boolean;
  dirtyCount: number;
  label: string;
  onClick(): void;
  total: number;
}): React.ReactElement {
  return (
    <button
      className={cn(
        "flex w-full items-center justify-between rounded px-3 py-2 text-left text-sm hover:bg-[#172131]",
        props.active ? "bg-[#17324a] text-[#f1f5f9]" : "text-[#d9e0ea]",
      )}
      onClick={props.onClick}
      type="button"
    >
      <span className="truncate">{props.label}</span>
      <span className="ml-2 flex items-center gap-1 text-xs text-[#9fb0c5]">
        {props.dirtyCount > 0 ? <span className="text-[#f6c177]">{props.dirtyCount}</span> : null}
        <span>{props.total}</span>
      </span>
    </button>
  );
}

function ScopePicker(props: {
  locale: AspSettingsLocale;
  onChange(target: AspSettingsTarget): void;
  payload: AspSettingsWebviewPayload;
  target: AspSettingsTarget;
}): React.ReactElement {
  const folderDisabled = !props.payload.scopes.workspaceFolderAvailable;
  const workspaceDisabled = !props.payload.scopes.workspaceAvailable;
  return (
    <div className="sticky top-0 z-10 border-b border-[#263140] bg-[#101419]/95 p-4 backdrop-blur">
      <div className="mb-2 text-xs font-semibold uppercase tracking-wide text-[#8190a4]">
        {text(props.locale, "scope")}
      </div>
      <div className="flex flex-wrap gap-2">
        <ScopeButton
          active={props.target.scope === "global"}
          label={text(props.locale, "global")}
          onClick={() => props.onChange({ ...props.target, scope: "global" })}
        />
        <ScopeButton
          active={props.target.scope === "workspace"}
          disabled={workspaceDisabled}
          label={text(props.locale, "workspace")}
          onClick={() => props.onChange({ ...props.target, scope: "workspace" })}
          title={workspaceDisabled ? text(props.locale, "workspaceUnavailable") : undefined}
        />
        <ScopeButton
          active={props.target.scope === "workspaceFolder"}
          disabled={folderDisabled}
          label={text(props.locale, "workspaceFolder")}
          onClick={() => props.onChange({ ...props.target, scope: "workspaceFolder" })}
          title={folderDisabled ? text(props.locale, "workspaceFolderUnavailable") : undefined}
        />
        <select
          className="min-w-[180px] rounded border border-[#263140] bg-[#0c1117] px-3 py-2 text-sm text-[#d9e0ea] disabled:opacity-50"
          disabled={folderDisabled}
          onChange={(event) =>
            props.onChange({ ...props.target, folderUri: event.currentTarget.value })
          }
          value={props.target.folderUri ?? props.payload.scopes.folders[0]?.uri ?? ""}
        >
          {props.payload.scopes.folders.length === 0 ? (
            <option value="">{text(props.locale, "selectFolder")}</option>
          ) : (
            props.payload.scopes.folders.map((folder) => (
              <option key={folder.uri} value={folder.uri}>
                {folder.name}
              </option>
            ))
          )}
        </select>
      </div>
    </div>
  );
}

function ScopeButton(props: {
  active: boolean;
  disabled?: boolean;
  label: string;
  onClick(): void;
  title?: string;
}): React.ReactElement {
  return (
    <button
      className={cn(
        "rounded border px-3 py-2 text-sm disabled:cursor-not-allowed disabled:opacity-50",
        props.active
          ? "border-[#7dd3fc] bg-[#17324a] text-[#f1f5f9]"
          : "border-[#263140] bg-[#101820] text-[#d9e0ea] hover:bg-[#172131]",
      )}
      disabled={props.disabled}
      onClick={props.onClick}
      title={props.title}
      type="button"
    >
      {props.label}
    </button>
  );
}

function SettingRow(props: {
  clearDraft(key: string): void;
  draft: DraftEntry | undefined;
  jsonError: string | undefined;
  locale: AspSettingsLocale;
  metadata: SettingsMetadata;
  onJsonError(key: string, error: string | undefined): void;
  onReset(key: string): void;
  onUpdate(key: string, value: unknown): void;
  state: SettingsValueState | undefined;
}): React.ReactElement {
  const value = currentSettingValue(props.metadata, props.state, props.draft);
  const dirty = draftIsDirty(props.metadata, props.draft, props.state);
  return (
    <article
      className={cn(
        "rounded border bg-[#151b23] p-4",
        dirty ? "border-[#7dd3fc]" : "border-[#263140]",
      )}
    >
      <div className="mb-3 flex items-start justify-between gap-4">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <h2 className="break-words text-sm font-semibold text-[#f1f5f9]">{props.metadata.title}</h2>
            {props.metadata.tags.map((tag) => (
              <span className="rounded bg-[#101820] px-2 py-0.5 text-[11px] text-[#9fb0c5]" key={tag}>
                {tag}
              </span>
            ))}
            {props.draft?.kind === "reset" ? (
              <span className="rounded border border-[#334255] px-2 py-0.5 text-[11px] text-[#f6c177]">
                {text(props.locale, "resetPending")}
              </span>
            ) : null}
          </div>
          <code className="mt-1 block break-all text-xs text-[#7dd3fc]">{props.metadata.key}</code>
        </div>
        <div className="flex shrink-0 gap-2">
          {dirty ? (
            <button
              className="rounded border border-[#263140] px-2 py-1 text-xs text-[#9fb0c5] hover:bg-[#172131]"
              onClick={() => props.clearDraft(props.metadata.key)}
              type="button"
            >
              {props.draft?.kind === "reset"
                ? text(props.locale, "cancelReset")
                : text(props.locale, "saved")}
            </button>
          ) : null}
          <button
            className="rounded border border-[#263140] px-2 py-1 text-xs text-[#9fb0c5] hover:bg-[#172131]"
            onClick={() => props.onReset(props.metadata.key)}
            type="button"
          >
            {text(props.locale, "reset")}
          </button>
        </div>
      </div>
      <p className="mb-3 text-sm leading-6 text-[#9fb0c5]">{props.metadata.description}</p>
      <SettingControl
        jsonError={props.jsonError}
        locale={props.locale}
        metadata={props.metadata}
        onJsonError={props.onJsonError}
        onUpdate={props.onUpdate}
        value={value}
      />
      <div className="mt-3 grid grid-cols-3 gap-2 text-xs text-[#8190a4]">
        <ValueSummary
          label={text(props.locale, "effective")}
          value={props.state?.effectiveValue ?? props.metadata.defaultValue}
        />
        <ValueSummary
          label={formatText(props.locale, "inheritedFrom", {
            source: sourceLabel(props.locale, props.state?.inheritedFrom ?? "default"),
          })}
          value={props.state?.inheritedValue ?? props.metadata.defaultValue}
        />
        <ValueSummary label={text(props.locale, "defaultValue")} value={props.state?.defaultValue} />
      </div>
    </article>
  );
}

function SettingControl(props: {
  jsonError: string | undefined;
  locale: AspSettingsLocale;
  metadata: SettingsMetadata;
  onJsonError(key: string, error: string | undefined): void;
  onUpdate(key: string, value: unknown): void;
  value: unknown;
}): React.ReactElement {
  if (props.metadata.type === "boolean") {
    return (
      <label className="inline-flex items-center gap-3 rounded border border-[#263140] bg-[#101820] px-3 py-2 text-sm">
        <input
          checked={Boolean(props.value)}
          className="h-4 w-4 accent-[#7dd3fc]"
          onChange={(event) => props.onUpdate(props.metadata.key, event.currentTarget.checked)}
          type="checkbox"
        />
        <span>{String(Boolean(props.value))}</span>
      </label>
    );
  }
  if (props.metadata.type === "enum") {
    return (
      <select
        className="w-full rounded border border-[#263140] bg-[#0c1117] px-3 py-2 text-sm text-[#d9e0ea]"
        onChange={(event) => props.onUpdate(props.metadata.key, event.currentTarget.value)}
        value={String(props.value ?? props.metadata.defaultValue ?? "")}
      >
        {props.metadata.enumValues?.map((value) => (
          <option key={value} value={value}>
            {displayEnumValue(value)}
          </option>
        ))}
      </select>
    );
  }
  if (props.metadata.type === "number") {
    return (
      <input
        className="w-full rounded border border-[#263140] bg-[#0c1117] px-3 py-2 text-sm text-[#d9e0ea]"
        min={props.metadata.minimum}
        onChange={(event) => {
          const input = event.currentTarget.value;
          props.onUpdate(props.metadata.key, input === "" && props.metadata.nullable ? null : Number(input));
        }}
        type="number"
        value={props.value === null || props.value === undefined ? "" : String(props.value)}
      />
    );
  }
  if (props.metadata.type === "array") {
    return (
      <textarea
        className="min-h-[86px] w-full rounded border border-[#263140] bg-[#0c1117] px-3 py-2 font-mono text-sm text-[#d9e0ea]"
        onChange={(event) =>
          props.onUpdate(
            props.metadata.key,
            event.currentTarget.value
              .split("\n")
              .map((line) => line.trim())
              .filter(Boolean),
          )
        }
        value={Array.isArray(props.value) ? props.value.join("\n") : ""}
      />
    );
  }
  if (props.metadata.type === "object") {
    return (
      <ObjectSettingControl
        jsonError={props.jsonError}
        locale={props.locale}
        metadata={props.metadata}
        onJsonError={props.onJsonError}
        onUpdate={props.onUpdate}
        value={props.value}
      />
    );
  }
  return (
    <input
      className="w-full rounded border border-[#263140] bg-[#0c1117] px-3 py-2 text-sm text-[#d9e0ea]"
      onChange={(event) => props.onUpdate(props.metadata.key, event.currentTarget.value)}
      type="text"
      value={String(props.value ?? "")}
    />
  );
}

function ObjectSettingControl(props: {
  jsonError: string | undefined;
  locale: AspSettingsLocale;
  metadata: SettingsMetadata;
  onJsonError(key: string, error: string | undefined): void;
  onUpdate(key: string, value: unknown): void;
  value: unknown;
}): React.ReactElement {
  const [jsonText, setJsonText] = useState(jsonString(props.value));
  useEffect(() => setJsonText(jsonString(props.value)), [props.metadata.key, props.value]);
  const objectValue = isRecord(props.value) ? props.value : {};

  function updateObject(value: Record<string, unknown>): void {
    props.onJsonError(props.metadata.key, undefined);
    props.onUpdate(props.metadata.key, value);
  }

  return (
    <div className="space-y-3">
      {props.metadata.key === "aspLsp.vbscript.identifierCaseByKind" ? (
        <div className="grid grid-cols-2 gap-2">
          {identifierCaseKinds.map((kind) => (
            <label className="space-y-1 text-xs text-[#9fb0c5]" key={kind}>
              <span>{kind}</span>
              <select
                className="w-full rounded border border-[#263140] bg-[#0c1117] px-2 py-1.5 text-sm text-[#d9e0ea]"
                onChange={(event) => updateObject({ ...objectValue, [kind]: event.currentTarget.value })}
                value={String(objectValue[kind] ?? "ignore")}
              >
                {identifierCaseValues.map((value) => (
                  <option key={value} value={value}>
                    {value}
                  </option>
                ))}
              </select>
            </label>
          ))}
        </div>
      ) : props.metadata.key === "aspLsp.vbscript.globals" ? (
        <SampleObjectButtons
          label="Globals"
          onApply={() =>
            updateObject({
              ...objectValue,
              Application: { kind: "variable", type: "ASP.Application" },
              ConnString: { kind: "constant", type: "String" },
            })
          }
          value={objectValue}
        />
      ) : props.metadata.key === "aspLsp.vbscript.comTypes" ? (
        <SampleObjectButtons
          label="COM Types"
          onApply={() =>
            updateObject({
              ...objectValue,
              "ADODB.Connection": {
                members: {
                  Close: { kind: "method", returnType: "Void" },
                  ConnectionString: { kind: "property", type: "String" },
                },
              },
            })
          }
          value={objectValue}
        />
      ) : null}
      <textarea
        className={cn(
          "min-h-[150px] w-full rounded border bg-[#0c1117] px-3 py-2 font-mono text-xs leading-5 text-[#d9e0ea]",
          props.jsonError ? "border-[#ff9fb4]" : "border-[#263140]",
        )}
        onChange={(event) => {
          const nextText = event.currentTarget.value;
          setJsonText(nextText);
          try {
            const parsed = nextText.trim() ? (JSON.parse(nextText) as unknown) : {};
            props.onJsonError(props.metadata.key, undefined);
            props.onUpdate(props.metadata.key, parsed);
          } catch (error) {
            props.onJsonError(
              props.metadata.key,
              formatText(props.locale, "jsonInvalid", {
                error: error instanceof Error ? error.message : String(error),
              }),
            );
          }
        }}
        spellCheck={false}
        value={jsonText}
      />
      {props.jsonError ? <p className="text-xs text-[#ff9fb4]">{props.jsonError}</p> : null}
    </div>
  );
}

function SampleObjectButtons(props: {
  label: string;
  onApply(): void;
  value: Record<string, unknown>;
}): React.ReactElement {
  return (
    <div className="rounded border border-[#263140] bg-[#101820] p-3">
      <div className="mb-2 flex items-center justify-between gap-3">
        <span className="text-sm font-medium text-[#f1f5f9]">{props.label}</span>
        <button
          className="rounded border border-[#334255] px-2 py-1 text-xs text-[#9fb0c5] hover:bg-[#172131]"
          onClick={props.onApply}
          type="button"
        >
          sample を追加
        </button>
      </div>
      <div className="flex flex-wrap gap-2">
        {Object.keys(props.value).length === 0 ? (
          <span className="text-xs text-[#8190a4]">JSON または sample で追加</span>
        ) : (
          Object.keys(props.value)
            .slice(0, 8)
            .map((name) => (
              <span className="rounded bg-[#17324a] px-2 py-1 text-xs text-[#d9e0ea]" key={name}>
                {name}
              </span>
            ))
        )}
      </div>
    </div>
  );
}

function ValueSummary(props: { label: string; value: unknown }): React.ReactElement {
  return (
    <div className="min-w-0 rounded border border-[#263140] bg-[#101820] p-2">
      <div className="mb-1 truncate text-[11px] uppercase tracking-wide text-[#8190a4]">{props.label}</div>
      <code className="block truncate text-[11px] text-[#d9e0ea]">{compactValue(props.value)}</code>
    </div>
  );
}

function PreviewPanel(props: {
  drafts: Drafts;
  locale: AspSettingsLocale;
  selectedSetting: SettingsMetadata | undefined;
  settingsByKey: Map<string, SettingsMetadata>;
  values: Record<string, SettingsValueState>;
}): React.ReactElement {
  const settingValue = (key: string): unknown => {
    const metadata = props.settingsByKey.get(key);
    return metadata ? currentSettingValue(metadata, props.values[key], props.drafts[key]) : undefined;
  };
  const previewKind = props.selectedSetting?.previewKind ?? "general";
  return (
    <aside className="settings-scrollbar min-h-0 overflow-auto bg-[#101820] p-4 max-[1050px]:hidden">
      <div className="mb-3 text-xs font-semibold uppercase tracking-wide text-[#8190a4]">Preview</div>
      {previewKind === "code" ? (
        <CodePreview settingValue={settingValue} />
      ) : previewKind === "graph" ? (
        <GraphPreview settingValue={settingValue} />
      ) : previewKind === "flowchart" ? (
        <FlowchartPreview settingValue={settingValue} />
      ) : previewKind === "excel" ? (
        <ExcelPreview settingValue={settingValue} />
      ) : previewKind === "workspace" ? (
        <WorkspacePreview locale={props.locale} settingValue={settingValue} />
      ) : previewKind === "cache" ? (
        <CachePreview settingValue={settingValue} />
      ) : previewKind === "network" ? (
        <NetworkPreview settingValue={settingValue} />
      ) : previewKind === "memory" ? (
        <MemoryPreview settingValue={settingValue} />
      ) : previewKind === "iis" ? (
        <IisPreview settingValue={settingValue} />
      ) : (
        <GeneralPreview settingValue={settingValue} />
      )}
    </aside>
  );
}

function CodePreview(props: { settingValue(key: string): unknown }): React.ReactElement {
  const indentSize = numberValue(
    props.settingValue("aspLsp.format.indentSize"),
    numberValue(props.settingValue("editor.tabSize"), 2),
  );
  const useTabs =
    props.settingValue("aspLsp.format.indentStyle") === "tab" ||
    props.settingValue("editor.insertSpaces") === false;
  const indent = useTabs ? "\t" : " ".repeat(indentSize);
  const upper = props.settingValue("aspLsp.format.uppercaseKeywords") === true;
  const align = props.settingValue("aspLsp.format.alignAssignments") === true;
  const defaultLanguage = stringValue(props.settingValue("aspLsp.defaultLanguage"), "VBScript");
  const inlayVars = props.settingValue("aspLsp.inlayHints.variableTypes") === true;
  const inlayParams = props.settingValue("aspLsp.inlayHints.parameterNames") !== false;
  const codeLens = props.settingValue("aspLsp.codeLens.references") !== false;
  const jsCheck = props.settingValue("aspLsp.checkJs") === true;
  const kw = (value: string) => (upper ? value.toUpperCase() : value);
  const lines =
    defaultLanguage === "JScript"
      ? [
          '<%@ Language="JScript" %>',
          "<script runat=\"server\">",
          `${indent}function total(price, tax) {`,
          `${indent}${indent}var result = price + tax;${jsCheck ? " // checked" : ""}`,
          `${indent}${indent}return result;`,
          `${indent}}`,
          "</script>",
        ]
      : [
          "<%",
          codeLens ? "' 2 references" : "' CodeLens hidden",
          `${kw("Function")} Total(price, tax)${inlayParams ? "  ' price:, tax:" : ""}`,
          `${indent}${kw("Dim")} result${inlayVars ? "  ' As Variant" : ""}`,
          align
            ? `${indent}price  = price`
            : `${indent}price = price`,
          align ? `${indent}result = price + tax` : `${indent}result = price + tax`,
          `${indent}Total = result`,
          `${kw("End Function")}`,
          "%>",
        ];
  return <CodeBlock lines={lines} />;
}

function GraphPreview(props: { settingValue(key: string): unknown }): React.ReactElement {
  const nodeKeys = [
    "showRootNodes",
    "showFileNodes",
    "showFunctionNodes",
    "showSubNodes",
    "showClassNodes",
    "showMemberNodes",
    "showLocalVariableNodes",
    "showUnresolvedNodes",
  ];
  return (
    <PreviewCard title="Graph">
      <div className="mb-4 grid grid-cols-2 gap-2">
        {nodeKeys.map((name) => {
          const enabled = props.settingValue(`aspLsp.graph.${name}`) !== false;
          return <StatusChip enabled={enabled} key={name} label={name.replace("show", "")} />;
        })}
      </div>
      <LimitMeter label="documents" value={numberValue(props.settingValue("aspLsp.graph.maxDocuments"), 5000)} />
      <LimitMeter label="nodes" value={numberValue(props.settingValue("aspLsp.graph.maxNodes"), 5000)} />
      <LimitMeter
        label="include tree"
        value={numberValue(props.settingValue("aspLsp.graph.includeTreeMaxDocuments"), 256)}
      />
    </PreviewCard>
  );
}

function FlowchartPreview(props: { settingValue(key: string): unknown }): React.ReactElement {
  const labelMode = stringValue(props.settingValue("aspLsp.flowchart.labelMode"), "normal");
  const sourcePanel = props.settingValue("aspLsp.flowchart.showSourcePanel") !== false;
  return (
    <PreviewCard title="Flowchart">
      <div className="grid gap-2">
        {["Start", labelMode === "raw" ? "If Request.QueryString(\"id\") <> \"\"" : "check id", "Render", "End"].map(
          (label) => (
            <div className="rounded border border-[#334255] bg-[#151b23] px-3 py-2 text-sm" key={label}>
              {label}
            </div>
          ),
        )}
      </div>
      <div className="mt-3 rounded border border-[#263140] bg-[#101419] p-3 text-xs text-[#9fb0c5]">
        source panel: {String(sourcePanel)}
      </div>
    </PreviewCard>
  );
}

function ExcelPreview(props: { settingValue(key: string): unknown }): React.ReactElement {
  return (
    <PreviewCard title="Excel">
      <LimitMeter label="documents" value={numberValue(props.settingValue("aspLsp.excel.maxDocuments"), 8192)} />
      <LimitMeter
        label="include tree"
        value={numberValue(props.settingValue("aspLsp.excel.includeTreeMaxDocuments"), 1024)}
      />
      <StatusChip
        enabled={props.settingValue("aspLsp.excel.skipTypeInference") === true}
        label="skip type inference"
      />
    </PreviewCard>
  );
}

function WorkspacePreview(props: {
  locale: AspSettingsLocale;
  settingValue(key: string): unknown;
}): React.ReactElement {
  const includes = stringArray(props.settingValue("aspLsp.workspace.includes"));
  const excludes = stringArray(props.settingValue("aspLsp.workspace.excludes"));
  return (
    <PreviewCard title="Workspace">
      <GlobList label="include" locale={props.locale} values={includes} />
      <GlobList label="exclude" locale={props.locale} values={excludes} />
      <LimitMeter
        label="max index files"
        value={numberValue(props.settingValue("aspLsp.workspace.maxIndexFiles"), 5000)}
      />
      <StatusChip
        enabled={props.settingValue("aspLsp.workspace.respectGitIgnore") === true}
        label="respect .gitignore"
      />
    </PreviewCard>
  );
}

function CachePreview(props: { settingValue(key: string): unknown }): React.ReactElement {
  return (
    <PreviewCard title="Cache">
      <StatusChip enabled={props.settingValue("aspLsp.cache.enabled") !== false} label="disk cache" />
      <LimitMeter label="TTL hours" value={numberValue(props.settingValue("aspLsp.cache.ttlHours"), 336)} />
      <LimitMeter label="size MB" value={numberValue(props.settingValue("aspLsp.cache.maxSizeMb"), 128)} />
      <div className="mt-2 break-all text-xs text-[#9fb0c5]">
        {stringValue(props.settingValue("aspLsp.cache.directory"), "(temporary directory)")}
      </div>
    </PreviewCard>
  );
}

function NetworkPreview(props: { settingValue(key: string): unknown }): React.ReactElement {
  const profile = stringValue(props.settingValue("aspLsp.network.profile"), "auto");
  return (
    <PreviewCard title="Network">
      <div className="mb-3 rounded border border-[#7dd3fc] bg-[#17324a] px-3 py-2 text-sm text-[#f1f5f9]">
        {profile}
      </div>
      <LimitMeter
        label="stat TTL ms"
        value={numberValue(props.settingValue("aspLsp.network.statCacheTtlMs"), -1)}
      />
      <LimitMeter
        label="read concurrency"
        value={numberValue(props.settingValue("aspLsp.network.includeReadConcurrency"), 0)}
      />
      <StatusChip
        enabled={props.settingValue("aspLsp.network.caseResolution") === "fast"}
        label="fast case resolution"
      />
    </PreviewCard>
  );
}

function MemoryPreview(props: { settingValue(key: string): unknown }): React.ReactElement {
  return (
    <PreviewCard title="Memory">
      <LimitMeter
        label="cache bytes"
        value={numberValue(props.settingValue("aspLsp.memory.maxCacheBytes"), 536870912)}
      />
      <StatusChip
        enabled={props.settingValue("aspLsp.memory.debugTelemetry") === true}
        label="debug telemetry"
      />
    </PreviewCard>
  );
}

function IisPreview(props: { settingValue(key: string): unknown }): React.ReactElement {
  return (
    <PreviewCard title="IIS">
      {["iis", "iisExpress"].map((kind) => (
        <div className="mb-3 rounded border border-[#263140] bg-[#151b23] p-3" key={kind}>
          <div className="mb-1 text-xs uppercase tracking-wide text-[#8190a4]">{kind}</div>
          <div className="break-all text-sm text-[#f1f5f9]">
            {stringValue(props.settingValue(`aspLsp.${kind}.url`), "http://localhost/")}
          </div>
          <div className="mt-1 break-all text-xs text-[#9fb0c5]">
            {stringValue(props.settingValue(`aspLsp.${kind}.webRoot`), "(workspace)")}
          </div>
        </div>
      ))}
    </PreviewCard>
  );
}

function GeneralPreview(props: { settingValue(key: string): unknown }): React.ReactElement {
  return (
    <PreviewCard title="Classic ASP LSP">
      <div className="grid gap-2">
        <StatusLine label="locale" value={stringValue(props.settingValue("aspLsp.locale"), "auto")} />
        <StatusLine
          label="language"
          value={stringValue(props.settingValue("aspLsp.defaultLanguage"), "VBScript")}
        />
        <StatusLine
          label="encoding"
          value={stringValue(props.settingValue("aspLsp.legacyEncoding"), "auto")}
        />
        <StatusLine
          label="webview theme"
          value={stringValue(props.settingValue("aspLsp.webview.theme"), "auto")}
        />
      </div>
    </PreviewCard>
  );
}

function PreviewCard(props: { children: React.ReactNode; title: string }): React.ReactElement {
  return (
    <div className="rounded border border-[#263140] bg-[#151b23] p-4">
      <h2 className="mb-3 text-sm font-semibold text-[#f1f5f9]">{props.title}</h2>
      {props.children}
    </div>
  );
}

function CodeBlock(props: { lines: string[] }): React.ReactElement {
  return (
    <pre className="overflow-auto rounded border border-[#263140] bg-[#0c1117] p-4 text-xs leading-6 text-[#d9e0ea]">
      {props.lines.map((line, index) => (
        <div key={`${index}-${line}`}>{highlightCodeLine(line)}</div>
      ))}
    </pre>
  );
}

function highlightCodeLine(line: string): React.ReactNode {
  if (line.trimStart().startsWith("'") || line.trimStart().startsWith("//")) {
    return <span className="settings-code-comment">{line}</span>;
  }
  const pieces = line.split(/("(?:[^"]|"")*"|Function|End Function|Dim|If|Then|End If|Return|function|var|return)/g);
  return pieces.map((piece, index) => {
    if (/^".*"$/.test(piece)) {
      return (
        <span className="settings-code-string" key={index}>
          {piece}
        </span>
      );
    }
    if (/^(Function|End Function|Dim|If|Then|End If|Return|function|var|return)$/i.test(piece)) {
      return (
        <span className="settings-code-keyword" key={index}>
          {piece}
        </span>
      );
    }
    return piece;
  });
}

function LimitMeter(props: { label: string; value: number }): React.ReactElement {
  const width = `${Math.max(8, Math.min(100, props.value <= 0 ? 8 : Math.log10(props.value + 10) * 18))}%`;
  return (
    <div className="mb-3">
      <div className="mb-1 flex justify-between text-xs text-[#9fb0c5]">
        <span>{props.label}</span>
        <span>{props.value.toLocaleString()}</span>
      </div>
      <div className="h-2 rounded bg-[#101419]">
        <div className="h-2 rounded bg-[#7dd3fc]" style={{ width }} />
      </div>
    </div>
  );
}

function StatusChip(props: { enabled: boolean; label: string }): React.ReactElement {
  return (
    <span
      className={cn(
        "inline-flex items-center rounded border px-2 py-1 text-xs",
        props.enabled
          ? "border-[#7dd3fc] bg-[#17324a] text-[#f1f5f9]"
          : "border-[#263140] bg-[#101419] text-[#8190a4]",
      )}
    >
      {props.label}
    </span>
  );
}

function StatusLine(props: { label: string; value: string }): React.ReactElement {
  return (
    <div className="flex justify-between gap-3 rounded border border-[#263140] bg-[#101820] px-3 py-2 text-sm">
      <span className="text-[#9fb0c5]">{props.label}</span>
      <span className="text-[#f1f5f9]">{props.value}</span>
    </div>
  );
}

function GlobList(props: {
  label: string;
  locale: AspSettingsLocale;
  values: string[];
}): React.ReactElement {
  return (
    <div className="mb-3">
      <div className="mb-2 text-xs uppercase tracking-wide text-[#8190a4]">{props.label}</div>
      <div className="flex flex-wrap gap-2">
        {props.values.length === 0 ? (
          <span className="text-xs text-[#8190a4]">{text(props.locale, "arrayEmpty")}</span>
        ) : (
          props.values.map((value) => (
            <code className="rounded bg-[#101419] px-2 py-1 text-xs text-[#d9e0ea]" key={value}>
              {value}
            </code>
          ))
        )}
      </div>
    </div>
  );
}

function currentSettingValue(
  metadata: SettingsMetadata,
  state: SettingsValueState | undefined,
  draft: DraftEntry | undefined,
): unknown {
  if (draft?.kind === "reset") {
    return state?.inheritedValue ?? metadata.defaultValue;
  }
  if (draft?.kind === "value") {
    return draft.value;
  }
  return state?.effectiveValue ?? metadata.defaultValue;
}

function draftIsDirty(
  _metadata: SettingsMetadata,
  draft: DraftEntry | undefined,
  state: SettingsValueState | undefined,
): boolean {
  if (!draft) {
    return false;
  }
  if (draft.kind === "reset") {
    return state?.targetDefined === true;
  }
  const base = state?.targetDefined ? state.targetValue : state?.inheritedValue;
  return !deepEqual(draft.value, base);
}

function emptyPayload(): AspSettingsWebviewPayload {
  return {
    categories: [],
    locale: "en",
    scopes: { folders: [], workspaceAvailable: false, workspaceFolderAvailable: false },
    selectedScope: { scope: "global" },
    settings: [],
    theme: "auto",
    values: {},
  };
}

function themeForPayload(payload: AspSettingsWebviewPayload): "dark" | "light" {
  if (payload.theme === "light") {
    return "light";
  }
  return "dark";
}

function text(locale: AspSettingsLocale, key: string): string {
  return messages[locale][key] ?? messages.en[key] ?? key;
}

function formatText(
  locale: AspSettingsLocale,
  key: string,
  params: Record<string, string | number>,
): string {
  let value = text(locale, key);
  for (const [name, replacement] of Object.entries(params)) {
    value = value.replaceAll(`{${name}}`, String(replacement));
  }
  return value;
}

function scopeLabel(locale: AspSettingsLocale, scope: SettingsTargetScope): string {
  if (scope === "workspaceFolder") {
    return text(locale, "workspaceFolder");
  }
  if (scope === "workspace") {
    return text(locale, "workspace");
  }
  return text(locale, "global");
}

function sourceLabel(locale: AspSettingsLocale, source: string): string {
  if (source === "workspaceFolder") {
    return text(locale, "workspaceFolder");
  }
  if (source === "workspace") {
    return text(locale, "workspace");
  }
  if (source === "global") {
    return text(locale, "global");
  }
  return text(locale, "defaultValue");
}

function displayEnumValue(value: string): string {
  if (value === "\n") {
    return "\\n";
  }
  if (value === "\r\n") {
    return "\\r\\n";
  }
  return value;
}

function createRequestId(): string {
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
}

function deepEqual(left: unknown, right: unknown): boolean {
  return JSON.stringify(left) === JSON.stringify(right);
}

function jsonString(value: unknown): string {
  return JSON.stringify(value ?? {}, null, 2);
}

function compactValue(value: unknown): string {
  if (value === undefined) {
    return "undefined";
  }
  if (value === null) {
    return "null";
  }
  if (typeof value === "string") {
    return displayEnumValue(value);
  }
  return JSON.stringify(value);
}

function numberValue(value: unknown, fallback: number): number {
  return typeof value === "number" && Number.isFinite(value) ? value : fallback;
}

function stringValue(value: unknown, fallback: string): string {
  return typeof value === "string" && value.length > 0 ? value : fallback;
}

function stringArray(value: unknown): string[] {
  return Array.isArray(value) ? value.filter((item): item is string => typeof item === "string") : [];
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

createRoot(document.getElementById("root")!).render(<App />);
