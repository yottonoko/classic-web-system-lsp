import fs from "node:fs";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import ts from "typescript";
import { getCSSLanguageService } from "vscode-css-languageservice";
import {
  getLanguageService as getHtmlLanguageService,
  TokenType,
} from "vscode-html-languageservice";
import { TextDocument } from "vscode-languageserver-textdocument";
import {
  CompletionItemKind,
  type Color,
  type ColorPresentation,
  DiagnosticSeverity,
  DiagnosticTag,
  MarkupKind,
  type CompletionItem,
  type Diagnostic,
  type DocumentHighlight,
  type DocumentSymbol,
  type FoldingRange,
  type Hover,
  type Location,
  type Position,
  type Range,
  type SelectionRange,
  type SymbolInformation,
  type TextEdit,
  type WorkspaceEdit,
} from "vscode-languageserver-types";

const frameKindJson = 1;
const browserJavaScriptLibs = ["lib.esnext.d.ts", "lib.dom.d.ts", "lib.dom.iterable.d.ts"];
const tsUnusedDiagnosticCodes = new Set([6133, 6192, 6196]);
const htmlService = getHtmlLanguageService();
const cssService = getCSSLanguageService();

interface VirtualDocument {
  uri: string;
  languageId: string;
  text: string;
}

interface EmbeddedRequest {
  id: number;
  operation: string;
  activeVirtual: VirtualDocument;
  openVirtuals: VirtualDocument[];
  settings: AspSettings;
  workspaceRoots: string[];
  projectGeneration: number;
  projectFingerprint?: string;
  projectResetReason?: string;
  params?: unknown;
}

interface EmbeddedResponse {
  id: number;
  ok: boolean;
  result?: unknown;
  error?: string;
  cacheStats?: SidecarCacheStats;
}

interface SidecarCacheStats {
  generationReset: number;
  resetReason?: string;
  projectFingerprint?: string;
  fileExistsHit: number;
  fileExistsMiss: number;
  readFileHit: number;
  readFileMiss: number;
  directoryExistsHit: number;
  directoryExistsMiss: number;
  getDirectoriesHit: number;
  getDirectoriesMiss: number;
  readDirectoryHit: number;
  readDirectoryMiss: number;
  realpathHit: number;
  realpathMiss: number;
}

type SidecarCacheStatCounter = {
  [Key in keyof SidecarCacheStats]-?: SidecarCacheStats[Key] extends number ? Key : never;
}[keyof SidecarCacheStats];

interface AspSettings {
  checkJs?: boolean;
  embedded?: {
    parallelism?: number;
  };
  javascript?: {
    unusedDiagnostics?: boolean;
    ignoreProjectConfig?: boolean;
  };
}

interface CachedTsDiagnostic {
  code: number;
  category: ts.DiagnosticCategory;
  messageText: string;
  start?: number;
  length?: number;
  reportsUnnecessary?: boolean;
}

interface JsProjectConfig {
  fileNames: string[];
  options: ts.CompilerOptions;
  currentDirectory: string;
}

interface CachedLanguageServiceProject {
  service: ts.LanguageService;
  fileName: string;
  lastUsed: number;
}

interface DecodedSemanticToken {
  range: Range;
  tokenType: number;
  tokenModifiers: number;
}

let inputBuffer = Buffer.alloc(0);
let currentProjectCacheKey: string | undefined;
const fileExistsCache = new Map<string, boolean>();
const readFileCache = new Map<string, string | undefined>();
const directoryExistsCache = new Map<string, boolean>();
const directoriesCache = new Map<string, string[]>();
const readDirectoryCache = new Map<string, string[]>();
const realpathCache = new Map<string, string>();
const languageServiceProjectCache = new Map<string, CachedLanguageServiceProject>();
let languageServiceProjectCacheTick = 0;
let currentRequestStats: SidecarCacheStats | undefined;
const maxLanguageServiceProjectCacheEntries = 8;

if (process.env.ASP_LSP_SIDECAR_TEST_MODE !== "1") {
  process.stdin.on("data", (chunk: Buffer) => {
    inputBuffer = Buffer.concat([inputBuffer, chunk]);
    for (const payload of readFrames()) {
      void handlePayload(payload);
    }
  });

  process.stdin.on("end", () => {
    process.exit(0);
  });
}

function readFrames(): Buffer[] {
  const frames: Buffer[] = [];
  while (inputBuffer.length >= 4) {
    const length = inputBuffer.readUInt32LE(0);
    if (inputBuffer.length < 4 + length) {
      break;
    }
    const kind = inputBuffer[4];
    const payload = inputBuffer.subarray(5, 4 + length);
    inputBuffer = inputBuffer.subarray(4 + length);
    if (kind === frameKindJson) {
      frames.push(payload);
    }
  }
  return frames;
}

async function handlePayload(payload: Buffer): Promise<void> {
  const request = JSON.parse(payload.toString("utf8")) as EmbeddedRequest;
  currentRequestStats = createCacheStats();
  try {
    resetCachesForProject(request);
    const result = await handleRequest(request);
    writeResponse({ id: request.id, ok: true, result, cacheStats: currentRequestStats });
  } catch (error) {
    writeResponse({
      id: request.id,
      ok: false,
      error: errorMessage(error),
      cacheStats: currentRequestStats,
    });
  } finally {
    currentRequestStats = undefined;
  }
}

async function handleRequest(request: EmbeddedRequest): Promise<unknown> {
  if (request.operation === "diagnostics") {
    return diagnostics(request);
  }
  if (request.operation === "completion") {
    return completion(request);
  }
  if (request.operation === "hover") {
    return hover(request);
  }
  if (request.operation === "definition") {
    return definition(request);
  }
  if (request.operation === "documentHighlights") {
    return documentHighlights(request);
  }
  if (request.operation === "selectionRanges") {
    return selectionRanges(request);
  }
  if (request.operation === "prepareRename") {
    return prepareRename(request);
  }
  if (request.operation === "rename") {
    return rename(request);
  }
  if (
    request.operation === "formatting" ||
    request.operation === "rangeFormatting" ||
    request.operation === "onTypeFormatting"
  ) {
    return formatting(request);
  }
  if (request.operation === "semanticTokens") {
    return semanticTokens(request);
  }
  if (request.operation === "documentSymbols") {
    return documentSymbols(request);
  }
  if (request.operation === "foldingRanges") {
    return foldingRanges(request);
  }
  if (request.operation === "documentColors") {
    return documentColors(request);
  }
  if (request.operation === "colorPresentations") {
    return colorPresentations(request);
  }
  if (request.operation === "linkedEditingRanges") {
    return linkedEditingRanges(request);
  }
  if (request.operation === "shutdown") {
    process.exit(0);
  }
  throw new Error(`unknown operation: ${request.operation}`);
}

