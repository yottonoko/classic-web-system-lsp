import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { createRequire } from "node:module";
import { execFileSync } from "node:child_process";
import { describe, expect, it } from "vitest";
import yauzl from "yauzl";
import { INITIAL, Registry, parseRawGrammar } from "vscode-textmate";
import { OnigScanner, OnigString, loadWASM } from "vscode-oniguruma";
import {
  imeSafeCommittedText,
  imeSafeCompositionEndValue,
  imeSafeCompositionStartSnapshot,
  imeSafeKeyboardEventIsComposing,
  imeSafeShouldWriteExternalValue,
} from "../src/webview/ime-safe-input";
import { getServerExecutablePath } from "../src/server-path";
import { uriTextForVSCode } from "../src/uri-encoding";
import {
  isAspFlowchartPayload,
  isAspNavigationGraphPayload,
  type AspNavigationGraphPayload,
} from "../src/protocol-types";
import {
  navigationFlowElementsFromElk,
  navigationGraphToElkGraph,
} from "../src/webview/navigation-graph-layout";
import {
  highlightFlowchartOutputFragment,
  highlightFlowchartSourceWithSetting,
  highlightFlowchartSource,
} from "../src/webview/flowchart-source-highlight";
import {
  flowchartSourceScrollTarget,
  shouldScrollFlowchartSource,
} from "../src/webview/flowchart-source-scroll";
import { flowchartThemePaletteForSetting } from "../src/webview/flowchart-theme";
import type { FlowchartSourceHighlight } from "../src/webview/flowchart-types";

function readWebviewSources(...files: string[]): string {
  return files.map((file) => fs.readFileSync(file, "utf8")).join("\n");
}

function readFlatSources(directory: string, include: (entry: string) => boolean): string {
  return fs
    .readdirSync(directory)
    .filter(include)
    .sort()
    .map((entry) => fs.readFileSync(path.join(directory, entry), "utf8"))
    .join("\n");
}

function readZipEntries(filePath: string): Promise<string[]> {
  return new Promise((resolve, reject) => {
    yauzl.open(filePath, { lazyEntries: true }, (error, zipFile) => {
      if (error || !zipFile) {
        reject(error ?? new Error(`Unable to open ZIP archive: ${filePath}`));
        return;
      }
      const entries: string[] = [];
      zipFile.on("entry", (entry) => {
        entries.push(entry.fileName);
        zipFile.readEntry();
      });
      zipFile.on("error", reject);
      zipFile.on("end", () => resolve(entries));
      zipFile.readEntry();
    });
  });
}

describe("URI percent encoding", () => {
  it("preserves valid URI encoding", () => {
    expect(uriTextForVSCode("file:///workspace/%E6%97%A5%E6%9C%AC%20file.asp")).toBe(
      "file:///workspace/%E6%97%A5%E6%9C%AC%20file.asp",
    );
  });

  it.each([
    ["incomplete escape", "file:///workspace/rate%.asp", "file:///workspace/rate%25.asp"],
    ["non-hex escape", "file:///workspace/%ZZ.asp", "file:///workspace/%25ZZ.asp"],
    [
      "mixed valid and invalid escapes",
      "file:///workspace/valid%20name/rate%.asp",
      "file:///workspace/valid%20name/rate%25.asp",
    ],
    [
      "invalid UTF-8 escape sequence",
      "file:///workspace/%E0%A4%A.asp",
      "file:///workspace/%25E0%25A4%25A.asp",
    ],
  ])("escapes percent signs for %s", (_name, uriText, expected) => {
    expect(() => uriTextForVSCode(uriText)).not.toThrow();
    expect(uriTextForVSCode(uriText)).toBe(expected);
  });

  it.each([
    ["UNC URI", "file:////server/share/default.asp", "file://server/share/default.asp"],
    [
      "UNC URI with extra slashes",
      "file://///server/share/default.asp",
      "file://server/share/default.asp",
    ],
    ["Windows drive URI", "file:////C:/site/default.asp", "file:///C:/site/default.asp"],
    ["empty file URI path", "file:////", "file:///"],
  ])("normalizes %s without a double-slash path", (_name, uriText, expected) => {
    expect(uriTextForVSCode(uriText)).toBe(expected);
  });
});

function readTypeScriptSources(directory: string): string {
  const entries = fs.readdirSync(directory, { withFileTypes: true });
  const sources: string[] = [];
  for (const entry of entries) {
    const entryPath = path.join(directory, entry.name);
    if (entry.isDirectory()) {
      sources.push(readTypeScriptSources(entryPath));
    } else if (entry.isFile() && entry.name.endsWith(".ts")) {
      sources.push(fs.readFileSync(entryPath, "utf8"));
    } else if (entry.isFile() && entry.name.endsWith(".tsx")) {
      sources.push(fs.readFileSync(entryPath, "utf8"));
    }
  }
  return sources.join("\n");
}

function readFlowchartWebviewSource(): string {
  return readWebviewSources(
    "src/webview/flowchart.tsx",
    "src/webview/flowchart-canvas.tsx",
    "src/webview/flowchart-dom.ts",
    "src/webview/flowchart-i18n.ts",
    "src/webview/flowchart-model.ts",
    "src/webview/flowchart-primitives.tsx",
    "src/webview/flowchart-runtime.ts",
    "src/webview/flowchart-sidebar.tsx",
    "src/webview/flowchart-source-highlight.ts",
    "src/webview/flowchart-source-scroll.ts",
    "src/webview/flowchart-source-panel.tsx",
    "src/webview/flowchart-theme.ts",
    "src/webview/flowchart-toolbar.tsx",
    "src/webview/flowchart-types.ts",
  );
}

function readExtensionSource(): string {
  return [
    fs.readFileSync("src/extension.ts", "utf8"),
    readFlatSources(
      "src",
      (entry) => entry !== "extension.ts" && (entry.endsWith(".ts") || entry.endsWith(".tsx")),
    ),
  ].join("\n");
}

function readLanguageServerSource(): string {
  const directory = "../../internal/lspserver";
  return readFlatSources(
    directory,
    (entry) => entry.endsWith(".go") && !entry.endsWith("_test.go"),
  );
}

function readNavigationGraphWebviewSource(): string {
  return readWebviewSources(
    "src/navigation-graph-webview.ts",
    "src/webview/navigation-graph.tsx",
    "src/webview/navigation-graph-layout.ts",
    "src/webview/navigation-graph-canvas.tsx",
    "src/webview/navigation-graph.css",
  );
}

function readWorkspaceFilesWebviewSource(): string {
  return readWebviewSources(
    "src/workspace-files-webview.ts",
    "src/webview/workspace-files.tsx",
    "src/webview/workspace-files-components.tsx",
    "src/webview/workspace-files.css",
    "src/webview/workspace-files-model.ts",
    "src/webview/workspace-files-types.ts",
  );
}

function sampleNavigationPayload(): AspNavigationGraphPayload {
  const range = {
    start: { line: 1, character: 2 },
    end: { line: 1, character: 24 },
  };
  return {
    scope: "workspace",
    rootUri: "file:///workspace/index.asp",
    nodes: [
      {
        id: "file:///workspace/index.asp",
        kind: "page",
        label: "index.asp",
        uri: "file:///workspace/index.asp",
        isRoot: true,
      },
      {
        id: "file:///workspace/search.asp",
        kind: "page",
        label: "search.asp",
        uri: "file:///workspace/search.asp",
      },
      {
        id: "https://example.com/help",
        kind: "external",
        label: "https://example.com/help",
        externalUrl: "https://example.com/help",
      },
      {
        id: "unknown:navigation-target",
        kind: "unknown",
        label: "unknown target",
      },
    ],
    edges: [
      {
        id: "edge-search",
        source: "file:///workspace/index.asp",
        target: "file:///workspace/search.asp",
        kind: "htmlForm",
        confidence: "certain",
        method: "GET",
        targetFrame: "_self",
        declaredInUri: "file:///workspace/includes/nav.inc",
        ranges: [range],
        parameters: [{ name: "q", source: "formControl", value: "term", confidence: "certain" }],
        evidence: [
          {
            uri: "file:///workspace/includes/nav.inc",
            range,
            label: "form action",
            snippet: '<form action="search.asp" method="get">',
            extractor: "html",
          },
        ],
      },
      {
        id: "edge-external",
        source: "file:///workspace/search.asp",
        target: "https://example.com/help",
        kind: "javascriptLocation",
        confidence: "possible",
        ranges: [range],
        evidence: [
          {
            uri: "file:///workspace/search.asp",
            range,
            label: "location assign",
            snippet: 'location.href = "https://example.com/help"',
            extractor: "javascript",
          },
        ],
      },
      {
        id: "edge-unknown",
        source: "file:///workspace/search.asp",
        target: "unknown:navigation-target",
        kind: "serverRedirect",
        confidence: "unknown",
        ranges: [range],
        parameters: [{ name: "next", source: "queryString", confidence: "unknown" }],
        evidence: [
          {
            uri: "file:///workspace/search.asp",
            range,
            label: "Response.Redirect",
            snippet: 'Response.Redirect Request("next")',
            extractor: "vbscript",
          },
        ],
      },
    ],
    stats: {
      documents: 2,
      nodes: 4,
      edges: 3,
      certain: 1,
      probable: 0,
      possible: 1,
      unknown: 1,
      external: 1,
    },
  };
}

