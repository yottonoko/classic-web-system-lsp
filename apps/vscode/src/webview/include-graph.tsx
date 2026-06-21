import React, {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  startTransition,
  useState,
} from "react";
import { createRoot } from "react-dom/client";
import ForceGraph2D from "react-force-graph-2d";
import ForceGraph3D from "react-force-graph-3d";
import { INITIAL, Registry, parseRawGrammar } from "vscode-textmate";
import { graphMessages } from "./include-graph-i18n";
import { graphThemePalettes } from "./include-graph-theme";
import { isFileLikeGraphNode } from "./include-graph-types";
import { ImeSafeInput, imeSafeKeyboardEventIsComposing } from "./ime-safe-input";
import type { GraphTextKey, GraphTextParams } from "./include-graph-i18n";
import type {
  GraphData,
  GraphLink,
  GraphLocale,
  GraphNode,
  GraphStatsListItem,
  GraphStatsMetric,
  GraphStatsTarget,
  GraphThemePalette,
  HighlightOffsets,
  InfoPanelPosition,
  LinkFilterCategory,
  NodeColorCategory,
  OpenFlowchartMessage,
  PendingPositionSync,
  PositionSyncEntry,
  Selection,
  SnippetHighlighter,
  SnippetHighlightState,
  SnippetLanguage,
  SourceRangesMessage,
  GraphUpdatedMessage,
  ViewMode,
  WebviewTheme,
  WebviewThemeSetting,
} from "./include-graph-types";
import { createOnigScanner, createOnigString, loadWASM } from "vscode-oniguruma";
import onigWasmUrl from "vscode-oniguruma/release/onig.wasm?url";
import classicAspGrammarJson from "../../syntaxes/classic-asp.tmLanguage.json";
import classicAspTagInjectionGrammarJson from "../../syntaxes/classic-asp-tag-injection.tmLanguage.json";
import vbscriptGrammarJson from "../../syntaxes/vbscript.tmLanguage.json";
import tailwindStyles from "./include-graph.css?inline";
import { VirtualList } from "./virtual-list";
import { cn } from "../lib/utils";
import {
  applyPositionSyncTo2d,
  applyPositionSyncTo3d,
  capturePositionSyncEntries,
  clamp,
  configureGraphForces,
  filterGraphData,
  focusGraphTarget,
  graphDataFor,
  graphDataForRender,
  graphLinkFilterCategory,
  graphNodeMap,
  graphStatsFor,
  graphStatsTargetForRange,
  highlightForSearchTargets,
  highlightForSelection,
  isGraphRange,
  isImplicitGlobalVariableNode,
  isSearchClearShortcut,
  isSearchFocusShortcut,
  linkArrowLength,
  linkColor,
  linkParticleCount,
  linkParticleWidth3d,
  linkSwatchWidth,
  linkWidth2d,
  linkWidth3d,
  maxInspectorWidthForLayout,
  nodeColor,
  nodeIdForEndpoint,
  nodeTextObject,
  paintNode,
  paintNodePointerArea,
  releasePositionSyncPins,
  searchNavigationDirection,
  searchTargetsForSearch,
  selectionForStatsTarget,
  toggledSet,
} from "./include-graph-model";
import type {
  AspGraphLink,
  AspGraphPayload,
  AspGraphRange,
  AspGraphSourceRangeRequestItem,
  AspGraphSourceRangeResponseItem,
} from "../include-graph-webview";
import type { ForceGraphMethods as ForceGraph2DMethods } from "react-force-graph-2d";
import type { ForceGraphMethods as ForceGraph3DMethods } from "react-force-graph-3d";
import type { IOnigLib, IRawGrammar, StateStack } from "vscode-textmate";

declare const acquireVsCodeApi: () => {
  postMessage(message: unknown): void;
};

declare global {
  interface Window {
    __ASP_LSP_GRAPH__?: AspGraphPayload;
    __ASP_LSP_GRAPH_TARGET_RANGE__?: AspGraphRange | null;
  }
}

const vscode = acquireVsCodeApi();
const initialGraph = window.__ASP_LSP_GRAPH__;
const initialGraphTargetRange = isGraphRange(window.__ASP_LSP_GRAPH_TARGET_RANGE__)
  ? window.__ASP_LSP_GRAPH_TARGET_RANGE__
  : undefined;

const graphLocale: GraphLocale = initialGraph?.locale === "ja" ? "ja" : "en";

function graphText(key: GraphTextKey, params?: GraphTextParams): string {
  let message = graphMessages[graphLocale][key] ?? graphMessages.en[key];
  for (const [name, value] of Object.entries(params ?? {})) {
    message = message.replaceAll(`{${name}}`, String(value));
  }
  return message;
}

const linkMeanings: Record<AspGraphLink["kind"], { label: string; description: string }> = {
  include: {
    label: graphText("link.include.label"),
    description: graphText("link.include.description"),
  },
  declares: {
    label: graphText("link.declares.label"),
    description: graphText("link.declares.description"),
  },
  references: {
    label: graphText("link.references.label"),
    description: graphText("link.references.description"),
  },
  assignments: {
    label: graphText("link.assignments.label"),
    description: graphText("link.assignments.description"),
  },
  calls: {
    label: graphText("link.calls.label"),
    description: graphText("link.calls.description"),
  },
  unresolvedReference: {
    label: graphText("link.unresolvedReference.label"),
    description: graphText("link.unresolvedReference.description"),
  },
};

const linkFilterLabels: Record<LinkFilterCategory, string> = {
  include: graphText("link.include.label"),
  declares: graphText("link.declares.label"),
  references: graphText("link.references.label"),
  assignments: graphText("link.assignments.label"),
  calls: graphText("link.calls.label"),
  unresolvedReference: graphText("link.unresolvedReference.label"),
  member: graphText("link.member.label"),
};

const linkFilterDescriptions: Record<LinkFilterCategory, string> = {
  include: graphText("link.include.description"),
  declares: graphText("link.declares.description"),
  references: graphText("link.references.description"),
  assignments: graphText("link.assignments.description"),
  calls: graphText("link.calls.description"),
  unresolvedReference: graphText("link.unresolvedReference.description"),
  member: graphText("link.member.description"),
};

const nodeCategoryLabels: Record<NodeColorCategory, string> = {
  root: graphText("label.root"),
  file: graphText("label.file"),
  missingInclude: graphText("label.missingInclude"),
  function: graphText("label.function"),
  sub: graphText("label.sub"),
  class: graphText("label.class"),
  method: graphText("label.method"),
  methodFunction: graphText("label.functionMethod"),
  methodSub: graphText("label.subMethod"),
  property: graphText("label.property"),
  member: graphText("label.member"),
  globalVariable: graphText("label.globalVariable"),
  implicitGlobalVariable: graphText("label.implicitGlobalVariable"),
  globalConstant: graphText("label.globalConstant"),
  localVariable: graphText("label.localVariable"),
  localConstant: graphText("label.localConstant"),
  parameter: graphText("label.parameter"),
  unresolvedFunction: graphText("label.unresolvedFunction"),
  unresolved: graphText("label.unresolved"),
};

const nodeCategoryDescriptions: Record<NodeColorCategory, string> = {
  root: graphText("node.root.description"),
  file: graphText("node.file.description"),
  missingInclude: graphText("node.missingInclude.description"),
  class: graphText("node.class.description"),
  function: graphText("node.function.description"),
  sub: graphText("node.sub.description"),
  method: graphText("node.method.description"),
  methodFunction: graphText("node.methodFunction.description"),
  methodSub: graphText("node.methodSub.description"),
  property: graphText("node.property.description"),
  member: graphText("node.member.description"),
  globalVariable: graphText("node.globalVariable.description"),
  implicitGlobalVariable: graphText("node.implicitGlobalVariable.description"),
  globalConstant: graphText("node.globalConstant.description"),
  localVariable: graphText("node.localVariable.description"),
  localConstant: graphText("node.localConstant.description"),
  parameter: graphText("node.parameter.description"),
  unresolvedFunction: graphText("node.unresolvedFunction.description"),
  unresolved: graphText("node.unresolved.description"),
};

const nodeCategoryOrder: NodeColorCategory[] = [
  "root",
  "file",
  "missingInclude",
  "class",
  "function",
  "sub",
  "methodFunction",
  "methodSub",
  "property",
  "member",
  "globalVariable",
  "implicitGlobalVariable",
  "globalConstant",
  "localVariable",
  "localConstant",
  "parameter",
  "unresolvedFunction",
  "unresolved",
];

const unresolvedNodeCategorySet = new Set<NodeColorCategory>([
  "missingInclude",
  "unresolvedFunction",
  "unresolved",
]);

const linkFilterOrder: LinkFilterCategory[] = [
  "include",
  "declares",
  "references",
  "assignments",
  "calls",
  "unresolvedReference",
  "member",
];

const graphFitDurationMs = 400;
const graphFitPadding2d = 100;
const graphFitPadding3d = 5;
const positionSyncPinMs = 600;
const graphForceVelocityDecay = 0.42;
const sourceHighlightMarkClassName =
  "rounded-sm bg-[#ffcb6b]/45 px-0.5 text-inherit shadow-[0_0_0_1px_rgb(255_203_107_/_75%)]";
const inspectorDefaultWidth = 320;
const inspectorMinimumWidth = 260;
const paneResizeKeyboardStep = 16;

