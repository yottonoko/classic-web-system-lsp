import type {
  VbArrayDeclaration,
  VbCstNode,
  VbParameterMetadata,
  VbParameterMode,
  VbToken,
} from "./types";

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
  "is",
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
  "rem",
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
    if (isRemCommentStart(text, index)) {
      index += 3;
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
    if (isDigit(char)) {
      index += 1;
      while (index < text.length && isNumberPart(text[index])) {
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

function isIdentifierStart(char: string | undefined): boolean {
  if (!char) {
    return false;
  }
  const code = char.charCodeAt(0);
  return (code >= 65 && code <= 90) || (code >= 97 && code <= 122);
}

function isIdentifierPart(char: string | undefined): boolean {
  if (!char) {
    return false;
  }
  const code = char.charCodeAt(0);
  return (
    (code >= 65 && code <= 90) ||
    (code >= 97 && code <= 122) ||
    (code >= 48 && code <= 57) ||
    code === 95
  );
}

function isDigit(char: string | undefined): boolean {
  if (!char) {
    return false;
  }
  const code = char.charCodeAt(0);
  return code >= 48 && code <= 57;
}

function isNumberPart(char: string | undefined): boolean {
  return char === "." || isDigit(char);
}

function isRemCommentStart(text: string, index: number): boolean {
  const first = text.charCodeAt(index);
  if (first !== 82 && first !== 114) {
    return false;
  }
  const second = text.charCodeAt(index + 1);
  if (second !== 69 && second !== 101) {
    return false;
  }
  const third = text.charCodeAt(index + 2);
  if (third !== 77 && third !== 109) {
    return false;
  }
  const after = text[index + 3];
  if (after && isIdentifierPart(after)) {
    return false;
  }
  let cursor = index - 1;
  while (cursor >= 0 && (text[cursor] === " " || text[cursor] === "\t")) {
    cursor -= 1;
  }
  return cursor < 0 || text[cursor] === "\n" || text[cursor] === "\r" || text[cursor] === ":";
}

export function isTriviaToken(token: VbToken): boolean {
  return token.kind === "whitespace" || token.kind === "comment" || token.kind === "newline";
}

export function isWhitespaceOrNewline(token: VbToken | undefined): boolean {
  return token?.kind === "whitespace" || token?.kind === "newline";
}

export function isDocCommentToken(token: VbToken | undefined): token is VbToken {
  return token?.kind === "comment" && token.text.startsWith("'''");
}

export function lowerToken(token: VbToken | undefined): string | undefined {
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
  let modeExplicit = false;
  let optional = false;
  let canReadName = true;
  while (cursor < tokens.length && tokens[cursor].text !== ")") {
    const token = tokens[cursor];
    const lower = token.text.toLowerCase();
    if (token.text === ",") {
      mode = undefined;
      modeExplicit = false;
      optional = false;
      canReadName = true;
    } else if (lower === "optional") {
      optional = true;
    } else if (lower === "byval") {
      mode = "byval";
      modeExplicit = true;
    } else if (lower === "byref") {
      mode = "byref";
      modeExplicit = true;
    } else if (canReadName && token.kind === "identifier") {
      parameters.push({ token, mode: mode ?? "byref", modeExplicit, optional });
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
  const arrayDeclarations: VbArrayDeclaration[] = [];
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
      const array = readArrayDeclaration(tokens, index, endIndex, declarationKind);
      if (array) {
        arrayDeclarations.push(array);
      }
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
    arrayDeclarations,
  };
}

function readArrayDeclaration(
  tokens: VbToken[],
  identifierIndex: number,
  endIndex: number,
  declarationKind: NonNullable<VbCstNode["declarationKind"]>,
): VbArrayDeclaration | undefined {
  let openIndex = identifierIndex + 1;
  while (openIndex <= endIndex && isWhitespaceOrNewline(tokens[openIndex])) {
    openIndex += 1;
  }
  if (tokens[openIndex]?.text !== "(") {
    return undefined;
  }
  let depth = 0;
  let closeIndex = -1;
  for (let index = openIndex; index <= endIndex; index += 1) {
    if (tokens[index]?.text === "(") {
      depth += 1;
    } else if (tokens[index]?.text === ")") {
      depth -= 1;
      if (depth === 0) {
        closeIndex = index;
        break;
      }
    }
  }
  if (closeIndex === -1) {
    return undefined;
  }
  const dimensions = arrayDimensionTexts(tokens.slice(openIndex + 1, closeIndex));
  return {
    name: tokens[identifierIndex],
    kind: declarationKind === "redim" || dimensions.length === 0 ? "dynamic" : "fixed",
    dimensions,
  };
}

function arrayDimensionTexts(tokens: VbToken[]): string[] {
  const dimensions: string[] = [];
  let current: VbToken[] = [];
  let depth = 0;
  const flush = (): void => {
    const text = current
      .filter((token) => !isWhitespaceOrNewline(token))
      .map((token) => token.text)
      .join("")
      .trim();
    if (text) {
      dimensions.push(text);
    }
    current = [];
  };
  for (const token of tokens) {
    if (token.text === "(") {
      depth += 1;
    } else if (token.text === ")") {
      depth = Math.max(0, depth - 1);
    }
    if (token.text === "," && depth === 0) {
      flush();
      continue;
    }
    current.push(token);
  }
  flush();
  return dimensions;
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

export function findCreateObjectCall(tokens: VbToken[], start: number, end: number): number {
  for (let index = start; index <= end; index += 1) {
    if (lowerToken(tokens[index]) === "createobject") {
      return index;
    }
    if (
      index + 2 <= end &&
      lowerToken(tokens[index]) === "server" &&
      tokens[index + 1]?.text === "." &&
      lowerToken(tokens[index + 2]) === "createobject"
    ) {
      return index;
    }
  }
  return -1;
}

export function unquoteVbString(value: string): string {
  return value.startsWith('"') && value.endsWith('"')
    ? value.slice(1, -1).replaceAll('""', '"')
    : value;
}

function findLastIndex<T>(items: T[], predicate: (item: T) => boolean): number {
  for (let index = items.length - 1; index >= 0; index -= 1) {
    if (predicate(items[index])) {
      return index;
    }
  }
  return -1;
}
