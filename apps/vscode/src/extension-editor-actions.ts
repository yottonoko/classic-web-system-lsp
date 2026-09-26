import * as vscode from "vscode";
import type { LanguageClient } from "vscode-languageclient/node";
import type { LineCommentEditsParams, LineCommentEditsResult } from "./protocol-types";
import { uriTextForVSCode } from "./uri-encoding";
import type { ExtensionMessageArgs, ExtensionMessageKey } from "./extension-localization";
import { expandCommentSelectionOffsetsAfterEdits } from "./comment-selection";

const lineCommentEditsRequestMethod = "aspLsp/textDocument/lineCommentEdits";
type Localize = (key: ExtensionMessageKey, args?: ExtensionMessageArgs) => string;

function waitForLanguageClientTextDocumentSync(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

export async function toggleLineComment(
  client: LanguageClient | undefined,
  localize: Localize,
): Promise<void> {
  const editor = vscode.window.activeTextEditor;
  if (!editor || editor.document.languageId !== "classic-asp") {
    await vscode.commands.executeCommand("editor.action.commentLine");
    return;
  }
  if (!client) {
    void vscode.window.showWarningMessage(localize("comment.serverUnavailable"));
    return;
  }
  const document = editor.document;
  const documentUri = document.uri.toString();
  const documentVersion = document.version;
  const originalSelections = [...editor.selections];
  const params: LineCommentEditsParams = {
    textDocument: { uri: documentUri, version: documentVersion },
    selections: originalSelections.map((selection) => ({
      start: { line: selection.start.line, character: selection.start.character },
      end: { line: selection.end.line, character: selection.end.character },
    })),
  };
  await waitForLanguageClientTextDocumentSync();
  if (
    document.version !== documentVersion ||
    vscode.window.activeTextEditor !== editor ||
    !sameSelections(editor.selections, originalSelections)
  ) {
    return;
  }
  const result = await client.sendRequest<LineCommentEditsResult | null>(
    lineCommentEditsRequestMethod,
    params,
  );
  if (
    !result ||
    result.version !== documentVersion ||
    document.version !== documentVersion ||
    vscode.window.activeTextEditor !== editor ||
    !sameSelections(editor.selections, originalSelections) ||
    result.edits.length === 0
  ) {
    if (result?.noOpReason) {
      void vscode.window.showWarningMessage(commentNoOpMessage(result.noOpReason, localize));
    }
    return;
  }
  const expandedSelectionOffsets = expandCommentSelectionOffsetsAfterEdits(
    originalSelections.map((selection) => selectedLineOffsets(document, selection)),
    result.edits.map((edit) => ({
      start: document.offsetAt(toVscodeRange(edit.range).start),
      end: document.offsetAt(toVscodeRange(edit.range).end),
      newTextLength: edit.newText.length,
    })),
  );
  const workspaceEdit = new vscode.WorkspaceEdit();
  for (const edit of result.edits) {
    workspaceEdit.replace(document.uri, toVscodeRange(edit.range), edit.newText);
  }
  let selectionChangedDuringApply = false;
  const selectionChangeSubscription = vscode.window.onDidChangeTextEditorSelection(
    (selectionChange) => {
      if (selectionChange.textEditor === editor && selectionChange.kind !== undefined) {
        selectionChangedDuringApply = true;
      }
    },
  );
  let applied: boolean;
  try {
    applied = await vscode.workspace.applyEdit(workspaceEdit);
    await new Promise<void>((resolve) => setTimeout(resolve, 0));
  } finally {
    selectionChangeSubscription.dispose();
  }
  if (!applied || selectionChangedDuringApply || vscode.window.activeTextEditor !== editor) {
    return;
  }
  const trackedSelections = editor.selections;
  editor.selections = originalSelections.map((selection, index) => {
    const expandedOffsets = expandedSelectionOffsets[index];
    const trackedSelection = trackedSelections[index];
    if (!expandedOffsets) {
      return trackedSelection ?? selection;
    }
    const expandedRange = new vscode.Range(
      document.positionAt(expandedOffsets.start),
      document.positionAt(expandedOffsets.end),
    );
    const range = trackedSelection ? expandedRange.union(trackedSelection) : expandedRange;
    return selection.isReversed
      ? new vscode.Selection(range.end, range.start)
      : new vscode.Selection(range.start, range.end);
  });
}

function selectedLineOffsets(
  document: vscode.TextDocument,
  selection: vscode.Selection,
): { start: number; end: number } | undefined {
  if (selection.isEmpty) {
    return undefined;
  }
  let endLine = selection.end.line;
  if (selection.end.character === 0 && endLine > selection.start.line) {
    endLine--;
  }
  return {
    start: document.offsetAt(new vscode.Position(selection.start.line, 0)),
    end: document.offsetAt(document.lineAt(endLine).range.end),
  };
}

function sameSelections(
  left: readonly vscode.Selection[],
  right: readonly vscode.Selection[],
): boolean {
  return (
    left.length === right.length &&
    left.every(
      (selection, index) =>
        selection.anchor.isEqual(right[index].anchor) &&
        selection.active.isEqual(right[index].active),
    )
  );
}

function commentNoOpMessage(
  reason: NonNullable<LineCommentEditsResult["noOpReason"]>,
  localize: Localize,
): string {
  if (reason === "directive-requires-full-document") {
    return localize("comment.directiveRequiresWholeDocument");
  }
  return localize("comment.unsafeStructure");
}

export function toVscodeRange(range: {
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

export async function showReferences(
  uri: unknown,
  position: unknown,
  locations: unknown,
): Promise<void> {
  const targetUri = toUri(uri);
  const targetPosition = toPosition(position);
  let targetLocations = Array.isArray(locations)
    ? locations.map(toLocation).filter((location): location is vscode.Location => Boolean(location))
    : [];
  if (!targetUri || !targetPosition) {
    return;
  }
  if (!Array.isArray(locations)) {
    targetLocations =
      (await vscode.commands.executeCommand<vscode.Location[]>(
        "vscode.executeReferenceProvider",
        targetUri,
        targetPosition,
      )) ?? [];
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
  return typeof value === "string" ? vscode.Uri.parse(uriTextForVSCode(value)) : undefined;
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
