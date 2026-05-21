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
import type { AspParsedDocument, AspRegion } from "./types";

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
  const word = wordAt(parsed.text, sourceOffset);
  if (!word) {
    return undefined;
  }
  const builtin = builtinDescriptions[word.toLowerCase()];
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
    for (const region of serverRegions(document)) {
      const text = document.text.slice(region.contentStart, region.contentEnd);
      const searchable = maskVbscriptStringsAndComments(text);
      const tokenPattern = new RegExp(`\\b${escapeRegExp(symbol.name)}\\b`, "gi");
      let match: RegExpExecArray | null;
      while ((match = tokenPattern.exec(searchable)) !== null) {
        const start = region.contentStart + match.index;
        const resolved = resolveSymbolAt(
          document,
          start + Math.floor(match[0].length / 2),
          symbols,
        );
        if (!resolved || !sameSymbol(resolved, symbol)) {
          continue;
        }
        references.push({
          uri: document.uri,
          range: rangeFromOffsets(document.text, start, start + match[0].length),
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
  const word = wordRangeAt(parsed.text, offset);
  return word?.text.toLowerCase() === symbol.name.toLowerCase() ? word.range : undefined;
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
  const call = callExpressionAt(parsed.text, offset);
  if (!call) {
    return undefined;
  }
  const activeParameter = countActiveParameter(parsed.text.slice(call.argumentsStart, offset));
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
  for (const region of serverRegions(parsed)) {
    const text = parsed.text.slice(region.contentStart, region.contentEnd);
    addDeclarationSymbols(parsed, region, text, symbols);
  }
  inferAssignedTypes(parsed, symbols);
  return symbols;
}

function addDeclarationSymbols(
  parsed: AspParsedDocument,
  region: AspRegion,
  text: string,
  symbols: VbSymbol[],
): void {
  const sourceText = parsed.text;
  const searchableText = maskVbscriptStringsAndComments(text);
  const contexts = buildContexts(sourceText, region.contentStart, searchableText);
  addClassSymbols(parsed, region.contentStart, searchableText, symbols, contexts);
  addProcedureSymbols(parsed, region.contentStart, searchableText, symbols, contexts);
  addVariableSymbols(parsed, region.contentStart, searchableText, symbols, contexts);
}

function addClassSymbols(
  parsed: AspParsedDocument,
  baseOffset: number,
  text: string,
  symbols: VbSymbol[],
  contexts: VbContext[],
): void {
  for (const context of contexts.filter((item) => item.kind === "class")) {
    const start = baseOffset + context.nameStart;
    symbols.push({
      name: context.name,
      kind: "class",
      range: rangeFromOffsets(parsed.text, start, start + context.name.length),
      sourceUri: parsed.uri,
      scopeRange: rangeFromOffsets(
        parsed.text,
        baseOffset + context.start,
        baseOffset + context.end,
      ),
    });
  }
}

function addProcedureSymbols(
  parsed: AspParsedDocument,
  baseOffset: number,
  text: string,
  symbols: VbSymbol[],
  contexts: VbContext[],
): void {
  const procedurePattern =
    /^\s*(?:(Public|Private)\s+)?(Sub|Function|Property\s+(?:Get|Let|Set))\s+([A-Za-z][A-Za-z0-9_]*)\s*(?:\(([^)]*)\))?/gim;
  let match: RegExpExecArray | null;
  while ((match = procedurePattern.exec(text)) !== null) {
    const name = match[3];
    const absoluteStart = baseOffset + match.index + match[0].indexOf(name);
    const context = innermostContext(contexts, match.index);
    const scope =
      context?.kind === "procedure" && context.name.toLowerCase() === name.toLowerCase()
        ? context
        : undefined;
    const memberOf = context?.kind === "class" ? context.name : context?.parentClass;
    const keyword = match[2].toLowerCase();
    const kind: VbSymbolKind = keyword.startsWith("property")
      ? "property"
      : memberOf
        ? "method"
        : keyword === "sub"
          ? "sub"
          : "function";
    symbols.push({
      name,
      kind,
      range: rangeFromOffsets(parsed.text, absoluteStart, absoluteStart + name.length),
      sourceUri: parsed.uri,
      memberOf,
      containerName: memberOf,
      scopeName: undefined,
      scopeRange: scope
        ? rangeFromOffsets(parsed.text, baseOffset + scope.start, baseOffset + scope.end)
        : undefined,
      parameters: parseParameters(match[4] ?? ""),
    });
    for (const parameter of parseParameters(match[4] ?? "")) {
      const parameterIndex = match[0].indexOf(parameter);
      if (parameterIndex === -1) {
        continue;
      }
      const parameterStart = baseOffset + match.index + parameterIndex;
      symbols.push({
        name: parameter,
        kind: "variable",
        range: rangeFromOffsets(parsed.text, parameterStart, parameterStart + parameter.length),
        sourceUri: parsed.uri,
        scopeName: name,
        scopeRange: scope
          ? rangeFromOffsets(parsed.text, baseOffset + scope.start, baseOffset + scope.end)
          : undefined,
      });
    }
  }
}

function addVariableSymbols(
  parsed: AspParsedDocument,
  baseOffset: number,
  text: string,
  symbols: VbSymbol[],
  contexts: VbContext[],
): void {
  const patterns: Array<[RegExp, "variable" | "constant"]> = [
    [
      /^\s*(?:Dim|ReDim(?:\s+Preserve)?|Private(?!\s+(?:Sub|Function|Property)\b)|Public(?!\s+(?:Sub|Function|Property)\b))\s+([A-Za-z][A-Za-z0-9_]*(?:\([^)]*\))?(?:\s*(?:,\s*|$)[A-Za-z][A-Za-z0-9_]*(?:\([^)]*\))?)*)/gim,
      "variable",
    ],
    [/^\s*Const\s+([A-Za-z][A-Za-z0-9_]*)/gim, "constant"],
    [/^\s*For\s+Each\s+([A-Za-z][A-Za-z0-9_]*)\s+In\b/gim, "variable"],
  ];
  for (const [pattern, baseKind] of patterns) {
    let match: RegExpExecArray | null;
    pattern.lastIndex = 0;
    while ((match = pattern.exec(text)) !== null) {
      const context = innermostContext(contexts, match.index);
      const declaration = match[1];
      for (const rawName of declaration.split(/\s*,\s*/)) {
        const name = rawName.trim().match(/^[A-Za-z][A-Za-z0-9_]*/)?.[0];
        if (!name) {
          continue;
        }
        const localStart = match.index + match[0].indexOf(rawName);
        const start = baseOffset + localStart;
        const memberOf =
          context?.kind === "class"
            ? context.name
            : context?.parentClass && !context.scopeName
              ? context.parentClass
              : undefined;
        const scope = context?.kind === "procedure" ? context : undefined;
        symbols.push({
          name,
          kind: memberOf && baseKind === "variable" ? "field" : baseKind,
          range: rangeFromOffsets(parsed.text, start, start + name.length),
          sourceUri: parsed.uri,
          memberOf,
          containerName: memberOf,
          scopeName: scope?.name,
          scopeRange: scope
            ? rangeFromOffsets(parsed.text, baseOffset + scope.start, baseOffset + scope.end)
            : context
              ? rangeFromOffsets(parsed.text, baseOffset + context.start, baseOffset + context.end)
              : undefined,
        });
      }
    }
  }
}