async function diagnostics(request: EmbeddedRequest): Promise<Diagnostic[]> {
  const language = request.activeVirtual.languageId;
  if (language === "html") {
    return htmlDiagnostics(request.activeVirtual);
  }
  if (language === "css") {
    return cssDiagnostics(request.activeVirtual);
  }
  if (language === "javascript" || language === "jscript") {
    return jsDiagnostics(request);
  }
  return [];
}

async function completion(request: EmbeddedRequest): Promise<CompletionItem[]> {
  const document = toTextDocument(request.activeVirtual);
  const position = requestPosition(request);
  const language = request.activeVirtual.languageId;
  if (language === "html") {
    const htmlDocument = htmlService.parseHTMLDocument(document);
    return htmlService.doComplete(document, position, htmlDocument).items;
  }
  if (language === "css") {
    const stylesheet = cssService.parseStylesheet(document);
    return cssService.doComplete(document, position, stylesheet).items;
  }
  if (language === "javascript" || language === "jscript") {
    return jsCompletion(request, document, position);
  }
  return [];
}

async function hover(request: EmbeddedRequest): Promise<Hover | null> {
  const document = toTextDocument(request.activeVirtual);
  const position = requestPosition(request);
  const language = request.activeVirtual.languageId;
  if (language === "html") {
    const htmlDocument = htmlService.parseHTMLDocument(document);
    return htmlService.doHover(document, position, htmlDocument);
  }
  if (language === "css") {
    const stylesheet = cssService.parseStylesheet(document);
    return cssService.doHover(document, position, stylesheet);
  }
  if (language === "javascript" || language === "jscript") {
    return jsHover(request, document, position);
  }
  return null;
}

async function definition(request: EmbeddedRequest): Promise<Location | Location[] | null> {
  const document = toTextDocument(request.activeVirtual);
  const position = requestPosition(request);
  const language = request.activeVirtual.languageId;
  if (language === "css") {
    const stylesheet = cssService.parseStylesheet(document);
    return cssService.findDefinition(document, position, stylesheet);
  }
  if (language === "javascript" || language === "jscript") {
    return jsDefinition(request, document, position);
  }
  return null;
}

async function documentHighlights(request: EmbeddedRequest): Promise<DocumentHighlight[]> {
  const document = toTextDocument(request.activeVirtual);
  const position = requestPosition(request);
  const language = request.activeVirtual.languageId;
  if (language === "html") {
    const htmlDocument = htmlService.parseHTMLDocument(document);
    return htmlService.findDocumentHighlights(document, position, htmlDocument);
  }
  if (language === "css") {
    const stylesheet = cssService.parseStylesheet(document);
    return cssService.findDocumentHighlights(document, position, stylesheet);
  }
  if (language === "javascript" || language === "jscript") {
    return jsDocumentHighlights(request, document, position);
  }
  return [];
}

async function selectionRanges(request: EmbeddedRequest): Promise<SelectionRange[]> {
  const document = toTextDocument(request.activeVirtual);
  const positions = requestPositions(request);
  const language = request.activeVirtual.languageId;
  if (language === "html") {
    return htmlService.getSelectionRanges(document, positions);
  }
  if (language === "css") {
    const stylesheet = cssService.parseStylesheet(document);
    return cssService.getSelectionRanges(document, positions, stylesheet);
  }
  if (language === "javascript" || language === "jscript") {
    return positions.map((position) => jsSelectionRange(request, document, position));
  }
  return [];
}

async function prepareRename(request: EmbeddedRequest): Promise<Range | null> {
  const document = toTextDocument(request.activeVirtual);
  const position = requestPosition(request);
  const language = request.activeVirtual.languageId;
  if (language === "html") {
    const htmlDocument = htmlService.parseHTMLDocument(document);
    return (
      htmlService
        .findDocumentHighlights(document, position, htmlDocument)
        .find((highlight) => rangeContainsPosition(highlight.range, position))?.range ?? null
    );
  }
  if (language === "css") {
    const stylesheet = cssService.parseStylesheet(document);
    return cssService.prepareRename(document, position, stylesheet) ?? null;
  }
  if (language === "javascript" || language === "jscript") {
    return jsPrepareRename(request, document, position);
  }
  return null;
}

async function rename(request: EmbeddedRequest): Promise<WorkspaceEdit | null> {
  const document = toTextDocument(request.activeVirtual);
  const position = requestPosition(request);
  const newName = requestNewName(request);
  const language = request.activeVirtual.languageId;
  if (language === "html") {
    const htmlDocument = htmlService.parseHTMLDocument(document);
    return htmlService.doRename(document, position, newName, htmlDocument);
  }
  if (language === "css") {
    const stylesheet = cssService.parseStylesheet(document);
    return cssService.doRename(document, position, newName, stylesheet);
  }
  if (language === "javascript" || language === "jscript") {
    return jsRename(request, document, position, newName);
  }
  return null;
}

async function formatting(request: EmbeddedRequest): Promise<TextEdit[]> {
  const document = toTextDocument(request.activeVirtual);
  const language = request.activeVirtual.languageId;
  const range =
    request.operation === "rangeFormatting" ? requestRange(request) : requestFormatRange(request);
  const options = requestFormatOptions(request);
  if (language === "html") {
    return htmlService.format(document, range, options);
  }
  if (language === "css") {
    return cssService.format(document, range, options);
  }
  if (language === "javascript" || language === "jscript") {
    return jsFormatting(request, document, range, options);
  }
  return [];
}

