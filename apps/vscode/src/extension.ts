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

export async function activate(context: vscode.ExtensionContext): Promise<void> {
  outputChannel = vscode.window.createOutputChannel("Classic ASP LSP");
  context.subscriptions.push(
    outputChannel,
    vscode.commands.registerCommand("aspLsp.restartServer", async () => restartServer(context)),
    vscode.commands.registerCommand("aspLsp.reindexWorkspace", async () =>
      client?.sendRequest("workspace/executeCommand", { command: "aspLsp.reindexWorkspace" }),
    ),
    vscode.commands.registerCommand("aspLsp.openOutput", () => outputChannel?.show()),
    vscode.commands.registerCommand("aspLsp.debugIisUrl", async () => debugIisUrl()),
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
      fileEvents: vscode.workspace.createFileSystemWatcher("**/*.{asp,asa,inc}"),
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
}

async function restartServer(context: vscode.ExtensionContext): Promise<void> {
  await client?.stop();
  client = undefined;
  await startClient(context);
}

async function debugIisUrl(): Promise<void> {
  const config = vscode.workspace.getConfiguration("aspLsp");
  const url = config.get<string>("iis.url") || "http://localhost/";
  const webRoot =
    config.get<string>("iis.webRoot") ||
    vscode.workspace.workspaceFolders?.[0]?.uri.fsPath ||
    "${workspaceFolder}";
  const browser = config.get<string>("iis.browser") || "pwa-chrome";
  await vscode.debug.startDebugging(undefined, {
    type: browser,
    request: "launch",
    name: "Debug Classic ASP URL",
    url,
    webRoot,
  });
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
      "$tsc",
    );
    task.group = name === "build" ? vscode.TaskGroup.Build : undefined;
    return task;
  }
}
