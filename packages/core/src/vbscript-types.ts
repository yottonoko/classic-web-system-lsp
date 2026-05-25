import type { Diagnostic, Range, TextEdit } from "vscode-languageserver-types";
import type {
  AspLocale,
  AspParsedDocument,
  AspVbscriptComType,
  AspVbscriptIdentifierCase,
  AspVbscriptIdentifierKind,
  VbParameterMode,
} from "./types";

export type VbSymbolKind =
  | "variable"
  | "parameter"
  | "constant"
  | "function"
  | "sub"
  | "class"
  | "method"
  | "field"
  | "property";

export interface VbSymbol {
  name: string;
  kind: VbSymbolKind;
  range: Range;
  sourceUri: string;
  containerName?: string;
  memberOf?: string;
  scopeName?: string;
  scopeRange?: Range;
  typeName?: string;
  type?: VbTypeRef;
  explicitType?: boolean;
  parameters?: string[];
  parameterDetails?: VbParameterInfo[];
  visibility?: "public" | "private";
  procedureKind?: "sub" | "function";
  propertyAccessor?: "get" | "let" | "set";
  parameterMode?: VbParameterMode;
  optional?: boolean;
  implicit?: boolean;
  array?: {
    kind: "fixed" | "dynamic";
    dimensions: string[];
  };
  documentation?: VbDocumentation;
}

export interface VbSemanticToken {
  range: Range;
  tokenType: "variable" | "parameter" | "function" | "class" | "method" | "property" | "operator";
  tokenModifiers?: Array<"public" | "private" | "readonly" | "library" | "byref" | "byval">;
}

export interface VbReference {
  uri: string;
  range: Range;
}

export interface VbReferenceOptions {
  includeDeclaration?: boolean;
  includeFunctionReturnAssignments?: boolean;
}

export interface VbProjectContext {
  symbols?: VbSymbol[];
  documents?: AspParsedDocument[];
  typeChecking?: "basic" | "strict";
  identifierCase?: AspVbscriptIdentifierCase;
  identifierCaseByKind?: Partial<Record<AspVbscriptIdentifierKind, AspVbscriptIdentifierCase>>;
  comTypes?: Record<string, AspVbscriptComType>;
  typeEnvironment?: VbTypeEnvironment;
  unusedDiagnostics?: boolean;
  syntaxSnippets?: boolean;
  locale?: AspLocale;
  debugStep?: <T>(name: string, action: () => T) => T;
}

export interface VbInlayHintOptions {
  variableTypes?: boolean;
  parameterNames?: boolean;
  functionReturnTypes?: boolean;
  implicitByRef?: boolean;
}

export interface VbCallHierarchyData {
  uri: string;
  name: string;
  kind: VbSymbolKind;
  memberOf?: string;
  rootUri?: string;
  line: number;
  character: number;
}

export interface VbTypeRef {
  name: string;
  object?: boolean;
  unionTypes?: VbTypeRef[];
}

export interface VbParameterInfo {
  name: string;
  mode: VbParameterMode;
  optional?: boolean;
}

export interface VbSignatureParameter {
  name: string;
  type?: VbTypeRef;
  mode?: VbParameterMode;
  optional?: boolean;
  documentation?: string;
}

export interface VbSignature {
  parameters: VbSignatureParameter[];
  returnType?: VbTypeRef;
  documentation?: string;
}

export type VbCallSyntaxDiagnosticCode =
  | "callStatementRequiresParentheses"
  | "expressionCallRequiresParentheses"
  | "statementCallDisallowsParenthesizedArguments";

export interface VbMember {
  name: string;
  kind: "field" | "property" | "method" | "event";
  type?: VbTypeRef;
  signature?: VbSignature;
  documentation?: string;
}

export interface VbType {
  name: string;
  kind: "intrinsic" | "classicAsp" | "class" | "com";
  members: VbMember[];
}

export interface VbTypeEnvironment {
  types: VbType[];
  symbols: VbSymbol[];
}

export type VbTypeDiagnostic = Diagnostic;

export interface VbDocumentation {
  summary?: string;
  remarks?: string;
  params: Record<string, string>;
  returns?: string;
  value?: string;
  exceptions: Array<{ cref?: string; text: string }>;
  see: Array<{ cref?: string; href?: string; langword?: string; text?: string }>;
  seealso: Array<{ cref?: string; href?: string; langword?: string; text?: string }>;
  example?: string;
  code?: string;
  ambiguousTarget?: boolean;
}

export interface VbDocumentationQuickAction {
  symbol: VbSymbol;
  edits: TextEdit[];
}