function inferAssignedTypes(parsed: AspParsedDocument, symbols: VbSymbol[]): void {
  const byName = new Map<string, VbSymbol[]>();
  for (const symbol of symbols) {
    const list = byName.get(symbol.name.toLowerCase()) ?? [];
    list.push(symbol);
    byName.set(symbol.name.toLowerCase(), list);
  }
  for (const region of serverRegions(parsed)) {
    const rawText = parsed.text.slice(region.contentStart, region.contentEnd);
    const text = maskVbscriptStringsAndComments(rawText);
    const assignmentPattern =
      /\bSet\s+([A-Za-z][A-Za-z0-9_]*)\s*=\s*New\s+([A-Za-z][A-Za-z0-9_]*)/gi;
    let match: RegExpExecArray | null;
    while ((match = assignmentPattern.exec(text)) !== null) {
      const offset = region.contentStart + match.index;
      const candidates = (byName.get(match[1].toLowerCase()) ?? []).filter(
        (symbol) => symbol.kind === "variable" || symbol.kind === "field",
      );
      const visible =
        candidates.find((candidate) =>
          isSymbolVisibleAt(candidate, parsed.uri, parsed.text, offset),
        ) ?? candidates[0];
      if (visible) {
        visible.typeName = match[2];
      }
    }
    const createObjectPattern =
      /\bSet\s+([A-Za-z][A-Za-z0-9_]*)\s*=\s*Server\.CreateObject\s*\(\s*"([^"]+)"\s*\)/gi;
    while ((match = createObjectPattern.exec(rawText)) !== null) {
      const offset = region.contentStart + match.index;
      const candidates = (byName.get(match[1].toLowerCase()) ?? []).filter(
        (symbol) => symbol.kind === "variable" || symbol.kind === "field",
      );
      const visible =
        candidates.find((candidate) =>
          isSymbolVisibleAt(candidate, parsed.uri, parsed.text, offset),
        ) ?? candidates[0];
      if (visible) {
        visible.typeName = match[2];
      }
    }
  }
}

