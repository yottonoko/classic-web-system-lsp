import {
  CompletionItemKind,
  DiagnosticSeverity,
  DocumentHighlightKind,
  InlayHintKind,
  InsertTextFormat,
  SymbolKind,
} from "vscode-languageserver-types";
import type {
  CallHierarchyIncomingCall,
  CallHierarchyItem,
  CallHierarchyOutgoingCall,
  CompletionItem,
  Diagnostic,
  DocumentHighlight,
  DocumentSymbol,
  InlayHint,
  Position,
  Range,
  SelectionRange,
  SignatureHelp,
} from "vscode-languageserver-types";
import { offsetAt, rangeFromOffsets } from "./position";
import { createLocalizer } from "./localize";
import type {
  AspCstNode,
  AspLocale,
  AspParsedDocument,
  AspRegion,
  AspVbscriptComType,
  AspVbscriptIdentifierCase,
  AspVbscriptIdentifierKind,
  VbCstNode,
  VbParameterMetadata,
  VbParameterMode,
  VbToken,
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
  parameters?: string[];
  parameterDetails?: VbParameterInfo[];
  visibility?: "public" | "private";
  procedureKind?: "sub" | "function";
  propertyAccessor?: "get" | "let" | "set";
  parameterMode?: VbParameterMode;
  optional?: boolean;
  implicit?: boolean;
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

export interface VbProjectContext {
  symbols?: VbSymbol[];
  documents?: AspParsedDocument[];
  typeChecking?: "basic" | "strict";
  identifierCase?: AspVbscriptIdentifierCase;
  identifierCaseByKind?: Partial<Record<AspVbscriptIdentifierKind, AspVbscriptIdentifierCase>>;
  comTypes?: Record<string, AspVbscriptComType>;
  typeEnvironment?: VbTypeEnvironment;
  unusedDiagnostics?: boolean;
  locale?: AspLocale;
}

export interface VbInlayHintOptions {
  variableTypes?: boolean;
  parameterNames?: boolean;
  functionReturnTypes?: boolean;
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
}

export interface VbSignature {
  parameters: VbSignatureParameter[];
  returnType?: VbTypeRef;
}

export interface VbMember {
  name: string;
  kind: "field" | "property" | "method";
  type?: VbTypeRef;
  signature?: VbSignature;
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

interface VbDocElement {
  name: string;
  attributes: Record<string, string>;
  children: Array<VbDocElement | string>;
  selfClosing?: boolean;
}

interface VbDocTagToken {
  kind: "start" | "end";
  name: string;
  selfClosing: boolean;
}

type VbDocXmlToken =
  | {
      kind: "text";
      start: number;
      end: number;
      text: string;
    }
  | {
      kind: "start";
      start: number;
      end: number;
      name: string;
      attributes: Record<string, string>;
      selfClosing: boolean;
    }
  | {
      kind: "end";
      start: number;
      end: number;
      name: string;
    };

const vbDocCommentTags = [
  "summary",
  "remarks",
  "param",
  "returns",
  "value",
  "exception",
  "see",
  "seealso",
  "example",
  "code",
  "c",
  "list",
  "para",
] as const;

const vbDocCommentAttributeCompletions: Record<string, string[]> = {
  param: ["name"],
  exception: ["cref"],
  see: ["cref", "href", "langword"],
  seealso: ["cref", "href", "langword"],
  list: ["type"],
};

function builtinCompletions(locale: AspLocale | undefined): CompletionItem[] {
  const localizer = createLocalizer(locale);
  return [
    {
      label: "Request",
      kind: CompletionItemKind.Variable,
      detail: localizer.t("vb.builtin.request.detail"),
      documentation: localizer.t("vb.builtin.request.documentation"),
    },
    {
      label: "Response",
      kind: CompletionItemKind.Variable,
      detail: localizer.t("vb.builtin.response.detail"),
    },
    {
      label: "Session",
      kind: CompletionItemKind.Variable,
      detail: localizer.t("vb.builtin.session.detail"),
    },
    {
      label: "Application",
      kind: CompletionItemKind.Variable,
      detail: localizer.t("vb.builtin.application.detail"),
    },
    {
      label: "Server",
      kind: CompletionItemKind.Variable,
      detail: localizer.t("vb.builtin.server.detail"),
    },
    {
      label: "ASPError",
      kind: CompletionItemKind.Class,
      detail: localizer.t("vb.builtin.asperror.detail"),
    },
    {
      label: "Option Explicit",
      kind: CompletionItemKind.Keyword,
      detail: localizer.t("vb.builtin.optionExplicit.detail"),
    },
    { label: "Dim", kind: CompletionItemKind.Keyword },
    { label: "Set", kind: CompletionItemKind.Keyword },
    { label: "Const", kind: CompletionItemKind.Keyword },
    { label: "Sub", kind: CompletionItemKind.Keyword },
    { label: "Function", kind: CompletionItemKind.Keyword },
    { label: "Class", kind: CompletionItemKind.Keyword },
    ...builtinFunctions.map(
      (item): CompletionItem => ({
        label: item.label,
        kind: CompletionItemKind.Function,
        detail: `Function ${item.signature} As ${item.returnType}`,
        documentation: item.documentation,
      }),
    ),
  ];
}

function builtinDescription(name: string, locale: AspLocale | undefined): string | undefined {
  const key = `vb.hover.builtin.${name.toLowerCase()}` as const;
  if (
    key === "vb.hover.builtin.request" ||
    key === "vb.hover.builtin.response" ||
    key === "vb.hover.builtin.session" ||
    key === "vb.hover.builtin.application" ||
    key === "vb.hover.builtin.server" ||
    key === "vb.hover.builtin.asperror"
  ) {
    return markdownHover(`Dim ${name} As ${name}`, createLocalizer(locale).t(key));
  }
  const builtin = builtinFunction(name);
  if (builtin) {
    return markdownHover(
      `Function ${builtin.signature} As ${builtin.returnType}`,
      `VBScript built-in function. ${builtin.documentation}`,
    );
  }
  return undefined;
}

const classicAspBuiltinSignatures: Record<string, string[]> = {
  "response.write": ["Response.Write value"],
  "response.redirect": ["Response.Redirect url"],
  "response.end": ["Response.End"],
  "response.flush": ["Response.Flush"],
  "response.clear": ["Response.Clear"],
  "request.querystring": ["Request.QueryString(name)"],
  "request.form": ["Request.Form(name)"],
  "request.cookies": ["Request.Cookies(name)"],
  "request.servervariables": ["Request.ServerVariables(name)"],
  "request.binaryread": ["Request.BinaryRead(count)"],
  "server.createobject": ["Server.CreateObject(progId)"],
  "server.mappath": ["Server.MapPath(path)"],
  "server.htmlencode": ["Server.HTMLEncode(value)"],
  "server.urlencode": ["Server.URLEncode(value)"],
  "server.getlasterror": ["Server.GetLastError"],
  "session.abandon": ["Session.Abandon"],
  "application.lock": ["Application.Lock"],
  "application.unlock": ["Application.Unlock"],
};

const memberCompletions: Record<string, CompletionItem[]> = {
  request: [
    "QueryString",
    "Form",
    "Cookies",
    "ServerVariables",
    "ClientCertificate",
    "TotalBytes",
    "BinaryRead",
  ].map(methodItem),
  response: [
    "Write",
    "Redirect",
    "End",
    "Flush",
    "Clear",
    "Cookies",
    "Status",
    "ContentType",
    "Charset",
  ].map(methodItem),
  session: ["Abandon", "Contents", "StaticObjects", "SessionID", "Timeout", "CodePage", "LCID"].map(
    methodItem,
  ),
  application: ["Lock", "Unlock", "Contents", "StaticObjects"].map(methodItem),
  server: [
    "CreateObject",
    "MapPath",
    "HTMLEncode",
    "URLEncode",
    "ScriptTimeout",
    "GetLastError",
  ].map(methodItem),
};

const externalObjectMembers: Record<string, CompletionItem[]> = {
  "adodb.connection": [
    "Open",
    "Close",
    "Execute",
    "BeginTrans",
    "CommitTrans",
    "RollbackTrans",
    "ConnectionString",
    "State",
  ].map(methodItem),
  "adodb.recordset": [
    "Open",
    "Close",
    "MoveNext",
    "MovePrevious",
    "MoveFirst",
    "MoveLast",
    "EOF",
    "BOF",
    "Fields",
    "RecordCount",
  ].map(methodItem),
  "adodb.command": ["Execute", "CreateParameter", "Parameters", "CommandText", "CommandType"].map(
    methodItem,
  ),
};

const intrinsicTypeNames = new Set([
  "string",
  "number",
  "boolean",
  "date",
  "variant",
  "unknown",
  "nothing",
]);

const classicAspTypeNames = new Set([
  "request",
  "response",
  "session",
  "application",
  "server",
  "asperror",
]);

const adoMemberTypes: Record<string, Record<string, string>> = {
  "adodb.connection": {
    Open: "Variant",
    Close: "Variant",
    Execute: "ADODB.Recordset",
    BeginTrans: "Number",
    CommitTrans: "Variant",
    RollbackTrans: "Variant",
    ConnectionString: "String",
    State: "Number",
  },
  "adodb.recordset": {
    Open: "Variant",
    Close: "Variant",
    MoveNext: "Variant",
    MovePrevious: "Variant",
    MoveFirst: "Variant",
    MoveLast: "Variant",
    EOF: "Boolean",
    BOF: "Boolean",
    Fields: "Object",
    RecordCount: "Number",
  },
  "adodb.command": {
    Execute: "ADODB.Recordset",
    CreateParameter: "Object",
    Parameters: "Object",
    CommandText: "String",
    CommandType: "Number",
  },
};

function methodItem(label: string): CompletionItem {
  return { label, kind: CompletionItemKind.Method };
}

interface BuiltinFunction {
  label: string;
  signature: string;
  returnType: string;
  documentation: string;
}

const builtinFunctions: BuiltinFunction[] = [
  {
    label: "CStr",
    signature: "CStr(value)",
    returnType: "String",
    documentation: "Converts a value to String.",
  },
  {
    label: "CInt",
    signature: "CInt(value)",
    returnType: "Number",
    documentation: "Converts a value to Integer.",
  },
  {
    label: "CLng",
    signature: "CLng(value)",
    returnType: "Number",
    documentation: "Converts a value to Long.",
  },
  {
    label: "CDbl",
    signature: "CDbl(value)",
    returnType: "Number",
    documentation: "Converts a value to Double.",
  },
  {
    label: "CBool",
    signature: "CBool(value)",
    returnType: "Boolean",
    documentation: "Converts a value to Boolean.",
  },
  {
    label: "CDate",
    signature: "CDate(value)",
    returnType: "Date",
    documentation: "Converts a value to Date.",
  },
  {
    label: "Array",
    signature: "Array(values)",
    returnType: "Array",
    documentation: "Creates a Variant array.",
  },
  {
    label: "UBound",
    signature: "UBound(array, dimension)",
    returnType: "Number",
    documentation: "Returns the largest available subscript for an array dimension.",
  },
  {
    label: "LBound",
    signature: "LBound(array, dimension)",
    returnType: "Number",
    documentation: "Returns the smallest available subscript for an array dimension.",
  },
  {
    label: "LCase",
    signature: "LCase(value)",
    returnType: "String",
    documentation: "Converts a string to lowercase.",
  },
  {
    label: "UCase",
    signature: "UCase(value)",
    returnType: "String",
    documentation: "Converts a string to uppercase.",
  },
  {
    label: "Trim",
    signature: "Trim(value)",
    returnType: "String",
    documentation: "Removes leading and trailing spaces.",
  },
  {
    label: "Len",
    signature: "Len(value)",
    returnType: "Number",
    documentation: "Returns the number of characters in a string.",
  },
  {
    label: "InStr",
    signature: "InStr(start, string1, string2, compare)",
    returnType: "Number",
    documentation: "Returns the position of one string within another.",
  },
  {
    label: "Replace",
    signature: "Replace(expression, find, replaceWith)",
    returnType: "String",
    documentation: "Returns a string with replacements applied.",
  },
  {
    label: "Left",
    signature: "Left(value, length)",
    returnType: "String",
    documentation: "Returns the left part of a string.",
  },
  {
    label: "Right",
    signature: "Right(value, length)",
    returnType: "String",
    documentation: "Returns the right part of a string.",
  },
  {
    label: "Mid",
    signature: "Mid(value, start, length)",
    returnType: "String",
    documentation: "Returns part of a string.",
  },
  {
    label: "Date",
    signature: "Date()",
    returnType: "Date",
    documentation: "Returns the current system date.",
  },
  {
    label: "Now",
    signature: "Now()",
    returnType: "Date",
    documentation: "Returns the current date and time.",
  },
  {
    label: "DateAdd",
    signature: "DateAdd(interval, number, date)",
    returnType: "Date",
    documentation: "Returns a date with an interval added.",
  },
  {
    label: "DateDiff",
    signature: "DateDiff(interval, date1, date2)",
    returnType: "Number",
    documentation: "Returns the number of intervals between two dates.",
  },
  {
    label: "Abs",
    signature: "Abs(number)",
    returnType: "Number",
    documentation: "Returns the absolute value of a number.",
  },
  {
    label: "Int",
    signature: "Int(number)",
    returnType: "Number",
    documentation: "Returns the integer part of a number.",
  },
  {
    label: "Round",
    signature: "Round(number, decimalPlaces)",
    returnType: "Number",
    documentation: "Rounds a number.",
  },
  {
    label: "IsArray",
    signature: "IsArray(value)",
    returnType: "Boolean",
    documentation: "Returns whether a value is an array.",
  },
  {
    label: "IsNull",
    signature: "IsNull(value)",
    returnType: "Boolean",
    documentation: "Returns whether a value is Null.",
  },
  {
    label: "IsEmpty",
    signature: "IsEmpty(value)",
    returnType: "Boolean",
    documentation: "Returns whether a variable is Empty.",
  },
  {
    label: "IsNumeric",
    signature: "IsNumeric(value)",
    returnType: "Boolean",
    documentation: "Returns whether a value can be evaluated as a number.",
  },
  {
    label: "TypeName",
    signature: "TypeName(value)",
    returnType: "String",
    documentation: "Returns the subtype name for a variable.",
  },
];

const vbKeywords = new Set([
  "and",
  "as",
  "byref",
  "byval",
  "call",
  "case",
  "class",
  "const",
  "dim",
  "do",
  "each",
  "else",
  "elseif",
  "empty",
  "end",
  "exit",
  "explicit",
  "false",
  "for",
  "function",
  "get",
  "if",
  "in",
  "let",
  "loop",
  "me",
  "mod",
  "new",
  "next",
  "not",
  "nothing",
  "null",
  "option",
  "or",
  "preserve",
  "private",
  "property",
  "public",
  "redim",
  "select",
  "set",
  "step",
  "sub",
  "then",
  "to",
  "true",
  "until",
  "wend",
  "while",
  "with",
]);

export function parseVbscriptCst(text: string, sourceText = text, baseOffset = 0): VbCstNode {
  const tokens = tokenizeVbscript(text, baseOffset);
  const document: VbCstNode = {
    kind: "Document",
    start: baseOffset,
    end: baseOffset + text.length,
    contentStart: baseOffset,
    contentEnd: baseOffset + text.length,
    tokens,
    children: [],
  };
  const significant = tokens.filter(
    (token) => token.kind !== "whitespace" && token.kind !== "comment",
  );
  const stack: VbCstNode[] = [document];
  for (let index = 0; index < significant.length; index += 1) {
    const token = significant[index];
    if (!isStatementStart(significant, index)) {
      continue;
    }
    const first = lowerToken(token);
    const second = lowerToken(significant[index + 1]);
    if (first === "class" && significant[index + 1]?.kind === "identifier") {
      const node = createBlockNode("Class", token, significant[index + 1], stack);
      addChild(stack.at(-1) ?? document, node);
      stack.push(node);
      continue;
    }
    if (first === "end") {
      closeBlock(stack, second, token);
      continue;
    }
    const declarationStart =
      first === "public" || first === "private" ? lowerToken(significant[index + 1]) : first;
    const declarationOffset = first === "public" || first === "private" ? 1 : 0;
    const visibility =
      first === "public" || first === "private" ? (first as "public" | "private") : undefined;
    if (declarationStart === "sub" || declarationStart === "function") {
      const nameToken = significant[index + declarationOffset + 1];
      if (nameToken?.kind === "identifier") {
        const node = createProcedureNode(
          declarationStart,
          token,
          nameToken,
          collectParameterMetadata(significant, index + declarationOffset + 2),
          stack,
          undefined,
          visibility,
        );
        addChild(stack.at(-1) ?? document, node);
        stack.push(node);
      }
      continue;
    }
    if (declarationStart === "property") {
      const accessor = lowerToken(significant[index + declarationOffset + 1]);
      const nameToken = significant[index + declarationOffset + 2];
      if (
        (accessor === "get" || accessor === "let" || accessor === "set") &&
        nameToken?.kind === "identifier"
      ) {
        const node = createProcedureNode(
          "property",
          token,
          nameToken,
          collectParameterMetadata(significant, index + declarationOffset + 3),
          stack,
          accessor,
          visibility,
        );
        addChild(stack.at(-1) ?? document, node);
        stack.push(node);
      }
      continue;
    }
    const current = stack.at(-1) ?? document;
    if (first === "loop") {
      closeBlock(stack, "loop", token);
      continue;
    }
    if (first === "wend") {
      closeBlock(stack, "wend", token);
      continue;
    }
    if (first === "next") {
      closeBlock(stack, "next", token);
      continue;
    }
    if (first === "if") {
      const node = createStatementNode("If", token, significant, index);
      addChild(current, node);
      if (isMultilineIf(significant, index)) {
        stack.push(node);
      }
      continue;
    }
    if (first === "select" && second === "case") {
      const node = createStatementNode("Select", token, significant, index);
      addChild(current, node);
      stack.push(node);
      continue;
    }
    if (first === "do") {
      const node = createStatementNode("DoLoop", token, significant, index);
      addChild(current, node);
      stack.push(node);
      continue;
    }
    if (first === "while") {
      const node = createStatementNode("While", token, significant, index);
      addChild(current, node);
      stack.push(node);
      continue;
    }
    if (first === "dim" || first === "redim") {
      addChild(
        current,
        createDeclarationNode(token, "VariableDeclaration", first, significant, index + 1),
      );
      continue;
    }
    if (
      (first === "public" || first === "private") &&
      !["sub", "function", "property"].includes(second ?? "")
    ) {
      addChild(
        current,
        createDeclarationNode(
          token,
          "VariableDeclaration",
          first,
          significant,
          index + 1,
          visibility,
        ),
      );
      continue;
    }
    if (first === "const") {
      addChild(
        current,
        createDeclarationNode(token, "ConstantDeclaration", "const", significant, index + 1),
      );
      continue;
    }
    if (first === "for" && second === "each" && significant[index + 2]?.kind === "identifier") {
      const nameToken = significant[index + 2];
      const node: VbCstNode = {
        kind: "ForEach",
        start: token.start,
        end: statementEnd(significant, index),
        nameToken,
        tokens: statementTokens(significant, index),
        children: [],
        declarationKind: "forEach",
        identifiers: [nameToken],
        memberOf: current.kind === "Class" ? current.nameToken?.text : current.memberOf,
        scopeName:
          current.kind === "Procedure" || current.kind === "Property"
            ? current.nameToken?.text
            : undefined,
        scopeStart: token.start,
        scopeEnd: statementEnd(significant, index),
      };
      addChild(current, node);
      stack.push(node);
      continue;
    }
    if (first === "with" && significant[index + 1]?.kind === "identifier") {
      const nameToken = significant[index + 1];
      const node: VbCstNode = {
        kind: "With",
        start: token.start,
        end: statementEnd(significant, index),
        nameToken,
        tokens: statementTokens(significant, index),
        children: [],
        scopeStart: token.start,
        scopeEnd: sourceText.length + baseOffset,
      };
      addChild(current, node);
      stack.push(node);
      continue;
    }
    if (
      first === "set" &&
      significant[index + 1]?.kind === "identifier" &&
      significant[index + 2]?.text === "="
    ) {
      const variableToken = significant[index + 1];
      const newIndex = findKeyword(
        significant,
        index + 3,
        statementEndIndex(significant, index),
        "new",
      );
      const createObjectIndex = findCreateObjectCall(
        significant,
        index + 3,
        statementEndIndex(significant, index),
      );
      if (newIndex !== -1 && significant[newIndex + 1]?.kind === "identifier") {
        addChild(current, {
          kind: "SetNew",
          start: token.start,
          end: statementEnd(significant, index),
          nameToken: variableToken,
          tokens: statementTokens(significant, index),
          children: [],
          typeName: significant[newIndex + 1].text,
        });
      } else if (createObjectIndex !== -1) {
        const stringToken = significant
          .slice(createObjectIndex)
          .find((item) => item.kind === "string");
        if (stringToken) {
          addChild(current, {
            kind: "CreateObject",
            start: token.start,
            end: statementEnd(significant, index),
            nameToken: variableToken,
            tokens: statementTokens(significant, index),
            children: [],
            typeName: stringToken.value ?? unquoteVbString(stringToken.text),
          });
        }
      }
      continue;
    }
    if (first === "call") {
      const nameToken = significant
        .slice(index + 1, statementEndIndex(significant, index))
        .find((item) => item.kind === "identifier");
      addChild(current, createStatementNode("Call", token, significant, index, nameToken));
      continue;
    }
    if (token.kind === "identifier" && statementHasSymbol(significant, index, "=")) {
      addChild(current, createStatementNode("Assignment", token, significant, index, token));
      continue;
    }
    addChild(current, createStatementNode("Expression", token, significant, index));
  }
  closeUnclosedBlocks(stack, document.end);
  return document;
}

function tokenizeVbscript(text: string, baseOffset: number): VbToken[] {
  const tokens: VbToken[] = [];
  let index = 0;
  while (index < text.length) {
    const start = index;
    const char = text[index];
    if (char === "\r" || char === "\n") {
      if (char === "\r" && text[index + 1] === "\n") {
        index += 2;
      } else {
        index += 1;
      }
      tokens.push(token("newline", text, start, index, baseOffset));
      continue;
    }
    if (char === " " || char === "\t") {
      while (index < text.length && (text[index] === " " || text[index] === "\t")) {
        index += 1;
      }
      tokens.push(token("whitespace", text, start, index, baseOffset));
      continue;
    }
    if (char === "'") {
      while (index < text.length && text[index] !== "\r" && text[index] !== "\n") {
        index += 1;
      }
      tokens.push(token("comment", text, start, index, baseOffset));
      continue;
    }
    if (char === '"') {
      index += 1;
      while (index < text.length) {
        if (text[index] === '"' && text[index + 1] === '"') {
          index += 2;
          continue;
        }
        if (text[index] === '"') {
          index += 1;
          break;
        }
        index += 1;
      }
      const result = token("string", text, start, index, baseOffset);
      result.value = unquoteVbString(result.text);
      tokens.push(result);
      continue;
    }
    if (isIdentifierStart(char)) {
      index += 1;
      while (index < text.length && isIdentifierPart(text[index])) {
        index += 1;
      }
      const result = token("identifier", text, start, index, baseOffset);
      if (vbKeywords.has(result.text.toLowerCase())) {
        result.kind = "keyword";
      }
      tokens.push(result);
      continue;
    }
    if (/[0-9]/.test(char)) {
      index += 1;
      while (index < text.length && /[0-9.]/.test(text[index])) {
        index += 1;
      }
      tokens.push(token("number", text, start, index, baseOffset));
      continue;
    }
    index += 1;
    tokens.push(token("symbol", text, start, index, baseOffset));
  }
  return tokens;
}

function token(
  kind: VbToken["kind"],
  text: string,
  start: number,
  end: number,
  baseOffset: number,
): VbToken {
  return { kind, start: baseOffset + start, end: baseOffset + end, text: text.slice(start, end) };
}

function isIdentifierStart(char: string): boolean {
  return /[A-Za-z]/.test(char);
}

function isIdentifierPart(char: string): boolean {
  return /[A-Za-z0-9_]/.test(char);
}

function isTriviaToken(token: VbToken): boolean {
  return token.kind === "whitespace" || token.kind === "comment" || token.kind === "newline";
}

function isWhitespaceOrNewline(token: VbToken | undefined): boolean {
  return token?.kind === "whitespace" || token?.kind === "newline";
}

function isDocCommentToken(token: VbToken | undefined): token is VbToken {
  return token?.kind === "comment" && token.text.startsWith("'''");
}

function lowerToken(token: VbToken | undefined): string | undefined {
  return token?.text.toLowerCase();
}

function isStatementStart(tokens: VbToken[], index: number): boolean {
  const previous = tokens[index - 1];
  return !previous || previous.kind === "newline" || previous.text === ":";
}

function createBlockNode(
  kind: "Class",
  startToken: VbToken,
  nameToken: VbToken,
  stack: VbCstNode[],
): VbCstNode {
  const parent = stack.at(-1);
  return {
    kind,
    start: startToken.start,
    end: startToken.end,
    nameToken,
    tokens: [startToken, nameToken],
    children: [],
    memberOf: parent?.kind === "Class" ? parent.nameToken?.text : parent?.memberOf,
    scopeStart: startToken.start,
    scopeEnd: startToken.end,
  };
}

function createProcedureNode(
  procedureKind: "sub" | "function" | "property",
  startToken: VbToken,
  nameToken: VbToken,
  parameterMetadata: VbParameterMetadata[],
  stack: VbCstNode[],
  propertyAccessor?: "get" | "let" | "set",
  visibility?: "public" | "private",
): VbCstNode {
  const parentClass = [...stack].reverse().find((node) => node.kind === "Class")?.nameToken?.text;
  return {
    kind: procedureKind === "property" ? "Property" : "Procedure",
    start: startToken.start,
    end: startToken.end,
    nameToken,
    tokens: [startToken, nameToken],
    children: [],
    procedureKind,
    propertyAccessor,
    visibility,
    parameters: parameterMetadata.map((parameter) => parameter.token),
    parameterMetadata,
    memberOf: parentClass,
    scopeName: nameToken.text,
    scopeStart: startToken.start,
    scopeEnd: startToken.end,
  };
}

function addChild(parent: VbCstNode, child: VbCstNode): void {
  parent.children.push(child);
}

function closeBlock(stack: VbCstNode[], endKind: string | undefined, endToken: VbToken): void {
  const targetKind =
    endKind === "class"
      ? "Class"
      : endKind === "property"
        ? "Property"
        : endKind === "with"
          ? "With"
          : endKind === "if"
            ? "If"
            : endKind === "select"
              ? "Select"
              : endKind === "loop"
                ? "DoLoop"
                : endKind === "wend"
                  ? "While"
                  : endKind === "next"
                    ? "ForEach"
                    : "Procedure";
  const index = findLastIndex(stack, (node) => node.kind === targetKind);
  if (index <= 0) {
    return;
  }
  const [node] = stack.splice(index, 1);
  node.end = endToken.end;
  node.scopeEnd = endToken.end;
}

function closeUnclosedBlocks(stack: VbCstNode[], end: number): void {
  for (const node of stack) {
    node.end = Math.max(node.end, end);
    node.scopeEnd = Math.max(node.scopeEnd ?? node.end, end);
  }
}

function collectParameterMetadata(tokens: VbToken[], index: number): VbParameterMetadata[] {
  const parameters: VbParameterMetadata[] = [];
  if (tokens[index]?.text !== "(") {
    return parameters;
  }
  let cursor = index + 1;
  let mode: VbParameterMode | undefined;
  let optional = false;
  let canReadName = true;
  while (cursor < tokens.length && tokens[cursor].text !== ")") {
    const token = tokens[cursor];
    const lower = token.text.toLowerCase();
    if (token.text === ",") {
      mode = undefined;
      optional = false;
      canReadName = true;
    } else if (lower === "optional") {
      optional = true;
    } else if (lower === "byval") {
      mode = "byval";
    } else if (lower === "byref") {
      mode = "byref";
    } else if (canReadName && token.kind === "identifier") {
      parameters.push({ token, mode: mode ?? "byref", optional });
      canReadName = false;
    }
    cursor += 1;
  }
  return parameters;
}

function createDeclarationNode(
  startToken: VbToken,
  kind: "VariableDeclaration" | "ConstantDeclaration",
  declarationKind: NonNullable<VbCstNode["declarationKind"]>,
  tokens: VbToken[],
  startIndex: number,
  visibility?: "public" | "private",
): VbCstNode {
  const endIndex = statementEndIndex(tokens, startIndex - 1);
  const identifiers: VbToken[] = [];
  let canReadIdentifier = true;
  for (let index = startIndex; index <= endIndex; index += 1) {
    const current = tokens[index];
    if (!current) {
      continue;
    }
    if (current.text === "(") {
      canReadIdentifier = false;
      continue;
    }
    if (current.text === ")" || current.text === ",") {
      canReadIdentifier = current.text === ",";
      continue;
    }
    if (current.text === "=") {
      break;
    }
    if (current.kind === "identifier" && canReadIdentifier) {
      identifiers.push(current);
      canReadIdentifier = false;
    }
  }
  return {
    kind,
    start: startToken.start,
    end: statementEnd(tokens, startIndex - 1),
    tokens: statementTokens(tokens, startIndex - 1),
    children: [],
    declarationKind,
    visibility,
    identifiers,
  };
}

function createStatementNode(
  kind: "If" | "Select" | "DoLoop" | "While" | "Call" | "Assignment" | "Expression",
  startToken: VbToken,
  tokens: VbToken[],
  startIndex: number,
  nameToken?: VbToken,
): VbCstNode {
  return {
    kind,
    start: startToken.start,
    end: statementEnd(tokens, startIndex),
    nameToken,
    tokens: statementTokens(tokens, startIndex),
    children: [],
    scopeStart: startToken.start,
    scopeEnd: statementEnd(tokens, startIndex),
  };
}

function statementEndIndex(tokens: VbToken[], startIndex: number): number {
  let index = startIndex;
  while (index + 1 < tokens.length) {
    const next = tokens[index + 1];
    if ((next.kind === "newline" && tokens[index]?.text !== "_") || next.text === ":") {
      break;
    }
    index += 1;
  }
  return index;
}

function statementEnd(tokens: VbToken[], startIndex: number): number {
  return tokens[statementEndIndex(tokens, startIndex)]?.end ?? tokens[startIndex]?.end ?? 0;
}

function statementTokens(tokens: VbToken[], startIndex: number): VbToken[] {
  return tokens.slice(startIndex, statementEndIndex(tokens, startIndex) + 1);
}

function isMultilineIf(tokens: VbToken[], startIndex: number): boolean {
  const endIndex = statementEndIndex(tokens, startIndex);
  const thenIndex = findKeyword(tokens, startIndex, endIndex, "then");
  return thenIndex !== -1 && thenIndex === endIndex;
}

function statementHasSymbol(tokens: VbToken[], startIndex: number, symbol: string): boolean {
  const endIndex = statementEndIndex(tokens, startIndex);
  for (let index = startIndex; index <= endIndex; index += 1) {
    if (tokens[index]?.text === symbol) {
      return true;
    }
  }
  return false;
}

function findKeyword(tokens: VbToken[], start: number, end: number, keyword: string): number {
  for (let index = start; index <= end; index += 1) {
    if (lowerToken(tokens[index]) === keyword) {
      return index;
    }
  }
  return -1;
}

function findCreateObjectCall(tokens: VbToken[], start: number, end: number): number {
  for (let index = start; index + 2 <= end; index += 1) {
    if (
      lowerToken(tokens[index]) === "server" &&
      tokens[index + 1]?.text === "." &&
      lowerToken(tokens[index + 2]) === "createobject"
    ) {
      return index;
    }
  }
  return -1;
}

function unquoteVbString(value: string): string {
  return value.startsWith('"') && value.endsWith('"')
    ? value.slice(1, -1).replaceAll('""', '"')
    : value;
}

export function getVbscriptCompletions(
  parsed: AspParsedDocument,
  position: Position,
  context: VbProjectContext = {},
): CompletionItem[] {
  const sourceOffset = offsetAt(parsed.text, position);
  const symbols = context.symbols ?? collectVbscriptSymbols(parsed, context);
  const docCompletions = getVbDocCommentCompletions(parsed, sourceOffset, symbols, context.locale);
  if (docCompletions.length > 0) {
    return docCompletions;
  }
  const typeEnvironment =
    context.typeEnvironment ?? buildVbTypeEnvironment(parsed, { ...context, symbols });
  const prefix = parsed.text.slice(Math.max(0, sourceOffset - 96), sourceOffset);
  const memberMatch = /([A-Za-z][A-Za-z0-9_]*)\.$/.exec(prefix);
  const withMemberMatch = !memberMatch && prefix.endsWith(".");
  if (memberMatch || withMemberMatch) {
    const ownerName = memberMatch?.[1];
    const builtin = ownerName ? memberCompletions[ownerName.toLowerCase()] : undefined;
    if (builtin) {
      return builtin;
    }
    const className =
      ownerName === undefined
        ? currentWithTypeName(parsed, sourceOffset, symbols)
        : ownerName.toLowerCase() === "me"
          ? currentClassName(parsed, sourceOffset, symbols)
          : inferVariableType(ownerName, parsed, sourceOffset, symbols);
    return className ? typeMemberCompletions(className, symbols, typeEnvironment) : [];
  }
  return dedupeCompletions([
    ...builtinCompletions(context.locale),
    ...visibleSymbols(parsed, sourceOffset, symbols).map((symbol) =>
      symbolToCompletion(symbol, context.locale),
    ),
  ]);
}

function getVbDocCommentCompletions(
  parsed: AspParsedDocument,
  offset: number,
  symbols: VbSymbol[],
  locale: AspLocale | undefined,
): CompletionItem[] {
  const localizer = createLocalizer(locale);
  const prefix = docCommentLinePrefixAt(parsed.text, offset);
  if (prefix === undefined) {
    return [];
  }
  const paramValue = /<param\b[^>]*\bname\s*=\s*"([^"]*)$/i.exec(prefix);
  if (paramValue) {
    return nextDocumentedProcedureParameters(parsed, offset).map((name) => ({
      label: name,
      kind: CompletionItemKind.Variable,
      detail: localizer.t("vb.doc.detail.parameter"),
    }));
  }
  const crefValue = /\bcref\s*=\s*"([^"]*)$/i.exec(prefix);
  if (crefValue) {
    return dedupeCompletions(
      symbols
        .filter((symbol) => symbol.kind !== "parameter")
        .map((symbol) => ({
          label: symbol.memberOf ? `${symbol.memberOf}.${symbol.name}` : symbol.name,
          kind: symbolToCompletion(symbol, locale).kind,
          detail: localizer.t("vb.doc.detail.cref"),
        })),
    );
  }
  const attribute = /<([A-Za-z][A-Za-z0-9]*)\b([^<>]*)\s+[A-Za-z0-9_-]*$/i.exec(prefix);
  if (attribute && !isInsideXmlAttributeValue(attribute[2] ?? "")) {
    const tag = attribute[1].toLowerCase();
    const used = new Set(
      [...(attribute[2] ?? "").matchAll(/\b([A-Za-z][A-Za-z0-9_-]*)\s*=/g)].map((match) =>
        match[1].toLowerCase(),
      ),
    );
    return (vbDocCommentAttributeCompletions[tag] ?? [])
      .filter((name) => !used.has(name.toLowerCase()))
      .map((name) => ({
        label: name,
        kind: CompletionItemKind.Property,
        detail: localizer.t("vb.doc.detail.attribute"),
        insertText: `${name}="$1"`,
        insertTextFormat: InsertTextFormat.Snippet,
      }));
  }
  const closing = /<\/([A-Za-z0-9_-]*)$/i.exec(prefix);
  if (closing) {
    return unclosedDocCommentTags(parsed, offset).map((tag) => ({
      label: tag,
      kind: CompletionItemKind.Property,
      detail: localizer.t("vb.doc.detail.closingTag"),
      insertText: `${tag}>`,
    }));
  }
  const tag = /<([A-Za-z0-9_-]*)$/i.exec(prefix);
  if (tag) {
    return vbDocCommentTags.map((name) => docCommentTagCompletion(name, locale));
  }
  return [];
}

