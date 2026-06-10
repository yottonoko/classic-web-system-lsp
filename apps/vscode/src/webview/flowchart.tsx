import React, { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import mermaid from "mermaid";
import tailwindStyles from "./flowchart.css?inline";
import type {
  AspFlowchartInclude,
  AspFlowchartNode,
  AspFlowchartNodeLink,
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
type WebviewTheme = "light" | "dark";
type WebviewThemeSetting = WebviewTheme | "auto";
type InfoPanelPosition = "left" | "right";

interface FlowchartPayload extends AspFlowchartPayload {
  locale?: FlowchartLocale;
  settings?: {
    maxTextSize?: number;
    theme?: WebviewThemeSetting;
    infoPanelPosition?: InfoPanelPosition;
  };
}

const vscode = acquireVsCodeApi();

const flowchartLabelLineLength = 28;
const flowchartEdgeLabelLineLength = 22;
const maximumFlowchartLabelCharacters = 180;
const maximumFlowchartEdgeLabelCharacters = 80;
const minimumFlowchartZoom = 0.4;
const maximumFlowchartZoom = 2.5;
const flowchartZoomStep = 0.1;
const defaultFlowchartMaxTextSize = 2_000_000;
const flowchartPanelDefaultWidth = 380;
const flowchartPanelMinimumWidth = 320;
const flowchartPanelMaximumWidth = 620;
const flowchartCanvasMinimumWidth = 360;
const flowchartPaneResizeHandleWidth = 6;
const flowchartPaneResizeKeyboardStep = 16;

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
    resizeInfoPanel: "Resize information pane",
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
    collapseSection: "Collapse section",
    expandSection: "Expand section",
    includesHint: "Included files referenced by this ASP file.",
    flowchartSectionHint:
      "Flow nodes grouped by top-level code, functions, procedures, classes, or properties.",
    flowchartNodeHint: "Flowchart element. Hover the rendered node for the same hint.",
    flowchartLinkHint: "Resolved symbol used by this flowchart element.",
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
    resizeInfoPanel: "情報 pane の幅を変更",
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
    collapseSection: "セクションを閉じる",
    expandSection: "セクションを開く",
    includesHint: "この ASP file が参照する include file です。",
    flowchartSectionHint:
      "トップレベル、関数、プロシージャ、class、property ごとの flow node です。",
    flowchartNodeHint: "フローチャートの要素です。描画された node でも同じ hint を表示します。",
    flowchartLinkHint: "このフローチャート要素で使われる解決済み symbol です。",
  },
};

type FlowchartNodeKind = AspFlowchartNode["kind"];
type FlowchartNodeLinkRole = AspFlowchartNodeLink["role"];

interface FlowchartVisualStyle {
  background: string;
  border: string;
  mermaidClass: string;
  text: string;
}

interface FlowchartThemePalette {
  mermaidTheme: "base" | "dark";
  mermaidThemeVariables?: Record<string, string>;
  nodeKindStyles: Record<FlowchartNodeKind, FlowchartVisualStyle>;
  linkRoleStyles: Record<FlowchartNodeLinkRole, FlowchartVisualStyle>;
  symbolKindStyles: Record<string, FlowchartVisualStyle>;
}

const darkFlowchartNodeKindStyles: Record<FlowchartNodeKind, FlowchartVisualStyle> = {
  start: {
    background: "#132538",
    border: "#7dd3fc",
    mermaidClass: "flowStart",
    text: "#e0f2fe",
  },
  end: {
    background: "#1f2937",
    border: "#94a3b8",
    mermaidClass: "flowEnd",
    text: "#f1f5f9",
  },
  if: {
    background: "#2f2410",
    border: "#f6c177",
    mermaidClass: "flowBranch",
    text: "#ffe8b6",
  },
  elseif: {
    background: "#2f2410",
    border: "#f6c177",
    mermaidClass: "flowBranch",
    text: "#ffe8b6",
  },
  else: {
    background: "#2f2410",
    border: "#f6c177",
    mermaidClass: "flowBranch",
    text: "#ffe8b6",
  },
  select: {
    background: "#2f2410",
    border: "#f6c177",
    mermaidClass: "flowBranch",
    text: "#ffe8b6",
  },
  case: {
    background: "#2f2410",
    border: "#f6c177",
    mermaidClass: "flowBranch",
    text: "#ffe8b6",
  },
  for: {
    background: "#251b35",
    border: "#c792ea",
    mermaidClass: "flowLoop",
    text: "#ead7ff",
  },
  forEach: {
    background: "#251b35",
    border: "#c792ea",
    mermaidClass: "flowLoop",
    text: "#ead7ff",
  },
  do: {
    background: "#251b35",
    border: "#c792ea",
    mermaidClass: "flowLoop",
    text: "#ead7ff",
  },
  while: {
    background: "#251b35",
    border: "#c792ea",
    mermaidClass: "flowLoop",
    text: "#ead7ff",
  },
  call: {
    background: "#102a2a",
    border: "#63e6be",
    mermaidClass: "flowCall",
    text: "#c8fff1",
  },
  declaration: {
    background: "#302610",
    border: "#ffcb6b",
    mermaidClass: "flowDeclaration",
    text: "#fff0b8",
  },
  exit: {
    background: "#34191d",
    border: "#ff7b8a",
    mermaidClass: "flowExit",
    text: "#ffd4da",
  },
  statement: {
    background: "#172131",
    border: "#89ddff",
    mermaidClass: "flowStatement",
    text: "#d9e0ea",
  },
};

