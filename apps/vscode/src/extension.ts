import path from "node:path";
import * as vscode from "vscode";
import { getClassicAspLineCommentEdits } from "@asp-lsp/core";
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
  postAspGraphWebviewUpdate,
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
import {
  postAspNavigationGraphWebviewUpdate,
  showAspNavigationGraphWebview,
  type AspNavigationGraphLocale,
  type AspNavigationGraphPayload,
  type AspNavigationGraphWebviewSettings,
  type AspNavigationGraphWebviewThemeSetting,
} from "./navigation-graph-webview";
import {
  showWorkspaceFilesWebview,
  type WorkspaceFilesPayload,
  type WorkspaceFilesPreviewRequest,
  type WorkspaceFilesSettingsRequest,
  type WorkspaceFilesSelectedExportRequest,
} from "./workspace-files-webview";
import { getServerModulePath } from "./server-path";

const maxCrashRestartCount = 4;
const crashRestartWindowMs = 3 * 60 * 1000;
const reindexWorkspaceServerCommand = "aspLsp.server.reindexWorkspace";
const clearCacheServerCommand = "aspLsp.server.clearCache";
const clearDiskCacheServerCommand = "aspLsp.server.clearDiskCache";
const clearProcessCacheServerCommand = "aspLsp.server.clearProcessCache";
const buildGraphServerCommand = "aspLsp.server.buildGraph";
const buildFlowchartServerCommand = "aspLsp.server.buildFlowchart";
const buildNavigationGraphServerCommand = "aspLsp.server.buildNavigationGraph";
const exportAnalysisExcelServerCommand = "aspLsp.server.exportAnalysisExcel";
const previewWorkspaceFilesServerCommand = "aspLsp.server.previewWorkspaceFiles";
const cancelProgressTaskServerCommand = "aspLsp.server.cancelProgressTask";
const serverStatusNotificationMethod = "aspLsp/status";
const graphUpdatedNotificationMethod = "aspLsp/graphUpdated";
const navigationGraphUpdatedNotificationMethod = "aspLsp/navigationGraphUpdated";
const htmlTagCompleteLookBehind = 2000;
const defaultFlowchartMaxTextSize = 2_000_000;
const defaultFlowchartMaxEdges = 100_000;
const defaultFlowchartLabelLineLength = 34;
const defaultFlowchartMinZoom = 0.1;
const defaultFlowchartMaxZoom = 4;
const defaultGraphMaxDocuments = 5_000;
const defaultGraphMaxTextLength = 256 * 1024 * 1024;
const defaultGraphIncludeTreeMaxDocuments = 256;
const defaultGraphIncludeTreeMaxTextLength = 16 * 1024 * 1024;
const defaultExcelMaxDocuments = 8_192;
const defaultExcelMaxTextLength = 512 * 1024 * 1024;
const defaultExcelIncludeTreeMaxDocuments = 1_024;
const defaultExcelIncludeTreeMaxTextLength = 64 * 1024 * 1024;
type GraphOpenLocation = "active" | "beside";
type GraphScope = "document" | "folder" | "workspace";
type WebviewThemeSetting = AspGraphWebviewThemeSetting &
  AspFlowchartWebviewThemeSetting &
  AspNavigationGraphWebviewThemeSetting;
type InfoPanelPosition = AspGraphInfoPanelPosition;
type ServerStatusKind = "idle" | "loading" | "analyzing";

type ProgressTaskState = "running" | "cancelling";

interface ProgressTask {
  id: string;
  kind: Exclude<ServerStatusKind, "idle">;
  label: string;
  detail?: string;
  current?: number;
  total?: number;
  activeItems?: string[];
  cancellable?: boolean;
  state: ProgressTaskState;
  startedAt: number;
  updatedAt: number;
  source: "server" | "extension";
}

interface ProgressQuickPickItem extends vscode.QuickPickItem {
  task?: ProgressTask;
}

interface GraphCommandRequest {
  scope: GraphScope;
  uri?: string;
  activeDocument?: vscode.TextDocument;
  includeIncomingDocumentIncludes?: boolean;
  includeRelatedIncludeTreesForUnresolved?: boolean;
  forceRelatedIncludeTreeAnalysis?: boolean;
  includeAnalysisTypeDetails?: boolean;
  maxDocuments?: number;
  maxTextLength?: number;
  includeTreeMaxDocuments?: number;
  includeTreeMaxTextLength?: number;
}

interface AspGraphUpdatedNotification {
  correlationId: string;
  scope: GraphScope;
  uri?: string;
  payload?: AspGraphPayload;
  final: boolean;
  error?: string;
}

interface TrackedGraphPanel {
  panel: vscode.WebviewPanel;
  locale: AspGraphLocale;
  theme: AspGraphWebviewThemeSetting;
  infoPanelPosition: AspGraphInfoPanelPosition;
  backgroundTaskId?: string;
}

type WorkspaceFilesServerPayload = Omit<WorkspaceFilesPayload, "locale" | "settings">;

let client: LanguageClient | undefined;
let outputChannel: vscode.OutputChannel | undefined;
let statusBarItem: vscode.StatusBarItem | undefined;
let statusNotificationSubscription: vscode.Disposable | undefined;
let graphUpdatedNotificationSubscription: vscode.Disposable | undefined;
let navigationGraphUpdatedNotificationSubscription: vscode.Disposable | undefined;
let serverStatusKind: ServerStatusKind = "idle";
let serverProgressTasks: ProgressTask[] = [];
let serverProgress: { current: number; total: number } | undefined;
let extensionProgressTaskSequence = 0;
const extensionProgressTasks = new Map<string, ProgressTask>();
const graphPanelsByCorrelation = new Map<string, TrackedGraphPanel>();
const navigationGraphPanelsByKey = new Map<
  string,
  {
    panel: vscode.WebviewPanel;
    locale: AspNavigationGraphLocale;
    settings: AspNavigationGraphWebviewSettings;
  }
>();
let restartPromise: Promise<void> | undefined;
let isDeactivating = false;
let isManualRestarting = false;
let crashRestartTimestamps: number[] = [];

export async function activate(context: vscode.ExtensionContext): Promise<void> {
  isDeactivating = false;
  outputChannel = vscode.window.createOutputChannel("Classic ASP LSP", "asp-lsp-output");
  statusBarItem = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Left, 100);
  statusBarItem.command = "aspLsp.showProgressDetails";
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
    vscode.commands.registerCommand("aspLsp.showProgressDetails", async () =>
      showProgressDetails(),
    ),
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
      "aspLsp.showCurrentFileNavigationGraph",
      async (uri?: vscode.Uri) => showNavigationGraph(context, "document", uri),
    ),
    vscode.commands.registerCommand("aspLsp.showFolderNavigationGraph", async (uri?: vscode.Uri) =>
      showNavigationGraph(context, "folder", uri),
    ),
    vscode.commands.registerCommand("aspLsp.showWorkspaceNavigationGraph", async () =>
      showNavigationGraph(context, "workspace"),
    ),
    vscode.commands.registerCommand("aspLsp.showWorkspaceGlobFiles", async () =>
      showWorkspaceGlobFiles(context),
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
    vscode.workspace.onDidChangeTextDocument((event) => {
      void autoCloseHtmlTag(event);
      void autoCloseAspBlock(event);
    }),
  );
  await startClient(context);
}

