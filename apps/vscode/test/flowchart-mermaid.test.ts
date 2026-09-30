import { describe, expect, it } from "vitest";
import { mermaidForSelectedSection } from "../src/webview/flowchart-model";
import { flowchartThemePalettes } from "../src/webview/flowchart-theme";
import type { FlowchartPayload } from "../src/webview/flowchart-types";

describe("flowchart Mermaid source", () => {
  it("keeps quoted Case values inside edge labels", () => {
    const range = { start: { line: 0, character: 0 }, end: { line: 0, character: 1 } };
    const nodes = [
      { id: "node-1", kind: "select", label: "metricKey", range },
      { id: "node-2", kind: "case", label: 'When "active"', range },
    ] as FlowchartPayload["nodes"];
    const edges = [
      { id: "edge-1", source: "node-1", target: "node-2", label: 'When "active" | "x"' },
    ] as FlowchartPayload["edges"];
    const source = mermaidForSelectedSection(
      { nodes, edges } as FlowchartPayload,
      nodes,
      edges,
      flowchartThemePalettes.dark,
    );
    expect(source).toContain('node_1 -->|"When “active” / “x”"| node_2');
  });
});
