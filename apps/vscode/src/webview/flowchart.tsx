import {
  createSignal,
  createMemo,
  createEffect,
  onSettled,
  untrack,
  type Accessor,
} from "solid-js";
import { render, type JSX } from "@solidjs/web";
import { webviewStyle, type WebviewStyle, type WebviewEvent } from "./webview-dom-types";
import tailwindStyles from "./flowchart.css?inline";
import { VirtualList } from "./virtual-list";
import { ImeSafeInput, imeSafeKeyboardEventIsComposing } from "./ime-safe-input";
import { cn } from "../lib/utils";
import { useElementSize } from "./flowchart-dom";
import { flowchartMessages } from "./flowchart-i18n";
import { flowchartThemePaletteForSetting } from "./flowchart-theme";
import { flowchartSourceScrollTarget } from "./flowchart-source-scroll";
import { WebviewErrorBoundary } from "./webview-error-boundary";
import { flowchartSourcePanelMinimumWidth, vscode, vscodeThemeColor } from "./flowchart-runtime";
import { FlowSection, IncludeList, SidebarAccordionSection } from "./flowchart-sidebar";
import { EmptyText, SectionHeading } from "./flowchart-primitives";
import {
  FlowchartPaneResizeHandle,
  FlowchartSourcePanel,
  flowchartPrimarySourceHighlight,
  flowchartSourceHighlights,
} from "./flowchart-source-panel";
import {
  FlowchartCanvas,
  flowchartNodeForRange,
  isFlowchartPayload,
  isRange,
  nodesBySectionId,
  openFlowchartTarget,
  sectionIdForNodeFlowchart,
  sectionIdForRange,
} from "./flowchart-canvas";
import type {
  FlowchartPayload,
  InfoPanelPosition,
  WebviewTheme,
  WebviewThemeSetting,
} from "./flowchart-types";
import {
  defaultSectionId,
  flowchartForSection,
  flowchartLabelModeForPayload,
  flowchartSearchMatches,
  flowchartPaneLayout,
  modulo,
} from "./flowchart-model";
import type {
  AspFlowchartLabelMode,
  AspFlowchartNode,
  AspFlowchartTarget,
} from "../protocol-types";
declare global {
  interface Window {
    __ASP_LSP_FLOWCHART__?: FlowchartPayload;
    __ASP_LSP_FLOWCHART_TARGET_RANGE__?: AspFlowchartTarget["range"] | null;
  }
}
const flowchartPanelDefaultWidth = 380;
const flowchartPanelMinimumWidth = 320;
const flowchartSourcePanelDefaultWidth = 420;
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
function createResolvedWebviewTheme(setting: Accessor<WebviewThemeSetting | undefined>): {
  revision: Accessor<number>;
  theme: Accessor<WebviewTheme>;
} {
  const [vscodeTheme, setVsCodeTheme] = createSignal<WebviewTheme>(
    untrack(() => detectedVsCodeTheme()),
  );
  const [revision, setRevision] = createSignal(0);
  createEffect(setting, (setting) => {
    if (setting === "light" || setting === "dark") {
      return undefined;
    }
    const observer = new MutationObserver(() => {
      setVsCodeTheme(detectedVsCodeTheme());
      setRevision((value) => value + 1);
    });
    const options: MutationObserverInit = { attributes: true, attributeFilter: ["class", "style"] };
    observer.observe(document.body, options);
    observer.observe(document.documentElement, options);
    return () => observer.disconnect();
  });
  return {
    revision,
    theme: createMemo(() => {
      const value = setting();
      return value === "light" || value === "dark" ? value : vscodeTheme();
    }),
  };
}
function detectedVsCodeTheme(): WebviewTheme {
  const classList = document.body.classList;
  return classList.contains("vscode-light") || classList.contains("vscode-high-contrast-light")
    ? "light"
    : "dark";
}
function flowchartErrorBoundaryTitle(): string {
  const locale = window.__ASP_LSP_FLOWCHART__?.locale === "ja" ? "ja" : "en";
  return flowchartMessages[locale].renderFailureTitle;
}
function App(): JSX.Element {
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
  const [payload, setPayload] = createSignal<FlowchartPayload>(initialPayload);
  const [labelMode, setLabelMode] = createSignal<AspFlowchartLabelMode>(
    untrack(() => flowchartLabelModeForPayload(initialPayload)),
  );
  const { revision: vscodeThemeRevision, theme } = createResolvedWebviewTheme(
    () => payload().settings?.theme,
  );
  const themePalette = createMemo(() => {
    vscodeThemeRevision();
    return flowchartThemePaletteForSetting(theme(), payload().settings?.theme, vscodeThemeColor);
  });
  const [selectedSectionId, setSelectedSectionId] = createSignal<string | undefined>(
    untrack(() => initialSectionId),
  );
  const selectedSectionIdRef = { current: initialSectionId } as {
    current: string | undefined;
  };
  const [autoOpenSectionId, setAutoOpenSectionId] = createSignal<string | undefined>(
    untrack(() => (initialTargetRange ? initialSectionId : undefined)),
  );
  const [focusedFlowchartNodeId, setFocusedFlowchartNodeId] = createSignal<string | undefined>(
    untrack(() => initialTargetNode?.id),
  );
  const [hoveredFlowchartNodeId, setHoveredFlowchartNodeId] = createSignal<string | undefined>();
  const [sourcePanelVisible, setSourcePanelVisible] = createSignal(
    untrack(() => initialPayload.settings?.showSourcePanel ?? true),
  );
  const [sectionSourceScrollSequence, setSectionSourceScrollSequence] = createSignal(1);
  const [searchQuery, setSearchQuery] = createSignal("");
  const [activeSearchIndex, setActiveSearchIndex] = createSignal(0);
  const [infoPanelWidth, setInfoPanelWidth] = createSignal(flowchartPanelDefaultWidth);
  const [sourcePanelWidth, setSourcePanelWidth] = createSignal(flowchartSourcePanelDefaultWidth);
  const searchInputRef = { current: null } as {
    current: (HTMLInputElement | null) | null;
  };
  const [layoutRef, layoutSize] = useElementSize<HTMLElement>();
  const infoPanelPosition = createMemo(() => payload().settings?.infoPanelPosition ?? "left");
  const [compactPane, setCompactPane] = createSignal<"graph" | "information" | "source">("graph");
  createEffect(sourcePanelVisible, (visible) => {
    if (!visible) setCompactPane((pane) => (pane === "source" ? "graph" : pane));
  });
  const panes = createMemo(() =>
    flowchartPaneLayout(
      layoutSize().width,
      infoPanelWidth(),
      sourcePanelWidth(),
      sourcePanelVisible(),
    ),
  );
  const maximumInfoPanelWidth = () => panes().maxInfoWidth;
  const maximumSourcePanelWidth = () => panes().maxSourceWidth;
  const clampedInfoPanelWidth = () => panes().infoWidth;
  const clampedSourcePanelWidth = () => panes().sourceWidth;
  const changeSourcePanelVisible = (visible: boolean) => {
    setSourcePanelVisible(visible);
    if (panes().compact) setCompactPane(visible ? "source" : "graph");
  };
  const layoutStyle = createMemo(
    () =>
      ({
        "--flowchart-panel-width": `${clampedInfoPanelWidth()}px`,
        "--flowchart-source-panel-width": `${clampedSourcePanelWidth()}px`,
      }) as WebviewStyle,
  );
  const showSourcePanel = createMemo(() => sourcePanelVisible());
  const layoutClassName = createMemo(() =>
    cn(
      "asp-lsp-flowchart-shell grid h-full bg-[#101419] text-[#d9e0ea]",
      panes().compact
        ? "grid-cols-1 grid-rows-[auto_minmax(0,1fr)]"
        : showSourcePanel()
          ? infoPanelPosition() === "right"
            ? "grid-cols-[var(--flowchart-source-panel-width)_1px_minmax(0,1fr)_1px_var(--flowchart-panel-width)]"
            : "grid-cols-[var(--flowchart-panel-width)_1px_minmax(0,1fr)_1px_var(--flowchart-source-panel-width)]"
          : infoPanelPosition() === "right"
            ? "grid-cols-[minmax(0,1fr)_1px_var(--flowchart-panel-width)]"
            : "grid-cols-[var(--flowchart-panel-width)_1px_minmax(0,1fr)]",
    ),
  );
  const infoPanelClassName = createMemo(() =>
    cn(
      "flex min-h-0 flex-col bg-[#151b23]",
      panes().compact
        ? compactPane() === "information"
          ? "order-1"
          : "hidden"
        : infoPanelPosition() === "right"
          ? [showSourcePanel() ? "order-5" : "order-3", "border-l border-[#263140]"]
          : "order-1 border-r border-[#263140]",
    ),
  );
  const canvasClassName = createMemo(() =>
    cn(
      panes().compact
        ? compactPane() === "graph"
          ? "order-1"
          : "hidden"
        : infoPanelPosition() === "right"
          ? showSourcePanel()
            ? "order-3"
            : "order-1"
          : "order-3",
    ),
  );
  const resizeHandleClassName = createMemo(() =>
    cn(
      panes().compact
        ? "hidden"
        : infoPanelPosition() === "right" && showSourcePanel()
          ? "order-4"
          : "order-2",
    ),
  );
  const sourceResizeHandleClassName = createMemo(() =>
    panes().compact ? "hidden" : infoPanelPosition() === "right" ? "order-2" : "order-4",
  );
  const sourcePanelPosition = createMemo<InfoPanelPosition>(() =>
    infoPanelPosition() === "right" ? "left" : "right",
  );
  const sourcePanelClassName = createMemo(() =>
    cn(
      panes().compact
        ? compactPane() === "source"
          ? "order-1"
          : "hidden"
        : infoPanelPosition() === "right"
          ? "order-1 border-r border-[#263140]"
          : "order-5 border-l border-[#263140]",
    ),
  );
  const locale = createMemo(() => payload().locale ?? "en");
  const text = (key: string): string =>
    flowchartMessages[locale()][key] ?? flowchartMessages.en[key] ?? key;
  const nodesBySection = createMemo(() => nodesBySectionId(payload()));
  const searchMatches = createMemo(() => flowchartSearchMatches(payload(), searchQuery()));
  const matchedNodeIds = createMemo(() => new Set(searchMatches().map((match) => match.node.id)));
  const activeSearchIndexForDisplay = createMemo(() =>
    searchMatches().length > 0 ? Math.min(activeSearchIndex(), searchMatches().length - 1) : 0,
  );
  const activeSearchNode = createMemo(() => searchMatches()[activeSearchIndexForDisplay()]?.node);
  const activeFlowchartNodeId = createMemo(
    () => focusedFlowchartNodeId() ?? activeSearchNode()?.id,
  );
  const selectedFlowchart = createMemo(() =>
    flowchartForSection(payload(), selectedSectionId(), themePalette()),
  );
  const selectedSection = createMemo(() => selectedFlowchart().sections[0]);
  const selectedSectionIndex = createMemo(() =>
    payload().sections.findIndex((section) => section.id === selectedSection()?.id),
  );
  const sourceHighlights = createMemo(() =>
    flowchartSourceHighlights(
      selectedSection(),
      selectedFlowchart().nodes,
      payload().nodes,
      hoveredFlowchartNodeId(),
      activeFlowchartNodeId(),
    ),
  );
  const primarySourceHighlight = createMemo(() =>
    flowchartPrimarySourceHighlight(sourceHighlights()),
  );
  const sourceScrollTarget = createMemo(() =>
    flowchartSourceScrollTarget(sourceHighlights(), {
      activeNodeId: activeFlowchartNodeId(),
      hoveredNodeId: hoveredFlowchartNodeId(),
      sectionId: selectedSection()?.id,
      sectionSequence: sectionSourceScrollSequence(),
      uri: payload().uri,
    }),
  );
  const selectFlowchartNode = (node: AspFlowchartNode) => {
    setSelectedSectionId(node.sectionId);
    setAutoOpenSectionId(node.sectionId);
    setFocusedFlowchartNodeId(node.id);
  };
  const openFlowchartForNode = (node: AspFlowchartNode) => {
    const target = node.links?.find((link) => link.target)?.target;
    if (target) {
      setAutoOpenSectionId(
        openFlowchartTarget(
          payload(),
          target,
          setSelectedSectionId,
          setFocusedFlowchartNodeId,
          labelMode(),
        ),
      );
      setSectionSourceScrollSequence((current) => current + 1);
    } else {
      const nextSectionId = sectionIdForNodeFlowchart(payload(), node);
      setSelectedSectionId(nextSectionId);
      setAutoOpenSectionId(nextSectionId);
      setFocusedFlowchartNodeId(undefined);
      setSectionSourceScrollSequence((current) => current + 1);
    }
  };
  const openTarget = (target: AspFlowchartTarget) => {
    setAutoOpenSectionId(
      openFlowchartTarget(
        payload(),
        target,
        setSelectedSectionId,
        setFocusedFlowchartNodeId,
        labelMode(),
      ),
    );
    setSectionSourceScrollSequence((current) => current + 1);
  };
  const changeLabelMode = (mode: AspFlowchartLabelMode) => {
    if (mode === labelMode()) {
      return;
    }
    setLabelMode(mode);
    vscode.postMessage({ type: "reloadFlowchart", uri: payload().uri, labelMode: mode });
  };
  const selectSearchMatch = (index: number) => {
    if (searchMatches().length === 0) {
      return;
    }
    const nextIndex = modulo(index, searchMatches().length);
    const nextSectionId = searchMatches()[nextIndex]?.node.sectionId;
    setFocusedFlowchartNodeId(undefined);
    setActiveSearchIndex(nextIndex);
    setSelectedSectionId(nextSectionId);
    setAutoOpenSectionId(nextSectionId);
  };
  const handleSearchKeyDown = (event: WebviewEvent<KeyboardEvent, HTMLInputElement>) => {
    if (imeSafeKeyboardEventIsComposing(event)) {
      return;
    }
    if (event.key === "Enter") {
      event.preventDefault();
      selectSearchMatch(activeSearchIndex() + (event.shiftKey ? -1 : 1));
    } else if (event.key === "Escape") {
      setSearchQuery("");
      setFocusedFlowchartNodeId(undefined);
    }
  };
  createEffect(
    () => [selectedSectionId()],
    () => {
      selectedSectionIdRef.current = selectedSectionId();
    },
  );
  onSettled(() => {
    const listener = (event: MessageEvent) => {
      const message = event.data as {
        type?: unknown;
        payload?: unknown;
        targetRange?: unknown;
      };
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
        if (message.payload.settings?.showSourcePanel !== payload().settings?.showSourcePanel) {
          setSourcePanelVisible(message.payload.settings?.showSourcePanel ?? true);
        }
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
  });
  onSettled(() => {
    const listener = (event: KeyboardEvent) => {
      if (imeSafeKeyboardEventIsComposing(event)) {
        return;
      }
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "f") {
        event.preventDefault();
        if (panes().compact) setCompactPane("information");
        window.requestAnimationFrame(() => {
          searchInputRef.current?.focus();
          searchInputRef.current?.select();
        });
      }
    };
    window.addEventListener("keydown", listener);
    return () => window.removeEventListener("keydown", listener);
  });
  createEffect(
    () => [payload(), searchQuery()],
    () => {
      setActiveSearchIndex(0);
    },
  );
  createEffect(
    () => [activeSearchIndex(), searchMatches(), searchQuery()],
    () => {
      if (searchMatches().length === 0) {
        return;
      }
      const nextSectionId =
        searchMatches()[Math.min(activeSearchIndex(), searchMatches().length - 1)].node.sectionId;
      setSelectedSectionId(nextSectionId);
      if (searchQuery().trim()) {
        setAutoOpenSectionId(nextSectionId);
      }
    },
  );
  return (
    <main
      ref={(element) => (layoutRef.current = element)}
      class={layoutClassName()}
      data-asp-lsp-theme={theme()}
      data-asp-lsp-theme-source={
        payload().settings?.theme === "light" || payload().settings?.theme === "dark"
          ? "fixed"
          : "vscode"
      }
      style={webviewStyle(layoutStyle())}
    >
      {panes().compact ? (
        <nav class="flex gap-1 border-b border-[#263140] p-2" aria-label={text("paneNavigation")}>
          {(["graph", "information", "source"] as const).map((pane) => (
            <button
              type="button"
              class={cn(
                "rounded border border-[#334255] px-3 py-1 text-xs",
                compactPane() === pane && "bg-[#17324a] text-white",
              )}
              aria-pressed={compactPane() === pane ? "true" : "false"}
              onClick={() => {
                if (pane === "source") setSourcePanelVisible(true);
                setCompactPane(pane);
              }}
            >
              {text(
                pane === "graph"
                  ? "paneGraph"
                  : pane === "information"
                    ? "paneInformation"
                    : "paneSource",
              )}
            </button>
          ))}
        </nav>
      ) : null}
      <aside class={cn(infoPanelClassName(), "min-w-0 overflow-hidden")}>
        <header class="border-b border-[#263140] px-4 py-3">
          <div
            class="overflow-hidden text-ellipsis whitespace-nowrap text-sm font-semibold text-[#f1f5f9]"
            title={selectedSection()?.label ?? payload().fileName ?? text("title")}
          >
            {selectedSection()?.label ?? payload().fileName ?? text("title")}
          </div>
          <div
            class="mt-1 overflow-hidden text-ellipsis whitespace-nowrap text-xs text-[#9fb0c5]"
            title={`${payload().stats.sections} ${text("sections")} / ${selectedFlowchart().stats.nodes} ${text("nodes")} / ${payload().includes.length} ${text("includes")}`}
          >
            {payload().stats.sections} {text("sections")} / {selectedFlowchart().stats.nodes}{" "}
            {text("nodes")} / {payload().includes.length} {text("includes")}
          </div>
        </header>
        <div class="border-b border-[#263140] px-3 py-2">
          <div class="flex items-center gap-1">
            <ImeSafeInput
              ref={(element) => (searchInputRef.current = element)}
              aria-label={text("searchNodes")}
              class="h-7 min-w-0 flex-1 rounded border border-[#334255] bg-[#0c1117] px-2 text-xs text-[#d9e0ea] outline-none placeholder:text-[#6f7e91] focus:border-[#7dd3fc]"
              placeholder={text("searchPlaceholder")}
              role="searchbox"
              type="text"
              value={searchQuery()}
              onValueChange={(value) => {
                setSearchQuery(value);
                setFocusedFlowchartNodeId(undefined);
              }}
              onKeyDown={handleSearchKeyDown}
            />
            <span class="min-w-[44px] text-center text-[11px] text-[#9fb0c5]">
              {searchMatches().length > 0
                ? `${activeSearchIndexForDisplay() + 1}/${searchMatches().length}`
                : "0"}
            </span>
            <button
              class="h-7 w-7 rounded border border-[#334255] text-xs text-[#c4d4e8] hover:border-[#7dd3fc] hover:text-white disabled:cursor-not-allowed disabled:border-[#263140] disabled:text-[#5f6d7e]"
              disabled={searchMatches().length === 0}
              title={text("searchPrevious")}
              type="button"
              onClick={() => selectSearchMatch(activeSearchIndex() - 1)}
            >
              ↑
            </button>
            <button
              class="h-7 w-7 rounded border border-[#334255] text-xs text-[#c4d4e8] hover:border-[#7dd3fc] hover:text-white disabled:cursor-not-allowed disabled:border-[#263140] disabled:text-[#5f6d7e]"
              disabled={searchMatches().length === 0}
              title={text("searchNext")}
              type="button"
              onClick={() => selectSearchMatch(activeSearchIndex() + 1)}
            >
              ↓
            </button>
          </div>
        </div>
        <div class="min-h-0 flex-1 overflow-auto p-3">
          <SidebarAccordionSection
            count={payload().includes.length}
            hint={text("includesHint")}
            text={text}
            title={text("includes")}
          >
            <IncludeList
              includes={payload().includes}
              labelMode={labelMode()}
              text={text}
              uri={payload().uri}
            />
          </SidebarAccordionSection>
          <SectionHeading>{text("flowcharts")}</SectionHeading>
          {payload().sections.length === 0 ? (
            <EmptyText>{text("emptyNodes")}</EmptyText>
          ) : (
            <VirtualList
              className="grid gap-3"
              estimateSize={72}
              getKey={(section) => section.id}
              items={payload().sections}
              maxHeight={560}
              overscan={8}
              scrollToIndex={selectedSectionIndex() >= 0 ? selectedSectionIndex() : undefined}
              renderItem={(section) => {
                const sectionNodes = nodesBySection().get(section.id) ?? [];
                const hasActiveNode = Boolean(
                  activeFlowchartNodeId() &&
                  sectionNodes.some((node) => node.id === activeFlowchartNodeId()),
                );
                return (
                  <FlowSection
                    locale={locale()}
                    nodes={sectionNodes}
                    selected={section.id === selectedSection()?.id}
                    section={section}
                    shouldAutoOpen={autoOpenSectionId() === section.id || hasActiveNode}
                    theme={theme()}
                    themePalette={themePalette()}
                    text={text}
                    activeSearchNodeId={activeFlowchartNodeId()}
                    matchedNodeIds={matchedNodeIds()}
                    onOpenCode={(range) =>
                      range && vscode.postMessage({ type: "openRange", uri: payload().uri, range })
                    }
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
          <div class="mb-2 mt-3 flex items-center gap-2">
            <SectionHeading>{text("mermaid")}</SectionHeading>
            <button
              class="ml-auto rounded border border-[#3b4a5f] px-2 py-0.5 text-[11px] text-[#c4d4e8] hover:border-[#7dd3fc] hover:text-white"
              type="button"
              onClick={() =>
                vscode.postMessage({
                  type: "copyText",
                  content: selectedFlowchart().mermaid,
                })
              }
            >
              {text("copyMermaid")}
            </button>
          </div>
          <pre class="max-h-52 overflow-auto rounded border border-[#263140] bg-[#0c1117] p-2 text-xs leading-5 text-[#b9c5d6]">
            {selectedFlowchart().mermaid}
          </pre>
        </div>
      </aside>
      <FlowchartPaneResizeHandle
        label={text("resizeInfoPanel")}
        maxWidth={maximumInfoPanelWidth()}
        minWidth={flowchartPanelMinimumWidth}
        position={infoPanelPosition()}
        width={clampedInfoPanelWidth()}
        className={resizeHandleClassName()}
        onWidthChange={setInfoPanelWidth}
      />
      <FlowchartCanvas
        className={canvasClassName()}
        payload={selectedFlowchart()}
        section={selectedSection()}
        themePalette={themePalette()}
        text={text}
        activeSearchNodeId={activeFlowchartNodeId()}
        matchedNodeIds={matchedNodeIds()}
        onOpenCode={(range) =>
          range && vscode.postMessage({ type: "openRange", uri: payload().uri, range })
        }
        onOpenFlowchart={openFlowchartForNode}
        labelMode={labelMode()}
        onLabelModeChange={changeLabelMode}
        sourcePanelVisible={sourcePanelVisible()}
        onSourcePanelVisibleChange={changeSourcePanelVisible}
        onHoverNode={setHoveredFlowchartNodeId}
        onSelectNode={selectFlowchartNode}
      />
      {showSourcePanel() ? (
        <>
          <FlowchartPaneResizeHandle
            label={text("resizeSourcePanel")}
            maxWidth={maximumSourcePanelWidth()}
            minWidth={flowchartSourcePanelMinimumWidth}
            position={sourcePanelPosition()}
            width={clampedSourcePanelWidth()}
            className={sourceResizeHandleClassName()}
            onWidthChange={setSourcePanelWidth}
          />
          <FlowchartSourcePanel
            activeLabel={primarySourceHighlight()?.label}
            className={sourcePanelClassName()}
            highlights={sourceHighlights()}
            nodes={payload().nodes}
            scrollTarget={sourceScrollTarget()}
            sourceText={payload().sourceText}
            text={text}
            theme={theme()}
            themeRevision={vscodeThemeRevision()}
            themeSetting={payload().settings?.theme}
            onHoverNode={setHoveredFlowchartNodeId}
            onOpenCode={(range) =>
              vscode.postMessage({ type: "openRange", uri: payload().uri, range })
            }
            onSelectNode={selectFlowchartNode}
          />
        </>
      ) : null}
    </main>
  );
}
const style = document.createElement("style");
style.textContent = tailwindStyles;
document.head.append(style);
render(
  () => (
    <WebviewErrorBoundary title={flowchartErrorBoundaryTitle()}>
      <App />
    </WebviewErrorBoundary>
  ),
  document.getElementById("root")!,
);
