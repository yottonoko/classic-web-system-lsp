import React, { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import { highlight, InnerLine, Pre } from "codehike/code";
import type { AnnotationHandler, CodeAnnotation, HighlightedCode } from "codehike/code";
import mermaid from "mermaid";
import tailwindStyles from "./flowchart.css?inline";
import { VirtualList } from "./virtual-list";
import { ImeSafeInput } from "./ime-safe-input";
import { cn } from "../lib/utils";
import {
  attachSvgNodeHandlers,
  clampedContextMenuPosition,
  serializedFlowchartSvg,
  syncSvgSearchHighlights,
  useElementSize,
} from "./flowchart-dom";
import { FlowchartToolbar } from "./flowchart-toolbar";
import { flowchartMessages } from "./flowchart-i18n";
import { flowchartThemePalettes } from "./flowchart-theme";
import type {
  FlowchartLocale,
  FlowchartPayload,
  FlowchartPanState,
  FlowchartSourceActiveKind,
  FlowchartSourceHighlight,
  FlowchartSourceRange,
  FlowchartSourceScrollTarget,
  FlowchartThemePalette,
  FlowchartViewportSize,
  InfoPanelPosition,
  WebviewTheme,
  WebviewThemeSetting,
} from "./flowchart-types";
import {
  centerFlowchartHorizontally,
  clamp,
  defaultSectionId,
  detailParts,
  flowchartFitWidthZoom,
  flowchartForSection,
  flowchartLabelModeForPayload,
  flowchartNodeHint,
  flowchartNodeKindLabel,
  flowchartNodeLinkHint,
  flowchartNodeLinkStyle,
  flowchartSearchMatches,
  flowchartSectionHint,
  flowchartSvgLayerStyle,
  flowchartSwatchStyle,
  flowchartZoomRange,
  maxFlowchartInfoPanelWidthForLayout,
  maxFlowchartSourcePanelWidthForLayout,
  measuredFlowchartSvgSize,
  modulo,
  roundFlowchartZoom,
  scaledFlowchartCanvasStyle,
} from "./flowchart-model";
import type {
  AspFlowchartInclude,
  AspFlowchartLabelMode,
  AspFlowchartNode,
  AspFlowchartSection,
  AspFlowchartTarget,
} from "@asp-lsp/core";

declare const acquireVsCodeApi: () => {
  postMessage(message: unknown): void;
};

declare global {
  interface Window {
    __ASP_LSP_FLOWCHART__?: FlowchartPayload;
    __ASP_LSP_FLOWCHART_TARGET_RANGE__?: AspFlowchartTarget["range"] | null;
  }
}

const vscode = acquireVsCodeApi();

const flowchartZoomStep = 0.1;
const defaultFlowchartMaxTextSize = 2_000_000;
const defaultFlowchartMaxEdges = 100_000;
const flowchartPanelDefaultWidth = 380;
const flowchartPanelMinimumWidth = 320;
const flowchartPaneResizeKeyboardStep = 16;
const flowchartSourcePanelDefaultWidth = 420;
const flowchartSourcePanelMinimumWidth = 280;
const flowchartSourceActiveAnnotationName = "flowchartSourceActive";

const fallbackPayload: FlowchartPayload = {
  uri: "",
  sections: [],
  nodes: [],
  edges: [],
  includes: [],
  mermaid: "flowchart TB",
  stats: {
    sections: 0,
    nodes: 0,
    edges: 0,
    includes: 0,
  },
};

function useResolvedWebviewTheme(setting: WebviewThemeSetting | undefined): WebviewTheme {
  const [vscodeTheme, setVsCodeTheme] = useState<WebviewTheme>(() => detectedVsCodeTheme());
  useEffect(() => {
    if (setting === "light" || setting === "dark") {
      return undefined;
    }
    const observer = new MutationObserver(() => setVsCodeTheme(detectedVsCodeTheme()));
    observer.observe(document.body, { attributes: true, attributeFilter: ["class"] });
    return () => observer.disconnect();
  }, [setting]);
  return setting === "light" || setting === "dark" ? setting : vscodeTheme;
}

function detectedVsCodeTheme(): WebviewTheme {
  const classList = document.body.classList;
  return classList.contains("vscode-light") || classList.contains("vscode-high-contrast-light")
    ? "light"
    : "dark";
}

function App(): React.ReactElement {
  const initialPayload = window.__ASP_LSP_FLOWCHART__ ?? fallbackPayload;
  const initialTargetRange = isRange(window.__ASP_LSP_FLOWCHART_TARGET_RANGE__)
    ? window.__ASP_LSP_FLOWCHART_TARGET_RANGE__
    : undefined;
  const initialTargetNode = initialTargetRange
    ? flowchartNodeForRange(initialPayload, initialTargetRange)
    : undefined;
  const initialSectionId = initialTargetRange
    ? (initialTargetNode?.sectionId ??
      sectionIdForRange(initialPayload, initialTargetRange) ??
      defaultSectionId(initialPayload))
    : defaultSectionId(initialPayload);
  const [payload, setPayload] = useState<FlowchartPayload>(initialPayload);
  const [labelMode, setLabelMode] = useState<AspFlowchartLabelMode>(() =>
    flowchartLabelModeForPayload(initialPayload),
  );
  const theme = useResolvedWebviewTheme(payload.settings?.theme);
  const themePalette = flowchartThemePalettes[theme];
  const [selectedSectionId, setSelectedSectionId] = useState<string | undefined>(
    () => initialSectionId,
  );
  const selectedSectionIdRef = useRef<string | undefined>(initialSectionId);
  const [autoOpenSectionId, setAutoOpenSectionId] = useState<string | undefined>(() =>
    initialTargetRange ? initialSectionId : undefined,
  );
  const [focusedFlowchartNodeId, setFocusedFlowchartNodeId] = useState<string | undefined>(
    () => initialTargetNode?.id,
  );
  const [hoveredFlowchartNodeId, setHoveredFlowchartNodeId] = useState<string | undefined>();
  const [sourcePanelVisible, setSourcePanelVisible] = useState(
    () => initialPayload.settings?.showSourcePanel ?? true,
  );
  const [sectionSourceScrollSequence, setSectionSourceScrollSequence] = useState(1);
  const [searchQuery, setSearchQuery] = useState("");
  const [activeSearchIndex, setActiveSearchIndex] = useState(0);
  const [infoPanelWidth, setInfoPanelWidth] = useState(flowchartPanelDefaultWidth);
  const [sourcePanelWidth, setSourcePanelWidth] = useState(flowchartSourcePanelDefaultWidth);
  const searchInputRef = useRef<HTMLInputElement | null>(null);
  const [layoutRef, layoutSize] = useElementSize<HTMLElement>();
  const infoPanelPosition = payload.settings?.infoPanelPosition ?? "left";
  const maximumInfoPanelWidth = maxFlowchartInfoPanelWidthForLayout(layoutSize.width);
  const maximumSourcePanelWidth = maxFlowchartSourcePanelWidthForLayout(layoutSize.width);
  const clampedInfoPanelWidth = clamp(
    infoPanelWidth,
    flowchartPanelMinimumWidth,
    maximumInfoPanelWidth,
  );
  const clampedSourcePanelWidth = clamp(
    sourcePanelWidth,
    flowchartSourcePanelMinimumWidth,
    maximumSourcePanelWidth,
  );
  const layoutStyle = {
    "--flowchart-panel-width": `${clampedInfoPanelWidth}px`,
    "--flowchart-source-panel-width": `${clampedSourcePanelWidth}px`,
  } as React.CSSProperties;
  const showSourcePanel = sourcePanelVisible;
  const layoutClassName = cn(
    "asp-lsp-flowchart-shell grid h-full bg-[#101419] text-[#d9e0ea]",
    showSourcePanel
      ? infoPanelPosition === "right"
        ? "grid-cols-[var(--flowchart-source-panel-width)_6px_minmax(0,1fr)_6px_var(--flowchart-panel-width)]"
        : "grid-cols-[var(--flowchart-panel-width)_6px_minmax(0,1fr)_6px_var(--flowchart-source-panel-width)]"
      : infoPanelPosition === "right"
        ? "grid-cols-[minmax(0,1fr)_6px_var(--flowchart-panel-width)]"
        : "grid-cols-[var(--flowchart-panel-width)_6px_minmax(0,1fr)]",
  );
  const infoPanelClassName = cn(
    "flex min-h-0 flex-col bg-[#151b23]",
    infoPanelPosition === "right"
      ? [showSourcePanel ? "order-5" : "order-3", "border-l border-[#263140]"]
      : "order-1 border-r border-[#263140]",
  );
  const canvasClassName = cn(
    infoPanelPosition === "right" ? (showSourcePanel ? "order-3" : "order-1") : "order-3",
  );
  const resizeHandleClassName = cn(
    infoPanelPosition === "right" && showSourcePanel ? "order-4" : "order-2",
  );
  const sourceResizeHandleClassName = infoPanelPosition === "right" ? "order-2" : "order-4";
  const sourcePanelPosition: InfoPanelPosition = infoPanelPosition === "right" ? "left" : "right";
  const sourcePanelClassName = cn(
    infoPanelPosition === "right"
      ? "order-1 border-r border-[#263140]"
      : "order-5 border-l border-[#263140]",
  );
  const locale = payload.locale ?? "en";
  const text = useCallback(
    (key: string): string => flowchartMessages[locale][key] ?? flowchartMessages.en[key] ?? key,
    [locale],
  );
  const nodesBySection = useMemo(() => nodesBySectionId(payload), [payload]);
  const searchMatches = useMemo(
    () => flowchartSearchMatches(payload, searchQuery),
    [payload, searchQuery],
  );
  const matchedNodeIds = useMemo(
    () => new Set(searchMatches.map((match) => match.node.id)),
    [searchMatches],
  );
  const activeSearchIndexForDisplay =
    searchMatches.length > 0 ? Math.min(activeSearchIndex, searchMatches.length - 1) : 0;
  const activeSearchNode = searchMatches[activeSearchIndexForDisplay]?.node;
  const activeFlowchartNodeId = focusedFlowchartNodeId ?? activeSearchNode?.id;
  const selectedFlowchart = useMemo(
    () => flowchartForSection(payload, selectedSectionId, themePalette),
    [payload, selectedSectionId, themePalette],
  );
  const selectedSection = selectedFlowchart.sections[0];
  const selectedSectionIndex = useMemo(
    () => payload.sections.findIndex((section) => section.id === selectedSection?.id),
    [payload.sections, selectedSection?.id],
  );
  const sourceHighlights = useMemo(
    () =>
      flowchartSourceHighlights(
        selectedSection,
        selectedFlowchart.nodes,
        payload.nodes,
        hoveredFlowchartNodeId,
        activeFlowchartNodeId,
      ),
    [
      activeFlowchartNodeId,
      hoveredFlowchartNodeId,
      payload.nodes,
      selectedFlowchart.nodes,
      selectedSection,
    ],
  );
  const primarySourceHighlight = flowchartPrimarySourceHighlight(sourceHighlights);
  const sourceScrollTarget = useMemo(
    () =>
      flowchartSourceScrollTarget(sourceHighlights, {
        activeNodeId: activeFlowchartNodeId,
        hoveredNodeId: hoveredFlowchartNodeId,
        sectionId: selectedSection?.id,
        sectionSequence: sectionSourceScrollSequence,
        uri: payload.uri,
      }),
    [
      activeFlowchartNodeId,
      hoveredFlowchartNodeId,
      payload.uri,
      sectionSourceScrollSequence,
      selectedSection?.id,
      sourceHighlights,
    ],
  );
  const selectFlowchartNode = useCallback((node: AspFlowchartNode) => {
    setSelectedSectionId(node.sectionId);
    setAutoOpenSectionId(node.sectionId);
    setFocusedFlowchartNodeId(node.id);
  }, []);
  const openFlowchartForNode = useCallback(
    (node: AspFlowchartNode) => {
      const target = node.links?.[0]?.target;
      if (target) {
        setAutoOpenSectionId(
          openFlowchartTarget(
            payload,
            target,
            setSelectedSectionId,
            setFocusedFlowchartNodeId,
            labelMode,
          ),
        );
        setSectionSourceScrollSequence((current) => current + 1);
      } else {
        const nextSectionId = sectionIdForNodeFlowchart(payload, node);
        setSelectedSectionId(nextSectionId);
        setAutoOpenSectionId(nextSectionId);
        setFocusedFlowchartNodeId(undefined);
        setSectionSourceScrollSequence((current) => current + 1);
      }
    },
    [labelMode, payload],
  );
  const openTarget = useCallback(
    (target: AspFlowchartTarget) => {
      setAutoOpenSectionId(
        openFlowchartTarget(
          payload,
          target,
          setSelectedSectionId,
          setFocusedFlowchartNodeId,
          labelMode,
        ),
      );
      setSectionSourceScrollSequence((current) => current + 1);
    },
    [labelMode, payload],
  );
  const openGraph = useCallback(
    (range: AspFlowchartNode["range"] | AspFlowchartSection["range"]) => {
      if (range) {
        vscode.postMessage({ type: "openGraphLocation", uri: payload.uri, range });
      }
    },
    [payload.uri],
  );
  const changeLabelMode = useCallback(
    (mode: AspFlowchartLabelMode) => {
      if (mode === labelMode) {
        return;
      }
      setLabelMode(mode);
      vscode.postMessage({ type: "reloadFlowchart", uri: payload.uri, labelMode: mode });
    },
    [labelMode, payload.uri],
  );
  const selectSearchMatch = useCallback(
    (index: number) => {
      if (searchMatches.length === 0) {
        return;
      }
      const nextIndex = modulo(index, searchMatches.length);
      const nextSectionId = searchMatches[nextIndex]?.node.sectionId;
      setFocusedFlowchartNodeId(undefined);
      setActiveSearchIndex(nextIndex);
      setSelectedSectionId(nextSectionId);
      setAutoOpenSectionId(nextSectionId);
    },
    [searchMatches],
  );
  const handleSearchKeyDown = useCallback(
    (event: React.KeyboardEvent<HTMLInputElement>) => {
      if (event.key === "Enter") {
        event.preventDefault();
        selectSearchMatch(activeSearchIndex + (event.shiftKey ? -1 : 1));
      } else if (event.key === "Escape") {
        setSearchQuery("");
        setFocusedFlowchartNodeId(undefined);
      }
    },
    [activeSearchIndex, selectSearchMatch],
  );
  useEffect(() => {
    selectedSectionIdRef.current = selectedSectionId;
  }, [selectedSectionId]);
  useEffect(() => {
    const listener = (event: MessageEvent) => {
      const message = event.data as { type?: unknown; payload?: unknown; targetRange?: unknown };
      if (message.type === "flowchartPayload" && isFlowchartPayload(message.payload)) {
        const targetRange = isRange(message.targetRange) ? message.targetRange : undefined;
        const targetNode = targetRange
          ? flowchartNodeForRange(message.payload, targetRange)
          : undefined;
        const preservedSectionId =
          !targetRange &&
          selectedSectionIdRef.current &&
          message.payload.sections.some((section) => section.id === selectedSectionIdRef.current)
            ? selectedSectionIdRef.current
            : undefined;
        const nextSectionId = targetRange
          ? (targetNode?.sectionId ??
            sectionIdForRange(message.payload, targetRange) ??
            defaultSectionId(message.payload))
          : (preservedSectionId ?? defaultSectionId(message.payload));
        setPayload(message.payload);
        setLabelMode(flowchartLabelModeForPayload(message.payload));
        setHoveredFlowchartNodeId(undefined);
        setFocusedFlowchartNodeId(targetNode?.id);
        setSelectedSectionId(nextSectionId);
        setAutoOpenSectionId(targetRange ? nextSectionId : undefined);
        if (targetRange) {
          setSectionSourceScrollSequence((current) => current + 1);
        }
      }
    };
    window.addEventListener("message", listener);
    return () => window.removeEventListener("message", listener);
  }, []);
  useEffect(() => {
    const listener = (event: KeyboardEvent) => {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "f") {
        event.preventDefault();
        searchInputRef.current?.focus();
        searchInputRef.current?.select();
      }
    };
    window.addEventListener("keydown", listener);
    return () => window.removeEventListener("keydown", listener);
  }, []);
  useEffect(() => {
    setActiveSearchIndex(0);
  }, [payload, searchQuery]);
  useEffect(() => {
    if (searchMatches.length === 0) {
      return;
    }
    const nextSectionId =
      searchMatches[Math.min(activeSearchIndex, searchMatches.length - 1)].node.sectionId;
    setSelectedSectionId(nextSectionId);
    if (searchQuery.trim()) {
      setAutoOpenSectionId(nextSectionId);
    }
  }, [activeSearchIndex, searchMatches, searchQuery]);
  useEffect(() => {
    setInfoPanelWidth((currentWidth) =>
      clamp(
        currentWidth,
        flowchartPanelMinimumWidth,
        maxFlowchartInfoPanelWidthForLayout(layoutSize.width),
      ),
    );
  }, [layoutSize.width]);
  useEffect(() => {
    setSourcePanelWidth((currentWidth) =>
      clamp(
        currentWidth,
        flowchartSourcePanelMinimumWidth,
        maxFlowchartSourcePanelWidthForLayout(layoutSize.width),
      ),
    );
  }, [layoutSize.width]);
  return (
    <main
      ref={layoutRef}
      className={layoutClassName}
      data-asp-lsp-theme={theme}
      style={layoutStyle}
    >
      <aside className={cn(infoPanelClassName, "min-w-0 overflow-hidden")}>
        <header className="border-b border-[#263140] px-4 py-3">
          <div
            className="overflow-hidden text-ellipsis whitespace-nowrap text-sm font-semibold text-[#f1f5f9]"
            title={selectedSection?.label ?? payload.fileName ?? text("title")}
          >
            {selectedSection?.label ?? payload.fileName ?? text("title")}
          </div>
          <div
            className="mt-1 overflow-hidden text-ellipsis whitespace-nowrap text-xs text-[#9fb0c5]"
            title={`${payload.stats.sections} ${text("sections")} / ${selectedFlowchart.stats.nodes} ${text("nodes")} / ${payload.includes.length} ${text("includes")}`}
          >
            {payload.stats.sections} {text("sections")} / {selectedFlowchart.stats.nodes}{" "}
            {text("nodes")} / {payload.includes.length} {text("includes")}
          </div>
        </header>
        <div className="border-b border-[#263140] px-3 py-2">
          <div className="flex items-center gap-1">
            <ImeSafeInput
              ref={searchInputRef}
              aria-label={text("searchNodes")}
              className="h-7 min-w-0 flex-1 rounded border border-[#334255] bg-[#0c1117] px-2 text-xs text-[#d9e0ea] outline-none placeholder:text-[#6f7e91] focus:border-[#7dd3fc]"
              placeholder={text("searchPlaceholder")}
              type="search"
              value={searchQuery}
              onValueChange={(value) => {
                setSearchQuery(value);
                setFocusedFlowchartNodeId(undefined);
              }}
              onKeyDown={handleSearchKeyDown}
            />
            <span className="min-w-[44px] text-center text-[11px] text-[#9fb0c5]">
              {searchMatches.length > 0
                ? `${activeSearchIndexForDisplay + 1}/${searchMatches.length}`
                : "0"}
            </span>
            <button
              className="h-7 w-7 rounded border border-[#334255] text-xs text-[#c4d4e8] hover:border-[#7dd3fc] hover:text-white disabled:cursor-not-allowed disabled:border-[#263140] disabled:text-[#5f6d7e]"
              disabled={searchMatches.length === 0}
              title={text("searchPrevious")}
              type="button"
              onClick={() => selectSearchMatch(activeSearchIndex - 1)}
            >
              ↑
            </button>
            <button
              className="h-7 w-7 rounded border border-[#334255] text-xs text-[#c4d4e8] hover:border-[#7dd3fc] hover:text-white disabled:cursor-not-allowed disabled:border-[#263140] disabled:text-[#5f6d7e]"
              disabled={searchMatches.length === 0}
              title={text("searchNext")}
              type="button"
              onClick={() => selectSearchMatch(activeSearchIndex + 1)}
            >
              ↓
            </button>
          </div>
        </div>
        <div className="min-h-0 flex-1 overflow-auto p-3">
          <SidebarAccordionSection
            count={payload.includes.length}
            hint={text("includesHint")}
            text={text}
            title={text("includes")}
          >
            <IncludeList
              includes={payload.includes}
              labelMode={labelMode}
              text={text}
              uri={payload.uri}
            />
          </SidebarAccordionSection>
          <SectionHeading>{text("flowcharts")}</SectionHeading>
          {payload.sections.length === 0 ? (
            <EmptyText>{text("emptyNodes")}</EmptyText>
          ) : (
            <VirtualList
              className="grid gap-3"
              estimateSize={72}
              getKey={(section) => section.id}
              items={payload.sections}
              maxHeight={560}
              overscan={8}
              scrollToIndex={selectedSectionIndex >= 0 ? selectedSectionIndex : undefined}
              renderItem={(section) => {
                const sectionNodes = nodesBySection.get(section.id) ?? [];
                const hasActiveNode = Boolean(
                  activeFlowchartNodeId &&
                  sectionNodes.some((node) => node.id === activeFlowchartNodeId),
                );
                return (
                  <FlowSection
                    locale={locale}
                    nodes={sectionNodes}
                    selected={section.id === selectedSection?.id}
                    section={section}
                    shouldAutoOpen={autoOpenSectionId === section.id || hasActiveNode}
                    themePalette={themePalette}
                    text={text}
                    activeSearchNodeId={activeFlowchartNodeId}
                    matchedNodeIds={matchedNodeIds}
                    onOpenCode={(range) =>
                      range && vscode.postMessage({ type: "openRange", uri: payload.uri, range })
                    }
                    onOpenGraph={openGraph}
                    onOpenTarget={openTarget}
                    onSelect={() => {
                      setSelectedSectionId(section.id);
                      setAutoOpenSectionId(section.id);
                      setFocusedFlowchartNodeId(undefined);
                      setSectionSourceScrollSequence((current) => current + 1);
                    }}
                    onSelectNode={selectFlowchartNode}
                  />
                );
              }}
            />
          )}
          <div className="mb-2 mt-3 flex items-center gap-2">
            <SectionHeading>{text("mermaid")}</SectionHeading>
            <button
              className="ml-auto rounded border border-[#3b4a5f] px-2 py-0.5 text-[11px] text-[#c4d4e8] hover:border-[#7dd3fc] hover:text-white"
              type="button"
              onClick={() =>
                vscode.postMessage({
                  type: "copyText",
                  content: selectedFlowchart.mermaid,
                })
              }
            >
              {text("copyMermaid")}
            </button>
          </div>
          <pre className="max-h-52 overflow-auto rounded border border-[#263140] bg-[#0c1117] p-2 text-xs leading-5 text-[#b9c5d6]">
            {selectedFlowchart.mermaid}
          </pre>
        </div>
      </aside>
      <FlowchartPaneResizeHandle
        label={text("resizeInfoPanel")}
        maxWidth={maximumInfoPanelWidth}
        minWidth={flowchartPanelMinimumWidth}
        position={infoPanelPosition}
        width={clampedInfoPanelWidth}
        className={resizeHandleClassName}
        onWidthChange={setInfoPanelWidth}
      />
      <FlowchartCanvas
        className={canvasClassName}
        payload={selectedFlowchart}
        section={selectedSection}
        themePalette={themePalette}
        text={text}
        activeSearchNodeId={activeFlowchartNodeId}
        matchedNodeIds={matchedNodeIds}
        onOpenCode={(range) =>
          range && vscode.postMessage({ type: "openRange", uri: payload.uri, range })
        }
        onOpenFlowchart={openFlowchartForNode}
        onOpenGraph={openGraph}
        labelMode={labelMode}
        onLabelModeChange={changeLabelMode}
        sourcePanelVisible={sourcePanelVisible}
        onSourcePanelVisibleChange={setSourcePanelVisible}
        onHoverNode={setHoveredFlowchartNodeId}
        onSelectNode={selectFlowchartNode}
      />
      {showSourcePanel ? (
        <>
          <FlowchartPaneResizeHandle
            label={text("resizeSourcePanel")}
            maxWidth={maximumSourcePanelWidth}
            minWidth={flowchartSourcePanelMinimumWidth}
            position={sourcePanelPosition}
            width={clampedSourcePanelWidth}
            className={sourceResizeHandleClassName}
            onWidthChange={setSourcePanelWidth}
          />
          <FlowchartSourcePanel
            activeLabel={primarySourceHighlight?.label}
            className={sourcePanelClassName}
            highlights={sourceHighlights}
            nodes={payload.nodes}
            scrollTarget={sourceScrollTarget}
            sourceText={payload.sourceText}
            text={text}
            theme={theme}
            onSelectNode={selectFlowchartNode}
          />
        </>
      ) : null}
    </main>
  );
}