function positiveModulo(value: number, divisor: number): number {
  return ((value % divisor) + divisor) % divisor;
}

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
  const [graph, setGraph] = useState<AspGraphPayload | undefined>(() => initialGraph);
  const [graphRevision, setGraphRevision] = useState(0);
  const [graphUpdateError, setGraphUpdateError] = useState<string>();
  const theme = useResolvedWebviewTheme(graph?.settings?.theme);
  const themePalette = graphThemePalettes[theme];
  const [mode, setMode] = useState<ViewMode>(graph?.settings?.initialViewMode ?? "2d");
  const [selection, setSelection] = useState<Selection>();
  const [searchInput, setSearchInput] = useState("");
  const [searchQuery, setSearchQuery] = useState("");
  const [searchCursor, setSearchCursor] = useState(-1);
  const [searchMatchCase, setSearchMatchCase] = useState(false);
  const [inspectorWidth, setInspectorWidth] = useState(inspectorDefaultWidth);
  const [hideSingleNodes, setHideSingleNodes] = useState(graph?.settings?.hideSingleNodes ?? true);
  const [hideUnreferencedGlobalSymbols, setHideUnreferencedGlobalSymbols] = useState(
    graph?.settings?.hideUnreferencedGlobalSymbols ?? true,
  );
  const [showOutgoingSelectionLinks, setShowOutgoingSelectionLinks] = useState(
    graph?.settings?.showOutgoingSelectionLinks ?? true,
  );
  const [hiddenNodeCategories, setHiddenNodeCategories] = useState<Set<NodeColorCategory>>(
    () => new Set(graph?.settings?.hiddenNodeCategories ?? []),
  );
  const [hiddenLinkCategories, setHiddenLinkCategories] = useState<Set<LinkFilterCategory>>(
    () => new Set(graph?.settings?.hiddenLinkCategories ?? []),
  );
  const inspectorPosition = graph?.settings?.infoPanelPosition ?? "right";
  const graph2dRef = useRef<ForceGraph2DMethods<GraphNode, GraphLink> | undefined>(undefined);
  const graph3dRef = useRef<ForceGraph3DMethods<GraphNode, GraphLink> | undefined>(undefined);
  const hasAutoFit2dRef = useRef(false);
  const hasAutoFit3dRef = useRef(false);
  const positionSyncRef = useRef(new Map<string, PositionSyncEntry>());
  const pendingSyncRef = useRef<PendingPositionSync | undefined>(undefined);
  const positionSyncGenerationRef = useRef(0);
  const positionSyncReleaseTimerRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const hasFocusedInitialTargetRef = useRef(false);
  const searchInputRef = useRef<HTMLInputElement>(null);
  const skipAutoFitForModeRef = useRef(new Set<ViewMode>());
  const forceFitForModeRef = useRef(new Set<ViewMode>());
  const graphData = useMemo(() => graphDataFor(graph, themePalette), [graph, themePalette]);
  const filteredGraphData = useMemo(
    () =>
      filterGraphData(
        graphData,
        hiddenNodeCategories,
        hiddenLinkCategories,
        hideSingleNodes,
        hideUnreferencedGlobalSymbols,
      ),
    [
      graphData,
      hiddenNodeCategories,
      hiddenLinkCategories,
      hideSingleNodes,
      hideUnreferencedGlobalSymbols,
    ],
  );
  const renderGraphData2d = useMemo(
    () => graphDataForRender(filteredGraphData, positionSyncRef.current, "2d"),
    [filteredGraphData],
  );
  const renderGraphData3d = useMemo(
    () => graphDataForRender(filteredGraphData, positionSyncRef.current, "3d"),
    [filteredGraphData],
  );
  const renderGraphData = mode === "3d" ? renderGraphData3d : renderGraphData2d;
  const filteredStats = useMemo(() => graphStatsFor(filteredGraphData), [filteredGraphData]);
  const searchTargets = useMemo(
    () => searchTargetsForSearch(searchQuery, searchMatchCase, filteredGraphData.nodes),
    [searchQuery, searchMatchCase, filteredGraphData.nodes],
  );
  const searchHighlight = useMemo(
    () => highlightForSearchTargets(searchTargets, filteredGraphData.links, searchQuery.trim()),
    [searchTargets, filteredGraphData.links, searchQuery],
  );
  const selectionHighlight = useMemo(
    () => highlightForSelection(selection, filteredGraphData.links, showOutgoingSelectionLinks),
    [selection, filteredGraphData.links, showOutgoingSelectionLinks],
  );
  const highlight = searchHighlight ?? selectionHighlight;
  const titleFileName = graphRootName(graph);
  const [layoutRef, layoutSize] = useElementSize<HTMLElement>();
  const [surfaceRef, surfaceSize] = useElementSize<HTMLElement>();
  const maximumInspectorWidth = maxInspectorWidthForLayout(layoutSize.width);
  const clampedInspectorWidth = clamp(inspectorWidth, inspectorMinimumWidth, maximumInspectorWidth);
  const layoutStyle = {
    "--inspector-width": `${clampedInspectorWidth}px`,
  } as React.CSSProperties;
  const layoutClassName = cn(
    "relative grid min-h-0 overflow-hidden max-[780px]:grid-cols-1 max-[780px]:grid-rows-[minmax(0,1fr)]",
    inspectorPosition === "left"
      ? "grid-cols-[var(--inspector-width)_6px_minmax(0,1fr)]"
      : "grid-cols-[minmax(0,1fr)_6px_var(--inspector-width)]",
  );
  const truncatedGraphText = graph?.truncated
    ? graphText("toolbar.truncated", {
        reason: graph.truncated.reason,
        shownNodes: graph.nodes.length,
        totalNodes: graph.truncated.nodes ?? graph.stats.nodes,
        shownLinks: graph.links.length,
        totalLinks: graph.truncated.links ?? graph.stats.links,
      })
    : undefined;
  const truncatedGraphTitle = graph?.truncated
    ? graphText("toolbar.truncatedHint", { setting: "aspLsp.graph.maxNodes" })
    : undefined;
  const graphUpdateText = graphUpdateError
    ? graphText("toolbar.updateFailed", { error: graphUpdateError })
    : graph?.pending
      ? graphText("toolbar.updating")
      : undefined;
  const surfaceClassName = cn(
    "relative min-h-0 min-w-0 overflow-hidden [&_canvas]:block",
    inspectorPosition === "left" ? "order-3 max-[780px]:order-1" : "order-1",
  );
  const canFitGraph =
    filteredGraphData.nodes.length > 0 && surfaceSize.width > 0 && surfaceSize.height > 0;
  const toggleNodeCategory = useCallback((category: NodeColorCategory) => {
    setHiddenNodeCategories((current) => toggledSet(current, category));
  }, []);
  const toggleLinkCategory = useCallback((category: LinkFilterCategory) => {
    setHiddenLinkCategories((current) => toggledSet(current, category));
  }, []);
  const updateSearchInput = useCallback((nextValue: string) => {
    setSearchInput(nextValue);
    startTransition(() => {
      setSearchQuery(nextValue);
    });
  }, []);
  const clearSearchInput = useCallback(() => {
    updateSearchInput("");
    setSearchCursor(-1);
  }, [updateSearchInput]);
  const fitGraphToCanvas = useCallback(
    (nextMode: ViewMode) => {
      if (!canFitGraph) {
        return false;
      }
      const graphRef = nextMode === "3d" ? graph3dRef : graph2dRef;
      if (!graphRef.current) {
        return false;
      }
      graphRef.current.zoomToFit(
        graphFitDurationMs,
        nextMode === "2d" ? graphFitPadding2d : graphFitPadding3d,
      );
      return true;
    },
    [canFitGraph],
  );
  const captureCurrentRenderPositions = useCallback(() => {
    capturePositionSyncEntries(
      mode,
      renderGraphData.nodes,
      graph2dRef.current,
      graph3dRef.current,
      positionSyncRef.current,
    );
  }, [mode, renderGraphData.nodes]);
  const selectGraphNode = useCallback(
    (node: GraphNode) => {
      captureCurrentRenderPositions();
      setSelection({ type: "node", item: node });
    },
    [captureCurrentRenderPositions],
  );
  const selectGraphLink = useCallback(
    (link: GraphLink) => {
      captureCurrentRenderPositions();
      setSelection({ type: "link", item: link });
    },
    [captureCurrentRenderPositions],
  );
  const selectAndFocusGraphTarget = useCallback(
    (target: GraphStatsTarget) => {
      const nextSelection = selectionForStatsTarget(target, filteredGraphData);
      if (!nextSelection) {
        return;
      }
      captureCurrentRenderPositions();
      setSelection(nextSelection);
      focusGraphTarget(target, mode, renderGraphData, graph2dRef.current, graph3dRef.current);
    },
    [captureCurrentRenderPositions, filteredGraphData, mode, renderGraphData],
  );
  const selectAndFocusGraphNode = useCallback(
    (node: GraphNode) => selectAndFocusGraphTarget({ type: "node", id: node.id }),
    [selectAndFocusGraphTarget],
  );
  const selectAndFocusGraphLink = useCallback(
    (link: GraphLink) => selectAndFocusGraphTarget({ type: "link", id: link.id }),
    [selectAndFocusGraphTarget],
  );
  useEffect(() => {
    if (hasFocusedInitialTargetRef.current || !initialGraphTargetRange) {
      return;
    }
    const target = graphStatsTargetForRange(
      filteredGraphData,
      initialGraphTargetRange,
      graph?.rootUri,
    );
    if (!target) {
      return;
    }
    hasFocusedInitialTargetRef.current = true;
    selectAndFocusGraphTarget(target);
  }, [filteredGraphData, graph?.rootUri, selectAndFocusGraphTarget]);
  const focusSearchResult = useCallback(
    (direction: 1 | -1) => {
      if (searchTargets.length === 0) {
        return false;
      }
      const baseCursor =
        searchCursor >= 0 && searchCursor < searchTargets.length
          ? searchCursor
          : direction > 0
            ? -1
            : 0;
      const nextCursor = positiveModulo(baseCursor + direction, searchTargets.length);
      const target = searchTargets[nextCursor];
      if (!target) {
        return false;
      }
      const nextSelection = selectionForStatsTarget(target, filteredGraphData);
      if (!nextSelection) {
        return false;
      }
      captureCurrentRenderPositions();
      setSearchCursor(nextCursor);
      setSelection(nextSelection);
      focusGraphTarget(target, mode, renderGraphData, graph2dRef.current, graph3dRef.current);
      return true;
    },
    [
      captureCurrentRenderPositions,
      filteredGraphData,
      mode,
      renderGraphData,
      searchCursor,
      searchTargets,
    ],
  );
  const switchGraphMode = useCallback(
    (nextMode: ViewMode) => {
      if (nextMode === mode) {
        return;
      }
      if (mode === "3d") {
        graph3dRef.current?.pauseAnimation();
      } else {
        graph2dRef.current?.pauseAnimation();
      }
      const generation = positionSyncGenerationRef.current + 1;
      positionSyncGenerationRef.current = generation;
      const entries = capturePositionSyncEntries(
        mode,
        renderGraphData.nodes,
        graph2dRef.current,
        graph3dRef.current,
        positionSyncRef.current,
      );
      pendingSyncRef.current =
        entries.size > 0
          ? {
              from: mode,
              to: nextMode,
              generation,
              entries,
            }
          : undefined;
      if (entries.size > 0) {
        skipAutoFitForModeRef.current.add(nextMode);
        if (mode === "2d" && nextMode === "3d") {
          forceFitForModeRef.current.add(nextMode);
        }
      }
      setMode(nextMode);
    },
    [mode, renderGraphData.nodes],
  );
  const handleEngineStop = useCallback(
    (nextMode: ViewMode) => {
      const autoFitRef = nextMode === "3d" ? hasAutoFit3dRef : hasAutoFit2dRef;
      if (forceFitForModeRef.current.delete(nextMode)) {
        autoFitRef.current = fitGraphToCanvas(nextMode);
        skipAutoFitForModeRef.current.delete(nextMode);
        return;
      }
      if (skipAutoFitForModeRef.current.delete(nextMode)) {
        autoFitRef.current = true;
        return;
      }
      if (autoFitRef.current) {
        return;
      }
      autoFitRef.current = fitGraphToCanvas(nextMode);
    },
    [fitGraphToCanvas],
  );
  useEffect(() => {
    setSearchCursor(-1);
  }, [searchTargets]);

  useEffect(() => {
    const handleSearchKeyDown = (event: KeyboardEvent) => {
      if (imeSafeKeyboardEventIsComposing(event)) {
        return;
      }
      if (isSearchFocusShortcut(event)) {
        event.preventDefault();
        searchInputRef.current?.focus();
        searchInputRef.current?.select();
        return;
      }
      if (isSearchClearShortcut(event, searchInputRef.current)) {
        event.preventDefault();
        if (searchInput) {
          clearSearchInput();
        } else {
          searchInputRef.current?.blur();
        }
        return;
      }
      const direction = searchNavigationDirection(event, searchInputRef.current);
      if (direction && searchInput.trim()) {
        event.preventDefault();
        void focusSearchResult(direction);
      }
    };
    window.addEventListener("keydown", handleSearchKeyDown);
    return () => window.removeEventListener("keydown", handleSearchKeyDown);
  }, [clearSearchInput, focusSearchResult, searchInput]);

  useEffect(() => {
    const clearSelectionOnEscape = (event: KeyboardEvent) => {
      if (
        event.defaultPrevented ||
        document.activeElement === searchInputRef.current ||
        event.key !== "Escape"
      ) {
        return;
      }
      setSelection(undefined);
    };
    window.addEventListener("keydown", clearSelectionOnEscape);
    return () => window.removeEventListener("keydown", clearSelectionOnEscape);
  }, []);

  useEffect(() => {
    const handleGraphUpdated = (event: MessageEvent<unknown>) => {
      const message = event.data;
      if (!isGraphUpdatedMessage(message)) {
        return;
      }
      startTransition(() => {
        if (message.payload) {
          setGraph(message.payload);
          setGraphRevision((current) => current + 1);
          setGraphUpdateError(undefined);
          return;
        }
        if (message.error) {
          setGraph((current) => (current ? { ...current, pending: false } : current));
          setGraphUpdateError(message.error);
        }
      });
    };
    window.addEventListener("message", handleGraphUpdated);
    return () => window.removeEventListener("message", handleGraphUpdated);
  }, []);

  useEffect(
    () => () => {
      if (positionSyncReleaseTimerRef.current) {
        clearTimeout(positionSyncReleaseTimerRef.current);
      }
    },
    [],
  );

  useEffect(() => {
    configureGraphForces(graph2dRef.current);
    configureGraphForces(graph3dRef.current);
  }, [renderGraphData2d, renderGraphData3d]);

  useEffect(() => {
    setSelection((current) =>
      current
        ? selectionForStatsTarget({ type: current.type, id: current.item.id }, filteredGraphData)
        : undefined,
    );
  }, [filteredGraphData]);

  useEffect(() => {
    if (layoutSize.width <= 780) {
      return;
    }
    setInspectorWidth((currentWidth) =>
      clamp(currentWidth, inspectorMinimumWidth, maxInspectorWidthForLayout(layoutSize.width)),
    );
  }, [layoutSize.width]);

  useLayoutEffect(() => {
    const pending = pendingSyncRef.current;
    if (!pending || pending.to !== mode || !canFitGraph) {
      return;
    }
    const graphRef = mode === "3d" ? graph3dRef.current : graph2dRef.current;
    if (!graphRef) {
      return;
    }
    const pinnedNodeIds =
      mode === "3d"
        ? applyPositionSyncTo3d(pending, renderGraphData.nodes, graph3dRef.current)
        : applyPositionSyncTo2d(pending, renderGraphData.nodes, graph2dRef.current);
    pendingSyncRef.current = undefined;
    if (pinnedNodeIds.size === 0) {
      return;
    }
    graphRef.d3ReheatSimulation();
    if (positionSyncReleaseTimerRef.current) {
      clearTimeout(positionSyncReleaseTimerRef.current);
    }
    positionSyncReleaseTimerRef.current = setTimeout(() => {
      if (positionSyncGenerationRef.current !== pending.generation) {
        return;
      }
      releasePositionSyncPins(renderGraphData.nodes, pinnedNodeIds);
      const currentGraphRef = mode === "3d" ? graph3dRef.current : graph2dRef.current;
      currentGraphRef?.d3ReheatSimulation();
    }, positionSyncPinMs);
  }, [canFitGraph, mode, renderGraphData.nodes]);

  useEffect(() => {
    capturePositionSyncEntries(
      mode,
      renderGraphData.nodes,
      graph2dRef.current,
      graph3dRef.current,
      positionSyncRef.current,
    );
  }, [mode, renderGraphData.nodes]);

  useEffect(() => {
    if (mode === "3d") {
      graph3dRef.current?.resumeAnimation();
      graph2dRef.current?.pauseAnimation();
    } else {
      graph2dRef.current?.resumeAnimation();
      graph3dRef.current?.pauseAnimation();
    }
  }, [mode]);

  if (!graph) {
    return (
      <Shell theme={theme}>
        <main className="grid place-items-center text-[#9aa7b8]">
          {graphText("empty.graphData")}
        </main>
      </Shell>
    );
  }

  return (
    <Shell theme={theme}>
      <header className="grid grid-cols-[minmax(180px,1fr)_minmax(220px,320px)_auto_auto_auto] items-center gap-3 border-b border-[#2b3442] bg-[#171c25] px-3 py-2.5 max-[980px]:grid-cols-1 max-[980px]:items-stretch">
        <div className="flex min-w-0 items-center gap-2.5">
          <span className="overflow-hidden text-ellipsis whitespace-nowrap text-[13px] font-semibold text-[#d7dde8]">
            {graphScopeTitle(graph.scope)}
          </span>
          {titleFileName ? (
            <span
              className="min-w-0 overflow-hidden text-ellipsis whitespace-nowrap text-[12px] text-[#9aa7b8]"
              title={titleFileName}
            >
              {titleFileName}
            </span>
          ) : null}
          {graphUpdateText ? (
            <span
              className={
                graphUpdateError
                  ? "min-w-0 overflow-hidden text-ellipsis whitespace-nowrap text-[11px] text-[#ff9cac]"
                  : "min-w-0 overflow-hidden text-ellipsis whitespace-nowrap text-[11px] text-[#89ddff]"
              }
              title={graphUpdateText}
            >
              {graphUpdateText}
            </span>
          ) : null}
          {graph.truncated && truncatedGraphText ? (
            <span className="inline-flex min-w-0 items-center gap-1.5 text-[11px] text-[#ffcb6b]">
              <span className="min-w-0 overflow-hidden text-ellipsis" title={truncatedGraphTitle}>
                {truncatedGraphText}
              </span>
              <button
                type="button"
                className="shrink-0 rounded border border-[#6d5d2b] px-1.5 py-0.5 text-[11px] text-[#ffe29b] hover:border-[#ffcb6b] hover:bg-[#2a2415] focus:border-[#ffcb6b] focus:outline-none"
                title={truncatedGraphTitle}
                onClick={() =>
                  vscode.postMessage({ type: "openSetting", setting: "aspLsp.graph.maxNodes" })
                }
              >
                {graphText("toolbar.truncatedSetting")}
              </button>
            </span>
          ) : null}
        </div>
        <div className="grid min-w-0 grid-cols-[minmax(0,1fr)_auto] items-center gap-2">
          <ImeSafeInput
            ref={searchInputRef}
            type="search"
            className="h-7 min-w-0 rounded-md border border-[#394456] bg-[#11151c] px-2.5 text-xs text-[#d7dde8] outline-none placeholder:text-[#717b8c] focus:border-[#89ddff]"
            aria-label={graphText("toolbar.searchNodes")}
            placeholder={graphText("toolbar.searchNodes")}
            value={searchInput}
            onValueChange={updateSearchInput}
          />
          <label
            className="inline-flex h-7 cursor-pointer select-none items-center gap-1.5 whitespace-nowrap rounded-md border border-[#394456] bg-[#151a22] px-2 text-[11px] text-[#b5c0d0]"
            title={graphText("toolbar.matchCaseDescription")}
          >
            <input
              type="checkbox"
              className="m-0 h-3.5 w-3.5 accent-[#89ddff]"
              checked={searchMatchCase}
              onChange={(event) => setSearchMatchCase(event.currentTarget.checked)}
            />
            {graphText("toolbar.matchCase")}
          </label>
        </div>
        <GraphStatsPopover
          graphData={filteredGraphData}
          stats={filteredStats}
          onSelectTarget={selectAndFocusGraphTarget}
        />
        <div
          className="inline-grid grid-cols-2 overflow-hidden rounded-md border border-[#394456]"
          aria-label={graphText("toolbar.graphMode")}
          title={graphText("toolbar.graphModeDescription")}
        >
          <button
            type="button"
            className={cn(
              "h-7 min-w-[42px] cursor-pointer border-0",
              mode === "3d" ? "bg-[#89ddff] text-[#11151c]" : "bg-[#151a22] text-[#b5c0d0]",
            )}
            title={graphText("toolbar.graphModeDescription")}
            onClick={() => switchGraphMode("3d")}
          >
            3D
          </button>
          <button
            type="button"
            className={cn(
              "h-7 min-w-[42px] cursor-pointer border-0",
              mode === "2d" ? "bg-[#89ddff] text-[#11151c]" : "bg-[#151a22] text-[#b5c0d0]",
            )}
            title={graphText("toolbar.graphModeDescription")}
            onClick={() => switchGraphMode("2d")}
          >
            2D
          </button>
        </div>
        <button
          type="button"
          className="h-7 min-w-[42px] cursor-pointer rounded-md border border-[#394456] bg-[#151a22] px-2.5 text-xs text-[#b5c0d0] disabled:cursor-not-allowed disabled:text-[#717b8c]"
          aria-label={graphText("action.fitGraph")}
          title={graphText("action.fitGraph")}
          disabled={!canFitGraph}
          onClick={() => fitGraphToCanvas(mode)}
        >
          {graphText("action.fit")}
        </button>
      </header>
      <main ref={layoutRef} className={layoutClassName} style={layoutStyle}>
        <section ref={surfaceRef} className={surfaceClassName}>
          <GraphLegend
            hiddenLinkCategories={hiddenLinkCategories}
            hiddenNodeCategories={hiddenNodeCategories}
            hideSingleNodes={hideSingleNodes}
            hideUnreferencedGlobalSymbols={hideUnreferencedGlobalSymbols}
            linkCategories={linkFilterOrder}
            nodeCategories={nodeCategoryOrder}
            themePalette={themePalette}
            showOutgoingSelectionLinks={showOutgoingSelectionLinks}
            onToggleHideSingleNodes={() => setHideSingleNodes((current) => !current)}
            onToggleHideUnreferencedGlobalSymbols={() =>
              setHideUnreferencedGlobalSymbols((current) => !current)
            }
            onToggleLinkCategory={toggleLinkCategory}
            onToggleNodeCategory={toggleNodeCategory}
            onToggleShowOutgoingSelectionLinks={() =>
              setShowOutgoingSelectionLinks((current) => !current)
            }
          />
          <div
            className={
              mode === "3d" ? "absolute inset-0" : "pointer-events-none absolute inset-0 opacity-0"
            }
            aria-hidden={mode !== "3d"}
          >
            <ForceGraph3D
              ref={graph3dRef}
              graphData={renderGraphData3d}
              width={surfaceSize.width}
              height={surfaceSize.height}
              backgroundColor={themePalette.canvasBackground}
              nodeColor={(node) => nodeColor(node as GraphNode, highlight, themePalette)}
              nodeVal={(node) => (node as GraphNode).value}
              nodeLabel={(node) => nodeLabel(node as GraphNode)}
              nodeThreeObjectExtend={true}
              nodeThreeObject={(node: GraphNode) => nodeTextObject(node, highlight, themePalette)}
              linkColor={(link) => linkColor(link as GraphLink, highlight, themePalette)}
              linkWidth={(link) => linkWidth3d(link as GraphLink, highlight)}
              linkLabel={(link) => linkLabel(link as GraphLink)}
              linkCurvature={0.25}
              linkDirectionalArrowLength={(link) => linkArrowLength(link as GraphLink)}
              linkDirectionalArrowRelPos={1}
              linkDirectionalArrowColor={(link) =>
                linkColor(link as GraphLink, highlight, themePalette)
              }
              linkDirectionalParticles={(link) => linkParticleCount(link as GraphLink, highlight)}
              linkDirectionalParticleWidth={(link) => linkParticleWidth3d(link as GraphLink)}
              onNodeClick={(node) => selectGraphNode(node as GraphNode)}
              onLinkClick={(link) => selectGraphLink(link as GraphLink)}
              onBackgroundClick={() => setSelection(undefined)}
              d3VelocityDecay={graphForceVelocityDecay}
              cooldownTicks={100}
              onEngineStop={() => handleEngineStop("3d")}
            />
          </div>
          <div
            className={
              mode === "2d" ? "absolute inset-0" : "pointer-events-none absolute inset-0 opacity-0"
            }
            aria-hidden={mode !== "2d"}
          >
            <ForceGraph2D
              ref={graph2dRef}
              graphData={renderGraphData2d}
              width={surfaceSize.width}
              height={surfaceSize.height}
              backgroundColor={themePalette.canvasBackground}
              nodeVal={(node) => (node as GraphNode).value}
              nodeLabel={(node) => nodeLabel(node as GraphNode)}
              linkColor={(link) => linkColor(link as GraphLink, highlight, themePalette)}
              linkWidth={(link) => linkWidth2d(link as GraphLink, highlight)}
              linkLabel={(link) => linkLabel(link as GraphLink)}
              linkCurvature={0.25}
              linkDirectionalArrowLength={(link) => linkArrowLength(link as GraphLink)}
              linkDirectionalArrowRelPos={1}
              linkDirectionalArrowColor={(link) =>
                linkColor(link as GraphLink, highlight, themePalette)
              }
              linkDirectionalParticles={(link) => linkParticleCount(link as GraphLink, highlight)}
              linkDirectionalParticleWidth={4.5}
              nodeCanvasObject={(node, canvas) =>
                paintNode(node as GraphNode, canvas, highlight, themePalette)
              }
              nodePointerAreaPaint={(node, color, canvas) =>
                paintNodePointerArea(node as GraphNode, color, canvas)
              }
              onNodeClick={(node) => selectGraphNode(node as GraphNode)}
              onLinkClick={(link) => selectGraphLink(link as GraphLink)}
              onBackgroundClick={() => setSelection(undefined)}
              d3VelocityDecay={graphForceVelocityDecay}
              cooldownTicks={100}
              onEngineStop={() => handleEngineStop("2d")}
            />
          </div>
        </section>
        <PaneResizeHandle
          maxWidth={maximumInspectorWidth}
          minWidth={inspectorMinimumWidth}
          position={inspectorPosition}
          width={clampedInspectorWidth}
          onWidthChange={setInspectorWidth}
        />
        <Inspector
          key={graphRevision}
          graphData={graphData}
          visibleGraphData={filteredGraphData}
          selection={selection}
          position={inspectorPosition}
          onClose={() => setSelection(undefined)}
          onSelectLink={selectAndFocusGraphLink}
          onSelectNode={selectAndFocusGraphNode}
        />
      </main>
    </Shell>
  );
}

