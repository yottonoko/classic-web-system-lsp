import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { createRequire } from "node:module";
import { execFileSync, spawn, type ChildProcessWithoutNullStreams } from "node:child_process";
import { describe, expect, it } from "vitest";
import { INITIAL, Registry, parseRawGrammar } from "vscode-textmate";
import { OnigScanner, OnigString, loadWASM } from "vscode-oniguruma";
import { getServerModulePath } from "../src/server-path";

interface JsonRpcMessage {
  id?: number;
  method?: string;
  params?: unknown;
  result?: unknown;
}

function readWebviewSources(...files: string[]): string {
  return files.map((file) => fs.readFileSync(file, "utf8")).join("\n");
}

function readGraphWebviewSource(): string {
  return readWebviewSources("src/webview/include-graph.tsx", "src/webview/include-graph-model.ts");
}

function readFlowchartWebviewSource(): string {
  return readWebviewSources("src/webview/flowchart.tsx", "src/webview/flowchart-model.ts");
}

describe("VS Code extension package", () => {
  it("keeps the language server as a runtime dependency", () => {
    const manifest = JSON.parse(fs.readFileSync("package.json", "utf8")) as {
      dependencies?: Record<string, string>;
      devDependencies?: Record<string, string>;
    };
    expect(manifest.dependencies?.["@asp-lsp/language-server"]).toBe("workspace:*");
    expect(manifest.dependencies?.["@tanstack/react-virtual"]).toBe("^3.14.2");
    expect(manifest.devDependencies?.["@asp-lsp/language-server"]).toBeUndefined();
  });

  it("keeps release manifests and server cache version in sync", () => {
    const rootManifest = JSON.parse(fs.readFileSync("../../package.json", "utf8")) as {
      version?: string;
    };
    const coreManifest = JSON.parse(
      fs.readFileSync("../../packages/core/package.json", "utf8"),
    ) as { version?: string };
    const serverManifest = JSON.parse(
      fs.readFileSync("../../packages/language-server/package.json", "utf8"),
    ) as { version?: string };
    const extensionManifest = JSON.parse(fs.readFileSync("package.json", "utf8")) as {
      version?: string;
    };
    const serverSource = fs.readFileSync(
      "../../packages/language-server/src/server/index.ts",
      "utf8",
    );

    expect(coreManifest.version).toBe(rootManifest.version);
    expect(serverManifest.version).toBe(rootManifest.version);
    expect(extensionManifest.version).toBe(rootManifest.version);
    expect(serverSource).toContain(`const languageServerVersion = "${rootManifest.version}";`);
  });

  it("declares a TypeScript-only VSIX packaging script", () => {
    const rootManifest = JSON.parse(fs.readFileSync("../../package.json", "utf8")) as {
      scripts?: Record<string, string>;
    };
    const manifest = JSON.parse(fs.readFileSync("package.json", "utf8")) as {
      scripts?: Record<string, string>;
    };
    const extensionSource = fs.readFileSync("src/extension.ts", "utf8");

    expect(rootManifest.scripts?.["package:vsix"]).toBe(
      "pnpm --filter classic-asp-lsp run package:vsix",
    );
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
    expect(manifest.scripts?.["package:vsix"]).not.toContain(removedBuild);
    expect(extensionSource).not.toContain(`package:vsix:${removedSuffix}`);
    expect(extensionSource).not.toContain(removedAnalysisEnv);
    expect(extensionSource).not.toContain(`aspLsp.${removedAnalysisSetting}`);
  });

  it("passes the configured locale into the graph webview UI", () => {
    const extensionSource = fs.readFileSync("src/extension.ts", "utf8");
    const graphHostSource = fs.readFileSync("src/include-graph-webview.ts", "utf8");
    const graphWebviewSource = readGraphWebviewSource();

    expect(extensionSource).toContain("extensionLocale()");
    expect(graphHostSource).toContain(
      "graphPayloadForWebview(payload, locale, theme, infoPanelPosition)",
    );
    expect(graphHostSource).toContain(
      "settings: { ...payload.settings, theme, infoPanelPosition }",
    );
    expect(graphHostSource).toContain('message.type === "openFlowchart"');
    expect(graphHostSource).toContain("openFlowchart(message.uri, message.range)");
    expect(graphHostSource).toContain('message.type === "openSetting"');
    expect(graphHostSource).toContain('"workbench.action.openSettings"');
    expect(graphHostSource).toContain("__ASP_LSP_GRAPH_TARGET_RANGE__");
    expect(graphHostSource).toContain('<html lang="${locale}">');
    expect(graphHostSource).toContain('graphHostText(locale, "sourceRangeUnavailable")');
    expect(graphWebviewSource).toContain(
      'const graphLocale: GraphLocale = initialGraph?.locale === "ja" ? "ja" : "en"',
    );
    expect(graphHostSource).toContain('type: "graphUpdated"');
    expect(graphHostSource).toContain("postAspGraphWebviewUpdate");
    expect(graphWebviewSource).toContain("isGraphUpdatedMessage");
    expect(graphWebviewSource).toContain("setGraph(message.payload)");
    expect(graphWebviewSource).toContain('"toolbar.updating": "graph 更新中..."');
    expect(graphWebviewSource).toContain('"action.fit": "フィット"');
    expect(graphWebviewSource).toContain('"action.openFlowchart": "フローチャートを開く"');
    expect(graphWebviewSource).toContain('type: "openFlowchart"');
    expect(graphWebviewSource).toContain('type: "openSetting"');
    expect(graphWebviewSource).toContain("aspLsp.graph.maxNodes");
    expect(graphWebviewSource).toContain("__ASP_LSP_GRAPH_TARGET_RANGE__");
    expect(graphWebviewSource).toContain("graphStatsTargetForRange");
    expect(graphWebviewSource).toContain("hasFocusedInitialTargetRef");
    expect(graphWebviewSource).toContain('"legend.heading": "凡例"');
    expect(graphWebviewSource).toContain('"legend.unresolvedNodeFilters": "未解決系"');
    expect(graphWebviewSource).toContain('"legend.visibilityFilters": "非表示系"');
    expect(graphWebviewSource).toContain(
      '"legend.hideUnreferencedGlobalSymbols": "未外部参照を隠す"',
    );
    expect(graphWebviewSource).toContain('"legend.linkFilters": "リンクフィルター"');
    expect(graphWebviewSource).toContain("unresolvedNodeCategorySet");
    expect(graphWebviewSource).toContain('"view.inspector": "情報"');
    expect(graphWebviewSource).toContain('missingInclude: "#ff4db8"');
    expect(graphWebviewSource).toContain('method: "#a6e3a1"');
    expect(graphWebviewSource).toContain('methodFunction: "#7ee787"');
    expect(graphWebviewSource).toContain('methodSub: "#b3f27c"');
    expect(graphWebviewSource).toContain('method: "#047857"');
    expect(graphWebviewSource).toContain('methodFunction: "#15803d"');
    expect(graphWebviewSource).toContain('methodSub: "#4d7c0f"');
    expect(graphWebviewSource).toContain('"label.missingInclude": "存在しない include"');
    expect(graphWebviewSource).toContain("graphRoleLabel(link.role)");
    expect(graphWebviewSource).toContain("includeModeLabel(link.include?.mode)");
    expect(graphWebviewSource).toContain("booleanLabel(link.include.exists)");
    expect(graphWebviewSource).toContain(
      "const canHideUnreferencedGlobalSymbols = hideUnreferencedGlobalSymbols && hasPayloadRoot",
    );
    expect(graphWebviewSource).toContain("retainedGlobalSymbolNodeIds");
    expect(graphWebviewSource).toContain("retainedGlobalNodeIds?.has(node.id) === true");
    expect(graphWebviewSource).toContain("rootNodeIds.has(sourceId)");
    expect(graphWebviewSource).toContain("rootUris.has(node.uri)");
    expect(graphWebviewSource).toContain("hideUnreferencedGlobalSymbols");
    expect(graphWebviewSource).toContain("asp-lsp-graph-inspector-title");
    expect(graphWebviewSource).toContain(
      "tooltipPositionFor(triggerRef.current, tooltipRef.current)",
    );
    expect(graphWebviewSource).toContain('graphText("toolbar.searchNodes")');
  });

  it("keeps flowchart rendering focused on the selected section", () => {
    const flowchartSource = readFlowchartWebviewSource();
    const flowchartStyles = fs.readFileSync("src/webview/flowchart.css", "utf8");
    const flowchartHostSource = fs.readFileSync("src/flowchart-webview.ts", "utf8");
    const virtualListSource = fs.readFileSync("src/webview/virtual-list.tsx", "utf8");

    expect(virtualListSource).toContain('from "@tanstack/react-virtual"');
    expect(virtualListSource).toContain("function VirtualList");
    expect(virtualListSource).toContain("items.length > threshold");
    expect(virtualListSource).toContain("virtualizer.measureElement");
    expect(flowchartSource).toContain(
      "flowchartForSection(payload, selectedSectionId, themePalette)",
    );
    expect(flowchartSource).toContain('from "./virtual-list"');
    expect(flowchartSource).toContain("<VirtualList");
    expect(flowchartSource).toContain(
      "scrollToIndex={activeNodeIndex >= 0 ? activeNodeIndex : undefined}",
    );
    expect(flowchartSource).toContain('const lines = ["flowchart TB"]');
    expect(flowchartSource).toContain("attachSvgNodeHandlers(");
    expect(flowchartSource).toContain("onOpenContextMenu");
    expect(flowchartSource).toContain("setFocusedFlowchartNodeId(node.id)");
    expect(flowchartSource).toContain("focusedFlowchartNodeId ?? activeSearchNode?.id");
    expect(flowchartSource).toContain("onSelectNode(node)");
    expect(flowchartSource).not.toContain("onOpenFlowchart(node);");
    expect(flowchartSource).toContain("flowchartThemePalettes");
    expect(flowchartSource).toContain("darkFlowchartNodeKindStyles");
    expect(flowchartSource).toContain("lightFlowchartNodeKindStyles");
    expect(flowchartSource).toContain("flowExceptionHandling");
    expect(flowchartSource).toContain('exceptionHandling: "Exception handling"');
    expect(flowchartSource).toContain('exceptionHandling: "例外処理"');
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
    expect(flowchartHostSource).toContain('<?xml version="1.0" encoding="UTF-8"?>');
    expect(flowchartHostSource).toContain("initialTargetRange");
    expect(flowchartSource).toContain("__ASP_LSP_FLOWCHART_TARGET_RANGE__");
    expect(flowchartSource).toContain("maxTextSize: flowchartMaxTextSize(payload)");
    expect(flowchartSource).toContain("maxEdges: flowchartMaxEdges(payload)");
    expect(flowchartSource).toContain("const defaultFlowchartMaxTextSize = 2_000_000");
    expect(flowchartSource).toContain("const defaultFlowchartMaxEdges = 100_000");
    expect(flowchartSource).toContain("payload.settings?.maxTextSize");
    expect(flowchartSource).toContain("payload.settings?.maxEdges");
    expect(flowchartSource).toContain("const defaultMaximumFlowchartZoom = 4");
    expect(flowchartSource).toContain("payload.settings?.minZoom");
    expect(flowchartSource).toContain("payload.settings?.maxZoom");
    expect(flowchartSource).toContain("flowchartFitWidthZoom");
    expect(flowchartSource).toContain("fitWidthDescription");
    expect(flowchartSource).toContain("function FlowchartToolbar");
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
    expect(flowchartSource).toContain('exportMenu: "Export"');
    expect(flowchartSource).toContain('title={section?.label ?? text("title")}');
    expect(flowchartSource).toContain("<span>{section.label}</span>");
    expect(flowchartSource).toContain("<span title={node.label}>{node.label}</span>");
    expect(flowchartSource).not.toContain(
      "text-left text-xs font-semibold uppercase tracking-wide text-[#9fb0c5] hover:text-[#f1f5f9]",
    );
    expect(flowchartSource).toContain('scrollbarGutter: "stable"');
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
      "style={scaledFlowchartCanvasStyle(svgSize, zoom, viewportSize)}",
    );
    expect(flowchartSource).toContain(
      "style={flowchartSvgLayerStyle(svgSize, zoom, viewportSize)}",
    );
    expect(flowchartSource).not.toContain("style={scaledFlowchartCanvasStyle(svgSize, zoom)}");
    expect(flowchartSource).toContain("beginCanvasPan");
    expect(flowchartSource).toContain("moveCanvasPan");
    expect(flowchartSource).toContain("cursor-grab");
    expect(flowchartSource).toContain("suppressCanvasClickAfterPan");
    expect(flowchartSource).toContain("scrollFlowchartElementIntoViewport");
    expect(flowchartSource).toContain("flowchartNodeForRange");
    expect(flowchartSource).toContain('type FlowchartSourceActiveKind = "hover"');
    expect(flowchartSource).toContain("flowchartSourceHighlights(");
    expect(flowchartSource).toContain("flowchartPrimarySourceHighlight(sourceHighlights)");
    expect(flowchartSource).toContain("flowchartSourceScrollTarget(sourceHighlights");
    expect(flowchartSource).toContain("sectionSourceScrollSequence");
    expect(flowchartSource).toContain("consumedSectionScrollKeysRef");
    expect(flowchartSource).toContain('previousKind === "hover"');
    expect(flowchartSource).toContain('previousKind === "selection"');
    expect(flowchartSource).toContain("flowchartSourceHighlightsByPriority(highlights)");
    expect(flowchartSource).toContain("flowchartSourceHighlightPriority");
    expect(flowchartSource).toContain('kind: "hover"');
    expect(flowchartSource).toContain('kind: "selection"');
    expect(flowchartSource).toContain('kind: "section"');
    expect(flowchartSource).toContain("flowchartSourceRangesForSection");
    expect(flowchartSource).toContain('section.kind !== "topLevel"');
    expect(flowchartSource).toContain("mergeFlowchartSourceRanges");
    expect(flowchartSource).toContain("nodes={payload.nodes}");
    expect(flowchartSource).toContain("function flowchartNodeForSourceLine");
    expect(flowchartSource).toContain("flowchartNodeForSourceLine(nodes, lineNumber)");
    expect(flowchartSource).toContain("sourceLineNumberFromEvent(event)");
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
    expect(flowchartSource).toContain("const [open, setOpen] = useState(false)");
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
    expect(flowchartSource).toContain('type: "openGraphLocation"');
    expect(flowchartSource).toContain('openGraph: "グラフを開く"');
    expect(flowchartSource).toContain("escapeMermaidEdgeText");
    expect(flowchartSource).toContain('if (node.kind !== "call")');
    expect(flowchartSource).toContain("function flowchartSearchText");
    expect(flowchartSource).toContain("return node.label;");
    expect(flowchartSource).not.toContain('${node.kind} ${node.label} ${section?.label ?? ""}');
    expect(flowchartSource).not.toContain(
      'vscode.postMessage({ type: "openRange", uri: payload.uri, range: node.range })',
    );
  });

  it("keeps graph search responsive and keyboard-accessible", () => {
    const graphWebviewSource = readGraphWebviewSource();

    expect(graphWebviewSource).toContain('from "./virtual-list"');
    expect(graphWebviewSource).toContain("<VirtualList");
    expect(graphWebviewSource).toContain("grid w-full cursor-pointer");
    expect(graphWebviewSource).toContain("onVisibleItemsChange={setVisibleItems}");
    expect(graphWebviewSource).toContain("requestedItems.map(sourceRangeRequestItem)");
    expect(graphWebviewSource).not.toContain("items.map(sourceRangeRequestItem)");
    expect(graphWebviewSource).toContain("startTransition");
    expect(graphWebviewSource).toContain("const [searchInput, setSearchInput] = useState");
    expect(graphWebviewSource).toContain("const searchInputRef = useRef<HTMLInputElement>");
    expect(graphWebviewSource).toContain("const highlight = searchHighlight ?? selectionHighlight");
    expect(graphWebviewSource).toContain(
      "highlightForSearchTargets(searchTargets, filteredGraphData.links, searchQuery.trim())",
    );
    expect(graphWebviewSource).toContain("function isSearchFocusShortcut");
    expect(graphWebviewSource).toContain("function searchNavigationDirection");
    expect(graphWebviewSource).toContain('event.key === "F3"');
    expect(graphWebviewSource).toContain('isPrimaryModifierShortcut(event, "g")');
    expect(graphWebviewSource).toContain('"toolbar.stats": "List"');
    expect(graphWebviewSource).toContain('"toolbar.stats": "一覧"');
  });

  it("keeps graph accordion hints beside their section titles", () => {
    const graphWebviewSource = readGraphWebviewSource();

    expect(graphWebviewSource).toContain('role="button"');
    expect(graphWebviewSource).toContain('className="flex min-w-0 items-center gap-1.5"');
    expect(graphWebviewSource).toContain("onClick={(event) => event.stopPropagation()}");
    expect(graphWebviewSource).toContain("onKeyDown={(event) => event.stopPropagation()}");
    expect(graphWebviewSource).not.toContain("</button>\n        {hint ? (");
  });

  it("keeps graph layout transitions stable across 2D and 3D views", () => {
    const graphWebviewSource = readGraphWebviewSource();

    expect(graphWebviewSource).toContain("function initialGraphNodePosition");
    expect(graphWebviewSource).toContain("forceFitForModeRef");
    expect(graphWebviewSource).toContain("graph2dCoordsFromScreen(");
    expect(graphWebviewSource).toContain("graph3dCoordsFromScreen(");
    expect(graphWebviewSource).toContain("configureGraphForces(");
    expect(graphWebviewSource).toContain("graphNodeChargeStrength");
    expect(graphWebviewSource).toContain("d3VelocityDecay={graphForceVelocityDecay}");
  });

  it("keeps graph node reference totals independent from link filters", () => {
    const graphWebviewSource = readGraphWebviewSource();

    expect(graphWebviewSource).toContain(
      "const referenceCounts = graphReferenceCounts(payload.links)",
    );
    expect(graphWebviewSource).not.toContain("node.referenceCount = referenceCount");
    expect(graphWebviewSource).toContain("node.value = nodeValue(referenceCount)");
  });

  it("uses one graph category for implicit global variables", () => {
    const graphHostSource = fs.readFileSync("src/include-graph-webview.ts", "utf8");
    const graphWebviewSource = readGraphWebviewSource();

    expect(graphHostSource).toContain('"implicitGlobalVariable"');
    expect(graphWebviewSource).toContain('"label.implicitGlobalVariable"');
    expect(graphWebviewSource).toContain('"node.implicitGlobalVariable.description"');
    expect(graphHostSource).not.toContain("implicitLocalVariable");
    expect(graphHostSource).not.toContain("unresolvedGlobalVariable");
    expect(graphWebviewSource).not.toContain("implicitLocalVariable");
    expect(graphWebviewSource).not.toContain("unresolvedGlobalVariable");
  });

  it("contributes commands, task definition and IIS debug settings", () => {
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
        problemMatchers?: Array<{ name: string }>;
        taskDefinitions?: Array<{ type: string }>;
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
    expect(manifest.dependencies?.["react-force-graph-2d"]).toBe("1.29.1");
    expect(manifest.dependencies?.["react-force-graph-3d"]).toBe("1.29.1");
    expect(manifest.dependencies?.["three-spritetext"]).toBe("1.10.0");
    expect(manifest.dependencies?.["write-excel-file"]).toBeUndefined();
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
    expect(nls["command.showProgressDetails.title"]).toBe("Classic ASP: Show Progress Details");
    expect(nlsJa["command.showProgressDetails.title"]).toBe("Classic ASP: 進行状況を表示");
    expect(commands).toContain("aspLsp.reindexWorkspace");
    expect(commands).toContain("aspLsp.clearCache");
    expect(commands).toContain("aspLsp.clearDiskCache");
    expect(commands).toContain("aspLsp.clearProcessCache");
    expect(commands).toContain("aspLsp.openOutput");
    expect(commands).toContain("aspLsp.showProgressDetails");
    expect(commands).toContain("aspLsp.debugIisUrl");
    expect(commands).toContain("aspLsp.debugIisExpressUrl");
    expect(commands).toContain("aspLsp.createLaunchConfig");
    const removedAnalysisSetting = "analysis" + "Backend";
    const removedAnalysisEnv = "ASP_LSP_ANALYSIS_" + "BACKEND";
    expect(configuration[`aspLsp.${removedAnalysisSetting}`]).toBeUndefined();
    const extensionSourceText = fs.readFileSync("src/extension.ts", "utf8");
    expect(extensionSourceText).not.toContain(removedAnalysisEnv);
    expect(extensionSourceText).not.toContain(`aspLsp.${removedAnalysisSetting}`);
    expect(configuration["aspLsp.debug.logFile.enabled"]).toEqual({
      type: "boolean",
      default: false,
      description: "%configuration.debug.logFile.enabled.description%",
    });
    expect(configuration["aspLsp.debug.logFile.path"]).toEqual({
      type: "string",
      default: "",
      description: "%configuration.debug.logFile.path.description%",
    });
    expect(nls["configuration.debug.logFile.enabled.description"]).toBeTruthy();
    expect(nls["configuration.debug.logFile.path.description"]).toBeTruthy();
    expect(nlsJa["configuration.debug.logFile.enabled.description"]).toBeTruthy();
    expect(nlsJa["configuration.debug.logFile.path.description"]).toBeTruthy();
    expect(extensionSourceText).toContain("ASP_LSP_DEFAULT_DEBUG_LOG_FILE");
    expect(extensionSourceText).toContain('const serverStatusNotificationMethod = "aspLsp/status"');
    expect(extensionSourceText).toContain(
      'const graphUpdatedNotificationMethod = "aspLsp/graphUpdated"',
    );
    expect(extensionSourceText).toContain(
      'const cancelProgressTaskServerCommand = "aspLsp.server.cancelProgressTask"',
    );
    expect(extensionSourceText).toContain("handleServerStatusNotification");
    expect(extensionSourceText).toContain("handleGraphUpdatedNotification");
    expect(extensionSourceText).toContain("graphPanelsByCorrelation");
    expect(extensionSourceText).toContain("showProgressDetails");
    expect(extensionSourceText).toContain('statusBarItem.command = "aspLsp.showProgressDetails"');
    expect(extensionSourceText).toContain("status.loading.text");
    expect(extensionSourceText).toContain("status.analyzing.text");
    expect(extensionSourceText).toContain("progressStatusText");
    expect(extensionSourceText).toContain("progressValueText");
    expect(extensionSourceText).toContain("Math.round((progress.current / progress.total) * 100)");
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
    expect(extensionSourceText).toContain("status.progress.graphIndexDocuments");
    expect(extensionSourceText).toContain("status.progress.graphAddUsages");
    expect(extensionSourceText).toContain("status.progress.graphResolveIncludes");
    expect(extensionSourceText).toContain("status.progress.graphFindIncomingIncludes");
    expect(extensionSourceText).toContain("status.progress.graphFilterIncomingIncludes");
    expect(extensionSourceText).toContain("status.progress.workspaceIndexScanFiles");
    expect(extensionSourceText).toContain("status.progress.referencesWorkspace");
    expect(extensionSourceText).toContain("progressStatusBarDetail");
    expect(extensionSourceText).toContain("progressTaskStatusPriority");
    expect(extensionSourceText).toContain('task.label.startsWith("excel.")');
    expect(extensionSourceText).toContain('label: "excel.chooseFile"');
    expect(extensionSourceText).toContain('label: "excel.graph"');
    expect(extensionSourceText).toContain("Generating current file graph");
    expect(extensionSourceText).toContain("Collecting Excel analysis graph");
    expect(extensionSourceText).toContain("Normalizing Excel graph payload");
    expect(extensionSourceText).toContain("Resolving include paths");
    expect(extensionSourceText).toContain("Writing Excel rows");
    expect(extensionSourceText).toContain("Excel 作成中");
    expect(extensionSourceText).toContain("Excel 解析 graph 取得中");
    expect(extensionSourceText).toContain("Excel row 書き込み中");
    expect(extensionSourceText).toContain("flowchart 生成中");
    expect(extensionSourceText).toContain("Classic ASP analysis workbook を作成中");
    expect(extensionSourceText).toContain("graphAnalysisLimitSettings");
    const analysisExcelSource = fs.readFileSync(
      "../../packages/language-server/src/analysis-excel/sheets.ts",
      "utf8",
    );
    const languageServerSource = fs.readFileSync(
      "../../packages/language-server/src/server/index.ts",
      "utf8",
    );
    const graphBuildSource = fs.readFileSync(
      "../../packages/language-server/src/asp-graph/build.ts",
      "utf8",
    );
    const graphSource = `${languageServerSource}\n${graphBuildSource}`;
    expect(analysisExcelSource).toContain("for (const row of rows)");
    expect(analysisExcelSource).toContain("for (const value of values)");
    expect(analysisExcelSource).not.toContain("Math.max(...rows.map");
    expect(graphSource).toContain("appendAspGraphDocuments(");
    expect(languageServerSource).toContain("appendAspGraphRanges(");
    expect(graphSource).not.toContain("documentsForGraph.push(...indexedGraphDocuments)");
    expect(languageServerSource).not.toContain("existing.push(...references)");
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
    ).toEqual(
      expect.objectContaining({
        type: "string",
        enum: ["analyzed", "workspace"],
        default: "analyzed",
      }),
    );
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
      manifest.contributes?.configuration?.properties?.["aspLsp.flowchart.maxTextSize"],
    ).toEqual(
      expect.objectContaining({
        type: "number",
        minimum: 1,
        default: 2000000,
      }),
    );
    expect(nls["configuration.flowchart.maxTextSize.description"]).toBeTruthy();
    expect(nlsJa["configuration.flowchart.maxTextSize.description"]).toBeTruthy();
    expect(manifest.contributes?.configuration?.properties?.["aspLsp.flowchart.maxEdges"]).toEqual(
      expect.objectContaining({
        type: "number",
        minimum: 1,
        default: 100000,
      }),
    );
    expect(nls["configuration.flowchart.maxEdges.description"]).toBeTruthy();
    expect(nlsJa["configuration.flowchart.maxEdges.description"]).toBeTruthy();
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
    const graphDefaults: Record<string, boolean> = {
      showRootNodes: true,
      showFileNodes: true,
      showFunctionNodes: true,
      showSubNodes: true,
      showClassNodes: true,
      showMethodNodes: false,
      showMethodFunctionNodes: false,
      showMethodSubNodes: false,
      showPropertyNodes: false,
      showMemberNodes: false,
      showGlobalVariableNodes: true,
      showGlobalConstantNodes: true,
      showLocalVariableNodes: false,
      showLocalConstantNodes: false,
      showParameterNodes: false,
      showUnresolvedNodes: true,
      hideSingleNodes: true,
      hideUnreferencedGlobalSymbols: true,
      showOutgoingSelectionLinks: true,
      showIncludeLinks: true,
      showDeclareLinks: true,
      showReferenceLinks: true,
      showAssignmentLinks: true,
      showCallLinks: true,
      showUnresolvedLinks: true,
      showMemberLinks: true,
      showIncomingDocumentIncludes: false,
      showIncomingFolderIncludes: false,
      useReverseIncludeIndex: true,
      includeRelatedIncludeTreesForUnresolved: true,
      workerSymbolExtraction: false,
    };
    for (const [name, defaultValue] of Object.entries(graphDefaults)) {
      const setting = `aspLsp.graph.${name}`;
      expect(manifest.contributes?.configuration?.properties?.[setting]).toEqual(
        expect.objectContaining({ type: "boolean", default: defaultValue }),
      );
      expect(nls[`configuration.graph.${name}.description`]).toBeTruthy();
      expect(nlsJa[`configuration.graph.${name}.description`]).toBeTruthy();
    }
    for (const removedName of [
      "showBuiltinSymbols",
      "showConfiguredGlobals",
      "showConfiguredComTypes",
      "showObjectMembers",
      "showFunctionParameters",
      "showLocalVariables",
      "showLocalConstants",
      "showClassFields",
      "showClassMethods",
      "showClassProperties",
      "showClassConstants",
      "showClasses",
      "showFunctions",
      "showSubs",
      "showGlobalVariables",
      "showGlobalConstants",
      "showFiles",
      "showMissingFiles",
      "showDeclarationLinks",
      "showUnresolvedReferences",
    ]) {
      expect(
        manifest.contributes?.configuration?.properties?.[`aspLsp.graph.${removedName}`],
      ).toBeUndefined();
      expect(nls[`configuration.graph.${removedName}.description`]).toBeUndefined();
      expect(nlsJa[`configuration.graph.${removedName}.description`]).toBeUndefined();
    }
    expect(manifest.contributes?.configuration?.properties?.["aspLsp.graph.openLocation"]).toEqual(
      expect.objectContaining({
        type: "string",
        enum: ["active", "beside"],
        default: "active",
      }),
    );
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.graph.includeTreeMaxDocuments"],
    ).toEqual(expect.objectContaining({ type: "number", minimum: 1, default: 256 }));
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.graph.includeTreeMaxTextLength"],
    ).toEqual(expect.objectContaining({ type: "number", minimum: 1, default: 16777216 }));
    expect(nls["configuration.graph.includeTreeMaxDocuments.description"]).toBeTruthy();
    expect(nlsJa["configuration.graph.includeTreeMaxDocuments.description"]).toBeTruthy();
    expect(nls["configuration.graph.includeTreeMaxTextLength.description"]).toBeTruthy();
    expect(nlsJa["configuration.graph.includeTreeMaxTextLength.description"]).toBeTruthy();
    expect(manifest.contributes?.configuration?.properties?.["aspLsp.graph.maxDocuments"]).toEqual(
      expect.objectContaining({ type: "number", minimum: 1, default: 5000 }),
    );
    expect(manifest.contributes?.configuration?.properties?.["aspLsp.graph.maxTextLength"]).toEqual(
      expect.objectContaining({ type: "number", minimum: 1, default: 268435456 }),
    );
    expect(manifest.contributes?.configuration?.properties?.["aspLsp.graph.maxNodes"]).toEqual(
      expect.objectContaining({ type: "number", minimum: 1, default: 5000 }),
    );
    expect(nls["configuration.graph.maxDocuments.description"]).toBeTruthy();
    expect(nlsJa["configuration.graph.maxDocuments.description"]).toBeTruthy();
    expect(nls["configuration.graph.maxTextLength.description"]).toBeTruthy();
    expect(nlsJa["configuration.graph.maxTextLength.description"]).toBeTruthy();
    expect(nls["configuration.graph.maxNodes.description"]).toBeTruthy();
    expect(nlsJa["configuration.graph.maxNodes.description"]).toBeTruthy();
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.graph.initialViewMode"],
    ).toEqual(
      expect.objectContaining({
        type: "string",
        enum: ["2d", "3d"],
        default: "2d",
      }),
    );
    expect(nls["configuration.graph.initialViewMode.description"]).toBeTruthy();
    expect(nlsJa["configuration.graph.initialViewMode.description"]).toBeTruthy();
    expect(nls["configuration.graph.openLocation.description"]).toBeTruthy();
    expect(nlsJa["configuration.graph.openLocation.description"]).toBeTruthy();
    expect(
      manifest.contributes?.configuration?.properties?.[
        "aspLsp.excel.includeRelatedIncludeTreesForUnresolved"
      ],
    ).toEqual(expect.objectContaining({ type: "boolean", default: true }));
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.excel.skipTypeInference"],
    ).toEqual(expect.objectContaining({ type: "boolean", default: false }));
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.excel.includeTreeMaxDocuments"],
    ).toEqual(expect.objectContaining({ type: "number", minimum: 1, default: 1024 }));
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.excel.includeTreeMaxTextLength"],
    ).toEqual(expect.objectContaining({ type: "number", minimum: 1, default: 67108864 }));
    expect(manifest.contributes?.configuration?.properties?.["aspLsp.excel.maxDocuments"]).toEqual(
      expect.objectContaining({ type: "number", minimum: 1, default: 8192 }),
    );
    expect(manifest.contributes?.configuration?.properties?.["aspLsp.excel.maxTextLength"]).toEqual(
      expect.objectContaining({ type: "number", minimum: 1, default: 536870912 }),
    );
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
    expect(nls["configuration.excel.includeTreeMaxDocuments.description"]).toBeTruthy();
    expect(nlsJa["configuration.excel.includeTreeMaxDocuments.description"]).toBeTruthy();
    expect(nls["configuration.excel.includeTreeMaxTextLength.description"]).toBeTruthy();
    expect(nlsJa["configuration.excel.includeTreeMaxTextLength.description"]).toBeTruthy();
    expect(nls["configuration.excel.maxDocuments.description"]).toBeTruthy();
    expect(nlsJa["configuration.excel.maxDocuments.description"]).toBeTruthy();
    expect(nls["configuration.excel.maxTextLength.description"]).toBeTruthy();
    expect(nlsJa["configuration.excel.maxTextLength.description"]).toBeTruthy();
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
      expect.objectContaining({ type: "number", default: 128, minimum: 1 }),
    );
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
        default: ["**/*.{asp,asa,inc}"],
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
    for (const setting of [
      "aspLsp.format.indentSize",
      "aspLsp.format.indentStyle",
      "aspLsp.format.vbscriptBlockIndent",
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
    expect(
      manifest.contributes?.configuration?.properties?.["aspLsp.format.vbscriptBlockIndent"],
    ).toEqual(
      expect.objectContaining({
        type: "string",
        enum: ["alignWithDelimiter", "indentInsideDelimiter"],
        default: "indentInsideDelimiter",
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
    const extensionSource = fs.readFileSync("src/extension.ts", "utf8");
    expect(extensionSource).toContain('registerCommand("aspLsp.restartServer"');
    expect(extensionSource).toContain("errorHandler: createLanguageClientErrorHandler()");
    expect(extensionSource).toContain("CloseAction.Restart");
    expect(extensionSource).toContain("ErrorAction.Continue");
    expect(extensionSource).toContain("restartPromise");
    expect(extensionSource).toContain("isDeactivating");
    expect(extensionSource).toContain("isManualRestarting");
    expect(extensionSource).toContain('registerCommand("aspLsp.showReferences"');
    expect(commands).toContain("aspLsp.showCurrentFileGraph");
    expect(commands).toContain("aspLsp.showFolderGraph");
    expect(commands).toContain("aspLsp.showWorkspaceGraph");
    expect(commands).toContain("aspLsp.exportCurrentFileAnalysisExcel");
    expect(commands).not.toContain("aspLsp.exportFolderAnalysisExcel");
    expect(commands).not.toContain("aspLsp.exportWorkspaceAnalysisExcel");
    expect(commands).toContain("aspLsp.showCurrentFileFlowchart");
    expect(commands).toContain("aspLsp.exportCurrentFileFlowchart");
    expect(
      manifest.contributes?.commands?.find(
        (command) => command.command === "aspLsp.showCurrentFileGraph",
      ),
    ).toEqual(expect.objectContaining({ icon: "$(graph)" }));
    expect(manifest.contributes?.menus?.["editor/title"]).toContainEqual(
      expect.objectContaining({
        command: "aspLsp.showCurrentFileGraph",
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
        command: "aspLsp.showFolderGraph",
        when: "explorerResourceIsFolder",
        group: "navigation",
      }),
    );
    expect(manifest.contributes?.menus?.["explorer/context"]).toContainEqual(
      expect.objectContaining({
        command: "aspLsp.showCurrentFileGraph",
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
    expect(nls["command.showCurrentFileGraph.title"]).toBeTruthy();
    expect(nls["command.showFolderGraph.title"]).toBeTruthy();
    expect(nls["command.showWorkspaceGraph.title"]).toBeTruthy();
    expect(nls["command.exportCurrentFileAnalysisExcel.title"]).toBeTruthy();
    expect(nls["command.exportFolderAnalysisExcel.title"]).toBeUndefined();
    expect(nls["command.exportWorkspaceAnalysisExcel.title"]).toBeUndefined();
    expect(nls["command.showCurrentFileFlowchart.title"]).toBeTruthy();
    expect(nls["command.exportCurrentFileFlowchart.title"]).toBeTruthy();
    expect(nlsJa["command.showCurrentFileGraph.title"]).toBeTruthy();
    expect(nlsJa["command.showFolderGraph.title"]).toBeTruthy();
    expect(nlsJa["command.showWorkspaceGraph.title"]).toBeTruthy();
    expect(nlsJa["command.exportCurrentFileAnalysisExcel.title"]).toBeTruthy();
    expect(nlsJa["command.exportFolderAnalysisExcel.title"]).toBeUndefined();
    expect(nlsJa["command.exportWorkspaceAnalysisExcel.title"]).toBeUndefined();
    expect(nlsJa["command.showCurrentFileFlowchart.title"]).toBeTruthy();
    expect(nlsJa["command.exportCurrentFileFlowchart.title"]).toBeTruthy();
    expect(manifest.activationEvents).toContain("onCommand:aspLsp.showCurrentFileGraph");
    expect(manifest.activationEvents).toContain("onCommand:aspLsp.showFolderGraph");
    expect(manifest.activationEvents).toContain("onCommand:aspLsp.showWorkspaceGraph");
    expect(manifest.activationEvents).toContain("onCommand:aspLsp.exportCurrentFileAnalysisExcel");
    expect(manifest.activationEvents).not.toContain("onCommand:aspLsp.exportFolderAnalysisExcel");
    expect(manifest.activationEvents).not.toContain(
      "onCommand:aspLsp.exportWorkspaceAnalysisExcel",
    );
    expect(manifest.activationEvents).toContain("onCommand:aspLsp.showCurrentFileFlowchart");
    expect(manifest.activationEvents).toContain("onCommand:aspLsp.exportCurrentFileFlowchart");
    expect(extensionSource).toContain('registerCommand("aspLsp.showCurrentFileGraph"');
    expect(extensionSource).toContain('registerCommand("aspLsp.showFolderGraph"');
    expect(extensionSource).toContain('registerCommand("aspLsp.showWorkspaceGraph"');
    expect(extensionSource).toContain('"aspLsp.exportCurrentFileAnalysisExcel"');
    expect(extensionSource).not.toContain('"aspLsp.exportFolderAnalysisExcel"');
    expect(extensionSource).not.toContain('"aspLsp.exportWorkspaceAnalysisExcel"');
    expect(extensionSource).not.toContain("createAnalysisExcelSheets");
    expect(extensionSource).toContain('"aspLsp.server.exportAnalysisExcel"');
    expect(extensionSource).toContain("targetPath: target.fsPath");
    expect(extensionSource).toContain("relatedIncludeTreeAnalysisSetting");
    expect(configuration["aspLsp.excel.locale"]).toBeTruthy();
    expect(languageServerSource).toContain("excelSettings.locale");
    expect(languageServerSource).toContain("createAnalysisExcelSheets");
    expect(extensionSource).toContain("includeRelatedIncludeTreesForUnresolved");
    expect(extensionSource).toContain("forceRelatedIncludeTreeAnalysis");
    expect(extensionSource).toContain("excelSkipTypeInferenceSetting");
    expect(extensionSource).toContain("skipTypeInference");
    expect(extensionSource).toContain("includeAnalysisTypeDetails");
    expect(extensionSource).toContain("graphAnalysisLimitSettings");
    expect(extensionSource).toContain('graphAnalysisLimitSettings("excel")');
    expect(extensionSource).toContain('graphAnalysisLimitSettings("graph")');
    expect(extensionSource).toContain("maxDocuments");
    expect(extensionSource).toContain("maxTextLength");
    expect(extensionSource).toContain("includeTreeMaxDocuments");
    expect(extensionSource).toContain("includeTreeMaxTextLength");
    expect(extensionSource).not.toContain("writeXlsxFile");
    expect(extensionSource).not.toContain(".toBuffer()");
    expect(extensionSource).not.toContain("vscode.workspace.fs.writeFile(target, workbook)");
    expect(extensionSource).not.toContain(".toFile(target.fsPath)");
    expect(extensionSource).toContain('registerCommand("aspLsp.showCurrentFileFlowchart"');
    expect(extensionSource).toContain('registerCommand("aspLsp.exportCurrentFileFlowchart"');
    expect(extensionSource).toContain('get<GraphOpenLocation>("graph.openLocation", "active")');
    expect(extensionSource).toContain("cancellable: true");
    expect(extensionSource).toContain("isGraphCancellationError");
    expect(extensionSource).toContain("vscode.ViewColumn.Active");
    expect(extensionSource).toContain("vscode.ViewColumn.Beside");
    expect(extensionSource).toContain('"aspLsp.server.buildGraph"');
    expect(extensionSource).toContain('"aspLsp.server.buildFlowchart"');
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
    ) as {
      comments?: { blockComment?: string[]; lineComment?: string };
      brackets?: string[][];
      colorizedBracketPairs?: string[][];
      autoClosingPairs?: Array<{ open?: string; close?: string }>;
      surroundingPairs?: Array<{ open?: string; close?: string }>;
    };
    expect(languageConfiguration.comments?.blockComment).toEqual(["<!--", "-->"]);
    expect(languageConfiguration.comments?.lineComment).toBeUndefined();
    expect(languageConfiguration.brackets).not.toContainEqual(["<", ">"]);
    expect(languageConfiguration.brackets).toContainEqual(["(", ")"]);
    expect(languageConfiguration.brackets).toContainEqual(["[", "]"]);
    expect(languageConfiguration.colorizedBracketPairs).toContainEqual(["(", ")"]);
    expect(languageConfiguration.colorizedBracketPairs).toContainEqual(["[", "]"]);
    expect(languageConfiguration.autoClosingPairs).toContainEqual({
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
    expect(vbscriptLanguage?.extensions).toBeUndefined();
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
    const autoCloseAspBlockSource = extensionSource.slice(
      extensionSource.indexOf("async function autoCloseAspBlock"),
      extensionSource.indexOf("function couldTriggerHtmlTagCompleteBefore"),
    );
    expect(autoCloseAspBlockSource).toContain(
      "const applied = await vscode.workspace.applyEdit(workspaceEdit)",
    );
    expect(autoCloseAspBlockSource).toContain("vscode.window.visibleTextEditors.find");
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
    expect(classicAspGrammar?.embeddedLanguages?.["source.css.embedded.html"]).toBe("css");
    const classicAspTagInjection = manifest.contributes?.grammars?.find(
      (grammar) => grammar.scopeName === "classic-asp.tag-injection",
    );
    expect(classicAspTagInjection?.embeddedLanguages?.["source.css.embedded.html"]).toBe("css");
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
            "1": expect.objectContaining({ name: "support.type.property-name.css" }),
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
    const embeddedInjection = Object.entries(classicAspGrammar.injections ?? {}).find(
      ([selector]) => selector.includes("source.css") && selector.includes("source.js"),
    )?.[1];
    expect(embeddedInjection?.patterns).toEqual(
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
    const source = `<input value="<%= Response.Write value %>" <% Response.Write "disabled" %>>`;
    const lines = source.split("\n");

    for (const { needle, scope } of [
      { needle: "Response.Write value", scope: "source.vbscript.embedded.asp.expression" },
      { needle: 'Response.Write "disabled"', scope: "source.vbscript.embedded.asp" },
    ]) {
      const token = tokenAtText(grammar, lines, 0, needle);
      expect(token?.scopes, needle).toContain(scope);
      const vbscriptIndex = token?.scopes.indexOf(scope) ?? -1;
      const stringIndex =
        token?.scopes.findIndex((scope) => scope.includes("string.quoted.double.html")) ?? -1;
      expect(vbscriptIndex).toBeGreaterThan(stringIndex);
    }
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

  it("resolves the packaged language server module path", () => {
    const root = process.cwd();
    const serverModule = getServerModulePath({
      asAbsolutePath: (relativePath) => path.join(root, relativePath),
    });
    expect(serverModule).toBe(path.join(root, "server", "language-server", "dist", "server.js"));
    expect(fs.existsSync(serverModule)).toBe(true);
  });

  it("bundles TypeScript browser libs for JavaScript language features", async () => {
    const serverModule = path.join(process.cwd(), "server", "language-server", "dist", "server.js");
    expect(fs.existsSync(path.join(path.dirname(serverModule), "lib.esnext.d.ts"))).toBe(true);
    expect(fs.existsSync(path.join(path.dirname(serverModule), "lib.dom.d.ts"))).toBe(true);

    const server = new RpcServer(serverModule);
    const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-bundled-js-"));
    try {
      await server.start();
      const uri = `file://${path.join(tempDir, "default.asp")}`;
      await server.request("initialize", {
        processId: process.pid,
        rootUri: `file://${tempDir}`,
        capabilities: {},
      });
      server.notify("workspace/didChangeConfiguration", {
        settings: { aspLsp: { checkJs: true, diagnostics: { debounceMs: 0 } } },
      });
      server.notify("textDocument/didOpen", {
        textDocument: {
          uri,
          languageId: "classic-asp",
          version: 1,
          text: `<script>
document.querySelector("#clientClock");
new Intl.DateTimeFormat("en");
</script>`,
        },
      });
      await server.waitForNotification("textDocument/publishDiagnostics");

      const diagnostics = await server.request("textDocument/diagnostic", {
        textDocument: { uri },
      });
      const serialized = JSON.stringify(diagnostics);
      expect(serialized).not.toContain("Cannot find name 'document'");
      expect(serialized).not.toContain("Cannot find name 'Intl'");

      await server.request("shutdown", null);
      server.notify("exit", undefined);
    } finally {
      server.stop();
      fs.rmSync(tempDir, { recursive: true, force: true });
    }
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
      expect(listing).toContain("extension/dist/webview/include-graph.js");
      expect(listing).toContain("extension/syntaxes/classic-asp-tag-injection.tmLanguage.json");
      expect(listing).toContain("extension/syntaxes/classic-asp.tmLanguage.json");
      expect(listing).toContain("extension/package.nls.json");
      expect(listing).toContain("extension/package.nls.ja.json");
      expect(listing).toContain("extension/assets/icon.png");
      expect(listing).toContain("extension/server/language-server/dist/server.js");
      expect(listing).toContain("extension/server/language-server/dist/js-diagnostics-worker.js");
      expect(listing).toContain("extension/server/language-server/dist/vb-diagnostics-worker.js");
      expect(listing).toContain("extension/server/language-server/dist/vb-references-worker.js");
      expect(listing).toContain("extension/server/language-server/dist/lib.esnext.d.ts");
      expect(listing).toContain("extension/server/language-server/dist/lib.dom.d.ts");
      expect(listing).not.toMatch(/extension\/.*\.map\b/);
      expect(listing).not.toContain("extension/server/language-server/" + "nati" + "ve/");
      expect(listing).not.toMatch(/asp-lsp-core(\.exe)?/);
      const removedRuntimeName = "was" + "m";
      expect(listing).not.toContain(`.${removedRuntimeName}`);
      expect(listing).not.toMatch(
        new RegExp(`extension/server/language-server/.*${removedRuntimeName}`, "i"),
      );
      expect(listing).not.toContain("extension/server/language-server/node_modules/");
      expect(listing).not.toContain("extension/node_modules/");
    } finally {
      fs.rmSync(tempDir, { recursive: true, force: true });
    }
  });
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
    ["text.html.basic", parseRawGrammar(JSON.stringify(minimalHtmlGrammar()), "html.json")],
    ["source.css", parseRawGrammar(JSON.stringify(minimalCssGrammar()), "css.json")],
    ["source.js", parseRawGrammar(JSON.stringify(minimalJavaScriptGrammar()), "javascript.json")],
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

class RpcServer {
  private child: ChildProcessWithoutNullStreams | undefined;
  private nextId = 1;
  private buffer = Buffer.alloc(0);
  private stderr = "";
  private responses = new Map<number, (message: JsonRpcMessage) => void>();
  private notifications = new Map<string, Array<(message: JsonRpcMessage) => void>>();
  private pendingNotifications = new Map<string, JsonRpcMessage[]>();

  constructor(private readonly serverModule: string) {}

  async start(): Promise<void> {
    this.child = spawn(process.execPath, [this.serverModule, "--stdio"], {
      cwd: process.cwd(),
      stdio: ["pipe", "pipe", "pipe"],
    });
    this.child.stdout.on("data", (chunk: Buffer) => this.read(chunk));
    this.child.stderr.on("data", (chunk: Buffer) => {
      this.stderr += chunk.toString("utf8");
    });
  }

  request(method: string, params: unknown): Promise<unknown> {
    const id = this.nextId;
    this.nextId += 1;
    this.write({ jsonrpc: "2.0", id, method, params });
    return new Promise((resolve, reject) => {
      this.responses.set(id, (message) => resolve(message.result));
      setTimeout(
        () => reject(new Error(`Timed out waiting for ${method}: ${this.stderr}`)),
        30_000,
      );
    });
  }

  notify(method: string, params: unknown): void {
    this.write({ jsonrpc: "2.0", method, params });
  }

  waitForNotification(method: string): Promise<JsonRpcMessage> {
    const pending = this.pendingNotifications.get(method);
    const message = pending?.shift();
    if (message) {
      return Promise.resolve(message);
    }
    return new Promise((resolve, reject) => {
      const callbacks = this.notifications.get(method) ?? [];
      callbacks.push(resolve);
      this.notifications.set(method, callbacks);
      setTimeout(
        () => reject(new Error(`Timed out waiting for ${method}: ${this.stderr}`)),
        30_000,
      );
    });
  }

  stop(): void {
    this.child?.kill();
  }

  private write(message: unknown): void {
    const body = JSON.stringify(message);
    this.child?.stdin.write(`Content-Length: ${Buffer.byteLength(body, "utf8")}\r\n\r\n${body}`);
  }

  private read(chunk: Buffer): void {
    this.buffer = Buffer.concat([this.buffer, chunk]);
    while (true) {
      const headerEnd = this.buffer.indexOf("\r\n\r\n");
      if (headerEnd === -1) {
        return;
      }
      const header = this.buffer.slice(0, headerEnd).toString("utf8");
      const length = /Content-Length:\s*(\d+)/i.exec(header)?.[1];
      if (!length) {
        throw new Error(`Missing Content-Length header: ${header}`);
      }
      const bodyStart = headerEnd + 4;
      const bodyEnd = bodyStart + Number(length);
      if (this.buffer.length < bodyEnd) {
        return;
      }
      const message = JSON.parse(
        this.buffer.slice(bodyStart, bodyEnd).toString("utf8"),
      ) as JsonRpcMessage;
      this.buffer = this.buffer.slice(bodyEnd);
      if (message.id !== undefined) {
        this.responses.get(message.id)?.(message);
        this.responses.delete(message.id);
      } else if (message.method) {
        const callbacks = this.notifications.get(message.method) ?? [];
        const callback = callbacks.shift();
        if (callback) {
          callback(message);
        } else {
          const pending = this.pendingNotifications.get(message.method) ?? [];
          pending.push(message);
          this.pendingNotifications.set(message.method, pending);
        }
      }
    }
  }
}
