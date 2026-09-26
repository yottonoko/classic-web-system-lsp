import { showLogAnalysisWebview } from "./log-analysis-webview";
import { mkdir } from "node:fs/promises";
import path from "node:path";
import * as vscode from "vscode";
import {
  LanguageClient,
  type ConfigurationRequest,
  type LanguageClientOptions,
  type ServerOptions,
} from "vscode-languageclient/node";
import {
  showAspFlowchartWebview,
  type AspFlowchartInfoPanelPosition,
  type AspFlowchartLabelMode,
  type AspFlowchartPayload,
  type AspFlowchartWebviewSettings,
  type AspFlowchartWebviewThemeSetting,
} from "./flowchart-webview";
import {
  isAspFlowchartPayload,
  isAspNavigationGraphPayload,
  type AspFlowchartResponse,
} from "./protocol-types";
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
import {
  getServerExecutablePath,
  serverExecutableStatus,
  type ServerExecutableStatus,
} from "./server-path";
import { createLanguageLogOutputChannel } from "./log-output-channel";
import { uriTextForVSCode } from "./uri-encoding";
import { ConfigurationSyncScheduler } from "./configuration-sync";
import {
  extensionLocalizer,
  extensionLocale,
  extensionLocalizerForLocale,
  type ExtensionMessageKey,
} from "./extension-localization";
import { createProgressController } from "./extension-progress";
import { autoCloseAspBlock, autoCloseHtmlTag } from "./extension-auto-close";
import { showReferences, toggleLineComment } from "./extension-editor-actions";
import { SharedDiagnosticCollectionProvider } from "./diagnostic-collection-provider";

const reindexWorkspaceServerCommand = "aspLsp.server.reindexWorkspace";
const clearCacheServerCommand = "aspLsp.server.clearCache";
const clearDiskCacheServerCommand = "aspLsp.server.clearDiskCache";
const clearProcessCacheServerCommand = "aspLsp.server.clearProcessCache";
const buildFlowchartServerCommand = "aspLsp.server.buildFlowchart";
const buildNavigationGraphServerCommand = "aspLsp.server.buildNavigationGraph";
const exportAnalysisExcelServerCommand = "aspLsp.server.exportAnalysisExcel";
const previewWorkspaceFilesServerCommand = "aspLsp.server.previewWorkspaceFiles";
const serverStatusNotificationMethod = "aspLsp/status";
const navigationGraphUpdatedNotificationMethod = "aspLsp/navigationGraphUpdated";
const didChangeConfigurationNotificationMethod = "workspace/didChangeConfiguration";
const configurationSyncDelayMs = 25;
const defaultFlowchartLabelLineLength = 34;
const defaultFlowchartMinZoom = 0.1;
const defaultFlowchartMaxZoom = 4;
type WebviewOpenLocation = "active" | "beside";
type GraphScope = "document" | "folder" | "workspace";
type WebviewThemeSetting = AspFlowchartWebviewThemeSetting & AspNavigationGraphWebviewThemeSetting;
type InfoPanelPosition = AspFlowchartInfoPanelPosition;

interface ScopeCommandRequest {
  scope: GraphScope;
  uri?: string;
  activeDocument?: vscode.TextDocument;
}

type WorkspaceFilesServerPayload = Omit<WorkspaceFilesPayload, "locale" | "settings">;

let client: LanguageClient | undefined;
let outputChannel: vscode.LogOutputChannel | undefined;
let statusBarItem: vscode.StatusBarItem | undefined;
let statusNotificationSubscription: vscode.Disposable | undefined;
let navigationGraphUpdatedNotificationSubscription: vscode.Disposable | undefined;
let fileSystemWatcher: vscode.FileSystemWatcher | undefined;
const navigationGraphPanelsByKey = new Map<
  string,
  {
    panel: vscode.WebviewPanel;
    locale: AspNavigationGraphLocale;
    settings: AspNavigationGraphWebviewSettings;
  }
>();
let restartPromise: Promise<void> | undefined;
let configurationSyncScheduler: ConfigurationSyncScheduler | undefined;
let diagnosticCollectionProvider: SharedDiagnosticCollectionProvider | undefined;
let isDeactivating = false;
let isManualRestarting = false;

