import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { execFileSync } from "node:child_process";
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

  it("contributes commands, task definition and IIS debug settings", () => {
    const manifest = JSON.parse(fs.readFileSync("package.json", "utf8")) as {
      contributes?: {
        commands?: Array<{ command: string }>;
        taskDefinitions?: Array<{ type: string }>;
        configuration?: { properties?: Record<string, unknown> };
      };
    };
    const commands = manifest.contributes?.commands?.map((command) => command.command) ?? [];
    expect(commands).toContain("aspLsp.restartServer");
    expect(commands).toContain("aspLsp.reindexWorkspace");
    expect(commands).toContain("aspLsp.openOutput");
    expect(commands).toContain("aspLsp.debugIisUrl");
    expect(manifest.contributes?.taskDefinitions?.some((task) => task.type === "asp-lsp")).toBe(
      true,
    );
    expect(manifest.contributes?.configuration?.properties?.["aspLsp.iis.url"]).toBeTruthy();
    expect(manifest.contributes?.configuration?.properties?.["aspLsp.iis.browser"]).toBeTruthy();
  });

  it("resolves the packaged language server module path", () => {
    const root = process.cwd();
    const serverModule = getServerModulePath({
      asAbsolutePath: (relativePath) => path.join(root, relativePath),
    });
    expect(serverModule).toBe(path.join(root, "server", "language-server", "dist", "server.js"));
    expect(fs.existsSync(serverModule)).toBe(true);
  });

  it("packages a VSIX with the language server entrypoint", () => {
    const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-vsix-"));
    const vsixPath = path.join(tempDir, "classic-asp-lsp.vsix");
    try {
      execFileSync(
        path.join("node_modules", ".bin", "vsce"),
        [
          "package",
          "--allow-missing-repository",
          "--no-dependencies",
          "--follow-symlinks",
          "--out",
          vsixPath,
        ],
        { stdio: "pipe" },
      );
      expect(fs.existsSync(vsixPath)).toBe(true);
      const listing = execFileSync("unzip", ["-l", vsixPath], { encoding: "utf8" });
      expect(listing).toContain("extension/dist/extension.js");
      expect(listing).toContain("extension/server/language-server/dist/server.js");
      expect(listing).toContain("extension/server/language-server/node_modules/@asp-lsp/core");
      expect(listing).toContain(
        "extension/server/language-server/node_modules/vscode-languageserver",
      );
    } finally {
      fs.rmSync(tempDir, { recursive: true, force: true });
    }
  });
});
