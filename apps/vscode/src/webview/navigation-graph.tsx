import React, { useMemo, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import type {
  AspNavigationEdge,
  AspNavigationEdgeKind,
  AspNavigationGraphPayload,
  AspNavigationNode,
} from "@asp-lsp/core";
import styles from "./navigation-graph.css?inline";
import {
  layoutNavigationGraph,
  type NavigationLayoutEdge,
  type NavigationLayoutNode,
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

function NavigationGraphApp(): React.ReactElement {
  const [payload, setPayload] = useState<NavigationGraphWebviewPayload>(
    window.__ASP_LSP_NAVIGATION_GRAPH__ ?? emptyPayload(),
  );
  const [search, setSearch] = useState("");
  const [confidence, setConfidence] = useState("all");
  const [edgeKind, setEdgeKind] = useState("all");
  const [method, setMethod] = useState("all");
  const [selection, setSelection] = useState<Selection>();
  const [view, setView] = useState({ x: 24, y: 24, scale: 1 });
  const drag = useRef<{ x: number; y: number; viewX: number; viewY: number } | undefined>(
    undefined,
  );
  React.useEffect(() => {
    const listener = (event: MessageEvent) => {
      const message = event.data as { type?: string; payload?: NavigationGraphWebviewPayload };
      if (message.type === "navigationGraphPayload" && message.payload) {
        setPayload(message.payload);
        setSelection(undefined);
      }
    };
    window.addEventListener("message", listener);
    return () => window.removeEventListener("message", listener);
  }, []);
  const filteredPayload = useMemo(
    () => filterPayload(payload, { search, confidence, edgeKind, method }),
    [payload, search, confidence, edgeKind, method],
  );
  const layout = useMemo(() => layoutNavigationGraph(filteredPayload), [filteredPayload]);
  const nodeById = useMemo(() => new Map(layout.nodes.map((node) => [node.id, node])), [layout]);
  const edgeById = useMemo(() => new Map(layout.edges.map((edge) => [edge.id, edge])), [layout]);
  const selectedNode = selection?.kind === "node" ? nodeById.get(selection.id) : undefined;
  const selectedEdge = selection?.kind === "edge" ? edgeById.get(selection.id) : undefined;
  const methods = useMemo(() => distinctMethods(payload.edges), [payload.edges]);
  return (
    <div className="navigation-shell">
      <style>{styles}</style>
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
        <button type="button" onClick={() => setView({ x: 24, y: 24, scale: 1 })}>
          Fit
        </button>
        <button
          type="button"
          onClick={() =>
            setView((value) => ({ ...value, scale: Math.min(2.5, value.scale + 0.1) }))
          }
        >
          +
        </button>
        <button
          type="button"
          onClick={() =>
            setView((value) => ({ ...value, scale: Math.max(0.25, value.scale - 0.1) }))
          }
        >
          -
        </button>
        <span>
          {filteredPayload.nodes.length} pages / {filteredPayload.edges.length} transitions
        </span>
      </div>
      <div className="navigation-main">
        <div className="navigation-canvas">
          <svg
            className={drag.current ? "dragging" : ""}
            viewBox={`0 0 ${Math.max(800, layout.width + 120)} ${Math.max(600, layout.height + 120)}`}
            onPointerDown={(event) => {
              if (event.target !== event.currentTarget) {
                return;
              }
              drag.current = { x: event.clientX, y: event.clientY, viewX: view.x, viewY: view.y };
              event.currentTarget.setPointerCapture(event.pointerId);
            }}
            onPointerMove={(event) => {
              if (!drag.current) {
                return;
              }
              setView({
                ...view,
                x: drag.current.viewX + event.clientX - drag.current.x,
                y: drag.current.viewY + event.clientY - drag.current.y,
              });
            }}
            onPointerUp={() => {
              drag.current = undefined;
            }}
            onWheel={(event) => {
              const direction = event.deltaY > 0 ? -1 : 1;
              setView((value) => ({
                ...value,
                scale: Math.max(0.25, Math.min(2.5, value.scale + direction * 0.08)),
              }));
            }}
          >
            <defs>
              <marker
                id="navigation-arrow"
                viewBox="0 0 10 10"
                refX="9"
                refY="5"
                markerWidth="7"
                markerHeight="7"
                orient="auto-start-reverse"
              >
                <path d="M 0 0 L 10 5 L 0 10 z" fill="currentColor" />
              </marker>
            </defs>
            <g transform={`translate(${view.x} ${view.y}) scale(${view.scale})`}>
              <g>
                {layout.edges.map((edge) => (
                  <NavigationEdgeShape
                    key={edge.id}
                    edge={edge}
                    selected={selection?.kind === "edge" && selection.id === edge.id}
                    onSelect={() => setSelection({ kind: "edge", id: edge.id })}
                  />
                ))}
              </g>
              <g>
                {layout.nodes.map((node) => (
                  <NavigationNodeShape
                    key={node.id}
                    node={node}
                    selected={selection?.kind === "node" && selection.id === node.id}
                    onSelect={() => setSelection({ kind: "node", id: node.id })}
                  />
                ))}
              </g>
            </g>
          </svg>
          <MiniMap layout={layout} />
        </div>
        <Inspector payload={filteredPayload} node={selectedNode} edge={selectedEdge} />
      </div>
    </div>
  );
}

function NavigationNodeShape({
  node,
  selected,
  onSelect,
}: {
  node: NavigationLayoutNode;
  selected: boolean;
  onSelect(): void;
}): React.ReactElement {
  return (
    <g
      className={`navigation-node ${node.kind} ${selected ? "selected" : ""}`}
      transform={`translate(${node.x} ${node.y})`}
      onClick={(event) => {
        event.stopPropagation();
        onSelect();
      }}
    >
      <rect width={node.width} height={node.height} />
      <text x="14" y="25" fontSize="13" fontWeight="600">
        {middleEllipsis(node.label, 30)}
      </text>
      <text className="kind" x="14" y="47">
        {node.kind}
        {node.exists === false ? " / missing" : ""}
      </text>
    </g>
  );
}

function NavigationEdgeShape({
  edge,
  selected,
  onSelect,
}: {
  edge: NavigationLayoutEdge;
  selected: boolean;
  onSelect(): void;
}): React.ReactElement {
  const label = `${edgeKindLabel(edge.kind)}${edge.count && edge.count > 1 ? ` x${edge.count}` : ""}`;
  const labelWidth = Math.max(72, label.length * 6.8 + 18);
  return (
    <g
      onClick={(event) => {
        event.stopPropagation();
        onSelect();
      }}
    >
      <path
        className={`navigation-edge ${edge.confidence} ${selected ? "selected" : ""}`}
        d={edge.path}
        markerEnd="url(#navigation-arrow)"
      />
      <g
        className="navigation-edge-label"
        transform={`translate(${edge.labelX - labelWidth / 2} ${edge.labelY - 11})`}
      >
        <rect width={labelWidth} height="22" />
        <text x={labelWidth / 2} y="15" textAnchor="middle">
          {label}
        </text>
      </g>
    </g>
  );
}

function MiniMap({
  layout,
}: {
  layout: ReturnType<typeof layoutNavigationGraph>;
}): React.ReactElement {
  const scale = Math.min(150 / Math.max(1, layout.width), 92 / Math.max(1, layout.height));
  return (
    <svg className="navigation-minimap" viewBox="0 0 164 108">
      <g transform={`translate(7 8) scale(${scale})`}>
        {layout.edges.map((edge) => (
          <path
            key={edge.id}
            d={edge.path}
            fill="none"
            stroke="var(--vscode-descriptionForeground)"
            strokeWidth={1 / scale}
            opacity="0.45"
          />
        ))}
        {layout.nodes.map((node) => (
          <rect
            key={node.id}
            x={node.x}
            y={node.y}
            width={node.width}
            height={node.height}
            rx="5"
            fill="var(--vscode-button-background)"
            opacity="0.8"
          />
        ))}
      </g>
    </svg>
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
          node.uri?.toLowerCase().includes(query),
      )
      .map((node) => node.id),
  );
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
    return (
      nodeMatches.has(edge.source) ||
      nodeMatches.has(edge.target) ||
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
