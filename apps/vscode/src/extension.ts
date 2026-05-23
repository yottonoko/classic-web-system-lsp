import * as vscode from "vscode";
import {
  LanguageClient,
  TransportKind,
  type LanguageClientOptions,
  type ServerOptions,
} from "vscode-languageclient/node";
import { getServerModulePath } from "./server-path";

let client: LanguageClient | undefined;
let outputChannel: vscode.OutputChannel | undefined;
let statusBarItem: vscode.StatusBarItem | undefined;

export async function activate(context: vscode.ExtensionContext): Promise<void> {
  const localizer = extensionLocalizer();
  outputChannel = vscode.window.createOutputChannel("Classic ASP LSP");
  statusBarItem = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Left, 100);
  statusBarItem.text = "$(code) ASP LSP";
  statusBarItem.tooltip = localizer("status.tooltip");
  statusBarItem.command = "aspLsp.openOutput";
  statusBarItem.show();
  context.subscriptions.push(
    outputChannel,
    statusBarItem,
    vscode.commands.registerCommand("aspLsp.restartServer", async () => restartServer(context)),
    vscode.commands.registerCommand("aspLsp.openOutput", () => outputChannel?.show()),
    vscode.commands.registerCommand("aspLsp.debugIisUrl", async () => debugIisUrl()),
    vscode.commands.registerCommand("aspLsp.debugIisExpressUrl", async () =>
      debugBrowserUrl("iisExpress", extensionLocalizer()("debug.iisExpress.name")),
    ),
    vscode.commands.registerCommand("aspLsp.createLaunchConfig", async () => createLaunchConfig()),
    vscode.tasks.registerTaskProvider("asp-lsp", new AspLspTaskProvider()),
  );
  await startClient(context);
}

async function startClient(context: vscode.ExtensionContext): Promise<void> {
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
  };

  client = new LanguageClient(
    "asp-lsp",
    "Classic ASP Language Server",
    serverOptions,
    clientOptions,
  );
  context.subscriptions.push(client);
  await client.start();
}

export async function deactivate(): Promise<void> {
  await client?.stop();
  client = undefined;
  outputChannel?.dispose();
  outputChannel = undefined;
  statusBarItem?.dispose();
  statusBarItem = undefined;
}

async function restartServer(context: vscode.ExtensionContext): Promise<void> {
  await client?.stop();
  client = undefined;
  await startClient(context);
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
  | "launch.created";

const extensionMessages: Record<"en" | "ja", Record<ExtensionMessageKey, string>> = {
  en: {
    "status.tooltip": "Classic ASP Language Server",
    "debug.iis.name": "Debug Classic ASP URL",
    "debug.iisExpress.name": "Debug Classic ASP IIS Express URL",
    "launch.noWorkspace": "Open a workspace before creating launch.json.",
    "launch.created": "Classic ASP launch.json snippet created.",
  },
  ja: {
    "status.tooltip": "Classic ASP Language Server",
    "debug.iis.name": "Classic ASP URL をデバッグ",
    "debug.iisExpress.name": "Classic ASP IIS Express URL をデバッグ",
    "launch.noWorkspace": "launch.json を作成する前に workspace を開いてください。",
    "launch.created": "Classic ASP の launch.json snippet を作成しました。",
  },
};

function extensionLocalizer(): (key: ExtensionMessageKey) => string {
  const configLocale = vscode.workspace.getConfiguration("aspLsp").get<string>("locale") ?? "auto";
  const locale =
    configLocale === "ja" || (configLocale !== "en" && vscode.env.language.startsWith("ja"))
      ? "ja"
      : "en";
  return (key) => extensionMessages[locale][key] ?? extensionMessages.en[key];
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
