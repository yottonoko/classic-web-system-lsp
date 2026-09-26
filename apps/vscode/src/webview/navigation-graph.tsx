import {
  createSignal,
  createMemo,
  createEffect,
  onSettled,
  untrack,
  type Accessor,
} from "solid-js";
import { render, type JSX } from "@solidjs/web";
import { webviewStyle, type WebviewStyle } from "./webview-dom-types";
import { createVirtualizer } from "./virtual-list";
import {
  navigationTreeEntries,
  visibleNavigationTreeEntries,
  navigationSourcePath,
} from "./navigation-graph-tree";
import { navigationComponents } from "./navigation-graph-components";
import { navigationConnections, navigationLoopEdgeIds } from "./navigation-graph-connections";
import {
  NavigationGraphCanvas,
  type NavigationViewportController,
} from "./navigation-graph-canvas";
import type {
  AspNavigationEvidence,
  AspNavigationEdge,
  AspNavigationEdgeKind,
  AspNavigationGraphPayload,
  AspNavigationNode,
} from "../protocol-types";
import styles from "./navigation-graph.css?inline";
import { WebviewErrorBoundary } from "./webview-error-boundary";
import {
  layoutNavigationGraphWithElk,
  type NavigationFlowEdge,
  type NavigationFlowLayout,
  type NavigationFlowNode,
} from "./navigation-graph-layout";
declare const acquireVsCodeApi: () => {
  postMessage(message: unknown): void;
};
declare global {
  interface Window {
    __ASP_LSP_NAVIGATION_GRAPH__?: NavigationGraphWebviewPayload;
  }
}
interface NavigationGraphWebviewPayload extends AspNavigationGraphPayload {
  locale?: "en" | "ja";
  webviewSettings?: {
    theme?: "auto" | "light" | "dark";
  };
}
type Selection =
  | {
      kind: "node";
      id: string;
    }
  | {
      kind: "edge";
      id: string;
    }
  | undefined;
type HoverTarget = Selection;
type NavigationTextKey =
  | "tree"
  | "graph"
  | "collapseAll"
  | "expandAll"
  | "cycle"
  | "shared"
  | "unresolved"
  | "unresolvedHelp"
  | "noResults"
  | "search"
  | "searchPlaceholder"
  | "allConfidence"
  | "certain"
  | "probable"
  | "possible"
  | "unknown"
  | "allTransitions"
  | "allMethods"
  | "fit"
  | "pagesTransitions"
  | "entry"
  | "missing"
  | "from"
  | "to"
  | "confidence"
  | "method"
  | "targetFrame"
  | "declaredIn"
  | "include"
  | "includeDerived"
  | "count"
  | "parameters"
  | "noParameterFlow"
  | "evidence"
  | "openSource"
  | "kind"
  | "uri"
  | "exists"
  | "known"
  | "navigationGraph"
  | "scope"
  | "documents"
  | "nodes"
  | "edges"
  | "selectPageOrTransition"
  | "transition"
  | "layouting"
  | "incoming"
  | "outgoing"
  | "bothDirections"
  | "uncertainLine"
  | "fitConnections"
  | "clearSelection"
  | "showLabels"
  | "noConnections"
  | "allComponents"
  | "component"
  | "isolatedPages"
  | "legendHelp"
  | "legendSolid"
  | "legendDashed"
  | "legendNodes"
  | "legendSelection";
