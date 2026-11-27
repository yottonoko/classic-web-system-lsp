import * as fs from "node:fs";
import * as path from "node:path";
import * as vscode from "vscode";
import {
  CloseAction,
  ErrorAction,
  LanguageClient,
  type LanguageClientOptions,
  type ServerOptions,
  TransportKind,
} from "vscode-languageclient/node";
import { readAspLspSettings } from "./aspConfig.js";
import { findAutoCloseTagName } from "./autoCloseTag.js";
import { clearCacheFiles, pruneCacheDir } from "./cacheManager.js";
import { formatIndexProgressStatus, type IndexProgressParams } from "./indexStatus.js";
import { createRestartPolicy } from "./restartPolicy.js";
import { resolveAspLspServer } from "./serverPath.js";

interface LspPosition {
  line: number;
  character: number;
}

interface LspRange {
  start: LspPosition;
  end: LspPosition;
}

interface LspTextEdit {
  range: LspRange;
  newText: string;
}

interface LspLocation {
  uri: string;
  range: LspRange;
}

let client: LanguageClient | undefined;
let outputChannel: vscode.LogOutputChannel | undefined;
let indexStatusBarItem: vscode.StatusBarItem | undefined;
let isStoppingClient = false;
const restartPolicy = createRestartPolicy();

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

  const storageRoot = context.globalStorageUri.fsPath;
  try {
    fs.mkdirSync(path.join(storageRoot, "asp-lsp-cache"), { recursive: true });
  } catch {
    // Ignore if global storage directory is not writable.
  }

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
          MIXIN_HOME: storageRoot,
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
    options: {
      env: {
        ...process.env,
        MIXIN_HOME: storageRoot,
      },
    },
  };
  return { run: exec, debug: exec };
}

async function startClient(context: vscode.ExtensionContext): Promise<void> {
  isStoppingClient = false;
  restartPolicy.reset();
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
      fileEvents: [
        vscode.workspace.createFileSystemWatcher("**/*.{asp,asa,inc,vbs,vba,bas,cls,frm}"),
        vscode.workspace.createFileSystemWatcher("**/.gitignore"),
      ],
    },
    outputChannel,
    errorHandler: {
      error(error, _message, count) {
        outputChannel?.warn(
          `Classic ASP LSP transport error (count=${count ?? 0}): ${error.message}`,
        );
        return { action: ErrorAction.Continue };
      },
      async closed() {
        indexStatusBarItem?.hide();
        if (isStoppingClient) {
          return { action: CloseAction.DoNotRestart, handled: true };
        }
        const { attempt, delayMs } = restartPolicy.nextRestart();
        outputChannel?.warn(
          `Classic ASP LSP server closed unexpectedly (attempt=${attempt}); restarting in ${delayMs}ms.`,
        );
        if (delayMs > 0) {
          await new Promise<void>((resolve) => setTimeout(resolve, delayMs));
        }
        if (isStoppingClient) {
          return { action: CloseAction.DoNotRestart, handled: true };
        }
        return { action: CloseAction.Restart, handled: true };
      },
    },
  };

  const nextClient = new LanguageClient(
    "classic-asp-lsp",
    "Classic ASP LSP",
    serverOptions,
    clientOptions,
  );
  nextClient.onNotification("asp/indexProgress", (payload: IndexProgressParams) => {
    if (!indexStatusBarItem || !payload || typeof payload.state !== "string") {
      return;
    }
    const status = formatIndexProgressStatus(payload, vscode.env.language);
    if (!status.visible) {
      indexStatusBarItem.hide();
      return;
    }
    indexStatusBarItem.text = status.text;
    indexStatusBarItem.tooltip = status.tooltip;
    indexStatusBarItem.show();
  });
  client = nextClient;
  await nextClient.start();
}

async function stopClient(): Promise<void> {
  isStoppingClient = true;
  restartPolicy.reset();
  indexStatusBarItem?.hide();
  const current = client;
  client = undefined;
  if (current !== undefined) {
    await current.stop();
  }
}

async function toggleAspComment(method: string, fallbackCommand: string): Promise<void> {
  const editor = vscode.window.activeTextEditor;
  if (!editor) {
    return;
  }
  if (editor.document.languageId !== "classic-asp" || client === undefined) {
    await vscode.commands.executeCommand(fallbackCommand);
    return;
  }

  const document = editor.document;
  const documentVersion = document.version;
  const selections: LspRange[] = editor.selections.map((sel) => ({
    start: { line: sel.start.line, character: sel.start.character },
    end: { line: sel.end.line, character: sel.end.character },
  }));

  try {
    const edits = await client.sendRequest<LspTextEdit[] | null>(method, {
      textDocument: { uri: document.uri.toString() },
      selections,
    });
    if (document.version !== documentVersion || vscode.window.activeTextEditor !== editor) {
      return;
    }
    if (!Array.isArray(edits)) {
      await vscode.commands.executeCommand(fallbackCommand);
      return;
    }
    if (edits.length === 0) {
      return;
    }
    const workspaceEdit = new vscode.WorkspaceEdit();
    for (const edit of edits) {
      workspaceEdit.replace(
        document.uri,
        new vscode.Range(toVscodePosition(edit.range.start), toVscodePosition(edit.range.end)),
        edit.newText,
      );
    }
    await vscode.workspace.applyEdit(workspaceEdit);
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err);
    outputChannel?.warn(`${method} failed (${message}); falling back to ${fallbackCommand}.`);
    await vscode.commands.executeCommand(fallbackCommand);
  }
}

function resolveCacheDir(context: vscode.ExtensionContext): string {
  return path.join(context.globalStorageUri.fsPath, "asp-lsp-cache");
}

