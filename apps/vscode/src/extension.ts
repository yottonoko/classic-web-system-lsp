import * as path from "node:path";
import * as vscode from "vscode";
import {
  LanguageClient,
  type LanguageClientOptions,
  type ServerOptions,
  TransportKind,
} from "vscode-languageclient/node";
import { readAspLspSettings } from "./aspConfig.js";
import { resolveAspLspServer } from "./serverPath.js";

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

function buildServerOptions(context: vscode.ExtensionContext): ServerOptions {
  const config = vscode.workspace.getConfiguration("aspLsp");
  const resolved = resolveAspLspServer({
    configuredPath: config.get<string>("server.path", ""),
    extraArgs: config.get<string[]>("server.extraArgs", []),
    extensionPath: context.extensionPath,
  });

  if (resolved.kind === "wasm") {
    const runnerPath = path.join(context.extensionPath, "dist", "wasm-runner.js");
    const preopens = (vscode.workspace.workspaceFolders ?? [])
      .map((f) => f.uri.fsPath)
      .join(path.delimiter);
    const exec = {
      command: process.execPath,
      args: ["--no-warnings", runnerPath, resolved.wasmPath, ...resolved.args],
      transport: TransportKind.stdio,
      options: {
        env: {
          ...process.env,
          ASP_LSP_WASI_PREOPENS: preopens,
        },
      },
    };
    return { run: exec, debug: exec };
  }

  const exec = {
    command: resolved.command,
    args: resolved.args,
    transport: TransportKind.stdio,
  };
  return { run: exec, debug: exec };
}

async function startClient(context: vscode.ExtensionContext): Promise<void> {
  const serverOptions = buildServerOptions(context);

  const clientOptions: LanguageClientOptions = {
    documentSelector: [
      { scheme: "file", language: "classic-asp" },
      { scheme: "file", language: "vbscript" },
      { scheme: "file", pattern: "**/*.{asp,asa,inc,vbs,vba,bas,cls,frm}" },
    ],
    initializationOptions: {
      aspLsp: readAspLspSettings("aspLsp"),
    },
    synchronize: {
      // Notify the server on file create/change/delete so network-drive & DB caches invalidate event-driven.
      fileEvents: vscode.workspace.createFileSystemWatcher(
        "**/*.{asp,asa,inc,vbs,vba,bas,cls,frm}",
      ),
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
