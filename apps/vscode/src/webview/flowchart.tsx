import React, { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import mermaid from "mermaid";
import tailwindStyles from "./flowchart.css?inline";
import { VirtualList } from "./virtual-list";
import type {
  AspFlowchartInclude,
  AspFlowchartLabelMode,
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
    __ASP_LSP_FLOWCHART_TARGET_RANGE__?: AspFlowchartTarget["range"] | null;
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
    maxEdges?: number;
    labelLineLength?: number;
    labelMode?: AspFlowchartLabelMode;
    minZoom?: number;
    maxZoom?: number;
    theme?: WebviewThemeSetting;
    infoPanelPosition?: InfoPanelPosition;
  };
}

const vscode = acquireVsCodeApi();

const flowchartLabelLineLength = 34;
const flowchartEdgeLabelLineLength = 22;
const maximumFlowchartLabelCharacters = 180;
const maximumFlowchartEdgeLabelCharacters = 80;
const minimumFlowchartLabelLineLength = 8;
const branchNodeHorizontalScale = 1.4;
const flowchartNodePadding = 2;
const defaultMinimumFlowchartZoom = 0.4;
const defaultMaximumFlowchartZoom = 4;
const flowchartZoomStep = 0.1;
const defaultFlowchartMaxTextSize = 2_000_000;
const defaultFlowchartMaxEdges = 100_000;
const flowchartPanelDefaultWidth = 380;
const flowchartPanelMinimumWidth = 320;
const flowchartPanelMaximumWidth = 620;
const flowchartCanvasMinimumWidth = 360;
const flowchartPaneResizeHandleWidth = 6;
const flowchartPaneResizeKeyboardStep = 16;
const flowchartLabelModes: AspFlowchartLabelMode[] = ["normal", "raw", "description"];

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
    openGraph: "Open graph",
    openMenu: "Open",
    openDefinition: "Open definition",
    renderError: "Mermaid render failed.",
    resizeInfoPanel: "Resize information pane",
    selectFlowchart: "Select flowchart",
    emptySection: "Empty",
    sections: "Sections",
    definitions: "Definitions",
    copyMermaid: "Copy Mermaid",
    exportMenu: "Export",
    exportMermaid: "Export Mermaid",
    exportSvg: "Export SVG",
    searchNodes: "Search nodes",
    searchPlaceholder: "Search",
    searchPrevious: "Previous match",
    searchNext: "Next match",
    zoomOut: "Zoom out",
    zoomIn: "Zoom in",
    resetZoom: "Reset zoom",
    fitWidth: "Fit",
    fitWidthDescription: "Fit width to view",
    labelMode: "Labels",
    labelModeNormal: "Normal",
    labelModeRaw: "Raw",
    labelModeDescription: "Prose",
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
    openGraph: "グラフを開く",
    openMenu: "Open",
    openDefinition: "定義を開く",
    renderError: "Mermaid render に失敗しました。",
    resizeInfoPanel: "情報 pane の幅を変更",
    selectFlowchart: "フローチャートを選択",
    emptySection: "空です",
    sections: "セクション",
    definitions: "定義",
    copyMermaid: "Mermaid コピー",
    exportMenu: "Export",
    exportMermaid: "Mermaid 出力",
    exportSvg: "SVG 出力",
    searchNodes: "ノード検索",
    searchPlaceholder: "検索",
    searchPrevious: "前の一致",
    searchNext: "次の一致",
    zoomOut: "縮小",
    zoomIn: "拡大",
    resetZoom: "ズームをリセット",
    fitWidth: "フィット",
    fitWidthDescription: "横幅にフィット",
    labelMode: "表示",
    labelModeNormal: "通常",
    labelModeRaw: "コード",
    labelModeDescription: "文章",
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

interface FlowchartPanState {
  pointerId: number;
  startX: number;
  startY: number;
  scrollLeft: number;
  scrollTop: number;
  moved: boolean;
}

interface FlowchartViewportSize {
  width: number;
  height: number;
}

type FlowchartToolbarMode = "full" | "compactExports" | "compactAll";
type FlowchartToolbarMenuKind = "open" | "export";

