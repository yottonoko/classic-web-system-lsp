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
  type DocumentSymbol,
  type FoldingRange,
  type Hover,
  type Location,
  type Position,
  type Range,
  type SymbolInformation,
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

interface AspSettings {
  checkJs?: boolean;
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

let inputBuffer = Buffer.alloc(0);
let currentProjectGeneration: number | undefined;
const fileExistsCache = new Map<string, boolean>();
const readFileCache = new Map<string, string | undefined>();
const directoryExistsCache = new Map<string, boolean>();
const directoriesCache = new Map<string, string[]>();
const readDirectoryCache = new Map<string, string[]>();
const realpathCache = new Map<string, string>();
let currentRequestStats: SidecarCacheStats | undefined;

process.stdin.on("data", (chunk: Buffer) => {
  inputBuffer = Buffer.concat([inputBuffer, chunk]);
  for (const payload of readFrames()) {
    void handlePayload(payload);
  }
});

process.stdin.on("end", () => {
  process.exit(0);
});

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
    resetCachesForProjectGeneration(request.projectGeneration);
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
  try {
    const fileName = normalizeFileName(jsVirtualFileName(request.activeVirtual.uri));
    const entries = project.service.getCompletionsAtPosition(
      fileName,
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
  } finally {
    project.service.dispose();
  }
}

function jsHover(
  request: EmbeddedRequest,
  document: TextDocument,
  position: Position,
): Hover | null {
  const project = createLanguageServiceProject(request);
  try {
    const fileName = normalizeFileName(jsVirtualFileName(request.activeVirtual.uri));
    const info = project.service.getQuickInfoAtPosition(fileName, document.offsetAt(position));
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
  } finally {
    project.service.dispose();
  }
}

function jsDefinition(
  request: EmbeddedRequest,
  document: TextDocument,
  position: Position,
): Location[] {
  const project = createLanguageServiceProject(request);
  try {
    const fileName = normalizeFileName(jsVirtualFileName(request.activeVirtual.uri));
    return (
      project.service
        .getDefinitionAtPosition(fileName, document.offsetAt(position))
        ?.flatMap((definition) => tsDefinitionToLocation(request, definition)) ?? []
    );
  } finally {
    project.service.dispose();
  }
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
  try {
    const fileName = normalizeFileName(jsVirtualFileName(request.activeVirtual.uri));
    return project.service.getSemanticDiagnostics(fileName).map(cacheTsDiagnostic);
  } finally {
    project.service.dispose();
  }
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

function createLanguageServiceProject(request: EmbeddedRequest): { service: ts.LanguageService } {
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
  return { service: ts.createLanguageService(host) };
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

function resetCachesForProjectGeneration(projectGeneration: number): void {
  if (currentProjectGeneration === projectGeneration) {
    return;
  }
  currentProjectGeneration = projectGeneration;
  recordCacheStat("generationReset");
  fileExistsCache.clear();
  readFileCache.clear();
  directoryExistsCache.clear();
  directoriesCache.clear();
  readDirectoryCache.clear();
  realpathCache.clear();
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

function recordCacheStat(stat: keyof SidecarCacheStats): void {
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
