import { describe, it, expect, vi } from "vitest";
import fs from "node:fs";
import path from "node:path";
const mock = vi.hoisted(() => ({
  panel: { webview: { html: "", asWebviewUri: (uri: string) => uri }, dispose: vi.fn() },
  create: vi.fn(),
}));
vi.mock("vscode", () => ({
  Uri: { joinPath: (root: string, ...parts: string[]) => [root, ...parts].join("/") },
  ViewColumn: { Active: 1 },
  window: { createWebviewPanel: mock.create },
}));
import { showLogAnalysisWebview } from "../src/log-analysis-webview";
describe("command-only log viewer", () => {
  it("opens a localized, script-restricted panel without exposing extra menus", () => {
    mock.create.mockReturnValue(mock.panel);
    const context = { extensionUri: "extension", subscriptions: [] };
    showLogAnalysisWebview(context as never, "ja");
    expect(mock.create).toHaveBeenCalledWith(
      "aspLsp.logAnalysis",
      "Classic ASP: デバッグログ解析",
      1,
      expect.objectContaining({
        enableScripts: true,
        localResourceRoots: ["extension/dist/webview"],
      }),
    );
    expect(context.subscriptions).toContain(mock.panel);
    expect(mock.panel.webview.html).toContain('lang="ja"');
    expect(mock.panel.webview.html).toContain("default-src 'none'");
    expect(mock.panel.webview.html).not.toContain("unsafe-eval");
    expect(mock.panel.webview.html).toMatch(
      /nonce="[a-f0-9]{48}" src="extension\/dist\/webview\/log-analysis.js"/,
    );
    const manifest = JSON.parse(
      fs.readFileSync(path.resolve(import.meta.dirname, "../package.json"), "utf8"),
    );
    expect(
      manifest.contributes.commands.some(
        (entry: { command: string }) => entry.command === "aspLsp.analyzeDebugLog",
      ),
    ).toBe(true);
    expect(JSON.stringify(manifest.contributes.menus)).not.toContain("aspLsp.analyzeDebugLog");
  });
});
