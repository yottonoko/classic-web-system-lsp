import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import mermaid from "mermaid";
import tailwindStyles from "./flowchart.css?inline";
import type {
  AspFlowchartInclude,
  AspFlowchartNode,
  AspFlowchartPayload,
  AspFlowchartSection,
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
    renderError: "Mermaid render failed.",
    selectFlowchart: "Select flowchart",
    emptySection: "Empty",
    sections: "Sections",
  },
  ja: {
    title: "ASP Flowchart",
    includes: "Includes",
    flowcharts: "フローチャート",
    nodes: "Nodes",
    mermaid: "Mermaid",
    emptyIncludes: "include は見つかりません。",
    emptyNodes: "VBScript flow node は見つかりません。",
    missing: "missing",
    openDirective: "directive を開く",
    openFlowchart: "flowchart を開く",
    openCode: "code を開く",
    renderError: "Mermaid render に失敗しました。",
    selectFlowchart: "flowchart を選択",
    emptySection: "空です",
    sections: "Sections",
  },
};

function App(): React.ReactElement {
  const initialPayload = window.__ASP_LSP_FLOWCHART__ ?? fallbackPayload;
  const [payload, setPayload] = useState<FlowchartPayload>(initialPayload);
  const [selectedSectionId, setSelectedSectionId] = useState<string | undefined>(() =>
    defaultSectionId(initialPayload),
  );
  const locale = payload.locale ?? "en";
  const text = (key: string): string => messages[locale][key] ?? messages.en[key] ?? key;
  const nodesBySection = useMemo(() => nodesBySectionId(payload), [payload]);
  const selectedFlowchart = useMemo(
    () => flowchartForSection(payload, selectedSectionId),
    [payload, selectedSectionId],
  );
  const selectedSection = selectedFlowchart.sections[0];
  const openFlowchartForNode = useCallback(
    (node: AspFlowchartNode) => {
      setSelectedSectionId(sectionIdForNodeFlowchart(payload, node));
    },
    [payload],
  );
  useEffect(() => {
    const listener = (event: MessageEvent) => {
      const message = event.data as { type?: unknown; payload?: unknown };
      if (message.type === "flowchartPayload" && isFlowchartPayload(message.payload)) {
        setPayload(message.payload);
        setSelectedSectionId(defaultSectionId(message.payload));
      }
    };
    window.addEventListener("message", listener);
    return () => window.removeEventListener("message", listener);
  }, []);
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
                onOpenCode={(range) =>
                  range && vscode.postMessage({ type: "openRange", uri: payload.uri, range })
                }
                onOpenFlowchart={openFlowchartForNode}
                onSelect={() => setSelectedSectionId(section.id)}
              />
            ))
          )}
          <SectionHeading>{text("mermaid")}</SectionHeading>
          <pre className="max-h-52 overflow-auto rounded border border-[#263140] bg-[#0c1117] p-2 text-xs leading-5 text-[#b9c5d6]">
            {selectedFlowchart.mermaid}
          </pre>
        </div>
      </aside>
      <FlowchartCanvas
        payload={selectedFlowchart}
        section={selectedSection}
        text={text}
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
  onOpenCode,
  onOpenFlowchart,
  onSelect,
}: {
  nodes: AspFlowchartNode[];
  selected: boolean;
  section: AspFlowchartSection;
  text(key: string): string;
  onOpenCode(range: AspFlowchartNode["range"] | AspFlowchartSection["range"]): void;
  onOpenFlowchart(node: AspFlowchartNode): void;
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
          visibleNodes.map((node) => (
            <div
              key={node.id}
              className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-1 rounded hover:bg-[#223044]"
            >
              <button
                className="min-w-0 truncate px-2 py-1 text-left text-xs text-[#d9e0ea]"
                title={text("openFlowchart")}
                type="button"
                onClick={() => onOpenFlowchart(node)}
              >
                <span className="mr-2 text-[#91c7ff]">{node.kind}</span>
                {node.label}
              </button>
              <button
                className="mr-1 shrink-0 rounded border border-[#334255] px-1.5 py-0.5 text-[11px] text-[#c4d4e8] hover:border-[#6fb6ff] hover:text-white disabled:cursor-not-allowed disabled:border-[#263140] disabled:text-[#5f6d7e]"
                disabled={!node.range}
                title={text("openCode")}
                type="button"
                onClick={() => node.range && onOpenCode(node.range)}
              >
                Code
              </button>
            </div>
          ))
        )}
      </div>
    </div>
  );
}

function FlowchartCanvas({
  payload,
  section,
  text,
  onOpenCode,
  onOpenFlowchart,
}: {
  payload: FlowchartPayload;
  section: AspFlowchartSection | undefined;
  text(key: string): string;
  onOpenCode(range: AspFlowchartNode["range"] | AspFlowchartSection["range"]): void;
  onOpenFlowchart(node: AspFlowchartNode): void;
}): React.ReactElement {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const [error, setError] = useState<string>();
  useEffect(() => {
    let cancelled = false;
    const render = async (): Promise<void> => {
      if (!containerRef.current) {
        return;
      }
      mermaid.initialize({
        startOnLoad: false,
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
        attachSvgNodeHandlers(containerRef.current, payload, onOpenFlowchart);
        setError(undefined);
      } catch (renderError) {
        if (!cancelled) {
          setError(renderError instanceof Error ? renderError.message : String(renderError));
        }
      }
    };
    void render();
    return () => {
      cancelled = true;
    };
  }, [onOpenFlowchart, payload]);
  return (
    <section className="grid min-h-0 grid-rows-[auto_1fr] overflow-hidden bg-[#0d1117]">
      <header className="flex items-center gap-2 border-b border-[#263140] px-5 py-3">
        <div className="min-w-0 flex-1 truncate text-sm font-semibold text-[#f1f5f9]">
          {section?.label ?? text("title")}
        </div>
        <button
          className="rounded border border-[#334255] px-3 py-1 text-xs text-[#c4d4e8] hover:border-[#6fb6ff] hover:text-white disabled:cursor-not-allowed disabled:border-[#263140] disabled:text-[#5f6d7e]"
          disabled={!section?.range}
          title={text("openCode")}
          type="button"
          onClick={() => section?.range && onOpenCode(section.range)}
        >
          Code
        </button>
      </header>
      <div className="min-h-0 overflow-auto p-5">
        {error ? (
          <div className="rounded border border-[#7f3434] bg-[#291416] p-3 text-sm text-[#ffd2cc]">
            {text("renderError")} {error}
          </div>
        ) : null}
        <div
          ref={containerRef}
          className="min-h-full min-w-full overflow-auto [&_svg]:h-auto [&_svg]:max-w-none"
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
  const nodesWithRanges = payload.nodes.filter((node) => node.range);
  for (const node of nodesWithRanges) {
    const mermaidId = node.id.replace(/[^A-Za-z0-9_]/g, "_");
    const elements = container.querySelectorAll<SVGGElement>(`[id*="${mermaidId}"]`);
    for (const element of elements) {
      element.style.cursor = "pointer";
      element.addEventListener("click", () => onOpenFlowchart(node));
    }
  }
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
  const label = escapeMermaidText(node.label);
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
    .replace(/\s+/g, " ")
    .trim();
}

function escapeMermaidEdgeLabel(value: string): string {
  return escapeMermaidText(value).replaceAll("|", "/");
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