async function autoCloseHtmlTag(event: vscode.TextDocumentChangeEvent): Promise<void> {
  if (
    !client ||
    event.document.languageId !== "classic-asp" ||
    event.contentChanges.length !== 1 ||
    event.contentChanges[0]?.text !== ">" ||
    !event.contentChanges[0].range.isEmpty
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
    event.contentChanges[0]?.text !== "%" ||
    !event.contentChanges[0].range.isEmpty
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

async function executeServerCommand(command: string, argument?: unknown): Promise<unknown> {
  return client?.sendRequest("workspace/executeCommand", {
    command,
    arguments: argument === undefined ? undefined : [argument],
  });
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
  Object.assign(request, graphAnalysisLimitSettings("graph"));
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
  const locale = extensionLocale();
  const theme = webviewThemeSetting();
  const infoPanelPosition = infoPanelPositionSetting("graph.infoPanelPosition", "right");
  const panel = showAspGraphWebview(
    context,
    payload,
    graphPanelTitle(payload, request.activeDocument),
    graphViewColumn(),
    locale,
    theme,
    infoPanelPosition,
    (uri, range) => showFlowchartFromGraph(context, uri, range),
    initialTargetRange,
  );
  registerGraphPanel(payload, panel, locale, theme, infoPanelPosition);
}

function registerGraphPanel(
  payload: AspGraphPayload,
  panel: vscode.WebviewPanel,
  locale: AspGraphLocale,
  theme: AspGraphWebviewThemeSetting,
  infoPanelPosition: AspGraphInfoPanelPosition,
): void {
  if (!payload.correlationId) {
    return;
  }
  const correlationId = payload.correlationId;
  graphPanelsByCorrelation.set(correlationId, {
    panel,
    locale,
    theme,
    infoPanelPosition,
    backgroundTaskId: payload.backgroundTaskId,
  });
  panel.onDidDispose(() => {
    const tracked = graphPanelsByCorrelation.get(correlationId);
    graphPanelsByCorrelation.delete(correlationId);
    if (tracked?.backgroundTaskId && !hasGraphPanelForBackgroundTask(tracked.backgroundTaskId)) {
      void executeServerCommand(cancelProgressTaskServerCommand, { id: tracked.backgroundTaskId });
    }
  });
}

function hasGraphPanelForBackgroundTask(backgroundTaskId: string): boolean {
  for (const tracked of graphPanelsByCorrelation.values()) {
    if (tracked.backgroundTaskId === backgroundTaskId) {
      return true;
    }
  }
  return false;
}

function handleGraphUpdatedNotification(notification: AspGraphUpdatedNotification): void {
  const tracked = graphPanelsByCorrelation.get(notification.correlationId);
  if (!tracked) {
    return;
  }
  void postAspGraphWebviewUpdate(
    tracked.panel,
    notification.payload,
    tracked.locale,
    tracked.theme,
    tracked.infoPanelPosition,
    { final: notification.final, error: notification.error },
  );
  if (notification.final) {
    graphPanelsByCorrelation.delete(notification.correlationId);
  }
}

async function showNavigationGraph(
  context: vscode.ExtensionContext,
  scope: GraphScope,
  selectedUri?: vscode.Uri,
): Promise<void> {
  if (!client) {
    void vscode.window.showWarningMessage(
      extensionLocalizer()("navigationGraph.serverUnavailable"),
    );
    return;
  }
  const request = navigationGraphCommandRequest(scope, selectedUri);
  if (!request) {
    return;
  }
  const payload = await loadNavigationGraphPayload(request);
  const locale = extensionLocale();
  const settings: AspNavigationGraphWebviewSettings = { theme: webviewThemeSetting() };
  const panel = showAspNavigationGraphWebview(
    context,
    payload,
    navigationGraphPanelTitle(payload, request.activeDocument),
    graphViewColumn(),
    locale,
    settings,
  );
  const key = navigationGraphPanelKey(payload.scope, payload.rootUri ?? request.uri);
  navigationGraphPanelsByKey.set(key, { panel, locale, settings });
  panel.onDidDispose(() => navigationGraphPanelsByKey.delete(key));
}

function navigationGraphCommandRequest(
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
    void vscode.window.showWarningMessage(extensionLocalizer()("navigationGraph.noActiveFile"));
    return undefined;
  }
  if (scope === "folder" && !uri) {
    void vscode.window.showWarningMessage(extensionLocalizer()("navigationGraph.noFolder"));
    return undefined;
  }
  return { scope, uri, activeDocument };
}

async function loadNavigationGraphPayload(
  request: GraphCommandRequest,
): Promise<AspNavigationGraphPayload> {
  if (!client) {
    throw new Error(extensionLocalizer()("navigationGraph.serverUnavailable"));
  }
  const activeClient = client;
  const configuration = vscode.workspace.getConfiguration("aspLsp");
  return vscode.window.withProgress(
    {
      location: vscode.ProgressLocation.Notification,
      title: extensionLocalizer()(navigationGraphTitleKey(request.scope)),
      cancellable: true,
    },
    async (_progress, token) =>
      activeClient.sendRequest<AspNavigationGraphPayload>(
        "workspace/executeCommand",
        {
          command: buildNavigationGraphServerCommand,
          arguments: [
            {
              scope: request.scope,
              uri: request.uri,
              maxNodes: configuration.get<number>("navigationGraph.maxNodes"),
              maxEdges: configuration.get<number>("navigationGraph.maxEdges"),
            },
          ],
        },
        token,
      ),
  );
}

function handleNavigationGraphUpdatedNotification(payload: AspNavigationGraphPayload): void {
  const tracked = navigationGraphPanelsByKey.get(
    navigationGraphPanelKey(payload.scope, payload.rootUri),
  );
  if (!tracked) {
    return;
  }
  void postAspNavigationGraphWebviewUpdate(
    tracked.panel,
    payload,
    tracked.locale,
    tracked.settings,
  );
}

function navigationGraphPanelKey(scope: GraphScope, uri: string | undefined): string {
  return `${scope}:${uri ?? ""}`;
}

function navigationGraphTitleKey(scope: GraphScope): ExtensionMessageKey {
  if (scope === "document") {
    return "navigationGraph.currentTitle";
  }
  if (scope === "folder") {
    return "navigationGraph.folderTitle";
  }
  return "navigationGraph.workspaceTitle";
}

function navigationGraphPanelTitle(
  payload: AspNavigationGraphPayload,
  activeDocument: vscode.TextDocument | undefined,
): string {
  const localize = extensionLocalizer();
  if (payload.scope === "workspace") {
    return localize("navigationGraph.workspacePanelTitle");
  }
  const name =
    payload.nodes.find((node) => node.isRoot)?.label ??
    baseNameFromUri(payload.rootUri) ??
    baseNameFromPath(activeDocument?.fileName) ??
    "Current File";
  return localize("navigationGraph.documentPanelTitle", { name });
}

async function showWorkspaceGlobFiles(context: vscode.ExtensionContext): Promise<void> {
  if (!client) {
    void vscode.window.showWarningMessage(extensionLocalizer()("workspaceFiles.serverUnavailable"));
    return;
  }
  const payload = await requestWorkspaceFilesPreview(undefined, "workspaceFiles.viewTitle");
  showWorkspaceFilesWebview(
    context,
    payload,
    extensionLocalizer()("workspaceFiles.viewPanelTitle"),
    graphViewColumn(),
    extensionLocale(),
    webviewThemeSetting(),
    {
      preview: (request) => requestWorkspaceFilesPreview(request, "workspaceFiles.viewTitle"),
      exportSelectedExcel: exportSelectedWorkspaceFileAnalysisExcel,
      saveSettings: saveWorkspaceFilesSettings,
    },
  );
}

async function saveWorkspaceFilesSettings(request: WorkspaceFilesSettingsRequest): Promise<void> {
  if ((vscode.workspace.workspaceFolders?.length ?? 0) === 0) {
    throw new Error(extensionLocalizer()("workspaceFiles.workspaceUnavailable"));
  }
  const configuration = vscode.workspace.getConfiguration("aspLsp");
  await configuration.update(
    "workspace.includes",
    workspaceGlobConfiguration(request.includeGlobs, ["**/*.{asp,asa,inc,vbs}"]),
    vscode.ConfigurationTarget.Workspace,
  );
  await configuration.update(
    "workspace.excludes",
    workspaceGlobConfiguration(request.excludeGlobs, []),
    vscode.ConfigurationTarget.Workspace,
  );
  await configuration.update(
    "workspace.respectGitIgnore",
    request.respectGitIgnore,
    vscode.ConfigurationTarget.Workspace,
  );
  void vscode.window.showInformationMessage(extensionLocalizer()("workspaceFiles.settingsSaved"));
}

async function requestWorkspaceFilesPreview(
  request: WorkspaceFilesPreviewRequest | undefined,
  titleKey: ExtensionMessageKey,
): Promise<WorkspaceFilesServerPayload> {
  if (!client) {
    throw new Error(extensionLocalizer()("workspaceFiles.serverUnavailable"));
  }
  const activeClient = client;
  return vscode.window.withProgress(
    {
      location: vscode.ProgressLocation.Notification,
      title: extensionLocalizer()(titleKey),
      cancellable: true,
    },
    async (_progress, token) =>
      activeClient.sendRequest<WorkspaceFilesServerPayload>(
        "workspace/executeCommand",
        {
          command: previewWorkspaceFilesServerCommand,
          arguments: request
            ? [
                {
                  includeGlobs: request.includeGlobs,
                  excludeGlobs: request.excludeGlobs,
                  respectGitIgnore: request.respectGitIgnore,
                  showUnmatched: request.showUnmatched,
                },
              ]
            : undefined,
        },
        token,
      ),
  );
}

function defaultWorkspaceFilesPreviewRequest(): WorkspaceFilesPreviewRequest {
  const configuration = vscode.workspace.getConfiguration("aspLsp");
  return {
    includeGlobs: workspaceGlobConfiguration(configuration.get<unknown>("workspace.includes"), [
      "**/*.{asp,asa,inc,vbs}",
    ]),
    excludeGlobs: workspaceGlobConfiguration(configuration.get<unknown>("workspace.excludes"), []),
    respectGitIgnore: configuration.get<boolean>("workspace.respectGitIgnore", false),
    showUnmatched: true,
  };
}

function workspaceGlobConfiguration(value: unknown, fallback: string[]): string[] {
  return Array.isArray(value)
    ? value
        .filter((item): item is string => typeof item === "string" && item.trim().length > 0)
        .map((item) => item.trim())
    : fallback;
}

async function exportSelectedWorkspaceFileAnalysisExcel(
  request: WorkspaceFilesSelectedExportRequest,
): Promise<void> {
  await exportAnalysisExcel(vscode.Uri.parse(request.selectedUri), {
    includeGlobs: request.includeGlobs,
    excludeGlobs: request.excludeGlobs,
    respectGitIgnore: request.respectGitIgnore,
    showUnmatched: request.showUnmatched,
  });
}

async function exportAnalysisExcel(
  selectedUri?: vscode.Uri,
  workspaceFilter?: WorkspaceFilesPreviewRequest,
): Promise<void> {
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
  const skipTypeInference = excelSkipTypeInferenceSetting();
  const graphLimits = graphAnalysisLimitSettings("excel");
  const workspaceFilterRequest = workspaceFilter ?? defaultWorkspaceFilesPreviewRequest();
  const exportStatus = beginExtensionProgressTask("analyzing", "excel.chooseFile", {
    current: 0,
    total: 4,
    detail: request.uri ? progressDetailFromUriText(request.uri) : undefined,
  });
  exportStatus.update({
    current: 1,
    label: "excel.chooseFile",
    detail: request.uri ? progressDetailFromUriText(request.uri) : undefined,
  });
  const target = await vscode.window.showSaveDialog({
    defaultUri: analysisExcelDefaultUri(request.uri, request.activeDocument),
    filters: { "Excel Workbook": ["xlsx"] },
    saveLabel: extensionLocalizer()("excel.saveLabel"),
  });
  if (!target) {
    exportStatus.end();
    return;
  }
  exportStatus.update({ current: 2, label: "excel.graph", detail: target.fsPath });
  try {
    await vscode.window.withProgress(
      {
        location: vscode.ProgressLocation.Notification,
        title: extensionLocalizer()("excel.writeTitle"),
        cancellable: false,
      },
      async () => {
        await activeClient.sendRequest("workspace/executeCommand", {
          command: exportAnalysisExcelServerCommand,
          arguments: [
            {
              scope: "document",
              uri: request.uri,
              activeDocument: request.activeDocument,
              targetPath: target.fsPath,
              includeGlobs: workspaceFilterRequest.includeGlobs,
              excludeGlobs: workspaceFilterRequest.excludeGlobs,
              respectGitIgnore: workspaceFilterRequest.respectGitIgnore,
              includeRelatedIncludeTreesForUnresolved,
              skipTypeInference,
              ...graphLimits,
            },
          ],
        });
        exportStatus.update({ current: 4, label: "excel.file", detail: target.fsPath });
      },
    );
  } finally {
    exportStatus.end();
  }
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
    async (_progress, token) => {
      const statusTask = beginExtensionProgressTask("analyzing", `graph.${request.scope}`, {
        detail: request.uri ? progressDetailFromUriText(request.uri) : undefined,
      });
      try {
        return await activeClient.sendRequest<AspGraphPayload>(
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
                maxDocuments: request.maxDocuments,
                maxTextLength: request.maxTextLength,
                includeTreeMaxDocuments: request.includeTreeMaxDocuments,
                includeTreeMaxTextLength: request.includeTreeMaxTextLength,
              },
            ],
          },
          token,
        );
      } finally {
        statusTask.end();
      }
    },
  );
}

