import React, { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import ForceGraph2D from "react-force-graph-2d";
import ForceGraph3D from "react-force-graph-3d";
import SpriteText from "three-spritetext";
import tailwindStyles from "./include-graph.css?inline";
import type { AspGraphLink, AspGraphNode, AspGraphPayload } from "../include-graph-webview";
import type { ForceGraphMethods as ForceGraph2DMethods } from "react-force-graph-2d";
import type { ForceGraphMethods as ForceGraph3DMethods } from "react-force-graph-3d";

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
  | "function"
  | "sub"
  | "class"
  | "method"
  | "methodFunction"
  | "methodSub"
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
  z?: number;
  vx?: number;
  vy?: number;
  vz?: number;
};

type GraphLink = Omit<AspGraphLink, "source" | "target"> & {
  source: string | GraphNode;
  target: string | GraphNode;
  color: string;
};

type GraphData = {
  nodes: GraphNode[];
  links: GraphLink[];
};

type Selection = { type: "node"; item: GraphNode } | { type: "link"; item: GraphLink } | undefined;

type HighlightState = {
  activeNodeIds: Set<string>;
  activeLinkIds: Set<string>;
};

type LinkFilterCategory = AspGraphLink["kind"] | "member";

type CenteredSpriteText = SpriteText & {
  center: {
    y: number;
  };
};

const vscode = acquireVsCodeApi();
const graph = window.__ASP_LSP_GRAPH__;

