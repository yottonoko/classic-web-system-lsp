import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { describe, expect, it } from "vitest";
import {
  getServerExecutablePath,
  serverExecutableIsRunnable,
  serverExecutableStatus,
} from "../src/server-path";

describe("server executable status", () => {
  it("distinguishes a missing server from an invalid path", () => {
    const directory = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-server-path-"));
    try {
      const missing = { kind: "go" as const, command: path.join(directory, "missing") };
      const invalid = { kind: "go" as const, command: directory };
      const nonExecutablePath = path.join(directory, "not-executable");
      fs.writeFileSync(nonExecutablePath, "server", "utf8");
      const nonExecutable = { kind: "go" as const, command: nonExecutablePath };

      expect(serverExecutableStatus(missing)).toBe("missing");
      expect(serverExecutableIsRunnable(missing)).toBe(false);
      expect(serverExecutableStatus(invalid)).toBe("invalid");
      expect(serverExecutableIsRunnable(invalid)).toBe(false);
      if (process.platform !== "win32") {
        expect(serverExecutableStatus(nonExecutable)).toBe("invalid");
        expect(serverExecutableIsRunnable(nonExecutable)).toBe(false);
      }
    } finally {
      fs.rmSync(directory, { recursive: true, force: true });
    }
  });

  it("accepts an executable regular file", () => {
    const directory = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-server-path-"));
    const command = path.join(directory, "asp-lsp-go");
    try {
      fs.writeFileSync(command, "#!/bin/sh\nexit 0\n", "utf8");
      fs.chmodSync(command, 0o755);
      expect(serverExecutableStatus({ kind: "go", command })).toBe("ready");
      expect(serverExecutableIsRunnable({ kind: "go", command })).toBe(true);
    } finally {
      fs.rmSync(directory, { recursive: true, force: true });
    }
  });

  it("falls back to a runnable bundled server when the development path is invalid", () => {
    const directory = fs.mkdtempSync(path.join(os.tmpdir(), "asp-lsp-server-path-"));
    const extensionRoot = path.join(directory, "apps", "vscode");
    const executableName = process.platform === "win32" ? "asp-lsp-go.exe" : "asp-lsp-go";
    const developmentCommand = path.join(directory, "bin", executableName);
    const bundledCommand = path.join(extensionRoot, "server", executableName);
    try {
      fs.mkdirSync(developmentCommand, { recursive: true });
      fs.mkdirSync(path.dirname(bundledCommand), { recursive: true });
      fs.writeFileSync(bundledCommand, "#!/bin/sh\nexit 0\n", "utf8");
      fs.chmodSync(bundledCommand, 0o755);

      const runtime = getServerExecutablePath({
        asAbsolutePath: (relativePath) => path.resolve(extensionRoot, relativePath),
      });

      expect(runtime.command).toBe(bundledCommand);
      expect(serverExecutableStatus(runtime)).toBe("ready");
    } finally {
      fs.rmSync(directory, { recursive: true, force: true });
    }
  });
});
