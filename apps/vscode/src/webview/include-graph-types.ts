import type SpriteText from "three-spritetext";
import type { IGrammar } from "vscode-textmate";
import type {
  AspGraphLink,
  AspGraphLinkFilterCategory,
  AspGraphNode,
  AspGraphNodeCategory,
  AspGraphPayload,
  AspGraphRange,
  AspGraphSourceRangeResponseItem,
} from "../include-graph-webview";

export type ViewMode = "3d" | "2d";

export type NodeColorCategory = AspGraphNodeCategory;

export type GraphNode = AspGraphNode & {
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
  fx?: number;
  fy?: number;
  fz?: number;
};

export type GraphLink = Omit<AspGraphLink, "source" | "target"> & {
  source: string | GraphNode;
  target: string | GraphNode;
  color: string;
};

export type GraphData = {
  nodes: GraphNode[];
  links: GraphLink[];
};

export type LegacyImplicitGlobalNodeFields = {
  implicitLocal?: boolean;
  unresolvedGlobal?: boolean;
};

export interface OpenFlowchartMessage {
  type: "openFlowchart";
  uri: string;
  range?: AspGraphRange;
}

export type WebviewTheme = "light" | "dark";
export type WebviewThemeSetting = WebviewTheme | "auto";
export type InfoPanelPosition = "left" | "right";

export interface GraphThemePalette {
  canvasBackground: string;
  mutedLink: string;
  mutedNode: string;
  nodeColors: Record<NodeColorCategory, string>;
  linkFilterColors: Record<LinkFilterCategory, string>;
}

export function isFileLikeGraphNode(node: Pick<AspGraphNode, "kind">): boolean {
  return node.kind === "file" || node.kind === "missingInclude";
}

export type Selection =
  | { type: "node"; item: GraphNode }
  | { type: "link"; item: GraphLink }
  | undefined;

export type GraphStatsMetric = "files" | "declarations" | "links" | "missingIncludes";

export type GraphStatsTarget = { type: "node"; id: string } | { type: "link"; id: string };

export interface GraphStatsListItem {
  id: string;
  title: string;
  target: GraphStatsTarget;
  detail?: string;
  status?: string;
  color?: string;
  lineWidth?: number;
}

export type HighlightState = {
  activeNodeIds: Set<string>;
  activeLinkIds: Set<string>;
};

export type LinkFilterCategory = AspGraphLinkFilterCategory;

export type CenteredSpriteText = SpriteText & {
  center: {
    y: number;
  };
};

export interface PositionSyncEntry {
  id: string;
  sourceMode: ViewMode;
  x?: number;
  y?: number;
  z?: number;
  screenX?: number;
  screenY?: number;
  cameraDistance?: number;
}

export interface PendingPositionSync {
  from: ViewMode;
  to: ViewMode;
  generation: number;
  entries: Map<string, PositionSyncEntry>;
}

export interface PositionSyncTransform {
  centerX: number;
  centerY: number;
  scale: number;
}

export interface SourceRangesMessage {
  type: "sourceRanges";
  requestId: string;
  items: AspGraphSourceRangeResponseItem[];
}

export interface GraphUpdatedMessage {
  type: "graphUpdated";
  payload?: AspGraphPayload;
  final?: boolean;
  error?: string;
}

export type SnippetLanguage = "classic-asp" | "vbscript";

export type SnippetHighlightState =
  | { status: "loading" }
  | { status: "ready"; highlighter: SnippetHighlighter }
  | { status: "failed" };

export interface SnippetHighlighter {
  classicAsp: IGrammar;
  vbscript: IGrammar;
}

export interface HighlightOffsets {
  start: number;
  end: number;
}

export type GraphLocale = "en" | "ja";
