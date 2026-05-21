import {
  CompletionItemKind,
  DiagnosticSeverity,
  DocumentHighlightKind,
  SymbolKind,
} from "vscode-languageserver-types";
import type {
  CompletionItem,
  Diagnostic,
  DocumentHighlight,
  DocumentSymbol,
  Position,
  Range,
  SignatureHelp,
} from "vscode-languageserver-types";
import { offsetAt, rangeFromOffsets } from "./position";
import type { AspCstNode, AspParsedDocument, AspRegion, VbCstNode, VbToken } from "./types";

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
  parameters?: string[];
}

export interface VbReference {
  uri: string;
  range: Range;
}

export interface VbProjectContext {
  symbols?: VbSymbol[];
  documents?: AspParsedDocument[];
}

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

function methodItem(label: string): CompletionItem {
  return { label, kind: CompletionItemKind.Method };
}

const vbKeywords = new Set([
  "and",
  "application",
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
  "request",
  "response",
  "select",
  "server",
  "session",
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
  const symbols = context.symbols ?? collectVbscriptSymbols(parsed);
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
    return className ? classMemberCompletions(className, symbols) : [];
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
  const symbols = context.symbols ?? collectVbscriptSymbols(parsed);
  const diagnostics: Diagnostic[] = [];
  const scriptText = getServerScriptText(parsed);
  const optionExplicit = /^\s*Option\s+Explicit\b/im.test(scriptText);
  if (optionExplicit) {
    diagnostics.push(...diagnoseUndeclaredVariables(parsed, symbols));
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
    context.symbols ?? collectVbscriptSymbols(parsed),
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
    context.symbols ?? collectVbscriptSymbols(parsed),
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
  const symbols = context.symbols ?? collectVbscriptSymbols(parsed);
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
  const symbol = resolveSymbolAt(parsed, offset, context.symbols ?? collectVbscriptSymbols(parsed));
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
  const builtin = builtinSignatures[call.name.toLowerCase()];
  const signatureLabels =
    builtin ??
    signatureSymbolsForCall(
      parsed,
      call.name,
      offset,
      context.symbols ?? collectVbscriptSymbols(parsed),
    ).map((symbol) => signatureLabel(symbol));
  if (signatureLabels.length === 0) {
    return undefined;
  }
  return {
    signatures: signatureLabels.map((label) => ({ label })),
    activeSignature: 0,
    activeParameter,
  };
}

export function collectVbscriptSymbols(parsed: AspParsedDocument): VbSymbol[] {
  const symbols: VbSymbol[] = [];
  for (const node of vbDocuments(parsed)) {
    addSymbolsFromVbNode(parsed, node, symbols);
  }
  inferAssignedTypes(parsed, symbols);
  return symbols;
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

function inferAssignedTypes(parsed: AspParsedDocument, symbols: VbSymbol[]): void {
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
        visible.typeName = node.typeName;
      }
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

function classMemberCompletions(className: string, symbols: VbSymbol[]): CompletionItem[] {
  return dedupeCompletions([
    ...symbols
      .filter(
        (symbol) =>
          symbol.memberOf?.toLowerCase() === className.toLowerCase() &&
          (symbol.kind === "method" || symbol.kind === "field" || symbol.kind === "property"),
      )
      .map(symbolToCompletion),
    ...(externalObjectMembers[className.toLowerCase()] ?? []),
  ]);
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

function signatureLabel(symbol: VbSymbol): string {
  const keyword = symbol.kind === "sub" || symbol.kind === "method" ? "Sub" : "Function";
  const owner = symbol.memberOf ? `${symbol.memberOf}.` : "";
  return `${keyword} ${owner}${symbol.name}(${(symbol.parameters ?? []).join(", ")})`;
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