const lightFlowchartNodeKindStyles: Record<FlowchartNodeKind, FlowchartVisualStyle> = {
  start: {
    background: "#e0f2fe",
    border: "#0284c7",
    mermaidClass: "flowStart",
    text: "#0f172a",
  },
  end: {
    background: "#e2e8f0",
    border: "#64748b",
    mermaidClass: "flowEnd",
    text: "#0f172a",
  },
  if: {
    background: "#fef3c7",
    border: "#b45309",
    mermaidClass: "flowBranch",
    text: "#3f2a04",
  },
  elseif: {
    background: "#fef3c7",
    border: "#b45309",
    mermaidClass: "flowBranch",
    text: "#3f2a04",
  },
  else: {
    background: "#fef3c7",
    border: "#b45309",
    mermaidClass: "flowBranch",
    text: "#3f2a04",
  },
  select: {
    background: "#fef3c7",
    border: "#b45309",
    mermaidClass: "flowBranch",
    text: "#3f2a04",
  },
  case: {
    background: "#fef3c7",
    border: "#b45309",
    mermaidClass: "flowBranch",
    text: "#3f2a04",
  },
  for: {
    background: "#f3e8ff",
    border: "#7e22ce",
    mermaidClass: "flowLoop",
    text: "#3b0764",
  },
  forEach: {
    background: "#f3e8ff",
    border: "#7e22ce",
    mermaidClass: "flowLoop",
    text: "#3b0764",
  },
  do: {
    background: "#f3e8ff",
    border: "#7e22ce",
    mermaidClass: "flowLoop",
    text: "#3b0764",
  },
  while: {
    background: "#f3e8ff",
    border: "#7e22ce",
    mermaidClass: "flowLoop",
    text: "#3b0764",
  },
  call: {
    background: "#ccfbf1",
    border: "#0f766e",
    mermaidClass: "flowCall",
    text: "#134e4a",
  },
  declaration: {
    background: "#fef3c7",
    border: "#ca8a04",
    mermaidClass: "flowDeclaration",
    text: "#3f2a04",
  },
  exit: {
    background: "#ffe4e6",
    border: "#e11d48",
    mermaidClass: "flowExit",
    text: "#881337",
  },
  statement: {
    background: "#e0f2fe",
    border: "#0284c7",
    mermaidClass: "flowStatement",
    text: "#0f172a",
  },
};

const flowchartNodeKindLabels: Record<FlowchartLocale, Record<FlowchartNodeKind, string>> = {
  en: {
    start: "Start",
    end: "End",
    if: "If",
    elseif: "ElseIf",
    else: "Else",
    select: "Select",
    case: "Case",
    for: "For",
    forEach: "For Each",
    do: "Do",
    while: "While",
    call: "Call",
    declaration: "Declaration",
    exit: "Exit",
    statement: "Statement",
  },
  ja: {
    start: "開始",
    end: "終了",
    if: "If",
    elseif: "ElseIf",
    else: "Else",
    select: "Select",
    case: "Case",
    for: "For",
    forEach: "For Each",
    do: "Do",
    while: "While",
    call: "呼び出し",
    declaration: "宣言",
    exit: "終了",
    statement: "実行",
  },
};

const flowchartSectionKindLabels: Record<
  FlowchartLocale,
  Record<AspFlowchartSection["kind"], string>
> = {
  en: {
    topLevel: "Top level",
    class: "Class",
    procedure: "Procedure",
    property: "Property",
  },
  ja: {
    topLevel: "トップレベル",
    class: "Class",
    procedure: "プロシージャ",
    property: "Property",
  },
};

const darkFlowchartLinkRoleStyles: Record<FlowchartNodeLinkRole, FlowchartVisualStyle> = {
  read: {
    background: "#162816",
    border: "#c3e88d",
    mermaidClass: "flowLinkRead",
    text: "#e5ffd0",
  },
  write: {
    background: "#302610",
    border: "#ffcb6b",
    mermaidClass: "flowLinkWrite",
    text: "#fff0b8",
  },
  call: {
    background: "#2f1f16",
    border: "#f78c6c",
    mermaidClass: "flowLinkCall",
    text: "#ffd8ca",
  },
  new: {
    background: "#251b35",
    border: "#c792ea",
    mermaidClass: "flowLinkNew",
    text: "#ead7ff",
  },
  member: {
    background: "#2f2314",
    border: "#ffb86c",
    mermaidClass: "flowLinkMember",
    text: "#ffe3bd",
  },
  definition: {
    background: "#112839",
    border: "#89ddff",
    mermaidClass: "flowLinkDefinition",
    text: "#d5f7ff",
  },
  unknown: {
    background: "#1f2937",
    border: "#b2ccd6",
    mermaidClass: "flowLinkUnknown",
    text: "#e4eef3",
  },
};

const lightFlowchartLinkRoleStyles: Record<FlowchartNodeLinkRole, FlowchartVisualStyle> = {
  read: {
    background: "#dcfce7",
    border: "#16a34a",
    mermaidClass: "flowLinkRead",
    text: "#14532d",
  },
  write: {
    background: "#fef3c7",
    border: "#ca8a04",
    mermaidClass: "flowLinkWrite",
    text: "#3f2a04",
  },
  call: {
    background: "#ffedd5",
    border: "#ea580c",
    mermaidClass: "flowLinkCall",
    text: "#7c2d12",
  },
  new: {
    background: "#f3e8ff",
    border: "#7e22ce",
    mermaidClass: "flowLinkNew",
    text: "#3b0764",
  },
  member: {
    background: "#ffedd5",
    border: "#c2410c",
    mermaidClass: "flowLinkMember",
    text: "#7c2d12",
  },
  definition: {
    background: "#e0f2fe",
    border: "#0284c7",
    mermaidClass: "flowLinkDefinition",
    text: "#0c4a6e",
  },
  unknown: {
    background: "#e2e8f0",
    border: "#64748b",
    mermaidClass: "flowLinkUnknown",
    text: "#334155",
  },
};

