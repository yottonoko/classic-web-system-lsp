#!/usr/bin/env node
import fs from "node:fs";
import path from "node:path";
import {
  CodeActionKind,
  CompletionItemKind,
  createConnection,
  DiagnosticSeverity,
  DocumentSymbol,
  FoldingRange,
  Hover,
  InitializeParams,
  InitializeResult,
  Location,
  ProposedFeatures,
  ReferenceParams,
  SemanticTokensBuilder,
  SymbolInformation,
  SymbolKind,
  TextDocumentPositionParams,
  TextDocuments,
  TextDocumentSyncKind,
} from "vscode-languageserver/node";
import type {
  CallHierarchyIncomingCall,
  CallHierarchyItem,
  CallHierarchyOutgoingCall,
  CodeAction,
  CodeLens,
  Color,
  ColorInformation,
  ColorPresentation,
  CompletionItem,
  Diagnostic,
  DocumentLink,
  DocumentHighlight,
  InlayHint,
  LinkedEditingRanges,
  Position,
  Range,
  RenameParams,
  SemanticTokens,
  SelectionRange,
  SignatureHelp,
  TextEdit,
  WorkspaceEdit,
} from "vscode-languageserver/node";
import { TextDocument } from "vscode-languageserver-textdocument";
import {
  analyzeVbscript,
  buildVirtualDocuments,
  collectVbscriptSymbols,
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
  getVbscriptSignatureHelp,
  getVbscriptTypeDefinition,
  parseAspDocument,
  prepareVbscriptCallHierarchy,
  resolveVbscriptCompletionItem,
  type AspFormattingOptions,
  type AspParsedDocument,
  type AspSettings,
  type AspRegion,
  type VirtualDocument,
  type VbProjectContext,
  type VbSymbolKind,
} from "@asp-lsp/core";
import { getCSSLanguageService } from "vscode-css-languageservice";
import {
  getLanguageService as getHtmlLanguageService,
  TokenType,
} from "vscode-html-languageservice";
import ts from "typescript";

const connection = createConnection(ProposedFeatures.all);
const documents = new TextDocuments(TextDocument);
const htmlService = getHtmlLanguageService();
const cssService = getCSSLanguageService();
const settingsByUri = new Map<string, AspSettings>();
const includeDocumentCache = new Map<string, { mtimeMs: number; parsed: AspParsedDocument }>();
let globalSettings: AspSettings = { defaultLanguage: "VBScript", checkJs: false };
let workspaceRoots: string[] = [];

const semanticTokenTypes = [
  "keyword",
  "variable",
  "function",
  "class",
  "method",
  "property",
  "comment",
] as const;

interface CachedDocument {
  source: TextDocument;
  parsed: AspParsedDocument;
  virtuals: Map<string, VirtualDocument>;
}

interface OffsetEdit {
  start: number;
  end: number;
  newText: string;
}

const cache = new Map<string, CachedDocument>();

connection.onInitialize((params: InitializeParams): InitializeResult => {
  workspaceRoots = [
    ...(params.workspaceFolders?.map((folder) => uriToFileName(folder.uri)) ?? []),
    ...(params.rootUri ? [uriToFileName(params.rootUri)] : []),
  ].filter((root, index, roots) => root.length > 0 && roots.indexOf(root) === index);
  return {
    capabilities: {
      textDocumentSync: TextDocumentSyncKind.Incremental,
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
      documentLinkProvider: { resolveProvider: false },
      codeActionProvider: { codeActionKinds: [CodeActionKind.QuickFix] },
      codeLensProvider: { resolveProvider: false },
      colorProvider: true,
      selectionRangeProvider: true,
      linkedEditingRangeProvider: true,
      inlayHintProvider: { resolveProvider: false },
      callHierarchyProvider: true,
      semanticTokensProvider: {
        legend: { tokenTypes: [...semanticTokenTypes], tokenModifiers: [] },
        full: { delta: true },
        range: true,
      },
      documentFormattingProvider: true,
      documentRangeFormattingProvider: true,
      documentOnTypeFormattingProvider: {
        firstTriggerCharacter: "\n",
        moreTriggerCharacter: [">"],
      },
    },
  };
});