function SidebarAccordionSection({
  children,
  count,
  hint,
  text,
  title,
}: {
  children: React.ReactNode;
  count?: number;
  hint?: string;
  text(key: string): string;
  title: string;
}): React.ReactElement {
  const [open, setOpen] = useState(true);
  const headerTitle = detailParts(title, hint, text(open ? "collapseSection" : "expandSection"));
  return (
    <section className="mb-4 min-w-0 overflow-hidden rounded border border-[#263140] bg-[#101820]">
      <div className="flex items-center gap-2">
        <button
          aria-expanded={open}
          aria-label={headerTitle}
          className="flex min-w-0 flex-1 items-center gap-2 px-2 py-1.5 text-left hover:bg-[#172131]"
          type="button"
          onClick={() => setOpen((current) => !current)}
        >
          <span className="h-6 w-6 shrink-0 rounded border border-[#334255] text-center text-xs leading-6 text-[#c4d4e8]">
            {open ? "▾" : "▸"}
          </span>
          <span className="min-w-0 flex-1 truncate text-xs font-semibold uppercase tracking-wide text-[#9fb0c5]">
            {title}
          </span>
          {typeof count === "number" ? (
            <span className="shrink-0 rounded border border-[#334255] px-1.5 py-0.5 text-[11px] text-[#9fb0c5]">
              {count}
            </span>
          ) : null}
        </button>
        {hint ? (
          <div className="shrink-0 pr-2">
            <FlowchartHint hint={hint} label={title} />
          </div>
        ) : null}
      </div>
      {open ? <div className="border-t border-[#263140] p-2">{children}</div> : null}
    </section>
  );
}