const navigationMessages: Record<"en" | "ja", Record<NavigationTextKey, string>> = {
  en: {
    legendHelp: "How to read the graph",
    legendSolid:
      "Solid line: a destination resolved with certainty by static analysis. The arrow points to the destination page.",
    legendDashed:
      "Dashed line: a probable, possible, or unresolved transition. Conditions or runtime values may affect the destination; select the line to inspect its source and confidence.",
    legendNodes:
      "Page colors: blue = page, green = included fragment with a transition, amber = external URL, purple = unresolved destination. Includes alone are hidden.",
    legendSelection:
      "After selecting a page: blue lines enter it, amber lines leave it, and purple lines return to the same page. Unrelated connections fade. Loop labels identify returns to the same or an earlier page. Labels appear for highlighted connections; the checkbox shows all labels.",
    tree: "Tree",
    graph: "Graph",
    collapseAll: "Collapse all",
    expandAll: "Expand all",
    cycle: "Loop",
    shared: "Shared destination",
    unresolved: "Unresolved destination",
    unresolvedHelp:
      "Static analysis could not determine this destination. Inspect the expression at the source location below.",
    noResults: "No matching transitions.",
    search: "Search",
    searchPlaceholder: "Search pages or transitions",
    allConfidence: "All confidence",
    certain: "Certain",
    probable: "Probable",
    possible: "Possible",
    unknown: "Unknown",
    allTransitions: "All transitions",
    allMethods: "All methods",
    fit: "Fit",
    pagesTransitions: "{pages} pages / {transitions} transitions",
    entry: "entry",
    missing: "missing",
    from: "From",
    to: "To",
    confidence: "Confidence",
    method: "Method",
    targetFrame: "Target frame",
    declaredIn: "Declared in",
    include: "Include",
    includeDerived: "declared in included fragment",
    count: "Count",
    parameters: "Parameters",
    noParameterFlow: "No parameter flow.",
    evidence: "Evidence",
    openSource: "Open source",
    kind: "Kind",
    uri: "URI",
    exists: "Exists",
    known: "Known",
    navigationGraph: "Navigation Graph",
    scope: "Scope",
    documents: "Documents",
    nodes: "Nodes",
    edges: "Edges",
    selectPageOrTransition: "Select a page or transition.",
    transition: "transition",
    layouting: "Laying out graph...",
    incoming: "Incoming",
    outgoing: "Outgoing",
    bothDirections: "Self-return",
    uncertainLine: "Uncertain (dashed)",
    fitConnections: "Fit connections",
    clearSelection: "Clear selection",
    showLabels: "Transition labels",
    noConnections: "No transitions.",
    allComponents: "All groups",
    component: "Page group",
    isolatedPages: "Pages without transitions",
  },
  ja: {
    legendHelp: "表示の見方",
    legendSolid: "実線：静的解析で遷移先を確定できた遷移。矢印は移動先のページを指します。",
    legendDashed:
      "点線：推定・候補・未解決の遷移。条件や実行時の値によって遷移先が変わる可能性があります。線を選ぶと確度と該当コードを確認できます。",
    legendNodes:
      "ページの色：青＝ページ、緑＝遷移に関わる include 断片、黄＝外部 URL、紫＝未解決の遷移先。include だけの項目は表示しません。",
    legendSelection:
      "ページ選択時の線：青＝入る遷移、黄＝出る遷移、紫＝同じページへ戻るループ。無関係な線は薄くなります。「ループ」は同じページや既に通ったページへ戻る線です。強調した線にラベルを表示し、チェックを入れると全ラベルを表示します。",
    tree: "ツリー",
    graph: "グラフ",
    collapseAll: "すべて折りたたむ",
    expandAll: "すべて展開",
    cycle: "ループ",
    shared: "共通の遷移先",
    unresolved: "未解決の遷移先",
    unresolvedHelp:
      "静的解析では遷移先を確定できません。以下の発生箇所で、遷移先を指定する式を確認できます。",
    noResults: "条件に一致する遷移はありません。",
    search: "検索",
    searchPlaceholder: "ページまたは遷移を検索",
    allConfidence: "確度: すべて",
    certain: "確実",
    probable: "有力",
    possible: "可能性あり",
    unknown: "不明",
    allTransitions: "遷移: すべて",
    allMethods: "メソッド: すべて",
    fit: "全体表示",
    pagesTransitions: "{pages} ページ / {transitions} 遷移",
    entry: "入口",
    missing: "未検出",
    from: "遷移元",
    to: "遷移先",
    confidence: "確度",
    method: "メソッド",
    targetFrame: "対象フレーム",
    declaredIn: "宣言元",
    include: "Include",
    includeDerived: "include fragment で宣言",
    count: "件数",
    parameters: "パラメーター",
    noParameterFlow: "パラメーターの流れはありません。",
    evidence: "根拠",
    openSource: "ソースを開く",
    kind: "種類",
    uri: "URI",
    exists: "存在",
    known: "既知",
    navigationGraph: "画面遷移グラフ",
    scope: "範囲",
    documents: "ドキュメント",
    nodes: "ノード",
    edges: "エッジ",
    selectPageOrTransition: "ページまたは遷移を選択してください。",
    transition: "遷移",
    layouting: "グラフを配置中…",
    incoming: "入る遷移",
    outgoing: "出る遷移",
    bothDirections: "ループ（同じページ）",
    uncertainLine: "未確定（破線）",
    fitConnections: "接続を拡大",
    clearSelection: "選択解除",
    showLabels: "遷移ラベル",
    noConnections: "遷移はありません。",
    allComponents: "すべてのまとまり",
    component: "ページのまとまり",
    isolatedPages: "つながりのないページ",
  },
};
function navigationText(
  locale: "en" | "ja",
  key: NavigationTextKey,
  params: Record<string, string | number> = {},
): string {
  let value = navigationMessages[locale][key] ?? navigationMessages.en[key];
  for (const [name, parameter] of Object.entries(params)) {
    value = value.replaceAll(`{${name}}`, String(parameter));
  }
  return value;
}
const vscode = acquireVsCodeApi();
const edgeKinds: AspNavigationEdgeKind[] = [
  "serverRedirect",
  "htmlAnchor",
  "htmlFrame",
  "htmlForm",
  "metaRefresh",
  "javascriptLocation",
  "javascriptHistory",
  "javascriptFormSubmit",
];
const emptyLayout: NavigationFlowLayout = { width: 720, height: 520, nodes: [], edges: [] };
const hoverClearDelayMs = 80;
const navigationFitViewPadding = 0.05;
function createResolvedNavigationTheme(
  setting: Accessor<"auto" | "light" | "dark" | undefined>,
): Accessor<"light" | "dark"> {
  const [theme, setTheme] = createSignal<"light" | "dark">(
    untrack(() => detectedNavigationTheme()),
  );
  createEffect(setting, (setting) => {
    if (setting === "light" || setting === "dark") {
      setTheme(setting);
      return undefined;
    }
    const observer = new MutationObserver(() => setTheme(detectedNavigationTheme()));
    const options: MutationObserverInit = { attributes: true, attributeFilter: ["class", "style"] };
    observer.observe(document.body, options);
    observer.observe(document.documentElement, options);
    setTheme(detectedNavigationTheme());
    return () => observer.disconnect();
  });
  return createMemo(() => {
    const value = setting();
    return value === "light" || value === "dark" ? value : theme();
  });
}
function detectedNavigationTheme(): "light" | "dark" {
  const classList = document.body.classList;
  return classList.contains("vscode-light") || classList.contains("vscode-high-contrast-light")
    ? "light"
    : "dark";
}
function NavigationGraphApp(): JSX.Element {
  const [payload, setPayload] = createSignal<NavigationGraphWebviewPayload>(
    window.__ASP_LSP_NAVIGATION_GRAPH__ ?? emptyPayload(),
  );
  const theme = createResolvedNavigationTheme(() => payload().webviewSettings?.theme);
  onSettled(() => {
    const listener = (event: MessageEvent) => {
      const message = event.data as {
        type?: string;
        payload?: NavigationGraphWebviewPayload;
      };
      if (message.type === "navigationGraphPayload" && message.payload) {
        setPayload(message.payload);
      }
    };
    window.addEventListener("message", listener);
    return () => window.removeEventListener("message", listener);
  });
  return (
    <div
      class="navigation-shell"
      data-asp-lsp-theme={theme()}
      data-asp-lsp-theme-setting={payload().webviewSettings?.theme ?? "auto"}
    >
      <style>{styles}</style>

      <NavigationGraphSurface payload={payload()} />
    </div>
  );
}
function NavigationGraphSurface(props: { payload: NavigationGraphWebviewPayload }): JSX.Element {
  const canvasController: {
    current?: NavigationViewportController;
  } = {};
  const reducedMotion = createReducedMotion();
  const locale = createMemo<"en" | "ja">(() => (props.payload.locale === "ja" ? "ja" : "en"));
  const text = (key: NavigationTextKey, params: Record<string, string | number> = {}) =>
    navigationText(locale(), key, params);
  const [viewMode, setViewMode] = createSignal<"tree" | "graph">("tree");
  const [search, setSearch] = createSignal("");
  const [confidence, setConfidence] = createSignal("all");
  const [edgeKind, setEdgeKind] = createSignal("all");
  const [method, setMethod] = createSignal("all");
  const [selection, setSelection] = createSignal<Selection>();
  const [showLabels, setShowLabels] = createSignal(false);
  const [hovered, setHovered] = createSignal<HoverTarget>();
  const [layout, setLayout] = createSignal<NavigationFlowLayout>(emptyLayout);
  const [isLayouting, setIsLayouting] = createSignal(false);
  const layoutRequest = { current: 0 };
  const hoverClearTimer = { current: undefined } as {
    current: (number | undefined) | undefined;
  };
  const [componentId, setComponentId] = createSignal("all");
  const searchedPayload = createMemo(() =>
    filterPayload(props.payload, {
      search: search(),
      confidence: confidence(),
      edgeKind: edgeKind(),
      method: method(),
    }),
  );
  const components = createMemo(() =>
    navigationComponents(searchedPayload().nodes, searchedPayload().edges),
  );
  const component = createMemo(() =>
    viewMode() === "graph" ? components().find((group) => group.id === componentId()) : undefined,
  );
  const filteredPayload = createMemo(() =>
    component()
      ? { ...searchedPayload(), nodes: component()!.nodes, edges: component()!.edges }
      : searchedPayload(),
  );
  const methods = createMemo(() => distinctMethods(props.payload.edges));
  createEffect(
    () => [filteredPayload(), viewMode()],
    () => {
      const requestId = layoutRequest.current + 1;
      layoutRequest.current = requestId;
      if (viewMode() !== "graph") {
        setIsLayouting(false);
        return;
      }
      setIsLayouting(true);
      void layoutNavigationGraphWithElk(filteredPayload())
        .then((nextLayout) => {
          if (layoutRequest.current === requestId) {
            setLayout(nextLayout);
          }
        })
        .catch(() => {
          if (layoutRequest.current === requestId) {
            setLayout(emptyLayout);
          }
        })
        .finally(() => {
          if (layoutRequest.current === requestId) {
            setIsLayouting(false);
          }
        });
      return () => {
        layoutRequest.current++;
      };
    },
  );
  const fitToView = () => {
    window.requestAnimationFrame(() => {
      canvasController.current?.fitView({
        padding: navigationFitViewPadding,
        duration: reducedMotion() ? 80 : 720,
        includeHiddenNodes: false,
      });
    });
  };
  createEffect(
    () => [fitToView, layout().width, layout().height],
    () => {
      fitToView();
    },
  );
  createEffect(
    () => [props.payload],
    () => {
      setSelection(undefined);
    },
  );
  createEffect(
    () => [props.payload, search(), confidence(), edgeKind(), method()],
    () => {
      if (hoverClearTimer.current !== undefined) {
        window.clearTimeout(hoverClearTimer.current);
        hoverClearTimer.current = undefined;
      }
      setHovered(undefined);
    },
  );
  createEffect(
    () => [hovered(), layout().edges, layout().nodes],
    () => {
      if (!hovered()) {
        return;
      }
      const stillVisible =
        hovered()?.kind === "node"
          ? layout().nodes.some((node) => node.id === hovered()?.id)
          : layout().edges.some((edge) => edge.id === hovered()?.id);
      if (!stillVisible) {
        setHovered(undefined);
      }
    },
  );
  onSettled(() => () => {
    if (hoverClearTimer.current !== undefined) {
      window.clearTimeout(hoverClearTimer.current);
    }
  });
  const setHoveredTarget = (target: Exclude<HoverTarget, undefined>) => {
    if (hoverClearTimer.current !== undefined) {
      window.clearTimeout(hoverClearTimer.current);
      hoverClearTimer.current = undefined;
    }
    setHovered((current) => (sameHoverTarget(current, target) ? current : target));
  };
  const clearHoveredTarget = () => {
    if (hoverClearTimer.current !== undefined) {
      window.clearTimeout(hoverClearTimer.current);
    }
    hoverClearTimer.current = window.setTimeout(() => {
      hoverClearTimer.current = undefined;
      setHovered(undefined);
    }, hoverClearDelayMs);
  };
  const searchHits = createMemo(() => searchHitSets(filteredPayload(), search()));
  const related = createMemo(() =>
    navigationConnections(filteredPayload().nodes, filteredPayload().edges, selection(), hovered()),
  );
  const fitConnections = () => {
    void canvasController.current?.fitView({
      nodes: [...related().nodes].map((id) => ({ id })),
      padding: 0.12,
      maxZoom: 1.15,
      duration: reducedMotion() ? 0 : 400,
    });
  };
  const unknownEvidence = createMemo(() => {
    const result = new Map<string, AspNavigationEvidence>();
    for (const edge of filteredPayload().edges) {
      if (edge.evidence[0] && !result.has(edge.target)) result.set(edge.target, edge.evidence[0]);
    }
    return result;
  });
  const flowNodes = createMemo(() =>
    layout().nodes.map((node) => {
      const selected = selection()?.kind === "node" && selection()?.id === node.id;
      const evidence = unknownEvidence().get(node.id);
      const searchHit = searchHits().nodes.has(node.id);
      const dimmed = !!related().active && !related().nodes.has(node.id);
      return {
        ...node,
        selected,
        className: classNames(
          "navigation-flow-node",
          `navigation-node--${node.data.node.kind}`,
          selected && "navigation-node--selected",
          searchHit && "navigation-node--search-hit",
          dimmed && "navigation-node--dimmed",
          related().nodes.has(node.id) && "navigation-node--connected",
        ),
        data: {
          ...node.data,
          node:
            node.data.node.kind === "unknown" && evidence
              ? {
                  ...node.data.node,
                  fileName: sourceLocation(evidence),
                  label: navigationText(locale(), "unresolved"),
                }
              : node.data.node,
          locale: locale(),
          selected,
          searchHit,
          dimmed,
          revealDelayMs: revealDelay(node.data.layer, node.data.revealIndex, reducedMotion()),
          onSelect: () => setSelection({ kind: "node", id: node.id }),
          onHover: () => setHoveredTarget({ kind: "node", id: node.id }),
          onHoverEnd: clearHoveredTarget,
        },
      };
    }),
  );
  const loopEdges = createMemo(() =>
    navigationLoopEdgeIds(filteredPayload().nodes, filteredPayload().edges),
  );
  const flowEdges = createMemo(() =>
    layout().edges.map((edge) => {
      const edgeData = edge.data as NonNullable<NavigationFlowEdge["data"]>;
      const selected = selection()?.kind === "edge" && selection()?.id === edge.id;
      const searchHit = searchHits().edges.has(edge.id);
      const dimmed = !!related().active && !related().edges.has(edge.id);
      const direction = related().edges.get(edge.id);
      const color = direction ? `var(--navigation-${direction})` : "var(--navigation-edge)";
      const uncertain = edgeData.confidence !== "certain";
      return {
        ...edge,
        selected,
        zIndex: direction ? 1 : 0,
        className: classNames(
          "navigation-flow-edge",
          `navigation-edge--${edgeData.confidence}`,
          uncertain && "navigation-edge--uncertain",
          selected && "navigation-edge--selected",
          searchHit && "navigation-edge--search-hit",
          dimmed && "navigation-edge--dimmed",
          direction && "navigation-edge--connected",
        ),
        data: {
          ...edgeData,
          label: `${loopEdges().has(edge.id) ? navigationText(locale(), "cycle") + " · " : ""}${edgeData.method ? edgeData.method + " · " : ""}${edgeKindLabel(edgeData.edgeKind, locale())}${(edgeData.edge.count ?? 1) > 1 ? " ×" + edgeData.edge.count : ""}`,
          color,
          showLabel: !dimmed && (showLabels() || !!direction || loopEdges().has(edge.id)),
          locale: locale(),
          selected,
          searchHit,
          dimmed,
          uncertain,
          revealDelayMs: revealDelay(0, edgeData.revealIndex, reducedMotion()),
          onSelect: () => setSelection({ kind: "edge", id: edge.id }),
          onHover: () => setHoveredTarget({ kind: "edge", id: edge.id }),
          onHoverEnd: clearHoveredTarget,
        },
      };
    }),
  );
  const nodeById = createMemo(
    () => new Map(filteredPayload().nodes.map((node) => [node.id, node])),
  );
  const edgeById = createMemo(
    () => new Map(filteredPayload().edges.map((edge) => [edge.id, edge])),
  );
  const selectedNode = createMemo(() =>
    selection()?.kind === "node" ? nodeById().get(selection()!.id) : undefined,
  );
  const selectedEdge = createMemo(() =>
    selection()?.kind === "edge" ? edgeById().get(selection()!.id) : undefined,
  );
  return (
    <>
      <div class="navigation-toolbar">
        <div class="navigation-view-switch">
          <button
            type="button"
            aria-pressed={viewMode() === "tree" ? "true" : "false"}
            onClick={() => setViewMode("tree")}
          >
            {text("tree")}
          </button>
          <button
            type="button"
            aria-pressed={viewMode() === "graph" ? "true" : "false"}
            onClick={() => setViewMode("graph")}
          >
            {text("graph")}
          </button>
        </div>
        <input
          aria-label={text("search")}
          placeholder={text("searchPlaceholder")}
          value={search()}
          onInput={(event) => setSearch(event.currentTarget.value)}
        />
        <select
          aria-label={text("confidence")}
          value={confidence()}
          onChange={(event) => setConfidence(event.currentTarget.value)}
        >
          <option value="all">{text("allConfidence")}</option>
          <option value="certain">{text("certain")}</option>
          <option value="probable">{text("probable")}</option>
          <option value="possible">{text("possible")}</option>
          <option value="unknown">{text("unknown")}</option>
        </select>
        <select
          aria-label={text("kind")}
          value={edgeKind()}
          onChange={(event) => setEdgeKind(event.currentTarget.value)}
        >
          <option value="all">{text("allTransitions")}</option>
          {edgeKinds.map((kind) => (
            <option value={kind}>{edgeKindLabel(kind, locale())}</option>
          ))}
        </select>
        <select
          aria-label={text("method")}
          value={method()}
          onChange={(event) => setMethod(event.currentTarget.value)}
        >
          <option value="all">{text("allMethods")}</option>
          {methods().map((item) => (
            <option value={item}>{item}</option>
          ))}
        </select>
        {viewMode() === "graph" ? (
          <button type="button" onClick={fitToView} aria-label={text("fit")}>
            {text("fit")}
          </button>
        ) : null}
        <span>
          {text("pagesTransitions", {
            pages: filteredPayload().nodes.length,
            transitions: filteredPayload().edges.length,
          })}
        </span>
      </div>
      <div class="navigation-main">
        {viewMode() === "tree" ? (
          <NavigationTree
            payload={filteredPayload()}
            locale={locale()}
            selection={selection()}
            onSelect={setSelection}
          />
        ) : (
          <div class="navigation-canvas">
            <div class="navigation-graph-tools">
              <div class="navigation-graph-legend">
                <span data-direction="incoming">{text("incoming")}</span>
                <span data-direction="outgoing">{text("outgoing")}</span>
                <span data-direction="both">{text("bothDirections")}</span>
                <span data-direction="uncertain">{text("uncertainLine")}</span>
              </div>
              <div class="navigation-graph-actions">
                {components().length > 1 ? (
                  <select
                    aria-label={text("component")}
                    value={component()?.id ?? "all"}
                    onChange={(event) => setComponentId(event.currentTarget.value)}
                  >
                    <option value="all">
                      {text("allComponents")} ({components().length})
                    </option>
                    {components().map((group) => (
                      <option value={group.id}>
                        {group.isolated ? text("isolatedPages") : group.label} ({group.nodes.length}
                        )
                      </option>
                    ))}
                  </select>
                ) : null}
                <label>
                  <input
                    type="checkbox"
                    checked={showLabels()}
                    onInput={(event) => setShowLabels(event.currentTarget.checked)}
                  />
                  {text("showLabels")}
                </label>
                <button
                  type="button"
                  disabled={!related().active || isLayouting()}
                  onClick={fitConnections}
                >
                  {text("fitConnections")}
                </button>
                <button
                  type="button"
                  disabled={!selection()}
                  onClick={() => {
                    setSelection(undefined);
                    setHovered(undefined);
                  }}
                >
                  {text("clearSelection")}
                </button>
              </div>
            </div>
            <details class="navigation-legend-help">
              <summary>{text("legendHelp")}</summary>
              <div>
                <p>{text("legendSolid")}</p>
                <p>{text("legendDashed")}</p>
                <p>{text("legendNodes")}</p>
                <p>{text("legendSelection")}</p>
              </div>
            </details>
            <NavigationGraphCanvas
              nodes={flowNodes()}
              edges={flowEdges()}
              groups={layout().groups ?? []}
              locale={locale()}
              onController={(controller) => {
                canvasController.current = controller;
              }}
              onClear={() => setSelection(undefined)}
              renderNode={(node) => (
                <NavigationPageNode data={node().data} selected={node().selected} />
              )}
              renderEdge={(edge, zoom) => <NavigationTransitionEdge edge={edge()} zoom={zoom()} />}
            />
            {isLayouting() ? <div class="navigation-layout-status">{text("layouting")}</div> : null}
          </div>
        )}
        <Inspector
          payload={filteredPayload()}
          locale={locale()}
          node={selectedNode()}
          edge={selectedEdge()}
          onSelect={setSelection}
        />
      </div>
    </>
  );
}
function NavigationPageNode(props: {
  data: NavigationFlowNode["data"];
  selected?: boolean;
}): JSX.Element {
  const node = createMemo(() => props.data.node);
  const locale = createMemo(() => props.data.locale ?? "en");
  const style = createMemo(
    () =>
      ({
        "--nav-reveal-delay": `${props.data.revealDelayMs ?? 0}ms`,
      }) as WebviewStyle,
  );
  return (
    <>
      <div
        class={classNames(
          "navigation-node-card",
          `navigation-node-card--${node().kind}`,
          props.selected && "is-selected",
          props.data.searchHit && "is-search-hit",
          props.data.dimmed && "is-dimmed",
        )}
        style={webviewStyle(style())}
        role="button"
        tabindex={0}
        aria-label={`${node().label} (${nodeKindLabel(node().kind, locale())})`}
        onClick={() => props.data.onSelect?.()}
        onKeyDown={(event) => {
          if (event.key === "Enter" || event.key === " ") {
            event.preventDefault();
            props.data.onSelect?.();
          }
        }}
        onPointerEnter={() => props.data.onHover?.()}
        onPointerLeave={() => props.data.onHoverEnd?.()}
      >
        <div class="navigation-node-card__shine" />
        <div class="navigation-node-card__header">
          <span>{nodeKindLabel(node().kind, locale())}</span>
          {node().isRoot ? <span>{navigationText(locale(), "entry")}</span> : null}
          {node().kind !== "unknown" && node().exists === false ? (
            <span>{navigationText(locale(), "missing")}</span>
          ) : null}
        </div>
        <div class="navigation-node-card__title" title={node().label}>
          {node().label}
        </div>
        <div class="navigation-node-card__meta" title={node().uri ?? node().externalUrl ?? ""}>
          {middleEllipsis(node().fileName ?? node().uri ?? node().externalUrl ?? "-", 42)}
        </div>
      </div>
    </>
  );
}
function NavigationTransitionEdge(props: { edge: NavigationFlowEdge; zoom: number }): JSX.Element {
  const data = createMemo(() => props.edge.data);
  const locale = createMemo(() => data().locale ?? "en");
  const markerId = createMemo(() => `arrow-${props.edge.id.replace(/[^a-zA-Z0-9_-]/g, "_")}`);
  return (
    <g class={props.edge.className} data-edge-id={props.edge.id}>
      <defs>
        <marker
          id={markerId()}
          markerWidth={8 / props.zoom}
          markerHeight={8 / props.zoom}
          viewBox="0 0 10 10"
          refX="9"
          refY="5"
          orient="auto"
          markerUnits="userSpaceOnUse"
        >
          <path d="M 0 0 L 10 5 L 0 10 Z" fill={data().color ?? "var(--navigation-edge)"} />
        </marker>
      </defs>
      <path class="navigation-flow-edge-halo" d={data().path} />
      <path
        class="navigation-flow-edge-interaction-path"
        d={data().path}
        role="button"
        tabindex="0"
        aria-label={data().label}
        onPointerEnter={() => data().onHover?.()}
        onPointerLeave={() => data().onHoverEnd?.()}
        onClick={(event) => {
          event.stopPropagation();
          data().onSelect?.();
        }}
        onKeyDown={(event) => {
          if (event.key === "Enter" || event.key === " ") {
            event.preventDefault();
            data().onSelect?.();
          }
        }}
      />
      <path
        class="navigation-flow-edge-path"
        d={data().path}
        marker-end={`url(#${markerId()})`}
        style={webviewStyle({ "--nav-connection-color": data().color })}
      />
      {data().showLabel !== false ? (
        <foreignObject
          x={(data().labelX ?? 0) - 70}
          y={(data().labelY ?? 0) - 14}
          width="140"
          height="28"
          class="navigation-edge-label"
        >
          <div class="navigation-edge-label-center">
            <button
              type="button"
              class={{
                "navigation-edge-badge": true,
                "is-selected": !!props.edge.selected,
                "is-search-hit": !!data().searchHit,
              }}
              style={webviewStyle({ "--nav-connection-color": data().color })}
              onPointerEnter={() => data().onHover?.()}
              onPointerLeave={() => data().onHoverEnd?.()}
              onClick={(event) => {
                event.stopPropagation();
                data().onSelect?.();
              }}
              aria-label={`${data().label} (${confidenceLabel(data().confidence, locale())})`}
            >
              <span>{data().label}</span>
              {data().uncertain ? (
                <strong title={confidenceLabel(data().confidence, locale())}>?</strong>
              ) : null}
            </button>
          </div>
        </foreignObject>
      ) : null}
    </g>
  );
}
function sourceLocation(evidence: AspNavigationEvidence): string {
  const path = navigationSourcePath(evidence.uri);
  const range = evidence.valueRange ?? evidence.range;
  return `${path.split("/").at(-1) ?? path}:${range.start.line + 1}:${range.start.character + 1}`;
}
function NavigationEvidence(props: {
  items: AspNavigationEvidence[];
  locale: "en" | "ja";
}): JSX.Element {
  return (
    <div class="navigation-evidence">
      {props.items.map((evidence) => (
        <div class="navigation-evidence-item">
          <strong>{sourceLocation(evidence)}</strong>
          <span class="navigation-source-path">{navigationSourcePath(evidence.uri)}</span>
          <pre>
            <code>{evidence.snippet ?? evidence.label}</code>
          </pre>
          <button
            type="button"
            onClick={() =>
              vscode.postMessage({
                type: "openRange",
                uri: evidence.uri,
                range: evidence.valueRange ?? evidence.range,
              })
            }
          >
            {navigationText(props.locale, "openSource")}
          </button>
        </div>
      ))}
    </div>
  );
}
function NavigationTree(props: {
  payload: AspNavigationGraphPayload;
  locale: "en" | "ja";
  selection: Selection;
  onSelect: (selection: Selection) => void;
}): JSX.Element {
  const entries = createMemo(() => navigationTreeEntries(props.payload));
  const [collapsed, setCollapsed] = createSignal<Set<string>>(untrack(() => new Set<string>()));
  const [focusedId, setFocusedId] = createSignal<string>();
  createEffect(
    () => [props.payload],
    () => {
      setCollapsed(new Set<string>());
      setFocusedId(undefined);
    },
  );
  const visible = createMemo(() => visibleNavigationTreeEntries(entries(), collapsed()));
  const entryIndices = createMemo(
    () => new Map(entries().map((entry, index) => [entry.id, index])),
  );
  const scrollElement = { current: null } as {
    current: HTMLDivElement | null;
  };
  const virtualizer = createVirtualizer(() => ({
    count: visible().length,
    getScrollElement: () => scrollElement.current,
    estimateSize: () => 78,
    overscan: 8,
    getItemKey: (index) => visible()[index].id,
  }));
  const toggle = (id: string) =>
    setCollapsed((current) => {
      const next = new Set(current);
      if (!next.delete(id)) next.add(id);
      return next;
    });
  const select = (index: number) => {
    const entry = visible()[index];
    if (!entry) return;
    setFocusedId(entry.id);
    props.onSelect(
      entry.edge ? { kind: "edge", id: entry.edge.id } : { kind: "node", id: entry.node.id },
    );
  };
  const focus = (index: number) => {
    const entry = visible()[index];
    if (!entry) return;
    setFocusedId(entry.id);
    virtualizer.scrollToIndex(index);
    window.requestAnimationFrame(() =>
      scrollElement.current?.querySelector<HTMLElement>(`[data-tree-index="${index}"]`)?.focus(),
    );
  };
  const activeId = createMemo(() =>
    visible().some((entry) => entry.id === focusedId()) ? focusedId() : visible()[0]?.id,
  );
  return (
    <section class="navigation-tree-panel">
      <div class="navigation-tree-toolbar">
        <strong>{navigationText(props.locale, "tree")}</strong>
        <button type="button" onClick={() => setCollapsed(new Set<string>())}>
          {navigationText(props.locale, "expandAll")}
        </button>
        <button
          type="button"
          onClick={() =>
            setCollapsed(
              new Set(
                entries()
                  .filter((entry, index) => entry.subtreeEnd > index + 1)
                  .map((entry) => entry.id),
              ),
            )
          }
        >
          {navigationText(props.locale, "collapseAll")}
        </button>
      </div>
      <div
        class="navigation-tree-scroll"
        ref={(element) => (scrollElement.current = element)}
        role="tree"
        aria-label={navigationText(props.locale, "tree")}
      >
        {!visible().length ? (
          <p class="navigation-empty">{navigationText(props.locale, "noResults")}</p>
        ) : null}
        <div style={webviewStyle({ height: virtualizer.getTotalSize(), position: "relative" })}>
          {virtualizer.getVirtualItems().map((item) => {
            const entry = visible()[item.index];
            const hasChildren = entry.subtreeEnd > (entryIndices().get(entry.id) ?? 0) + 1;
            const isSelected = entry.edge
              ? props.selection?.kind === "edge" && props.selection.id === entry.edge.id
              : props.selection?.kind === "node" && props.selection.id === entry.node.id;
            const evidence = entry.edge?.evidence[0];
            const label =
              entry.node.kind === "unknown"
                ? navigationText(props.locale, "unresolved")
                : entry.node.label;
            return (
              <div
                role="treeitem"
                aria-level={entry.depth + 1}
                aria-expanded={
                  (hasChildren ? !collapsed().has(entry.id) : undefined) == null
                    ? undefined
                    : (hasChildren ? !collapsed().has(entry.id) : undefined)
                      ? "true"
                      : "false"
                }
                aria-selected={isSelected == null ? undefined : isSelected ? "true" : "false"}
                tabindex={activeId() === entry.id ? 0 : -1}
                data-tree-index={item.index}
                class={classNames("navigation-tree-row", isSelected && "is-selected")}
                style={webviewStyle({
                  transform: `translateY(${item.start}px)`,
                  height: item.size,
                  paddingLeft: 12 + Math.min(entry.depth, 12) * 22,
                })}
                onFocus={() => setFocusedId(entry.id)}
                onClick={() => select(item.index)}
                onKeyDown={(event) => {
                  if (
                    ![
                      "ArrowDown",
                      "ArrowUp",
                      "ArrowLeft",
                      "ArrowRight",
                      "Home",
                      "End",
                      "Enter",
                      " ",
                    ].includes(event.key)
                  )
                    return;
                  event.preventDefault();
                  if (event.key === "Enter" || event.key === " ") {
                    select(item.index);
                    return;
                  }
                  if (event.key === "ArrowDown")
                    focus(Math.min(visible().length - 1, item.index + 1));
                  if (event.key === "ArrowUp") focus(Math.max(0, item.index - 1));
                  if (event.key === "Home") focus(0);
                  if (event.key === "End") focus(visible().length - 1);
                  if (event.key === "ArrowRight" && hasChildren) {
                    if (collapsed().has(entry.id)) toggle(entry.id);
                    else focus(item.index + 1);
                  }
                  if (event.key === "ArrowLeft") {
                    if (hasChildren && !collapsed().has(entry.id)) toggle(entry.id);
                    else if (entry.parentIndex !== undefined)
                      focus(
                        visible().findIndex(
                          (candidate) => candidate.id === entries()[entry.parentIndex!].id,
                        ),
                      );
                  }
                }}
              >
                <button
                  type="button"
                  tabindex={-1}
                  class="navigation-tree-disclosure"
                  aria-label={label}
                  aria-expanded={
                    (hasChildren ? !collapsed().has(entry.id) : undefined) == null
                      ? undefined
                      : (hasChildren ? !collapsed().has(entry.id) : undefined)
                        ? "true"
                        : "false"
                  }
                  disabled={!hasChildren}
                  onClick={(event) => {
                    event.stopPropagation();
                    toggle(entry.id);
                  }}
                >
                  {hasChildren ? (collapsed().has(entry.id) ? "▸" : "▾") : "·"}
                </button>
                <span
                  class={`navigation-tree-kind navigation-tree-kind--${entry.node.kind}`}
                  aria-hidden="true"
                />
                <div class="navigation-tree-content">
                  <div class="navigation-tree-heading">
                    <strong>{label}</strong>
                    {entry.node.isRoot ? (
                      <span class="navigation-tree-badge">
                        {navigationText(props.locale, "entry")}
                      </span>
                    ) : null}
                    {entry.reference ? (
                      <span class="navigation-tree-badge">
                        {navigationText(props.locale, entry.reference)}
                      </span>
                    ) : null}
                    {entry.depth > 12 ? (
                      <span class="navigation-tree-badge">{entry.depth + 1}</span>
                    ) : null}
                    {entry.edge ? (
                      <span
                        class="navigation-tree-confidence"
                        data-confidence={entry.edge.confidence}
                      >
                        {confidenceLabel(entry.edge.confidence, props.locale)}
                      </span>
                    ) : null}
                  </div>
                  <div class="navigation-tree-meta">
                    {entry.edge
                      ? `${entry.edge.method ? entry.edge.method + " · " : ""}${edgeKindLabel(entry.edge.kind, props.locale)}${evidence ? " · " + sourceLocation(evidence) : ""}`
                      : nodeKindLabel(entry.node.kind, props.locale)}
                  </div>
                  <code
                    class="navigation-tree-preview"
                    title={
                      entry.node.kind === "unknown"
                        ? evidence?.snippet
                        : (entry.node.uri ?? entry.node.externalUrl)
                    }
                  >
                    {entry.node.kind === "unknown"
                      ? (evidence?.snippet ?? entry.node.label)
                      : navigationSourcePath(
                          entry.node.uri ?? entry.node.externalUrl ?? entry.node.label,
                        )}
                  </code>
                </div>
              </div>
            );
          })}
        </div>
      </div>
    </section>
  );
}
function Inspector(props: {
  payload: AspNavigationGraphPayload;
  locale: "en" | "ja";
  node?: AspNavigationNode;
  edge?: AspNavigationEdge;
  onSelect: (selection: Selection) => void;
}): JSX.Element {
  return (
    <>
      {(() => {
        if (props.edge) {
          const source = props.payload.nodes.find((item) => item.id === props.edge!.source);
          const target = props.payload.nodes.find((item) => item.id === props.edge!.target);
          const includeDerived =
            !!props.edge!.declaredInUri &&
            !!source?.uri &&
            normalizeUri(props.edge!.declaredInUri) !== normalizeUri(source.uri);
          return (
            <aside class="navigation-inspector">
              <h2>{edgeKindLabel(props.edge!.kind, props.locale)}</h2>
              {target?.kind === "unknown" ? (
                <>
                  <p class="navigation-unresolved-help">
                    {navigationText(props.locale, "unresolvedHelp")}
                  </p>
                  <NavigationEvidence items={props.edge!.evidence} locale={props.locale} />
                </>
              ) : null}
              <dl>
                <dt>{navigationText(props.locale, "from")}</dt>
                <dd>
                  <button
                    type="button"
                    class="navigation-inspector-link"
                    onClick={() => props.onSelect({ kind: "node", id: props.edge!.source })}
                  >
                    {source?.label ?? props.edge!.source}
                  </button>
                </dd>
                <dt>{navigationText(props.locale, "to")}</dt>
                <dd>
                  <button
                    type="button"
                    class="navigation-inspector-link"
                    onClick={() => props.onSelect({ kind: "node", id: props.edge!.target })}
                  >
                    {target?.kind === "unknown"
                      ? navigationText(props.locale, "unresolved")
                      : (target?.label ?? props.edge!.target)}
                  </button>
                </dd>
                <dt>{navigationText(props.locale, "confidence")}</dt>
                <dd>{confidenceLabel(props.edge!.confidence, props.locale)}</dd>
                <dt>{navigationText(props.locale, "method")}</dt>
                <dd>{props.edge!.method ?? "-"}</dd>
                <dt>{navigationText(props.locale, "targetFrame")}</dt>
                <dd>{props.edge!.targetFrame ?? "-"}</dd>
                <dt>{navigationText(props.locale, "declaredIn")}</dt>
                <dd>{props.edge!.declaredInUri ?? "-"}</dd>
                <dt>{navigationText(props.locale, "include")}</dt>
                <dd>{includeDerived ? navigationText(props.locale, "includeDerived") : "-"}</dd>
                <dt>{navigationText(props.locale, "count")}</dt>
                <dd>{props.edge!.count ?? 1}</dd>
              </dl>
              <h3>{navigationText(props.locale, "parameters")}</h3>
              {props.edge!.parameters && props.edge!.parameters.length > 0 ? (
                <dl>
                  {props.edge!.parameters.map((parameter) => (
                    <>
                      <dt>{parameter.source}</dt>
                      <dd>
                        {parameter.name}
                        {parameter.value ? ` = ${parameter.value}` : ""}
                      </dd>
                    </>
                  ))}
                </dl>
              ) : (
                <p class="navigation-empty">{navigationText(props.locale, "noParameterFlow")}</p>
              )}
              {target?.kind !== "unknown" ? (
                <>
                  <h3>{navigationText(props.locale, "evidence")}</h3>
                  <NavigationEvidence items={props.edge!.evidence} locale={props.locale} />
                </>
              ) : null}
            </aside>
          );
        }
        if (props.node) {
          const evidence = props.payload.edges
            .filter((item) => item.target === props.node!.id)
            .flatMap((item) => item.evidence);
          if (props.node!.kind === "unknown") {
            return (
              <aside class="navigation-inspector">
                <h2>{navigationText(props.locale, "unresolved")}</h2>
                <p class="navigation-unresolved-help">
                  {navigationText(props.locale, "unresolvedHelp")}
                </p>
                {props.node!.label !== "{unknown}" ? <code>{props.node!.label}</code> : null}
                <h3>{navigationText(props.locale, "evidence")}</h3>
                <NavigationEvidence items={evidence} locale={props.locale} />
                <NodeConnections
                  payload={props.payload}
                  node={props.node}
                  locale={props.locale}
                  onSelect={props.onSelect}
                />
              </aside>
            );
          }
          return (
            <aside class="navigation-inspector">
              <h2>{props.node!.label}</h2>
              <NodeConnections
                payload={props.payload}
                node={props.node}
                locale={props.locale}
                onSelect={props.onSelect}
              />
              <dl>
                <dt>{navigationText(props.locale, "kind")}</dt>
                <dd>{nodeKindLabel(props.node!.kind, props.locale)}</dd>
                <dt>{navigationText(props.locale, "uri")}</dt>
                <dd>{props.node!.uri ?? props.node!.externalUrl ?? "-"}</dd>
                <dt>{navigationText(props.locale, "exists")}</dt>
                <dd>
                  {props.node!.exists === false
                    ? navigationText(props.locale, "missing")
                    : navigationText(props.locale, "known")}
                </dd>
              </dl>
            </aside>
          );
        }
        return (
          <aside class="navigation-inspector">
            <h2>{navigationText(props.locale, "navigationGraph")}</h2>
            <dl>
              <dt>{navigationText(props.locale, "scope")}</dt>
              <dd>{props.payload.scope}</dd>
              <dt>{navigationText(props.locale, "documents")}</dt>
              <dd>{props.payload.stats.documents}</dd>
              <dt>{navigationText(props.locale, "nodes")}</dt>
              <dd>{props.payload.stats.nodes}</dd>
              <dt>{navigationText(props.locale, "edges")}</dt>
              <dd>{props.payload.stats.edges}</dd>
            </dl>
            <p class="navigation-empty">{navigationText(props.locale, "selectPageOrTransition")}</p>
          </aside>
        );
      })()}
    </>
  );
}
function NodeConnections(props: {
  payload: AspNavigationGraphPayload;
  node: AspNavigationNode;
  locale: "en" | "ja";
  onSelect: (selection: Selection) => void;
}): JSX.Element {
  const nodeById = createMemo(() => new Map(props.payload.nodes.map((item) => [item.id, item])));
  return (
    <div class="navigation-connections">
      {(["incoming", "outgoing"] as const).map((direction) => {
        const edges = props.payload.edges.filter(
          (edge) => (direction === "incoming" ? edge.target : edge.source) === props.node.id,
        );
        return (
          <section data-direction={direction}>
            <h3>
              {navigationText(props.locale, direction)} <span>{edges.length}</span>
            </h3>
            {edges.length ? (
              <ul>
                {edges.map((edge) => {
                  const otherId = direction === "incoming" ? edge.source : edge.target;
                  const other = nodeById().get(otherId);
                  const label =
                    other?.kind === "unknown"
                      ? navigationText(props.locale, "unresolved")
                      : (other?.label ?? otherId);
                  return (
                    <li>
                      <button
                        type="button"
                        onClick={() => props.onSelect({ kind: "edge", id: edge.id })}
                      >
                        <span>
                          {direction === "incoming" ? "← " : "→ "}
                          {label}
                        </span>
                        <small>
                          {edge.method ? edge.method + " · " : ""}
                          {edgeKindLabel(edge.kind, props.locale)} ·{" "}
                          {confidenceLabel(edge.confidence, props.locale)}
                        </small>
                      </button>
                    </li>
                  );
                })}
              </ul>
            ) : (
              <p class="navigation-empty">{navigationText(props.locale, "noConnections")}</p>
            )}
          </section>
        );
      })}
    </div>
  );
}
function filterPayload(
  payload: NavigationGraphWebviewPayload,
  filters: {
    search: string;
    confidence: string;
    edgeKind: string;
    method: string;
  },
): NavigationGraphWebviewPayload {
  const query = filters.search.trim().toLowerCase();
  const nodeMatches = new Set(
    payload.nodes
      .filter(
        (node) =>
          !query ||
          node.label.toLowerCase().includes(query) ||
          node.uri?.toLowerCase().includes(query) ||
          node.externalUrl?.toLowerCase().includes(query),
      )
      .map((node) => node.id),
  );
  const nodeById = new Map(payload.nodes.map((node) => [node.id, node]));
  const edges = payload.edges.filter((edge) => {
    if (filters.confidence !== "all" && edge.confidence !== filters.confidence) {
      return false;
    }
    if (filters.edgeKind !== "all" && edge.kind !== filters.edgeKind) {
      return false;
    }
    if (filters.method !== "all" && (edge.method ?? "") !== filters.method) {
      return false;
    }
    if (!query) {
      return true;
    }
    const source = nodeById.get(edge.source);
    const target = nodeById.get(edge.target);
    return (
      nodeMatches.has(edge.source) ||
      nodeMatches.has(edge.target) ||
      source?.label.toLowerCase().includes(query) ||
      target?.label.toLowerCase().includes(query) ||
      edge.kind.toLowerCase().includes(query) ||
      edge.method?.toLowerCase().includes(query) ||
      edge.evidence.some((item) => item.snippet?.toLowerCase().includes(query))
    );
  });
  const visibleNodeIds = new Set(edges.flatMap((edge) => [edge.source, edge.target]));
  for (const id of nodeMatches) {
    visibleNodeIds.add(id);
  }
  return {
    ...payload,
    nodes: payload.nodes.filter((node) => visibleNodeIds.has(node.id)),
    edges,
  };
}
function searchHitSets(
  payload: AspNavigationGraphPayload,
  search: string,
): {
  nodes: Set<string>;
  edges: Set<string>;
} {
  const query = search.trim().toLowerCase();
  if (!query) {
    return { nodes: new Set(), edges: new Set() };
  }
  const nodeById = new Map(payload.nodes.map((node) => [node.id, node]));
  const nodes = new Set<string>();
  const edges = new Set<string>();
  for (const node of payload.nodes) {
    if (
      node.label.toLowerCase().includes(query) ||
      node.uri?.toLowerCase().includes(query) ||
      node.externalUrl?.toLowerCase().includes(query)
    ) {
      nodes.add(node.id);
    }
  }
  for (const edge of payload.edges) {
    const source = nodeById.get(edge.source);
    const target = nodeById.get(edge.target);
    if (
      nodes.has(edge.source) ||
      nodes.has(edge.target) ||
      source?.label.toLowerCase().includes(query) ||
      target?.label.toLowerCase().includes(query) ||
      edge.kind.toLowerCase().includes(query) ||
      edge.method?.toLowerCase().includes(query) ||
      edge.confidence.toLowerCase().includes(query) ||
      edge.evidence.some((item) => item.snippet?.toLowerCase().includes(query))
    ) {
      edges.add(edge.id);
    }
  }
  return { nodes, edges };
}
function sameHoverTarget(left: HoverTarget, right: HoverTarget): boolean {
  return left?.kind === right?.kind && left?.id === right?.id;
}
function createReducedMotion(): Accessor<boolean> {
  const [reducedMotion, setReducedMotion] = createSignal(
    untrack(() => window.matchMedia("(prefers-reduced-motion: reduce)").matches),
  );
  onSettled(() => {
    const query = window.matchMedia("(prefers-reduced-motion: reduce)");
    const update = () => setReducedMotion(query.matches);
    query.addEventListener("change", update);
    return () => query.removeEventListener("change", update);
  });
  return reducedMotion;
}
function revealDelay(layer: number, index: number, reducedMotion: boolean): number {
  if (reducedMotion) {
    return 0;
  }
  return Math.min(620, layer * 90 + index * 18);
}
function distinctMethods(edges: AspNavigationEdge[]): string[] {
  return [
    ...new Set(edges.map((edge) => edge.method).filter((value): value is string => !!value)),
  ].sort((left, right) => left.localeCompare(right));
}
function edgeKindLabel(kind: AspNavigationEdgeKind, locale: "en" | "ja" = "en"): string {
  if (locale === "ja") {
    const labels: Record<AspNavigationEdgeKind, string> = {
      serverRedirect: "サーバーリダイレクト",
      htmlAnchor: "HTML アンカー",
      htmlFrame: "HTML フレーム",
      htmlForm: "HTML フォーム",
      metaRefresh: "meta refresh",
      javascriptLocation: "JavaScript location",
      javascriptHistory: "JavaScript history",
      javascriptFormSubmit: "JavaScript フォーム送信",
    };
    return labels[kind];
  }
  return kind
    .replace(/^html/, "HTML ")
    .replace(/^javascript/, "JS ")
    .replace(/^server/, "Server ")
    .replace(/([a-z])([A-Z])/g, "$1 $2");
}
function nodeKindLabel(kind: AspNavigationNode["kind"], locale: "en" | "ja"): string {
  if (locale === "ja") {
    const labels: Record<AspNavigationNode["kind"], string> = {
      page: "ページ",
      fragment: "フラグメント",
      external: "外部",
      unknown: "不明",
    };
    return labels[kind];
  }
  return kind;
}
function confidenceLabel(
  confidence: AspNavigationEdge["confidence"] | undefined,
  locale: "en" | "ja",
): string {
  switch (confidence) {
    case "certain":
      return navigationText(locale, "certain");
    case "probable":
      return navigationText(locale, "probable");
    case "possible":
      return navigationText(locale, "possible");
    default:
      return navigationText(locale, "unknown");
  }
}
function middleEllipsis(value: string, maxLength: number): string {
  if (value.length <= maxLength) {
    return value;
  }
  const keep = Math.max(4, Math.floor((maxLength - 1) / 2));
  return `${value.slice(0, keep)}...${value.slice(-keep)}`;
}
function normalizeUri(uri: string): string {
  return uri.toLowerCase();
}
function classNames(...values: Array<string | false | undefined>): string {
  return values.filter(Boolean).join(" ");
}
function emptyPayload(): NavigationGraphWebviewPayload {
  return {
    scope: "document",
    nodes: [],
    edges: [],
    stats: {
      documents: 0,
      nodes: 0,
      edges: 0,
      certain: 0,
      probable: 0,
      possible: 0,
      unknown: 0,
      external: 0,
    },
  };
}
const navigationErrorLocale = window.__ASP_LSP_NAVIGATION_GRAPH__?.locale === "ja" ? "ja" : "en";
render(
  () => (
    <WebviewErrorBoundary
      title={
        navigationErrorLocale === "ja"
          ? "画面遷移グラフの表示に失敗しました"
          : "Navigation graph failed to render"
      }
    >
      <NavigationGraphApp />
    </WebviewErrorBoundary>
  ),
  document.getElementById("root")!,
);
