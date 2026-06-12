import { describe, expect, it } from "vitest";
import { TextDocument } from "vscode-languageserver-textdocument";
import type { AspParsedDocument, AspSettings } from "@asp-lsp/core";
import { DocumentStore, type CachedDocument } from "../src/document-store";

describe("DocumentStore", () => {
  it("touches documents returned by URI lookups", () => {
    const store = new DocumentStore();
    const cached = cachedDocument("file:///site/default.asp", "text");
    cached.lastAccess = 1;
    store.cache.set(cached.source.uri, cached);

    expect(store.cachedDocumentForUri(cached.source.uri)).toBe(cached);
    expect(cached.lastAccess).toBeGreaterThan(1);
  });

  it("demotes evictable analysis state to skeleton and bumps generation", () => {
    const store = new DocumentStore();
    const cached = cachedDocument("file:///site/default.asp", "<% Dim value %>");
    cached.parseDepth = "full";
    cached.analysis = { diagnostics: { key: "diagnostics", items: [], text: cached.parsed.text } };
    cached.virtuals.set("html", {
      uri: `${cached.source.uri}.__virtual.html`,
      languageId: "html",
      text: "html",
      sourceMap: {
        segments: [],
        toSourceOffset: () => undefined,
        toVirtualOffset: () => undefined,
        toSourcePosition: () => undefined,
        toVirtualPosition: () => undefined,
      },
    });
    cached.virtualsMaterialized = true;
    const generation = cached.generation;

    const demoted = store.demote(cached, {
      settings: {},
      now: 42,
      parseSkeleton: (uri, text) => parsedDocument(uri, `skeleton:${text}`),
    });

    expect(demoted).toBe(true);
    expect(cached.analysis).toBeUndefined();
    expect(cached.virtuals.size).toBe(0);
    expect(cached.virtualsMaterialized).toBe(false);
    expect(cached.parseDepth).toBe("skeleton");
    expect(cached.parsed.text).toBe("skeleton:<% Dim value %>");
    expect(cached.generation).toBe(generation + 1);
    expect(cached.demotedAt).toBe(42);
  });

  it("does not bump generation when there is nothing to demote", () => {
    const store = new DocumentStore();
    const cached = cachedDocument("file:///site/default.asp", "plain");
    cached.parseDepth = "skeleton";
    const generation = cached.generation;

    expect(
      store.demote(cached, {
        settings: {},
        parseSkeleton: (uri, text) => parsedDocument(uri, text),
      }),
    ).toBe(false);
    expect(cached.generation).toBe(generation);
  });
});

function cachedDocument(uri: string, text: string): CachedDocument {
  const source = TextDocument.create(uri, "classic-asp", 1, text);
  const settings: AspSettings = {};
  return {
    source,
    parsed: parsedDocument(uri, text),
    parseDepth: "full",
    virtuals: new Map(),
    virtualsMaterialized: false,
    identity: { uri, version: source.version },
    generation: 1,
    lastAccess: 1,
    parseSettingsIdentity: JSON.stringify(settings),
    includeResolutionIdentity: JSON.stringify(settings),
    diagnosticsIdentity: JSON.stringify(settings),
    jsProjectIdentity: JSON.stringify(settings),
    workspaceGeneration: 1,
    includeResolutionGeneration: 1,
    jsProjectGeneration: 1,
    editHistory: [],
  };
}

function parsedDocument(uri: string, text: string): AspParsedDocument {
  return {
    uri,
    text,
    cst: {
      kind: "Document",
      start: 0,
      end: text.length,
      contentStart: 0,
      contentEnd: text.length,
      tokens: [],
      children: [],
    },
    regions: [],
    directives: [],
    includes: [],
    serverObjects: [],
    defaultLanguage: "VBScript",
    diagnostics: [],
  };
}
