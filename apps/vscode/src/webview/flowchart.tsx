import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import mermaid from "mermaid";
import tailwindStyles from "./flowchart.css?inline";
import type {
  AspFlowchartInclude,
  AspFlowchartNode,
  AspFlowchartPayload,
  AspFlowchartSection,
  AspFlowchartTarget,
} from "@asp-lsp/core";

declare const acquireVsCodeApi: () => {
  postMessage(message: unknown): void;
};

declare global {
  interface Window {
    __ASP_LSP_FLOWCHART__?: FlowchartPayload;
  }
}

type FlowchartLocale = "en" | "ja";

interface FlowchartPayload extends AspFlowchartPayload {
  locale?: FlowchartLocale;
}

const vscode = acquireVsCodeApi();

const flowchartLabelLineLength = 28;
const flowchartEdgeLabelLineLength = 22;
const maximumFlowchartLabelCharacters = 180;
const maximumFlowchartEdgeLabelCharacters = 80;
const minimumFlowchartZoom = 0.4;
const maximumFlowchartZoom = 2.5;
const flowchartZoomStep = 0.1;

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

const messages: Record<FlowchartLocale, Record<string, string>> = {
  en: {
    title: "ASP Flowchart",
    includes: "Includes",
    flowcharts: "Flowcharts",
    nodes: "Nodes",
    mermaid: "Mermaid",
    emptyIncludes: "No includes found.",
    emptyNodes: "No VBScript flow nodes found.",
    missing: "missing",
    openDirective: "Open directive",
    openFlowchart: "Open flowchart",
    openCode: "Open code",
    openDefinition: "Open definition",
    renderError: "Mermaid render failed.",
    selectFlowchart: "Select flowchart",
    emptySection: "Empty",
    sections: "Sections",
    definitions: "Definitions",
    copyMermaid: "Copy Mermaid",
    exportMermaid: "Export Mermaid",
    exportSvg: "Export SVG",
    searchNodes: "Search nodes",
    searchPlaceholder: "Search",
    searchPrevious: "Previous match",
    searchNext: "Next match",
    zoomOut: "Zoom out",
    zoomIn: "Zoom in",
    resetZoom: "Reset zoom",
    zoomWithWheel: "Hold Ctrl or Command and use the mouse wheel to zoom.",
  },
  ja: {
    title: "ASP Flowchart",
    includes: "参照ファイル",
    flowcharts: "フローチャート",
    nodes: "ノード",
    mermaid: "Mermaid",
    emptyIncludes: "include は見つかりません。",
    emptyNodes: "VBScript flow node は見つかりません。",
    missing: "missing",
    openDirective: "directive を開く",
    openFlowchart: "フローチャートを開く",
    openCode: "コードを開く",
    openDefinition: "定義を開く",
    renderError: "Mermaid render に失敗しました。",
    selectFlowchart: "フローチャートを選択",
    emptySection: "空です",
    sections: "セクション",
    definitions: "定義",
    copyMermaid: "Mermaid コピー",
    exportMermaid: "Mermaid 出力",
    exportSvg: "SVG 出力",
    searchNodes: "ノード検索",
    searchPlaceholder: "検索",
    searchPrevious: "前の一致",
    searchNext: "次の一致",
    zoomOut: "縮小",
    zoomIn: "拡大",
    resetZoom: "ズームをリセット",
    zoomWithWheel: "Ctrl または Command を押しながらホイールでズーム",
  },
};