describe("VS Code extension package", () => {
  it("prioritizes a selected flowchart node and scrolls each target only once", () => {
    const range = { start: { line: 4, character: 0 }, end: { line: 4, character: 8 } };
    const highlights: FlowchartSourceHighlight[] = [
      { kind: "hover", ranges: [range] },
      { kind: "selection", ranges: [range] },
      { kind: "section", ranges: [range] },
    ];
    const target = flowchartSourceScrollTarget(highlights, {
      activeNodeId: "selected",
      hoveredNodeId: "hovered",
      sectionId: "section",
      sectionSequence: 1,
      uri: "file:///flow.asp",
    });

    expect(target?.kind).toBe("selection");
    expect(shouldScrollFlowchartSource(new Set(), target)).toBe(true);
    expect(shouldScrollFlowchartSource(new Set([target?.key ?? ""]), target)).toBe(false);
    expect(
      shouldScrollFlowchartSource(
        new Set([target?.key ?? ""]),
        flowchartSourceScrollTarget(highlights, {
          activeNodeId: "next",
          hoveredNodeId: "hovered",
          sectionId: "section",
          sectionSequence: 1,
          uri: "file:///flow.asp",
        }),
      ),
    ).toBe(true);
  });

  it("resolves auto flowchart colors from VS Code while preserving fixed themes", () => {
    const colors = new Map([
      ["editor-background", "#112233"],
      ["editor-foreground", "#ddeeff"],
      ["editorWidget-background", "#223344"],
      ["focusBorder", "#55aaff"],
      ["charts-blue", "#1234aa"],
      ["symbolIcon-keywordForeground", "#cc44aa"],
    ]);
    const color = (name: string) => colors.get(name);
    const autoPalette = flowchartThemePaletteForSetting("dark", "auto", color);
    const fixedPalette = flowchartThemePaletteForSetting("dark", "dark", color);
    const highlighted = highlightFlowchartSourceWithSetting(
      "<% If ready Then %>",
      "dark",
      "auto",
      color,
    );

    expect(autoPalette.mermaidThemeVariables?.background).toBe("#112233");
    expect(autoPalette.nodeKindStyles.start.background).toBe("#223344");
    expect(autoPalette.nodeKindStyles.start.border).toBe("#1234aa");
    expect(fixedPalette.nodeKindStyles.start.background).not.toBe("#223344");
    expect(highlighted.style.background).toBe("#112233");
    expect(highlighted.tokens).toContainEqual(["If", "#cc44aa"]);
  });

  it("normalizes functional VS Code colors for Mermaid class definitions", () => {
    const colors = new Map([
      ["editor-background", "rgb(13, 17, 23)"],
      ["editor-foreground", "rgb(217, 224, 234)"],
      ["editorWidget-background", "rgba(32, 43, 56, 0.95)"],
      ["focusBorder", "rgb(85, 170, 255)"],
      ["charts-blue", "rgb(18, 52, 170)"],
    ]);
    const palette = flowchartThemePaletteForSetting("dark", "auto", (name) => colors.get(name));

    expect(palette.mermaidThemeVariables?.background).toBe("#0d1117");
    expect(palette.nodeKindStyles.start.background).toBe("#202b38f2");
    expect(palette.nodeKindStyles.start.border).toBe("#1234aa");
    expect(JSON.stringify(palette)).not.toMatch(/rgba?\(/);
  });

  it("highlights flowchart source without loading external grammar or theme resources", () => {
    const highlighted = highlightFlowchartSource(
      '<%\nDim greeting\ngreeting = "Hello"\n\' comment\n%>',
      "dark",
    );
    const colorsByText = new Map(
      highlighted.tokens
        .filter((token): token is [string, string] => Array.isArray(token) && Boolean(token[1]))
        .map(([text, color]) => [text, color]),
    );

    expect(highlighted.lang).toBe("asp");
    expect(colorsByText.get("Dim")).toBe("#ff7b72");
    expect(colorsByText.get('"Hello"')).toBe("#a5d6ff");
    expect(colorsByText.get("' comment")).toBe("#8b949e");
    expect(new Set(colorsByText.values()).size).toBeGreaterThan(3);
  });

  it("uses distinct HTML, CSS, JavaScript, and ASP source colors", () => {
    const highlighted = highlightFlowchartSource(
      [
        '<main class="card">',
        "<style>.card { color: #fff; }</style>",
        "<script>const render = (value) => document.write(value);</script>",
        '<% Response.Write "done" %>',
        "</main>",
      ].join("\n"),
      "dark",
    );
    const coloredTokens = highlighted.tokens.filter(
      (token): token is [string, string] => Array.isArray(token) && Boolean(token[1]),
    );
    const colorsFor = (text: string) =>
      coloredTokens.filter(([token]) => token === text).map(([, color]) => color);

    expect(colorsFor("main")).toContain("#7ee787");
    expect(colorsFor("class")).toContain("#d2a8ff");
    expect(colorsFor("color")).toContain("#79c0ff");
    expect(colorsFor("const")).toContain("#ff7b72");
    expect(colorsFor("render")).toContain("#d2a8ff");
    expect(colorsFor("Response")).toContain("#ffa657");
    expect(new Set(coloredTokens.map(([, color]) => color)).size).toBeGreaterThanOrEqual(8);
  });

  it("highlights typed response output fragments", () => {
    const html = highlightFlowchartOutputFragment(
      '<button aria-label="Save">Save</button>',
      "html",
      "dark",
    );
    const css = highlightFlowchartOutputFragment("button { color: red; }", "css", "dark");
    const javascript = highlightFlowchartOutputFragment(
      "const save = () => submit();",
      "javascript",
      "dark",
    );

    expect(html.lang).toBe("html");
    expect(css.lang).toBe("css");
    expect(javascript.lang).toBe("javascript");
    expect(html.tokens).toContainEqual(["button", "#7ee787"]);
    expect(css.tokens).toContainEqual(["color", "#79c0ff"]);
    expect(javascript.tokens).toContainEqual(["const", "#ff7b72"]);
  });

  it("preserves multiline client-language state around ASP islands", () => {
    const source = [
      "<!-- comment",
      "continued -->",
      "<style>",
      "/* css",
      "continued */ .card { color: <% Response.Write themeColor %>; }",
      "</style>",
      "<script>",
      "const template = `first",
      "second`; const done = true;",
      "</script>",
      '<div class="card"',
      ' data-name="value">text</div>',
    ].join("\r\n");
    const highlighted = highlightFlowchartSource(source, "dark");

    expect(highlighted.value).toBe(source);
    expect(highlighted.tokens).toContainEqual(["continued -->", "#8b949e"]);
    expect(highlighted.tokens).toContainEqual(["continued */", "#8b949e"]);
    expect(highlighted.tokens).toContainEqual(["const", "#ff7b72"]);
    expect(highlighted.tokens).toContainEqual(["data-name", "#d2a8ff"]);
    expect(() =>
      highlightFlowchartSource("<script>const value = `unterminated", "dark"),
    ).not.toThrow();
  });

  it("keeps the Go language server as the runtime dependency", () => {
    const manifest = JSON.parse(fs.readFileSync("package.json", "utf8")) as {
      dependencies?: Record<string, string>;
    };
    expect(manifest.dependencies?.["@tanstack/virtual-core"]).toBe("3.17.7");
    expect(manifest.dependencies?.["solid-js"]).toBe("2.0.0-rc.6");
    expect(manifest.dependencies?.["@solidjs/web"]).toBe("2.0.0-rc.6");
    expect(manifest.dependencies?.["react"]).toBeUndefined();
    expect(manifest.dependencies?.["react-dom"]).toBeUndefined();
    expect(manifest.dependencies?.["clsx"]).toBeDefined();
    expect(manifest.dependencies?.["tailwind-merge"]).toBeDefined();
  });

  it("keeps release manifests and Go server version in sync", () => {
    const rootManifest = JSON.parse(fs.readFileSync("../../package.json", "utf8")) as {
      version?: string;
    };
    const extensionManifest = JSON.parse(fs.readFileSync("package.json", "utf8")) as {
      version?: string;
    };
    const serverSource = readLanguageServerSource();

    expect(extensionManifest.version).toBe(rootManifest.version);
    expect(serverSource).toContain(`"version": "${rootManifest.version}-go"`);
  });

  it("does not configure GitHub Actions workflows", () => {
    const workflowDirectory = "../../.github/workflows";
    const workflowFiles = fs.existsSync(workflowDirectory)
      ? fs.readdirSync(workflowDirectory).filter((entry) => /\.ya?ml$/u.test(entry))
      : [];

    expect(workflowFiles).toEqual([]);
  });

  it("declares Go development server scripts while leaving VSIX packaging on TypeScript", () => {
    const rootManifest = JSON.parse(fs.readFileSync("../../package.json", "utf8")) as {
      scripts?: Record<string, string>;
    };
    const manifest = JSON.parse(fs.readFileSync("package.json", "utf8")) as {
      scripts?: Record<string, string>;
    };
    const extensionSource = readExtensionSource();

    expect(rootManifest.scripts?.["package:vsix"]).toBe(
      "pnpm --filter classic-asp-lsp run package:vsix",
    );
    expect(rootManifest.scripts?.["build:go"]).toBe("go build -o bin/asp-lsp-go ./cmd/asp-lsp-go");
    expect(rootManifest.scripts?.["test:go"]).toBe("go test ./...");
    const copyServerRuntime = fs.readFileSync("scripts/copy-server-runtime.mjs", "utf8");
    expect(copyServerRuntime).toContain('"-trimpath"');
    expect(copyServerRuntime).toContain('"-buildvcs=false"');
    expect(copyServerRuntime).toContain('"-ldflags=-s -w -buildid="');
    expect(copyServerRuntime).toContain('execFileSync("go", goBuildArgs');
    expect(copyServerRuntime).toContain('CGO_ENABLED: "0"');
    expect(copyServerRuntime).toContain("ASP_LSP_SERVER_GOOS");
    expect(copyServerRuntime).toContain("asp-lsp-go.exe");
    expect(copyServerRuntime).not.toContain("if (!fs.existsSync(sourceBinary))");
    const justfile = fs.readFileSync("../../justfile", "utf8");
    expect(justfile).toContain('ldflags := "-s -w -buildid="');
    expect(justfile).toContain("go build -trimpath -buildvcs=false");
    const removedSuffix = "no-" + "nati" + "ve";
    const removedBuild = "build:" + "nati" + "ve";
    const removedAnalysisSetting = "analysis" + "Backend";
    const removedAnalysisEnv = "ASP_LSP_ANALYSIS_" + "BACKEND";
    expect(rootManifest.scripts?.[`package:vsix:${removedSuffix}`]).toBeUndefined();
    expect(rootManifest.scripts?.[removedBuild]).toBeUndefined();
    expect(manifest.scripts?.[`build:${removedSuffix}`]).toBeUndefined();
    expect(manifest.scripts?.[`package:vsix:${removedSuffix}`]).toBeUndefined();
    expect(manifest.scripts?.["build"]).toContain("scripts/build-webview.mjs");
    expect(manifest.scripts?.["typecheck"]).toContain("tsconfig.webview.json");
    expect(manifest.scripts?.["package:vsix"]).toContain("scripts/clean-vsix.mjs");
    expect(manifest.scripts?.["package:vsix"]).not.toContain(removedBuild);
    const cleanVsixScript = fs.readFileSync("scripts/clean-vsix.mjs", "utf8");
    expect(cleanVsixScript).toContain("/^classic-asp-lsp-.*\\.vsix$/");
    expect(cleanVsixScript).toContain("fs.rmSync");
    expect(extensionSource).not.toContain(`package:vsix:${removedSuffix}`);
    expect(extensionSource).not.toContain(removedAnalysisEnv);
    expect(extensionSource).not.toContain(`aspLsp.${removedAnalysisSetting}`);
    expect(extensionSource).toContain("getServerExecutablePath(context)");
  });

  it("keeps the legacy TypeScript LSP runtime removed", () => {
    const repoRoot = path.resolve("..", "..");
    const rootManifest = JSON.parse(fs.readFileSync("../../package.json", "utf8")) as {
      dependencies?: Record<string, string>;
      devDependencies?: Record<string, string>;
      workspaces?: unknown;
    };
    const manifest = JSON.parse(fs.readFileSync("package.json", "utf8")) as {
      dependencies?: Record<string, string>;
      devDependencies?: Record<string, string>;
    };
    const extensionSources = readTypeScriptSources("src");
    const rootDependencies = {
      ...rootManifest.dependencies,
      ...rootManifest.devDependencies,
      ...manifest.dependencies,
      ...manifest.devDependencies,
    };

    expect(fs.existsSync(path.join(repoRoot, "packages", "core"))).toBe(false);
    expect(fs.existsSync(path.join(repoRoot, "packages", "language-server"))).toBe(false);
    expect(rootManifest.workspaces).not.toEqual(expect.arrayContaining(["packages/*"]));
    expect(rootDependencies["@asp-lsp/core"]).toBeUndefined();
    expect(rootDependencies["@asp-lsp/language-server"]).toBeUndefined();
    expect(extensionSources).not.toContain("@asp-lsp/core");
    expect(extensionSources).not.toContain("@asp-lsp/language-server");
    expect(extensionSources).not.toContain("getServerModulePath");
    expect(extensionSources).not.toContain("serverModule");
    expect(extensionSources).toContain("getServerExecutablePath");
  });

  it("allows remaining webviews to load wasm, fonts, workers and blob resources", () => {
    const cspSource = fs.readFileSync("src/webview-csp.ts", "utf8");
    const flowchartHostSource = fs.readFileSync("src/flowchart-webview.ts", "utf8");
    const navigationHostSource = fs.readFileSync("src/navigation-graph-webview.ts", "utf8");
    const workspaceFilesHostSource = fs.readFileSync("src/workspace-files-webview.ts", "utf8");

    expect(cspSource).toContain("graphWebviewContentSecurityPolicy");
    expect(cspSource).toContain("img-src ${webview.cspSource} data: blob:");
    expect(cspSource).toContain("font-src ${webview.cspSource} data:");
    expect(cspSource).toContain("connect-src ${webview.cspSource} data: blob:");
    expect(cspSource).toContain("script-src 'nonce-${nonce}' 'wasm-unsafe-eval'");
    expect(cspSource).toContain("worker-src ${webview.cspSource} blob:");
    for (const source of [flowchartHostSource, navigationHostSource, workspaceFilesHostSource]) {
      expect(source).toContain('from "./webview-csp"');
      expect(source).toContain("${graphWebviewContentSecurityPolicy(webview, nonce)}");
    }
  });

  it("normalizes flowchart and navigation payload arrays before rendering webviews", () => {
    const flowchartHostSource = fs.readFileSync("src/flowchart-webview.ts", "utf8");
    const navigationHostSource = fs.readFileSync("src/navigation-graph-webview.ts", "utf8");

    expect(flowchartHostSource).toContain("const sections = Array.isArray(payload.sections)");
    expect(flowchartHostSource).toContain("nodeIds: Array.isArray(section.nodeIds)");
    expect(flowchartHostSource).toContain(
      "const nodes = Array.isArray(payload.nodes) ? payload.nodes : []",
    );
    expect(navigationHostSource).toContain(
      "const nodes = Array.isArray(payload.nodes) ? payload.nodes : []",
    );
    expect(navigationHostSource).toContain(
      "const edges = Array.isArray(payload.edges) ? payload.edges : []",
    );
    expect(navigationHostSource).toContain("ranges: Array.isArray(edge.ranges) ? edge.ranges : []");
    expect(navigationHostSource).toContain(
      "evidence: Array.isArray(edge.evidence) ? edge.evidence : []",
    );
  });

  it("rejects incomplete flowchart responses before rendering or exporting", () => {
    const complete = {
      uri: "file:///workspace/main.asp",
      sections: [],
      nodes: [],
      edges: [],
      includes: [],
      mermaid: "flowchart TB",
      stats: { sections: 0, nodes: 0, edges: 0, includes: 0 },
    };
    expect(isAspFlowchartPayload(complete)).toBe(true);
    expect(isAspFlowchartPayload({ uri: complete.uri, incomplete: true })).toBe(false);
    expect(isAspFlowchartPayload({ ...complete, incomplete: true })).toBe(false);
    expect(isAspFlowchartPayload({ ...complete, mermaid: undefined })).toBe(false);

    const extensionSource = fs.readFileSync("src/extension.ts", "utf8");
    expect(extensionSource).toContain("isAspFlowchartPayload(response)");
    expect(extensionSource).toContain('"flowchart.incomplete"');
  });

  it("accepts valid empty and populated navigation graph responses", () => {
    const empty = {
      scope: "folder",
      nodes: [],
      edges: [],
      stats: {
        documents: 0,
        nodes: 0,
        edges: 0,
        certain: 0,
        probable: 0,
        possible: 0,
        unknown: 0,
        external: 0,
      },
    };
    expect(isAspNavigationGraphPayload(empty)).toBe(true);
    expect(isAspNavigationGraphPayload(sampleNavigationPayload())).toBe(true);

    const boundary = sampleNavigationPayload();
    const boundaryRange = {
      start: { line: 0, character: 0 },
      end: { line: 0, character: 0 },
    };
    boundary.edges[0] = {
      ...boundary.edges[0],
      count: Number.MAX_SAFE_INTEGER,
      ranges: [boundaryRange],
    };
    boundary.stats.documents = Number.MAX_SAFE_INTEGER;
    expect(isAspNavigationGraphPayload(boundary)).toBe(true);
  });

  it("rejects malformed navigation graph responses before rendering", () => {
    const populated = sampleNavigationPayload();
    const node = populated.nodes[0];
    const edge = populated.edges[0];
    const range = edge.ranges[0];
    const evidence = edge.evidence[0];
    const parameter = edge.parameters?.[0] ?? { name: "q", source: "formControl" };
    const invalidPayloads: unknown[] = [
      null,
      "payload",
      1,
      {},
      { ...populated, scope: "invalid" },
      { ...populated, rootUri: null },
      { ...populated, pending: "true" },
      { ...populated, stats: null },
      { ...populated, stats: { ...populated.stats, edges: null } },
      { ...populated, nodes: null },
      { ...populated, nodes: [null] },
      { ...populated, nodes: [1] },
      { ...populated, nodes: [{ ...node, id: undefined }] },
      { ...populated, nodes: [{ ...node, kind: "invalid" }] },
      { ...populated, nodes: [{ ...node, label: null }] },
      { ...populated, nodes: [{ ...node, uri: null }] },
      { ...populated, nodes: [{ ...node, exists: "true" }] },
      { ...populated, nodes: [{ ...node, externalUrl: 1 }] },
      { ...populated, nodes: [{ ...node, isRoot: "true" }] },
      { ...populated, edges: null },
      { ...populated, edges: [null] },
      { ...populated, edges: ["edge"] },
      { ...populated, edges: [{ ...edge, id: undefined }] },
      { ...populated, edges: [{ ...edge, source: null }] },
      { ...populated, edges: [{ ...edge, target: 1 }] },
      { ...populated, edges: [{ ...edge, kind: "invalid" }] },
      { ...populated, edges: [{ ...edge, label: null }] },
      { ...populated, edges: [{ ...edge, confidence: null }] },
      { ...populated, edges: [{ ...edge, method: 1 }] },
      { ...populated, edges: [{ ...edge, targetFrame: false }] },
      { ...populated, edges: [{ ...edge, ranges: null }] },
      { ...populated, edges: [{ ...edge, ranges: [null] }] },
      { ...populated, edges: [{ ...edge, ranges: [{ ...range, start: null }] }] },
      {
        ...populated,
        edges: [{ ...edge, ranges: [{ ...range, end: { line: "1", character: 0 } }] }],
      },
      {
        ...populated,
        edges: [
          {
            ...edge,
            ranges: [{ start: { line: 2, character: 4 }, end: { line: 2, character: 3 } }],
          },
        ],
      },
      {
        ...populated,
        edges: [
          {
            ...edge,
            ranges: [{ start: { line: 3, character: 0 }, end: { line: 2, character: 5 } }],
          },
        ],
      },
      { ...populated, edges: [{ ...edge, parameters: null }] },
      { ...populated, edges: [{ ...edge, parameters: [null] }] },
      { ...populated, edges: [{ ...edge, parameters: [{ ...parameter, name: null }] }] },
      { ...populated, edges: [{ ...edge, parameters: [{ ...parameter, source: "invalid" }] }] },
      {
        ...populated,
        edges: [{ ...edge, parameters: [{ ...parameter, confidence: "invalid" }] }],
      },
      {
        ...populated,
        edges: [{ ...edge, parameters: [{ ...parameter, range: { ...range, end: null } }] }],
      },
      {
        ...populated,
        edges: [
          {
            ...edge,
            parameters: [
              {
                ...parameter,
                range: { start: { line: 2, character: 4 }, end: { line: 2, character: 3 } },
              },
            ],
          },
        ],
      },
      { ...populated, edges: [{ ...edge, declaredInUri: null }] },
      { ...populated, edges: [{ ...edge, evidence: null }] },
      { ...populated, edges: [{ ...edge, evidence: [null] }] },
      { ...populated, edges: [{ ...edge, evidence: [{ ...evidence, uri: null }] }] },
      { ...populated, edges: [{ ...edge, evidence: [{ ...evidence, range: null }] }] },
      { ...populated, edges: [{ ...edge, evidence: [{ ...evidence, valueRange: null }] }] },
      { ...populated, edges: [{ ...edge, evidence: [{ ...evidence, label: 1 }] }] },
      { ...populated, edges: [{ ...edge, evidence: [{ ...evidence, snippet: false }] }] },
      { ...populated, edges: [{ ...edge, evidence: [{ ...evidence, extractor: "invalid" }] }] },
      {
        ...populated,
        edges: [
          {
            ...edge,
            evidence: [
              {
                ...evidence,
                valueRange: {
                  start: { line: 2, character: 4 },
                  end: { line: 2, character: 3 },
                },
              },
            ],
          },
        ],
      },
      { ...populated, edges: [{ ...edge, count: "1" }] },
      { ...populated, edges: [{ ...edge, count: -1 }] },
      { ...populated, edges: [{ ...edge, count: 1.5 }] },
      { ...populated, edges: [{ ...edge, count: Number.NaN }] },
      { ...populated, edges: [{ ...edge, count: Number.POSITIVE_INFINITY }] },
      { ...populated, stats: { ...populated.stats, documents: -1 } },
      { ...populated, stats: { ...populated.stats, nodes: 1.5 } },
      { ...populated, stats: { ...populated.stats, edges: Number.NaN } },
      { ...populated, stats: { ...populated.stats, certain: Number.POSITIVE_INFINITY } },
    ];
    for (const payload of invalidPayloads) {
      expect(isAspNavigationGraphPayload(payload)).toBe(false);
    }

    const extensionSource = fs.readFileSync("src/extension.ts", "utf8");
    expect(extensionSource).toContain("isAspNavigationGraphPayload(response)");
    expect(extensionSource).toContain('"navigationGraph.incomplete"');
    expect(extensionSource).toContain("isAspNavigationGraphPayload(payload)");
  });

  it("avoids controlled text writes during IME composition in webviews", () => {
    const imeInputSource = fs.readFileSync("src/webview/ime-safe-input.tsx", "utf8");

    expect(imeInputSource).toContain("isComposingRef.current = true");
    expect(imeInputSource).toContain("isComposingRef.current = false");
    expect(imeInputSource).toContain("compositionSnapshotRef.current");
    expect(imeInputSource).toContain("latestCompositionTextRef.current");
    expect(imeInputSource).toContain("previousSelectionSnapshotRef.current");
    expect(imeInputSource).toContain('inputType === "insertCompositionText"');
    expect(imeInputSource).toContain("onCompositionUpdate");
    expect(imeInputSource).toContain("imeSafeKeyboardEventIsComposing");
    expect(imeInputSource).toContain("element.value = props.control.value");
    expect(imeInputSource).toContain("element.value = currentValue");
    expect(imeInputSource).not.toContain("value={value}");
    expect(readWorkspaceFilesWebviewSource()).toContain("ImeSafeInput");
    expect(readFlowchartWebviewSource()).toContain("ImeSafeInput");
    expect(readFlowchartWebviewSource()).toContain("imeSafeKeyboardEventIsComposing(event)");
    expect(readFlowchartWebviewSource()).toContain('role="searchbox"');
    expect(readFlowchartWebviewSource()).not.toContain('type="search"');
  });

  it("replaces the original selection when IME commits text", () => {
    expect(
      imeSafeCompositionEndValue(
        { selectionEnd: 3, selectionStart: 0, value: "ACC" },
        "AC",
        "ACCC",
      ),
    ).toBe("AC");
    expect(
      imeSafeCompositionEndValue(
        { selectionEnd: 4, selectionStart: 1, value: "xACCz" },
        "AC",
        "xACCCz",
      ),
    ).toBe("xACz");
    expect(imeSafeCompositionEndValue(undefined, "AC", "ACCC")).toBe("ACCC");
  });

  it("uses the latest IME update when compositionend has no committed text", () => {
    expect(imeSafeCommittedText("", "AC")).toBe("AC");
    expect(
      imeSafeCompositionEndValue(
        { selectionEnd: 3, selectionStart: 0, value: "ACC" },
        imeSafeCommittedText("", "AC"),
        "ACCC",
      ),
    ).toBe("AC");
  });

  it("keeps complete half-width IME text when compositionend reports a partial commit", () => {
    expect(imeSafeCommittedText("C", "AC")).toBe("AC");
    expect(imeSafeCommittedText("A", "AC")).toBe("AC");
    expect(imeSafeCommittedText("京", "東京")).toBe("京");
    expect(
      imeSafeCompositionEndValue(
        { selectionEnd: 3, selectionStart: 0, value: "ACC" },
        imeSafeCommittedText("C", "AC"),
        "ACCC",
      ),
    ).toBe("AC");
  });

  it("keeps the current DOM value when IME replacement no longer has selected-text leftovers", () => {
    expect(
      imeSafeCompositionEndValue(
        { selectionEnd: 6, selectionStart: 3, value: "abcXYZdef" },
        "q",
        "abcqdef",
      ),
    ).toBe("abcqdef");
    expect(
      imeSafeCompositionEndValue(
        { selectionEnd: 6, selectionStart: 3, value: "abcXYZdef" },
        "q",
        "abcq-live",
      ),
    ).toBe("abcq-live");
  });

  it("only removes selected-text leftovers from IME replacement fallbacks", () => {
    expect(
      imeSafeCompositionEndValue(
        { selectionEnd: 6, selectionStart: 3, value: "abcXYZdef" },
        "q",
        "abcqXYZdef",
      ),
    ).toBe("abcqdef");
    expect(
      imeSafeCompositionEndValue(
        { selectionEnd: 6, selectionStart: 3, value: "abcXYZdef" },
        "q",
        "abcXYZqdef",
      ),
    ).toBe("abcqdef");
  });

  it("still removes committed-text leftovers when IME duplicates at a collapsed cursor", () => {
    expect(
      imeSafeCompositionEndValue(
        { selectionEnd: 3, selectionStart: 3, value: "abc" },
        "XY",
        "abcXYY",
      ),
    ).toBe("abcXY");
    expect(
      imeSafeCompositionEndValue(
        { selectionEnd: 3, selectionStart: 3, value: "abc" },
        "XY",
        "abcXY-live",
      ),
    ).toBe("abcXY-live");
  });

  it("detects IME composing keyboard events from native DOM events", () => {
    expect(
      imeSafeKeyboardEventIsComposing({ isComposing: true, keyCode: 0 } as KeyboardEvent),
    ).toBe(true);
    expect(
      imeSafeKeyboardEventIsComposing({ isComposing: false, keyCode: 229 } as KeyboardEvent),
    ).toBe(true);

    expect(
      imeSafeKeyboardEventIsComposing({ isComposing: false, keyCode: 13 } as KeyboardEvent),
    ).toBe(false);
  });

  it("does not write stale external values over the last emitted IME value", () => {
    expect(imeSafeShouldWriteExternalValue("abcqdef", "abcqdef", undefined, false)).toBe(false);
    expect(imeSafeShouldWriteExternalValue("abcqdef", "abcXYZdef", "abcqdef", false)).toBe(false);
    expect(imeSafeShouldWriteExternalValue("abcXYZdef", "abcqdef", undefined, true)).toBe(false);
    expect(imeSafeShouldWriteExternalValue("abcXYZdef", "abcqdef", undefined, false)).toBe(true);
  });

  it("recovers the previous selected range when compositionstart sees a collapsed selection", () => {
    expect(
      imeSafeCompositionStartSnapshot(
        { selectionEnd: 1, selectionStart: 1, value: "ACC" },
        { selectionEnd: 3, selectionStart: 0, value: "ACC" },
        "ACC",
        "",
      ),
    ).toEqual({ selectionEnd: 3, selectionStart: 0, value: "ACC" });
    expect(
      imeSafeCompositionStartSnapshot(
        { selectionEnd: 1, selectionStart: 1, value: "ACC" },
        undefined,
        "ACC",
        "ACC",
      ),
    ).toEqual({ selectionEnd: 3, selectionStart: 0, value: "ACC" });
  });

  it("recovers the previous selection when the first IME text already replaced it", () => {
    const fullPrefixMatch = imeSafeCompositionStartSnapshot(
      { selectionEnd: 1, selectionStart: 0, value: "A" },
      { selectionEnd: 3, selectionStart: 0, value: "ABC" },
      "A",
      "A",
    );
    expect(fullPrefixMatch).toEqual({ selectionEnd: 3, selectionStart: 0, value: "ABC" });
    expect(imeSafeCompositionEndValue(fullPrefixMatch, imeSafeCommittedText("A", "A"), "A")).toBe(
      "A",
    );

    const embeddedPrefixMatch = imeSafeCompositionStartSnapshot(
      { selectionEnd: 2, selectionStart: 1, value: "xAz" },
      { selectionEnd: 4, selectionStart: 1, value: "xABCz" },
      "xAz",
      "A",
    );
    expect(embeddedPrefixMatch).toEqual({ selectionEnd: 4, selectionStart: 1, value: "xABCz" });
    expect(
      imeSafeCompositionEndValue(embeddedPrefixMatch, imeSafeCommittedText("A", "A"), "xAz"),
    ).toBe("xAz");

    const japanesePrefixMatch = imeSafeCompositionStartSnapshot(
      { selectionEnd: 1, selectionStart: 0, value: "あ" },
      { selectionEnd: 3, selectionStart: 0, value: "あいう" },
      "あ",
      "あ",
    );
    expect(japanesePrefixMatch).toEqual({ selectionEnd: 3, selectionStart: 0, value: "あいう" });
    expect(
      imeSafeCompositionEndValue(japanesePrefixMatch, imeSafeCommittedText("あ", "あ"), "あ"),
    ).toBe("あ");
  });

  it("does not infer a wider first-text replacement without a previous selection", () => {
    expect(
      imeSafeCompositionStartSnapshot(
        { selectionEnd: 1, selectionStart: 0, value: "A" },
        undefined,
        "A",
        "A",
      ),
    ).toEqual({ selectionEnd: 1, selectionStart: 0, value: "A" });
  });

  it("keeps the previous full selection when compositionstart exposes a short IME range", () => {
    const snapshot = imeSafeCompositionStartSnapshot(
      { selectionEnd: 1, selectionStart: 0, value: "ACC" },
      { selectionEnd: 3, selectionStart: 0, value: "ACC" },
      "ACC",
      "A",
    );

    expect(snapshot).toEqual({ selectionEnd: 3, selectionStart: 0, value: "ACC" });
    expect(imeSafeCompositionEndValue(snapshot, imeSafeCommittedText("", "AC"), "ACCC")).toBe("AC");
  });

  it("keeps flowchart rendering focused on the selected section", () => {
    const flowchartSource = readFlowchartWebviewSource();
    const flowchartStyles = fs.readFileSync("src/webview/flowchart.css", "utf8");
    const flowchartHostSource = fs.readFileSync("src/flowchart-webview.ts", "utf8");
    const virtualListSource = fs.readFileSync("src/webview/virtual-list.tsx", "utf8");

    expect(virtualListSource).toContain('from "@tanstack/virtual-core"');
    expect(virtualListSource).toContain("function VirtualList");
    expect(virtualListSource).toContain("props.items.length > (props.threshold ?? 40)");
    expect(virtualListSource).toContain("virtualizer.measureElement");
    expect(virtualListSource).toContain('class={cn(props.className, "overflow-auto pr-1")}');
    expect(virtualListSource).toContain('class="relative w-full"');
    expect(virtualListSource).toContain('"absolute top-0 left-0 box-border w-full"');
    expect(flowchartSource).toContain(
      "flowchartForSection(payload(), selectedSectionId(), themePalette())",
    );
    expect(flowchartSource).toContain('from "./virtual-list"');
    expect(flowchartSource).toContain("<VirtualList");
    expect(flowchartSource).toContain(
      "scrollToIndex={activeNodeIndex() >= 0 ? activeNodeIndex() : undefined}",
    );
    expect(flowchartSource).toContain('const lines = ["flowchart TB"]');
    expect(flowchartSource).toContain("attachSvgNodeHandlers(");
    expect(flowchartSource).toContain("onOpenContextMenu");
    expect(flowchartSource).toContain("setFocusedFlowchartNodeId(node.id)");
    expect(flowchartSource).toContain("focusedFlowchartNodeId() ?? activeSearchNode()?.id");
    expect(flowchartSource).toContain("onSelectNode(node)");
    expect(flowchartSource).toContain("onOpenFlowchart(node);");
    expect(flowchartSource).toContain('element.addEventListener("click"');
    expect(flowchartSource).toContain("node.links?.some((link) => link.target)");
    expect(flowchartSource).toContain("node.links?.find((link) => link.target)?.target");
    expect(flowchartSource).toContain('element.addEventListener("focus"');
    expect(flowchartSource).toContain('element.setAttribute("role", "button")');
    expect(flowchartSource).toContain("setSvgNodeTitle(element, hint)");
    expect(flowchartSource).toContain("flowchartThemePalettes");
    expect(flowchartSource).toContain("darkFlowchartNodeKindStyles");
    expect(flowchartSource).toContain("lightFlowchartNodeKindStyles");
    expect(flowchartSource).toContain("flowExceptionHandling");
    expect(flowchartSource).toContain('exceptionHandling: "Exception handling"');
    expect(flowchartSource).toContain('exceptionHandling: "例外処理"');
    expect(flowchartSource).toContain('merge: "Merge"');
    expect(flowchartSource).toContain('output: "Response output"');
    expect(flowchartSource).toContain("flowchartNodeVisualStyle(themePalette, node.kind)");
    expect(flowchartSource).toContain("flowchartNodeHint(node, text, locale)");
    expect(flowchartSource).toContain("flowchartMermaidClassDefinitions(themePalette)");
    expect(flowchartSource).toContain('type: "copyText"');
    expect(flowchartSource).toContain('format: "mermaid"');
    expect(flowchartSource).toContain('format: "svg"');
    expect(flowchartSource).toContain("serializedFlowchartSvg(containerRef.current) ?? svg");
    expect(flowchartSource).toContain("new XMLSerializer().serializeToString(clone)");
    expect(flowchartHostSource).toContain("flowchartExportMessageContent(message)");
    expect(flowchartHostSource).toContain("new TextEncoder().encode(content)");
    expect(flowchartHostSource).toContain("exportFailed");
    expect(flowchartHostSource).toContain("openFailed");
    expect(flowchartHostSource).toContain(
      'extensionLocalizerForLocale(locale)("flowchart.openFailed"',
    );
    expect(flowchartHostSource).toContain('<?xml version="1.0" encoding="UTF-8"?>');
    expect(flowchartHostSource).toContain("initialTargetRange");
    expect(flowchartSource).toContain("__ASP_LSP_FLOWCHART_TARGET_RANGE__");
    expect(flowchartSource).toContain("maxTextSize: Number.POSITIVE_INFINITY");
    expect(flowchartSource).toContain("maxEdges: Number.POSITIVE_INFINITY");
    expect(flowchartSource).toContain("const defaultMaximumFlowchartZoom = 4");
    expect(flowchartSource).toContain("payload.settings?.minZoom");
    expect(flowchartSource).toContain("payload.settings?.maxZoom");
    expect(flowchartSource).toContain("flowchartFitWidthZoom");
    expect(flowchartSource).toContain("fitWidthDescription");
    expect(flowchartSource).toContain("function FlowchartToolbar");
    expect(flowchartSource).toContain("WebviewErrorBoundary");
    expect(flowchartSource).toContain("flowchartErrorBoundaryTitle");
    expect(flowchartSource).toContain("renderFailureTitle");
    expect(flowchartSource).toContain("!element || !element.isConnected");
    expect(flowchartSource).toContain("!Number.isFinite(rect.left)");
    expect(flowchartSource).toContain("tooltip?.isConnected");
    expect(flowchartSource).toContain('type FlowchartToolbarMode = "full"');
    expect(flowchartSource).toContain("compactExports");
    expect(flowchartSource).toContain("compactAll");
    expect(flowchartSource).toContain("overflow-x-auto");
    expect(flowchartSource).toContain(
      'const flowchartLabelModes: AspFlowchartLabelMode[] = ["raw", "normal", "description"]',
    );
    expect(flowchartSource).toContain('labelModeNormal: "Normal"');
    expect(flowchartSource).toContain('labelModeRaw: "Raw"');
    expect(flowchartSource).toContain('labelModeDescription: "Prose"');
    expect(flowchartSource).toContain('vscode.postMessage({ type: "reloadFlowchart"');
    expect(flowchartSource).toContain("labelMode,");
    expect(flowchartSource).toContain("flowchartLabelModeForPayload");
    expect(flowchartSource).toContain("onLabelModeChange(mode)");
    expect(flowchartSource).toContain("selectedSectionIdRef");
    expect(flowchartHostSource).toContain('message.type === "reloadFlowchart"');
    expect(flowchartHostSource).toContain("message.labelMode");
    expect(flowchartHostSource).toContain("loadPayload(uri, labelMode)");
    expect(flowchartSource).toContain('openMenu: "Open"');
    expect(flowchartSource).toContain('selectNode: "Select node"');
    expect(flowchartSource).toContain('selectNode: "ノードを選択"');
    expect(flowchartSource).toContain('exportMenu: "Export"');
    expect(flowchartSource).toContain('title={props.section?.label ?? props.text("title")}');
    expect(flowchartSource).toContain("<span>{props.section.label}</span>");
    expect(flowchartSource).toContain("<span title={node.label}>{node.label}</span>");
    expect(flowchartSource).not.toContain(
      "text-left text-xs font-semibold uppercase tracking-wide text-[#9fb0c5] hover:text-[#f1f5f9]",
    );
    expect(flowchartSource).toContain("[scrollbar-gutter:stable]");
    expect(flowchartSource).toContain("new ResizeObserver(updateViewportSize)");
    expect(flowchartSource).toContain("centerFlowchartHorizontally");
    expect(flowchartSource).toContain("flowchartHorizontalPanGutter");
    expect(flowchartSource).toContain('flowchart: { htmlLabels: false, curve: "basis" }');
    expect(flowchartSource).not.toContain("flowchartNodePadding");
    expect(flowchartSource).not.toContain("branchNodePadding");
    expect(flowchartSource).not.toContain("branchNodeHorizontalScale");
    expect(flowchartSource).not.toContain("adjustSvgBranchPolygon");
    expect(flowchartSource).not.toContain("insetSvgCoordinate");
    expect(flowchartSource).toContain("userPannedFlowchartKeyRef");
    expect(flowchartSource).toContain(
      "scaledFlowchartCanvasStyle(svgSize(), zoom(), viewportSize())",
    );
    expect(flowchartSource).toContain("flowchartSvgLayerStyle(svgSize(), zoom(), viewportSize())");
    expect(flowchartSource).not.toContain("style={scaledFlowchartCanvasStyle(svgSize, zoom)}");
    expect(flowchartSource).toContain("beginCanvasPan");
    expect(flowchartSource).toContain("moveCanvasPan");
    expect(flowchartSource).toContain("cursor-grab");
    expect(flowchartSource).toContain("suppressCanvasClickAfterPan");
    expect(flowchartSource).toContain("scrollFlowchartElementIntoViewport");
    expect(flowchartSource).toContain("flowchartNodeForRange");
    expect(flowchartSource).toContain("flowchartNodeForRange(message.payload, targetRange)");
    expect(flowchartSource).toContain("setFocusedFlowchartNodeId(targetNode?.id)");
    expect(flowchartSource).toContain('type FlowchartSourceActiveKind = "hover"');
    expect(flowchartSource).toContain("flowchartSourceHighlights(");
    expect(flowchartSource).toContain("flowchartPrimarySourceHighlight(sourceHighlights())");
    expect(flowchartSource).toContain("flowchartSourceScrollTarget(sourceHighlights");
    expect(flowchartSource).toContain("sectionSourceScrollSequence");
    expect(flowchartSource).toContain("consumedScrollKeysRef");
    expect(flowchartSource).toContain("shouldScrollFlowchartSource");
    expect(flowchartSource).toContain("flowchartSourceHighlightsByPriority(highlights)");
    expect(flowchartSource).toContain("highlightFlowchartSourceWithSetting(");
    expect(flowchartSource).not.toContain("await highlight(");
    expect(flowchartSource).toContain("flowchartSourceHighlightPriority");
    expect(flowchartSource).toContain('kind: "hover"');
    expect(flowchartSource).toContain('kind: "selection"');
    expect(flowchartSource).toContain('kind: "section"');
    expect(flowchartSource).toContain("flowchartSourceRangesForSection");
    expect(flowchartSource).toContain('section.kind !== "topLevel"');
    expect(flowchartSource).toContain("mergeFlowchartSourceRanges");
    expect(flowchartSource).toContain("const selectContextMenuNode = () =>");
    expect(flowchartSource).toContain("onClick={selectContextMenuNode}");
    expect(flowchartSource).toContain("nodes={payload().nodes}");
    expect(flowchartSource).toContain("function flowchartNodeForSourceLine");
    expect(flowchartSource).toContain("flowchartNodeForSourceLine(props.nodes, lineNumber)");
    expect(flowchartSource).toContain("sourceLineNumberFromEvent(event)");
    expect(flowchartSource).toContain("handleSourceCodeMouseMove");
    expect(flowchartSource).toContain("handleSourceCodeDoubleClick");
    expect(flowchartSource).toContain('target?.closest<HTMLElement>("[data-source-line]")');
    expect(flowchartSource).toContain('node.kind !== "start"');
    expect(flowchartSource).toContain('node.kind !== "end"');
    expect(flowchartSource).toContain("flowchartSourceActiveBlockClassName");
    expect(flowchartSource).toContain("flowchartSourceActiveLineClassName");
    expect(flowchartSource).toContain("tooltipPositionFor(triggerRef.current, tooltipRef.current)");
    expect(flowchartSource).toContain('window.addEventListener("scroll", updatePosition, true)');
    expect(flowchartStyles).toContain("--asp-lsp-source-hover-bg");
    expect(flowchartStyles).toContain("--asp-lsp-source-selection-bg");
    expect(flowchartStyles).toContain("--asp-lsp-source-section-bg");
    expect(flowchartStyles).toContain(".asp-lsp-source-active-block--hover");
    expect(flowchartStyles).toContain(".asp-lsp-source-active-block--selection");
    expect(flowchartStyles).toContain(".asp-lsp-source-active-block--section");
    expect(flowchartStyles).toContain(".asp-lsp-source-active-block .asp-lsp-source-active-block");
    expect(flowchartStyles).toContain(
      ".asp-lsp-source-code .asp-lsp-source-line[data-source-line]",
    );
    expect(flowchartSource).toContain("const [open, setOpen] = createSignal(false)");
    expect(flowchartSource).toContain("shouldAutoOpen");
    expect(flowchartSource).toContain("flowchartNodesById(allNodes)");
    expect(flowchartSource).toContain("svgElementsByFlowchartNodeId(container, payload.nodes)");
    expect(flowchartSource).toContain('container.querySelectorAll<SVGGElement>("g[id]")');
    expect(flowchartSource).toContain(
      "svgElementIdContainsMermaidNodeId(element.id, node.mermaidId)",
    );
    expect(flowchartSource).not.toContain('querySelectorAll<SVGGElement>(`[id*="${id}"]`)');
    expect(flowchartSource).toContain("wrapFlowchartLabel");
    expect(flowchartSource).toContain("setClampedZoom");
    expect(flowchartSource).toContain("zoomWithWheel");
    expect(flowchartSource).toContain('vscode.postMessage({ type: "openRange"');
    expect(flowchartSource).not.toContain('type: "openGraphLocation"');
    expect(flowchartSource).not.toContain('openGraph: "グラフを開く"');
    expect(flowchartSource).toContain("escapeMermaidEdgeText");
    expect(flowchartSource).toContain('if (node.kind !== "call")');
    expect(flowchartSource).toContain("function flowchartSearchText");
    expect(flowchartSource).toContain("return node.label;");
    expect(flowchartSource).toContain("node.outputFragments?.length");
    expect(flowchartSource).toContain("highlightFlowchartOutputFragment(");
    expect(flowchartSource).not.toContain('${node.kind} ${node.label} ${section?.label ?? ""}');
    expect(flowchartSource).not.toContain(
      'vscode.postMessage({ type: "openRange", uri: payload.uri, range: node.range })',
    );
  });

  it("declares the workspace file preview webview and selected Excel export", () => {
    const manifest = JSON.parse(fs.readFileSync("package.json", "utf8")) as {
      contributes?: {
        commands?: Array<{ command?: string; title?: string }>;
      };
    };
    const extensionSource = readExtensionSource();
    const buildScript = fs.readFileSync("scripts/build-webview.mjs", "utf8");
    const webviewSource = readWorkspaceFilesWebviewSource();
    const japanesePackageNls = JSON.parse(fs.readFileSync("package.nls.ja.json", "utf8")) as Record<
      string,
      string
    >;

    expect(manifest.contributes?.commands?.map((command) => command.command)).toEqual(
      expect.arrayContaining([
        "aspLsp.showWorkspaceGlobFiles",
        "aspLsp.exportCurrentFileAnalysisExcel",
      ]),
    );
    expect(manifest.contributes?.commands?.map((command) => command.command)).not.toContain(
      "aspLsp.openAnalysisExcelExport",
    );
    expect(extensionSource).toContain("previewWorkspaceFilesServerCommand");
    expect(extensionSource).not.toContain("previewWorkspaceFileRelationsServerCommand");
    expect(extensionSource).toContain("showWorkspaceGlobFiles(context)");
    expect(extensionSource).not.toContain("showAnalysisExcelExport(context)");
    expect(buildScript).toContain("workspace-files.tsx");
    expect(buildScript).toContain("workspace-files.js");
    expect(webviewSource).toContain("__ASP_LSP_WORKSPACE_FILES__");
    expect(webviewSource).toContain('type: "preview"');
    expect(webviewSource).toContain('type: "saveSettings"');
    expect(webviewSource).toContain('type: "saveSettingsResult"');
    expect(webviewSource).not.toContain('type: "previewRelations"');
    expect(webviewSource).toContain('type: "exportSelectedExcel"');
    expect(webviewSource).not.toContain('type: "exportExcel"');
    expect(webviewSource).toContain("VirtualList");
    expect(webviewSource).toContain("globStats");
    expect(webviewSource).toContain("GlobEditor");
    expect(webviewSource).toContain("glob-count");
    expect(webviewSource).toContain("showUnmatched");
    expect(webviewSource).toContain("matchesFilter");
    expect(webviewSource).toContain("onContextMenu");
    expect(webviewSource).toContain("context-menu");
    expect(webviewSource).toContain("excludePatternForTreeRow");
    expect(webviewSource).toContain(
      'return row.kind === "folder" && row.detail ? `${row.detail}/**`',
    );
    expect(webviewSource).toContain("opacity-50");
    expect(webviewSource).toContain("previewRequestSignature");
    expect(webviewSource).toContain("settingsRequestSignature");
    expect(extensionSource).toContain("saveWorkspaceFilesSettings");
    expect(extensionSource).toContain("vscode.ConfigurationTarget.Workspace");
    expect(webviewSource).not.toContain("relation-descendant");
    expect(webviewSource).not.toContain("relation-ancestor");
    expect(webviewSource).not.toContain("relation-relative");
    expect(webviewSource).not.toContain("action.refresh");
    expect(webviewSource).not.toContain("action.preview");
    expect(webviewSource).toContain('@import "tailwindcss";');
    expect(webviewSource).toContain('from "../lib/utils"');
    expect(webviewSource).toContain(
      "grid-template-columns: minmax(0, 1fr) minmax(300px, min(34vw, 420px))",
    );
    expect(webviewSource).toContain("visibleTreeRows(treeRows(payload()), collapsedTreeIds())");
    expect(webviewSource).toContain("function HighlightedText");
    expect(webviewSource).toContain('class="tree-match"');
    expect(webviewSource).toContain("aria-expanded=");
    expect(webviewSource).not.toContain("treeRows(payload, search)");
    expect(webviewSource).not.toContain("root.files.filter");
    expect(webviewSource).toContain('title: "解析ファイル"');
    expect(webviewSource).toContain('projectRoot: "プロジェクトルート"');
    expect(webviewSource).toContain('selectedFile: "選択中のファイル"');
    expect(webviewSource).toContain('showUnmatched: "対象外ファイル/フォルダーも表示"');
    expect(extensionSource).toContain(
      '"workspaceFiles.viewPanelTitle": "Classic ASP ファイル: プロジェクト glob"',
    );
    expect(extensionSource).toContain('"workspaceFiles.serverUnavailable":');
    expect(extensionSource).toContain("ワークスペースファイルをプレビューする前に");
    expect(japanesePackageNls["command.showWorkspaceGlobFiles.title"]).toBe(
      "Classic ASP: プロジェクト glob ファイルを表示",
    );
    expect(japanesePackageNls["command.exportCurrentFileAnalysisExcel.title"]).toBe(
      "Classic ASP: 現在のファイル解析を Excel 出力",
    );
  });

  it("declares a dedicated 2D navigation graph webview without force graph imports", () => {
    const manifest = JSON.parse(fs.readFileSync("package.json", "utf8")) as {
      contributes?: {
        commands?: Array<{ command?: string; title?: string }>;
        menus?: {
          "editor/title"?: Array<{ command?: string; when?: string; group?: string }>;
          "explorer/context"?: Array<{ command?: string; when?: string; group?: string }>;
        };
        configuration?: { properties?: Record<string, unknown> };
      };
    };
    const extensionSource = readExtensionSource();
    const buildScript = fs.readFileSync("scripts/build-webview.mjs", "utf8");
    const webviewSource = readNavigationGraphWebviewSource();
    const nls = JSON.parse(fs.readFileSync("package.nls.json", "utf8")) as Record<string, string>;
    const nlsJa = JSON.parse(fs.readFileSync("package.nls.ja.json", "utf8")) as Record<
      string,
      string
    >;
    const commands = manifest.contributes?.commands?.map((command) => command.command) ?? [];

    expect(commands).toEqual(
      expect.arrayContaining([
        "aspLsp.showCurrentFileNavigationGraph",
        "aspLsp.showFolderNavigationGraph",
        "aspLsp.showWorkspaceNavigationGraph",
      ]),
    );
    expect(extensionSource).toContain("buildNavigationGraphServerCommand");
    expect(extensionSource).toContain("navigationGraphUpdatedNotificationMethod");
    expect(extensionSource).toContain("showAspNavigationGraphWebview");
    expect(buildScript).toContain("navigation-graph.tsx");
    expect(buildScript).toContain("navigation-graph.js");
    expect(webviewSource).toContain("__ASP_LSP_NAVIGATION_GRAPH__");
    expect(webviewSource).toContain('from "solid-js"');
    expect(webviewSource).not.toContain("@xyflow/react");
    expect(webviewSource).toContain("elkjs/lib/elk.bundled.js");
    expect(webviewSource).toContain("NavigationGraphCanvas");
    expect(webviewSource).toContain("layoutNavigationGraphWithElk");
    expect(webviewSource).toContain("navigationGraphToElkGraph");
    expect(webviewSource).toContain("navigationFlowElementsFromElk");
    expect(webviewSource).toContain("navigation-minimap");
    expect(webviewSource).toContain("navigation-viewport-controls");
    expect(webviewSource).toContain("navigation-component-group");
    expect(webviewSource).toContain("fitView");
    expect(webviewSource).toContain("const navigationFitViewPadding = 0.05");
    expect(webviewSource).not.toContain("padding: 0.18");
    expect(webviewSource).toContain("Inspector");
    expect(webviewSource).toContain('type: "openRange"');
    expect(webviewSource).not.toContain("react-force-graph-2d");
    expect(webviewSource).not.toContain("react-force-graph-3d");
    expect(webviewSource).not.toContain("include-graph-model");
    expect(webviewSource).not.toContain("include-graph-theme");
    expect(webviewSource).toContain("nav-node-reveal");
    expect(webviewSource).toContain("nav-edge-draw");
    expect(webviewSource).toContain("nav-dash-flow");
    expect(webviewSource).toContain("nav-selected-pulse");
    expect(webviewSource).toContain("nav-search-pulse");
    expect(webviewSource).toContain("prefers-reduced-motion");
    expect(webviewSource).toContain("hoverClearDelayMs");
    expect(webviewSource).toContain('onHover: () => setHoveredTarget({ kind: "node"');
    expect(webviewSource).toContain("navigation-flow-edge-interaction-path");
    expect(webviewSource).toContain("pointer-events: none");
    expect(webviewSource).toContain("pointer-events: stroke");
    expect(manifest.contributes?.menus?.["editor/title"]).toContainEqual(
      expect.objectContaining({
        command: "aspLsp.showCurrentFileNavigationGraph",
        when: "editorLangId == classic-asp",
        group: "navigation",
      }),
    );
    expect(manifest.contributes?.menus?.["explorer/context"]).toContainEqual(
      expect.objectContaining({
        command: "aspLsp.showFolderNavigationGraph",
        when: "explorerResourceIsFolder",
        group: "navigation",
      }),
    );
    expect(manifest.contributes?.menus?.["explorer/context"]).toContainEqual(
      expect.objectContaining({
        command: "aspLsp.showWorkspaceNavigationGraph",
        when: "explorerResourceIsFolder",
        group: "navigation",
      }),
    );
    expect(nls["command.showCurrentFileNavigationGraph.title"]).toBeTruthy();
    expect(nls["command.showFolderNavigationGraph.title"]).toBeTruthy();
    expect(nls["command.showWorkspaceNavigationGraph.title"]).toBe(
      "Classic ASP: Show Project Navigation Graph",
    );
    expect(nlsJa["command.showCurrentFileNavigationGraph.title"]).toBeTruthy();
    expect(nlsJa["command.showWorkspaceNavigationGraph.title"]).toBe(
      "Classic ASP: プロジェクト画面遷移グラフを表示",
    );
    expect(nls["configuration.navigationGraph.maxNodes.description"]).toBeUndefined();
    expect(nlsJa["configuration.navigationGraph.maxEdges.description"]).toBeUndefined();
  });

  it("converts navigation graph payloads into ELK and framework-independent layout elements", () => {
    const payload = sampleNavigationPayload();
    const elkInput = navigationGraphToElkGraph(payload);
    expect(elkInput.layoutOptions?.["elk.algorithm"]).toBe("org.eclipse.elk.layered");
    expect(elkInput.layoutOptions?.["elk.direction"]).toBe("DOWN");
    expect(elkInput.layoutOptions?.["elk.edgeRouting"]).toBe("ORTHOGONAL");
    const rootNode = elkInput.children?.find((node) => node.id === "file:///workspace/index.asp");
    const externalNode = elkInput.children?.find((node) => node.id === "https://example.com/help");
    const unknownNode = elkInput.children?.find((node) => node.id === "unknown:navigation-target");
    expect(rootNode?.layoutOptions?.["org.eclipse.elk.layered.layering.layerConstraint"]).toBe(
      "FIRST",
    );
    expect(externalNode?.layoutOptions?.["org.eclipse.elk.layered.layering.layerConstraint"]).toBe(
      "LAST",
    );
    expect(unknownNode?.layoutOptions?.["org.eclipse.elk.layered.layering.layerConstraint"]).toBe(
      "LAST",
    );
    expect(rootNode?.layoutOptions?.["elk.portConstraints"]).toBe("FIXED_SIDE");
    expect(rootNode?.ports?.map((port) => port.id)).toEqual([
      "file:///workspace/index.asp:source:edge-search",
    ]);
    expect(rootNode?.ports?.map((port) => port.layoutOptions?.["elk.port.side"])).toEqual([
      "SOUTH",
    ]);
    expect(elkInput.edges?.find((edge) => edge.id === "edge-search")).toEqual(
      expect.objectContaining({
        sources: ["file:///workspace/index.asp:source:edge-search"],
        targets: ["file:///workspace/search.asp:target:edge-search"],
      }),
    );

    const laidOutGraph = {
      ...elkInput,
      width: 820,
      height: 460,
      children: elkInput.children?.map((node, index) => ({
        ...node,
        x: 48 + index * 220,
        y: 40 + index * 72,
      })),
      edges: elkInput.edges?.map((edge, index) => ({
        ...edge,
        sections: [
          {
            id: `${edge.id}:section`,
            startPoint: { x: 286, y: 84 + index * 12 },
            bendPoints: [{ x: 396, y: 84 + index * 12 }],
            endPoint: { x: 488, y: 104 + index * 12 },
          },
        ],
      })),
    };
    const flowLayout = navigationFlowElementsFromElk(payload, laidOutGraph);
    expect(flowLayout.width).toBe(892);
    expect(flowLayout.height).toBe(532);
    expect(
      flowLayout.nodes.find((node) => node.id === "https://example.com/help")?.data.node.kind,
    ).toBe("external");
    expect(
      flowLayout.nodes.find((node) => node.id === "unknown:navigation-target")?.data.node.kind,
    ).toBe("unknown");
    const formEdge = flowLayout.edges.find((edge) => edge.id === "edge-search");
    expect(formEdge?.type).toBe("navigationTransition");
    expect(formEdge?.sourceHandle).toBe("source:edge-search");
    expect(formEdge?.targetHandle).toBe("target:edge-search");
    expect(formEdge?.data?.confidence).toBe("certain");
    expect(formEdge?.data?.edgeKind).toBe("htmlForm");
    expect(formEdge?.data?.method).toBe("GET");
    expect(formEdge?.data?.parameters).toEqual([
      { name: "q", source: "formControl", value: "term", confidence: "certain" },
    ]);
    expect(formEdge?.data?.path).toMatch(/[LQ] 396 96\b/);
  });

  it("assigns deterministic unbounded layers through long chains and cycles", () => {
    const nodes = Array.from({ length: 16 }, (_, index) => ({
      id: `page-${index.toString().padStart(2, "0")}`,
      kind: "page" as const,
      label: `Page ${index}`,
      isRoot: index === 0,
    })).concat([
      { id: "cycle-a", kind: "page" as const, label: "Cycle A", isRoot: false },
      { id: "cycle-b", kind: "page" as const, label: "Cycle B", isRoot: false },
      { id: "tail", kind: "page" as const, label: "Tail", isRoot: false },
    ]);
    const pairs = Array.from({ length: 15 }, (_, index) => [
      `page-${index.toString().padStart(2, "0")}`,
      `page-${(index + 1).toString().padStart(2, "0")}`,
    ]).concat([
      ["page-15", "cycle-a"],
      ["cycle-a", "cycle-b"],
      ["cycle-b", "cycle-a"],
      ["cycle-b", "tail"],
    ]);
    const edges = pairs.map(([source, target], index) => ({
      id: `edge-${index}`,
      source,
      target,
      kind: "htmlAnchor" as const,
      confidence: "certain" as const,
      ranges: [],
      evidence: [],
    }));
    const payload: AspNavigationGraphPayload = {
      scope: "workspace",
      nodes,
      edges,
      stats: {
        documents: nodes.length,
        nodes: nodes.length,
        edges: edges.length,
        certain: edges.length,
        probable: 0,
        possible: 0,
        unknown: 0,
        external: 0,
      },
    };
    const elkInput = navigationGraphToElkGraph(payload);
    const layout = navigationFlowElementsFromElk(payload, {
      ...elkInput,
      children: elkInput.children?.map((node) => ({ ...node, x: 0, y: 0 })),
    });
    const layer = (id: string): number | undefined =>
      layout.nodes.find((node) => node.id === id)?.data.layer;

    expect(layer("page-15")).toBe(15);
    expect(layer("cycle-a")).toBe(16);
    expect(layer("cycle-b")).toBe(16);
    expect(layer("tail")).toBe(17);
    expect(layout.nodes).toHaveLength(nodes.length);
    expect(layout.edges).toHaveLength(edges.length);
  });

  it("does not contribute the removed Classic ASP settings webview", () => {
    const manifest = JSON.parse(fs.readFileSync("package.json", "utf8")) as {
      activationEvents?: string[];
      contributes?: {
        commands?: Array<{ command?: string; title?: string }>;
      };
    };
    const extensionSource = readExtensionSource();
    const buildScript = fs.readFileSync("scripts/build-webview.mjs", "utf8");
    const nls = JSON.parse(fs.readFileSync("package.nls.json", "utf8")) as Record<string, string>;
    const nlsJa = JSON.parse(fs.readFileSync("package.nls.ja.json", "utf8")) as Record<
      string,
      string
    >;

    expect(manifest.activationEvents ?? []).not.toContain("onCommand:aspLsp.openSettings");
    expect(manifest.contributes?.commands?.map((command) => command.command)).not.toContain(
      "aspLsp.openSettings",
    );
    expect(
      manifest.contributes?.commands?.find((command) => command.command === "aspLsp.openSettings"),
    ).toBeUndefined();
    expect(nls["command.openSettings.title"]).toBeUndefined();
    expect(nlsJa["command.openSettings.title"]).toBeUndefined();
    expect(extensionSource).not.toContain("showAspSettingsWebview");
    expect(extensionSource).not.toContain("aspLsp.openSettings");
    expect(buildScript).not.toContain("settings.tsx");
    expect(buildScript).not.toContain("settings.js");
    for (const file of [
      "src/settings-webview.ts",
      "src/settings-metadata.ts",
      "src/webview/settings-preview.ts",
      "src/webview/settings.tsx",
      "src/webview/settings.css",
    ]) {
      expect(fs.existsSync(file), file).toBe(false);
    }
  });

  it("synchronizes every Classic ASP setting change through one batched path", () => {
    const extensionSource = readExtensionSource();

    expect(extensionSource).toContain("vscode.workspace.onDidChangeConfiguration");
    expect(extensionSource).toContain('event.affectsConfiguration("aspLsp")');
    expect(extensionSource).toContain("configurationSyncScheduler?.schedule()");
    expect(extensionSource).toContain("await synchronizeAspLspConfiguration(nextClient)");
    expect(extensionSource).not.toContain('configurationSection: "aspLsp"');
  });

  it("does not authorize external filesystem roots from an untrusted workspace", () => {
    const extensionSource = readExtensionSource();

    expect(extensionSource).toContain(
      "vscode.workspace.onDidGrantWorkspaceTrust(() => configurationSyncScheduler?.schedule())",
    );
    expect(extensionSource).toContain("vscode.workspace.isTrusted");
    expect(extensionSource).toContain("safe.includePaths = []");
    expect(extensionSource).toContain('safe.virtualRoot = ""');
    expect(extensionSource).toContain("safe.virtualRoots = []");
    expect(extensionSource).toContain("workspaceConfigurationMiddleware");
    expect(extensionSource).toContain('section === "aspLsp"');
  });

  it("releases replaced language clients and file watchers", () => {
    const extensionSource = readExtensionSource();
    const startClientSource = extensionSource.slice(
      extensionSource.indexOf("async function startClient"),
      extensionSource.indexOf("export async function deactivate"),
    );
    const deactivateSource = extensionSource.slice(
      extensionSource.indexOf("export async function deactivate"),
      extensionSource.indexOf("async function synchronizeAspLspConfiguration"),
    );
    const restartSource = extensionSource.slice(
      extensionSource.indexOf("async function restartServerOnce"),
      extensionSource.indexOf("function updateStatusBar"),
    );

    expect(extensionSource).toContain(
      "let fileSystemWatcher: vscode.FileSystemWatcher | undefined",
    );
    expect(startClientSource).toContain("fileEvents: nextFileSystemWatcher");
    expect(startClientSource).toContain("if (fileSystemWatcher === nextFileSystemWatcher)");
    expect(startClientSource).toContain("nextFileSystemWatcher.dispose()");
    expect(startClientSource).toContain("await nextClient.dispose()");
    expect(startClientSource).not.toContain("context.subscriptions.push(nextClient)");
    expect(deactivateSource).toContain("fileSystemWatcher?.dispose()");
    expect(deactivateSource).toContain("fileSystemWatcher = undefined");
    expect(deactivateSource).toContain("await activeClient?.dispose()");
    expect(deactivateSource).toContain("disposeNavigationGraphPanels()");
    expect(restartSource).toContain("fileSystemWatcher?.dispose()");
    expect(restartSource).toContain("fileSystemWatcher = undefined");
    expect(restartSource).toContain("await activeClient?.dispose()");
    expect(restartSource).toContain("disposeNavigationGraphPanels()");
    expect(extensionSource).toContain("tracked.panel.dispose()");
  });

  it("keeps a newer navigation panel registered when an older duplicate is disposed", () => {
    const extensionSource = readExtensionSource();
    const showNavigationGraphSource = extensionSource.slice(
      extensionSource.indexOf("async function showNavigationGraph"),
      extensionSource.indexOf("function navigationGraphCommandRequest"),
    );

    expect(showNavigationGraphSource).toContain(
      "if (navigationGraphPanelsByKey.get(key)?.panel === panel)",
    );
    expect(showNavigationGraphSource).not.toContain(
      "panel.onDidDispose(() => navigationGraphPanelsByKey.delete(key))",
    );
  });

  it("contributes commands and settings", () => {
    const manifest = JSON.parse(fs.readFileSync("package.json", "utf8")) as {
      repository?: { url?: string };
      icon?: string;
      galleryBanner?: { color?: string };
      dependencies?: Record<string, string>;
      activationEvents?: string[];
      contributes?: {
        languages?: Array<{ id: string; extensions?: string[]; configuration?: string }>;
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
        commands?: Array<{ command: string; title?: string; icon?: string }>;
        keybindings?: Array<{ command?: string; key?: string; mac?: string; when?: string }>;
        menus?: {
          "editor/title"?: Array<{ command?: string; when?: string; group?: string }>;
          "explorer/context"?: Array<{ command?: string; when?: string; group?: string }>;
        };
        configuration?: { properties?: Record<string, unknown> };
      };
      capabilities?: { untrustedWorkspaces?: { supported?: boolean } };
    };
    const rootManifest = JSON.parse(fs.readFileSync("../../package.json", "utf8")) as {
      license?: string;
    };
    const nls = JSON.parse(fs.readFileSync("package.nls.json", "utf8")) as Record<string, string>;
    const nlsJa = JSON.parse(fs.readFileSync("package.nls.ja.json", "utf8")) as Record<
      string,
      string
    >;
    const commands = manifest.contributes?.commands?.map((command) => command.command) ?? [];
    const keybindings = manifest.contributes?.keybindings ?? [];
    const configuration = manifest.contributes?.configuration?.properties ?? {};
    expect(rootManifest.license).toBe("MIT OR Apache-2.0");
    expect(manifest.license).toBe("MIT OR Apache-2.0");
    expect(manifest.dependencies?.["@xyflow/react"]).toBeUndefined();
    expect(manifest.dependencies?.["elkjs"]).toBe("^0.12.0");
    expect(manifest.dependencies?.["react-force-graph-2d"]).toBeUndefined();
    expect(manifest.dependencies?.["react-force-graph-3d"]).toBeUndefined();
    expect(manifest.dependencies?.["three-spritetext"]).toBeUndefined();
    expect(manifest.dependencies?.["write-excel-file"]).toBeUndefined();
    expect(fs.existsSync("../../LICENSE-MIT")).toBe(true);
    expect(fs.existsSync("../../LICENSE-APACHE")).toBe(true);
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
    expect(nls["command.showProgressDetails.title"]).toBe("Classic ASP: Show Progress Details");
    expect(nlsJa["command.showProgressDetails.title"]).toBe("Classic ASP: 進行状況を表示");
    expect(commands).toContain("aspLsp.reindexWorkspace");
    expect(commands).toContain("aspLsp.clearCache");
    expect(commands).toContain("aspLsp.clearDiskCache");
    expect(commands).toContain("aspLsp.clearProcessCache");
    expect(commands).toContain("aspLsp.openOutput");
    expect(commands).not.toContain("aspLsp.openSettings");
    expect(commands).toContain("aspLsp.showProgressDetails");
    expect(commands).not.toContain("aspLsp.debugIisUrl");
    expect(commands).not.toContain("aspLsp.debugIisExpressUrl");
    expect(commands).not.toContain("aspLsp.createLaunchConfig");
    const removedAnalysisSetting = "analysis" + "Backend";
    const removedAnalysisEnv = "ASP_LSP_ANALYSIS_" + "BACKEND";
    expect(configuration[`aspLsp.${removedAnalysisSetting}`]).toBeUndefined();
    const extensionSourceText = readExtensionSource();
    expect(extensionSourceText).not.toContain(removedAnalysisEnv);
    expect(extensionSourceText).not.toContain(`aspLsp.${removedAnalysisSetting}`);
    expect(configuration["aspLsp.debug.logFile.enabled"]).toEqual(
      expect.objectContaining({
        type: "boolean",
        default: false,
        description: "%configuration.debug.logFile.enabled.description%",
        tags: ["advanced"],
      }),
    );
    expect(configuration["aspLsp.debug.logFile.path"]).toEqual(
      expect.objectContaining({
        type: "string",
        default: "",
        description: "%configuration.debug.logFile.path.description%",
        tags: ["advanced"],
      }),
    );
    expect(nls["configuration.debug.logFile.enabled.description"]).toBeTruthy();
    expect(nls["configuration.debug.logFile.path.description"]).toBeTruthy();
    expect(nlsJa["configuration.debug.logFile.enabled.description"]).toBeTruthy();
    expect(nlsJa["configuration.debug.logFile.path.description"]).toBeTruthy();
    expect(extensionSourceText).toContain("ASP_LSP_DEFAULT_DEBUG_LOG_FILE");
    expect(extensionSourceText).toContain('const serverStatusNotificationMethod = "aspLsp/status"');
    expect(extensionSourceText).not.toContain("aspLsp/graphUpdated");
    const advancedConfigurationSettings = [
      "aspLsp.incremental.mode",
      "aspLsp.incremental.analysis",
      "aspLsp.diagnostics.debounceMs",
      "aspLsp.debug.output",
      "aspLsp.debug.logFile.enabled",
      "aspLsp.debug.logFile.path",
      "aspLsp.javascript.ignoreProjectConfig",
      "aspLsp.windowsPathResolution",
      "aspLsp.legacyEncoding",
      "aspLsp.vbscript.identifierCaseByKind",
      "aspLsp.vbscript.comTypes",
      "aspLsp.vbscript.globals",
      "aspLsp.vbscript.autoIncludes",
      "aspLsp.vbscript.showUnresolvedSymbolsInCompletion",
      "aspLsp.vbscript.initializedDimQuickFixStyle",
      "aspLsp.vbscript.ifSyntaxDiagnostics",
      "aspLsp.codeLens.includeRelatedIncludeTreesForUnresolved",
      "aspLsp.rename.updateIncludesOnFileRename",
      "aspLsp.rename.workspaceSymbolRename",
      "aspLsp.flowchart.minZoom",
      "aspLsp.flowchart.maxZoom",
      "aspLsp.excel.includeRelatedIncludeTreesForUnresolved",
      "aspLsp.excel.skipTypeInference",
      "aspLsp.cache.enabled",
      "aspLsp.cache.directory",
      "aspLsp.cache.freshness",
      "aspLsp.cache.ttlHours",
      "aspLsp.cache.maxSizeMb",
      "aspLsp.cache.gzip",
      "aspLsp.memory.maxCacheBytes",
      "aspLsp.memory.debugTelemetry",
      "aspLsp.network.profile",
      "aspLsp.network.statCacheTtlMs",
      "aspLsp.network.readdirCacheTtlMs",
      "aspLsp.network.includeReadConcurrency",
      "aspLsp.network.caseResolution",
      "aspLsp.workspace.scanChunkSize",
      "aspLsp.workspace.busyAnalysisConcurrency",
    ];
    for (const setting of advancedConfigurationSettings) {
      expect(configuration[setting]).toEqual(expect.objectContaining({ tags: ["advanced"] }));
    }
    expect(extensionSourceText).toContain(
      'const cancelProgressTaskServerCommand = "aspLsp.server.cancelProgressTask"',
    );
    expect(extensionSourceText).toContain("handleServerStatusNotification");
    expect(extensionSourceText).not.toContain("handleGraphUpdatedNotification");
    expect(extensionSourceText).not.toContain("graphPanelsByCorrelation");
    expect(extensionSourceText).toContain("showProgressDetails");
    expect(extensionSourceText).toContain('statusBarItem.command = "aspLsp.showProgressDetails"');
    expect(extensionSourceText).toContain("status.loading.text");
    expect(extensionSourceText).toContain("status.analyzing.text");
    expect(extensionSourceText).toContain("progressStatusText");
    expect(extensionSourceText).toContain("progressValueText");
    expect(extensionSourceText).toContain("normalizeProgressValue");
    expect(extensionSourceText).toContain(
      "Math.round((normalized.current / normalized.total) * 100)",
    );
    expect(extensionSourceText).toContain("status.progress.loadingStatusText");
    expect(extensionSourceText).toContain("status.progress.analyzingStatusText");
    expect(extensionSourceText).toContain("status.progress.excel");
    expect(extensionSourceText).toContain("status.progress.excelGraph");
    expect(extensionSourceText).toContain("status.progress.excelNormalizeGraph");
    expect(extensionSourceText).toContain("status.progress.excelAnalysisContext");
    expect(extensionSourceText).toContain("status.progress.excelSheet");
    expect(extensionSourceText).toContain("status.progress.excelChooseFile");
    expect(extensionSourceText).toContain("status.progress.excelSheets");
    expect(extensionSourceText).toContain("status.progress.excelWorkbook");
    expect(extensionSourceText).toContain("status.progress.excelFile");
    expect(extensionSourceText).toContain("status.progress.excelFileRows");
    expect(extensionSourceText).toContain("excelGraphProgressStageLabelKey");
    expect(extensionSourceText).toContain("status.progress.graphIndexDocuments");
    expect(extensionSourceText).toContain("status.progress.graphAddUsages");
    expect(extensionSourceText).toContain("status.progress.graphResolveIncludes");
    expect(extensionSourceText).toContain("status.progress.graphFindIncomingIncludes");
    expect(extensionSourceText).toContain("status.progress.graphFilterIncomingIncludes");
    expect(extensionSourceText).toContain("status.progress.workspaceIndexScanFiles");
    expect(extensionSourceText).toContain("progressTasksForActiveDocument");
    expect(extensionSourceText).toContain("activeProgressDocument");
    expect(extensionSourceText).toContain("record.documentVersion");
    expect(extensionSourceText).toContain("vscode.window.onDidChangeActiveTextEditor");
    expect(extensionSourceText).toContain("progressStatusBarDetail");
    expect(extensionSourceText).toContain("progressTaskStatusPriority");
    expect(extensionSourceText).toContain("withServerTaskProgress");
    expect(extensionSourceText.match(/withServerTaskProgress\(/g)).toHaveLength(4);
    expect(extensionSourceText.match(/vscode\.window\.withProgress/g)).toHaveLength(1);
    expect(extensionSourceText).toContain("reportServerProgressTasks(serverProgressTasks)");
    expect(extensionSourceText).toContain("claimedServerProgressTaskIds");
    expect(extensionSourceText).toContain("serverProgressReporterMatchScore");
    expect(extensionSourceText).toContain("registration.exactLabels.has(task.label)");
    expect(extensionSourceText).toContain("task.updatedAt >= registration.startedAt");
    expect(extensionSourceText).toContain("left.id - right.id");
    expect(extensionSourceText).toContain("progressPercentage(progressFromTask(task))");
    expect(extensionSourceText).toContain('{ labelPrefixes: ["flowchart."] }');
    expect(extensionSourceText).toContain('{ labelPrefixes: ["navigationGraph."] }');
    expect(extensionSourceText).toContain('{ exactLabels: ["workspace.previewFiles"] }');
    expect(extensionSourceText).toContain('{ labelPrefixes: ["excel."] }');
    expect(extensionSourceText).toContain('task.label.startsWith("excel.")');
    expect(extensionSourceText).toContain('label: "excel.chooseFile"');
    expect(extensionSourceText).toContain('label: "excel.graph"');
    expect(extensionSourceText).toContain("Generating current file graph");
    expect(extensionSourceText).toContain("Collecting Excel analysis graph");
    expect(extensionSourceText).toContain("Normalizing Excel graph payload");
    expect(extensionSourceText).toContain("Resolving include paths");
    expect(extensionSourceText).toContain("Writing Excel rows");
    expect(extensionSourceText).toContain("Excel 作成中");
    expect(extensionSourceText).toContain("Excel 解析グラフを取得中");
    expect(extensionSourceText).toContain("Excel 行を書き込み中");
    expect(extensionSourceText).toContain("フローチャートを生成中");
    expect(extensionSourceText).toContain("Classic ASP 解析ブックを作成中");
    expect(extensionSourceText).not.toContain("graphAnalysisLimitSettings");
    const analysisExcelSource = fs.readFileSync("../../internal/excel/export.go", "utf8");
    const languageServerSource = readLanguageServerSource();
    const graphBuildSource = fs.readFileSync("../../internal/graph/graph.go", "utf8");
    const graphSource = `${languageServerSource}\n${graphBuildSource}`;
    expect(analysisExcelSource).toContain("for rowIndex, row := range rows");
    expect(analysisExcelSource).toContain("for columnIndex, value := range row");
    expect(analysisExcelSource).not.toContain("Math.max(...rows.map");
    expect(graphSource).toContain("type Payload struct");
    expect(graphSource).not.toContain("documentsForGraph.push(...indexedGraphDocuments)");
    expect(languageServerSource).not.toContain("existing.push(...references)");
    expect(manifest.contributes).not.toHaveProperty("taskDefinitions");
    expect(manifest.contributes).not.toHaveProperty("problemMatchers");
    expect(extensionSourceText).not.toContain("registerTaskProvider");
    expect(extensionSourceText).not.toContain("AspLspTaskProvider");
    const removedIisSettings = [
      "aspLsp.iis.url",
      "aspLsp.iis.webRoot",
      "aspLsp.iis.browser",
      "aspLsp.iisExpress.url",
      "aspLsp.iisExpress.webRoot",
      "aspLsp.iisExpress.browser",
    ];
    for (const key of removedIisSettings) {
      expect(manifest.contributes?.configuration?.properties?.[key]).toBeUndefined();
    }
    const removedIisNlsKeys = [
      "configuration.iis.url.description",
      "configuration.iis.webRoot.description",
      "configuration.iis.browser.description",
      "configuration.iisExpress.url.description",
      "configuration.iisExpress.webRoot.description",
      "configuration.iisExpress.browser.description",
      "command.debugIisUrl.title",
      "command.debugIisExpressUrl.title",
      "command.createLaunchConfig.title",
    ];
    for (const key of removedIisNlsKeys) {
      expect(nls[key]).toBeUndefined();
      expect(nlsJa[key]).toBeUndefined();
    }
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.vbscript.unusedDiagnostics"],
    ).toBeTruthy();
    expect(
      manifest.contributes?.configuration?.properties?.[
        "aspLsp.vbscript.implicitGlobalDiagnostics"
      ],
    ).toEqual(expect.objectContaining({ type: "boolean", default: false }));
    expect(nls["configuration.vbscript.implicitGlobalDiagnostics.description"]).toBeTruthy();
    expect(nlsJa["configuration.vbscript.implicitGlobalDiagnostics.description"]).toBeTruthy();
    const removedIncludeSuggestions = "include" + "Suggestions";
    expect(
      manifest.contributes?.configuration?.properties?.[
        `aspLsp.vbscript.${removedIncludeSuggestions}`
      ],
    ).toBeUndefined();
    const removedIncludeSuggestionMaxFiles = "include" + "SuggestionMaxFiles";
    expect(
      manifest.contributes?.configuration?.properties?.[
        `aspLsp.vbscript.${removedIncludeSuggestionMaxFiles}`
      ],
    ).toBeUndefined();
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.vbscript.syntaxSnippets"],
    ).toEqual(expect.objectContaining({ type: "boolean", default: true }));
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.vbscript.syntaxKeywords"],
    ).toEqual(expect.objectContaining({ type: "boolean", default: true }));
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.vbscript.autoIncludes"],
    ).toEqual(
      expect.objectContaining({
        type: "boolean",
        default: false,
        description: "%configuration.vbscript.autoIncludes.description%",
        tags: ["advanced"],
      }),
    );
    expect(nls["configuration.vbscript.autoIncludes.description"]).toBeTruthy();
    expect(nlsJa["configuration.vbscript.autoIncludes.description"]).toBeTruthy();
    expect(
      manifest.contributes?.configuration?.properties?.[
        "aspLsp.vbscript.initializedDimQuickFixStyle"
      ],
    ).toEqual(
      expect.objectContaining({
        type: "string",
        enum: ["newline", "sameLineColon"],
        default: "sameLineColon",
      }),
    );
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.vbscript.ifSyntaxDiagnostics"],
    ).toEqual(expect.objectContaining({ type: "string", default: "basic" }));
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
      manifest.contributes?.configuration?.properties?.["aspLsp.javascript.compilerOptions"],
    ).toEqual(expect.objectContaining({ type: "object", default: {}, tags: ["advanced"] }));
    expect(nls["configuration.javascript.compilerOptions.description"]).toBeTruthy();
    expect(nlsJa["configuration.javascript.compilerOptions.description"]).toBeTruthy();
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.inlayHints.implicitByRef"],
    ).toEqual(expect.objectContaining({ type: "boolean", default: false }));
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.inlayHints.variableTypes"],
    ).toEqual(expect.objectContaining({ type: "boolean", default: false }));
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.inlayHints.functionReturnTypes"],
    ).toEqual(expect.objectContaining({ type: "boolean", default: false }));
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.inlayHints.scopeMarkers.global"],
    ).toEqual(expect.objectContaining({ type: "boolean", default: false }));
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.inlayHints.scopeMarkers.local"],
    ).toEqual(expect.objectContaining({ type: "boolean", default: false }));
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.inlayHints.scopeMarkers.uncertain"],
    ).toEqual(expect.objectContaining({ type: "boolean", default: false }));
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.inlayHints.globalVariableMarkers"],
    ).toBeUndefined();
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.codeLens.referenceScope"],
    ).toBeUndefined();
    expect(
      manifest.contributes?.configuration?.properties?.[
        "aspLsp.codeLens.includeRelatedIncludeTreesForUnresolved"
      ],
    ).toEqual(expect.objectContaining({ type: "boolean", default: true }));
    expect(
      nls["configuration.codeLens.includeRelatedIncludeTreesForUnresolved.description"],
    ).toBeTruthy();
    expect(
      nlsJa["configuration.codeLens.includeRelatedIncludeTreesForUnresolved.description"],
    ).toBeTruthy();
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.styleExtraction.insertionMode"],
    ).toEqual(
      expect.objectContaining({
        type: "string",
        enum: ["nearby", "reuseExistingStyleTag"],
        default: "nearby",
      }),
    );
    expect(nls["configuration.styleExtraction.insertionMode.description"]).toBeTruthy();
    expect(nlsJa["configuration.styleExtraction.insertionMode.description"]).toBeTruthy();
    expect(
      manifest.contributes?.configuration?.properties?.[
        "aspLsp.vbscript.showUnresolvedSymbolsInCompletion"
      ],
    ).toEqual(
      expect.objectContaining({
        type: "boolean",
        default: false,
      }),
    );
    expect(
      nls["configuration.vbscript.showUnresolvedSymbolsInCompletion.description"],
    ).toBeTruthy();
    expect(
      nlsJa["configuration.vbscript.showUnresolvedSymbolsInCompletion.description"],
    ).toBeTruthy();
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.vbscript.assumeUndefinedGlobals"],
    ).toEqual(
      expect.objectContaining({
        type: "boolean",
        default: false,
      }),
    );
    expect(nls["configuration.vbscript.assumeUndefinedGlobals.description"]).toBeTruthy();
    expect(nlsJa["configuration.vbscript.assumeUndefinedGlobals.description"]).toBeTruthy();
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.flowchart.maxTextSize"],
    ).toBeUndefined();
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.flowchart.maxEdges"],
    ).toBeUndefined();
    expect(nls["configuration.flowchart.maxTextSize.description"]).toBeUndefined();
    expect(nlsJa["configuration.flowchart.maxEdges.description"]).toBeUndefined();
    for (const setting of ["flowchart", "navigationGraph", "workspaceFiles"]) {
      expect(
        manifest.contributes?.configuration?.properties?.[`aspLsp.${setting}.openLocation`],
      ).toEqual(
        expect.objectContaining({ type: "string", enum: ["active", "beside"], default: "active" }),
      );
      expect(nls[`configuration.${setting}.openLocation.description`]).toBeTruthy();
      expect(nlsJa[`configuration.${setting}.openLocation.description`]).toBeTruthy();
    }
    expect(manifest.contributes?.configuration?.properties?.["aspLsp.flowchart.labelMode"]).toEqual(
      expect.objectContaining({
        type: "string",
        enum: ["normal", "raw", "description"],
        default: "normal",
      }),
    );
    expect(nls["configuration.flowchart.labelMode.description"]).toBeTruthy();
    expect(nlsJa["configuration.flowchart.labelMode.description"]).toBeTruthy();
    expect(manifest.contributes?.configuration?.properties?.["aspLsp.flowchart.minZoom"]).toEqual(
      expect.objectContaining({
        type: "number",
        minimum: 0.1,
        default: 0.1,
      }),
    );
    expect(manifest.contributes?.configuration?.properties?.["aspLsp.flowchart.maxZoom"]).toEqual(
      expect.objectContaining({
        type: "number",
        minimum: 0.1,
        default: 4,
      }),
    );
    expect(nls["configuration.flowchart.minZoom.description"]).toBeTruthy();
    expect(nlsJa["configuration.flowchart.minZoom.description"]).toBeTruthy();
    expect(nls["configuration.flowchart.maxZoom.description"]).toBeTruthy();
    expect(nlsJa["configuration.flowchart.maxZoom.description"]).toBeTruthy();
    for (const key of [
      "referenceProcedures",
      "referenceGlobals",
      "referenceClasses",
      "referenceClassMembers",
    ]) {
      expect(manifest.contributes?.configuration?.properties?.[`aspLsp.codeLens.${key}`]).toEqual(
        expect.objectContaining({ type: "boolean", default: true }),
      );
    }
    expect(
      Object.keys(manifest.contributes?.configuration?.properties ?? {}).some((key) =>
        key.startsWith("aspLsp.graph."),
      ),
    ).toBe(false);
    expect(Object.keys(nls).some((key) => key.startsWith("configuration.graph."))).toBe(false);
    expect(Object.keys(nlsJa).some((key) => key.startsWith("configuration.graph."))).toBe(false);
    expect(
      manifest.contributes?.configuration?.properties?.[
        "aspLsp.excel.includeRelatedIncludeTreesForUnresolved"
      ],
    ).toEqual(expect.objectContaining({ type: "boolean", default: true }));
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.excel.skipTypeInference"],
    ).toEqual(expect.objectContaining({ type: "boolean", default: false }));
    expect(manifest.contributes?.configuration?.properties?.["aspLsp.excel.locale"]).toEqual(
      expect.objectContaining({ type: "string", enum: ["auto", "en", "ja"], default: "auto" }),
    );
    expect(
      nls["configuration.excel.includeRelatedIncludeTreesForUnresolved.description"],
    ).toBeTruthy();
    expect(
      nlsJa["configuration.excel.includeRelatedIncludeTreesForUnresolved.description"],
    ).toBeTruthy();
    expect(nls["configuration.excel.skipTypeInference.description"]).toBeTruthy();
    expect(nlsJa["configuration.excel.skipTypeInference.description"]).toBeTruthy();
    expect(nls["configuration.excel.includeTreeMaxDocuments.description"]).toBeUndefined();
    expect(nlsJa["configuration.excel.includeTreeMaxDocuments.description"]).toBeUndefined();
    expect(nls["configuration.excel.includeTreeMaxTextLength.description"]).toBeUndefined();
    expect(nlsJa["configuration.excel.includeTreeMaxTextLength.description"]).toBeUndefined();
    expect(nls["configuration.excel.maxDocuments.description"]).toBeUndefined();
    expect(nlsJa["configuration.excel.maxDocuments.description"]).toBeUndefined();
    expect(nls["configuration.excel.maxTextLength.description"]).toBeUndefined();
    expect(nlsJa["configuration.excel.maxTextLength.description"]).toBeUndefined();
    expect(nls["configuration.excel.locale.description"]).toBeTruthy();
    expect(nlsJa["configuration.excel.locale.description"]).toBeTruthy();
    expect(manifest.contributes?.configuration?.properties?.["aspLsp.locale"]).toBeTruthy();
    expect(manifest.contributes?.configuration?.properties?.["aspLsp.incremental.mode"]).toEqual(
      expect.objectContaining({
        type: "string",
        enum: ["legacy", "full", "off"],
        default: "full",
      }),
    );
    expect(nls["configuration.incremental.mode.description"]).toBeTruthy();
    expect(nlsJa["configuration.incremental.mode.description"]).toBeTruthy();
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.incremental.analysis"],
    ).toEqual(expect.objectContaining({ type: "boolean", default: true }));
    expect(nls["configuration.incremental.analysis.description"]).toBeTruthy();
    expect(nlsJa["configuration.incremental.analysis.description"]).toBeTruthy();
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
    expect(manifest.contributes?.configuration?.properties?.["aspLsp.cache.freshness"]).toEqual(
      expect.objectContaining({
        type: "string",
        enum: ["auto", "metadata", "watch"],
        default: "auto",
      }),
    );
    expect(manifest.contributes?.configuration?.properties?.["aspLsp.cache.ttlHours"]).toEqual(
      expect.objectContaining({ type: "number", default: 336, minimum: 1 }),
    );
    expect(manifest.contributes?.configuration?.properties?.["aspLsp.cache.maxSizeMb"]).toEqual(
      expect.objectContaining({ type: "number", default: 16384, minimum: 1 }),
    );
    expect(manifest.contributes?.configuration?.properties?.["aspLsp.cache.gzip"]).toEqual(
      expect.objectContaining({ type: "boolean", default: false }),
    );
    for (const key of [
      "configuration.cache.enabled.description",
      "configuration.cache.directory.description",
      "configuration.cache.freshness.description",
      "configuration.cache.ttlHours.description",
      "configuration.cache.maxSizeMb.description",
      "configuration.cache.gzip.description",
    ]) {
      expect(nls[key]).toContain("analysis database");
      expect(nls[key]).not.toMatch(/disk (?:analysis )?cache/i);
      expect(nlsJa[key]).toContain("解析データベース");
      expect(nlsJa[key]).not.toMatch(/disk|ディスク解析キャッシュ/i);
    }
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.memory.maxCacheBytes"],
    ).toEqual(expect.objectContaining({ type: "number", default: 536870912, minimum: 1 }));
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.memory.debugTelemetry"],
    ).toEqual(expect.objectContaining({ type: "boolean", default: false }));
    expect(nls["configuration.memory.maxCacheBytes.description"]).toBeTruthy();
    expect(nlsJa["configuration.memory.maxCacheBytes.description"]).toBeTruthy();
    expect(nls["configuration.memory.debugTelemetry.description"]).toBeTruthy();
    expect(nlsJa["configuration.memory.debugTelemetry.description"]).toBeTruthy();
    expect(manifest.contributes?.configuration?.properties?.["aspLsp.network.profile"]).toEqual(
      expect.objectContaining({
        type: "string",
        enum: ["auto", "local", "network"],
        default: "auto",
      }),
    );
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.network.statCacheTtlMs"],
    ).toEqual(expect.objectContaining({ type: "number", default: -1, minimum: -1 }));
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.network.readdirCacheTtlMs"],
    ).toEqual(expect.objectContaining({ type: "number", default: -1, minimum: -1 }));
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.network.includeReadConcurrency"],
    ).toEqual(expect.objectContaining({ type: "number", default: 0, minimum: 0 }));
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.network.caseResolution"],
    ).toEqual(
      expect.objectContaining({
        type: "string",
        enum: ["auto", "full", "fast"],
        default: "auto",
      }),
    );
    for (const name of [
      "profile",
      "statCacheTtlMs",
      "readdirCacheTtlMs",
      "includeReadConcurrency",
      "caseResolution",
    ]) {
      expect(nls[`configuration.network.${name}.description`]).toBeTruthy();
      expect(nlsJa[`configuration.network.${name}.description`]).toBeTruthy();
    }
    expect(manifest.contributes?.configuration?.properties?.["aspLsp.workspace.includes"]).toEqual(
      expect.objectContaining({
        type: "array",
        default: ["**/*.{asp,asa,inc,vbs}"],
      }),
    );
    expect(manifest.contributes?.configuration?.properties?.["aspLsp.workspace.excludes"]).toEqual(
      expect.objectContaining({ type: "array", default: [] }),
    );
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.workspace.respectGitIgnore"],
    ).toEqual(expect.objectContaining({ type: "boolean", default: false }));
    for (const name of ["includes", "excludes", "respectGitIgnore"]) {
      expect(nls[`configuration.workspace.${name}.description`]).toBeTruthy();
      expect(nlsJa[`configuration.workspace.${name}.description`]).toBeTruthy();
    }
    const removedBackgroundAnalysis = "background" + "Analysis";
    const removedIdleAnalysisConcurrency = "i" + "dleAnalysisConcurrency";
    expect(
      manifest.contributes?.configuration?.properties?.[
        `aspLsp.workspace.${removedBackgroundAnalysis}`
      ],
    ).toBeUndefined();
    expect(
      manifest.contributes?.configuration?.properties?.[
        `aspLsp.workspace.${removedIdleAnalysisConcurrency}`
      ],
    ).toBeUndefined();
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.workspace.busyAnalysisConcurrency"],
    ).toEqual(expect.objectContaining({ type: "number", default: 0, minimum: 0 }));
    expect(manifest.contributes?.configuration?.properties?.["aspLsp.legacyEncoding"]).toEqual(
      expect.objectContaining({
        enum: ["auto", "utf8", "shift_jis", "cp932"],
        default: "auto",
      }),
    );
    const formatSettings = Object.keys(
      manifest.contributes?.configuration?.properties ?? {},
    ).filter((setting) => setting.startsWith("aspLsp.format."));
    for (const setting of formatSettings.filter(
      (setting) =>
        setting !== "aspLsp.format.uppercaseKeywords" &&
        setting !== "aspLsp.format.alignAssignments",
    )) {
      expect(manifest.contributes?.configuration?.properties?.[setting]).toEqual(
        expect.objectContaining({ tags: ["advanced"] }),
      );
    }
    expect(manifest.contributes?.configuration?.properties?.["aspLsp.format.indentSize"]).toEqual(
      expect.objectContaining({ type: ["number", "null"], default: null, minimum: 1 }),
    );
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.format.vbscriptIndentSize"],
    ).toEqual(expect.objectContaining({ type: ["number", "null"], default: null, minimum: 1 }));
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.format.vbscriptIndentStyle"],
    ).toEqual(expect.objectContaining({ type: "string", enum: ["space", "tab"] }));
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.format.vbscriptBlockIndent"],
    ).toEqual(
      expect.objectContaining({
        type: "string",
        enum: ["alignWithDelimiter", "indentInsideDelimiter"],
        default: "indentInsideDelimiter",
      }),
    );
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.format.enabledLanguages"],
    ).toEqual(
      expect.objectContaining({
        type: "array",
        uniqueItems: true,
        default: ["html", "vbscript", "css", "javascript", "jscript"],
      }),
    );
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.format.htmlWrapAttributes"],
    ).toEqual(
      expect.objectContaining({
        type: "string",
        enum: [
          "auto",
          "force",
          "force-aligned",
          "force-expand-multiline",
          "aligned-multiple",
          "preserve",
          "preserve-aligned",
        ],
        default: "auto",
      }),
    );
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.format.cssBraceStyle"],
    ).toEqual(
      expect.objectContaining({
        type: "string",
        enum: ["collapse", "expand"],
        default: "collapse",
      }),
    );
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.format.javascriptSemicolons"],
    ).toEqual(
      expect.objectContaining({
        type: ["string", "null"],
        enum: ["ignore", "insert", "remove", null],
        default: null,
      }),
    );
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.format.vbscriptKeywordCase"],
    ).toEqual(
      expect.objectContaining({
        type: ["string", "null"],
        enum: ["preserve", "upper", "lower", "title", null],
        default: null,
      }),
    );
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.format.nestedAspInCssJs"],
    ).toEqual(
      expect.objectContaining({
        type: "string",
        enum: ["skipRegion", "protectAspOnly", "formatAroundAsp"],
        default: "skipRegion",
      }),
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
    expect(manifest.repository?.url).toContain("github.com/yottonoko/classic-web-system-lsp");
    expect(manifest.icon).toBe("assets/icon.png");
    expect(fs.existsSync(manifest.icon ?? "")).toBe(true);
    expect(manifest.galleryBanner?.color).toBeTruthy();
    expect(manifest.capabilities?.untrustedWorkspaces?.supported).toBe(true);
    const extensionSource = readExtensionSource();
    expect(extensionSource).toContain('registerCommand("aspLsp.restartServer"');
    expect(extensionSource).toContain(
      "errorHandler: progressController.createLanguageClientErrorHandler()",
    );
    expect(extensionSource).toContain("CloseAction.Restart");
    expect(extensionSource).toContain("ErrorAction.Continue");
    expect(extensionSource).toContain("restartPromise");
    expect(extensionSource).toContain("isDeactivating");
    expect(extensionSource).toContain("isManualRestarting");
    expect(extensionSource).toContain('registerCommand("aspLsp.showReferences"');
    expect(commands).not.toContain("aspLsp.showCurrentFileGraph");
    expect(commands).not.toContain("aspLsp.showFolderGraph");
    expect(commands).not.toContain("aspLsp.showWorkspaceGraph");
    expect(commands).toContain("aspLsp.showCurrentFileNavigationGraph");
    expect(commands).toContain("aspLsp.showFolderNavigationGraph");
    expect(commands).toContain("aspLsp.showWorkspaceNavigationGraph");
    expect(commands).toContain("aspLsp.showWorkspaceGlobFiles");
    expect(commands).not.toContain("aspLsp.openSettings");
    expect(commands).not.toContain("aspLsp.openAnalysisExcelExport");
    expect(commands).toContain("aspLsp.exportCurrentFileAnalysisExcel");
    expect(commands).not.toContain("aspLsp.exportFolderAnalysisExcel");
    expect(commands).not.toContain("aspLsp.exportWorkspaceAnalysisExcel");
    expect(commands).toContain("aspLsp.showCurrentFileFlowchart");
    expect(commands).toContain("aspLsp.exportCurrentFileFlowchart");
    expect(
      manifest.contributes?.commands?.find(
        (command) => command.command === "aspLsp.showCurrentFileNavigationGraph",
      ),
    ).toEqual(expect.objectContaining({ icon: "$(graph)" }));
    expect(manifest.contributes?.menus?.["editor/title"]).toContainEqual(
      expect.objectContaining({
        command: "aspLsp.showCurrentFileNavigationGraph",
        when: "editorLangId == classic-asp",
        group: "navigation",
      }),
    );
    expect(manifest.contributes?.menus?.["editor/title"]).toContainEqual(
      expect.objectContaining({
        command: "aspLsp.exportCurrentFileAnalysisExcel",
        when: "editorLangId == classic-asp",
        group: "navigation",
      }),
    );
    expect(manifest.contributes?.menus?.["editor/title"]).toContainEqual(
      expect.objectContaining({
        command: "aspLsp.showCurrentFileFlowchart",
        when: "editorLangId == classic-asp",
        group: "navigation",
      }),
    );
    expect(manifest.contributes?.menus?.["explorer/context"]).toContainEqual(
      expect.objectContaining({
        command: "aspLsp.showFolderNavigationGraph",
        when: "explorerResourceIsFolder",
        group: "navigation",
      }),
    );
    expect(manifest.contributes?.menus?.["explorer/context"]).toContainEqual(
      expect.objectContaining({
        command: "aspLsp.showCurrentFileNavigationGraph",
        when: "resourceExtname =~ /\\.(asp|asa|inc)$/i",
        group: "navigation",
      }),
    );
    expect(manifest.contributes?.menus?.["explorer/context"]).not.toContainEqual(
      expect.objectContaining({
        command: "aspLsp.exportFolderAnalysisExcel",
      }),
    );
    expect(manifest.contributes?.menus?.["explorer/context"]).toContainEqual(
      expect.objectContaining({
        command: "aspLsp.exportCurrentFileAnalysisExcel",
        when: "resourceExtname =~ /\\.(asp|asa|inc)$/i",
        group: "navigation",
      }),
    );
    expect(manifest.contributes?.menus?.["explorer/context"]).toContainEqual(
      expect.objectContaining({
        command: "aspLsp.showCurrentFileFlowchart",
        when: "resourceExtname =~ /\\.(asp|asa|inc)$/i",
        group: "navigation",
      }),
    );
    expect(nls["command.showCurrentFileGraph.title"]).toBeUndefined();
    expect(nls["command.showFolderGraph.title"]).toBeUndefined();
    expect(nls["command.showWorkspaceGraph.title"]).toBeUndefined();
    expect(nls["command.showCurrentFileNavigationGraph.title"]).toBeTruthy();
    expect(nls["command.showFolderNavigationGraph.title"]).toBeTruthy();
    expect(nls["command.showWorkspaceNavigationGraph.title"]).toBeTruthy();
    expect(nls["command.showWorkspaceGlobFiles.title"]).toBeTruthy();
    expect(nls["command.openAnalysisExcelExport.title"]).toBeUndefined();
    expect(nls["command.exportCurrentFileAnalysisExcel.title"]).toBeTruthy();
    expect(nls["command.exportFolderAnalysisExcel.title"]).toBeUndefined();
    expect(nls["command.exportWorkspaceAnalysisExcel.title"]).toBeUndefined();
    expect(nls["command.showCurrentFileFlowchart.title"]).toBeTruthy();
    expect(nls["command.exportCurrentFileFlowchart.title"]).toBeTruthy();
    expect(nlsJa["command.showCurrentFileGraph.title"]).toBeUndefined();
    expect(nlsJa["command.showFolderGraph.title"]).toBeUndefined();
    expect(nlsJa["command.showWorkspaceGraph.title"]).toBeUndefined();
    expect(nlsJa["command.showWorkspaceGlobFiles.title"]).toBeTruthy();
    expect(nlsJa["command.openAnalysisExcelExport.title"]).toBeUndefined();
    expect(nlsJa["command.exportCurrentFileAnalysisExcel.title"]).toBeTruthy();
    expect(nlsJa["command.exportFolderAnalysisExcel.title"]).toBeUndefined();
    expect(nlsJa["command.exportWorkspaceAnalysisExcel.title"]).toBeUndefined();
    expect(nlsJa["command.showCurrentFileFlowchart.title"]).toBeTruthy();
    expect(nlsJa["command.exportCurrentFileFlowchart.title"]).toBeTruthy();
    expect(manifest.activationEvents).toBeUndefined();
    expect(extensionSource).not.toContain('registerCommand("aspLsp.showCurrentFileGraph"');
    expect(extensionSource).not.toContain('registerCommand("aspLsp.showFolderGraph"');
    expect(extensionSource).not.toContain('registerCommand("aspLsp.showWorkspaceGraph"');
    expect(extensionSource).toContain('"aspLsp.exportCurrentFileAnalysisExcel"');
    expect(extensionSource).not.toContain('"aspLsp.exportFolderAnalysisExcel"');
    expect(extensionSource).not.toContain('"aspLsp.exportWorkspaceAnalysisExcel"');
    expect(extensionSource).not.toContain("createAnalysisExcelSheets");
    expect(extensionSource).toContain('"aspLsp.server.exportAnalysisExcel"');
    expect(extensionSource).toContain("targetPath: target.fsPath");
    expect(extensionSource).toContain("relatedIncludeTreeAnalysisSetting");
    expect(configuration["aspLsp.excel.locale"]).toBeTruthy();
    expect(languageServerSource).toContain("func (s *Server) exportAnalysisExcel");
    expect(analysisExcelSource).toContain("func WriteXLSX");
    expect(extensionSource).toContain("includeRelatedIncludeTreesForUnresolved");
    expect(extensionSource).toContain("excelSkipTypeInferenceSetting");
    expect(extensionSource).toContain("skipTypeInference");
    expect(extensionSource).not.toContain("graphAnalysisLimitSettings");
    expect(extensionSource).not.toContain("writeXlsxFile");
    expect(extensionSource).not.toContain(".toBuffer()");
    expect(extensionSource).not.toContain("vscode.workspace.fs.writeFile(target, workbook)");
    expect(extensionSource).not.toContain(".toFile(target.fsPath)");
    expect(extensionSource).toContain('registerCommand("aspLsp.showCurrentFileFlowchart"');
    expect(extensionSource).toContain('registerCommand("aspLsp.exportCurrentFileFlowchart"');
    expect(extensionSource).toContain('webviewViewColumn("flowchart.openLocation")');
    expect(extensionSource).toContain('webviewViewColumn("navigationGraph.openLocation")');
    expect(extensionSource).toContain('webviewViewColumn("workspaceFiles.openLocation")');
    expect(extensionSource).toContain("cancellable: true");
    expect(extensionSource).not.toContain("isGraphCancellationError");
    expect(extensionSource).toContain("vscode.ViewColumn.Active");
    expect(extensionSource).toContain("vscode.ViewColumn.Beside");
    expect(extensionSource).not.toContain('"aspLsp.server.buildGraph"');
    expect(extensionSource).toContain('"aspLsp.server.buildFlowchart"');
    expect(extensionSource).toContain('"editor.action.showReferences"');
    expect(extensionSource).toContain('registerCommand("aspLsp.toggleLineComment"');
    expect(extensionSource).toContain("onDidChangeTextEditorSelection");
    expect(extensionSource).toContain("selectionChange.kind !== undefined");
    expect(keybindings).toContainEqual(
      expect.objectContaining({
        command: "aspLsp.toggleLineComment",
        key: "ctrl+/",
        mac: "cmd+/",
        when: "editorTextFocus && editorLangId == classic-asp",
      }),
    );
    const contributedCommands = manifest.contributes?.commands ?? [];
    expect(contributedCommands).toContainEqual(
      expect.objectContaining({ command: "aspLsp.toggleLineComment" }),
    );
    expect(manifest.contributes?.menus?.["editor/context"]).toContainEqual(
      expect.objectContaining({ command: "aspLsp.toggleLineComment" }),
    );
    const languageConfiguration = JSON.parse(
      fs.readFileSync("language-configuration.json", "utf8"),
    ) as {
      comments?: { blockComment?: string[]; lineComment?: string };
      brackets?: string[][];
      colorizedBracketPairs?: string[][];
      autoClosingPairs?: Array<{ open?: string; close?: string }>;
      surroundingPairs?: Array<{ open?: string; close?: string }>;
    };
    expect(languageConfiguration.comments).toBeUndefined();
    expect(languageConfiguration.brackets).not.toContainEqual(["<", ">"]);
    expect(languageConfiguration.brackets).toContainEqual(["(", ")"]);
    expect(languageConfiguration.brackets).toContainEqual(["[", "]"]);
    expect(languageConfiguration.colorizedBracketPairs).toContainEqual(["(", ")"]);
    expect(languageConfiguration.colorizedBracketPairs).toContainEqual(["[", "]"]);
    expect(languageConfiguration.autoClosingPairs).not.toContainEqual({
      open: "<",
      close: ">",
    });
    expect(languageConfiguration.autoClosingPairs).not.toContainEqual({
      open: "'",
      close: "'",
    });
    expect(languageConfiguration.surroundingPairs).toContainEqual({
      open: "'",
      close: "'",
    });
    const vbscriptLanguage = manifest.contributes?.languages?.find(
      (language) => language.id === "vbscript",
    );
    expect(vbscriptLanguage).toBeTruthy();
    expect(vbscriptLanguage?.extensions).toEqual([".vbs"]);
    expect(vbscriptLanguage?.configuration).toBe("./vbscript-language-configuration.json");
    const vbscriptLanguageConfiguration = JSON.parse(
      fs.readFileSync("vbscript-language-configuration.json", "utf8"),
    ) as {
      brackets?: string[][];
      colorizedBracketPairs?: string[][];
      autoClosingPairs?: Array<{ open?: string; close?: string }>;
    };
    expect(vbscriptLanguageConfiguration.brackets).toContainEqual(["(", ")"]);
    expect(vbscriptLanguageConfiguration.brackets).toContainEqual(["[", "]"]);
    expect(vbscriptLanguageConfiguration.colorizedBracketPairs).toContainEqual(["(", ")"]);
    expect(vbscriptLanguageConfiguration.colorizedBracketPairs).toContainEqual(["[", "]"]);
    expect(vbscriptLanguageConfiguration.autoClosingPairs).toContainEqual({
      open: "(",
      close: ")",
    });
    expect(vbscriptLanguageConfiguration.autoClosingPairs).not.toContainEqual({
      open: "'",
      close: "'",
    });
    expect(extensionSource).toContain("autoCloseHtmlTag");
    expect(extensionSource).toContain("couldTriggerHtmlTagCompleteBefore");
    expect(extensionSource).toContain(
      "editor.selection = new vscode.Selection(position, position)",
    );
    expect(extensionSource).toContain("textDocument/onTypeFormatting");
    expect(extensionSource).toContain("await waitForLanguageClientTextDocumentSync()");
    expect(extensionSource).toContain("document.version !== documentVersion");
    expect(extensionSource).toContain("setTimeout(resolve, 0)");
    expect(extensionSource).toContain("autoCloseAspBlock");
    const autoCloseHtmlTagSource = extensionSource.slice(
      extensionSource.indexOf("async function autoCloseHtmlTag"),
      extensionSource.indexOf("function waitForLanguageClientTextDocumentSync"),
    );
    expect(autoCloseHtmlTagSource).toContain("!event.contentChanges[0].range.isEmpty");
    const autoCloseAspBlockSource = extensionSource.slice(
      extensionSource.indexOf("async function autoCloseAspBlock"),
      extensionSource.indexOf("function couldTriggerHtmlTagCompleteBefore"),
    );
    expect(autoCloseAspBlockSource).toContain("!event.contentChanges[0].range.isEmpty");
    expect(autoCloseAspBlockSource).toContain(
      "const applied = await vscode.workspace.applyEdit(workspaceEdit)",
    );
    expect(extensionSource).toContain("vscode.window.activeTextEditor");
    expect(extensionSource).toContain("vscode.window.activeTextEditor === editor");
    expect(extensionSource).not.toContain("vscode.window.visibleTextEditors.find");
    expect(autoCloseAspBlockSource).toContain(
      "editor.selection = new vscode.Selection(position, position)",
    );
    expect(extensionSource).not.toContain("autoCloseApostrophe");
    expect(extensionSource).not.toContain("pendingApostropheAutoCloseEdits");
    expect(extensionSource).not.toContain("consumePendingApostropheAutoClose");
    expect(extensionSource).not.toContain('ch: "\'"');
    expect(extensionSource).toContain("%>");
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
        state?: {
          patterns?: Array<{ match?: string; name?: string }>;
        };
        step?: {
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
    expect(durationScope("durationMs=100000")).toBe("constant.numeric.duration.asp-lsp-output");
    const stateScope = (text: string) =>
      outputGrammar.repository?.state?.patterns?.find(
        (pattern) => pattern.match && new RegExp(pattern.match).test(text),
      )?.name;
    const stepScope = (text: string) =>
      outputGrammar.repository?.step?.patterns?.find(
        (pattern) => pattern.name && pattern.match && new RegExp(pattern.match).test(text),
      )?.name;
    expect(stateScope("requested")).toBe("keyword.control.state.asp-lsp-output");
    expect(stateScope("complete")).toBe("keyword.control.state.asp-lsp-output");
    expect(stepScope("workspaceIndex.complete")).toBe("entity.name.function.step.asp-lsp-output");
    expect(stepScope("vb.references.batch.complete")).toBe(
      "entity.name.function.step.asp-lsp-output",
    );
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
    expect(classicAspGrammar?.embeddedLanguages?.["source.css.embedded.html"]).toBe("css");
    const classicAspTagInjection = manifest.contributes?.grammars?.find(
      (grammar) => grammar.scopeName === "classic-asp.tag-injection",
    );
    expect(classicAspTagInjection?.embeddedLanguages?.["source.css.embedded.html"]).toBe("css");
  });

  it("contributes a getting started walkthrough for new users", () => {
    const manifest = JSON.parse(fs.readFileSync("package.json", "utf8")) as {
      contributes?: {
        walkthroughs?: Array<{
          id?: string;
          title?: string;
          description?: string;
          steps?: Array<{
            id?: string;
            title?: string;
            description?: string;
            media?: { markdown?: string };
            completionEvents?: string[];
          }>;
        }>;
      };
    };
    const walkthrough = manifest.contributes?.walkthroughs?.find(
      (candidate) => candidate.id === "classicAspLsp.gettingStarted",
    );
    expect(walkthrough?.title).toBe("%walkthrough.gettingStarted.title%");
    expect(walkthrough?.description).toBe("%walkthrough.gettingStarted.description%");
    expect(walkthrough?.steps?.map((step) => step.id)).toEqual([
      "openClassicAspFile",
      "readEditorSignals",
      "understandHintsAndCodeLens",
      "useProjectViews",
    ]);
    expect(walkthrough?.steps?.[0]?.completionEvents).toBeUndefined();
    expect(walkthrough?.steps?.[1]?.completionEvents).toContain(
      "onCommand:editor.action.triggerSuggest",
    );
    expect(walkthrough?.steps?.[2]?.completionEvents).toContain(
      "onCommand:workbench.action.openSettings",
    );
    expect(walkthrough?.steps?.[3]?.completionEvents).toContain(
      "onCommand:aspLsp.showWorkspaceGlobFiles",
    );
    for (const step of walkthrough?.steps ?? []) {
      expect(step.title).toMatch(/^%walkthrough\.gettingStarted\./);
      expect(step.description).toMatch(/^%walkthrough\.gettingStarted\./);
      expect(step.media?.markdown).toMatch(/^%walkthrough\.gettingStarted\./);
    }

    const englishHintPage = fs.readFileSync(
      "walkthroughs/getting-started-hints-codelens.md",
      "utf8",
    );
    const japaneseHintPage = fs.readFileSync(
      "walkthroughs/getting-started-hints-codelens.ja.md",
      "utf8",
    );
    expect(englishHintPage).toContain("Inlay hints are small inline labels");
    expect(englishHintPage).toContain("CodeLens items are clickable links");
    expect(englishHintPage).toContain("aspLsp.inlayHints.parameterNames");
    expect(englishHintPage).toContain("aspLsp.codeLens.references");
    expect(japaneseHintPage).toContain("Inlay Hints（インレイヒント）");
    expect(japaneseHintPage).toContain("CodeLens（コードレンズ）");
    expect(japaneseHintPage).toContain("VS Code では");
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
    expect(nls["command.clearCache.title"]).toBe(
      "Classic ASP: Clear Process Cache and Analysis Database",
    );
    expect(nls["command.clearDiskCache.title"]).toBe("Classic ASP: Clear bbolt Analysis Database");
    expect(nls["command.clearProcessCache.title"]).toBe(
      "Classic ASP: Clear Process Analysis Cache",
    );
    expect(nlsJa["command.clearCache.title"]).toBe(
      "Classic ASP: プロセスキャッシュと解析データベースを消去",
    );
    expect(nlsJa["command.clearDiskCache.title"]).toBe("Classic ASP: bbolt 解析データベースを消去");
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
    expect(keywordPattern).toContain("On");
    expect(keywordPattern).toContain("Error");
    expect(keywordPattern).toContain("Resume");
    expect(keywordPattern).toContain("GoTo");
    const aspObjectPattern = patterns.find(
      (pattern) => pattern.name === "support.class.asp",
    )?.match;
    expect(aspObjectPattern).toContain("Err");
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
        {
          begin?: string;
          contentName?: string;
          end?: string;
          patterns?: Array<{ include?: string }>;
        }
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
        (pattern) => pattern.include === "#asp-vbscript",
      ),
    ).toBe(true);
    expect(
      classicAspGrammar.repository?.["asp-directive"]?.patterns?.some(
        (pattern) => pattern.include === "#asp-directive-content",
      ),
    ).toBe(true);
    expect(JSON.stringify(classicAspGrammar.repository?.["asp-directive-content"])).toContain(
      "entity.other.attribute-name.directive.asp",
    );
    expect(JSON.stringify(classicAspGrammar.repository?.["asp-directive-content"])).toContain(
      "constant.numeric.directive.asp",
    );
    expect(classicAspGrammar.repository?.["asp-vbscript"]?.patterns).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ include: "#asp-vbscript-apostrophe-comment" }),
        expect.objectContaining({ include: "source.vbscript" }),
      ]),
    );
    expect(classicAspGrammar.repository?.["asp-vbscript-apostrophe-comment"]?.end).toContain("%>");
    expect(classicAspGrammar.repository?.["asp-vbscript-string"]?.end).toContain("%>");
    expect(classicAspGrammar.repository?.["asp-expression"]?.end).toBe("%>");
    expect(classicAspTagInjection.injectionSelector).toContain("L:text.html.classic-asp meta.tag");
    expect(classicAspTagInjection.injectionSelector).toContain(
      "L:text.html.classic-asp meta.tag string.quoted",
    );
    expect(classicAspTagInjection.patterns).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ include: "#style-attribute-double" }),
        expect.objectContaining({ include: "#style-attribute-single" }),
        expect.objectContaining({ include: "#asp-attribute-double" }),
        expect.objectContaining({ include: "#asp-attribute-single" }),
        expect.objectContaining({ include: "#asp-expression" }),
        expect.objectContaining({ include: "#asp-directive" }),
        expect.objectContaining({ include: "#asp-block" }),
      ]),
    );
    expect(classicAspTagInjection.repository?.["style-attribute-double"]?.contentName).toBe(
      "source.css.embedded.html",
    );
    expect(classicAspTagInjection.repository?.["style-attribute-double"]?.patterns).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ include: "#asp-expression" }),
        expect.objectContaining({ include: "#style-css" }),
      ]),
    );
    expect(classicAspTagInjection.repository?.["style-css"]?.patterns).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          captures: expect.objectContaining({
            "2": expect.objectContaining({ name: "support.type.property-name.css" }),
          }),
        }),
      ]),
    );
    expect(classicAspTagInjection.repository?.["asp-expression"]?.end).toBe("%>");
    expect(classicAspTagInjection.repository?.["asp-block"]?.end).toBe("%>");
    expect(classicAspTagInjection.repository?.["asp-vbscript"]?.patterns).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ include: "#asp-vbscript-apostrophe-comment" }),
        expect.objectContaining({ include: "source.vbscript" }),
      ]),
    );
    for (const selector of ["L:source.css", "L:source.js"]) {
      expect(classicAspGrammar.injections?.[selector]?.patterns, selector).toEqual(
        expect.arrayContaining([
          expect.objectContaining({ include: "#asp-expression" }),
          expect.objectContaining({ include: "#asp-block" }),
        ]),
      );
    }
    expect(classicAspGrammar.injections?.["L:source.css"]?.patterns).toContainEqual({
      include: "#css-property-at-rule",
    });
    expect(classicAspGrammar.injections?.["L:source.js"]?.patterns).not.toContainEqual({
      include: "#css-property-at-rule",
    });
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

  it("keeps ASP islands inside CSS and JavaScript comments from capturing following scopes", async () => {
    const grammar = await loadClassicAspTextMateGrammar();
    const cases = [
      {
        source: `<style>
/* <% 'css comment %> */
.next { color: red; }
</style>`,
        line: 2,
        needle: ".next",
        expectedScope: "source.css",
      },
      {
        source: `<script>
// <% 'js comment %>
const next = 1;
</script>`,
        line: 2,
        needle: "const",
        expectedScope: "source.js",
      },
      {
        source: `<script>
/* <% 'js comment %> */
const next = 1;
</script>`,
        line: 2,
        needle: "const",
        expectedScope: "source.js",
      },
    ];

    for (const testCase of cases) {
      const lines = testCase.source.split("\n");
      const token = tokenAtText(grammar, lines, testCase.line, testCase.needle);
      expect(token?.scopes, testCase.source).toContain(testCase.expectedScope);
      expect(token?.scopes.some((scope) => scope.includes("source.vbscript.embedded.asp"))).toBe(
        false,
      );
    }
  });

  it("keeps tab-indented VBScript comments fully comment-scoped", async () => {
    const grammar = await loadClassicAspTextMateGrammar();
    const lines = [
      "<%",
      "\t'\tplain comment\tDim value",
      "\t'''\t<summary>documentation</summary>",
      "\t'\t@param value As String",
      "\tREM\tplain comment\tDim value",
      "%>",
      "<%\t'\tinline comment %>",
      "<%\tREM\tinline comment %>",
    ];

    for (const { line, needle, scope } of [
      { line: 1, needle: "\t'", scope: "comment.line.apostrophe.vbscript" },
      { line: 2, needle: "\t'''", scope: "comment.line.documentation.vbscript" },
      { line: 3, needle: "\t'\t@", scope: "comment.line.annotation.vbscript" },
      { line: 4, needle: "\tREM", scope: "comment.line.rem.vbscript" },
      { line: 6, needle: "\t'", scope: "comment.line.apostrophe.vbscript" },
      { line: 7, needle: "\tREM", scope: "comment.line.rem.vbscript" },
    ]) {
      expect(tokenAtText(grammar, lines, line, needle)?.scopes, `${line}:${needle}`).toContain(
        scope,
      );
    }

    for (const { line, needle, scope } of [
      { line: 1, needle: "Dim", scope: "comment.line.apostrophe.vbscript" },
      { line: 4, needle: "Dim", scope: "comment.line.rem.vbscript" },
    ]) {
      expect(tokenAtText(grammar, lines, line, needle)?.scopes, `${line}:${needle}`).toContain(
        scope,
      );
      expect(tokenAtText(grammar, lines, line, needle)?.scopes).not.toContain(
        "keyword.control.vbscript",
      );
    }
  });

  it("keeps leading tabs in standalone VBScript comments comment-scoped", async () => {
    const grammar = await loadVBScriptTextMateGrammar();
    const lines = [
      "\t'\tplain comment",
      "\t'''\t<summary>documentation</summary>",
      "\t'\t@param value As String",
      "\tREM\tplain comment",
    ];

    for (const { line, needle, scope } of [
      { line: 0, needle: "\t'", scope: "comment.line.apostrophe.vbscript" },
      { line: 1, needle: "\t'''", scope: "comment.line.documentation.vbscript" },
      { line: 2, needle: "\t'\t@", scope: "comment.line.annotation.vbscript" },
      { line: 3, needle: "\tREM", scope: "comment.line.rem.vbscript" },
    ]) {
      expect(tokenAtText(grammar, lines, line, needle)?.scopes, `${line}:${needle}`).toContain(
        scope,
      );
    }
  });

  it("colors ASP but not inline CSS inside HTML comments", async () => {
    const grammar = await loadClassicAspTextMateGrammar();
    const lines = [
      '<!-- <div style="color: red"><%= title %></div> -->',
      '<!-- <div style="--brand-color: red; -webkit-user-select: none; color: var(--brand-color)">x</div> -->',
      '<!-- <div style=" -->/* --brand-color: red; -webkit-user-select: none */<!-- ">x</div> -->',
      '<div style="color: blue"><%= activeTitle %></div>',
      '<div style="--brand-color: red; -webkit-user-select: none; color: var(--brand-color)">x</div>',
    ];

    for (const needle of ["color", "red"]) {
      const token = tokenAtText(grammar, lines, 0, needle);
      expect(token?.scopes, needle).toContain("comment.block.html");
      expect(
        token?.scopes.some((scope) => scope === "source.css.embedded.html"),
        needle,
      ).toBe(false);
    }

    for (const { line, needles, commentScope } of [
      {
        line: 1,
        needles: ["--brand-color", "-webkit-user-select", "var(--brand-color)", "</div>"],
        commentScope: "comment.block.html",
      },
      {
        line: 2,
        needles: ["--brand-color", "-webkit-user-select"],
        commentScope: "comment.block.css",
      },
    ]) {
      for (const needle of needles) {
        const token = tokenAtText(grammar, lines, line, needle);
        expect(token?.scopes, needle).toContain(commentScope);
        expect(token?.scopes, needle).not.toContain("support.type.property-name.css");
      }
    }

    for (const needle of ["<%=", "title", "%>"]) {
      const token = tokenAtText(grammar, lines, 0, needle);
      expect(token?.scopes, needle).toContain("comment.block.html");
      expect(
        token?.scopes.some((scope) => scope.includes("source.vbscript.embedded.asp")),
        needle,
      ).toBe(true);
    }

    expect(tokenAtText(grammar, lines, 3, "color")?.scopes).toContain("source.css.embedded.html");
    expect(
      tokenAtText(grammar, lines, 3, "activeTitle")?.scopes.some((scope) =>
        scope.includes("source.vbscript.embedded.asp"),
      ),
    ).toBe(true);
    for (const needle of ["--brand-color", "-webkit-user-select", "color"]) {
      expect(tokenAtText(grammar, lines, 4, needle)?.scopes, needle).toContain(
        "support.type.property-name.css",
      );
    }
  });

  it("tokenizes root script tags between ASP procedure blocks as JavaScript", async () => {
    const grammar = await loadClassicAspTextMateGrammar();
    const source = `<% Sub A() %>
<script>
const a = 10;
console.log(a);
</script>
<% End Sub %>`;
    const lines = source.split("\n");

    for (const testCase of [
      { line: 2, needle: "const" },
      { line: 3, needle: "console" },
    ]) {
      const token = tokenAtText(grammar, lines, testCase.line, testCase.needle);
      expect(token?.scopes, source).toContain("source.js");
      expect(token?.scopes.some((scope) => scope.includes("source.vbscript.embedded.asp"))).toBe(
        false,
      );
    }
  });

  it("tokenizes ASP islands inside HTML attributes as embedded VBScript", async () => {
    const grammar = await loadClassicAspTextMateGrammar();
    const lines = [
      `<%= Response.Write value %>`,
      `<input value="before <%= Response.Write value %> after" data-id='<% Response.Write itemId %>' <% Response.Write "disabled" %>>`,
    ];

    for (const { line, needle, scope } of [
      { line: 1, needle: "Response.Write value", scope: "source.vbscript.embedded.asp.expression" },
      { line: 1, needle: "Response.Write itemId", scope: "source.vbscript.embedded.asp" },
      { line: 1, needle: 'Response.Write "disabled"', scope: "source.vbscript.embedded.asp" },
    ]) {
      const token = tokenAtText(grammar, lines, line, needle);
      expect(token?.scopes, needle).toContain(scope);
      expect(
        token?.scopes.some(
          (candidate) =>
            candidate.includes("string.quoted.double.html") ||
            candidate.includes("string.quoted.single.html"),
        ),
        needle,
      ).toBe(false);
    }

    const rootVBScopes =
      tokenAtText(grammar, lines, 0, "Response.Write")?.scopes.filter((scope) =>
        scope.includes("vbscript"),
      ) ?? [];
    const attributeVBScopes =
      tokenAtText(grammar, lines, 1, "Response.Write")?.scopes.filter((scope) =>
        scope.includes("vbscript"),
      ) ?? [];
    expect(attributeVBScopes).toEqual(rootVBScopes);
    expect(tokenAtText(grammar, lines, 1, "before")?.scopes).toContain("string.quoted.double.html");
    expect(tokenAtText(grammar, lines, 1, "after")?.scopes).toContain("string.quoted.double.html");
  });

  it("tokenizes ASP directives with directive-specific scopes", async () => {
    const grammar = await loadClassicAspTextMateGrammar();
    const source = `<%@ Language="VBScript" CodePage=65001 %>`;
    const lines = [source];

    expect(tokenAtText(grammar, lines, 0, "<%@")?.scopes).toContain(
      "punctuation.section.embedded.begin.asp",
    );
    expect(tokenAtText(grammar, lines, 0, "Language")?.scopes).toContain(
      "entity.other.attribute-name.directive.asp",
    );
    expect(tokenAtText(grammar, lines, 0, "VBScript")?.scopes).toContain(
      "string.quoted.double.directive.asp",
    );
    expect(tokenAtText(grammar, lines, 0, "CodePage")?.scopes).toContain(
      "entity.other.attribute-name.directive.asp",
    );
    expect(tokenAtText(grammar, lines, 0, "65001")?.scopes).toContain(
      "constant.numeric.directive.asp",
    );
    expect(tokenAtText(grammar, lines, 0, "%>")?.scopes).toContain(
      "punctuation.section.embedded.end.asp",
    );
  });

  it("tokenizes dotted output lifecycle states with their configured colors", async () => {
    const grammar = await loadAspLspOutputTextMateGrammar();
    const line = "[asp-lsp] workspaceIndex.cancelled durationMs=100000";
    expect(tokenAtText(grammar, [line], 0, "workspaceIndex")?.scopes).toContain(
      "entity.name.function.step.asp-lsp-output",
    );
    expect(tokenAtText(grammar, [line], 0, "cancelled")?.scopes).toContain(
      "keyword.control.state.asp-lsp-output",
    );
    expect(tokenAtText(grammar, [line], 0, "100000")?.scopes).toContain(
      "constant.numeric.duration.asp-lsp-output",
    );
  });

  it("tokenizes quoted ASP islands in embedded strings and style attributes as ASP", async () => {
    const grammar = await loadClassicAspTextMateGrammar();
    const source = [
      '<div style="color: <%= "styleColor" %>; background: #fff" title="<%= "titleText" %>"></div>',
      "<style>",
      '.banner::before { content: "<%= "cssDoubleText" %>"; }',
      ".banner::after { content: '<% 'cssSingleText %>'; }",
      "</style>",
      "<script>",
      'const jsDouble = "<%= "jsDoubleText" %>";',
      "const jsSingle = '<% 'jsSingleText %>';",
      "const jsTemplate = `<% `jsTemplateText` %>`;",
      "const afterTemplate = 1;",
      "</script>",
    ].join("\n");
    const lines = source.split("\n");

    const styleProperty = tokenAtText(grammar, lines, 0, "color");
    expect(styleProperty?.scopes).toContain("source.css.embedded.html");
    expect(styleProperty?.scopes).toContain("support.type.property-name.css");
    expect(styleProperty?.scopes.some((scope) => scope.includes("string.quoted.double.html"))).toBe(
      false,
    );
    const closingTagStart = tokenAtText(grammar, lines, 0, "</div>");
    expect(closingTagStart?.scopes).toContain("punctuation.definition.tag.begin.html");
    expect(closingTagStart?.scopes).not.toContain("source.css.embedded.html");

    for (const { line, needle } of [
      { line: 0, needle: "styleColor" },
      { line: 0, needle: "titleText" },
      { line: 2, needle: "cssDoubleText" },
      { line: 3, needle: "cssSingleText" },
      { line: 6, needle: "jsDoubleText" },
      { line: 7, needle: "jsSingleText" },
      { line: 8, needle: "jsTemplateText" },
    ]) {
      const token = tokenAtText(grammar, lines, line, needle);
      expect(
        token?.scopes.some((scope) => scope.includes("source.vbscript.embedded.asp")),
        needle,
      ).toBe(true);
      const vbscriptIndex =
        token?.scopes.findIndex((scope) => scope.includes("source.vbscript.embedded.asp")) ?? -1;
      const hostStringIndex =
        token?.scopes.findIndex(
          (scope) =>
            scope.includes("string.quoted.double.html") ||
            scope.includes("string.quoted.double.css") ||
            scope.includes("string.quoted.single.css") ||
            scope.includes("string.quoted.double.js") ||
            scope.includes("string.quoted.single.js") ||
            scope.includes("string.template.js"),
        ) ?? -1;
      expect(vbscriptIndex, needle).toBeGreaterThan(hostStringIndex);
    }

    const afterTemplate = tokenAtText(grammar, lines, 9, "afterTemplate");
    expect(afterTemplate?.scopes).toContain("source.js");
    expect(
      afterTemplate?.scopes.some((scope) => scope.includes("source.vbscript.embedded.asp")),
    ).toBe(false);
  });

  it("tokenizes CSS class selectors in style blocks without property coloring", async () => {
    const grammar = await loadClassicAspTextMateGrammar();
    const source = ["<style>", ".A > .b { color: black; }", "</style>"].join("\n");
    const lines = source.split("\n");

    for (const needle of [".A", ".b"]) {
      const token = tokenAtText(grammar, lines, 1, needle);
      expect(token?.scopes, needle).toContain("source.css");
      expect(token?.scopes, needle).toContain("entity.other.attribute-name.class.css");
      expect(token?.scopes, needle).not.toContain("support.type.property-name.css");
    }

    const property = tokenAtText(grammar, lines, 1, "color");
    expect(property?.scopes).toContain("support.type.property-name.css");
  });

  it("ends CSS @property coloring before following CSS, HTML, JavaScript, and VBScript", async () => {
    const grammar = await loadClassicAspTextMateGrammar();
    const source = [
      "<style>",
      "@property --accent-color {",
      '  syntax: "<color>";',
      "  inherits: false;",
      "  initial-value: <%= accentColor %>;",
      "}",
      "@property --surface-color {",
      '  syntax: "*";',
      "  inherits: true;",
      "  initial-value: #c0ffee;",
      "}",
      ".after { color: red; }",
      "</style>",
      "<main>after</main>",
      "<script>",
      "const afterProperty = true;",
      "</script>",
      "<% Response.Write afterProperty %>",
    ].join("\n");
    const lines = source.split("\n");

    for (const { line, needle } of [
      { line: 1, needle: "@property" },
      { line: 1, needle: "--accent-color" },
      { line: 2, needle: "syntax" },
      { line: 3, needle: "inherits" },
      { line: 4, needle: "initial-value" },
      { line: 6, needle: "@property" },
      { line: 6, needle: "--surface-color" },
      { line: 7, needle: "syntax" },
      { line: 8, needle: "inherits" },
      { line: 9, needle: "initial-value" },
    ]) {
      const token = tokenAtText(grammar, lines, line, needle);
      expect(token?.scopes, needle).toContain("meta.at-rule.property.css");
      expect(
        token?.scopes.filter((scope) => scope === "meta.at-rule.property.css"),
        needle,
      ).toHaveLength(1);
    }

    expect(tokenAtText(grammar, lines, 2, "syntax")?.scopes).toContain(
      "support.type.property-name.css",
    );
    expect(tokenAtText(grammar, lines, 2, '"<color>"')?.scopes).toContain(
      "string.quoted.double.css",
    );
    expect(tokenAtText(grammar, lines, 3, "inherits")?.scopes).toContain(
      "support.type.property-name.css",
    );
    expect(tokenAtText(grammar, lines, 3, "false")?.scopes).toContain(
      "constant.language.boolean.css",
    );
    expect(tokenAtText(grammar, lines, 4, "initial-value")?.scopes).toContain(
      "support.type.property-name.css",
    );

    expect(
      tokenAtText(grammar, lines, 4, "accentColor")?.scopes.some((scope) =>
        scope.includes("source.vbscript.embedded.asp"),
      ),
    ).toBe(true);
    expect(tokenAtText(grammar, lines, 7, '"*"')?.scopes).toContain("string.quoted.double.css");
    expect(tokenAtText(grammar, lines, 8, "true")?.scopes).toContain(
      "constant.language.boolean.css",
    );
    expect(tokenAtText(grammar, lines, 9, "#c0ffee")?.scopes).toContain(
      "constant.other.color.rgb-value.hex.css",
    );

    const followingSelector = tokenAtText(grammar, lines, 11, ".after");
    expect(followingSelector?.scopes).toContain("source.css");
    expect(followingSelector?.scopes).toContain("entity.other.attribute-name.class.css");
    expect(followingSelector?.scopes).not.toContain("meta.at-rule.property.css");
    const followingProperty = tokenAtText(grammar, lines, 11, "color");
    expect(followingProperty?.scopes).toContain("support.type.property-name.css");
    expect(followingProperty?.scopes).not.toContain("meta.at-rule.property.css");

    const followingHTML = tokenAtText(grammar, lines, 13, "main");
    expect(
      followingHTML?.scopes.some(
        (scope) => scope === "meta.tag.html" || scope === "meta.tag.structure.main.start.html",
      ),
    ).toBe(true);
    expect(followingHTML?.scopes).not.toContain("source.css");
    expect(followingHTML?.scopes).not.toContain("meta.at-rule.property.css");
    const followingJavaScript = tokenAtText(grammar, lines, 15, "const");
    expect(followingJavaScript?.scopes).toContain("source.js");
    expect(followingJavaScript?.scopes).not.toContain("meta.at-rule.property.css");
    const followingVBScript = tokenAtText(grammar, lines, 17, "Response.Write");
    expect(
      followingVBScript?.scopes.some((scope) => scope.includes("source.vbscript.embedded.asp")),
    ).toBe(true);
    expect(followingVBScript?.scopes).not.toContain("meta.at-rule.property.css");
  });

  it("recovers an unclosed CSS @property block at the style end tag", async () => {
    const grammar = await loadClassicAspTextMateGrammar();
    const lines = [
      "<style>",
      "@property --unfinished {",
      '  syntax: "*";',
      "</style>",
      "<main>after</main>",
      "<script>",
      "const afterUnfinishedProperty = true;",
      "</script>",
    ];

    expect(
      tokenAtText(grammar, lines, 1, "@property")?.scopes.filter(
        (scope) => scope === "meta.at-rule.property.css",
      ),
    ).toHaveLength(1);
    const followingHTML = tokenAtText(grammar, lines, 4, "main");
    expect(followingHTML?.scopes).not.toContain("source.css");
    expect(followingHTML?.scopes).not.toContain("meta.at-rule.property.css");
    const followingJavaScript = tokenAtText(grammar, lines, 6, "const");
    expect(followingJavaScript?.scopes).toContain("source.js");
    expect(followingJavaScript?.scopes).not.toContain("meta.at-rule.property.css");
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

  it("prefers the Go language server executable when it exists", () => {
    const tempRoot = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-go-resolver-"));
    const extensionRoot = path.join(tempRoot, "apps", "vscode");
    const binRoot = path.join(tempRoot, "bin");
    const executableName = process.platform === "win32" ? "asp-lsp-go.exe" : "asp-lsp-go";
    try {
      fs.mkdirSync(extensionRoot, { recursive: true });
      fs.mkdirSync(binRoot, { recursive: true });
      const executablePath = path.join(binRoot, executableName);
      fs.writeFileSync(executablePath, "");
      fs.chmodSync(executablePath, 0o755);
      const serverPath = getServerExecutablePath({
        asAbsolutePath: (relativePath) => path.join(extensionRoot, relativePath),
      });
      expect(serverPath).toEqual({
        kind: "go",
        command: path.join(extensionRoot, "..", "..", "bin", executableName),
      });
      expect(fs.existsSync(serverPath.kind === "go" ? serverPath.command : "")).toBe(true);
    } finally {
      fs.rmSync(tempRoot, { recursive: true, force: true });
    }
  });

  it("packages a VSIX with the language server entrypoint", async () => {
    const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-vsix-"));
    const vsixPath = path.join(tempDir, "classic-asp-lsp.vsix");
    try {
      execFileSync(
        process.execPath,
        [
          path.join("node_modules", "@vscode", "vsce", "vsce"),
          "package",
          "--no-dependencies",
          "--follow-symlinks",
          "--out",
          vsixPath,
        ],
        { stdio: "pipe" },
      );
      expect(fs.existsSync(vsixPath)).toBe(true);
      const listing = (await readZipEntries(vsixPath)).join("\n");
      expect(listing).toContain("extension/dist/extension.js");
      expect(listing).not.toContain("extension/dist/webview/include-graph.js");
      expect(listing).toContain("extension/dist/webview/flowchart.js");
      expect(listing).toContain("extension/dist/webview/navigation-graph.js");
      expect(listing).toContain("extension/dist/webview/workspace-files.js");
      expect(listing).not.toContain("extension/dist/webview/settings.js");
      expect(listing).toContain("extension/syntaxes/classic-asp-tag-injection.tmLanguage.json");
      expect(listing).toContain("extension/syntaxes/classic-asp.tmLanguage.json");
      expect(listing).toContain("extension/package.nls.json");
      expect(listing).toContain("extension/package.nls.ja.json");
      expect(listing).toContain("extension/assets/icon.png");
      expect(listing).toContain("extension/walkthroughs/getting-started-open-file.md");
      expect(listing).toContain("extension/walkthroughs/getting-started-open-file.ja.md");
      expect(listing).toContain("extension/walkthroughs/getting-started-hints-codelens.md");
      expect(listing).toContain("extension/walkthroughs/getting-started-hints-codelens.ja.md");
      expect(listing).toContain(
        process.platform === "win32"
          ? "extension/server/asp-lsp-go.exe"
          : "extension/server/asp-lsp-go",
      );
      expect(listing).not.toMatch(/extension\/.*\.map\b/);
      expect(listing).not.toMatch(/asp-lsp-core(\.exe)?/);
      const removedRuntimeName = "was" + "m";
      expect(listing).not.toContain(`.${removedRuntimeName}`);
      expect(listing).not.toMatch(new RegExp(`extension/server/.*${removedRuntimeName}`, "i"));
      expect(listing).not.toContain("extension/server/node_modules/");
      expect(listing).not.toContain("extension/node_modules/");
    } finally {
      fs.rmSync(tempDir, { recursive: true, force: true });
    }
  }, 60000);
});

