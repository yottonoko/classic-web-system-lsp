import * as vscode from "vscode";
import type { LanguageClient } from "vscode-languageclient/node";
import { toVscodeRange } from "./extension-editor-actions";

const htmlTagCompleteLookBehind = 2000;

export async function autoCloseHtmlTag(
  event: vscode.TextDocumentChangeEvent,
  client: LanguageClient | undefined,
): Promise<void> {
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
  const editor = activeEditorForDocument(document);
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
  if (applied && editor && vscode.window.activeTextEditor === editor) {
    editor.selection = new vscode.Selection(position, position);
  }
}

function waitForLanguageClientTextDocumentSync(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

export async function autoCloseAspBlock(event: vscode.TextDocumentChangeEvent): Promise<void> {
  if (
    event.document.languageId !== "classic-asp" ||
    event.contentChanges.length !== 1 ||
    event.contentChanges[0]?.text !== "%" ||
    !event.contentChanges[0].range.isEmpty
  ) {
    return;
  }
  const change = event.contentChanges[0];
  const editor = activeEditorForDocument(event.document);
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
  if (applied && editor && vscode.window.activeTextEditor === editor) {
    editor.selection = new vscode.Selection(position, position);
  }
}

function activeEditorForDocument(document: vscode.TextDocument): vscode.TextEditor | undefined {
  const editor = vscode.window.activeTextEditor;
  return editor?.document.uri.toString() === document.uri.toString() ? editor : undefined;
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
