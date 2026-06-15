import type React from "react";
import type {
  AspFlowchartLabelMode,
  AspFlowchartNode,
  AspFlowchartNodeLink,
  AspFlowchartPayload,
  AspFlowchartSection,
} from "@asp-lsp/core";

type FlowchartLocale = "en" | "ja";
type InfoPanelPosition = "left" | "right";
type FlowchartNodeKind = AspFlowchartNode["kind"];
type FlowchartNodeLinkRole = AspFlowchartNodeLink["role"];

interface FlowchartPayload extends AspFlowchartPayload {
  locale?: FlowchartLocale;
  settings?: {
    maxTextSize?: number;
    maxEdges?: number;
    labelLineLength?: number;
    labelMode?: AspFlowchartLabelMode;
    minZoom?: number;
    maxZoom?: number;
    theme?: "light" | "dark" | "auto";
    infoPanelPosition?: InfoPanelPosition;
    showSourcePanel?: boolean;
  };
}

interface FlowchartVisualStyle {
  background: string;
  border: string;
  text: string;
  mermaidClass: string;
}

interface FlowchartThemePalette {
  mermaidTheme: "base" | "dark";
  mermaidThemeVariables?: Record<string, string>;
  nodeKindStyles: Record<FlowchartNodeKind, FlowchartVisualStyle>;
  linkRoleStyles: Record<FlowchartNodeLinkRole, FlowchartVisualStyle>;
  symbolKindStyles: Record<string, FlowchartVisualStyle>;
}

interface FlowchartViewportSize {
  width: number;
  height: number;
}

interface FlowchartSvgSize {
  width: number;
  height: number;
}

interface FlowchartSearchMatch {
  node: AspFlowchartNode;
}

interface FlowchartZoomRange {
  minimum: number;
  maximum: number;
}

const flowchartLabelLineLength = 34;
const flowchartEdgeLabelLineLength = 22;
const maximumFlowchartLabelCharacters = 180;
const maximumFlowchartEdgeLabelCharacters = 80;
const minimumFlowchartLabelLineLength = 8;
const defaultMinimumFlowchartZoom = 0.1;
const defaultMaximumFlowchartZoom = 4;
const flowchartPanelMinimumWidth = 320;
const flowchartPanelMaximumWidth = 620;
const flowchartCanvasMinimumWidth = 360;
const flowchartPaneResizeHandleWidth = 6;
const flowchartSourcePanelDefaultWidth = 420;
const flowchartSourcePanelMinimumWidth = 280;
const flowchartSourcePanelMaximumWidth = 720;
const flowchartLabelModes: AspFlowchartLabelMode[] = ["raw", "normal", "description"];

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
    exceptionHandling: "Exception handling",
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
    exceptionHandling: "例外処理",
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

