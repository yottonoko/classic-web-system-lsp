import * as vscode from "vscode";
import { getClassicAspLineCommentEdits, type AspAnalysisBackendInfo } from "@asp-lsp/core";
import {
  CloseAction,
  ErrorAction,
  LanguageClient,
  TransportKind,
  type ErrorHandler,
  type LanguageClientOptions,
  type ServerOptions,
} from "vscode-languageclient/node";
import { getServerModulePath } from "./server-path";

const maxCrashRestartCount = 4;
const crashRestartWindowMs = 3 * 60 * 1000;
const reindexWorkspaceServerCommand = "aspLsp.server.reindexWorkspace";
const clearCacheServerCommand = "aspLsp.server.clearCache";
const backendStatusMethod = "aspLsp/backendStatus";

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
  updateBackendStatus();
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
    vscode.commands.registerCommand("aspLsp.openOutput", () => outputChannel?.show()),
    vscode.commands.registerCommand("aspLsp.showReferences", async (uri, position, locations) =>
      showReferences(uri, position, locations),
    ),
    vscode.commands.registerCommand("aspLsp.toggleLineComment", async () => toggleLineComment()),
    vscode.commands.registerCommand("aspLsp.debugIisUrl", async () => debugIisUrl()),
    vscode.commands.registerCommand("aspLsp.debugIisExpressUrl", async () =>
      debugBrowserUrl("iisExpress", extensionLocalizer()("debug.iisExpress.name")),
    ),
    vscode.commands.registerCommand("aspLsp.createLaunchConfig", async () => createLaunchConfig()),
    vscode.tasks.registerTaskProvider("asp-lsp", new AspLspTaskProvider()),
    vscode.workspace.onDidChangeConfiguration(async (event) => {
      if (event.affectsConfiguration("aspLsp.analysisBackend")) {
        await restartServer(context);
      }
    }),
  );
  await startClient(context);
}

async function executeServerCommand(command: string): Promise<unknown> {
  return client?.sendRequest("workspace/executeCommand", { command });
}

async function startClient(context: vscode.ExtensionContext): Promise<void> {
  if (isDeactivating) {
    return;
  }
  const serverModule = getServerModulePath(context);
  const env = {
    ...process.env,
    ASP_LSP_ANALYSIS_BACKEND: configuredAnalysisBackend(),
  };
  const serverOptions: ServerOptions = {
    run: { module: serverModule, transport: TransportKind.ipc, options: { env } },
    debug: {
      module: serverModule,
      transport: TransportKind.ipc,
      options: { execArgv: ["--nolazy", "--inspect=6009"], env },
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
  const backendStatusSubscription = nextClient.onNotification(
    backendStatusMethod,
    (status: unknown) => {
      updateBackendStatus(asBackendStatus(status));
    },
  );
  client = nextClient;
  updateBackendStatus();
  try {
    await nextClient.start();
  } catch (error) {
    if (client === nextClient) {
      client = undefined;
    }
    throw error;
  }
  void requestBackendStatus(nextClient);
  context.subscriptions.push(nextClient, backendStatusSubscription);
}

function configuredAnalysisBackend(): string {
  const value = vscode.workspace.getConfiguration("aspLsp").get("analysisBackend");
  return value === "native" || value === "typescript" ? value : "auto";
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
    updateBackendStatus();
    crashRestartTimestamps = [];
  } finally {
    isManualRestarting = false;
  }
  await startClient(context);
}

async function requestBackendStatus(languageClient: LanguageClient): Promise<void> {
  try {
    const status = asBackendStatus(await languageClient.sendRequest(backendStatusMethod, null));
    if (client === languageClient) {
      updateBackendStatus(status);
    }
  } catch {
    if (client === languageClient) {
      updateBackendStatus();
    }
  }
}

function updateBackendStatus(status?: AspAnalysisBackendInfo): void {
  if (!statusBarItem) {
    return;
  }
  const localizer = extensionLocalizer();
  if (!status || status.reason === "not loaded") {
    statusBarItem.text = "$(code) ASP LSP: ...";
    statusBarItem.tooltip = [localizer("status.tooltip"), localizer("status.backend.pending")].join(
      "\n",
    );
    return;
  }
  const isNative = status.backend === "native";
  statusBarItem.text = isNative ? "$(code) ASP LSP: Native" : "$(code) ASP LSP: TS";
  const backendLine = localizer(isNative ? "status.backend.native" : "status.backend.typescript", {
    engine: status.engine,
  });
  statusBarItem.tooltip = [
    localizer("status.tooltip"),
    backendLine,
    ...(status.reason ? [localizer("status.backend.reason", { reason: status.reason })] : []),
  ].join("\n");
}

function asBackendStatus(value: unknown): AspAnalysisBackendInfo | undefined {
  if (!value || typeof value !== "object") {
    return undefined;
  }
  const candidate = value as Partial<AspAnalysisBackendInfo>;
  if (
    (candidate.backend === "native" || candidate.backend === "typescript-fallback") &&
    typeof candidate.engine === "string"
  ) {
    return {
      backend: candidate.backend,
      engine: candidate.engine,
      version: typeof candidate.version === "string" ? candidate.version : undefined,
      reason: typeof candidate.reason === "string" ? candidate.reason : undefined,
    };
  }
  return undefined;
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
  | "status.backend.pending"
  | "status.backend.native"
  | "status.backend.typescript"
  | "status.backend.reason"
  | "debug.iis.name"
  | "debug.iisExpress.name"
  | "launch.noWorkspace"
  | "launch.created";

type ExtensionMessageArgs = Record<string, string>;

const extensionMessages: Record<"en" | "ja", Record<ExtensionMessageKey, string>> = {
  en: {
    "status.tooltip": "Classic ASP Language Server",
    "status.backend.pending": "Backend: detecting",
    "status.backend.native": "Backend: Native ({engine})",
    "status.backend.typescript": "Backend: TypeScript ({engine})",
    "status.backend.reason": "Reason: {reason}",
    "debug.iis.name": "Debug Classic ASP URL",
    "debug.iisExpress.name": "Debug Classic ASP IIS Express URL",
    "launch.noWorkspace": "Open a workspace before creating launch.json.",
    "launch.created": "Classic ASP launch.json snippet created.",
  },
  ja: {
    "status.tooltip": "Classic ASP Language Server",
    "status.backend.pending": "Backend: 判定中",
    "status.backend.native": "Backend: Native ({engine})",
    "status.backend.typescript": "Backend: TypeScript ({engine})",
    "status.backend.reason": "Reason: {reason}",
    "debug.iis.name": "Classic ASP URL をデバッグ",
    "debug.iisExpress.name": "Classic ASP IIS Express URL をデバッグ",
    "launch.noWorkspace": "launch.json を作成する前に workspace を開いてください。",
    "launch.created": "Classic ASP の launch.json snippet を作成しました。",
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
      this.task("package VSIX (no native)", "pnpm", ["run", "package:vsix:no-native"]),
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
