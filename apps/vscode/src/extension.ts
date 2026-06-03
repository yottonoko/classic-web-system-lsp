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
import { getServerLaunchPath, type ServerLaunchPath } from "./server-path";

const maxCrashRestartCount = 4;
const crashRestartWindowMs = 3 * 60 * 1000;
const reindexWorkspaceServerCommand = "aspLsp.server.reindexWorkspace";
const clearCacheServerCommand = "aspLsp.server.clearCache";
const clearDiskCacheServerCommand = "aspLsp.server.clearDiskCache";
const clearProcessCacheServerCommand = "aspLsp.server.clearProcessCache";
const backendStatusMethod = "aspLsp/backendStatus";
const viewFileTextMethod = "rust-analyzer/viewFileText";
const viewSyntaxTreeMethod = "rust-analyzer/viewSyntaxTree";
const analyzerStatusMethod = "rust-analyzer/analyzerStatus";
const memoryUsageMethod = "rust-analyzer/memoryUsage";
const matchingBraceMethod = "rust-analyzer/matchingBrace";
const parentModuleMethod = "experimental/parentModule";
const childModulesMethod = "experimental/childModules";
const joinLinesMethod = "experimental/joinLines";
const onEnterMethod = "experimental/onEnter";
const moveItemMethod = "experimental/moveItem";
const externalDocsMethod = "experimental/externalDocs";
const ssrMethod = "experimental/ssr";
const introspectionScheme = "asp-lsp-introspection";

interface AspAnalysisBackendInfo {
  backend: "rust";
  engine: string;
  version?: string;
  reason?: string;
}

let client: LanguageClient | undefined;
let outputChannel: vscode.OutputChannel | undefined;
let statusBarItem: vscode.StatusBarItem | undefined;
let restartPromise: Promise<void> | undefined;
let isDeactivating = false;
let isManualRestarting = false;
let crashRestartTimestamps: number[] = [];
const introspectionDocuments = new Map<string, string>();

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
    vscode.commands.registerCommand("aspLsp.clearDiskCache", async () =>
      executeServerCommand(clearDiskCacheServerCommand),
    ),
    vscode.commands.registerCommand("aspLsp.clearProcessCache", async () =>
      executeServerCommand(clearProcessCacheServerCommand),
    ),
    vscode.commands.registerCommand("aspLsp.openOutput", () => outputChannel?.show()),
    vscode.commands.registerCommand("aspLsp.viewFileText", async () => viewFileText()),
    vscode.commands.registerCommand("aspLsp.viewSyntaxTree", async () => viewSyntaxTree()),
    vscode.commands.registerCommand("aspLsp.analyzerStatus", async () => showAnalyzerStatus()),
    vscode.commands.registerCommand("aspLsp.memoryUsage", async () => showMemoryUsage()),
    vscode.commands.registerCommand("aspLsp.openServerLogs", () => outputChannel?.show()),
    vscode.commands.registerCommand("aspLsp.matchingBrace", async () => goToMatchingBrace()),
    vscode.commands.registerCommand("aspLsp.parentModule", async () =>
      pickIncludeLocation(parentModuleMethod),
    ),
    vscode.commands.registerCommand("aspLsp.childModules", async () =>
      pickIncludeLocation(childModulesMethod),
    ),
    vscode.commands.registerCommand("aspLsp.joinLines", async () => applyJoinLines()),
    vscode.commands.registerCommand("aspLsp.onEnter", async () => applyOnEnter()),
    vscode.commands.registerCommand("aspLsp.moveItemUp", async () => applyMoveItem("up")),
    vscode.commands.registerCommand("aspLsp.moveItemDown", async () => applyMoveItem("down")),
    vscode.commands.registerCommand("aspLsp.externalDocs", async (uri, position) =>
      openExternalDocs(uri, position),
    ),
    vscode.commands.registerCommand("aspLsp.ssr", async () => applySsr()),
    vscode.workspace.registerTextDocumentContentProvider(introspectionScheme, {
      provideTextDocumentContent(uri) {
        return introspectionDocuments.get(uri.toString()) ?? "";
      },
    }),
    vscode.workspace.onDidCloseTextDocument((document) => {
      if (document.uri.scheme === introspectionScheme) {
        introspectionDocuments.delete(document.uri.toString());
      }
    }),
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
  );
  await startClient(context);
}

