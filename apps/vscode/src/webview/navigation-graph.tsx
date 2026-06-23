import React, { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import {
  Background,
  Controls,
  EdgeLabelRenderer,
  Handle,
  MiniMap,
  Position,
  ReactFlow,
  ReactFlowProvider,
  getSmoothStepPath,
  useReactFlow,
  type EdgeProps,
  type EdgeTypes,
  type NodeProps,
  type NodeTypes,
} from "@xyflow/react";
import reactFlowStyles from "@xyflow/react/dist/style.css?inline";
import type {
  AspNavigationEdge,
  AspNavigationEdgeKind,
  AspNavigationGraphPayload,
  AspNavigationNode,
} from "@asp-lsp/core";
import styles from "./navigation-graph.css?inline";
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

type Selection = { kind: "node"; id: string } | { kind: "edge"; id: string } | undefined;
type HoverTarget = Selection;

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
const nodeTypes = { navigationPage: NavigationPageNode } satisfies NodeTypes;
const edgeTypes = { navigationTransition: NavigationTransitionEdge } satisfies EdgeTypes;
const hoverClearDelayMs = 80;

function NavigationGraphApp(): React.ReactElement {
  const [payload, setPayload] = useState<NavigationGraphWebviewPayload>(
    window.__ASP_LSP_NAVIGATION_GRAPH__ ?? emptyPayload(),
  );

  useEffect(() => {
    const listener = (event: MessageEvent) => {
      const message = event.data as { type?: string; payload?: NavigationGraphWebviewPayload };
      if (message.type === "navigationGraphPayload" && message.payload) {
        setPayload(message.payload);
      }
    };
    window.addEventListener("message", listener);
    return () => window.removeEventListener("message", listener);
  }, []);

  return (
    <div className="navigation-shell">
      <style>{reactFlowStyles}</style>
      <style>{styles}</style>
      <ReactFlowProvider>
        <NavigationGraphSurface payload={payload} />
      </ReactFlowProvider>
    </div>
  );
}

function NavigationGraphSurface({
  payload,
}: {
  payload: NavigationGraphWebviewPayload;
}): React.ReactElement {
  const reactFlow = useReactFlow<NavigationFlowNode, NavigationFlowEdge>();
  const reducedMotion = useReducedMotion();
  const [search, setSearch] = useState("");
  const [confidence, setConfidence] = useState("all");
  const [edgeKind, setEdgeKind] = useState("all");
  const [method, setMethod] = useState("all");
  const [selection, setSelection] = useState<Selection>();
  const [hovered, setHovered] = useState<HoverTarget>();
  const [layout, setLayout] = useState<NavigationFlowLayout>(emptyLayout);
  const [isLayouting, setIsLayouting] = useState(false);
  const layoutRequest = useRef(0);
  const hoverClearTimer = useRef<number | undefined>(undefined);
  const filteredPayload = useMemo(
    () => filterPayload(payload, { search, confidence, edgeKind, method }),
    [payload, search, confidence, edgeKind, method],
  );
  const methods = useMemo(() => distinctMethods(payload.edges), [payload.edges]);

  useEffect(() => {
    const requestId = layoutRequest.current + 1;
    layoutRequest.current = requestId;
    setIsLayouting(true);
    void layoutNavigationGraphWithElk(filteredPayload)
      .then((nextLayout) => {
        if (layoutRequest.current === requestId) {
          setLayout(nextLayout);
        }
      })
      .finally(() => {
        if (layoutRequest.current === requestId) {
          setIsLayouting(false);
        }
      });
  }, [filteredPayload]);

  const fitToView = useCallback(() => {
    window.requestAnimationFrame(() => {
      reactFlow.fitView({
        padding: 0.18,
        duration: reducedMotion ? 80 : 720,
        includeHiddenNodes: false,
      });
    });
  }, [reactFlow, reducedMotion]);

  useEffect(() => {
    fitToView();
  }, [fitToView, layout.width, layout.height]);

  useEffect(() => {
    setSelection(undefined);
  }, [payload]);

  useEffect(
    () => () => {
      if (hoverClearTimer.current !== undefined) {
        window.clearTimeout(hoverClearTimer.current);
      }
    },
    [],
  );

  const setHoveredTarget = useCallback((target: Exclude<HoverTarget, undefined>) => {
    if (hoverClearTimer.current !== undefined) {
      window.clearTimeout(hoverClearTimer.current);
      hoverClearTimer.current = undefined;
    }
    setHovered((current) => (sameHoverTarget(current, target) ? current : target));
  }, []);
  const clearHoveredTarget = useCallback(() => {
    if (hoverClearTimer.current !== undefined) {
      window.clearTimeout(hoverClearTimer.current);
    }
    hoverClearTimer.current = window.setTimeout(() => {
      hoverClearTimer.current = undefined;
      setHovered(undefined);
    }, hoverClearDelayMs);
  }, []);

  const searchHits = useMemo(
    () => searchHitSets(filteredPayload, search),
    [filteredPayload, search],
  );
  const related = useMemo(() => relatedElementSets(layout.edges, hovered), [hovered, layout.edges]);
  const flowNodes = useMemo(
    () =>
      layout.nodes.map((node) => {
        const selected = selection?.kind === "node" && selection.id === node.id;
        const searchHit = searchHits.nodes.has(node.id);
        const dimmed = !!hovered && !related.nodes.has(node.id);
        return {
          ...node,
          selected,
          className: classNames(
            "navigation-flow-node",
            `navigation-node--${node.data.node.kind}`,
            selected && "navigation-node--selected",
            searchHit && "navigation-node--search-hit",
            dimmed && "navigation-node--dimmed",
          ),
          data: {
            ...node.data,
            selected,
            searchHit,
            dimmed,
            revealDelayMs: revealDelay(node.data.layer, node.data.revealIndex, reducedMotion),
            onHover: () => setHoveredTarget({ kind: "node", id: node.id }),
            onHoverEnd: clearHoveredTarget,
          },
        };
      }),
    [
      clearHoveredTarget,
      hovered,
      layout.nodes,
      reducedMotion,
      related.nodes,
      searchHits.nodes,
      selection,
      setHoveredTarget,
    ],
  );
  const flowEdges = useMemo(
    () =>
      layout.edges.map((edge) => {
        const edgeData = edge.data as NonNullable<NavigationFlowEdge["data"]>;
        const selected = selection?.kind === "edge" && selection.id === edge.id;
        const searchHit = searchHits.edges.has(edge.id);
        const dimmed = !!hovered && !related.edges.has(edge.id);
        const uncertain = edgeData.confidence !== "certain";
        return {
          ...edge,
          selected,
          className: classNames(
            "navigation-flow-edge",
            `navigation-edge--${edgeData.confidence}`,
            uncertain && "navigation-edge--uncertain",
            selected && "navigation-edge--selected",
            searchHit && "navigation-edge--search-hit",
            dimmed && "navigation-edge--dimmed",
          ),
          data: {
            ...edgeData,
            selected,
            searchHit,
            dimmed,
            uncertain,
            revealDelayMs: revealDelay(0, edgeData.revealIndex, reducedMotion),
            onSelect: () => setSelection({ kind: "edge", id: edge.id }),
            onHover: () => setHoveredTarget({ kind: "edge", id: edge.id }),
            onHoverEnd: clearHoveredTarget,
          },
        };
      }),
    [
      clearHoveredTarget,
      hovered,
      layout.edges,
      reducedMotion,
      related.edges,
      searchHits.edges,
      selection,
      setHoveredTarget,
    ],
  );
  const nodeById = useMemo(
    () => new Map(filteredPayload.nodes.map((node) => [node.id, node])),
    [filteredPayload.nodes],
  );
  const edgeById = useMemo(
    () => new Map(filteredPayload.edges.map((edge) => [edge.id, edge])),
    [filteredPayload.edges],
  );
  const selectedNode = selection?.kind === "node" ? nodeById.get(selection.id) : undefined;
  const selectedEdge = selection?.kind === "edge" ? edgeById.get(selection.id) : undefined;

  return (
    <>
      <div className="navigation-toolbar">
        <input
          aria-label="Search"
          placeholder="Search pages or transitions"
          value={search}
          onChange={(event) => setSearch(event.currentTarget.value)}
        />
        <select value={confidence} onChange={(event) => setConfidence(event.currentTarget.value)}>
          <option value="all">All confidence</option>
          <option value="certain">Certain</option>
          <option value="probable">Probable</option>
          <option value="possible">Possible</option>
          <option value="unknown">Unknown</option>
        </select>
        <select value={edgeKind} onChange={(event) => setEdgeKind(event.currentTarget.value)}>
          <option value="all">All transitions</option>
          {edgeKinds.map((kind) => (
            <option key={kind} value={kind}>
              {edgeKindLabel(kind)}
            </option>
          ))}
        </select>
        <select value={method} onChange={(event) => setMethod(event.currentTarget.value)}>
          <option value="all">All methods</option>
          {methods.map((item) => (
            <option key={item} value={item}>
              {item}
            </option>
          ))}
        </select>
        <button type="button" onClick={fitToView}>
          Fit
        </button>
        <span>
          {filteredPayload.nodes.length} pages / {filteredPayload.edges.length} transitions
        </span>
      </div>
      <div className="navigation-main">
        <div className="navigation-canvas">
          <ReactFlow<NavigationFlowNode, NavigationFlowEdge>
            nodes={flowNodes}
            edges={flowEdges}
            nodeTypes={nodeTypes}
            edgeTypes={edgeTypes}
            minZoom={0.12}
            maxZoom={2.2}
            defaultViewport={{ x: 0, y: 0, zoom: 1 }}
            nodesDraggable={false}
            nodesConnectable={false}
            elementsSelectable
            panOnScroll
            fitView
            fitViewOptions={{ padding: 0.18 }}
            proOptions={{ hideAttribution: true }}
            onNodeClick={(_, node) => setSelection({ kind: "node", id: node.id })}
            onEdgeClick={(_, edge) => setSelection({ kind: "edge", id: edge.id })}
            onPaneClick={() => setSelection(undefined)}
            onNodeMouseEnter={(_, node) => setHoveredTarget({ kind: "node", id: node.id })}
            onNodeMouseLeave={clearHoveredTarget}
            onEdgeMouseEnter={(_, edge) => setHoveredTarget({ kind: "edge", id: edge.id })}
            onEdgeMouseLeave={clearHoveredTarget}
          >
            <Background gap={24} size={1.2} color="var(--navigation-grid-dot)" />
            <MiniMap
              pannable
              zoomable
              nodeStrokeWidth={3}
              maskColor="var(--navigation-minimap-mask)"
              nodeColor={(node) =>
                miniMapNodeColor((node.data as NonNullable<NavigationFlowNode["data"]>).node.kind)
              }
            />
            <Controls showInteractive={false} />
          </ReactFlow>
          {isLayouting ? <div className="navigation-layout-status">Layout...</div> : null}
        </div>
        <Inspector payload={filteredPayload} node={selectedNode} edge={selectedEdge} />
      </div>
    </>
  );
}

function NavigationPageNode({ data, selected }: NodeProps<NavigationFlowNode>): React.ReactElement {
  const node = data.node;
  const style = {
    "--nav-reveal-delay": `${data.revealDelayMs ?? 0}ms`,
  } as React.CSSProperties;
  return (
    <div
      className={classNames(
        "navigation-node-card",
        `navigation-node-card--${node.kind}`,
        selected && "is-selected",
        data.searchHit && "is-search-hit",
        data.dimmed && "is-dimmed",
      )}
      style={style}
      onPointerEnter={() => data.onHover?.()}
      onPointerLeave={() => data.onHoverEnd?.()}
    >
      <Handle
        className="navigation-handle navigation-handle--target"
        type="target"
        id="target"
        position={Position.Left}
      />
      <div className="navigation-node-card__shine" />
      <div className="navigation-node-card__header">
        <span>{node.kind}</span>
        {node.isRoot ? <span>entry</span> : null}
        {node.exists === false ? <span>missing</span> : null}
      </div>
      <div className="navigation-node-card__title" title={node.label}>
        {node.label}
      </div>
      <div className="navigation-node-card__meta" title={node.uri ?? node.externalUrl ?? ""}>
        {middleEllipsis(node.uri ?? node.externalUrl ?? "-", 42)}
      </div>
      <Handle
        className="navigation-handle navigation-handle--source"
        type="source"
        id="source"
        position={Position.Right}
      />
    </div>
  );
}

function NavigationTransitionEdge({
  id,
  data,
  markerEnd,
  selected,
  sourceX,
  sourceY,
  sourcePosition,
  targetX,
  targetY,
  targetPosition,
}: EdgeProps<NavigationFlowEdge>): React.ReactElement {
  const [fallbackPath, fallbackLabelX, fallbackLabelY] = getSmoothStepPath({
    sourceX,
    sourceY,
    sourcePosition,
    targetX,
    targetY,
    targetPosition,
    borderRadius: 18,
  });
  const path = data?.path ?? fallbackPath;
  const labelX = data?.labelX ?? fallbackLabelX;
  const labelY = data?.labelY ?? fallbackLabelY;
  const pathRef = useRef<SVGPathElement | null>(null);
  const [pathLength, setPathLength] = useState(1);
  useLayoutEffect(() => {
    const nextLength = pathRef.current?.getTotalLength();
    if (nextLength && Number.isFinite(nextLength)) {
      setPathLength(nextLength);
    }
  }, [path]);
  const style = {
    "--nav-edge-length": `${pathLength}`,
    "--nav-edge-delay": `${data?.revealDelayMs ?? 0}ms`,
  } as React.CSSProperties;
  const labelStyle = {
    transform: `translate(-50%, -50%) translate(${labelX}px, ${labelY}px)`,
    "--nav-edge-delay": `${data?.revealDelayMs ?? 0}ms`,
  } as React.CSSProperties;
  return (
    <>
      <path
        className="navigation-flow-edge-interaction-path"
        d={path}
        onPointerEnter={() => data?.onHover?.()}
        onPointerLeave={() => data?.onHoverEnd?.()}
      />
      <path
        id={id}
        ref={pathRef}
        className="react-flow__edge-path navigation-flow-edge-path"
        d={path}
        markerEnd={markerEnd}
        style={style}
      />
      <EdgeLabelRenderer>
        <button
          type="button"
          className={classNames(
            "navigation-edge-badge",
            "nodrag",
            "nopan",
            selected && "is-selected",
            data?.searchHit && "is-search-hit",
          )}
          style={labelStyle}
          onPointerEnter={() => data?.onHover?.()}
          onPointerLeave={() => data?.onHoverEnd?.()}
          onClick={(event) => {
            event.stopPropagation();
            data?.onSelect?.();
          }}
        >
          <span>{data?.label ?? "transition"}</span>
          <strong>{data?.confidence ?? "unknown"}</strong>
        </button>
      </EdgeLabelRenderer>
    </>
  );
}

function Inspector({
  payload,
  node,
  edge,
}: {
  payload: AspNavigationGraphPayload;
  node?: AspNavigationNode;
  edge?: AspNavigationEdge;
}): React.ReactElement {
  if (edge) {
    const source = payload.nodes.find((item) => item.id === edge.source);
    const target = payload.nodes.find((item) => item.id === edge.target);
    const includeDerived =
      !!edge.declaredInUri &&
      !!source?.uri &&
      normalizeUri(edge.declaredInUri) !== normalizeUri(source.uri);
    return (
      <aside className="navigation-inspector">
        <h2>{edgeKindLabel(edge.kind)}</h2>
        <dl>
          <dt>From</dt>
          <dd>{source?.label ?? edge.source}</dd>
          <dt>To</dt>
          <dd>{target?.label ?? edge.target}</dd>
          <dt>Confidence</dt>
          <dd>{edge.confidence}</dd>
          <dt>Method</dt>
          <dd>{edge.method ?? "-"}</dd>
          <dt>Target frame</dt>
          <dd>{edge.targetFrame ?? "-"}</dd>
          <dt>Declared in</dt>
          <dd>{edge.declaredInUri ?? "-"}</dd>
          <dt>Include</dt>
          <dd>{includeDerived ? "declared in included fragment" : "-"}</dd>
          <dt>Count</dt>
          <dd>{edge.count ?? 1}</dd>
        </dl>
        <h3>Parameters</h3>
        {edge.parameters && edge.parameters.length > 0 ? (
          <dl>
            {edge.parameters.map((parameter, index) => (
              <React.Fragment key={`${parameter.source}:${parameter.name}:${index}`}>
                <dt>{parameter.source}</dt>
                <dd>
                  {parameter.name}
                  {parameter.value ? ` = ${parameter.value}` : ""}
                </dd>
              </React.Fragment>
            ))}
          </dl>
        ) : (
          <p className="navigation-empty">No parameter flow.</p>
        )}
        <h3>Evidence</h3>
        <div className="navigation-evidence">
          {edge.evidence.map((evidence, index) => (
            <div className="navigation-evidence-item" key={`${evidence.uri}:${index}`}>
              <strong>{evidence.label}</strong>
              <code>{evidence.snippet ?? evidence.uri}</code>
              <button
                type="button"
                onClick={() =>
                  vscode.postMessage({
                    type: "openRange",
                    uri: evidence.uri,
                    range: evidence.range,
                  })
                }
              >
                Open source
              </button>
            </div>
          ))}
        </div>
      </aside>
    );
  }
  if (node) {
    return (
      <aside className="navigation-inspector">
        <h2>{node.label}</h2>
        <dl>
          <dt>Kind</dt>
          <dd>{node.kind}</dd>
          <dt>URI</dt>
          <dd>{node.uri ?? node.externalUrl ?? "-"}</dd>
          <dt>Exists</dt>
          <dd>{node.exists === false ? "missing" : "known"}</dd>
        </dl>
      </aside>
    );
  }
  return (
    <aside className="navigation-inspector">
      <h2>Navigation Graph</h2>
      <dl>
        <dt>Scope</dt>
        <dd>{payload.scope}</dd>
        <dt>Documents</dt>
        <dd>{payload.stats.documents}</dd>
        <dt>Nodes</dt>
        <dd>{payload.stats.nodes}</dd>
        <dt>Edges</dt>
        <dd>{payload.stats.edges}</dd>
      </dl>
      <p className="navigation-empty">Select a page or transition.</p>
    </aside>
  );
}

function filterPayload(
  payload: NavigationGraphWebviewPayload,
  filters: { search: string; confidence: string; edgeKind: string; method: string },
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
): { nodes: Set<string>; edges: Set<string> } {
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

function relatedElementSets(
  edges: NavigationFlowEdge[],
  hovered: HoverTarget,
): { nodes: Set<string>; edges: Set<string> } {
  if (!hovered) {
    return { nodes: new Set(), edges: new Set() };
  }
  const nodes = new Set<string>();
  const edgeIds = new Set<string>();
  if (hovered.kind === "node") {
    nodes.add(hovered.id);
    for (const edge of edges) {
      if (edge.source === hovered.id || edge.target === hovered.id) {
        edgeIds.add(edge.id);
        nodes.add(edge.source);
        nodes.add(edge.target);
      }
    }
  } else {
    edgeIds.add(hovered.id);
    const edge = edges.find((item) => item.id === hovered.id);
    if (edge) {
      nodes.add(edge.source);
      nodes.add(edge.target);
    }
  }
  return { nodes, edges: edgeIds };
}

function sameHoverTarget(left: HoverTarget, right: HoverTarget): boolean {
  return left?.kind === right?.kind && left?.id === right?.id;
}

function useReducedMotion(): boolean {
  const [reducedMotion, setReducedMotion] = useState(
    () => window.matchMedia("(prefers-reduced-motion: reduce)").matches,
  );
  useEffect(() => {
    const query = window.matchMedia("(prefers-reduced-motion: reduce)");
    const update = () => setReducedMotion(query.matches);
    query.addEventListener("change", update);
    return () => query.removeEventListener("change", update);
  }, []);
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

function edgeKindLabel(kind: AspNavigationEdgeKind): string {
  return kind
    .replace(/^html/, "HTML ")
    .replace(/^javascript/, "JS ")
    .replace(/^server/, "Server ")
    .replace(/([a-z])([A-Z])/g, "$1 $2");
}

function middleEllipsis(value: string, maxLength: number): string {
  if (value.length <= maxLength) {
    return value;
  }
  const keep = Math.max(4, Math.floor((maxLength - 1) / 2));
  return `${value.slice(0, keep)}...${value.slice(-keep)}`;
}

function miniMapNodeColor(kind: AspNavigationNode["kind"]): string {
  switch (kind) {
    case "external":
      return "var(--navigation-external)";
    case "unknown":
      return "var(--navigation-unknown)";
    case "fragment":
      return "var(--navigation-fragment)";
    case "page":
      return "var(--navigation-page)";
  }
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

createRoot(document.getElementById("root")!).render(<NavigationGraphApp />);
