#!/usr/bin/env node
import fs from "node:fs";
import path from "node:path";
import { analyse, detect } from "chardet";
import {
  CodeActionKind,
  CompletionItemKind,
  createConnection,
  DiagnosticSeverity,
  DocumentHighlightKind,
  DocumentSymbol,
  FileChangeType,
  FoldingRange,
  Hover,
  InlineValueVariableLookup,
  InitializeParams,
  InitializeResult,
  Location,
  MonikerKind,
  ProposedFeatures,
  ReferenceParams,
  SemanticTokensBuilder,
  SymbolInformation,
  SymbolKind,
  TextDocumentPositionParams,
  TextDocuments,
  TextDocumentSyncKind,
  UniquenessLevel,
} from "vscode-languageserver/node";
import type {
  CallHierarchyIncomingCall,
  CallHierarchyItem,
  CallHierarchyOutgoingCall,
  CodeAction,
  CodeActionContext,
  CodeLens,
  Color,
  ColorInformation,
  ColorPresentation,
  CompletionItem,
  Diagnostic,
  DocumentLink,
  DocumentHighlight,
  FormattingOptions,
  InlineValue,
  InlineValueParams,
  InlayHint,
  LinkedEditingRanges,
  Moniker,
  Position,
  Range,
  RenameParams,
  SemanticTokens,
  SemanticTokensDelta,
  SelectionRange,
  SignatureHelp,
  TextEdit,
  TextDocumentContentChangeEvent,
  TypeHierarchyItem,
  WorkspaceEdit,
} from "vscode-languageserver/node";
import { TextDocument } from "vscode-languageserver-textdocument";
import {
  analyzeVbscript,
  buildVbTypeEnvironment,
  buildVirtualDocuments,
  collectVbscriptSymbols,
  createLocalizer,
  formatAspDocument,
  formatAspRange,
  getVbscriptCompletions,
  getVbscriptDefinition,
  getVbscriptDocumentHighlights,
  getVbscriptDocumentSymbols,
  getVbscriptHover,
  getVbscriptImplementation,
  getVbscriptIncomingCalls,
  getVbscriptInlayHints,
  getVbscriptOutgoingCalls,
  getVbscriptRenameRange,
  getVbscriptReferences,
  getVbscriptSelectionRanges,
  getVbscriptSemanticTokens,
  getVbscriptSignatureHelp,
  getVbscriptTypeDefinition,
  parseAspDocument,
  prepareVbscriptCallHierarchy,
  resolveVbscriptCompletionItem,
  updateAspParsedDocument,
  type AspFormattingOptions,
  type AspIncrementalChange,
  type AspLegacyEncoding,
  type AspLocale,
  type AspLocaleSetting,
  type AspParsedDocument,
  type AspSettings,
  type AspRegion,
  type VirtualDocument,
  type VbProjectContext,
  type VbSymbol,
  type VbSymbolKind,
  type VbType,
} from "@asp-lsp/core";
import { getCSSLanguageService } from "vscode-css-languageservice";
import {
  getLanguageService as getHtmlLanguageService,
  TokenType,
} from "vscode-html-languageservice";
import ts from "typescript";

const connection = createConnection(ProposedFeatures.all);
const pendingDocumentChanges = new Map<string, PendingDocumentChange>();
const documents = new TextDocuments({
  create: TextDocument.create,
  update(document, changes, version) {
    pendingDocumentChanges.set(document.uri, {
      previousText: document.getText(),
      changes,
      version,
    });
    return TextDocument.update(document, changes, version);
  },
});
const htmlService = getHtmlLanguageService();
const cssService = getCSSLanguageService();
const settingsByUri = new Map<string, AspSettings>();
const includeDocumentCache = new Map<string, { mtimeMs: number; parsed: AspParsedDocument }>();
const workspaceIndex = new Map<string, WorkspaceIndexedDocument>();
const workspaceScriptFilesCache = new Map<
  string,
  { mtimeMs: number; size: number; files: string[] }
>();
const jsLanguageServiceCache = new Map<string, JsLanguageServiceCacheEntry>();
const semanticTokenResults = new Map<string, { uri: string; data: number[] }>();
const latestSemanticTokenResultByUri = new Map<string, string>();
const defaultMaxIndexFiles = 5000;
const defaultScanChunkSize = 200;
const defaultDiagnosticsDebounceMs = 250;
let globalSettings: AspSettings = { defaultLanguage: "VBScript", checkJs: false };
let workspaceRoots: string[] = [];
let clientLocale = "en";
let workspaceIndexDirty = true;
let workspaceIndexTruncated = false;
let jsLanguageServiceCacheTick = 0;
let semanticTokenResultCounter = 0;
const tsUnusedDiagnosticCodes = new Set([6133, 6138, 6192, 6196, 6198]);
const hiddenJavaScriptGlobalCompletions = new Set(["__dirname", "__filename"]);
const browserJavaScriptLibs = ["lib.esnext.d.ts", "lib.dom.d.ts", "lib.dom.iterable.d.ts"];

const semanticTokenTypes = [
  "keyword",
  "variable",
  "parameter",
  "function",
  "class",
  "method",
  "property",
  "comment",
  "string",
  "operator",
  "namespace",
  "interface",
  "enum",
  "enumMember",
  "typeAlias",
  "typeParameter",
] as const;
const semanticTokenModifiers = [
  "public",
  "private",
  "readonly",
  "library",
  "byref",
  "byval",
] as const;

interface SemanticTokenData {
  line: number;
  character: number;
  length: number;
  tokenType: string;
  tokenModifiers?: readonly string[];
}

interface CachedDocument {
  source: TextDocument;
  parsed: AspParsedDocument;
  virtuals: Map<string, VirtualDocument>;
  analysis?: CachedAnalysis;
  changes?: AspIncrementalChange[];
  dirtyLanguages?: Set<AspIncrementalChange["language"]>;
}

interface CachedAnalysis {
  diagnostics?: DiagnosticCacheEntry;
  fastDiagnostics?: DiagnosticCacheEntry;
  slowDiagnostics?: DiagnosticCacheEntry;
  htmlDiagnostics?: DiagnosticCacheEntry;
  cssDiagnostics?: DiagnosticCacheEntry;
  vbDiagnostics?: DiagnosticCacheEntry;
  jsSyntaxDiagnostics?: DiagnosticCacheEntry;
  jsSlowDiagnostics?: DiagnosticCacheEntry;
  vbProjectContext?: { key: string; context: VbProjectContext };
}

interface DiagnosticCacheEntry {
  key: string;
  items: Diagnostic[];
  text: string;
}

interface PendingDocumentChange {
  previousText: string;
  changes: TextDocumentContentChangeEvent[];
  version: number;
}

interface SlowDiagnosticsJob {
  version: number;
  sequence: number;
  timer: ReturnType<typeof setTimeout>;
}

interface OffsetEdit {
  start: number;
  end: number;
  newText: string;
}

interface WorkspaceIndexedDocument {
  uri: string;
  fileName: string;
  mtimeMs: number;
  size: number;
  parsed: AspParsedDocument;
}

type JavaScriptMode = "definition" | "declaration" | "typeDefinition" | "implementation";

interface JsProjectFile {
  fileName: string;
  text: string;
  version: string;
  uri: string;
  virtual?: VirtualDocument;
}

interface JsProjectContext {
  virtual: VirtualDocument;
  service: ts.LanguageService;
  fileName: string;
  offset: number;
  files: Map<string, JsProjectFile>;
}

interface JsLanguageServiceProject {
  service: ts.LanguageService;
  host: ts.LanguageServiceHost;
  files: Map<string, JsProjectFile>;
}

interface JsLanguageServiceCacheEntry {
  project: JsLanguageServiceProject;
  lastUsed: number;
}

interface JsCallHierarchyData {
  kind: "javascript";
  rootUri: string;
  language: string;
  fileName: string;
  position: number;
}

interface VbTypeHierarchyData {
  kind: "vbscript";
  rootUri: string;
  uri: string;
  typeName: string;
  line: number;
  character: number;
}

function aspFileOperationFilter() {
  return {
    scheme: "file",
    pattern: {
      glob: "**/*.{asp,asa,inc}",
      matches: "file" as const,
      options: { ignoreCase: true },
    },
  };
}

const cache = new Map<string, CachedDocument>();
const diagnosticsTimers = new Map<string, ReturnType<typeof setTimeout>>();
const slowDiagnosticsJobs = new Map<string, SlowDiagnosticsJob>();
let slowDiagnosticsSequence = 0;

connection.onInitialize((params: InitializeParams): InitializeResult => {
  clientLocale = typeof params.locale === "string" ? params.locale : "en";
  globalSettings = normalizeSettings(globalSettings);
  workspaceRoots = [
    ...(params.workspaceFolders?.map((folder) => uriToFileName(folder.uri)) ?? []),
    ...(params.rootUri ? [uriToFileName(params.rootUri)] : []),
  ].filter((root, index, roots) => root.length > 0 && roots.indexOf(root) === index);
  return {
    capabilities: {
      textDocumentSync: {
        openClose: true,
        change: TextDocumentSyncKind.Incremental,
        willSave: true,
        willSaveWaitUntil: true,
        save: { includeText: false },
      },
      completionProvider: {
        triggerCharacters: ["<", ".", '"', "'", ":", "#", "("],
        resolveProvider: true,
      },
      signatureHelpProvider: { triggerCharacters: ["(", ","] },
      hoverProvider: true,
      declarationProvider: true,
      definitionProvider: true,
      typeDefinitionProvider: true,
      implementationProvider: true,
      referencesProvider: true,
      renameProvider: { prepareProvider: true },
      documentHighlightProvider: true,
      workspaceSymbolProvider: true,
      documentSymbolProvider: true,
      foldingRangeProvider: true,
      documentLinkProvider: { resolveProvider: true },
      codeActionProvider: {
        resolveProvider: true,
        codeActionKinds: [
          CodeActionKind.QuickFix,
          CodeActionKind.Refactor,
          CodeActionKind.Source,
          CodeActionKind.SourceOrganizeImports,
          "source.organizeImports.aspLsp.javascript",
        ],
      },
      executeCommandProvider: {
        commands: ["aspLsp.reindexWorkspace", "aspLsp.clearCache"],
      },
      codeLensProvider: { resolveProvider: true },
      colorProvider: true,
      selectionRangeProvider: true,
      linkedEditingRangeProvider: true,
      inlayHintProvider: { resolveProvider: true },
      callHierarchyProvider: true,
      typeHierarchyProvider: true,
      monikerProvider: true,
      inlineValueProvider: true,
      semanticTokensProvider: {
        legend: {
          tokenTypes: [...semanticTokenTypes],
          tokenModifiers: [...semanticTokenModifiers],
        },
        full: { delta: true },
        range: true,
      },
      documentFormattingProvider: true,
      documentRangeFormattingProvider: true,
      documentOnTypeFormattingProvider: {
        firstTriggerCharacter: "\n",
        moreTriggerCharacter: [">"],
      },
      workspace: {
        workspaceFolders: {
          supported: true,
          changeNotifications: true,
        },
        fileOperations: {
          willRename: { filters: [aspFileOperationFilter()] },
          didRename: { filters: [aspFileOperationFilter()] },
          didCreate: { filters: [aspFileOperationFilter()] },
          didDelete: { filters: [aspFileOperationFilter()] },
        },
      },
    },
  };
});

documents.onDidOpen((event) => {
  cache.delete(event.document.uri);
  clearJsLanguageServiceCache();
  validate(event.document);
});
documents.onDidChangeContent((event) => {
  cancelSlowDiagnostics(event.document.uri);
  refreshCachedDocument(event.document);
  scheduleDiagnostics(event.document);
});
documents.onDidSave((event) => {
  cache.delete(event.document.uri);
  clearJsLanguageServiceCache();
  validate(event.document);
});
documents.onDidClose((event) => {
  cancelScheduledDiagnostics(event.document.uri);
  cancelSlowDiagnostics(event.document.uri);
  pendingDocumentChanges.delete(event.document.uri);
  cache.delete(event.document.uri);
  clearJsLanguageServiceCache();
  clearSemanticTokensForUri(event.document.uri);
  connection.sendDiagnostics({ uri: event.document.uri, diagnostics: [] });
});

documents.onWillSave((event) => {
  validate(event.document);
});

documents.onWillSaveWaitUntil((event) => {
  validate(event.document);
  const cached = getCached(event.document.uri);
  const settings = cached ? cachedSettings(cached.source.uri) : undefined;
  return cached && settings?.format?.onSave === true
    ? formatAspDocumentWithDelegates(cached, defaultFormattingOptions(settings))
    : [];
});

connection.onInitialized(() => {
  void refreshConfiguration();
});

connection.onNotification(
  "workspace/didChangeWorkspaceFolders",
  (event: {
    event?: {
      added?: Array<{ uri: string }>;
      removed?: Array<{ uri: string }>;
    };
  }) => {
    const added = event.event?.added ?? [];
    const removedFolders = event.event?.removed ?? [];
    const removed = new Set(
      removedFolders.map((folder) => normalizeFileName(uriToFileName(folder.uri))),
    );
    workspaceRoots = [
      ...workspaceRoots.filter((root) => !removed.has(normalizeFileName(root))),
      ...added.map((folder) => uriToFileName(folder.uri)).filter((root) => root.length > 0),
    ].filter((root, index, roots) => roots.indexOf(root) === index);
    invalidateWorkspaceIndex();
    includeDocumentCache.clear();
    clearJsProjectCaches();
    cancelAllSlowDiagnostics();
    for (const document of documents.all()) {
      validate(document);
    }
  },
);

connection.onDidChangeConfiguration((change) => {
  const incoming = readSettingsFromChange(change.settings);
  if (incoming) {
    globalSettings = normalizeSettings(incoming);
  }
  settingsByUri.clear();
  cache.clear();
  includeDocumentCache.clear();
  clearJsLanguageServiceCache();
  clearSemanticTokens();
  invalidateWorkspaceIndex();
  cancelAllSlowDiagnostics();
  for (const document of documents.all()) {
    validate(document);
  }
});

connection.onDidChangeWatchedFiles((change) => {
  let aspChanged = false;
  let scriptChanged = false;
  cache.clear();
  clearSemanticTokens();
  cancelAllSlowDiagnostics();
  for (const file of change.changes) {
    const fileName = normalizeFileName(uriToFileName(file.uri));
    if (isAspWorkspaceFile(fileName)) {
      aspChanged = true;
      if (file.type === FileChangeType.Deleted) {
        workspaceIndex.delete(fileName);
      } else {
        indexWorkspaceFile(fileName);
      }
    }
    if (isScriptWorkspaceFile(fileName) || isJavaScriptProjectEnvironmentFile(fileName)) {
      scriptChanged = true;
    }
  }
  if (aspChanged) {
    includeDocumentCache.clear();
  }
  if (aspChanged || scriptChanged) {
    clearJsProjectCaches();
  }
  for (const document of documents.all()) {
    validate(document);
  }
});

connection.workspace.onWillRenameFiles((params) => includeRenameWorkspaceEdit(params.files));

connection.workspace.onDidRenameFiles(() => {
  invalidateWorkspaceIndex();
  includeDocumentCache.clear();
  clearJsProjectCaches();
});

connection.workspace.onDidCreateFiles(() => {
  invalidateWorkspaceIndex();
  includeDocumentCache.clear();
  clearJsProjectCaches();
});

connection.workspace.onDidDeleteFiles(() => {
  invalidateWorkspaceIndex();
  includeDocumentCache.clear();
  clearJsProjectCaches();
});

connection.onCompletion((params) => {
  const cached = getCached(params.textDocument.uri);
  if (!cached) {
    return [];
  }
  const region = findRegionAt(cached.parsed, cached.source.offsetAt(params.position));
  if (!region) {
    return [];
  }
  if (region.language === "vbscript") {
    return withCompletionData(
      getVbscriptCompletions(
        cached.parsed,
        params.position,
        buildVbProjectContext(cached, cachedSettings(cached.source.uri)),
      ),
      { kind: "vbscript", uri: cached.source.uri },
    );
  }
  if (region.language === "html") {
    const virtual = cached.virtuals.get("html");
    if (!virtual) {
      return [];
    }
    const virtualDocument = toTextDocument(virtual);
    return withCompletionData(
      htmlService.doComplete(
        virtualDocument,
        params.position,
        htmlService.parseHTMLDocument(virtualDocument),
      ).items,
      {
        kind: "html",
        uri: cached.source.uri,
        locale: cachedSettings(cached.source.uri).resolvedLocale,
      },
    );
  }
  if (region.language === "css") {
    return withCompletionData(cssCompletion(cached, params, "css"), {
      kind: "css",
      uri: cached.source.uri,
      locale: cachedSettings(cached.source.uri).resolvedLocale,
    });
  }
  if (region && isJavaScriptLikeRegion(region)) {
    return jsCompletion(cached, params);
  }
  return [];
});

connection.onCompletionResolve((item) => {
  const data = item.data as { kind?: string; uri?: string } | undefined;
  if (data?.kind === "vbscript" && data.uri) {
    const cached = getCached(data.uri);
    return cached
      ? resolveVbscriptCompletionItem(
          item,
          cached.parsed,
          buildVbProjectContext(cached, cachedSettings(cached.source.uri)),
        )
      : item;
  }
  if (data?.kind === "javascript" && data.uri) {
    const resolved = resolveJsCompletion(item, data.uri);
    return resolved ?? item;
  }
  if ((data?.kind === "html" || data?.kind === "css") && data.uri) {
    return resolveEmbeddedCompletion(item, data.kind);
  }
  return item;
});

connection.onHover((params) => {
  const cached = getCached(params.textDocument.uri);
  if (!cached) {
    return null;
  }
  const region = findRegionAt(cached.parsed, cached.source.offsetAt(params.position));
  if (!region) {
    return null;
  }
  if (region.language === "vbscript") {
    return aspHover(cached, params);
  }
  if (isJavaScriptLikeRegion(region)) {
    return jsHover(cached, params.position);
  }
  if (region.language === "html") {
    const virtual = cached.virtuals.get("html");
    if (!virtual) {
      return null;
    }
    const doc = toTextDocument(virtual);
    return htmlService.doHover(doc, params.position, htmlService.parseHTMLDocument(doc));
  }
  if (region.language === "css") {
    const virtual = cached.virtuals.get("css");
    if (!virtual) {
      return null;
    }
    const virtualPosition = virtual.sourceMap.toVirtualPosition(params.position);
    if (!virtualPosition) {
      return null;
    }
    const doc = toTextDocument(virtual);
    return cssService.doHover(doc, virtualPosition, cssService.parseStylesheet(doc));
  }
  return null;
});

connection.onDefinition((params) => {
  return definitionLikeLocation(params.textDocument.uri, params.position, "definition");
});

connection.onDeclaration((params) => {
  return definitionLikeLocation(params.textDocument.uri, params.position, "declaration");
});

connection.onTypeDefinition((params) => {
  return definitionLikeLocation(params.textDocument.uri, params.position, "typeDefinition");
});

connection.onImplementation((params) => {
  return definitionLikeLocation(params.textDocument.uri, params.position, "implementation");
});

connection.onReferences((params: ReferenceParams) => {
  const cached = getCached(params.textDocument.uri);
  if (!cached) {
    return [];
  }
  const region = findRegionAt(cached.parsed, cached.source.offsetAt(params.position));
  if (!region) {
    return [];
  }
  if (isJavaScriptLikeRegion(region)) {
    return jsReferences(cached, params.position);
  }
  if (region.language !== "vbscript") {
    return [];
  }
  return getVbscriptReferences(
    cached.parsed,
    params.position,
    buildVbProjectContext(cached, cachedSettings(cached.source.uri)),
    { includeDeclaration: params.context.includeDeclaration },
  ).map((reference) => Location.create(reference.uri, reference.range));
});

connection.onPrepareRename((params) => {
  const cached = getCached(params.textDocument.uri);
  if (!cached) {
    return null;
  }
  if (isJavaScriptPosition(cached, params.position)) {
    return (
      jsPrepareRename(cached, params.position) ??
      crossLanguagePrepareRename(cached, params.position)
    );
  }
  if (isHtmlPosition(cached, params.position)) {
    return (
      crossLanguagePrepareRename(cached, params.position) ??
      htmlPrepareRename(cached, params.position)
    );
  }
  if (isCssPosition(cached, params.position)) {
    return (
      crossLanguagePrepareRename(cached, params.position) ??
      cssPrepareRename(cached, params.position)
    );
  }
  if (!isVbscriptPosition(cached, params.position)) {
    return null;
  }
  return (
    getVbscriptRenameRange(
      cached.parsed,
      params.position,
      buildVbProjectContext(cached, cachedSettings(cached.source.uri)),
    ) ?? null
  );
});

connection.onRenameRequest((params: RenameParams): WorkspaceEdit | null => {
  const cached = getCached(params.textDocument.uri);
  if (!cached) {
    return null;
  }
  if (isJavaScriptPosition(cached, params.position)) {
    return (
      mergeWorkspaceEdits([
        jsRename(cached, params.position, params.newName),
        crossLanguageRename(cached, params.position, params.newName),
      ]) ?? null
    );
  }
  if (isHtmlPosition(cached, params.position)) {
    return (
      mergeWorkspaceEdits([
        htmlRename(cached, params.position, params.newName),
        crossLanguageRename(cached, params.position, params.newName),
      ]) ?? null
    );
  }
  if (isCssPosition(cached, params.position)) {
    return (
      mergeWorkspaceEdits([
        cssRename(cached, params.position, params.newName),
        crossLanguageRename(cached, params.position, params.newName),
      ]) ?? null
    );
  }
  if (!isVbscriptPosition(cached, params.position)) {
    return null;
  }
  const context = buildVbProjectContext(cached, cachedSettings(cached.source.uri));
  const range = getVbscriptRenameRange(cached.parsed, params.position, context);
  if (!range || !/^[A-Za-z][A-Za-z0-9_]*$/.test(params.newName)) {
    return null;
  }
  const changes: WorkspaceEdit["changes"] = {};
  for (const reference of getVbscriptReferences(cached.parsed, params.position, context)) {
    const edits = changes[reference.uri] ?? [];
    edits.push({ range: reference.range, newText: params.newName });
    changes[reference.uri] = edits;
  }
  return { changes };
});

connection.onDocumentHighlight((params): DocumentHighlight[] => {
  const cached = getCached(params.textDocument.uri);
  if (!cached) {
    return [];
  }
  const region = findRegionAt(cached.parsed, cached.source.offsetAt(params.position));
  if (region?.language === "vbscript") {
    return getVbscriptDocumentHighlights(
      cached.parsed,
      params.position,
      buildVbProjectContext(cached, cachedSettings(cached.source.uri)),
    );
  }
  if (isJavaScriptLikeRegion(region)) {
    return jsDocumentHighlights(cached, params.position);
  }
  if (region?.language === "html") {
    return htmlDocumentHighlights(cached, params.position);
  }
  if (region?.language === "css") {
    return cssDocumentHighlights(cached, params.position);
  }
  return [];
});

connection.onSignatureHelp((params): SignatureHelp | null => {
  const cached = getCached(params.textDocument.uri);
  if (!cached) {
    return null;
  }
  if (isJavaScriptPosition(cached, params.position)) {
    return jsSignatureHelp(cached, params.position);
  }
  if (!isVbscriptPosition(cached, params.position)) {
    return null;
  }
  return (
    getVbscriptSignatureHelp(
      cached.parsed,
      params.position,
      buildVbProjectContext(cached, cachedSettings(cached.source.uri)),
    ) ?? null
  );
});

connection.onWorkspaceSymbol(async (params, token) => {
  await ensureWorkspaceIndexAsync(globalSettings, token);
  const query = params.query.toLowerCase();
  const openedUris = new Set(documents.all().map((document) => document.uri));
  const indexedDocuments = [...workspaceIndex.values()]
    .filter((entry) => !openedUris.has(entry.uri))
    .map((entry) => entry.parsed);
  const openSymbols = documents.all().flatMap((document) => {
    const cached = getCached(document.uri);
    return cached
      ? (buildVbProjectContext(cached, cachedSettings(document.uri)).symbols ?? [])
      : [];
  });
  const indexedSymbols = indexedDocuments.flatMap((parsed) =>
    collectVbscriptSymbols(parsed, { documents: indexedDocuments }),
  );
  const vbSymbols = [...openSymbols, ...indexedSymbols].map((symbol) =>
    SymbolInformation.create(
      symbol.name,
      vbWorkspaceSymbolKind(symbol.kind),
      symbol.range,
      symbol.sourceUri,
      symbol.memberOf,
    ),
  );
  const richSymbols = [
    ...documents.all().flatMap((document) => {
      const cached = getCached(document.uri);
      return cached ? workspaceSymbolsForCached(cached) : [];
    }),
    ...[...workspaceIndex.values()]
      .filter((entry) => !openedUris.has(entry.uri))
      .flatMap((entry) => workspaceSymbolsForCached(cachedFromIndexed(entry))),
  ];
  return [...vbSymbols, ...richSymbols].filter(
    (symbol) => query.length === 0 || symbol.name.toLowerCase().includes(query),
  );
});

connection.onDocumentSymbol((params) => {
  const cached = getCached(params.textDocument.uri);
  if (!cached) {
    return [];
  }
  const htmlVirtual = cached.virtuals.get("html");
  const htmlSymbols: DocumentSymbol[] = htmlVirtual
    ? htmlService.findDocumentSymbols2(
        toTextDocument(htmlVirtual),
        htmlService.parseHTMLDocument(toTextDocument(htmlVirtual)),
      )
    : [];
  return [
    ...htmlSymbols,
    ...cssDocumentSymbols(cached),
    ...jsDocumentSymbols(cached),
    ...getVbscriptDocumentSymbols(cached.parsed),
  ];
});

connection.onFoldingRanges((params) => {
  const cached = getCached(params.textDocument.uri);
  if (!cached) {
    return [];
  }
  const htmlVirtual = cached.virtuals.get("html");
  const htmlRanges: FoldingRange[] = htmlVirtual
    ? htmlService.getFoldingRanges(toTextDocument(htmlVirtual), {})
    : [];
  const cssRanges = cssFoldingRanges(cached);
  const jsRanges = jsFoldingRanges(cached);
  const vbRanges = vbscriptFoldingRanges(cached);
  const aspRanges: FoldingRange[] = cached.parsed.regions
    .filter(
      (region) =>
        region.kind !== "html" &&
        cached.source.positionAt(region.start).line !== cached.source.positionAt(region.end).line,
    )
    .map((region) => ({
      startLine: cached.source.positionAt(region.start).line,
      endLine: cached.source.positionAt(region.end).line,
    }));
  return [...htmlRanges, ...cssRanges, ...jsRanges, ...vbRanges, ...aspRanges];
});

connection.onDocumentLinks((params) => {
  const cached = getCached(params.textDocument.uri);
  if (!cached) {
    return [];
  }
  return cached.parsed.includes.map((include): DocumentLink => {
    const targetPath = resolveIncludePath(
      cached.source.uri,
      include.path,
      include.mode,
      cachedSettings(cached.source.uri),
    );
    return { range: include.pathRange, target: pathToFileUri(targetPath) };
  });
});

