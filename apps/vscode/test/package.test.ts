import fs from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";
import { getServerModulePath } from "../src/server-path";

describe("VS Code extension package", () => {
  it("keeps the language server as a runtime dependency", () => {
    const manifest = JSON.parse(fs.readFileSync("package.json", "utf8")) as {
      dependencies?: Record<string, string>;
      devDependencies?: Record<string, string>;
    };
    expect(manifest.dependencies?.["@asp-lsp/language-server"]).toBe("workspace:*");
    expect(manifest.devDependencies?.["@asp-lsp/language-server"]).toBeUndefined();
  });

  it("resolves the packaged language server module path", () => {
    const root = process.cwd();
    const serverModule = getServerModulePath({
      asAbsolutePath: (relativePath) => path.join(root, relativePath),
    });
    expect(serverModule).toBe(
      path.join(root, "node_modules", "@asp-lsp", "language-server", "dist", "server.js"),
    );
    expect(fs.existsSync(serverModule)).toBe(true);
  });
});