interface TooltipPosition {
  left: number;
  top: number;
  maxWidth: number;
}

function FlowchartHint({ hint, label }: { hint: string; label: string }): React.ReactElement {
  const triggerRef = useRef<HTMLSpanElement>(null);
  const tooltipRef = useRef<HTMLSpanElement>(null);
  const [visible, setVisible] = useState(false);
  const [position, setPosition] = useState<TooltipPosition>();
  const showTooltip = useCallback(() => {
    setVisible(true);
  }, []);
  const hideTooltip = useCallback(() => {
    setVisible(false);
    setPosition(undefined);
  }, []);
  useLayoutEffect(() => {
    if (!visible) {
      return undefined;
    }
    const updatePosition = (): void => {
      setPosition(tooltipPositionFor(triggerRef.current, tooltipRef.current));
    };
    updatePosition();
    window.addEventListener("resize", updatePosition);
    window.addEventListener("scroll", updatePosition, true);
    return () => {
      window.removeEventListener("resize", updatePosition);
      window.removeEventListener("scroll", updatePosition, true);
    };
  }, [visible]);
  return (
    <span className="group relative inline-flex shrink-0 items-center">
      <span
        ref={triggerRef}
        tabIndex={0}
        aria-label={`${label}: ${hint}`}
        className="inline-grid h-3.5 w-3.5 cursor-help place-items-center rounded-full border border-[#405068] text-[10px] leading-none text-[#8d98a8] outline-none hover:border-[#89ddff] hover:text-[#d7dde8] focus:border-[#89ddff] focus:text-[#d7dde8]"
        onBlur={hideTooltip}
        onFocus={showTooltip}
        onPointerEnter={showTooltip}
        onPointerLeave={hideTooltip}
      >
        ?
      </span>
      {visible ? (
        <span
          ref={tooltipRef}
          role="tooltip"
          className={cn(
            "pointer-events-none fixed z-[1000] rounded-md border border-[#405068] bg-[#0d1117] px-2 py-1.5 text-[11px] leading-[1.35] whitespace-normal text-[#d7dde8] shadow-[0_10px_24px_rgb(0_0_0_/_35%)]",
            position ? "visible" : "invisible",
          )}
          style={{
            left: position?.left ?? -9999,
            top: position?.top ?? -9999,
            maxWidth: position?.maxWidth ?? tooltipMaximumWidth(),
          }}
        >
          {hint}
        </span>
      ) : null}
    </span>
  );
}