connection.onSelectionRanges((params): SelectionRange[] => {
  const cached = getCached(params.textDocument.uri);
  if (!cached) {
    return [];
  }
  return params.positions.map((position) => selectionRangeAt(cached, position));
});

connection.languages.inlayHint.on((params): InlayHint[] => {
  const cached = getCached(params.textDocument.uri);
  if (!cached) {
    return [];
  }
  return [
    ...getVbscriptInlayHints(
      cached.parsed,
      params.range,
      buildVbProjectContext(cached, cachedSettings(cached.source.uri)),
      cachedSettings(cached.source.uri).inlayHints,
    ),
    ...jsInlayHints(cached, params.range),
  ];
});

connection.languages.diagnostics.on((params) => {
  const cached = getCached(params.textDocument.uri) ?? getIndexedCached(params.textDocument.uri);
  return {
    kind: "full" as const,
    items: cached ? diagnosticsForCached(cached, cachedSettings(cached.source.uri)) : [],
  };
});

connection.languages.diagnostics.onWorkspace(async (_params, token) => {
  await ensureWorkspaceIndexAsync(globalSettings, token);
  const openedUris = new Set(documents.all().map((document) => document.uri));
  return {
    items: [
      ...documents.all().flatMap((document) => {
        const cached = getCached(document.uri);
        return cached
          ? [
              {
                kind: "full" as const,
                uri: document.uri,
                version: document.version,
                items: diagnosticsForCached(cached, cachedSettings(document.uri)),
              },
            ]
          : [];
      }),
      ...[...workspaceIndex.values()]
        .filter((entry) => !openedUris.has(entry.uri))
        .map((entry) => {
          const cached = cachedFromIndexed(entry);
          return {
            kind: "full" as const,
            uri: entry.uri,
            version: null,
            items: diagnosticsForCached(cached, cachedSettings(entry.uri)),
          };
        }),
    ],
  };
});

connection.onExecuteCommand((params) => {
  if (params.command === "aspLsp.reindexWorkspace" || params.command === "aspLsp.clearCache") {
    invalidateWorkspaceIndex();
    includeDocumentCache.clear();
    cache.clear();
    clearJsProjectCaches();
    for (const document of documents.all()) {
      validate(document);
    }
    return { ok: true };
  }
  return {
    ok: false,
    message: createLocalizer(globalSettings.resolvedLocale).t("server.unknownCommand", {
      command: params.command,
    }),
  };
});

connection.languages.callHierarchy.onPrepare((params): CallHierarchyItem[] => {
  const cached = getCached(params.textDocument.uri);
  if (!cached) {
    return [];
  }
  if (isJavaScriptPosition(cached, params.position)) {
    return jsPrepareCallHierarchy(cached, params.position);
  }
  if (!isVbscriptPosition(cached, params.position)) {
    return [];
  }
  return prepareVbscriptCallHierarchy(
    cached.parsed,
    params.position,
    buildVbProjectContext(cached, cachedSettings(cached.source.uri)),
    cached.source.uri,
  );
});

connection.languages.callHierarchy.onIncomingCalls((params): CallHierarchyIncomingCall[] => {
  if (isJsCallHierarchyItem(params.item)) {
    return jsIncomingCalls(params.item);
  }
  const root = callHierarchyRootUri(params.item);
  const cached = getCached(root) ?? getCached(params.item.uri);
  if (!cached) {
    return [];
  }
  return getVbscriptIncomingCalls(
    params.item,
    buildVbProjectContext(cached, cachedSettings(cached.source.uri)),
  );
});

connection.languages.callHierarchy.onOutgoingCalls((params): CallHierarchyOutgoingCall[] => {
  if (isJsCallHierarchyItem(params.item)) {
    return jsOutgoingCalls(params.item);
  }
  const root = callHierarchyRootUri(params.item);
  const cached = getCached(root) ?? getCached(params.item.uri);
  if (!cached) {
    return [];
  }
  return getVbscriptOutgoingCalls(
    params.item,
    buildVbProjectContext(cached, cachedSettings(cached.source.uri)),
  );
});

connection.languages.typeHierarchy.onPrepare((params): TypeHierarchyItem[] => {
  const cached = getCached(params.textDocument.uri);
  if (!cached) {
    return [];
  }
  const item = vbTypeHierarchyItemAt(cached, params.position);
  return item ? [item] : [];
});

connection.languages.typeHierarchy.onSupertypes((): TypeHierarchyItem[] => []);

connection.languages.typeHierarchy.onSubtypes((params): TypeHierarchyItem[] =>
  vbTypeHierarchyRelatedItems(params.item),
);

connection.languages.moniker.on((params): Moniker[] => {
  const cached = getCached(params.textDocument.uri);
  if (!cached) {
    return [];
  }
  return monikersAt(cached, params.position);
});

connection.languages.inlineValue.on((params: InlineValueParams): InlineValue[] => {
  const cached = getCached(params.textDocument.uri);
  if (!cached) {
    return [];
  }
  return inlineValues(cached, params.range);
});

connection.languages.onLinkedEditingRange((params): LinkedEditingRanges | null => {
  const cached = getCached(params.textDocument.uri);
  if (!cached) {
    return null;
  }
  const ranges = htmlLinkedRanges(cached, params.position);
  return ranges ? { ranges } : null;
});

function htmlLinkedRanges(cached: CachedDocument, position: Position): Range[] | null {
  const region = findRegionAt(cached.parsed, cached.source.offsetAt(position));
  if (!region || region.language !== "html") {
    return null;
  }
  const virtual = cached.virtuals.get("html");
  if (!virtual) {
    return null;
  }
  const doc = toTextDocument(virtual);
  return htmlService.findLinkedEditingRanges(doc, position, htmlService.parseHTMLDocument(doc));
}

connection.onDocumentColor((params): ColorInformation[] => {
  const cached = getCached(params.textDocument.uri);
  if (!cached) {
    return [];
  }
  return cssDocumentColors(cached);
});

connection.onColorPresentation((params): ColorPresentation[] => {
  const cached = getCached(params.textDocument.uri);
  if (!cached) {
    return [];
  }
  return cssColorPresentations(cached, params.color, params.range);
});

connection.onCodeLens((params): CodeLens[] => {
  const cached = getCached(params.textDocument.uri);
  if (!cached) {
    return [];
  }
  return codeLenses(cached);
});

connection.onDocumentFormatting((params) => {
  const cached = getCached(params.textDocument.uri);
  if (!cached) {
    return [];
  }
  return formatAspDocumentWithDelegates(cached, params.options);
});

connection.onDocumentRangeFormatting((params) => {
  const cached = getCached(params.textDocument.uri);
  if (!cached) {
    return [];
  }
  const region = findRegionAt(cached.parsed, cached.source.offsetAt(params.range.start));
  if (!region || region.language !== "html") {
    return formatAspRangeWithDelegates(cached, params.range, params.options);
  }
  const virtual = cached.virtuals.get("html");
  if (!virtual) {
    return [];
  }
  if (rangeOverlapsNonHtml(cached, params.range)) {
    return formatAspRangeWithDelegates(cached, params.range, params.options);
  }
  return htmlService.format(toTextDocument(virtual), params.range, {
    tabSize: params.options.tabSize,
    insertSpaces: params.options.insertSpaces,
  }) as TextEdit[];
});

connection.onCodeAction((params): CodeAction[] => {
  const cached = getCached(params.textDocument.uri);
  if (!cached) {
    return [];
  }
  return [
    ...params.context.diagnostics.flatMap((diagnostic) =>
      quickFixesForDiagnostic(cached, diagnostic),
    ),
    ...vbscriptCodeActions(cached, params.range, params.context),
    ...cssCodeActions(cached, params.range, params.context),
    ...jsCodeActions(cached, params.range, params.context),
  ];
});

connection.onCodeActionResolve((action) => action);

connection.onCodeLensResolve((lens) => lens);

connection.onDocumentLinkResolve((link) => link);

connection.languages.inlayHint.resolve((hint) => hint);

connection.onDocumentOnTypeFormatting((params) => {
  const cached = getCached(params.textDocument.uri);
  if (!cached) {
    return [];
  }
  return onTypeFormatting(cached, params.position, params.ch, params.options);
});

connection.languages.semanticTokens.on((params) => {
  const cached = getCached(params.textDocument.uri);
  if (!cached) {
    return { data: [] };
  }
  return cacheSemanticTokens(cached.source.uri, buildSemanticTokens(cached).data);
});

connection.languages.semanticTokens.onRange((params): SemanticTokens => {
  const cached = getCached(params.textDocument.uri);
  if (!cached) {
    return { data: [] };
  }
  return buildSemanticTokens(cached, params.range);
});

connection.languages.semanticTokens.onDelta((params): SemanticTokens | SemanticTokensDelta => {
  const cached = getCached(params.textDocument.uri);
  if (!cached) {
    return { data: [] };
  }
  const previous = semanticTokenResults.get(params.previousResultId);
  const next = buildSemanticTokens(cached).data;
  if (!previous || previous.uri !== cached.source.uri) {
    return cacheSemanticTokens(cached.source.uri, next);
  }
  const resultId = nextSemanticTokenResultId();
  semanticTokenResults.set(resultId, { uri: cached.source.uri, data: next });
  latestSemanticTokenResultByUri.set(cached.source.uri, resultId);
  return {
    resultId,
    edits: [semanticTokenDeltaEdit(previous.data, next)],
  };
});

function validate(document: TextDocument): void {
  cancelScheduledDiagnostics(document.uri);
  publishStagedDiagnosticsForCached(refreshCachedDocument(document), cachedSettings(document.uri));
}

function refreshCachedDocument(document: TextDocument): CachedDocument {
  const startedAt = process.hrtime.bigint();
  const settingsStartedAt = startedAt;
  const settings = cachedSettings(document.uri);
  startAnalysisLog(settings, document.uri);
  finishDebugStep(settings, document.uri, "analysis.settings", settingsStartedAt);
  const previous = cache.get(document.uri);
  const pending = pendingDocumentChanges.get(document.uri);
  const parseStartedAt = process.hrtime.bigint();
  const update =
    previous && pending?.version === document.version
      ? updateAspParsedDocument(
          { ...previous.parsed, text: pending.previousText },
          document.getText(),
          pending.changes,
          settings,
        )
      : undefined;
  const parsed = update?.parsed ?? parseAspDocument(document.uri, document.getText(), settings);
  finishDebugStep(
    settings,
    document.uri,
    update?.incremental === true ? "analysis.parse.incremental" : "analysis.parse.full",
    parseStartedAt,
  );
  const virtualStartedAt = process.hrtime.bigint();
  const virtuals = buildVirtualDocuments(parsed);
  finishDebugStep(settings, document.uri, "analysis.virtualDocuments", virtualStartedAt);
  const changes = update?.incremental
    ? [...(previous?.changes ?? []), ...(update.change ? [update.change] : [])]
    : undefined;
  const dirtyLanguages = update?.incremental
    ? new Set([
        ...(previous?.dirtyLanguages ?? []),
        ...(update.change ? [update.change.language] : []),
      ])
    : undefined;
  const cacheStartedAt = process.hrtime.bigint();
  const cached = {
    source: document,
    parsed,
    virtuals,
    analysis: previous?.analysis,
    changes,
    dirtyLanguages,
  };
  cache.set(document.uri, cached);
  if (pending?.version === document.version) {
    pendingDocumentChanges.delete(document.uri);
  }
  finishDebugStep(settings, document.uri, "analysis.cacheUpdate", cacheStartedAt);
  finishAnalysisLog(settings, document.uri, startedAt, update?.incremental === true);
  return cached;
}

function startAnalysisLog(settings: AspSettings, uri: string): void {
  logDebugSummary(settings, `[asp-lsp] LSP analysis started: ${uri}`);
}

function finishAnalysisLog(
  settings: AspSettings,
  uri: string,
  startedAt: bigint,
  incremental: boolean,
): void {
  const elapsedMs = Number(process.hrtime.bigint() - startedAt) / 1_000_000;
  const mode = incremental ? "incremental" : "full";
  logDebugSummary(
    settings,
    `[asp-lsp] LSP analysis completed: ${uri} ${formatElapsedMs(elapsedMs)}, mode=${mode}`,
  );
}

function scheduleDiagnostics(document: TextDocument): void {
  cancelScheduledDiagnostics(document.uri);
  const delay =
    cachedSettings(document.uri).diagnostics?.debounceMs ?? defaultDiagnosticsDebounceMs;
  if (delay <= 0) {
    publishDiagnosticsForVersion(document.uri, document.version);
    return;
  }
  const version = document.version;
  diagnosticsTimers.set(
    document.uri,
    setTimeout(() => {
      diagnosticsTimers.delete(document.uri);
      publishDiagnosticsForVersion(document.uri, version);
    }, delay),
  );
}

function publishDiagnosticsForVersion(uri: string, version: number): void {
  const document = documents.get(uri);
  if (!document || document.version !== version) {
    return;
  }
  publishStagedDiagnosticsForCached(
    getCached(uri) ?? refreshCachedDocument(document),
    cachedSettings(uri),
  );
}

function cancelScheduledDiagnostics(uri: string): void {
  const timer = diagnosticsTimers.get(uri);
  if (!timer) {
    return;
  }
  clearTimeout(timer);
  diagnosticsTimers.delete(uri);
}

function publishStagedDiagnosticsForCached(cached: CachedDocument, settings: AspSettings): void {
  cancelSlowDiagnostics(cached.source.uri);
  const diagnostics = fastDiagnosticsForCached(cached, settings);
  connection.sendDiagnostics({
    uri: cached.source.uri,
    diagnostics,
  });
  enqueueSlowDiagnostics(cached, settings);
}

function enqueueSlowDiagnostics(cached: CachedDocument, settings: AspSettings): void {
  const uri = cached.source.uri;
  const version = cached.source.version;
  const sequence = ++slowDiagnosticsSequence;
  const timer = setTimeout(() => {
    const current = slowDiagnosticsJobs.get(uri);
    if (!current || current.sequence !== sequence) {
      return;
    }
    slowDiagnosticsJobs.delete(uri);
    publishSlowDiagnosticsForVersion(uri, version, settings, sequence);
  }, 0);
  slowDiagnosticsJobs.set(uri, { version, sequence, timer });
}

function publishSlowDiagnosticsForVersion(
  uri: string,
  version: number,
  settings: AspSettings,
  sequence: number,
): void {
  const document = documents.get(uri);
  const cached = getCached(uri);
  if (!document || !cached || document.version !== version || cached.source.version !== version) {
    return;
  }
  const startedAt = startSlowCheckLog(cached, settings);
  const slowItems = slowDiagnosticsForCached(cached, settings, "check.slow");
  if (documents.get(uri)?.version !== version || getCached(uri)?.source.version !== version) {
    return;
  }
  const fastItems =
    cached.analysis?.fastDiagnostics?.items ?? fastDiagnosticsForCached(cached, settings);
  const items = measureDebugStep(settings, uri, "check.slow.merge", () =>
    dedupeDiagnostics([...fastItems, ...slowItems]),
  );
  analysisFor(cached).diagnostics = {
    key: diagnosticsCacheKey(cached, settings),
    items,
    text: cached.parsed.text,
  };
  cached.changes = undefined;
  cached.dirtyLanguages = undefined;
  connection.sendDiagnostics({ uri, diagnostics: items });
  finishSlowCheckLog(cached, settings, startedAt, items.length, sequence);
}

function cancelSlowDiagnostics(uri: string): void {
  const job = slowDiagnosticsJobs.get(uri);
  if (!job) {
    return;
  }
  clearTimeout(job.timer);
  slowDiagnosticsJobs.delete(uri);
}

function cancelAllSlowDiagnostics(): void {
  for (const uri of slowDiagnosticsJobs.keys()) {
    cancelSlowDiagnostics(uri);
  }
}

function diagnosticsForCached(cached: CachedDocument, settings: AspSettings): Diagnostic[] {
  const startedAt = startCheckLog(cached, settings);
  const key = diagnosticsCacheKey(cached, settings);
  if (cached.analysis?.diagnostics?.key === key) {
    finishDebugStep(settings, cached.source.uri, "check.cacheReuse", startedAt);
    finishCheckLog(cached, settings, startedAt, cached.analysis.diagnostics.items.length, true);
    return cached.analysis.diagnostics.items;
  }
  const fastItems = fastDiagnosticsForCached(cached, settings, "check");
  const slowItems = slowDiagnosticsForCached(cached, settings, "check");
  const items = measureDebugStep(settings, cached.source.uri, "check.dedupe", () =>
    dedupeDiagnostics([...fastItems, ...slowItems]),
  );
  analysisFor(cached).diagnostics = { key, items, text: cached.parsed.text };
  cached.changes = undefined;
  cached.dirtyLanguages = undefined;
  finishCheckLog(cached, settings, startedAt, items.length, false);
  return items;
}

function fastDiagnosticsForCached(
  cached: CachedDocument,
  settings: AspSettings,
  stepPrefix = "check.fast",
): Diagnostic[] {
  const shouldLogFastSummary = stepPrefix === "check.fast";
  const startedAt = shouldLogFastSummary
    ? startFastCheckLog(cached, settings)
    : process.hrtime.bigint();
  const key = fastDiagnosticsCacheKey(cached, settings);
  if (cached.analysis?.fastDiagnostics?.key === key) {
    finishDebugStep(settings, cached.source.uri, `${stepPrefix}.cacheReuse`, startedAt);
    if (shouldLogFastSummary) {
      finishFastCheckLog(cached, settings, startedAt, cached.analysis.fastDiagnostics.items.length);
    }
    return cached.analysis.fastDiagnostics.items;
  }
  const parserDiagnostics = measureDebugStep(
    settings,
    cached.source.uri,
    `${stepPrefix}.parserDiagnostics`,
    () => cached.parsed.diagnostics,
  );
  const includeItems = measureDebugStep(
    settings,
    cached.source.uri,
    `${stepPrefix}.includeDiagnostics`,
    () => includeDiagnostics(cached, settings),
  );
  const diagnostics: Diagnostic[] = [...parserDiagnostics, ...includeItems];
  if (!isIncDocument(cached.source.uri)) {
    diagnostics.push(
      ...measureDebugStep(settings, cached.source.uri, `${stepPrefix}.htmlDiagnostics`, () =>
        htmlDiagnostics(cached),
      ),
    );
  }
  diagnostics.push(
    ...measureDebugStep(settings, cached.source.uri, `${stepPrefix}.cssDiagnostics`, () =>
      cssDiagnostics(cached),
    ),
  );
  diagnostics.push(
    ...measureDebugStep(settings, cached.source.uri, `${stepPrefix}.javascriptSyntax`, () =>
      jsSyntaxDiagnostics(cached),
    ),
  );
  const items = measureDebugStep(settings, cached.source.uri, `${stepPrefix}.dedupe`, () =>
    dedupeDiagnostics(diagnostics),
  );
  analysisFor(cached).fastDiagnostics = { key, items, text: cached.parsed.text };
  if (shouldLogFastSummary) {
    finishFastCheckLog(cached, settings, startedAt, items.length);
  }
  return items;
}

function slowDiagnosticsForCached(
  cached: CachedDocument,
  settings: AspSettings,
  stepPrefix = "check.slow",
): Diagnostic[] {
  const key = slowDiagnosticsCacheKey(cached, settings);
  if (cached.analysis?.slowDiagnostics?.key === key) {
    finishDebugStep(
      settings,
      cached.source.uri,
      `${stepPrefix}.cacheReuse`,
      process.hrtime.bigint(),
    );
    return cached.analysis.slowDiagnostics.items;
  }
  const vbItems = vbDiagnostics(cached, settings, stepPrefix);
  const jsItems = jsSlowDiagnostics(cached, settings, stepPrefix);
  const items = measureDebugStep(settings, cached.source.uri, `${stepPrefix}.dedupe`, () =>
    dedupeDiagnostics([...vbItems, ...jsItems]),
  );
  analysisFor(cached).slowDiagnostics = { key, items, text: cached.parsed.text };
  return items;
}

function startCheckLog(cached: CachedDocument, settings: AspSettings): bigint {
  logDebugSummary(settings, `[asp-lsp] LSP check started: ${cached.source.uri}`);
  return process.hrtime.bigint();
}

function finishCheckLog(
  cached: CachedDocument,
  settings: AspSettings,
  startedAt: bigint,
  diagnosticCount: number,
  cachedResult: boolean,
): void {
  const elapsedMs = Number(process.hrtime.bigint() - startedAt) / 1_000_000;
  const cacheText = cachedResult ? ", cached=true" : "";
  logDebugSummary(
    settings,
    `[asp-lsp] LSP check completed: ${cached.source.uri} ${formatElapsedMs(elapsedMs)}, diagnostics=${diagnosticCount}${cacheText}`,
  );
}

function startFastCheckLog(cached: CachedDocument, settings: AspSettings): bigint {
  logDebugSummary(settings, `[asp-lsp] LSP check fast started: ${cached.source.uri}`);
  return process.hrtime.bigint();
}

function finishFastCheckLog(
  cached: CachedDocument,
  settings: AspSettings,
  startedAt: bigint,
  diagnosticCount: number,
): void {
  const elapsedMs = Number(process.hrtime.bigint() - startedAt) / 1_000_000;
  logDebugSummary(
    settings,
    `[asp-lsp] LSP check fast completed: ${cached.source.uri} ${formatElapsedMs(elapsedMs)}, diagnostics=${diagnosticCount}`,
  );
}

function startSlowCheckLog(cached: CachedDocument, settings: AspSettings): bigint {
  logDebugSummary(settings, `[asp-lsp] LSP check slow started: ${cached.source.uri}`);
  return process.hrtime.bigint();
}

function finishSlowCheckLog(
  cached: CachedDocument,
  settings: AspSettings,
  startedAt: bigint,
  diagnosticCount: number,
  sequence: number,
): void {
  const elapsedMs = Number(process.hrtime.bigint() - startedAt) / 1_000_000;
  logDebugSummary(
    settings,
    `[asp-lsp] LSP check slow completed: ${cached.source.uri} ${formatElapsedMs(elapsedMs)}, diagnostics=${diagnosticCount}, sequence=${sequence}`,
  );
}

function logDebugSummary(settings: AspSettings, message: string): void {
  if (isDebugSummaryEnabled(settings)) {
    connection.console.info(message);
  }
}

function finishDebugStep(
  settings: AspSettings,
  uri: string,
  step: string,
  startedAt: bigint,
): void {
  if (!isDebugVerboseEnabled(settings)) {
    return;
  }
  const elapsedMs = Number(process.hrtime.bigint() - startedAt) / 1_000_000;
  connection.console.info(`[asp-lsp] ${step}: ${uri} ${formatElapsedMs(elapsedMs)}`);
}

function measureDebugStep<T>(
  settings: AspSettings,
  uri: string,
  step: string,
  callback: () => T,
): T {
  const startedAt = process.hrtime.bigint();
  try {
    return callback();
  } finally {
    finishDebugStep(settings, uri, step, startedAt);
  }
}

function isDebugSummaryEnabled(settings: AspSettings): boolean {
  return settings.debug?.output === "summary" || settings.debug?.output === "verbose";
}

function isDebugVerboseEnabled(settings: AspSettings): boolean {
  return settings.debug?.output === "verbose";
}

function formatElapsedMs(elapsedMs: number): string {
  return `in ${elapsedMs.toFixed(1)} ms`;
}

function dedupeDiagnostics(diagnostics: Diagnostic[]): Diagnostic[] {
  const seen = new Set<string>();
  return diagnostics.filter((diagnostic) => {
    const key = diagnosticKey(diagnostic);
    if (seen.has(key)) {
      return false;
    }
    seen.add(key);
    return true;
  });
}

function diagnosticKey(diagnostic: Diagnostic): string {
  return JSON.stringify({
    source: diagnostic.source ?? "",
    code: diagnostic.code ?? "",
    severity: diagnostic.severity ?? "",
    range: diagnostic.range,
    message: diagnostic.message,
  });
}

function diagnosticsCacheKey(cached: CachedDocument, settings: AspSettings): string {
  return JSON.stringify({
    fast: fastDiagnosticsCacheKey(cached, settings),
    slow: slowDiagnosticsCacheKey(cached, settings),
  });
}

function fastDiagnosticsCacheKey(cached: CachedDocument, settings: AspSettings): string {
  return JSON.stringify({
    text: textFingerprint(cached.parsed.text),
    parsedDiagnostics: cached.parsed.diagnostics.map(diagnosticKey),
    settings: diagnosticsSettingsKey(settings),
  });
}

function slowDiagnosticsCacheKey(cached: CachedDocument, settings: AspSettings): string {
  return JSON.stringify({
    vbscript: vbDiagnosticsCacheKey(cached, settings),
    javascript: jsSlowDiagnosticsCacheKey(cached, settings),
  });
}

function diagnosticsSettingsKey(settings: AspSettings): unknown {
  return {
    checkJs: settings.checkJs ?? false,
    javascript: {
      unusedDiagnostics: settings.javascript?.unusedDiagnostics !== false,
      ignoreProjectConfig: settings.javascript?.ignoreProjectConfig === true,
    },
    vbscript: {
      typeChecking: settings.vbscript?.typeChecking,
      identifierCase: settings.vbscript?.identifierCase,
      identifierCaseByKind: settings.vbscript?.identifierCaseByKind,
      comTypes: settings.vbscript?.comTypes,
      globals: settings.vbscript?.globals,
      unusedDiagnostics: settings.vbscript?.unusedDiagnostics !== false,
    },
    locale: settings.resolvedLocale,
    virtualRoot: settings.virtualRoot,
    virtualRoots: settings.virtualRoots,
    legacyEncoding: settings.legacyEncoding,
  };
}

function vbDiagnosticsCacheKey(cached: CachedDocument, settings: AspSettings): string {
  return JSON.stringify({
    project: vbProjectContextCacheKey(collectVbProjectDocuments(cached.parsed, settings), settings),
    settings: {
      typeChecking: settings.vbscript?.typeChecking,
      identifierCase: settings.vbscript?.identifierCase,
      identifierCaseByKind: settings.vbscript?.identifierCaseByKind,
      comTypes: settings.vbscript?.comTypes,
      globals: settings.vbscript?.globals,
      unusedDiagnostics: settings.vbscript?.unusedDiagnostics !== false,
      locale: settings.resolvedLocale,
    },
  });
}

function jsSlowDiagnosticsCacheKey(cached: CachedDocument, settings: AspSettings): string {
  return JSON.stringify({
    virtuals: jsVirtualDocuments(cached).map(virtualCacheKey),
    settings: {
      checkJs: settings.checkJs ?? false,
      unusedDiagnostics: settings.javascript?.unusedDiagnostics !== false,
      ignoreProjectConfig: settings.javascript?.ignoreProjectConfig === true,
    },
    project:
      settings.checkJs === true
        ? jsVirtualDocuments(cached).map((virtual) =>
            jsLanguageServiceCacheKey(virtual, settings, {}),
          )
        : undefined,
  });
}

function virtualCacheKey(virtual: VirtualDocument): string {
  return JSON.stringify({
    uri: virtual.uri,
    languageId: virtual.languageId,
    text: textFingerprint(virtual.text),
    segments: virtual.sourceMap.segments,
  });
}

function analysisFor(cached: CachedDocument): CachedAnalysis {
  cached.analysis ??= {};
  return cached.analysis;
}

