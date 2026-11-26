import * as vscode from "vscode";
import {
  LanguageClient,
  type LanguageClientOptions,
  type ServerOptions,
  TransportKind,
} from "vscode-languageclient/node";
import { readAspLspSettings } from "./aspConfig.js";
import { resolveMixinServerPath } from "./serverPath.js";

interface LspPosition {
  line: number;
  character: number;
}

interface LspLocation {
  uri: string;
  range: {
    start: LspPosition;
    end: LspPosition;
  };
}

let client: LanguageClient | undefined;
let outputChannel: vscode.LogOutputChannel | undefined;

function toVscodePosition(pos: LspPosition): vscode.Position {
  return new vscode.Position(pos.line, pos.character);
}

async function startClient(context: vscode.ExtensionContext): Promise<void> {
  const config = vscode.workspace.getConfiguration("aspLsp");
  const command = resolveMixinServerPath({
    configuredPath: config.get<string>("server.path", ""),
    extensionPath: context.extensionPath,
  });
  const extraArgs = config.get<string[]>("server.extraArgs", []);
  const args = ["lsp", "--asp-only", ...extraArgs];

  const serverOptions: ServerOptions = {
    run: { command, args, transport: TransportKind.stdio },
    debug: { command, args, transport: TransportKind.stdio },
  };

  const clientOptions: LanguageClientOptions = {
    documentSelector: [
      { scheme: "file", language: "classic-asp" },
      { scheme: "file", language: "vbscript" },
      { scheme: "file", pattern: "**/*.{asp,asa,inc,vbs,vba,bas,cls,frm}" },
    ],
    initializationOptions: {
      aspLsp: readAspLspSettings("aspLsp"),
    },
    outputChannel,
  };

  const nextClient = new LanguageClient(
    "classic-asp-lsp",
    "Classic ASP LSP",
    serverOptions,
    clientOptions,
  );
  client = nextClient;
  await nextClient.start();
}

async function stopClient(): Promise<void> {
  const current = client;
  client = undefined;
  if (current !== undefined) {
    await current.stop();
  }
}

export async function activate(context: vscode.ExtensionContext): Promise<void> {
  outputChannel = vscode.window.createOutputChannel("Classic ASP LSP", { log: true });
  context.subscriptions.push(outputChannel);

  context.subscriptions.push(
    vscode.commands.registerCommand("aspLsp.restartServer", async () => {
      await stopClient();
      await startClient(context);
    }),
    vscode.commands.registerCommand(
      "mixin.showReferences",
      async (uri: string, position: LspPosition | null, locations: LspLocation[]) => {
        const locs = (locations ?? []).map(
          (loc) =>
            new vscode.Location(
              vscode.Uri.parse(loc.uri),
              new vscode.Range(toVscodePosition(loc.range.start), toVscodePosition(loc.range.end)),
            ),
        );
        await vscode.commands.executeCommand(
          "editor.action.showReferences",
          vscode.Uri.parse(uri),
          toVscodePosition(position ?? { line: 0, character: 0 }),
          locs,
        );
      },
    ),
    vscode.workspace.onDidChangeConfiguration(async (event) => {
      if (event.affectsConfiguration("aspLsp.server")) {
        await stopClient();
        await startClient(context);
      } else if (event.affectsConfiguration("aspLsp") && client !== undefined) {
        await client.sendNotification("workspace/didChangeConfiguration", {
          settings: { aspLsp: readAspLspSettings("aspLsp") },
        });
      }
    }),
  );

  await startClient(context);
}

export async function deactivate(): Promise<void> {
  await stopClient();
}