const darkFlowchartSymbolKindStyles: Record<string, FlowchartVisualStyle> = {
  function: {
    background: "#102a2a",
    border: "#63e6be",
    mermaidClass: "flowSymbolFunction",
    text: "#c8fff1",
  },
  sub: {
    background: "#102a2a",
    border: "#7dd3fc",
    mermaidClass: "flowSymbolSub",
    text: "#dff6ff",
  },
  class: {
    background: "#1f2c14",
    border: "#c3e88d",
    mermaidClass: "flowSymbolClass",
    text: "#e5ffd0",
  },
  method: {
    background: "#2f1f16",
    border: "#f78c6c",
    mermaidClass: "flowSymbolMethod",
    text: "#ffd8ca",
  },
  property: {
    background: "#351c28",
    border: "#ff9cac",
    mermaidClass: "flowSymbolProperty",
    text: "#ffdce8",
  },
  variable: {
    background: "#302610",
    border: "#ffcb6b",
    mermaidClass: "flowSymbolVariable",
    text: "#fff0b8",
  },
  constant: {
    background: "#16243a",
    border: "#82aaff",
    mermaidClass: "flowSymbolConstant",
    text: "#dce8ff",
  },
  parameter: {
    background: "#1b2930",
    border: "#b2ccd6",
    mermaidClass: "flowSymbolParameter",
    text: "#e4eef3",
  },
  field: {
    background: "#2f2314",
    border: "#ffb86c",
    mermaidClass: "flowSymbolField",
    text: "#ffe3bd",
  },
  member: {
    background: "#2f2314",
    border: "#ffb86c",
    mermaidClass: "flowSymbolMember",
    text: "#ffe3bd",
  },
};

const lightFlowchartSymbolKindStyles: Record<string, FlowchartVisualStyle> = {
  function: {
    background: "#ccfbf1",
    border: "#0f766e",
    mermaidClass: "flowSymbolFunction",
    text: "#134e4a",
  },
  sub: {
    background: "#e0f2fe",
    border: "#0284c7",
    mermaidClass: "flowSymbolSub",
    text: "#0c4a6e",
  },
  class: {
    background: "#dcfce7",
    border: "#16a34a",
    mermaidClass: "flowSymbolClass",
    text: "#14532d",
  },
  method: {
    background: "#ffedd5",
    border: "#ea580c",
    mermaidClass: "flowSymbolMethod",
    text: "#7c2d12",
  },
  property: {
    background: "#ffe4e6",
    border: "#e11d48",
    mermaidClass: "flowSymbolProperty",
    text: "#881337",
  },
  variable: {
    background: "#fef3c7",
    border: "#ca8a04",
    mermaidClass: "flowSymbolVariable",
    text: "#3f2a04",
  },
  constant: {
    background: "#dbeafe",
    border: "#2563eb",
    mermaidClass: "flowSymbolConstant",
    text: "#1e3a8a",
  },
  parameter: {
    background: "#e2e8f0",
    border: "#64748b",
    mermaidClass: "flowSymbolParameter",
    text: "#334155",
  },
  field: {
    background: "#ffedd5",
    border: "#c2410c",
    mermaidClass: "flowSymbolField",
    text: "#7c2d12",
  },
  member: {
    background: "#ffedd5",
    border: "#c2410c",
    mermaidClass: "flowSymbolMember",
    text: "#7c2d12",
  },
};

const flowchartThemePalettes: Record<WebviewTheme, FlowchartThemePalette> = {
  dark: {
    mermaidTheme: "dark",
    nodeKindStyles: darkFlowchartNodeKindStyles,
    linkRoleStyles: darkFlowchartLinkRoleStyles,
    symbolKindStyles: darkFlowchartSymbolKindStyles,
  },
  light: {
    mermaidTheme: "base",
    mermaidThemeVariables: {
      background: "#f8fafc",
      mainBkg: "#ffffff",
      primaryColor: "#e0f2fe",
      primaryBorderColor: "#0284c7",
      primaryTextColor: "#0f172a",
      lineColor: "#64748b",
      textColor: "#0f172a",
      edgeLabelBackground: "#ffffff",
    },
    nodeKindStyles: lightFlowchartNodeKindStyles,
    linkRoleStyles: lightFlowchartLinkRoleStyles,
    symbolKindStyles: lightFlowchartSymbolKindStyles,
  },
};

function useResolvedWebviewTheme(setting: WebviewThemeSetting | undefined): WebviewTheme {
  const [vscodeTheme, setVsCodeTheme] = useState<WebviewTheme>(() => detectedVsCodeTheme());
  useEffect(() => {
    if (setting === "light" || setting === "dark") {
      return undefined;
    }
    const observer = new MutationObserver(() => setVsCodeTheme(detectedVsCodeTheme()));
    observer.observe(document.body, { attributes: true, attributeFilter: ["class"] });
    return () => observer.disconnect();
  }, [setting]);
  return setting === "light" || setting === "dark" ? setting : vscodeTheme;
}

