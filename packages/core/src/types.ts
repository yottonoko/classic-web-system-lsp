import type { Diagnostic, Position, Range } from "vscode-languageserver-types";

export type AspEmbeddedLanguage =
  | "html"
  | "css"
  | "javascript"
  | "vbscript"
  | "jscript"
  | "asp-directive";

export type AspRegionKind =
  | "html"
  | "asp-block"
  | "asp-expression"
  | "asp-directive"
  | "style"
  | "style-attribute"
  | "client-script"
  | "server-script";

export interface AspSettings {
  locale?: AspLocaleSetting;
  resolvedLocale?: AspLocale;
  defaultLanguage?: "VBScript" | "JScript";
  virtualRoot?: string;
  virtualRoots?: string[];
  windowsPathResolution?: boolean;
  checkJs?: boolean;
  javascript?: AspJavascriptSettings;
  includePaths?: string[];
  legacyEncoding?: AspLegacyEncoding;
  diagnostics?: AspDiagnosticsSettings;
  debug?: AspDebugSettings;
  format?: AspFormatSettings;
  vbscript?: AspVbscriptSettings;
  inlayHints?: AspInlayHintSettings;
  codeLens?: AspCodeLensSettings;
  rename?: AspRenameSettings;
  styleExtraction?: AspStyleExtractionSettings;
  flowchart?: AspFlowchartSettings;
  graph?: AspGraphSettings;
  excel?: AspExcelSettings;
  cache?: AspCacheSettings;
  network?: AspNetworkSettings;
  workspace?: AspWorkspaceSettings;
}

export interface AspIncrementalChange {
  range: Range;
  text: string;
  rangeOffset?: number;
  rangeLength?: number;
}

export type AspEditImpactKind = "incremental" | "full";

export interface AspEditImpact {
  kind: AspEditImpactKind;
  reason: string;
  startOffset: number;
  endOffset: number;
  insertedLength: number;
  deletedLength: number;
  delta: number;
  language?: AspEmbeddedLanguage | "mixed";
}

export interface AspIncrementalUpdateResult {
  parsed: AspParsedDocument;
  impact: AspEditImpact;
}

export type AspLocaleSetting = "auto" | AspLocale;

export type AspLocale = "en" | "ja";

export type AspLegacyEncoding = "auto" | "utf8" | "shift_jis" | "cp932";

export type AspVbscriptBlockIndent = "alignWithDelimiter" | "indentInsideDelimiter";

export interface AspFormatSettings {
  indentSize?: number;
  indentStyle?: "space" | "tab";
  uppercaseKeywords?: boolean;
  alignAssignments?: boolean;
  onSave?: boolean;
  vbscriptBlockIndent?: AspVbscriptBlockIndent;
  ignoreVbscriptTagIndent?: boolean;
  ignoreCssTagIndent?: boolean;
  ignoreJavaScriptTagIndent?: boolean;
}

export interface AspDiagnosticsSettings {
  debounceMs?: number;
}

export type AspDebugOutputLevel = "off" | "summary" | "verbose";

export interface AspDebugSettings {
  output?: AspDebugOutputLevel;
}

export interface AspFormattingOptions extends AspFormatSettings {
  tabSize: number;
  insertSpaces: boolean;
}

export interface AspVbscriptSettings {
  typeChecking?: "basic" | "strict";
  ifSyntaxDiagnostics?: AspVbscriptIfSyntaxDiagnostics;
  identifierCase?: AspVbscriptIdentifierCase;
  identifierCaseByKind?: Partial<Record<AspVbscriptIdentifierKind, AspVbscriptIdentifierCase>>;
  comTypes?: Record<string, AspVbscriptComType>;
  globals?: Record<string, string | AspVbscriptGlobal>;
  unusedDiagnostics?: boolean;
  deadCodeDiagnostics?: boolean;
  syntaxSnippets?: boolean;
  syntaxKeywords?: boolean;
  showUnresolvedSymbolsInCompletion?: boolean;
  initializedDimQuickFixStyle?: AspVbscriptInitializedDimQuickFixStyle;
}

export type AspVbscriptIfSyntaxDiagnostics = "off" | "basic" | "strict";

export type AspVbscriptInitializedDimQuickFixStyle = "newline" | "sameLineColon";

export type AspVbscriptIdentifierCase =
  | "PascalCase"
  | "UPPERCASE"
  | "camelCase"
  | "lowercase"
  | "snake_case"
  | "UPPER_SNAKE"
  | "ignore";