async function semanticTokens(request: EmbeddedRequest): Promise<DecodedSemanticToken[]> {
  const document = toTextDocument(request.activeVirtual);
  const language = request.activeVirtual.languageId;
  if (language === "css") {
    return cssSemanticTokens(document);
  }
  if (language === "javascript" || language === "jscript") {
    return jsSemanticTokens(request, document);
  }
  return [];
}

function cssSemanticTokens(document: TextDocument): DecodedSemanticToken[] {
  const tokens: DecodedSemanticToken[] = [];
  const pattern = /\b([A-Za-z-]+)\s*:/g;
  const text = document.getText();
  let match: RegExpExecArray | null;
  while ((match = pattern.exec(text))) {
    const start = match.index;
    const end = start + match[1].length;
    tokens.push({
      range: {
        start: document.positionAt(start),
        end: document.positionAt(end),
      },
      tokenType: 6,
      tokenModifiers: 0,
    });
  }
  return tokens;
}

async function documentSymbols(
  request: EmbeddedRequest,
): Promise<DocumentSymbol[] | SymbolInformation[]> {
  const document = toTextDocument(request.activeVirtual);
  const language = request.activeVirtual.languageId;
  if (language === "html") {
    const htmlDocument = htmlService.parseHTMLDocument(document);
    return htmlService.findDocumentSymbols2(document, htmlDocument);
  }
  if (language === "css") {
    const stylesheet = cssService.parseStylesheet(document);
    return cssService.findDocumentSymbols2(document, stylesheet);
  }
  return [];
}

async function foldingRanges(request: EmbeddedRequest): Promise<FoldingRange[]> {
  const document = toTextDocument(request.activeVirtual);
  const language = request.activeVirtual.languageId;
  if (language === "html") {
    return htmlService.getFoldingRanges(document);
  }
  if (language === "css") {
    return cssService.getFoldingRanges(document);
  }
  return [];
}

async function documentColors(request: EmbeddedRequest): Promise<unknown[]> {
  const document = toTextDocument(request.activeVirtual);
  if (request.activeVirtual.languageId !== "css") {
    return [];
  }
  const stylesheet = cssService.parseStylesheet(document);
  return cssService.findDocumentColors(document, stylesheet);
}

async function colorPresentations(request: EmbeddedRequest): Promise<ColorPresentation[]> {
  const document = toTextDocument(request.activeVirtual);
  if (request.activeVirtual.languageId !== "css") {
    return [];
  }
  const stylesheet = cssService.parseStylesheet(document);
  return cssService.getColorPresentations(
    document,
    stylesheet,
    requestColor(request),
    requestRange(request),
  );
}

async function linkedEditingRanges(request: EmbeddedRequest): Promise<{ ranges: Range[] } | null> {
  const document = toTextDocument(request.activeVirtual);
  if (request.activeVirtual.languageId !== "html") {
    return null;
  }
  const htmlDocument = htmlService.parseHTMLDocument(document);
  const ranges = htmlService.findLinkedEditingRanges(
    document,
    requestPosition(request),
    htmlDocument,
  );
  return ranges ? { ranges } : null;
}

function htmlDiagnostics(virtual: VirtualDocument): Diagnostic[] {
  const document = toTextDocument(virtual);
  const scanner = htmlService.createScanner(virtual.text);
  const diagnostics: Diagnostic[] = [];
  let token = scanner.scan();
  while (token !== TokenType.EOS) {
    const error = scanner.getTokenError();
    if (error) {
      diagnostics.push({
        severity: DiagnosticSeverity.Warning,
        range: {
          start: document.positionAt(scanner.getTokenOffset()),
          end: document.positionAt(scanner.getTokenEnd()),
        },
        message: error,
        source: "asp-lsp-html",
      });
    }
    token = scanner.scan();
  }
  return diagnostics;
}

function cssDiagnostics(virtual: VirtualDocument): Diagnostic[] {
  const document = toTextDocument(virtual);
  const stylesheet = cssService.parseStylesheet(document);
  return cssService
    .doValidation(document, stylesheet)
    .map((diagnostic) => ({ ...diagnostic, source: "asp-lsp-css" }));
}

function jsCompletion(
  request: EmbeddedRequest,
  document: TextDocument,
  position: Position,
): CompletionItem[] {
  const project = createLanguageServiceProject(request);
  const entries = project.service.getCompletionsAtPosition(
    project.fileName,
    document.offsetAt(position),
    {},
  );
  return (
    entries?.entries.map((entry) => ({
      label: entry.name,
      kind: tsCompletionItemKind(entry.kind),
      sortText: entry.sortText,
    })) ?? []
  );
}

function jsHover(
  request: EmbeddedRequest,
  document: TextDocument,
  position: Position,
): Hover | null {
  const project = createLanguageServiceProject(request);
  const info = project.service.getQuickInfoAtPosition(project.fileName, document.offsetAt(position));
  if (!info) {
    return null;
  }
  const display = ts.displayPartsToString(info.displayParts ?? []);
  const documentation = ts.displayPartsToString(info.documentation ?? []);
  return {
    contents: {
      kind: MarkupKind.Markdown,
      value: documentation ? `\`\`\`javascript\n${display}\n\`\`\`\n\n${documentation}` : display,
    },
    range: {
      start: document.positionAt(info.textSpan.start),
      end: document.positionAt(info.textSpan.start + info.textSpan.length),
    },
  };
}

function jsDefinition(
  request: EmbeddedRequest,
  document: TextDocument,
  position: Position,
): Location[] {
  const project = createLanguageServiceProject(request);
  return (
    project.service
      .getDefinitionAtPosition(project.fileName, document.offsetAt(position))
      ?.flatMap((definition) => tsDefinitionToLocation(request, definition)) ?? []
  );
}

