import path from "node:path";
import * as vscode from "vscode";
import { getClassicAspLineCommentEdits } from "@asp-lsp/core";
import writeXlsxFile from "write-excel-file/node";
import { analysisExcelWorkbookFeatures, createAnalysisExcelSheets } from "./analysis-excel";
import {
  CloseAction,
  ErrorAction,
  LanguageClient,
  TransportKind,
  type ErrorHandler,
  type LanguageClientOptions,
  type ServerOptions,
} from "vscode-languageclient/node";
import {
  showAspGraphWebview,
  type AspGraphInfoPanelPosition,
  type AspGraphLocale,
  type AspGraphPayload,
  type AspGraphRange,
  type AspGraphWebviewThemeSetting,
} from "./include-graph-webview";
import {
  showAspFlowchartWebview,
  type AspFlowchartLabelMode,
  type AspFlowchartPayload,
  type AspFlowchartWebviewSettings,
  type AspFlowchartWebviewThemeSetting,
} from "./flowchart-webview";
import { getServerModulePath } from "./server-path";

const maxCrashRestartCount = 4;
const crashRestartWindowMs = 3 * 60 * 1000;
const reindexWorkspaceServerCommand = "aspLsp.server.reindexWorkspace";
const clearCacheServerCommand = "aspLsp.server.clearCache";
const clearDiskCacheServerCommand = "aspLsp.server.clearDiskCache";
const clearProcessCacheServerCommand = "aspLsp.server.clearProcessCache";
const buildGraphServerCommand = "aspLsp.server.buildGraph";
const buildFlowchartServerCommand = "aspLsp.server.buildFlowchart";
const serverStatusNotificationMethod = "aspLsp/status";
const htmlTagCompleteLookBehind = 2000;
const defaultFlowchartMaxTextSize = 2_000_000;
const defaultFlowchartMaxEdges = 100_000;
const defaultFlowchartLabelLineLength = 34;
const defaultFlowchartMinZoom = 0.4;
const defaultFlowchartMaxZoom = 4;
type GraphOpenLocation = "active" | "beside";
type GraphScope = "document" | "folder" | "workspace";
type WebviewThemeSetting = AspGraphWebviewThemeSetting & AspFlowchartWebviewThemeSetting;
type InfoPanelPosition = AspGraphInfoPanelPosition;
type ServerStatusKind = "idle" | "loading" | "analyzing";

interface GraphCommandRequest {
  scope: GraphScope;
  uri?: string;
  activeDocument?: vscode.TextDocument;
  includeIncomingDocumentIncludes?: boolean;
  includeRelatedIncludeTreesForUnresolved?: boolean;
  forceRelatedIncludeTreeAnalysis?: boolean;
  includeAnalysisTypeDetails?: boolean;
}

let client: LanguageClient | undefined;
let outputChannel: vscode.OutputChannel | undefined;
let statusBarItem: vscode.StatusBarItem | undefined;
let statusNotificationSubscription: vscode.Disposable | undefined;
let serverStatusKind: ServerStatusKind = "idle";
let restartPromise: Promise<void> | undefined;
let isDeactivating = false;
let isManualRestarting = false;
let crashRestartTimestamps: number[] = [];

export async function activate(context: vscode.ExtensionContext): Promise<void> {
  isDeactivating = false;
  outputChannel = vscode.window.createOutputChannel("Classic ASP LSP", "asp-lsp-output");
  statusBarItem = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Left, 100);
  statusBarItem.command = "aspLsp.openOutput";
  updateStatusBar();
  statusBarItem.show();
  context.subscriptions.push(
    outputChannel,
    statusBarItem,
    vscode.commands.registerCommand("aspLsp.restartServer", async () => restartServer(context)),
    vscode.commands.registerCommand("aspLsp.reindexWorkspace", async () =>
      executeServerCommand(reindexWorkspaceServerCommand),
    ),
    vscode.commands.registerCommand("aspLsp.clearCache", async () =>
      executeServerCommand(clearCacheServerCommand),
    ),
    vscode.commands.registerCommand("aspLsp.clearDiskCache", async () =>
      executeServerCommand(clearDiskCacheServerCommand),
    ),
    vscode.commands.registerCommand("aspLsp.clearProcessCache", async () =>
      executeServerCommand(clearProcessCacheServerCommand),
    ),
    vscode.commands.registerCommand("aspLsp.openOutput", () => outputChannel?.show()),
    vscode.commands.registerCommand("aspLsp.showCurrentFileGraph", async (uri?: vscode.Uri) =>
      showGraph(context, "document", uri),
    ),
    vscode.commands.registerCommand("aspLsp.showFolderGraph", async (uri?: vscode.Uri) =>
      showGraph(context, "folder", uri),
    ),
    vscode.commands.registerCommand("aspLsp.showWorkspaceGraph", async () =>
      showGraph(context, "workspace"),
    ),
    vscode.commands.registerCommand(
      "aspLsp.exportCurrentFileAnalysisExcel",
      async (uri?: vscode.Uri) => exportAnalysisExcel(uri),
    ),
    vscode.commands.registerCommand("aspLsp.showCurrentFileFlowchart", async (uri?: vscode.Uri) =>
      showFlowchart(context, uri),
    ),
    vscode.commands.registerCommand("aspLsp.exportCurrentFileFlowchart", async (uri?: vscode.Uri) =>
      exportFlowchart(uri),
    ),
    vscode.commands.registerCommand("aspLsp.showReferences", async (uri, position, locations) =>
      showReferences(uri, position, locations),
    ),
    vscode.commands.registerCommand("aspLsp.toggleLineComment", async () => toggleLineComment()),
    vscode.commands.registerCommand("aspLsp.debugIisUrl", async () => debugIisUrl()),
    vscode.commands.registerCommand("aspLsp.debugIisExpressUrl", async () =>
      debugBrowserUrl("iisExpress", extensionLocalizer()("debug.iisExpress.name")),
    ),
    vscode.commands.registerCommand("aspLsp.createLaunchConfig", async () => createLaunchConfig()),
    vscode.workspace.onDidChangeTextDocument((event) => {
      void autoCloseHtmlTag(event);
      void autoCloseAspBlock(event);
    }),
    vscode.tasks.registerTaskProvider("asp-lsp", new AspLspTaskProvider()),
  );
  await startClient(context);
}

