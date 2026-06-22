import type {
  AspNavigationCandidate,
  AspNavigationConfidence,
  AspNavigationEdgeKind,
  AspNavigationParameterFlow,
  AspNavigationParameterSource,
  AspNavigationUrlPart,
  AspNavigationUrlValue,
  AspParsedDocument,
  AspRegion,
  VbToken,
} from "./types";
import { extractHtmlNavigationCandidates } from "./asp-scanner";
import { rangeFromOffsets } from "./position";
import { tokenizeVbscript } from "./vbscript-cst";

interface VbNavigationState {
  variables: Map<string, AspNavigationUrlValue>;
  functions: Map<string, AspNavigationUrlValue>;
  currentFunction?: {
    name: string;
    value?: AspNavigationUrlValue;
  };
}

export function extractAspNavigationCandidates(
  parsed: AspParsedDocument,
): AspNavigationCandidate[] {
  const candidates = extractHtmlNavigationCandidates(parsed.text, parsed.uri);
  for (const region of parsed.regions) {
    if (region.language !== "vbscript" || region.kind === "asp-directive") {
      continue;
    }
    candidates.push(...extractVbscriptNavigationCandidates(parsed.text, region, parsed.uri));
  }
  return candidates;
}

export function extractVbscriptNavigationCandidates(
  sourceText: string,
  region: AspRegion,
  uri: string,
): AspNavigationCandidate[] {
  const regionText = sourceText.slice(region.contentStart, region.contentEnd);
  const tokens = tokenizeVbscript(regionText, region.contentStart);
  const statements = splitVbStatements(tokens);
  const state: VbNavigationState = {
    variables: new Map(),
    functions: new Map(),
  };
  const candidates: AspNavigationCandidate[] = [];
  for (const statement of statements) {
    const significant = statement.filter(
      (token) => token.kind !== "whitespace" && token.kind !== "comment",
    );
    if (significant.length === 0) {
      continue;
    }
    updateVbNavigationState(significant, state);
    candidates.push(...extractStatementNavigation(sourceText, significant, state, uri));
  }
  return candidates;
}

function splitVbStatements(tokens: VbToken[]): VbToken[][] {
  const statements: VbToken[][] = [];
  let current: VbToken[] = [];
  let parenDepth = 0;
  for (let index = 0; index < tokens.length; index += 1) {
    const token = tokens[index];
    if (token.text === "(") {
      parenDepth += 1;
    } else if (token.text === ")") {
      parenDepth = Math.max(0, parenDepth - 1);
    }
    const previousSignificant = lastSignificantToken(current);
    const isLineContinuation =
      token.kind === "newline" && previousSignificant?.text === "_" && parenDepth === 0;
    const isStatementBreak =
      !isLineContinuation &&
      parenDepth === 0 &&
      (token.kind === "newline" || (token.kind === "symbol" && token.text === ":"));
    if (isStatementBreak) {
      pushVbStatement(statements, current);
      current = [];
      continue;
    }
    current.push(token);
  }
  pushVbStatement(statements, current);
  return statements;
}

function pushVbStatement(statements: VbToken[][], statement: VbToken[]): void {
  if (
    statement.some(
      (token) =>
        token.kind !== "whitespace" && token.kind !== "comment" && token.kind !== "newline",
    )
  ) {
    statements.push(statement);
  }
}

function lastSignificantToken(tokens: VbToken[]): VbToken | undefined {
  for (let index = tokens.length - 1; index >= 0; index -= 1) {
    const token = tokens[index];
    if (token.kind !== "whitespace" && token.kind !== "comment" && token.kind !== "newline") {
      return token;
    }
  }
  return undefined;
}

function updateVbNavigationState(tokens: VbToken[], state: VbNavigationState): void {
  const first = lowerToken(tokens[0]);
  const second = lowerToken(tokens[1]);
  if (first === "function" && tokens[1]?.kind === "identifier") {
    state.currentFunction = { name: normalizeName(tokens[1].text) };
    return;
  }
  if (first === "end" && second === "function") {
    if (state.currentFunction?.value) {
      state.functions.set(state.currentFunction.name, state.currentFunction.value);
    }
    state.currentFunction = undefined;
    return;
  }
  if (first === "const") {
    const name = tokens.find((token, index) => index > 0 && token.kind === "identifier");
    const equalIndex = tokens.findIndex((token) => token.text === "=");
    if (name && equalIndex !== -1) {
      state.variables.set(
        normalizeName(name.text),
        evaluateVbExpression(tokens.slice(equalIndex + 1), state),
      );
    }
    return;
  }
  const assignment = readSimpleAssignment(tokens);
  if (!assignment) {
    return;
  }
  const value = evaluateVbExpression(assignment.expression, state);
  const normalizedName = normalizeName(assignment.name.text);
  if (state.currentFunction?.name === normalizedName) {
    state.currentFunction.value = value;
  } else {
    state.variables.set(normalizedName, value);
  }
}

