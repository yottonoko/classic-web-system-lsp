import {
  CompletionItemKind,
  DiagnosticSeverity,
  DocumentHighlightKind,
  InlayHintKind,
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
import type {
  AspCstNode,
  AspParsedDocument,
  AspRegion,
  AspVbscriptComType,
  VbCstNode,
  VbToken,
} from "./types";

export type VbSymbolKind =
  | "variable"
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
}

export interface VbReference {
  uri: string;
  range: Range;
}

export interface VbProjectContext {
  symbols?: VbSymbol[];
  documents?: AspParsedDocument[];
  typeChecking?: "basic" | "strict";
  comTypes?: Record<string, AspVbscriptComType>;
  typeEnvironment?: VbTypeEnvironment;
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

export interface VbSignatureParameter {
  name: string;
  type?: VbTypeRef;
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

const builtins: CompletionItem[] = [
  {
    label: "Request",
    kind: CompletionItemKind.Variable,
    detail: "Classic ASP Request object",
    documentation:
      "Reads client request values such as QueryString, Form, Cookies, and ServerVariables.",
  },
  { label: "Response", kind: CompletionItemKind.Variable, detail: "Classic ASP Response object" },
  { label: "Session", kind: CompletionItemKind.Variable, detail: "Classic ASP Session object" },
  {
    label: "Application",
    kind: CompletionItemKind.Variable,
    detail: "Classic ASP Application object",
  },
  { label: "Server", kind: CompletionItemKind.Variable, detail: "Classic ASP Server object" },
  { label: "ASPError", kind: CompletionItemKind.Class, detail: "Classic ASP error object" },
  {
    label: "Option Explicit",
    kind: CompletionItemKind.Keyword,
    detail: "Require explicit variable declarations",
  },
  { label: "Dim", kind: CompletionItemKind.Keyword },
  { label: "Set", kind: CompletionItemKind.Keyword },
  { label: "Const", kind: CompletionItemKind.Keyword },
  { label: "Sub", kind: CompletionItemKind.Keyword },
  { label: "Function", kind: CompletionItemKind.Keyword },
  { label: "Class", kind: CompletionItemKind.Keyword },
];

const builtinDescriptions: Record<string, string> = {
  request: "Classic ASP Request object. Reads values sent by the client.",
  response: "Classic ASP Response object. Writes output and controls the HTTP response.",
  session: "Classic ASP Session object. Stores per-user state.",
  application: "Classic ASP Application object. Stores application-wide state.",
  server: "Classic ASP Server object. Creates COM objects, maps paths, and encodes values.",
  asperror: "Classic ASP error object returned by Server.GetLastError.",
};

const builtinSignatures: Record<string, string[]> = {
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
    if (declarationStart === "sub" || declarationStart === "function") {
      const nameToken = significant[index + declarationOffset + 1];
      if (nameToken?.kind === "identifier") {
        const node = createProcedureNode(
          declarationStart,
          token,
          nameToken,
          collectParameterTokens(significant, index + declarationOffset + 2),
          stack,
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
          collectParameterTokens(significant, index + declarationOffset + 3),
          stack,
          accessor,
        );
        addChild(stack.at(-1) ?? document, node);
        stack.push(node);
      }
      continue;
    }
    const current = stack.at(-1) ?? document;
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
        createDeclarationNode(token, "VariableDeclaration", first, significant, index + 1),
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
      addChild(current, {
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
        scopeStart: current.start,
        scopeEnd: current.end,
      });
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
    }
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
  return /[A-Za-z_]/.test(char);
}

function isIdentifierPart(char: string): boolean {
  return /[A-Za-z0-9_]/.test(char);
}

function isTriviaToken(token: VbToken): boolean {
  return token.kind === "whitespace" || token.kind === "comment" || token.kind === "newline";
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
  parameters: VbToken[],
  stack: VbCstNode[],
  propertyAccessor?: "get" | "let" | "set",
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
    parameters,
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

function collectParameterTokens(tokens: VbToken[], index: number): VbToken[] {
  const parameters: VbToken[] = [];
  if (tokens[index]?.text !== "(") {
    return parameters;
  }
  let cursor = index + 1;
  while (cursor < tokens.length && tokens[cursor].text !== ")") {
    if (
      tokens[cursor].kind === "identifier" &&
      !["byval", "byref"].includes(tokens[cursor].text.toLowerCase())
    ) {
      parameters.push(tokens[cursor]);
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
    identifiers,
  };
}

function statementEndIndex(tokens: VbToken[], startIndex: number): number {
  let index = startIndex;
  while (index + 1 < tokens.length) {
    const next = tokens[index + 1];
    if (next.kind === "newline" || next.text === ":") {
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
    ...builtins,
    ...visibleSymbols(parsed, sourceOffset, symbols).map(symbolToCompletion),
  ]);
}

export function analyzeVbscript(
  parsed: AspParsedDocument,
  context: VbProjectContext = {},
): { diagnostics: Diagnostic[]; symbols: VbSymbol[] } {
  const symbols = context.symbols ?? collectVbscriptSymbols(parsed, context);
  const diagnostics: Diagnostic[] = [];
  const scriptText = getServerScriptText(parsed);
  const optionExplicit = /^\s*Option\s+Explicit\b/im.test(scriptText);
  if (optionExplicit) {
    diagnostics.push(...diagnoseUndeclaredVariables(parsed, symbols));
  }
  if (context.typeChecking === "strict") {
    diagnostics.push(
      ...diagnoseTypeIssues(
        parsed,
        symbols,
        context.typeEnvironment ?? buildVbTypeEnvironment(parsed, { ...context, symbols }),
      ),
    );
  }
  return { diagnostics, symbols };
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
  const builtin = builtinDescriptions[token.text.toLowerCase()];
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
  const container = symbol.memberOf
    ? ` of ${symbol.memberOf}`
    : symbol.scopeName
      ? ` in ${symbol.scopeName}`
      : "";
  const type = symbol.typeName ? ` As ${symbol.typeName}` : "";
  return `${symbol.kind} ${symbol.name}${type}${container}`;
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
  const builtin = builtinSignatures[call.name.toLowerCase()];
  const signatureLabels =
    typeSignatureLabelsForCall(parsed, call.name, offset, symbols, typeEnvironment) ??
    builtin ??
    signatureSymbolsForCall(parsed, call.name, offset, symbols).map((symbol) =>
      signatureLabel(symbol),
    );
  if (signatureLabels.length === 0) {
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
  const builtin = builtins.find((candidate) => candidate.label.toLowerCase() === label);
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
    const owner = symbol.memberOf ? ` of ${symbol.memberOf}` : "";
    return {
      ...item,
      detail: `${symbol.kind}${type}${owner}`,
      documentation: `${signatureLabelForDocumentation(symbol)}\n\nDefined in ${symbol.sourceUri}.`,
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
      documentation: `${member.member.kind} ${member.type.name}.${member.member.name}${type}`,
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
        !["variable", "constant", "field"].includes(symbol.kind) ||
        !symbol.typeName ||
        isLooseType(symbol.typeName) ||
        !rangeOverlapsOffsets(parsed.text, symbol.range, rangeStart, rangeEnd)
      ) {
        continue;
      }
      hints.push({
        position: symbol.range.end,
        label: ` As ${symbol.typeName}`,
        kind: InlayHintKind.Type,
        paddingLeft: true,
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
        label: ` As ${symbol.typeName}`,
        kind: InlayHintKind.Type,
        paddingLeft: true,
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
                    parameters: (member.parameters ?? []).map((name) => ({ name })),
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
  if (node.kind === "Class" && node.nameToken) {
    symbols.push({
      name: node.nameToken.text,
      kind: "class",
      range: rangeFromOffsets(parsed.text, node.nameToken.start, node.nameToken.end),
      sourceUri: parsed.uri,
      scopeRange: rangeFromOffsets(parsed.text, node.start, node.end),
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
    });
    for (const parameter of node.parameters ?? []) {
      symbols.push({
        name: parameter.text,
        kind: "variable",
        range: rangeFromOffsets(parsed.text, parameter.start, parameter.end),
        sourceUri: parsed.uri,
        scopeName: name,
        scopeRange: rangeFromOffsets(parsed.text, node.start, node.end),
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
    for (const identifier of node.identifiers ?? (node.nameToken ? [node.nameToken] : [])) {
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
      });
    }
  }
  for (const child of node.children) {
    addSymbolsFromVbNode(parsed, child, symbols);
  }
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
  const first = significant[0];
  if (!first) {
    return undefined;
  }
  if (first.kind === "string") {
    return typeRef("String");
  }
  if (first.kind === "number") {
    return typeRef("Number");
  }
  if (first.text === "#" && significant.at(-1)?.text === "#") {
    return typeRef("Date");
  }
  const lower = first.text.toLowerCase();
  if (lower === "true" || lower === "false") {
    return typeRef("Boolean");
  }
  if (lower === "nothing" || lower === "null" || lower === "empty") {
    return typeRef(lower === "nothing" ? "Nothing" : "Variant");
  }
  if (lower === "new" && significant[1]?.kind === "identifier") {
    return typeRef(significant[1].text);
  }
  const createObjectIndex = findCreateObjectCall(significant, 0, significant.length - 1);
  if (createObjectIndex !== -1) {
    const stringToken = significant
      .slice(createObjectIndex)
      .find((token) => token.kind === "string");
    return stringToken
      ? typeRef(stringToken.value ?? unquoteVbString(stringToken.text))
      : undefined;
  }
  if (
    first.kind === "identifier" &&
    significant[1]?.text === "." &&
    significant[2]?.kind === "identifier"
  ) {
    const ownerType = inferVariableType(first.text, parsed, offset, symbols);
    return ownerType
      ? (memberReturnType(ownerType, significant[2].text, env) ??
          memberType(ownerType, significant[2].text, env))
      : undefined;
  }
  if (first.kind === "identifier") {
    const called = significant[1]?.text === "(";
    if (called) {
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
        candidate.kind === "variable" &&
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
      .map(symbolToCompletion),
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
  return owner?.kind === "identifier"
    ? { owner: owner.text, member: token.text }
    : { owner: "", member: token.text };
}

function diagnoseUndeclaredVariables(parsed: AspParsedDocument, symbols: VbSymbol[]): Diagnostic[] {
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
      message: `'${name}' is not declared under Option Explicit.`,
      source: "asp-lsp-vbscript",
    });
  }
  return diagnostics;
}

function getServerScriptText(parsed: AspParsedDocument): string {
  return serverRegions(parsed)
    .map((region) => parsed.text.slice(region.contentStart, region.contentEnd))
    .join("\n");
}

function serverRegions(parsed: AspParsedDocument): AspRegion[] {
  return parsed.regions.filter(
    (region) => region.language === "vbscript" || region.language === "jscript",
  );
}

function symbolToCompletion(symbol: VbSymbol): CompletionItem {
  const kind =
    symbol.kind === "variable"
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
    ? `${symbol.kind} of ${symbol.memberOf}`
    : symbol.typeName
      ? `${symbol.kind} As ${symbol.typeName}`
      : symbol.kind;
  return { label: symbol.name, kind, detail };
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
    return parameter.type ? `${parameter.name} As ${parameter.type.name}` : parameter.name;
  });
  return `${owner}.${name}(${parameters.join(", ")})`;
}

function signatureLabel(symbol: VbSymbol): string {
  const keyword = symbol.kind === "sub" || symbol.kind === "method" ? "Sub" : "Function";
  const owner = symbol.memberOf ? `${symbol.memberOf}.` : "";
  const returnType = symbol.type ?? (symbol.typeName ? typeRef(symbol.typeName) : undefined);
  return `${keyword} ${owner}${symbol.name}(${(symbol.parameters ?? []).join(", ")})${
    returnType && keyword === "Function" ? ` As ${returnType.name}` : ""
  }`;
}

function diagnoseTypeIssues(
  parsed: AspParsedDocument,
  symbols: VbSymbol[],
  env: VbTypeEnvironment,
): Diagnostic[] {
  const diagnostics: Diagnostic[] = [];
  for (const statement of vbStatements(parsed)) {
    diagnostics.push(...diagnoseAssignmentTypes(parsed, statement, symbols, env));
    diagnostics.push(...diagnoseCallTypes(parsed, statement, symbols, env));
    diagnostics.push(...diagnoseMemberAccess(parsed, statement, symbols, env));
  }
  return diagnostics;
}

function diagnoseAssignmentTypes(
  parsed: AspParsedDocument,
  statement: VbToken[],
  symbols: VbSymbol[],
  env: VbTypeEnvironment,
): Diagnostic[] {
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
        `Set assigns an object reference, but '${target.text}' receives ${rhsType.name}.`,
      ),
    );
  }
  if (!isSet && rhsType && isClearlyObjectType(rhsType, env)) {
    diagnostics.push(
      typeWarning(
        parsed,
        target.start,
        statement.at(-1)?.end ?? target.end,
        `Object assignment to '${target.text}' should use Set.`,
      ),
    );
  }
  if (lhsTypeName && rhsType && !isCompatibleType(typeRef(lhsTypeName), rhsType, env)) {
    diagnostics.push(
      typeWarning(
        parsed,
        target.start,
        statement.at(-1)?.end ?? target.end,
        `Type mismatch: '${target.text}' is ${lhsTypeName}, but assigned ${rhsType.name}.`,
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
): Diagnostic[] {
  const diagnostics: Diagnostic[] = [];
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
            `Call target '${name}' is not known.`,
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
          `Argument count mismatch for '${name}': expected ${signature.parameters.length}, got ${argumentCount}.`,
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
): Diagnostic[] {
  const diagnostics: Diagnostic[] = [];
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
          `Type '${ownerTypeName}' has no member '${member.text}'.`,
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
    parameters: (symbol.parameters ?? []).map((parameter) => ({ name: parameter })),
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
  const label = builtinSignatures[name.toLowerCase()]?.[0];
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

function builtinReturnType(name: string): string {
  const lower = name.toLowerCase();
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
): Diagnostic {
  return {
    severity: DiagnosticSeverity.Warning,
    range: rangeFromOffsets(parsed.text, start, end),
    message,
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
    builtins.some((item) => item.label.toLowerCase() === lower) ||
    Object.values(memberCompletions).some((items) =>
      items.some((item) => item.label.toLowerCase() === lower),
    )
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