function tooltipPositionFor(
  element: HTMLElement | null,
  tooltip: HTMLElement | null,
): TooltipPosition | undefined {
  if (!element) {
    return undefined;
  }
  const margin = 12;
  const gap = 6;
  const rect = element.getBoundingClientRect();
  const maxWidth = tooltipMaximumWidth();
  const tooltipRect = tooltip?.getBoundingClientRect();
  const tooltipWidth = Math.min(tooltipRect?.width ?? maxWidth, maxWidth);
  const tooltipHeight = tooltipRect?.height ?? 80;
  const left = clamp(
    rect.left + rect.width / 2 - tooltipWidth / 2,
    margin,
    Math.max(margin, window.innerWidth - tooltipWidth - margin),
  );
  const belowTop = rect.bottom + gap;
  const aboveTop = rect.top - gap - tooltipHeight;
  const top =
    belowTop + tooltipHeight + margin <= window.innerHeight || aboveTop < margin
      ? clamp(belowTop, margin, Math.max(margin, window.innerHeight - tooltipHeight - margin))
      : aboveTop;
  return { left, top, maxWidth };
}

function tooltipMaximumWidth(): number {
  const margin = 12;
  return Math.max(160, Math.min(280, window.innerWidth - margin * 2));
}

function IncludeList({
  includes,
  labelMode,
  text,
  uri,
}: {
  includes: AspFlowchartInclude[];
  labelMode: AspFlowchartLabelMode;
  text(key: string): string;
  uri: string;
}): React.ReactElement {
  if (includes.length === 0) {
    return <EmptyText>{text("emptyIncludes")}</EmptyText>;
  }
  return (
    <VirtualList
      className="grid gap-2"
      estimateSize={84}
      getKey={(include, index) => `${include.mode}:${include.path}:${index}`}
      items={includes}
      maxHeight={280}
      overscan={8}
      renderItem={(include) => (
        <div className="min-w-0 overflow-hidden rounded border border-[#263140] bg-[#101820] p-2">
          <div className="flex min-w-0 items-start justify-between gap-2">
            <button
              className="min-w-0 flex-1 text-left text-sm font-medium text-[#8ec7ff] hover:underline disabled:cursor-not-allowed disabled:text-[#7b8796] disabled:no-underline"
              disabled={!include.exists || !include.resolvedUri}
              title={detailParts(text("openFlowchart"), include.path, include.actualPath)}
              type="button"
              onClick={() =>
                include.resolvedUri &&
                vscode.postMessage({
                  type: "openIncludeFlowchart",
                  uri: include.resolvedUri,
                  labelMode,
                })
              }
            >
              <span className="block overflow-hidden text-ellipsis whitespace-nowrap">
                {include.path}
              </span>
            </button>
            {include.exists === false ? (
              <span className="text-xs text-[#ffb4a8]">{text("missing")}</span>
            ) : null}
          </div>
          <div className="mt-1 flex min-w-0 items-center justify-between gap-2 text-xs text-[#9fb0c5]">
            <span className="min-w-0 overflow-hidden text-ellipsis whitespace-nowrap">
              {include.mode}
            </span>
            <button
              className="shrink-0 text-[#c4d4e8] hover:text-white hover:underline"
              title={detailParts(text("openDirective"), include.path)}
              type="button"
              onClick={() => vscode.postMessage({ type: "openRange", uri, range: include.range })}
            >
              {text("openDirective")}
            </button>
          </div>
          {include.actualPath ? (
            <div
              className="mt-1 overflow-hidden text-ellipsis whitespace-nowrap text-xs text-[#7d8ca1]"
              title={include.actualPath}
            >
              {include.actualPath}
            </div>
          ) : null}
        </div>
      )}
    />
  );
}

function FlowSection({
  locale,
  nodes,
  selected,
  section,
  shouldAutoOpen,
  themePalette,
  text,
  activeSearchNodeId,
  matchedNodeIds,
  onOpenCode,
  onOpenGraph,
  onOpenTarget,
  onSelect,
  onSelectNode,
}: {
  locale: FlowchartLocale;
  nodes: AspFlowchartNode[];
  selected: boolean;
  section: AspFlowchartSection;
  shouldAutoOpen: boolean;
  themePalette: FlowchartThemePalette;
  text(key: string): string;
  activeSearchNodeId?: string;
  matchedNodeIds: Set<string>;
  onOpenCode(range: AspFlowchartNode["range"] | AspFlowchartSection["range"]): void;
  onOpenGraph(range: AspFlowchartNode["range"] | AspFlowchartSection["range"]): void;
  onOpenTarget(target: AspFlowchartTarget): void;
  onSelect(): void;
  onSelectNode(node: AspFlowchartNode): void;
}): React.ReactElement {
  const visibleNodes = nodes.filter((node) => node.kind !== "start" && node.kind !== "end");
  const [open, setOpen] = useState(false);
  const sectionHint = flowchartSectionHint(section, text, locale);
  const headerTitle = detailParts(
    section.label,
    sectionHint,
    text(open ? "collapseSection" : "expandSection"),
  );
  useEffect(() => {
    if (shouldAutoOpen) {
      setOpen(true);
    }
  }, [shouldAutoOpen]);
  const activeNodeIndex = visibleNodes.findIndex((node) => node.id === activeSearchNodeId);
  return (
    <div
      className={cn(
        "mb-3 min-w-0 overflow-hidden rounded border bg-[#101820]",
        selected ? "border-[#6fb6ff]" : "border-[#263140]",
      )}
    >
      <div className="flex items-center gap-2 border-b border-[#263140] px-2 py-1.5">
        <button
          aria-expanded={open}
          aria-label={headerTitle}
          className="h-6 w-6 shrink-0 rounded border border-[#334255] text-xs text-[#c4d4e8] hover:border-[#7dd3fc] hover:text-white"
          type="button"
          onClick={() => setOpen((current) => !current)}
        >
          {open ? "▾" : "▸"}
        </button>
        <button
          aria-label={detailParts(text("selectFlowchart"), section.label, sectionHint)}
          className="min-w-0 flex-1 truncate text-left text-xs font-semibold text-[#9fb0c5] hover:text-[#f1f5f9]"
          type="button"
          onClick={onSelect}
        >
          <span>{section.label}</span>
        </button>
        <FlowchartHint hint={sectionHint} label={section.label} />
        <button
          className="shrink-0 rounded border border-[#334255] px-2 py-0.5 text-[11px] text-[#c4d4e8] hover:border-[#6fb6ff] hover:text-white disabled:cursor-not-allowed disabled:border-[#263140] disabled:text-[#5f6d7e]"
          disabled={!section.range}
          title={text("openCode")}
          type="button"
          onClick={() => section.range && onOpenCode(section.range)}
        >
          Code
        </button>
        <button
          className="shrink-0 rounded border border-[#334255] px-2 py-0.5 text-[11px] text-[#c4d4e8] hover:border-[#6fb6ff] hover:text-white disabled:cursor-not-allowed disabled:border-[#263140] disabled:text-[#5f6d7e]"
          disabled={!section.range}
          title={text("openGraph")}
          type="button"
          onClick={() => section.range && onOpenGraph(section.range)}
        >
          Graph
        </button>
      </div>
      {open ? (
        <div className="p-2">
          {visibleNodes.length === 0 ? (
            <EmptyText>{text("emptySection")}</EmptyText>
          ) : (
            <VirtualList
              className="grid gap-1"
              estimateSize={(node) => (node.links?.length ? 76 : 42)}
              getKey={(node) => node.id}
              items={visibleNodes}
              maxHeight={360}
              overscan={10}
              scrollToIndex={activeNodeIndex >= 0 ? activeNodeIndex : undefined}
              renderItem={(node) => {
                const isSearchMatch = matchedNodeIds.has(node.id);
                const isActiveSearchMatch = activeSearchNodeId === node.id;
                const nodeHint = flowchartNodeHint(node, text, locale);
                const nodeStyle = themePalette.nodeKindStyles[node.kind];
                return (
                  <div
                    className={cn(
                      "rounded px-1 py-1 hover:bg-[#223044]",
                      isActiveSearchMatch
                        ? "bg-[#17324a] ring-1 ring-[#7dd3fc]"
                        : isSearchMatch && "bg-[#2b2b18] ring-1 ring-[#f6c177]",
                    )}
                  >
                    <div className="grid grid-cols-[minmax(0,1fr)_auto_auto] items-center gap-1">
                      <button
                        className="min-w-0 overflow-hidden text-ellipsis whitespace-nowrap px-1 py-1 text-left text-xs text-[#d9e0ea] hover:text-white"
                        title={detailParts(text("openFlowchart"), node.label, nodeHint)}
                        type="button"
                        onClick={() => onSelectNode(node)}
                      >
                        <span
                          className="mr-2 rounded border px-1 py-0.5 text-[10px]"
                          style={flowchartSwatchStyle(nodeStyle)}
                          title={flowchartNodeKindLabel(node.kind, locale)}
                        >
                          {flowchartNodeKindLabel(node.kind, locale)}
                        </span>
                        <span title={node.label}>{node.label}</span>
                      </button>
                      <button
                        className="mr-1 shrink-0 rounded border border-[#3b4a5f] px-1.5 py-0.5 text-[11px] text-[#c4d4e8] hover:border-[#7dd3fc] hover:text-white disabled:cursor-not-allowed disabled:border-[#263140] disabled:text-[#5f6d7e]"
                        disabled={!node.range}
                        title={text("openCode")}
                        type="button"
                        onClick={() => node.range && onOpenCode(node.range)}
                      >
                        Code
                      </button>
                      <button
                        className="mr-1 shrink-0 rounded border border-[#3b4a5f] px-1.5 py-0.5 text-[11px] text-[#c4d4e8] hover:border-[#7dd3fc] hover:text-white disabled:cursor-not-allowed disabled:border-[#263140] disabled:text-[#5f6d7e]"
                        disabled={!node.range}
                        title={text("openGraph")}
                        type="button"
                        onClick={() => node.range && onOpenGraph(node.range)}
                      >
                        Graph
                      </button>
                    </div>
                    {node.links?.length ? (
                      <div className="ml-1 mt-1 flex flex-wrap gap-1">
                        {node.links.map((link) => {
                          const linkStyle = flowchartNodeLinkStyle(link, themePalette);
                          return (
                            <button
                              key={link.id}
                              className="max-w-full overflow-hidden text-ellipsis whitespace-nowrap rounded border px-1.5 py-0.5 text-[11px] hover:text-white"
                              style={flowchartSwatchStyle(linkStyle)}
                              title={flowchartNodeLinkHint(link, text, locale)}
                              type="button"
                              onClick={() => onOpenTarget(link.target)}
                            >
                              {link.label}
                            </button>
                          );
                        })}
                      </div>
                    ) : null}
                  </div>
                );
              }}
            />
          )}
        </div>
      ) : null}
    </div>
  );
}

