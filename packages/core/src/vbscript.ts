import { CompletionItemKind, DiagnosticSeverity, SymbolKind } from "vscode-languageserver-types";
import type { CompletionItem, Diagnostic, DocumentSymbol, Position, Range } from "vscode-languageserver-types";
import { offsetAt, positionAt, rangeFromOffsets } from "./position";
import type { AspParsedDocument, AspRegion } from "./types";

export interface VbSymbol {
  name: string;
  kind: "variable" | "constant" | "function" | "sub" | "class";
  range: Range;
}

const builtins: CompletionItem[] = [
  {
    label: "Request",
    kind: CompletionItemKind.Variable,
    detail: "Classic ASP Request object",
    documentation: "Reads client request values such as QueryString, Form, Cookies, and ServerVariables.",
  },
  { label: "Response", kind: CompletionItemKind.Variable, detail: "Classic ASP Response object" },
  { label: "Session", kind: CompletionItemKind.Variable, detail: "Classic ASP Session object" },
  { label: "Application", kind: CompletionItemKind.Variable, detail: "Classic ASP Application object" },
  { label: "Server", kind: CompletionItemKind.Variable, detail: "Classic ASP Server object" },
  { label: "ASPError", kind: CompletionItemKind.Class, detail: "Classic ASP error object" },
  { label: "Option Explicit", kind: CompletionItemKind.Keyword, detail: "Require explicit variable declarations" },
  { label: "Dim", kind: CompletionItemKind.Keyword },
  { label: "Set", kind: CompletionItemKind.Keyword },
  { label: "Const", kind: CompletionItemKind.Keyword },
  { label: "Sub", kind: CompletionItemKind.Keyword },
  { label: "Function", kind: CompletionItemKind.Keyword },
  { label: "Class", kind: CompletionItemKind.Keyword },
];

const memberCompletions: Record<string, CompletionItem[]> = {
  request: ["QueryString", "Form", "Cookies", "ServerVariables", "ClientCertificate", "TotalBytes", "BinaryRead"].map(methodItem),
  response: ["Write", "Redirect", "End", "Flush", "Clear", "Cookies", "Status", "ContentType", "Charset"].map(methodItem),
  session: ["Abandon", "Contents", "StaticObjects", "SessionID", "Timeout", "CodePage", "LCID"].map(methodItem),
  application: ["Lock", "Unlock", "Contents", "StaticObjects"].map(methodItem),
  server: ["CreateObject", "MapPath", "HTMLEncode", "URLEncode", "ScriptTimeout", "GetLastError"].map(methodItem),
};

function methodItem(label: string): CompletionItem {
  return { label, kind: CompletionItemKind.Method };
}

export function getVbscriptCompletions(parsed: AspParsedDocument, position: Position): CompletionItem[] {
  const sourceOffset = offsetAt(parsed.text, position);
  const prefix = parsed.text.slice(Math.max(0, sourceOffset - 64), sourceOffset);
  const memberMatch = /([A-Za-z][A-Za-z0-9_]*)\.$/.exec(prefix);
  if (memberMatch) {
    return memberCompletions[memberMatch[1].toLowerCase()] ?? [];
  }
  return [...builtins, ...collectVbscriptSymbols(parsed).map(symbolToCompletion)];
}

export function analyzeVbscript(parsed: AspParsedDocument): { diagnostics: Diagnostic[]; symbols: VbSymbol[] } {
  const symbols = collectVbscriptSymbols(parsed);
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
    .filter((symbol) => symbol.kind === "function" || symbol.kind === "sub" || symbol.kind === "class")
    .map((symbol) => ({
      name: symbol.name,
      kind: symbol.kind === "class" ? SymbolKind.Class : SymbolKind.Function,
      range: symbol.range,
      selectionRange: symbol.range,
    }));
}

export function collectVbscriptSymbols(parsed: AspParsedDocument): VbSymbol[] {
  const symbols: VbSymbol[] = [];
  for (const region of serverRegions(parsed)) {
    const text = parsed.text.slice(region.contentStart, region.contentEnd);
    addDeclarationSymbols(parsed.text, region.contentStart, text, symbols);
  }
  return symbols;
}

function addDeclarationSymbols(sourceText: string, baseOffset: number, text: string, symbols: VbSymbol[]): void {
  const searchableText = maskVbscriptStringsAndComments(text);
  const patterns: Array<[RegExp, VbSymbol["kind"]]> = [
    [/\b(?:Dim|Private|Public)\s+([A-Za-z][A-Za-z0-9_]*(?:\s*,\s*[A-Za-z][A-Za-z0-9_]*)*)/gi, "variable"],
    [/\bConst\s+([A-Za-z][A-Za-z0-9_]*)/gi, "constant"],
    [/\bFunction\s+([A-Za-z][A-Za-z0-9_]*)/gi, "function"],
    [/\bSub\s+([A-Za-z][A-Za-z0-9_]*)/gi, "sub"],
    [/\bClass\s+([A-Za-z][A-Za-z0-9_]*)/gi, "class"],
  ];
  for (const [pattern, kind] of patterns) {
    let match: RegExpExecArray | null;
    pattern.lastIndex = 0;
    while ((match = pattern.exec(searchableText)) !== null) {
      const names = kind === "variable" ? match[1].split(/\s*,\s*/) : [match[1]];
      for (const rawName of names) {
        const name = rawName.trim();
        const localStart = match.index + match[0].indexOf(name);
        const start = baseOffset + localStart;
        symbols.push({ name, kind, range: rangeFromOffsets(sourceText, start, start + name.length) });
      }
    }
  }
}

function diagnoseUndeclaredVariables(parsed: AspParsedDocument, symbols: VbSymbol[]): Diagnostic[] {
  const declared = new Set([
    ...symbols.map((symbol) => symbol.name.toLowerCase()),
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
  ]);
  const diagnostics: Diagnostic[] = [];
  const keywordSet = new Set([
    "option",
    "explicit",
    "dim",
    "set",
    "const",
    "sub",
    "function",
    "end",
    "if",
    "then",
    "else",
    "elseif",
    "for",
    "each",
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
      const previous = text.slice(Math.max(0, match.index - 24), match.index).toLowerCase();
      if (declared.has(lower) || keywordSet.has(lower) || /(?:dim|const|sub|function|class)\s+$/.test(previous) || /\.$/.test(previous.trimEnd())) {
        continue;
      }
      const next = searchableText.slice(match.index + name.length, match.index + name.length + 1);
      if (next === "(" && /^[A-Z]/.test(name)) {
        continue;
      }
      const start = region.contentStart + match.index;
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

function maskVbscriptStringsAndComments(text: string): string {
  const chars = text.split("");
  let inString = false;
  for (let index = 0; index < chars.length; index += 1) {
    const char = chars[index];
    if (char === "\"") {
      chars[index] = " ";
      if (inString && chars[index + 1] === "\"") {
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
  return parsed.regions.filter((region) => region.language === "vbscript" || region.language === "jscript");
}

function symbolToCompletion(symbol: VbSymbol): CompletionItem {
  const kind =
    symbol.kind === "variable"
      ? CompletionItemKind.Variable
      : symbol.kind === "constant"
        ? CompletionItemKind.Constant
        : symbol.kind === "class"
          ? CompletionItemKind.Class
          : CompletionItemKind.Function;
  return { label: symbol.name, kind };
}