const nodeColors: Record<NodeColorCategory, string> = {
  root: "#ffffff",
  file: "#67d8ef",
  function: "#c792ea",
  sub: "#b39ddb",
  class: "#c3e88d",
  method: "#f78c6c",
  methodFunction: "#f78c6c",
  methodSub: "#ffb86c",
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

const linkFilterColors: Record<LinkFilterCategory, string> = {
  ...linkColors,
  member: nodeColors.member,
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

const linkFilterLabels: Record<LinkFilterCategory, string> = {
  include: "Include",
  declares: "Declares",
  references: "Reference",
  calls: "Call",
  unresolvedReference: "Unresolved",
  member: "Member",
};

const linkFilterDescriptions: Record<LinkFilterCategory, string> = {
  include: "A Classic ASP file includes another file.",
  declares: "A file or containing scope declares a VBScript symbol.",
  references: "VBScript reads, writes, or otherwise references a resolved symbol.",
  calls: "VBScript calls a procedure, function, method, constructor, or member.",
  unresolvedReference: "VBScript references a symbol that could not be resolved.",
  member: "VBScript references or calls an object member.",
};

const nodeCategoryLabels: Record<NodeColorCategory, string> = {
  root: "Root",
  file: "File",
  function: "Function",
  sub: "Sub",
  class: "Class",
  method: "Method",
  methodFunction: "Function method",
  methodSub: "Sub method",
  property: "Property",
  member: "Member",
  globalVariable: "Global variable",
  globalConstant: "Global constant",
  localVariable: "Local variable",
  localConstant: "Local constant",
  parameter: "Parameter",
  unresolved: "Unresolved",
};

const nodeCategoryOrder: NodeColorCategory[] = [
  "root",
  "file",
  "class",
  "function",
  "sub",
  "method",
  "methodFunction",
  "methodSub",
  "property",
  "member",
  "globalVariable",
  "globalConstant",
  "localVariable",
  "localConstant",
  "parameter",
  "unresolved",
];

const linkFilterOrder: LinkFilterCategory[] = [
  "include",
  "declares",
  "references",
  "calls",
  "unresolvedReference",
  "member",
];

const minimumNodeValue = 0.6;
const maximumNodeValue = 16;
const maximumNodeScaleReferenceCount = 144;
const graphFitDurationMs = 400;
const graphFitPadding2d = 80;
const graphFitPadding3d = 30;

function App(): React.ReactElement {
  const [mode, setMode] = useState<ViewMode>("3d");
  const [selection, setSelection] = useState<Selection>();
  const [searchQuery, setSearchQuery] = useState("");
  const [searchMatchCase, setSearchMatchCase] = useState(false);
  const [isLegendVisible, setLegendVisible] = useState(true);
  const [hiddenNodeCategories, setHiddenNodeCategories] = useState<Set<NodeColorCategory>>(
    () => new Set(),
  );
  const [hiddenLinkCategories, setHiddenLinkCategories] = useState<Set<LinkFilterCategory>>(
    () => new Set(),
  );
  const graph2dRef = useRef<ForceGraph2DMethods<GraphNode, GraphLink> | undefined>(undefined);
  const graph3dRef = useRef<ForceGraph3DMethods<GraphNode, GraphLink> | undefined>(undefined);
  const hasAutoFit2dRef = useRef(false);
  const hasAutoFit3dRef = useRef(false);
  const graphData = useMemo(() => graphDataFor(graph), []);
  const filteredGraphData = useMemo(
    () => filterGraphData(graphData, hiddenNodeCategories, hiddenLinkCategories),
    [graphData, hiddenNodeCategories, hiddenLinkCategories],
  );
  const filteredStats = useMemo(() => graphStatsFor(filteredGraphData), [filteredGraphData]);
  const nodeLegendCategories = useMemo(
    () => nodeLegendCategoriesFor(graphData.nodes),
    [graphData.nodes],
  );
  const linkLegendCategories = useMemo(
    () => linkLegendCategoriesFor(graphData.links),
    [graphData.links],
  );
  const searchHighlight = useMemo(
    () =>
      highlightForSearch(
        searchQuery,
        searchMatchCase,
        filteredGraphData.nodes,
        filteredGraphData.links,
      ),
    [searchQuery, searchMatchCase, filteredGraphData.nodes, filteredGraphData.links],
  );
  const selectionHighlight = useMemo(
    () => highlightForSelection(selection, filteredGraphData.links),
    [selection, filteredGraphData.links],
  );
  const highlight = selectionHighlight ?? searchHighlight;
  const [surfaceRef, surfaceSize] = useElementSize<HTMLElement>();
  const canFitGraph =
    filteredGraphData.nodes.length > 0 && surfaceSize.width > 0 && surfaceSize.height > 0;
  const toggleNodeCategory = useCallback((category: NodeColorCategory) => {
    setHiddenNodeCategories((current) => toggledSet(current, category));
  }, []);
  const toggleLinkCategory = useCallback((category: LinkFilterCategory) => {
    setHiddenLinkCategories((current) => toggledSet(current, category));
  }, []);
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
  const handleEngineStop = useCallback(
    (nextMode: ViewMode) => {
      const autoFitRef = nextMode === "3d" ? hasAutoFit3dRef : hasAutoFit2dRef;
      if (autoFitRef.current) {
        return;
      }
      autoFitRef.current = fitGraphToCanvas(nextMode);
    },
    [fitGraphToCanvas],
  );

  useEffect(() => {
    const clearSelectionOnEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        setSelection(undefined);
      }
    };
    window.addEventListener("keydown", clearSelectionOnEscape);
    return () => window.removeEventListener("keydown", clearSelectionOnEscape);
  }, []);

  useEffect(() => {
    if (selection && !isSelectionVisible(selection, filteredGraphData)) {
      setSelection(undefined);
    }
  }, [filteredGraphData, selection]);

  if (!graph) {
    return (
      <Shell>
        <main className="grid place-items-center text-[#9aa7b8]">Graph data is unavailable.</main>
      </Shell>
    );
  }

  return (
    <Shell>
      <header className="grid grid-cols-[minmax(180px,1fr)_minmax(220px,320px)_auto_auto_auto] items-center gap-3 border-b border-[#2b3442] bg-[#171c25] px-3 py-2.5 max-[980px]:grid-cols-1 max-[980px]:items-stretch">
        <div className="flex min-w-0 items-center gap-2.5">
          <span className="overflow-hidden text-ellipsis whitespace-nowrap text-[13px] font-semibold text-[#d7dde8]">
            {graph.scope === "workspace" ? "Workspace Graph" : "Current File Graph"}
          </span>
          {graph.truncated ? (
            <span className="text-[11px] text-[#ffcb6b]">truncated: {graph.truncated.reason}</span>
          ) : null}
        </div>
        <div className="grid min-w-0 grid-cols-[minmax(0,1fr)_auto] items-center gap-2">
          <input
            type="search"
            className="h-7 min-w-0 rounded-md border border-[#394456] bg-[#11151c] px-2.5 text-xs text-[#d7dde8] outline-none placeholder:text-[#717b8c] focus:border-[#89ddff]"
            aria-label="Search nodes"
            placeholder="Search nodes"
            value={searchQuery}
            onChange={(event) => setSearchQuery(event.currentTarget.value)}
          />
          <label className="inline-flex h-7 cursor-pointer select-none items-center gap-1.5 whitespace-nowrap rounded-md border border-[#394456] bg-[#151a22] px-2 text-[11px] text-[#b5c0d0]">
            <input
              type="checkbox"
              className="m-0 h-3.5 w-3.5 accent-[#89ddff]"
              checked={searchMatchCase}
              onChange={(event) => setSearchMatchCase(event.currentTarget.checked)}
            />
            Match case
          </label>
        </div>
        <div className="flex flex-wrap gap-2">
          <Metric label="Files" value={filteredStats.files} />
          <Metric label="VB" value={filteredStats.declarations} />
          <Metric label="Links" value={filteredStats.links} />
          <Metric label="Missing" value={filteredStats.missingIncludes} />
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
        <button
          type="button"
          className="h-7 min-w-[42px] cursor-pointer rounded-md border border-[#394456] bg-[#151a22] px-2.5 text-xs text-[#b5c0d0] disabled:cursor-not-allowed disabled:text-[#717b8c]"
          aria-label="Fit graph to canvas"
          title="Fit graph to canvas"
          disabled={!canFitGraph}
          onClick={() => fitGraphToCanvas(mode)}
        >
          Fit
        </button>
      </header>
      <main className="relative grid min-h-0 grid-cols-[minmax(0,1fr)_320px] overflow-hidden max-[780px]:grid-cols-1 max-[780px]:grid-rows-[minmax(0,1fr)]">
        <section
          ref={surfaceRef}
          className="relative min-h-0 min-w-0 overflow-hidden [&_canvas]:block"
        >
          {isLegendVisible ? (
            <GraphLegend
              hiddenLinkCategories={hiddenLinkCategories}
              hiddenNodeCategories={hiddenNodeCategories}
              linkCategories={linkLegendCategories}
              nodeCategories={nodeLegendCategories}
              onClose={() => setLegendVisible(false)}
              onToggleLinkCategory={toggleLinkCategory}
              onToggleNodeCategory={toggleNodeCategory}
            />
          ) : (
            <button
              type="button"
              className="absolute top-3 left-3 z-10 h-7 cursor-pointer rounded-md border border-[#303a49] bg-[#171c25]/90 px-2.5 text-xs text-[#d7dde8] shadow-[0_10px_26px_rgb(0_0_0_/_28%)] backdrop-blur"
              aria-label="Show graph legend"
              onClick={() => setLegendVisible(true)}
            >
              Legend
            </button>
          )}
          {mode === "3d" ? (
            <ForceGraph3D
              ref={graph3dRef}
              graphData={filteredGraphData}
              width={surfaceSize.width}
              height={surfaceSize.height}
              backgroundColor="#11151c"
              nodeColor={(node) => nodeColor(node as GraphNode, highlight)}
              nodeVal={(node) => (node as GraphNode).value}
              nodeLabel={(node) => nodeLabel(node as GraphNode)}
              nodeThreeObjectExtend={true}
              nodeThreeObject={(node: GraphNode) => nodeTextObject(node, highlight)}
              linkColor={(link) => linkColor(link as GraphLink, highlight)}
              linkWidth={(link) => linkWidth3d(link as GraphLink, highlight)}
              linkLabel={(link) => linkLabel(link as GraphLink)}
              linkCurvature={0.25}
              linkDirectionalArrowLength={(link) => linkArrowLength(link as GraphLink)}
              linkDirectionalArrowRelPos={1}
              linkDirectionalArrowColor={(link) => linkColor(link as GraphLink, highlight)}
              linkDirectionalParticles={(link) => linkParticleCount(link as GraphLink, highlight)}
              linkDirectionalParticleWidth={1.5}
              onNodeClick={(node) => setSelection({ type: "node", item: node as GraphNode })}
              onLinkClick={(link) => setSelection({ type: "link", item: link as GraphLink })}
              onBackgroundClick={() => setSelection(undefined)}
              cooldownTicks={100}
              onEngineStop={() => handleEngineStop("3d")}
            />
          ) : (
            <ForceGraph2D
              ref={graph2dRef}
              graphData={filteredGraphData}
              width={surfaceSize.width}
              height={surfaceSize.height}
              backgroundColor="#11151c"
              nodeVal={(node) => (node as GraphNode).value}
              nodeLabel={(node) => nodeLabel(node as GraphNode)}
              linkColor={(link) => linkColor(link as GraphLink, highlight)}
              linkWidth={(link) => linkWidth2d(link as GraphLink, highlight)}
              linkLabel={(link) => linkLabel(link as GraphLink)}
              linkCurvature={0.25}
              linkDirectionalArrowLength={(link) => linkArrowLength(link as GraphLink)}
              linkDirectionalArrowRelPos={1}
              linkDirectionalArrowColor={(link) => linkColor(link as GraphLink, highlight)}
              linkDirectionalParticles={(link) => linkParticleCount(link as GraphLink, highlight)}
              linkDirectionalParticleWidth={4.5}
              nodeCanvasObject={(node, canvas) => paintNode(node as GraphNode, canvas, highlight)}
              nodePointerAreaPaint={(node, color, canvas) =>
                paintNodePointerArea(node as GraphNode, color, canvas)
              }
              onNodeClick={(node) => setSelection({ type: "node", item: node as GraphNode })}
              onLinkClick={(link) => setSelection({ type: "link", item: link as GraphLink })}
              onBackgroundClick={() => setSelection(undefined)}
              cooldownTicks={100}
              onEngineStop={() => handleEngineStop("2d")}
            />
          )}
        </section>
        <Inspector selection={selection} onClose={() => setSelection(undefined)} />
      </main>
    </Shell>
  );
}

