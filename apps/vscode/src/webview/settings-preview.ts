import { formatAspDocument, parseAspDocument, type AspFormattingOptions } from "@asp-lsp/core";

type SettingValueReader = (key: string) => unknown;

interface PreviewTextEdit {
  range: {
    start: { character: number; line: number };
    end: { character: number; line: number };
  };
  newText: string;
}

export interface SettingsFormatterPreview {
  formattedText: string;
  options: AspFormattingOptions;
  sourceText: string;
}

export const settingsFormatterPreviewSource = `<%
If enabled Then
Response.Write "ok"
End If
Select Case status
Case "ready"
Response.Write "ready"
End Select
total = first + _
longerName
first=1
longerName=2
%>`;

const formatterPreviewKeys = new Set(["editor.tabSize", "editor.insertSpaces"]);

export function isFormatterPreviewSetting(key: string): boolean {
  return key.startsWith("aspLsp.format.") || formatterPreviewKeys.has(key);
}

export function settingsFormatterPreview(
  settingValue: SettingValueReader,
): SettingsFormatterPreview {
  const options = settingsFormatterPreviewOptions(settingValue);
  const parsed = parseAspDocument("file:///settings-preview.asp", settingsFormatterPreviewSource);
  const formattedText = applyPreviewTextEdits(
    settingsFormatterPreviewSource,
    formatAspDocument(parsed, options),
  );
  return {
    formattedText,
    options,
    sourceText: settingsFormatterPreviewSource,
  };
}

function settingsFormatterPreviewOptions(settingValue: SettingValueReader): AspFormattingOptions {
  const indentSize = positiveNumber(settingValue("aspLsp.format.indentSize"));
  const vbscriptIndentSize = positiveNumber(settingValue("aspLsp.format.vbscriptIndentSize"));
  const tabSize = positiveNumber(settingValue("editor.tabSize")) ?? 2;
  const indentStyle = stringUnion(settingValue("aspLsp.format.indentStyle"), ["space", "tab"]);
  const vbscriptIndentStyle = stringUnion(settingValue("aspLsp.format.vbscriptIndentStyle"), [
    "space",
    "tab",
  ]);
  const vbscriptBlockIndent = stringUnion(settingValue("aspLsp.format.vbscriptBlockIndent"), [
    "alignWithDelimiter",
    "indentInsideDelimiter",
  ]);
  return {
    alignAssignments: settingValue("aspLsp.format.alignAssignments") === true,
    aspBlockNewline: stringUnion(settingValue("aspLsp.format.aspBlockNewline"), [
      "preserve",
      "alwaysMultiline",
      "singleLineWhenPossible",
    ]),
    aspDelimiterSpacing: stringUnion(settingValue("aspLsp.format.aspDelimiterSpacing"), [
      "padded",
      "compact",
    ]),
    ignoreVbscriptTagIndent: settingValue("aspLsp.format.ignoreVbscriptTagIndent") === true,
    indentSize,
    indentStyle,
    insertSpaces: indentStyle
      ? indentStyle !== "tab"
      : settingValue("editor.insertSpaces") !== false,
    tabSize,
    uppercaseKeywords: settingValue("aspLsp.format.uppercaseKeywords") === true,
    vbscriptBlockIndent,
    vbscriptIndentSize,
    vbscriptIndentStyle,
    vbscriptKeywordCase: stringUnion(settingValue("aspLsp.format.vbscriptKeywordCase"), [
      "preserve",
      "upper",
      "lower",
      "title",
    ]),
    vbscriptLineContinuationIndentSize: positiveNumber(
      settingValue("aspLsp.format.vbscriptLineContinuationIndentSize"),
    ),
    vbscriptSelectCaseIndent: stringUnion(settingValue("aspLsp.format.vbscriptSelectCaseIndent"), [
      "caseIndented",
      "caseAligned",
    ]),
    vbscriptTagIndentMode: stringUnion(settingValue("aspLsp.format.vbscriptTagIndentMode"), [
      "relativeToTag",
      "ignoreTag",
      "preserveExisting",
    ]),
  };
}

function applyPreviewTextEdits(text: string, edits: PreviewTextEdit[]): string {
  return [...edits]
    .sort((left, right) => offsetAt(text, right.range.start) - offsetAt(text, left.range.start))
    .reduce((current, edit) => {
      const start = offsetAt(current, edit.range.start);
      const end = offsetAt(current, edit.range.end);
      return current.slice(0, start) + edit.newText + current.slice(end);
    }, text);
}

function offsetAt(text: string, position: { character: number; line: number }): number {
  let offset = 0;
  let line = 0;
  while (line < position.line && offset < text.length) {
    const next = text.indexOf("\n", offset);
    if (next === -1) {
      return text.length;
    }
    offset = next + 1;
    line += 1;
  }
  return Math.min(text.length, offset + position.character);
}

function positiveNumber(value: unknown): number | undefined {
  return typeof value === "number" && Number.isFinite(value) && value > 0 ? value : undefined;
}

function stringUnion<const T extends string>(value: unknown, values: readonly T[]): T | undefined {
  return typeof value === "string" && values.includes(value as T) ? (value as T) : undefined;
}
