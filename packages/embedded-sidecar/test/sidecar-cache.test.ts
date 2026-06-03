import { describe, expect, it } from "vitest";

process.env.ASP_LSP_SIDECAR_TEST_MODE = "1";

const { __test } = await import("../src/sidecar");

function request(overrides: Partial<Record<string, unknown>> = {}) {
  const activeVirtual = {
    uri: "file:///tmp/asp-lsp-sidecar/default.asp.javascript.virtual",
    languageId: "javascript",
    text: "const value = 1;\nvalue.toFixed();\n",
  };
  return {
    id: 1,
    operation: "completion",
    activeVirtual,
    openVirtuals: [activeVirtual],
    settings: { checkJs: true },
    workspaceRoots: [],
    projectGeneration: 1,
    projectFingerprint: "fingerprint-a",
    projectResetReason: "test",
    params: { position: { line: 1, character: 6 } },
    ...overrides,
  };
}

describe("sidecar language service cache", () => {
  it("reuses a language service for the same project fingerprint and virtual contents", () => {
    __test.clearLanguageServiceProjectCache();
    __test.resetCachesForProject(request());

    const first = __test.createLanguageServiceProject(request());
    const second = __test.createLanguageServiceProject(request());

    expect(second.service).toBe(first.service);
    expect(__test.languageServiceProjectCacheSize()).toBe(1);
  });

  it("does not reuse a service after project or virtual content changes", () => {
    __test.clearLanguageServiceProjectCache();
    __test.resetCachesForProject(request());

    const first = __test.createLanguageServiceProject(request());
    __test.resetCachesForProject(request({ projectFingerprint: "fingerprint-b" }));
    const changedProject = __test.createLanguageServiceProject(
      request({ projectFingerprint: "fingerprint-b" }),
    );

    const changedVirtual = {
      uri: "file:///tmp/asp-lsp-sidecar/default.asp.javascript.virtual",
      languageId: "javascript",
      text: "const value = 'changed';\nvalue.toUpperCase();\n",
    };
    const changedContent = __test.createLanguageServiceProject(
      request({
        projectFingerprint: "fingerprint-b",
        activeVirtual: changedVirtual,
        openVirtuals: [changedVirtual],
      }),
    );

    expect(changedProject.service).not.toBe(first.service);
    expect(changedContent.service).not.toBe(changedProject.service);
  });
});

describe("sidecar embedded language features", () => {
  it("serves HTML highlights, selection ranges, and rename edits", async () => {
    const activeVirtual = {
      uri: "file:///tmp/asp-lsp-sidecar/default.asp.html.virtual",
      languageId: "html",
      text: "<div><span>Ada</span></div>\n",
    };
    const base = request({
      activeVirtual,
      openVirtuals: [activeVirtual],
      params: { position: { line: 0, character: 2 } },
    });

    const highlights = (await __test.handleRequest({
      ...base,
      operation: "documentHighlights",
    })) as unknown[];
    expect(highlights.length).toBeGreaterThanOrEqual(2);

    const ranges = (await __test.handleRequest({
      ...base,
      operation: "selectionRanges",
      params: { positions: [{ line: 0, character: 2 }] },
    })) as unknown[];
    expect(ranges).toHaveLength(1);

    const rename = (await __test.handleRequest({
      ...base,
      operation: "rename",
      params: { position: { line: 0, character: 2 }, newName: "section" },
    })) as { changes?: Record<string, unknown[]> };
    expect(rename.changes?.[activeVirtual.uri]?.length).toBeGreaterThanOrEqual(2);
  });

  it("serves CSS formatting and semantic tokens", async () => {
    const activeVirtual = {
      uri: "file:///tmp/asp-lsp-sidecar/default.asp.css.virtual",
      languageId: "css",
      text: ".box{color:red;--accent:blue;}\n",
    };
    const base = request({
      activeVirtual,
      openVirtuals: [activeVirtual],
      params: { options: { tabSize: 2, insertSpaces: true } },
    });

    const formatting = (await __test.handleRequest({
      ...base,
      operation: "formatting",
    })) as unknown[];
    expect(formatting.length).toBeGreaterThan(0);

    const tokens = (await __test.handleRequest({
      ...base,
      operation: "semanticTokens",
      params: {},
    })) as Array<{ tokenType: number }>;
    expect(tokens.some((token) => token.tokenType === 6)).toBe(true);
  });

  it("serves JS language service features from the cached project", async () => {
    __test.clearLanguageServiceProjectCache();
    const activeVirtual = {
      uri: "file:///tmp/asp-lsp-sidecar/default.asp.javascript.virtual",
      languageId: "javascript",
      text: "function greet(name){return name;}\ngreet('Ada');\n",
    };
    const base = request({
      activeVirtual,
      openVirtuals: [activeVirtual],
      params: { position: { line: 1, character: 2 } },
    });

    const highlights = (await __test.handleRequest({
      ...base,
      operation: "documentHighlights",
    })) as unknown[];
    expect(highlights.length).toBeGreaterThanOrEqual(2);

    const ranges = (await __test.handleRequest({
      ...base,
      operation: "selectionRanges",
      params: { positions: [{ line: 1, character: 2 }] },
    })) as unknown[];
    expect(ranges).toHaveLength(1);

    const prepareRename = (await __test.handleRequest({
      ...base,
      operation: "prepareRename",
    })) as { start?: unknown; end?: unknown } | null;
    expect(prepareRename?.start).toBeTruthy();

    const rename = (await __test.handleRequest({
      ...base,
      operation: "rename",
      params: { position: { line: 1, character: 2 }, newName: "welcome" },
    })) as { changes?: Record<string, unknown[]> };
    expect(rename.changes?.[activeVirtual.uri]?.length).toBeGreaterThanOrEqual(2);

    const formatting = (await __test.handleRequest({
      ...base,
      operation: "formatting",
      params: { options: { tabSize: 2, insertSpaces: true } },
    })) as unknown[];
    expect(formatting.length).toBeGreaterThan(0);

    const tokens = (await __test.handleRequest({
      ...base,
      operation: "semanticTokens",
      params: {},
    })) as Array<{ tokenType: number }>;
    expect(tokens.length).toBeGreaterThan(0);
    expect(__test.languageServiceProjectCacheSize()).toBe(1);
  });
});