function graphScopeTitle(scope: AspGraphPayload["scope"]): string {
  if (scope === "document") {
    return graphText("view.currentFileGraph");
  }
  if (scope === "folder") {
    return graphText("view.folderGraph");
  }
  return graphText("view.workspaceGraph");
}

function graphRootName(payload: AspGraphPayload | undefined): string | undefined {
  if (!payload || payload.scope === "workspace") {
    return undefined;
  }
  if (payload.scope === "folder") {
    return baseNameFromUri(payload.rootUri);
  }
  const rootNode =
    payload.nodes.find((node) => node.isRoot) ??
    payload.nodes.find((node) => node.uri === payload.rootUri);
  return (
    rootNode?.label || baseNameFromPath(rootNode?.fileName) || baseNameFromUri(payload.rootUri)
  );
}

function baseNameFromPath(value: string | undefined): string | undefined {
  const fileName = value?.replaceAll("\\", "/").split("/").filter(Boolean).at(-1);
  return fileName || undefined;
}

function baseNameFromUri(value: string | undefined): string | undefined {
  if (!value) {
    return undefined;
  }
  try {
    return baseNameFromPath(decodeURIComponent(new URL(value).pathname));
  } catch {
    return baseNameFromPath(value);
  }
}

function GraphLegend({
  hiddenLinkCategories,
  hiddenNodeCategories,
  hideSingleNodes,
  hideUnreferencedGlobalSymbols,
  linkCategories,
  nodeCategories,
  themePalette,
  showOutgoingSelectionLinks,
  onToggleHideSingleNodes,
  onToggleHideUnreferencedGlobalSymbols,
  onToggleLinkCategory,
  onToggleNodeCategory,
  onToggleShowOutgoingSelectionLinks,
}: {
  hiddenLinkCategories: ReadonlySet<LinkFilterCategory>;
  hiddenNodeCategories: ReadonlySet<NodeColorCategory>;
  hideSingleNodes: boolean;
  hideUnreferencedGlobalSymbols: boolean;
  linkCategories: LinkFilterCategory[];
  nodeCategories: NodeColorCategory[];
  themePalette: GraphThemePalette;
  showOutgoingSelectionLinks: boolean;
  onToggleHideSingleNodes(): void;
  onToggleHideUnreferencedGlobalSymbols(): void;
  onToggleLinkCategory(category: LinkFilterCategory): void;
  onToggleNodeCategory(category: NodeColorCategory): void;
  onToggleShowOutgoingSelectionLinks(): void;
}): React.ReactElement {
  const [isOpen, setOpen] = useState(false);
  const regularNodeCategories = nodeCategories.filter(
    (category) => !unresolvedNodeCategorySet.has(category),
  );
  const unresolvedNodeCategories = nodeCategories.filter((category) =>
    unresolvedNodeCategorySet.has(category),
  );
  return (
    <div className="absolute top-3 left-3 z-10 grid max-w-[min(560px,calc(100%_-_24px))] gap-0 rounded-md border border-[#303a49] bg-[#171c25]/90 px-2 py-0.5 shadow-[0_10px_26px_rgb(0_0_0_/_28%)] backdrop-blur">
      <button
        type="button"
        className="flex h-7 min-w-0 cursor-pointer items-center justify-between gap-3 border-0 bg-transparent p-0 text-left"
        aria-expanded={isOpen}
        title={graphText("legend.heading")}
        onClick={() => setOpen((current) => !current)}
      >
        <span className="min-w-0 overflow-hidden text-ellipsis whitespace-nowrap text-[11px] font-semibold tracking-[0.08em] text-[#9aa7b8] uppercase">
          {graphText("legend.heading")}
        </span>
        <span className="text-[11px] text-[#8d98a8]" aria-hidden="true">
          {isOpen ? "-" : "+"}
        </span>
      </button>
      {isOpen ? (
        <div className="grid gap-2 pt-1.5 pb-2">
          <LegendFilterGroup title={graphText("legend.linkFilters")}>
            {linkCategories.map((category) => (
              <LegendFilterItem
                key={category}
                checked={!hiddenLinkCategories.has(category)}
                color={themePalette.linkFilterColors[category]}
                label={linkFilterLabels[category]}
                title={linkFilterDescriptions[category]}
                variant="link"
                onToggle={() => onToggleLinkCategory(category)}
              />
            ))}
          </LegendFilterGroup>
          <LegendFilterGroup title={graphText("legend.nodeFilters")}>
            {regularNodeCategories.map((category) => (
              <LegendFilterItem
                key={category}
                checked={!hiddenNodeCategories.has(category)}
                color={themePalette.nodeColors[category]}
                label={nodeCategoryLabels[category]}
                title={nodeCategoryDescriptions[category]}
                variant="node"
                onToggle={() => onToggleNodeCategory(category)}
              />
            ))}
          </LegendFilterGroup>
          <LegendFilterGroup title={graphText("legend.unresolvedNodeFilters")}>
            {unresolvedNodeCategories.map((category) => (
              <LegendFilterItem
                key={category}
                checked={!hiddenNodeCategories.has(category)}
                color={themePalette.nodeColors[category]}
                label={nodeCategoryLabels[category]}
                title={nodeCategoryDescriptions[category]}
                variant="node"
                onToggle={() => onToggleNodeCategory(category)}
              />
            ))}
          </LegendFilterGroup>
          <LegendFilterGroup title={graphText("legend.visibilityFilters")}>
            <LegendFilterItem
              checked={hideSingleNodes}
              color={themePalette.mutedNode}
              label={graphText("legend.hideSingleNodes")}
              title={graphText("legend.hideSingleNodesDescription")}
              variant="node"
              onToggle={onToggleHideSingleNodes}
            />
            <LegendFilterItem
              checked={hideUnreferencedGlobalSymbols}
              color={themePalette.mutedNode}
              label={graphText("legend.hideUnreferencedGlobalSymbols")}
              title={graphText("legend.hideUnreferencedGlobalSymbolsDescription")}
              variant="node"
              onToggle={onToggleHideUnreferencedGlobalSymbols}
            />
          </LegendFilterGroup>
          <LegendFilterGroup title={graphText("legend.selection")}>
            <LegendFilterItem
              checked={showOutgoingSelectionLinks}
              color={themePalette.nodeColors.function}
              label={graphText("legend.outgoingLinks")}
              title={graphText("legend.outgoingLinksDescription")}
              variant="link"
              onToggle={onToggleShowOutgoingSelectionLinks}
            />
          </LegendFilterGroup>
        </div>
      ) : null}
    </div>
  );
}

function LegendFilterGroup({
  children,
  title,
}: {
  children: React.ReactNode;
  title: string;
}): React.ReactElement {
  return (
    <div>
      <LegendHeading>{title}</LegendHeading>
      <div className="grid grid-cols-[repeat(2,minmax(0,1fr))] gap-x-3 gap-y-1.5 max-[560px]:grid-cols-1">
        {children}
      </div>
    </div>
  );
}

function LegendHeading({ children }: { children: React.ReactNode }): React.ReactElement {
  return (
    <div className="mb-1.5 text-[11px] font-semibold tracking-[0.08em] text-[#9aa7b8] uppercase">
      {children}
    </div>
  );
}

