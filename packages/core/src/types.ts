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
  cache?: AspCacheSettings;
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

export interface AspFormatSettings {
  indentSize?: number;
  indentStyle?: "space" | "tab";
  uppercaseKeywords?: boolean;
  alignAssignments?: boolean;
  onSave?: boolean;
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
  identifierCase?: AspVbscriptIdentifierCase;
  identifierCaseByKind?: Partial<Record<AspVbscriptIdentifierKind, AspVbscriptIdentifierCase>>;
  comTypes?: Record<string, AspVbscriptComType>;
  globals?: Record<string, string | AspVbscriptGlobal>;
  unusedDiagnostics?: boolean;
  includeSuggestions?: boolean;
  syntaxSnippets?: boolean;
}

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
  globalVariableMarkers?: AspInlayHintMarkerMode;
}

export type AspInlayHintMarkerMode = "global" | "local" | "all" | "off";

export interface AspCodeLensSettings {
  references?: boolean;
  includes?: boolean;
  referenceScope?: "analyzed" | "workspace";
}

export interface AspCacheSettings {
  enabled?: boolean;
  directory?: string;
  ttlHours?: number;
  maxSizeMb?: number;
}

export interface AspWorkspaceSettings {
  maxIndexFiles?: number;
  scanChunkSize?: number;
  backgroundAnalysis?: boolean;
  idleAnalysisConcurrency?: number;
  busyAnalysisConcurrency?: number;
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

export interface AspParsedDocument {
  uri: string;
  text: string;
  cst: AspCstNode;
  regions: AspRegion[];
  directives: AspDirective[];
  includes: AspInclude[];
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
  declarationKind?: "dim" | "redim" | "public" | "private" | "const" | "forEach";
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
