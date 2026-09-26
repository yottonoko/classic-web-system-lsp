import { createMemo, For } from "solid-js";
import { type JSX } from "@solidjs/web";
import { webviewStyle } from "./webview-dom-types";
import { ImeSafeInput, imeSafeKeyboardEventIsComposing } from "./ime-safe-input";
import { cn } from "../lib/utils";
import {
  fileType,
  formatBytes,
  formatDateShort,
  globCountText,
  globStatCount,
  highlightRanges,
  isCollapsibleTreeRow,
} from "./workspace-files-model";
import type { GlobInputItem, GlobKind, Locale, TextKey, TreeRow } from "./workspace-files-types";
import type { WorkspaceFilesGlobStat } from "../workspace-files-webview";
export function MetricCard(props: { detail?: string; label: string; value: string }): JSX.Element {
  return (
    <div class="metric-card">
      <span>{props.label}</span>
      <strong>{props.value}</strong>
      {props.detail ? <small>{props.detail}</small> : null}
    </div>
  );
}
export function GlobEditor(props: {
  items: GlobInputItem[];
  kind: GlobKind;
  label: string;
  stats: WorkspaceFilesGlobStat[] | undefined;
  text(key: TextKey, params?: Record<string, string | number>): string;
  onAdd(): void;
  onChange(id: string, value: string): void;
  onCommit(id: string, value: string): void;
  onRemove(id: string): void;
}): JSX.Element {
  return (
    <section class="glob-editor">
      <div class="glob-editor-heading">
        <span>{props.label}</span>
        <button type="button" onClick={props.onAdd}>
          {props.text("action.addGlob")}
        </button>
      </div>
      <div class="glob-editor-list">
        <For each={props.items} keyed={(item) => item.id}>
          {(item, index) => (
            <div class={cn("glob-row", props.kind)}>
              <ImeSafeInput
                aria-label={`${props.label} ${index() + 1}`}
                value={item().value}
                onValueChange={(value) => props.onChange(item().id, value)}
                onBlur={(event) => props.onCommit(item().id, event.currentTarget.value)}
                onKeyDown={(event) => {
                  if (event.key !== "Enter" || imeSafeKeyboardEventIsComposing(event)) {
                    return;
                  }
                  props.onCommit(item().id, event.currentTarget.value);
                  event.currentTarget.blur();
                }}
                spellcheck={false}
              />
              <span class="glob-count">
                {globCountText(globStatCount(props.stats, index(), item().value), props.text)}
              </span>
              <button
                type="button"
                class="icon-button"
                title={props.text("action.removeGlob")}
                onClick={() => props.onRemove(item().id)}
              >
                x
              </button>
            </div>
          )}
        </For>
      </div>
    </section>
  );
}
export function GlobChips(props: {
  label: string;
  none: string;
  stats: WorkspaceFilesGlobStat[] | undefined;
  text(key: TextKey, params?: Record<string, string | number>): string;
  tone?: "danger" | "default";
  values: string[];
}): JSX.Element {
  return (
    <div class="filter-chip-row">
      <span>{props.label}:</span>
      <div>
        {props.values.length === 0 ? (
          <em>{props.none}</em>
        ) : (
          props.values.map((value, index) => (
            <code class={cn("filter-chip", props.tone ?? "default")}>
              {value}
              <span>{globCountText(globStatCount(props.stats, index, value), props.text)}</span>
            </code>
          ))
        )}
      </div>
    </div>
  );
}
export function TreeRowView(props: {
  collapsed: boolean;
  locale: Locale;
  row: TreeRow;
  search: string;
  selected: boolean;
  text(key: TextKey, params?: Record<string, string | number>): string;
  onContextMenu(event: MouseEvent): void;
  onSelect(): void;
}): JSX.Element {
  const className = createMemo(() =>
    cn(
      "tree-row",
      props.row.kind,
      props.selected && "selected",
      !props.selected && !props.row.matchesFilter && "opacity-50",
    ),
  );
  const collapsible = createMemo(() => isCollapsibleTreeRow(props.row));
  return (
    <button
      type="button"
      aria-expanded={
        (collapsible() ? !props.collapsed : undefined) == null
          ? undefined
          : (collapsible() ? !props.collapsed : undefined)
            ? "true"
            : "false"
      }
      class={className()}
      onContextMenu={props.onContextMenu}
      onClick={props.onSelect}
      title={props.row.detail ?? props.row.label}
    >
      <span
        class="tree-name"
        style={webviewStyle({ paddingLeft: `${10 + props.row.depth * 18}px` })}
      >
        <span class="tree-disclosure" aria-hidden="true">
          {collapsible() ? (props.collapsed ? "+" : "-") : ""}
        </span>
        <span class="tree-icon" aria-hidden="true">
          {props.row.kind === "file"
            ? fileType(props.row.file)
            : props.row.kind === "folder"
              ? "/"
              : "WS"}
        </span>
        <span class="tree-label">
          <HighlightedText query={props.search} text={props.row.label} />
        </span>
      </span>
      <span class="tree-type">
        {props.row.kind === "file"
          ? fileType(props.row.file)
          : props.row.kind === "folder"
            ? props.text("folder")
            : props.text("workspace")}
      </span>
      <span class="tree-size">
        {props.row.kind === "file" ? formatBytes(props.row.file.size) : "-"}
      </span>
      <span class="tree-modified">
        {props.row.kind === "file" ? formatDateShort(props.row.file.mtimeMs, props.locale) : "-"}
      </span>
    </button>
  );
}
function HighlightedText(props: { query: string; text: string }): JSX.Element {
  const parts = createMemo(() => {
    const query = props.query.trim().toLowerCase();
    if (!query) return props.text;
    const ranges = highlightRanges(props.text, query);
    const result: JSX.Element[] = [];
    let cursor = 0;
    for (const [start, end] of ranges) {
      if (cursor < start) result.push(props.text.slice(cursor, start));
      result.push(<mark class="tree-match">{props.text.slice(start, end)}</mark>);
      cursor = end;
    }
    if (cursor < props.text.length) result.push(props.text.slice(cursor));
    return result;
  });
  return <>{parts()}</>;
}