export type AspVbscriptIdentifierKind =
  | "variable"
  | "parameter"
  | "class"
  | "function"
  | "sub"
  | "constant"
  | "field"
  | "property"
  | "method";

export interface AspJavascriptSettings {
  unusedDiagnostics?: boolean;
  autoImports?: boolean;
  ignoreProjectConfig?: boolean;
}

export interface AspInlayHintSettings {
  variableTypes?: boolean;
  parameterNames?: boolean;
  functionReturnTypes?: boolean;
  implicitByRef?: boolean;
  scopeMarkers?: AspInlayHintScopeMarkerSettings;
}

export interface AspInlayHintScopeMarkerSettings {
  global?: boolean;
  local?: boolean;
  uncertain?: boolean;
}

export interface AspCodeLensSettings {
  references?: boolean;
  includes?: boolean;
  referenceScope?: "analyzed" | "workspace";
  referenceProcedures?: boolean;
  referenceGlobals?: boolean;
  referenceClasses?: boolean;
  referenceClassMembers?: boolean;
  includeRelatedIncludeTreesForUnresolved?: boolean;
}

export interface AspRenameSettings {
  updateIncludesOnFileRename?: boolean;
  workspaceSymbolRename?: boolean;
}

export interface AspStyleExtractionSettings {
  insertionMode?: "nearby" | "reuseExistingStyleTag";
}

export interface AspGraphSettings {
  initialViewMode?: "2d" | "3d";
  showRootNodes?: boolean;
  showFileNodes?: boolean;
  showFunctionNodes?: boolean;
  showSubNodes?: boolean;
  showClassNodes?: boolean;
  showMethodNodes?: boolean;
  showMethodFunctionNodes?: boolean;
  showMethodSubNodes?: boolean;
  showPropertyNodes?: boolean;
  showMemberNodes?: boolean;
  showGlobalVariableNodes?: boolean;
  showGlobalConstantNodes?: boolean;
  showLocalVariableNodes?: boolean;
  showLocalConstantNodes?: boolean;
  showParameterNodes?: boolean;
  showUnresolvedNodes?: boolean;
  hideSingleNodes?: boolean;
  hideUnreferencedGlobalSymbols?: boolean;
  showOutgoingSelectionLinks?: boolean;
  showIncludeLinks?: boolean;
  showDeclareLinks?: boolean;
  showReferenceLinks?: boolean;
  showAssignmentLinks?: boolean;
  showCallLinks?: boolean;
  showUnresolvedLinks?: boolean;
  showMemberLinks?: boolean;
  showIncomingDocumentIncludes?: boolean;
  showIncomingFolderIncludes?: boolean;
  includeRelatedIncludeTreesForUnresolved?: boolean;
  useReverseIncludeIndex?: boolean;
  maxDocuments?: number;
  maxTextLength?: number;
  includeTreeMaxDocuments?: number;
  includeTreeMaxTextLength?: number;
}

export interface AspExcelSettings {
  includeRelatedIncludeTreesForUnresolved?: boolean;
  maxDocuments?: number;
  maxTextLength?: number;
  includeTreeMaxDocuments?: number;
  includeTreeMaxTextLength?: number;
}

export interface AspNetworkSettings {
  profile?: "auto" | "local" | "network";
  statCacheTtlMs?: number;
  readdirCacheTtlMs?: number;
  includeReadConcurrency?: number;
  caseResolution?: "auto" | "full" | "fast";
}

export interface AspFlowchartBuildOptions {
  fileName?: string;
  includes?: AspFlowchartInclude[];
  labelLineLength?: number;
  labelMode?: AspFlowchartLabelMode;
  locale?: AspLocale;
  symbols?: AspFlowchartSymbolDocument[];
}

export interface AspFlowchartSettings {
  labelLineLength?: number;
  labelMode?: AspFlowchartLabelMode;
}

export type AspFlowchartLabelMode = "normal" | "raw" | "description";

export interface AspFlowchartPayload {
  uri: string;
  fileName?: string;
  labelMode?: AspFlowchartLabelMode;
  sourceText?: string;
  sections: AspFlowchartSection[];
  nodes: AspFlowchartNode[];
  edges: AspFlowchartEdge[];
  includes: AspFlowchartInclude[];
  mermaid: string;
  stats: {
    sections: number;
    nodes: number;
    edges: number;
    includes: number;
  };
}

