import React, { useEffect, useMemo, useRef, useState } from "react";
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
  mermaid: "flowchart TD",
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
    nodes: "Nodes",
    mermaid: "Mermaid",
    emptyIncludes: "No includes found.",
    emptyNodes: "No VBScript flow nodes found.",
    missing: "missing",
    openDirective: "Open directive",
    openFlowchart: "Open flowchart",
    renderError: "Mermaid render failed.",
    sections: "Sections",
  },
  ja: {
    title: "ASP Flowchart",
    includes: "Includes",
    nodes: "Nodes",
    mermaid: "Mermaid",
    emptyIncludes: "include は見つかりません。",
    emptyNodes: "VBScript flow node は見つかりません。",
    missing: "missing",
    openDirective: "directive を開く",
    openFlowchart: "flowchart を開く",
    renderError: "Mermaid render に失敗しました。",
    sections: "Sections",
  },
};

function App(): React.ReactElement {
  const [payload, setPayload] = useState<FlowchartPayload>(
    window.__ASP_LSP_FLOWCHART__ ?? fallbackPayload,
  );
  const locale = payload.locale ?? "en";
  const text = (key: string): string => messages[locale][key] ?? messages.en[key] ?? key;
  const nodesBySection = useMemo(() => nodesBySectionId(payload), [payload]);
  useEffect(() => {
    const listener = (event: MessageEvent) => {
      const message = event.data as { type?: unknown; payload?: unknown };
      if (message.type === "flowchartPayload" && isFlowchartPayload(message.payload)) {
        setPayload(message.payload);
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
            {payload.fileName ?? text("title")}
          </div>
          <div className="mt-1 text-xs text-[#9fb0c5]">
            {payload.stats.sections} {text("sections")} / {payload.stats.nodes} {text("nodes")} /{" "}
            {payload.includes.length} {text("includes")}
          </div>
        </header>
        <div className="min-h-0 flex-1 overflow-auto p-3">
          <SectionHeading>{text("includes")}</SectionHeading>
          <IncludeList includes={payload.includes} text={text} uri={payload.uri} />
          <SectionHeading>{text("nodes")}</SectionHeading>
          {payload.sections.length === 0 ? (
            <EmptyText>{text("emptyNodes")}</EmptyText>
          ) : (
            payload.sections.map((section) => (
              <FlowSection
                key={section.id}
                nodes={nodesBySection.get(section.id) ?? []}
                section={section}
                uri={payload.uri}
              />
            ))
          )}
          <SectionHeading>{text("mermaid")}</SectionHeading>
          <pre className="max-h-52 overflow-auto rounded border border-[#263140] bg-[#0c1117] p-2 text-xs leading-5 text-[#b9c5d6]">
            {payload.mermaid}
          </pre>
        </div>
      </aside>
      <FlowchartCanvas payload={payload} text={text} />
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
  section,
  uri,
}: {
  nodes: AspFlowchartNode[];
  section: AspFlowchartSection;
  uri: string;
}): React.ReactElement {
  const visibleNodes = nodes.filter((node) => node.kind !== "start" && node.kind !== "end");
  return (
    <div className="mb-3 rounded border border-[#263140] bg-[#101820]">
      <div className="border-b border-[#263140] px-2 py-1.5 text-xs font-semibold uppercase tracking-wide text-[#9fb0c5]">
        {section.label}
      </div>
      <div className="grid gap-1 p-2">
        {visibleNodes.length === 0 ? (
          <EmptyText>Empty</EmptyText>
        ) : (
          visibleNodes.map((node) => (
            <button
              key={node.id}
              className="truncate rounded px-2 py-1 text-left text-xs text-[#d9e0ea] hover:bg-[#223044]"
              type="button"
              onClick={() =>
                node.range && vscode.postMessage({ type: "openRange", uri, range: node.range })
              }
            >
              <span className="mr-2 text-[#91c7ff]">{node.kind}</span>
              {node.label}
            </button>
          ))
        )}
      </div>
    </div>
  );
}

function FlowchartCanvas({
  payload,
  text,
}: {
  payload: FlowchartPayload;
  text(key: string): string;
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
        const result = await mermaid.render(id, payload.mermaid || "flowchart TD");
        if (cancelled || !containerRef.current) {
          return;
        }
        containerRef.current.innerHTML = result.svg;
        attachSvgNodeHandlers(containerRef.current, payload);
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
  }, [payload]);
  return (
    <section className="min-h-0 overflow-auto bg-[#0d1117] p-5">
      {error ? (
        <div className="rounded border border-[#7f3434] bg-[#291416] p-3 text-sm text-[#ffd2cc]">
          {text("renderError")} {error}
        </div>
      ) : null}
      <div
        ref={containerRef}
        className="min-h-full min-w-full overflow-auto [&_svg]:h-auto [&_svg]:max-w-none"
      />
    </section>
  );
}

function attachSvgNodeHandlers(container: HTMLDivElement, payload: FlowchartPayload): void {
  const nodesWithRanges = payload.nodes.filter((node) => node.range);
  for (const node of nodesWithRanges) {
    const mermaidId = node.id.replace(/[^A-Za-z0-9_]/g, "_");
    const elements = container.querySelectorAll<SVGGElement>(`[id*="${mermaidId}"]`);
    for (const element of elements) {
      element.style.cursor = "pointer";
      element.addEventListener("click", () =>
        vscode.postMessage({ type: "openRange", uri: payload.uri, range: node.range }),
      );
    }
  }
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