function docCommentLinePrefixAt(text: string, offset: number): string | undefined {
  const lineStart = text.lastIndexOf("\n", Math.max(0, offset - 1)) + 1;
  const prefix = text.slice(lineStart, offset).replace(/\r$/, "");
  const match = /^\s*'''\s?(.*)$/.exec(prefix);
  return match?.[1];
}

function docCommentTagCompletion(
  tag: (typeof vbDocCommentTags)[number],
  locale: AspLocale | undefined,
): CompletionItem {
  const snippet =
    tag === "see" || tag === "seealso"
      ? `${tag} cref="$1" />`
      : tag === "param"
        ? 'param name="$1">$0</param>'
        : tag === "exception"
          ? 'exception cref="$1">$0</exception>'
          : tag === "list"
            ? 'list type="$1">$0</list>'
            : `${tag}>$0</${tag}>`;
  return {
    label: tag,
    kind: CompletionItemKind.Property,
    detail: createLocalizer(locale).t("vb.doc.detail.tag"),
    insertText: snippet,
    insertTextFormat: InsertTextFormat.Snippet,
  };
}

function isInsideXmlAttributeValue(text: string): boolean {
  return (text.match(/"/g)?.length ?? 0) % 2 === 1 || (text.match(/'/g)?.length ?? 0) % 2 === 1;
}

function nextDocumentedProcedureParameters(parsed: AspParsedDocument, offset: number): string[] {
  const node = vbDocuments(parsed)
    .flatMap((document) => flattenVbNodes(document))
    .filter(
      (candidate) =>
        candidate.start >= offset &&
        (candidate.kind === "Procedure" || candidate.kind === "Property"),
    )
    .sort((left, right) => left.start - right.start)[0];
  return node?.parameters?.map((token) => token.text) ?? [];
}

function unclosedDocCommentTags(parsed: AspParsedDocument, offset: number): string[] {
  const text = docCommentTextUpToOffset(parsed, offset);
  const stack: string[] = [];
  for (const token of tokenizeDocTags(text)) {
    const tag = token.name.toLowerCase();
    if (token.kind === "end") {
      const index = stack.lastIndexOf(tag);
      if (index !== -1) {
        stack.splice(index, 1);
      }
    } else if (
      !token.selfClosing &&
      vbDocCommentTags.includes(tag as (typeof vbDocCommentTags)[number])
    ) {
      stack.push(tag);
    }
  }
  return [...new Set(stack.reverse())];
}

function docCommentTextUpToOffset(parsed: AspParsedDocument, offset: number): string {
  return docCommentBlockAtOffset(parsed, offset)
    .filter((token) => token.start <= offset)
    .map((token) =>
      stripDocCommentPrefix(
        token.start <= offset && offset <= token.end
          ? token.text.slice(0, offset - token.start)
          : token.text,
      ),
    )
    .join("\n");
}

function docCommentBlockAtOffset(parsed: AspParsedDocument, offset: number): VbToken[] {
  const document = vbDocuments(parsed).find(
    (candidate) => offset >= candidate.start && offset <= candidate.end,
  );
  const tokens = document?.tokens ?? [];
  const currentIndex = tokens.findIndex((token) => offset >= token.start && offset <= token.end);
  if (currentIndex === -1 || !isDocCommentToken(tokens[currentIndex])) {
    return [];
  }
  const start = docCommentBlockBoundary(tokens, currentIndex, -1);
  const end = docCommentBlockBoundary(tokens, currentIndex, 1);
  return tokens.slice(start, end + 1).filter(isDocCommentToken);
}

function docCommentBlockBoundary(tokens: VbToken[], startIndex: number, direction: -1 | 1): number {
  let boundary = startIndex;
  let index = startIndex + direction;
  while (index >= 0 && index < tokens.length) {
    while (index >= 0 && index < tokens.length && isWhitespaceOrNewline(tokens[index])) {
      index += direction;
    }
    if (!isDocCommentToken(tokens[index])) {
      break;
    }
    boundary = index;
    index += direction;
  }
  return boundary;
}

export function analyzeVbscript(
  parsed: AspParsedDocument,
  context: VbProjectContext = {},
): { diagnostics: Diagnostic[]; symbols: VbSymbol[] } {
  const symbols = context.symbols ?? collectVbscriptSymbols(parsed, context);
  const diagnostics: Diagnostic[] = [];
  const scriptText = getServerScriptText(parsed);
  if (/^\s*Option\s+Explicit\b/im.test(scriptText)) {
    diagnostics.push(...diagnoseUndeclaredVariables(parsed, symbols, context.locale));
  }
  if (context.unusedDiagnostics !== false) {
    diagnostics.push(...diagnoseUnusedSymbols(parsed, symbols, context));
  }
  diagnostics.push(...diagnoseIdentifierCase(parsed, symbols, context));
  if (context.typeChecking === "strict") {
    diagnostics.push(
      ...diagnoseTypeIssues(
        parsed,
        symbols,
        context.typeEnvironment ?? buildVbTypeEnvironment(parsed, { ...context, symbols }),
        context.locale,
      ),
    );
  }
  return { diagnostics: dedupeDiagnostics(diagnostics), symbols };
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

export function getVbscriptDocumentSymbols(parsed: AspParsedDocument): DocumentSymbol[] {
  return collectVbscriptSymbols(parsed)
    .filter(
      (symbol) =>
        symbol.sourceUri === parsed.uri &&
        (symbol.kind === "function" ||
          symbol.kind === "sub" ||
          symbol.kind === "class" ||
          symbol.kind === "method" ||
          symbol.kind === "property"),
    )
    .map((symbol) => ({
      name: symbol.memberOf ? `${symbol.memberOf}.${symbol.name}` : symbol.name,
      kind:
        symbol.kind === "class"
          ? SymbolKind.Class
          : symbol.kind === "property"
            ? SymbolKind.Property
            : SymbolKind.Function,
      range: symbol.range,
      selectionRange: symbol.range,
    }));
}

export function getVbscriptHover(
  parsed: AspParsedDocument,
  position: Position,
  context: VbProjectContext = {},
): string | undefined {
  const sourceOffset = offsetAt(parsed.text, position);
  const token = identifierTokenAt(parsed, sourceOffset);
  if (!token) {
    return undefined;
  }
  const builtin = builtinDescription(token.text, context.locale);
  if (builtin) {
    return builtin;
  }
  const symbol = resolveSymbolAt(
    parsed,
    sourceOffset,
    context.symbols ?? collectVbscriptSymbols(parsed, context),
  );
  if (!symbol) {
    return undefined;
  }
  return appendDocumentationMarkdown(
    markdownHover(vbscriptHoverSignature(symbol), vbscriptHoverDescription(symbol)),
    symbol.documentation,
    context.locale,
  );
}

function markdownHover(signature: string, description?: string): string {
  const base = `\`\`\`vbscript\n${signature}\n\`\`\``;
  return description ? `${base}\n\n${description}` : base;
}

function vbscriptHoverSignature(symbol: VbSymbol): string {
  const typeSuffix = symbol.typeName ? ` As ${symbol.typeName}` : "";
  const visibility = symbol.visibility ? `${titleCaseKeyword(symbol.visibility)} ` : "";
  const parameters = `(${parameterLabels(symbol).join(", ")})`;
  if (symbol.kind === "class") {
    return `Class ${symbol.name}`;
  }
  if (symbol.kind === "property") {
    const accessor = titleCaseKeyword(symbol.propertyAccessor ?? "get");
    return `${visibility}Property ${accessor} ${symbol.name}${parameters}${typeSuffix}`;
  }
  if (symbol.kind === "sub") {
    return `${visibility}Sub ${symbol.name}${parameters}`;
  }
  if (symbol.kind === "function") {
    return `${visibility}Function ${symbol.name}${parameters}${typeSuffix}`;
  }
  if (symbol.kind === "method") {
    const keyword = symbol.procedureKind === "sub" ? "Sub" : "Function";
    return keyword === "Sub"
      ? `${visibility}${keyword} ${symbol.name}${parameters}`
      : `${visibility}${keyword} ${symbol.name}${parameters}${typeSuffix}`;
  }
  if (symbol.kind === "field") {
    return `${visibility || "Public "}${symbol.name}${typeSuffix}`;
  }
  if (symbol.kind === "constant") {
    return `Const ${symbol.name}${typeSuffix}`;
  }
  if (symbol.kind === "parameter") {
    return `${parameterLabel({
      name: symbol.name,
      mode: symbol.parameterMode ?? "byref",
      optional: symbol.optional,
    })}${typeSuffix}`;
  }
  return `Dim ${symbol.name}${typeSuffix}`;
}

function parameterLabels(symbol: VbSymbol): string[] {
  return parameterDetails(symbol).map(parameterLabel);
}

function parameterDetails(symbol: VbSymbol): VbParameterInfo[] {
  return symbol.parameterDetails && symbol.parameterDetails.length > 0
    ? symbol.parameterDetails
    : (symbol.parameters ?? []).map((name): VbParameterInfo => ({ name, mode: "byref" }));
}

function parameterLabel(parameter: VbParameterInfo): string {
  const optional = parameter.optional ? "Optional " : "";
  return `${optional}${parameterModeKeyword(parameter.mode)} ${parameter.name}`;
}

function parameterModeKeyword(mode: VbParameterMode): "ByRef" | "ByVal" {
  return mode === "byval" ? "ByVal" : "ByRef";
}

function vbscriptHoverDescription(symbol: VbSymbol): string {
  if (symbol.implicit) {
    return "Implicit VBScript variable.";
  }
  const kindDescription =
    symbol.kind === "class"
      ? "VBScript class."
      : symbol.kind === "property"
        ? "VBScript property."
        : symbol.kind === "field"
          ? "VBScript field."
          : symbol.kind === "constant"
            ? "VBScript constant."
            : symbol.kind === "parameter"
              ? "VBScript parameter."
              : symbol.kind === "sub"
                ? "VBScript subroutine."
                : "VBScript function.";
  if (symbol.memberOf) {
    return `${kindDescription} Member of \`${symbol.memberOf}\`.`;
  }
  if (symbol.scopeName) {
    return symbol.kind === "parameter"
      ? `${kindDescription} Parameter of \`${symbol.scopeName}\`.`
      : `${kindDescription} Declared in \`${symbol.scopeName}\`.`;
  }
  return kindDescription;
}

function titleCaseKeyword(value: string): string {
  return value.charAt(0).toUpperCase() + value.slice(1).toLowerCase();
}

export function getVbscriptDefinition(
  parsed: AspParsedDocument,
  position: Position,
  context: VbProjectContext = {},
): VbSymbol | undefined {
  return resolveSymbolAt(
    parsed,
    offsetAt(parsed.text, position),
    context.symbols ?? collectVbscriptSymbols(parsed, context),
  );
}

export function getVbscriptReferences(
  parsed: AspParsedDocument,
  position: Position,
  context: VbProjectContext = {},
): VbReference[] {
  const symbol = getVbscriptDefinition(parsed, position, context);
  if (!symbol) {
    return [];
  }
  const symbols = context.symbols ?? collectVbscriptSymbols(parsed, context);
  const documents = context.documents ?? [parsed];
  const references: VbReference[] = [];
  for (const document of documents) {
    for (const token of identifierTokens(document)) {
      if (token.text.toLowerCase() === symbol.name.toLowerCase()) {
        const resolved = resolveSymbolAt(
          document,
          token.start + Math.floor(token.text.length / 2),
          symbols,
        );
        if (!resolved || !sameSymbol(resolved, symbol)) {
          continue;
        }
        references.push({
          uri: document.uri,
          range: rangeFromOffsets(document.text, token.start, token.end),
        });
      }
    }
  }
  return references;
}

export function getVbscriptSemanticTokens(
  parsed: AspParsedDocument,
  context: VbProjectContext = {},
): VbSemanticToken[] {
  const symbols = context.symbols ?? collectVbscriptSymbols(parsed, context);
  const tokens: VbSemanticToken[] = operatorSemanticTokens(parsed);
  for (const token of identifierTokens(parsed)) {
    if (isClassicAspObjectName(token.text)) {
      tokens.push({
        range: rangeFromOffsets(parsed.text, token.start, token.end),
        tokenType: "variable",
        tokenModifiers: ["library"],
      });
      continue;
    }
    const symbol = resolveSymbolAt(
      parsed,
      token.start + Math.floor(token.text.length / 2),
      symbols,
    );
    if (symbol && !isBuiltinName(symbol.name)) {
      const tokenType = semanticTokenTypeForSymbol(symbol);
      if (!tokenType) {
        continue;
      }
      tokens.push({
        range: rangeFromOffsets(parsed.text, token.start, token.end),
        tokenType,
        tokenModifiers: semanticTokenModifiersForSymbol(symbol),
      });
      continue;
    }
    const builtinToken = builtinSemanticTokenForIdentifier(parsed, token);
    if (builtinToken) {
      tokens.push(builtinToken);
    }
  }
  return tokens;
}

function builtinSemanticTokenForIdentifier(
  parsed: AspParsedDocument,
  token: VbToken,
): VbSemanticToken | undefined {
  const previous = previousSignificantToken(parsed, token.start);
  if (previous?.text === ".") {
    const owner = previousSignificantToken(parsed, previous.start);
    if (
      !owner ||
      !isClassicAspObjectName(owner.text) ||
      !builtinMemberName(owner.text, token.text)
    ) {
      return undefined;
    }
    return {
      range: rangeFromOffsets(parsed.text, token.start, token.end),
      tokenType: builtinSignature(`${owner.text}.${token.text}`) ? "method" : "property",
      tokenModifiers: ["library"],
    };
  }
  if (!builtinFunction(token.text)) {
    return undefined;
  }
  return {
    range: rangeFromOffsets(parsed.text, token.start, token.end),
    tokenType: "function",
    tokenModifiers: ["library"],
  };
}

function builtinMemberName(owner: string, member: string): boolean {
  return (
    memberCompletions[owner.toLowerCase()]?.some(
      (item) => item.label.toLowerCase() === member.toLowerCase(),
    ) ?? false
  );
}

function operatorSemanticTokens(parsed: AspParsedDocument): VbSemanticToken[] {
  const operators: VbSemanticToken[] = [];
  const documents = vbDocuments(parsed);
  for (const document of documents) {
    const tokens = document.tokens.filter((token) => !isTriviaToken(token));
    for (let index = 0; index < tokens.length; index += 1) {
      const token = tokens[index];
      const next = tokens[index + 1];
      const multiChar =
        token.text === "<" && (next?.text === ">" || next?.text === "=")
          ? next
          : token.text === ">" && next?.text === "="
            ? next
            : undefined;
      if (multiChar) {
        operators.push({
          range: rangeFromOffsets(parsed.text, token.start, multiChar.end),
          tokenType: "operator",
        });
        index += 1;
        continue;
      }
      if (isVbscriptOperator(token.text)) {
        operators.push({
          range: rangeFromOffsets(parsed.text, token.start, token.end),
          tokenType: "operator",
        });
      }
    }
  }
  return operators;
}

function isVbscriptOperator(text: string): boolean {
  return [
    "&",
    "+",
    "-",
    "*",
    "/",
    "\\",
    "^",
    "=",
    "<",
    ">",
    "and",
    "or",
    "not",
    "mod",
    "is",
    "xor",
    "eqv",
    "imp",
  ].includes(text.toLowerCase());
}

function semanticTokenTypeForSymbol(symbol: VbSymbol): VbSemanticToken["tokenType"] | undefined {
  if (symbol.kind === "class") {
    return "class";
  }
  if (symbol.kind === "method") {
    return "method";
  }
  if (symbol.kind === "field" || symbol.kind === "property") {
    return "property";
  }
  if (symbol.kind === "function" || symbol.kind === "sub") {
    return "function";
  }
  if (symbol.kind === "parameter") {
    return "parameter";
  }
  if (symbol.kind === "variable" || symbol.kind === "constant") {
    return "variable";
  }
  return undefined;
}

function semanticTokenModifiersForSymbol(
  symbol: VbSymbol,
): NonNullable<VbSemanticToken["tokenModifiers"]> {
  const modifiers: NonNullable<VbSemanticToken["tokenModifiers"]> = [];
  if (symbol.visibility) {
    modifiers.push(symbol.visibility);
  }
  if (symbol.kind === "constant") {
    modifiers.push("readonly");
  }
  if (symbol.kind === "parameter") {
    modifiers.push(symbol.parameterMode ?? "byref");
  }
  return modifiers;
}

export function getVbscriptRenameRange(
  parsed: AspParsedDocument,
  position: Position,
  context: VbProjectContext = {},
): Range | undefined {
  const offset = offsetAt(parsed.text, position);
  const symbol = resolveSymbolAt(
    parsed,
    offset,
    context.symbols ?? collectVbscriptSymbols(parsed, context),
  );
  if (!symbol || isBuiltinName(symbol.name)) {
    return undefined;
  }
  const token = identifierTokenAt(parsed, offset);
  return token?.text.toLowerCase() === symbol.name.toLowerCase()
    ? rangeFromOffsets(parsed.text, token.start, token.end)
    : undefined;
}

export function getVbscriptDocumentHighlights(
  parsed: AspParsedDocument,
  position: Position,
  context: VbProjectContext = {},
): DocumentHighlight[] {
  return getVbscriptReferences(parsed, position, context)
    .filter((reference) => reference.uri === parsed.uri)
    .map((reference) => ({
      range: reference.range,
      kind: DocumentHighlightKind.Text,
    }));
}

export function getVbscriptSignatureHelp(
  parsed: AspParsedDocument,
  position: Position,
  context: VbProjectContext = {},
): SignatureHelp | undefined {
  const offset = offsetAt(parsed.text, position);
  const call = callExpressionAt(parsed, offset);
  if (!call) {
    return undefined;
  }
  const activeParameter = countActiveParameter(parsed, call.argumentsStart, offset);
  const symbols = context.symbols ?? collectVbscriptSymbols(parsed, context);
  const typeEnvironment =
    context.typeEnvironment ?? buildVbTypeEnvironment(parsed, { ...context, symbols });
  const builtin = builtinSignatureLabels(call.name);
  const signatureSymbols = signatureSymbolsForCall(parsed, call.name, offset, symbols);
  if (signatureSymbols.length > 0) {
    return {
      signatures: signatureSymbols.map((symbol) =>
        symbolToSignatureInformation(symbol, context.locale),
      ),
      activeSignature: 0,
      activeParameter,
    };
  }
  const signatureLabels =
    typeSignatureLabelsForCall(parsed, call.name, offset, symbols, typeEnvironment) ?? builtin;
  if (!signatureLabels || signatureLabels.length === 0) {
    return undefined;
  }
  return {
    signatures: signatureLabels.map((label) => ({ label })),
    activeSignature: 0,
    activeParameter,
  };
}

export function resolveVbscriptCompletionItem(
  item: CompletionItem,
  parsed: AspParsedDocument,
  context: VbProjectContext = {},
): CompletionItem {
  const symbols = context.symbols ?? collectVbscriptSymbols(parsed, context);
  const env = context.typeEnvironment ?? buildVbTypeEnvironment(parsed, { ...context, symbols });
  const label = item.label.toLowerCase();
  const builtin = builtinCompletions(context.locale).find(
    (candidate) => candidate.label.toLowerCase() === label,
  );
  if (builtin) {
    return {
      ...item,
      detail: item.detail ?? builtin.detail,
      documentation: item.documentation ?? builtin.documentation,
    };
  }
  const symbol = symbols.find((candidate) => candidate.name.toLowerCase() === label);
  if (symbol) {
    const type = symbol.typeName ? ` As ${symbol.typeName}` : "";
    const owner = symbol.memberOf
      ? createLocalizer(context.locale).t("vb.symbol.owner", { owner: symbol.memberOf })
      : "";
    return {
      ...item,
      detail: `${symbol.kind}${type}${owner}`,
      documentation: appendDocumentationMarkdown(
        `${signatureLabelForDocumentation(symbol)}\n\n${createLocalizer(context.locale).t(
          "vb.completion.definedIn",
          { uri: symbol.sourceUri },
        )}`,
        symbol.documentation,
        context.locale,
      ),
    };
  }
  const member = env.types
    .flatMap((type) => type.members.map((candidate) => ({ type, member: candidate })))
    .find((candidate) => candidate.member.name.toLowerCase() === label);
  if (member) {
    const signature = member.member.signature
      ? signatureLabelFromMember(member.type.name, member.member.name, member.member.signature)
      : undefined;
    const type = member.member.type ? ` As ${member.member.type.name}` : "";
    return {
      ...item,
      detail: signature ?? `${member.member.kind}${type}`,
      documentation: createLocalizer(context.locale).t("vb.completion.memberDocumentation", {
        kind: member.member.kind,
        type: member.type.name,
        member: member.member.name,
        suffix: type,
      }),
    };
  }
  return item;
}

export function getVbscriptSelectionRanges(
  parsed: AspParsedDocument,
  positions: Position[],
): SelectionRange[] {
  return positions.map((position) => {
    const offset = offsetAt(parsed.text, position);
    const ranges = uniqueRanges(
      [
        tokenRangeAt(parsed, offset),
        statementRangeAt(parsed, offset),
        ...enclosingVbNodes(parsed, offset).map((node) =>
          rangeFromOffsets(parsed.text, node.start, node.end),
        ),
        regionRangeAt(parsed, offset),
        rangeFromOffsets(parsed.text, 0, parsed.text.length),
      ].filter(isRange),
    );
    return buildSelectionRangeChain(ranges);
  });
}

export function getVbscriptInlayHints(
  parsed: AspParsedDocument,
  range: Range,
  context: VbProjectContext = {},
  options: VbInlayHintOptions = {},
): InlayHint[] {
  const settings = {
    variableTypes: options.variableTypes !== false,
    parameterNames: options.parameterNames !== false,
    functionReturnTypes: options.functionReturnTypes !== false,
  };
  const symbols = context.symbols ?? collectVbscriptSymbols(parsed, context);
  const env = context.typeEnvironment ?? buildVbTypeEnvironment(parsed, { ...context, symbols });
  const hints: InlayHint[] = [];
  const rangeStart = offsetAt(parsed.text, range.start);
  const rangeEnd = offsetAt(parsed.text, range.end);
  if (settings.variableTypes) {
    for (const symbol of symbols) {
      if (
        symbol.sourceUri !== parsed.uri ||
        !["variable", "parameter", "constant", "field"].includes(symbol.kind) ||
        !symbol.typeName ||
        isLooseType(symbol.typeName) ||
        !rangeOverlapsOffsets(parsed.text, symbol.range, rangeStart, rangeEnd)
      ) {
        continue;
      }
      hints.push({
        position: symbol.range.end,
        label: `  As ${symbol.typeName}`,
        kind: InlayHintKind.Type,
        paddingLeft: false,
        paddingRight: true,
        tooltip: "Inferred VBScript type",
      });
    }
  }
  if (settings.functionReturnTypes) {
    for (const symbol of symbols) {
      if (
        symbol.sourceUri !== parsed.uri ||
        !["function", "property"].includes(symbol.kind) ||
        !symbol.typeName ||
        isLooseType(symbol.typeName) ||
        !rangeOverlapsOffsets(parsed.text, symbol.range, rangeStart, rangeEnd)
      ) {
        continue;
      }
      hints.push({
        position: symbol.range.end,
        label: `  As ${symbol.typeName}`,
        kind: InlayHintKind.Type,
        paddingLeft: false,
        paddingRight: true,
        tooltip: "Inferred VBScript return type",
      });
    }
  }
  if (settings.parameterNames) {
    for (const statement of vbStatements(parsed)) {
      for (let index = 0; index < statement.length; index += 1) {
        if (statement[index].text !== "(" || statement[index - 1]?.kind !== "identifier") {
          continue;
        }
        const name = callNameBefore(statement, index);
        const signature = name
          ? signatureForCall(parsed, name, statement[index].start, symbols, env)
          : undefined;
        if (!signature) {
          continue;
        }
        const closeIndex = matchingCloseParen(statement, index);
        const argumentTokens = topLevelArgumentStarts(
          statement.slice(index + 1, closeIndex === -1 ? undefined : closeIndex),
        );
        for (const [argumentIndex, token] of argumentTokens.entries()) {
          const parameter = signature.parameters[argumentIndex];
          if (
            !parameter ||
            token.start < rangeStart ||
            token.start > rangeEnd ||
            isNamedArgument(statement, token)
          ) {
            continue;
          }
          hints.push({
            position: rangeFromOffsets(parsed.text, token.start, token.end).start,
            label: `${parameter.name}:`,
            kind: InlayHintKind.Parameter,
            paddingRight: true,
            tooltip: "VBScript parameter name",
          });
        }
      }
    }
  }
  return hints.sort(
    (left, right) =>
      left.position.line - right.position.line ||
      left.position.character - right.position.character,
  );
}

export function getVbscriptTypeDefinition(
  parsed: AspParsedDocument,
  position: Position,
  context: VbProjectContext = {},
): VbSymbol | undefined {
  const offset = offsetAt(parsed.text, position);
  const symbols = context.symbols ?? collectVbscriptSymbols(parsed, context);
  const symbol = resolveSymbolAt(parsed, offset, symbols);
  const typeName = symbol?.typeName ?? typeNameAtOffset(parsed, offset, symbols);
  if (!typeName || isLooseType(typeName)) {
    return undefined;
  }
  return symbols.find(
    (candidate) =>
      candidate.kind === "class" && candidate.name.toLowerCase() === typeName.toLowerCase(),
  );
}

export function getVbscriptImplementation(
  parsed: AspParsedDocument,
  position: Position,
  context: VbProjectContext = {},
): VbSymbol | undefined {
  const symbol = getVbscriptDefinition(parsed, position, context);
  return symbol && !isBuiltinName(symbol.name) ? symbol : undefined;
}

export function prepareVbscriptCallHierarchy(
  parsed: AspParsedDocument,
  position: Position,
  context: VbProjectContext = {},
  rootUri = parsed.uri,
): CallHierarchyItem[] {
  const symbols = context.symbols ?? collectVbscriptSymbols(parsed, context);
  const symbol = resolveSymbolAt(parsed, offsetAt(parsed.text, position), symbols);
  if (!symbol || !isCallableHierarchySymbol(symbol)) {
    return [];
  }
  return [symbolToCallHierarchyItem(symbol, rootUri)];
}

export function getVbscriptIncomingCalls(
  item: CallHierarchyItem,
  context: VbProjectContext = {},
): CallHierarchyIncomingCall[] {
  const symbols = context.symbols ?? [];
  const documents = context.documents ?? [];
  const target = callHierarchyTargetSymbol(item, symbols);
  if (!target) {
    return [];
  }
  const callers = new Map<string, { item: CallHierarchyItem; ranges: Range[] }>();
  for (const document of documents) {
    for (const call of callSitesInDocument(document)) {
      const resolved = resolveCallTargetSymbol(document, call.name, call.offset, symbols);
      if (!resolved || !sameSymbol(resolved, target)) {
        continue;
      }
      const caller = enclosingCallableSymbol(document, call.offset, symbols);
      if (!caller) {
        continue;
      }
      const key = symbolKey(caller);
      const existing =
        callers.get(key) ??
        ({
          item: symbolToCallHierarchyItem(caller, callHierarchyRootUri(item)),
          ranges: [],
        } satisfies { item: CallHierarchyItem; ranges: Range[] });
      existing.ranges.push(call.range);
      callers.set(key, existing);
    }
  }
  return [...callers.values()].map((caller) => ({
    from: caller.item,
    fromRanges: caller.ranges,
  }));
}

export function getVbscriptOutgoingCalls(
  item: CallHierarchyItem,
  context: VbProjectContext = {},
): CallHierarchyOutgoingCall[] {
  const symbols = context.symbols ?? [];
  const documents = context.documents ?? [];
  const source = callHierarchyTargetSymbol(item, symbols);
  if (!source?.scopeRange) {
    return [];
  }
  const document = documents.find((candidate) => candidate.uri === source.sourceUri);
  if (!document) {
    return [];
  }
  const start = offsetAt(document.text, source.scopeRange.start);
  const end = offsetAt(document.text, source.scopeRange.end);
  const callees = new Map<string, { item: CallHierarchyItem; ranges: Range[] }>();
  for (const call of callSitesInDocument(document).filter(
    (candidate) => candidate.offset >= start && candidate.offset <= end,
  )) {
    const resolved = resolveCallTargetSymbol(document, call.name, call.offset, symbols);
    if (!resolved || sameSymbol(resolved, source) || !isCallableHierarchySymbol(resolved)) {
      continue;
    }
    const key = symbolKey(resolved);
    const existing =
      callees.get(key) ??
      ({
        item: symbolToCallHierarchyItem(resolved, callHierarchyRootUri(item)),
        ranges: [],
      } satisfies { item: CallHierarchyItem; ranges: Range[] });
    existing.ranges.push(call.range);
    callees.set(key, existing);
  }
  return [...callees.values()].map((callee) => ({
    to: callee.item,
    fromRanges: callee.ranges,
  }));
}

export function collectVbscriptSymbols(
  parsed: AspParsedDocument,
  context: VbProjectContext = {},
): VbSymbol[] {
  const symbols: VbSymbol[] = [];
  for (const node of vbDocuments(parsed)) {
    addSymbolsFromVbNode(parsed, node, symbols);
  }
  addImplicitAssignmentSymbols(parsed, symbols);
  applyTypeAnnotations(parsed, symbols);
  inferAssignedTypes(parsed, symbols, context);
  applyTypeAnnotations(parsed, symbols);
  return symbols;
}

export function buildVbTypeEnvironment(
  parsed: AspParsedDocument,
  context: VbProjectContext = {},
): VbTypeEnvironment {
  const symbols = context.symbols ?? collectVbscriptSymbols(parsed, context);
  const typeMap = new Map<string, VbType>();
  for (const type of builtinTypes()) {
    addType(typeMap, type);
  }
  for (const type of configuredComTypes(context.comTypes ?? {})) {
    addType(typeMap, type);
  }
  for (const symbol of symbols.filter((item) => item.kind === "class")) {
    addType(typeMap, {
      name: symbol.name,
      kind: "class",
      members: symbols
        .filter((member) => member.memberOf?.toLowerCase() === symbol.name.toLowerCase())
        .map(
          (member): VbMember => ({
            name: member.name,
            kind:
              member.kind === "method" ? "method" : member.kind === "field" ? "field" : "property",
            type: member.type ?? typeRef(member.typeName ?? "Variant"),
            signature:
              member.kind === "method" || member.kind === "property"
                ? {
                    parameters: parameterDetails(member).map((parameter) => ({
                      name: parameter.name,
                      mode: parameter.mode,
                      optional: parameter.optional,
                    })),
                    returnType: member.type ?? typeRef(member.typeName ?? "Variant"),
                  }
                : undefined,
          }),
        ),
    });
  }
  for (const annotation of parseTypeAnnotations(parsed).members) {
    const existing = typeMap.get(annotation.typeName.toLowerCase()) ?? {
      name: annotation.typeName,
      kind: "class" as const,
      members: [],
    };
    existing.members = [
      ...existing.members.filter(
        (member) => member.name.toLowerCase() !== annotation.memberName.toLowerCase(),
      ),
      {
        name: annotation.memberName,
        kind: "property",
        type: typeRef(annotation.memberType),
      },
    ];
    addType(typeMap, existing);
  }
  return { types: [...typeMap.values()], symbols };
}

function addSymbolsFromVbNode(
  parsed: AspParsedDocument,
  node: VbCstNode,
  symbols: VbSymbol[],
): void {
  const documentation = documentationForNode(parsed, node);
  if (node.kind === "Class" && node.nameToken) {
    symbols.push({
      name: node.nameToken.text,
      kind: "class",
      range: rangeFromOffsets(parsed.text, node.nameToken.start, node.nameToken.end),
      sourceUri: parsed.uri,
      scopeRange: rangeFromOffsets(parsed.text, node.start, node.end),
      documentation,
    });
  }
  if ((node.kind === "Procedure" || node.kind === "Property") && node.nameToken) {
    const name = node.nameToken.text;
    const kind: VbSymbolKind =
      node.kind === "Property"
        ? "property"
        : node.memberOf
          ? "method"
          : node.procedureKind === "sub"
            ? "sub"
            : "function";
    symbols.push({
      name,
      kind,
      range: rangeFromOffsets(parsed.text, node.nameToken.start, node.nameToken.end),
      sourceUri: parsed.uri,
      memberOf: node.memberOf,
      containerName: node.memberOf,
      scopeName: undefined,
      scopeRange: rangeFromOffsets(parsed.text, node.start, node.end),
      parameters: node.parameters?.map((token) => token.text) ?? [],
      parameterDetails:
        node.parameterMetadata?.map((parameter) => ({
          name: parameter.token.text,
          mode: parameter.mode,
          optional: parameter.optional || undefined,
        })) ?? [],
      visibility: node.visibility,
      procedureKind:
        node.procedureKind === "function" || node.procedureKind === "sub"
          ? node.procedureKind
          : undefined,
      propertyAccessor: node.propertyAccessor,
      documentation,
    });
    for (const parameter of node.parameterMetadata ?? []) {
      symbols.push({
        name: parameter.token.text,
        kind: "parameter",
        range: rangeFromOffsets(parsed.text, parameter.token.start, parameter.token.end),
        sourceUri: parsed.uri,
        scopeName: name,
        scopeRange: rangeFromOffsets(parsed.text, node.start, node.end),
        parameterMode: parameter.mode,
        optional: parameter.optional || undefined,
      });
    }
  }
  if (
    node.kind === "VariableDeclaration" ||
    node.kind === "ConstantDeclaration" ||
    node.kind === "ForEach"
  ) {
    const baseKind: "variable" | "constant" =
      node.kind === "ConstantDeclaration" ? "constant" : "variable";
    const scope = scopeNodeAt(parsed, node.start);
    const memberOf = scope ? undefined : (node.memberOf ?? parentClassName(parsed, node.start));
    const identifiers = node.identifiers ?? (node.nameToken ? [node.nameToken] : []);
    const variableDocumentation = identifiers.length === 1 ? documentation : undefined;
    for (const identifier of identifiers) {
      symbols.push({
        name: identifier.text,
        kind: memberOf && baseKind === "variable" ? "field" : baseKind,
        range: rangeFromOffsets(parsed.text, identifier.start, identifier.end),
        sourceUri: parsed.uri,
        memberOf,
        containerName: memberOf,
        scopeName: scope?.nameToken?.text,
        scopeRange: scope
          ? rangeFromOffsets(parsed.text, scope.start, scope.end)
          : memberOf
            ? rangeFromOffsets(parsed.text, node.start, node.end)
            : undefined,
        visibility: node.visibility,
        documentation: variableDocumentation,
      });
    }
  }
  for (const child of node.children) {
    addSymbolsFromVbNode(parsed, child, symbols);
  }
}

function addImplicitAssignmentSymbols(parsed: AspParsedDocument, symbols: VbSymbol[]): void {
  if (hasOptionExplicit(parsed)) {
    return;
  }
  for (const statement of vbStatements(parsed)) {
    const first = lowerToken(statement[0]);
    const targetIndex = first === "set" ? 1 : 0;
    const target = statement[targetIndex];
    const equalsIndex = statement.findIndex((token) => token.text === "=");
    if (
      !target ||
      target.kind !== "identifier" ||
      equalsIndex === -1 ||
      equalsIndex <= targetIndex ||
      statement[targetIndex + 1]?.text === "." ||
      isBuiltinName(target.text) ||
      isImplicitKeywordName(target.text)
    ) {
      continue;
    }
    const scope = scopeNodeAt(parsed, target.start);
    const memberOf = scope ? undefined : parentClassName(parsed, target.start);
    if (scope?.nameToken?.text.toLowerCase() === target.text.toLowerCase()) {
      continue;
    }
    if (
      symbols.some(
        (symbol) =>
          symbol.name.toLowerCase() === target.text.toLowerCase() &&
          symbol.sourceUri === parsed.uri &&
          (symbol.scopeName ?? "") === (scope?.nameToken?.text ?? "") &&
          (symbol.memberOf ?? "") === (memberOf ?? "") &&
          isSymbolVisibleAt(symbol, parsed.uri, parsed.text, target.start),
      )
    ) {
      continue;
    }
    symbols.push({
      name: target.text,
      kind: "variable",
      range: rangeFromOffsets(parsed.text, target.start, target.end),
      sourceUri: parsed.uri,
      memberOf,
      containerName: memberOf,
      scopeName: scope?.nameToken?.text,
      scopeRange: scope
        ? rangeFromOffsets(parsed.text, scope.start, scope.end)
        : memberOf
          ? rangeFromOffsets(parsed.text, target.start, statement.at(-1)?.end ?? target.end)
          : undefined,
      implicit: true,
    });
  }
}

function hasOptionExplicit(parsed: AspParsedDocument): boolean {
  return /^\s*Option\s+Explicit\b/im.test(getServerScriptText(parsed));
}

function isImplicitKeywordName(name: string): boolean {
  return ["true", "false", "nothing", "empty", "null", "me"].includes(name.toLowerCase());
}

function inferAssignedTypes(
  parsed: AspParsedDocument,
  symbols: VbSymbol[],
  context: VbProjectContext,
): void {
  const byName = new Map<string, VbSymbol[]>();
  for (const symbol of symbols) {
    const list = byName.get(symbol.name.toLowerCase()) ?? [];
    list.push(symbol);
    byName.set(symbol.name.toLowerCase(), list);
  }
  for (const document of vbDocuments(parsed)) {
    for (const node of flattenVbNodes(document)) {
      if (
        (node.kind !== "SetNew" && node.kind !== "CreateObject") ||
        !node.nameToken ||
        !node.typeName
      ) {
        continue;
      }
      const candidates = (byName.get(node.nameToken.text.toLowerCase()) ?? []).filter(
        (symbol) => symbol.kind === "variable" || symbol.kind === "field",
      );
      const visible =
        candidates.find((candidate) =>
          isSymbolVisibleAt(candidate, parsed.uri, parsed.text, node.start),
        ) ?? candidates[0];
      if (visible) {
        setSymbolType(visible, node.typeName);
      }
    }
  }
  inferStatementTypes(parsed, symbols, context);
}

function inferStatementTypes(
  parsed: AspParsedDocument,
  symbols: VbSymbol[],
  context: VbProjectContext,
): void {
  const env = buildVbTypeEnvironment(parsed, { ...context, symbols });
  for (const statement of vbStatements(parsed)) {
    const first = lowerToken(statement[0]);
    const targetIndex = first === "set" ? 1 : 0;
    const target = statement[targetIndex];
    const equalsIndex = statement.findIndex((token) => token.text === "=");
    if (!target || target.kind !== "identifier" || equalsIndex === -1) {
      continue;
    }
    const symbol = visibleSymbols(parsed, target.start, symbols).find(
      (item) => item.name.toLowerCase() === target.text.toLowerCase(),
    );
    if (!symbol) {
      continue;
    }
    const expressionType = inferExpressionType(
      parsed,
      statement.slice(equalsIndex + 1),
      symbols,
      env,
      target.start,
    );
    if (expressionType && (!symbol.typeName || symbol.typeName === "Variant")) {
      setSymbolType(symbol, expressionType.name);
    }
  }
  inferFunctionReturnTypes(parsed, symbols, env);
}

function inferFunctionReturnTypes(
  parsed: AspParsedDocument,
  symbols: VbSymbol[],
  env: VbTypeEnvironment,
): void {
  for (const node of vbDocuments(parsed).flatMap((document) => flattenVbNodes(document))) {
    if (node.kind !== "Procedure" || node.procedureKind !== "function" || !node.nameToken) {
      continue;
    }
    const symbol = symbols.find(
      (candidate) =>
        candidate.kind === "function" &&
        candidate.name.toLowerCase() === node.nameToken?.text.toLowerCase() &&
        candidate.range.start.line ===
          rangeFromOffsets(parsed.text, node.nameToken.start, node.nameToken.end).start.line,
    );
    if (!symbol || symbol.typeName) {
      continue;
    }
    const assignment = vbStatements(parsed)
      .filter((statement) => statement[0]?.start >= node.start && statement[0]?.end <= node.end)
      .find(
        (statement) =>
          statement[0]?.kind === "identifier" &&
          statement[0].text.toLowerCase() === node.nameToken?.text.toLowerCase() &&
          statement[1]?.text === "=",
      );
    const expressionType = assignment
      ? inferExpressionType(parsed, assignment.slice(2), symbols, env, assignment[0].start)
      : undefined;
    if (expressionType) {
      setSymbolType(symbol, expressionType.name);
    }
  }
}

interface TypeAnnotations {
  types: Array<{ name: string; typeName: string; offset: number }>;
  params: Array<{ name: string; typeName: string; procedureName?: string }>;
  returns: Array<{ name: string; typeName: string }>;
  members: Array<{ typeName: string; memberName: string; memberType: string }>;
}

function parseTypeAnnotations(parsed: AspParsedDocument): TypeAnnotations {
  const annotations: TypeAnnotations = { types: [], params: [], returns: [], members: [] };
  for (const document of vbDocuments(parsed)) {
    for (const token of document.tokens.filter((item) => item.kind === "comment")) {
      const text = token.text.replace(/^'\s*/, "").trim();
      const type = /^@type\s+([A-Za-z_][A-Za-z0-9_]*)\s+As\s+([A-Za-z_][A-Za-z0-9_.]*)/i.exec(text);
      if (type) {
        annotations.types.push({ name: type[1], typeName: type[2], offset: token.start });
        continue;
      }
      const param =
        /^@param\s+([A-Za-z_][A-Za-z0-9_]*)(?:\.([A-Za-z_][A-Za-z0-9_]*))?\s+As\s+([A-Za-z_][A-Za-z0-9_.]*)/i.exec(
          text,
        );
      if (param) {
        annotations.params.push({
          procedureName: param[2] ? param[1] : undefined,
          name: param[2] ?? param[1],
          typeName: param[3],
        });
        continue;
      }
      const returns = /^@returns(?:\s+([A-Za-z_][A-Za-z0-9_]*))?\s+([A-Za-z_][A-Za-z0-9_.]*)/i.exec(
        text,
      );
      if (returns) {
        const procedure = returns[1] ?? nextProcedureName(parsed, token.start);
        if (procedure) {
          annotations.returns.push({ name: procedure, typeName: returns[2] });
        }
        continue;
      }
      const member =
        /^@member\s+([A-Za-z_][A-Za-z0-9_.]*)\.([A-Za-z_][A-Za-z0-9_]*)\s+As\s+([A-Za-z_][A-Za-z0-9_.]*)/i.exec(
          text,
        );
      if (member) {
        annotations.members.push({
          typeName: member[1],
          memberName: member[2],
          memberType: member[3],
        });
      }
    }
  }
  return annotations;
}

function documentationForNode(
  parsed: AspParsedDocument,
  node: VbCstNode,
): VbDocumentation | undefined {
  if (!node.nameToken && !node.identifiers?.length) {
    return undefined;
  }
  const tokens = docCommentBlockBefore(parsed, node.start);
  return tokens.length > 0 ? parseVbDocumentation(tokens) : undefined;
}

function docCommentBlockBefore(parsed: AspParsedDocument, offset: number): VbToken[] {
  const document = vbDocuments(parsed).find(
    (candidate) => offset >= candidate.start && offset <= candidate.end,
  );
  const tokens = document?.tokens ?? [];
  let index = tokens.findIndex((token) => token.start >= offset);
  index = index === -1 ? tokens.length - 1 : index - 1;
  while (index >= 0 && isWhitespaceOrNewline(tokens[index])) {
    index -= 1;
  }
  const comments: VbToken[] = [];
  while (index >= 0) {
    const current = tokens[index];
    if (!isDocCommentToken(current)) {
      break;
    }
    comments.push(current);
    index -= 1;
    while (index >= 0 && isWhitespaceOrNewline(tokens[index])) {
      index -= 1;
    }
  }
  return comments.reverse();
}

function parseVbDocumentation(tokens: VbToken[]): VbDocumentation | undefined {
  const xmlText = tokens.map((token) => stripDocCommentPrefix(token.text)).join("\n");
  const docRoot = parseVbDocXml(xmlText);
  const documentation: VbDocumentation = {
    params: {},
    exceptions: [],
    see: [],
    seealso: [],
  };
  documentation.summary = firstDocElementText(docRoot, "summary");
  documentation.remarks = firstDocElementText(docRoot, "remarks");
  documentation.returns = firstDocElementText(docRoot, "returns");
  documentation.value = firstDocElementText(docRoot, "value");
  documentation.example = firstDocElementText(docRoot, "example");
  documentation.code = firstDocElementText(docRoot, "code", true);
  for (const element of docElements(docRoot, "param")) {
    const name = element.attributes.name;
    if (name) {
      documentation.params[name] = docElementText(element);
    }
  }
  for (const element of docElements(docRoot, "exception")) {
    documentation.exceptions.push({
      cref: element.attributes.cref,
      text: docElementText(element),
    });
  }
  for (const tag of ["see", "seealso"] as const) {
    for (const element of docElements(docRoot, tag)) {
      documentation[tag].push({
        cref: element.attributes.cref,
        href: element.attributes.href,
        langword: element.attributes.langword,
        text: docElementText(element),
      });
    }
  }
  return hasDocumentationContent(documentation) ? documentation : undefined;
}

function stripDocCommentPrefix(text: string): string {
  return text.replace(/^'''\s?/, "");
}