function reuseUnchangedDiagnostics(
  cached: CachedDocument,
  language: AspIncrementalChange["language"],
  entry: DiagnosticCacheEntry | undefined,
): Diagnostic[] | undefined {
  const changes = cached.changes;
  if (!changes || changes.length === 0 || !entry || isDiagnosticLanguageDirty(cached, language)) {
    return undefined;
  }
  const items = shiftDiagnosticsAfterChanges(entry.items, entry.text, cached.source, changes);
  entry.items = items;
  entry.text = cached.parsed.text;
  entry.key = `${entry.key}|shift:${changes
    .map((change) => `${change.start}:${change.end}:${change.delta}`)
    .join(",")}`;
  return items;
}

function isDiagnosticLanguageDirty(
  cached: CachedDocument,
  language: AspIncrementalChange["language"],
): boolean {
  if (!cached.dirtyLanguages) {
    return false;
  }
  if (language === "javascript" || language === "jscript") {
    return cached.dirtyLanguages.has("javascript") || cached.dirtyLanguages.has("jscript");
  }
  return cached.dirtyLanguages.has(language);
}

function shiftDiagnosticsAfterChanges(
  diagnostics: Diagnostic[],
  previousText: string,
  document: TextDocument,
  changes: AspIncrementalChange[],
): Diagnostic[] {
  const previousDocument = TextDocument.create(document.uri, document.languageId, 0, previousText);
  return diagnostics.map((diagnostic) => ({
    ...diagnostic,
    range: shiftDiagnosticRange(previousDocument, document, diagnostic.range, changes),
  }));
}

function shiftDiagnosticRange(
  previousDocument: TextDocument,
  nextDocument: TextDocument,
  range: Range,
  changes: AspIncrementalChange[],
): Range {
  const start = changes.reduce(
    (offset, change) => shiftDiagnosticOffset(offset, change),
    previousDocument.offsetAt(range.start),
  );
  const end = changes.reduce(
    (offset, change) => shiftDiagnosticOffset(offset, change),
    previousDocument.offsetAt(range.end),
  );
  return { start: nextDocument.positionAt(start), end: nextDocument.positionAt(end) };
}

function shiftDiagnosticOffset(offset: number, change: AspIncrementalChange): number {
  if (offset >= change.end) {
    return offset + change.delta;
  }
  return offset;
}

function htmlDiagnostics(cached: CachedDocument): Diagnostic[] {
  const virtual = cached.virtuals.get("html");
  if (!virtual) {
    return [];
  }
  const reused = reuseUnchangedDiagnostics(cached, "html", cached.analysis?.htmlDiagnostics);
  if (reused) {
    return reused;
  }
  const key = virtualCacheKey(virtual);
  if (cached.analysis?.htmlDiagnostics?.key === key) {
    return cached.analysis.htmlDiagnostics.items;
  }
  const scanner = htmlService.createScanner(virtual.text);
  const diagnostics: Diagnostic[] = [];
  let token = scanner.scan();
  while (token !== TokenType.EOS) {
    const error = scanner.getTokenError();
    if (error) {
      diagnostics.push({
        severity: DiagnosticSeverity.Warning,
        range: {
          start: cached.source.positionAt(scanner.getTokenOffset()),
          end: cached.source.positionAt(scanner.getTokenEnd()),
        },
        message: error,
        source: "asp-lsp-html",
      });
    }
    token = scanner.scan();
  }
  analysisFor(cached).htmlDiagnostics = { key, items: diagnostics, text: cached.parsed.text };
  return diagnostics;
}

function cssDiagnostics(cached: CachedDocument): Diagnostic[] {
  const virtual = cached.virtuals.get("css");
  if (!virtual) {
    return [];
  }
  const reused = reuseUnchangedDiagnostics(cached, "css", cached.analysis?.cssDiagnostics);
  if (reused) {
    return reused;
  }
  const key = virtualCacheKey(virtual);
  if (cached.analysis?.cssDiagnostics?.key === key) {
    return cached.analysis.cssDiagnostics.items;
  }
  const doc = toTextDocument(virtual);
  const items = cssService
    .doValidation(doc, cssService.parseStylesheet(doc))
    .map((diagnostic) => remapDiagnostic(virtual, diagnostic, "asp-lsp-css"))
    .filter(isDiagnostic);
  analysisFor(cached).cssDiagnostics = { key, items, text: cached.parsed.text };
  return items;
}

function jsSyntaxDiagnostics(cached: CachedDocument): Diagnostic[] {
  const reused =
    reuseUnchangedDiagnostics(cached, "javascript", cached.analysis?.jsSyntaxDiagnostics) ??
    reuseUnchangedDiagnostics(cached, "jscript", cached.analysis?.jsSyntaxDiagnostics);
  if (reused) {
    return reused;
  }
  const key = JSON.stringify({
    virtuals: jsVirtualDocuments(cached).map(virtualCacheKey),
  });
  if (cached.analysis?.jsSyntaxDiagnostics?.key === key) {
    return cached.analysis.jsSyntaxDiagnostics.items;
  }
  const items = jsVirtualDocuments(cached).flatMap((virtual) => {
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
    return parseDiagnostics
      .map((diagnostic) => tsDiagnosticToLsp(virtual, diagnostic))
      .filter(isDiagnostic);
  });
  analysisFor(cached).jsSyntaxDiagnostics = { key, items, text: cached.parsed.text };
  return items;
}

function jsSlowDiagnostics(
  cached: CachedDocument,
  settings: AspSettings,
  stepPrefix: string,
): Diagnostic[] {
  const reused =
    reuseUnchangedDiagnostics(cached, "javascript", cached.analysis?.jsSlowDiagnostics) ??
    reuseUnchangedDiagnostics(cached, "jscript", cached.analysis?.jsSlowDiagnostics);
  if (reused) {
    return reused;
  }
  const key = jsSlowDiagnosticsCacheKey(cached, settings);
  if (cached.analysis?.jsSlowDiagnostics?.key === key) {
    return cached.analysis.jsSlowDiagnostics.items;
  }
  const items = jsVirtualDocuments(cached).flatMap((virtual) => {
    const semantic = measureDebugStep(
      settings,
      cached.source.uri,
      `${stepPrefix}.javascriptSemantic`,
      () => {
        if (settings.checkJs !== true) {
          return [];
        }
        const service = createJsLanguageService(virtual, settings).service;
        return service.getSemanticDiagnostics(jsVirtualFileName(virtual.uri));
      },
    );
    const unused = measureDebugStep(
      settings,
      cached.source.uri,
      `${stepPrefix}.javascriptUnused`,
      () =>
        settings.javascript?.unusedDiagnostics === false
          ? []
          : lightweightJsUnusedDiagnostics(virtual),
    );
    const semanticKeys = new Set(semantic.map(tsDiagnosticKey));
    const unusedOnly = unused.filter(
      (diagnostic) => !semanticKeys.has(tsDiagnosticKey(diagnostic)),
    );
    return [
      ...semantic.map((diagnostic) => tsDiagnosticToLsp(virtual, diagnostic)),
      ...unusedOnly.map((diagnostic) =>
        tsDiagnosticToLsp(virtual, diagnostic, {
          severity: DiagnosticSeverity.Hint,
          source: "asp-lsp-typescript-unused",
        }),
      ),
    ].filter(isDiagnostic);
  });
  analysisFor(cached).jsSlowDiagnostics = { key, items, text: cached.parsed.text };
  return items;
}

function vbDiagnostics(
  cached: CachedDocument,
  settings: AspSettings,
  stepPrefix: string,
): Diagnostic[] {
  const reused = reuseUnchangedDiagnostics(cached, "vbscript", cached.analysis?.vbDiagnostics);
  if (reused) {
    return reused;
  }
  const key = vbDiagnosticsCacheKey(cached, settings);
  if (cached.analysis?.vbDiagnostics?.key === key) {
    return cached.analysis.vbDiagnostics.items;
  }
  const context = measureDebugStep(
    settings,
    cached.source.uri,
    `${stepPrefix}.vbscript.projectContext`,
    () => buildVbProjectContext(cached, settings),
  );
  const items = measureDebugStep(
    settings,
    cached.source.uri,
    `${stepPrefix}.vbscript.diagnostics`,
    () => analyzeVbscript(cached.parsed, context).diagnostics,
  );
  analysisFor(cached).vbDiagnostics = { key, items, text: cached.parsed.text };
  return items;
}

function lightweightJsUnusedDiagnostics(virtual: VirtualDocument): ts.Diagnostic[] {
  const fileName = jsVirtualFileName(virtual.uri);
  const files = new Map([[normalizeFileName(fileName), virtual.text]]);
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
      .filter((diagnostic) => tsUnusedDiagnosticCodes.has(diagnostic.code));
  } finally {
    service.dispose();
  }
}

function cssCompletion(
  cached: CachedDocument,
  params: TextDocumentPositionParams,
  language: "css",
): CompletionItem[] {
  const virtual = cached.virtuals.get(language);
  if (!virtual) {
    return [];
  }
  const position = virtual.sourceMap.toVirtualPosition(params.position);
  if (!position) {
    return [];
  }
  const doc = toTextDocument(virtual);
  return cssService.doComplete(doc, position, cssService.parseStylesheet(doc)).items;
}

function jsCompletion(
  cached: CachedDocument,
  params: TextDocumentPositionParams,
): CompletionItem[] {
  const context = jsContextAt(cached, params.position);
  if (!context) {
    return [];
  }
  const { offset, service, fileName } = context;
  const preferences = jsCompletionPreferences(cachedSettings(cached.source.uri));
  return (
    service
      .getCompletionsAtPosition(fileName, offset, preferences)
      ?.entries.filter((entry) => !hiddenJavaScriptGlobalCompletions.has(entry.name))
      .map((entry) => {
        const details =
          entry.hasAction || entry.source
            ? safeGetCompletionEntryDetails(
                service,
                fileName,
                offset,
                entry.name,
                entry.source,
                preferences,
                entry.data,
              )
            : undefined;
        return {
          label: entry.name,
          kind: tsCompletionKind(entry.kind),
          detail: entry.kind,
          data: {
            kind: "javascript",
            uri: cached.source.uri,
            language: context.virtual.languageId,
            name: entry.name,
            virtualOffset: offset,
            source: entry.source,
            tsData: entry.data,
            importTextChanges: details?.codeActions?.flatMap((action) => action.changes),
          },
        };
      }) ?? []
  );
}

function jsCompletionPreferences(settings: AspSettings): ts.GetCompletionsAtPositionOptions {
  if (settings.javascript?.autoImports === false) {
    return {};
  }
  return {
    includeCompletionsForModuleExports: true,
    includeCompletionsForImportStatements: true,
  };
}

function safeGetCompletionEntryDetails(
  service: ts.LanguageService,
  fileName: string,
  position: number,
  name: string,
  source: string | undefined,
  preferences: ts.UserPreferences,
  data: ts.CompletionEntryData | undefined,
): ts.CompletionEntryDetails | undefined {
  try {
    return service.getCompletionEntryDetails(
      fileName,
      position,
      name,
      {},
      source,
      preferences,
      data,
    );
  } catch {
    return undefined;
  }
}

function jsHover(cached: CachedDocument, position: Position): Hover | null {
  const context = jsContextAt(cached, position);
  if (!context) {
    return null;
  }
  const quickInfo = context.service.getQuickInfoAtPosition(context.fileName, context.offset);
  if (!quickInfo) {
    return null;
  }
  const signature = ts.displayPartsToString(quickInfo.displayParts);
  const docs = ts.displayPartsToString(quickInfo.documentation);
  return {
    contents: {
      kind: "markdown",
      value: docs
        ? `\`\`\`javascript\n${signature}\n\`\`\`\n\n${docs}`
        : `\`\`\`javascript\n${signature}\n\`\`\``,
    },
    range: textSpanToSourceRange(context.virtual, quickInfo.textSpan),
  };
}

function jsReferences(cached: CachedDocument, position: Position): Location[] {
  const context = jsContextAt(cached, position);
  if (!context) {
    return [];
  }
  return (
    context.service
      .getReferencesAtPosition(context.fileName, context.offset)
      ?.map((reference) => tsReferenceToLocation(context, reference))
      .filter((location): location is Location => Boolean(location)) ?? []
  );
}

function jsPrepareRename(cached: CachedDocument, position: Position): Range | null {
  const context = jsContextAt(cached, position);
  if (!context) {
    return null;
  }
  const info = context.service.getRenameInfo(context.fileName, context.offset, {});
  if (!info.canRename) {
    return null;
  }
  return textSpanToSourceRange(context.virtual, info.triggerSpan) ?? null;
}

function jsRename(
  cached: CachedDocument,
  position: Position,
  newName: string,
): WorkspaceEdit | null {
  const context = jsContextAt(cached, position);
  if (!context || !/^[\p{ID_Start}_$][\p{ID_Continue}_$]*$/u.test(newName)) {
    return null;
  }
  const locations = context.service.findRenameLocations(
    context.fileName,
    context.offset,
    false,
    false,
    {},
  );
  if (!locations) {
    return null;
  }
  const changes: NonNullable<WorkspaceEdit["changes"]> = {};
  for (const location of locations) {
    const mapped = textSpanToLocation(context, location.fileName, location.textSpan);
    if (!mapped) {
      continue;
    }
    changes[mapped.uri] = [
      ...(changes[mapped.uri] ?? []),
      { range: mapped.range, newText: newName },
    ];
  }
  return Object.keys(changes).length > 0 ? { changes } : null;
}

function htmlPrepareRename(cached: CachedDocument, position: Position): Range | null {
  const ranges = htmlLinkedRanges(cached, position);
  return ranges?.[0] ?? wordRangeAt(cached.source, position);
}

function htmlRename(
  cached: CachedDocument,
  position: Position,
  newName: string,
): WorkspaceEdit | null {
  const virtual = cached.virtuals.get("html");
  if (!virtual) {
    return null;
  }
  const doc = toTextDocument(virtual);
  const edit = htmlService.doRename(doc, position, newName, htmlService.parseHTMLDocument(doc));
  return edit ? remapWorkspaceEdit(virtual, edit, cached.source.uri) : null;
}

function cssPrepareRename(cached: CachedDocument, position: Position): Range | null {
  const virtual = cached.virtuals.get("css");
  const virtualPosition = virtual?.sourceMap.toVirtualPosition(position);
  if (!virtual || !virtualPosition) {
    return null;
  }
  const doc = toTextDocument(virtual);
  const range = cssService.prepareRename(doc, virtualPosition, cssService.parseStylesheet(doc));
  return range ? (sourceRangeFromVirtualRange(virtual, range) ?? null) : null;
}

function cssRename(
  cached: CachedDocument,
  position: Position,
  newName: string,
): WorkspaceEdit | null {
  const virtual = cached.virtuals.get("css");
  const virtualPosition = virtual?.sourceMap.toVirtualPosition(position);
  if (!virtual || !virtualPosition) {
    return null;
  }
  const doc = toTextDocument(virtual);
  return remapWorkspaceEdit(
    virtual,
    cssService.doRename(doc, virtualPosition, newName, cssService.parseStylesheet(doc)),
    cached.source.uri,
  );
}

function crossLanguageRename(
  cached: CachedDocument,
  position: Position,
  newName: string,
): WorkspaceEdit | undefined {
  const target = crossLanguageRenameTarget(cached, position);
  if (!target || !/^[A-Za-z_][A-Za-z0-9_-]*$/.test(newName)) {
    return undefined;
  }
  const changes: NonNullable<WorkspaceEdit["changes"]> = {};
  const seen = new Set<string>();
  for (const candidate of crossLanguageRenameCandidates(cached)) {
    const text = candidate.source.getText();
    const edits = [
      ...htmlAttributeRenameEdits(candidate, target, newName),
      ...cssSelectorRenameEdits(candidate, target, newName),
      ...jsSelectorRenameEdits(candidate, target, newName),
    ];
    for (const edit of edits) {
      const key = `${candidate.source.uri}:${offsetAtText(text, edit.range.start)}:${offsetAtText(text, edit.range.end)}`;
      if (seen.has(key)) {
        continue;
      }
      seen.add(key);
      changes[candidate.source.uri] = [...(changes[candidate.source.uri] ?? []), edit];
    }
  }
  return Object.keys(changes).length > 0 ? { changes } : undefined;
}

function crossLanguageRenameCandidates(active: CachedDocument): CachedDocument[] {
  ensureWorkspaceIndex();
  const candidates: CachedDocument[] = [active];
  const seen = new Set([active.source.uri]);
  for (const document of documents.all()) {
    if (seen.has(document.uri)) {
      continue;
    }
    const cached = getCached(document.uri);
    if (cached) {
      seen.add(cached.source.uri);
      candidates.push(cached);
    }
  }
  for (const entry of workspaceIndex.values()) {
    if (seen.has(entry.uri)) {
      continue;
    }
    seen.add(entry.uri);
    candidates.push(cachedFromIndexed(entry));
  }
  return candidates;
}

function crossLanguagePrepareRename(cached: CachedDocument, position: Position): Range | null {
  return crossLanguageRenameTarget(cached, position) ? wordRangeAt(cached.source, position) : null;
}

function crossLanguageRenameTarget(
  cached: CachedDocument,
  position: Position,
): { kind: "id" | "class"; name: string } | undefined {
  const region = findRegionAt(cached.parsed, cached.source.offsetAt(position));
  if (region?.language === "html") {
    return htmlAttributeRenameTarget(cached, position);
  }
  if (region?.language === "css") {
    return cssSelectorRenameTarget(cached, position);
  }
  if (region && isJavaScriptLikeRegion(region)) {
    return jsSelectorRenameTarget(cached, position);
  }
  return undefined;
}