type TextMateGrammar = NonNullable<Awaited<ReturnType<Registry["loadGrammar"]>>>;
type TextMateToken = { startIndex: number; endIndex: number; scopes: string[] };

async function loadClassicAspTextMateGrammar(): Promise<TextMateGrammar> {
  const require = createRequire(path.join(process.cwd(), "package.json"));
  const onigWasm = fs.readFileSync(require.resolve("vscode-oniguruma/release/onig.wasm"));
  const onigBytes = onigWasm.buffer.slice(
    onigWasm.byteOffset,
    onigWasm.byteOffset + onigWasm.byteLength,
  );
  const onigLib = loadWASM(onigBytes).then(() => ({
    createOnigScanner: (sources: string[]) => new OnigScanner(sources),
    createOnigString: (value: string) => new OnigString(value),
  }));
  const rawGrammars = new Map([
    [
      "text.html.classic-asp",
      parseRawGrammar(
        fs.readFileSync("syntaxes/classic-asp.tmLanguage.json", "utf8"),
        "classic-asp.tmLanguage.json",
      ),
    ],
    [
      "classic-asp.tag-injection",
      parseRawGrammar(
        fs.readFileSync("syntaxes/classic-asp-tag-injection.tmLanguage.json", "utf8"),
        "classic-asp-tag-injection.tmLanguage.json",
      ),
    ],
    [
      "source.vbscript",
      parseRawGrammar(
        fs.readFileSync("syntaxes/vbscript.tmLanguage.json", "utf8"),
        "vbscript.tmLanguage.json",
      ),
    ],
    [
      "text.html.basic",
      loadHostGrammar("ASP_LSP_TEST_HTML_GRAMMAR", minimalHtmlGrammar(), "html.json"),
    ],
    ["source.css", loadHostGrammar("ASP_LSP_TEST_CSS_GRAMMAR", minimalCssGrammar(), "css.json")],
    [
      "source.js",
      loadHostGrammar(
        "ASP_LSP_TEST_JAVASCRIPT_GRAMMAR",
        minimalJavaScriptGrammar(),
        "javascript.json",
      ),
    ],
  ]);
  const registry = new Registry({
    onigLib,
    loadGrammar: async (scopeName) => rawGrammars.get(scopeName) ?? null,
    getInjections: (scopeName) =>
      scopeName === "text.html.classic-asp" ? ["classic-asp.tag-injection"] : [],
  });
  const grammar = await registry.loadGrammar("text.html.classic-asp");
  if (!grammar) {
    throw new Error("Failed to load Classic ASP TextMate grammar.");
  }
  return grammar;
}