function readSimpleAssignment(
  tokens: VbToken[],
): { name: VbToken; expression: VbToken[] } | undefined {
  const offset = lowerToken(tokens[0]) === "let" ? 1 : 0;
  const name = tokens[offset];
  if (!name || name.kind !== "identifier") {
    return undefined;
  }
  if (tokens[offset + 1]?.text === ".") {
    return undefined;
  }
  const equalIndex = tokens.findIndex((token, index) => index > offset && token.text === "=");
  if (equalIndex === -1) {
    return undefined;
  }
  return { name, expression: tokens.slice(equalIndex + 1) };
}

function extractStatementNavigation(
  sourceText: string,
  tokens: VbToken[],
  state: VbNavigationState,
  uri: string,
): AspNavigationCandidate[] {
  const offset = lowerToken(tokens[0]) === "call" ? 1 : 0;
  const member = readMemberPath(tokens, offset);
  if (!member) {
    return [];
  }
  const memberName = member.path.join(".");
  const args = readVbArguments(tokens, member.endIndex);
  if (memberName === "response.redirect" || memberName === "response.redirectpermanent") {
    return [
      vbNavigationCandidate(
        sourceText,
        tokens,
        args[0] ?? [],
        "serverRedirect",
        "Response.Redirect",
        uri,
        state,
      ),
    ];
  }
  if (memberName === "server.transfer") {
    return [
      vbNavigationCandidate(
        sourceText,
        tokens,
        args[0] ?? [],
        "serverRedirect",
        "Server.Transfer",
        uri,
        state,
      ),
    ];
  }
  if (memberName === "response.addheader" && isLocationHeader(args[0], state)) {
    return [
      vbNavigationCandidate(
        sourceText,
        tokens,
        args[1] ?? [],
        "serverRedirect",
        "Response.AddHeader Location",
        uri,
        state,
      ),
    ];
  }
  if (memberName === "response.write") {
    return extractResponseWriteHtmlNavigation(sourceText, tokens, args[0] ?? [], state, uri);
  }
  return [];
}

function vbNavigationCandidate(
  sourceText: string,
  statement: VbToken[],
  expression: VbToken[],
  kind: AspNavigationEdgeKind,
  label: string,
  uri: string,
  state: VbNavigationState,
): AspNavigationCandidate {
  const target =
    expression.length === 0 ? unknownUrlValue() : evaluateVbExpression(expression, state);
  const statementRange = tokenRange(sourceText, statement);
  const valueRange = expression.length > 0 ? tokenRange(sourceText, expression) : undefined;
  const confidence = confidenceFromUrlValue(target);
  return {
    kind,
    target,
    range: statementRange,
    valueRange,
    parameters: parametersFromUrlValue(target),
    declaredInUri: uri,
    evidence: [
      {
        uri,
        range: statementRange,
        valueRange,
        label,
        snippet: statementSnippet(sourceText, statement),
        extractor: "vbscript",
      },
    ],
    confidence,
    source: "vbscript",
  };
}

function extractResponseWriteHtmlNavigation(
  sourceText: string,
  statement: VbToken[],
  expression: VbToken[],
  state: VbNavigationState,
  uri: string,
): AspNavigationCandidate[] {
  const value =
    expression.length === 0 ? unknownUrlValue() : evaluateVbExpression(expression, state);
  const html = value.text ?? "";
  if (!looksLikeNavigationHtml(html)) {
    return [];
  }
  const statementRange = tokenRange(sourceText, statement);
  const valueRange = expression.length > 0 ? tokenRange(sourceText, expression) : undefined;
  const generated = extractHtmlNavigationCandidates(html, uri);
  return generated.map((candidate) => {
    const confidence = lowerConfidence(
      candidate.confidence ?? "possible",
      confidenceFromUrlValue(value),
    );
    return {
      ...candidate,
      range: statementRange,
      valueRange,
      declaredInUri: uri,
      evidence: [
        {
          uri,
          range: statementRange,
          valueRange,
          label: `Response.Write ${candidate.kind}`,
          snippet: statementSnippet(sourceText, statement),
          extractor: "vbscript",
        },
      ],
      confidence,
      source: "vbscript" as const,
      parameters: [...(candidate.parameters ?? []), ...parametersFromUrlValue(value)],
    };
  });
}