async function executeServerCommand(command: string): Promise<unknown> {
  return client?.sendRequest("workspace/executeCommand", { command });
}

async function viewFileText(): Promise<void> {
  const uri = activeDocumentUri();
  if (!uri) {
    return;
  }
  const content = await sendIntrospectionRequest(viewFileTextMethod, {
    textDocument: { uri },
  });
  if (content === undefined) {
    return;
  }
  await showReadonlyIntrospectionDocument(
    extensionLocalizer()("introspection.viewFileText.title"),
    "asp",
    "classic-asp",
    content,
  );
}

async function viewSyntaxTree(): Promise<void> {
  const uri = activeDocumentUri();
  if (!uri) {
    return;
  }
  const content = await sendIntrospectionRequest(viewSyntaxTreeMethod, {
    textDocument: { uri },
  });
  if (content === undefined) {
    return;
  }
  await showReadonlyIntrospectionDocument(
    extensionLocalizer()("introspection.viewSyntaxTree.title"),
    "json",
    "json",
    content,
  );
}

async function showAnalyzerStatus(): Promise<void> {
  const content = await sendIntrospectionRequest(analyzerStatusMethod, null);
  if (content === undefined) {
    return;
  }
  await showReadonlyIntrospectionDocument(
    extensionLocalizer()("introspection.analyzerStatus.title"),
    "txt",
    "plaintext",
    content,
  );
}

async function showMemoryUsage(): Promise<void> {
  const content = await sendIntrospectionRequest(memoryUsageMethod, null);
  if (content === undefined) {
    return;
  }
  await showReadonlyIntrospectionDocument(
    extensionLocalizer()("introspection.memoryUsage.title"),
    "txt",
    "plaintext",
    content,
  );
}

async function goToMatchingBrace(): Promise<void> {
  const editor = vscode.window.activeTextEditor;
  if (!editor) {
    void vscode.window.showWarningMessage(extensionLocalizer()("introspection.noActiveEditor"));
    return;
  }
  const result = await sendJsonRequest(matchingBraceMethod, {
    textDocument: { uri: editor.document.uri.toString() },
    position: {
      line: editor.selection.active.line,
      character: editor.selection.active.character,
    },
  });
  const positions = Array.isArray(result)
    ? result.map(toPosition).filter((item): item is vscode.Position => Boolean(item))
    : [];
  const target = matchingBraceTarget(positions, editor.selection.active);
  if (!target) {
    return;
  }
  editor.selection = new vscode.Selection(target, target);
  editor.revealRange(
    new vscode.Range(target, target),
    vscode.TextEditorRevealType.InCenterIfOutsideViewport,
  );
}

async function pickIncludeLocation(method: string): Promise<void> {
  const uri = activeDocumentUri();
  if (!uri) {
    return;
  }
  const result = await sendJsonRequest(method, { textDocument: { uri } });
  const locations = Array.isArray(result)
    ? result.map(toLocation).filter((location): location is vscode.Location => Boolean(location))
    : [];
  if (locations.length === 0) {
    return;
  }
  const picked = await vscode.window.showQuickPick(
    locations.map((location) => ({
      label: location.uri.fsPath || location.uri.toString(),
      location,
    })),
  );
  if (!picked) {
    return;
  }
  const document = await vscode.workspace.openTextDocument(picked.location.uri);
  const editor = await vscode.window.showTextDocument(document);
  editor.selection = new vscode.Selection(picked.location.range.start, picked.location.range.start);
  editor.revealRange(picked.location.range, vscode.TextEditorRevealType.InCenterIfOutsideViewport);
}