function parseVbDocXml(text: string): VbDocElement {
  const root: VbDocElement = { name: "__root", attributes: {}, children: [] };
  const stack: VbDocElement[] = [root];
  let cursor = 0;
  for (const token of tokenizeDocXml(text)) {
    if (token.start > cursor) {
      stack.at(-1)?.children.push(text.slice(cursor, token.start));
    }
    cursor = token.end;
    if (token.kind === "text") {
      stack.at(-1)?.children.push(token.text);
      continue;
    }
    if (token.kind === "start") {
      const element: VbDocElement = {
        name: token.name.toLowerCase(),
        attributes: token.attributes,
        children: [],
        selfClosing: token.selfClosing,
      };
      stack.at(-1)?.children.push(element);
      if (!token.selfClosing) {
        stack.push(element);
      }
      continue;
    }
    const index = findLastIndex(stack, (element) => element.name === token.name.toLowerCase());
    if (index > 0) {
      stack.splice(index);
    }
  }
  if (cursor < text.length) {
    stack.at(-1)?.children.push(text.slice(cursor));
  }
  return root;
}

function tokenizeDocXml(text: string): VbDocXmlToken[] {
  const tokens: VbDocXmlToken[] = [];
  let cursor = 0;
  while (cursor < text.length) {
    const start = text.indexOf("<", cursor);
    if (start === -1) {
      tokens.push({ kind: "text", start: cursor, end: text.length, text: text.slice(cursor) });
      break;
    }
    if (start > cursor) {
      tokens.push({ kind: "text", start: cursor, end: start, text: text.slice(cursor, start) });
    }
    const end = findDocTagEnd(text, start + 1);
    if (end === -1) {
      tokens.push({ kind: "text", start, end: text.length, text: text.slice(start) });
      break;
    }
    const raw = text.slice(start + 1, end).trim();
    const closing = raw.startsWith("/");
    const body = closing ? raw.slice(1).trim() : raw;
    const name = /^([A-Za-z][A-Za-z0-9_-]*)/.exec(body)?.[1];
    if (!name) {
      tokens.push({ kind: "text", start, end: end + 1, text: text.slice(start, end + 1) });
      cursor = end + 1;
      continue;
    }
    if (closing) {
      tokens.push({ kind: "end", start, end: end + 1, name });
    } else {
      const selfClosing = /\/\s*$/.test(body);
      const attributeText = body.slice(name.length).replace(/\/\s*$/, "");
      tokens.push({
        kind: "start",
        start,
        end: end + 1,
        name,
        attributes: parseDocAttributes(attributeText),
        selfClosing,
      });
    }
    cursor = end + 1;
  }
  return tokens;
}

