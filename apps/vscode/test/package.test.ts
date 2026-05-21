import fs from "node:fs";
import { describe, expect, it } from "vitest";

describe("VS Code extension package", () => {
  it("keeps the language server as a runtime dependency", () => {
    const manifest = JSON.parse(fs.readFileSync("package.json", "utf8")) as {
      dependencies?: Record<string, string>;
      devDependencies?: Record<string, string>;
    };
    expect(manifest.dependencies?.["@asp-lsp/language-server"]).toBe("workspace:*");
    expect(manifest.devDependencies?.["@asp-lsp/language-server"]).toBeUndefined();
  });
});
