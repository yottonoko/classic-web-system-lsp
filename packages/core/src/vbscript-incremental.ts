import type { VbCstNode, VbToken } from "./types";

export interface VbStatementEntry {
  tokens: VbToken[];
  documentIndex: number;
}

export interface VbStatementSnapshot {
  entries: VbStatementEntry[];
  statements: VbToken[][];
}

interface CachedVbDocumentStatements {
  tokens: VbToken[];
  statements: VbToken[][];
}

const statementCache = new WeakMap<VbCstNode, CachedVbDocumentStatements>();

export function cachedVbStatementSnapshot(
  documents: VbCstNode[],
  options: { cache?: boolean } = {},
): VbStatementSnapshot {
  const cacheEnabled = options.cache !== false;
  const entries: VbStatementEntry[] = [];
  documents.forEach((document, documentIndex) => {
    let statements: VbToken[][];
    if (cacheEnabled) {
      const cached = statementCache.get(document);
      if (cached?.tokens === document.tokens) {
        statements = cached.statements;
      } else {
        statements = splitVbDocumentStatements(document);
        statementCache.set(document, { tokens: document.tokens, statements });
      }
    } else {
      statements = splitVbDocumentStatements(document);
    }
    for (const tokens of statements) {
      entries.push({ tokens, documentIndex });
    }
  });
  return {
    entries,
    statements: entries.map((entry) => entry.tokens),
  };
}

export function stablePublicBoundaryHash(value: unknown): string {
  return textFingerprint(JSON.stringify(value));
}

function splitVbDocumentStatements(document: VbCstNode): VbToken[][] {
  const statements: VbToken[][] = [];
  let current: VbToken[] = [];
  for (const token of document.tokens.filter(
    (item) => item.kind !== "whitespace" && item.kind !== "comment",
  )) {
    if (token.kind === "newline" || token.text === ":") {
      if (token.kind === "newline" && current.at(-1)?.text === "_") {
        continue;
      }
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
  return statements;
}

function textFingerprint(text: string): string {
  let hash = 2166136261;
  hash = mixText(hash, text);
  return `${text.length}:${(hash >>> 0).toString(16)}`;
}

function mixText(hash: number, text: string): number {
  let result = hash;
  for (let index = 0; index < text.length; index += 1) {
    result ^= text.charCodeAt(index);
    result = Math.imul(result, 16777619);
  }
  return result;
}