function LegendFilterItem({
  checked,
  color,
  label,
  onToggle,
  title,
  variant,
}: {
  checked: boolean;
  color: string;
  label: string;
  onToggle(): void;
  title?: string;
  variant: "link" | "node";
}): React.ReactElement {
  return (
    <label className="group relative flex min-w-0 cursor-pointer select-none items-center gap-2 text-[11px] text-[#d7dde8]">
      <input
        type="checkbox"
        className="m-0 h-3.5 w-3.5 shrink-0 accent-[#89ddff]"
        checked={checked}
        onChange={onToggle}
      />
      {variant === "link" ? (
        <span
          className="h-0 w-7 shrink-0 rounded-full border-t-2"
          style={{
            borderColor: color,
            borderTopWidth: label === linkFilterLabels.include ? 4 : 2,
          }}
          aria-hidden="true"
        />
      ) : (
        <span
          className="h-2.5 w-2.5 shrink-0 rounded-full"
          style={{ backgroundColor: color }}
          aria-hidden="true"
        />
      )}
      <span className="min-w-0 overflow-hidden text-ellipsis whitespace-nowrap">{label}</span>
      {title ? (
        <span
          role="tooltip"
          className="pointer-events-none absolute top-[calc(100%_+_6px)] left-0 z-30 hidden w-[min(260px,calc(100vw_-_40px))] rounded-md border border-[#405068] bg-[#0d1117] px-2 py-1.5 text-[11px] leading-[1.35] whitespace-normal text-[#d7dde8] shadow-[0_10px_24px_rgb(0_0_0_/_35%)] group-focus-within:block group-hover:block"
        >
          {title}
        </span>
      ) : null}
    </label>
  );
}

function useElementSize<TElement extends HTMLElement>(): [
  React.RefObject<TElement | null>,
  { width: number; height: number },
] {
  const ref = useRef<TElement>(null);
  const [size, setSize] = useState({ width: 0, height: 0 });

  useLayoutEffect(() => {
    const element = ref.current;
    if (!element) {
      return undefined;
    }

    const updateSize = () => {
      const { width, height } = element.getBoundingClientRect();
      const nextSize = {
        width: Math.max(1, Math.floor(width)),
        height: Math.max(1, Math.floor(height)),
      };
      setSize((currentSize) =>
        currentSize.width === nextSize.width && currentSize.height === nextSize.height
          ? currentSize
          : nextSize,
      );
    };

    updateSize();
    const observer = new ResizeObserver(updateSize);
    observer.observe(element);
    return () => observer.disconnect();
  }, []);

  return [ref, size];
}

function Shell({
  children,
  theme,
}: {
  children: React.ReactNode;
  theme: WebviewTheme;
}): React.ReactElement {
  return (
    <>
      <style>{tailwindStyles}</style>
      <div
        className="asp-lsp-graph-shell grid h-full min-w-0 grid-rows-[auto_1fr] bg-[#11151c] text-[#d7dde8]"
        data-asp-lsp-theme={theme}
      >
        {children}
      </div>
    </>
  );
}

function GraphStatsPopover({
  graphData,
  onSelectTarget,
  stats,
}: {
  graphData: GraphData;
  onSelectTarget(target: GraphStatsTarget): void;
  stats: AspGraphPayload["stats"];
}): React.ReactElement {
  const [isOpen, setOpen] = useState(false);
  const [activeMetric, setActiveMetric] = useState<GraphStatsMetric>("files");
  const containerRef = useRef<HTMLDivElement>(null);
  const statsItems = useMemo(
    () => statsItemsForMetric(activeMetric, graphData),
    [activeMetric, graphData],
  );

  useEffect(() => {
    if (!isOpen) {
      return;
    }
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        setOpen(false);
      }
    };
    const closeOnOutsidePointerDown = (event: PointerEvent) => {
      const target = event.target;
      if (target instanceof Node && containerRef.current?.contains(target)) {
        return;
      }
      setOpen(false);
    };
    window.addEventListener("keydown", closeOnEscape);
    window.addEventListener("pointerdown", closeOnOutsidePointerDown);
    return () => {
      window.removeEventListener("keydown", closeOnEscape);
      window.removeEventListener("pointerdown", closeOnOutsidePointerDown);
    };
  }, [isOpen]);

  return (
    <div ref={containerRef} className="relative inline-flex justify-end max-[980px]:justify-start">
      <button
        type="button"
        className="inline-flex h-7 cursor-pointer items-center rounded-md border border-[#394456] bg-[#151a22] px-2.5 text-xs text-[#b5c0d0] hover:border-[#4b5a70] hover:text-[#d7dde8]"
        aria-haspopup="dialog"
        aria-expanded={isOpen}
        aria-label={graphText("stats.show")}
        title={graphText("stats.show")}
        onClick={() => setOpen((current) => !current)}
      >
        <span className="font-semibold text-[#d7dde8]">{graphText("toolbar.stats")}</span>
      </button>
      {isOpen ? (
        <div
          role="dialog"
          aria-label={graphText("stats.title")}
          className="absolute top-[calc(100%_+_6px)] right-0 z-20 grid w-[min(430px,calc(100vw_-_24px))] gap-2 rounded-md border border-[#303a49] bg-[#171c25] p-2 shadow-[0_14px_34px_rgb(0_0_0_/_34%)] max-[980px]:right-auto max-[980px]:left-0"
        >
          <div className="text-[11px] font-semibold tracking-[0.08em] text-[#9aa7b8] uppercase">
            {graphText("stats.heading")}
          </div>
          <div className="grid grid-cols-2 gap-2 max-[360px]:grid-cols-1">
            <MetricButton
              activeMetric={activeMetric}
              label={graphText("stats.metric.files")}
              metric="files"
              value={stats.files}
              onSelect={setActiveMetric}
            />
            <MetricButton
              activeMetric={activeMetric}
              label={graphText("stats.metric.vb")}
              metric="declarations"
              value={stats.declarations}
              onSelect={setActiveMetric}
            />
            <MetricButton
              activeMetric={activeMetric}
              label={graphText("stats.metric.links")}
              metric="links"
              value={stats.links}
              onSelect={setActiveMetric}
            />
            <MetricButton
              activeMetric={activeMetric}
              label={graphText("stats.metric.missing")}
              metric="missingIncludes"
              value={stats.missingIncludes}
              onSelect={setActiveMetric}
            />
          </div>
          <GraphStatsList
            items={statsItems}
            onSelectItem={(target) => {
              setOpen(false);
              onSelectTarget(target);
            }}
          />
        </div>
      ) : null}
    </div>
  );
}

function MetricButton({
  activeMetric,
  label,
  metric,
  onSelect,
  value,
}: {
  activeMetric: GraphStatsMetric;
  label: string;
  metric: GraphStatsMetric;
  onSelect(metric: GraphStatsMetric): void;
  value: number;
}): React.ReactElement {
  const active = activeMetric === metric;
  return (
    <button
      type="button"
      className={
        active
          ? "inline-flex cursor-pointer items-baseline gap-[5px] rounded-md border border-[#89ddff] bg-[#1c2d3a] px-[7px] py-[3px] text-left text-[11px] text-[#d7dde8]"
          : "inline-flex cursor-pointer items-baseline gap-[5px] rounded-md border border-[#303a49] bg-transparent px-[7px] py-[3px] text-left text-[11px] text-[#9aa7b8] hover:border-[#4b5a70] hover:text-[#d7dde8]"
      }
      aria-pressed={active}
      title={`${label}: ${value}`}
      onClick={() => onSelect(metric)}
    >
      <span>{label}</span>
      <strong className="text-xs text-[#f4f7fb]">{value}</strong>
    </button>
  );
}

function GraphStatsList({
  items,
  onSelectItem,
}: {
  items: GraphStatsListItem[];
  onSelectItem(target: GraphStatsTarget): void;
}): React.ReactElement {
  if (items.length === 0) {
    return (
      <p className="m-0 rounded-md border border-[#303a49] bg-[#11151c] p-2 text-xs text-[#8d98a8]">
        {graphText("empty.matchingItems")}
      </p>
    );
  }
  return (
    <VirtualList
      className="grid gap-1.5"
      estimateSize={(item) => (item.detail ? 72 : 48)}
      getKey={(item) => item.id}
      items={items}
      maxHeight={288}
      overscan={8}
      renderItem={(item) => (
        <button
          type="button"
          className="grid w-full cursor-pointer gap-1 rounded-md border border-[#303a49] bg-[#11151c] p-2 text-left hover:border-[#4b5a70]"
          title={detailParts(item.title, item.detail, item.status).join(" · ")}
          onClick={() => onSelectItem(item.target)}
        >
          <div className="flex min-w-0 items-center gap-2">
            {item.color ? (
              <GraphStatsColorIndicator
                color={item.color}
                lineWidth={item.lineWidth}
                variant={item.target.type}
              />
            ) : null}
            <span className="min-w-0 flex-1 overflow-hidden text-ellipsis whitespace-nowrap text-xs font-semibold text-[#d7dde8]">
              {item.title}
            </span>
            {item.status ? (
              <span className="shrink-0 rounded border border-[#405068] px-1.5 py-[1px] text-[10px] text-[#9aa7b8]">
                {item.status}
              </span>
            ) : null}
          </div>
          {item.detail ? (
            <div className="text-[11px] leading-[1.35] text-[#8d98a8] [overflow-wrap:anywhere]">
              {item.detail}
            </div>
          ) : null}
        </button>
      )}
    />
  );
}

function GraphStatsColorIndicator({
  color,
  lineWidth,
  variant,
}: {
  color: string;
  lineWidth?: number;
  variant: GraphStatsTarget["type"];
}): React.ReactElement {
  if (variant === "link") {
    return (
      <span
        className="h-0 w-7 shrink-0 rounded-full border-t-2"
        style={{ borderColor: color, borderTopWidth: lineWidth ?? 2 }}
        aria-hidden="true"
      />
    );
  }
  return (
    <span
      className="h-2.5 w-2.5 shrink-0 rounded-full"
      style={{ backgroundColor: color }}
      aria-hidden="true"
    />
  );
}

function statsItemsForMetric(metric: GraphStatsMetric, graphData: GraphData): GraphStatsListItem[] {
  switch (metric) {
    case "files":
      return graphData.nodes.filter(isFileLikeGraphNode).map((node) => ({
        id: `file:${node.id}`,
        title: node.label,
        target: { type: "node", id: node.id },
        detail: detailParts(nodeFileLabel(node)).join(" · "),
        status: fileStatsStatus(node),
        color: node.color,
      }));
    case "declarations":
      return graphData.nodes
        .filter((node) => node.kind === "vbDeclaration")
        .map((node) => ({
          id: `declaration:${node.id}`,
          title: node.label,
          target: { type: "node", id: node.id },
          detail: detailParts(
            nodeTypeLabel(node),
            nodeFileLabel(node),
            rangeLineLabel(node.range),
          ).join(" · "),
          status: nodeStatusLabel(node),
          color: node.color,
        }));
    case "links":
      return linkStatsItems(graphData);
    case "missingIncludes":
      return missingIncludeStatsItems(graphData);
  }
}

function linkStatsItems(graphData: GraphData): GraphStatsListItem[] {
  const nodesById = graphNodeMap(graphData.nodes);
  return graphData.links.map((link) => ({
    id: `link:${link.id}`,
    title: linkStatsTitle(link),
    target: { type: "link", id: link.id },
    detail: detailParts(
      `${endpointLabel(link.source, nodesById)} -> ${endpointLabel(link.target, nodesById)}`,
      graphRoleLabel(link.role),
    ).join(" · "),
    status: `x${link.count}`,
    color: link.color,
    lineWidth: linkSwatchWidth(link),
  }));
}

function missingIncludeStatsItems(graphData: GraphData): GraphStatsListItem[] {
  const nodesById = graphNodeMap(graphData.nodes);
  return graphData.links
    .filter((link) => link.kind === "include" && link.include?.exists === false)
    .map((link) => {
      const directive = link.ranges[0];
      return {
        id: `missing:${link.id}`,
        title: link.include?.path ?? link.label,
        target: { type: "link", id: link.id },
        detail: detailParts(
          directive
            ? graphText("detail.directive", { source: directiveSourceLabel(directive) })
            : undefined,
          `${endpointLabel(link.source, nodesById)} -> ${endpointLabel(link.target, nodesById)}`,
          includeModeLabel(link.include?.mode),
        ).join(" · "),
        status: graphText("label.missing"),
        color: link.color,
        lineWidth: linkSwatchWidth(link),
      };
    });
}

function fileStatsStatus(node: GraphNode): string {
  const status = detailParts(
    node.isRoot ? graphText("label.root") : undefined,
    node.exists === false ? graphText("label.missing") : undefined,
  );
  return status.length > 0 ? status.join(" / ") : graphText("label.file");
}

function linkStatsTitle(link: GraphLink): string {
  if (graphLinkFilterCategory(link) === "member") {
    return linkFilterLabels.member;
  }
  return linkMeanings[link.kind].label;
}

function endpointLabel(
  endpoint: string | GraphNode,
  nodesById: ReadonlyMap<string, GraphNode>,
): string {
  const id = nodeIdForEndpoint(endpoint);
  const node = nodesById.get(id);
  return node ? graphNodeDisplayText(node) : id;
}

function graphNodeDisplayText(node: GraphNode): string {
  return node.kind === "vbMemberReference" ? (node.fullPath ?? node.label) : node.label;
}

function directiveSourceLabel(location: {
  uri: string;
  displayPath?: string;
  range: AspGraphRange;
}): string {
  return detailParts(
    location.displayPath ?? pathForDisplay(location.uri) ?? location.uri,
    rangeLineLabel(location.range),
  ).join(" ");
}

function rangeLineLabel(range: AspGraphRange | undefined): string | undefined {
  return range ? graphText("detail.line", { line: range.start.line + 1 }) : undefined;
}

function detailParts(...values: Array<string | undefined>): string[] {
  return values.filter((value): value is string => Boolean(value));
}

function PaneResizeHandle({
  maxWidth,
  minWidth,
  onWidthChange,
  position,
  width,
}: {
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
          width + (position === "left" ? -paneResizeKeyboardStep : paneResizeKeyboardStep),
        );
      } else if (event.key === "ArrowRight") {
        event.preventDefault();
        updateWidth(
          width + (position === "left" ? paneResizeKeyboardStep : -paneResizeKeyboardStep),
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
      aria-label={graphText("view.resizeInspector")}
      aria-orientation="vertical"
      aria-valuemin={minWidth}
      aria-valuemax={maxWidth}
      aria-valuenow={width}
      title={graphText("view.resizeInspector")}
      className="relative order-2 cursor-col-resize bg-[#11151c] outline-none before:absolute before:inset-y-0 before:left-1/2 before:w-px before:-translate-x-1/2 before:bg-[#2b3442] hover:bg-[#1c2430] focus:bg-[#1c2430] focus:before:bg-[#89ddff] max-[780px]:hidden"
      onKeyDown={handleKeyDown}
      onPointerDown={handlePointerDown}
    />
  );
}