function jsDocumentHighlights(
  request: EmbeddedRequest,
  document: TextDocument,
  position: Position,
): DocumentHighlight[] {
  const project = createLanguageServiceProject(request);
  const highlights =
    project.service.getDocumentHighlights(project.fileName, document.offsetAt(position), [
      project.fileName,
    ]) ?? [];
  return highlights.flatMap((highlight) =>
    highlight.highlightSpans.map((span) => ({
      range: textSpanToRange(document, span.textSpan),
      kind: span.kind === "writtenReference" ? 3 : 2,
    })),
  );
}

function jsSelectionRange(
  request: EmbeddedRequest,
  document: TextDocument,
  position: Position,
): SelectionRange {
  const project = createLanguageServiceProject(request);
  return tsSelectionRangeToLsp(
    document,
    project.service.getSmartSelectionRange(project.fileName, document.offsetAt(position)),
  );
}

function jsPrepareRename(
  request: EmbeddedRequest,
  document: TextDocument,
  position: Position,
): Range | null {
  const project = createLanguageServiceProject(request);
  const info = project.service.getRenameInfo(project.fileName, document.offsetAt(position), {});
  if (!info.canRename) {
    return null;
  }
  return textSpanToRange(document, info.triggerSpan);
}

function jsRename(
  request: EmbeddedRequest,
  document: TextDocument,
  position: Position,
  newName: string,
): WorkspaceEdit | null {
  const project = createLanguageServiceProject(request);
  const locations = project.service.findRenameLocations(
    project.fileName,
    document.offsetAt(position),
    false,
    false,
    {},
  );
  if (!locations || locations.length === 0) {
    return null;
  }
  const changes: NonNullable<WorkspaceEdit["changes"]> = {};
  for (const location of locations) {
    const uri = fileNameToVirtualUri(request, location.fileName);
    if (!uri) {
      continue;
    }
    (changes[uri] ??= []).push({
      range: textSpanToRange(documentForVirtualUri(request, uri), location.textSpan),
      newText: newName,
    });
  }
  return Object.keys(changes).length === 0 ? null : { changes };
}

function jsFormatting(
  request: EmbeddedRequest,
  document: TextDocument,
  range: Range | undefined,
  options: { tabSize: number; insertSpaces: boolean },
): TextEdit[] {
  const project = createLanguageServiceProject(request);
  const formatOptions = tsFormatOptions(options);
  const changes =
    request.operation === "onTypeFormatting"
      ? project.service.getFormattingEditsAfterKeystroke(
          project.fileName,
          document.offsetAt(requestPosition(request)),
          requestCharacter(request),
          formatOptions,
        )
      : project.service.getFormattingEditsForRange(
          project.fileName,
          range ? document.offsetAt(range.start) : 0,
          range ? document.offsetAt(range.end) : document.getText().length,
          formatOptions,
        );
  return changes.map((change) => textChangeToTextEdit(document, change));
}

function jsSemanticTokens(
  request: EmbeddedRequest,
  document: TextDocument,
): DecodedSemanticToken[] {
  const project = createLanguageServiceProject(request);
  const spans = project.service.getEncodedSemanticClassifications(
    project.fileName,
    {
      start: 0,
      length: document.getText().length,
    },
    ts.SemanticClassificationFormat.TwentyTwenty,
  ).spans;
  const tokens: DecodedSemanticToken[] = [];
  for (let index = 0; index + 2 < spans.length; index += 3) {
    const token = jsSemanticTokenFromClassification(spans[index + 2]);
    if (!token) {
      continue;
    }
    tokens.push({
      range: {
        start: document.positionAt(spans[index]),
        end: document.positionAt(spans[index] + spans[index + 1]),
      },
      tokenType: token.tokenType,
      tokenModifiers: token.tokenModifiers,
    });
  }
  return tokens;
}

async function jsDiagnostics(request: EmbeddedRequest): Promise<Diagnostic[]> {
  const syntax = jsSyntaxDiagnostics(request.activeVirtual);
  const semantic = request.settings.checkJs === true ? jsSemanticDiagnostics(request) : [];
  const semanticKeys = new Set(semantic.map(tsDiagnosticKey));
  const unused =
    request.settings.javascript?.unusedDiagnostics === false
      ? []
      : lightweightJsUnusedDiagnostics(request.activeVirtual).filter(
          (diagnostic) => !semanticKeys.has(tsDiagnosticKey(diagnostic)),
        );
  return [
    ...syntax.map((diagnostic) => tsDiagnosticToLsp(request.activeVirtual, diagnostic)),
    ...semantic.map((diagnostic) => tsDiagnosticToLsp(request.activeVirtual, diagnostic)),
    ...unused.map((diagnostic) =>
      tsDiagnosticToLsp(request.activeVirtual, diagnostic, {
        severity: DiagnosticSeverity.Hint,
        source: "asp-lsp-typescript-unused",
      }),
    ),
  ].filter((diagnostic): diagnostic is Diagnostic => Boolean(diagnostic));
}

function jsSyntaxDiagnostics(virtual: VirtualDocument): CachedTsDiagnostic[] {
  const sourceFile = ts.createSourceFile(
    jsVirtualFileName(virtual.uri),
    virtual.text,
    ts.ScriptTarget.ESNext,
    true,
    ts.ScriptKind.JS,
  );
  const parseDiagnostics =
    (sourceFile as ts.SourceFile & { parseDiagnostics?: readonly ts.Diagnostic[] })
      .parseDiagnostics ?? [];
  return parseDiagnostics.map(cacheTsDiagnostic);
}

function jsSemanticDiagnostics(request: EmbeddedRequest): CachedTsDiagnostic[] {
  const project = createLanguageServiceProject(request);
  return project.service.getSemanticDiagnostics(project.fileName).map(cacheTsDiagnostic);
}

