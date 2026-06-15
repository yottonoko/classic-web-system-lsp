import React, { useCallback, useEffect, useRef, useState } from "react";
import type { AspFlowchartLabelMode } from "@asp-lsp/core";
import { flowchartLabelModeTitleSuffix } from "./flowchart-model";
import { useElementSize } from "./flowchart-dom";
import { cn } from "../lib/utils";
import type {
  FlowchartToolbarMenuKind,
  FlowchartToolbarMenuState,
  FlowchartToolbarMode,
} from "./flowchart-types";

const flowchartLabelModes: AspFlowchartLabelMode[] = ["raw", "normal", "description"];

export function FlowchartToolbar({
  canExportSvg,
  canFitFlowchartWidth,
  canOpenSection,
  labelMode,
  text,
  zoom,
  onLabelModeChange,
  onCopyMermaid,
  onExportMermaid,
  onExportSvg,
  onFitFlowchartWidth,
  onOpenCode,
  onOpenGraph,
  onResetZoom,
  sourcePanelVisible,
  onSourcePanelVisibleChange,
  onZoomIn,
  onZoomOut,
}: {
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
  onOpenGraph(): void;
  onResetZoom(): void;
  sourcePanelVisible: boolean;
  onSourcePanelVisibleChange(value: boolean): void;
  onZoomIn(): void;
  onZoomOut(): void;
}): React.ReactElement {
  const [toolbarRef, toolbarSize] = useElementSize<HTMLDivElement>();
  const menuRef = useRef<HTMLDivElement | null>(null);
  const [menu, setMenu] = useState<FlowchartToolbarMenuState>();
  const toolbarMode = flowchartToolbarMode(toolbarSize.width);
  const compactExports = toolbarMode === "compactExports" || toolbarMode === "compactAll";
  const compactAll = toolbarMode === "compactAll";
  const closeMenu = useCallback(() => setMenu(undefined), []);
  const openMenu = useCallback((kind: FlowchartToolbarMenuKind, button: HTMLButtonElement) => {
    const rect = button.getBoundingClientRect();
    setMenu({
      kind,
      left: Math.max(8, Math.min(rect.left, window.innerWidth - flowchartToolbarMenuWidth - 8)),
      top: Math.min(rect.bottom + 6, window.innerHeight - 8),
    });
  }, []);
  const runMenuAction = useCallback(
    (action: () => void) => {
      action();
      closeMenu();
    },
    [closeMenu],
  );

  useEffect(() => {
    if (!menu) {
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
  }, [closeMenu, menu, toolbarRef]);

  return (
    <div ref={toolbarRef} className="min-w-0 max-w-full overflow-x-auto">
      <div className="flex min-w-max items-center gap-2 pb-px">
        <div
          className="flex items-center overflow-hidden rounded border border-[#3b4a5f]"
          title={text("zoomWithWheel")}
        >
          <button
            className="h-7 min-w-7 border-r border-[#3b4a5f] px-2 text-xs text-[#c4d4e8] hover:bg-[#172131] hover:text-white"
            title={text("zoomOut")}
            type="button"
            onClick={onZoomOut}
          >
            -
          </button>
          <button
            className="h-7 min-w-[52px] border-r border-[#3b4a5f] px-2 text-xs text-[#c4d4e8] hover:bg-[#172131] hover:text-white"
            title={text("resetZoom")}
            type="button"
            onClick={onResetZoom}
          >
            {Math.round(zoom * 100)}%
          </button>
          <button
            className="h-7 min-w-7 border-r border-[#3b4a5f] px-2 text-xs text-[#c4d4e8] hover:bg-[#172131] hover:text-white"
            title={text("zoomIn")}
            type="button"
            onClick={onZoomIn}
          >
            +
          </button>
          <button
            className="h-7 min-w-[42px] px-2 text-xs text-[#c4d4e8] hover:bg-[#172131] hover:text-white disabled:cursor-not-allowed disabled:text-[#5f6d7e]"
            disabled={!canFitFlowchartWidth}
            title={text("fitWidthDescription")}
            type="button"
            onClick={onFitFlowchartWidth}
          >
            {text("fitWidth")}
          </button>
        </div>
        <div
          className="flex items-center overflow-hidden rounded border border-[#3b4a5f]"
          title={text("labelMode")}
        >
          {flowchartLabelModes.map((mode) => (
            <button
              key={mode}
              aria-pressed={labelMode === mode}
              className={cn(
                "h-7 min-w-[58px] border-r border-[#3b4a5f] px-2 text-xs last:border-r-0",
                labelMode === mode
                  ? "bg-[#17324a] text-white"
                  : "text-[#c4d4e8] hover:bg-[#172131] hover:text-white",
              )}
              title={text(`labelMode${flowchartLabelModeTitleSuffix(mode)}`)}
              type="button"
              onClick={() => onLabelModeChange(mode)}
            >
              {text(`labelMode${flowchartLabelModeTitleSuffix(mode)}`)}
            </button>
          ))}
        </div>
        <button
          aria-pressed={sourcePanelVisible}
          className={cn(
            flowchartToolbarButtonClass,
            sourcePanelVisible && "bg-[#17324a] text-white",
          )}
          title={sourcePanelVisible ? text("hideSource") : text("showSource")}
          type="button"
          onClick={() => onSourcePanelVisibleChange(!sourcePanelVisible)}
        >
          {text("source")}
        </button>
        {compactAll ? (
          <button
            aria-expanded={menu?.kind === "open"}
            aria-haspopup="menu"
            className={flowchartToolbarButtonClass}
            title={text("openMenu")}
            type="button"
            onClick={(event) => openMenu("open", event.currentTarget)}
          >
            {text("openMenu")}
          </button>
        ) : (
          <>
            <FlowchartToolbarButton
              disabled={!canOpenSection}
              label="Code"
              title={text("openCode")}
              onClick={onOpenCode}
            />
            <FlowchartToolbarButton
              disabled={!canOpenSection}
              label="Graph"
              title={text("openGraph")}
              onClick={onOpenGraph}
            />
          </>
        )}
        {compactExports ? (
          <button
            aria-expanded={menu?.kind === "export"}
            aria-haspopup="menu"
            className={flowchartToolbarButtonClass}
            title={text("exportMenu")}
            type="button"
            onClick={(event) => openMenu("export", event.currentTarget)}
          >
            {text("exportMenu")}
          </button>
        ) : (
          <>
            <FlowchartToolbarButton
              label={text("copyMermaid")}
              title={text("copyMermaid")}
              onClick={onCopyMermaid}
            />
            <FlowchartToolbarButton
              label={text("exportMermaid")}
              title={text("exportMermaid")}
              onClick={onExportMermaid}
            />
            <FlowchartToolbarButton
              disabled={!canExportSvg}
              label={text("exportSvg")}
              title={text("exportSvg")}
              onClick={onExportSvg}
            />
          </>
        )}
      </div>
      {menu ? (
        <div
          ref={menuRef}
          className="fixed z-50 grid w-[180px] overflow-hidden rounded-md border border-[#3b4a5f] bg-[#151b23] py-1 text-xs text-[#d9e0ea] shadow-[0_12px_28px_rgb(0_0_0_/_32%)]"
          role="menu"
          style={{ left: menu.left, top: menu.top }}
        >
          {menu.kind === "open" ? (
            <>
              <FlowchartToolbarMenuItem
                disabled={!canOpenSection}
                label="Code"
                onClick={() => runMenuAction(onOpenCode)}
              />
              <FlowchartToolbarMenuItem
                disabled={!canOpenSection}
                label="Graph"
                onClick={() => runMenuAction(onOpenGraph)}
              />
            </>
          ) : (
            <>
              <FlowchartToolbarMenuItem
                label={text("copyMermaid")}
                onClick={() => runMenuAction(onCopyMermaid)}
              />
              <FlowchartToolbarMenuItem
                label={text("exportMermaid")}
                onClick={() => runMenuAction(onExportMermaid)}
              />
              <FlowchartToolbarMenuItem
                disabled={!canExportSvg}
                label={text("exportSvg")}
                onClick={() => runMenuAction(onExportSvg)}
              />
            </>
          )}
        </div>
      ) : null}
    </div>
  );
}

function FlowchartToolbarButton({
  disabled,
  label,
  title,
  onClick,
}: {
  disabled?: boolean;
  label: string;
  title: string;
  onClick(): void;
}): React.ReactElement {
  return (
    <button
      className={flowchartToolbarButtonClass}
      disabled={disabled}
      title={title}
      type="button"
      onClick={onClick}
    >
      {label}
    </button>
  );
}

function FlowchartToolbarMenuItem({
  disabled,
  label,
  onClick,
}: {
  disabled?: boolean;
  label: string;
  onClick(): void;
}): React.ReactElement {
  return (
    <button
      className="px-3 py-1.5 text-left hover:bg-[#172131] disabled:cursor-not-allowed disabled:text-[#5f6d7e]"
      disabled={disabled}
      role="menuitem"
      type="button"
      onClick={onClick}
    >
      {label}
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