function Inspector({
  graphData,
  visibleGraphData,
  onSelectLink,
  onSelectNode,
  position,
  selection,
  onClose,
}: {
  graphData: GraphData;
  visibleGraphData: GraphData;
  onSelectLink(link: GraphLink): void;
  onSelectNode(node: GraphNode): void;
  position: InfoPanelPosition;
  selection: Selection;
  onClose(): void;
}): React.ReactElement {
  const borderClass = position === "left" ? "border-r" : "border-l";
  const orderClass = position === "left" ? "order-1" : "order-3";
  const className = selection
    ? `${orderClass} min-w-0 overflow-auto ${borderClass} border-[#2b3442] bg-[#171c25] p-3.5 max-[780px]:absolute max-[780px]:inset-x-2.5 max-[780px]:top-2.5 max-[780px]:z-10 max-[780px]:block max-[780px]:max-h-[min(260px,calc(100%_-_20px))] max-[780px]:rounded-md max-[780px]:border max-[780px]:shadow-[0_14px_34px_rgb(0_0_0_/_34%)]`
    : `${orderClass} min-w-0 overflow-auto ${borderClass} border-[#2b3442] bg-[#171c25] p-3.5 max-[780px]:hidden`;
  if (!selection) {
    return (
      <aside className={className}>
        <h2 className="asp-lsp-graph-inspector-title mb-3 text-sm leading-[1.35] font-semibold [overflow-wrap:anywhere]">
          {graphText("view.inspector")}
        </h2>
        <p className="m-0 text-xs text-[#8d98a8]">{graphText("inspector.selectPrompt")}</p>
      </aside>
    );
  }
  return (
    <aside className={className}>
      <div className="mb-3 flex min-w-0 items-start gap-2">
        <h2
          className="asp-lsp-graph-inspector-title m-0 min-w-0 flex-1 text-sm leading-[1.35] font-semibold [overflow-wrap:anywhere]"
          title={inspectorTitleForSelection(selection, graphData)}
        >
          {inspectorTitleForSelection(selection, graphData)}
        </h2>
        <button
          type="button"
          className="hidden h-7 w-7 shrink-0 rounded-md border border-[#405068] bg-[#202735] text-sm leading-none text-[#d7dde8] max-[780px]:inline-grid max-[780px]:place-items-center"
          aria-label={graphText("inspector.close")}
          title={graphText("inspector.close")}
          onClick={onClose}
        >
          x
        </button>
      </div>
      {selection.type === "node" ? (
        <NodeInspector
          graphData={graphData}
          node={selection.item}
          visibleGraphData={visibleGraphData}
          onSelectLink={onSelectLink}
        />
      ) : (
        <LinkInspector graphData={graphData} link={selection.item} onSelectNode={onSelectNode} />
      )}
    </aside>
  );
}

function inspectorTitleForSelection(selection: Selection, graphData: GraphData): string {
  if (!selection) {
    return graphText("view.inspector");
  }
  if (selection.type === "node") {
    return graphNodeDisplayText(selection.item);
  }
  const nodesById = graphNodeMap(graphData.nodes);
  return `${linkInspectorTypeLabel(selection.item)}: ${endpointLabel(
    selection.item.source,
    nodesById,
  )} -> ${endpointLabel(selection.item.target, nodesById)}`;
}

function NodeInspector({
  graphData,
  node,
  onSelectLink,
  visibleGraphData,
}: {
  graphData: GraphData;
  node: GraphNode;
  onSelectLink(link: GraphLink): void;
  visibleGraphData: GraphData;
}): React.ReactElement {
  const location = node.uri ? { uri: node.uri, range: node.range } : undefined;
  const canOpenFlowchart = Boolean(location?.uri && node.exists !== false);
  return (
    <>
      <NodeDetails node={node} />
      <div className="mb-3 grid grid-cols-2 gap-2">
        <OpenLocationButton
          className="h-[30px] w-full"
          disabled={!location?.uri}
          label={
            isFileLikeGraphNode(node) ? graphText("action.openFile") : graphText("action.open")
          }
          range={location?.range}
          uri={location?.uri}
        />
        <OpenFlowchartButton
          className="h-[30px] w-full"
          disabled={!canOpenFlowchart}
          label={graphText("action.openFlowchart")}
          range={location?.range}
          uri={location?.uri}
        />
      </div>
      {isFileLikeGraphNode(node) ? (
        <FileNodeRelations graphData={graphData} node={node} />
      ) : (
        <NodeSourceSections graphData={graphData} node={node} />
      )}
      <NodeLinkSections graphData={visibleGraphData} node={node} onSelectLink={onSelectLink} />
    </>
  );
}

function LinkInspector({
  graphData,
  link,
  onSelectNode,
}: {
  graphData: GraphData;
  link: GraphLink;
  onSelectNode(node: GraphNode): void;
}): React.ReactElement {
  const nodesById = useMemo(() => graphNodeMap(graphData.nodes), [graphData.nodes]);
  const sourceItems = useMemo(() => sourceItemsForLink(link), [link]);
  const location = link.ranges[0];
  return (
    <>
      <LinkDetails link={link} nodesById={nodesById} onSelectNode={onSelectNode} />
      <div className="mb-3 grid gap-2">
        <Accordion
          count={link.count}
          defaultOpen={true}
          hint={graphText("section.occurrencesHint")}
          headerAction={
            location ? (
              <div className="flex flex-wrap gap-1.5">
                <OpenLocationButton
                  className="h-7 px-2"
                  label={graphText("action.openFirst")}
                  range={location.range}
                  uri={location.uri}
                />
                <OpenFlowchartButton
                  className="h-7 px-2"
                  label={graphText("action.openFlowchart")}
                  range={location.range}
                  uri={location.uri}
                />
              </div>
            ) : undefined
          }
          title={graphText("section.occurrences")}
        >
          {sourceItems.length > 0 ? (
            <SourceFileGroups items={sourceItems} />
          ) : (
            <EmptyInspectorText>{graphText("empty.sourceRanges")}</EmptyInspectorText>
          )}
        </Accordion>
      </div>
    </>
  );
}

interface GraphSourceItem {
  id: string;
  uri: string;
  displayPath?: string;
  range: AspGraphRange;
  highlightRange: AspGraphRange;
  kind: AspGraphSourceRangeRequestItem["kind"];
  title: string;
  detail?: string;
}

interface GraphSourceState {
  loading: boolean;
  byId: Map<string, AspGraphSourceRangeResponseItem>;
}

interface IncludeRelation {
  id: string;
  title: string;
  detail: string;
  fileUri?: string;
  fileRange?: AspGraphRange;
  fileLabel: string;
  directiveUri?: string;
  directiveRange?: AspGraphRange;
  directiveLabel: string;
  exists?: boolean;
}

function NodeLinkSections({
  graphData,
  node,
  onSelectLink,
}: {
  graphData: GraphData;
  node: GraphNode;
  onSelectLink(link: GraphLink): void;
}): React.ReactElement {
  const { incoming, outgoing } = useMemo(
    () => nodeLinksFor(node, graphData.links),
    [graphData.links, node],
  );
  const nodesById = useMemo(() => graphNodeMap(graphData.nodes), [graphData.nodes]);
  return (
    <div className="mb-3 grid gap-2">
      <Accordion
        count={outgoing.length}
        defaultOpen={outgoing.length > 0}
        hint={graphText("section.outgoingLinksHint")}
        title={graphText("section.outgoingLinks")}
      >
        {outgoing.length > 0 ? (
          <NodeLinkList
            direction="outgoing"
            links={outgoing}
            nodesById={nodesById}
            onSelectLink={onSelectLink}
          />
        ) : (
          <EmptyInspectorText>{graphText("empty.outgoingLinks")}</EmptyInspectorText>
        )}
      </Accordion>
      <Accordion
        count={incoming.length}
        defaultOpen={incoming.length > 0}
        hint={graphText("section.incomingLinksHint")}
        title={graphText("section.incomingLinks")}
      >
        {incoming.length > 0 ? (
          <NodeLinkList
            direction="incoming"
            links={incoming}
            nodesById={nodesById}
            onSelectLink={onSelectLink}
          />
        ) : (
          <EmptyInspectorText>{graphText("empty.incomingLinks")}</EmptyInspectorText>
        )}
      </Accordion>
    </div>
  );
}

function NodeLinkList({
  direction,
  links,
  nodesById,
  onSelectLink,
}: {
  direction: "incoming" | "outgoing";
  links: GraphLink[];
  nodesById: ReadonlyMap<string, GraphNode>;
  onSelectLink(link: GraphLink): void;
}): React.ReactElement {
  return (
    <VirtualList
      className="grid gap-2"
      estimateSize={(link) => (nodeLinkDetail(link) ? 104 : 78)}
      getKey={(link) => link.id}
      items={links}
      maxHeight={360}
      overscan={8}
      renderItem={(link) => (
        <button
          type="button"
          className="grid w-full cursor-pointer gap-1 rounded-md border border-[#303a49] bg-[#11151c] p-2 text-left hover:border-[#4b5a70]"
          title={detailParts(
            linkInspectorTypeLabel(link),
            `${endpointLabel(link.source, nodesById)} -> ${endpointLabel(link.target, nodesById)}`,
            nodeLinkDetail(link),
          ).join(" · ")}
          onClick={() => onSelectLink(link)}
        >
          <div className="flex min-w-0 items-center gap-2">
            <span
              className="h-2.5 w-2.5 shrink-0 rounded-full"
              style={{ backgroundColor: link.color }}
              aria-hidden="true"
            />
            <span className="min-w-0 flex-1 overflow-hidden text-ellipsis whitespace-nowrap text-xs font-semibold text-[#d7dde8]">
              {linkInspectorTypeLabel(link)}
            </span>
            <span className="shrink-0 rounded border border-[#405068] bg-[#151a22] px-1.5 py-0.5 text-[10px] leading-none text-[#9aa7b8]">
              {link.count}
            </span>
          </div>
          <div
            className="overflow-hidden text-ellipsis whitespace-nowrap text-[11px] text-[#8d98a8]"
            title={`${endpointLabel(link.source, nodesById)} -> ${endpointLabel(
              link.target,
              nodesById,
            )}`}
          >
            {direction === "outgoing" ? graphText("direction.to") : graphText("direction.from")}{" "}
            {endpointLabel(direction === "outgoing" ? link.target : link.source, nodesById)}
          </div>
          {nodeLinkDetail(link) ? (
            <div className="text-[11px] leading-[1.35] text-[#8d98a8] [overflow-wrap:anywhere]">
              {nodeLinkDetail(link)}
            </div>
          ) : null}
        </button>
      )}
    />
  );
}

function NodeSourceSections({
  graphData,
  node,
}: {
  graphData: GraphData;
  node: GraphNode;
}): React.ReactElement {
  const { declarationItems, usageItems } = useMemo(
    () => sourceItemsForNode(node, graphData),
    [graphData, node],
  );
  const declarationItem = declarationItems[0];
  const declarationSourceState = useSourceRanges(declarationItems);
  return (
    <div className="mb-3 grid gap-2">
      <Accordion
        defaultOpen={true}
        hint={graphText("section.declarationHint")}
        headerAction={
          declarationItem ? (
            <OpenLocationButton
              className="h-7 px-2"
              label={graphText("action.open")}
              range={declarationItem.highlightRange}
              uri={declarationItem.uri}
            />
          ) : undefined
        }
        title={graphText("section.declaration")}
      >
        {declarationItem ? (
          <DeclarationSource item={declarationItem} sourceState={declarationSourceState} />
        ) : (
          <EmptyInspectorText>{graphText("empty.declarationSource")}</EmptyInspectorText>
        )}
      </Accordion>
      <Accordion
        count={usageItems.length}
        defaultOpen={true}
        hint={graphText("section.referencesCallsHint")}
        title={graphText("section.referencesCalls")}
      >
        {usageItems.length > 0 ? (
          <SourceFileGroups items={usageItems} />
        ) : (
          <EmptyInspectorText>{graphText("empty.referencesOrCalls")}</EmptyInspectorText>
        )}
      </Accordion>
    </div>
  );
}

function FileNodeRelations({
  graphData,
  node,
}: {
  graphData: GraphData;
  node: GraphNode;
}): React.ReactElement {
  const { incoming, outgoing } = useMemo(
    () => includeRelationsForFileNode(node, graphData),
    [graphData, node],
  );
  return (
    <div className="mb-3 grid gap-2">
      <Accordion
        count={outgoing.length}
        defaultOpen={true}
        hint={graphText("section.includesHint")}
        title={graphText("section.includes")}
      >
        {outgoing.length > 0 ? (
          <IncludeRelationList relations={outgoing} />
        ) : (
          <EmptyInspectorText>{graphText("empty.includedFiles")}</EmptyInspectorText>
        )}
      </Accordion>
      <Accordion
        count={incoming.length}
        defaultOpen={incoming.length > 0}
        hint={graphText("section.includedByHint")}
        title={graphText("section.includedBy")}
      >
        {incoming.length > 0 ? (
          <IncludeRelationList relations={incoming} />
        ) : (
          <EmptyInspectorText>{graphText("empty.includeSources")}</EmptyInspectorText>
        )}
      </Accordion>
    </div>
  );
}

function Accordion({
  children,
  count,
  defaultOpen,
  hint,
  headerAction,
  title,
}: {
  children: React.ReactNode;
  count?: number;
  defaultOpen: boolean;
  hint?: string;
  headerAction?: React.ReactNode;
  title: string;
}): React.ReactElement {
  const [isOpen, setOpen] = useState(defaultOpen);
  const headerTitle = detailParts(title, hint).join(" · ");
  return (
    <section className="overflow-hidden rounded-md border border-[#303a49] bg-[#151a22]">
      <div className="flex min-h-9 items-center gap-2">
        <div
          role="button"
          tabIndex={0}
          className="flex min-h-9 min-w-0 flex-1 cursor-pointer items-center justify-between gap-2 px-2.5 py-1.5 text-left outline-none focus-visible:ring-1 focus-visible:ring-[#89ddff]"
          aria-expanded={isOpen}
          aria-label={headerTitle}
          onClick={() => setOpen((current) => !current)}
          onKeyDown={(event) => {
            if (event.key === "Enter" || event.key === " ") {
              event.preventDefault();
              setOpen((current) => !current);
            }
          }}
        >
          <span className="flex min-w-0 items-center gap-1.5">
            <TooltipText
              className="min-w-0 overflow-hidden text-ellipsis whitespace-nowrap text-xs font-semibold text-[#d7dde8]"
              text={title}
              tooltip={headerTitle}
            />
            {hint ? (
              <span
                className="shrink-0"
                onClick={(event) => event.stopPropagation()}
                onKeyDown={(event) => event.stopPropagation()}
              >
                <DetailHint hint={hint} label={title} />
              </span>
            ) : null}
          </span>
          <span className="inline-flex shrink-0 items-center gap-2 text-[11px] text-[#8d98a8]">
            {count !== undefined ? <span>{count}</span> : null}
            <span aria-hidden="true">{isOpen ? "-" : "+"}</span>
          </span>
        </div>
        {headerAction ? <div className="shrink-0 pr-2">{headerAction}</div> : null}
      </div>
      {isOpen ? <div className="grid gap-2 border-t border-[#303a49] p-2.5">{children}</div> : null}
    </section>
  );
}