function lightweightJsUnusedDiagnostics(virtual: VirtualDocument): CachedTsDiagnostic[] {
  const fileName = normalizeFileName(jsVirtualFileName(virtual.uri));
  const files = new Map([[fileName, virtual.text]]);
  const host: ts.LanguageServiceHost = {
    getScriptFileNames: () => [fileName],
    getScriptVersion: () => "0",
    getScriptSnapshot: (requested) => {
      const text = files.get(normalizeFileName(requested));
      return text === undefined ? undefined : ts.ScriptSnapshot.fromString(text);
    },
    getScriptKind: () => ts.ScriptKind.JS,
    getCurrentDirectory: () => path.dirname(uriToFileName(virtualSourceUri(virtual))),
    getCompilationSettings: () => ({
      allowJs: true,
      checkJs: true,
      noEmit: true,
      noLib: true,
      noUnusedLocals: true,
      noUnusedParameters: true,
      target: ts.ScriptTarget.ESNext,
      module: ts.ModuleKind.CommonJS,
      types: [],
    }),
    getDefaultLibFileName: () => "",
    fileExists: (requested) => files.has(normalizeFileName(requested)),
    readFile: (requested) => files.get(normalizeFileName(requested)),
    readDirectory: () => [],
    directoryExists: () => true,
    getDirectories: () => [],
  };
  const service = ts.createLanguageService(host);
  try {
    return service
      .getSemanticDiagnostics(fileName)
      .filter((diagnostic) => tsUnusedDiagnosticCodes.has(diagnostic.code))
      .map(cacheTsDiagnostic);
  } finally {
    service.dispose();
  }
}

function createLanguageServiceProject(request: EmbeddedRequest): CachedLanguageServiceProject {
  const activeFile = normalizeFileName(jsVirtualFileName(request.activeVirtual.uri));
  const ownerFile = uriToFileName(virtualSourceUri(request.activeVirtual));
  const config = projectConfig(ownerFile, request.settings, request.workspaceRoots);
  const files = new Map<string, { text: string; version: string }>();
  for (const virtual of [...request.openVirtuals, request.activeVirtual]) {
    files.set(normalizeFileName(jsVirtualFileName(virtual.uri)), {
      text: virtual.text,
      version: textFingerprint(virtual.text),
    });
  }
  for (const fileName of config.fileNames) {
    const normalized = normalizeFileName(fileName);
    if (files.has(normalized) || !cachedFileExists(normalized)) {
      continue;
    }
    const text = cachedReadFile(normalized);
    if (text === undefined) {
      continue;
    }
    const stat = fs.statSync(normalized, { throwIfNoEntry: false });
    files.set(normalized, {
      text,
      version: stat ? `${stat.mtimeMs}:${stat.size}` : "0",
    });
  }
  const cacheKey = languageServiceProjectCacheKey(request, activeFile, config, files);
  const cached = languageServiceProjectCache.get(cacheKey);
  if (cached) {
    cached.lastUsed = ++languageServiceProjectCacheTick;
    return cached;
  }
  const host: ts.LanguageServiceHost = {
    getScriptFileNames: () => [...new Set([activeFile, ...files.keys()])],
    getProjectVersion: () => [...files.values()].map((file) => file.version).join("|"),
    getScriptVersion: (requested) => files.get(normalizeFileName(requested))?.version ?? "0",
    getScriptSnapshot: (requested) => {
      const text = files.get(normalizeFileName(requested))?.text ?? cachedReadFile(requested);
      return text === undefined ? undefined : ts.ScriptSnapshot.fromString(text);
    },
    getScriptKind: scriptKindForFileName,
    getCurrentDirectory: () => config.currentDirectory,
    getCompilationSettings: () => config.options,
    getDefaultLibFileName: (compilerOptions) => ts.getDefaultLibFilePath(compilerOptions),
    fileExists: (requested) =>
      files.has(normalizeFileName(requested)) || cachedFileExists(requested),
    readFile: (requested) =>
      files.get(normalizeFileName(requested))?.text ?? cachedReadFile(requested),
    readDirectory: cachedReadDirectory,
    directoryExists: cachedDirectoryExists,
    getDirectories: cachedGetDirectories,
    realpath: cachedRealpath,
    useCaseSensitiveFileNames: () => ts.sys.useCaseSensitiveFileNames,
  };
  const project = {
    service: ts.createLanguageService(host),
    fileName: activeFile,
    lastUsed: ++languageServiceProjectCacheTick,
  };
  languageServiceProjectCache.set(cacheKey, project);
  pruneLanguageServiceProjectCache();
  return project;
}

function languageServiceProjectCacheKey(
  request: EmbeddedRequest,
  activeFile: string,
  config: JsProjectConfig,
  files: Map<string, { text: string; version: string }>,
): string {
  return JSON.stringify({
    project: request.projectFingerprint ?? `generation:${request.projectGeneration}`,
    activeFile,
    currentDirectory: config.currentDirectory,
    options: config.options,
    fileNames: config.fileNames.map(normalizeFileName).sort(),
    files: [...files.entries()]
      .map(([fileName, file]) => [fileName, file.version])
      .sort(([left], [right]) => left.localeCompare(right)),
  });
}

function pruneLanguageServiceProjectCache(): void {
  while (languageServiceProjectCache.size > maxLanguageServiceProjectCacheEntries) {
    const oldest = [...languageServiceProjectCache.entries()].sort(
      (left, right) => left[1].lastUsed - right[1].lastUsed,
    )[0];
    oldest[1].service.dispose();
    languageServiceProjectCache.delete(oldest[0]);
  }
}

function clearLanguageServiceProjectCache(): void {
  for (const project of languageServiceProjectCache.values()) {
    project.service.dispose();
  }
  languageServiceProjectCache.clear();
}