interface FlowchartContextMenuState {
  node: AspFlowchartNode;
  x: number;
  y: number;
}

interface FlowchartSvgSize {
  width: number;
  height: number;
}

function FlowchartPaneResizeHandle({
  className,
  label,
  maxWidth,
  minWidth,
  onWidthChange,
  position,
  width,
}: {
  className?: string;
  label: string;
  maxWidth: number;
  minWidth: number;
  onWidthChange(width: number): void;
  position: InfoPanelPosition;
  width: number;
}): React.ReactElement {
  const updateWidth = useCallback(
    (nextWidth: number) => onWidthChange(clamp(nextWidth, minWidth, maxWidth)),
    [maxWidth, minWidth, onWidthChange],
  );
  const handlePointerDown = useCallback(
    (event: React.PointerEvent<HTMLDivElement>) => {
      if (event.button !== 0) {
        return;
      }
      event.preventDefault();
      const startX = event.clientX;
      const startWidth = width;
      const previousCursor = document.body.style.cursor;
      const previousUserSelect = document.body.style.userSelect;
      document.body.style.cursor = "col-resize";
      document.body.style.userSelect = "none";

      const handlePointerMove = (moveEvent: PointerEvent) => {
        moveEvent.preventDefault();
        const deltaX = moveEvent.clientX - startX;
        updateWidth(position === "left" ? startWidth + deltaX : startWidth - deltaX);
      };
      const stopResize = () => {
        document.body.style.cursor = previousCursor;
        document.body.style.userSelect = previousUserSelect;
        window.removeEventListener("pointermove", handlePointerMove);
        window.removeEventListener("pointerup", stopResize);
        window.removeEventListener("pointercancel", stopResize);
      };

      window.addEventListener("pointermove", handlePointerMove);
      window.addEventListener("pointerup", stopResize);
      window.addEventListener("pointercancel", stopResize);
    },
    [position, updateWidth, width],
  );
  const handleKeyDown = useCallback(
    (event: React.KeyboardEvent<HTMLDivElement>) => {
      if (event.key === "ArrowLeft") {
        event.preventDefault();
        updateWidth(
          width +
            (position === "left"
              ? -flowchartPaneResizeKeyboardStep
              : flowchartPaneResizeKeyboardStep),
        );
      } else if (event.key === "ArrowRight") {
        event.preventDefault();
        updateWidth(
          width +
            (position === "left"
              ? flowchartPaneResizeKeyboardStep
              : -flowchartPaneResizeKeyboardStep),
        );
      } else if (event.key === "Home") {
        event.preventDefault();
        updateWidth(minWidth);
      } else if (event.key === "End") {
        event.preventDefault();
        updateWidth(maxWidth);
      }
    },
    [maxWidth, minWidth, position, updateWidth, width],
  );

  return (
    <div
      role="separator"
      tabIndex={0}
      aria-label={label}
      aria-orientation="vertical"
      aria-valuemin={minWidth}
      aria-valuemax={maxWidth}
      aria-valuenow={width}
      title={label}
      className={cn(
        "relative cursor-col-resize bg-[#101820] outline-none before:absolute before:inset-y-0 before:left-1/2 before:w-px before:-translate-x-1/2 before:bg-[#263140] hover:bg-[#172131] focus:bg-[#172131] focus:before:bg-[#7dd3fc]",
        className ?? "order-2",
      )}
      onKeyDown={handleKeyDown}
      onPointerDown={handlePointerDown}
    />
  );
}

function FlowchartSourcePanel({
  activeLabel,
  className,
  highlights,
  nodes,
  scrollTarget,
  sourceText,
  text,
  theme,
  onSelectNode,
}: {
  activeLabel?: string;
  className?: string;
  highlights: readonly FlowchartSourceHighlight[];
  nodes: readonly AspFlowchartNode[];
  scrollTarget?: FlowchartSourceScrollTarget;
  sourceText?: string;
  text(key: string): string;
  theme: WebviewTheme;
  onSelectNode(node: AspFlowchartNode): void;
}): React.ReactElement {
  const preRef = useRef<HTMLPreElement | null>(null);
  const previousScrollKindRef = useRef<FlowchartSourceActiveKind | undefined>(undefined);
  const consumedSectionScrollKeysRef = useRef(new Set<string>());
  const [highlightedCode, setHighlightedCode] = useState<HighlightedCode>();
  const [highlightError, setHighlightError] = useState<string>();
  const scrollLineNumber = flowchartSourceFirstLineNumber(scrollTarget?.ranges);
  const highlightedCodeWithActiveRange = useMemo(
    () =>
      highlightedCode
        ? flowchartHighlightedCodeWithSourceHighlights(highlightedCode, highlights)
        : undefined,
    [highlightedCode, highlights],
  );
  const selectSourceLineNode = useCallback(
    (lineNumber: number) => {
      const node = flowchartNodeForSourceLine(nodes, lineNumber);
      if (node) {
        onSelectNode(node);
      }
    },
    [nodes, onSelectNode],
  );
  const handleSourceCodeClick = useCallback(
    (event: React.MouseEvent<HTMLElement>) => {
      const lineNumber = sourceLineNumberFromEvent(event);
      if (lineNumber !== undefined) {
        selectSourceLineNode(lineNumber);
      }
    },
    [selectSourceLineNode],
  );
  const handlers = useMemo<AnnotationHandler[]>(
    () => [
      {
        name: "flowchartSourceLine",
        Line: (props) => (
          <InnerLine
            merge={props}
            className="asp-lsp-source-line"
            data-source-line={props.lineNumber}
          />
        ),
      },
      {
        name: flowchartSourceActiveAnnotationName,
        Block: ({ annotation, children }) => (
          <div
            className={flowchartSourceActiveBlockClassName(
              flowchartSourceAnnotationKind(annotation),
            )}
          >
            {children}
          </div>
        ),
        AnnotatedLine: (props) => (
          <InnerLine
            merge={props}
            className={flowchartSourceActiveLineClassName(
              flowchartSourceAnnotationKind(props.annotation),
            )}
          />
        ),
      },
    ],
    [],
  );

  useEffect(() => {
    let cancelled = false;
    if (!sourceText) {
      setHighlightedCode(undefined);
      setHighlightError(undefined);
      return;
    }
    const run = async (): Promise<void> => {
      try {
        const code = await highlight(
          { value: sourceText, lang: "vb", meta: "" },
          theme === "dark" ? "github-dark" : "github-light",
        );
        if (!cancelled) {
          setHighlightedCode(code);
          setHighlightError(undefined);
        }
      } catch (error) {
        if (!cancelled) {
          setHighlightedCode(undefined);
          setHighlightError(error instanceof Error ? error.message : String(error));
        }
      }
    };
    void run();
    return () => {
      cancelled = true;
    };
  }, [sourceText, theme]);

  useLayoutEffect(() => {
    if (!scrollTarget || !scrollLineNumber || !preRef.current) {
      previousScrollKindRef.current = scrollTarget?.kind;
      return;
    }
    const previousKind = previousScrollKindRef.current;
    if (
      scrollTarget.kind === "section" &&
      (consumedSectionScrollKeysRef.current.has(scrollTarget.key) ||
        previousKind === "hover" ||
        previousKind === "selection")
    ) {
      consumedSectionScrollKeysRef.current.add(scrollTarget.key);
      previousScrollKindRef.current = scrollTarget.kind;
      return;
    }
    const frame = window.requestAnimationFrame(() => {
      const pre = preRef.current;
      if (pre && scrollSourceLineIntoView(pre, scrollLineNumber)) {
        if (scrollTarget.kind === "section") {
          consumedSectionScrollKeysRef.current.add(scrollTarget.key);
        }
      }
    });
    previousScrollKindRef.current = scrollTarget.kind;
    return () => window.cancelAnimationFrame(frame);
  }, [highlightedCodeWithActiveRange, scrollLineNumber, scrollTarget]);

  return (
    <aside className={cn(className, "flex min-h-0 min-w-0 flex-col bg-[#101820]")}>
      <header className="border-b border-[#263140] px-3 py-2">
        <div className="flex min-w-0 items-center gap-2">
          <div className="min-w-0 flex-1 truncate text-xs font-semibold uppercase tracking-wide text-[#9fb0c5]">
            {text("source")}
          </div>
          {activeLabel ? (
            <div
              className="min-w-0 flex-1 truncate text-right text-[11px] text-[#c4d4e8]"
              title={activeLabel}
            >
              {activeLabel}
            </div>
          ) : null}
        </div>
      </header>
      <div className="min-h-0 flex-1 overflow-hidden" onClick={handleSourceCodeClick}>
        {sourceText ? (
          highlightedCodeWithActiveRange ? (
            <Pre
              ref={preRef}
              code={highlightedCodeWithActiveRange}
              handlers={handlers}
              className="asp-lsp-source-code h-full overflow-auto bg-[#0c1117] p-3 text-xs leading-5"
              style={highlightedCodeWithActiveRange.style}
            />
          ) : (
            <pre
              ref={preRef}
              className="asp-lsp-source-code h-full overflow-auto bg-[#0c1117] p-3 text-xs leading-5 text-[#d9e0ea]"
            >
              {highlightError ? `${highlightError}\n\n` : null}
              <FlowchartSourcePlainText sourceText={sourceText} />
            </pre>
          )
        ) : (
          <EmptyText>{text("sourceEmpty")}</EmptyText>
        )}
      </div>
    </aside>
  );
}

