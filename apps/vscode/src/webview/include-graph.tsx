import React, { useLayoutEffect, useMemo, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import ForceGraph2D from "react-force-graph-2d";
import ForceGraph3D from "react-force-graph-3d";
import type { AspGraphLink, AspGraphNode, AspGraphPayload } from "../include-graph-webview";

declare const acquireVsCodeApi: () => {
  postMessage(message: unknown): void;
};

declare global {
  interface Window {
    __ASP_LSP_GRAPH__?: AspGraphPayload;
  }
}

type ViewMode = "3d" | "2d";

type GraphNode = AspGraphNode & {
  color: string;
  x?: number;
  y?: number;
};

type GraphLink = Omit<AspGraphLink, "source" | "target"> & {
  source: string | GraphNode;
  target: string | GraphNode;
  color: string;
};

type Selection = { type: "node"; item: GraphNode } | { type: "link"; item: GraphLink } | undefined;

const vscode = acquireVsCodeApi();
const graph = window.__ASP_LSP_GRAPH__;

const nodeColors: Record<AspGraphNode["kind"], string> = {
  file: "#67d8ef",
  vbDeclaration: "#c792ea",
  vbUnresolved: "#ffcb6b",
};

const linkColors: Record<AspGraphLink["kind"], string> = {
  include: "#82aaff",
  declares: "#89ddff",
  references: "#c3e88d",
  calls: "#f78c6c",
  unresolvedReference: "#ffcb6b",
};

function App(): React.ReactElement {
  const [mode, setMode] = useState<ViewMode>("3d");
  const [selection, setSelection] = useState<Selection>();
  const graphData = useMemo(() => graphDataFor(graph), []);
  const [surfaceRef, surfaceSize] = useElementSize<HTMLElement>();

  if (!graph) {
    return (
      <Shell>
        <main className="empty-state">Graph data is unavailable.</main>
      </Shell>
    );
  }

  return (
    <Shell>
      <header className="toolbar">
        <div className="title">
          <span className="title-main">
            {graph.scope === "workspace" ? "Workspace Graph" : "Current File Graph"}
          </span>
          {graph.truncated ? (
            <span className="warning">truncated: {graph.truncated.reason}</span>
          ) : null}
        </div>
        <div className="stats">
          <Metric label="Files" value={graph.stats.files} />
          <Metric label="VB" value={graph.stats.declarations} />
          <Metric label="Links" value={graph.stats.links} />
          <Metric label="Missing" value={graph.stats.missingIncludes} />
        </div>
        <div className="segmented" aria-label="Graph mode">
          <button
            type="button"
            className={mode === "3d" ? "active" : ""}
            onClick={() => setMode("3d")}
          >
            3D
          </button>
          <button
            type="button"
            className={mode === "2d" ? "active" : ""}
            onClick={() => setMode("2d")}
          >
            2D
          </button>
        </div>
      </header>
      <main className="workspace">
        <section ref={surfaceRef} className="graph-surface">
          {mode === "3d" ? (
            <ForceGraph3D
              graphData={graphData}
              width={surfaceSize.width}
              height={surfaceSize.height}
              backgroundColor="#11151c"
              nodeColor={(node) => (node as GraphNode).color}
              nodeLabel={(node) => nodeLabel(node as GraphNode)}
              linkColor={(link) => (link as GraphLink).color}
              linkDirectionalParticles={1}
              linkDirectionalParticleWidth={(link) => Math.min(4, (link as GraphLink).count)}
              onNodeClick={(node) => setSelection({ type: "node", item: node as GraphNode })}
              onLinkClick={(link) => setSelection({ type: "link", item: link as GraphLink })}
            />
          ) : (
            <ForceGraph2D
              graphData={graphData}
              width={surfaceSize.width}
              height={surfaceSize.height}
              backgroundColor="#11151c"
              nodeLabel={(node) => nodeLabel(node as GraphNode)}
              linkColor={(link) => (link as GraphLink).color}
              linkDirectionalParticles={1}
              linkDirectionalParticleWidth={(link) => Math.min(4, (link as GraphLink).count)}
              nodeCanvasObject={(node, canvas, scale) =>
                paintNode(node as GraphNode, canvas, scale)
              }
              onNodeClick={(node) => setSelection({ type: "node", item: node as GraphNode })}
              onLinkClick={(link) => setSelection({ type: "link", item: link as GraphLink })}
            />
          )}
        </section>
        <Inspector selection={selection} />
      </main>
    </Shell>
  );
}

function useElementSize<TElement extends HTMLElement>(): [
  React.RefObject<TElement | null>,
  { width: number; height: number },
] {
  const ref = useRef<TElement>(null);
  const [size, setSize] = useState({ width: 1, height: 1 });

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

function Shell({ children }: { children: React.ReactNode }): React.ReactElement {
  return (
    <>
      <style>{styles}</style>
      <div className="app-shell">{children}</div>
    </>
  );
}

function Metric({ label, value }: { label: string; value: number }): React.ReactElement {
  return (
    <span className="metric">
      <span>{label}</span>
      <strong>{value}</strong>
    </span>
  );
}

function Inspector({ selection }: { selection: Selection }): React.ReactElement {
  if (!selection) {
    return (
      <aside className="inspector">
        <h2>Inspector</h2>
        <p className="muted">Select a node or link.</p>
      </aside>
    );
  }
  const location =
    selection.type === "node"
      ? { uri: selection.item.uri, range: selection.item.range }
      : selection.item.ranges[0];
  return (
    <aside className="inspector">
      <h2>{selection.type === "node" ? selection.item.label : selection.item.label}</h2>
      {selection.type === "node" ? (
        <NodeDetails node={selection.item} />
      ) : (
        <LinkDetails link={selection.item} />
      )}
      <button
        type="button"
        className="jump-button"
        disabled={!location?.uri}
        onClick={() => {
          if (location?.uri) {
            vscode.postMessage({ type: "openRange", uri: location.uri, range: location.range });
          }
        }}
      >
        Open Source
      </button>
    </aside>
  );
}

function NodeDetails({ node }: { node: GraphNode }): React.ReactElement {
  return (
    <dl>
      <Detail label="Kind" value={node.kind} />
      <Detail label="Group" value={node.group} />
      <Detail label="Declaration" value={node.declarationKind} />
      <Detail label="Member of" value={node.memberOf} />
      <Detail label="Scope" value={node.bindingScope} />
      <Detail label="File" value={node.fileName} />
      <Detail label="URI" value={node.uri} />
    </dl>
  );
}

function LinkDetails({ link }: { link: GraphLink }): React.ReactElement {
  return (
    <dl>
      <Detail label="Kind" value={link.kind} />
      <Detail label="Role" value={link.role} />
      <Detail label="Count" value={String(link.count)} />
      <Detail label="Include" value={link.include?.path} />
      <Detail label="Mode" value={link.include?.mode} />
      <Detail label="Exists" value={link.include ? String(link.include.exists) : undefined} />
      <Detail label="Actual path" value={link.include?.actualPath} />
    </dl>
  );
}

function Detail({
  label,
  value,
}: {
  label: string;
  value: string | undefined;
}): React.ReactElement | null {
  if (!value) {
    return null;
  }
  return (
    <>
      <dt>{label}</dt>
      <dd title={value}>{value}</dd>
    </>
  );
}

function graphDataFor(payload: AspGraphPayload | undefined): {
  nodes: GraphNode[];
  links: GraphLink[];
} {
  if (!payload) {
    return { nodes: [], links: [] };
  }
  return {
    nodes: payload.nodes.map((node) => ({
      ...node,
      color: nodeColors[node.kind],
    })),
    links: payload.links.map((link) => ({
      ...link,
      color: linkColors[link.kind],
    })),
  };
}

function nodeLabel(node: GraphNode): string {
  if (node.kind === "vbDeclaration" && node.declarationKind) {
    return `${node.declarationKind}: ${node.label}`;
  }
  if (node.kind === "vbUnresolved" && node.role) {
    return `${node.role}: ${node.label}`;
  }
  return node.label;
}

function paintNode(node: GraphNode, canvas: CanvasRenderingContext2D, scale: number): void {
  const radius = node.kind === "file" ? 5 : 4;
  canvas.beginPath();
  canvas.arc(node.x ?? 0, node.y ?? 0, radius, 0, 2 * Math.PI, false);
  canvas.fillStyle = node.color;
  canvas.fill();
  if (scale > 1.2) {
    canvas.font = `${Math.max(8, 12 / scale)}px system-ui, sans-serif`;
    canvas.fillStyle = "#d7dde8";
    canvas.fillText(node.label, (node.x ?? 0) + radius + 2, (node.y ?? 0) + 3);
  }
}

const styles = `
:root {
  color-scheme: dark;
  font-family: var(--vscode-font-family), system-ui, sans-serif;
  background: #11151c;
  color: #d7dde8;
}

html,
body,
#root {
  height: 100%;
  margin: 0;
}

button {
  font: inherit;
}

.app-shell {
  display: grid;
  grid-template-rows: auto 1fr;
  height: 100%;
  min-width: 0;
  background: #11151c;
}

.toolbar {
  display: grid;
  grid-template-columns: minmax(180px, 1fr) auto auto;
  gap: 12px;
  align-items: center;
  padding: 10px 12px;
  border-bottom: 1px solid #2b3442;
  background: #171c25;
}

.title {
  display: flex;
  align-items: center;
  min-width: 0;
  gap: 10px;
}

.title-main {
  overflow: hidden;
  font-size: 13px;
  font-weight: 650;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.warning {
  color: #ffcb6b;
  font-size: 11px;
}

.stats {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

.metric {
  display: inline-flex;
  align-items: baseline;
  gap: 5px;
  padding: 3px 7px;
  border: 1px solid #303a49;
  border-radius: 6px;
  color: #9aa7b8;
  font-size: 11px;
}

.metric strong {
  color: #f4f7fb;
  font-size: 12px;
}

.segmented {
  display: inline-grid;
  grid-template-columns: 1fr 1fr;
  border: 1px solid #394456;
  border-radius: 6px;
  overflow: hidden;
}

.segmented button {
  min-width: 42px;
  height: 28px;
  border: 0;
  color: #b5c0d0;
  background: #151a22;
  cursor: pointer;
}

.segmented button.active {
  color: #11151c;
  background: #89ddff;
}

.workspace {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 320px;
  min-height: 0;
  overflow: hidden;
}

.graph-surface {
  position: relative;
  min-width: 0;
  min-height: 0;
  overflow: hidden;
}

.graph-surface canvas {
  display: block;
}

.inspector {
  min-width: 0;
  padding: 14px;
  border-left: 1px solid #2b3442;
  background: #171c25;
  overflow: auto;
}

.inspector h2 {
  margin: 0 0 12px;
  overflow-wrap: anywhere;
  font-size: 14px;
  line-height: 1.35;
}

.muted {
  margin: 0;
  color: #8d98a8;
  font-size: 12px;
}

dl {
  display: grid;
  grid-template-columns: 86px minmax(0, 1fr);
  gap: 8px 10px;
  margin: 0 0 14px;
}

dt {
  color: #8d98a8;
  font-size: 11px;
}

dd {
  margin: 0;
  overflow: hidden;
  color: #d7dde8;
  font-size: 12px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.jump-button {
  width: 100%;
  height: 30px;
  border: 1px solid #405068;
  border-radius: 6px;
  color: #11151c;
  background: #c3e88d;
  cursor: pointer;
}

.jump-button:disabled {
  color: #717b8c;
  background: #202735;
  cursor: not-allowed;
}

.empty-state {
  display: grid;
  place-items: center;
  color: #9aa7b8;
}

@media (max-width: 780px) {
  .toolbar {
    grid-template-columns: 1fr;
    align-items: stretch;
  }

  .workspace {
    grid-template-columns: 1fr;
    grid-template-rows: minmax(360px, 1fr) 260px;
  }

  .inspector {
    border-left: 0;
    border-top: 1px solid #2b3442;
  }
}
`;

createRoot(document.getElementById("root") ?? document.body).render(<App />);