function tokenizeDocTags(text: string): VbDocTagToken[] {
  return tokenizeDocXml(text).flatMap((token) => {
    if (token.kind === "text") {
      return [];
    }
    return {
      kind: token.kind,
      name: token.name,
      selfClosing: token.kind === "start" ? token.selfClosing : false,
    };
  });
}

function findDocTagEnd(text: string, offset: number): number {
  let quote: string | undefined;
  for (let index = offset; index < text.length; index += 1) {
    const char = text[index];
    if (quote) {
      if (char === quote) {
        quote = undefined;
      }
      continue;
    }
    if (char === '"' || char === "'") {
      quote = char;
      continue;
    }
    if (char === ">") {
      return index;
    }
  }
  return -1;
}

function parseDocAttributes(text: string): Record<string, string> {
  const attributes: Record<string, string> = {};
  const pattern = /([A-Za-z][A-Za-z0-9_-]*)\s*=\s*("[^"]*"|'[^']*'|[^\s"'>/]+)/g;
  for (const match of text.matchAll(pattern)) {
    const rawValue = match[2] ?? "";
    const value =
      (rawValue.startsWith('"') && rawValue.endsWith('"')) ||
      (rawValue.startsWith("'") && rawValue.endsWith("'"))
        ? rawValue.slice(1, -1)
        : rawValue;
    attributes[match[1].toLowerCase()] = decodeXmlEntities(value);
  }
  return attributes;
}