interface FlowchartToolbarMenuState {
  kind: FlowchartToolbarMenuKind;
  left: number;
  top: number;
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
  implicitglobalvariable: {
    background: "#332014",
    border: "#f78c6c",
    mermaidClass: "flowSymbolImplicitGlobalVariable",
    text: "#ffe0cf",
  },
  unresolvedfunction: {
    background: "#351c28",
    border: "#ff9cac",
    mermaidClass: "flowSymbolUnresolvedFunction",
    text: "#ffdce8",
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
  implicitglobalvariable: {
    background: "#ffedd5",
    border: "#c2410c",
    mermaidClass: "flowSymbolImplicitGlobalVariable",
    text: "#7c2d12",
  },
  unresolvedfunction: {
    background: "#ffe4e6",
    border: "#e11d48",
    mermaidClass: "flowSymbolUnresolvedFunction",
    text: "#881337",
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
  const initialTargetRange = isRange(window.__ASP_LSP_FLOWCHART_TARGET_RANGE__)
    ? window.__ASP_LSP_FLOWCHART_TARGET_RANGE__
    : undefined;
  const initialTargetNode = initialTargetRange
    ? flowchartNodeForRange(initialPayload, initialTargetRange)
    : undefined;
  const initialSectionId = initialTargetRange
    ? (initialTargetNode?.sectionId ??
      sectionIdForRange(initialPayload, initialTargetRange) ??
      defaultSectionId(initialPayload))
    : defaultSectionId(initialPayload);
  const [payload, setPayload] = useState<FlowchartPayload>(initialPayload);
  const [labelMode, setLabelMode] = useState<AspFlowchartLabelMode>(() =>
    flowchartLabelModeForPayload(initialPayload),
  );
  const theme = useResolvedWebviewTheme(payload.settings?.theme);
  const themePalette = flowchartThemePalettes[theme];
  const [selectedSectionId, setSelectedSectionId] = useState<string | undefined>(
    () => initialSectionId,
  );
  const selectedSectionIdRef = useRef<string | undefined>(initialSectionId);
  const [autoOpenSectionId, setAutoOpenSectionId] = useState<string | undefined>(() =>
    initialTargetRange ? initialSectionId : undefined,
  );
  const [focusedFlowchartNodeId, setFocusedFlowchartNodeId] = useState<string | undefined>(
    () => initialTargetNode?.id,
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
  const activeFlowchartNodeId = focusedFlowchartNodeId ?? activeSearchNode?.id;
  const selectedFlowchart = useMemo(
    () => flowchartForSection(payload, selectedSectionId, themePalette),
    [payload, selectedSectionId, themePalette],
  );
  const selectedSection = selectedFlowchart.sections[0];
  const selectedSectionIndex = useMemo(
    () => payload.sections.findIndex((section) => section.id === selectedSection?.id),
    [payload.sections, selectedSection?.id],
  );
  const selectFlowchartNode = useCallback((node: AspFlowchartNode) => {
    setSelectedSectionId(node.sectionId);
    setAutoOpenSectionId(node.sectionId);
    setFocusedFlowchartNodeId(node.id);
  }, []);
  const openFlowchartForNode = useCallback(
    (node: AspFlowchartNode) => {
      setFocusedFlowchartNodeId(undefined);
      const target = node.links?.[0]?.target;
      if (target) {
        setAutoOpenSectionId(openFlowchartTarget(payload, target, setSelectedSectionId, labelMode));
      } else {
        const nextSectionId = sectionIdForNodeFlowchart(payload, node);
        setSelectedSectionId(nextSectionId);
        setAutoOpenSectionId(nextSectionId);
      }
    },
    [labelMode, payload],
  );
  const openTarget = useCallback(
    (target: AspFlowchartTarget) => {
      setFocusedFlowchartNodeId(undefined);
      setAutoOpenSectionId(openFlowchartTarget(payload, target, setSelectedSectionId, labelMode));
    },
    [labelMode, payload],
  );
  const openGraph = useCallback(
    (range: AspFlowchartNode["range"] | AspFlowchartSection["range"]) => {
      if (range) {
        vscode.postMessage({ type: "openGraphLocation", uri: payload.uri, range });
      }
    },
    [payload.uri],
  );
  const changeLabelMode = useCallback(
    (mode: AspFlowchartLabelMode) => {
      if (mode === labelMode) {
        return;
      }
      setLabelMode(mode);
      vscode.postMessage({ type: "reloadFlowchart", uri: payload.uri, labelMode: mode });
    },
    [labelMode, payload.uri],
  );
  const selectSearchMatch = useCallback(
    (index: number) => {
      if (searchMatches.length === 0) {
        return;
      }
      const nextIndex = modulo(index, searchMatches.length);
      const nextSectionId = searchMatches[nextIndex]?.node.sectionId;
      setFocusedFlowchartNodeId(undefined);
      setActiveSearchIndex(nextIndex);
      setSelectedSectionId(nextSectionId);
      setAutoOpenSectionId(nextSectionId);
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
        setFocusedFlowchartNodeId(undefined);
      }
    },
    [activeSearchIndex, selectSearchMatch],
  );
  useEffect(() => {
    selectedSectionIdRef.current = selectedSectionId;
  }, [selectedSectionId]);
  useEffect(() => {
    const listener = (event: MessageEvent) => {
      const message = event.data as { type?: unknown; payload?: unknown; targetRange?: unknown };
      if (message.type === "flowchartPayload" && isFlowchartPayload(message.payload)) {
        const targetRange = isRange(message.targetRange) ? message.targetRange : undefined;
        const targetNode = targetRange
          ? flowchartNodeForRange(message.payload, targetRange)
          : undefined;
        const preservedSectionId =
          !targetRange &&
          selectedSectionIdRef.current &&
          message.payload.sections.some((section) => section.id === selectedSectionIdRef.current)
            ? selectedSectionIdRef.current
            : undefined;
        const nextSectionId = targetRange
          ? (targetNode?.sectionId ??
            sectionIdForRange(message.payload, targetRange) ??
            defaultSectionId(message.payload))
          : (preservedSectionId ?? defaultSectionId(message.payload));
        setPayload(message.payload);
        setLabelMode(flowchartLabelModeForPayload(message.payload));
        setFocusedFlowchartNodeId(targetNode?.id);
        setSelectedSectionId(nextSectionId);
        setAutoOpenSectionId(targetRange ? nextSectionId : undefined);
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
    const nextSectionId =
      searchMatches[Math.min(activeSearchIndex, searchMatches.length - 1)].node.sectionId;
    setSelectedSectionId(nextSectionId);
    if (searchQuery.trim()) {
      setAutoOpenSectionId(nextSectionId);
    }
  }, [activeSearchIndex, searchMatches, searchQuery]);
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
      <aside className={`${infoPanelClassName} min-w-0 overflow-hidden`}>
        <header className="border-b border-[#263140] px-4 py-3">
          <div
            className="overflow-hidden text-ellipsis whitespace-nowrap text-sm font-semibold text-[#f1f5f9]"
            title={selectedSection?.label ?? payload.fileName ?? text("title")}
          >
            {selectedSection?.label ?? payload.fileName ?? text("title")}
          </div>
          <div
            className="mt-1 overflow-hidden text-ellipsis whitespace-nowrap text-xs text-[#9fb0c5]"
            title={`${payload.stats.sections} ${text("sections")} / ${selectedFlowchart.stats.nodes} ${text("nodes")} / ${payload.includes.length} ${text("includes")}`}
          >
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
              onChange={(event) => {
                setSearchQuery(event.target.value);
                setFocusedFlowchartNodeId(undefined);
              }}
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
            <IncludeList
              includes={payload.includes}
              labelMode={labelMode}
              text={text}
              uri={payload.uri}
            />
          </SidebarAccordionSection>
          <SectionHeading>{text("flowcharts")}</SectionHeading>
          {payload.sections.length === 0 ? (
            <EmptyText>{text("emptyNodes")}</EmptyText>
          ) : (
            <VirtualList
              className="grid gap-3"
              estimateSize={72}
              getKey={(section) => section.id}
              items={payload.sections}
              maxHeight={560}
              overscan={8}
              scrollToIndex={selectedSectionIndex >= 0 ? selectedSectionIndex : undefined}
              renderItem={(section) => {
                const sectionNodes = nodesBySection.get(section.id) ?? [];
                const hasActiveNode = Boolean(
                  activeFlowchartNodeId &&
                  sectionNodes.some((node) => node.id === activeFlowchartNodeId),
                );
                return (
                  <FlowSection
                    locale={locale}
                    nodes={sectionNodes}
                    selected={section.id === selectedSection?.id}
                    section={section}
                    shouldAutoOpen={autoOpenSectionId === section.id || hasActiveNode}
                    themePalette={themePalette}
                    text={text}
                    activeSearchNodeId={activeFlowchartNodeId}
                    matchedNodeIds={matchedNodeIds}
                    onOpenCode={(range) =>
                      range && vscode.postMessage({ type: "openRange", uri: payload.uri, range })
                    }
                    onOpenGraph={openGraph}
                    onOpenTarget={openTarget}
                    onSelect={() => {
                      setSelectedSectionId(section.id);
                      setAutoOpenSectionId(section.id);
                      setFocusedFlowchartNodeId(undefined);
                    }}
                    onSelectNode={selectFlowchartNode}
                  />
                );
              }}
            />
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
        activeSearchNodeId={activeFlowchartNodeId}
        matchedNodeIds={matchedNodeIds}
        onOpenCode={(range) =>
          range && vscode.postMessage({ type: "openRange", uri: payload.uri, range })
        }
        onOpenFlowchart={openFlowchartForNode}
        onOpenGraph={openGraph}
        labelMode={labelMode}
        onLabelModeChange={changeLabelMode}
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
    <section className="mb-4 min-w-0 overflow-hidden rounded border border-[#263140] bg-[#101820]">
      <div className="flex items-center gap-2">
        <button
          aria-expanded={open}
          aria-label={headerTitle}
          className="flex min-w-0 flex-1 items-center gap-2 px-2 py-1.5 text-left hover:bg-[#172131]"
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
  labelMode,
  text,
  uri,
}: {
  includes: AspFlowchartInclude[];
  labelMode: AspFlowchartLabelMode;
  text(key: string): string;
  uri: string;
}): React.ReactElement {
  if (includes.length === 0) {
    return <EmptyText>{text("emptyIncludes")}</EmptyText>;
  }
  return (
    <VirtualList
      className="grid gap-2"
      estimateSize={84}
      getKey={(include, index) => `${include.mode}:${include.path}:${index}`}
      items={includes}
      maxHeight={280}
      overscan={8}
      renderItem={(include) => (
        <div className="min-w-0 overflow-hidden rounded border border-[#263140] bg-[#101820] p-2">
          <div className="flex min-w-0 items-start justify-between gap-2">
            <button
              className="min-w-0 flex-1 text-left text-sm font-medium text-[#8ec7ff] hover:underline disabled:cursor-not-allowed disabled:text-[#7b8796] disabled:no-underline"
              disabled={!include.exists || !include.resolvedUri}
              title={detailParts(text("openFlowchart"), include.path, include.actualPath)}
              type="button"
              onClick={() =>
                include.resolvedUri &&
                vscode.postMessage({
                  type: "openIncludeFlowchart",
                  uri: include.resolvedUri,
                  labelMode,
                })
              }
            >
              <span className="block overflow-hidden text-ellipsis whitespace-nowrap">
                {include.path}
              </span>
            </button>
            {include.exists === false ? (
              <span className="text-xs text-[#ffb4a8]">{text("missing")}</span>
            ) : null}
          </div>
          <div className="mt-1 flex min-w-0 items-center justify-between gap-2 text-xs text-[#9fb0c5]">
            <span className="min-w-0 overflow-hidden text-ellipsis whitespace-nowrap">
              {include.mode}
            </span>
            <button
              className="shrink-0 text-[#c4d4e8] hover:text-white hover:underline"
              title={detailParts(text("openDirective"), include.path)}
              type="button"
              onClick={() => vscode.postMessage({ type: "openRange", uri, range: include.range })}
            >
              {text("openDirective")}
            </button>
          </div>
          {include.actualPath ? (
            <div
              className="mt-1 overflow-hidden text-ellipsis whitespace-nowrap text-xs text-[#7d8ca1]"
              title={include.actualPath}
            >
              {include.actualPath}
            </div>
          ) : null}
        </div>
      )}
    />
  );
}

function FlowSection({
  locale,
  nodes,
  selected,
  section,
  shouldAutoOpen,
  themePalette,
  text,
  activeSearchNodeId,
  matchedNodeIds,
  onOpenCode,
  onOpenGraph,
  onOpenTarget,
  onSelect,
  onSelectNode,
}: {
  locale: FlowchartLocale;
  nodes: AspFlowchartNode[];
  selected: boolean;
  section: AspFlowchartSection;
  shouldAutoOpen: boolean;
  themePalette: FlowchartThemePalette;
  text(key: string): string;
  activeSearchNodeId?: string;
  matchedNodeIds: Set<string>;
  onOpenCode(range: AspFlowchartNode["range"] | AspFlowchartSection["range"]): void;
  onOpenGraph(range: AspFlowchartNode["range"] | AspFlowchartSection["range"]): void;
  onOpenTarget(target: AspFlowchartTarget): void;
  onSelect(): void;
  onSelectNode(node: AspFlowchartNode): void;
}): React.ReactElement {
  const visibleNodes = nodes.filter((node) => node.kind !== "start" && node.kind !== "end");
  const [open, setOpen] = useState(false);
  const sectionHint = flowchartSectionHint(section, text, locale);
  const headerTitle = detailParts(
    section.label,
    sectionHint,
    text(open ? "collapseSection" : "expandSection"),
  );
  useEffect(() => {
    if (shouldAutoOpen) {
      setOpen(true);
    }
  }, [shouldAutoOpen]);
  const activeNodeIndex = visibleNodes.findIndex((node) => node.id === activeSearchNodeId);
  return (
    <div
      className={`mb-3 min-w-0 overflow-hidden rounded border bg-[#101820] ${
        selected ? "border-[#6fb6ff]" : "border-[#263140]"
      }`}
    >
      <div className="flex items-center gap-2 border-b border-[#263140] px-2 py-1.5">
        <button
          aria-expanded={open}
          aria-label={headerTitle}
          className="h-6 w-6 shrink-0 rounded border border-[#334255] text-xs text-[#c4d4e8] hover:border-[#7dd3fc] hover:text-white"
          type="button"
          onClick={() => setOpen((current) => !current)}
        >
          {open ? "▾" : "▸"}
        </button>
        <button
          aria-label={detailParts(text("selectFlowchart"), section.label, sectionHint)}
          className="min-w-0 flex-1 truncate text-left text-xs font-semibold text-[#9fb0c5] hover:text-[#f1f5f9]"
          type="button"
          onClick={onSelect}
        >
          <span>{section.label}</span>
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
        <button
          className="shrink-0 rounded border border-[#334255] px-2 py-0.5 text-[11px] text-[#c4d4e8] hover:border-[#6fb6ff] hover:text-white disabled:cursor-not-allowed disabled:border-[#263140] disabled:text-[#5f6d7e]"
          disabled={!section.range}
          title={text("openGraph")}
          type="button"
          onClick={() => section.range && onOpenGraph(section.range)}
        >
          Graph
        </button>
      </div>
      {open ? (
        <div className="p-2">
          {visibleNodes.length === 0 ? (
            <EmptyText>{text("emptySection")}</EmptyText>
          ) : (
            <VirtualList
              className="grid gap-1"
              estimateSize={(node) => (node.links?.length ? 76 : 42)}
              getKey={(node) => node.id}
              items={visibleNodes}
              maxHeight={360}
              overscan={10}
              scrollToIndex={activeNodeIndex >= 0 ? activeNodeIndex : undefined}
              renderItem={(node) => {
                const isSearchMatch = matchedNodeIds.has(node.id);
                const isActiveSearchMatch = activeSearchNodeId === node.id;
                const nodeHint = flowchartNodeHint(node, text, locale);
                const nodeStyle = themePalette.nodeKindStyles[node.kind];
                return (
                  <div
                    className={`rounded px-1 py-1 hover:bg-[#223044] ${
                      isActiveSearchMatch
                        ? "bg-[#17324a] ring-1 ring-[#7dd3fc]"
                        : isSearchMatch
                          ? "bg-[#2b2b18] ring-1 ring-[#f6c177]"
                          : ""
                    }`}
                  >
                    <div className="grid grid-cols-[minmax(0,1fr)_auto_auto] items-center gap-1">
                      <button
                        className="min-w-0 overflow-hidden text-ellipsis whitespace-nowrap px-1 py-1 text-left text-xs text-[#d9e0ea] hover:text-white"
                        title={detailParts(text("openFlowchart"), node.label, nodeHint)}
                        type="button"
                        onClick={() => onSelectNode(node)}
                      >
                        <span
                          className="mr-2 rounded border px-1 py-0.5 text-[10px]"
                          style={flowchartSwatchStyle(nodeStyle)}
                          title={flowchartNodeKindLabel(node.kind, locale)}
                        >
                          {flowchartNodeKindLabel(node.kind, locale)}
                        </span>
                        <span title={node.label}>{node.label}</span>
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
                      <button
                        className="mr-1 shrink-0 rounded border border-[#3b4a5f] px-1.5 py-0.5 text-[11px] text-[#c4d4e8] hover:border-[#7dd3fc] hover:text-white disabled:cursor-not-allowed disabled:border-[#263140] disabled:text-[#5f6d7e]"
                        disabled={!node.range}
                        title={text("openGraph")}
                        type="button"
                        onClick={() => node.range && onOpenGraph(node.range)}
                      >
                        Graph
                      </button>
                    </div>
                    {node.links?.length ? (
                      <div className="ml-1 mt-1 flex flex-wrap gap-1">
                        {node.links.map((link) => {
                          const linkStyle = flowchartNodeLinkStyle(link, themePalette);
                          return (
                            <button
                              key={link.id}
                              className="max-w-full overflow-hidden text-ellipsis whitespace-nowrap rounded border px-1.5 py-0.5 text-[11px] hover:text-white"
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
              }}
            />
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

interface FlowchartSvgSize {
  width: number;
  height: number;
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
  labelMode,
  payload,
  section,
  themePalette,
  text,
  activeSearchNodeId,
  matchedNodeIds,
  onOpenCode,
  onOpenFlowchart,
  onOpenGraph,
  onLabelModeChange,
}: {
  className?: string;
  labelMode: AspFlowchartLabelMode;
  payload: FlowchartPayload;
  section: AspFlowchartSection | undefined;
  themePalette: FlowchartThemePalette;
  text(key: string): string;
  activeSearchNodeId?: string;
  matchedNodeIds: Set<string>;
  onOpenCode(range: AspFlowchartNode["range"] | AspFlowchartSection["range"]): void;
  onOpenFlowchart(node: AspFlowchartNode): void;
  onOpenGraph(range: AspFlowchartNode["range"] | AspFlowchartSection["range"]): void;
  onLabelModeChange(mode: AspFlowchartLabelMode): void;
}): React.ReactElement {
  const viewportRef = useRef<HTMLDivElement | null>(null);
  const containerRef = useRef<HTMLDivElement | null>(null);
  const panStateRef = useRef<FlowchartPanState | undefined>(undefined);
  const suppressNextCanvasClickRef = useRef(false);
  const userPannedFlowchartKeyRef = useRef<string | undefined>(undefined);
  const [error, setError] = useState<string>();
  const [svg, setSvg] = useState<string>("");
  const [svgSize, setSvgSize] = useState<FlowchartSvgSize>();
  const [zoom, setZoom] = useState(1);
  const [viewportSize, setViewportSize] = useState<FlowchartViewportSize>({ width: 0, height: 0 });
  const [isPanning, setIsPanning] = useState(false);
  const [contextMenu, setContextMenu] = useState<FlowchartContextMenuState>();
  const flowchartViewKey = useMemo(
    () => `${payload.uri}\n${section?.id ?? ""}\n${payload.mermaid}`,
    [payload.mermaid, payload.uri, section?.id],
  );
  const zoomRange = useMemo(
    () => flowchartZoomRange(payload),
    [payload.settings?.maxZoom, payload.settings?.minZoom],
  );
  const setClampedZoom = useCallback(
    (value: number) =>
      setZoom(roundFlowchartZoom(clamp(value, zoomRange.minimum, zoomRange.maximum))),
    [zoomRange],
  );
  const adjustZoom = useCallback(
    (direction: 1 | -1) => setClampedZoom(zoom + direction * flowchartZoomStep),
    [setClampedZoom, zoom],
  );
  const fitFlowchartWidth = useCallback(() => {
    if (!viewportRef.current || !svgSize) {
      return;
    }
    const nextZoom = flowchartFitWidthZoom(viewportRef.current, svgSize, zoomRange);
    if (nextZoom !== undefined) {
      userPannedFlowchartKeyRef.current = undefined;
      setClampedZoom(nextZoom);
    }
  }, [setClampedZoom, svgSize, zoomRange]);
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
  const beginCanvasPan = useCallback((event: React.PointerEvent<HTMLDivElement>) => {
    if (event.button !== 0 || !viewportRef.current) {
      return;
    }
    const target = event.target instanceof Node ? event.target : undefined;
    if (!target || !viewportRef.current.contains(target)) {
      return;
    }
    panStateRef.current = {
      pointerId: event.pointerId,
      startX: event.clientX,
      startY: event.clientY,
      scrollLeft: viewportRef.current.scrollLeft,
      scrollTop: viewportRef.current.scrollTop,
      moved: false,
    };
    viewportRef.current.setPointerCapture(event.pointerId);
    setIsPanning(true);
  }, []);
  const moveCanvasPan = useCallback((event: React.PointerEvent<HTMLDivElement>) => {
    const pan = panStateRef.current;
    if (!pan || pan.pointerId !== event.pointerId || !viewportRef.current) {
      return;
    }
    const deltaX = event.clientX - pan.startX;
    const deltaY = event.clientY - pan.startY;
    if (Math.abs(deltaX) > 2 || Math.abs(deltaY) > 2) {
      pan.moved = true;
    }
    viewportRef.current.scrollLeft = pan.scrollLeft - deltaX;
    viewportRef.current.scrollTop = pan.scrollTop - deltaY;
    event.preventDefault();
  }, []);
  const endCanvasPan = useCallback(
    (event: React.PointerEvent<HTMLDivElement>) => {
      const pan = panStateRef.current;
      if (!pan || pan.pointerId !== event.pointerId || !viewportRef.current) {
        return;
      }
      if (viewportRef.current.hasPointerCapture(event.pointerId)) {
        viewportRef.current.releasePointerCapture(event.pointerId);
      }
      if (pan.moved) {
        userPannedFlowchartKeyRef.current = flowchartViewKey;
        suppressNextCanvasClickRef.current = true;
        window.setTimeout(() => {
          suppressNextCanvasClickRef.current = false;
        }, 0);
      }
      panStateRef.current = undefined;
      setIsPanning(false);
    },
    [flowchartViewKey],
  );
  const suppressCanvasClickAfterPan = useCallback((event: React.MouseEvent<HTMLDivElement>) => {
    if (!suppressNextCanvasClickRef.current) {
      return;
    }
    event.preventDefault();
    event.stopPropagation();
  }, []);
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
  const openContextMenuGraph = useCallback(() => {
    if (!contextMenu?.node.range) {
      return;
    }
    onOpenGraph(contextMenu.node.range);
    closeContextMenu();
  }, [closeContextMenu, contextMenu, onOpenGraph]);
  useEffect(() => {
    setZoom((currentZoom) =>
      roundFlowchartZoom(clamp(currentZoom, zoomRange.minimum, zoomRange.maximum)),
    );
  }, [zoomRange]);
  useEffect(() => {
    userPannedFlowchartKeyRef.current = undefined;
  }, [flowchartViewKey]);
  useLayoutEffect(() => {
    const viewport = viewportRef.current;
    if (!viewport) {
      return undefined;
    }
    const updateViewportSize = (): void => {
      setViewportSize({ width: viewport.clientWidth, height: viewport.clientHeight });
    };
    updateViewportSize();
    const resizeObserver = new ResizeObserver(updateViewportSize);
    resizeObserver.observe(viewport);
    return () => resizeObserver.disconnect();
  }, []);
  useLayoutEffect(() => {
    const viewport = viewportRef.current;
    if (
      !viewport ||
      !svgSize ||
      viewportSize.width <= 0 ||
      userPannedFlowchartKeyRef.current === flowchartViewKey
    ) {
      return;
    }
    centerFlowchartHorizontally(viewport, svgSize, zoom, viewportSize);
  }, [flowchartViewKey, svgSize, viewportSize, zoom]);
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
        maxEdges: flowchartMaxEdges(payload),
        securityLevel: "strict",
        theme: themePalette.mermaidTheme,
        themeVariables: themePalette.mermaidThemeVariables,
        flowchart: { htmlLabels: false, curve: "basis", padding: flowchartNodePadding },
      });
      try {
        const id = `asp-lsp-flowchart-${Date.now().toString(36)}`;
        const result = await mermaid.render(id, payload.mermaid || "flowchart TB");
        if (cancelled || !containerRef.current) {
          return;
        }
        containerRef.current.innerHTML = result.svg;
        adjustSvgBranchNodeShapes(containerRef.current, payload);
        setSvg(containerRef.current.querySelector("svg")?.outerHTML ?? result.svg);
        setSvgSize(measuredFlowchartSvgSize(containerRef.current));
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
          setSvgSize(undefined);
        }
      }
    };
    void render();
    return () => {
      cancelled = true;
    };
  }, [onOpenFlowchart, openContextMenu, payload, themePalette, text]);
  useEffect(() => {
    if (!containerRef.current || !viewportRef.current) {
      return;
    }
    syncSvgSearchHighlights(
      containerRef.current,
      viewportRef.current,
      payload,
      matchedNodeIds,
      activeSearchNodeId,
    );
  }, [activeSearchNodeId, matchedNodeIds, payload, svg]);
  const exportSvg = useCallback(() => {
    vscode.postMessage({
      type: "exportFlowchart",
      format: "svg",
      uri: payload.uri,
      sectionLabel: section?.label,
      content: serializedFlowchartSvg(containerRef.current) ?? svg,
    });
  }, [payload.uri, section?.label, svg]);
  const copyMermaid = useCallback(() => {
    vscode.postMessage({
      type: "copyText",
      content: payload.mermaid,
    });
  }, [payload.mermaid]);
  const exportMermaid = useCallback(() => {
    vscode.postMessage({
      type: "exportFlowchart",
      format: "mermaid",
      uri: payload.uri,
      sectionLabel: section?.label,
      content: `${payload.mermaid}\n`,
    });
  }, [payload.mermaid, payload.uri, section?.label]);
  const contextMenuPosition = contextMenu
    ? clampedContextMenuPosition(contextMenu.x, contextMenu.y)
    : undefined;
  const canFitFlowchartWidth = Boolean(svgSize);
  return (
    <section
      className={`${className ?? ""} grid min-h-0 grid-rows-[auto_1fr] overflow-hidden bg-[#0d1117]`}
    >
      <header className="flex min-w-0 items-center gap-2 border-b border-[#263140] px-5 py-3">
        <div
          className="min-w-0 flex-1 truncate text-sm font-semibold text-[#f1f5f9]"
          title={section?.label ?? text("title")}
        >
          {section?.label ?? text("title")}
        </div>
        <FlowchartToolbar
          canExportSvg={Boolean(svg)}
          canFitFlowchartWidth={canFitFlowchartWidth}
          canOpenSection={Boolean(section?.range)}
          labelMode={labelMode}
          text={text}
          zoom={zoom}
          onLabelModeChange={onLabelModeChange}
          onCopyMermaid={copyMermaid}
          onExportMermaid={exportMermaid}
          onExportSvg={exportSvg}
          onFitFlowchartWidth={fitFlowchartWidth}
          onOpenCode={() => section?.range && onOpenCode(section.range)}
          onOpenGraph={() => section?.range && onOpenGraph(section.range)}
          onResetZoom={() => setClampedZoom(1)}
          onZoomIn={() => adjustZoom(1)}
          onZoomOut={() => adjustZoom(-1)}
        />
      </header>
      <div
        ref={viewportRef}
        className={`min-h-0 overflow-auto p-5 ${isPanning ? "cursor-grabbing" : "cursor-grab"}`}
        style={flowchartViewportStyle}
        onPointerCancel={endCanvasPan}
        onPointerDown={beginCanvasPan}
        onPointerMove={moveCanvasPan}
        onPointerUp={endCanvasPan}
        onClickCapture={suppressCanvasClickAfterPan}
        onWheel={handleWheel}
      >
        {error ? (
          <div className="rounded border border-[#7f3434] bg-[#291416] p-3 text-sm text-[#ffd2cc]">
            {text("renderError")} {error}
          </div>
        ) : null}
        <div
          className="relative select-none"
          style={scaledFlowchartCanvasStyle(svgSize, zoom, viewportSize)}
        >
          <div
            ref={containerRef}
            className="absolute top-0 inline-block origin-top-left [&_svg]:block [&_svg]:h-full [&_svg]:w-full [&_svg]:max-w-none"
            style={flowchartSvgLayerStyle(svgSize, zoom, viewportSize)}
          />
        </div>
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
            <button
              className="px-3 py-1.5 text-left hover:bg-[#172131] disabled:cursor-not-allowed disabled:text-[#5f6d7e]"
              disabled={!contextMenu.node.range}
              role="menuitem"
              type="button"
              onClick={openContextMenuGraph}
            >
              Graph
            </button>
          </div>
        ) : null}
      </div>
    </section>
  );
}

function FlowchartToolbar({
  canExportSvg,
  canFitFlowchartWidth,
  canOpenSection,
  labelMode,
  text,
  zoom,
  onLabelModeChange,
  onCopyMermaid,
  onExportMermaid,
  onExportSvg,
  onFitFlowchartWidth,
  onOpenCode,
  onOpenGraph,
  onResetZoom,
  onZoomIn,
  onZoomOut,
}: {
  canExportSvg: boolean;
  canFitFlowchartWidth: boolean;
  canOpenSection: boolean;
  labelMode: AspFlowchartLabelMode;
  text(key: string): string;
  zoom: number;
  onLabelModeChange(mode: AspFlowchartLabelMode): void;
  onCopyMermaid(): void;
  onExportMermaid(): void;
  onExportSvg(): void;
  onFitFlowchartWidth(): void;
  onOpenCode(): void;
  onOpenGraph(): void;
  onResetZoom(): void;
  onZoomIn(): void;
  onZoomOut(): void;
}): React.ReactElement {
  const [toolbarRef, toolbarSize] = useElementSize<HTMLDivElement>();
  const menuRef = useRef<HTMLDivElement | null>(null);
  const [menu, setMenu] = useState<FlowchartToolbarMenuState>();
  const toolbarMode = flowchartToolbarMode(toolbarSize.width);
  const compactExports = toolbarMode === "compactExports" || toolbarMode === "compactAll";
  const compactAll = toolbarMode === "compactAll";
  const closeMenu = useCallback(() => setMenu(undefined), []);
  const openMenu = useCallback((kind: FlowchartToolbarMenuKind, button: HTMLButtonElement) => {
    const rect = button.getBoundingClientRect();
    setMenu({
      kind,
      left: Math.max(8, Math.min(rect.left, window.innerWidth - flowchartToolbarMenuWidth - 8)),
      top: Math.min(rect.bottom + 6, window.innerHeight - 8),
    });
  }, []);
  const runMenuAction = useCallback(
    (action: () => void) => {
      action();
      closeMenu();
    },
    [closeMenu],
  );

  useEffect(() => {
    if (!menu) {
      return undefined;
    }
    const closeOnEscape = (event: KeyboardEvent): void => {
      if (event.key === "Escape") {
        closeMenu();
      }
    };
    const closeOnOutsidePointerDown = (event: PointerEvent): void => {
      const target = event.target;
      if (!(target instanceof Node)) {
        closeMenu();
        return;
      }
      if (toolbarRef.current?.contains(target) || menuRef.current?.contains(target)) {
        return;
      }
      closeMenu();
    };
    window.addEventListener("keydown", closeOnEscape);
    window.addEventListener("pointerdown", closeOnOutsidePointerDown);
    window.addEventListener("blur", closeMenu);
    return () => {
      window.removeEventListener("keydown", closeOnEscape);
      window.removeEventListener("pointerdown", closeOnOutsidePointerDown);
      window.removeEventListener("blur", closeMenu);
    };
  }, [closeMenu, menu, toolbarRef]);

  return (
    <div ref={toolbarRef} className="min-w-0 max-w-full overflow-x-auto">
      <div className="flex min-w-max items-center gap-2 pb-px">
        <div
          className="flex items-center overflow-hidden rounded border border-[#3b4a5f]"
          title={text("zoomWithWheel")}
        >
          <button
            className="h-7 min-w-7 border-r border-[#3b4a5f] px-2 text-xs text-[#c4d4e8] hover:bg-[#172131] hover:text-white"
            title={text("zoomOut")}
            type="button"
            onClick={onZoomOut}
          >
            -
          </button>
          <button
            className="h-7 min-w-[52px] border-r border-[#3b4a5f] px-2 text-xs text-[#c4d4e8] hover:bg-[#172131] hover:text-white"
            title={text("resetZoom")}
            type="button"
            onClick={onResetZoom}
          >
            {Math.round(zoom * 100)}%
          </button>
          <button
            className="h-7 min-w-7 border-r border-[#3b4a5f] px-2 text-xs text-[#c4d4e8] hover:bg-[#172131] hover:text-white"
            title={text("zoomIn")}
            type="button"
            onClick={onZoomIn}
          >
            +
          </button>
          <button
            className="h-7 min-w-[42px] px-2 text-xs text-[#c4d4e8] hover:bg-[#172131] hover:text-white disabled:cursor-not-allowed disabled:text-[#5f6d7e]"
            disabled={!canFitFlowchartWidth}
            title={text("fitWidthDescription")}
            type="button"
            onClick={onFitFlowchartWidth}
          >
            {text("fitWidth")}
          </button>
        </div>
        <div
          className="flex items-center overflow-hidden rounded border border-[#3b4a5f]"
          title={text("labelMode")}
        >
          {flowchartLabelModes.map((mode) => (
            <button
              key={mode}
              aria-pressed={labelMode === mode}
              className={`h-7 min-w-[58px] border-r border-[#3b4a5f] px-2 text-xs last:border-r-0 ${
                labelMode === mode
                  ? "bg-[#17324a] text-white"
                  : "text-[#c4d4e8] hover:bg-[#172131] hover:text-white"
              }`}
              title={text(`labelMode${flowchartLabelModeTitleSuffix(mode)}`)}
              type="button"
              onClick={() => onLabelModeChange(mode)}
            >
              {text(`labelMode${flowchartLabelModeTitleSuffix(mode)}`)}
            </button>
          ))}
        </div>
        {compactAll ? (
          <button
            aria-expanded={menu?.kind === "open"}
            aria-haspopup="menu"
            className={flowchartToolbarButtonClass}
            title={text("openMenu")}
            type="button"
            onClick={(event) => openMenu("open", event.currentTarget)}
          >
            {text("openMenu")}
          </button>
        ) : (
          <>
            <FlowchartToolbarButton
              disabled={!canOpenSection}
              label="Code"
              title={text("openCode")}
              onClick={onOpenCode}
            />
            <FlowchartToolbarButton
              disabled={!canOpenSection}
              label="Graph"
              title={text("openGraph")}
              onClick={onOpenGraph}
            />
          </>
        )}
        {compactExports ? (
          <button
            aria-expanded={menu?.kind === "export"}
            aria-haspopup="menu"
            className={flowchartToolbarButtonClass}
            title={text("exportMenu")}
            type="button"
            onClick={(event) => openMenu("export", event.currentTarget)}
          >
            {text("exportMenu")}
          </button>
        ) : (
          <>
            <FlowchartToolbarButton
              label={text("copyMermaid")}
              title={text("copyMermaid")}
              onClick={onCopyMermaid}
            />
            <FlowchartToolbarButton
              label={text("exportMermaid")}
              title={text("exportMermaid")}
              onClick={onExportMermaid}
            />
            <FlowchartToolbarButton
              disabled={!canExportSvg}
              label={text("exportSvg")}
              title={text("exportSvg")}
              onClick={onExportSvg}
            />
          </>
        )}
      </div>
      {menu ? (
        <div
          ref={menuRef}
          className="fixed z-50 grid w-[180px] overflow-hidden rounded-md border border-[#3b4a5f] bg-[#151b23] py-1 text-xs text-[#d9e0ea] shadow-[0_12px_28px_rgb(0_0_0_/_32%)]"
          role="menu"
          style={{ left: menu.left, top: menu.top }}
        >
          {menu.kind === "open" ? (
            <>
              <FlowchartToolbarMenuItem
                disabled={!canOpenSection}
                label="Code"
                onClick={() => runMenuAction(onOpenCode)}
              />
              <FlowchartToolbarMenuItem
                disabled={!canOpenSection}
                label="Graph"
                onClick={() => runMenuAction(onOpenGraph)}
              />
            </>
          ) : (
            <>
              <FlowchartToolbarMenuItem
                label={text("copyMermaid")}
                onClick={() => runMenuAction(onCopyMermaid)}
              />
              <FlowchartToolbarMenuItem
                label={text("exportMermaid")}
                onClick={() => runMenuAction(onExportMermaid)}
              />
              <FlowchartToolbarMenuItem
                disabled={!canExportSvg}
                label={text("exportSvg")}
                onClick={() => runMenuAction(onExportSvg)}
              />
            </>
          )}
        </div>
      ) : null}
    </div>
  );
}

function FlowchartToolbarButton({
  disabled,
  label,
  title,
  onClick,
}: {
  disabled?: boolean;
  label: string;
  title: string;
  onClick(): void;
}): React.ReactElement {
  return (
    <button
      className={flowchartToolbarButtonClass}
      disabled={disabled}
      title={title}
      type="button"
      onClick={onClick}
    >
      {label}
    </button>
  );
}

function FlowchartToolbarMenuItem({
  disabled,
  label,
  onClick,
}: {
  disabled?: boolean;
  label: string;
  onClick(): void;
}): React.ReactElement {
  return (
    <button
      className="px-3 py-1.5 text-left hover:bg-[#172131] disabled:cursor-not-allowed disabled:text-[#5f6d7e]"
      disabled={disabled}
      role="menuitem"
      type="button"
      onClick={onClick}
    >
      {label}
    </button>
  );
}

function flowchartToolbarMode(width: number): FlowchartToolbarMode {
  if (width > 0 && width < 520) {
    return "compactAll";
  }
  if (width > 0 && width < 760) {
    return "compactExports";
  }
  return "full";
}

const flowchartToolbarButtonClass =
  "rounded border border-[#3b4a5f] px-3 py-1 text-xs text-[#c4d4e8] hover:border-[#7dd3fc] hover:text-white disabled:cursor-not-allowed disabled:border-[#263140] disabled:text-[#5f6d7e]";
const flowchartToolbarMenuWidth = 180;

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
      element.querySelector("title")?.remove();
      element.style.cursor = "pointer";
      element.addEventListener("click", () => onOpenFlowchart(node));
      element.addEventListener("contextmenu", (event) => onOpenContextMenu(node, event));
    }
  }
}

function adjustSvgBranchNodeShapes(container: HTMLDivElement, payload: FlowchartPayload): void {
  for (const node of payload.nodes) {
    if (!isBranchFlowchartNode(node)) {
      continue;
    }
    for (const element of svgElementsForFlowchartNode(container, node)) {
      for (const polygon of element.querySelectorAll<SVGPolygonElement>("polygon")) {
        widenSvgPolygon(polygon, branchNodeHorizontalScale);
      }
    }
  }
}

function isBranchFlowchartNode(node: AspFlowchartNode): boolean {
  return (
    node.kind === "if" || node.kind === "elseif" || node.kind === "select" || node.kind === "case"
  );
}

function widenSvgPolygon(polygon: SVGPolygonElement, scaleX: number): void {
  const points = svgPolygonPoints(polygon.getAttribute("points") ?? "");
  if (points.length === 0) {
    return;
  }
  const centerX = points.reduce((sum, point) => sum + point.x, 0) / points.length;
  polygon.setAttribute(
    "points",
    points
      .map(
        (point) =>
          `${formatSvgNumber(centerX + (point.x - centerX) * scaleX)},${formatSvgNumber(point.y)}`,
      )
      .join(" "),
  );
}

function svgPolygonPoints(value: string): Array<{ x: number; y: number }> {
  return value
    .trim()
    .split(/\s+/)
    .map((point) => {
      const [rawX, rawY] = point.split(",");
      const x = Number(rawX);
      const y = Number(rawY);
      return Number.isFinite(x) && Number.isFinite(y) ? { x, y } : undefined;
    })
    .filter((point): point is { x: number; y: number } => Boolean(point));
}

function formatSvgNumber(value: number): string {
  return Number.isInteger(value) ? String(value) : value.toFixed(2).replace(/\.?0+$/, "");
}

function syncSvgSearchHighlights(
  container: HTMLDivElement,
  viewport: HTMLDivElement,
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
  if (activeElement) {
    scrollFlowchartElementIntoViewport(activeElement, viewport);
  }
}

function scrollFlowchartElementIntoViewport(
  element: SVGGraphicsElement,
  viewport: HTMLElement,
): void {
  const elementRect = element.getBoundingClientRect();
  const viewportRect = viewport.getBoundingClientRect();
  const nextLeft =
    viewport.scrollLeft +
    elementRect.left +
    elementRect.width / 2 -
    viewportRect.left -
    viewport.clientWidth / 2;
  const nextTop =
    viewport.scrollTop +
    elementRect.top +
    elementRect.height / 2 -
    viewportRect.top -
    viewport.clientHeight / 2;
  viewport.scrollTo({
    left: Math.max(0, nextLeft),
    top: Math.max(0, nextTop),
  });
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

function serializedFlowchartSvg(container: HTMLDivElement | null): string | undefined {
  const svgElement = container?.querySelector<SVGSVGElement>("svg");
  if (!svgElement) {
    return undefined;
  }
  const clone = svgElement.cloneNode(true) as SVGSVGElement;
  if (!clone.getAttribute("xmlns")) {
    clone.setAttribute("xmlns", "http://www.w3.org/2000/svg");
  }
  if (!clone.getAttribute("xmlns:xlink")) {
    clone.setAttribute("xmlns:xlink", "http://www.w3.org/1999/xlink");
  }
  return `${new XMLSerializer().serializeToString(clone)}\n`;
}

function svgElementsForFlowchartNode(
  container: HTMLDivElement,
  node: AspFlowchartNode,
): SVGGElement[] {
  const id = mermaidId(node.id);
  const elements = [...container.querySelectorAll<SVGGElement>("g[id]")];
  const nodeElements = elements.filter((element) => element.classList.contains("node"));
  const matchedNodeElements = nodeElements.filter((element) =>
    svgElementIdContainsMermaidNodeId(element.id, id),
  );
  if (matchedNodeElements.length > 0) {
    return matchedNodeElements;
  }
  return elements.filter((element) => svgElementIdContainsMermaidNodeId(element.id, id));
}

function svgElementIdContainsMermaidNodeId(elementId: string, mermaidNodeId: string): boolean {
  const index = elementId.indexOf(mermaidNodeId);
  if (index < 0) {
    return false;
  }
  const before = index === 0 ? "" : elementId[index - 1];
  const after = elementId[index + mermaidNodeId.length] ?? "";
  return isMermaidIdBoundary(before) && isMermaidIdBoundary(after);
}

function isMermaidIdBoundary(value: string): boolean {
  return !value || !/[A-Za-z0-9_]/.test(value);
}

interface FlowchartSearchMatch {
  node: AspFlowchartNode;
}

function flowchartSearchMatches(payload: FlowchartPayload, query: string): FlowchartSearchMatch[] {
  const normalizedQuery = normalizeFlowchartSearchText(query);
  if (!normalizedQuery) {
    return [];
  }
  return payload.nodes
    .filter((node) => node.kind !== "start" && node.kind !== "end")
    .filter((node) =>
      normalizeFlowchartSearchText(flowchartSearchText(node)).includes(normalizedQuery),
    )
    .map((node) => ({ node }));
}

function flowchartSearchText(node: AspFlowchartNode): string {
  return node.label;
}

function normalizeFlowchartSearchText(value: string): string {
  return value.trim().toLocaleLowerCase();
}

function flowchartLabelModeForPayload(payload: FlowchartPayload): AspFlowchartLabelMode {
  const mode = payload.labelMode ?? payload.settings?.labelMode;
  return flowchartLabelModes.includes(mode as AspFlowchartLabelMode)
    ? (mode as AspFlowchartLabelMode)
    : "normal";
}

function flowchartLabelModeTitleSuffix(mode: AspFlowchartLabelMode): string {
  switch (mode) {
    case "raw":
      return "Raw";
    case "description":
      return "Description";
    case "normal":
    default:
      return "Normal";
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
    mermaid: mermaidForSelectedSection(payload, nodes, edges, themePalette),
    stats: {
      ...payload.stats,
      sections: 1,
      nodes: nodes.length,
      edges: edges.length,
    },
  };
}

function mermaidForSelectedSection(
  payload: FlowchartPayload,
  nodes: AspFlowchartNode[],
  edges: FlowchartPayload["edges"],
  themePalette: FlowchartThemePalette,
): string {
  const labelLineLength = flowchartLabelLineLengthForPayload(payload);
  const lines = ["flowchart TB"];
  for (const node of nodes) {
    lines.push(`  ${mermaidNode(node, labelLineLength)}`);
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

function mermaidNode(node: AspFlowchartNode, labelLineLength: number): string {
  const id = mermaidId(node.id);
  const label = mermaidLabel(node.label, { lineLength: labelLineLength });
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
    escape: escapeMermaidEdgeText,
    lineLength: flowchartEdgeLabelLineLength,
    maximumCharacters: maximumFlowchartEdgeLabelCharacters,
  });
}

function mermaidLabel(
  value: string,
  options: {
    escape?: (value: string) => string;
    lineLength?: number;
    maximumCharacters?: number;
  } = {},
): string {
  const normalized = value.replace(/\s+/g, " ").trim();
  const clipped = clipFlowchartLabel(
    normalized,
    options.maximumCharacters ?? maximumFlowchartLabelCharacters,
  );
  const lines = wrapFlowchartLabel(clipped, options.lineLength ?? flowchartLabelLineLength);
  const escape = options.escape ?? escapeMermaidText;
  return (lines.length > 0 ? lines : [""]).map(escape).join("<br/>");
}

function flowchartLabelLineLengthForPayload(payload: FlowchartPayload): number {
  const value = payload.settings?.labelLineLength;
  return typeof value === "number" &&
    Number.isFinite(value) &&
    value >= minimumFlowchartLabelLineLength
    ? Math.floor(value)
    : flowchartLabelLineLength;
}

function escapeMermaidEdgeText(value: string): string {
  return value.replaceAll("<", "&lt;").replaceAll(">", "&gt;").replaceAll("|", "/").trim();
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

interface FlowchartZoomRange {
  minimum: number;
  maximum: number;
}

const flowchartViewportStyle: React.CSSProperties = {
  scrollbarGutter: "stable",
  touchAction: "none",
};

function flowchartZoomRange(payload: FlowchartPayload): FlowchartZoomRange {
  const minimum = positiveFiniteNumber(payload.settings?.minZoom, defaultMinimumFlowchartZoom);
  const maximum = positiveFiniteNumber(payload.settings?.maxZoom, defaultMaximumFlowchartZoom);
  return {
    minimum,
    maximum: Math.max(minimum, maximum),
  };
}

function roundFlowchartZoom(value: number): number {
  return Math.round(value * 100) / 100;
}

function scaledFlowchartCanvasStyle(
  svgSize: FlowchartSvgSize | undefined,
  zoom: number,
  viewportSize: FlowchartViewportSize,
): React.CSSProperties {
  if (!svgSize) {
    return {};
  }
  const scaledWidth = scaledFlowchartWidth(svgSize, zoom);
  const panGutter = flowchartHorizontalPanGutter(svgSize, zoom, viewportSize);
  return {
    width: `${Math.max(1, Math.ceil(scaledWidth + panGutter * 2))}px`,
    height: `${Math.max(1, Math.ceil(svgSize.height * zoom))}px`,
  };
}

function flowchartSvgLayerStyle(
  svgSize: FlowchartSvgSize | undefined,
  zoom: number,
  viewportSize: FlowchartViewportSize,
): React.CSSProperties {
  const style: React.CSSProperties = {
    transform: `scale(${zoom})`,
    transformOrigin: "top left",
  };
  if (!svgSize) {
    return style;
  }
  return {
    ...style,
    left: `${Math.ceil(flowchartHorizontalPanGutter(svgSize, zoom, viewportSize))}px`,
    width: `${Math.max(1, Math.ceil(svgSize.width))}px`,
    height: `${Math.max(1, Math.ceil(svgSize.height))}px`,
  };
}

function centerFlowchartHorizontally(
  viewport: HTMLElement,
  svgSize: FlowchartSvgSize,
  zoom: number,
  viewportSize: FlowchartViewportSize,
): void {
  const scaledWidth = scaledFlowchartWidth(svgSize, zoom);
  const svgLeft = flowchartHorizontalPanGutter(svgSize, zoom, viewportSize);
  viewport.scrollLeft = Math.max(0, svgLeft + scaledWidth / 2 - viewport.clientWidth / 2);
}

function flowchartHorizontalPanGutter(
  svgSize: FlowchartSvgSize,
  zoom: number,
  viewportSize: FlowchartViewportSize,
): number {
  const viewportWidth = positiveFiniteNumber(viewportSize.width, 0);
  if (!viewportWidth) {
    return 0;
  }
  const scaledWidth = scaledFlowchartWidth(svgSize, zoom);
  return Math.max(viewportWidth / 2, (viewportWidth - scaledWidth) / 2, 0);
}

function scaledFlowchartWidth(svgSize: FlowchartSvgSize, zoom: number): number {
  return Math.max(1, svgSize.width * zoom);
}

function flowchartFitWidthZoom(
  viewport: HTMLElement,
  svgSize: FlowchartSvgSize,
  zoomRange: FlowchartZoomRange,
): number | undefined {
  const availableWidth = positiveFiniteNumber(
    viewport.clientWidth - horizontalPadding(viewport),
    undefined,
  );
  if (!availableWidth) {
    return undefined;
  }
  return roundFlowchartZoom(
    clamp(availableWidth / svgSize.width, zoomRange.minimum, zoomRange.maximum),
  );
}

function horizontalPadding(element: HTMLElement): number {
  const style = window.getComputedStyle(element);
  return cssPixelValue(style.paddingLeft) + cssPixelValue(style.paddingRight);
}

function cssPixelValue(value: string): number {
  const parsed = Number.parseFloat(value);
  return Number.isFinite(parsed) ? parsed : 0;
}

function measuredFlowchartSvgSize(container: HTMLDivElement): FlowchartSvgSize | undefined {
  const svgElement = container.querySelector<SVGSVGElement>("svg");
  if (!svgElement) {
    return undefined;
  }
  const viewBox = svgElement.viewBox.baseVal;
  const viewBoxWidth = positiveFiniteNumber(viewBox.width, undefined);
  const viewBoxHeight = positiveFiniteNumber(viewBox.height, undefined);
  if (viewBoxWidth && viewBoxHeight) {
    return { width: viewBoxWidth, height: viewBoxHeight };
  }
  const attrWidth = svgLengthAttribute(svgElement.getAttribute("width"));
  const attrHeight = svgLengthAttribute(svgElement.getAttribute("height"));
  if (attrWidth && attrHeight) {
    return { width: attrWidth, height: attrHeight };
  }
  try {
    const box = svgElement.getBBox();
    const width = positiveFiniteNumber(box.width, undefined);
    const height = positiveFiniteNumber(box.height, undefined);
    return width && height ? { width, height } : undefined;
  } catch {
    return undefined;
  }
}

function svgLengthAttribute(value: string | null): number | undefined {
  if (!value || value.includes("%")) {
    return undefined;
  }
  const match = /^\s*([0-9]+(?:\.[0-9]+)?)/.exec(value);
  return positiveFiniteNumber(match ? Number(match[1]) : undefined, undefined);
}

function positiveFiniteNumber(value: unknown, fallback: number): number;
function positiveFiniteNumber(value: unknown, fallback: undefined): number | undefined;
function positiveFiniteNumber(value: unknown, fallback: number | undefined): number | undefined {
  return typeof value === "number" && Number.isFinite(value) && value > 0 ? value : fallback;
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
  labelMode?: AspFlowchartLabelMode,
): string | undefined {
  const targetRange = target.nameRange ?? target.range;
  if (target.uri && target.uri !== payload.uri) {
    vscode.postMessage({
      type: "openFlowchartLocation",
      uri: target.uri,
      range: targetRange,
      labelMode,
    });
    return undefined;
  }
  const sectionId = targetRange
    ? (sectionIdForRange(payload, targetRange) ?? defaultSectionId(payload))
    : defaultSectionId(payload);
  setSelectedSectionId(sectionId);
  return sectionId;
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

function flowchartNodeForRange(
  payload: FlowchartPayload,
  range: NonNullable<AspFlowchartTarget["range"]>,
): AspFlowchartNode | undefined {
  return (
    bestFlowchartNodeForRange(payload, range, (nodeRange) => rangeContains(nodeRange, range)) ??
    bestFlowchartNodeForRange(payload, range, (nodeRange) => rangesOverlap(nodeRange, range))
  );
}

function bestFlowchartNodeForRange(
  payload: FlowchartPayload,
  range: NonNullable<AspFlowchartTarget["range"]>,
  matches: (nodeRange: NonNullable<AspFlowchartNode["range"]>) => boolean,
): AspFlowchartNode | undefined {
  return payload.nodes
    .filter((node) => node.kind !== "start" && node.kind !== "end")
    .filter((node): node is AspFlowchartNode & { range: NonNullable<AspFlowchartNode["range"]> } =>
      Boolean(node.range && matches(node.range)),
    )
    .sort((left, right) => rangeLength(left.range) - rangeLength(right.range))[0];
}

function rangeContains(
  outer: NonNullable<AspFlowchartNode["range"]>,
  inner: NonNullable<AspFlowchartTarget["range"]>,
): boolean {
  return (
    positionBeforeOrEqual(outer.start, inner.start) && positionBeforeOrEqual(inner.end, outer.end)
  );
}

function rangesOverlap(
  left: NonNullable<AspFlowchartNode["range"]>,
  right: NonNullable<AspFlowchartTarget["range"]>,
): boolean {
  return (
    positionBeforeOrEqual(left.start, right.end) && positionBeforeOrEqual(right.start, left.end)
  );
}

function rangeLength(range: NonNullable<AspFlowchartNode["range"]>): number {
  return (
    (range.end.line - range.start.line) * 1_000_000 + range.end.character - range.start.character
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

function flowchartMaxEdges(payload: FlowchartPayload): number {
  const value = payload.settings?.maxEdges;
  return typeof value === "number" && Number.isFinite(value) && value >= 1
    ? Math.floor(value)
    : defaultFlowchartMaxEdges;
}

const style = document.createElement("style");
style.textContent = tailwindStyles;
document.head.append(style);

createRoot(document.getElementById("root")!).render(<App />);