async function loadVBScriptTextMateGrammar(): Promise<TextMateGrammar> {
  const require = createRequire(path.join(process.cwd(), "package.json"));
  const onigWasm = fs.readFileSync(require.resolve("vscode-oniguruma/release/onig.wasm"));
  const onigBytes = onigWasm.buffer.slice(
    onigWasm.byteOffset,
    onigWasm.byteOffset + onigWasm.byteLength,
  );
  const onigLib = loadWASM(onigBytes).then(() => ({
    createOnigScanner: (sources: string[]) => new OnigScanner(sources),
    createOnigString: (value: string) => new OnigString(value),
  }));
  const rawGrammar = parseRawGrammar(
    fs.readFileSync("syntaxes/vbscript.tmLanguage.json", "utf8"),
    "vbscript.tmLanguage.json",
  );
  const registry = new Registry({
    onigLib,
    loadGrammar: async (scopeName) => (scopeName === "source.vbscript" ? rawGrammar : null),
  });
  const grammar = await registry.loadGrammar("source.vbscript");
  if (!grammar) {
    throw new Error("Failed to load VBScript TextMate grammar.");
  }
  return grammar;
}

async function loadAspLspOutputTextMateGrammar(): Promise<TextMateGrammar> {
  const require = createRequire(path.join(process.cwd(), "package.json"));
  const onigWasm = fs.readFileSync(require.resolve("vscode-oniguruma/release/onig.wasm"));
  const onigBytes = onigWasm.buffer.slice(
    onigWasm.byteOffset,
    onigWasm.byteOffset + onigWasm.byteLength,
  );
  const onigLib = loadWASM(onigBytes).then(() => ({
    createOnigScanner: (sources: string[]) => new OnigScanner(sources),
    createOnigString: (value: string) => new OnigString(value),
  }));
  const rawGrammar = parseRawGrammar(
    fs.readFileSync("syntaxes/asp-lsp-output.tmLanguage.json", "utf8"),
    "asp-lsp-output.tmLanguage.json",
  );
  const registry = new Registry({
    onigLib,
    loadGrammar: async (scopeName) => (scopeName === "source.asp-lsp-output" ? rawGrammar : null),
  });
  const grammar = await registry.loadGrammar("source.asp-lsp-output");
  if (!grammar) {
    throw new Error("Failed to load ASP LSP output TextMate grammar.");
  }
  return grammar;
}