function htmlAttributeRenameTarget(
  cached: CachedDocument,
  position: Position,
): { kind: "id" | "class"; name: string } | undefined {
  const offset = cached.source.offsetAt(position);
  const text = cached.source.getText();
  for (const match of text.matchAll(/\b(id|class)\s*=\s*(["'])([^"']*)\2/gi)) {
    const value = match[3] ?? "";
    const valueStart = (match.index ?? 0) + match[0].indexOf(value);
    const valueEnd = valueStart + value.length;
    if (offset < valueStart || offset > valueEnd) {
      continue;
    }
    const kind = (match[1]?.toLowerCase() === "id" ? "id" : "class") as "id" | "class";
    if (kind === "id") {
      return { kind, name: value };
    }
    return classTokenAt(value, valueStart, offset);
  }
  return undefined;
}

function cssSelectorRenameTarget(
  cached: CachedDocument,
  position: Position,
): { kind: "id" | "class"; name: string } | undefined {
  const text = cached.source.getText();
  const offset = cached.source.offsetAt(position);
  let start = offset;
  while (start > 0 && /[A-Za-z0-9_-]/.test(text[start - 1] ?? "")) {
    start -= 1;
  }
  let end = offset;
  while (end < text.length && /[A-Za-z0-9_-]/.test(text[end] ?? "")) {
    end += 1;
  }
  const prefix = text[start - 1];
  if ((prefix !== "." && prefix !== "#") || start === end) {
    return undefined;
  }
  return { kind: prefix === "#" ? "id" : "class", name: text.slice(start, end) };
}

function jsSelectorRenameTarget(
  cached: CachedDocument,
  position: Position,
): { kind: "id" | "class"; name: string } | undefined {
  const offset = cached.source.offsetAt(position);
  for (const match of cached.source.getText().matchAll(/(["'])([^"']*)\1/g)) {
    const value = match[2] ?? "";
    const valueStart = (match.index ?? 0) + 1;
    const valueEnd = valueStart + value.length;
    if (offset < valueStart || offset > valueEnd) {
      continue;
    }
    if (isSelectorStringContext(cached.source.getText(), match.index ?? 0, "getElementById")) {
      return { kind: "id", name: value };
    }
    if (isSelectorStringContext(cached.source.getText(), match.index ?? 0, "classList")) {
      return { kind: "class", name: value };
    }
    const selector = selectorTokenAt(value, valueStart, offset);
    if (
      selector &&
      (isSelectorStringContext(cached.source.getText(), match.index ?? 0, "querySelector") ||
        isSelectorStringContext(cached.source.getText(), match.index ?? 0, "querySelectorAll"))
    ) {
      return selector;
    }
  }
  return undefined;
}

function htmlAttributeRenameEdits(
  cached: CachedDocument,
  target: { kind: "id" | "class"; name: string },
  newName: string,
): TextEdit[] {
  const edits: TextEdit[] = [];
  const text = cached.source.getText();
  for (const match of text.matchAll(/\b(id|class)\s*=\s*(["'])([^"']*)\2/gi)) {
    const kind = match[1]?.toLowerCase() === "id" ? "id" : "class";
    const value = match[3] ?? "";
    const valueStart = (match.index ?? 0) + match[0].indexOf(value);
    if (kind === "id" && target.kind === "id" && value === target.name) {
      edits.push(offsetTextEdit(cached.source, valueStart, valueStart + value.length, newName));
    }
    if (kind === "class" && target.kind === "class") {
      edits.push(...classTokenEdits(cached.source, value, valueStart, target.name, newName));
    }
  }
  return edits;
}

function cssSelectorRenameEdits(
  cached: CachedDocument,
  target: { kind: "id" | "class"; name: string },
  newName: string,
): TextEdit[] {
  const edits: TextEdit[] = [];
  const text = cached.source.getText();
  const prefix = target.kind === "id" ? "#" : ".";
  const pattern = new RegExp(`\\${prefix}${escapeRegExp(target.name)}\\b`, "g");
  for (const match of text.matchAll(pattern)) {
    const start = (match.index ?? 0) + 1;
    if (findRegionAt(cached.parsed, start)?.language === "css") {
      edits.push(offsetTextEdit(cached.source, start, start + target.name.length, newName));
    }
  }
  return edits;
}

function jsSelectorRenameEdits(
  cached: CachedDocument,
  target: { kind: "id" | "class"; name: string },
  newName: string,
): TextEdit[] {
  const edits: TextEdit[] = [];
  const text = cached.source.getText();
  for (const match of text.matchAll(/(["'])([^"']*)\1/g)) {
    const quoteStart = match.index ?? 0;
    const region = findRegionAt(cached.parsed, quoteStart);
    if (!region || !isJavaScriptLikeRegion(region)) {
      continue;
    }
    const value = match[2] ?? "";
    const valueStart = quoteStart + 1;
    if (
      target.kind === "id" &&
      value === target.name &&
      isSelectorStringContext(text, quoteStart, "getElementById")
    ) {
      edits.push(offsetTextEdit(cached.source, valueStart, valueStart + value.length, newName));
    }
    if (
      target.kind === "class" &&
      value === target.name &&
      isSelectorStringContext(text, quoteStart, "classList")
    ) {
      edits.push(offsetTextEdit(cached.source, valueStart, valueStart + value.length, newName));
    }
    if (
      isSelectorStringContext(text, quoteStart, "querySelector") ||
      isSelectorStringContext(text, quoteStart, "querySelectorAll")
    ) {
      edits.push(
        ...selectorTokenEdits(cached.source, value, valueStart, target.kind, target.name, newName),
      );
    }
  }
  return edits;
}

function classTokenAt(
  value: string,
  valueStart: number,
  offset: number,
): { kind: "class"; name: string } | undefined {
  for (const match of value.matchAll(/[A-Za-z_][A-Za-z0-9_-]*/g)) {
    const start = valueStart + (match.index ?? 0);
    const end = start + match[0].length;
    if (offset >= start && offset <= end) {
      return { kind: "class", name: match[0] };
    }
  }
  return undefined;
}

function selectorTokenAt(
  value: string,
  valueStart: number,
  offset: number,
): { kind: "id" | "class"; name: string } | undefined {
  for (const match of value.matchAll(/([#.])([A-Za-z_][A-Za-z0-9_-]*)/g)) {
    const nameStart = valueStart + (match.index ?? 0) + 1;
    const nameEnd = nameStart + (match[2]?.length ?? 0);
    if (offset >= nameStart && offset <= nameEnd) {
      return { kind: match[1] === "#" ? "id" : "class", name: match[2] ?? "" };
    }
  }
  return undefined;
}

function classTokenEdits(
  document: TextDocument,
  value: string,
  valueStart: number,
  oldName: string,
  newName: string,
): TextEdit[] {
  return [...value.matchAll(/[A-Za-z_][A-Za-z0-9_-]*/g)]
    .filter((match) => match[0] === oldName)
    .map((match) =>
      offsetTextEdit(
        document,
        valueStart + (match.index ?? 0),
        valueStart + (match.index ?? 0) + match[0].length,
        newName,
      ),
    );
}

function selectorTokenEdits(
  document: TextDocument,
  value: string,
  valueStart: number,
  kind: "id" | "class",
  oldName: string,
  newName: string,
): TextEdit[] {
  return [...value.matchAll(/([#.])([A-Za-z_][A-Za-z0-9_-]*)/g)]
    .filter((match) => (kind === "id" ? match[1] === "#" : match[1] === "."))
    .filter((match) => match[2] === oldName)
    .map((match) =>
      offsetTextEdit(
        document,
        valueStart + (match.index ?? 0) + 1,
        valueStart + (match.index ?? 0) + 1 + oldName.length,
        newName,
      ),
    );
}

function isSelectorStringContext(text: string, quoteStart: number, callName: string): boolean {
  const prefix = text.slice(Math.max(0, quoteStart - 80), quoteStart);
  return new RegExp(`${callName.replace(".", "\\.")}\\s*\\([^)]*$`).test(prefix);
}

function offsetTextEdit(
  document: TextDocument,
  start: number,
  end: number,
  newText: string,
): TextEdit {
  return {
    range: { start: document.positionAt(start), end: document.positionAt(end) },
    newText,
  };
}

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function jsSignatureHelp(cached: CachedDocument, position: Position): SignatureHelp | null {
  const context = jsContextAt(cached, position);
  if (!context) {
    return null;
  }
  const help = context.service.getSignatureHelpItems(context.fileName, context.offset, undefined);
  if (!help) {
    return null;
  }
  return {
    signatures: help.items.map((item) => ({
      label:
        ts.displayPartsToString(item.prefixDisplayParts) +
        item.parameters
          .map((parameter) => ts.displayPartsToString(parameter.displayParts))
          .join(ts.displayPartsToString(item.separatorDisplayParts)) +
        ts.displayPartsToString(item.suffixDisplayParts),
      documentation: ts.displayPartsToString(item.documentation),
      parameters: item.parameters.map((parameter) => ({
        label: ts.displayPartsToString(parameter.displayParts),
        documentation: ts.displayPartsToString(parameter.documentation),
      })),
    })),
    activeSignature: help.selectedItemIndex,
    activeParameter: help.argumentIndex,
  };
}

function jsDocumentHighlights(cached: CachedDocument, position: Position): DocumentHighlight[] {
  const context = jsContextAt(cached, position);
  if (!context) {
    return [];
  }
  return (
    context.service
      .getDocumentHighlights(context.fileName, context.offset, [context.fileName])
      ?.flatMap((fileHighlights) =>
        fileHighlights.highlightSpans
          .map((span): DocumentHighlight | undefined => {
            const range = textSpanToSourceRange(context.virtual, span.textSpan);
            return range
              ? {
                  range,
                  kind:
                    span.kind === "writtenReference"
                      ? DocumentHighlightKind.Write
                      : DocumentHighlightKind.Read,
                }
              : undefined;
          })
          .filter((highlight): highlight is DocumentHighlight => Boolean(highlight)),
      ) ?? []
  );
}

function htmlDocumentHighlights(cached: CachedDocument, position: Position): DocumentHighlight[] {
  const virtual = cached.virtuals.get("html");
  if (!virtual) {
    return [];
  }
  const doc = toTextDocument(virtual);
  const service = htmlService as {
    findDocumentHighlights?: (
      document: TextDocument,
      position: Position,
      htmlDocument: unknown,
    ) => DocumentHighlight[];
  };
  return service.findDocumentHighlights?.(doc, position, htmlService.parseHTMLDocument(doc)) ?? [];
}

function cssDocumentHighlights(cached: CachedDocument, position: Position): DocumentHighlight[] {
  const virtual = cached.virtuals.get("css");
  const virtualPosition = virtual?.sourceMap.toVirtualPosition(position);
  if (!virtual || !virtualPosition) {
    return [];
  }
  const doc = toTextDocument(virtual);
  const service = cssService as {
    findDocumentHighlights?: (
      document: TextDocument,
      position: Position,
      stylesheet: unknown,
    ) => DocumentHighlight[];
  };
  return (
    service
      .findDocumentHighlights?.(doc, virtualPosition, cssService.parseStylesheet(doc))
      .map((highlight) => {
        const range = sourceRangeFromVirtualRange(virtual, highlight.range);
        return range ? { ...highlight, range } : undefined;
      })
      .filter((highlight): highlight is DocumentHighlight => Boolean(highlight)) ?? []
  );
}

function jsInlayHints(cached: CachedDocument, range: Range): InlayHint[] {
  const settings = cachedSettings(cached.source.uri);
  const hints = settings.inlayHints;
  if (
    hints?.parameterNames === false &&
    hints.variableTypes === false &&
    hints.functionReturnTypes === false
  ) {
    return [];
  }
  return jsVirtualDocuments(cached).flatMap((virtual) => {
    const sourceStart = cached.source.offsetAt(range.start);
    const sourceEnd = cached.source.offsetAt(range.end);
    const segment = virtual.sourceMap.segments.find(
      (candidate) => candidate.sourceStart < sourceEnd && candidate.sourceEnd > sourceStart,
    );
    if (!segment) {
      return [];
    }
    const start = segment.virtualStart + Math.max(0, sourceStart - segment.sourceStart);
    const end = segment.virtualStart + Math.min(segment.sourceEnd, sourceEnd) - segment.sourceStart;
    if (start === undefined || end === undefined || start >= end) {
      return [];
    }
    const project = createJsLanguageService(virtual, settings);
    return project.service
      .provideInlayHints(
        jsVirtualFileName(virtual.uri),
        { start, length: end - start },
        {
          includeInlayParameterNameHints: hints?.parameterNames === false ? "none" : "all",
          includeInlayVariableTypeHints: hints?.variableTypes !== false,
          includeInlayFunctionLikeReturnTypeHints: hints?.functionReturnTypes !== false,
          includeInlayPropertyDeclarationTypeHints: hints?.variableTypes !== false,
        },
      )
      .map((hint): InlayHint | undefined => {
        const sourcePosition = virtual.sourceMap.toSourcePosition(
          toTextDocument(virtual).positionAt(hint.position),
        );
        if (!sourcePosition || !isJavaScriptPosition(cached, sourcePosition)) {
          return undefined;
        }
        const label =
          hint.text ||
          hint.displayParts
            ?.map((part) => part.text)
            .join("")
            .trim();
        return label ? { position: sourcePosition, label } : undefined;
      })
      .filter((hint): hint is InlayHint => Boolean(hint));
  });
}

function jsPrepareCallHierarchy(cached: CachedDocument, position: Position): CallHierarchyItem[] {
  const context = jsContextAt(cached, position);
  if (!context) {
    return [];
  }
  const items = context.service.prepareCallHierarchy(context.fileName, context.offset);
  const list = Array.isArray(items) ? items : items ? [items] : [];
  return list
    .map((item) => tsCallHierarchyItemToLsp(context, item, cached.source.uri))
    .filter((item): item is CallHierarchyItem => Boolean(item));
}

function jsIncomingCalls(item: CallHierarchyItem): CallHierarchyIncomingCall[] {
  const context = jsCallHierarchyContext(item);
  if (!context) {
    return [];
  }
  return context.service
    .provideCallHierarchyIncomingCalls(context.fileName, context.offset)
    .map((call) => {
      const from = tsCallHierarchyItemToLsp(context, call.from, context.rootUri);
      return from
        ? {
            from,
            fromRanges: call.fromSpans
              .map((span) => textSpanToLocation(context, call.from.file, span)?.range)
              .filter((range): range is Range => Boolean(range)),
          }
        : undefined;
    })
    .filter((call): call is CallHierarchyIncomingCall => Boolean(call));
}

function jsOutgoingCalls(item: CallHierarchyItem): CallHierarchyOutgoingCall[] {
  const context = jsCallHierarchyContext(item);
  if (!context) {
    return [];
  }
  return context.service
    .provideCallHierarchyOutgoingCalls(context.fileName, context.offset)
    .map((call) => {
      const to = tsCallHierarchyItemToLsp(context, call.to, context.rootUri);
      return to
        ? {
            to,
            fromRanges: call.fromSpans
              .map((span) => textSpanToLocation(context, context.fileName, span)?.range)
              .filter((range): range is Range => Boolean(range)),
          }
        : undefined;
    })
    .filter((call): call is CallHierarchyOutgoingCall => Boolean(call));
}

function isJsCallHierarchyItem(item: CallHierarchyItem): boolean {
  return (item.data as Partial<JsCallHierarchyData> | undefined)?.kind === "javascript";
}

function jsCallHierarchyContext(
  item: CallHierarchyItem,
): (JsProjectContext & { rootUri: string }) | undefined {
  const data = item.data as Partial<JsCallHierarchyData> | undefined;
  if (data?.kind !== "javascript" || !data.rootUri || !data.language) {
    return undefined;
  }
  const cached = getCached(data.rootUri);
  const virtual = cached?.virtuals.get(data.language);
  if (!cached || !virtual || typeof data.position !== "number") {
    return undefined;
  }
  const project = createJsLanguageService(virtual, cachedSettings(data.rootUri));
  return {
    virtual,
    service: project.service,
    fileName: data.fileName ?? jsVirtualFileName(virtual.uri),
    offset: data.position,
    files: project.files,
    rootUri: data.rootUri,
  };
}

function tsCallHierarchyItemToLsp(
  context: JsProjectContext,
  item: ts.CallHierarchyItem,
  rootUri: string,
): CallHierarchyItem | undefined {
  const range = textSpanToLocation(context, item.file, item.span)?.range;
  const selectionRange = textSpanToLocation(context, item.file, item.selectionSpan)?.range;
  const location = textSpanToLocation(context, item.file, item.selectionSpan);
  if (!range || !selectionRange || !location) {
    return undefined;
  }
  return {
    name: item.name,
    kind: tsSymbolKind(item.kind),
    detail: item.containerName,
    uri: location.uri,
    range,
    selectionRange,
    data: {
      kind: "javascript",
      rootUri,
      language: context.virtual.languageId,
      fileName: item.file,
      position: item.selectionSpan.start,
    } satisfies JsCallHierarchyData,
  };
}

function vbTypeHierarchyItemAt(
  cached: CachedDocument,
  position: Position,
): TypeHierarchyItem | undefined {
  if (!isVbscriptPosition(cached, position)) {
    return undefined;
  }
  const context = buildVbProjectContext(cached, cachedSettings(cached.source.uri));
  const symbol =
    getVbscriptDefinition(cached.parsed, position, context) ??
    getVbscriptTypeDefinition(cached.parsed, position, context);
  if (!symbol) {
    return undefined;
  }
  const typeName =
    symbol.kind === "class" ? symbol.name : (symbol.type?.name ?? symbol.typeName ?? symbol.name);
  const type = vbTypeByName(context, typeName);
  if (!type || type.kind === "intrinsic") {
    return undefined;
  }
  return vbTypeHierarchyItem(type, cached.source.uri, symbol.range, context);
}

function vbTypeHierarchyItem(
  type: VbType,
  rootUri: string,
  fallbackRange: Range,
  context: VbProjectContext,
): TypeHierarchyItem {
  const symbol = context.symbols?.find(
    (candidate) =>
      candidate.kind === "class" &&
      candidate.name.toLowerCase() === type.name.toLowerCase() &&
      candidate.sourceUri,
  );
  const range = symbol?.range ?? fallbackRange;
  const uri = symbol?.sourceUri ?? rootUri;
  return {
    name: type.name,
    kind: type.kind === "com" ? SymbolKind.Interface : SymbolKind.Class,
    detail: type.kind === "com" ? "COM type catalog" : uri,
    uri,
    range,
    selectionRange: range,
    data: {
      kind: "vbscript",
      rootUri,
      uri,
      typeName: type.name,
      line: range.start.line,
      character: range.start.character,
    } satisfies VbTypeHierarchyData,
  };
}

function vbTypeHierarchyRelatedItems(item: TypeHierarchyItem): TypeHierarchyItem[] {
  const data = item.data as Partial<VbTypeHierarchyData> | undefined;
  if (data?.kind !== "vbscript" || !data.typeName) {
    return [];
  }
  const cached = getCached(data.rootUri ?? item.uri) ?? getCached(item.uri);
  if (!cached) {
    return [];
  }
  const context = buildVbProjectContext(cached, cachedSettings(cached.source.uri));
  const type = vbTypeByName(context, data.typeName);
  if (!type) {
    return [];
  }
  const seen = new Set<string>();
  return type.members.flatMap((member) => {
    const typeName = member.type?.name ?? member.signature?.returnType?.name;
    const related = typeName ? vbTypeByName(context, typeName) : undefined;
    if (!related || related.kind === "intrinsic" || related.name === type.name) {
      return [];
    }
    const key = related.name.toLowerCase();
    if (seen.has(key)) {
      return [];
    }
    seen.add(key);
    return [vbTypeHierarchyItem(related, cached.source.uri, item.selectionRange, context)];
  });
}

function vbTypeByName(context: VbProjectContext, name: string): VbType | undefined {
  return context.typeEnvironment?.types.find(
    (type) => type.name.toLowerCase() === name.toLowerCase(),
  );
}

function monikersAt(cached: CachedDocument, position: Position): Moniker[] {
  if (isVbscriptPosition(cached, position)) {
    return vbMonikersAt(cached, position);
  }
  if (isJavaScriptPosition(cached, position)) {
    return jsMonikersAt(cached, position);
  }
  return [];
}

function vbMonikersAt(cached: CachedDocument, position: Position): Moniker[] {
  const context = buildVbProjectContext(cached, cachedSettings(cached.source.uri));
  const symbol =
    getVbscriptDefinition(cached.parsed, position, context) ??
    getVbscriptTypeDefinition(cached.parsed, position, context);
  if (!symbol) {
    return [];
  }
  return [
    {
      scheme: "asp-lsp",
      identifier: [
        symbol.sourceUri,
        symbol.containerName,
        symbol.memberOf,
        symbol.scopeName,
        symbol.name,
        symbol.range.start.line,
        symbol.range.start.character,
      ]
        .filter((part) => part !== undefined && part !== "")
        .join("#"),
      unique: UniquenessLevel.project,
      kind: symbol.scopeName ? MonikerKind.local : MonikerKind.$export,
    },
  ];
}

function jsMonikersAt(cached: CachedDocument, position: Position): Moniker[] {
  const context = jsContextAt(cached, position);
  const quickInfo = context?.service.getQuickInfoAtPosition(context.fileName, context.offset);
  if (!context || !quickInfo) {
    return [];
  }
  const sourceRange = textSpanToSourceRange(context.virtual, quickInfo.textSpan);
  const name = sourceRange ? textInRange(cached.source, sourceRange).trim() : "";
  if (!name) {
    return [];
  }
  return [
    {
      scheme: "asp-lsp-js",
      identifier: [
        cached.source.uri,
        context.virtual.languageId,
        name,
        quickInfo.textSpan.start,
        quickInfo.textSpan.length,
      ].join("#"),
      unique: UniquenessLevel.project,
      kind:
        quickInfo.kind === "class" || quickInfo.kind === "function"
          ? MonikerKind.$export
          : MonikerKind.local,
    },
  ];
}

function inlineValues(cached: CachedDocument, range: Range): InlineValue[] {
  return [...vbInlineValues(cached, range), ...jsInlineValues(cached, range)];
}

function vbInlineValues(cached: CachedDocument, range: Range): InlineValue[] {
  const context = buildVbProjectContext(cached, cachedSettings(cached.source.uri));
  const seen = new Set<string>();
  return (context.symbols ?? [])
    .filter(
      (symbol) =>
        symbol.sourceUri === cached.source.uri &&
        isInlineValueSymbol(symbol.kind) &&
        rangesOverlap(symbol.range, range),
    )
    .flatMap((symbol) => {
      const key = `${symbol.name}:${symbol.range.start.line}:${symbol.range.start.character}`;
      if (seen.has(key)) {
        return [];
      }
      seen.add(key);
      return [InlineValueVariableLookup.create(symbol.range, symbol.name, false)];
    });
}

function jsInlineValues(cached: CachedDocument, range: Range): InlineValue[] {
  const sourceStart = cached.source.offsetAt(range.start);
  const sourceEnd = cached.source.offsetAt(range.end);
  const seen = new Set<string>();
  return jsVirtualDocuments(cached).flatMap((virtual) => {
    const project = createJsLanguageService(virtual, cachedSettings(cached.source.uri));
    const fileName = jsVirtualFileName(virtual.uri);
    const file = project.files.get(fileName);
    if (!file) {
      return [];
    }
    const sourceFile = ts.createSourceFile(fileName, file.text, ts.ScriptTarget.Latest, true);
    const values: InlineValue[] = [];
    const visit = (node: ts.Node): void => {
      if (ts.isIdentifier(node)) {
        const start = node.getStart(sourceFile);
        const end = node.getEnd();
        const segment = virtual.sourceMap.segments.find(
          (candidate) =>
            candidate.virtualStart <= start &&
            candidate.virtualStart + (candidate.sourceEnd - candidate.sourceStart) >= end &&
            candidate.sourceStart < sourceEnd &&
            candidate.sourceEnd > sourceStart,
        );
        const sourceRange = segment
          ? textSpanToSourceRange(virtual, { start, length: end - start })
          : undefined;
        const key = sourceRange
          ? `${node.text}:${sourceRange.start.line}:${sourceRange.start.character}`
          : undefined;
        if (sourceRange && rangesOverlap(sourceRange, range) && key && !seen.has(key)) {
          seen.add(key);
          values.push(InlineValueVariableLookup.create(sourceRange, node.text, true));
        }
      }
      ts.forEachChild(node, visit);
    };
    visit(sourceFile);
    return values;
  });
}

function isInlineValueSymbol(kind: VbSymbolKind): boolean {
  return ["variable", "parameter", "constant", "field", "property"].includes(kind);
}

function resolveJsCompletion(item: CompletionItem, uri: string): CompletionItem | undefined {
  const cached = getCached(uri);
  const data = item.data as
    | {
        name?: string;
        virtualOffset?: number;
        language?: string;
        source?: string;
        tsData?: ts.CompletionEntryData;
        importTextChanges?: ts.FileTextChanges[];
      }
    | undefined;
  const virtual = cached?.virtuals.get(data?.language === "jscript" ? "jscript" : "javascript");
  if (!cached || !virtual || typeof data?.virtualOffset !== "number" || !data.name) {
    return undefined;
  }
  const service = createJsLanguageService(virtual, cachedSettings(uri)).service;
  const preferences = jsCompletionPreferences(cachedSettings(uri));
  const details = safeGetCompletionEntryDetails(
    service,
    jsVirtualFileName(virtual.uri),
    data.virtualOffset,
    data.name,
    data.source,
    preferences,
    data.tsData,
  );
  const importEdit = fileTextChangesToWorkspaceEdit(
    virtual,
    details?.codeActions?.flatMap((action) => action.changes) ?? data.importTextChanges ?? [],
  );
  return {
    ...item,
    detail: details ? ts.displayPartsToString(details.displayParts) : item.detail,
    documentation: details ? ts.displayPartsToString(details.documentation) : item.documentation,
    additionalTextEdits: importEdit?.changes?.[cached.source.uri],
  };
}

function aspHover(cached: CachedDocument, params: TextDocumentPositionParams): Hover | null {
  const value = getVbscriptHover(
    cached.parsed,
    params.position,
    buildVbProjectContext(cached, cachedSettings(cached.source.uri)),
  );
  return value ? { contents: { kind: "markdown", value } } : null;
}

function withCompletionData(
  items: CompletionItem[],
  data: Record<string, unknown>,
): CompletionItem[] {
  return items.map((item) => ({
    ...item,
    data: { ...data, ...(item.data as object | undefined) },
  }));
}

function callHierarchyRootUri(item: CallHierarchyItem): string {
  const data = item.data as { rootUri?: string } | undefined;
  return data?.rootUri ?? item.uri;
}

function resolveEmbeddedCompletion(item: CompletionItem, kind: "html" | "css"): CompletionItem {
  const service = (kind === "html" ? htmlService : cssService) as {
    doResolve?: (completion: CompletionItem) => CompletionItem | Promise<CompletionItem>;
  };
  const resolved = service.doResolve?.(item);
  if (resolved && typeof (resolved as Promise<CompletionItem>).then !== "function") {
    return resolved as CompletionItem;
  }
  const localizer = createLocalizer((item.data as { locale?: AspLocale } | undefined)?.locale);
  return {
    ...item,
    detail:
      item.detail ??
      localizer.t(
        kind === "html" ? "server.completion.html.detail" : "server.completion.css.detail",
      ),
    documentation:
      item.documentation ??
      localizer.t(
        kind === "html"
          ? "server.completion.html.documentation"
          : "server.completion.css.documentation",
      ),
  };
}

function definitionLikeLocation(
  uri: string,
  position: Position,
  mode: JavaScriptMode,
): Location | Location[] | null {
  const cached = getCached(uri);
  if (!cached) {
    return null;
  }
  const region = findRegionAt(cached.parsed, cached.source.offsetAt(position));
  if (!region) {
    return null;
  }
  if (region.language === "vbscript") {
    const context = buildVbProjectContext(cached, cachedSettings(cached.source.uri));
    const symbol =
      mode === "typeDefinition"
        ? getVbscriptTypeDefinition(cached.parsed, position, context)
        : mode === "implementation"
          ? getVbscriptImplementation(cached.parsed, position, context)
          : getVbscriptDefinition(cached.parsed, position, context);
    return symbol ? Location.create(symbol.sourceUri, symbol.range) : null;
  }
  if (isJavaScriptLikeRegion(region)) {
    return jsLocations(cached, position, mode);
  }
  if (region.language === "css" && mode !== "implementation") {
    const virtual = cached.virtuals.get("css");
    const virtualPosition = virtual?.sourceMap.toVirtualPosition(position);
    if (!virtual || !virtualPosition) {
      return null;
    }
    const doc = toTextDocument(virtual);
    const location = cssService.findDefinition(
      doc,
      virtualPosition,
      cssService.parseStylesheet(doc),
    );
    return location ? (remapLocation(virtual, location) ?? null) : null;
  }
  return null;
}

function jsLocations(cached: CachedDocument, position: Position, mode: JavaScriptMode): Location[] {
  const context = jsContextAt(cached, position);
  if (!context) {
    return [];
  }
  const definitions =
    mode === "typeDefinition"
      ? context.service.getTypeDefinitionAtPosition(context.fileName, context.offset)
      : mode === "implementation"
        ? context.service.getImplementationAtPosition(context.fileName, context.offset)
        : context.service.getDefinitionAtPosition(context.fileName, context.offset);
  return (
    definitions
      ?.map((definition) => tsDefinitionToLocation(context, definition))
      .filter((location): location is Location => Boolean(location)) ?? []
  );
}

function tsDefinitionToLocation(
  context: JsProjectContext,
  definition: ts.DefinitionInfo | ts.ImplementationLocation,
): Location | undefined {
  return textSpanToLocation(context, definition.fileName, definition.textSpan);
}

function remapLocation(virtual: VirtualDocument, location: Location): Location | undefined {
  const start = virtual.sourceMap.toSourcePosition(location.range.start);
  const end = virtual.sourceMap.toSourcePosition(location.range.end);
  return start && end
    ? Location.create(virtual.uri.replace(`.${virtual.languageId}.virtual`, ""), { start, end })
    : undefined;
}

function virtualSourceUri(virtual: VirtualDocument): string {
  return virtual.uri.replace(`.${virtual.languageId}.virtual`, "");
}

function jsContextAt(cached: CachedDocument, position: Position): JsProjectContext | undefined {
  const region = findRegionAt(cached.parsed, cached.source.offsetAt(position));
  if (!region || !isJavaScriptLikeRegion(region)) {
    return undefined;
  }
  const virtual = cached.virtuals.get(region.language);
  const virtualPosition = virtual?.sourceMap.toVirtualPosition(position);
  if (!virtual || !virtualPosition) {
    return undefined;
  }
  const doc = toTextDocument(virtual);
  const fileName = jsVirtualFileName(virtual.uri);
  const project = createJsLanguageService(virtual, cachedSettings(cached.source.uri));
  return {
    virtual,
    service: project.service,
    fileName,
    offset: doc.offsetAt(virtualPosition),
    files: project.files,
  };
}

function jsVirtualDocuments(cached: CachedDocument): VirtualDocument[] {
  return ["javascript", "jscript"]
    .map((language) => cached.virtuals.get(language))
    .filter((virtual): virtual is VirtualDocument => Boolean(virtual));
}

function workspaceSymbolsForCached(cached: CachedDocument): SymbolInformation[] {
  return [
    ...includeSymbols(cached),
    ...htmlWorkspaceSymbols(cached),
    ...cssWorkspaceSymbols(cached),
    ...jsWorkspaceSymbols(cached),
  ];
}

function includeSymbols(cached: CachedDocument): SymbolInformation[] {
  return cached.parsed.includes.map((include) =>
    SymbolInformation.create(
      include.path,
      SymbolKind.File,
      include.range,
      cached.source.uri,
      "include",
    ),
  );
}

function htmlWorkspaceSymbols(cached: CachedDocument): SymbolInformation[] {
  const symbols: SymbolInformation[] = [];
  const html = cached.virtuals.get("html");
  if (!html) {
    return symbols;
  }
  const text = html.text;
  const pattern = /\b(?:id|name)=["']([^"']+)["']/gi;
  let match: RegExpExecArray | null;
  while ((match = pattern.exec(text))) {
    const start = match.index + match[0].indexOf(match[1]);
    const end = start + match[1].length;
    const range = textSpanToSourceRange(html, { start, length: end - start });
    if (range) {
      symbols.push(
        SymbolInformation.create(match[1], SymbolKind.Key, range, cached.source.uri, "html"),
      );
    }
  }
  return symbols;
}

function cssWorkspaceSymbols(cached: CachedDocument): SymbolInformation[] {
  return cssDocumentSymbols(cached).map((symbol) =>
    SymbolInformation.create(
      symbol.name,
      symbol.kind,
      symbol.selectionRange,
      cached.source.uri,
      "css",
    ),
  );
}

function jsWorkspaceSymbols(cached: CachedDocument): SymbolInformation[] {
  return jsDocumentSymbols(cached).map((symbol) =>
    SymbolInformation.create(
      symbol.name,
      symbol.kind,
      symbol.selectionRange,
      cached.source.uri,
      "javascript",
    ),
  );
}

function cssDocumentSymbols(cached: CachedDocument): DocumentSymbol[] {
  const virtual = cached.virtuals.get("css");
  if (!virtual) {
    return [];
  }
  const doc = toTextDocument(virtual);
  return cssService
    .findDocumentSymbols2(doc, cssService.parseStylesheet(doc))
    .map((symbol) => remapDocumentSymbol(virtual, symbol))
    .filter((symbol): symbol is DocumentSymbol => Boolean(symbol));
}

function jsDocumentSymbols(cached: CachedDocument): DocumentSymbol[] {
  return jsVirtualDocuments(cached).flatMap((virtual) => {
    const project = createJsLanguageService(virtual, cachedSettings(virtualSourceUri(virtual)));
    const tree = project.service.getNavigationTree(jsVirtualFileName(virtual.uri));
    return (tree.childItems ?? [])
      .map((item) => navigationTreeToDocumentSymbol(virtual, item))
      .filter((symbol): symbol is DocumentSymbol => Boolean(symbol));
  });
}

function navigationTreeToDocumentSymbol(
  activeVirtual: VirtualDocument,
  item: ts.NavigationTree,
): DocumentSymbol | undefined {
  const primarySpan = item.spans[0];
  if (!primarySpan) {
    return undefined;
  }
  const range = textSpanToSourceRange(activeVirtual, primarySpan);
  const selectionRange = textSpanToSourceRange(activeVirtual, item.nameSpan ?? primarySpan);
  if (!range || !selectionRange) {
    return undefined;
  }
  return {
    name: item.text,
    detail: item.kindModifiers,
    kind: tsSymbolKind(item.kind),
    range,
    selectionRange,
    children: (item.childItems ?? [])
      .map((child) => navigationTreeToDocumentSymbol(activeVirtual, child))
      .filter((symbol): symbol is DocumentSymbol => Boolean(symbol)),
  };
}

function remapDocumentSymbol(
  virtual: VirtualDocument,
  symbol: DocumentSymbol,
): DocumentSymbol | undefined {
  const range = sourceRangeFromVirtualRange(virtual, symbol.range);
  const selectionRange = sourceRangeFromVirtualRange(virtual, symbol.selectionRange);
  if (!range || !selectionRange) {
    return undefined;
  }
  return {
    ...symbol,
    range,
    selectionRange,
    children: symbol.children
      ?.map((child) => remapDocumentSymbol(virtual, child))
      .filter((child): child is DocumentSymbol => Boolean(child)),
  };
}

function cssFoldingRanges(cached: CachedDocument): FoldingRange[] {
  const virtual = cached.virtuals.get("css");
  if (!virtual) {
    return [];
  }
  return cssService
    .getFoldingRanges(toTextDocument(virtual), {})
    .map((range) => remapFoldingRange(virtual, range))
    .filter((range): range is FoldingRange => Boolean(range));
}

function jsFoldingRanges(cached: CachedDocument): FoldingRange[] {
  return jsVirtualDocuments(cached).flatMap((virtual) => {
    const project = createJsLanguageService(virtual, cachedSettings(virtualSourceUri(virtual)));
    return project.service
      .getOutliningSpans(jsVirtualFileName(virtual.uri))
      .map((span) => textSpanToSourceRange(virtual, span.textSpan))
      .filter((range): range is Range => Boolean(range))
      .map((range) => ({ startLine: range.start.line, endLine: range.end.line }));
  });
}

function vbscriptFoldingRanges(cached: CachedDocument): FoldingRange[] {
  const context = buildVbProjectContext(cached, cachedSettings(cached.source.uri));
  return (context.symbols ?? [])
    .filter((symbol) => symbol.sourceUri === cached.source.uri && symbol.scopeRange)
    .map((symbol) => symbol.scopeRange)
    .filter((range): range is Range => Boolean(range))
    .filter((range) => range.start.line < range.end.line)
    .map((range) => ({ startLine: range.start.line, endLine: range.end.line }));
}

function remapFoldingRange(
  virtual: VirtualDocument,
  range: FoldingRange,
): FoldingRange | undefined {
  const doc = toTextDocument(virtual);
  const start = virtual.sourceMap.toSourcePosition({
    line: range.startLine,
    character: range.startCharacter ?? 0,
  });
  const endOffset = doc.offsetAt({
    line: range.endLine,
    character: range.endCharacter ?? Number.MAX_SAFE_INTEGER,
  });
  const end = virtual.sourceMap.toSourcePosition(doc.positionAt(endOffset));
  return start && end ? { ...range, startLine: start.line, endLine: end.line } : undefined;
}

function textSpanToSourceRange(virtual: VirtualDocument, span: ts.TextSpan): Range | undefined {
  const doc = toTextDocument(virtual);
  const start = virtual.sourceMap.toSourcePosition(doc.positionAt(span.start));
  const end = virtual.sourceMap.toSourcePosition(doc.positionAt(span.start + span.length));
  return start && end ? { start, end } : undefined;
}

function textSpanToLocation(
  context: JsProjectContext,
  fileName: string,
  span: ts.TextSpan,
): Location | undefined {
  const file = context.files.get(normalizeFileName(fileName));
  if (!file) {
    return undefined;
  }
  if (file.virtual) {
    const range = textSpanToSourceRange(file.virtual, span);
    return range ? Location.create(virtualSourceUri(file.virtual), range) : undefined;
  }
  const doc = TextDocument.create(file.uri, "javascript", 0, file.text);
  return Location.create(file.uri, {
    start: doc.positionAt(span.start),
    end: doc.positionAt(span.start + span.length),
  });
}

function tsReferenceToLocation(
  context: JsProjectContext,
  reference: ts.ReferenceEntry,
): Location | undefined {
  return textSpanToLocation(context, reference.fileName, reference.textSpan);
}

function buildVbProjectContext(cached: CachedDocument, settings: AspSettings): VbProjectContext {
  const documents = collectVbProjectDocuments(cached.parsed, settings);
  const contextSettings = {
    typeChecking: settings.vbscript?.typeChecking,
    identifierCase: settings.vbscript?.identifierCase,
    identifierCaseByKind: settings.vbscript?.identifierCaseByKind,
    comTypes: settings.vbscript?.comTypes,
    unusedDiagnostics: settings.vbscript?.unusedDiagnostics !== false,
    locale: settings.resolvedLocale,
  };
  const key = vbProjectContextCacheKey(documents, settings);
  if (cached.analysis?.vbProjectContext?.key === key) {
    return cached.analysis.vbProjectContext.context;
  }
  const symbols = documents.flatMap((document) =>
    collectVbscriptSymbols(document, contextSettings),
  );
  symbols.push(...configuredVbscriptGlobals(cached, settings));
  const typeEnvironment = buildVbTypeEnvironment(cached.parsed, { ...contextSettings, symbols });
  const context = {
    documents,
    symbols,
    typeEnvironment,
    ...contextSettings,
  };
  analysisFor(cached).vbProjectContext = { key, context };
  return context;
}

function vbProjectContextCacheKey(documents: AspParsedDocument[], settings: AspSettings): string {
  return JSON.stringify({
    documents: documents.map((document) => ({
      uri: document.uri,
      vbscript: vbProjectDocumentFingerprint(document),
    })),
    settings: {
      typeChecking: settings.vbscript?.typeChecking,
      identifierCase: settings.vbscript?.identifierCase,
      identifierCaseByKind: settings.vbscript?.identifierCaseByKind,
      comTypes: settings.vbscript?.comTypes,
      unusedDiagnostics: settings.vbscript?.unusedDiagnostics !== false,
      locale: settings.resolvedLocale,
    },
    globals: settings.vbscript?.globals,
  });
}

function vbProjectDocumentFingerprint(document: AspParsedDocument): unknown {
  return {
    defaultLanguage: document.defaultLanguage,
    includes: document.includes.map((include) => ({
      offset: include.offset,
      path: include.path,
      mode: include.mode,
    })),
    regions: document.regions
      .filter((region) => region.language === "vbscript")
      .map((region) => ({
        kind: region.kind,
        start: region.start,
        end: region.end,
        contentStart: region.contentStart,
        contentEnd: region.contentEnd,
        text: textFingerprint(document.text.slice(region.contentStart, region.contentEnd)),
      })),
  };
}

function configuredVbscriptGlobals(cached: CachedDocument, settings: AspSettings): VbSymbol[] {
  const globals = settings.vbscript?.globals;
  if (!globals) {
    return [];
  }
  const range = {
    start: { line: 0, character: 0 },
    end: { line: 0, character: 0 },
  };
  return Object.entries(globals).flatMap(([name, value]) => {
    const typeName = typeof value === "string" ? value : value.type;
    if (!/^[A-Za-z][A-Za-z0-9_]*$/.test(name) || !typeName) {
      return [];
    }
    return [
      {
        name,
        kind: typeof value === "object" && value.kind === "constant" ? "constant" : "variable",
        range,
        sourceUri: `${cached.source.uri}#runtime-global`,
        typeName,
        type: { name: typeName, object: true },
      } satisfies VbSymbol,
    ];
  });
}

function collectVbProjectDocuments(
  root: AspParsedDocument,
  settings: AspSettings,
): AspParsedDocument[] {
  const documents: AspParsedDocument[] = [];
  const visited = new Set<string>();
  const visit = (document: AspParsedDocument, depth: number): void => {
    if (depth > 20 || visited.has(document.uri)) {
      return;
    }
    visited.add(document.uri);
    documents.push(document);
    for (const include of document.includes) {
      const resolved = resolveIncludePath(document.uri, include.path, include.mode, settings);
      if (!fs.existsSync(resolved)) {
        continue;
      }
      const uri = pathToFileUri(resolved);
      if (visited.has(uri)) {
        continue;
      }
      visit(readParsedIncludeDocument(resolved, settings), depth + 1);
    }
  };
  visit(root, 0);
  return documents;
}

function readParsedIncludeDocument(fileName: string, settings: AspSettings): AspParsedDocument {
  const normalized = normalizeFileName(fileName);
  const stats = fs.statSync(normalized);
  const cached = includeDocumentCache.get(normalized);
  if (cached && cached.mtimeMs === stats.mtimeMs) {
    return cached.parsed;
  }
  const text = readTextFile(normalized, settings.legacyEncoding);
  const parsed = parseAspDocument(pathToFileUri(normalized), text, settings);
  includeDocumentCache.set(normalized, { mtimeMs: stats.mtimeMs, parsed });
  return parsed;
}

function readTextFile(fileName: string, encoding: AspLegacyEncoding | undefined): string {
  return decodeLegacyText(fs.readFileSync(fileName), encoding);
}

function includeDiagnostics(cached: CachedDocument, settings: AspSettings): Diagnostic[] {
  const diagnostics: Diagnostic[] = [];
  const owner = uriToFileName(cached.source.uri);
  const localizer = localizerForSettings(settings);
  for (const include of cached.parsed.includes) {
    const resolved = resolveIncludePath(cached.source.uri, include.path, include.mode, settings);
    if (!resolved || !fs.existsSync(resolved)) {
      diagnostics.push({
        severity: DiagnosticSeverity.Warning,
        range: include.range,
        message: localizer.t("server.include.unresolved", { path: include.path }),
        code: "include.missing",
        source: "asp-lsp-include",
      });
    }
    if (sameFile(resolved, owner)) {
      diagnostics.push({
        severity: DiagnosticSeverity.Warning,
        range: include.range,
        message: localizer.t("server.include.currentDocument"),
        code: "include.currentDocument",
        source: "asp-lsp-include",
      });
      continue;
    }
    const cycle = findIncludeCycle(owner, resolved, settings);
    if (cycle) {
      diagnostics.push({
        severity: DiagnosticSeverity.Warning,
        range: include.range,
        message: localizer.t("server.include.cycle", {
          cycle: cycle.map((fileName) => path.basename(fileName)).join(" -> "),
        }),
        code: "include.cycle",
        source: "asp-lsp-include",
      });
    }
  }
  return diagnostics;
}

function includeRenameWorkspaceEdit(
  files: Array<{ oldUri: string; newUri: string }>,
): WorkspaceEdit | null {
  ensureWorkspaceIndex();
  const changes: NonNullable<WorkspaceEdit["changes"]> = {};
  const seenEdits = new Set<string>();
  const renamePairs = files.map((file) => ({
    oldFile: normalizeFileName(uriToFileName(file.oldUri)),
    newFile: normalizeFileName(uriToFileName(file.newUri)),
  }));
  const candidates = [
    ...documents.all().flatMap((document) => {
      const cached = getCached(document.uri);
      return cached ? [cached] : [];
    }),
    ...[...workspaceIndex.values()].map(cachedFromIndexed),
  ];
  for (const cached of candidates) {
    const settings = cachedSettings(cached.source.uri);
    for (const include of cached.parsed.includes) {
      const resolved = normalizeFileName(
        resolveIncludePath(cached.source.uri, include.path, include.mode, settings),
      );
      const pair = renamePairs.find((item) => sameFile(item.oldFile, resolved));
      if (!pair) {
        continue;
      }
      const key = `${cached.source.uri}:${include.range.start.line}:${include.range.start.character}`;
      if (seenEdits.has(key)) {
        continue;
      }
      seenEdits.add(key);
      const nextPath = includePathForRenamedTarget(
        cached.source.uri,
        include.mode,
        pair.newFile,
        settings,
      );
      changes[cached.source.uri] = [
        ...(changes[cached.source.uri] ?? []),
        {
          range: include.range,
          newText: `<!-- #include ${include.mode}="${nextPath}" -->`,
        },
      ];
    }
  }
  return Object.keys(changes).length > 0 ? { changes } : null;
}

function includePathForRenamedTarget(
  ownerUri: string,
  mode: "file" | "virtual",
  targetFile: string,
  settings: AspSettings,
): string {
  if (mode === "file") {
    return path
      .relative(path.dirname(uriToFileName(ownerUri)), targetFile)
      .split(path.sep)
      .join("/");
  }
  for (const root of [...(settings.virtualRoots ?? []), settings.virtualRoot, ...workspaceRoots]) {
    if (!root) {
      continue;
    }
    const relative = path.relative(root, targetFile);
    if (!relative.startsWith("..") && !path.isAbsolute(relative)) {
      return `/${relative.split(path.sep).join("/")}`;
    }
  }
  return `/${path.basename(targetFile)}`;
}

function findIncludeCycle(
  owner: string,
  start: string,
  settings: AspSettings,
): string[] | undefined {
  if (!fs.existsSync(start)) {
    return undefined;
  }
  const visited = new Set<string>();
  const stack: string[] = [];
  const stackIndexes = new Map<string, number>();
  const search = (fileName: string, depth: number): string[] | undefined => {
    if (depth > 20) {
      return undefined;
    }
    const normalized = normalizeFileName(fileName);
    if (sameFile(normalized, owner) && stack.length > 0) {
      return [...stack, owner];
    }
    const existingStackIndex = stackIndexes.get(normalized);
    if (existingStackIndex !== undefined) {
      return [...stack.slice(existingStackIndex), normalized];
    }
    if (visited.has(normalized)) {
      return undefined;
    }
    visited.add(normalized);
    stackIndexes.set(normalized, stack.length);
    stack.push(normalized);
    const parsed = readParsedIncludeDocument(normalized, settings);
    for (const include of parsed.includes) {
      const next = resolveIncludePath(
        pathToFileUri(normalized),
        include.path,
        include.mode,
        settings,
      );
      if (!fs.existsSync(next)) {
        continue;
      }
      const cycle = search(next, depth + 1);
      if (cycle) {
        return cycle;
      }
    }
    stack.pop();
    stackIndexes.delete(normalized);
    return undefined;
  };
  return search(start, 0);
}

function remapDiagnostic(
  virtual: VirtualDocument,
  diagnostic: Diagnostic,
  source: string,
): Diagnostic | undefined {
  const start = virtual.sourceMap.toSourcePosition(diagnostic.range.start);
  const end = virtual.sourceMap.toSourcePosition(diagnostic.range.end);
  if (!start || !end) {
    return undefined;
  }
  return { ...diagnostic, range: { start, end }, source };
}

function selectionRangeAt(cached: CachedDocument, position: Position): SelectionRange {
  const region = findRegionAt(cached.parsed, cached.source.offsetAt(position));
  if (region?.language === "html") {
    const virtual = cached.virtuals.get("html");
    if (virtual) {
      return remapSelectionRange(
        virtual,
        htmlService.getSelectionRanges(toTextDocument(virtual), [position])[0],
      );
    }
  }
  if (region?.language === "css") {
    const virtual = cached.virtuals.get("css");
    const virtualPosition = virtual?.sourceMap.toVirtualPosition(position);
    if (virtual && virtualPosition) {
      const doc = toTextDocument(virtual);
      return remapSelectionRange(
        virtual,
        cssService.getSelectionRanges(doc, [virtualPosition], cssService.parseStylesheet(doc))[0],
      );
    }
  }
  if (region?.language === "javascript") {
    const range = jsSelectionRange(cached, position);
    if (range) {
      return range;
    }
  }
  if (region?.language === "vbscript" || region?.language === "jscript") {
    return getVbscriptSelectionRanges(cached.parsed, [position])[0];
  }
  return { range: { start: position, end: position } };
}

function remapSelectionRange(virtual: VirtualDocument, range: SelectionRange): SelectionRange {
  const start = virtual.sourceMap.toSourcePosition(range.range.start) ?? range.range.start;
  const end = virtual.sourceMap.toSourcePosition(range.range.end) ?? range.range.end;
  return {
    range: { start, end },
    parent: range.parent ? remapSelectionRange(virtual, range.parent) : undefined,
  };
}

function jsSelectionRange(cached: CachedDocument, position: Position): SelectionRange | undefined {
  const context = jsContextAt(cached, position);
  if (!context) {
    return undefined;
  }
  const selection = context.service.getSmartSelectionRange(context.fileName, context.offset);
  return remapTsSelectionRange(context.virtual, selection);
}

function remapTsSelectionRange(virtual: VirtualDocument, range: ts.SelectionRange): SelectionRange {
  const doc = toTextDocument(virtual);
  const start = virtual.sourceMap.toSourcePosition(doc.positionAt(range.textSpan.start));
  const end = virtual.sourceMap.toSourcePosition(
    doc.positionAt(range.textSpan.start + range.textSpan.length),
  );
  return {
    range: {
      start: start ?? { line: 0, character: 0 },
      end: end ?? start ?? { line: 0, character: 0 },
    },
    parent: range.parent ? remapTsSelectionRange(virtual, range.parent) : undefined,
  };
}

function cssDocumentColors(cached: CachedDocument): ColorInformation[] {
  const virtual = cached.virtuals.get("css");
  if (!virtual) {
    return [];
  }
  const doc = toTextDocument(virtual);
  return cssService
    .findDocumentColors(doc, cssService.parseStylesheet(doc))
    .map((color) => {
      const range = sourceRangeFromVirtualRange(virtual, color.range);
      return range ? { ...color, range } : undefined;
    })
    .filter((color): color is ColorInformation => Boolean(color));
}

function cssColorPresentations(
  cached: CachedDocument,
  color: Color,
  range: Range,
): ColorPresentation[] {
  const virtual = cached.virtuals.get("css");
  if (!virtual) {
    return [];
  }
  const start = virtual.sourceMap.toVirtualPosition(range.start);
  const end = virtual.sourceMap.toVirtualPosition(range.end);
  if (!start || !end) {
    return [];
  }
  const doc = toTextDocument(virtual);
  return cssService
    .getColorPresentations(doc, cssService.parseStylesheet(doc), color, { start, end })
    .map((presentation) => ({
      ...presentation,
      textEdit: presentation.textEdit
        ? {
            ...presentation.textEdit,
            range: sourceRangeFromVirtualRange(virtual, presentation.textEdit.range) ?? range,
          }
        : undefined,
      additionalTextEdits: presentation.additionalTextEdits
        ?.map((edit) => {
          const editRange = sourceRangeFromVirtualRange(virtual, edit.range);
          return editRange ? { ...edit, range: editRange } : undefined;
        })
        .filter((edit): edit is TextEdit => Boolean(edit)),
    }));
}

function sourceRangeFromVirtualRange(virtual: VirtualDocument, range: Range): Range | undefined {
  const start = virtual.sourceMap.toSourcePosition(range.start);
  const end = virtual.sourceMap.toSourcePosition(range.end);
  return start && end ? { start, end } : undefined;
}

function remapWorkspaceEdit(
  virtual: VirtualDocument,
  edit: WorkspaceEdit,
  sourceUri: string,
): WorkspaceEdit {
  const changes: NonNullable<WorkspaceEdit["changes"]> = {};
  for (const [uri, textEdits] of Object.entries(edit.changes ?? {})) {
    const targetUri = uri === virtual.uri ? sourceUri : uri;
    changes[targetUri] = [
      ...(changes[targetUri] ?? []),
      ...textEdits
        .map((textEdit) => {
          const range =
            uri === virtual.uri
              ? sourceRangeFromVirtualRange(virtual, textEdit.range)
              : textEdit.range;
          return range ? { ...textEdit, range } : undefined;
        })
        .filter((textEdit): textEdit is TextEdit => Boolean(textEdit)),
    ];
  }
  return { changes };
}

function tsDiagnosticToLsp(
  virtual: VirtualDocument,
  diagnostic: ts.Diagnostic,
  override: { severity?: DiagnosticSeverity; source?: string } = {},
): Diagnostic | undefined {
  if (diagnostic.start === undefined || diagnostic.length === undefined) {
    return undefined;
  }
  const virtualDoc = toTextDocument(virtual);
  const start = virtualDoc.positionAt(diagnostic.start);
  const end = virtualDoc.positionAt(diagnostic.start + diagnostic.length);
  const sourceStart = virtual.sourceMap.toSourcePosition(start);
  const sourceEnd = virtual.sourceMap.toSourcePosition(end);
  if (
    !sourceStart ||
    !sourceEnd ||
    !virtualRangeStaysWithinSegment(virtual, diagnostic.start, diagnostic.start + diagnostic.length)
  ) {
    return undefined;
  }
  return {
    severity:
      override.severity ??
      (diagnostic.category === ts.DiagnosticCategory.Error
        ? DiagnosticSeverity.Error
        : DiagnosticSeverity.Warning),
    range: { start: sourceStart, end: sourceEnd },
    message: ts.flattenDiagnosticMessageText(diagnostic.messageText, "\n"),
    code: diagnostic.code,
    source: override.source ?? "asp-lsp-typescript",
  };
}

function virtualRangeStaysWithinSegment(
  virtual: VirtualDocument,
  start: number,
  end: number,
): boolean {
  const lastOffset = Math.max(start, end - 1);
  return virtual.sourceMap.segments.some(
    (segment) => start >= segment.virtualStart && lastOffset < segment.virtualEnd,
  );
}

function tsDiagnosticKey(diagnostic: ts.Diagnostic): string {
  return [diagnostic.code, diagnostic.start ?? -1, diagnostic.length ?? -1].join(":");
}

function toTextDocument(virtual: VirtualDocument): TextDocument {
  return TextDocument.create(virtual.uri, virtual.languageId, 0, virtual.text);
}

function getCached(uri: string): CachedDocument | undefined {
  const existing = cache.get(uri);
  if (existing) {
    return existing;
  }
  const document = documents.get(uri);
  if (!document) {
    return undefined;
  }
  const parsed = parseAspDocument(uri, document.getText(), cachedSettings(uri));
  const cached = { source: document, parsed, virtuals: buildVirtualDocuments(parsed) };
  cache.set(uri, cached);
  return cached;
}

function getIndexedCached(uri: string): CachedDocument | undefined {
  ensureWorkspaceIndex();
  const entry = workspaceIndex.get(normalizeFileName(uriToFileName(uri)));
  return entry ? cachedFromIndexed(entry) : undefined;
}

function cachedFromIndexed(entry: WorkspaceIndexedDocument): CachedDocument {
  return {
    source: TextDocument.create(entry.uri, "classic-asp", 0, entry.parsed.text),
    parsed: entry.parsed,
    virtuals: buildVirtualDocuments(entry.parsed),
  };
}

function ensureWorkspaceIndex(): void {
  if (!workspaceIndexDirty) {
    return;
  }
  for (const root of workspaceRoots) {
    indexWorkspaceRoot(root);
  }
  workspaceIndexDirty = false;
}

async function ensureWorkspaceIndexAsync(
  settings: AspSettings,
  token?: { isCancellationRequested?: boolean },
): Promise<void> {
  if (!workspaceIndexDirty) {
    return;
  }
  workspaceIndex.clear();
  workspaceIndexTruncated = false;
  let scannedFiles = 0;
  const maxFiles = settings.workspace?.maxIndexFiles ?? defaultMaxIndexFiles;
  const chunkSize = settings.workspace?.scanChunkSize ?? defaultScanChunkSize;
  for (const root of workspaceRoots) {
    if (token?.isCancellationRequested || scannedFiles >= maxFiles) {
      break;
    }
    scannedFiles = await indexWorkspaceRootAsync(root, settings, {
      scannedFiles,
      maxFiles,
      chunkSize,
      token,
    });
  }
  workspaceIndexTruncated = scannedFiles >= maxFiles;
  workspaceIndexDirty = Boolean(token?.isCancellationRequested);
  if (workspaceIndexTruncated) {
    connection.console.warn(
      createLocalizer(settings.resolvedLocale).t("server.workspaceIndex.truncated", { maxFiles }),
    );
  }
}

async function indexWorkspaceRootAsync(
  root: string,
  settings: AspSettings,
  state: {
    scannedFiles: number;
    maxFiles: number;
    chunkSize: number;
    token?: { isCancellationRequested?: boolean };
  },
): Promise<number> {
  const stat = await fs.promises.stat(root).catch(() => undefined);
  if (!stat?.isDirectory()) {
    return state.scannedFiles;
  }
  const directories = [root];
  let scannedFiles = state.scannedFiles;
  let operations = 0;
  while (directories.length > 0 && scannedFiles < state.maxFiles) {
    if (state.token?.isCancellationRequested) {
      return scannedFiles;
    }
    const directory = directories.pop() ?? root;
    const entries = await fs.promises.readdir(directory, { withFileTypes: true }).catch(() => []);
    for (const entry of entries) {
      if (state.token?.isCancellationRequested || scannedFiles >= state.maxFiles) {
        return scannedFiles;
      }
      const fullPath = path.join(directory, entry.name);
      if (entry.isDirectory()) {
        if (!isExcludedWorkspaceDirectory(entry.name, fullPath)) {
          directories.push(fullPath);
        }
      } else if (entry.isFile() && isAspWorkspaceFile(entry.name)) {
        await indexWorkspaceFileAsync(fullPath, settings);
        scannedFiles += 1;
      }
      operations += 1;
      if (operations % state.chunkSize === 0) {
        await yieldToEventLoop();
      }
    }
  }
  return scannedFiles;
}

async function indexWorkspaceFileAsync(fileName: string, settings: AspSettings): Promise<void> {
  const normalized = normalizeFileName(fileName);
  const stat = await fs.promises.stat(normalized).catch(() => undefined);
  if (!stat?.isFile()) {
    workspaceIndex.delete(normalized);
    return;
  }
  const existing = workspaceIndex.get(normalized);
  if (existing && existing.mtimeMs === stat.mtimeMs && existing.size === stat.size) {
    return;
  }
  const uri = pathToFileUri(normalized);
  const text = await readTextFileAsync(normalized, settings.legacyEncoding);
  const parsed = parseAspDocument(uri, text, cachedSettings(uri));
  workspaceIndex.set(normalized, {
    uri,
    fileName: normalized,
    mtimeMs: stat.mtimeMs,
    size: stat.size,
    parsed,
  });
}

async function readTextFileAsync(
  fileName: string,
  encoding: AspLegacyEncoding | undefined,
): Promise<string> {
  return decodeLegacyText(await fs.promises.readFile(fileName), encoding);
}

function decodeLegacyText(buffer: Uint8Array, encoding: AspLegacyEncoding | undefined): string {
  return new TextDecoder(textDecoderLabel(buffer, encoding)).decode(buffer);
}

function textDecoderLabel(buffer: Uint8Array, encoding: AspLegacyEncoding | undefined): string {
  switch (encoding ?? "auto") {
    case "utf8":
      return "utf-8";
    case "shift_jis":
    case "cp932":
      return "shift_jis";
    case "auto":
      return autoTextDecoderLabel(buffer);
  }
}

function autoTextDecoderLabel(buffer: Uint8Array): string {
  if (isValidUtf8(buffer)) {
    return "utf-8";
  }
  const detected = supportedTextDecoderLabel(detect(buffer));
  if (detected && detected !== "utf-8") {
    return detected;
  }
  return (
    analyse(buffer)
      .map((match) => supportedTextDecoderLabel(match.name))
      .find((label) => label && label !== "utf-8") ?? "utf-8"
  );
}

function supportedTextDecoderLabel(name: string | null): string | undefined {
  const normalized = name?.toLowerCase().replace(/[-_\s]/g, "");
  if (!normalized || normalized === "ascii" || normalized === "utf8") {
    return "utf-8";
  }
  if (
    normalized === "shiftjis" ||
    normalized === "sjis" ||
    normalized === "cp932" ||
    normalized === "windows31j" ||
    normalized === "windows932" ||
    normalized === "mskanji"
  ) {
    return "shift_jis";
  }
  if (normalized === "eucjp") {
    return "euc-jp";
  }
  if (normalized === "iso2022jp") {
    return "iso-2022-jp";
  }
  return undefined;
}

function isValidUtf8(buffer: Uint8Array): boolean {
  try {
    new TextDecoder("utf-8", { fatal: true }).decode(buffer);
    return true;
  } catch {
    return false;
  }
}

async function yieldToEventLoop(): Promise<void> {
  await new Promise<void>((resolve) => setImmediate(resolve));
}

function indexWorkspaceRoot(root: string): void {
  const stat = fs.statSync(root, { throwIfNoEntry: false });
  if (!stat?.isDirectory()) {
    return;
  }
  const visit = (dir: string): void => {
    for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
      const fullPath = path.join(dir, entry.name);
      if (entry.isDirectory()) {
        if (!isExcludedWorkspaceDirectory(entry.name, fullPath)) {
          visit(fullPath);
        }
        continue;
      }
      if (entry.isFile() && isAspWorkspaceFile(entry.name)) {
        indexWorkspaceFile(fullPath);
      }
    }
  };
  visit(root);
}

function indexWorkspaceFile(fileName: string): void {
  const normalized = normalizeFileName(fileName);
  const stat = fs.statSync(normalized, { throwIfNoEntry: false });
  if (!stat?.isFile()) {
    workspaceIndex.delete(normalized);
    return;
  }
  const existing = workspaceIndex.get(normalized);
  if (existing && existing.mtimeMs === stat.mtimeMs && existing.size === stat.size) {
    return;
  }
  const uri = pathToFileUri(normalized);
  const text = readTextFile(normalized, globalSettings.legacyEncoding);
  const parsed = parseAspDocument(uri, text, cachedSettings(uri));
  workspaceIndex.set(normalized, {
    uri,
    fileName: normalized,
    mtimeMs: stat.mtimeMs,
    size: stat.size,
    parsed,
  });
}

function invalidateWorkspaceIndex(): void {
  workspaceIndexDirty = true;
  workspaceIndexTruncated = false;
  workspaceIndex.clear();
}

function isExcludedWorkspaceDirectory(name: string, fullPath: string): boolean {
  const normalized = fullPath.split(path.sep).join("/");
  return (
    [".git", "node_modules", "dist", "out"].includes(name) ||
    normalized.endsWith("/server/language-server/node_modules")
  );
}

function isAspWorkspaceFile(fileName: string): boolean {
  return /\.(?:asp|asa|inc)$/i.test(fileName);
}

function isScriptWorkspaceFile(fileName: string): boolean {
  return /\.(?:[cm]?js|jsx|[cm]?ts|tsx|d\.ts)$/i.test(fileName);
}

function isJavaScriptProjectEnvironmentFile(fileName: string): boolean {
  const normalized = normalizeFileName(fileName).split(path.sep).join("/");
  return (
    /\/(?:tsconfig|jsconfig|package)\.json$/i.test(normalized) ||
    normalized.includes("/node_modules/@types/")
  );
}

function nearestPackageJson(directory: string): string | undefined {
  let current = normalizeFileName(directory);
  for (;;) {
    const candidate = path.join(current, "package.json");
    if (ts.sys.fileExists(candidate)) {
      return candidate;
    }
    const parent = path.dirname(current);
    if (parent === current) {
      return undefined;
    }
    current = parent;
  }
}

function findRegionAt(parsed: AspParsedDocument, offset: number) {
  return parsed.regions
    .filter((region) => offset >= region.contentStart && offset <= region.contentEnd)
    .sort(
      (left, right) =>
        left.contentEnd - left.contentStart - (right.contentEnd - right.contentStart),
    )[0];
}

function cachedSettings(uri: string): AspSettings {
  const existing = settingsByUri.get(uri);
  if (existing) {
    return existing;
  }
  const settings: AspSettings = {
    ...globalSettings,
    virtualRoot:
      globalSettings.virtualRoot || globalSettings.virtualRoots?.[0] || workspaceRootFromUri(uri),
    virtualRoots:
      globalSettings.virtualRoots && globalSettings.virtualRoots.length > 0
        ? globalSettings.virtualRoots
        : [workspaceRootFromUri(uri), ...workspaceRoots],
  };
  settingsByUri.set(uri, settings);
  return settings;
}

function localizerForSettings(settings: AspSettings) {
  return createLocalizer(settings.resolvedLocale);
}

function localizerForUri(uri: string) {
  return localizerForSettings(cachedSettings(uri));
}

async function refreshConfiguration(): Promise<void> {
  try {
    globalSettings = normalizeSettings(
      (await connection.workspace.getConfiguration("aspLsp")) as Record<string, unknown>,
    );
    settingsByUri.clear();
    for (const document of documents.all()) {
      await validate(document);
    }
  } catch {
    globalSettings = normalizeSettings(globalSettings);
  }
}

function readSettingsFromChange(settings: unknown): Record<string, unknown> | undefined {
  if (!settings || typeof settings !== "object") {
    return undefined;
  }
  const record = settings as Record<string, unknown>;
  const nested = record.aspLsp;
  return nested && typeof nested === "object" ? (nested as Record<string, unknown>) : record;
}

function normalizeSettings(settings: Record<string, unknown> | AspSettings): AspSettings {
  const rawVirtualRoots = Array.isArray(settings.virtualRoots)
    ? settings.virtualRoots
    : Array.isArray(settings.includePaths)
      ? settings.includePaths
      : undefined;
  const virtualRoots = rawVirtualRoots
    ?.filter((value): value is string => typeof value === "string" && value.length > 0)
    .map((value) => path.resolve(value));
  const includePaths = Array.isArray(settings.includePaths)
    ? settings.includePaths
        .filter((value): value is string => typeof value === "string" && value.length > 0)
        .map((value) => path.resolve(value))
    : undefined;
  return {
    locale: normalizeLocaleSetting(settings.locale),
    resolvedLocale: resolveLocale(normalizeLocaleSetting(settings.locale)),
    defaultLanguage: settings.defaultLanguage === "JScript" ? "JScript" : "VBScript",
    checkJs: settings.checkJs === true,
    virtualRoot:
      typeof settings.virtualRoot === "string" && settings.virtualRoot.length > 0
        ? path.resolve(settings.virtualRoot)
        : undefined,
    virtualRoots,
    includePaths,
    legacyEncoding: normalizeLegacyEncoding(settings.legacyEncoding),
    diagnostics: normalizeDiagnosticsSettings(settings),
    debug: normalizeDebugSettings(settings),
    format: normalizeFormatSettings(settings),
    javascript: normalizeJavascriptSettings(settings),
    vbscript: normalizeVbscriptSettings(settings),
    inlayHints: normalizeInlayHintSettings(settings),
    codeLens: normalizeCodeLensSettings(settings),
    workspace: normalizeWorkspaceSettings(settings),
  };
}

function normalizeLocaleSetting(value: unknown): AspLocaleSetting {
  return value === "en" || value === "ja" || value === "auto" ? value : "auto";
}

function resolveLocale(setting: AspLocaleSetting): AspLocale {
  if (setting === "en" || setting === "ja") {
    return setting;
  }
  return clientLocale.toLowerCase().startsWith("ja") ? "ja" : "en";
}

function normalizeWorkspaceSettings(
  settings: Record<string, unknown> | AspSettings,
): AspSettings["workspace"] {
  const raw = settings.workspace;
  const record = raw && typeof raw === "object" ? (raw as Record<string, unknown>) : {};
  return {
    maxIndexFiles:
      typeof record.maxIndexFiles === "number" && record.maxIndexFiles > 0
        ? Math.floor(record.maxIndexFiles)
        : defaultMaxIndexFiles,
    scanChunkSize:
      typeof record.scanChunkSize === "number" && record.scanChunkSize > 0
        ? Math.floor(record.scanChunkSize)
        : defaultScanChunkSize,
  };
}

function normalizeLegacyEncoding(value: unknown): AspLegacyEncoding {
  return value === "auto" || value === "utf8" || value === "shift_jis" || value === "cp932"
    ? value
    : "auto";
}

function normalizeDiagnosticsSettings(
  settings: Record<string, unknown> | AspSettings,
): AspSettings["diagnostics"] {
  const raw = settings.diagnostics;
  const record = raw && typeof raw === "object" ? (raw as Record<string, unknown>) : {};
  return {
    debounceMs:
      typeof record.debounceMs === "number" && record.debounceMs >= 0
        ? Math.floor(record.debounceMs)
        : defaultDiagnosticsDebounceMs,
  };
}

function normalizeDebugSettings(
  settings: Record<string, unknown> | AspSettings,
): AspSettings["debug"] {
  const raw = settings.debug;
  const record = raw && typeof raw === "object" ? (raw as Record<string, unknown>) : {};
  return {
    output: normalizeDebugOutputLevel(record.output),
  };
}

function normalizeDebugOutputLevel(value: unknown): NonNullable<AspSettings["debug"]>["output"] {
  return value === "summary" || value === "verbose" ? value : "off";
}

function normalizeInlayHintSettings(
  settings: Record<string, unknown> | AspSettings,
): AspSettings["inlayHints"] {
  const raw = settings.inlayHints;
  const record = raw && typeof raw === "object" ? (raw as Record<string, unknown>) : {};
  return {
    variableTypes: record.variableTypes !== false,
    parameterNames: record.parameterNames !== false,
    functionReturnTypes: record.functionReturnTypes !== false,
    implicitByRef: record.implicitByRef !== false,
  };
}

function normalizeCodeLensSettings(
  settings: Record<string, unknown> | AspSettings,
): AspSettings["codeLens"] {
  const raw = settings.codeLens;
  const record = raw && typeof raw === "object" ? (raw as Record<string, unknown>) : {};
  return {
    references: record.references !== false,
    includes: record.includes !== false,
  };
}

function normalizeVbscriptSettings(
  settings: Record<string, unknown> | AspSettings,
): AspSettings["vbscript"] {
  const raw = settings.vbscript;
  const record = raw && typeof raw === "object" ? (raw as Record<string, unknown>) : {};
  return {
    typeChecking: record.typeChecking === "strict" ? "strict" : "basic",
    identifierCase: normalizeVbscriptIdentifierCase(record.identifierCase),
    identifierCaseByKind: normalizeVbscriptIdentifierCaseByKind(record.identifierCaseByKind),
    comTypes:
      record.comTypes && typeof record.comTypes === "object"
        ? (record.comTypes as NonNullable<AspSettings["vbscript"]>["comTypes"])
        : undefined,
    globals:
      record.globals && typeof record.globals === "object"
        ? (record.globals as NonNullable<AspSettings["vbscript"]>["globals"])
        : undefined,
    unusedDiagnostics: record.unusedDiagnostics !== false,
    includeSuggestions: record.includeSuggestions !== false,
  };
}

function normalizeVbscriptIdentifierCase(
  value: unknown,
): NonNullable<NonNullable<AspSettings["vbscript"]>["identifierCase"]> | undefined {
  switch (value) {
    case "PascalCase":
    case "UPPERCASE":
    case "camelCase":
    case "lowercase":
    case "snake_case":
    case "UPPER_SNAKE":
    case "ignore":
      return value;
    case "pascal":
      return "PascalCase";
    case "upper":
      return "UPPERCASE";
    case "camel":
      return "camelCase";
    case "lower":
      return "lowercase";
    case "snake":
      return "snake_case";
    case "upperSnake":
      return "UPPER_SNAKE";
    default:
      return undefined;
  }
}

function normalizeVbscriptIdentifierCaseByKind(
  value: unknown,
): NonNullable<NonNullable<AspSettings["vbscript"]>["identifierCaseByKind"]> | undefined {
  if (!value || typeof value !== "object") {
    return undefined;
  }
  const record = value as Record<string, unknown>;
  const result: NonNullable<NonNullable<AspSettings["vbscript"]>["identifierCaseByKind"]> = {};
  for (const key of [
    "variable",
    "parameter",
    "class",
    "function",
    "sub",
    "constant",
    "field",
    "property",
    "method",
  ] as const) {
    const normalized = normalizeVbscriptIdentifierCase(record[key]);
    if (normalized) {
      result[key] = normalized;
    }
  }
  return Object.keys(result).length > 0 ? result : undefined;
}

function normalizeJavascriptSettings(
  settings: Record<string, unknown> | AspSettings,
): AspSettings["javascript"] {
  const raw = settings.javascript;
  const record = raw && typeof raw === "object" ? (raw as Record<string, unknown>) : {};
  return {
    unusedDiagnostics: record.unusedDiagnostics !== false,
    autoImports: record.autoImports !== false,
    ignoreProjectConfig: record.ignoreProjectConfig === true,
  };
}

function normalizeFormatSettings(
  settings: Record<string, unknown> | AspSettings,
): AspSettings["format"] {
  const raw = settings.format;
  const record = raw && typeof raw === "object" ? (raw as Record<string, unknown>) : {};
  const indentStyle =
    record.indentStyle === "tab" || record.indentStyle === "space" ? record.indentStyle : undefined;
  return {
    indentSize:
      typeof record.indentSize === "number" && record.indentSize > 0
        ? record.indentSize
        : undefined,
    indentStyle,
    uppercaseKeywords: record.uppercaseKeywords === true,
    alignAssignments: record.alignAssignments === true,
    onSave: record.onSave === true,
  };
}

function formatOptions(options: { tabSize: number; insertSpaces: boolean }, settings: AspSettings) {
  return {
    tabSize: options.tabSize,
    insertSpaces: options.insertSpaces,
    ...settings.format,
  };
}

function defaultFormattingOptions(settings: AspSettings): {
  tabSize: number;
  insertSpaces: boolean;
} {
  return {
    tabSize: settings.format?.indentSize ?? 2,
    insertSpaces: settings.format?.indentStyle !== "tab",
  };
}

function formatAspDocumentWithDelegates(
  cached: CachedDocument,
  options: { tabSize: number; insertSpaces: boolean },
): TextEdit[] {
  const settings = cachedSettings(cached.source.uri);
  const startedAt = startFormattingLog(cached, settings, "document");
  const formattingOptions = measureDebugStep(settings, cached.source.uri, "format.options", () =>
    formatOptions(options, settings),
  );
  const original = cached.source.getText();
  let formatted = measureDebugStep(settings, cached.source.uri, "format.core", () =>
    applyTextEdits(original, formatAspDocument(cached.parsed, formattingOptions)),
  );
  const parsed = measureDebugStep(settings, cached.source.uri, "format.reparse", () =>
    parseAspDocument(cached.source.uri, formatted, settings),
  );
  formatted = applyOffsetEdits(
    formatted,
    embeddedFormattingEdits(parsed, formatted, formattingOptions, settings, cached.source.uri),
  );
  const edits = measureDebugStep(settings, cached.source.uri, "format.editAssembly", () =>
    formatted === original
      ? []
      : [
          {
            range: {
              start: cached.source.positionAt(0),
              end: cached.source.positionAt(original.length),
            },
            newText: formatted,
          },
        ],
  );
  finishFormattingLog(cached, settings, "document", startedAt, edits.length);
  return edits;
}

function formatAspRangeWithDelegates(
  cached: CachedDocument,
  range: Range,
  options: { tabSize: number; insertSpaces: boolean },
): TextEdit[] {
  const settings = cachedSettings(cached.source.uri);
  const startedAt = startFormattingLog(cached, settings, "range");
  const formattingOptions = measureDebugStep(settings, cached.source.uri, "format.options", () =>
    formatOptions(options, settings),
  );
  const original = cached.source.getText();
  const rangeStart = lineStartOffset(original, cached.source.offsetAt(range.start));
  const rangeEnd = lineEndOffset(original, cached.source.offsetAt(range.end));
  const coreEdits = measureDebugStep(settings, cached.source.uri, "format.core", () =>
    formatAspRange(cached.parsed, range, formattingOptions),
  );
  let formatted = measureDebugStep(settings, cached.source.uri, "format.core.apply", () =>
    applyTextEdits(original, coreEdits),
  );
  let formattedRangeEnd =
    coreEdits.length === 1
      ? rangeStart + coreEdits[0].newText.length
      : rangeEnd + offsetEditsDelta(original, coreEdits);
  const parsed = measureDebugStep(settings, cached.source.uri, "format.reparse", () =>
    parseAspDocument(cached.source.uri, formatted, settings),
  );
  const embeddedEdits = embeddedFormattingEdits(
    parsed,
    formatted,
    formattingOptions,
    settings,
    cached.source.uri,
    rangeStart,
    formattedRangeEnd,
  );
  formattedRangeEnd += offsetEditsDelta(formatted, embeddedEdits);
  formatted = applyOffsetEdits(formatted, embeddedEdits);
  const newText = formatted.slice(rangeStart, formattedRangeEnd);
  const originalText = original.slice(rangeStart, rangeEnd);
  const edits = measureDebugStep(settings, cached.source.uri, "format.editAssembly", () =>
    newText === originalText
      ? []
      : [
          {
            range: {
              start: cached.source.positionAt(rangeStart),
              end: cached.source.positionAt(rangeEnd),
            },
            newText,
          },
        ],
  );
  finishFormattingLog(cached, settings, "range", startedAt, edits.length);
  return edits;
}

function startFormattingLog(
  cached: CachedDocument,
  settings: AspSettings,
  scope: "document" | "range",
): bigint {
  logDebugSummary(
    settings,
    `[asp-lsp] Formatting conversion started (${scope}): ${cached.source.uri}`,
  );
  return process.hrtime.bigint();
}

function finishFormattingLog(
  cached: CachedDocument,
  settings: AspSettings,
  scope: "document" | "range",
  startedAt: bigint,
  editCount: number,
): void {
  const elapsedMs = Number(process.hrtime.bigint() - startedAt) / 1_000_000;
  logDebugSummary(
    settings,
    `[asp-lsp] Formatting conversion completed (${scope}): ${cached.source.uri} ${formatElapsedMs(elapsedMs)}, edits=${editCount}`,
  );
}

function embeddedFormattingEdits(
  parsed: AspParsedDocument,
  text: string,
  options: AspFormattingOptions,
  settings: AspSettings,
  uri: string,
  spanStart = 0,
  spanEnd = text.length,
): OffsetEdit[] {
  const startedAt = process.hrtime.bigint();
  const edits = [
    ...measureDebugStep(settings, uri, "format.embedded.css", () =>
      cssFormattingEdits(parsed, text, options, spanStart, spanEnd),
    ),
    ...measureDebugStep(settings, uri, "format.embedded.javascript", () =>
      javaScriptFormattingEdits(parsed, text, options, spanStart, spanEnd),
    ),
  ];
  finishDebugStep(settings, uri, "format.embedded", startedAt);
  return edits;
}

function cssFormattingEdits(
  parsed: AspParsedDocument,
  text: string,
  options: AspFormattingOptions,
  spanStart: number,
  spanEnd: number,
): OffsetEdit[] {
  if (
    !parsed.regions.some(
      (region) =>
        region.language === "css" && region.contentEnd > spanStart && region.contentStart < spanEnd,
    )
  ) {
    return [];
  }
  return parsed.regions
    .filter(
      (region) =>
        region.language === "css" && region.contentEnd > spanStart && region.contentStart < spanEnd,
    )
    .filter((region) => !regionHasNestedAsp(parsed, region))
    .flatMap((region) => formatCssRegion(text, region, options, spanStart, spanEnd));
}

function formatCssRegion(
  text: string,
  region: AspRegion,
  options: AspFormattingOptions,
  spanStart: number,
  spanEnd: number,
): OffsetEdit[] {
  const content = text.slice(region.contentStart, region.contentEnd);
  const doc = TextDocument.create("__asp_lsp_format.css", "css", 0, content);
  const localStart = Math.max(0, spanStart - region.contentStart);
  const localEnd = Math.min(content.length, spanEnd - region.contentStart);
  if (localStart >= localEnd) {
    return [];
  }
  return cssService
    .format(
      doc,
      { start: doc.positionAt(localStart), end: doc.positionAt(localEnd) },
      {
        tabSize: options.indentSize ?? options.tabSize,
        insertSpaces: (options.indentStyle ?? (options.insertSpaces ? "space" : "tab")) !== "tab",
      },
    )
    .map((edit) => ({
      start: region.contentStart + offsetAtText(content, edit.range.start),
      end: region.contentStart + offsetAtText(content, edit.range.end),
      newText: edit.newText,
    }));
}

function javaScriptFormattingEdits(
  parsed: AspParsedDocument,
  text: string,
  options: AspFormattingOptions,
  spanStart = 0,
  spanEnd = text.length,
): OffsetEdit[] {
  return parsed.regions
    .filter(
      (region) =>
        isJavaScriptLikeRegion(region) &&
        region.contentEnd > spanStart &&
        region.contentStart < spanEnd,
    )
    .filter((region) => !regionHasNestedAsp(parsed, region))
    .flatMap((region) => formatJavaScriptRegion(text, region, options, spanStart, spanEnd));
}

function regionHasNestedAsp(parsed: AspParsedDocument, owner: AspRegion): boolean {
  return parsed.regions.some(
    (region) =>
      region !== owner &&
      (region.kind === "asp-block" ||
        region.kind === "asp-expression" ||
        region.kind === "asp-directive") &&
      region.start >= owner.contentStart &&
      region.end <= owner.contentEnd,
  );
}

function formatJavaScriptRegion(
  text: string,
  region: AspRegion,
  options: AspFormattingOptions,
  spanStart: number,
  spanEnd: number,
): OffsetEdit[] {
  const content = text.slice(region.contentStart, region.contentEnd);
  const localStart = Math.max(0, spanStart - region.contentStart);
  const localEnd = Math.min(content.length, spanEnd - region.contentStart);
  if (localStart >= localEnd) {
    return [];
  }
  const changes =
    localStart === 0 && localEnd === content.length
      ? getJavaScriptFormattingService(content).getFormattingEditsForDocument(
          "__asp_lsp_format.js",
          tsFormatOptions(options),
        )
      : getJavaScriptFormattingService(content).getFormattingEditsForRange(
          "__asp_lsp_format.js",
          localStart,
          localEnd,
          tsFormatOptions(options),
        );
  return changes.map((change) => ({
    start: region.contentStart + change.span.start,
    end: region.contentStart + change.span.start + change.span.length,
    newText: change.newText,
  }));
}

function getJavaScriptFormattingService(text: string): ts.LanguageService {
  const fileName = "__asp_lsp_format.js";
  return ts.createLanguageService({
    getScriptFileNames: () => [fileName],
    getScriptVersion: () => "0",
    getScriptSnapshot: (requested) =>
      requested === fileName ? ts.ScriptSnapshot.fromString(text) : undefined,
    getCurrentDirectory: () => process.cwd(),
    getCompilationSettings: () => ({
      allowJs: true,
      checkJs: false,
      target: ts.ScriptTarget.ESNext,
      module: ts.ModuleKind.CommonJS,
    }),
    getDefaultLibFileName: (compilerOptions) => ts.getDefaultLibFilePath(compilerOptions),
    fileExists: ts.sys.fileExists,
    readFile: ts.sys.readFile,
    readDirectory: ts.sys.readDirectory,
  });
}

function tsFormatOptions(options: AspFormattingOptions): ts.FormatCodeSettings {
  const indentStyle = options.indentStyle ?? (options.insertSpaces ? "space" : "tab");
  return {
    indentSize: options.indentSize ?? options.tabSize,
    tabSize: options.tabSize,
    convertTabsToSpaces: indentStyle !== "tab",
    newLineCharacter: "\n",
  };
}

function isJavaScriptLikeRegion(region: AspRegion): boolean {
  return region.language === "javascript" || region.language === "jscript";
}

function applyTextEdits(text: string, edits: TextEdit[]): string {
  return applyOffsetEdits(
    text,
    edits.map((edit) => ({
      start: offsetAtText(text, edit.range.start),
      end: offsetAtText(text, edit.range.end),
      newText: edit.newText,
    })),
  );
}

function applyOffsetEdits(text: string, edits: OffsetEdit[]): string {
  return [...edits]
    .sort((left, right) => right.start - left.start || right.end - left.end)
    .reduce(
      (current, edit) => `${current.slice(0, edit.start)}${edit.newText}${current.slice(edit.end)}`,
      text,
    );
}

function offsetEditsDelta(text: string, edits: TextEdit[] | OffsetEdit[]): number {
  return edits.reduce((delta, edit) => {
    if ("range" in edit) {
      return (
        delta +
        edit.newText.length -
        (offsetAtText(text, edit.range.end) - offsetAtText(text, edit.range.start))
      );
    }
    return delta + edit.newText.length - (edit.end - edit.start);
  }, 0);
}

function offsetAtText(text: string, position: Range["start"]): number {
  let line = 0;
  let character = 0;
  for (let index = 0; index < text.length; index += 1) {
    if (line === position.line && character === position.character) {
      return index;
    }
    if (text[index] === "\n") {
      line += 1;
      character = 0;
    } else {
      character += 1;
    }
  }
  return text.length;
}

function lineStartOffset(text: string, offset: number): number {
  return text.lastIndexOf("\n", Math.max(0, offset - 1)) + 1;
}

function lineEndOffset(text: string, offset: number): number {
  const end = text.indexOf("\n", offset);
  return end === -1 ? text.length : end;
}

function rangeOverlapsNonHtml(cached: CachedDocument, range: Range): boolean {
  const start = cached.source.offsetAt(range.start);
  const end = cached.source.offsetAt(range.end);
  return cached.parsed.regions.some(
    (region) => region.kind !== "html" && region.start < end && region.end > start,
  );
}

function createJsLanguageService(
  virtual: VirtualDocument,
  settings: AspSettings,
  optionOverrides: Partial<ts.CompilerOptions> = {},
): JsLanguageServiceProject {
  const cacheKey = jsLanguageServiceCacheKey(virtual, settings, optionOverrides);
  const cached = jsLanguageServiceCache.get(cacheKey);
  if (cached) {
    cached.lastUsed = ++jsLanguageServiceCacheTick;
    return cached.project;
  }
  const project = collectJsProjectFiles(virtual, settings, optionOverrides);
  const host: ts.LanguageServiceHost = {
    getScriptFileNames: () => [...project.files.keys()],
    getScriptVersion: (requested) =>
      project.files.get(normalizeFileName(requested))?.version ?? "0",
    getScriptSnapshot: (requested) => {
      const file = project.files.get(normalizeFileName(requested));
      if (file) {
        return ts.ScriptSnapshot.fromString(file.text);
      }
      const text = ts.sys.readFile(requested);
      return text === undefined ? undefined : ts.ScriptSnapshot.fromString(text);
    },
    getScriptKind: (requested) => scriptKindForFileName(requested),
    getCurrentDirectory: () => project.currentDirectory,
    getCompilationSettings: () => project.options,
    getDefaultLibFileName: (options) => ts.getDefaultLibFilePath(options),
    fileExists: (requested) =>
      project.files.has(normalizeFileName(requested)) || ts.sys.fileExists(requested),
    readFile: (requested) =>
      project.files.get(normalizeFileName(requested))?.text ?? ts.sys.readFile(requested),
    readDirectory: ts.sys.readDirectory,
    directoryExists: ts.sys.directoryExists,
    getDirectories: ts.sys.getDirectories,
  };
  const result = { service: ts.createLanguageService(host), host, files: project.files };
  jsLanguageServiceCache.set(cacheKey, {
    project: result,
    lastUsed: ++jsLanguageServiceCacheTick,
  });
  pruneJsLanguageServiceCache();
  return result;
}

function jsLanguageServiceCacheKey(
  virtual: VirtualDocument,
  settings: AspSettings,
  optionOverrides: Partial<ts.CompilerOptions>,
): string {
  return JSON.stringify({
    uri: virtual.uri,
    language: virtual.languageId,
    text: textFingerprint(virtual.text),
    settings: {
      checkJs: settings.checkJs ?? false,
      autoImports: settings.javascript?.autoImports !== false,
      unusedDiagnostics: settings.javascript?.unusedDiagnostics !== false,
      ignoreProjectConfig: settings.javascript?.ignoreProjectConfig === true,
    },
    optionOverrides,
    projectEnvironment: jsProjectEnvironmentFingerprint(virtualSourceUri(virtual), settings),
    roots: workspaceRoots.map(normalizeFileName).sort(),
    documents: documents
      .all()
      .map((document) => `${document.uri}:${document.version}`)
      .sort(),
  });
}

function jsProjectEnvironmentFingerprint(ownerUri: string, settings: AspSettings): string {
  const ownerFile = uriToFileName(ownerUri);
  const ownerDirectory = path.dirname(ownerFile);
  const configPath =
    settings.javascript?.ignoreProjectConfig === true
      ? undefined
      : (ts.findConfigFile(ownerDirectory, ts.sys.fileExists, "tsconfig.json") ??
        ts.findConfigFile(ownerDirectory, ts.sys.fileExists, "jsconfig.json"));
  return [configPath, nearestPackageJson(ownerDirectory)]
    .filter((fileName): fileName is string => Boolean(fileName))
    .map((fileName) => {
      const stat = fs.statSync(fileName, { throwIfNoEntry: false });
      return stat ? `${normalizeFileName(fileName)}:${stat.mtimeMs}:${stat.size}` : fileName;
    })
    .join("|");
}

function pruneJsLanguageServiceCache(): void {
  while (jsLanguageServiceCache.size > 16) {
    const oldest = [...jsLanguageServiceCache.entries()].sort(
      (left, right) => left[1].lastUsed - right[1].lastUsed,
    )[0];
    if (!oldest) {
      return;
    }
    oldest[1].project.service.dispose();
    jsLanguageServiceCache.delete(oldest[0]);
  }
}

function clearJsLanguageServiceCache(): void {
  for (const entry of jsLanguageServiceCache.values()) {
    entry.project.service.dispose();
  }
  jsLanguageServiceCache.clear();
}

function clearJsProjectCaches(): void {
  clearJsLanguageServiceCache();
  workspaceScriptFilesCache.clear();
}

function textFingerprint(text: string): string {
  let hash = 2166136261;
  for (let index = 0; index < text.length; index += 1) {
    hash ^= text.charCodeAt(index);
    hash = Math.imul(hash, 16777619);
  }
  return `${text.length}:${hash >>> 0}`;
}

function collectJsProjectFiles(
  activeVirtual: VirtualDocument,
  settings: AspSettings,
  optionOverrides: Partial<ts.CompilerOptions> = {},
): { files: Map<string, JsProjectFile>; options: ts.CompilerOptions; currentDirectory: string } {
  const files = new Map<string, JsProjectFile>();
  const addVirtual = (virtual: VirtualDocument, version: string): void => {
    const fileName = normalizeFileName(jsVirtualFileName(virtual.uri));
    files.set(fileName, {
      fileName,
      text: virtual.text,
      version,
      uri: virtualSourceUri(virtual),
      virtual,
    });
  };
  addVirtual(activeVirtual, "0");
  for (const document of documents.all()) {
    const cached = getCached(document.uri);
    if (!cached) {
      continue;
    }
    for (const virtual of jsVirtualDocuments(cached)) {
      addVirtual(virtual, String(document.version));
    }
  }

  const ownerFile = uriToFileName(virtualSourceUri(activeVirtual));
  const config = readJsProjectConfig(ownerFile, settings, optionOverrides);
  for (const fileName of config.fileNames) {
    const normalized = normalizeFileName(fileName);
    if (files.has(normalized) || !ts.sys.fileExists(normalized)) {
      continue;
    }
    const text = ts.sys.readFile(normalized);
    if (text === undefined) {
      continue;
    }
    const stat = fs.statSync(normalized, { throwIfNoEntry: false });
    files.set(normalized, {
      fileName: normalized,
      text,
      version: stat ? `${stat.mtimeMs}:${stat.size}` : "0",
      uri: pathToFileUri(normalized),
    });
  }
  return { files, options: config.options, currentDirectory: config.currentDirectory };
}

function readJsProjectConfig(
  ownerFile: string,
  settings: AspSettings,
  optionOverrides: Partial<ts.CompilerOptions> = {},
): { fileNames: string[]; options: ts.CompilerOptions; currentDirectory: string } {
  const ownerDirectory = path.dirname(ownerFile);
  const configPath =
    settings.javascript?.ignoreProjectConfig === true
      ? undefined
      : (ts.findConfigFile(ownerDirectory, ts.sys.fileExists, "tsconfig.json") ??
        ts.findConfigFile(ownerDirectory, ts.sys.fileExists, "jsconfig.json"));
  const defaultOptions: ts.CompilerOptions = {
    allowJs: true,
    checkJs: settings.checkJs ?? false,
    noEmit: true,
    target: ts.ScriptTarget.ESNext,
    module: ts.ModuleKind.CommonJS,
    moduleResolution: ts.ModuleResolutionKind.Node10,
    lib: browserJavaScriptLibs,
  };
  if (configPath) {
    const config = ts.readConfigFile(configPath, ts.sys.readFile);
    const parsed = ts.parseJsonConfigFileContent(
      config.config ?? {},
      ts.sys,
      path.dirname(configPath),
      defaultOptions,
      configPath,
    );
    const currentDirectory = path.dirname(configPath);
    return {
      fileNames: parsed.fileNames,
      options: browserJavaScriptCompilerOptions(
        parsed.options,
        currentDirectory,
        settings,
        optionOverrides,
      ),
      currentDirectory,
    };
  }
  const roots = workspaceRoots.length > 0 ? workspaceRoots : [ownerDirectory];
  const currentDirectory = roots[0] ?? ownerDirectory;
  return {
    fileNames: roots.flatMap((root) => collectWorkspaceScriptFiles(root)),
    options: browserJavaScriptCompilerOptions(
      defaultOptions,
      currentDirectory,
      settings,
      optionOverrides,
    ),
    currentDirectory,
  };
}

function browserJavaScriptCompilerOptions(
  options: ts.CompilerOptions,
  currentDirectory: string,
  settings: AspSettings,
  optionOverrides: Partial<ts.CompilerOptions>,
): ts.CompilerOptions {
  const next: ts.CompilerOptions = {
    ...options,
    ...optionOverrides,
    allowJs: true,
    noEmit: true,
    noLib: false,
    checkJs: optionOverrides.checkJs ?? settings.checkJs ?? false,
  };
  next.lib = ensureBrowserJavaScriptLibs(next.lib);
  next.types = browserJavaScriptTypes(next, currentDirectory);
  return next;
}

function ensureBrowserJavaScriptLibs(libs: string[] | undefined): string[] {
  const existing = new Set((libs ?? browserJavaScriptLibs).map((lib) => lib.toLowerCase()));
  return [
    ...(libs ?? []),
    ...browserJavaScriptLibs.filter((lib) => !existing.has(lib.toLowerCase())),
  ];
}

function browserJavaScriptTypes(
  options: ts.CompilerOptions,
  currentDirectory: string,
): string[] | undefined {
  if (options.types) {
    return options.types.filter((type) => type.toLowerCase() !== "node");
  }
  const host: ts.ModuleResolutionHost = {
    fileExists: ts.sys.fileExists,
    readFile: ts.sys.readFile,
    directoryExists: ts.sys.directoryExists,
    getDirectories: ts.sys.getDirectories,
    realpath: ts.sys.realpath,
    getCurrentDirectory: () => currentDirectory,
  };
  const types = ts
    .getAutomaticTypeDirectiveNames(options, host)
    .filter((type) => type.toLowerCase() !== "node");
  return types;
}

function collectWorkspaceScriptFiles(root: string): string[] {
  const result: string[] = [];
  const normalizedRoot = normalizeFileName(root);
  const stat = fs.statSync(normalizedRoot, { throwIfNoEntry: false });
  if (!stat?.isDirectory()) {
    return result;
  }
  const cached = workspaceScriptFilesCache.get(normalizedRoot);
  if (cached && cached.mtimeMs === stat.mtimeMs && cached.size === stat.size) {
    return cached.files;
  }
  const visit = (dir: string): void => {
    for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
      const fullPath = path.join(dir, entry.name);
      if (entry.isDirectory()) {
        if (!isExcludedWorkspaceDirectory(entry.name, fullPath)) {
          visit(fullPath);
        }
        continue;
      }
      if (entry.isFile() && isScriptWorkspaceFile(entry.name)) {
        result.push(normalizeFileName(fullPath));
      }
    }
  };
  visit(normalizedRoot);
  workspaceScriptFilesCache.set(normalizedRoot, {
    mtimeMs: stat.mtimeMs,
    size: stat.size,
    files: result,
  });
  return result;
}

function scriptKindForFileName(fileName: string): ts.ScriptKind {
  const lower = fileName.toLowerCase();
  if (lower.endsWith(".ts") || lower.endsWith(".mts") || lower.endsWith(".cts")) {
    return ts.ScriptKind.TS;
  }
  if (lower.endsWith(".tsx")) {
    return ts.ScriptKind.TSX;
  }
  if (lower.endsWith(".jsx")) {
    return ts.ScriptKind.JSX;
  }
  return ts.ScriptKind.JS;
}

function resolveIncludePath(
  ownerUri: string,
  includePath: string,
  mode: "file" | "virtual",
  settings: AspSettings,
): string {
  if (mode === "virtual") {
    const normalizedInclude = includePath.replace(/^\/+/, "");
    for (const root of [
      ...(settings.virtualRoots ?? []),
      settings.virtualRoot,
      ...workspaceRoots,
      workspaceRootFromUri(ownerUri),
    ]) {
      if (!root) {
        continue;
      }
      const candidate = path.resolve(root, normalizedInclude);
      if (fs.existsSync(candidate)) {
        return candidate;
      }
    }
    return path.resolve(settings.virtualRoot ?? workspaceRootFromUri(ownerUri), normalizedInclude);
  }
  const local = path.resolve(path.dirname(uriToFileName(ownerUri)), includePath);
  if (fs.existsSync(local)) {
    return local;
  }
  for (const root of [...(settings.includePaths ?? []), ...(settings.virtualRoots ?? [])]) {
    const candidate = path.resolve(root, includePath);
    if (fs.existsSync(candidate)) {
      return candidate;
    }
  }
  return local;
}

function workspaceRootFromUri(uri: string): string {
  const fileName = uriToFileName(uri);
  return fs.statSync(fileName, { throwIfNoEntry: false })?.isDirectory()
    ? fileName
    : path.dirname(fileName);
}

function uriToFileName(uri: string): string {
  if (uri.startsWith("file://")) {
    return decodeURIComponent(new URL(uri).pathname);
  }
  return uri.replace(/\.(html|css|javascript|vbscript|jscript)\.virtual$/, "");
}

function jsVirtualFileName(uri: string): string {
  const fileName = uri.startsWith("file://") ? decodeURIComponent(new URL(uri).pathname) : uri;
  return fileName.replace(/\.(javascript|jscript)\.virtual$/, ".$1.js");
}

function pathToFileUri(fileName: string): string {
  return new URL(`file://${fileName}`).toString();
}

function normalizeFileName(fileName: string): string {
  return path.resolve(fileName);
}

function sameFile(left: string, right: string): boolean {
  return normalizeFileName(left) === normalizeFileName(right);
}

function isIncDocument(uri: string): boolean {
  return uriToFileName(uri).toLowerCase().endsWith(".inc");
}

function tsCompletionKind(kind: string): CompletionItemKind {
  switch (kind) {
    case "function":
    case "method":
      return CompletionItemKind.Function;
    case "var":
    case "let":
    case "const":
      return CompletionItemKind.Variable;
    case "class":
      return CompletionItemKind.Class;
    case "property":
      return CompletionItemKind.Property;
    default:
      return CompletionItemKind.Text;
  }
}

function isVbscriptPosition(cached: CachedDocument, position: Range["start"]): boolean {
  const region = findRegionAt(cached.parsed, cached.source.offsetAt(position));
  return region?.language === "vbscript";
}

function isJavaScriptPosition(cached: CachedDocument, position: Range["start"]): boolean {
  const region = findRegionAt(cached.parsed, cached.source.offsetAt(position));
  return Boolean(region && isJavaScriptLikeRegion(region));
}

function isHtmlPosition(cached: CachedDocument, position: Range["start"]): boolean {
  const region = findRegionAt(cached.parsed, cached.source.offsetAt(position));
  return region?.language === "html";
}

function isCssPosition(cached: CachedDocument, position: Range["start"]): boolean {
  const region = findRegionAt(cached.parsed, cached.source.offsetAt(position));
  return region?.language === "css";
}

function vbWorkspaceSymbolKind(kind: VbSymbolKind): SymbolKind {
  switch (kind) {
    case "class":
      return SymbolKind.Class;
    case "method":
    case "sub":
    case "function":
      return SymbolKind.Function;
    case "property":
      return SymbolKind.Property;
    case "constant":
      return SymbolKind.Constant;
    case "field":
      return SymbolKind.Field;
    case "parameter":
    case "variable":
      return SymbolKind.Variable;
  }
}

function tsSymbolKind(kind: ts.ScriptElementKind): SymbolKind {
  switch (kind) {
    case ts.ScriptElementKind.classElement:
      return SymbolKind.Class;
    case ts.ScriptElementKind.memberFunctionElement:
    case ts.ScriptElementKind.functionElement:
    case ts.ScriptElementKind.localFunctionElement:
      return SymbolKind.Function;
    case ts.ScriptElementKind.memberVariableElement:
    case ts.ScriptElementKind.memberGetAccessorElement:
    case ts.ScriptElementKind.memberSetAccessorElement:
      return SymbolKind.Property;
    case ts.ScriptElementKind.constElement:
      return SymbolKind.Constant;
    case ts.ScriptElementKind.letElement:
    case ts.ScriptElementKind.variableElement:
      return SymbolKind.Variable;
    case ts.ScriptElementKind.moduleElement:
      return SymbolKind.Module;
    default:
      return SymbolKind.Object;
  }
}

function quickFixesForDiagnostic(cached: CachedDocument, diagnostic: Diagnostic): CodeAction[] {
  if (diagnostic.source === "asp-lsp-vbscript") {
    const name = textInRange(cached.source, diagnostic.range).trim();
    if (!name) {
      return [];
    }
    const line = diagnostic.range.start.line;
    const localizer = localizerForUri(cached.source.uri);
    return [
      {
        title: localizer.t("server.quickfix.declareDim", { name }),
        kind: CodeActionKind.QuickFix,
        diagnostics: [diagnostic],
        edit: {
          changes: {
            [cached.source.uri]: [
              {
                range: {
                  start: { line, character: 0 },
                  end: { line, character: 0 },
                },
                newText: `Dim ${name}\n`,
              },
            ],
          },
        },
      },
      ...vbscriptIncludeSuggestionActions(cached, diagnostic, name),
    ];
  }
  if (diagnostic.source === "asp-lsp-vbscript-unused") {
    return removeUnusedVbscriptDeclarationActions(cached, diagnostic);
  }
  if (diagnostic.source === "asp-lsp-vbscript-type") {
    return vbscriptTypeDiagnosticActions(cached, diagnostic);
  }
  if (diagnostic.source === "asp-lsp-vbscript-naming") {
    return vbscriptNamingDiagnosticActions(cached, diagnostic);
  }
  if (diagnostic.source === "asp-lsp-include") {
    const include = cached.parsed.includes.find(
      (candidate) =>
        candidate.range.start.line === diagnostic.range.start.line &&
        candidate.range.start.character === diagnostic.range.start.character,
    );
    const includePath = include?.path;
    if (!includePath) {
      return [];
    }
    const localizer = localizerForUri(cached.source.uri);
    const targetPath = include
      ? resolveIncludePath(
          cached.source.uri,
          include.path,
          include.mode,
          cachedSettings(cached.source.uri),
        )
      : undefined;
    return [
      {
        title: localizer.t("server.quickfix.createMissingInclude", { path: includePath }),
        kind: CodeActionKind.QuickFix,
        diagnostics: [diagnostic],
        edit: targetPath
          ? ({
              documentChanges: [
                {
                  kind: "create",
                  uri: pathToFileUri(targetPath),
                  options: { ignoreIfExists: true },
                },
              ],
            } satisfies WorkspaceEdit)
          : undefined,
      },
    ];
  }
  return [];
}

function vbscriptNamingDiagnosticActions(
  cached: CachedDocument,
  diagnostic: Diagnostic,
): CodeAction[] {
  const data = diagnostic.data as
    | {
        name?: string;
        expectedName?: string;
      }
    | undefined;
  const name = data?.name ?? textInRange(cached.source, diagnostic.range).trim();
  const expectedName = data?.expectedName;
  if (
    !name ||
    !expectedName ||
    name === expectedName ||
    !/^[A-Za-z][A-Za-z0-9_]*$/.test(expectedName)
  ) {
    return [];
  }
  const context = buildVbProjectContext(cached, cachedSettings(cached.source.uri));
  const symbol = context.symbols?.find(
    (candidate) =>
      candidate.sourceUri === cached.source.uri && sameRange(candidate.range, diagnostic.range),
  );
  if (!symbol || hasVbscriptIdentifierCollision(symbol, expectedName, context.symbols ?? [])) {
    return [];
  }
  const changes: WorkspaceEdit["changes"] = {};
  for (const reference of getVbscriptReferences(cached.parsed, diagnostic.range.start, context)) {
    const edits = changes[reference.uri] ?? [];
    edits.push({ range: reference.range, newText: expectedName });
    changes[reference.uri] = edits;
  }
  if (Object.keys(changes).length === 0) {
    return [];
  }
  return [
    {
      title: localizerForUri(cached.source.uri).t("server.quickfix.renameIdentifierCase", {
        name,
        expectedName,
      }),
      kind: CodeActionKind.QuickFix,
      diagnostics: [diagnostic],
      edit: { changes },
    },
  ];
}

function hasVbscriptIdentifierCollision(
  symbol: VbSymbol,
  expectedName: string,
  symbols: VbSymbol[],
): boolean {
  const lower = expectedName.toLowerCase();
  return symbols.some(
    (candidate) =>
      candidate.name.toLowerCase() === lower &&
      !sameVbscriptSymbol(candidate, symbol) &&
      sameVbscriptNamingScope(candidate, symbol),
  );
}

function sameVbscriptSymbol(left: VbSymbol, right: VbSymbol): boolean {
  return (
    left.sourceUri === right.sourceUri &&
    left.kind === right.kind &&
    (left.memberOf ?? "").toLowerCase() === (right.memberOf ?? "").toLowerCase() &&
    (left.scopeName ?? "").toLowerCase() === (right.scopeName ?? "").toLowerCase() &&
    sameRange(left.range, right.range)
  );
}

function sameVbscriptNamingScope(left: VbSymbol, right: VbSymbol): boolean {
  if (left.memberOf || right.memberOf) {
    return (left.memberOf ?? "").toLowerCase() === (right.memberOf ?? "").toLowerCase();
  }
  if (left.scopeName || right.scopeName) {
    return (
      left.sourceUri === right.sourceUri &&
      (left.scopeName ?? "").toLowerCase() === (right.scopeName ?? "").toLowerCase()
    );
  }
  return true;
}

function vbscriptTypeDiagnosticActions(
  cached: CachedDocument,
  diagnostic: Diagnostic,
): CodeAction[] {
  const code = String(diagnostic.code ?? "");
  const data = diagnostic.data as
    | {
        name?: string;
        type?: string;
        actual?: string;
      }
    | undefined;
  const name = data?.name ?? textInRange(cached.source, diagnostic.range).trim();
  if (!name) {
    return [];
  }
  const localizer = localizerForUri(cached.source.uri);
  if (code === "objectNeedsSet") {
    return [
      {
        title: localizer.t("server.quickfix.addSet", { name }),
        kind: CodeActionKind.QuickFix,
        diagnostics: [diagnostic],
        edit: {
          changes: {
            [cached.source.uri]: [
              {
                range: { start: diagnostic.range.start, end: diagnostic.range.start },
                newText: "Set ",
              },
            ],
          },
        },
      },
    ];
  }
  if (code === "setScalar") {
    const edit = removeLeadingSetEdit(cached.source, diagnostic.range.start.line);
    return edit
      ? [
          {
            title: localizer.t("server.quickfix.removeSet", { name }),
            kind: CodeActionKind.QuickFix,
            diagnostics: [diagnostic],
            edit: { changes: { [cached.source.uri]: [edit] } },
          },
        ]
      : [];
  }
  if (code === "typeMismatch" && data?.actual) {
    return [
      {
        title: localizer.t("server.quickfix.annotateType", { name, type: data.actual }),
        kind: CodeActionKind.QuickFix,
        diagnostics: [diagnostic],
        edit: {
          changes: {
            [cached.source.uri]: [
              {
                range: {
                  start: { line: diagnostic.range.start.line, character: 0 },
                  end: { line: diagnostic.range.start.line, character: 0 },
                },
                newText: `' @type ${name} As ${data.actual}\n`,
              },
            ],
          },
        },
      },
    ];
  }
  return [];
}

function removeUnusedVbscriptDeclarationActions(
  cached: CachedDocument,
  diagnostic: Diagnostic,
): CodeAction[] {
  const symbol = buildVbProjectContext(cached, cachedSettings(cached.source.uri)).symbols?.find(
    (candidate) =>
      candidate.sourceUri === cached.source.uri && sameRange(candidate.range, diagnostic.range),
  );
  if (!symbol || symbol.kind === "class" || symbol.kind === "function" || symbol.kind === "sub") {
    return [];
  }
  const edit =
    symbol.kind === "parameter"
      ? removeVbscriptParameterEdit(cached, symbol)
      : removeLineEdit(cached.source, symbol.range.start.line);
  if (!edit) {
    return [];
  }
  return [
    {
      title: localizerForUri(cached.source.uri).t("server.quickfix.removeUnusedDeclaration", {
        name: symbol.name,
      }),
      kind: CodeActionKind.QuickFix,
      diagnostics: [diagnostic],
      edit: {
        changes: {
          [cached.source.uri]: [edit],
        },
      },
    },
  ];
}

function removeLeadingSetEdit(document: TextDocument, line: number): TextEdit | undefined {
  const text = lineText(document, line);
  const match = /^(\s*)Set\s+/i.exec(text);
  if (!match) {
    return undefined;
  }
  const start = match[1]?.length ?? 0;
  return {
    range: {
      start: { line, character: start },
      end: { line, character: match[0].length },
    },
    newText: "",
  };
}

function removeVbscriptParameterEdit(
  cached: CachedDocument,
  symbol: VbSymbol,
): TextEdit | undefined {
  const line = lineText(cached.source, symbol.range.start.line);
  const startOffset = cached.source.offsetAt(symbol.range.start);
  const endOffset = cached.source.offsetAt(symbol.range.end);
  const lineStart = cached.source.offsetAt({ line: symbol.range.start.line, character: 0 });
  const lineEnd = lineStart + line.length;
  let removeStart = startOffset;
  let removeEnd = endOffset;
  const after = cached.source.getText({
    start: symbol.range.end,
    end: cached.source.positionAt(lineEnd),
  });
  const before = cached.source.getText({
    start: cached.source.positionAt(lineStart),
    end: symbol.range.start,
  });
  const afterComma = /^\s*,\s*/.exec(after);
  const beforeComma = /,\s*$/.exec(before);
  if (afterComma) {
    removeEnd += afterComma[0].length;
  } else if (beforeComma) {
    removeStart -= beforeComma[0].length;
  }
  if (removeStart < lineStart || removeEnd > lineEnd || removeStart >= removeEnd) {
    return undefined;
  }
  return {
    range: {
      start: cached.source.positionAt(removeStart),
      end: cached.source.positionAt(removeEnd),
    },
    newText: "",
  };
}

function vbscriptIncludeSuggestionActions(
  cached: CachedDocument,
  diagnostic: Diagnostic,
  symbolName: string,
): CodeAction[] {
  const settings = cachedSettings(cached.source.uri);
  if (settings.vbscript?.includeSuggestions === false) {
    return [];
  }
  ensureWorkspaceIndex();
  const ownerFile = normalizeFileName(uriToFileName(cached.source.uri));
  const includedFiles = new Set(
    cached.parsed.includes.map((include) =>
      normalizeFileName(
        resolveIncludePath(cached.source.uri, include.path, include.mode, settings),
      ),
    ),
  );
  const candidates = new Map<string, CachedDocument>();
  for (const entry of workspaceIndex.values()) {
    if (normalizeFileName(entry.fileName) !== ownerFile) {
      candidates.set(normalizeFileName(entry.fileName), cachedFromIndexed(entry));
    }
  }
  for (const document of documents.all()) {
    const fileName = normalizeFileName(uriToFileName(document.uri));
    if (fileName !== ownerFile) {
      const opened = getCached(document.uri);
      if (opened) {
        candidates.set(fileName, opened);
      }
    }
  }
  const matches = [...candidates.entries()]
    .filter(([fileName]) => !includedFiles.has(fileName))
    .map(([fileName, candidate]) => {
      const hasSymbol = collectVbscriptSymbols(candidate.parsed).some(
        (symbol) =>
          symbol.sourceUri === candidate.source.uri &&
          !symbol.memberOf &&
          symbol.name.toLowerCase() === symbolName.toLowerCase(),
      );
      return hasSymbol ? { fileName, candidate } : undefined;
    })
    .filter((item): item is { fileName: string; candidate: CachedDocument } => Boolean(item))
    .sort(
      (left, right) => includeCandidateRank(left.fileName) - includeCandidateRank(right.fileName),
    )
    .slice(0, 5);
  const insert = includeInsertionPoint(cached);
  const localizer = localizerForUri(cached.source.uri);
  return matches.map(({ fileName }) => {
    const include = includeSuggestionPath(cached.source.uri, fileName, settings);
    return {
      title: localizer.t("server.quickfix.includeSymbol", {
        path: include.path,
        symbol: symbolName,
      }),
      kind: CodeActionKind.QuickFix,
      diagnostics: [diagnostic],
      edit: {
        changes: {
          [cached.source.uri]: [
            {
              range: { start: insert, end: insert },
              newText: `<!-- #include ${include.mode}="${include.path}" -->\n`,
            },
          ],
        },
      },
    } satisfies CodeAction;
  });
}

function includeCandidateRank(fileName: string): number {
  const lower = fileName.toLowerCase();
  return (lower.endsWith(".inc") ? 0 : lower.endsWith(".asa") ? 1 : 2) * 100_000 + lower.length;
}

function includeInsertionPoint(cached: CachedDocument): Position {
  if (cached.parsed.includes.length === 0) {
    return { line: 0, character: 0 };
  }
  const last = cached.parsed.includes.reduce((current, include) =>
    include.range.end.line > current.range.end.line ? include : current,
  );
  return { line: last.range.end.line + 1, character: 0 };
}

function includeSuggestionPath(
  ownerUri: string,
  targetFile: string,
  settings: AspSettings,
): { mode: "file" | "virtual"; path: string } {
  for (const root of [...(settings.virtualRoots ?? []), settings.virtualRoot]) {
    if (!root) {
      continue;
    }
    const relative = path.relative(root, targetFile);
    if (!relative.startsWith("..") && !path.isAbsolute(relative)) {
      return { mode: "virtual", path: `/${relative.split(path.sep).join("/")}` };
    }
  }
  return {
    mode: "file",
    path: path
      .relative(path.dirname(uriToFileName(ownerUri)), targetFile)
      .split(path.sep)
      .join("/"),
  };
}

const vbscriptExtractVariableKind = `${CodeActionKind.Refactor}.extract`;

function vbscriptCodeActions(
  cached: CachedDocument,
  range: Range,
  context: CodeActionContext,
): CodeAction[] {
  if (!codeActionAllows(context, vbscriptExtractVariableKind)) {
    return [];
  }
  const edit = extractVbscriptVariableEdit(cached, range);
  if (!edit) {
    return [];
  }
  return [
    {
      title: localizerForUri(cached.source.uri).t("server.refactor.extractVbscriptVariable"),
      kind: vbscriptExtractVariableKind,
      edit,
    },
  ];
}

function extractVbscriptVariableEdit(
  cached: CachedDocument,
  range: Range,
): WorkspaceEdit | undefined {
  if (!isVbscriptRange(cached, range) || range.start.line !== range.end.line) {
    return undefined;
  }
  const selected = textInRange(cached.source, range);
  if (!selected.trim() || selected !== selected.trim() || /[\r\n]/.test(selected)) {
    return undefined;
  }
  const line = lineText(cached.source, range.start.line);
  const indent = /^\s*/.exec(line)?.[0] ?? "";
  const name = nextVbscriptExtractVariableName(cached);
  const insertPosition = { line: range.start.line, character: 0 };
  return {
    changes: {
      [cached.source.uri]: [
        {
          range: { start: insertPosition, end: insertPosition },
          newText: `${indent}Dim ${name}\n${indent}${name} = ${selected}\n`,
        },
        {
          range,
          newText: name,
        },
      ],
    },
  };
}

function nextVbscriptExtractVariableName(cached: CachedDocument): string {
  const used = new Set(
    collectVbscriptSymbols(cached.parsed).map((symbol) => symbol.name.toLowerCase()),
  );
  if (!used.has("extractedvalue")) {
    return "extractedValue";
  }
  for (let index = 1; index < 1000; index += 1) {
    const name = `extractedValue${index}`;
    if (!used.has(name.toLowerCase())) {
      return name;
    }
  }
  return "extractedValue";
}

function isVbscriptRange(cached: CachedDocument, range: Range): boolean {
  const start = cached.source.offsetAt(range.start);
  const end = cached.source.offsetAt(range.end);
  if (start >= end) {
    return false;
  }
  return (
    findRegionAt(cached.parsed, start)?.language === "vbscript" &&
    findRegionAt(cached.parsed, Math.max(start, end - 1))?.language === "vbscript"
  );
}

function cssCodeActions(
  cached: CachedDocument,
  range: Range,
  context: CodeActionContext,
): CodeAction[] {
  const virtual = cached.virtuals.get("css");
  const start = virtual?.sourceMap.toVirtualPosition(range.start);
  const end = virtual?.sourceMap.toVirtualPosition(range.end);
  if (!virtual || !start || !end) {
    return [];
  }
  const doc = toTextDocument(virtual);
  return cssService
    .doCodeActions2(doc, { start, end }, context, cssService.parseStylesheet(doc))
    .map((action) => remapCssCodeAction(virtual, action, cached.source.uri))
    .filter((action): action is CodeAction => Boolean(action));
}

function remapCssCodeAction(
  virtual: VirtualDocument,
  action: CodeAction,
  sourceUri: string,
): CodeAction | undefined {
  if (!action.edit) {
    return action;
  }
  return {
    ...action,
    edit: remapWorkspaceEdit(virtual, action.edit, sourceUri),
  };
}

function jsCodeActions(
  cached: CachedDocument,
  range: Range,
  context: CodeActionContext,
): CodeAction[] {
  const actions: CodeAction[] = [];
  if (codeActionAllows(context, CodeActionKind.SourceOrganizeImports)) {
    const edit = organizeJavaScriptImportsEdit(cached);
    if (edit || jsVirtualDocuments(cached).length > 0) {
      actions.push({
        title: localizerForUri(cached.source.uri).t("server.codeAction.organizeJavascriptImports"),
        kind: "source.organizeImports.aspLsp.javascript",
        edit: edit ?? { changes: {} },
      });
    }
  }
  const sourceStart = cached.source.offsetAt(range.start);
  const sourceEnd = cached.source.offsetAt(range.end);
  for (const virtual of jsVirtualDocuments(cached)) {
    const virtualStart = virtual.sourceMap.toVirtualOffset(sourceStart);
    const virtualEnd = virtual.sourceMap.toVirtualOffset(sourceEnd);
    if (virtualStart === undefined || virtualEnd === undefined) {
      continue;
    }
    const service = createJsLanguageService(virtual, cachedSettings(cached.source.uri)).service;
    const fileName = jsVirtualFileName(virtual.uri);
    if (codeActionAllows(context, CodeActionKind.QuickFix)) {
      const errorCodes = context.diagnostics
        .filter(
          (diagnostic) =>
            diagnostic.source === "asp-lsp-typescript" ||
            diagnostic.source === "asp-lsp-typescript-unused",
        )
        .map((diagnostic) => Number(diagnostic.code))
        .filter((code) => Number.isInteger(code));
      for (const fix of service.getCodeFixesAtPosition(
        fileName,
        virtualStart,
        virtualEnd,
        errorCodes,
        {},
        jsCompletionPreferences(cachedSettings(cached.source.uri)),
      )) {
        const edit = fileTextChangesToWorkspaceEdit(virtual, fix.changes);
        if (edit) {
          actions.push({ title: fix.description, kind: CodeActionKind.QuickFix, edit });
        }
      }
    }
    if (codeActionAllows(context, CodeActionKind.Refactor)) {
      for (const refactor of service.getApplicableRefactors(
        fileName,
        { pos: virtualStart, end: virtualEnd },
        {},
      )) {
        for (const action of refactor.actions.filter((item) => !item.notApplicableReason)) {
          const edits = service.getEditsForRefactor(
            fileName,
            {},
            { pos: virtualStart, end: virtualEnd },
            refactor.name,
            action.name,
            {},
          );
          const edit = edits ? fileTextChangesToWorkspaceEdit(virtual, edits.edits) : undefined;
          if (edit) {
            actions.push({
              title: action.description,
              kind: action.kind ?? CodeActionKind.Refactor,
              edit,
            });
          }
        }
      }
    }
  }
  return actions;
}

function codeActionAllows(context: CodeActionContext, kind: string): boolean {
  if (!context.only || context.only.length === 0) {
    return true;
  }
  return context.only.some(
    (candidate) =>
      kind === candidate || kind.startsWith(`${candidate}.`) || candidate.startsWith(`${kind}.`),
  );
}

function organizeJavaScriptImportsEdit(cached: CachedDocument): WorkspaceEdit | undefined {
  const edits = jsVirtualDocuments(cached)
    .map((virtual) => {
      const service = createJsLanguageService(virtual, cachedSettings(cached.source.uri)).service;
      return fileTextChangesToWorkspaceEdit(
        virtual,
        service.organizeImports({ type: "file", fileName: jsVirtualFileName(virtual.uri) }, {}, {}),
      );
    })
    .filter((edit): edit is WorkspaceEdit => Boolean(edit));
  return mergeWorkspaceEdits(edits);
}

function fileTextChangesToWorkspaceEdit(
  virtual: VirtualDocument,
  changes: readonly ts.FileTextChanges[],
): WorkspaceEdit | undefined {
  const sourceUri = virtualSourceUri(virtual);
  const currentFileName = normalizeFileName(jsVirtualFileName(virtual.uri));
  const edits: TextEdit[] = [];
  for (const change of changes) {
    if (normalizeFileName(change.fileName) !== currentFileName) {
      return undefined;
    }
    for (const textChange of change.textChanges) {
      const edit = textChangeToSourceTextEdit(virtual, textChange);
      if (!edit) {
        return undefined;
      }
      edits.push(edit);
    }
  }
  return edits.length > 0 ? { changes: { [sourceUri]: edits } } : undefined;
}

function textChangeToSourceTextEdit(
  virtual: VirtualDocument,
  textChange: ts.TextChange,
): TextEdit | undefined {
  const range = textSpanToSourceRange(virtual, textChange.span);
  return range ? { range, newText: textChange.newText } : undefined;
}

function mergeWorkspaceEdits(
  edits: Array<WorkspaceEdit | null | undefined>,
): WorkspaceEdit | undefined {
  const changes: NonNullable<WorkspaceEdit["changes"]> = {};
  for (const edit of edits) {
    if (!edit) {
      continue;
    }
    for (const [uri, textEdits] of Object.entries(edit.changes ?? {})) {
      changes[uri] = [...(changes[uri] ?? []), ...textEdits];
    }
  }
  return Object.keys(changes).length > 0 ? { changes } : undefined;
}

function codeLenses(cached: CachedDocument): CodeLens[] {
  const settings = cachedSettings(cached.source.uri).codeLens;
  const context = buildVbProjectContext(cached, cachedSettings(cached.source.uri));
  const localizer = localizerForUri(cached.source.uri);
  const lenses: CodeLens[] = [];
  if (settings?.references !== false) {
    for (const symbol of (context.symbols ?? []).filter(
      (item) =>
        item.sourceUri === cached.source.uri &&
        ["function", "sub", "class", "method", "property"].includes(item.kind),
    )) {
      const references = getVbscriptReferences(cached.parsed, symbol.range.start, context, {
        includeDeclaration: false,
        includeFunctionReturnAssignments: false,
      });
      lenses.push({
        range: symbol.range,
        command: {
          title: localizer.t(
            references.length === 1 ? "server.codeLens.reference" : "server.codeLens.references",
            { count: references.length },
          ),
          command: "aspLsp.showReferences",
          arguments: [
            cached.source.uri,
            symbol.range.start,
            references.map((reference) => Location.create(reference.uri, reference.range)),
          ],
        },
      });
    }
  }
  if (settings?.includes !== false) {
    for (const include of cached.parsed.includes) {
      const target = resolveIncludePath(
        cached.source.uri,
        include.path,
        include.mode,
        cachedSettings(cached.source.uri),
      );
      lenses.push({
        range: include.range,
        command: {
          title: localizer.t("server.codeLens.include", { name: path.basename(target) }),
          command: "vscode.open",
          arguments: [pathToFileUri(target)],
        },
      });
    }
  }
  return lenses;
}

function onTypeFormatting(
  cached: CachedDocument,
  position: Position,
  character: string,
  formattingOptions: FormattingOptions,
): TextEdit[] {
  if (character === ">") {
    return (
      jsOnTypeFormatting(cached, position, character, formattingOptions) ??
      htmlOnTypeFormatting(cached, position, formattingOptions) ??
      aspCloseOnTypeFormatting(cached, position, formattingOptions) ??
      []
    );
  }
  const jsEdits = jsOnTypeFormatting(cached, position, character, formattingOptions);
  if (jsEdits) {
    return jsEdits;
  }
  if (!isVbscriptPosition(cached, position)) {
    return [];
  }
  const line = position.line;
  if (line <= 0) {
    return [];
  }
  const current = lineText(cached.source, line);
  const previous = previousNonEmptyLine(cached.source, line - 1);
  if (!previous) {
    return [];
  }
  const options = formatOptions(formattingOptions, cachedSettings(cached.source.uri));
  const unit =
    options.indentStyle === "tab" || options.insertSpaces === false
      ? "\t"
      : " ".repeat(options.indentSize ?? options.tabSize);
  const baseIndent = /^\s*/.exec(previous.text)?.[0] ?? "";
  const trimmedPrevious = previous.text.trim();
  const trimmedCurrent = current.trim();
  const shouldIndent =
    /^(If|For|For\s+Each|Do|While|With|Sub|Function|Class|Property)\b/i.test(trimmedPrevious) &&
    !/^End\b/i.test(trimmedPrevious);
  const shouldOutdent = /^(End|Else|ElseIf|Next|Loop|Wend)\b/i.test(trimmedCurrent);
  const desired = shouldOutdent
    ? baseIndent.slice(0, Math.max(0, baseIndent.length - unit.length))
    : shouldIndent
      ? `${baseIndent}${unit}`
      : baseIndent;
  const existing = /^\s*/.exec(current)?.[0] ?? "";
  return existing === desired
    ? []
    : [
        {
          range: {
            start: { line, character: 0 },
            end: { line, character: existing.length },
          },
          newText: desired,
        },
      ];
}

function jsOnTypeFormatting(
  cached: CachedDocument,
  position: Position,
  character: string,
  formattingOptions: FormattingOptions,
): TextEdit[] | undefined {
  const context = jsContextAt(cached, position);
  if (!context) {
    return undefined;
  }
  return context.service
    .getFormattingEditsAfterKeystroke(
      context.fileName,
      context.offset,
      character,
      tsFormatOptions(formatOptions(formattingOptions, cachedSettings(cached.source.uri))),
    )
    .map((change) => textChangeToSourceTextEdit(context.virtual, change))
    .filter((edit): edit is TextEdit => Boolean(edit));
}

function htmlOnTypeFormatting(
  cached: CachedDocument,
  position: Position,
  formattingOptions: FormattingOptions,
): TextEdit[] | undefined {
  const offset = Math.max(0, cached.source.offsetAt(position) - 1);
  const region = findRegionAt(cached.parsed, offset);
  if (!region || region.language !== "html") {
    return undefined;
  }
  const lineRange = {
    start: { line: position.line, character: 0 },
    end: { line: position.line + 1, character: 0 },
  };
  if (rangeOverlapsNonHtml(cached, lineRange)) {
    return undefined;
  }
  const virtual = cached.virtuals.get("html");
  if (!virtual) {
    return undefined;
  }
  return htmlService
    .format(toTextDocument(virtual), lineRange, {
      tabSize: formattingOptions.tabSize,
      insertSpaces: formattingOptions.insertSpaces,
    })
    .map((edit) => {
      const range = sourceRangeFromVirtualRange(virtual, edit.range);
      return range ? { ...edit, range } : undefined;
    })
    .filter((edit): edit is TextEdit => Boolean(edit));
}

function aspCloseOnTypeFormatting(
  cached: CachedDocument,
  position: Position,
  formattingOptions: FormattingOptions,
): TextEdit[] | undefined {
  const current = lineText(cached.source, position.line);
  if (!current.slice(0, position.character).trimEnd().endsWith("%>")) {
    return undefined;
  }
  const previous = previousNonEmptyLine(cached.source, position.line - 1);
  if (!previous) {
    return undefined;
  }
  const options = formatOptions(formattingOptions, cachedSettings(cached.source.uri));
  const unit =
    options.indentStyle === "tab" || options.insertSpaces === false
      ? "\t"
      : " ".repeat(options.indentSize ?? options.tabSize);
  const previousIndent = /^\s*/.exec(previous.text)?.[0] ?? "";
  const desired = /^End\b/i.test(previous.text.trim())
    ? previousIndent.slice(0, Math.max(0, previousIndent.length - unit.length))
    : previousIndent;
  const existing = /^\s*/.exec(current)?.[0] ?? "";
  return existing === desired
    ? []
    : [
        {
          range: {
            start: { line: position.line, character: 0 },
            end: { line: position.line, character: existing.length },
          },
          newText: desired,
        },
      ];
}

function lineText(document: TextDocument, line: number): string {
  return document
    .getText({
      start: { line, character: 0 },
      end: { line: line + 1, character: 0 },
    })
    .replace(/\r?\n$/, "");
}

function textInRange(document: TextDocument, range: Range): string {
  return document.getText(range);
}

function removeLineEdit(document: TextDocument, line: number): TextEdit {
  const end =
    line + 1 < document.lineCount
      ? { line: line + 1, character: 0 }
      : { line, character: lineText(document, line).length };
  return {
    range: {
      start: { line, character: 0 },
      end,
    },
    newText: "",
  };
}

function sameRange(left: Range, right: Range): boolean {
  return (
    left.start.line === right.start.line &&
    left.start.character === right.start.character &&
    left.end.line === right.end.line &&
    left.end.character === right.end.character
  );
}

function rangesOverlap(left: Range, right: Range): boolean {
  return comparePositions(left.start, right.end) < 0 && comparePositions(left.end, right.start) > 0;
}

function comparePositions(left: Position, right: Position): number {
  if (left.line !== right.line) {
    return left.line - right.line;
  }
  return left.character - right.character;
}

function wordRangeAt(document: TextDocument, position: Position): Range | null {
  const line = lineText(document, position.line);
  const pattern = /[A-Za-z_][A-Za-z0-9_-]*/g;
  let match: RegExpExecArray | null;
  while ((match = pattern.exec(line))) {
    const start = match.index;
    const end = start + match[0].length;
    if (position.character >= start && position.character <= end) {
      return {
        start: { line: position.line, character: start },
        end: { line: position.line, character: end },
      };
    }
  }
  return null;
}

function previousNonEmptyLine(
  document: TextDocument,
  line: number,
): { line: number; text: string } | undefined {
  for (let current = line; current >= 0; current -= 1) {
    const text = lineText(document, current);
    if (text.trim().length > 0) {
      return { line: current, text };
    }
  }
  return undefined;
}

function buildSemanticTokens(cached: CachedDocument, range?: Range): SemanticTokens {
  const rangeStart = range ? cached.source.offsetAt(range.start) : 0;
  const rangeEnd = range ? cached.source.offsetAt(range.end) : cached.source.getText().length;
  const tokens: SemanticTokenData[] = [];
  for (const region of cached.parsed.regions) {
    if (region.end < rangeStart || region.start > rangeEnd) {
      continue;
    }
    if (region.kind === "asp-block" || region.kind === "asp-expression") {
      addSemanticToken(tokens, cached.source, region.start, 2, "keyword");
      if (region.kind === "asp-expression") {
        addSemanticToken(tokens, cached.source, region.start + 2, 1, "keyword");
      }
      if (region.end - region.contentEnd >= 2) {
        addSemanticToken(tokens, cached.source, region.contentEnd, 2, "keyword");
      }
    } else if (region.kind === "asp-directive") {
      addSemanticToken(
        tokens,
        cached.source,
        region.start,
        Math.min(region.end - region.start, 3),
        "keyword",
      );
    }
  }
  for (const semanticToken of getVbscriptSemanticTokens(
    cached.parsed,
    buildVbProjectContext(cached, cachedSettings(cached.source.uri)),
  )) {
    const offset = cached.source.offsetAt(semanticToken.range.start);
    if (offset < rangeStart || offset > rangeEnd) {
      continue;
    }
    tokens.push({
      line: semanticToken.range.start.line,
      character: semanticToken.range.start.character,
      length: Math.max(1, semanticToken.range.end.character - semanticToken.range.start.character),
      tokenType: semanticToken.tokenType,
      tokenModifiers: semanticToken.tokenModifiers,
    });
  }
  addIncludeSemanticTokens(tokens, cached, rangeStart, rangeEnd);
  addEmbeddedSemanticTokens(tokens, cached, rangeStart, rangeEnd);
  const uniqueTokens = dedupeSemanticTokens(tokens).sort(
    (left, right) => left.line - right.line || left.character - right.character,
  );
  const builder = new SemanticTokensBuilder();
  for (const token of uniqueTokens) {
    builder.push(
      token.line,
      token.character,
      token.length,
      semanticTokenTypes.indexOf(token.tokenType as (typeof semanticTokenTypes)[number]),
      semanticTokenModifierBitset(token.tokenModifiers),
    );
  }
  return builder.build();
}

function addIncludeSemanticTokens(
  tokens: SemanticTokenData[],
  cached: CachedDocument,
  rangeStart: number,
  rangeEnd: number,
): void {
  for (const include of cached.parsed.includes) {
    addRangeSemanticToken(
      tokens,
      cached.source,
      include.directiveRange,
      "keyword",
      rangeStart,
      rangeEnd,
    );
    addRangeSemanticToken(
      tokens,
      cached.source,
      include.modeRange,
      "property",
      rangeStart,
      rangeEnd,
    );
    addRangeSemanticToken(tokens, cached.source, include.pathRange, "string", rangeStart, rangeEnd);
  }
}

function addRangeSemanticToken(
  tokens: SemanticTokenData[],
  document: TextDocument,
  range: Range,
  tokenType: string,
  rangeStart: number,
  rangeEnd: number,
): void {
  const offset = document.offsetAt(range.start);
  if (offset < rangeStart || offset > rangeEnd || range.start.line !== range.end.line) {
    return;
  }
  addSemanticToken(tokens, document, offset, document.offsetAt(range.end) - offset, tokenType);
}

function dedupeSemanticTokens(tokens: SemanticTokenData[]): SemanticTokenData[] {
  const seen = new Set<string>();
  return tokens.filter((token) => {
    const key = `${token.line}:${token.character}:${token.length}:${token.tokenType}:${semanticTokenModifierBitset(token.tokenModifiers)}`;
    if (seen.has(key)) {
      return false;
    }
    seen.add(key);
    return true;
  });
}

function semanticTokenModifierBitset(modifiers: readonly string[] | undefined): number {
  let bitset = 0;
  for (const modifier of modifiers ?? []) {
    const index = semanticTokenModifiers.indexOf(
      modifier as (typeof semanticTokenModifiers)[number],
    );
    if (index !== -1) {
      bitset |= 1 << index;
    }
  }
  return bitset;
}

function cacheSemanticTokens(uri: string, data: number[]): SemanticTokens {
  const previous = latestSemanticTokenResultByUri.get(uri);
  if (previous) {
    semanticTokenResults.delete(previous);
  }
  const resultId = nextSemanticTokenResultId();
  semanticTokenResults.set(resultId, { uri, data });
  latestSemanticTokenResultByUri.set(uri, resultId);
  return { data, resultId };
}

function nextSemanticTokenResultId(): string {
  semanticTokenResultCounter += 1;
  return String(semanticTokenResultCounter);
}

function semanticTokenDeltaEdit(previous: number[], next: number[]) {
  let prefix = 0;
  while (prefix < previous.length && prefix < next.length && previous[prefix] === next[prefix]) {
    prefix += 1;
  }
  let previousSuffix = previous.length;
  let nextSuffix = next.length;
  while (
    previousSuffix > prefix &&
    nextSuffix > prefix &&
    previous[previousSuffix - 1] === next[nextSuffix - 1]
  ) {
    previousSuffix -= 1;
    nextSuffix -= 1;
  }
  return {
    start: prefix,
    deleteCount: previousSuffix - prefix,
    data: next.slice(prefix, nextSuffix),
  };
}

function clearSemanticTokensForUri(uri: string): void {
  const resultId = latestSemanticTokenResultByUri.get(uri);
  if (resultId) {
    semanticTokenResults.delete(resultId);
    latestSemanticTokenResultByUri.delete(uri);
  }
}

function clearSemanticTokens(): void {
  semanticTokenResults.clear();
  latestSemanticTokenResultByUri.clear();
}

function addEmbeddedSemanticTokens(
  tokens: SemanticTokenData[],
  cached: CachedDocument,
  rangeStart: number,
  rangeEnd: number,
): void {
  const html = cached.virtuals.get("html");
  if (html) {
    const pattern = /<\/?\s*([A-Za-z][A-Za-z0-9:-]*)/g;
    let match: RegExpExecArray | null;
    while ((match = pattern.exec(html.text))) {
      addVirtualWordToken(
        tokens,
        cached.source,
        cached,
        html,
        match.index + match[0].lastIndexOf(match[1]),
        match[1].length,
        "keyword",
        rangeStart,
        rangeEnd,
      );
    }
  }
  const css = cached.virtuals.get("css");
  if (css) {
    for (const match of css.text.matchAll(/\b([A-Za-z-]+)\s*:/g)) {
      if (match.index !== undefined) {
        addVirtualWordToken(
          tokens,
          cached.source,
          cached,
          css,
          match.index,
          match[1].length,
          "property",
          rangeStart,
          rangeEnd,
        );
      }
    }
  }
  for (const virtual of jsVirtualDocuments(cached)) {
    const project = createJsLanguageService(virtual, cachedSettings(cached.source.uri));
    addJavaScriptSemanticTokens(tokens, cached, virtual, project.service, rangeStart, rangeEnd);
  }
}

function addJavaScriptSemanticTokens(
  tokens: SemanticTokenData[],
  cached: CachedDocument,
  virtual: VirtualDocument,
  service: ts.LanguageService,
  rangeStart: number,
  rangeEnd: number,
): void {
  const spans = service.getEncodedSemanticClassifications(
    jsVirtualFileName(virtual.uri),
    { start: 0, length: virtual.text.length },
    ts.SemanticClassificationFormat.TwentyTwenty,
  ).spans;
  for (let index = 0; index + 2 < spans.length; index += 3) {
    const token = jsSemanticTokenFromClassification(spans[index + 2]);
    if (!token) {
      continue;
    }
    addVirtualWordToken(
      tokens,
      cached.source,
      cached,
      virtual,
      spans[index],
      spans[index + 1],
      token.tokenType,
      rangeStart,
      rangeEnd,
      token.tokenModifiers,
    );
  }
}

function jsSemanticTokenFromClassification(
  classification: number,
): { tokenType: string; tokenModifiers?: readonly string[] } | undefined {
  const typeIndex = (classification >> 8) - 1;
  const modifierSet = classification & 255;
  const tokenType = jsSemanticTokenType(typeIndex);
  if (!tokenType) {
    return undefined;
  }
  const tokenModifiers = jsSemanticTokenModifiers(modifierSet);
  return { tokenType, tokenModifiers };
}

function jsSemanticTokenType(typeIndex: number): string | undefined {
  switch (typeIndex) {
    case 0:
      return "class";
    case 1:
      return "enum";
    case 2:
      return "interface";
    case 3:
      return "namespace";
    case 4:
      return "typeParameter";
    case 5:
      return "typeAlias";
    case 6:
      return "parameter";
    case 7:
      return "variable";
    case 8:
      return "enumMember";
    case 9:
      return "property";
    case 10:
      return "function";
    case 11:
      return "method";
    default:
      return undefined;
  }
}

function jsSemanticTokenModifiers(modifierSet: number): string[] | undefined {
  const modifiers: string[] = [];
  if (modifierSet & (1 << 3)) {
    modifiers.push("readonly");
  }
  if (modifierSet & (1 << 4)) {
    modifiers.push("library");
  }
  return modifiers.length > 0 ? modifiers : undefined;
}

function addVirtualWordToken(
  tokens: SemanticTokenData[],
  document: TextDocument,
  cached: CachedDocument,
  virtual: VirtualDocument,
  virtualOffset: number,
  length: number,
  tokenType: string,
  rangeStart: number,
  rangeEnd: number,
  tokenModifiers?: readonly string[],
): void {
  const sourceOffset = virtual.sourceMap.toSourceOffset(virtualOffset);
  const sourceEndOffset = virtual.sourceMap.toSourceOffset(virtualOffset + length - 1);
  if (
    sourceOffset === undefined ||
    sourceEndOffset === undefined ||
    sourceOffset < rangeStart ||
    sourceOffset > rangeEnd ||
    !isVirtualTokenSourceRegion(cached, sourceOffset, sourceEndOffset, virtual.languageId)
  ) {
    return;
  }
  addSemanticToken(
    tokens,
    document,
    sourceOffset,
    sourceEndOffset - sourceOffset + 1,
    tokenType,
    tokenModifiers,
  );
}

function isVirtualTokenSourceRegion(
  cached: CachedDocument,
  sourceStart: number,
  sourceEnd: number,
  languageId: VirtualDocument["languageId"],
): boolean {
  const startRegion = findRegionAt(cached.parsed, sourceStart);
  const endRegion = findRegionAt(cached.parsed, sourceEnd);
  return Boolean(
    startRegion && endRegion && startRegion === endRegion && startRegion.language === languageId,
  );
}

function addSemanticToken(
  tokens: SemanticTokenData[],
  document: TextDocument,
  offset: number,
  length: number,
  tokenType: string,
  tokenModifiers?: readonly string[],
): void {
  const position = document.positionAt(offset);
  tokens.push({
    line: position.line,
    character: position.character,
    length,
    tokenType,
    tokenModifiers,
  });
}

function isDiagnostic(value: Diagnostic | undefined): value is Diagnostic {
  return value !== undefined;
}

documents.listen(connection);
connection.listen();
