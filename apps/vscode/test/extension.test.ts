import * as fs from "node:fs";
import * as path from "node:path";
import { describe, expect, it } from "vitest";
import { resolveMixinServerPath } from "../src/serverPath.js";

const extensionRoot = path.resolve(import.meta.dirname, "..");

describe("resolveMixinServerPath", () => {
  it("prefers explicit configured path", () => {
    expect(
      resolveMixinServerPath({
        configuredPath: "/custom/bin/mixin",
        extensionPath: "/ext",
        env: { MIXIN_BIN: "/env/mixin" },
        fileExists: () => true,
      }),
    ).toBe("/custom/bin/mixin");
  });

  it("uses bundled server/mixin when present", () => {
    expect(
      resolveMixinServerPath({
        configuredPath: "",
        extensionPath: "/ext",
        platform: "linux",
        fileExists: (p) => p === path.join("/ext", "server", "mixin"),
      }),
    ).toBe(path.join("/ext", "server", "mixin"));
  });

  it("uses bundled server/mixin.exe on Windows", () => {
    expect(
      resolveMixinServerPath({
        configuredPath: "",
        extensionPath: "/ext",
        platform: "win32",
        fileExists: (p) => p === path.join("/ext", "server", "mixin.exe"),
      }),
    ).toBe(path.join("/ext", "server", "mixin.exe"));
  });

  it("falls back to MIXIN_BIN env var then PATH", () => {
    expect(
      resolveMixinServerPath({
        configuredPath: "",
        extensionPath: "/ext",
        env: { MIXIN_BIN: "/env/mixin" },
        fileExists: () => false,
      }),
    ).toBe("/env/mixin");

    expect(
      resolveMixinServerPath({
        configuredPath: "",
        extensionPath: "/ext",
        env: {},
        fileExists: () => false,
      }),
    ).toBe("mixin");
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