function loadHostGrammar(environmentName: string, fallback: object, fallbackPath: string) {
  const grammarPath = process.env[environmentName];
  return grammarPath
    ? parseRawGrammar(fs.readFileSync(grammarPath, "utf8"), grammarPath)
    : parseRawGrammar(JSON.stringify(fallback), fallbackPath);
}

function tokenAtText(
  grammar: TextMateGrammar,
  lines: string[],
  lineIndex: number,
  needle: string,
): TextMateToken | undefined {
  let state = INITIAL;
  let tokens: TextMateToken[] = [];
  for (let index = 0; index <= lineIndex; index += 1) {
    const result = grammar.tokenizeLine(lines[index] ?? "", state);
    tokens = result.tokens;
    state = result.ruleStack;
  }
  const needleStart = lines[lineIndex]?.indexOf(needle) ?? -1;
  if (needleStart === -1) {
    throw new Error(`Missing token text: ${needle}`);
  }
  return tokens.find((token) => token.startIndex <= needleStart && token.endIndex > needleStart);
}

function minimalHtmlGrammar() {
  return {
    scopeName: "text.html.basic",
    patterns: [
      { include: "#style" },
      { include: "#script" },
      { include: "#end-tag" },
      { include: "#tag" },
      { match: "[^<]+" },
    ],
    repository: {
      "end-tag": {
        begin: "(</)([A-Za-z][A-Za-z0-9:-]*)\\b",
        beginCaptures: {
          "1": { name: "punctuation.definition.tag.begin.html" },
          "2": { name: "entity.name.tag.html" },
        },
        end: ">",
        endCaptures: {
          "0": { name: "punctuation.definition.tag.end.html" },
        },
        name: "meta.tag.structure.end.html",
      },
      tag: {
        begin: "<[A-Za-z][A-Za-z0-9:-]*\\b",
        end: ">",
        name: "meta.tag.html",
        patterns: [
          {
            begin: '"',
            end: '"',
            name: "string.quoted.double.html",
          },
          {
            begin: "'",
            end: "'",
            name: "string.quoted.single.html",
          },
        ],
      },
      style: {
        begin: "<style\\b[^>]*>",
        end: "</style>",
        contentName: "source.css",
        name: "meta.embedded.block.css.html",
        patterns: [{ include: "source.css" }],
      },
      script: {
        begin: "<script\\b[^>]*>",
        end: "</script>",
        contentName: "source.js",
        name: "meta.embedded.block.javascript.html",
        patterns: [{ include: "source.js" }],
      },
    },
  };
}