function projectConfig(
  ownerFile: string,
  settings: AspSettings,
  workspaceRoots: string[],
): JsProjectConfig {
  const defaultOptions: ts.CompilerOptions = {
    allowJs: true,
    checkJs: settings.checkJs ?? false,
    noEmit: true,
    target: ts.ScriptTarget.ESNext,
    module: ts.ModuleKind.CommonJS,
    moduleResolution: ts.ModuleResolutionKind.Node10,
    lib: browserJavaScriptLibs,
  };
  if (settings.javascript?.ignoreProjectConfig === true) {
    return defaultProjectConfig(ownerFile, workspaceRoots, defaultOptions);
  }
  const ownerDirectory = path.dirname(ownerFile);
  const configPath =
    ts.findConfigFile(ownerDirectory, cachedFileExists, "tsconfig.json") ??
    ts.findConfigFile(ownerDirectory, cachedFileExists, "jsconfig.json");
  if (!configPath) {
    return defaultProjectConfig(ownerFile, workspaceRoots, defaultOptions);
  }
  const parsed = ts.parseJsonConfigFileContent(
    ts.readConfigFile(configPath, cachedReadFile).config ?? {},
    {
      useCaseSensitiveFileNames: ts.sys.useCaseSensitiveFileNames,
      fileExists: cachedFileExists,
      readFile: cachedReadFile,
      readDirectory: cachedReadDirectory,
    },
    path.dirname(configPath),
    defaultOptions,
  );
  if (parsed.errors.length > 0 && workspaceRoots.length === 0) {
    return defaultProjectConfig(ownerFile, workspaceRoots, defaultOptions);
  }
  return {
    fileNames: parsed.fileNames,
    options: parsed.options,
    currentDirectory: path.dirname(configPath),
  };
}

function tsDefinitionToLocation(
  request: EmbeddedRequest,
  definition: ts.DefinitionInfo,
): Location[] {
  const activeFile = normalizeFileName(jsVirtualFileName(request.activeVirtual.uri));
  const definitionFile = normalizeFileName(definition.fileName);
  if (definitionFile === activeFile) {
    const document = toTextDocument(request.activeVirtual);
    return [
      {
        uri: request.activeVirtual.uri,
        range: {
          start: document.positionAt(definition.textSpan.start),
          end: document.positionAt(definition.textSpan.start + definition.textSpan.length),
        },
      },
    ];
  }
  const text = cachedReadFile(definitionFile);
  if (text === undefined) {
    return [];
  }
  const document = TextDocument.create(fileNameToUri(definitionFile), "javascript", 0, text);
  return [
    {
      uri: document.uri,
      range: {
        start: document.positionAt(definition.textSpan.start),
        end: document.positionAt(definition.textSpan.start + definition.textSpan.length),
      },
    },
  ];
}

function textSpanToRange(document: TextDocument, span: ts.TextSpan): Range {
  return {
    start: document.positionAt(span.start),
    end: document.positionAt(span.start + span.length),
  };
}

function textChangeToTextEdit(document: TextDocument, change: ts.TextChange): TextEdit {
  return {
    range: textSpanToRange(document, change.span),
    newText: change.newText,
  };
}

function tsSelectionRangeToLsp(document: TextDocument, range: ts.SelectionRange): SelectionRange {
  return {
    range: textSpanToRange(document, range.textSpan),
    parent: range.parent ? tsSelectionRangeToLsp(document, range.parent) : undefined,
  };
}

function tsFormatOptions(options: { tabSize: number; insertSpaces: boolean }): ts.FormatCodeSettings {
  return {
    tabSize: options.tabSize,
    indentSize: options.tabSize,
    convertTabsToSpaces: options.insertSpaces,
    newLineCharacter: "\n",
  };
}

function fileNameToVirtualUri(request: EmbeddedRequest, fileName: string): string | undefined {
  const normalized = normalizeFileName(fileName);
  return [request.activeVirtual, ...request.openVirtuals].find(
    (virtual) =>
      (virtual.languageId === "javascript" || virtual.languageId === "jscript") &&
      normalizeFileName(jsVirtualFileName(virtual.uri)) === normalized,
  )?.uri;
}

function documentForVirtualUri(request: EmbeddedRequest, uri: string): TextDocument {
  const virtual = [request.activeVirtual, ...request.openVirtuals].find((item) => item.uri === uri);
  if (virtual) {
    return toTextDocument(virtual);
  }
  const fileName = uriToFileName(uri);
  return TextDocument.create(uri, "javascript", 0, cachedReadFile(fileName) ?? "");
}

function jsSemanticTokenFromClassification(
  classification: number,
): { tokenType: number; tokenModifiers: number } | undefined {
  const typeIndex = (classification >> 8) - 1;
  const tokenType = jsSemanticTokenType(typeIndex);
  if (tokenType === undefined) {
    return undefined;
  }
  return { tokenType, tokenModifiers: jsSemanticTokenModifiers(classification & 255) };
}

function jsSemanticTokenType(typeIndex: number): number | undefined {
  switch (typeIndex) {
    case 0:
      return 4;
    case 1:
      return 12;
    case 2:
      return 11;
    case 3:
      return 10;
    case 4:
      return 15;
    case 5:
      return 14;
    case 6:
      return 2;
    case 7:
      return 1;
    case 8:
      return 13;
    case 9:
      return 6;
    case 10:
      return 3;
    case 11:
      return 5;
    default:
      return undefined;
  }
}

function jsSemanticTokenModifiers(modifierSet: number): number {
  let modifiers = 0;
  if (modifierSet & (1 << 3)) {
    modifiers |= 1 << 2;
  }
  if (modifierSet & (1 << 4)) {
    modifiers |= 1 << 3;
  }
  return modifiers;
}

function tsCompletionItemKind(kind: string): CompletionItemKind {
  switch (kind) {
    case ts.ScriptElementKind.functionElement:
    case ts.ScriptElementKind.memberFunctionElement:
      return CompletionItemKind.Function;
    case ts.ScriptElementKind.classElement:
      return CompletionItemKind.Class;
    case ts.ScriptElementKind.constElement:
      return CompletionItemKind.Constant;
    case ts.ScriptElementKind.memberVariableElement:
    case ts.ScriptElementKind.variableElement:
    case ts.ScriptElementKind.letElement:
      return CompletionItemKind.Variable;
    case ts.ScriptElementKind.keyword:
      return CompletionItemKind.Keyword;
    default:
      return CompletionItemKind.Property;
  }
}

function defaultProjectConfig(
  ownerFile: string,
  workspaceRoots: string[],
  options: ts.CompilerOptions,
): JsProjectConfig {
  const ownerDirectory = path.dirname(ownerFile);
  const roots = workspaceRoots.length > 0 ? workspaceRoots.map(uriToFileName) : [ownerDirectory];
  return {
    fileNames: roots.flatMap((root) =>
      cachedReadDirectory(root, [".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx", ".mts", ".cts"]),
    ),
    options,
    currentDirectory: ownerDirectory,
  };
}