async function autoCloseHtmlTag(event: vscode.TextDocumentChangeEvent): Promise<void> {
  if (
    !client ||
    event.document.languageId !== "classic-asp" ||
    event.contentChanges.length !== 1 ||
    event.contentChanges[0]?.text !== ">"
  ) {
    return;
  }
  const change = event.contentChanges[0];
  const document = event.document;
  const documentVersion = document.version;
  const position = new vscode.Position(change.range.start.line, change.range.start.character + 1);
  if (!couldTriggerHtmlTagCompleteBefore(document, position)) {
    return;
  }
  // Let vscode-languageclient enqueue the matching didChange before this custom request.
  await waitForLanguageClientTextDocumentSync();
  if (
    document.version !== documentVersion ||
    !couldTriggerHtmlTagCompleteBefore(document, position)
  ) {
    return;
  }
  const editor = vscode.window.visibleTextEditors.find(
    (candidate) => candidate.document.uri.toString() === document.uri.toString(),
  );
  if (vscode.workspace.getConfiguration("editor", document.uri).get("formatOnType")) {
    return;
  }
  const edits = await client.sendRequest<
    Array<{
      range: {
        start: { line: number; character: number };
        end: { line: number; character: number };
      };
      newText: string;
    }>
  >("textDocument/onTypeFormatting", {
    textDocument: { uri: document.uri.toString() },
    position: { line: position.line, character: position.character },
    ch: ">",
    options: {
      tabSize: numericEditorOption(editor?.options.tabSize, 2),
      insertSpaces: booleanEditorOption(editor?.options.insertSpaces, true),
    },
  });
  if (document.version !== documentVersion || !edits || edits.length === 0) {
    return;
  }
  const workspaceEdit = new vscode.WorkspaceEdit();
  for (const edit of edits) {
    workspaceEdit.replace(document.uri, toVscodeRange(edit.range), edit.newText);
  }
  const applied = await vscode.workspace.applyEdit(workspaceEdit);
  if (applied && editor) {
    editor.selection = new vscode.Selection(position, position);
  }
}