function FlowchartSourcePlainText({ sourceText }: { sourceText: string }): React.ReactElement {
  return (
    <>
      {sourceText.split(/\r\n|\r|\n/).map((line, index) => (
        <span key={index} className="asp-lsp-source-line" data-source-line={index + 1}>
          {line}
        </span>
      ))}
    </>
  );
}

function sourceLineNumberFromEvent(event: React.MouseEvent<HTMLElement>): number | undefined {
  const target = event.target instanceof Element ? event.target : undefined;
  const line = target?.closest<HTMLElement>("[data-source-line]");
  return sourceLineNumber(line?.dataset.sourceLine);
}

function sourceLineNumber(value: string | undefined): number | undefined {
  const lineNumber = value ? Number(value) : NaN;
  return Number.isInteger(lineNumber) && lineNumber > 0 ? lineNumber : undefined;
}

function flowchartNodeForSourceLine(
  nodes: readonly AspFlowchartNode[],
  lineNumber: number,
): AspFlowchartNode | undefined {
  const sourceLine = lineNumber - 1;
  return nodes
    .filter((node): node is AspFlowchartNode & { range: FlowchartSourceRange } =>
      Boolean(
        node.range &&
        node.kind !== "start" &&
        node.kind !== "end" &&
        flowchartRangeContainsSourceLine(node.range, sourceLine),
      ),
    )
    .sort((left, right) => rangeLength(left.range) - rangeLength(right.range))[0];
}

function flowchartRangeContainsSourceLine(range: FlowchartSourceRange, line: number): boolean {
  return range.start.line <= line && line <= flowchartSourceHighlightEndLine(range);
}

function flowchartSourceHighlightEndLine(range: FlowchartSourceRange): number {
  return range.end.character === 0 && range.end.line > range.start.line
    ? range.end.line - 1
    : range.end.line;
}

function flowchartHighlightedCodeWithSourceHighlights(
  code: HighlightedCode,
  highlights: readonly FlowchartSourceHighlight[],
): HighlightedCode {
  const annotations = code.annotations.filter(
    (annotation) => annotation.name !== flowchartSourceActiveAnnotationName,
  );
  for (const highlight of flowchartSourceHighlightsByPriority(highlights)) {
    for (const range of highlight.ranges) {
      annotations.push(flowchartSourceActiveAnnotation(range, highlight.kind));
    }
  }
  return { ...code, annotations };
}

function flowchartSourceActiveAnnotation(
  range: FlowchartSourceRange,
  kind: FlowchartSourceActiveKind,
): CodeAnnotation {
  return {
    name: flowchartSourceActiveAnnotationName,
    query: kind,
    fromLineNumber: range.start.line + 1,
    toLineNumber: flowchartSourceHighlightEndLine(range) + 1,
    data: { kind },
  };
}

function flowchartSourceFirstLineNumber(
  ranges: readonly FlowchartSourceRange[] | undefined,
): number | undefined {
  return ranges?.[0] ? ranges[0].start.line + 1 : undefined;
}

function flowchartSourceAnnotationKind(annotation: CodeAnnotation): FlowchartSourceActiveKind {
  const kind = annotation.data?.kind;
  return kind === "hover" || kind === "selection" || kind === "section" ? kind : "section";
}

function flowchartSourceActiveBlockClassName(kind: FlowchartSourceActiveKind): string {
  return `asp-lsp-source-active-block asp-lsp-source-active-block--${kind}`;
}

function flowchartSourceActiveLineClassName(kind: FlowchartSourceActiveKind): string {
  return `asp-lsp-source-active-line asp-lsp-source-active-line--${kind}`;
}

function scrollSourceLineIntoView(container: HTMLElement, lineNumber: number): boolean {
  const line = container.querySelector<HTMLElement>(`[data-source-line="${lineNumber}"]`);
  if (!line) {
    return false;
  }
  const containerRect = container.getBoundingClientRect();
  const lineRect = line.getBoundingClientRect();
  const isAbove = lineRect.top < containerRect.top;
  const isBelow = lineRect.bottom > containerRect.bottom;
  if (!isAbove && !isBelow) {
    return true;
  }
  const nextTop =
    container.scrollTop +
    lineRect.top +
    lineRect.height / 2 -
    containerRect.top -
    container.clientHeight / 2;
  container.scrollTo({ top: Math.max(0, nextTop), behavior: "smooth" });
  return true;
}

function flowchartSourceHighlights(
  selectedSection: AspFlowchartSection | undefined,
  selectedSectionNodes: readonly AspFlowchartNode[],
  allNodes: readonly AspFlowchartNode[],
  hoveredNodeId: string | undefined,
  selectedNodeId: string | undefined,
): FlowchartSourceHighlight[] {
  const highlights: FlowchartSourceHighlight[] = [];
  const nodesById = flowchartNodesById(allNodes);
  const sectionRanges = flowchartSourceRangesForSection(selectedSection, selectedSectionNodes);
  if (sectionRanges.length > 0) {
    highlights.push({ kind: "section", label: selectedSection?.label, ranges: sectionRanges });
  }
  const selectedNode = flowchartNodeById(nodesById, selectedNodeId);
  if (selectedNode?.range) {
    highlights.push({ kind: "selection", label: selectedNode.label, ranges: [selectedNode.range] });
  }
  const hoveredNode = flowchartNodeById(nodesById, hoveredNodeId);
  if (hoveredNode?.range) {
    highlights.push({ kind: "hover", label: hoveredNode.label, ranges: [hoveredNode.range] });
  }
  return highlights;
}

function flowchartPrimarySourceHighlight(
  highlights: readonly FlowchartSourceHighlight[],
): FlowchartSourceHighlight | undefined {
  return [...highlights].sort(
    (left, right) =>
      flowchartSourceHighlightPriority(right.kind) - flowchartSourceHighlightPriority(left.kind),
  )[0];
}

function flowchartSourceScrollTarget(
  highlights: readonly FlowchartSourceHighlight[],
  context: {
    activeNodeId?: string;
    hoveredNodeId?: string;
    sectionId?: string;
    sectionSequence: number;
    uri: string;
  },
): FlowchartSourceScrollTarget | undefined {
  const hovered = highlights.find((highlight) => highlight.kind === "hover");
  if (hovered) {
    return {
      kind: "hover",
      key: `hover:${context.uri}:${context.hoveredNodeId ?? ""}`,
      ranges: hovered.ranges,
    };
  }
  const selection = highlights.find((highlight) => highlight.kind === "selection");
  if (selection) {
    return {
      kind: "selection",
      key: `selection:${context.uri}:${context.activeNodeId ?? ""}`,
      ranges: selection.ranges,
    };
  }
  const section = highlights.find((highlight) => highlight.kind === "section");
  if (!section) {
    return undefined;
  }
  return {
    kind: "section",
    key: `section:${context.uri}:${context.sectionId ?? ""}:${context.sectionSequence}`,
    ranges: section.ranges,
  };
}

function flowchartSourceHighlightsByPriority(
  highlights: readonly FlowchartSourceHighlight[],
): FlowchartSourceHighlight[] {
  return [...highlights].sort(
    (left, right) =>
      flowchartSourceHighlightPriority(left.kind) - flowchartSourceHighlightPriority(right.kind),
  );
}