function App(): React.ReactElement {
  const initialPayload = window.__ASP_LSP_FLOWCHART__ ?? fallbackPayload;
  const [payload, setPayload] = useState<FlowchartPayload>(initialPayload);
  const [selectedSectionId, setSelectedSectionId] = useState<string | undefined>(() =>
    defaultSectionId(initialPayload),
  );
  const [searchQuery, setSearchQuery] = useState("");
  const [activeSearchIndex, setActiveSearchIndex] = useState(0);
  const searchInputRef = useRef<HTMLInputElement | null>(null);
  const locale = payload.locale ?? "en";
  const text = (key: string): string => messages[locale][key] ?? messages.en[key] ?? key;
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
  const selectedFlowchart = useMemo(
    () => flowchartForSection(payload, selectedSectionId),
    [payload, selectedSectionId],
  );
  const selectedSection = selectedFlowchart.sections[0];
  const openFlowchartForNode = useCallback(
    (node: AspFlowchartNode) => {
      const target = node.links?.[0]?.target;
      if (target) {
        openFlowchartTarget(payload, target, setSelectedSectionId);
      } else {
        setSelectedSectionId(sectionIdForNodeFlowchart(payload, node));
      }
    },
    [payload],
  );
  const openTarget = useCallback(
    (target: AspFlowchartTarget) => openFlowchartTarget(payload, target, setSelectedSectionId),
    [payload],
  );
  const selectSearchMatch = useCallback(
    (index: number) => {
      if (searchMatches.length === 0) {
        return;
      }
      const nextIndex = modulo(index, searchMatches.length);
      setActiveSearchIndex(nextIndex);
      setSelectedSectionId(searchMatches[nextIndex]?.node.sectionId);
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
      }
    },
    [activeSearchIndex, selectSearchMatch],
  );
  useEffect(() => {
    const listener = (event: MessageEvent) => {
      const message = event.data as { type?: unknown; payload?: unknown; targetRange?: unknown };
      if (message.type === "flowchartPayload" && isFlowchartPayload(message.payload)) {
        setPayload(message.payload);
        setSelectedSectionId(
          isRange(message.targetRange)
            ? (sectionIdForRange(message.payload, message.targetRange) ??
                defaultSectionId(message.payload))
            : defaultSectionId(message.payload),
        );
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
    setSelectedSectionId(
      searchMatches[Math.min(activeSearchIndex, searchMatches.length - 1)].node.sectionId,
    );
  }, [activeSearchIndex, searchMatches]);
  return (
    <main className="grid h-full grid-cols-[minmax(320px,380px)_1fr] bg-[#101419] text-[#d9e0ea]">
      <aside className="flex min-h-0 flex-col border-r border-[#263140] bg-[#151b23]">
        <header className="border-b border-[#263140] px-4 py-3">
          <div className="text-sm font-semibold text-[#f1f5f9]">
            {selectedSection?.label ?? payload.fileName ?? text("title")}
          </div>
          <div className="mt-1 text-xs text-[#9fb0c5]">
            {payload.stats.sections} {text("sections")} / {selectedFlowchart.stats.nodes}{" "}
            {text("nodes")} / {payload.includes.length} {text("includes")}
          </div>
        </header>
        <div className="border-b border-[#263140] px-3 py-2">
          <div className="flex items-center gap-1">
            <input
              ref={searchInputRef}
              aria-label={text("searchNodes")}
              className="h-7 min-w-0 flex-1 rounded border border-[#334255] bg-[#0c1117] px-2 text-xs text-[#d9e0ea] outline-none placeholder:text-[#6f7e91] focus:border-[#7dd3fc]"
              placeholder={text("searchPlaceholder")}
              type="search"
              value={searchQuery}
              onChange={(event) => setSearchQuery(event.target.value)}
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
          <SectionHeading>{text("includes")}</SectionHeading>
          <IncludeList includes={payload.includes} text={text} uri={payload.uri} />
          <SectionHeading>{text("flowcharts")}</SectionHeading>
          {payload.sections.length === 0 ? (
            <EmptyText>{text("emptyNodes")}</EmptyText>
          ) : (
            payload.sections.map((section) => (
              <FlowSection
                key={section.id}
                nodes={nodesBySection.get(section.id) ?? []}
                selected={section.id === selectedSection?.id}
                section={section}
                text={text}
                activeSearchNodeId={activeSearchNode?.id}
                matchedNodeIds={matchedNodeIds}
                onOpenCode={(range) =>
                  range && vscode.postMessage({ type: "openRange", uri: payload.uri, range })
                }
                onOpenFlowchart={openFlowchartForNode}
                onOpenTarget={openTarget}
                onSelect={() => setSelectedSectionId(section.id)}
              />
            ))
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
      <FlowchartCanvas
        payload={selectedFlowchart}
        section={selectedSection}
        text={text}
        activeSearchNodeId={activeSearchNode?.id}
        matchedNodeIds={matchedNodeIds}
        onOpenCode={(range) =>
          range && vscode.postMessage({ type: "openRange", uri: payload.uri, range })
        }
        onOpenFlowchart={openFlowchartForNode}
      />
    </main>
  );
}

function IncludeList({
  includes,
  text,
  uri,
}: {
  includes: AspFlowchartInclude[];
  text(key: string): string;
  uri: string;
}): React.ReactElement {
  if (includes.length === 0) {
    return <EmptyText>{text("emptyIncludes")}</EmptyText>;
  }
  return (
    <div className="mb-4 grid gap-2">
      {includes.map((include, index) => (
        <div
          key={`${include.mode}:${include.path}:${index}`}
          className="rounded border border-[#263140] bg-[#101820] p-2"
        >
          <div className="flex items-start justify-between gap-2">
            <button
              className="min-w-0 flex-1 text-left text-sm font-medium text-[#8ec7ff] hover:underline disabled:cursor-not-allowed disabled:text-[#7b8796] disabled:no-underline"
              disabled={!include.exists || !include.resolvedUri}
              title={text("openFlowchart")}
              type="button"
              onClick={() =>
                include.resolvedUri &&
                vscode.postMessage({ type: "openIncludeFlowchart", uri: include.resolvedUri })
              }
            >
              <span className="block truncate">{include.path}</span>
            </button>
            {include.exists === false ? (
              <span className="text-xs text-[#ffb4a8]">{text("missing")}</span>
            ) : null}
          </div>
          <div className="mt-1 flex items-center justify-between gap-2 text-xs text-[#9fb0c5]">
            <span>{include.mode}</span>
            <button
              className="text-[#c4d4e8] hover:text-white hover:underline"
              type="button"
              onClick={() => vscode.postMessage({ type: "openRange", uri, range: include.range })}
            >
              {text("openDirective")}
            </button>
          </div>
          {include.actualPath ? (
            <div className="mt-1 truncate text-xs text-[#7d8ca1]">{include.actualPath}</div>
          ) : null}
        </div>
      ))}
    </div>
  );
}

function FlowSection({
  nodes,
  selected,
  section,
  text,
  activeSearchNodeId,
  matchedNodeIds,
  onOpenCode,
  onOpenFlowchart,
  onOpenTarget,
  onSelect,
}: {
  nodes: AspFlowchartNode[];
  selected: boolean;
  section: AspFlowchartSection;
  text(key: string): string;
  activeSearchNodeId?: string;
  matchedNodeIds: Set<string>;
  onOpenCode(range: AspFlowchartNode["range"] | AspFlowchartSection["range"]): void;
  onOpenFlowchart(node: AspFlowchartNode): void;
  onOpenTarget(target: AspFlowchartTarget): void;
  onSelect(): void;
}): React.ReactElement {
  const visibleNodes = nodes.filter((node) => node.kind !== "start" && node.kind !== "end");
  return (
    <div
      className={`mb-3 rounded border bg-[#101820] ${
        selected ? "border-[#6fb6ff]" : "border-[#263140]"
      }`}
    >
      <div className="flex items-center gap-2 border-b border-[#263140] px-2 py-1.5">
        <button
          className="min-w-0 flex-1 truncate text-left text-xs font-semibold uppercase tracking-wide text-[#9fb0c5] hover:text-[#f1f5f9]"
          title={text("selectFlowchart")}
          type="button"
          onClick={onSelect}
        >
          {section.label}
        </button>
        <button
          className="shrink-0 rounded border border-[#334255] px-2 py-0.5 text-[11px] text-[#c4d4e8] hover:border-[#6fb6ff] hover:text-white disabled:cursor-not-allowed disabled:border-[#263140] disabled:text-[#5f6d7e]"
          disabled={!section.range}
          title={text("openCode")}
          type="button"
          onClick={() => section.range && onOpenCode(section.range)}
        >
          Code
        </button>
      </div>
      <div className="grid gap-1 p-2">
        {visibleNodes.length === 0 ? (
          <EmptyText>{text("emptySection")}</EmptyText>
        ) : (
          visibleNodes.map((node) => {
            const isSearchMatch = matchedNodeIds.has(node.id);
            const isActiveSearchMatch = activeSearchNodeId === node.id;
            return (
              <div
                key={node.id}
                className={`rounded px-1 py-1 hover:bg-[#223044] ${
                  isActiveSearchMatch
                    ? "bg-[#17324a] ring-1 ring-[#7dd3fc]"
                    : isSearchMatch
                      ? "bg-[#2b2b18] ring-1 ring-[#f6c177]"
                      : ""
                }`}
              >
                <div className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-1">
                  <button
                    className="min-w-0 truncate px-1 py-1 text-left text-xs text-[#d9e0ea]"
                    title={text("openFlowchart")}
                    type="button"
                    onClick={() => onOpenFlowchart(node)}
                  >
                    <span className="mr-2 text-[#7dd3fc]">{node.kind}</span>
                    {node.label}
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
                </div>
                {node.links?.length ? (
                  <div className="ml-1 mt-1 flex flex-wrap gap-1">
                    {node.links.map((link) => (
                      <button
                        key={link.id}
                        className="rounded border border-[#315f58] bg-[#10241f] px-1.5 py-0.5 text-[11px] text-[#9ee7d0] hover:border-[#63e6be] hover:text-white"
                        title={text("openDefinition")}
                        type="button"
                        onClick={() => onOpenTarget(link.target)}
                      >
                        {link.label}
                      </button>
                    ))}
                  </div>
                ) : null}
              </div>
            );
          })
        )}
      </div>
    </div>
  );
}

function FlowchartCanvas({
  payload,
  section,
  text,
  activeSearchNodeId,
  matchedNodeIds,
  onOpenCode,
  onOpenFlowchart,
}: {
  payload: FlowchartPayload;
  section: AspFlowchartSection | undefined;
  text(key: string): string;
  activeSearchNodeId?: string;
  matchedNodeIds: Set<string>;
  onOpenCode(range: AspFlowchartNode["range"] | AspFlowchartSection["range"]): void;
  onOpenFlowchart(node: AspFlowchartNode): void;
}): React.ReactElement {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const [error, setError] = useState<string>();
  const [svg, setSvg] = useState<string>("");
  const [zoom, setZoom] = useState(1);
  const setClampedZoom = useCallback(
    (value: number) =>
      setZoom(Math.round(clamp(value, minimumFlowchartZoom, maximumFlowchartZoom) * 100) / 100),
    [],
  );
  const adjustZoom = useCallback(
    (direction: 1 | -1) => setClampedZoom(zoom + direction * flowchartZoomStep),
    [setClampedZoom, zoom],
  );
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
  useEffect(() => {
    let cancelled = false;
    const render = async (): Promise<void> => {
      if (!containerRef.current) {
        return;
      }
      mermaid.initialize({
        startOnLoad: false,
        maxTextSize: 2_000_000,
        securityLevel: "strict",
        theme: "dark",
        flowchart: { htmlLabels: false, curve: "basis" },
      });
      try {
        const id = `asp-lsp-flowchart-${Date.now().toString(36)}`;
        const result = await mermaid.render(id, payload.mermaid || "flowchart TB");
        if (cancelled || !containerRef.current) {
          return;
        }
        containerRef.current.innerHTML = result.svg;
        setSvg(result.svg);
        attachSvgNodeHandlers(containerRef.current, payload, onOpenFlowchart);
        setError(undefined);
      } catch (renderError) {
        if (!cancelled) {
          setError(renderError instanceof Error ? renderError.message : String(renderError));
          setSvg("");
        }
      }
    };
    void render();
    return () => {
      cancelled = true;
    };
  }, [onOpenFlowchart, payload]);
  useEffect(() => {
    if (!containerRef.current) {
      return;
    }
    syncSvgSearchHighlights(containerRef.current, payload, matchedNodeIds, activeSearchNodeId);
  }, [activeSearchNodeId, matchedNodeIds, payload, svg]);
  return (
    <section className="grid min-h-0 grid-rows-[auto_1fr] overflow-hidden bg-[#0d1117]">
      <header className="flex items-center gap-2 border-b border-[#263140] px-5 py-3">
        <div className="min-w-0 flex-1 truncate text-sm font-semibold text-[#f1f5f9]">
          {section?.label ?? text("title")}
        </div>
        <div
          className="flex items-center overflow-hidden rounded border border-[#3b4a5f]"
          title={text("zoomWithWheel")}
        >
          <button
            className="h-7 min-w-7 border-r border-[#3b4a5f] px-2 text-xs text-[#c4d4e8] hover:bg-[#172131] hover:text-white"
            title={text("zoomOut")}
            type="button"
            onClick={() => adjustZoom(-1)}
          >
            -
          </button>
          <button
            className="h-7 min-w-[52px] border-r border-[#3b4a5f] px-2 text-xs text-[#c4d4e8] hover:bg-[#172131] hover:text-white"
            title={text("resetZoom")}
            type="button"
            onClick={() => setClampedZoom(1)}
          >
            {Math.round(zoom * 100)}%
          </button>
          <button
            className="h-7 min-w-7 px-2 text-xs text-[#c4d4e8] hover:bg-[#172131] hover:text-white"
            title={text("zoomIn")}
            type="button"
            onClick={() => adjustZoom(1)}
          >
            +
          </button>
        </div>
        <button
          className="rounded border border-[#3b4a5f] px-3 py-1 text-xs text-[#c4d4e8] hover:border-[#7dd3fc] hover:text-white disabled:cursor-not-allowed disabled:border-[#263140] disabled:text-[#5f6d7e]"
          disabled={!section?.range}
          title={text("openCode")}
          type="button"
          onClick={() => section?.range && onOpenCode(section.range)}
        >
          Code
        </button>
        <button
          className="rounded border border-[#3b4a5f] px-3 py-1 text-xs text-[#c4d4e8] hover:border-[#63e6be] hover:text-white"
          type="button"
          onClick={() =>
            vscode.postMessage({
              type: "copyText",
              content: payload.mermaid,
            })
          }
        >
          {text("copyMermaid")}
        </button>
        <button
          className="rounded border border-[#3b4a5f] px-3 py-1 text-xs text-[#c4d4e8] hover:border-[#63e6be] hover:text-white"
          type="button"
          onClick={() =>
            vscode.postMessage({
              type: "exportFlowchart",
              format: "mermaid",
              uri: payload.uri,
              sectionLabel: section?.label,
              content: `${payload.mermaid}\n`,
            })
          }
        >
          {text("exportMermaid")}
        </button>
        <button
          className="rounded border border-[#3b4a5f] px-3 py-1 text-xs text-[#c4d4e8] hover:border-[#f6c177] hover:text-white disabled:cursor-not-allowed disabled:border-[#263140] disabled:text-[#5f6d7e]"
          disabled={!svg}
          type="button"
          onClick={() =>
            vscode.postMessage({
              type: "exportFlowchart",
              format: "svg",
              uri: payload.uri,
              sectionLabel: section?.label,
              content: svg,
            })
          }
        >
          {text("exportSvg")}
        </button>
      </header>
      <div className="min-h-0 overflow-auto p-5" onWheel={handleWheel}>
        {error ? (
          <div className="rounded border border-[#7f3434] bg-[#291416] p-3 text-sm text-[#ffd2cc]">
            {text("renderError")} {error}
          </div>
        ) : null}
        <div
          ref={containerRef}
          className="min-h-full min-w-full overflow-auto [&_svg]:h-auto [&_svg]:max-w-none"
          style={{ zoom }}
        />
      </div>
    </section>
  );
}

function attachSvgNodeHandlers(
  container: HTMLDivElement,
  payload: FlowchartPayload,
  onOpenFlowchart: (node: AspFlowchartNode) => void,
): void {
  for (const node of payload.nodes) {
    for (const element of svgElementsForFlowchartNode(container, node)) {
      element.style.cursor = "pointer";
      element.addEventListener("click", () => onOpenFlowchart(node));
    }
  }
}

function syncSvgSearchHighlights(
  container: HTMLDivElement,
  payload: FlowchartPayload,
  matchedNodeIds: Set<string>,
  activeNodeId: string | undefined,
): void {
  for (const element of container.querySelectorAll<SVGGElement>(
    ".asp-lsp-flowchart-match, .asp-lsp-flowchart-active",
  )) {
    element.classList.remove("asp-lsp-flowchart-match", "asp-lsp-flowchart-active");
  }
  let activeElement: SVGGElement | undefined;
  for (const node of payload.nodes) {
    if (!matchedNodeIds.has(node.id) && node.id !== activeNodeId) {
      continue;
    }
    for (const element of svgElementsForFlowchartNode(container, node)) {
      if (matchedNodeIds.has(node.id)) {
        element.classList.add("asp-lsp-flowchart-match");
      }
      if (node.id === activeNodeId) {
        element.classList.add("asp-lsp-flowchart-active");
        activeElement = element;
      }
    }
  }
  activeElement?.scrollIntoView({ block: "center", inline: "center" });
}

function svgElementsForFlowchartNode(
  container: HTMLDivElement,
  node: AspFlowchartNode,
): SVGGElement[] {
  const id = mermaidId(node.id);
  return [...container.querySelectorAll<SVGGElement>(`[id*="${id}"]`)];
}

interface FlowchartSearchMatch {
  node: AspFlowchartNode;
}

function flowchartSearchMatches(payload: FlowchartPayload, query: string): FlowchartSearchMatch[] {
  const normalizedQuery = normalizeFlowchartSearchText(query);
  if (!normalizedQuery) {
    return [];
  }
  const sectionsById = new Map(payload.sections.map((section) => [section.id, section]));
  return payload.nodes
    .filter((node) => node.kind !== "start" && node.kind !== "end")
    .filter((node) => {
      const section = sectionsById.get(node.sectionId);
      return normalizeFlowchartSearchText(
        `${node.kind} ${node.label} ${section?.label ?? ""}`,
      ).includes(normalizedQuery);
    })
    .map((node) => ({ node }));
}

function normalizeFlowchartSearchText(value: string): string {
  return value.trim().toLocaleLowerCase();
}

function defaultSectionId(payload: FlowchartPayload): string | undefined {
  return (
    payload.sections.find((section) =>
      section.nodeIds.some((nodeId) => {
        const node = payload.nodes.find((candidate) => candidate.id === nodeId);
        return node && node.kind !== "start" && node.kind !== "end";
      }),
    ) ?? payload.sections[0]
  )?.id;
}

function flowchartForSection(
  payload: FlowchartPayload,
  selectedSectionId: string | undefined,
): FlowchartPayload {
  const section =
    payload.sections.find((candidate) => candidate.id === selectedSectionId) ??
    payload.sections.find((candidate) => candidate.id === defaultSectionId(payload));
  if (!section) {
    return { ...payload, sections: [], nodes: [], edges: [], mermaid: "flowchart TB" };
  }
  const nodeIds = new Set(section.nodeIds);
  const nodes = payload.nodes.filter((node) => nodeIds.has(node.id));
  const edges = payload.edges.filter(
    (edge) => edge.sectionId === section.id && nodeIds.has(edge.source) && nodeIds.has(edge.target),
  );
  return {
    ...payload,
    sections: [section],
    nodes,
    edges,
    mermaid: mermaidForSelectedSection(nodes, edges),
    stats: {
      ...payload.stats,
      sections: 1,
      nodes: nodes.length,
      edges: edges.length,
    },
  };
}

function mermaidForSelectedSection(
  nodes: AspFlowchartNode[],
  edges: FlowchartPayload["edges"],
): string {
  const lines = ["flowchart TB"];
  for (const node of nodes) {
    lines.push(`  ${mermaidNode(node)}`);
  }
  for (const edge of edges) {
    lines.push(
      `  ${mermaidId(edge.source)} -->${edge.label ? `|${escapeMermaidEdgeLabel(edge.label)}|` : ""} ${mermaidId(edge.target)}`,
    );
  }
  return lines.join("\n");
}

function mermaidNode(node: AspFlowchartNode): string {
  const id = mermaidId(node.id);
  const label = mermaidLabel(node.label);
  if (node.kind === "start" || node.kind === "end") {
    return `${id}(["${label}"])`;
  }
  if (
    node.kind === "if" ||
    node.kind === "elseif" ||
    node.kind === "select" ||
    node.kind === "case"
  ) {
    return `${id}{"${label}"}`;
  }
  return `${id}["${label}"]`;
}

function mermaidId(id: string): string {
  return id.replace(/[^A-Za-z0-9_]/g, "_");
}

function escapeMermaidText(value: string): string {
  return value
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("[", "&#91;")
    .replaceAll("]", "&#93;")
    .replaceAll("{", "&#123;")
    .replaceAll("}", "&#125;")
    .trim();
}

function escapeMermaidEdgeLabel(value: string): string {
  return mermaidLabel(value, {
    lineLength: flowchartEdgeLabelLineLength,
    maximumCharacters: maximumFlowchartEdgeLabelCharacters,
  }).replaceAll("|", "/");
}

function mermaidLabel(
  value: string,
  options: { lineLength?: number; maximumCharacters?: number } = {},
): string {
  const normalized = value.replace(/\s+/g, " ").trim();
  const clipped = clipFlowchartLabel(
    normalized,
    options.maximumCharacters ?? maximumFlowchartLabelCharacters,
  );
  const lines = wrapFlowchartLabel(clipped, options.lineLength ?? flowchartLabelLineLength);
  return (lines.length > 0 ? lines : [""]).map(escapeMermaidText).join("<br/>");
}

function clipFlowchartLabel(value: string, maximumCharacters: number): string {
  const characters = Array.from(value);
  if (characters.length <= maximumCharacters) {
    return value;
  }
  return `${characters.slice(0, Math.max(0, maximumCharacters - 3)).join("")}...`;
}

function wrapFlowchartLabel(value: string, lineLength: number): string[] {
  const lines: string[] = [];
  let current = "";
  for (const word of value.split(" ")) {
    if (!word) {
      continue;
    }
    for (const part of flowchartWordParts(word, lineLength)) {
      if (!current) {
        current = part;
        continue;
      }
      const next = `${current} ${part}`;
      if (Array.from(next).length <= lineLength) {
        current = next;
      } else {
        lines.push(current);
        current = part;
      }
    }
  }
  if (current) {
    lines.push(current);
  }
  return lines;
}

function flowchartWordParts(value: string, lineLength: number): string[] {
  const characters = Array.from(value);
  if (characters.length <= lineLength) {
    return [value];
  }
  const parts: string[] = [];
  for (let index = 0; index < characters.length; index += lineLength) {
    parts.push(characters.slice(index, index + lineLength).join(""));
  }
  return parts;
}

function clamp(value: number, minimum: number, maximum: number): number {
  return Math.min(maximum, Math.max(minimum, value));
}

function modulo(value: number, divisor: number): number {
  return ((value % divisor) + divisor) % divisor;
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
): void {
  const targetRange = target.nameRange ?? target.range;
  if (target.uri && target.uri !== payload.uri) {
    vscode.postMessage({
      type: "openFlowchartLocation",
      uri: target.uri,
      range: targetRange,
    });
    return;
  }
  setSelectedSectionId(
    targetRange
      ? (sectionIdForRange(payload, targetRange) ?? defaultSectionId(payload))
      : defaultSectionId(payload),
  );
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

function rangeContains(
  outer: NonNullable<AspFlowchartNode["range"]>,
  inner: NonNullable<AspFlowchartTarget["range"]>,
): boolean {
  return (
    positionBeforeOrEqual(outer.start, inner.start) && positionBeforeOrEqual(inner.end, outer.end)
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

const style = document.createElement("style");
style.textContent = tailwindStyles;
document.head.append(style);

createRoot(document.getElementById("root")!).render(<App />);