documents.onDidOpen((event) => validate(event.document));
documents.onDidChangeContent((event) => validate(event.document));
documents.onDidClose((event) => {
  cache.delete(event.document.uri);
  connection.sendDiagnostics({ uri: event.document.uri, diagnostics: [] });
});

connection.onInitialized(() => {
  void refreshConfiguration();
});

connection.onDidChangeConfiguration((change) => {
  const incoming = readSettingsFromChange(change.settings);
  if (incoming) {
    globalSettings = normalizeSettings(incoming);
  }
  settingsByUri.clear();
  cache.clear();
  includeDocumentCache.clear();
  for (const document of documents.all()) {
    validate(document);
  }
});

connection.onDidChangeWatchedFiles(() => {
  includeDocumentCache.clear();
  cache.clear();
  for (const document of documents.all()) {
    validate(document);
  }
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
  if (region.language === "vbscript" || region.language === "jscript") {
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
    return htmlService.doComplete(
      virtualDocument,
      params.position,
      htmlService.parseHTMLDocument(virtualDocument),
    ).items;
  }
  if (region.language === "css") {
    return cssCompletion(cached, params, "css");
  }
  if (region.language === "javascript") {
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
  if (region.language === "vbscript" || region.language === "jscript") {
    return aspHover(cached, params);
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
  const cached = getCached(params.textDocument.uri);
  if (!cached) {
    return null;
  }
  const region = findRegionAt(cached.parsed, cached.source.offsetAt(params.position));
  if (!region || (region.language !== "vbscript" && region.language !== "jscript")) {
    return null;
  }
  const symbol = getVbscriptDefinition(
    cached.parsed,
    params.position,
    buildVbProjectContext(cached, cachedSettings(cached.source.uri)),
  );
  return symbol ? Location.create(symbol.sourceUri, symbol.range) : null;
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
  if (!region || (region.language !== "vbscript" && region.language !== "jscript")) {
    return [];
  }
  return getVbscriptReferences(
    cached.parsed,
    params.position,
    buildVbProjectContext(cached, cachedSettings(cached.source.uri)),
  ).map((reference) => Location.create(reference.uri, reference.range));
});

connection.onPrepareRename((params) => {
  const cached = getCached(params.textDocument.uri);
  if (!cached || !isVbscriptPosition(cached, params.position)) {
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
  if (!cached || !isVbscriptPosition(cached, params.position)) {
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
  if (!cached || !isVbscriptPosition(cached, params.position)) {
    return [];
  }
  return getVbscriptDocumentHighlights(
    cached.parsed,
    params.position,
    buildVbProjectContext(cached, cachedSettings(cached.source.uri)),
  );
});

connection.onSignatureHelp((params): SignatureHelp | null => {
  const cached = getCached(params.textDocument.uri);
  if (!cached || !isVbscriptPosition(cached, params.position)) {
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

connection.onWorkspaceSymbol((params) => {
  const query = params.query.toLowerCase();
  const symbols = documents
    .all()
    .flatMap((document) => {
      const cached = getCached(document.uri);
      return cached
        ? (buildVbProjectContext(cached, cachedSettings(document.uri)).symbols ?? [])
        : [];
    })
    .filter((symbol) => query.length === 0 || symbol.name.toLowerCase().includes(query));
  return symbols.map((symbol) =>
    SymbolInformation.create(
      symbol.name,
      vbWorkspaceSymbolKind(symbol.kind),
      symbol.range,
      symbol.sourceUri,
      symbol.memberOf,
    ),
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
  return [...htmlSymbols, ...getVbscriptDocumentSymbols(cached.parsed)];
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
  return [...htmlRanges, ...aspRanges];
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
    return { range: include.range, target: pathToFileUri(targetPath) };
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
  return getVbscriptInlayHints(
    cached.parsed,
    params.range,
    buildVbProjectContext(cached, cachedSettings(cached.source.uri)),
    cachedSettings(cached.source.uri).inlayHints,
  );
});

connection.languages.callHierarchy.onPrepare((params): CallHierarchyItem[] => {
  const cached = getCached(params.textDocument.uri);
  if (!cached || !isVbscriptPosition(cached, params.position)) {
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

connection.languages.onLinkedEditingRange((params): LinkedEditingRanges | null => {
  const cached = getCached(params.textDocument.uri);
  if (!cached) {
    return null;
  }
  const region = findRegionAt(cached.parsed, cached.source.offsetAt(params.position));
  if (!region || region.language !== "html") {
    return null;
  }
  const virtual = cached.virtuals.get("html");
  if (!virtual) {
    return null;
  }
  const doc = toTextDocument(virtual);
  const ranges = htmlService.findLinkedEditingRanges(
    doc,
    params.position,
    htmlService.parseHTMLDocument(doc),
  );
  return ranges ? { ranges } : null;
});

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
  return params.context.diagnostics.flatMap((diagnostic) =>
    quickFixesForDiagnostic(cached, diagnostic),
  );
});

connection.onDocumentOnTypeFormatting((params) => {
  const cached = getCached(params.textDocument.uri);
  if (!cached) {
    return [];
  }
  return onTypeFormatting(cached, params.position, params.ch);
});

connection.languages.semanticTokens.on((params) => {
  const cached = getCached(params.textDocument.uri);
  if (!cached) {
    return { data: [] };
  }
  return buildSemanticTokens(cached);
});

connection.languages.semanticTokens.onRange((params): SemanticTokens => {
  const cached = getCached(params.textDocument.uri);
  if (!cached) {
    return { data: [] };
  }
  return buildSemanticTokens(cached, params.range);
});

connection.languages.semanticTokens.onDelta((params): SemanticTokens => {
  const cached = getCached(params.textDocument.uri);
  if (!cached) {
    return { data: [] };
  }
  return buildSemanticTokens(cached);
});

async function validate(document: TextDocument): Promise<void> {
  const settings = cachedSettings(document.uri);
  const parsed = parseAspDocument(document.uri, document.getText(), settings);
  const virtuals = buildVirtualDocuments(parsed);
  const cached = { source: document, parsed, virtuals };
  cache.set(document.uri, cached);
  const diagnostics: Diagnostic[] = [
    ...parsed.diagnostics,
    ...analyzeVbscript(parsed, buildVbProjectContext(cached, settings)).diagnostics,
    ...includeDiagnostics(cached, settings),
  ];
  if (!isIncDocument(document.uri)) {
    diagnostics.push(...htmlDiagnostics(cached));
  }
  diagnostics.push(...cssDiagnostics(cached));
  diagnostics.push(...jsDiagnostics(cached, settings));
  connection.sendDiagnostics({ uri: document.uri, diagnostics });
}

function htmlDiagnostics(cached: CachedDocument): Diagnostic[] {
  const virtual = cached.virtuals.get("html");
  if (!virtual) {
    return [];
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
  return diagnostics;
}

function cssDiagnostics(cached: CachedDocument): Diagnostic[] {
  const virtual = cached.virtuals.get("css");
  if (!virtual) {
    return [];
  }
  const doc = toTextDocument(virtual);
  return cssService
    .doValidation(doc, cssService.parseStylesheet(doc))
    .map((diagnostic) => remapDiagnostic(virtual, diagnostic, "asp-lsp-css"))
    .filter(isDiagnostic);
}

function jsDiagnostics(cached: CachedDocument, settings: AspSettings): Diagnostic[] {
  const virtual = cached.virtuals.get("javascript");
  if (!virtual) {
    return [];
  }
  const host = createJsLanguageHost(virtual, settings);
  const service = ts.createLanguageService(host);
  const fileName = jsVirtualFileName(virtual.uri);
  const syntactic = service.getSyntacticDiagnostics(fileName);
  const semantic = settings.checkJs ? service.getSemanticDiagnostics(fileName) : [];
  return [...syntactic, ...semantic]
    .map((diagnostic) => tsDiagnosticToLsp(virtual, diagnostic))
    .filter(isDiagnostic);
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
  const virtual = cached.virtuals.get("javascript");
  if (!virtual) {
    return [];
  }
  const virtualPosition = virtual.sourceMap.toVirtualPosition(params.position);
  if (!virtualPosition) {
    return [];
  }
  const fileName = jsVirtualFileName(virtual.uri);
  const service = ts.createLanguageService(
    createJsLanguageHost(virtual, cachedSettings(cached.source.uri)),
  );
  const offset = toTextDocument(virtual).offsetAt(virtualPosition);
  return (
    service.getCompletionsAtPosition(fileName, offset, {})?.entries.map((entry) => ({
      label: entry.name,
      kind: tsCompletionKind(entry.kind),
      detail: entry.kind,
      data: {
        kind: "javascript",
        uri: cached.source.uri,
        name: entry.name,
        virtualOffset: offset,
        source: entry.source,
        tsData: entry.data,
      },
    })) ?? []
  );
}

function resolveJsCompletion(item: CompletionItem, uri: string): CompletionItem | undefined {
  const cached = getCached(uri);
  const data = item.data as
    | {
        name?: string;
        virtualOffset?: number;
        source?: string;
        tsData?: ts.CompletionEntryData;
      }
    | undefined;
  const virtual = cached?.virtuals.get("javascript");
  if (!cached || !virtual || typeof data?.virtualOffset !== "number" || !data.name) {
    return undefined;
  }
  const service = ts.createLanguageService(createJsLanguageHost(virtual, cachedSettings(uri)));
  const details = service.getCompletionEntryDetails(
    jsVirtualFileName(virtual.uri),
    data.virtualOffset,
    data.name,
    undefined,
    data.source,
    undefined,
    data.tsData,
  );
  if (!details) {
    return item;
  }
  return {
    ...item,
    detail: ts.displayPartsToString(details.displayParts),
    documentation: ts.displayPartsToString(details.documentation),
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

function definitionLikeLocation(
  uri: string,
  position: Position,
  mode: "declaration" | "typeDefinition" | "implementation",
): Location | Location[] | null {
  const cached = getCached(uri);
  if (!cached) {
    return null;
  }
  const region = findRegionAt(cached.parsed, cached.source.offsetAt(position));
  if (!region) {
    return null;
  }
  if (region.language === "vbscript" || region.language === "jscript") {
    const context = buildVbProjectContext(cached, cachedSettings(cached.source.uri));
    const symbol =
      mode === "typeDefinition"
        ? getVbscriptTypeDefinition(cached.parsed, position, context)
        : mode === "implementation"
          ? getVbscriptImplementation(cached.parsed, position, context)
          : getVbscriptDefinition(cached.parsed, position, context);
    return symbol ? Location.create(symbol.sourceUri, symbol.range) : null;
  }
  if (region.language === "javascript") {
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

function jsLocations(
  cached: CachedDocument,
  position: Position,
  mode: "declaration" | "typeDefinition" | "implementation",
): Location[] {
  const virtual = cached.virtuals.get("javascript");
  const virtualPosition = virtual?.sourceMap.toVirtualPosition(position);
  if (!virtual || !virtualPosition) {
    return [];
  }
  const doc = toTextDocument(virtual);
  const offset = doc.offsetAt(virtualPosition);
  const fileName = jsVirtualFileName(virtual.uri);
  const service = ts.createLanguageService(
    createJsLanguageHost(virtual, cachedSettings(cached.source.uri)),
  );
  const definitions =
    mode === "typeDefinition"
      ? service.getTypeDefinitionAtPosition(fileName, offset)
      : mode === "implementation"
        ? service.getImplementationAtPosition(fileName, offset)
        : service.getDefinitionAtPosition(fileName, offset);
  return (
    definitions
      ?.map((definition) => tsDefinitionToLocation(virtual, definition))
      .filter((location): location is Location => Boolean(location)) ?? []
  );
}

function tsDefinitionToLocation(
  virtual: VirtualDocument,
  definition: ts.DefinitionInfo | ts.ImplementationLocation,
): Location | undefined {
  if (definition.fileName !== jsVirtualFileName(virtual.uri)) {
    return undefined;
  }
  const doc = toTextDocument(virtual);
  const start = doc.positionAt(definition.textSpan.start);
  const end = doc.positionAt(definition.textSpan.start + definition.textSpan.length);
  const sourceStart = virtual.sourceMap.toSourcePosition(start);
  const sourceEnd = virtual.sourceMap.toSourcePosition(end);
  return sourceStart && sourceEnd
    ? Location.create(virtual.uri.replace(/\.javascript\.virtual$/, ""), {
        start: sourceStart,
        end: sourceEnd,
      })
    : undefined;
}

function remapLocation(virtual: VirtualDocument, location: Location): Location | undefined {
  const start = virtual.sourceMap.toSourcePosition(location.range.start);
  const end = virtual.sourceMap.toSourcePosition(location.range.end);
  return start && end
    ? Location.create(virtual.uri.replace(`.${virtual.languageId}.virtual`, ""), { start, end })
    : undefined;
}

function buildVbProjectContext(cached: CachedDocument, settings: AspSettings): VbProjectContext {
  const documents = collectVbProjectDocuments(cached.parsed, settings);
  const contextSettings = {
    typeChecking: settings.vbscript?.typeChecking,
    comTypes: settings.vbscript?.comTypes,
  };
  const symbols = documents.flatMap((document) =>
    collectVbscriptSymbols(document, contextSettings),
  );
  return {
    documents,
    symbols,
    ...contextSettings,
  };
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

function readTextFile(fileName: string, encoding: string | undefined): string {
  const normalized = (encoding ?? "utf8").toLowerCase().replace(/[-_]/g, "");
  if (normalized === "shiftjis" || normalized === "sjis" || normalized === "cp932") {
    return new TextDecoder("shift_jis").decode(fs.readFileSync(fileName));
  }
  return fs.readFileSync(fileName, "utf8");
}

function includeDiagnostics(cached: CachedDocument, settings: AspSettings): Diagnostic[] {
  const diagnostics: Diagnostic[] = [];
  const owner = uriToFileName(cached.source.uri);
  for (const include of cached.parsed.includes) {
    const resolved = resolveIncludePath(cached.source.uri, include.path, include.mode, settings);
    if (!resolved || !fs.existsSync(resolved)) {
      diagnostics.push({
        severity: DiagnosticSeverity.Warning,
        range: include.range,
        message: `Include file '${include.path}' could not be resolved.`,
        source: "asp-lsp-include",
      });
    }
    if (sameFile(resolved, owner)) {
      diagnostics.push({
        severity: DiagnosticSeverity.Warning,
        range: include.range,
        message: "Include file references the current document.",
        source: "asp-lsp-include",
      });
      continue;
    }
    const cycle = findIncludeCycle(owner, resolved, settings);
    if (cycle) {
      diagnostics.push({
        severity: DiagnosticSeverity.Warning,
        range: include.range,
        message: `Include cycle detected: ${cycle.map((fileName) => path.basename(fileName)).join(" -> ")}.`,
        source: "asp-lsp-include",
      });
    }
  }
  return diagnostics;
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
  const virtual = cached.virtuals.get("javascript");
  const virtualPosition = virtual?.sourceMap.toVirtualPosition(position);
  if (!virtual || !virtualPosition) {
    return undefined;
  }
  const doc = toTextDocument(virtual);
  const selection = ts
    .createLanguageService(createJsLanguageHost(virtual, cachedSettings(cached.source.uri)))
    .getSmartSelectionRange(jsVirtualFileName(virtual.uri), doc.offsetAt(virtualPosition));
  return remapTsSelectionRange(virtual, selection);
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

function tsDiagnosticToLsp(
  virtual: VirtualDocument,
  diagnostic: ts.Diagnostic,
): Diagnostic | undefined {
  if (diagnostic.start === undefined || diagnostic.length === undefined) {
    return undefined;
  }
  const virtualDoc = toTextDocument(virtual);
  const start = virtualDoc.positionAt(diagnostic.start);
  const end = virtualDoc.positionAt(diagnostic.start + diagnostic.length);
  const sourceStart = virtual.sourceMap.toSourcePosition(start);
  const sourceEnd = virtual.sourceMap.toSourcePosition(end);
  if (!sourceStart || !sourceEnd) {
    return undefined;
  }
  return {
    severity:
      diagnostic.category === ts.DiagnosticCategory.Error
        ? DiagnosticSeverity.Error
        : DiagnosticSeverity.Warning,
    range: { start: sourceStart, end: sourceEnd },
    message: ts.flattenDiagnosticMessageText(diagnostic.messageText, "\n"),
    source: "asp-lsp-typescript",
  };
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
    defaultLanguage: settings.defaultLanguage === "JScript" ? "JScript" : "VBScript",
    checkJs: settings.checkJs === true,
    virtualRoot:
      typeof settings.virtualRoot === "string" && settings.virtualRoot.length > 0
        ? path.resolve(settings.virtualRoot)
        : undefined,
    virtualRoots,
    includePaths,
    legacyEncoding:
      typeof settings.legacyEncoding === "string" ? settings.legacyEncoding : undefined,
    format: normalizeFormatSettings(settings),
    vbscript: normalizeVbscriptSettings(settings),
    inlayHints: normalizeInlayHintSettings(settings),
    codeLens: normalizeCodeLensSettings(settings),
  };
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
    comTypes:
      record.comTypes && typeof record.comTypes === "object"
        ? (record.comTypes as NonNullable<AspSettings["vbscript"]>["comTypes"])
        : undefined,
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
  };
}

function formatOptions(options: { tabSize: number; insertSpaces: boolean }, settings: AspSettings) {
  return {
    tabSize: options.tabSize,
    insertSpaces: options.insertSpaces,
    ...settings.format,
  };
}

function formatAspDocumentWithDelegates(
  cached: CachedDocument,
  options: { tabSize: number; insertSpaces: boolean },
): TextEdit[] {
  const settings = cachedSettings(cached.source.uri);
  const formattingOptions = formatOptions(options, settings);
  const original = cached.source.getText();
  let formatted = applyTextEdits(original, formatAspDocument(cached.parsed, formattingOptions));
  const parsed = parseAspDocument(cached.source.uri, formatted, settings);
  formatted = applyOffsetEdits(
    formatted,
    javaScriptFormattingEdits(parsed, formatted, formattingOptions),
  );
  return formatted === original
    ? []
    : [
        {
          range: {
            start: cached.source.positionAt(0),
            end: cached.source.positionAt(original.length),
          },
          newText: formatted,
        },
      ];
}

function formatAspRangeWithDelegates(
  cached: CachedDocument,
  range: Range,
  options: { tabSize: number; insertSpaces: boolean },
): TextEdit[] {
  const settings = cachedSettings(cached.source.uri);
  const formattingOptions = formatOptions(options, settings);
  const original = cached.source.getText();
  const rangeStart = lineStartOffset(original, cached.source.offsetAt(range.start));
  const rangeEnd = lineEndOffset(original, cached.source.offsetAt(range.end));
  const coreEdits = formatAspRange(cached.parsed, range, formattingOptions);
  let formatted = applyTextEdits(original, coreEdits);
  let formattedRangeEnd =
    coreEdits.length === 1
      ? rangeStart + coreEdits[0].newText.length
      : rangeEnd + offsetEditsDelta(original, coreEdits);
  const parsed = parseAspDocument(cached.source.uri, formatted, settings);
  const jsEdits = javaScriptFormattingEdits(
    parsed,
    formatted,
    formattingOptions,
    rangeStart,
    formattedRangeEnd,
  );
  formattedRangeEnd += offsetEditsDelta(formatted, jsEdits);
  formatted = applyOffsetEdits(formatted, jsEdits);
  const newText = formatted.slice(rangeStart, formattedRangeEnd);
  const originalText = original.slice(rangeStart, rangeEnd);
  return newText === originalText
    ? []
    : [
        {
          range: {
            start: cached.source.positionAt(rangeStart),
            end: cached.source.positionAt(rangeEnd),
          },
          newText,
        },
      ];
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
    .flatMap((region) => formatJavaScriptRegion(text, region, options, spanStart, spanEnd));
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
      target: ts.ScriptTarget.ES2022,
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

function createJsLanguageHost(
  virtual: VirtualDocument,
  settings: AspSettings,
): ts.LanguageServiceHost {
  const fileName = jsVirtualFileName(virtual.uri);
  return {
    getScriptFileNames: () => [fileName],
    getScriptVersion: () => "0",
    getScriptSnapshot: (requested) =>
      requested === fileName ? ts.ScriptSnapshot.fromString(virtual.text) : undefined,
    getScriptKind: (requested) =>
      requested === fileName ? ts.ScriptKind.JS : ts.ScriptKind.Unknown,
    getCurrentDirectory: () => process.cwd(),
    getCompilationSettings: () => ({
      allowJs: true,
      checkJs: settings.checkJs ?? false,
      noEmit: true,
      target: ts.ScriptTarget.ES2022,
      module: ts.ModuleKind.CommonJS,
    }),
    getDefaultLibFileName: (options) => ts.getDefaultLibFilePath(options),
    fileExists: ts.sys.fileExists,
    readFile: ts.sys.readFile,
    readDirectory: ts.sys.readDirectory,
  };
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
  return region?.language === "vbscript" || region?.language === "jscript";
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
    case "variable":
      return SymbolKind.Variable;
  }
}

function quickFixesForDiagnostic(cached: CachedDocument, diagnostic: Diagnostic): CodeAction[] {
  if (diagnostic.source === "asp-lsp-vbscript") {
    const name = /'([^']+)' is not declared/.exec(diagnostic.message)?.[1];
    if (!name) {
      return [];
    }
    const line = diagnostic.range.start.line;
    return [
      {
        title: `Declare ${name} with Dim`,
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
    ];
  }
  if (diagnostic.source === "asp-lsp-include") {
    const includePath = /'([^']+)'/.exec(diagnostic.message)?.[1];
    if (!includePath) {
      return [];
    }
    return [
      {
        title: `Create missing include ${includePath}`,
        kind: CodeActionKind.QuickFix,
        diagnostics: [diagnostic],
        data: { includePath },
      },
    ];
  }
  return [];
}

function codeLenses(cached: CachedDocument): CodeLens[] {
  const settings = cachedSettings(cached.source.uri).codeLens;
  const context = buildVbProjectContext(cached, cachedSettings(cached.source.uri));
  const lenses: CodeLens[] = [];
  if (settings?.references !== false) {
    for (const symbol of (context.symbols ?? []).filter(
      (item) =>
        item.sourceUri === cached.source.uri &&
        ["function", "sub", "class", "method", "property"].includes(item.kind),
    )) {
      const references = getVbscriptReferences(cached.parsed, symbol.range.start, context);
      lenses.push({
        range: symbol.range,
        command: {
          title: `${references.length} reference${references.length === 1 ? "" : "s"}`,
          command: "",
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
          title: `include ${path.basename(target)}`,
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
): TextEdit[] {
  if (character === ">") {
    return [];
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
  const options = formatOptions(
    { tabSize: 2, insertSpaces: true },
    cachedSettings(cached.source.uri),
  );
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

function lineText(document: TextDocument, line: number): string {
  return document
    .getText({
      start: { line, character: 0 },
      end: { line: line + 1, character: 0 },
    })
    .replace(/\r?\n$/, "");
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
  const tokens: Array<{ line: number; character: number; length: number; tokenType: string }> = [];
  for (const region of cached.parsed.regions) {
    if (region.end < rangeStart || region.start > rangeEnd) {
      continue;
    }
    if (region.kind === "asp-block" || region.kind === "asp-expression") {
      addSemanticToken(tokens, cached.source, region.start, 2, "keyword");
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
  const symbols = collectVbscriptSymbols(cached.parsed);
  for (const symbol of symbols) {
    const offset = cached.source.offsetAt(symbol.range.start);
    if (offset < rangeStart || offset > rangeEnd) {
      continue;
    }
    const tokenType =
      symbol.kind === "class"
        ? "class"
        : symbol.kind === "method"
          ? "method"
          : symbol.kind === "field" || symbol.kind === "property"
            ? "property"
            : symbol.kind === "function" || symbol.kind === "sub"
              ? "function"
              : "variable";
    tokens.push({
      line: symbol.range.start.line,
      character: symbol.range.start.character,
      length: Math.max(1, symbol.range.end.character - symbol.range.start.character),
      tokenType,
    });
  }
  for (const name of ["Request", "Response", "Session", "Application", "Server", "ASPError"]) {
    addWordSemanticTokens(
      tokens,
      cached.source,
      cached.parsed,
      name,
      "variable",
      rangeStart,
      rangeEnd,
    );
  }
  tokens.sort((left, right) => left.line - right.line || left.character - right.character);
  const builder = new SemanticTokensBuilder();
  for (const token of tokens) {
    builder.push(
      token.line,
      token.character,
      token.length,
      semanticTokenTypes.indexOf(token.tokenType as (typeof semanticTokenTypes)[number]),
      0,
    );
  }
  return builder.build();
}

function addSemanticToken(
  tokens: Array<{ line: number; character: number; length: number; tokenType: string }>,
  document: TextDocument,
  offset: number,
  length: number,
  tokenType: string,
): void {
  const position = document.positionAt(offset);
  tokens.push({ line: position.line, character: position.character, length, tokenType });
}

function addWordSemanticTokens(
  tokens: Array<{ line: number; character: number; length: number; tokenType: string }>,
  document: TextDocument,
  parsed: AspParsedDocument,
  word: string,
  tokenType: string,
  rangeStart = 0,
  rangeEnd = parsed.text.length,
): void {
  const pattern = new RegExp(`\\b${word}\\b`, "gi");
  for (const region of parsed.regions) {
    if (region.language !== "vbscript" && region.language !== "jscript") {
      continue;
    }
    const text = parsed.text.slice(region.contentStart, region.contentEnd);
    let match: RegExpExecArray | null;
    while ((match = pattern.exec(text)) !== null) {
      const offset = region.contentStart + match.index;
      if (offset >= rangeStart && offset <= rangeEnd) {
        addSemanticToken(tokens, document, offset, word.length, tokenType);
      }
    }
  }
}

function isDiagnostic(value: Diagnostic | undefined): value is Diagnostic {
  return value !== undefined;
}

documents.listen(connection);
connection.listen();