function relatedIncludeTreeAnalysisSetting(scope: "excel" | "graph"): boolean {
  return vscode.workspace
    .getConfiguration("aspLsp")
    .get<boolean>(`${scope}.includeRelatedIncludeTreesForUnresolved`, true);
}

function graphAnalysisLimitSettings(scope: "excel" | "graph"): {
  maxDocuments: number;
  maxTextLength: number;
  includeTreeMaxDocuments: number;
  includeTreeMaxTextLength: number;
} {
  const defaults =
    scope === "excel"
      ? {
          maxDocuments: defaultExcelMaxDocuments,
          maxTextLength: defaultExcelMaxTextLength,
          includeTreeMaxDocuments: defaultExcelIncludeTreeMaxDocuments,
          includeTreeMaxTextLength: defaultExcelIncludeTreeMaxTextLength,
        }
      : {
          maxDocuments: defaultGraphMaxDocuments,
          maxTextLength: defaultGraphMaxTextLength,
          includeTreeMaxDocuments: defaultGraphIncludeTreeMaxDocuments,
          includeTreeMaxTextLength: defaultGraphIncludeTreeMaxTextLength,
        };
  const configuration = vscode.workspace.getConfiguration("aspLsp");
  return {
    maxDocuments: positiveNumberSetting(
      configuration.get<number>(`${scope}.maxDocuments`, defaults.maxDocuments),
      defaults.maxDocuments,
    ),
    maxTextLength: positiveNumberSetting(
      configuration.get<number>(`${scope}.maxTextLength`, defaults.maxTextLength),
      defaults.maxTextLength,
    ),
    includeTreeMaxDocuments: positiveNumberSetting(
      configuration.get<number>(
        `${scope}.includeTreeMaxDocuments`,
        defaults.includeTreeMaxDocuments,
      ),
      defaults.includeTreeMaxDocuments,
    ),
    includeTreeMaxTextLength: positiveNumberSetting(
      configuration.get<number>(
        `${scope}.includeTreeMaxTextLength`,
        defaults.includeTreeMaxTextLength,
      ),
      defaults.includeTreeMaxTextLength,
    ),
  };
}

function excelSkipTypeInferenceSetting(): boolean {
  return vscode.workspace.getConfiguration("aspLsp").get<boolean>("excel.skipTypeInference", false);
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
    showSourcePanel: config.get<boolean>("flowchart.showSourcePanel", true),
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
  targetUri: string | undefined,
  activeDocument: vscode.TextDocument | undefined,
): vscode.Uri | undefined {
  const fileName = `${sanitizeFileName(analysisExcelBaseName(targetUri, activeDocument))}.xlsx`;
  const root = targetUri?.startsWith("file://") ? vscode.Uri.parse(targetUri) : undefined;
  if (root) {
    const directory = vscode.Uri.file(path.dirname(root.fsPath));
    return vscode.Uri.joinPath(directory, fileName);
  }
  const folder = vscode.workspace.workspaceFolders?.[0]?.uri;
  return folder ? vscode.Uri.joinPath(folder, fileName) : undefined;
}