export interface AspFlowchartSection {
  id: string;
  label: string;
  kind: "topLevel" | "class" | "procedure" | "property";
  range?: Range;
  nodeIds: string[];
}

export type AspFlowchartNodeKind =
  | "start"
  | "end"
  | "if"
  | "elseif"
  | "else"
  | "select"
  | "case"
  | "for"
  | "forEach"
  | "do"
  | "while"
  | "call"
  | "declaration"
  | "exceptionHandling"
  | "exit"
  | "statement";

export interface AspFlowchartNode {
  id: string;
  sectionId: string;
  kind: AspFlowchartNodeKind;
  label: string;
  description?: string;
  links?: AspFlowchartNodeLink[];
  range?: Range;
}

export interface AspFlowchartNodeLink {
  id: string;
  label: string;
  role: "read" | "write" | "call" | "new" | "member" | "definition" | "unknown";
  symbolKind?: string;
  target: AspFlowchartTarget;
}

export interface AspFlowchartTarget {
  uri: string;
  range?: Range;
  nameRange?: Range;
}

export interface AspFlowchartEdge {
  id: string;
  sectionId: string;
  source: string;
  target: string;
  label?: string;
}

export interface AspFlowchartSymbolDocument {
  uri: string;
  declarations: AspFlowchartSymbolDeclaration[];
  references?: AspFlowchartSymbolReference[];
  callSites?: AspFlowchartCallSite[];
}

export interface AspFlowchartSymbolDeclaration {
  id: string;
  name: string;
  normalizedName: string;
  kind: string;
  range: Range;
  nameRange: Range;
  sourceRange?: Range;
  scopeId?: string;
  parentId?: string;
  memberOf?: string;
  bindingScope?: string;
  procedureKind?: string;
  typeName?: string;
  implicit?: boolean;
  implicitGlobal?: boolean;
  implicitGlobalCandidate?: boolean;
}

export interface AspFlowchartSymbolReference {
  name: string;
  normalizedName: string;
  range: Range;
  scopeId?: string;
  resolvedId?: string;
  role: "read" | "write" | "call" | "new" | "member" | "unknown";
  expectedKinds?: string[];
  baseName?: string;
  memberName?: string;
}

export interface AspFlowchartCallSite {
  name: string;
  normalizedName: string;
  range: Range;
  scopeId?: string;
  receiverName?: string;
  memberName?: string;
  callKind: "procedure" | "function" | "constructor" | "member" | "unknown";
  argumentCount?: number;
  resolvedId?: string;
}

export interface AspFlowchartInclude {
  path: string;
  mode: AspInclude["mode"];
  range: Range;
  exists?: boolean;
  resolvedUri?: string;
  actualPath?: string;
  pathCaseMatches?: boolean;
}

export interface AspCacheSettings {
  enabled?: boolean;
  directory?: string;
  freshness?: "auto" | "metadata" | "watch";
  ttlHours?: number;
  maxSizeMb?: number;
}

export interface AspWorkspaceSettings {
  includes?: string[];
  excludes?: string[];
  respectGitIgnore?: boolean;
  maxIndexFiles?: number;
  scanChunkSize?: number;
  busyAnalysisConcurrency?: number;
  vbProjectMaxDocuments?: number;
  vbProjectMaxTextLength?: number;
}

export interface AspVbscriptComType {
  members?: Record<string, string | AspVbscriptComMember>;
}

export interface AspVbscriptComMember {
  kind?: "field" | "property" | "method";
  type?: string;
  returnType?: string;
  parameters?: Array<string | AspVbscriptComParameter>;
}

export interface AspVbscriptGlobal {
  type?: string;
  kind?: "variable" | "constant";
}

export interface AspVbscriptComParameter {
  name: string;
  type?: string;
}

export interface AspRegion {
  kind: AspRegionKind;
  language: AspEmbeddedLanguage;
  start: number;
  end: number;
  contentStart: number;
  contentEnd: number;
  attributes?: Record<string, string | true>;
}

export interface AspDirective {
  range: Range;
  offset: number;
  name: string;
  attributes: Record<string, string | true>;
}

export interface AspInclude {
  range: Range;
  offset: number;
  path: string;
  mode: "file" | "virtual";
  directiveRange: Range;
  modeRange: Range;
  pathRange: Range;
}

export interface AspServerObject {
  range: Range;
  offset: number;
  id: string;
  idRange: Range;
  progId?: string;
  progIdRange?: Range;
  classId?: string;
  classIdRange?: Range;
  attributes: Record<string, string | true>;
}