function flowchartSourceHighlightPriority(kind: FlowchartSourceActiveKind): number {
  switch (kind) {
    case "hover":
      return 3;
    case "selection":
      return 2;
    case "section":
      return 1;
  }
}

function flowchartNodesById(nodes: readonly AspFlowchartNode[]): Map<string, AspFlowchartNode> {
  return new Map(nodes.map((node) => [node.id, node]));
}

function flowchartNodeById(
  nodesById: Map<string, AspFlowchartNode>,
  id: string | undefined,
): AspFlowchartNode | undefined {
  return id ? nodesById.get(id) : undefined;
}

function flowchartSourceRangesForSection(
  section: AspFlowchartSection | undefined,
  nodes: readonly AspFlowchartNode[],
): FlowchartSourceRange[] {
  if (!section) {
    return [];
  }
  if (section.kind !== "topLevel") {
    return section.range ? [section.range] : [];
  }
  return mergeFlowchartSourceRanges(
    nodes
      .filter((node) => node.kind !== "start" && node.kind !== "end")
      .map((node) => node.range)
      .filter((range): range is FlowchartSourceRange => Boolean(range)),
  );
}

function mergeFlowchartSourceRanges(
  ranges: readonly FlowchartSourceRange[],
): FlowchartSourceRange[] {
  const sorted = [...ranges].sort(compareFlowchartSourceRangeStart);
  const merged: FlowchartSourceRange[] = [];
  for (const range of sorted) {
    const current = merged.at(-1);
    if (!current || !flowchartSourceRangesTouchOrOverlap(current, range)) {
      merged.push(cloneFlowchartSourceRange(range));
      continue;
    }
    current.end = laterFlowchartPosition(current.end, range.end);
  }
  return merged;
}

function cloneFlowchartSourceRange(range: FlowchartSourceRange): FlowchartSourceRange {
  return {
    start: { ...range.start },
    end: { ...range.end },
  };
}

function compareFlowchartSourceRangeStart(
  left: FlowchartSourceRange,
  right: FlowchartSourceRange,
): number {
  return compareFlowchartPosition(left.start, right.start);
}

function flowchartSourceRangesTouchOrOverlap(
  left: FlowchartSourceRange,
  right: FlowchartSourceRange,
): boolean {
  return (
    compareFlowchartPosition(right.start, left.end) <= 0 || right.start.line <= left.end.line + 1
  );
}

function laterFlowchartPosition(
  left: FlowchartSourceRange["end"],
  right: FlowchartSourceRange["end"],
): FlowchartSourceRange["end"] {
  return compareFlowchartPosition(left, right) >= 0 ? left : right;
}

function compareFlowchartPosition(
  left: FlowchartSourceRange["start"],
  right: FlowchartSourceRange["start"],
): number {
  return left.line === right.line ? left.character - right.character : left.line - right.line;
}