function waitForLanguageClientTextDocumentSync(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

async function autoCloseAspBlock(event: vscode.TextDocumentChangeEvent): Promise<void> {
  if (
    event.document.languageId !== "classic-asp" ||
    event.contentChanges.length !== 1 ||
    event.contentChanges[0]?.text !== "%"
  ) {
    return;
  }
  const change = event.contentChanges[0];
  const position = new vscode.Position(change.range.start.line, change.range.start.character + 1);
  if (!isAfterAspOpenDelimiter(event.document, position)) {
    return;
  }
  const workspaceEdit = new vscode.WorkspaceEdit();
  const nextPairRange =
    position.character + 1 < event.document.lineAt(position.line).text.length
      ? new vscode.Range(position, position.translate(0, 2))
      : undefined;
  if (nextPairRange && event.document.getText(nextPairRange) === "%>") {
    return;
  }
  const nextRange =
    position.character < event.document.lineAt(position.line).text.length
      ? new vscode.Range(position, position.translate(0, 1))
      : undefined;
  if (nextRange && event.document.getText(nextRange) === ">") {
    workspaceEdit.replace(event.document.uri, nextRange, "%>");
  } else {
    workspaceEdit.insert(event.document.uri, position, "%>");
  }
  const applied = await vscode.workspace.applyEdit(workspaceEdit);
  if (applied) {
    const editor = vscode.window.visibleTextEditors.find(
      (candidate) => candidate.document.uri.toString() === event.document.uri.toString(),
    );
    if (editor) {
      editor.selection = new vscode.Selection(position, position);
    }
  }
}

function couldTriggerHtmlTagCompleteBefore(
  document: vscode.TextDocument,
  position: vscode.Position,
): boolean {
  const offset = document.offsetAt(position);
  const start = document.positionAt(Math.max(0, offset - htmlTagCompleteLookBehind));
  const prefix = document.getText(new vscode.Range(start, position));
  if (!prefix.endsWith(">") || prefix.endsWith("%>")) {
    return false;
  }
  const open = prefix.lastIndexOf("<");
  if (open === -1) {
    return false;
  }
  const fragment = prefix.slice(open);
  return !fragment.startsWith("<%");
}

function isAfterAspOpenDelimiter(
  document: vscode.TextDocument,
  position: vscode.Position,
): boolean {
  if (position.character < 2) {
    return false;
  }
  const prefix = document.lineAt(position.line).text.slice(0, position.character);
  return prefix.endsWith("<%");
}

function numericEditorOption(value: string | number | undefined, fallback: number): number {
  return typeof value === "number" ? value : fallback;
}

function booleanEditorOption(value: string | boolean | undefined, fallback: boolean): boolean {
  return typeof value === "boolean" ? value : fallback;
}

async function executeServerCommand(command: string): Promise<unknown> {
  return client?.sendRequest("workspace/executeCommand", { command });
}

async function showFlowchart(
  context: vscode.ExtensionContext,
  selectedUri?: vscode.Uri,
): Promise<void> {
  const uri = currentClassicAspUri(selectedUri);
  if (!uri) {
    void vscode.window.showWarningMessage(extensionLocalizer()("flowchart.noActiveFile"));
    return;
  }
  const result = await loadFlowchartPayload(uri);
  showAspFlowchartWebview(
    context,
    result.payload,
    result.title,
    graphViewColumn(),
    extensionLocale(),
    flowchartWebviewSettings(),
    loadFlowchartPayload,
    (uri, range) => showGraphFromFlowchart(context, uri, range),
  );
}

async function showFlowchartFromGraph(
  context: vscode.ExtensionContext,
  uriText: string,
  range?: AspGraphRange,
): Promise<void> {
  const result = await loadFlowchartPayload(uriText);
  showAspFlowchartWebview(
    context,
    result.payload,
    result.title,
    graphViewColumn(),
    extensionLocale(),
    flowchartWebviewSettings(),
    loadFlowchartPayload,
    (uri, range) => showGraphFromFlowchart(context, uri, range),
    range,
  );
}

async function showGraphFromFlowchart(
  context: vscode.ExtensionContext,
  uriText: string,
  range?: AspGraphRange,
): Promise<void> {
  await showGraph(context, "document", vscode.Uri.parse(uriText), range);
}

async function exportFlowchart(selectedUri?: vscode.Uri): Promise<void> {
  const uri = currentClassicAspUri(selectedUri);
  if (!uri) {
    void vscode.window.showWarningMessage(extensionLocalizer()("flowchart.noActiveFile"));
    return;
  }
  const { payload } = await loadFlowchartPayload(uri);
  const document = await vscode.workspace.openTextDocument(
    vscode.Uri.parse(`untitled:${flowchartUntitledName(payload)}.mmd`),
  );
  const editor = await vscode.window.showTextDocument(document, { preview: false });
  await editor.edit((builder) => {
    builder.replace(new vscode.Range(0, 0, document.lineCount, 0), `${payload.mermaid}\n`);
  });
}

async function loadFlowchartPayload(
  uri: string | vscode.Uri,
  labelMode?: AspFlowchartLabelMode,
): Promise<{ payload: AspFlowchartPayload; title: string }> {
  if (!client) {
    throw new Error(extensionLocalizer()("flowchart.serverUnavailable"));
  }
  const uriText = uri instanceof vscode.Uri ? uri.toString() : uri;
  const title = extensionLocalizer()("flowchart.currentTitle");
  const payload = await vscode.window.withProgress(
    { location: vscode.ProgressLocation.Notification, title, cancellable: true },
    async (_progress, token) =>
      client!.sendRequest<AspFlowchartPayload>(
        "workspace/executeCommand",
        {
          command: buildFlowchartServerCommand,
          arguments: [
            {
              uri: uriText,
              locale: extensionLocale(),
              labelLineLength: flowchartWebviewSettings().labelLineLength,
              labelMode: labelMode ?? flowchartWebviewSettings().labelMode,
            },
          ],
        },
        token,
      ),
  );
  return {
    payload,
    title: flowchartPanelTitle(payload),
  };
}

function currentClassicAspUri(selectedUri: vscode.Uri | undefined): vscode.Uri | undefined {
  if (selectedUri) {
    return selectedUri;
  }
  const activeDocument = vscode.window.activeTextEditor?.document;
  return activeDocument?.languageId === "classic-asp" ? activeDocument.uri : undefined;
}

function flowchartPanelTitle(payload: AspFlowchartPayload): string {
  const name = payload.fileName ?? baseNameFromUri(payload.uri) ?? "Current File";
  return extensionLocalizer()("flowchart.documentPanelTitle", { name });
}

function flowchartUntitledName(payload: AspFlowchartPayload): string {
  const name = (payload.fileName ?? baseNameFromUri(payload.uri) ?? "flowchart")
    .split("/")
    .at(-1)
    ?.replace(/\.[^.]+$/, "");
  return (name || "flowchart").replace(/[^A-Za-z0-9._-]+/g, "-");
}

async function showGraph(
  context: vscode.ExtensionContext,
  scope: GraphScope,
  selectedUri?: vscode.Uri,
  initialTargetRange?: AspGraphRange,
): Promise<void> {
  if (!client) {
    void vscode.window.showWarningMessage(extensionLocalizer()("graph.serverUnavailable"));
    return;
  }
  const activeClient = client;
  const request = graphCommandRequest(scope, selectedUri);
  if (!request) {
    return;
  }
  request.includeRelatedIncludeTreesForUnresolved = relatedIncludeTreeAnalysisSetting("graph");
  let payload: AspGraphPayload;
  try {
    payload = await requestAspGraphPayload(
      activeClient,
      request,
      extensionLocalizer()(graphTitleKey(scope)),
    );
  } catch (error) {
    if (isGraphCancellationError(error)) {
      return;
    }
    throw error;
  }
  showAspGraphWebview(
    context,
    payload,
    graphPanelTitle(payload, request.activeDocument),
    graphViewColumn(),
    extensionLocale(),
    webviewThemeSetting(),
    infoPanelPositionSetting("graph.infoPanelPosition", "right"),
    (uri, range) => showFlowchartFromGraph(context, uri, range),
    initialTargetRange,
  );
}

async function exportAnalysisExcel(selectedUri?: vscode.Uri): Promise<void> {
  if (!client) {
    void vscode.window.showWarningMessage(extensionLocalizer()("excel.serverUnavailable"));
    return;
  }
  const request = graphCommandRequest("document", selectedUri);
  if (!request) {
    return;
  }
  const activeClient = client;
  const includeRelatedIncludeTreesForUnresolved = relatedIncludeTreeAnalysisSetting("excel");
  let payload: AspGraphPayload;
  try {
    payload = await requestAspGraphPayload(
      activeClient,
      {
        scope: "document",
        uri: request.uri,
        activeDocument: request.activeDocument,
        includeRelatedIncludeTreesForUnresolved,
        forceRelatedIncludeTreeAnalysis: includeRelatedIncludeTreesForUnresolved,
        includeAnalysisTypeDetails: true,
      },
      extensionLocalizer()("excel.currentTitle"),
    );
  } catch (error) {
    if (isGraphCancellationError(error)) {
      return;
    }
    throw error;
  }
  const target = await vscode.window.showSaveDialog({
    defaultUri: analysisExcelDefaultUri(payload, request.uri, request.activeDocument),
    filters: { "Excel Workbook": ["xlsx"] },
    saveLabel: extensionLocalizer()("excel.saveLabel"),
  });
  if (!target) {
    return;
  }
  const excelLocale = excelExportLocale();
  const sheets = createAnalysisExcelSheets(payload, excelLocale, {
    generatedAt: new Date(),
    targetUri: request.uri,
    settings: {
      excelLocale: excelLocaleSetting(),
      includeRelatedIncludeTreesForUnresolved,
      forceRelatedIncludeTreeAnalysis: includeRelatedIncludeTreesForUnresolved,
      includeAnalysisTypeDetails: true,
    },
  });
  await vscode.window.withProgress(
    {
      location: vscode.ProgressLocation.Notification,
      title: extensionLocalizer()("excel.writeTitle"),
      cancellable: false,
    },
    async () => {
      const workbook = await writeXlsxFile(sheets, {
        features: analysisExcelWorkbookFeatures,
        fontFamily: "Calibri",
        fontSize: 11,
      }).toBuffer();
      await vscode.workspace.fs.writeFile(target, workbook);
    },
  );
  void vscode.window.showInformationMessage(
    extensionLocalizer()("excel.exported", { file: target.fsPath }),
  );
}

function graphCommandRequest(
  scope: GraphScope,
  selectedUri?: vscode.Uri,
): GraphCommandRequest | undefined {
  const activeDocument = vscode.window.activeTextEditor?.document;
  const selectedUriText = selectedUri?.toString();
  const uri =
    scope === "document"
      ? (selectedUriText ??
        (activeDocument?.languageId === "classic-asp" ? activeDocument.uri.toString() : undefined))
      : scope === "folder"
        ? selectedUriText
        : undefined;
  if (scope === "document" && !uri) {
    void vscode.window.showWarningMessage(extensionLocalizer()("graph.noActiveFile"));
    return undefined;
  }
  if (scope === "folder" && !uri) {
    void vscode.window.showWarningMessage(extensionLocalizer()("graph.noFolder"));
    return undefined;
  }
  return { scope, uri, activeDocument };
}

async function requestAspGraphPayload(
  activeClient: LanguageClient,
  request: GraphCommandRequest,
  title: string,
): Promise<AspGraphPayload> {
  return vscode.window.withProgress(
    { location: vscode.ProgressLocation.Notification, title, cancellable: true },
    async (_progress, token) =>
      activeClient.sendRequest<AspGraphPayload>(
        "workspace/executeCommand",
        {
          command: buildGraphServerCommand,
          arguments: [
            {
              scope: request.scope,
              uri: request.uri,
              includeIncomingDocumentIncludes: request.includeIncomingDocumentIncludes,
              includeRelatedIncludeTreesForUnresolved:
                request.includeRelatedIncludeTreesForUnresolved,
              forceRelatedIncludeTreeAnalysis: request.forceRelatedIncludeTreeAnalysis,
              includeAnalysisTypeDetails: request.includeAnalysisTypeDetails,
            },
          ],
        },
        token,
      ),
  );
}

function relatedIncludeTreeAnalysisSetting(scope: "excel" | "graph"): boolean {
  return vscode.workspace
    .getConfiguration("aspLsp")
    .get<boolean>(`${scope}.includeRelatedIncludeTreesForUnresolved`, true);
}

function excelLocaleSetting(): AspGraphLocale | "auto" {
  const value = vscode.workspace.getConfiguration("aspLsp").get<string>("excel.locale", "auto");
  return value === "en" || value === "ja" ? value : "auto";
}

function excelExportLocale(): AspGraphLocale {
  const value = excelLocaleSetting();
  return localeFromSetting(value);
}

function isGraphCancellationError(error: unknown): boolean {
  if (error instanceof vscode.CancellationError) {
    return true;
  }
  if (!error || typeof error !== "object") {
    return false;
  }
  const candidate = error as { code?: unknown; message?: unknown };
  if (candidate.code === -32800) {
    return true;
  }
  return typeof candidate.message === "string" && /\bcancell?ed\b/i.test(candidate.message);
}

function graphTitleKey(scope: GraphScope): ExtensionMessageKey {
  if (scope === "document") {
    return "graph.currentTitle";
  }
  if (scope === "folder") {
    return "graph.folderTitle";
  }
  return "graph.workspaceTitle";
}

function graphViewColumn(): vscode.ViewColumn {
  const openLocation = vscode.workspace
    .getConfiguration("aspLsp")
    .get<GraphOpenLocation>("graph.openLocation", "active");
  return openLocation === "beside" ? vscode.ViewColumn.Beside : vscode.ViewColumn.Active;
}

function flowchartWebviewSettings(): AspFlowchartWebviewSettings {
  const config = vscode.workspace.getConfiguration("aspLsp");
  const maxTextSize = config.get<number>("flowchart.maxTextSize", defaultFlowchartMaxTextSize);
  const maxEdges = config.get<number>("flowchart.maxEdges", defaultFlowchartMaxEdges);
  const labelLineLength = config.get<number>(
    "flowchart.labelLineLength",
    defaultFlowchartLabelLineLength,
  );
  const minZoom = positiveFiniteNumberSetting(
    config.get<number>("flowchart.minZoom", defaultFlowchartMinZoom),
    defaultFlowchartMinZoom,
  );
  const maxZoom = positiveFiniteNumberSetting(
    config.get<number>("flowchart.maxZoom", defaultFlowchartMaxZoom),
    defaultFlowchartMaxZoom,
  );
  return {
    maxTextSize: positiveNumberSetting(maxTextSize, defaultFlowchartMaxTextSize),
    maxEdges: positiveNumberSetting(maxEdges, defaultFlowchartMaxEdges),
    labelLineLength: Math.max(
      8,
      positiveNumberSetting(labelLineLength, defaultFlowchartLabelLineLength),
    ),
    labelMode: flowchartLabelModeSetting(config.get<string>("flowchart.labelMode", "normal")),
    minZoom,
    maxZoom: Math.max(minZoom, maxZoom),
    theme: webviewThemeSetting(),
    infoPanelPosition: infoPanelPositionSetting("flowchart.infoPanelPosition", "left"),
  };
}

function flowchartLabelModeSetting(value: string): AspFlowchartLabelMode {
  return value === "raw" || value === "description" ? value : "normal";
}

function webviewThemeSetting(): WebviewThemeSetting {
  const value = vscode.workspace.getConfiguration("aspLsp").get<string>("webview.theme", "auto");
  return value === "light" || value === "dark" || value === "auto" ? value : "auto";
}

function infoPanelPositionSetting(
  key: "flowchart.infoPanelPosition" | "graph.infoPanelPosition",
  fallback: InfoPanelPosition,
): InfoPanelPosition {
  const value = vscode.workspace.getConfiguration("aspLsp").get<string>(key, fallback);
  return value === "left" || value === "right" ? value : fallback;
}

function positiveNumberSetting(value: unknown, fallback: number): number {
  return typeof value === "number" && Number.isFinite(value) && value >= 1
    ? Math.floor(value)
    : fallback;
}

function positiveFiniteNumberSetting(value: unknown, fallback: number): number {
  return typeof value === "number" && Number.isFinite(value) && value > 0 ? value : fallback;
}

function graphPanelTitle(
  payload: AspGraphPayload,
  activeDocument: vscode.TextDocument | undefined,
): string {
  const localize = extensionLocalizer();
  if (payload.scope === "workspace") {
    return localize("graph.workspacePanelTitle");
  }
  const name =
    currentFileGraphName(payload) ??
    baseNameFromUri(payload.rootUri) ??
    baseNameFromPath(activeDocument?.fileName) ??
    "Current File";
  return localize("graph.documentPanelTitle", { name });
}

function currentFileGraphName(payload: AspGraphPayload): string | undefined {
  if (payload.scope !== "document") {
    return undefined;
  }
  const rootNode =
    payload.nodes.find((node) => node.isRoot) ??
    payload.nodes.find((node) => node.uri === payload.rootUri);
  return (
    rootNode?.label || baseNameFromPath(rootNode?.fileName) || baseNameFromUri(payload.rootUri)
  );
}

function baseNameFromPath(value: string | undefined): string | undefined {
  const fileName = value?.replaceAll("\\", "/").split("/").filter(Boolean).at(-1);
  return fileName || undefined;
}

function baseNameFromUri(value: string | undefined): string | undefined {
  if (!value) {
    return undefined;
  }
  try {
    return baseNameFromPath(decodeURIComponent(new URL(value).pathname));
  } catch {
    return baseNameFromPath(value);
  }
}

function analysisExcelDefaultUri(
  payload: AspGraphPayload,
  targetUri: string | undefined,
  activeDocument: vscode.TextDocument | undefined,
): vscode.Uri | undefined {
  const fileName = `${sanitizeFileName(analysisExcelBaseName(payload, targetUri, activeDocument))}.xlsx`;
  const root = targetUri?.startsWith("file://") ? vscode.Uri.parse(targetUri) : undefined;
  if (root) {
    const directory = vscode.Uri.file(path.dirname(root.fsPath));
    return vscode.Uri.joinPath(directory, fileName);
  }
  const folder = vscode.workspace.workspaceFolders?.[0]?.uri;
  return folder ? vscode.Uri.joinPath(folder, fileName) : undefined;
}

function analysisExcelBaseName(
  payload: AspGraphPayload,
  targetUri: string | undefined,
  activeDocument: vscode.TextDocument | undefined,
): string {
  const name =
    baseNameFromUri(targetUri) ??
    currentFileGraphName(payload) ??
    baseNameFromPath(activeDocument?.fileName) ??
    "classic-asp-analysis";
  return `${name.replace(/\.[^.\\/]+$/, "")}-analysis`;
}

function sanitizeFileName(value: string): string {
  return value.replace(/[\\/:*?"<>|]+/g, "-");
}

async function startClient(context: vscode.ExtensionContext): Promise<void> {
  if (isDeactivating) {
    return;
  }
  const serverModule = getServerModulePath(context);
  const serverOptions: ServerOptions = {
    run: { module: serverModule, transport: TransportKind.ipc },
    debug: {
      module: serverModule,
      transport: TransportKind.ipc,
      options: { execArgv: ["--nolazy", "--inspect=6009"] },
    },
  };
  const clientOptions: LanguageClientOptions = {
    documentSelector: [{ scheme: "file", language: "classic-asp" }],
    outputChannel,
    synchronize: {
      configurationSection: "aspLsp",
      fileEvents: vscode.workspace.createFileSystemWatcher(
        "**/*.{asp,asa,inc,js,jsx,mjs,cjs,ts,tsx,mts,cts,d.ts}",
      ),
    },
    errorHandler: createLanguageClientErrorHandler(),
  };

  const nextClient = new LanguageClient(
    "asp-lsp",
    "Classic ASP Language Server",
    serverOptions,
    clientOptions,
  );
  statusNotificationSubscription?.dispose();
  statusNotificationSubscription = nextClient.onNotification(
    serverStatusNotificationMethod,
    handleServerStatusNotification,
  );
  client = nextClient;
  serverStatusKind = "idle";
  updateStatusBar();
  try {
    await nextClient.start();
  } catch (error) {
    statusNotificationSubscription?.dispose();
    statusNotificationSubscription = undefined;
    if (client === nextClient) {
      client = undefined;
    }
    serverStatusKind = "idle";
    updateStatusBar();
    throw error;
  }
  context.subscriptions.push(nextClient);
}

export async function deactivate(): Promise<void> {
  isDeactivating = true;
  await restartPromise?.catch(() => undefined);
  await client?.stop();
  client = undefined;
  statusNotificationSubscription?.dispose();
  statusNotificationSubscription = undefined;
  serverStatusKind = "idle";
  outputChannel?.dispose();
  outputChannel = undefined;
  statusBarItem?.dispose();
  statusBarItem = undefined;
}

async function toggleLineComment(): Promise<void> {
  const editor = vscode.window.activeTextEditor;
  if (!editor || editor.document.languageId !== "classic-asp") {
    await vscode.commands.executeCommand("editor.action.commentLine");
    return;
  }
  const edits = getClassicAspLineCommentEdits(
    editor.document.uri.toString(),
    editor.document.getText(),
    editor.selections.map((selection) => ({
      start: { line: selection.start.line, character: selection.start.character },
      end: { line: selection.end.line, character: selection.end.character },
    })),
  );
  if (edits.length === 0) {
    return;
  }
  await editor.edit((builder) => {
    for (const edit of edits) {
      builder.replace(toVscodeRange(edit.range), edit.newText);
    }
  });
}

function toVscodeRange(range: {
  start: { line: number; character: number };
  end: { line: number; character: number };
}): vscode.Range {
  return new vscode.Range(
    range.start.line,
    range.start.character,
    range.end.line,
    range.end.character,
  );
}

async function showReferences(uri: unknown, position: unknown, locations: unknown): Promise<void> {
  const targetUri = toUri(uri);
  const targetPosition = toPosition(position);
  const targetLocations = Array.isArray(locations)
    ? locations.map(toLocation).filter((location): location is vscode.Location => Boolean(location))
    : [];
  if (!targetUri || !targetPosition) {
    return;
  }
  await vscode.commands.executeCommand(
    "editor.action.showReferences",
    targetUri,
    targetPosition,
    targetLocations,
  );
}

function toUri(value: unknown): vscode.Uri | undefined {
  if (value instanceof vscode.Uri) {
    return value;
  }
  return typeof value === "string" ? vscode.Uri.parse(value) : undefined;
}

function toPosition(value: unknown): vscode.Position | undefined {
  if (value instanceof vscode.Position) {
    return value;
  }
  if (!value || typeof value !== "object") {
    return undefined;
  }
  const candidate = value as { line?: unknown; character?: unknown };
  return typeof candidate.line === "number" && typeof candidate.character === "number"
    ? new vscode.Position(candidate.line, candidate.character)
    : undefined;
}

function toRange(value: unknown): vscode.Range | undefined {
  if (value instanceof vscode.Range) {
    return value;
  }
  if (!value || typeof value !== "object") {
    return undefined;
  }
  const candidate = value as { start?: unknown; end?: unknown };
  const start = toPosition(candidate.start);
  const end = toPosition(candidate.end);
  return start && end ? new vscode.Range(start, end) : undefined;
}

function toLocation(value: unknown): vscode.Location | undefined {
  if (value instanceof vscode.Location) {
    return value;
  }
  if (!value || typeof value !== "object") {
    return undefined;
  }
  const candidate = value as { uri?: unknown; range?: unknown };
  const uri = toUri(candidate.uri);
  const range = toRange(candidate.range);
  return uri && range ? new vscode.Location(uri, range) : undefined;
}

async function restartServer(context: vscode.ExtensionContext): Promise<void> {
  restartPromise ??= restartServerOnce(context).finally(() => {
    restartPromise = undefined;
  });
  await restartPromise;
}

async function restartServerOnce(context: vscode.ExtensionContext): Promise<void> {
  isManualRestarting = true;
  try {
    await client?.stop();
    client = undefined;
    statusNotificationSubscription?.dispose();
    statusNotificationSubscription = undefined;
    serverStatusKind = "idle";
    updateStatusBar();
    crashRestartTimestamps = [];
  } finally {
    isManualRestarting = false;
  }
  await startClient(context);
}

function updateStatusBar(): void {
  if (!statusBarItem) {
    return;
  }
  const localizer = extensionLocalizer();
  if (serverStatusKind === "loading") {
    statusBarItem.text = `$(sync~spin) ${localizer("status.loading.text")}`;
    statusBarItem.tooltip = localizer("status.loading.tooltip");
    return;
  }
  if (serverStatusKind === "analyzing") {
    statusBarItem.text = `$(loading~spin) ${localizer("status.analyzing.text")}`;
    statusBarItem.tooltip = localizer("status.analyzing.tooltip");
    return;
  }
  statusBarItem.text = "$(code) ASP LSP";
  statusBarItem.tooltip = localizer("status.tooltip");
}

function handleServerStatusNotification(params: unknown): void {
  const status = (params as { status?: unknown } | undefined)?.status;
  if (status !== "idle" && status !== "loading" && status !== "analyzing") {
    return;
  }
  serverStatusKind = status;
  updateStatusBar();
}

function createLanguageClientErrorHandler(): ErrorHandler {
  return {
    error(_error, _message, count) {
      if (count && count <= 3) {
        return { action: ErrorAction.Continue };
      }
      return { action: ErrorAction.Shutdown };
    },
    closed() {
      serverStatusKind = "idle";
      updateStatusBar();
      if (isDeactivating || isManualRestarting) {
        return { action: CloseAction.DoNotRestart, handled: true };
      }
      crashRestartTimestamps.push(Date.now());
      if (crashRestartTimestamps.length <= maxCrashRestartCount) {
        return { action: CloseAction.Restart };
      }
      const elapsedMs =
        crashRestartTimestamps[crashRestartTimestamps.length - 1] - crashRestartTimestamps[0];
      if (elapsedMs <= crashRestartWindowMs) {
        return {
          action: CloseAction.DoNotRestart,
          message:
            "The Classic ASP Language Server crashed 5 times in the last 3 minutes. The server will not be restarted. See the output for more information.",
        };
      }
      crashRestartTimestamps.shift();
      return { action: CloseAction.Restart };
    },
  };
}

async function debugIisUrl(): Promise<void> {
  await debugBrowserUrl("iis", extensionLocalizer()("debug.iis.name"));
}

async function debugBrowserUrl(configPrefix: "iis" | "iisExpress", name: string): Promise<void> {
  const config = vscode.workspace.getConfiguration("aspLsp");
  const url =
    config.get<string>(`${configPrefix}.url`) ||
    (configPrefix === "iisExpress" ? "http://localhost:8080/" : "http://localhost/");
  const webRoot =
    config.get<string>(`${configPrefix}.webRoot`) ||
    vscode.workspace.workspaceFolders?.[0]?.uri.fsPath ||
    "${workspaceFolder}";
  const browser = config.get<string>(`${configPrefix}.browser`) || "pwa-chrome";
  await vscode.debug.startDebugging(undefined, {
    type: browser,
    request: "launch",
    name,
    url,
    webRoot,
  });
}

async function createLaunchConfig(): Promise<void> {
  const folder = vscode.workspace.workspaceFolders?.[0];
  if (!folder) {
    void vscode.window.showWarningMessage(extensionLocalizer()("launch.noWorkspace"));
    return;
  }
  const vscodeDir = vscode.Uri.joinPath(folder.uri, ".vscode");
  const launchUri = vscode.Uri.joinPath(vscodeDir, "launch.json");
  await vscode.workspace.fs.createDirectory(vscodeDir);
  let launch: { version: string; configurations: Array<Record<string, unknown>> } = {
    version: "0.2.0",
    configurations: [],
  };
  try {
    const existing = new TextDecoder().decode(await vscode.workspace.fs.readFile(launchUri));
    launch = JSON.parse(existing) as typeof launch;
    launch.configurations = Array.isArray(launch.configurations) ? launch.configurations : [];
  } catch {
    // Missing or invalid launch.json is replaced with a minimal browser debug config.
  }
  const config = vscode.workspace.getConfiguration("aspLsp");
  const name = extensionLocalizer()("debug.iis.name");
  const next = {
    type: config.get<string>("iis.browser") || "pwa-chrome",
    request: "launch",
    name,
    url: config.get<string>("iis.url") || "http://localhost/",
    webRoot: config.get<string>("iis.webRoot") || "${workspaceFolder}",
  };
  launch.configurations = [...launch.configurations.filter((item) => item.name !== name), next];
  await vscode.workspace.fs.writeFile(
    launchUri,
    new TextEncoder().encode(`${JSON.stringify(launch, null, 2)}\n`),
  );
  void vscode.window.showInformationMessage(extensionLocalizer()("launch.created"));
}

type ExtensionMessageKey =
  | "status.tooltip"
  | "status.loading.text"
  | "status.loading.tooltip"
  | "status.analyzing.text"
  | "status.analyzing.tooltip"
  | "debug.iis.name"
  | "debug.iisExpress.name"
  | "launch.noWorkspace"
  | "launch.created"
  | "graph.serverUnavailable"
  | "graph.noActiveFile"
  | "graph.noFolder"
  | "graph.currentTitle"
  | "graph.folderTitle"
  | "graph.workspaceTitle"
  | "graph.documentPanelTitle"
  | "graph.workspacePanelTitle"
  | "excel.serverUnavailable"
  | "excel.currentTitle"
  | "excel.saveLabel"
  | "excel.writeTitle"
  | "excel.exported"
  | "flowchart.serverUnavailable"
  | "flowchart.noActiveFile"
  | "flowchart.currentTitle"
  | "flowchart.documentPanelTitle";

type ExtensionMessageArgs = Record<string, string>;

const extensionMessages: Record<"en" | "ja", Record<ExtensionMessageKey, string>> = {
  en: {
    "status.tooltip": "Classic ASP Language Server",
    "status.loading.text": "ASP Loading",
    "status.loading.tooltip": "Classic ASP Language Server is loading workspace data.",
    "status.analyzing.text": "ASP Analyzing",
    "status.analyzing.tooltip": "Classic ASP Language Server is analyzing ASP files.",
    "debug.iis.name": "Debug Classic ASP URL",
    "debug.iisExpress.name": "Debug Classic ASP IIS Express URL",
    "launch.noWorkspace": "Open a workspace before creating launch.json.",
    "launch.created": "Classic ASP launch.json snippet created.",
    "graph.serverUnavailable": "Start the Classic ASP Language Server before building a graph.",
    "graph.noActiveFile": "Open a Classic ASP file before building the current file graph.",
    "graph.noFolder": "Select a folder before building the folder graph.",
    "graph.currentTitle": "Classic ASP: Current File Graph",
    "graph.folderTitle": "Classic ASP: Folder Graph",
    "graph.workspaceTitle": "Classic ASP: Workspace Graph",
    "graph.documentPanelTitle": "Classic ASP Graph: {name}",
    "graph.workspacePanelTitle": "Classic ASP Graph: Workspace",
    "excel.serverUnavailable": "Start the Classic ASP Language Server before exporting analysis.",
    "excel.currentTitle": "Classic ASP: Export Current File Analysis",
    "excel.saveLabel": "Export",
    "excel.writeTitle": "Writing Classic ASP analysis workbook",
    "excel.exported": "Classic ASP analysis exported to {file}.",
    "flowchart.serverUnavailable":
      "Start the Classic ASP Language Server before building a flowchart.",
    "flowchart.noActiveFile": "Open a Classic ASP file before building the current file flowchart.",
    "flowchart.currentTitle": "Classic ASP: Current File Flowchart",
    "flowchart.documentPanelTitle": "Classic ASP Flowchart: {name}",
  },
  ja: {
    "status.tooltip": "Classic ASP Language Server",
    "status.loading.text": "ASP 読み込み中",
    "status.loading.tooltip": "Classic ASP Language Server が workspace data を読み込み中です。",
    "status.analyzing.text": "ASP 解析中",
    "status.analyzing.tooltip": "Classic ASP Language Server が ASP file を解析中です。",
    "debug.iis.name": "Classic ASP URL をデバッグ",
    "debug.iisExpress.name": "Classic ASP IIS Express URL をデバッグ",
    "launch.noWorkspace": "launch.json を作成する前に workspace を開いてください。",
    "launch.created": "Classic ASP の launch.json snippet を作成しました。",
    "graph.serverUnavailable":
      "graph を作成する前に Classic ASP Language Server を起動してください。",
    "graph.noActiveFile": "current file graph を作成する前に Classic ASP file を開いてください。",
    "graph.noFolder": "folder graph を作成する前に folder を選択してください。",
    "graph.currentTitle": "Classic ASP: Current File Graph",
    "graph.folderTitle": "Classic ASP: Folder Graph",
    "graph.workspaceTitle": "Classic ASP: Workspace Graph",
    "graph.documentPanelTitle": "Classic ASP Graph: {name}",
    "graph.workspacePanelTitle": "Classic ASP Graph: Workspace",
    "excel.serverUnavailable":
      "analysis を export する前に Classic ASP Language Server を起動してください。",
    "excel.currentTitle": "Classic ASP: Current file analysis を Excel export",
    "excel.saveLabel": "Export",
    "excel.writeTitle": "Classic ASP analysis workbook を書き込み中",
    "excel.exported": "Classic ASP analysis を {file} に export しました。",
    "flowchart.serverUnavailable":
      "flowchart を作成する前に Classic ASP Language Server を起動してください。",
    "flowchart.noActiveFile":
      "current file flowchart を作成する前に Classic ASP file を開いてください。",
    "flowchart.currentTitle": "Classic ASP: Current File Flowchart",
    "flowchart.documentPanelTitle": "Classic ASP Flowchart: {name}",
  },
};

function extensionLocalizer(): (key: ExtensionMessageKey, args?: ExtensionMessageArgs) => string {
  const locale = extensionLocale();
  return (key, args) => {
    let message = extensionMessages[locale][key] ?? extensionMessages.en[key];
    for (const [name, value] of Object.entries(args ?? {})) {
      message = message.replaceAll(`{${name}}`, value);
    }
    return message;
  };
}

function extensionLocale(): AspGraphLocale {
  const configLocale = vscode.workspace.getConfiguration("aspLsp").get<string>("locale") ?? "auto";
  return localeFromSetting(configLocale);
}

function localeFromSetting(value: unknown): AspGraphLocale {
  return value === "ja" || (value !== "en" && vscode.env.language.startsWith("ja")) ? "ja" : "en";
}

class AspLspTaskProvider implements vscode.TaskProvider {
  provideTasks(): vscode.ProviderResult<vscode.Task[]> {
    return [
      this.task("typecheck", "pnpm", ["run", "typecheck"]),
      this.task("test", "pnpm", ["run", "test"]),
      this.task("build", "pnpm", ["run", "build"]),
      this.task("package VSIX", "pnpm", ["run", "package:vsix"]),
    ];
  }

  resolveTask(task: vscode.Task): vscode.ProviderResult<vscode.Task> {
    return task;
  }

  private task(name: string, command: string, args: string[]): vscode.Task {
    const definition: vscode.TaskDefinition = { type: "asp-lsp", task: name };
    const task = new vscode.Task(
      definition,
      vscode.TaskScope.Workspace,
      `asp-lsp: ${name}`,
      "asp-lsp",
      new vscode.ShellExecution(command, args),
      "$asp-lsp",
    );
    task.group = name === "build" ? vscode.TaskGroup.Build : undefined;
    return task;
  }
}
