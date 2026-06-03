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
