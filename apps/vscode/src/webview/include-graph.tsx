import React, { useLayoutEffect, useMemo, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import ForceGraph2D from "react-force-graph-2d";
import ForceGraph3D from "react-force-graph-3d";
import tailwindStyles from "./include-graph.css?inline";
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
        <main className="grid place-items-center text-[#9aa7b8]">Graph data is unavailable.</main>
      </Shell>
    );
  }

  return (
    <Shell>
      <header className="grid grid-cols-[minmax(180px,1fr)_auto_auto] items-center gap-3 border-b border-[#2b3442] bg-[#171c25] px-3 py-2.5 max-[780px]:grid-cols-1 max-[780px]:items-stretch">
        <div className="flex min-w-0 items-center gap-2.5">
          <span className="overflow-hidden text-ellipsis whitespace-nowrap text-[13px] font-semibold text-[#d7dde8]">
            {graph.scope === "workspace" ? "Workspace Graph" : "Current File Graph"}
          </span>
          {graph.truncated ? (
            <span className="text-[11px] text-[#ffcb6b]">truncated: {graph.truncated.reason}</span>
          ) : null}
        </div>
        <div className="flex flex-wrap gap-2">
          <Metric label="Files" value={graph.stats.files} />
          <Metric label="VB" value={graph.stats.declarations} />
          <Metric label="Links" value={graph.stats.links} />
          <Metric label="Missing" value={graph.stats.missingIncludes} />
        </div>
        <div
          className="inline-grid grid-cols-2 overflow-hidden rounded-md border border-[#394456]"
          aria-label="Graph mode"
        >
          <button
            type="button"
            className={
              mode === "3d"
                ? "h-7 min-w-[42px] cursor-pointer border-0 bg-[#89ddff] text-[#11151c]"
                : "h-7 min-w-[42px] cursor-pointer border-0 bg-[#151a22] text-[#b5c0d0]"
            }
            onClick={() => setMode("3d")}
          >
            3D
          </button>
          <button
            type="button"
            className={
              mode === "2d"
                ? "h-7 min-w-[42px] cursor-pointer border-0 bg-[#89ddff] text-[#11151c]"
                : "h-7 min-w-[42px] cursor-pointer border-0 bg-[#151a22] text-[#b5c0d0]"
            }
            onClick={() => setMode("2d")}
          >
            2D
          </button>
        </div>
      </header>
      <main className="relative grid min-h-0 grid-cols-[minmax(0,1fr)_320px] overflow-hidden max-[780px]:grid-cols-1 max-[780px]:grid-rows-[minmax(0,1fr)]">
        <section
          ref={surfaceRef}
          className="relative min-h-0 min-w-0 overflow-hidden [&_canvas]:block"
        >
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
        <Inspector selection={selection} onClose={() => setSelection(undefined)} />
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
      <style>{tailwindStyles}</style>
      <div className="grid h-full min-w-0 grid-rows-[auto_1fr] bg-[#11151c] text-[#d7dde8]">
        {children}
      </div>
    </>
  );
}

function Metric({ label, value }: { label: string; value: number }): React.ReactElement {
  return (
    <span className="inline-flex items-baseline gap-[5px] rounded-md border border-[#303a49] px-[7px] py-[3px] text-[11px] text-[#9aa7b8]">
      <span>{label}</span>
      <strong className="text-xs text-[#f4f7fb]">{value}</strong>
    </span>
  );
}

function Inspector({
  selection,
  onClose,
}: {
  selection: Selection;
  onClose(): void;
}): React.ReactElement {
  const className = selection
    ? "min-w-0 overflow-auto border-l border-[#2b3442] bg-[#171c25] p-3.5 max-[780px]:absolute max-[780px]:inset-x-2.5 max-[780px]:top-2.5 max-[780px]:z-10 max-[780px]:block max-[780px]:max-h-[min(260px,calc(100%_-_20px))] max-[780px]:rounded-md max-[780px]:border max-[780px]:shadow-[0_14px_34px_rgb(0_0_0_/_34%)]"
    : "min-w-0 overflow-auto border-l border-[#2b3442] bg-[#171c25] p-3.5 max-[780px]:hidden";
  if (!selection) {
    return (
      <aside className={className}>
        <h2 className="mb-3 text-sm leading-[1.35] font-semibold [overflow-wrap:anywhere]">
          Inspector
        </h2>
        <p className="m-0 text-xs text-[#8d98a8]">Select a node or link.</p>
      </aside>
    );
  }
  const location =
    selection.type === "node"
      ? { uri: selection.item.uri, range: selection.item.range }
      : selection.item.ranges[0];
  return (
    <aside className={className}>
      <div className="mb-3 flex min-w-0 items-start gap-2">
        <h2 className="m-0 min-w-0 flex-1 text-sm leading-[1.35] font-semibold [overflow-wrap:anywhere]">
          {selection.type === "node" ? selection.item.label : selection.item.label}
        </h2>
        <button
          type="button"
          className="hidden h-7 w-7 shrink-0 rounded-md border border-[#405068] bg-[#202735] text-sm leading-none text-[#d7dde8] max-[780px]:inline-grid max-[780px]:place-items-center"
          aria-label="Close inspector"
          onClick={onClose}
        >
          x
        </button>
      </div>
      {selection.type === "node" ? (
        <NodeDetails node={selection.item} />
      ) : (
        <LinkDetails link={selection.item} />
      )}
      <button
        type="button"
        className="h-[30px] w-full cursor-pointer rounded-md border border-[#405068] bg-[#c3e88d] text-[#11151c] disabled:cursor-not-allowed disabled:bg-[#202735] disabled:text-[#717b8c]"
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
    <dl className="mb-3.5 grid grid-cols-[86px_minmax(0,1fr)] gap-x-2.5 gap-y-2">
      <Detail label="Kind" value={node.kind} />
      <Detail label="Group" value={node.group} />
      <Detail label="Declaration" value={node.declarationKind} />
      <Detail label="Member of" value={node.memberOf} />
      <Detail label="Scope" value={node.bindingScope} />
      <Detail label="Origin" value={node.origin} />
      <Detail label="External" value={node.externalKind} />
      <Detail label="File" value={node.fileName} />
      <Detail label="URI" value={node.uri} />
    </dl>
  );
}

function LinkDetails({ link }: { link: GraphLink }): React.ReactElement {
  return (
    <dl className="mb-3.5 grid grid-cols-[86px_minmax(0,1fr)] gap-x-2.5 gap-y-2">
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
      <dt className="text-[11px] text-[#8d98a8]">{label}</dt>
      <dd
        className="m-0 overflow-hidden text-ellipsis whitespace-nowrap text-xs text-[#d7dde8]"
        title={value}
      >
        {value}
      </dd>
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

createRoot(document.getElementById("root") ?? document.body).render(<App />);
