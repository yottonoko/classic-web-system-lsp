import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { execFileSync } from "node:child_process";
import { describe, expect, it } from "vitest";
import { getServerLaunchPath } from "../src/server-path";

describe("VS Code extension package", () => {
  it("does not keep the old Node language server as a runtime dependency", () => {
    const manifest = JSON.parse(fs.readFileSync("package.json", "utf8")) as {
      dependencies?: Record<string, string>;
      devDependencies?: Record<string, string>;
    };
    expect(manifest.dependencies?.["@asp-lsp/core"]).toBe("workspace:*");
    expect(manifest.dependencies?.["@asp-lsp/language-server"]).toBeUndefined();
    expect(manifest.devDependencies?.["@asp-lsp/language-server"]).toBeUndefined();
  });

  it("declares a no-native VSIX packaging script", () => {
    const rootManifest = JSON.parse(fs.readFileSync("../../package.json", "utf8")) as {
      scripts?: Record<string, string>;
    };
    const manifest = JSON.parse(fs.readFileSync("package.json", "utf8")) as {
      scripts?: Record<string, string>;
      contributes?: {
        configuration?: {
          properties?: Record<string, unknown>;
        };
      };
    };
    const extensionSource = fs.readFileSync("src/extension.ts", "utf8");
    const releaseWorkflow = fs.readFileSync("../../.github/workflows/release-vsix.yml", "utf8");
    const buildServerScript = fs.readFileSync("../../scripts/build-server.mjs", "utf8");
    const copyServerRuntimeScript = fs.readFileSync("scripts/copy-server-runtime.mjs", "utf8");

    expect(rootManifest.scripts?.["package:vsix:no-native"]).toBe(
      "pnpm --filter classic-asp-lsp run package:vsix:no-native",
    );
    expect(manifest.scripts?.["build:no-native"]).toContain(
      "node scripts/copy-server-runtime.mjs --no-native",
    );
    expect(rootManifest.scripts?.build).toBe(
      "pnpm --filter @asp-lsp/core run build && pnpm --filter @asp-lsp/embedded-sidecar run build && pnpm --filter classic-asp-lsp run build",
    );
    expect(manifest.scripts?.build).toContain("node ../../scripts/clean-dist.mjs");
    expect(manifest.scripts?.["build:no-native"]).toContain("node ../../scripts/clean-dist.mjs");
    expect(manifest.scripts?.["package:vsix:no-native"]).toContain("pnpm run build:no-native");
    expect(manifest.scripts?.["package:vsix:no-native"]).not.toContain("@asp-lsp/language-server");
    expect(manifest.scripts?.["package:vsix"]).not.toContain("@asp-lsp/language-server");
    expect(manifest.scripts?.test).not.toContain("@asp-lsp/language-server");
    expect(manifest.scripts?.["package:vsix:no-native"]).not.toContain("build:native");
    expect(manifest.scripts?.["package:vsix"]).not.toContain("build:native");
    expect(manifest.scripts?.test).not.toContain("build:native");
    expect(extensionSource).toContain("package:vsix:no-native");
    expect(extensionSource).toContain("package VSIX (no Rust server)");
    expect(manifest.contributes?.configuration?.properties?.["aspLsp.server.path"]).toEqual(
      expect.objectContaining({
        type: "string",
        default: "",
        tags: ["advanced"],
      }),
    );
    expect(copyServerRuntimeScript).toContain("--no-native");
    expect(releaseWorkflow).not.toContain("target: linux-x64");
    expect(releaseWorkflow).not.toContain("target: darwin-x64");
    expect(releaseWorkflow).not.toContain("target: darwin-arm64");
    expect(releaseWorkflow).toContain("target: win32-x64");
    expect(releaseWorkflow).not.toContain("release-vsix-no-native");
    expect(releaseWorkflow).not.toContain("Package VSIX without Rust server binary");
    expect(releaseWorkflow).not.toContain("# - os: ubuntu-latest");
    expect(releaseWorkflow).not.toContain("#   target: linux-x64");
    expect(releaseWorkflow).toContain("ASP_LSP_PACKAGE_TARGET: ${{ matrix.target }}");
    expect(buildServerScript).toContain("ASP_LSP_PACKAGE_TARGET");
    expect(copyServerRuntimeScript).toContain("ASP_LSP_PACKAGE_TARGET");
  });

  it("contributes commands, task definition and IIS debug settings", () => {
    const manifest = JSON.parse(fs.readFileSync("package.json", "utf8")) as {
      repository?: { url?: string };
      icon?: string;
      galleryBanner?: { color?: string };
      dependencies?: Record<string, string>;
      contributes?: {
        languages?: Array<{ id: string; extensions?: string[] }>;
        grammars?: Array<{
          language?: string;
          scopeName?: string;
          path?: string;
          injectTo?: string[];
          embeddedLanguages?: Record<string, string>;
        }>;
        configurationDefaults?: {
          "editor.tokenColorCustomizations"?: {
            textMateRules?: Array<{ scope?: string; settings?: Record<string, unknown> }>;
          };
        };
        commands?: Array<{ command: string; title?: string }>;
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
    expect(commands.filter((command) => command === "aspLsp.restartServer")).toHaveLength(1);
    expect(
      manifest.contributes?.commands?.find((command) => command.command === "aspLsp.restartServer")
        ?.title,
    ).toBe("%command.restartServer.title%");
    expect(commands).toContain("aspLsp.reindexWorkspace");
    expect(commands).toContain("aspLsp.clearCache");
    expect(commands).toContain("aspLsp.clearDiskCache");
    expect(commands).toContain("aspLsp.clearProcessCache");
    expect(commands).toContain("aspLsp.openOutput");
    expect(commands).toContain("aspLsp.debugIisUrl");
    expect(commands).toContain("aspLsp.debugIisExpressUrl");
    expect(commands).toContain("aspLsp.createLaunchConfig");
    const launchSource = fs.readFileSync("src/extension.ts", "utf8");
    expect(launchSource).not.toContain("ASP_LSP_ANALYSIS_BACKEND");
    expect(launchSource).not.toContain("aspLsp.analysisBackend");
    expect(launchSource).not.toContain("aspLsp.useLegacyServer");
    expect(launchSource).not.toContain("typescript-fallback");
    expect(launchSource).not.toContain("status.backend.typescript");
    expect(launchSource).toContain('get<string>("server.path")');
    expect(launchSource).toContain('candidate.backend === "rust"');
    expect(launchSource).toContain("ASP LSP: Rust");
    expect(launchSource).toContain("Backend: Rust ({engine})");
    expect(launchSource).not.toContain("ASP LSP: Native");
    expect(launchSource).not.toContain("Backend: Native");
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
      manifest.contributes?.configuration?.properties?.["aspLsp.vbscript.syntaxSnippets"],
    ).toEqual(expect.objectContaining({ type: "boolean", default: true }));
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
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.inlayHints.globalVariableMarkers"],
    ).toEqual(
      expect.objectContaining({
        type: "string",
        enum: ["global", "local", "all", "off"],
        default: "off",
      }),
    );
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.codeLens.referenceScope"],
    ).toEqual(
      expect.objectContaining({
        type: "string",
        enum: ["analyzed", "workspace"],
        default: "analyzed",
      }),
    );
    expect(manifest.contributes?.configuration?.properties?.["aspLsp.locale"]).toBeTruthy();
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.windowsPathResolution"],
    ).toEqual(expect.objectContaining({ type: "boolean", default: true }));
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.workspace.backgroundConcurrency"],
    ).toBeUndefined();
    expect(manifest.contributes?.configuration?.properties?.["aspLsp.cache.enabled"]).toEqual(
      expect.objectContaining({ type: "boolean", default: true }),
    );
    expect(manifest.contributes?.configuration?.properties?.["aspLsp.cache.directory"]).toEqual(
      expect.objectContaining({ type: "string", default: "" }),
    );
    expect(manifest.contributes?.configuration?.properties?.["aspLsp.cache.ttlHours"]).toEqual(
      expect.objectContaining({ type: "number", default: 336, minimum: 1 }),
    );
    expect(manifest.contributes?.configuration?.properties?.["aspLsp.cache.maxSizeMb"]).toEqual(
      expect.objectContaining({ type: "number", default: 128, minimum: 1 }),
    );
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.embedded.parallelism"],
    ).toEqual(expect.objectContaining({ type: "number", default: 0, minimum: 0 }));
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.workspace.backgroundAnalysis"],
    ).toBeUndefined();
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.workspace.maxIndexFiles"],
    ).toEqual(expect.objectContaining({ type: "number", default: 5000, minimum: 1 }));
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.workspace.diagnosticConcurrency"],
    ).toBeUndefined();
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.workspace.idleAnalysisConcurrency"],
    ).toBeUndefined();
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.workspace.busyAnalysisConcurrency"],
    ).toBeUndefined();
    expect(manifest.contributes?.configuration?.properties?.["aspLsp.legacyEncoding"]).toEqual(
      expect.objectContaining({
        enum: ["auto", "utf8", "shift_jis", "cp932"],
        default: "auto",
      }),
    );
    for (const setting of [
      "aspLsp.format.indentSize",
      "aspLsp.format.indentStyle",
      "aspLsp.format.ignoreVbscriptTagIndent",
      "aspLsp.format.ignoreCssTagIndent",
      "aspLsp.format.ignoreJavaScriptTagIndent",
      "aspLsp.format.onSave",
    ]) {
      expect(manifest.contributes?.configuration?.properties?.[setting]).toEqual(
        expect.objectContaining({ tags: ["advanced"] }),
      );
    }
    expect(manifest.contributes?.configuration?.properties?.["aspLsp.format.indentSize"]).toEqual(
      expect.objectContaining({ type: ["number", "null"], default: null, minimum: 1 }),
    );
    for (const setting of [
      "aspLsp.format.ignoreVbscriptTagIndent",
      "aspLsp.format.ignoreCssTagIndent",
      "aspLsp.format.ignoreJavaScriptTagIndent",
    ]) {
      expect(manifest.contributes?.configuration?.properties?.[setting]).toEqual(
        expect.objectContaining({ type: "boolean", default: false }),
      );
    }
    expect(manifest.repository?.url).toContain("github.com/yottonoko/asp-lsp");
    expect(manifest.icon).toBe("assets/icon.png");
    expect(fs.existsSync(manifest.icon ?? "")).toBe(true);
    expect(manifest.galleryBanner?.color).toBeTruthy();
    expect(manifest.capabilities?.untrustedWorkspaces?.supported).toBe(true);
    const extensionSource = fs.readFileSync("src/extension.ts", "utf8");
    expect(extensionSource).toContain('registerCommand("aspLsp.restartServer"');
    expect(extensionSource).toContain("errorHandler: createLanguageClientErrorHandler()");
    expect(extensionSource).toContain("CloseAction.Restart");
    expect(extensionSource).toContain("ErrorAction.Continue");
    expect(extensionSource).toContain("restartPromise");
    expect(extensionSource).toContain("isDeactivating");
    expect(extensionSource).toContain("isManualRestarting");
    expect(extensionSource).toContain('registerCommand("aspLsp.showReferences"');
    expect(extensionSource).toContain('"editor.action.showReferences"');
    expect(extensionSource).toContain('registerCommand("aspLsp.toggleLineComment"');
    expect(extensionSource).toContain("provideHover");
    expect(extensionSource).toContain("onDidCloseTextDocument");
    expect(keybindings).toContainEqual(
      expect.objectContaining({
        command: "aspLsp.toggleLineComment",
        key: "ctrl+/",
        mac: "cmd+/",
        when: "editorTextFocus && editorLangId == classic-asp",
      }),
    );
    expect(keybindings).toContainEqual(
      expect.objectContaining({
        command: "aspLsp.joinLines",
        key: "ctrl+shift+j",
        mac: "cmd+shift+j",
        when: "editorTextFocus && editorLangId == classic-asp",
      }),
    );
    expect(keybindings).toContainEqual(
      expect.objectContaining({
        command: "aspLsp.onEnter",
        key: "enter",
        when: "editorTextFocus && editorLangId == classic-asp",
      }),
    );
    expect(keybindings).toContainEqual(
      expect.objectContaining({
        command: "aspLsp.matchingBrace",
        key: "ctrl+shift+\\",
        mac: "cmd+shift+\\",
        when: "editorTextFocus && editorLangId == classic-asp",
      }),
    );
    expect(keybindings).toContainEqual(
      expect.objectContaining({
        command: "aspLsp.moveItemUp",
        key: "alt+up",
        when: "editorTextFocus && editorLangId == classic-asp",
      }),
    );
    expect(keybindings).toContainEqual(
      expect.objectContaining({
        command: "aspLsp.moveItemDown",
        key: "alt+down",
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
    expect(
      manifest.contributes?.grammars?.some(
        (grammar) =>
          grammar.scopeName === "classic-asp.tag-injection" &&
          grammar.path === "./syntaxes/classic-asp-tag-injection.tmLanguage.json" &&
          grammar.injectTo?.includes("text.html.classic-asp"),
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
    const classicAspGrammar = manifest.contributes?.grammars?.find(
      (grammar) => grammar.scopeName === "text.html.classic-asp",
    );
    expect(classicAspGrammar?.embeddedLanguages?.["source.vbscript.embedded.asp"]).toBe("vbscript");
    expect(classicAspGrammar?.embeddedLanguages?.["source.vbscript.embedded.asp.expression"]).toBe(
      "vbscript",
    );
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
    expect(nls["command.restartServer.title"]).toBe("Classic ASP: Restart Language Server");
    expect(nlsJa["command.restartServer.title"]).toBe("Classic ASP: Language Server を再起動");
    expect(nls["command.clearCache.title"]).toBe("Classic ASP: Clear All Analysis Caches");
    expect(nls["command.clearDiskCache.title"]).toBe("Classic ASP: Clear Disk Analysis Cache");
    expect(nls["command.clearProcessCache.title"]).toBe(
      "Classic ASP: Clear Process Analysis Cache",
    );
    for (const key of keys) {
      expect(nls[key], key).toBeTruthy();
      expect(nlsJa[key], key).toBeTruthy();
    }
  });

  it("highlights common VBScript declaration keywords", () => {
    const grammar = JSON.parse(fs.readFileSync("syntaxes/vbscript.tmLanguage.json", "utf8")) as {
      repository?: {
        "vbscript-basic"?: {
          patterns?: Array<{
            captures?: Record<string, { name?: string }>;
            include?: string;
            match?: string;
            name?: string;
          }>;
        };
      };
    };
    const patterns = grammar.repository?.["vbscript-basic"]?.patterns ?? [];
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
    const remCommentPattern = grammar.repository?.["vbscript-basic"]?.patterns?.find(
      (pattern) => pattern.name === "comment.line.rem.vbscript",
    )?.match;
    expect(remCommentPattern).toContain("Rem");
    const functionDeclarationPattern = patterns.find(
      (pattern) => pattern.captures?.["3"]?.name === "entity.name.function.vbscript",
    );
    expect(functionDeclarationPattern?.match).toContain("Function|Sub");
    const propertyDeclarationPattern = patterns.find(
      (pattern) => pattern.captures?.["4"]?.name === "entity.name.function.vbscript",
    );
    expect(propertyDeclarationPattern?.match).toContain("Property");
    const typePattern = patterns.find(
      (pattern) => pattern.captures?.["2"]?.name === "support.type.vbscript",
    );
    expect(typePattern?.match).toContain("String");
    expect(typePattern?.match).toContain("Variant");
    expect(typePattern?.match).toContain("Number");
    const stringIndex = patterns.findIndex(
      (pattern) => pattern.name === "string.quoted.double.vbscript",
    );
    const documentationIndex = patterns.findIndex(
      (pattern) => pattern.include === "#documentation-comment",
    );
    const annotationIndex = patterns.findIndex(
      (pattern) => pattern.include === "#annotation-comment",
    );
    const apostropheIndex = patterns.findIndex(
      (pattern) => pattern.name === "comment.line.apostrophe.vbscript",
    );
    const keywordIndex = patterns.findIndex(
      (pattern) => pattern.name === "keyword.control.vbscript",
    );
    expect(stringIndex).toBeLessThan(patterns.indexOf(functionDeclarationPattern!));
    expect(documentationIndex).toBeLessThan(patterns.indexOf(functionDeclarationPattern!));
    expect(annotationIndex).toBeLessThan(patterns.indexOf(functionDeclarationPattern!));
    expect(apostropheIndex).toBeLessThan(patterns.indexOf(functionDeclarationPattern!));
    expect(stringIndex).toBeLessThan(keywordIndex);
    expect(apostropheIndex).toBeLessThan(keywordIndex);
    expect(patterns.indexOf(functionDeclarationPattern!)).toBeLessThan(
      patterns.findIndex((pattern) => pattern.name === "keyword.control.vbscript"),
    );

    const classicAspGrammar = JSON.parse(
      fs.readFileSync("syntaxes/classic-asp.tmLanguage.json", "utf8"),
    ) as {
      patterns?: Array<{ include?: string }>;
      injections?: Record<string, { patterns?: Array<{ include?: string }> }>;
      repository?: Record<
        string,
        { begin?: string; end?: string; patterns?: Array<{ include?: string; match?: string }> }
      >;
    };
    const classicAspTagInjection = JSON.parse(
      fs.readFileSync("syntaxes/classic-asp-tag-injection.tmLanguage.json", "utf8"),
    ) as {
      injectionSelector?: string;
      patterns?: Array<{ include?: string }>;
      repository?: Record<
        string,
        { begin?: string; end?: string; patterns?: Array<{ include?: string }> }
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
    expect(classicAspGrammar.repository?.["asp-expression"]?.end).toBe("%>");
    expect(classicAspTagInjection.injectionSelector).toBe("L:text.html.classic-asp meta.tag");
    expect(classicAspTagInjection.patterns).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ include: "#asp-expression" }),
        expect.objectContaining({ include: "#asp-block" }),
      ]),
    );
    expect(classicAspTagInjection.repository?.["asp-expression"]?.end).toBe("%>");
    expect(classicAspTagInjection.repository?.["asp-block"]?.end).toBe("%>");
    expect(classicAspGrammar.injections?.["source.css, source.js"]?.patterns).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ include: "#asp-expression" }),
        expect.objectContaining({ include: "#asp-block" }),
      ]),
    );
  });

  it("highlights VBScript documentation comments and type annotations", () => {
    type GrammarPattern = {
      begin?: string;
      beginCaptures?: Record<string, { name?: string }>;
      captures?: Record<string, { name?: string }>;
      end?: string;
      endCaptures?: Record<string, { name?: string }>;
      include?: string;
      match?: string;
      name?: string;
      patterns?: GrammarPattern[];
    };
    const grammar = JSON.parse(fs.readFileSync("syntaxes/vbscript.tmLanguage.json", "utf8")) as {
      repository?: Record<string, { patterns?: GrammarPattern[] } & GrammarPattern>;
    };
    const vbPatterns = grammar.repository?.["vbscript-basic"]?.patterns ?? [];
    const documentationIndex = vbPatterns.findIndex(
      (pattern) => pattern.include === "#documentation-comment",
    );
    const annotationIndex = vbPatterns.findIndex(
      (pattern) => pattern.include === "#annotation-comment",
    );
    const apostropheIndex = vbPatterns.findIndex(
      (pattern) => pattern.name === "comment.line.apostrophe.vbscript",
    );
    expect(documentationIndex).toBeGreaterThan(-1);
    expect(annotationIndex).toBeGreaterThan(-1);
    expect(documentationIndex).toBeLessThan(apostropheIndex);
    expect(annotationIndex).toBeLessThan(apostropheIndex);

    const documentation = grammar.repository?.["documentation-comment"];
    expect(documentation?.begin).toContain("'''");
    expect(documentation?.beginCaptures?.["1"]?.name).toBe("comment.line.documentation.vbscript");
    expect(documentation?.patterns).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ include: "#documentation-tag" }),
        expect.objectContaining({ include: "#documentation-entity" }),
        expect.objectContaining({ name: "string.unquoted.documentation.vbscript" }),
      ]),
    );

    const tag = grammar.repository?.["documentation-tag"];
    expect(new RegExp(tag?.begin ?? "").test("<summary")).toBe(true);
    expect(new RegExp(tag?.begin ?? "").test("</summary")).toBe(true);
    expect(tag?.beginCaptures?.["2"]?.name).toBe("entity.name.tag.documentation.vbscript");
    expect(tag?.endCaptures?.["1"]?.name).toBe("punctuation.definition.tag.documentation.vbscript");
    const attribute = tag?.patterns?.find((pattern) =>
      pattern.captures?.["1"]?.name?.includes("attribute-name"),
    );
    expect(new RegExp(attribute?.match ?? "").test('name="first"')).toBe(true);
    expect(new RegExp(attribute?.match ?? "").test('cref="BuildName"')).toBe(true);
    expect(attribute?.captures?.["1"]?.name).toBe(
      "entity.other.attribute-name.documentation.vbscript",
    );
    expect(attribute?.captures?.["3"]?.name).toBe("string.quoted.documentation.vbscript");
    expect(
      new RegExp(grammar.repository?.["documentation-entity"]?.match ?? "").test("&amp;"),
    ).toBe(true);

    const annotation = grammar.repository?.["annotation-comment"];
    expect(annotation?.beginCaptures?.["1"]?.name).toBe("comment.line.annotation.vbscript");
    const annotationPatterns = annotation?.patterns ?? [];
    const caseInsensitivePattern = (match: string | undefined) =>
      new RegExp((match ?? "").replace("(?i)", ""), "i");
    const typePattern = annotationPatterns.find((pattern) => pattern.match?.includes("@type"));
    const paramPattern = annotationPatterns.find((pattern) => pattern.match?.includes("@param"));
    const returnsWithProcedurePattern = annotationPatterns.find((pattern) =>
      pattern.match?.includes("@returns"),
    );
    const returnsTypePattern = annotationPatterns.find(
      (pattern) =>
        pattern.match?.includes("@returns") && pattern.captures?.["2"]?.name?.includes("type"),
    );
    const memberPattern = annotationPatterns.find((pattern) => pattern.match?.includes("@member"));
    expect(caseInsensitivePattern(typePattern?.match).test("@type customerId As Long")).toBe(true);
    expect(
      caseInsensitivePattern(paramPattern?.match).test("@param BuildName.first As String"),
    ).toBe(true);
    expect(
      caseInsensitivePattern(returnsWithProcedurePattern?.match).test("@returns BuildName String"),
    ).toBe(true);
    expect(caseInsensitivePattern(returnsTypePattern?.match).test("@returns String")).toBe(true);
    expect(
      caseInsensitivePattern(memberPattern?.match).test("@member Customer.Name As String"),
    ).toBe(true);
    for (const pattern of [
      typePattern,
      paramPattern,
      returnsWithProcedurePattern,
      returnsTypePattern,
      memberPattern,
    ]) {
      expect(pattern?.captures?.["1"]?.name).toBe("keyword.other.annotation.vbscript");
      expect(JSON.stringify(pattern?.captures)).not.toContain("comment.line");
    }
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
          properties?: Record<
            string,
            { default?: unknown; enum?: string[]; properties?: Record<string, unknown> }
          >;
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
    expect(identifierCase?.default).toBe("ignore");
    expect(identifierCase?.enum).not.toEqual(expect.arrayContaining(["lower", "upper"]));
    expect(byKind?.properties).toEqual(
      expect.objectContaining({
        variable: expect.anything(),
        class: expect.anything(),
        property: expect.anything(),
      }),
    );
  });

  it("resolves the Rust server binary by default", () => {
    const root = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-server-path-"));
    const executableName = process.platform === "win32" ? "asp-lsp-server.exe" : "asp-lsp-server";
    const binary = path.join(
      root,
      "server",
      "bin",
      `${process.platform}-${process.arch}`,
      executableName,
    );
    fs.mkdirSync(path.dirname(binary), { recursive: true });
    fs.writeFileSync(binary, "");

    const launch = getServerLaunchPath({
      asAbsolutePath: (relativePath) => path.join(root, relativePath),
    });

    expect(launch).toEqual({ kind: "binary", path: binary });
  });

  it("resolves an external Rust server binary for no-native packages", () => {
    const root = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-server-path-"));
    const binary = path.join(
      root,
      process.platform === "win32" ? "asp-lsp-server.exe" : "asp-lsp-server",
    );
    fs.writeFileSync(binary, "");
    try {
      const launch = getServerLaunchPath(
        {
          asAbsolutePath: (relativePath) => path.join(root, relativePath),
        },
        binary,
      );
      expect(launch).toEqual({ kind: "binary", path: binary });
    } finally {
      fs.rmSync(root, { recursive: true, force: true });
    }
  });

  it("fails clearly when no Rust server binary is configured or packaged", () => {
    const root = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-server-path-"));
    try {
      expect(() =>
        getServerLaunchPath({
          asAbsolutePath: (relativePath) => path.join(root, relativePath),
        }),
      ).toThrow("Rust language server binary not found");
      expect(() =>
        getServerLaunchPath(
          {
            asAbsolutePath: (relativePath) => path.join(root, relativePath),
          },
          path.join(root, "missing", "asp-lsp-server"),
        ),
      ).toThrow("Configured aspLsp.server.path does not exist");
    } finally {
      fs.rmSync(root, { recursive: true, force: true });
    }
  });

  it("bundles TypeScript browser libs for the embedded sidecar", () => {
    const sidecarDirectory = path.join(process.cwd(), "server", "sidecar", "dist");
    expect(fs.existsSync(path.join(sidecarDirectory, "sidecar.js"))).toBe(true);
    expect(fs.existsSync(path.join(sidecarDirectory, "lib.esnext.d.ts"))).toBe(true);
    expect(fs.existsSync(path.join(sidecarDirectory, "lib.dom.d.ts"))).toBe(true);
  });

  it("packages a VSIX with the Rust server and embedded sidecar", () => {
    const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-vsix-"));
    const vsixPath = path.join(tempDir, "classic-asp-lsp.vsix");
    try {
      execFileSync(process.execPath, ["scripts/copy-server-runtime.mjs"], { stdio: "pipe" });
      execFileSync(
        path.join("node_modules", ".bin", "vsce"),
        ["package", "--no-dependencies", "--follow-symlinks", "--out", vsixPath],
        { stdio: "pipe" },
      );
      expect(fs.existsSync(vsixPath)).toBe(true);
      const listing = execFileSync("unzip", ["-l", vsixPath], { encoding: "utf8" });
      expect(listing).toContain("extension/dist/extension.js");
      expect(listing).toContain("extension/syntaxes/classic-asp-tag-injection.tmLanguage.json");
      expect(listing).toContain("extension/syntaxes/classic-asp.tmLanguage.json");
      expect(listing).toContain("extension/package.nls.json");
      expect(listing).toContain("extension/package.nls.ja.json");
      expect(listing).toContain("extension/assets/icon.png");
      expect(listing).toContain("extension/server/sidecar/dist/sidecar.js");
      expect(listing).toContain("extension/server/sidecar/dist/lib.esnext.d.ts");
      expect(listing).toContain("extension/server/sidecar/dist/lib.dom.d.ts");
      expect(listing).toContain("extension/server/sidecar/package.json");
      expect(listing).toMatch(/extension\/server\/bin\/[^/]+\/asp-lsp-server/);
      expect(listing).not.toContain("extension/server/language-server/");
      const removedRuntimeName = "was" + "m";
      expect(listing).not.toContain(`.${removedRuntimeName}`);
      expect(listing).not.toMatch(
        new RegExp(`extension/server/language-server/.*${removedRuntimeName}`, "i"),
      );
      expect(listing).not.toContain("extension/node_modules/");
      expect(fs.readFileSync(".vscodeignore", "utf8")).not.toContain("server/language-server");
    } finally {
      fs.rmSync(tempDir, { recursive: true, force: true });
    }
  }, 30_000);

  it("packages a no-native VSIX with the embedded sidecar and no Rust server", () => {
    const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-vsix-no-native-"));
    const vsixPath = path.join(tempDir, "classic-asp-lsp-no-native.vsix");
    try {
      execFileSync(process.execPath, ["scripts/copy-server-runtime.mjs", "--no-native"], {
        stdio: "pipe",
      });
      execFileSync(
        path.join("node_modules", ".bin", "vsce"),
        ["package", "--no-dependencies", "--follow-symlinks", "--out", vsixPath],
        { stdio: "pipe" },
      );
      expect(fs.existsSync(vsixPath)).toBe(true);
      const listing = execFileSync("unzip", ["-l", vsixPath], { encoding: "utf8" });
      expect(listing).toContain("extension/dist/extension.js");
      expect(listing).toContain("extension/server/sidecar/dist/sidecar.js");
      expect(listing).toContain("extension/server/sidecar/dist/lib.esnext.d.ts");
      expect(listing).toContain("extension/server/sidecar/dist/lib.dom.d.ts");
      expect(listing).toContain("extension/server/sidecar/package.json");
      expect(listing).not.toMatch(/extension\/server\/bin\/[^/]+\/asp-lsp-server/);
      expect(listing).not.toContain("extension/server/language-server/");
      expect(listing).not.toContain("extension/node_modules/");
    } finally {
      fs.rmSync(tempDir, { recursive: true, force: true });
    }
  }, 30_000);

  it("packages a win32-x64 VSIX layout with the Windows Rust server name", () => {
    const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-vsix-win32-x64-"));
    const vsixPath = path.join(tempDir, "classic-asp-lsp-win32-x64.vsix");
    try {
      execFileSync(process.execPath, ["scripts/copy-server-runtime.mjs", "--target", "win32-x64"], {
        stdio: "pipe",
      });
      execFileSync(
        path.join("node_modules", ".bin", "vsce"),
        ["package", "--no-dependencies", "--follow-symlinks", "--out", vsixPath],
        { stdio: "pipe" },
      );
      expect(fs.existsSync(vsixPath)).toBe(true);
      const listing = execFileSync("unzip", ["-l", vsixPath], { encoding: "utf8" });
      expect(listing).toContain("extension/server/bin/win32-x64/asp-lsp-server.exe");
      expect(listing).toContain("extension/server/sidecar/dist/sidecar.js");
      expect(listing).toContain("extension/server/sidecar/package.json");
      expect(listing).not.toContain("extension/server/bin/win32-x64/asp-lsp-server\n");
    } finally {
      fs.rmSync(tempDir, { recursive: true, force: true });
    }
  }, 30_000);
});