export async function activate(context: vscode.ExtensionContext): Promise<void> {
  outputChannel = vscode.window.createOutputChannel("Classic ASP LSP", { log: true });
  context.subscriptions.push(outputChannel);
  indexStatusBarItem = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Left, 50);
  context.subscriptions.push(indexStatusBarItem);

  context.subscriptions.push(
    vscode.commands.registerCommand("aspLsp.restartServer", async () => {
      await stopClient();
      await startClient(context);
    }),
    vscode.commands.registerCommand("aspLsp.clearCache", async () => {
      await stopClient();
      const removed = clearCacheFiles(resolveCacheDir(context), "all");
      await startClient(context);
      const isJa = vscode.env.language.startsWith("ja");
      const msg = isJa
        ? `Classic ASP LSP: キャッシュを削除しました（${removed} 件）。`
        : `Classic ASP LSP: Cleared ${removed} cache file(s).`;
      void vscode.window.showInformationMessage(msg);
    }),
    vscode.commands.registerCommand("aspLsp.clearIndexCache", async () => {
      await stopClient();
      const removed = clearCacheFiles(resolveCacheDir(context), "index");
      await startClient(context);
      const isJa = vscode.env.language.startsWith("ja");
      const msg = isJa
        ? `Classic ASP LSP: ワークスペースインデックスキャッシュを削除しました（${removed} 件）。`
        : `Classic ASP LSP: Cleared ${removed} workspace index cache file(s).`;
      void vscode.window.showInformationMessage(msg);
    }),
    vscode.commands.registerCommand("aspLsp.toggleLineComment", async () => {
      await toggleAspComment("asp/toggleLineComment", "editor.action.commentLine");
    }),
    vscode.commands.registerCommand("aspLsp.toggleBlockComment", async () => {
      await toggleAspComment("asp/toggleBlockComment", "editor.action.blockComment");
    }),
    vscode.commands.registerCommand(
      "mixin.showReferences",
      async (uri: vscode.Uri | string, position: LspPosition | null, locations: LspLocation[]) => {
        const uriStr = typeof uri === "string" ? uri : uri.toString();
        const activeDoc = vscode.window.activeTextEditor?.document;
        const targetUri =
          activeDoc && activeDoc.uri.toString() === uriStr
            ? activeDoc.uri
            : typeof uri === "string"
              ? vscode.Uri.parse(uri)
              : uri;
        const pos = toVscodePosition(position ?? { line: 0, character: 0 });
        let locs = (locations ?? [])
          .filter((loc) => loc && typeof loc.uri === "string" && loc.range)
          .map(
            (loc) =>
              new vscode.Location(
                loc.uri === uriStr ? targetUri : vscode.Uri.parse(loc.uri),
                new vscode.Range(
                  toVscodePosition(loc.range.start),
                  toVscodePosition(loc.range.end),
                ),
              ),
          );
        const isOnlySelf =
          locs.length === 1 &&
          locs[0].uri.toString() === targetUri.toString() &&
          locs[0].range.start.line === pos.line &&
          locs[0].range.start.character === pos.character;
        if (locs.length === 0 || isOnlySelf) {
          const providerLocs = await vscode.commands.executeCommand<vscode.Location[]>(
            "vscode.executeReferenceProvider",
            targetUri,
            pos,
          );
          if (Array.isArray(providerLocs) && providerLocs.length > 0) {
            locs = providerLocs;
          }
        }
        if (!activeDoc || activeDoc.uri.toString() !== targetUri.toString()) {
          await vscode.window.showTextDocument(targetUri, { preview: false });
        }
        await vscode.commands.executeCommand("editor.action.showReferences", targetUri, pos, locs);
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
    vscode.workspace.onDidChangeTextDocument((event) => {
      if (event.document.languageId !== "classic-asp" || event.contentChanges.length !== 1) {
        return;
      }
      const change = event.contentChanges[0];
      if (change.text !== ">") {
        return;
      }
      const autoClosingTags = vscode.workspace
        .getConfiguration("html", event.document.uri)
        .get<boolean>("autoClosingTags", true);
      if (!autoClosingTags) {
        return;
      }
      const editor = vscode.window.activeTextEditor;
      if (!editor || editor.document !== event.document) {
        return;
      }
      const offsetAfterGt = change.rangeOffset + change.text.length;
      const tagName = findAutoCloseTagName(event.document.getText(), offsetAfterGt);
      if (!tagName) {
        return;
      }
      const pos = event.document.positionAt(offsetAfterGt);
      void editor.insertSnippet(new vscode.SnippetString(`$0</${tagName}>`), pos);
    }),
  );

  try {
    const cacheConfig = vscode.workspace.getConfiguration("aspLsp.cache");
    const extensionVersion =
      typeof context.extension?.packageJSON?.version === "string"
        ? context.extension.packageJSON.version
        : "0.12.10";
    const pruned = pruneCacheDir(resolveCacheDir(context), {
      extensionVersion,
      maxAgeDays: cacheConfig.get<number>("maxAgeDays", 14),
      maxSizeMb: cacheConfig.get<number>("maxSizeMb", 256),
    });
    if (pruned.versionCleared > 0 || pruned.expiredRemoved > 0 || pruned.sizePruned > 0) {
      outputChannel.info(
        `Classic ASP LSP cache auto-cleanup: versionCleared=${pruned.versionCleared}, expiredRemoved=${pruned.expiredRemoved}, sizePruned=${pruned.sizePruned}.`,
      );
    }
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err);
    outputChannel.warn(`Classic ASP LSP cache auto-cleanup skipped: ${message}`);
  }

  await startClient(context);
}

export async function deactivate(): Promise<void> {
  await stopClient();
}