async function applyJoinLines(): Promise<void> {
  const editor = vscode.window.activeTextEditor;
  if (!editor) {
    void vscode.window.showWarningMessage(extensionLocalizer()("introspection.noActiveEditor"));
    return;
  }
  const result = await sendJsonRequest(joinLinesMethod, {
    textDocument: { uri: editor.document.uri.toString() },
    ranges: editor.selections.map((selection) => ({
      start: { line: selection.start.line, character: selection.start.character },
      end: { line: selection.end.line, character: selection.end.character },
    })),
  });
  if (!(await applyTextEdits(editor.document.uri, result))) {
    await vscode.commands.executeCommand("editor.action.joinLines");
  }
}

async function applyOnEnter(): Promise<void> {
  const editor = vscode.window.activeTextEditor;
  if (!editor) {
    void vscode.window.showWarningMessage(extensionLocalizer()("introspection.noActiveEditor"));
    return;
  }
  const position = editor.selection.active;
  const result = await sendJsonRequest(onEnterMethod, {
    textDocument: { uri: editor.document.uri.toString() },
    position: { line: position.line, character: position.character },
  });
  if (!(await applyTextEdits(editor.document.uri, result))) {
    await vscode.commands.executeCommand("default:type", { text: "\n" });
    return;
  }
  const firstEdit = Array.isArray(result) ? toTextEdit(result[0]) : undefined;
  const newPosition = firstEdit
    ? positionAfterInsertedText(position, firstEdit.newText)
    : undefined;
  if (newPosition) {
    editor.selection = new vscode.Selection(newPosition, newPosition);
  }
}

async function applyMoveItem(direction: "up" | "down"): Promise<void> {
  const editor = vscode.window.activeTextEditor;
  if (!editor) {
    void vscode.window.showWarningMessage(extensionLocalizer()("introspection.noActiveEditor"));
    return;
  }
  const position = editor.selection.active;
  const result = await sendJsonRequest(moveItemMethod, {
    textDocument: { uri: editor.document.uri.toString() },
    position: { line: position.line, character: position.character },
    direction,
  });
  if (!(await applyTextEdits(editor.document.uri, result))) {
    await vscode.commands.executeCommand(
      direction === "up" ? "editor.action.moveLinesUpAction" : "editor.action.moveLinesDownAction",
    );
  }
}

async function openExternalDocs(uri?: unknown, position?: unknown): Promise<void> {
  const editor = vscode.window.activeTextEditor;
  const targetUri = toUri(uri) ?? editor?.document.uri;
  const targetPosition = toPosition(position) ?? editor?.selection.active;
  if (!targetUri || !targetPosition) {
    void vscode.window.showWarningMessage(extensionLocalizer()("introspection.noActiveEditor"));
    return;
  }
  const result = await sendJsonRequest(externalDocsMethod, {
    textDocument: { uri: targetUri.toString() },
    position: { line: targetPosition.line, character: targetPosition.character },
  });
  if (!result || typeof result !== "object") {
    return;
  }
  const url = (result as { web_url?: unknown }).web_url;
  if (typeof url === "string") {
    await vscode.env.openExternal(vscode.Uri.parse(url));
  }
}

async function applySsr(): Promise<void> {
  const editor = vscode.window.activeTextEditor;
  if (!editor) {
    void vscode.window.showWarningMessage(extensionLocalizer()("introspection.noActiveEditor"));
    return;
  }
  const search = await vscode.window.showInputBox({
    prompt: extensionLocalizer()("introspection.ssrSearch"),
  });
  if (!search) {
    return;
  }
  const replace = await vscode.window.showInputBox({
    prompt: extensionLocalizer()("introspection.ssrReplace"),
  });
  if (!replace) {
    return;
  }
  const result = await sendJsonRequest(ssrMethod, {
    textDocument: { uri: editor.document.uri.toString() },
    search,
    replace,
  });
  await applyWorkspaceEdit(result);
}

