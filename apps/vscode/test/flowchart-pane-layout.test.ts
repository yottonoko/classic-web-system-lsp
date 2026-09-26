import { describe, it, expect } from "vitest";
import { flowchartPaneLayout } from "../src/webview/flowchart-model";

describe("flowchart pane allocation", () => {
  it("reserves the canvas when both side panes are large", () => {
    for (const width of [962, 1000, 1200, 1440]) {
      const panes = flowchartPaneLayout(width, 620, 720, true);
      expect(panes.compact).toBe(false);
      expect(width - panes.infoWidth - panes.sourceWidth - 2).toBeGreaterThanOrEqual(359.999);
      expect(panes.infoWidth).toBeGreaterThanOrEqual(320);
      expect(panes.sourceWidth).toBeGreaterThanOrEqual(280);
    }
  });
  it("uses tabs when the minimum panes cannot fit and restores requested sizes on wider screens", () => {
    expect(flowchartPaneLayout(800, 380, 420, true).compact).toBe(true);
    expect(flowchartPaneLayout(375, 380, 420, false).compact).toBe(true);
    expect(flowchartPaneLayout(800, 380, 420, false).compact).toBe(false);
    expect(flowchartPaneLayout(1440, 380, 420, true)).toMatchObject({
      infoWidth: 380,
      sourceWidth: 420,
      compact: false,
    });
  });
});
