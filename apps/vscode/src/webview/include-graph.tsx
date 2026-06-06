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

type NodeColorCategory =
  | "root"
  | "file"
  | "callable"
  | "class"
  | "method"
  | "property"
  | "member"
  | "globalVariable"
  | "globalConstant"
  | "localVariable"
  | "localConstant"
  | "parameter"
  | "unresolved";

type GraphNode = AspGraphNode & {
  color: string;
  category: NodeColorCategory;
  referenceCount: number;
  value: number;
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

const nodeColors: Record<NodeColorCategory, string> = {
  root: "#ffffff",
  file: "#67d8ef",
  callable: "#c792ea",
  class: "#c3e88d",
  method: "#f78c6c",
  property: "#ff9cac",
  member: "#ffb86c",
  globalVariable: "#ffcb6b",
  globalConstant: "#82aaff",
  localVariable: "#dcdcaa",
  localConstant: "#80cbc4",
  parameter: "#b2ccd6",
  unresolved: "#ff5370",
};

const linkColors: Record<AspGraphLink["kind"], string> = {
  include: "#82aaff",
  declares: "#89ddff",
  references: "#c3e88d",
  calls: "#f78c6c",
  unresolvedReference: "#ff5370",
};

const linkMeanings: Record<AspGraphLink["kind"], { label: string; description: string }> = {
  include: {
    label: "Include",
    description: "A Classic ASP file includes another file.",
  },
  declares: {
    label: "Declares",
    description: "A file or containing scope declares a VBScript symbol.",
  },
  references: {
    label: "Reference",
    description: "VBScript reads, writes, or otherwise references a resolved symbol.",
  },
  calls: {
    label: "Call",
    description: "VBScript calls a procedure, function, method, constructor, or member.",
  },
  unresolvedReference: {
    label: "Unresolved",
    description: "VBScript references a symbol that could not be resolved.",
  },
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
          <LinkLegend />
          {mode === "3d" ? (
            <ForceGraph3D
              graphData={graphData}
              width={surfaceSize.width}
              height={surfaceSize.height}
              backgroundColor="#11151c"
              nodeColor={(node) => (node as GraphNode).color}
              nodeVal={(node) => (node as GraphNode).value}
              nodeLabel={(node) => nodeLabel(node as GraphNode)}
              linkColor={(link) => (link as GraphLink).color}
              linkWidth={0}
              linkLabel={(link) => linkLabel(link as GraphLink)}
              linkCurvature={0.25}
              linkDirectionalArrowLength={3.5}
              linkDirectionalArrowRelPos={1}
              linkDirectionalArrowColor={(link) => (link as GraphLink).color}
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
              nodeVal={(node) => (node as GraphNode).value}
              nodeLabel={(node) => nodeLabel(node as GraphNode)}
              linkColor={(link) => (link as GraphLink).color}
              linkWidth={(link) => linkWidth(link as GraphLink)}
              linkLabel={(link) => linkLabel(link as GraphLink)}
              linkCurvature={0.25}
              linkDirectionalArrowLength={3.5}
              linkDirectionalArrowRelPos={1}
              linkDirectionalArrowColor={(link) => (link as GraphLink).color}
              linkDirectionalParticles={1}
              linkDirectionalParticleWidth={(link) => linkParticleWidth2d(link as GraphLink)}
              nodeCanvasObject={(node, canvas, scale) =>
                paintNode(node as GraphNode, canvas, scale)
              }
              nodePointerAreaPaint={(node, color, canvas) =>
                paintNodePointerArea(node as GraphNode, color, canvas)
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

function LinkLegend(): React.ReactElement {
  return (
    <div className="pointer-events-none absolute top-3 left-3 z-10 max-w-[min(460px,calc(100%_-_24px))] rounded-md border border-[#303a49] bg-[#171c25]/90 px-3 py-2 shadow-[0_10px_26px_rgb(0_0_0_/_28%)] backdrop-blur">
      <div className="mb-1.5 text-[11px] font-semibold tracking-[0.08em] text-[#9aa7b8] uppercase">
        Link colors
      </div>
      <div className="grid grid-cols-[repeat(2,minmax(0,1fr))] gap-x-3 gap-y-1.5 max-[560px]:grid-cols-1">
        {Object.entries(linkMeanings).map(([kind, meaning]) => (
          <div key={kind} className="flex min-w-0 items-center gap-2">
            <span
              className="h-0 w-7 shrink-0 rounded-full border-t-2"
              style={{ borderColor: linkColors[kind as AspGraphLink["kind"]] }}
              aria-hidden="true"
            />
            <span
              className="min-w-0 overflow-hidden text-ellipsis whitespace-nowrap text-[11px] text-[#d7dde8]"
              title={meaning.description}
            >
              {meaning.label}
            </span>
          </div>
        ))}
      </div>
    </div>
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
      <Detail label="Category" value={node.category} />
      <Detail label="References" value={String(node.referenceCount)} />
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
  const referenceCounts = graphReferenceCounts(payload.links);
  return {
    nodes: payload.nodes.map((node) => {
      const category = nodeCategoryForColor(node);
      const referenceCount = referenceCounts.get(node.id) ?? 0;
      return {
        ...node,
        category,
        referenceCount,
        value: nodeValue(referenceCount),
        color: nodeColors[category],
      };
    }),
    links: payload.links.map((link) => ({
      ...link,
      color: linkColors[link.kind],
    })),
  };
}

function graphReferenceCounts(links: AspGraphLink[]): Map<string, number> {
  const counts = new Map<string, number>();
  for (const link of links) {
    if (
      link.kind !== "include" &&
      link.kind !== "references" &&
      link.kind !== "calls" &&
      link.kind !== "unresolvedReference"
    ) {
      continue;
    }
    counts.set(link.target, (counts.get(link.target) ?? 0) + link.count);
  }
  return counts;
}

function nodeValue(referenceCount: number): number {
  return clamp(1 + Math.sqrt(referenceCount) * 1.5, 1, 10);
}

function linkWidth(link: GraphLink): number {
  return clamp(0.8 + Math.log2(link.count + 1) * 0.4, 0.8, 3);
}

function linkParticleWidth2d(link: GraphLink): number {
  return clamp(4 + Math.log2(link.count + 1) * 1.5, 5.5, 12);
}

function clamp(value: number, minimum: number, maximum: number): number {
  return Math.min(maximum, Math.max(minimum, value));
}

function nodeCategoryForColor(node: AspGraphNode): NodeColorCategory {
  if (node.isRoot) {
    return "root";
  }
  if (node.kind === "file") {
    return "file";
  }
  if (node.kind === "vbUnresolved") {
    return "unresolved";
  }
  switch (node.declarationKind) {
    case "function":
    case "sub":
      return "callable";
    case "class":
      return "class";
    case "method":
      return "method";
    case "property":
      return "property";
    case "field":
      return "localVariable";
    case "parameter":
      return "parameter";
    case "variable":
      return node.bindingScope === "local" ? "localVariable" : "globalVariable";
    case "constant":
      if (node.bindingScope === "local") {
        return "localConstant";
      }
      return node.memberOf ? "localConstant" : "globalConstant";
    case "object":
      return "globalVariable";
    default:
      return externalNodeCategory(node);
  }
}

function externalNodeCategory(node: AspGraphNode): NodeColorCategory {
  switch (node.externalKind) {
    case "function":
      return "callable";
    case "constant":
      return "globalConstant";
    case "member":
      return "member";
    case "object":
      return "globalVariable";
    default:
      return "globalVariable";
  }
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

function linkLabel(link: GraphLink): string {
  const meaning = linkMeanings[link.kind];
  const count = link.count === 1 ? "1 occurrence" : `${link.count} occurrences`;
  return `${meaning.label}: ${link.label} (${count})\n${meaning.description}`;
}

function paintNode(node: GraphNode, canvas: CanvasRenderingContext2D, scale: number): void {
  const radius = nodeRadius(node);
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

function paintNodePointerArea(
  node: GraphNode,
  color: string,
  canvas: CanvasRenderingContext2D,
): void {
  canvas.fillStyle = color;
  canvas.beginPath();
  canvas.arc(node.x ?? 0, node.y ?? 0, nodeRadius(node) + 2, 0, 2 * Math.PI, false);
  canvas.fill();
}

function nodeRadius(node: GraphNode): number {
  return (node.kind === "file" ? 4 : 3.5) + node.value;
}

createRoot(document.getElementById("root") ?? document.body).render(<App />);
