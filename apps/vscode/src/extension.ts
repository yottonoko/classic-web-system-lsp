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
import { showAspGraphWebview, type AspGraphPayload } from "./include-graph-webview";
import { getServerModulePath } from "./server-path";

const maxCrashRestartCount = 4;
const crashRestartWindowMs = 3 * 60 * 1000;
const reindexWorkspaceServerCommand = "aspLsp.server.reindexWorkspace";
const clearCacheServerCommand = "aspLsp.server.clearCache";
const clearDiskCacheServerCommand = "aspLsp.server.clearDiskCache";
const clearProcessCacheServerCommand = "aspLsp.server.clearProcessCache";
const buildGraphServerCommand = "aspLsp.server.buildGraph";
const htmlTagCompleteLookBehind = 2000;
type GraphOpenLocation = "active" | "beside";

let client: LanguageClient | undefined;
let outputChannel: vscode.OutputChannel | undefined;
let statusBarItem: vscode.StatusBarItem | undefined;
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
    vscode.commands.registerCommand("aspLsp.showCurrentFileGraph", async () =>
      showGraph(context, "document"),
    ),
    vscode.commands.registerCommand("aspLsp.showWorkspaceGraph", async () =>
      showGraph(context, "workspace"),
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
  const position = new vscode.Position(change.range.start.line, change.range.start.character + 1);
  if (!couldTriggerHtmlTagCompleteBefore(event.document, position)) {
    return;
  }
  const editor = vscode.window.visibleTextEditors.find(
    (candidate) => candidate.document.uri.toString() === event.document.uri.toString(),
  );
  if (vscode.workspace.getConfiguration("editor", event.document.uri).get("formatOnType")) {
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
    textDocument: { uri: event.document.uri.toString() },
    position: { line: position.line, character: position.character },
    ch: ">",
    options: {
      tabSize: numericEditorOption(editor?.options.tabSize, 2),
      insertSpaces: booleanEditorOption(editor?.options.insertSpaces, true),
    },
  });
  if (!edits || edits.length === 0) {
    return;
  }
  const workspaceEdit = new vscode.WorkspaceEdit();
  for (const edit of edits) {
    workspaceEdit.replace(event.document.uri, toVscodeRange(edit.range), edit.newText);
  }
  const applied = await vscode.workspace.applyEdit(workspaceEdit);
  if (applied && editor) {
    editor.selection = new vscode.Selection(position, position);
  }
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
  await vscode.workspace.applyEdit(workspaceEdit);
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

async function showGraph(
  context: vscode.ExtensionContext,
  scope: "document" | "workspace",
): Promise<void> {
  if (!client) {
    void vscode.window.showWarningMessage(extensionLocalizer()("graph.serverUnavailable"));
    return;
  }
  const activeClient = client;
  const activeDocument = vscode.window.activeTextEditor?.document;
  const uri =
    scope === "document" && activeDocument?.languageId === "classic-asp"
      ? activeDocument.uri.toString()
      : undefined;
  if (scope === "document" && !uri) {
    void vscode.window.showWarningMessage(extensionLocalizer()("graph.noActiveFile"));
    return;
  }
  const title = extensionLocalizer()(
    scope === "document" ? "graph.currentTitle" : "graph.workspaceTitle",
  );
  const payload = await vscode.window.withProgress(
    { location: vscode.ProgressLocation.Notification, title },
    async () =>
      activeClient.sendRequest<AspGraphPayload>("workspace/executeCommand", {
        command: buildGraphServerCommand,
        arguments: [{ scope, uri }],
      }),
  );
  showAspGraphWebview(context, payload, title, graphViewColumn());
}

function graphViewColumn(): vscode.ViewColumn {
  const openLocation = vscode.workspace
    .getConfiguration("aspLsp")
    .get<GraphOpenLocation>("graph.openLocation", "active");
  return openLocation === "beside" ? vscode.ViewColumn.Beside : vscode.ViewColumn.Active;
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
  client = nextClient;
  updateStatusBar();
  try {
    await nextClient.start();
  } catch (error) {
    if (client === nextClient) {
      client = undefined;
    }
    throw error;
  }
  context.subscriptions.push(nextClient);
}

export async function deactivate(): Promise<void> {
  isDeactivating = true;
  await restartPromise?.catch(() => undefined);
  await client?.stop();
  client = undefined;
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
  statusBarItem.text = "$(code) ASP LSP";
  statusBarItem.tooltip = localizer("status.tooltip");
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
  | "debug.iis.name"
  | "debug.iisExpress.name"
  | "launch.noWorkspace"
  | "launch.created"
  | "graph.serverUnavailable"
  | "graph.noActiveFile"
  | "graph.currentTitle"
  | "graph.workspaceTitle";

type ExtensionMessageArgs = Record<string, string>;

const extensionMessages: Record<"en" | "ja", Record<ExtensionMessageKey, string>> = {
  en: {
    "status.tooltip": "Classic ASP Language Server",
    "debug.iis.name": "Debug Classic ASP URL",
    "debug.iisExpress.name": "Debug Classic ASP IIS Express URL",
    "launch.noWorkspace": "Open a workspace before creating launch.json.",
    "launch.created": "Classic ASP launch.json snippet created.",
    "graph.serverUnavailable": "Start the Classic ASP Language Server before building a graph.",
    "graph.noActiveFile": "Open a Classic ASP file before building the current file graph.",
    "graph.currentTitle": "Classic ASP: Current File Graph",
    "graph.workspaceTitle": "Classic ASP: Workspace Graph",
  },
  ja: {
    "status.tooltip": "Classic ASP Language Server",
    "debug.iis.name": "Classic ASP URL をデバッグ",
    "debug.iisExpress.name": "Classic ASP IIS Express URL をデバッグ",
    "launch.noWorkspace": "launch.json を作成する前に workspace を開いてください。",
    "launch.created": "Classic ASP の launch.json snippet を作成しました。",
    "graph.serverUnavailable":
      "graph を作成する前に Classic ASP Language Server を起動してください。",
    "graph.noActiveFile": "current file graph を作成する前に Classic ASP file を開いてください。",
    "graph.currentTitle": "Classic ASP: Current File Graph",
    "graph.workspaceTitle": "Classic ASP: Workspace Graph",
  },
};

function extensionLocalizer(): (key: ExtensionMessageKey, args?: ExtensionMessageArgs) => string {
  const configLocale = vscode.workspace.getConfiguration("aspLsp").get<string>("locale") ?? "auto";
  const locale =
    configLocale === "ja" || (configLocale !== "en" && vscode.env.language.startsWith("ja"))
      ? "ja"
      : "en";
  return (key, args) => {
    let message = extensionMessages[locale][key] ?? extensionMessages.en[key];
    for (const [name, value] of Object.entries(args ?? {})) {
      message = message.replaceAll(`{${name}}`, value);
    }
    return message;
  };
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
