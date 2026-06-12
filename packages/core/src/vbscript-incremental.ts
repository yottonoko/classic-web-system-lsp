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
  fingerprint: string;
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
    const fingerprint = vbDocumentStatementFingerprint(document);
    const cached = cacheEnabled ? statementCache.get(document) : undefined;
    const statements =
      cached?.fingerprint === fingerprint ? cached.statements : splitVbDocumentStatements(document);
    if (cached?.fingerprint !== fingerprint) {
      if (cacheEnabled) {
        statementCache.set(document, { fingerprint, statements });
      }
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

function vbDocumentStatementFingerprint(document: VbCstNode): string {
  let hash = 2166136261;
  hash = mixHash(hash, document.start);
  hash = mixHash(hash, document.end);
  hash = mixHash(hash, document.tokens.length);
  for (const token of document.tokens) {
    hash = mixHash(hash, token.start);
    hash = mixHash(hash, token.end);
    hash = mixText(hash, token.kind);
    hash = mixText(hash, token.text);
  }
  return `${document.start}:${document.end}:${document.tokens.length}:${hash >>> 0}`;
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

function mixHash(hash: number, value: number): number {
  let result = hash;
  result ^= value & 0xff;
  result = Math.imul(result, 16777619);
  result ^= (value >>> 8) & 0xff;
  result = Math.imul(result, 16777619);
  result ^= (value >>> 16) & 0xff;
  result = Math.imul(result, 16777619);
  result ^= (value >>> 24) & 0xff;
  return Math.imul(result, 16777619);
}