async function applyTextEdits(uri: vscode.Uri, edits: unknown): Promise<boolean> {
  if (!Array.isArray(edits) || edits.length === 0) {
    return false;
  }
  const workspaceEdit = new vscode.WorkspaceEdit();
  let hasEdit = false;
  for (const edit of edits) {
    const candidate = toTextEdit(edit);
    if (!candidate) {
      continue;
    }
    workspaceEdit.replace(uri, candidate.range, candidate.newText);
    hasEdit = true;
  }
  return hasEdit ? vscode.workspace.applyEdit(workspaceEdit) : false;
}

async function applyWorkspaceEdit(edit: unknown): Promise<void> {
  if (!edit || typeof edit !== "object") {
    return;
  }
  const changes = (edit as { changes?: unknown }).changes;
  if (!changes || typeof changes !== "object") {
    return;
  }
  const workspaceEdit = new vscode.WorkspaceEdit();
  for (const [uriText, edits] of Object.entries(changes as Record<string, unknown>)) {
    if (!Array.isArray(edits)) {
      continue;
    }
    const uri = vscode.Uri.parse(uriText);
    for (const edit of edits) {
      if (!edit || typeof edit !== "object") {
        continue;
      }
      const candidate = edit as { range?: unknown; newText?: unknown };
      const range = toRange(candidate.range);
      if (!range || typeof candidate.newText !== "string") {
        continue;
      }
      workspaceEdit.replace(uri, range, candidate.newText);
    }
  }
  await vscode.workspace.applyEdit(workspaceEdit);
}

function activeDocumentUri(): string | undefined {
  const uri = vscode.window.activeTextEditor?.document.uri.toString();
  if (!uri) {
    void vscode.window.showWarningMessage(extensionLocalizer()("introspection.noActiveEditor"));
  }
  return uri;
}

async function sendIntrospectionRequest(
  method: string,
  params: unknown,
): Promise<string | undefined> {
  const result = await sendJsonRequest(method, params);
  if (result === undefined) {
    return undefined;
  }
  return typeof result === "string" ? result : JSON.stringify(result, null, 2);
}

async function sendJsonRequest(method: string, params: unknown): Promise<unknown> {
  const languageClient = client;
  if (!languageClient) {
    void vscode.window.showWarningMessage(extensionLocalizer()("introspection.serverUnavailable"));
    return undefined;
  }
  try {
    return await languageClient.sendRequest(method, params);
  } catch (error) {
    void vscode.window.showErrorMessage(
      extensionLocalizer()("introspection.requestFailed", { error: String(error) }),
    );
    return undefined;
  }
}

async function showReadonlyIntrospectionDocument(
  title: string,
  extension: string,
  languageId: string,
  content: string,
): Promise<void> {
  const id = `${Date.now()}-${Math.random().toString(16).slice(2)}`;
  const uri = vscode.Uri.from({
    scheme: introspectionScheme,
    path: `/${id}.${extension}`,
    query: title,
  });
  introspectionDocuments.set(uri.toString(), content);
  const document = await vscode.workspace.openTextDocument(uri);
  const typedDocument = await vscode.languages.setTextDocumentLanguage(document, languageId);
  await vscode.window.showTextDocument(typedDocument, { preview: false });
}

