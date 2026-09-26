import { createSignal, createMemo, createEffect } from "solid-js";
import { type JSX } from "@solidjs/web";
import { webviewStyle } from "./webview-dom-types";
import type { AspFlowchartLabelMode } from "../protocol-types";
import { flowchartLabelModeTitleSuffix } from "./flowchart-model";
import { useElementSize } from "./flowchart-dom";
import { cn } from "../lib/utils";
import type {
  FlowchartToolbarMenuKind,
  FlowchartToolbarMenuState,
  FlowchartToolbarMode,
} from "./flowchart-types";
const flowchartLabelModes: AspFlowchartLabelMode[] = ["raw", "normal", "description"];
export function FlowchartToolbar(props: {
  canExportSvg: boolean;
  canFitFlowchartWidth: boolean;
  canOpenSection: boolean;
  labelMode: AspFlowchartLabelMode;
  text(key: string): string;
  zoom: number;
  onLabelModeChange(mode: AspFlowchartLabelMode): void;
  onCopyMermaid(): void;
  onExportMermaid(): void;
  onExportSvg(): void;
  onFitFlowchartWidth(): void;
  onOpenCode(): void;
  onResetZoom(): void;
  sourcePanelVisible: boolean;
  onSourcePanelVisibleChange(value: boolean): void;
  onZoomIn(): void;
  onZoomOut(): void;
}): JSX.Element {
  const [toolbarRef, toolbarSize] = useElementSize<HTMLDivElement>();
  const menuRef = { current: null } as {
    current: (HTMLDivElement | null) | null;
  };
  const [menu, setMenu] = createSignal<FlowchartToolbarMenuState>();
  const toolbarMode = createMemo(() => flowchartToolbarMode(toolbarSize().width));
  const compactExports = createMemo(
    () => toolbarMode() === "compactExports" || toolbarMode() === "compactAll",
  );
  const compactAll = createMemo(() => toolbarMode() === "compactAll");
  const closeMenu = () => setMenu(undefined);
  const openMenu = (kind: FlowchartToolbarMenuKind, button: HTMLButtonElement) => {
    if (!button.isConnected) {
      return;
    }
    const rect = button.getBoundingClientRect();
    if (!Number.isFinite(rect.left) || !Number.isFinite(rect.bottom)) {
      return;
    }
    setMenu({
      kind,
      left: Math.max(8, Math.min(rect.left, window.innerWidth - flowchartToolbarMenuWidth - 8)),
      top: Math.min(rect.bottom + 6, window.innerHeight - 8),
    });
  };
  const runMenuAction = (action: () => void) => {
    action();
    closeMenu();
  };
  createEffect(
    () => [closeMenu, menu(), toolbarRef],
    () => {
      if (!menu()) {
        return undefined;
      }
      const closeOnEscape = (event: KeyboardEvent): void => {
        if (event.key === "Escape") {
          closeMenu();
        }
      };
      const closeOnOutsidePointerDown = (event: PointerEvent): void => {
        const target = event.target;
        if (!(target instanceof Node)) {
          closeMenu();
          return;
        }
        if (toolbarRef.current?.contains(target) || menuRef.current?.contains(target)) {
          return;
        }
        closeMenu();
      };
      window.addEventListener("keydown", closeOnEscape);
      window.addEventListener("pointerdown", closeOnOutsidePointerDown);
      window.addEventListener("blur", closeMenu);
      return () => {
        window.removeEventListener("keydown", closeOnEscape);
        window.removeEventListener("pointerdown", closeOnOutsidePointerDown);
        window.removeEventListener("blur", closeMenu);
      };
    },
  );
  return (
    <div
      ref={(element) => (toolbarRef.current = element)}
      class="min-w-0 max-w-full overflow-x-auto"
    >
      <div class={cn("flex items-center gap-2 pb-px", compactAll() ? "flex-wrap" : "min-w-max")}>
        <div
          class="flex items-center overflow-hidden rounded border border-[#3b4a5f]"
          title={props.text("zoomWithWheel")}
        >
          <button
            class="h-7 min-w-7 border-r border-[#3b4a5f] px-2 text-xs text-[#c4d4e8] hover:bg-[#172131] hover:text-white"
            title={props.text("zoomOut")}
            type="button"
            onClick={props.onZoomOut}
          >
            -
          </button>
          <button
            class="h-7 min-w-[52px] border-r border-[#3b4a5f] px-2 text-xs text-[#c4d4e8] hover:bg-[#172131] hover:text-white"
            title={props.text("resetZoom")}
            type="button"
            onClick={props.onResetZoom}
          >
            {Math.round(props.zoom * 100)}%
          </button>
          <button
            class="h-7 min-w-7 border-r border-[#3b4a5f] px-2 text-xs text-[#c4d4e8] hover:bg-[#172131] hover:text-white"
            title={props.text("zoomIn")}
            type="button"
            onClick={props.onZoomIn}
          >
            +
          </button>
          <button
            class="h-7 min-w-[42px] px-2 text-xs text-[#c4d4e8] hover:bg-[#172131] hover:text-white disabled:cursor-not-allowed disabled:text-[#5f6d7e]"
            disabled={!props.canFitFlowchartWidth}
            title={props.text("fitWidthDescription")}
            type="button"
            onClick={props.onFitFlowchartWidth}
          >
            {props.text("fitWidth")}
          </button>
        </div>
        {compactAll() ? (
          <select
            class="h-7 rounded border border-[#3b4a5f] bg-[#101820] px-2 text-xs text-[#c4d4e8]"
            aria-label={props.text("labelMode")}
            value={props.labelMode}
            onChange={(event) =>
              props.onLabelModeChange(event.currentTarget.value as AspFlowchartLabelMode)
            }
          >
            {flowchartLabelModes.map((mode) => (
              <option value={mode}>
                {props.text(`labelMode${flowchartLabelModeTitleSuffix(mode)}`)}
              </option>
            ))}
          </select>
        ) : (
          <div
            class="flex items-center overflow-hidden rounded border border-[#3b4a5f]"
            title={props.text("labelMode")}
          >
            {flowchartLabelModes.map((mode) => (
              <button
                aria-pressed={props.labelMode === mode ? "true" : "false"}
                class={cn(
                  "h-7 min-w-[58px] border-r border-[#3b4a5f] px-2 text-xs last:border-r-0",
                  props.labelMode === mode
                    ? "bg-[#17324a] text-white"
                    : "text-[#c4d4e8] hover:bg-[#172131] hover:text-white",
                )}
                title={props.text(`labelMode${flowchartLabelModeTitleSuffix(mode)}`)}
                type="button"
                onClick={() => props.onLabelModeChange(mode)}
              >
                {props.text(`labelMode${flowchartLabelModeTitleSuffix(mode)}`)}
              </button>
            ))}
          </div>
        )}
        <button
          aria-pressed={
            props.sourcePanelVisible == null
              ? undefined
              : props.sourcePanelVisible
                ? "true"
                : "false"
          }
          class={cn(
            flowchartToolbarButtonClass,
            props.sourcePanelVisible && "bg-[#17324a] text-white",
          )}
          title={props.sourcePanelVisible ? props.text("hideSource") : props.text("showSource")}
          type="button"
          onClick={() => props.onSourcePanelVisibleChange(!props.sourcePanelVisible)}
        >
          {props.text("source")}
        </button>
        {compactAll() ? (
          <button
            aria-expanded={menu()?.kind === "open" ? "true" : "false"}
            aria-haspopup="menu"
            class={flowchartToolbarButtonClass}
            title={props.text("openMenu")}
            type="button"
            onClick={(event) => openMenu("open", event.currentTarget)}
          >
            {props.text("openMenu")}
          </button>
        ) : (
          <>
            <FlowchartToolbarButton
              disabled={!props.canOpenSection}
              label={props.text("code")}
              title={props.text("openCode")}
              onClick={props.onOpenCode}
            />
          </>
        )}
        {compactExports() ? (
          <button
            aria-expanded={menu()?.kind === "export" ? "true" : "false"}
            aria-haspopup="menu"
            class={flowchartToolbarButtonClass}
            title={props.text("exportMenu")}
            type="button"
            onClick={(event) => openMenu("export", event.currentTarget)}
          >
            {props.text("exportMenu")}
          </button>
        ) : (
          <>
            <FlowchartToolbarButton
              label={props.text("copyMermaid")}
              title={props.text("copyMermaid")}
              onClick={props.onCopyMermaid}
            />
            <FlowchartToolbarButton
              label={props.text("exportMermaid")}
              title={props.text("exportMermaid")}
              onClick={props.onExportMermaid}
            />
            <FlowchartToolbarButton
              disabled={!props.canExportSvg}
              label={props.text("exportSvg")}
              title={props.text("exportSvg")}
              onClick={props.onExportSvg}
            />
          </>
        )}
      </div>
      {menu() ? (
        <div
          ref={(element) => (menuRef.current = element)}
          class="fixed z-50 grid w-[180px] overflow-hidden rounded-md border border-[#3b4a5f] bg-[#151b23] py-1 text-xs text-[#d9e0ea] shadow-[0_12px_28px_rgb(0_0_0_/_32%)]"
          role="menu"
          style={webviewStyle({ left: menu()!.left, top: menu()!.top })}
        >
          {menu()!.kind === "open" ? (
            <>
              <FlowchartToolbarMenuItem
                disabled={!props.canOpenSection}
                label={props.text("code")}
                onClick={() => runMenuAction(props.onOpenCode)}
              />
            </>
          ) : (
            <>
              <FlowchartToolbarMenuItem
                label={props.text("copyMermaid")}
                onClick={() => runMenuAction(props.onCopyMermaid)}
              />
              <FlowchartToolbarMenuItem
                label={props.text("exportMermaid")}
                onClick={() => runMenuAction(props.onExportMermaid)}
              />
              <FlowchartToolbarMenuItem
                disabled={!props.canExportSvg}
                label={props.text("exportSvg")}
                onClick={() => runMenuAction(props.onExportSvg)}
              />
            </>
          )}
        </div>
      ) : null}
    </div>
  );
}
function FlowchartToolbarButton(props: {
  disabled?: boolean;
  label: string;
  title: string;
  onClick(): void;
}): JSX.Element {
  return (
    <button
      class={flowchartToolbarButtonClass}
      disabled={props.disabled}
      title={props.title}
      type="button"
      onClick={props.onClick}
    >
      {props.label}
    </button>
  );
}
function FlowchartToolbarMenuItem(props: {
  disabled?: boolean;
  label: string;
  onClick(): void;
}): JSX.Element {
  return (
    <button
      class="px-3 py-1.5 text-left hover:bg-[#172131] disabled:cursor-not-allowed disabled:text-[#5f6d7e]"
      disabled={props.disabled}
      role="menuitem"
      type="button"
      onClick={props.onClick}
    >
      {props.label}
    </button>
  );
}
function flowchartToolbarMode(width: number): FlowchartToolbarMode {
  if (width > 0 && width < 520) {
    return "compactAll";
  }
  if (width > 0 && width < 760) {
    return "compactExports";
  }
  return "full";
}
const flowchartToolbarButtonClass =
  "rounded border border-[#3b4a5f] px-3 py-1 text-xs text-[#c4d4e8] hover:border-[#7dd3fc] hover:text-white disabled:cursor-not-allowed disabled:border-[#263140] disabled:text-[#5f6d7e]";
const flowchartToolbarMenuWidth = 180;