function tsDiagnosticToLsp(
  virtual: VirtualDocument,
  diagnostic: CachedTsDiagnostic,
  override: { severity?: DiagnosticSeverity; source?: string } = {},
): Diagnostic | undefined {
  if (diagnostic.start === undefined || diagnostic.length === undefined) {
    return undefined;
  }
  const document = toTextDocument(virtual);
  return {
    severity:
      override.severity ??
      (diagnostic.category === ts.DiagnosticCategory.Error
        ? DiagnosticSeverity.Error
        : DiagnosticSeverity.Warning),
    range: {
      start: document.positionAt(diagnostic.start),
      end: document.positionAt(diagnostic.start + diagnostic.length),
    },
    message: diagnostic.messageText,
    code: diagnostic.code,
    source: override.source ?? "asp-lsp-typescript",
    tags: diagnostic.reportsUnnecessary === true ? [DiagnosticTag.Unnecessary] : undefined,
  };
}

function cacheTsDiagnostic(diagnostic: ts.Diagnostic): CachedTsDiagnostic {
  return {
    code: diagnostic.code,
    category: diagnostic.category,
    messageText: ts.flattenDiagnosticMessageText(diagnostic.messageText, "\n"),
    start: diagnostic.start,
    length: diagnostic.length,
    reportsUnnecessary: diagnostic.reportsUnnecessary === true,
  };
}

function toTextDocument(virtual: VirtualDocument): TextDocument {
  return TextDocument.create(virtual.uri, virtual.languageId, 0, virtual.text);
}

function requestPosition(request: EmbeddedRequest): Position {
  const position = (request.params as { position?: unknown } | undefined)?.position;
  if (!position || typeof position !== "object") {
    throw new Error("params.position is required");
  }
  const line = (position as { line?: unknown }).line;
  const character = (position as { character?: unknown }).character;
  if (typeof line !== "number" || typeof character !== "number") {
    throw new Error("params.position must be an LSP position");
  }
  return { line, character };
}

function requestPositions(request: EmbeddedRequest): Position[] {
  const positions = (request.params as { positions?: unknown } | undefined)?.positions;
  if (!Array.isArray(positions) || !positions.every(isPosition)) {
    throw new Error("params.positions must be an LSP position array");
  }
  return positions;
}

function requestRange(request: EmbeddedRequest): Range {
  const range = (request.params as { range?: unknown } | undefined)?.range;
  if (!range || typeof range !== "object") {
    throw new Error("params.range is required");
  }
  const start = (range as { start?: unknown }).start;
  const end = (range as { end?: unknown }).end;
  if (!isPosition(start) || !isPosition(end)) {
    throw new Error("params.range must be an LSP range");
  }
  return { start, end };
}

function requestFormatRange(request: EmbeddedRequest): Range | undefined {
  const range = (request.params as { range?: unknown } | undefined)?.range;
  if (range === undefined || range === null) {
    return undefined;
  }
  if (typeof range !== "object") {
    throw new Error("params.range must be an LSP range");
  }
  const start = (range as { start?: unknown }).start;
  const end = (range as { end?: unknown }).end;
  if (!isPosition(start) || !isPosition(end)) {
    throw new Error("params.range must be an LSP range");
  }
  return { start, end };
}

function requestNewName(request: EmbeddedRequest): string {
  const newName = (request.params as { newName?: unknown } | undefined)?.newName;
  if (typeof newName !== "string") {
    throw new Error("params.newName is required");
  }
  return newName;
}

function requestCharacter(request: EmbeddedRequest): string {
  const character = (request.params as { character?: unknown } | undefined)?.character;
  if (typeof character !== "string") {
    throw new Error("params.character is required");
  }
  return character;
}

function requestFormatOptions(request: EmbeddedRequest): { tabSize: number; insertSpaces: boolean } {
  const options = (request.params as { options?: unknown } | undefined)?.options;
  if (!options || typeof options !== "object") {
    return { tabSize: 2, insertSpaces: true };
  }
  const tabSize = (options as { tabSize?: unknown }).tabSize;
  const insertSpaces = (options as { insertSpaces?: unknown }).insertSpaces;
  return {
    tabSize: typeof tabSize === "number" ? tabSize : 2,
    insertSpaces: typeof insertSpaces === "boolean" ? insertSpaces : true,
  };
}

function requestColor(request: EmbeddedRequest): Color {
  const color = (request.params as { color?: unknown } | undefined)?.color;
  if (!color || typeof color !== "object") {
    throw new Error("params.color is required");
  }
  const red = (color as { red?: unknown }).red;
  const green = (color as { green?: unknown }).green;
  const blue = (color as { blue?: unknown }).blue;
  const alpha = (color as { alpha?: unknown }).alpha;
  if (
    typeof red !== "number" ||
    typeof green !== "number" ||
    typeof blue !== "number" ||
    typeof alpha !== "number"
  ) {
    throw new Error("params.color must be an LSP color");
  }
  return { red, green, blue, alpha };
}

function isPosition(value: unknown): value is Position {
  if (!value || typeof value !== "object") {
    return false;
  }
  return (
    typeof (value as { line?: unknown }).line === "number" &&
    typeof (value as { character?: unknown }).character === "number"
  );
}

function rangeContainsPosition(range: Range, position: Position): boolean {
  return (
    comparePosition(range.start, position) <= 0 &&
    comparePosition(position, range.end) <= 0
  );
}

function comparePosition(left: Position, right: Position): number {
  return left.line - right.line || left.character - right.character;
}

function virtualSourceUri(virtual: VirtualDocument): string {
  return virtual.uri.replace(`.${virtual.languageId}.virtual`, "");
}

function uriToFileName(uri: string): string {
  return uri.startsWith("file://") ? fileURLToPath(uri) : uri;
}

function fileNameToUri(fileName: string): string {
  return pathToFileURL(fileName).toString();
}