export function flowchartSearchMatches(
  payload: FlowchartPayload,
  query: string,
): FlowchartSearchMatch[] {
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

export function flowchartLabelModeForPayload(payload: FlowchartPayload): AspFlowchartLabelMode {
  const mode = payload.labelMode ?? payload.settings?.labelMode;
  return flowchartLabelModes.includes(mode as AspFlowchartLabelMode)
    ? (mode as AspFlowchartLabelMode)
    : "normal";
}

export function flowchartLabelModeTitleSuffix(mode: AspFlowchartLabelMode): string {
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

export function defaultSectionId(payload: FlowchartPayload): string | undefined {
  return (
    payload.sections.find((section) =>
      section.nodeIds.some((nodeId) => {
        const node = payload.nodes.find((candidate) => candidate.id === nodeId);
        return node && node.kind !== "start" && node.kind !== "end";
      }),
    ) ?? payload.sections[0]
  )?.id;
}

export function flowchartForSection(
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

export function mermaidForSelectedSection(
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

export function mermaidNode(node: AspFlowchartNode, labelLineLength: number): string {
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

export function flowchartMermaidClassDefinitions(themePalette: FlowchartThemePalette): string[] {
  const stylesByClass = new Map<string, FlowchartVisualStyle>();
  for (const style of Object.values(themePalette.nodeKindStyles)) {
    stylesByClass.set(style.mermaidClass, style);
  }
  return [...stylesByClass.values()].map(
    (style) =>
      `  classDef ${style.mermaidClass} fill:${style.background},stroke:${style.border},stroke-width:2px,color:${style.text};`,
  );
}

export function mermaidId(id: string): string {
  return id.replace(/[^A-Za-z0-9_]/g, "_");
}

export function escapeMermaidText(value: string): string {
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

export function flowchartSectionHint(
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

export function flowchartNodeHint(
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

export function flowchartNodeLinkHint(
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

export function flowchartNodeKindLabel(kind: FlowchartNodeKind, locale: FlowchartLocale): string {
  return flowchartNodeKindLabels[locale][kind] ?? flowchartNodeKindLabels.en[kind] ?? kind;
}

export function flowchartSectionKindLabel(
  kind: AspFlowchartSection["kind"],
  locale: FlowchartLocale,
): string {
  return flowchartSectionKindLabels[locale][kind] ?? flowchartSectionKindLabels.en[kind] ?? kind;
}

export function flowchartNodeLinkRoleLabel(
  role: FlowchartNodeLinkRole,
  locale: FlowchartLocale,
): string {
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

export function flowchartNodeLinkStyle(
  link: AspFlowchartNodeLink,
  themePalette: FlowchartThemePalette,
): FlowchartVisualStyle {
  return (
    flowchartSymbolStyle(link.symbolKind, themePalette) ?? themePalette.linkRoleStyles[link.role]
  );
}

export function flowchartSymbolStyle(
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

export function normalizeFlowchartSymbolKind(symbolKind: string | undefined): string {
  return symbolKind ? symbolKind.replace(/[^A-Za-z]/g, "").toLowerCase() : "";
}

export function formatFlowchartSymbolKind(symbolKind: string): string {
  return symbolKind
    .replace(/([a-z])([A-Z])/g, "$1 $2")
    .replace(/[_-]+/g, " ")
    .trim();
}

export function flowchartSwatchStyle(style: FlowchartVisualStyle): React.CSSProperties {
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

export function flowchartZoomRange(payload: FlowchartPayload): FlowchartZoomRange {
  const minimum = positiveFiniteNumber(payload.settings?.minZoom, defaultMinimumFlowchartZoom);
  const maximum = positiveFiniteNumber(payload.settings?.maxZoom, defaultMaximumFlowchartZoom);
  return {
    minimum,
    maximum: Math.max(minimum, maximum),
  };
}

export function roundFlowchartZoom(value: number): number {
  return Math.round(value * 100) / 100;
}

export function scaledFlowchartCanvasStyle(
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

export function flowchartSvgLayerStyle(
  svgSize: FlowchartSvgSize | undefined,
  zoom: number,
  viewportSize: FlowchartViewportSize,
): React.CSSProperties {
  const style: React.CSSProperties = {
    transform: `scale(${zoom})`,
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

export function centerFlowchartHorizontally(
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

export function flowchartFitWidthZoom(
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

export function measuredFlowchartSvgSize(container: HTMLDivElement): FlowchartSvgSize | undefined {
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

export function detailParts(...parts: Array<string | undefined>): string {
  return parts.filter((part): part is string => Boolean(part && part.trim())).join(" · ");
}

export function clamp(value: number, minimum: number, maximum: number): number {
  return Math.min(maximum, Math.max(minimum, value));
}

export function maxFlowchartInfoPanelWidthForLayout(containerWidth: number): number {
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

export function maxFlowchartSourcePanelWidthForLayout(containerWidth: number): number {
  if (containerWidth <= 0) {
    return flowchartSourcePanelDefaultWidth;
  }
  return Math.max(
    flowchartSourcePanelMinimumWidth,
    Math.min(
      flowchartSourcePanelMaximumWidth,
      Math.floor(containerWidth * 0.42),
      containerWidth - flowchartCanvasMinimumWidth - flowchartPaneResizeHandleWidth * 2,
    ),
  );
}

export function modulo(value: number, divisor: number): number {
  return ((value % divisor) + divisor) % divisor;
}