function docElements(root: VbDocElement, name: string): VbDocElement[] {
  return root.children.filter(
    (child): child is VbDocElement =>
      typeof child !== "string" && child.name.toLowerCase() === name.toLowerCase(),
  );
}

function firstDocElementText(
  root: VbDocElement,
  name: string,
  preserveTags = false,
): string | undefined {
  const element = docElements(root, name)[0];
  if (!element) {
    return undefined;
  }
  return docElementText(element, preserveTags);
}

function docElementText(element: VbDocElement, preserveCode = false): string {
  return normalizeDocText(docChildrenText(element.children, preserveCode));
}

function docChildrenText(children: Array<VbDocElement | string>, preserveCode = false): string {
  return children
    .map((child) => {
      if (typeof child === "string") {
        return decodeXmlEntities(child);
      }
      if (child.name === "c") {
        return `\`${docChildrenText(child.children, true).trim()}\``;
      }
      if (child.name === "code" && preserveCode) {
        return docChildrenText(child.children, true);
      }
      if (child.name === "see" || child.name === "seealso") {
        return child.attributes.langword ?? child.attributes.cref ?? child.attributes.href ?? "";
      }
      if (child.name === "para") {
        return `\n\n${docChildrenText(child.children, preserveCode)}\n\n`;
      }
      if (child.name === "list") {
        return docChildrenText(child.children, preserveCode);
      }
      return docChildrenText(child.children, preserveCode);
    })
    .join("");
}

function normalizeDocText(text: string): string {
  return text
    .replace(/[ \t]+\n/g, "\n")
    .replace(/\n{3,}/g, "\n\n")
    .trim();
}

function decodeXmlEntities(text: string): string {
  return text
    .replaceAll("&lt;", "<")
    .replaceAll("&gt;", ">")
    .replaceAll("&quot;", '"')
    .replaceAll("&apos;", "'")
    .replaceAll("&amp;", "&");
}

function hasDocumentationContent(documentation: VbDocumentation): boolean {
  return Boolean(
    documentation.summary ||
    documentation.remarks ||
    Object.keys(documentation.params).length > 0 ||
    documentation.returns ||
    documentation.value ||
    documentation.exceptions.length > 0 ||
    documentation.see.length > 0 ||
    documentation.seealso.length > 0 ||
    documentation.example ||
    documentation.code,
  );
}

function documentationMarkdown(
  documentation: VbDocumentation | undefined,
  locale: AspLocale | undefined = undefined,
): string | undefined {
  if (!documentation) {
    return undefined;
  }
  const localizer = createLocalizer(locale);
  const lines: string[] = [];
  if (documentation.summary) {
    lines.push(documentation.summary);
  }
  if (documentation.remarks) {
    lines.push(`**${localizer.t("vb.doc.heading.remarks")}**\n\n${documentation.remarks}`);
  }
  const params = Object.entries(documentation.params);
  if (params.length > 0) {
    lines.push(
      [
        `**${localizer.t("vb.doc.heading.parameters")}**`,
        ...params.map(([name, text]) => `- \`${name}\`: ${text}`),
      ].join("\n"),
    );
  }
  if (documentation.returns) {
    lines.push(`**${localizer.t("vb.doc.heading.returns")}**\n\n${documentation.returns}`);
  }
  if (documentation.value) {
    lines.push(`**${localizer.t("vb.doc.heading.value")}**\n\n${documentation.value}`);
  }
  if (documentation.exceptions.length > 0) {
    lines.push(
      [
        `**${localizer.t("vb.doc.heading.exceptions")}**`,
        ...documentation.exceptions.map((item) =>
          item.cref ? `- \`${item.cref}\`: ${item.text}` : `- ${item.text}`,
        ),
      ].join("\n"),
    );
  }
  if (documentation.see.length > 0 || documentation.seealso.length > 0) {
    lines.push(
      [
        `**${localizer.t("vb.doc.heading.seeAlso")}**`,
        ...[...documentation.see, ...documentation.seealso].map((item) => {
          const target = item.text || item.cref || item.href || item.langword || "";
          return target.startsWith("http") ? `- ${target}` : `- \`${target}\``;
        }),
      ].join("\n"),
    );
  }
  if (documentation.example) {
    lines.push(`**${localizer.t("vb.doc.heading.example")}**\n\n${documentation.example}`);
  }
  if (documentation.code) {
    lines.push(`\`\`\`vbscript\n${documentation.code}\n\`\`\``);
  }
  if (hasDocumentationTypeLikeContent(documentation)) {
    lines.push(`_${localizer.t("vb.doc.typeNote")}_`);
  }
  return lines.filter(Boolean).join("\n\n");
}

function hasDocumentationTypeLikeContent(documentation: VbDocumentation): boolean {
  return Boolean(
    Object.keys(documentation.params).length > 0 || documentation.returns || documentation.value,
  );
}