function FlowchartCanvas({
  className,
  labelMode,
  payload,
  section,
  themePalette,
  text,
  activeSearchNodeId,
  matchedNodeIds,
  onOpenCode,
  onOpenFlowchart,
  onOpenGraph,
  onLabelModeChange,
  sourcePanelVisible,
  onSourcePanelVisibleChange,
  onHoverNode,
  onSelectNode,
}: {
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
  onOpenGraph(range: AspFlowchartNode["range"] | AspFlowchartSection["range"]): void;
  onLabelModeChange(mode: AspFlowchartLabelMode): void;
  sourcePanelVisible: boolean;
  onSourcePanelVisibleChange(value: boolean): void;
  onHoverNode(nodeId: string | undefined): void;
  onSelectNode(node: AspFlowchartNode): void;
}): React.ReactElement {
  const viewportRef = useRef<HTMLDivElement | null>(null);
  const containerRef = useRef<HTMLDivElement | null>(null);
  const panStateRef = useRef<FlowchartPanState | undefined>(undefined);
  const suppressNextCanvasClickRef = useRef(false);
  const userPannedFlowchartKeyRef = useRef<string | undefined>(undefined);
  const [error, setError] = useState<string>();
  const [svg, setSvg] = useState<string>("");
  const [svgSize, setSvgSize] = useState<FlowchartSvgSize>();
  const [zoom, setZoom] = useState(1);
  const [viewportSize, setViewportSize] = useState<FlowchartViewportSize>({ width: 0, height: 0 });
  const [isPanning, setIsPanning] = useState(false);
  const [contextMenu, setContextMenu] = useState<FlowchartContextMenuState>();
  const flowchartViewKey = useMemo(
    () => `${payload.uri}\n${section?.id ?? ""}\n${payload.mermaid}`,
    [payload.mermaid, payload.uri, section?.id],
  );
  const zoomRange = useMemo(
    () => flowchartZoomRange(payload),
    [payload.settings?.maxZoom, payload.settings?.minZoom],
  );
  const setClampedZoom = useCallback(
    (value: number) =>
      setZoom(roundFlowchartZoom(clamp(value, zoomRange.minimum, zoomRange.maximum))),
    [zoomRange],
  );
  const adjustZoom = useCallback(
    (direction: 1 | -1) => setClampedZoom(zoom + direction * flowchartZoomStep),
    [setClampedZoom, zoom],
  );
  const fitFlowchartWidth = useCallback(() => {
    if (!viewportRef.current || !svgSize) {
      return;
    }
    const nextZoom = flowchartFitWidthZoom(viewportRef.current, svgSize, zoomRange);
    if (nextZoom !== undefined) {
      userPannedFlowchartKeyRef.current = undefined;
      setClampedZoom(nextZoom);
    }
  }, [setClampedZoom, svgSize, zoomRange]);
  const handleWheel = useCallback(
    (event: React.WheelEvent<HTMLDivElement>) => {
      if (!event.ctrlKey && !event.metaKey) {
        return;
      }
      event.preventDefault();
      const direction = event.deltaY < 0 ? 1 : -1;
      setClampedZoom(zoom + direction * flowchartZoomStep);
    },
    [setClampedZoom, zoom],
  );
  const beginCanvasPan = useCallback((event: React.PointerEvent<HTMLDivElement>) => {
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
  }, []);
  const moveCanvasPan = useCallback((event: React.PointerEvent<HTMLDivElement>) => {
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
  }, []);
  const endCanvasPan = useCallback(
    (event: React.PointerEvent<HTMLDivElement>) => {
      const pan = panStateRef.current;
      if (!pan || pan.pointerId !== event.pointerId || !viewportRef.current) {
        return;
      }
      if (viewportRef.current.hasPointerCapture(event.pointerId)) {
        viewportRef.current.releasePointerCapture(event.pointerId);
      }
      if (pan.moved) {
        userPannedFlowchartKeyRef.current = flowchartViewKey;
        suppressNextCanvasClickRef.current = true;
        window.setTimeout(() => {
          suppressNextCanvasClickRef.current = false;
        }, 0);
      }
      panStateRef.current = undefined;
      setIsPanning(false);
    },
    [flowchartViewKey],
  );
  const suppressCanvasClickAfterPan = useCallback((event: React.MouseEvent<HTMLDivElement>) => {
    if (!suppressNextCanvasClickRef.current) {
      return;
    }
    event.preventDefault();
    event.stopPropagation();
  }, []);
  const openContextMenu = useCallback((node: AspFlowchartNode, event: MouseEvent) => {
    event.preventDefault();
    setContextMenu({ node, x: event.clientX, y: event.clientY });
  }, []);
  const closeContextMenu = useCallback(() => setContextMenu(undefined), []);
  const openContextMenuCode = useCallback(() => {
    if (!contextMenu?.node.range) {
      return;
    }
    onOpenCode(contextMenu.node.range);
    closeContextMenu();
  }, [closeContextMenu, contextMenu, onOpenCode]);
  const openContextMenuFlowchart = useCallback(() => {
    if (!contextMenu) {
      return;
    }
    onOpenFlowchart(contextMenu.node);
    closeContextMenu();
  }, [closeContextMenu, contextMenu, onOpenFlowchart]);
  const selectContextMenuNode = useCallback(() => {
    if (!contextMenu) {
      return;
    }
    onSelectNode(contextMenu.node);
    closeContextMenu();
  }, [closeContextMenu, contextMenu, onSelectNode]);
  const openContextMenuGraph = useCallback(() => {
    if (!contextMenu?.node.range) {
      return;
    }
    onOpenGraph(contextMenu.node.range);
    closeContextMenu();
  }, [closeContextMenu, contextMenu, onOpenGraph]);
  useEffect(() => {
    setZoom((currentZoom) =>
      roundFlowchartZoom(clamp(currentZoom, zoomRange.minimum, zoomRange.maximum)),
    );
  }, [zoomRange]);
  useEffect(() => {
    userPannedFlowchartKeyRef.current = undefined;
  }, [flowchartViewKey]);
  useLayoutEffect(() => {
    const viewport = viewportRef.current;
    if (!viewport) {
      return undefined;
    }
    const updateViewportSize = (): void => {
      setViewportSize({ width: viewport.clientWidth, height: viewport.clientHeight });
    };
    updateViewportSize();
    const resizeObserver = new ResizeObserver(updateViewportSize);
    resizeObserver.observe(viewport);
    return () => resizeObserver.disconnect();
  }, []);
  useLayoutEffect(() => {
    const viewport = viewportRef.current;
    if (
      !viewport ||
      !svgSize ||
      viewportSize.width <= 0 ||
      userPannedFlowchartKeyRef.current === flowchartViewKey
    ) {
      return;
    }
    centerFlowchartHorizontally(viewport, svgSize, zoom, viewportSize);
  }, [flowchartViewKey, svgSize, viewportSize, zoom]);
  useEffect(() => {
    if (!contextMenu) {
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
  }, [closeContextMenu, contextMenu]);
  useEffect(() => {
    let cancelled = false;
    const render = async (): Promise<void> => {
      if (!containerRef.current) {
        return;
      }
      mermaid.initialize({
        startOnLoad: false,
        maxTextSize: flowchartMaxTextSize(payload),
        maxEdges: flowchartMaxEdges(payload),
        securityLevel: "strict",
        theme: themePalette.mermaidTheme,
        themeVariables: themePalette.mermaidThemeVariables,
        flowchart: { htmlLabels: false, curve: "basis" },
      });
      try {
        const id = `asp-lsp-flowchart-${Date.now().toString(36)}`;
        const result = await mermaid.render(id, payload.mermaid || "flowchart TB");
        if (cancelled || !containerRef.current) {
          return;
        }
        containerRef.current.innerHTML = result.svg;
        setSvg(containerRef.current.querySelector("svg")?.outerHTML ?? result.svg);
        setSvgSize(measuredFlowchartSvgSize(containerRef.current));
        attachSvgNodeHandlers(
          containerRef.current,
          payload,
          text,
          openContextMenu,
          onHoverNode,
          onOpenFlowchart,
        );
        setError(undefined);
      } catch (renderError) {
        if (!cancelled) {
          setError(renderError instanceof Error ? renderError.message : String(renderError));
          setSvg("");
          setSvgSize(undefined);
        }
      }
    };
    void render();
    return () => {
      cancelled = true;
    };
  }, [onHoverNode, onOpenFlowchart, onSelectNode, openContextMenu, payload, themePalette, text]);
  useEffect(() => {
    if (!containerRef.current || !viewportRef.current) {
      return;
    }
    syncSvgSearchHighlights(
      containerRef.current,
      viewportRef.current,
      payload,
      matchedNodeIds,
      activeSearchNodeId,
    );
  }, [activeSearchNodeId, matchedNodeIds, payload, svg]);
  const exportSvg = useCallback(() => {
    vscode.postMessage({
      type: "exportFlowchart",
      format: "svg",
      uri: payload.uri,
      sectionLabel: section?.label,
      content: serializedFlowchartSvg(containerRef.current) ?? svg,
    });
  }, [payload.uri, section?.label, svg]);
  const copyMermaid = useCallback(() => {
    vscode.postMessage({
      type: "copyText",
      content: payload.mermaid,
    });
  }, [payload.mermaid]);
  const exportMermaid = useCallback(() => {
    vscode.postMessage({
      type: "exportFlowchart",
      format: "mermaid",
      uri: payload.uri,
      sectionLabel: section?.label,
      content: `${payload.mermaid}\n`,
    });
  }, [payload.mermaid, payload.uri, section?.label]);
  const contextMenuPosition = contextMenu
    ? clampedContextMenuPosition(contextMenu.x, contextMenu.y)
    : undefined;
  const canFitFlowchartWidth = Boolean(svgSize);
  return (
    <section
      className={cn(className, "grid min-h-0 grid-rows-[auto_1fr] overflow-hidden bg-[#0d1117]")}
    >
      <header className="flex min-w-0 items-center gap-2 border-b border-[#263140] px-4 py-3">
        <div
          className="min-w-0 flex-1 truncate text-sm font-semibold text-[#f1f5f9]"
          title={section?.label ?? text("title")}
        >
          {section?.label ?? text("title")}
        </div>
        <FlowchartToolbar
          canExportSvg={Boolean(svg)}
          canFitFlowchartWidth={canFitFlowchartWidth}
          canOpenSection={Boolean(section?.range)}
          labelMode={labelMode}
          text={text}
          zoom={zoom}
          onLabelModeChange={onLabelModeChange}
          onCopyMermaid={copyMermaid}
          onExportMermaid={exportMermaid}
          onExportSvg={exportSvg}
          onFitFlowchartWidth={fitFlowchartWidth}
          onOpenCode={() => section?.range && onOpenCode(section.range)}
          onOpenGraph={() => section?.range && onOpenGraph(section.range)}
          onResetZoom={() => setClampedZoom(1)}
          sourcePanelVisible={sourcePanelVisible}
          onSourcePanelVisibleChange={onSourcePanelVisibleChange}
          onZoomIn={() => adjustZoom(1)}
          onZoomOut={() => adjustZoom(-1)}
        />
      </header>
      <div
        ref={viewportRef}
        className={cn(
          "min-h-0 overflow-auto p-4 [scrollbar-gutter:stable] [touch-action:none]",
          isPanning ? "cursor-grabbing" : "cursor-grab",
        )}
        onPointerCancel={endCanvasPan}
        onPointerDown={beginCanvasPan}
        onPointerMove={moveCanvasPan}
        onPointerUp={endCanvasPan}
        onClickCapture={suppressCanvasClickAfterPan}
        onWheel={handleWheel}
      >
        {error ? (
          <div className="rounded border border-[#7f3434] bg-[#291416] p-3 text-sm text-[#ffd2cc]">
            {text("renderError")} {error}
          </div>
        ) : null}
        <div
          className="relative select-none"
          style={scaledFlowchartCanvasStyle(svgSize, zoom, viewportSize)}
        >
          <div
            ref={containerRef}
            className="absolute top-0 inline-block origin-top-left [&_svg]:block [&_svg]:h-full [&_svg]:w-full [&_svg]:max-w-none"
            style={flowchartSvgLayerStyle(svgSize, zoom, viewportSize)}
          />
        </div>
        {contextMenu && contextMenuPosition ? (
          <div
            className="fixed z-50 grid min-w-40 overflow-hidden rounded-md border border-[#3b4a5f] bg-[#151b23] py-1 text-xs text-[#d9e0ea] shadow-[0_12px_28px_rgb(0_0_0_/_32%)]"
            role="menu"
            style={{ left: contextMenuPosition.left, top: contextMenuPosition.top }}
            onContextMenu={(event) => event.preventDefault()}
            onPointerDown={(event) => event.stopPropagation()}
          >
            <button
              className="px-3 py-1.5 text-left hover:bg-[#172131]"
              role="menuitem"
              type="button"
              onClick={selectContextMenuNode}
            >
              {text("selectNode")}
            </button>
            <button
              className="px-3 py-1.5 text-left hover:bg-[#172131] disabled:cursor-not-allowed disabled:text-[#5f6d7e]"
              disabled={!contextMenu.node.range}
              role="menuitem"
              type="button"
              onClick={openContextMenuCode}
            >
              Code
            </button>
            <button
              className="px-3 py-1.5 text-left hover:bg-[#172131]"
              role="menuitem"
              type="button"
              onClick={openContextMenuFlowchart}
            >
              {text("openFlowchart")}
            </button>
            <button
              className="px-3 py-1.5 text-left hover:bg-[#172131] disabled:cursor-not-allowed disabled:text-[#5f6d7e]"
              disabled={!contextMenu.node.range}
              role="menuitem"
              type="button"
              onClick={openContextMenuGraph}
            >
              Graph
            </button>
          </div>
        ) : null}
      </div>
    </section>
  );
}

function sectionIdForNodeFlowchart(payload: FlowchartPayload, node: AspFlowchartNode): string {
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

function openFlowchartTarget(
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

function sectionIdForRange(
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

function flowchartNodeForRange(
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
    .filter((node): node is AspFlowchartNode & { range: NonNullable<AspFlowchartNode["range"]> } =>
      Boolean(node.range && matches(node.range)),
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
    (range.end.line - range.start.line) * 1_000_000 + range.end.character - range.start.character
  );
}

function positionBeforeOrEqual(
  left: { line: number; character: number },
  right: { line: number; character: number },
): boolean {
  return left.line < right.line || (left.line === right.line && left.character <= right.character);
}

function isRange(value: unknown): value is NonNullable<AspFlowchartTarget["range"]> {
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

function nodesBySectionId(payload: FlowchartPayload): Map<string, AspFlowchartNode[]> {
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

function SectionHeading({ children }: { children: React.ReactNode }): React.ReactElement {
  return (
    <h2 className="mb-2 mt-3 text-xs font-semibold uppercase tracking-wide text-[#91a4bb]">
      {children}
    </h2>
  );
}

function EmptyText({ children }: { children: React.ReactNode }): React.ReactElement {
  return (
    <div className="mb-3 rounded border border-dashed border-[#2f3d50] p-2 text-xs text-[#8190a4]">
      {children}
    </div>
  );
}

function isFlowchartPayload(value: unknown): value is FlowchartPayload {
  return Boolean(value && typeof value === "object" && "mermaid" in value && "nodes" in value);
}

function flowchartMaxTextSize(payload: FlowchartPayload): number {
  const value = payload.settings?.maxTextSize;
  return typeof value === "number" && Number.isFinite(value) && value >= 1
    ? Math.floor(value)
    : defaultFlowchartMaxTextSize;
}

function flowchartMaxEdges(payload: FlowchartPayload): number {
  const value = payload.settings?.maxEdges;
  return typeof value === "number" && Number.isFinite(value) && value >= 1
    ? Math.floor(value)
    : defaultFlowchartMaxEdges;
}

const style = document.createElement("style");
style.textContent = tailwindStyles;
document.head.append(style);

createRoot(document.getElementById("root")!).render(<App />);