function GraphLegend({
  hiddenLinkCategories,
  hiddenNodeCategories,
  linkCategories,
  nodeCategories,
  onClose,
  onToggleLinkCategory,
  onToggleNodeCategory,
}: {
  hiddenLinkCategories: ReadonlySet<LinkFilterCategory>;
  hiddenNodeCategories: ReadonlySet<NodeColorCategory>;
  linkCategories: LinkFilterCategory[];
  nodeCategories: NodeColorCategory[];
  onClose(): void;
  onToggleLinkCategory(category: LinkFilterCategory): void;
  onToggleNodeCategory(category: NodeColorCategory): void;
}): React.ReactElement {
  return (
    <div className="absolute top-3 left-3 z-10 grid max-w-[min(560px,calc(100%_-_24px))] gap-2.5 rounded-md border border-[#303a49] bg-[#171c25]/90 px-3 py-2 shadow-[0_10px_26px_rgb(0_0_0_/_28%)] backdrop-blur">
      <div className="flex min-w-0 items-center justify-between gap-3">
        <span className="text-[11px] font-semibold tracking-[0.08em] text-[#9aa7b8] uppercase">
          Legend
        </span>
        <button
          type="button"
          className="grid h-6 w-6 shrink-0 cursor-pointer place-items-center rounded-md border border-[#405068] bg-[#202735] text-xs leading-none text-[#d7dde8]"
          aria-label="Close graph legend"
          onClick={onClose}
        >
          x
        </button>
      </div>
      <LegendFilterGroup itemCount={linkCategories.length} title="Link filters">
        {linkCategories.map((category) => (
          <LegendFilterItem
            key={category}
            checked={!hiddenLinkCategories.has(category)}
            color={linkFilterColors[category]}
            label={linkFilterLabels[category]}
            title={linkFilterDescriptions[category]}
            variant={category === "member" ? "node" : "link"}
            onToggle={() => onToggleLinkCategory(category)}
          />
        ))}
      </LegendFilterGroup>
      <LegendFilterGroup itemCount={nodeCategories.length} title="Node filters">
        {nodeCategories.map((category) => (
          <LegendFilterItem
            key={category}
            checked={!hiddenNodeCategories.has(category)}
            color={nodeColors[category]}
            label={nodeCategoryLabels[category]}
            variant="node"
            onToggle={() => onToggleNodeCategory(category)}
          />
        ))}
      </LegendFilterGroup>
    </div>
  );
}