function appendDocumentationMarkdown(
  base: string,
  documentation: VbDocumentation | undefined,
  locale: AspLocale | undefined = undefined,
): string {
  const markdown = documentationMarkdown(documentation, locale);
  return markdown ? `${base}\n\n${markdown}` : base;
}

function nextProcedureName(parsed: AspParsedDocument, offset: number): string | undefined {
  return vbDocuments(parsed)
    .flatMap((document) => flattenVbNodes(document))
    .filter(
      (node) => node.start >= offset && (node.kind === "Procedure" || node.kind === "Property"),
    )
    .sort((left, right) => left.start - right.start)[0]?.nameToken?.text;
}

function vbStatements(parsed: AspParsedDocument): VbToken[][] {
  const statements: VbToken[][] = [];
  for (const document of vbDocuments(parsed)) {
    let current: VbToken[] = [];
    for (const token of document.tokens.filter(
      (item) => item.kind !== "whitespace" && item.kind !== "comment",
    )) {
      if (token.kind === "newline" || token.text === ":") {
        if (current.length > 0) {
          statements.push(current);
          current = [];
        }
        continue;
      }
      current.push(token);
    }
    if (current.length > 0) {
      statements.push(current);
    }
  }
  return statements;
}

function inferExpressionType(
  parsed: AspParsedDocument,
  tokens: VbToken[],
  symbols: VbSymbol[],
  env: VbTypeEnvironment,
  offset: number,
): VbTypeRef | undefined {
  const significant = tokens.filter((token) => !isTriviaToken(token));
  return inferSignificantExpressionType(parsed, significant, symbols, env, offset);
}

function inferSignificantExpressionType(
  parsed: AspParsedDocument,
  significant: VbToken[],
  symbols: VbSymbol[],
  env: VbTypeEnvironment,
  offset: number,
): VbTypeRef | undefined {
  const expression = trimOuterParens(significant);
  const binary = splitByLowestPrecedenceOperator(expression);
  if (binary) {
    const left = inferSignificantExpressionType(parsed, binary.left, symbols, env, offset);
    const right = inferSignificantExpressionType(parsed, binary.right, symbols, env, offset);
    return inferBinaryExpressionType(binary.operator, left, right);
  }
  const first = expression[0];
  if (!first) {
    return undefined;
  }
  if (first.kind === "string") {
    return typeRef("String");
  }
  if (first.kind === "number") {
    return typeRef("Number");
  }
  if (first.text === "#" && expression.at(-1)?.text === "#") {
    return typeRef("Date");
  }
  const lower = first.text.toLowerCase();
  if (lower === "true" || lower === "false") {
    return typeRef("Boolean");
  }
  if (lower === "nothing" || lower === "null" || lower === "empty") {
    return typeRef(lower === "nothing" ? "Nothing" : "Variant");
  }
  if (lower === "array" && expression[1]?.text === "(") {
    return typeRef("Array");
  }
  if (lower === "new" && expression[1]?.kind === "identifier") {
    return typeRef(expression[1].text);
  }
  const createObjectIndex = findCreateObjectCall(expression, 0, expression.length - 1);
  if (createObjectIndex !== -1) {
    const stringToken = expression
      .slice(createObjectIndex)
      .find((token) => token.kind === "string");
    return stringToken
      ? typeRef(stringToken.value ?? unquoteVbString(stringToken.text))
      : undefined;
  }
  if (
    first.kind === "identifier" &&
    expression[1]?.text === "." &&
    expression[2]?.kind === "identifier"
  ) {
    const ownerType = inferVariableType(first.text, parsed, offset, symbols);
    return ownerType
      ? (memberReturnType(ownerType, expression[2].text, env) ??
          memberType(ownerType, expression[2].text, env))
      : undefined;
  }
  if (first.kind === "identifier") {
    const called = expression[1]?.text === "(";
    if (called) {
      const builtin = builtinSignature(first.text);
      if (builtin) {
        return builtin.returnType;
      }
      const symbol = visibleSymbols(parsed, offset, symbols).find(
        (candidate) =>
          candidate.name.toLowerCase() === first.text.toLowerCase() &&
          (candidate.kind === "function" ||
            candidate.kind === "method" ||
            candidate.kind === "property"),
      );
      return symbol?.type ?? (symbol?.typeName ? typeRef(symbol.typeName) : undefined);
    }
    const typeName = inferVariableType(first.text, parsed, offset, symbols);
    return typeName ? typeRef(typeName) : undefined;
  }
  return undefined;
}

function trimOuterParens(tokens: VbToken[]): VbToken[] {
  let result = tokens;
  while (result[0]?.text === "(" && result.at(-1)?.text === ")") {
    const closeIndex = matchingCloseParen(result, 0);
    if (closeIndex !== result.length - 1) {
      break;
    }
    result = result.slice(1, -1);
  }
  return result;
}

function splitByLowestPrecedenceOperator(
  tokens: VbToken[],
): { left: VbToken[]; operator: string; right: VbToken[] } | undefined {
  const operators = [
    ["or", "xor", "eqv", "imp"],
    ["and"],
    ["=", "<>", "<", ">", "<=", ">=", "is"],
    ["&"],
    ["+", "-"],
    ["mod"],
    ["*", "/"],
    ["\\"],
    ["^"],
  ];
  for (const group of operators) {
    let depth = 0;
    for (let index = tokens.length - 1; index >= 0; index -= 1) {
      const token = tokens[index];
      if (token.text === ")") {
        depth += 1;
        continue;
      }
      if (token.text === "(") {
        depth -= 1;
        continue;
      }
      const operator = token.text.toLowerCase();
      if (depth === 0 && group.includes(operator) && index > 0 && index < tokens.length - 1) {
        return {
          left: tokens.slice(0, index),
          operator,
          right: tokens.slice(index + 1),
        };
      }
    }
  }
  return undefined;
}

function inferBinaryExpressionType(
  operator: string,
  left: VbTypeRef | undefined,
  right: VbTypeRef | undefined,
): VbTypeRef | undefined {
  if (
    ["=", "<>", "<", ">", "<=", ">=", "is", "and", "or", "xor", "eqv", "imp"].includes(operator)
  ) {
    return typeRef("Boolean");
  }
  if (operator === "&") {
    return typeRef("String");
  }
  if (operator === "+" && (left?.name === "String" || right?.name === "String")) {
    return typeRef("String");
  }
  if (["+", "-", "*", "/", "\\", "mod", "^"].includes(operator)) {
    return typeRef("Number");
  }
  return left ?? right;
}

function applyTypeAnnotations(parsed: AspParsedDocument, symbols: VbSymbol[]): void {
  const annotations = parseTypeAnnotations(parsed);
  for (const annotation of annotations.types) {
    const symbol =
      visibleSymbols(parsed, annotation.offset, symbols).find(
        (candidate) => candidate.name.toLowerCase() === annotation.name.toLowerCase(),
      ) ??
      symbols.find((candidate) => candidate.name.toLowerCase() === annotation.name.toLowerCase());
    if (symbol) {
      setSymbolType(symbol, annotation.typeName);
    }
  }
  for (const annotation of annotations.params) {
    const symbol = symbols.find(
      (candidate) =>
        (candidate.kind === "variable" || candidate.kind === "parameter") &&
        candidate.name.toLowerCase() === annotation.name.toLowerCase() &&
        (!annotation.procedureName ||
          candidate.scopeName?.toLowerCase() === annotation.procedureName.toLowerCase()),
    );
    if (symbol) {
      setSymbolType(symbol, annotation.typeName);
    }
  }
  for (const annotation of annotations.returns) {
    const symbol = symbols.find(
      (candidate) =>
        (candidate.kind === "function" || candidate.kind === "method") &&
        candidate.name.toLowerCase() === annotation.name.toLowerCase(),
    );
    if (symbol) {
      setSymbolType(symbol, annotation.typeName);
    }
  }
}

function vbDocuments(parsed: AspParsedDocument): VbCstNode[] {
  const documents: VbCstNode[] = [];
  const visit = (node: AspCstNode): void => {
    if (node.vbscript) {
      documents.push(node.vbscript);
    }
    for (const child of node.children ?? []) {
      visit(child);
    }
  };
  visit(parsed.cst);
  if (documents.length === 0) {
    for (const region of serverRegions(parsed)) {
      documents.push(
        parseVbscriptCst(
          parsed.text.slice(region.contentStart, region.contentEnd),
          parsed.text,
          region.contentStart,
        ),
      );
    }
  }
  return documents;
}

function flattenVbNodes(node: VbCstNode): VbCstNode[] {
  return [node, ...node.children.flatMap((child) => flattenVbNodes(child))];
}

function parentClassName(parsed: AspParsedDocument, offset: number): string | undefined {
  return enclosingVbNodes(parsed, offset)
    .reverse()
    .find((node) => node.kind === "Class")?.nameToken?.text;
}

function scopeNodeAt(parsed: AspParsedDocument, offset: number): VbCstNode | undefined {
  return enclosingVbNodes(parsed, offset)
    .reverse()
    .find((node) => node.kind === "Procedure" || node.kind === "Property");
}

function enclosingVbNodes(parsed: AspParsedDocument, offset: number): VbCstNode[] {
  const result: VbCstNode[] = [];
  const visit = (node: VbCstNode): void => {
    if (offset < node.start || offset > node.end) {
      return;
    }
    result.push(node);
    for (const child of node.children) {
      visit(child);
    }
  };
  for (const document of vbDocuments(parsed)) {
    visit(document);
  }
  return result;
}

function currentClassName(
  parsed: AspParsedDocument,
  offset: number,
  symbols: VbSymbol[],
): string | undefined {
  return symbols.find(
    (symbol) =>
      symbol.kind === "class" &&
      symbol.sourceUri === parsed.uri &&
      rangeContainsOffset(parsed.text, symbol.scopeRange, offset),
  )?.name;
}

function inferVariableType(
  name: string,
  parsed: AspParsedDocument,
  offset: number,
  symbols: VbSymbol[],
): string | undefined {
  return visibleSymbols(parsed, offset, symbols)
    .filter((symbol) => symbol.name.toLowerCase() === name.toLowerCase())
    .sort(
      (left, right) =>
        Number(Boolean(right.typeName)) - Number(Boolean(left.typeName)) ||
        symbolPriority(right) - symbolPriority(left),
    )[0]?.typeName;
}

function currentWithTypeName(
  parsed: AspParsedDocument,
  offset: number,
  symbols: VbSymbol[],
): string | undefined {
  const withNode = enclosingVbNodes(parsed, offset)
    .reverse()
    .find((node) => node.kind === "With" && node.nameToken);
  return withNode?.nameToken
    ? inferVariableType(withNode.nameToken.text, parsed, offset, symbols)
    : undefined;
}

function typeMemberCompletions(
  typeName: string,
  symbols: VbSymbol[],
  env: VbTypeEnvironment,
): CompletionItem[] {
  const type = findType(env, typeName);
  return dedupeCompletions([
    ...(type?.members.map(memberToCompletion) ?? []),
    ...symbols
      .filter(
        (symbol) =>
          symbol.memberOf?.toLowerCase() === typeName.toLowerCase() &&
          (symbol.kind === "method" || symbol.kind === "field" || symbol.kind === "property"),
      )
      .map((symbol) => symbolToCompletion(symbol)),
    ...(externalObjectMembers[typeName.toLowerCase()] ?? []),
  ]);
}

function memberToCompletion(member: VbMember): CompletionItem {
  return {
    label: member.name,
    kind:
      member.kind === "method"
        ? CompletionItemKind.Method
        : member.kind === "field"
          ? CompletionItemKind.Field
          : CompletionItemKind.Property,
    detail: member.type ? `${member.kind} As ${member.type.name}` : member.kind,
  };
}

function visibleSymbols(
  parsed: AspParsedDocument,
  offset: number,
  symbols: VbSymbol[],
): VbSymbol[] {
  return symbols.filter((symbol) => isSymbolVisibleAt(symbol, parsed.uri, parsed.text, offset));
}

function isSymbolVisibleAt(
  symbol: VbSymbol,
  uri: string,
  sourceText: string,
  offset: number,
): boolean {
  if (symbol.sourceUri !== uri) {
    return !symbol.scopeName && !symbol.memberOf;
  }
  if (
    (symbol.kind === "class" || symbol.kind === "function" || symbol.kind === "sub") &&
    !symbol.memberOf
  ) {
    return true;
  }
  if (!symbol.scopeRange) {
    return true;
  }
  return rangeContainsOffset(sourceText, symbol.scopeRange, offset);
}

function resolveSymbolAt(
  parsed: AspParsedDocument,
  offset: number,
  symbols: VbSymbol[],
): VbSymbol | undefined {
  const member = memberAccessAt(parsed, offset);
  if (member) {
    const className =
      member.owner === ""
        ? currentWithTypeName(parsed, offset, symbols)
        : member.owner.toLowerCase() === "me"
          ? currentClassName(parsed, offset, symbols)
          : inferVariableType(member.owner, parsed, offset, symbols);
    return className
      ? symbols.find(
          (symbol) =>
            symbol.memberOf?.toLowerCase() === className.toLowerCase() &&
            symbol.name.toLowerCase() === member.member.toLowerCase(),
        )
      : undefined;
  }
  const token = identifierTokenAt(parsed, offset);
  if (!token) {
    return undefined;
  }
  return visibleSymbols(parsed, offset, symbols)
    .filter((symbol) => symbol.name.toLowerCase() === token.text.toLowerCase())
    .sort((left, right) => symbolPriority(right) - symbolPriority(left))[0];
}

function memberAccessAt(
  parsed: AspParsedDocument,
  offset: number,
): { owner: string; member: string } | undefined {
  const token = identifierTokenAt(parsed, offset);
  if (!token) {
    return undefined;
  }
  const tokens = significantTokens(parsed);
  const index = tokens.findIndex((item) => item.start === token.start && item.end === token.end);
  if (tokens[index - 1]?.text !== ".") {
    return undefined;
  }
  const owner = tokens[index - 2];
  return owner?.kind === "identifier" || owner?.text.toLowerCase() === "me"
    ? { owner: owner.text, member: token.text }
    : { owner: "", member: token.text };
}

function diagnoseUndeclaredVariables(
  parsed: AspParsedDocument,
  symbols: VbSymbol[],
  locale: AspLocale | undefined,
): Diagnostic[] {
  const localizer = createLocalizer(locale);
  const declaredBuiltins = new Set([
    "request",
    "response",
    "session",
    "application",
    "server",
    "asperror",
    "true",
    "false",
    "nothing",
    "empty",
    "null",
    "me",
  ]);
  const diagnostics: Diagnostic[] = [];
  for (const token of identifierTokens(parsed)) {
    const name = token.text;
    const lower = name.toLowerCase();
    const previous = previousSignificantToken(parsed, token.start);
    const next = nextSignificantToken(parsed, token.end);
    if (
      declaredBuiltins.has(lower) ||
      visibleSymbols(parsed, token.start, symbols).some(
        (symbol) => symbol.name.toLowerCase() === lower,
      ) ||
      isBuiltinName(name) ||
      isDeclarationNameToken(parsed, token) ||
      previous?.text === "."
    ) {
      continue;
    }
    if (next?.text === "(" && /^[A-Z]/.test(name)) {
      continue;
    }
    diagnostics.push({
      severity: DiagnosticSeverity.Warning,
      range: rangeFromOffsets(parsed.text, token.start, token.end),
      message: localizer.t("vb.diagnostic.undeclared", { name }),
      source: "asp-lsp-vbscript",
    });
  }
  return diagnostics;
}

function diagnoseUnusedSymbols(
  parsed: AspParsedDocument,
  symbols: VbSymbol[],
  context: VbProjectContext,
): Diagnostic[] {
  const diagnostics: Diagnostic[] = [];
  for (const symbol of symbols) {
    if (
      symbol.sourceUri !== parsed.uri ||
      isBuiltinName(symbol.name) ||
      isRuntimeEntryPoint(parsed, symbol) ||
      !isUnusedDiagnosticCandidate(symbol)
    ) {
      continue;
    }
    const references = getVbscriptReferences(parsed, symbol.range.start, {
      ...context,
      symbols,
    }).filter((reference) => !sameRange(reference.range, symbol.range));
    if (references.length > 0) {
      continue;
    }
    diagnostics.push({
      severity: DiagnosticSeverity.Hint,
      range: symbol.range,
      message: unusedDiagnosticMessage(symbol, context.locale),
      source: "asp-lsp-vbscript-unused",
    });
  }
  return diagnostics;
}

function isUnusedDiagnosticCandidate(symbol: VbSymbol): boolean {
  if (symbol.implicit) {
    return false;
  }
  if (symbol.memberOf) {
    return symbol.visibility === "private";
  }
  return ["variable", "parameter", "constant", "function", "sub", "class"].includes(symbol.kind);
}

function unusedDiagnosticMessage(symbol: VbSymbol, locale: AspLocale | undefined): string {
  const localizer = createLocalizer(locale);
  if (symbol.kind === "parameter") {
    return localizer.t("vb.diagnostic.unusedParameter", { name: symbol.name });
  }
  return localizer.t("vb.diagnostic.unusedSymbol", { name: symbol.name });
}

function diagnoseIdentifierCase(
  parsed: AspParsedDocument,
  symbols: VbSymbol[],
  context: VbProjectContext,
): Diagnostic[] {
  const localizer = createLocalizer(context.locale);
  return symbols
    .filter(
      (symbol) =>
        symbol.sourceUri === parsed.uri &&
        /^[A-Za-z][A-Za-z0-9_]*$/.test(symbol.name) &&
        !symbol.implicit &&
        !isRuntimeEntryPoint(parsed, symbol) &&
        !isBuiltinName(symbol.name),
    )
    .flatMap((symbol): Diagnostic[] => {
      const style = identifierCaseForSymbol(symbol, context);
      if (style === "ignore") {
        return [];
      }
      const expectedName = formatIdentifierCase(symbol.name, style);
      return expectedName && expectedName !== symbol.name
        ? [
            {
              severity: DiagnosticSeverity.Hint,
              range: symbol.range,
              message: localizer.t("vb.diagnostic.identifierCase", {
                name: symbol.name,
                expectedName,
                style,
              }),
              source: "asp-lsp-vbscript-naming",
              code: "identifierCase",
              data: {
                name: symbol.name,
                expectedName,
                style,
              },
            },
          ]
        : [];
    });
}