function evaluateVbExpression(tokens: VbToken[], state: VbNavigationState): AspNavigationUrlValue {
  const significant = trimExpressionTokens(
    tokens.filter(
      (token) =>
        token.kind !== "whitespace" && token.kind !== "comment" && token.kind !== "newline",
    ),
  );
  if (significant.length === 0) {
    return unknownUrlValue();
  }
  const concatParts = splitExpressionByConcatenation(significant);
  if (concatParts.length > 1) {
    return combineUrlValues(concatParts.map((part) => evaluateVbExpression(part, state)));
  }
  if (isWrappedByParentheses(significant)) {
    return evaluateVbExpression(significant.slice(1, -1), state);
  }
  if (significant.length === 1) {
    const token = significant[0];
    if (token.kind === "string") {
      return { kind: "literal", text: token.value ?? "" };
    }
    if (token.kind === "number") {
      return { kind: "literal", text: token.text };
    }
    const lower = lowerToken(token);
    if (lower === "true" || lower === "false") {
      return { kind: "literal", text: lower };
    }
    if (token.kind === "identifier" || token.kind === "keyword") {
      return (
        state.variables.get(normalizeName(token.text)) ??
        state.functions.get(normalizeName(token.text)) ??
        unknownUrlValue()
      );
    }
  }
  const call = readMemberPath(significant, 0);
  if (call) {
    const memberName = call.path.join(".");
    const args = readVbArguments(significant, call.endIndex);
    if (memberName === "server.urlencode" || memberName === "server.htmlencode") {
      return evaluateVbExpression(args[0] ?? [], state);
    }
    if (
      memberName === "cstr" ||
      memberName === "trim" ||
      memberName === "lcase" ||
      memberName === "ucase"
    ) {
      return evaluateVbExpression(args[0] ?? [], state);
    }
    if (
      memberName === "request" ||
      memberName === "request.querystring" ||
      memberName === "request.form"
    ) {
      return requestValueFromCall(memberName, args[0] ?? []);
    }
    if (call.path.length === 1 && state.functions.has(call.path[0])) {
      return state.functions.get(call.path[0]) ?? unknownUrlValue();
    }
  }
  return unknownUrlValue();
}

function splitExpressionByConcatenation(tokens: VbToken[]): VbToken[][] {
  const parts: VbToken[][] = [];
  let current: VbToken[] = [];
  let parenDepth = 0;
  for (const token of tokens) {
    if (token.text === "(") {
      parenDepth += 1;
    } else if (token.text === ")") {
      parenDepth = Math.max(0, parenDepth - 1);
    }
    if (parenDepth === 0 && (token.text === "&" || token.text === "+")) {
      parts.push(current);
      current = [];
      continue;
    }
    current.push(token);
  }
  parts.push(current);
  return parts;
}

function combineUrlValues(values: AspNavigationUrlValue[]): AspNavigationUrlValue {
  const parts: AspNavigationUrlPart[] = [];
  for (const value of values) {
    if (value.kind === "literal") {
      parts.push({ kind: "text", text: value.text ?? "" });
      continue;
    }
    if (value.kind === "template") {
      parts.push(...(value.parts ?? [{ kind: "unknown", text: value.text }]));
      continue;
    }
    parts.push({ kind: "unknown" });
  }
  if (parts.every((part) => part.kind === "text")) {
    return { kind: "literal", text: parts.map((part) => part.text ?? "").join("") };
  }
  return {
    kind: "template",
    text: parts
      .map((part) => {
        if (part.kind === "text") {
          return part.text ?? "";
        }
        if (part.kind === "request") {
          return `{${part.source ?? "request"}:${part.name ?? "value"}}`;
        }
        return "{unknown}";
      })
      .join(""),
    parts,
  };
}

function requestValueFromCall(memberName: string, keyExpression: VbToken[]): AspNavigationUrlValue {
  const source: AspNavigationParameterSource =
    memberName === "request.querystring"
      ? "queryString"
      : memberName === "request.form"
        ? "form"
        : "request";
  const name =
    keyExpression.length === 1 && keyExpression[0].kind === "string"
      ? keyExpression[0].value
      : undefined;
  return {
    kind: "template",
    text: `{${source}:${name ?? "value"}}`,
    parts: [{ kind: "request", source, name }],
  };
}