function LegendFilterGroup({
  children,
  itemCount,
  title,
}: {
  children: React.ReactNode;
  itemCount: number;
  title: string;
}): React.ReactElement {
  const [isOpen, setOpen] = useState(false);
  return (
    <section className="overflow-hidden rounded-md border border-[#303a49] bg-[#11151c]/45">
      <button
        type="button"
        className="flex h-8 w-full cursor-pointer items-center justify-between gap-3 border-0 bg-transparent px-2.5 text-left text-[11px] font-semibold tracking-[0.08em] text-[#9aa7b8] uppercase"
        aria-expanded={isOpen}
        onClick={() => setOpen((current) => !current)}
      >
        <span className="min-w-0 overflow-hidden text-ellipsis whitespace-nowrap">{title}</span>
        <span className="inline-flex shrink-0 items-center gap-2 text-[10px] tracking-normal text-[#8d98a8] normal-case">
          {itemCount}
          <span aria-hidden="true">{isOpen ? "-" : "+"}</span>
        </span>
      </button>
      {isOpen ? (
        <div className="grid grid-cols-[repeat(2,minmax(0,1fr))] gap-x-3 gap-y-1.5 border-t border-[#303a49] px-2.5 py-2 max-[560px]:grid-cols-1">
          {children}
        </div>
      ) : null}
    </section>
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
    <label
      className="flex min-w-0 cursor-pointer select-none items-center gap-2 text-[11px] text-[#d7dde8]"
      title={title}
    >
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
      <Detail label="Procedure" value={node.procedureKind} />
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

function graphDataFor(payload: AspGraphPayload | undefined): GraphData {
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

function filterGraphData(
  graphData: GraphData,
  hiddenNodeCategories: ReadonlySet<NodeColorCategory>,
  hiddenLinkCategories: ReadonlySet<LinkFilterCategory>,
): GraphData {
  const visibleNodes = graphData.nodes.filter((node) => !hiddenNodeCategories.has(node.category));
  const visibleNodeIds = new Set(visibleNodes.map((node) => node.id));
  const visibleLinks = graphData.links.filter((link) => {
    const sourceId = nodeIdForEndpoint(link.source);
    const targetId = nodeIdForEndpoint(link.target);
    return (
      visibleNodeIds.has(sourceId) &&
      visibleNodeIds.has(targetId) &&
      !hiddenLinkCategories.has(link.kind) &&
      !(link.role === "member" && hiddenLinkCategories.has("member"))
    );
  });
  const referenceCounts = graphReferenceCountsForGraphLinks(visibleLinks);
  for (const node of visibleNodes) {
    const referenceCount = referenceCounts.get(node.id) ?? 0;
    node.referenceCount = referenceCount;
    node.value = nodeValue(referenceCount);
  }
  return {
    nodes: visibleNodes,
    links: visibleLinks,
  };
}

function graphStatsFor(graphData: GraphData): AspGraphPayload["stats"] {
  const stats: AspGraphPayload["stats"] = {
    files: 0,
    declarations: 0,
    references: 0,
    calls: 0,
    unresolvedReferences: 0,
    includes: 0,
    missingIncludes: 0,
    nodes: graphData.nodes.length,
    links: graphData.links.length,
  };
  for (const node of graphData.nodes) {
    if (node.kind === "file") {
      stats.files += 1;
    } else if (node.kind === "vbDeclaration") {
      stats.declarations += 1;
    }
  }
  for (const link of graphData.links) {
    if (link.kind === "include") {
      stats.includes += 1;
      if (link.include?.exists === false) {
        stats.missingIncludes += 1;
      }
    } else if (link.kind === "references") {
      stats.references += 1;
    } else if (link.kind === "calls") {
      stats.calls += 1;
    } else if (link.kind === "unresolvedReference") {
      stats.unresolvedReferences += 1;
    }
  }
  return stats;
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

function graphReferenceCountsForGraphLinks(links: GraphLink[]): Map<string, number> {
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
    const targetId = nodeIdForEndpoint(link.target);
    counts.set(targetId, (counts.get(targetId) ?? 0) + link.count);
  }
  return counts;
}

function nodeLegendCategoriesFor(nodes: GraphNode[]): NodeColorCategory[] {
  const categories = new Set(nodes.map((node) => node.category));
  return nodeCategoryOrder.filter((category) => categories.has(category));
}

function linkLegendCategoriesFor(links: GraphLink[]): LinkFilterCategory[] {
  const categories = new Set<LinkFilterCategory>();
  for (const link of links) {
    categories.add(link.kind);
    if (link.role === "member") {
      categories.add("member");
    }
  }
  return linkFilterOrder.filter((category) => categories.has(category));
}

function toggledSet<T>(set: ReadonlySet<T>, value: T): Set<T> {
  const nextSet = new Set(set);
  if (nextSet.has(value)) {
    nextSet.delete(value);
  } else {
    nextSet.add(value);
  }
  return nextSet;
}

function isSelectionVisible(selection: Selection, graphData: GraphData): boolean {
  if (selection?.type === "node") {
    return graphData.nodes.some((node) => node.id === selection.item.id);
  }
  if (selection?.type === "link") {
    return graphData.links.some((link) => link.id === selection.item.id);
  }
  return true;
}

function highlightForSearch(
  query: string,
  matchCase: boolean,
  nodes: GraphNode[],
  links: GraphLink[],
): HighlightState | undefined {
  const normalizedQuery = normalizeSearchText(query.trim(), matchCase);
  if (!normalizedQuery) {
    return undefined;
  }
  const activeNodeIds = new Set(
    nodes
      .filter((node) => searchableNodeText(node, matchCase).includes(normalizedQuery))
      .map((node) => node.id),
  );
  const activeLinkIds = new Set(
    links
      .filter(
        (link) =>
          activeNodeIds.has(nodeIdForEndpoint(link.source)) &&
          activeNodeIds.has(nodeIdForEndpoint(link.target)),
      )
      .map((link) => link.id),
  );
  return { activeNodeIds, activeLinkIds };
}

function searchableNodeText(node: GraphNode, matchCase: boolean): string {
  return normalizeSearchText(node.label, matchCase);
}

function normalizeSearchText(value: string, matchCase: boolean): string {
  return matchCase ? value : value.toLowerCase();
}

function highlightForSelection(
  selection: Selection,
  links: GraphLink[],
): HighlightState | undefined {
  if (selection?.type !== "node") {
    return undefined;
  }
  const selectedNodeId = selection.item.id;
  const activeNodeIds = new Set<string>([selectedNodeId]);
  const activeLinkIds = new Set<string>();
  for (const link of links) {
    if (nodeIdForEndpoint(link.target) !== selectedNodeId) {
      continue;
    }
    const sourceNodeId = nodeIdForEndpoint(link.source);
    activeNodeIds.add(sourceNodeId);
    activeLinkIds.add(link.id);
  }
  return { activeNodeIds, activeLinkIds };
}

function nodeIdForEndpoint(endpoint: string | GraphNode): string {
  return typeof endpoint === "string" ? endpoint : endpoint.id;
}

function isActiveNode(node: GraphNode, highlight: HighlightState | undefined): boolean {
  return !highlight || highlight.activeNodeIds.has(node.id);
}

function isActiveLink(link: GraphLink, highlight: HighlightState | undefined): boolean {
  return !highlight || highlight.activeLinkIds.has(link.id);
}

function nodeColor(node: GraphNode, highlight: HighlightState | undefined): string {
  return isActiveNode(node, highlight) ? node.color : "#2d3542";
}

function linkColor(link: GraphLink, highlight: HighlightState | undefined): string {
  return isActiveLink(link, highlight) ? link.color : "#29313d";
}

function nodeTextObject(node: GraphNode, highlight: HighlightState | undefined): SpriteText {
  const offset = nodeTextOffset(node);
  const textHeight = nodeTextHeight(node);
  const sprite = new SpriteText(node.label, textHeight, nodeColor(node, highlight));
  sprite.fontFace = "system-ui, sans-serif";
  sprite.fontWeight = nodeTextFontWeight(node);
  sprite.backgroundColor = false;
  sprite.padding = 0.5;
  (sprite as CenteredSpriteText).center.y = nodeTextAnchor(offset, textHeight);
  return sprite;
}

function nodeTextHeight(node: GraphNode): number {
  return node.kind === "file" ? 5 : 4;
}

function nodeTextOffset(node: GraphNode): number {
  return nodeRadius(node) + (node.kind === "file" ? 5 : 3.5);
}

function nodeTextAnchor(offset: number, textHeight: number): number {
  return -offset / textHeight;
}

function nodeTextFontWeight(node: GraphNode): "500" | "600" {
  return node.kind === "file" ? "600" : "500";
}

function nodeValue(referenceCount: number): number {
  const scale = Math.sqrt(
    clamp(referenceCount, 0, maximumNodeScaleReferenceCount) / maximumNodeScaleReferenceCount,
  );
  return minimumNodeValue + scale * (maximumNodeValue - minimumNodeValue);
}

function linkArrowLength(link: GraphLink): number {
  return link.kind === "include" ? 6 : 3.5;
}

function linkWidth2d(link: GraphLink, highlight: HighlightState | undefined): number {
  const width = clamp(0.8 + Math.log2(link.count + 1) * 0.4, 0.8, 3);
  const visibleWidth = link.kind === "include" ? Math.max(3.5, width + 1.5) : width;
  return isActiveLink(link, highlight) ? visibleWidth : Math.max(0.35, visibleWidth * 0.35);
}

function linkWidth3d(link: GraphLink, highlight: HighlightState | undefined): number {
  if (!isActiveLink(link, highlight)) {
    return 0.05;
  }
  return link.kind === "include" ? 1.5 : 0;
}

function linkParticleCount(link: GraphLink, highlight: HighlightState | undefined): number {
  if (!isActiveLink(link, highlight)) {
    return 0;
  }
  return clamp(Math.ceil(Math.sqrt(link.count)), 1, 8);
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
    return node.role === "member" ? "member" : "unresolved";
  }
  if (node.externalKind === "member") {
    return "member";
  }
  switch (node.declarationKind) {
    case "function":
      return "function";
    case "sub":
      return "sub";
    case "class":
      return "class";
    case "method":
      if (node.procedureKind === "function") {
        return "methodFunction";
      }
      if (node.procedureKind === "sub") {
        return "methodSub";
      }
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
      return "function";
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

function paintNode(
  node: GraphNode,
  canvas: CanvasRenderingContext2D,
  highlight: HighlightState | undefined,
): void {
  const radius = nodeRadius(node);
  const active = isActiveNode(node, highlight);
  canvas.beginPath();
  canvas.arc(node.x ?? 0, node.y ?? 0, radius, 0, 2 * Math.PI, false);
  canvas.fillStyle = active ? node.color : "#2d3542";
  canvas.fill();
  const offset = nodeTextOffset(node);
  canvas.save();
  canvas.font = `${nodeTextFontWeight(node)} ${nodeTextHeight(node)}px system-ui, sans-serif`;
  canvas.fillStyle = nodeColor(node, highlight);
  canvas.textAlign = "center";
  canvas.textBaseline = "bottom";
  canvas.fillText(node.label, node.x ?? 0, (node.y ?? 0) - offset);
  canvas.restore();
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
  return (node.kind === "file" ? 3.4 : 3) + node.value;
}

createRoot(document.getElementById("root") ?? document.body).render(<App />);