function minimalCssGrammar() {
  return {
    scopeName: "source.css",
    name: "source.css",
    patterns: [
      {
        begin: '"',
        end: '"',
        name: "string.quoted.double.css",
      },
      {
        begin: "'",
        end: "'",
        name: "string.quoted.single.css",
      },
      {
        begin: "/\\*",
        end: "\\*/",
        name: "comment.block.css",
        patterns: aspIslandPatterns(),
      },
      { match: "\\.[A-Za-z_][A-Za-z0-9_-]*", name: "entity.other.attribute-name.class.css" },
      { match: "-?[A-Za-z_][A-Za-z0-9_-]*(?=\\s*:)", name: "support.type.property-name.css" },
    ],
    repository: {
      "comment-block": {
        begin: "/\\*",
        end: "\\*/",
        name: "comment.block.css",
        patterns: aspIslandPatterns(),
      },
      "property-values": {
        patterns: [
          { begin: '"', end: '"', name: "string.quoted.double.css" },
          { begin: "'", end: "'", name: "string.quoted.single.css" },
          { include: "#comment-block" },
          { match: "#[0-9A-Fa-f]{3,8}\\b", name: "constant.other.color.rgb-value.hex.css" },
          { match: "\\b(?:true|false)\\b", name: "constant.language.boolean.css" },
        ],
      },
    },
  };
}

function minimalJavaScriptGrammar() {
  return {
    scopeName: "source.js",
    name: "source.js",
    patterns: [
      {
        begin: '"',
        end: '"',
        name: "string.quoted.double.js",
      },
      {
        begin: "'",
        end: "'",
        name: "string.quoted.single.js",
      },
      {
        begin: "`",
        end: "`",
        name: "string.template.js",
      },
      {
        begin: "//",
        end: "$",
        name: "comment.line.double-slash.js",
        patterns: aspIslandPatterns(),
      },
      {
        begin: "/\\*",
        end: "\\*/",
        name: "comment.block.js",
        patterns: aspIslandPatterns(),
      },
      { match: "\\bconst\\b", name: "storage.modifier.js" },
      { match: "[A-Za-z_$][A-Za-z0-9_$]*", name: "variable.other.js" },
    ],
  };
}

function aspIslandPatterns() {
  return [
    { include: "text.html.classic-asp#asp-expression" },
    { include: "text.html.classic-asp#asp-directive" },
    { include: "text.html.classic-asp#asp-block" },
  ];
}
