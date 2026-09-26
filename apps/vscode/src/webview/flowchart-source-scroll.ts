import type { FlowchartSourceHighlight, FlowchartSourceScrollTarget } from "./flowchart-types";

export interface FlowchartSourceScrollContext {
  activeNodeId?: string;
  hoveredNodeId?: string;
  sectionId?: string;
  sectionSequence: number;
  uri: string;
}

export function flowchartSourceScrollTarget(
  highlights: readonly FlowchartSourceHighlight[],
  context: FlowchartSourceScrollContext,
): FlowchartSourceScrollTarget | undefined {
  const selection = highlights.find((highlight) => highlight.kind === "selection");
  if (selection) {
    return {
      kind: "selection",
      key: `selection:${context.uri}:${context.activeNodeId ?? ""}`,
      ranges: selection.ranges,
    };
  }
  const hovered = highlights.find((highlight) => highlight.kind === "hover");
  if (hovered) {
    return {
      kind: "hover",
      key: `hover:${context.uri}:${context.hoveredNodeId ?? ""}`,
      ranges: hovered.ranges,
    };
  }
  const section = highlights.find((highlight) => highlight.kind === "section");
  if (!section) {
    return undefined;
  }
  return {
    kind: "section",
    key: `section:${context.uri}:${context.sectionId ?? ""}:${context.sectionSequence}`,
    ranges: section.ranges,
  };
}

export function shouldScrollFlowchartSource(
  consumedKeys: ReadonlySet<string>,
  target: FlowchartSourceScrollTarget | undefined,
): boolean {
  return Boolean(target && !consumedKeys.has(target.key));
}
