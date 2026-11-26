import * as fs from "node:fs";
import * as path from "node:path";
import { describe, expect, it } from "vitest";
import { createRestartPolicy } from "../src/restartPolicy.js";
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
    expect(pkg.version).toBe("0.12.0");
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

  it("registers classic-asp comment toggle commands and keybindings", () => {
    const commands = pkg.contributes.commands.map((c: { command: string }) => c.command);
    expect(commands).toContain("aspLsp.toggleLineComment");
    expect(commands).toContain("aspLsp.toggleBlockComment");

    const lineBinding = pkg.contributes.keybindings.find(
      (k: { command: string }) => k.command === "aspLsp.toggleLineComment",
    );
    expect(lineBinding).toEqual({
      command: "aspLsp.toggleLineComment",
      key: "ctrl+/",
      mac: "cmd+/",
      when: "editorTextFocus && editorLangId == classic-asp",
    });

    const blockBinding = pkg.contributes.keybindings.find(
      (k: { command: string }) => k.command === "aspLsp.toggleBlockComment",
    );
    expect(blockBinding).toMatchObject({
      command: "aspLsp.toggleBlockComment",
      when: "editorTextFocus && editorLangId == classic-asp",
    });
  });
});

describe("createRestartPolicy", () => {
  it("recovers beyond 5 consecutive crashes with bounded exponential backoff", () => {
    let clock = 1_000;
    const policy = createRestartPolicy({
      baseDelayMs: 500,
      maxDelayMs: 10_000,
      resetWindowMs: 60_000,
      now: () => clock,
    });

    const delays: number[] = [];
    for (let i = 1; i <= 8; i++) {
      clock += 1_000;
      const decision = policy.nextRestart();
      expect(decision.attempt).toBe(i);
      delays.push(decision.delayMs);
    }

    expect(delays).toEqual([0, 500, 1000, 2000, 4000, 8000, 10_000, 10_000]);
  });

  it("resets backoff after the reset window or explicit reset", () => {
    let clock = 0;
    const policy = createRestartPolicy({
      baseDelayMs: 500,
      maxDelayMs: 10_000,
      resetWindowMs: 60_000,
      now: () => clock,
    });

    expect(policy.nextRestart()).toEqual({ attempt: 1, delayMs: 0 });
    clock += 1_000;
    expect(policy.nextRestart()).toEqual({ attempt: 2, delayMs: 500 });

    clock += 60_001;
    expect(policy.nextRestart()).toEqual({ attempt: 1, delayMs: 0 });

    clock += 1_000;
    expect(policy.nextRestart()).toEqual({ attempt: 2, delayMs: 500 });
    policy.reset();
    expect(policy.nextRestart()).toEqual({ attempt: 1, delayMs: 0 });
  });
});