interface VbContext {
  kind: "class" | "procedure";
  name: string;
  nameStart: number;
  start: number;
  end: number;
  parentClass?: string;
  scopeName?: string;
}

function buildContexts(sourceText: string, baseOffset: number, text: string): VbContext[] {
  const starts: VbContext[] = [];
  const contexts: VbContext[] = [];
  const tokenPattern =
    /^\s*(End\s+Class|End\s+Sub|End\s+Function|End\s+Property|Class\s+([A-Za-z][A-Za-z0-9_]*)|(?:(?:Public|Private)\s+)?(?:Sub|Function|Property\s+(?:Get|Let|Set))\s+([A-Za-z][A-Za-z0-9_]*))/gim;
  let match: RegExpExecArray | null;
  while ((match = tokenPattern.exec(text)) !== null) {
    const token = match[1].toLowerCase();
    const className = match[2];
    const procedureName = match[3];
    if (token.startsWith("class ") && className) {
      const name = className;
      starts.push({
        kind: "class",
        name,
        nameStart: match.index + match[0].indexOf(name),
        start: match.index,
        end: text.length,
      });
      continue;
    }
    if (!token.startsWith("end ") && procedureName) {
      const name = procedureName;
      const parentClass = [...starts].reverse().find((context) => context.kind === "class")?.name;
      starts.push({
        kind: "procedure",
        name,
        nameStart: match.index + match[0].indexOf(name),
        start: match.index,
        end: text.length,
        parentClass,
        scopeName: name,
      });
      continue;
    }
    const endKind = token.includes("class") ? "class" : "procedure";
    const openIndex = findLastIndex(starts, (context) => context.kind === endKind);
    if (openIndex !== -1) {
      const [context] = starts.splice(openIndex, 1);
      context.end = match.index + match[0].length;
      contexts.push(context);
    }
  }
  contexts.push(...starts.map((context) => ({ ...context, end: text.length })));
  return contexts.filter(
    (context) => baseOffset + context.start >= 0 && baseOffset + context.end <= sourceText.length,
  );
}

function innermostContext(contexts: VbContext[], offset: number): VbContext | undefined {
  return contexts
    .filter((context) => offset >= context.start && offset <= context.end)
    .sort((left, right) => right.start - left.start)[0];
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
  for (const region of serverRegions(parsed)) {
    if (offset < region.contentStart || offset > region.contentEnd) {
      continue;
    }
    const localOffset = offset - region.contentStart;
    const text = maskVbscriptStringsAndComments(
      parsed.text.slice(region.contentStart, region.contentEnd),
    );
    const tokenPattern = /^\s*(With\s+([A-Za-z][A-Za-z0-9_]*)|End\s+With)\b/gim;
    const stack: string[] = [];
    let match: RegExpExecArray | null;
    while ((match = tokenPattern.exec(text)) !== null && match.index < localOffset) {
      if (match[2]) {
        stack.push(match[2]);
      } else {
        stack.pop();
      }
    }
    const ownerName = stack.at(-1);
    return ownerName ? inferVariableType(ownerName, parsed, offset, symbols) : undefined;
  }
  return undefined;
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
  const member = memberAccessAt(parsed.text, offset);
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
  const word = wordAt(parsed.text, offset);
  if (!word) {
    return undefined;
  }
  return visibleSymbols(parsed, offset, symbols)
    .filter((symbol) => symbol.name.toLowerCase() === word.toLowerCase())
    .sort((left, right) => symbolPriority(right) - symbolPriority(left))[0];
}