function jsVirtualFileName(uri: string): string {
  const fileName = uri.startsWith("file://") ? fileURLToPath(uri) : uri;
  return fileName.replace(/\.(javascript|jscript)\.virtual$/, ".$1.js");
}

function normalizeFileName(fileName: string): string {
  return path.resolve(fileName);
}

function scriptKindForFileName(fileName: string): ts.ScriptKind {
  const extension = path.extname(fileName).toLowerCase();
  if (extension === ".ts" || extension === ".mts" || extension === ".cts") {
    return ts.ScriptKind.TS;
  }
  if (extension === ".tsx") {
    return ts.ScriptKind.TSX;
  }
  if (extension === ".jsx") {
    return ts.ScriptKind.JSX;
  }
  return ts.ScriptKind.JS;
}

function resetCachesForProject(request: EmbeddedRequest): void {
  const projectCacheKey = request.projectFingerprint ?? `generation:${request.projectGeneration}`;
  if (currentProjectCacheKey === projectCacheKey) {
    return;
  }
  currentProjectCacheKey = projectCacheKey;
  recordCacheStat("generationReset");
  if (currentRequestStats) {
    currentRequestStats.resetReason =
      request.projectResetReason ??
      (request.projectFingerprint ? "projectFingerprint" : "projectGeneration");
    currentRequestStats.projectFingerprint = projectCacheKey;
  }
  fileExistsCache.clear();
  readFileCache.clear();
  directoryExistsCache.clear();
  directoriesCache.clear();
  readDirectoryCache.clear();
  realpathCache.clear();
  clearLanguageServiceProjectCache();
}

function createCacheStats(): SidecarCacheStats {
  return {
    generationReset: 0,
    fileExistsHit: 0,
    fileExistsMiss: 0,
    readFileHit: 0,
    readFileMiss: 0,
    directoryExistsHit: 0,
    directoryExistsMiss: 0,
    getDirectoriesHit: 0,
    getDirectoriesMiss: 0,
    readDirectoryHit: 0,
    readDirectoryMiss: 0,
    realpathHit: 0,
    realpathMiss: 0,
  };
}

function recordCacheStat(stat: SidecarCacheStatCounter): void {
  if (currentRequestStats) {
    currentRequestStats[stat] += 1;
  }
}

function cachedFileExists(fileName: string): boolean {
  const normalized = normalizeFileName(fileName);
  const cached = fileExistsCache.get(normalized);
  if (cached !== undefined) {
    recordCacheStat("fileExistsHit");
    return cached;
  }
  recordCacheStat("fileExistsMiss");
  const exists = fs.statSync(normalized, { throwIfNoEntry: false })?.isFile() === true;
  fileExistsCache.set(normalized, exists);
  return exists;
}

function cachedReadFile(fileName: string): string | undefined {
  const normalized = normalizeFileName(fileName);
  if (readFileCache.has(normalized)) {
    recordCacheStat("readFileHit");
    return readFileCache.get(normalized);
  }
  recordCacheStat("readFileMiss");
  const text = fs.existsSync(normalized) ? fs.readFileSync(normalized, "utf8") : undefined;
  readFileCache.set(normalized, text);
  return text;
}

function cachedDirectoryExists(directory: string): boolean {
  const normalized = normalizeFileName(directory);
  const cached = directoryExistsCache.get(normalized);
  if (cached !== undefined) {
    recordCacheStat("directoryExistsHit");
    return cached;
  }
  recordCacheStat("directoryExistsMiss");
  const exists = fs.statSync(normalized, { throwIfNoEntry: false })?.isDirectory() === true;
  directoryExistsCache.set(normalized, exists);
  return exists;
}

function cachedGetDirectories(directory: string): string[] {
  const normalized = normalizeFileName(directory);
  if (directoriesCache.has(normalized)) {
    recordCacheStat("getDirectoriesHit");
    return directoriesCache.get(normalized) ?? [];
  }
  recordCacheStat("getDirectoriesMiss");
  const entries = fs.existsSync(normalized)
    ? fs
        .readdirSync(normalized, { withFileTypes: true })
        .filter((entry) => entry.isDirectory())
        .map((entry) => entry.name)
    : [];
  directoriesCache.set(normalized, entries);
  return entries;
}

function cachedReadDirectory(rootDir: string, extensions?: readonly string[]): string[] {
  const normalized = normalizeFileName(rootDir);
  const key = JSON.stringify({ rootDir: normalized, extensions });
  if (readDirectoryCache.has(key)) {
    recordCacheStat("readDirectoryHit");
    return readDirectoryCache.get(key) ?? [];
  }
  recordCacheStat("readDirectoryMiss");
  const files = cachedDirectoryExists(normalized)
    ? ts.sys.readDirectory(normalized, extensions)
    : [];
  readDirectoryCache.set(key, files);
  return files;
}

function cachedRealpath(fileName: string): string {
  const normalized = normalizeFileName(fileName);
  if (realpathCache.has(normalized)) {
    recordCacheStat("realpathHit");
    return realpathCache.get(normalized) ?? normalized;
  }
  recordCacheStat("realpathMiss");
  const realpath = ts.sys.realpath ? ts.sys.realpath(normalized) : normalized;
  realpathCache.set(normalized, realpath);
  return realpath;
}

function textFingerprint(text: string): string {
  return `${text.length}:${text.slice(0, 64)}:${text.slice(-64)}`;
}

function tsDiagnosticKey(diagnostic: CachedTsDiagnostic): string {
  return [diagnostic.code, diagnostic.start ?? -1, diagnostic.length ?? -1].join(":");
}

function writeResponse(response: EmbeddedResponse): void {
  const payload = Buffer.from(JSON.stringify(response), "utf8");
  const frame = Buffer.allocUnsafe(5 + payload.length);
  frame.writeUInt32LE(1 + payload.length, 0);
  frame[4] = frameKindJson;
  payload.copy(frame, 5);
  process.stdout.write(frame);
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

export const __test = {
  handleRequest,
  createLanguageServiceProject,
  resetCachesForProject,
  clearLanguageServiceProjectCache,
  languageServiceProjectCacheSize: () => languageServiceProjectCache.size,
};