function DeclarationSource({
  item,
  sourceState,
}: {
  item: GraphSourceItem;
  sourceState: GraphSourceState;
}): React.ReactElement {
  const source = sourceState.byId.get(item.id);
  const displayRange = source?.range ?? item.range;
  const loading = sourceState.loading && !source;
  const detail = item.detail ?? graphText("detail.line", { line: displayRange.start.line + 1 });
  return (
    <div className="grid gap-2">
      <TooltipText
        className="overflow-hidden text-ellipsis whitespace-nowrap text-[11px] text-[#8d98a8]"
        text={detail}
      />
      {source?.error ? (
        <p className="m-0 text-[11px] text-[#ff9cac]">{source.error}</p>
      ) : (
        <SourceCodeSnippet className="max-h-80" item={item} loading={loading} source={source} />
      )}
    </div>
  );
}

function SourceFileGroups({ items }: { items: GraphSourceItem[] }): React.ReactElement {
  const groups = groupedSourceItems(items);
  return (
    <VirtualList
      className="grid gap-2"
      estimateSize={(group) => (group.items.length === 1 ? 68 : 96)}
      getKey={(group) => group.uri}
      items={groups}
      maxHeight={620}
      overscan={5}
      renderItem={(group) => (
        <Accordion
          count={group.items.length}
          defaultOpen={groups.length === 1}
          hint={graphText("section.sourceFileHint")}
          title={sourceGroupTitle(group.uri, group.items)}
        >
          <SourceRangeList items={group.items} />
        </Accordion>
      )}
    />
  );
}

function SourceRangeList({ items }: { items: GraphSourceItem[] }): React.ReactElement {
  const [visibleItems, setVisibleItems] = useState<readonly GraphSourceItem[]>(() =>
    items.slice(0, Math.min(items.length, 48)),
  );
  useEffect(() => {
    setVisibleItems(items.slice(0, Math.min(items.length, 48)));
  }, [items]);
  const sourceState = useSourceRanges(visibleItems);
  return (
    <VirtualList
      className="grid gap-2"
      estimateSize={220}
      getKey={(item) => item.id}
      items={items}
      maxHeight={560}
      onVisibleItemsChange={setVisibleItems}
      overscan={8}
      renderItem={(item) => (
        <SourceRangeCard
          item={item}
          loading={sourceState.loading && visibleItems.some((visible) => visible.id === item.id)}
          source={sourceState.byId.get(item.id)}
        />
      )}
    />
  );
}

function SourceRangeCard({
  item,
  loading,
  source,
}: {
  item: GraphSourceItem;
  loading: boolean;
  source: AspGraphSourceRangeResponseItem | undefined;
}): React.ReactElement {
  const displayRange = source?.range ?? item.range;
  const detail = item.detail ?? graphText("detail.line", { line: displayRange.start.line + 1 });
  return (
    <article className="grid gap-2 rounded-md border border-[#303a49] bg-[#11151c] p-2">
      <div className="grid min-w-0 gap-2">
        <div className="min-w-0">
          <TooltipText
            className="overflow-hidden text-ellipsis whitespace-nowrap text-xs font-semibold text-[#d7dde8]"
            text={item.title}
          />
          <TooltipText
            className="overflow-hidden text-ellipsis whitespace-nowrap text-[11px] text-[#8d98a8]"
            text={detail}
          />
        </div>
        <div className="flex min-w-0 flex-wrap gap-1.5">
          <OpenLocationButton
            className="h-7 min-w-[52px] px-2"
            label={graphText("action.open")}
            range={item.highlightRange}
            uri={item.uri}
          />
          <OpenFlowchartButton
            className="h-7 min-w-[128px] px-2"
            label={graphText("action.openFlowchart")}
            range={item.highlightRange}
            uri={item.uri}
          />
        </div>
      </div>
      {source?.error ? (
        <p className="m-0 text-[11px] text-[#ff9cac]">{source.error}</p>
      ) : (
        <SourceCodeSnippet className="max-h-44" item={item} loading={loading} source={source} />
      )}
    </article>
  );
}

function SourceCodeSnippet({
  className,
  item,
  loading,
  source,
}: {
  className: string;
  item: GraphSourceItem;
  loading: boolean;
  source: AspGraphSourceRangeResponseItem | undefined;
}): React.ReactElement {
  const text =
    source?.text ??
    (loading ? graphText("snippet.loadingSource") : graphText("empty.sourceUnavailable"));
  const highlightState = useSnippetHighlightState(Boolean(source?.text));
  const highlightOffsets = sourceHighlightOffsets(source);
  const language = snippetLanguageForSourceItem(item);
  return (
    <pre
      className={cn(
        "m-0 overflow-auto rounded border border-[#253041] bg-[#0d1117] p-2 font-mono text-[11px] leading-[1.45] whitespace-pre-wrap text-[#d7dde8] [tab-size:2]",
        className,
      )}
    >
      {source?.text && highlightState.status === "ready" ? (
        <HighlightedSourceText
          highlightOffsets={highlightOffsets}
          highlighter={highlightState.highlighter}
          language={language}
          text={source.text}
        />
      ) : (
        <PlainSourceText highlightOffsets={highlightOffsets} text={text} />
      )}
    </pre>
  );
}

function sourceHighlightOffsets(
  source: AspGraphSourceRangeResponseItem | undefined,
): HighlightOffsets | undefined {
  if (!source?.text || !source.range || !source.highlightRange) {
    return undefined;
  }
  const start = positionOffsetInRangeText(source.text, source.range, source.highlightRange.start);
  const end = positionOffsetInRangeText(source.text, source.range, source.highlightRange.end);
  if (start === undefined || end === undefined || end <= start) {
    return undefined;
  }
  return { start, end };
}

function PlainSourceText({
  highlightOffsets,
  text,
}: {
  highlightOffsets: HighlightOffsets | undefined;
  text: string;
}): React.ReactElement {
  return <>{renderCodeSegment(text, "plain", 0, highlightOffsets)}</>;
}

function HighlightedSourceText({
  highlightOffsets,
  highlighter,
  language,
  text,
}: {
  highlightOffsets: HighlightOffsets | undefined;
  highlighter: SnippetHighlighter;
  language: SnippetLanguage;
  text: string;
}): React.ReactElement {
  const grammar = language === "classic-asp" ? highlighter.classicAsp : highlighter.vbscript;
  const lines = text.split("\n");
  const children: React.ReactNode[] = [];
  let state: StateStack | null = INITIAL;
  let lineOffset = 0;
  for (let index = 0; index < lines.length; index += 1) {
    const line = lines[index] ?? "";
    const result = grammar.tokenizeLine(line, state);
    state = result.ruleStack;
    children.push(
      <React.Fragment key={`line:${index}`}>
        {renderTokenizedLine(line, result.tokens, lineOffset, highlightOffsets)}
        {index < lines.length - 1 ? "\n" : null}
      </React.Fragment>,
    );
    lineOffset += line.length + (index < lines.length - 1 ? 1 : 0);
  }
  return <>{children}</>;
}

function renderTokenizedLine(
  line: string,
  tokens: Array<{ startIndex: number; endIndex: number; scopes: string[] }>,
  lineOffset: number,
  highlightOffsets: HighlightOffsets | undefined,
): React.ReactNode[] {
  const parts: React.ReactNode[] = [];
  let cursor = 0;
  for (let index = 0; index < tokens.length; index += 1) {
    const token = tokens[index];
    const start = clamp(token.startIndex, 0, line.length);
    const end = clamp(token.endIndex, start, line.length);
    if (start > cursor) {
      parts.push(
        ...renderCodeSegment(
          line.slice(cursor, start),
          `gap:${index}:${cursor}`,
          lineOffset + cursor,
          highlightOffsets,
        ),
      );
    }
    if (end > start) {
      parts.push(
        ...renderCodeSegment(
          line.slice(start, end),
          `token:${index}:${start}`,
          lineOffset + start,
          highlightOffsets,
          tokenStyleForScopes(token.scopes),
        ),
      );
    }
    cursor = end;
  }
  if (cursor < line.length) {
    parts.push(
      ...renderCodeSegment(
        line.slice(cursor),
        `tail:${cursor}`,
        lineOffset + cursor,
        highlightOffsets,
      ),
    );
  }
  return parts.length > 0 ? parts : [""];
}

function renderCodeSegment(
  text: string,
  key: string,
  offset: number,
  highlightOffsets: HighlightOffsets | undefined,
  style?: React.CSSProperties,
): React.ReactNode[] {
  if (!text) {
    return [];
  }
  if (!highlightOffsets) {
    return [styledSourceSpan(key, text, style)];
  }
  const highlightStart = clamp(highlightOffsets.start - offset, 0, text.length);
  const highlightEnd = clamp(highlightOffsets.end - offset, 0, text.length);
  if (highlightEnd <= 0 || highlightStart >= text.length || highlightEnd <= highlightStart) {
    return [styledSourceSpan(key, text, style)];
  }
  const parts: React.ReactNode[] = [];
  if (highlightStart > 0) {
    parts.push(styledSourceSpan(`${key}:before`, text.slice(0, highlightStart), style));
  }
  parts.push(
    <mark key={`${key}:highlight`} className={sourceHighlightMarkClassName} style={style}>
      {text.slice(highlightStart, highlightEnd)}
    </mark>,
  );
  if (highlightEnd < text.length) {
    parts.push(styledSourceSpan(`${key}:after`, text.slice(highlightEnd), style));
  }
  return parts;
}

function styledSourceSpan(
  key: string,
  text: string,
  style: React.CSSProperties | undefined,
): React.ReactElement {
  return (
    <span key={key} style={style}>
      {text}
    </span>
  );
}

function tokenStyleForScopes(scopes: string[]): React.CSSProperties | undefined {
  if (scopes.some((scope) => scope.includes("comment"))) {
    return { color: "#6a9955", fontStyle: "italic" };
  }
  if (scopes.some((scope) => scope.includes("string"))) {
    return { color: "#c3e88d" };
  }
  if (scopes.some((scope) => scope.includes("constant.numeric"))) {
    return { color: "#f78c6c" };
  }
  if (scopes.some((scope) => scope.includes("keyword") || scope.includes("storage"))) {
    return { color: "#c792ea", fontWeight: 600 };
  }
  if (scopes.some((scope) => scope.includes("entity.name.tag"))) {
    return { color: "#ff5370", fontWeight: 600 };
  }
  if (scopes.some((scope) => scope.includes("meta.tag") || scope.includes("punctuation"))) {
    return { color: "#89ddff" };
  }
  if (
    scopes.some(
      (scope) =>
        scope.includes("entity.name") ||
        scope.includes("support.function") ||
        scope.includes("support.class"),
    )
  ) {
    return { color: "#82aaff" };
  }
  if (scopes.some((scope) => scope.includes("variable.parameter"))) {
    return { color: "#b2ccd6" };
  }
  if (scopes.some((scope) => scope.includes("variable") || scope.includes("support"))) {
    return { color: "#dcdcaa" };
  }
  return undefined;
}

function useSnippetHighlightState(enabled: boolean): SnippetHighlightState {
  const [state, setState] = useState<SnippetHighlightState>({ status: "loading" });
  useEffect(() => {
    if (!enabled) {
      return undefined;
    }
    let cancelled = false;
    setState({ status: "loading" });
    void loadSnippetHighlighter()
      .then((highlighter) => {
        if (!cancelled) {
          setState({ status: "ready", highlighter });
        }
      })
      .catch(() => {
        if (!cancelled) {
          setState({ status: "failed" });
        }
      });
    return () => {
      cancelled = true;
    };
  }, [enabled]);
  return enabled ? state : { status: "failed" };
}

function snippetLanguageForSourceItem(item: GraphSourceItem): SnippetLanguage {
  return item.kind === "include" ? "classic-asp" : "vbscript";
}

let snippetHighlighterPromise: Promise<SnippetHighlighter> | undefined;

function loadSnippetHighlighter(): Promise<SnippetHighlighter> {
  snippetHighlighterPromise ??= createSnippetHighlighter();
  return snippetHighlighterPromise;
}

async function createSnippetHighlighter(): Promise<SnippetHighlighter> {
  const onigLib = (async (): Promise<IOnigLib> => {
    await loadWASM(await fetch(onigWasmUrl));
    return {
      createOnigScanner,
      createOnigString,
    };
  })();
  const rawGrammars = new Map<string, IRawGrammar>([
    [
      "text.html.classic-asp",
      rawTextMateGrammar(classicAspGrammarJson, "classic-asp.tmLanguage.json"),
    ],
    [
      "classic-asp.tag-injection",
      rawTextMateGrammar(
        classicAspTagInjectionGrammarJson,
        "classic-asp-tag-injection.tmLanguage.json",
      ),
    ],
    ["source.vbscript", rawTextMateGrammar(vbscriptGrammarJson, "vbscript.tmLanguage.json")],
    ["text.html.basic", rawTextMateGrammar(minimalHtmlGrammar(), "html.json")],
    ["source.css", rawTextMateGrammar(minimalCssGrammar(), "css.json")],
    ["source.js", rawTextMateGrammar(minimalJavaScriptGrammar(), "javascript.json")],
  ]);
  const registry = new Registry({
    onigLib,
    loadGrammar: async (scopeName) => rawGrammars.get(scopeName) ?? null,
    getInjections: (scopeName) =>
      scopeName === "text.html.classic-asp" ? ["classic-asp.tag-injection"] : undefined,
  });
  const [classicAsp, vbscript] = await Promise.all([
    registry.loadGrammar("text.html.classic-asp"),
    registry.loadGrammar("source.vbscript"),
  ]);
  if (!classicAsp || !vbscript) {
    throw new Error("Failed to load graph snippet TextMate grammars.");
  }
  return { classicAsp, vbscript };
}

function rawTextMateGrammar(grammar: unknown, fileName: string): IRawGrammar {
  return parseRawGrammar(JSON.stringify(grammar), fileName);
}