class ServerStartupError extends Error {
  constructor(
    readonly executablePath: string,
    readonly status: ServerExecutableStatus | "startFailed",
    cause?: unknown,
  ) {
    super(errorMessage(cause ?? "server executable is unavailable"));
    this.name = "ServerStartupError";
  }

  get missing(): boolean {
    return this.status === "missing";
  }
}

const progressController = createProgressController({
  getClient: () => client,
  getStatusBarItem: () => statusBarItem,
  isDeactivating: () => isDeactivating,
  isManualRestarting: () => isManualRestarting,
  localize: extensionLocalizer,
  locale: extensionLocale,
  baseNameFromPath,
  baseNameFromUri,
});

export async function activate(context: vscode.ExtensionContext): Promise<void> {
  isDeactivating = false;
  diagnosticCollectionProvider = new SharedDiagnosticCollectionProvider();
  outputChannel = createLanguageLogOutputChannel("Classic ASP LSP", "asp-lsp-output");
  statusBarItem = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Left, 100);
  statusBarItem.command = "aspLsp.showProgressDetails";
  progressController.updateStatusBar();
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
    vscode.commands.registerCommand("aspLsp.analyzeDebugLog", () =>
      showLogAnalysisWebview(context, extensionLocale()),
    ),
    vscode.commands.registerCommand("aspLsp.showProgressDetails", async () =>
      progressController.showProgressDetails(),
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
    vscode.commands.registerCommand("aspLsp.toggleLineComment", async () =>
      toggleLineComment(client, extensionLocalizer()),
    ),
    vscode.workspace.onDidChangeTextDocument((event) => {
      void autoCloseHtmlTag(event, client);
      void autoCloseAspBlock(event);
      if (
        vscode.window.activeTextEditor?.document.uri.toString() === event.document.uri.toString()
      ) {
        progressController.updateStatusBar();
      }
    }),
    vscode.window.onDidChangeActiveTextEditor(() => progressController.updateStatusBar()),
    vscode.workspace.onDidChangeConfiguration((event) => {
      if (event.affectsConfiguration("aspLsp")) {
        configurationSyncScheduler?.schedule();
      }
    }),
    vscode.workspace.onDidGrantWorkspaceTrust(() => configurationSyncScheduler?.schedule()),
  );
  configurationSyncScheduler = new ConfigurationSyncScheduler(
    configurationSyncDelayMs,
    synchronizeAspLspConfiguration,
    (error) => outputChannel?.error("Failed to synchronize Classic ASP LSP settings", error),
  );
  context.subscriptions.push(configurationSyncScheduler);
  try {
    await startClient(context);
  } catch (error) {
    await reportServerStartupError(error, context);
  }
}

async function executeServerCommand(command: string, argument?: unknown): Promise<void> {
  const activeClient = client;
  if (!activeClient) {
    await vscode.window.showWarningMessage(extensionLocalizer()("maintenance.serverUnavailable"));
    return;
  }
  try {
    const result = await activeClient.sendRequest<unknown>("workspace/executeCommand", {
      command,
      arguments: argument === undefined ? undefined : [argument],
    });
    if (isRecord(result) && result.ok === false) {
      const message = typeof result.message === "string" ? result.message : "unknown error";
      await vscode.window.showErrorMessage(
        extensionLocalizer()("maintenance.commandFailed", { error: message }),
      );
      return;
    }
    const messageKey = maintenanceCompletionMessageKey(command);
    await vscode.window.showInformationMessage(
      extensionLocalizer()(result === undefined ? "maintenance.noOp" : messageKey),
    );
  } catch (error) {
    await vscode.window.showErrorMessage(
      extensionLocalizer()("maintenance.commandFailed", { error: errorMessage(error) }),
    );
  }
}