async function startClient(context: vscode.ExtensionContext): Promise<void> {
  if (isDeactivating) {
    return;
  }
  const env = { ...process.env };
  const configuredServerPath =
    vscode.workspace.getConfiguration("aspLsp").get<string>("server.path") ?? "";
  const serverOptions = createServerOptions(
    getServerLaunchPath(context, configuredServerPath),
    env,
  );
  const clientOptions: LanguageClientOptions = {
    documentSelector: [{ scheme: "file", language: "classic-asp" }],
    outputChannel,
    synchronize: {
      configurationSection: "aspLsp",
      fileEvents: vscode.workspace.createFileSystemWatcher(
        "**/*.{asp,asa,inc,js,jsx,mjs,cjs,ts,tsx,mts,cts,d.ts,json}",
      ),
    },
    errorHandler: createLanguageClientErrorHandler(),
    middleware: {
      async provideHover(document, position, token, next) {
        const result = await sendJsonRequest("textDocument/hover", {
          textDocument: { uri: document.uri.toString() },
          position: { line: position.line, character: position.character },
        });
        return toHoverWithActions(result) ?? next(document, position, token);
      },
    },
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

function createServerOptions(
  serverLaunch: ServerLaunchPath,
  env: NodeJS.ProcessEnv,
): ServerOptions {
  return {
    run: {
      command: serverLaunch.path,
      transport: TransportKind.stdio,
      options: { env },
    },
    debug: {
      command: serverLaunch.path,
      transport: TransportKind.stdio,
      options: { env },
    },
  };
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

function matchingBraceTarget(
  positions: vscode.Position[],
  active: vscode.Position,
): vscode.Position | undefined {
  const [start, end] = positions;
  if (!start || !end) {
    return undefined;
  }
  if (active.isEqual(start)) {
    return end;
  }
  if (active.isEqual(end)) {
    return start;
  }
  return end;
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

function toTextEdit(value: unknown): { range: vscode.Range; newText: string } | undefined {
  if (!value || typeof value !== "object") {
    return undefined;
  }
  const candidate = value as { range?: unknown; newText?: unknown };
  const range = toRange(candidate.range);
  return range && typeof candidate.newText === "string"
    ? { range, newText: candidate.newText }
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

function positionAfterInsertedText(
  position: vscode.Position,
  insertedText: string,
): vscode.Position {
  const normalized = insertedText.replace(/\r\n/g, "\n");
  const lines = normalized.split("\n");
  if (lines.length === 1) {
    return position.translate(0, insertedText.length);
  }
  return new vscode.Position(
    position.line + lines.length - 1,
    lines[lines.length - 1]?.length ?? 0,
  );
}

function toHoverWithActions(value: unknown): vscode.Hover | undefined {
  if (!value || typeof value !== "object") {
    return undefined;
  }
  const candidate = value as { contents?: unknown; range?: unknown; actions?: unknown };
  const contents = hoverContentsToMarkdown(candidate.contents);
  const actionMarkdown = hoverActionsToMarkdown(candidate.actions);
  if (actionMarkdown) {
    contents.push(actionMarkdown);
  }
  if (contents.length === 0) {
    return undefined;
  }
  return new vscode.Hover(contents, toRange(candidate.range));
}

function hoverContentsToMarkdown(contents: unknown): vscode.MarkdownString[] {
  if (Array.isArray(contents)) {
    return contents.flatMap(hoverContentsToMarkdown);
  }
  if (typeof contents === "string") {
    return [new vscode.MarkdownString(contents)];
  }
  if (!contents || typeof contents !== "object") {
    return [];
  }
  const candidate = contents as { kind?: unknown; value?: unknown; language?: unknown };
  if (typeof candidate.value !== "string") {
    return [];
  }
  if (typeof candidate.language === "string") {
    const markdown = new vscode.MarkdownString();
    markdown.appendCodeblock(candidate.value, candidate.language);
    return [markdown];
  }
  return [new vscode.MarkdownString(candidate.value)];
}

function hoverActionsToMarkdown(actions: unknown): vscode.MarkdownString | undefined {
  if (!Array.isArray(actions)) {
    return undefined;
  }
  const links = actions
    .map((action) => {
      if (!action || typeof action !== "object") {
        return undefined;
      }
      const candidate = action as { title?: unknown; command?: unknown; arguments?: unknown };
      if (typeof candidate.title !== "string" || typeof candidate.command !== "string") {
        return undefined;
      }
      if (candidate.command !== "aspLsp.externalDocs") {
        return undefined;
      }
      const args = Array.isArray(candidate.arguments) ? candidate.arguments : [];
      const encodedArgs = encodeURIComponent(JSON.stringify(args));
      return `[${escapeMarkdownLinkText(candidate.title)}](command:${candidate.command}?${encodedArgs})`;
    })
    .filter((link): link is string => Boolean(link));
  if (links.length === 0) {
    return undefined;
  }
  const markdown = new vscode.MarkdownString(links.join("  \n"));
  markdown.isTrusted = { enabledCommands: ["aspLsp.externalDocs"] };
  return markdown;
}

function escapeMarkdownLinkText(text: string): string {
  return text.replace(/([\\[\]])/g, "\\$1");
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
  statusBarItem.text = "$(code) ASP LSP: Rust";
  const backendLine = localizer("status.backend.rust", {
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
  if (candidate.backend === "rust" && typeof candidate.engine === "string") {
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
  | "status.backend.rust"
  | "status.backend.reason"
  | "debug.iis.name"
  | "debug.iisExpress.name"
  | "launch.noWorkspace"
  | "launch.created"
  | "introspection.viewFileText.title"
  | "introspection.viewSyntaxTree.title"
  | "introspection.analyzerStatus.title"
  | "introspection.memoryUsage.title"
  | "introspection.noActiveEditor"
  | "introspection.serverUnavailable"
  | "introspection.requestFailed"
  | "introspection.ssrSearch"
  | "introspection.ssrReplace";

type ExtensionMessageArgs = Record<string, string>;

const extensionMessages: Record<"en" | "ja", Record<ExtensionMessageKey, string>> = {
  en: {
    "status.tooltip": "Classic ASP Language Server",
    "status.backend.pending": "Backend: detecting",
    "status.backend.rust": "Backend: Rust ({engine})",
    "status.backend.reason": "Reason: {reason}",
    "debug.iis.name": "Debug Classic ASP URL",
    "debug.iisExpress.name": "Debug Classic ASP IIS Express URL",
    "launch.noWorkspace": "Open a workspace before creating launch.json.",
    "launch.created": "Classic ASP launch.json snippet created.",
    "introspection.viewFileText.title": "Classic ASP File Text",
    "introspection.viewSyntaxTree.title": "Classic ASP Parsed JSON",
    "introspection.analyzerStatus.title": "Classic ASP Analyzer Status",
    "introspection.memoryUsage.title": "Classic ASP Memory Usage",
    "introspection.noActiveEditor": "Open an editor before running this command.",
    "introspection.serverUnavailable": "Classic ASP Language Server is not running.",
    "introspection.requestFailed": "Classic ASP LSP request failed: {error}",
    "introspection.ssrSearch": "Search identifier",
    "introspection.ssrReplace": "Replacement identifier",
  },
  ja: {
    "status.tooltip": "Classic ASP Language Server",
    "status.backend.pending": "Backend: 判定中",
    "status.backend.rust": "Backend: Rust ({engine})",
    "status.backend.reason": "Reason: {reason}",
    "debug.iis.name": "Classic ASP URL をデバッグ",
    "debug.iisExpress.name": "Classic ASP IIS Express URL をデバッグ",
    "launch.noWorkspace": "launch.json を作成する前に workspace を開いてください。",
    "launch.created": "Classic ASP の launch.json snippet を作成しました。",
    "introspection.viewFileText.title": "Classic ASP file text",
    "introspection.viewSyntaxTree.title": "Classic ASP parsed JSON",
    "introspection.analyzerStatus.title": "Classic ASP analyzer status",
    "introspection.memoryUsage.title": "Classic ASP memory usage",
    "introspection.noActiveEditor": "この command を実行する前に editor を開いてください。",
    "introspection.serverUnavailable": "Classic ASP Language Server は起動していません。",
    "introspection.requestFailed": "Classic ASP LSP request が失敗しました: {error}",
    "introspection.ssrSearch": "検索する identifier",
    "introspection.ssrReplace": "置換後の identifier",
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
      this.task("package VSIX (no Rust server)", "pnpm", ["run", "package:vsix:no-native"]),
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