function minimalHtmlGrammar(): unknown {
  return {
    scopeName: "text.html.basic",
    patterns: [
      { include: "#style" },
      { include: "#script" },
      { include: "#tag" },
      { match: "[^<]+" },
    ],
    repository: {
      tag: {
        begin: "<[A-Za-z][A-Za-z0-9:-]*\\b",
        end: ">",
        name: "meta.tag.html",
        patterns: [
          {
            begin: '"',
            end: '"',
            name: "string.quoted.double.html",
          },
          {
            begin: "'",
            end: "'",
            name: "string.quoted.single.html",
          },
        ],
      },
      style: {
        begin: "<style\\b[^>]*>",
        end: "</style>",
        contentName: "source.css",
        name: "meta.embedded.block.css.html",
        patterns: [{ include: "source.css" }],
      },
      script: {
        begin: "<script\\b[^>]*>",
        end: "</script>",
        contentName: "source.js",
        name: "meta.embedded.block.javascript.html",
        patterns: [{ include: "source.js" }],
      },
    },
  };
}

function minimalCssGrammar(): unknown {
  return {
    scopeName: "source.css",
    name: "source.css",
    patterns: [
      {
        begin: "/\\*",
        end: "\\*/",
        name: "comment.block.css",
        patterns: aspIslandPatterns(),
      },
      { match: "\\.[A-Za-z_][A-Za-z0-9_-]*", name: "entity.other.attribute-name.class.css" },
      { match: "-?[A-Za-z_][A-Za-z0-9_-]*(?=\\s*:)", name: "support.type.property-name.css" },
    ],
  };
}

function minimalJavaScriptGrammar(): unknown {
  return {
    scopeName: "source.js",
    name: "source.js",
    patterns: [
      {
        begin: "//",
        end: "$",
        name: "comment.line.double-slash.js",
        patterns: aspIslandPatterns(),
      },
      {
        begin: "/\\*",
        end: "\\*/",
        name: "comment.block.js",
        patterns: aspIslandPatterns(),
      },
      { match: "\\bconst\\b", name: "storage.modifier.js" },
      { match: "[A-Za-z_$][A-Za-z0-9_$]*", name: "variable.other.js" },
    ],
  };
}

function aspIslandPatterns(): unknown[] {
  return [
    { include: "text.html.classic-asp#asp-expression" },
    { include: "text.html.classic-asp#asp-directive" },
    { include: "text.html.classic-asp#asp-block" },
  ];
}

function positionOffsetInRangeText(
  text: string,
  range: AspGraphRange,
  position: AspGraphRange["start"],
): number | undefined {
  if (
    position.line < range.start.line ||
    position.line > range.end.line ||
    (position.line === range.start.line && position.character < range.start.character)
  ) {
    return undefined;
  }
  let offset = 0;
  for (let line = range.start.line; line < position.line; line += 1) {
    const newlineOffset = text.indexOf("\n", offset);
    if (newlineOffset === -1) {
      return undefined;
    }
    offset = newlineOffset + 1;
  }
  const lineStartCharacter = position.line === range.start.line ? range.start.character : 0;
  return clamp(offset + position.character - lineStartCharacter, 0, text.length);
}

function IncludeRelationList({ relations }: { relations: IncludeRelation[] }): React.ReactElement {
  return (
    <VirtualList
      className="grid gap-2"
      estimateSize={112}
      getKey={(relation) => relation.id}
      items={relations}
      maxHeight={360}
      overscan={8}
      renderItem={(relation) => (
        <article className="grid min-w-0 gap-2 overflow-hidden rounded-md border border-[#303a49] bg-[#11151c] p-2">
          <div className="min-w-0">
            <TooltipText
              className="overflow-hidden text-ellipsis whitespace-nowrap text-xs font-semibold text-[#d7dde8]"
              text={relation.title}
            />
            <TooltipText
              className="overflow-hidden text-ellipsis whitespace-nowrap text-[11px] text-[#8d98a8]"
              text={relation.detail}
            />
          </div>
          <div className="flex flex-wrap gap-2">
            <OpenLocationButton
              className="h-7 px-2"
              disabled={relation.exists === false || !relation.fileUri}
              label={relation.fileLabel}
              range={relation.fileRange}
              uri={relation.fileUri}
            />
            <OpenFlowchartButton
              className="h-7 px-2"
              disabled={relation.exists === false || !relation.fileUri}
              label={graphText("action.openFlowchart")}
              range={relation.fileRange}
              uri={relation.fileUri}
            />
            <OpenLocationButton
              className="h-7 px-2"
              disabled={!relation.directiveUri}
              label={relation.directiveLabel}
              range={relation.directiveRange}
              uri={relation.directiveUri}
            />
          </div>
        </article>
      )}
    />
  );
}

function EmptyInspectorText({ children }: { children: React.ReactNode }): React.ReactElement {
  return <p className="m-0 text-xs text-[#8d98a8]">{children}</p>;
}

function OpenLocationButton({
  className,
  disabled,
  label,
  range,
  uri,
}: {
  className?: string;
  disabled?: boolean;
  label: string;
  range?: AspGraphRange;
  uri?: string;
}): React.ReactElement {
  return (
    <button
      type="button"
      className={cn(
        "cursor-pointer rounded-md border border-[#405068] bg-[#c3e88d] text-xs text-[#11151c] disabled:cursor-not-allowed disabled:bg-[#202735] disabled:text-[#717b8c]",
        className,
      )}
      disabled={disabled || !uri}
      title={label}
      onClick={(event) => {
        event.stopPropagation();
        if (uri) {
          vscode.postMessage({ type: "openRange", uri, range });
        }
      }}
    >
      {label}
    </button>
  );
}

function OpenFlowchartButton({
  className,
  disabled,
  label,
  range,
  uri,
}: {
  className?: string;
  disabled?: boolean;
  label: string;
  range?: AspGraphRange;
  uri?: string;
}): React.ReactElement {
  return (
    <button
      type="button"
      className={cn(
        "cursor-pointer rounded-md border border-[#405068] bg-[#89ddff] text-xs text-[#11151c] disabled:cursor-not-allowed disabled:bg-[#202735] disabled:text-[#717b8c]",
        className,
      )}
      disabled={disabled || !uri}
      title={label}
      onClick={(event) => {
        event.stopPropagation();
        if (uri) {
          const message: OpenFlowchartMessage = { type: "openFlowchart", uri, range };
          vscode.postMessage(message);
        }
      }}
    >
      {label}
    </button>
  );
}

function NodeDetails({ node }: { node: GraphNode }): React.ReactElement {
  return (
    <dl className="mb-3.5 grid grid-cols-[82px_minmax(0,1fr)] gap-x-2.5 gap-y-2">
      <Detail
        hint={graphText("detail.nodeTypeHint")}
        label={graphText("detail.type")}
        value={nodeTypeLabel(node)}
      />
      <Detail
        hint={graphText("detail.nodeReferencesHint")}
        label={graphText("detail.references")}
        value={String(node.referenceCount)}
      />
      <Detail
        hint={graphText("detail.fileHint")}
        label={graphText("detail.file")}
        value={nodeFileLabel(node)}
      />
      <Detail
        hint={graphText("detail.memberOfHint")}
        label={graphText("detail.memberOf")}
        value={node.memberOf}
      />
      <Detail
        hint={graphText("detail.scopeHint")}
        label={graphText("detail.scope")}
        value={nodeScopeLabel(node)}
      />
      <Detail
        hint={graphText("detail.nodeStatusHint")}
        label={graphText("detail.status")}
        value={nodeStatusLabel(node)}
      />
    </dl>
  );
}

function nodeTypeLabel(node: GraphNode): string {
  if (node.kind === "missingInclude") {
    return graphText("label.missingInclude");
  }
  if (node.kind === "file") {
    return node.exists === false ? graphText("label.missingFile") : graphText("label.file");
  }
  if (node.kind === "vbUnresolved") {
    if (node.role === "member") {
      return graphText("label.unresolvedMember");
    }
    if (isCallableUnresolvedRole(node.role)) {
      return graphText("label.unresolvedFunction");
    }
    return graphText("label.unresolved");
  }
  if (node.kind === "vbMemberReference") {
    return graphText("label.member");
  }
  switch (node.declarationKind) {
    case "function":
      return graphText("label.function");
    case "sub":
      return graphText("label.sub");
    case "class":
      return graphText("label.class");
    case "method":
      if (node.procedureKind === "function") {
        return graphText("label.functionMethod");
      }
      if (node.procedureKind === "sub") {
        return graphText("label.subMethod");
      }
      return graphText("label.classMethod");
    case "property":
      return graphText("label.property");
    case "field":
      return graphText("label.field");
    case "parameter":
      return graphText("label.parameter");
    case "variable":
      if (isImplicitGlobalVariableNode(node)) {
        return graphText("label.implicitGlobalVariable");
      }
      return node.bindingScope === "local"
        ? graphText("label.localVariable")
        : graphText("label.globalVariable");
    case "constant":
      if (node.memberOf) {
        return graphText("label.classConstant");
      }
      return node.bindingScope === "local"
        ? graphText("label.localConstant")
        : graphText("label.globalConstant");
    case "object":
      return graphText("label.object");
    case "event":
      return graphText("label.event");
    default:
      return node.externalKind
        ? externalKindLabel(node.externalKind)
        : nodeCategoryLabels[node.category];
  }
}

function nodeFileLabel(node: GraphNode): string | undefined {
  return node.displayPath ?? node.fileName ?? pathForDisplay(node.uri);
}

function nodeScopeLabel(node: GraphNode): string | undefined {
  switch (node.bindingScope) {
    case "global":
      return graphText("label.global");
    case "local":
      return graphText("label.local");
    case "unknown":
      return graphText("label.unknown");
    default:
      return undefined;
  }
}

function nodeStatusLabel(node: GraphNode): string | undefined {
  if (node.kind === "missingInclude") {
    return graphText("label.missing");
  }
  if (node.kind === "file") {
    return node.isRoot ? graphText("label.root") : undefined;
  }
  if (node.origin === "builtin") {
    return graphText("label.builtin");
  }
  if (node.origin === "configured") {
    return graphText("label.configured");
  }
  return undefined;
}

function pathForDisplay(value: string | undefined): string | undefined {
  if (!value) {
    return undefined;
  }
  let normalized = value;
  if (normalized.startsWith("file://")) {
    try {
      const url = new URL(normalized);
      normalized =
        url.protocol === "file:" ? `${url.host ? `//${url.host}` : ""}${url.pathname}` : normalized;
    } catch {
      normalized = normalized.replace(/^file:\/\//, "");
    }
  }
  return safeDecodeURIComponent(normalized);
}

function safeDecodeURIComponent(value: string): string {
  try {
    return decodeURIComponent(value);
  } catch {
    return value;
  }
}

function externalKindLabel(value: NonNullable<GraphNode["externalKind"]>): string {
  switch (value) {
    case "function":
      return graphText("label.function");
    case "constant":
      return graphText("label.globalConstant");
    case "member":
      return graphText("label.member");
    case "object":
      return graphText("label.object");
    case "event":
      return graphText("label.event");
  }
}

function graphRoleLabel(role: string | undefined): string | undefined {
  switch (role) {
    case "member":
      return graphText("label.member");
    default:
      return role;
  }
}

function includeModeLabel(
  mode: NonNullable<AspGraphLink["include"]>["mode"] | undefined,
): string | undefined {
  switch (mode) {
    case "file":
      return graphText("label.file");
    case "virtual":
      return graphText("label.virtual");
    default:
      return undefined;
  }
}

function booleanLabel(value: boolean | undefined): string | undefined {
  return value === undefined ? undefined : graphText(value ? "label.yes" : "label.no");
}

function LinkDetails({
  link,
  nodesById,
  onSelectNode,
}: {
  link: GraphLink;
  nodesById: ReadonlyMap<string, GraphNode>;
  onSelectNode(node: GraphNode): void;
}): React.ReactElement {
  const typeLabel = linkInspectorTypeLabel(link);
  const sourceNode = nodesById.get(nodeIdForEndpoint(link.source));
  const targetNode = nodesById.get(nodeIdForEndpoint(link.target));
  const sourceLabel = endpointLabel(link.source, nodesById);
  const targetLabel = endpointLabel(link.target, nodesById);
  return (
    <div className="mb-3.5 grid gap-3">
      <section className="rounded-md border border-[#303a49] bg-[#11151c] p-3">
        <div className="flex min-w-0 items-center gap-3">
          <span
            className="h-3 w-3 shrink-0 rounded-full"
            style={{ backgroundColor: link.color }}
            aria-hidden="true"
          />
          <div className="min-w-0 flex-1">
            <div className="overflow-hidden text-ellipsis whitespace-nowrap text-xs font-semibold text-[#d7dde8]">
              {typeLabel}
            </div>
            <div
              className="text-[11px] leading-[1.35] text-[#8d98a8] [overflow-wrap:anywhere]"
              title={`${sourceLabel} -> ${targetLabel}`}
            >
              {sourceLabel} -&gt; {targetLabel}
            </div>
            <div className="mt-2 flex flex-wrap gap-2">
              <SelectEndpointButton
                label={graphText("action.selectSource")}
                node={sourceNode}
                onSelectNode={onSelectNode}
              />
              <SelectEndpointButton
                label={graphText("action.selectTarget")}
                node={targetNode}
                onSelectNode={onSelectNode}
              />
            </div>
          </div>
          <div className="grid min-w-[58px] shrink-0 justify-items-end rounded border border-[#405068] bg-[#151a22] px-2 py-1">
            <span className="text-sm leading-none font-semibold text-[#f4f7fb]">{link.count}</span>
            <span className="inline-flex items-center gap-1 text-[10px] leading-[1.2] text-[#9aa7b8]">
              {graphText("detail.count")}
              <DetailHint hint={graphText("detail.countHint")} label={graphText("detail.count")} />
            </span>
          </div>
        </div>
      </section>
      <dl className="grid grid-cols-[86px_minmax(0,1fr)] gap-x-2.5 gap-y-2">
        <Detail
          hint={graphText("detail.linkTypeHint")}
          label={graphText("detail.type")}
          value={typeLabel}
        />
        <Detail
          hint={graphText("detail.sourceHint")}
          label={graphText("detail.source")}
          value={sourceLabel}
        />
        <Detail
          hint={graphText("detail.targetHint")}
          label={graphText("detail.target")}
          value={targetLabel}
        />
        <Detail
          hint={graphText("detail.roleHint")}
          label={graphText("detail.role")}
          value={graphRoleLabel(link.role)}
        />
        <Detail
          hint={graphText("detail.labelHint")}
          label={graphText("detail.label")}
          value={link.label !== typeLabel ? link.label : undefined}
        />
        <Detail
          hint={graphText("detail.includeHint")}
          label={graphText("detail.include")}
          value={link.include?.path}
        />
        <Detail
          hint={graphText("detail.modeHint")}
          label={graphText("detail.mode")}
          value={includeModeLabel(link.include?.mode)}
        />
        <Detail
          hint={graphText("detail.existsHint")}
          label={graphText("detail.exists")}
          value={link.include ? booleanLabel(link.include.exists) : undefined}
        />
        <Detail
          hint={graphText("detail.actualPathHint")}
          label={graphText("detail.actualPath")}
          value={link.include?.actualPath}
        />
      </dl>
    </div>
  );
}

function SelectEndpointButton({
  label,
  node,
  onSelectNode,
}: {
  label: string;
  node: GraphNode | undefined;
  onSelectNode(node: GraphNode): void;
}): React.ReactElement {
  return (
    <button
      type="button"
      className="h-7 cursor-pointer rounded-md border border-[#405068] bg-[#202735] px-2 text-[11px] text-[#d7dde8] hover:border-[#4b5a70] disabled:cursor-not-allowed disabled:border-[#303a49] disabled:text-[#717b8c]"
      disabled={!node}
      title={label}
      onClick={() => {
        if (node) {
          onSelectNode(node);
        }
      }}
    >
      {label}
    </button>
  );
}

function Detail({
  hint,
  label,
  value,
}: {
  hint?: string;
  label: string;
  value: string | undefined;
}): React.ReactElement | null {
  if (!value) {
    return null;
  }
  return (
    <>
      <dt className="flex min-w-0 items-center gap-1 text-[11px] text-[#8d98a8]">
        <span className="min-w-0 overflow-hidden text-ellipsis whitespace-nowrap">{label}</span>
        {hint ? <DetailHint hint={hint} label={label} /> : null}
      </dt>
      <dd className="m-0 min-w-0">
        <TooltipText
          className="block overflow-hidden text-ellipsis whitespace-nowrap text-xs text-[#d7dde8]"
          text={value}
        />
      </dd>
    </>
  );
}

interface TooltipPosition {
  left: number;
  top: number;
  maxWidth: number;
}

function TooltipText({
  className,
  text,
  tooltip,
}: {
  className?: string;
  text: string;
  tooltip?: string;
}): React.ReactElement {
  const triggerRef = useRef<HTMLSpanElement>(null);
  const tooltipRef = useRef<HTMLSpanElement>(null);
  const [visible, setVisible] = useState(false);
  const [position, setPosition] = useState<TooltipPosition>();
  const tooltipText = tooltip ?? text;
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
    <>
      <span
        ref={triggerRef}
        tabIndex={0}
        aria-label={tooltipText}
        className={className}
        onBlur={hideTooltip}
        onFocus={showTooltip}
        onPointerEnter={showTooltip}
        onPointerLeave={hideTooltip}
      >
        {text}
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
          {tooltipText}
        </span>
      ) : null}
    </>
  );
}

