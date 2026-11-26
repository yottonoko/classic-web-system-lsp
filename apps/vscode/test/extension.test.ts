import * as fs from "node:fs";
import * as path from "node:path";
import { describe, expect, it } from "vitest";
import { resolveAspLspServer } from "../src/serverPath.js";

const extensionRoot = path.resolve(import.meta.dirname, "..");

describe("resolveAspLspServer", () => {
  it("prefers explicit configured native or wasm path", () => {
    expect(
      resolveAspLspServer({
        configuredPath: "/custom/bin/classic-asp-lsp",
        extensionPath: "/ext",
        env: { CLASSIC_ASP_LSP_BIN: "/env/classic-asp-lsp" },
        fileExists: () => true,
      }),
    ).toEqual({ kind: "native", command: "/custom/bin/classic-asp-lsp", args: [] });

    expect(
      resolveAspLspServer({
        configuredPath: "/custom/classic-asp-lsp.wasm",
        extraArgs: ["--lang=asp"],
        extensionPath: "/ext",
        fileExists: () => true,
      }),
    ).toEqual({ kind: "wasm", wasmPath: "/custom/classic-asp-lsp.wasm", args: ["--lang=asp"] });
  });

  it("uses bundled native binary on Windows and POSIX", () => {
    expect(
      resolveAspLspServer({
        extensionPath: "/ext",
        platform: "win32",
        fileExists: (p) => p === path.join("/ext", "server", "classic-asp-lsp.exe"),
      }),
    ).toEqual({
      kind: "native",
      command: path.join("/ext", "server", "classic-asp-lsp.exe"),
      args: [],
    });

    expect(
      resolveAspLspServer({
        extensionPath: "/ext",
        platform: "linux",
        fileExists: (p) => p === path.join("/ext", "server", "classic-asp-lsp"),
      }),
    ).toEqual({
      kind: "native",
      command: path.join("/ext", "server", "classic-asp-lsp"),
      args: [],
    });
  });

  it("falls back to bundled WASM (Any OS) when native binary is absent", () => {
    expect(
      resolveAspLspServer({
        extensionPath: "/ext",
        platform: "darwin",
        fileExists: (p) => p === path.join("/ext", "server", "classic-asp-lsp.wasm"),
      }),
    ).toEqual({
      kind: "wasm",
      wasmPath: path.join("/ext", "server", "classic-asp-lsp.wasm"),
      args: [],
    });
  });

  it("falls back to CLASSIC_ASP_LSP_BIN env var then PATH", () => {
    expect(
      resolveAspLspServer({
        extensionPath: "/ext",
        env: { CLASSIC_ASP_LSP_BIN: "/env/classic-asp-lsp.wasm" },
        fileExists: () => false,
      }),
    ).toEqual({ kind: "wasm", wasmPath: "/env/classic-asp-lsp.wasm", args: [] });

    expect(
      resolveAspLspServer({
        extensionPath: "/ext",
        env: {},
        fileExists: () => false,
      }),
    ).toEqual({ kind: "native", command: "classic-asp-lsp", args: [] });
  });
});

describe("extension manifest & localization", () => {
  const pkg = JSON.parse(fs.readFileSync(path.join(extensionRoot, "package.json"), "utf8"));
  const nlsEn = JSON.parse(fs.readFileSync(path.join(extensionRoot, "package.nls.json"), "utf8"));
  const nlsJa = JSON.parse(
    fs.readFileSync(path.join(extensionRoot, "package.nls.ja.json"), "utf8"),
  );

  it("registers classic-asp and vbscript languages and grammars", () => {
    expect(pkg.version).toBe("1.0.0");
    const langIds = pkg.contributes.languages.map((l: { id: string }) => l.id);
    expect(langIds).toEqual(["classic-asp", "vbscript"]);
    for (const grammar of pkg.contributes.grammars) {
      expect(fs.existsSync(path.join(extensionRoot, grammar.path))).toBe(true);
    }
  });

  it("resolves every %key% placeholder in both EN and JA NLS files", () => {
    const raw = JSON.stringify(pkg);
    const matches = [...raw.matchAll(/%([a-zA-Z0-9_.-]+)%/g)].map((m) => m[1]);
    expect(matches.length).toBeGreaterThan(0);
    for (const key of matches) {
      expect(nlsEn[key], `missing EN NLS key: ${key}`).toBeTypeOf("string");
      expect(nlsJa[key], `missing JA NLS key: ${key}`).toBeTypeOf("string");
    }
  });
});
