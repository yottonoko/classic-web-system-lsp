import { describe, expect, it } from "vitest";
import { flowchartMessages } from "../src/webview/flowchart-i18n";

describe("flowchart localization", () => {
  it("localizes visible Japanese graph actions", () => {
    expect(flowchartMessages.ja.code).toBe("コード");
    expect(flowchartMessages.ja.missing).not.toBe(flowchartMessages.en.missing);
    expect(flowchartMessages.ja.openMenu).not.toBe(flowchartMessages.en.openMenu);
    expect(flowchartMessages.ja.exportMenu).not.toBe(flowchartMessages.en.exportMenu);
  });
});