function identifierCaseForSymbol(
  symbol: VbSymbol,
  context: VbProjectContext,
): AspVbscriptIdentifierCase {
  const kind = identifierKindForSymbol(symbol);
  return (
    context.identifierCaseByKind?.[kind] ??
    context.identifierCase ??
    defaultIdentifierCaseForKind(kind)
  );
}

function identifierKindForSymbol(symbol: VbSymbol): AspVbscriptIdentifierKind {
  return symbol.kind === "sub" ? "sub" : symbol.kind;
}

function defaultIdentifierCaseForKind(kind: AspVbscriptIdentifierKind): AspVbscriptIdentifierCase {
  return kind === "variable" || kind === "parameter" ? "camel" : "pascal";
}

function formatIdentifierCase(
  name: string,
  style: Exclude<AspVbscriptIdentifierCase, "ignore">,
): string | undefined {
  const words = identifierWords(name);
  if (words.length === 0) {
    return undefined;
  }
  switch (style) {
    case "upper":
      return words.join("").toUpperCase();
    case "lower":
      return words.join("").toLowerCase();
    case "camel":
      return [words[0]?.toLowerCase(), ...words.slice(1).map(capitalizeWord)].join("");
    case "pascal":
      return words.map(capitalizeWord).join("");
    case "snake":
      return words.join("_");
    case "upperSnake":
      return words.join("_").toUpperCase();
  }
}

function identifierWords(name: string): string[] {
  return name
    .split("_")
    .flatMap((part) => part.match(/[A-Z]+(?=[A-Z][a-z])|[A-Z]?[a-z]+|[A-Z]+|\d+/g) ?? [])
    .map((word) => word.toLowerCase())
    .filter(Boolean);
}

function capitalizeWord(word: string): string {
  return word.length === 0 ? word : `${word[0]?.toUpperCase()}${word.slice(1).toLowerCase()}`;
}

function isRuntimeEntryPoint(parsed: AspParsedDocument, symbol: VbSymbol): boolean {
  if (symbol.memberOf || (symbol.kind !== "sub" && symbol.kind !== "function")) {
    return false;
  }
  const normalizedUri = parsed.uri.toLowerCase();
  if (!normalizedUri.endsWith("/global.asa") && !normalizedUri.endsWith("\\global.asa")) {
    return false;
  }
  return new Set([
    "application_onstart",
    "application_onend",
    "session_onstart",
    "session_onend",
  ]).has(symbol.name.toLowerCase());
}

function getServerScriptText(parsed: AspParsedDocument): string {
  return serverRegions(parsed)
    .map((region) => parsed.text.slice(region.contentStart, region.contentEnd))
    .join("\n");
}

function serverRegions(parsed: AspParsedDocument): AspRegion[] {
  return parsed.regions.filter((region) => region.language === "vbscript");
}

function symbolToCompletion(
  symbol: VbSymbol,
  locale: AspLocale | undefined = undefined,
): CompletionItem {
  const kind =
    symbol.kind === "variable" || symbol.kind === "parameter"
      ? CompletionItemKind.Variable
      : symbol.kind === "constant"
        ? CompletionItemKind.Constant
        : symbol.kind === "class"
          ? CompletionItemKind.Class
          : symbol.kind === "field"
            ? CompletionItemKind.Field
            : symbol.kind === "property"
              ? CompletionItemKind.Property
              : CompletionItemKind.Function;
  const detail = symbol.memberOf
    ? `${symbol.kind}${createLocalizer(locale).t("vb.symbol.owner", { owner: symbol.memberOf })}`
    : symbol.typeName
      ? `${symbol.kind} As ${symbol.typeName}`
      : symbol.kind;
  return {
    label: symbol.name,
    kind,
    detail,
    documentation: documentationMarkdown(symbol.documentation, locale),
  };
}

function dedupeCompletions(items: CompletionItem[]): CompletionItem[] {
  const seen = new Set<string>();
  return items.filter((item) => {
    const key = item.label.toLowerCase();
    if (seen.has(key)) {
      return false;
    }
    seen.add(key);
    return true;
  });
}

function significantTokens(parsed: AspParsedDocument): VbToken[] {
  return vbDocuments(parsed)
    .flatMap((document) => document.tokens)
    .filter((token) => !isTriviaToken(token));
}

function identifierTokens(parsed: AspParsedDocument): VbToken[] {
  return significantTokens(parsed).filter((token) => token.kind === "identifier");
}

function identifierTokenAt(parsed: AspParsedDocument, offset: number): VbToken | undefined {
  return identifierTokens(parsed).find((token) => offset >= token.start && offset <= token.end);
}

function previousSignificantToken(parsed: AspParsedDocument, offset: number): VbToken | undefined {
  return significantTokens(parsed)
    .filter((token) => token.end <= offset)
    .at(-1);
}

function nextSignificantToken(parsed: AspParsedDocument, offset: number): VbToken | undefined {
  return significantTokens(parsed).find((token) => token.start >= offset);
}

function isDeclarationNameToken(parsed: AspParsedDocument, token: VbToken): boolean {
  return vbDocuments(parsed)
    .flatMap((document) => flattenVbNodes(document))
    .some(
      (node) =>
        ((node.kind === "Class" || node.kind === "Procedure" || node.kind === "Property") &&
          node.nameToken === token) ||
        node.parameters?.includes(token) ||
        node.identifiers?.includes(token),
    );
}

function callExpressionAt(
  parsed: AspParsedDocument,
  offset: number,
): { name: string; argumentsStart: number } | undefined {
  const tokens = significantTokens(parsed).filter((token) => token.start < offset);
  let depth = 0;
  for (let index = tokens.length - 1; index >= 0; index -= 1) {
    const current = tokens[index];
    if (current.text === ")") {
      depth += 1;
      continue;
    }
    if (current.text !== "(") {
      continue;
    }
    if (depth > 0) {
      depth -= 1;
      continue;
    }
    const name = callNameBefore(tokens, index);
    return name ? { name, argumentsStart: current.end } : undefined;
  }
  return undefined;
}

function callNameBefore(tokens: VbToken[], openParenIndex: number): string | undefined {
  const before = tokens[openParenIndex - 1];
  if (!before || before.kind !== "identifier") {
    return undefined;
  }
  if (
    tokens[openParenIndex - 2]?.text === "." &&
    tokens[openParenIndex - 3]?.kind === "identifier"
  ) {
    return `${tokens[openParenIndex - 3].text}.${before.text}`;
  }
  return before.text;
}

function countActiveParameter(parsed: AspParsedDocument, start: number, offset: number): number {
  const tokens = significantTokens(parsed).filter(
    (token) => token.start >= start && token.end <= offset,
  );
  let depth = 0;
  let count = 0;
  for (const token of tokens) {
    if (token.text === "(") {
      depth += 1;
    } else if (token.text === ")" && depth > 0) {
      depth -= 1;
    } else if (token.text === "," && depth === 0) {
      count += 1;
    }
  }
  return count;
}

function signatureSymbolsForCall(
  parsed: AspParsedDocument,
  name: string,
  offset: number,
  symbols: VbSymbol[],
): VbSymbol[] {
  const [owner, member] = name.includes(".") ? name.split(".", 2) : [undefined, name];
  if (owner && member) {
    const className =
      owner.toLowerCase() === "me"
        ? currentClassName(parsed, offset, symbols)
        : inferVariableType(owner, parsed, offset, symbols);
    if (!className) {
      return [];
    }
    return symbols.filter(
      (symbol) =>
        symbol.memberOf?.toLowerCase() === className.toLowerCase() &&
        symbol.name.toLowerCase() === member.toLowerCase() &&
        (symbol.kind === "method" || symbol.kind === "property"),
    );
  }
  return visibleSymbols(parsed, offset, symbols).filter(
    (symbol) =>
      symbol.name.toLowerCase() === name.toLowerCase() &&
      (symbol.kind === "function" || symbol.kind === "sub"),
  );
}

function typeSignatureLabelsForCall(
  parsed: AspParsedDocument,
  name: string,
  offset: number,
  symbols: VbSymbol[],
  env: VbTypeEnvironment,
): string[] | undefined {
  const [owner, member] = name.includes(".") ? name.split(".", 2) : [undefined, name];
  if (owner && member) {
    const typeName =
      owner.toLowerCase() === "me"
        ? currentClassName(parsed, offset, symbols)
        : inferVariableType(owner, parsed, offset, symbols);
    const signature = typeName ? memberSignature(typeName, member, env) : undefined;
    return signature ? [signatureLabelFromMember(owner, member, signature)] : undefined;
  }
  const symbol = visibleSymbols(parsed, offset, symbols).find(
    (candidate) =>
      candidate.name.toLowerCase() === name.toLowerCase() &&
      (candidate.kind === "function" || candidate.kind === "sub"),
  );
  if (!symbol?.type && !symbol?.typeName) {
    return undefined;
  }
  return [signatureLabel(symbol)];
}

function signatureLabelFromMember(owner: string, name: string, signature: VbSignature): string {
  const parameters = signature.parameters.map((parameter) => {
    const prefix = parameter.mode
      ? `${parameter.optional ? "Optional " : ""}${parameterModeKeyword(parameter.mode)} `
      : "";
    return parameter.type
      ? `${prefix}${parameter.name} As ${parameter.type.name}`
      : `${prefix}${parameter.name}`;
  });
  return `${owner}.${name}(${parameters.join(", ")})`;
}

function signatureLabel(symbol: VbSymbol): string {
  const keyword = symbol.kind === "sub" || symbol.kind === "method" ? "Sub" : "Function";
  const owner = symbol.memberOf ? `${symbol.memberOf}.` : "";
  const returnType = symbol.type ?? (symbol.typeName ? typeRef(symbol.typeName) : undefined);
  return `${keyword} ${owner}${symbol.name}(${parameterLabels(symbol).join(", ")})${
    returnType && keyword === "Function" ? ` As ${returnType.name}` : ""
  }`;
}

function symbolToSignatureInformation(symbol: VbSymbol, locale: AspLocale | undefined) {
  return {
    label: signatureLabel(symbol),
    documentation: documentationMarkdown(symbol.documentation, locale),
    parameters: parameterDetails(symbol).map((parameter) => ({
      label: parameterLabel(parameter),
      documentation: symbol.documentation?.params[parameter.name],
    })),
  };
}

function diagnoseTypeIssues(
  parsed: AspParsedDocument,
  symbols: VbSymbol[],
  env: VbTypeEnvironment,
  locale: AspLocale | undefined,
): Diagnostic[] {
  const diagnostics: Diagnostic[] = [];
  for (const statement of vbStatements(parsed)) {
    diagnostics.push(...diagnoseAssignmentTypes(parsed, statement, symbols, env, locale));
    diagnostics.push(...diagnoseCallTypes(parsed, statement, symbols, env, locale));
    diagnostics.push(...diagnoseMemberAccess(parsed, statement, symbols, env, locale));
  }
  return diagnostics;
}

function diagnoseAssignmentTypes(
  parsed: AspParsedDocument,
  statement: VbToken[],
  symbols: VbSymbol[],
  env: VbTypeEnvironment,
  locale: AspLocale | undefined,
): Diagnostic[] {
  const localizer = createLocalizer(locale);
  const first = lowerToken(statement[0]);
  const isSet = first === "set";
  const targetIndex = isSet ? 1 : 0;
  const target = statement[targetIndex];
  const equalsIndex = statement.findIndex((token) => token.text === "=");
  if (!target || target.kind !== "identifier" || equalsIndex === -1) {
    return [];
  }
  const lhsTypeName = inferVariableType(target.text, parsed, target.start, symbols);
  const rhsType = inferExpressionType(
    parsed,
    statement.slice(equalsIndex + 1),
    symbols,
    env,
    target.start,
  );
  const diagnostics: Diagnostic[] = [];
  if (isSet && rhsType && isClearlyScalarType(rhsType)) {
    diagnostics.push(
      typeWarning(
        parsed,
        target.start,
        statement.at(-1)?.end ?? target.end,
        localizer.t("vb.diagnostic.setScalar", { name: target.text, type: rhsType.name }),
        "setScalar",
        { name: target.text, type: rhsType.name },
      ),
    );
  }
  if (!isSet && rhsType && isClearlyObjectType(rhsType, env)) {
    diagnostics.push(
      typeWarning(
        parsed,
        target.start,
        statement.at(-1)?.end ?? target.end,
        localizer.t("vb.diagnostic.objectNeedsSet", { name: target.text }),
        "objectNeedsSet",
        { name: target.text, type: rhsType.name },
      ),
    );
  }
  if (lhsTypeName && rhsType && !isCompatibleType(typeRef(lhsTypeName), rhsType, env)) {
    diagnostics.push(
      typeWarning(
        parsed,
        target.start,
        statement.at(-1)?.end ?? target.end,
        localizer.t("vb.diagnostic.typeMismatch", {
          name: target.text,
          expected: lhsTypeName,
          actual: rhsType.name,
        }),
        "typeMismatch",
        { name: target.text, expected: lhsTypeName, actual: rhsType.name },
      ),
    );
  }
  return diagnostics;
}

function diagnoseCallTypes(
  parsed: AspParsedDocument,
  statement: VbToken[],
  symbols: VbSymbol[],
  env: VbTypeEnvironment,
  locale: AspLocale | undefined,
): Diagnostic[] {
  const diagnostics: Diagnostic[] = [];
  const localizer = createLocalizer(locale);
  for (let index = 0; index < statement.length; index += 1) {
    if (statement[index].text !== "(" || statement[index - 1]?.kind !== "identifier") {
      continue;
    }
    const name = callNameBefore(statement, index);
    if (!name) {
      continue;
    }
    const signature = signatureForCall(parsed, name, statement[index].start, symbols, env);
    if (!signature) {
      const callName = name.split(".").at(-1) ?? name;
      if (!isLikelyDynamicCall(callName)) {
        diagnostics.push(
          typeWarning(
            parsed,
            statement[index - 1].start,
            statement[index - 1].end,
            localizer.t("vb.diagnostic.unknownCall", { name }),
            "unknownCall",
            { name },
          ),
        );
      }
      continue;
    }
    const closeIndex = matchingCloseParen(statement, index);
    const argumentCount = countArguments(
      statement.slice(index + 1, closeIndex === -1 ? undefined : closeIndex),
    );
    if (argumentCount !== signature.parameters.length) {
      diagnostics.push(
        typeWarning(
          parsed,
          statement[index - 1].start,
          statement[index - 1].end,
          localizer.t("vb.diagnostic.argumentCountMismatch", {
            name,
            expected: signature.parameters.length,
            actual: argumentCount,
          }),
          "argumentCountMismatch",
          { name, expected: signature.parameters.length, actual: argumentCount },
        ),
      );
    }
  }
  return diagnostics;
}

function diagnoseMemberAccess(
  parsed: AspParsedDocument,
  statement: VbToken[],
  symbols: VbSymbol[],
  env: VbTypeEnvironment,
  locale: AspLocale | undefined,
): Diagnostic[] {
  const diagnostics: Diagnostic[] = [];
  const localizer = createLocalizer(locale);
  for (let index = 1; index + 1 < statement.length; index += 1) {
    if (statement[index].text !== "." || statement[index + 1]?.kind !== "identifier") {
      continue;
    }
    const owner = statement[index - 1];
    const member = statement[index + 1];
    const ownerTypeName =
      owner.kind === "identifier"
        ? inferVariableType(owner.text, parsed, owner.start, symbols)
        : undefined;
    if (!ownerTypeName || isLooseType(ownerTypeName)) {
      continue;
    }
    const type = findType(env, ownerTypeName);
    if (
      type &&
      !type.members.some((item) => item.name.toLowerCase() === member.text.toLowerCase())
    ) {
      diagnostics.push(
        typeWarning(
          parsed,
          member.start,
          member.end,
          localizer.t("vb.diagnostic.missingMember", {
            type: ownerTypeName,
            member: member.text,
          }),
          "missingMember",
          { type: ownerTypeName, member: member.text },
        ),
      );
    }
  }
  return diagnostics;
}

function signatureForCall(
  parsed: AspParsedDocument,
  name: string,
  offset: number,
  symbols: VbSymbol[],
  env: VbTypeEnvironment,
): VbSignature | undefined {
  const [owner, member] = name.includes(".") ? name.split(".", 2) : [undefined, name];
  if (owner && member) {
    const typeName =
      owner.toLowerCase() === "me"
        ? currentClassName(parsed, offset, symbols)
        : inferVariableType(owner, parsed, offset, symbols);
    return typeName ? memberSignature(typeName, member, env) : undefined;
  }
  const symbol = visibleSymbols(parsed, offset, symbols).find(
    (candidate) =>
      candidate.name.toLowerCase() === name.toLowerCase() &&
      (candidate.kind === "function" || candidate.kind === "sub"),
  );
  if (!symbol) {
    return undefined;
  }
  return {
    parameters: parameterDetails(symbol).map((parameter) => ({
      name: parameter.name,
      mode: parameter.mode,
      optional: parameter.optional,
    })),
    returnType: symbol.type ?? (symbol.typeName ? typeRef(symbol.typeName) : undefined),
  };
}

function memberSignature(
  typeName: string,
  memberName: string,
  env: VbTypeEnvironment,
): VbSignature | undefined {
  return findType(env, typeName)?.members.find(
    (member) => member.name.toLowerCase() === memberName.toLowerCase(),
  )?.signature;
}

function memberType(
  typeName: string,
  memberName: string,
  env: VbTypeEnvironment,
): VbTypeRef | undefined {
  return findType(env, typeName)?.members.find(
    (member) => member.name.toLowerCase() === memberName.toLowerCase(),
  )?.type;
}

function memberReturnType(
  typeName: string,
  memberName: string,
  env: VbTypeEnvironment,
): VbTypeRef | undefined {
  return memberSignature(typeName, memberName, env)?.returnType;
}