export interface AspParsedDocument {
  uri: string;
  text: string;
  cst: AspCstNode;
  regions: AspRegion[];
  directives: AspDirective[];
  includes: AspInclude[];
  serverObjects: AspServerObject[];
  defaultLanguage: "VBScript" | "JScript";
  diagnostics: Diagnostic[];
}

export interface SourceMapSegment {
  virtualStart: number;
  virtualEnd: number;
  sourceStart: number;
  sourceEnd: number;
}

export interface SourceMap {
  segments: SourceMapSegment[];
  toSourceOffset(offset: number): number | undefined;
  toVirtualOffset(offset: number): number | undefined;
  toSourcePosition(position: Position): Position | undefined;
  toVirtualPosition(position: Position): Position | undefined;
}

export interface VirtualDocument {
  uri: string;
  languageId: AspEmbeddedLanguage;
  text: string;
  sourceMap: SourceMap;
}

export type TriviaKind = "whitespace" | "comment" | "newline";

export interface Trivia {
  kind: TriviaKind;
  start: number;
  end: number;
  text: string;
}

export interface ParseError {
  message: string;
  start: number;
  end: number;
}

export type AspTokenKind =
  | "text"
  | "aspOpen"
  | "aspExpressionOpen"
  | "aspDirectiveOpen"
  | "aspClose"
  | "tagOpen"
  | "tagClose"
  | "tagName"
  | "attributeName"
  | "attributeEquals"
  | "attributeValue"
  | "includeDirective";

export interface AspToken {
  kind: AspTokenKind;
  start: number;
  end: number;
  text: string;
  leadingTrivia?: Trivia[];
  trailingTrivia?: Trivia[];
}

export type AspCstNodeKind =
  | "Document"
  | "HtmlText"
  | "AspBlock"
  | "AspExpression"
  | "AspDirective"
  | "IncludeDirective"
  | "StyleElement"
  | "ClientScriptElement"
  | "ServerScriptElement"
  | "StyleAttribute";

export interface AspCstNode {
  kind: AspCstNodeKind;
  start: number;
  end: number;
  contentStart: number;
  contentEnd: number;
  language?: AspEmbeddedLanguage;
  text?: string;
  tokens: AspToken[];
  children: AspCstNode[];
  attributes?: Record<string, string | true>;
  regionKind?: AspRegionKind;
  directive?: AspDirective;
  include?: AspInclude;
  serverObjects?: AspServerObject[];
  vbscript?: VbCstNode;
  errors?: ParseError[];
}

export type VbTokenKind =
  | "identifier"
  | "keyword"
  | "string"
  | "number"
  | "symbol"
  | "comment"
  | "whitespace"
  | "newline"
  | "unknown";

export interface VbToken {
  kind: VbTokenKind;
  start: number;
  end: number;
  text: string;
  value?: string;
}

export type VbParameterMode = "byref" | "byval";

export interface VbParameterMetadata {
  token: VbToken;
  mode: VbParameterMode;
  modeExplicit: boolean;
  optional: boolean;
}

export interface VbArrayDeclaration {
  name: VbToken;
  kind: "fixed" | "dynamic";
  dimensions: string[];
}

export type VbCstNodeKind =
  | "Document"
  | "Class"
  | "Procedure"
  | "Property"
  | "VariableDeclaration"
  | "ConstantDeclaration"
  | "For"
  | "ForEach"
  | "With"
  | "If"
  | "Select"
  | "DoLoop"
  | "While"
  | "Call"
  | "Assignment"
  | "Expression"
  | "SetNew"
  | "CreateObject";

export interface VbCstNode {
  kind: VbCstNodeKind;
  start: number;
  end: number;
  contentStart?: number;
  contentEnd?: number;
  nameToken?: VbToken;
  tokens: VbToken[];
  children: VbCstNode[];
  procedureKind?: "sub" | "function" | "property";
  propertyAccessor?: "get" | "let" | "set";
  declarationKind?: "dim" | "redim" | "public" | "private" | "const" | "for" | "forEach";
  visibility?: "public" | "private";
  identifiers?: VbToken[];
  arrayDeclarations?: VbArrayDeclaration[];
  parameters?: VbToken[];
  parameterMetadata?: VbParameterMetadata[];
  typeName?: string;
  memberOf?: string;
  scopeName?: string;
  scopeStart?: number;
  scopeEnd?: number;
  errors?: ParseError[];
}
