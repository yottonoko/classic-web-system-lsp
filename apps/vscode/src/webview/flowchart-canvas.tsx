import { createSignal, createMemo, createEffect, onSettled } from "solid-js";
import { type JSX } from "@solidjs/web";
import { webviewStyle, type WebviewEvent } from "./webview-dom-types";
import mermaid from "mermaid";
import { cn } from "../lib/utils";
import {
  attachSvgNodeHandlers,
  clampedContextMenuPosition,
  serializedFlowchartSvg,
  syncSvgSearchHighlights,
} from "./flowchart-dom";
import { FlowchartToolbar } from "./flowchart-toolbar";
import { vscode } from "./flowchart-runtime";
import {
  centerFlowchartHorizontally,
  clamp,
  defaultSectionId,
  flowchartFitWidthZoom,
  flowchartSvgLayerStyle,
  flowchartZoomRange,
  measuredFlowchartSvgSize,
  roundFlowchartZoom,
  scaledFlowchartCanvasStyle,
} from "./flowchart-model";
import type {
  FlowchartPanState,
  FlowchartPayload,
  FlowchartThemePalette,
  FlowchartViewportSize,
} from "./flowchart-types";
import type {
  AspFlowchartLabelMode,
  AspFlowchartNode,
  AspFlowchartSection,
  AspFlowchartTarget,
} from "../protocol-types";
const flowchartZoomStep = 0.1;
interface FlowchartContextMenuState {
  node: AspFlowchartNode;
  x: number;
  y: number;
}
interface FlowchartSvgSize {
  width: number;
  height: number;
}
export function FlowchartCanvas(props: {
  className?: string;
  labelMode: AspFlowchartLabelMode;
  payload: FlowchartPayload;
  section: AspFlowchartSection | undefined;
  themePalette: FlowchartThemePalette;
  text(key: string): string;
  activeSearchNodeId?: string;
  matchedNodeIds: Set<string>;
  onOpenCode(range: AspFlowchartNode["range"] | AspFlowchartSection["range"]): void;
  onOpenFlowchart(node: AspFlowchartNode): void;
  onLabelModeChange(mode: AspFlowchartLabelMode): void;
  sourcePanelVisible: boolean;
  onSourcePanelVisibleChange(value: boolean): void;
  onHoverNode(nodeId: string | undefined): void;
  onSelectNode(node: AspFlowchartNode): void;
}): JSX.Element {
  const viewportRef = { current: null } as {
    current: (HTMLDivElement | null) | null;
  };
  const containerRef = { current: null } as {
    current: (HTMLDivElement | null) | null;
  };
  const panStateRef = { current: undefined } as {
    current: (FlowchartPanState | undefined) | undefined;
  };
  const suppressNextCanvasClickRef = { current: false };
  const userPannedFlowchartKeyRef = { current: undefined } as {
    current: (string | undefined) | undefined;
  };
  const renderGenerationRef = { current: 0 };
  const [error, setError] = createSignal<string>();
  const [svg, setSvg] = createSignal<string>("");
  const [svgSize, setSvgSize] = createSignal<FlowchartSvgSize>();
  const [zoom, setZoom] = createSignal(1);
  const [viewportSize, setViewportSize] = createSignal<FlowchartViewportSize>({
    width: 0,
    height: 0,
  });
  const [isPanning, setIsPanning] = createSignal(false);
  const [contextMenu, setContextMenu] = createSignal<FlowchartContextMenuState>();
  const flowchartViewKey = createMemo(
    () => `${props.payload.uri}\n${props.section?.id ?? ""}\n${props.payload.mermaid}`,
  );
  const zoomRange = createMemo(() => flowchartZoomRange(props.payload));
  const setClampedZoom = (value: number) =>
    setZoom(roundFlowchartZoom(clamp(value, zoomRange().minimum, zoomRange().maximum)));
  const adjustZoom = (direction: 1 | -1) => setClampedZoom(zoom() + direction * flowchartZoomStep);
  const fitFlowchartWidth = () => {
    if (!viewportRef.current || !svgSize()) {
      return;
    }
    const nextZoom = flowchartFitWidthZoom(viewportRef.current, svgSize()!, zoomRange());
    if (nextZoom !== undefined) {
      userPannedFlowchartKeyRef.current = undefined;
      setClampedZoom(nextZoom);
    }
  };
  const handleWheel = (event: WebviewEvent<WheelEvent, HTMLDivElement>) => {
    if (!event.ctrlKey && !event.metaKey) {
      return;
    }
    event.preventDefault();
    const direction = event.deltaY < 0 ? 1 : -1;
    setClampedZoom(zoom() + direction * flowchartZoomStep);
  };
  const beginCanvasPan = (event: WebviewEvent<PointerEvent, HTMLDivElement>) => {
    if (event.button !== 0 || !viewportRef.current) {
      return;
    }
    const target = event.target instanceof Node ? event.target : undefined;
    if (!target || !viewportRef.current.contains(target)) {
      return;
    }
    panStateRef.current = {
      pointerId: event.pointerId,
      startX: event.clientX,
      startY: event.clientY,
      scrollLeft: viewportRef.current.scrollLeft,
      scrollTop: viewportRef.current.scrollTop,
      moved: false,
    };
    viewportRef.current.setPointerCapture(event.pointerId);
    setIsPanning(true);
  };
  const moveCanvasPan = (event: WebviewEvent<PointerEvent, HTMLDivElement>) => {
    const pan = panStateRef.current;
    if (!pan || pan.pointerId !== event.pointerId || !viewportRef.current) {
      return;
    }
    const deltaX = event.clientX - pan.startX;
    const deltaY = event.clientY - pan.startY;
    if (Math.abs(deltaX) > 2 || Math.abs(deltaY) > 2) {
      pan.moved = true;
    }
    viewportRef.current.scrollLeft = pan.scrollLeft - deltaX;
    viewportRef.current.scrollTop = pan.scrollTop - deltaY;
    event.preventDefault();
  };
  const endCanvasPan = (event: WebviewEvent<PointerEvent, HTMLDivElement>) => {
    const pan = panStateRef.current;
    if (!pan || pan.pointerId !== event.pointerId || !viewportRef.current) {
      return;
    }
    if (viewportRef.current.hasPointerCapture(event.pointerId)) {
      viewportRef.current.releasePointerCapture(event.pointerId);
    }
    if (pan.moved) {
      userPannedFlowchartKeyRef.current = flowchartViewKey();
      suppressNextCanvasClickRef.current = true;
      window.setTimeout(() => {
        suppressNextCanvasClickRef.current = false;
      }, 0);
    }
    panStateRef.current = undefined;
    setIsPanning(false);
  };
  const suppressCanvasClickAfterPan = (event: WebviewEvent<MouseEvent, HTMLDivElement>) => {
    if (!suppressNextCanvasClickRef.current) {
      return;
    }
    event.preventDefault();
    event.stopPropagation();
  };
  const openContextMenu = (node: AspFlowchartNode, event: MouseEvent) => {
    event.preventDefault();
    setContextMenu({ node, x: event.clientX, y: event.clientY });
  };
  const closeContextMenu = () => setContextMenu(undefined);
  const openContextMenuCode = () => {
    if (!contextMenu()?.node.range) {
      return;
    }
    props.onOpenCode(contextMenu()!.node.range);
    closeContextMenu();
  };
  const openContextMenuFlowchart = () => {
    if (!contextMenu()) {
      return;
    }
    props.onOpenFlowchart(contextMenu()!.node);
    closeContextMenu();
  };
  const selectContextMenuNode = () => {
    if (!contextMenu()) {
      return;
    }
    props.onSelectNode(contextMenu()!.node);
    closeContextMenu();
  };
  createEffect(
    () => [zoomRange()],
    () => {
      setZoom((currentZoom) =>
        roundFlowchartZoom(clamp(currentZoom, zoomRange().minimum, zoomRange().maximum)),
      );
    },
  );
  createEffect(
    () => [flowchartViewKey()],
    () => {
      userPannedFlowchartKeyRef.current = undefined;
    },
  );
  onSettled(() => {
    const viewport = viewportRef.current;
    if (!viewport) {
      return undefined;
    }
    const updateViewportSize = (): void => {
      setViewportSize({ width: viewport.clientWidth, height: viewport.clientHeight });
    };
    const suppressClick = (event: MouseEvent) =>
      suppressCanvasClickAfterPan(event as WebviewEvent<MouseEvent, HTMLDivElement>);
    viewport.addEventListener("click", suppressClick, true);
    updateViewportSize();
    const resizeObserver = new ResizeObserver(updateViewportSize);
    resizeObserver.observe(viewport);
    return () => {
      resizeObserver.disconnect();
      viewport.removeEventListener("click", suppressClick, true);
    };
  });
  createEffect(
    () => [flowchartViewKey(), svgSize(), viewportSize(), zoom()],
    () => {
      const viewport = viewportRef.current;
      if (
        !viewport ||
        !svgSize() ||
        viewportSize().width <= 0 ||
        userPannedFlowchartKeyRef.current === flowchartViewKey()
      ) {
        return;
      }
      centerFlowchartHorizontally(viewport, svgSize()!, zoom(), viewportSize());
    },
  );
  createEffect(
    () => [closeContextMenu, contextMenu()],
    () => {
      if (!contextMenu()) {
        return undefined;
      }
      const closeOnPointerDown = () => closeContextMenu();
      const closeOnKeyDown = (event: KeyboardEvent) => {
        if (event.key === "Escape") {
          closeContextMenu();
        }
      };
      window.addEventListener("pointerdown", closeOnPointerDown);
      window.addEventListener("keydown", closeOnKeyDown);
      window.addEventListener("blur", closeContextMenu);
      return () => {
        window.removeEventListener("pointerdown", closeOnPointerDown);
        window.removeEventListener("keydown", closeOnKeyDown);
        window.removeEventListener("blur", closeContextMenu);
      };
    },
  );
  createEffect(
    () => [
      props.onHoverNode,
      props.onOpenFlowchart,
      props.onSelectNode,
      openContextMenu,
      props.payload,
      props.themePalette,
      props.text,
    ],
    () => {
      let cancelled = false;
      const generation = ++renderGenerationRef.current;
      const isCurrent = (): boolean => !cancelled && generation === renderGenerationRef.current;
      const render = async (): Promise<void> => {
        if (!containerRef.current) {
          return;
        }
        mermaid.initialize({
          startOnLoad: false,
          maxTextSize: Number.POSITIVE_INFINITY,
          maxEdges: Number.POSITIVE_INFINITY,
          securityLevel: "strict",
          theme: props.themePalette.mermaidTheme,
          themeVariables: props.themePalette.mermaidThemeVariables,
          flowchart: { htmlLabels: false, curve: "basis" },
        });
        try {
          const id = `asp-lsp-flowchart-${generation}-${Date.now().toString(36)}`;
          const result = await mermaid.render(id, props.payload.mermaid || "flowchart TB");
          if (!isCurrent() || !containerRef.current) {
            return;
          }
          containerRef.current.innerHTML = result.svg;
          setSvg(containerRef.current.querySelector("svg")?.outerHTML ?? result.svg);
          setSvgSize(measuredFlowchartSvgSize(containerRef.current));
          attachSvgNodeHandlers(
            containerRef.current,
            props.payload,
            props.text,
            openContextMenu,
            props.onHoverNode,
            props.onSelectNode,
            props.onOpenFlowchart,
          );
          setError(undefined);
        } catch (renderError) {
          if (isCurrent()) {
            setError(renderError instanceof Error ? renderError.message : String(renderError));
            setSvg("");
            setSvgSize(undefined);
          }
        }
      };
      void render();
      return () => {
        cancelled = true;
        if (renderGenerationRef.current === generation) {
          renderGenerationRef.current += 1;
        }
      };
    },
  );
  createEffect(
    () => [props.activeSearchNodeId, props.matchedNodeIds, props.payload, svg()],
    () => {
      if (!containerRef.current || !viewportRef.current) {
        return;
      }
      syncSvgSearchHighlights(
        containerRef.current,
        viewportRef.current,
        props.payload,
        props.matchedNodeIds,
        props.activeSearchNodeId,
      );
    },
  );
  const exportSvg = () => {
    vscode.postMessage({
      type: "exportFlowchart",
      format: "svg",
      uri: props.payload.uri,
      sectionLabel: props.section?.label,
      content: serializedFlowchartSvg(containerRef.current) ?? svg(),
    });
  };
  const copyMermaid = () => {
    vscode.postMessage({
      type: "copyText",
      content: props.payload.mermaid,
    });
  };
  const exportMermaid = () => {
    vscode.postMessage({
      type: "exportFlowchart",
      format: "mermaid",
      uri: props.payload.uri,
      sectionLabel: props.section?.label,
      content: `${props.payload.mermaid}\n`,
    });
  };
  const contextMenuPosition = createMemo(() =>
    contextMenu() ? clampedContextMenuPosition(contextMenu()!.x, contextMenu()!.y) : undefined,
  );
  const canFitFlowchartWidth = createMemo(() => Boolean(svgSize()));
  return (
    <section
      class={cn("grid min-h-0 grid-rows-[auto_1fr] overflow-hidden bg-[#0d1117]", props.className)}
    >
      <header class="flex min-w-0 items-center gap-2 border-b border-[#263140] px-4 py-3">
        <div
          class="min-w-0 flex-1 truncate text-sm font-semibold text-[#f1f5f9]"
          title={props.section?.label ?? props.text("title")}
        >
          {props.section?.label ?? props.text("title")}
        </div>
        <FlowchartToolbar
          canExportSvg={Boolean(svg())}
          canFitFlowchartWidth={canFitFlowchartWidth()}
          canOpenSection={Boolean(props.section?.range)}
          labelMode={props.labelMode}
          text={props.text}
          zoom={zoom()}
          onLabelModeChange={props.onLabelModeChange}
          onCopyMermaid={copyMermaid}
          onExportMermaid={exportMermaid}
          onExportSvg={exportSvg}
          onFitFlowchartWidth={fitFlowchartWidth}
          onOpenCode={() => props.section?.range && props.onOpenCode(props.section.range)}
          onResetZoom={() => setClampedZoom(1)}
          sourcePanelVisible={props.sourcePanelVisible}
          onSourcePanelVisibleChange={props.onSourcePanelVisibleChange}
          onZoomIn={() => adjustZoom(1)}
          onZoomOut={() => adjustZoom(-1)}
        />
      </header>
      <div
        ref={(element) => (viewportRef.current = element)}
        class={cn(
          "min-h-0 overflow-auto p-4 [scrollbar-gutter:stable] [touch-action:none]",
          isPanning() ? "cursor-grabbing" : "cursor-grab",
        )}
        onPointerCancel={endCanvasPan}
        onPointerDown={beginCanvasPan}
        onPointerMove={moveCanvasPan}
        onPointerUp={endCanvasPan}
        onWheel={handleWheel}
      >
        {error() ? (
          <div class="rounded border border-[#7f3434] bg-[#291416] p-3 text-sm text-[#ffd2cc]">
            {props.text("renderError")} {error()}
          </div>
        ) : null}
        <div
          class="relative select-none"
          style={webviewStyle(scaledFlowchartCanvasStyle(svgSize(), zoom(), viewportSize()))}
        >
          <div
            ref={(element) => (containerRef.current = element)}
            class="absolute top-0 inline-block origin-top-left [&_svg]:block [&_svg]:h-full [&_svg]:w-full [&_svg]:max-w-none"
            style={webviewStyle(flowchartSvgLayerStyle(svgSize(), zoom(), viewportSize()))}
          />
        </div>
        {contextMenu() && contextMenuPosition() ? (
          <div
            class="fixed z-50 grid min-w-40 overflow-hidden rounded-md border border-[#3b4a5f] bg-[#151b23] py-1 text-xs text-[#d9e0ea] shadow-[0_12px_28px_rgb(0_0_0_/_32%)]"
            role="menu"
            style={webviewStyle({
              left: contextMenuPosition()!.left,
              top: contextMenuPosition()!.top,
            })}
            onContextMenu={(event) => event.preventDefault()}
            onPointerDown={(event) => event.stopPropagation()}
          >
            <button
              class="px-3 py-1.5 text-left hover:bg-[#172131]"
              role="menuitem"
              type="button"
              onClick={selectContextMenuNode}
            >
              {props.text("selectNode")}
            </button>
            <button
              class="px-3 py-1.5 text-left hover:bg-[#172131] disabled:cursor-not-allowed disabled:text-[#5f6d7e]"
              disabled={!contextMenu()!.node.range}
              role="menuitem"
              type="button"
              onClick={openContextMenuCode}
            >
              {props.text("code")}
            </button>
            <button
              class="px-3 py-1.5 text-left hover:bg-[#172131]"
              role="menuitem"
              type="button"
              onClick={openContextMenuFlowchart}
            >
              {props.text("openFlowchart")}
            </button>
          </div>
        ) : null}
      </div>
    </section>
  );
}
export function sectionIdForNodeFlowchart(
  payload: FlowchartPayload,
  node: AspFlowchartNode,
): string {
  if (node.kind !== "call") {
    return node.sectionId;
  }
  const callableName = callableNameFromNodeLabel(node.label);
  if (!callableName) {
    return node.sectionId;
  }
  const normalizedCallable = normalizeFlowchartName(callableName);
  const targetSection = payload.sections.find((section) => {
    const sectionName = callableNameFromSectionLabel(section.label);
    return sectionName ? normalizeFlowchartName(sectionName) === normalizedCallable : false;
  });
  return targetSection?.id ?? node.sectionId;
}
export function openFlowchartTarget(
  payload: FlowchartPayload,
  target: AspFlowchartTarget,
  setSelectedSectionId: (sectionId: string | undefined) => void,
  setFocusedFlowchartNodeId: (nodeId: string | undefined) => void,
  labelMode?: AspFlowchartLabelMode,
): string | undefined {
  const targetRange = target.nameRange ?? target.range;
  if (target.uri && target.uri !== payload.uri) {
    setFocusedFlowchartNodeId(undefined);
    vscode.postMessage({
      type: "openFlowchartLocation",
      uri: target.uri,
      range: targetRange,
      labelMode,
    });
    return undefined;
  }
  const targetNode = targetRange ? flowchartNodeForRange(payload, targetRange) : undefined;
  const sectionId = targetRange
    ? (targetNode?.sectionId ??
      sectionIdForRange(payload, targetRange) ??
      defaultSectionId(payload))
    : defaultSectionId(payload);
  setSelectedSectionId(sectionId);
  setFocusedFlowchartNodeId(targetNode?.id);
  return sectionId;
}
export function sectionIdForRange(
  payload: FlowchartPayload,
  range: NonNullable<AspFlowchartTarget["range"]>,
): string | undefined {
  return (
    payload.sections.find((section) => section.range && rangeContains(section.range, range))?.id ??
    payload.sections.find((section) =>
      section.nodeIds.some((nodeId) => {
        const node = payload.nodes.find((candidate) => candidate.id === nodeId);
        return node?.range ? rangeContains(node.range, range) : false;
      }),
    )?.id
  );
}
export function flowchartNodeForRange(
  payload: FlowchartPayload,
  range: NonNullable<AspFlowchartTarget["range"]>,
): AspFlowchartNode | undefined {
  return (
    bestFlowchartNodeForRange(payload, range, (nodeRange) => rangeContains(nodeRange, range)) ??
    bestFlowchartNodeForRange(payload, range, (nodeRange) => rangesOverlap(nodeRange, range))
  );
}
function bestFlowchartNodeForRange(
  payload: FlowchartPayload,
  range: NonNullable<AspFlowchartTarget["range"]>,
  matches: (nodeRange: NonNullable<AspFlowchartNode["range"]>) => boolean,
): AspFlowchartNode | undefined {
  return payload.nodes
    .filter((node) => node.kind !== "start" && node.kind !== "end")
    .filter(
      (
        node,
      ): node is AspFlowchartNode & {
        range: NonNullable<AspFlowchartNode["range"]>;
      } => Boolean(node.range && matches(node.range)),
    )
    .sort((left, right) => rangeLength(left.range) - rangeLength(right.range))[0];
}
function rangeContains(
  outer: NonNullable<AspFlowchartNode["range"]>,
  inner: NonNullable<AspFlowchartTarget["range"]>,
): boolean {
  return (
    positionBeforeOrEqual(outer.start, inner.start) && positionBeforeOrEqual(inner.end, outer.end)
  );
}
function rangesOverlap(
  left: NonNullable<AspFlowchartNode["range"]>,
  right: NonNullable<AspFlowchartTarget["range"]>,
): boolean {
  return (
    positionBeforeOrEqual(left.start, right.end) && positionBeforeOrEqual(right.start, left.end)
  );
}
function rangeLength(range: NonNullable<AspFlowchartNode["range"]>): number {
  return (
    (range.end.line - range.start.line) * 1000000 + range.end.character - range.start.character
  );
}
function positionBeforeOrEqual(
  left: {
    line: number;
    character: number;
  },
  right: {
    line: number;
    character: number;
  },
): boolean {
  return left.line < right.line || (left.line === right.line && left.character <= right.character);
}
export function isRange(value: unknown): value is NonNullable<AspFlowchartTarget["range"]> {
  return Boolean(value && typeof value === "object" && "start" in value && "end" in value);
}
function callableNameFromNodeLabel(label: string): string | undefined {
  const withoutCall = label.trim().replace(/^call\s+/i, "");
  const match = /^([A-Za-z_][A-Za-z0-9_.]*)/.exec(withoutCall);
  return match?.[1];
}
function callableNameFromSectionLabel(label: string): string | undefined {
  return label
    .replace(/^(?:Sub|Function)\s+/i, "")
    .replace(/^Property\s+(?:Get|Let|Set)\s+/i, "")
    .trim();
}
function normalizeFlowchartName(value: string): string {
  return value.replace(/\s+/g, "").toLowerCase();
}
export function nodesBySectionId(payload: FlowchartPayload): Map<string, AspFlowchartNode[]> {
  const byId = new Map(payload.nodes.map((node) => [node.id, node]));
  const result = new Map<string, AspFlowchartNode[]>();
  for (const section of payload.sections) {
    result.set(
      section.id,
      section.nodeIds
        .map((id) => byId.get(id))
        .filter((node): node is AspFlowchartNode => Boolean(node)),
    );
  }
  return result;
}
export function isFlowchartPayload(value: unknown): value is FlowchartPayload {
  return Boolean(value && typeof value === "object" && "mermaid" in value && "nodes" in value);
}