function builtinTypes(): VbType[] {
  const intrinsic: VbType[] = [
    "String",
    "Number",
    "Boolean",
    "Date",
    "Object",
    "Variant",
    "Nothing",
    "Array",
    "Unknown",
  ].map((name) => ({ name, kind: "intrinsic" as const, members: [] }));
  const classicAsp: VbType[] = Object.entries(memberCompletions).map(([name, members]) => ({
    name: canonicalBuiltinTypeName(name),
    kind: "classicAsp",
    members: members.map((member) => {
      const signature = builtinSignature(`${name}.${member.label}`);
      return {
        name: member.label,
        kind: signature ? "method" : "property",
        type: signature?.returnType ?? typeRef("Variant"),
        signature,
      };
    }),
  }));
  const ado: VbType[] = Object.entries(externalObjectMembers).map(([name, members]) => ({
    name,
    kind: "com",
    members: members.map((member) => {
      const returnType = adoMemberTypes[name]?.[member.label] ?? "Variant";
      return {
        name: member.label,
        kind: isMethodLikeMember(member.label) ? "method" : "property",
        type: typeRef(returnType),
        signature: isMethodLikeMember(member.label)
          ? { parameters: [], returnType: typeRef(returnType) }
          : undefined,
      };
    }),
  }));
  return [...intrinsic, ...classicAsp, ...ado];
}

function configuredComTypes(comTypes: Record<string, AspVbscriptComType>): VbType[] {
  return Object.entries(comTypes).map(([name, config]) => ({
    name,
    kind: "com",
    members: Object.entries(config.members ?? {}).map(([memberName, member]) => {
      if (typeof member === "string") {
        return { name: memberName, kind: "property", type: typeRef(member) };
      }
      const returnType = member.returnType ?? member.type ?? "Variant";
      return {
        name: memberName,
        kind: member.kind ?? (member.parameters ? "method" : "property"),
        type: typeRef(returnType),
        signature: member.parameters
          ? {
              parameters: member.parameters.map((parameter, index) =>
                typeof parameter === "string"
                  ? { name: `arg${index + 1}`, type: typeRef(parameter) }
                  : {
                      name: parameter.name,
                      type: parameter.type ? typeRef(parameter.type) : undefined,
                    },
              ),
              returnType: typeRef(returnType),
            }
          : undefined,
      };
    }),
  }));
}

function builtinSignature(name: string): VbSignature | undefined {
  const label = builtinSignatureLabels(name)?.[0];
  if (!label) {
    return undefined;
  }
  const parameterText = /\((.*)\)/.exec(label)?.[1] ?? label.split(/\s+/).slice(1).join(", ");
  const parameters = parameterText
    ? parameterText
        .split(",")
        .map((part) => part.trim())
        .filter(Boolean)
        .map((parameter) => ({ name: parameter }))
    : [];
  return { parameters, returnType: typeRef(builtinReturnType(name)) };
}

function builtinSignatureLabels(name: string): string[] | undefined {
  const lower = name.toLowerCase();
  const classicAsp = classicAspBuiltinSignatures[lower];
  if (classicAsp) {
    return classicAsp;
  }
  const builtin = builtinFunction(lower);
  return builtin ? [builtin.signature] : undefined;
}

function builtinReturnType(name: string): string {
  const lower = name.toLowerCase();
  const builtin = builtinFunction(lower);
  if (builtin) {
    return builtin.returnType;
  }
  if (lower.includes("createobject") || lower.includes("getlasterror")) {
    return "Object";
  }
  if (
    lower.includes("map") ||
    lower.includes("encode") ||
    lower.includes("querystring") ||
    lower.includes("form")
  ) {
    return "String";
  }
  return "Variant";
}

function builtinFunction(name: string): BuiltinFunction | undefined {
  return builtinFunctions.find((item) => item.label.toLowerCase() === name.toLowerCase());
}

function typeRef(name: string): VbTypeRef {
  return { name, object: isObjectTypeName(name) };
}

function setSymbolType(symbol: VbSymbol, typeName: string): void {
  symbol.typeName = typeName;
  symbol.type = typeRef(typeName);
}

function addType(typeMap: Map<string, VbType>, type: VbType): void {
  typeMap.set(type.name.toLowerCase(), type);
}

function findType(env: VbTypeEnvironment, name: string): VbType | undefined {
  return env.types.find((type) => type.name.toLowerCase() === name.toLowerCase());
}

function canonicalBuiltinTypeName(name: string): string {
  return name.charAt(0).toUpperCase() + name.slice(1);
}

function isMethodLikeMember(name: string): boolean {
  return /^[A-Z]/.test(name) && !["EOF", "BOF", "Fields", "RecordCount", "State"].includes(name);
}

function isObjectTypeName(name: string): boolean {
  const lower = name.toLowerCase();
  return (
    lower === "object" ||
    lower === "array" ||
    classicAspTypeNames.has(lower) ||
    (!intrinsicTypeNames.has(lower) &&
      lower !== "string" &&
      lower !== "number" &&
      lower !== "boolean" &&
      lower !== "date")
  );
}

function isLooseType(typeName: string): boolean {
  const lower = typeName.toLowerCase();
  return lower === "unknown" || lower === "variant";
}

function isClearlyObjectType(type: VbTypeRef, env: VbTypeEnvironment): boolean {
  if (isLooseType(type.name) || type.name.toLowerCase() === "nothing") {
    return false;
  }
  return type.object === true || Boolean(findType(env, type.name) && !isClearlyScalarType(type));
}

function isClearlyScalarType(type: VbTypeRef): boolean {
  return ["string", "number", "boolean", "date"].includes(type.name.toLowerCase());
}

function isCompatibleType(left: VbTypeRef, right: VbTypeRef, env: VbTypeEnvironment): boolean {
  if (isLooseType(left.name) || isLooseType(right.name) || right.name.toLowerCase() === "nothing") {
    return true;
  }
  if (left.name.toLowerCase() === right.name.toLowerCase()) {
    return true;
  }
  if (left.name.toLowerCase() === "object" && isClearlyObjectType(right, env)) {
    return true;
  }
  return false;
}

function typeWarning(
  parsed: AspParsedDocument,
  start: number,
  end: number,
  message: string,
  code: string,
  data?: Record<string, string | number>,
): Diagnostic {
  return {
    severity: DiagnosticSeverity.Warning,
    range: rangeFromOffsets(parsed.text, start, end),
    message,
    code,
    data,
    source: "asp-lsp-vbscript-type",
  };
}

function matchingCloseParen(tokens: VbToken[], openIndex: number): number {
  let depth = 0;
  for (let index = openIndex; index < tokens.length; index += 1) {
    if (tokens[index].text === "(") {
      depth += 1;
    } else if (tokens[index].text === ")") {
      depth -= 1;
      if (depth === 0) {
        return index;
      }
    }
  }
  return -1;
}

function countArguments(tokens: VbToken[]): number {
  const meaningful = tokens.filter((token) => token.text !== ")" && !isTriviaToken(token));
  if (meaningful.length === 0) {
    return 0;
  }
  let depth = 0;
  let count = 1;
  for (const token of meaningful) {
    if (token.text === "(") {
      depth += 1;
    } else if (token.text === ")" && depth > 0) {
      depth -= 1;
    } else if (token.text === "," && depth === 0) {
      count += 1;
    }
  }
  return count;
}

function signatureLabelForDocumentation(symbol: VbSymbol): string {
  if (symbol.kind === "function" || symbol.kind === "sub" || symbol.kind === "method") {
    return signatureLabel(symbol);
  }
  return `${symbol.kind} ${symbol.name}${symbol.typeName ? ` As ${symbol.typeName}` : ""}`;
}

function tokenRangeAt(parsed: AspParsedDocument, offset: number): Range | undefined {
  const token = significantTokens(parsed).find(
    (item) => offset >= item.start && offset <= item.end,
  );
  return token ? rangeFromOffsets(parsed.text, token.start, token.end) : undefined;
}

function statementRangeAt(parsed: AspParsedDocument, offset: number): Range | undefined {
  const statement = vbStatements(parsed).find(
    (tokens) => offset >= (tokens[0]?.start ?? 0) && offset <= (tokens.at(-1)?.end ?? 0),
  );
  return statement
    ? rangeFromOffsets(parsed.text, statement[0].start, statement.at(-1)?.end ?? statement[0].end)
    : undefined;
}

function regionRangeAt(parsed: AspParsedDocument, offset: number): Range | undefined {
  const region = parsed.regions.find(
    (candidate) =>
      (candidate.language === "vbscript" || candidate.language === "jscript") &&
      offset >= candidate.start &&
      offset <= candidate.end,
  );
  return region ? rangeFromOffsets(parsed.text, region.start, region.end) : undefined;
}

function uniqueRanges(ranges: Range[]): Range[] {
  const keys = new Set<string>();
  return ranges
    .filter((range) => {
      const key = `${range.start.line}:${range.start.character}:${range.end.line}:${range.end.character}`;
      if (keys.has(key)) {
        return false;
      }
      keys.add(key);
      return true;
    })
    .sort(
      (left, right) =>
        rangeSize(left) - rangeSize(right) ||
        left.start.line - right.start.line ||
        left.start.character - right.start.character,
    );
}

function buildSelectionRangeChain(ranges: Range[]): SelectionRange {
  let parent: SelectionRange | undefined;
  for (const range of [...ranges].reverse()) {
    parent = { range, parent };
  }
  return (
    parent ?? {
      range: {
        start: { line: 0, character: 0 },
        end: { line: 0, character: 0 },
      },
    }
  );
}

function rangeSize(range: Range): number {
  return (
    (range.end.line - range.start.line) * 100_000 + range.end.character - range.start.character
  );
}

function isRange(value: Range | undefined): value is Range {
  return Boolean(value);
}

function rangeOverlapsOffsets(
  sourceText: string,
  range: Range,
  startOffset: number,
  endOffset: number,
): boolean {
  const start = offsetAt(sourceText, range.start);
  const end = offsetAt(sourceText, range.end);
  return start < endOffset && end > startOffset;
}

function topLevelArgumentStarts(tokens: VbToken[]): VbToken[] {
  const starts: VbToken[] = [];
  let depth = 0;
  let expectingArgument = true;
  for (const token of tokens.filter((item) => !isTriviaToken(item))) {
    if (token.text === "(") {
      depth += 1;
    } else if (token.text === ")" && depth > 0) {
      depth -= 1;
    } else if (token.text === "," && depth === 0) {
      expectingArgument = true;
      continue;
    }
    if (expectingArgument && token.text !== "," && token.text !== ")") {
      starts.push(token);
      expectingArgument = false;
    }
  }
  return starts;
}

function isNamedArgument(statement: VbToken[], token: VbToken): boolean {
  const index = statement.findIndex((item) => item.start === token.start && item.end === token.end);
  return statement[index + 1]?.text === ":=";
}

function typeNameAtOffset(
  parsed: AspParsedDocument,
  offset: number,
  symbols: VbSymbol[],
): string | undefined {
  const member = memberAccessAt(parsed, offset);
  if (member) {
    return member.owner === ""
      ? currentWithTypeName(parsed, offset, symbols)
      : member.owner.toLowerCase() === "me"
        ? currentClassName(parsed, offset, symbols)
        : inferVariableType(member.owner, parsed, offset, symbols);
  }
  const token = identifierTokenAt(parsed, offset);
  return token ? inferVariableType(token.text, parsed, offset, symbols) : undefined;
}

function isCallableHierarchySymbol(symbol: VbSymbol): boolean {
  return ["function", "sub", "method", "property", "class"].includes(symbol.kind);
}

function symbolToCallHierarchyItem(
  symbol: VbSymbol,
  rootUri = symbol.sourceUri,
): CallHierarchyItem {
  const data: VbCallHierarchyData = {
    uri: symbol.sourceUri,
    name: symbol.name,
    kind: symbol.kind,
    memberOf: symbol.memberOf,
    rootUri,
    line: symbol.range.start.line,
    character: symbol.range.start.character,
  };
  return {
    name: symbol.memberOf ? `${symbol.memberOf}.${symbol.name}` : symbol.name,
    kind: vbCallHierarchySymbolKind(symbol.kind),
    detail: symbol.typeName ? `As ${symbol.typeName}` : symbol.kind,
    uri: symbol.sourceUri,
    range: symbol.scopeRange ?? symbol.range,
    selectionRange: symbol.range,
    data,
  };
}

function vbCallHierarchySymbolKind(kind: VbSymbolKind): SymbolKind {
  if (kind === "class") {
    return SymbolKind.Class;
  }
  if (kind === "method" || kind === "sub") {
    return SymbolKind.Method;
  }
  if (kind === "property" || kind === "field") {
    return SymbolKind.Property;
  }
  return SymbolKind.Function;
}

function callHierarchyTargetSymbol(
  item: CallHierarchyItem,
  symbols: VbSymbol[],
): VbSymbol | undefined {
  const data = item.data as Partial<VbCallHierarchyData> | undefined;
  return symbols.find(
    (symbol) =>
      symbol.sourceUri === (data?.uri ?? item.uri) &&
      symbol.name.toLowerCase() ===
        (data?.name ?? item.name.split(".").at(-1) ?? "").toLowerCase() &&
      symbol.kind === (data?.kind ?? symbol.kind) &&
      (symbol.memberOf ?? "").toLowerCase() === (data?.memberOf ?? "").toLowerCase() &&
      symbol.range.start.line === (data?.line ?? item.selectionRange.start.line) &&
      symbol.range.start.character === (data?.character ?? item.selectionRange.start.character),
  );
}

function callHierarchyRootUri(item: CallHierarchyItem): string {
  const data = item.data as Partial<VbCallHierarchyData> | undefined;
  return data?.rootUri ?? item.uri;
}

function callSitesInDocument(
  parsed: AspParsedDocument,
): Array<{ name: string; offset: number; range: Range }> {
  const calls: Array<{ name: string; offset: number; range: Range }> = [];
  for (const statement of vbStatements(parsed)) {
    for (let index = 0; index < statement.length; index += 1) {
      if (statement[index].text !== "(" || statement[index - 1]?.kind !== "identifier") {
        continue;
      }
      const name = callNameBefore(statement, index);
      if (!name) {
        continue;
      }
      const start = name.includes(".")
        ? (statement[index - 3]?.start ?? statement[index - 1].start)
        : statement[index - 1].start;
      calls.push({
        name,
        offset: statement[index].start,
        range: rangeFromOffsets(parsed.text, start, statement[index - 1].end),
      });
    }
  }
  return calls;
}

function resolveCallTargetSymbol(
  parsed: AspParsedDocument,
  name: string,
  offset: number,
  symbols: VbSymbol[],
): VbSymbol | undefined {
  const [owner, member] = name.includes(".") ? name.split(".", 2) : [undefined, name];
  if (owner && member) {
    const typeName =
      owner.toLowerCase() === "me"
        ? currentClassName(parsed, offset, symbols)
        : inferVariableType(owner, parsed, offset, symbols);
    if (!typeName) {
      return undefined;
    }
    return symbols.find(
      (symbol) =>
        symbol.memberOf?.toLowerCase() === typeName.toLowerCase() &&
        symbol.name.toLowerCase() === member.toLowerCase() &&
        isCallableHierarchySymbol(symbol),
    );
  }
  return visibleSymbols(parsed, offset, symbols).find(
    (symbol) =>
      symbol.name.toLowerCase() === name.toLowerCase() && isCallableHierarchySymbol(symbol),
  );
}

function enclosingCallableSymbol(
  parsed: AspParsedDocument,
  offset: number,
  symbols: VbSymbol[],
): VbSymbol | undefined {
  return symbols
    .filter(
      (symbol) =>
        symbol.sourceUri === parsed.uri &&
        isCallableHierarchySymbol(symbol) &&
        rangeContainsOffset(parsed.text, symbol.scopeRange, offset),
    )
    .sort(
      (left, right) =>
        rangeSize(left.scopeRange ?? left.range) - rangeSize(right.scopeRange ?? right.range),
    )[0];
}

function symbolKey(symbol: VbSymbol): string {
  return [
    symbol.sourceUri,
    symbol.kind,
    symbol.memberOf ?? "",
    symbol.name.toLowerCase(),
    symbol.range.start.line,
    symbol.range.start.character,
  ].join("|");
}

function isLikelyDynamicCall(name: string): boolean {
  return /^[A-Z]/.test(name);
}

function isBuiltinName(name: string): boolean {
  const lower = name.toLowerCase();
  return (
    builtinCompletions(undefined).some((item) => item.label.toLowerCase() === lower) ||
    Object.values(memberCompletions).some((items) =>
      items.some((item) => item.label.toLowerCase() === lower),
    )
  );
}

function isClassicAspObjectName(name: string): boolean {
  return ["request", "response", "session", "application", "server", "asperror"].includes(
    name.toLowerCase(),
  );
}

function sameSymbol(left: VbSymbol, right: VbSymbol): boolean {
  return (
    left.sourceUri === right.sourceUri &&
    left.name.toLowerCase() === right.name.toLowerCase() &&
    left.kind === right.kind &&
    (left.memberOf ?? "").toLowerCase() === (right.memberOf ?? "").toLowerCase() &&
    left.range.start.line === right.range.start.line &&
    left.range.start.character === right.range.start.character &&
    left.range.end.line === right.range.end.line &&
    left.range.end.character === right.range.end.character
  );
}

function sameRange(left: Range, right: Range): boolean {
  return (
    left.start.line === right.start.line &&
    left.start.character === right.start.character &&
    left.end.line === right.end.line &&
    left.end.character === right.end.character
  );
}

function rangeContainsOffset(
  sourceText: string,
  range: Range | undefined,
  offset: number,
): boolean {
  if (!range) {
    return false;
  }
  const start = offsetAt(sourceText, range.start);
  const end = offsetAt(sourceText, range.end);
  return offset >= start && offset <= end;
}

function symbolPriority(symbol: VbSymbol): number {
  if (symbol.scopeName) {
    return 3;
  }
  if (symbol.memberOf) {
    return 2;
  }
  return 1;
}

function findLastIndex<T>(items: T[], predicate: (item: T) => boolean): number {
  for (let index = items.length - 1; index >= 0; index -= 1) {
    if (predicate(items[index])) {
      return index;
    }
  }
  return -1;
}