function readMemberPath(
  tokens: VbToken[],
  startIndex: number,
): { path: string[]; endIndex: number } | undefined {
  const first = tokens[startIndex];
  if (!first || (first.kind !== "identifier" && first.kind !== "keyword")) {
    return undefined;
  }
  const path = [normalizeName(first.text)];
  let cursor = startIndex + 1;
  while (
    tokens[cursor]?.text === "." &&
    (tokens[cursor + 1]?.kind === "identifier" || tokens[cursor + 1]?.kind === "keyword")
  ) {
    path.push(normalizeName(tokens[cursor + 1].text));
    cursor += 2;
  }
  return { path, endIndex: cursor };
}

function readVbArguments(tokens: VbToken[], startIndex: number): VbToken[][] {
  let cursor = startIndex;
  if (tokens[cursor]?.text === "(") {
    const closeIndex = matchingCloseParenIndex(tokens, cursor);
    return splitArguments(tokens.slice(cursor + 1, closeIndex === -1 ? tokens.length : closeIndex));
  }
  return splitArguments(tokens.slice(cursor));
}

function splitArguments(tokens: VbToken[]): VbToken[][] {
  const args: VbToken[][] = [];
  let current: VbToken[] = [];
  let parenDepth = 0;
  for (const token of tokens) {
    if (token.text === "(") {
      parenDepth += 1;
    } else if (token.text === ")") {
      parenDepth = Math.max(0, parenDepth - 1);
    }
    if (parenDepth === 0 && token.text === ",") {
      args.push(trimExpressionTokens(current));
      current = [];
      continue;
    }
    current.push(token);
  }
  if (current.length > 0 || tokens.length > 0) {
    args.push(trimExpressionTokens(current));
  }
  return args;
}

function matchingCloseParenIndex(tokens: VbToken[], openIndex: number): number {
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

function trimExpressionTokens(tokens: VbToken[]): VbToken[] {
  let start = 0;
  let end = tokens.length;
  while (start < end && tokens[start].text === "_") {
    start += 1;
  }
  while (end > start && tokens[end - 1].text === "_") {
    end -= 1;
  }
  return tokens.slice(start, end);
}

function isWrappedByParentheses(tokens: VbToken[]): boolean {
  if (tokens[0]?.text !== "(" || tokens.at(-1)?.text !== ")") {
    return false;
  }
  return matchingCloseParenIndex(tokens, 0) === tokens.length - 1;
}

function isLocationHeader(tokens: VbToken[] | undefined, state: VbNavigationState): boolean {
  if (!tokens) {
    return false;
  }
  const value = evaluateVbExpression(tokens, state);
  return value.kind === "literal" && value.text?.toLowerCase() === "location";
}

function parametersFromUrlValue(value: AspNavigationUrlValue): AspNavigationParameterFlow[] {
  return (value.parts ?? [])
    .filter((part) => part.kind === "request")
    .map((part) => ({
      name: part.name ?? "value",
      source: part.source ?? "request",
      targetUsage: value.text,
      confidence: "possible" as const,
    }));
}

function confidenceFromUrlValue(value: AspNavigationUrlValue): AspNavigationConfidence {
  if (value.kind === "literal") {
    return "certain";
  }
  if (value.kind === "template") {
    return "possible";
  }
  return "unknown";
}

function lowerConfidence(
  left: AspNavigationConfidence,
  right: AspNavigationConfidence,
): AspNavigationConfidence {
  const order: AspNavigationConfidence[] = ["certain", "probable", "possible", "unknown"];
  return order[Math.max(order.indexOf(left), order.indexOf(right))] ?? "unknown";
}

function unknownUrlValue(): AspNavigationUrlValue {
  return { kind: "unknown", text: "{unknown}", parts: [{ kind: "unknown" }] };
}

function looksLikeNavigationHtml(value: string): boolean {
  return /<\s*(?:a|area|iframe|frame|form|input|button|meta)\b/i.test(value);
}

function tokenRange(sourceText: string, tokens: VbToken[]) {
  const first = tokens[0];
  const last = tokens.at(-1);
  return rangeFromOffsets(sourceText, first?.start ?? 0, last?.end ?? first?.end ?? 0);
}

function statementSnippet(sourceText: string, tokens: VbToken[]): string {
  const first = tokens[0];
  const last = tokens.at(-1);
  if (!first || !last) {
    return "";
  }
  return sourceText.slice(first.start, last.end).replace(/\s+/g, " ").trim().slice(0, 240);
}

function lowerToken(token: VbToken | undefined): string | undefined {
  return token?.text.toLowerCase();
}

function normalizeName(value: string): string {
  return value.toLowerCase();
}
