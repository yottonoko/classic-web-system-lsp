import type {
  AspFlowchartLabelMode,
  AspFlowchartNode,
  AspFlowchartNodeLink,
  AspFlowchartPayload,
} from "@asp-lsp/core";

export type FlowchartLocale = "en" | "ja";
export type WebviewTheme = "light" | "dark";
export type WebviewThemeSetting = WebviewTheme | "auto";
export type InfoPanelPosition = "left" | "right";
export type FlowchartSourceActiveKind = "hover" | "selection" | "section";
export type FlowchartSourceRange = NonNullable<AspFlowchartNode["range"]>;

export interface FlowchartSourceHighlight {
  kind: FlowchartSourceActiveKind;
  label?: string;
  ranges: FlowchartSourceRange[];
}

export interface FlowchartSourceScrollTarget {
  kind: FlowchartSourceActiveKind;
  key: string;
  ranges: FlowchartSourceRange[];
}

export interface FlowchartPayload extends AspFlowchartPayload {
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
    showSourcePanel?: boolean;
  };
}

export type FlowchartNodeKind = AspFlowchartNode["kind"];
export type FlowchartNodeLinkRole = AspFlowchartNodeLink["role"];

export interface FlowchartVisualStyle {
  background: string;
  border: string;
  mermaidClass: string;
  text: string;
}

export interface FlowchartPanState {
  pointerId: number;
  startX: number;
  startY: number;
  scrollLeft: number;
  scrollTop: number;
  moved: boolean;
}

export interface FlowchartViewportSize {
  width: number;
  height: number;
}

export type FlowchartToolbarMode = "full" | "compactExports" | "compactAll";
export type FlowchartToolbarMenuKind = "open" | "export";

export interface FlowchartToolbarMenuState {
  kind: FlowchartToolbarMenuKind;
  left: number;
  top: number;
}

export interface FlowchartThemePalette {
  mermaidTheme: "base" | "dark";
  mermaidThemeVariables?: Record<string, string>;
  nodeKindStyles: Record<FlowchartNodeKind, FlowchartVisualStyle>;
  linkRoleStyles: Record<FlowchartNodeLinkRole, FlowchartVisualStyle>;
  symbolKindStyles: Record<string, FlowchartVisualStyle>;
}