function analysisExcelBaseName(
  targetUri: string | undefined,
  activeDocument: vscode.TextDocument | undefined,
): string {
  const name =
    baseNameFromUri(targetUri) ??
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
  const serverEnv = {
    ...process.env,
    ASP_LSP_DEFAULT_DEBUG_LOG_FILE: path.join(context.globalStorageUri.fsPath, "asp-lsp-debug.log"),
  };
  const serverOptions: ServerOptions = {
    run: { module: serverModule, transport: TransportKind.ipc, options: { env: serverEnv } },
    debug: {
      module: serverModule,
      transport: TransportKind.ipc,
      options: { execArgv: ["--nolazy", "--inspect=6009"], env: serverEnv },
    },
  };
  const clientOptions: LanguageClientOptions = {
    documentSelector: [
      { scheme: "file", language: "classic-asp" },
      { scheme: "file", language: "vbscript" },
    ],
    outputChannel,
    synchronize: {
      configurationSection: "aspLsp",
      fileEvents: vscode.workspace.createFileSystemWatcher(
        "**/*.{asp,asa,inc,vbs,js,jsx,mjs,cjs,ts,tsx,mts,cts,d.ts}",
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
  graphUpdatedNotificationSubscription?.dispose();
  navigationGraphUpdatedNotificationSubscription?.dispose();
  statusNotificationSubscription = nextClient.onNotification(
    serverStatusNotificationMethod,
    handleServerStatusNotification,
  );
  graphUpdatedNotificationSubscription = nextClient.onNotification(
    graphUpdatedNotificationMethod,
    handleGraphUpdatedNotification,
  );
  navigationGraphUpdatedNotificationSubscription = nextClient.onNotification(
    navigationGraphUpdatedNotificationMethod,
    handleNavigationGraphUpdatedNotification,
  );
  client = nextClient;
  serverStatusKind = "idle";
  clearServerProgressState();
  updateStatusBar();
  try {
    await nextClient.start();
  } catch (error) {
    statusNotificationSubscription?.dispose();
    statusNotificationSubscription = undefined;
    graphUpdatedNotificationSubscription?.dispose();
    graphUpdatedNotificationSubscription = undefined;
    navigationGraphUpdatedNotificationSubscription?.dispose();
    navigationGraphUpdatedNotificationSubscription = undefined;
    if (client === nextClient) {
      client = undefined;
    }
    serverStatusKind = "idle";
    clearServerProgressState();
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
  graphUpdatedNotificationSubscription?.dispose();
  graphUpdatedNotificationSubscription = undefined;
  navigationGraphUpdatedNotificationSubscription?.dispose();
  navigationGraphUpdatedNotificationSubscription = undefined;
  graphPanelsByCorrelation.clear();
  navigationGraphPanelsByKey.clear();
  serverStatusKind = "idle";
  clearServerProgressState();
  extensionProgressTasks.clear();
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
    graphUpdatedNotificationSubscription?.dispose();
    graphUpdatedNotificationSubscription = undefined;
    navigationGraphUpdatedNotificationSubscription?.dispose();
    navigationGraphUpdatedNotificationSubscription = undefined;
    graphPanelsByCorrelation.clear();
    navigationGraphPanelsByKey.clear();
    serverStatusKind = "idle";
    clearServerProgressState();
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
  const tasks = activeProgressTasks();
  const primaryTask = primaryProgressTask(tasks);
  const progressText =
    progressValueText(progressFromTask(primaryTask)) ||
    progressSummaryText(tasks) ||
    progressValueText(serverProgress);
  const activeKind = activeStatusKind(tasks);
  if (activeKind === "loading") {
    statusBarItem.text = `$(sync~spin) ${progressStatusText(
      "status.loading.text",
      "status.progress.loadingStatusText",
      primaryTask,
      localizer,
    )}${progressText}`;
    statusBarItem.tooltip = progressTooltip(localizer("status.loading.tooltip"), tasks, localizer);
    return;
  }
  if (activeKind === "analyzing") {
    statusBarItem.text = `$(loading~spin) ${progressStatusText(
      "status.analyzing.text",
      "status.progress.analyzingStatusText",
      primaryTask,
      localizer,
    )}${progressText}`;
    statusBarItem.tooltip = progressTooltip(
      localizer("status.analyzing.tooltip"),
      tasks,
      localizer,
    );
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
  serverProgress = progressFromStatusNotification(params);
  serverProgressTasks = progressTasksFromStatusNotification(params);
  updateStatusBar();
}

function clearServerProgressState(): void {
  serverProgress = undefined;
  serverProgressTasks = [];
}

function activeProgressTasks(): ProgressTask[] {
  const extensionFallbackTasks = [...extensionProgressTasks.values()].filter(
    (task) => !serverProgressTasks.some((serverTask) => serverTask.label === task.label),
  );
  return [...serverProgressTasks, ...extensionFallbackTasks];
}

function activeStatusKind(tasks: ProgressTask[]): ServerStatusKind {
  if (tasks.some((task) => task.kind === "analyzing")) {
    return "analyzing";
  }
  if (tasks.some((task) => task.kind === "loading")) {
    return "loading";
  }
  return serverStatusKind;
}

function progressSummaryText(tasks: ProgressTask[]): string | undefined {
  const progress = aggregateProgress(tasks);
  return progressValueText(progress) || undefined;
}

function progressStatusText(
  fallbackKey: ExtensionMessageKey,
  taskKey: ExtensionMessageKey,
  task: ProgressTask | undefined,
  localizer: (key: ExtensionMessageKey, args?: ExtensionMessageArgs) => string,
): string {
  if (!task) {
    return localizer(fallbackKey);
  }
  const label = progressTaskDisplayLabel(task.label, localizer);
  const detail = progressStatusBarDetail(task.detail);
  return localizer(taskKey, { task: detail ? `${label}: ${detail}` : label });
}

function primaryProgressTask(tasks: ProgressTask[]): ProgressTask | undefined {
  return [...tasks].sort(
    (left, right) =>
      right.updatedAt - left.updatedAt ||
      progressTaskStatusPriority(right) - progressTaskStatusPriority(left) ||
      right.startedAt - left.startedAt,
  )[0];
}

function progressTaskStatusPriority(task: ProgressTask): number {
  return task.label.startsWith("excel.") ? 10 : 0;
}

function aggregateProgress(tasks: ProgressTask[]): { current: number; total: number } | undefined {
  const measurable = tasks.filter(
    (task) => typeof task.current === "number" && typeof task.total === "number",
  );
  if (measurable.length === 0) {
    return undefined;
  }
  return measurable.reduce(
    (total, task) => ({
      current: total.current + (task.current ?? 0),
      total: total.total + (task.total ?? 0),
    }),
    { current: 0, total: 0 },
  );
}

function progressFromTask(
  task: ProgressTask | undefined,
): { current: number; total: number } | undefined {
  return task && typeof task.current === "number" && typeof task.total === "number"
    ? { current: task.current, total: task.total }
    : undefined;
}

function progressStatusBarDetail(detail: string | undefined): string | undefined {
  if (!detail) {
    return undefined;
  }
  const normalized = detail.replace(/\s+/g, " ").trim();
  if (normalized.length <= 72) {
    return normalized;
  }
  return `${normalized.slice(0, 34)}...${normalized.slice(-35)}`;
}

function progressValueText(progress: { current: number; total: number } | undefined): string {
  if (!progress || progress.total <= 0) {
    return "";
  }
  const percent = Math.max(0, Math.min(100, Math.round((progress.current / progress.total) * 100)));
  return ` ${progress.current}/${progress.total} (${percent}%)`;
}

function progressTooltip(
  fallback: string,
  tasks: ProgressTask[],
  localizer: (key: ExtensionMessageKey, args?: ExtensionMessageArgs) => string,
): string {
  if (tasks.length === 0) {
    return fallback;
  }
  return tasks
    .map((task) => {
      const progress = progressValueText(
        typeof task.current === "number" && typeof task.total === "number"
          ? { current: task.current, total: task.total }
          : undefined,
      ).trim();
      const state =
        task.state === "cancelling" ? ` ${localizer("status.progress.cancelling")}` : "";
      const detail = task.detail ? ` - ${task.detail}` : "";
      const label = progressTaskDisplayLabel(task.label, localizer);
      return `${label}${progress ? ` ${progress}` : ""}${state}${detail}`;
    })
    .join("\n");
}

function progressFromStatusNotification(
  params: unknown,
): { current: number; total: number } | undefined {
  const progress = (params as { progress?: unknown } | undefined)?.progress;
  if (!progress || typeof progress !== "object") {
    return undefined;
  }
  const current = (progress as { current?: unknown }).current;
  const total = (progress as { total?: unknown }).total;
  return typeof current === "number" && typeof total === "number" ? { current, total } : undefined;
}

function progressTasksFromStatusNotification(params: unknown): ProgressTask[] {
  const tasks = (params as { tasks?: unknown } | undefined)?.tasks;
  if (!Array.isArray(tasks)) {
    return [];
  }
  return tasks.flatMap((task) => {
    const parsed = progressTaskFromStatusNotification(task);
    return parsed ? [parsed] : [];
  });
}

function progressTaskFromStatusNotification(task: unknown): ProgressTask | undefined {
  if (!task || typeof task !== "object") {
    return undefined;
  }
  const record = task as Record<string, unknown>;
  const id = record.id;
  const kind = record.kind;
  const label = record.label;
  if (
    typeof id !== "string" ||
    (kind !== "loading" && kind !== "analyzing") ||
    typeof label !== "string"
  ) {
    return undefined;
  }
  const state = record.state === "cancelling" ? "cancelling" : "running";
  const activeItems = Array.isArray(record.activeItems)
    ? record.activeItems.filter((item): item is string => typeof item === "string")
    : undefined;
  const startedAt = typeof record.startedAt === "number" ? record.startedAt : Date.now();
  return {
    id,
    kind,
    label,
    detail: typeof record.detail === "string" ? record.detail : undefined,
    current: typeof record.current === "number" ? record.current : undefined,
    total: typeof record.total === "number" ? record.total : undefined,
    activeItems,
    cancellable: record.cancellable === true,
    state,
    startedAt,
    updatedAt: typeof record.updatedAt === "number" ? record.updatedAt : startedAt,
    source: "server",
  };
}

function beginExtensionProgressTask(
  kind: Exclude<ServerStatusKind, "idle">,
  label: string,
  options: {
    detail?: string;
    current?: number;
    total?: number;
    cancellable?: boolean;
  } = {},
): {
  update(update: Partial<Pick<ProgressTask, "label" | "detail" | "current" | "total">>): void;
  end(): void;
} {
  const id = `extension-${++extensionProgressTaskSequence}`;
  const startedAt = Date.now();
  extensionProgressTasks.set(id, {
    id,
    kind,
    label,
    detail: options.detail,
    current: options.current,
    total: options.total,
    cancellable: options.cancellable === true,
    state: "running",
    startedAt,
    updatedAt: startedAt,
    source: "extension",
  });
  updateStatusBar();
  return {
    update(update) {
      const task = extensionProgressTasks.get(id);
      if (!task) {
        return;
      }
      if (update.detail !== undefined) {
        task.detail = update.detail;
      }
      if (update.label !== undefined) {
        task.label = update.label;
      }
      if (update.current !== undefined) {
        task.current = update.current;
      }
      if (update.total !== undefined) {
        task.total = update.total;
      }
      task.updatedAt = Date.now();
      updateStatusBar();
    },
    end() {
      extensionProgressTasks.delete(id);
      updateStatusBar();
    },
  };
}

async function showProgressDetails(): Promise<void> {
  const localizer = extensionLocalizer();
  const quickPick = vscode.window.createQuickPick<ProgressQuickPickItem>();
  quickPick.title = localizer("status.progress.title");
  quickPick.placeholder = localizer("status.progress.placeholder");
  quickPick.matchOnDescription = true;
  quickPick.matchOnDetail = true;
  const refresh = (): void => {
    quickPick.items = progressQuickPickItems(localizer);
  };
  refresh();
  const refreshTimer = setInterval(refresh, 500);
  quickPick.onDidTriggerItemButton(async (event) => {
    const task = event.item.task;
    if (!task || !task.cancellable || task.state === "cancelling") {
      return;
    }
    await cancelProgressTask(task);
    refresh();
  });
  quickPick.onDidHide(() => {
    clearInterval(refreshTimer);
    quickPick.dispose();
  });
  quickPick.show();
}

function progressQuickPickItems(
  localizer: (key: ExtensionMessageKey, args?: ExtensionMessageArgs) => string,
): ProgressQuickPickItem[] {
  const tasks = activeProgressTasks();
  const taskItems = tasks.map((task): ProgressQuickPickItem => {
    const progress = progressValueText(
      typeof task.current === "number" && typeof task.total === "number"
        ? { current: task.current, total: task.total }
        : undefined,
    ).trim();
    const state = task.state === "cancelling" ? ` ${localizer("status.progress.cancelling")}` : "";
    const activeItems =
      task.activeItems && task.activeItems.length > 0
        ? `\n${localizer("status.progress.active")}: ${task.activeItems.join(", ")}`
        : "";
    return {
      label: `${progressTaskDisplayLabel(task.label, localizer)}${progress ? ` ${progress}` : ""}${state}`,
      description: task.detail,
      detail: activeItems || undefined,
      task,
      buttons:
        task.cancellable && task.state !== "cancelling"
          ? [
              {
                iconPath: new vscode.ThemeIcon("close"),
                tooltip: localizer("status.progress.cancel"),
              },
            ]
          : undefined,
    };
  });
  return [
    ...(taskItems.length > 0
      ? taskItems
      : [{ label: localizer("status.progress.none"), alwaysShow: true }]),
  ];
}

function progressTaskDisplayLabel(
  label: string,
  localizer: (key: ExtensionMessageKey, args?: ExtensionMessageArgs) => string,
): string {
  const key = progressTaskDisplayLabelKey(label);
  return key ? localizer(key) : label;
}

function progressTaskDisplayLabelKey(label: string): ExtensionMessageKey | undefined {
  switch (label) {
    case "workspace.diagnostics":
      return "status.progress.workspaceDiagnostics";
    case "workspace.diagnostics.indexed":
      return "status.progress.workspaceDiagnosticsIndexed";
    case "workspace.diagnostics.openDocuments":
      return "status.progress.workspaceDiagnosticsOpenDocuments";
    case "diagnostics":
      return "status.progress.diagnostics";
    case "diagnostics.include":
      return "status.progress.diagnosticsInclude";
    case "diagnostics.syntax":
      return "status.progress.diagnosticsSyntax";
    case "diagnostics.projectFast":
      return "status.progress.diagnosticsProjectFast";
    case "diagnostics.project":
      return "status.progress.diagnosticsProject";
    case "document.analysis":
      return "status.progress.documentAnalysis";
    case "document.analysis.incremental":
      return "status.progress.documentAnalysisIncremental";
    case "document.analysis.parse":
      return "status.progress.documentAnalysisParse";
    case "document.analysis.cache":
      return "status.progress.documentAnalysisCache";
    case "document.analysis.ready":
      return "status.progress.documentAnalysisReady";
    case "workspace.index":
      return "status.progress.workspaceIndex";
    case "workspace.index.scanRoot":
      return "status.progress.workspaceIndexScanRoot";
    case "workspace.index.scanFiles":
      return "status.progress.workspaceIndexScanFiles";
    case "workspace.index.writeCache":
      return "status.progress.workspaceIndexWriteCache";
    case "workspace.previewFiles":
      return "status.progress.workspacePreviewFiles";
    case "flowchart.build":
      return "status.progress.flowchart";
    case "flowchart.loadDocument":
      return "status.progress.flowchartLoadDocument";
    case "flowchart.hydrateDocument":
      return "status.progress.flowchartHydrateDocument";
    case "flowchart.collectIncludes":
      return "status.progress.flowchartCollectIncludes";
    case "flowchart.indexDocuments":
      return "status.progress.flowchartIndexDocuments";
    case "flowchart.canonicalizeSymbols":
      return "status.progress.flowchartCanonicalizeSymbols";
    case "flowchart.buildPayload":
      return "status.progress.flowchartBuildPayload";
    case "navigationGraph.build":
      return "status.progress.navigationGraph";
    case "navigationGraph.collectDocuments":
      return "status.progress.navigationGraphCollectDocuments";
    case "navigationGraph.resolveIncludes":
      return "status.progress.navigationGraphResolveIncludes";
    case "navigationGraph.extract":
      return "status.progress.navigationGraphExtract";
    case "navigationGraph.buildPayload":
      return "status.progress.navigationGraphBuildPayload";
    case "graph.document":
      return "status.progress.graphDocument";
    case "graph.folder":
      return "status.progress.graphFolder";
    case "graph.workspace":
      return "status.progress.graphWorkspace";
    case "graph.workspaceIndex":
      return "status.progress.graphWorkspaceIndex";
    case "graph.openDocuments":
      return "status.progress.graphOpenDocuments";
    case "graph.prepareDocuments":
      return "status.progress.graphPrepareDocuments";
    case "graph.loadDocuments":
      return "status.progress.graphLoadDocuments";
    case "graph.collectIncludes":
      return "status.progress.graphCollectIncludes";
    case "graph.prefetchIncludes":
      return "status.progress.graphPrefetchIncludes";
    case "graph.resolveIncludes":
      return "status.progress.graphResolveIncludes";
    case "graph.collectRelatedIncludes":
      return "status.progress.graphCollectRelatedIncludes";
    case "graph.checkRelatedIncludes":
      return "status.progress.graphCheckRelatedIncludes";
    case "graph.collectIncomingIncludes":
      return "status.progress.graphCollectIncomingIncludes";
    case "graph.findIncomingIncludes":
      return "status.progress.graphFindIncomingIncludes";
    case "graph.reverseIncludeIndex":
      return "status.progress.graphReverseIncludeIndex";
    case "graph.filterIncomingIncludes":
      return "status.progress.graphFilterIncomingIncludes";
    case "graph.indexDocuments":
      return "status.progress.graphIndexDocuments";
    case "graph.spillIndexes":
      return "status.progress.graphSpillIndexes";
    case "graph.canonicalizeSymbols":
      return "status.progress.graphCanonicalizeSymbols";
    case "graph.addStructure":
      return "status.progress.graphAddStructure";
    case "graph.addUsages":
      return "status.progress.graphAddUsages";
    case "graph.finalize":
      return "status.progress.graphFinalize";
    case "references.count":
      return "status.progress.referencesCount";
    case "references.workspace":
      return "status.progress.referencesWorkspace";
    case "references.relatedIncludeTree":
      return "status.progress.referencesRelatedIncludeTree";
    case "references.finalize":
      return "status.progress.referencesFinalize";
    case "excel.graph":
      return "status.progress.excelGraph";
    case "excel.normalizeGraph":
      return "status.progress.excelNormalizeGraph";
    case "excel.analysisContext":
      return "status.progress.excelAnalysisContext";
    case "excel.analysisSummary":
      return "status.progress.excelAnalysisSummary";
    case "excel.sheet":
      return "status.progress.excelSheet";
    case "excel.chooseFile":
      return "status.progress.excelChooseFile";
    case "excel.sheets":
      return "status.progress.excelSheets";
    case "excel.workbook":
      return "status.progress.excelWorkbook";
    case "excel.file":
      return "status.progress.excelFile";
    case "excel.fileSheet":
      return "status.progress.excelFileSheet";
    case "excel.fileRows":
      return "status.progress.excelFileRows";
    case "excel.fileCommit":
      return "status.progress.excelFileCommit";
    case "excel.write":
      return "status.progress.excel";
    default:
      return undefined;
  }
}

function progressDetailFromUriText(uriText: string): string {
  return baseNameFromUri(uriText) ?? uriText;
}

async function cancelProgressTask(task: ProgressTask): Promise<void> {
  if (task.source === "extension") {
    const current = extensionProgressTasks.get(task.id);
    if (current) {
      current.state = "cancelling";
      current.updatedAt = Date.now();
      updateStatusBar();
    }
    return;
  }
  await client?.sendRequest("workspace/executeCommand", {
    command: cancelProgressTaskServerCommand,
    arguments: [{ id: task.id }],
  });
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

type ExtensionMessageKey =
  | "status.tooltip"
  | "status.loading.text"
  | "status.loading.tooltip"
  | "status.analyzing.text"
  | "status.analyzing.tooltip"
  | "status.progress.active"
  | "status.progress.cancel"
  | "status.progress.cancelling"
  | "status.progress.analyzingStatusText"
  | "status.progress.diagnostics"
  | "status.progress.diagnosticsInclude"
  | "status.progress.diagnosticsProjectFast"
  | "status.progress.diagnosticsProject"
  | "status.progress.diagnosticsSyntax"
  | "status.progress.documentAnalysis"
  | "status.progress.documentAnalysisCache"
  | "status.progress.documentAnalysisIncremental"
  | "status.progress.documentAnalysisParse"
  | "status.progress.documentAnalysisReady"
  | "status.progress.excel"
  | "status.progress.excelChooseFile"
  | "status.progress.excelFile"
  | "status.progress.excelFileCommit"
  | "status.progress.excelFileRows"
  | "status.progress.excelFileSheet"
  | "status.progress.excelGraph"
  | "status.progress.excelAnalysisContext"
  | "status.progress.excelAnalysisSummary"
  | "status.progress.excelNormalizeGraph"
  | "status.progress.excelSheet"
  | "status.progress.excelSheets"
  | "status.progress.excelWorkbook"
  | "status.progress.flowchart"
  | "status.progress.flowchartBuildPayload"
  | "status.progress.flowchartCanonicalizeSymbols"
  | "status.progress.flowchartCollectIncludes"
  | "status.progress.flowchartHydrateDocument"
  | "status.progress.flowchartIndexDocuments"
  | "status.progress.flowchartLoadDocument"
  | "status.progress.navigationGraph"
  | "status.progress.navigationGraphBuildPayload"
  | "status.progress.navigationGraphCollectDocuments"
  | "status.progress.navigationGraphExtract"
  | "status.progress.navigationGraphResolveIncludes"
  | "status.progress.graphAddStructure"
  | "status.progress.graphAddUsages"
  | "status.progress.graphCanonicalizeSymbols"
  | "status.progress.graphCheckRelatedIncludes"
  | "status.progress.graphCollectIncomingIncludes"
  | "status.progress.graphCollectIncludes"
  | "status.progress.graphCollectRelatedIncludes"
  | "status.progress.graphDocument"
  | "status.progress.graphFilterIncomingIncludes"
  | "status.progress.graphFindIncomingIncludes"
  | "status.progress.graphFinalize"
  | "status.progress.graphFolder"
  | "status.progress.graphIndexDocuments"
  | "status.progress.graphLoadDocuments"
  | "status.progress.graphOpenDocuments"
  | "status.progress.graphPrepareDocuments"
  | "status.progress.graphPrefetchIncludes"
  | "status.progress.graphResolveIncludes"
  | "status.progress.graphReverseIncludeIndex"
  | "status.progress.graphSpillIndexes"
  | "status.progress.graphWorkspaceIndex"
  | "status.progress.graphWorkspace"
  | "status.progress.loadingStatusText"
  | "status.progress.none"
  | "status.progress.placeholder"
  | "status.progress.referencesCount"
  | "status.progress.referencesFinalize"
  | "status.progress.referencesRelatedIncludeTree"
  | "status.progress.referencesWorkspace"
  | "status.progress.title"
  | "status.progress.workspaceDiagnostics"
  | "status.progress.workspaceDiagnosticsIndexed"
  | "status.progress.workspaceDiagnosticsOpenDocuments"
  | "status.progress.workspaceIndex"
  | "status.progress.workspaceIndexScanFiles"
  | "status.progress.workspaceIndexScanRoot"
  | "status.progress.workspaceIndexWriteCache"
  | "status.progress.workspacePreviewFiles"
  | "graph.serverUnavailable"
  | "graph.noActiveFile"
  | "graph.noFolder"
  | "graph.currentTitle"
  | "graph.folderTitle"
  | "graph.workspaceTitle"
  | "graph.documentPanelTitle"
  | "graph.workspacePanelTitle"
  | "navigationGraph.serverUnavailable"
  | "navigationGraph.noActiveFile"
  | "navigationGraph.noFolder"
  | "navigationGraph.currentTitle"
  | "navigationGraph.folderTitle"
  | "navigationGraph.workspaceTitle"
  | "navigationGraph.documentPanelTitle"
  | "navigationGraph.workspacePanelTitle"
  | "excel.serverUnavailable"
  | "excel.currentTitle"
  | "excel.saveLabel"
  | "excel.writeTitle"
  | "excel.exported"
  | "workspaceFiles.serverUnavailable"
  | "workspaceFiles.settingsSaved"
  | "workspaceFiles.viewTitle"
  | "workspaceFiles.viewPanelTitle"
  | "workspaceFiles.workspaceUnavailable"
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
    "status.progress.active": "Active",
    "status.progress.cancel": "Cancel",
    "status.progress.cancelling": "Cancelling",
    "status.progress.analyzingStatusText": "ASP {task}",
    "status.progress.diagnostics": "Document diagnostics",
    "status.progress.diagnosticsInclude": "Checking include diagnostics",
    "status.progress.diagnosticsProjectFast": "Checking fast project diagnostics",
    "status.progress.diagnosticsProject": "Checking project diagnostics",
    "status.progress.diagnosticsSyntax": "Checking syntax diagnostics",
    "status.progress.documentAnalysis": "Document analysis",
    "status.progress.documentAnalysisCache": "Updating document analysis cache",
    "status.progress.documentAnalysisIncremental": "Applying incremental document analysis",
    "status.progress.documentAnalysisParse": "Parsing document",
    "status.progress.documentAnalysisReady": "Document analysis ready",
    "status.progress.excel": "Creating Excel workbook",
    "status.progress.excelChooseFile": "Choosing Excel output path",
    "status.progress.excelFile": "Writing Excel file",
    "status.progress.excelFileCommit": "Finalizing Excel file",
    "status.progress.excelFileRows": "Writing Excel rows",
    "status.progress.excelFileSheet": "Writing Excel sheet",
    "status.progress.excelGraph": "Collecting Excel analysis graph",
    "status.progress.excelAnalysisContext": "Classifying Excel graph data",
    "status.progress.excelAnalysisSummary": "Building Excel analysis summary",
    "status.progress.excelNormalizeGraph": "Normalizing Excel graph payload",
    "status.progress.excelSheet": "Building Excel sheet",
    "status.progress.excelSheets": "Building Excel sheets",
    "status.progress.excelWorkbook": "Generating Excel workbook",
    "status.progress.flowchart": "Generating flowchart",
    "status.progress.flowchartBuildPayload": "Building flowchart payload",
    "status.progress.flowchartCanonicalizeSymbols": "Canonicalizing flowchart symbols",
    "status.progress.flowchartCollectIncludes": "Collecting flowchart include tree",
    "status.progress.flowchartHydrateDocument": "Hydrating flowchart VBScript",
    "status.progress.flowchartIndexDocuments": "Indexing flowchart documents",
    "status.progress.flowchartLoadDocument": "Loading flowchart document",
    "status.progress.navigationGraph": "Generating navigation graph",
    "status.progress.navigationGraphBuildPayload": "Building navigation graph payload",
    "status.progress.navigationGraphCollectDocuments": "Collecting navigation graph documents",
    "status.progress.navigationGraphExtract": "Extracting navigation transitions",
    "status.progress.navigationGraphResolveIncludes": "Resolving navigation include owners",
    "status.progress.graphAddStructure": "Adding graph structure",
    "status.progress.graphAddUsages": "Adding graph usages",
    "status.progress.graphCanonicalizeSymbols": "Canonicalizing graph symbols",
    "status.progress.graphCheckRelatedIncludes": "Checking related include analysis need",
    "status.progress.graphCollectIncomingIncludes": "Collecting incoming include files",
    "status.progress.graphCollectIncludes": "Collecting include tree",
    "status.progress.graphCollectRelatedIncludes": "Collecting related include trees",
    "status.progress.graphDocument": "Generating current file graph",
    "status.progress.graphFilterIncomingIncludes": "Filtering incoming include files",
    "status.progress.graphFindIncomingIncludes": "Checking open incoming include files",
    "status.progress.graphFinalize": "Finalizing graph",
    "status.progress.graphFolder": "Generating folder graph",
    "status.progress.graphIndexDocuments": "Indexing graph documents",
    "status.progress.graphLoadDocuments": "Loading graph documents",
    "status.progress.graphOpenDocuments": "Loading open graph documents",
    "status.progress.graphPrepareDocuments": "Preparing graph documents",
    "status.progress.graphPrefetchIncludes": "Prefetching include targets",
    "status.progress.graphResolveIncludes": "Resolving include paths",
    "status.progress.graphReverseIncludeIndex": "Reading reverse include index",
    "status.progress.graphSpillIndexes": "Writing graph index spill files",
    "status.progress.graphWorkspace": "Generating workspace graph",
    "status.progress.graphWorkspaceIndex": "Loading graph workspace index",
    "status.progress.loadingStatusText": "ASP Loading: {task}",
    "status.progress.none": "No active Classic ASP tasks.",
    "status.progress.placeholder": "Current Classic ASP tasks",
    "status.progress.referencesCount": "Reference count analysis",
    "status.progress.referencesFinalize": "Finalizing reference count",
    "status.progress.referencesRelatedIncludeTree": "Counting related include-tree references",
    "status.progress.referencesWorkspace": "Counting workspace references",
    "status.progress.title": "Classic ASP Progress",
    "status.progress.workspaceDiagnostics": "Workspace diagnostics",
    "status.progress.workspaceDiagnosticsIndexed": "Checking indexed workspace diagnostics",
    "status.progress.workspaceDiagnosticsOpenDocuments": "Checking open document diagnostics",
    "status.progress.workspaceIndex": "Loading workspace index",
    "status.progress.workspaceIndexScanFiles": "Scanning workspace files",
    "status.progress.workspaceIndexScanRoot": "Scanning workspace root",
    "status.progress.workspaceIndexWriteCache": "Writing workspace index cache",
    "status.progress.workspacePreviewFiles": "Previewing workspace files",
    "graph.serverUnavailable": "Start the Classic ASP Language Server before building a graph.",
    "graph.noActiveFile": "Open a Classic ASP file before building the current file graph.",
    "graph.noFolder": "Select a folder before building the folder graph.",
    "graph.currentTitle": "Classic ASP: Current File Graph",
    "graph.folderTitle": "Classic ASP: Folder Graph",
    "graph.workspaceTitle": "Classic ASP: Workspace Graph",
    "graph.documentPanelTitle": "Classic ASP Graph: {name}",
    "graph.workspacePanelTitle": "Classic ASP Graph: Workspace",
    "navigationGraph.serverUnavailable":
      "Start the Classic ASP Language Server before building a navigation graph.",
    "navigationGraph.noActiveFile":
      "Open a Classic ASP file before building the current file navigation graph.",
    "navigationGraph.noFolder": "Select a folder before building the folder navigation graph.",
    "navigationGraph.currentTitle": "Classic ASP: Current File Navigation Graph",
    "navigationGraph.folderTitle": "Classic ASP: Folder Navigation Graph",
    "navigationGraph.workspaceTitle": "Classic ASP: Workspace Navigation Graph",
    "navigationGraph.documentPanelTitle": "Classic ASP Navigation Graph: {name}",
    "navigationGraph.workspacePanelTitle": "Classic ASP Navigation Graph: Workspace",
    "excel.serverUnavailable": "Start the Classic ASP Language Server before exporting analysis.",
    "excel.currentTitle": "Classic ASP: Export Current File Analysis",
    "excel.saveLabel": "Export",
    "excel.writeTitle": "Creating Classic ASP analysis workbook",
    "excel.exported": "Classic ASP analysis exported to {file}.",
    "workspaceFiles.serverUnavailable":
      "Start the Classic ASP Language Server before previewing workspace files.",
    "workspaceFiles.settingsSaved": "Classic ASP workspace glob settings saved.",
    "workspaceFiles.viewTitle": "Classic ASP: Project glob files",
    "workspaceFiles.viewPanelTitle": "Classic ASP Files: Project glob",
    "workspaceFiles.workspaceUnavailable":
      "Open a workspace before saving Classic ASP workspace glob settings.",
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
    "status.progress.active": "実行中",
    "status.progress.cancel": "キャンセル",
    "status.progress.cancelling": "キャンセル中",
    "status.progress.analyzingStatusText": "ASP {task}",
    "status.progress.diagnostics": "document diagnostics",
    "status.progress.diagnosticsInclude": "include diagnostics 確認中",
    "status.progress.diagnosticsProjectFast": "fast project diagnostics 確認中",
    "status.progress.diagnosticsProject": "project diagnostics 確認中",
    "status.progress.diagnosticsSyntax": "syntax diagnostics 確認中",
    "status.progress.documentAnalysis": "document 解析",
    "status.progress.documentAnalysisCache": "document 解析 cache 更新中",
    "status.progress.documentAnalysisIncremental": "document 差分解析中",
    "status.progress.documentAnalysisParse": "document parse 中",
    "status.progress.documentAnalysisReady": "document 解析完了",
    "status.progress.excel": "Excel 作成中",
    "status.progress.excelChooseFile": "Excel 出力先選択中",
    "status.progress.excelFile": "Excel ファイルを書き込み中",
    "status.progress.excelFileCommit": "Excel ファイルを仕上げ中",
    "status.progress.excelFileRows": "Excel 行を書き込み中",
    "status.progress.excelFileSheet": "Excel シートを書き込み中",
    "status.progress.excelGraph": "Excel 解析グラフを取得中",
    "status.progress.excelAnalysisContext": "Excel グラフデータを分類中",
    "status.progress.excelAnalysisSummary": "Excel 解析サマリーを作成中",
    "status.progress.excelNormalizeGraph": "Excel グラフ payload を正規化中",
    "status.progress.excelSheet": "Excel シートを作成中",
    "status.progress.excelSheets": "Excel シートを作成中",
    "status.progress.excelWorkbook": "Excel ブックを生成中",
    "status.progress.flowchart": "フローチャートを生成中",
    "status.progress.flowchartBuildPayload": "フローチャート payload を作成中",
    "status.progress.flowchartCanonicalizeSymbols": "フローチャート symbol を正規化中",
    "status.progress.flowchartCollectIncludes": "フローチャート include ツリーを収集中",
    "status.progress.flowchartHydrateDocument": "フローチャート用 VBScript を復元中",
    "status.progress.flowchartIndexDocuments": "フローチャート用ドキュメントをインデックス中",
    "status.progress.flowchartLoadDocument": "フローチャート用ドキュメントを読み込み中",
    "status.progress.navigationGraph": "画面遷移グラフを生成中",
    "status.progress.navigationGraphBuildPayload": "画面遷移グラフ payload を作成中",
    "status.progress.navigationGraphCollectDocuments": "画面遷移グラフ用ドキュメントを収集中",
    "status.progress.navigationGraphExtract": "画面遷移を抽出中",
    "status.progress.navigationGraphResolveIncludes": "画面遷移 include 元を解決中",
    "status.progress.graphAddStructure": "グラフ構造を追加中",
    "status.progress.graphAddUsages": "グラフ使用箇所を追加中",
    "status.progress.graphCanonicalizeSymbols": "グラフ symbol を正規化中",
    "status.progress.graphCheckRelatedIncludes": "関連 include 解析の必要性を確認中",
    "status.progress.graphCollectIncomingIncludes": "取り込み元 include ファイルを収集中",
    "status.progress.graphCollectIncludes": "include ツリーを収集中",
    "status.progress.graphCollectRelatedIncludes": "関連 include ツリーを収集中",
    "status.progress.graphDocument": "現在のファイルのグラフを生成中",
    "status.progress.graphFilterIncomingIncludes": "取り込み元 include ファイルを絞り込み中",
    "status.progress.graphFindIncomingIncludes": "開いている取り込み元 include ファイルを確認中",
    "status.progress.graphFinalize": "グラフを仕上げ中",
    "status.progress.graphFolder": "フォルダーグラフを生成中",
    "status.progress.graphIndexDocuments": "グラフ用ドキュメントをインデックス中",
    "status.progress.graphLoadDocuments": "グラフ用ドキュメントを読み込み中",
    "status.progress.graphOpenDocuments": "開いているドキュメントのグラフを読み込み中",
    "status.progress.graphPrepareDocuments": "グラフ用ドキュメントを準備中",
    "status.progress.graphPrefetchIncludes": "include 先を先読み中",
    "status.progress.graphResolveIncludes": "include パスを解決中",
    "status.progress.graphReverseIncludeIndex": "逆 include インデックスを読み込み中",
    "status.progress.graphSpillIndexes": "グラフ用インデックスをディスクへ書き込み中",
    "status.progress.graphWorkspace": "ワークスペースグラフを生成中",
    "status.progress.graphWorkspaceIndex": "グラフ用ワークスペースインデックスを読み込み中",
    "status.progress.loadingStatusText": "ASP 読み込み中: {task}",
    "status.progress.none": "実行中の Classic ASP タスクはありません。",
    "status.progress.placeholder": "現在の Classic ASP タスク",
    "status.progress.referencesCount": "参照数解析",
    "status.progress.referencesFinalize": "参照数解析を仕上げ中",
    "status.progress.referencesRelatedIncludeTree": "関連 include ツリーの参照数を解析中",
    "status.progress.referencesWorkspace": "ワークスペース参照数を解析中",
    "status.progress.title": "Classic ASP 進行状況",
    "status.progress.workspaceDiagnostics": "ワークスペース診断中",
    "status.progress.workspaceDiagnosticsIndexed": "インデックス済みワークスペース診断を確認中",
    "status.progress.workspaceDiagnosticsOpenDocuments": "開いているドキュメントの診断を確認中",
    "status.progress.workspaceIndex": "ワークスペースインデックスを読み込み中",
    "status.progress.workspaceIndexScanFiles": "ワークスペースファイルをスキャン中",
    "status.progress.workspaceIndexScanRoot": "ワークスペースルートをスキャン中",
    "status.progress.workspaceIndexWriteCache": "ワークスペースインデックスキャッシュを書き込み中",
    "status.progress.workspacePreviewFiles": "ワークスペースファイルをプレビュー中",
    "graph.serverUnavailable":
      "グラフを作成する前に Classic ASP Language Server を起動してください。",
    "graph.noActiveFile":
      "現在のファイルグラフを作成する前に Classic ASP ファイルを開いてください。",
    "graph.noFolder": "フォルダーグラフを作成する前にフォルダーを選択してください。",
    "graph.currentTitle": "Classic ASP: 現在のファイルグラフ",
    "graph.folderTitle": "Classic ASP: フォルダーグラフ",
    "graph.workspaceTitle": "Classic ASP: ワークスペースグラフ",
    "graph.documentPanelTitle": "Classic ASP グラフ: {name}",
    "graph.workspacePanelTitle": "Classic ASP グラフ: ワークスペース",
    "navigationGraph.serverUnavailable":
      "画面遷移グラフを作成する前に Classic ASP Language Server を起動してください。",
    "navigationGraph.noActiveFile":
      "現在のファイルの画面遷移グラフを作成する前に Classic ASP ファイルを開いてください。",
    "navigationGraph.noFolder":
      "フォルダー画面遷移グラフを作成する前にフォルダーを選択してください。",
    "navigationGraph.currentTitle": "Classic ASP: 現在のファイル画面遷移グラフ",
    "navigationGraph.folderTitle": "Classic ASP: フォルダー画面遷移グラフ",
    "navigationGraph.workspaceTitle": "Classic ASP: ワークスペース画面遷移グラフ",
    "navigationGraph.documentPanelTitle": "Classic ASP 画面遷移グラフ: {name}",
    "navigationGraph.workspacePanelTitle": "Classic ASP 画面遷移グラフ: ワークスペース",
    "excel.serverUnavailable":
      "解析を出力する前に Classic ASP Language Server を起動してください。",
    "excel.currentTitle": "Classic ASP: 現在のファイル解析を Excel 出力",
    "excel.saveLabel": "出力",
    "excel.writeTitle": "Classic ASP 解析ブックを作成中",
    "excel.exported": "Classic ASP 解析を {file} に出力しました。",
    "workspaceFiles.serverUnavailable":
      "ワークスペースファイルをプレビューする前に Classic ASP Language Server を起動してください。",
    "workspaceFiles.settingsSaved": "Classic ASP の workspace glob 設定を保存しました。",
    "workspaceFiles.viewTitle": "Classic ASP: プロジェクト glob ファイル",
    "workspaceFiles.viewPanelTitle": "Classic ASP ファイル: プロジェクト glob",
    "workspaceFiles.workspaceUnavailable":
      "Classic ASP の workspace glob 設定を保存する前にワークスペースを開いてください。",
    "flowchart.serverUnavailable":
      "フローチャートを作成する前に Classic ASP Language Server を起動してください。",
    "flowchart.noActiveFile":
      "現在のファイルのフローチャートを作成する前に Classic ASP ファイルを開いてください。",
    "flowchart.currentTitle": "Classic ASP: 現在のファイルフローチャート",
    "flowchart.documentPanelTitle": "Classic ASP フローチャート: {name}",
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