function detectedVsCodeTheme(): WebviewTheme {
  const classList = document.body.classList;
  return classList.contains("vscode-light") || classList.contains("vscode-high-contrast-light")
    ? "light"
    : "dark";
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

function App(): React.ReactElement {
  const initialPayload = window.__ASP_LSP_FLOWCHART__ ?? fallbackPayload;
  const [payload, setPayload] = useState<FlowchartPayload>(initialPayload);
  const theme = useResolvedWebviewTheme(payload.settings?.theme);
  const themePalette = flowchartThemePalettes[theme];
  const [selectedSectionId, setSelectedSectionId] = useState<string | undefined>(() =>
    defaultSectionId(initialPayload),
  );
  const [searchQuery, setSearchQuery] = useState("");
  const [activeSearchIndex, setActiveSearchIndex] = useState(0);
  const [infoPanelWidth, setInfoPanelWidth] = useState(flowchartPanelDefaultWidth);
  const searchInputRef = useRef<HTMLInputElement | null>(null);
  const [layoutRef, layoutSize] = useElementSize<HTMLElement>();
  const infoPanelPosition = payload.settings?.infoPanelPosition ?? "left";
  const maximumInfoPanelWidth = maxFlowchartInfoPanelWidthForLayout(layoutSize.width);
  const clampedInfoPanelWidth = clamp(
    infoPanelWidth,
    flowchartPanelMinimumWidth,
    maximumInfoPanelWidth,
  );
  const layoutStyle = {
    "--flowchart-panel-width": `${clampedInfoPanelWidth}px`,
  } as React.CSSProperties;
  const layoutClassName =
    infoPanelPosition === "right"
      ? "asp-lsp-flowchart-shell grid h-full grid-cols-[minmax(0,1fr)_6px_var(--flowchart-panel-width)] bg-[#101419] text-[#d9e0ea]"
      : "asp-lsp-flowchart-shell grid h-full grid-cols-[var(--flowchart-panel-width)_6px_minmax(0,1fr)] bg-[#101419] text-[#d9e0ea]";
  const infoPanelClassName =
    infoPanelPosition === "right"
      ? "order-3 flex min-h-0 flex-col border-l border-[#263140] bg-[#151b23]"
      : "order-1 flex min-h-0 flex-col border-r border-[#263140] bg-[#151b23]";
  const canvasClassName = infoPanelPosition === "right" ? "order-1" : "order-3";
  const locale = payload.locale ?? "en";
  const text = useCallback(
    (key: string): string => messages[locale][key] ?? messages.en[key] ?? key,
    [locale],
  );
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
    () => flowchartForSection(payload, selectedSectionId, themePalette),
    [payload, selectedSectionId, themePalette],
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
  useEffect(() => {
    setInfoPanelWidth((currentWidth) =>
      clamp(
        currentWidth,
        flowchartPanelMinimumWidth,
        maxFlowchartInfoPanelWidthForLayout(layoutSize.width),
      ),
    );
  }, [layoutSize.width]);
  return (
    <main
      ref={layoutRef}
      className={layoutClassName}
      data-asp-lsp-theme={theme}
      style={layoutStyle}
    >
      <aside className={infoPanelClassName}>
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
          <SidebarAccordionSection
            count={payload.includes.length}
            hint={text("includesHint")}
            text={text}
            title={text("includes")}
          >
            <IncludeList includes={payload.includes} text={text} uri={payload.uri} />
          </SidebarAccordionSection>
          <SectionHeading>{text("flowcharts")}</SectionHeading>
          {payload.sections.length === 0 ? (
            <EmptyText>{text("emptyNodes")}</EmptyText>
          ) : (
            payload.sections.map((section) => (
              <FlowSection
                key={section.id}
                locale={locale}
                nodes={nodesBySection.get(section.id) ?? []}
                selected={section.id === selectedSection?.id}
                section={section}
                themePalette={themePalette}
                text={text}
                activeSearchNodeId={activeSearchNode?.id}
                matchedNodeIds={matchedNodeIds}
                onOpenCode={(range) =>
                  range && vscode.postMessage({ type: "openRange", uri: payload.uri, range })
                }
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
      <FlowchartPaneResizeHandle
        label={text("resizeInfoPanel")}
        maxWidth={maximumInfoPanelWidth}
        minWidth={flowchartPanelMinimumWidth}
        position={infoPanelPosition}
        width={clampedInfoPanelWidth}
        onWidthChange={setInfoPanelWidth}
      />
      <FlowchartCanvas
        className={canvasClassName}
        payload={selectedFlowchart}
        section={selectedSection}
        themePalette={themePalette}
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

function SidebarAccordionSection({
  children,
  count,
  hint,
  text,
  title,
}: {
  children: React.ReactNode;
  count?: number;
  hint?: string;
  text(key: string): string;
  title: string;
}): React.ReactElement {
  const [open, setOpen] = useState(true);
  const headerTitle = detailParts(title, hint, text(open ? "collapseSection" : "expandSection"));
  return (
    <section className="mb-4 rounded border border-[#263140] bg-[#101820]">
      <div className="flex items-center gap-2">
        <button
          aria-expanded={open}
          className="flex min-w-0 flex-1 items-center gap-2 px-2 py-1.5 text-left hover:bg-[#172131]"
          title={headerTitle}
          type="button"
          onClick={() => setOpen((current) => !current)}
        >
          <span className="h-6 w-6 shrink-0 rounded border border-[#334255] text-center text-xs leading-6 text-[#c4d4e8]">
            {open ? "▾" : "▸"}
          </span>
          <span className="min-w-0 flex-1 truncate text-xs font-semibold uppercase tracking-wide text-[#9fb0c5]">
            {title}
          </span>
          {typeof count === "number" ? (
            <span className="shrink-0 rounded border border-[#334255] px-1.5 py-0.5 text-[11px] text-[#9fb0c5]">
              {count}
            </span>
          ) : null}
        </button>
        {hint ? (
          <div className="shrink-0 pr-2">
            <FlowchartHint hint={hint} label={title} />
          </div>
        ) : null}
      </div>
      {open ? <div className="border-t border-[#263140] p-2">{children}</div> : null}
    </section>
  );
}

interface TooltipPosition {
  left: number;
  top: number;
  maxWidth: number;
}

function FlowchartHint({ hint, label }: { hint: string; label: string }): React.ReactElement {
  const triggerRef = useRef<HTMLSpanElement>(null);
  const [position, setPosition] = useState<TooltipPosition>();
  const showTooltip = useCallback(() => {
    setPosition(tooltipPositionFor(triggerRef.current));
  }, []);
  const hideTooltip = useCallback(() => setPosition(undefined), []);
  return (
    <span className="group relative inline-flex shrink-0 items-center">
      <span
        ref={triggerRef}
        tabIndex={0}
        aria-label={`${label}: ${hint}`}
        className="inline-grid h-3.5 w-3.5 cursor-help place-items-center rounded-full border border-[#405068] text-[10px] leading-none text-[#8d98a8] outline-none hover:border-[#89ddff] hover:text-[#d7dde8] focus:border-[#89ddff] focus:text-[#d7dde8]"
        onBlur={hideTooltip}
        onFocus={showTooltip}
        onPointerEnter={showTooltip}
        onPointerLeave={hideTooltip}
      >
        ?
      </span>
      {position ? (
        <span
          role="tooltip"
          className="pointer-events-none fixed z-[1000] rounded-md border border-[#405068] bg-[#0d1117] px-2 py-1.5 text-[11px] leading-[1.35] whitespace-normal text-[#d7dde8] shadow-[0_10px_24px_rgb(0_0_0_/_35%)]"
          style={{
            left: position.left,
            top: position.top,
            maxWidth: position.maxWidth,
          }}
        >
          {hint}
        </span>
      ) : null}
    </span>
  );
}

function tooltipPositionFor(element: HTMLElement | null): TooltipPosition | undefined {
  if (!element) {
    return undefined;
  }
  const margin = 12;
  const gap = 6;
  const rect = element.getBoundingClientRect();
  const maxWidth = Math.max(160, Math.min(260, window.innerWidth - margin * 2));
  const left = clamp(rect.left, margin, Math.max(margin, window.innerWidth - maxWidth - margin));
  const top = Math.min(rect.bottom + gap, Math.max(margin, window.innerHeight - margin - 96));
  return { left, top, maxWidth };
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
    <div className="grid gap-2">
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
  locale,
  nodes,
  selected,
  section,
  themePalette,
  text,
  activeSearchNodeId,
  matchedNodeIds,
  onOpenCode,
  onOpenTarget,
  onSelect,
}: {
  locale: FlowchartLocale;
  nodes: AspFlowchartNode[];
  selected: boolean;
  section: AspFlowchartSection;
  themePalette: FlowchartThemePalette;
  text(key: string): string;
  activeSearchNodeId?: string;
  matchedNodeIds: Set<string>;
  onOpenCode(range: AspFlowchartNode["range"] | AspFlowchartSection["range"]): void;
  onOpenTarget(target: AspFlowchartTarget): void;
  onSelect(): void;
}): React.ReactElement {
  const visibleNodes = nodes.filter((node) => node.kind !== "start" && node.kind !== "end");
  const [open, setOpen] = useState(true);
  const sectionHint = flowchartSectionHint(section, text, locale);
  const headerTitle = detailParts(
    section.label,
    sectionHint,
    text(open ? "collapseSection" : "expandSection"),
  );
  return (
    <div
      className={`mb-3 rounded border bg-[#101820] ${
        selected ? "border-[#6fb6ff]" : "border-[#263140]"
      }`}
    >
      <div className="flex items-center gap-2 border-b border-[#263140] px-2 py-1.5">
        <button
          aria-expanded={open}
          className="h-6 w-6 shrink-0 rounded border border-[#334255] text-xs text-[#c4d4e8] hover:border-[#7dd3fc] hover:text-white"
          title={headerTitle}
          type="button"
          onClick={() => setOpen((current) => !current)}
        >
          {open ? "▾" : "▸"}
        </button>
        <button
          className="min-w-0 flex-1 truncate text-left text-xs font-semibold uppercase tracking-wide text-[#9fb0c5] hover:text-[#f1f5f9]"
          title={detailParts(text("selectFlowchart"), section.label, sectionHint)}
          type="button"
          onClick={onSelect}
        >
          {section.label}
        </button>
        <FlowchartHint hint={sectionHint} label={section.label} />
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
      {open ? (
        <div className="grid gap-1 p-2">
          {visibleNodes.length === 0 ? (
            <EmptyText>{text("emptySection")}</EmptyText>
          ) : (
            visibleNodes.map((node) => {
              const isSearchMatch = matchedNodeIds.has(node.id);
              const isActiveSearchMatch = activeSearchNodeId === node.id;
              const nodeHint = flowchartNodeHint(node, text, locale);
              const nodeStyle = themePalette.nodeKindStyles[node.kind];
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
                      className="min-w-0 truncate px-1 py-1 text-left text-xs text-[#d9e0ea] disabled:cursor-not-allowed disabled:text-[#5f6d7e]"
                      disabled={!node.range}
                      title={detailParts(text("openCode"), nodeHint)}
                      type="button"
                      onClick={() => node.range && onOpenCode(node.range)}
                    >
                      <span
                        className="mr-2 rounded border px-1 py-0.5 text-[10px]"
                        style={flowchartSwatchStyle(nodeStyle)}
                        title={flowchartNodeKindLabel(node.kind, locale)}
                      >
                        {flowchartNodeKindLabel(node.kind, locale)}
                      </span>
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
                      {node.links.map((link) => {
                        const linkStyle = flowchartNodeLinkStyle(link, themePalette);
                        return (
                          <button
                            key={link.id}
                            className="rounded border px-1.5 py-0.5 text-[11px] hover:text-white"
                            style={flowchartSwatchStyle(linkStyle)}
                            title={flowchartNodeLinkHint(link, text, locale)}
                            type="button"
                            onClick={() => onOpenTarget(link.target)}
                          >
                            {link.label}
                          </button>
                        );
                      })}
                    </div>
                  ) : null}
                </div>
              );
            })
          )}
        </div>
      ) : null}
    </div>
  );
}

interface FlowchartContextMenuState {
  node: AspFlowchartNode;
  x: number;
  y: number;
}

function FlowchartPaneResizeHandle({
  label,
  maxWidth,
  minWidth,
  onWidthChange,
  position,
  width,
}: {
  label: string;
  maxWidth: number;
  minWidth: number;
  onWidthChange(width: number): void;
  position: InfoPanelPosition;
  width: number;
}): React.ReactElement {
  const updateWidth = useCallback(
    (nextWidth: number) => onWidthChange(clamp(nextWidth, minWidth, maxWidth)),
    [maxWidth, minWidth, onWidthChange],
  );
  const handlePointerDown = useCallback(
    (event: React.PointerEvent<HTMLDivElement>) => {
      if (event.button !== 0) {
        return;
      }
      event.preventDefault();
      const startX = event.clientX;
      const startWidth = width;
      const previousCursor = document.body.style.cursor;
      const previousUserSelect = document.body.style.userSelect;
      document.body.style.cursor = "col-resize";
      document.body.style.userSelect = "none";

      const handlePointerMove = (moveEvent: PointerEvent) => {
        moveEvent.preventDefault();
        const deltaX = moveEvent.clientX - startX;
        updateWidth(position === "left" ? startWidth + deltaX : startWidth - deltaX);
      };
      const stopResize = () => {
        document.body.style.cursor = previousCursor;
        document.body.style.userSelect = previousUserSelect;
        window.removeEventListener("pointermove", handlePointerMove);
        window.removeEventListener("pointerup", stopResize);
        window.removeEventListener("pointercancel", stopResize);
      };

      window.addEventListener("pointermove", handlePointerMove);
      window.addEventListener("pointerup", stopResize);
      window.addEventListener("pointercancel", stopResize);
    },
    [position, updateWidth, width],
  );
  const handleKeyDown = useCallback(
    (event: React.KeyboardEvent<HTMLDivElement>) => {
      if (event.key === "ArrowLeft") {
        event.preventDefault();
        updateWidth(
          width +
            (position === "left"
              ? -flowchartPaneResizeKeyboardStep
              : flowchartPaneResizeKeyboardStep),
        );
      } else if (event.key === "ArrowRight") {
        event.preventDefault();
        updateWidth(
          width +
            (position === "left"
              ? flowchartPaneResizeKeyboardStep
              : -flowchartPaneResizeKeyboardStep),
        );
      } else if (event.key === "Home") {
        event.preventDefault();
        updateWidth(minWidth);
      } else if (event.key === "End") {
        event.preventDefault();
        updateWidth(maxWidth);
      }
    },
    [maxWidth, minWidth, position, updateWidth, width],
  );

  return (
    <div
      role="separator"
      tabIndex={0}
      aria-label={label}
      aria-orientation="vertical"
      aria-valuemin={minWidth}
      aria-valuemax={maxWidth}
      aria-valuenow={width}
      title={label}
      className="relative order-2 cursor-col-resize bg-[#101820] outline-none before:absolute before:inset-y-0 before:left-1/2 before:w-px before:-translate-x-1/2 before:bg-[#263140] hover:bg-[#172131] focus:bg-[#172131] focus:before:bg-[#7dd3fc]"
      onKeyDown={handleKeyDown}
      onPointerDown={handlePointerDown}
    />
  );
}

function FlowchartCanvas({
  className,
  payload,
  section,
  themePalette,
  text,
  activeSearchNodeId,
  matchedNodeIds,
  onOpenCode,
  onOpenFlowchart,
}: {
  className?: string;
  payload: FlowchartPayload;
  section: AspFlowchartSection | undefined;
  themePalette: FlowchartThemePalette;
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
  const [contextMenu, setContextMenu] = useState<FlowchartContextMenuState>();
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
  const openContextMenu = useCallback((node: AspFlowchartNode, event: MouseEvent) => {
    event.preventDefault();
    setContextMenu({ node, x: event.clientX, y: event.clientY });
  }, []);
  const closeContextMenu = useCallback(() => setContextMenu(undefined), []);
  const openContextMenuCode = useCallback(() => {
    if (!contextMenu?.node.range) {
      return;
    }
    onOpenCode(contextMenu.node.range);
    closeContextMenu();
  }, [closeContextMenu, contextMenu, onOpenCode]);
  const openContextMenuFlowchart = useCallback(() => {
    if (!contextMenu) {
      return;
    }
    onOpenFlowchart(contextMenu.node);
    closeContextMenu();
  }, [closeContextMenu, contextMenu, onOpenFlowchart]);
  useEffect(() => {
    if (!contextMenu) {
      return undefined;
    }
    const closeOnPointerDown = () => closeContextMenu();
    const closeOnKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        closeContextMenu();
      }
    };
    window.addEventListener("pointerdown", closeOnPointerDown);
    window.addEventListener("keydown", closeOnKeyDown);
    window.addEventListener("blur", closeContextMenu);
    return () => {
      window.removeEventListener("pointerdown", closeOnPointerDown);
      window.removeEventListener("keydown", closeOnKeyDown);
      window.removeEventListener("blur", closeContextMenu);
    };
  }, [closeContextMenu, contextMenu]);
  useEffect(() => {
    let cancelled = false;
    const render = async (): Promise<void> => {
      if (!containerRef.current) {
        return;
      }
      mermaid.initialize({
        startOnLoad: false,
        maxTextSize: flowchartMaxTextSize(payload),
        securityLevel: "strict",
        theme: themePalette.mermaidTheme,
        themeVariables: themePalette.mermaidThemeVariables,
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
        attachSvgNodeHandlers(
          containerRef.current,
          payload,
          text,
          onOpenFlowchart,
          openContextMenu,
        );
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
  }, [onOpenFlowchart, openContextMenu, payload, themePalette, text]);
  useEffect(() => {
    if (!containerRef.current) {
      return;
    }
    syncSvgSearchHighlights(containerRef.current, payload, matchedNodeIds, activeSearchNodeId);
  }, [activeSearchNodeId, matchedNodeIds, payload, svg]);
  const contextMenuPosition = contextMenu
    ? clampedContextMenuPosition(contextMenu.x, contextMenu.y)
    : undefined;
  return (
    <section
      className={`${className ?? ""} grid min-h-0 grid-rows-[auto_1fr] overflow-hidden bg-[#0d1117]`}
    >
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
        {contextMenu && contextMenuPosition ? (
          <div
            className="fixed z-50 grid min-w-40 overflow-hidden rounded-md border border-[#3b4a5f] bg-[#151b23] py-1 text-xs text-[#d9e0ea] shadow-[0_12px_28px_rgb(0_0_0_/_32%)]"
            role="menu"
            style={{ left: contextMenuPosition.left, top: contextMenuPosition.top }}
            onContextMenu={(event) => event.preventDefault()}
            onPointerDown={(event) => event.stopPropagation()}
          >
            <button
              className="px-3 py-1.5 text-left hover:bg-[#172131] disabled:cursor-not-allowed disabled:text-[#5f6d7e]"
              disabled={!contextMenu.node.range}
              role="menuitem"
              type="button"
              onClick={openContextMenuCode}
            >
              Code
            </button>
            <button
              className="px-3 py-1.5 text-left hover:bg-[#172131]"
              role="menuitem"
              type="button"
              onClick={openContextMenuFlowchart}
            >
              {text("openFlowchart")}
            </button>
          </div>
        ) : null}
      </div>
    </section>
  );
}

function attachSvgNodeHandlers(
  container: HTMLDivElement,
  payload: FlowchartPayload,
  text: (key: string) => string,
  onOpenFlowchart: (node: AspFlowchartNode) => void,
  onOpenContextMenu: (node: AspFlowchartNode, event: MouseEvent) => void,
): void {
  const locale = payload.locale ?? "en";
  for (const node of payload.nodes) {
    for (const element of svgElementsForFlowchartNode(container, node)) {
      const hint = flowchartNodeHint(node, text, locale);
      element.setAttribute("aria-label", hint);
      let titleElement = element.querySelector<SVGTitleElement>("title");
      if (!titleElement) {
        titleElement = document.createElementNS(
          "http://www.w3.org/2000/svg",
          "title",
        ) as SVGTitleElement;
        element.prepend(titleElement);
      }
      titleElement.textContent = hint;
      element.style.cursor = "pointer";
      element.addEventListener("click", () => onOpenFlowchart(node));
      element.addEventListener("contextmenu", (event) => onOpenContextMenu(node, event));
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

function clampedContextMenuPosition(x: number, y: number): { left: number; top: number } {
  const margin = 8;
  const estimatedWidth = 180;
  const estimatedHeight = 88;
  return {
    left: clamp(x, margin, Math.max(margin, window.innerWidth - estimatedWidth - margin)),
    top: clamp(y, margin, Math.max(margin, window.innerHeight - estimatedHeight - margin)),
  };
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
  themePalette: FlowchartThemePalette,
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
    mermaid: mermaidForSelectedSection(nodes, edges, themePalette),
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
  themePalette: FlowchartThemePalette,
): string {
  const lines = ["flowchart TB"];
  for (const node of nodes) {
    lines.push(`  ${mermaidNode(node)}`);
    lines.push(
      `  class ${mermaidId(node.id)} ${themePalette.nodeKindStyles[node.kind].mermaidClass}`,
    );
  }
  for (const edge of edges) {
    lines.push(
      `  ${mermaidId(edge.source)} -->${edge.label ? `|${escapeMermaidEdgeLabel(edge.label)}|` : ""} ${mermaidId(edge.target)}`,
    );
  }
  lines.push(...flowchartMermaidClassDefinitions(themePalette));
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

function flowchartMermaidClassDefinitions(themePalette: FlowchartThemePalette): string[] {
  const stylesByClass = new Map<string, FlowchartVisualStyle>();
  for (const style of Object.values(themePalette.nodeKindStyles)) {
    stylesByClass.set(style.mermaidClass, style);
  }
  return [...stylesByClass.values()].map(
    (style) =>
      `  classDef ${style.mermaidClass} fill:${style.background},stroke:${style.border},stroke-width:2px,color:${style.text};`,
  );
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

function flowchartSectionHint(
  section: AspFlowchartSection,
  text: (key: string) => string,
  locale: FlowchartLocale,
): string {
  return detailParts(
    text("flowchartSectionHint"),
    flowchartSectionKindLabel(section.kind, locale),
    section.label,
  );
}

function flowchartNodeHint(
  node: AspFlowchartNode,
  text: (key: string) => string,
  locale: FlowchartLocale,
): string {
  return detailParts(
    text("flowchartNodeHint"),
    flowchartNodeKindLabel(node.kind, locale),
    node.label,
    node.description,
    node.links?.length ? `${node.links.length} ${text("definitions")}` : undefined,
  );
}

function flowchartNodeLinkHint(
  link: AspFlowchartNodeLink,
  text: (key: string) => string,
  locale: FlowchartLocale,
): string {
  return detailParts(
    text("flowchartLinkHint"),
    flowchartNodeLinkRoleLabel(link.role, locale),
    link.symbolKind ? formatFlowchartSymbolKind(link.symbolKind) : undefined,
    link.label,
  );
}

function flowchartNodeKindLabel(kind: FlowchartNodeKind, locale: FlowchartLocale): string {
  return flowchartNodeKindLabels[locale][kind] ?? flowchartNodeKindLabels.en[kind] ?? kind;
}

function flowchartSectionKindLabel(
  kind: AspFlowchartSection["kind"],
  locale: FlowchartLocale,
): string {
  return flowchartSectionKindLabels[locale][kind] ?? flowchartSectionKindLabels.en[kind] ?? kind;
}

function flowchartNodeLinkRoleLabel(role: FlowchartNodeLinkRole, locale: FlowchartLocale): string {
  const labels: Record<FlowchartLocale, Record<FlowchartNodeLinkRole, string>> = {
    en: {
      read: "Read",
      write: "Write",
      call: "Call",
      new: "Create",
      member: "Member",
      definition: "Definition",
      unknown: "Reference",
    },
    ja: {
      read: "参照",
      write: "代入",
      call: "呼び出し",
      new: "作成",
      member: "メンバー",
      definition: "定義",
      unknown: "参照",
    },
  };
  return labels[locale][role] ?? labels.en[role] ?? role;
}

function flowchartNodeLinkStyle(
  link: AspFlowchartNodeLink,
  themePalette: FlowchartThemePalette,
): FlowchartVisualStyle {
  return (
    flowchartSymbolStyle(link.symbolKind, themePalette) ?? themePalette.linkRoleStyles[link.role]
  );
}

function flowchartSymbolStyle(
  symbolKind: string | undefined,
  themePalette: FlowchartThemePalette,
): FlowchartVisualStyle | undefined {
  const normalized = normalizeFlowchartSymbolKind(symbolKind);
  if (!normalized) {
    return undefined;
  }
  if (themePalette.symbolKindStyles[normalized]) {
    return themePalette.symbolKindStyles[normalized];
  }
  if (normalized.includes("function")) {
    return themePalette.symbolKindStyles.function;
  }
  if (normalized.includes("sub")) {
    return themePalette.symbolKindStyles.sub;
  }
  if (normalized.includes("class")) {
    return themePalette.symbolKindStyles.class;
  }
  if (normalized.includes("method")) {
    return themePalette.symbolKindStyles.method;
  }
  if (normalized.includes("property")) {
    return themePalette.symbolKindStyles.property;
  }
  if (normalized.includes("variable")) {
    return themePalette.symbolKindStyles.variable;
  }
  if (normalized.includes("constant")) {
    return themePalette.symbolKindStyles.constant;
  }
  if (normalized.includes("parameter")) {
    return themePalette.symbolKindStyles.parameter;
  }
  if (normalized.includes("field")) {
    return themePalette.symbolKindStyles.field;
  }
  if (normalized.includes("member")) {
    return themePalette.symbolKindStyles.member;
  }
  return undefined;
}

function normalizeFlowchartSymbolKind(symbolKind: string | undefined): string {
  return symbolKind ? symbolKind.replace(/[^A-Za-z]/g, "").toLowerCase() : "";
}

function formatFlowchartSymbolKind(symbolKind: string): string {
  return symbolKind
    .replace(/([a-z])([A-Z])/g, "$1 $2")
    .replace(/[_-]+/g, " ")
    .trim();
}

function flowchartSwatchStyle(style: FlowchartVisualStyle): React.CSSProperties {
  return {
    backgroundColor: style.background,
    borderColor: style.border,
    color: style.text,
  };
}

function detailParts(...parts: Array<string | undefined>): string {
  return parts.filter((part): part is string => Boolean(part && part.trim())).join(" · ");
}

function clamp(value: number, minimum: number, maximum: number): number {
  return Math.min(maximum, Math.max(minimum, value));
}

function maxFlowchartInfoPanelWidthForLayout(containerWidth: number): number {
  if (containerWidth <= 0) {
    return flowchartPanelMaximumWidth;
  }
  return Math.max(
    flowchartPanelMinimumWidth,
    Math.min(
      flowchartPanelMaximumWidth,
      containerWidth - flowchartCanvasMinimumWidth - flowchartPaneResizeHandleWidth,
    ),
  );
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

function flowchartMaxTextSize(payload: FlowchartPayload): number {
  const value = payload.settings?.maxTextSize;
  return typeof value === "number" && Number.isFinite(value) && value >= 1
    ? Math.floor(value)
    : defaultFlowchartMaxTextSize;
}

const style = document.createElement("style");
style.textContent = tailwindStyles;
document.head.append(style);

createRoot(document.getElementById("root")!).render(<App />);