function DetailHint({ hint, label }: { hint: string; label: string }): React.ReactElement {
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

function useSourceRanges(items: readonly GraphSourceItem[]): GraphSourceState {
  const cacheRef = useRef(new Map<string, AspGraphSourceRangeResponseItem>());
  const loadingIdsRef = useRef(new Set<string>());
  const [state, setState] = useState<GraphSourceState>(() => ({
    loading: false,
    byId: new Map(),
  }));

  useEffect(() => {
    if (items.length === 0) {
      setState({ loading: false, byId: new Map(cacheRef.current) });
      return undefined;
    }
    const requestedItems = items.filter(
      (item) => !cacheRef.current.has(item.id) && !loadingIdsRef.current.has(item.id),
    );
    if (requestedItems.length === 0) {
      setState({
        loading: items.some((item) => loadingIdsRef.current.has(item.id)),
        byId: new Map(cacheRef.current),
      });
      return undefined;
    }
    const requestId = `source:${Date.now()}:${Math.random().toString(36).slice(2)}`;
    for (const item of requestedItems) {
      loadingIdsRef.current.add(item.id);
    }
    const handleMessage = (event: MessageEvent<unknown>) => {
      if (!isSourceRangesMessage(event.data) || event.data.requestId !== requestId) {
        return;
      }
      for (const item of event.data.items) {
        cacheRef.current.set(item.id, item);
        loadingIdsRef.current.delete(item.id);
      }
      setState({
        loading: items.some((item) => loadingIdsRef.current.has(item.id)),
        byId: new Map(cacheRef.current),
      });
    };
    window.addEventListener("message", handleMessage);
    setState({ loading: true, byId: new Map(cacheRef.current) });
    vscode.postMessage({
      type: "readSourceRanges",
      requestId,
      items: requestedItems.map(sourceRangeRequestItem),
    });
    return () => {
      window.removeEventListener("message", handleMessage);
      for (const item of requestedItems) {
        loadingIdsRef.current.delete(item.id);
      }
    };
  }, [items]);

  return state;
}

function isSourceRangesMessage(value: unknown): value is SourceRangesMessage {
  if (!value || typeof value !== "object") {
    return false;
  }
  const message = value as Partial<SourceRangesMessage>;
  return (
    message.type === "sourceRanges" &&
    typeof message.requestId === "string" &&
    Array.isArray(message.items)
  );
}

function isGraphUpdatedMessage(value: unknown): value is GraphUpdatedMessage {
  if (!value || typeof value !== "object") {
    return false;
  }
  const message = value as Partial<GraphUpdatedMessage>;
  return (
    message.type === "graphUpdated" &&
    (message.payload === undefined || typeof message.payload === "object") &&
    (message.error === undefined || typeof message.error === "string")
  );
}

function sourceRangeRequestItem(item: GraphSourceItem): AspGraphSourceRangeRequestItem {
  return {
    id: item.id,
    uri: item.uri,
    range: item.range,
    highlightRange: item.highlightRange,
    kind: item.kind,
  };
}

function sourceItemsForNode(
  node: GraphNode,
  graphData: GraphData,
): { declarationItems: GraphSourceItem[]; usageItems: GraphSourceItem[] } {
  const declarationItems =
    node.uri && node.range
      ? [
          {
            id: `declaration:${node.id}`,
            uri: node.uri,
            displayPath: nodeFileLabel(node),
            range: node.sourceRange ?? node.range,
            highlightRange: node.range,
            kind: "declaration" as const,
            title: graphText("detail.declaration"),
            detail: nodeTypeLabel(node),
          },
        ]
      : [];
  const usageItems: GraphSourceItem[] = [];
  for (const link of graphData.links) {
    if (
      nodeIdForEndpoint(link.target) !== node.id ||
      (link.kind !== "references" &&
        link.kind !== "assignments" &&
        link.kind !== "calls" &&
        link.kind !== "unresolvedReference")
    ) {
      continue;
    }
    link.ranges.forEach((location, index) => {
      usageItems.push({
        id: `usage:${link.id}:${index}:${location.uri}:${location.range.start.line}:${location.range.start.character}`,
        uri: location.uri,
        displayPath: location.displayPath ?? pathForDisplay(location.uri),
        range: location.range,
        highlightRange: location.range,
        kind: link.kind === "calls" ? "call" : "reference",
        title: sourceUsageTitle(link),
      });
    });
  }
  usageItems.sort(compareSourceItems);
  return { declarationItems, usageItems };
}

function sourceItemsForLink(link: GraphLink): GraphSourceItem[] {
  return link.ranges
    .map((location, index) => ({
      id: `link:${link.id}:${index}:${location.uri}:${location.range.start.line}:${location.range.start.character}`,
      uri: location.uri,
      displayPath: location.displayPath ?? pathForDisplay(location.uri),
      range: location.range,
      highlightRange: location.range,
      kind: sourceKindForLink(link),
      title: linkOccurrenceTitle(link),
      detail: linkOccurrenceDetail(link, location),
    }))
    .sort(compareSourceItems);
}

function nodeLinksFor(
  node: GraphNode,
  links: GraphLink[],
): { incoming: GraphLink[]; outgoing: GraphLink[] } {
  const incoming: GraphLink[] = [];
  const outgoing: GraphLink[] = [];
  for (const link of links) {
    if (nodeIdForEndpoint(link.source) === node.id) {
      outgoing.push(link);
    }
    if (nodeIdForEndpoint(link.target) === node.id) {
      incoming.push(link);
    }
  }
  return { incoming, outgoing };
}

function nodeLinkDetail(link: GraphLink): string | undefined {
  if (link.kind === "include") {
    return includeRelationDetail(link);
  }
  const typeLabel = linkInspectorTypeLabel(link);
  const parts = detailParts(
    graphRoleLabel(link.role),
    link.label !== typeLabel ? link.label : undefined,
    link.ranges.length > 0 ? occurrenceCountLabel(link.ranges.length) : undefined,
  );
  return parts.length > 0 ? parts.join(" · ") : undefined;
}

function sourceKindForLink(link: GraphLink): AspGraphSourceRangeRequestItem["kind"] {
  switch (link.kind) {
    case "include":
      return "include";
    case "declares":
      return "declaration";
    case "calls":
      return "call";
    case "assignments":
    case "references":
    case "unresolvedReference":
      return "reference";
  }
}

function linkOccurrenceTitle(link: GraphLink): string {
  if (link.kind === "include") {
    return graphText("occurrence.includeDirective");
  }
  if (link.kind === "declares") {
    return graphText("detail.declaration");
  }
  return sourceUsageTitle(link);
}

function linkOccurrenceDetail(
  link: GraphLink,
  location: { uri: string; range: AspGraphRange },
): string | undefined {
  const detail = detailParts(
    rangeLineLabel(location.range),
    link.kind === "include" ? link.include?.path : undefined,
    graphRoleLabel(link.role),
  );
  return detail.length > 0 ? detail.join(" · ") : undefined;
}

function sourceUsageTitle(link: GraphLink): string {
  const label =
    link.kind === "unresolvedReference"
      ? graphText("label.unresolved")
      : linkMeanings[link.kind].label;
  const roleLabel = graphRoleLabel(link.role);
  return roleLabel ? `${label}: ${roleLabel}` : label;
}

function occurrenceCountLabel(count: number): string {
  return graphText(count === 1 ? "occurrence.one" : "occurrence.other", { count });
}

function linkInspectorTypeLabel(link: GraphLink): string {
  const label = linkMeanings[link.kind].label;
  if (graphLinkFilterCategory(link) !== "member") {
    return label;
  }
  return graphText("link.memberType", {
    label: graphLocale === "en" ? label.toLowerCase() : label,
  });
}

function compareSourceItems(left: GraphSourceItem, right: GraphSourceItem): number {
  return (
    left.uri.localeCompare(right.uri) ||
    left.range.start.line - right.range.start.line ||
    left.range.start.character - right.range.start.character ||
    left.title.localeCompare(right.title)
  );
}

function groupedSourceItems(
  items: GraphSourceItem[],
): Array<{ uri: string; items: GraphSourceItem[] }> {
  const groups = new Map<string, GraphSourceItem[]>();
  for (const item of items) {
    const group = groups.get(item.uri);
    if (group) {
      group.push(item);
    } else {
      groups.set(item.uri, [item]);
    }
  }
  return [...groups.entries()].map(([uri, groupItems]) => ({ uri, items: groupItems }));
}

function sourceGroupTitle(uri: string, items: GraphSourceItem[]): string {
  return items[0]?.displayPath ?? pathForDisplay(uri) ?? uri;
}

function includeRelationsForFileNode(
  node: GraphNode,
  graphData: GraphData,
): { incoming: IncludeRelation[]; outgoing: IncludeRelation[] } {
  const nodesById = graphNodeMap(graphData.nodes);
  const incoming: IncludeRelation[] = [];
  const outgoing: IncludeRelation[] = [];
  for (const link of graphData.links) {
    if (link.kind !== "include") {
      continue;
    }
    const sourceId = nodeIdForEndpoint(link.source);
    const targetId = nodeIdForEndpoint(link.target);
    if (sourceId === node.id) {
      outgoing.push(includeTargetRelation(link, nodesById));
    }
    if (targetId === node.id) {
      incoming.push(includeSourceRelation(link, nodesById));
    }
  }
  return { incoming, outgoing };
}

function includeTargetRelation(
  link: GraphLink,
  nodesById: ReadonlyMap<string, GraphNode>,
): IncludeRelation {
  const target = nodesById.get(nodeIdForEndpoint(link.target));
  const directive = link.ranges[0];
  const targetUri = target?.uri ?? link.include?.resolvedUri;
  return {
    id: `include-target:${link.id}`,
    title: target?.label ?? link.include?.path ?? link.label,
    detail: includeRelationDetail(link),
    fileUri: targetUri,
    fileLabel: graphText("action.openFile"),
    directiveUri: directive?.uri,
    directiveRange: directive?.range,
    directiveLabel: graphText("action.openDirective"),
    exists: link.include?.exists,
  };
}

function includeSourceRelation(
  link: GraphLink,
  nodesById: ReadonlyMap<string, GraphNode>,
): IncludeRelation {
  const source = nodesById.get(nodeIdForEndpoint(link.source));
  const directive = link.ranges[0];
  return {
    id: `include-source:${link.id}`,
    title: source?.label ?? directive?.displayPath ?? pathForDisplay(directive?.uri) ?? link.label,
    detail: includeRelationDetail(link),
    fileUri: source?.uri ?? directive?.uri,
    fileLabel: graphText("action.openFile"),
    directiveUri: directive?.uri,
    directiveRange: directive?.range,
    directiveLabel: graphText("action.openDirective"),
    exists: true,
  };
}

function includeRelationDetail(link: GraphLink): string {
  const parts = [
    includeModeLabel(link.include?.mode),
    link.include?.path,
    link.include?.exists === false ? graphText("label.missing") : undefined,
  ].filter(Boolean);
  return parts.length > 0 ? parts.join(" · ") : link.label;
}

function nodeLabel(node: GraphNode): string {
  if (node.kind === "vbDeclaration" && node.declarationKind) {
    return `${nodeTypeLabel(node)}: ${node.label}`;
  }
  if (node.kind === "vbUnresolved" && node.role) {
    return `${nodeTypeLabel(node)}: ${node.label}`;
  }
  if (node.kind === "vbMemberReference") {
    return `${graphText("label.member")}: ${node.fullPath ?? node.label}`;
  }
  return node.label;
}

function isCallableUnresolvedRole(role: string | undefined): boolean {
  return role === "function" || role === "procedure" || role === "unknown";
}

function linkLabel(link: GraphLink): string {
  const meaning = linkMeanings[link.kind];
  const count = occurrenceCountLabel(link.count);
  return `${meaning.label}: ${link.label} (${count})\n${meaning.description}`;
}

createRoot(document.getElementById("root") ?? document.body).render(<App />);
