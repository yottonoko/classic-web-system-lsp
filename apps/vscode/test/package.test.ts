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
      repository?: { url?: string };
      icon?: string;
      galleryBanner?: { color?: string };
      dependencies?: Record<string, string>;
      contributes?: {
        languages?: Array<{ id: string; extensions?: string[] }>;
        grammars?: Array<{ language?: string; scopeName?: string; path?: string }>;
        configurationDefaults?: {
          "editor.tokenColorCustomizations"?: {
            textMateRules?: Array<{ scope?: string; settings?: Record<string, unknown> }>;
          };
        };
        commands?: Array<{ command: string }>;
        keybindings?: Array<{ command?: string; key?: string; mac?: string; when?: string }>;
        problemMatchers?: Array<{ name: string }>;
        taskDefinitions?: Array<{ type: string }>;
        configuration?: { properties?: Record<string, unknown> };
      };
      capabilities?: { untrustedWorkspaces?: { supported?: boolean } };
    };
    const rootManifest = JSON.parse(fs.readFileSync("../../package.json", "utf8")) as {
      license?: string;
    };
    const commands = manifest.contributes?.commands?.map((command) => command.command) ?? [];
    const keybindings = manifest.contributes?.keybindings ?? [];
    expect(rootManifest.license).toBe("MIT OR Apache-2.0");
    expect(manifest.license).toBe("MIT OR Apache-2.0");
    expect(fs.existsSync("../../LICENSE-MIT")).toBe(true);
    expect(fs.existsSync("../../LICENSE-APACHE")).toBe(true);
    expect(manifest.dependencies?.["@asp-lsp/core"]).toBe("workspace:*");
    const readme = fs.readFileSync("README.md", "utf8");
    expect(readme).toContain("## License");
    expect(readme).toContain("MIT License");
    expect(readme).toContain("Apache License, Version 2.0");
    expect(commands).toContain("aspLsp.restartServer");
    expect(commands).toContain("aspLsp.reindexWorkspace");
    expect(commands).toContain("aspLsp.openOutput");
    expect(commands).toContain("aspLsp.debugIisUrl");
    expect(commands).toContain("aspLsp.debugIisExpressUrl");
    expect(commands).toContain("aspLsp.createLaunchConfig");
    expect(manifest.contributes?.taskDefinitions?.some((task) => task.type === "asp-lsp")).toBe(
      true,
    );
    expect(
      manifest.contributes?.problemMatchers?.some((matcher) => matcher.name === "asp-lsp"),
    ).toBe(true);
    expect(manifest.contributes?.configuration?.properties?.["aspLsp.iis.url"]).toBeTruthy();
    expect(manifest.contributes?.configuration?.properties?.["aspLsp.iis.browser"]).toBeTruthy();
    expect(manifest.contributes?.configuration?.properties?.["aspLsp.iisExpress.url"]).toBeTruthy();
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.vbscript.unusedDiagnostics"],
    ).toBeTruthy();
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.vbscript.includeSuggestions"],
    ).toBeTruthy();
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.javascript.unusedDiagnostics"],
    ).toBeTruthy();
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.javascript.autoImports"],
    ).toBeTruthy();
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.javascript.ignoreProjectConfig"],
    ).toBeTruthy();
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.inlayHints.implicitByRef"],
    ).toBeTruthy();
    expect(manifest.contributes?.configuration?.properties?.["aspLsp.locale"]).toBeTruthy();
    expect(manifest.repository?.url).toContain("github.com/yottonoko/asp-lsp");
    expect(manifest.icon).toBe("assets/icon.png");
    expect(fs.existsSync(manifest.icon ?? "")).toBe(true);
    expect(manifest.galleryBanner?.color).toBeTruthy();
    expect(manifest.capabilities?.untrustedWorkspaces?.supported).toBe(true);
    const extensionSource = fs.readFileSync("src/extension.ts", "utf8");
    expect(extensionSource).toContain('registerCommand("aspLsp.showReferences"');
    expect(extensionSource).toContain('"editor.action.showReferences"');
    expect(extensionSource).toContain('registerCommand("aspLsp.toggleLineComment"');
    expect(keybindings).toContainEqual(
      expect.objectContaining({
        command: "aspLsp.toggleLineComment",
        key: "ctrl+/",
        mac: "cmd+/",
        when: "editorTextFocus && editorLangId == classic-asp",
      }),
    );
    const languageConfiguration = JSON.parse(
      fs.readFileSync("language-configuration.json", "utf8"),
    ) as { comments?: { blockComment?: string[]; lineComment?: string } };
    expect(languageConfiguration.comments?.blockComment).toEqual(["<!--", "-->"]);
    expect(languageConfiguration.comments?.lineComment).toBeUndefined();
    const vbscriptLanguage = manifest.contributes?.languages?.find(
      (language) => language.id === "vbscript",
    );
    expect(vbscriptLanguage).toBeTruthy();
    expect(vbscriptLanguage?.extensions).toBeUndefined();
    expect(
      manifest.contributes?.grammars?.some(
        (grammar) =>
          grammar.language === "vbscript" &&
          grammar.scopeName === "source.vbscript" &&
          grammar.path === "./syntaxes/vbscript.tmLanguage.json",
      ),
    ).toBe(true);
    const outputLanguage = manifest.contributes?.languages?.find(
      (language) => language.id === "asp-lsp-output",
    );
    expect(outputLanguage).toBeTruthy();
    expect(outputLanguage?.extensions).toBeUndefined();
    expect(
      manifest.contributes?.grammars?.some(
        (grammar) =>
          grammar.language === "asp-lsp-output" &&
          grammar.scopeName === "source.asp-lsp-output" &&
          grammar.path === "./syntaxes/asp-lsp-output.tmLanguage.json",
      ),
    ).toBe(true);
    expect(fs.existsSync("syntaxes/asp-lsp-output.tmLanguage.json")).toBe(true);
    const outputGrammarText = fs.readFileSync("syntaxes/asp-lsp-output.tmLanguage.json", "utf8");
    const outputGrammar = JSON.parse(outputGrammarText) as {
      repository?: {
        duration?: {
          patterns?: Array<{ match?: string; name?: string }>;
        };
      };
    };
    expect(outputGrammarText).toContain("markup.underline.link.uri.asp-lsp-output");
    expect(outputGrammarText).toContain("constant.numeric.duration.asp-lsp-output.fast");
    expect(outputGrammarText).toContain("constant.numeric.duration.asp-lsp-output.medium");
    expect(outputGrammarText).toContain("constant.numeric.duration.asp-lsp-output.slow");
    expect(outputGrammarText).toContain("constant.numeric.duration.asp-lsp-output.hot");
    expect(outputGrammarText).not.toContain("heat=");
    expect(outputGrammarText).not.toContain("duration-00");
    const durationScope = (text: string) =>
      outputGrammar.repository?.duration?.patterns?.find(
        (pattern) => pattern.match && new RegExp(pattern.match).test(text),
      )?.name;
    expect(durationScope("in 50.0 ms")).toBe("constant.numeric.duration.asp-lsp-output.fast");
    expect(durationScope("in 50.1 ms")).toBe("constant.numeric.duration.asp-lsp-output.medium");
    expect(durationScope("in 100.0 ms")).toBe("constant.numeric.duration.asp-lsp-output.medium");
    expect(durationScope("in 100.1 ms")).toBe("constant.numeric.duration.asp-lsp-output.slow");
    expect(durationScope("in 200.0 ms")).toBe("constant.numeric.duration.asp-lsp-output.slow");
    expect(durationScope("in 200.1 ms")).toBe("constant.numeric.duration.asp-lsp-output.hot");
    const outputRules =
      manifest.contributes?.configurationDefaults?.["editor.tokenColorCustomizations"]
        ?.textMateRules ?? [];
    expect(outputRules).toContainEqual(
      expect.objectContaining({ scope: "markup.underline.link.uri.asp-lsp-output" }),
    );
    expect(outputRules).toContainEqual(
      expect.objectContaining({ scope: "constant.numeric.duration.asp-lsp-output" }),
    );
    const colorByScope = new Map(
      outputRules.map((rule) => [rule.scope, rule.settings?.foreground]),
    );
    expect(colorByScope.get("markup.underline.link.uri.asp-lsp-output")).toBe("#40D86A");
    expect(colorByScope.get("constant.numeric.duration.asp-lsp-output")).toBe("#8A8A8A");
    expect(colorByScope.get("constant.numeric.duration.asp-lsp-output.fast")).toBe("#40D86A");
    expect(colorByScope.get("constant.numeric.duration.asp-lsp-output.medium")).toBe("#F0C33A");
    expect(colorByScope.get("constant.numeric.duration.asp-lsp-output.slow")).toBe("#F79333");
    expect(colorByScope.get("constant.numeric.duration.asp-lsp-output.hot")).toBe("#E84545");
    expect(outputRules.map((rule) => rule.scope)).toEqual(
      expect.arrayContaining([
        "constant.numeric.duration.asp-lsp-output.fast",
        "constant.numeric.duration.asp-lsp-output.medium",
        "constant.numeric.duration.asp-lsp-output.slow",
        "constant.numeric.duration.asp-lsp-output.hot",
      ]),
    );
    expect(
      outputRules.some((rule) =>
        rule.scope?.startsWith("constant.numeric.duration.heat.duration-"),
      ),
    ).toBe(false);
  });

  it("keeps package localization keys resolved", () => {
    const manifestText = fs.readFileSync("package.json", "utf8");
    const nls = JSON.parse(fs.readFileSync("package.nls.json", "utf8")) as Record<string, string>;
    const nlsJa = JSON.parse(fs.readFileSync("package.nls.ja.json", "utf8")) as Record<
      string,
      string
    >;
    const keys = [...manifestText.matchAll(/%([A-Za-z0-9_.]+)%/g)].map((match) => match[1]);
    expect(keys).toContain("extension.description");
    expect(keys).toContain("command.restartServer.title");
    expect(keys).toContain("configuration.locale.description");
    for (const key of keys) {
      expect(nls[key], key).toBeTruthy();
      expect(nlsJa[key], key).toBeTruthy();
    }
  });

  it("highlights common VBScript declaration keywords", () => {
    const grammar = JSON.parse(fs.readFileSync("syntaxes/vbscript.tmLanguage.json", "utf8")) as {
      repository?: {
        "vbscript-basic"?: {
          patterns?: Array<{ match?: string; name?: string }>;
        };
      };
    };
    const keywordPattern = grammar.repository?.["vbscript-basic"]?.patterns?.find(
      (pattern) => pattern.name === "keyword.control.vbscript",
    )?.match;
    expect(keywordPattern).toBeTruthy();
    expect(keywordPattern).toContain("(?i)");
    expect(keywordPattern).toContain("Public");
    expect(keywordPattern).toContain("Property");
    expect(keywordPattern).toContain("Get");
    expect(keywordPattern).toContain("As");
    expect(keywordPattern).toContain("ElseIf");
    expect(keywordPattern).toContain("Is");

    const classicAspGrammar = JSON.parse(
      fs.readFileSync("syntaxes/classic-asp.tmLanguage.json", "utf8"),
    ) as {
      patterns?: Array<{ include?: string }>;
      repository?: Record<
        string,
        { begin?: string; patterns?: Array<{ include?: string; match?: string }> }
      >;
    };
    expect(classicAspGrammar.patterns?.some((pattern) => pattern.include === "#asp-include")).toBe(
      true,
    );
    expect(classicAspGrammar.repository?.["asp-include"]?.begin).toContain("#include");
    expect(
      classicAspGrammar.repository?.["asp-include"]?.patterns?.some((pattern) =>
        pattern.match?.includes("file|virtual"),
      ),
    ).toBe(true);
    expect(
      classicAspGrammar.repository?.["asp-block"]?.patterns?.some(
        (pattern) => pattern.include === "source.vbscript",
      ),
    ).toBe(true);
  });

  it("describes the COM type catalog schema for settings UI", () => {
    const manifest = JSON.parse(fs.readFileSync("package.json", "utf8")) as {
      contributes?: {
        configuration?: {
          properties?: Record<
            string,
            {
              additionalProperties?: {
                properties?: {
                  members?: {
                    additionalProperties?: unknown;
                  };
                };
              };
            }
          >;
        };
      };
    };
    const comTypes = manifest.contributes?.configuration?.properties?.["aspLsp.vbscript.comTypes"];
    expect(comTypes?.additionalProperties?.properties?.members?.additionalProperties).toBeTruthy();
    expect(JSON.stringify(comTypes)).toContain("returnType");
    expect(JSON.stringify(comTypes)).toContain("parameters");
  });

  it("describes VBScript identifier casing settings", () => {
    const manifest = JSON.parse(fs.readFileSync("package.json", "utf8")) as {
      contributes?: {
        configuration?: {
          properties?: Record<string, { enum?: string[]; properties?: Record<string, unknown> }>;
        };
      };
    };
    const properties = manifest.contributes?.configuration?.properties;
    const identifierCase = properties?.["aspLsp.vbscript.identifierCase"];
    const byKind = properties?.["aspLsp.vbscript.identifierCaseByKind"];
    expect(identifierCase?.enum).toEqual(
      expect.arrayContaining([
        "PascalCase",
        "UPPERCASE",
        "camelCase",
        "lowercase",
        "snake_case",
        "UPPER_SNAKE",
        "ignore",
      ]),
    );
    expect(identifierCase?.enum).not.toEqual(expect.arrayContaining(["lower", "upper"]));
    expect(byKind?.properties).toEqual(
      expect.objectContaining({
        variable: expect.anything(),
        class: expect.anything(),
        property: expect.anything(),
      }),
    );
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
        ["package", "--no-dependencies", "--follow-symlinks", "--out", vsixPath],
        { stdio: "pipe" },
      );
      expect(fs.existsSync(vsixPath)).toBe(true);
      const listing = execFileSync("unzip", ["-l", vsixPath], { encoding: "utf8" });
      expect(listing).toContain("extension/dist/extension.js");
      expect(listing).toContain("extension/package.nls.json");
      expect(listing).toContain("extension/package.nls.ja.json");
      expect(listing).toContain("extension/assets/icon.png");
      expect(listing).toContain("extension/server/language-server/dist/server.js");
      expect(listing).not.toContain("extension/server/language-server/node_modules/");
      expect(listing).not.toContain("extension/node_modules/");
    } finally {
      fs.rmSync(tempDir, { recursive: true, force: true });
    }
  });
});