function memberAccessAt(
  text: string,
  offset: number,
): { owner: string; member: string } | undefined {
  const before = text.slice(Math.max(0, offset - 128), offset);
  const after = text.slice(offset, offset + 64);
  const left = before.match(/(?:([A-Za-z][A-Za-z0-9_]*)|)\.([A-Za-z][A-Za-z0-9_]*)?$/);
  if (!left) {
    return undefined;
  }
  const memberRight = after.match(/^[A-Za-z0-9_]*/)?.[0] ?? "";
  const member = `${left[2] ?? ""}${memberRight}`;
  return member ? { owner: left[1] ?? "", member } : undefined;
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
  const keywordSet = new Set([
    "option",
    "explicit",
    "dim",
    "redim",
    "preserve",
    "private",
    "public",
    "set",
    "const",
    "sub",
    "function",
    "property",
    "get",
    "let",
    "end",
    "if",
    "then",
    "else",
    "elseif",
    "for",
    "each",
    "in",
    "next",
    "to",
    "step",
    "while",
    "wend",
    "do",
    "loop",
    "until",
    "select",
    "case",
    "class",
    "new",
    "with",
    "call",
    "and",
    "or",
    "not",
    "mod",
    "byval",
    "byref",
    "exit",
  ]);
  for (const region of serverRegions(parsed)) {
    const text = parsed.text.slice(region.contentStart, region.contentEnd);
    const searchableText = maskVbscriptStringsAndComments(text);
    const tokenPattern = /\b[A-Za-z][A-Za-z0-9_]*\b/g;
    let match: RegExpExecArray | null;
    while ((match = tokenPattern.exec(searchableText)) !== null) {
      const name = match[0];
      const lower = name.toLowerCase();
      const start = region.contentStart + match.index;
      const previous = text.slice(Math.max(0, match.index - 24), match.index).toLowerCase();
      if (
        declaredBuiltins.has(lower) ||
        keywordSet.has(lower) ||
        visibleSymbols(parsed, start, symbols).some(
          (symbol) => symbol.name.toLowerCase() === lower,
        ) ||
        /(?:dim|const|sub|function|class|property\s+(?:get|let|set)|new)\s+$/.test(previous) ||
        previous.trimEnd().endsWith(".")
      ) {
        continue;
      }
      const next = searchableText.slice(match.index + name.length, match.index + name.length + 1);
      if (next === "(" && /^[A-Z]/.test(name)) {
        continue;
      }
      diagnostics.push({
        severity: DiagnosticSeverity.Warning,
        range: rangeFromOffsets(parsed.text, start, start + name.length),
        message: `'${name}' is not declared under Option Explicit.`,
        source: "asp-lsp-vbscript",
      });
    }
  }
  return diagnostics;
}

function parseParameters(text: string): string[] {
  return text
    .split(",")
    .map((part) => part.trim().replace(/^(ByVal|ByRef)\s+/i, ""))
    .map((part) => part.match(/^[A-Za-z][A-Za-z0-9_]*/)?.[0])
    .filter((part): part is string => part !== undefined);
}

function maskVbscriptStringsAndComments(text: string): string {
  const chars = text.split("");
  let inString = false;
  for (let index = 0; index < chars.length; index += 1) {
    const char = chars[index];
    if (char === '"') {
      chars[index] = " ";
      if (inString && chars[index + 1] === '"') {
        chars[index + 1] = " ";
        index += 1;
        continue;
      }
      inString = !inString;
      continue;
    }
    if (!inString && char === "'") {
      while (index < chars.length && chars[index] !== "\n" && chars[index] !== "\r") {
        chars[index] = " ";
        index += 1;
      }
      index -= 1;
      continue;
    }
    if (inString && char !== "\n" && char !== "\r") {
      chars[index] = " ";
    }
  }
  return chars.join("");
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

function wordAt(text: string, offset: number): string | undefined {
  const left = text.slice(0, offset).match(/[A-Za-z][A-Za-z0-9_]*$/)?.[0] ?? "";
  const right = text.slice(offset).match(/^[A-Za-z0-9_]*/)?.[0] ?? "";
  return left || right ? `${left}${right}` : undefined;
}

function wordRangeAt(text: string, offset: number): { text: string; range: Range } | undefined {
  const left = text.slice(0, offset).match(/[A-Za-z][A-Za-z0-9_]*$/)?.[0] ?? "";
  const right = text.slice(offset).match(/^[A-Za-z0-9_]*/)?.[0] ?? "";
  const word = `${left}${right}`;
  if (!word) {
    return undefined;
  }
  const start = offset - left.length;
  return { text: word, range: rangeFromOffsets(text, start, start + word.length) };
}

function callExpressionAt(
  text: string,
  offset: number,
): { name: string; argumentsStart: number } | undefined {
  const masked = maskVbscriptStringsAndComments(text.slice(0, offset));
  let depth = 0;
  for (let index = masked.length - 1; index >= 0; index -= 1) {
    const char = masked[index];
    if (char === ")") {
      depth += 1;
      continue;
    }
    if (char !== "(") {
      continue;
    }
    if (depth > 0) {
      depth -= 1;
      continue;
    }
    const before = masked
      .slice(0, index)
      .match(/([A-Za-z][A-Za-z0-9_]*(?:\.[A-Za-z][A-Za-z0-9_]*)?)\s*$/);
    return before ? { name: before[1], argumentsStart: index + 1 } : undefined;
  }
  return undefined;
}

function countActiveParameter(text: string): number {
  const masked = maskVbscriptStringsAndComments(text);
  let depth = 0;
  let count = 0;
  for (const char of masked) {
    if (char === "(") {
      depth += 1;
    } else if (char === ")" && depth > 0) {
      depth -= 1;
    } else if (char === "," && depth === 0) {
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

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}