function maintenanceCompletionMessageKey(
  command: string,
):
  | "maintenance.reindexRequested"
  | "maintenance.clearCacheCompleted"
  | "maintenance.clearDiskCacheCompleted"
  | "maintenance.clearProcessCacheCompleted" {
  switch (command) {
    case reindexWorkspaceServerCommand:
      return "maintenance.reindexRequested";
    case clearDiskCacheServerCommand:
      return "maintenance.clearDiskCacheCompleted";
    case clearProcessCacheServerCommand:
      return "maintenance.clearProcessCacheCompleted";
    default:
      return "maintenance.clearCacheCompleted";
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

function showLocalizedError(key: ExtensionMessageKey, error: unknown): void {
  void vscode.window.showErrorMessage(extensionLocalizer()(key, { error: errorMessage(error) }));
}

function showLocalizedErrorForLocale(
  locale: "en" | "ja",
  key: ExtensionMessageKey,
  error: unknown,
): void {
  void vscode.window.showErrorMessage(
    extensionLocalizerForLocale(locale)(key, { error: errorMessage(error) }),
  );
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
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
  const result = await safeLoadFlowchartPayload(uri);
  if (!result) {
    return;
  }
  showAspFlowchartWebview(
    context,
    result.payload,
    result.title,
    webviewViewColumn("flowchart.openLocation"),
    extensionLocale(),
    flowchartWebviewSettings(),
    loadFlowchartPayload,
  );
}

async function exportFlowchart(selectedUri?: vscode.Uri): Promise<void> {
  const uri = currentClassicAspUri(selectedUri);
  if (!uri) {
    void vscode.window.showWarningMessage(extensionLocalizer()("flowchart.noActiveFile"));
    return;
  }
  const result = await safeLoadFlowchartPayload(uri);
  if (!result) {
    return;
  }
  const { payload } = result;
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
  const payload = await progressController.withServerTaskProgress(
    { location: vscode.ProgressLocation.Notification, title, cancellable: true },
    { labelPrefixes: ["flowchart."] },
    async (_progress, token) => {
      const response = await client!.sendRequest<AspFlowchartResponse>(
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
      );
      if (!isAspFlowchartPayload(response)) {
        throw new Error(extensionLocalizer()("flowchart.incomplete"));
      }
      return response;
    },
  );
  return {
    payload,
    title: flowchartPanelTitle(payload),
  };
}

async function safeLoadFlowchartPayload(
  uri: string | vscode.Uri,
  labelMode?: AspFlowchartLabelMode,
): Promise<{ payload: AspFlowchartPayload; title: string } | undefined> {
  try {
    return await loadFlowchartPayload(uri, labelMode);
  } catch (error) {
    showLocalizedError("flowchart.openFailed", error);
    return undefined;
  }
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
  let payload: AspNavigationGraphPayload;
  try {
    payload = await loadNavigationGraphPayload(request);
  } catch (error) {
    showLocalizedError("navigationGraph.openFailed", error);
    return;
  }
  const locale = extensionLocale();
  const settings: AspNavigationGraphWebviewSettings = { theme: webviewThemeSetting() };
  const panel = showAspNavigationGraphWebview(
    context,
    payload,
    navigationGraphPanelTitle(payload, request.activeDocument),
    webviewViewColumn("navigationGraph.openLocation"),
    locale,
    settings,
  );
  const key = navigationGraphPanelKey(payload.scope, payload.rootUri ?? request.uri);
  navigationGraphPanelsByKey.set(key, { panel, locale, settings });
  panel.onDidDispose(() => {
    if (navigationGraphPanelsByKey.get(key)?.panel === panel) {
      navigationGraphPanelsByKey.delete(key);
    }
  });
}

function navigationGraphCommandRequest(
  scope: GraphScope,
  selectedUri?: vscode.Uri,
): ScopeCommandRequest | undefined {
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
  request: ScopeCommandRequest,
): Promise<AspNavigationGraphPayload> {
  if (!client) {
    throw new Error(extensionLocalizer()("navigationGraph.serverUnavailable"));
  }
  const activeClient = client;
  return progressController.withServerTaskProgress(
    {
      location: vscode.ProgressLocation.Notification,
      title: extensionLocalizer()(navigationGraphTitleKey(request.scope)),
      cancellable: true,
    },
    { labelPrefixes: ["navigationGraph."] },
    async (_progress, token) => {
      const response = await activeClient.sendRequest<unknown>(
        "workspace/executeCommand",
        {
          command: buildNavigationGraphServerCommand,
          arguments: [
            {
              scope: request.scope,
              uri: request.uri,
            },
          ],
        },
        token,
      );
      if (!isAspNavigationGraphPayload(response)) {
        throw new Error(extensionLocalizer()("navigationGraph.incomplete"));
      }
      return response;
    },
  );
}

function handleNavigationGraphUpdatedNotification(payload: unknown): void {
  if (!isAspNavigationGraphPayload(payload)) {
    return;
  }
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
  ).catch((error) =>
    showLocalizedErrorForLocale(tracked.locale, "navigationGraph.updateFailed", error),
  );
}

function navigationGraphPanelKey(scope: GraphScope, uri: string | undefined): string {
  return `${scope}:${uri ?? ""}`;
}

function disposeNavigationGraphPanels(): void {
  const panels = [...navigationGraphPanelsByKey.values()];
  navigationGraphPanelsByKey.clear();
  for (const tracked of panels) {
    tracked.panel.dispose();
  }
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
  let payload: WorkspaceFilesServerPayload;
  try {
    payload = await requestWorkspaceFilesPreview(undefined, "workspaceFiles.viewTitle");
  } catch (error) {
    showLocalizedError("workspaceFiles.previewFailed", error);
    return;
  }
  showWorkspaceFilesWebview(
    context,
    payload,
    extensionLocalizer()("workspaceFiles.viewPanelTitle"),
    webviewViewColumn("workspaceFiles.openLocation"),
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
  return progressController.withServerTaskProgress(
    {
      location: vscode.ProgressLocation.Notification,
      title: extensionLocalizer()(titleKey),
      cancellable: true,
    },
    { exactLabels: ["workspace.previewFiles"] },
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
  await exportAnalysisExcel(vscode.Uri.parse(uriTextForVSCode(request.selectedUri)), {
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
  const request = documentAnalysisRequest(selectedUri);
  if (!request) {
    return;
  }
  const activeClient = client;
  const includeRelatedIncludeTreesForUnresolved = relatedIncludeTreeAnalysisSetting("excel");
  const skipTypeInference = excelSkipTypeInferenceSetting();
  const workspaceFilterRequest = workspaceFilter ?? defaultWorkspaceFilesPreviewRequest();
  const exportStatus = progressController.beginExtensionProgressTask(
    "analyzing",
    "excel.chooseFile",
    {
      current: 0,
      total: 4,
      detail: request.uri ? progressController.progressDetailFromUriText(request.uri) : undefined,
    },
  );
  exportStatus.update({
    current: 1,
    label: "excel.chooseFile",
    detail: request.uri ? progressController.progressDetailFromUriText(request.uri) : undefined,
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
    await progressController.withServerTaskProgress(
      {
        location: vscode.ProgressLocation.Notification,
        title: extensionLocalizer()("excel.writeTitle"),
        cancellable: true,
      },
      { labelPrefixes: ["excel."] },
      async (_progress, token) => {
        await activeClient.sendRequest(
          "workspace/executeCommand",
          {
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
              },
            ],
          },
          token,
        );
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

function documentAnalysisRequest(selectedUri?: vscode.Uri): ScopeCommandRequest | undefined {
  const activeDocument = vscode.window.activeTextEditor?.document;
  const selectedUriText = selectedUri?.toString();
  const uri =
    selectedUriText ??
    (activeDocument?.languageId === "classic-asp" ? activeDocument.uri.toString() : undefined);
  if (!uri) {
    void vscode.window.showWarningMessage(extensionLocalizer()("excel.noActiveFile"));
    return undefined;
  }
  return { scope: "document", uri, activeDocument };
}

function relatedIncludeTreeAnalysisSetting(scope: "excel"): boolean {
  return vscode.workspace
    .getConfiguration("aspLsp")
    .get<boolean>(`${scope}.includeRelatedIncludeTreesForUnresolved`, true);
}

function excelSkipTypeInferenceSetting(): boolean {
  return vscode.workspace.getConfiguration("aspLsp").get<boolean>("excel.skipTypeInference", false);
}

function webviewViewColumn(
  setting:
    | "flowchart.openLocation"
    | "navigationGraph.openLocation"
    | "workspaceFiles.openLocation",
): vscode.ViewColumn {
  const openLocation = vscode.workspace
    .getConfiguration("aspLsp")
    .get<WebviewOpenLocation>(setting, "active");
  return openLocation === "beside" ? vscode.ViewColumn.Beside : vscode.ViewColumn.Active;
}

function flowchartWebviewSettings(): AspFlowchartWebviewSettings {
  const config = vscode.workspace.getConfiguration("aspLsp");
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
  key: "flowchart.infoPanelPosition",
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
  const root = targetUri?.startsWith("file://")
    ? vscode.Uri.parse(uriTextForVSCode(targetUri))
    : undefined;
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
  const activeDiagnosticCollectionProvider = (diagnosticCollectionProvider ??=
    new SharedDiagnosticCollectionProvider());
  const serverPath = getServerExecutablePath(context);
  const executableStatus = serverExecutableStatus(serverPath);
  if (executableStatus !== "ready") {
    throw new ServerStartupError(serverPath.command, executableStatus);
  }
  await mkdir(context.globalStorageUri.fsPath, { recursive: true });
  const defaultDebugLogFile = path.join(context.globalStorageUri.fsPath, "asp-lsp-debug.log");
  const serverEnv = {
    ...process.env,
    ASP_LSP_DEFAULT_DEBUG_LOG_FILE: defaultDebugLogFile,
  };
  const serverOptions: ServerOptions = {
    run: { command: serverPath.command, args: ["--stdio"], options: { env: serverEnv } },
    debug: { command: serverPath.command, args: ["--stdio"], options: { env: serverEnv } },
  };
  const nextFileSystemWatcher = vscode.workspace.createFileSystemWatcher(
    "**/*.{asp,asa,inc,vbs,js,jsx,mjs,cjs,ts,tsx,mts,cts,d.ts}",
  );
  fileSystemWatcher?.dispose();
  fileSystemWatcher = nextFileSystemWatcher;
  const clientOptions: LanguageClientOptions = {
    documentSelector: [
      { scheme: "file", language: "classic-asp" },
      { scheme: "file", language: "vbscript" },
    ],
    outputChannel,
    diagnosticCollectionProvider: activeDiagnosticCollectionProvider,
    synchronize: {
      fileEvents: nextFileSystemWatcher,
    },
    middleware: {
      workspace: {
        configuration: workspaceConfigurationMiddleware,
      },
    },
    errorHandler: progressController.createLanguageClientErrorHandler(),
  };

  const nextClient = new LanguageClient(
    "asp-lsp",
    "Classic ASP Language Server",
    serverOptions,
    clientOptions,
  );
  statusNotificationSubscription?.dispose();
  navigationGraphUpdatedNotificationSubscription?.dispose();
  statusNotificationSubscription = nextClient.onNotification(
    serverStatusNotificationMethod,
    progressController.handleServerStatusNotification,
  );
  navigationGraphUpdatedNotificationSubscription = nextClient.onNotification(
    navigationGraphUpdatedNotificationMethod,
    handleNavigationGraphUpdatedNotification,
  );
  client = nextClient;
  progressController.resetServerState();
  try {
    await nextClient.start();
    configurationSyncScheduler?.cancelPending();
    try {
      await synchronizeAspLspConfiguration(nextClient);
    } catch (error) {
      outputChannel?.error("Failed to synchronize Classic ASP LSP settings", error);
    }
  } catch (error) {
    statusNotificationSubscription?.dispose();
    statusNotificationSubscription = undefined;
    navigationGraphUpdatedNotificationSubscription?.dispose();
    navigationGraphUpdatedNotificationSubscription = undefined;
    if (client === nextClient) {
      client = undefined;
    }
    if (fileSystemWatcher === nextFileSystemWatcher) {
      nextFileSystemWatcher.dispose();
      fileSystemWatcher = undefined;
    }
    try {
      await nextClient.dispose();
    } catch {
      // Preserve the original startup failure.
    }
    progressController.resetServerState();
    throw new ServerStartupError(serverPath.command, "startFailed", error);
  }
}

async function reportServerStartupError(
  error: unknown,
  context: vscode.ExtensionContext,
): Promise<void> {
  const startupError =
    error instanceof ServerStartupError
      ? error
      : new ServerStartupError(getServerExecutablePath(context).command, "startFailed", error);
  const command =
    process.platform === "win32"
      ? "go build -o bin\\asp-lsp-go.exe .\\cmd\\asp-lsp-go"
      : "go build -o bin/asp-lsp-go ./cmd/asp-lsp-go";
  const key =
    startupError.status === "missing"
      ? "server.binaryNotFound"
      : startupError.status === "invalid"
        ? "server.binaryInvalid"
        : "server.startFailed";
  const message = extensionLocalizer()(key, {
    path: startupError.executablePath,
    command,
    error: startupError.message,
  });
  outputChannel?.error(message);
  const choice = await vscode.window.showErrorMessage(
    message,
    extensionLocalizer()("server.openOutput"),
  );
  if (choice === extensionLocalizer()("server.openOutput")) {
    outputChannel?.show();
  }
}

export async function deactivate(): Promise<void> {
  isDeactivating = true;
  configurationSyncScheduler?.dispose();
  configurationSyncScheduler = undefined;
  await restartPromise?.catch(() => undefined);
  const activeClient = client;
  client = undefined;
  fileSystemWatcher?.dispose();
  fileSystemWatcher = undefined;
  const activeDiagnosticCollectionProvider = diagnosticCollectionProvider;
  diagnosticCollectionProvider = undefined;
  try {
    await activeClient?.dispose();
  } finally {
    activeDiagnosticCollectionProvider?.dispose();
  }
  statusNotificationSubscription?.dispose();
  statusNotificationSubscription = undefined;
  navigationGraphUpdatedNotificationSubscription?.dispose();
  navigationGraphUpdatedNotificationSubscription = undefined;
  disposeNavigationGraphPanels();
  progressController.resetServerState();
  progressController.clearExtensionProgressTasks();
  outputChannel?.dispose();
  outputChannel = undefined;
  statusBarItem?.dispose();
  statusBarItem = undefined;
}

export async function synchronizeAspLspConfiguration(targetClient = client): Promise<void> {
  if (!targetClient) {
    return;
  }
  const configuredAspLsp = vscode.workspace.getConfiguration().get<unknown>("aspLsp", {});
  const aspLsp = workspaceSafeAspLspConfiguration(configuredAspLsp);
  await targetClient.sendNotification(didChangeConfigurationNotificationMethod, {
    settings: { aspLsp },
  });
}

export const workspaceConfigurationMiddleware: ConfigurationRequest.MiddlewareSignature = async (
  params,
  token,
  next,
) => {
  const result = await next(params, token);
  if (vscode.workspace.isTrusted || !Array.isArray(result)) {
    return result;
  }
  return result.map((value, index) =>
    params.items[index]?.section === "aspLsp"
      ? workspaceSafeAspLspConfiguration(value, false)
      : value,
  );
};

export function workspaceSafeAspLspConfiguration(
  value: unknown,
  isTrusted = vscode.workspace.isTrusted,
): unknown {
  if (isTrusted) {
    return value;
  }
  if (!configurationRecord(value)) {
    return emptyWorkspaceSafeAspLspConfiguration();
  }
  const safe = deepCloneConfigurationValue(value) as Record<string, unknown>;
  return applyWorkspaceSafeAspLspClears(safe);
}

function emptyWorkspaceSafeAspLspConfiguration(): Record<string, unknown> {
  return applyWorkspaceSafeAspLspClears({});
}

function applyWorkspaceSafeAspLspClears(safe: Record<string, unknown>): Record<string, unknown> {
  safe.includePaths = [];
  safe.virtualRoot = "";
  safe.virtualRoots = [];
  const cache = configurationObject(safe.cache);
  cache.directory = "";
  safe.cache = cache;
  const debug = configurationObject(safe.debug);
  const logFile = configurationObject(debug.logFile);
  logFile.enabled = false;
  logFile.path = "";
  debug.logFile = logFile;
  safe.debug = debug;
  return safe;
}

function configurationRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function configurationObject(value: unknown): Record<string, unknown> {
  return configurationRecord(value) ? value : {};
}

function deepCloneConfigurationValue(value: unknown): unknown {
  if (Array.isArray(value)) {
    return value.map(deepCloneConfigurationValue);
  }
  if (value !== null && typeof value === "object") {
    const clone: Record<string, unknown> = {};
    for (const [key, nestedValue] of Object.entries(value)) {
      clone[key] = deepCloneConfigurationValue(nestedValue);
    }
    return clone;
  }
  return value;
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
    configurationSyncScheduler?.cancelPending();
    const activeClient = client;
    client = undefined;
    fileSystemWatcher?.dispose();
    fileSystemWatcher = undefined;
    await activeClient?.dispose();
    statusNotificationSubscription?.dispose();
    statusNotificationSubscription = undefined;
    navigationGraphUpdatedNotificationSubscription?.dispose();
    navigationGraphUpdatedNotificationSubscription = undefined;
    disposeNavigationGraphPanels();
    progressController.resetServerState();
    progressController.resetCrashRestartHistory();
  } finally {
    isManualRestarting = false;
  }
  try {
    await startClient(context);
  } catch (error) {
    await reportServerStartupError(error, context);
  }
}
